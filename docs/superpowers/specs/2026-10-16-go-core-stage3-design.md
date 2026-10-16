# Go backend, stage 3: summary, LLM pack and bug report in Go; Bun and the Electron app leave the build

> **Amendments after stage 2 (2026-10-16), binding over the text below.**
> 1. Test IDs for stage 3 start at **601**, not 301 (stage 2 used 2xx and 3xx). Read every "3xx" ID below as the matching "6xx" ID, for example AC-301 as AC-601.
> 2. There is **no `internal/redact` package**. Redaction lives in the public library `github.com/danihrndzld/gconnect` (v0.1.0), as `gconnect.Redact` and `gconnect.RedactURL`, and `internal/report` imports it. Drop the rows that move `redact.go`. The redaction cases golden goes to `internal/report/testdata/`.
> 3. The Hexagonal rules of `docs/superpowers/specs/2026-10-16-architecture-hexagonal-gconnect.md` apply, enforced by `tauri-app/core/arch_test.go`. The new `pack`, `report` and `summary` commands are handlers in `cmd/`. The use cases take typed requests and a `Reporter`, never `map[string]any`.
> 4. Decisions taken: D1, `electron-app/` is deleted and tagged `archive/electron-app`. D2, rewrite `tools/release.py`, `tools/ci_pipeline.py` and their workflows for Tauri and Go. D3, Bun stays as the task runner. D4, this ships as 4.5.0.

Date: 2026-10-16. Status: draft for the user's review. Baseline: the tree after stage 2 Task 13 (`feat: the Go core is the default Garmin engine; docs for saved logins and source switches`).

## Goal and context

Stage 1 (`2026-10-14-go-core-mfp-design.md`) added the Go binary `garmin-export-core` and the MyFitnessPal export. Stage 2 (`2026-10-16-go-core-garmin-design.md`) moves the Garmin login, client, cache and CSV writer to Go and makes Go the default engine. After stage 2 the Bun sidecar `garmin-core` still ships for three commands: `summary` (the preview cards and the pack preview), `pack` (writes `llm-<lang>/` or `llm-<lang>-one-file/`) and `report` (the scrubbed bug report links), plus `export` as a fallback behind the `ge_engine` switch.

Stage 3 moves those three commands to Go and removes everything that only existed to run JavaScript at run time:

