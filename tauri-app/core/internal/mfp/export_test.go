package mfp

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/danihrndzld/garmin-health-exporter/tauri-app/core/internal/mfp/mfptest"
)

const (
	msgNoSession  = "Connect MyFitnessPal first."
	msgExpired    = "The MyFitnessPal session expired. Reconnect MyFitnessPal."
	msgUnexpected = "MyFitnessPal did not answer as expected. See the log for details."
)

// seedFinal stores a day as the cache would after a run long after that date.
func seedFinal(t *testing.T, e *exportEnv, date string, items []DiaryItem) {
	t.Helper()
	day, _ := time.ParseInLocation("2006-01-02", date, time.Local)
	if err := (Cache{Dir: filepath.Join(e.dataDir, "mfp-cache")}).Put(date, items, day.Add(8*24*time.Hour)); err != nil {
		t.Fatal(err)
	}
}

func fixtureItems(t *testing.T, date string) []DiaryItem {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("testdata", "diary", date+".json"))
	if err != nil {
		t.Fatal(err)
	}
	var out struct{ Items []DiaryItem }
	if err := json.Unmarshal(b, &out); err != nil {
		t.Fatal(err)
	}
	return out.Items
}

// logHas reports whether a log line of the run contains sub.
func logHas(lines []map[string]any, sub string) bool {
	for _, l := range lines {
		if m, _ := l["msg"].(string); l["kind"] == "log" && strings.Contains(m, sub) {
			return true
		}
	}
	return false
}

func csvPresent(e *exportEnv) (n int) {
	for _, f := range csvNames {
		if exists(filepath.Join(e.csvDir, f)) {
			n++
		}
	}
	return
}

