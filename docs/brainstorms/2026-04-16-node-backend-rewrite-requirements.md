---
date: 2026-04-16
topic: node-backend-rewrite
---

# Node.js/TypeScript In-Process Backend + SQLite Incremental Cache

## Problem Frame

The Garmin Health Exporter's Python backend hits HTTP 429 account-level rate limits when calling 22+ endpoints per day x N days. The rate limit is account-scoped (not IP-based), so the only viable mitigation is reducing total API call volume. Additionally, the Python/uv dependency chain creates onboarding friction (CLT install, Homebrew fallback, quarantine issues).

This rewrite consolidates the entire pipeline — API fetching, caching, and CSV conversion — into a single JS module running in Electron's main process, eliminating all Python dependencies.

## User Flow

```
User clicks "Export" in UI
        │
        ▼
┌─────────────────────────┐
│  Check SQLite cache for  │
│  each (date, endpoint)   │
│  pair in requested range │
└───────────┬─────────────┘
            │
    ┌───────┴───────┐
    │               │
  Cached        Not cached
    │               │
    ▼               ▼
 Skip call    Fetch from Garmin API
                    │
                    ▼
             Store in SQLite
                    │
    ┌───────────────┘
    │
    ▼
┌─────────────────────────┐
│  Merge cached + fresh    │
│  into unified JSON       │
└───────────┬─────────────┘
            │
            ▼
┌─────────────────────────┐
│  Convert JSON → CSVs     │
│  (in-process, no Python) │
└───────────┬─────────────┘
            │
            ▼
      Export complete
```

## Requirements

**API Client**
- R1. Hand-roll a Garmin Connect API client using `fetch`, implementing the OAuth/SSO flow and all endpoints directly. Reference the `garth` Python library and `garminconnect` source for URL patterns and auth flow. No third-party Garmin npm package dependency
- R2. Implement all 22 per-day endpoints, 12 aggregated endpoints, and per-activity fetches (get_activities_by_date, get_activity_splits, get_activity_typed_splits, get_activity_details, get_goals) currently covered by `garmin_health_export.py` and `download_activity_details.py`
- R3. Implement OAuth/SSO authentication with persistent token caching (equivalent to current `~/.garmin_exporter_tokens/` behavior). Tokens should be stored with restrictive file permissions (mode 0600)
- R4. Output the same JSON schema as the current Python backend so downstream CSV conversion remains compatible

**Incremental Cache**
- R5. Persist fetched data in a local SQLite database (via `better-sqlite3` or `sql.js`), keyed by `(date, endpoint_name)` for daily endpoints and `(activity_id, data_type)` for per-activity detail/split fetches
- R6. On each run, skip API calls for `(date, endpoint)` pairs that already exist in the cache and fall outside the refresh window (aggregated endpoints and activity lists are exempt per R8)
- R7. Provide a configurable "refresh window" (1-7 days) as a dropdown on the existing export form (next to "days back"), defaulting to 3 days — data within this window is always re-fetched to capture Garmin's retroactive updates (sleep scores, training status)
- R8. Aggregated endpoints (date-range based) and activity lists are always re-fetched since they span the full requested range

**CSV Conversion**
- R9. Port `json_to_csv.py` logic to JavaScript, producing identical CSV output (same columns, same filenames, same directory structure)
- R10. Port `download_activity_details.py` activity-type grouping and CSV export to JavaScript (caminar/correr/gym groups, same field set)

**Rate Limit Handling**
- R11. On HTTP 429 responses, implement exponential backoff with jitter (initial 5s, max 60s, up to 5 retries per endpoint) before failing. For 401/403 auth errors, fail immediately, clear cached tokens, and prompt re-authentication — do not retry
- R12. Add a configurable inter-request delay (default 500ms) between sequential API calls to reduce burst pressure

**Migration**
- R13. Remove all Python scripts (`garmin_health_export.py`, `json_to_csv.py`, `download_activity_details.py`), their bundled copies in `electron-app/scripts/`, and the corresponding `extraResources` entries in the electron-builder config
- R14. Remove all uv resolution, install, and dependency-check logic from `main.js`
- R15. Remove `setup-required`, `install-deps`, and `check-deps` IPC handlers and corresponding UI (setup screen)

