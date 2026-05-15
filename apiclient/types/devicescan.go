package types

// DeviceScanManifest is what `obot scan` submits. Server-assigned
// fields (id, receivedAt, submittedBy) live on DeviceScan instead.
// Child observations share the same wire type for submission and
// response — the ID field is server-set and decoded into a zero value
// on submission, which DeviceScanFromManifest deliberately does not
// copy. Submitters cannot trample existing row PKs.
type DeviceScanManifest struct {
	// ScannerVersion is the obot version that produced the scan.
	ScannerVersion string `json:"scannerVersion"`
	// ScannedAt is when the scanner finished collecting on the device.
	ScannedAt Time `json:"scannedAt"`
	// DeviceID is the persisted per-device identifier so re-scans collate.
	DeviceID string `json:"deviceID"`
	// Hostname is the device hostname at scan time.
	Hostname string `json:"hostname"`
	// OS is the operating system (darwin, linux, windows).
	OS string `json:"os"`
	// Arch is the CPU architecture (amd64, arm64).
	Arch string `json:"arch"`
	// Username is the OS user that ran the scan.
	Username string `json:"username,omitempty"`
	// Files are the config / manifest files captured during the scan,
	// deduped by absolute path.
	Files []DeviceScanFile `json:"files"`
	// MCPServers are the MCP server observations.
	MCPServers []DeviceScanMCPServer `json:"mcpServers"`
	// Skills are the skill observations (SKILL.md hits).
	Skills []DeviceScanSkill `json:"skills"`
	// Plugins are the plugin observations.
	Plugins []DeviceScanPlugin `json:"plugins"`
	// Clients are the per-client presence + roll-up rows.
	Clients []DeviceScanClient `json:"clients"`
	// TopPrompts is populated when --include-top-prompts is set on `obot scan`.
	// Capped at 10 entries by server validation; sorted by metrics.totalTokens
	// descending on submission.
	TopPrompts []DeviceScanPrompt `json:"topPrompts,omitempty"`
}

// DeviceScan is a persisted scan: the submitted manifest plus
// server-assigned fields.
type DeviceScan struct {
	DeviceScanManifest `json:",inline"`
	// ID is the server-assigned primary key.
	ID uint `json:"id"`
	// ReceivedAt is the server's receipt timestamp.
	ReceivedAt Time `json:"receivedAt"`
	// SubmittedBy is the user ID of the caller that posted the scan.
	SubmittedBy string `json:"submittedBy"`
}

type DeviceScanList List[DeviceScan]

// DeviceScanResponse is returned by GET /api/devices/scans.
type DeviceScanResponse struct {
	DeviceScanList `json:",inline"`
	Total          int64 `json:"total"`
	Limit          int   `json:"limit"`
	Offset         int   `json:"offset"`
}

// DeviceScanFile is one captured config or manifest file.
type DeviceScanFile struct {
	// Path is the absolute path on the device.
	Path string `json:"path"`
	// SizeBytes is the file size in bytes.
	SizeBytes int64 `json:"sizeBytes"`
	// Oversized is true when the file exceeded the per-file content cap.
	Oversized bool `json:"oversized"`
	// Content is the raw file bytes; omitted when Oversized.
	Content string `json:"content,omitempty"`
}

