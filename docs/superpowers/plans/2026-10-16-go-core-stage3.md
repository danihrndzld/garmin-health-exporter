# Go backend stage 3: summary, LLM pack and bug report in Go; Bun and the Electron app leave the build, implementation plan

> **Amendments after stage 2 (2026-10-16), binding over the text below.**
> 1. Test IDs for stage 3 start at **601**, not 301 (stage 2 used 2xx and 3xx). Read every "3xx" ID below as the matching "6xx" ID, for example AC-301 as AC-601.
> 2. There is **no `internal/redact` package**. Redaction lives in the public library `github.com/danihrndzld/gconnect` (v0.1.0), as `gconnect.Redact` and `gconnect.RedactURL`, and `internal/report` imports it. Drop the rows that move `redact.go`. The redaction cases golden goes to `internal/report/testdata/`.
> 3. The Hexagonal rules of `docs/superpowers/specs/2026-10-16-architecture-hexagonal-gconnect.md` apply, enforced by `tauri-app/core/arch_test.go`. The new `pack`, `report` and `summary` commands are handlers in `cmd/`. The use cases take typed requests and a `Reporter`, never `map[string]any`.
> 4. Decisions taken: D1, `electron-app/` is deleted and tagged `archive/electron-app`. D2, rewrite `tools/release.py`, `tools/ci_pipeline.py` and their workflows for Tauri and Go. D3, Bun stays as the task runner. D4, this ships as 4.5.0.

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Move the `summary`, `pack` and `report` commands from the Bun sidecar to the Go binary `garmin-export-core` with byte equal output, then remove the Bun sidecar, `electron-app/` and the Electron build tools from the repository, so that the app ships one 14 MB Go binary next to the Tauri shell and the universal build is `go build`, `lipo` and `tauri build`.

**Architecture:** New Go packages `internal/pack` (port of `tauri-app/sidecar/llm-pack.js`), `internal/report` (port of `sidecar/report.js`) and `internal/redact` (the stage 2 `redactString` port, moved so the Garmin client and `report` share it), plus `internal/garmin/summary.go` (port of `cli.js:summarize`). The outputs of the JavaScript are frozen into golden files before anything is deleted, a Bun test pins the JavaScript to them until the sidecar goes, and Go tests pin Go to them for good. The UI changes one default string. Every deletion is its own commit, so `git revert` is the rollback.

**Tech Stack:** Go 1.27 standard library only (`regexp`, `unicode/utf16`, `encoding/json`, `os`), Bun 1.3 as the task runner and for `node:test`, Playwright for the UI gate, Tauri 2 CLI.

**Spec:** `docs/superpowers/specs/2026-10-16-go-core-stage3-design.md`

**Baseline:** the tree after stage 2 Task 13 (`feat: the Go core is the default Garmin engine; docs for saved logins and source switches`). Stage 2 is being implemented while this plan is written, so this plan cites functions and commands, not line numbers, in every file stage 2 edits (`cmd/garmin-export-core/main.go`, `tauri-app/ui/app.js`, `tauri-app/sidecar/cli.js`, `tauri-app/sidecar/e2e.test.js`, `internal/garmin/*`). Only Task 1 may run before stage 2 is merged.

## Global Constraints

- Stage 1 and 2 rules still hold: module `github.com/danihrndzld/garmin-health-exporter/tauri-app/core`, `go 1.24` in `go.mod`, no third party Go modules, `CGO_ENABLED=0`, universal binary through `lipo`.
- Every path in a request resolves inside `$HOME` through `guard.InsideHome` before anything is read or written. `pack` also requires an existing directory named `csv_*`. `report` never touches the disk.
- Stage 3 copies bytes. No wording, unit, column or URL changes. The golden files in `internal/pack/testdata/golden`, `internal/report/testdata/golden.json` and `internal/redact/testdata/cases.json` are written once by the JavaScript in Task 1 and never by hand. The Go golden test for the Garmin CSV files gains `UPDATE_GOLDEN=1` in Task 7; no task in this plan runs it.
- No live sign in to Garmin or MyFitnessPal. The one live check (Task 11) uses the saved login and makes no sign in. Never run live login experiments in a loop: Garmin answered 429 after 4 sign ins in 40 minutes on 2026-10-14.
- Never `tauri build` in the checkout the user runs the app from; builds happen in a `git worktree` (Task 6, Task 9, Task 11).
- Results never use a payload key named `kind` (`protocol.Emitter.Result` overwrites it).
- `pack.files` is always a JSON array; `pack` is JSON `null` when there is no pack; `summary` is JSON `null` without `jsonPath`.
- Test names carry the technique ID. Go tests for stage 3 use IDs from 301: AC-301.., EP-301.., BVA-301.., ST-301.., DT-301.., EG-301... Playwright: one page object per component in `e2e/pages/`, mock switches only in `e2e/support/mock-backend.js`, wiring only in `e2e/fixtures.js`, assertions only in specs, IDs continue the existing series. Stage 3 adds no UI behaviour, so it adds no spec; the suite is a gate in Tasks 5, 6 and 11.
- `workflows/*.md` change only after the user says yes (Task 9). `electron-app/` is deleted only after the user picks that option (Task 8).
- Commits use the author `Daniel Hernández <danisnowman@gmail.com>` and carry no AI attribution.
- User facing text follows the plain writing rules: complete sentences, no dashes, no jargon.

## Review Focus

1. `CONTEXT.es.md`, `CONTEXT.en.md` and the two one-file packs must be byte equal to the files the JavaScript wrote, including the `→` arrows, `días`, `°C` and the trailing newline. Pinned in Task 1 (JavaScript) and Task 3 (Go, AC-302, AC-303).
2. The issue and mailto URLs must be byte equal to the JavaScript output: the encoder must keep `!*'()~` and write a space as `%20`, and the title must be cut at 120 UTF-16 units. Pinned in Task 4 (AC-304, EP-302, EG-305).
3. An empty `csv_*` folder must give `"files":[]`, never `"files":null`, and a missing folder must give `"pack":null`, never `{}`. Pinned in Task 3 (EG-301) and Task 5 (AC-306).
4. `summary` must accept all three request shapes the UI sends after stage 2: with `jsonPath` and `warnings`, with `jsonPath` only, and with `csvDir` plus dates only. Pinned in Task 5 (AC-306, EG-306).
5. `pack` must refuse a folder outside `$HOME`, a folder that is not a directory, and a folder whose name does not start with `csv_`, before it deletes anything, because `Build` removes `llm-*` subfolders. Pinned in Task 5 (EG-308).
6. The redaction port must give the JavaScript output for every case in `cases.json`, including a JSON pair with a secret key, a Bearer value in mixed case and a JWT in the middle of a sentence. Pinned in Task 2 (AC-301).
7. After Task 6 the repository must build and test with `electron-app/node_modules` absent. Pinned in Task 6 Step 7 by running the full gate after `mv electron-app/node_modules /tmp/nm-aside`.

---

## File structure

```
tauri-app/core/
  internal/jsjson/format.go                     + SliceUTF16
  internal/redact/redact.go                     String, URL, IsSecretKey (moved from internal/garmin/redact.go)
  internal/redact/redact_test.go                AC-301 from testdata/cases.json
  internal/redact/testdata/cases.json           frozen from electron-app/util/redact.js (Task 1)
  internal/pack/pack.go                         describe, units, T, scanCSV, Context, BuildPreview, Build, TZName
  internal/pack/command.go                      Command(req) for the pack command
  internal/pack/pack_test.go                    AC-302, AC-303, DT-301, EG-301..303, EG-307, BVA-301, ST-301, EP-301
  internal/pack/testdata/csv_2024-02-21/*.csv   21 files: the 18 Garmin goldens and the 3 MFP goldens (Task 1)
  internal/pack/testdata/golden/CONTEXT.es.md   frozen from llm-pack.js (Task 1)
  internal/pack/testdata/golden/CONTEXT.en.md
  internal/pack/testdata/golden/garmin_2024-02-17_2024-02-21.es.md
  internal/pack/testdata/golden/garmin_2024-02-17_2024-02-21.en.md
  internal/pack/testdata/golden/preview.json
  internal/report/report.go                     Scrub, Build, EncodeURIComponent, Command
  internal/report/report_test.go                AC-304, EP-302, EG-304, EG-305
  internal/report/testdata/input.json           frozen input (Task 1)
  internal/report/testdata/golden.json          frozen URLs (Task 1)
  internal/garmin/summary.go                    Summarize, SummaryCommand
  internal/garmin/summary_test.go               AC-305, EG-306
  internal/garmin/testdata/fixtures/export-sample.json   moved in Task 7
  internal/garmin/testdata/golden/*.csv                  moved in Task 7
  cmd/garmin-export-core/main.go                + summary, pack, report
  cmd/garmin-export-core/main_test.go           + AC-306, EG-308
tauri-app/sidecar/golden.test.js                Task 1 only; deleted with the folder in Task 6
tauri-app/ui/date-range.js                      tracked from Task 6
tauri-app/ui/__tests__/date-range.test.js       moved in Task 6
tauri-app/scripts/make-fixture.js               moved in Task 7
tauri-app/scripts/compare-csv.js                moved in Task 7
tools/ci_pipeline.py, tools/release.py          rewritten in Task 9
workflows/cicd_pipeline.md, workflows/release.md rewritten in Task 9
```

