package devicescan

import (
	"strings"

	"github.com/obot-platform/obot/apiclient/types"
)

// Claude Desktop on-disk layout (macOS only — Windows %APPDATA% would not
// be reachable through os.DirFS rooted at $HOME, so the file simply will
// not exist there).
const (
	claudeDesktopExtRel    = "Library/Application Support/Claude/extensions-installations.json"
	claudeDesktopConfigRel = "Library/Application Support/Claude/claude_desktop_config.json"
)

var claudedesktopDef = clientDef{
	name:           "claude_desktop",
	projectMarkers: nil, // No project-level configs.
	scanGlobal:     scanClaudeDesktopGlobal,
	scanProject:    nil,
}

func scanClaudeDesktopGlobal(r *Result) {
	scanClaudeDesktopExtensions(r)
	emitJSONServersGlobal(r, claudeDesktopConfigRel, "mcpServers", "claude_desktop")
}

// scanClaudeDesktopExtensions parses extensions-installations.json. Each
// extension entry holds a nested `manifest.server.mcp_config` block in the
// same shape as a standard mcpServers entry; fall back to `entry_point` if
// mcp_config is absent.
func scanClaudeDesktopExtensions(r *Result) {
	cfg := readJSONFile(r.fsys, claudeDesktopExtRel)
	if cfg == nil {
		return
	}
	r.markGlobalConfig(claudeDesktopExtRel)
	configAbs := r.abs(claudeDesktopExtRel)

	exts, ok := cfg["extensions"].(map[string]any)
	if !ok {
		return
	}
	for name, raw := range exts {
		entry, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		r.AddMCPServer(claudeDesktopExtensionEntry(name, entry, configAbs))
	}
}

// claudeDesktopExtensionEntry mirrors runlayer's _parse_server_entry
// manifest branch.
func claudeDesktopExtensionEntry(name string, raw map[string]any, configAbs string) types.DeviceScanMCPServer {
	displayName := name
	var command, url string
	var args []string
	var env map[string]any
	transport := "stdio"

	manifest, _ := raw["manifest"].(map[string]any)
	if manifest != nil {
		if dn := asString(manifest["display_name"]); dn != "" {
			displayName = dn
		}
		serverInfo, _ := manifest["server"].(map[string]any)
		if serverInfo != nil {
			if t := asString(serverInfo["type"]); t != "" {
				transport = t
				if transport == "node" {
					transport = "stdio"
				}
			}
			if mcpCfg, ok := serverInfo["mcp_config"].(map[string]any); ok && mcpCfg != nil {
				command = asString(mcpCfg["command"])
				args = asStringSlice(mcpCfg["args"])
				env = asMap(mcpCfg["env"])
			} else if ep := asString(serverInfo["entry_point"]); ep != "" {
				parts := strings.Fields(ep)
				if len(parts) > 0 {
					command = parts[0]
					args = parts[1:]
				}
			}
		}
	}

	return types.DeviceScanMCPServer{
		Client:     "claude_desktop",
		Scope:      "global",
		ConfigFile: configAbs,
		Name:       displayName,
		Transport:  transport,
		Command:    command,
		Args:       args,
		URL:        url,
		EnvKeys:    sortedKeys(env),
		HeaderKeys: []string{},
		ConfigHash: mcpConfigHash(displayName, transport, command, args, url),
	}
}

// scanClaudeDesktopConnectors emits one DeviceScanPlugin
// (plugin_type=claude_desktop_connector) per entry in the
// claude_desktop_config.json mcpServers block. The MCP server observation
// itself is already produced by scanClaudeDesktopGlobal; the plugin entry
// captures the connector as a first-class artifact alongside it.
func scanClaudeDesktopConnectors(r *Result) {
	cfg := readJSONFile(r.fsys, claudeDesktopConfigRel)
	if cfg == nil {
		return
	}
	servers, ok := cfg["mcpServers"].(map[string]any)
	if !ok {
		return
	}
	configAbs := r.abs(claudeDesktopConfigRel)

	for name, raw := range servers {
		entry, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		hasCmd := asString(entry["command"]) != ""
		hasURL := asString(entry["url"]) != "" || asString(entry["serverUrl"]) != ""

		r.AddPlugin(types.DeviceScanPlugin{
			Client:        "claude_desktop",
			Scope:         "global",
			Name:          name,
			PluginType:    "claude_desktop_connector",
			Enabled:       true,
			Files:         []string{configAbs},
			HasMCPServers: hasCmd || hasURL,
		})
	}
}