// Decision table for mfp-export. Conditions: C1 session file valid, C2 auth_token answers JSON,
// C3 MFP answers 429, C4 every day of the range already final in the cache, C5 goals valid.
func TestExportDecisionTable(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name       string
		noSession  bool
		setup      func(t *testing.T, e *exportEnv)
		start, end string
		wantOK     bool
		wantCode   string
		wantMsg    string
		wantLog    string // in a log line, not in the error the UI shows
		diaryCalls int
		authCalls  int
	}{
		{name: "DT-R107 C1 no, so MFP_SESSION asks to connect and no request is made", noSession: true,
			start: "2026-10-04", end: "2026-10-07", wantCode: "MFP_SESSION", wantMsg: msgNoSession},
		{name: "DT-R108 C1 yes and C2 no (HTML), so MFP_SESSION asks to reconnect after one request",
			setup: func(t *testing.T, e *exportEnv) { e.srv.AuthHTML = true }, start: "2026-10-04", end: "2026-10-07",
			wantCode: "MFP_SESSION", wantMsg: msgExpired, authCalls: 1},
		{name: "DT-R109 C1 and C2 yes, C3 no, nothing cached, so every day is fetched and the files are written",
			start: "2026-10-04", end: "2026-10-07", wantOK: true, diaryCalls: 4, authCalls: 1},
		{name: "DT-R110 C1 and C2 yes, C3 yes on the diary, nothing cached, so MFP_RATE_LIMITED after one request",
			setup: func(t *testing.T, e *exportEnv) { e.srv.Diary429 = true }, start: "2026-10-04", end: "2026-10-07",
			wantCode: "MFP_RATE_LIMITED", diaryCalls: 1, authCalls: 1},
		{name: "DT-R111 C3 yes but C4 yes, so the cache answers and the 429 is never reached",
			setup: func(t *testing.T, e *exportEnv) {
				e.srv.Diary429 = true
				seedFinal(t, e, "2026-10-05", fixtureItems(t, "2026-10-05"))
			}, start: "2026-10-05", end: "2026-10-05", wantOK: true, authCalls: 1},
		{name: "DT-R112 C3 yes and only part of the range cached, so the first missing day hits the 429",
			setup: func(t *testing.T, e *exportEnv) {
				e.srv.Diary429 = true
				seedFinal(t, e, "2026-10-05", fixtureItems(t, "2026-10-05"))
			}, start: "2026-10-05", end: "2026-10-06", wantCode: "MFP_RATE_LIMITED", diaryCalls: 1, authCalls: 1},
		{name: "DT-R113 C2 answers HTTP 403 (a bot challenge), so MFP_ERROR, the status only in the log, and not MFP_SESSION",
			setup: func(t *testing.T, e *exportEnv) { e.srv.AuthStatus = 403 }, start: "2026-10-04", end: "2026-10-07",
			wantCode: "MFP_ERROR", wantMsg: msgUnexpected, wantLog: "403", authCalls: 1},
		{name: "DT-R114 C3 yes on auth_token, so MFP_RATE_LIMITED before any diary request",
			setup: func(t *testing.T, e *exportEnv) { e.srv.AuthStatus = 429 }, start: "2026-10-04", end: "2026-10-07",
			wantCode: "MFP_RATE_LIMITED", authCalls: 1},
		{name: "DT-R115 C5 no (goals are not JSON), so the export still succeeds with empty goals",
			setup: func(t *testing.T, e *exportEnv) {
				dir := t.TempDir()
				if err := os.CopyFS(dir, os.DirFS("testdata")); err != nil {
					t.Fatal(err)
				}
				os.WriteFile(filepath.Join(dir, "goals.json"), []byte("{not json"), 0o644)
				e.srv.Close()
				e.srv = mfptest.NewServer(dir)
				t.Cleanup(e.srv.Close)
			}, start: "2026-10-04", end: "2026-10-07", wantOK: true, diaryCalls: 4, authCalls: 1},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			e := newExportEnv(t)
			if tc.noSession {
				os.Remove(e.sessPath)
			}
			if tc.setup != nil {
				tc.setup(t, e)
			}
			for _, f := range csvNames { // a previous run's files
				os.WriteFile(filepath.Join(e.csvDir, f), []byte("old\n"), 0o644)
			}
			res, lines := e.run(t, e.req(tc.start, tc.end))
			if tc.wantOK != (res["ok"] == true) || (!tc.wantOK && res["errorCode"] != tc.wantCode) {
				t.Fatalf("result = %v", res)
			}
			if tc.wantMsg != "" && !strings.Contains(res["error"].(string), tc.wantMsg) {
				t.Errorf("error = %q, want it to contain %q", res["error"], tc.wantMsg)
			}
			if tc.wantLog != "" && !logHas(lines, tc.wantLog) {
				t.Errorf("no log line contains %q: %v", tc.wantLog, lines)
			}
			if got := e.srv.CallCount("/v2/diary?"); got != tc.diaryCalls {
				t.Errorf("diary calls = %d, want %d", got, tc.diaryCalls)
			}
			if got := e.srv.CallCount("/user/auth_token"); got != tc.authCalls {
				t.Errorf("auth_token calls = %d, want %d", got, tc.authCalls)
			}
			if tc.wantOK {
				for _, f := range csvNames {
					if b, _ := os.ReadFile(filepath.Join(e.csvDir, f)); bytes.Equal(b, []byte("old\n")) {
						t.Errorf("%s was not rewritten", f)
					}
				}
			} else if n := csvPresent(e); n != 0 {
				t.Errorf("%d stale CSV files survived a failed run", n)
			}
			for _, l := range lines { // every line of a run follows the protocol
				if k := l["kind"]; k != "log" && k != "progress" {
					t.Errorf("unexpected protocol line %v", l)
				}
			}
		})
	}
}

func TestExportInputValidation(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		set  map[string]any // overrides on a valid request
		del  string         // key removed from a valid request
		code string
	}{
		{name: "BVA-120 a range of 91 days is refused before any request", set: map[string]any{"startDate": "2026-07-16", "endDate": "2026-10-14"}, code: "MFP_INPUT"},
		{name: "EP-157 an end date before the start date is refused", set: map[string]any{"startDate": "2026-10-07", "endDate": "2026-10-04"}, code: "MFP_INPUT"},
		{name: "EP-158 a malformed date is refused", set: map[string]any{"startDate": "04/10/2026"}, code: "MFP_INPUT"},
		{name: "EP-159 a csvDir outside HOME is refused", set: map[string]any{"csvDir": "/tmp"}, code: "MFP_INPUT"},
		{name: "EP-160 a dataDir outside HOME is refused", set: map[string]any{"dataDir": "/etc"}, code: "MFP_INPUT"},
		{name: "EG-124 a date sent as a number instead of a string is refused", set: map[string]any{"startDate": 20261004}, code: "MFP_INPUT"},
		{name: "EG-125 a request without dataDir is refused", del: "dataDir", code: "MFP_INPUT"},
		{name: "EG-126 a request without csvDir is refused", del: "csvDir", code: "MFP_INPUT"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			e := newExportEnv(t)
			req := e.req("2026-10-04", "2026-10-07")
			for k, v := range tc.set {
				req[k] = v
			}
			if tc.del != "" {
				delete(req, tc.del)
			}
			res, _ := e.run(t, req)
			if res["ok"] != false || res["errorCode"] != tc.code {
				t.Fatalf("result = %v", res)
			}
			if n := e.srv.CallCount("/"); n != 0 {
				t.Errorf("a refused request still made %d HTTP calls", n)
			}
		})
	}
	t.Run("EP-161 a dataDir that is a symlink out of HOME is refused", func(t *testing.T) {
		t.Parallel()
		e := newExportEnv(t)
		link := filepath.Join(filepath.Dir(e.dataDir), "escape")
		if err := os.Symlink("/etc", link); err != nil {
			t.Fatal(err)
		}
		req := e.req("2026-10-04", "2026-10-07")
		req["dataDir"] = link
		if res, _ := e.run(t, req); res["errorCode"] != "MFP_INPUT" {
			t.Fatalf("result = %v", res)
		}
	})
}

