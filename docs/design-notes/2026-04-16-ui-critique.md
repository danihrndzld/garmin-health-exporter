---
title: "UI Critique — post-audit baseline"
date: 2026-04-16
status: reference
premise: .impeccable.md
scope: electron-app/renderer/
---

# UI Critique — post-audit baseline

Read-only assessment of the renderer after audit-fixes landed
(`docs/plans/2026-04-16-002-refactor-frontend-audit-fixes-plan.md`). Findings
are a punch list for Units 3–8 of plan 003. No code is changed here.

Severity: **H** high · **M** medium · **L** low.
Target-skill: which upcoming unit owns the fix.

---

## Color

| # | Severity | Finding | Target-skill |
|---|---|---|---|
| C1 | H | Only three accent hues (red, teal, orange) sit on one near-black; they behave as three independent signals rather than a coordinated tier. Nothing tells the eye "red + red-dim are the same lineage." | colorize |
| C2 | M | `--text`, `--text-mid`, `--text-dim`, `--text-faint` are too close in lightness once rendered on `--bg`; the faint tier does its job but the mid tier barely differentiates from the base. | colorize |
| C3 | M | Red appears in five roles (primary action, header accent, active dot, error banner, focus ring echo). Without tier distinction the same red shouts at you from five places. | colorize |
| C4 | L | No idle/disabled neutral distinct from `--text-dim`; disabled buttons rely on opacity only, which reads as "loading" more than "inert." | colorize |
| C5 | L | Teal is single-weight (connection pill + success banner use the same value). Success deserves a slightly brighter variant so it reads as arrival, not steady-state. | colorize |

## Type

| # | Severity | Finding | Target-skill |
|---|---|---|---|
| T1 | H | Scale is flat: 10px captions, 12–13px body, one 14–15px UI tier, one 22px title. No deliberate section-heading tier between field and title. | typeset |
| T2 | M | Barlow Condensed (display) is used only in the titlebar. The section-heading slab (`01`, `02`, `03`, `04`) is small Oxanium instead; the display face is underused. | typeset |
| T3 | M | Monospace appears in values, logs, and status pills indistinctly — values and logs both render at the same 12px, so the log and the UI don't read as different surfaces. | typeset |
| T4 | L | Letter-spacing is uniform across tiers. Tight display with looser caption labels would sharpen hierarchy without changing sizes. | typeset |
| T5 | L | Multiple direct `font-size: Npx` literals rather than a named scale. Any later tuning means hunting. | typeset |

## Space

| # | Severity | Finding | Target-skill |
|---|---|---|---|
| S1 | H | Every `.section` uses the same `14px 18px` padding. The sidebar reads as a repeated form rather than an ordered instrument panel. | arrange |
| S2 | M | Gap between sidebar sections is uniform; credentials (the entry point) deserve more breathing room than the "Export" button block. | arrange |
| S3 | M | `.dir-row` centers the path between label and control — the eye has no anchor. Left-aligned path with right-aligned control would read cleaner. | arrange |
| S4 | L | Terminal header, progress wrap, banner, and log share no rhythmic gap system — each has its own margin value. | arrange |
| S5 | L | No declared `--sp-*` scale; space values are magic literals. | arrange |

## Motion

| # | Severity | Finding | Target-skill |
|---|---|---|---|
| M1 | H | Nothing communicates *transition* between idle → running → done. Connection pill flips state without easing; progress fill snaps at 0%. The app is silent when it should be alive. | animate |
| M2 | M | Log entries render via `createElement` with no enter animation. A stream of data just appears; motion would reinforce the telemetry read. | animate |
| M3 | M | Success and error banners toggle via `display` — no slide/fade. They blink on. | animate |
| M4 | L | The existing `pulse` + `scanline` + `draw-in ECG` animations are decorative-only and don't intensify during a run. They could carry signal. | animate |

## Detail

| # | Severity | Finding | Target-skill |
|---|---|---|---|
| D1 | M | Progress bar fill is a flat color with no texture or reading. A telemetry aesthetic invites subtle internal structure (scanline, dotted segmentation, tick markers). | delight (secondary) / polish |
| D2 | M | Focus rings are inherited browser defaults on some controls (eye toggle, clear button) and custom-tinted on others. | polish |
| D3 | L | SVG strokes in ECG icons (titlebar, welcome, banner) use different weights: 1.5, 2, 1.75. They should align. | polish |
| D4 | L | Scrollbar is system default; a terminal panel warrants a styled thin scrollbar. | polish |
| D5 | L | Borders alternate between 1px and implicit 1px-rgba in different selectors; the etched look needs consistency. | polish |

## Interaction

| # | Severity | Finding | Target-skill |
|---|---|---|---|
| I1 | M | "Clear cache" two-step confirm is correct but the armed state (orange text) is the only feedback. A countdown or micro-progress would make the window obvious. | animate + delight |
| I2 | L | Eye toggle has no transition on swap; icons clip. | animate |
| I3 | L | Hover states on `.btn-secondary` and `.cache-link` read the same (lift + faint border brighten) — they're different controls and should feel different. | polish |

## UX Writing

| # | Severity | Finding | Target-skill |
|---|---|---|---|
| W1 | L | "System Output" is the terminal title; acceptable but generic. "TELEMETRY" or "RUN LOG" would carry the aesthetic without being cute. | polish (optional, only if it lands) |
| W2 | L | Welcome copy "Ready · Enter credentials and choose an action" is fine but could become part of a boot sequence for Unit 7 delight. | delight |
| W3 | L | Status pill reads "OFFLINE/ONLINE" — functional. Could read "STANDBY / ACTIVE / DONE" to match instrumentation tone. | polish |

---

## Priorities for Units 3–8

1. **Unit 3 (colorize)** — address C1, C2, C3 first. They set the tier language every other unit reuses.
2. **Unit 4 (typeset)** — T1, T2 create the heading tier that then governs section rhythm.
3. **Unit 5 (arrange)** — S1, S2, S3; needs the type tiers from Unit 4 to know where rhythm should widen.
4. **Unit 6 (animate)** — M1, M3, M2 in that order. State first, then content.
5. **Unit 7 (delight)** — exactly one idle moment (W2 / M4 pairing — ECG cadence on welcome) and one success moment. Cap at two.
6. **Unit 8 (polish)** — sweep Detail + Interaction residue (D2, D3, D5, I3) and anything units 3–7 leave behind.

## Scope Reminders

- Don't touch layout (sidebar + terminal is fixed).
- Don't introduce new dependencies.
- WCAG AA must survive Unit 3; `prefers-reduced-motion` must survive Unit 6 and Unit 7.
