package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/danihrndzld/garmin-health-exporter/tauri-app/core/internal/garmin/garmintest"
	"github.com/danihrndzld/garmin-health-exporter/tauri-app/core/internal/mfp"
	"github.com/danihrndzld/garmin-health-exporter/tauri-app/core/internal/mfp/mfptest"
	"github.com/danihrndzld/gconnect/gconnecttest"
)

const fixtures = "../../internal/mfp/testdata"

var binary string

// TestMain builds the real binary once: these tests speak to it the way the Tauri shell does.
func TestMain(m *testing.M) {
	dir, err := os.MkdirTemp("", "garmin-export-core-test-")
	if err != nil {
		panic(err)
	}
	binary = filepath.Join(dir, "garmin-export-core")
	if out, err := exec.Command("go", "build", "-o", binary, ".").CombinedOutput(); err != nil {
		os.Stderr.Write(out)
		os.RemoveAll(dir)
		os.Exit(1)
	}
	code := m.Run()
	os.RemoveAll(dir)
	os.Exit(code)
}

// runBinary writes one request line to stdin and returns every JSON line from stdout.
// An empty fakeURL leaves MFP_FAKE_SERVER unset.
func runBinary(t *testing.T, fakeURL, stdin string) []map[string]any {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, binary)
	cmd.Stdin = strings.NewReader(stdin)
	cmd.Env = append(os.Environ(), "MFP_FAKE_SERVER="+fakeURL)
	var stdout bytes.Buffer
	cmd.Stdout = &stdout
	if err := cmd.Run(); err != nil {
		t.Fatalf("binary failed: %v", err)
	}
	var lines []map[string]any
	for _, l := range strings.Split(strings.TrimSpace(stdout.String()), "\n") {
		var m map[string]any
		if err := json.Unmarshal([]byte(l), &m); err != nil {
			t.Fatalf("stdout line %q is not JSON: %v", l, err)
		}
		lines = append(lines, m)
	}
	return lines
}

func result(t *testing.T, lines []map[string]any) map[string]any {
	t.Helper()
	last := lines[len(lines)-1]
	if last["kind"] != "result" {
		t.Fatalf("last line is not a result: %v", last)
	}
	return last
}