// DeviceScanMCPServer is one MCP server observation. ID is
// server-assigned on insert and stable across responses.
type DeviceScanMCPServer struct {
	// ID is the row's primary key. Server-set; ignored on submission.
	ID uint `json:"id,omitempty"`
	// Client is the canonical client name (e.g. "cursor"); empty for orphans.
	Client string `json:"client"`
	// ProjectPath is the project root for project-scope observations; empty for global.
	ProjectPath string `json:"projectPath,omitempty"`
	// File is the absolute path of the defining config file.
	File string `json:"file,omitempty"`
	// ConfigHash is the content-addressed identity for fleet-wide aggregation.
	// Computed over Name, Transport, Command, Args, URL only — env / header
	// keys are excluded so a server with a rotated secret stays one entity.
	ConfigHash string `json:"configHash,omitempty"`
	// EnvKeys are the env var names referenced by the server config (values redacted).
	EnvKeys []string `json:"envKeys"`
	// HeaderKeys are the HTTP header names referenced by the server config (values redacted).
	HeaderKeys []string `json:"headerKeys"`
	// Name is the server's configured name.
	Name string `json:"name"`
	// Transport is "stdio", "http", "sse", etc.
	Transport string `json:"transport"`
	// Command is the stdio command, if any.
	Command string `json:"command,omitempty"`
	// Args are the stdio command arguments.
	Args []string `json:"args,omitempty"`
	// URL is the remote endpoint for HTTP / SSE transports.
	URL string `json:"url,omitempty"`
}

// DeviceScanSkill is one skill (SKILL.md) observation. ID is
// server-assigned on insert and stable across responses.
type DeviceScanSkill struct {
	// ID is the row's primary key. Server-set; ignored on submission.
	ID uint `json:"id,omitempty"`
	// Client is the canonical client name; "multi" for free-floating
	// SKILL.md files with no canonical owning client (e.g.
	// .agents/skills, .agent/skills, project skills outside a known
	// client tree).
	Client string `json:"client"`
	// ProjectPath is the project root for project-scope skills.
	ProjectPath string `json:"projectPath,omitempty"`
	// File is the absolute path of the SKILL.md file.
	File string `json:"file,omitempty"`
	// Name is the skill name (typically the SKILL.md frontmatter name).
	Name string `json:"name"`
	// Description is the skill description from frontmatter.
	Description string `json:"description,omitempty"`
	// Files lists SKILL.md plus any supporting artifacts in the skill directory.
	Files []string `json:"files"`
	// HasScripts indicates the skill ships at least one executable script.
	HasScripts bool `json:"hasScripts"`
	// GitRemoteURL is the git remote of the skill's enclosing repo, if any.
	GitRemoteURL string `json:"gitRemoteURL,omitempty"`
}

// DeviceScanClient is a per-device record for an AI client. Carries
// presence facts plus roll-ups summarising what the device has
// configured for this client.
type DeviceScanClient struct {
	// Name is the canonical client name (e.g. "claudecode", "cursor").
	Name string `json:"name"`
	// Version is the client version, when one was discoverable.
	Version string `json:"version,omitempty"`
	// BinaryPath is the resolved $PATH location of the client binary.
	BinaryPath string `json:"binaryPath,omitempty"`
	// InstallPath is the install location (e.g. an /Applications bundle).
	InstallPath string `json:"installPath,omitempty"`
	// ConfigPath is the client's primary config directory under $HOME.
	ConfigPath string `json:"configPath,omitempty"`
	// HasMCPServers is true when at least one MCPServers row references this client.
	HasMCPServers bool `json:"hasMCPServers"`
	// HasSkills is true when at least one Skills row references this client.
	HasSkills bool `json:"hasSkills"`
	// HasPlugins is true when at least one Plugins row references this client.
	HasPlugins bool `json:"hasPlugins"`
}

