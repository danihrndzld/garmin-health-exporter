# Architecture: Hexagonal core and the public gconnect library

Status: accepted 2026-10-15. Decisions taken under the user's standing autonomy grant, following this analysis (Fable): D1 Hexagonal with the three rules and `arch_test.go`; D2 library in tree at `tauri-app/core/gconnect/` until stage 2 Task 13, then its own repository; D3 name `gconnect` (`github.com/danihrndzld/gconnect`); D4 MIT for the library; D5 MyFitnessPal stays private to the app; D6 raw `json.RawMessage` responses; D7 publish the fake as `gconnecttest`; D8 extraction after Task 13; D9 the live check needs the user (no agent writes the real tokens.json); D10 the `Reporter` + typed request reorg lands with Task 12.

Date: 2026-10-16. Baseline: worktree `.worktrees/go-core-garmin`, branch `feat/go-core-garmin`, commit `a0a096a` (stage 2 Tasks 1 to 5 done; Tasks 6 and 7 being committed by other agents while this was written). Read: `CLAUDE.md`, the stage 1 spec, the stage 2 spec, plan and tech debt note, the stage 3 drafts in `scratchpad/stage3/`, every `.go` file under `tauri-app/core`, `tauri-app/ui/app.js`, `electron-app/garmin/{client,endpoints,exporter,csv-writer}.js`. Every number is counted in this tree unless it says "est."; section 6 lists what was not verified.

## 0. Summary

1. Pattern: **Hexagonal (Ports and Adapters)**, with "strict" defined as three mechanical rules (section 1.2) and a `go test` checker, with a self-test for each rule, that fails the build on a forbidden edge (section 1.8). Three interfaces in total; everything else stays a struct.
2. Library: `github.com/danihrndzld/gconnect`, MIT, stdlib only, **never touches the disk or the environment**, raw `json.RawMessage` responses, ships `gconnecttest` (the fake Garmin). The app keeps `tokens.json`, the cooldown file, the cache, `jsjson`, the CSV writer, the LLM pack, the protocol, `guard`, and all of MyFitnessPal.
3. Sequence: option (c). Right after Tasks 6 and 7 land, move `auth.go`, the token types and `garmintest` into an in-tree, non-internal package `tauri-app/core/gconnect/` and make them disk free (about 200 lines rewritten, 1,324 moved by `git mv`). Tasks 8 and 9 run in parallel with that move. Task 10 writes `endpoints.go`, `client.go`, `redact.go` straight into `gconnect/`. Tasks 11 to 13 unchanged. Extract to the public repo **after** the Task 13 live compare, with `git subtree split` and one import path `sed`.
4. Size: about 1,300 lines move unchanged, about 400 lines change, 0 bytes of protocol, `tokens.json`, UI or CSV output change. Medium refactor, 1 day, pinned by `cmd/garmin-export-core/main_test.go` (282 lines, runs the real binary) and the Bun end to end tests.

## 1. Pattern

### 1.1 Candidates against this code's forces

Forces: 2 data sources that the UI drives as separate commands with different result shapes; 1 driving adapter (one JSON line on stdin, JSON lines on stdout); file storage (`tokens.json`, `mfp_session.json`, two JSON caches, `csv_*`); 2 HTTP clients with `httptest` fakes; byte equal CSV output pinned by 18 goldens; Go stdlib only; 1 developer; 2,210 lines of Go product code and 2,943 of Go tests today (5,153 total, `wc -l`).

| Candidate | What it would add here | Verdict |
| --- | --- | --- |
| Hexagonal (Ports and Adapters) | One inside (use cases + Garmin/MFP logic), driving adapter (`cmd/` + `protocol` + `guard`), driven adapters (files, HTTP) behind narrow seams. Two or three interfaces. | **Chosen.** It names the one real defect today (`internal/mfp/export.go:22` takes `*protocol.Emitter` and a raw `map[string]any`: the inside knows the adapter) and nothing else. |
| Clean Architecture | Four rings (entities, use cases, interface adapters, frameworks) and a DTO per crossing. | No. The "entities" ring would hold `Tokens` and three CSV row types; the DTO layer would double the 7 command handlers. Clean is Hexagonal plus ceremony this size does not pay for. |
| Onion | Same rings as Clean, centred on a domain model. | No. There is no domain model: the data is Garmin's raw JSON, kept raw on purpose for byte equality. |
| Pipes and Filters for the export | Fetch → cache → JSON file → CSV as filters on a stream. | No. `exporter.js:exportHealth` is one 300 line sequence whose order and log lines the UI parses (`app.js` `PHASE_START`, spec "Log lines"). A filter interface would split a function the goldens pin as a whole. Split it into plain functions (`fetchDaily`, `fetchAggregated`, `fetchActivities`, `writeJSON`, `WriteCSVs`), no framework. |
| GoF Strategy for sources | A `Source` interface with Garmin and MFP implementations. | No. Nothing dispatches over sources: the UI calls `export` then `mfp-export` (`app.js:348`, `app.js:717`), the results differ, the failure rules differ (spec "Run rules"). An interface with 2 implementations and 0 polymorphic call sites is bloat. |
| GoF Command for the protocol | One handler per command. | Yes, minimal: `map[string]func(...)` in `cmd/`, one function per command, no interface (section 1.9). |
| GoF Adapter | `protocol.Emitter` as the adapter that satisfies the `Reporter` port; `gconnecttest.Server` as the test double for Garmin. | Yes, it is what Hexagonal means by adapter. |
| GoF Template Method | Shared export skeleton with per source steps. | No. Go has no inheritance, and the two exports share only "loop over dates and write CSVs" (about 15 lines). |

### 1.2 What "strict" means (three rules)

Strict is a dependency direction and a test, not an interface count.

1. **Imports point inward.** `cmd/` may import everything. `internal/garmin` and `internal/mfp` may import `gconnect`, `internal/jsjson`, `internal/atomicfile`, `internal/guard`. `gconnect` imports the standard library only. `internal/protocol` is imported by `cmd/` only. `internal/garmin` and `internal/mfp` never import each other.
2. **The outside world enters through `cmd/` only.** `os.Getenv`, `os.Args`, `os.Stdin`, `os.Stdout` appear under `cmd/` only (test files excepted for `UPDATE_GOLDEN`). The inside gets typed, validated input and a `Reporter`.
3. **The library cannot reach the disk.** `gconnect` never imports `os`, `os/exec`, `path/filepath`, `io/fs`. A library that cannot open a file cannot leak a token to one.

