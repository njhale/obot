# DESIGN: Top K Prompts for `obot scan`

Companion to [FEATURE.md](./FEATURE.md). This document captures the design
decisions for the first milestone: surfacing the top-K highest token-usage
Claude Code prompts in the Obot admin UI, computed by an opt-in flag on
`obot scan`.

## Goals

- Admins/Owners/Auditors can see the top-N highest token-usage **prompts**
  collected from a user's device, attached to a specific `obot scan`
  submission.
- Each prompt row exposes: prompt text (truncated), total token breakdown,
  tool/subagent activity summary, model, timestamps.
- Drill-in view shows the per-tool and per-subagent breakdown that rolls up
  into the prompt's totals — closely matching the `claude-devtools`
  visualization at the chunk level.
- CLI gains an opt-in `--include-top-prompts <n>` flag (default off).
  When set, the CLI parses local Claude Code session logs, ranks prompts,
  and uploads the top N alongside the existing scan manifest.
- Only Claude Code is supported in milestone 1. Parser is structured so
  additional clients (codex, opencode, cursor, etc.) can be added later.
- Runs on every OS the existing `obot scan` runs on — macOS, Linux,
  Windows — with no OS-conditional code paths in the milestone-1
  scanner. See "Cross-platform support" below.

## Non-goals (milestone 1)

- Cost-in-USD calculation. We surface raw tokens (input / output /
  cache_read / cache_creation) only.
- Cross-scan / cross-device fleet-wide ranking endpoints. Prompts are
  scoped to a single `DeviceScan` row. The device-detail page can render
  the top prompts from the *latest* scan; aggregation across scans is a
  follow-up.
- Uploading file contents alongside the prompt timeline.
- Uploading **tool input values**. Tool-input *keys* (the top-level
  parameter names) ship; values do not. This is the same redaction
  pattern as `EnvKeys` on `DeviceScanMCPServer`. *(M2 update: M1's
  blanket exclusion of assistant text, tool outputs, subagent
  transcripts, and thinking blocks no longer applies — those ship as
  ≤512 B truncated heads plus SHA-256 hashes under the same
  `--include-top-prompts` consent. See "Privacy & safety".)*
- Other clients (codex, opencode, cursor, …). Hooks are designed for
  extension but only `claude_code` ships.

## Decisions (locked)

| Decision | Choice |
|---|---|
| Unit of "prompt" | A top-level (non-meta) user turn. Token totals accumulate from that turn until the next top-level user turn. |
| Upload scope | Aggregates + truncated prompt text + per-step timeline (≤512 B truncated heads + SHA-256 + full byte length for every user / assistant text / thinking / tool result; tool *names* and top-level input *keys* — no values; image blocks shipped as a `[image: ...]` placeholder, never base64). Capped at 2000 steps per prompt. |
| Ranking window | Hardcoded 30-day look-back, computed CLI-side. Top-K-per-scan uploaded; server does not re-rank. |
| CLI flag default | Opt-in, off by default. `obot scan` behaves identically without `--include-top-prompts`. |
| Prompt-text upload | Uploaded by default when `--include-top-prompts` is on. First 2 KiB + SHA-256 of full text + full-text length. |
| Subagent attribution | Full transitive rollup. Subagent internal token totals are summed into the prompt; `mainSessionImpact` (Task call + result tokens visible to the parent) is recorded separately per subagent. |
| Server storage | New child resource `DeviceScanPrompt`, FK to `DeviceScan` (sibling to `DeviceScanMCPServer`, etc.). |
| Rank metric | `totalTokens = input + output`, matching `claude-devtools` `SessionMetrics.totalTokens`. Full 4-component breakdown surfaced on every row. |
| UI placement | New "Top Prompts" section on the existing device-detail page and scan-detail view. Drill-in at `/admin/devices/[device_id]/prompts/[id]`. |
| Hard N cap | `10`. Values above the cap return an error. |

## CLI surface

```
obot scan [existing flags] \
  [--include-top-prompts <n>]      # opt-in. 1..10. Off by default.
```

- Default behavior of `obot scan` is unchanged.
- `--include-top-prompts <n>` enables the new behavior. `n` must be in
  `[1, 10]`; otherwise the CLI errors before submitting.
- `--dry-run` includes the computed top prompts in the printed manifest
  so users can inspect exactly what would be uploaded (including the
  truncated prompt text) before submitting.

## Claude Code log parsing

Source paths (read-only, under `$HOME`):

