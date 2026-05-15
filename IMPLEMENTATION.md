# IMPLEMENTATION: Top K Prompts — Milestone 1

Phase-by-phase plan for delivering the milestone-1 scope of [FEATURE.md](./FEATURE.md)
as specified in [DESIGN.md](./DESIGN.md). Scope is Claude Code only;
hooks are built for additional clients but no additional client parsers
ship in this milestone.

## Phase map

```
Phase 0  API types + server skeleton    ─┐
Phase 1  CLI scanner framework          ─┤── can run in parallel
                                         │
Phase 2  Claude Code prompt scanner      │ ── depends on Phase 1
                                         │
Phase 3  End-to-end submission + GETs    │ ── depends on Phases 0, 2
                                         │
Phase 4  UI — device-page Top Prompts   ─┤── parallel after Phase 3
Phase 5  UI — drill-in page             ─┘
                                         │
Phase 6  Cross-platform CI + polish      │ ── depends on Phases 2-5
```

Each phase is a single PR. Phases marked "parallel" can be opened
concurrently once their dependencies merge.

---

## Phase 0 — API types and server skeleton

**Goal.** Wire format is real and the server accepts it as a no-op
(persists, returns via GET) so downstream phases can integrate against
a real endpoint.

**Deliverables.**

- `apiclient/types/devicescan.go`
  - `DeviceScanPrompt`, `DeviceScanPromptMetrics`,
    `DeviceScanPromptToolCall`, `DeviceScanPromptSubagent`,
    `DeviceScanPromptSubagentImpact` (recursive shape; depth cap noted
    in field comment).
  - `DeviceScanManifest.TopPrompts []DeviceScanPrompt` added.
- DB migration: `device_scan_prompts` table (PK `id`, FK `device_scan_id`
  on cascade delete, indexed `(device_scan_id, total_tokens DESC)`).
  Columns: scalar columns for indexed fields (`device_scan_id`,
  `client`, `chunk_id`, `total_tokens`, `started_at`); `tool_calls`
  and `subagents` as JSONB; `metrics` and `main_metrics` as flat
  columns or one JSONB column — pick whichever matches the existing
  `device_scan_*` table conventions in this repo.
- `pkg/storage/apis/obot.obot.ai/v1/` — new CRD-style resource for
  `DeviceScanPrompt` matching the patterns used by
  `DeviceScanMCPServer` etc.
- Server: extend the existing `SubmitDeviceScan` handler to unmarshal
  and insert `TopPrompts` in the same transaction as the parent
  `DeviceScan`. No business logic yet beyond the validation rules in
  DESIGN.md (text ≤ 2048 bytes, hash is 64 hex, total = input + output,
  client in allow-list, `len(TopPrompts) ≤ 10`, subagent depth ≤ 5).
- Server: stub two new endpoints:
  - `GET /api/devices/scans/{id}/prompts` — returns rows for a scan,
    ordered by `total_tokens DESC`.
  - `GET /api/devices/scans/{id}/prompts/{chunkID}` — single row.
  - (Convenience endpoint `/api/devices/{deviceID}/prompts/latest`
    deferred to Phase 3 so we can validate the scan-scoped paths first.)
- Auth: same admin/owner/auditor gate as existing scan endpoints.

**Acceptance criteria.**

- `go test ./apiclient/... ./pkg/api/...` passes including a new
  handler-level test that submits a manifest with `TopPrompts` and
  fetches it back via the GET.
- DB migration runs forward and backward against the local Postgres
  dev image.
- `make validate-go-code` is clean.

**Scope notes.**

- The new endpoints can ship behind no flag — they simply return
  empty arrays until Phase 2 produces real data. This is safer than
  feature-flagging.
- Validation errors return 400 with a structured error body matching
  the existing scan endpoint conventions.

---

## Phase 1 — CLI scanner framework

**Goal.** The `PromptScanner` interface, registry, and shared helpers
exist but no scanners are registered yet. CLI flag plumbing is in
place; `obot scan --include-top-prompts 10` runs end-to-end and
submits a manifest with `TopPrompts: nil`.

**Deliverables.**

- `pkg/devicescan/prompts/prompts.go`
  - `Options` struct (`HomeFS`, `HomeAbs`, `Since`, `TopK`).
  - `PromptScanner` interface (`Client`, `Presence`, `TopPrompts`).
- `pkg/devicescan/prompts/registry.go`
  - `Register(s PromptScanner)` and `All() []PromptScanner`.
- `pkg/devicescan/prompts/redact.go`
  - `TruncatePromptText(s string) (truncated string, fullBytes int64, hash string)`
    — truncates to ≤2048 bytes on a UTF-8 boundary, appends `…` when
    shortened, returns SHA-256 hex of the *full* (untruncated) input.