Go idiom refines the pattern in three places: interfaces are declared by the consumer, next to the use that needs them (`internal/garmin.Reporter`, `internal/mfp.Reporter`), not in a `ports` package; concrete types cross the boundary whenever only one implementation exists (`*gconnect.Client`, `garmin.Cache`, `atomicfile.WriteFile`); and the driven side for files is a function (`atomicfile.WriteFile`) and a callback (`Client.OnTokens`), not a repository interface.

### 1.3 Package layout under `tauri-app/core`

```
tauri-app/core/
  go.mod                         unchanged now; after extraction: require github.com/danihrndzld/gconnect vX.Y.Z
  arch_test.go                   NEW: the import rules (section 1.8); `go test .` runs it
  cmd/garmin-export-core/        DRIVING ADAPTER (composition root)
    main.go                      read one request line, look up the command, write the result
    garmin.go                    garmin-status, garmin-connect, garmin-disconnect, export, clear-cache
    mfp.go                       mfp-export
    (stage 3) pack.go, report.go, summary.go
    main_test.go                 unchanged in intent: the real binary over stdin
  cmd/fake-mfp/                  unchanged until stage 3 deletes it
  internal/protocol/             adapter support: Emitter, ReadRequest (cmd only)
  internal/guard/                trust boundary checks; called by cmd before any use case
  gconnect/                      THE LIBRARY, in tree until the extraction (section 3)
    auth.go, tokens.go (types only), endpoints.go, client.go, redact.go, doc.go
    gconnecttest/server.go
  internal/garmin/               app side of Garmin (use cases + policy)
    tokensfile.go                tokens.json: embeds gconnect.Tokens + saved_at + expired marker; StatusOf; Delete
    connect.go                   Connect: cooldown file, gconnect.Auth.Login, Save
    cache.go                     per day JSON files, Trust, InRefreshWindow
    csv.go                       20 extractors, WriteCSVs (jsjson rows)
    export.go                    Export use case (the exporter.js port), PrepareDir
    (stage 3) summary.go
  internal/mfp/                  unchanged files; Export gets the typed request and the Reporter
  internal/jsjson/               pure, stdlib only
  internal/atomicfile/           pure, stdlib only
  (stage 3) internal/pack/, internal/report/   report imports gconnect.Redact (no internal/redact package)
```

### 1.4 Dependency direction (the only allowed edges)

```
cmd ──► protocol, guard, internal/garmin, internal/mfp, gconnect, atomicfile
internal/garmin ──► gconnect, jsjson, atomicfile, guard
internal/mfp    ──► jsjson (if needed), atomicfile, guard
internal/pack, internal/report (stage 3) ──► gconnect (Redact), jsjson, guard
gconnect, jsjson, atomicfile, guard, protocol ──► standard library only
gconnecttest ──► gconnect + standard library
```

Nothing points at `cmd` (Go forbids it) or at `protocol` except `cmd`. `guard` is allowed inside because its functions are pure checks plus `$HOME` resolution and the inside may re-check; the rule that **matters** is that `cmd` runs `guard` before every use case, which `main_test.go` already pins with `GARMIN_INPUT` / `MFP_INPUT` cases.

### 1.5 Ports: exactly three interfaces

```go
// internal/garmin/export.go (consumer side). *protocol.Emitter satisfies it without any change.
type Reporter interface {
	Log(kind, msg string)
	Warn(msg, code string)
	Progress(phase string, current, total int)
}

// internal/mfp/export.go (consumer side; it needs no Warn).
type Reporter interface {
	Log(kind, msg string)
	Progress(phase string, current, total int)
}

// cmd/garmin-export-core/main.go (consumer side): any use case error that carries a protocol code.
type coded interface{ ErrorCode() string }
```

The library has **no** interface. Its two seams are function fields: `Auth.Log func(string)` and `Client.OnTokens func(*Tokens) error` (section 2.4). A one method `TokenStore` interface was considered and rejected: the only implementation would be "write tokens.json", and a func field is the Go spelling of a one method interface.

Typed inputs and outputs replace `map[string]any` at the boundary:

```go
// internal/garmin
type ExportRequest struct {
	StartDate, EndDate string
	RefreshWindow      int
	OutputDir, DataDir string        // already resolved by guard.InsideHome in cmd
	Hosts              gconnect.Hosts // zero value: the real Garmin; cmd sets the loopback fake
	Gap                time.Duration  // 500 ms; cmd sets 0 only with the fake
	Now                func() time.Time
}
type ExportResult struct {
	Path       string   `json:"path"`
	JSONPath   *string  `json:"jsonPath"` // null with garmin:false
	StartDate  string   `json:"startDate"`
	EndDate    string   `json:"endDate"`
	Days       int      `json:"days,omitempty"`
	Activities int      `json:"activities,omitempty"`
	Files      []string `json:"files,omitempty"`
}
func Export(ctx context.Context, r ExportRequest, out Reporter) (*ExportResult, error)
func PrepareDir(outputDir, endDate string) (csvDir string, err error)
func Connect(ctx context.Context, a *gconnect.Auth, dataDir, email, password string, now time.Time) (Status, error)
func StatusOf(dataDir string, now time.Time) Status
func Disconnect(dataDir string) error

// Every failure the UI must name is a *CodeError; cmd maps it through the `coded` interface.
type CodeError struct{ Code, Msg string } // GARMIN_INPUT, GARMIN_SESSION, GARMIN_BAD_PASSWORD, GARMIN_MFA, GARMIN_RATE_LIMITED, GARMIN_BLOCKED, GARMIN_ERROR
func (e *CodeError) Error() string     { return e.Msg }
func (e *CodeError) ErrorCode() string { return e.Code }

// internal/mfp
type ExportRequest struct {
	StartDate, EndDate, DataDir, CSVDir string
	Base string        // test server; "" for the real MFP
	Gap  time.Duration
}
type ExportResult struct {
	Days       int      `json:"days"`
	LoggedDays int      `json:"loggedDays"`
	CSVDir     string   `json:"csvDir"`
	Files      []string `json:"files"`
}
func Export(ctx context.Context, r ExportRequest, out Reporter) (*ExportResult, error)

// internal/protocol: Result takes any; it marshals through a map to force kind:"result" (8 lines).
func (e *Emitter) Result(v any)
```

`cmd` is the only place that knows the wire: it decodes the request map, runs `guard`, reads `GARMIN_FAKE_SERVER` / `MFP_FAKE_SERVER` with the loopback regexp, builds the typed request, calls the use case, and turns `(*T, error)` into `{ok:true, ...}` or `{ok:false, errorCode, error}`.

