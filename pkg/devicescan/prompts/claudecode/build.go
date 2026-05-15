package claudecode

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"io/fs"
	"sort"

	"github.com/obot-platform/obot/apiclient/types"
	"github.com/obot-platform/obot/logger"
	"github.com/obot-platform/obot/pkg/devicescan/prompts"
)

var log = logger.Package()

// metricsDriftWarnPct is the drift threshold above which the
// reconciler logs a warning that step-derived totals diverged from
// the rollup metrics. Below this we keep the rollups silently — they
// remain authoritative.
const metricsDriftWarnPct = 1.0

// clientID is the canonical client identifier persisted in
// DeviceScanPrompt.Client. Matches the server allow-list entry.
const clientID = "claude_code"

// buildPrompts assembles every candidate DeviceScanPrompt row from
// every in-window session under HomeFS. Ranking + top-K trimming
// happens in the caller (or `prompts.TopK`); this function intentionally
// returns every chunk so the merge-and-rank logic stays in one place.
func buildPrompts(ctx context.Context, fsys fs.FS, opts prompts.Options) []types.DeviceScanPrompt {
	sessions := discover(fsys, opts.Since)
	if len(sessions) == 0 {
		return nil
	}

	out := make([]types.DeviceScanPrompt, 0, len(sessions))
	for _, sess := range sessions {
		if err := ctx.Err(); err != nil {
			return out
		}
		chunks, err := chunkSession(fsys, sess.SessionFile, sess.SessionID)
		if err != nil || len(chunks) == 0 {
			continue
		}
		rc := &resolveCtx{
			fsys:       fsys,
			projectDir: sess.ProjectDir,
			candidates: gatherSubagentCandidates(fsys, sess.ProjectDir),
		}
		for _, c := range chunks {
			out = append(out, buildRow(c, rc))
		}
	}

	// Stable secondary order so equal-token rows have a deterministic
	// arrangement before the caller's TopK runs. The CLI's TopK is
	// itself stable, so this preserves "most recent first" among
	// ties.
	sort.SliceStable(out, func(i, j int) bool {
		return out[i].EndedAt.GetTime().After(out[j].EndedAt.GetTime())
	})
	return out
}

func buildRow(c *chunk, rc *resolveCtx) types.DeviceScanPrompt {
	res := rc.resolveSubagents(c.SessionID, c.TasksByID, c.AgentToTask, 0)

	transitive := c.MainMetrics
	for _, sa := range res.Subagents {
		addMetrics(&transitive, sa.Metrics)
	}
	sealMetrics(&transitive)

	text, fullBytes, hashHex := prompts.TruncatePromptText(c.UserText)

	cid := chunkID(c.SessionID, c.FirstAssistantUUID)
	steps := assembleSteps(c, res, cid)

	row := types.DeviceScanPrompt{
		Client:      clientID,
		SessionID:   c.SessionID,
		ChunkID:     cid,
		Model:       c.Model,
		StartedAt:   types.Time{Time: c.StartedAt},
		EndedAt:     types.Time{Time: c.EndedAt},
		Cwd:         c.Cwd,
		GitBranch:   c.GitBranch,
		PromptText:  text,
		PromptHash:  hashHex,
		PromptBytes: fullBytes,
		Metrics:     transitive,
		MainMetrics: c.MainMetrics,
		ToolCalls:   c.ToolCalls.emit(),
		Subagents:   res.Subagents,
		Steps:       steps,
	}
	if !c.StartedAt.IsZero() && !c.EndedAt.IsZero() {
		row.DurationMs = c.EndedAt.Sub(c.StartedAt).Milliseconds()
	}
	reconcileMetrics(cid, row.MainMetrics, row.Subagents, steps)
	return row
}

// assembleSteps merges main-context steps + synthetic subagent_call
// markers (for main → direct children) + every descendant's flat
// steps, sorts the result by StartedAt (stable), caps at
// MaxStepsPerPrompt with a logged warning, and fills in
// AccumulatedContextTokens.
func assembleSteps(c *chunk, res resolveResult, chunkID string) []types.DeviceScanPromptStep {
	mainCalls := emitSubagentCallSteps(c.Bridges, c.AgentToTask, res.Subagents)
	steps := make([]types.DeviceScanPromptStep, 0, len(c.Steps)+len(mainCalls)+len(res.Steps))
	steps = append(steps, c.Steps...)
	steps = append(steps, mainCalls...)
	steps = append(steps, res.Steps...)
	sort.SliceStable(steps, func(i, j int) bool {
		return steps[i].StartedAt.GetTime().Before(steps[j].StartedAt.GetTime())
	})
	if len(steps) > MaxStepsPerPrompt {
		log.Warnf("claudecode: prompt %s timeline %d steps exceeds cap %d; truncating",
			chunkID, len(steps), MaxStepsPerPrompt)
		steps = steps[:MaxStepsPerPrompt]
	}
	accumulateContextTokens(steps)
	return steps
}

