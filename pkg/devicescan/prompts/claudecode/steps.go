package claudecode

import (
	"bytes"
	"encoding/json"
	"fmt"
	"time"

	"github.com/obot-platform/obot/apiclient/types"
	"github.com/obot-platform/obot/pkg/devicescan/prompts"
)

// MaxStepsPerPrompt is the server-enforced cap on a single prompt's
// timeline (DESIGN.md M2 Phase 0). The CLI truncates and logs a
// warning rather than emitting something the server will reject.
const MaxStepsPerPrompt = 2000

// stepBuilder accumulates an ordered list of steps for one
// (main-context or single-subagent-context) timeline as a chunker
// streams entries through the parser. It owns the per-turn token
// proportioning so a single assistant turn's output_tokens are split
// across its content blocks by byte size.
type stepBuilder struct {
	context    string // "main" or "subagent"
	subagentID string // empty for "main"; node id for "subagent"
	out        []types.DeviceScanPromptStep
	bridges    []taskBridge
}

func newStepBuilder(context, subagentID string) *stepBuilder {
	return &stepBuilder{context: context, subagentID: subagentID}
}

// addUser emits one `user` step at chunk start.
func (b *stepBuilder) addUser(e entry, body string) {
	head, full, hash := prompts.TruncateContent(body, prompts.MaxStepHeadBytes)
	b.out = append(b.out, types.DeviceScanPromptStep{
		Kind:       "user",
		Context:    b.context,
		SubagentID: b.subagentID,
		StartedAt:  types.Time{Time: e.Timestamp},
		TextHead:   head,
		TextBytes:  full,
		TextHash:   hash,
	})
}

// addAssistant emits one step per content block in the assistant turn
// e, proportioning the turn's output_tokens across the blocks by byte
// size. Input / cache tokens are charged to the first block. Records
// any Task tool_use blocks as taskBridges so the build layer can pair
// each with a synthetic subagent_call step once the subagent tree
// resolves and assigns a SubagentID.
func (b *stepBuilder) addAssistant(e entry) {
	if e.Message == nil || len(e.Message.Content.Blocks) == 0 {
		return
	}
	usage := e.Message.Usage
	blocks := e.Message.Content.Blocks
	allocs := proportionOutput(blocks, usage)

	firstTokenSlot := -1
	for i, blk := range blocks {
		st := types.DeviceScanPromptStep{
			Context:    b.context,
			SubagentID: b.subagentID,
			StartedAt:  types.Time{Time: e.Timestamp},
			Tokens:     types.DeviceScanPromptStepTokens{Output: allocs[i]},
		}
		switch blk.Type {
		case "text":
			st.Kind = "text"
			st.TextHead, st.TextBytes, st.TextHash =
				prompts.TruncateContent(blk.Text, prompts.MaxStepHeadBytes)
		case "thinking":
			st.Kind = "thinking"
			st.TextHead, st.TextBytes, st.TextHash =
				prompts.TruncateContent(blk.Thinking, prompts.MaxStepHeadBytes)
		case "tool_use":
			st.Kind = "tool_use"
			st.ToolUseID = blk.ID
			st.ToolName = blk.Name
			st.ToolInputKeys = topLevelKeys(blk.Input)
		case "image":
			st.Kind = "text"
			st.TextHead = imagePlaceholder(blk.Source)
			// No bytes / hash — placeholder is synthetic and the
			// original payload (base64) deliberately never ships.
		default:
			// Unknown / tool_result inside an assistant turn: skip.
			continue
		}
		if firstTokenSlot < 0 {
			firstTokenSlot = len(b.out)
		}
		b.out = append(b.out, st)
		if st.Kind == "tool_use" && jsonString(blk.Input, "subagent_type") != "" {
			b.bridges = append(b.bridges, taskBridge{
				ToolUseID:   blk.ID,
				Description: jsonString(blk.Input, "description"),
				StartedAt:   e.Timestamp,
				Context:     b.context,
				SubagentID:  b.subagentID,
			})
		}
	}
	// Charge input / cache to the first emitted step of this turn so
	// AccumulatedContextTokens picks them up once.
	if usage != nil && firstTokenSlot >= 0 {
		b.out[firstTokenSlot].Tokens.Input = usage.InputTokens
		b.out[firstTokenSlot].Tokens.CacheRead = usage.CacheReadInputTokens
		b.out[firstTokenSlot].Tokens.CacheCreation = usage.CacheCreationInputTokens
	}
}

// addToolResult emits one `tool_result` step per tool_result block in
// the user entry e. Falls back to the envelope-level sourceToolUseID
// when a block lacks tool_use_id.
func (b *stepBuilder) addToolResult(e entry) {
	if e.Message == nil {
		return
	}
	for _, blk := range e.Message.Content.Blocks {
		if blk.Type != "tool_result" {
			continue
		}
		ref := blk.ToolUseID
		if ref == "" {
			ref = e.SourceToolUseID
		}
		body := toolResultContentToString(blk.Content)
		head, full, hash := prompts.TruncateContent(body, prompts.MaxStepHeadBytes)
		b.out = append(b.out, types.DeviceScanPromptStep{
			Kind:       "tool_result",
			Context:    b.context,
			SubagentID: b.subagentID,
			StartedAt:  types.Time{Time: e.Timestamp},
			ToolUseRef: ref,
			IsError:    blk.IsError,
			TextHead:   head,
			TextBytes:  full,
			TextHash:   hash,
		})
	}
}

