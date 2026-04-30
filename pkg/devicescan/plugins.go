package devicescan

import (
	"encoding/json"
	"io/fs"
	"path"

	"github.com/obot-platform/obot/apiclient/types"
)

// pluginExts is the file-collection extension allowlist used when ingesting
// plugin install directories. Wider than skillExts because plugins commonly
// include manifest, schema, and config files alongside scripts.
var pluginExts = map[string]bool{
	".md":    true,
	".mdc":   true,
	".txt":   true,
	".sh":    true,
	".py":    true,
	".js":    true,
	".ts":    true,
	".json":  true,
	".jsonc": true,
	".yaml":  true,
	".yml":   true,
	".toml":  true,
}

// emitPluginOpts is the per-client input for emitPlugin. Per-client
// scanners (claudecode.go, codex.go) populate this struct from their cache
// layouts and call emitPlugin to do the shared work of file ingestion,
// component detection, and nested-observation emission.
type emitPluginOpts struct {
	installRel  string // plugin directory, relative to fsys
	manifestRel string // manifest path within installRel (e.g. ".claude-plugin/plugin.json")
	pluginType  string // wire plugin_type (e.g. "claude_code_plugin")
	client      string // wire client (e.g. "claude_code")
	scope       string // wire scope ("global" / "user")
	marketplace string
	enabled     bool

	// nameFallback / versionFallback are used when the manifest is missing
	// or omits the corresponding field.
	nameFallback    string
	versionFallback string

	// nestedMCPRel optionally points to a separate file (e.g. mcp.json or
	// .mcp.json) checked first for nested MCP server definitions; if empty
	// or missing, the manifest's top-level `mcpServers` is used.
	nestedMCPRel []string

	// mcpServerXform, when non-nil, is invoked on each nested MCP server's
	// raw map before parsing. Used by Claude Code for ${CLAUDE_PLUGIN_ROOT}
	// substitution.
	mcpServerXform func(raw map[string]any)
}

// emitPlugin performs the shared plugin-emit work: parses the manifest,
// runs file collection, detects components, emits nested MCP server and
// skill observations, and finally emits the DeviceScanPlugin envelope.
func emitPlugin(r *Result, o emitPluginOpts) {
	manifestAbs, _ := r.AddFile(o.manifestRel)
	manifest := readJSONFile(r.fsys, o.manifestRel)

	name, version, description, author := manifestMetadata(manifest)
	if name == "" {
		name = o.nameFallback
	}
	if version == "" {
		version = o.versionFallback
	}

	files, _ := r.collectArtifactFiles(o.installRel, pluginExts)

	hasMCP, hasSkills, hasRules, hasCommands, hasHooks := detectComponents(r.fsys, o.installRel, manifest)

	// Nested MCP server observations (scope=plugin).
	mcpRaw, mcpSourceRel := pluginMCPServersBlock(r.fsys, o.installRel, o.nestedMCPRel, manifest, o.manifestRel)
	mcpSourceAbs := ""
	if mcpSourceRel != "" {
		mcpSourceAbs = r.abs(mcpSourceRel)
	}
	for serverName, raw := range mcpRaw {
		entry, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		if o.mcpServerXform != nil {
			o.mcpServerXform(entry)
		}
		obs := jsonServerEntry(serverName, entry, o.client, "plugin", mcpSourceAbs, manifestAbs, "")
		r.AddMCPServer(obs)
	}
	if len(mcpRaw) > 0 {
		hasMCP = true
	}

	// Nested skills under <installRel>/skills/<name>/SKILL.md (scope=plugin).
	if hasSkills {
		emitNestedSkills(r, o.installRel, o.client, manifestAbs)
	}

	r.AddPlugin(types.DeviceScanPlugin{
		Client:        o.client,
		Scope:         o.scope,
		Name:          name,
		PluginType:    o.pluginType,
		PluginFile:    manifestAbs,
		Version:       version,
		Description:   description,
		Author:        author,
		Enabled:       o.enabled,
		Marketplace:   o.marketplace,
		Files:         files,
		HasMCPServers: hasMCP,
		HasSkills:     hasSkills,
		HasRules:      hasRules,
		HasCommands:   hasCommands,
		HasHooks:      hasHooks,
	})
}

