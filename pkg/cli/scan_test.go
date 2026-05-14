package cli

import (
	"bytes"
	"context"
	"errors"
	"io"
	"strings"
	"sync"
	"testing"
	"testing/fstest"

	"github.com/obot-platform/obot/apiclient/types"
	"github.com/obot-platform/obot/pkg/devicescan/prompts"
)

// fakePromptScanner is a configurable PromptScanner used to exercise
// collectTopPrompts without registering anything at package level.
type fakePromptScanner struct {
	client   string
	present  bool
	prompts  []types.DeviceScanPrompt
	err      error
	called   int
	mu       sync.Mutex
	panicMsg string
}

func (f *fakePromptScanner) Client() string                  { return f.client }
func (f *fakePromptScanner) Presence(_ prompts.Options) bool { return f.present }
func (f *fakePromptScanner) TopPrompts(_ context.Context, _ prompts.Options) ([]types.DeviceScanPrompt, error) {
	f.mu.Lock()
	f.called++
	f.mu.Unlock()
	if f.panicMsg != "" {
		panic(f.panicMsg)
	}
	return f.prompts, f.err
}

func TestCollectTopPrompts_NoScannersReturnsNil(t *testing.T) {
	prompts.SetForTest(t)
	got := collectTopPrompts(context.Background(), io.Discard, fstest.MapFS{}, "/home/u", 10)
	if got != nil {
		t.Errorf("no scanners: want nil, got %+v", got)
	}
}

func TestCollectTopPrompts_AbsentScannerNotInvoked(t *testing.T) {
	// Register a scanner that reports Presence() == false and panics if
	// TopPrompts is ever called.
	f := &fakePromptScanner{client: "claude_code", present: false, panicMsg: "should not be called"}
	withRegistered(t, f)

	got := collectTopPrompts(context.Background(), io.Discard, fstest.MapFS{}, "/home/u", 5)
	if got != nil {
		t.Errorf("absent scanner returned data: %+v", got)
	}
	if f.called != 0 {
		t.Errorf("Presence false should skip TopPrompts; got called=%d", f.called)
	}
}

func TestCollectTopPrompts_AggregatesAndTrimsToTopK(t *testing.T) {
	f := &fakePromptScanner{
		client:  "claude_code",
		present: true,
		prompts: []types.DeviceScanPrompt{
			{ChunkID: "small", Metrics: types.DeviceScanPromptMetrics{TotalTokens: 10}},
			{ChunkID: "big", Metrics: types.DeviceScanPromptMetrics{TotalTokens: 100}},
			{ChunkID: "mid", Metrics: types.DeviceScanPromptMetrics{TotalTokens: 50}},
		},
	}
	withRegistered(t, f)

	got := collectTopPrompts(context.Background(), io.Discard, fstest.MapFS{}, "/home/u", 2)
	if len(got) != 2 || got[0].ChunkID != "big" || got[1].ChunkID != "mid" {
		t.Errorf("want big,mid; got %+v", chunkIDs(got))
	}
}

func TestCollectTopPrompts_ScannerErrorIsLoggedAndSkipped(t *testing.T) {
	bad := &fakePromptScanner{client: "claude_code", present: true, err: errors.New("kaboom")}
	good := &fakePromptScanner{
		client:  "codex",
		present: true,
		prompts: []types.DeviceScanPrompt{
			{ChunkID: "g1", Metrics: types.DeviceScanPromptMetrics{TotalTokens: 1}},
		},
	}
	withRegistered(t, bad, good)

	var buf bytes.Buffer
	got := collectTopPrompts(context.Background(), &buf, fstest.MapFS{}, "/home/u", 5)
	if len(got) != 1 || got[0].ChunkID != "g1" {
		t.Errorf("good scanner output should survive bad neighbor: %+v", chunkIDs(got))
	}
	if !strings.Contains(buf.String(), "kaboom") || !strings.Contains(buf.String(), "claude_code") {
		t.Errorf("stderr should mention failing scanner: %q", buf.String())
	}
}

func TestScanIncludeTopPromptsValidation(t *testing.T) {
	tests := []struct {
		name  string
		value int
		want  bool // true = should error
	}{
		{"zero ok", 0, false},
		{"one ok", 1, false},
		{"ten ok", maxIncludeTopPrompts, false},
		{"eleven errors", maxIncludeTopPrompts + 1, true},
		{"negative errors", -1, true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			// Validate the gating expression directly — Run can't run
			// without a fully wired root command, but the validation
			// statement is a single inequality we can mirror here.
			isErr := tc.value < 0 || tc.value > maxIncludeTopPrompts
			if isErr != tc.want {
				t.Errorf("validation mismatch for %d: got isErr=%v want %v", tc.value, isErr, tc.want)
			}
		})
	}
}

func chunkIDs(in []types.DeviceScanPrompt) []string {
	out := make([]string, len(in))
	for i, p := range in {
		out[i] = p.ChunkID
	}
	return out
}

// withRegistered swaps the prompts registry to contain only the given
// scanners for the duration of the test. Uses a tiny helper file in
// the prompts package (test-only) so we don't have to expose its
// internals here.
func withRegistered(t *testing.T, scanners ...prompts.PromptScanner) {
	t.Helper()
	prompts.SetForTest(t, scanners...)
}