- `pkg/devicescan/prompts/rank.go`
  - `TopK(prompts []types.DeviceScanPrompt, k int) []types.DeviceScanPrompt`
    — sort by `metrics.totalTokens` desc, tie-break by `endedAt` desc,
    return first `k`.
- `pkg/cli/scan.go`
  - New `IncludeTopPrompts int` field on the `Scan` struct (env:
    `OBOT_SCAN_INCLUDE_TOP_PROMPTS`, default 0).
  - Validation: error before any work if value is outside `[0, 10]`.
  - When > 0, invoke `prompts.All()` and feed each registered
    scanner's results into `prompts.TopK`. Per-scanner errors are
    logged and skipped (one bad client must not nuke the scan).
  - `--dry-run` already prints the manifest; with no scanners
    registered, `TopPrompts` remains absent.
- Tests for `redact.go` (UTF-8 boundary cases, exact-length input,
  hash stability) and `rank.go` (sort stability, tie-breaker, k=0).

**Acceptance criteria.**

- `obot scan --include-top-prompts 10 --dry-run` prints a manifest
  with no `topPrompts` key (no scanners registered).
- `obot scan --include-top-prompts 11` errors with a clear message
  before any filesystem work.
- `obot scan` (no flag) is byte-identical to the pre-change behavior.

**Risks.**

- The CLI changes here are minimal but touch the existing scan path.
  Add a regression test that runs `obot scan --dry-run` (no flag) and
  diffs against a checked-in expected manifest fixture to catch
  accidental side-effects.

---

## Phase 2 — Claude Code prompt scanner

**Goal.** Real parser. After this phase, a developer can run
`obot scan --include-top-prompts 10 --dry-run` on their own machine
and see real prompt data extracted from `~/.claude/projects/`.

**Deliverables.** All under `pkg/devicescan/prompts/claudecode/`.

- `scanner.go` — `PromptScanner` implementation; `init()` registers
  it. `Presence()` reuses the existing `claudeCodeScanner.Presence()`
  (binary `claude` or directory `.claude`).
- `discover.go` — walks `~/.claude/projects/*` filtered by file mtime
  ≥ `now - 30d` (the 30-day window is hardcoded per DESIGN.md).
  Yields `(projectDir, sessionFile, sidechainDir)` tuples.
- `jsonl.go` — streaming line-by-line parser using `bufio.Scanner`.
  Strongly-typed entry types matching claude-devtools'
  `src/main/types/jsonl.ts`: `UserEntry`, `AssistantEntry`,
  `SystemEntry`, `SummaryEntry`, etc. Skip + warn on malformed lines.
  CRLF tolerant (default `ScanLines` behavior).
- `chunk.go` — groups entries into user-led chunks following the
  6-step algorithm in DESIGN.md "Prompt extraction." Filters
  non-real user input (`<local-command-stdout>`, `<local-command-caveat>`,
  `<system-reminder>`-only messages). Skips chunks with no completed
  assistant turn.
- `metrics.go` — aggregates token totals over a chunk's entries.
  Builds the `{name, count}` tool-call aggregate from each assistant
  turn's `tool_use` blocks. Parent-only.
- `subagents.go` — resolves the subagent tree:
  - Reads sidechain JSONL files (both new structure
    `{session-uuid}/agent_*.jsonl` and legacy structure
    `agent_*.jsonl` at the project root).
  - Matches sidechain `sessionId` and first-activity time to the
    parent chunk's bounds.
  - Recursively resolves grandchildren by re-running the resolver
    with the child's session UUID as the parent.
  - Caps recursion at depth 5; logs a warning and folds deeper
    activity into the level-5 ancestor's metrics.
  - For each subagent node, computes:
    - `metrics` (internal totals, transitively summed),
    - `mainSessionImpact` (Task `tool_use.input_tokens` +
      `tool_result.output_tokens` from the *direct parent's*
      session),
    - `toolCalls` (extracted from this subagent's own `tool_use`
      blocks),
    - `subagents` (recursive — children).
- `build.go` — top-level orchestrator: for each in-window session,
  yields candidate `types.DeviceScanPrompt` rows. Computes `chunkID`
  (stable hash of `sessionID + first-assistant-uuid`). Calls
  `prompts.TruncatePromptText` and assembles the final row.
