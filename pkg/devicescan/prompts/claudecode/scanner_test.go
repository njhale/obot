package claudecode

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"github.com/obot-platform/obot/apiclient/types"
	"github.com/obot-platform/obot/pkg/devicescan/prompts"
)

// ---------------------------------------------------------------------------
// Fixture helpers
// ---------------------------------------------------------------------------

// jsonl serializes one entry per line. Tests assemble fixtures by
// passing a slice of structured maps; we render to bytes so MapFS can
// hand them to the streaming parser as if they came off disk.
func jsonl(t *testing.T, lines ...any) []byte {
	t.Helper()
	var sb strings.Builder
	for _, l := range lines {
		b, err := json.Marshal(l)
		if err != nil {
			t.Fatalf("marshal fixture line: %v", err)
		}
		sb.Write(b)
		sb.WriteByte('\n')
	}
	return []byte(sb.String())
}

// recentFile produces a MapFile whose mtime is well inside the 30-day
// window used by the scanner.
func recentFile(t *testing.T, data []byte) *fstest.MapFile {
	t.Helper()
	return &fstest.MapFile{Data: data, ModTime: time.Now()}
}

// userEntry assembles a real-user-input JSONL line. Use this for
// chunk-starting entries.
func userEntry(uuid, text, ts string) map[string]any {
	return map[string]any{
		"type":        "user",
		"uuid":        uuid,
		"timestamp":   ts,
		"sessionId":   "sess",
		"isSidechain": false,
		"cwd":         "/work",
		"gitBranch":   "main",
		"message": map[string]any{
			"role":    "user",
			"content": text,
		},
	}
}

// assistantEntry assembles an assistant entry with optional usage,
// model, and content blocks.
func assistantEntry(uuid, model, ts string, in, out int64, blocks ...map[string]any) map[string]any {
	if blocks == nil {
		blocks = []map[string]any{{"type": "text", "text": "ok"}}
	}
	return map[string]any{
		"type":        "assistant",
		"uuid":        uuid,
		"timestamp":   ts,
		"sessionId":   "sess",
		"isSidechain": false,
		"message": map[string]any{
			"role":        "assistant",
			"id":          "msg_" + uuid,
			"model":       model,
			"stop_reason": "end_turn",
			"usage": map[string]any{
				"input_tokens":                in,
				"output_tokens":               out,
				"cache_read_input_tokens":     0,
				"cache_creation_input_tokens": 0,
			},
			"content": blocks,
		},
	}
}

// toolResultUser assembles an internal-user tool_result entry. Use to
// close out a Task call so the chunker can attribute result tokens.
func toolResultUser(uuid, ts, toolUseID, content, agentID string) map[string]any {
	return map[string]any{
		"type":            "user",
		"uuid":            uuid,
		"timestamp":       ts,
		"sessionId":       "sess",
		"isSidechain":     false,
		"isMeta":          true,
		"sourceToolUseID": toolUseID,
		"toolUseResult":   map[string]any{"agentId": agentID},
		"message": map[string]any{
			"role": "user",
			"content": []map[string]any{
				{"type": "tool_result", "tool_use_id": toolUseID, "content": content},
			},
		},
	}
}

// ---------------------------------------------------------------------------
// Single-turn smoke test
// ---------------------------------------------------------------------------

