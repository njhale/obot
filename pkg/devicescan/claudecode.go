package devicescan

import (
	"bufio"
	"encoding/json"
	"io/fs"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/obot-platform/obot/apiclient/types"
)

const (
	claudeGlobalConfigRel     = ".claude.json"
	claudeSettingsRel         = ".claude/settings.json"
	claudeInstalledPluginsRel = ".claude/plugins/installed_plugins.json"
	claudePluginManifestSub   = ".claude-plugin/plugin.json"
	claudeProjectsRel         = ".claude/projects"

	// claudePromptPreviewRunes caps preview length so prompt text leaving
	// the device is bounded. 200 runes ≈ one tweet; enough to identify a
	// prompt by sight but not enough to leak whole prompts.
	claudePromptPreviewRunes = 200
	// claudePromptWindow is the lookback for which session files are
	// parsed; older files are skipped by mtime.
	claudePromptWindow = 30 * 24 * time.Hour
	// claudePromptScanBufferBytes raises bufio.Scanner's per-line limit;
	// transcript lines (especially attachment payloads) routinely exceed
	// the default 64 KiB.
	claudePromptScanBufferBytes = 10 << 20 // 10 MiB
)

// claudeCodeConfig is the shape of ~/.claude.json: a global mcpServers
// map plus a projects map keyed by absolute project path, each with its
// own mcpServers block.
type claudeCodeConfig struct {
	MCPServers map[string]mcpServerSpec `json:"mcpServers"`
	Projects   map[string]struct {
		MCPServers map[string]mcpServerSpec `json:"mcpServers"`
	} `json:"projects"`
}

// claudePluginsRegistry is the shape of installed_plugins.json: a
// `plugins` map keyed by "name@marketplace" → list of installations.
type claudePluginsRegistry struct {
	Plugins map[string][]struct {
		InstallPath string `json:"installPath"`
		Version     string `json:"version"`
	} `json:"plugins"`
}

// claudeSettings — only the field we read.
type claudeSettings struct {
	EnabledPlugins map[string]bool `json:"enabledPlugins"`
}

type claudeCodeScanner struct{}

func (claudeCodeScanner) Name() string { return "claude_code" }

func (claudeCodeScanner) Presence() clientPresenceDef {
	return clientPresenceDef{binaries: []string{"claude"}, configPaths: []string{".claude"}}
}

func (claudeCodeScanner) GlobalConfigPaths() []string { return []string{claudeGlobalConfigRel} }

func (claudeCodeScanner) ProjectGlobs() []string { return []string{"**/.mcp.json"} }

func (claudeCodeScanner) ScanGlobal(s *scanState) []types.DeviceScanMCPServer {
	cfg, ok := readJSON[claudeCodeConfig](s.fsys, claudeGlobalConfigRel)
	if !ok {
		return nil
	}
	configAbs := s.addFileOrAbs(claudeGlobalConfigRel)

	out := make([]types.DeviceScanMCPServer, 0, len(cfg.MCPServers))
	for name, e := range cfg.MCPServers {
		out = append(out, e.toServer(name, "claude_code", configAbs, ""))
	}
	for projKey, proj := range cfg.Projects {
		for name, e := range proj.MCPServers {
			out = append(out, e.toServer(name, "claude_code", configAbs, projKey))
		}
	}
	return out
}

func (claudeCodeScanner) ScanProject(s *scanState, configRel string) []types.DeviceScanMCPServer {
	projectAbs := s.abs(path.Dir(configRel))
	return emitJSONServersProject(s, configRel, "mcpServers", "claude_code", projectAbs)
}

