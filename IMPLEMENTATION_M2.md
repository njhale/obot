# IMPLEMENTATION: Top K Prompts — Milestone 2 (Timeline Drilldown)

Phase-by-phase plan for the milestone-2 follow-on to
[IMPLEMENTATION.md](./IMPLEMENTATION.md). M1 shipped a rollup-only
view of the top-K prompts (truncated prompt text + hash + per-prompt
and per-subagent token totals + tool-call aggregates). M2 adds an
ordered per-prompt **timeline** so the admin drilldown renders the
shape of the turn — thinking blocks, tool calls with inputs, tool
results, subagent invocations — in the same style as
`claude-devtools`.

The timeline is **mandatory** whenever `--include-top-prompts` is set;
there is no second opt-in flag. The existing privacy ratchet — "no
tool inputs/outputs, assistant text, thinking blocks leak" — is
deliberately rewritten to: "all such content is shipped truncated +
hashed under the same `--include-top-prompts` consent." See Phase 0
for the doc rewrite.

Scope is still Claude Code only. The hooks built into other phases of
M1 stay; no additional client extractors ship in M2.

## Phase map

```
M2-Phase 0  Wire + persistence + privacy rewrite  ─┐
M2-Phase 1  CLI extraction of timeline steps      ─┤── 0 → 1
M2-Phase 2  UI timeline drilldown rebuild         ─┤── 1 → 2
M2-Phase 3  Privacy docs + help text + CI polish  ─┘── after 2
```

Each phase is a single PR. Phase 2 may start against mocked data once
Phase 0 lands, but cannot merge until Phase 1 lands so the
acceptance-criteria screenshots are real.

---

## Phase 0 — Wire + persistence + privacy rewrite

**Goal.** Server accepts and round-trips a `steps[]` array on every
`DeviceScanPrompt`, persisted as a single JSONB blob. Privacy docs
are rewritten so M1's "no content leaks" promise is replaced by M2's
"all content shipped truncated + hashed" promise.

**Deliverables.**

- `apiclient/types/devicescan.go`
  - New types:
    - `DeviceScanPromptStep` with fields:
      - `Kind` (string enum: `user` | `thinking` | `text` |
        `tool_use` | `tool_result` | `subagent_call`)
      - `Context` (string enum: `main` | `subagent`)
      - `SubagentID` (string; empty for main-context steps; matches
        `DeviceScanPromptSubagent.SubagentID` for nested steps)
      - `StartedAt` (Time), `DurationMs` (int64)
      - `ToolUseID`, `ToolName` (strings; populated on `tool_use`)
      - `ToolInputKeys` ([]string; top-level keys only — no values,
        same redaction pattern as `EnvKeys` on
        `DeviceScanMCPServer`)
      - `ToolUseRef` (string; populated on `tool_result`, links
        back to a previous step's `ToolUseID`)
      - `IsError` (bool; populated on `tool_result`)
      - `TextHead` (string; ≤512 bytes UTF-8 safe, populated on
        `user` / `thinking` / `text` / `tool_result`)
      - `TextBytes` (int64; full untruncated content length)
      - `TextHash` (string; 64-hex SHA-256 of the full untruncated
        content)
      - `Tokens` (`DeviceScanPromptStepTokens`)
      - `AccumulatedContextTokens` (int64; running sum of
        `Input + CacheRead + CacheCreation` up to and including
        this step)
    - `DeviceScanPromptStepTokens` with fields `Input`, `Output`,
      `CacheRead`, `CacheCreation` (int64 each).
  - `DeviceScanPrompt.Steps []DeviceScanPromptStep` added.
  - `DeviceScanPromptSubagent.SubagentID string` added so steps can
    reference tree nodes deterministically.
- DB migration: add `steps` JSONB column to `device_scan_prompts`
  (single blob — no per-step queries planned in M2). No index.
- `pkg/storage/apis/obot.obot.ai/v1/` — extend the CRD shape from
  M1 Phase 0 with the new fields.
- Server: extend the submit handler to validate and persist `Steps`
  in the same transaction. Validation rules:
  - `len(Steps) ≤ 2000` per prompt (errs with 400).
  - For each step: `Kind` and `Context` are in the allowed sets;
    `TextHead` ≤ 512 bytes; `TextHash` is empty or 64-hex; tokens
    ≥ 0; `SubagentID` matches a node in the recursive tree when
    `Context == "subagent"` (otherwise 400).
  - Cross-check: `tool_result.ToolUseRef` must match some earlier
    `tool_use.ToolUseID` within the same prompt's step list (warn
    + drop the bad row rather than rejecting the whole submission).
