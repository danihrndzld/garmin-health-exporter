# Go backend stage 2: Garmin login, client, cache and CSV files, implementation plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Move the Garmin side of the export from the Bun sidecar to the Go binary `garmin-export-core`, with saved Garmin logins (connect once, no password per run) and an Include switch per source, until the 18 golden CSV files and the end to end tests pass against Go and Go is the default engine.

**Architecture:** New Go commands `garmin-status`, `garmin-connect`, `garmin-disconnect`, `clear-cache` and `export` in `cmd/garmin-export-core`. Package `internal/garmin` ports `auth.js`, `client.js`, `endpoints.js`, `exporter.js` and `csv-writer.js`; package `internal/jsjson` gives the JavaScript key order and number output that byte equal CSV files need. The UI keeps the chain export, `mfp-export`, `summary`; a `ge_engine` setting picks Go or Bun for `export` until the flip in Task 13.

**Tech Stack:** Go 1.27 standard library only (`net/http`, `crypto/hmac`, `crypto/sha1`, `encoding/json` token decoder, `net/http/httptest`), vanilla JS UI, Bun for the end to end tests, Playwright for the UI.

**Spec:** `docs/superpowers/specs/2026-10-16-go-core-garmin-design.md`

## Global Constraints

- Stage 1 rules still hold: module `github.com/danihrndzld/garmin-health-exporter/tauri-app/core`, `go 1.24` in `go.mod`, no third party Go modules, `CGO_ENABLED=0`, universal binary through `lipo`.
- The Go binary never opens `garmin_cache.db`. `clear-cache` may delete it.
- `tokens.json` keeps the exact shapes of `electron-app/garmin/auth.js` (DI and legacy), plus `saved_at`. The expired marker is `{"expired_at","saved_at"}` and holds no secret. Mode 0600, temporary file and rename.
- No automatic sign in. Only `garmin-connect` signs in, and it makes at most 2 attempts (mobile, then widget). `sso-cooldown.json` (`{"until": <unix s>}`, 15 minutes, capped when read) blocks it.
- The expired marker is written only on a 400/401 from the DI refresh grant, a 401 from the legacy OAuth2 exchange, or a second 401 on data after a successful refresh. Never on a network error, timeout, 5xx, 403 or 429.
- DI tokens send `NATIVE_HEADERS` from `auth.js:71-80` on every connectapi request.
- `GARMIN_FAKE_SERVER` is used only when it matches `^http://(127\.0\.0\.1|localhost):\d+$`, read on every request. Only then may the pause between requests be 0; otherwise it is 500 ms.
- No test signs in to the real Garmin. Live calls happen only in Task 0 and Task 13, each after the user agrees, never in a loop (Garmin: 429 after 4 sign ins in 40 minutes).
- The password arrives only on stdin, is never logged, never written to disk, never placed in an error message or in argv.
- Every path resolves inside `$HOME` (`guard.InsideHome`). Ranges: at most 90 days, or `daysBack` 1 to 90. `refreshWindow` 1 to 7.
- The golden files in `electron-app/garmin/__tests__/golden` are read only for Go. Only the JavaScript regression test may rewrite them (`UPDATE_GOLDEN=1`), and no task in this plan does.
- Log text that the UI parses stays the same: "Pulling aggregated", "Pulling activities", "Found N activities", "JSON saved: ", progress phases `daily`, `aggregated`, `activities`, `csv`.
- Commits use the author `Daniel Hernández <danisnowman@gmail.com>` and carry no AI attribution.
- User facing text follows the plain writing rules: complete sentences, no dashes, no jargon.

## Review Focus

1. A network error or a 5xx during a token refresh must keep `tokens.json` unchanged, and the run must end with `GARMIN_ERROR`, not `GARMIN_SESSION`. Pinned in Task 3 (ST-T3) and Task 12.
2. `garmin-status` must never send a request. Pinned in Task 2 by a test with `GARMIN_FAKE_SERVER` pointing at a server that fails the test on any call.
3. A cell that holds `72.0` in Garmin's JSON must print `72`, and `1234567` must not print `1.234567e+06`. Pinned in Task 8.
4. `primaryDevice` must pick the same device as JavaScript when a device id key looks like an array index. Pinned in Task 8 (key order) and Task 9 (golden `training.csv`).
5. A wrong password or MFA must stop after the mobile login, with no widget request. A 429 on both logins must write the cooldown, and the next `garmin-connect` must send nothing. Pinned in Task 3 (DT-R2, DT-R3, DT-R9).
6. Cancelling an export (the UI kills the process) must never leave a half written `tokens.json`, cache day file, or session file. Pinned by `atomicfile.WriteFile` tests in Task 1.
7. A Go written `tokens.json` must load in Bun's `auth.loadTokens`, and a Bun written one must load in Go. Pinned in Task 12.

---

## File structure

```
tauri-app/core/
  internal/atomicfile/atomicfile.go            WriteFile (moved from internal/mfp/session.go)
  internal/atomicfile/atomicfile_test.go
  internal/protocol/protocol.go                + Emitter.Warn(msg, code)
  internal/guard/guard.go                      + DaysBack, RefreshWindow, Credentials
  internal/jsjson/value.go                     Value, Object, Parse (JavaScript key order)
  internal/jsjson/format.go                    Number, Stringify, Indent, Cell (String(v))
  internal/jsjson/jsjson_test.go
  internal/garmin/fake.go                      FakeServer() (loopback check), host()
  internal/garmin/tokens.go                    Tokens, LoadTokens, Save, MarkExpired, Delete, Status
  internal/garmin/tokens_test.go
  internal/garmin/auth.go                      Login, Refresh, cooldown, OAuth1 signing, NativeHeaders
  internal/garmin/auth_test.go
  internal/garmin/endpoints.go                 Endpoint table and URL builders
  internal/garmin/endpoints_test.go
  internal/garmin/redact.go                    Redact (port of electron-app/util/redact.js redactString)
  internal/garmin/client.go                    Client.Get: pacing, 401, 403, 429, 204, retries
  internal/garmin/client_test.go
  internal/garmin/cache.go                     per day and per activity JSON files, final rule
  internal/garmin/cache_test.go
  internal/garmin/csv.go                       20 extractors, WriteCSVs
  internal/garmin/csv_test.go                  golden test against electron-app fixtures
  internal/garmin/export.go                    Export (the export command), PrepareDir (garmin:false)
  internal/garmin/export_test.go
  internal/garmin/garmintest/server.go         fake Garmin (port of fake-garmin.js)
  cmd/garmin-export-core/main.go               + garmin-status, garmin-connect, garmin-disconnect, clear-cache, export
  cmd/garmin-export-core/main_test.go          + binary tests for the new commands
tauri-app/sidecar/garmin-e2e.test.js           Go binary against the JavaScript fake, goldens, cross engine tokens
```

Modified: `internal/mfp/session.go`, `internal/mfp/cache.go` (use `atomicfile`), `tauri-app/sidecar/cli.js` (export from tokens, summary from csvDir), `tauri-app/sidecar/e2e.test.js`, `tauri-app/ui/index.html`, `app.js`, `app.css`, `dev-mock.js`, `tauri-app/e2e/support/mock-backend.js`, `e2e/pages/setup-form.js`, new `e2e/pages/garmin-connection.js`, `e2e/export-flow.spec.js`, new `e2e/garmin.spec.js`, `README.md`, `CLAUDE.md`.

Test fixture paths used from Go (relative to `tauri-app/core/internal/garmin`):

```go
const (
	fixturePath = "../../../../electron-app/garmin/__tests__/fixtures/export-sample.json"
	goldenDir   = "../../../../electron-app/garmin/__tests__/golden"
)
```

Stage 3 moves both into `internal/garmin/testdata` when `electron-app/` leaves the build.

## Shippable increments

| After task | What the user gets | Engine for `export` |
| --- | --- | --- |
| 5 | Garmin connects once in Go; exports need no password | Bun |
| 6 | Clear cache in Go | Bun |
| 7 | Include switch per source; MFP only runs | Bun (Go creates the folder when Garmin is off) |
| 12 | Go export complete, tested against the goldens, still behind `ge_engine=go` | Bun |
| 13 | Go is the default | Go |

---

### Task 0: Go spike on the real account (throwaway, needs the user's approval)

This task answers two questions before product code depends on them. Nothing goes into the repository except the answers, which go into the spec under "Spike results".

1. Does Go's `net/http` TLS client get the same answers as Node from `diauth.garmin.com` and `connectapi.garmin.com`?
2. Does the DI refresh rotate the refresh token, and does the answer carry `refresh_token_expires_in`?

Question 3, whether Go can sign in at `sso.garmin.com/mobile/api/login`, costs one of the 4 sign ins per 40 minutes. Ask the user before Step 4, and skip it if they say no; the Go fake covers the logic, and Task 13 makes the first real sign in.

**Files:**
- Create (scratchpad only): `<scratchpad>/garmin-spike/main.go`

- [ ] **Step 1: Ask the user.** Say: the spike reads the app's `tokens.json`, makes one token refresh (not a sign in) and one profile request, and writes the new tokens back to the same file, because Garmin may retire the old refresh token. The app must be closed while it runs.

- [ ] **Step 2: Write the spike.** It prints status codes and field names only, never token values.

