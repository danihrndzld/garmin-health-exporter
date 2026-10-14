# Go backend stage 1: MyFitnessPal export, implementation plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add a Go binary, `garmin-export-core`, that downloads the user's MyFitnessPal food diary and writes three CSV files next to the Garmin CSV files, plus the Rust login window and the UI that drive it.

**Architecture:** The Go binary speaks the same protocol as the Bun sidecar: one JSON request line on stdin, JSON lines out. Rust opens an in app MFP login window, copies the session cookies to `mfp_session.json` (mode 0600), and never sees the password. After the Bun sidecar finishes the Garmin export, the UI starts the Go binary with `mfp-export`, and then calls the Bun `summary` command so that the LLM pack lists the new files.

**Tech Stack:** Go 1.27 standard library only (`net/http`, `encoding/json`, `encoding/csv`, `net/http/httptest`), Rust with Tauri 2.12 (`WebviewWindowBuilder`, `cookies()`), vanilla JS UI, Bun for the end to end test.

**Spec:** `docs/superpowers/specs/2026-10-14-go-core-mfp-design.md`

## Global Constraints

- Go module path `github.com/danihrndzld/garmin-health-exporter/tauri-app/core`, in `tauri-app/core/`, with `go 1.24` in `go.mod`, and no third party Go modules.
- Release builds use `CGO_ENABLED=0 go build -trimpath -ldflags "-s -w"` for `arm64` and `amd64`, then `lipo` into `garmin-export-core-universal-apple-darwin`.
- The diary request uses exactly `types=food_entry,exercise_entry,steps_aggregate`. An unknown type makes MFP return an empty list without an error.
- Requests to MFP are at least 1 second apart. The client stops at the first HTTP 429.
- `MFP_FAKE_SERVER` is accepted only when it matches `^http://(127\.0\.0\.1|localhost):\d+$`, and it is read on every request.
- Every path in a request must resolve inside `$HOME` after symlinks are resolved. Ranges are `YYYY-MM-DD` dates, with start on or before end, and at most 90 days.
- `mfp_session.json` and the cache files are written with mode 0600, through a temporary file and a rename.
- The Go binary never opens `garmin_cache.db`.
- A cached day is final only when it has at least one food entry and `fetched_at` is 7 or more days after its date.
- On a day with `logged=no`, the total columns are empty, not 0.
- Days are keyed by the diary date, never by `consumed_at`. Energy is always kcal; kilojoules are divided by 4.184.
- Commits use the author `Daniel Hernández <danisnowman@gmail.com>` and carry no AI attribution.
- User facing text follows the plain writing rules: complete sentences, no dashes, no jargon.

## Review Focus

1. A missing or corrupt `mfp_session.json` should end with `errorCode: "MFP_SESSION"` and the message "Connect MyFitnessPal first.", not a crash. Pinned in Task 5.
2. A range that ends today should fetch today on every run, because today is never final. Pinned in Task 3.
3. A food entry with `null` nutrients should give empty cells, not 0. Pinned in Task 4.
4. A food description with commas, quotes or a line break, e.g. `Pan "Bimbo", integral`, should still give a valid CSV row. Pinned in Task 4.
5. Cancelling an export halfway (the UI kills the process) should never leave a half written session or cache file. Pinned in Task 3 and Task 2 by the temporary file and rename, with a test that a failed write keeps the old file.

---

## File structure

```
tauri-app/core/
  go.mod
  cmd/garmin-export-core/main.go        reads the request, dispatches commands, prints the result
  cmd/fake-mfp/main.go                  test helper: serves mfptest.Server on a free port, prints the URL
  internal/protocol/protocol.go         Emitter (log, progress, result lines) and ReadRequest
  internal/protocol/protocol_test.go
  internal/guard/guard.go               InsideHome, ValidateRange, Dates
  internal/guard/guard_test.go
  internal/mfp/session.go               Session, LoadSession, Save, Merge
  internal/mfp/session_test.go
  internal/mfp/types.go                 DiaryItem and the nested JSON types
  internal/mfp/client.go                Client: Auth, Goals, Diary, rate limit, errors
  internal/mfp/client_test.go
  internal/mfp/cache.go                 per day JSON files, final rule
  internal/mfp/cache_test.go
  internal/mfp/csv.go                   WriteFiles: mfp_food.csv, mfp_daily.csv, mfp_exercise.csv
  internal/mfp/csv_test.go
  internal/mfp/export.go                Export: the mfp-export command
  internal/mfp/export_test.go
  internal/mfp/mfptest/server.go        fake MFP server used by Go tests and by cmd/fake-mfp
  internal/mfp/testdata/diary/*.json    fixture days, shaped like real v2/diary answers
  internal/mfp/testdata/goals.json
  internal/mfp/testdata/golden/*.csv    expected CSV output
tauri-app/src-tauri/src/mfp.rs          mfp_connect, mfp_disconnect, mfp_status
tauri-app/src-tauri/src/main.rs         registers the commands
tauri-app/sidecar/mfp-e2e.test.js       bun end to end test of the Go binary
```

Modified: `tauri-app/package.json` (build scripts), `tauri-app/src-tauri/tauri.conf.json` (`externalBin`), `tauri-app/src-tauri/capabilities/default.json` (spawn permission), `tauri-app/src-tauri/Cargo.toml` (serde), `tauri-app/sidecar/cli.js` (clear cache), `tauri-app/sidecar/llm-pack.js` and its test (descriptions, units, context line), `tauri-app/ui/index.html`, `app.js`, `app.css`, `dev-mock.js` (MFP row and run row), `README.md`, `CLAUDE.md`.

---

### Task 0: Go spike on the real account (throwaway)

This task answers the three open questions in the spec before any product code is written. Nothing from it goes into the repository except the answers, which go into the spec.

**Files:**
- Create (scratchpad only): `<scratchpad>/mfp-spike/main.go`, `<scratchpad>/mfp-spike/dump_cookies.py`

- [ ] **Step 1: Export the browser session to JSON.** The user signs in to myfitnesspal.com in Zen with "remember me", opens the diary, and quits Zen. Then save this file and ask the user to run it with `! uv run --python 3.12 --with browser-cookie3 python <scratchpad>/mfp-spike/dump_cookies.py`. It writes `<scratchpad>/mfp-spike/session.json` with mode 0600 and prints only cookie names.

```python
import glob, json, os, browser_cookie3
f = max(glob.glob(os.path.expanduser("~/Library/Application Support/zen/Profiles/*/cookies.sqlite")), key=os.path.getmtime)
jar = browser_cookie3.firefox(cookie_file=f, domain_name="myfitnesspal.com")
out = os.path.join(os.path.dirname(__file__), "session.json")
fd = os.open(out, os.O_WRONLY | os.O_CREAT | os.O_TRUNC, 0o600)
with os.fdopen(fd, "w") as fh:
    json.dump({"cookies": [{"name": c.name, "value": c.value, "domain": c.domain, "path": c.path, "expires": c.expires or 0} for c in jar],
               "user_agent": "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/18.6 Safari/605.1.15"}, fh)
print(sorted(c.name for c in jar))
```

- [ ] **Step 2: Write the spike.** It prints status codes, field names and nutrient units only, never values of cookies or tokens.

```go
package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"sort"
	"strings"
	"time"
)

type cookie struct {
	Name, Value, Domain, Path string
	Expires                   int64
}
type session struct {
	Cookies   []cookie `json:"cookies"`
	UserAgent string   `json:"user_agent"`
}

func main() {
	raw, _ := os.ReadFile("session.json")
	var s session
	if err := json.Unmarshal(raw, &s); err != nil {
		panic(err)
	}
	header := func(names map[string]bool) string {
		var parts []string
		for _, c := range s.Cookies {
			if names == nil || names[c.Name] {
				parts = append(parts, c.Name+"="+c.Value)
			}
		}
		return strings.Join(parts, "; ")
	}
	get := func(url string, h map[string]string) (*http.Response, []byte) {
		req, _ := http.NewRequest("GET", url, nil)
		req.Header.Set("User-Agent", s.UserAgent)
		for k, v := range h {
			req.Header.Set(k, v)
		}
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			panic(err)
		}
		defer resp.Body.Close()
		b, _ := io.ReadAll(resp.Body)
		time.Sleep(time.Second)
		return resp, b
	}
	for _, c := range s.Cookies {
		if c.Name == "__Secure-next-auth.session-token" {
			fmt.Println("session-token expires:", time.Unix(c.Expires, 0).Format(time.RFC3339))
		}
	}
	// Q1a: auth_token with only the session token, then with all cookies.
	var tok struct {
		AccessToken string `json:"access_token"`
		UserID      string `json:"user_id"`
		ExpiresIn   int    `json:"expires_in"`
	}
	for _, names := range []map[string]bool{{"__Secure-next-auth.session-token": true}, nil} {
		resp, b := get("https://www.myfitnesspal.com/user/auth_token?refresh=true", map[string]string{"Cookie": header(names)})
		var setNames []string
		for _, c := range resp.Cookies() {
			setNames = append(setNames, c.Name)
		}
		fmt.Printf("auth_token cookies=%v -> HTTP %d %s json=%v set-cookie=%v\n", names != nil, resp.StatusCode, resp.Header.Get("Content-Type"), json.Valid(b), setNames)
		if json.Valid(b) {
			_ = json.Unmarshal(b, &tok)
		}
	}
	fmt.Println("token expires_in:", tok.ExpiresIn, "user id present:", tok.UserID != "")
	api := map[string]string{"Authorization": "Bearer " + tok.AccessToken, "mfp-client-id": "mfp-main-js", "mfp-user-id": tok.UserID, "Accept": "application/json"}
	// Q1b: diary with only the Bearer token (no cookies).
	resp, b := get("https://api.myfitnesspal.com/v2/diary?entry_date=2026-10-06&types=food_entry,exercise_entry,steps_aggregate", api)
	fmt.Println("diary without cookies -> HTTP", resp.StatusCode)
	var diary struct{ Items []map[string]any }
	_ = json.Unmarshal(b, &diary)
	// Q3: nutrient units, and the keys of every item type.
	for _, it := range diary.Items {
		keys := make([]string, 0, len(it))
		for k := range it {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		fmt.Println(it["type"], "keys:", keys)
		if it["type"] == "food_entry" {
			nc := it["nutritional_contents"].(map[string]any)
			fmt.Println("  energy:", nc["energy"], "calcium:", nc["calcium"], "iron:", nc["iron"], "vitamin_d:", nc["vitamin_d"])
			food := it["food"].(map[string]any)
			fmt.Println("  food.nutritional_contents energy:", food["nutritional_contents"].(map[string]any)["energy"], "servings:", it["servings"])
		}
		if it["type"] == "exercise_entry" {
			b, _ := json.Marshal(it)
			fmt.Println("  exercise item shape:", string(b)[:min(len(b), 600)])
		}
	}
	resp, _ = get("https://api.myfitnesspal.com/v2/nutrient-goals?date=2026-10-06", api)
	fmt.Println("nutrient-goals -> HTTP", resp.StatusCode)
}
```

- [ ] **Step 3: Run it.** `cd <scratchpad>/mfp-spike && go mod init spike && go run .` Expected: `auth_token` returns `application/json` at least with all cookies, and the diary returns 200.

