# Go backend, stage 2: Garmin login, client, cache and CSV files

Date: 2026-10-16. Status: approved by the user on 2026-10-15 (design, A1-A7, Task 0).

## Goal and context

Stage 1 (`docs/superpowers/specs/2026-10-14-go-core-mfp-design.md`) added the Go binary `garmin-export-core` next to the Bun sidecar `garmin-core` and moved the MyFitnessPal (MFP) export to it. Stage 2 moves the Garmin side: the login, the HTTP client, the per day cache, the export loop and the CSV writer. Stage 2 is done when the 18 golden CSV files in `electron-app/garmin/__tests__/golden` and the end to end tests pass against the Go binary, and the app uses the Go path by default.

Stage 2 also adds four behaviours that the user approved on 2026-10-15:

1. Saved logins for Garmin, the same model as MFP. The user connects Garmin once. The password goes to the Go binary once, on stdin, and is never stored. While `tokens.json` holds a valid login, an export needs no email and no password. When the login is no longer valid, the Setup row says "Session expired" and offers a reconnect button.
2. A switch per source. Garmin and MFP can each be switched off. A run needs at least one source that is switched on and connected.
3. Strava is not added. Its API agreement (section 5.3) forbids passing its data to an LLM, which is the point of this app.
4. New development goes in Go. The Garmin status, connect and disconnect commands, and the clear cache command, are written in Go, not in the Bun sidecar.

Stage 3 moves the LLM pack, the bug report and the `summary` command to Go and removes the Bun sidecar. Stage 2 keeps them in Bun.

## Scope

In scope:
- Go package `internal/garmin`: tokens file, login (DI OAuth first, the older widget and OAuth1 flow once as fallback), sign in cooldown, HTTP client, endpoint table, per day JSON cache, export loop, JSON export file and CSV writer.
- Go package `internal/jsjson`: an order preserving JSON value and JavaScript compatible number and string output, needed for byte equal CSV files.
- Go commands `garmin-status`, `garmin-connect`, `garmin-disconnect`, `export` and `clear-cache`.
- A Go fake Garmin server for Go tests, ported from `electron-app/garmin/__tests__/helpers/fake-garmin.js`.
- UI: a Garmin connection row like the MFP row, a connect form that replaces the always visible email and password fields, an Include switch per source, and an engine switch that picks Go or Bun for the Garmin export.
- Two small Bun changes that keep Bun usable as the fallback: `export` works from `tokens.json` without a password, and `summary` works from `csvDir` without `jsonPath`.

Out of scope (stage 3 or later):
- LLM pack, bug report and `summary` in Go.
- Removing the Bun sidecar, `electron-app/` and `tools/ci_pipeline.py` / `tools/release.py` from the build.
- Fixing the CSV bugs listed in the tech debt note. Stage 2 copies the current output byte for byte first. Fixes come after acceptance, each with an on purpose golden update.
- macOS Keychain storage of tokens (same reason as stage 1: ad hoc signing).
- Two step verification (MFA). It stays a clear error.
- Strava.

## Architecture

```
UI (app.js)
  ├─ garmin-status / garmin-connect / garmin-disconnect / clear-cache ──► garmin-export-core (Go)
  ├─ export (Garmin, or only the csv folder when Garmin is off) ─────────► garmin-export-core (Go)   [engine "go"]
  │                                                                  └──► garmin-core (Bun)          [engine "bun", fallback]
  ├─ mfp-export ─────────────────────────────────────────────────────────► garmin-export-core (Go)   (unchanged)
  └─ summary / pack / report ────────────────────────────────────────────► garmin-core (Bun)         (stage 3)
```

The UI keeps the stage 1 chain: export, then `mfp-export`, then `summary`. The chain keeps the MFP step separate, so a cancelled or failed MFP step never hides a finished Garmin export (Playwright test DT-R5 in `e2e/mfp.spec.js`).

### Go packages

