# Stage 3 (Bun to Go): advice

Date: 2026-10-16. Read with `2026-10-16-go-core-stage3-design.md` (spec) and `2026-10-16-go-core-stage3.md` (plan). Every number below was measured on 2026-10-15 in this repository unless it says "estimate".

## Verdict

Do stage 3, as its own branch, right after stage 2 Task 13 is merged. Do not fold it into stage 2's tail.

Why it is worth it:

- The Bun sidecar is 61.9 MB per architecture (`garmin-core-aarch64-apple-darwin`, built 2026-10-13) and after stage 2 it serves 345 lines of JavaScript (`cli.js` 159, `llm-pack.js` 152, `report.js` 34). The Go binary that replaces it is 6.7 MB arm64, 7.2 MB amd64, 13.9 MB universal, and it already ships in v4.3.0.
- The universal DMG is 62.1 MB (v4.3.0) and 56.2 MB (v4.0.0). Those two releases held about 150 MB of Mach-O slices (two Bun slices, two Tauri slices, two Go slices in 4.3.0) at a 2.7 to 1 compression. After stage 3 the bundle holds about 42 MB of slices, so the DMG lands around 15 to 20 MB (estimate; the plan measures it in Task 6).
- Building the Tauri app today needs `electron-app/node_modules` (an `npm install` that downloads Electron 33 and electron-builder) only to reach `sql.js/dist/sql-wasm.wasm` and `renderer/date-range.js`. After stage 3 the prerequisites are Bun, Rust, Go and the Xcode tools.
- The repository loses 4,090 lines of Electron product JavaScript, 345 lines of sidecar, 3,363 lines of JavaScript tests, the JavaScript fake Garmin server (the Go one stays, which closes the "two fakes" debt item) and the two engine debt items that stage 2's tech debt note lists with "Exit: stage 3". It gains about 450 lines of Go in four packages plus about 400 lines of tests. The Go core is 3,777 lines today (stage 1 plus the first stage 2 tasks).
- Start time: Go answers in 10 ms warm (260 ms with a cold file cache); the Bun sidecar in 30 ms warm (860 ms cold). The UI already pads the pack buttons with `MIN_BUILD_MS = 700`, so no visible change, but every `summary` call at boot and after MyFitnessPal gets faster.

Why not inside stage 2:

- Stage 2 has its own gate (18 goldens through Go, the live compare, the default flip) and three agents are editing `main.go`, `app.js` and `cli.js` right now. Stage 3 edits the same three files. Two plans on one branch means merge conflicts and a blurred gate.
- Stage 2 ships saved logins and the source switches, both user visible, and needs its own live check. Let it ship as 4.4.0; stage 3 follows as 4.5.0 with "same files, 40 MB smaller".
- One exception: Task 1 of stage 3 (freeze the JavaScript outputs) only adds files under `tauri-app/core/internal/*/testdata/` and one Bun test, none of which stage 2 touches. It can run today on its own branch and be rebased.

## Risks, with the mitigation and where the plan pins it