func TestBuildPrompts_SingleTurn(t *testing.T) {
	session := jsonl(t,
		userEntry("u1", "What's 2 + 2?", "2026-05-10T10:00:00Z"),
		assistantEntry("a1", "claude-opus-4-7", "2026-05-10T10:00:01Z", 50, 5),
	)
	fsys := fstest.MapFS{
		".claude/projects/-Users-x/sess.jsonl": recentFile(t, session),
	}

	rows := buildPrompts(context.Background(), fsys, prompts.Options{
		Since: time.Now().Add(-30 * 24 * time.Hour),
	})
	if len(rows) != 1 {
		t.Fatalf("expected 1 row, got %d", len(rows))
	}
	r := rows[0]
	if r.Client != "claude_code" {
		t.Errorf("client: got %q want claude_code", r.Client)
	}
	if r.PromptText != "What's 2 + 2?" {
		t.Errorf("promptText: got %q", r.PromptText)
	}
	if r.MainMetrics.InputTokens != 50 || r.MainMetrics.OutputTokens != 5 {
		t.Errorf("main metrics: %+v", r.MainMetrics)
	}
	if r.Metrics.TotalTokens != 55 {
		t.Errorf("transitive total: got %d want 55", r.Metrics.TotalTokens)
	}
	if r.Model != "claude-opus-4-7" {
		t.Errorf("model: %q", r.Model)
	}
	if len(r.ToolCalls) != 0 || len(r.Subagents) != 0 {
		t.Errorf("expected no tools/subagents, got %+v / %+v", r.ToolCalls, r.Subagents)
	}
	if r.DurationMs != 1000 {
		t.Errorf("durationMs: got %d want 1000", r.DurationMs)
	}
	if r.PromptHash == "" || len(r.PromptHash) != 64 {
		t.Errorf("promptHash: %q", r.PromptHash)
	}
}

// ---------------------------------------------------------------------------
// Multi-turn + tool calls aggregation
// ---------------------------------------------------------------------------

func TestBuildPrompts_ToolCallsAggregated(t *testing.T) {
	session := jsonl(t,
		userEntry("u1", "Read foo.txt then write bar.txt", "2026-05-10T10:00:00Z"),
		assistantEntry("a1", "claude-opus-4-7", "2026-05-10T10:00:01Z", 100, 20,
			map[string]any{"type": "tool_use", "id": "t1", "name": "Read", "input": map[string]any{"path": "foo.txt"}},
		),
		toolResultUser("u2", "2026-05-10T10:00:02Z", "t1", "file contents", ""),
		assistantEntry("a2", "claude-opus-4-7", "2026-05-10T10:00:03Z", 110, 30,
			map[string]any{"type": "tool_use", "id": "t2", "name": "Write", "input": map[string]any{"path": "bar.txt"}},
			map[string]any{"type": "tool_use", "id": "t3", "name": "Read", "input": map[string]any{"path": "baz.txt"}},
		),
		toolResultUser("u3", "2026-05-10T10:00:04Z", "t2", "ok", ""),
		toolResultUser("u4", "2026-05-10T10:00:04Z", "t3", "more", ""),
	)
	fsys := fstest.MapFS{
		".claude/projects/-Users-x/sess.jsonl": recentFile(t, session),
	}
	rows := buildPrompts(context.Background(), fsys, prompts.Options{Since: time.Now().Add(-30 * 24 * time.Hour)})
	if len(rows) != 1 {
		t.Fatalf("rows: %d", len(rows))
	}
	r := rows[0]
	if r.MainMetrics.InputTokens != 210 || r.MainMetrics.OutputTokens != 50 {
		t.Errorf("main metrics: %+v", r.MainMetrics)
	}
	// Read x2, Write x1, sorted desc by count then asc by name.
	want := []types.DeviceScanPromptToolCall{{Name: "Read", Count: 2}, {Name: "Write", Count: 1}}
	if !equalToolCalls(r.ToolCalls, want) {
		t.Errorf("toolCalls: got %+v want %+v", r.ToolCalls, want)
	}
}

// ---------------------------------------------------------------------------
// Subagent (new layout) — single level
// ---------------------------------------------------------------------------