- `internal/protocol`: unchanged, plus `Emitter.Warn(msg, code string)` that writes a `log` line with `type:"warn"` and `errorCode`. Bun's warn lines carry `errorCode`, and the bug report reads it.
- `internal/guard`: unchanged, plus `DaysBack(n) (start, end string, err error)` (1 to 90, dates in local time ending today) and `RefreshWindow(n) error` (1 to 7). Both port `tauri-app/sidecar/cli.js:runExport`.
- `internal/atomicfile`: `WriteFile(path, data)` moved from `internal/mfp/session.go` (temporary file, mode 0600, rename). MFP and Garmin both use it.
- `internal/jsjson`: decodes JSON into an ordered value tree, and prints numbers and nested values exactly as JavaScript's `String(n)` and `JSON.stringify` do. See "Byte equal CSV files".
- `internal/garmin`:
  - `tokens.go`: the `tokens.json` shapes, `LoadTokens`, `Save`, `MarkExpired`, `Delete`, `Status(now)`.
  - `auth.go`: `Login(ctx, email, password)`, the cooldown, `Refresh(ctx)`, OAuth1 signing, native headers.
  - `endpoints.go`: the endpoint table, ported from `endpoints.js` without the dead entries (`activity_details`, `activity_split_summaries`).
  - `client.go`: `Get(ctx, name, params)` with pacing, 401, 403, 429, 204 and retry rules.
  - `cache.go`: per day JSON files and the final rule.
  - `export.go`: the `export` command.
  - `csv.go`: the 20 CSV extractors and the writer.
  - `garmintest/server.go`: fake Garmin for Go tests.
- `cmd/garmin-export-core/main.go`: adds the five commands to the `switch`.

The module keeps the stage 1 rules: Go standard library only, `CGO_ENABLED=0`, a universal binary built with `lipo`.

### The trust boundary moves to Go

Every Go command that takes a path or a range runs the checks of `internal/guard` before it does anything else:
- `dataDir` and `outputDir` resolve inside `$HOME` after `filepath.EvalSymlinks`.
- `startDate` and `endDate` are `YYYY-MM-DD`, start on or before end, at most 90 days; or `daysBack` is an integer from 1 to 90.
- `refreshWindow` is an integer from 1 to 7.
- `email` and `password` are non empty strings of at most 320 and 1024 bytes. The password is never logged, never written, and never put in an error message.
- `GARMIN_FAKE_SERVER` is used only when it matches `^http://(127\.0\.0\.1|localhost):\d+$`, and it is read on every request, like `MFP_FAKE_SERVER`.

Assumption A1: the Bun `dataDir` check only asked for an absolute path; Go asks for a path inside `$HOME`. The Tauri app data folder (`~/Library/Application Support/<identifier>`) is inside `$HOME`, so the app is not affected. Tests must create their data folders inside `$HOME`, as the stage 1 tests already do.

## Protocol commands

Every command reads one JSON line on stdin and ends with one `{kind:"result", ok, ...}` line. Errors carry `errorCode` and a plain sentence in `error`.

### garmin-status

Request: `{"cmd":"garmin-status","dataDir":"<abs>"}`

Result: `{"ok":true,"connected":true,"expired":false,"savedAt":"2026-10-15T14:02:11Z","loginKind":"di"}`

- No network call, ever. The UI calls it on start and after focus.
- `connected` is true when `tokens.json` exists and parses as a DI login, a legacy login, or an expired marker.
- `expired` is true when the file is an expired marker, or when it is a DI login whose `refresh_token` is missing or whose `refresh_token_expires_at` is in the past.
- A legacy login is never expired by date. Its OAuth1 token lasts about a year and Garmin gives no expiry; only a failed run marks it.
- `savedAt` is the `saved_at` field when present, else `null`. Bun's refresh drops unknown fields, so `savedAt` can go back to `null` while Bun is the engine (assumption A2, harmless: the UI shows it only as a tooltip).
- `loginKind` is `"di"`, `"legacy"` or `null`. The field is not called `kind` because `protocol.Result` sets `kind` to `"result"` on every result line.

### garmin-connect

Request: `{"cmd":"garmin-connect","dataDir":"<abs>","email":"...","password":"..."}`

Result on success: `{"ok":true,"connected":true,"expired":false,"savedAt":"...","loginKind":"di"}`, the same shape as `garmin-status`.