func TestExportWholeRange(t *testing.T) {
	t.Parallel()
	e := newExportEnv(t)
	res, lines := e.run(t, e.req("2026-10-04", "2026-10-07"))
	t.Run("CL-128 a good run answers ok with days, loggedDays, csvDir and the three file names", func(t *testing.T) {
		files, _ := res["files"].([]string)
		if res["ok"] != true || res["days"] != 4 || res["loggedDays"] != 2 || res["csvDir"] == "" || len(files) != 3 {
			t.Fatalf("result = %v", res)
		}
	})
	t.Run("CL-129 the run emits one progress line per day, ending at total", func(t *testing.T) {
		n, last := 0, map[string]any{}
		for _, l := range lines {
			if l["kind"] == "progress" {
				n++
				last = l
			}
		}
		if n != 4 || last["phase"] != "mfp" || last["current"] != float64(4) || last["total"] != float64(4) {
			t.Fatalf("progress lines = %d, last = %v", n, last)
		}
	})
	t.Run("CL-130 the run ends with a success log that counts logged days and cache use", func(t *testing.T) {
		last := lines[len(lines)-1]
		msg, _ := last["msg"].(string)
		if last["kind"] != "log" || last["type"] != "success" || !strings.Contains(msg, "2 of 4 days") || !strings.Contains(msg, "4 downloaded, 0 from the cache") {
			t.Fatalf("last line = %v", last)
		}
	})
	t.Run("CL-131 the data dir holds only mfp_session.json and mfp-cache, never garmin_cache.db", func(t *testing.T) {
		entries, _ := os.ReadDir(e.dataDir)
		var names []string
		for _, en := range entries {
			names = append(names, en.Name())
		}
		if strings.Join(names, ",") != "mfp-cache,mfp_session.json" {
			t.Fatalf("data dir = %v", names)
		}
	})
	t.Run("BVA-121 a range of 90 days is exported in full", func(t *testing.T) {
		t.Parallel()
		e := newExportEnv(t)
		res, lines := e.run(t, e.req("2026-07-17", "2026-10-14"))
		if res["ok"] != true || res["days"] != 90 {
			t.Fatalf("result = %v", res)
		}
		if rows := readCSVMaps(t, filepath.Join(e.csvDir, "mfp_daily.csv")); len(rows) != 90 {
			t.Fatalf("daily rows = %d", len(rows))
		}
		if e.srv.CallCount("/v2/diary?") != 90 || len(lines) < 91 {
			t.Fatalf("diary calls = %d, lines = %d", e.srv.CallCount("/v2/diary?"), len(lines))
		}
	})
}

