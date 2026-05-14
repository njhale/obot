package claudecode

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"io/fs"
	"sort"

	"github.com/obot-platform/obot/apiclient/types"
	"github.com/obot-platform/obot/pkg/devicescan/prompts"
)

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
	subagents := rc.resolveSubagents(c.SessionID, c.TasksByID, c.AgentToTask, 0)

	transitive := c.MainMetrics
	for _, sa := range subagents {
		addMetrics(&transitive, sa.Metrics)
	}
	sealMetrics(&transitive)

	text, fullBytes, hashHex := prompts.TruncatePromptText(c.UserText)

	row := types.DeviceScanPrompt{
		Client:      clientID,
		SessionID:   c.SessionID,
		ChunkID:     chunkID(c.SessionID, c.FirstAssistantUUID),
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
		Subagents:   subagents,
	}
	if !c.StartedAt.IsZero() && !c.EndedAt.IsZero() {
		row.DurationMs = c.EndedAt.Sub(c.StartedAt).Milliseconds()
	}
	return row
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
