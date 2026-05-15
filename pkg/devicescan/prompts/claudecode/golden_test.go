package claudecode

import (
	"context"
	"encoding/json"
	"flag"
	"os"
	"path/filepath"
	"testing"
	"testing/fstest"
	"time"

	"github.com/obot-platform/obot/pkg/devicescan/prompts"
)

// updateGolden regenerates testdata/*.json from the current
// implementation. Off by default — run `go test -run TestGolden -update`
// to refresh after an intentional shape change. CI never sees this
// flag, so unintentional drift surfaces as a normal test failure.
var updateGolden = flag.Bool("update", false, "regenerate golden files")

// TestGolden_SimpleSession freezes the wire shape for one chunk with
// a single assistant turn that ships text + a tool_use + a matching
// tool_result. The fixture is deliberately deterministic so drift in
// step ordering, token attribution, or field layout shows up in the
// golden diff.
func TestGolden_SimpleSession(t *testing.T) {
	session := jsonl(t,
		userEntry("u1", "Read foo.txt and summarize it.", "2026-05-10T10:00:00Z"),
		assistantEntry("a1", "claude-opus-4-7", "2026-05-10T10:00:01Z", 100, 20,
			map[string]any{"type": "text", "text": "I'll read the file."},
			map[string]any{"type": "tool_use", "id": "t1", "name": "Read", "input": map[string]any{"path": "foo.txt"}},
		),
		toolResultUser("u2", "2026-05-10T10:00:02Z", "t1", "hello world", ""),
		assistantEntry("a2", "claude-opus-4-7", "2026-05-10T10:00:03Z", 110, 5,
			map[string]any{"type": "text", "text": "It says hello world."},
		),
	)
	fsys := fstest.MapFS{".claude/projects/p/sess.jsonl": recentFile(t, session)}
	rows := buildPrompts(context.Background(), fsys, prompts.Options{Since: time.Now().Add(-30 * 24 * time.Hour)})
	if len(rows) != 1 {
		t.Fatalf("rows: %d", len(rows))
	}

	got, err := json.MarshalIndent(rows[0].Steps, "", "  ")
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	got = append(got, '\n')

	goldenPath := filepath.Join("testdata", "simple_session_steps.json")
	if *updateGolden {
		if err := os.WriteFile(goldenPath, got, 0644); err != nil {
			t.Fatalf("write golden: %v", err)
		}
		t.Logf("wrote %s", goldenPath)
		return
	}
	want, err := os.ReadFile(goldenPath)
	if err != nil {
		t.Fatalf("read golden: %v (run with -update to create)", err)
	}
	if string(got) != string(want) {
		t.Errorf("steps JSON drifted from %s.\nRun:\n  go test ./pkg/devicescan/prompts/claudecode/... -run TestGolden -update\nto refresh after an intentional change.\n--- got ---\n%s\n--- want ---\n%s", goldenPath, got, want)
	}
}
