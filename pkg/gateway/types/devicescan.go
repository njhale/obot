//nolint:revive
package types

import (
	"database/sql/driver"
	"fmt"
	"time"

	types2 "github.com/obot-platform/obot/apiclient/types"
	"gorm.io/datatypes"
)

// AggTime is a time scanner that tolerates both time.Time and string
// values returned by drivers. Postgres returns aggregate timestamps as
// time.Time, but mattn/go-sqlite3 returns them as strings — so plain
// time.Time fields fail Scan() on aggregate result rows under SQLite.
type AggTime struct {
	time.Time
}

var sqliteTimeLayouts = []string{
	"2006-01-02 15:04:05.999999999-07:00",
	"2006-01-02 15:04:05.999-07:00",
	"2006-01-02 15:04:05-07:00",
	"2006-01-02 15:04:05.999999999",
	"2006-01-02 15:04:05.999",
	"2006-01-02 15:04:05",
	time.RFC3339Nano,
	time.RFC3339,
}

func (t *AggTime) Scan(value any) error {
	if value == nil {
		t.Time = time.Time{}
		return nil
	}
	switch v := value.(type) {
	case time.Time:
		t.Time = v
		return nil
	case []byte:
		return t.Scan(string(v))
	case string:
		for _, layout := range sqliteTimeLayouts {
			if parsed, err := time.Parse(layout, v); err == nil {
				t.Time = parsed
				return nil
			}
		}
		return fmt.Errorf("AggTime: unrecognized time format: %q", v)
	}
	return fmt.Errorf("AggTime: unsupported scan type %T", value)
}

func (t AggTime) Value() (driver.Value, error) {
	if t.IsZero() {
		return nil, nil
	}
	return t.Time, nil
}

// DeviceScan is the parent envelope. Children (MCPServers, Skills,
// Plugins, Files) are GORM associations — db.Create(&scan) inserts
// everything atomically; db.Preload(...).First(...) loads them back.
//
// Composite indexes:
//   - idx_ds_user_time   (submitted_by, created_at) — list scans for a user
//   - idx_ds_device_time (device_id, created_at)    — list scans for a device
type DeviceScan struct {
	ID             uint      `json:"id" gorm:"primaryKey"`
	CreatedAt      time.Time `json:"createdAt" gorm:"index:idx_ds_user_time,priority:2;index:idx_ds_device_time,priority:2"`
	SubmittedBy    string    `json:"submittedBy" gorm:"index:idx_ds_user_time,priority:1"`
	DeviceID       string    `json:"deviceID" gorm:"index:idx_ds_device_time,priority:1"`
	Hostname       string    `json:"hostname"`
	Username       string    `json:"username"`
	OS             string    `json:"os"`
	Arch           string    `json:"arch"`
	ScannerVersion string    `json:"scannerVersion"`
	ScannedAt      time.Time `json:"scannedAt" gorm:"index"`

	MCPServers []DeviceScanMCPServer `json:"mcpServers,omitempty" gorm:"foreignKey:DeviceScanID;constraint:OnDelete:CASCADE"`
	Skills     []DeviceScanSkill     `json:"skills,omitempty"     gorm:"foreignKey:DeviceScanID;constraint:OnDelete:CASCADE"`
	Plugins    []DeviceScanPlugin    `json:"plugins,omitempty"    gorm:"foreignKey:DeviceScanID;constraint:OnDelete:CASCADE"`
	Files      []DeviceScanFile      `json:"files,omitempty"      gorm:"foreignKey:DeviceScanID;constraint:OnDelete:CASCADE"`
}

