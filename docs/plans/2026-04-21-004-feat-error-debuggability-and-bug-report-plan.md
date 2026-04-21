---
title: Error debuggability + in-app bug report
type: feat
status: active
date: 2026-04-21
origin: docs/ideation/2026-04-21-error-debuggability-ideation.md
---

# Error debuggability + in-app bug report

## Overview

Replace the ambiguous `[client] Network error: Unexpected end of JSON input` log line with structured, actionable diagnostics, and add a renderer button that composes a pre-filled bug-report email to `danisnowman@gmail.com` including app/environment versions and recent log context. Bundles ideation survivors #1, #2, and #4 as the first PR; #3 (diagnostic bundle file) is included as the fourth unit because it removes the `mailto:` body-length limitation for free.

## Problem Frame

The screenshot shows three retries of `Network error: Unexpected end of JSON input` with no indication of which endpoint failed, what HTTP status was received, whether the body was empty vs. malformed, or which user/version is affected. The message is produced at `electron-app/garmin/client.js:155` where the catch block only surfaces `err.message`. Failures in `resp.json()` (line 150) hit this same path when Garmin returns an empty 200 body, making the error look like a network failure when it is actually a backend quirk. Users cannot report the problem without a channel for logs — today's app has no bug-report affordance.

## Requirements Trace

