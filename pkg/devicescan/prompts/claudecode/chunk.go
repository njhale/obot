package claudecode

import (
	"encoding/json"
	"io/fs"
	"time"

	"github.com/obot-platform/obot/apiclient/types"
)

// taskInvocation captures a single parent-session Task tool_use call —
// the bridge between a chunk and the subagent it spawns. CallTokens
// and ResultTokens are estimates produced from JSON byte lengths via
// the same 4-chars-per-token heuristic claude-devtools uses for its
// MetricsPill display, since the real Anthropic API doesn't break
// usage out by tool call.
type taskInvocation struct {
	ID           string
	Description  string
	SubagentType string
	CallTokens   int64
	ResultTokens int64
}

// chunk is one prompt-aligned view of a parent session: the user turn
// and every downstream entry up to (but not including) the next real
// user turn.
type chunk struct {
	SessionID          string
	SessionFile        string
	StartedAt          time.Time
	EndedAt            time.Time
	UserText           string
	Cwd                string
	GitBranch          string
	Model              string
	FirstAssistantUUID string
	HasCompletedTurn   bool

	MainMetrics types.DeviceScanPromptMetrics
	ToolCalls   *toolCallCounter

	Tasks     []*taskInvocation
	TasksByID map[string]*taskInvocation
	// AgentToTask maps a sidechain `agentId` (from a Task
	// tool_result's `toolUseResult.agentId` field) back to the
	// parent-session tool_use ID that invoked it. The subagent
	// resolver uses this to attribute subagent files to specific Task
	// calls; mirrors claude-devtools' linkToTaskCalls phase-1 logic.
	AgentToTask map[string]string
}

// chunkSession parses one main-session JSONL file from fsys and emits
// finalized chunks. Chunks with no completed assistant turn are
// dropped (they have no rankable token cost). The function is
// best-effort: parse errors on individual lines are skipped silently
// (see scanEntries' contract).
func chunkSession(fsys fs.FS, sessionFile, sessionID string) ([]*chunk, error) {
	var (
		current *chunk
		chunks  []*chunk
		lastTS  time.Time
	)

	err := scanEntries(fsys, sessionFile, func(e entry) error {
		// Skip everything that originated on a sidechain (subagent
		// activity) from the parent-session view — those entries are
		// folded back in by the subagent resolver.
		if e.IsSidechain {
			return nil
		}

		if text, ok := isRealUserChunkStart(e); ok {
			finalizeChunk(current, lastTS)
			current = newChunk(sessionFile, sessionID, e, text)
			chunks = append(chunks, current)
			if !e.Timestamp.IsZero() {
				lastTS = e.Timestamp
			}
			return nil
		}

		if current == nil {
			// Pre-first-user entries (system init, queue ops, etc.)
			// have no chunk to attach to.
			return nil
		}

		if !e.Timestamp.IsZero() {
			lastTS = e.Timestamp
		}

		switch e.Type {
		case entryAssistant:
			absorbAssistant(current, e)
		case entryUser:
			absorbUserFlow(current, e)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}

	finalizeChunk(current, lastTS)

	// Drop chunks with no completed assistant turn — they have nothing
	// to rank.
	out := chunks[:0]
	for _, c := range chunks {
		if c.HasCompletedTurn {
			out = append(out, c)
		}
	}
	return out, nil
}

func newChunk(sessionFile, sessionID string, e entry, text string) *chunk {
	return &chunk{
		SessionID:   sessionID,
		SessionFile: sessionFile,
		StartedAt:   e.Timestamp,
		UserText:    text,
		Cwd:         e.Cwd,
		GitBranch:   e.GitBranch,
		ToolCalls:   newToolCallCounter(),
		TasksByID:   map[string]*taskInvocation{},
		AgentToTask: map[string]string{},
	}
}

// absorbAssistant folds one assistant entry into the chunk: usage,
// model, first-assistant uuid, tool-call counters, and Task tracking.
func absorbAssistant(c *chunk, e entry) {
	if e.Message == nil {
		return
	}
	if c.FirstAssistantUUID == "" {
		c.FirstAssistantUUID = e.UUID
	}
	if c.Model == "" {
		c.Model = e.Message.Model
	}
	addUsage(&c.MainMetrics, e.Message.Usage)
	if e.Message.StopReason != "" {
		c.HasCompletedTurn = true
	}

	for _, b := range e.Message.Content.Blocks {
		if b.Type != "tool_use" {
			continue
		}
		c.ToolCalls.add(b.Name)
		// Subagent-spawning tools have evolved by name across Claude
		// Code versions ("Task" historically, "Agent" currently). The
		// stable discriminator is a `subagent_type` field in the
		// tool_use input; anything else is a regular tool call.
		subagentType := jsonString(b.Input, "subagent_type")
		if subagentType == "" {
			continue
		}
		t := &taskInvocation{
			ID:           b.ID,
			CallTokens:   estimateTokens(len(b.Name) + len(b.Input)),
			Description:  jsonString(b.Input, "description"),
			SubagentType: subagentType,
		}
		c.Tasks = append(c.Tasks, t)
		if b.ID != "" {
			c.TasksByID[b.ID] = t
		}
	}
}

// absorbUserFlow handles "internal" user entries — tool_results that
// follow assistant tool_use blocks. The chunk's HasCompletedTurn flag
// is also set here for tool-driven turns whose assistants reported no
// explicit stop_reason but did emit a downstream tool_result.
func absorbUserFlow(c *chunk, e entry) {
	if e.Message == nil {
		// No message means no content blocks to walk — but we may
		// still have an envelope-level toolUseResult to extract. The
		// agent linking below needs a task ID source; without
		// message.content[].tool_use_id we fall back to
		// sourceToolUseID.
		if agentID := e.ToolResult.agent(); agentID != "" && e.SourceToolUseID != "" {
			c.AgentToTask[agentID] = e.SourceToolUseID
		}
		return
	}
	c.HasCompletedTurn = true
	agentID := e.ToolResult.agent()
	for _, b := range e.Message.Content.Blocks {
		if b.Type != "tool_result" {
			continue
		}
		// Real-world Claude Code entries rarely surface
		// sourceToolUseID; the canonical link is the inner
		// content block's tool_use_id, which always matches the
		// originating assistant tool_use ID. Prefer that, falling
		// back to the envelope field for older sessions.
		taskID := b.ToolUseID
		if taskID == "" {
			taskID = e.SourceToolUseID
		}
		if agentID != "" && taskID != "" {
			c.AgentToTask[agentID] = taskID
		}
		if t, ok := c.TasksByID[taskID]; ok {
			t.ResultTokens += estimateTokens(len(b.Content))
		}
	}
}

func finalizeChunk(c *chunk, lastTS time.Time) {
	if c == nil {
		return
	}
	if !lastTS.IsZero() {
		c.EndedAt = lastTS
	}
	sealMetrics(&c.MainMetrics)
}

// jsonString plucks a top-level string field from a JSON object
// embedded as raw bytes. Returns "" on any error (missing field, wrong
// type, malformed JSON). Used to extract `description` /
// `subagent_type` from Task tool_use inputs without committing to a
// fixed schema.
func jsonString(b []byte, field string) string {
	if len(b) == 0 {
		return ""
	}
	var m map[string]json.RawMessage
	if err := json.Unmarshal(b, &m); err != nil {
		return ""
	}
	raw, ok := m[field]
	if !ok {
		return ""
	}
	var s string
	if err := json.Unmarshal(raw, &s); err != nil {
		return ""
	}
	return s
}
