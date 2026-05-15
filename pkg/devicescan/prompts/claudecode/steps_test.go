package claudecode

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"github.com/obot-platform/obot/apiclient/types"
	"github.com/obot-platform/obot/pkg/devicescan/prompts"
)

// stepsByKind groups a step slice by Kind for compact assertions.
func stepsByKind(s []types.DeviceScanPromptStep) map[string][]types.DeviceScanPromptStep {
	out := map[string][]types.DeviceScanPromptStep{}
	for _, st := range s {
		out[st.Kind] = append(out[st.Kind], st)
	}
	return out
}

// findStep returns the first step matching pred, or zero + false.
func findStep(s []types.DeviceScanPromptStep, pred func(types.DeviceScanPromptStep) bool) (types.DeviceScanPromptStep, bool) {
	for _, st := range s {
		if pred(st) {
			return st, true
		}
	}
	return types.DeviceScanPromptStep{}, false
}

// ---------------------------------------------------------------------------
// Block ordering: user → thinking → text in one turn
// ---------------------------------------------------------------------------

func TestSteps_BlockOrdering_ThinkingThenText(t *testing.T) {
	session := jsonl(t,
		userEntry("u1", "Why is the sky blue?", "2026-05-10T10:00:00Z"),
		assistantEntry("a1", "claude-opus-4-7", "2026-05-10T10:00:01Z", 40, 12,
			map[string]any{"type": "thinking", "thinking": "Rayleigh scattering — wavelengths…"},
			map[string]any{"type": "text", "text": "Short wavelengths scatter more, so the sky looks blue."},
		),
	)
	fsys := fstest.MapFS{".claude/projects/p/sess.jsonl": recentFile(t, session)}
	rows := buildPrompts(context.Background(), fsys, prompts.Options{Since: time.Now().Add(-30 * 24 * time.Hour)})
	if len(rows) != 1 {
		t.Fatalf("rows: %d", len(rows))
	}
	st := rows[0].Steps
	if len(st) != 3 {
		t.Fatalf("steps: got %d (%+v), want 3 (user, thinking, text)", len(st), st)
	}
	wantKinds := []string{"user", "thinking", "text"}
	for i, k := range wantKinds {
		if st[i].Kind != k {
			t.Errorf("step[%d].Kind: got %q want %q", i, st[i].Kind, k)
		}
		if st[i].Context != "main" {
			t.Errorf("step[%d].Context: got %q want main", i, st[i].Context)
		}
	}
	// Input + cache tokens land on the first emitted assistant step.
	if st[1].Tokens.Input != 40 {
		t.Errorf("thinking.Tokens.Input: got %d want 40", st[1].Tokens.Input)
	}
	if st[2].Tokens.Input != 0 {
		t.Errorf("text.Tokens.Input: got %d want 0 (charged to thinking)", st[2].Tokens.Input)
	}
	// Output split across two non-zero-size blocks. Sum == usage.OutputTokens.
	if st[1].Tokens.Output+st[2].Tokens.Output != 12 {
		t.Errorf("output sum: got %d+%d want 12", st[1].Tokens.Output, st[2].Tokens.Output)
	}
}

// ---------------------------------------------------------------------------
// Token proportioning across multiple tool_use blocks
// ---------------------------------------------------------------------------

