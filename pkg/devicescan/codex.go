package devicescan

import (
	"io/fs"
	"path"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/BurntSushi/toml"
	"github.com/obot-platform/obot/apiclient/types"
)

// Codex on-disk layout. Paths are relative to the home fs.
const (
	codexGlobalConfigRel     = ".codex/config.toml"
	codexProjectMarkerName   = "config.toml"
	codexProjectMarkerParent = ".codex"
	codexPluginCacheRel      = ".codex/plugins/cache"
	codexPluginManifestSub   = ".codex-plugin/plugin.json"
)

var codexDef = clientDef{
	name: "codex",
	projectMarkers: []markerRule{
		{basename: codexProjectMarkerName, parent: codexProjectMarkerParent},
	},
	scanGlobal:  scanCodexGlobal,
	scanProject: scanCodexProject,
}

func scanCodexGlobal(r *Result) {
	cfg := readTOMLFile(r.fsys, codexGlobalConfigRel)
	if cfg == nil {
		return
	}
	r.markGlobalConfig(codexGlobalConfigRel)
	configAbs := r.abs(codexGlobalConfigRel)
	emitCodexServers(r, cfg, "global", configAbs, "")
}

// Project-scope marker is <project>/.codex/config.toml. Project root =
// parent of the .codex/ directory.
func scanCodexProject(r *Result, markerRel string) {
	cfg := readTOMLFile(r.fsys, markerRel)
	if cfg == nil {
		return
	}
	configAbs := r.abs(markerRel)
	projectAbs := r.abs(path.Dir(path.Dir(markerRel)))
	emitCodexServers(r, cfg, "project", configAbs, projectAbs)
}

// emitCodexServers walks the [mcp_servers.<name>] tables in a parsed Codex
// TOML config and emits one MCP server observation per enabled entry.
func emitCodexServers(r *Result, cfg map[string]any, scope, configAbs, projectPathAbs string) {
	servers, ok := cfg["mcp_servers"].(map[string]any)
	if !ok {
		return
	}
	for name, raw := range servers {
		entry, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		// Skip explicitly disabled servers.
		if enabled, ok := entry["enabled"].(bool); ok && !enabled {
			continue
		}
		r.AddMCPServer(codexServerEntry(name, entry, scope, configAbs, "", projectPathAbs))
	}
}

// codexServerEntry converts one [mcp_servers.<name>] table into wire shape.
// Codex has unique header semantics: http_headers (literal map),
// env_http_headers (header_name → env_var, stored as ${VAR} placeholder),
// and bearer_token_env_var (yields Authorization: "Bearer ${VAR}"). Only
// the header *names* propagate to the wire HeaderKeys field.
func codexServerEntry(name string, raw map[string]any, scope, configAbs, pluginFileAbs, projectPathAbs string) types.DeviceScanMCPServer {
	transport := codexTransport(raw)
	cmd := asString(raw["command"])
	args := asStringSlice(raw["args"])
	url := asString(raw["url"])
	env := asMap(raw["env"])

	headerNames := map[string]struct{}{}
	if h, ok := raw["http_headers"].(map[string]any); ok {
		for k := range h {
			headerNames[k] = struct{}{}
		}
	}
	if eh, ok := raw["env_http_headers"].(map[string]any); ok {
		for k := range eh {
			headerNames[k] = struct{}{}
		}
	}
	if bt := asString(raw["bearer_token_env_var"]); bt != "" {
		headerNames["Authorization"] = struct{}{}
	}
	headerKeys := make([]string, 0, len(headerNames))
	for k := range headerNames {
		headerKeys = append(headerKeys, k)
	}
	sort.Strings(headerKeys)

	return types.DeviceScanMCPServer{
		Client:      "codex",
		Scope:       scope,
		ProjectPath: projectPathAbs,
		ConfigFile:  configAbs,
		PluginFile:  pluginFileAbs,
		Name:        name,
		Transport:   transport,
		Command:     cmd,
		Args:        args,
		URL:         url,
		EnvKeys:     sortedKeys(env),
		HeaderKeys:  headerKeys,
		ConfigHash:  mcpConfigHash(name, transport, cmd, args, url),
	}
}

func codexTransport(raw map[string]any) string {
	if explicit := firstNonEmpty(asString(raw["type"]), asString(raw["transport"])); explicit != "" {
		n := strings.ReplaceAll(strings.ToLower(strings.TrimSpace(explicit)), "_", "-")
		if n == "streamablehttp" {
			n = "streamable-http"
		}
		return n
	}
	if asString(raw["url"]) != "" {
		return "streamable-http"
	}
	return "stdio"
}