- Server: extend the GET endpoints from M1 Phase 0 to return
  `Steps` (no new endpoint surface). The convenience endpoint
  `/api/devices/latest-prompts/{device_id}` similarly includes
  `Steps`.
- DESIGN.md rewrite:
  - "Identified gaps" no longer lists timeline as deferred.
  - The privacy section is rewritten to enumerate the exact wire
    fields: truncated heads (≤512 B), SHA-256 hash, full byte
    length, tool-input *keys* (no values).
- IMPLEMENTATION.md rewrite of the **Cross-cutting requirements →
  Privacy ratchet** bullet: replace "no tool inputs/outputs,
  assistant text, thinking blocks leak" with the M2 content
  policy and link to the new doc page (Phase 3).

**Acceptance criteria.**

- `go test ./apiclient/... ./pkg/api/...` passes including a new
  handler-level test that submits a manifest whose `TopPrompts`
  carry `Steps` and fetches them back via every GET endpoint.
- DB migration runs forward and backward against the local Postgres
  dev image. Existing M1 rows are unaffected (the new column is
  nullable).
- `make validate-go-code` clean.
- DESIGN.md and IMPLEMENTATION.md updated; `git grep "no tool
  inputs/outputs"` returns zero hits.

**Scope notes.**

- `MainMetrics` and the parent `ToolCalls` aggregate on
  `DeviceScanPrompt` are kept. They are cheap rollups that the
  step list reproduces, but they remain queryable without parsing
  JSONB — useful for ranking and for the device-page table.
  Server-side validation reconciles them with the step totals
  (warn-and-fix rather than reject on mismatch).
- The new wire fields are additive; old CLIs that ship no `Steps`
  continue to work and produce an empty timeline on the drilldown.

---

## Phase 1 — CLI extraction of timeline steps

**Goal.** With `--include-top-prompts` set, each shipped prompt
carries a real `Steps` array reconstructed from the Claude Code
JSONL. Existing M1 fields (`PromptText`, `Metrics`, `MainMetrics`,
`ToolCalls`, recursive `Subagents`) continue to be populated and
reconcile with the step totals.

**Deliverables.** All under `pkg/devicescan/prompts/claudecode/`.

- `steps.go` — new file. Walks the entries belonging to a chunk
  (already grouped in M1 by `chunk.go`) and produces an ordered
  `[]types.DeviceScanPromptStep`. Mapping:
  - `UserEntry` with non-meta content → one `user` step. `TextHead`
    is the truncated user-visible body; `TextBytes` / `TextHash`
    cover the full untruncated content. Filtered user inputs
    (`<local-command-stdout>` etc., see M1 chunk filtering) do
    not appear in `steps`.
  - `AssistantEntry.message.content` → split into one step per
    block, preserving order:
    - `text` block → `text` step.
    - `thinking` block → `thinking` step. `signature` is dropped.
    - `tool_use` block → `tool_use` step. `ToolName` is the block
      name; `ToolUseID` is the block id; `ToolInputKeys` are the
      top-level keys of `input` (recursively flattened to dotted
      paths is **out of scope** — top-level only).
    - `image` block → emitted as a `text` step with
      `TextHead = "[image: <media_type>, <bytes>]"`,
      `TextBytes = 0`, `TextHash = ""`. No base64 ever ships.
  - `UserEntry.toolUseResult` (the synthetic tool-result wrapper
    Claude Code emits) → `tool_result` step. `ToolUseRef` is the
    `sourceToolUseID`; `IsError` mirrors the original block's
    `is_error`. `TextHead` is the truncated stringified content
    (matching claude-devtools' rendering — JSON-encoded if a list
    of blocks, raw string otherwise).
  - A `tool_use` step with `ToolName == "Task"` is also paired
    with a synthetic `subagent_call` step emitted immediately
    after, carrying the resolved `SubagentID` of the spawned
    subagent's tree node. The `subagent_call` step's `TextHead`
    is the truncated Task `description` field (no `prompt`).
  - Subagent JSONL files are walked recursively. Each subagent's
    own entries produce `context: "subagent"` steps with
    `SubagentID` set to the matching tree node. Steps are
    appended to the same flat list (the UI filters by
    `SubagentID` — see Phase 2).