Steps, ported from `auth.js:login` and `_loginChain`:
1. If `sso-cooldown.json` says a cooldown is running, stop with `GARMIN_RATE_LIMITED` and the minutes left. No request is sent.
2. Mobile login (`POST /mobile/api/login`). A wrong password stops with `GARMIN_BAD_PASSWORD`. MFA stops with `GARMIN_MFA`. A 429 does not stop: Garmin limits the two logins separately (measured 2026-10-14).
3. DI exchange at `diauth.garmin.com`, trying the four client ids in order. A 429 stops with `GARMIN_RATE_LIMITED`.
4. Only when step 2 was blocked (403, captcha, not JSON, 429) or step 3 failed without a 429: the widget login once, then the legacy OAuth1 and OAuth2 exchange. A 429 here stops with `GARMIN_RATE_LIMITED`. A 403 or other non 200 stops with `GARMIN_BLOCKED`.
5. A `GARMIN_RATE_LIMITED` result from a login writes `sso-cooldown.json` (`{"until": <unix seconds>}`, 15 minutes, capped at 15 minutes when read).
6. On success, `tokens.json` is written (mode 0600, temporary file and rename) with `saved_at`, and the old file is replaced.

`garmin-connect` makes at most 2 sign in attempts (mobile, then widget). The app never calls it on its own: only the connect and reconnect buttons do.

### garmin-disconnect

Request: `{"cmd":"garmin-disconnect","dataDir":"<abs>"}`. Result: `{"ok":true}`.

Deletes `tokens.json`. It keeps `sso-cooldown.json`, so a disconnect and reconnect cannot skip a running cooldown. It does not touch the cache.

### export

Request:

```json
{"cmd":"export","garmin":true,"startDate":"2026-09-16","endDate":"2026-10-15",
 "refreshWindow":3,"outputDir":"/Users/dani/Documents/GarminExport","dataDir":"<abs>"}
```

`daysBack` may replace `startDate` and `endDate`, as in Bun. `garmin` defaults to `true`.

Result:

```json
{"ok":true,"path":"<outputDir>/csv_2026-10-15","jsonPath":"<outputDir>/garmin_health_2026-10-15_14-03-55.json",
 "startDate":"2026-09-16","endDate":"2026-10-15","days":30,"activities":12,"files":["daily_summary.csv", "..."]}
```

With `garmin:true`, the command:
1. Validates the input, then loads `tokens.json`. A missing file ends with `GARMIN_SESSION` "Connect Garmin first." An expired marker ends with `GARMIN_SESSION` "The Garmin session expired. Reconnect Garmin."
2. Refreshes the access token when it expires within 15 minutes (DI refresh grant, or the OAuth1 to OAuth2 exchange for legacy tokens). A refresh is not a sign in and does not count against the sign in limit.
3. Gets the display name (`social_profile`), then the daily, aggregated, activity list, splits and goals requests, in the same order as `exporter.js:exportHealth`, with the cache rules below.
4. Writes `garmin_health_<today>_<HH-MM-SS>.json` in the same shape as Bun (`export_date`, `date_range`, `daily`, `aggregated`, `activities`, `goals`), because Bun `summary` reads it until stage 3.
5. Writes the CSV files into `csv_<today>`, the same folder name as Bun, and deletes `mfp_food.csv`, `mfp_daily.csv` and `mfp_exercise.csv` left there by an earlier run.

With `garmin:false`, the command only validates the input, creates `csv_<endDate>` inside `outputDir` (after the `$HOME` check), deletes the stale MFP files there, and returns `{"ok":true,"path":...,"jsonPath":null,"startDate":...,"endDate":...}`. It reads no tokens and sends no request.

Assumption A3: the folder name. The user approved "csv_<end date>" for the Garmin off case. The Garmin on case keeps `csv_<today>`, the current name, because Bun, the end to end tests and the user's existing folders use it. The two names are the same in the default "last N days" mode, where the end date is today. They differ only for a custom range that ends before today. If the user wants one rule for both, `csv_<endDate>` for both is a one line change in Go and Bun, and it should land with the engine flip, not before.