func TestSteps_TokenProportioning_MultipleToolUse(t *testing.T) {
	// Two tool_use blocks whose JSON `input` sizes differ markedly.
	// output_tokens should split proportionally with the remainder
	// landing on the larger block.
	bigInput := map[string]any{"path": strings.Repeat("a", 200)}
	smallInput := map[string]any{"path": "x"}
	session := jsonl(t,
		userEntry("u1", "do two things", "2026-05-10T10:00:00Z"),
		assistantEntry("a1", "claude-opus-4-7", "2026-05-10T10:00:01Z", 50, 100,
			map[string]any{"type": "tool_use", "id": "tBig", "name": "Read", "input": bigInput},
			map[string]any{"type": "tool_use", "id": "tSmall", "name": "Read", "input": smallInput},
		),
		toolResultUser("u2", "2026-05-10T10:00:02Z", "tBig", "big result", ""),
		toolResultUser("u3", "2026-05-10T10:00:03Z", "tSmall", "small result", ""),
	)
	fsys := fstest.MapFS{".claude/projects/p/sess.jsonl": recentFile(t, session)}
	rows := buildPrompts(context.Background(), fsys, prompts.Options{Since: time.Now().Add(-30 * 24 * time.Hour)})
	if len(rows) != 1 {
		t.Fatalf("rows: %d", len(rows))
	}
	steps := rows[0].Steps
	byKind := stepsByKind(steps)
	tu := byKind["tool_use"]
	if len(tu) != 2 {
		t.Fatalf("tool_use steps: %d", len(tu))
	}
	// First tool_use (bigger input) carries the input/cache token charge.
	if tu[0].Tokens.Input != 50 {
		t.Errorf("first tool_use.Input: got %d want 50", tu[0].Tokens.Input)
	}
	if tu[1].Tokens.Input != 0 {
		t.Errorf("second tool_use.Input: got %d want 0", tu[1].Tokens.Input)
	}
	// Sum of proportioned output_tokens equals the turn total.
	if tu[0].Tokens.Output+tu[1].Tokens.Output != 100 {
		t.Errorf("proportioned output sum: %d+%d want 100", tu[0].Tokens.Output, tu[1].Tokens.Output)
	}
	if tu[0].Tokens.Output <= tu[1].Tokens.Output {
		t.Errorf("larger input should receive larger output share: got big=%d small=%d", tu[0].Tokens.Output, tu[1].Tokens.Output)
	}
	// ToolUseRef linkage on the two tool_result steps.
	tr := byKind["tool_result"]
	if len(tr) != 2 {
		t.Fatalf("tool_result steps: %d", len(tr))
	}
	gotRefs := map[string]bool{tr[0].ToolUseRef: true, tr[1].ToolUseRef: true}
	if !gotRefs["tBig"] || !gotRefs["tSmall"] {
		t.Errorf("tool_result.ToolUseRef linkage: got %+v", gotRefs)
	}
}

// ---------------------------------------------------------------------------
// is_error tool_result
// ---------------------------------------------------------------------------

func TestSteps_ToolResultIsError(t *testing.T) {
	// Hand-rolled tool_result with is_error:true in the inner block.
	resultUserEntry := map[string]any{
		"type":            "user",
		"uuid":            "u2",
		"timestamp":       "2026-05-10T10:00:02Z",
		"sessionId":       "sess",
		"isSidechain":     false,
		"isMeta":          true,
		"sourceToolUseID": "t1",
		"message": map[string]any{
			"role": "user",
			"content": []map[string]any{
				{"type": "tool_result", "tool_use_id": "t1", "is_error": true, "content": "boom"},
			},
		},
	}
	session := jsonl(t,
		userEntry("u1", "do it", "2026-05-10T10:00:00Z"),
		assistantEntry("a1", "claude-opus-4-7", "2026-05-10T10:00:01Z", 4, 1,
			map[string]any{"type": "tool_use", "id": "t1", "name": "Run", "input": map[string]any{}},
		),
		resultUserEntry,
	)
	fsys := fstest.MapFS{".claude/projects/p/sess.jsonl": recentFile(t, session)}
	rows := buildPrompts(context.Background(), fsys, prompts.Options{Since: time.Now().Add(-30 * 24 * time.Hour)})
	if len(rows) != 1 {
		t.Fatalf("rows: %d", len(rows))
	}
	steps := rows[0].Steps
	tr, ok := findStep(steps, func(s types.DeviceScanPromptStep) bool { return s.Kind == "tool_result" })
	if !ok {
		t.Fatalf("no tool_result step found in %+v", steps)
	}
	if !tr.IsError {
		t.Errorf("expected IsError=true, got %+v", tr)
	}
	if tr.ToolUseRef != "t1" {
		t.Errorf("ToolUseRef: got %q want t1", tr.ToolUseRef)
	}
	if tr.TextHead != "boom" {
		t.Errorf("TextHead: got %q want boom", tr.TextHead)
	}
}

