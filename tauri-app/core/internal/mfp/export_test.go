package mfp

import (
	"bytes"
	"context"
	"encoding/csv"
	"os"
	"path/filepath"
	"strings"
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

func TestExportSurvivesInvalidGoals(t *testing.T) {
	dir := t.TempDir()
	if err := os.CopyFS(dir, os.DirFS("testdata")); err != nil {
		t.Fatal(err)
	}
	os.WriteFile(filepath.Join(dir, "goals.json"), []byte("{not json"), 0o644)
	srv := mfptest.NewServer(dir)
	defer srv.Close()
	dataDir, csvDir := homeDirs(t)
	testSession().Save(filepath.Join(dataDir, "mfp_session.json"))
	res := run(t, srv, map[string]any{"cmd": "mfp-export", "startDate": "2026-10-04", "endDate": "2026-10-07", "dataDir": dataDir, "csvDir": csvDir})
	if res["ok"] != true {
		t.Fatalf("result = %v", res)
	}
	f, _ := os.Open(filepath.Join(csvDir, "mfp_daily.csv"))
	defer f.Close()
	rows, err := csv.NewReader(f).ReadAll()
	if err != nil || len(rows) < 2 {
		t.Fatalf("daily csv: %v %v", rows, err)
	}
	for i, h := range rows[0] {
		if strings.HasPrefix(h, "goal_") {
			for _, r := range rows[1:] {
				if r[i] != "" {
					t.Errorf("%s = %q, want empty", h, r[i])
				}
			}
		}
	}
}