| Risk | What can go wrong | Mitigation | Pinned |
| --- | --- | --- | --- |
| Byte equal pack text | `CONTEXT.<lang>.md` has 2 nondeterministic values: `Exported: 2026-10-15 20:53 (America/Guatemala)`. Everything else is string work. | `now` and the zone name are injected (`pack.Options.Now`, `.TZ`); the JavaScript freeze runs with `now` fixed and `TZ=America/Guatemala`. | Task 1, Task 3 AC-302 |
| Zone name | JavaScript used `Intl...timeZone`; Go names the local zone `Local`. | `pack.TZName`: `$TZ`, else the `/etc/localtime` symlink after `zoneinfo/` (`/var/db/timezone/zoneinfo/America/Guatemala` on this Mac), else `CST`. Cosmetic if it falls through. | Task 3 EG-307 |
| Token estimate | `Math.round(bytes / 3)`. | `math.Round` agrees for positive numbers; `Buffer.byteLength` is `len()` for valid UTF-8, which our CSV writer always produces. | Task 3 AC-302 (`tokens`, `bytes` in `preview.json`) |
| Unicode and Markdown | `→`, `días`, `°C`, `·` are UTF-8 in both; `trimEnd` differs only on U+0085, which no CSV holds. | Copy the `T` and `DESCRIBE` strings verbatim; `TrimRightFunc(unicode.IsSpace or U+FEFF)`. | Task 3 AC-302, AC-303 |
| Regex parity | 37 regexes across `redact.js` (7), `report.js` (3), `llm-pack.js` (25), `cli.js` (2). None uses lookbehind, backreferences or the `u` flag, so RE2 compiles all of them. Two gaps: JavaScript `\s` matches U+00A0, U+2028, U+FEFF and RE2 does not; RE2 `(?i)` folds `s` with U+017F and `k` with U+212A. | In every regex here both gaps make Go redact or match more, never less. Recorded in `redact.go`; the frozen 28 cases pin the common paths. | Task 2 AC-301 |
| URL encoding | `encodeURIComponent` keeps `-_.!~*'()` and writes a space as `%20`. Measured: Go `url.QueryEscape` gives `a+b%21%2A%27%28%29...` and `url.PathEscape` keeps `$&+:=@`, so neither matches. | A 12 line `EncodeURIComponent`. The report golden has 300 lines with `·`, `é`-free text and every reserved character. | Task 4 EP-302, AC-304 |
| UTF-16 slices | `title.slice(0,120)`, `String(x).slice(0,500)` count UTF-16 units. | `jsjson.SliceUTF16`. A pair cut in half decodes to U+FFFD where JavaScript's `encodeURIComponent` would throw. Harmless, documented. | Task 2, Task 4 EG-305 |
| UI contract | `pack.files` must be `[]` not `null` (the UI calls `.length`, `.forEach`); `pack` must be JSON `null` not `{}` (`pack = s.pack \|\| pack`); no payload key named `kind` (`Emitter.Result` overwrites it, stage 2 already renamed `kind` to `loginKind`). | Non nil slices; nil pointers marshal to `null`; no `kind` key. | Task 3 EG-301, Task 5 AC-306 |
| Three `summary` request shapes | After stage 2 the UI sends `{jsonPath, csvDir, warnings}`, `{jsonPath, csvDir}` and `{csvDir, startDate, endDate}`. | One command handles all three. | Task 5 AC-306, EG-306 |
| `pack` deletes folders | `Build` removes and recreates `llm-*` under `csvDir`. | `guard.InsideHome`, directory check and the `csv_` name check run before any deletion, as `cli.js` does. | Task 5 EG-308 |
| Losing the Electron app | 3.x users cannot get fixes from `main`. | The 3.x DMGs stay on GitHub releases; the tag `archive/electron-app` marks the last commit with the folder; the 3.x update check is already broken (README, v4 note), so nobody is pulled to a 4.x build by accident. | Task 8 |
| Gatekeeper and signing | `signingIdentity: "-"` (ad hoc). | Unchanged: v4.3.0 already ships this exact universal Go binary (Go's linker signs darwin binaries, `lipo` keeps the slices, Tauri re-signs the bundle). The plan runs `codesign --verify --deep --strict` on the worktree build. | Task 6 Step 5 |
| Universal build config | One `beforeBuildCommand: bun run core:universal` for both the host build and `--target universal-apple-darwin` is an assumption (A5). | `core:universal` leaves the per architecture files on disk, so both targets find their binary; the plan verifies and keeps the `--config` override if Tauri objects. | Task 6 Step 5 |
| Cold worktree builds | A fresh worktree compiles Tauri from zero (minutes). | `--here` for a build in place when the app is closed; or a shared `CARGO_TARGET_DIR` (not in the plan; add if the wait hurts). | Task 9 |
| Goldens become the only oracle | `npm test` (JavaScript CSV writer) disappears with `electron-app/`. | `UPDATE_GOLDEN=1` moves to the Go test and stays a deliberate act with its own commit, as the stage 2 debt note demands. | Task 7 |

## Do not port (dead or redundant)

- `cli.js:summarize` fields `types` and `activities`: `app.js:renderSummary` reads `days` only and the MyFitnessPal step reads `range` only (spec assumption A2).
- `cli.js:runExport`'s `email`/`password` pass through, `clearCache` with `sql.js`, `resolveDataDir`: gone with Bun; Go has `guard`.
- `date-range.js:formatSpan`, `isValidDateString`, `toLocalYmd` exports: `app.js` uses `validateRange` and `defaultCustomRange` only. Keep the file as it is (it moves, it is not edited beyond its header), because trimming it buys nothing.
- `electron-app/scripts/download-fonts.js`, `main.js`, `preload.js`, `renderer/renderer.js`, `BUILD.md`, `Remove Quarantine.command`: the Electron app.
- `tauri-app/core/cmd/fake-mfp`: only `sidecar/mfp-e2e.test.js` used it; `cmd/garmin-export-core/main_test.go` already drives the binary against `mfptest` in process.
- `ge_engine` and `DEFAULT_ENGINE` in `app.js`: one engine.
- The `--config '{"build":{"beforeBuildCommand":""}}'` override in the `release` script, if A5 holds.
- A new `errorCode` for `pack` and `summary` failures: the UI shows `error` only, as with Bun.

## Test strategy

1. Freeze first (Task 1). A Bun test, `sidecar/golden.test.js`, runs the JavaScript once with fixed inputs and writes: `CONTEXT.es.md`, `CONTEXT.en.md`, the two one-file packs, `preview.json` (days, files, bytes, tokens), `report/testdata/input.json` and `golden.json` (both URLs), `redact/testdata/cases.json` (28 cases). The same test, without `UPDATE_GOLDEN`, pins the JavaScript to those files until the sidecar is deleted.
2. Go pins itself to the same files (Tasks 2 to 5): AC-301 redaction, AC-302 and AC-303 pack, AC-304 report, AC-305 summary, AC-306 the three commands through the real binary over stdin, plus the partitions and edge cases (EP-301, EP-302, BVA-301, ST-301, DT-301, EG-301 to EG-308). IDs start at 301, as agreed.
3. Both engines run side by side after Task 5 and before Task 6: the goldens are the cross check in CI, and Task 5 Step 7 is the manual one on a real folder (`diff` after dropping the `Exported:` line).
4. Gates: `bun run test:core`, `bun test ui/__tests__` (the moved `date-range.test.js`, Bun 1.3.11 runs `node:test`), `bunx playwright test` on Chromium and WebKit, `cargo test`, after Tasks 5, 6 and 11. Task 6 Step 7 runs the gate with `electron-app/node_modules` moved away to prove nothing reaches into it.
5. No new Playwright spec: the UI changes one default string. The suite stays the gate under the Page Object Model rules.
6. One live check (Task 11 Step 3): export 7 days with the saved login, both packs, one forced failure to see the report links. No sign in anywhere.

## Rollback

- After Task 5, before Task 6: Bun still ships. One command back to Bun is one string in `platform.run` (`'binaries/garmin-core'` on that call).
- After Task 6 and 8: the deletions are pure deletion commits that no later task edits, so `git revert <range>` restores Bun and `electron-app/` without conflicts.
- Releases: the stage 2 DMG stays on GitHub; stage 3 writes no new file format (`csv_*`, `llm-*`, `tokens.json`, `mfp_session.json`, the two caches), so going back loses no data.

## Gaps found on the way (not stage 3 work)

- Go `export` validates ranges with `guard.ValidateRange`, which has no "end date after today" rule; Bun's `export` had `BAD_RANGE_FUTURE` through `date-range.js`. The UI blocks future dates before sending, so it is a trust boundary gap, not a user facing one. Worth a 5 line fix in stage 2's review.
- `clear-cache` keeps deleting `garmin_cache.db`; users of 4.3.0 still have the file. Keep it for two releases, then drop the line.
- `scanCsv` splits the header on commas without quote handling. No Garmin or MyFitnessPal column name holds a comma today; if one appears, the pack lists a broken column name. Port as is (byte parity), note it.

## Decisions for the user

- D1: `electron-app/` is deleted with a tag `archive/electron-app` (recommended), or kept out of the build, or moved to a branch.
- D2: permission to rewrite `tools/ci_pipeline.py`, `tools/release.py`, `workflows/cicd_pipeline.md`, `workflows/release.md` for the Tauri app (CLAUDE.md requires it). Alternative: delete the tools and keep releasing by hand as v4.0 to v4.3 were.
- D3: Bun stays as the task runner for `@tauri-apps/cli`, Playwright and `bun test` (recommended, zero cost); the alternative is `cargo tauri` plus `npx`.
- D4: versions: stage 2 as 4.4.0, stage 3 as 4.5.0 (recommended), or both in one release.
- D5: run Task 1 now, in parallel with stage 2, on its own branch (recommended), or wait for the stage 2 merge.
- D6: the Task 5 and Task 11 live checks use the saved login and make no sign in; confirm that running them does not count against the day's Garmin sign ins (it should not; a refresh is not a sign in).
