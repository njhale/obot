package handlers

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"

	"github.com/obot-platform/obot/apiclient/types"
	"github.com/obot-platform/obot/pkg/api"
	gclient "github.com/obot-platform/obot/pkg/gateway/client"
	gtypes "github.com/obot-platform/obot/pkg/gateway/types"
	"gorm.io/gorm"
	kuser "k8s.io/apiserver/pkg/authentication/user"
)

// TestDeviceScanPromptsEndToEnd exercises Phase 3: submit a manifest
// with TopPrompts, fetch through all three GET endpoints, then verify
// the parent-scan delete cascades to the prompt rows. The test runs the
// real handlers against a sqlite-backed gateway client — no HTTP
// transport, but every persistence and serialization path is hit.
func TestDeviceScanPromptsEndToEnd(t *testing.T) {
	gw := gclient.NewForTest(t)
	h := NewDeviceScansHandler()
	deviceID := "device-int-1"
	now := time.Now().UTC().Truncate(time.Second)

	manifest := types.DeviceScanManifest{
		ScannerVersion: "v0.0.0-test",
		ScannedAt:      *types.NewTime(now),
		DeviceID:       deviceID,
		Hostname:       "host",
		OS:             "darwin",
		Arch:           "arm64",
		Username:       "alice",
		TopPrompts: []types.DeviceScanPrompt{
			makeTestPrompt("chunk-low", 100, now.Add(-3*time.Minute)),
			makeTestPrompt("chunk-high", 900, now.Add(-2*time.Minute), types.DeviceScanPromptSubagent{
				SubagentType: "Explore",
				Description:  "code search",
				Metrics:      types.DeviceScanPromptMetrics{InputTokens: 100, OutputTokens: 50, TotalTokens: 150},
				MainSessionImpact: types.DeviceScanPromptSubagentImpact{
					CallTokens: 20, ResultTokens: 30, TotalTokens: 50,
				},
				ToolCalls: []types.DeviceScanPromptToolCall{{Name: "Grep", Count: 5}},
			}),
			makeTestPrompt("chunk-mid", 500, now.Add(-1*time.Minute)),
		},
	}

	created := doSubmit(t, h, gw, manifest, "alice")
	if created.ID == 0 {
		t.Fatalf("submit: missing server-assigned ID")
	}
	if got, want := len(created.TopPrompts), 3; got != want {
		t.Fatalf("submit: prompts in response: want %d, got %d", want, got)
	}

	// (1) ListPrompts: ordered by total_tokens DESC.
	list := doListScanPrompts(t, h, gw, created.ID, "")
	if list.Total != 3 || len(list.Items) != 3 {
		t.Fatalf("list: total=%d len=%d (%+v)", list.Total, len(list.Items), list.Items)
	}
	wantOrder := []string{"chunk-high", "chunk-mid", "chunk-low"}
	for i, w := range wantOrder {
		if list.Items[i].ChunkID != w {
			t.Errorf("list order[%d]: want %q, got %q (tokens=%d)", i, w, list.Items[i].ChunkID, list.Items[i].Metrics.TotalTokens)
		}
	}

	// (1a) ?limit=2 caps the result; total still reports the true count.
	limited := doListScanPrompts(t, h, gw, created.ID, "2")
	if limited.Total != 3 || limited.Limit != 2 || len(limited.Items) != 2 {
		t.Errorf("list limit=2: total=%d limit=%d len=%d", limited.Total, limited.Limit, len(limited.Items))
	}
	if limited.Items[0].ChunkID != "chunk-high" || limited.Items[1].ChunkID != "chunk-mid" {
		t.Errorf("list limit=2 order: %+v", limited.Items)
	}

	// (1b) Invalid limit returns 400.
	rec := callHandler(t, h.ListPrompts, gw, "alice", "GET",
		"/api/devices/scans/"+strconv.FormatUint(uint64(created.ID), 10)+"/prompts?limit=abc",
		map[string]string{"scan_id": strconv.FormatUint(uint64(created.ID), 10)}, nil)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("list limit=abc: want 400, got %d (%s)", rec.Code, rec.Body.String())
	}

	// (2) GetPrompt: single row with subagent payload preserved.
	prompt := doGetPrompt(t, h, gw, created.ID, "chunk-high")
	if prompt.Metrics.TotalTokens != 900 {
		t.Errorf("get prompt: total tokens want 900, got %d", prompt.Metrics.TotalTokens)
	}
	if len(prompt.Subagents) != 1 || prompt.Subagents[0].SubagentType != "Explore" {
		t.Errorf("get prompt: subagents not preserved: %+v", prompt.Subagents)
	}
	if len(prompt.Subagents[0].ToolCalls) != 1 || prompt.Subagents[0].ToolCalls[0].Name != "Grep" {
		t.Errorf("get prompt: subagent tool calls dropped: %+v", prompt.Subagents[0].ToolCalls)
	}

	// (2a) Missing chunk returns 404.
	missing := callHandler(t, h.GetPrompt, gw, "alice", "GET",
		"/api/devices/scans/"+strconv.FormatUint(uint64(created.ID), 10)+"/prompts/nope",
		map[string]string{"scan_id": strconv.FormatUint(uint64(created.ID), 10), "chunk_id": "nope"}, nil)
	if missing.Code != http.StatusNotFound {
		t.Errorf("get missing chunk: want 404, got %d (%s)", missing.Code, missing.Body.String())
	}

	// (3) GetLatestDevicePrompts: returns the same scan's prompts.
	latest := doGetLatest(t, h, gw, deviceID, "")
	if latest.Total != 3 || len(latest.Items) != 3 {
		t.Fatalf("latest: total=%d len=%d (%+v)", latest.Total, len(latest.Items), latest.Items)
	}
	if latest.Items[0].DeviceScanID != created.ID {
		t.Errorf("latest: scan id mismatch: want %d, got %d", created.ID, latest.Items[0].DeviceScanID)
	}
	for i, w := range wantOrder {
		if latest.Items[i].ChunkID != w {
			t.Errorf("latest order[%d]: want %q, got %q", i, w, latest.Items[i].ChunkID)
		}
	}

	// (3a) A newer scan with NO prompts must NOT bury the prompt scan —
	// "latest scan that has any prompts" wins.
	doSubmit(t, h, gw, types.DeviceScanManifest{
		ScannerVersion: "v0.0.0-test",
		ScannedAt:      *types.NewTime(now.Add(time.Hour)),
		DeviceID:       deviceID,
		OS:             "darwin",
		Arch:           "arm64",
	}, "alice")
	stillLatest := doGetLatest(t, h, gw, deviceID, "")
	if stillLatest.Total != 3 || len(stillLatest.Items) != 3 {
		t.Errorf("latest after empty newer scan: total=%d len=%d", stillLatest.Total, len(stillLatest.Items))
	}
	if stillLatest.Items[0].DeviceScanID != created.ID {
		t.Errorf("latest after empty newer scan: scan id mismatch: want %d, got %d", created.ID, stillLatest.Items[0].DeviceScanID)
	}

	// (3b) Unknown device returns empty 200, not 404.
	empty := doGetLatest(t, h, gw, "device-never-seen", "")
	if empty.Total != 0 || len(empty.Items) != 0 {
		t.Errorf("latest unknown device: want empty, got total=%d len=%d", empty.Total, len(empty.Items))
	}

	// (4) Cascade delete: removing the scan removes its prompt rows.
	delRec := callHandler(t, h.Delete, gw, "alice", "DELETE",
		"/api/devices/scans/"+strconv.FormatUint(uint64(created.ID), 10),
		map[string]string{"scan_id": strconv.FormatUint(uint64(created.ID), 10)}, nil)
	if delRec.Code >= 300 {
		t.Fatalf("delete scan: status %d (%s)", delRec.Code, delRec.Body.String())
	}
	if _, err := gw.GetScanPrompt(t.Context(), created.ID, "chunk-high"); !errors.Is(err, gorm.ErrRecordNotFound) {
		t.Errorf("after delete: want ErrRecordNotFound, got %v", err)
	}
	afterDelete := doGetLatest(t, h, gw, deviceID, "")
	if afterDelete.Total != 0 || len(afterDelete.Items) != 0 {
		t.Errorf("latest after delete: want empty, got total=%d len=%d", afterDelete.Total, len(afterDelete.Items))
	}
}