func TestBuildPrompts_SubagentNewLayout(t *testing.T) {
	parent := jsonl(t,
		userEntry("u1", "Explore the repo", "2026-05-10T10:00:00Z"),
		assistantEntry("a1", "claude-opus-4-7", "2026-05-10T10:00:01Z", 80, 10,
			map[string]any{
				"type": "tool_use", "id": "task1", "name": "Task",
				"input": map[string]any{"description": "Explore", "subagent_type": "explorer"},
			},
		),
		toolResultUser("u2", "2026-05-10T10:00:30Z", "task1", "result body", "agentX"),
	)
	subagent := jsonl(t,
		map[string]any{
			"type": "user", "uuid": "su1", "timestamp": "2026-05-10T10:00:02Z",
			"sessionId": "sess", "isSidechain": true,
			"message": map[string]any{"role": "user", "content": "Explore"},
		},
		map[string]any{
			"type": "assistant", "uuid": "sa1", "timestamp": "2026-05-10T10:00:10Z",
			"sessionId": "sess", "isSidechain": true,
			"message": map[string]any{
				"role": "assistant", "id": "m", "model": "claude-opus-4-7", "stop_reason": "end_turn",
				"usage":   map[string]any{"input_tokens": 30, "output_tokens": 7},
				"content": []map[string]any{{"type": "tool_use", "id": "ti1", "name": "Grep", "input": map[string]any{}}},
			},
		},
	)
	fsys := fstest.MapFS{
		".claude/projects/-Users-x/sess.jsonl":                        recentFile(t, parent),
		".claude/projects/-Users-x/sess/subagents/agent-agentX.jsonl": recentFile(t, subagent),
	}
	rows := buildPrompts(context.Background(), fsys, prompts.Options{Since: time.Now().Add(-30 * 24 * time.Hour)})
	if len(rows) != 1 {
		t.Fatalf("rows: %d", len(rows))
	}
	r := rows[0]
	if len(r.Subagents) != 1 {
		t.Fatalf("expected 1 subagent, got %d", len(r.Subagents))
	}
	sa := r.Subagents[0]
	if sa.Description != "Explore" {
		t.Errorf("description: %q", sa.Description)
	}
	if sa.SubagentType != "explorer" {
		t.Errorf("subagentType: %q", sa.SubagentType)
	}
	if sa.Metrics.InputTokens != 30 || sa.Metrics.OutputTokens != 7 {
		t.Errorf("sa metrics: %+v", sa.Metrics)
	}
	if sa.MainSessionImpact.TotalTokens == 0 {
		t.Errorf("expected non-zero main session impact, got %+v", sa.MainSessionImpact)
	}
	if sa.MainSessionImpact.TotalTokens != sa.MainSessionImpact.CallTokens+sa.MainSessionImpact.ResultTokens {
		t.Errorf("impact total != call+result: %+v", sa.MainSessionImpact)
	}
	// Transitive metrics include subagent contribution.
	if r.Metrics.InputTokens != 80+30 || r.Metrics.OutputTokens != 10+7 {
		t.Errorf("transitive: %+v", r.Metrics)
	}
	if r.MainMetrics.InputTokens != 80 {
		t.Errorf("main metrics should exclude subagent: %+v", r.MainMetrics)
	}
}

// ---------------------------------------------------------------------------
// Two chunks in the same session — each owns its own subagents
// ---------------------------------------------------------------------------

