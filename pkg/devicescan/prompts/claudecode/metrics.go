package claudecode

import (
	"sort"

	"github.com/obot-platform/obot/apiclient/types"
)

// addUsage folds u into m. Safe when u is nil (no-op). TotalTokens is
// not maintained here — it is derived by sealMetrics once all entries
// have been accumulated, keeping us aligned with claude-devtools'
// SessionMetrics shape (total = input + output, cache columns tracked
// separately).
func addUsage(m *types.DeviceScanPromptMetrics, u *usage) {
	if u == nil {
		return
	}
	m.InputTokens += u.InputTokens
	m.OutputTokens += u.OutputTokens
	m.CacheReadTokens += u.CacheReadInputTokens
	m.CacheCreationTokens += u.CacheCreationInputTokens
}

// sealMetrics computes the derived TotalTokens field. Call once
// aggregation for a metrics bucket is complete.
func sealMetrics(m *types.DeviceScanPromptMetrics) {
	m.TotalTokens = m.InputTokens + m.OutputTokens
}

// addMetrics folds add into dst (no resealing here — callers must
// sealMetrics on the parent if they read TotalTokens off it).
func addMetrics(dst *types.DeviceScanPromptMetrics, add types.DeviceScanPromptMetrics) {
	dst.InputTokens += add.InputTokens
	dst.OutputTokens += add.OutputTokens
	dst.CacheReadTokens += add.CacheReadTokens
	dst.CacheCreationTokens += add.CacheCreationTokens
}

// toolCallCounter accumulates {name → count} pairs and emits the
// sorted-by-count-desc slice the wire type expects. Sorting is stable
// on name (asc) inside count buckets so output is deterministic.
type toolCallCounter struct {
	counts map[string]int
}

func newToolCallCounter() *toolCallCounter {
	return &toolCallCounter{counts: map[string]int{}}
}

func (c *toolCallCounter) add(name string) {
	if name == "" {
		return
	}
	c.counts[name]++
}

func (c *toolCallCounter) emit() []types.DeviceScanPromptToolCall {
	if len(c.counts) == 0 {
		return nil
	}
	out := make([]types.DeviceScanPromptToolCall, 0, len(c.counts))
	for n, ct := range c.counts {
		out = append(out, types.DeviceScanPromptToolCall{Name: n, Count: ct})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Count != out[j].Count {
			return out[i].Count > out[j].Count
		}
		return out[i].Name < out[j].Name
	})
	return out
}

// estimateTokens approximates a token count from a byte length using
// the 4-chars-per-token heuristic claude-devtools uses for
// pre-computed display values (shared/utils/tokenFormatting.ts).
// Returning byte-length-based estimates keeps us aligned with their
// MetricsPill rendering even though we never see the underlying text.
func estimateTokens(n int) int64 {
	if n <= 0 {
		return 0
	}
	return int64((n + 3) / 4)
}
