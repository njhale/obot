// Package claudecode is the Claude Code implementation of the
// prompts.PromptScanner interface. It registers itself via init()
// so the CLI picks it up automatically when --include-top-prompts
// is set; no other package needs to import this one for the
// registration to take effect.
package claudecode

import (
	"context"

	"github.com/obot-platform/obot/apiclient/types"
	"github.com/obot-platform/obot/pkg/devicescan"
	"github.com/obot-platform/obot/pkg/devicescan/prompts"
)

func init() {
	prompts.Register(&scanner{})
}

// scanner is the prompts.PromptScanner for Claude Code.
type scanner struct{}

func (scanner) Client() string { return clientID }

// Presence reuses the existing devicescan-level presence detection so
// the "is Claude Code installed?" answer stays consistent with what
// the config scan reports.
func (scanner) Presence(opts prompts.Options) bool {
	c := devicescan.DetectClaudeCodePresence(opts.HomeAbs)
	return c.BinaryPath != "" || c.InstallPath != "" || c.ConfigPath != ""
}

func (scanner) TopPrompts(ctx context.Context, opts prompts.Options) ([]types.DeviceScanPrompt, error) {
	all := buildPrompts(ctx, opts.HomeFS, opts)
	return prompts.TopK(all, opts.TopK), nil
}