Log lines: the UI finds its phases by matching log text (`app.js` `PHASE_START` and `onLog`), so Go writes the same lines as `exporter.js`, in the same order:
- `dim` "Using the saved Garmin login" (new; replaces "Loaded saved tokens" and "Login successful")
- `info` "Pulling N days: START -> END"
- `dim` "  YYYY-MM-DD" after each day, with progress `{phase:"daily"}`
- `info` "Pulling aggregated metrics...", progress `{phase:"aggregated"}`
- `info` "Pulling activities...", then `info` "Found N activities", then `dim` "  [i/n] DATE | NAME" with progress `{phase:"activities"}`
- `success` "JSON saved: PATH", then progress `{phase:"csv",current:1,total:1}` and `success` "CSVs written to DIR"
- `success` "Done. N days | M activities"
- `warn` "[skip] ENDPOINT: ERROR" with `errorCode`, for each skipped request
- `dim` "[no data] ENDPOINT (HTTP 204)"

The JS line "Display name: <name>" is not ported. It writes account data into the log, which the bug report sends out.

### clear-cache

Request: `{"cmd":"clear-cache","dataDir":"<abs>"}`. Result: `{"ok":true}`.

Deletes `garmin_cache.db`, `garmin-cache/` and `mfp-cache/` in `dataDir`. Deleting the sql.js file is not opening it, so the rule "Go never opens `garmin_cache.db`" holds. It replaces the Bun `clear-cache`, which loaded sql.js to empty two tables. The UI already blocks Clear cache while a run is going, so no export writes at the same time.

### mfp-export

Unchanged from stage 1.

## File ownership

| File in `dataDir` | Written by | Read by | Notes |
| --- | --- | --- | --- |
| `tokens.json` | Go `garmin-connect`, Go `export` (refresh, expired marker), Bun `export` (refresh, while Bun is the engine) | Go, Bun | Same JSON shapes as `auth.js`. Mode 0600. Go writes through a temporary file and a rename. |
| `sso-cooldown.json` | Go `garmin-connect` | Go `garmin-connect`, Bun login | `{"until": <unix seconds>}`, unchanged. |
| `garmin-cache/daily/YYYY-MM-DD.json` | Go `export` | Go `export` | New. |
| `garmin-cache/activity/<activityId>.json` | Go `export` | Go `export` | New. |
| `garmin_cache.db` | Bun `export` only | Bun `export` only | Go deletes it in `clear-cache` and never opens it. Removed in stage 3. |
| `mfp_session.json`, `mfp-cache/` | Stage 1 rules | Stage 1 rules | Unchanged. |

Only one process runs an export at a time, because the UI runs one job at a time. Go and Bun never write the same cache file: they use different files.

### tokens.json shapes

Go reads and writes exactly the shapes of `auth.js`, so that the Bun fallback can use a login made by Go, and the other way round:

```json
{"di":{"access_token":"<jwt>","refresh_token":"...","client_id":"GARMIN_CONNECT_MOBILE_ANDROID_DI_2025Q2",
       "expires_at":1760540000,"refresh_token_expires_at":1763132000},
 "saved_at":"2026-10-15T14:02:11Z"}
```

```json
{"oauth1":{"oauth_token":"...","oauth_token_secret":"..."},
 "oauth2":{"access_token":"...","token_type":"Bearer","refresh_token":"...","expires_at":1760540000,"refresh_token_expires_at":1763132000},
 "consumer":{"consumer_key":"...","consumer_secret":"..."},
 "saved_at":"2026-10-15T14:02:11Z"}
```

The expired marker holds no secret:

```json
{"expired_at":"2026-10-20T08:00:00Z","saved_at":"2026-10-15T14:02:11Z"}
```

Bun reads the marker as "Token file is incomplete", so a Bun run after the marker also asks for a reconnect.

### When a login becomes expired

Go writes the expired marker only on an answer that proves the login is dead:
- the DI refresh grant answers 400 or 401;
- the legacy OAuth2 exchange answers 401;
- a data request answers 401, the refresh succeeds, and the retry answers 401 again.