// taskBridge ties one Task tool_use step (already emitted on its
// timeline) to the synthetic subagent_call step that should follow it
// once the subagent resolver has produced a real SubagentID. Context
// + SubagentID describe the timeline the bridge belongs to (main or a
// specific subagent — Tasks can be nested).
type taskBridge struct {
	ToolUseID   string
	Description string
	StartedAt   time.Time
	Context     string
	SubagentID  string
}

// proportionOutput splits usage.OutputTokens across the assistant
// turn's content blocks by byte size. Sizes that fall to zero (image
// blocks, missing thinking, etc.) still receive a remainder share when
// every size is zero so a turn never silently loses its output tokens.
func proportionOutput(blocks []contentBlock, u *usage) []int64 {
	out := make([]int64, len(blocks))
	if u == nil || u.OutputTokens <= 0 {
		return out
	}
	sizes := make([]int64, len(blocks))
	var total int64
	for i, b := range blocks {
		sizes[i] = blockSize(b)
		total += sizes[i]
	}
	if total == 0 {
		share := u.OutputTokens / int64(len(blocks))
		for i := range out {
			out[i] = share
		}
		out[0] += u.OutputTokens - share*int64(len(blocks))
		return out
	}
	var assigned int64
	for i, sz := range sizes {
		out[i] = u.OutputTokens * sz / total
		assigned += out[i]
	}
	if r := u.OutputTokens - assigned; r != 0 {
		// Drop the remainder onto the largest block (deterministic;
		// ties resolve to the earliest such block).
		bestI := 0
		bestSz := int64(-1)
		for i, sz := range sizes {
			if sz > bestSz {
				bestI, bestSz = i, sz
			}
		}
		out[bestI] += r
	}
	return out
}

func blockSize(b contentBlock) int64 {
	switch b.Type {
	case "text":
		return int64(len(b.Text))
	case "thinking":
		return int64(len(b.Thinking))
	case "tool_use":
		return int64(len(b.Input))
	default:
		return 0
	}
}

// topLevelKeys returns the top-level keys of a JSON object in
// declaration order — empty when input is not an object. Keys ship
// without their values under the privacy ratchet (same redaction
// pattern as EnvKeys on DeviceScanMCPServer).
func topLevelKeys(raw json.RawMessage) []string {
	if len(raw) == 0 {
		return nil
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	t, err := dec.Token()
	if err != nil || t != json.Delim('{') {
		return nil
	}
	var keys []string
	for dec.More() {
		k, err := dec.Token()
		if err != nil {
			return keys
		}
		s, ok := k.(string)
		if !ok {
			return keys
		}
		keys = append(keys, s)
		var skip json.RawMessage
		if err := dec.Decode(&skip); err != nil {
			return keys
		}
	}
	return keys
}

// imagePlaceholder renders the synthetic [image: <media_type>, <bytes>]
// head. bytes is the decoded size of the base64 payload (≈ 3/4 of the
// base64 length). When the source is malformed or absent the
// placeholder falls back to a bare type tag so admins still see that
// an image block existed.
func imagePlaceholder(src imageSource) string {
	if src.MediaType == "" && src.Data == "" {
		return "[image]"
	}
	mt := src.MediaType
	if mt == "" {
		mt = "image"
	}
	approxBytes := len(src.Data) * 3 / 4
	return fmt.Sprintf("[image: %s, %d bytes]", mt, approxBytes)
}

// toolResultContentToString flattens a tool_result block's content
// field to a string for redacted head extraction. Matches
// claude-devtools' rendering: a JSON-string value is unquoted; an
// array (or any other JSON value) is rendered as its raw JSON bytes.
func toolResultContentToString(raw json.RawMessage) string {
	bs := bytes.TrimSpace([]byte(raw))
	if len(bs) == 0 || string(bs) == "null" {
		return ""
	}
	if bs[0] == '"' {
		var s string
		if err := json.Unmarshal(bs, &s); err == nil {
			return s
		}
	}
	return string(bs)
}

// accumulateContextTokens runs a single pass over steps and fills in
// each step's AccumulatedContextTokens — the running sum of
// Input + CacheRead + CacheCreation across the step's context (main
// timeline or a single subagent timeline). Subagent contexts keep
// independent running sums keyed by SubagentID; the main context's
// sum is keyed by the empty string.
func accumulateContextTokens(steps []types.DeviceScanPromptStep) {
	sums := make(map[string]int64)
	for i := range steps {
		key := contextKey(steps[i])
		sums[key] += steps[i].Tokens.Input + steps[i].Tokens.CacheRead + steps[i].Tokens.CacheCreation
		steps[i].AccumulatedContextTokens = sums[key]
	}
}

func contextKey(s types.DeviceScanPromptStep) string {
	if s.Context == "subagent" {
		return "s:" + s.SubagentID
	}
	return "m"
}