- [ ] **Step 4: Record the answers in the spec.** Append a section "Spike results (Go)" to `docs/superpowers/specs/2026-10-14-go-core-mfp-design.md` with: whether only the session token is enough, whether the diary works without cookies, the session token expiry and the `expires_in` of the token, whether `auth_token` sets cookies, the units of calcium, iron and vitamin D (milligrams or percent), whether the entry `nutritional_contents` are already multiplied by the servings (compare with the food's own values), and the exact keys of `exercise_entry`. If any key differs from the struct tags in Task 2 Step 3 (`types.go`), edit those tags before starting Task 2. If calcium, iron and vitamin D are percents, rename the three columns in Task 4 to `calcium_pct_dv`, `iron_pct_dv` and `vitamin_d_pct_dv`; if they are milligrams, rename them to `calcium_mg`, `iron_mg` and `vitamin_d_mcg` or `vitamin_d_iu` to match the unit shown. If Go gets HTTP 403 where Python got 200, stop and add a `utls` task before Task 2.

- [ ] **Step 5: Commit the spec change.**

```bash
git add docs/superpowers/specs/2026-10-14-go-core-mfp-design.md
git commit -m "docs(spec): record the Go spike results for MyFitnessPal"
```

Delete `session.json` from the scratchpad after the commit.

---

### Task 1: Go module, protocol and input checks

**Files:**
- Create: `tauri-app/core/go.mod`, `internal/protocol/protocol.go`, `internal/protocol/protocol_test.go`, `internal/guard/guard.go`, `internal/guard/guard_test.go`, `cmd/garmin-export-core/main.go`

**Interfaces:**
- Produces: `protocol.Emitter{Out io.Writer}` with `Log(kind, msg string)`, `Progress(phase string, current, total int)`, `Result(v map[string]any)`; `protocol.ReadRequest(r io.Reader) (map[string]any, error)`; `guard.InsideHome(p string) (string, error)`; `guard.ValidateRange(start, end string, maxDays int) error`; `guard.Dates(start, end string) []string`.

- [ ] **Step 1: Create the module.**

```bash
mkdir -p tauri-app/core && cd tauri-app/core && go mod init github.com/danihrndzld/garmin-health-exporter/tauri-app/core && go mod edit -go=1.24
```

- [ ] **Step 2: Write the failing tests.**

`internal/protocol/protocol_test.go`:

```go
package protocol

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
)

func TestEmitterWritesOneJSONObjectPerLine(t *testing.T) {
	var buf bytes.Buffer
	e := Emitter{Out: &buf}
	e.Log("dim", "hello")
	e.Progress("mfp", 2, 5)
	e.Result(map[string]any{"ok": true})
	lines := strings.Split(strings.TrimSpace(buf.String()), "\n")
	if len(lines) != 3 {
		t.Fatalf("got %d lines", len(lines))
	}
	var last map[string]any
	if err := json.Unmarshal([]byte(lines[2]), &last); err != nil || last["kind"] != "result" || last["ok"] != true {
		t.Fatalf("bad result line %q", lines[2])
	}
	var first map[string]any
	_ = json.Unmarshal([]byte(lines[0]), &first)
	if first["kind"] != "log" || first["type"] != "dim" || first["msg"] != "hello" || first["ts"] == "" {
		t.Fatalf("bad log line %q", lines[0])
	}
}

func TestReadRequestUsesOnlyTheFirstLine(t *testing.T) {
	req, err := ReadRequest(strings.NewReader("{\"cmd\":\"mfp-export\"}\n{\"cmd\":\"other\"}\n"))
	if err != nil || req["cmd"] != "mfp-export" {
		t.Fatalf("got %v %v", req, err)
	}
	if _, err := ReadRequest(strings.NewReader("not json\n")); err == nil {
		t.Fatal("expected an error for invalid JSON")
	}
}
```

`internal/guard/guard_test.go`:

```go
package guard

import (
	"os"
	"path/filepath"
	"testing"
)

func TestInsideHome(t *testing.T) {
	home, _ := os.UserHomeDir()
	in, err := os.MkdirTemp(home, ".guard-test-")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(in)
	if _, err := InsideHome(in); err != nil {
		t.Fatalf("dir in home rejected: %v", err)
	}
	for _, bad := range []string{"/etc", "/tmp", home + "-evil", filepath.Join(in, "..", "..", "..")} {
		if _, err := InsideHome(bad); err == nil {
			t.Errorf("%s accepted", bad)
		}
	}
	link := filepath.Join(in, "link")
	_ = os.Symlink("/etc", link)
	if _, err := InsideHome(link); err == nil {
		t.Error("symlink out of home accepted")
	}
	// A path that does not exist yet is checked through its nearest existing parent.
	if _, err := InsideHome(filepath.Join(in, "new", "dir")); err != nil {
		t.Errorf("new dir in home rejected: %v", err)
	}
}

func TestValidateRange(t *testing.T) {
	ok := [][2]string{{"2026-10-01", "2026-10-01"}, {"2026-07-17", "2026-10-14"}}
	for _, r := range ok {
		if err := ValidateRange(r[0], r[1], 90); err != nil {
			t.Errorf("%v: %v", r, err)
		}
	}
	bad := [][2]string{{"2026-07-16", "2026-10-14"}, {"2026-10-02", "2026-10-01"}, {"2026-13-01", "2026-13-02"}, {"", "2026-10-01"}}
	for _, r := range bad {
		if err := ValidateRange(r[0], r[1], 90); err == nil {
			t.Errorf("%v accepted", r)
		}
	}
	if d := Dates("2026-09-29", "2026-10-02"); len(d) != 4 || d[0] != "2026-09-29" || d[3] != "2026-10-02" {
		t.Errorf("Dates = %v", d)
	}
}
```

- [ ] **Step 3: Run them and see them fail.** Run: `cd tauri-app/core && go test ./internal/...` Expected: FAIL, `undefined: Emitter`, `undefined: InsideHome`.

- [ ] **Step 4: Implement.**

`internal/protocol/protocol.go`:

```go
// Package protocol is the stdin/stdout contract shared with the Bun sidecar:
// one JSON request line in; JSON lines out with kind log, progress or result.
package protocol

import (
	"bufio"
	"encoding/json"
	"io"
	"sync"
	"time"
)

type Emitter struct {
	Out io.Writer
	mu  sync.Mutex
}

func (e *Emitter) write(v map[string]any) {
	e.mu.Lock()
	defer e.mu.Unlock()
	b, _ := json.Marshal(v)
	e.Out.Write(append(b, '\n'))
}

func (e *Emitter) Log(kind, msg string) {
	e.write(map[string]any{"kind": "log", "type": kind, "msg": msg, "ts": time.Now().Format("15:04:05")})
}

func (e *Emitter) Progress(phase string, current, total int) {
	e.write(map[string]any{"kind": "progress", "phase": phase, "current": current, "total": total})
}

func (e *Emitter) Result(v map[string]any) {
	out := map[string]any{"kind": "result"}
	for k, x := range v {
		out[k] = x
	}
	e.write(out)
}

// ReadRequest reads the first line only: Tauri's shell Child can write but cannot close stdin.
func ReadRequest(r io.Reader) (map[string]any, error) {
	line, err := bufio.NewReader(r).ReadString('\n')
	if err != nil && line == "" {
		return nil, err
	}
	var req map[string]any
	if err := json.Unmarshal([]byte(line), &req); err != nil {
		return nil, err
	}
	return req, nil
}
```

`internal/guard/guard.go`:

```go
// Package guard holds the checks that make this binary a trust boundary:
// paths must stay inside $HOME after symlinks, and ranges are short and valid.
package guard

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const layout = "2006-01-02"

// InsideHome returns the resolved path when it is inside the user's home folder.
// A path that does not exist yet is resolved through its nearest existing parent.
func InsideHome(p string) (string, error) {
	if p == "" {
		return "", errors.New("path is empty")
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	home, err = filepath.EvalSymlinks(home)
	if err != nil {
		return "", err
	}
	abs, err := filepath.Abs(p)
	if err != nil {
		return "", err
	}
	existing, rest := abs, ""
	for {
		if _, err := os.Lstat(existing); err == nil {
			break
		}
		parent := filepath.Dir(existing)
		if parent == existing {
			return "", errors.New("path has no existing parent")
		}
		rest = filepath.Join(filepath.Base(existing), rest)
		existing = parent
	}
	resolved, err := filepath.EvalSymlinks(existing)
	if err != nil {
		return "", err
	}
	resolved = filepath.Join(resolved, rest)
	// Compare with a trailing separator: /Users/dani-evil must not pass as /Users/dani.
	if resolved != home && !strings.HasPrefix(resolved, home+string(filepath.Separator)) {
		return "", errors.New("path must be within your home directory")
	}
	return resolved, nil
}

func ValidateRange(start, end string, maxDays int) error {
	s, err1 := time.Parse(layout, start)
	e, err2 := time.Parse(layout, end)
	if err1 != nil || err2 != nil {
		return errors.New("startDate and endDate must be YYYY-MM-DD")
	}
	if e.Before(s) {
		return errors.New("startDate must be on or before endDate")
	}
	if int(e.Sub(s).Hours()/24)+1 > maxDays {
		return errors.New("the range can be at most 90 days")
	}
	return nil
}

// Dates lists every date from start to end, inclusive, oldest first.
func Dates(start, end string) []string {
	s, _ := time.Parse(layout, start)
	e, _ := time.Parse(layout, end)
	var out []string
	for d := s; !d.After(e); d = d.AddDate(0, 0, 1) {
		out = append(out, d.Format(layout))
	}
	return out
}
```

`cmd/garmin-export-core/main.go` (the `mfp-export` case is added in Task 5):

```go
// garmin-export-core: the Go backend for Garmin Export. Stage 1 serves the
// MyFitnessPal export; the Garmin commands move here in stage 2.
package main

import (
	"os"

	"github.com/danihrndzld/garmin-health-exporter/tauri-app/core/internal/protocol"
)

func main() {
	out := &protocol.Emitter{Out: os.Stdout}
	req, err := protocol.ReadRequest(os.Stdin)
	if err != nil {
		out.Result(map[string]any{"ok": false, "error": "Invalid request."})
		return
	}
	switch req["cmd"] {
	default:
		out.Result(map[string]any{"ok": false, "error": "Unknown command."})
	}
}
```

- [ ] **Step 5: Run the tests.** Run: `cd tauri-app/core && go vet ./... && go test ./...` Expected: PASS.

- [ ] **Step 6: Commit.**

```bash
git add tauri-app/core
git commit -m "feat(core): Go module with the sidecar protocol and input checks"
```

---

### Task 2: Session file and MFP client

**Files:**
- Create: `internal/mfp/session.go`, `session_test.go`, `types.go`, `client.go`, `client_test.go`, `internal/mfp/mfptest/server.go`

**Interfaces:**
- Consumes: nothing from Task 1.
- Produces: `mfp.Session{Cookies []Cookie; UserAgent string; SavedAt string; SessionExpires int64}`, `mfp.Cookie{Name, Value, Domain, Path string; Expires int64}`, `mfp.LoadSession(path string) (*Session, error)`, `(*Session).Save(path string) error`, `(*Session).Merge(set []*http.Cookie) bool`; `mfp.Client`, `mfp.NewClient(s *Session, opts Options) *Client`, `mfp.Options{Gap time.Duration; Base string; HTTP *http.Client}`, `(*Client).Auth(ctx) error`, `(*Client).Goals(ctx, date string) (Goals, error)`, `(*Client).Diary(ctx, date string) ([]DiaryItem, error)`, errors `mfp.ErrSession`, `mfp.ErrRateLimited`; `mfptest.NewServer(dir string) *mfptest.Server` with fields `URL string`, `Calls []string`, and knobs `AuthHTML bool`, `Diary429 bool`, `Diary401Once bool`, `SetCookie string`.

- [ ] **Step 1: Write the fixture days.** Use the real field names from the spike. Create `internal/mfp/testdata/diary/2026-10-05.json`:

```json
{"items": [
 {"type": "food_entry", "date": "2026-10-05", "meal_name": "Breakfast", "meal_position": 0,
  "consumed_at": "2026-10-05T07:40:00-06:00", "logged_at": "2026-10-05T08:02:11-06:00",
  "food": {"description": "Pan \"Bimbo\", integral", "brand_name": "Bimbo", "verified": true},
  "serving_size": {"value": 1, "unit": "slice", "gram_weight": 28, "nutrition_multiplier": 1},
  "servings": 2,
  "nutritional_contents": {"energy": {"unit": "calories", "value": 140}, "protein": 6, "carbohydrates": 24, "fiber": 4,
   "sugar": 3, "added_sugars": 2, "fat": 2, "saturated_fat": 0.4, "trans_fat": 0, "cholesterol": 0,
   "sodium": 260, "potassium": 120, "calcium": 4, "iron": 8, "vitamin_d": null}},
 {"type": "food_entry", "date": "2026-10-05", "meal_name": "Lunch", "meal_position": 1,
  "consumed_at": "2026-10-05T13:10:00-06:00", "logged_at": "2026-10-05T13:30:00-06:00",
  "food": {"description": "Arroz con pollo\nde la casa", "brand_name": "", "verified": false},
  "serving_size": {"value": 1, "unit": "cup", "gram_weight": 240, "nutrition_multiplier": 1},
  "servings": 1.5,
  "nutritional_contents": {"energy": {"unit": "calories", "value": 615}, "protein": 42, "carbohydrates": 70, "fiber": 3,
   "sugar": 2, "added_sugars": null, "fat": 16, "saturated_fat": 4, "trans_fat": null, "cholesterol": 110,
   "sodium": 980, "potassium": null, "calcium": null, "iron": null, "vitamin_d": null}},
 {"type": "exercise_entry", "date": "2026-10-05", "exercise": {"description": "Walking, 5 km/h", "type": "cardio"},
  "duration": 2700, "energy": {"unit": "calories", "value": 180}, "source": "MFP"}
]}
```

`internal/mfp/testdata/diary/2026-10-06.json` (a day with only exercise, i.e., no food logged):

```json
{"items": [{"type": "exercise_entry", "date": "2026-10-06", "exercise": {"description": "Strength training", "type": "strength"},
  "duration": 3600, "energy": {"unit": "calories", "value": 210}, "source": "Garmin"}]}
```

`internal/mfp/testdata/diary/2026-10-07.json` (an account that logs in kilojoules):

```json
{"items": [{"type": "food_entry", "date": "2026-10-07", "meal_name": "Snacks", "meal_position": 3,
  "consumed_at": "2026-10-07T16:00:00-06:00", "logged_at": "2026-10-07T16:05:00-06:00",
  "food": {"description": "Banana", "brand_name": "", "verified": true},
  "serving_size": {"value": 1, "unit": "medium", "gram_weight": 118, "nutrition_multiplier": 1},
  "servings": 1,
  "nutritional_contents": {"energy": {"unit": "kilojoules", "value": 439}, "protein": 1.3, "carbohydrates": 27, "fiber": 3.1,
   "sugar": 14, "fat": 0.4, "sodium": 1, "potassium": 422}}]}
```

`internal/mfp/testdata/goals.json`:

```json
{"items": [{"default_goal": {"energy": {"unit": "calories", "value": 2200}, "protein": 150, "carbohydrates": 220, "fat": 73, "sodium": 2300, "sugar": 50}}]}
```

- [ ] **Step 2: Write the fake server.** `internal/mfp/mfptest/server.go`:

```go
// Package mfptest is a fake MyFitnessPal for tests: auth_token, nutrient-goals and
// v2/diary, answered from testdata files. Days without a file answer {"items":[]}.
package mfptest

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"sync"
)

type Server struct {
	URL          string
	Calls        []string
	AuthHTML     bool   // answer auth_token with HTML, like an expired session
	Diary429     bool   // answer every diary request with 429
	Diary401Once bool   // answer the next diary request with 401
	SetCookie    string // Set-Cookie header sent with auth_token, to test rotation
	mu           sync.Mutex
	srv          *httptest.Server
}

var day = regexp.MustCompile(`^\d{4}-\d{2}-\d{2}$`)

func NewServer(dir string) *Server {
	s := &Server{}
	mux := http.NewServeMux()
	mux.HandleFunc("/user/auth_token", func(w http.ResponseWriter, r *http.Request) {
		s.record(r)
		if s.AuthHTML || !hasSession(r) {
			w.Header().Set("Content-Type", "text/html")
			w.Write([]byte("<html>Log in</html>"))
			return
		}
		if s.SetCookie != "" {
			w.Header().Add("Set-Cookie", s.SetCookie)
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"access_token":"test-token","user_id":"42","expires_in":3600}`))
	})
	mux.HandleFunc("/v2/nutrient-goals", func(w http.ResponseWriter, r *http.Request) {
		s.record(r)
		serve(w, filepath.Join(dir, "goals.json"))
	})
	mux.HandleFunc("/v2/diary", func(w http.ResponseWriter, r *http.Request) {
		s.record(r)
		if r.Header.Get("Authorization") != "Bearer test-token" || r.Header.Get("mfp-client-id") != "mfp-main-js" || r.Header.Get("mfp-user-id") != "42" {
			w.WriteHeader(401)
			return
		}
		if r.URL.Query().Get("types") != "food_entry,exercise_entry,steps_aggregate" {
			w.Write([]byte(`{"items":[]}`)) // the real API also answers an unknown type with nothing
			return
		}
		s.mu.Lock()
		once := s.Diary401Once
		s.Diary401Once = false
		s.mu.Unlock()
		if once {
			w.WriteHeader(401)
			return
		}
		if s.Diary429 {
			w.WriteHeader(429)
			return
		}
		d := r.URL.Query().Get("entry_date")
		if !day.MatchString(d) {
			w.WriteHeader(400)
			return
		}
		serve(w, filepath.Join(dir, "diary", d+".json"))
	})
	s.srv = httptest.NewServer(mux)
	s.URL = s.srv.URL
	return s
}

