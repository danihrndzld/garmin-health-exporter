# Garmin Personal API key: suggested use

## Objective

Export Garmin data with the official, read only Personal API key instead of, or next to, the email sign in. The key needs no password, has no MFA and does not count against Garmin's sign in limit (429 after about 4 sign ins in 40 minutes, measured 2026-10-14).

## Verdict (probed 2026-10-24)

The key covers the daily wellness data and the activity list. It does not cover training metrics or laps.

| CSV | With the key alone | Source on the Personal API |
| --- | --- | --- |
| `daily_summary.csv` | 19 of 26 columns. Empty: `lastSevenDaysAvgRestingHeartRate`, `stepsGoal`, `stepsWellness` and the 4 hydration columns (Garmin answered `{}`) | `usersummary-service/v2/dailysummaries`, 90 days per request |
| `sleep.csv` | 31 of 36 columns. Empty: `unmeasurableSeconds`, `sleepResultType`, `restlessMomentsCount`, `avgSkinTempDeviationC`, `bodyBatteryChange` | `sleep-service/sleep/dailySleepSummariesByDate`, 31 days |
| `hrv.csv`, `body_battery.csv`, `spo2_respiration.csv` | all columns | `hrv-service/hrv/daily` (90 days), `bodyBattery/reports/daily` (31 days), the v2 summary |
| `activities.csv` and the per-type files | all columns except `vO2MaxValue` | `activitylist-service` (same path as connectapi) |
| `daily_steps_aggregated.csv`, `misc_metrics.csv` (fitness age) | all columns | `stats/steps/daily` (28 days), `fitnessage/v2` |
| `training.csv` | rows of dates only: no readiness, training status, VO2 max, load or endurance | none (404) |
| `*_laps.csv`, goals, blood pressure, hill score, running tolerance, weekly intensity, race predictions | missing | none (404) |

Speed: 30 days took 5 s with the key (about 10 requests). The login sends 19 requests per day with a 0.5 s pause, about 5 minutes for 30 days; with the key saved too it sends 10 per day (14 days took 80 s). Respiration `avgValue` covers the whole day; the legacy `avgWakingRespirationValue` left sleep out. Sleep respiration lows and highs can differ by 1 to 2 breaths from the login export, because Garmin's two sleep endpoints compute them differently.

## Suggested setup

1. Create a key at <https://connect.garmin.com/app/settings/personalApiToken> (log in on the web, accept the terms, pick 30, 90, 180 or 365 days). Garmin shows the key once; copy it.
2. In the app, Garmin Connect → **connect** → paste the key → **save key**. The app trades it for a token once to prove it works and saves `garmin-api-key.json` (mode 0600) in the app data dir.
3. For training data and laps too, click **add email login** and sign in once. The app keeps both: each export takes what the key answers from the Personal API and only the rest from the login, so the login sends 10 requests per day instead of 19.
4. Garmin emails about a week before the key expires. When an export ends with "Garmin no longer accepts the API key", create a new key and click **reconnect**.

| Saved | Export uses | Gets |
| --- | --- | --- |
| key only | Personal API | wellness, sleep, HRV, Body Battery, activities; one `PERSONAL_API_LIMITS` warning names what is missing |
| key and login | Personal API first, login for the rest | everything, with fewer login requests |
| login only | connectapi, as before 4.6 | everything |
| expired key and live login | login, with a `GARMIN_KEY_EXPIRED` warning | everything |

## Inputs

- `API_KEY="..."` in `.env` (only for the probe tool; the app never reads `.env`).

## Tools

- `uv run tools/probe_personal_api.py [--end YYYY-MM-DD] [--days N]`: one token request, then one GET per path the app uses. Prints status per path and writes `.tmp/personal_api_probe.json`. Exit 1 when a path API key mode needs stops answering 200.

## Outputs

- The probe table and `.tmp/personal_api_probe.json`.
- In the app, the usual `csv_<endDate>/` folder.

## When Garmin changes the Personal API

1. Run the probe. A `needed` path that is not 200 breaks API key mode; a `login` path that turns 200 is new coverage.
2. For new coverage: add the path to `gconnect` (`PersonalRanges` in `endpoints.go`, the allowlist in `gconnecttest/server.go`), tag, `go get` it here, then map it in `tauri-app/core/internal/garmin/personal.go` (`fromSummary` or `personalDays`) and add an AC test in `personal_test.go`.
3. Update the verdict table above with the date.

## Edge cases

- The token request needs `Accept: */*`. `Accept: application/json` gets 406 because the token comes back as text (found by the live test on 2026-10-24; gconnect v0.2.1 fixed it).
- A wrong, expired or deleted key answers 401 on the token request; the app marks only `garmin-api-key.json` expired and never touches `tokens.json`.
- The activity list caps `limit` at 20 per page; the app already pages by 20.
- Range limits: 90 days for the v2 summary and HRV, 31 for sleep and Body Battery, 28 for steps. A longer range answers 400; the app splits ranges.
- A night without sleep data comes back as a row of nulls with `calendarDate: null`; it adds no sleep row.
