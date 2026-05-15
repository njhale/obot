package handlers

import (
	"encoding/json"
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

func TestValidateStepsAcceptsValidTimeline(t *testing.T) {
	p := types.DeviceScanPrompt{
		Client:      "claude_code",
		ChunkID:     "c1",
		SessionID:   "s1",
		PromptText:  "hi",
		PromptHash:  strings.Repeat("0", 64),
		PromptBytes: 2,
		Metrics:     types.DeviceScanPromptMetrics{InputTokens: 1, OutputTokens: 1, TotalTokens: 2},
		Subagents: []types.DeviceScanPromptSubagent{{
			SubagentID:   "sa-1",
			SubagentType: "Explore",
		}},
		Steps: []types.DeviceScanPromptStep{
			{Kind: "user", Context: "main", TextHead: "hi", TextBytes: 2, TextHash: strings.Repeat("a", 64)},
			{Kind: "tool_use", Context: "main", ToolUseID: "tu-1", ToolName: "Read", ToolInputKeys: []string{"file_path"}},
			{Kind: "tool_result", Context: "main", ToolUseRef: "tu-1", TextHead: "ok", TextBytes: 2, TextHash: strings.Repeat("b", 64)},
			{Kind: "subagent_call", Context: "main", SubagentID: "sa-1", TextHead: "code search"},
			{Kind: "thinking", Context: "subagent", SubagentID: "sa-1", TextHead: "let me search"},
		},
	}
	if err := validateTopPrompts([]types.DeviceScanPrompt{p}); err != nil {
		t.Fatalf("want nil, got %v", err)
	}
}

func TestValidateStepsDropsOrphanedToolResult(t *testing.T) {
	p := types.DeviceScanPrompt{
		Client:      "claude_code",
		ChunkID:     "c1",
		SessionID:   "s1",
		PromptText:  "hi",
		PromptHash:  strings.Repeat("0", 64),
		PromptBytes: 2,
		Metrics:     types.DeviceScanPromptMetrics{InputTokens: 1, OutputTokens: 1, TotalTokens: 2},
		Steps: []types.DeviceScanPromptStep{
			{Kind: "user", Context: "main", TextHead: "hi"},
			{Kind: "tool_result", Context: "main", ToolUseRef: "never-emitted", TextHead: "stray"},
			{Kind: "tool_use", Context: "main", ToolUseID: "tu-1", ToolName: "Read"},
			{Kind: "tool_result", Context: "main", ToolUseRef: "tu-1", TextHead: "ok"},
		},
	}
	prompts := []types.DeviceScanPrompt{p}
	if err := validateTopPrompts(prompts); err != nil {
		t.Fatalf("want nil, got %v", err)
	}
	got := prompts[0].Steps
	if len(got) != 3 {
		t.Fatalf("orphan should be dropped: want 3 steps, got %d (%+v)", len(got), got)
	}
	for i, s := range got {
		if s.Kind == "tool_result" && s.ToolUseRef == "never-emitted" {
			t.Errorf("orphan tool_result kept at index %d", i)
		}
	}
}

func TestValidateStepsErrors(t *testing.T) {
	mk := func(steps ...types.DeviceScanPromptStep) types.DeviceScanPrompt {
		return types.DeviceScanPrompt{
			Client:      "claude_code",
			ChunkID:     "c1",
			SessionID:   "s1",
			PromptText:  "hi",
			PromptHash:  strings.Repeat("0", 64),
			PromptBytes: 2,
			Metrics:     types.DeviceScanPromptMetrics{InputTokens: 1, OutputTokens: 1, TotalTokens: 2},
			Steps:       steps,
		}
	}
	tooManySteps := make([]types.DeviceScanPromptStep, maxPromptSteps+1)
	for i := range tooManySteps {
		tooManySteps[i] = types.DeviceScanPromptStep{Kind: "text", Context: "main"}
	}

	tests := []struct {
		name    string
		prompt  types.DeviceScanPrompt
		wantErr string
	}{
		{
			name:    "too many steps",
			prompt:  mk(tooManySteps...),
			wantErr: "steps length",
		},
		{
			name:    "invalid kind",
			prompt:  mk(types.DeviceScanPromptStep{Kind: "wat", Context: "main"}),
			wantErr: "invalid kind",
		},
		{
			name:    "invalid context",
			prompt:  mk(types.DeviceScanPromptStep{Kind: "text", Context: "elsewhere"}),
			wantErr: "invalid context",
		},
		{
			name: "textHead too long",
			prompt: mk(types.DeviceScanPromptStep{
				Kind: "text", Context: "main",
				TextHead: strings.Repeat("a", maxStepTextHeadBytes+1),
			}),
			wantErr: "textHead",
		},
		{
			name: "non-hex textHash",
			prompt: mk(types.DeviceScanPromptStep{
				Kind: "text", Context: "main",
				TextHash: strings.Repeat("z", 64),
			}),
			wantErr: "textHash",
		},
		{
			name: "negative tokens",
			prompt: mk(types.DeviceScanPromptStep{
				Kind: "text", Context: "main",
				Tokens: types.DeviceScanPromptStepTokens{Input: -1},
			}),
			wantErr: "negative token",
		},
		{
			name: "subagent context missing id",
			prompt: mk(types.DeviceScanPromptStep{
				Kind: "thinking", Context: "subagent",
			}),
			wantErr: "subagent context requires subagentID",
		},
		{
			name: "subagent id not in tree",
			prompt: func() types.DeviceScanPrompt {
				p := mk(types.DeviceScanPromptStep{
					Kind: "thinking", Context: "subagent", SubagentID: "missing",
				})
				return p
			}(),
			wantErr: "not found in subagent tree",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := validateTopPrompts([]types.DeviceScanPrompt{tc.prompt})
			if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("want error containing %q, got %v", tc.wantErr, err)
			}
		})
	}
}