// reconcileMetrics compares the rollup metrics (MainMetrics and each
// subagent node's transitive Metrics) against the totals derived from
// the step list. The rollups stay authoritative; we only emit a
// warning when the step-derived totals diverge by more than
// metricsDriftWarnPct percent — that's a hint a fixture or
// proportioning bug is hiding behind the aggregate.
func reconcileMetrics(
	chunkID string,
	mainRollup types.DeviceScanPromptMetrics,
	subagents []types.DeviceScanPromptSubagent,
	steps []types.DeviceScanPromptStep,
) {
	mainDerived := types.DeviceScanPromptMetrics{}
	perSubagent := map[string]*types.DeviceScanPromptMetrics{}
	for _, s := range steps {
		dst := &mainDerived
		if s.Context == "subagent" && s.SubagentID != "" && s.Kind != "subagent_call" {
			// subagent_call steps live in the caller's context with
			// SubagentID = spawned child, so don't accidentally
			// charge their tokens (always zero) into the child node.
			d, ok := perSubagent[s.SubagentID]
			if !ok {
				d = &types.DeviceScanPromptMetrics{}
				perSubagent[s.SubagentID] = d
			}
			dst = d
		}
		dst.InputTokens += s.Tokens.Input
		dst.OutputTokens += s.Tokens.Output
		dst.CacheReadTokens += s.Tokens.CacheRead
		dst.CacheCreationTokens += s.Tokens.CacheCreation
	}
	if drifted(mainRollup, mainDerived) {
		log.Warnf("claudecode: prompt %s main metrics drift >1%%: rollup=%+v step-derived=%+v",
			chunkID, mainRollup, mainDerived)
	}
	for _, sa := range subagents {
		reconcileSubagent(chunkID, sa, perSubagent)
	}
}

func reconcileSubagent(
	chunkID string,
	node types.DeviceScanPromptSubagent,
	perSubagent map[string]*types.DeviceScanPromptMetrics,
) {
	if node.SubagentID != "" {
		derived := perSubagent[node.SubagentID]
		if derived == nil {
			derived = &types.DeviceScanPromptMetrics{}
		}
		// node.Metrics is the *transitive* total. Recompose the same
		// shape from per-node derived sums for an apples-to-apples
		// comparison.
		transitiveDerived := *derived
		accumulateTransitive(&transitiveDerived, node.Subagents, perSubagent)
		if drifted(node.Metrics, transitiveDerived) {
			log.Warnf("claudecode: prompt %s subagent %s metrics drift >1%%: rollup=%+v step-derived=%+v",
				chunkID, node.SubagentID, node.Metrics, transitiveDerived)
		}
	}
	for _, child := range node.Subagents {
		reconcileSubagent(chunkID, child, perSubagent)
	}
}

func accumulateTransitive(
	dst *types.DeviceScanPromptMetrics,
	nodes []types.DeviceScanPromptSubagent,
	perSubagent map[string]*types.DeviceScanPromptMetrics,
) {
	for _, n := range nodes {
		if d, ok := perSubagent[n.SubagentID]; ok && d != nil {
			dst.InputTokens += d.InputTokens
			dst.OutputTokens += d.OutputTokens
			dst.CacheReadTokens += d.CacheReadTokens
			dst.CacheCreationTokens += d.CacheCreationTokens
		}
		accumulateTransitive(dst, n.Subagents, perSubagent)
	}
}

// drifted returns true when any non-zero token component of rollup
// differs from derived by more than metricsDriftWarnPct percent.
// Components where both sides are zero are treated as matching.
func drifted(rollup, derived types.DeviceScanPromptMetrics) bool {
	pairs := [...][2]int64{
		{rollup.InputTokens, derived.InputTokens},
		{rollup.OutputTokens, derived.OutputTokens},
		{rollup.CacheReadTokens, derived.CacheReadTokens},
		{rollup.CacheCreationTokens, derived.CacheCreationTokens},
	}
	for _, p := range pairs {
		a, b := p[0], p[1]
		if a == 0 && b == 0 {
			continue
		}
		if a == 0 {
			return true
		}
		diff := b - a
		if diff < 0 {
			diff = -diff
		}
		if float64(diff)*100.0/float64(a) > metricsDriftWarnPct {
			return true
		}
	}
	return false
}

// chunkID is the stable per-prompt identifier: sha256 of
// sessionID + first-assistant uuid, truncated to 16 hex chars. When
// the first assistant uuid is missing (degenerate chunk), the session
// id is used twice — still stable, still unique within a scan.
func chunkID(sessionID, firstAssistantUUID string) string {
	if firstAssistantUUID == "" {
		firstAssistantUUID = sessionID
	}
	sum := sha256.Sum256([]byte(sessionID + "|" + firstAssistantUUID))
	return hex.EncodeToString(sum[:8])
}
