package claudecode

import (
	"io/fs"
	"path"
	"sort"

	"github.com/obot-platform/obot/apiclient/types"
)

// maxSubagentDepth caps the recursive subagent tree. Per DESIGN.md
// "Subagent attribution" — deeper subagents are folded into their
// level-5 ancestor's Metrics.
const maxSubagentDepth = 5

// parsedSubagent is the streamed aggregate view of one subagent JSONL
// file. Subagent files are treated as a single logical "session" (no
// chunking by user turn) because each file represents a single
// subagent invocation.
type parsedSubagent struct {
	AgentID     string
	SessionID   string
	Metrics     types.DeviceScanPromptMetrics
	ToolCalls   *toolCallCounter
	Tasks       []*taskInvocation
	TasksByID   map[string]*taskInvocation
	AgentToTask map[string]string
}

// parseSubagentFile streams a sidechain JSONL file and aggregates its
// metrics, tool calls, and any inner Task invocations (for nested
// recursion). Returns nil on read error so the caller can skip the
// file cleanly.
func parseSubagentFile(fsys fs.FS, file string) (*parsedSubagent, error) {
	s := &parsedSubagent{
		AgentID:     subagentIDFromFile(file),
		ToolCalls:   newToolCallCounter(),
		TasksByID:   map[string]*taskInvocation{},
		AgentToTask: map[string]string{},
	}
	pseudo := &chunk{
		ToolCalls:   s.ToolCalls,
		TasksByID:   s.TasksByID,
		AgentToTask: s.AgentToTask,
	}
	err := scanEntries(fsys, file, func(e entry) error {
		if s.SessionID == "" {
			s.SessionID = e.SessionID
		}
		switch e.Type {
		case entryAssistant:
			absorbAssistant(pseudo, e)
		case entryUser:
			absorbUserFlow(pseudo, e)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	s.Metrics = pseudo.MainMetrics
	s.Tasks = pseudo.Tasks
	sealMetrics(&s.Metrics)
	return s, nil
}

// resolveCtx threads the per-scan filesystem context through the
// recursive subagent resolver so callers don't have to plumb it
// through every call site.
type resolveCtx struct {
	fsys       fs.FS
	projectDir string
	// candidates is the set of *all* sidechain files for this project
	// — both NEW layout (under {projectDir}/{anyID}/subagents/) and
	// legacy (at {projectDir}/agent-*.jsonl). Pre-collected at
	// resolveStart so recursion doesn't re-walk the FS.
	candidates []candidateFile
}

// candidateFile is one sidechain JSONL path with its agentID
// (filename) and its sessionId (first-line field) precomputed.
type candidateFile struct {
	Path      string
	AgentID   string
	SessionID string
}

// resolveSubagents resolves the recursive subagent tree rooted at
// parentSessionID. parentTasks / parentAgentToTask come from the
// chunk (root call) or from a previously-parsed subagent (recursive
// calls). Depth is 0-based; recursion stops at maxSubagentDepth.
//
// At the root call (depth == 0) the per-chunk agent→task map is the
// definitive filter: only subagent files actually spawned by *this*
// chunk are included. Without that filter, every chunk in a multi-
// chunk parent session would inherit the entire session's sidechain
// directory.
func (rc *resolveCtx) resolveSubagents(
	parentSessionID string,
	parentTasks map[string]*taskInvocation,
	parentAgentToTask map[string]string,
	depth int,
) []types.DeviceScanPromptSubagent {
	if depth >= maxSubagentDepth {
		return nil
	}

	files := rc.subagentFilesFor(parentSessionID)
	if len(files) == 0 {
		return nil
	}

	out := make([]types.DeviceScanPromptSubagent, 0, len(files))
	for _, f := range files {
		taskID, linked := parentAgentToTask[f.AgentID]
		// Root-level subagents must be attributable to a Task call in
		// the current chunk. Nested levels keep the looser sessionId-
		// only match because the subagent transcript reliably names
		// its grandchildren.
		if depth == 0 && !linked {
			continue
		}

		parsed, err := parseSubagentFile(rc.fsys, f.Path)
		if err != nil || parsed == nil {
			continue
		}

		var (
			description  string
			subagentType string
			impact       types.DeviceScanPromptSubagentImpact
		)
		if linked {
			if t, ok := parentTasks[taskID]; ok {
				description = t.Description
				subagentType = t.SubagentType
				impact = types.DeviceScanPromptSubagentImpact{
					CallTokens:   t.CallTokens,
					ResultTokens: t.ResultTokens,
					TotalTokens:  t.CallTokens + t.ResultTokens,
				}
			}
		}

		// Recurse for nested subagents using THIS subagent's id as
		// the next parentSessionID.
		children := rc.resolveSubagents(parsed.AgentID, parsed.TasksByID, parsed.AgentToTask, depth+1)

		// Transitive metrics: this node's local metrics plus the
		// metrics of every descendant. At depth = maxSubagentDepth-1
		// children is nil; deeper activity is silently dropped per
		// the design contract.
		metrics := parsed.Metrics
		for _, c := range children {
			addMetrics(&metrics, c.Metrics)
		}
		sealMetrics(&metrics)

		out = append(out, types.DeviceScanPromptSubagent{
			SubagentType:      subagentType,
			Description:       description,
			Metrics:           metrics,
			MainSessionImpact: impact,
			ToolCalls:         parsed.ToolCalls.emit(),
			Subagents:         children,
		})
	}

	// Stable order keyed off the subagent file path keeps golden
	// fixtures deterministic.
	sort.SliceStable(out, func(i, j int) bool {
		return out[i].Description < out[j].Description
	})
	return out
}

func (rc *resolveCtx) subagentFilesFor(parentSessionID string) []candidateFile {
	var out []candidateFile
	for _, c := range rc.candidates {
		if c.SessionID == parentSessionID {
			out = append(out, c)
		}
	}
	return out
}

// gatherSubagentCandidates pre-walks every sidechain file under
// projectDir and records its agentID + sessionId. We pay for one
// per-file first-line read up front so recursion can match by
// sessionId in O(1).
//
// Two layouts are merged:
//
//	NEW:    {projectDir}/{anyID}/subagents/agent-*.jsonl
//	LEGACY: {projectDir}/agent-*.jsonl
//
// (DESIGN.md uses the term "new" for the per-session subdir; the
// modern Claude Code on-disk layout sits one level deeper than the
// docs imply — files live under .../{id}/subagents/agent-*.jsonl.)
func gatherSubagentCandidates(fsys fs.FS, projectDir string) []candidateFile {
	var out []candidateFile

	// Legacy: top-level agent-*.jsonl in projectDir.
	if entries, err := fs.ReadDir(fsys, projectDir); err == nil {
		for _, e := range entries {
			if e.IsDir() {
				continue
			}
			if !isSubagentName(e.Name()) {
				continue
			}
			p := path.Join(projectDir, e.Name())
			c, ok := newCandidate(fsys, p)
			if ok {
				out = append(out, c)
			}
		}
	}

	// New layout: walk every immediate {anyID}/subagents/ directory
	// under projectDir.
	if entries, err := fs.ReadDir(fsys, projectDir); err == nil {
		for _, e := range entries {
			if !e.IsDir() {
				continue
			}
			sub := path.Join(projectDir, e.Name(), "subagents")
			subEntries, err := fs.ReadDir(fsys, sub)
			if err != nil {
				continue
			}
			for _, se := range subEntries {
				if se.IsDir() || !isSubagentName(se.Name()) {
					continue
				}
				p := path.Join(sub, se.Name())
				c, ok := newCandidate(fsys, p)
				if ok {
					out = append(out, c)
				}
			}
		}
	}

	return out
}

func isSubagentName(name string) bool {
	return len(name) > len("agent-.jsonl") &&
		name[:len("agent-")] == "agent-" &&
		name[len(name)-len(".jsonl"):] == ".jsonl"
}

func newCandidate(fsys fs.FS, p string) (candidateFile, bool) {
	first, err := readFirstLine(fsys, p)
	if err != nil {
		return candidateFile{}, false
	}
	return candidateFile{
		Path:      p,
		AgentID:   subagentIDFromFile(p),
		SessionID: first.SessionID,
	}, true
}