func TestBuildPrompts_SubagentScopedToChunk(t *testing.T) {
	// Parent session with two chunks. Each chunk spawns its own
	// subagent. The sidechain directory contains both subagent files;
	// without per-chunk filtering we'd double-attribute both subagents
	// to both chunks.
	parent := jsonl(t,
		userEntry("u1", "First chunk question", "2026-05-10T10:00:00Z"),
		assistantEntry("a1", "claude-opus-4-7", "2026-05-10T10:00:01Z", 10, 1,
			map[string]any{"type": "tool_use", "id": "tA", "name": "Agent",
				"input": map[string]any{"description": "First", "subagent_type": "explorer"}},
		),
		toolResultUser("u2", "2026-05-10T10:00:30Z", "tA", "ok", "aOne"),
		userEntry("u3", "Second chunk question", "2026-05-10T11:00:00Z"),
		assistantEntry("a2", "claude-opus-4-7", "2026-05-10T11:00:01Z", 20, 2,
			map[string]any{"type": "tool_use", "id": "tB", "name": "Agent",
				"input": map[string]any{"description": "Second", "subagent_type": "worker"}},
		),
		toolResultUser("u4", "2026-05-10T11:00:30Z", "tB", "ok", "aTwo"),
	)
	mkSidechain := func(in int64) []byte {
		return jsonl(t,
			map[string]any{"type": "assistant", "uuid": "x", "sessionId": "sess", "isSidechain": true,
				"timestamp": "2026-05-10T10:00:05Z",
				"message": map[string]any{"role": "assistant", "id": "m", "model": "claude-opus-4-7",
					"stop_reason": "end_turn",
					"usage":       map[string]any{"input_tokens": in, "output_tokens": 1},
					"content":     []map[string]any{}}},
		)
	}
	fsys := fstest.MapFS{
		".claude/projects/p/sess.jsonl":                      recentFile(t, parent),
		".claude/projects/p/sess/subagents/agent-aOne.jsonl": recentFile(t, mkSidechain(100)),
		".claude/projects/p/sess/subagents/agent-aTwo.jsonl": recentFile(t, mkSidechain(200)),
	}
	rows := buildPrompts(context.Background(), fsys, prompts.Options{Since: time.Now().Add(-30 * 24 * time.Hour)})
	if len(rows) != 2 {
		t.Fatalf("rows: %d", len(rows))
	}
	// Sort by user text so assertions are stable regardless of order.
	sort.Slice(rows, func(i, j int) bool { return rows[i].PromptText < rows[j].PromptText })
	if len(rows[0].Subagents) != 1 || rows[0].Subagents[0].Description != "First" {
		t.Errorf("first chunk's subagent: %+v", rows[0].Subagents)
	}
	if len(rows[1].Subagents) != 1 || rows[1].Subagents[0].Description != "Second" {
		t.Errorf("second chunk's subagent: %+v", rows[1].Subagents)
	}
}

// ---------------------------------------------------------------------------
// Subagent (legacy layout)
// ---------------------------------------------------------------------------

func TestBuildPrompts_SubagentLegacyLayout(t *testing.T) {
	parent := jsonl(t,
		userEntry("u1", "Explore", "2026-05-10T10:00:00Z"),
		assistantEntry("a1", "claude-opus-4-7", "2026-05-10T10:00:01Z", 50, 5,
			map[string]any{
				"type": "tool_use", "id": "task1", "name": "Task",
				"input": map[string]any{"description": "Legacy", "subagent_type": "old"},
			},
		),
		toolResultUser("u2", "2026-05-10T10:00:30Z", "task1", "result", "legacyAgent"),
	)
	subagent := jsonl(t,
		map[string]any{
			"type": "user", "uuid": "su1", "timestamp": "2026-05-10T10:00:02Z",
			"sessionId": "sess", "isSidechain": true,
			"message": map[string]any{"role": "user", "content": "Legacy"},
		},
		map[string]any{
			"type": "assistant", "uuid": "sa1", "timestamp": "2026-05-10T10:00:10Z",
			"sessionId": "sess", "isSidechain": true,
			"message": map[string]any{
				"role": "assistant", "id": "m", "model": "claude-opus-4-7", "stop_reason": "end_turn",
				"usage":   map[string]any{"input_tokens": 22, "output_tokens": 4},
				"content": []map[string]any{},
			},
		},
	)
	fsys := fstest.MapFS{
		".claude/projects/-Users-x/sess.jsonl":              recentFile(t, parent),
		".claude/projects/-Users-x/agent-legacyAgent.jsonl": recentFile(t, subagent),
	}
	rows := buildPrompts(context.Background(), fsys, prompts.Options{Since: time.Now().Add(-30 * 24 * time.Hour)})
	if len(rows) != 1 || len(rows[0].Subagents) != 1 {
		t.Fatalf("legacy: got %d rows / %d subagents", len(rows), len(rows[0].Subagents))
	}
	if rows[0].Subagents[0].Description != "Legacy" {
		t.Errorf("description: %q", rows[0].Subagents[0].Description)
	}
}