// ---------------------------------------------------------------------------
// Subagent linkage + per-context AccumulatedContextTokens (3 deep)
// ---------------------------------------------------------------------------

func TestSteps_SubagentLinkageAndAccumulator_3Deep(t *testing.T) {
	parent := jsonl(t,
		userEntry("u1", "Investigate", "2026-05-10T10:00:00Z"),
		assistantEntry("a1", "claude-opus-4-7", "2026-05-10T10:00:01Z", 10, 1,
			map[string]any{"type": "tool_use", "id": "t1", "name": "Task",
				"input": map[string]any{"description": "L1 work", "subagent_type": "lead"}},
		),
		toolResultUser("u2", "2026-05-10T10:00:50Z", "t1", "ok", "L1AGENT"),
	)
	l1 := jsonl(t,
		map[string]any{"type": "user", "uuid": "x1", "sessionId": "sess", "isSidechain": true,
			"timestamp": "2026-05-10T10:00:05Z",
			"message":   map[string]any{"role": "user", "content": "do l1"}},
		map[string]any{"type": "assistant", "uuid": "x2", "sessionId": "sess", "isSidechain": true,
			"timestamp": "2026-05-10T10:00:06Z",
			"message": map[string]any{"role": "assistant", "id": "m1", "model": "claude-opus-4-7",
				"stop_reason": "end_turn",
				"usage":       map[string]any{"input_tokens": 5, "output_tokens": 2},
				"content": []map[string]any{{
					"type": "tool_use", "id": "t2", "name": "Task",
					"input": map[string]any{"description": "L2 work", "subagent_type": "worker"},
				}}}},
		map[string]any{"type": "user", "uuid": "x3", "sessionId": "sess", "isSidechain": true,
			"timestamp":       "2026-05-10T10:00:20Z",
			"isMeta":          true,
			"sourceToolUseID": "t2",
			"toolUseResult":   map[string]any{"agentId": "L2AGENT"},
			"message": map[string]any{"role": "user", "content": []map[string]any{
				{"type": "tool_result", "tool_use_id": "t2", "content": "ok"},
			}}},
	)
	l2 := jsonl(t,
		map[string]any{"type": "user", "uuid": "y1", "sessionId": "L1AGENT", "isSidechain": true,
			"timestamp": "2026-05-10T10:00:07Z",
			"message":   map[string]any{"role": "user", "content": "do l2"}},
		map[string]any{"type": "assistant", "uuid": "y2", "sessionId": "L1AGENT", "isSidechain": true,
			"timestamp": "2026-05-10T10:00:08Z",
			"message": map[string]any{"role": "assistant", "id": "m2", "model": "claude-opus-4-7",
				"stop_reason": "end_turn",
				"usage":       map[string]any{"input_tokens": 3, "output_tokens": 1},
				"content": []map[string]any{{
					"type": "tool_use", "id": "t3", "name": "Task",
					"input": map[string]any{"description": "L3 work", "subagent_type": "scout"},
				}}}},
		map[string]any{"type": "user", "uuid": "y3", "sessionId": "L1AGENT", "isSidechain": true,
			"timestamp":       "2026-05-10T10:00:18Z",
			"isMeta":          true,
			"sourceToolUseID": "t3",
			"toolUseResult":   map[string]any{"agentId": "L3AGENT"},
			"message": map[string]any{"role": "user", "content": []map[string]any{
				{"type": "tool_result", "tool_use_id": "t3", "content": "ok"},
			}}},
	)
	l3 := jsonl(t,
		map[string]any{"type": "assistant", "uuid": "z1", "sessionId": "L2AGENT", "isSidechain": true,
			"timestamp": "2026-05-10T10:00:09Z",
			"message": map[string]any{"role": "assistant", "id": "m3", "model": "claude-opus-4-7",
				"stop_reason": "end_turn",
				"usage":       map[string]any{"input_tokens": 2, "output_tokens": 1},
				"content":     []map[string]any{{"type": "text", "text": "leaf done"}}}},
	)
	fsys := fstest.MapFS{
		".claude/projects/p/sess.jsonl":                            recentFile(t, parent),
		".claude/projects/p/sess/subagents/agent-L1AGENT.jsonl":    recentFile(t, l1),
		".claude/projects/p/L1AGENT/subagents/agent-L2AGENT.jsonl": recentFile(t, l2),
		".claude/projects/p/L2AGENT/subagents/agent-L3AGENT.jsonl": recentFile(t, l3),
	}
	rows := buildPrompts(context.Background(), fsys, prompts.Options{Since: time.Now().Add(-30 * 24 * time.Hour)})
	if len(rows) != 1 {
		t.Fatalf("rows: %d", len(rows))
	}
	steps := rows[0].Steps

	// Subagent_call from main → L1.
	mainCall, ok := findStep(steps, func(s types.DeviceScanPromptStep) bool {
		return s.Kind == "subagent_call" && s.Context == "main"
	})
	if !ok {
		t.Fatalf("no main-context subagent_call step in %d steps", len(steps))
	}
	if mainCall.SubagentID != "L1AGENT" {
		t.Errorf("main subagent_call.SubagentID: got %q want L1AGENT", mainCall.SubagentID)
	}
	if mainCall.ToolUseID != "t1" {
		t.Errorf("main subagent_call.ToolUseID: got %q want t1", mainCall.ToolUseID)
	}
	if mainCall.TextHead != "L1 work" {
		t.Errorf("main subagent_call.TextHead: got %q", mainCall.TextHead)
	}

	// L1 transcript step exists with Context=subagent, SubagentID=L1AGENT.
	if _, ok := findStep(steps, func(s types.DeviceScanPromptStep) bool {
		return s.Context == "subagent" && s.SubagentID == "L1AGENT" && s.Kind == "tool_use"
	}); !ok {
		t.Errorf("missing L1AGENT tool_use step")
	}

	// Subagent_call from inside L1 → L2 (carries the spawned ID = L2AGENT,
	// Context = subagent).
	if _, ok := findStep(steps, func(s types.DeviceScanPromptStep) bool {
		return s.Kind == "subagent_call" && s.Context == "subagent" && s.SubagentID == "L2AGENT"
	}); !ok {
		t.Errorf("missing nested L1→L2 subagent_call step")
	}

	// Accumulator: main context input + cache should rise monotonically
	// across main steps; subagent context has its own running sum.
	var lastMain, lastL1 int64
	for _, s := range steps {
		switch {
		case s.Context == "main" && s.Kind != "subagent_call":
			if s.AccumulatedContextTokens < lastMain {
				t.Errorf("main accumulator non-monotone: %d -> %d", lastMain, s.AccumulatedContextTokens)
			}
			lastMain = s.AccumulatedContextTokens
		case s.Context == "subagent" && s.SubagentID == "L1AGENT" && s.Kind != "subagent_call":
			if s.AccumulatedContextTokens < lastL1 {
				t.Errorf("L1 accumulator non-monotone: %d -> %d", lastL1, s.AccumulatedContextTokens)
			}
			lastL1 = s.AccumulatedContextTokens
		}
	}
	if lastMain != 10 {
		t.Errorf("main accumulator final: got %d want 10", lastMain)
	}
	if lastL1 != 5 {
		t.Errorf("L1 accumulator final: got %d want 5", lastL1)
	}

	// Subagent tree node IDs are exposed for UI cross-linking.
	if rows[0].Subagents[0].SubagentID != "L1AGENT" {
		t.Errorf("subagent tree SubagentID: got %q want L1AGENT", rows[0].Subagents[0].SubagentID)
	}
}