```go
package main

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"
)

var native = map[string]string{
	"User-Agent":                  "GCM-Android-5.23",
	"X-Garmin-User-Agent":         "com.garmin.android.apps.connectmobile/5.23; ; Google/sdk_gphone64_arm64/google; Android/33; Dalvik/2.1.0",
	"X-Garmin-Paired-App-Version": "10861",
	"X-Garmin-Client-Platform":    "Android",
	"X-App-Ver":                   "10861",
	"X-Lang":                      "en",
	"X-GCExperience":              "GC5",
	"Accept-Language":             "en-US,en;q=0.9",
}

func main() {
	path := os.Args[1] // the app's tokens.json
	raw, err := os.ReadFile(path)
	if err != nil {
		panic(err)
	}
	var file map[string]json.RawMessage
	_ = json.Unmarshal(raw, &file)
	var di struct {
		RefreshToken string `json:"refresh_token"`
		ClientID     string `json:"client_id"`
	}
	if json.Unmarshal(file["di"], &di) != nil || di.RefreshToken == "" {
		fmt.Println("no DI login in tokens.json; stop here")
		return
	}
	form := url.Values{"grant_type": {"refresh_token"}, "client_id": {di.ClientID}, "refresh_token": {di.RefreshToken}}
	req, _ := http.NewRequest("POST", "https://diauth.garmin.com/di-oauth2-service/oauth/token", strings.NewReader(form.Encode()))
	for k, v := range native {
		req.Header.Set(k, v)
	}
	req.Header.Set("Authorization", "Basic "+base64.StdEncoding.EncodeToString([]byte(di.ClientID+":")))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		panic(err)
	}
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	var tok map[string]any
	_ = json.Unmarshal(body, &tok)
	keys := []string{}
	for k := range tok {
		keys = append(keys, k)
	}
	fmt.Println("refresh -> HTTP", resp.StatusCode, "keys:", keys)
	if resp.StatusCode != 200 {
		return
	}
	fmt.Println("refresh token rotated:", tok["refresh_token"] != nil && tok["refresh_token"] != di.RefreshToken)
	fmt.Println("refresh_token_expires_in:", tok["refresh_token_expires_in"])
	// Write the new tokens back in auth.js's shape, so the app keeps its login.
	now := time.Now().Unix()
	exp, _ := tok["expires_in"].(float64)
	next := map[string]any{"di": map[string]any{
		"access_token": tok["access_token"], "refresh_token": firstNonNil(tok["refresh_token"], di.RefreshToken),
		"client_id": di.ClientID, "expires_at": now + int64(exp), "refresh_token_expires_at": nil,
	}}
	if r, ok := tok["refresh_token_expires_in"].(float64); ok {
		next["di"].(map[string]any)["refresh_token_expires_at"] = now + int64(r)
	}
	b, _ := json.MarshalIndent(next, "", "  ")
	if err := os.WriteFile(path, b, 0o600); err != nil {
		panic(err)
	}
	time.Sleep(time.Second)
	req, _ = http.NewRequest("GET", "https://connectapi.garmin.com/userprofile-service/socialProfile", nil)
	for k, v := range native {
		req.Header.Set(k, v)
	}
	req.Header.Set("Authorization", "Bearer "+tok["access_token"].(string))
	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		panic(err)
	}
	resp.Body.Close()
	fmt.Println("socialProfile -> HTTP", resp.StatusCode)
}

func firstNonNil(a any, b string) any {
	if a != nil {
		return a
	}
	return b
}
```

- [ ] **Step 3: Run it** with the app closed: `cd <scratchpad>/garmin-spike && go run . "$HOME/Library/Application Support/<identifier>/tokens.json"`. The identifier is `dev.danihrndzld.garmin-export` (`tauri-app/src-tauri/tauri.conf.json`). Expected: `refresh -> HTTP 200`, `socialProfile -> HTTP 200`. If either is 403 with a Cloudflare page, add a step to Task 3 that uses `utls` for the Garmin hosts and tell the user, because it breaks the "no third party modules" rule.

- [ ] **Step 4 (only if the user agrees to one sign in):** run the mobile login request from `auth.js:259-288` once from Go, then the DI exchange, and write the tokens back. Record the HTTP status of each step.

- [ ] **Step 5:** Add a "Spike results (Go), <date>" section to the spec with the answers. Commit only the spec change: `git commit -m "docs(spec): Go stage 2 spike results"`.

---

### Task 1: Shared pieces: atomic write, warn with code, guard checks

**Files:**
- Create: `internal/atomicfile/atomicfile.go`, `internal/atomicfile/atomicfile_test.go`
- Modify: `internal/mfp/session.go` (delete `writeFileAtomic`, call `atomicfile.WriteFile`), `internal/mfp/cache.go`, `internal/protocol/protocol.go`, `internal/protocol/protocol_test.go`, `internal/guard/guard.go`, `internal/guard/guard_test.go`

**Interfaces:**
- Produces: `atomicfile.WriteFile(path string, data []byte) error`; `(*protocol.Emitter).Warn(msg, code string)`; `guard.DaysBack(n int, now time.Time) (start, end string, err error)`; `guard.RefreshWindow(n int) error`; `guard.Credentials(email, password string) error`.

- [ ] **Step 1: Move the helper.** `git mv` is not possible for a function, so create `internal/atomicfile/atomicfile.go` with the body of `writeFileAtomic` from `internal/mfp/session.go:97-117`, renamed `WriteFile`, and replace the two call sites in `internal/mfp`. Move its existing test cases from `internal/mfp/session_test.go` (the "failed write keeps the old file" case) to `atomicfile_test.go`.

- [ ] **Step 2: Write the failing tests.**

`internal/protocol/protocol_test.go`, add:

```go
func TestWarnCarriesTheErrorCode(t *testing.T) {
	var buf bytes.Buffer
	e := Emitter{Out: &buf}
	e.Warn("[skip] hrv: HTTP 500", "HTTP_5XX")
	var line map[string]any
	if err := json.Unmarshal(buf.Bytes(), &line); err != nil {
		t.Fatal(err)
	}
	if line["kind"] != "log" || line["type"] != "warn" || line["errorCode"] != "HTTP_5XX" || line["msg"] != "[skip] hrv: HTTP 500" {
		t.Fatalf("bad warn line %v", line)
	}
}
```

`internal/guard/guard_test.go`, add:

```go
func TestDaysBackEndsTodayInLocalTime(t *testing.T) {
	now := time.Date(2026, 10, 15, 23, 30, 0, 0, time.Local)
	s, e, err := DaysBack(30, now)
	if err != nil || s != "2026-09-16" || e != "2026-10-15" {
		t.Fatalf("DaysBack(30) = %s %s %v", s, e, err)
	}
	for _, bad := range []int{0, -1, 91} {
		if _, _, err := DaysBack(bad, now); err == nil {
			t.Errorf("DaysBack(%d) accepted", bad)
		}
	}
}

func TestRefreshWindow(t *testing.T) {
	for _, ok := range []int{1, 3, 7} {
		if RefreshWindow(ok) != nil {
			t.Errorf("%d rejected", ok)
		}
	}
	for _, bad := range []int{0, 8} {
		if RefreshWindow(bad) == nil {
			t.Errorf("%d accepted", bad)
		}
	}
}

func TestCredentials(t *testing.T) {
	if Credentials("a@b.c", "pw") != nil {
		t.Error("valid credentials rejected")
	}
	for _, c := range [][2]string{{"", "pw"}, {"a@b.c", ""}, {strings.Repeat("a", 321), "pw"}, {"a@b.c", strings.Repeat("p", 1025)}} {
		if Credentials(c[0], c[1]) == nil {
			t.Errorf("%q accepted", c[0])
		}
	}
	// The error text must never contain the password.
	if err := Credentials("a@b.c", strings.Repeat("s3cret", 200)); err == nil || strings.Contains(err.Error(), "s3cret") {
		t.Errorf("error leaks the password: %v", err)
	}
}
```

- [ ] **Step 3: Run them.** Run: `cd tauri-app/core && go test ./internal/protocol ./internal/guard ./internal/atomicfile` Expected: FAIL, the functions do not exist.

- [ ] **Step 4: Implement.** `Warn` writes `{"kind":"log","type":"warn","msg":msg,"errorCode":code,"ts":...}`. `DaysBack` ports `exporter.js:224-228` (`end = today`, `start = today - (n-1) days`, both from `now.In(time.Local)`). `RefreshWindow` ports `cli.js:54-55`. `Credentials` checks non empty and the byte limits from the spec, with fixed error sentences.

- [ ] **Step 5: Run every Go test.** Run: `cd tauri-app && bun run test:core` Expected: PASS, including all stage 1 MFP tests.

- [ ] **Step 6: Commit.**

```bash
git add tauri-app/core
git commit -m "refactor(core): shared atomic write, warn lines with a code, guard checks for Garmin input"
```

---

### Task 2: tokens.json, garmin-status and garmin-disconnect

**Files:**
- Create: `internal/garmin/fake.go`, `internal/garmin/tokens.go`, `internal/garmin/tokens_test.go`
- Modify: `cmd/garmin-export-core/main.go`, `cmd/garmin-export-core/main_test.go`

**Interfaces:**
- Produces:

```go
type DI struct {
	AccessToken           string `json:"access_token"`
	RefreshToken          string `json:"refresh_token,omitempty"`
	ClientID              string `json:"client_id"`
	ExpiresAt             int64  `json:"expires_at"`
	RefreshTokenExpiresAt *int64 `json:"refresh_token_expires_at"` // null when Garmin sends no lifetime
}
type OAuth1 struct{ Token, Secret string } // json: oauth_token, oauth_token_secret
type OAuth2 struct {
	AccessToken           string `json:"access_token"`
	TokenType             string `json:"token_type"`
	RefreshToken          string `json:"refresh_token"`
	ExpiresAt             int64  `json:"expires_at"`
	RefreshTokenExpiresAt int64  `json:"refresh_token_expires_at"`
}
type Consumer struct{ Key, Secret string } // json: consumer_key, consumer_secret
type Tokens struct {
	DI        *DI       `json:"di,omitempty"`
	OAuth1    *OAuth1   `json:"oauth1,omitempty"`
	OAuth2    *OAuth2   `json:"oauth2,omitempty"`
	Consumer  *Consumer `json:"consumer,omitempty"`
	SavedAt   string    `json:"saved_at,omitempty"`
	ExpiredAt string    `json:"expired_at,omitempty"`
}
type Status struct {
	Connected bool    `json:"connected"`
	Expired   bool    `json:"expired"`
	SavedAt   *string `json:"savedAt"`
	Kind      *string `json:"kind"`
}
var ErrNoLogin = errors.New("no Garmin login")
func LoadTokens(dataDir string) (*Tokens, error)      // ErrNoLogin when missing, corrupt or incomplete
func (t *Tokens) Save(dataDir string) error             // sets SavedAt when empty; atomicfile, 0600
func MarkExpired(dataDir string, now time.Time) error  // writes {expired_at, saved_at}
func Delete(dataDir string) error                       // missing file is not an error
func StatusOf(dataDir string, now time.Time) Status    // never sends a request
func FakeServer() string                                // GARMIN_FAKE_SERVER when loopback, else ""
```

- [ ] **Step 1: Write the failing tests.** `internal/garmin/tokens_test.go`:

```go
package garmin

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

// Written by electron-app/garmin/auth.js:_writeTokens; Go must read both shapes as they are.
const bunDI = `{"di":{"access_token":"a.b.c","refresh_token":"refresh-1","client_id":"GARMIN_CONNECT_MOBILE_ANDROID_DI_2025Q2","expires_at":1760540000,"refresh_token_expires_at":1763132000}}`
const bunLegacy = `{"oauth1":{"oauth_token":"ot","oauth_token_secret":"os"},"oauth2":{"access_token":"legacy-1","token_type":"Bearer","refresh_token":"lr","expires_at":1760540000,"refresh_token_expires_at":1763132000},"consumer":{"consumer_key":"ck","consumer_secret":"cs"}}`

func write(t *testing.T, dir, body string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, "tokens.json"), []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestStatusDecisionTable(t *testing.T) {
	now := time.Unix(1760000000, 0)
	cases := []struct {
		id, body           string
		connected, expired bool
		kind               string
	}{
		{"DT-S1 no file", "", false, false, ""},
		{"DT-S2 corrupt file", "{", false, false, ""},
		{"DT-S3 DI, refresh token valid", bunDI, true, false, "di"},
		{"DT-S4 DI, refresh token past its expiry", `{"di":{"access_token":"x","refresh_token":"r","client_id":"c","expires_at":1,"refresh_token_expires_at":1759999999}}`, true, true, "di"},
		{"DT-S5 DI without a refresh token", `{"di":{"access_token":"x","client_id":"c","expires_at":1}}`, true, true, "di"},
		{"DT-S6 DI with no known refresh lifetime", `{"di":{"access_token":"x","refresh_token":"r","client_id":"c","expires_at":1,"refresh_token_expires_at":null}}`, true, false, "di"},
		{"DT-S7 legacy, old OAuth2 dates do not expire it", bunLegacy, true, false, "legacy"},
		{"DT-S8 expired marker", `{"expired_at":"2026-10-01T00:00:00Z"}`, true, true, ""},
		{"DT-S9 legacy without consumer is incomplete", `{"oauth1":{"oauth_token":"ot","oauth_token_secret":"os"},"oauth2":{"access_token":"x"}}`, false, false, ""},
	}
	for _, c := range cases {
		t.Run(c.id, func(t *testing.T) {
			dir := t.TempDir()
			if c.body != "" {
				write(t, dir, c.body)
			}
			s := StatusOf(dir, now)
			kind := ""
			if s.Kind != nil {
				kind = *s.Kind
			}
			if s.Connected != c.connected || s.Expired != c.expired || kind != c.kind {
				t.Fatalf("got %+v kind=%q", s, kind)
			}
		})
	}
}

func TestSaveKeepsAuthJsShapeAndMode(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, bunDI)
	tok, err := LoadTokens(dir)
	if err != nil {
		t.Fatal(err)
	}
	if err := tok.Save(dir); err != nil {
		t.Fatal(err)
	}
	info, _ := os.Stat(filepath.Join(dir, "tokens.json"))
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("mode %v", info.Mode().Perm())
	}
	var back map[string]map[string]any
	b, _ := os.ReadFile(filepath.Join(dir, "tokens.json"))
	_ = json.Unmarshal(b, &back)
	for _, k := range []string{"access_token", "refresh_token", "client_id", "expires_at", "refresh_token_expires_at"} {
		if _, ok := back["di"][k]; !ok {
			t.Errorf("di.%s missing after a Go save", k)
		}
	}
}

func TestMarkExpiredDropsEverySecret(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, bunLegacy)
	if err := MarkExpired(dir, time.Unix(1760000000, 0)); err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(filepath.Join(dir, "tokens.json"))
	for _, secret := range []string{"legacy-1", "lr", "ot", "os", "cs"} {
		if strings.Contains(string(b), `"`+secret+`"`) {
			t.Errorf("marker still holds %q: %s", secret, b)
		}
	}
	if _, err := LoadTokens(dir); err == nil {
		t.Error("an expired marker must not load as a login")
	}
}

func TestDeleteMissingFileIsFine(t *testing.T) {
	if err := Delete(t.TempDir()); err != nil {
		t.Fatal(err)
	}
}

func TestFakeServerAcceptsLoopbackOnly(t *testing.T) {
	for v, want := range map[string]string{
		"http://127.0.0.1:4100": "http://127.0.0.1:4100", "http://localhost:9": "http://localhost:9",
		"https://127.0.0.1:4100": "", "http://evil.com:80": "", "http://127.0.0.1:4100/x": "", "": "",
	} {
		t.Setenv("GARMIN_FAKE_SERVER", v)
		if got := FakeServer(); got != want {
			t.Errorf("%q -> %q", v, got)
		}
	}
}
```

Add the imports `encoding/json` and `strings`.

`cmd/garmin-export-core/main_test.go`, add a test that runs the binary with `garmin-status` while `GARMIN_FAKE_SERVER` points at an `httptest` server whose handler calls `t.Errorf("garmin-status sent a request")`, then `garmin-disconnect`, then `garmin-status` again and expects `connected:false`. Data dirs come from `os.MkdirTemp(home, ".garmin-binary-test-")`.

- [ ] **Step 2: Run them.** Run: `cd tauri-app/core && go test ./internal/garmin ./cmd/...` Expected: FAIL.

- [ ] **Step 3: Implement.** `LoadTokens` accepts a DI login when `di.access_token` is non empty, a legacy login when `oauth1`, `oauth2` and `consumer` are all present (`auth.js:540-542`), and returns `ErrNoLogin` otherwise, including for the marker. `StatusOf` reads the file once and follows the decision table. In `main.go`:

```go
case "garmin-status", "garmin-disconnect":
	dataDir, err := guard.InsideHome(str(req, "dataDir"))
	if err != nil {
		out.Result(map[string]any{"ok": false, "errorCode": "GARMIN_INPUT", "error": "dataDir: " + err.Error()})
		return
	}
	if req["cmd"] == "garmin-disconnect" {
		if err := garmin.Delete(dataDir); err != nil {
			out.Result(map[string]any{"ok": false, "errorCode": "GARMIN_ERROR", "error": err.Error()})
			return
		}
	}
	s := garmin.StatusOf(dataDir, time.Now())
	out.Result(map[string]any{"ok": true, "connected": s.Connected, "expired": s.Expired, "savedAt": s.SavedAt, "kind": s.Kind})
```

Add a small `str(req, key)` helper in `main.go`.

- [ ] **Step 4: Run them.** Run: `cd tauri-app && bun run test:core` Expected: PASS.

- [ ] **Step 5: Commit.**

```bash
git add tauri-app/core
git commit -m "feat(core): read and write tokens.json in the auth.js shapes; garmin-status and garmin-disconnect"
```

---

### Task 3: Garmin sign in and token refresh in Go

**Files:**
- Create: `internal/garmin/garmintest/server.go`, `internal/garmin/auth.go`, `internal/garmin/auth_test.go`

**Interfaces:**
- Consumes: `Tokens`, `LoadTokens`, `MarkExpired` (Task 2).
- Produces:

```go
type LoginError struct{ Code, Msg string } // Code: GARMIN_BAD_PASSWORD, GARMIN_MFA, GARMIN_RATE_LIMITED, GARMIN_BLOCKED, GARMIN_ERROR
func (e *LoginError) Error() string
type Auth struct {
	DataDir string
	HTTP    *http.Client
	Log     func(msg string) // dim lines; never receives the password
	Now     func() time.Time
}
func (a *Auth) Login(ctx context.Context, email, password string) (*Tokens, error) // writes tokens.json on success
func (a *Auth) Refresh(ctx context.Context, t *Tokens) (*Tokens, error)            // ErrSessionDead on a proven dead login
func (a *Auth) Fresh(ctx context.Context) (*Tokens, error)                          // load, and refresh 15 minutes before expiry
func Headers(t *Tokens, access string) http.Header                                  // native headers for DI, mobile UA for legacy
var ErrSessionDead = errors.New("the Garmin login is no longer valid")
```

`garmintest.Server` mirrors `fake-garmin.js:startFakeGarmin`:

```go
type Control struct {
	Password     string         // default "right-password"
	MobileLogin  string         // "ok" | "mfa" | "cloudflare" | "429" | "captcha"
	DIAuth       string         // "ok" | "429" | "fail"
	RefreshFail  int            // when not 0, the refresh grant answers this status (new: 400, 401, 500)
	TokenTTL     int            // seconds, default 3600
	Widget429    bool
	WidgetStatus int
	DataStatus   map[string]int // path prefix -> status, every time
	FailOnce     map[string]int // path prefix -> status, once
}
type Call struct{ Method, Path, Query string; Header http.Header; Body string }
func New(fixture []byte) *Server // fixture: the export JSON used for the data routes (Task 10 fills them)
func (s *Server) URL() string
func (s *Server) Control() *Control // guarded by a mutex
func (s *Server) Calls(path string) []Call
func (s *Server) Reset()
func (s *Server) Close()
```

In this task the server answers the login, diauth, consumer, widget and OAuth1/OAuth2 routes, plus `/userprofile-service/socialProfile` with a token check (401 for an unknown token). Task 10 adds the data routes.

- [ ] **Step 1: Write the failing tests.** `internal/garmin/auth_test.go` maps each login test of `electron-app/garmin/__tests__/integration.test.js` to a table row with the same ID. One table for the sign in chain:

```go
func TestLoginDecisionTable(t *testing.T) {
	cases := []struct {
		id                string
		set               func(*garmintest.Control)
		password          string
		wantCode, wantKind string // wantCode "" means success
		widgetPosts       int
	}{
		{"DT-R1 mobile and DI succeed", nil, "right-password", "", "di", 0},
		{"DT-R2 wrong password stops the chain", nil, "nope", "GARMIN_BAD_PASSWORD", "", 0},
		{"DT-R3 MFA stops the chain", func(c *garmintest.Control) { c.MobileLogin = "mfa" }, "right-password", "GARMIN_MFA", "", 0},
		{"DT-R4 429 on mobile, legacy signs in", func(c *garmintest.Control) { c.MobileLogin = "429" }, "right-password", "", "legacy", 1},
		{"DT-R5 Cloudflare 403 on mobile, legacy signs in", func(c *garmintest.Control) { c.MobileLogin = "cloudflare" }, "right-password", "", "legacy", 1},
		{"DT-R6 captcha on mobile, legacy signs in", func(c *garmintest.Control) { c.MobileLogin = "captcha" }, "right-password", "", "legacy", 1},
		{"DT-R7 DI refused for every client id, legacy signs in", func(c *garmintest.Control) { c.DIAuth = "fail" }, "right-password", "", "legacy", 1},
		{"DT-R8 DI answers 429, no legacy", func(c *garmintest.Control) { c.DIAuth = "429" }, "right-password", "GARMIN_RATE_LIMITED", "", 0},
		{"DT-R10 both logins blocked (403)", func(c *garmintest.Control) { c.MobileLogin = "cloudflare"; c.WidgetStatus = 403 }, "right-password", "GARMIN_BLOCKED", "", 1},
	}
	srv := garmintest.New(nil)
	t.Cleanup(srv.Close)
	t.Setenv("GARMIN_FAKE_SERVER", srv.URL())
	for _, c := range cases {
		t.Run(c.id, func(t *testing.T) {
			srv.Reset()
			if c.set != nil {
				c.set(srv.Control())
			}
			dir := t.TempDir()
			a := &Auth{DataDir: dir}
			tok, err := a.Login(context.Background(), "e2e@example.invalid", c.password)
			if c.wantCode == "" {
				if err != nil {
					t.Fatalf("login failed: %v", err)
				}
				if (c.wantKind == "di") != (tok.DI != nil) {
					t.Fatalf("kind: got DI=%v", tok.DI != nil)
				}
			} else {
				var le *LoginError
				if !errors.As(err, &le) || le.Code != c.wantCode {
					t.Fatalf("got %v, want %s", err, c.wantCode)
				}
				if strings.Contains(le.Msg, c.password) {
					t.Fatal("the error holds the password")
				}
			}
			if n := posts(srv.Calls("/sso/signin")); n != c.widgetPosts {
				t.Fatalf("widget sign in posts: %d, want %d", n, c.widgetPosts)
			}
		})
	}
}
```