// ---------------------------------------------------------------------------
// Nested subagents (3 deep)
// ---------------------------------------------------------------------------

func TestBuildPrompts_NestedSubagents(t *testing.T) {
	parent := jsonl(t,
		userEntry("u1", "Investigate", "2026-05-10T10:00:00Z"),
		assistantEntry("a1", "claude-opus-4-7", "2026-05-10T10:00:01Z", 10, 1,
			map[string]any{"type": "tool_use", "id": "t1", "name": "Task",
				"input": map[string]any{"description": "L1", "subagent_type": "lead"}},
		),
		toolResultUser("u2", "2026-05-10T10:00:50Z", "t1", "ok", "a1"),
	)
	// L1 subagent — spawns L2.
	l1 := jsonl(t,
		map[string]any{"type": "user", "uuid": "x1", "sessionId": "sess", "isSidechain": true,
			"timestamp": "2026-05-10T10:00:05Z",
			"message":   map[string]any{"role": "user", "content": "do it"}},
		map[string]any{"type": "assistant", "uuid": "x2", "sessionId": "sess", "isSidechain": true,
			"timestamp": "2026-05-10T10:00:06Z",
			"message": map[string]any{"role": "assistant", "id": "m1", "model": "claude-opus-4-7",
				"stop_reason": "end_turn",
				"usage":       map[string]any{"input_tokens": 5, "output_tokens": 1},
				"content": []map[string]any{{
					"type": "tool_use", "id": "t2", "name": "Task",
					"input": map[string]any{"description": "L2", "subagent_type": "worker"},
				}}}},
		map[string]any{"type": "user", "uuid": "x3", "sessionId": "sess", "isSidechain": true,
			"timestamp":       "2026-05-10T10:00:20Z",
			"isMeta":          true,
			"sourceToolUseID": "t2",
			"toolUseResult":   map[string]any{"agentId": "a2"},
			"message": map[string]any{"role": "user", "content": []map[string]any{
				{"type": "tool_result", "tool_use_id": "t2", "content": "ok"},
			}}},
	)
	// L2 subagent — spawns L3.
	l2 := jsonl(t,
		map[string]any{"type": "user", "uuid": "y1", "sessionId": "a1", "isSidechain": true,
			"timestamp": "2026-05-10T10:00:07Z",
			"message":   map[string]any{"role": "user", "content": "do it"}},
		map[string]any{"type": "assistant", "uuid": "y2", "sessionId": "a1", "isSidechain": true,
			"timestamp": "2026-05-10T10:00:08Z",
			"message": map[string]any{"role": "assistant", "id": "m2", "model": "claude-opus-4-7",
				"stop_reason": "end_turn",
				"usage":       map[string]any{"input_tokens": 3, "output_tokens": 1},
				"content": []map[string]any{{
					"type": "tool_use", "id": "t3", "name": "Task",
					"input": map[string]any{"description": "L3", "subagent_type": "scout"},
				}}}},
		map[string]any{"type": "user", "uuid": "y3", "sessionId": "a1", "isSidechain": true,
			"timestamp":       "2026-05-10T10:00:18Z",
			"isMeta":          true,
			"sourceToolUseID": "t3",
			"toolUseResult":   map[string]any{"agentId": "a3"},
			"message": map[string]any{"role": "user", "content": []map[string]any{
				{"type": "tool_result", "tool_use_id": "t3", "content": "ok"},
			}}},
	)
	// L3 subagent — leaf.
	l3 := jsonl(t,
		map[string]any{"type": "assistant", "uuid": "z1", "sessionId": "a2", "isSidechain": true,
			"timestamp": "2026-05-10T10:00:09Z",
			"message": map[string]any{"role": "assistant", "id": "m3", "model": "claude-opus-4-7",
				"stop_reason": "end_turn",
				"usage":       map[string]any{"input_tokens": 2, "output_tokens": 1},
				"content":     []map[string]any{{"type": "text", "text": "done"}}}},
	)
	fsys := fstest.MapFS{
		".claude/projects/p/sess.jsonl":                    recentFile(t, parent),
		".claude/projects/p/sess/subagents/agent-a1.jsonl": recentFile(t, l1),
		".claude/projects/p/a1/subagents/agent-a2.jsonl":   recentFile(t, l2),
		".claude/projects/p/a2/subagents/agent-a3.jsonl":   recentFile(t, l3),
	}
	rows := buildPrompts(context.Background(), fsys, prompts.Options{Since: time.Now().Add(-30 * 24 * time.Hour)})
	if len(rows) != 1 {
		t.Fatalf("rows: %d", len(rows))
	}
	r := rows[0]
	if len(r.Subagents) != 1 {
		t.Fatalf("L1: %d", len(r.Subagents))
	}
	if len(r.Subagents[0].Subagents) != 1 {
		t.Fatalf("L2: %d", len(r.Subagents[0].Subagents))
	}
	if len(r.Subagents[0].Subagents[0].Subagents) != 1 {
		t.Fatalf("L3: %d", len(r.Subagents[0].Subagents[0].Subagents))
	}
	// Transitive sum at the root includes L1+L2+L3 contributions.
	wantInput := int64(10 + 5 + 3 + 2)
	if r.Metrics.InputTokens != wantInput {
		t.Errorf("root transitive input: got %d want %d", r.Metrics.InputTokens, wantInput)
	}
}

