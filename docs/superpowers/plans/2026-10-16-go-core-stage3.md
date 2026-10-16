# Go backend stage 3: summary, LLM pack and bug report in Go; Bun and the Electron app leave the build, implementation plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Move the `summary`, `pack` and `report` commands from the Bun sidecar to the Go binary `garmin-export-core` with byte equal output, then remove the Bun sidecar, `electron-app/` and the Electron build tools from the repository, so that the app ships one 14 MB Go binary next to the Tauri shell and the universal build is `go build`, `lipo` and `tauri build`. Ships as 4.5.0.

**Architecture:** New Go packages `internal/pack` (port of `tauri-app/sidecar/llm-pack.js`) and `internal/report` (port of `sidecar/report.js`, redaction from the library `github.com/danihrndzld/gconnect`), plus `internal/garmin/summary.go` (port of `cli.js:summarize`). Three new handlers in `cmd/garmin-export-core/` (`summary.go`, `pack.go`, `report.go`) decode the wire request, run `guard`, resolve the time zone name, and call the use cases with typed requests, as `garmin.go` and `mfp.go` do today. The outputs of the JavaScript are frozen into golden files before anything is deleted, a Bun test pins the JavaScript to them until the sidecar goes, and Go tests pin Go to them for good. The UI changes one default string and loses the engine switch. Every deletion is its own commit, so `git revert` is the rollback.

**Tech Stack:** Go 1.27 toolchain with `go 1.24` in `go.mod`, standard library plus `github.com/danihrndzld/gconnect` v0.1.0, Bun 1.3.11 as the task runner and for `bun test` (it runs `node:test` files; checked 2026-10-16, 14 pass), Playwright for the UI gate, Tauri 2 CLI, `uv` for the two Python tools.

**Spec:** `docs/superpowers/specs/2026-10-16-go-core-stage3-design.md`

**Baseline:** `main` at `6d56875` (after the v4.4.0 release of 2026-10-16, which carries stage 2 and the gconnect extraction). The branch `feat/go-core-stage3` already exists in the worktree `.worktrees/go-core-stage3` at that commit. Every path, function and command below was read in that tree on 2026-10-16; the Go tests pass there (`go vet ./... && go test ./...`, 7.8 s).

**Decisions taken (2026-10-16, not reopened here):** D1 `electron-app/` is deleted and the last commit that holds it is tagged `archive/electron-app`. D2 `tools/release.py`, `tools/ci_pipeline.py` and both workflows are rewritten for Tauri and Go (the user gave the permission CLAUDE.md asks for). D3 Bun stays as the task runner. D4 this ships as 4.5.0. The user also authorized publishing the release; if the permission classifier blocks `gh release create`, the user runs the printed command by hand.

## Global Constraints

- Module `github.com/danihrndzld/garmin-health-exporter/tauri-app/core`, `go 1.24` in `go.mod`, exactly one `require` (`github.com/danihrndzld/gconnect v0.1.0`; `arch_test.go` allows nothing else), no `replace`, `CGO_ENABLED=0`, universal binary through `lipo`.
- The Hexagonal rules of `docs/superpowers/specs/2026-10-16-architecture-hexagonal-gconnect.md` hold and `tauri-app/core/arch_test.go` enforces them: only `cmd/` reads the environment, `os.Args`, stdin or stdout (so the time zone name is resolved in `cmd/`, never in `internal/pack`); only `cmd/` imports `internal/protocol`; no exported function or type of `internal/garmin`, `internal/mfp`, `internal/pack` or `internal/report` holds `map[string]any` (Task 2 adds the two new packages to `wirePackages`); `internal/garmin` and `internal/mfp` never import each other; nothing outside `cmd/` prints to the standard streams. Use cases take typed requests and return typed results; `cmd/` turns `(T, error)` into the result line.
- Every path in a request resolves inside `$HOME` through `guard.InsideHome` in `cmd/` before anything is read or written. `pack` also requires an existing directory named `csv_*`. `report` never touches the disk.
- Stage 3 copies bytes. No wording, unit, column or URL changes. The golden files in `internal/pack/testdata/golden`, `internal/report/testdata/golden.json` and `internal/report/testdata/cases.json` are written once by the JavaScript in Task 1 and never by hand. The Go golden test for the Garmin CSV files gains `UPDATE_GOLDEN=1` in Task 7; no task in this plan runs it.
- No live sign in to Garmin or MyFitnessPal. The checks that need the user at the app (Task 5 Step 7, Task 6 Step 6, Task 11 Step 3) use the saved login and make no sign in. Never run live login experiments in a loop: Garmin answered 429 after 4 sign ins in 40 minutes on 2026-10-14.
- Never `tauri build` in the checkout the user runs the app from; builds happen in a `git worktree` (Task 6, Task 9, Task 11).
- Results never use a payload key named `kind` (`protocol.Emitter.Result` overwrites it).
- `pack.files` is always a JSON array; `pack` is JSON `null` when there is no pack; `summary` is JSON `null` without `jsonPath`. `pack` and `summary` failures carry `error` only, no `errorCode`, as in Bun.
- Test IDs. `tauri-app/scripts/test-ids.js` runs in `bun run test` and exits 1 on a duplicate ID or an ID that is in no test name; it accepts the prefixes `AC`, `BVA`, `CL`, `DT-R`, `EG`, `EP`, `ST-T`, `ST-I` only, and it counts a Go ID only inside a string literal (`t.Run("AC-601 ...")`, as stage 2 does), never in a function name. Stage 3 Go IDs start at 601: AC-601 to AC-606, EP-601, EP-602, BVA-601 to BVA-603, ST-T601, DT-R601, EG-601 to EG-608 (checked free on 2026-10-16: no 6xx ID exists). The Bun golden test of Task 1 uses AC-18, AC-19 and AC-20 (AC-04 to AC-17 are taken by the sidecar tests). Never name a Bun ID in a Go comment: after Task 6 deletes the Bun test, the script would report it as "not in a test name". Playwright: one page object per component in `e2e/pages/`, mock switches only in `e2e/support/mock-backend.js`, wiring only in `e2e/fixtures.js`, assertions only in specs. Stage 3 removes the engine switch, so `e2e/engine.spec.js` loses ST-T20 (the Bun engine) and `MockBackend.engine()` goes; no other spec changes. `bun scripts/test-ids.js` must exit 0 after every task.
- Commits use the author `Daniel Hernández <danisnowman@gmail.com>` and carry no AI attribution.
- User facing text follows the plain writing rules: complete sentences, no dashes, no jargon.

## Review Focus

1. `CONTEXT.es.md`, `CONTEXT.en.md` and the two one-file packs must be byte equal to the files the JavaScript wrote, including the `→` arrows, `días`, `°C` and the trailing newline. Pinned in Task 1 (JavaScript, AC-18) and Task 3 (Go, AC-602, AC-603).
2. The issue and mailto URLs must be byte equal to the JavaScript output: the encoder must keep `!*'()~` and write a space as `%20`, and the title must be cut at 120 UTF-16 units. Pinned in Task 4 (AC-604, EP-602, EG-605).
3. An empty `csv_*` folder must give `"files":[]`, never `"files":null`, and a missing folder must give `"pack":null`, never `{}`. Pinned in Task 3 (EG-601) and Task 5 (AC-606).
4. `summary` must accept all three request shapes the UI sends: with `jsonPath` and `warnings`, with `jsonPath` only, and with `csvDir` plus dates only, with the `cli.js` sentences on failure (`csvDir must be a folder within your home directory.`, `jsonPath must be a .json file within your home directory.`, `Could not read export.`). A `csvDir` outside `$HOME` next to a valid `jsonPath` gives `pack: null`, not an error. Pinned in Task 5 (AC-606, EG-608).
5. `pack` must refuse a folder outside `$HOME`, a folder that is not a directory, and a folder whose name does not start with `csv_`, before it deletes anything, because `Build` removes `llm-*` subfolders. Pinned in Task 5 (EG-608).
6. `gconnect.Redact` and `gconnect.RedactURL` must give the JavaScript output for every case in `cases.json`, including a JSON pair with a secret key, a Bearer value in mixed case and a JWT in the middle of a sentence. Pinned in Task 4 (AC-601). A mismatch is a library fix (new gconnect tag, `go get`), never an app side patch.
7. After Task 6 the repository must build and test with `electron-app/node_modules` absent. Pinned in Task 6 Step 7.
8. `arch_test.go` must stay green after every Go task: no `os.Getenv` in `internal/pack` (the zone name comes from `cmd/`), no `map[string]any` in the exported surface of `internal/garmin/summary.go`, `internal/pack` or `internal/report`.

---

## File structure

```
tauri-app/core/
  arch_test.go                                  + internal/pack, internal/report in wirePackages; two fixture rows (Task 2)
  internal/jsjson/format.go                     + SliceUTF16 (Task 2)
  internal/jsjson/jsjson_test.go                + BVA-602
  internal/pack/pack.go                         Options, File, Lang, Preview, Result, Langs, BuildPreview, Build, Context (Task 3)
  internal/pack/pack_test.go                    AC-602, AC-603, ST-T601, EP-601, EG-601..603, DT-R601, BVA-601
  internal/pack/testdata/csv_2024-02-21/*.csv   21 files: the 18 Garmin goldens and the 3 MFP goldens (Task 1)
  internal/pack/testdata/golden/CONTEXT.es.md   frozen from llm-pack.js (Task 1)
  internal/pack/testdata/golden/CONTEXT.en.md
  internal/pack/testdata/golden/garmin_2024-02-17_2024-02-21.es.md
  internal/pack/testdata/golden/garmin_2024-02-17_2024-02-21.en.md
  internal/pack/testdata/golden/preview.json
  internal/report/report.go                     Line, Result, Scrub, Build, EncodeURIComponent, Repo, Email (Task 4)
  internal/report/report_test.go                AC-604, EP-602, EG-604, EG-605
  internal/report/redact_test.go                AC-601 (gconnect pinned to cases.json)
  internal/report/testdata/cases.json           frozen from electron-app/util/redact.js (Task 1)
  internal/report/testdata/input.json           frozen input (Task 1)
  internal/report/testdata/golden.json          frozen URLs (Task 1)
  internal/garmin/summary.go                    Summary, Day, Summarize, (*Summary).Dates (Task 5)
  internal/garmin/summary_test.go               AC-605, EG-606
  internal/garmin/testdata/fixtures/export-sample.json   moved in Task 7
  internal/garmin/testdata/golden/*.csv                  moved in Task 7
  cmd/garmin-export-core/main.go                + summary, pack, report in the command map (Task 5)
  cmd/garmin-export-core/summary.go             summary handler, warnings, tzName, packOptions (Task 5)
  cmd/garmin-export-core/pack.go                buildPack handler (Task 5)
  cmd/garmin-export-core/report.go              buildReport handler (Task 5)
  cmd/garmin-export-core/stage3_test.go         EG-607, BVA-603 (Task 5)
  cmd/garmin-export-core/main_test.go           + AC-606, EG-608 (Task 5); fixture paths (Task 7)
tauri-app/sidecar/golden.test.js                Task 1 only; deleted with the folder in Task 6
tauri-app/ui/date-range.js                      tracked from Task 6
tauri-app/ui/__tests__/date-range.test.js       moved in Task 6
tauri-app/scripts/make-fixture.js               moved in Task 7
tauri-app/scripts/compare-csv.js                moved in Task 7
tools/ci_pipeline.py, tools/release.py          rewritten in Task 9
workflows/cicd_pipeline.md, workflows/release.md rewritten in Task 9
```

Modified: `tauri-app/ui/app.js` (default binary, no engine switch), `tauri-app/ui/dev-mock.js` (default binary, one `export` answer), `tauri-app/e2e/engine.spec.js`, `tauri-app/e2e/support/mock-backend.js`, `tauri-app/package.json`, `tauri-app/src-tauri/tauri.conf.json`, `tauri-app/src-tauri/capabilities/default.json`, `tauri-app/playwright.config.js`, `tauri-app/.gitignore`, `.gitignore`, `internal/garmin/csv_test.go` (paths, `UPDATE_GOLDEN`), `internal/garmin/garmintest/fixture_test.go` (path), `README.md`, `CLAUDE.md`, three header comments (`main.go`, `protocol.go`, `main.rs`).

Deleted: `tauri-app/sidecar/` (Task 6), `tauri-app/core/cmd/fake-mfp/` (Task 6), `electron-app/` (Task 8).

## Shippable increments

| After task | What the user gets | In the bundle |
| --- | --- | --- |
| 1 | Nothing visible; the JavaScript outputs are frozen and pinned | Bun + Go |
| 5 | Every command runs in Go; Bun ships unused; rollback is one string per command | Bun + Go |
| 6 | DMG without Bun (about 15 to 20 MB, measured in Step 5) | Go only |
| 8 | Repository without `electron-app/`; tests need no `npm install` | Go only |
| 11 | Release 4.5.0 through the rewritten tool | Go only |

---

### Task 0: The worktree and the rollback point

**Files:** none.

- [ ] **Step 1: Confirm the baseline.** Run: `cd .worktrees/go-core-stage3 && git log --oneline -1 && git status --short` Expected: `6d56875 docs: v4.4.0 screenshots in the README, with the four accent colors`, a clean tree. Every later command runs inside this worktree.

- [ ] **Step 2: Install.** The worktree has no `node_modules` (checked 2026-10-16). Run: `(cd electron-app && npm install) && (cd tauri-app && bun install && bunx playwright install chromium webkit)`. Until Task 6 the Bun sidecar tests still import `sql.js` from `electron-app/node_modules`.

- [ ] **Step 3: Green at the start.** Run: `cd tauri-app && bun run test && (cd ../electron-app && npm test)`. Expected: `510 IDs in 32 files, 0 duplicates, 0 not in a test name`, every Go package `ok`, Bun 0 fail, Playwright green on chromium and webkit, `npm test` green. If anything fails here, stop; stage 3 must not start on a red tree.

---

### Task 1: Freeze the JavaScript outputs into golden files

This task runs the JavaScript once, writes what it produced, and pins the JavaScript to those files. It needs no `node_modules`: `llm-pack.js`, `report.js` and `electron-app/util/redact.js` import nothing.

**Files:**
- Create: `tauri-app/sidecar/golden.test.js`, `tauri-app/core/internal/pack/testdata/csv_2024-02-21/` (21 copies), `tauri-app/core/internal/pack/testdata/golden/` (5 files), `tauri-app/core/internal/report/testdata/input.json`, `tauri-app/core/internal/report/testdata/golden.json`, `tauri-app/core/internal/report/testdata/cases.json`

- [ ] **Step 1: Copy the pack input.** The pack goldens must not move when the Garmin goldens are rebuilt, so the 21 CSV files are copied, not referenced.

```bash
mkdir -p tauri-app/core/internal/pack/testdata/csv_2024-02-21
cp electron-app/garmin/__tests__/golden/*.csv tauri-app/core/internal/pack/testdata/csv_2024-02-21/
cp tauri-app/core/internal/mfp/testdata/golden/*.csv tauri-app/core/internal/pack/testdata/csv_2024-02-21/
ls tauri-app/core/internal/pack/testdata/csv_2024-02-21 | wc -l   # 21
```

