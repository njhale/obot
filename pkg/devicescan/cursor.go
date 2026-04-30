package devicescan

import (
	"io/fs"
	"path"
)

// Cursor on-disk layout.
const (
	cursorGlobalConfigRel     = ".cursor/mcp.json"
	cursorProjectMarkerName   = "mcp.json"
	cursorProjectMarkerParent = ".cursor"
	cursorSettingsRel         = ".cursor/settings.json"
	cursorPluginCacheRel      = ".cursor/plugins/cache/cursor-public"
	cursorPluginManifestSub   = ".cursor-plugin/plugin.json"
	cursorMarketplace         = "cursor-public"
)

var cursorDef = clientDef{
	name: "cursor",
	projectMarkers: []markerRule{
		{basename: cursorProjectMarkerName, parent: cursorProjectMarkerParent},
	},
	scanGlobal:  scanCursorGlobal,
	scanProject: scanCursorProject,
}

func scanCursorGlobal(r *Result) {
	emitJSONServersGlobal(r, cursorGlobalConfigRel, "mcpServers", "cursor")
}

func scanCursorProject(r *Result, markerRel string) {
	emitJSONServersProject(r, markerRel, "mcpServers", "cursor", r.abs(path.Dir(path.Dir(markerRel))))
}

// scanCursorPlugins walks ~/.cursor/plugins/cache/cursor-public/<name>/<hash>/
// looking for .cursor-plugin/plugin.json manifests. Mirrors runlayer's
// scan_cursor_native_plugins: dedup by plugin name (first hash dir wins),
// resolve enabled state from .cursor/settings.json with two key forms.
func scanCursorPlugins(r *Result) {
	plugins, err := fs.ReadDir(r.fsys, cursorPluginCacheRel)
	if err != nil {
		return
	}
	enabledByKey := readEnabledPluginsMap(r.fsys, cursorSettingsRel)
	seen := map[string]bool{}

	for _, p := range plugins {
		if !p.IsDir() || seen[p.Name()] {
			continue
		}
		pluginRel := path.Join(cursorPluginCacheRel, p.Name())
		hashes, err := fs.ReadDir(r.fsys, pluginRel)
		if err != nil {
			continue
		}
		for _, h := range hashes {
			if !h.IsDir() {
				continue
			}
			installRel := path.Join(pluginRel, h.Name())
			manifestRel := path.Join(installRel, cursorPluginManifestSub)
			if !fileExists(r.fsys, manifestRel) {
				continue
			}

			enabled := false
			for _, key := range []string{p.Name() + "@" + cursorMarketplace, p.Name()} {
				if v, ok := enabledByKey[key]; ok {
					enabled = v
					break
				}
			}

			emitPlugin(r, emitPluginOpts{
				installRel:   installRel,
				manifestRel:  manifestRel,
				pluginType:   "cursor_plugin",
				client:       "cursor",
				scope:        "global",
				marketplace:  cursorMarketplace,
				enabled:      enabled,
				nameFallback: p.Name(),
				nestedMCPRel: []string{"mcp.json", ".mcp.json"},
			})
			seen[p.Name()] = true
			break
		}
	}
}
