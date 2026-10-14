# Go backend, stage 1: MyFitnessPal export

Date: 2026-10-14. Status: approved in chat, waiting for review of this document.

## Goal and context

Garmin Export will move its whole backend from the Bun sidecar to one Go binary. The move happens in three stages, and the app keeps working after each one.

1. Stage 1, this document: a new Go binary, `garmin-export-core`, ships next to the Bun sidecar and exports MyFitnessPal (MFP) food data. It also sets up the protocol, the input checks, and the test setup that the later stages reuse.
2. Stage 2: the Garmin login, client, cache and CSV writer move to Go, until the 18 golden CSV files and the end to end tests pass against the Go binary.
3. Stage 3: the LLM pack, the bug report and the summary command move to Go. Then the Bun sidecar and `electron-app/` are removed from the build. The 3.x releases and the git history stay. `tools/ci_pipeline.py`, `tools/release.py` and their workflows are replaced, and the workflows change only with the user's permission, as CLAUDE.md requires.

The user wants MFP data in the export so that an LLM can relate food to sleep, HRV, training and weight. The user has MFP Premium, and MFP isn't linked to Garmin Connect. The user logs food on most days but not all of them. MFP had food on 17 of the last 45 days.

## What the spike showed (2026-10-14, the user's account)

- MFP has no public API, and it doesn't accept new API partners. Its Terms of Service (section 5) forbid automated access. The user accepted that risk for reading their own account, read only, at about one request per second.
- A login page opened through Chrome DevTools was blocked by MFP's bot detection. A normal browser session works.
- `GET https://www.myfitnesspal.com/user/auth_token?refresh=true` with the browser session cookies returns JSON with `access_token` and `user_id`. When the session isn't valid, it returns HTML with HTTP 200.
- The diary request is `GET https://api.myfitnesspal.com/v2/diary?entry_date=YYYY-MM-DD&types=food_entry,exercise_entry,steps_aggregate`. It needs the headers `Authorization: Bearer <token>`, `mfp-client-id: mfp-main-js` and `mfp-user-id: <id>`, and it returns `items`. Each `food_entry` holds `meal_name`, `meal_position`, `consumed_at`, `logged_at`, `food` (`description`, `brand_name`, `verified`), `serving_size` (`value`, `unit`, `gram_weight`, `nutrition_multiplier`), `servings` and `nutritional_contents`. Those contents are `energy` (an object), fat, saturated fat, trans fat, cholesterol, sodium, potassium, carbohydrates, fiber, sugar, added sugars, protein, calcium, iron and vitamin D.
- Without `types`, food comes grouped as `diary_meal` items. A `types` list with an unknown type returns an empty list without an error, so the client uses only the three confirmed types.
- The HTML diary page and the JSON API agreed on the number of foods for every day tested.

## Architecture

### Go module

The module lives in `tauri-app/core/` and uses only the Go standard library.

- `cmd/garmin-export-core/main.go` reads one JSON request line on stdin and writes JSON lines on stdout, with the same shapes as the Bun sidecar: `{kind:"log"}`, `{kind:"progress"}` and `{kind:"result", ok, ...}`.
- `internal/guard` checks that paths resolve inside `$HOME` after `filepath.EvalSymlinks`, and that ranges are valid dates of 90 days or fewer. It ports `resolveOutputDir` and `validateRange`, and it follows `docs/solutions/security-issues/path-containment-prefix-bypass-2026-04-16.md`.
- `internal/mfp` holds the HTTP client, the session file, the per-day cache and the CSV writer.

The build runs `CGO_ENABLED=0 go build -trimpath -ldflags "-s -w"` for `arm64` and `amd64`, and then `lipo` joins the two into `binaries/garmin-export-core-universal-apple-darwin`. The binary is added to `externalBin` and to the `shell:allow-spawn` capability.

### Connecting the account (Rust)