- Tests:
  - `fstest.MapFS` fixtures covering:
    - single-turn prompt, no tools, no subagents,
    - multi-turn with tool calls,
    - prompt with a single subagent (new sidechain layout),
    - prompt with a single subagent (legacy sidechain layout),
    - prompt with nested subagents 3 levels deep,
    - prompt with subagents 6 levels deep (verifies cap),
    - prompt with malformed JSONL lines (verifies skip+warn),
    - prompt that's still ongoing (verifies it's dropped),
    - filtered-out user inputs (`<system-reminder>` only),
    - prompts older than 30 days (verifies they're skipped).
  - Golden-output test: a fixture session yields a known-shape JSON
    row that's diffed against a checked-in expected file.

**Acceptance criteria.**

- All new tests pass on linux/darwin/windows (CI matrix exercised in
  Phase 6 but tests pass on whatever the developer is running).
- `obot scan --include-top-prompts 10 --dry-run` against a real
  `~/.claude/` returns at most 10 rows with non-zero
  `metrics.totalTokens`, sorted descending, with truncated prompt
  text ≤ 2048 bytes.
- Memory usage stays bounded (no full-transcript load) under a
  fixture session of ~50 MB JSONL — verify with `go test -benchmem`.

**Risks.**

- Subagent matching by `sessionId` + time window may produce false
  positives if two top-level prompts are very close together.
  Mitigation: use the `parentTaskId` field (the Task `tool_use.id`)
  for matching when present; fall back to time window only when not.
- Token totals may drift from claude-devtools' if we miss a corner
  case. Mitigation: golden-output tests against fixtures borrowed
  from `~/projects/njhale/claude-devtools/test/` where shapes are
  known.

---

## Phase 3 — End-to-end submission

**Goal.** Production code path works: CLI parses, uploads, server
persists, server returns. The convenience endpoint also lands.

**Deliverables.**

- Server: implement
  `GET /api/devices/latest-prompts/{device_id}` — returns top prompts
  from the device's most recent scan that has any prompts. (The
  originally-proposed `/api/devices/{deviceID}/prompts/latest` shape
  collides with the existing `/api/devices/scans/` authz subtree;
  using a distinct second-segment literal avoids the conflict without
  fanning out the authz patterns.)
- Server: enrich the existing GET endpoints with the per-scan
  ordering / limit query params used by the UI (`?limit=10`).
- Server: ensure the `client` allow-list lives in one constant so
  adding `codex`, `cursor`, etc. is a one-line change later.
- CLI: `--dry-run` output documented in `obot scan --help` (mention
  that prompt text is uploaded truncated when `--include-top-prompts`
  is set; suggest `--dry-run` first).
- End-to-end test under `tests/integration/` that:
  1. boots the server with a temp DB,
  2. submits a manifest containing a hand-crafted `TopPrompts` array,
  3. fetches via all three GET endpoints and verifies shape +
     ordering + the cascade delete (delete the scan, prompts go).

**Acceptance criteria.**

- `make test-integration` passes including the new test.
- Running `obot scan --include-top-prompts 10` against a local dev
  server (`make dev`) persists rows that come back through
  `/api/devices/scans/{id}/prompts`.
- Hitting the new endpoints without admin/owner/auditor auth returns
  the same 401/403 the existing scan endpoints return.

---

## Phase 4 — UI: device-page Top Prompts section

**Goal.** Admins see the latest top prompts directly on the device
detail page. Read-only.

**Deliverables.**

- `ui/user/src/lib/services/admin/operations.ts` — new client
  functions: `getDevicePromptsLatest(deviceID)`,
  `getScanPrompts(scanID)`, `getScanPrompt(scanID, chunkID)`.
- `ui/user/src/lib/services/admin/types.ts` — TS types mirroring
  `apiclient/types/devicescan.go` (recursive subagent shape).
- `ui/user/src/lib/components/admin/device-scan/TopPromptsTable.svelte`
  (new) — table with columns:
  - Prompt (truncated, hover/click reveals full 2 KiB),
  - Started (RFC3339 → relative),
  - Model,
  - Tokens (compact 4-component bar: input/output/cache_read/cache_creation),
  - Tool calls count (parent + sum across subagent tree),
  - Subagents count (recursive total),
  - row click → `/admin/devices/[device_id]/prompts/[chunkID]`.
- `ui/user/src/routes/admin/devices/[device_id]/+page.svelte` — add a
  "Top Prompts" section rendering the new component.
- Empty state: if the latest scan has no `topPrompts`, render a
  one-line explainer noting the flag is opt-in and link to a docs
  anchor (docs page added in Phase 6).
- Tests: a Vitest component test against a mocked client response
  exercising empty / single-row / 10-row states.

**Acceptance criteria.**

- `pnpm run check` and `pnpm run lint` are clean.
- Manual: with a device that has top prompts, the section renders
  with sensible column widths at 1280px and 1440px viewports.
- Manual: empty state renders cleanly on a device whose scans didn't
  include top prompts.

---

## Phase 5 — UI: drill-in page

**Goal.** Per-prompt detail view with the recursive subagent tree and
per-node tool-call tables. The most UX-heavy phase.

