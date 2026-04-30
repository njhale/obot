package devicescan

import (
	"io/fs"
	"path"
	"path/filepath"
	"strings"
)

// Claude Code on-disk layout. Paths are relative to the home fs.
const (
	claudeGlobalConfigRel     = ".claude.json"
	claudeProjectMarkerName   = ".mcp.json"
	claudeSettingsRel         = ".claude/settings.json"
	claudeInstalledPluginsRel = ".claude/plugins/installed_plugins.json"
	claudePluginManifestSub   = ".claude-plugin/plugin.json"
)

var claudecodeDef = clientDef{
	name: "claude_code",
	projectMarkers: []markerRule{
		{basename: claudeProjectMarkerName},
	},
	scanGlobal:  scanClaudeCodeGlobal,
	scanProject: scanClaudeCodeProject,
}

// scanClaudeCodeGlobal reads ~/.claude.json and emits MCP server
// observations for both the top-level mcpServers map (scope=global) and
// any servers under projects.<absPath>.mcpServers (scope=project, with
// ProjectPath set to <absPath>).
func scanClaudeCodeGlobal(r *Result) {
	cfg := readJSONFile(r.fsys, claudeGlobalConfigRel)
	if cfg == nil {
		return
	}
	r.markGlobalConfig(claudeGlobalConfigRel)
	configAbs := r.abs(claudeGlobalConfigRel)

	if servers, ok := cfg["mcpServers"].(map[string]any); ok {
		for name, raw := range servers {
			entry, ok := raw.(map[string]any)
			if !ok {
				continue
			}
			r.AddMCPServer(jsonServerEntry(name, entry, "claude_code", "global", configAbs, "", ""))
		}
	}

	projects, _ := cfg["projects"].(map[string]any)
	for projKey, projVal := range projects {
		proj, ok := projVal.(map[string]any)
		if !ok {
			continue
		}
		servers, ok := proj["mcpServers"].(map[string]any)
		if !ok {
			continue
		}
		for name, raw := range servers {
			entry, ok := raw.(map[string]any)
			if !ok {
				continue
			}
			r.AddMCPServer(jsonServerEntry(name, entry, "claude_code", "project", configAbs, "", projKey))
		}
	}
}

// scanClaudeCodeProject parses a project-scope .mcp.json and emits MCP
// server observations with scope=project. Project root = parent of the
// .mcp.json file.
func scanClaudeCodeProject(r *Result, markerRel string) {
	cfg := readJSONFile(r.fsys, markerRel)
	if cfg == nil {
		return
	}
	configAbs := r.abs(markerRel)
	projectAbs := r.abs(path.Dir(markerRel))

	servers, ok := cfg["mcpServers"].(map[string]any)
	if !ok {
		return
	}
	for name, raw := range servers {
		entry, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		r.AddMCPServer(jsonServerEntry(name, entry, "claude_code", "project", configAbs, "", projectAbs))
	}
}

// scanClaudeCodePlugins reads installed_plugins.json and emits a Plugin
// observation (plus nested MCPServer / Skill observations) for each
// installation that resolves to a directory under the home fs.
func scanClaudeCodePlugins(r *Result) {
	registry := readJSONFile(r.fsys, claudeInstalledPluginsRel)
	if registry == nil {
		return
	}
	plugins, ok := registry["plugins"].(map[string]any)
	if !ok {
		return
	}

	enabledByKey := readEnabledPluginsMap(r.fsys, claudeSettingsRel)

	for pluginKey, installsRaw := range plugins {
		installs, ok := installsRaw.([]any)
		if !ok {
			continue
		}
		pluginName, marketplace := splitPluginKey(pluginKey)

		for _, instRaw := range installs {
			install, ok := instRaw.(map[string]any)
			if !ok {
				continue
			}
			installPathAbs := asString(install["installPath"])
			if installPathAbs == "" {
				continue
			}
			installRel, ok := relUnderHome(r.homeAbs, installPathAbs)
			if !ok {
				continue
			}
			if !dirExists(r.fsys, installRel) {
				continue
			}
			manifestRel := path.Join(installRel, claudePluginManifestSub)
			if !fileExists(r.fsys, manifestRel) {
				continue
			}

			scope := asString(install["scope"])
			if scope == "" {
				scope = "user"
			}
			version := asString(install["version"])

			emitPlugin(r, emitPluginOpts{
				installRel:      installRel,
				manifestRel:     manifestRel,
				pluginType:      "claude_code_plugin",
				client:          "claude_code",
				scope:           scope,
				marketplace:     marketplace,
				enabled:         enabledByKey[pluginKey],
				nameFallback:    pluginName,
				versionFallback: version,
				nestedMCPRel:    []string{"mcp.json", ".mcp.json"},
				mcpServerXform:  substituteClaudePluginRoot(installPathAbs),
			})
		}
	}
}

// substituteClaudePluginRoot returns an mcpServerXform that replaces
// ${CLAUDE_PLUGIN_ROOT} with installPathAbs in the command, args, env, and
// url fields of a raw server entry.
func substituteClaudePluginRoot(installPathAbs string) func(map[string]any) {
	return func(raw map[string]any) {
		sub := func(s string) string {
			return strings.ReplaceAll(s, "${CLAUDE_PLUGIN_ROOT}", installPathAbs)
		}
		if c, ok := raw["command"].(string); ok {
			raw["command"] = sub(c)
		}
		if u, ok := raw["url"].(string); ok {
			raw["url"] = sub(u)
		}
		if a, ok := raw["args"].([]any); ok {
			for i, x := range a {
				if s, ok := x.(string); ok {
					a[i] = sub(s)
				}
			}
		}
		if env, ok := raw["env"].(map[string]any); ok {
			for k, v := range env {
				if s, ok := v.(string); ok {
					env[k] = sub(s)
				}
			}
		}
	}
}

// readEnabledPluginsMap reads the enabledPlugins object from a settings
// file and returns it as map[key]bool. Unrecognised values default to
// false. Returns nil if the file is missing or malformed.
func readEnabledPluginsMap(fsys fs.FS, rel string) map[string]bool {
	data := readJSONFile(fsys, rel)
	if data == nil {
		return nil
	}
	ep, ok := data["enabledPlugins"].(map[string]any)
	if !ok {
		return nil
	}
	out := make(map[string]bool, len(ep))
	for k, v := range ep {
		b, _ := v.(bool)
		out[k] = b
	}
	return out
}

// splitPluginKey separates "name@marketplace" plugin keys into their parts.
// A bare "name" yields marketplace="".
func splitPluginKey(key string) (name, marketplace string) {
	at := strings.IndexByte(key, '@')
	if at < 0 {
		return key, ""
	}
	return key[:at], key[at+1:]
}

// relUnderHome converts an absolute path into its fs-relative form when
// the path lies under homeAbs. Returns ok=false otherwise (plugin
// installed outside the home fs are not scannable here).
func relUnderHome(homeAbs, abs string) (string, bool) {
	rel, err := filepath.Rel(homeAbs, abs)
	if err != nil {
		return "", false
	}
	if strings.HasPrefix(rel, "..") {
		return "", false
	}
	return filepath.ToSlash(rel), true
}
