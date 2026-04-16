---
date: 2026-04-16
topic: alternative-garmin-backends
focus: HTTP 429 account-level rate limiting — explore alternative backends (any language) that produce the same data output
---

# Ideation: Alternative Garmin Connect Backends

## Codebase Context

- **Stack:** Python backend (garminconnect lib) + Electron desktop app (macOS)
- **API surface:** 22 per-day endpoints + 12 aggregated endpoints + per-activity details/splits
- **Problem:** HTTP 429 account-level rate limits. Not IP-based — changing IPs doesn't help. The call volume (22 x N days) exceeds the account ceiling.
- **Pain points:** No retry/backoff logic, no caching (re-fetches everything every run), Python/uv dependency chain causes onboarding friction
- **No past learnings or docs/solutions/ found**

## Ranked Ideas

### 1. SQLite Incremental Cache + Any Backend
**Description:** Persist every fetched `(date, endpoint)` pair in a local SQLite database. On subsequent runs, only fetch days newer than `max(stored_date)` plus a configurable refresh window. A daily user pulling 30 days goes from ~660 API calls to ~44 on re-runs — a 93% reduction.
**Rationale:** Single highest-leverage change. Account-level rate limits are about total volume. Works with any backend language.
**Downsides:** First run still hits full call volume. Some Garmin metrics update retroactively.
**Confidence:** 90%
**Complexity:** Low-Medium
**Status:** Explored (combined with Idea #3)

### 2. FIT Bulk Export + Incremental API Delta (Hybrid)
**Description:** Use Garmin's GDPR data export to download a ZIP of all historical FIT/JSON files. Parse them locally. Use the live API only for the last 1-7 days.
**Rationale:** Eliminates historical backfill API calls entirely. FIT files are Garmin's canonical format.
**Downsides:** GDPR export takes hours, must be manually triggered. Not all metrics have FIT equivalents.
**Confidence:** 75%
**Complexity:** Medium
**Status:** Unexplored

### 3. Node.js/TypeScript In-Process Backend + SQLite Cache
**Description:** Replace Python subprocess with a TypeScript module in Electron's main process. Use `garmin-connect` npm package for auth/API. Use `better-sqlite3` for incremental caching. Eliminates Python/uv entirely.
**Rationale:** Collapses 3-step pipeline into one process, removes uv/Python install friction, puts rate-limit control in same runtime as UI.
**Downsides:** npm package may lag Python library's endpoint coverage. Auth maintenance when Garmin changes SSO.
**Confidence:** 80%
**Complexity:** Medium
**Status:** Explored — requirements doc at `docs/brainstorms/2026-04-16-node-backend-rewrite-requirements.md`

### 4. Bun Single-Executable Backend + Built-in SQLite
**Description:** TypeScript targeting Bun with native SQLite support. Compiles to single executable via `bun compile`.
**Rationale:** Single-binary distribution like Go but in TypeScript. Built-in SQLite, ~10ms startup.
**Downsides:** Bun is newer, less battle-tested. Potential macOS code signing edge cases.
**Confidence:** 65%
**Complexity:** Medium
**Status:** Unexplored

### 5. Go Single-Binary Backend + Persistent Cache
**Description:** Go binary (~8-12MB) with net/http, SQLite, goroutines with semaphore for controlled concurrency. Ships in Electron's extraResources.
**Rationale:** Zero runtime dependencies, fast startup, clean concurrency model. Trivial main.js integration.
**Downsides:** Two languages in codebase. Existing Go Garmin libraries are immature.
**Confidence:** 70%
**Complexity:** Medium-High
**Status:** Unexplored

### 6. Garmin Health API (Official Partner API)
**Description:** Apply for access to Garmin's official Health API. Standard OAuth 2.0, documented rate limits, webhook push support.
**Rationale:** Only path to zero rate-limit anxiety. Stable contract, no ToS risk.
**Downsides:** Approval takes weeks to months. Different data schema. Requires public webhook endpoint for push.
**Confidence:** 55%
**Complexity:** Medium (code) + High (bureaucracy)
**Status:** Unexplored

## Rejection Summary

| # | Idea | Reason Rejected |
|---|------|-----------------|
| 1 | Multi-account sharding | ToS violation risk, fragile |
| 2 | mitmproxy mobile traffic interception | High maintenance, brittle to app updates |
| 3 | Connect IQ BLE companion app | Massive effort, limited metric coverage |
| 4 | USB/MTP FIT file extraction | Requires physical cable; most users sync wirelessly |
| 5 | Deno proxy sidecar | No clear advantage over Node/Bun |
| 6 | ETag/conditional requests | Garmin wellness endpoints unlikely to support properly |
| 7 | Reduce to summary endpoint only | Loses granular data user explicitly exports |
| 8 | Aggressive backoff in Python only | Account ceiling too low for 22xN calls |
| 9 | Playwright as primary backend | Ships Chromium, fragile to UI changes |
| 10 | Rust backend | No existing Garmin library; high effort for personal tool |
| 11 | Token rotation (same account) | Garmin likely rate-limits per account ID |
| 12 | Garmin Connect web scraper | Same endpoints, same rate limits |
| 13 | Browser session cookie injection | Duplicates Playwright idea |
| 14 | Pure Node.js without cache | Language swap alone doesn't reduce call volume |
| 15 | Pure Go without cache | Same — no call reduction |

## Session Log
- 2026-04-16: Initial ideation — 38 raw ideas generated across 5 frames, 22 unique after dedup, 6 survived filtering. Idea #3 selected for brainstorm.