// DeviceScanPlugin is one plugin observation. ID is server-assigned
// on insert and stable across responses.
type DeviceScanPlugin struct {
	// ID is the row's primary key. Server-set; ignored on submission.
	ID uint `json:"id,omitempty"`
	// Client is the canonical client name that owns the plugin host.
	Client string `json:"client"`
	// ProjectPath is the project root for project-scope plugins.
	ProjectPath string `json:"projectPath,omitempty"`
	// ConfigPath is the absolute path of the plugin's defining manifest.
	ConfigPath string `json:"configPath,omitempty"`
	// Name is the plugin name.
	Name string `json:"name"`
	// PluginType identifies the plugin kind (extension, marketplace package, etc.).
	PluginType string `json:"pluginType"`
	// Version is the plugin version.
	Version string `json:"version,omitempty"`
	// Description is the plugin description from its manifest.
	Description string `json:"description,omitempty"`
	// Author is the plugin author from its manifest.
	Author string `json:"author,omitempty"`
	// Marketplace is the source marketplace, if applicable.
	Marketplace string `json:"marketplace,omitempty"`
	// Files lists every file collected from the plugin directory.
	Files []string `json:"files"`
	// Enabled is true when the plugin is enabled per the host's config.
	Enabled bool `json:"enabled"`
	// HasMCPServers is true when the plugin defines MCP servers.
	HasMCPServers bool `json:"hasMCPServers"`
	// HasSkills is true when the plugin defines skills.
	HasSkills bool `json:"hasSkills"`
	// HasRules is true when the plugin defines rules.
	HasRules bool `json:"hasRules"`
	// HasCommands is true when the plugin defines commands.
	HasCommands bool `json:"hasCommands"`
	// HasHooks is true when the plugin defines hooks.
	HasHooks bool `json:"hasHooks"`
}

// DeviceMCPServerStat is one row of the fleet-wide MCP aggregation,
// keyed by ConfigHash. Identity fields (Name, Transport, Command,
// Args, URL) are stable within a hash group by construction — they
// are inputs to the hash itself.
type DeviceMCPServerStat struct {
	// ConfigHash is the aggregation key.
	ConfigHash string   `json:"configHash"`
	Name       string   `json:"name"`
	Transport  string   `json:"transport"`
	Command    string   `json:"command,omitempty"`
	Args       []string `json:"args,omitempty"`
	URL        string   `json:"url,omitempty"`
	// DeviceCount is the number of distinct devices observing this hash.
	DeviceCount int64 `json:"deviceCount"`
	// UserCount is the number of distinct submitters observing this hash.
	UserCount int64 `json:"userCount"`
	// ClientCount is the number of distinct client names observing this hash.
	ClientCount int64 `json:"clientCount"`
	// ObservationCount is the total number of rows with this hash.
	ObservationCount int64 `json:"observationCount"`
}

// DeviceMCPServerDetail is the GET /api/devices/mcp-servers/{config_hash}
// response. EnvKeys and HeaderKeys are not in the hash and may vary
// per observation; they are unioned across all observations.
type DeviceMCPServerDetail struct {
	DeviceMCPServerStat
	// EnvKeys is the set union of env var names referenced across
	// observations of this hash.
	EnvKeys []string `json:"envKeys"`
	// HeaderKeys is the set union of HTTP header names referenced
	// across observations of this hash.
	HeaderKeys []string `json:"headerKeys"`
}

// DeviceClientStat is one row of the per-client rollup.
type DeviceClientStat struct {
	// Name is the canonical client name.
	Name string `json:"name"`
	// DeviceCount is the number of distinct devices with this client.
	DeviceCount int64 `json:"deviceCount"`
	// UserCount is the number of distinct submitters with this client.
	UserCount int64 `json:"userCount"`
	// ObservationCount is the total number of client rows for this name.
	ObservationCount int64 `json:"observationCount"`
}

// DeviceSkillStat is one row of the per-skill rollup.
type DeviceSkillStat struct {
	// Name is the skill name (the aggregation key).
	Name string `json:"name"`
	// DeviceCount is the number of distinct devices with this skill.
	DeviceCount int64 `json:"deviceCount"`
	// UserCount is the number of distinct submitters with this skill.
	UserCount int64 `json:"userCount"`
	// ObservationCount is the total number of skill rows for this name.
	ObservationCount int64 `json:"observationCount"`
}

type DeviceSkillStatList List[DeviceSkillStat]

// DeviceSkillStatResponse is returned by GET /api/devices/skills.
type DeviceSkillStatResponse struct {
	DeviceSkillStatList `json:",inline"`
	Total               int64 `json:"total"`
	Limit               int   `json:"limit"`
	Offset              int   `json:"offset"`
}