// ScanPlugins reads installed_plugins.json and emits a Plugin observation
// (plus nested MCPServer / Skill observations) for each installation that
// resolves to a directory under the home fs.
func (claudeCodeScanner) ScanPlugins(s *scanState) (
	[]types.DeviceScanPlugin, []types.DeviceScanMCPServer, []types.DeviceScanSkill,
) {
	registry, ok := readJSON[claudePluginsRegistry](s.fsys, claudeInstalledPluginsRel)
	if !ok || len(registry.Plugins) == 0 {
		return nil, nil, nil
	}

	settings, _ := readJSON[claudeSettings](s.fsys, claudeSettingsRel)

	var (
		ps  []types.DeviceScanPlugin
		ms  []types.DeviceScanMCPServer
		sks []types.DeviceScanSkill
	)
	for pluginKey, installs := range registry.Plugins {
		pluginName, marketplace := splitPluginKey(pluginKey)
		for _, install := range installs {
			if install.InstallPath == "" {
				continue
			}
			installRel, ok := relUnderHome(s.homeAbs, install.InstallPath)
			if !ok || !dirExists(s.fsys, installRel) {
				continue
			}
			manifestRel := path.Join(installRel, claudePluginManifestSub)
			if !fileExists(s.fsys, manifestRel) {
				continue
			}
			ep := emitPlugin(s, emitPluginOpts{
				installRel:      installRel,
				manifestRel:     manifestRel,
				pluginType:      "claude_code_plugin",
				client:          "claude_code",
				marketplace:     marketplace,
				enabled:         settings.EnabledPlugins[pluginKey],
				nameFallback:    pluginName,
				versionFallback: install.Version,
				nestedMCPRel:    []string{"mcp.json", ".mcp.json"},
				mcpServerXform:  substituteClaudePluginRoot(install.InstallPath),
			})
			ps = append(ps, ep.plugin)
			ms = append(ms, ep.servers...)
			sks = append(sks, ep.skills...)
		}
	}
	return ps, ms, sks
}

// substituteClaudePluginRoot returns an mcpServerXform that replaces
// ${CLAUDE_PLUGIN_ROOT} with installPathAbs in the command, args, env,
// and url fields of a parsed mcpServerSpec.
func substituteClaudePluginRoot(installPathAbs string) func(*mcpServerSpec) {
	return func(e *mcpServerSpec) {
		sub := func(s string) string {
			return strings.ReplaceAll(s, "${CLAUDE_PLUGIN_ROOT}", installPathAbs)
		}
		e.Command = sub(e.Command)
		e.URL = sub(e.URL)
		for i, a := range e.Args {
			e.Args[i] = sub(a)
		}
		for k, v := range e.Env {
			if str, ok := v.(string); ok {
				e.Env[k] = sub(str)
			}
		}
	}
}

// readEnabledPluginsMap reads enabledPlugins from a settings file (used
// by Cursor as well as Claude Code) and returns it as map[key]bool.
func readEnabledPluginsMap(fsys fs.FS, rel string) map[string]bool {
	type settings struct {
		EnabledPlugins map[string]bool `json:"enabledPlugins"`
	}
	out, ok := readJSON[settings](fsys, rel)
	if !ok {
		return nil
	}
	return out.EnabledPlugins
}

// splitPluginKey separates "name@marketplace" plugin keys into their parts.
func splitPluginKey(key string) (name, marketplace string) {
	at := strings.IndexByte(key, '@')
	if at < 0 {
		return key, ""
	}
	return key[:at], key[at+1:]
}

// ScanPrompts walks Claude Code's per-session transcript JSONL files
// under ~/.claude/projects and returns the top-K originating user
// prompts ranked by total tokens. A bucket spans from one originating
// user turn to the next; every assistant turn in between (including
// sub-agents, which share the same file linked by parentUuid) rolls up.
// Preview is the first claudePromptPreviewRunes runes of the prompt
// text. Files older than claudePromptWindow are skipped by mtime.
func (claudeCodeScanner) ScanPrompts(s *scanState, topK int) []types.DeviceScanPrompt {
	if topK <= 0 {
		return nil
	}
	dirs, err := fs.ReadDir(s.fsys, claudeProjectsRel)
	if err != nil {
		return nil
	}
	cutoff := time.Now().Add(-claudePromptWindow)

	var out []types.DeviceScanPrompt
	for _, d := range dirs {
		if !d.IsDir() {
			continue
		}
		dirRel := path.Join(claudeProjectsRel, d.Name())
		files, err := fs.ReadDir(s.fsys, dirRel)
		if err != nil {
			continue
		}
		for _, f := range files {
			if f.IsDir() || !strings.HasSuffix(f.Name(), ".jsonl") {
				continue
			}
			info, err := f.Info()
			if err != nil || info.ModTime().Before(cutoff) {
				continue
			}
			out = append(out, parseClaudeCodeTranscript(s.fsys, path.Join(dirRel, f.Name()))...)
		}
	}

	sort.Slice(out, func(i, j int) bool { return out[i].TotalTokens > out[j].TotalTokens })
	if len(out) > topK {
		out = out[:topK]
	}
	return out
}