func readTOMLFile(fsys fs.FS, rel string) map[string]any {
	data, err := fs.ReadFile(fsys, rel)
	if err != nil {
		return nil
	}
	var out map[string]any
	if _, err := toml.Decode(string(data), &out); err != nil {
		return nil
	}
	return out
}

// scanCodexPlugins walks .codex/plugins/cache/<marketplace>/<plugin>/<ver>/
// and emits a Plugin observation for the highest-semver version of each
// plugin that has a manifest at .codex-plugin/plugin.json.
func scanCodexPlugins(r *Result) {
	mkts, err := fs.ReadDir(r.fsys, codexPluginCacheRel)
	if err != nil {
		return
	}
	for _, mkt := range mkts {
		if !mkt.IsDir() {
			continue
		}
		mktRel := path.Join(codexPluginCacheRel, mkt.Name())
		plugins, err := fs.ReadDir(r.fsys, mktRel)
		if err != nil {
			continue
		}
		for _, p := range plugins {
			if !p.IsDir() {
				continue
			}
			pluginRel := path.Join(mktRel, p.Name())
			versionRel, version, ok := pickHighestVersionDir(r.fsys, pluginRel)
			if !ok {
				continue
			}
			manifestRel := path.Join(versionRel, codexPluginManifestSub)
			if !fileExists(r.fsys, manifestRel) {
				continue
			}
			emitPlugin(r, emitPluginOpts{
				installRel:      versionRel,
				manifestRel:     manifestRel,
				pluginType:      "codex_plugin",
				client:          "codex",
				scope:           "global",
				marketplace:     mkt.Name(),
				enabled:         true,
				nameFallback:    p.Name(),
				versionFallback: version,
				nestedMCPRel:    []string{"mcp.json", ".mcp.json"},
			})
		}
	}
}

// pickHighestVersionDir returns the version subdirectory with the highest
// semver-aware key, the directory's basename (the version string), and ok.
// Non-directory entries are ignored.
func pickHighestVersionDir(fsys fs.FS, pluginRel string) (string, string, bool) {
	entries, err := fs.ReadDir(fsys, pluginRel)
	if err != nil {
		return "", "", false
	}
	type cand struct {
		name string
		key  []vPart
	}
	cands := make([]cand, 0, len(entries))
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		cands = append(cands, cand{name: e.Name(), key: versionSortKey(e.Name())})
	}
	if len(cands) == 0 {
		return "", "", false
	}
	sort.Slice(cands, func(i, j int) bool {
		return compareVParts(cands[i].key, cands[j].key) > 0 // descending
	})
	top := cands[0].name
	return path.Join(pluginRel, top), top, true
}

// vPart is one segment of a parsed version string. Mirrors runlayer's
// _version_sort_key tuple: kind 0 = numeric segment / numeric prerelease,
// kind 1 = alpha segment / alpha prerelease token, kind 2 = release
// sentinel (appended when the version has no prerelease suffix).
type vPart struct {
	kind int
	n    int
	s    string
}

func versionSortKey(name string) []vPart {
	versionStr, prerelease, hasDash := strings.Cut(name, "-")
	var parts []vPart
	for seg := range strings.SplitSeq(versionStr, ".") {
		if n, err := strconv.Atoi(seg); err == nil {
			parts = append(parts, vPart{0, n, ""})
		} else {
			parts = append(parts, vPart{1, 0, seg})
		}
	}
	if hasDash && prerelease != "" {
		for _, tok := range alphaNumTokens(prerelease) {
			if n, err := strconv.Atoi(tok); err == nil {
				parts = append(parts, vPart{0, n, ""})
			} else {
				parts = append(parts, vPart{1, 0, tok})
			}
		}
	} else {
		parts = append(parts, vPart{2, 0, ""})
	}
	return parts
}

var alphaNumRe = regexp.MustCompile(`[A-Za-z]+|\d+`)

func alphaNumTokens(s string) []string {
	return alphaNumRe.FindAllString(s, -1)
}

func compareVParts(a, b []vPart) int {
	for i := 0; i < len(a) && i < len(b); i++ {
		if a[i].kind != b[i].kind {
			return a[i].kind - b[i].kind
		}
		if a[i].n != b[i].n {
			return a[i].n - b[i].n
		}
		if a[i].s != b[i].s {
			if a[i].s < b[i].s {
				return -1
			}
			return 1
		}
	}
	return len(a) - len(b)
}
