---
title: "feat: Custom date-range picker alongside last-N-days slider"
type: feat
status: active
date: 2026-04-22
---

# feat: Custom date-range picker alongside last-N-days slider

## Overview

The Date Range section currently only supports "last N days back from today" via a 1–90 slider. This plan adds a second mode — a custom `[from, to]` picker — so users can backfill non-contiguous gaps (e.g., *"I already exported 90→50 and 30→1, but I need 49→31"*). A segmented toggle at the top of the Date Range section switches between the two modes. The backend already operates on explicit `startDate` / `endDate` strings internally, so most of the lift is a wider IPC contract plus UI wiring.

## Problem Frame

The current input model forces the user to re-fetch everything from today back to the earliest missing day, which:
- wastes Garmin API budget re-fetching already-cached recent days
- makes surgical backfills impossible (no way to target *only* days 49–31)
- is opaque — the user has to do calendar math in their head to turn "March 3 to March 21" into a `daysBack` value

The framing decision (`Modo 'Rango personalizado' + slider`) came from the brainstorm conversation on 2026-04-22. A coverage-timeline alternative was considered but deferred as a higher-scope follow-up.

## Requirements Trace

**Input UX**
- R1. Add a segmented toggle at the top of the Date Range section with two modes: `LAST N DAYS` (default, current slider) and `CUSTOM RANGE`.
- R2. In `CUSTOM RANGE` mode, show two date inputs: **From** and **To**. Hide the slider + ticks + big readout; keep the refresh-window select (it still applies in both modes).
- R3. First switch to custom mode defaults `To = today`, `From = today - (daysBack - 1)` so the user starts from the same window they were already looking at.
- R4. Below the inputs, show an inclusive span readout: `31 days · 2026-03-23 → 2026-04-22`. Updates live.
- R5. Persist the selected mode across launches in `localStorage` under `garmin_date_mode`. Do **not** persist the exact dates — always re-default from today on load.

**Validation**
- R6. Reject ranges where `from > to`, `to` is in the future, or span exceeds 90 days. Match the existing slider ceiling (1–90) to avoid a hidden second limit.
- R7. When invalid, disable the Export button and show a small inline error under the affected input.
- R8. Validation logic lives in a pure helper module so both the renderer (for instant UI feedback) and the main-process IPC handler (for defense-in-depth) call the same function.

**Backend contract**
- R9. The `download-health` IPC handler accepts either `{ daysBack }` (existing) or `{ startDate, endDate }` (new), with `refreshWindow` applying to both. If both are present, `startDate` / `endDate` wins; log which path was taken.
- R10. `runHealthExport` in `electron-app/garmin/exporter.js` takes the explicit range when provided and skips the `daysBack → startDate/endDate` derivation. The existing refresh-window and per-date cache logic are date-based and keep working unchanged.

## Scope Boundaries