- [ ] **Step 2: Write the Bun golden test.** `tauri-app/sidecar/golden.test.js`:

```js
// Pins the JavaScript pack, report and redaction outputs to golden files that the Go port
// reproduces byte for byte (stage 3). Regenerate only on purpose:
//   TZ=America/Guatemala UPDATE_GOLDEN=1 bun test sidecar/golden.test.js
// The time zone is fixed because the context's "Exported" line prints the IANA zone name.
import { test, expect } from 'bun:test';
import fs from 'node:fs';
import os from 'node:os';
import path from 'node:path';
import { createRequire } from 'node:module';
import { previewPack, buildPack } from './llm-pack.js';
import { buildReport } from './report.js';

const require = createRequire(import.meta.url);
const { redactString, redactUrl } = require('../../electron-app/util/redact.js');

const CORE = path.resolve(import.meta.dir, '../core/internal');
const PACK = path.join(CORE, 'pack/testdata');
const REPORT = path.join(CORE, 'report/testdata');
const UPDATE = !!process.env.UPDATE_GOLDEN;

function pin(file, text) {
  if (UPDATE) { fs.mkdirSync(path.dirname(file), { recursive: true }); fs.writeFileSync(file, text); }
  expect(fs.readFileSync(file, 'utf8')).toBe(text);
}
const json = (v) => JSON.stringify(v, null, 2) + '\n';

// The user's Mac is in America/Guatemala (/etc/localtime -> /var/db/timezone/zoneinfo/America/Guatemala,
// checked 2026-10-16), so the file also passes inside a plain `bun test sidecar`. On another zone run it
// with TZ=America/Guatemala; the gates in Tasks 5 and 6 say so.
test('the zone is fixed, so the Exported line is reproducible', () => {
  expect(Intl.DateTimeFormat().resolvedOptions().timeZone).toBe('America/Guatemala');
});

const packOpts = (csvDir) => ({
  csvDir, startDate: '2024-02-17', endDate: '2024-02-21',
  warnings: ['hill_score skipped (404)', '[skip] endurance_score: HTTP 204'],
  now: new Date(2026, 9, 15, 20, 53), // local 2026-10-15 20:53
});

test('AC-18 pack preview and both one-file packs equal the golden files', () => {
  const csvDir = path.join(PACK, 'csv_2024-02-21');
  const p = previewPack(packOpts(csvDir));
  pin(path.join(PACK, 'golden/CONTEXT.es.md'), p.langs.es.context);
  pin(path.join(PACK, 'golden/CONTEXT.en.md'), p.langs.en.context);
  const { csvDir: _dir, langs, ...rest } = p; // csvDir is machine specific; context is in the .md files
  pin(path.join(PACK, 'golden/preview.json'), json({ ...rest, langs: Object.fromEntries(Object.entries(langs).map(([l, v]) => [l, { bytes: v.bytes, tokens: v.tokens, singleName: v.singleName }])) }));
  for (const lang of ['es', 'en']) {
    const work = fs.mkdtempSync(path.join(os.tmpdir(), 'golden-pack-'));
    const copy = path.join(work, 'csv_2024-02-21');
    fs.cpSync(csvDir, copy, { recursive: true });
    const b = buildPack({ ...packOpts(copy), lang, mode: 'single' });
    pin(path.join(PACK, 'golden', path.basename(b.file)), fs.readFileSync(b.file, 'utf8'));
    const f = buildPack({ ...packOpts(copy), lang, mode: 'files' });
    expect(fs.readFileSync(path.join(f.dir, `CONTEXT.${lang}.md`), 'utf8')).toBe(p.langs[lang].context);
    expect(f.count).toBe(22);
    fs.rmSync(work, { recursive: true, force: true });
  }
});

test('AC-19 the bug report URLs equal the golden file', () => {
  const log = Array.from({ length: 300 }, (_, i) => ({
    ts: `10:00:${String(i % 60).padStart(2, '0')}`, type: i % 2 ? 'warn' : 'dim',
    msg: `line ${i} ${'x'.repeat(40)} token=t${i} user${i}@example.com /Users/dani/Documents/x${i}`,
  }));
  const input = {
    error: 'Auth failed for a@b.com: Authorization: Bearer eyJhbGciOiJIUzI1NiJ9.eyJzdWIiOiIxMjM0In0.sig password=hunter2 in /Users/dani/Library',
    log, version: '4.3.0', os: 'Mac OS X 15.6',
  };
  pin(path.join(REPORT, 'input.json'), json(input));
  pin(path.join(REPORT, 'golden.json'), json(buildReport(input)));
});

test('AC-20 redaction cases equal the golden file', () => {
  const inputs = [
    'https://api.example.com/x?token=abc&access_token=def&authCode=ghi&secret=s&password=p&refresh_token=rt&sig=zz&keep=ok',
    'plain text', '',
    'Authorization: Bearer eyJ0.abc.def', 'auth: bearer abc123.def-ghi', 'Authorization: Basic dXNlcjpwYXNz',
    'Cookie: SESSIONID=abc; other=xyz', 'Set-Cookie: JWT=eyJ...; Path=/',
    'error with token eyJhbGciOiJIUzI1NiJ9.eyJzdWIiOiIxMjM0In0.sig embedded',
    '{"password":"hunter2","token":"abc","keep":"ok"}',
    'GET https://x/y?access_token=abc&name=me failed',
    'HTTP 500: {"error":"bad","password":"x"} Bearer abc.def.ghi at /Users/dani/x',
    'two lines\nAuthorization: Bearer one.two.three\nCookie: a=b',
    'acentos: contraseña=secreta y "clientSecret":"é"',
  ];
  const cases = [];
  for (const s of inputs) cases.push({ fn: 'string', in: s, out: redactString(s) }, { fn: 'url', in: s, out: redactUrl(s) });
  pin(path.join(REPORT, 'cases.json'), json(cases));
});
```

- [ ] **Step 3: Generate the goldens.** Run: `cd tauri-app && TZ=America/Guatemala UPDATE_GOLDEN=1 bun test sidecar/golden.test.js` Expected: 4 pass; the 5 pack files, `input.json`, `golden.json` and `cases.json` exist. Then run it again without `UPDATE_GOLDEN`: `TZ=America/Guatemala bun test sidecar/golden.test.js` Expected: PASS (the files reproduce). Then `bun scripts/test-ids.js` Expected: `513 IDs ... 0 duplicates, 0 not in a test name`.

- [ ] **Step 4: Read the goldens once.** Open `CONTEXT.en.md` and check by eye: the range line reads `Range: 2024-02-17 → 2024-02-21 (5 days)`, the exported line reads `Exported: 2026-10-15 20:53 (America/Guatemala)`, `## Files (21)`, the MyFitnessPal line says `food logged on 2 of 4 days` (check the count against `mfp_daily.csv`; whatever it is, the Go test reads the same file), and the two warnings are listed under `## Missing data and warnings`. `golden.json` holds two URLs, the issue URL at most 7,500 characters, the mailto URL at most 1,800, neither containing `example.com`, `hunter2` or `/Users/dani`. `cases.json` holds 28 cases.

- [ ] **Step 5: Commit.**

```bash
git add tauri-app/sidecar/golden.test.js tauri-app/core/internal/pack/testdata tauri-app/core/internal/report/testdata
git -c user.name="Daniel Hernández" -c user.email=danisnowman@gmail.com commit -m "test(sidecar): freeze the pack, report and redaction outputs as golden files for the Go port"
```

---

### Task 2: `jsjson.SliceUTF16` and the architecture rules for the two new packages

Redaction already lives in `gconnect` (`Redact`, `RedactURL`), so stage 3 adds no `internal/redact` package. This task adds the one JavaScript string helper the report and the warning cut need, and extends `arch_test.go` so the wire rule covers `internal/pack` and `internal/report` before they exist.

**Files:**
- Modify: `internal/jsjson/format.go`, `internal/jsjson/jsjson_test.go`, `arch_test.go`

**Interfaces:**
- Produces: `jsjson.SliceUTF16(s string, n int) string`.

- [ ] **Step 1: Write the failing test.** `internal/jsjson/jsjson_test.go`, add:

```go
func TestSliceUTF16CountsCodeUnits(t *testing.T) {
	t.Run("BVA-602 s.slice(0, n) counts UTF-16 code units: 2 of héllo, 5 of ab, and half an astral pair", func(t *testing.T) {
		if got := SliceUTF16("héllo", 2); got != "hé" {
			t.Errorf("got %q", got)
		}
		if got := SliceUTF16("ab", 5); got != "ab" {
			t.Errorf("got %q", got)
		}
		// An astral character is two code units; cutting it in half gives U+FFFD, where JavaScript would keep a lone surrogate.
		if got := SliceUTF16("a😀b", 2); got != "a\uFFFD" {
			t.Errorf("got %q", got)
		}
		if got := SliceUTF16("", 3); got != "" {
			t.Errorf("got %q", got)
		}
	})
}
```

- [ ] **Step 2: Run it.** Run: `cd tauri-app/core && go test ./internal/jsjson` Expected: FAIL (`undefined: SliceUTF16`).

- [ ] **Step 3: Implement.** `internal/jsjson/format.go`, add (`unicode/utf16` is standard library, so the leaf rule holds):

```go
// SliceUTF16 is JavaScript's s.slice(0, n): n UTF-16 code units. A surrogate pair cut in half
// decodes to U+FFFD, where JavaScript keeps a lone surrogate; nothing here encodes such a string again.
func SliceUTF16(s string, n int) string {
	u := utf16.Encode([]rune(s))
	if n < len(u) {
		u = u[:n]
	}
	return string(utf16.Decode(u))
}
```

- [ ] **Step 4: The wire rule covers the new packages.** In `arch_test.go`: `var wirePackages = []string{"internal/garmin", "internal/mfp", "internal/pack", "internal/report", "gconnect"}`. In `TestArchitectureCheckerCatchesEachRule`, add one fixture to CL-268: `{"a stage 3 package", map[string]string{"internal/pack/a.go": src("pack", `"errors"`, "var _ = errors.New\nfunc Build(req map[string]any) {}")}}`, and one to CL-274: `{"a field of an exported struct in report", map[string]string{"internal/report/a.go": "package report\n\ntype Input struct{ Extra map[string]any }\n"}}`. The test lists its fixtures by hand so that dropping a name from the map fails it; these two rows do the same for the new names.

- [ ] **Step 5: Run everything.** Run: `cd tauri-app && bun run test:core && bun scripts/test-ids.js` Expected: PASS; `514 IDs`.

- [ ] **Step 6: Commit.**

```bash
git add tauri-app/core
git -c user.name="Daniel Hernández" -c user.email=danisnowman@gmail.com commit -m "feat(core): jsjson.SliceUTF16; the wire rule covers internal/pack and internal/report"
```

---

### Task 3: `internal/pack`, byte equal to the frozen pack

**Files:**
- Create: `internal/pack/pack.go`, `internal/pack/pack_test.go`

**Interfaces:**
- Consumes: nothing from the app (`os`, `path/filepath`, `regexp`, `strings`, `time`, `math`, `unicode`).
- Produces:

```go
type Options struct {
	CSVDir, StartDate, EndDate string
	Warnings                   []string
	Now                        time.Time // the Exported line; cmd passes time.Now()
	TZ                         string    // the zone name in the Exported line; cmd resolves it (os.Getenv is forbidden here)
}
type File struct {
	Name string `json:"name"`
	Rows int    `json:"rows"`
	header            []string
	text, first, last string
}
type Lang struct {
	Context    string `json:"context"`
	Bytes      int    `json:"bytes"`
	Tokens     int    `json:"tokens"`
	SingleName string `json:"singleName"`
}
type Preview struct {
	CSVDir    string          `json:"csvDir"`
	StartDate string          `json:"startDate"`
	EndDate   string          `json:"endDate"`
	Days      int             `json:"days"`
	Files     []File          `json:"files"`    // never nil
	Warnings  []string        `json:"warnings"` // never nil
	Langs     map[string]Lang `json:"langs"`
}
type Result struct {
	Dir   string `json:"dir"`
	File  string `json:"file,omitempty"` // single mode only, as buildPack
	Count int    `json:"count"`
}
var Langs = []string{"es", "en"}
func BuildPreview(o Options) (*Preview, error)              // nil, nil when previewPack returned null
func Build(o Options, lang, mode string) (*Result, error)
func Context(lang string, o Options, files []File) string
```

- [ ] **Step 1: Write the failing tests.** `internal/pack/pack_test.go`:

```go
package pack

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// The frozen inputs and outputs of tauri-app/sidecar/llm-pack.js (stage 3 Task 1).
const fixtureDir = "testdata/csv_2024-02-21"

func opts(csvDir string) Options {
	return Options{CSVDir: csvDir, StartDate: "2024-02-17", EndDate: "2024-02-21",
		Warnings: []string{"hill_score skipped (404)", "[skip] endurance_score: HTTP 204"},
		Now: time.Date(2026, 10, 15, 20, 53, 0, 0, time.Local), TZ: "America/Guatemala"}
}

func golden(t *testing.T, name string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("testdata/golden", name))
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func copyFixture(t *testing.T) string {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "csv_2024-02-21")
	if err := os.CopyFS(dir, os.DirFS(fixtureDir)); err != nil {
		t.Fatal(err)
	}
	return dir
}

func TestPreviewEqualsTheJavaScriptGoldens(t *testing.T) {
	t.Run("AC-602 the context of both languages and the preview numbers equal the frozen files", func(t *testing.T) {
		p, err := BuildPreview(opts(fixtureDir))
		if err != nil || p == nil {
			t.Fatalf("preview: %v %v", p, err)
		}
		for _, lang := range Langs {
			if got, want := p.Langs[lang].Context, golden(t, "CONTEXT."+lang+".md"); got != want {
				t.Errorf("CONTEXT.%s.md differs at byte %d:\n got %q\nwant %q", lang, firstDiff(got, want), got, want)
			}
		}
		var want struct {
			StartDate, EndDate string
			Days               int
			Files              []File
			Warnings           []string
			Langs              map[string]struct {
				Bytes, Tokens int
				SingleName    string
			}
		}
		if err := json.Unmarshal([]byte(golden(t, "preview.json")), &want); err != nil {
			t.Fatal(err)
		}
		if p.Days != want.Days || len(p.Files) != 21 || len(p.Files) != len(want.Files) {
			t.Fatalf("days %d files %d, want %d and %d", p.Days, len(p.Files), want.Days, len(want.Files))
		}
		for i, f := range want.Files {
			if p.Files[i].Name != f.Name || p.Files[i].Rows != f.Rows {
				t.Errorf("file %d: got %+v want %+v", i, p.Files[i], f)
			}
		}
		for lang, w := range want.Langs {
			g := p.Langs[lang]
			if g.Bytes != w.Bytes || g.Tokens != w.Tokens || g.SingleName != w.SingleName {
				t.Errorf("%s: got %d %d %s, want %d %d %s", lang, g.Bytes, g.Tokens, g.SingleName, w.Bytes, w.Tokens, w.SingleName)
			}
		}
	})
}

func TestBuildWritesTheGoldenPacks(t *testing.T) {
	t.Run("AC-603 single writes the frozen one-file pack and files writes the context plus byte equal CSV copies, removing the v4.0.0 root files", func(t *testing.T) {
		for _, lang := range Langs {
			dir := copyFixture(t)
			os.WriteFile(filepath.Join(dir, "CONTEXT.es.md"), []byte("legacy"), 0o644) // v4.0.0 root files
			os.WriteFile(filepath.Join(dir, "garmin_2026-01-01_2026-01-02.en.md"), []byte("legacy"), 0o644)
			r, err := Build(opts(dir), lang, "single")
			if err != nil || r.Count != 1 || r.Dir != filepath.Join(dir, "llm-"+lang+"-one-file") {
				t.Fatalf("single: %+v %v", r, err)
			}
			got, _ := os.ReadFile(r.File)
			if want := golden(t, filepath.Base(r.File)); string(got) != want {
				t.Errorf("%s differs at byte %d", filepath.Base(r.File), firstDiff(string(got), want))
			}
			r, err = Build(opts(dir), lang, "files")
			if err != nil || r.Count != 22 || r.File != "" || r.Dir != filepath.Join(dir, "llm-"+lang) {
				t.Fatalf("files: %+v %v", r, err)
			}
			ctx, _ := os.ReadFile(filepath.Join(r.Dir, "CONTEXT."+lang+".md"))
			if string(ctx) != golden(t, "CONTEXT."+lang+".md") {
				t.Error("CONTEXT in the files folder differs from the preview context")
			}
			if b, _ := os.ReadFile(filepath.Join(r.Dir, "sleep.csv")); !bytes.Equal(b, mustRead(t, filepath.Join(dir, "sleep.csv"))) {
				t.Error("sleep.csv was not copied byte for byte")
			}
			entries, _ := os.ReadDir(dir)
			var names []string
			for _, e := range entries {
				if strings.HasSuffix(e.Name(), ".md") {
					names = append(names, e.Name())
				}
			}
			if len(names) != 0 {
				t.Errorf("legacy root files kept: %v", names)
			}
		}
	})
}

func TestASecondPressReplacesTheFolder(t *testing.T) {
	t.Run("ST-T601 files, a stray file in llm-en, files again: the stray file is gone", func(t *testing.T) {
		dir := copyFixture(t)
		r, err := Build(opts(dir), "en", "files")
		if err != nil {
			t.Fatal(err)
		}
		os.WriteFile(filepath.Join(r.Dir, "stale.txt"), []byte("x"), 0o644)
		if _, err := Build(opts(dir), "en", "files"); err != nil {
			t.Fatal(err)
		}
		if _, err := os.Stat(filepath.Join(r.Dir, "stale.txt")); !os.IsNotExist(err) {
			t.Error("stale file survived the second press")
		}
	})
}

func TestLangAndModePartitions(t *testing.T) {
	t.Run("EP-601 fr and zip are refused with the cli.js sentences before anything is deleted", func(t *testing.T) {
		dir := copyFixture(t)
		os.WriteFile(filepath.Join(dir, "CONTEXT.en.md"), []byte("legacy"), 0o644)
		if _, err := Build(opts(dir), "fr", "files"); err == nil || err.Error() != "lang must be es or en." {
			t.Errorf("fr: %v", err)
		}
		if _, err := Build(opts(dir), "es", "zip"); err == nil || err.Error() != "mode must be files or single." {
			t.Errorf("zip: %v", err)
		}
		if _, err := os.Stat(filepath.Join(dir, "CONTEXT.en.md")); err != nil {
			t.Error("a refused request deleted a file")
		}
	})
}

func TestMissingFolderIsNilAndEmptyFolderHasAnEmptyList(t *testing.T) {
	t.Run("EG-601 a missing folder or a missing date gives nil; an empty folder gives files [] and 0 files in the context", func(t *testing.T) {
		o := opts(filepath.Join(t.TempDir(), "none"))
		if p, err := BuildPreview(o); p != nil || err != nil {
			t.Fatalf("missing folder: %v %v", p, err)
		}
		for _, o := range []Options{{CSVDir: fixtureDir, EndDate: "2024-02-21"}, {CSVDir: fixtureDir, StartDate: "2024-02-17"}, {StartDate: "2024-02-17", EndDate: "2024-02-21"}} {
			if p, _ := BuildPreview(o); p != nil {
				t.Errorf("preview without a folder and both dates must be nil")
			}
		}
		empty := filepath.Join(t.TempDir(), "csv_2024-02-21")
		os.Mkdir(empty, 0o755)
		p, err := BuildPreview(opts(empty))
		if err != nil || p == nil {
			t.Fatal(err)
		}
		b, _ := json.Marshal(p)
		if !strings.Contains(string(b), `"files":[]`) || !strings.Contains(string(b), `"warnings":[`) {
			t.Errorf("empty folder must serialize files as []: %s", b)
		}
		if !strings.Contains(p.Langs["en"].Context, "## Files (0)") {
			t.Error("an empty folder lists 0 files")
		}
		if p, _ := BuildPreview(Options{CSVDir: empty, StartDate: "2024-02-17", EndDate: "2024-02-21"}); p == nil || p.Warnings == nil {
			t.Error("nil Warnings in must still be [] out")
		}
	})
}

func TestLineEndingsQuotedHeaderAndEmptyFile(t *testing.T) {
	t.Run("EG-602 CRLF lines, a quoted header cell and an empty file scan as the JavaScript did", func(t *testing.T) {
		dir := filepath.Join(t.TempDir(), "csv_2026-05-12")
		os.Mkdir(dir, 0o755)
		os.WriteFile(filepath.Join(dir, "a.csv"), []byte("date,\"sleepTimeSeconds\",x\r\n2026-05-12,1,2\r\n\r\n2026-05-11,3,4\r\n"), 0o644)
		os.WriteFile(filepath.Join(dir, "b.csv"), []byte(""), 0o644)
		files, err := csvFiles(dir)
		if err != nil {
			t.Fatal(err)
		}
		a, b := files[0], files[1]
		if a.Rows != 2 || a.first != "2026-05-11" || a.last != "2026-05-12" || a.header[1] != "sleepTimeSeconds" {
			t.Errorf("a: %+v", a)
		}
		if b.Rows != 0 || len(b.header) != 1 || b.header[0] != "" {
			t.Errorf("b: %+v (JavaScript gives one empty column name for an empty file)", b)
		}
		ctx := Context("en", Options{StartDate: "2026-05-11", EndDate: "2026-05-12", TZ: "UTC"}, files)
		if !strings.Contains(ctx, "- `a.csv`: a. 2 rows, 2026-05-11 → 2026-05-12.\n  Columns: date, sleepTimeSeconds (s), x\n") {
			t.Errorf("context:\n%s", ctx)
		}
		if !strings.Contains(ctx, "- `b.csv`: b. 0 rows.\n  Columns: \n") {
			t.Errorf("empty file line:\n%s", ctx)
		}
	})
}

func TestLoggedColumnByHeaderNotPosition(t *testing.T) {
	t.Run("EG-603 the MyFitnessPal line counts the logged column wherever the header puts it", func(t *testing.T) {
		dir := filepath.Join(t.TempDir(), "csv_2026-05-12")
		os.Mkdir(dir, 0o755)
		os.WriteFile(filepath.Join(dir, "mfp_daily.csv"), []byte("date,meals_logged,logged\n2026-05-11,2,yes\n2026-05-12,0,no\n"), 0o644)
		files, _ := csvFiles(dir)
		ctx := Context("en", Options{StartDate: "2026-05-11", EndDate: "2026-05-12"}, files)
		if !strings.Contains(ctx, "MyFitnessPal: food logged on 1 of 2 days.") {
			t.Errorf("context:\n%s", ctx)
		}
	})
}

func TestUnitTable(t *testing.T) {
	t.Run("DT-R601 unitOf gives the UNITS answer for 25 column names, first match wins", func(t *testing.T) {
		cases := map[string]string{
			"sleepTimeSeconds": "s", "restingHeartRate": "bpm", "hrTimeInZone_2": "s", "averageSpO2HRSleep": "bpm",
			"averageSpO2Value": "%", "energy_kcal": "kcal", "exercise_kcal_mfp": "kcal", "protein_g": "g", "grams": "g",
			"sodium_mg": "mg", "duration_min": "min", "lastNightAvg": "ms", "baselineLowUpper": "ms", "vo2max": "ml/kg/min",
			"VO2Max": "ml/kg/min", "maxTempC": "°C", "averageSpeed": "m/s", "weight": "g", "calcium": "", "date": "",
			"distance": "m", "calories": "kcal", "hrv": "ms", "durationInMilliseconds": "ms", "hydrationMl": "ml",
		}
		for col, want := range cases {
			if got := unitOf(col); got != want {
				t.Errorf("unitOf(%q) = %q, want %q", col, got, want)
			}
		}
	})
}

func TestDaysBetween(t *testing.T) {
	t.Run("BVA-601 the same day is 1 and 2026-07-17 to 2026-10-14 is 90", func(t *testing.T) {
		if d := daysBetween("2026-10-15", "2026-10-15"); d != 1 {
			t.Errorf("same day = %d", d)
		}
		if d := daysBetween("2026-07-17", "2026-10-14"); d != 90 {
			t.Errorf("90 days = %d", d)
		}
	})
}

func firstDiff(a, b string) int {
	for i := 0; i < len(a) && i < len(b); i++ {
		if a[i] != b[i] {
			return i
		}
	}
	return min(len(a), len(b))
}

func mustRead(t *testing.T, p string) []byte {
	t.Helper()
	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	return b
}
```

Check the exact wording of the EG-602 expectations against `CONTEXT.en.md` from Task 1 (the `- \`name\`: desc. N rows, first → last.` line) before running; only the file names differ. The DT-R601 answers come from `UNITS` in `llm-pack.js` read top to bottom; check any doubtful one with `bun -e 'import("./sidecar/llm-pack.js")'` style one liners before Task 6 deletes the file.

- [ ] **Step 2: Run them.** Run: `cd tauri-app/core && go test ./internal/pack` Expected: FAIL (`undefined: BuildPreview`).

- [ ] **Step 3: Implement `pack.go`.** Port `llm-pack.js` top to bottom, one Go function per JavaScript function, with the JavaScript name in a comment. No `os.Getenv`, no `fmt.Print`, no `time.Now()` (both come in through `Options`). The parts that decide byte equality:

```go
// Package pack builds the LLM pack: CONTEXT.<lang>.md next to the CSV files, or one Markdown
// file with every CSV inline. Port of tauri-app/sidecar/llm-pack.js; the files in
// testdata/golden were written by that JavaScript and pin the bytes.
package pack

var (
	dateRe   = regexp.MustCompile(`\b(\d{4}-\d{2}-\d{2})`)
	legacyRe = regexp.MustCompile(`^(CONTEXT|garmin_).*\.(es|en)\.md$`) // v4.0.0 wrote these next to the CSVs
)

// describe copies DESCRIBE: [es, en] per file base name. Copy the 23 entries verbatim.
var describe = map[string][2]string{ /* ... */ }

// units copies UNITS in order; the first match wins. (?i) stands for the /i flag.
var units = []struct {
	re   *regexp.Regexp
	unit string
}{
	{regexp.MustCompile(`_kcal(_mfp)?$`), "kcal"}, {regexp.MustCompile(`_g$|^grams$`), "g"}, {regexp.MustCompile(`_mg$`), "mg"}, {regexp.MustCompile(`_min$`), "min"},
	{regexp.MustCompile(`TimeInZone`), "s"}, {regexp.MustCompile(`(?i)SpO2HR`), "bpm"},
	{regexp.MustCompile(`(?i)Milliseconds$|InMilliseconds$`), "ms"}, {regexp.MustCompile(`Seconds$|InSeconds$|^duration$|Duration$`), "s"},
	{regexp.MustCompile(`Meters$|^distance$|Distance$`), "m"}, {regexp.MustCompile(`Kilocalories$|^calories$|Calories$`), "kcal"},
	{regexp.MustCompile(`Hrv$|HRV|hrv`), "ms"}, {regexp.MustCompile(`(?i)HeartRate$|Hr$|^hr|Bpm$`), "bpm"}, {regexp.MustCompile(`(?i)Ml$|InML$`), "ml"},
	{regexp.MustCompile(`^lastNight|^baseline(Low|Balanced)`), "ms"}, {regexp.MustCompile(`(?i)^vo2max$`), "ml/kg/min"}, {regexp.MustCompile(`(?i)Percent$|Pct$|SpO2`), "%"}, {regexp.MustCompile(`C$`), "°C"}, {regexp.MustCompile(`Speed$`), "m/s"}, {regexp.MustCompile(`[Ww]eight$`), "g"},
}
// ponytail: JavaScript \s also matches U+00A0, U+2028 and U+FEFF and RE2 does not; and RE2's (?i)
// folds s with U+017F and k with U+212A. No column name here holds any of them.

// scanCSV ports scanCsv: lines are split on \n with one trailing \r removed and empty lines dropped;
// the header is the first line split on commas with one leading and one trailing quote removed.
func scanCSV(path string) (File, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return File{}, err
	}
	f := File{Name: filepath.Base(path), text: string(b)}
	lines := splitLines(f.text)
	first := ""
	if len(lines) > 0 {
		first = lines[0]
	}
	for _, h := range strings.Split(first, ",") { // "".split(",") is [""] in JavaScript: one empty column
		f.header = append(f.header, strings.TrimSuffix(strings.TrimPrefix(h, `"`), `"`))
	}
	if len(lines) > 1 {
		f.Rows = len(lines) - 1
	}
	for _, l := range lines[min(1, len(lines)):] {
		m := dateRe.FindStringSubmatch(l)
		if m == nil {
			continue
		}
		if f.first == "" || m[1] < f.first {
			f.first = m[1]
		}
		if f.last == "" || m[1] > f.last {
			f.last = m[1]
		}
	}
	return f, nil
}

func splitLines(text string) []string {
	var out []string
	for _, l := range strings.Split(text, "\n") {
		if l = strings.TrimSuffix(l, "\r"); l != "" {
			out = append(out, l)
		}
	}
	return out
}

// csvFiles ports csvFiles plus the scan: every *.csv in the folder, sorted by name.
func csvFiles(dir string) ([]File, error) { /* os.ReadDir, filter .csv, sort by name, scanCSV each */ }

// trimEnd ports String.prototype.trimEnd. Go also trims U+0085, which no CSV here holds.
func trimEnd(s string) string {
	return strings.TrimRightFunc(s, func(r rune) bool { return unicode.IsSpace(r) || r == '\uFEFF' })
}

// daysBetween ports daysBetween: inclusive count of calendar days. Both dates parse as UTC, so
// no DST day is 23 or 25 hours and no rounding is needed.
func daysBetween(a, b string) int {
	s, _ := time.Parse("2006-01-02", a)
	e, _ := time.Parse("2006-01-02", b)
	return int(e.Sub(s).Hours()/24) + 1
}
```

`Context` builds the same `out` slice as `buildContext`, line by line, and returns `strings.Join(out, "\n") + "\n"`. The exported line is `o.Now.Format("2006-01-02 15:04") + " (" + o.TZ + ")"` (JavaScript: `now.toLocaleDateString('sv-SE') + ' ' + now.toTimeString().slice(0, 5)` and `Intl.DateTimeFormat().resolvedOptions().timeZone`). The MyFitnessPal line uses the first file named `mfp_daily.csv`: rows are `strings.Split(text, "\n")[1:]` with `\r` trimmed and empties dropped (the JavaScript drops the first raw line before filtering), `col` is the index of `logged` in the header, and a row counts when `strings.Split(row, ",")` has more than `col` items and item `col` is `yes`; `col < 0` counts nothing.

`single(ctx, files)` joins `ctx` and one "## `name`\n\n```csv\n" + trimEnd(text) + "\n```\n" block per file with `\n`.

`BuildPreview` returns `nil, nil` when `CSVDir`, `StartDate` or `EndDate` is empty or the folder does not exist (`os.Stat` fails); any other read error is returned. It reads the files once, builds both languages, `Bytes = len(single)`, `Tokens = int(math.Round(float64(len(single)) / 3))`, `Files = make([]File, 0, n)`, `Warnings` non nil (`[]string{}` when `o.Warnings` is nil).

`Build` checks `lang` and `mode` with the two `cli.js` sentences first, then removes root files matching `legacyRe` (an `os.Remove` error is returned, as `rmSync` throws), `os.RemoveAll` and `os.Mkdir(dir, 0o755)`, writes with `os.WriteFile(..., 0o644)` (not `atomicfile`: the folder is rebuilt on every press), copies CSV files byte for byte, and returns `&Result{Dir, Count: len(files)+1}` or `&Result{Dir, File, Count: 1}`.

- [ ] **Step 4: Run them.** Run: `cd tauri-app/core && go test ./internal/pack && go test . && go vet ./...` Expected: PASS, including `TestArchitectureRules`. If AC-602 fails, print the bytes around `firstDiff` from both strings and fix the port, never the golden file.

- [ ] **Step 5: Commit.**

```bash
git add tauri-app/core/internal/pack
git -c user.name="Daniel Hernández" -c user.email=danisnowman@gmail.com commit -m "feat(core): LLM pack in Go, byte equal to the frozen JavaScript output"
```

---

### Task 4: `internal/report`, byte equal URLs

**Files:**
- Create: `internal/report/report.go`, `internal/report/report_test.go`, `internal/report/redact_test.go`

**Interfaces:**
- Consumes: `gconnect.Redact`, `gconnect.RedactURL`, `gconnect.IsSecretKey` (v0.1.2; Step 0 takes it), `jsjson.SliceUTF16` (Task 2).

- [ ] **Step 0: Take gconnect v0.1.2.** Run: `cd tauri-app/core && go get github.com/danihrndzld/gconnect@v0.1.2 && go mod tidy && go test ./...` Expected: PASS. v0.1.2 adds only `IsSecretKey` and docs. Commit with the task.
- Produces: `report.Line{TS, Type, Msg string}` (json `ts`, `type`, `msg`), `report.Result{IssueURL, MailtoURL string}` (json `issueUrl`, `mailtoUrl`), `report.Build(errText string, log []Line, version, osName string) Result`, `report.Scrub(s string) string`, `report.EncodeURIComponent(s string) string`, constants `Repo`, `Email`.

- [ ] **Step 1: Write the failing tests.** `internal/report/redact_test.go`:

```go
package report

import (
	"encoding/json"
	"os"
	"testing"

	"github.com/danihrndzld/gconnect"
)

// cases.json was written by electron-app/util/redact.js (stage 3 Task 1). The report URLs depend
// on the library giving the same output, so a gconnect bump that changes redaction fails here first.
func TestRedactionMatchesTheJavaScript(t *testing.T) {
	t.Run("AC-601 gconnect.Redact and RedactURL give the JavaScript output for every frozen case", func(t *testing.T) {
		raw, err := os.ReadFile("testdata/cases.json")
		if err != nil {
			t.Fatal(err)
		}
		var cases []struct{ Fn, In, Out string }
		if err := json.Unmarshal(raw, &cases); err != nil {
			t.Fatal(err)
		}
		if len(cases) != 28 {
			t.Fatalf("%d cases, want 28", len(cases))
		}
		for _, c := range cases {
			got := gconnect.RedactURL(c.In)
			if c.Fn == "string" {
				got = gconnect.Redact(c.In)
			}
			if got != c.Out {
				t.Errorf("%s(%q)\n got %q\nwant %q", c.Fn, c.In, got, c.Out)
			}
		}
	})
}
```

`internal/report/report_test.go`:

```go
package report

import (
	"encoding/json"
	"net/url"
	"os"
	"strings"
	"testing"
)

type input struct {
	Error   string `json:"error"`
	Log     []Line `json:"log"`
	Version string `json:"version"`
	OS      string `json:"os"`
}

func load(t *testing.T) (input, map[string]string) {
	t.Helper()
	var in input
	var want map[string]string
	b, err := os.ReadFile("testdata/input.json")
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(b, &in); err != nil {
		t.Fatal(err)
	}
	b, _ = os.ReadFile("testdata/golden.json")
	if err := json.Unmarshal(b, &want); err != nil {
		t.Fatal(err)
	}
	return in, want
}

func TestReportURLsEqualTheJavaScript(t *testing.T) {
	t.Run("AC-604 both URLs equal golden.json byte for byte", func(t *testing.T) {
		in, want := load(t)
		r := Build(in.Error, in.Log, in.Version, in.OS)
		if r.IssueURL != want["issueUrl"] {
			t.Errorf("issueUrl differs at %d:\n got %s\nwant %s", firstDiff(r.IssueURL, want["issueUrl"]), r.IssueURL, want["issueUrl"])
		}
		if r.MailtoURL != want["mailtoUrl"] {
			t.Errorf("mailtoUrl differs at %d:\n got %s\nwant %s", firstDiff(r.MailtoURL, want["mailtoUrl"]), r.MailtoURL, want["mailtoUrl"])
		}
	})
}

func TestNewestLinesKeptWithinTheLimits(t *testing.T) {
	t.Run("EG-604 300 lines: line 299 stays, line 0 goes, the limits hold and no secret, email or home folder leaks", func(t *testing.T) {
		in, _ := load(t)
		r := Build(in.Error, in.Log, in.Version, in.OS)
		if len(r.IssueURL) > 7500 || len(r.MailtoURL) > 1800 {
			t.Fatalf("lengths %d %d", len(r.IssueURL), len(r.MailtoURL))
		}
		body, _ := url.QueryUnescape(strings.SplitN(r.IssueURL, "&body=", 2)[1])
		if !strings.Contains(body, "line 299") || strings.Contains(body, "line 0 ") {
			t.Error("the newest line must stay and the oldest must go")
		}
		for _, secret := range []string{"example.com", "hunter2", "/Users/dani", "eyJhbGci", "a@b.com"} {
			if strings.Contains(body, secret) || strings.Contains(r.MailtoURL, secret) {
				t.Errorf("%q leaked", secret)
			}
		}
		if !strings.HasPrefix(r.IssueURL, "https://github.com/"+Repo+"/issues/new?title=") || !strings.HasPrefix(r.MailtoURL, "mailto:"+Email+"?subject=") {
			t.Error("prefixes")
		}
	})
}

func TestEncodeURIComponentMatchesJavaScript(t *testing.T) {
	t.Run("EP-602 the unreserved set, the space, every reserved character and a multibyte rune encode as node does", func(t *testing.T) {
		// node -e 'console.log(encodeURIComponent("a b!*'"'"'()~-_.é\n`$&+,/:;=?@#"))' on 2026-10-15
		if got := EncodeURIComponent("a b!*'()~-_.é\n`$&+,/:;=?@#"); got != "a%20b!*'()~-_.%C3%A9%0A%60%24%26%2B%2C%2F%3A%3B%3D%3F%40%23" {
			t.Errorf("got %s", got)
		}
		if got := EncodeURIComponent("·"); got != "%C2%B7" {
			t.Errorf("got %s", got)
		}
	})
}

