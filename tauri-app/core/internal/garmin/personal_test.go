package garmin

import (
	"context"
	"encoding/csv"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/danihrndzld/gconnect"
	"github.com/danihrndzld/gconnect/gconnecttest"
)

// FRs for the Personal API (sources: Garmin's Personal API FAQ and Python client, the probes
// of 2026-10-24 recorded in workflows/personal_api.md, and the user's ask "api key preferred if
// possible"):
//   FR-1 garmin-connect with a key Garmin accepts saves garmin-api-key.json (0600) and leaves tokens.json alone.
//   FR-2 a key Garmin refuses answers GARMIN_BAD_KEY and saves nothing.
//   FR-3 an export with the key alone fills the daily CSV columns from the Personal API ranges,
//        sends no request the Personal API would answer 404, and warns once about what is missing.
//   FR-4 floors are the legacy 3.048 m floors, to 5 decimals.
//   FR-5 range requests respect Garmin's limits: 90 days for summaries and HRV, 31 for sleep and Body Battery.
//   FR-6 with the key and the login, the login asks only for what the key cannot answer.
//   FR-7 a key Garmin refuses during an export marks garmin-api-key.json expired, never tokens.json.
//   FR-8 an expired key next to a live login: the export runs on the login and says the key expired.
//   FR-9 garmin-status reports the key apart from the login.
// Every answer below is written by hand in the shape the real service answered.

const testKey = "right-api-key"

// personalRoutes answers for day (the full row) and the day after it (a sparse row).
func personalRoutes(srv *gconnecttest.Server, day, next string) {
	js := func(body string) http.HandlerFunc {
		body = strings.NewReplacer("2026-10-10", day, "2026-10-11", next).Replace(body)
		return func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			w.Write([]byte(body))
		}
	}
	srv.Handle("/usersummary-service/v2/dailysummaries", js(`{"data":[
		{"calendarDate":"2026-10-10","wellnessChronology":{"totalDurationInMillis":86400000},
		 "movement":{"steps":{"goal":4550,"value":1647,"distanceInMeters":1289.0},"floorsAscended":{"value":5,"distanceInMeters":15.24},"floorsDescended":{"value":3,"distanceInMeters":9.28}},
		 "calories":{"burnedResting":2154,"burnedActive":30,"burnedTotal":2184,"consumedRemaining":2590},
		 "heartRate":{"minValue":59,"maxValue":106,"restingValue":60},"intensityMinutes":{"moderate":12,"vigorous":3},
		 "stress":{"avgLevel":26,"maxLevel":87},"respiration":{"avgValue":14,"maxValue":22,"minValue":8},"pulseOx":{"avgValue":96,"minValue":92}},
		{"calendarDate":"2026-10-11","movement":{"steps":{"value":1595}},"heartRate":{"restingValue":59}}]}`))
	srv.Handle("/sleep-service/sleep/dailySleepSummariesByDate", js(`[
		{"calendarDate":"2026-10-10","sleepTimeSeconds":25200,"deepSleepSeconds":3600,"lowestRespirationValue":8},
		{"calendarDate":null,"sleepTimeSeconds":null}]`))
	srv.Handle("/hrv-service/hrv/daily/", js(`{"hrvSummaries":[{"calendarDate":"2026-10-10","weeklyAvg":41,"lastNightAvg":44,"status":"BALANCED"}]}`))
	srv.Handle("/wellness-service/wellness/bodyBattery/reports/daily", js(`[{"date":"2026-10-10","charged":66,"drained":21,"startTimestampLocal":"2026-10-10T00:00:00.0","endTimestampLocal":"2026-10-11T00:00:00.0"}]`))
	srv.Handle("/usersummary-service/stats/steps/daily/", js(`[{"calendarDate":"2026-10-10","totalSteps":1647,"totalDistance":1289,"stepGoal":4550}]`))
	srv.Handle("/weight-service/weight/range/", js(`{"dailyWeightSummaries":[]}`))
	srv.Handle("/fitnessage-service/fitnessage/v2/", js(`{"fitnessAge":31.5,"chronologicalAge":38}`))
	srv.Handle("/activitylist-service/activities/search/activities", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("start") != "0" {
			w.Write([]byte(`[]`))
			return
		}
		w.Write([]byte(`[{"activityId":1001,"activityName":"Treadmill Running","activityType":{"typeKey":"treadmill_running"},"startTimeLocal":"2026-10-10 06:30:00","distance":5000}]`))
	})
}

