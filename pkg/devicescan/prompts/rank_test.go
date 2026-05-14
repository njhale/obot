package prompts

import (
	"testing"
	"time"

	"github.com/obot-platform/obot/apiclient/types"
)

func mkPrompt(chunkID string, total int64, ended time.Time) types.DeviceScanPrompt {
	return types.DeviceScanPrompt{
		ChunkID: chunkID,
		EndedAt: *types.NewTime(ended),
		Metrics: types.DeviceScanPromptMetrics{TotalTokens: total},
	}
}

func TestTopK_OrderingAndLimit(t *testing.T) {
	now := time.Unix(1_700_000_000, 0).UTC()
	in := []types.DeviceScanPrompt{
		mkPrompt("low", 100, now.Add(-3*time.Minute)),
		mkPrompt("high", 900, now.Add(-2*time.Minute)),
		mkPrompt("mid", 500, now.Add(-1*time.Minute)),
	}
	got := TopK(in, 2)
	if len(got) != 2 {
		t.Fatalf("len: want 2, got %d", len(got))
	}
	if got[0].ChunkID != "high" || got[1].ChunkID != "mid" {
		t.Errorf("order: got %q,%q want high,mid", got[0].ChunkID, got[1].ChunkID)
	}
}

func TestTopK_TieBrokenByEndedAtDesc(t *testing.T) {
	now := time.Unix(1_700_000_000, 0).UTC()
	in := []types.DeviceScanPrompt{
		mkPrompt("old", 100, now.Add(-3*time.Minute)),
		mkPrompt("new", 100, now.Add(-1*time.Minute)),
		mkPrompt("mid", 100, now.Add(-2*time.Minute)),
	}
	got := TopK(in, 3)
	want := []string{"new", "mid", "old"}
	for i, w := range want {
		if got[i].ChunkID != w {
			t.Errorf("tie-break[%d]: got %q want %q", i, got[i].ChunkID, w)
		}
	}
}

func TestTopK_StableForFullTies(t *testing.T) {
	// Identical tokens + identical endedAt — must preserve input order.
	now := time.Unix(1_700_000_000, 0).UTC()
	in := []types.DeviceScanPrompt{
		mkPrompt("a", 100, now),
		mkPrompt("b", 100, now),
		mkPrompt("c", 100, now),
	}
	got := TopK(in, 3)
	want := []string{"a", "b", "c"}
	for i, w := range want {
		if got[i].ChunkID != w {
			t.Errorf("stability[%d]: got %q want %q", i, got[i].ChunkID, w)
		}
	}
}

func TestTopK_KZeroReturnsNil(t *testing.T) {
	in := []types.DeviceScanPrompt{mkPrompt("a", 1, time.Now())}
	if got := TopK(in, 0); got != nil {
		t.Errorf("k=0: want nil, got %+v", got)
	}
	if got := TopK(in, -1); got != nil {
		t.Errorf("k<0: want nil, got %+v", got)
	}
}

func TestTopK_EmptyInput(t *testing.T) {
	if got := TopK(nil, 5); got != nil {
		t.Errorf("nil input: want nil, got %+v", got)
	}
}

func TestTopK_KGreaterThanLenReturnsAll(t *testing.T) {
	now := time.Now()
	in := []types.DeviceScanPrompt{
		mkPrompt("a", 10, now),
		mkPrompt("b", 20, now),
	}
	got := TopK(in, 99)
	if len(got) != 2 {
		t.Fatalf("len: want 2, got %d", len(got))
	}
	if got[0].ChunkID != "b" {
		t.Errorf("highest first: got %q", got[0].ChunkID)
	}
}

func TestTopK_DoesNotMutateInput(t *testing.T) {
	now := time.Now()
	in := []types.DeviceScanPrompt{
		mkPrompt("first", 100, now),
		mkPrompt("second", 900, now.Add(time.Minute)),
	}
	_ = TopK(in, 2)
	if in[0].ChunkID != "first" || in[1].ChunkID != "second" {
		t.Errorf("input mutated: %+v", in)
	}
}