func hasSession(r *http.Request) bool {
	c, err := r.Cookie("__Secure-next-auth.session-token")
	return err == nil && c.Value != ""
}

func serve(w http.ResponseWriter, path string) {
	w.Header().Set("Content-Type", "application/json")
	b, err := os.ReadFile(path)
	if err != nil {
		b = []byte(`{"items":[]}`)
	}
	w.Write(b)
}

func (s *Server) record(r *http.Request) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.Calls = append(s.Calls, r.URL.Path+"?"+r.URL.RawQuery)
}

func (s *Server) Close() { s.srv.Close() }

// CallCount counts recorded requests whose path starts with prefix.
func (s *Server) CallCount(prefix string) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	n := 0
	for _, c := range s.Calls {
		if len(c) >= len(prefix) && c[:len(prefix)] == prefix {
			n++
		}
	}
	return n
}
```

- [ ] **Step 3: Write the types.** `internal/mfp/types.go` (edit the tags here if the spike in Task 0 found different keys):

```go
package mfp

// Energy is in "calories" (kcal) or "kilojoules".
type Energy struct {
	Unit  string  `json:"unit"`
	Value float64 `json:"value"`
}

func (e *Energy) Kcal() *float64 {
	if e == nil {
		return nil
	}
	v := e.Value
	if e.Unit == "kilojoules" {
		v = v / 4.184
	}
	return &v
}

// Nutrients are pointers, so a null from MFP stays empty in the CSV instead of 0.
type Nutrients struct {
	Energy       *Energy  `json:"energy"`
	Protein      *float64 `json:"protein"`
	Carbohydrate *float64 `json:"carbohydrates"`
	Fiber        *float64 `json:"fiber"`
	Sugar        *float64 `json:"sugar"`
	AddedSugars  *float64 `json:"added_sugars"`
	Fat          *float64 `json:"fat"`
	SaturatedFat *float64 `json:"saturated_fat"`
	TransFat     *float64 `json:"trans_fat"`
	Cholesterol  *float64 `json:"cholesterol"`
	Sodium       *float64 `json:"sodium"`
	Potassium    *float64 `json:"potassium"`
	Calcium      *float64 `json:"calcium"`
	Iron         *float64 `json:"iron"`
	VitaminD     *float64 `json:"vitamin_d"`
}

type DiaryItem struct {
	Type         string `json:"type"`
	Date         string `json:"date"`
	MealName     string `json:"meal_name"`
	MealPosition int    `json:"meal_position"`
	ConsumedAt   string `json:"consumed_at"`
	LoggedAt     string `json:"logged_at"`
	Food         struct {
		Description string `json:"description"`
		BrandName   string `json:"brand_name"`
		Verified    bool   `json:"verified"`
	} `json:"food"`
	ServingSize struct {
		Value      float64 `json:"value"`
		Unit       string  `json:"unit"`
		GramWeight float64 `json:"gram_weight"`
	} `json:"serving_size"`
	Servings  float64   `json:"servings"`
	Nutrients Nutrients `json:"nutritional_contents"`
	// exercise_entry fields
	Exercise struct {
		Description string `json:"description"`
		Type        string `json:"type"`
	} `json:"exercise"`
	DurationSec float64 `json:"duration"`
	Energy      *Energy `json:"energy"`
	Source      string  `json:"source"`
}

type Goals struct {
	EnergyKcal, Protein, Carbohydrate, Fat, Sodium, Sugar *float64
}
```

- [ ] **Step 4: Write the failing client and session tests.** `internal/mfp/client_test.go`:

```go
package mfp

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/danihrndzld/garmin-health-exporter/tauri-app/core/internal/mfp/mfptest"
)

func testSession() *Session {
	return &Session{UserAgent: "test-agent", Cookies: []Cookie{{Name: "__Secure-next-auth.session-token", Value: "s1", Domain: ".myfitnesspal.com", Path: "/"}}}
}

func newTestClient(t *testing.T) (*Client, *mfptest.Server) {
	srv := mfptest.NewServer("testdata")
	t.Cleanup(srv.Close)
	return NewClient(testSession(), Options{Base: srv.URL}), srv
}

func TestAuthThenDiary(t *testing.T) {
	c, srv := newTestClient(t)
	ctx := context.Background()
	if err := c.Auth(ctx); err != nil {
		t.Fatal(err)
	}
	items, err := c.Diary(ctx, "2026-10-05")
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 3 || items[0].Food.Description != `Pan "Bimbo", integral` {
		t.Fatalf("items = %+v", items)
	}
	if srv.CallCount("/v2/diary?") != 1 {
		t.Fatalf("calls = %v", srv.Calls)
	}
}

func TestExpiredSessionIsErrSession(t *testing.T) {
	c, srv := newTestClient(t)
	srv.AuthHTML = true
	if err := c.Auth(context.Background()); !errors.Is(err, ErrSession) {
		t.Fatalf("err = %v", err)
	}
}

func TestDiary401RefreshesOnceAndRetries(t *testing.T) {
	c, srv := newTestClient(t)
	ctx := context.Background()
	_ = c.Auth(ctx)
	srv.Diary401Once = true
	if _, err := c.Diary(ctx, "2026-10-05"); err != nil {
		t.Fatal(err)
	}
	if srv.CallCount("/user/auth_token") != 2 {
		t.Fatalf("expected one refresh, calls = %v", srv.Calls)
	}
}

func TestDiary429StopsWithErrRateLimited(t *testing.T) {
	c, srv := newTestClient(t)
	ctx := context.Background()
	_ = c.Auth(ctx)
	srv.Diary429 = true
	if _, err := c.Diary(ctx, "2026-10-05"); !errors.Is(err, ErrRateLimited) {
		t.Fatalf("err = %v", err)
	}
	if srv.CallCount("/v2/diary?") != 1 {
		t.Fatal("a 429 must not be retried")
	}
}

func TestRotatedCookiesAreMergedAndSaved(t *testing.T) {
	c, srv := newTestClient(t)
	srv.SetCookie = "__Secure-next-auth.session-token=s2; Path=/; Secure; HttpOnly"
	if err := c.Auth(context.Background()); err != nil {
		t.Fatal(err)
	}
	if !c.SessionChanged() || c.Session().Cookies[0].Value != "s2" {
		t.Fatalf("cookie not merged: %+v", c.Session().Cookies)
	}
	p := filepath.Join(t.TempDir(), "mfp_session.json")
	if err := c.Session().Save(p); err != nil {
		t.Fatal(err)
	}
	if st, _ := os.Stat(p); st.Mode().Perm() != 0o600 {
		t.Fatalf("mode = %v", st.Mode().Perm())
	}
	back, err := LoadSession(p)
	if err != nil || back.Cookies[0].Value != "s2" {
		t.Fatalf("reload: %+v %v", back, err)
	}
}

func TestRequestsCarryTheSessionUserAgent(t *testing.T) {
	c, srv := newTestClient(t)
	_ = srv
	if c.userAgent() != "test-agent" {
		t.Fatal("user agent not taken from the session")
	}
}

func TestFakeServerEnvAcceptsOnlyLoopback(t *testing.T) {
	for v, want := range map[string]string{
		"http://127.0.0.1:8080":         "http://127.0.0.1:8080",
		"http://localhost:9":            "http://localhost:9",
		"http://127.0.0.1:1@evil.com":   "",
		"https://127.0.0.1:8080":        "",
		"http://127.0.0.1:8080/x":       "",
		"http://example.com:80":         "",
	} {
		t.Setenv("MFP_FAKE_SERVER", v)
		if got := FakeServer(); got != want {
			t.Errorf("%q -> %q, want %q", v, got, want)
		}
	}
}
```

`internal/mfp/session_test.go`:

```go
package mfp

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadSessionMissingOrCorruptIsErrSession(t *testing.T) {
	dir := t.TempDir()
	if _, err := LoadSession(filepath.Join(dir, "none.json")); err == nil {
		t.Fatal("missing file accepted")
	}
	bad := filepath.Join(dir, "bad.json")
	os.WriteFile(bad, []byte("{not json"), 0o600)
	if _, err := LoadSession(bad); err == nil {
		t.Fatal("corrupt file accepted")
	}
	noToken := filepath.Join(dir, "notoken.json")
	os.WriteFile(noToken, []byte(`{"cookies":[{"name":"x","value":"y"}]}`), 0o600)
	if _, err := LoadSession(noToken); err == nil {
		t.Fatal("file without the session token accepted")
	}
}

func TestSaveKeepsTheOldFileWhenTheWriteFails(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "mfp_session.json")
	s := testSession()
	if err := s.Save(p); err != nil {
		t.Fatal(err)
	}
	os.Chmod(dir, 0o500) // the temporary file cannot be created
	defer os.Chmod(dir, 0o700)
	s.Cookies[0].Value = "changed"
	if err := s.Save(p); err == nil {
		t.Fatal("expected a write error")
	}
	back, err := LoadSession(p)
	if err != nil || back.Cookies[0].Value != "s1" {
		t.Fatalf("old file damaged: %+v %v", back, err)
	}
}
```

- [ ] **Step 5: Run them and see them fail.** Run: `cd tauri-app/core && go test ./internal/mfp/` Expected: FAIL, `undefined: Session`, `undefined: NewClient`.

- [ ] **Step 6: Implement the session.** `internal/mfp/session.go`:

```go
package mfp