// ---------------------------------------------------------------------------
// Depth cap (6 levels — only 5 surface)
// ---------------------------------------------------------------------------

func TestBuildPrompts_DepthCap(t *testing.T) {
	parent := jsonl(t,
		userEntry("u1", "Go deep", "2026-05-10T10:00:00Z"),
		assistantEntry("a1", "claude-opus-4-7", "2026-05-10T10:00:01Z", 1, 1,
			map[string]any{"type": "tool_use", "id": "t1", "name": "Task",
				"input": map[string]any{"description": "L1", "subagent_type": "x"}},
		),
		toolResultUser("u2", "2026-05-10T10:00:02Z", "t1", "ok", "ag1"),
	)
	fsys := fstest.MapFS{
		".claude/projects/p/sess.jsonl": recentFile(t, parent),
	}
	// Build a chain: parent → ag1 → ag2 → ag3 → ag4 → ag5 → ag6.
	chainParents := []string{"sess", "ag1", "ag2", "ag3", "ag4", "ag5"}
	chainSelves := []string{"ag1", "ag2", "ag3", "ag4", "ag5", "ag6"}
	chainSpawnsNext := []bool{true, true, true, true, true, false}
	for i, self := range chainSelves {
		entries := []any{
			map[string]any{
				"type": "assistant", "uuid": "x" + self, "sessionId": chainParents[i], "isSidechain": true,
				"timestamp": fmt.Sprintf("2026-05-10T10:00:%02dZ", 10+i),
				"message": map[string]any{"role": "assistant", "id": "m", "model": "claude-opus-4-7",
					"stop_reason": "end_turn",
					"usage":       map[string]any{"input_tokens": 1, "output_tokens": 1},
					"content":     []map[string]any{{"type": "text", "text": "ok"}}}},
		}
		if chainSpawnsNext[i] {
			next := chainSelves[i+1]
			entries = append(entries,
				map[string]any{
					"type": "assistant", "uuid": "spawn_" + self, "sessionId": chainParents[i], "isSidechain": true,
					"timestamp": fmt.Sprintf("2026-05-10T10:01:%02dZ", 10+i),
					"message": map[string]any{"role": "assistant", "id": "m", "model": "claude-opus-4-7",
						"stop_reason": "end_turn",
						"usage":       map[string]any{"input_tokens": 0, "output_tokens": 0},
						"content": []map[string]any{{
							"type": "tool_use", "id": "tt_" + self, "name": "Task",
							"input": map[string]any{"description": "next", "subagent_type": "x"},
						}}}},
				map[string]any{
					"type": "user", "uuid": "tr_" + self, "sessionId": chainParents[i], "isSidechain": true,
					"timestamp":       fmt.Sprintf("2026-05-10T10:02:%02dZ", 10+i),
					"isMeta":          true,
					"sourceToolUseID": "tt_" + self,
					"toolUseResult":   map[string]any{"agentId": next},
					"message": map[string]any{"role": "user", "content": []map[string]any{
						{"type": "tool_result", "tool_use_id": "tt_" + self, "content": "ok"},
					}}})
		}
		fsys[fmt.Sprintf(".claude/projects/p/%s/subagents/agent-%s.jsonl", chainParents[i], self)] = recentFile(t, jsonl(t, entries...))
	}

	rows := buildPrompts(context.Background(), fsys, prompts.Options{Since: time.Now().Add(-30 * 24 * time.Hour)})
	if len(rows) != 1 {
		t.Fatalf("rows: %d", len(rows))
	}

	// Walk the tree and verify depth is capped at 5.
	depth := 1
	cur := rows[0].Subagents
	for len(cur) > 0 {
		depth++
		cur = cur[0].Subagents
	}
	// depth here counts the parent prompt as level 1, so a fully
	// resolved tree has depth = 1 (parent) + 5 (subagents) = 6.
	if depth != 1+maxSubagentDepth {
		t.Errorf("depth: got %d want %d", depth, 1+maxSubagentDepth)
	}
}

