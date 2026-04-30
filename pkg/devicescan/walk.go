package devicescan

import (
	"io/fs"
	"path"
	"strings"
)

// markerRule matches a project-scope marker file by exact basename, with
// an optional immediate-parent constraint to disambiguate clients that
// share a basename (e.g. mcp.json under .cursor/ vs .vscode/).
type markerRule struct {
	basename string
	parent   string // empty matches any parent
}

// markerSkipDirs are basenames the crawl prunes when descending. The set
// covers dependency caches, build outputs, system / app-support trees that
// can't host project configs, and the macOS Trash. We rely on basename
// matching rather than path-suffix matching for simplicity; this loses
// some precision (the entire ~/Library tree is skipped, not just
// ~/Library/Caches) but is acceptable because client global configs are
// opened directly in Phase 1, not via the marker walk.
var markerSkipDirs = map[string]bool{
	"node_modules": true,
	".git":         true,
	".venv":        true,
	"venv":         true,
	"__pycache__":  true,
	"vendor":       true,
	"dist":         true,
	"build":        true,
	"target":       true,
	".next":        true,
	".nuxt":        true,
	".turbo":       true,
	".cache":       true,
	".npm":         true,
	".yarn":        true,
	"Library":      true,
	"AppData":      true,
	".Trash":       true,
	"tmp":          true,
	"temp":         true,
}

// allMarkerRules collects the marker rules every concern wants the crawl
// to surface. Skills register SKILL.md; each clientDef registers its
// project marker(s).
func allMarkerRules() []markerRule {
	rules := []markerRule{{basename: "SKILL.md"}}
	for _, c := range clientDefs {
		rules = append(rules, c.projectMarkers...)
	}
	return rules
}

// walkMarkers performs a single bounded fs.WalkDir from the root of fsys
// and returns the relative paths of every file whose basename (and
// optional parent) matches a rule. Order is not guaranteed.
func walkMarkers(fsys fs.FS, rules []markerRule, maxDepth int) []string {
	if fsys == nil || len(rules) == 0 {
		return nil
	}
	byBasename := map[string][]markerRule{}
	for _, r := range rules {
		byBasename[r.basename] = append(byBasename[r.basename], r)
	}

	var hits []string
	_ = fs.WalkDir(fsys, ".", func(rel string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() {
			if rel == "." {
				return nil
			}
			if markerSkipDirs[d.Name()] {
				return fs.SkipDir
			}
			depth := strings.Count(rel, "/") + 1
			if depth >= maxDepth {
				return fs.SkipDir
			}
			return nil
		}
		candidates, ok := byBasename[d.Name()]
		if !ok {
			return nil
		}
		parent := path.Base(path.Dir(rel))
		for _, c := range candidates {
			if c.parent == "" || c.parent == parent {
				hits = append(hits, rel)
				return nil
			}
		}
		return nil
	})
	return hits
}
