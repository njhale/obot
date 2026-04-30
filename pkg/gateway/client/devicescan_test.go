package client

import (
	"context"
	"testing"
	"time"

	"github.com/obot-platform/obot/pkg/gateway/types"
	"gorm.io/datatypes"
)

func insertScan(t *testing.T, c *Client, scan types.DeviceScan) types.DeviceScan {
	t.Helper()
	if err := c.db.WithContext(context.Background()).Create(&scan).Error; err != nil {
		t.Fatalf("failed to insert scan: %v", err)
	}
	return scan
}

// TestAggregateMCPServers exercises the core aggregation: ConfigHash dedup
// across devices, latest-scan-per-device filtering, and time-window bounding.
func TestAggregateMCPServers(t *testing.T) {
	c := newTestClient(t)
	ctx := context.Background()

	now := time.Now().UTC()
	old := now.Add(-180 * 24 * time.Hour)

	// Two devices both run the same MCP. Device A also runs a unique one.
	sharedHash := "hash-shared"
	uniqueHash := "hash-unique"
	stale := "hash-stale"

	insertScan(t, c, types.DeviceScan{
		SubmittedBy: "user-a", DeviceID: "device-a", ScannedAt: now.Add(-1 * time.Hour),
		MCPServers: []types.DeviceScanMCPServer{
			{Client: "claude-code", Scope: "global", Name: "shared", Transport: "stdio", ConfigHash: sharedHash, Args: datatypes.JSONSlice[string]{"x"}},
			{Client: "claude-code", Scope: "global", Name: "unique", Transport: "stdio", ConfigHash: uniqueHash},
		},
	})
	insertScan(t, c, types.DeviceScan{
		SubmittedBy: "user-b", DeviceID: "device-b", ScannedAt: now.Add(-2 * time.Hour),
		MCPServers: []types.DeviceScanMCPServer{
			{Client: "codex", Scope: "global", Name: "shared", Transport: "stdio", ConfigHash: sharedHash},
		},
	})
	// Stale device — only old scans, drops out under window-then-latest.
	insertScan(t, c, types.DeviceScan{
		SubmittedBy: "user-c", DeviceID: "device-c", ScannedAt: old,
		MCPServers: []types.DeviceScanMCPServer{
			{Client: "claude-code", Scope: "global", Name: "stale", Transport: "stdio", ConfigHash: stale},
		},
	})

	// Window narrow enough to exclude the stale device.
	rows, total, err := c.AggregateMCPServers(ctx, AggregateMCPServerOptions{
		StartTime: now.Add(-30 * 24 * time.Hour),
		EndTime:   now.Add(time.Hour),
	})
	if err != nil {
		t.Fatalf("aggregate failed: %v", err)
	}
	if total != 2 {
		t.Errorf("expected 2 distinct hashes after window filter, got %d (%v)", total, rows)
	}
	byHash := map[string]types.AggregatedMCPServer{}
	for _, r := range rows {
		byHash[r.ConfigHash] = r
	}
	if got := byHash[sharedHash].DeviceCount; got != 2 {
		t.Errorf("shared hash device_count: want 2, got %d", got)
	}
	if got := byHash[sharedHash].UserCount; got != 2 {
		t.Errorf("shared hash user_count: want 2, got %d", got)
	}
	if got := byHash[sharedHash].ClientCount; got != 2 {
		t.Errorf("shared hash client_count: want 2, got %d", got)
	}
	if got := byHash[uniqueHash].DeviceCount; got != 1 {
		t.Errorf("unique hash device_count: want 1, got %d", got)
	}
	if _, ok := byHash[stale]; ok {
		t.Errorf("stale hash should be excluded by window filter")
	}

	// Default sort is device_count DESC: shared first, unique second.
	if rows[0].ConfigHash != sharedHash {
		t.Errorf("default sort device_count DESC: want shared first, got %q", rows[0].ConfigHash)
	}
}

