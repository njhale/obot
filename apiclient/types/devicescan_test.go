package types

import (
	"encoding/json"
	"testing"
	"time"
)

// prdExample is the example payload from prd.md §"Proposed Payload Shape".
// Kept verbatim so this test catches drift between the wire format and the
// PRD. The PRD includes os_version, which DeviceScan no longer carries —
// unmarshal silently drops it; subsequent re-marshals don't emit it.
const prdExample = `{
  "scanner_version": "0.1.0",
  "scanned_at": "2026-04-30T14:01:18Z",
  "device_id": "dev_7f4b4e9d8a2c",
  "hostname": "nicks-macbook-pro",
  "os": "darwin",
  "os_version": "15.4",
  "arch": "arm64",
  "username": "nick",
  "files": [
    {
      "path": "/Users/nick/.config/opencode/opencode.json",
      "size_bytes": 2048,
      "oversized": false
    },
    {
      "path": "/Users/nick/.claude/skills/code-review/SKILL.md",
      "size_bytes": 1420,
      "oversized": false,
      "content": "# Code Review\n\nUse this skill to review code changes..."
    }
  ],
  "mcp_servers": [
    {
      "client": "opencode",
      "scope": "global",
      "config_file": "/Users/nick/.config/opencode/opencode.json",
      "name": "github",
      "transport": "stdio",
      "command": "npx",
      "args": [
        "-y",
        "@modelcontextprotocol/server-github"
      ],
      "url": null,
      "env_keys": [
        "GITHUB_TOKEN"
      ],
      "header_keys": [],
      "config_hash": "mcp_config_hash_123"
    }
  ],
  "skills": [
    {
      "client": "claude_code",
      "scope": "user",
      "name": "code-review",
      "description": "Review code changes and identify risks.",
      "files": [
        "/Users/nick/.claude/skills/code-review/SKILL.md"
      ],
      "has_scripts": true,
      "git_remote_url": null
    }
  ],
  "plugins": [
    {
      "client": "codex",
      "scope": "global",
      "name": "Documents",
      "plugin_type": "codex_plugin",
      "version": "26.426.12240",
      "description": "Document editing support.",
      "author": "OpenAI",
      "enabled": true,
      "marketplace": "openai-primary-runtime",
      "files": [],
      "has_mcp_servers": true,
      "has_skills": true,
      "has_rules": false,
      "has_commands": false,
      "has_hooks": false
    }
  ]
}`

func TestDeviceScan_RoundTripIsIdempotent(t *testing.T) {
	var first DeviceScan
	if err := json.Unmarshal([]byte(prdExample), &first); err != nil {
		t.Fatalf("unmarshal PRD example: %v", err)
	}

	firstBytes, err := json.Marshal(first)
	if err != nil {
		t.Fatalf("marshal first: %v", err)
	}

	var second DeviceScan
	if err := json.Unmarshal(firstBytes, &second); err != nil {
		t.Fatalf("unmarshal first marshal: %v", err)
	}

	secondBytes, err := json.Marshal(second)
	if err != nil {
		t.Fatalf("marshal second: %v", err)
	}

	if string(firstBytes) != string(secondBytes) {
		t.Errorf("round-trip not idempotent\nfirst:  %s\nsecond: %s", firstBytes, secondBytes)
	}
}

func TestDeviceScan_PreservesPRDFields(t *testing.T) {
	var s DeviceScan
	if err := json.Unmarshal([]byte(prdExample), &s); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if s.ScannerVersion != "0.1.0" {
		t.Errorf("ScannerVersion = %q", s.ScannerVersion)
	}
	if got, want := s.ScannedAt.GetTime().UTC(), time.Date(2026, 4, 30, 14, 1, 18, 0, time.UTC); !got.Equal(want) {
		t.Errorf("ScannedAt = %v, want %v", got, want)
	}
	if s.DeviceID != "dev_7f4b4e9d8a2c" {
		t.Errorf("DeviceID = %q", s.DeviceID)
	}
	if s.OS != "darwin" || s.Arch != "arm64" || s.Username != "nick" {
		t.Errorf("env: os=%q arch=%q user=%q", s.OS, s.Arch, s.Username)
	}

	if len(s.MCPServers) != 1 || s.MCPServers[0].Name != "github" {
		t.Errorf("MCPServers: %+v", s.MCPServers)
	}
	if len(s.Skills) != 1 || s.Skills[0].Name != "code-review" {
		t.Errorf("Skills: %+v", s.Skills)
	}
	if len(s.Plugins) != 1 || s.Plugins[0].Name != "Documents" {
		t.Errorf("Plugins: %+v", s.Plugins)
	}
	if len(s.Files) != 2 {
		t.Errorf("Files len = %d", len(s.Files))
	}

	gh := s.MCPServers[0]
	if gh.Transport != "stdio" || gh.Command != "npx" || gh.ConfigHash != "mcp_config_hash_123" {
		t.Errorf("MCPServers[0] mismatch: %+v", gh)
	}
	if len(gh.Args) != 2 || gh.Args[0] != "-y" || gh.Args[1] != "@modelcontextprotocol/server-github" {
		t.Errorf("MCPServers[0].Args = %v", gh.Args)
	}
	if len(gh.EnvKeys) != 1 || gh.EnvKeys[0] != "GITHUB_TOKEN" {
		t.Errorf("MCPServers[0].EnvKeys = %v", gh.EnvKeys)
	}
}