func makeTestPrompt(chunkID string, total int64, started time.Time, subagents ...types.DeviceScanPromptSubagent) types.DeviceScanPrompt {
	return types.DeviceScanPrompt{
		Client:      "claude_code",
		SessionID:   "session-a",
		ChunkID:     chunkID,
		Model:       "claude-opus-4-7",
		StartedAt:   *types.NewTime(started),
		EndedAt:     *types.NewTime(started.Add(2 * time.Second)),
		DurationMs:  2000,
		Cwd:         "/repo",
		GitBranch:   "main",
		PromptText:  "do the thing",
		PromptHash:  "0000000000000000000000000000000000000000000000000000000000000000",
		PromptBytes: 64,
		Metrics: types.DeviceScanPromptMetrics{
			InputTokens:  total / 2,
			OutputTokens: total - total/2,
			TotalTokens:  total,
		},
		Subagents: subagents,
		ToolCalls: []types.DeviceScanPromptToolCall{
			{Name: "Read", Count: 3},
			{Name: "Bash", Count: 1},
		},
	}
}

func callHandler(t *testing.T, fn func(api.Context) error, gw *gclient.Client, userID, method, target string, pathVars map[string]string, body []byte) *httptest.ResponseRecorder {
	t.Helper()
	var reqBody *bytes.Reader
	if body == nil {
		reqBody = bytes.NewReader(nil)
	} else {
		reqBody = bytes.NewReader(body)
	}
	req := httptest.NewRequest(method, target, reqBody)
	for k, v := range pathVars {
		req.SetPathValue(k, v)
	}
	rec := httptest.NewRecorder()
	ctx := api.Context{
		ResponseWriter: rec,
		Request:        req,
		GatewayClient:  gw,
		User:           &kuser.DefaultInfo{UID: userID, Name: userID},
	}
	if err := fn(ctx); err != nil {
		// Mirror the production router: render errors with the existing
		// helper so the response code matches what callers would see.
		writeHandlerError(rec, err)
	}
	return rec
}