// ---------------------------------------------------------------------------
// Image block → placeholder, no base64 ever crosses
// ---------------------------------------------------------------------------

func TestSteps_ImageBlockPlaceholder(t *testing.T) {
	bigSecret := strings.Repeat("Z", 4096) // pretend base64; must never appear in output
	session := jsonl(t,
		userEntry("u1", "describe image", "2026-05-10T10:00:00Z"),
		assistantEntry("a1", "claude-opus-4-7", "2026-05-10T10:00:01Z", 4, 1,
			map[string]any{
				"type": "image",
				"source": map[string]any{
					"type":       "base64",
					"media_type": "image/png",
					"data":       bigSecret,
				},
			},
		),
	)
	fsys := fstest.MapFS{".claude/projects/p/sess.jsonl": recentFile(t, session)}
	rows := buildPrompts(context.Background(), fsys, prompts.Options{Since: time.Now().Add(-30 * 24 * time.Hour)})
	if len(rows) != 1 {
		t.Fatalf("rows: %d", len(rows))
	}
	img, ok := findStep(rows[0].Steps, func(s types.DeviceScanPromptStep) bool {
		return s.Kind == "text" && strings.HasPrefix(s.TextHead, "[image")
	})
	if !ok {
		t.Fatalf("expected image placeholder step in %+v", rows[0].Steps)
	}
	if !strings.Contains(img.TextHead, "image/png") {
		t.Errorf("placeholder missing media_type: %q", img.TextHead)
	}
	// Walk every step's textual field — no base64 secret should escape.
	for _, s := range rows[0].Steps {
		if strings.Contains(s.TextHead, bigSecret) {
			t.Errorf("base64 leaked into TextHead: %q", s.TextHead)
		}
		if strings.Contains(s.TextHash, bigSecret) {
			t.Errorf("base64 leaked into TextHash: %q", s.TextHash)
		}
	}
	if img.TextHash != "" {
		t.Errorf("image placeholder must have empty TextHash; got %q", img.TextHash)
	}
	if img.TextBytes != 0 {
		t.Errorf("image placeholder must have zero TextBytes; got %d", img.TextBytes)
	}
}