- `tauri-app/sidecar/` (`cli.js` 159 lines, `llm-pack.js` 152, `report.js` 34, and 4 test files), the `bun build --compile` step, and the 62 MB per architecture `garmin-core` binary.
- `electron-app/` (4,090 lines of product JavaScript, 3,363 lines of tests, `sql.js`, electron-builder) and the `npm install` that the Tauri build needs today only to reach `electron-app/node_modules/sql.js` and `electron-app/renderer/date-range.js`.
- `tools/ci_pipeline.py`, `tools/release.py` and the two workflows, which build and release the Electron app. Replacing them changes `workflows/`, which CLAUDE.md allows only with the user's permission.

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
- Go packages `internal/pack` (port of `llm-pack.js`), `internal/report` (port of `report.js`), `internal/redact` (the `redactString` port that stage 2 puts in `internal/garmin`, moved so that `report` and the Garmin client share it), and `internal/garmin/summary.go` (port of `cli.js:summarize`).
- Go commands `summary`, `pack` and `report`, with the exact result shapes the UI reads today.
- Golden files generated from the JavaScript before any deletion: `CONTEXT.es.md`, `CONTEXT.en.md`, the two one-file packs, the preview numbers, the report URLs and the redaction cases. A Bun test pins the JavaScript to them until the sidecar is deleted; a Go test pins Go to them for good.
- UI: `platform.run` defaults to `binaries/garmin-export-core`; the `ge_engine` switch and the two explicit binary arguments go away. No visible change.
- Build: `package.json` scripts, `tauri.conf.json` (`externalBin`, `beforeDevCommand`, `beforeBuildCommand`), `capabilities/default.json`, `playwright.config.js`, `.gitignore`. The universal build becomes `go build` twice, `lipo`, `tauri build --target universal-apple-darwin`.
- `electron-app/renderer/date-range.js` becomes `tauri-app/ui/date-range.js`, a tracked file, with its `node:test` suite under `tauri-app/ui/__tests__/` run by `bun test` (Bun 1.3.11 implements `node:test`; checked 2026-10-15).
- The scrubbed fixture and the 18 golden CSV files move to `tauri-app/core/internal/garmin/testdata/`; the Go golden test gets `UPDATE_GOLDEN=1`. `scripts/make-fixture.js` and `scripts/compare-csv.js` move to `tauri-app/scripts/` and run with `bun`.
- `electron-app/` is deleted, after an annotated tag on the last commit that holds it (decision D1).
- `tools/ci_pipeline.py`, `tools/release.py`, `workflows/cicd_pipeline.md` and `workflows/release.md` are rewritten for `tauri-app/` (decision D2, needs the user's permission).
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

- `internal/redact`: `String(s)`, `URL(s)`, `IsSecretKey(k)`. Moved from `internal/garmin/redact.go` (stage 2 Task 10). The Garmin client and `report` import it.
- `internal/pack`: `BuildPreview(Options) (*Preview, error)`, `Build(Options, lang, mode) (dir, file string, count int, err error)`, `Context(lang, Options, []File) string`, `TZName() string`, `Command(req) map[string]any`. `Options{CSVDir, StartDate, EndDate string; Warnings []string; Now time.Time; TZ string}`: `Now` and `TZ` are the only inputs that change between runs, so tests inject them.
- `internal/report`: `Scrub(s)`, `Build(errText string, log []Line, version, os string) (issueURL, mailtoURL string)`, `EncodeURIComponent(s)`, `Command(req)`.
- `internal/garmin/summary.go`: `Summarize(raw []byte) (*Summary, bool)`, `SummaryCommand(req)`.
- `internal/jsjson`: `SliceUTF16(s string, n int) string`, JavaScript's `s.slice(0, n)` (UTF-16 code units), used by the warning cut and the report title.
- `cmd/garmin-export-core/main.go`: three new cases, one line each.

The module keeps the stage 1 rules: Go standard library only, `CGO_ENABLED=0`, universal binary through `lipo`.

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
- Without `jsonPath`: `csvDir` must resolve inside `$HOME` and `startDate`/`endDate` must be `YYYY-MM-DD`; the result has `summary: null`.
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
3. Regular expressions. The 37 regexes in `redact.js` (7), `report.js` (3), `llm-pack.js` (25) and `cli.js` (2) use no lookbehind, no backreference and no `u` flag, so each compiles in RE2 with `/i` as `(?i)`. Two differences stay: JavaScript `\s` matches Unicode spaces (U+00A0, U+2028, U+FEFF), RE2 `\s` is `[\t\n\f\r ]`; and RE2 `(?i)` folds `s` with U+017F and `k` with U+212A. In every regex here both differences make Go redact or match more, never less. No test; noted in `redact.go`.
4. `text.trimEnd()` removes JavaScript WhiteSpace and LineTerminator code points. Go uses `strings.TrimRightFunc(s, func(r rune) bool { return unicode.IsSpace(r) || r == '﻿' })`; the only extra rune Go trims is U+0085, which no CSV writer here emits.
5. Lines: `text.split(/\r?\n/).filter(Boolean)` is `strings.Split(text, "\n")`, then `strings.TrimSuffix(line, "\r")` once per line, then drop empty lines. `(lines[0] || '').split(',')` on an empty file gives one empty column name.
6. `Buffer.byteLength(s)` is `len(s)` for valid UTF-8, which every CSV here is (our own writer, Garmin's JSON decoded by Go).
7. `Math.round(bytes / 3)` is `math.Round` for positive numbers. `daysBetween` is a count of local calendar days, as `guard` already does.
8. `now.toLocaleDateString('sv-SE') + ' ' + now.toTimeString().slice(0, 5)` is `now.Format("2006-01-02 15:04")`. `Intl.DateTimeFormat().resolvedOptions().timeZone` is the IANA name; Go names the local zone `Local`, so `pack.TZName` returns `$TZ` when set, else the target of the `/etc/localtime` symlink after `zoneinfo/` (on this Mac: `/var/db/timezone/zoneinfo/America/Guatemala`), else the abbreviation. Only the `Exported:` line of the context uses these two values.
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
| `tauri-app/core/internal/garmin/redact.go` (+ test) | `tauri-app/core/internal/redact/` | Shared by the Garmin client and `report` |
| `tauri-app/sidecar/*` | deleted | Replaced by Go |
| `tauri-app/core/cmd/fake-mfp/` | deleted | Only `sidecar/mfp-e2e.test.js` used it; `cmd/garmin-export-core/main_test.go` already runs the binary against `mfptest` in process |
| `electron-app/` (everything else) | deleted, tag `archive/electron-app` on the last commit that holds it | 3.x DMGs stay on the GitHub releases; a 3.x fix is `git checkout archive/electron-app -- electron-app` |
| `tools/ci_pipeline.py`, `tools/release.py`, `workflows/*.md` | rewritten for `tauri-app/` | They only know `electron-app/dist` and electron-builder |

New files: `internal/pack/testdata/csv_2024-02-21/` (copies of the 18 Garmin goldens and the 3 MFP goldens, 21 files, the pack's fixed input), `internal/pack/testdata/golden/` (4 Markdown files and `preview.json`), `internal/report/testdata/{input.json,golden.json}`, `internal/redact/testdata/cases.json`.

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

- `platform.run(req, onEvent, bin = 'binaries/garmin-export-core')`; the explicit third argument in `refreshGarmin`, `startExport` and `runMfp` goes, and so do `DEFAULT_ENGINE`, `engine()` and the `ge_engine` read.
- `dev-mock.js` is unchanged: it answers `summary`, `pack` and `report` already.
- No visible change, so no new Playwright spec. The suite runs as a gate.

## Testing

Test names carry the technique ID. Go IDs for stage 3 start at 301: AC-301.., EP-301.., BVA-301.., ST-301.., DT-301.., EG-301..

- Task 1 (JavaScript, before any deletion): `tauri-app/sidecar/golden.test.js` builds the pack for `testdata/csv_2024-02-21` with `now = 2026-10-15 20:53` local and `TZ=America/Guatemala`, the report for `report/testdata/input.json`, and `redactString`/`redactUrl` for a fixed case list, and compares each with the golden files. `UPDATE_GOLDEN=1` rewrites them. This test is deleted with the sidecar, after the Go tests have passed against the same files.
- `internal/redact`: AC-301, every case in `cases.json`.
- `internal/pack`: AC-302 context and preview numbers equal the goldens for both languages; AC-303 `Build` writes the golden one-file packs and the files folder; DT-301 unit table; EG-301 missing folder gives `nil`, empty folder gives `files: []`; EG-302 CRLF lines, a quoted header cell and an empty CSV; EG-303 the `logged` column by header position; EG-307 `TZName` from `$TZ`; BVA-301 one day and 90 days; ST-301 a second press replaces the folder and removes the v4.0.0 root files; EP-301 `fr` and `zip` are rejected with the `cli.js` sentences.
- `internal/report`: AC-304 both URLs equal `golden.json` byte for byte; EP-302 `EncodeURIComponent` table including the measured string above; EG-304 300 lines keep `line 299`, drop `line 0`, no email, limits hold; EG-305 title cut at 120 UTF-16 units with `é`.
- `internal/garmin`: AC-305 `Summarize` of the fixture gives 5 days ascending with the 5 fields and the fixture's `date_range`; EG-306 a non JSON file gives "Could not read export."; `csvDir` only gives `summary: null`.
- `cmd/garmin-export-core/main_test.go`: AC-306 `summary`, `pack` (both modes) and `report` through the binary; EG-308 `pack` refuses a folder outside `$HOME` and a folder not named `csv_*`; an empty `csv_*` folder gives `"files":[]`.
- Regression gates after Task 5, Task 6 and Task 11: `bun run test:core`, `bun test ui/__tests__`, `bunx playwright test` (Chromium and WebKit), `cargo test`. The Task 11 run writes its output to `tauri-app/reportes-pruebas/<date>-stage3/` (gitignored), as CLAUDE.md asks.
- No test signs in to Garmin or MyFitnessPal. The one live check (Task 11) exports 7 days with the saved login, builds both packs, forces one failure to see the report links, and makes no sign in.

## Rollback

- After Task 5 and before Task 6, Bun still ships, unused. Going back for one command is one string in `platform.run` (`'binaries/garmin-core'` for that call).
- After Task 6, the deletions are pure deletion commits; `git revert` of the range restores Bun and the Electron tree without conflicts, because no later task edits those paths.
- The previous universal DMG stays on GitHub. Nothing stage 3 writes to disk has a new format, so a user who goes back to the stage 2 release loses nothing.

## Risks

- Byte equality holds only for what the goldens cover: the 21 CSV files of the pack fixture, one report input and 14 redaction inputs. Unit inference for a column name that no golden CSV has is pinned by DT-301, not by a golden.
- The `Exported:` line depends on `TZName`. If `/etc/localtime` is not a symlink on a user's Mac, Go prints the abbreviation (`CST`) where JavaScript printed `America/Guatemala`. Cosmetic.
- `beforeBuildCommand: bun run core:universal` for the host build is assumption A5. If `tauri build` without `--target` rejects it, keep a `core` pre step for `build` and the `--config` override for `release`, as today.
- A cold `cargo build` in a fresh worktree takes minutes. The release tool accepts `--here` for a build in place when the user confirms the app is closed.
- Deleting `electron-app/` removes `npm test` as a cross check of the Go CSV writer. After stage 3 the goldens are the only oracle; `UPDATE_GOLDEN=1` must stay a deliberate act with its own commit, as the stage 2 tech debt note asks.
- The Go `export` command checks the range with `guard.ValidateRange`, which has no "end date in the future" rule; the Bun `export` had one through `date-range.js:validateRange` (`BAD_RANGE_FUTURE`). The UI blocks future dates before sending, so this is a gap at the trust boundary, not a user facing one. Stage 3 does not fix it (out of scope); it is listed for stage 2's review.

## Assumptions to confirm

- A1: stage 2 Task 13 is merged before stage 3 Task 2 starts. Only Task 1 (golden freeze) runs earlier, on its own branch, because it adds files and one Bun test that stage 2 does not touch.
- A2: `summary.types` and `summary.activities` are not ported; the UI never reads them.
- A3: `pack` and `summary` errors carry no `errorCode`, as in Bun; the UI shows `error` only.
- A4: pack files are written with mode 0644 and not through a temporary file.
- A5: one `beforeBuildCommand` (`bun run core:universal`) serves both the host and the universal build.
- A6: the bump stage refreshes `Cargo.lock` offline; the command is verified on the first run (`cargo metadata --offline --format-version 1 > /dev/null` is the candidate), else `Cargo.lock` is committed after the build stage.
- A7: `electron-app/` is deleted (not archived in a branch or kept out of the build); the tag `archive/electron-app` is the archive.
- A8: the tools and workflows are rewritten (not deleted), with the same stage names, under the user's permission.
- A9: stage 3 ships as 4.5.0 after a 4.4.0 that carries stage 2; the version numbers are the user's call.
- A10: Bun stays as the task runner; the README prerequisites become Bun, Rust, Go and the Xcode command line tools.
