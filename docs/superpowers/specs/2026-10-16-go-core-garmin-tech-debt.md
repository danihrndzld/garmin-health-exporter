# Tech debt in the Garmin JS that stage 2 removes or must not port as is

Date: 2026-10-15. Read with `2026-10-16-go-core-garmin-design.md`.

## Removed by the migration

| Debt | Where | What stage 2 does |
| --- | --- | --- |
| The password is stored in the UI and sent on every run; a failed refresh falls back to a new sign in with it. That spends Garmin's limit of 4 sign ins per 40 minutes without the user asking. | `cli.js:39`, `exporter.js:157-169`, `app.js:344-356` | Only `garmin-connect` signs in. Runs use `tokens.json`; a dead login shows "Session expired". |
| Any refresh failure (network, 5xx, 429) and any second 401 delete `tokens.json` (`clearTokens`), which forces a new sign in. | `auth.js:loadTokens`, `client.js:215` | Go keeps the tokens on temporary failures and writes a marker without secrets only on a proven dead login. |
| `garmin_cache.db` is a sql.js file that is rewritten whole on every save, so two writers erase each other; a schema change drops the whole cache. | `cache.js:save`, `initCache` | Go uses per day JSON files and never opens the sql.js file. `clear-cache` deletes it without loading sql.js. |
| Clear cache loads the sql.js WASM only to run two `DELETE`s. | `cli.js:clearCache` | Go deletes the files. |
| `summary` needs the Garmin JSON file, so the LLM pack cannot be built without Garmin. | `cli.js:summary` | Bun `summary` accepts `csvDir` alone (stage 2); stage 3 moves it to Go. |
| The display name is written to the log, and the bug report sends the log. | `exporter.js:200` | Not ported. |
| `flattenDetails`, the `activity_details` and `activity_split_summaries` endpoints are dead since 4.2, but still defined and tested. | `csv-writer.js:459-491`, `endpoints.js:245-268` | Not ported. The JS keeps them until stage 3 deletes `electron-app/`. |
| The token file is written with `fs.writeFileSync` and a `chmod`, not a temporary file and a rename, so a kill during the write leaves a broken login. | `auth.js:_writeTokens` | Go uses `atomicfile.WriteFile`. |
| The cache is saved every 10 days, so a kill loses up to 9 days of downloads. | `exporter.js:291` and `:377` | Go writes each finished day. |
| The refresh window divides milliseconds by 86,400,000, which is off by one hour across a daylight saving change. | `cache.js:isWithinRefreshWindow` | Go counts calendar days (assumption A4 in the spec). |
| The `GARMIN_FAKE_SERVER` comment is pasted twice. | `auth.js:32-36` | Gone with the file in stage 3. |

## Ported as is for byte equality, fix after acceptance

Each fix changes CSV output, so each needs its own commit with `UPDATE_GOLDEN=1` and a note in the release.

1. `extractWeight` reads `aggregated.weigh_ins`, but the aggregated endpoint is named `agg_weigh_ins` (the daily one is `weigh_ins`). Weight rows come only from `body_composition`, and `agg_weigh_ins` is downloaded for nothing. (`csv-writer.js:316-326`)
2. `weight_body_comp.csv` and `blood_pressure.csv` have no golden file, because the fixture has neither. The Go plan pins them with a hand made input run through the JS writer (Task 9), but neither has been checked against a real Garmin answer.
3. A cell that holds an object prints `[object Object]`, and an array prints its items joined by commas, because `formatCsvField` calls `String(v)`. No such cell is in the fixture, but a Garmin field that turns into an object would print that. (`csv-writer.js:55-62`)
4. `(d.body_battery || [])` and `(d.running_tolerance || [])` are iterated with `for...of`. If Garmin sends an object, JavaScript throws and the whole CSV step fails. Go treats it as an empty list, the only intended difference from JS, marked with a `ponytail:` comment.
5. `extractSleep` stores `sleepScores` and other objects as a JSON string in one cell, which an LLM reads poorly. A flat `sleepScore_overall` column would be better.
6. `extractDailySummary` adds `stepsGoal` and `stepsWellness` only when `steps` is an object, so the columns of `daily_summary.csv` depend on what Garmin sent for the first day.
7. Requests whose answers no CSV uses: daily `rhr`, `max_metrics`, `body_battery_events`, `weigh_ins`, and aggregated `weekly_steps` (52 weeks every run), `progress_summary`, `agg_weigh_ins`. They cost about 4 requests per day plus 3 per run. They stay in stage 2 because they fill the JSON file that Bun `summary` reads. Stage 3 can drop the ones `summary` does not need.
8. `fetchDailyStepsChunked` has a `chunk.values` branch that no fixture or answer has shown, and its own copy of `formatDate`. (`exporter.js:50-77`)

## Do not port

- The `onLog` callback shape with `meta` objects; the Go emitter takes `(type, msg, errorCode)`.
- `formatErrorMessage` legacy prefixes kept "for tests that match substrings" (`client.js:62-67`). Port the sentences the UI and the bug report show; drop the rest.
- The `daysBack` and explicit range double path inside the exporter (`exporter.js:216-236`). Go turns `daysBack` into a range in the guard, once.
- `GarminCache.getStats`, `getCachedDaily` (unused next to `getCachedDailyEntry`), and the schema version table.
- `createClient` and `createGarminClient` factories; Go uses struct literals.
- Exported test helpers (`backoffDelay`, `trimPreview`, `jwtClaims` as public API); keep them package private in Go.

## Debt the migration adds, with its exit

- Two engines share `tokens.json` for one release. Exit: stage 3 deletes Bun.
- Go tests read the fixture and golden files from `electron-app/garmin/__tests__` by relative path. Exit: stage 3 moves them to `internal/garmin/testdata`.
- Two fake Garmin servers (JS for the Bun tests, Go for the Go tests). Exit: stage 3 deletes the JS one.
- `ge_engine` switch in `app.js`. Exit: stage 3 deletes it with Bun.
- Bun `export` still accepts `email` and `password`. Exit: the release after the flip, or stage 3.