// DeviceScanMCPServer is one MCP server observation. The parent
// DeviceScan supplies SubmittedBy / DeviceID via JOIN; per-asset queries
// scoped to a name / client / transport / config-hash hit the indexes
// on this table directly.
type DeviceScanMCPServer struct {
	ID           uint                        `json:"id" gorm:"primaryKey"`
	DeviceScanID uint                        `json:"deviceScanID" gorm:"index;not null"`
	CreatedAt    time.Time                   `json:"createdAt" gorm:"index"`
	Client       string                      `json:"client" gorm:"index"`
	Scope        string                      `json:"scope" gorm:"index"`
	ProjectPath  string                      `json:"projectPath" gorm:"index"`
	ConfigFile   string                      `json:"configFile"`
	PluginFile   string                      `json:"pluginFile"`
	Name         string                      `json:"name" gorm:"index"`
	Transport    string                      `json:"transport" gorm:"index"`
	Command      string                      `json:"command"`
	Args         datatypes.JSONSlice[string] `json:"args"`
	URL          string                      `json:"url"`
	EnvKeys      datatypes.JSONSlice[string] `json:"envKeys"`
	HeaderKeys   datatypes.JSONSlice[string] `json:"headerKeys"`
	ConfigHash   string                      `json:"configHash" gorm:"index"`
}

type DeviceScanSkill struct {
	ID           uint                        `json:"id" gorm:"primaryKey"`
	DeviceScanID uint                        `json:"deviceScanID" gorm:"index;not null"`
	CreatedAt    time.Time                   `json:"createdAt" gorm:"index"`
	Client       string                      `json:"client" gorm:"index"`
	Scope        string                      `json:"scope" gorm:"index"`
	PluginFile   string                      `json:"pluginFile"`
	Name         string                      `json:"name" gorm:"index"`
	Description  string                      `json:"description"`
	HasScripts   bool                        `json:"hasScripts"`
	GitRemoteURL string                      `json:"gitRemoteURL" gorm:"index"`
	Files        datatypes.JSONSlice[string] `json:"files"`
}

type DeviceScanPlugin struct {
	ID            uint                        `json:"id" gorm:"primaryKey"`
	DeviceScanID  uint                        `json:"deviceScanID" gorm:"index;not null"`
	CreatedAt     time.Time                   `json:"createdAt" gorm:"index"`
	Client        string                      `json:"client" gorm:"index"`
	Scope         string                      `json:"scope" gorm:"index"`
	Name          string                      `json:"name" gorm:"index"`
	PluginType    string                      `json:"pluginType" gorm:"index"`
	PluginFile    string                      `json:"pluginFile"`
	Version       string                      `json:"version"`
	Description   string                      `json:"description"`
	Author        string                      `json:"author"`
	Enabled       bool                        `json:"enabled"`
	Marketplace   string                      `json:"marketplace"`
	Files         datatypes.JSONSlice[string] `json:"files"`
	HasMCPServers bool                        `json:"hasMCPServers"`
	HasSkills     bool                        `json:"hasSkills"`
	HasRules      bool                        `json:"hasRules"`
	HasCommands   bool                        `json:"hasCommands"`
	HasHooks      bool                        `json:"hasHooks"`
}

type DeviceScanFile struct {
	ID           uint      `json:"id" gorm:"primaryKey"`
	DeviceScanID uint      `json:"deviceScanID" gorm:"index;not null"`
	CreatedAt    time.Time `json:"createdAt" gorm:"index"`
	Path         string    `json:"path" gorm:"index"`
	SizeBytes    int64     `json:"sizeBytes"`
	Oversized    bool      `json:"oversized"`
	Content      string    `json:"content" gorm:"type:text"`
}

// ConvertDeviceScan converts internal DeviceScan to API type. Children
// must already be loaded (via Preload) for them to appear in the result.
func ConvertDeviceScan(s DeviceScan) types2.DeviceScan {
	out := types2.DeviceScan{
		ID:             s.ID,
		ReceivedAt:     *types2.NewTime(s.CreatedAt),
		SubmittedBy:    s.SubmittedBy,
		ScannerVersion: s.ScannerVersion,
		ScannedAt:      *types2.NewTime(s.ScannedAt),
		DeviceID:       s.DeviceID,
		Hostname:       s.Hostname,
		OS:             s.OS,
		Arch:           s.Arch,
		Username:       s.Username,
	}
	if len(s.Files) > 0 {
		out.Files = make([]types2.DeviceScanFile, len(s.Files))
		for i, f := range s.Files {
			out.Files[i] = ConvertDeviceScanFile(f)
		}
	}
	if len(s.MCPServers) > 0 {
		out.MCPServers = make([]types2.DeviceScanMCPServer, len(s.MCPServers))
		for i, m := range s.MCPServers {
			out.MCPServers[i] = ConvertDeviceScanMCPServer(m)
		}
	}
	if len(s.Skills) > 0 {
		out.Skills = make([]types2.DeviceScanSkill, len(s.Skills))
		for i, sk := range s.Skills {
			out.Skills[i] = ConvertDeviceScanSkill(sk)
		}
	}
	if len(s.Plugins) > 0 {
		out.Plugins = make([]types2.DeviceScanPlugin, len(s.Plugins))
		for i, p := range s.Plugins {
			out.Plugins[i] = ConvertDeviceScanPlugin(p)
		}
	}
	return out
}