// scenario is a fake MFP, a session on disk and a request line, all inside $HOME.
func scenario(t *testing.T, start, end string) (*mfptest.Server, string, string, string) {
	t.Helper()
	srv := mfptest.NewServer(fixtures)
	t.Cleanup(srv.Close)
	home, _ := os.UserHomeDir()
	root, err := os.MkdirTemp(home, ".mfp-binary-test-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(root) })
	dataDir, csvDir := filepath.Join(root, "data"), filepath.Join(root, "csv")
	for _, d := range []string{dataDir, csvDir} {
		os.MkdirAll(d, 0o755)
	}
	sess := &mfp.Session{UserAgent: "test-agent", Cookies: []mfp.Cookie{{Name: "__Secure-next-auth.session-token", Value: "s1"}}}
	if err := sess.Save(filepath.Join(dataDir, "mfp_session.json")); err != nil {
		t.Fatal(err)
	}
	b, _ := json.Marshal(map[string]any{"cmd": "mfp-export", "startDate": start, "endDate": end, "dataDir": dataDir, "csvDir": csvDir})
	return srv, dataDir, csvDir, string(b) + "\n"
}

func TestBinaryExportsTheGoldenFiles(t *testing.T) {
	t.Parallel()
	srv, _, csvDir, req := scenario(t, "2026-10-04", "2026-10-07")
	lines := runBinary(t, srv.URL, req)
	t.Run("CL-134 every stdout line is a JSON object of kind log, progress or result, and the last is the result", func(t *testing.T) {
		for _, l := range lines {
			if k := l["kind"]; k != "log" && k != "progress" && k != "result" {
				t.Errorf("unexpected line %v", l)
			}
		}
		if res := result(t, lines); res["ok"] != true || res["days"] != float64(4) || res["loggedDays"] != float64(2) {
			t.Errorf("result = %v", res)
		}
	})
	t.Run("CL-135 the binary sends all four diary requests to MFP_FAKE_SERVER", func(t *testing.T) {
		if n := srv.CallCount("/v2/diary?"); n != 4 {
			t.Errorf("diary calls = %d", n)
		}
	})
	for _, f := range []string{"mfp_food.csv", "mfp_daily.csv", "mfp_exercise.csv"} {
		name := map[string]string{
			"mfp_food.csv":     "CL-136 the binary writes mfp_food.csv equal to the golden file",
			"mfp_daily.csv":    "CL-137 the binary writes mfp_daily.csv equal to the golden file",
			"mfp_exercise.csv": "CL-138 the binary writes mfp_exercise.csv equal to the golden file",
		}[f]
		t.Run(name, func(t *testing.T) {
			got, _ := os.ReadFile(filepath.Join(csvDir, f))
			want, _ := os.ReadFile(filepath.Join(fixtures, "golden", f))
			if !bytes.Equal(got, want) {
				t.Errorf("%s differs from the golden file:\n%s", f, got)
			}
		})
	}
}

func TestBinaryReportsFailuresAsOneResultLine(t *testing.T) {
	t.Parallel()
	t.Run("CL-139 an expired session ends with ok=false, errorCode and error in the result line", func(t *testing.T) {
		srv, _, _, req := scenario(t, "2026-10-04", "2026-10-07")
		srv.AuthHTML = true
		res := result(t, runBinary(t, srv.URL, req))
		if res["ok"] != false || res["errorCode"] != "MFP_SESSION" || res["error"] == "" {
			t.Fatalf("result = %v", res)
		}
	})
	t.Run("CL-140 a 429 ends with MFP_RATE_LIMITED in the result line", func(t *testing.T) {
		srv, _, _, req := scenario(t, "2026-10-04", "2026-10-07")
		srv.Diary429 = true
		if res := result(t, runBinary(t, srv.URL, req)); res["errorCode"] != "MFP_RATE_LIMITED" {
			t.Fatalf("result = %v", res)
		}
	})
}

func TestBinaryRejectsBadRequests(t *testing.T) {
	t.Parallel()
	cases := []struct{ name, stdin, want string }{
		{"EG-137 a request line that is not JSON answers Invalid request", "not json\n", "Invalid request."},
		{"EG-138 an empty stdin answers Invalid request", "", "Invalid request."},
		{"EG-139 an unknown command answers Unknown command", `{"cmd":"garmin-export"}` + "\n", "Unknown command."},
		{"EG-140 a request without cmd answers Unknown command", `{"startDate":"2026-10-04"}` + "\n", "Unknown command."},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			lines := runBinary(t, "", tc.stdin)
			if res := result(t, lines); len(lines) != 1 || res["ok"] != false || res["error"] != tc.want {
				t.Fatalf("lines = %v", lines)
			}
		})
	}
}