- The `mfp_connect` command opens a `WebviewWindow` labeled `mfp-login` on `https://www.myfitnesspal.com/account/login`, with a full Safari User-Agent. The window has no IPC capability.
- When a page finishes loading outside `/account/login`, a spawned async task calls `cookies()` and keeps the cookies whose domain ends in `myfitnesspal.com`. It doesn't use `cookies_for_url`, because that call drops cookies set on `.myfitnesspal.com`.
- If `__Secure-next-auth.session-token` is present, Rust writes `mfp_session.json` with mode 0600 in the app data folder. The file holds `{cookies, user_agent, saved_at, session_expires}`. Then Rust closes the window and emits `mfp-connected`.
- The `mfp_disconnect` command deletes the file and the MFP cookies in the webview cookie store.
- Neither Rust nor the UI handles the MFP password.

### Export

The order is: the Bun sidecar exports Garmin, then the UI starts `garmin-export-core` with `{cmd:"mfp-export", startDate, endDate, dataDir, csvDir}`, then the UI calls the existing `summary` command so that the LLM pack preview includes the MFP files.

The Go client works in this order.

1. First, it reads `mfp_session.json` and sends its cookies and User-Agent.
2. Second, it calls `auth_token`. An HTML answer ends MFP with `errorCode: "MFP_SESSION"`. When the response sets new cookies, the client rewrites the session file.
3. Third, it calls `nutrient-goals` once.
4. Fourth, it calls `v2/diary` once per day that isn't final in the cache, at one request per second.

The client stops at the first HTTP 429. On a 401 it refreshes `auth_token` once and tries again. A failure in MFP never changes the result of the Garmin export. The UI shows "MFP skipped: <reason>", and an expired session turns the setup row into "Reconnect".

### Cache

The cache keeps one JSON file per day in `dataDir/mfp-cache/YYYY-MM-DD.json`, with `{fetched_at, items}`. The Go binary never writes `garmin_cache.db`, because sql.js rewrites the whole file and the last writer would erase the other's data.

A cached day is final only when it has at least one food entry and was fetched 7 or more days after its date. Every other day is fetched again, because the user fills in days by hand and sometimes late. The existing Clear cache button also deletes `mfp-cache/`, which needs a one line change in the Bun `clear-cache` command.

## Output files

All three files use the local date `YYYY-MM-DD`, the same key as the Garmin files, so an LLM joins them by `date`. Units are in the column names.

`mfp_food.csv` has one row per food entry:
- `date`, `meal`, `meal_position`, `consumed_at`, `logged_at`
- `description`, `brand`, `verified`, `servings`, `serving_size`, `serving_unit`, `grams`
- `energy_kcal`, `protein_g`, `carbohydrates_g`, `fiber_g`, `sugar_g`, `added_sugars_g`, `fat_g`, `saturated_fat_g`, `trans_fat_g`, `cholesterol_mg`, `sodium_mg`, `potassium_mg`, `calcium`, `iron`, `vitamin_d`

`mfp_daily.csv` has one row for every date in the range:
- `date`, `logged`, `meals_logged`, `foods_logged`
- `energy_kcal`, `protein_g`, `carbohydrates_g`, `fat_g`, `fiber_g`, `sugar_g`, `sodium_mg`
- `goal_energy_kcal`, `goal_protein_g`, `goal_carbohydrates_g`, `goal_fat_g`, `goal_sodium_mg`, `goal_sugar_g`
- `exercise_kcal_mfp`

`mfp_exercise.csv` has one row per exercise entry: `date`, `name`, `type`, `duration_min`, `energy_kcal`, `source`.

Rules for the values:
- Days are keyed by the diary date (`date` / `entry_date`), not by `consumed_at`.
- Energy is converted to kcal when the account uses kilojoules.
- On a day with `logged=no`, the totals are empty, not 0.
- `calcium`, `iron` and `vitamin_d` carry no unit, because the API sends none and the values do not settle percent of daily value versus milligrams.
- `grams` is `serving_size.value` times `servings` when `serving_size.unit`, lowercased and trimmed, is `g`, `gram`, `grams`, `gramo` or `gramos`. For any other unit the cell is empty.

