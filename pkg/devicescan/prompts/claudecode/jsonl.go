package claudecode

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"strings"
	"time"
)

// JSONL entry shapes mirror claude-devtools' src/main/types/jsonl.ts.
// Only the fields we actually consume are decoded; everything else is
// dropped so the parser stays bounded in memory.

type entryType string

const (
	entryUser      entryType = "user"
	entryAssistant entryType = "assistant"
)

// entry is one JSONL line. Fields not on every line are zero-valued
// when absent; conditionals on Type below ensure we only read what
// applies.
type entry struct {
	Type            entryType  `json:"type"`
	Timestamp       time.Time  `json:"-"`
	TimestampS      string     `json:"timestamp"`
	UUID            string     `json:"uuid"`
	SessionID       string     `json:"sessionId"`
	IsSidechain     bool       `json:"isSidechain"`
	IsMeta          bool       `json:"isMeta"`
	Cwd             string     `json:"cwd"`
	GitBranch       string     `json:"gitBranch"`
	AgentID         string     `json:"agentId"`
	SourceToolUseID string     `json:"sourceToolUseID"`
	Message         *entryMsg  `json:"message,omitempty"`
	ToolResult      toolResult `json:"toolUseResult"`
}

// entryMsg is the common shape of user and assistant `message` fields.
// Content may be a string (older user messages) or an array of content
// blocks (newer entries; always an array for assistant entries).
type entryMsg struct {
	Role       string         `json:"role"`
	Model      string         `json:"model"`
	ID         string         `json:"id"`
	StopReason string         `json:"stop_reason"`
	Usage      *usage         `json:"usage,omitempty"`
	Content    messageContent `json:"content"`
}

type usage struct {
	InputTokens              int64 `json:"input_tokens"`
	OutputTokens             int64 `json:"output_tokens"`
	CacheReadInputTokens     int64 `json:"cache_read_input_tokens"`
	CacheCreationInputTokens int64 `json:"cache_creation_input_tokens"`
}

// messageContent decodes the dual-shape `message.content` field. A
// JSON string lands in Text; a JSON array lands in Blocks.
type messageContent struct {
	Text   string
	Blocks []contentBlock
}

func (m *messageContent) UnmarshalJSON(b []byte) error {
	t := bytes.TrimSpace(b)
	if len(t) == 0 || bytes.Equal(t, []byte("null")) {
		return nil
	}
	if t[0] == '"' {
		return json.Unmarshal(t, &m.Text)
	}
	return json.Unmarshal(t, &m.Blocks)
}

// contentBlock is one entry in `message.content`. Only the fields we
// consume are decoded; unrecognized blocks are kept as Type with the
// rest discarded.
type contentBlock struct {
	Type      string          `json:"type"`
	Text      string          `json:"text,omitempty"`
	Thinking  string          `json:"thinking,omitempty"`
	ID        string          `json:"id,omitempty"`
	Name      string          `json:"name,omitempty"`
	Input     json.RawMessage `json:"input,omitempty"`
	ToolUseID string          `json:"tool_use_id,omitempty"`
	IsError   bool            `json:"is_error,omitempty"`
	Content   json.RawMessage `json:"content,omitempty"`
	Source    imageSource     `json:"source"`
}

// imageSource is the inner shape of an image block. Only the metadata
// fields are decoded — the base64 `data` is held just long enough to
// compute the original byte count, then discarded. No base64 ever
// reaches the wire under the privacy ratchet.
type imageSource struct {
	Type      string `json:"type,omitempty"`
	MediaType string `json:"media_type,omitempty"`
	Data      string `json:"data,omitempty"`
}

// toolResult is the part of `toolUseResult` we read — agent id fields
// used to link Task tool_use blocks to subagent session files.
type toolResult struct {
	AgentID      string `json:"agentId"`
	AgentIDSnake string `json:"agent_id"`
}

func (t toolResult) agent() string {
	if t.AgentID != "" {
		return t.AgentID
	}
	return t.AgentIDSnake
}

// UnmarshalJSON tolerates `toolUseResult` arriving as either an object
// (the common case) or some non-object value (a bare string like
// "User rejected tool use"). Non-objects are decoded as zero.
func (t *toolResult) UnmarshalJSON(b []byte) error {
	bs := bytes.TrimSpace(b)
	if len(bs) == 0 || bs[0] != '{' {
		return nil
	}
	type alias toolResult
	var a alias
	if err := json.Unmarshal(bs, &a); err != nil {
		return nil // malformed — drop quietly per the parser contract
	}
	*t = toolResult(a)
	return nil
}

