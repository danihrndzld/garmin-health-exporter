# Go backend, stage 3: summary, LLM pack and bug report in Go; Bun and the Electron app leave the build

Date: 2026-10-16. Status: accepted; rebased the same day onto `main` at `6d56875` (after the v4.4.0 release, which carries stage 2 and the extraction of `github.com/danihrndzld/gconnect`). Decisions taken: D1 `electron-app/` is deleted and tagged `archive/electron-app`; D2 `tools/release.py`, `tools/ci_pipeline.py` and both workflows are rewritten for Tauri and Go; D3 Bun stays as the task runner; D4 this ships as 4.5.0.

## Goal and context

Stage 1 (`2026-10-14-go-core-mfp-design.md`) added the Go binary `garmin-export-core` and the MyFitnessPal export. Stage 2 (`2026-10-16-go-core-garmin-design.md`) moves the Garmin login, client, cache and CSV writer to Go and makes Go the default engine. After stage 2 the Bun sidecar `garmin-core` still ships for three commands: `summary` (the preview cards and the pack preview), `pack` (writes `llm-<lang>/` or `llm-<lang>-one-file/`) and `report` (the scrubbed bug report links), plus `export` as a fallback behind the `ge_engine` switch.

Stage 3 moves those three commands to Go and removes everything that only existed to run JavaScript at run time:

- `tauri-app/sidecar/` (`cli.js` 159 lines, `llm-pack.js` 152, `report.js` 34, and 4 test files), the `bun build --compile` step, and the 62 MB per architecture `garmin-core` binary.
- `electron-app/` (4,090 lines of product JavaScript, 3,363 lines of tests, `sql.js`, electron-builder) and the `npm install` that the Tauri build needs today only to reach `electron-app/node_modules/sql.js` and `electron-app/renderer/date-range.js`.
- `tools/ci_pipeline.py`, `tools/release.py` and the two workflows, which build and release the Electron app. Replacing them changes `workflows/`, which CLAUDE.md allows only with the user's permission; the user gave it on 2026-10-16 (D2).

The user keeps: the same CSV files, the same `llm-*` folders, the same `tokens.json`, `mfp_session.json` and cache files, and the same UI. Nothing the app writes changes format in stage 3, so a 4.x release before stage 3 reads everything stage 3 writes, and the other way round.

Measured on 2026-10-15 (Apple Silicon, this repository):

| Item | Today | After stage 3 |
| --- | --- | --- |
| `garmin-core` (Bun, arm64 slice) | 61.9 MB | gone |
| `garmin-export-core` (Go) | 6.7 MB arm64, 7.2 MB amd64, 13.9 MB universal | the same |
| Universal DMG on GitHub | 62.1 MB (v4.3.0), 56.2 MB (v4.0.0) | about 15 to 20 MB, estimated from the v4.0.0 compression ratio; measured in Task 6 |
| Start to first answer, warm | Bun 30 ms, Go 10 ms | Go 10 ms |
| Start to first answer, cold cache | Bun 860 ms, Go 260 ms | Go 260 ms |
| Build prerequisites | Node.js, npm, Bun, Rust, Go, Xcode CLT | Bun (task runner for `@tauri-apps/cli` and Playwright), Rust, Go, Xcode CLT |
| JavaScript product code in the build | 4,435 lines | 147 (`ui/date-range.js`) plus `ui/app.js` |

## Scope