- `tokens.go` — new file (or extension of existing `metrics.go`).
  Per-step token accounting:
  - `Input`, `Output`, `CacheRead`, `CacheCreation` per step are
    derived from the owning assistant turn's `message.usage`. When
    a single assistant turn produces multiple `tool_use` blocks,
    its `output_tokens` are proportioned across them by content
    size — same approach claude-devtools uses (see
    `contextAccumulator.ts`).
  - `AccumulatedContextTokens` is computed as the running sum of
    `Input + CacheRead + CacheCreation` across the chunk's main
    context. Subagent steps maintain their own running sum scoped
    to the subagent.
- `redact.go` — extend with `TruncateContent(s string, maxBytes
  int) (head string, fullBytes int64, hash string)`. M1's
  `TruncatePromptText` becomes a thin wrapper that pins
  `maxBytes = 2048`. New callers in `steps.go` use
  `maxBytes = 512`.
- `build.go` — call into `steps.go` after the existing per-chunk
  metric / subagent resolution; assemble `DeviceScanPrompt.Steps`.
  Reconcile `MainMetrics` against the per-step totals of
  `context == "main"` steps; reconcile each subagent node's
  `Metrics` against its own step totals. Log a warning when they
  drift by >1% and persist the rollup values (do not overwrite
  with step-derived totals — the rollup is authoritative).
- Tests:
  - New `fstest.MapFS` fixtures, additive to M1's:
    - single-turn user → assistant-with-thinking-and-text
      (verifies block ordering),
    - assistant turn with two `tool_use` blocks and two matching
      `tool_result` entries (verifies `ToolUseRef` linkage and
      token proportioning),
    - error tool result (verifies `IsError`),
    - subagent Task call with a 3-level-deep chain (verifies
      `SubagentID` linkage and per-subagent
      `AccumulatedContextTokens`),
    - assistant turn containing an `image` block (verifies the
      placeholder text + no base64 in the wire output),
    - extra-long thinking block (verifies 512 B truncation +
      hash + full byte count),
    - prompt at the 2000-step server cap (verifies that the
      CLI truncates and logs a warning rather than emitting
      something the server will reject).
  - Golden-output test: fixture session → known `Steps` JSON
    diffed against a checked-in expected file.

**Acceptance criteria.**

- All new tests pass on linux/darwin/windows.
- `obot scan --include-top-prompts 10 --dry-run` against a real
  `~/.claude/` returns prompts whose `Steps` arrays are non-empty
  and whose per-step totals roughly match
  `MainMetrics` / subagent `Metrics` (≤1% drift).
- A 50-MB fixture session still extracts inside the memory budget
  set in M1 Phase 2 (verified with `go test -benchmem`).
- The `steps` JSONB blob for a typical 100-turn session weighs
  ≤80 KB per prompt — verify on a real session.

**Risks.**

- Token proportioning across multiple `tool_use` blocks in one
  assistant turn is the most likely source of drift from the
  rollup. Mitigation: the golden-output test fixtures should
  include the exact proportioned values claude-devtools emits
  for the same input — borrowed from
  `~/projects/njhale/claude-devtools/test/`.
- `tool_result` linkage breaks if Claude Code ever emits a result
  whose `sourceToolUseID` doesn't match any earlier `tool_use.id`
  in the chunk's main context. Mitigation: emit the step anyway
  with an empty `ToolUseRef`; the UI tolerates unlinked results.
