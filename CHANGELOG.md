# Changelog

## Unreleased — Node backend rewrite (`refactor/node-backend-sqlite-cache`)

### Breaking changes (renderer ↔ main IPC contract)

The preload bridge (`window.garmin.*`) dropped the following APIs as the Python
bootstrap was removed. Custom renderer code relying on them will error:

- `checkDeps()` — removed (no Python to check)
- `installDeps()` — removed
- `onSetupRequired(cb)` — removed (setup overlay deleted)
- `onSetupLog(cb)` — removed

### Added

- `clearCache()` — wipes the sql.js cache DB
- `refreshWindow` option on `downloadHealth({ refreshWindow })` — days within which cached daily data is re-fetched (1–7)
- R10 per-activity-type grouped CSVs: `caminar.csv`, `correr.csv`, `gym.csv` plus matching `*_laps.csv`
- R13 `daily_steps` endpoint is now chunked into 28-day windows internally
- Fetch timeout (30s default, configurable via `timeoutMs` client option) with `AbortController`
- `client.authFailed` flag short-circuits remaining work after 401/403 instead of letting the full export eat auth errors

### Changed

- Output-dir validation now uses `path.relative` containment instead of `startsWith` (prevents `/Users/dani-evil/` matching `/Users/dani`)
- Output-dir path is resolved through `fs.realpathSync` so symlinks inside `$HOME` pointing outside are rejected
- `clear-cache` IPC now refuses to run while an export is in progress
- Cache is now flushed to disk every 10 days and every 10 activities during an export (was end-of-run only), so a crash mid-export no longer loses all prior work
- `shell.openExternal` only accepts `http:`/`https:` schemes
- Token file permissions re-enforced to `0o600` on every overwrite
- Tokens are now cached in memory after first read (was hitting disk on every API call)
- CSV generation failure now returns `{ ok: false }` instead of a fake success

### Removed

- `electron-app/garmin/auth-spike.js` (development scratchpad)
