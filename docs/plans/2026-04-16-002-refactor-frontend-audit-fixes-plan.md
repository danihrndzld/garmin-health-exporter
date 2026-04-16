---
title: "refactor: Address frontend audit findings in Electron renderer"
type: refactor
status: completed
date: 2026-04-16
---

# Address frontend audit findings in Electron renderer

## Overview

The renderer (`electron-app/renderer/index.html`) commits to a distinctive "medical telemetry" aesthetic (red/near-black, Barlow Condensed display, JetBrains Mono values, Oxanium UI, ECG motifs, scanline). It is largely free of AI-slop fingerprints, but a systematic audit surfaced a cluster of accessibility, quality, and maintainability issues worth addressing without diluting the aesthetic.

This plan captures the audit and sequences the fixes into small, atomic units.

## Problem Frame

The renderer is a single 1100-line HTML file mixing inline CSS, inline JS, and markup. Audit findings fall into five buckets:

1. **Contrast and readability** — several text colors fall near or below WCAG AA on the near-black background.
2. **Aesthetic polish** — pure-black background, emoji-as-icons, glow stacking drift toward AI-slop tells.
3. **A11y correctness** — orphaned `aria-describedby`, keyboard affordances missing on the credentials form, destructive "Clear cache" has no confirmation.
4. **Maintainability** — the inline `<style>` block is now too large to iterate on; theme tokens are bypassed by ad-hoc `rgba()` literals.
5. **Resilience** — update-check button has a click-race, welcome-state teardown is fragile (`appendChild` of a removed node), banner re-activation clears `role=alert`.

No functional regression is planned. Visuals should remain recognizable; changes are surgical.

## Requirements Trace

- R1. Raise all primary/secondary text to WCAG AA on its background.
- R2. Remove AI-slop tells (pure-black bg, emoji icons, unmotivated glows) while preserving the industrial/medical aesthetic.
- R3. Fix orphaned ARIA references and missing keyboard affordances.
- R4. Confirm destructive actions (cache clear).
- R5. Extract renderer CSS/JS to external files so future iterations are reviewable as diffs.
- R6. Harden the update-check and welcome-state flows against obvious races.

## Scope Boundaries

- **Non-goal**: Redesign. The red/black telemetry aesthetic stays.
- **Non-goal**: Light-mode theme. Deferred — desktop telemetry app, dark-only is intentional.
- **Non-goal**: Responsive mobile layout. Electron desktop, min-size enforced by window.
- **Non-goal**: Replacing the terminal log with a virtualized list. Current log volume does not warrant it.
- **Non-goal**: Framework migration (React/Vue/etc).

## Context & Research

### Relevant Code and Patterns

- `electron-app/renderer/index.html` — the entire renderer, styles, and script.
- `electron-app/preload.js` — IPC surface (`window.garmin.*`); do not touch.
- `electron-app/assets/fonts/` — bundled WOFF2; keep `@font-face` declarations.
- `electron-app/main.js` — loads `renderer/index.html`; reference point for any `webPreferences` constraint.

### Institutional Learnings

- Prior refactor (`docs/plans/2026-04-16-001-refactor-node-backend-rewrite-plan.md`) moved backend to Node/SQLite — renderer IPC contract is stable; safe to split renderer files.

### External References

- WCAG 2.2 AA contrast minimums (4.5:1 body, 3:1 large).
- MDN: `color-contrast()` and `oklch()` for perceptually uniform token shifts.

## Key Technical Decisions

- **Split renderer into three files** (`index.html`, `renderer.css`, `renderer.js`) rather than a bundler. No build step added; Electron loads them directly.
  - *Rationale*: keeps zero-build simplicity, makes diffs reviewable.
- **Tint the base background** from `#080808` to a slightly warm near-black (e.g. `oklch(0.14 0.005 20)` → `~#0a0908`).
  - *Rationale*: removes the "pure black" AI tell without visible change.
- **Replace emoji icons** (`👁`/`🙈`, `✓`/`✗`) with inline SVG glyphs matching the existing ECG-style stroke weight.
  - *Rationale*: emoji render inconsistently across macOS versions and diverge from the chosen aesthetic.
- **Raise `--text-dim`** to meet 4.5:1 on the main bg; promote the old value to `--text-faint` for decorative use only.
- **Confirm destructive "Clear cache"** via an inline two-step confirm (first click arms; second click within 3s executes), not a modal. Keeps single-page flow.
- **Guard the update-check button** with a request-in-flight flag; ignore clicks while checking.
- **Welcome-state**: keep a persistent `#welcome` DOM node; toggle visibility via CSS class instead of detach/reattach.