func TestTitleIsCutAt120UTF16Units(t *testing.T) {
	t.Run("EG-605 a 200 rune error gives a 120 unit title; an empty error reads Unknown error", func(t *testing.T) {
		long := strings.Repeat("é", 200)
		r := Build(long, nil, "1", "x")
		title, _ := url.QueryUnescape(strings.TrimPrefix(strings.SplitN(r.IssueURL, "&body=", 2)[0], "https://github.com/"+Repo+"/issues/new?title="))
		if want := "Export failed: " + strings.Repeat("é", 105); title != want {
			t.Errorf("title has %d runes", len([]rune(title)))
		}
		if r2 := Build("", nil, "1", "x"); !strings.Contains(r2.IssueURL, "Unknown%20error") {
			t.Error("an empty error reads Unknown error")
		}
	})
}

func firstDiff(a, b string) int {
	for i := 0; i < len(a) && i < len(b); i++ {
		if a[i] != b[i] {
			return i
		}
	}
	return min(len(a), len(b))
}
```

- [ ] **Step 2: Run them.** Run: `cd tauri-app/core && go test ./internal/report` Expected: AC-601 PASS (the library already ports `redact.js`; if it fails, stop: the fix is a gconnect commit, a new tag and `go get`, which the user does, see "Decisions"), the rest FAIL (`undefined: Build`).

- [ ] **Step 3: Implement `report.go`.**

```go
// Package report builds the two bug report links (a prefilled GitHub issue and a mailto),
// scrubbed before anything leaves the machine. Port of tauri-app/sidecar/report.js.
package report

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/danihrndzld/garmin-health-exporter/tauri-app/core/internal/jsjson"
	"github.com/danihrndzld/gconnect"
)

const (
	Repo         = "danihrndzld/garmin-health-exporter"
	Email        = "danielernesto@ufm.edu"
	issueURLMax  = 7500 // GitHub rejects much longer prefilled URLs
	mailtoURLMax = 1800 // most mail clients cap the whole URL near 2 KB
)

var (
	kvRe    = regexp.MustCompile(`\b([\w-]+)=([^\s&"']+)`)
	emailRe = regexp.MustCompile(`[\w.+-]+@[\w-]+(\.[\w-]+)+`)
	homeRe  = regexp.MustCompile(`/Users/[^/\s]+`)
)

type Line struct {
	TS   string `json:"ts"`
	Type string `json:"type"`
	Msg  string `json:"msg"`
}

type Result struct {
	IssueURL  string `json:"issueUrl"`
	MailtoURL string `json:"mailtoUrl"`
}

// Scrub ports scrub: redaction, key=value secrets, emails, home folders.
func Scrub(s string) string {
	s = gconnect.Redact(s)
	s = kvRe.ReplaceAllStringFunc(s, func(m string) string {
		k := m[:strings.IndexByte(m, '=')]
		if gconnect.IsSecretKey(k) {
			return k + "=REDACTED"
		}
		return m
	})
	s = emailRe.ReplaceAllString(s, "[email]")
	return homeRe.ReplaceAllString(s, "~")
}

// EncodeURIComponent keeps A-Z a-z 0-9 - _ . ! ~ * ' ( ) and writes every other byte as %XX.
// url.QueryEscape writes a space as + and escapes !*'(); url.PathEscape keeps $&+:=@.
func EncodeURIComponent(s string) string {
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		c := s[i]
		if 'A' <= c && c <= 'Z' || 'a' <= c && c <= 'z' || '0' <= c && c <= '9' || strings.IndexByte("-_.!~*'()", c) >= 0 {
			b.WriteByte(c)
		} else {
			fmt.Fprintf(&b, "%%%02X", c)
		}
	}
	return b.String()
}

// fit ports fit: drop lines from the top until the encoded URL fits.
func fit(prefix, head string, lines []string, max int) string {
	tail := lines
	build := func() string {
		end := ""
		if len(tail) > 0 {
			end = "\n```\n"
		}
		return prefix + EncodeURIComponent(head+strings.Join(tail, "\n")+end)
	}
	u := build()
	for len(tail) > 0 && len(u) > max {
		tail = tail[1:]
		u = build()
	}
	return u
}

// Build ports buildReport: an empty error reads Unknown error, the last 200 lines, every piece scrubbed.
func Build(errText string, log []Line, version, osName string) Result {
	if errText == "" {
		errText = "Unknown error"
	}
	e := Scrub(errText)
	if len(log) > 200 {
		log = log[len(log)-200:]
	}
	lines := make([]string, 0, len(log))
	for _, l := range log {
		lines = append(lines, Scrub(l.TS)+" ["+Scrub(l.Type)+"] "+Scrub(l.Msg))
	}
	title := jsjson.SliceUTF16("Export failed: "+e, 120)
	app := "v" + Scrub(version) + " · " + Scrub(osName)
	head := "**Error:** " + e + "\n\n**App:** " + app + "\n\n**Log (newest last):**\n```\n"
	mailHead := "Error: " + e + "\nApp: " + app + "\n\nLog:\n```\n"
	return Result{
		IssueURL:  fit("https://github.com/"+Repo+"/issues/new?title="+EncodeURIComponent(title)+"&body=", head, lines, issueURLMax),
		MailtoURL: fit("mailto:"+Email+"?subject="+EncodeURIComponent("Garmin Export: "+title)+"&body=", mailHead, lines, mailtoURLMax),
	}
}
```

`fmt.Fprintf` into a builder is allowed by the architecture test (only `fmt.Print*` is banned outside `cmd/`).

- [ ] **Step 4: Run them.** Run: `cd tauri-app/core && go test ./internal/report && go test . && go vet ./...` Expected: PASS.

- [ ] **Step 5: Commit.**

```bash
git add tauri-app/core/internal/report
git -c user.name="Daniel Hernández" -c user.email=danisnowman@gmail.com commit -m "feat(core): bug report links in Go with JavaScript URL encoding; gconnect redaction pinned to the frozen cases"
```

---

### Task 5: `summary`, `pack` and `report` commands; the UI runs everything in Go

This is the first shippable increment: Bun still ships, nothing calls it.

**Files:**
- Create: `internal/garmin/summary.go`, `internal/garmin/summary_test.go`, `cmd/garmin-export-core/summary.go`, `cmd/garmin-export-core/pack.go`, `cmd/garmin-export-core/report.go`, `cmd/garmin-export-core/stage3_test.go`
- Modify: `cmd/garmin-export-core/main.go`, `cmd/garmin-export-core/main_test.go`, `tauri-app/ui/app.js`, `tauri-app/ui/dev-mock.js`, `tauri-app/e2e/engine.spec.js`, `tauri-app/e2e/support/mock-backend.js`

**Interfaces:**
- Produces: `garmin.Summary{Range json.RawMessage; Days []Day}`, `garmin.Day{Date string; Steps, RHR, SleepScore, SleepSec, HRV json.RawMessage}`, `garmin.Summarize(raw []byte) (*Summary, error)`, `(*Summary).Dates() (start, end string)`; handlers `summary`, `buildPack`, `buildReport` with the existing signature `func(ctx context.Context, req map[string]any, out *protocol.Emitter) (any, error)`; `cmd` helpers `warnings(req) []string`, `tzName() string`, `packOptions(...)`.
- Consumes: `pack.BuildPreview`, `pack.Build`, `pack.Options`, `pack.Preview`, `pack.Result` (Task 3); `report.Build`, `report.Line`, `report.Result` (Task 4); `jsjson.SliceUTF16` (Task 2); `guard.InsideHome`.

- [ ] **Step 1: Write the failing Go tests.** `internal/garmin/summary_test.go` (`fixturePath` is the package variable in `csv_test.go`; Task 7 changes it in one place):

```go
package garmin