func TestSessionLifecycle(t *testing.T) {
	t.Parallel()
	t.Run("ST-T103 a rotated token is saved, and the next run sends the new token", func(t *testing.T) {
		t.Parallel()
		e := newExportEnv(t)
		e.srv.SetCookie = "__Secure-next-auth.session-token=s2; Path=/; Secure; HttpOnly"
		if res, _ := e.run(t, e.req("2026-10-05", "2026-10-05")); res["ok"] != true {
			t.Fatalf("run 1: %v", res)
		}
		saved, err := LoadSession(e.sessPath)
		if err != nil || saved.Cookies[0].Value != "s2" {
			t.Fatalf("saved session = %+v, %v", saved, err)
		}
		if st, _ := os.Stat(e.sessPath); st.Mode().Perm() != 0o600 {
			t.Errorf("mode = %v", st.Mode().Perm())
		}
		if res, _ := e.run(t, e.req("2026-10-05", "2026-10-05")); res["ok"] != true {
			t.Fatalf("run 2: %v", res)
		}
		heads := e.srv.CookieHeaders()
		if first, last := heads[0], heads[len(heads)-1]; !strings.Contains(first, "session-token=s1") || !strings.Contains(last, "session-token=s2") {
			t.Fatalf("cookie headers = %q", heads)
		}
	})
	t.Run("ST-T104 a rotated token is saved even when the run then stops on a 429", func(t *testing.T) {
		t.Parallel()
		e := newExportEnv(t)
		e.srv.SetCookie = "__Secure-next-auth.session-token=s2; Path=/; Secure; HttpOnly"
		e.srv.Diary429 = true
		if res, _ := e.run(t, e.req("2026-10-05", "2026-10-05")); res["errorCode"] != "MFP_RATE_LIMITED" {
			t.Fatalf("result = %v", res)
		}
		if saved, err := LoadSession(e.sessPath); err != nil || saved.Cookies[0].Value != "s2" {
			t.Fatalf("saved session = %+v, %v", saved, err)
		}
	})
	t.Run("ST-I101 an expired session never takes the cookies of the login page, so the file stays byte for byte", func(t *testing.T) {
		t.Parallel()
		e := newExportEnv(t)
		before, _ := os.ReadFile(e.sessPath)
		e.srv.AuthHTML, e.srv.SetCookie = true, "tracking=1; Path=/"
		if res, _ := e.run(t, e.req("2026-10-04", "2026-10-07")); res["errorCode"] != "MFP_SESSION" {
			t.Fatalf("result = %v", res)
		}
		if after, _ := os.ReadFile(e.sessPath); !bytes.Equal(before, after) {
			t.Fatalf("session file rewritten on an expired run:\n%s\n%s", before, after)
		}
	})
	t.Run("ST-T105 an expired session works again after the user reconnects", func(t *testing.T) {
		t.Parallel()
		e := newExportEnv(t)
		e.srv.AuthHTML = true
		if res, _ := e.run(t, e.req("2026-10-05", "2026-10-05")); res["errorCode"] != "MFP_SESSION" {
			t.Fatalf("expired run: %v", res)
		}
		e.srv.AuthHTML = false
		s := testSession()
		s.Cookies[0].Value = "s9" // Rust writes a fresh file after the user signs in again
		if err := s.Save(e.sessPath); err != nil {
			t.Fatal(err)
		}
		if res, _ := e.run(t, e.req("2026-10-05", "2026-10-05")); res["ok"] != true {
			t.Fatalf("run after reconnect: %v", res)
		}
	})
}