// DeviceSkillDetail is the GET /api/devices/skills/{name} response.
// The metadata fields come from one canonical observation and are not
// guaranteed to be stable across observations sharing the same name.
type DeviceSkillDetail struct {
	DeviceSkillStat
	// Description is the skill's short summary.
	Description string `json:"description,omitempty"`
	// HasScripts is true when the skill ships executable scripts.
	HasScripts bool `json:"hasScripts"`
	// GitRemoteURL is the upstream repo, if any.
	GitRemoteURL string `json:"gitRemoteURL,omitempty"`
	// Files lists every file collected from the skill directory.
	Files []string `json:"files,omitempty"`
}

// DeviceScanStats is returned by GET /api/devices/scan-stats.
type DeviceScanStats struct {
	// TimeStart is the inclusive lower bound of the rollup window.
	TimeStart Time `json:"timeStart"`
	// TimeEnd is the exclusive upper bound of the rollup window.
	TimeEnd Time `json:"timeEnd"`
	// DeviceCount is the number of distinct devices in the window.
	DeviceCount int64 `json:"deviceCount"`
	// UserCount is the number of distinct submitters in the window.
	UserCount int64 `json:"userCount"`
	// Clients is the full ranked per-client breakdown.
	Clients []DeviceClientStat `json:"clients"`
	// MCPServers is the full ranked per-ConfigHash breakdown.
	MCPServers []DeviceMCPServerStat `json:"mcpServers"`
	// Skills is the full ranked per-skill breakdown.
	Skills []DeviceSkillStat `json:"skills"`
	// ScanTimestamps is every scan submission's scanned_at inside the
	// window, sorted ascending. The dashboard chart buckets these
	// client-side in the user's local timezone. Counts every submission,
	// not just the latest-per-device subset that drives the other
	// rollups.
	ScanTimestamps []Time `json:"scanTimestamps"`
}

// DeviceMCPServerOccurrence is one device's latest-scan row for a
// specific ConfigHash.
type DeviceMCPServerOccurrence struct {
	// DeviceScanID is the parent scan's primary key.
	DeviceScanID uint `json:"deviceScanID"`
	// DeviceID is the device that submitted the parent scan.
	DeviceID string `json:"deviceID"`
	// Client is the canonical client name on this row.
	Client string `json:"client"`
	// Scope is "global" or "project".
	Scope string `json:"scope"`
	// ScannedAt is when the parent scan was collected on the device.
	ScannedAt Time `json:"scannedAt"`
	// ID is the observation's stable identifier.
	ID uint `json:"id"`
}

type DeviceMCPServerOccurrenceList List[DeviceMCPServerOccurrence]

// DeviceMCPServerOccurrenceResponse is returned by
// GET /api/devices/mcp-servers/{config_hash}/occurrences.
type DeviceMCPServerOccurrenceResponse struct {
	DeviceMCPServerOccurrenceList `json:",inline"`
	Total                         int64 `json:"total"`
	Limit                         int   `json:"limit"`
	Offset                        int   `json:"offset"`
}

// DeviceSkillOccurrence is one device's latest-scan row for a specific
// skill name.
type DeviceSkillOccurrence struct {
	// DeviceScanID is the parent scan's primary key.
	DeviceScanID uint `json:"deviceScanID"`
	// DeviceID is the device that submitted the parent scan.
	DeviceID string `json:"deviceID"`
	// Client is the canonical client name on this row.
	Client string `json:"client"`
	// Scope is "global" or "project".
	Scope string `json:"scope"`
	// ProjectPath is the project root for project-scope rows.
	ProjectPath string `json:"projectPath,omitempty"`
	// ScannedAt is when the parent scan was collected on the device.
	ScannedAt Time `json:"scannedAt"`
	// ID is the observation's stable identifier.
	ID uint `json:"id"`
}

type DeviceSkillOccurrenceList List[DeviceSkillOccurrence]

// DeviceSkillOccurrenceResponse is returned by
// GET /api/devices/skills/{name}/occurrences.
type DeviceSkillOccurrenceResponse struct {
	DeviceSkillOccurrenceList `json:",inline"`
	Total                     int64 `json:"total"`
	Limit                     int   `json:"limit"`
	Offset                    int   `json:"offset"`
}