`posts(calls)` counts the calls whose `Method` is `POST`. Separate tests, one per ID:
- `DT-R9`: mobile 429 and `Widget429`: `GARMIN_RATE_LIMITED`, `sso-cooldown.json` exists with `until` about 900 s ahead, and a second `Login` sends no request (`len(srv.Calls(""))` unchanged) and says how many minutes are left.
- `DT-R11` (new): a cooldown file written by Bun (`{"until": now+600}`) blocks Go. A file with `until` a year ahead blocks for at most 15 minutes.
- `ST-T1`: a DI token within 15 minutes of expiry is refreshed by `Fresh`, and `tokens.json` holds the new access token.
- `ST-T2`: a legacy token past its OAuth2 refresh expiry is renewed through the OAuth1 exchange, not a sign in (no `/mobile/api/login` call).
- `ST-T3` (new, Review Focus 1): the refresh grant answers 500, then the network is closed (`srv.Close()` before the call): `Fresh` returns an error that is not `ErrSessionDead`, and `tokens.json` is byte for byte unchanged.
- `ST-T4` (new): the refresh grant answers 400: `Fresh` returns `ErrSessionDead` and the caller can mark the file expired.
- `EG-06`: the JWT helper of the fake builds tokens whose `exp` and `client_id` `Auth` reads (`jwtClaims`).
- `OAuth1`: `signOAuth1` with fixed nonce and timestamp gives the signature that `auth.js:signOAuth1` gives for the same input. Compute the expected value once with `node -e "console.log(require('./electron-app/garmin/auth').signOAuth1('GET','https://x/oauth-service/oauth/preauthorized',{oauth_consumer_key:'ck',oauth_nonce:'n',oauth_signature_method:'HMAC-SHA1',oauth_timestamp:'1',oauth_version:'1.0',ticket:'ST-1','login-url':'https://sso.garmin.com/sso/embed','accepts-mfa-tokens':'true'},'cs'))"` and paste it into the test.

- [ ] **Step 2: Run them.** Run: `cd tauri-app/core && go test ./internal/garmin -run 'Login|Refresh|Fresh|Cooldown|OAuth1|DT_R|ST_T'` Expected: FAIL.

- [ ] **Step 3: Implement `garmintest.Server`** by porting `fake-garmin.js:42-139` route by route. Keep the same JSON answers, the `set-cookie: GARMIN-SSO=1` on `/sso/embed`, and the ticket page `var response_url = "https://sso.garmin.com/sso/embed?ticket=ST-widget-1";`.

- [ ] **Step 4: Implement `auth.go`** by porting `auth.js`:
- constants: lines 57-93 (`DI_CLIENT_IDS`, `NATIVE_HEADERS`, user agents, `REFRESH_MARGIN_S`, `SSO_COOLDOWN_S`, `CSRF_RE`, `TICKET_RE`).
- `percentEncode` (RFC 3986: `url.QueryEscape` then `+` to `%20`, and keep `~`), `buildBaseString`, `signOAuth1`, `oauthHeader`: lines 122-154.
- `Login` = `login` + `_loginChain` + `_loginMobile` + `_exchangeTicketForDi` + `_diTokenRequest` + `_loginWidget` + `_legacyFromTicket` + `_exchangeOAuth1ForOAuth2`: lines 202-516 and 638-689. The widget flow uses an `http.Client` with `CheckRedirect` returning `http.ErrUseLastResponse` (the JS uses `redirect: 'manual'`) and a cookie jar of its own (`net/http/cookiejar`).
- `Fresh` = `loadTokens`: lines 518-561. `Refresh` = `refresh`: lines 563-589, with one change: return `ErrSessionDead` only for a 400 or 401 from diauth or a 401 from the OAuth2 exchange, and a plain error for anything else.
- Never pass `password` to `Log` or into an error. Map the JS messages to the spec's error sentences.

- [ ] **Step 5: Run them.** Run: `cd tauri-app && bun run test:core` Expected: PASS.

- [ ] **Step 6: Commit.**

```bash
git add tauri-app/core/internal/garmin
git commit -m "feat(core): Garmin sign in (DI first, legacy once), cooldown and token refresh, with a fake Garmin"
```

---

### Task 4: garmin-connect command

**Files:**
- Modify: `cmd/garmin-export-core/main.go`, `cmd/garmin-export-core/main_test.go`

**Interfaces:**
- Consumes: `Auth.Login` (Task 3), `guard.Credentials` (Task 1), `StatusOf` (Task 2).
- Produces: the `garmin-connect` result described in the spec.

- [ ] **Step 1: Write the failing test.** In `main_test.go`, start `garmintest.New(nil)`, set `GARMIN_FAKE_SERVER` for the binary, and check three rows:
- right password: `{ok:true, connected:true, expired:false, kind:"di"}`, `tokens.json` mode 0600, and no stdout line contains the password;
- wrong password: `{ok:false, errorCode:"GARMIN_BAD_PASSWORD"}`, no `tokens.json`;
- a request with `"password":""`: `{ok:false, errorCode:"GARMIN_INPUT"}`, and the fake saw no call.

- [ ] **Step 2: Run it.** Run: `cd tauri-app/core && go test ./cmd/...` Expected: FAIL.

- [ ] **Step 3: Implement.** In `main.go`:

```go
case "garmin-connect":
	dataDir, err := guard.InsideHome(str(req, "dataDir"))
	if err == nil {
		err = guard.Credentials(str(req, "email"), str(req, "password"))
	}
	if err != nil {
		out.Result(map[string]any{"ok": false, "errorCode": "GARMIN_INPUT", "error": err.Error()})
		return
	}
	a := &garmin.Auth{DataDir: dataDir, Log: func(m string) { out.Log("dim", m) }}
	if _, err := a.Login(context.Background(), str(req, "email"), str(req, "password")); err != nil {
		out.Result(garmin.LoginResult(err))
		return
	}
	s := garmin.StatusOf(dataDir, time.Now())
	out.Result(map[string]any{"ok": true, "connected": s.Connected, "expired": s.Expired, "savedAt": s.SavedAt, "kind": s.Kind})
```

`garmin.LoginResult(err)` maps a `*LoginError` to `{ok:false, errorCode, error}` and anything else to `GARMIN_ERROR`.

- [ ] **Step 4: Run it.** Run: `cd tauri-app && bun run test:core` Expected: PASS.

- [ ] **Step 5: Commit.**

```bash
git add tauri-app/core/cmd
git commit -m "feat(core): garmin-connect signs in once and saves tokens.json"
```

---

### Task 5: Garmin connection row in the UI; Bun exports from the saved login

This is the first shippable increment: the user connects Garmin once, and each export runs without a password.

**Files:**
- Modify: `electron-app/garmin/exporter.js:163`, `tauri-app/sidecar/cli.js:39`, `tauri-app/sidecar/e2e.test.js`, `tauri-app/ui/index.html:71-97`, `tauri-app/ui/app.js`, `tauri-app/ui/app.css`, `tauri-app/ui/dev-mock.js`, `tauri-app/e2e/support/mock-backend.js`, `tauri-app/e2e/pages/setup-form.js`, `tauri-app/e2e/export-flow.spec.js`
- Create: `tauri-app/e2e/pages/garmin-connection.js`, `tauri-app/e2e/garmin.spec.js`

**Interfaces:**
- Consumes: `garmin-status`, `garmin-connect`, `garmin-disconnect` (Tasks 2 and 4).
- Produces: Bun `export` without `email`/`password`; UI state `garmin = {connected, expired, savedAt}`; localStorage `ge_garmin_on`.

- [ ] **Step 1: Write the failing Bun test.** In `tauri-app/sidecar/e2e.test.js`, add:

```js
test('AC-08 export runs from a saved login without email or password', async () => {
  const { out, data } = dirs();
  const login = await sidecar({ cmd: 'export', email: 'e2e@example.invalid', password: 'right-password',
    startDate: FIXTURE.date_range.start, endDate: FIXTURE.date_range.end, outputDir: out, dataDir: data });
  expect(login.result.ok).toBe(true);
  fake.calls.length = 0;
  const { result } = await sidecar({ cmd: 'export', startDate: FIXTURE.date_range.start, endDate: FIXTURE.date_range.end, outputDir: out, dataDir: data });
  expect(result.ok).toBe(true);
  expect(fake.calls.filter((c) => c.path === '/mobile/api/login' || c.path === '/sso/signin')).toEqual([]);
}, 60000);

test('AC-09 without a saved login, export asks to connect Garmin and sends no sign in', async () => {
  const { out, data } = dirs();
  fake.calls.length = 0;
  const { result } = await sidecar({ cmd: 'export', startDate: FIXTURE.date_range.start, endDate: FIXTURE.date_range.end, outputDir: out, dataDir: data });
  expect(result).toMatchObject({ ok: false, errorCode: 'GARMIN_SESSION', error: 'Connect Garmin first.' });
  expect(fake.calls.filter((c) => c.path === '/mobile/api/login')).toEqual([]);
}, 60000);
```

- [ ] **Step 2: Run it.** Run: `cd tauri-app && bun test sidecar/e2e.test.js` Expected: AC-08 and AC-09 FAIL ("Email and password are required.").