import (
	"os"
	"testing"
)

func TestSummarizeTheFixture(t *testing.T) {
	t.Run("AC-605 the fixture gives 5 days ascending with the 5 fields as JSON, the first day's numbers, nulls where the path is missing, and the file's date_range", func(t *testing.T) {
		raw, err := os.ReadFile(fixturePath)
		if err != nil {
			t.Fatal(err)
		}
		s, err := Summarize(raw)
		if err != nil || len(s.Days) != 5 {
			t.Fatalf("summary: %+v %v", s, err)
		}
		if start, end := s.Dates(); start != "2024-02-17" || end != "2024-02-21" {
			t.Fatalf("dates %s %s", start, end)
		}
		if string(s.Range) != `{"start":"2024-02-17","end":"2024-02-21","days":6}` { // the file's date_range as it is (jq checked 2026-10-16)
			t.Errorf("range %s", s.Range)
		}
		for i := 1; i < len(s.Days); i++ {
			if s.Days[i].Date < s.Days[i-1].Date {
				t.Fatal("days are not ascending")
			}
		}
		d := s.Days[0] // 2024-02-17 (jq checked 2026-10-16)
		if d.Date != "2024-02-17" || string(d.Steps) != "1403" || string(d.RHR) != "54" || string(d.SleepScore) != "84" || string(d.SleepSec) != "42146" || string(d.HRV) != "55" {
			t.Errorf("first day: %+v", d)
		}
		last := s.Days[4] // 2024-02-21: sleepTimeSeconds is null and sleepScores is absent
		if string(last.SleepSec) != "null" || string(last.SleepScore) != "null" {
			t.Errorf("last day: %+v", last)
		}
		// A day without a sleep section gives null, not a crash; a value of 0 stays 0.
		s2, _ := Summarize([]byte(`{"date_range":{"start":"2026-01-01","end":"2026-01-01"},"daily":{"2026-01-01":{"stats":{"totalSteps":0}}}}`))
		if string(s2.Days[0].SleepScore) != "null" || string(s2.Days[0].Steps) != "0" {
			t.Errorf("got %+v", s2.Days[0])
		}
	})
}

func TestSummarizeEdges(t *testing.T) {
	t.Run("EG-606 text that is not JSON is an error; a file without daily or date_range gives 0 days and a null range", func(t *testing.T) {
		if _, err := Summarize([]byte("{not json")); err == nil {
			t.Error("corrupt JSON accepted")
		}
		s, err := Summarize([]byte(`{}`))
		if err != nil || len(s.Days) != 0 || string(s.Range) != "null" {
			t.Errorf("got %+v %v", s, err)
		}
		if start, end := s.Dates(); start != "" || end != "" {
			t.Errorf("dates %q %q", start, end)
		}
	})
}
```

`cmd/garmin-export-core/stage3_test.go` (no `t.Parallel`: `t.Setenv` forbids it):

```go
package main

import (
	"strings"
	"testing"
)

func TestTZName(t *testing.T) {
	t.Run("EG-607 tzName is $TZ when set, else the zone behind /etc/localtime, never Local", func(t *testing.T) {
		t.Setenv("TZ", "America/Guatemala")
		if tzName() != "America/Guatemala" {
			t.Errorf("got %q", tzName())
		}
		t.Setenv("TZ", "")
		if n := tzName(); n == "" || n == "Local" {
			t.Errorf("got %q", n)
		}
	})
}

func TestWarningsCut(t *testing.T) {
	t.Run("BVA-603 warnings keep 200 of 201 items and 500 of 501 UTF-16 units; a missing or non list value is an empty list", func(t *testing.T) {
		list := make([]any, 201)
		for i := range list {
			list[i] = strings.Repeat("é", 501)
		}
		w := warnings(map[string]any{"warnings": list})
		if len(w) != 200 || len([]rune(w[0])) != 500 {
			t.Errorf("len %d, first has %d runes", len(w), len([]rune(w[0])))
		}
		for _, req := range []map[string]any{{}, {"warnings": "x"}, {"warnings": nil}} {
			if w := warnings(req); w == nil || len(w) != 0 {
				t.Errorf("%v -> %v", req, w)
			}
		}
	})
}
```

`cmd/garmin-export-core/main_test.go`, add (the fixture path is the same one `TestBinaryGarminExport` uses; Task 7 changes both):

```go
func TestBinaryStage3Commands(t *testing.T) {
	t.Parallel()
	home, _ := os.UserHomeDir()
	root, err := os.MkdirTemp(home, ".garmin-stage3-test-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(root) })
	csvDir := filepath.Join(root, "csv_2024-02-21")
	if err := os.CopyFS(csvDir, os.DirFS("../../internal/pack/testdata/csv_2024-02-21")); err != nil {
		t.Fatal(err)
	}
	jsonPath := filepath.Join(root, "garmin_health.json")
	fx, err := os.ReadFile(filepath.Join("..", "..", "..", "..", "electron-app", "garmin", "__tests__", "fixtures", "export-sample.json"))
	if err != nil {
		t.Fatal(err)
	}
	os.WriteFile(jsonPath, fx, 0o644)
	line := func(v map[string]any) string { b, _ := json.Marshal(v); return string(b) + "\n" }

	t.Run("AC-606 summary with jsonPath, pack in both modes, report, and summary with csvDir only, through the binary", func(t *testing.T) {
		res := result(t, runBinary(t, "", line(map[string]any{"cmd": "summary", "jsonPath": jsonPath, "csvDir": csvDir, "warnings": []string{"w1"}})))
		s, _ := res["summary"].(map[string]any)
		p, _ := res["pack"].(map[string]any)
		if res["ok"] != true || s == nil || p == nil || len(s["days"].([]any)) != 5 || len(p["files"].([]any)) != 21 || p["warnings"].([]any)[0] != "w1" || p["days"] != 5.0 {
			t.Fatalf("summary: %v", res)
		}
		if p["langs"].(map[string]any)["es"].(map[string]any)["tokens"].(float64) <= 0 {
			t.Fatal("tokens")
		}
		res = result(t, runBinary(t, "", line(map[string]any{"cmd": "summary", "jsonPath": jsonPath})))
		if res["ok"] != true || res["summary"] == nil || res["pack"] != nil {
			t.Fatalf("jsonPath only: %v", res)
		}
		for _, mode := range []string{"files", "single"} {
			res = result(t, runBinary(t, "", line(map[string]any{"cmd": "pack", "mode": mode, "lang": "en", "csvDir": csvDir, "startDate": "2024-02-17", "endDate": "2024-02-21"})))
			if res["ok"] != true {
				t.Fatalf("pack %s: %v", mode, res)
			}
			if mode == "files" && (res["count"].(float64) != 22 || res["file"] != nil || res["dir"] != filepath.Join(csvDir, "llm-en")) {
				t.Fatalf("files: %v", res)
			}
			if mode == "single" && (res["count"].(float64) != 1 || !strings.HasSuffix(res["file"].(string), "garmin_2024-02-17_2024-02-21.en.md")) {
				t.Fatalf("single: %v", res)
			}
		}
		res = result(t, runBinary(t, "", line(map[string]any{"cmd": "report", "error": "x for a@b.com", "log": []map[string]string{{"ts": "1", "type": "dim", "msg": "Bearer abc.def.ghi"}}, "version": "4.5.0", "os": "mac"})))
		for _, k := range []string{"issueUrl", "mailtoUrl"} {
			u, _ := res[k].(string)
			if u == "" || strings.Contains(u, "a%40b.com") || strings.Contains(u, "abc.def.ghi") {
				t.Errorf("%s: %q", k, u)
			}
		}
		// An empty csv_* folder still gives an array for files, and no jsonPath gives summary null.
		empty := filepath.Join(root, "csv_2026-01-01")
		os.Mkdir(empty, 0o755)
		lines := runBinary(t, "", line(map[string]any{"cmd": "summary", "csvDir": empty, "startDate": "2026-01-01", "endDate": "2026-01-01"}))
		last, _ := json.Marshal(lines[len(lines)-1])
		if !strings.Contains(string(last), `"files":[]`) || !strings.Contains(string(last), `"summary":null`) {
			t.Errorf("empty folder: %s", last)
		}
	})

	t.Run("EG-608 pack refuses a folder outside $HOME, a folder not named csv_* and a missing folder before touching anything; summary keeps the cli.js sentences", func(t *testing.T) {
		notCsv := filepath.Join(root, "Documents")
		os.Mkdir(notCsv, 0o755)
		os.WriteFile(filepath.Join(notCsv, "keep.md"), []byte("x"), 0o644)
		for _, dir := range []string{"/tmp", notCsv, filepath.Join(root, "csv_missing"), ""} {
			res := result(t, runBinary(t, "", line(map[string]any{"cmd": "pack", "mode": "files", "lang": "en", "csvDir": dir, "startDate": "2026-01-01", "endDate": "2026-01-02"})))
			if res["ok"] != false || res["error"] != "csvDir must be an export csv_ folder within your home directory." || res["errorCode"] != nil {
				t.Errorf("%q: %v", dir, res)
			}
		}
		if _, err := os.Stat(filepath.Join(notCsv, "keep.md")); err != nil {
			t.Error("pack touched a folder it refused")
		}
		os.Mkdir(filepath.Join(root, "csv_x"), 0o755)
		if res := result(t, runBinary(t, "", line(map[string]any{"cmd": "pack", "mode": "files", "lang": "en", "csvDir": filepath.Join(root, "csv_x"), "startDate": "2026-1-1", "endDate": "2026-01-02"}))); res["error"] != "startDate and endDate must be YYYY-MM-DD." {
			t.Errorf("dates: %v", res)
		}
		if res := result(t, runBinary(t, "", line(map[string]any{"cmd": "pack", "mode": "zip", "lang": "en", "csvDir": filepath.Join(root, "csv_x"), "startDate": "2026-01-01", "endDate": "2026-01-02"}))); res["error"] != "mode must be files or single." {
			t.Errorf("mode: %v", res)
		}
		if res := result(t, runBinary(t, "", line(map[string]any{"cmd": "summary", "csvDir": "/tmp", "startDate": "2026-01-01", "endDate": "2026-01-02"}))); res["error"] != "csvDir must be a folder within your home directory." {
			t.Errorf("csvDir: %v", res)
		}
		bad := filepath.Join(root, "x.txt")
		os.WriteFile(bad, []byte("{}"), 0o644)
		if res := result(t, runBinary(t, "", line(map[string]any{"cmd": "summary", "jsonPath": bad}))); res["error"] != "jsonPath must be a .json file within your home directory." {
			t.Errorf("not json: %v", res)
		}
		corrupt := filepath.Join(root, "x.json")
		os.WriteFile(corrupt, []byte("{"), 0o644)
		if res := result(t, runBinary(t, "", line(map[string]any{"cmd": "summary", "jsonPath": corrupt}))); res["error"] != "Could not read export." {
			t.Errorf("corrupt: %v", res)
		}
		// A csvDir outside $HOME next to a valid jsonPath gives pack null, not an error, as cli.js did.
		if res := result(t, runBinary(t, "", line(map[string]any{"cmd": "summary", "jsonPath": jsonPath, "csvDir": "/tmp"}))); res["ok"] != true || res["pack"] != nil {
			t.Errorf("outside csvDir: %v", res)
		}
	})
}
```

- [ ] **Step 2: Run them.** Run: `cd tauri-app/core && go test ./internal/garmin -run 'Summarize' ; go test ./cmd/garmin-export-core` Expected: FAIL (`undefined: Summarize`, `undefined: tzName`).

- [ ] **Step 3: Implement.**

`internal/garmin/summary.go` (no `map[string]any` anywhere exported; `map[string]json.RawMessage` is allowed):

```go
package garmin

import (
	"encoding/json"
	"maps"
	"slices"
)

// Summary is the per day series for the preview cards, ported from cli.js:summarize. The UI
// reads range and days only, so types and activities from the JavaScript are not ported.
type Summary struct {
	Range json.RawMessage `json:"range"` // the file's date_range as it is; null when absent
	Days  []Day           `json:"days"`
}

// Day holds each value as the JSON the export file has, or null when any step of the path is missing.
type Day struct {
	Date       string          `json:"date"`
	Steps      json.RawMessage `json:"steps"`
	RHR        json.RawMessage `json:"rhr"`
	SleepScore json.RawMessage `json:"sleepScore"`
	SleepSec   json.RawMessage `json:"sleepSec"`
	HRV        json.RawMessage `json:"hrv"`
}

var null = json.RawMessage("null")

// Summarize reads an export file. The JavaScript returned null for anything that was not JSON.
func Summarize(raw []byte) (*Summary, error) {
	var data struct {
		DateRange json.RawMessage            `json:"date_range"`
		Daily     map[string]json.RawMessage `json:"daily"`
	}
	if err := json.Unmarshal(raw, &data); err != nil {
		return nil, err
	}
	s := &Summary{Range: data.DateRange, Days: make([]Day, 0, len(data.Daily))}
	if len(s.Range) == 0 {
		s.Range = null
	}
	for _, d := range slices.Sorted(maps.Keys(data.Daily)) {
		x := data.Daily[d]
		s.Days = append(s.Days, Day{Date: d,
			Steps: dig(x, "stats", "totalSteps"), RHR: dig(x, "heart_rates", "restingHeartRate"),
			SleepScore: dig(x, "sleep", "dailySleepDTO", "sleepScores", "overall", "value"),
			SleepSec: dig(x, "sleep", "dailySleepDTO", "sleepTimeSeconds"), HRV: dig(x, "hrv", "hrvSummary", "lastNightAvg")})
	}
	return s, nil
}

// Dates reads start and end from the range, for the pack; empty when the range has none.
func (s *Summary) Dates() (start, end string) {
	var r struct{ Start, End string }
	_ = json.Unmarshal(s.Range, &r)
	return r.Start, r.End
}

// dig is the optional chain x.a?.b?.c ?? null: null as soon as a step is not an object or the key is absent.
func dig(raw json.RawMessage, keys ...string) json.RawMessage {
	for _, k := range keys {
		var m map[string]json.RawMessage
		if json.Unmarshal(raw, &m) != nil || m[k] == nil {
			return null
		}
		raw = m[k]
	}
	return raw
}
```

`cmd/garmin-export-core/summary.go`:

```go
package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"regexp"
	"strings"
	"time"

	"github.com/danihrndzld/garmin-health-exporter/tauri-app/core/internal/garmin"
	"github.com/danihrndzld/garmin-health-exporter/tauri-app/core/internal/guard"
	"github.com/danihrndzld/garmin-health-exporter/tauri-app/core/internal/jsjson"
	"github.com/danihrndzld/garmin-health-exporter/tauri-app/core/internal/pack"
	"github.com/danihrndzld/garmin-health-exporter/tauri-app/core/internal/protocol"
)

