package main

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/danihrndzld/garmin-health-exporter/tauri-app/core/internal/mfp"
	"github.com/danihrndzld/garmin-health-exporter/tauri-app/core/internal/mfp/mfptest"
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