// ---------------------------------------------------------------------------
// Malformed JSONL is skipped, not fatal
// ---------------------------------------------------------------------------

func TestBuildPrompts_MalformedLineSkipped(t *testing.T) {
	good := jsonl(t,
		userEntry("u1", "Hi", "2026-05-10T10:00:00Z"),
		assistantEntry("a1", "claude-opus-4-7", "2026-05-10T10:00:01Z", 4, 1),
	)
	mixed := append([]byte("{not json\n"), good...)
	fsys := fstest.MapFS{
		".claude/projects/p/sess.jsonl": recentFile(t, mixed),
	}
	rows := buildPrompts(context.Background(), fsys, prompts.Options{Since: time.Now().Add(-30 * 24 * time.Hour)})
	if len(rows) != 1 {
		t.Fatalf("rows: %d", len(rows))
	}
}

// ---------------------------------------------------------------------------
// Ongoing prompt (no assistant turn) is dropped
// ---------------------------------------------------------------------------

func TestBuildPrompts_OngoingDropped(t *testing.T) {
	session := jsonl(t,
		userEntry("u1", "Hello", "2026-05-10T10:00:00Z"),
	)
	fsys := fstest.MapFS{
		".claude/projects/p/sess.jsonl": recentFile(t, session),
	}
	rows := buildPrompts(context.Background(), fsys, prompts.Options{Since: time.Now().Add(-30 * 24 * time.Hour)})
	if len(rows) != 0 {
		t.Fatalf("expected 0 rows, got %d", len(rows))
	}
}

// ---------------------------------------------------------------------------
// System-reminder-only user entries don't start a chunk
// ---------------------------------------------------------------------------

func TestBuildPrompts_FilteredUserInputSkipped(t *testing.T) {
	// System-reminder-only user entry — not a chunk starter — followed
	// by an assistant entry. With no real chunk start, we should emit
	// nothing.
	session := jsonl(t,
		userEntry("u1", "<system-reminder>foo</system-reminder>", "2026-05-10T10:00:00Z"),
		assistantEntry("a1", "claude-opus-4-7", "2026-05-10T10:00:01Z", 4, 1),
	)
	fsys := fstest.MapFS{
		".claude/projects/p/sess.jsonl": recentFile(t, session),
	}
	rows := buildPrompts(context.Background(), fsys, prompts.Options{Since: time.Now().Add(-30 * 24 * time.Hour)})
	if len(rows) != 0 {
		t.Fatalf("expected 0 rows, got %d", len(rows))
	}
}

