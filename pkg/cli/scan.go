package cli

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/user"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/adrg/xdg"
	"github.com/google/uuid"
	"github.com/obot-platform/obot/apiclient/types"
	"github.com/obot-platform/obot/pkg/devicescan"
	"github.com/obot-platform/obot/pkg/devicescan/prompts"

	// Side-effect import: each prompt scanner sub-package registers
	// itself with the shared registry in init(). New clients land here.
	_ "github.com/obot-platform/obot/pkg/devicescan/prompts/claudecode"
	"github.com/obot-platform/obot/pkg/version"
	"github.com/spf13/cobra"
)

// promptScanWindow is the hardcoded 30-day look-back window for
// prompt scanning (DESIGN.md "Ranking window"). Locked at this layer
// because all registered PromptScanners must use the same window.
const promptScanWindow = 30 * 24 * time.Hour

// maxIncludeTopPrompts is the cap on --include-top-prompts. Matches
// the server-side cap on DeviceScanManifest.TopPrompts.
const maxIncludeTopPrompts = 10

type Scan struct {
	DeviceID          string `usage:"Override the persisted device identifier. Empty resolves via OBOT_SCAN_DEVICE_ID env var or the file at $XDG_DATA_HOME/obot/device_id (generated on first run)" env:"OBOT_SCAN_DEVICE_ID"`
	DryRun            bool   `usage:"Print the scan payload to stdout without submitting it" env:"OBOT_SCAN_DRY_RUN"`
	Timeout           int    `usage:"Number of seconds to wait for the scan to complete" default:"60" env:"OBOT_SCAN_TIMEOUT"`
	MaxDepth          int    `usage:"Maximum path depth (in segments below $HOME) to match when crawling for project-scope configs and skills; e.g. 5 matches files up to $HOME/a/b/c/d/e" default:"5" env:"OBOT_SCAN_MAX_DEPTH"`
	IncludeTopPrompts int    `usage:"Opt-in: extract and upload the top N (1..10) highest token-usage prompts from local AI client session logs over the last 30 days. When set, the truncated prompt text (≤2 KiB) and a SHA-256 of the full untruncated text are included alongside aggregate metrics; tool inputs/outputs, thinking blocks, and assistant text are NEVER uploaded. Run with --dry-run first to inspect the exact payload before submitting. 0 disables (default)." default:"0" env:"OBOT_SCAN_INCLUDE_TOP_PROMPTS"`

	root *Obot
}

func (s *Scan) Customize(cmd *cobra.Command) {
	cmd.Use = "scan"
	cmd.Short = "Inventory local AI client configuration and submit it to Obot"
	cmd.Args = cobra.NoArgs
}

func (s *Scan) Run(cmd *cobra.Command, _ []string) error {
	if s.IncludeTopPrompts < 0 || s.IncludeTopPrompts > maxIncludeTopPrompts {
		return fmt.Errorf("--include-top-prompts must be in [0, %d], got %d", maxIncludeTopPrompts, s.IncludeTopPrompts)
	}

	deviceID, err := ensureDeviceID(s.DeviceID)
	if err != nil {
		return fmt.Errorf("resolve device id: %w", err)
	}

	hostname, _ := os.Hostname()
	var username string
	if u, err := user.Current(); err == nil {
		username = u.Username
	}

	manifest := types.DeviceScanManifest{
		ScannerVersion: version.Get().String(),
		ScannedAt:      types.Time{Time: time.Now().UTC()},
		DeviceID:       deviceID,
		Hostname:       hostname,
		OS:             runtime.GOOS,
		Arch:           runtime.GOARCH,
		Username:       username,
	}

	ctx := cmd.Context()
	if s.Timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, time.Duration(s.Timeout)*time.Second)
		defer cancel()
	}

	homePath, err := os.UserHomeDir()
	if err != nil {
		return fmt.Errorf("failed to get user home dir: %w", err)
	}

	collected, err := devicescan.Scan(ctx, os.DirFS(homePath), homePath, s.MaxDepth)
	if err != nil {
		return fmt.Errorf("scan: %w", err)
	}

	manifest.Files = collected.Files
	manifest.MCPServers = collected.MCPServers
	manifest.Skills = collected.Skills
	manifest.Plugins = collected.Plugins
	manifest.Clients = collected.Clients

	if s.IncludeTopPrompts > 0 {
		manifest.TopPrompts = collectTopPrompts(ctx, cmd.ErrOrStderr(), os.DirFS(homePath), homePath, s.IncludeTopPrompts)
	}

	if s.DryRun {
		enc := json.NewEncoder(cmd.OutOrStdout())
		enc.SetIndent("", "  ")
		return enc.Encode(manifest)
	}

	if s.root.Client == nil {
		return fmt.Errorf("scan: no API client configured (set OBOT_TOKEN and OBOT_BASE_URL, or pass --dry-run)")
	}
	resp, err := s.root.Client.SubmitDeviceScan(ctx, manifest)
	if err != nil {
		return fmt.Errorf("submit scan: %w", err)
	}
	fmt.Fprintf(cmd.ErrOrStderr(), "Submitted scan #%d (received_at=%s)\n", resp.ID, resp.ReceivedAt.GetTime().Format(time.RFC3339))
	return nil
}

// collectTopPrompts fans out to every registered PromptScanner, then
// merges the results and trims to the global TopK. Per-scanner errors
// are logged to stderr and skipped — one misbehaving client must not
// fail the scan submission. With no scanners registered (the case in
// Phase 1 of M1), this returns nil so the manifest omits topPrompts
// entirely.
func collectTopPrompts(ctx context.Context, stderr io.Writer, homeFS fs.FS, homeAbs string, topK int) []types.DeviceScanPrompt {
	opts := prompts.Options{
		HomeFS:  homeFS,
		HomeAbs: homeAbs,
		Since:   time.Now().Add(-promptScanWindow),
		TopK:    topK,
	}
	var all []types.DeviceScanPrompt
	for _, s := range prompts.All() {
		if !s.Presence(opts) {
			continue
		}
		got, err := s.TopPrompts(ctx, opts)
		if err != nil {
			fmt.Fprintf(stderr, "obot scan: prompt scanner %q failed: %v\n", s.Client(), err)
			continue
		}
		all = append(all, got...)
	}
	return prompts.TopK(all, topK)
}

// ensureDeviceID returns deviceID if it is non-empty after trimming.
// Otherwise it reads (or, on first call, generates and persists) a UUIDv4 at
// xdg.DataFile("obot/device_id") with mode 0600.
//
// On macOS the file lands at ~/Library/Application Support/obot/device_id;
// on Linux at $XDG_DATA_HOME/obot/device_id (defaulting to
// ~/.local/share/obot/device_id); on Windows at %LocalAppData%\obot\device_id.
func ensureDeviceID(deviceID string) (string, error) {
	if deviceID = strings.TrimSpace(deviceID); deviceID != "" {
		return deviceID, nil
	}
	path, err := xdg.DataFile(filepath.Join("obot", "device_id"))
	if err != nil {
		return "", err
	}
	data, err := os.ReadFile(path)
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return "", fmt.Errorf("reading %s: %w", path, err)
	}
	if id := strings.TrimSpace(string(data)); id != "" {
		return id, nil
	}
	id := uuid.NewString()
	if err := os.WriteFile(path, []byte(id), 0600); err != nil {
		return "", fmt.Errorf("writing %s: %w", path, err)
	}
	return id, nil
}