func TestCacheLifecycle(t *testing.T) {
	t.Parallel()
	t.Run("ST-T106 days with food are served from the cache on the second run, days without food are fetched again", func(t *testing.T) {
		t.Parallel()
		e := newExportEnv(t)
		req := e.req("2026-10-04", "2026-10-07")
		e.run(t, req)
		before := e.srv.CallCount("/v2/diary?")
		e.run(t, req)
		// Oct 5 and Oct 7 have food and were fetched long after their date (the clock is real time).
		if got := e.srv.CallCount("/v2/diary?") - before; got != 2 {
			t.Fatalf("second run fetched %d days, want 2 (Oct 4 and Oct 6, which have no food)", got)
		}
	})
	t.Run("ST-T107 a day with food fetched less than 7 days after its date is fetched again and then becomes final", func(t *testing.T) {
		t.Parallel()
		e := newExportEnv(t)
		day, _ := time.ParseInLocation("2006-01-02", "2026-10-05", time.Local)
		cache := Cache{Dir: filepath.Join(e.dataDir, "mfp-cache")}
		if err := cache.Put("2026-10-05", fixtureItems(t, "2026-10-05"), day.Add(24*time.Hour)); err != nil {
			t.Fatal(err)
		}
		req := e.req("2026-10-05", "2026-10-05")
		e.run(t, req)
		if got := e.srv.CallCount("/v2/diary?"); got != 1 {
			t.Fatalf("run 1 fetched %d days, want 1", got)
		}
		e.run(t, req)
		if got := e.srv.CallCount("/v2/diary?"); got != 1 {
			t.Fatalf("run 2 fetched again (%d calls in total)", got)
		}
	})
	t.Run("ST-T108 a cleared cache (mfp-cache deleted) is rebuilt from MFP", func(t *testing.T) {
		t.Parallel()
		e := newExportEnv(t)
		req := e.req("2026-10-05", "2026-10-05")
		e.run(t, req)
		if err := os.RemoveAll(filepath.Join(e.dataDir, "mfp-cache")); err != nil {
			t.Fatal(err)
		}
		e.run(t, req)
		if got := e.srv.CallCount("/v2/diary?"); got != 2 {
			t.Fatalf("diary calls = %d, want 2 (one per run)", got)
		}
	})
	t.Run("ST-T109 after a 429 the days already downloaded stay cached and the next run resumes after them", func(t *testing.T) {
		t.Parallel()
		e := newExportEnv(t)
		e.srv.RateLimitAfter = 2
		res, _ := e.run(t, e.req("2026-10-05", "2026-10-07"))
		if res["errorCode"] != "MFP_RATE_LIMITED" || e.srv.CallCount("/v2/diary?") != 3 {
			t.Fatalf("result = %v, diary calls = %d (want 3: two served, the third answered 429)", res, e.srv.CallCount("/v2/diary?"))
		}
		for d, want := range map[string]bool{"2026-10-05": true, "2026-10-06": true, "2026-10-07": false} {
			if got := exists(filepath.Join(e.dataDir, "mfp-cache", d+".json")); got != want {
				t.Errorf("cache file for %s present = %v, want %v", d, got, want)
			}
		}
		e.srv.RateLimitAfter = 0
		before := e.srv.CallCount("/v2/diary?")
		if res, _ := e.run(t, e.req("2026-10-05", "2026-10-07")); res["ok"] != true {
			t.Fatalf("resume: %v", res)
		}
		// Oct 5 has food and is final; Oct 6 has none and Oct 7 was never fetched.
		if got := e.srv.CallCount("/v2/diary?") - before; got != 2 {
			t.Fatalf("resume fetched %d days, want 2", got)
		}
	})
	t.Run("ST-I102 a 429 stops the run at once: no later day is requested and no CSV is written", func(t *testing.T) {
		t.Parallel()
		e := newExportEnv(t)
		e.srv.RateLimitAfter = 1
		res, _ := e.run(t, e.req("2026-10-04", "2026-10-07"))
		if res["errorCode"] != "MFP_RATE_LIMITED" || e.srv.CallCount("/v2/diary?") != 2 || csvPresent(e) != 0 {
			t.Fatalf("result = %v, diary calls = %d, csv files = %d", res, e.srv.CallCount("/v2/diary?"), csvPresent(e))
		}
	})
}

func TestExportCancellation(t *testing.T) {
	t.Parallel()
	t.Run("ST-T110 a context cancelled before the start makes no request and writes no CSV", func(t *testing.T) {
		t.Parallel()
		e := newExportEnv(t)
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		res, _ := runExport(t, ctx, e.srv, e.req("2026-10-04", "2026-10-07"))
		if res["ok"] != false || res["errorCode"] != "MFP_ERROR" || e.srv.CallCount("/") != 0 || csvPresent(e) != 0 {
			t.Fatalf("result = %v, calls = %v", res, e.srv.Calls)
		}
	})
	t.Run("ST-T111 a context that ends during the pause between requests stops the run without a CSV", func(t *testing.T) {
		t.Parallel()
		e := newExportEnv(t)
		ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
		defer cancel()
		var buf bytes.Buffer
		start := time.Now()
		res := Export(ctx, e.req("2026-10-04", "2026-10-07"), nilEmitter(&buf), Options{Base: e.srv.URL, Gap: time.Hour})
		if res["ok"] != false || res["errorCode"] != "MFP_ERROR" || csvPresent(e) != 0 {
			t.Fatalf("result = %v", res)
		}
		if time.Since(start) > 10*time.Second {
			t.Fatal("the run did not stop at the deadline")
		}
		if got := e.srv.CallCount("/"); got != 1 { // only auth_token went out before the pause
			t.Fatalf("calls = %v", e.srv.Calls)
		}
	})
}