func TestBinaryGarminStatusAndDisconnect(t *testing.T) {
	t.Parallel()
	var requests atomic.Int32
	fake := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		w.WriteHeader(http.StatusInternalServerError)
	}))
	t.Cleanup(fake.Close)
	home, _ := os.UserHomeDir()
	dir, err := os.MkdirTemp(home, ".garmin-binary-test-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(dir) })
	tokens := `{"di":{"access_token":"a.b.c","refresh_token":"r","client_id":"c","expires_at":1760540000,"refresh_token_expires_at":null},"saved_at":"2026-10-01T00:00:00Z"}`
	if err := os.WriteFile(filepath.Join(dir, "tokens.json"), []byte(tokens), 0o600); err != nil {
		t.Fatal(err)
	}
	run := func(t *testing.T, cmd string) map[string]any {
		b, _ := json.Marshal(map[string]any{"cmd": cmd, "dataDir": dir})
		c := exec.Command(binary)
		c.Stdin = strings.NewReader(string(b) + "\n")
		c.Env = append(os.Environ(), "GARMIN_FAKE_SERVER="+fake.URL)
		out, err := c.Output()
		if err != nil {
			t.Fatalf("binary failed: %v", err)
		}
		var m map[string]any
		if err := json.Unmarshal([]byte(strings.TrimSpace(string(out))), &m); err != nil {
			t.Fatalf("stdout %q: %v", out, err)
		}
		return m
	}
	// The steps share one data dir and run in order.
	t.Run("CL-213 status before disconnect reads the saved DI login", func(t *testing.T) {
		if r := run(t, "garmin-status"); r["ok"] != true || r["connected"] != true || r["loginKind"] != "di" || r["savedAt"] != "2026-10-01T00:00:00Z" {
			t.Errorf("status before disconnect = %v", r)
		}
	})
	t.Run("CL-214 disconnect answers not connected", func(t *testing.T) {
		if r := run(t, "garmin-disconnect"); r["ok"] != true || r["connected"] != false {
			t.Errorf("disconnect = %v", r)
		}
	})
	t.Run("CL-215 status after disconnect is not connected", func(t *testing.T) {
		if r := run(t, "garmin-status"); r["connected"] != false {
			t.Errorf("status after disconnect = %v", r)
		}
	})
	t.Run("CL-216 disconnect deletes tokens.json", func(t *testing.T) {
		if _, err := os.Stat(filepath.Join(dir, "tokens.json")); !os.IsNotExist(err) {
			t.Errorf("tokens.json still exists: %v", err)
		}
	})
	t.Run("CL-217 a dataDir outside home is GARMIN_INPUT", func(t *testing.T) {
		b, _ := json.Marshal(map[string]any{"cmd": "garmin-status", "dataDir": "/etc"})
		c := exec.Command(binary)
		c.Stdin = strings.NewReader(string(b) + "\n")
		out, _ := c.Output()
		if !strings.Contains(string(out), `"errorCode":"GARMIN_INPUT"`) {
			t.Errorf("dataDir outside home answered %s", out)
		}
	})
	t.Run("EP-217 garmin-status and garmin-disconnect send no request", func(t *testing.T) {
		if n := requests.Load(); n != 0 {
			t.Errorf("the fake Garmin got %d requests", n)
		}
	})
}

func TestBinaryGarminConnect(t *testing.T) {
	t.Parallel()
	fake := gconnecttest.New()
	t.Cleanup(fake.Close)
	home, _ := os.UserHomeDir()
	connect := func(password string) (map[string]any, string, string) {
		dir, err := os.MkdirTemp(home, ".garmin-binary-test-")
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { os.RemoveAll(dir) })
		b, _ := json.Marshal(map[string]any{"cmd": "garmin-connect", "dataDir": dir, "email": "e2e@example.invalid", "password": password})
		c := exec.Command(binary)
		c.Stdin = strings.NewReader(string(b) + "\n")
		c.Env = append(os.Environ(), "GARMIN_FAKE_SERVER="+fake.URL())
		out, err := c.CombinedOutput()
		if err != nil {
			t.Fatalf("binary failed: %v", err)
		}
		lines := strings.Split(strings.TrimSpace(string(out)), "\n")
		var m map[string]any
		if err := json.Unmarshal([]byte(lines[len(lines)-1]), &m); err != nil {
			t.Fatalf("stdout %q: %v", out, err)
		}
		return m, dir, string(out)
	}
	t.Run("CL-226 the right password saves a 0600 tokens.json and never prints the password", func(t *testing.T) {
		r, dir, out := connect("right-password")
		if r["ok"] != true || r["connected"] != true || r["expired"] != false || r["loginKind"] != "di" {
			t.Fatalf("result = %v", r)
		}
		fi, err := os.Stat(filepath.Join(dir, "tokens.json"))
		if err != nil || fi.Mode().Perm() != 0o600 {
			t.Fatalf("tokens.json: %v %v", fi, err)
		}
		if strings.Contains(out, "right-password") {
			t.Fatal("stdout holds the password")
		}
	})
	t.Run("CL-227 a wrong password answers GARMIN_BAD_PASSWORD and writes no tokens.json", func(t *testing.T) {
		r, dir, _ := connect("nope")
		if r["ok"] != false || r["errorCode"] != "GARMIN_BAD_PASSWORD" {
			t.Fatalf("result = %v", r)
		}
		if _, err := os.Stat(filepath.Join(dir, "tokens.json")); !os.IsNotExist(err) {
			t.Fatalf("tokens.json exists: %v", err)
		}
	})
	t.Run("CL-228 an empty password answers GARMIN_INPUT and sends nothing", func(t *testing.T) {
		fake.Reset()
		r, _, _ := connect("")
		if r["ok"] != false || r["errorCode"] != "GARMIN_INPUT" {
			t.Fatalf("result = %v", r)
		}
		if n := len(fake.Calls("")); n != 0 {
			t.Fatalf("%d requests", n)
		}
	})
}