```
~/.claude/projects/<encoded-project>/<session-uuid>.jsonl       # main agent
~/.claude/projects/<encoded-project>/<session-uuid>/agent_*.jsonl  # subagents (new)
~/.claude/projects/<encoded-project>/agent_*.jsonl                 # subagents (legacy)
```

Time filtering uses the JSONL file's modification time against the
30 day window. Files outside the window are skipped
entirely. Within the window, JSONL is streamed line-by-line — never fully
loaded.

### Prompt extraction (mirrors `claude-devtools`' chunk model)

For each session file:

1. Walk entries in order. A **prompt chunk** begins on the first entry
   that satisfies `isParsedUserChunkMessage` — i.e. `type=user`,
   `isMeta != true`, content contains real text (not solely
   `<local-command-stdout>`, `<local-command-caveat>`, or
   `<system-reminder>`). Slash commands (`<command-name>`) count as real
   user input.
2. The chunk continues across subsequent `assistant` entries, tool
   `user`/`tool_result` entries, and interruptions, until the next
   top-level user message starts the next chunk.
3. Each chunk aggregates `SessionMetrics`-style counters from
   `assistant.message.usage` on every assistant turn inside it:
   `inputTokens`, `outputTokens`, `cacheReadTokens`, `cacheCreationTokens`,
   plus derived `totalTokens = input + output`.
4. Tool executions are extracted from each **parent-session** assistant
   turn's `tool_use` blocks (mirroring claude-devtools'
   `AIChunk.toolExecutions`, which is built from `responses` only in
   `ChunkFactory.ts:125`). For each tool name, we record `{name, count}`
   on the prompt's top-level `toolCalls`. Note that `Task` tool calls
   that spawn subagents appear here once per spawn.
5. Subagents form a **tree**, not a flat list, mirroring how
   claude-devtools lazily recurses in `SubagentDetailBuilder.ts:43-79`
   (re-running `subagentResolver.resolveSubagents` with the child's ID
   as the parent sessionId, then `buildChunksFn(messages, nestedSubagents)`):
   - Direct children: sidechain session files (`isSidechain=true`) whose
     `sessionId` points back to the prompt's parent session and whose
     first activity falls within the chunk's time bounds.
   - Nested children: the same resolution is then run with each direct
     child's session ID as the parent, capturing grandchildren, and so
     on. Subagent depth is capped at 5 to bound payloads; deeper
     subagents are dropped (with a CLI warning) and their tokens folded
     into their level-5 ancestor's `metrics`.
   - Each node in the tree carries its own `metrics`, `mainSessionImpact`,
     `toolCalls`, and recursive `subagents`.
6. For each subagent node (at every depth), we run the same `tool_use`
   extraction over **its own messages** and store the `{name, count}`
   aggregate on that node's row. This is the precomputed equivalent of
   claude-devtools running `buildToolExecutions(process.messages)` on
   demand — we have to precompute it at every level because the server
   never receives the subagent transcripts.
7. Chunks with no completed assistant turn yet (no `stop_reason`, no
   downstream messages) are skipped — they have no meaningful token
   totals to rank.

### Per-prompt fields the CLI computes

For each candidate prompt chunk:

- `client = "claude_code"`
- `sessionID` (UUID of the parent .jsonl)
- `chunkID` (stable hash of `sessionID + first-assistant-uuid`)
- `model` (from the first assistant turn's `message.model`)
- `startedAt`, `endedAt` (RFC3339)
- `durationMs`
- `cwd`, `gitBranch` (from the user entry)
- `promptText` (truncated to 2048 bytes, valid UTF-8 boundary; lossy
  truncation marker `…` appended when shortened)
- `promptHash` (SHA-256 of the full, untruncated prompt text)
- `promptBytes` (full untruncated length)
- `metrics`: `{ inputTokens, outputTokens, cacheReadTokens,
  cacheCreationTokens, totalTokens }` — transitively rolled up.
- `mainMetrics`: same shape, but only the parent session's contribution
  (subagent internals excluded). Lets the UI show
  "parent context vs actual cost" without re-summing.
- `toolCalls`: `[{name: string, count: int}]`, sorted by count desc.
  **Parent-session only** — does not include tool calls a subagent made
  internally. The parent's `Task` call appears here once per spawn.
- `subagents`: recursive tree. Each node is `{ subagentType: string?,
  description: string?, metrics: {…}, mainSessionImpact: { callTokens,
  resultTokens, totalTokens }, toolCalls: [{name, count}],
  subagents: [...recursive] }`. Internal token metrics are the
  subagent's own rollup at that node; `mainSessionImpact` is what the
  *direct parent* session paid to invoke this subagent; `toolCalls` is
  the `{name, count}` aggregate over this subagent's own `tool_use`
  blocks; `subagents` carries its children. Depth capped at 5.

### Ranking

After all in-window sessions are parsed, all chunks are sorted by
`metrics.totalTokens` descending and the top `n` are kept.
Ties broken by `endedAt` descending (most recent first).

## API & types

### New types (`apiclient/types/devicescan.go`)

```go
// DeviceScanPrompt is one captured top-level user prompt with rolled-up
// token usage and tool/subagent activity. Attached to a DeviceScan.
type DeviceScanPrompt struct {
    DeviceScanID uint   `json:"deviceScanID"`
    Client       string `json:"client"` // "claude_code"
    SessionID    string `json:"sessionID"`
    ChunkID      string `json:"chunkID"` // unique within scan
    Model        string `json:"model,omitempty"`

    StartedAt Time `json:"startedAt"`
    EndedAt   Time `json:"endedAt"`
    DurationMs int64 `json:"durationMs"`

    Cwd       string `json:"cwd,omitempty"`
    GitBranch string `json:"gitBranch,omitempty"`

    PromptText  string `json:"promptText,omitempty"` // ≤2048 bytes
    PromptHash  string `json:"promptHash"`            // sha256 hex of full text
    PromptBytes int64  `json:"promptBytes"`           // full untruncated length

    Metrics     DeviceScanPromptMetrics     `json:"metrics"`     // transitive
    MainMetrics DeviceScanPromptMetrics     `json:"mainMetrics"` // parent-only
    ToolCalls   []DeviceScanPromptToolCall  `json:"toolCalls,omitempty"`
    Subagents   []DeviceScanPromptSubagent  `json:"subagents,omitempty"`
}

type DeviceScanPromptMetrics struct {
    InputTokens         int64 `json:"inputTokens"`
    OutputTokens        int64 `json:"outputTokens"`
    CacheReadTokens     int64 `json:"cacheReadTokens"`
    CacheCreationTokens int64 `json:"cacheCreationTokens"`
    TotalTokens         int64 `json:"totalTokens"` // input + output
}

type DeviceScanPromptToolCall struct {
    Name  string `json:"name"`
    Count int    `json:"count"`
}

type DeviceScanPromptSubagent struct {
    SubagentType       string                         `json:"subagentType,omitempty"`
    Description        string                         `json:"description,omitempty"`
    Metrics            DeviceScanPromptMetrics        `json:"metrics"`            // this node's internal totals (transitively summed over its own descendants)
    MainSessionImpact  DeviceScanPromptSubagentImpact `json:"mainSessionImpact"`  // tokens the direct parent paid to invoke this subagent
    // ToolCalls is the {name, count} aggregate computed from this
    // subagent's own tool_use blocks. Precomputed at scan time because
    // the server never receives the subagent transcript.
    ToolCalls          []DeviceScanPromptToolCall     `json:"toolCalls,omitempty"`
    // Subagents are the children this subagent spawned via the Task
    // tool. Recursive — mirrors how claude-devtools lazily resolves
    // nested subagents at drill-in time. CLI caps depth at 5.
    Subagents          []DeviceScanPromptSubagent     `json:"subagents,omitempty"`
}

type DeviceScanPromptSubagentImpact struct {
    CallTokens   int64 `json:"callTokens"`   // Task tool_use input tokens
    ResultTokens int64 `json:"resultTokens"` // Task tool_result output tokens
    TotalTokens  int64 `json:"totalTokens"`  // callTokens + resultTokens
}
```

`DeviceScanManifest` gains:

```go
// TopPrompts is populated when --include-top-prompts is set on `obot scan`.
TopPrompts []DeviceScanPrompt `json:"topPrompts,omitempty"`
```

### REST endpoints

| Method | Path | Purpose |
|---|---|---|
| `POST` | `/api/devices/scans` | Existing; now accepts `topPrompts` on the manifest. |
| `GET`  | `/api/devices/scans/{id}/prompts` | List prompts for a scan, sorted by `metrics.totalTokens` desc. Supports `limit`. |
| `GET`  | `/api/devices/scans/{id}/prompts/{chunkID}` | Single prompt with full subagent + tool breakdown. |
| `GET`  | `/api/devices/{deviceID}/prompts/latest` | Convenience: top prompts from the device's most recent scan. |

Auth: same admin/owner/auditor authorization as the existing scan
endpoints. No user-facing endpoint.

## Server-side storage

New table `device_scan_prompts` with FK `device_scan_id` referencing
`device_scans(id)` on cascade delete. `tool_calls` and `subagents`
persist as JSONB columns since they are write-once and only ever read
back as a whole for the drill-in view. Indexed by
`(device_scan_id, total_tokens DESC)` for the list endpoint.

Ingestion: the existing `SubmitDeviceScan` handler unmarshals
`TopPrompts` from the manifest and inserts rows in a single
transaction with the parent `DeviceScan` row. Per-row validation:

- `0 < len(PromptText) ≤ 2048`
- `PromptHash` is 64 hex chars
- `Metrics.TotalTokens == Metrics.InputTokens + Metrics.OutputTokens`
- `client` is one of the registered prompt-scanner client IDs
  (milestone 1: only `"claude_code"`; the allow-list is a constant the
  server reads, not a hard-coded string in the handler)
- `len(TopPrompts) ≤ 10`
- Subagent tree depth (max recursion through `subagents[].subagents[]`)
  ≤ 5. Payloads exceeding either cap are rejected.

## CLI implementation: pluggable per-client scanners

The CLI side mirrors the existing config-scanner extension pattern in
`pkg/devicescan/` (see `claudeCodeScanner`, `codexScanner`,
`cursorScanner`, etc., each implementing a small interface and
registered into a shared scan runner). Prompt scanning gets the same
treatment so adding a new client is "drop in a new package + register
it" with no changes to the CLI plumbing or the upload path.

### `PromptScanner` interface

```go
// pkg/devicescan/prompts/prompts.go

package prompts

import (
    "context"
    "io/fs"
    "time"

    "github.com/obot-platform/obot/apiclient/types"
)

// Options carries shared CLI inputs to every scanner.
type Options struct {
    HomeFS   fs.FS         // os.DirFS($HOME) at runtime
    HomeAbs  string        // absolute $HOME path for resolving symlinks
    Since    time.Time     // earliest activity to consider — CLI passes now-30d
    TopK     int           // 1..10 — number of prompts each scanner returns
}

// PromptScanner discovers and ranks top-K user prompts for one client.
//
// Implementations MUST be:
//   - read-only against HomeFS
//   - safe to run concurrently with other PromptScanners
//   - bounded in memory (stream files; never load full transcripts)
//   - resilient to malformed/partial logs (skip + warn, never panic)
//
// Each implementation owns its own log discovery, parsing, chunking,
// and token aggregation, but emits the shared types.DeviceScanPrompt
// shape so the manifest is uniform.
type PromptScanner interface {
    // Client is the stable identifier persisted in DeviceScanPrompt.Client
    // (e.g. "claude_code", "codex", "cursor"). Must be lowercase snake_case.
    Client() string

    // Presence reports whether the client appears installed/configured
    // on this device. Scanners whose Presence returns false are skipped
    // before any heavier parsing work.
    Presence(opts Options) bool

    // TopPrompts returns up to opts.TopK prompts ranked by
    // metrics.totalTokens descending. Returning fewer is fine.
    TopPrompts(ctx context.Context, opts Options) ([]types.DeviceScanPrompt, error)
}
```

### Registry

```go
// pkg/devicescan/prompts/registry.go

var registry []PromptScanner

func Register(s PromptScanner) { registry = append(registry, s) }
func All() []PromptScanner    { return registry }
```

Each client lives in its own sub-package and registers itself via
`init()`:

```
pkg/devicescan/prompts/
  prompts.go            // interface, Options, shared helpers
  registry.go           // Register / All
  redact.go             // truncate prompt text to 2 KiB on UTF-8 boundary,
                        // compute SHA-256 of full text — shared helper
  rank.go               // generic top-K + tie-breaker helper

  claudecode/           // milestone 1 — the only scanner that ships
    scanner.go          // PromptScanner impl; init() { prompts.Register(&S{}) }
    discover.go         // walk ~/.claude/projects, filter by Since
    jsonl.go            // streaming JSONL parser (matches the entry types
                        // in claude-devtools' src/main/types/jsonl.ts)
    chunk.go            // group entries into user-led chunks
    metrics.go          // aggregate usage across a chunk + subagents

  codex/                // future — empty in milestone 1
  opencode/             // future
  cursor/               // future
```

The CLI in `pkg/cli/scan.go` invokes the registry only when
`--include-top-prompts > 0`:

```go
var all []types.DeviceScanPrompt
for _, s := range prompts.All() {
    if !s.Presence(opts) {
        continue
    }
    p, err := s.TopPrompts(ctx, opts)
    // log+continue on per-scanner error so one bad client doesn't
    // nuke the whole submission
    all = append(all, p...)
}
// final top-K across all clients
manifest.TopPrompts = prompts.TopK(all, opts.TopK)
```

The new flags live on the existing `Scan` struct in `pkg/cli/scan.go`;
help text spells out the privacy implications.

### Cross-platform support

`obot scan` already targets macOS, Linux, and Windows (see `pkg/cli/scan.go`
populating `manifest.OS = runtime.GOOS` and the existing
`pkg/devicescan/` scanners using `os.UserHomeDir()` + `os.DirFS(home)`
with slash-separated `fs.FS` paths). The prompt-scanning extension
follows the same rules, with these specifics:

**Path resolution.** Claude Code uses the same on-disk layout on all
three OSes — `<userhome>/.claude/projects/<encoded-project>/<session-uuid>.jsonl`
where `<userhome>` is whatever `os.UserHomeDir()` returns
(`/Users/<u>` on macOS, `/home/<u>` on Linux, `C:\Users\<u>` on
Windows). No `runtime.GOOS` branching is needed for the milestone-1
scanner. The scanner reads `Options.HomeFS` (an `fs.FS` rooted at
home) and uses the `path` package — never `filepath` — for all
relative paths so a literal like `".claude/projects/foo"` works
identically on every platform. Conversions to absolute OS paths use
`filepath.Join(Options.HomeAbs, …)` exactly like the existing
`scanState.abs` helper.

**Line endings.** Claude Code on Windows may write JSONL with `\r\n`
terminators. The streaming parser uses `bufio.Scanner` with the
default `ScanLines` split function, which strips both `\r\n` and `\n`
— so the same parser handles all three OSes without conditionals.

**Hidden / system directories.** `pkg/devicescan/walk.go` already
skips macOS `~/Library` and Windows `~/AppData` when walking for
project configs (basename-keyed skip list). Prompt scanning targets
`~/.claude/` directly rather than walking from home, so this list is
not consulted; the new scanner still uses `os.DirFS(home)` so that
any future scanner needing to descend into `Library` (e.g. a
hypothetical macOS-only Claude Desktop log path) can opt in
explicitly.

**Symlinks and Windows junctions.** Session directories may contain
symlinks (developer setups, CI mounts). The scanner uses
`fs.WalkDir` with the default behavior — following symlinks that
resolve to regular files — but caps walk depth at the
`{session-uuid}/agent_*.jsonl` level so a loop can't cause runaway
traversal. Junction reparse points on Windows are treated identically
by Go's `fs` package.

**Long paths on Windows.** `~/.claude/projects/<encoded-project>/<session-uuid>/agent_<agent-uuid>.jsonl`
can exceed the legacy 260-char MAX_PATH on deeply nested projects.
The Go toolchain enables long-path support automatically when
`LongPathsEnabled` is set in the Windows registry (default on
Windows 10 1607+ when the application manifest opts in, which Go's
runtime does). No additional `\\?\` prefixing is needed in our code.

**File locking / antivirus.** All JSONL reads open files with
`os.Open` (read-only, no exclusive lock) so Windows Defender's
real-time scan and concurrent Claude Code writes don't fail the
scan. Transient `ERROR_SHARING_VIOLATION` on Windows is retried once
after a 100 ms backoff inside the parser; persistent failure logs a
warning and skips the file (consistent with the "skip + warn, never
panic" requirement in the `PromptScanner` contract).

**Per-OS config paths for future scanners.** The `PromptScanner`
interface does not assume a single home-relative path. Scanners that
need OS-specific paths follow the same `configPaths []string` pattern
used by the existing config scanners (e.g. `claudeDesktopScanner`
listing both `Library/Application Support/Claude` and
`.config/Claude`). Each scanner is free to switch on `runtime.GOOS`
internally; the interface contract is the same on every OS.

**Tests.** Following the existing `scanners_test.go` pattern, every
prompt scanner gets table-driven tests against `fstest.MapFS` with
fixtures that exercise the slash-path code paths. CI runs Go tests
on linux/darwin/windows, matching the matrix already used for the
rest of the project, so platform-specific regressions are caught
without OS-conditional code in the scanner itself.

### Schema portability across clients

`DeviceScanPrompt` was designed so the same row works for non-Claude
clients with no schema migration in the common case. Three caveats —
see "Identified gaps" below.

**Per-field portability (universal unless noted):**

| Field | Portable | Notes |
|---|---|---|
| `client`, `sessionID`, `chunkID`, `startedAt`, `endedAt`, `durationMs`, `cwd` | Universal | Every plausible client log has direct or trivially-synthesized equivalents. |
| `gitBranch` | Best-effort | Claude Code records it on every entry; Codex/Cursor/Opencode have it inconsistently. Empty string is acceptable. |
| `model` | Lossy under model switches | Captures only the first assistant turn's model. Cursor in particular lets users toggle providers mid-chunk; we accept the lossiness rather than complicating the row. |
| `promptText`, `promptHash`, `promptBytes` | Universal | **Text-only.** Attachments (pasted files, images) are not hashed or counted; if a future client surfaces them prominently we'd add `promptAttachments []{kind,size,hash}` as a strictly additive field. |
| `metrics.inputTokens`, `metrics.outputTokens`, `metrics.totalTokens` | Universal | Anthropic `usage.input_tokens`/`output_tokens`; OpenAI `prompt_tokens`/`completion_tokens`. Map cleanly. |
| `metrics.cacheReadTokens` | Multi-provider | Anthropic `cache_read_input_tokens`; OpenAI `prompt_tokens_details.cached_tokens`. Zero for providers that don't expose it. |
| `metrics.cacheCreationTokens` | **Anthropic-only** | OpenAI's prompt cache is implicit (no "creation" surface). Zero for non-Anthropic. UI treats zero as "not reported." |
| `mainMetrics` | Optional | Equal to `metrics` for any client without a subagent concept. Tiny duplication on those rows; not worth a discriminator. |
| `toolCalls` | Universal | Any client with tool/function/MCP calls populates this. |
| `subagents` | Claude-only today | Empty slice on every other shipped client. Schema is generic — `subagentType` is a free string, `mainSessionImpact` can be `{0,0,0}` for clients whose subagent primitive doesn't have a parent-context cost, recursion is permissive. Opencode's "agents" or a future Cursor agent feature would slot in without changes. |

**Per-client stress test:**

| Client | Works as-is | Needs additive fields | Notes |
|---|---|---|---|
| Claude Code | ✓ M1 baseline | — | All fields used. |
| Codex CLI (OpenAI) | ✓ | — (M1 schema sufficient) | Maps `prompt_tokens` → `inputTokens`, `completion_tokens` → `outputTokens`, `prompt_tokens_details.cached_tokens` → `cacheReadTokens`, `cacheCreationTokens=0`. Empty `subagents`. `mainMetrics=metrics`. |
| Cursor | ✓ for token-instrumented chats | Likely `tokensReported bool` later | Cursor often surfaces only "fast-request" credits, not raw tokens, for non-API-key sessions. Where tokens are present (custom-key sessions), the schema fits. Where absent, we'd want a flag so the UI doesn't show misleading zeros. |
| Opencode | ✓ | — | Multi-provider; uses OpenAI/Anthropic-shaped usage. Subagents (when added to opencode) slot into the existing recursive shape. |
| Aider / Continue / Cline | ✓ | — | Simpler still — no subagents, MCP-shaped tool calls. |

**Identified gaps** (none block M1 or M2; named so they're not
surprises when a future client lands):

1. **Reasoning tokens.** OpenAI's o-series and GPT-5 expose
   `completion_tokens_details.reasoning_tokens` (think-step tokens
   billed separately from the visible output). Anthropic extended
   thinking is currently folded into `output_tokens`. Our schema
   collapses both into `outputTokens` — defensible (it's where they're
   billed) but loses the breakdown an admin investigating an
   o3-driven Cursor session might want. **Resolution:** if/when a
   client with first-class reasoning tokens lands, add
   `metrics.reasoningTokens int64` as an additive field; populate it
   when reported and treat it as a subset of `outputTokens` rather
   than a parallel quantity.
2. **Multi-model chunks.** Cursor (and to a lesser degree opencode)
   let the user switch model mid-conversation. `model` records only
   the first assistant turn; a chunk that started on Claude and
   finished on GPT-5 will look mono-model in the row. **Resolution:**
   if this becomes a problem in practice, change `model` to a
   `models []string` field (additive — old rows keep working if we
   keep `model` populated with `models[0]` during a deprecation
   window).
3. **Attachments in the prompt.** Cursor in particular routinely
   attaches files / images / @-mentioned code to the prompt. Those
   bytes aren't in `promptText` and don't contribute to `promptHash`
   /`promptBytes`. **Resolution:** add
   `promptAttachments []{kind: "file"|"image", bytes: int64, hash: string}`
   when a client with prominent attachment behavior lands. Strictly
   additive; M1 omits.


## UI surface

Two SvelteKit additions:

1. **Top Prompts table** as a section on
   `/admin/devices/[device_id]/+page.svelte`. Sources its data from
   `GET /api/devices/{deviceID}/prompts/latest`. Columns:
   - Prompt (truncated, with hover for the full 2 KiB text)
   - Started
   - Model
   - Total tokens (with a small bar chart breaking down
     input/output/cache_read/cache_creation, matching `claude-devtools`'
     four-component visual)
   - Tool calls (count)
   - Subagents (count)
   - Row click → drill-in

2. **Drill-in page** at
   `/admin/devices/[device_id]/prompts/[chunkID]/+page.svelte`. Sources
   data from `GET /api/devices/scans/{scanID}/prompts/{chunkID}`.
   Sections:
   - Header: prompt text (full 2 KiB), model, cwd, gitBranch, timestamps.
   - Metrics card: four-component token breakdown, transitive total,
     parent-only total (so the user can see "parent context vs actual
     cost"), duration.
   - Tool calls table: `name`, `count`, sorted by count desc.
   - Subagents table: `subagentType`, `description`, internal totals
     (4-component breakdown), main-session impact (call tokens / result
     tokens). Each row shows both numbers side by side, matching
     `claude-devtools` `Process.mainSessionImpact`.

The scan-detail view gets a "Top Prompts" subsection that links to the
same drill-in route, scoped to that specific scan.

## Privacy & safety

- The default `obot scan` is unchanged. Top-prompt collection — and
  the M2 per-prompt timeline that comes with it — requires an
  explicit `--include-top-prompts` flag. There is **one** opt-in.
  Setting the flag enables every field below in a single step; there
  is no separate timeline toggle.
- `--dry-run` prints the exact payload to be uploaded — including
  truncated prompt text *and* every step head — so users can inspect
  before submitting.
- Server validation rejects payloads that exceed any cap, include an
  unsupported client, or reference a subagent ID that isn't in the
  prompt's tree. Tool-result steps whose ToolUseRef can't be resolved
  are dropped with a warning rather than failing the whole upload.

### Exact wire fields under `--include-top-prompts`

What ships:

- **Prompt text**: truncated to ≤2 KiB UTF-8 safe; the full text's
  SHA-256 hash and untruncated byte length are included so admins
  can correlate duplicates without seeing the rest.
- **Per-step timeline**: ≤2000 steps per prompt, each one a single
  user input / assistant text block / thinking block / tool call /
  tool result / synthetic subagent_call marker, in the order they
  occurred. For every text-bearing step we ship:
  - a ≤512 B UTF-8 safe head of the content,
  - the full content's SHA-256 (64 hex chars),
  - the full content's untruncated byte length.
- **Tool calls**: tool *name* and the top-level *keys* of the input
  object. Input values are never shipped. Same redaction pattern as
  `EnvKeys` on `DeviceScanMCPServer`.
- **Image blocks** appear as a `[image: <media_type>, <bytes>]`
  placeholder string in `textHead`. Base64 bytes never cross the
  wire. `textHash` is empty for image placeholders.
- **Subagent transcripts** ship as `context: "subagent"` steps with
  the same per-step caps and redaction as the main timeline. They
  live in the same flat `steps[]` list, keyed by `SubagentID` so the
  drilldown UI can filter to one subagent's view.
- **Tokens**: per-step `{input, output, cacheRead, cacheCreation}`
  derived from each assistant turn's usage, plus a running
  `accumulatedContextTokens` for the step's context.

What does **not** ship:

- Full prompt text (above 2 KiB).
- Tool input values.
- Full assistant text, thinking blocks, or tool results (anything
  beyond the 512 B head; the SHA-256 lets two admins prove identity
  without the bytes).
- Image base64.
- File contents from the developer's machine.
- Any `~/.claude/`, `~/.config/`, or repo file the scanner doesn't
  already publish under a separate consent path.

## Mapping to `claude-devtools`

Our `DeviceScanPrompt` shape is a strict subset of claude-devtools'
in-memory model — chosen so the per-prompt visualization can mirror
theirs without the privacy cost of shipping full transcripts. Mapping:

| Obot field | claude-devtools source | Notes |
|---|---|---|
| (the chunk concept itself) | `AIChunk` paired with the preceding `UserChunk` (`src/main/types/chunks.ts`) | We collapse the User+AI chunk pair into a single row keyed on the user turn. |
| `chunkID` | derived from `UserChunk.id` (`user-${message.uuid}`) | They use the user message UUID; we hash `sessionID + first-assistant-uuid` to be stable even when the user UUID is missing. |
| `sessionID` | `Session.id` (`src/main/types/domain.ts:83`) | Same UUID — the JSONL filename. |
| `model` | first `AssistantEntry.message.model` in the chunk | Direct copy. |
| `startedAt` / `endedAt` / `durationMs` | `BaseChunk.startTime` / `endTime` / `durationMs` | Computed from the same JSONL `timestamp` fields. |
| `cwd`, `gitBranch` | `ConversationalEntry.cwd` / `gitBranch` | Direct copy. |
| `promptText` | `UserChunk.userMessage.content` (text portion) | claude-devtools displays the full text; we truncate to 2 KiB. |
| `promptHash`, `promptBytes` | (no equivalent) | Obot-only — needed because we don't ship full text. |
| `metrics.inputTokens` | `SessionMetrics.inputTokens` | Same definition: sum of `assistant.message.usage.input_tokens`. |
| `metrics.outputTokens` | `SessionMetrics.outputTokens` | Same. |
| `metrics.cacheReadTokens` | `SessionMetrics.cacheReadTokens` | Same. |
| `metrics.cacheCreationTokens` | `SessionMetrics.cacheCreationTokens` | Same. |
| `metrics.totalTokens` | `SessionMetrics.totalTokens` | Identical formula: `input + output` (cache tokens tracked but not in the rank key). |
| `mainMetrics` | the AIChunk's `metrics` *before* transitive subagent rollup | claude-devtools computes both views; we persist both so the UI can show them side-by-side. |
| `toolCalls[]` (top-level) | derived from `AIChunk.toolExecutions[]` (`ChunkFactory.ts:125` runs `buildToolExecutions(responses)` over **parent-session** messages only) | We keep only `{name, count}` — claude-devtools also keeps inputs/outputs/timing/result, which we deliberately drop. Parent-only, matching their structure. |
| `subagents[].metrics` | `Process.metrics` (`chunks.ts:40`) | Subagent's internal `SessionMetrics`. |
| `subagents[].mainSessionImpact` | `Process.mainSessionImpact` (`chunks.ts:56-63`) — `{callTokens, resultTokens, totalTokens}` | Same shape, same semantics. |
| `subagents[].toolCalls[]` | claude-devtools re-runs `buildToolExecutions(process.messages)` on demand from `Process.messages` for the drill-in | We precompute the `{name, count}` aggregate at scan time and persist it on the row, because the server never receives `Process.messages`. Without this field the drill-in would lose all subagent tool activity. |
| `subagents[].subagents[]` (recursive) | claude-devtools' lazy recursion in `SubagentDetailBuilder.ts:43-79` — re-runs `subagentResolver.resolveSubagents(projectId, subagentId, …)` with the child's ID as the parent sessionId, then `buildChunksFn(messages, nestedSubagents)` | claude-devtools' `Process` type is **flat** (no `processes` field) and discovers nested subagents on-demand at drill-in time. We materialize the tree eagerly because we don't ship transcripts. Depth capped at 5. |
| `subagents[].subagentType`, `description` | `Process.subagentType`, `Process.description` | Direct copy. |
| (not captured) | `Process.team`, `Process.isParallel`, `Process.parentTaskId`, `Process.isOngoing`, file-history snapshots, thinking blocks, phase/compaction breakdown, slash-command output | Out of scope for a "top prompts" admin view; would require either full transcripts or a richer data contract. Easy to add later as additive fields. |

**What admins lose vs running claude-devtools locally on a developer
machine** (M2 reduced this gap significantly): tool *input values*,
content past each step's 512 B head, compaction/phase visualization,
slash-command markers, and per-tool-call timing. **What they gain:**
centralized cross-device rollup, no need to install claude-devtools
on every developer's machine, and a server-enforced privacy boundary
(see "Exact wire fields under `--include-top-prompts`" above).

## Open follow-ups (out of milestone 1)

- Additional clients (codex, opencode, cursor): the interface and
  registry above are in place; each new client is a new sub-package
  under `pkg/devicescan/prompts/` plus a server allow-list entry.
- Cross-scan aggregation endpoints (top prompts across all of user X's
  scans in a time range).
- USD cost via a configurable pricing source.
- Configurable redaction (`--prompt-text-redact-regex`) for
  organizations with custom secret patterns.
- Optional second flag to suppress prompt-text upload while keeping
  aggregates (`--no-prompt-text`).
- Capturing claude-devtools' richer fields when justified by admin use
  cases: `Process.isParallel` / `parentTaskId` for subagent topology;
  `phaseBreakdown` for compaction-aware context cost.