## Open Questions

### Resolved During Planning

- "Should we move to a framework?" → No. Single-view app, IPC already clean.
- "Should we add light mode?" → Deferred. Out of scope.

### Deferred to Implementation

- Exact OKLCH values for retuned tokens — settle while eyeballing the retuned palette against real log output.
- SVG icon stroke specifics for eye/check/cross — match against the existing ECG paths during implementation.

## Implementation Units

- [ ] **Unit 1: Extract renderer CSS and JS into external files**

**Goal:** Split `index.html` into `index.html` + `renderer.css` + `renderer.js` with no behavior change.

**Requirements:** R5

**Dependencies:** none

**Files:**
- Modify: `electron-app/renderer/index.html`
- Create: `electron-app/renderer/renderer.css`
- Create: `electron-app/renderer/renderer.js`

**Approach:**
- Move the two `<style>` blocks verbatim into `renderer.css`; link via `<link rel="stylesheet">`.
- Move the IIFE verbatim into `renderer.js`; reference via `<script src="renderer.js" defer>`.
- Keep `@font-face` in CSS; keep Google Fonts fallback `<link>` in HTML.
- Verify `main.js` still resolves `renderer/index.html` (no path change).

**Verification:**
- App launches, credentials persist, log renders, a health download end-to-end succeeds unchanged.

**Test expectation:** none — pure refactor, covered by manual smoke.

- [ ] **Unit 2: Fix contrast and token hygiene**

**Goal:** Raise text contrast to WCAG AA and consolidate ad-hoc `rgba()` literals into tokens.

**Requirements:** R1

**Dependencies:** Unit 1

**Files:**
- Modify: `electron-app/renderer/renderer.css`

**Approach:**
- Retune `--bg` to a tinted near-black (not pure black).
- Split `--text-dim` into `--text-dim` (AA on bg) and `--text-faint` (decorative only; never body copy).
- Replace `#welcome { opacity: .5 }` with a token-driven color; drop opacity.
- Convert inline `rgba(0,212,160,.08)` / `rgba(232,0,28,.08)` / etc. to semantic tokens (`--teal-bg-soft`, `--red-bg-soft`).
- Verify log entry classes (`.log-dim`, `.log-ts`) meet 4.5:1 against the panel background.

**Test scenarios:**
- Edge case: contrast checker (manual, Chrome DevTools) reports ≥ 4.5:1 for body text, ≥ 3:1 for 14px+ semibold headings.
- Happy path: welcome state "Ready" / "Enter credentials…" remains readable without `opacity` hack.
- Integration: teal success banner text on teal-soft bg meets 4.5:1.

**Verification:**
- All audited text passes AA in Chrome DevTools "Contrast" inspector.

- [ ] **Unit 3: Replace emoji icons with inline SVG**

**Goal:** Remove `👁` / `🙈` / `✓` / `✗` emojis; replace with inline SVG glyphs matching the ECG/telemetry stroke aesthetic.

**Requirements:** R2

**Dependencies:** Unit 1

**Files:**
- Modify: `electron-app/renderer/index.html`
- Modify: `electron-app/renderer/renderer.css`
- Modify: `electron-app/renderer/renderer.js`

**Approach:**
- Eye / eye-off: two inline SVGs, toggle `hidden` attribute instead of swapping `textContent`.
- Banner check/cross: inline SVG with `currentColor` fill; drop leading char in text.
- Keep `aria-label` on the eye button; keep `role=alert` on the banner.

**Test scenarios:**
- Happy path: clicking eye toggles password visibility and swaps icon; aria-label updates.
- Edge case: after a failed download, banner shows cross SVG in red; after success, check SVG in teal.
- A11y: screen reader announces "Show password" / "Hide password" correctly after toggle.

**Verification:**
- No emoji characters remain in the renderer source tree (grep check).

- [ ] **Unit 4: ARIA + keyboard correctness pass**

**Goal:** Fix orphaned ARIA and missing keyboard affordances.

**Requirements:** R3

**Dependencies:** Unit 1

**Files:**
- Modify: `electron-app/renderer/index.html`
- Modify: `electron-app/renderer/renderer.js`

