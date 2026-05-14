package handlers

import (
	"strings"
	"testing"

	types "github.com/obot-platform/obot/apiclient/types"
)

func TestValidateTopPrompts(t *testing.T) {
	mk := func(mutate func(*types.DeviceScanPrompt)) types.DeviceScanPrompt {
		p := types.DeviceScanPrompt{
			Client:      "claude_code",
			ChunkID:     "c1",
			SessionID:   "s1",
			PromptText:  "hi",
			PromptHash:  strings.Repeat("0", 64),
			PromptBytes: 2,
			Metrics: types.DeviceScanPromptMetrics{
				InputTokens: 10, OutputTokens: 20, TotalTokens: 30,
			},
		}
		if mutate != nil {
			mutate(&p)
		}
		return p
	}

	tests := []struct {
		name    string
		prompts []types.DeviceScanPrompt
		wantErr string
	}{
		{name: "empty", prompts: nil},
		{name: "ok single", prompts: []types.DeviceScanPrompt{mk(nil)}},
		{
			name:    "too many",
			prompts: makePrompts(11, mk),
			wantErr: "at most 10",
		},
		{
			name:    "bad client",
			prompts: []types.DeviceScanPrompt{mk(func(p *types.DeviceScanPrompt) { p.Client = "cursor" })},
			wantErr: "unsupported client",
		},
		{
			name:    "empty text",
			prompts: []types.DeviceScanPrompt{mk(func(p *types.DeviceScanPrompt) { p.PromptText = "" })},
			wantErr: "promptText length",
		},
		{
			name: "text too long",
			prompts: []types.DeviceScanPrompt{mk(func(p *types.DeviceScanPrompt) {
				p.PromptText = strings.Repeat("a", maxPromptTextBytes+1)
			})},
			wantErr: "promptText length",
		},
		{
			name:    "bad hash length",
			prompts: []types.DeviceScanPrompt{mk(func(p *types.DeviceScanPrompt) { p.PromptHash = "abc" })},
			wantErr: "promptHash",
		},
		{
			name:    "non-hex hash",
			prompts: []types.DeviceScanPrompt{mk(func(p *types.DeviceScanPrompt) { p.PromptHash = strings.Repeat("z", 64) })},
			wantErr: "promptHash",
		},
		{
			name: "mismatched totals",
			prompts: []types.DeviceScanPrompt{mk(func(p *types.DeviceScanPrompt) {
				p.Metrics.TotalTokens = 999
			})},
			wantErr: "metrics.totalTokens",
		},
		{
			name: "subagent depth ok at 5",
			prompts: []types.DeviceScanPrompt{mk(func(p *types.DeviceScanPrompt) {
				p.Subagents = nestSubagents(5)
			})},
		},
		{
			name: "subagent depth exceeds 5",
			prompts: []types.DeviceScanPrompt{mk(func(p *types.DeviceScanPrompt) {
				p.Subagents = nestSubagents(6)
			})},
			wantErr: "subagent tree depth",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := validateTopPrompts(tc.prompts)
			if tc.wantErr == "" {
				if err != nil {
					t.Fatalf("want nil, got %v", err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("want error containing %q, got %v", tc.wantErr, err)
			}
		})
	}
}

func makePrompts(n int, mk func(func(*types.DeviceScanPrompt)) types.DeviceScanPrompt) []types.DeviceScanPrompt {
	out := make([]types.DeviceScanPrompt, n)
	for i := range out {
		out[i] = mk(nil)
	}
	return out
}

func nestSubagents(depth int) []types.DeviceScanPromptSubagent {
	if depth <= 0 {
		return nil
	}
	return []types.DeviceScanPromptSubagent{{
		SubagentType: "Explore",
		Subagents:    nestSubagents(depth - 1),
	}}
}
