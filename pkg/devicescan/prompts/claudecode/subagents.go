package claudecode

import (
	"io/fs"
	"path"
	"sort"

	"github.com/obot-platform/obot/apiclient/types"
	"github.com/obot-platform/obot/pkg/devicescan/prompts"
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
	// Steps is the subagent-context timeline emitted from this
	// transcript. Bridges are unresolved Task tool_use → subagent_call
	// pairs the build layer finalizes once the recursive tree resolves.
	Steps   []types.DeviceScanPromptStep
	Bridges []taskBridge
}

// parseSubagentFile streams a sidechain JSONL file and aggregates its
// metrics, tool calls, and any inner Task invocations (for nested
// recursion). Returns nil on read error so the caller can skip the
// file cleanly.
func parseSubagentFile(fsys fs.FS, file string) (*parsedSubagent, error) {
	agentID := subagentIDFromFile(file)
	s := &parsedSubagent{
		AgentID:     agentID,
		ToolCalls:   newToolCallCounter(),
		TasksByID:   map[string]*taskInvocation{},
		AgentToTask: map[string]string{},
	}
	pseudo := &chunk{
		ToolCalls:   s.ToolCalls,
		TasksByID:   s.TasksByID,
		AgentToTask: s.AgentToTask,
		steps:       newStepBuilder("subagent", agentID),
	}
	// The subagent's first user entry isn't a chunk start (we don't
	// chunk subagents) but we still want it on the timeline. Track
	// whether we've emitted the priming user step yet.
	firstUserEmitted := false
	err := scanEntries(fsys, file, func(e entry) error {
		if s.SessionID == "" {
			s.SessionID = e.SessionID
		}
		switch e.Type {
		case entryAssistant:
			absorbAssistant(pseudo, e)
		case entryUser:
			if !firstUserEmitted {
				if text, ok := isRealUserChunkStart(e); ok {
					pseudo.steps.addUser(e, text)
					firstUserEmitted = true
					return nil
				}
			}
			absorbUserFlow(pseudo, e)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	s.Metrics = pseudo.MainMetrics
	s.Tasks = pseudo.Tasks
	s.Steps = pseudo.steps.out
	s.Bridges = pseudo.steps.bridges
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

// resolveResult is the recursive resolver's return value: the
// external-shape subagent tree plus the flat list of subagent-context
// steps (own steps + descendants' steps + synthetic subagent_call
// markers emitted in each parsed subagent's own context for the
// children it spawned).
type resolveResult struct {
	Subagents []types.DeviceScanPromptSubagent
	Steps     []types.DeviceScanPromptStep
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
) resolveResult {
	if depth >= maxSubagentDepth {
		return resolveResult{}
	}

	files := rc.subagentFilesFor(parentSessionID)
	if len(files) == 0 {
		return resolveResult{}
	}

	var (
		nodes     = make([]types.DeviceScanPromptSubagent, 0, len(files))
		flatSteps []types.DeviceScanPromptStep
	)
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
		child := rc.resolveSubagents(parsed.AgentID, parsed.TasksByID, parsed.AgentToTask, depth+1)

		// Transitive metrics: this node's local metrics plus the
		// metrics of every descendant. At depth = maxSubagentDepth-1
		// children is nil; deeper activity is silently dropped per
		// the design contract.
		metrics := parsed.Metrics
		for _, c := range child.Subagents {
			addMetrics(&metrics, c.Metrics)
		}
		sealMetrics(&metrics)

		nodes = append(nodes, types.DeviceScanPromptSubagent{
			SubagentID:        parsed.AgentID,
			SubagentType:      subagentType,
			Description:       description,
			Metrics:           metrics,
			MainSessionImpact: impact,
			ToolCalls:         parsed.ToolCalls.emit(),
			Subagents:         child.Subagents,
		})

		// Flatten the timeline: this subagent's own steps, the
		// synthetic subagent_call markers for tasks it spawned, and
		// every descendant's steps (which already include their own
		// synthetic markers).
		flatSteps = append(flatSteps, parsed.Steps...)
		flatSteps = append(flatSteps, emitSubagentCallSteps(parsed.Bridges, parsed.AgentToTask, child.Subagents)...)
		flatSteps = append(flatSteps, child.Steps...)
	}

	// Stable order keyed off the subagent file path keeps golden
	// fixtures deterministic.
	sort.SliceStable(nodes, func(i, j int) bool {
		return nodes[i].Description < nodes[j].Description
	})
	return resolveResult{Subagents: nodes, Steps: flatSteps}
}

// emitSubagentCallSteps generates synthetic subagent_call steps for
// every Task tool_use bridge that resolves to a child subagent. The
// returned steps live in the caller's timeline (bridges carry the
// caller's Context + SubagentID) and the SubagentID field on each
// step points at the spawned child's tree node — matching the spec
// in DESIGN.md "Per-step DeviceScanPromptStep.SubagentID".
func emitSubagentCallSteps(
	bridges []taskBridge,
	agentToTask map[string]string,
	children []types.DeviceScanPromptSubagent,
) []types.DeviceScanPromptStep {
	if len(bridges) == 0 || len(children) == 0 {
		return nil
	}
	// Reverse map: taskID → spawned agentID. Skip Tasks that never
	// resolved to a subagent file (no tool_result with agentId).
	taskToAgent := make(map[string]string, len(agentToTask))
	for agent, task := range agentToTask {
		taskToAgent[task] = agent
	}
	resolved := make(map[string]struct{}, len(children))
	for _, ch := range children {
		if ch.SubagentID != "" {
			resolved[ch.SubagentID] = struct{}{}
		}
	}
	var out []types.DeviceScanPromptStep
	for _, br := range bridges {
		agentID, ok := taskToAgent[br.ToolUseID]
		if !ok {
			continue
		}
		if _, ok := resolved[agentID]; !ok {
			continue
		}
		head, full, hash := prompts.TruncateContent(br.Description, prompts.MaxStepHeadBytes)
		out = append(out, types.DeviceScanPromptStep{
			Kind:       "subagent_call",
			Context:    br.Context,
			SubagentID: agentID,
			StartedAt:  types.Time{Time: br.StartedAt},
			ToolUseID:  br.ToolUseID,
			TextHead:   head,
			TextBytes:  full,
			TextHash:   hash,
		})
	}
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