**Approach:**
- Remove `aria-describedby="password-hint"` (referent doesn't exist) or add a visually-hidden hint.
- Wrap credentials fields in a `<form>` so Enter submits (binding to the primary Download button).
- Ensure the banner `tabindex`/`role=alert` contract isn't clobbered when `className` is reset.
- Confirm the progress bar `aria-valuenow` resets on completion (already done — verify).

**Test scenarios:**
- Happy path: focus email, tab to password, press Enter → download starts.
- Edge case: banner activated, focused, then cleared on next run — focus handling does not throw.
- A11y: `axe` devtools shows zero critical violations on the main view.

**Verification:**
- `axe` (Chrome extension) reports 0 critical, 0 serious issues.

- [ ] **Unit 5: Two-step confirm for destructive cache clear**

**Goal:** Protect against accidental cache wipes.

**Requirements:** R4

**Dependencies:** Unit 1

**Files:**
- Modify: `electron-app/renderer/renderer.js`
- Modify: `electron-app/renderer/renderer.css`

**Approach:**
- First click: button flips to "Confirm clear?" style (orange token); 3s timer arms the action.
- Second click within the window: executes `window.garmin.clearCache()`.
- Timer expires or click outside: revert to idle label.
- No modal.

**Test scenarios:**
- Happy path: click → "Confirm clear?" → click again → cache cleared, log entry.
- Edge case: click once, wait > 3s, click again → arms again (does not execute stale intent).
- Edge case: click once, click elsewhere in the app → reverts to idle.

**Verification:**
- Single click never calls `clearCache()`; two-step is the only path.

- [ ] **Unit 6: Harden update-check and welcome-state flows**

**Goal:** Eliminate the rapid-click race in the update button and the detach/reattach fragility of the welcome node.

**Requirements:** R6

**Dependencies:** Unit 1

**Files:**
- Modify: `electron-app/renderer/renderer.js`
- Modify: `electron-app/renderer/renderer.css`

**Approach:**
- Update button: single `inFlight` boolean; ignore clicks while true; collapse the `setTimeout` reset into a `setUpdateState(state)` helper to avoid `innerHTML = ...` round-trips that currently recreate `#app-version` each time.
- Welcome node: keep in the DOM permanently; toggle a `.hidden` class when the first log entry arrives. The Clear button resets the class instead of calling `appendChild`.

**Test scenarios:**
- Happy path: rapid-click update button 5x → exactly one check runs.
- Edge case: run download, then click Clear → welcome reappears without reordering other children.
- Edge case: update check fails mid-flight → button state returns to idle and re-click works.

**Verification:**
- Manual: five rapid clicks on update button → single network/IPC call in the log.
- Manual: Clear after a download restores welcome in the correct position every time.

## System-Wide Impact

- **Interaction graph:** Only the renderer. `preload.js` IPC surface untouched. `main.js` untouched beyond confirming the resource load still works.
- **Error propagation:** No change — `res.ok`/`res.error` paths preserved.
- **State lifecycle risks:** Update-check button is the only stateful DOM mutation being refactored; test rapid-click explicitly.
- **API surface parity:** None — no IPC change.
- **Unchanged invariants:** IPC contract (`window.garmin.*`), persisted `localStorage` keys (`garmin_email`, `garmin_dir`, `garmin_refresh_window`), font bundling.

## Risks & Dependencies

| Risk | Mitigation |
|------|------------|
| CSS extraction breaks `file://` relative paths for fonts | Keep fonts path (`fonts/…`) relative to the CSS file; verify on first launch. |
| Token retune changes perceived brand red | Constrain changes to neutrals and decorative-only tokens; do not touch `--red` / `--red-bright`. |
| Removing emoji icons subtly changes button widths and shifts layout | Match SVG bounding box to prior emoji glyph box; eyeball against screenshots. |
| Two-step confirm feels like a bug without a visible affordance | Use orange token + label change so state is unambiguous. |

## Documentation / Operational Notes

- Update `electron-app/BUILD.md` only if font loading path needs a note after CSS split (unlikely — relative paths preserved).
- No migration, no rollout flag; ship in one release.

## Sources & References

- Audit transcript: this document's Overview section.
- Renderer source: `electron-app/renderer/index.html`.
- Prior refactor: `docs/plans/2026-04-16-001-refactor-node-backend-rewrite-plan.md`.
- `frontend-design` skill guidance for AI-slop tells and contrast rules.