- **Not** adding coverage visualization / gap detection (deferred — the brainstorm's higher-upside challenger option).
- **Not** supporting multiple discrete ranges in one run — users who need two disjoint ranges run the export twice.
- **Not** changing the 90-day ceiling. Same hard limit in both modes.
- **Not** persisting exact custom dates across launches — they go stale fast and freshness-by-default is safer.
- **Not** adding a full month-grid calendar widget. Using the browser-native `<input type="date">` is intentional: zero-dependency, accessible, keyboard-navigable, already themed-compatible.

## Context & Research

### Relevant Code and Patterns

- `electron-app/garmin/exporter.js:110–190` — `runHealthExport` currently derives `startDate` / `endDate` from `daysBack` at lines 185–189. This is the only derivation point; everything downstream already consumes explicit date strings.
- `electron-app/garmin/exporter.js:50–90` — `fetchDailyStepsChunked` and `generateDateRange` operate on `YYYY-MM-DD` strings. No change needed in these.
- `electron-app/garmin/cache.js:20–74` — cache is keyed by `date + endpoint`. `isWithinRefreshWindow(date, refreshDays)` is already date-based. No change needed.
- `electron-app/main.js:348–397` — `download-health` IPC handler: destructures opts, validates `daysBack` (1–90) and `refreshWindow` (1–7), then calls `runHealthExport`. This is where the widened contract lands.
- `electron-app/preload.js` — `downloadHealth: (opts) => ipcRenderer.invoke('download-health', opts)` is a pass-through. No change needed.
- `electron-app/util/redact.js` + `electron-app/util/__tests__/redact.test.js` — pattern for pure CommonJS util + `node:test` coverage. Same shape suits the new date-range helpers.
- `electron-app/renderer/index.html` section 02 (lines ~75–97) — current slider + refresh-window markup. The mode toggle inserts above the existing content; slider and custom-range inputs sit in two sibling sub-panels whose visibility is driven by a single `data-mode` attribute on the section.
- `electron-app/renderer/renderer.css` — existing `input[type="email"] / [type="password"]` styles (lines ~322–347) are the visual target for the new date inputs. The segmented toggle reuses the `.status-pill` border/color tokens for coherence.
- `electron-app/renderer/renderer.js:206–214` — `getOpts()` is the single point that constructs the IPC payload. Widening happens here.

### Institutional Learnings

- Structured errors with `errorCode` + `meta` propagate through the log pipeline (see `feat(client): structured error classification` and related plan `docs/plans/2026-04-21-004-feat-error-debuggability-and-bug-report-plan.md`). Validation failures on the new contract should use the same envelope (`errorCode: 'BAD_RANGE'` or similar) for consistency.

### External References

Skipped. Local patterns are solid, the feature is well-bounded, no novel domain.

## Key Technical Decisions

- **Two IPC payload shapes, one validation surface.** The handler accepts either `{ daysBack }` or `{ startDate, endDate }`. All validation — range order, span cap, future-date guard — lives in a pure helper imported by both the renderer and main. One source of truth, same error messages on both sides.
- **Browser-native `<input type="date">` over custom calendar widget.** Accessible, keyboard-friendly, zero new dependency, and the date-picker popover is styled-out by platform. A custom grid would be nicer visually but violates the brief's *"no new dependencies"* and adds ongoing carrying cost for little gain in a two-input UX. If a richer picker is ever warranted, revisit as part of the deferred coverage-timeline feature.
- **Segmented toggle, not a checkbox or dropdown.** Two modes, mutually exclusive, both equally primary — a two-segment rocker matches the industrial aesthetic and reads as a deliberate channel switch rather than an option tucked into a menu.
- **Default custom `From` to match the current slider position.** On first switch, the user sees the same window they were already inspecting, preventing a jarring jump to "1 week ago" or "empty inputs." Reduces perceived friction.
- **No persisted custom dates.** Dates in localStorage go stale by definition — reopening the app a week later with `To = 2026-04-22` would lie. The *mode* persists; the values don't.
- **Keep the 90-day ceiling identical.** Two modes, one hard limit. A custom-mode user who hits the cap gets the same ceiling as a slider user, avoiding the "why did this fail" surprise.

## Open Questions

### Resolved During Planning

- *Should custom mode allow unlimited historical range?* — No. Match the slider's 90-day ceiling for consistency. Users with genuine multi-month archives can run multiple exports.
- *Where should validation live?* — In a pure helper (`electron-app/renderer/date-range.js` with dual CommonJS/browser export) so both renderer and main call it.
- *Do we need a renderer test harness?* — No. Pure logic is extracted to a testable module; DOM wiring is verified manually. Follows the existing pattern (`util/redact.js` + test, `renderer.js` untested).

### Deferred to Implementation

- Exact wording of the span readout ("31 days · 2026-03-23 → 2026-04-22" vs "23 Mar → 22 Apr · 31 days"). Decide in Unit 4 once the typography lands.
- Whether the native `<input type="date">` popover needs additional CSS coaxing on macOS to sit well against the dark theme — verify once the input is styled and adjust if the indicator glyph is illegible.
- Whether to reuse the existing `range-display` big-number slot to show the span count in custom mode, or render a new compact label. Decide once both modes are side-by-side in the browser.

## High-Level Technical Design

> *This illustrates the intended approach and is directional guidance for review, not implementation specification. The implementing agent should treat it as context, not code to reproduce.*

**Mode → payload → validation mapping:**

| UI mode          | Renderer emits                              | Main validates via                     |
| ---------------- | ------------------------------------------- | -------------------------------------- |
| `last-n-days`    | `{ daysBack, refreshWindow }`               | existing `1 ≤ days ≤ 90` integer check |
| `custom-range`   | `{ startDate, endDate, refreshWindow }`     | shared `validateRange()` helper        |

**Shared helper surface (sketch, not signature):**

```
validateRange({ from, to, today, maxSpanDays })
  → { ok: true }  | { ok: false, errorCode, message }

defaultCustomRange(today, daysBack)
  → { from: 'YYYY-MM-DD', to: 'YYYY-MM-DD' }

formatSpan(from, to)
  → '31 days · 2026-03-23 → 2026-04-22'
```

**Data flow on export (both modes):**

```
renderer getOpts()
  → { email, password, refreshWindow, outputDir,
      ...(mode === 'custom' ? { startDate, endDate } : { daysBack }) }
  → IPC download-health
  → main validates (widened): if startDate+endDate, validateRange(); else daysBack range check
  → runHealthExport(opts)
  → exporter: if opts.startDate+endDate → use directly; else derive from daysBack (existing path)
  → per-date fetch + cache + refresh-window logic unchanged
```

## Implementation Units

- [ ] **Unit 1: Pure date-range helpers + tests**

**Goal:** Extract range defaulting, validation, and span formatting into a pure, dual-exported module. This is the shared validation surface for renderer and main.

**Requirements:** R3, R4, R6, R8

**Dependencies:** None — this unit unblocks everything else.

**Files:**
- Create: `electron-app/renderer/date-range.js`
- Create: `electron-app/renderer/__tests__/date-range.test.js`

**Approach:**
- Export (dual CommonJS + browser global) three functions: `validateRange`, `defaultCustomRange`, `formatSpan`.
- `validateRange` returns `{ ok: false, errorCode, message }` on failure using the project's existing structured-error style. Errors: `BAD_RANGE_ORDER` (from > to), `BAD_RANGE_FUTURE` (to > today), `BAD_RANGE_SPAN` (exceeds `maxSpanDays`, default 90).
- All inputs are `YYYY-MM-DD` strings; internal comparison via `new Date(str + 'T00:00:00')` to avoid TZ drift (same pattern `cache.js:68` uses).
- Footer: `if (typeof module !== 'undefined' && module.exports) module.exports = { ... }; else window.DateRange = { ... }`.

**Patterns to follow:**
- `electron-app/util/redact.js` — pure CommonJS module, node-testable, no side effects.
- `electron-app/util/__tests__/redact.test.js` — `node:test` + `node:assert/strict` style.

**Test scenarios:**
- Happy path — valid 7-day range returns `{ ok: true }`.
- Happy path — same-day range (`from === to`) returns `{ ok: true }`, span = 1.
- Edge case — 90-day max span exactly returns `{ ok: true }`.
- Edge case — leap-year crossing (`2024-02-28 → 2024-03-01`) counts 3 days.
- Error path — `from > to` returns `errorCode: 'BAD_RANGE_ORDER'`.
- Error path — `to` one day in the future returns `errorCode: 'BAD_RANGE_FUTURE'`.
- Error path — 91-day span returns `errorCode: 'BAD_RANGE_SPAN'`.
- Edge case — `defaultCustomRange(today='2026-04-22', daysBack=7)` → `{ from: '2026-04-16', to: '2026-04-22' }`.
- Happy path — `formatSpan('2026-03-23', '2026-04-22')` returns a string containing `31 days`, both dates, and a `→` separator.

**Verification:**
- `node --test electron-app/renderer/__tests__/date-range.test.js` reports all scenarios passing.
- CI pipeline's test stage still green.

---

- [ ] **Unit 2: Exporter accepts explicit `startDate` / `endDate`**

**Goal:** Let `runHealthExport` skip the `daysBack → range` derivation when the caller supplies an explicit range.

**Requirements:** R9, R10

**Dependencies:** Unit 1 (not strictly required — this unit can land first — but the validation helper is reused here for defense-in-depth).

**Files:**
- Modify: `electron-app/garmin/exporter.js`
- Modify: `electron-app/garmin/__tests__/exporter.test.js`

**Approach:**
- In `runHealthExport`, branch at the existing lines 185–189: if `opts.startDate && opts.endDate`, use them directly; otherwise keep the current `daysBack`-based derivation.
- Compute `daysBack` locally for the summary metadata block (`date_range.days` at line 358 and the log message at line 191) as `(endDate - startDate) + 1` when the explicit path is taken, so the summary stays truthful regardless of path.
- The refresh-window logic at line 207 already operates on each date individually — no change.
- Inclusive-range semantics stay the same: both endpoints are fetched.

**Patterns to follow:**
- Existing structure of `runHealthExport` — keep the derivation branch minimal and colocated with the current lines to avoid rearranging the file.
- `generateDateRange` already handles inclusive start/end — reuse it.

**Test scenarios:**
- Happy path — passing `{ startDate: '2026-03-01', endDate: '2026-03-07' }` (no `daysBack`) produces a 7-day export; summary reports `date_range.days === 7`.
- Happy path — passing both `daysBack: 90` and `startDate: '2026-04-16', endDate: '2026-04-22'` uses the explicit range (7 days), not 90.
- Integration — refresh-window still re-fetches cached recent days that fall inside the explicit range.
- Error path — `startDate > endDate` is rejected with a structured error before any Garmin call is made (reuse `validateRange` from Unit 1).
- Edge case — single-day range (`startDate === endDate`) fetches exactly one date.

**Verification:**
- All exporter tests pass (`node --test electron-app/garmin/__tests__/exporter.test.js`).
- Manual: run the export with an explicit range and confirm logs say `Pulling 7 days: 2026-03-01 -> 2026-03-07`.

---

- [ ] **Unit 3: Widen `download-health` IPC handler to accept explicit range**

**Goal:** Make the IPC contract accept either payload shape and route validation through the shared helper.

**Requirements:** R6, R8, R9

**Dependencies:** Unit 1 (for validation helper), Unit 2 (for exporter to consume the new fields).

**Files:**
- Modify: `electron-app/main.js` (handler at lines 348–397)

**Approach:**
- Destructure `startDate` and `endDate` from the incoming opts alongside the existing fields.
- When both explicit fields are present: call the shared `validateRange` from Unit 1; on failure, return `{ ok: false, error, errorCode }`.
- When they're absent: keep the current `daysBack` integer validation exactly as-is (1–90).
- Log which path was taken at `info` level so diagnostic bundles capture the intent.
- Forward `startDate` / `endDate` (when present) into the `runHealthExport` opts object at line 392–394.

**Patterns to follow:**
- Existing validation return shape: `{ ok: false, error: '<message>' }`. Add `errorCode` alongside `error` to stay consistent with the structured-error work in plan 2026-04-21-004.

**Test scenarios:**
- `Test expectation: none` — `main.js` IPC handlers are not unit-tested in this project. Validation logic is covered in Unit 1; integration is covered by manual end-to-end verification in Unit 4.

**Verification:**
- Manual: trigger an export with both payload shapes; confirm logs record the chosen path and that an invalid range returns the expected structured error in the renderer.

---

- [ ] **Unit 4: Renderer UI — segmented mode toggle + custom range inputs**

**Goal:** Add the mode switch, custom-range inputs, live span readout, and inline validation; emit the right payload shape from `getOpts()`.

**Requirements:** R1, R2, R3, R4, R5, R7

**Dependencies:** Units 1 and 3.

**Files:**
- Modify: `electron-app/renderer/index.html` (section 02 structure; add `<script src="date-range.js" defer>` before `renderer.js`)
- Modify: `electron-app/renderer/renderer.css` (segmented toggle, date inputs, span readout, error line)
- Modify: `electron-app/renderer/renderer.js` (mode state, payload switch in `getOpts`, validation wiring, localStorage persistence)

**Approach:**
- Section 02 gets a `data-mode="last-n"` attribute at the container level. CSS uses `[data-mode="last-n"] .range-custom { display: none }` and the inverse to switch between panels. No JS toggling of individual element visibility — one attribute, CSS does the rest.
- Segmented toggle: two buttons with `role="radio"` semantics inside a `role="radiogroup"`. Active state uses the existing red-accent border/glow vocabulary (mirrors `.status-pill.connected` shape). Press animates with the same `.15s` transition timing as other controls.
- Date inputs use `<input type="date" max="{today}">`. `max` gets set at init and refreshed at midnight (or on focus — simpler: on focus). No min, to stay out of the user's way; validation catches out-of-range values.
- Span readout sits below the inputs, styled like `.field-help`, updating on every `input` event using `DateRange.formatSpan(from, to)`. When invalid, it's replaced by a red-text error line (one of the `validateRange` messages) and the Export button goes `disabled`.
- `getOpts()` reads `section.dataset.mode`; for `custom-range` returns `{ startDate, endDate, refreshWindow, ... }`, otherwise the existing shape. The existing slider-disable logic during `isRunning` stays as-is; `lockableInputs` extends to include the two date inputs.
- Persist `section.dataset.mode` to `localStorage.garmin_date_mode` on toggle; restore at init. Dates are always re-defaulted from today on load.
- First switch to custom mode populates inputs via `DateRange.defaultCustomRange(today, daysSlider.value)` so the user sees their current window, not blanks.

**Execution note:** All behavior logic lives in `date-range.js` (tested in Unit 1). This unit is DOM wiring; verify manually.

**Patterns to follow:**
- Existing section shell in `index.html` (section 02 slider block) for layout.
- `.status-pill` and `.dir-picker` for dark-themed input treatment and focus-ring tokens.
- Existing `lockableInputs` pattern in `renderer.js:118` for input-disable-during-run.
- `@media (prefers-reduced-motion: reduce)` block — no new motion needed beyond matching the existing `.15s` transitions.

**Test scenarios:**
- Happy path (manual) — toggling to custom mode shows two date inputs pre-filled with the previous slider window; span readout reads correctly.
- Happy path (manual) — submitting an export in custom mode triggers the new IPC path; logs show `Pulling N days: YYYY-MM-DD -> YYYY-MM-DD`.
- Edge case (manual) — setting From after To disables Export and shows an inline error; swapping back re-enables.
- Edge case (manual) — setting a 100-day span disables Export with the span error.
- Edge case (manual) — setting To to tomorrow disables Export with the future-date error.
- Integration (manual) — refresh-window select still applies in custom mode (overlap with recent days re-fetches correctly).
- Persistence (manual) — switching mode, closing the app, reopening: mode is restored; dates are re-defaulted (not stale).
- Reduced motion (manual) — no new animations violate the existing `prefers-reduced-motion` respect.

**Verification:**
- `uv run tools/ci_pipeline.py` green (clean + install + test + build + open).
- Manual walkthrough of all scenarios above against the launched `.app`.

## System-Wide Impact

- **Interaction graph:** The `download-health` IPC handler accepts a widened opts object; all other IPC channels unchanged. Preload bridge is a pure pass-through so no surface change there.
- **Error propagation:** Validation failures use the existing `{ ok: false, error, errorCode }` envelope — renderer's `runHealthDownload` already handles this; error text appears in the result banner + log.
- **State lifecycle risks:** Mode toggle has one persisted bit (`garmin_date_mode`). No partial-write or duplicate-run risk — the export itself is unchanged.
- **API surface parity:** The widened IPC contract is additive and backward-compatible: legacy `{ daysBack }` clients keep working.
- **Integration coverage:** The renderer ↔ main ↔ exporter path is only exercised end-to-end via manual verification in Unit 4. Pure validation logic (Unit 1) is unit-tested; exporter range handling (Unit 2) is unit-tested.
- **Unchanged invariants:** The 90-day ceiling, the refresh-window semantics, the per-date cache behavior, the CSV output shape, and the `date_range.days` summary field all stay identical across both modes.

## Risks & Dependencies

| Risk | Mitigation |
|------|------------|
| Native `<input type="date">` appearance on dark theme is ugly on some macOS versions (gray popover, hard-to-read glyph). | Apply minimal CSS (`color-scheme: dark`, adjusted `::-webkit-calendar-picker-indicator` filter) and verify on the build machine. If irredeemable, fall back to two plain `type="text"` inputs with pattern validation. |
| Timezone drift — constructing `Date` from `YYYY-MM-DD` without a TZ suffix is interpreted as UTC, which shifts by up to a day in local render. | Use the existing `new Date(str + 'T00:00:00')` pattern (already used in `cache.js:68` and `exporter.js:53`) to force local midnight interpretation. |
| Users assume custom dates persist and get confused when they reset. | Span readout always shows the actual chosen dates; the value reset is obvious on next launch. If confusion reports come in, reconsider persisting dates with a "stale since" hint. |
| Widened IPC contract accepts ambiguous payloads (`daysBack` + partial `startDate`). | Main-side validation: only route to the explicit path when **both** `startDate` and `endDate` are present; otherwise fall back to `daysBack` with the existing validation. |

## Documentation / Operational Notes

- No external docs to update — the app has no user-facing manual beyond the in-UI copy.
- `CLAUDE.md` and `AGENTS.md` need no changes; no new workflow or tool.
- Release notes for the next version should call out the custom-range mode as the headline user-facing change.

## Sources & References

- Feature framing origin: brainstorm conversation on 2026-04-22 (no persisted requirements doc — planning bootstrap used instead).
- Related plan: [docs/plans/2026-04-21-004-feat-error-debuggability-and-bug-report-plan.md](../plans/2026-04-21-004-feat-error-debuggability-and-bug-report-plan.md) — structured-error envelope pattern.
- Related plan: [docs/plans/2026-04-16-003-refactor-ui-impeccable-refinement-plan.md](../plans/2026-04-16-003-refactor-ui-impeccable-refinement-plan.md) — instrumentation aesthetic guardrails.
- Design context: [.impeccable.md](../../.impeccable.md) — non-negotiables (90-day ceiling implied by the existing slider max; no new dependencies; reduced-motion respect).
- Touch points: `electron-app/renderer/index.html`, `electron-app/renderer/renderer.js`, `electron-app/renderer/renderer.css`, `electron-app/garmin/exporter.js`, `electron-app/main.js`, `electron-app/preload.js` (verified pass-through, no change).