func TestBinaryClearCache(t *testing.T) {
	t.Parallel()
	home, _ := os.UserHomeDir()
	dir, err := os.MkdirTemp(home, ".clear-cache-test-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(dir) })
	run := func(dataDir string) map[string]any {
		b, _ := json.Marshal(map[string]any{"cmd": "clear-cache", "dataDir": dataDir})
		c := exec.Command(binary)
		c.Stdin = strings.NewReader(string(b) + "\n")
		out, _ := c.Output()
		var m map[string]any
		if err := json.Unmarshal([]byte(strings.TrimSpace(string(out))), &m); err != nil {
			t.Fatalf("stdout %q: %v", out, err)
		}
		return m
	}
	exists := func(name string) bool { _, err := os.Stat(filepath.Join(dir, name)); return err == nil }
	t.Run("CL-231 ports the Bun clear-cache test: the three caches go, tokens.json and mfp_session.json stay", func(t *testing.T) {
		for name, body := range map[string]string{
			"garmin_cache.db":                    "db",
			"garmin-cache/daily/2026-10-01.json": "{}",
			"mfp-cache/2026-10-01.json":          "{}",
			"tokens.json":                        "{}",
			"mfp_session.json":                   "{}",
		} {
			p := filepath.Join(dir, name)
			os.MkdirAll(filepath.Dir(p), 0o755)
			if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
				t.Fatal(err)
			}
		}
		if r := run(dir); r["ok"] != true {
			t.Fatalf("result = %v", r)
		}
		for _, gone := range []string{"garmin_cache.db", "garmin-cache", "mfp-cache"} {
			if exists(gone) {
				t.Errorf("%s still exists", gone)
			}
		}
		for _, kept := range []string{"tokens.json", "mfp_session.json"} {
			if !exists(kept) {
				t.Errorf("%s was removed", kept)
			}
		}
	})
	t.Run("CL-232 a missing cache is ok", func(t *testing.T) {
		if r := run(dir); r["ok"] != true {
			t.Errorf("result = %v", r)
		}
	})
	t.Run("CL-233 a dataDir outside home is rejected", func(t *testing.T) {
		if r := run("/etc"); r["ok"] != false || r["errorCode"] != "GARMIN_INPUT" {
			t.Errorf("result = %v", r)
		}
	})
}