// readJSONFile reads and parses a JSON file. Returns nil on any error.
// Note: standard encoding/json is used; manifests with comments / trailing
// commas will fail to parse. Adding a JSON5 dependency is an open question
// flagged in the design.
func readJSONFile(fsys fs.FS, rel string) map[string]any {
	data, err := fs.ReadFile(fsys, rel)
	if err != nil {
		return nil
	}
	var out map[string]any
	if err := json.Unmarshal(data, &out); err != nil {
		return nil
	}
	return out
}

// manifestMetadata pulls (name, version, description, author) out of a
// parsed plugin manifest. author may be a string or {"name": "…"} object.
func manifestMetadata(m map[string]any) (name, version, description, author string) {
	if m == nil {
		return
	}
	name = asString(m["name"])
	version = asString(m["version"])
	description = asString(m["description"])
	switch a := m["author"].(type) {
	case string:
		author = a
	case map[string]any:
		author = asString(a["name"])
	}
	return
}

// detectComponents returns the has_* booleans for the wire plugin
// observation. Mirrors plugin_scanner._detect_components: mcp is true if
// mcp.json / .mcp.json exists, or the manifest has a non-empty mcpServers
// dict; the others key off subdirectory presence.
func detectComponents(fsys fs.FS, installRel string, manifest map[string]any) (mcp, skills, rules, commands, hooks bool) {
	if fileExists(fsys, path.Join(installRel, "mcp.json")) || fileExists(fsys, path.Join(installRel, ".mcp.json")) {
		mcp = true
	}
	if !mcp && manifest != nil {
		if m, ok := manifest["mcpServers"].(map[string]any); ok && len(m) > 0 {
			mcp = true
		}
	}
	skills = dirExists(fsys, path.Join(installRel, "skills"))
	rules = dirExists(fsys, path.Join(installRel, "rules"))
	commands = dirExists(fsys, path.Join(installRel, "commands"))
	hooks = dirExists(fsys, path.Join(installRel, "hooks"))
	return
}

// pluginMCPServersBlock locates the nested MCP server definitions for a
// plugin. It tries each candidate file in nestedMCPRel (typically
// "mcp.json" / ".mcp.json") and falls back to the manifest's mcpServers
// dict. Returns the raw server-name → raw-config map and the fs-relative
// path of the file the entries came from (manifestRel on fallback, ""
// when nothing was found).
func pluginMCPServersBlock(fsys fs.FS, installRel string, nestedMCPRel []string, manifest map[string]any, manifestRel string) (map[string]any, string) {
	for _, fname := range nestedMCPRel {
		fileRel := path.Join(installRel, fname)
		data := readJSONFile(fsys, fileRel)
		if data == nil {
			continue
		}
		if m, ok := data["mcpServers"].(map[string]any); ok && len(m) > 0 {
			return m, fileRel
		}
		// Some configs store servers at root level rather than under mcpServers.
		root := map[string]any{}
		for k, v := range data {
			if entry, ok := v.(map[string]any); ok {
				if _, hasCmd := entry["command"]; hasCmd {
					root[k] = v
					continue
				}
				if _, hasURL := entry["url"]; hasURL {
					root[k] = v
					continue
				}
				if _, hasSrvURL := entry["serverUrl"]; hasSrvURL {
					root[k] = v
				}
			}
		}
		if len(root) > 0 {
			return root, fileRel
		}
	}
	if manifest != nil {
		if m, ok := manifest["mcpServers"].(map[string]any); ok && len(m) > 0 {
			return m, manifestRel
		}
	}
	return nil, ""
}

// emitNestedSkills walks <installRel>/skills/<name>/SKILL.md and emits a
// plugin-scope skill observation for each, attributed to client with
// pluginFileAbs set to the plugin's manifest path.
func emitNestedSkills(r *Result, installRel, client, manifestAbs string) {
	skillsRoot := path.Join(installRel, "skills")
	entries, err := fs.ReadDir(r.fsys, skillsRoot)
	if err != nil {
		return
	}
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		skillDir := path.Join(skillsRoot, e.Name())
		if !fileExists(r.fsys, path.Join(skillDir, "SKILL.md")) {
			continue
		}
		ingestSkill(r, skillDir, "plugin", client, manifestAbs)
	}
}