- [ ] **Step 3: Change Bun.** The guard goes in the shared function, not only in `cli.js`: `exporter.js:163` calls `auth.login(email, password)` whenever `loadTokens()` fails, including when a refresh fails on a network error, so without a guard there the Bun engine would send a sign in with empty credentials and spend one of the 4 sign ins per 40 minutes.
- In `electron-app/garmin/exporter.js`, right before line 163, add `if (!email || !password) return { ok: false, errorCode: 'GARMIN_SESSION', error: 'Connect Garmin first.' };`.
- In `cli.js:runExport`, delete `if (!email || !password) return fail('Email and password are required.');` and change `if (!res.ok) return fail(res.error);` to `if (!res.ok) return fail(res.error, res.errorCode ? { errorCode: res.errorCode } : {});`.
- `email` and `password` still pass through when present, so an old UI keeps working for one release; the new UI never sends them.
- Add to AC-09 a second case: write a DI `tokens.json` that expires within 15 minutes, set `fake.control.diauth = 'fail'`, run `export` without credentials, and expect `GARMIN_SESSION` and no call to `/mobile/api/login` or `/sso/signin`. Reset `fake.control.diauth = 'ok'` in a `finally`.

Run Step 2 again and `cd electron-app && npm test`. Expected: PASS.

- [ ] **Step 4: UI markup.** In `index.html`, replace the email and password labels in the Setup card with a Garmin row built like the MFP row:

```html
<div class="field full"><span>Garmin Connect <i class="hint" title="Connect once. The app saves a login token, not your password.">?</i></span>
  <div class="mfp-row"><span class="mono" id="garmin-state">Not connected</span><span class="grow"></span>
    <label class="switch"><input type="checkbox" id="garmin-on"><span>Include</span></label>
    <button type="button" class="link mono" id="garmin-btn">connect</button></div>
  <div class="garmin-form" id="garmin-form" hidden>
    <label><span>Email</span><input type="email" id="email" autocomplete="username" spellcheck="false" placeholder="you@example.com"></label>
    <label><span>Password</span><span class="pw"><input type="password" id="password" autocomplete="current-password" placeholder="••••••••">
      <button type="button" class="eye" id="eye" aria-label="Show password">show</button></span></label>
    <p class="err" id="garmin-err" role="alert" hidden></p>
    <button type="button" class="link mono" id="garmin-signin">sign in</button>
    <button type="button" class="link mono" id="garmin-cancel">cancel</button></div></div>
```

The ids `email`, `password` and `eye` stay, so existing CSS and the `ge_email` code keep working.

- [ ] **Step 5: UI logic.** In `app.js`, next to the MFP block:
- `let garmin = { connected: false, expired: false }`; `renderGarmin()` mirrors `renderMfp()` (`app.js:676-685`) with `ge_garmin_on`, and hides or shows the form.
- `refreshGarmin()` runs `platform.run({ cmd: 'garmin-status', dataDir: paths.dataDir }, () => {}, 'binaries/garmin-export-core').done` on start and in `platform.onFocus`.
- The button: connected and not expired runs `garmin-disconnect`; otherwise it opens the form and focuses the email field.
- "sign in" runs `garmin-connect` with `{ email, password, dataDir }` on the Go binary, then sets `passEl.value = ''` in a `finally`. On `ok`, close the form and toast "Garmin connected."; else show `res.error` in `#garmin-err`.
- `startExport`: remove the email and password check (`app.js:344-350`) and the two fields from the request. Instead, when `!usable(garmin, garminOn)` toast "Connect Garmin first." and return. (Task 7 widens this rule to MFP only runs.)
- After an export result with `errorCode === 'GARMIN_SESSION'`, set `garmin.expired = garmin.connected` and render, as `runMfp` does for `MFP_SESSION`.
- `lock()` adds `garminBtn`, `garminOn` and the form fields.

- [ ] **Step 6: Mock and Playwright.** In `dev-mock.js`, answer `garmin-status`, `garmin-connect` (`fail@x.com` gives `GARMIN_BAD_PASSWORD`, `mfa@x.com` gives `GARMIN_MFA`), and `garmin-disconnect` from `localStorage` keys `mock_garmin` and `mock_garmin_expired`; `export` with `mock_garmin_expired` set returns `GARMIN_SESSION`. In `mock-backend.js`, add `garminConnected()` and `garminSessionExpired()`. New page object `e2e/pages/garmin-connection.js` (state, button, include switch, `connect({email, password})`). `e2e/garmin.spec.js` covers, with the IDs of `mfp.spec.js`:
- `DT-G1` not connected: Include off and disabled, Pull shows "Connect Garmin first."
- `ST-G1` Not connected → Connected after sign in; the password field is empty afterwards.
- `ST-G2` wrong password keeps the form open with "Wrong email or password."
- `ST-G3` Connected → Not connected on disconnect.
- `ST-G4` Connected → Session expired when a run ends with `GARMIN_SESSION`; reconnect → Connected.
- `ST-G5` Running: the Garmin button and switch are disabled.

Update `export-flow.spec.js`: the fixture scenario starts with `garminConnected()`; `ST-I1` becomes "not connected: stays Ready, asks to connect Garmin"; `ST-T5` and `ST-T7` (rejected login) move to the connect flow in `garmin.spec.js`; `EG-02` (email with spaces) checks the connect form.

- [ ] **Step 7: Run everything.** Run: `cd tauri-app && bun run test:core && bun test sidecar && bunx playwright test && (cd ../electron-app && npm test)` Expected: PASS.

- [ ] **Step 8: Try it in the app (ask the user first).** `bun run sidecar && bun run dev`. The user connects Garmin once with the right password; this is one real sign in. Do not try a wrong password live: every attempt counts against Garmin's limit. Run one export of 7 days. Expected: no password asked, CSV files written.

- [ ] **Step 9: Commit.**

```bash
git add tauri-app/sidecar tauri-app/ui tauri-app/e2e
git commit -m "feat(ui): connect Garmin once; exports run from the saved login without a password"
```

---

### Task 6: clear-cache in Go

**Files:**
- Modify: `cmd/garmin-export-core/main.go`, `cmd/garmin-export-core/main_test.go`, `tauri-app/ui/app.js:216`, `tauri-app/ui/dev-mock.js`
- Modify: `tauri-app/sidecar/cli.js` (delete `clearCache` and its entry in `COMMANDS`)

- [ ] **Step 1: Write the failing test.** In `main_test.go`: create `garmin_cache.db`, `garmin-cache/daily/2026-10-01.json`, `mfp-cache/2026-10-01.json`, `tokens.json` and `mfp_session.json` in a data dir inside `$HOME`; run `clear-cache`; expect `{ok:true}`, the three cache paths gone, and `tokens.json` and `mfp_session.json` still there.

- [ ] **Step 2: Run it.** Expected: FAIL ("Unknown command.").

- [ ] **Step 3: Implement** with `os.Remove` for the file and `os.RemoveAll` for the two folders, after `guard.InsideHome(dataDir)`. Point the UI's Clear cache button at `binaries/garmin-export-core`. Delete Bun's `clearCache`.

- [ ] **Step 4: Run.** Run: `cd tauri-app && bun run test:core && bun test sidecar && bunx playwright test` Expected: PASS.

- [ ] **Step 5: Commit.**

```bash
git add tauri-app
git commit -m "feat(core): clear-cache in Go removes the Garmin and MyFitnessPal caches"
```

---

### Task 7: Include switch per source; export with Garmin off

**Files:**
- Create: `internal/garmin/export.go` (only `PrepareDir` in this task), `internal/garmin/export_test.go`
- Modify: `cmd/garmin-export-core/main.go`, `cmd/garmin-export-core/main_test.go`, `tauri-app/sidecar/cli.js:summary`, `tauri-app/sidecar/e2e.test.js`, `tauri-app/ui/app.js`, `tauri-app/ui/dev-mock.js`, `tauri-app/e2e/garmin.spec.js`, `tauri-app/e2e/mfp.spec.js`

**Interfaces:**
- Produces: `garmin.PrepareDir(outputDir, endDate string) (csvDir string, err error)`; the `export` command with `garmin:false`; Bun `summary` with `{csvDir, startDate, endDate}` and no `jsonPath`.

- [ ] **Step 1: Write the failing Go tests.** `export_test.go`:

```go
func TestPrepareDirMakesTheEndDateFolderAndDropsStaleMfpFiles(t *testing.T) {
	home, _ := os.UserHomeDir()
	out, err := os.MkdirTemp(home, ".garmin-prepare-test-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(out) })
	stale := filepath.Join(out, "csv_2026-10-07")
	os.MkdirAll(stale, 0o755)
	os.WriteFile(filepath.Join(stale, "mfp_food.csv"), []byte("old\n"), 0o644)
	dir, err := PrepareDir(out, "2026-10-07")
	if err != nil || dir != stale {
		t.Fatalf("PrepareDir = %q %v", dir, err)
	}
	if _, err := os.Stat(filepath.Join(stale, "mfp_food.csv")); !os.IsNotExist(err) {
		t.Fatal("stale MFP file kept")
	}
	if _, err := PrepareDir("/tmp", "2026-10-07"); err == nil {
		t.Fatal("a folder outside $HOME was accepted")
	}
}
```

`main_test.go`: `export` with `garmin:false`, a range and an `outputDir` in `$HOME` gives `{ok:true, path:<out>/csv_<end>, jsonPath:null, startDate, endDate}`, and the fake Garmin sees no call. `export` with `garmin:true` gives `{ok:false, errorCode:"GARMIN_ERROR", error:"The Go Garmin export is not ready yet."}` until Task 12 replaces it.

- [ ] **Step 2: Write the failing Bun test.** In `e2e.test.js`:

```js
test('AC-10 summary builds the pack from a csv folder without a Garmin JSON file', async () => {
  const { out } = dirs();
  const csvDir = path.join(out, 'csv_2026-10-07');
  fs.mkdirSync(csvDir);
  fs.writeFileSync(path.join(csvDir, 'mfp_daily.csv'), 'date,logged\n2026-10-07,yes\n');
  const { result } = await sidecar({ cmd: 'summary', csvDir, startDate: '2026-10-01', endDate: '2026-10-07' });
  expect(result.ok).toBe(true);
  expect(result.summary).toBe(null);
  expect(result.pack.files.map((f) => f.name)).toContain('mfp_daily.csv');
}, 60000);
```

Check the shape of `pack.files` in `llm-pack.js:previewPack` before running, and adjust the last line if the field is not `name`.

- [ ] **Step 3: Run them.** Run: `cd tauri-app && bun run test:core && bun test sidecar/e2e.test.js` Expected: FAIL.

- [ ] **Step 4: Implement.**
- `PrepareDir`: `guard.InsideHome(outputDir)`, `os.MkdirAll(filepath.Join(out, "csv_"+endDate), 0o755)`, remove the three MFP files (reuse the list in `internal/mfp/export.go:csvFiles`; export it as `mfp.CSVFiles`).
- `main.go` `export`: validate the range (`startDate`/`endDate`, or `daysBack` with `guard.DaysBack`), and with `garmin:false` call `PrepareDir`.
- Bun `summary`: when `req.jsonPath` is absent, require `csvDir` (through `resolveOutputDir`) and `ISO` dates, and return `{ ok: true, summary: null, pack: previewPack({...}) }`.

