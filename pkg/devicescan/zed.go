package devicescan

import (
	"io/fs"
	"path"
	"strings"

	"github.com/obot-platform/obot/apiclient/types"
)

// Zed on-disk layout. macOS-style extensions path. Other platforms expose
// their extensions under different roots that we do not currently scan.
const (
	zedGlobalConfigRel     = ".config/zed/settings.json"
	zedProjectMarkerName   = "settings.json"
	zedProjectMarkerParent = ".zed"
	zedExtensionsRel       = "Library/Application Support/Zed/extensions/installed"
	zedExtensionPrefix     = "mcp-server-"
	zedServersKey          = "context_servers"
)

var zedDef = clientDef{
	name: "zed",
	projectMarkers: []markerRule{
		{basename: zedProjectMarkerName, parent: zedProjectMarkerParent},
	},
	scanGlobal:  scanZedGlobal,
	scanProject: scanZedProject,
}

func scanZedGlobal(r *Result) {
	cfg := readJSONFile(r.fsys, zedGlobalConfigRel)
	if cfg == nil {
		// Even if there is no settings.json we still want extensions-only
		// installations to surface.
		mergeZedExtensions(r, "", nil)
		return
	}
	r.markGlobalConfig(zedGlobalConfigRel)
	configAbs := r.abs(zedGlobalConfigRel)

	servers, _ := cfg[zedServersKey].(map[string]any)
	existing := emitZedContextServers(r, servers, "global", configAbs, "")
	mergeZedExtensions(r, configAbs, existing)
}

func scanZedProject(r *Result, markerRel string) {
	cfg := readJSONFile(r.fsys, markerRel)
	if cfg == nil {
		return
	}
	configAbs := r.abs(markerRel)
	servers, _ := cfg[zedServersKey].(map[string]any)
	projectAbs := r.abs(path.Dir(path.Dir(markerRel)))
	emitZedContextServers(r, servers, "project", configAbs, projectAbs)
}

// emitZedContextServers parses Zed's context_servers map. Returns the set
// of server names that were emitted so the extensions merge can dedupe.
func emitZedContextServers(r *Result, servers map[string]any, scope, configAbs, projectAbs string) map[string]bool {
	emitted := map[string]bool{}
	if servers == nil {
		return emitted
	}
	for name, raw := range servers {
		entry, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		obs, ok := zedContextServerEntry(name, entry, scope, configAbs, projectAbs)
		if !ok {
			continue
		}
		r.AddMCPServer(obs)
		emitted[name] = true
	}
	return emitted
}

// zedContextServerEntry mirrors runlayer's _parse_zed_context_server.
// `enabled: false` skips; `url` -> sse; `command` -> stdio; entries with
// neither (settings-only extension placeholders) are dropped.
func zedContextServerEntry(name string, raw map[string]any, scope, configAbs, projectAbs string) (types.DeviceScanMCPServer, bool) {
	if enabled, ok := raw["enabled"].(bool); ok && !enabled {
		return types.DeviceScanMCPServer{}, false
	}

	if url := asString(raw["url"]); url != "" {
		env := asMap(raw["env"])
		headers := asMap(raw["headers"])
		return types.DeviceScanMCPServer{
			Client:      "zed",
			Scope:       scope,
			ProjectPath: projectAbs,
			ConfigFile:  configAbs,
			Name:        name,
			Transport:   "sse",
			URL:         url,
			EnvKeys:     sortedKeys(env),
			HeaderKeys:  sortedKeys(headers),
			ConfigHash:  mcpConfigHash(name, "sse", "", nil, url),
		}, true
	}

	if cmd := asString(raw["command"]); cmd != "" {
		args := asStringSlice(raw["args"])
		env := asMap(raw["env"])
		return types.DeviceScanMCPServer{
			Client:      "zed",
			Scope:       scope,
			ProjectPath: projectAbs,
			ConfigFile:  configAbs,
			Name:        name,
			Transport:   "stdio",
			Command:     cmd,
			Args:        args,
			EnvKeys:     sortedKeys(env),
			HeaderKeys:  []string{},
			ConfigHash:  mcpConfigHash(name, "stdio", cmd, args, ""),
		}, true
	}

	return types.DeviceScanMCPServer{}, false
}

// mergeZedExtensions scans the macOS extensions tree for folders prefixed
// with mcp-server- and emits a stdio observation for each name not already
// present in the parsed settings. The extension itself supplies the
// command/args at runtime, so we leave them blank.
func mergeZedExtensions(r *Result, configAbs string, existing map[string]bool) {
	entries, err := fs.ReadDir(r.fsys, zedExtensionsRel)
	if err != nil {
		return
	}
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		if !strings.HasPrefix(e.Name(), zedExtensionPrefix) {
			continue
		}
		name := e.Name()
		if existing != nil && existing[name] {
			continue
		}
		r.AddMCPServer(types.DeviceScanMCPServer{
			Client:     "zed",
			Scope:      "global",
			ConfigFile: configAbs,
			Name:       name,
			Transport:  "stdio",
			EnvKeys:    []string{},
			HeaderKeys: []string{},
			ConfigHash: mcpConfigHash(name, "stdio", "", nil, ""),
		})
	}
}