var isoDate = regexp.MustCompile(`^\d{4}-\d{2}-\d{2}$`)

// warnings ports cli.js:cleanWarnings: at most 200 items of 500 UTF-16 units; anything that is not a list is empty.
func warnings(req map[string]any) []string {
	out := []string{}
	list, _ := req["warnings"].([]any)
	for _, w := range list[:min(200, len(list))] {
		s, ok := w.(string)
		if !ok {
			s = fmt.Sprint(w)
		}
		out = append(out, jsjson.SliceUTF16(s, 500))
	}
	return out
}

// tzName is what Intl.DateTimeFormat().resolvedOptions().timeZone gave the JavaScript. Go names the
// local zone "Local", so read $TZ, else where /etc/localtime points (macOS: .../zoneinfo/<name>),
// else the abbreviation. Only the Exported line of the context uses it.
func tzName() string {
	if tz := os.Getenv("TZ"); tz != "" {
		return tz
	}
	if link, err := os.Readlink("/etc/localtime"); err == nil {
		if i := strings.Index(link, "zoneinfo/"); i >= 0 {
			return link[i+len("zoneinfo/"):]
		}
	}
	return time.Now().Format("MST")
}

func packOptions(csvDir, start, end string, req map[string]any) pack.Options {
	return pack.Options{CSVDir: csvDir, StartDate: start, EndDate: end, Warnings: warnings(req), Now: time.Now(), TZ: tzName()}
}

// summaryResult: a nil pointer marshals as null, which is what the UI expects for a missing part.
type summaryResult struct {
	Summary *garmin.Summary `json:"summary"`
	Pack    *pack.Preview   `json:"pack"`
}

// summary ports cli.js:summary, with its three request shapes and its sentences. Failures carry no errorCode.
func summary(_ context.Context, req map[string]any, _ *protocol.Emitter) (any, error) {
	if req["jsonPath"] == nil {
		// A run with Garmin off has no JSON: only the pack, from the csv folder and the range.
		csvDir, err := guard.InsideHome(str(req, "csvDir"))
		if err != nil {
			return nil, errors.New("csvDir must be a folder within your home directory.")
		}
		start, end := str(req, "startDate"), str(req, "endDate")
		if !isoDate.MatchString(start) || !isoDate.MatchString(end) {
			return nil, errors.New("startDate and endDate must be YYYY-MM-DD.")
		}
		p, err := pack.BuildPreview(packOptions(csvDir, start, end, req))
		if err != nil {
			return nil, err
		}
		return summaryResult{Pack: p}, nil
	}
	p, err := guard.InsideHome(str(req, "jsonPath"))
	if err != nil || !strings.HasSuffix(p, ".json") {
		return nil, errors.New("jsonPath must be a .json file within your home directory.")
	}
	raw, err := os.ReadFile(p)
	var s *garmin.Summary
	if err == nil {
		s, err = garmin.Summarize(raw)
	}
	if err != nil {
		return nil, errors.New("Could not read export.")
	}
	res := summaryResult{Summary: s}
	// A csvDir that is absent or outside $HOME gives no pack, not an error, as cli.js did.
	if csvDir, err := guard.InsideHome(str(req, "csvDir")); err == nil {
		start, end := s.Dates()
		if res.Pack, err = pack.BuildPreview(packOptions(csvDir, start, end, req)); err != nil {
			return nil, err
		}
	}
	return res, nil
}
```

`cmd/garmin-export-core/pack.go`:

```go
package main

// buildPack ports cli.js:pack. Build deletes and recreates llm-* subfolders, so only an existing
// folder inside $HOME whose name starts with csv_ is accepted, and that is checked before anything else.
func buildPack(_ context.Context, req map[string]any, _ *protocol.Emitter) (any, error) {
	csvDir, err := guard.InsideHome(str(req, "csvDir"))
	if err == nil {
		var st os.FileInfo
		if st, err = os.Stat(csvDir); err == nil && (!st.IsDir() || !strings.HasPrefix(filepath.Base(csvDir), "csv_")) {
			err = errors.New("not an export folder")
		}
	}
	if err != nil {
		return nil, errors.New("csvDir must be an export csv_ folder within your home directory.")
	}
	start, end := str(req, "startDate"), str(req, "endDate")
	if !isoDate.MatchString(start) || !isoDate.MatchString(end) {
		return nil, errors.New("startDate and endDate must be YYYY-MM-DD.")
	}
	return pack.Build(packOptions(csvDir, start, end, req), str(req, "lang"), str(req, "mode"))
}
```

`cmd/garmin-export-core/report.go`:

```go
package main

// buildReport decodes the request through a typed struct; an absent version or os reads "?", as
// report.js defaulted them. A log line that is not {ts, type, msg} strings is an invalid request.
func buildReport(_ context.Context, req map[string]any, _ *protocol.Emitter) (any, error) {
	in := struct {
		Error   string        `json:"error"`
		Log     []report.Line `json:"log"`
		Version string        `json:"version"`
		OS      string        `json:"os"`
	}{Version: "?", OS: "?"}
	b, _ := json.Marshal(req)
	if err := json.Unmarshal(b, &in); err != nil {
		return nil, errors.New("Invalid report request.")
	}
	return report.Build(in.Error, in.Log, in.Version, in.OS), nil
}
```

`main.go`: three entries in `commands` (`"summary": summary, "pack": buildPack, "report": buildReport`; `pack` and `report` are package names, so the handlers are `buildPack` and `buildReport`), and the header comment becomes "The commands are in garmin.go, mfp.go, summary.go, pack.go and report.go."

- [ ] **Step 4: UI.** In `tauri-app/ui/app.js`: `run(req, onEvent, bin = 'binaries/garmin-export-core')`; delete the explicit third argument in the `clear-cache` call (`'binaries/garmin-export-core'`), in `runGarmin` (`engine() === 'go' ? GO : 'binaries/garmin-core'`), in `runWithoutGarmin` (`GO`), in `runMfp` (`'binaries/garmin-export-core'`) and in `goRun` (`GO`); delete the `GO` constant, `DEFAULT_ENGINE`, `engine` and the comment above them. In `runGarmin`, `const files = res.pack?.files || res.files; // Bun answers ...` becomes `const files = res.files;`, the two lines `if (res.summary) renderSummary(res.summary); pack = res.pack || null;` go, and the block guarded by `if (!res.summary || (m?.ok && !m.skipped))` runs unconditionally (Go never sends `summary`, so `!res.summary` was always true: the pack is refreshed after every run, whether MyFitnessPal ran, failed or was off): delete the `if` line and its closing brace, keep the comment as "The Go core answers without summary and pack: the previews stay empty until this summary paints them.", make the `data-empty` line unconditional (drop its `if (!res.summary) ` prefix), keep `const s = await refreshPack({ csvDir: res.path, jsonPath: res.jsonPath, warnings });`, and change `if (s.ok && s.summary && !res.summary)` to `if (s.ok && s.summary)`. Comments: "Every Garmin call goes to the bundled `garmin-core` sidecar" becomes "Every backend call goes to the bundled Go core `garmin-export-core`"; "Each button asks the sidecar to build ONE language" and "The sidecar scrubs secrets" and "Asks Bun's `summary`" name the Go core instead. In `tauri-app/ui/dev-mock.js`: `run(req, onEvent, bin = 'binaries/garmin-export-core')`, and the `export` answer keeps only the Go shape (delete the `if (bin === 'binaries/garmin-export-core')` test and the Bun return with `summary` and `pack`; the comment above becomes "The Go core answers with file names only; the UI asks `summary` for the rest."). The `runMfp` error path in the UI still calls `refreshPack` only when the MFP step ran; check the three branches by reading the function once after the edit.

- [ ] **Step 5: e2e.** `tauri-app/e2e/engine.spec.js`: the header comment becomes "The export run on the Go core: `export` answers with file names only and the UI asks `summary` for the rest, so the run ends on the same screen as before."; the `for` loop over `['ST-T19', 'go'], ['ST-T20', 'bun']` becomes one plain `test('ST-T19 Running → Done with the csv count, the summary and the warnings in the pack', ...)` without `scenario.engine(engine)`; ST-T21 and EG-11 drop `scenario.engine('go')` (ST-T21 keeps `scenario.mfpConnected()`). `tauri-app/e2e/support/mock-backend.js`: delete `engine(name)` and its comment. Then `bun scripts/test-ids.js` Expected: `0 duplicates, 0 not in a test name` (ST-T20 is gone, nothing else names it).

- [ ] **Step 6: Run everything.** Run: `cd tauri-app && bun run test` (test IDs, Go, `bun test sidecar` including the golden pin, Playwright) Expected: PASS. The Playwright run is the gate for Steps 4 and 5.

- [ ] **Step 7: Try it in the app (needs the user at the app; no sign in).** `bun run dev`. With the saved Garmin login, export 7 days, open the LLM pack sheet with `L`, press both build buttons in both languages, and check that `llm-es/`, `llm-es-one-file/`, `llm-en/`, `llm-en-one-file/` appear in the `csv_*` folder. Then switch Garmin off and press Pull: the previews come from `summary` with `csvDir` only. Then, with the app closed, rename `tokens.json`, open the app and press Pull: the bug report box must show both links and the issue link must open GitHub with the log, without the email or the home folder. Rename `tokens.json` back.

- [ ] **Step 8: Optional cross check on a real folder (both binaries still ship).** With the app closed and one real `csv_*` folder `F` copied twice (`F1`, `F2`): run `pack` with Bun (`echo '{"cmd":"pack","mode":"single","lang":"en","csvDir":"F1","startDate":"...","endDate":"..."}' | tauri-app/src-tauri/binaries/garmin-core-aarch64-apple-darwin`) and with Go (`... | tauri-app/src-tauri/binaries/garmin-export-core-aarch64-apple-darwin`), then `diff <(grep -v '^Exported:' F1/llm-en-one-file/*.md) <(grep -v '^Exported:' F2/llm-en-one-file/*.md)`. Expected: no output. Record the result in the commit message of Step 9.

- [ ] **Step 9: Commit.**

```bash
git add tauri-app/core tauri-app/ui tauri-app/e2e
git -c user.name="Daniel Hernández" -c user.email=danisnowman@gmail.com commit -m "feat: summary, pack and report run in Go; the UI calls one binary and loses the engine switch"
```

---

### Task 6: Bun leaves the build

Second shippable increment. After this task `bun build --compile` is never run, `tauri-app/sidecar/` does not exist, and the DMG holds one external binary.

**Files:**
- Modify: `tauri-app/package.json`, `tauri-app/src-tauri/tauri.conf.json`, `tauri-app/src-tauri/capabilities/default.json`, `tauri-app/playwright.config.js`, `tauri-app/.gitignore`, `cmd/garmin-export-core/main.go` (header), `internal/protocol/protocol.go` (header)
- Move: `electron-app/renderer/date-range.js` → `tauri-app/ui/date-range.js`; `electron-app/renderer/__tests__/date-range.test.js` → `tauri-app/ui/__tests__/date-range.test.js`
- Delete: `tauri-app/sidecar/` (`cli.js`, `llm-pack.js`, `report.js`, `golden.test.js`, `e2e.test.js`, `garmin-e2e.test.js`, `mfp-e2e.test.js`, `llm-pack.test.js`, `report.test.js`), `tauri-app/core/cmd/fake-mfp/`

- [ ] **Step 1: Check that nothing still needs the sidecar.** Run: `rg -n "garmin-core'|garmin-core\"|binaries/garmin-core[^-]|sidecar" tauri-app --glob '!node_modules' --glob '!sidecar'` Expected: only `package.json`, `tauri.conf.json`, `capabilities/default.json`, the two Go header comments, `main.rs` line 1 (Task 10) and `dev-mock.js` comments. Anything in `app.js` logic is a Task 5 miss: fix it first.

- [ ] **Step 2: date-range.js becomes a tracked UI file.**

```bash
sed -i '' '/^ui\/date-range.js$/d' tauri-app/.gitignore
rm -f tauri-app/ui/date-range.js
git mv electron-app/renderer/date-range.js tauri-app/ui/date-range.js
mkdir -p tauri-app/ui/__tests__
git mv electron-app/renderer/__tests__/date-range.test.js tauri-app/ui/__tests__/date-range.test.js
```

Edit the header comment of `date-range.js` (it names the Electron renderer and main process): "Pure helpers for the Date Range section of `ui/app.js`. The Go core checks the range again at the trust boundary (`internal/guard`)." The test's `require('../date-range')` already resolves. Run: `cd tauri-app && bun test ui/__tests__` Expected: `14 pass` (checked 2026-10-16 in a scratch copy). Never a bare `bun test`: it would also collect `e2e/*.spec.js`.

- [ ] **Step 3: Scripts and configuration.**

`tauri-app/package.json` scripts, replace the block with:

```json
"core": "cd core && CGO_ENABLED=0 go build -trimpath -ldflags '-s -w' -o ../src-tauri/binaries/garmin-export-core-$(rustc -vV | awk '/host/{print $2}') ./cmd/garmin-export-core",
"core:universal": "cd core && for a in arm64:aarch64 amd64:x86_64; do CGO_ENABLED=0 GOOS=darwin GOARCH=${a%%:*} go build -trimpath -ldflags '-s -w' -o ../src-tauri/binaries/garmin-export-core-${a##*:}-apple-darwin ./cmd/garmin-export-core || exit 1; done && lipo -create -output ../src-tauri/binaries/garmin-export-core-universal-apple-darwin ../src-tauri/binaries/garmin-export-core-aarch64-apple-darwin ../src-tauri/binaries/garmin-export-core-x86_64-apple-darwin",
"test": "node scripts/test-ids.js && bun run test:core && bun run test:ui && bunx playwright test",
"test:core": "cd core && go vet ./... && go test ./...",
"test:ui": "bun test ui/__tests__",
"dev": "tauri dev",
"build": "tauri build",
"tauri": "tauri",
"release": "tauri build --target universal-apple-darwin"
```

`tauri.conf.json`: `"beforeDevCommand": "bun run core"`, `"beforeBuildCommand": "bun run core:universal"`, `"externalBin": ["binaries/garmin-export-core"]`.

`capabilities/default.json`: remove the `binaries/garmin-core` object from `shell:allow-spawn`.

`playwright.config.js` `webServer.command`: `` `PORT=${PORT} bun e2e/support/serve-ui.js` `` and delete the comment above it.

- [ ] **Step 4: Delete the sidecar and the fake MFP binary.**

```bash
git rm -r tauri-app/sidecar tauri-app/core/cmd/fake-mfp
```

`cmd/garmin-export-core/main_test.go` already runs the binary against `mfptest` in process (`scenario`), which is what `mfp-e2e.test.js` did; AC-11 and AC-12 of `garmin-e2e.test.js` are covered by the stage 2 binary tests against `garmintest`; the Go goldens of Tasks 3 and 4 replace `golden.test.js`. Update the `main.go` header comment to "garmin-export-core: the whole backend of Garmin Export (Garmin, MyFitnessPal, the LLM pack and the bug report)." and the `protocol.go` package comment ("shared with the Bun sidecar") to "one JSON request line in; JSON lines out with kind log, progress or result".