- Subagent step ordering: subagent JSONL timestamps and main JSONL
  timestamps share a wall clock but may interleave. Mitigation:
  flat step list ordered by `StartedAt`, with the UI filtering
  by `SubagentID`. Do not attempt to splice subagent steps into
  the main-context list at "Task call" time.

---

## Phase 2 — UI timeline drilldown rebuild

**Goal.** The drilldown route built in M1 Phase 5 is replaced with
a timeline-first view that mirrors the claude-devtools layout.
Admins can scan a prompt's main timeline, expand any subagent into
its own nested timeline, and read per-step thinking / tool-call /
result content (truncated) with per-step metrics inline.

**Deliverables.**

- `ui/user/src/lib/services/admin/types.ts` — TS types mirroring
  the new Go types from Phase 0: `DeviceScanPromptStep`,
  `DeviceScanPromptStepTokens`. Extend the existing
  `DeviceScanPromptSubagent` with `subagentID?: string`. Extend
  `DeviceScanPrompt` with `steps?: DeviceScanPromptStep[]`.
- `ui/user/src/lib/components/admin/device-scan/timeline/` — new
  directory:
  - `Timeline.svelte` — outer container. Props: `prompt`,
    `subagentID?` (omitted = main timeline; set = nested view).
    Filters the prompt's `steps[]` by `subagentID` and renders
    each step using the right item component. Renders a
    `MetricsPill` at the top showing the filtered subset's
    totals.
  - `StepThinking.svelte` — collapsed by default. Shows kind
    icon, ~100-char preview of `TextHead`, `MetricsPill`,
    duration. Expands to reveal the full `TextHead`,
    `TextBytes` + `TextHash` underneath in monospace.
  - `StepToolUse.svelte` — title is `ToolName`. Default-collapsed
    body lists `ToolInputKeys` as a compact key chip row, with a
    small note "(values redacted)". Carries a backreference to
    the matched `tool_result` step (resolved by walking `steps[]`
    for the first later step with `ToolUseRef == this.ToolUseID`)
    and renders the linked result inline as a child
    `StepToolResult` component — same idea as claude-devtools'
    `LinkedToolItem`.
  - `StepToolResult.svelte` — content head, `IsError` dot,
    `MetricsPill`. Used both standalone (when the result has no
    matching `tool_use` in the prompt) and embedded inside
    `StepToolUse`.
  - `StepText.svelte` — assistant text head, collapsed preview,
    expand to full `TextHead`, `MetricsPill`.
  - `StepSubagentCall.svelte` — shows the spawned subagent's
    `subagentType` + truncated `description`, a `MetricsPill`
    showing `mainSessionImpact`, and an expand/collapse toggle.
    When expanded, renders a nested `Timeline` with
    `subagentID={step.subagentID}` underneath. The nested
    `Timeline` recursively handles further `subagent_call`
    steps the same way.
  - `MetricsPill.svelte` — compact chip showing
    `Input | Output | CacheRead | CacheCreation` as a 4-segment
    bar plus a total. Reused per-step and as the timeline header.
    Visual style mirrors claude-devtools' `MetricsPill.tsx`.
- `ui/user/src/routes/admin/devices/[device_id]/prompts/[chunkID]/+page.svelte`
  — rewritten:
  - **Header** card (kept): prompt text, hash, byte length, model,
    cwd, gitBranch, timestamps, duration.
  - **Tokens** card (kept): parent vs. transitive 4-component
    bars + the "parent saw N, subagents consumed M extra" line.
  - **Timeline** section (new): renders
    `<Timeline {prompt} />` for the main context.
  - The standalone tool-calls table and recursive subagent tree
    from M1 Phase 5 are **removed** — both are subsumed by the
    timeline. The `TopPromptsTable.svelte` row-summary columns
    (tool count, subagent count) on the device page stay.
- `ui/user/src/lib/components/admin/device-scan/SubagentNode.svelte`
  is **deleted**.

**Acceptance criteria.**