// DeviceClientFleetSkill is one skill row on a device client fleet summary
// (client match, not "multi"; canonical row is earliest observation id per
// client + skill name).
type DeviceClientFleetSkill struct {
	// Name is the skill name (typically from SKILL.md frontmatter).
	Name string `json:"name"`
	// Description is the short summary from frontmatter when present.
	Description string `json:"description,omitempty"`
	// HasScripts is true when the skill directory includes executable scripts.
	HasScripts bool `json:"hasScripts"`
	// Files is the number of file paths recorded for that skill observation.
	Files int `json:"files"`
}

// DeviceClientFleetSummary rolls up latest-scan-per-device data for one
// canonical client name (from device_scan_clients).
type DeviceClientFleetSummary struct {
	// Name is the canonical client identifier (e.g. "cursor", "claude-code").
	Name string `json:"name"`
	// Users are distinct scan submitters whose latest scan lists this client.
	Users []string `json:"users"`
	// Skills lists one entry per distinct skill name with metadata on each
	// device's latest scan (client match; excludes "multi").
	Skills []DeviceClientFleetSkill `json:"skills"`
	// MCPServers are distinct MCP servers (by ConfigHash) observed with
	// Client == Name in those latest scans; rows with client "multi" are excluded.
	MCPServers []DeviceMCPServerStat `json:"mcpServers"`
}

type DeviceClientFleetSummaryList List[DeviceClientFleetSummary]

// DeviceClientFleetSummaryResponse is returned by GET /api/devices/clients.
type DeviceClientFleetSummaryResponse struct {
	DeviceClientFleetSummaryList `json:",inline"`
	Total                        int64 `json:"total"`
	Limit                        int    `json:"limit"`
	Offset                       int    `json:"offset"`
}

// DeviceScanPrompt is one captured top-level user prompt with rolled-up
// token usage and tool/subagent activity. Attached to a DeviceScan.
type DeviceScanPrompt struct {
	// ID is the row's primary key. Server-set; ignored on submission.
	ID uint `json:"id,omitempty"`
	// DeviceScanID is the parent scan's primary key. Server-set.
	DeviceScanID uint `json:"deviceScanID,omitempty"`
	// Client is the canonical client identifier (e.g. "claude_code").
	Client string `json:"client"`
	// SessionID is the source-session UUID this prompt was extracted from.
	SessionID string `json:"sessionID"`
	// ChunkID is a stable per-prompt identifier; unique within a scan.
	ChunkID string `json:"chunkID"`
	// Model is the assistant model recorded on the first assistant turn.
	Model string `json:"model,omitempty"`

	// StartedAt is when the user turn began.
	StartedAt Time `json:"startedAt"`
	// EndedAt is when the prompt's last assistant turn completed.
	EndedAt Time `json:"endedAt"`
	// DurationMs is EndedAt - StartedAt in milliseconds.
	DurationMs int64 `json:"durationMs"`

	// Cwd is the working directory recorded on the user entry.
	Cwd string `json:"cwd,omitempty"`
	// GitBranch is the active git branch recorded on the user entry.
	GitBranch string `json:"gitBranch,omitempty"`

	// PromptText is the truncated user prompt (≤2048 bytes, UTF-8 safe).
	PromptText string `json:"promptText,omitempty"`
	// PromptHash is the SHA-256 (64 hex chars) of the full untruncated prompt.
	PromptHash string `json:"promptHash"`
	// PromptBytes is the full untruncated prompt length in bytes.
	PromptBytes int64 `json:"promptBytes"`

	// Metrics are the transitive token totals for the prompt
	// (parent session + every nested subagent).
	Metrics DeviceScanPromptMetrics `json:"metrics"`
	// MainMetrics is the parent-session-only token totals (subagent
	// internal usage excluded). Lets the UI show "parent context cost"
	// vs "actual cost including subagents" without re-summing.
	MainMetrics DeviceScanPromptMetrics `json:"mainMetrics"`
	// ToolCalls aggregates the parent session's tool_use blocks as
	// {name, count}, sorted by count desc. Tool calls a subagent
	// performed internally live on the subagent node, not here.
	ToolCalls []DeviceScanPromptToolCall `json:"toolCalls,omitempty"`
	// Subagents is the recursive subagent tree rooted at this prompt.
	// Server-enforced depth cap is 5.
	Subagents []DeviceScanPromptSubagent `json:"subagents,omitempty"`
	// Steps is the ordered timeline of user / thinking / text / tool_use
	// / tool_result / subagent_call entries that make up this prompt's
	// turn. Populated when --include-top-prompts is set. Capped at 2000
	// entries per prompt by server validation.
	Steps []DeviceScanPromptStep `json:"steps,omitempty"`
}