- [ ] **Step 5: UI.**
- `usable(src) = src.on && src.connected && !src.expired` for both sources. With neither usable, Pull toasts "Switch on and connect Garmin or MyFitnessPal first." and returns.
- Garmin off: run `export` with `garmin:false` on the Go binary, set the five Garmin rows to the text "Off" with no ring progress, then `runMfp(res.path, {startDate: res.startDate, endDate: res.endDate})`, then `summary` with `{csvDir, startDate, endDate}`. If MFP fails, the run ends as Failed with the MFP error.
- Garmin on: unchanged, through the engine (Bun until Task 13).
- `dev-mock.js`: `export` with `garmin:false` returns the folder at once.
- Playwright: `DT-G2` Garmin off and MFP on: Garmin rows "Off", MFP row "Food logged on 17 of 30 days", LLM pack offered. `DT-G3` both off: stays Ready with the toast. `DT-G4` Garmin off, MFP session expired: run Failed, MFP row "Session expired".

- [ ] **Step 6: Run everything.** Run: `cd tauri-app && bun run test:core && bun test sidecar && bunx playwright test` Expected: PASS.

- [ ] **Step 7: Commit.**

```bash
git add tauri-app
git commit -m "feat: Include switch per source; with Garmin off the Go core makes the csv folder and MFP runs alone"
```

---

### Task 8: jsjson: JavaScript key order, numbers and JSON.stringify

**Files:**
- Create: `internal/jsjson/value.go`, `internal/jsjson/format.go`, `internal/jsjson/jsjson_test.go`

**Interfaces:**
- Produces:

```go
type Kind uint8
const (Undefined Kind = iota; Null; Bool; Number; String; Array; Object)
type Value struct {
	Kind Kind
	B    bool
	N    float64
	S    string
	A    []Value
	O    *Obj
}
type Obj struct {
	Keys []string         // JavaScript property order
	m    map[string]Value
}
func Parse(b []byte) (Value, error)
func NewObj() *Obj
func (o *Obj) Get(k string) Value // Undefined when absent
func (o *Obj) Set(k string, v Value) // keeps the first position, like JS assignment
func (v Value) Get(k string) Value  // Undefined unless v is an object
func (v Value) IsNullish() bool     // Undefined or Null, for ?? and || {}
func Num(f float64) Value; Str(s string) Value
func FormatNumber(f float64) string // String(n)
func Stringify(v Value) string      // JSON.stringify(v), no spaces
func Indent(v Value) []byte         // JSON.stringify(v, null, 2)
func Cell(v Value) string           // String(v) for a CSV cell; "" for Undefined and Null
```

- [ ] **Step 1: Write the failing tests.** `internal/jsjson/jsjson_test.go`. The expected strings come from `node -e 'console.log(String(n))'`; recompute any you add.

```go
package jsjson

import "testing"

func TestFormatNumberMatchesJavaScript(t *testing.T) {
	cases := map[float64]string{
		0: "0", 1: "1", -1: "-1", 72.0: "72", 0.1: "0.1", 1.5: "1.5", 
		1234567: "1234567", 12345678901: "12345678901", 1e-6: "0.000001", 1e-7: "1e-7", 1.5e-7: "1.5e-7",
		1e21: "1e+21", 1.5e21: "1.5e+21", 123456789012345680000: "123456789012345680000", -2.5e-8: "-2.5e-8",
	}
	for f, want := range cases {
		if got := FormatNumber(f); got != want {
			t.Errorf("FormatNumber(%v) = %q, want %q", f, got, want)
		}
	}
	// A runtime sum: 0.1 + 0.2 written as a constant would be folded to exactly 0.3.
	a, b := 0.1, 0.2
	if got := FormatNumber(a + b); got != "0.30000000000000004" {
		t.Errorf("FormatNumber(0.1+0.2) = %q", got)
	}
	negZero := math.Copysign(0, -1)
	if FormatNumber(negZero) != "0" {
		t.Error("-0 must print 0")
	}
}

func TestParseKeepsJavaScriptKeyOrder(t *testing.T) {
	v, err := Parse([]byte(`{"b":1,"a":2,"1001":3,"10":4,"01":5,"4294967295":6,"b":7}`))
	if err != nil {
		t.Fatal(err)
	}
	// Index-like keys first in numeric order; "01" and 2^32-1 are not index-like; a repeated key keeps its first place.
	want := []string{"10", "1001", "b", "a", "01", "4294967295"}
	if fmt.Sprint(v.O.Keys) != fmt.Sprint(want) {
		t.Fatalf("keys %v, want %v", v.O.Keys, want)
	}
	if v.Get("b").N != 7 {
		t.Fatal("the last value of a repeated key wins")
	}
}

func TestParseReadsSourceNumberTextAsFloat(t *testing.T) {
	v, _ := Parse([]byte(`{"x":72.0,"y":1E3}`))
	if Cell(v.Get("x")) != "72" || Cell(v.Get("y")) != "1000" {
		t.Fatalf("got %q %q", Cell(v.Get("x")), Cell(v.Get("y")))
	}
}

func TestStringifyMatchesJavaScript(t *testing.T) {
	v, _ := Parse([]byte("{\"z\":[1,null,true,\"a\\\"b\"],\"t\":\"<&> \\u0001\\n\",\"n\":{\"2\":0.5,\"k\":-0}}"))
	want := "{\"z\":[1,null,true,\"a\\\"b\"],\"t\":\"<&> \\u0001\\n\",\"n\":{\"2\":0.5,\"k\":0}}"
	if got := Stringify(v); got != want {
		t.Fatalf("got  %s\nwant %s", got, want)
	}
}

func TestCellMatchesJavaScriptString(t *testing.T) {
	arr, _ := Parse([]byte(`[1,null,"a",[2,3]]`))
	obj, _ := Parse([]byte(`{"a":1}`))
	cases := []struct {
		v    Value
		want string
	}{
		{Value{}, ""}, {Value{Kind: Null}, ""}, {Value{Kind: Bool, B: true}, "true"}, {Value{Kind: Bool}, "false"},
		{Str("x"), "x"}, {Num(3), "3"}, {arr, "1,,a,2,3"}, {obj, "[object Object]"},
	}
	for _, c := range cases {
		if got := Cell(c.v); got != c.want {
			t.Errorf("Cell = %q, want %q", got, c.want)
		}
	}
}
```

Add the imports `fmt` and `math`.

- [ ] **Step 2: Run them.** Run: `cd tauri-app/core && go test ./internal/jsjson` Expected: FAIL.

- [ ] **Step 3: Implement.** The two parts that need care:

```go
// FormatNumber prints f as JavaScript's String(n) does.
func FormatNumber(f float64) string {
	if f == 0 {
		return "0" // also -0
	}
	if a := math.Abs(f); a >= 1e-6 && a < 1e21 {
		return strconv.FormatFloat(f, 'f', -1, 64)
	}
	s := strconv.FormatFloat(f, 'e', -1, 64) // "1.5e-07"
	mant, exp, _ := strings.Cut(s, "e")
	return mant + "e" + exp[:1] + strings.TrimLeft(exp[1:], "0")
}

// isIndex reports whether k is a canonical array index, which JavaScript orders first.
func isIndex(k string) bool {
	if k == "0" {
		return true
	}
	if k == "" || k[0] < '1' || k[0] > '9' {
		return false
	}
	n, err := strconv.ParseUint(k, 10, 64)
	return err == nil && n < 1<<32-1
}
```