func writeHandlerError(rec *httptest.ResponseRecorder, err error) {
	code := http.StatusInternalServerError
	msg := err.Error()
	if httpErr, ok := errors.AsType[*types.ErrHTTP](err); ok {
		code = httpErr.Code
		msg = httpErr.Message
	}
	rec.WriteHeader(code)
	_, _ = rec.WriteString(msg)
}

func doSubmit(t *testing.T, h *DeviceScansHandler, gw *gclient.Client, m types.DeviceScanManifest, user string) types.DeviceScan {
	t.Helper()
	body, err := json.Marshal(m)
	if err != nil {
		t.Fatalf("marshal manifest: %v", err)
	}
	rec := callHandler(t, h.Submit, gw, user, "POST", "/api/devices/scans", nil, body)
	if rec.Code != http.StatusCreated {
		t.Fatalf("submit: want 201, got %d (%s)", rec.Code, rec.Body.String())
	}
	var out types.DeviceScan
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("submit: unmarshal: %v", err)
	}
	return out
}

func doListScanPrompts(t *testing.T, h *DeviceScansHandler, gw *gclient.Client, scanID uint, limit string) types.DeviceScanPromptResponse {
	t.Helper()
	target := fmt.Sprintf("/api/devices/scans/%d/prompts", scanID)
	if limit != "" {
		target += "?limit=" + limit
	}
	rec := callHandler(t, h.ListPrompts, gw, "alice", "GET", target,
		map[string]string{"scan_id": strconv.FormatUint(uint64(scanID), 10)}, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("list: want 200, got %d (%s)", rec.Code, rec.Body.String())
	}
	var out types.DeviceScanPromptResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("list: unmarshal: %v", err)
	}
	return out
}

func doGetPrompt(t *testing.T, h *DeviceScansHandler, gw *gclient.Client, scanID uint, chunkID string) types.DeviceScanPrompt {
	t.Helper()
	rec := callHandler(t, h.GetPrompt, gw, "alice", "GET",
		fmt.Sprintf("/api/devices/scans/%d/prompts/%s", scanID, chunkID),
		map[string]string{"scan_id": strconv.FormatUint(uint64(scanID), 10), "chunk_id": chunkID}, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("get prompt: want 200, got %d (%s)", rec.Code, rec.Body.String())
	}
	var out types.DeviceScanPrompt
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("get prompt: unmarshal: %v", err)
	}
	return out
}

func doGetLatest(t *testing.T, h *DeviceScansHandler, gw *gclient.Client, deviceID, limit string) types.DeviceScanPromptResponse {
	t.Helper()
	target := "/api/devices/latest-prompts/" + deviceID
	if limit != "" {
		target += "?limit=" + limit
	}
	rec := callHandler(t, h.GetLatestDevicePrompts, gw, "alice", "GET", target,
		map[string]string{"device_id": deviceID}, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("get latest: want 200, got %d (%s)", rec.Code, rec.Body.String())
	}
	var out types.DeviceScanPromptResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("get latest: unmarshal: %v", err)
	}
	return out
}

// Ensure the gateway client's ConvertDeviceScan path is the canonical
// converter — referenced for clarity even though doSubmit uses it
// indirectly through h.Submit.
var _ = gtypes.ConvertDeviceScan