// DeviceScanPromptStep is one ordered timeline entry inside a prompt
// (one block of an assistant message, a user input, a tool call, a
// tool result, or a synthetic subagent_call marker). The list of steps
// reproduces the shape of the turn for the admin drilldown without
// shipping full transcripts: every text-bearing step carries a
// truncated head, the SHA-256 hash of the full content, and the full
// content's byte length.
type DeviceScanPromptStep struct {
	// Kind identifies what the step represents. One of: "user",
	// "thinking", "text", "tool_use", "tool_result", "subagent_call".
	Kind string `json:"kind"`
	// Context is "main" for steps in the parent session and "subagent"
	// for steps that originate inside a spawned subagent.
	Context string `json:"context"`
	// SubagentID matches DeviceScanPromptSubagent.SubagentID when the
	// step belongs to a subagent's own transcript; empty for main-
	// context steps. For a synthetic subagent_call step (kind ==
	// "subagent_call") this points at the spawned subagent's node.
	SubagentID string `json:"subagentID,omitempty"`
	// StartedAt is when this step began, taken from the underlying
	// JSONL entry's timestamp.
	StartedAt Time `json:"startedAt"`
	// DurationMs is the step's wall-clock duration when computable;
	// 0 otherwise.
	DurationMs int64 `json:"durationMs,omitempty"`
	// ToolUseID is the upstream tool_use block id (populated on
	// kind == "tool_use" and kind == "subagent_call").
	ToolUseID string `json:"toolUseID,omitempty"`
	// ToolName is the tool name (populated on kind == "tool_use").
	ToolName string `json:"toolName,omitempty"`
	// ToolInputKeys are the top-level keys of the tool's input object.
	// Values are deliberately not shipped — same redaction pattern as
	// EnvKeys on DeviceScanMCPServer.
	ToolInputKeys []string `json:"toolInputKeys,omitempty"`
	// ToolUseRef links a tool_result step back to its originating
	// tool_use step's ToolUseID. Empty when the upstream link cannot
	// be resolved.
	ToolUseRef string `json:"toolUseRef,omitempty"`
	// IsError is true when a tool_result step represents a failed
	// tool execution.
	IsError bool `json:"isError,omitempty"`
	// TextHead is the truncated, UTF-8 safe head of the step's
	// text-bearing content (≤512 bytes). Populated for user /
	// thinking / text / tool_result steps and for the subagent_call
	// step's description.
	TextHead string `json:"textHead,omitempty"`
	// TextBytes is the full untruncated content length in bytes.
	TextBytes int64 `json:"textBytes,omitempty"`
	// TextHash is the SHA-256 (64 hex chars) of the full untruncated
	// content. Empty when the step has no text content (tool_use,
	// some subagent_call steps) or when the head is a synthetic
	// placeholder (e.g. image blocks).
	TextHash string `json:"textHash,omitempty"`
	// Tokens are the per-step token attribution derived from the
	// owning assistant turn's usage.
	Tokens DeviceScanPromptStepTokens `json:"tokens"`
	// AccumulatedContextTokens is the running sum of
	// Input + CacheRead + CacheCreation across the step's context
	// (main timeline vs. a single subagent timeline) up to and
	// including this step.
	AccumulatedContextTokens int64 `json:"accumulatedContextTokens,omitempty"`
}