// scanEntries streams a JSONL file from fsys, yielding one entry per
// callback. Malformed lines are skipped (warnings emitted by the
// caller via the returned errors channel-less contract: this function
// silently drops). EOF returns nil. The scanner's max line size is
// bumped to 16 MiB to accommodate large pasted prompts and large tool
// inputs/outputs; lines longer than that are skipped.
func scanEntries(fsys fs.FS, name string, visit func(e entry) error) error {
	f, err := fsys.Open(name)
	if err != nil {
		return err
	}
	defer f.Close()

	sc := bufio.NewScanner(f)
	const maxLine = 16 * 1024 * 1024
	buf := make([]byte, 0, 64*1024)
	sc.Buffer(buf, maxLine)

	for sc.Scan() {
		line := bytes.TrimSpace(sc.Bytes())
		if len(line) == 0 || line[0] != '{' {
			continue
		}
		var e entry
		if err := json.Unmarshal(line, &e); err != nil {
			continue
		}
		if e.TimestampS != "" {
			if ts, err := parseTimestamp(e.TimestampS); err == nil {
				e.Timestamp = ts
			}
		}
		if err := visit(e); err != nil {
			return err
		}
	}
	if err := sc.Err(); err != nil {
		// bufio.ErrTooLong means we hit a single line >maxLine — treat
		// as a soft skip rather than failing the whole file.
		if !errors.Is(err, bufio.ErrTooLong) {
			return fmt.Errorf("scan: %w", err)
		}
	}
	return nil
}

func parseTimestamp(s string) (time.Time, error) {
	// Claude Code writes RFC3339 with millisecond fractions ("Z" suffix).
	// time.RFC3339Nano handles both.
	return time.Parse(time.RFC3339Nano, s)
}

// systemOutputTags mirrors claude-devtools' SYSTEM_OUTPUT_TAGS. A user
// entry starting with any of these is treated as system output, not
// real user input.
var systemOutputTags = []string{
	"<local-command-stdout>",
	"<local-command-stderr>",
	"<local-command-caveat>",
	"<system-reminder>",
}

// isRealUserChunkStart implements claude-devtools'
// isParsedUserChunkMessage: type=user, not meta, has text/image
// content, and the text doesn't start with a system-output tag.
// Slash commands (<command-name>) ARE real user input.
//
// It also extracts the joined text payload of the user turn so callers
// can hash/truncate it without re-walking the blocks.
func isRealUserChunkStart(e entry) (text string, ok bool) {
	if e.Type != entryUser || e.IsMeta || e.Message == nil {
		return "", false
	}

	if e.Message.Content.Text != "" {
		t := strings.TrimSpace(e.Message.Content.Text)
		if t == "" {
			return "", false
		}
		for _, tag := range systemOutputTags {
			if strings.HasPrefix(t, tag) {
				return "", false
			}
		}
		return e.Message.Content.Text, true
	}

	if len(e.Message.Content.Blocks) == 0 {
		return "", false
	}

	// Array form: must have at least one text/image block, none of
	// which start with a system-output tag, and not be the single
	// "[Request interrupted by user…]" interruption shape.
	hasUserContent := false
	var parts []string
	for _, b := range e.Message.Content.Blocks {
		if b.Type == "text" || b.Type == "image" {
			hasUserContent = true
		}
		if b.Type == "text" {
			for _, tag := range systemOutputTags {
				if strings.HasPrefix(b.Text, tag) {
					return "", false
				}
			}
			parts = append(parts, b.Text)
		}
	}
	if !hasUserContent {
		return "", false
	}
	if len(e.Message.Content.Blocks) == 1 {
		only := e.Message.Content.Blocks[0]
		if only.Type == "text" && strings.HasPrefix(only.Text, "[Request interrupted by user") {
			return "", false
		}
	}
	return strings.Join(parts, "\n"), true
}

// readFirstLine returns the trimmed first non-empty JSON line of a
// JSONL file. Used to peek a sidechain file's sessionId / agentId
// without parsing the whole file.
func readFirstLine(fsys fs.FS, name string) (entry, error) {
	f, err := fsys.Open(name)
	if err != nil {
		return entry{}, err
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64*1024), 16*1024*1024)
	for sc.Scan() {
		line := bytes.TrimSpace(sc.Bytes())
		if len(line) == 0 || line[0] != '{' {
			continue
		}
		var e entry
		if err := json.Unmarshal(line, &e); err != nil {
			return entry{}, err
		}
		if e.TimestampS != "" {
			if ts, err := parseTimestamp(e.TimestampS); err == nil {
				e.Timestamp = ts
			}
		}
		return e, nil
	}
	if err := sc.Err(); err != nil {
		return entry{}, err
	}
	return entry{}, io.EOF
}