- R1. Every error logged by the Garmin client must include: endpoint name, redacted URL, HTTP status (when known), content-type (when known), body preview (≤200 chars, when available), attempt number, elapsed ms, and a single `errorCode` symbol (ideation #1, #4).
- R2. Empty-body responses and JSON parse failures must be classified distinctly (`EMPTY_BODY` vs `BAD_JSON`) and carry a human-readable explanation (ideation #4).
- R3. The renderer must expose a "Send bug report" button that opens the user's mail client (`mailto:danisnowman@gmail.com`) pre-filled with subject and a body containing app version, Electron/Node/OS versions, last error summary, and the tail of the log (ideation #2).
- R4. When log contents exceed what `mailto:` can carry, the app must persist a diagnostic bundle file to disk and reference its path in the email body so the user can attach it (ideation #3).
- R5. No secrets (Bearer tokens, passwords, email) may appear in logs or diagnostic bundles.

## Scope Boundaries

- Non-goal: remote telemetry (Sentry, custom backend) — explicitly rejected (R1 of rejection summary).
- Non-goal: per-log-entry "copy diagnostics" icon (ideation #5) — deferred.
- Non-goal: rolling persistent `app.log` with rotation (ideation #6) — deferred; this plan writes a diagnostic bundle on demand only.
- Non-goal: changing retry behavior, backoff, or 429/auth handling.
- Non-goal: redesigning the log UI beyond adding one button and possibly a subtle secondary action.

## Context & Research

### Relevant Code and Patterns

- `electron-app/garmin/client.js` — `_request` method (lines ~109–167). Every ideation-#1 and #4 change lands here plus its error return shape.
- `electron-app/garmin/client.js:150` — `resp.json()` failure site that produces the screenshot's message.
- `electron-app/garmin/__tests__/client.test.js` — existing test style using injected `fetch` and `log` mocks. Tests assert on `result.error` substrings (see line 330). New tests must follow the same pattern.
- `electron-app/main.js` — `ipcMain.handle('download-health', ...)` at line 106 is the path that forwards logs to the renderer via `event.sender.send('log', ...)`. Bug-report and diagnostic-bundle IPC handlers land here next to `open-url` (line 68) and `get-version` (line 79).
- `electron-app/preload.js` — contextBridge whitelist. New methods `sendBugReport` and optionally `saveDiagnosticBundle` must be added here.
- `electron-app/renderer/renderer.js` — `appendLog(type, msg, ts)` at line 65 is the log rendering entry point. A new button lives near the existing "Clear" button (line 188 area).
- `electron-app/renderer/index.html` / `renderer.css` — style tokens already established by the recent "impeccable refinement" plan (`docs/plans/2026-04-16-003-...`).
- `electron-app/package.json` — version 3.1.0 read via `app.getVersion()` (already used by `get-version` IPC).

### Institutional Learnings

- `docs/solutions/` is absent in this repo; no prior learnings indexed.
- `.agents/skills/harden/SKILL.md` (surfaced by grep) is a generic authoring skill, not repo-specific guidance for this work.

### External References

- Not needed. The change is repo-internal and uses only Electron APIs already in use (`shell.openExternal`, `app.getVersion`, `app.getPath('userData')`).

## Key Technical Decisions

- **Structured error return shape.** `_request` will return `{ ok: false, error: string, errorCode: string, meta: { endpoint, url, status, contentType, bodyPreview, attempt, elapsedMs, errorClass } }`. Callers that only read `result.error` keep working unchanged; the `meta` object is additive. Rationale: minimizes blast radius; every current call site in `exporter.js` already pattern-matches on `ok`/`error`.
- **Text-then-parse.** Replace `await resp.json()` at `client.js:150` with `await resp.text()` followed by explicit emptiness check and `JSON.parse`. Rationale: lets us distinguish `EMPTY_BODY` from `BAD_JSON` and capture a body preview.
- **Error code taxonomy.** `EMPTY_BODY`, `BAD_JSON`, `NETWORK`, `TIMEOUT`, `HTTP_4XX`, `HTTP_5XX`, `AUTH` (401/403), `RATE_LIMIT` (429 after retries). Rationale: small, memorable set covers every existing branch in `_request`.
- **Log line format.** Human-readable prefix stays as `[client]`; structured `meta` is serialized compactly for the same log line and also included raw on the IPC payload as an optional `meta` field on `log` events. Rationale: preserves existing `appendLog` UX while enabling richer rendering and bundling later.
- **Redaction.** URL scrubbed of any `?token=` or `access_token` query params; Authorization header never logged; email and password never logged. A single `redact(url)` helper centralizes this.
- **Bug-report channel.** `mailto:` via `shell.openExternal` (already imported in `main.js:1`). Subject: `Garmin Data Exporter bug — <errorCode || "general"> — v<version>`. Body: structured text (not HTML), `encodeURIComponent`-encoded.
- **Diagnostic bundle threshold.** If the assembled mail body exceeds 1800 chars, write the full bundle to `app.getPath('userData')/diagnostics/diagnostic-<timestamp>.txt` and include the path in the mail body with a "copy this path to attach the file" line. Rationale: 2000-char `mailto` body limit is conservative cross-platform; leave headroom.
- **Single-click UX.** Button always writes the bundle file and always opens `mailto:`. Predictable; no "file opens sometimes" branching. If writing the bundle fails, still open `mailto:` with whatever fits and log a warn.

## Open Questions

### Resolved During Planning

- Should we persist a rolling log file? **No** — deferred to ideation #6 follow-up. This plan writes a diagnostic bundle on demand only.
- Should the button appear always, or only after an error? **Always** — lower cognitive load; users may want to report usability issues too.
- Where should the button live? **Next to the existing "Clear" log control**, with a secondary (ghost) visual weight per the current design tokens.

### Deferred to Implementation

- Exact visual spec for the button (padding, iconography) — follow existing secondary-button pattern in `renderer.css` and adapt.
- Precise tail size of the log to embed in the email vs. the bundle — tuned during implementation; target ~30 lines in email, full log in bundle.
- Whether to include `os.arch()` or just `os.platform()` + `os.release()` in env block — decide when writing the env helper.

## Implementation Units

- [ ] **Unit 1: Structured error classification in `_request`**

**Goal:** Replace every failure-path return in `GarminClient._request` with a structured `{ ok, error, errorCode, meta }` shape, and classify empty-body / bad-JSON responses distinctly.

**Requirements:** R1, R2, R5

**Dependencies:** None

**Files:**
- Modify: `electron-app/garmin/client.js`
- Test: `electron-app/garmin/__tests__/client.test.js`

**Approach:**
- Add a `classifyError({ err, status, contentType, body, endpoint, url, attempt, startedAt })` helper near the top of `client.js`.
- Switch happy path from `resp.json()` to `resp.text()` then conditional parse. Empty string → `EMPTY_BODY`; `JSON.parse` throw → `BAD_JSON` with body preview.
- Map AbortError → `TIMEOUT`; other catch-branch errors → `NETWORK`.
- Map 401/403 → `AUTH`; 429 exhausted → `RATE_LIMIT`; other non-OK → `HTTP_4XX` or `HTTP_5XX` by status range.
- Every return merges `meta` onto the error object; the legacy `error` string is generated from `errorCode` + short human explanation for backward compatibility.
- Introduce `redactUrl(url)` that strips `token`, `access_token`, and any query keys containing `auth` or `secret` (case-insensitive).
- Change retry logs at lines 130 and 161 to include `endpoint` and redacted `url`.

**Execution note:** Characterization-first — start by asserting today's exact return shape in two or three tests (empty body, timeout, 500), then expand to new error codes. The existing `{ok,error}` consumers must keep working.

**Technical design (directional, not implementation spec):**

```
error = {
  ok: false,
  errorCode: 'EMPTY_BODY' | 'BAD_JSON' | 'NETWORK' | 'TIMEOUT'
            | 'HTTP_4XX' | 'HTTP_5XX' | 'AUTH' | 'RATE_LIMIT',
  error: '<short human sentence derived from errorCode + meta>',
  meta: {
    endpoint, url, status, contentType,
    bodyPreview,      // <=200 chars, newline-collapsed
    attempt,          // 1-based
    elapsedMs,
    errorClass,       // err.name when caught
  }
}
```

**Patterns to follow:**
- Existing branches in `_request` (lines 117, 127, 141, 152) already return `{ok:false,error}`; mirror their placement.
- Existing test style in `client.test.js` using injected `fetch` mocks and a captured `log` spy.

**Test scenarios:**
- Happy path — 200 with valid JSON body returns `{ok:true,data}`; meta is absent on success.
- Happy path — 200 with non-empty JSON keeps current success return shape.
- Edge case — 200 with empty body: returns `errorCode: 'EMPTY_BODY'`, `meta.status === 200`, `meta.endpoint === <name>`, `error` message mentions "empty".
- Edge case — 200 with body `"{not json"`: `errorCode: 'BAD_JSON'`, `meta.bodyPreview` includes the invalid fragment.
- Error path — 500 with text body `"Internal Server Error"`: `errorCode: 'HTTP_5XX'`, `meta.status === 500`, `meta.bodyPreview` includes the text.
- Error path — 404: `errorCode: 'HTTP_4XX'`, `meta.status === 404`.
- Error path — fetch throws `TypeError: fetch failed`: `errorCode: 'NETWORK'`, `meta.errorClass === 'TypeError'`, retry log includes endpoint.
- Error path — AbortError: `errorCode: 'TIMEOUT'`, `meta.elapsedMs` present.
- Error path — 401: `errorCode: 'AUTH'`, tokens cleared (existing behavior preserved).
- Error path — 429 exhausted after `maxRetries`: `errorCode: 'RATE_LIMIT'`, `meta.attempt === maxRetries + 1`.
- Edge case — URL with `?token=abc&date=2026-04-15` is redacted to `?token=REDACTED&date=2026-04-15` in both the logged line and `meta.url`.
- Edge case — Authorization header value never appears in any log call (assert via log spy).

**Verification:**
- All existing tests still pass without modification to their assertions except where a new error code strictly improves specificity.
- New tests cover each `errorCode` branch.
- Manually searching the captured log output for `"Bearer"` or the test token literal returns zero hits.

---

- [ ] **Unit 2: Propagate structured meta to the renderer log channel**

**Goal:** Carry the structured error `meta` from client/exporter to the renderer so the UI has enough context for the bug-report button, without breaking existing `appendLog(type,msg,ts)` consumers.

**Requirements:** R1, R3

**Dependencies:** Unit 1

**Files:**
- Modify: `electron-app/main.js`
- Modify: `electron-app/preload.js`
- Modify: `electron-app/renderer/renderer.js`
- Modify: `electron-app/garmin/exporter.js` (if it currently reformats `result.error` strings, surface `errorCode`/`meta` too)
- Test: `electron-app/garmin/__tests__/exporter.test.js` (assertion that error propagation preserves `errorCode`/`meta`)

**Approach:**
- Extend the `log` IPC payload from `{type,msg,ts}` to `{type,msg,ts,meta?}`. `meta` is optional and silently ignored by older renderer code.
- In `main.js`, when the exporter emits an error event, forward its `errorCode` and `meta`.
- In `renderer.js`, store the *last* error's `{errorCode, meta, msg, ts}` in a module-scoped `lastError` variable whenever a log arrives with `type === 'error'`. Also keep a bounded ring buffer of the last N (≈200) log entries for the bug-report body.
- Do not visually change `appendLog` output in this unit. Rendering tweaks (like a hover tooltip showing meta) are deferred.

**Patterns to follow:**
- Existing `window.garmin.onLog(logHandler)` in `renderer.js:249`.
- Existing IPC surface in `preload.js`.

**Test scenarios:**
- Integration — a forced `EMPTY_BODY` from the exporter surfaces with `errorCode: 'EMPTY_BODY'` and non-empty `meta` in the `log` IPC payload (verified via exporter unit test with an IPC mock).
- Integration — legacy log events without `meta` still render via `appendLog` without errors (DOM smoke test or manual check).
- Edge case — the ring buffer caps at 200 entries and evicts oldest-first.

**Verification:**
- In dev mode, triggering a forced `EMPTY_BODY` in an endpoint shows the error in the log; `window.__lastError` (debug helper, optional) carries the full meta.

---

- [ ] **Unit 3: "Send bug report" button in the renderer (mailto composition)**

**Goal:** Add a button that composes a `mailto:danisnowman@gmail.com` with subject + body pre-filled with app version, environment, last error, and recent log tail.

**Requirements:** R3, R5

**Dependencies:** Unit 2

**Files:**
- Modify: `electron-app/renderer/index.html` (add button adjacent to existing log controls)
- Modify: `electron-app/renderer/renderer.css` (secondary/ghost button style token reuse)
- Modify: `electron-app/renderer/renderer.js` (click handler, body assembly, tail extraction, redaction)
- Modify: `electron-app/main.js` (new `ipcMain.handle('send-bug-report', ...)`)
- Modify: `electron-app/preload.js` (expose `sendBugReport(payload)`)

**Approach:**
- Button label: "Send bug report". Secondary visual weight; does not dominate primary actions.
- On click, renderer assembles a payload: `{ appVersion, platform, osRelease, nodeVersion, electronVersion, chromeVersion, lastError, recentLog }` (last 30 entries formatted `HH:MM:SS [type] msg`). Forbidden fields: email, password, Bearer tokens.
- Renderer posts payload to main via `window.garmin.sendBugReport(payload)`.
- Main constructs subject + body string, `encodeURIComponent`s it, calls `shell.openExternal('mailto:danisnowman@gmail.com?subject=...&body=...')`.
- If `shell.openExternal` rejects, main returns `{ok:false,error}` and renderer logs a warn "Could not open mail client. Diagnostic bundle saved to: <path>" (the bundle is always written — see Unit 4).

**Patterns to follow:**
- Existing `get-version`, `open-url` IPC handlers in `main.js:68,79`.
- Existing `checkForUpdates` button style/placement for the secondary-action look.

**Test scenarios:**
- Happy path — click with a populated log: `shell.openExternal` is called once with a `mailto:` URL containing the app version and the latest error code. (Unit-test at main level with `shell.openExternal` spied via `require` injection or dependency rewire.)
- Edge case — click when no errors have occurred: subject is `Garmin Data Exporter bug — general — v3.1.0`; body notes "No recent errors".
- Error path — `shell.openExternal` rejects: renderer shows a warn log; no uncaught rejection; button remains clickable.
- Edge case — assembled body exceeds 1800 chars: body is truncated and ends with `"…full log in diagnostic bundle: <path>"` (hand-off to Unit 4).
- Edge case — payload contains a string resembling a Bearer token (`Bearer abc123`) in a log line: token is scrubbed before the `mailto:` URL is built. (Assert via a log line spy.)

**Verification:**
- On a dev machine, clicking the button opens the default mail client with the expected subject/body.
- Viewing the URL passed to `shell.openExternal` shows no password, email, or `Bearer` literal.

---

- [ ] **Unit 4: Diagnostic bundle file + path in email body**

**Goal:** Always write a diagnostic bundle to `userData/diagnostics/` and reference its path in the `mailto:` body so users can attach the full log.

**Requirements:** R3, R4, R5

**Dependencies:** Unit 3

**Files:**
- Modify: `electron-app/main.js` (extend `send-bug-report` handler to write the bundle first)
- Create: `electron-app/garmin/diagnostics.js` (helper: `writeBundle({userDataDir, payload}) => {path}`) — or co-locate in `main.js` if it stays under ~30 lines.

**Approach:**
- On `send-bug-report`, main builds the full text bundle (all fields from Unit 3 payload + full log, not truncated), ensures `userData/diagnostics/` exists, writes `diagnostic-YYYY-MM-DD-HHMMSS.txt`.
- `mailto:` body contains a short summary + `"Full diagnostics: <absolute path>"`. On macOS, `shell.showItemInFolder(path)` may be offered via a secondary "Reveal in Finder" toast; acceptable to defer if it adds friction.
- If write fails (e.g., disk full), fall back to mailto-only with a truncated log and log a warn.

**Patterns to follow:**
- Existing `app.getPath('userData')` usage is implied by Electron conventions; follow `app.getPath('documents')` pattern already used in `main.js:50`.
- `fs.promises.mkdir({recursive:true})` + `fs.promises.writeFile`.

**Test scenarios:**
- Happy path — bundle file is written at an absolute path under `userData/diagnostics/`; path matches `diagnostic-\d{4}-\d{2}-\d{2}-\d{6}\.txt`.
- Happy path — file contents include app version, env block, full log (all lines from the renderer's ring buffer), and the last error's `errorCode` + `meta` JSON.
- Error path — `fs.writeFile` rejects with `ENOSPC`: main returns `{ok:true, bundlePath:null, warning}`; `mailto:` still opens with truncated body.
- Edge case — two bug-report clicks within one second produce distinct filenames (timestamp precision to seconds + counter or ms).
- Edge case — the bundle file does not contain the Authorization header, password, or the raw email from login.

**Verification:**
- After clicking the button, `~/Library/Application Support/<app>/diagnostics/` contains the new file.
- Opening the file shows a complete, redacted log.
- The `mailto:` body references the same path.

---

## System-Wide Impact

- **Interaction graph:** `client.js` → `exporter.js` → `main.js` (log IPC) → renderer. Only the log-payload shape and error return shape change; the direction of flow does not.
- **Error propagation:** All current branches in `_request` keep returning `{ok:false,error}` semantics; adding `errorCode`/`meta` is additive. `exporter.js` must be audited to ensure it forwards (not reformats-and-drops) these fields.
- **State lifecycle risks:** Ring buffer in renderer is in-memory only and bounded; no persistence across sessions. Diagnostic bundle writes are one-shot; no cleanup policy in this PR (bundles accumulate under `userData/diagnostics/`).
- **API surface parity:** `preload.js` `window.garmin.*` surface gains `sendBugReport`. No other IPC contracts change shape; `log` gains an *optional* `meta` field that older code ignores.
- **Integration coverage:** Unit 2 needs an exporter-level test (or manual check) that `errorCode`/`meta` survive the IPC hop — unit tests at each layer alone will not prove this.
- **Unchanged invariants:** Retry counts, backoff formula, 401/403 token-clearing behavior, 429 behavior, inter-request throttling, `GarminClient.fetch`/`safe` signatures.

## Risks & Dependencies

| Risk | Mitigation |
|------|------------|
| Callers in `exporter.js` only consume `result.error` strings and a subtle regex breaks if the message format changes. | Preserve the leading phrase pattern for existing codes (e.g., `Rate limited (429) after N attempts`) or update call sites explicitly. Covered by characterization-first tests in Unit 1. |
| `mailto:` body length varies by mail client and OS. | 1800-char conservative cap + always-write diagnostic bundle so the full log is available even when mail client truncates. |
| User has no default mail client configured. | Bundle is still written to disk with a known path; warn log tells the user where to find it. |
| Accidentally logging secrets (Bearer token, password). | Centralized `redactUrl` + explicit test that spies on log/writer calls and asserts absence of the test token literal and password literal. |
| Unbounded diagnostic bundle accumulation under `userData/diagnostics/`. | Accepted for this PR; cleanup policy is a follow-up (linked to ideation #6 rolling log). |
| New ring buffer leaks memory if renderer stays open indefinitely. | Hard cap (200 entries); constant memory. |

## Documentation / Operational Notes

- Update `electron-app/BUILD.md` with a one-line mention of the new "Send bug report" affordance.
- `CHANGELOG.md` entry under next version: "Added: structured error codes and in-app bug report (mailto + diagnostic bundle)."
- No migrations, no rollout gating, no telemetry changes.

## Sources & References

- **Origin document:** [docs/ideation/2026-04-21-error-debuggability-ideation.md](docs/ideation/2026-04-21-error-debuggability-ideation.md)
- Related code: `electron-app/garmin/client.js`, `electron-app/main.js`, `electron-app/preload.js`, `electron-app/renderer/renderer.js`
- Related tests: `electron-app/garmin/__tests__/client.test.js`, `electron-app/garmin/__tests__/exporter.test.js`
- Prior plan: `docs/plans/2026-04-16-003-refactor-ui-impeccable-refinement-plan.md` (design-token conventions for secondary button)
