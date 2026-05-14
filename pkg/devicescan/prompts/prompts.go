// Package prompts is the per-client prompt-scanning extension point
// used by `obot scan --include-top-prompts`. Each supported client
// (claude_code today; codex/opencode/cursor in future milestones)
// ships its own sub-package that implements PromptScanner and
// registers itself via init() — the CLI wires registered scanners
// uniformly without per-client branching.
package prompts

import (
	"context"
	"io/fs"
	"time"

	"github.com/obot-platform/obot/apiclient/types"
)

// Options carries shared CLI inputs to every PromptScanner.
type Options struct {
	// HomeFS is the user's home directory as an fs.FS (os.DirFS($HOME) at
	// runtime). Scanners use slash-separated `path`-style paths against
	// this FS so the same code works on macOS, Linux, and Windows.
	HomeFS fs.FS
	// HomeAbs is the absolute $HOME path for resolving symlinks and for
	// converting fs.FS-relative paths back to OS paths when needed.
	HomeAbs string
	// Since is the earliest activity to consider — CLI passes now-30d.
	// Scanners must skip session files whose mtime is older.
	Since time.Time
	// TopK is the per-scanner cap (1..10) on the number of prompts
	// each scanner returns. Implementations may return fewer.
	TopK int
}

// PromptScanner discovers and ranks top-K user prompts for one client.
//
// Implementations MUST be:
//   - read-only against HomeFS
//   - safe to run concurrently with other PromptScanners
//   - bounded in memory (stream files; never load full transcripts)
//   - resilient to malformed/partial logs (skip + warn, never panic)
//
// Each implementation owns its own log discovery, parsing, chunking,
// and token aggregation, but emits the shared types.DeviceScanPrompt
// shape so the manifest is uniform.
type PromptScanner interface {
	// Client is the stable identifier persisted in
	// DeviceScanPrompt.Client (e.g. "claude_code", "codex", "cursor").
	// Must be lowercase snake_case and match a server allow-list entry.
	Client() string

	// Presence reports whether the client appears installed/configured
	// on this device. Scanners whose Presence returns false are skipped
	// before any heavier parsing work.
	Presence(opts Options) bool

	// TopPrompts returns up to opts.TopK prompts ranked by
	// metrics.totalTokens descending. Returning fewer is fine.
	TopPrompts(ctx context.Context, opts Options) ([]types.DeviceScanPrompt, error)
}