- `pnpm run ci` clean.
- Manual: against a real captured prompt with mixed thinking +
  multi-tool turns + a 3-level-deep subagent chain, the timeline
  renders in chronological order, every step shows a
  `MetricsPill`, tool calls visibly link to their results, and
  subagent expansion shows that subagent's own filtered timeline.
- Manual: a prompt with zero `Steps` (legacy M1 row or new row
  whose extraction failed) shows a single empty-state line under
  the timeline section instead of the section header alone.
- Manual: keyboard navigation works — every step is a focusable
  expand/collapse control.

**Risks.**

- Recursive `Timeline` rendering with deep subagent chains could
  jank on large prompts. Mitigation: default every subagent
  expansion to collapsed; lazy-evaluate the filtered step list
  per-`subagentID` via `$derived`.
- The visual density needs to match claude-devtools without
  blowing up the existing admin layout. Mitigation: borrow
  spacing / typography directly from claude-devtools'
  `DisplayItemList.tsx` and item components; do not reinvent.

---

## Phase 3 — Privacy docs + help text + cross-platform CI

**Goal.** Verify the extraction on macOS, Linux, and Windows.
Finish documentation. Update help text so admins understand the
upgraded content policy before they enable the flag.

**Deliverables.**

- CI: ensure the matrix runs Go tests for
  `pkg/devicescan/prompts/claudecode/...` on linux/darwin/windows
  including the new step-extraction fixtures.
- `obot scan --help` text re-reviewed. The privacy paragraph now
  enumerates exactly what's uploaded under
  `--include-top-prompts`: truncated user prompt text (≤2 KiB),
  truncated heads of every assistant text / thinking block / tool
  result (≤512 B each), tool *names* and top-level input *keys*
  (no values), SHA-256 hashes and full byte lengths of all of the
  above.
- `docs/` page from M1 Phase 6 is rewritten to cover the timeline.
  Includes a screenshot of the drilldown. The empty-state link
  from the device page (M1 Phase 4) points at the new content.
- README touch-up: the line about `--include-top-prompts` is
  updated to mention timeline data.
- Smoke test on Windows: a manual session running
  `obot scan --include-top-prompts 10 --dry-run` against a real
  `~/.claude/` directory. Record the output (one trimmed prompt
  with its `steps[]`) in the PR description.

**Acceptance criteria.**

- CI green on all three OS targets.
- `make validate-go-code` clean.
- Docs page renders correctly under `make serve-docs`.

---

## Cross-cutting requirements (apply to every phase)

- **No regressions.** `make test`, `make lint`, `pnpm run ci`,
  `make validate-go-code` stay clean.
- **No new dependencies.** Step extraction continues to use only
  stdlib (`bufio`, `encoding/json`, `crypto/sha256`, `unicode/utf8`).
- **Read-only filesystem access.** No phase writes to `~/.claude/`
  or any user directory.
- **Privacy policy (rewritten from M1).** Every phase that touches
  the upload path must verify — by code review and a checked-in
  unit test — that:
  - No tool input *values* leak (only top-level keys ship).
  - No content longer than its declared cap ships (≤2048 B prompt
    text, ≤512 B per step head).
  - Hashes are computed over the *full* untruncated content so an
    admin can confirm two prompts had identical content without
    seeing it.
  - No image-block base64 data leaks.
  - No subagent transcript reaches the server outside the `steps`
    array's truncated heads.

## Out of scope (deferred to a later milestone)

- Other clients (codex, opencode, cursor) — same as M1.
- Compact-boundary steps (Claude Code's conversation compaction
  markers).
- Interruption-marker steps.
- Slash-command steps (`/clear`, `/compact`, custom commands).
- Per-step queryable storage (a `device_scan_prompt_steps` table
  rather than a JSONB blob) — only worth it once we want
  fleet-wide step filtering.
- Cross-scan step diff ("what changed in this prompt's shape
  between yesterday and today?").
- USD cost computation per step.
- Configurable per-step redaction patterns (e.g., admin policy
  that drops text heads for specific tools).
