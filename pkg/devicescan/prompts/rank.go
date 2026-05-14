package prompts

import (
	"sort"

	"github.com/obot-platform/obot/apiclient/types"
)

// TopK returns the top k prompts from in, ranked by
// metrics.totalTokens descending with endedAt descending as the
// tie-breaker (most recent first). The sort is stable so equal rows
// keep their original input order. Passing k <= 0 returns nil. If
// k >= len(in) the whole slice is returned in ranked order.
//
// TopK does not mutate the input slice.
func TopK(in []types.DeviceScanPrompt, k int) []types.DeviceScanPrompt {
	if k <= 0 || len(in) == 0 {
		return nil
	}

	ranked := make([]types.DeviceScanPrompt, len(in))
	copy(ranked, in)
	sort.SliceStable(ranked, func(i, j int) bool {
		if ranked[i].Metrics.TotalTokens != ranked[j].Metrics.TotalTokens {
			return ranked[i].Metrics.TotalTokens > ranked[j].Metrics.TotalTokens
		}
		return ranked[i].EndedAt.GetTime().After(ranked[j].EndedAt.GetTime())
	})

	if k >= len(ranked) {
		return ranked
	}
	return ranked[:k]
}