- [ ] **Step 5: Build the universal app in a worktree and measure.**

```bash
git worktree add ../../.worktrees/stage3-build HEAD
cd ../../.worktrees/stage3-build/tauri-app && bun install && bun run release
ls -l src-tauri/target/universal-apple-darwin/release/bundle/dmg/*.dmg
ls -l "src-tauri/target/universal-apple-darwin/release/bundle/macos/Garmin Export.app/Contents/MacOS/"
lipo -info src-tauri/binaries/garmin-export-core-universal-apple-darwin
codesign --verify --deep --strict "src-tauri/target/universal-apple-darwin/release/bundle/macos/Garmin Export.app" && echo signed
```

Expected: one DMG; `Contents/MacOS` holds `garmin-export` and `garmin-export-core` only (no `garmin-core`); `lipo -info` says `x86_64 arm64`; `codesign` prints `signed`. Write the DMG size into the commit message. Then check assumption A5: `bun run build` (host target) in the same worktree must also succeed with `beforeBuildCommand: bun run core:universal` (it leaves `garmin-export-core-aarch64-apple-darwin` on disk, which the host build looks for); if Tauri rejects it, keep `"build": "bun run core && tauri build --config '{\"build\":{\"beforeBuildCommand\":\"\"}}'"` and say so in the commit. Remove the worktree afterwards: `git worktree remove --force ../../.worktrees/stage3-build`.

- [ ] **Step 6: Run the app from the worktree build once (needs the user at the app; no sign in).** Open the built `.app` (not the DMG install), export 7 days with the saved login, build one pack. Expected: works; the log shows Go timestamps (`20:53:01`) for every line.

- [ ] **Step 7: Full gate without `electron-app/node_modules`.** Run: `rm -rf electron-app/node_modules && cd tauri-app && bun run test && (cd src-tauri && cargo test)` Expected: PASS with the folder gone (Review Focus 7). `cd electron-app && npm test` leaves the gate: the sidecar tests that compared engines are gone, and the Go goldens are the oracle from Task 7 on.

- [ ] **Step 8: Commit.**

```bash
git add -A tauri-app electron-app
git -c user.name="Daniel Hernández" -c user.email=danisnowman@gmail.com commit -m "build: the Go core is the only external binary; the Bun sidecar and bun build --compile are gone (universal DMG <size> MB)"
```

---

### Task 7: Go owns the fixture, the goldens and the two scripts

**Files:**
- Move: `electron-app/garmin/__tests__/fixtures/export-sample.json` → `tauri-app/core/internal/garmin/testdata/fixtures/export-sample.json`; `electron-app/garmin/__tests__/golden/` → `tauri-app/core/internal/garmin/testdata/golden/`; `electron-app/scripts/make-fixture.js`, `electron-app/scripts/compare-csv.js` → `tauri-app/scripts/`
- Modify: `internal/garmin/csv_test.go` (`fixturePath`, `goldenDir`, `UPDATE_GOLDEN`), `internal/garmin/garmintest/fixture_test.go` (`fixturePath`), `cmd/garmin-export-core/main_test.go` (the fixture path in `TestBinaryGarminExport` and `TestBinaryStage3Commands`, the golden path in `TestBinaryGarminExport`), the two scripts' header comments

- [ ] **Step 1: Move.**

```bash
mkdir -p tauri-app/core/internal/garmin/testdata/fixtures tauri-app/scripts
git mv electron-app/garmin/__tests__/fixtures/export-sample.json tauri-app/core/internal/garmin/testdata/fixtures/export-sample.json
git mv electron-app/garmin/__tests__/golden tauri-app/core/internal/garmin/testdata/golden
git mv electron-app/scripts/make-fixture.js electron-app/scripts/compare-csv.js tauri-app/scripts/
```

- [ ] **Step 2: Paths and `UPDATE_GOLDEN`.** Four sites read the old paths (`rg -n "electron-app" tauri-app/core --glob '*_test.go'` lists them):
- `internal/garmin/csv_test.go`: `fixturePath = filepath.Join("testdata", "fixtures", "export-sample.json")`, `goldenDir = filepath.Join("testdata", "golden")`.
- `internal/garmin/garmintest/fixture_test.go`: `fixturePath = filepath.Join("..", "testdata", "fixtures", "export-sample.json")`.
- `cmd/garmin-export-core/main_test.go`: both `filepath.Join(repo, "electron-app", "garmin", "__tests__", "fixtures", "export-sample.json")` and the Task 5 copy become `filepath.Join("..", "..", "internal", "garmin", "testdata", "fixtures", "export-sample.json")`; `filepath.Join(repo, "electron-app", "garmin", "__tests__", "golden")` becomes `filepath.Join("..", "..", "internal", "garmin", "testdata", "golden")`; delete `repo` if nothing else uses it.

In `csv_test.go`, `TestAC301CSVFilesEqualTheGoldenFilesByteForByte` gains, right after `writeFixtureCSVs`, the pattern `internal/mfp/csv_test.go` already uses:

```go
		if os.Getenv("UPDATE_GOLDEN") != "" { // on purpose only: UPDATE_GOLDEN=1 go test ./internal/garmin -run AC301
			os.RemoveAll(goldenDir)
			if err := os.CopyFS(goldenDir, os.DirFS(csvDir)); err != nil {
				t.Fatal(err)
			}
		}
```

Run: `cd tauri-app/core && go test ./... && go vet ./...` Expected: PASS without `UPDATE_GOLDEN` (the files moved, not changed). Do not run with `UPDATE_GOLDEN=1`.

- [ ] **Step 3: Scripts.** Change the usage lines to `bun tauri-app/scripts/compare-csv.js <oldDir> <newDir> [--json]` and `bun tauri-app/scripts/make-fixture.js <garmin_health_*.json> tauri-app/core/internal/garmin/testdata/fixtures/export-sample.json`, and in `make-fixture.js` the sentence "so the regression test sees real Garmin response shapes" names `internal/garmin/csv_test.go`. Run: `bun tauri-app/scripts/compare-csv.js tauri-app/core/internal/garmin/testdata/golden tauri-app/core/internal/pack/testdata/csv_2024-02-21` Expected: exit 1 and a report that lists the 3 added `mfp_*` files and no differing columns (the script runs under Bun as it did under Node).

- [ ] **Step 4: Commit.**

```bash
git add -A tauri-app electron-app
git -c user.name="Daniel Hernández" -c user.email=danisnowman@gmail.com commit -m "test(core): Go owns the Garmin fixture and golden files; UPDATE_GOLDEN moves to the Go test; the two CSV scripts move to tauri-app/scripts"
```

---

### Task 8: `electron-app/` is deleted (decision D1)

**Files:**
- Delete: `electron-app/` (everything left: `garmin/`, `util/`, `renderer/`, `main.js`, `preload.js`, `scripts/download-fonts.js`, `BUILD.md`, `package.json`, `package-lock.json`, `Remove Quarantine.command`)
- Modify: `.gitignore`

- [ ] **Step 1: Tag the archive point.** `git tag -a archive/electron-app -m "Last commit with electron-app/ (Electron 3.x app and the JavaScript Garmin core used by 4.0 to 4.4)"`. The tag is pushed with the branch in Task 11.

- [ ] **Step 2: Check for stragglers.** Run: `rg -n "electron-app" --glob '!node_modules' --glob '!.worktrees' --glob '!docs/superpowers' .` Expected: `CLAUDE.md`, `README.md`, `tools/*.py`, `workflows/*.md` and `.gitignore` (`electron-app/.omc/`), which Tasks 9 and 10 edit, plus Go and JavaScript comments that name the JavaScript file a function was ported from (`internal/garmin/csv.go`, `tokensfile.go`, `tokensfile_test.go`, `csv_test.go`, `ui/dev-mock.js`, `e2e/range.spec.js`). Those comments stay: the files live at the tag, which CLAUDE.md names in Task 10. Any other hit under `tauri-app/` is a bug: fix it first.

- [ ] **Step 3: Delete.**

```bash
git rm -r electron-app
sed -i '' '/^electron-app\/.omc\/$/d' .gitignore
```

- [ ] **Step 4: Gate.** Run: `cd tauri-app && bun run test` Expected: PASS.

- [ ] **Step 5: Commit.**

```bash
git add -A
git -c user.name="Daniel Hernández" -c user.email=danisnowman@gmail.com commit -m "chore: remove electron-app; the 3.x app and the JavaScript core stay at tag archive/electron-app"
```

---

### Task 9: Tools and workflows for the Tauri app (decision D2)

**Files:**
- Rewrite: `tools/ci_pipeline.py`, `tools/release.py`, `workflows/cicd_pipeline.md`, `workflows/release.md`
- Modify: `tauri-app/.gitignore` (add `dist/`)
- Create at run time: `tauri-app/dist/` (gitignored: the DMG and the `.app` the pipeline copies out of the worktree)

`tools/ci_pipeline.py` keeps its name, its PEP 723 header, its exit codes and its flags (`--stages`, `--skip-clean`, `--skip-install`, `--skip-tests`, `--skip-build`, `--no-open`), and gets `--here` (build in this checkout instead of a worktree; only when the user says the app is closed). Stages:

| Stage | What it does | Failure means |
| --- | --- | --- |
| clean | `rm -rf tauri-app/dist tauri-app/src-tauri/binaries` and the `bundle/` folders under `src-tauri/target/*/release/`; never the cargo cache | file permissions |
| install | `bun install` in `tauri-app/` | registry or lockfile |
| test | `bun run test` (test IDs, Go, `ui/__tests__`, Playwright) then `cargo test` in `src-tauri/` | regression: stop |
| build | `git worktree add .worktrees/build-<sha> HEAD`, `bun install`, `bun run release` there, copy `src-tauri/target/universal-apple-darwin/release/bundle/dmg/Garmin Export_<ver>_universal.dmg` and `.../bundle/macos/Garmin Export.app` to `tauri-app/dist/`, `git worktree remove --force`; with `--here`, the same commands in this checkout | Go, Rust or bundler error |
| open | `codesign --verify --deep --strict` on `tauri-app/dist/Garmin Export.app`, then `open -n` | signature or launch |

`tools/release.py` keeps its flags (`--bump`, `--version`, `--source`, `--notes`, `--notes-file`, `--dry-run`, `--skip-merge`, `--skip-build`) and adds nothing. Stages: preflight (clean tree, `gh auth status`, no tag or release `vX.Y.Z`, `tauri-app/package.json` present), bump (`version` in `tauri-app/package.json` and `tauri-app/src-tauri/tauri.conf.json` through `json.loads` and a narrow `re.subn`, the first `version = "..."` of `tauri-app/src-tauri/Cargo.toml`; refresh `Cargo.lock` offline, assumption A6, candidate `cargo update --workspace --offline` in `src-tauri/`; commit `chore(release): bump to vX.Y.Z` with the four files), merge (unchanged), build (`ci_pipeline.py --no-open`), tag (unchanged), release (`gh release create vX.Y.Z "tauri-app/dist/Garmin Export_X.Y.Z_universal.dmg" --title ... --notes ...`; GitHub stores the asset as `Garmin.Export_X.Y.Z_universal.dmg`, as it did for v4.4.0). `--skip-build` checks that one DMG, not two. The default notes name the one universal DMG and the Gatekeeper note (ad hoc signature: right click, Open, the first time).

- [ ] **Step 1: Dry run the old tool once** to keep its output shape: `uv run tools/release.py --bump minor --dry-run | head -20`. Keep the `==>` and `  $` log lines.

- [ ] **Step 2: Rewrite `ci_pipeline.py`** (about 180 lines) and **`release.py`** (about 250 lines). The `--dry-run` path prints every command. Add `dist/` to `tauri-app/.gitignore`.

- [ ] **Step 3: Test the tools without releasing.** Run: `uv run tools/ci_pipeline.py --stages test` (PASS), `uv run tools/ci_pipeline.py --skip-install --skip-tests --no-open` (a DMG and an `.app` in `tauri-app/dist/`), `uv run tools/release.py --bump minor --dry-run` (prints the commands, changes nothing, `git status` clean). For A6, run the bump stage on a throwaway branch and check `git diff --stat` lists `package.json`, `tauri.conf.json`, `Cargo.toml` and `Cargo.lock`; if `Cargo.lock` is missing, move its refresh to after the build stage (the build regenerates it) and write the actual command into the workflow. Delete the throwaway branch.

- [ ] **Step 4: Workflows.** Rewrite both files with the same sections (Objective, Required Inputs, Tool, Stage contract, Invocation patterns, Expected Outputs, Edge Cases, Self-Improvement Log). Edge cases that stay true: non fast forward merge, `gh auth`, existing tag, mid flight abort. New edge cases: the worktree build is cold (minutes); `--here` only when the app is closed; the universal build needs the two rustup targets and v4.0.0 used an isolated rustup (`RUSTUP_HOME`/`CARGO_HOME`) because Homebrew's Rust lacks `x86_64-apple-darwin`; ad hoc signing (`signingIdentity: "-"`) means Gatekeeper asks on first open and the release notes say so; `gh release create` may be refused by the agent's permission classifier, in which case the tool has printed the exact command and the user runs it. Start each Self-Improvement Log with one line: "2026-10-16: rewritten for `tauri-app/`; the electron-builder entries live in git history before this date."

- [ ] **Step 5: Commit.**

```bash
git add tools workflows tauri-app/.gitignore
git -c user.name="Daniel Hernández" -c user.email=danisnowman@gmail.com commit -m "tools: ci_pipeline and release target the Tauri app; universal DMG built in a worktree"
```

---

### Task 10: Docs

**Files:**
- Modify: `CLAUDE.md`, `README.md`, `tauri-app/src-tauri/src/main.rs` (header comment)