Modified: `tauri-app/ui/app.js` (default binary), `tauri-app/package.json`, `tauri-app/src-tauri/tauri.conf.json`, `tauri-app/src-tauri/capabilities/default.json`, `tauri-app/playwright.config.js`, `tauri-app/.gitignore`, `internal/garmin/client.go` (import `redact`), `internal/garmin/csv_test.go` (paths, `UPDATE_GOLDEN`), `README.md`, `CLAUDE.md`, three header comments.

Deleted: `tauri-app/sidecar/` (Task 6), `tauri-app/core/cmd/fake-mfp/` (Task 6), `electron-app/` (Task 8).

## Shippable increments

| After task | What the user gets | In the bundle |
| --- | --- | --- |
| 1 | Nothing visible; the JavaScript outputs are frozen and pinned | Bun + Go |
| 5 | Every command runs in Go; Bun ships unused; rollback is one string per command | Bun + Go |
| 6 | DMG without Bun (about 15 to 20 MB, measured in Step 6) | Go only |
| 8 | Repository without `electron-app/`; tests need no `npm install` | Go only |
| 11 | Release 4.5.0 (assumption A9) through the new tool | Go only |

---

### Task 0: Branch and the rollback point

**Files:** none.

- [ ] **Step 1: Confirm the baseline.** Run: `git log --oneline -1 && git status --short` on `main` after the stage 2 merge. Expected: the stage 2 Task 13 commit, a clean tree. Record its SHA in the first commit message of Task 2 ("baseline <sha>") so a reviewer can diff from it.

- [ ] **Step 2: Create the branch.** Run: `git worktree add .worktrees/go-core-stage3 -b feat/go-core-stage3 main && cd .worktrees/go-core-stage3 && (cd electron-app && npm install) && (cd tauri-app && bun install)`. A fresh worktree has no `node_modules`; until Task 6 the Bun sidecar tests still import `sql.js` from `electron-app/node_modules`. Work only inside the worktree.

- [ ] **Step 3: Green at the start.** Run: `cd tauri-app && bun run test:core && bun test sidecar && bunx playwright test && (cd ../electron-app && npm test)`. Expected: PASS. If anything fails here, stop; stage 3 must not start on a red tree.

---

### Task 1: Freeze the JavaScript outputs into golden files

This task runs the JavaScript once, writes what it produced, and pins the JavaScript to those files. It may run before stage 2 is merged: it adds files and one Bun test only.

**Files:**
- Create: `tauri-app/sidecar/golden.test.js`, `tauri-app/core/internal/pack/testdata/csv_2024-02-21/` (21 copies), `tauri-app/core/internal/pack/testdata/golden/` (5 files), `tauri-app/core/internal/report/testdata/input.json`, `tauri-app/core/internal/report/testdata/golden.json`, `tauri-app/core/internal/redact/testdata/cases.json`

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
const REDACT = path.join(CORE, 'redact/testdata');
const UPDATE = !!process.env.UPDATE_GOLDEN;

function pin(file, text) {
  if (UPDATE) { fs.mkdirSync(path.dirname(file), { recursive: true }); fs.writeFileSync(file, text); }
  expect(fs.readFileSync(file, 'utf8')).toBe(text);
}
const json = (v) => JSON.stringify(v, null, 2) + '\n';

// The user's Mac is in America/Guatemala (/etc/localtime, checked 2026-10-15), so the file also passes
// inside a plain `bun test sidecar`. On another zone run it with TZ=America/Guatemala; the gates in
// Tasks 5 and 6 say so.
test('the zone is fixed, so the Exported line is reproducible', () => {
  expect(Intl.DateTimeFormat().resolvedOptions().timeZone).toBe('America/Guatemala');
});

const packOpts = (csvDir) => ({
  csvDir, startDate: '2024-02-17', endDate: '2024-02-21',
  warnings: ['hill_score skipped (404)', '[skip] endurance_score: HTTP 204'],
  now: new Date(2026, 9, 15, 20, 53), // local 2026-10-15 20:53
});

