package devicescan

import (
	"context"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/obot-platform/obot/apiclient/types"
	"github.com/stretchr/testify/require"
)

// runScanFS runs Scan against an in-memory fs with a neutralised
// presence environment (no real $PATH, no /Applications). Returns the
// scan manifest for assertions.
func runScanFS(t *testing.T, files map[string]string) types.DeviceScanManifest {
	t.Helper()
	mapfs := fstest.MapFS{}
	for p, body := range files {
		mapfs[p] = &fstest.MapFile{Data: []byte(body)}
	}

	t.Setenv("PATH", t.TempDir())
	t.Setenv("OPENCLAW_PROFILE", "")
	clientAppBundleDirs = []string{t.TempDir()}
	t.Cleanup(func() { clientAppBundleDirs = nil })

	scan, err := Scan(context.Background(), mapfs, "/home/test", 8)
	if err != nil {
		t.Fatalf("Scan: %v", err)
	}
	return scan
}

// findServer returns the first MCP server matching client+name, or nil.
func findServer(scan types.DeviceScanManifest, client, name string) *types.DeviceScanMCPServer {
	for i, m := range scan.MCPServers {
		if m.Client == client && m.Name == name {
			return &scan.MCPServers[i]
		}
	}
	return nil
}

// TestScanners_Smoke covers each scanner with one happy-path config
// (stdio or http, whichever is most natural) and asserts the server is
// emitted with the expected client + transport. The orchestrator,
// walker, build(), and per-scanner toServer logic are all exercised.
func TestScanners_Smoke(t *testing.T) {
	cases := []struct {
		name      string
		client    string
		serverNm  string
		transport string
		files     map[string]string
	}{
		{
			name:      "claude_code stdio",
			client:    "claude_code",
			serverNm:  "github",
			transport: "stdio",
			files: map[string]string{
				".claude.json": `{"mcpServers":{"github":{"command":"npx","args":["-y","@modelcontextprotocol/server-github"]}}}`,
			},
		},
		{
			name:      "claude_desktop stdio",
			client:    "claude_desktop",
			serverNm:  "github",
			transport: "stdio",
			files: map[string]string{
				"Library/Application Support/Claude/claude_desktop_config.json": `{"mcpServers":{"github":{"command":"npx","args":["-y","x"]}}}`,
			},
		},
		{
			name:      "codex stdio",
			client:    "codex",
			serverNm:  "github",
			transport: "stdio",
			files: map[string]string{
				".codex/config.toml": "[mcp_servers.github]\ncommand = \"npx\"\nargs = [\"-y\", \"x\"]\n",
			},
		},
		{
			name:      "cursor stdio",
			client:    "cursor",
			serverNm:  "github",
			transport: "stdio",
			files: map[string]string{
				".cursor/mcp.json": `{"mcpServers":{"github":{"command":"npx","args":["-y","x"]}}}`,
			},
		},
		{
			name:      "goose stdio",
			client:    "goose",
			serverNm:  "github",
			transport: "stdio",
			files: map[string]string{
				".config/goose/config.yaml": "extensions:\n  github:\n    type: stdio\n    cmd: npx\n    args: [\"-y\", \"x\"]\n    enabled: true\n",
			},
		},
		{
			name:      "hermes http",
			client:    "hermes",
			serverNm:  "remote",
			transport: "streamable-http",
			files: map[string]string{
				".hermes/config.yaml": "mcp_servers:\n  remote:\n    url: https://mcp.example.com/mcp\n",
			},
		},
		{
			name:      "opencode local",
			client:    "opencode",
			serverNm:  "github",
			transport: "stdio",
			files: map[string]string{
				".config/opencode/opencode.json": `{"mcp":{"github":{"type":"local","command":["npx","-y","x"]}}}`,
			},
		},
		{
			name:      "vscode stdio",
			client:    "vscode",
			serverNm:  "github",
			transport: "stdio",
			files: map[string]string{
				"Library/Application Support/Code/User/mcp.json": `{"servers":{"github":{"command":"npx","args":["-y","x"]}}}`,
			},
		},
		{
			name:      "windsurf stdio",
			client:    "windsurf",
			serverNm:  "github",
			transport: "stdio",
			files: map[string]string{
				".codeium/windsurf/mcp_config.json": `{"mcpServers":{"github":{"command":"npx","args":["-y","x"]}}}`,
			},
		},
		{
			name:      "zed stdio",
			client:    "zed",
			serverNm:  "github",
			transport: "stdio",
			files: map[string]string{
				".config/zed/settings.json": `{"context_servers":{"github":{"command":"npx","args":["-y","x"]}}}`,
			},
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			scan := runScanFS(t, c.files)
			s := findServer(scan, c.client, c.serverNm)
			if s == nil {
				t.Fatalf("no server emitted for client=%q name=%q; got %+v", c.client, c.serverNm, scan.MCPServers)
			}
			if s.Transport != c.transport {
				t.Errorf("Transport = %q, want %q", s.Transport, c.transport)
			}
			if s.ConfigHash == "" {
				t.Errorf("ConfigHash empty")
			}
			// build() must synthesise a clients[] row whenever an
			// observation references a client, even if presence didn't
			// fire in the test environment.
			var clientFound bool
			for _, cl := range scan.Clients {
				if cl.Name == c.client {
					clientFound = true
					if !cl.HasMCPServers {
						t.Errorf("HasMCPServers = false for client %q", c.client)
					}
				}
			}
			if !clientFound {
				t.Errorf("no clients[] row synthesised for %q", c.client)
			}
		})
	}
}