- [ ] **Step 1: CLAUDE.md.**
- WAT Layout: "`electron-app/dist/` for app builds" becomes "`tauri-app/dist/`".
- Available Workflows and Tools tables: the Task 9 descriptions ("Rebuild the Tauri app: clean, install, test, universal DMG in a worktree, launch"; "Cut a release: bump the three version files, FF merge, build, tag, GitHub release with the universal DMG"; `--here` in the flag list).
- Project Context, first bullet: the `tauri-app/` paragraph without the sidecar. Keep: vanilla UI, dev and build commands (`cd tauri-app && bun install && bun run dev`; `bun run build`; `bun run release` with the rustup note), the worktree rule, `ui/dev-mock.js`. Add: `ui/date-range.js` is a tracked file; `beforeBuildCommand` builds the universal Go binary; v4 releases go through `tools/release.py` from 4.5.0 on.
- Go core bullet: all commands (`garmin-status`, `garmin-connect`, `garmin-disconnect`, `clear-cache`, `export`, `mfp-export`, `summary`, `pack`, `report`), the packages (`internal/garmin`, `mfp`, `pack`, `report`, `guard`, `jsjson`, `protocol`, `atomicfile`; redaction from `gconnect`), the testdata paths, `UPDATE_GOLDEN=1 go test ./internal/garmin -run AC301` as the only way to rebuild the Garmin goldens, the stage 3 goldens frozen from the JavaScript on 2026-10-16 (never regenerated: the JavaScript is gone), `GARMIN_FAKE_SERVER` and `MFP_FAKE_SERVER`. Delete "`summary`, `pack` and `report` stay in Bun until stage 3", "The UI picks the binary for `export` with `localStorage.ge_engine`" and "Go reads the golden CSV files in `electron-app/garmin/__tests__/golden`".
- Go layout bullet: add `internal/pack` and `internal/report` to the wire rule list; the zone name for the pack is resolved in `cmd/`.
- Garmin login bullet: "ported from `electron-app/garmin/auth.js`" becomes "ported from the JavaScript at tag `archive/electron-app`"; keep the 429 warning.
- Tests bullet: `cd tauri-app && bun run test && (cd src-tauri && cargo test)`; fixture via `bun tauri-app/scripts/make-fixture.js`; compare via `bun tauri-app/scripts/compare-csv.js`; Go IDs 101+ and 2xx to 3xx (stages 1 and 2), 601+ (stage 3); `bun scripts/test-ids.js` runs in `bun run test`.
- LLM pack bullet: `internal/pack` (`units` is the `UNITS` table), the sheet's buttons call the Go `pack` command.
- Delete the three `electron-app` bullets and the BUILD.md link; add "The Electron 3.x app and the JavaScript core are at tag `archive/electron-app`; Go comments that name a `.js` file refer to that tag."

- [ ] **Step 2: README.** "Build it yourself": prerequisites become Bun, Rust, Go 1.24 or later and the Xcode command line tools; the commands become `cd tauri-app && bun install && bun run dev`; tests `cd tauri-app && bun run test`; the paragraph that compares output with `electron-app/garmin/__tests__/golden` names `tauri-app/core/internal/garmin/testdata/golden` and `UPDATE_GOLDEN=1 go test ./internal/garmin -run AC301` in `tauri-app/core`; the fixture sentence names `bun scripts/make-fixture.js`. "Project layout": the `tauri-app/core` bullet adds the summary, the LLM pack and the bug report and names `github.com/danihrndzld/gconnect` as the library; the `tauri-app/sidecar` and `electron-app/garmin` bullets go.

- [ ] **Step 3: Comments.** `main.rs` line 1: "Garmin work happens in the Go core; Rust only handles the MyFitnessPal sign in window."

- [ ] **Step 4: Commit.**

```bash
git add CLAUDE.md README.md tauri-app/src-tauri/src/main.rs
git -c user.name="Daniel Hernández" -c user.email=danisnowman@gmail.com commit -m "docs: one Go core, no sidecar; build, test and release instructions for the Tauri app"
```

---

### Task 11: Full regression, build, smoke, release 4.5.0 (decision D4)

**Files:** `tauri-app/package.json`, `tauri.conf.json`, `Cargo.toml`, `Cargo.lock` (version, by the tool), `tauri-app/reportes-pruebas/<date>-stage3/` (gitignored evidence)

- [ ] **Step 1: Regression with evidence.**

```bash
D=tauri-app/reportes-pruebas/$(date +%F)-stage3 && mkdir -p $D
(cd tauri-app && bun scripts/test-ids.js 2>&1 | tee $D/test-ids.txt)
(cd tauri-app/core && go vet ./... && go test ./... -v 2>&1 | tee ../$D/go-test.txt | tail -5)
(cd tauri-app && bun test ui/__tests__ 2>&1 | tee $D/ui-test.txt | tail -3)
(cd tauri-app && bunx playwright test --reporter=list,html 2>&1 | tee $D/playwright.txt | tail -3 && cp -r playwright-report $D/)
(cd tauri-app/src-tauri && cargo test 2>&1 | tee ../$D/cargo-test.txt | tail -3)
```

Expected: `0 duplicates, 0 not in a test name`; `ok` for every Go package; `14 pass` in Bun; Playwright green on chromium and webkit; cargo `test result: ok`. Count the Go tests whose names carry 601 and above: 21 (AC-601 to AC-606, EP-601, EP-602, BVA-601 to BVA-603, ST-T601, DT-R601, EG-601 to EG-608): `rg -c '"(AC|EP|BVA|ST-T|DT-R|EG)-6[0-9]{2} ' tauri-app/core --glob '*_test.go'`.

- [ ] **Step 2: Build.** `uv run tools/ci_pipeline.py --skip-install --skip-tests --no-open`. Record the DMG size next to the 62 MB of v4.3.0 and the size of v4.4.0 (`gh release view v4.4.0 --json assets`).

- [ ] **Step 3: Smoke in the built app (needs the user at the app; no sign in).** Open `tauri-app/dist/Garmin Export.app`. Garmin row shows Connected from the saved login. Export the last 7 days: rows Done, CSV files in `csv_<today>`, the preview cards filled from `summary`. `L`, build the folder pack in Spanish and the one file pack in English; check by eye: `llm-es/` has `CONTEXT.es.md` plus the CSV copies, `llm-en-one-file/` has one `.md` whose `Exported:` line names the Mac's zone. Close the app, rename `tokens.json` to force `GARMIN_SESSION` on the next Pull, open the app, Pull, open the GitHub link from the report box: the issue body has the log, no email, `~` instead of the home folder. Rename `tokens.json` back. Clear cache: toast "Cache cleared."

- [ ] **Step 4: Release.** `uv run tools/release.py --bump minor --notes-file <notes>` with notes that say: one Go core, no Bun sidecar, DMG size before and after, same files on disk, Gatekeeper note unchanged. Expected: tag `v4.5.0`, one asset `Garmin.Export_4.5.0_universal.dmg`. The tool pushes the branch, `main` and the tags (`archive/electron-app` goes with `git push origin --tags` if the tool's `git push origin <tag>` left it behind; check with `git ls-remote --tags origin`). If the permission classifier blocks `gh release create`, the tool has printed the command: the user runs it by hand.

- [ ] **Step 5: Merge and clean up.** The release tool's FF merge lands the branch on `main`; then `git worktree remove .worktrees/go-core-stage3`.

---

## Pre-flight scan (2026-10-16)

Scanned against the tree at `6d56875`: every file, function, path and ID named above was read or run on 2026-10-16. Rows name what one task produces, what the other consumes, and the finding; every conflict is resolved in the task text above.

| Pair | Produces | Consumes | Finding |
| --- | --- | --- | --- |
| Task 1 → Task 3 | `internal/pack/testdata/csv_2024-02-21/` (21 files), `golden/CONTEXT.*.md`, `golden/garmin_*.md`, `golden/preview.json` | `pack_test.go` AC-602, AC-603 read the same names; `preview.json` fields `startDate, endDate, days, files, warnings, langs{bytes,tokens,singleName}` match the Go `want` struct | Consistent. `csvDir` is left out of `preview.json` on purpose and not compared. |
| Task 1 → Task 4 | `internal/report/testdata/input.json` (`error, log[{ts,type,msg}], version, os`), `golden.json` (`issueUrl, mailtoUrl`), `cases.json` (28 of `{fn,in,out}`) | `report_test.go` `input` struct and `want map[string]string`; `redact_test.go` `[]struct{Fn, In, Out}` (`encoding/json` matches the lower case keys) | Consistent. The old plan put `cases.json` under `internal/redact/testdata`; that package does not exist, so Task 1 writes it to `internal/report/testdata` and Task 4 reads it there. |
| Task 1 → Task 6 | `sidecar/golden.test.js` pins the JavaScript (AC-18, AC-19, AC-20) | Task 6 deletes it with the folder | Safe: Tasks 3 and 4 pin Go to the same files before the deletion, and no Go comment names AC-18 to AC-20, so `test-ids.js` stays at 0 "not in a test name". |
| Task 2 → Task 4, Task 5 | `jsjson.SliceUTF16(s, n)` | `report.Build` (title, 120 units), `cmd` `warnings` (500 units) | Consistent; `unicode/utf16` keeps `jsjson` a leaf package. |
| Task 2 → Task 3, Task 4, Task 5 | `wirePackages` holds `internal/pack`, `internal/report` | `pack.Options`, `pack.Preview` (`map[string]Lang`), `report.Line`, `report.Result`, `garmin.Summary`, `garmin.Day` hold no `map[string]any`; `map[string]json.RawMessage` inside `Summarize` is unexported and not the banned type | Consistent. The old plan's `Summary.Days []map[string]any` and `Command(req map[string]any)` would have failed CL-268/CL-274; both are gone. |
| Task 3 → Task 5 | `pack.Options{CSVDir, StartDate, EndDate, Warnings, Now, TZ}`, `BuildPreview(o) (*Preview, error)`, `Build(o, lang, mode) (*Result, error)` | `cmd/summary.go` `packOptions` fills every field (`Now: time.Now()`, `TZ: tzName()`), `summary` and `buildPack` call them | Consistent. The old plan's `pack.TZName()` with `os.Getenv` would have failed CL-266; it is `tzName()` in `cmd/` (EG-607 moved there, no `t.Parallel`). |
| Task 3 → Task 5 | `pack.Result{Dir, File omitempty, Count}` | AC-606 expects no `file` key in files mode and `count` 22 / 1 | Consistent: `omitempty` drops the empty `File`; `Emitter.Result` marshals the struct and merges it. |
| Task 4 → Task 5 | `report.Build(err, log, version, os) Result`, `report.Line` json tags `ts,type,msg` | `cmd/report.go` decodes `req` into `[]report.Line` through `json.Marshal` and returns `Result` | Consistent. Defaults `?` are set before `Unmarshal`, so an absent key keeps them. |
| Task 4 ↔ gconnect v0.1.2 | `gconnect.Redact`, `RedactURL`, `IsSecretKey` exported (v0.1.2, tagged 2026-10-16) | `report.Scrub` needs the key test for `k=v` pairs | Resolved: Task 4 Step 0 takes v0.1.2 and `Scrub` calls `gconnect.IsSecretKey`; AC-601 pins the library to `cases.json`. |
| Task 5 → Task 6 | `app.js` default `bin = 'binaries/garmin-export-core'`, no `GO`/`engine`; `dev-mock.js` default and one `export` shape; `engine.spec.js` without ST-T20; `mock-backend.js` without `engine()` | Task 6 removes `binaries/garmin-core` from `tauri.conf.json` and `capabilities/default.json` and the `sidecar*` scripts | Consistent: after Task 5 nothing spawns `garmin-core`; Task 6 Step 1 greps for leftovers. The old plan and spec said "dev-mock.js unchanged": wrong, its `export` answer branched on the binary name. |
| Task 5 → Task 7 | `main_test.go` `TestBinaryStage3Commands` reads the fixture at the `electron-app` path, `summary_test.go` reads `fixturePath` | Task 7 changes `fixturePath` (csv_test.go, one place for the `garmin` package), `garmintest/fixture_test.go`, and both `main_test.go` sites | Consistent; the old plan listed two sites, the tree has four. |
| Task 5 → Task 7 | `csv_test.go` `TestAC301CSVFilesEqualTheGoldenFilesByteForByte` (stage 2's AC-301) | Task 7 adds `UPDATE_GOLDEN` there, run with `-run AC301` | Consistent; the old plan's `-run AC01` named no test. |
| Task 6 → Task 7 | `package.json` `test` script = test IDs, Go, `ui/__tests__`, Playwright | Task 7's `compare-csv.js` under `tauri-app/scripts/` is not a test file, so `test-ids.js` ignores it | Consistent. |
| Task 6 → Task 8 | `date-range.js` and its test moved out of `electron-app/renderer` | Task 8 deletes `electron-app/` | Ordered correctly; `index.html` already loads `date-range.js` from `ui/`. |
| Task 7 → Task 8 | fixture, goldens and scripts moved out of `electron-app/` | Task 8 deletes the rest | Ordered correctly; Task 8 Step 2 lists the comment hits that may stay. |
| Task 8 → Task 9 | `tools/*.py` and `workflows/*.md` still name `electron-app/` after Task 8 | Task 9 rewrites them | Expected and listed in Task 8 Step 2; nothing runs the old tools between the two tasks. |
| Task 9 → Task 10 | tool stage names and flags (`--here`), `tauri-app/dist/` | CLAUDE.md tool and workflow tables | Consistent. |
| Task 9 → Task 11 | `release.py` bumps 4 files, builds through `ci_pipeline.py`, attaches `Garmin Export_X.Y.Z_universal.dmg` | Task 11 Step 4 runs it with `--bump minor` from 4.4.0 | Consistent: 4.4.0 is on `main` and released (2026-10-16), so `minor` gives 4.5.0 (D4). |
| Task 1 ↔ Task 11 | the golden `Exported:` line is `America/Guatemala` | the smoke check expects the Mac's zone in a real pack | Consistent: `/etc/localtime` points at `America/Guatemala` on this Mac (checked 2026-10-16), and `tzName()` reads that link. |
| Task 0 (self) | `bun run test` and `npm test` green at `6d56875` | | Go verified green on 2026-10-16 (`go vet ./... && go test ./...`, 7.8 s); the Bun and Playwright runs need the Step 2 install first (no `node_modules` in the worktree). |
| Task 1 (self) | 4 Bun tests, 8 golden files | the test reads back what it wrote; `UPDATE_GOLDEN` is the only writer | Consistent; the zone assertion runs first so a wrong zone fails before a wrong golden is written. |
| Task 2 (self) | `SliceUTF16` + BVA-602; two arch fixture rows | `go test ./internal/jsjson` and `go test .` | Consistent; the fixture rows use the same `src` helper and the same `want` substrings as the rows next to them. |
| Task 3 (self) | 9 tests over `BuildPreview`, `Build`, `Context`, `csvFiles`, `unitOf`, `daysBetween` | all six names exist in Step 3 | Consistent; `csvFiles` returns `[]File` sorted by name, which EG-602 indexes as `a.csv`, `b.csv`. |
| Task 4 (self) | `Build` returns `Result`; tests read `r.IssueURL`, `r.MailtoURL` | | Consistent; the old plan returned two strings and its tests used `issue, mailto :=`; both sides changed together. |
| Task 5 (self) | handlers named `summary`, `buildPack`, `buildReport` | `commands` map; package names `pack` and `report` are imported in the same files | Consistent; `pack`/`report` as handler names would have shadowed the packages. |
| Task 5 (self) | AC-606 `p["days"] == 5.0` | `daysBetween("2024-02-17", "2024-02-21")` | Consistent (5 inclusive days; the fixture's own `date_range.days` is 6 and is only passed through as `range`). |
| Task 6 (self) | `test:ui` = `bun test ui/__tests__` | Bun 1.3.11 runs the `node:test` file: 14 pass (scratch copy, 2026-10-16) | Consistent. |
| Task 7 (self) | `os.CopyFS` in the `UPDATE_GOLDEN` block | `go 1.24` in `go.mod` (`CopyFS` since 1.23) | Consistent. |
| Task 9 (self) | `ci_pipeline.py` test stage runs `bun run test` | Task 6 defined that script | Consistent. |
| Task 11 (self) | 21 Go IDs at 601+ | the ID list in Global Constraints | Consistent: 6 AC, 2 EP, 3 BVA, 1 ST-T, 1 DT-R, 8 EG. |