// ---------------------------------------------------------------------------
// Files older than the window are skipped
// ---------------------------------------------------------------------------

func TestBuildPrompts_OldFileSkipped(t *testing.T) {
	session := jsonl(t,
		userEntry("u1", "old", "2026-05-10T10:00:00Z"),
		assistantEntry("a1", "claude-opus-4-7", "2026-05-10T10:00:01Z", 4, 1),
	)
	fsys := fstest.MapFS{
		".claude/projects/p/sess.jsonl": {Data: session, ModTime: time.Now().Add(-60 * 24 * time.Hour)},
	}
	rows := buildPrompts(context.Background(), fsys, prompts.Options{Since: time.Now().Add(-30 * 24 * time.Hour)})
	if len(rows) != 0 {
		t.Fatalf("expected 0 rows, got %d", len(rows))
	}
}

// ---------------------------------------------------------------------------
// chunkID stability
// ---------------------------------------------------------------------------

func TestChunkID_Stable(t *testing.T) {
	a := chunkID("sess", "uuid")
	b := chunkID("sess", "uuid")
	if a != b || a == "" {
		t.Errorf("unstable chunkID: %q vs %q", a, b)
	}
	if chunkID("sess", "uuid") == chunkID("sess2", "uuid") {
		t.Error("chunkID collision across sessions")
	}
}

// ---------------------------------------------------------------------------
// Ranking integration — top-K trims to opts.TopK
// ---------------------------------------------------------------------------

func TestScanner_TopPrompts_HonorsTopK(t *testing.T) {
	// Three sessions, varying token totals. TopK=2 should keep the two
	// largest.
	mk := func(n int64) []byte {
		return jsonl(t,
			userEntry(fmt.Sprintf("u_%d", n), fmt.Sprintf("size %d", n), "2026-05-10T10:00:00Z"),
			assistantEntry("a1", "claude-opus-4-7", "2026-05-10T10:00:01Z", n, 1),
		)
	}
	fsys := fstest.MapFS{
		".claude/projects/p/big.jsonl":    recentFile(t, mk(1000)),
		".claude/projects/p/medium.jsonl": recentFile(t, mk(500)),
		".claude/projects/p/small.jsonl":  recentFile(t, mk(100)),
	}
	s := scanner{}
	rows, err := s.TopPrompts(context.Background(), prompts.Options{
		HomeFS: fsys,
		Since:  time.Now().Add(-30 * 24 * time.Hour),
		TopK:   2,
	})
	if err != nil {
		t.Fatalf("TopPrompts: %v", err)
	}
	if len(rows) != 2 {
		t.Fatalf("rows: got %d want 2", len(rows))
	}
	if rows[0].MainMetrics.InputTokens != 1000 || rows[1].MainMetrics.InputTokens != 500 {
		t.Errorf("ranking wrong: %d, %d",
			rows[0].MainMetrics.InputTokens,
			rows[1].MainMetrics.InputTokens)
	}
}

// ---------------------------------------------------------------------------
// helpers
// ---------------------------------------------------------------------------

func equalToolCalls(a, b []types.DeviceScanPromptToolCall) bool {
	if len(a) != len(b) {
		return false
	}
	ac := append([]types.DeviceScanPromptToolCall(nil), a...)
	bc := append([]types.DeviceScanPromptToolCall(nil), b...)
	less := func(s []types.DeviceScanPromptToolCall) func(i, j int) bool {
		return func(i, j int) bool {
			if s[i].Count != s[j].Count {
				return s[i].Count > s[j].Count
			}
			return s[i].Name < s[j].Name
		}
	}
	sort.Slice(ac, less(ac))
	sort.Slice(bc, less(bc))
	for i := range ac {
		if ac[i] != bc[i] {
			return false
		}
	}
	return true
}
