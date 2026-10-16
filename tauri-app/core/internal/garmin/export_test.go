package garmin

import (
	"bytes"
	"context"
	"encoding/json"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/danihrndzld/garmin-health-exporter/tauri-app/core/gconnect"
	"github.com/danihrndzld/garmin-health-exporter/tauri-app/core/gconnect/gconnecttest"
	"github.com/danihrndzld/garmin-health-exporter/tauri-app/core/internal/garmin/garmintest"
	"github.com/danihrndzld/garmin-health-exporter/tauri-app/core/internal/guard"
	"github.com/danihrndzld/garmin-health-exporter/tauri-app/core/internal/jsjson"
)

func TestPrepareDirMakesTheEndDateFolderAndDropsStaleMfpFiles(t *testing.T) {
	home, _ := os.UserHomeDir()
	out, err := os.MkdirTemp(home, ".garmin-prepare-test-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(out) })
	t.Run("CL-241 PrepareDir returns csv_<endDate> inside outputDir and removes the stale MFP files there", func(t *testing.T) {
		stale := filepath.Join(out, "csv_2026-10-07")
		os.MkdirAll(stale, 0o755)
		files := []string{"mfp_food.csv", "mfp_daily.csv", "mfp_exercise.csv"}
		for _, f := range files {
			os.WriteFile(filepath.Join(stale, f), []byte("old\n"), 0o644)
		}
		dir, err := PrepareDir(out, "2026-10-07")
		if err != nil || dir != stale {
			t.Fatalf("PrepareDir = %q %v", dir, err)
		}
		for _, f := range files {
			if _, err := os.Stat(filepath.Join(stale, f)); !os.IsNotExist(err) {
				t.Fatalf("stale %s kept", f)
			}
		}
	})
	t.Run("CL-242 PrepareDir creates a folder that does not exist yet", func(t *testing.T) {
		dir, err := PrepareDir(filepath.Join(out, "new"), "2026-10-08")
		if err != nil {
			t.Fatal(err)
		}
		if fi, err := os.Stat(dir); err != nil || !fi.IsDir() {
			t.Fatalf("%s: %v", dir, err)
		}
	})
	t.Run("EP-221 PrepareDir rejects a folder outside $HOME", func(t *testing.T) {
		if _, err := PrepareDir("/tmp", "2026-10-07"); err == nil {
			t.Fatal("a folder outside $HOME was accepted")
		}
	})
}

// recorder is a Reporter that keeps every line for the assertions.
type recorder struct {
	msgs, warns, phases []string
}

func (r *recorder) Log(kind, msg string) { r.msgs = append(r.msgs, msg) }
func (r *recorder) Warn(msg, code string) {
	r.msgs = append(r.msgs, msg)
	r.warns = append(r.warns, code+" "+msg)
}
func (r *recorder) Progress(phase string, current, total int) {
	if n := len(r.phases); n == 0 || r.phases[n-1] != phase {
		r.phases = append(r.phases, phase)
	}
}

// exportScenario serves the fixture, signs in the way garmin-connect does, and clears the
// call log, so the calls the test reads are the export's own.
func exportScenario(t *testing.T) (*gconnecttest.Server, ExportRequest) {
	t.Helper()
	srv, a := fakeGarmin(t)
	if err := garmintest.Serve(srv, fixturePath); err != nil {
		t.Fatal(err)
	}
	home, _ := os.UserHomeDir()
	root, err := os.MkdirTemp(home, ".garmin-export-test-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(root) })
	r := ExportRequest{StartDate: "2024-02-17", EndDate: "2024-02-21", RefreshWindow: 3,
		OutputDir: filepath.Join(root, "out"), DataDir: filepath.Join(root, "data"), Hosts: srv.Hosts(), Gap: -1,
		Now: func() time.Time { return time.Date(2026, 10, 7, 12, 0, 0, 0, time.Local) }} // pinned: no test may cross midnight
	if err := connect(a, r.DataDir, time.Now()); err != nil {
		t.Fatal(err)
	}
	srv.Reset()
	return srv, r
}

func goldenNames(t *testing.T) []string {
	t.Helper()
	entries, err := os.ReadDir(goldenDir)
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, e := range entries {
		names = append(names, e.Name())
	}
	return names
}

func TestExportWritesTheGoldenFiles(t *testing.T) {
	srv, r := exportScenario(t)
	rec := &recorder{}
	res, err := Export(context.Background(), r, rec)
	if err != nil {
		t.Fatal(err)
	}
	t.Run("AC-361 ports AC-03: a full export of the fixture range writes CSV files equal to the golden files", func(t *testing.T) {
		names := goldenNames(t)
		got := slices.Clone(res.Files)
		slices.Sort(got)
		if !slices.Equal(got, names) {
			t.Fatalf("files = %v, want %v", got, names)
		}
		sameFiles(t, res.Path, goldenDir, names)
		if res.Path != filepath.Join(r.OutputDir, "csv_2026-10-07") || res.Days != 5 || res.Activities != 4 ||
			res.StartDate != r.StartDate || res.EndDate != r.EndDate {
			t.Fatalf("result = %+v", res)
		}
	})
	t.Run("AC-362 ports AC-03: the JSON file has the daily keys newest first and splits on every activity", func(t *testing.T) {
		b, err := os.ReadFile(*res.JSONPath)
		if err != nil {
			t.Fatal(err)
		}
		v, err := jsjson.Parse(b)
		if err != nil {
			t.Fatal(err)
		}
		if keys := v.Get("daily").O.Keys; !slices.Equal(keys, []string{"2024-02-21", "2024-02-20", "2024-02-19", "2024-02-18", "2024-02-17"}) {
			t.Fatalf("daily keys = %v", keys)
		}
		for _, a := range v.Get("activities").A {
			if a.Get("splits").Kind != jsjson.Object {
				t.Fatalf("activity %s has no splits", jsjson.Cell(a.Get("activityId")))
			}
		}
		if want := []string{"export_date", "date_range", "daily", "aggregated", "activities", "goals"}; !slices.Equal(v.O.Keys, want) {
			t.Fatalf("top keys = %v", v.O.Keys)
		}
	})
	t.Run("EG-361 ports EG-02: the alias endpoints are requested once per day and split_summaries never", func(t *testing.T) {
		for _, date := range []string{"2024-02-17", "2024-02-21"} {
			for _, p := range []string{"/wellness-service/wellness/dailyStress/", "/metrics-service/metrics/trainingreadiness/"} {
				if n := len(srv.Calls(p + date)); n != 1 {
					t.Errorf("%s%s asked %d times", p, date, n)
				}
			}
		}
		if n := len(srv.Calls("/usersummary-service/usersummary/daily/" + garmintest.DisplayName)); n != 5 {
			t.Errorf("stats asked %d times for 5 days", n)
		}
		for _, c := range srv.Calls("") {
			if strings.Contains(c.Path, "split_summaries") {
				t.Errorf("split_summaries asked: %s", c.Path)
			}
		}
	})
	t.Run("EG-362 ports EG-03: goals are asked with start=1 and Sec-Fetch-Site: same-origin", func(t *testing.T) {
		calls := srv.Calls("/goal-service/goal/goals")
		if len(calls) != 1 || !strings.Contains("&"+calls[0].Query+"&", "&start=1&") || calls[0].Header.Get("Sec-Fetch-Site") != "same-origin" {
			t.Fatalf("goals calls = %+v", calls)
		}
	})
	t.Run("EG-363 the log lines the UI parses come in order, the progress phases too, and no line holds the display name", func(t *testing.T) {
		want := []string{"Using the saved Garmin login", "Pulling 5 days", "Pulling aggregated metrics...", "Pulling activities...", "Found 4 activities", "JSON saved: ", "CSVs written to ", "Done."}
		i := 0
		for _, m := range rec.msgs {
			if i < len(want) && strings.HasPrefix(m, want[i]) {
				i++
			}
			if strings.Contains(m, garmintest.DisplayName) {
				t.Errorf("a log line holds the display name: %q", m)
			}
		}
		if i != len(want) {
			t.Fatalf("missing %q in order; lines: %q", want[i], rec.msgs)
		}
		if !slices.Equal(rec.phases, []string{"daily", "aggregated", "activities", "csv"}) {
			t.Fatalf("phases = %v", rec.phases)
		}
		if len(rec.warns) != 0 {
			t.Fatalf("warnings: %q", rec.warns)
		}
	})
}

func TestExportReadsFinalDaysFromTheCache(t *testing.T) {
	srv, r := exportScenario(t)
	r.Now = func() time.Time { return time.Date(2024, 3, 22, 12, 0, 0, 0, time.Local) } // 30 days after the end: every day is final
	if _, err := Export(context.Background(), r, &recorder{}); err != nil {
		t.Fatal(err)
	}
	srv.Reset()
	res, err := Export(context.Background(), r, &recorder{})
	if err != nil {
		t.Fatal(err)
	}
	t.Run("EG-364 ports EG-04: a second export of old days asks only for the answers that were 204", func(t *testing.T) {
		raw, _ := os.ReadFile(fixturePath)
		fx, _ := jsjson.Parse(raw)
		daily, want := map[string]bool{}, map[string]bool{}
		for _, date := range guard.Dates(r.StartDate, r.EndDate) {
			for _, ep := range gconnect.Daily() {
				if ep.AliasOf != "" {
					continue
				}
				u := ep.URL("", gconnect.Params{Date: date, DisplayName: garmintest.DisplayName})
				daily[u] = true
				if fx.Get("daily").Get(date).Get(ep.Name).IsNullish() {
					want[u] = true
				}
			}
		}
		if len(want) == 0 {
			t.Fatal("the fixture has no 204 day; the test proves nothing")
		}
		got := map[string]bool{}
		for _, c := range srv.Calls("") {
			u := c.Path
			if c.Query != "" {
				u += "?" + c.Query
			}
			if daily[u] {
				if got[u] {
					t.Errorf("%s asked twice", u)
				}
				got[u] = true
			}
		}
		if !maps.Equal(got, want) {
			t.Fatalf("daily requests = %v, want the 204 ones %v", got, want)
		}
	})
	t.Run("EG-365 the files from cached answers still equal the golden files", func(t *testing.T) {
		sameFiles(t, res.Path, goldenDir, goldenNames(t))
	})
}

func TestExportStops(t *testing.T) {
	t.Run("EG-366 ports EG-05: a persistent 429 stops with GARMIN_RATE_LIMITED and the finished days are on disk", func(t *testing.T) {
		srv, r := exportScenario(t)
		srv.Set(func(c *gconnecttest.Control) { c.DataStatus = map[string]int{"/bloodpressure-service": 429} })
		_, err := Export(context.Background(), r, &recorder{})
		if codeOf(err) != "GARMIN_RATE_LIMITED" {
			t.Fatalf("err = %v", err)
		}
		days, _ := os.ReadDir(filepath.Join(r.DataDir, "garmin-cache", "daily"))
		if len(days) != 5 {
			t.Fatalf("%d day files on disk, want 5", len(days))
		}
	})
	t.Run("EG-367 ports EG-07: a 429 on the activity list stops instead of reporting success without activities", func(t *testing.T) {
		srv, r := exportScenario(t)
		srv.Set(func(c *gconnecttest.Control) { c.DataStatus = map[string]int{"/activitylist-service": 429} })
		res, err := Export(context.Background(), r, &recorder{})
		if res != nil || codeOf(err) != "GARMIN_RATE_LIMITED" {
			t.Fatalf("res = %+v, err = %v", res, err)
		}
		if n := len(srv.Calls("/goal-service/goal/goals")); n != 0 {
			t.Fatalf("goals asked after the stop: %d", n)
		}
	})
	t.Run("EG-368 without tokens.json the export answers GARMIN_SESSION Connect Garmin first. and sends nothing", func(t *testing.T) {
		srv, r := exportScenario(t)
		os.Remove(filepath.Join(r.DataDir, "tokens.json"))
		_, err := Export(context.Background(), r, &recorder{})
		if codeOf(err) != "GARMIN_SESSION" || err.Error() != "Connect Garmin first." || len(srv.Calls("")) != 0 {
			t.Fatalf("err = %v, %d calls", err, len(srv.Calls("")))
		}
	})
	t.Run("EG-369 with the expired marker the export asks to reconnect and sends nothing", func(t *testing.T) {
		srv, r := exportScenario(t)
		if err := MarkExpired(r.DataDir, time.Now()); err != nil {
			t.Fatal(err)
		}
		_, err := Export(context.Background(), r, &recorder{})
		if codeOf(err) != "GARMIN_SESSION" || err.Error() != "The Garmin session expired. Reconnect Garmin." || len(srv.Calls("")) != 0 {
			t.Fatalf("err = %v, %d calls", err, len(srv.Calls("")))
		}
	})
}

// expiringScenario signs in with tokens that expire within the refresh margin, so the
// export's first request refreshes them.
func expiringScenario(t *testing.T, refreshStatus int) (*gconnecttest.Server, ExportRequest, []byte) {
	t.Helper()
	srv, r := exportScenario(t)
	os.Remove(filepath.Join(r.DataDir, "tokens.json"))
	srv.Set(func(c *gconnecttest.Control) { c.TokenTTL = 60 })
	if err := connect(&gconnect.Auth{Hosts: srv.Hosts()}, r.DataDir, time.Now()); err != nil {
		t.Fatal(err)
	}
	srv.Reset()
	srv.Set(func(c *gconnecttest.Control) { c.RefreshFail = refreshStatus })
	return srv, r, tokensFileOf(t, r.DataDir)
}

func TestExportRefresh(t *testing.T) {
	t.Run("ST-T361 a 5xx on the token refresh ends with GARMIN_ERROR and keeps tokens.json byte for byte", func(t *testing.T) {
		_, r, before := expiringScenario(t, 500)
		_, err := Export(context.Background(), r, &recorder{})
		if codeOf(err) != "GARMIN_ERROR" {
			t.Fatalf("err = %v", err)
		}
		if after := tokensFileOf(t, r.DataDir); !bytes.Equal(after, before) {
			t.Fatalf("tokens.json changed:\n%s", after)
		}
	})
	t.Run("ST-T362 a 401 on the token refresh writes the expired marker and ends with GARMIN_SESSION", func(t *testing.T) {
		_, r, _ := expiringScenario(t, 401)
		_, err := Export(context.Background(), r, &recorder{})
		if codeOf(err) != "GARMIN_SESSION" || err.Error() != "The Garmin session expired. Reconnect Garmin." {
			t.Fatalf("err = %v", err)
		}
		if s := StatusOf(r.DataDir, time.Now()); !s.Expired || s.Kind != nil {
			t.Fatalf("status = %+v", s)
		}
	})
	t.Run("ST-T363 a refreshed login is saved to tokens.json with its saved_at kept", func(t *testing.T) {
		_, r, before := expiringScenario(t, 0)
		if _, err := Export(context.Background(), r, &recorder{}); err != nil {
			t.Fatal(err)
		}
		after := tokensFileOf(t, r.DataDir)
		var b, a struct {
			SavedAt string `json:"saved_at"`
			DI      struct {
				AccessToken string `json:"access_token"`
			} `json:"di"`
		}
		json.Unmarshal(before, &b)
		json.Unmarshal(after, &a)
		if a.DI.AccessToken == b.DI.AccessToken || a.SavedAt != b.SavedAt {
			t.Fatalf("before %s\nafter %s", before, after)
		}
	})
}

func TestExportDropsStaleMfpFiles(t *testing.T) {
	t.Run("EG-370 stale mfp_*.csv files in the csv folder are deleted", func(t *testing.T) {
		_, r := exportScenario(t)
		dir := filepath.Join(r.OutputDir, "csv_2026-10-07")
		os.MkdirAll(dir, 0o755)
		for _, f := range mfpFiles {
			os.WriteFile(filepath.Join(dir, f), []byte("old\n"), 0o644)
		}
		if _, err := Export(context.Background(), r, &recorder{}); err != nil {
			t.Fatal(err)
		}
		for _, f := range mfpFiles {
			if _, err := os.Stat(filepath.Join(dir, f)); !os.IsNotExist(err) {
				t.Errorf("%s kept", f)
			}
		}
	})
}

func TestExportStopsOnTheLogin(t *testing.T) {
	t.Run("EG-371 a 403 on social_profile ends with GARMIN_BLOCKED and keeps tokens.json", func(t *testing.T) {
		srv, r := exportScenario(t)
		before := tokensFileOf(t, r.DataDir)
		srv.Set(func(c *gconnecttest.Control) { c.DataStatus = map[string]int{"/userprofile-service": 403} })
		_, err := Export(context.Background(), r, &recorder{})
		if codeOf(err) != "GARMIN_BLOCKED" {
			t.Fatalf("err = %v", err)
		}
		if !bytes.Equal(tokensFileOf(t, r.DataDir), before) {
			t.Fatal("tokens.json changed")
		}
	})
	t.Run("EG-372 a second 401 on data after a refresh writes the expired marker and ends with GARMIN_SESSION", func(t *testing.T) {
		srv, r := exportScenario(t)
		srv.Set(func(c *gconnecttest.Control) { c.DataStatus = map[string]int{"/userprofile-service": 401} })
		_, err := Export(context.Background(), r, &recorder{})
		if codeOf(err) != "GARMIN_SESSION" || err.Error() != "The Garmin session expired. Reconnect Garmin." {
			t.Fatalf("err = %v", err)
		}
		if len(srv.Calls("/di-oauth2-service/oauth/token")) == 0 {
			t.Fatal("no refresh before the second 401")
		}
		if s := StatusOf(r.DataDir, time.Now()); !s.Expired || s.Kind != nil {
			t.Fatalf("status = %+v", s)
		}
	})
	t.Run("EG-373 a marker that cannot be written logs a SAVE_FAILED warning and still ends with GARMIN_SESSION", func(t *testing.T) {
		srv, r := exportScenario(t)
		srv.Set(func(c *gconnecttest.Control) { c.DataStatus = map[string]int{"/userprofile-service": 401} })
		if err := os.Chmod(r.DataDir, 0o500); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { os.Chmod(r.DataDir, 0o700) })
		rec := &recorder{}
		_, err := Export(context.Background(), r, rec)
		if codeOf(err) != "GARMIN_SESSION" {
			t.Fatalf("err = %v", err)
		}
		if !slices.ContainsFunc(rec.warns, func(w string) bool {
			return strings.HasPrefix(w, "SAVE_FAILED Could not mark the Garmin login as expired")
		}) {
			t.Fatalf("warnings: %q", rec.warns)
		}
	})
	t.Run("EG-374 a 429 on a daily answer of the second date keeps both started days on disk", func(t *testing.T) {
		srv, r := exportScenario(t)
		srv.Set(func(c *gconnecttest.Control) { c.DataStatus = map[string]int{"/hrv-service/hrv/2024-02-20": 429} })
		_, err := Export(context.Background(), r, &recorder{})
		if codeOf(err) != "GARMIN_RATE_LIMITED" {
			t.Fatalf("err = %v", err)
		}
		dir := filepath.Join(r.DataDir, "garmin-cache", "daily")
		for date, want := range map[string]bool{"2024-02-21": true, "2024-02-20": true, "2024-02-19": false} {
			if _, err := os.Stat(filepath.Join(dir, date+".json")); (err == nil) != want {
				t.Errorf("%s on disk = %v, want %v", date, err == nil, want)
			}
		}
		if e := (Cache{Dir: filepath.Join(r.DataDir, "garmin-cache")}).Day("2024-02-20"); e["stats"].Data == nil || e["hrv"].Data != nil {
			t.Fatalf("2024-02-20 holds %v", slices.Collect(maps.Keys(e)))
		}
	})
}

// The user's own login is legacy (oauth1, oauth2, consumer), written by Bun: the first Go
// export runs on that file with an expired OAuth2.
func TestExportOnABunLegacyLoginWithAnExpiredOAuth2(t *testing.T) {
	srv, r := exportScenario(t)
	if err := os.WriteFile(filepath.Join(r.DataDir, "tokens.json"), []byte(strings.Replace(bunLegacy, `"expires_at":1760540000`, `"expires_at":1`, 1)), 0o600); err != nil {
		t.Fatal(err)
	}
	res, err := Export(context.Background(), r, &recorder{})
	if err != nil {
		t.Fatal(err)
	}
	saved, err := LoadTokens(r.DataDir)
	if err != nil || saved.OAuth2 == nil || !strings.HasPrefix(saved.OAuth2.AccessToken, "legacy-") {
		t.Fatalf("saved tokens = %+v %v", saved, err)
	}
	t.Run("AC-363 a legacy login exports CSV files equal to the golden files", func(t *testing.T) {
		sameFiles(t, res.Path, goldenDir, goldenNames(t))
	})
	t.Run("EG-375 the expired OAuth2 is renewed by one OAuth1 to OAuth2 exchange, with no sign in", func(t *testing.T) {
		if n := len(srv.Calls("/oauth-service/oauth/exchange/user/2.0")); n != 1 {
			t.Fatalf("%d exchange calls, want 1", n)
		}
		for _, c := range srv.Calls("") {
			if strings.HasPrefix(c.Path, "/sso/") || strings.HasPrefix(c.Path, "/di-oauth2-service/") || c.Path == "/oauth-service/oauth/preauthorized" {
				t.Errorf("the export signed in: %s", c.Path)
			}
		}
	})
	t.Run("EG-376 every data request carries the mobile app User-Agent and none of the DI native headers", func(t *testing.T) {
		n := 0
		for _, c := range srv.Calls("") {
			if strings.HasPrefix(c.Path, "/oauth-service/") {
				continue
			}
			n++
			if ua := c.Header.Get("User-Agent"); ua != "com.garmin.android.apps.connectmobile" || c.Header.Get("X-Garmin-User-Agent") != "" {
				t.Fatalf("%s: User-Agent %q, X-Garmin-User-Agent %q", c.Path, ua, c.Header.Get("X-Garmin-User-Agent"))
			}
			if c.Header.Get("Authorization") != "Bearer "+saved.OAuth2.AccessToken {
				t.Fatalf("%s: Authorization %q", c.Path, c.Header.Get("Authorization"))
			}
		}
		if n == 0 {
			t.Fatal("no data request")
		}
	})
	t.Run("CL-280 tokens.json keeps the legacy shape of auth.js after the save", func(t *testing.T) {
		var back map[string]json.RawMessage
		if err := json.Unmarshal(tokensFileOf(t, r.DataDir), &back); err != nil {
			t.Fatal(err)
		}
		if got := slices.Sorted(maps.Keys(back)); !slices.Equal(got, []string{"consumer", "oauth1", "oauth2", "saved_at"}) {
			t.Fatalf("keys = %v", got)
		}
		var o2 map[string]any
		_ = json.Unmarshal(back["oauth2"], &o2)
		if o2["access_token"] != saved.OAuth2.AccessToken || o2["token_type"] != "Bearer" || o2["expires_at"].(float64) <= 1 {
			t.Fatalf("oauth2 = %v", o2)
		}
		for _, k := range []string{"refresh_token", "refresh_token_expires_at"} {
			if _, ok := o2[k]; !ok {
				t.Errorf("oauth2.%s missing", k)
			}
		}
		if string(back["oauth1"]) == "" || !strings.Contains(string(back["oauth1"]), `"ot"`) || !strings.Contains(string(back["consumer"]), `"ck"`) {
			t.Fatalf("oauth1 or consumer changed: %s %s", back["oauth1"], back["consumer"])
		}
	})
}
