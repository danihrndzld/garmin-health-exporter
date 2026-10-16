// Package garmintest serves the app's scrubbed export (export-sample.json) through the
// gconnecttest fake. It ports fake-garmin.js routesFor and its data answers (lines 18-37 and
// 127-137); the URLs come from gconnect.Lookup, so the fake and the client agree on them.
package garmintest

import (
	"net/http"
	"os"
	"strconv"
	"strings"

	"github.com/danihrndzld/garmin-health-exporter/tauri-app/core/gconnect"
	"github.com/danihrndzld/garmin-health-exporter/tauri-app/core/gconnect/gconnecttest"
	"github.com/danihrndzld/garmin-health-exporter/tauri-app/core/internal/jsjson"
)

// DisplayName is what the fake's social profile answers.
const DisplayName = "displayName-redacted"

func send(w http.ResponseWriter, v jsjson.Value) {
	if v.IsNullish() {
		w.WriteHeader(204) // no data for that day, like an unsynced watch
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.Write([]byte(jsjson.Stringify(v)))
}

func orEmpty(v jsjson.Value) jsjson.Value {
	if v.IsNullish() {
		return jsjson.Value{Kind: jsjson.Array}
	}
	return v
}

// Serve registers on srv the data routes for the fixture at path.
func Serve(srv *gconnecttest.Server, path string) error {
	raw, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	fx, err := jsjson.Parse(raw)
	if err != nil {
		return err
	}

	routes := map[string]jsjson.Value{} // path?query -> answer
	daily, agg := fx.Get("daily"), fx.Get("aggregated")
	for _, date := range daily.O.Keys {
		for _, ep := range gconnect.Daily() {
			routes[ep.URL("", gconnect.Params{Date: date, DisplayName: DisplayName})] = daily.Get(date).Get(ep.Name)
		}
	}
	rng := fx.Get("date_range")
	for _, ep := range gconnect.Aggregated() {
		if ep.Name != "daily_steps" { // chunked: served by prefix below
			routes[ep.URL("", gconnect.Params{Start: rng.Get("start").S, End: rng.Get("end").S, DisplayName: DisplayName})] = agg.Get(ep.Name)
		}
	}
	splits, _ := gconnect.Lookup("activity_splits")
	var list []jsjson.Value // the activity list carries no splits
	for _, a := range fx.Get("activities").A {
		routes[splits.URL("", gconnect.Params{ActivityID: jsjson.Cell(a.Get("activityId"))})] = a.Get("splits")
		o := jsjson.NewObj()
		for _, k := range a.O.Keys {
			if k != "splits" && k != "typed_splits" {
				o.Set(k, a.Get(k))
			}
		}
		list = append(list, jsjson.Value{Kind: jsjson.Object, O: o})
	}

	seen := map[string]bool{}
	for u := range routes {
		p, _, _ := strings.Cut(u, "?")
		if seen[p] {
			continue
		}
		seen[p] = true
		srv.Handle(p, func(w http.ResponseWriter, r *http.Request) {
			key := r.URL.Path
			if r.URL.RawQuery != "" {
				key += "?" + r.URL.RawQuery
			}
			v, ok := routes[key]
			if !ok {
				http.Error(w, `{"message":"fake garmin has no route for `+key+`"}`, 404)
				return
			}
			send(w, v)
		})
	}
	srv.Handle("/activitylist-service/activities/search/activities", func(w http.ResponseWriter, r *http.Request) {
		start, _ := strconv.Atoi(r.URL.Query().Get("start"))
		limit, err := strconv.Atoi(r.URL.Query().Get("limit"))
		if err != nil {
			limit = 20
		}
		lo, hi := min(start, len(list)), min(start+limit, len(list))
		send(w, jsjson.Value{Kind: jsjson.Array, A: list[lo:hi]})
	})
	srv.Handle("/usersummary-service/stats/steps/daily/", func(w http.ResponseWriter, _ *http.Request) { send(w, orEmpty(agg.Get("daily_steps"))) })
	srv.Handle("/goal-service/goal/goals", func(w http.ResponseWriter, _ *http.Request) { send(w, orEmpty(fx.Get("goals"))) })
	return nil
}
