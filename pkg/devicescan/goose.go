package devicescan

import (
	"io/fs"
	"strings"

	"github.com/obot-platform/obot/apiclient/types"
	"gopkg.in/yaml.v3"
)

const gooseGlobalConfigRel = ".config/goose/config.yaml"

var gooseDef = clientDef{
	name:           "goose",
	projectMarkers: nil, // Goose has only a global config.
	scanGlobal:     scanGooseGlobal,
	scanProject:    nil,
}

// Goose stores extensions under top-level `extensions:` in YAML. Each
// entry has fields cmd/envs/uri (rather than command/env/url) and an
// `enabled` flag. The `type` field gates which entries are MCP-relevant —
// only stdio/sse/streamable_http are surfaced.
func scanGooseGlobal(r *Result) {
	cfg := readYAMLFile(r.fsys, gooseGlobalConfigRel)
	if cfg == nil {
		return
	}
	r.markGlobalConfig(gooseGlobalConfigRel)
	configAbs := r.abs(gooseGlobalConfigRel)

	exts, ok := cfg["extensions"].(map[string]any)
	if !ok {
		return
	}
	for key, raw := range exts {
		entry, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		obs, ok := gooseExtensionEntry(key, entry, configAbs)
		if !ok {
			continue
		}
		r.AddMCPServer(obs)
	}
}

func gooseExtensionEntry(key string, raw map[string]any, configAbs string) (types.DeviceScanMCPServer, bool) {
	if enabled, ok := raw["enabled"].(bool); !ok || !enabled {
		return types.DeviceScanMCPServer{}, false
	}
	extType := asString(raw["type"])
	switch extType {
	case "stdio", "sse", "streamable_http":
	default:
		return types.DeviceScanMCPServer{}, false
	}

	name := key
	if dn := asString(raw["name"]); dn != "" {
		name = dn
	}
	envs := asMap(raw["envs"])

	if extType == "stdio" {
		cmd := asString(raw["cmd"])
		args := asStringSlice(raw["args"])
		return types.DeviceScanMCPServer{
			Client:     "goose",
			Scope:      "global",
			ConfigFile: configAbs,
			Name:       name,
			Transport:  "stdio",
			Command:    cmd,
			Args:       args,
			EnvKeys:    sortedKeys(envs),
			HeaderKeys: []string{},
			ConfigHash: mcpConfigHash(name, "stdio", cmd, args, ""),
		}, true
	}

	transport := strings.ReplaceAll(extType, "_", "-")
	url := asString(raw["uri"])
	headers := asMap(raw["headers"])
	return types.DeviceScanMCPServer{
		Client:     "goose",
		Scope:      "global",
		ConfigFile: configAbs,
		Name:       name,
		Transport:  transport,
		URL:        url,
		EnvKeys:    sortedKeys(envs),
		HeaderKeys: sortedKeys(headers),
		ConfigHash: mcpConfigHash(name, transport, "", nil, url),
	}, true
}

func readYAMLFile(fsys fs.FS, rel string) map[string]any {
	data, err := fs.ReadFile(fsys, rel)
	if err != nil {
		return nil
	}
	var out map[string]any
	if err := yaml.Unmarshal(data, &out); err != nil {
		return nil
	}
	return out
}