// ---------------------------------------------------------------------------
// Long thinking head — 512 B truncation + hash + full byte count
// ---------------------------------------------------------------------------

func TestSteps_ThinkingTruncationAndHash(t *testing.T) {
	long := strings.Repeat("t", 4096)
	session := jsonl(t,
		userEntry("u1", "ponder", "2026-05-10T10:00:00Z"),
		assistantEntry("a1", "claude-opus-4-7", "2026-05-10T10:00:01Z", 4, 1,
			map[string]any{"type": "thinking", "thinking": long},
			map[string]any{"type": "text", "text": "done"},
		),
	)
	fsys := fstest.MapFS{".claude/projects/p/sess.jsonl": recentFile(t, session)}
	rows := buildPrompts(context.Background(), fsys, prompts.Options{Since: time.Now().Add(-30 * 24 * time.Hour)})
	if len(rows) != 1 {
		t.Fatalf("rows: %d", len(rows))
	}
	th, ok := findStep(rows[0].Steps, func(s types.DeviceScanPromptStep) bool { return s.Kind == "thinking" })
	if !ok {
		t.Fatal("no thinking step")
	}
	if len(th.TextHead) > prompts.MaxStepHeadBytes {
		t.Errorf("TextHead %d > cap %d", len(th.TextHead), prompts.MaxStepHeadBytes)
	}
	if !strings.HasSuffix(th.TextHead, "…") {
		t.Errorf("expected truncation marker, got tail %q", th.TextHead[len(th.TextHead)-6:])
	}
	if th.TextBytes != 4096 {
		t.Errorf("TextBytes: got %d want 4096", th.TextBytes)
	}
	sum := sha256.Sum256([]byte(long))
	if th.TextHash != hex.EncodeToString(sum[:]) {
		t.Errorf("TextHash must be SHA-256 of full untruncated content")
	}
}

