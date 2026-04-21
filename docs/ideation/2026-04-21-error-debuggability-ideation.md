---
date: 2026-04-21
topic: error-debuggability
focus: richer error messages + in-app bug-report button (mailto:danisnowman@gmail.com)
---

# Ideation: Error Debuggability & Bug Reporting

## Codebase Context

- Electron app (`electron-app/`), renderer + main, version 3.1.0. Garmin HTTP client at `electron-app/garmin/client.js`.
- Ambiguous error in screenshot — `[client] Network error: Unexpected end of JSON input on attempt 1...` — originates at `electron-app/garmin/client.js:150` (`resp.json()`). The catch branch at `client.js:152-163` only logs `err.message`, dropping endpoint name, URL, HTTP status, `content-type`, body preview, and params.
- In-app log is rendered but not persisted. No bug-report affordance today.

## Ranked Ideas

### 1. Enrich error payloads with diagnostic context
**Description:** On every failure path in `client.js` (and relevant call sites), capture endpoint name, redacted URL, HTTP status, content-type, response body preview (≤200 chars), attempt number, elapsed ms, and a single `errorCode` (`EMPTY_BODY`, `BAD_JSON`, `NETWORK`, `TIMEOUT`, `HTTP_5XX`, `AUTH`, `RATE_LIMIT`). Log a structured object and a human-readable summary.
**Rationale:** Today's message hides that Garmin returned a 200 with an empty body — a different failure class than a socket drop. Endpoint + status + body preview triages in seconds.
**Downsides:** Slightly noisier logs; must scrub auth headers and tokens carefully.
**Confidence:** 95% · **Complexity:** Low · **Status:** Unexplored

### 2. "Send bug report" button with `mailto:` composition
**Description:** Button in the renderer (near the log controls). On click, gather app version, Electron/Node/OS versions, last N log lines, last error code, and open `mailto:danisnowman@gmail.com?subject=...&body=...` via `shell.openExternal`. Pre-fill subject with error code + timestamp.
**Rationale:** Direct user→maintainer channel with structured context; no backend.
**Downsides:** `mailto:` body length capped (~2000 chars) — truncate and pair with "copy full log"; some users have no default mail client.
**Confidence:** 90% · **Complexity:** Low · **Status:** Unexplored

### 3. Auto-save diagnostic bundle to disk alongside mailto
**Description:** When user clicks Send Bug Report, also write `diagnostic-YYYY-MM-DD-HHMM.txt` to the user's data dir (full log + env + redacted config). Toast with "Reveal in Finder". Email body references the path so user can attach.
**Rationale:** Solves the mailto body-length limit; works for users who prefer Slack/Drive over email.
**Downsides:** Extra FS write; manual attach step.
**Confidence:** 85% · **Complexity:** Low · **Status:** Unexplored

### 4. Classify empty-body responses explicitly before JSON parsing
**Description:** In `_request`, read `resp.text()` once; if trimmed length is 0 return `{ errorCode: 'EMPTY_BODY', endpoint, status, contentType }` with a helpful message. Only `JSON.parse` non-empty text; on parse failure use `BAD_JSON` with body preview.
**Rationale:** Directly eliminates the confusing message from the screenshot, replacing it with an actionable label.
**Downsides:** Adjusts the happy path (text-then-parse); negligible perf cost.
**Confidence:** 92% · **Complexity:** Low · **Status:** Unexplored

### 5. In-app "Copy diagnostics" action on error log lines
**Description:** Error lines/toasts get a small "Copy diagnostics" icon that copies the structured error object (JSON) to clipboard.
**Rationale:** Lower friction than email for many users; pairs with #1.
**Downsides:** Additional UI affordance to keep minimal and consistent.
**Confidence:** 75% · **Complexity:** Low · **Status:** Unexplored

### 6. Persist a rolling `app.log` file
**Description:** Tee `this._log(...)` output and renderer log lines to `userData/logs/app.log` with rotation (last 3 files, 1 MB each). Bug-report button attaches the current log path.
**Rationale:** Today the log dies with the window; crashes and hangs especially need post-mortem data.
**Downsides:** Adds a fs layer; rotation logic must be correct.
**Confidence:** 80% · **Complexity:** Medium · **Status:** Unexplored

## Rejection Summary

| # | Idea | Reason Rejected |
|---|------|-----------------|
| R1 | Sentry / remote telemetry | Out of scope; privacy-heavy; user explicitly wants mailto |
| R2 | Full error-handling architecture redesign | Overkill; focused changes cover 90% of value |
| R3 | Self-healing retry on empty body | Treats the symptom; goal is debuggability |
| R4 | Auto-open GitHub issue via `gh` CLI | Fragile; requires CLI install; user specified email |
| R5 | Screenshot capture with bug report | Scope creep; log + versions sufficient |
| R6 | "Last 50 events" telemetry buffer on disk | Subsumed by #6 |

## Session Log
- 2026-04-21: Initial ideation — 12 candidates generated, 6 survived. User focus: debuggability + mailto bug-report button. Recommended first bundle: #1 + #2 + #4.
