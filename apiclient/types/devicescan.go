package types

// DeviceScan is the wire shape submitted by `obot scan` and stored by the
// backend. Server-assigned fields (ID, ReceivedAt, SubmittedBy) are zero
// in CLI-built instances and populated by the backend on receipt.
type DeviceScan struct {
	ID          uint   `json:"id,omitempty"`
	ReceivedAt  Time   `json:"received_at"`
	SubmittedBy string `json:"submitted_by,omitempty"`

	ScannerVersion string                `json:"scanner_version"`
	ScannedAt      Time                  `json:"scanned_at"`
	DeviceID       string                `json:"device_id"`
	Hostname       string                `json:"hostname"`
	OS             string                `json:"os"`
	Arch           string                `json:"arch"`
	Username       string                `json:"username,omitempty"`
	Files          []DeviceScanFile      `json:"files"`
	MCPServers     []DeviceScanMCPServer `json:"mcp_servers"`
	Skills         []DeviceScanSkill     `json:"skills"`
	Plugins        []DeviceScanPlugin    `json:"plugins"`
}

// DeviceScanList is the response shape for GET /api/device-scans.
type DeviceScanList struct {
	Items  []DeviceScan `json:"items"`
	Total  int64        `json:"total"`
	Limit  int          `json:"limit"`
	Offset int          `json:"offset"`
}

type DeviceScanFile struct {
	Path      string `json:"path"`
	SizeBytes int64  `json:"size_bytes"`
	Oversized bool   `json:"oversized"`
	Content   string `json:"content,omitempty"`
}

type DeviceScanMCPServer struct {
	Client      string   `json:"client"`
	Scope       string   `json:"scope"`
	ProjectPath string   `json:"project_path,omitempty"`
	ConfigFile  string   `json:"config_file,omitempty"`
	PluginFile  string   `json:"plugin_file,omitempty"`
	ConfigHash  string   `json:"config_hash,omitempty"`
	EnvKeys     []string `json:"env_keys"`
	HeaderKeys  []string `json:"header_keys"`
	Name        string   `json:"name"`
	Transport   string   `json:"transport"`
	Command     string   `json:"command,omitempty"`
	Args        []string `json:"args,omitempty"`
	URL         string   `json:"url,omitempty"`
}

type DeviceScanSkill struct {
	Client       string   `json:"client"`
	Scope        string   `json:"scope"`
	PluginFile   string   `json:"plugin_file,omitempty"`
	Name         string   `json:"name"`
	Description  string   `json:"description,omitempty"`
	Files        []string `json:"files"`
	HasScripts   bool     `json:"has_scripts"`
	GitRemoteURL string   `json:"git_remote_url,omitempty"`
}

type DeviceScanPlugin struct {
	Client        string   `json:"client"`
	Scope         string   `json:"scope"`
	Name          string   `json:"name"`
	PluginType    string   `json:"plugin_type"`
	PluginFile    string   `json:"plugin_file,omitempty"`
	Version       string   `json:"version,omitempty"`
	Description   string   `json:"description,omitempty"`
	Author        string   `json:"author,omitempty"`
	Marketplace   string   `json:"marketplace,omitempty"`
	Files         []string `json:"files"`
	Enabled       bool     `json:"enabled"`
	HasMCPServers bool     `json:"has_mcp_servers"`
	HasSkills     bool     `json:"has_skills"`
	HasRules      bool     `json:"has_rules"`
	HasCommands   bool     `json:"has_commands"`
	HasHooks      bool     `json:"has_hooks"`
}

// AggregatedDeviceMCPServer is one row of the fleet-wide MCP aggregation:
// every DeviceScanMCPServer sharing the same ConfigHash collapses into a
// single entity. Identity fields (Name, Transport, Command, URL) are
// stable within a ConfigHash by construction. Args is omitted from the
// list response and surfaced on the detail variant only.
type AggregatedDeviceMCPServer struct {
	ConfigHash       string `json:"config_hash"`
	Name             string `json:"name"`
	Transport        string `json:"transport"`
	Command          string `json:"command,omitempty"`
	URL              string `json:"url,omitempty"`
	DeviceCount      int64  `json:"device_count"`
	UserCount        int64  `json:"user_count"`
	ClientCount      int64  `json:"client_count"`
	ScopeCount       int64  `json:"scope_count"`
	ObservationCount int64  `json:"observation_count"`
	FirstSeen        Time   `json:"first_seen"`
	LastSeen         Time   `json:"last_seen"`
}

// AggregatedDeviceMCPServerList is the response shape for
// GET /api/devices/mcp-servers.
type AggregatedDeviceMCPServerList struct {
	Items  []AggregatedDeviceMCPServer `json:"items"`
	Total  int64                       `json:"total"`
	Limit  int                         `json:"limit"`
	Offset int                         `json:"offset"`
}

// AggregatedDeviceMCPServerDetail is the response shape for
// GET /api/devices/mcp-servers/{config_hash}. EnvKeys and HeaderKeys are
// the union across all observations of this hash (the hash deliberately
// excludes them).
type AggregatedDeviceMCPServerDetail struct {
	AggregatedDeviceMCPServer
	Args       []string `json:"args,omitempty"`
	EnvKeys    []string `json:"env_keys"`
	HeaderKeys []string `json:"header_keys"`
}

// DeviceMCPServerOccurrence is one device's latest-scan instance of a
// specific ConfigHash. Index is the position of the row inside its
// parent scan's MCPServers slice, suitable for deep-linking.
type DeviceMCPServerOccurrence struct {
	DeviceScanID uint   `json:"device_scan_id"`
	DeviceID     string `json:"device_id"`
	Client       string `json:"client"`
	Scope        string `json:"scope"`
	ScannedAt    Time   `json:"scanned_at"`
	Index        int    `json:"index"`
}

// DeviceMCPServerOccurrenceList is the response shape for
// GET /api/devices/mcp-servers/{config_hash}/occurrences.
type DeviceMCPServerOccurrenceList struct {
	Items  []DeviceMCPServerOccurrence `json:"items"`
	Total  int64                       `json:"total"`
	Limit  int                         `json:"limit"`
	Offset int                         `json:"offset"`
}
