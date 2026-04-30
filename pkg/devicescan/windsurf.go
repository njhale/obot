package devicescan

import "path"

// Windsurf on-disk layout. Paths are home-relative.
const (
	windsurfGlobalConfigRel     = ".codeium/windsurf/mcp_config.json"
	windsurfProjectMarkerName   = "mcp_config.json"
	windsurfProjectMarkerParent = ".windsurf"
)

var windsurfDef = clientDef{
	name: "windsurf",
	projectMarkers: []markerRule{
		{basename: windsurfProjectMarkerName, parent: windsurfProjectMarkerParent},
	},
	scanGlobal:  scanWindsurfGlobal,
	scanProject: scanWindsurfProject,
}

func scanWindsurfGlobal(r *Result) {
	emitJSONServersGlobal(r, windsurfGlobalConfigRel, "mcpServers", "windsurf")
}

func scanWindsurfProject(r *Result, markerRel string) {
	emitJSONServersProject(r, markerRel, "mcpServers", "windsurf", r.abs(path.Dir(path.Dir(markerRel))))
}