Go keeps `tokens.json` as it is on a network error, a timeout, a 5xx, a 403 or a 429. The JS deleted the file on any refresh failure and then signed in again with the stored password. Go never signs in again on its own, so a temporary failure must not cost the user a reconnect.

The export result for a dead login is `GARMIN_SESSION` "The Garmin session expired. Reconnect Garmin."

## Cache

The Go cache keeps JSON files in `dataDir/garmin-cache/`:
- `daily/YYYY-MM-DD.json`: `{"<endpoint>": {"fetched_at": "<RFC 3339 UTC>", "data": <raw answer>}}`, one entry per daily endpoint.
- `activity/<activityId>.json`: `{"splits": {"fetched_at": "...", "data": <raw answer>}}`.

The data is stored as the raw bytes Garmin sent (`json.RawMessage`), so key order and number text survive.

Rules, the same as `exporter.js`:
- A null answer (204, empty body, or a failed request) is never stored.
- A daily answer is used from the cache only when the day is outside the refresh window and the answer was fetched 7 or more days after the day (`FINAL_AFTER_DAYS`).
- A day is inside the refresh window when it is fewer than `refreshWindow` calendar days before today, in local time. (The JS divides milliseconds by 86,400,000, which can be off by an hour across a change to or from daylight saving time. Go counts calendar days. Assumption A4: the difference matters only for one day per year in time zones with daylight saving time, and the user's time zone, Guatemala, has none.)
- Splits are cached forever once fetched, as in JS.
- Aggregated requests, the activity list and goals are never cached.
- A day file is written after the day is done (temporary file and rename). A killed run keeps every finished day. The JS saved the sql.js file every 10 days.

### Why JSON files and not SQLite

- The stage 1 rule allows no third party Go modules, and a pure Go SQLite (`modernc.org/sqlite`) is a large one. `CGO_ENABLED=0` rules out the C SQLite.
- While Bun is the fallback, both engines would write `garmin_cache.db`, and sql.js rewrites the whole file, so the last writer erases the other's data.
- The MFP cache already uses per day JSON files.

Cost: the first Go export downloads everything again, about 17 requests per day plus one per activity, because it cannot read the sql.js cache. A 30 day export is about 560 requests, about 5 minutes at the 500 ms pace. A 90 day export is about 15 minutes. If Garmin answers 429 on the way, the run stops, every finished day stays cached, and the next run goes on from there. Assumption A5: this one time cost is acceptable. The alternative is a one time Bun command that copies `garmin_cache.db` into the JSON files; it is about 40 lines and is deleted in stage 3. Add it only if the user asks.

## Connection model and UI

Both sources use one model: `{connected, expired, savedAt}`.

| State | Text | Button | Include switch |
| --- | --- | --- | --- |
| Not connected | "Not connected" | connect | off and disabled |
| Connected | "Connected" | disconnect | enabled, remembered per source (`ge_garmin_on`, `ge_mfp_on`) |
| Session expired | "Session expired" | reconnect | enabled, but the source is skipped |

Garmin row:
- The Setup card loses the always visible email and password fields. It gets a "Garmin Connect" row with the same layout as the MFP row.
- connect and reconnect open a small form in the card: email (remembered in `ge_email`, as now), password, a "Sign in" button and a cancel link. "Sign in" runs `garmin-connect`. The password field is cleared when the call returns, on success and on failure.
- On success, the row says "Connected" and the toast says "Garmin connected." On failure, the form stays open and shows the error sentence. `GARMIN_RATE_LIMITED` shows the minutes left.
- disconnect runs `garmin-disconnect`.
- An export that ends with `GARMIN_SESSION` turns the row into "Session expired".

Run rules:
- A source is usable when its switch is on, it is connected, and it is not expired.
- With no usable source, the Pull button stays enabled but the toast says "Switch on and connect Garmin or MyFitnessPal first." and no run starts. This replaces the toast "Email and password are required."
- Garmin off and MFP on: the UI runs `export` with `garmin:false`, then `mfp-export`, then `summary` with `csvDir`, `startDate` and `endDate`. The Garmin rows show "Off" and their rings stay empty. The MFP row and ring work as today. If MFP fails, the run ends as Failed, because no source produced data.
- Garmin on: as today. A Garmin failure stops the run before MFP. An MFP failure after a Garmin success is a warning row, and the run is Done.

## Engine switch and fallback

- `app.js` gets `const DEFAULT_ENGINE = 'bun'` and reads `store.get('engine', DEFAULT_ENGINE)`. The engine picks the binary for the `export` command only: `binaries/garmin-export-core` for `go`, `binaries/garmin-core` for `bun`.
- `garmin-status`, `garmin-connect`, `garmin-disconnect`, `clear-cache` and `mfp-export` always run in Go. `summary`, `pack` and `report` always run in Bun.
- Both binaries stay in `externalBin` and in the `shell:allow-spawn` list until stage 3.
- Bun `export` loses its "Email and password are required" check. `exporter.js` gets a guard right before its `auth.login` call: without email and password it ends with `errorCode: "GARMIN_SESSION"` and "Connect Garmin first." The guard sits in the shared function because `exportHealth` also falls back to a sign in when a refresh fails on a network error; without it, the Bun engine would spend a sign in with empty credentials. It still accepts `email` and `password` for one release, so an old UI keeps working; the new UI never sends them.
- Bun `summary` accepts `{csvDir, startDate, endDate}` without `jsonPath` and returns `{ok:true, summary:null, pack}`.
- The default flips to `go` in the last task, after the Go golden test, the Go end to end test and the Bun end to end test against the Go binary pass, and after one real export compared with a Bun export of the same range (`node electron-app/scripts/compare-csv.js`). Going back is a one line commit, because Bun still ships. The `ge_engine` key is a developer switch; release builds have no UI for it.

## Byte equal CSV files

The golden files were written by JavaScript. Go must copy these JavaScript behaviours, each pinned by a unit test in `internal/jsjson` or `internal/garmin`:

1. Key order. `JSON.parse` keeps keys in source order, except keys that look like array indexes (`"0"`, `"1001"`), which come first in numeric order. The fixture has one such key, a device id (`"1001"`), and `primaryDevice` picks `Object.values(map)[0]` when no device is marked primary. Go maps lose order, so `jsjson` decodes with `json.Decoder.Token` into `Object{Keys []string, Vals []Value}` and applies the JavaScript order.
2. Numbers. JavaScript parses every number into a float64 and prints it with `String(n)`. Go must format the float64, never the source text: Garmin can send `72.0`, which JavaScript prints as `72`. `FormatFloat(f, 'g', -1, 64)` is wrong (it prints `1.234567e+06`). The rule: shortest round trip digits; plain decimal notation when 1e-6 <= |n| < 1e21 (so `0.000001`); else exponent form like `1e-7` and `1.5e+21`; `-0` prints `0`.
3. `JSON.stringify` for nested values in cells (`sleepScores`, `sleepScoreFeedback` when an object, and arrays in `flatten`): no spaces, source key order, numbers as in rule 2, and only `"`, `\`, and control characters escaped (`\b \f \n \r \t`, others `\u00XX`). Go's `json.Marshal` escapes `<`, `>`, `&`, U+2028 and U+2029, so `jsjson` has its own small encoder. The fixture has none of those characters, so unit tests pin them.
4. `String(v)` for a cell: `true`/`false`; an array prints its items joined by `,` with null as empty; an object prints `[object Object]`. Only the first two occur in the fixture; the others are ported for parity and listed as debt.
5. The CSV writer: the header is the union of row keys in first seen order; a field is quoted when it holds `,`, `"`, `\n` or `\r`, with `"` doubled; lines end in `\n`, with one at the end of the file; no BOM; a table with no rows writes no file (so the fixture gives 18 of the 20 files).
6. Day order. `daily` is filled newest day first (`generateDateRange` counts down), and every daily CSV keeps that order. Go keeps a slice of dates, never a map. `guard.Dates` is oldest first, so the export reverses it.
7. Branches on type. `extractDailySummary` adds `stepsGoal` and `stepsWellness` only when `steps` is an object and not an array; this decides which columns exist. `extractTraining` takes the last item when readiness is an array.
8. Local time. JavaScript computes `today`, `export_date`, the file time tag and the refresh window in local time. Go uses `time.Local` for all of them, as `internal/mfp/cache.go` already does.

The Go CSV test reads the scrubbed fixture `electron-app/garmin/__tests__/fixtures/export-sample.json` and compares the 18 files with `electron-app/garmin/__tests__/golden` byte for byte. Go has no `UPDATE_GOLDEN`: the JavaScript test owns the golden files until stage 3, which moves both into `tauri-app/core/internal/garmin/testdata`.

## Error codes

| Code | Commands | Meaning | UI |
| --- | --- | --- | --- |
| `GARMIN_INPUT` | all Garmin commands | a path, range, refresh window or credential failed the guard | toast, run does not start |
| `GARMIN_SESSION` | `export` | no `tokens.json`, the expired marker, or a dead login found during the run | Garmin row becomes "Session expired" (or stays "Not connected") |
| `GARMIN_BAD_PASSWORD` | `garmin-connect` | wrong email or password | form shows the error |
| `GARMIN_MFA` | `garmin-connect` | the account uses two step verification | form shows "This account uses two step verification, which Garmin Export does not support yet." |
| `GARMIN_RATE_LIMITED` | `garmin-connect`, `export` | a 429 on a sign in (cooldown starts), a running cooldown, or a 429 on data after 2 retries | form or run shows the wait |
| `GARMIN_BLOCKED` | `garmin-connect`, `export` | both logins refused (403 or another non 200), a 403 on the profile, or 5 403 answers in a row | toast and log; tokens kept |
| `GARMIN_ERROR` | all | anything else: network, disk, a bug | toast and bug report box |

The skip codes on `warn` lines stay as in `client.js` (`EMPTY_BODY`, `BAD_JSON`, `TIMEOUT`, `NETWORK`, `FORBIDDEN`, `HTTP_4XX`, `HTTP_5XX`), so the bug report reads the same.

## Client rules

Ported from `client.js:fetchUrl`, which the integration tests pin:
- At least 500 ms between requests. Only the loopback fake server may use 0.
- DI tokens send the native Android headers (`NATIVE_HEADERS` in `auth.js`); connectapi answers 403 without them. Legacy tokens send `User-Agent: com.garmin.android.apps.connectmobile`.
- 401: refresh once and retry. A second 401 writes the expired marker and stops the run.
- 403: skip that request and keep the tokens. Stop the run on a 403 for `social_profile` or after 5 in a row.
- 429: wait for `Retry-After` (seconds or an HTTP date, at most 60 s) or the backoff, at most 2 retries, then stop the run with `GARMIN_RATE_LIMITED`.
- 204 or an empty body: success with no data, logged as `[no data]`, not stored in the cache.
- Network error or timeout (30 s): up to 5 retries with backoff `min(5000 * 2^attempt + jitter, 60000)` ms.
- The `goals` request sends `Sec-Fetch-Site: same-origin` and `start=1`.
- Error previews are cut to 200 characters and pass through a Go port of `redactString` (Bearer and Basic values, cookies, JWT shaped strings, secret query keys, JSON pairs with secret keys).

## Testing

The tests keep the trophy shape and the technique IDs of the existing suites.

- Unit tests in `internal/jsjson`: key order with index-like keys, the number table (`0`, `-0`, `1`, `72.0`, `0.1`, `1.5`, `1234567`, `12345678901`, `1e-7`, `1.5e-7`, `1e21`, `123456789012345680000`), the string escape table, and `String(v)` for arrays and objects.
- Unit tests in `internal/garmin` against `garmintest.Server`, a port of `fake-garmin.js` with the same control knobs (`password`, `mobileLogin`, `diauth`, `tokenTtl`, `dataStatus`, `failOnce`, `widget429`, `widgetStatus`) and a call log. Every login and client test in `electron-app/garmin/__tests__/integration.test.js` gets a Go test with the same ID: DT-R1 to DT-R10, ST-T1, ST-T2, EP-01 to EP-04, BVA-01 to BVA-05, EG-01 to EG-08.
- New Go tests for the new behaviour: status offline (never a request), the expired marker on each dead login answer and never on network, 5xx, 403 or 429, the cooldown read from a file Bun wrote, and `garmin:false`.
- The golden test: the fixture through `csv.go` gives the 18 golden files byte for byte, plus the AC-02 check that the fixed columns are not empty.
- `cmd/garmin-export-core/main_test.go`: the real binary over stdin against `garmintest`: `garmin-connect`, `garmin-status`, `export`, `garmin-disconnect`, `clear-cache`.
- `tauri-app/sidecar/garmin-e2e.test.js` (Bun): builds the Go binary and runs it against the JavaScript `fake-garmin.js`, the same fake that the Bun end to end test uses. The CSV files must equal the golden files, and a `tokens.json` written by Go must load in `auth.loadTokens`, and the other way round. Using the JavaScript fake here makes it a cross engine check: both engines talk to one server.
- Playwright (`bunx playwright test` in `tauri-app`): the Garmin connection row, the connect form, the Include switches and the "no usable source" toast, through `ui/dev-mock.js`.
- No test signs in to the real Garmin. A live run happens only in Task 0 and in the final check, each after the user agrees.

## Risks

- Go's TLS fingerprint against Garmin's SSO and diauth hosts is not measured. Node's `fetch` works today. If Cloudflare blocks Go's TLS client, the plan adds `utls` for the sign in hosts only, as stage 1 planned for MFP. Task 0 answers this with one DI refresh (not a sign in) and at most one sign in.
- Garmin limits sign ins per account: a 429 after 4 sign ins in 40 minutes (2026-10-14). The plan allows at most one live sign in in Task 0 and one in the final check, and none in a loop.
- The DI refresh token may rotate on each refresh. Task 0 must write the new token back to the app's real `tokens.json`, or the app loses its login.
- The DI refresh token lifetime is not known. If Garmin sends no `refresh_token_expires_in`, `garmin-status` cannot predict expiry, and only a failed run shows "Session expired". This matches MFP.
- Byte equality covers only what the fixture holds. `weight_body_comp.csv` and `blood_pressure.csv` have no golden file, because the fixture has no weigh in and no blood pressure. Go tests pin them with small hand made inputs that are first run through the JavaScript writer to get the expected output.
- The first Go export downloads the whole range again (see Cache).
- Two engines share `tokens.json` for one release. The cross engine test pins the shapes, and only one export runs at a time.

## Assumptions to confirm

- A1: `dataDir` must be inside `$HOME` (it already is in the app).
- A2: `saved_at` may go back to `null` while Bun refreshes the tokens.
- A3: `csv_<today>` with Garmin on, `csv_<endDate>` with Garmin off.
- A4: calendar days for the refresh window.
- A5: no import of the sql.js cache; the first Go export downloads again.
- A6: the Garmin connect form lives inside the Setup card, not in a separate window. Garmin has no web login that works in a webview for this flow (the DI login is a JSON API), so the app still sees the password once; MFP's "the app never sees your password" does not apply to Garmin, and the README must say so.
- A7: the `ge_engine` switch has no UI. Rollback is a release with the default changed back.

## Spike results (Go), 2026-10-16, partial

Task 0's live token renewal was not run: the permission classifier blocked writing the user's real `tokens.json`. Instead, one unauthenticated GET per host was sent from Go's `net/http` and from Node's `fetch`, with the same User-Agent and no credentials, one second apart:

| Host and path | Go | Node |
| --- | --- | --- |
| `connectapi.garmin.com/userprofile-service/socialProfile` | 403, no challenge page | 403, no challenge page |
| `diauth.garmin.com/di-oauth2-service/oauth/token` | 405 | 405 |
| `sso.garmin.com/mobile/api/login` | 400 | 429 (the second GET on this path within seconds) |
| `sso.garmin.com/sso/embed` | 200 | 200 |

Every answer came from Cloudflare (`Server: cloudflare`), and neither client got a JavaScript challenge page, so Cloudflare does not block Go's TLS fingerprint at the edge for these hosts. Two questions stay open until the user's first `garmin-connect` and export: a sign in from Go, and DI refresh token rotation. `/mobile/api/login` rate limits unauthenticated GETs by IP within seconds, so the user's first connect should wait about 15 minutes after any probe.