import (
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"time"
)

const sessionCookie = "__Secure-next-auth.session-token"

// Cookie and Session match the JSON that Rust writes in mfp_session.json.
type Cookie struct {
	Name    string `json:"name"`
	Value   string `json:"value"`
	Domain  string `json:"domain"`
	Path    string `json:"path"`
	Expires int64  `json:"expires"`
}

type Session struct {
	Cookies        []Cookie `json:"cookies"`
	UserAgent      string   `json:"user_agent"`
	SavedAt        string   `json:"saved_at"`
	SessionExpires int64    `json:"session_expires"`
}

func LoadSession(path string) (*Session, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, ErrSession
	}
	var s Session
	if json.Unmarshal(b, &s) != nil {
		return nil, ErrSession
	}
	for _, c := range s.Cookies {
		if c.Name == sessionCookie && c.Value != "" {
			return &s, nil
		}
	}
	return nil, ErrSession
}

// Save writes through a temporary file and a rename, so a killed process never
// leaves a half written session.
func (s *Session) Save(path string) error {
	s.SavedAt = time.Now().UTC().Format(time.RFC3339)
	b, err := json.MarshalIndent(s, "", " ")
	if err != nil {
		return err
	}
	return writeFileAtomic(path, b)
}

// Merge applies Set-Cookie headers from MFP and reports whether anything changed.
func (s *Session) Merge(set []*http.Cookie) bool {
	changed := false
	for _, nc := range set {
		found := false
		for i := range s.Cookies {
			if s.Cookies[i].Name == nc.Name {
				found = true
				if s.Cookies[i].Value != nc.Value {
					s.Cookies[i].Value = nc.Value
					changed = true
				}
				if !nc.Expires.IsZero() {
					s.Cookies[i].Expires = nc.Expires.Unix()
				}
			}
		}
		if !found && nc.Value != "" {
			s.Cookies = append(s.Cookies, Cookie{Name: nc.Name, Value: nc.Value, Domain: ".myfitnesspal.com", Path: "/", Expires: nc.Expires.Unix()})
			changed = true
		}
		if nc.Name == sessionCookie && !nc.Expires.IsZero() {
			s.SessionExpires = nc.Expires.Unix()
		}
	}
	return changed
}

func writeFileAtomic(path string, data []byte) error {
	tmp, err := os.CreateTemp(filepath.Dir(path), ".tmp-*")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if err := tmp.Chmod(0o600); err != nil {
		tmp.Close()
		return err
	}
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Rename(tmp.Name(), path); err != nil {
		return errors.Join(errors.New("could not replace "+filepath.Base(path)), err)
	}
	return nil
}
```

- [ ] **Step 7: Implement the client.** `internal/mfp/client.go`:

```go
package mfp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"strings"
	"time"
)

var (
	ErrSession     = errors.New("the MyFitnessPal session is missing or expired")
	ErrRateLimited = errors.New("MyFitnessPal is rate limiting requests (HTTP 429)")
)

const (
	wwwBase    = "https://www.myfitnesspal.com"
	apiBase    = "https://api.myfitnesspal.com"
	diaryTypes = "food_entry,exercise_entry,steps_aggregate" // an unknown type makes MFP return nothing
)

var loopback = regexp.MustCompile(`^http://(127\.0\.0\.1|localhost):\d+$`)

// FakeServer returns MFP_FAKE_SERVER when it is a loopback URL, else "".
// It is read on every request, so it can never redirect traffic elsewhere.
func FakeServer() string {
	v := os.Getenv("MFP_FAKE_SERVER")
	if loopback.MatchString(v) {
		return v
	}
	return ""
}

type Options struct {
	Gap  time.Duration // pause between requests; 1 s against the real MFP
	Base string        // test server; overrides both hosts
	HTTP *http.Client
}

type Client struct {
	sess     *Session
	opts     Options
	token    string
	userID   string
	changed  bool
	lastCall time.Time
}

func NewClient(s *Session, opts Options) *Client {
	if opts.HTTP == nil {
		opts.HTTP = &http.Client{Timeout: 30 * time.Second}
	}
	return &Client{sess: s, opts: opts}
}

func (c *Client) Session() *Session   { return c.sess }
func (c *Client) SessionChanged() bool { return c.changed }
func (c *Client) userAgent() string    { return c.sess.UserAgent }

func (c *Client) host(real string) string {
	if c.opts.Base != "" {
		return c.opts.Base
	}
	if f := FakeServer(); f != "" {
		return f
	}
	return real
}

func (c *Client) cookieHeader() string {
	parts := make([]string, 0, len(c.sess.Cookies))
	for _, ck := range c.sess.Cookies {
		parts = append(parts, ck.Name+"="+ck.Value)
	}
	return strings.Join(parts, "; ")
}