### 1.6 Where each current file goes

| File today | Goes to | Change |
| --- | --- | --- |
| `cmd/garmin-export-core/main.go` (67) | `cmd/.../main.go` + `garmin.go` + `mfp.go` | the `switch` becomes a `map[string]handler`; one function per command; est. 130 lines over 3 files |
| `cmd/garmin-export-core/main_test.go` (282) | stays | no change: it drives the binary |
| `internal/protocol/protocol.go` (61) | stays | `Result(v any)` |
| `internal/guard/guard.go` (113) | stays | none |
| `internal/atomicfile/*` (32) | stays | none |
| `internal/garmin/auth.go` (637) | `gconnect/auth.go` | `host()` (8 call sites) → `a.Hosts.X`; `DataDir` (7 uses) removed: `Login` returns tokens and does not save; the cooldown (`cooldownLeft`, `startCooldown`, 2 funcs) moves to `internal/garmin/connect.go`; `Refresh` returns tokens and does not save; `ErrNoLogin` stays in the app. est. 60 lines |
| `internal/garmin/auth_test.go` (382) | `gconnect/auth_test.go` + `internal/garmin/connect_test.go` | 11 `DataDir` uses and 24 file assertions split: DT-R9, DT-R11 (cooldown file) and the `tokens.json` checks of ST-T1 to ST-T4 move to `connect_test.go`; the HTTP behaviour (DT-R1 to R8, R10, EG-06, OAuth1 signature) stays with the lib. est. 120 lines touched |
| `internal/garmin/tokens.go` (139) | types (45 lines: `DI`, `OAuth1`, `OAuth2`, `Consumer`, `Tokens`, `isDI`, `isLegacy`) → `gconnect/tokens.go`; the rest → `internal/garmin/tokensfile.go` | `type tokenFile struct { gconnect.Tokens; SavedAt string \`json:"saved_at,omitempty"\`; ExpiredAt string \`json:"expired_at,omitempty"\` }`: `encoding/json` promotes embedded fields, so the bytes of `tokens.json` are unchanged and the Bun cross engine test (AC-12) keeps passing |
| `internal/garmin/tokens_test.go` (136) | `internal/garmin/tokensfile_test.go` | rename only |
| `internal/garmin/fake.go` (18) | deleted | the loopback check moves to `cmd/garmin.go`; the lib gets `Hosts` |
| `internal/garmin/garmintest/server.go` (260) | `gconnect/gconnecttest/server.go` | `New()` without a fixture; `Handle(path string, h http.HandlerFunc)` for consumer fixtures; `Hosts() gconnect.Hosts`; the app's test helper builds the data routes from `export-sample.json` with `gconnect.Lookup(name).URL` |
| Task 7 `internal/garmin/export.go` (`PrepareDir`) | stays | if Task 7 imported `internal/mfp` for `CSVFiles`, replace with a 3 string list in `internal/garmin` (rule 1 forbids the lateral edge) |
| Task 8 `internal/jsjson/*` | stays | none |
| Task 9 `internal/garmin/csv.go` | stays | none |
| Task 10 `endpoints.go`, `client.go`, `redact.go` + tests | **written directly in `gconnect/`** | `Client` uses `OnTokens` and `StopError` instead of `MarkExpired` and `Stop`/`StopCode` fields |
| Task 11 `internal/garmin/cache.go` | stays | none |
| Task 12 `internal/garmin/export.go` (`Export`) | stays | written with `Reporter` and `ExportRequest` from the start |
| `internal/mfp/export.go` (112) | stays | signature only (5 test call sites: `helpers_test.go` 3, `export_test.go` 2) |
| `internal/mfp/client.go:32` `FakeServer()` | `cmd/mfp.go` | the env read leaves the inside; `Options.Base` already exists |
| stage 3 `internal/redact` | does not exist | `gconnect.Redact`, `gconnect.RedactURL`; `internal/report` imports the lib |

### 1.7 Forbidden

- `internal/protocol` imported anywhere but `cmd/`.
- `os.Getenv`, `os.LookupEnv`, `os.Environ`, `os.Args`, `os.Stdin`, `os.Stdout` outside `cmd/` (non test files).
- `gconnect` importing anything outside the standard library, or importing `os`, `os/exec`, `path/filepath`, `io/fs`.
- `internal/garmin` ↔ `internal/mfp` imports, in either direction.
- `map[string]any` as a use case parameter or result (the wire shape belongs to `cmd`).
- A `*protocol.Emitter` parameter in any package but `cmd`.
- `go.mod` `replace` directives committed (section 2.5).
- A third party Go module anywhere (`go.mod` has 0 `require` lines today; the extraction adds exactly one).
- Writing `tokens.json`, the cooldown file or any cache file from `gconnect`.

### 1.8 Mechanical enforcement

`tauri-app/core/arch_test.go`, package `core`, standard library only. `bun run test:core` runs `go vet ./...` and `go test ./...`, which includes `.`.

The first version of this section held a 70 line test that matched the identifier `os` and checked 5 of the 9 items in section 1.7. The Task 7A review got 3 bypasses past it: an alias import (`xos "os"`), a dot import (`. "os"`) and `syscall.Getenv`. It also found that `os.ExpandEnv` and `io/ioutil` were not covered. Ruling R15 replaced that test with the checker described below.

`checkTree(root, modPrefix) ([]string, error)` walks a module, skipping `testdata`. It parses every `.go` file in full and returns one line per violation:

| Rule | Where it applies | What fails |
| --- | --- | --- |
| Library is standard library only | `gconnect/**`, test files included | any import outside the standard library except `gconnect/...` itself |
| Library cannot reach the disk or the process | `gconnect/**`, non-test | importing `os`, `os/exec`, `os/signal`, `path/filepath`, `io/fs`, `io/ioutil`, `syscall` or `unsafe`, under any name. This rule also covers "no writes from gconnect" in section 1.7. |
| Only `cmd` speaks the protocol | everything but `cmd/**` and `internal/protocol`, test files included | importing `internal/protocol`, under any name |
| No lateral edge | `internal/garmin/**` and `internal/mfp/**` | either one importing the other |
| Leaf packages | `internal/jsjson`, `internal/atomicfile`, `internal/guard`, `internal/protocol`, non-test | any import outside the standard library |
| The environment and the standard streams enter through `cmd` | outside `cmd/**`, non-test | a selector `X.Getenv`, `LookupEnv`, `Environ`, `Args`, `Stdin`, `Stdout`, `Stderr`, `ExpandEnv`, `Expand`, `Setenv` or `Unsetenv`, where `X` is whatever local name the file gave `os` or `syscall` (the default name or an alias). A method value such as `var f = os.Getenv` counts. A dot import of `os` or `syscall` fails on its own. |
| No `io/ioutil` | outside `cmd/**`, non-test | importing it, under any name |
| The wire shape belongs to `cmd` | `internal/garmin/**`, `internal/mfp/**`, `gconnect/**`, non-test | an exported func or method whose parameters or results hold `map[string]any` or `map[string]interface{}` at any depth (slices, func types) |
| No `replace` | `go.mod` | a `replace` line or block |
| No third party module | `go.mod` | a `require` of a module that is not in `allowedRequires`. The list is empty today. The extraction (section 2.5) adds exactly one module, `github.com/danihrndzld/gconnect`, to that list. |

