package devicescan

import (
	"io/fs"
	"path"
	"strings"

	"github.com/obot-platform/obot/apiclient/types"
)

// File extensions ingested when collecting a skill's directory.
// Mirrors runlayer's skill_scanner.SUPPORTED_EXTENSIONS.
var skillExts = map[string]bool{
	".md":  true,
	".mdc": true,
	".txt": true,
	".sh":  true,
	".py":  true,
	".js":  true,
	".ts":  true,
}

// globalSkillDirs are home-relative directories whose immediate children
// are skill directories. The tool tag is wired into Client on the wire
// observation (the schema has no separate Tool field; "multi" is used for
// dirs that are not associated with a single client).
var globalSkillDirs = []struct {
	rel  string
	tool string
}{
	{".claude/skills", "claude_code"},
	{".agents/skills", "multi"},
	{".codex/skills", "codex"},
	{".config/opencode/skills", "opencode"},
	{".agent/skills", "multi"},
	{".skillport/skills", "skillport"},
}

// homeClientTool maps the first home-relative path component to a tool
// tag, used to attribute SKILL.md files found anywhere under that
// component to the right client with scope=user.
var homeClientTool = map[string]string{
	".cursor":    "cursor",
	".claude":    "claude_code",
	".codex":     "codex",
	".codeium":   "windsurf",
	".windsurf":  "windsurf",
	".agents":    "multi",
	".agent":     "multi",
	".skillport": "skillport",
}

func scanGlobalSkills(r *Result) {
	for _, gd := range globalSkillDirs {
		entries, err := fs.ReadDir(r.fsys, gd.rel)
		if err != nil {
			continue
		}
		for _, e := range entries {
			if !e.IsDir() {
				continue
			}
			skillDir := path.Join(gd.rel, e.Name())
			if !fileExists(r.fsys, path.Join(skillDir, "SKILL.md")) {
				continue
			}
			ingestSkill(r, skillDir, "global", gd.tool, "")
		}
	}
}

func scanProjectSkills(r *Result, markers []string) {
	prefixes := globalSkillPrefixes()
	seen := map[string]bool{}
	for _, m := range markers {
		if path.Base(m) != "SKILL.md" {
			continue
		}
		if hasAnyPrefix(m, prefixes) {
			continue
		}
		skillDir := path.Dir(m)
		if seen[skillDir] {
			continue
		}
		seen[skillDir] = true

		if tool, ok := inferHomeTool(m); ok {
			ingestSkill(r, skillDir, "user", tool, "")
		} else {
			ingestSkill(r, skillDir, "project", "multi", "")
		}
	}
}

// ingestSkill builds a DeviceScanSkill for the directory at skillDirRel,
// records its files, and emits the observation. pluginFileAbs is the
// absolute path of the owning plugin's manifest when the skill is
// plugin-scoped; pass "" otherwise.
func ingestSkill(r *Result, skillDirRel, scope, client, pluginFileAbs string) {
	markerRel := path.Join(skillDirRel, "SKILL.md")
	markerData, err := fs.ReadFile(r.fsys, markerRel)
	if err != nil {
		return
	}
	name, description := parseFrontmatter(markerData)
	if name == "" {
		name = clipRunes(path.Base(skillDirRel), skillNameMaxRunes)
	}

	hasScripts := dirExists(r.fsys, path.Join(skillDirRel, "scripts"))
	files, _ := r.collectArtifactFiles(skillDirRel, skillExts)
	gitURL := readGitOrigin(r.fsys, skillDirRel)

	r.AddSkill(types.DeviceScanSkill{
		Client:       client,
		Scope:        scope,
		PluginFile:   pluginFileAbs,
		Name:         name,
		Description:  description,
		Files:        files,
		HasScripts:   hasScripts,
		GitRemoteURL: gitURL,
	})
}

// globalSkillPrefixes returns the set of fs-relative path prefixes that
// scanGlobalSkills owns; SKILL.md files under these prefixes are skipped
// by scanProjectSkills to avoid double-counting.
func globalSkillPrefixes() []string {
	out := make([]string, 0, len(globalSkillDirs))
	for _, gd := range globalSkillDirs {
		out = append(out, gd.rel+"/")
	}
	return out
}

func hasAnyPrefix(rel string, prefixes []string) bool {
	for _, p := range prefixes {
		if strings.HasPrefix(rel, p) {
			return true
		}
	}
	return false
}

// inferHomeTool returns the tool tag if rel is under a known home
// dot-directory (e.g. .claude/.../SKILL.md → claude_code). The empty
// string is returned for paths outside any known home dot-dir.
func inferHomeTool(rel string) (string, bool) {
	first, _, _ := strings.Cut(rel, "/")
	tool, ok := homeClientTool[first]
	return tool, ok
}

func fileExists(fsys fs.FS, rel string) bool {
	info, err := fs.Stat(fsys, rel)
	return err == nil && !info.IsDir()
}

func dirExists(fsys fs.FS, rel string) bool {
	info, err := fs.Stat(fsys, rel)
	return err == nil && info.IsDir()
}