func (c *Client) do(ctx context.Context, u string, api bool) (*http.Response, []byte, error) {
	if wait := c.opts.Gap - time.Since(c.lastCall); wait > 0 {
		select {
		case <-time.After(wait):
		case <-ctx.Done():
			return nil, nil, ctx.Err()
		}
	}
	c.lastCall = time.Now()
	req, err := http.NewRequestWithContext(ctx, "GET", u, nil)
	if err != nil {
		return nil, nil, err
	}
	req.Header.Set("User-Agent", c.userAgent())
	req.Header.Set("Cookie", c.cookieHeader())
	if api {
		req.Header.Set("Authorization", "Bearer "+c.token)
		req.Header.Set("mfp-client-id", "mfp-main-js")
		req.Header.Set("mfp-user-id", c.userID)
		req.Header.Set("Accept", "application/json")
	}
	resp, err := c.opts.HTTP.Do(req)
	if err != nil {
		return nil, nil, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if c.sess.Merge(resp.Cookies()) {
		c.changed = true
	}
	return resp, body, err
}

// Auth trades the browser session for a bearer token. An HTML answer means the
// session is no longer valid.
func (c *Client) Auth(ctx context.Context) error {
	resp, body, err := c.do(ctx, c.host(wwwBase)+"/user/auth_token?refresh=true", false)
	if err != nil {
		return err
	}
	if resp.StatusCode == 429 {
		return ErrRateLimited
	}
	if !strings.HasPrefix(resp.Header.Get("Content-Type"), "application/json") {
		return ErrSession
	}
	var tok struct {
		AccessToken string `json:"access_token"`
		UserID      any    `json:"user_id"`
	}
	if json.Unmarshal(body, &tok) != nil || tok.AccessToken == "" {
		return ErrSession
	}
	c.token, c.userID = tok.AccessToken, fmt.Sprint(tok.UserID)
	return nil
}

// apiGet calls the JSON API; a 401 refreshes the token once and tries again.
func (c *Client) apiGet(ctx context.Context, u string) ([]byte, error) {
	for attempt := 0; attempt < 2; attempt++ {
		resp, body, err := c.do(ctx, u, true)
		if err != nil {
			return nil, err
		}
		switch {
		case resp.StatusCode == 429:
			return nil, ErrRateLimited
		case resp.StatusCode == 401 && attempt == 0:
			if err := c.Auth(ctx); err != nil {
				return nil, err
			}
			continue
		case resp.StatusCode == 401:
			return nil, ErrSession
		case resp.StatusCode != 200:
			return nil, fmt.Errorf("MyFitnessPal answered HTTP %d", resp.StatusCode)
		}
		return body, nil
	}
	return nil, ErrSession
}

func (c *Client) Diary(ctx context.Context, date string) ([]DiaryItem, error) {
	q := url.Values{"entry_date": {date}, "types": {diaryTypes}}
	body, err := c.apiGet(ctx, c.host(apiBase)+"/v2/diary?"+q.Encode())
	if err != nil {
		return nil, err
	}
	var out struct {
		Items []DiaryItem `json:"items"`
	}
	if err := json.Unmarshal(body, &out); err != nil {
		return nil, fmt.Errorf("diary for %s is not valid JSON", date)
	}
	return out.Items, nil
}

func (c *Client) Goals(ctx context.Context, date string) (Goals, error) {
	body, err := c.apiGet(ctx, c.host(apiBase)+"/v2/nutrient-goals?date="+url.QueryEscape(date))
	if err != nil {
		return Goals{}, err
	}
	var out struct {
		Items []struct {
			Default struct {
				Energy       *Energy  `json:"energy"`
				Protein      *float64 `json:"protein"`
				Carbohydrate *float64 `json:"carbohydrates"`
				Fat          *float64 `json:"fat"`
				Sodium       *float64 `json:"sodium"`
				Sugar        *float64 `json:"sugar"`
			} `json:"default_goal"`
		} `json:"items"`
	}
	if json.Unmarshal(body, &out) != nil || len(out.Items) == 0 {
		return Goals{}, nil
	}
	d := out.Items[0].Default
	return Goals{EnergyKcal: d.Energy.Kcal(), Protein: d.Protein, Carbohydrate: d.Carbohydrate, Fat: d.Fat, Sodium: d.Sodium, Sugar: d.Sugar}, nil
}
```

- [ ] **Step 8: Run the tests.** Run: `cd tauri-app/core && go vet ./... && go test ./internal/mfp/` Expected: PASS. Note: `TestSaveKeepsTheOldFileWhenTheWriteFails` must not run as root.

- [ ] **Step 9: Commit.**

```bash
git add tauri-app/core/internal/mfp
git commit -m "feat(core): MyFitnessPal session file and API client with a fake server"
```

---

### Task 3: Per day cache with the final rule

**Files:**
- Create: `internal/mfp/cache.go`, `internal/mfp/cache_test.go`

**Interfaces:**
- Consumes: `DiaryItem`, `writeFileAtomic` (Task 2).
- Produces: `mfp.Cache{Dir string}`, `(Cache).Get(date string, now time.Time) (items []DiaryItem, final bool, ok bool)`, `(Cache).Put(date string, items []DiaryItem, now time.Time) error`, `mfp.HasFood(items []DiaryItem) bool`.

- [ ] **Step 1: Write the failing test.**

```go
package mfp

import (
	"testing"
	"time"
)

func TestCacheFinalRule(t *testing.T) {
	c := Cache{Dir: t.TempDir()}
	food := []DiaryItem{{Type: "food_entry"}}
	exerciseOnly := []DiaryItem{{Type: "exercise_entry"}}
	at := func(s string) time.Time { v, _ := time.Parse(time.RFC3339, s); return v }

	_ = c.Put("2026-10-01", food, at("2026-10-07T23:59:00Z")) // 6.99 days after
	if _, final, ok := c.Get("2026-10-01", at("2026-10-20T00:00:00Z")); !ok || final {
		t.Fatal("food fetched 6.99 days after must not be final")
	}
	_ = c.Put("2026-10-01", food, at("2026-10-08T00:00:00Z")) // 7 days after
	if _, final, _ := c.Get("2026-10-01", at("2026-10-20T00:00:00Z")); !final {
		t.Fatal("food fetched 7 days after must be final")
	}
	_ = c.Put("2026-09-01", exerciseOnly, at("2026-10-01T00:00:00Z")) // 30 days after, no food
	if _, final, _ := c.Get("2026-09-01", at("2026-10-20T00:00:00Z")); final {
		t.Fatal("a day without food is never final: the user may fill it in later")
	}
	today := time.Now().UTC().Format("2006-01-02")
	_ = c.Put(today, food, time.Now())
	if _, final, _ := c.Get(today, time.Now()); final {
		t.Fatal("today is never final")
	}
	if _, _, ok := c.Get("2020-01-01", time.Now()); ok {
		t.Fatal("missing day reported as cached")
	}
}
```

- [ ] **Step 2: Run it.** Run: `go test ./internal/mfp/ -run TestCacheFinalRule` Expected: FAIL, `undefined: Cache`.

- [ ] **Step 3: Implement.** `internal/mfp/cache.go`:

```go
package mfp

import (
	"encoding/json"
	"os"
	"path/filepath"
	"time"
)

// FinalAfterDays: a day with food is final once it was fetched this many days after
// its date. Days without food are always fetched again, because the user may log late.
const FinalAfterDays = 7

type Cache struct{ Dir string }

type cachedDay struct {
	FetchedAt string      `json:"fetched_at"`
	Items     []DiaryItem `json:"items"`
}

func HasFood(items []DiaryItem) bool {
	for _, it := range items {
		if it.Type == "food_entry" {
			return true
		}
	}
	return false
}

func (c Cache) path(date string) string { return filepath.Join(c.Dir, date+".json") }

func (c Cache) Get(date string, now time.Time) (items []DiaryItem, final bool, ok bool) {
	b, err := os.ReadFile(c.path(date))
	if err != nil {
		return nil, false, false
	}
	var d cachedDay
	if json.Unmarshal(b, &d) != nil {
		return nil, false, false
	}
	day, err1 := time.Parse("2006-01-02", date)
	fetched, err2 := time.Parse(time.RFC3339, d.FetchedAt)
	final = err1 == nil && err2 == nil && HasFood(d.Items) &&
		fetched.Sub(day) >= FinalAfterDays*24*time.Hour
	return d.Items, final, true
}

func (c Cache) Put(date string, items []DiaryItem, now time.Time) error {
	if err := os.MkdirAll(c.Dir, 0o700); err != nil {
		return err
	}
	if items == nil {
		items = []DiaryItem{}
	}
	b, err := json.Marshal(cachedDay{FetchedAt: now.UTC().Format(time.RFC3339), Items: items})
	if err != nil {
		return err
	}
	return writeFileAtomic(c.path(date), b)
}
```

- [ ] **Step 4: Run the tests.** Run: `go test ./internal/mfp/` Expected: PASS.

- [ ] **Step 5: Commit.**

```bash
git add tauri-app/core/internal/mfp/cache.go tauri-app/core/internal/mfp/cache_test.go
git commit -m "feat(core): per day MyFitnessPal cache; only days with food become final"
```

---

### Task 4: CSV files

**Files:**
- Create: `internal/mfp/csv.go`, `internal/mfp/csv_test.go`, `internal/mfp/testdata/golden/{mfp_food,mfp_daily,mfp_exercise}.csv` (generated in Step 4)

**Interfaces:**
- Consumes: `DiaryItem`, `Goals`, `Energy.Kcal` (Task 2).
- Produces: `mfp.DayData{Date string; Items []DiaryItem}`, `mfp.WriteFiles(dir string, days []DayData, goals Goals) (logged int, err error)`.

- [ ] **Step 1: Write the failing golden test.** Column names for calcium, iron and vitamin D follow the Task 0 result; the code below uses the percent version, so change the three header names if the spike found milligrams.

```go
package mfp

import (
	"bytes"
	"encoding/csv"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func loadFixtureDays(t *testing.T) []DayData {
	var days []DayData
	for _, d := range []string{"2026-10-04", "2026-10-05", "2026-10-06", "2026-10-07"} {
		var out struct{ Items []DiaryItem }
		if b, err := os.ReadFile(filepath.Join("testdata", "diary", d+".json")); err == nil {
			if err := json.Unmarshal(b, &out); err != nil {
				t.Fatal(err)
			}
		}
		days = append(days, DayData{Date: d, Items: out.Items})
	}
	return days
}

func fixtureGoals() Goals {
	f := func(v float64) *float64 { return &v }
	return Goals{EnergyKcal: f(2200), Protein: f(150), Carbohydrate: f(220), Fat: f(73), Sodium: f(2300), Sugar: f(50)}
}

func TestWriteFilesMatchesGolden(t *testing.T) {
	dir := t.TempDir()
	logged, err := WriteFiles(dir, loadFixtureDays(t), fixtureGoals())
	if err != nil {
		t.Fatal(err)
	}
	if logged != 2 {
		t.Fatalf("logged = %d, want 2 (Oct 5 and Oct 7)", logged)
	}
	for _, f := range []string{"mfp_food.csv", "mfp_daily.csv", "mfp_exercise.csv"} {
		got, _ := os.ReadFile(filepath.Join(dir, f))
		golden := filepath.Join("testdata", "golden", f)
		if os.Getenv("UPDATE_GOLDEN") != "" {
			os.MkdirAll(filepath.Dir(golden), 0o755)
			os.WriteFile(golden, got, 0o644)
		}
		want, _ := os.ReadFile(golden)
		if !bytes.Equal(got, want) {
			t.Errorf("%s differs from the golden file:\n%s", f, got)
		}
	}
}

func TestCSVRules(t *testing.T) {
	dir := t.TempDir()
	if _, err := WriteFiles(dir, loadFixtureDays(t), fixtureGoals()); err != nil {
		t.Fatal(err)
	}
	read := func(f string) []map[string]string {
		fh, _ := os.Open(filepath.Join(dir, f))
		defer fh.Close()
		rows, err := csv.NewReader(fh).ReadAll()
		if err != nil {
			t.Fatalf("%s is not valid CSV: %v", f, err)
		}
		var out []map[string]string
		for _, r := range rows[1:] {
			m := map[string]string{}
			for i, h := range rows[0] {
				m[h] = r[i]
			}
			out = append(out, m)
		}
		return out
	}
	food := read("mfp_food.csv")
	if food[0]["description"] != `Pan "Bimbo", integral` || food[1]["description"] != "Arroz con pollo\nde la casa" {
		t.Errorf("quotes, commas or line breaks lost: %q %q", food[0]["description"], food[1]["description"])
	}
	if food[1]["added_sugars_g"] != "" || food[1]["potassium_mg"] != "" {
		t.Error("a null nutrient must be an empty cell, not 0")
	}
	if food[2]["energy_kcal"] != "104.9" {
		t.Errorf("439 kJ must be 104.9 kcal, got %q", food[2]["energy_kcal"])
	}
	daily := read("mfp_daily.csv")
	if len(daily) != 4 {
		t.Fatalf("mfp_daily.csv must have one row per date in the range, got %d", len(daily))
	}
	byDate := map[string]map[string]string{}
	for _, d := range daily {
		byDate[d["date"]] = d
	}
	if d := byDate["2026-10-04"]; d["logged"] != "no" || d["energy_kcal"] != "" || d["goal_energy_kcal"] != "2200" {
		t.Errorf("a day without a log must have empty totals and still show goals: %v", d)
	}
	if d := byDate["2026-10-06"]; d["logged"] != "no" || d["exercise_kcal_mfp"] != "210" {
		t.Errorf("exercise only day: %v", d)
	}
	if d := byDate["2026-10-05"]; d["logged"] != "yes" || d["meals_logged"] != "2" || d["foods_logged"] != "2" || d["energy_kcal"] != "755" {
		t.Errorf("logged day totals: %v", d)
	}
	if ex := read("mfp_exercise.csv"); len(ex) != 2 || ex[0]["duration_min"] != "45" {
		t.Errorf("exercise rows: %v", ex)
	}
}
```

- [ ] **Step 2: Run it.** Run: `go test ./internal/mfp/ -run 'TestWriteFiles|TestCSVRules'` Expected: FAIL, `undefined: WriteFiles`.

- [ ] **Step 3: Implement.** `internal/mfp/csv.go`:

```go
package mfp

import (
	"encoding/csv"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strconv"
)

type DayData struct {
	Date  string
	Items []DiaryItem
}

var foodHeader = []string{"date", "meal", "meal_position", "consumed_at", "logged_at", "description", "brand", "verified",
	"servings", "serving_size", "serving_unit", "grams", "energy_kcal", "protein_g", "carbohydrates_g", "fiber_g", "sugar_g",
	"added_sugars_g", "fat_g", "saturated_fat_g", "trans_fat_g", "cholesterol_mg", "sodium_mg", "potassium_mg",
	"calcium_pct_dv", "iron_pct_dv", "vitamin_d_pct_dv"}

var dailyHeader = []string{"date", "logged", "meals_logged", "foods_logged", "energy_kcal", "protein_g", "carbohydrates_g",
	"fat_g", "fiber_g", "sugar_g", "sodium_mg", "goal_energy_kcal", "goal_protein_g", "goal_carbohydrates_g", "goal_fat_g",
	"goal_sodium_mg", "goal_sugar_g", "exercise_kcal_mfp"}

var exerciseHeader = []string{"date", "name", "type", "duration_min", "energy_kcal", "source"}

// num formats a value with at most one decimal, and nil as an empty cell.
func num(v *float64) string {
	if v == nil {
		return ""
	}
	return strconv.FormatFloat(math.Round(*v*10)/10, 'f', -1, 64)
}

func f(v float64) *float64 { return &v }

func writeCSV(path string, header []string, rows [][]string) error {
	fh, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o644)
	if err != nil {
		return err
	}
	w := csv.NewWriter(fh)
	w.Write(header)
	w.WriteAll(rows)
	if err := w.Error(); err != nil {
		fh.Close()
		return err
	}
	return fh.Close()
}

// sum adds the values that are present; it stays nil when every value is nil.
func sum(acc **float64, v *float64) {
	if v == nil {
		return
	}
	if *acc == nil {
		*acc = f(0)
	}
	**acc += *v
}

// WriteFiles writes the three MFP CSV files and returns the number of days with food.
func WriteFiles(dir string, days []DayData, g Goals) (int, error) {
	sort.Slice(days, func(i, j int) bool { return days[i].Date < days[j].Date })
	var food, daily, exercise [][]string
	logged := 0
	for _, d := range days {
		var kcal, protein, carbs, fat, fiber, sugar, sodium, exKcal *float64
		meals := map[string]bool{}
		foods := 0
		for _, it := range d.Items {
			switch it.Type {
			case "food_entry":
				n := it.Nutrients
				foods++
				meals[it.MealName] = true
				food = append(food, []string{d.Date, it.MealName, strconv.Itoa(it.MealPosition), it.ConsumedAt, it.LoggedAt,
					it.Food.Description, it.Food.BrandName, strconv.FormatBool(it.Food.Verified),
					num(f(it.Servings)), num(f(it.ServingSize.Value)), it.ServingSize.Unit, num(f(it.ServingSize.GramWeight*it.Servings)),
					num(n.Energy.Kcal()), num(n.Protein), num(n.Carbohydrate), num(n.Fiber), num(n.Sugar), num(n.AddedSugars),
					num(n.Fat), num(n.SaturatedFat), num(n.TransFat), num(n.Cholesterol), num(n.Sodium), num(n.Potassium),
					num(n.Calcium), num(n.Iron), num(n.VitaminD)})
				sum(&kcal, n.Energy.Kcal())
				sum(&protein, n.Protein)
				sum(&carbs, n.Carbohydrate)
				sum(&fat, n.Fat)
				sum(&fiber, n.Fiber)
				sum(&sugar, n.Sugar)
				sum(&sodium, n.Sodium)
			case "exercise_entry":
				exercise = append(exercise, []string{d.Date, it.Exercise.Description, it.Exercise.Type,
					num(f(it.DurationSec / 60)), num(it.Energy.Kcal()), it.Source})
				sum(&exKcal, it.Energy.Kcal())
			}
		}
		yes := "no"
		if foods > 0 {
			yes = "yes"
			logged++
		}
		daily = append(daily, []string{d.Date, yes, strconv.Itoa(len(meals)), strconv.Itoa(foods),
			num(kcal), num(protein), num(carbs), num(fat), num(fiber), num(sugar), num(sodium),
			num(g.EnergyKcal), num(g.Protein), num(g.Carbohydrate), num(g.Fat), num(g.Sodium), num(g.Sugar), num(exKcal)})
	}
	for name, part := range map[string]struct {
		h    []string
		rows [][]string
	}{"mfp_food.csv": {foodHeader, food}, "mfp_daily.csv": {dailyHeader, daily}, "mfp_exercise.csv": {exerciseHeader, exercise}} {
		if err := writeCSV(filepath.Join(dir, name), part.h, part.rows); err != nil {
			return logged, err
		}
	}
	return logged, nil
}
```

- [ ] **Step 4: Create the golden files and check them by eye.** Run: `UPDATE_GOLDEN=1 go test ./internal/mfp/ -run TestWriteFilesMatchesGolden && cat internal/mfp/testdata/golden/*.csv`. Check that the breakfast row shows 140 kcal and 56 grams, that the Oct 4 daily row has empty totals, and that the kJ banana shows 104.9 kcal.

- [ ] **Step 5: Run all tests.** Run: `go test ./internal/mfp/` Expected: PASS.

- [ ] **Step 6: Commit.**

```bash
git add tauri-app/core/internal/mfp/csv.go tauri-app/core/internal/mfp/csv_test.go tauri-app/core/internal/mfp/testdata
git commit -m "feat(core): write mfp_food, mfp_daily and mfp_exercise CSV files"
```

---

### Task 5: The mfp-export command

**Files:**
- Create: `internal/mfp/export.go`, `internal/mfp/export_test.go`, `cmd/fake-mfp/main.go`
- Modify: `cmd/garmin-export-core/main.go`

**Interfaces:**
- Consumes: everything from Tasks 1 to 4.
- Produces: `mfp.Export(ctx context.Context, req map[string]any, out *protocol.Emitter, opts Options) map[string]any`. Note for `TestExportWritesFilesAndUsesTheCache`: it relies on the real clock being at least 7 days after 2026-10-07, which holds from 2026-10-14 on, so the fixture days with food are final on the second run. The result map is `{ok: true, days, loggedDays, files: [...], csvDir}` or `{ok: false, error, errorCode}` with `errorCode` one of `MFP_SESSION`, `MFP_RATE_LIMITED`, `MFP_INPUT`, `MFP_ERROR`. The request is `{cmd: "mfp-export", startDate, endDate, dataDir, csvDir}`.

- [ ] **Step 1: Write the failing tests.** `internal/mfp/export_test.go`:

```go
package mfp

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/danihrndzld/garmin-health-exporter/tauri-app/core/internal/mfp/mfptest"
	"github.com/danihrndzld/garmin-health-exporter/tauri-app/core/internal/protocol"
)

func homeDirs(t *testing.T) (dataDir, csvDir string) {
	home, _ := os.UserHomeDir()
	root, err := os.MkdirTemp(home, ".mfp-export-test-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(root) })
	dataDir, csvDir = filepath.Join(root, "data"), filepath.Join(root, "csv_2026-10-07")
	os.MkdirAll(dataDir, 0o700)
	os.MkdirAll(csvDir, 0o755)
	return
}

func run(t *testing.T, srv *mfptest.Server, req map[string]any) map[string]any {
	var buf bytes.Buffer
	return Export(context.Background(), req, &protocol.Emitter{Out: &buf}, Options{Base: srv.URL})
}

func TestExportWritesFilesAndUsesTheCache(t *testing.T) {
	srv := mfptest.NewServer("testdata")
	defer srv.Close()
	dataDir, csvDir := homeDirs(t)
	testSession().Save(filepath.Join(dataDir, "mfp_session.json"))
	req := map[string]any{"cmd": "mfp-export", "startDate": "2026-10-04", "endDate": "2026-10-07", "dataDir": dataDir, "csvDir": csvDir}
	res := run(t, srv, req)
	if res["ok"] != true || res["loggedDays"] != 2 || res["days"] != 4 {
		t.Fatalf("result = %v", res)
	}
	for _, f := range []string{"mfp_food.csv", "mfp_daily.csv", "mfp_exercise.csv"} {
		if _, err := os.Stat(filepath.Join(csvDir, f)); err != nil {
			t.Errorf("%s missing", f)
		}
	}
	// Second run: Oct 5 and Oct 7 have food and were fetched long after their date
	// (the test clock is real time), so they come from the cache; the empty days do not.
	before := srv.CallCount("/v2/diary?")
	run(t, srv, req)
	if got := srv.CallCount("/v2/diary?") - before; got != 2 {
		t.Fatalf("second run fetched %d days, want 2 (the days without food)", got)
	}
}

func TestExportErrors(t *testing.T) {
	srv := mfptest.NewServer("testdata")
	defer srv.Close()
	dataDir, csvDir := homeDirs(t)
	base := map[string]any{"cmd": "mfp-export", "startDate": "2026-10-04", "endDate": "2026-10-07", "dataDir": dataDir, "csvDir": csvDir}

	res := run(t, srv, base) // no session file yet
	if res["ok"] != false || res["errorCode"] != "MFP_SESSION" || res["error"] != "Connect MyFitnessPal first." {
		t.Fatalf("missing session: %v", res)
	}
	os.WriteFile(filepath.Join(dataDir, "mfp_session.json"), []byte("{broken"), 0o600)
	if res := run(t, srv, base); res["errorCode"] != "MFP_SESSION" {
		t.Fatalf("corrupt session: %v", res)
	}

	testSession().Save(filepath.Join(dataDir, "mfp_session.json"))
	srv.AuthHTML = true
	if res := run(t, srv, base); res["errorCode"] != "MFP_SESSION" || res["error"] != "The MyFitnessPal session expired. Reconnect MyFitnessPal." {
		t.Fatalf("expired: %v", res)
	}
	srv.AuthHTML = false
	srv.Diary429 = true
	if res := run(t, srv, base); res["errorCode"] != "MFP_RATE_LIMITED" {
		t.Fatalf("429: %v", res)
	}
	srv.Diary429 = false

	for _, bad := range []map[string]any{
		{"startDate": "2026-01-01", "endDate": "2026-10-07"},
		{"csvDir": "/tmp"},
		{"dataDir": "/etc"},
	} {
		req := map[string]any{}
		for k, v := range base {
			req[k] = v
		}
		for k, v := range bad {
			req[k] = v
		}
		if res := run(t, srv, req); res["errorCode"] != "MFP_INPUT" {
			t.Errorf("%v accepted: %v", bad, res)
		}
	}
}
```

- [ ] **Step 2: Run them.** Run: `go test ./internal/mfp/ -run TestExport` Expected: FAIL, `undefined: Export`.

- [ ] **Step 3: Implement.** `internal/mfp/export.go`:

```go
package mfp

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"time"

	"github.com/danihrndzld/garmin-health-exporter/tauri-app/core/internal/guard"
	"github.com/danihrndzld/garmin-health-exporter/tauri-app/core/internal/protocol"
)

func fail(code, msg string) map[string]any {
	return map[string]any{"ok": false, "errorCode": code, "error": msg}
}

// Export runs the mfp-export command. It never touches garmin_cache.db.
func Export(ctx context.Context, req map[string]any, out *protocol.Emitter, opts Options) map[string]any {
	str := func(k string) string { s, _ := req[k].(string); return s }
	start, end := str("startDate"), str("endDate")
	if err := guard.ValidateRange(start, end, 90); err != nil {
		return fail("MFP_INPUT", err.Error())
	}
	dataDir, err := guard.InsideHome(str("dataDir"))
	if err != nil {
		return fail("MFP_INPUT", "dataDir: "+err.Error())
	}
	csvDir, err := guard.InsideHome(str("csvDir"))
	if err != nil {
		return fail("MFP_INPUT", "csvDir: "+err.Error())
	}

	sessPath := filepath.Join(dataDir, "mfp_session.json")
	sess, err := LoadSession(sessPath)
	if err != nil {
		return fail("MFP_SESSION", "Connect MyFitnessPal first.")
	}
	client := NewClient(sess, opts)
	// Save rotated cookies even when the run stops early.
	defer func() {
		if client.SessionChanged() {
			_ = client.Session().Save(sessPath)
		}
	}()

	mapErr := func(err error) map[string]any {
		switch {
		case errors.Is(err, ErrSession):
			return fail("MFP_SESSION", "The MyFitnessPal session expired. Reconnect MyFitnessPal.")
		case errors.Is(err, ErrRateLimited):
			return fail("MFP_RATE_LIMITED", "MyFitnessPal is rate limiting requests (HTTP 429). The days downloaded so far are cached; try again later.")
		default:
			return fail("MFP_ERROR", err.Error())
		}
	}

	out.Log("dim", "MyFitnessPal: signing in with the saved session")
	if err := client.Auth(ctx); err != nil {
		return mapErr(err)
	}
	goals, err := client.Goals(ctx, end)
	if err != nil {
		return mapErr(err)
	}

	cache := Cache{Dir: filepath.Join(dataDir, "mfp-cache")}
	dates := guard.Dates(start, end)
	days := make([]DayData, 0, len(dates))
	fetched := 0
	for i, d := range dates {
		items, final, ok := cache.Get(d, time.Now())
		if !ok || !final {
			items, err = client.Diary(ctx, d)
			if err != nil {
				return mapErr(err)
			}
			fetched++
			if err := cache.Put(d, items, time.Now()); err != nil {
				out.Log("warn", "MyFitnessPal cache write failed for "+d+": "+err.Error())
			}
		}
		days = append(days, DayData{Date: d, Items: items})
		out.Progress("mfp", i+1, len(dates))
	}

	logged, err := WriteFiles(csvDir, days, goals)
	if err != nil {
		return fail("MFP_ERROR", "Could not write the MyFitnessPal CSV files: "+err.Error())
	}
	out.Log("success", fmt.Sprintf("MyFitnessPal: food logged on %d of %d days (%d downloaded, %d from the cache)", logged, len(dates), fetched, len(dates)-fetched))
	return map[string]any{"ok": true, "days": len(dates), "loggedDays": logged, "csvDir": csvDir,
		"files": []string{"mfp_food.csv", "mfp_daily.csv", "mfp_exercise.csv"}}
}
```

- [ ] **Step 4: Wire the command.** In `cmd/garmin-export-core/main.go`, add the import `"context"`, `"time"` and `.../internal/mfp`, and replace the `switch` with:

```go
	switch req["cmd"] {
	case "mfp-export":
		opts := mfp.Options{Gap: time.Second}
		if mfp.FakeServer() != "" {
			opts.Gap = 0 // only the loopback test server may skip the pause
		}
		out.Result(mfp.Export(context.Background(), req, out, opts))
	default:
		out.Result(map[string]any{"ok": false, "error": "Unknown command."})
	}
```

- [ ] **Step 5: Add the fake server command** used by the bun end to end test. `cmd/fake-mfp/main.go`:

```go
// fake-mfp serves the test fixtures on a free loopback port and prints its URL.
// Test helper only; it is never bundled with the app.
package main

import (
	"fmt"
	"os"

	"github.com/danihrndzld/garmin-health-exporter/tauri-app/core/internal/mfp/mfptest"
)

func main() {
	if len(os.Args) < 2 {
		fmt.Fprintln(os.Stderr, "usage: fake-mfp <testdata dir>")
		os.Exit(2)
	}
	srv := mfptest.NewServer(os.Args[1])
	fmt.Println(srv.URL)
	select {}
}
```

- [ ] **Step 6: Run all Go tests.** Run: `cd tauri-app/core && go vet ./... && go test ./...` Expected: PASS.

- [ ] **Step 7: Commit.**

```bash
git add tauri-app/core
git commit -m "feat(core): mfp-export command with clear errors for session, rate limit and input"
```

---

### Task 6: Build, bundle and the Bun side

**Files:**
- Modify: `tauri-app/package.json`, `tauri-app/src-tauri/tauri.conf.json`, `tauri-app/src-tauri/capabilities/default.json`, `tauri-app/sidecar/cli.js:125-131`, `tauri-app/.gitignore`
- Create: `tauri-app/sidecar/mfp-e2e.test.js`

**Interfaces:**
- Consumes: the `garmin-export-core` binary and `cmd/fake-mfp` (Task 5).
- Produces: `binaries/garmin-export-core-<triple>` in dev and release builds; the sidecar name `binaries/garmin-export-core` for the UI.

- [ ] **Step 1: Write the failing end to end test.** `tauri-app/sidecar/mfp-e2e.test.js`:

```js
// End to end: the Go binary as the app runs it (one JSON line on stdin), against the
// fake MFP server from cmd/fake-mfp. The CSV files must equal the Go golden files.
import { test, expect, beforeAll, afterAll } from 'bun:test';
import fs from 'node:fs';
import os from 'node:os';
import path from 'node:path';

const CORE = path.resolve(import.meta.dir, '../core');
const TESTDATA = path.join(CORE, 'internal/mfp/testdata');
const BIN = path.join(os.tmpdir(), `garmin-export-core-e2e-${process.pid}`);
let fake, base;
const cleanup = [];

beforeAll(async () => {
  const build = Bun.spawnSync(['go', 'build', '-o', BIN, './cmd/garmin-export-core'], { cwd: CORE });
  if (build.exitCode !== 0) throw new Error(String(build.stderr));
  fake = Bun.spawn(['go', 'run', './cmd/fake-mfp', TESTDATA], { cwd: CORE, stdout: 'pipe' });
  const reader = fake.stdout.getReader();
  base = new TextDecoder().decode((await reader.read()).value).trim();
});
afterAll(() => { fake?.kill(); fs.rmSync(BIN, { force: true }); for (const d of cleanup) fs.rmSync(d, { recursive: true, force: true }); });

async function core(req) {
  const p = Bun.spawn([BIN], { stdin: 'pipe', stdout: 'pipe', env: { ...process.env, MFP_FAKE_SERVER: base } });
  p.stdin.write(JSON.stringify(req) + '\n');
  p.stdin.end();
  const lines = (await new Response(p.stdout).text()).trim().split('\n').map((l) => JSON.parse(l));
  return { result: lines.find((l) => l.kind === 'result'), progress: lines.filter((l) => l.kind === 'progress') };
}

function homeDirs() {
  const root = fs.mkdtempSync(path.join(os.homedir(), '.mfp-e2e-'));
  cleanup.push(root);
  const dataDir = path.join(root, 'data'), csvDir = path.join(root, 'csv_2026-10-07');
  fs.mkdirSync(dataDir); fs.mkdirSync(csvDir);
  return { dataDir, csvDir };
}

test('AC-08 mfp-export through the Go binary writes CSV files equal to the golden files', async () => {
  const { dataDir, csvDir } = homeDirs();
  fs.writeFileSync(path.join(dataDir, 'mfp_session.json'), JSON.stringify({ user_agent: 'e2e',
    cookies: [{ name: '__Secure-next-auth.session-token', value: 's1', domain: '.myfitnesspal.com', path: '/' }] }), { mode: 0o600 });
  const { result, progress } = await core({ cmd: 'mfp-export', startDate: '2026-10-04', endDate: '2026-10-07', dataDir, csvDir });
  expect(result.ok).toBe(true);
  expect(result.loggedDays).toBe(2);
  expect(progress.at(-1)).toMatchObject({ phase: 'mfp', current: 4, total: 4 });
  for (const f of ['mfp_food.csv', 'mfp_daily.csv', 'mfp_exercise.csv']) {
    expect(fs.readFileSync(path.join(csvDir, f)).equals(fs.readFileSync(path.join(TESTDATA, 'golden', f)))).toBe(true);
  }
}, 60000);

test('AC-09 without a session the binary asks to connect MyFitnessPal', async () => {
  const { dataDir, csvDir } = homeDirs();
  const { result } = await core({ cmd: 'mfp-export', startDate: '2026-10-04', endDate: '2026-10-07', dataDir, csvDir });
  expect(result).toMatchObject({ ok: false, errorCode: 'MFP_SESSION', error: 'Connect MyFitnessPal first.' });
}, 60000);
```

- [ ] **Step 2: Run it.** Run: `cd tauri-app && bun test sidecar/mfp-e2e.test.js` Expected: PASS once Task 5 is done (this test checks the binary as the app runs it). If it fails, fix the binary, not the test.

- [ ] **Step 3: Build scripts.** In `tauri-app/package.json`, replace the `sidecar` and `sidecar:universal` scripts and add `core`:

```json
"core": "cd core && CGO_ENABLED=0 go build -trimpath -ldflags '-s -w' -o ../src-tauri/binaries/garmin-export-core-$(rustc -vV | awk '/host/{print $2}') ./cmd/garmin-export-core",
"core:universal": "cd core && for a in arm64:aarch64 amd64:x86_64; do CGO_ENABLED=0 GOOS=darwin GOARCH=${a%%:*} go build -trimpath -ldflags '-s -w' -o ../src-tauri/binaries/garmin-export-core-${a##*:}-apple-darwin ./cmd/garmin-export-core || exit 1; done && lipo -create -output ../src-tauri/binaries/garmin-export-core-universal-apple-darwin ../src-tauri/binaries/garmin-export-core-aarch64-apple-darwin ../src-tauri/binaries/garmin-export-core-x86_64-apple-darwin",
"sidecar": "cp ../electron-app/renderer/date-range.js ui/ && bun build --compile sidecar/cli.js --outfile src-tauri/binaries/garmin-core-$(rustc -vV | awk '/host/{print $2}') && bun run core",
```

In `sidecar:universal`, append `&& bun run core:universal` at the end. Also add `"test:core": "cd core && go vet ./... && go test ./..."`.

- [ ] **Step 4: Bundle the binary.** In `tauri.conf.json`, change `"externalBin": ["binaries/garmin-core"]` to `"externalBin": ["binaries/garmin-core", "binaries/garmin-export-core"]`. In `capabilities/default.json`, add a second entry to the `shell:allow-spawn` `allow` list:

```json
{ "name": "binaries/garmin-export-core", "sidecar": true }
```

- [ ] **Step 5: Clear cache also deletes the MFP cache.** In `tauri-app/sidecar/cli.js`, inside `clearCache` after `cache.close();`, add:

```js
  // The Go core keeps MyFitnessPal days as JSON files next to the Garmin cache.
  fs.rmSync(path.join(resolveDataDir(req.dataDir), 'mfp-cache'), { recursive: true, force: true });
```

- [ ] **Step 6: Build and check.** Run: `cd tauri-app && bun run core && ls -la src-tauri/binaries/ && bun test sidecar && bun run test:core`. Expected: `garmin-export-core-aarch64-apple-darwin` exists, about 7 MB, and every test passes.

- [ ] **Step 7: Commit.**

```bash
git add tauri-app/package.json tauri-app/src-tauri/tauri.conf.json tauri-app/src-tauri/capabilities/default.json tauri-app/sidecar/cli.js tauri-app/sidecar/mfp-e2e.test.js
git commit -m "build: bundle the Go core next to the Bun sidecar; clear cache removes MFP days"
```

---

### Task 7: Rust login window and session commands

**Files:**
- Create: `tauri-app/src-tauri/src/mfp.rs`
- Modify: `tauri-app/src-tauri/src/main.rs`, `tauri-app/src-tauri/Cargo.toml`

**Interfaces:**
- Produces: Tauri commands `mfp_connect() -> Result<(), String>`, `mfp_disconnect() -> Result<(), String>`, `mfp_status() -> MfpStatus { connected: bool, saved_at: Option<String>, session_expires: Option<i64> }`; the event `mfp-connected` with payload `MfpStatus`; the file `<app_data_dir>/mfp_session.json` in the JSON shape of `mfp.Session` (Task 2).

- [ ] **Step 1: Add the serde dependencies.** In `Cargo.toml` under `[dependencies]`:

```toml
serde = { version = "1", features = ["derive"] }
serde_json = "1"
```

- [ ] **Step 2: Write the module with a unit test for the cookie filter.** `src-tauri/src/mfp.rs`:

```rust
//! MyFitnessPal sign in. The user signs in inside an app window; we copy the session
//! cookies to mfp_session.json (mode 0600) for the Go core. The password never
//! reaches Rust or the UI, and the login window has no IPC permissions.
use serde::Serialize;
use std::{fs, io::Write, os::unix::fs::OpenOptionsExt, path::PathBuf};
use tauri::{
    webview::{Cookie, PageLoadEvent},
    AppHandle, Emitter, Manager, WebviewUrl, WebviewWindowBuilder,
};

const LOGIN_URL: &str = "https://www.myfitnesspal.com/account/login";
const SESSION_COOKIE: &str = "__Secure-next-auth.session-token";
// A full Safari string; the Go core sends the same one, because Cloudflare ties
// cf_clearance to the User-Agent.
const USER_AGENT: &str = "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/18.6 Safari/605.1.15";

#[derive(Serialize, Clone)]
struct SavedCookie {
    name: String,
    value: String,
    domain: String,
    path: String,
    expires: i64,
}

#[derive(Serialize, Clone)]
pub struct MfpStatus {
    connected: bool,
    saved_at: Option<String>,
    session_expires: Option<i64>,
}

fn session_path(app: &AppHandle) -> Result<PathBuf, String> {
    let dir = app.path().app_data_dir().map_err(|e| e.to_string())?;
    fs::create_dir_all(&dir).map_err(|e| e.to_string())?;
    Ok(dir.join("mfp_session.json"))
}

/// Keep cookies for myfitnesspal.com and its subdomains. Filtering by suffix matters:
/// cookies_for_url drops cookies set on ".myfitnesspal.com" (cf_clearance).
fn mfp_cookies(all: Vec<Cookie<'static>>) -> Vec<SavedCookie> {
    all.into_iter()
        .filter(|c| {
            let d = c.domain().unwrap_or("").trim_start_matches('.');
            d == "myfitnesspal.com" || d.ends_with(".myfitnesspal.com")
        })
        .map(|c| SavedCookie {
            name: c.name().to_string(),
            value: c.value().to_string(),
            domain: c.domain().unwrap_or("").to_string(),
            path: c.path().unwrap_or("/").to_string(),
            expires: c.expires_datetime().map(|t| t.unix_timestamp()).unwrap_or(0),
        })
        .collect()
}

fn write_private(path: &PathBuf, data: &[u8]) -> std::io::Result<()> {
    let tmp = path.with_extension("json.tmp");
    let mut f = fs::OpenOptions::new().write(true).create(true).truncate(true).mode(0o600).open(&tmp)?;
    f.write_all(data)?;
    f.sync_all()?;
    fs::rename(tmp, path)
}

fn status_from_file(path: &PathBuf) -> MfpStatus {
    let v: Option<serde_json::Value> = fs::read(path).ok().and_then(|b| serde_json::from_slice(&b).ok());
    match v {
        Some(v) => MfpStatus {
            connected: true,
            saved_at: v["saved_at"].as_str().map(String::from),
            session_expires: v["session_expires"].as_i64(),
        },
        None => MfpStatus { connected: false, saved_at: None, session_expires: None },
    }
}

#[tauri::command]
pub fn mfp_status(app: AppHandle) -> Result<MfpStatus, String> {
    Ok(status_from_file(&session_path(&app)?))
}

#[tauri::command]
pub async fn mfp_connect(app: AppHandle) -> Result<(), String> {
    if let Some(w) = app.get_webview_window("mfp-login") {
        let _ = w.set_focus();
        return Ok(());
    }
    let path = session_path(&app)?;
    WebviewWindowBuilder::new(&app, "mfp-login", WebviewUrl::External(LOGIN_URL.parse().unwrap()))
        .title("Connect MyFitnessPal")
        .inner_size(480.0, 720.0)
        .user_agent(USER_AGENT)
        .on_page_load(move |win, payload| {
            if payload.event() != PageLoadEvent::Finished || payload.url().path().starts_with("/account/login") {
                return;
            }
            let (win, path) = (win.clone(), path.clone());
            // cookies() pumps the main run loop; never call it from the page load callback itself.
            tauri::async_runtime::spawn(async move {
                let Ok(all) = win.cookies() else { return };
                let cookies = mfp_cookies(all);
                let Some(token) = cookies.iter().find(|c| c.name == SESSION_COOKIE) else { return };
                let body = serde_json::json!({
                    "cookies": cookies,
                    "user_agent": USER_AGENT,
                    "saved_at": time::OffsetDateTime::now_utc().format(&time::format_description::well_known::Rfc3339).unwrap_or_default(),
                    "session_expires": token.expires,
                });
                if write_private(&path, body.to_string().as_bytes()).is_ok() {
                    let app = win.app_handle().clone();
                    let _ = app.emit("mfp-connected", status_from_file(&path));
                    let _ = win.close();
                }
            });
        })
        .build()
        .map_err(|e| e.to_string())?;
    Ok(())
}

#[tauri::command]
pub async fn mfp_disconnect(app: AppHandle) -> Result<(), String> {
    let path = session_path(&app)?;
    let _ = fs::remove_file(&path);
    // The login window shares the main webview's cookie store (macOS 11 has no separate
    // data stores), so remove the MFP cookies there too.
    if let Some(main) = app.get_webview_window("main") {
        if let Ok(all) = main.cookies() {
            for c in all {
                let d = c.domain().unwrap_or("").trim_start_matches('.').to_string();
                if d == "myfitnesspal.com" || d.ends_with(".myfitnesspal.com") {
                    let _ = main.delete_cookie(c);
                }
            }
        }
    }
    Ok(())
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn keeps_only_myfitnesspal_cookies_including_dot_domains() {
        let make = |name: &'static str, domain: &'static str| Cookie::build((name, "v")).domain(domain).path("/").build().into_owned();
        let kept = mfp_cookies(vec![
            make(SESSION_COOKIE, "www.myfitnesspal.com"),
            make("cf_clearance", ".myfitnesspal.com"),
            make("other", "evilmyfitnesspal.com"),
            make("ga", "google.com"),
        ]);
        let names: Vec<_> = kept.iter().map(|c| c.name.as_str()).collect();
        assert_eq!(names, vec![SESSION_COOKIE, "cf_clearance"]);
    }
}
```

Add `time = { version = "0.3", features = ["formatting"] }` to `[dependencies]` (Tauri already depends on it, so this adds no new crate to the build).

- [ ] **Step 3: Register the commands.** In `src-tauri/src/main.rs`, add `mod mfp;` at the top, update the header comment to "Garmin work happens in the sidecars; Rust only handles the MyFitnessPal sign in window.", and add before `.run(...)`:

```rust
        .invoke_handler(tauri::generate_handler![mfp::mfp_connect, mfp::mfp_disconnect, mfp::mfp_status])
```

- [ ] **Step 4: Test and build.** Run: `cd tauri-app/src-tauri && cargo test && cargo build`. Expected: the cookie filter test passes and the build has no errors.

- [ ] **Step 5: Manual check with the user.** Build the dev app in a worktree that the user is not running (`git worktree add <scratchpad>/wt-mfp`), run `bun run dev` there, click Connect (after Task 8) or call `window.__TAURI__.core.invoke('mfp_connect')` from the dev console, and ask the user to sign in. Expected: the window closes by itself, `~/Library/Application Support/dev.danihrndzld.garmin-export/mfp_session.json` exists with mode 0600, and it contains `__Secure-next-auth.session-token`. Then run the Go binary once by hand with `{"cmd":"mfp-export", ...}` for the last 7 days. If Cloudflare blocks the window, stop and tell the user; the fallback is the Zen import that the spec defers.

- [ ] **Step 6: Commit.**

```bash
git add tauri-app/src-tauri
git commit -m "feat(tauri): MyFitnessPal sign in window that saves the session for the Go core"
```

---

### Task 8: UI rows and the export chain

**Files:**
- Modify: `tauri-app/ui/index.html`, `tauri-app/ui/app.js`, `tauri-app/ui/app.css`, `tauri-app/ui/dev-mock.js`

**Interfaces:**
- Consumes: `mfp_connect`, `mfp_disconnect`, `mfp_status`, the `mfp-connected` event (Task 7); `mfp-export` (Task 5); the Bun `summary` command (`{cmd:'summary', jsonPath, csvDir, warnings}` returns `{ok, summary, pack}`).

- [ ] **Step 1: Generalize `platform.run`.** In `app.js`, change `run(req, onEvent)` to `run(req, onEvent, bin = 'binaries/garmin-core')` and `T.shell.Command.sidecar('binaries/garmin-core')` to `T.shell.Command.sidecar(bin)`. Add to the platform object:

```js
    invoke: (cmd, args) => T.core.invoke(cmd, args),
    listen: (ev, cb) => T.event.listen(ev, (e) => cb(e.payload)),
```

In `dev-mock.js`, add `invoke` and `listen` that keep a `connected` flag in `localStorage` (`mock_mfp`), and make `run` answer `mfp-export` after 400 ms with `{ ok: true, days: 30, loggedDays: 17, files: [...] }` and emit three `{kind:'progress', phase:'mfp'}` events.

- [ ] **Step 2: Setup row.** In `index.html`, after the Output field inside the setup grid, add:

```html
      <div class="field wide"><span>MyFitnessPal <i class="hint" title="Adds your food diary to each export. You sign in on MyFitnessPal's own page; the app never sees your password.">?</i></span>
        <div class="mfp-row"><span class="mono" id="mfp-state">Not connected</span><span class="grow"></span>
          <label class="switch"><input type="checkbox" id="mfp-on"><span>Include</span></label>
          <button type="button" class="link mono" id="mfp-btn">connect</button></div></div>
```

- [ ] **Step 3: Run row.** In the `#rows` list, after the `csv` row, add:

```html
      <li data-phase="mfp" hidden><span class="st"></span><div><b>MyFitnessPal</b><span class="sub">Food diary, one request per second</span></div><em class="mono t">—</em></li>
```

- [ ] **Step 4: Wire it in `app.js`.** Add after the bug report section:

```js
  // ── MyFitnessPal ────────────────────────────────────────────────────────
  // Rust owns the sign in window and the session file; the Go core reads it.
  const mfpState = $('mfp-state'), mfpBtn = $('mfp-btn'), mfpOn = $('mfp-on');
  let mfp = { connected: false, expired: false };
  function renderMfp() {
    mfpState.textContent = mfp.expired ? 'Session expired' : mfp.connected ? 'Connected' : 'Not connected';
    mfpBtn.textContent = mfp.expired ? 'reconnect' : mfp.connected ? 'disconnect' : 'connect';
    mfpOn.disabled = !mfp.connected;
    mfpOn.checked = mfp.connected && store.get('mfp_on', '1') === '1';
    row('mfp').hidden = !mfpOn.checked;
  }
  async function refreshMfp() {
    try { mfp = { ...(await platform.invoke('mfp_status')), expired: false }; } catch { mfp = { connected: false }; }
    renderMfp();
  }
  mfpBtn.addEventListener('click', async () => {
    if (mfp.connected && !mfp.expired) { await platform.invoke('mfp_disconnect'); await refreshMfp(); toast('MyFitnessPal disconnected.'); return; }
    toast('Sign in to MyFitnessPal in the new window.');
    await platform.invoke('mfp_connect');
  });
  mfpOn.addEventListener('change', () => { store.set('mfp_on', mfpOn.checked ? '1' : '0'); renderMfp(); });
  platform.listen('mfp-connected', (s) => { mfp = { ...s, expired: false }; renderMfp(); toast('MyFitnessPal connected.'); });

  // Runs after a successful Garmin export; a failure here never changes the Garmin result.
  async function runMfp(csvDir, range) {
    if (!mfpOn.checked) return;
    enterPhase('mfp');
    // onProgress only knows the four Garmin rings, so MFP progress goes to its own row.
    const res = await platform.run({ cmd: 'mfp-export', ...range, dataDir: paths.dataDir, csvDir },
      (e) => (e.kind === 'progress' ? setRow('mfp', undefined, `${e.current} of ${e.total} days`) : e.kind === 'log' && onLog(e)),
      'binaries/garmin-export-core').done;
    if (res.ok) {
      finishPhase('mfp');
      setRow('mfp', 'ok', `Food logged on ${res.loggedDays} of ${res.days} days`);
      const s = await platform.run({ cmd: 'summary', jsonPath: store.get('last_json'), csvDir, warnings: pack?.warnings || [] }, () => {}).done;
      if (s.ok) pack = s.pack || pack;
    } else {
      setRow('mfp', 'warn', 'Skipped: ' + res.error);
      log('warn', 'MyFitnessPal skipped: ' + res.error);
      if (res.errorCode === 'MFP_SESSION') { mfp.expired = mfp.connected; renderMfp(); }
    }
  }
```

In `startExport`, inside `if (res.ok) {`, right after `if (res.summary) renderSummary(res.summary);` and `pack = res.pack || null;`, add the line below. It runs before `running = null`, which `enterPhase` and `finishPhase` need. In `resetRun`, nothing changes: it already resets every row, including `mfp`.

```js
      await runMfp(res.path, { startDate: res.summary?.range?.start, endDate: res.summary?.range?.end });
```

In the boot function, call `refreshMfp();` after `loadLastSummary();`. Add `mfp: 'MyFitnessPal'` to `VERB`. Do not add `mfp` to `PHASES` or `WEIGHT`, so the four Garmin rings keep their meaning; the MFP run row shows its own progress.

- [ ] **Step 5: Style.** In `app.css`, add:

```css
.mfp-row { display: flex; align-items: center; gap: 10px; min-height: 36px; padding: 0 12px; border-radius: 12px; background: var(--field); }
.switch { display: inline-flex; align-items: center; gap: 6px; font-size: 12px; color: var(--mute); }
.switch input:disabled + span { opacity: .5; }
```

- [ ] **Step 6: Check in the browser preview.** Run `python3 -m http.server 4195 -d tauri-app/ui`, open `http://127.0.0.1:4195/index.html` with the Playwright tools, connect through the mock, run an export, and take a screenshot. Expected: the Setup row shows Connected, the MyFitnessPal run row ends with "Food logged on 17 of 30 days", there are no console errors, and nothing overflows at 1000 by 720 or at 860 by 620.

- [ ] **Step 7: Commit.**

```bash
git add tauri-app/ui
git commit -m "feat(ui): MyFitnessPal connect row and run step after the Garmin export"
```

---

### Task 9: LLM pack descriptions, units and the logged days line

**Files:**
- Modify: `tauri-app/sidecar/llm-pack.js`, `tauri-app/sidecar/llm-pack.test.js`

**Interfaces:**
- Consumes: the three CSV files from Task 4.
- Produces: `buildContext` adds descriptions, units, and the logged days line.

- [ ] **Step 1: Write the failing test.** Add to `llm-pack.test.js`:

```js
test('MyFitnessPal files get descriptions, units and the logged days line', () => {
  const dir = exportDir();
  fs.writeFileSync(path.join(dir, 'mfp_daily.csv'), 'date,logged,meals_logged,foods_logged,energy_kcal,protein_g\n2026-05-11,yes,2,4,1800,120\n2026-05-12,no,0,0,,\n2026-05-13,yes,3,6,2100,140\n');
  fs.writeFileSync(path.join(dir, 'mfp_food.csv'), 'date,meal,description,grams,energy_kcal,sodium_mg,calcium_pct_dv\n2026-05-11,Breakfast,Oats,40,150,2,2\n');
  const p = previewPack(opts(dir));
  const en = p.langs.en.context, es = p.langs.es.context;
  expect(en).toContain('MyFitnessPal: food logged on 2 of 3 days. Days with logged=no have no data, which does not mean that the person did not eat. Join with the Garmin files by date.');
  expect(es).toContain('MyFitnessPal: comida registrada en 2 de 3 días.');
  for (const c of ['energy_kcal (kcal)', 'protein_g (g)', 'sodium_mg (mg)', 'grams (g)', 'calcium_pct_dv (% DV)']) expect(en).toContain(c);
  expect(en).toContain('Food diary from MyFitnessPal, one row per food');
});
```

- [ ] **Step 2: Run it.** Run: `cd tauri-app && bun test sidecar/llm-pack.test.js` Expected: FAIL on the first `toContain`.

- [ ] **Step 3: Implement.** In `llm-pack.js`, add to `DESCRIBE`:

```js
  mfp_food: ['Diario de comida de MyFitnessPal, una fila por alimento: comida del día, hora, porción y nutrientes', 'Food diary from MyFitnessPal, one row per food: meal, time, serving and nutrients'],
  mfp_daily: ['MyFitnessPal por día: si hay registro, totales, metas y calorías de ejercicio registradas en MFP', 'MyFitnessPal per day: whether food was logged, totals, goals and exercise calories logged in MFP'],
  mfp_exercise: ['Ejercicio registrado en MyFitnessPal; puede repetir ejercicio que Garmin ya tiene', 'Exercise logged in MyFitnessPal; it can repeat exercise that Garmin already has'],
```

At the start of the `UNITS` array, add:

```js
  [/_kcal(_mfp)?$/, 'kcal'], [/_g$|^grams$/, 'g'], [/_mg$/, 'mg'], [/_min$/, 'min'], [/_pct_dv$/, '% DV'],
```

Add to `T.es` and `T.en`:

```js
    mfp: (n, total) => `MyFitnessPal: comida registrada en ${n} de ${total} días. Los días con logged=no no tienen datos, lo que no significa que la persona no comió. Une con los archivos de Garmin por fecha.`,
```

```js
    mfp: (n, total) => `MyFitnessPal: food logged on ${n} of ${total} days. Days with logged=no have no data, which does not mean that the person did not eat. Join with the Garmin files by date.`,
```

In `buildContext`, before `out.push('', t.missing, ...)`, add:

```js
  const daily = files.find((f) => f.name === 'mfp_daily.csv');
  if (daily) {
    const rows = daily.text.split(/\r?\n/).slice(1).filter(Boolean);
    const logged = rows.filter((r) => r.split(',')[1] === 'yes').length;
    out.push('', t.mfp(logged, rows.length));
  }
```

- [ ] **Step 4: Run all tests.** Run: `cd tauri-app && bun test sidecar && cd ../electron-app && npm test`. Expected: PASS everywhere; the Garmin golden files do not change.

- [ ] **Step 5: Commit.**

```bash
git add tauri-app/sidecar/llm-pack.js tauri-app/sidecar/llm-pack.test.js
git commit -m "feat(llm-pack): describe the MyFitnessPal files and count the days with a food log"
```

---

### Task 10: Docs and the full check

**Files:**
- Modify: `README.md`, `CLAUDE.md`

- [ ] **Step 1: README.** Add a section "Add your MyFitnessPal food diary" after "Give the data to an LLM". It says: the app downloads the food diary from MyFitnessPal after each export; you click Connect in the Setup card and sign in on MyFitnessPal's page in a window of the app, and the app never sees your password; the three files and what they hold; days without a log are marked `logged=no`; MyFitnessPal has no public API, so this uses the same private API as its website, which MyFitnessPal's terms don't allow, and it can stop working when MyFitnessPal changes its site; the app only reads, at one request per second. Add Go to the "Build it yourself" requirements and `bun run test:core` to the test commands.

- [ ] **Step 2: CLAUDE.md.** In the tauri-app section, add one bullet: the Go core in `tauri-app/core` (stage 1 of the Go backend, spec and plan paths), the `mfp-export` command, `mfp_session.json` written by Rust, `mfp-cache/` per day files, `MFP_FAKE_SERVER` loopback only, `bun run test:core`, and the rule that the Go binary never opens `garmin_cache.db`.

- [ ] **Step 3: Full check.** Run, in order: `cd tauri-app && bun run test:core && bun test sidecar && (cd ../electron-app && npm test) && (cd src-tauri && cargo test)`. Expected: all pass. Then build the universal app in a separate worktree (never in the folder the user runs the app from), install it only after asking the user, and run one real export of the last 14 days with MFP connected. Compare `mfp_daily.csv` with the spike: food logged on the same days as `mfp_probe3.py` showed.

- [ ] **Step 4: Commit.**

```bash
git add README.md CLAUDE.md
git commit -m "docs: MyFitnessPal food diary export and the Go core"
```
