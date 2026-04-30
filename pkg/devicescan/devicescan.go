// Package devicescan inventories local AI client configuration on a device.
//
// Scan reads known config locations under a home directory (provided as an
// fs.FS), parses MCP server, skill, and plugin observations, and returns a
// types.DeviceScan suitable for submission to the Obot backend.
//
// Organisation is by concern rather than by client. Skills (skills.go) own
// SKILL.md handling regardless of which client a file belongs to, consulting
// a path-prefix table for tool attribution. MCP server parsing (mcp.go) is
// shared with per-client quirk hooks. Plugin manifest helpers (plugins.go)
// are shared and called from per-client scanners that know each client's
// cache layout (claudecode.go, codex.go).
package devicescan

import (
	"context"
	"io/fs"
	"path/filepath"
	"sort"

	"github.com/obot-platform/obot/apiclient/types"
)

// Scan runs the full collection pipeline against fsys (rooted at homeAbs)
// and returns the assembled DeviceScan. Per-phase errors are logged and
// skipped so a missing or malformed config never aborts the rest of the
// scan. Context cancellation propagates.
//
// Server-assigned envelope fields (ScannerVersion, ScannedAt, DeviceID,
// Hostname, OS, Arch, Username, ID, ReceivedAt, SubmittedBy) are left zero;
// the caller fills them in.
//
// maxDepth caps how deep the shared $HOME marker crawl will descend from
// the home root when looking for project-scope configs and SKILL.md files.
func Scan(ctx context.Context, fsys fs.FS, homeAbs string, maxDepth int) (types.DeviceScan, error) {
	r := newResult(fsys, homeAbs)

	// Phase 1 — per-client global MCP configs.
	for _, c := range clientDefs {
		if err := ctx.Err(); err != nil {
			return types.DeviceScan{}, err
		}
		parseGlobalMCPConfig(r, c)
	}

	// Phase 2 — shared $HOME marker crawl (one walk, many concerns).
	if err := ctx.Err(); err != nil {
		return types.DeviceScan{}, err
	}
	markers := walkMarkers(r.fsys, allMarkerRules(), maxDepth)

	// Phase 3 — project-level MCP configs (consume markers).
	if err := ctx.Err(); err != nil {
		return types.DeviceScan{}, err
	}
	parseProjectMCPConfigs(r, markers)

	// Phase 4 — per-client plugin scans.
	for _, fn := range []func(*Result){
		scanClaudeCodePlugins,
		scanCodexPlugins,
		scanCursorPlugins,
		scanClaudeDesktopConnectors,
		scanOpenCodePlugins,
	} {
		if err := ctx.Err(); err != nil {
			return types.DeviceScan{}, err
		}
		fn(r)
	}

	// Phase 5 — skills (global dirs first, then project hits).
	if err := ctx.Err(); err != nil {
		return types.DeviceScan{}, err
	}
	scanGlobalSkills(r)
	if err := ctx.Err(); err != nil {
		return types.DeviceScan{}, err
	}
	scanProjectSkills(r, markers)

	// Phase 6 — build.
	return r.Build(), nil
}

// Result is the accumulator threaded through phase functions. It owns the
// fs.FS, dedupes ingested files by absolute path, and collects observation
// records.
type Result struct {
	fsys    fs.FS
	homeAbs string

	files   map[string]types.DeviceScanFile
	mcps    []types.DeviceScanMCPServer
	skills  []types.DeviceScanSkill
	plugins []types.DeviceScanPlugin

	// globalConfigRels holds fs-relative paths that were already opened as
	// a client's global MCP config. parseProjectMCPConfigs uses this set to
	// skip marker hits that resolve to a known global file (e.g. Cursor's
	// ~/.cursor/mcp.json appears in both the global path list and the
	// project marker walk). Mirrors runlayer project_scanner.py:162.
	globalConfigRels map[string]bool
}

func newResult(fsys fs.FS, homeAbs string) *Result {
	return &Result{
		fsys:             fsys,
		homeAbs:          homeAbs,
		files:            map[string]types.DeviceScanFile{},
		globalConfigRels: map[string]bool{},
	}
}

// markGlobalConfig records rel as a known global config path so that any
// project-marker walk hit at the same path is suppressed.
func (r *Result) markGlobalConfig(rel string) { r.globalConfigRels[rel] = true }

// isGlobalConfig reports whether rel was previously registered via
// markGlobalConfig.
func (r *Result) isGlobalConfig(rel string) bool { return r.globalConfigRels[rel] }

// AddMCPServer records an MCP server observation.
func (r *Result) AddMCPServer(s types.DeviceScanMCPServer) { r.mcps = append(r.mcps, s) }

// AddSkill records a skill observation.
func (r *Result) AddSkill(s types.DeviceScanSkill) { r.skills = append(r.skills, s) }

// AddPlugin records a plugin observation.
func (r *Result) AddPlugin(p types.DeviceScanPlugin) { r.plugins = append(r.plugins, p) }

// abs converts an fs.FS-relative path to the absolute path that should
// appear in wire output.
func (r *Result) abs(rel string) string {
	return filepath.Join(r.homeAbs, filepath.FromSlash(rel))
}

// Build flattens the accumulator into a wire-shape DeviceScan. Files are
// path-sorted so output is deterministic. Empty slices are kept non-nil so
// JSON serialisation produces `[]` rather than `null`.
func (r *Result) Build() types.DeviceScan {
	out := types.DeviceScan{
		Files:      make([]types.DeviceScanFile, 0, len(r.files)),
		MCPServers: r.mcps,
		Skills:     r.skills,
		Plugins:    r.plugins,
	}
	if out.MCPServers == nil {
		out.MCPServers = []types.DeviceScanMCPServer{}
	}
	if out.Skills == nil {
		out.Skills = []types.DeviceScanSkill{}
	}
	if out.Plugins == nil {
		out.Plugins = []types.DeviceScanPlugin{}
	}
	paths := make([]string, 0, len(r.files))
	for p := range r.files {
		paths = append(paths, p)
	}
	sort.Strings(paths)
	for _, p := range paths {
		out.Files = append(out.Files, r.files[p])
	}
	return out
}