**UX Continuity**
- R16. Preserve existing `PROGRESS:` reporting behavior via IPC (`onProgress` callback) so the UI progress bar works identically
- R17. Preserve existing `log` IPC events (dim/info/warn/success types) for the activity log panel
- R18. Preserve the output directory structure: `garmin_health_YYYY-MM-DD_HH-MM-SS.json` + `csv_YYYY-MM-DD/` folder

## Success Criteria

- A 30-day export on a second run (with default 3-day refresh) achieves ≥80% total call reduction: ~66 per-day calls (3 days x 22 endpoints) + 12 aggregated (always re-fetched) + activity list + cached activity detail calls ≈ 80-110 calls vs ~660+ baseline
- No Python or uv required to run the app
- CSV output is schema-compatible with current Python-generated CSVs (same column names, same column order, equivalent value formatting). Verified by diffing column headers and spot-checking value types
- HTTP 429 errors trigger visible retry behavior in the UI log instead of silent failure

## Scope Boundaries

- **Not porting to Go/Rust/Bun** — staying in the JS/Electron ecosystem for maximum simplicity
- **Not adding Garmin Health API (official)** — that requires partner approval and is a separate initiative
- **Not adding FIT file parsing** — the API remains the data source; FIT is a future enhancement
- **Not changing the UI** — only the settings panel gets a "refresh window" control; all other UI stays identical
- **Not adding background/scheduled sync** — the user still triggers exports manually

## Key Decisions

- **In-process module over subprocess**: Eliminates spawn/IPC serialization overhead, enables direct SQLite access, simplifies error handling. Long-running work should use async patterns to avoid blocking the main thread
- **Hand-rolled API client over npm package**: The `garmin-connect` npm package covers only ~8 of ~36 required endpoints and was last published Jan 2024. Hand-rolling from `garth`/`garminconnect` Python source gives full control and no dependency risk
- **Clean break over Python fallback**: One runtime to maintain, no dual-path complexity, cleaner distribution
- **SQLite over flat-file cache**: Enables efficient key lookups, atomic writes, and future query capabilities without re-parsing JSON files
- **Configurable refresh window on export form**: Balances data freshness with API call reduction; avoids the need for a separate settings panel

## Dependencies / Assumptions

- The Garmin Connect OAuth/SSO flow and API URL patterns are well-documented in `garth` and `garminconnect` Python sources and can be replicated in JS using `fetch`
- `better-sqlite3` or `sql.js` works reliably in Electron's main process (`better-sqlite3` requires `electron-rebuild` for native module ABI compatibility; `sql.js` avoids this as pure WASM)
- Garmin's retroactive data updates (sleep, training) settle within 3 days (the default refresh window) — this is a best estimate; the configurable window (1-7 days) mitigates if it's longer

## Outstanding Questions

### Resolve Before Planning
_(none — all product decisions resolved)_

### Deferred to Planning
- [Affects R1, R2][Needs research] Map all ~36 Garmin Connect API endpoint URLs from the `garth`/`garminconnect` Python source — document the exact proxy paths, query parameters, and auth headers needed for each
- [Affects R5][Technical] SQLite schema design — single table with `(date, endpoint, json_blob)` + `(activity_id, data_type, json_blob)` vs. normalized per-endpoint tables
- [Affects R5][Technical] `better-sqlite3` (native, requires electron-rebuild) vs. `sql.js` (WASM, no native deps) — evaluate performance and build complexity tradeoffs
- [Affects R3][Technical] Token storage location — reuse `~/.garmin_exporter_tokens/` for migration continuity or move to Electron's `app.getPath('userData')` with mode 0600
- [Affects R9][Technical] CSV generation approach — streaming writes vs. in-memory construction for large exports. Generate a reference CSV corpus from current Python pipeline for schema-compatibility verification
- [Affects R1][Technical] Consider Electron utility process (Electron ≥22) or `worker_threads` to run the API client + SQLite off the main thread, preventing UI freezing during long exports
- [Affects R5][Technical] Cache integrity — add a "clear cache" UI option and handle corrupted SQLite gracefully (detect and recreate)

## Next Steps

-> `/ce:plan` for structured implementation planning