// TestScan_DisabledServerSkipped covers the per-scanner rule that an
// explicit `enabled = false` removes a server from the output. Codex
// (TOML) is exercised here; hermes_test.go covers the YAML path; goose
// inverts the default (must be explicit true).
func TestScan_DisabledServerSkipped(t *testing.T) {
	scan := runScanFS(t, map[string]string{
		".codex/config.toml": "[mcp_servers.on]\ncommand = \"x\"\n\n[mcp_servers.off]\ncommand = \"y\"\nenabled = false\n",
	})
	if findServer(scan, "codex", "off") != nil {
		t.Errorf("disabled server emitted")
	}
	if findServer(scan, "codex", "on") == nil {
		t.Errorf("enabled server missing")
	}
}

// TestScan_ProjectScopeWalk verifies the walker dispatches a
// project-scope config to its owning scanner with the project root
// resolved correctly.
func TestScan_ProjectScopeWalk(t *testing.T) {
	scan := runScanFS(t, map[string]string{
		"projects/foo/.cursor/mcp.json": `{"mcpServers":{"github":{"command":"npx"}}}`,
	})
	s := findServer(scan, "cursor", "github")
	if s == nil {
		t.Fatalf("no project-scope server emitted; got %+v", scan.MCPServers)
	}
	if s.ProjectPath == "" {
		t.Errorf("ProjectPath empty for project-scope hit; want non-empty")
	}
}

// findSkillByFileSuffix returns the first skill whose SKILL.md absolute
// path ends with suffix, or nil.
func findSkillByFileSuffix(scan types.DeviceScanManifest, suffix string) *types.DeviceScanSkill {
	for i, sk := range scan.Skills {
		if strings.HasSuffix(sk.File, suffix) {
			return &scan.Skills[i]
		}
	}
	return nil
}

// TestAgentsSkillsScanGlobalScope covers the ~/.agents/skills (and
// ~/.agent/skills) convention: skills under those paths are emitted as
// client=multi with empty ProjectPath (global scope) at arbitrary nesting
// depth, while a free-floating multi skill outside that tree stays
// project-scoped.
func TestAgentsSkillsScanGlobalScope(t *testing.T) {
	scan := runScanFS(t, map[string]string{
		".agents/skills/foo/SKILL.md":     "---\nname: foo\n---\nbody",
		".agent/skills/bar/SKILL.md":      "---\nname: bar\n---\nbody",
		".agents/skills/cat/baz/SKILL.md": "---\nname: baz\n---\nbody",
		"myproj/skills/qux/SKILL.md":      "---\nname: qux\n---\nbody",
	})

	foo := findSkillByFileSuffix(scan, ".agents/skills/foo/SKILL.md")
	require.NotNil(t, foo, "agents-skills depth-1 skill missing")
	require.Equal(t, "multi", foo.Client)
	require.Empty(t, foo.ProjectPath, "agents-skills row should be global-scoped")

	bar := findSkillByFileSuffix(scan, ".agent/skills/bar/SKILL.md")
	require.NotNil(t, bar, "agent-skills singular variant missing")
	require.Equal(t, "multi", bar.Client)
	require.Empty(t, bar.ProjectPath, ".agent/skills row should be global-scoped")

	baz := findSkillByFileSuffix(scan, ".agents/skills/cat/baz/SKILL.md")
	require.NotNil(t, baz, "nested agents-skills row missing")
	require.Equal(t, "multi", baz.Client)
	require.Empty(t, baz.ProjectPath, "nested agents-skills row should be global-scoped")

	qux := findSkillByFileSuffix(scan, "myproj/skills/qux/SKILL.md")
	require.NotNil(t, qux, "free-floating project-multi skill missing")
	require.Equal(t, "multi", qux.Client)
	require.NotEmpty(t, qux.ProjectPath, "free-floating multi row should stay project-scoped")
}

// TestAgentsSkillsSupportedClients guards the contract that consumers
// (gateway, CLI) rely on: cursor, vscode, opencode, and goose are all
// part of the exported supporting-clients list.
func TestAgentsSkillsSupportedClients(t *testing.T) {
	got := map[string]bool{}
	for _, c := range AgentsSkillsSupportedClients {
		got[c] = true
	}
	for _, want := range []string{"cursor", "vscode", "opencode", "goose"} {
		require.True(t, got[want], "AgentsSkillsSupportedClients missing %q", want)
	}
}