test('AC-13 pack preview and both one-file packs equal the golden files', () => {
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

test('AC-14 the bug report URLs equal the golden file', () => {
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

test('AC-15 redaction cases equal the golden file', () => {
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
  pin(path.join(REDACT, 'cases.json'), json(cases));
});
```

- [ ] **Step 3: Generate the goldens.** Run: `cd tauri-app && TZ=America/Guatemala UPDATE_GOLDEN=1 bun test sidecar/golden.test.js` Expected: 4 pass; the 5 pack files, 2 report files and `cases.json` exist. Then run it again without `UPDATE_GOLDEN`: `TZ=America/Guatemala bun test sidecar/golden.test.js` Expected: PASS (the files reproduce).

- [ ] **Step 4: Read the goldens once.** Open `CONTEXT.en.md` and check by eye: the range line reads `Range: 2024-02-17 → 2024-02-21 (5 days)`, the exported line reads `Exported: 2026-10-15 20:53 (America/Guatemala)`, `## Files (21)`, the MyFitnessPal line says `food logged on 2 of 4 days`, and the two warnings are listed under `## Missing data and warnings`. `golden.json` holds two URLs, the issue URL at most 7,500 characters, the mailto URL at most 1,800, neither containing `example.com`, `hunter2` or `/Users/dani`.

- [ ] **Step 5: Commit.**

```bash
git add tauri-app/sidecar/golden.test.js tauri-app/core/internal/pack/testdata tauri-app/core/internal/report/testdata tauri-app/core/internal/redact/testdata
git commit -m "test(sidecar): freeze the pack, report and redaction outputs as golden files for the Go port"
```

---

### Task 2: `internal/redact`, shared by the Garmin client and the report

**Files:**
- Move: `internal/garmin/redact.go` → `internal/redact/redact.go`; its test (stage 2 Task 10 named it `EG-10` inside `client_test.go` or `redact_test.go`; confirm the actual placement) → `internal/redact/redact_test.go`
- Create: `internal/redact/redact_test.go` (AC-301)
- Modify: `internal/garmin/client.go` (import), `internal/jsjson/format.go` (`SliceUTF16`), `internal/jsjson/jsjson_test.go`

**Interfaces:**
- Produces: `redact.String(s string) string` (port of `redactString`), `redact.URL(s string) string` (port of `redactUrl`), `redact.IsSecretKey(k string) bool`; `jsjson.SliceUTF16(s string, n int) string`.

- [ ] **Step 1: Move the file.** `git mv tauri-app/core/internal/garmin/redact.go tauri-app/core/internal/redact/redact.go`, change `package garmin` to `package redact`, rename the functions to `String`, `URL`, `IsSecretKey`, and update the call sites in `internal/garmin/client.go` (`redact.String(preview)`). If stage 2 did not port `redactUrl`, add it now from `electron-app/util/redact.js:redactUrl` (6 lines).

- [ ] **Step 2: Write the failing tests.** `internal/redact/redact_test.go`:

```go
package redact

import (
	"encoding/json"
	"os"
	"testing"
)

// cases.json was written by electron-app/util/redact.js (stage 3 Task 1); Go must give the same output.
func TestAC301RedactionMatchesTheJavaScript(t *testing.T) {
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
		got := URL(c.In)
		if c.Fn == "string" {
			got = String(c.In)
		}
		if got != c.Out {
			t.Errorf("%s(%q)\n got %q\nwant %q", c.Fn, c.In, got, c.Out)
		}
	}
}

func TestIsSecretKeyVariants(t *testing.T) {
	for k, want := range map[string]bool{"token": true, "Access_Token": true, "clientSecret": true, "X-Auth-Token": true, "jwt": true, "username": false, "date": false} {
		if IsSecretKey(k) != want {
			t.Errorf("IsSecretKey(%q) = %v", k, !want)
		}
	}
}
```

`internal/jsjson/jsjson_test.go`, add:

```go
func TestSliceUTF16CountsCodeUnits(t *testing.T) {
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
}
```

- [ ] **Step 3: Run them.** Run: `cd tauri-app/core && go test ./internal/redact ./internal/jsjson` Expected: FAIL (`undefined: URL` or `SliceUTF16`, or a case mismatch).

- [ ] **Step 4: Implement.** The regexes, one per JavaScript line, with `(?i)` for `/i`:

```go
var (
	authHeader = regexp.MustCompile(`(?i)(Authorization:\s*)(Bearer|Basic)\s+\S+`)
	bearer     = regexp.MustCompile(`(?i)\b(Bearer|Basic)\s+[A-Za-z0-9._~+/=-]+`)
	cookie     = regexp.MustCompile(`(?i)((?:set-)?cookie:\s*)[^\r\n]+`)
	jwt        = regexp.MustCompile(`eyJ[A-Za-z0-9_-]+\.[A-Za-z0-9_-]+\.[A-Za-z0-9_-]+`)
	jsonPair   = regexp.MustCompile(`"([^"]+)"\s*:\s*"([^"]*)"`)
	queryPair  = regexp.MustCompile(`([?&])([^=&\s"']+)=([^&\s"']*)`)
	urlPair    = regexp.MustCompile(`([?&])([^=&]+)=([^&]*)`)
)
// ponytail: JavaScript \s also matches U+00A0, U+2028 and U+FEFF and RE2 does not; and RE2's (?i)
// folds s with U+017F and k with U+212A. Both make Go redact more, never less.
```

`String` applies them in the JavaScript order (`authHeader`, `bearer`, `cookie`, `jwt`, `jsonPair` with `IsSecretKey` on group 1, `queryPair` with `IsSecretKey` on group 2). For the two callback replacements use `ReplaceAllStringFunc` and `FindStringSubmatch` on the match. `SliceUTF16` is `utf16.Encode([]rune(s))`, cut, `utf16.Decode`.

- [ ] **Step 5: Run everything.** Run: `cd tauri-app && bun run test:core` Expected: PASS, including the stage 2 client test that reads redacted previews.

- [ ] **Step 6: Commit.**

```bash
git add tauri-app/core
git commit -m "refactor(core): redact is its own package, pinned to the JavaScript cases; jsjson.SliceUTF16"
```

---

### Task 3: `internal/pack`, byte equal to the frozen pack

**Files:**
- Create: `internal/pack/pack.go`, `internal/pack/pack_test.go`

**Interfaces:**
- Produces:

```go
type Options struct {
	CSVDir, StartDate, EndDate string
	Warnings                   []string
	Now                        time.Time // the Exported line; time.Now() in the command
	TZ                         string    // the zone name in the Exported line; TZName() in the command
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
var Langs = []string{"es", "en"}
func BuildPreview(o Options) (*Preview, error)                          // nil, nil when previewPack returned null
func Build(o Options, lang, mode string) (dir, file string, count int, err error)
func Context(lang string, o Options, files []File) string
func TZName() string
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
	if err := os.Mkdir(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	entries, _ := os.ReadDir(fixtureDir)
	for _, e := range entries {
		b, _ := os.ReadFile(filepath.Join(fixtureDir, e.Name()))
		os.WriteFile(filepath.Join(dir, e.Name()), b, 0o644)
	}
	return dir
}

func TestAC302PreviewEqualsTheJavaScriptGoldens(t *testing.T) {
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
}

func TestAC303BuildWritesTheGoldenPacks(t *testing.T) {
	for _, lang := range Langs {
		dir := copyFixture(t)
		os.WriteFile(filepath.Join(dir, "CONTEXT.es.md"), []byte("legacy"), 0o644) // v4.0.0 root files
		os.WriteFile(filepath.Join(dir, "garmin_2026-01-01_2026-01-02.en.md"), []byte("legacy"), 0o644)
		sub, file, count, err := Build(opts(dir), lang, "single")
		if err != nil || count != 1 || sub != filepath.Join(dir, "llm-"+lang+"-one-file") {
			t.Fatalf("single: %s %s %d %v", sub, file, count, err)
		}
		got, _ := os.ReadFile(file)
		if want := golden(t, filepath.Base(file)); string(got) != want {
			t.Errorf("%s differs at byte %d", filepath.Base(file), firstDiff(string(got), want))
		}
		sub, file, count, err = Build(opts(dir), lang, "files")
		if err != nil || count != 22 || file != "" || sub != filepath.Join(dir, "llm-"+lang) {
			t.Fatalf("files: %s %s %d %v", sub, file, count, err)
		}
		ctx, _ := os.ReadFile(filepath.Join(sub, "CONTEXT."+lang+".md"))
		if string(ctx) != golden(t, "CONTEXT."+lang+".md") {
			t.Error("CONTEXT in the files folder differs from the preview context")
		}
		if b, _ := os.ReadFile(filepath.Join(sub, "sleep.csv")); !bytes.Equal(b, mustRead(t, filepath.Join(dir, "sleep.csv"))) {
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
}

func TestST301ASecondPressReplacesTheFolder(t *testing.T) {
	dir := copyFixture(t)
	sub, _, _, _ := Build(opts(dir), "en", "files")
	os.WriteFile(filepath.Join(sub, "stale.txt"), []byte("x"), 0o644)
	if _, _, _, err := Build(opts(dir), "en", "files"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(sub, "stale.txt")); !os.IsNotExist(err) {
		t.Error("stale file survived the second press")
	}
}

func TestEP301LangAndModePartitions(t *testing.T) {
	dir := copyFixture(t)
	if _, _, _, err := Build(opts(dir), "fr", "files"); err == nil || err.Error() != "lang must be es or en." {
		t.Errorf("fr: %v", err)
	}
	if _, _, _, err := Build(opts(dir), "es", "zip"); err == nil || err.Error() != "mode must be files or single." {
		t.Errorf("zip: %v", err)
	}
}

func TestEG301MissingFolderIsNilAndEmptyFolderHasAnEmptyList(t *testing.T) {
	o := opts(filepath.Join(t.TempDir(), "none"))
	if p, err := BuildPreview(o); p != nil || err != nil {
		t.Fatalf("missing folder: %v %v", p, err)
	}
	for _, o := range []Options{{CSVDir: fixtureDir, EndDate: "2024-02-21"}, {CSVDir: fixtureDir, StartDate: "2024-02-17"}} {
		if p, _ := BuildPreview(o); p != nil {
			t.Errorf("preview without both dates must be nil")
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
}

func TestEG302LineEndingsQuotedHeaderAndEmptyFile(t *testing.T) {
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
}

func TestEG303LoggedColumnByHeaderNotPosition(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "csv_2026-05-12")
	os.Mkdir(dir, 0o755)
	os.WriteFile(filepath.Join(dir, "mfp_daily.csv"), []byte("date,meals_logged,logged\n2026-05-11,2,yes\n2026-05-12,0,no\n"), 0o644)
	files, _ := csvFiles(dir)
	ctx := Context("en", Options{StartDate: "2026-05-11", EndDate: "2026-05-12"}, files)
	if !strings.Contains(ctx, "MyFitnessPal: food logged on 1 of 2 days.") {
		t.Errorf("context:\n%s", ctx)
	}
}

func TestDT301UnitTable(t *testing.T) {
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
}

func TestBVA301DaysBetween(t *testing.T) {
	if d := daysBetween("2026-10-15", "2026-10-15"); d != 1 {
		t.Errorf("same day = %d", d)
	}
	if d := daysBetween("2026-07-17", "2026-10-14"); d != 90 {
		t.Errorf("90 days = %d", d)
	}
}

func TestEG307TZNameFromTheEnvironment(t *testing.T) {
	t.Setenv("TZ", "America/Guatemala")
	if TZName() != "America/Guatemala" {
		t.Errorf("got %q", TZName())
	}
	t.Setenv("TZ", "")
	if TZName() == "" || TZName() == "Local" {
		t.Errorf("got %q", TZName())
	}
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

Check the exact wording of the EG-302 expectations against `CONTEXT.en.md` from Task 1 (the `- \`name\`: desc. N rows, first → last.` line) before running; only the file names differ.

- [ ] **Step 2: Run them.** Run: `cd tauri-app/core && go test ./internal/pack` Expected: FAIL (`undefined: BuildPreview`).

- [ ] **Step 3: Implement `pack.go`.** Port `llm-pack.js` top to bottom, one Go function per JavaScript function, with the JavaScript name in a comment. The parts that decide byte equality:

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

// trimEnd ports String.prototype.trimEnd. Go also trims U+0085, which no CSV here holds.
func trimEnd(s string) string {
	return strings.TrimRightFunc(s, func(r rune) bool { return unicode.IsSpace(r) || r == '\uFEFF' })
}

// daysBetween ports daysBetween: inclusive count of local calendar days.
func daysBetween(a, b string) int {
	s, _ := time.ParseInLocation("2006-01-02", a, time.Local)
	e, _ := time.ParseInLocation("2006-01-02", b, time.Local)
	return int(math.Round(e.Sub(s).Hours()/24)) + 1
}

// TZName is what Intl.DateTimeFormat().resolvedOptions().timeZone gave the JavaScript. Go names the
// local zone "Local", so read $TZ, else where /etc/localtime points (macOS: .../zoneinfo/<name>).
func TZName() string {
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
```

`Context` builds the same `out` slice as `buildContext`, line by line, and returns `strings.Join(out, "\n") + "\n"`. The MyFitnessPal line uses the first file named `mfp_daily.csv`: rows are `strings.Split(text, "\n")[1:]` with `\r` trimmed and empties dropped (the JavaScript drops the first raw line before filtering), `col` is the index of `logged` in the header, and a row counts when `strings.Split(row, ",")` has more than `col` items and item `col` is `yes`; `col < 0` counts nothing.

`single(ctx, files)` joins `ctx` and one "## `name`\n\n```csv\n" + trimEnd(text) + "\n```\n" block per file with `\n`.

`BuildPreview` returns `nil, nil` when `CSVDir`, `StartDate` or `EndDate` is empty or the folder does not exist; otherwise reads the files once, builds both languages, `Bytes = len(single)`, `Tokens = int(math.Round(float64(len(single)) / 3))`, `Files = make([]File, 0, n)`, `Warnings` non nil.

`Build` checks `lang` and `mode` with the two `cli.js` sentences, removes root files matching `legacyRe` (an `os.Remove` error is returned, as `rmSync` throws), `os.RemoveAll` and `os.Mkdir(dir, 0o755)`, writes with `os.WriteFile(..., 0o644)`, copies CSV files byte for byte, and returns `count = len(files)+1` or `1`.

- [ ] **Step 4: Run them.** Run: `cd tauri-app/core && go test ./internal/pack` Expected: PASS. If AC-302 fails, print the bytes around `firstDiff` from both strings and fix the port, never the golden file.

- [ ] **Step 5: Commit.**

```bash
git add tauri-app/core/internal/pack
git commit -m "feat(core): LLM pack in Go, byte equal to the frozen JavaScript output"
```

---

### Task 4: `internal/report`, byte equal URLs

**Files:**
- Create: `internal/report/report.go`, `internal/report/report_test.go`

**Interfaces:**
- Consumes: `redact.String`, `redact.IsSecretKey` (Task 2), `jsjson.SliceUTF16`.
- Produces: `report.Line{TS, Type, Msg string}`, `report.Build(errText string, log []Line, version, os string) (issueURL, mailtoURL string)`, `report.Scrub(s string) string`, `report.EncodeURIComponent(s string) string`, constants `Repo`, `Email`.

- [ ] **Step 1: Write the failing tests.** `internal/report/report_test.go`:

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

func TestAC304ReportURLsEqualTheJavaScript(t *testing.T) {
	in, want := load(t)
	issue, mailto := Build(in.Error, in.Log, in.Version, in.OS)
	if issue != want["issueUrl"] {
		t.Errorf("issueUrl differs at %d:\n got %s\nwant %s", firstDiff(issue, want["issueUrl"]), issue, want["issueUrl"])
	}
	if mailto != want["mailtoUrl"] {
		t.Errorf("mailtoUrl differs at %d:\n got %s\nwant %s", firstDiff(mailto, want["mailtoUrl"]), mailto, want["mailtoUrl"])
	}
}

func TestEG304NewestLinesKeptWithinTheLimits(t *testing.T) {
	in, _ := load(t)
	issue, mailto := Build(in.Error, in.Log, in.Version, in.OS)
	if len(issue) > 7500 || len(mailto) > 1800 {
		t.Fatalf("lengths %d %d", len(issue), len(mailto))
	}
	body, _ := url.QueryUnescape(strings.SplitN(issue, "&body=", 2)[1])
	if !strings.Contains(body, "line 299") || strings.Contains(body, "line 0 ") {
		t.Error("the newest line must stay and the oldest must go")
	}
	for _, secret := range []string{"example.com", "hunter2", "/Users/dani", "eyJhbGci", "a@b.com"} {
		if strings.Contains(body, secret) || strings.Contains(mailto, secret) {
			t.Errorf("%q leaked", secret)
		}
	}
	if !strings.HasPrefix(issue, "https://github.com/"+Repo+"/issues/new?title=") || !strings.HasPrefix(mailto, "mailto:"+Email+"?subject=") {
		t.Error("prefixes")
	}
}

func TestEP302EncodeURIComponentMatchesJavaScript(t *testing.T) {
	// node -e 'console.log(encodeURIComponent("a b!*'"'"'()~-_.é\n`$&+,/:;=?@#"))' on 2026-10-15
	if got := EncodeURIComponent("a b!*'()~-_.é\n`$&+,/:;=?@#"); got != "a%20b!*'()~-_.%C3%A9%0A%60%24%26%2B%2C%2F%3A%3B%3D%3F%40%23" {
		t.Errorf("got %s", got)
	}
	if got := EncodeURIComponent("·"); got != "%C2%B7" {
		t.Errorf("got %s", got)
	}
}

func TestEG305TitleIsCutAt120UTF16Units(t *testing.T) {
	long := strings.Repeat("é", 200)
	issue, _ := Build(long, nil, "1", "x")
	title, _ := url.QueryUnescape(strings.TrimPrefix(strings.SplitN(issue, "&body=", 2)[0], "https://github.com/"+Repo+"/issues/new?title="))
	if want := "Export failed: " + strings.Repeat("é", 105); title != want {
		t.Errorf("title has %d runes", len([]rune(title)))
	}
	if issue2, _ := Build("", nil, "1", "x"); !strings.Contains(issue2, "Unknown%20error") {
		t.Error("an empty error reads Unknown error")
	}
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

- [ ] **Step 2: Run them.** Run: `cd tauri-app/core && go test ./internal/report` Expected: FAIL.

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
	"github.com/danihrndzld/garmin-health-exporter/tauri-app/core/internal/redact"
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

// Scrub ports scrub: redaction, key=value secrets, emails, home folders.
func Scrub(s string) string {
	s = redact.String(s)
	s = kvRe.ReplaceAllStringFunc(s, func(m string) string {
		k := m[:strings.IndexByte(m, '=')]
		if redact.IsSecretKey(k) {
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

func Build(errText string, log []Line, version, osName string) (issueURL, mailtoURL string) {
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
	issueURL = fit("https://github.com/"+Repo+"/issues/new?title="+EncodeURIComponent(title)+"&body=", head, lines, issueURLMax)
	mailHead := "Error: " + e + "\nApp: " + app + "\n\nLog:\n```\n"
	mailtoURL = fit("mailto:"+Email+"?subject="+EncodeURIComponent("Garmin Export: "+title)+"&body=", mailHead, lines, mailtoURLMax)
	return issueURL, mailtoURL
}
```

`Command(req map[string]any) map[string]any` reads `error`, `version`, `os` as strings (absent means `?` for version and os, as the JavaScript defaults), `log` as a list of objects with `ts`, `type`, `msg` (non strings become `fmt.Sprint`), and returns `{ok:true, issueUrl, mailtoUrl}`.

- [ ] **Step 4: Run them.** Run: `cd tauri-app/core && go test ./internal/report` Expected: PASS.

- [ ] **Step 5: Commit.**

```bash
git add tauri-app/core/internal/report
git commit -m "feat(core): bug report links in Go with JavaScript URL encoding; byte equal to the frozen output"
```

---

### Task 5: `summary`, `pack` and `report` commands; the UI runs everything in Go

This is the first shippable increment: Bun still ships, nothing calls it.

**Files:**
- Create: `internal/garmin/summary.go`, `internal/garmin/summary_test.go`, `internal/pack/command.go`
- Modify: `cmd/garmin-export-core/main.go`, `cmd/garmin-export-core/main_test.go`, `tauri-app/ui/app.js`

**Interfaces:**
- Produces: `garmin.Summarize(raw []byte) (*Summary, bool)` with `Summary{Range json.RawMessage; Days []map[string]any; Start, End string}`; `garmin.SummaryCommand(req) map[string]any`; `pack.Command(req) map[string]any`; `report.Command(req) map[string]any`.

- [ ] **Step 1: Write the failing Go tests.** `internal/garmin/summary_test.go`:

```go
func TestAC305SummarizeTheFixture(t *testing.T) {
	raw, err := os.ReadFile(fixturePath)
	if err != nil {
		t.Fatal(err)
	}
	s, ok := Summarize(raw)
	if !ok || len(s.Days) != 5 || s.Start != "2024-02-17" || s.End != "2024-02-21" {
		t.Fatalf("summary: %+v %v", s, ok)
	}
	for i := 1; i < len(s.Days); i++ {
		if s.Days[i]["date"].(string) < s.Days[i-1]["date"].(string) {
			t.Fatal("days are not ascending")
		}
	}
	d := s.Days[0]
	for _, k := range []string{"date", "steps", "rhr", "sleepScore", "sleepSec", "hrv"} {
		if _, ok := d[k]; !ok {
			t.Errorf("day has no %s", k)
		}
	}
	if d["steps"] != 1403.0 || d["sleepSec"] != 42146.0 { // 2024-02-17 in the fixture (jq checked 2026-10-15); 02-20 and 02-21 are null
		t.Errorf("fixture values for the first day: %v", d)
	}
	// A day without a sleep section gives null, not a crash.
	s2, _ := Summarize([]byte(`{"date_range":{"start":"2026-01-01","end":"2026-01-01"},"daily":{"2026-01-01":{"stats":{"totalSteps":7}}}}`))
	if s2.Days[0]["sleepScore"] != nil || s2.Days[0]["steps"].(float64) != 7 {
		t.Errorf("got %v", s2.Days[0])
	}
}

func TestEG306UnreadableExportAndCsvDirOnly(t *testing.T) {
	if _, ok := Summarize([]byte("{not json")); ok {
		t.Error("corrupt JSON accepted")
	}
	home, _ := os.UserHomeDir()
	dir, _ := os.MkdirTemp(home, ".garmin-summary-test-")
	defer os.RemoveAll(dir)
	csvDir := filepath.Join(dir, "csv_2026-10-07")
	os.Mkdir(csvDir, 0o755)
	os.WriteFile(filepath.Join(csvDir, "mfp_daily.csv"), []byte("date,logged\n2026-10-07,yes\n"), 0o644)
	res := SummaryCommand(map[string]any{"csvDir": csvDir, "startDate": "2026-10-01", "endDate": "2026-10-07"})
	if res["ok"] != true || res["summary"] != nil || res["pack"] == nil {
		t.Fatalf("csvDir only: %v", res)
	}
	bad := filepath.Join(dir, "x.txt")
	os.WriteFile(bad, []byte("{}"), 0o644)
	if res := SummaryCommand(map[string]any{"jsonPath": bad}); res["error"] != "jsonPath must be a .json file within your home directory." {
		t.Errorf("got %v", res)
	}
	corrupt := filepath.Join(dir, "x.json")
	os.WriteFile(corrupt, []byte("{"), 0o644)
	if res := SummaryCommand(map[string]any{"jsonPath": corrupt}); res["error"] != "Could not read export." {
		t.Errorf("got %v", res)
	}
}
```

`cmd/garmin-export-core/main_test.go`, add:

```go
// AC-306: the three stage 3 commands through the binary, as the Tauri shell runs them.
func TestAC306SummaryPackAndReportThroughTheBinary(t *testing.T) {
	home, _ := os.UserHomeDir()
	root, _ := os.MkdirTemp(home, ".garmin-stage3-test-")
	defer os.RemoveAll(root)
	csvDir := filepath.Join(root, "csv_2024-02-21")
	os.Mkdir(csvDir, 0o755)
	src := "../../internal/pack/testdata/csv_2024-02-21"
	entries, _ := os.ReadDir(src)
	for _, e := range entries {
		b, _ := os.ReadFile(filepath.Join(src, e.Name()))
		os.WriteFile(filepath.Join(csvDir, e.Name()), b, 0o644)
	}
	jsonPath := filepath.Join(root, "garmin_health.json")
	fx, _ := os.ReadFile("../../internal/garmin/testdata/fixtures/export-sample.json") // Task 7 moves it here; before that use the electron-app path
	os.WriteFile(jsonPath, fx, 0o644)

	req, _ := json.Marshal(map[string]any{"cmd": "summary", "jsonPath": jsonPath, "csvDir": csvDir, "warnings": []string{"w1"}})
	res := result(t, runBinary(t, "", string(req)+"\n"))
	s := res["summary"].(map[string]any)
	p := res["pack"].(map[string]any)
	if res["ok"] != true || len(s["days"].([]any)) != 5 || len(p["files"].([]any)) != 21 || p["warnings"].([]any)[0] != "w1" {
		t.Fatalf("summary: %v", res)
	}
	if p["langs"].(map[string]any)["es"].(map[string]any)["tokens"].(float64) <= 0 {
		t.Fatal("tokens")
	}

	for _, mode := range []string{"files", "single"} {
		req, _ = json.Marshal(map[string]any{"cmd": "pack", "mode": mode, "lang": "en", "csvDir": csvDir, "startDate": "2024-02-17", "endDate": "2024-02-21"})
		res = result(t, runBinary(t, "", string(req)+"\n"))
		if res["ok"] != true {
			t.Fatalf("pack %s: %v", mode, res)
		}
		if mode == "files" && (res["count"].(float64) != 22 || res["file"] != nil) {
			t.Fatalf("files: %v", res)
		}
		if mode == "single" && (res["count"].(float64) != 1 || !strings.HasSuffix(res["file"].(string), "garmin_2024-02-17_2024-02-21.en.md")) {
			t.Fatalf("single: %v", res)
		}
	}

	req, _ = json.Marshal(map[string]any{"cmd": "report", "error": "x for a@b.com", "log": []map[string]string{{"ts": "1", "type": "dim", "msg": "Bearer abc.def.ghi"}}, "version": "4.5.0", "os": "mac"})
	res = result(t, runBinary(t, "", string(req)+"\n"))
	for _, k := range []string{"issueUrl", "mailtoUrl"} {
		u, _ := res[k].(string)
		if u == "" || strings.Contains(u, "a%40b.com") || strings.Contains(u, "abc.def.ghi") {
			t.Errorf("%s: %q", k, u)
		}
	}

	// An empty csv_* folder still gives an array for files.
	empty := filepath.Join(root, "csv_2026-01-01")
	os.Mkdir(empty, 0o755)
	req, _ = json.Marshal(map[string]any{"cmd": "summary", "csvDir": empty, "startDate": "2026-01-01", "endDate": "2026-01-01"})
	lines := runBinary(t, "", string(req)+"\n")
	last, _ := json.Marshal(lines[len(lines)-1])
	if !strings.Contains(string(last), `"files":[]`) || !strings.Contains(string(last), `"summary":null`) {
		t.Errorf("empty folder: %s", last)
	}
}

// EG-308: pack refuses to touch anything but an export folder inside $HOME.
func TestEG308PackRefusesUnsafeFolders(t *testing.T) {
	home, _ := os.UserHomeDir()
	root, _ := os.MkdirTemp(home, ".garmin-stage3-test-")
	defer os.RemoveAll(root)
	notCsv := filepath.Join(root, "Documents")
	os.Mkdir(notCsv, 0o755)
	os.WriteFile(filepath.Join(notCsv, "keep.md"), []byte("x"), 0o644)
	for _, dir := range []string{"/tmp", notCsv, filepath.Join(root, "csv_missing")} {
		req, _ := json.Marshal(map[string]any{"cmd": "pack", "mode": "files", "lang": "en", "csvDir": dir, "startDate": "2026-01-01", "endDate": "2026-01-02"})
		res := result(t, runBinary(t, "", string(req)+"\n"))
		if res["ok"] != false || res["error"] != "csvDir must be an export csv_ folder within your home directory." {
			t.Errorf("%s: %v", dir, res)
		}
	}
	if _, err := os.Stat(filepath.Join(notCsv, "keep.md")); err != nil {
		t.Error("pack touched a folder it refused")
	}
	req, _ := json.Marshal(map[string]any{"cmd": "pack", "mode": "files", "lang": "en", "csvDir": filepath.Join(root, "csv_x"), "startDate": "2026-1-1", "endDate": "2026-01-02"})
	os.Mkdir(filepath.Join(root, "csv_x"), 0o755)
	if res := result(t, runBinary(t, "", string(req)+"\n")); res["error"] != "startDate and endDate must be YYYY-MM-DD." {
		t.Errorf("dates: %v", res)
	}
}
```

Until Task 7 moves the fixture, the `fx` line reads `../../../../electron-app/garmin/__tests__/fixtures/export-sample.json`, the same constant stage 2 uses; Task 7 changes both.

- [ ] **Step 2: Run them.** Run: `cd tauri-app/core && go test ./internal/garmin -run 'AC305|EG306' && go test ./cmd/...` Expected: FAIL.

- [ ] **Step 3: Implement.**

`internal/garmin/summary.go`:

```go
// Summarize ports cli.js:summarize: the per day series for the preview cards. The UI reads
// range and days only, so types and activities from the JavaScript are not ported.
type Summary struct {
	Range      json.RawMessage  `json:"range"`
	Days       []map[string]any `json:"days"`
	Start, End string           `json:"-"`
}

func Summarize(raw []byte) (*Summary, bool) {
	var data struct {
		DateRange json.RawMessage           `json:"date_range"`
		Daily     map[string]map[string]any `json:"daily"`
	}
	if json.Unmarshal(raw, &data) != nil {
		return nil, false
	}
	dates := make([]string, 0, len(data.Daily))
	for d := range data.Daily {
		dates = append(dates, d)
	}
	sort.Strings(dates)
	s := &Summary{Range: data.DateRange, Days: make([]map[string]any, 0, len(dates))}
	for _, d := range dates {
		x := data.Daily[d]
		s.Days = append(s.Days, map[string]any{"date": d,
			"steps": dig(x, "stats", "totalSteps"), "rhr": dig(x, "heart_rates", "restingHeartRate"),
			"sleepScore": dig(x, "sleep", "dailySleepDTO", "sleepScores", "overall", "value"),
			"sleepSec": dig(x, "sleep", "dailySleepDTO", "sleepTimeSeconds"), "hrv": dig(x, "hrv", "hrvSummary", "lastNightAvg")})
	}
	var r struct{ Start, End string }
	_ = json.Unmarshal(data.DateRange, &r)
	s.Start, s.End = r.Start, r.End
	if len(s.Range) == 0 {
		s.Range = json.RawMessage("null")
	}
	return s, true
}

// dig is the optional chain d.a?.b?.c ?? null: nil as soon as a step is not an object.
func dig(m map[string]any, keys ...string) any {
	var v any = m
	for _, k := range keys {
		mm, ok := v.(map[string]any)
		if !ok {
			return nil
		}
		v = mm[k]
	}
	return v
}
```

`SummaryCommand(req)`: with `jsonPath` present, `guard.InsideHome` and the `.json` suffix, else the `cli.js` sentence; `os.ReadFile` and `Summarize`, else `"Could not read export."`; dates from `s.Start`, `s.End`. Without `jsonPath`: `csvDir` through `guard.InsideHome` (sentence `"csvDir must be within your home directory."`) and both dates matching `^\d{4}-\d{2}-\d{2}$` (`"startDate and endDate must be YYYY-MM-DD."`), `summary` nil. Then `pack.BuildPreview(pack.Options{CSVDir, StartDate, EndDate, Warnings: pack.CleanWarnings(req["warnings"]), Now: time.Now(), TZ: pack.TZName()})` when `csvDir` is present; the result map holds `"summary": s` (a nil `*Summary` marshals as `null`) and `"pack": p` (a nil `*Preview` marshals as `null`).

`pack.CleanWarnings(v any) []string` ports `cleanWarnings`: at most 200 items, each `jsjson.SliceUTF16(s, 500)`; a non list gives an empty, non nil slice.

`pack.Command(req)`: `guard.InsideHome(csvDir)`, then `os.Stat` is a directory and `strings.HasPrefix(filepath.Base(dir), "csv_")`, else `"csvDir must be an export csv_ folder within your home directory."`; the two dates; `Build`; `{ok:true, dir, count}` plus `file` only in single mode.

`main.go`:

```go
case "summary":
	out.Result(garmin.SummaryCommand(req))
case "pack":
	out.Result(pack.Command(req))
case "report":
	out.Result(report.Command(req))
```

- [ ] **Step 4: UI.** In `tauri-app/ui/app.js`: `run(req, onEvent, bin = 'binaries/garmin-export-core')`; delete the explicit `'binaries/garmin-export-core'` argument in `refreshGarmin`, in the `garmin-connect`, `garmin-disconnect` and `clear-cache` calls, in `startExport` and in `runMfp` (stage 2 added them); delete `DEFAULT_ENGINE`, `engine()` and the `ge_engine` read, so `startExport` runs `export` on the default binary; change the comment above `platform` from "the bundled `garmin-core` sidecar" to "the bundled Go core `garmin-export-core`". `dev-mock.js` needs no change.

- [ ] **Step 5: Run everything.** Run: `cd tauri-app && bun run test:core && TZ=America/Guatemala bun test sidecar && bunx playwright test` Expected: PASS. The Playwright run is the gate for Step 4 (the mock ignores the binary name, so a typo there would show up only in the app; see Step 6).

- [ ] **Step 6: Try it in the app (ask the user first).** `bun run dev`. With the saved Garmin login (no sign in), export 7 days, open the LLM pack sheet with `L`, press both build buttons in both languages, and check that `llm-es/`, `llm-es-one-file/`, `llm-en/`, `llm-en-one-file/` appear in the `csv_*` folder. Then switch MyFitnessPal off, switch Garmin off, and press Pull to see the toast; then set an output folder outside `$HOME` is not possible from the dialog, so force a failure by renaming `tokens.json` while the app is closed, open the app and press Pull: the bug report box must show both links and the issue link must open GitHub with the log, without the email or the home folder. Rename `tokens.json` back.

- [ ] **Step 7: Optional cross check on a real folder (both binaries still ship).** With the app closed and one real `csv_*` folder `F` copied twice (`F1`, `F2`): run `pack` with Bun (`echo '{"cmd":"pack","mode":"single","lang":"en","csvDir":"F1",...}' | tauri-app/src-tauri/binaries/garmin-core-aarch64-apple-darwin`) and with Go (`... | tauri-app/src-tauri/binaries/garmin-export-core-aarch64-apple-darwin`), then `diff <(grep -v '^Exported:' F1/llm-en-one-file/*.md) <(grep -v '^Exported:' F2/llm-en-one-file/*.md)`. Expected: no output. Record the result in the commit message of Step 8.

- [ ] **Step 8: Commit.**

```bash
git add tauri-app/core tauri-app/ui/app.js
git commit -m "feat: summary, pack and report run in Go; the UI calls one binary"
```

---

### Task 6: Bun leaves the build

Second shippable increment. After this task `bun build --compile` is never run, `tauri-app/sidecar/` does not exist, and the DMG holds one external binary.

**Files:**
- Modify: `tauri-app/package.json`, `tauri-app/src-tauri/tauri.conf.json`, `tauri-app/src-tauri/capabilities/default.json`, `tauri-app/playwright.config.js`, `tauri-app/.gitignore`
- Move: `electron-app/renderer/date-range.js` → `tauri-app/ui/date-range.js`; `electron-app/renderer/__tests__/date-range.test.js` → `tauri-app/ui/__tests__/date-range.test.js`
- Delete: `tauri-app/sidecar/` (including `golden.test.js`, `e2e.test.js`, `garmin-e2e.test.js`, `mfp-e2e.test.js`, `llm-pack.test.js`, `report.test.js`), `tauri-app/core/cmd/fake-mfp/`

- [ ] **Step 1: Check that nothing still needs the sidecar.** Run: `rg -n "garmin-core'|garmin-core\"|binaries/garmin-core[^-]|sidecar/" tauri-app --glob '!node_modules' --glob '!sidecar'` Expected: only the entries this task edits (`package.json`, `tauri.conf.json`, `capabilities/default.json`, comments).

- [ ] **Step 2: date-range.js becomes a tracked UI file.**

```bash
sed -i '' '/^ui\/date-range.js$/d' tauri-app/.gitignore
rm -f tauri-app/ui/date-range.js
git mv electron-app/renderer/date-range.js tauri-app/ui/date-range.js
mkdir -p tauri-app/ui/__tests__
git mv electron-app/renderer/__tests__/date-range.test.js tauri-app/ui/__tests__/date-range.test.js
```

Edit the header comment of `date-range.js` (it names the Electron renderer and main process): "Pure helpers for the Date Range section of `ui/app.js`. The Go core checks the range again at the trust boundary (`internal/guard`)." The test's `require('../date-range')` already resolves. Run: `cd tauri-app && bun test ui/__tests__` Expected: PASS (Bun 1.3.11 runs `node:test` files).

- [ ] **Step 3: Scripts and configuration.**

`tauri-app/package.json` scripts, replace the block with:

```json
"core": "cd core && CGO_ENABLED=0 go build -trimpath -ldflags '-s -w' -o ../src-tauri/binaries/garmin-export-core-$(rustc -vV | awk '/host/{print $2}') ./cmd/garmin-export-core",
"core:universal": "cd core && for a in arm64:aarch64 amd64:x86_64; do CGO_ENABLED=0 GOOS=darwin GOARCH=${a%%:*} go build -trimpath -ldflags '-s -w' -o ../src-tauri/binaries/garmin-export-core-${a##*:}-apple-darwin ./cmd/garmin-export-core || exit 1; done && lipo -create -output ../src-tauri/binaries/garmin-export-core-universal-apple-darwin ../src-tauri/binaries/garmin-export-core-aarch64-apple-darwin ../src-tauri/binaries/garmin-export-core-x86_64-apple-darwin",
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

`cmd/garmin-export-core/main_test.go` already runs the binary against `mfptest` in process (`scenario`), which is what `mfp-e2e.test.js` did; AC-11 and AC-12 of `garmin-e2e.test.js` are covered by the stage 2 binary tests against `garmintest`. Update the `main.go` header comment ("Stage 1 serves the MyFitnessPal export; the Garmin commands move here in stage 2") to "the whole backend of Garmin Export: Garmin, MyFitnessPal, the LLM pack and the bug report" and the `protocol.go` package comment ("shared with the Bun sidecar") to "one JSON request line in; JSON lines out".

- [ ] **Step 5: Build the universal app in a worktree and measure.**

```bash
git worktree add ../../.worktrees/stage3-build HEAD
cd ../../.worktrees/stage3-build/tauri-app && bun install && bun run release
ls -l src-tauri/target/universal-apple-darwin/release/bundle/dmg/*.dmg
ls -l "src-tauri/target/universal-apple-darwin/release/bundle/macos/Garmin Export.app/Contents/MacOS/"
lipo -info src-tauri/binaries/garmin-export-core-universal-apple-darwin
codesign --verify --deep --strict "src-tauri/target/universal-apple-darwin/release/bundle/macos/Garmin Export.app" && echo signed
```

Expected: one DMG; `Contents/MacOS` holds `garmin-export` and `garmin-export-core` only (no `garmin-core`); `lipo -info` says `x86_64 arm64`; `codesign` prints `signed`. Write the DMG size into the commit message. Then check assumption A5: `bun run build` (host target) in the same worktree must also succeed with `beforeBuildCommand: bun run core:universal`; if Tauri rejects it, keep `"build": "bun run core && tauri build --config '{\"build\":{\"beforeBuildCommand\":\"\"}}'"` and say so in the commit. Remove the worktree afterwards: `git worktree remove --force ../../.worktrees/stage3-build`.

- [ ] **Step 6: Run the app from the worktree build once (ask the user).** Open the built `.app` (not the DMG install), export 7 days with the saved login, build one pack. Expected: works; the log shows Go timestamps (`20:53:01`) for every line.

- [ ] **Step 7: Full gate without `electron-app/node_modules`.** Run: `mv electron-app/node_modules /tmp/nm-aside && cd tauri-app && bun run test:core && bun test ui/__tests__ && bunx playwright test && (cd src-tauri && cargo test); mv /tmp/nm-aside ../electron-app/node_modules` Expected: PASS with the folder away (Review Focus 7). Note that `cd electron-app && npm test` is no longer part of the gate: the sidecar tests that compared engines are gone, and the Go goldens are the oracle from Task 7 on.

- [ ] **Step 8: Commit.**

```bash
git add -A tauri-app
git commit -m "build: the Go core is the only external binary; the Bun sidecar and bun build --compile are gone (universal DMG <size> MB)"
```

---

### Task 7: Go owns the fixture, the goldens and the two scripts

**Files:**
- Move: `electron-app/garmin/__tests__/fixtures/export-sample.json` → `tauri-app/core/internal/garmin/testdata/fixtures/export-sample.json`; `electron-app/garmin/__tests__/golden/` → `tauri-app/core/internal/garmin/testdata/golden/`; `electron-app/scripts/make-fixture.js`, `electron-app/scripts/compare-csv.js` → `tauri-app/scripts/`
- Modify: `internal/garmin/csv_test.go` (constants, `UPDATE_GOLDEN`), `cmd/garmin-export-core/main_test.go` (the fixture path from Task 5), the two scripts' header comments

- [ ] **Step 1: Move.**

```bash
mkdir -p tauri-app/core/internal/garmin/testdata/fixtures tauri-app/scripts
git mv electron-app/garmin/__tests__/fixtures/export-sample.json tauri-app/core/internal/garmin/testdata/fixtures/export-sample.json
git mv electron-app/garmin/__tests__/golden tauri-app/core/internal/garmin/testdata/golden
git mv electron-app/scripts/make-fixture.js electron-app/scripts/compare-csv.js tauri-app/scripts/
```

- [ ] **Step 2: Write the failing test change.** In `internal/garmin/csv_test.go` the constants become `fixturePath = "testdata/fixtures/export-sample.json"` and `goldenDir = "testdata/golden"`, and `TestAC01CSVFilesEqualTheGoldenFilesByteForByte` gains, right after `WriteCSVs`:

```go
	if os.Getenv("UPDATE_GOLDEN") != "" { // on purpose only: UPDATE_GOLDEN=1 go test ./internal/garmin -run AC01
		os.RemoveAll(goldenDir)
		if err := os.CopyFS(goldenDir, os.DirFS(csvDir)); err != nil {
			t.Fatal(err)
		}
	}
```

Run: `cd tauri-app/core && go test ./internal/garmin -run AC01 && go test ./cmd/...` Expected: PASS without `UPDATE_GOLDEN` (the files moved, not changed). Do not run with `UPDATE_GOLDEN=1`.

- [ ] **Step 3: Scripts.** Change the usage lines to `bun tauri-app/scripts/compare-csv.js <oldDir> <newDir> [--json]` and `bun tauri-app/scripts/make-fixture.js <garmin_health_*.json> tauri-app/core/internal/garmin/testdata/fixtures/export-sample.json`, and in `make-fixture.js` the sentence "so the regression test sees real Garmin response shapes" to name `internal/garmin/csv_test.go`. Run: `bun tauri-app/scripts/compare-csv.js tauri-app/core/internal/garmin/testdata/golden tauri-app/core/internal/pack/testdata/csv_2024-02-21` Expected: exit 1 and a report that lists the 3 added `mfp_*` files and no differing columns (the script still runs under Bun).

- [ ] **Step 4: Commit.**

```bash
git add -A tauri-app electron-app
git commit -m "test(core): Go owns the Garmin fixture and golden files; UPDATE_GOLDEN moves to the Go test; the two CSV scripts move to tauri-app/scripts"
```

---

### Task 8: `electron-app/` is deleted (needs the user's decision D1)

**Files:**
- Delete: `electron-app/` (everything left: `garmin/`, `util/`, `renderer/`, `main.js`, `preload.js`, `scripts/download-fonts.js`, `BUILD.md`, `package.json`, `package-lock.json`, `Remove Quarantine.command`)

- [ ] **Step 1: Tag the archive point.** `git tag -a archive/electron-app -m "Last commit with electron-app/ (Electron 3.x app and the JavaScript Garmin core used by 4.0 to 4.4)"`. Push the tag with the branch.

- [ ] **Step 2: Check for stragglers.** Run: `rg -n "electron-app" --glob '!node_modules' --glob '!.worktrees' --glob '!docs/superpowers' .` Expected: only `CLAUDE.md`, `README.md`, `tools/*.py`, `workflows/*.md` and `.gitignore` (`electron-app/.omc/`), which Tasks 9 and 10 edit. Anything under `tauri-app/` is a bug: fix it first.

- [ ] **Step 3: Delete.**

```bash
git rm -r electron-app
sed -i '' '/^electron-app\/.omc\/$/d' .gitignore
```

- [ ] **Step 4: Gate.** Run: `cd tauri-app && bun run test:core && bun test ui/__tests__ && bunx playwright test` Expected: PASS.

- [ ] **Step 5: Commit.**

```bash
git add -A
git commit -m "chore: remove electron-app; the 3.x app and the JavaScript core stay at tag archive/electron-app"
```

---

### Task 9: Tools and workflows for the Tauri app (needs the user's permission, D2)

CLAUDE.md: "Never overwrite a workflow without permission." Ask before this task, with the stage tables below in the question. If the user says no: leave `tools/` and `workflows/` untouched, and in Task 10 add one line under the CLAUDE.md tool table: "Both tools target `electron-app/`, retired with tag `archive/electron-app`; 4.x releases are cut by hand with `bun run release` in a worktree and `gh release create`." Task 11 Steps 2 and 4 then use those hand commands.

**Files:**
- Rewrite: `tools/ci_pipeline.py`, `tools/release.py`, `workflows/cicd_pipeline.md`, `workflows/release.md`
- Create: `tauri-app/dist/` (gitignored, the DMG and `.app` the pipeline copies out of the worktree)

`tools/ci_pipeline.py` keeps its name, its PEP 723 header, its exit codes and its flags (`--stages`, `--skip-install`, `--skip-tests`, `--skip-build`, `--no-open`), and gets `--here` (build in this checkout instead of a worktree; only when the user says the app is closed). Stages:

| Stage | What it does | Failure means |
| --- | --- | --- |
| clean | `rm -rf tauri-app/dist tauri-app/src-tauri/binaries` and the `bundle/` folders under `src-tauri/target/*/release/`; never the cargo cache | file permissions |
| install | `bun install` in `tauri-app/` | registry or lockfile |
| test | `bun run test:core`, `bun test ui/__tests__`, `bunx playwright test`, `cargo test` in `src-tauri/` | regression: stop |
| build | `git worktree add .worktrees/build-<sha> HEAD`, `bun install`, `bun run release` there, copy the DMG and the `.app` to `tauri-app/dist/`, `git worktree remove --force` | Go, Rust or bundler error |
| open | `codesign --verify --deep --strict` on `tauri-app/dist/Garmin Export.app`, then `open -n` | signature or launch |

`tools/release.py` keeps its flags and adds nothing. Stages: preflight (clean tree, `gh auth status`, no tag or release `vX.Y.Z`), bump (`version` in `tauri-app/package.json`, `tauri-app/src-tauri/tauri.conf.json` and `tauri-app/src-tauri/Cargo.toml`; refresh `Cargo.lock` offline, assumption A6; commit `chore(release): bump to vX.Y.Z`), merge (unchanged), build (`ci_pipeline.py --no-open`), tag (unchanged), release (`gh release create vX.Y.Z "tauri-app/dist/Garmin Export_X.Y.Z_universal.dmg" --title ... --notes ...`; GitHub renames the asset to `Garmin.Export_X.Y.Z_universal.dmg`).

- [ ] **Step 1: Dry run the old tool once** to keep its output shape: `uv run tools/release.py --bump minor --dry-run | head -20`. Keep the `==>` and `  $` log lines.

- [ ] **Step 2: Rewrite `ci_pipeline.py`** (about 180 lines) and **`release.py`** (about 250 lines; the bump stage edits three files with `json.loads` for the two JSON files and a regex for `Cargo.toml`'s first `version = "..."` line). The `--dry-run` path prints every command.

- [ ] **Step 3: Test the tools without releasing.** Run: `uv run tools/ci_pipeline.py --stages test` (PASS), `uv run tools/ci_pipeline.py --skip-install --skip-tests --no-open` (a DMG in `tauri-app/dist/`), `uv run tools/release.py --bump minor --dry-run` (prints the commands, changes nothing, `git status` clean). For A6, run the bump stage on a throwaway branch and check `git diff --stat` lists `package.json`, `tauri.conf.json`, `Cargo.toml` and `Cargo.lock`; if `Cargo.lock` is missing, move its refresh to after the build stage and write the actual command into the workflow.

- [ ] **Step 4: Workflows.** Rewrite both files with the same sections (Objective, Required Inputs, Tool, Stage contract, Invocation patterns, Expected Outputs, Edge Cases, Self-Improvement Log). Edge cases that stay true: non fast forward merge, `gh auth`, existing tag, mid flight abort. New edge cases: the worktree build is cold (minutes); `--here` only when the app is closed; the universal build needs the two rustup targets and v4.0.0 used an isolated rustup (`RUSTUP_HOME`/`CARGO_HOME`) because Homebrew's Rust lacks `x86_64-apple-darwin`; ad hoc signing (`signingIdentity: "-"`) means Gatekeeper asks on first open and the release notes say so. Start each Self-Improvement Log with one line: "2026-10-16: rewritten for `tauri-app/`; the electron-builder entries live in git history before this date."

- [ ] **Step 5: Commit.**

```bash
git add tools workflows
git commit -m "tools: ci_pipeline and release target the Tauri app; universal DMG built in a worktree"
```

---

### Task 10: Docs

**Files:**
- Modify: `CLAUDE.md`, `README.md`, `tauri-app/src-tauri/src/main.rs` (header comment)

- [ ] **Step 1: CLAUDE.md.**
- WAT Layout: "`electron-app/dist/` for app builds" becomes "`tauri-app/dist/`".
- Available Workflows and Tools tables: the stage 9 descriptions ("Rebuild the Tauri app: clean, install, test, universal DMG in a worktree, launch"; "Cut a release: bump the three version files, FF merge, build, tag, GitHub release with the universal DMG").
- Project Context, first bullet: `tauri-app/` paragraph without the sidecar. Keep: vanilla UI, dev and build commands (`cd tauri-app && bun install && bun run dev`; `bun run build`; `bun run release` with the rustup note), the worktree rule, `ui/dev-mock.js`. Add: `ui/date-range.js` is a tracked file; `beforeBuildCommand` builds the universal Go binary.
- Go core bullet: all commands (`garmin-status`, `garmin-connect`, `garmin-disconnect`, `clear-cache`, `export`, `mfp-export`, `summary`, `pack`, `report`), the packages (`internal/garmin`, `mfp`, `pack`, `report`, `redact`, `guard`, `jsjson`, `protocol`, `atomicfile`), the testdata paths, `UPDATE_GOLDEN=1 go test ./internal/garmin -run AC01` as the only way to rebuild goldens, the stage 3 goldens frozen from the JavaScript on 2026-10-16, `GARMIN_FAKE_SERVER` and `MFP_FAKE_SERVER`.
- Garmin login bullet: point at `internal/garmin/auth.go`; keep the 429 warning.
- Tests bullet: `cd tauri-app && bun run test:core && bun test ui/__tests__ && bunx playwright test && (cd src-tauri && cargo test)`; fixture via `bun tauri-app/scripts/make-fixture.js`; compare via `bun tauri-app/scripts/compare-csv.js`; Go IDs 101+ (stage 2) and 301+ (stage 3).
- LLM pack bullet: `internal/pack`, `UNITS` is `units` in `pack.go`.
- Delete the three `electron-app` bullets and the BUILD.md link; add "The Electron 3.x app and the JavaScript core are at tag `archive/electron-app`."

- [ ] **Step 2: README.** "Build it yourself": prerequisites become Bun, Rust, Go 1.24 or later and the Xcode command line tools; the commands become `cd tauri-app && bun install && bun run dev`; tests as above; the paragraph that compares output with `electron-app/garmin/__tests__/golden` names `tauri-app/core/internal/garmin/testdata/golden`; the two architecture bullets about `tauri-app/sidecar` and `electron-app/garmin` become one bullet about `tauri-app/core` (Go: sign in, client, cache, CSV, MyFitnessPal, LLM pack, bug report; one JSON request on stdin, so the password never appears in the process list; the range, the refresh window and every path are checked there).

- [ ] **Step 3: Comments.** `main.rs` line 1: "Garmin work happens in the Go core; Rust only handles the MyFitnessPal sign in window."

- [ ] **Step 4: Commit.**

```bash
git add CLAUDE.md README.md tauri-app/src-tauri/src/main.rs
git commit -m "docs: one Go core, no sidecar; build, test and release instructions for the Tauri app"
```

---

### Task 11: Full regression, build, smoke, release

**Files:** `tauri-app/package.json`, `tauri.conf.json`, `Cargo.toml` (version, by the tool), `tauri-app/reportes-pruebas/<date>-stage3/` (gitignored evidence)

- [ ] **Step 1: Regression with evidence.**

```bash
D=tauri-app/reportes-pruebas/$(date +%F)-stage3 && mkdir -p $D
(cd tauri-app/core && go vet ./... && go test ./... -v 2>&1 | tee ../$D/go-test.txt | tail -5)
(cd tauri-app && bun test ui/__tests__ 2>&1 | tee $D/ui-test.txt | tail -3)
(cd tauri-app && bunx playwright test --reporter=list,html 2>&1 | tee $D/playwright.txt | tail -3 && cp -r playwright-report $D/)
(cd tauri-app/src-tauri && cargo test 2>&1 | tee ../$D/cargo-test.txt | tail -3)
```

Expected: `ok` for every Go package, 0 failed in Bun, Playwright green on chromium and webkit, cargo `test result: ok`. Count the Go tests whose names carry 301 and above: 19 (AC-301 to AC-306, EP-301 and EP-302, BVA-301, ST-301, DT-301, EG-301 to EG-308).

- [ ] **Step 2: Build.** `uv run tools/ci_pipeline.py --skip-install --skip-tests --no-open` (Task 9), or in a worktree by hand if Task 9 was not approved. Record the DMG size next to the 62 MB of v4.3.0.

- [ ] **Step 3: Smoke in the built app (ask the user; no sign in).** Install the DMG. Garmin row shows Connected from the saved login. Export the last 7 days: rows Done, CSV files in `csv_<today>`, the preview cards filled from `summary`. `L`, build the folder pack in Spanish and the one file pack in English; drag check by eye: `llm-es/` has `CONTEXT.es.md` plus the CSV copies, `llm-en-one-file/` has one `.md`. Close the app, rename `tokens.json` to force `GARMIN_SESSION` on the next Pull, open the app, Pull, open the GitHub link from the report box: the issue body has the log, no email, `~` instead of the home folder. Rename `tokens.json` back. Clear cache: toast "Cache cleared."

- [ ] **Step 4: Release (ask the user; decision D4 on the version).** `uv run tools/release.py --bump minor --notes-file <notes>` with notes that say: one Go core, no Bun sidecar, DMG size before and after, same files on disk, Gatekeeper note unchanged. Expected: tag `v4.5.0`, one asset `Garmin.Export_4.5.0_universal.dmg`.

- [ ] **Step 5: Merge the branch into main** through the release tool's FF merge, then `git worktree remove .worktrees/go-core-stage3`.