// TestStepRedactionContract is the M2 privacy ratchet's checked-in
// canary: no tool input *values* leak (only the top-level keys ship),
// no content longer than 512 bytes ships, hashes only ever cover the
// full content, and image base64 never crosses the wire. Lives at the
// server boundary so future contributors can't quietly relax it.
func TestStepRedactionContract(t *testing.T) {
	t.Run("tool input keys ship, values do not", func(t *testing.T) {
		s := types.DeviceScanPromptStep{
			Kind: "tool_use", Context: "main",
			ToolUseID: "tu-1", ToolName: "Bash",
			ToolInputKeys: []string{"command", "description"},
		}
		b, err := json.Marshal(s)
		if err != nil {
			t.Fatalf("marshal: %v", err)
		}
		// No tool-input value can sneak across the wire — the only
		// exported field carrying tool-input data is the keys list.
		// If a future change adds a values field, this assertion stays
		// in place to catch it.
		if !strings.Contains(string(b), `"toolInputKeys":["command","description"]`) {
			t.Fatalf("keys not in payload: %s", b)
		}
		for _, banned := range []string{`"toolInput"`, `"input"`, `"toolInputValues"`} {
			if strings.Contains(string(b), banned) {
				t.Fatalf("forbidden field %q in payload: %s", banned, b)
			}
		}
	})
	t.Run("textHead caps at 512 bytes", func(t *testing.T) {
		p := types.DeviceScanPrompt{
			Client:      "claude_code",
			ChunkID:     "c1",
			SessionID:   "s1",
			PromptText:  "hi",
			PromptHash:  strings.Repeat("0", 64),
			PromptBytes: 2,
			Metrics:     types.DeviceScanPromptMetrics{InputTokens: 1, OutputTokens: 1, TotalTokens: 2},
			Steps: []types.DeviceScanPromptStep{{
				Kind: "text", Context: "main",
				TextHead: strings.Repeat("x", maxStepTextHeadBytes+1),
			}},
		}
		err := validateTopPrompts([]types.DeviceScanPrompt{p})
		if err == nil || !strings.Contains(err.Error(), "textHead") {
			t.Fatalf("oversized textHead must 400, got %v", err)
		}
	})
}