// ---------------------------------------------------------------------------
// 2000-step cap — CLI truncates rather than emitting an oversize blob
// ---------------------------------------------------------------------------

func TestSteps_CapAtMaxStepsPerPrompt(t *testing.T) {
	// Emit one assistant turn that fans out into many text blocks. The
	// emitted step count = 1 user + N text blocks; with N tuned past
	// MaxStepsPerPrompt the post-assembly cap should kick in.
	const overflow = MaxStepsPerPrompt + 50
	blocks := make([]map[string]any, 0, overflow)
	for i := range overflow {
		blocks = append(blocks, map[string]any{"type": "text", "text": fmt.Sprintf("chunk%d", i)})
	}
	session := jsonl(t,
		userEntry("u1", "fan out", "2026-05-10T10:00:00Z"),
		assistantEntry("a1", "claude-opus-4-7", "2026-05-10T10:00:01Z", 4, 1, blocks...),
	)
	fsys := fstest.MapFS{".claude/projects/p/sess.jsonl": recentFile(t, session)}
	rows := buildPrompts(context.Background(), fsys, prompts.Options{Since: time.Now().Add(-30 * 24 * time.Hour)})
	if len(rows) != 1 {
		t.Fatalf("rows: %d", len(rows))
	}
	got := len(rows[0].Steps)
	if got != MaxStepsPerPrompt {
		t.Errorf("steps capped: got %d want %d", got, MaxStepsPerPrompt)
	}
}

// ---------------------------------------------------------------------------
// Privacy: tool_use ships keys only — values never leak
// ---------------------------------------------------------------------------

func TestSteps_ToolInputKeysOnlyNoValues(t *testing.T) {
	secret := "API_KEY_TOP_SECRET_5cKfL"
	session := jsonl(t,
		userEntry("u1", "call", "2026-05-10T10:00:00Z"),
		assistantEntry("a1", "claude-opus-4-7", "2026-05-10T10:00:01Z", 4, 1,
			map[string]any{
				"type": "tool_use", "id": "t1", "name": "Fetch",
				"input": map[string]any{"url": "https://example.com", "auth": secret, "method": "GET"},
			},
		),
	)
	fsys := fstest.MapFS{".claude/projects/p/sess.jsonl": recentFile(t, session)}
	rows := buildPrompts(context.Background(), fsys, prompts.Options{Since: time.Now().Add(-30 * 24 * time.Hour)})
	if len(rows) != 1 {
		t.Fatalf("rows: %d", len(rows))
	}
	tu, ok := findStep(rows[0].Steps, func(s types.DeviceScanPromptStep) bool { return s.Kind == "tool_use" })
	if !ok {
		t.Fatal("no tool_use step")
	}
	// Keys are present.
	gotKeys := map[string]bool{}
	for _, k := range tu.ToolInputKeys {
		gotKeys[k] = true
	}
	for _, want := range []string{"url", "auth", "method"} {
		if !gotKeys[want] {
			t.Errorf("missing key %q in ToolInputKeys (got %+v)", want, tu.ToolInputKeys)
		}
	}
	// Values must not appear anywhere on the step.
	if strings.Contains(tu.TextHead, secret) || strings.Contains(tu.ToolName, secret) {
		t.Errorf("value leaked into tool_use step: %+v", tu)
	}
	for _, k := range tu.ToolInputKeys {
		if strings.Contains(k, secret) {
			t.Errorf("value smuggled into key: %q", k)
		}
	}
}