**Deliverables.**

- `ui/user/src/routes/admin/devices/[device_id]/prompts/[chunkID]/+page.svelte`
  (new) + `+page.ts` (loader).
- Sections:
  1. **Header** — full 2 KiB prompt text (monospace box,
     copy-to-clipboard, hash + full length displayed underneath),
     model, cwd, gitBranch, timestamps, duration.
  2. **Metrics card** — 4-component token breakdown (the visual that
     mirrors claude-devtools), `totalTokens` headline,
     transitive-vs-parent-only comparison ("parent context saw N,
     subagents consumed M extra").
  3. **Tool calls table** — `name`, `count`, sorted by count desc.
     Parent-session only (subagent tool calls live under the
     subagent tree).
  4. **Subagent tree** — recursive component
     `SubagentNode.svelte` that renders one node and recursively
     renders its `subagents`. Each node shows:
     - `subagentType`, `description`,
     - internal `metrics` (4-component breakdown),
     - `mainSessionImpact` side-by-side with internal metrics so the
       admin sees "parent paid X to call; subagent actually consumed
       Y",
     - per-node `toolCalls` table,
     - expandable to reveal children.
- Scan-detail view: if a scan-detail page exists today, add a "Top
  Prompts" subsection linking to the same drill-in route. If no
  scan-detail page exists yet, defer — not blocking M1.
- Tests: Vitest component test for `SubagentNode.svelte` with a
  3-level-deep fixture.

**Acceptance criteria.**

- `pnpm run ci` is clean.
- Manual: navigation from device page row → drill-in → back works
  via browser back button and the explicit breadcrumb.
- Manual: a fixture with 3-level-deep subagents renders without
  layout collapse; collapsed by default with one-click expand.
- Manual: copy-to-clipboard on the prompt text works in Firefox and
  Chrome.

---

## Phase 6 — Cross-platform CI + polish

**Goal.** Verify the parser on macOS, Linux, and Windows; finish
documentation; sand the edges.

**Deliverables.**

- CI: ensure the matrix runs Go tests for the new
  `pkg/devicescan/prompts/...` packages on linux/darwin/windows. If
  the existing matrix already does, no change. If not, add it.
- CI: ensure `pkg/devicescan/prompts/claudecode/` tests run with
  `GOOS=windows` against the same `fstest.MapFS` fixtures.
- `obot scan --help` text reviewed — the privacy implications of
  `--include-top-prompts` (truncated text uploaded, hash uploaded)
  are spelled out in one short paragraph.
- `docs/` page: short admin-facing doc covering the feature, what
  the flag does, what's uploaded vs what isn't, and a screenshot of
  the drill-in. Linked from the empty state in Phase 4.
- README touch-up under the existing CLI section: one-line mention
  of the new flag.
- Smoke test on Windows: a manual session running `obot scan
  --include-top-prompts 10 --dry-run` against a real `~/.claude/`
  directory. Record the output in the PR description.

**Acceptance criteria.**

- CI green on all three OS targets.
- `make validate-go-code` clean.
- Docs page renders correctly under `make serve-docs`.

---

## Cross-cutting requirements (apply to every phase)

- **No regressions.** `make test`, `make lint`, `pnpm run ci`,
  `make validate-go-code` must stay clean.
- **No new dependencies** unless a comment in the PR justifies why.
  The parsing work uses only stdlib (`bufio`, `encoding/json`,
  `crypto/sha256`, `path`, `io/fs`).
- **Read-only filesystem access.** No phase writes to `~/.claude/`
  or any other user directory; everything is read-only with
  `os.DirFS(home)`.
- **Privacy ratchet (rewritten for M2 — see
  [IMPLEMENTATION_M2.md](./IMPLEMENTATION_M2.md) Phase 0 and the
  "Privacy & safety" section of DESIGN.md).** Every phase that
  touches the upload path must verify (by code review and a
  checked-in unit test) that:
  - tool input *values* never leak (only top-level keys ship),
  - no content longer than its declared cap ships (≤2048 B prompt
    text, ≤512 B per step head),
  - SHA-256 hashes are computed over the *full* untruncated content
    so two prompts can be confirmed identical without showing the
    bytes,
  - image-block base64 never crosses the wire (rendered as a
    `[image: …]` placeholder), and
  - file contents from the developer's machine never reach the
    server via `TopPrompts`.

## Out of scope (deferred to a later milestone)

- Other clients (codex, opencode, cursor).
- USD cost computation.
- Cross-scan / fleet-wide aggregation endpoints.
- Configurable redaction patterns.
- Reasoning-tokens / multi-model / prompt-attachment additive fields
  (documented in DESIGN.md "Identified gaps").
