package garmintest_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/danihrndzld/garmin-health-exporter/tauri-app/core/internal/garmin/garmintest"
	"github.com/danihrndzld/garmin-health-exporter/tauri-app/core/internal/jsjson"
	"github.com/danihrndzld/gconnect"
	"github.com/danihrndzld/gconnect/gconnecttest"
)

var fixturePath = filepath.Join("..", "..", "..", "..", "..", "electron-app", "garmin", "__tests__", "fixtures", "export-sample.json")

func TestServeFixture(t *testing.T) {
	ctx := context.Background()
	srv := gconnecttest.New()
	t.Cleanup(srv.Close)
	if err := garmintest.Serve(srv, fixturePath); err != nil {
		t.Fatal(err)
	}
	tok, err := (&gconnect.Auth{Hosts: srv.Hosts()}).Login(ctx, "e@x", "right-password")
	if err != nil {
		t.Fatal(err)
	}
	c := &gconnect.Client{Auth: &gconnect.Auth{Hosts: srv.Hosts()}, Tokens: tok, Gap: -1}
	get := func(name string, p gconnect.Params) gconnect.Result {
		e, _ := gconnect.Lookup(name)
		return c.Get(ctx, e, p)
	}
	raw, _ := os.ReadFile(fixturePath)
	fx, _ := jsjson.Parse(raw)
	start, end := fx.Get("date_range").Get("start").S, fx.Get("date_range").Get("end").S

	t.Run("CL-339 daily and aggregated answers equal JSON.stringify of the fixture; a null day answers 204", func(t *testing.T) {
		nulls := 0
		for _, date := range fx.Get("daily").O.Keys {
			for _, name := range []string{"stats", "hrv", "sleep"} {
				res := get(name, gconnect.Params{Date: date, DisplayName: garmintest.DisplayName})
				want := fx.Get("daily").Get(date).Get(name)
				if want.IsNullish() {
					nulls++
					if res.Status != 204 || res.Data != nil {
						t.Errorf("%s %s: %+v", name, date, res)
					}
				} else if string(res.Data) != jsjson.Stringify(want) {
					t.Errorf("%s %s: answer differs", name, date)
				}
			}
		}
		if nulls == 0 {
			t.Error("the fixture has no null day to check")
		}
		res := get("hill_score", gconnect.Params{Start: start, End: end})
		if want := fx.Get("aggregated").Get("hill_score"); res.Code != "" || (!want.IsNullish() && string(res.Data) != jsjson.Stringify(want)) {
			t.Errorf("hill_score: %+v", res)
		}
	})

	t.Run("CL-340 the activity list pages without splits; splits, steps chunks and goals are served", func(t *testing.T) {
		acts := fx.Get("activities").A
		page := get("activities_by_date", gconnect.Params{Start: start, End: end, Offset: 1, Limit: 2})
		got, _ := jsjson.Parse(page.Data)
		if len(got.A) != min(2, len(acts)-1) || strings.Contains(string(page.Data), `"splits"`) {
			t.Fatalf("page = %.200s", page.Data)
		}
		if jsjson.Cell(got.A[0].Get("activityId")) != jsjson.Cell(acts[1].Get("activityId")) {
			t.Fatal("the page starts at the wrong activity")
		}
		id := jsjson.Cell(acts[0].Get("activityId"))
		if res := get("activity_splits", gconnect.Params{ActivityID: id}); res.Code != "" || (res.Data == nil) != acts[0].Get("splits").IsNullish() {
			t.Fatalf("splits: %+v", res)
		}
		if res := get("daily_steps", gconnect.Params{Start: "2024-01-01", End: "2024-01-28"}); res.Code != "" || res.Data == nil {
			t.Fatalf("steps chunk: %+v", res)
		}
		if res := get("goals", gconnect.Params{}); res.Code != "" || string(res.Data) != jsjson.Stringify(fx.Get("goals")) {
			t.Fatalf("goals: %+v", res)
		}
	})
}