// ConvertDeviceScanFile converts a stored file row to its wire form.
// Content is included only when the file wasn't flagged as oversized.
func ConvertDeviceScanFile(f DeviceScanFile) types2.DeviceScanFile {
	out := types2.DeviceScanFile{
		Path:      f.Path,
		SizeBytes: f.SizeBytes,
		Oversized: f.Oversized,
	}
	if !f.Oversized {
		out.Content = f.Content
	}
	return out
}

func ConvertDeviceScanMCPServer(m DeviceScanMCPServer) types2.DeviceScanMCPServer {
	return types2.DeviceScanMCPServer{
		Client:      m.Client,
		Scope:       m.Scope,
		ProjectPath: m.ProjectPath,
		ConfigFile:  m.ConfigFile,
		PluginFile:  m.PluginFile,
		Name:        m.Name,
		Transport:   m.Transport,
		Command:     m.Command,
		Args:        []string(m.Args),
		URL:         m.URL,
		EnvKeys:     []string(m.EnvKeys),
		HeaderKeys:  []string(m.HeaderKeys),
		ConfigHash:  m.ConfigHash,
	}
}

func ConvertDeviceScanSkill(s DeviceScanSkill) types2.DeviceScanSkill {
	return types2.DeviceScanSkill{
		Client:       s.Client,
		Scope:        s.Scope,
		PluginFile:   s.PluginFile,
		Name:         s.Name,
		Description:  s.Description,
		Files:        []string(s.Files),
		HasScripts:   s.HasScripts,
		GitRemoteURL: s.GitRemoteURL,
	}
}

func ConvertDeviceScanPlugin(p DeviceScanPlugin) types2.DeviceScanPlugin {
	return types2.DeviceScanPlugin{
		Client:        p.Client,
		Scope:         p.Scope,
		Name:          p.Name,
		PluginType:    p.PluginType,
		PluginFile:    p.PluginFile,
		Version:       p.Version,
		Description:   p.Description,
		Author:        p.Author,
		Enabled:       p.Enabled,
		Marketplace:   p.Marketplace,
		Files:         []string(p.Files),
		HasMCPServers: p.HasMCPServers,
		HasSkills:     p.HasSkills,
		HasRules:      p.HasRules,
		HasCommands:   p.HasCommands,
		HasHooks:      p.HasHooks,
	}
}

// AggregatedMCPServer is one row of the device-fleet MCP aggregation:
// every DeviceScanMCPServer with the same ConfigHash, observed in any
// device's latest scan within the requested time window, collapses into
// a single entity. Identity fields (Name, Transport, Command, URL) are
// constant within a ConfigHash group by construction — they're inputs
// to the hash itself. Args is excluded from the list aggregate because
// MAX() doesn't work on JSONB in Postgres; it's fetched via a single
// canonical row read on the detail endpoint.
type AggregatedMCPServer struct {
	ConfigHash       string  `gorm:"column:config_hash"`
	Name             string  `gorm:"column:name"`
	Transport        string  `gorm:"column:transport"`
	Command          string  `gorm:"column:command"`
	URL              string  `gorm:"column:url"`
	DeviceCount      int64   `gorm:"column:device_count"`
	UserCount        int64   `gorm:"column:user_count"`
	ClientCount      int64   `gorm:"column:client_count"`
	ScopeCount       int64   `gorm:"column:scope_count"`
	ObservationCount int64   `gorm:"column:observation_count"`
	FirstSeen        AggTime `gorm:"column:first_seen"`
	LastSeen         AggTime `gorm:"column:last_seen"`
}