func TestExportFaults(t *testing.T) {
	t.Parallel()
	t.Run("EG-127 a diary answer that is not JSON gives MFP_ERROR, the detail only in the log, and no CSV", func(t *testing.T) {
		t.Parallel()
		for _, body := range []string{"", "{", `[]`, `{"items":"x"}`} {
			dir := t.TempDir()
			os.MkdirAll(filepath.Join(dir, "diary"), 0o755)
			os.WriteFile(filepath.Join(dir, "diary", "2026-10-05.json"), []byte(body), 0o644)
			e := newExportEnv(t)
			e.srv.Close()
			e.srv = mfptest.NewServer(dir)
			t.Cleanup(e.srv.Close)
			res, lines := e.run(t, e.req("2026-10-05", "2026-10-05"))
			if res["ok"] != false || res["errorCode"] != "MFP_ERROR" || res["error"] != msgUnexpected || !logHas(lines, "not valid JSON") || csvPresent(e) != 0 {
				t.Errorf("body %q: result = %v", body, res)
			}
		}
	})
	t.Run("EG-128 an empty items list and a null items list both give a logged=no day", func(t *testing.T) {
		t.Parallel()
		for _, body := range []string{`{}`, `{"items":null}`, `{"items":[]}`} {
			dir := t.TempDir()
			os.MkdirAll(filepath.Join(dir, "diary"), 0o755)
			os.WriteFile(filepath.Join(dir, "diary", "2026-10-05.json"), []byte(body), 0o644)
			e := newExportEnv(t)
			e.srv.Close()
			e.srv = mfptest.NewServer(dir)
			t.Cleanup(e.srv.Close)
			res, _ := e.run(t, e.req("2026-10-05", "2026-10-05"))
			day := readCSVMaps(t, filepath.Join(e.csvDir, "mfp_daily.csv"))
			if res["ok"] != true || res["loggedDays"] != 0 || len(day) != 1 || day[0]["logged"] != "no" {
				t.Errorf("body %q: result = %v, daily = %v", body, res, day)
			}
		}
	})
	t.Run("EG-129 a corrupt cache file is ignored and the day is fetched again", func(t *testing.T) {
		t.Parallel()
		e := newExportEnv(t)
		cacheDir := filepath.Join(e.dataDir, "mfp-cache")
		os.MkdirAll(cacheDir, 0o700)
		os.WriteFile(filepath.Join(cacheDir, "2026-10-05.json"), []byte("{broken"), 0o600)
		res, _ := e.run(t, e.req("2026-10-05", "2026-10-05"))
		if res["ok"] != true || res["loggedDays"] != 1 || e.srv.CallCount("/v2/diary?") != 1 {
			t.Fatalf("result = %v", res)
		}
	})
	t.Run("EG-130 a session cookie with a line break fails cleanly and nothing reaches the server", func(t *testing.T) {
		t.Parallel()
		e := newExportEnv(t)
		s := testSession()
		s.Cookies[0].Value = "abc\r\nX-Injected: 1"
		if err := s.Save(e.sessPath); err != nil {
			t.Fatal(err)
		}
		res, _ := e.run(t, e.req("2026-10-04", "2026-10-07"))
		if res["ok"] != false || res["errorCode"] == "" || e.srv.CallCount("/") != 0 {
			t.Fatalf("result = %v, calls = %v", res, e.srv.Calls)
		}
	})
	t.Run("EG-131 a corrupt session file is MFP_SESSION, the same as a missing one", func(t *testing.T) {
		t.Parallel()
		e := newExportEnv(t)
		os.WriteFile(e.sessPath, []byte("{broken"), 0o600)
		if res, _ := e.run(t, e.req("2026-10-04", "2026-10-07")); res["errorCode"] != "MFP_SESSION" || res["error"] != msgNoSession {
			t.Fatalf("result = %v", res)
		}
	})
	t.Run("EG-132 a csv dir that does not exist yet gives MFP_ERROR, not a panic", func(t *testing.T) {
		t.Parallel()
		e := newExportEnv(t)
		req := e.req("2026-10-05", "2026-10-05")
		req["csvDir"] = filepath.Join(e.csvDir, "not-created")
		res, _ := e.run(t, req)
		if res["ok"] != false || res["errorCode"] != "MFP_ERROR" {
			t.Fatalf("result = %v", res)
		}
	})
}
