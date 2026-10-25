package main

import (
	"encoding/json"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/danihrndzld/gconnect"
	"github.com/danihrndzld/gconnect/gconnecttest"
)

// captureStdout runs f with os.Stdout sent to a pipe and returns what it wrote.
func captureStdout(t *testing.T, f func() int) (string, int) {
	t.Helper()
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	saved := os.Stdout
	os.Stdout = w
	rc := f()
	os.Stdout = saved
	w.Close()
	out, _ := io.ReadAll(r)
	return string(out), rc
}

// The probe against the fake Garmin: the key mode paths answer 200, the login only paths 404,
// and neither the key nor the token reaches stdout or the JSON file.
func TestProbeAgainstTheFakeGarmin(t *testing.T) {
	srv := gconnecttest.New()
	defer srv.Close()
	// Every path answers; the fake still answers 404 to an API key token outside the Personal API paths.
	srv.Handle("/", func(w http.ResponseWriter, _ *http.Request) { io.WriteString(w, `{"data": [1, 2]}`) })
	root = t.TempDir()
	if err := os.WriteFile(filepath.Join(root, ".env"), []byte("OTHER=1\nAPI_KEY=\"right-api-key\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	probeHosts, probeGap = srv.Hosts(), -1
	defer func() { probeHosts, probeGap = gconnect.Hosts{}, 0 }()

	out, rc := captureStdout(t, func() int { return probeMain([]string{"--end", "2026-10-20", "--days", "31"}) })
	if rc != 0 {
		t.Errorf("exit %d:\n%s", rc, out)
	}
	raw, err := os.ReadFile(filepath.Join(root, ".tmp", "personal_api_probe.json"))
	if err != nil {
		t.Fatal(err)
	}
	var got struct {
		End  string
		Rows []struct {
			Name   string
			Status int
			Needed bool
		}
	}
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatal(err)
	}
	if got.End != "2026-10-20" || len(got.Rows) != len(probeRows) {
		t.Fatalf("end %s, %d rows", got.End, len(got.Rows))
	}
	if !strings.Contains(out, "personal_daily_summaries   200  needed  data: list of 2") {
		t.Errorf("the table line is missing:\n%s", out)
	}
	for _, r := range got.Rows {
		if want := map[bool]int{true: 200, false: 404}[r.Needed]; r.Status != want {
			t.Errorf("%s: HTTP %d, want %d", r.Name, r.Status, want)
		}
	}
	for _, secret := range []string{"right-api-key", "eyJ"} {
		if strings.Contains(out, secret) || strings.Contains(string(raw), secret) {
			t.Errorf("the output holds %q", secret)
		}
	}
	// The sleep range asks 31 days from the end date.
	if calls := srv.Calls("/sleep-service/sleep/dailySleepSummariesByDate"); len(calls) != 1 || calls[0].Query != "startDate=2026-09-20&endDate=2026-10-20" {
		t.Errorf("sleep calls: %+v", calls)
	}
}

// Every Personal API range gconnect knows is in the probe table, so a new one is probed too.
func TestProbeCoversEveryPersonalRange(t *testing.T) {
	names := map[string]bool{}
	for _, r := range probeRows {
		names[r.name] = true
	}
	for _, ep := range gconnect.PersonalRanges() {
		if !names[ep.Name] {
			t.Errorf("probeRows misses %s", ep.Name)
		}
	}
}
