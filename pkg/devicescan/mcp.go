package devicescan

import (
	"path"
	"sort"
	"strings"

	"github.com/obot-platform/obot/apiclient/types"
)

// clientDef registers a client with the orchestrator. Per-client behaviour
// lives in scanGlobal / scanProject closures so each client can use the
// format and quirks it needs (JSON vs TOML, custom header rules, etc.)
// without leaking those into the shared layer.
type clientDef struct {
	name           string
	projectMarkers []markerRule

	// scanGlobal opens the client's known global config path(s) and emits
	// MCP server observations via r.AddMCPServer. nil disables Phase 1 for
	// the client.
	scanGlobal func(r *Result)

	// scanProject parses a single project-scope marker file (relative to
	// fsys) and emits MCP server observations. nil disables Phase 3 for
	// this client.
	scanProject func(r *Result, markerRel string)
}

// clientDefs is the static registry consumed by the Scan pipeline.
// Entries are declared in their per-client files (claudecode.go, codex.go)
// so each registration sits next to the code that implements it.
var clientDefs = []clientDef{
	claudecodeDef,
	codexDef,
	cursorDef,
	vscodeDef,
	windsurfDef,
	claudedesktopDef,
	gooseDef,
	zedDef,
	opencodeDef,
}

func parseGlobalMCPConfig(r *Result, def clientDef) {
	if def.scanGlobal == nil {
		return
	}
	def.scanGlobal(r)
}

// parseProjectMCPConfigs dispatches each marker hit (excluding SKILL.md) to
// the first client whose marker rule matches. SKILL.md hits are owned by
// scanProjectSkills and skipped here. Markers whose rel path was registered
// as a global config in Phase 1 are skipped to avoid double-emission.
func parseProjectMCPConfigs(r *Result, markers []string) {
	for _, m := range markers {
		if path.Base(m) == "SKILL.md" {
			continue
		}
		if r.isGlobalConfig(m) {
			continue
		}
		for i := range clientDefs {
			def := clientDefs[i]
			if def.scanProject == nil {
				continue
			}
			if !markerMatches(def.projectMarkers, m) {
				continue
			}
			def.scanProject(r, m)
			break
		}
	}
}

func markerMatches(rules []markerRule, rel string) bool {
	base := path.Base(rel)
	parent := path.Base(path.Dir(rel))
	for _, r := range rules {
		if r.basename != base {
			continue
		}
		if r.parent != "" && r.parent != parent {
			continue
		}
		return true
	}
	return false
}

// ---------------------------------------------------------------------------
// JSON helpers shared by JSON-format clients (Claude Code, Cursor, VS Code,
// Zed, OpenCode). Codex provides its own TOML-aware parser in codex.go and
// does not consume these.
// ---------------------------------------------------------------------------

// jsonTransport returns the wire transport string for a JSON server entry.
// Mirrors runlayer's _parse_server_entry: explicit `type` or `transport`
// (lowercased, `_`→`-`, `streamablehttp`→`streamable-http`), or sse for
// remote servers (presence of url/serverUrl), or stdio otherwise.
func jsonTransport(raw map[string]any) string {
	explicit := firstNonEmpty(asString(raw["type"]), asString(raw["transport"]))
	if explicit != "" {
		n := strings.ReplaceAll(strings.ToLower(strings.TrimSpace(explicit)), "_", "-")
		if n == "streamablehttp" {
			n = "streamable-http"
		}
		return n
	}
	if firstNonEmpty(asString(raw["url"]), asString(raw["serverUrl"])) != "" {
		return "sse"
	}
	return "stdio"
}

// emitJSONServersGlobal opens a JSON file at configRel, marks it as a
// known global config, and emits one MCP server observation per entry
// found at the given top-level dictionary key (e.g. "mcpServers" or
// "servers"). Used by JSON-format clients with no client-specific quirks.
func emitJSONServersGlobal(r *Result, configRel, serversKey, client string) {
	cfg := readJSONFile(r.fsys, configRel)
	if cfg == nil {
		return
	}
	r.markGlobalConfig(configRel)
	configAbs := r.abs(configRel)
	servers, ok := cfg[serversKey].(map[string]any)
	if !ok {
		return
	}
	for name, raw := range servers {
		entry, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		r.AddMCPServer(jsonServerEntry(name, entry, client, "global", configAbs, "", ""))
	}
}

// emitJSONServersProject parses a project-scope JSON config file and emits
// MCP server observations with scope=project. Caller supplies projectAbs
// (the absolute project root path, derived per-client).
func emitJSONServersProject(r *Result, markerRel, serversKey, client, projectAbs string) {
	cfg := readJSONFile(r.fsys, markerRel)
	if cfg == nil {
		return
	}
	configAbs := r.abs(markerRel)
	servers, ok := cfg[serversKey].(map[string]any)
	if !ok {
		return
	}
	for name, raw := range servers {
		entry, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		r.AddMCPServer(jsonServerEntry(name, entry, client, "project", configAbs, "", projectAbs))
	}
}

// jsonServerEntry converts one entry from a JSON mcpServers map into wire
// shape. configFileAbs / pluginFileAbs / projectPathAbs are absolute paths
// embedded in the observation; pass "" if not applicable.
func jsonServerEntry(
	name string,
	raw map[string]any,
	client, scope string,
	configFileAbs, pluginFileAbs, projectPathAbs string,
) types.DeviceScanMCPServer {
	transport := jsonTransport(raw)
	cmd := asString(raw["command"])
	args := asStringSlice(raw["args"])
	url := firstNonEmpty(asString(raw["url"]), asString(raw["serverUrl"]))
	env := asMap(raw["env"])
	headers := asMap(raw["headers"])

	return types.DeviceScanMCPServer{
		Client:      client,
		Scope:       scope,
		ProjectPath: projectPathAbs,
		ConfigFile:  configFileAbs,
		PluginFile:  pluginFileAbs,
		Name:        name,
		Transport:   transport,
		Command:     cmd,
		Args:        args,
		URL:         url,
		EnvKeys:     sortedKeys(env),
		HeaderKeys:  sortedKeys(headers),
		ConfigHash:  mcpConfigHash(name, transport, cmd, args, url),
	}
}

// ---------------------------------------------------------------------------
// Generic any/json helpers
// ---------------------------------------------------------------------------

// asMap returns v as map[string]any if it is one, else nil.
func asMap(v any) map[string]any {
	m, _ := v.(map[string]any)
	return m
}

// asStringSlice returns v as []string by best-effort coercion of a []any
// whose elements are strings. Returns nil for any other shape.
func asStringSlice(v any) []string {
	arr, ok := v.([]any)
	if !ok {
		return nil
	}
	out := make([]string, 0, len(arr))
	for _, e := range arr {
		if s, ok := e.(string); ok {
			out = append(out, s)
		}
	}
	return out
}

// sortedKeys returns the keys of m in alphabetical order, or an empty
// (non-nil) slice if m is empty/nil.
func sortedKeys(m map[string]any) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// firstNonEmpty returns the first non-empty string, or "".
func firstNonEmpty(ss ...string) string {
	for _, s := range ss {
		if s != "" {
			return s
		}
	}
	return ""
}
