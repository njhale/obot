package devicescan

import (
	"io/fs"
	"path"
	"slices"
	"strings"

	"github.com/obot-platform/obot/apiclient/types"
)

// OpenCode on-disk layout. Both opencode.json and opencode.jsonc are
// accepted; .jsonc with comments will fail stdlib JSON parsing — we
// silently skip rather than vendoring a JSON5 parser.
const (
	opencodeGlobalConfigJSONRel  = ".config/opencode/opencode.json"
	opencodeGlobalConfigJSONCRel = ".config/opencode/opencode.jsonc"
	opencodeProjectMarkerName    = "opencode.json"
	opencodeLocalPluginsRel      = ".config/opencode/plugins"
	opencodeNPMCacheRel          = ".cache/opencode/node_modules"
)

var opencodePluginExts = map[string]bool{
	".js":  true,
	".ts":  true,
	".mjs": true,
	".mts": true,
}

var opencodeDef = clientDef{
	name: "opencode",
	projectMarkers: []markerRule{
		{basename: opencodeProjectMarkerName},
	},
	scanGlobal:  scanOpenCodeGlobal,
	scanProject: scanOpenCodeProject,
}

func scanOpenCodeGlobal(r *Result) {
	for _, rel := range []string{opencodeGlobalConfigJSONRel, opencodeGlobalConfigJSONCRel} {
		cfg := readJSONFile(r.fsys, rel)
		if cfg == nil {
			continue
		}
		r.markGlobalConfig(rel)
		configAbs := r.abs(rel)
		emitOpenCodeServers(r, cfg, "global", configAbs, "")
	}
}

func scanOpenCodeProject(r *Result, markerRel string) {
	cfg := readJSONFile(r.fsys, markerRel)
	if cfg == nil {
		return
	}
	configAbs := r.abs(markerRel)
	projectAbs := r.abs(path.Dir(markerRel))
	emitOpenCodeServers(r, cfg, "project", configAbs, projectAbs)
}

func emitOpenCodeServers(r *Result, cfg map[string]any, scope, configAbs, projectAbs string) {
	servers, ok := cfg["mcp"].(map[string]any)
	if !ok {
		return
	}
	for name, raw := range servers {
		entry, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		obs, ok := opencodeServerEntry(name, entry, scope, configAbs, projectAbs)
		if !ok {
			continue
		}
		r.AddMCPServer(obs)
	}
}