`Parse` walks `json.NewDecoder(bytes.NewReader(b))` with `UseNumber()` and `Token()`; numbers go through `strconv.ParseFloat(string(num), 64)`. After an object closes, stable sort `Keys` so index-like keys come first in numeric order. `Stringify` escapes only `"`, `\`, and runes below 0x20 (`\b`, `\f`, `\n`, `\r`, `\t`, else `\u00xx`), and writes lone surrogates of invalid UTF-8 as `�` (Go strings cannot hold them; note this in the code with a `ponytail:` comment). `Indent` is the same walk with newlines and two spaces.

- [ ] **Step 4: Run them.** Run: `cd tauri-app/core && go test ./internal/jsjson` Expected: PASS.

- [ ] **Step 5: Commit.**

```bash
git add tauri-app/core/internal/jsjson
git commit -m "feat(core): jsjson keeps JavaScript key order and prints numbers and JSON like JavaScript"
```

---

### Task 9: CSV writer, byte equal to the golden files

**Files:**
- Create: `internal/garmin/csv.go`, `internal/garmin/csv_test.go`, `internal/garmin/testdata/weight_bp/` (input JSON and expected CSV files)

**Interfaces:**
- Consumes: `jsjson` (Task 8).
- Produces: `WriteCSVs(export jsjson.Value, outputDir string) (csvDir string, written []string, err error)`; `csvDir` is `outputDir/csv_<export_date>`.

- [ ] **Step 1: Write the failing golden test.** `internal/garmin/csv_test.go`:

```go
func TestAC01CSVFilesEqualTheGoldenFilesByteForByte(t *testing.T) {
	raw, err := os.ReadFile(fixturePath)
	if err != nil {
		t.Fatal(err)
	}
	export, err := jsjson.Parse(raw)
	if err != nil {
		t.Fatal(err)
	}
	csvDir, written, err := WriteCSVs(export, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	golden, _ := os.ReadDir(goldenDir)
	if len(written) != len(golden) || len(golden) != 18 {
		t.Fatalf("wrote %v, golden has %d files", written, len(golden))
	}
	for _, g := range golden {
		want, _ := os.ReadFile(filepath.Join(goldenDir, g.Name()))
		got, err := os.ReadFile(filepath.Join(csvDir, g.Name()))
		if err != nil {
			t.Fatalf("%s not written", g.Name())
		}
		if !bytes.Equal(got, want) {
			t.Errorf("%s differs from the golden file; first difference at byte %d", g.Name(), firstDiff(got, want))
		}
	}
}
```

Add `firstDiff` (index of the first differing byte), and AC-02: the columns listed in `regression.test.js:46-51` have at least one non empty cell, using `encoding/csv` to read the Go output.

For `weight_body_comp.csv` and `blood_pressure.csv`, which the fixture does not produce, add `TestWeightAndBloodPressureMatchJavaScript`: a small hand made export in `testdata/weight_bp/export.json` with two `body_composition.dateWeightList` entries (one with nested objects and an array), one `blood_pressure.measurementSummaries` entry, and an index-like key. Generate the expected files once with JavaScript and commit them:

```bash
node -e "const {generateCsvs}=require('./electron-app/garmin/csv-writer');const fs=require('fs');const r=generateCsvs(JSON.parse(fs.readFileSync('tauri-app/core/internal/garmin/testdata/weight_bp/export.json','utf8')),'/tmp/wbp');console.log(r.written)"
cp /tmp/wbp/csv_*/weight_body_comp.csv /tmp/wbp/csv_*/blood_pressure.csv tauri-app/core/internal/garmin/testdata/weight_bp/
```

- [ ] **Step 2: Run them.** Run: `cd tauri-app/core && go test ./internal/garmin -run 'AC0|Weight'` Expected: FAIL.

- [ ] **Step 3: Implement** by porting `csv-writer.js` function by function, in its order, with one Go function per JS function and the JS line range in a comment above it:
- `groupFor` (lines 9-20), `flatten` (31-48), `formatCsvField` (55-62, using `jsjson.Cell`), `writeCsv` (69-100).
- the extractors (106-404), `SUMMARY_FIELDS` (411-420), `extractGroupSummary` (426-438), `extractGroupLaps` (444-453), `flattenLaps` (497-517), and `generateCsvs` (525-583).
- Do not port `flattenDetails` (459-491): nothing calls it.
- Rows are `*jsjson.Obj`, so the header order is the JavaScript order.
- Port these exactly, because they decide which columns exist: `typeof steps === 'object' && !Array.isArray(steps)` in `extractDailySummary`; `Array.isArray(trRaw) && trRaw.length ? last : (object ? it : {})` in `extractTraining`; `x !== undefined ? x : null` versus `x ?? null` (both give an empty cell); `(d.body_battery || [])` iterated with `for...of` (a non array object would throw in JavaScript; in Go treat it as an empty list and add a `ponytail:` comment that names the JS throw).
- Iterate `daily` through `export.Get("daily").O.Keys`, never through a map.

- [ ] **Step 4: Run them.** Run: `cd tauri-app/core && go test ./internal/garmin -run 'AC0|Weight'` Expected: PASS. If a file differs, print both lines around `firstDiff` and fix the port, never the golden file.

- [ ] **Step 5: Commit.**

```bash
git add tauri-app/core/internal/garmin
git commit -m "feat(core): Garmin CSV writer in Go, byte equal to the 18 golden files"
```

---

### Task 10: Endpoint table and HTTP client

**Files:**
- Create: `internal/garmin/endpoints.go`, `internal/garmin/endpoints_test.go`, `internal/garmin/redact.go`, `internal/garmin/client.go`, `internal/garmin/client_test.go`
- Modify: `internal/garmin/garmintest/server.go` (data routes from the fixture)

**Interfaces:**
- Consumes: `Auth.Fresh`, `Auth.Refresh`, `Headers`, `MarkExpired` (Tasks 2 and 3).
- Produces:

```go
type Endpoint struct {
	Name, Type, AliasOf string // Type: daily, aggregated, activity, list
	Header                http.Header
	URL                   func(base string, p Params) string
}
type Params struct{ Date, Start, End, DisplayName string; ActivityID string; Offset, Limit int }
var Daily, Aggregated []Endpoint
func Lookup(name string) (Endpoint, bool)

type Result struct {
	Data   json.RawMessage // nil for 204 or an empty body
	Status int
	Code   string // "" on success, else EMPTY_BODY, BAD_JSON, TIMEOUT, NETWORK, AUTH, FORBIDDEN, RATE_LIMIT, HTTP_4XX, HTTP_5XX
	Err    string
}
type Client struct {
	Auth      *Auth
	Gap       time.Duration // 500 ms; 0 only with the fake server
	Sleep     func(time.Duration)
	Jitter    func() time.Duration
	Log       func(kind, msg, code string)
	Stop      string // set when the run must stop; the stop reason
	StopCode  string // GARMIN_SESSION, GARMIN_RATE_LIMITED or GARMIN_BLOCKED
}
func (c *Client) Get(ctx context.Context, ep Endpoint, p Params) Result
func (c *Client) Safe(ctx context.Context, name string, p Params) json.RawMessage // logs [skip] and [no data]
```

- [ ] **Step 1: Write the failing tests.**
- `endpoints_test.go`: for each endpoint, the URL equals the URL that `endpoints.js` builds for the same params. Build the table once with `node -e` over `allEndpoints` (date `2026-10-07`, range `2026-10-01`..`2026-10-07`, displayName `dn`, activityId `42`, base `http://127.0.0.1:1`) and paste it into the test as a `map[string]string`. Leave out `activity_details` and `activity_split_summaries`. Check that `user_summary`, `all_day_stress` and `morning_training_readiness` have `AliasOf` set, and that `goals` carries `Sec-Fetch-Site: same-origin`.
- `client_test.go`, one test per ID from `integration.test.js` and `client.test.js`:
  - `EP-01` 204 is success with no data, logged `[no data] hrv (HTTP 204)` as `dim`, no `[skip]`.
  - `EP-02` one 401 refreshes and retries; `tokens.json` keeps a login.
  - `EP-03` 401 again after a refresh: `StopCode == "GARMIN_SESSION"` and `tokens.json` is the expired marker.
  - `EP-04` one 403 skips that endpoint; tokens unchanged; the run goes on.
  - `BVA-01` four 403 in a row do not stop. `BVA-02` the fifth stops with `GARMIN_BLOCKED`.
  - `BVA-03` a 429 that clears after 2 retries succeeds and waits `Retry-After` each time (record `Sleep` calls). `BVA-04` a third 429 stops with `GARMIN_RATE_LIMITED`.
  - `EG-01` `Retry-After` as an HTTP date is honored and capped at 60 s.
  - `EG-08` 403 on `social_profile` stops at once.
  - `EG-09` (new) DI requests carry every `NATIVE_HEADERS` header; legacy requests carry the mobile User-Agent.
  - `EG-10` (new) a 500 body with `Bearer abc.def.ghi` and `"password":"x"` comes out redacted in the `[skip]` line, cut to 200 characters.
  - `EG-11` (new) with `GARMIN_FAKE_SERVER` unset, `Client` refuses `Gap == 0` and uses 500 ms.
  - `TIMEOUT` and `NETWORK`: 5 retries with backoff `min(5000*2^attempt + jitter, 60000)` ms; inject `Sleep` and `Jitter` so the test takes no real time.

- [ ] **Step 2: Run them.** Run: `cd tauri-app/core && go test ./internal/garmin -run 'Endpoint|EP_|BVA|EG_|Client'` Expected: FAIL.

- [ ] **Step 3: Data routes in the fake.** Port `fake-garmin.js:routesFor` and lines 104-118: build the route map from the fixture with the Go `Endpoint.URL` (so the fake and the client agree on URLs), answer `socialProfile`, the paged activity list without `splits`, the steps chunks, goals, and 204 for a null answer.

- [ ] **Step 4: Implement** `endpoints.go` from `endpoints.js`, `redact.go` from `electron-app/util/redact.js` (`redactUrl` and `redactString` only), and `client.go` from `client.js:151-428`, with these changes from the JavaScript:
- a second 401 calls `MarkExpired` instead of `clearTokens`, and sets `StopCode = "GARMIN_SESSION"`;
- a refresh that fails with a plain error (not `ErrSessionDead`) stops the run with `GARMIN_ERROR` and keeps the tokens;
- `Safe` logs through `Log("warn", msg, code)` so the warn line carries `errorCode`.

- [ ] **Step 5: Run them.** Run: `cd tauri-app && bun run test:core` Expected: PASS.

- [ ] **Step 6: Commit.**

```bash
git add tauri-app/core/internal/garmin
git commit -m "feat(core): Garmin endpoint table and HTTP client with the 401, 403, 429 and 204 rules"
```

---

### Task 11: Per day JSON cache

**Files:**
- Create: `internal/garmin/cache.go`, `internal/garmin/cache_test.go`

**Interfaces:**
- Produces:

```go
const FinalAfterDays = 7
type Cache struct{ Dir string } // dataDir/garmin-cache
func (c Cache) Day(date string) map[string]Entry                 // endpoint -> entry; empty map on a miss or a corrupt file
func (c Cache) PutDay(date string, entries map[string]Entry) error // atomicfile
func (c Cache) Splits(activityID string) (json.RawMessage, bool)
func (c Cache) PutSplits(activityID string, data json.RawMessage) error
type Entry struct {
	FetchedAt string          `json:"fetched_at"`
	Data      json.RawMessage `json:"data"`
}
func Trust(e Entry, date string) bool                          // fetched 7 or more days after the day, data not null
func InRefreshWindow(date string, refreshDays int, now time.Time) bool // calendar days, time.Local
```

- [ ] **Step 1: Write the failing tests.**
- `BVA-05` (from `integration.test.js:337`): `Trust` is false at 6 days 23 hours after the day and true at exactly 7 days; false for `null` data.
- `BVA-06` (new): `InRefreshWindow` with `refreshDays = 3` and today 2026-10-15: true for 10-15, 10-14, 10-13; false for 10-12.
- `EG-12` (new): a corrupt day file is a miss, not an error; `PutDay` leaves no temporary file behind; the file mode is 0600; the raw data comes back byte for byte (key order and `72.0` survive).

- [ ] **Step 2: Run them.** Expected: FAIL.

- [ ] **Step 3: Implement.** Files at `garmin-cache/daily/<date>.json` and `garmin-cache/activity/<id>.json`. Validate `activityID` against `^\d+$` before using it in a path. `InRefreshWindow` compares `time.Date(y, m, d, 0, 0, 0, 0, time.Local)` values and counts calendar days.

- [ ] **Step 4: Run them.** Run: `cd tauri-app/core && go test ./internal/garmin -run 'BVA_05|BVA_06|EG_12|Cache'` Expected: PASS.

- [ ] **Step 5: Commit.**

```bash
git add tauri-app/core/internal/garmin
git commit -m "feat(core): Garmin per day JSON cache with the 7 day final rule"
```

---

### Task 12: The Go export command

**Files:**
- Modify: `internal/garmin/export.go`, `internal/garmin/export_test.go`, `cmd/garmin-export-core/main.go`, `cmd/garmin-export-core/main_test.go`
- Create: `tauri-app/sidecar/garmin-e2e.test.js`

**Interfaces:**
- Consumes: everything above.
- Produces: `Export(ctx, req map[string]any, out *protocol.Emitter, opts Options) map[string]any` with the result of the spec; `Options{Gap time.Duration; Now func() time.Time}`.

- [ ] **Step 1: Write the failing Go tests.** `export_test.go`, against `garmintest.New(fixture)` with tokens from a `garmin-connect` style login written to a data dir in `$HOME`:
- `AC-03` a full export of the fixture range writes CSV files equal to the golden files, and a JSON file whose `daily` keys are newest first and whose `activities[*].splits` exist.
- `EG-02` the alias endpoints are requested once per day, and `split_summaries` never.
- `EG-03` goals are requested with `start=1` and `Sec-Fetch-Site: same-origin`.
- `EG-04` a second export of old days reads the cache; only the 204 days are asked again. Set `Options.Now` to 30 days after the fixture's end so every day is final.
- `EG-05` a persistent 429 stops with `GARMIN_RATE_LIMITED`, and the finished days are on disk.
- `EG-07` a 429 on the activity list stops instead of reporting success without activities.
- `EG-13` (new) no `tokens.json`: `GARMIN_SESSION` "Connect Garmin first.", no request. The expired marker: `GARMIN_SESSION` "The Garmin session expired. Reconnect Garmin.", no request.
- `EG-14` (new) the log lines the UI parses appear in order: "Using the saved Garmin login", "Pulling 5 days", "Pulling aggregated metrics...", "Pulling activities...", "Found N activities", "JSON saved: ", "CSVs written to ", "Done."; progress phases `daily`, `aggregated`, `activities`, `csv` in that order; no line holds the display name.
- `EG-15` (new) stale `mfp_*.csv` files in the csv folder are deleted.

`main_test.go`: the binary runs `garmin-connect` then `export` against `garmintest` and gets `ok:true` with the golden files.

- [ ] **Step 2: Write the failing Bun test.** `tauri-app/sidecar/garmin-e2e.test.js`:

```js
// End to end, cross engine: the Go binary against the JavaScript fake Garmin that the Bun
// end to end test uses. The CSV files must equal the golden files, and tokens.json must
// work in both engines.
import { test, expect, beforeAll, afterAll } from 'bun:test';
import fs from 'node:fs';
import os from 'node:os';
import path from 'node:path';
import { createRequire } from 'node:module';

const require = createRequire(import.meta.url);
const T = path.resolve(import.meta.dir, '../../electron-app/garmin/__tests__');
const { startFakeGarmin } = require(path.join(T, 'helpers/fake-garmin.js'));
const { compareDirs } = require(path.resolve(import.meta.dir, '../../electron-app/scripts/compare-csv.js'));
const auth = require(path.resolve(import.meta.dir, '../../electron-app/garmin/auth.js'));
const FIXTURE = JSON.parse(fs.readFileSync(path.join(T, 'fixtures/export-sample.json'), 'utf8'));
const CORE = path.resolve(import.meta.dir, '../core');
const BIN = path.join(os.tmpdir(), `garmin-export-core-garmin-e2e-${process.pid}`);
let fake;
const cleanup = [];

beforeAll(async () => {
  const build = Bun.spawnSync(['go', 'build', '-o', BIN, './cmd/garmin-export-core'], { cwd: CORE });
  if (build.exitCode !== 0) throw new Error(String(build.stderr));
  fake = await startFakeGarmin(FIXTURE);
});
afterAll(async () => { await fake.close(); fs.rmSync(BIN, { force: true }); for (const d of cleanup) fs.rmSync(d, { recursive: true, force: true }); });

async function core(req) {
  const p = Bun.spawn([BIN], { stdin: 'pipe', stdout: 'pipe', env: { ...process.env, GARMIN_FAKE_SERVER: fake.base } });
  p.stdin.write(JSON.stringify(req) + '\n');
  p.stdin.end();
  const lines = (await new Response(p.stdout).text()).trim().split('\n').map((l) => JSON.parse(l));
  return { result: lines.find((l) => l.kind === 'result'), logs: lines.filter((l) => l.kind === 'log') };
}

function dirs() {
  // The Go core only accepts paths inside $HOME, the data dir included.
  const root = fs.mkdtempSync(path.join(os.homedir(), '.garmin-go-e2e-'));
  cleanup.push(root);
  const out = path.join(root, 'out'), data = path.join(root, 'data');
  fs.mkdirSync(out); fs.mkdirSync(data);
  return { out, data };
}

const range = { startDate: FIXTURE.date_range.start, endDate: FIXTURE.date_range.end };

test('AC-11 Go connects once and exports CSV files equal to the golden files', async () => {
  const { out, data } = dirs();
  const c = await core({ cmd: 'garmin-connect', email: 'e2e@example.invalid', password: 'right-password', dataDir: data });
  expect(c.result).toMatchObject({ ok: true, connected: true, kind: 'di' });
  const { result, logs } = await core({ cmd: 'export', garmin: true, ...range, outputDir: out, dataDir: data, refreshWindow: 3 });
  expect(result.ok).toBe(true);
  expect(compareDirs(path.join(T, 'golden'), result.path)).toEqual({ addedFiles: [], removedFiles: [], files: {} });
  for (const f of fs.readdirSync(path.join(T, 'golden'))) {
    expect(fs.readFileSync(path.join(result.path, f)).equals(fs.readFileSync(path.join(T, 'golden', f)))).toBe(true);
  }
  expect(logs.filter((l) => l.type === 'warn')).toEqual([]);
}, 120000);

test('AC-12 a tokens.json written by Go loads in Bun, and one written by Bun loads in Go', async () => {
  const { data } = dirs();
  await core({ cmd: 'garmin-connect', email: 'e2e@example.invalid', password: 'right-password', dataDir: data });
  const loaded = await auth.createClient(data).loadTokens();
  expect(loaded.ok).toBe(true);
  fs.rmSync(path.join(data, 'tokens.json'));
  process.env.GARMIN_FAKE_SERVER = fake.base;
  try {
    expect((await auth.createClient(data).login('e2e@example.invalid', 'right-password')).ok).toBe(true);
  } finally {
    delete process.env.GARMIN_FAKE_SERVER;
  }
  const { result } = await core({ cmd: 'garmin-status', dataDir: data });
  expect(result).toMatchObject({ ok: true, connected: true, expired: false, kind: 'di' });
}, 120000);
```

- [ ] **Step 3: Run them.** Run: `cd tauri-app && bun run test:core && bun test sidecar/garmin-e2e.test.js` Expected: FAIL.

- [ ] **Step 4: Implement `Export`** by porting `exporter.js:exportHealth` (lines 138-452) in the same order: input checks; `Auth.Fresh` (map `ErrNoLogin` and the marker to `GARMIN_SESSION`, `ErrSessionDead` to the marker plus `GARMIN_SESSION`); the display name; the daily loop over the dates newest first with aliases, the refresh window, `Trust`, and a `PutDay` after each day; the aggregated endpoints with the 28 day `daily_steps` chunks (`exporter.js:50-77`); the paged activity list (20 per page); splits with the cache; goals; the JSON file through `jsjson.Indent`; `WriteCSVs`; the stale MFP files. Build `daily`, `aggregated` and each activity as `*jsjson.Obj` so that `WriteCSVs` gets the same value as in Task 9. Stop with `Client.StopCode` after each step, as the JavaScript checks `client.authFailed || client.rateLimited`. Replace the `GARMIN_ERROR` stub in `main.go` with `garmin.Export`.

- [ ] **Step 5: Run everything.** Run: `cd tauri-app && bun run test:core && bun test sidecar && (cd ../electron-app && npm test)` Expected: PASS.

- [ ] **Step 6: Commit.**

```bash
git add tauri-app/core tauri-app/sidecar/garmin-e2e.test.js
git commit -m "feat(core): Garmin export in Go; golden CSV files and the cross engine test pass"
```

---

### Task 13: Go as the default engine, docs and the full check

**Files:**
- Modify: `tauri-app/ui/app.js`, `tauri-app/ui/dev-mock.js`, `README.md`, `CLAUDE.md`, `tauri-app/package.json` (version)

- [ ] **Step 1: The engine switch.** In `app.js`, add `const DEFAULT_ENGINE = 'go';` and `const engine = () => store.get('engine', DEFAULT_ENGINE);`. `startExport` runs `export` on `engine() === 'go' ? 'binaries/garmin-export-core' : 'binaries/garmin-core'`. With the Go engine, the result has no `summary` and no `pack`, so call `summary` with `{ jsonPath: res.jsonPath, csvDir: res.path }` after the MFP step, as `runMfp` already does, and use `res.startDate` and `res.endDate` for the MFP range. The csv row's file count comes from `res.pack.files.length` today (`app.js:389`); with the Go engine take it from `res.files.length`, because the pack arrives later from `summary`. Commit this step first with `DEFAULT_ENGINE = 'bun'`, run Playwright with `localStorage.ge_engine = 'go'` in the mock, and only then change the default to `'go'` in a separate commit.

- [ ] **Step 2: Compare one real export (ask the user first).** With the app closed, the user's account, and the last 14 days: run the Bun export once and the Go export once into two output folders, then `node electron-app/scripts/compare-csv.js <bun csv dir> <go csv dir>`. Expected: no differences, except rows for today, which can change between the two runs. The Go run downloads everything once, because it cannot read the sql.js cache; tell the user it takes about 3 minutes for 14 days.

- [ ] **Step 3: README.** Change the Garmin section: you connect Garmin once in the Setup card; the app sends your password to Garmin once and saves a login token, not the password; when Garmin ends the login, the row says "Session expired" and you reconnect; each source has an Include switch; with Garmin off, MyFitnessPal runs alone. Unlike MyFitnessPal, the app does see the Garmin password once, because Garmin's sign in is an API, not a web page.

- [ ] **Step 4: CLAUDE.md.** Update the Go core bullet: stage 2 commands (`garmin-status`, `garmin-connect`, `garmin-disconnect`, `clear-cache`, `export`), the `garmin-cache/` files, the expired marker, `GARMIN_FAKE_SERVER` read in Go too, the `ge_engine` switch, and that Go reads the goldens from `electron-app/garmin/__tests__` until stage 3. Update the Garmin login bullet: only `garmin-connect` signs in.

- [ ] **Step 5: Full check.** Run, in order: `cd tauri-app && bun run test:core && bun test sidecar && bunx playwright test && (cd ../electron-app && npm test) && (cd src-tauri && cargo test)`. Expected: all pass. Build the universal app in a separate worktree (never in the folder the user runs the app from), and install it only after asking the user.

- [ ] **Step 6: Commit.**

```bash
git add tauri-app/ui README.md CLAUDE.md tauri-app/package.json
git commit -m "feat: the Go core is the default Garmin engine; docs for saved logins and source switches"
```
