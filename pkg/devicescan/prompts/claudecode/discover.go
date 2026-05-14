package claudecode

import (
	"io/fs"
	"path"
	"sort"
	"strings"
	"time"
)

// projectsRoot is the slash-separated root of Claude Code's per-project
// session storage under $HOME.
const projectsRoot = ".claude/projects"

// discoveredSession is one main-agent session file plus its associated
// sidechain (subagent) file paths.
type discoveredSession struct {
	// ProjectDir is the project directory under projectsRoot (slash-
	// separated, fs.FS-relative).
	ProjectDir string
	// SessionFile is the fs.FS-relative path to the main session
	// JSONL file.
	SessionFile string
	// SessionID is the file's basename without ".jsonl".
	SessionID string
	// ModTime is the session file's modification time.
	ModTime time.Time
	// NewSubagents are sidechain files under
	//   {projectDir}/{sessionID}/subagents/agent-*.jsonl
	// (modern layout).
	NewSubagents []string
	// LegacySubagents are sidechain files at
	//   {projectDir}/agent-*.jsonl
	// whose first-line sessionId matches SessionID.
	LegacySubagents []string
}

// discover walks ~/.claude/projects under HomeFS and returns every
// main-agent session file modified at or after `since`, paired with
// its candidate sidechain files. Files outside the window are skipped
// without being parsed. The scan is read-only and bounded; descent
// stops at the depths we know about.
//
// Discovery is best-effort. Per-project / per-file errors are
// swallowed (returning an empty slice) rather than aborting the whole
// scan, mirroring the "skip + warn, never panic" PromptScanner
// contract.
func discover(fsys fs.FS, since time.Time) []discoveredSession {
	projects, err := fs.ReadDir(fsys, projectsRoot)
	if err != nil {
		return nil
	}

	var out []discoveredSession
	for _, p := range projects {
		if !p.IsDir() {
			continue
		}
		projectDir := path.Join(projectsRoot, p.Name())
		out = append(out, discoverProject(fsys, projectDir, since)...)
	}

	// Stable order across runs simplifies golden-output tests.
	sort.Slice(out, func(i, j int) bool {
		return out[i].SessionFile < out[j].SessionFile
	})
	return out
}

func discoverProject(fsys fs.FS, projectDir string, since time.Time) []discoveredSession {
	entries, err := fs.ReadDir(fsys, projectDir)
	if err != nil {
		return nil
	}

	type sessionCandidate struct {
		id      string
		file    string
		modTime time.Time
	}

	var (
		sessions       []sessionCandidate
		legacySidechan []string
	)

	for _, e := range entries {
		name := e.Name()
		switch {
		case e.IsDir():
			// New-layout sidechains live under {sessionID}/subagents/
			// — they're discovered via newSubagentsFor when we visit
			// the matching session candidate.
			continue
		case strings.HasPrefix(name, "agent-") && strings.HasSuffix(name, ".jsonl"):
			legacySidechan = append(legacySidechan, path.Join(projectDir, name))
		case strings.HasSuffix(name, ".jsonl"):
			id := strings.TrimSuffix(name, ".jsonl")
			if id == "" {
				continue
			}
			info, err := e.Info()
			if err != nil {
				continue
			}
			if info.ModTime().Before(since) {
				continue
			}
			sessions = append(sessions, sessionCandidate{
				id:      id,
				file:    path.Join(projectDir, name),
				modTime: info.ModTime(),
			})
		}
	}

	if len(sessions) == 0 {
		return nil
	}

	out := make([]discoveredSession, 0, len(sessions))
	for _, s := range sessions {
		ds := discoveredSession{
			ProjectDir:  projectDir,
			SessionFile: s.file,
			SessionID:   s.id,
			ModTime:     s.modTime,
		}
		ds.NewSubagents = newSubagentsFor(fsys, projectDir, s.id)
		ds.LegacySubagents = filterLegacySubagents(fsys, legacySidechan, s.id)
		out = append(out, ds)
	}
	return out
}

// newSubagentsFor returns sidechain files in the modern layout:
//
//	{projectDir}/{sessionID}/subagents/agent-*.jsonl
func newSubagentsFor(fsys fs.FS, projectDir, sessionID string) []string {
	dir := path.Join(projectDir, sessionID, "subagents")
	entries, err := fs.ReadDir(fsys, dir)
	if err != nil {
		return nil
	}
	out := make([]string, 0, len(entries))
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || !strings.HasPrefix(name, "agent-") || !strings.HasSuffix(name, ".jsonl") {
			continue
		}
		out = append(out, path.Join(dir, name))
	}
	sort.Strings(out)
	return out
}

// filterLegacySubagents narrows the set of {projectDir}/agent-*.jsonl
// files to those whose first entry's sessionId field matches
// sessionID. Required because the legacy layout pools sidechains across
// all sessions in one project.
func filterLegacySubagents(fsys fs.FS, candidates []string, sessionID string) []string {
	if len(candidates) == 0 {
		return nil
	}
	out := make([]string, 0, len(candidates))
	for _, p := range candidates {
		e, err := readFirstLine(fsys, p)
		if err != nil {
			continue
		}
		if e.SessionID == sessionID {
			out = append(out, p)
		}
	}
	sort.Strings(out)
	return out
}

// subagentIDFromFile returns the trailing identifier in agent-{id}.jsonl
// — used to link sidechain files to Task tool_use blocks via tool_result
// agentId fields.
func subagentIDFromFile(p string) string {
	base := path.Base(p)
	base = strings.TrimSuffix(base, ".jsonl")
	return strings.TrimPrefix(base, "agent-")
}