In scope:
- Go packages `internal/pack` (port of `llm-pack.js`) and `internal/report` (port of `report.js`; redaction comes from `gconnect.Redact` and `gconnect.RedactURL`, already ported in the library, so there is no `internal/redact`), and `internal/garmin/summary.go` (port of `cli.js:summarize`).
- Go commands `summary`, `pack` and `report` as handlers in `cmd/garmin-export-core/` (`summary.go`, `pack.go`, `report.go`), with the exact result shapes the UI reads today. The handlers decode the wire request, run `guard`, resolve the time zone name, and call the use cases with typed requests, as the stage 2 handlers do; the use cases never see `map[string]any`.
- Golden files generated from the JavaScript before any deletion: `CONTEXT.es.md`, `CONTEXT.en.md`, the two one-file packs, the preview numbers, the report URLs and the redaction cases. A Bun test pins the JavaScript to them until the sidecar is deleted; a Go test pins Go (and the library's redaction) to them for good.
- UI: `platform.run` defaults to `binaries/garmin-export-core`; the `ge_engine` switch, `DEFAULT_ENGINE`, the explicit binary arguments and the handling of Bun's `export` answer (summary and pack inside the result) go away. `dev-mock.js` answers `export` with the Go shape only; `e2e/engine.spec.js` loses the Bun engine test (ST-T20) and `MockBackend.engine()` goes. No visible change.
- Build: `package.json` scripts, `tauri.conf.json` (`externalBin`, `beforeDevCommand`, `beforeBuildCommand`), `capabilities/default.json`, `playwright.config.js`, `.gitignore`. The universal build becomes `go build` twice, `lipo`, `tauri build --target universal-apple-darwin`.
- `electron-app/renderer/date-range.js` becomes `tauri-app/ui/date-range.js`, a tracked file, with its `node:test` suite under `tauri-app/ui/__tests__/` run by `bun test` (Bun 1.3.11 implements `node:test`; checked 2026-10-15).
- The scrubbed fixture and the 18 golden CSV files move to `tauri-app/core/internal/garmin/testdata/`; the Go golden test gets `UPDATE_GOLDEN=1`. `scripts/make-fixture.js` and `scripts/compare-csv.js` move to `tauri-app/scripts/` and run with `bun`.
- `electron-app/` is deleted, after an annotated tag on the last commit that holds it (decision D1).
- `tools/ci_pipeline.py`, `tools/release.py`, `workflows/cicd_pipeline.md` and `workflows/release.md` are rewritten for `tauri-app/` (decision D2).
- Docs: `CLAUDE.md`, `README.md`, the three header comments that still say "sidecar" (`ui/app.js`, `src-tauri/src/main.rs`, `internal/protocol/protocol.go`).

Out of scope:
- Any change to CSV content, pack wording, unit inference or the report format. Stage 3 copies bytes. Improvements come after, each with an on purpose golden update.
- Dropping Bun as the development task runner. `bun run`, `bunx playwright` and `bun test` stay; only `bun build --compile` and the sidecar go (decision D3).
- The legacy Python scripts at the repository root (`garmin_health_export.py`, `download_activity_details.py`, `json_to_csv.py`). CLAUDE.md says to leave them alone.
- Keychain storage, MFA, Strava (unchanged from stages 1 and 2).
- Removing the `garmin_cache.db` deletion from `clear-cache`. Users of 4.3.0 still have the file; keep deleting it for at least two releases.

## Architecture

```
UI (app.js)
  ├─ garmin-status / garmin-connect / garmin-disconnect / clear-cache ──► garmin-export-core (Go)
  ├─ export ──────────────────────────────────────────────────────────► garmin-export-core (Go)
  ├─ mfp-export ──────────────────────────────────────────────────────► garmin-export-core (Go)
  ├─ summary / pack ──────────────────────────────────────────────────► garmin-export-core (Go)   [stage 3]
  └─ report ──────────────────────────────────────────────────────────► garmin-export-core (Go)   [stage 3]
Rust: mfp_connect / mfp_disconnect / mfp_status (unchanged)
```

The UI keeps the chain stage 2 leaves: `export`, then `mfp-export`, then `summary` with `{jsonPath, csvDir, warnings}`; at boot `summary` with `{jsonPath, csvDir, warnings}`; with Garmin off `summary` with `{csvDir, startDate, endDate}`. Stage 3 changes no request the UI sends.

### Go packages

- `github.com/danihrndzld/gconnect` v0.1.2: `Redact(s)`, `RedactURL(s)` and `IsSecretKey(k)`, which `report` uses for the `k=v` pairs of `scrub` (one list of secret keys, in the library).
- `internal/pack`: `BuildPreview(Options) (*Preview, error)`, `Build(Options, lang, mode) (*Result, error)`, `Context(lang, Options, []File) string`. `Options{CSVDir, StartDate, EndDate string; Warnings []string; Now time.Time; TZ string}`: `Now` and `TZ` are the only inputs that change between runs, so tests inject them and `cmd/` fills them (`os.Getenv` is forbidden inside). `Result{Dir, File (omitempty), Count}`.
- `internal/report`: `Scrub(s)`, `Build(errText string, log []Line, version, os string) Result`, `EncodeURIComponent(s)`, `Result{IssueURL, MailtoURL}`.
- `internal/garmin/summary.go`: `Summarize(raw []byte) (*Summary, error)`, `Summary{Range json.RawMessage; Days []Day}`, `Day{Date string; Steps, RHR, SleepScore, SleepSec, HRV json.RawMessage}` (each value as the file has it, or `null`), `(*Summary).Dates()`.
- `internal/jsjson`: `SliceUTF16(s string, n int) string`, JavaScript's `s.slice(0, n)` (UTF-16 code units), used by the warning cut and the report title.
- `cmd/garmin-export-core/`: `summary.go` (handler `summary`, helpers `warnings`, `tzName`, `packOptions`), `pack.go` (handler `buildPack`), `report.go` (handler `buildReport`); three entries in the `commands` map of `main.go`. `arch_test.go` adds `internal/pack` and `internal/report` to the packages whose exported surface may not hold `map[string]any`.

The module keeps the stage 1 rules: Go standard library plus the one `gconnect` require, `CGO_ENABLED=0`, universal binary through `lipo`.

### Trust boundary

`pack` and `summary` take paths. As in stage 2, every path goes through `guard.InsideHome` first. `pack` also requires the folder to exist, to be a directory and to be named `csv_*`, because `Build` deletes and recreates `llm-*` subfolders inside it. `summary` requires `jsonPath` to end in `.json`. Warnings from the UI are cut to 200 items of 500 UTF-16 units, as `cli.js:cleanWarnings` does. `report` takes text only and never touches the disk.

## Protocol commands

Every command reads one JSON line on stdin and ends with one `{kind:"result", ok, ...}` line. `protocol.Emitter.Result` sets `kind` last, so no payload key may be named `kind` (stage 2 renamed `kind` to `loginKind` for this reason). None of the three commands uses that key.

### summary

Request, three shapes, all sent by the UI after stage 2:

```json
{"cmd":"summary","jsonPath":"<abs>/garmin_health_2026-10-15_14-03-55.json","csvDir":"<abs>/csv_2026-10-15","warnings":["hill_score skipped (404)"]}
{"cmd":"summary","jsonPath":"<abs>/garmin_health_....json","csvDir":"<abs>/csv_2026-10-15"}
{"cmd":"summary","csvDir":"<abs>/csv_2026-10-15","startDate":"2026-09-16","endDate":"2026-10-15"}
```

Result:

```json
{"ok":true,
 "summary":{"range":{"start":"2026-09-16","end":"2026-10-15","days":30},
            "days":[{"date":"2026-09-16","steps":8123,"rhr":52,"sleepScore":81,"sleepSec":25200,"hrv":61}, "..."]},
 "pack":{"csvDir":"<abs>/csv_2026-10-15","startDate":"2026-09-16","endDate":"2026-10-15","days":30,
         "files":[{"name":"activities.csv","rows":12}, "..."],"warnings":["hill_score skipped (404)"],
         "langs":{"es":{"context":"# Datos de Garmin Connect\n...","bytes":121000,"tokens":40333,"singleName":"garmin_2026-09-16_2026-10-15.es.md"},
                  "en":{"context":"# Garmin Connect data\n...","bytes":119000,"tokens":39667,"singleName":"garmin_2026-09-16_2026-10-15.en.md"}}}}
```

Rules, ported from `cli.js:summary` and `summarize` as stage 2 leaves them:
- With `jsonPath`: it must resolve inside `$HOME` and end with `.json`, else `{ok:false, error:"jsonPath must be a .json file within your home directory."}`. An unreadable or non JSON file ends with `"Could not read export."`. The pack dates come from `summary.range`.
- Without `jsonPath`: `csvDir` must resolve inside `$HOME` (else `"csvDir must be a folder within your home directory."`) and `startDate`/`endDate` must be `YYYY-MM-DD` (else `"startDate and endDate must be YYYY-MM-DD."`); the result has `summary: null`. With `jsonPath`, a `csvDir` that is absent or outside `$HOME` gives `pack: null`, not an error, as `cli.js` did.
- `summary.days` is sorted by date ascending. Each value is the JSON number from the export file or `null` when any step of the path is missing (`stats.totalSteps`, `heart_rates.restingHeartRate`, `sleep.dailySleepDTO.sleepScores.overall.value`, `sleep.dailySleepDTO.sleepTimeSeconds`, `hrv.hrvSummary.lastNightAvg`). `range` is the file's `date_range` as it is.
- `types` and `activities` from the JavaScript `summarize` are not ported: `app.js:renderSummary` reads only `days`, and the MFP step reads only `range` (assumption A2).
- `pack` is `null` (JSON null, never `{}`) when `csvDir` is absent, missing on disk, or the dates are empty, because the UI keeps its last pack with `pack = s.pack || pack`.
- `pack.files` is always a JSON array, `[]` for an empty folder, never `null`: the UI calls `.length` and `.forEach` on it.

### pack

Request: `{"cmd":"pack","mode":"files"|"single","lang":"es"|"en","csvDir":"<abs>/csv_2026-10-15","startDate":"2026-09-16","endDate":"2026-10-15","warnings":[...]}`

Result: `{"ok":true,"dir":"<csvDir>/llm-es","count":19}` for `files`; `{"ok":true,"dir":"<csvDir>/llm-es-one-file","file":"<dir>/garmin_2026-09-16_2026-10-15.es.md","count":1}` for `single`.

Errors keep the `cli.js` sentences and carry no `errorCode`, as today: `"csvDir must be an export csv_ folder within your home directory."`, `"startDate and endDate must be YYYY-MM-DD."`, `"lang must be es or en."`, `"mode must be files or single."`.

Behaviour, ported from `llm-pack.js:buildPack`: delete root level files matching `^(CONTEXT|garmin_).*\.(es|en)\.md$` (v4.0.0 leftovers), delete and recreate the one subfolder, write `CONTEXT.<lang>.md` plus a copy of every `*.csv` (files mode) or the one Markdown file (single mode). Files are written with mode 0644, not through `atomicfile`: the folder is rebuilt on every press and the user drags its files into a chat.

### report

Request: `{"cmd":"report","error":"...","log":[{"ts":"20:53:01","type":"warn","msg":"..."}, "..."],"version":"4.5.0","os":"Mac OS X 15.6"}`

Result: `{"ok":true,"issueUrl":"https://github.com/danihrndzld/garmin-health-exporter/issues/new?title=...&body=...","mailtoUrl":"mailto:danielernesto@ufm.edu?subject=...&body=..."}`

Rules, ported from `report.js`: `error` empty means `Unknown error`; `version` and `os` absent mean `?`; the last 200 log lines; every piece goes through `Scrub`; the title is `Export failed: <error>` cut to 120 UTF-16 units; lines are dropped from the top until the encoded URL is at most 7,500 characters (issue) or 1,800 (mailto).

## Parity rules

The golden files were written by JavaScript. Go must copy these behaviours, each pinned by a test in Task 1 (JavaScript side) and Tasks 2 to 5 (Go side):

1. `encodeURIComponent` keeps `A-Z a-z 0-9 - _ . ! ~ * ' ( )` and encodes every other UTF-8 byte as `%XX` upper case. Go's `url.QueryEscape` turns a space into `+` and escapes `!*'()`; `url.PathEscape` keeps `$&+:=@`. Measured 2026-10-15: `encodeURIComponent("a b!*'()~-_.é\n`$&+,/:;=?@#")` is `a%20b!*'()~-_.%C3%A9%0A%60%24%26%2B%2C%2F%3A%3B%3D%3F%40%23`; neither Go function gives that string. `report.EncodeURIComponent` is a 12 line loop.
2. `s.slice(0, n)` and `log.slice(-200)` count UTF-16 code units and array items. `jsjson.SliceUTF16` encodes to UTF-16, cuts, decodes. A surrogate pair cut in half decodes to U+FFFD in Go; JavaScript's `encodeURIComponent` throws on the lone surrogate. Deliberate, harmless difference, recorded in the code.
3. Regular expressions. The 37 regexes in `redact.js` (7, already ported in `gconnect/redact.go` and pinned by AC-601), `report.js` (3), `llm-pack.js` (25) and `cli.js` (2) use no lookbehind, no backreference and no `u` flag, so each compiles in RE2 with `/i` as `(?i)`. Two differences stay: JavaScript `\s` matches Unicode spaces (U+00A0, U+2028, U+FEFF), RE2 `\s` is `[\t\n\f\r ]`; and RE2 `(?i)` folds `s` with U+017F and `k` with U+212A. In every regex here both differences make Go redact or match more, never less. No test; noted in `pack.go`.
4. `text.trimEnd()` removes JavaScript WhiteSpace and LineTerminator code points. Go uses `strings.TrimRightFunc(s, func(r rune) bool { return unicode.IsSpace(r) || r == '﻿' })`; the only extra rune Go trims is U+0085, which no CSV writer here emits.
5. Lines: `text.split(/\r?\n/).filter(Boolean)` is `strings.Split(text, "\n")`, then `strings.TrimSuffix(line, "\r")` once per line, then drop empty lines. `(lines[0] || '').split(',')` on an empty file gives one empty column name.
6. `Buffer.byteLength(s)` is `len(s)` for valid UTF-8, which every CSV here is (our own writer, Garmin's JSON decoded by Go).
7. `Math.round(bytes / 3)` is `math.Round` for positive numbers. `daysBetween` is a count of local calendar days, as `guard` already does.
8. `now.toLocaleDateString('sv-SE') + ' ' + now.toTimeString().slice(0, 5)` is `now.Format("2006-01-02 15:04")`. `Intl.DateTimeFormat().resolvedOptions().timeZone` is the IANA name; Go names the local zone `Local`, so `tzName()` in `cmd/garmin-export-core/summary.go` returns `$TZ` when set, else the target of the `/etc/localtime` symlink after `zoneinfo/` (on this Mac: `/var/db/timezone/zoneinfo/America/Guatemala`), else the abbreviation, and passes it as `pack.Options.TZ` (the inside may not read the environment). Only the `Exported:` line of the context uses these two values.
9. Key order in results does not matter (the UI parses JSON), and Go's `json.Marshal` escaping of `<`, `>`, `&` as `<` inside JSON strings is invisible after parsing. Files on disk are written as raw bytes, never through `json.Marshal`.
10. `JSON.stringify` drops a key whose value is `undefined`; Go writes `null`. The UI reads every such key with `?.` or `||`, so `null` is equivalent (`summary.range` when the export file has no `date_range`).

## File moves

| From | To | Why |
| --- | --- | --- |
| `electron-app/garmin/__tests__/fixtures/export-sample.json` | `tauri-app/core/internal/garmin/testdata/fixtures/export-sample.json` | Go owns the fixture; `fixturePath` const changes |
| `electron-app/garmin/__tests__/golden/*.csv` (18 files, 76 KB) | `tauri-app/core/internal/garmin/testdata/golden/` | Go owns the goldens; `goldenDir` const changes; `UPDATE_GOLDEN=1` moves to the Go test |
| `electron-app/renderer/date-range.js` | `tauri-app/ui/date-range.js` (tracked; the `.gitignore` line and the `cp` in `package.json` and `playwright.config.js` go) | The UI loads it with a `<script>` tag |
| `electron-app/renderer/__tests__/date-range.test.js` | `tauri-app/ui/__tests__/date-range.test.js` | Same `require('../date-range')` path; run by `bun test ui/__tests__` |
| `electron-app/scripts/make-fixture.js`, `compare-csv.js` | `tauri-app/scripts/` | Developer tools with no dependency; run with `bun` |
| `tauri-app/sidecar/*` | deleted | Replaced by Go |
| `tauri-app/core/cmd/fake-mfp/` | deleted | Only `sidecar/mfp-e2e.test.js` used it; `cmd/garmin-export-core/main_test.go` already runs the binary against `mfptest` in process |
| `electron-app/` (everything else) | deleted, tag `archive/electron-app` on the last commit that holds it | 3.x DMGs stay on the GitHub releases; a 3.x fix is `git checkout archive/electron-app -- electron-app` |
| `tools/ci_pipeline.py`, `tools/release.py`, `workflows/*.md` | rewritten for `tauri-app/` | They only know `electron-app/dist` and electron-builder |

New files: `internal/pack/testdata/csv_2024-02-21/` (copies of the 18 Garmin goldens and the 3 MFP goldens, 21 files, the pack's fixed input), `internal/pack/testdata/golden/` (4 Markdown files and `preview.json`), `internal/report/testdata/{input.json,golden.json,cases.json}`, `cmd/garmin-export-core/{summary,pack,report}.go`.

## Build and release changes

`tauri-app/package.json` scripts after stage 3:

```json
"core": "cd core && CGO_ENABLED=0 go build -trimpath -ldflags '-s -w' -o ../src-tauri/binaries/garmin-export-core-$(rustc -vV | awk '/host/{print $2}') ./cmd/garmin-export-core",
"core:universal": "<unchanged: two go builds and one lipo>",
"test:core": "cd core && go vet ./... && go test ./...",
"test:ui": "bun test ui/__tests__",
"dev": "tauri dev",
"build": "tauri build",
"tauri": "tauri",
"release": "tauri build --target universal-apple-darwin"
```

`tauri.conf.json`: `externalBin` lists only `binaries/garmin-export-core`; `beforeDevCommand` is `bun run core`; `beforeBuildCommand` is `bun run core:universal`, which leaves the two per architecture files and the universal file on disk, so both `tauri build` (host triple) and `tauri build --target universal-apple-darwin` find their binary and the `--config '{"build":{"beforeBuildCommand":""}}'` override of today's `release` script goes away (assumption A5, verified in Task 6). `capabilities/default.json` keeps one `shell:allow-spawn` entry.

`sidecar` and `sidecar:universal` are deleted. `electron-app/node_modules` is no longer needed for anything.

Release flow (tool `tools/release.py`, rewritten): preflight (clean tree, `gh auth status`, no tag), bump `tauri-app/package.json`, `tauri-app/src-tauri/tauri.conf.json` and `tauri-app/src-tauri/Cargo.toml` (and `Cargo.lock`, see assumption A6), FF merge into `main`, build in a git worktree (CLAUDE.md: never `tauri build` in the checkout the user runs the app from), copy the DMG to `tauri-app/dist/`, tag, `gh release create` with the one universal DMG. `tools/ci_pipeline.py` keeps its name and its stage flags with the stages `clean` (bundle outputs only, not the cargo cache), `test` (`bun run test:core`, `bun test ui/__tests__`, `bunx playwright test`, `cargo test`), `build` (worktree, `bun install`, `bun run release`), `open`.

## UI

- `platform.run(req, onEvent, bin = 'binaries/garmin-export-core')`; the explicit third argument in the `clear-cache` call, `runGarmin`, `runWithoutGarmin`, `runMfp` and `goRun` goes, and so do `GO`, `DEFAULT_ENGINE`, `engine()` and the `ge_engine` read. `runGarmin` stops reading `res.summary` and `res.pack` from the `export` answer (only Bun sent them) and always asks `summary` after the run.
- `dev-mock.js` answers `summary`, `pack` and `report` already; its `export` answer branched on the binary name, so it keeps the Go shape only and its default binary changes with the app's.
- No new Playwright spec. `e2e/engine.spec.js` loses ST-T20 (the Bun engine) and the `scenario.engine()` calls; `MockBackend.engine()` goes. The suite runs as a gate.

## Testing

Test names carry the technique ID inside a string literal (`t.Run("AC-601 ...")`), which is what `tauri-app/scripts/test-ids.js` counts; it accepts the prefixes `AC`, `BVA`, `CL`, `DT-R`, `EG`, `EP`, `ST-T`, `ST-I`. Go IDs for stage 3 start at 601 (stage 2 used 2xx and 3xx): AC-601 to AC-606, EP-601, EP-602, BVA-601 to BVA-603, ST-T601, DT-R601, EG-601 to EG-608. The Bun golden test uses AC-18 to AC-20.

- Task 1 (JavaScript, before any deletion): `tauri-app/sidecar/golden.test.js` (AC-18, AC-19, AC-20) builds the pack for `testdata/csv_2024-02-21` with `now = 2026-10-15 20:53` local and `TZ=America/Guatemala`, the report for `report/testdata/input.json`, and `redactString`/`redactUrl` for a fixed case list, and compares each with the golden files. `UPDATE_GOLDEN=1` rewrites them. This test is deleted with the sidecar, after the Go tests have passed against the same files.
- `internal/jsjson`: BVA-602 `SliceUTF16` counts code units.
- `internal/report`: AC-601 `gconnect.Redact` and `RedactURL` give the JavaScript output for every case in `cases.json`; AC-604 both URLs equal `golden.json` byte for byte; EP-602 `EncodeURIComponent` table including the measured string above; EG-604 300 lines keep `line 299`, drop `line 0`, no email, limits hold; EG-605 title cut at 120 UTF-16 units with `é`.
- `internal/pack`: AC-602 context and preview numbers equal the goldens for both languages; AC-603 `Build` writes the golden one-file packs and the files folder; DT-R601 unit table; EG-601 missing folder gives `nil`, empty folder gives `files: []`; EG-602 CRLF lines, a quoted header cell and an empty CSV; EG-603 the `logged` column by header position; BVA-601 one day and 90 days; ST-T601 a second press replaces the folder and removes the v4.0.0 root files; EP-601 `fr` and `zip` are rejected with the `cli.js` sentences.
- `internal/garmin`: AC-605 `Summarize` of the fixture gives 5 days ascending with the 5 fields as JSON and the fixture's `date_range`; EG-606 text that is not JSON is an error, a file without `daily` gives 0 days.
- `cmd/garmin-export-core`: EG-607 `tzName` from `$TZ`, never `Local`; BVA-603 the warning cut (200 items, 500 units); AC-606 `summary` (all three shapes), `pack` (both modes) and `report` through the binary, an empty `csv_*` folder gives `"files":[]`; EG-608 `pack` refuses a folder outside `$HOME`, a folder not named `csv_*` and a missing folder before touching anything, and `summary` keeps the `cli.js` sentences.
- Regression gates after Task 5, Task 6 and Task 11: `bun run test` (`test-ids.js`, `bun run test:core`, `bun test ui/__tests__`, `bunx playwright test` on Chromium and WebKit) and `cargo test`. The Task 11 run writes its output to `tauri-app/reportes-pruebas/<date>-stage3/` (gitignored), as CLAUDE.md asks.
- No test signs in to Garmin or MyFitnessPal. The checks at the app (Task 5 Step 7, Task 6 Step 6, Task 11 Step 3) export 7 days with the saved login, build packs, force one failure to see the report links, and make no sign in; they need the user at the app.

## Rollback

- After Task 5 and before Task 6, Bun still ships, unused. Going back for one command is one string in `platform.run` (`'binaries/garmin-core'` for that call).
- After Task 6, the deletions are pure deletion commits; `git revert` of the range restores Bun and the Electron tree without conflicts, because no later task edits those paths.
- The previous universal DMG stays on GitHub. Nothing stage 3 writes to disk has a new format, so a user who goes back to the stage 2 release loses nothing.

## Risks

- Byte equality holds only for what the goldens cover: the 21 CSV files of the pack fixture, one report input and 14 redaction inputs. Unit inference for a column name that no golden CSV has is pinned by DT-R601, not by a golden.
- The `Exported:` line depends on `tzName` in `cmd/`. If `/etc/localtime` is not a symlink on a user's Mac, Go prints the abbreviation (`CST`) where JavaScript printed `America/Guatemala`. Cosmetic.
- `beforeBuildCommand: bun run core:universal` for the host build is assumption A5. If `tauri build` without `--target` rejects it, keep a `core` pre step for `build` and the `--config` override for `release`, as today.
- A cold `cargo build` in a fresh worktree takes minutes. The release tool accepts `--here` for a build in place when the user confirms the app is closed.
- Deleting `electron-app/` removes `npm test` as a cross check of the Go CSV writer. After stage 3 the goldens are the only oracle; `UPDATE_GOLDEN=1` must stay a deliberate act with its own commit, as the stage 2 tech debt note asks.
- The Go `export` command checks the range with `guard.ValidateRange`, which has no "end date in the future" rule; the Bun `export` had one through `date-range.js:validateRange` (`BAD_RANGE_FUTURE`). The UI blocks future dates before sending, so this is a gap at the trust boundary, not a user facing one. Stage 3 does not fix it (out of scope); it is listed for stage 2's review.

## Assumptions to confirm

- A1 (settled): stage 2 and the gconnect extraction are on `main` (`6d56875`, released as v4.4.0 on 2026-10-16); every stage 3 task starts from there.
- A2: `summary.types` and `summary.activities` are not ported; the UI never reads them.
- A3: `pack` and `summary` errors carry no `errorCode`, as in Bun; the UI shows `error` only.
- A4: pack files are written with mode 0644 and not through a temporary file.
- A5: one `beforeBuildCommand` (`bun run core:universal`) serves both the host and the universal build.
- A6: the bump stage refreshes `Cargo.lock` offline; the command is verified on the first run (`cargo metadata --offline --format-version 1 > /dev/null` is the candidate), else `Cargo.lock` is committed after the build stage.
- A7 (settled, D1): `electron-app/` is deleted; the tag `archive/electron-app` is the archive.
- A8 (settled, D2): the tools and workflows are rewritten, with the same stage names and flags plus `--here`.
- A9 (settled, D4): stage 3 ships as 4.5.0 after the 4.4.0 that carries stage 2.
- A10 (settled, D3): Bun stays as the task runner; the README prerequisites become Bun, Rust, Go and the Xcode command line tools.
- A11: the app takes `gconnect` v0.1.2 in Task 4, which exports `IsSecretKey` (tagged 2026-10-16).