// claudeCodeTurnRecord is the minimal subset of a Claude Code transcript
// record we decode. Content is RawMessage so we can cheaply tell a
// user-typed prompt (string body) from a tool-result user record (array
// body) without fully unmarshalling tool result payloads.
type claudeCodeTurnRecord struct {
	Type        string    `json:"type"`
	UUID        string    `json:"uuid"`
	SessionID   string    `json:"sessionId"`
	IsSidechain bool      `json:"isSidechain"`
	Timestamp   time.Time `json:"timestamp"`
	CWD         string    `json:"cwd"`
	Message     struct {
		Role    string          `json:"role"`
		Content json.RawMessage `json:"content"`
		Usage   struct {
			InputTokens              int64 `json:"input_tokens"`
			OutputTokens             int64 `json:"output_tokens"`
			CacheCreationInputTokens int64 `json:"cache_creation_input_tokens"`
			CacheReadInputTokens     int64 `json:"cache_read_input_tokens"`
		} `json:"usage"`
	} `json:"message"`
}

// parseClaudeCodeTranscript streams one session JSONL file and produces
// one DeviceScanPrompt per originating user turn. Records appear in
// chronological order in the file, so a single forward pass aggregates
// every assistant turn (including sub-agents) into the current bucket.
func parseClaudeCodeTranscript(fsys fs.FS, rel string) []types.DeviceScanPrompt {
	f, err := fsys.Open(rel)
	if err != nil {
		return nil
	}
	defer f.Close()

	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 64*1024), claudePromptScanBufferBytes)

	var (
		out     []types.DeviceScanPrompt
		current *types.DeviceScanPrompt
	)
	flush := func() {
		if current != nil {
			current.TotalTokens = current.InputTokens + current.OutputTokens +
				current.CacheCreateTokens + current.CacheReadTokens
			out = append(out, *current)
			current = nil
		}
	}

	for sc.Scan() {
		var r claudeCodeTurnRecord
		if err := json.Unmarshal(sc.Bytes(), &r); err != nil {
			continue
		}
		switch r.Type {
		case "user":
			var promptText string
			if err := json.Unmarshal(r.Message.Content, &promptText); err != nil {
				// Non-string content (tool_result array) folds into the
				// current bucket rather than starting a new one.
				continue
			}
			flush()
			current = &types.DeviceScanPrompt{
				Client:      "claude_code",
				SessionID:   r.SessionID,
				TurnUUID:    r.UUID,
				ProjectPath: r.CWD,
				Preview:     truncateRunes(promptText, claudePromptPreviewRunes),
				StartedAt:   types.Time{Time: r.Timestamp},
				EndedAt:     types.Time{Time: r.Timestamp},
			}
		case "assistant":
			if current == nil {
				continue
			}
			current.InputTokens += r.Message.Usage.InputTokens
			current.OutputTokens += r.Message.Usage.OutputTokens
			current.CacheCreateTokens += r.Message.Usage.CacheCreationInputTokens
			current.CacheReadTokens += r.Message.Usage.CacheReadInputTokens
			if r.Timestamp.After(current.EndedAt.Time) {
				current.EndedAt = types.Time{Time: r.Timestamp}
			}
		}
	}
	flush()
	return out
}

// truncateRunes returns s with at most n runes. Safe across multi-byte
// UTF-8 boundaries.
func truncateRunes(s string, n int) string {
	if n <= 0 {
		return ""
	}
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n])
}

// relUnderHome converts an absolute path into its fs-relative form when
// the path lies under homeAbs. ok=false otherwise.
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