func homeTemp(t *testing.T) string {
	t.Helper()
	home, _ := os.UserHomeDir()
	dir, err := os.MkdirTemp(home, ".garmin-personal-test-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(dir) })
	return dir
}

// keyScenario saves the key the way garmin-connect does and returns a key only export of
// 2026-10-10..2026-10-11.
func keyScenario(t *testing.T) (*gconnecttest.Server, ExportRequest) {
	t.Helper()
	srv, a := fakeGarmin(t)
	personalRoutes(srv, "2026-10-10", "2026-10-11")
	root := homeTemp(t)
	r := ExportRequest{StartDate: "2026-10-10", EndDate: "2026-10-11", RefreshWindow: 3,
		OutputDir: filepath.Join(root, "out"), DataDir: filepath.Join(root, "data"), Hosts: srv.Hosts(), Gap: -1,
		Now: func() time.Time { return time.Date(2026, 10, 12, 12, 0, 0, 0, time.Local) }}
	if _, err := ConnectAPIKey(context.Background(), a, r.DataDir, testKey, time.Now()); err != nil {
		t.Fatal(err)
	}
	srv.Reset()
	return srv, r
}

// cell reads one cell of a written CSV by the value of its first column and a header name.
func cell(t *testing.T, path, row, col string) string {
	t.Helper()
	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	rows, err := csv.NewReader(f).ReadAll()
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range rows[1:] {
		if r[0] == row {
			for i, h := range rows[0] {
				if h == col {
					return r[i]
				}
			}
			t.Fatalf("%s has no column %s", path, col)
		}
	}
	t.Fatalf("%s has no row %s", path, row)
	return ""
}

func TestConnectAPIKey(t *testing.T) {
	t.Run("AC-701 FR-1 an accepted key: garmin-api-key.json with mode 0600, status connected, no tokens.json", func(t *testing.T) {
		srv, a := fakeGarmin(t)
		dir := homeTemp(t)

		s, err := ConnectAPIKey(context.Background(), a, dir, testKey, time.Now())

		fi, statErr := os.Stat(filepath.Join(dir, "garmin-api-key.json"))
		if err != nil || statErr != nil || fi.Mode().Perm() != 0o600 || !s.APIKey.Connected || s.Connected {
			t.Fatalf("status %+v, err %v, stat %v", s, err, statErr)
		}
		if _, err := os.Stat(filepath.Join(dir, "tokens.json")); !os.IsNotExist(err) || len(srv.Calls("/mobile/api/login")) != 0 {
			t.Fatal("the key wrote tokens.json or signed in")
		}
	})
	t.Run("EP-701 FR-2 a refused key: GARMIN_BAD_KEY and no file", func(t *testing.T) {
		_, a := fakeGarmin(t)
		dir := homeTemp(t)

		_, err := ConnectAPIKey(context.Background(), a, dir, "wrong-key", time.Now())

		if codeOf(err) != "GARMIN_BAD_KEY" {
			t.Fatalf("err %v", err)
		}
		if _, statErr := os.Stat(filepath.Join(dir, "garmin-api-key.json")); !os.IsNotExist(statErr) {
			t.Fatal("a refused key was saved")
		}
	})
	t.Run("EP-702 FR-2 Garmin's token service down (503): GARMIN_ERROR, not a bad key", func(t *testing.T) {
		srv, a := fakeGarmin(t)
		srv.Set(func(c *gconnecttest.Control) { c.APIKeyStatus = 503 })

		_, err := ConnectAPIKey(context.Background(), a, homeTemp(t), testKey, time.Now())

		if codeOf(err) != "GARMIN_ERROR" {
			t.Fatalf("err %v", err)
		}
	})
}

func TestExportWithTheKeyAlone(t *testing.T) {
	srv, r := keyScenario(t)
	rec := &recorder{}
	res, err := Export(context.Background(), r, rec)
	if err != nil {
		t.Fatal(err)
	}
	daily := filepath.Join(res.Path, "daily_summary.csv")
	t.Run("AC-702 FR-3 daily_summary.csv holds the v2 summary values under the legacy column names", func(t *testing.T) {
		want := map[string]string{"totalKilocalories": "2184", "activeKilocalories": "30", "bmrKilocalories": "2154",
			"wellnessKilocalories": "2184", "totalSteps": "1647", "totalDistanceMeters": "1289", "stepGoal": "4550",
			"restingHeartRate": "60", "maxHeartRate": "106", "avgStressLevel": "26", "maxStressLevel": "87",
			"moderateIntensityMinutes": "12", "vigorousIntensityMinutes": "3", "durationInMilliseconds": "86400000"}
		for col, v := range want {
			if got := cell(t, daily, "2026-10-10", col); got != v {
				t.Errorf("%s = %q, want %q", col, got, v)
			}
		}
	})
	t.Run("BVA-701 FR-4 floors: 15.24 m is 5 floors and 9.28 m is 3.04462, as the legacy answer counts them", func(t *testing.T) {
		if a, d := cell(t, daily, "2026-10-10", "floorsAscended"), cell(t, daily, "2026-10-10", "floorsDescended"); a != "5" || d != "3.04462" {
			t.Fatalf("floors %s and %s", a, d)
		}
	})
	t.Run("AC-703 FR-3 sleep, HRV, Body Battery and SpO2 rows come from the ranges, with HRV and resting HR next to sleep", func(t *testing.T) {
		got := []string{
			cell(t, filepath.Join(res.Path, "sleep.csv"), "2026-10-10", "sleepTimeSeconds"),
			cell(t, filepath.Join(res.Path, "sleep.csv"), "2026-10-10", "avgOvernightHrv"),
			cell(t, filepath.Join(res.Path, "sleep.csv"), "2026-10-10", "restingHeartRate"),
			cell(t, filepath.Join(res.Path, "hrv.csv"), "2026-10-10", "lastNightAvg"),
			cell(t, filepath.Join(res.Path, "body_battery.csv"), "2026-10-10", "charged"),
			cell(t, filepath.Join(res.Path, "spo2_respiration.csv"), "2026-10-10", "spo2_avg"),
			cell(t, filepath.Join(res.Path, "spo2_respiration.csv"), "2026-10-10", "respiration_lowest"),
		}
		if strings.Join(got, ",") != "25200,44,60,44,66,96,8" {
			t.Fatalf("got %v", got)
		}
	})
	t.Run("CL-701 FR-3 no request goes to a path the Personal API answers 404, and nothing signs in", func(t *testing.T) {
		for _, c := range srv.Calls("") {
			if c.Path == "/userprofile-service/socialProfile" || strings.Contains(c.Path, "metrics-service") ||
				strings.HasSuffix(c.Path, "/splits") || strings.HasPrefix(c.Path, "/goal-service") || c.Path == "/mobile/api/login" {
				t.Errorf("sent %s", c.Path)
			}
		}
	})
	t.Run("CL-702 FR-3 one PERSONAL_API_LIMITS warning and no [skip] warning", func(t *testing.T) {
		if len(rec.warns) != 1 || !strings.HasPrefix(rec.warns[0], "PERSONAL_API_LIMITS ") {
			t.Fatalf("warnings %v", rec.warns)
		}
	})
	t.Run("AC-704 FR-3 the activity list comes from the Personal API", func(t *testing.T) {
		if res.Activities != 1 || cell(t, filepath.Join(res.Path, "activities.csv"), "1001", "activityName") != "Treadmill Running" {
			t.Fatalf("activities %d", res.Activities)
		}
	})
}

func TestPersonalRangeLimits(t *testing.T) {
	for _, c := range []struct {
		name          string
		start, end    string
		summaries, sl int
	}{
		{"BVA-702 FR-5 31 days: one sleep and one summary request", "2026-08-01", "2026-08-31", 1, 1},
		{"BVA-703 FR-5 32 days: two sleep requests, one summary request", "2026-08-01", "2026-09-01", 1, 2},
		{"BVA-704 FR-5 90 days: one summary request, three sleep requests", "2026-07-01", "2026-09-28", 1, 3},
	} {
		t.Run(c.name, func(t *testing.T) {
			srv, r := keyScenario(t)
			r.StartDate, r.EndDate = c.start, c.end

			if _, err := Export(context.Background(), r, &recorder{}); err != nil {
				t.Fatal(err)
			}

			s, sl := len(srv.Calls("/usersummary-service/v2/dailysummaries")), len(srv.Calls("/sleep-service/sleep/dailySleepSummariesByDate"))
			if s != c.summaries || sl != c.sl {
				t.Fatalf("%d summary and %d sleep requests, want %d and %d", s, sl, c.summaries, c.sl)
			}
		})
	}
}

func TestExportWithTheKeyAndTheLogin(t *testing.T) {
	srv, r := exportScenario(t) // the login and its fixture, 2024-02-17..2024-02-21
	personalRoutes(srv, "2024-02-21", "2024-02-22")
	if _, err := ConnectAPIKey(context.Background(), &gconnect.Auth{Hosts: srv.Hosts()}, r.DataDir, testKey, time.Now()); err != nil {
		t.Fatal(err)
	}
	srv.Reset()
	rec := &recorder{}

	res, err := Export(context.Background(), r, rec)

	if err != nil {
		t.Fatal(err)
	}
	t.Run("AC-705 FR-6 the login asks for training readiness, splits and goals, which the key cannot answer", func(t *testing.T) {
		if len(srv.Calls("/metrics-service/metrics/trainingreadiness/2024-02-21")) != 1 || len(srv.Calls("/goal-service/goal/goals")) != 1 || res.Activities == 0 {
			t.Fatalf("readiness %d, goals %d, activities %d", len(srv.Calls("/metrics-service/metrics/trainingreadiness/2024-02-21")), len(srv.Calls("/goal-service/goal/goals")), res.Activities)
		}
	})
	t.Run("CL-703 FR-6 for a day the key answered, the login never asks for daily stress, HRV or sleep", func(t *testing.T) {
		for _, p := range []string{"/wellness-service/wellness/dailyStress/2024-02-21", "/hrv-service/hrv/2024-02-21"} {
			if n := len(srv.Calls(p)); n != 0 {
				t.Errorf("%s asked %d times", p, n)
			}
		}
		for _, c := range srv.Calls("") {
			if strings.HasPrefix(c.Path, "/wellness-service/wellness/dailySleepData/") && strings.Contains(c.Query, "date=2024-02-21") {
				t.Errorf("sent %s?%s", c.Path, c.Query)
			}
		}
	})
	t.Run("CL-705 FR-6 for a day the key had nothing, the login still asks for daily stress", func(t *testing.T) {
		if n := len(srv.Calls("/wellness-service/wellness/dailyStress/2024-02-20")); n != 1 {
			t.Fatalf("asked %d times", n)
		}
	})
	t.Run("CL-704 FR-6 no PERSONAL_API_LIMITS warning when the login fills the gaps", func(t *testing.T) {
		for _, w := range rec.warns {
			if strings.HasPrefix(w, "PERSONAL_API_LIMITS") {
				t.Fatalf("warned %s", w)
			}
		}
	})
}

func TestExportWhenTheKeyDies(t *testing.T) {
	t.Run("ST-T701 FR-7 Garmin refuses the key when the token is renewed: GARMIN_SESSION, the key file is the expired marker, tokens.json kept", func(t *testing.T) {
		srv, r := exportScenario(t)
		personalRoutes(srv, "2024-02-21", "2024-02-22")
		if _, err := ConnectAPIKey(context.Background(), &gconnect.Auth{Hosts: srv.Hosts()}, r.DataDir, testKey, time.Now()); err != nil {
			t.Fatal(err)
		}
		f, _ := readFile(r.DataDir, apiKeyFile)
		f.Personal.ExpiresAt = 1 // the next request renews the token
		f.saveAs(r.DataDir, apiKeyFile)
		before := tokensFileOf(t, r.DataDir)
		srv.Set(func(c *gconnecttest.Control) { c.APIKeyStatus = 401 })

		_, err := Export(context.Background(), r, &recorder{})

		after, _ := readFile(r.DataDir, apiKeyFile)
		if codeOf(err) != "GARMIN_SESSION" || !strings.Contains(err.Error(), "API key") || after.ExpiredAt == "" || after.Personal != nil {
			t.Fatalf("err %v, key file %+v", err, after)
		}
		if string(tokensFileOf(t, r.DataDir)) != string(before) {
			t.Fatal("tokens.json changed")
		}
	})
	t.Run("ST-T702 FR-8 an expired key next to a live login: the export runs on the login and warns GARMIN_KEY_EXPIRED", func(t *testing.T) {
		srv, r := exportScenario(t)
		markExpired(r.DataDir, apiKeyFile, time.Now())
		rec := &recorder{}

		_, err := Export(context.Background(), r, rec)

		if err != nil || len(srv.Calls(gconnect.PersonalTokenPath)) != 0 || !strings.HasPrefix(strings.Join(rec.warns, "|"), "GARMIN_KEY_EXPIRED ") {
			t.Fatalf("err %v, warnings %v", err, rec.warns)
		}
	})
	t.Run("ST-T703 FR-7 an expired key and no login: GARMIN_SESSION names the key, nothing is sent", func(t *testing.T) {
		srv, a := fakeGarmin(t)
		dir := homeTemp(t)
		markExpired(dir, apiKeyFile, time.Now())
		r := ExportRequest{StartDate: "2026-10-10", EndDate: "2026-10-11", RefreshWindow: 3, OutputDir: dir, DataDir: dir, Hosts: a.Hosts, Gap: -1}

		_, err := Export(context.Background(), r, &recorder{})

		if codeOf(err) != "GARMIN_SESSION" || !strings.Contains(err.Error(), "API key") || len(srv.Calls("")) != 0 {
			t.Fatalf("err %v, %d requests", err, len(srv.Calls("")))
		}
	})
}

func TestStatusReportsTheKeyApart(t *testing.T) {
	t.Run("ST-T704 FR-9 no key, a key, an expired key: apiKey connected and expired follow the file, the login fields stay off", func(t *testing.T) {
		dir := homeTemp(t)
		none := StatusOf(dir, time.Now())
		(&tokenFile{Tokens: gconnect.Tokens{Personal: &gconnect.Personal{APIKey: "k"}}}).saveAs(dir, apiKeyFile)
		live := StatusOf(dir, time.Now())
		markExpired(dir, apiKeyFile, time.Now())

		dead := StatusOf(dir, time.Now())

		if none.APIKey.Connected || !live.APIKey.Connected || live.APIKey.Expired || !dead.APIKey.Connected || !dead.APIKey.Expired || dead.Connected || dead.APIKey.SavedAt == nil {
			t.Fatalf("none %+v, live %+v, dead %+v", none.APIKey, live.APIKey, dead)
		}
	})
}