// TestAggregateMCPServers_LatestScanWins verifies that adding a newer scan
// for a device that *omits* a previously-seen config drops that device's
// contribution to the config's device_count.
func TestAggregateMCPServers_LatestScanWins(t *testing.T) {
	c := newTestClient(t)
	ctx := context.Background()

	now := time.Now().UTC()
	hash := "hash-changing"

	// Device A initially had the config.
	insertScan(t, c, types.DeviceScan{
		SubmittedBy: "user-a", DeviceID: "device-a", ScannedAt: now.Add(-2 * time.Hour),
		MCPServers: []types.DeviceScanMCPServer{
			{Client: "claude-code", Scope: "global", Name: "x", Transport: "stdio", ConfigHash: hash},
		},
	})
	// Device B also has it.
	insertScan(t, c, types.DeviceScan{
		SubmittedBy: "user-b", DeviceID: "device-b", ScannedAt: now.Add(-2 * time.Hour),
		MCPServers: []types.DeviceScanMCPServer{
			{Client: "claude-code", Scope: "global", Name: "x", Transport: "stdio", ConfigHash: hash},
		},
	})
	// Device A re-scans and no longer has it.
	insertScan(t, c, types.DeviceScan{
		SubmittedBy: "user-a", DeviceID: "device-a", ScannedAt: now.Add(-1 * time.Hour),
		MCPServers: nil,
	})

	rows, _, err := c.AggregateMCPServers(ctx, AggregateMCPServerOptions{})
	if err != nil {
		t.Fatalf("aggregate failed: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("want 1 row, got %d", len(rows))
	}
	if rows[0].DeviceCount != 1 {
		t.Errorf("after device-a re-scan without config, device_count: want 1, got %d", rows[0].DeviceCount)
	}
}

// TestAggregateMCPServers_FilterBy_TransportClient verifies row-level
// filters narrow the candidate set before grouping.
func TestAggregateMCPServers_FilterBy_TransportClient(t *testing.T) {
	c := newTestClient(t)
	ctx := context.Background()

	now := time.Now().UTC()
	insertScan(t, c, types.DeviceScan{
		SubmittedBy: "u", DeviceID: "d", ScannedAt: now,
		MCPServers: []types.DeviceScanMCPServer{
			{Client: "claude-code", Transport: "stdio", Name: "a", ConfigHash: "ha"},
			{Client: "codex", Transport: "http", Name: "b", URL: "https://example.com", ConfigHash: "hb"},
		},
	})

	stdioRows, _, err := c.AggregateMCPServers(ctx, AggregateMCPServerOptions{
		Transports: []string{"stdio"},
	})
	if err != nil {
		t.Fatalf("aggregate failed: %v", err)
	}
	if len(stdioRows) != 1 || stdioRows[0].ConfigHash != "ha" {
		t.Errorf("transport=stdio filter: want only ha, got %v", stdioRows)
	}

	codexRows, _, err := c.AggregateMCPServers(ctx, AggregateMCPServerOptions{
		Clients: []string{"codex"},
	})
	if err != nil {
		t.Fatalf("aggregate failed: %v", err)
	}
	if len(codexRows) != 1 || codexRows[0].ConfigHash != "hb" {
		t.Errorf("client=codex filter: want only hb, got %v", codexRows)
	}
}

// TestGetAggregatedMCPServer verifies single-hash detail load and that
// EnvKeys / HeaderKeys are unioned across observations.
func TestGetAggregatedMCPServer(t *testing.T) {
	c := newTestClient(t)
	ctx := context.Background()

	now := time.Now().UTC()
	hash := "h"
	insertScan(t, c, types.DeviceScan{
		SubmittedBy: "u1", DeviceID: "d1", ScannedAt: now,
		MCPServers: []types.DeviceScanMCPServer{{
			Client: "claude-code", Name: "x", Transport: "stdio", ConfigHash: hash,
			Args:    datatypes.JSONSlice[string]{"--flag"},
			EnvKeys: datatypes.JSONSlice[string]{"FOO", "BAR"},
		}},
	})
	insertScan(t, c, types.DeviceScan{
		SubmittedBy: "u2", DeviceID: "d2", ScannedAt: now,
		MCPServers: []types.DeviceScanMCPServer{{
			Client: "codex", Name: "x", Transport: "stdio", ConfigHash: hash,
			Args:       datatypes.JSONSlice[string]{"--flag"},
			EnvKeys:    datatypes.JSONSlice[string]{"FOO", "BAZ"},
			HeaderKeys: datatypes.JSONSlice[string]{"X-Auth"},
		}},
	})

	d, err := c.GetAggregatedMCPServer(ctx, hash)
	if err != nil {
		t.Fatalf("get failed: %v", err)
	}
	if d.DeviceCount != 2 {
		t.Errorf("device_count: want 2, got %d", d.DeviceCount)
	}
	if len(d.Args) != 1 || d.Args[0] != "--flag" {
		t.Errorf("args: want [--flag], got %v", d.Args)
	}
	envSet := map[string]bool{}
	for _, k := range d.EnvKeys {
		envSet[k] = true
	}
	for _, want := range []string{"FOO", "BAR", "BAZ"} {
		if !envSet[want] {
			t.Errorf("env keys missing %q: got %v", want, d.EnvKeys)
		}
	}
	if len(d.HeaderKeys) != 1 || d.HeaderKeys[0] != "X-Auth" {
		t.Errorf("header keys: want [X-Auth], got %v", d.HeaderKeys)
	}
}

// TestListMCPServerOccurrences_PaginationAndIndex verifies the
// occurrence index is the row's position within its parent scan.
func TestListMCPServerOccurrences_PaginationAndIndex(t *testing.T) {
	c := newTestClient(t)
	ctx := context.Background()

	now := time.Now().UTC()
	hash := "h"
	// Scan with the target row at index 1 (second of three).
	insertScan(t, c, types.DeviceScan{
		SubmittedBy: "u", DeviceID: "d", ScannedAt: now,
		MCPServers: []types.DeviceScanMCPServer{
			{Client: "claude-code", Name: "first", Transport: "stdio", ConfigHash: "other-1"},
			{Client: "claude-code", Name: "target", Transport: "stdio", ConfigHash: hash},
			{Client: "claude-code", Name: "third", Transport: "stdio", ConfigHash: "other-2"},
		},
	})

	rows, total, err := c.ListMCPServerOccurrences(ctx, hash, 50, 0)
	if err != nil {
		t.Fatalf("list failed: %v", err)
	}
	if total != 1 {
		t.Errorf("total: want 1, got %d", total)
	}
	if len(rows) != 1 {
		t.Fatalf("rows: want 1, got %d", len(rows))
	}
	if rows[0].Index != 1 {
		t.Errorf("occurrence index within parent scan: want 1, got %d", rows[0].Index)
	}
}

// TestListMCPServerFilterOptions returns distinct values within window.
func TestListMCPServerFilterOptions(t *testing.T) {
	c := newTestClient(t)
	ctx := context.Background()

	now := time.Now().UTC()
	insertScan(t, c, types.DeviceScan{
		SubmittedBy: "u", DeviceID: "d1", ScannedAt: now,
		MCPServers: []types.DeviceScanMCPServer{
			{Client: "claude-code", Transport: "stdio", Name: "a", ConfigHash: "1"},
			{Client: "codex", Transport: "stdio", Name: "b", ConfigHash: "2"},
		},
	})
	insertScan(t, c, types.DeviceScan{
		SubmittedBy: "u", DeviceID: "d2", ScannedAt: now,
		MCPServers: []types.DeviceScanMCPServer{
			{Client: "claude-desktop", Transport: "http", Name: "c", URL: "https://x", ConfigHash: "3"},
		},
	})

	clients, err := c.ListMCPServerFilterOptions(ctx, "client", AggregateMCPServerOptions{})
	if err != nil {
		t.Fatalf("filter options failed: %v", err)
	}
	wantClients := []string{"claude-code", "claude-desktop", "codex"}
	if !equalStrings(clients, wantClients) {
		t.Errorf("client options: want %v, got %v", wantClients, clients)
	}

	transports, err := c.ListMCPServerFilterOptions(ctx, "transport", AggregateMCPServerOptions{})
	if err != nil {
		t.Fatalf("filter options failed: %v", err)
	}
	wantTransports := []string{"http", "stdio"}
	if !equalStrings(transports, wantTransports) {
		t.Errorf("transport options: want %v, got %v", wantTransports, transports)
	}
}

func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