// opencodeServerEntry mirrors runlayer's _parse_opencode_mcp_server.
// type=local: command is an array [cmd, ...args]; type=remote: url + headers.
func opencodeServerEntry(name string, raw map[string]any, scope, configAbs, projectAbs string) (types.DeviceScanMCPServer, bool) {
	if enabled, ok := raw["enabled"].(bool); ok && !enabled {
		return types.DeviceScanMCPServer{}, false
	}

	switch asString(raw["type"]) {
	case "local":
		commandList := asStringSlice(raw["command"])
		if len(commandList) == 0 {
			return types.DeviceScanMCPServer{}, false
		}
		cmd := commandList[0]
		var args []string
		if len(commandList) > 1 {
			args = commandList[1:]
		}
		env := asMap(raw["environment"])
		return types.DeviceScanMCPServer{
			Client:      "opencode",
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
	case "remote":
		url := asString(raw["url"])
		if url == "" {
			return types.DeviceScanMCPServer{}, false
		}
		headers := asMap(raw["headers"])
		return types.DeviceScanMCPServer{
			Client:      "opencode",
			Scope:       scope,
			ProjectPath: projectAbs,
			ConfigFile:  configAbs,
			Name:        name,
			Transport:   "http",
			URL:         url,
			EnvKeys:     []string{},
			HeaderKeys:  sortedKeys(headers),
			ConfigHash:  mcpConfigHash(name, "http", "", nil, url),
		}, true
	}
	return types.DeviceScanMCPServer{}, false
}

// scanOpenCodePlugins emits Plugin observations for OpenCode plugins. Two
// sources: subdirectories under ~/.config/opencode/plugins/, and npm
// packages listed under opencode.json's `plugin` array, found in
// ~/.cache/opencode/node_modules/<pkg>/.
func scanOpenCodePlugins(r *Result) {
	scanOpenCodeLocalPlugins(r)
	scanOpenCodeNPMPlugins(r)
}

func scanOpenCodeLocalPlugins(r *Result) {
	entries, err := fs.ReadDir(r.fsys, opencodeLocalPluginsRel)
	if err != nil {
		return
	}
	for _, e := range entries {
		itemRel := path.Join(opencodeLocalPluginsRel, e.Name())
		if e.IsDir() {
			emitOpenCodePluginDir(r, itemRel, e.Name(), "opencode_plugin", "")
			continue
		}
		if !opencodePluginExts[path.Ext(e.Name())] {
			continue
		}
		// Standalone plugin file. Emit minimal Plugin observation with the
		// single file ingested, matching runlayer's behaviour.
		fileAbs, err := r.AddFile(itemRel)
		if err != nil {
			continue
		}
		base := strings.TrimSuffix(e.Name(), path.Ext(e.Name()))
		r.AddPlugin(types.DeviceScanPlugin{
			Client:     "opencode",
			Scope:      "global",
			Name:       base,
			PluginType: "opencode_plugin",
			PluginFile: fileAbs,
			Enabled:    true,
			Files:      []string{fileAbs},
			HasHooks:   true,
		})
	}
}

func scanOpenCodeNPMPlugins(r *Result) {
	names := readOpenCodeNPMPluginNames(r.fsys, opencodeGlobalConfigJSONRel)
	for _, n := range readOpenCodeNPMPluginNames(r.fsys, opencodeGlobalConfigJSONCRel) {
		if !slices.Contains(names, n) {
			names = append(names, n)
		}
	}
	if len(names) == 0 {
		return
	}
	if !dirExists(r.fsys, opencodeNPMCacheRel) {
		return
	}
	for _, pkg := range names {
		pkgRel := path.Join(opencodeNPMCacheRel, pkg)
		if !dirExists(r.fsys, pkgRel) {
			continue
		}
		emitOpenCodePluginDir(r, pkgRel, pkg, "opencode_npm_plugin", "npm")
	}
}

// emitOpenCodePluginDir reads a plugin directory's package.json (if any)
// for metadata and produces a Plugin observation. nestedMCPRel is fixed to
// {mcp.json, .mcp.json} as in runlayer.
func emitOpenCodePluginDir(r *Result, installRel, fallbackName, pluginType, marketplace string) {
	packageRel := path.Join(installRel, "package.json")
	pkg := readJSONFile(r.fsys, packageRel)
	name, version, description, author := manifestMetadata(pkg)
	if name == "" {
		name = fallbackName
	}

	files, _ := r.collectArtifactFiles(installRel, pluginExts)
	hasMCP := fileExists(r.fsys, path.Join(installRel, "mcp.json")) ||
		fileExists(r.fsys, path.Join(installRel, ".mcp.json"))

	mcpRaw, mcpSourceRel := pluginMCPServersBlock(r.fsys, installRel, []string{"mcp.json", ".mcp.json"}, pkg, packageRel)
	mcpSourceAbs := ""
	if mcpSourceRel != "" {
		mcpSourceAbs = r.abs(mcpSourceRel)
	}
	pluginFileAbs := ""
	if pkg != nil {
		pluginFileAbs = r.abs(packageRel)
	}
	for serverName, raw := range mcpRaw {
		entry, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		obs := jsonServerEntry(serverName, entry, "opencode", "plugin", mcpSourceAbs, pluginFileAbs, "")
		r.AddMCPServer(obs)
	}
	if len(mcpRaw) > 0 {
		hasMCP = true
	}

	r.AddPlugin(types.DeviceScanPlugin{
		Client:        "opencode",
		Scope:         "global",
		Name:          name,
		PluginType:    pluginType,
		PluginFile:    pluginFileAbs,
		Version:       version,
		Description:   description,
		Author:        author,
		Enabled:       true,
		Marketplace:   marketplace,
		Files:         files,
		HasMCPServers: hasMCP,
		HasHooks:      true,
	})
}

func readOpenCodeNPMPluginNames(fsys fs.FS, rel string) []string {
	cfg := readJSONFile(fsys, rel)
	if cfg == nil {
		return nil
	}
	arr, ok := cfg["plugin"].([]any)
	if !ok {
		return nil
	}
	out := make([]string, 0, len(arr))
	for _, v := range arr {
		if s, ok := v.(string); ok {
			out = append(out, s)
		}
	}
	return out
}