// DeviceScanPromptStepTokens is the per-step 4-component token
// breakdown. Same field semantics as DeviceScanPromptMetrics minus
// the derived TotalTokens.
type DeviceScanPromptStepTokens struct {
	Input         int64 `json:"input,omitempty"`
	Output        int64 `json:"output,omitempty"`
	CacheRead     int64 `json:"cacheRead,omitempty"`
	CacheCreation int64 `json:"cacheCreation,omitempty"`
}

// DeviceScanPromptMetrics is the 4-component token breakdown plus the
// derived TotalTokens used for ranking. Matches claude-devtools'
// SessionMetrics shape.
type DeviceScanPromptMetrics struct {
	InputTokens         int64 `json:"inputTokens"`
	OutputTokens        int64 `json:"outputTokens"`
	CacheReadTokens     int64 `json:"cacheReadTokens"`
	CacheCreationTokens int64 `json:"cacheCreationTokens"`
	// TotalTokens is the ranking metric. Equal to InputTokens + OutputTokens.
	TotalTokens int64 `json:"totalTokens"`
}

// DeviceScanPromptToolCall is one row of the tool-name aggregate.
type DeviceScanPromptToolCall struct {
	Name  string `json:"name"`
	Count int    `json:"count"`
}

// DeviceScanPromptSubagent is one node in a prompt's recursive
// subagent tree. CLI caps depth at 5; deeper nodes are folded into
// their level-5 ancestor's Metrics.
type DeviceScanPromptSubagent struct {
	// SubagentID is the stable, scan-local identifier for this node.
	// Empty on legacy M1 rows; populated by M2 CLIs so step entries
	// can reference the node via DeviceScanPromptStep.SubagentID.
	SubagentID string `json:"subagentID,omitempty"`
	// SubagentType is the subagent's declared type (free-form string).
	SubagentType string `json:"subagentType,omitempty"`
	// Description is the subagent's declared description.
	Description string `json:"description,omitempty"`
	// Metrics is this node's internal token totals, transitively
	// summed over its own descendants.
	Metrics DeviceScanPromptMetrics `json:"metrics"`
	// MainSessionImpact is the token cost the direct parent paid to
	// invoke this subagent (Task tool_use input + tool_result output).
	MainSessionImpact DeviceScanPromptSubagentImpact `json:"mainSessionImpact"`
	// ToolCalls aggregates this subagent's own tool_use blocks as
	// {name, count}. Precomputed at scan time because the server
	// never receives the subagent transcript.
	ToolCalls []DeviceScanPromptToolCall `json:"toolCalls,omitempty"`
	// Subagents are the children this subagent spawned via the Task
	// tool. Recursive; depth capped at 5 across the whole tree.
	Subagents []DeviceScanPromptSubagent `json:"subagents,omitempty"`
}

// DeviceScanPromptSubagentImpact is the parent-context cost of
// invoking a subagent — what the direct parent paid in input/output
// tokens for the Task call and its result.
type DeviceScanPromptSubagentImpact struct {
	// CallTokens is the Task tool_use's input_tokens.
	CallTokens int64 `json:"callTokens"`
	// ResultTokens is the Task tool_result's output_tokens.
	ResultTokens int64 `json:"resultTokens"`
	// TotalTokens is CallTokens + ResultTokens.
	TotalTokens int64 `json:"totalTokens"`
}

type DeviceScanPromptList List[DeviceScanPrompt]

// DeviceScanPromptResponse is returned by GET /api/devices/scans/{id}/prompts.
type DeviceScanPromptResponse struct {
	DeviceScanPromptList `json:",inline"`
	Total                int64 `json:"total"`
	Limit                int   `json:"limit"`
	Offset               int   `json:"offset"`
}