func TestBinaryExportWithGarminOff(t *testing.T) {
	t.Parallel()
	fake := gconnecttest.New()
	t.Cleanup(fake.Close)
	home, _ := os.UserHomeDir()
	out, err := os.MkdirTemp(home, ".garmin-export-off-test-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(out) })
	run := func(req map[string]any) map[string]any {
		b, _ := json.Marshal(req)
		c := exec.Command(binary)
		c.Stdin = strings.NewReader(string(b) + "\n")
		c.Env = append(os.Environ(), "GARMIN_FAKE_SERVER="+fake.URL())
		stdout, err := c.Output()
		if err != nil {
			t.Fatalf("binary failed: %v", err)
		}
		var m map[string]any
		if err := json.Unmarshal([]byte(strings.TrimSpace(string(stdout))), &m); err != nil {
			t.Fatalf("stdout %q: %v", stdout, err)
		}
		return m
	}
	t.Run("CL-243 garmin:false makes csv_<endDate>, answers jsonPath null and sends no Garmin request", func(t *testing.T) {
		r := run(map[string]any{"cmd": "export", "garmin": false, "startDate": "2026-10-01", "endDate": "2026-10-07", "outputDir": out})
		jsonPath, has := r["jsonPath"]
		if r["ok"] != true || r["path"] != filepath.Join(out, "csv_2026-10-07") || !has || jsonPath != nil || r["startDate"] != "2026-10-01" || r["endDate"] != "2026-10-07" {
			t.Fatalf("result = %v", r)
		}
		if fi, err := os.Stat(filepath.Join(out, "csv_2026-10-07")); err != nil || !fi.IsDir() {
			t.Fatalf("csv folder: %v", err)
		}
		if n := len(fake.Calls("")); n != 0 {
			t.Fatalf("%d Garmin requests", n)
		}
	})
	t.Run("CL-244 garmin:false with daysBack 1 makes the folder of today", func(t *testing.T) {
		today := time.Now().Format("2006-01-02")
		r := run(map[string]any{"cmd": "export", "garmin": false, "daysBack": 1, "outputDir": out})
		if r["ok"] != true || r["startDate"] != today || r["endDate"] != today || r["path"] != filepath.Join(out, "csv_"+today) {
			t.Fatalf("result = %v", r)
		}
	})
	for _, c := range []struct {
		name string
		req  map[string]any
	}{
		{"EP-222 an outputDir outside $HOME is rejected", map[string]any{"startDate": "2026-10-01", "endDate": "2026-10-07", "outputDir": "/etc"}},
		{"EP-223 a range of 91 days is rejected", map[string]any{"startDate": "2026-07-16", "endDate": "2026-10-14", "outputDir": out}},
		{"EP-224 daysBack 0 is rejected", map[string]any{"daysBack": 0, "outputDir": out}},
		{"EP-225 a daysBack that is not a whole number is rejected", map[string]any{"daysBack": 1.5, "outputDir": out}},
		{"EP-226 a refreshWindow of 8 is rejected", map[string]any{"daysBack": 7, "refreshWindow": 8, "outputDir": out}},
		{"BVA-217 an endDate of tomorrow is rejected", map[string]any{"startDate": time.Now().Format("2006-01-02"), "endDate": time.Now().AddDate(0, 0, 1).Format("2006-01-02"), "outputDir": out}},
	} {
		t.Run(c.name, func(t *testing.T) {
			c.req["cmd"], c.req["garmin"] = "export", false
			if r := run(c.req); r["ok"] != false || r["errorCode"] != "GARMIN_INPUT" || r["error"] == "" {
				t.Fatalf("result = %v", r)
			}
		})
	}
}

func TestBinaryGarminExport(t *testing.T) {
	t.Parallel()
	repo := filepath.Join("..", "..", "..", "..")
	fake := gconnecttest.New()
	t.Cleanup(fake.Close)
	if err := garmintest.Serve(fake, filepath.Join(repo, "electron-app", "garmin", "__tests__", "fixtures", "export-sample.json")); err != nil {
		t.Fatal(err)
	}
	home, _ := os.UserHomeDir()
	root, err := os.MkdirTemp(home, ".garmin-binary-export-test-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(root) })
	data, out := filepath.Join(root, "data"), filepath.Join(root, "out")
	run := func(req map[string]any) map[string]any {
		b, _ := json.Marshal(req)
		c := exec.Command(binary)
		c.Stdin = strings.NewReader(string(b) + "\n")
		c.Env = append(os.Environ(), "GARMIN_FAKE_SERVER="+fake.URL())
		stdout, err := c.Output()
		if err != nil {
			t.Fatalf("binary failed: %v", err)
		}
		lines := strings.Split(strings.TrimSpace(string(stdout)), "\n")
		var m map[string]any
		if err := json.Unmarshal([]byte(lines[len(lines)-1]), &m); err != nil {
			t.Fatalf("stdout %q: %v", stdout, err)
		}
		return m
	}
	t.Run("CL-361 garmin-connect then export through the binary answers ok with the golden CSV files", func(t *testing.T) {
		if r := run(map[string]any{"cmd": "garmin-connect", "email": "e2e@example.invalid", "password": "right-password", "dataDir": data}); r["ok"] != true {
			t.Fatalf("connect = %v", r)
		}
		r := run(map[string]any{"cmd": "export", "garmin": true, "startDate": "2024-02-17", "endDate": "2024-02-21", "refreshWindow": 3, "outputDir": out, "dataDir": data})
		// The folder and the JSON name come from one clock read, so they name the same day even across midnight.
		day := strings.TrimPrefix(filepath.Base(fmt.Sprint(r["jsonPath"])), "garmin_health_")
		day = day[:min(10, len(day))]
		if r["ok"] != true || r["path"] != filepath.Join(out, "csv_"+day) || r["days"] != 5.0 || r["activities"] != 4.0 {
			t.Fatalf("export = %v", r)
		}
		golden := filepath.Join(repo, "electron-app", "garmin", "__tests__", "golden")
		names, _ := os.ReadDir(golden)
		if files, _ := r["files"].([]any); len(files) != len(names) {
			t.Fatalf("files = %v, want %d", r["files"], len(names))
		}
		for _, n := range names {
			want, _ := os.ReadFile(filepath.Join(golden, n.Name()))
			got, err := os.ReadFile(filepath.Join(r["path"].(string), n.Name()))
			if err != nil || !bytes.Equal(got, want) {
				t.Errorf("%s differs from the golden file (%v)", n.Name(), err)
			}
		}
		if p, _ := r["jsonPath"].(string); !strings.HasPrefix(p, filepath.Join(out, "garmin_health_")) {
			t.Errorf("jsonPath = %v", r["jsonPath"])
		}
	})
	t.Run("CL-362 export without a garmin-connect answers GARMIN_SESSION and sends nothing", func(t *testing.T) {
		fake.Reset()
		empty := filepath.Join(root, "empty")
		r := run(map[string]any{"cmd": "export", "startDate": "2024-02-17", "endDate": "2024-02-21", "outputDir": out, "dataDir": empty})
		if r["ok"] != false || r["errorCode"] != "GARMIN_SESSION" || r["error"] != "Connect Garmin first." || len(fake.Calls("")) != 0 {
			t.Fatalf("result = %v, %d calls", r, len(fake.Calls("")))
		}
	})
}

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
		// A csv_ symlink to a folder outside $HOME and a csv_ regular file are refused too.
		outside := t.TempDir()
		os.WriteFile(filepath.Join(outside, "keep.md"), []byte("x"), 0o644)
		os.Mkdir(filepath.Join(outside, "llm-en"), 0o755)
		link, file := filepath.Join(root, "csv_link"), filepath.Join(root, "csv_file")
		os.Symlink(outside, link)
		os.WriteFile(file, []byte("x"), 0o644)
		for _, dir := range []string{link, file} {
			res := result(t, runBinary(t, "", line(map[string]any{"cmd": "pack", "mode": "files", "lang": "en", "csvDir": dir, "startDate": "2026-01-01", "endDate": "2026-01-02"})))
			if res["error"] != "csvDir must be an export csv_ folder within your home directory." {
				t.Errorf("%q: %v", dir, res)
			}
		}
		if _, err := os.Stat(filepath.Join(outside, "llm-en")); err != nil {
			t.Error("pack deleted through a symlink")
		}
		if res := result(t, runBinary(t, "", line(map[string]any{"cmd": "summary", "csvDir": csvDir, "startDate": "2026-01-01", "endDate": "x/../../y"}))); res["error"] != "startDate and endDate must be YYYY-MM-DD." {
			t.Errorf("summary dates: %v", res)
		}
		// A date_range that is not YYYY-MM-DD never reaches the pack: summary yes, pack null.
		slash := filepath.Join(root, "slash.json")
		os.WriteFile(slash, []byte(`{"date_range":{"start":"2024/02/17","end":"2024-02-21"},"daily":{}}`), 0o644)
		if res := result(t, runBinary(t, "", line(map[string]any{"cmd": "summary", "jsonPath": slash, "csvDir": csvDir}))); res["ok"] != true || res["summary"] == nil || res["pack"] != nil {
			t.Errorf("file range: %v", res)
		}
		// No version and no os read "?", as report.js defaulted them.
		if res := result(t, runBinary(t, "", line(map[string]any{"cmd": "report", "error": "x"}))); !strings.Contains(res["issueUrl"].(string), "v%3F%20%C2%B7%20%3F") {
			t.Errorf("report defaults: %v", res)
		}
		if res := result(t, runBinary(t, "", line(map[string]any{"cmd": "report", "error": "x", "log": []any{map[string]any{"ts": 1}}}))); res["error"] != "Invalid report request." {
			t.Errorf("bad log: %v", res)
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