// AggregatedMCPServerDetail is the detail-page payload: an aggregated
// row plus Args (read from a canonical row) and the union of EnvKeys /
// HeaderKeys observed across every occurrence (those are deliberately
// excluded from the hash).
type AggregatedMCPServerDetail struct {
	AggregatedMCPServer
	Args       []string
	EnvKeys    []string
	HeaderKeys []string
}

// MCPServerOccurrence is one device's latest-scan instance of a given
// ConfigHash. Index is the position of the row inside the parent scan's
// MCPServers slice (GORM preload order, by id ASC) so the UI can deep-
// link to /admin/device-scans/{deviceScanID}/mcp/{index}.
type MCPServerOccurrence struct {
	DeviceScanID uint    `gorm:"column:device_scan_id"`
	DeviceID     string  `gorm:"column:device_id"`
	Client       string  `gorm:"column:client"`
	Scope        string  `gorm:"column:scope"`
	ScannedAt    AggTime `gorm:"column:scanned_at"`
	Index        int     `gorm:"column:idx"`
}

// DeviceScanFromWire builds a gateway DeviceScan + its children from a
// wire-format payload. Caller is responsible for setting SubmittedBy on
// the returned struct before passing it to InsertDeviceScan.
func DeviceScanFromWire(p types2.DeviceScan) DeviceScan {
	s := DeviceScan{
		DeviceID:       p.DeviceID,
		Hostname:       p.Hostname,
		Username:       p.Username,
		OS:             p.OS,
		Arch:           p.Arch,
		ScannerVersion: p.ScannerVersion,
		ScannedAt:      p.ScannedAt.GetTime(),
	}
	if len(p.Files) > 0 {
		s.Files = make([]DeviceScanFile, len(p.Files))
		for i, f := range p.Files {
			s.Files[i] = DeviceScanFile{
				Path:      f.Path,
				SizeBytes: f.SizeBytes,
				Oversized: f.Oversized,
				Content:   f.Content,
			}
		}
	}
	if len(p.MCPServers) > 0 {
		s.MCPServers = make([]DeviceScanMCPServer, len(p.MCPServers))
		for i, m := range p.MCPServers {
			s.MCPServers[i] = DeviceScanMCPServer{
				Client:      m.Client,
				Scope:       m.Scope,
				ProjectPath: m.ProjectPath,
				ConfigFile:  m.ConfigFile,
				PluginFile:  m.PluginFile,
				Name:        m.Name,
				Transport:   m.Transport,
				Command:     m.Command,
				Args:        datatypes.JSONSlice[string](m.Args),
				URL:         m.URL,
				EnvKeys:     datatypes.JSONSlice[string](m.EnvKeys),
				HeaderKeys:  datatypes.JSONSlice[string](m.HeaderKeys),
				ConfigHash:  m.ConfigHash,
			}
		}
	}
	if len(p.Skills) > 0 {
		s.Skills = make([]DeviceScanSkill, len(p.Skills))
		for i, sk := range p.Skills {
			s.Skills[i] = DeviceScanSkill{
				Client:       sk.Client,
				Scope:        sk.Scope,
				PluginFile:   sk.PluginFile,
				Name:         sk.Name,
				Description:  sk.Description,
				HasScripts:   sk.HasScripts,
				GitRemoteURL: sk.GitRemoteURL,
				Files:        datatypes.JSONSlice[string](sk.Files),
			}
		}
	}
	if len(p.Plugins) > 0 {
		s.Plugins = make([]DeviceScanPlugin, len(p.Plugins))
		for i, pl := range p.Plugins {
			s.Plugins[i] = DeviceScanPlugin{
				Client:        pl.Client,
				Scope:         pl.Scope,
				Name:          pl.Name,
				PluginType:    pl.PluginType,
				PluginFile:    pl.PluginFile,
				Version:       pl.Version,
				Description:   pl.Description,
				Author:        pl.Author,
				Enabled:       pl.Enabled,
				Marketplace:   pl.Marketplace,
				HasMCPServers: pl.HasMCPServers,
				HasSkills:     pl.HasSkills,
				HasRules:      pl.HasRules,
				HasCommands:   pl.HasCommands,
				HasHooks:      pl.HasHooks,
				Files:         datatypes.JSONSlice[string](pl.Files),
			}
		}
	}
	return s
}
