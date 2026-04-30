package devicescan

import "path"

// VS Code on-disk layout. The macOS path is relative to $HOME; on other
// platforms the file simply will not exist and Phase 1 is a no-op.
const (
	vscodeGlobalConfigRel     = "Library/Application Support/Code/User/mcp.json"
	vscodeProjectMarkerName   = "mcp.json"
	vscodeProjectMarkerParent = ".vscode"
)

var vscodeDef = clientDef{
	name: "vscode",
	projectMarkers: []markerRule{
		{basename: vscodeProjectMarkerName, parent: vscodeProjectMarkerParent},
	},
	scanGlobal:  scanVSCodeGlobal,
	scanProject: scanVSCodeProject,
}

// VS Code uses "servers" rather than "mcpServers" for both global and
// project configs; entries follow the standard JSON shape so jsonServerEntry
// handles the wire conversion.
func scanVSCodeGlobal(r *Result) {
	emitJSONServersGlobal(r, vscodeGlobalConfigRel, "servers", "vscode")
}

func scanVSCodeProject(r *Result, markerRel string) {
	emitJSONServersProject(r, markerRel, "servers", "vscode", r.abs(path.Dir(path.Dir(markerRel))))
}