The rule "no `*protocol.Emitter` parameter outside `cmd`" follows from the protocol import rule.

Two tests use the checker:
- `TestArchitectureRules` runs it on the real module and fails on every line it returns.
- `TestArchitectureCheckerCatchesEachRule` proves each rule can fail. CL-261 to CL-270 write one small module per rule with `t.TempDir()` and the module path `example.com/fake`, and each module breaks that rule. The tests include the alias, dot import, `syscall`, method value and `io/ioutil` bypasses, and they assert that the reported line names that rule. CL-271 is a clean module, in which `cmd` and test files read the environment and every allowed edge is used, and it must pass. The fixture lists of banned imports and names are written out in the test rather than read from the checker's maps, so dropping a name from a map fails the test. Removing alias resolution, a ban, the `replace` case or the `any` match each made a fixture fail, which was checked by mutation on 2026-10-16.

After the extraction, the library repo carries only the two `gconnect` rules, plus a `TestMain` transport guard (section 2.6) so that no test can reach the real Garmin.

### 1.9 GoF inside the hexagon: used and not used

- **Command**: `cmd/garmin-export-core/main.go` has `type handler func(ctx context.Context, req map[string]any, out *protocol.Emitter) (any, error)` and `var commands = map[string]handler`, with 6 entries: `garmin-status`, `garmin-disconnect`, `garmin-connect`, `clear-cache` and `export` in `garmin.go`, and `mfp-export` in `mfp.go`. `main` reads one line. If the line is not JSON it writes `{ok:false, error:"Invalid request."}`. If the command is not in the map it writes `{ok:false, error:"Unknown command."}`. Otherwise it calls the handler. On success it writes `Result({ok:true}, value)`. On an error it writes `{ok:false, error: err.Error()}` and adds `errorCode` only when the error satisfies `coded` (`interface{ ErrorCode() string }`, found with `errors.As`). There is no fallback code by command name: `clear-cache` failures have carried no `errorCode` since stage 1, and every failure the UI names is a `*garmin.CodeError` or a `*mfp.CodeError` (`GARMIN_INPUT`, `GARMIN_ERROR`, the sign in codes, `MFP_INPUT`, `MFP_SESSION`, `MFP_RATE_LIMITED`, `MFP_ERROR`). `garmin-disconnect` wraps a failed delete in `CodeError{GARMIN_ERROR}`. `garmin.Connect` maps a `LoginCode` that the app does not know to `GARMIN_ERROR`, never to an empty code. Before `mfp.Export`, `cmd` runs `guard` (`ValidateRange`, then `InsideHome` on `dataDir` and on `csvDir`) and passes the resolved dirs. `mfp.Export` runs the same checks again, with the same messages. The handler type is a func type, not an interface.
- **Adapter**: `protocol.Emitter` satisfies the two `Reporter` ports; `gconnecttest.Server` stands in for four Garmin hosts.
- **Strategy for sources**: not used (section 1.1).
- **Template Method**: not used.
- **Pipes and Filters**: not used; the export loop is plain functions in one file.

## 2. The public library

### 2.1 Existing libraries, and why this one is not redundant

Checked on 2026-10-16 with `gh repo view` and `gh api search/code` (counts are code search hits in that repository):