The LLM pack gets a description for each file in `DESCRIBE`, the new unit suffixes in `UNITS`, and one line in the context file, e.g., "MyFitnessPal: food logged on 17 of 45 days. Days with logged=no have no data, which does not mean that the person did not eat. Join with the Garmin files by date." `mfp_exercise.csv` gets a note that it can repeat exercise that Garmin already recorded.

## UI

- The Setup card gets a "MyFitnessPal" row that shows Connected, Not connected or Reconnect, with a Connect or Disconnect button and a switch to include MFP in exports.
- The Run card gets a fifth task row, "MyFitnessPal", with its own progress and time.

## Tests

The tests follow the same trophy shape as the Garmin tests.

- Go tests run against an `httptest` fake MFP server that replays scrubbed items from the user's account. The scrubbing uses the per-run secret method of `scripts/make-fixture.js`, and food descriptions and brands are replaced. The cases are login expired (HTML), 401 then success, 429 stop, cookie rotation, a day with no food, kJ energy, the cache final rule, and `$HOME` containment.
- Golden files for the three CSVs, rebuilt only on purpose with `UPDATE_GOLDEN=1`.
- One bun end to end test that starts the Go binary over stdin with `MFP_FAKE_SERVER` set and compares the CSV files with the golden files.
- `MFP_FAKE_SERVER` accepts only `http://127.0.0.1:<port>` or `http://localhost:<port>`, and the binary reads it on every request, the same as `GARMIN_FAKE_SERVER` in `auth.js`.

## First task: Go spike on the real account

Before the client is written, a throwaway Go program answers three questions with the user's session.

1. Does `v2/diary` work with only the Bearer token, or does it also need the cookies, including `cf_clearance`?
2. When does the session token expire, and does `auth_token` set new cookies?
3. Are calcium, iron and vitamin D milligrams or a percent of daily value?

If Go's TLS client gets HTTP 403 where Python got 200, the plan adds `utls` for that one request.

## Out of scope for stage 1

- macOS Keychain storage. It waits for proper code signing, because ad hoc signing makes the Keychain ask for access after every update.
- Reading cookies from Zen. It gets added only if the in app login window is blocked.
- MFP water, notes and weight.
- An energy balance column computed by the app.
- Garmin, LLM pack and report code in Go (stages 2 and 3).

## Risks

- MFP can change its private API or its bot detection at any time, as it did in June and December 2025. The client fails with a clear message, and the Garmin export isn't affected.
- Google and Apple sign in usually refuse embedded windows. Email and password sign in should work.
- The session lifetime is unknown until the spike measures it.

## Spike results (Go), 2026-10-14

The spike ran against the real MyFitnessPal API.

- The `auth_token` request needs all the session cookies. Sending only the session token gets HTTP 302 to the login page.
- `v2/diary` works with only the Bearer token and the headers, without cookies.
- The session token lasted 30 days, so a sign in on 2026-10-14 expires on 2026-11-13.
- The API token lasted 10 days (`expires_in` is 864000).
- `auth_token` sets the cookies `known_user`, `_mfp_session` and `__cf_bm`.
- `user_id` is a JSON string, not a number.
- The nutrients of an entry are already totals for the logged amount, so they are not multiplied by `servings` again.
- Calcium, iron and vitamin D have no unit field. Values such as 20.06, 122.46, 29.1 and 1.7 do not settle percent of daily value versus milligrams, so the three columns carry no unit and the LLM pack says so.
- Grams come from `serving_size.value` times `servings`, and only for gram units. `gram_weight` was unreliable: a 26 g entry had a `gram_weight` of 0.01 per serving, which gives 0.26 g. Entries in `ml` have no reliable weight, so their grams cell is empty.