| Library | Last push | Stars | License | Deps in `go.mod` | DI mobile login (`mobile/api/login` + `diauth`) | `httptest` fake |
| --- | --- | --- | --- | --- | --- | --- |
| [abrander/garmin-connect](https://github.com/abrander/garmin-connect) | 2025-10-06 | 153 | MIT | 2, `go 1.15` | 0 hits for either | 0 hits |
| [barnes-c/go-garminconnect](https://github.com/barnes-c/go-garminconnect) | 2026-10-09 | 9 | Apache-2.0 | 12, `go 1.24.0` | 2 and 3 hits | 5 files |
| [llehouerou/go-garmin](https://github.com/llehouerou/go-garmin) (lib + CLI + MCP server) | 2026-02-17 | 7 | none | 18, `go 1.25.5` | not checked (API rate limit) | 1 file |
| [harrybrwn/go-garmin](https://github.com/harrybrwn/go-garmin), [ndeloof/go-garmin](https://github.com/ndeloof/go-garmin) | 2025-10-09, 2026-09-28 | 3, 0 | Apache-2.0 | `go 1.23`, `go 1.26` | not checked | not checked |
| [phacops/garminconnect](https://github.com/phacops/garminconnect) ([pkg.go.dev](https://pkg.go.dev/github.com/phacops/garminconnect)), [benammann/garmin-connect-go](https://github.com/benammann/garmin-connect-go) | 2018-01-02, 2023-03-06 | 1, 1 | MIT | | 0 hits (benammann) | |
| `sstent/go-garth`, `sstent/go-garminconnect` | | | | | GitHub returns 404; they exist only on a [Gitea mirror](https://gitea.service.dc1.fbleagh.duckdns.org/sstent/go-garth/src/branch/feat/pkg-reorg/garth/doc.go) the search found | |
| Python: [matin/garth](https://github.com/matin/garth), [cyberjunky/python-garminconnect](https://github.com/cyberjunky/python-garminconnect) | | | | not Go | the DI flow this app ports (`auth.js` header: "as python-garminconnect 0.3.x") | |

So the field is not empty: `barnes-c/go-garminconnect` does the DI login and has fakes. This library is still worth publishing if it is the **only one** with all four of: standard library only (0 dependencies; the closest active peer has 12); no disk and no environment access (the caller owns token storage; peers write session files, unverified for each); raw bytes out (`json.RawMessage`, body read through `io.LimitReader`, never decoded and re-encoded, which is what a byte exact exporter or a research pipeline needs); and a published fake Garmin (`gconnecttest`) with the sign in decision table (wrong password, MFA, 429, Cloudflare 403, captcha, DI refused, widget fallback) that consumers can run their own tests against. The token refresh policy is also stricter than the JavaScript it replaces: a refresh failure on a network error, 5xx, 403 or 429 keeps the tokens (`auth.go:600-603`); only a 400 or 401 from diauth or a 401 from the OAuth2 exchange reports `ErrSessionDead`.

If the user wants a library only when it is the sole option, the answer is: it is not the sole option; `barnes-c/go-garminconnect` (Apache-2.0, 12 dependencies) could be adopted instead, at the cost of giving up stdlib only, raw bytes and the fake, and of re-pinning every DT-R and ST-T test against someone else's auth code. Not recommended.

### 2.2 Boundary

In `gconnect` (the API knowledge, no policy):
- sign in: DI mobile SSO (`/mobile/api/login` → `diauth.garmin.com`) first, the widget + OAuth1 + OAuth2 exchange once as fallback; the `LoginError` codes; `Headers` (the native Android headers that connectapi requires, measured 2026-10-14);
- token types with Garmin's field names; `Refresh`; `Fresh` (refresh before a margin); `ErrSessionDead`;
- `Client`: pacing (500 ms default), 401 refresh once then dead, 403 skip with the 5 in a row stop, 429 with `Retry-After` (seconds or HTTP date, 60 s cap) and 2 retries, 204 and empty body as no data, 5 retries with `min(5000·2^n + jitter, 60000)` ms on network errors and timeouts, 1 MB body cap, redacted 200 character error previews;
- the endpoint table with the same names and the same URL builders as `endpoints.js` minus the 2 dead entries; `Profile` (display name) because 6 daily URLs need it;
- `Redact`, `RedactURL`;
- `gconnecttest`: the fake with the control knobs and the call log.

In the app (policy, storage, output):
- `tokens.json` with `saved_at` and the expired marker; `sso-cooldown.json` (15 minutes); the `garmin-status` decision table; `StatusOf` never sends a request;
- the per day JSON cache and its final rule; the refresh window;
- the export loop, its log lines and progress phases; the JSON export file; `jsjson`; the CSV writer and the goldens; `PrepareDir`;
- `guard`, `protocol`, `atomicfile`, the `GARMIN_FAKE_SERVER` loopback rule, the 0 ms gap only with the fake;
- MyFitnessPal, all of it (section 2.8);
- stage 3: pack, report, summary.

Typed responses: **no**. Raw `json.RawMessage` only, with one exception (`Profile` returns the display name). Reasons: the goldens depend on JavaScript key order and `72.0` → `72`, which any typed decode and re-encode destroys; Garmin's shapes drift (the tech debt note lists 8 of them); typed accessors can be added later without breaking raw callers.

### 2.3 Name, module path, license

- Name: **`gconnect`**, module `github.com/danihrndzld/gconnect`, package `gconnect`, test package `gconnecttest`. `gh repo view danihrndzld/gconnect` returns "could not resolve", so the path is free. pkg.go.dev has `gitlab.com/natitec_public/gkit/gconnect` (a ConnectRPC transport package) and `github.com/elins-research-club/gconnect-webserver`; different paths, no conflict. The name does not start with a Garmin trademark, which keeps the "unofficial" status visible in the import path itself. Alternatives if the user dislikes it: `garthgo` (credits `matin/garth`, whose DI flow this ports; free), `connectapi-go` (free; vaguer).
- README first line: "gconnect is an unofficial Go client for the private Garmin Connect API. It is not affiliated with, endorsed by, or supported by Garmin Ltd. Garmin and Garmin Connect are trademarks of Garmin Ltd. or its subsidiaries."
- License: **MIT**. `abrander/garmin-connect` and `phacops/garminconnect` are MIT; `barnes-c` and `harrybrwn` are Apache-2.0. MIT is shorter and matches the Python peers (unverified for garth and python-garminconnect, see section 6). The app repository has **no `LICENSE` file** (`ls LICENSE*` finds none; `package.json` has no `license` field): the user must pick one for the library before the first push, and should pick one for the app at the same time.

### 2.4 API surface

```go
package gconnect // import "github.com/danihrndzld/gconnect"

// Hosts names the Garmin hosts. The zero value is the real Garmin. Tests set every field to one httptest URL.
type Hosts struct{ SSO, DIAuth, ConnectAPI, Consumer string }
func SameHost(base string) Hosts // all four = base
func (h Hosts) sso() string      // and 3 siblings: field, else the real host

type DI struct {
	AccessToken           string `json:"access_token"`
	RefreshToken          string `json:"refresh_token,omitempty"`
	ClientID              string `json:"client_id"`
	ExpiresAt             int64  `json:"expires_at"`
	RefreshTokenExpiresAt *int64 `json:"refresh_token_expires_at"`
}
type OAuth1 struct{ Token string `json:"oauth_token"`; Secret string `json:"oauth_token_secret"` }
type OAuth2 struct { AccessToken, TokenType, RefreshToken string; ExpiresAt, RefreshTokenExpiresAt int64 } // same tags as today
type Consumer struct{ Key string `json:"consumer_key"`; Secret string `json:"consumer_secret"` }
type Tokens struct {
	DI       *DI       `json:"di,omitempty"`
	OAuth1   *OAuth1   `json:"oauth1,omitempty"`
	OAuth2   *OAuth2   `json:"oauth2,omitempty"`
	Consumer *Consumer `json:"consumer,omitempty"`
}
type Kind int
const (KindNone Kind = iota; KindDI; KindLegacy)
func (t *Tokens) Kind() Kind
func (t *Tokens) AccessExpiresAt() int64 // DI.ExpiresAt or OAuth2.ExpiresAt

type Auth struct {
	Hosts Hosts
	HTTP  *http.Client     // nil: 30 s timeout
	Now   func() time.Time // nil: time.Now
	Log   func(msg string) // optional; never receives the password
}
func (a *Auth) Login(ctx context.Context, email, password string) (*Tokens, error)                   // error is always *LoginError
func (a *Auth) Refresh(ctx context.Context, t *Tokens) (*Tokens, error)                              // ErrSessionDead, or a plain error that means "keep the old tokens"
func (a *Auth) Fresh(ctx context.Context, t *Tokens, margin time.Duration) (tok *Tokens, refreshed bool, err error)
func Headers(t *Tokens) http.Header // native Android headers for DI, the mobile UA for legacy, Authorization set

type LoginCode string
const (
	BadPassword LoginCode = "bad_password"
	MFARequired LoginCode = "mfa"
	RateLimited LoginCode = "rate_limited" // the caller should wait 15 minutes before another Login
	Blocked     LoginCode = "blocked"      // 403 or another non 200 on both flows
	LoginFailed LoginCode = "error"        // network, not JSON, unexpected status
)
type LoginError struct{ Code LoginCode; Msg string } // Msg never holds the password
var ErrSessionDead = errors.New("gconnect: the saved login is no longer valid")
var ErrNoTokens = errors.New("gconnect: the tokens hold neither a DI nor a legacy login") // Refresh/Fresh on Kind() == KindNone (today auth.go:622 returns the app's ErrNoLogin); the app maps it to GARMIN_SESSION

type Params struct{ Date, Start, End, DisplayName, ActivityID string; Offset, Limit int }
type Endpoint struct {
	Name, Type, AliasOf string // Type: "daily" | "aggregated" | "activity" | "list"; AliasOf: same URL as another endpoint
	Header              http.Header // goals carries Sec-Fetch-Site: same-origin
	URL                 func(base string, p Params) string
}
var Daily, Aggregated, Activity, List []Endpoint
func Lookup(name string) (Endpoint, bool)

type Result struct {
	Data   json.RawMessage // nil on 204, an empty body, or any failure
	Status int
	Code   string // "" on success; EMPTY_BODY, BAD_JSON, TIMEOUT, NETWORK, AUTH, FORBIDDEN, RATE_LIMIT, HTTP_4XX, HTTP_5XX
	Err    string // redacted, at most 200 characters
}
type StopReason string
const (StopSessionDead StopReason = "session_dead"; StopRateLimited StopReason = "rate_limited"; StopBlocked StopReason = "blocked"; StopRefreshFailed StopReason = "refresh_failed")
type StopError struct{ Reason StopReason; Msg string }

type Client struct {
	Auth     *Auth
	Tokens   *Tokens
	OnTokens func(*Tokens) error // called after every successful refresh; the app writes tokens.json here. On an error the client keeps the refreshed tokens in memory, logs it as "warn" with code SAVE_FAILED and goes on; the app decides whether a failed save is fatal
	Gap      time.Duration       // minimum time between requests; 0 means 500 ms
	Timeout  time.Duration       // 0 means 30 s
	Sleep    func(context.Context, time.Duration) error // tests inject
	Jitter   func() time.Duration
	Log      func(kind, msg, code string) // "dim" "[no data] hrv (HTTP 204)" ""; "warn" "[skip] hrv: HTTP 500" "HTTP_5XX"
}
func (c *Client) Get(ctx context.Context, ep Endpoint, p Params) Result
func (c *Client) GetURL(ctx context.Context, url string, h http.Header) Result
func (c *Client) Safe(ctx context.Context, ep Endpoint, p Params) json.RawMessage // logs [skip] / [no data]; nil when there is nothing
func (c *Client) Profile(ctx context.Context) (displayName string, err error)
func (c *Client) Stopped() *StopError // non nil once the run must stop; every later Get returns it

func Redact(s string) string   // Bearer/Basic values, cookies, JWT shaped strings, secret query keys, secret JSON pairs
func RedactURL(u string) string

package gconnecttest // import "github.com/danihrndzld/gconnect/gconnecttest"
type Control struct {
	Password     string // default "right-password"
	MobileLogin  string // "ok" | "mfa" | "cloudflare" | "429" | "captcha"
	DIAuth       string // "ok" | "429" | "fail"
	RefreshFail  int    // the DI refresh grant and the OAuth1 to OAuth2 exchange answer this status
	TokenTTL     int    // seconds, default 3600
	Widget429    bool
	WidgetStatus int
	DataStatus   map[string]int // path prefix -> status, every time
	FailOnce     map[string]int // path prefix -> status, once
}
type Call struct{ Method, Path, Query string; Header http.Header; Body string }
func New() *Server
func (s *Server) URL() string
func (s *Server) Hosts() gconnect.Hosts
func (s *Server) Control() *Control
func (s *Server) Calls(path string) []Call
func (s *Server) Reset()
func (s *Server) Close()
func (s *Server) Handle(path string, h http.HandlerFunc) // data routes; the consumer registers its own fixtures
func JWT(claims any) string
```

Changes from the stage 2 plan's Task 10 interface: `Client.Stop`/`StopCode` string fields become `Stopped() *StopError`; `MarkExpired` is not called by the client (the app does it when `Stopped().Reason == StopSessionDead`); `Auth.DataDir` is gone; `FakeServer()` is gone; `garmintest.New(fixture)` becomes `New()` + `Handle`. The app's cooldown and `LoginResult` mapping (`LoginCode` → `GARMIN_*`) are 8 lines in `internal/garmin/connect.go`.

### 2.5 How the app consumes it

- Until the extraction: `tauri-app/core/gconnect/` is a package of the app module. Import path `github.com/danihrndzld/garmin-health-exporter/tauri-app/core/gconnect`. No `go.mod` change.
- Extraction (after Task 13): `git subtree split -P tauri-app/core/gconnect -b gconnect-export`, push to the new repository (history kept), tag `v0.1.0`; in the app, `sed` the import path, `go get github.com/danihrndzld/gconnect@v0.1.0`, delete the in tree folder. `go.mod` gains exactly one `require` line. `go.sum` is committed.
- Local iteration on both at once: `tauri-app/core/go.work` (gitignored) with `use . ../../../gconnect`; CI runs with `GOWORK=off` so the tagged version is what builds. Never a `replace` in the committed `go.mod` (it breaks `go install` for anyone else and the release build).
- Versions: SemVer, `v0.x` while the app is the only known consumer; `v1.0.0` after the API has survived two app releases without a breaking change. `go.mod` of the library: `go 1.24`, no `toolchain` line, no requires.
- The app pins a tag, not a commit; a bump is one commit (`go get ... && go mod tidy`).

### 2.6 Testing strategy for the library

- Shape: the same trophy as the app. Table driven, every test name starts with its technique ID (the app's convention: EP, BVA, DT, ST, EG; library IDs start at 401 to stay disjoint from the app's 101+ and stage 3's 301+). The existing decision table in `auth_test.go` (DT-R1 to DT-R10, DT-R211 to R222, ST-T201 to T211, EG-06, EG-221, the OAuth1 signature test) moves with the file and is renumbered.
- Fake: `gconnecttest` is the only server any test talks to. `TestMain` wraps `http.DefaultTransport` with a `DialContext` that refuses any address that is not loopback (10 lines), so no test can sign in to the real Garmin even by mistake; the Garmin rate limit (429 after 4 sign ins in 40 minutes, measured 2026-10-14) makes this rule a hard one.
- Goldens: the endpoint URL table (`endpoints_test.go`, generated once from `endpoints.js` with `node -e`, 30 rows); the OAuth1 base string and signature with a fixed nonce and timestamp (expected value from `auth.js:signOAuth1`); the redaction cases (the 14 inputs of `redact.js`'s tests, later the 28 the stage 3 draft freezes). No golden holds a real token: the fake issues `refresh-N` and `legacy-N` strings.
- Race: `go test -race ./...` on every push (`Client` has a mutex for `Stopped`; the fake has one for its call log).
- Coverage: measure on the first green run with `go test -coverprofile`, set the gate 5 points below the measured value, raise it when it is beaten; target 90% of statements for `auth.go` and `client.go` (est.), which the decision tables already approach in the app.
- Static checks: `gofmt -l`, `go vet ./...`, the arch test (stdlib only, no disk). No third party linter in CI, to keep the "stdlib only" promise honest for the build as well.
- CI: `.github/workflows/ci.yml`, matrix `ubuntu-latest` × `macos-latest` and Go `1.24.x` × `stable`, steps `go vet`, `go test -race -count=1 -coverprofile`, `gofmt`, coverage gate with `go tool cover -func` and `awk`. No secrets, no network.
- Fixtures: the library's own tests carry one hand written 3 endpoint fixture for the data path; the app's scrubbed `export-sample.json` (`scripts/make-fixture.js`, per run secret) never enters the library repository.

### 2.7 README outline

1. One paragraph: what it is, unofficial disclaimer, trademark note, "works as of <date of the last verified live run>".
2. Status table: DI mobile sign in, legacy sign in fallback, refresh, MFA (not supported, returns `MFARequired`), endpoints covered (list the names), Go 1.24+, 0 dependencies.
3. Install: `go get github.com/danihrndzld/gconnect`.
4. Sixty second example: `Login` once, save `Tokens` yourself (JSON, mode 0600), build a `Client`, `Get` the daily summary, print the raw JSON.
5. Sign in flows and what each error code means; the 15 minute wait after `RateLimited`.
6. Token lifecycle: `Fresh`, `OnTokens`, `ErrSessionDead`, and the rule "a network error, 5xx, 403 or 429 never invalidates a saved login".
7. Rate limits, with the measured numbers (500 ms between requests; 4 sign ins per 40 minutes observed; `Retry-After` honoured, 60 s cap).
8. Endpoints: the table with `Name`, `Type`, `AliasOf`, params.
9. Testing your code with `gconnecttest` (one example with `Handle`).
10. Security: no disk, no environment, redacted errors; you own storage; never log tokens.
11. Legal and ethics (section 2.8).
12. Versioning and compatibility promise (v0 caveat).
13. Contributing: how to add an endpoint (URL builder + one table row + one golden line), how fixtures are scrubbed, no real data in PRs.
14. Credits: `matin/garth` and `cyberjunky/python-garminconnect` (the flows), the legacy flow's OAuth consumer file is fetched at run time from `thegarth.s3.amazonaws.com`, a bucket that belongs to the garth project, not to this library (a run time dependency on a third party that the README must name).
15. License.

### 2.8 Legal and ethical notes

- The API is private and reverse engineered. The README must say that Garmin can change or block it at any time, that the library may stop working without notice, and that the user is responsible for complying with Garmin's terms. Do not quote a Garmin terms clause without reading it first (not fetched here; section 6).
- Rate limits are not a courtesy: the measured 429 after 4 sign ins in 40 minutes locks the user's own account out of the mobile app too. The library enforces a 500 ms default gap and documents the sign in limit; it does not implement a cooldown file (that is policy and storage, kept in the app).
- Credentials: `Login` takes the password as a parameter and never logs or stores it; `LoginError.Msg` is tested to never contain the password; the fake issues synthetic tokens only; the arch test proves the library cannot write a file.
- Fixtures: no real account data in the library repository, ever. The app's fixture is scrubbed with a per run secret; the library ships hand written fixtures only.
- MyFitnessPal stays out of the library and out of any public code: the stage 1 spec records that MFP's Terms of Service (section 5) forbid automated access and that the user accepted that risk for their own account only. Publishing an MFP client as a library would be publishing a tool for others to break those terms.
- `thegarth.s3.amazonaws.com`: the legacy fallback downloads the OAuth consumer key from a bucket run by the garth project. Name it, credit it, and make the `Consumer` host configurable (`Hosts.Consumer`), so a consumer can pin their own copy.
- Trademarks: "Garmin" and "Garmin Connect" appear only in prose with the trademark note, never in the module path or the repository name.

## 3. Sequencing

Stage 2 after Task 7 still has: Task 8 `jsjson` (new package, independent of auth), Task 9 CSV writer (depends on 8 only), Task 10 endpoints + client + redact (depends on `Auth`), Task 11 cache (independent), Task 12 export (depends on all), Task 13 default flip and the only live check.

| Option | What moves and when | Lines moved (`git mv`) | Lines rewritten | Extra cost | Saves |
| --- | --- | --- | --- | --- | --- |
| (a) finish stage 2 as planned, then refactor and extract | after Task 13: auth, tokens types, garmintest, plus the Task 10 files | 1,324 now existing + est. 1,230 from Task 10 (endpoints ~250, client ~300, redact ~80, tests ~600) = est. 2,550 | est. 300: `host()` 8 sites, `DataDir` 7 + 11, 24 file assertions re-homed, `client.go` from `MarkExpired`/`Stop` fields to `OnTokens`/`StopError` (~60), `export.go` call sites (~20), `main.go` (~30) | rewriting freshly written, golden pinned Task 10 and 12 code a second time | nothing |
| (b) pause after Task 7, build the library and the public repo first, then Tasks 8 to 12 against it | now: auth, tokens types, garmintest; Task 10 written in the external repo | 1,324 | est. 200 (the same `host()`/`DataDir`/test split) | repo, README, CI, `v0.1.0`, `go.work` before the client exists; every Task 10 iteration crosses two repositories; the API is designed before its main consumer (`Export`, Task 12) exists, so churn is likely; a README that claims "sign in works" before the only live check (Task 13 Step 2) | the est. 100 lines of double rewrite of (a) |
| **(c) in tree library package now, public repo after Task 13** | right after Tasks 6 and 7 land: `git mv` auth, tokens types, garmintest into `tauri-app/core/gconnect/` and make them disk free and env free; Tasks 8, 9, 11 unchanged and in parallel; Task 10 written in `gconnect/`; Task 12 written with `Reporter` + typed request; after Task 13: `git subtree split`, tag, `go get` | 1,324 | est. 200 now + est. 150 with Task 12 (`main.go` command map, `mfp.Export` signature and its 5 test call sites, `protocol.Result(any)`, `arch_test.go`) | none during stage 2: one module, one repository, one test command | the double rewrite of (a) and the cross repository friction of (b) |

Risks are equal across the three: no option adds a live sign in (the 429 limit is untouched; every test runs against the fake), and no option touches `jsjson` or the CSV writer (the 18 goldens are unaffected). The one scheduling risk is specific to (b) and (c): `main.go` and `internal/garmin` are being edited by the Task 6 and 7 agents right now, so the move must wait for their commits, which is why (c) says "right after Tasks 6 and 7 land" and not "now".

Recommendation: **(c)**. Two consequences for the stage 2 plan: Task 10's interface block changes as listed at the end of section 2.4 (the test IDs and the fake's knobs stay), and Task 12 writes `Export(ctx, ExportRequest, Reporter)` instead of `Export(ctx, req map[string]any, out *protocol.Emitter, opts)`. One consequence for the stage 3 draft: its `internal/redact` package and the "move `redact.go` out of `internal/garmin`" row disappear; `internal/report` imports `gconnect.Redact`.

## 4. Is it a major refactor? Numbers

- Moves (package clause and import path only): `auth.go` 637, `auth_test.go` 382, `garmintest/server.go` 260, 45 lines of `tokens.go` = **1,324 lines**, all with `git mv` so `git blame` survives.
- Rewrites and new code: `auth.go` est. 60 (`Hosts`, no `DataDir`, no cooldown, `Login` and `Refresh` return without saving); `auth_test.go` est. 120 (11 `DataDir` uses, 24 file assertions split off); `internal/garmin/connect.go` est. 60 new (cooldown file, `Login`, `Save`, `LoginCode` → `GARMIN_*`); `tokensfile.go` est. 30 (the embedding); `gconnecttest` est. 20 (`New()`, `Handle`, `Hosts`); `main.go` 67 → est. 130 over 3 files; `internal/mfp/export.go` est. 30 plus 5 test call sites est. 25; `protocol.Result(any)` 8; `arch_test.go` 70 new. **About 400 lines changed or new**, in 2 commits (the library move; the hexagonal `cmd` reorg with Task 12).
- Deleted: `internal/garmin/fake.go` (18).
- What breaks: nothing a user or the UI can see. The protocol lines, the `tokens.json` bytes (embedding keeps `di`/`oauth1`/`oauth2`/`consumer`/`saved_at`/`expired_at`), `sso-cooldown.json`, the Bun fallback, the DMG build and the Playwright suite are untouched. Three internal Go symbols change for in repository callers only: `garmin.Auth{DataDir}`, `garmin.FakeServer()`, `mfp.Export(req, *Emitter)`.
- Proof: `cmd/garmin-export-core/main_test.go` (282 lines, runs the real binary over stdin against the fakes) and `tauri-app/sidecar/*.test.js` (Bun, end to end) are the regression net and need no change.
- Verdict: a **medium** refactor by volume (1,300 moved, 400 changed) and a **small** one by blast radius (0 external contracts). One day of work; not "major".

## 5. Decisions the user must make

1. D1: adopt Hexagonal with the three rules and the arch test (section 1.2, 1.8). Yes or no.
2. D2: the library lives in tree at `tauri-app/core/gconnect/` until Task 13, then moves to its own repository (recommended), or a separate repository from day one (option (b)).
3. D3: the name. `gconnect` (recommended, path free), `garthgo`, or `connectapi-go`.
4. D4: the license. MIT (recommended) or Apache-2.0. The app repository has no `LICENSE` file either; decide both.
5. D5: MyFitnessPal stays private to the app (recommended; MFP's terms forbid automated access).
6. D6: raw `json.RawMessage` responses only (recommended) or typed models from the start.
7. D7: publish the fake as `gconnecttest` (recommended) or keep it internal.
8. D8: extraction after the Task 13 live compare (recommended) or before.
9. D9: the live check. The stage 2 spec has no "Spike results (Go)" section: Go's TLS against `diauth.garmin.com` and `connectapi.garmin.com` is unverified in the repository. Before the README says "works", one DI refresh and one profile request must run live (Task 0 Step 3 or Task 13 Step 2), with the user's permission and no sign in.
10. D10: the `Reporter` + typed request reorg lands with Task 12 (recommended, the code is written once) or as a separate commit after Task 13.

## 6. Not verified

- Go's TLS fingerprint against Garmin's Cloudflare fronted hosts: the spec's Task 0 results are not in the spec, and no `garmin-spike` folder exists in the scratchpad. Only the Bun engine is known to sign in today.
- Whether Task 5 Step 8 (one real sign in from the Go binary) ran. The lead should confirm.
- Line estimates marked "est." for Task 10 output and for the rewrites; the moved line counts are `wc -l` of this tree.
- Competitors: `barnes-c/go-garminconnect` dependencies (12 `require` lines counted, contents not read) and whether it writes session files; `llehouerou/go-garmin`, `harrybrwn/go-garmin`, `ndeloof/go-garmin` login flows (GitHub code search rate limited the checks); `sstent/go-garth` contents (404 on GitHub).
- Licenses of `matin/garth` and `cyberjunky/python-garminconnect` (not fetched).
- Garmin's terms of use clause on automated access (not fetched; the README must not quote it unread).
- (Verified 2026-10-16, `scratchpad/arch/embedcheck/main.go`, `go run`): embedding `gconnect.Tokens` in the app's `tokenFile` gives byte equal `json.MarshalIndent` output for the DI (232 bytes), legacy (350) and expired marker (42) shapes, and both structs decode the same file. `TestSaveKeepsAuthJsShapeAndMode` and AC-12 remain the regression proof after the move.
- The in tree `gconnect` folder name versus Go tooling: a non `internal` package in the app module is importable by anyone who imports the app module; harmless, and it ends at the extraction.
