package garmin

import (
	"context"
	"math"
	"time"

	"github.com/danihrndzld/garmin-health-exporter/tauri-app/core/internal/jsjson"
	"github.com/danihrndzld/gconnect"
)

// personalLimits names what the Personal API does not serve (404 on 2026-10-24), for the one
// warning a run with the API key alone logs instead of one [skip] per endpoint and day.
const personalLimits = "API key only: Garmin's Personal API has no training readiness, training status, VO2 max, " +
	"endurance or hill score, race predictions, laps, goals, blood pressure or body composition. " +
	"Connect with email too to add them."

// personalAggregated are the aggregated names the Personal API answers, with the endpoint to ask.
var personalAggregated = map[string]string{
	"daily_steps":   "daily_steps", // same path on both hosts, 28 days per request
	"agg_weigh_ins": "agg_weigh_ins",
	"fitness_age":   "personal_fitness_age",
}

// chunks splits start..end (inclusive ISO dates) into ranges of at most n days.
func chunks(start, end string, n int) [][2]string {
	var out [][2]string
	s, _ := time.Parse("2006-01-02", start)
	e, _ := time.Parse("2006-01-02", end)
	for cur := s; !cur.After(e); {
		last := cur.AddDate(0, 0, n-1)
		if last.After(e) {
			last = e
		}
		out = append(out, [2]string{cur.Format("2006-01-02"), last.Format("2006-01-02")})
		cur = last.AddDate(0, 0, 1)
	}
	return out
}

// pick walks src through keys; a missing step is undefined.
func pick(src jsjson.Value, keys ...string) jsjson.Value {
	for _, k := range keys {
		src = src.Get(k)
	}
	return src
}

// fields builds an object from name, path pairs and leaves out what src does not hold, so
// copyKeys prints an empty cell for it as it does for a login export without that key.
func fields(src jsjson.Value, pairs ...[]string) jsjson.Value {
	o := jsjson.NewObj()
	for _, p := range pairs {
		if v := pick(src, p[1:]...); !v.IsNullish() {
			o.Set(p[0], v)
		}
	}
	return obj(o)
}

func at(name string, path ...string) []string { return append([]string{name}, path...) }

var statsFields = [][]string{
	at("totalKilocalories", "calories", "burnedTotal"),
	at("activeKilocalories", "calories", "burnedActive"),
	at("bmrKilocalories", "calories", "burnedResting"),
	at("totalSteps", "movement", "steps", "value"),
	at("totalDistanceMeters", "movement", "steps", "distanceInMeters"),
	at("wellnessKilocalories", "calories", "burnedTotal"),
	at("remainingKilocalories", "calories", "consumedRemaining"),
	at("floorsAscended", "movement", "floorsAscended", "distanceInMeters"),
	at("floorsDescended", "movement", "floorsDescended", "distanceInMeters"),
	at("durationInMilliseconds", "wellnessChronology", "totalDurationInMillis"),
	at("dailyStepGoal", "movement", "steps", "goal"),
	at("moderateIntensityMinutes", "intensityMinutes", "moderate"),
	at("vigorousIntensityMinutes", "intensityMinutes", "vigorous"),
}

// floor is the 3.048 m (10 ft) floor the legacy answer counts in, to 5 decimals: the v2 row
// rounds floors to whole numbers but keeps the meters.
const floor = 3.048

// fromSummary maps one usersummary-service/v2/dailysummaries row onto the legacy per-day
// answers csv.go reads. respiration.avgValue is the whole day, where the legacy
// avgWakingRespirationValue left sleep out.
func fromSummary(d jsjson.Value) map[string]jsjson.Value {
	stats := fields(d, statsFields...)
	for _, k := range []string{"floorsAscended", "floorsDescended"} {
		if m := stats.Get(k); m.Kind == jsjson.Number {
			stats.O.Set(k, jsjson.Num(math.Round(m.N/floor*1e5)/1e5))
		}
	}
	return map[string]jsjson.Value{
		"stats": stats,
		"heart_rates": fields(d,
			at("maxHeartRate", "heartRate", "maxValue"),
			at("minHeartRate", "heartRate", "minValue"),
			at("restingHeartRate", "heartRate", "restingValue"),
		),
		"stress": fields(d,
			at("maxStressLevel", "stress", "maxLevel"),
			at("avgStressLevel", "stress", "avgLevel"),
		),
		"intensity_minutes": fields(d,
			at("moderateMinutes", "intensityMinutes", "moderate"),
			at("vigorousMinutes", "intensityMinutes", "vigorous"),
		),
		"respiration": fields(d,
			at("avgWakingRespirationValue", "respiration", "avgValue"),
			at("highestRespirationValue", "respiration", "maxValue"),
			at("lowestRespirationValue", "respiration", "minValue"),
		),
		"spo2": fields(d,
			at("averageSpO2", "pulseOx", "avgValue"),
			at("lowestSpO2", "pulseOx", "minValue"),
		),
	}
}

// personalDays asks the Personal API for start..end in a few range requests and returns,
// per date, the legacy daily answers it can stand in for. It stops at the first request that
// stops the client; the caller reads c.Stopped().
func personalDays(ctx context.Context, c *gconnect.Client, start, end string) map[string]map[string]jsjson.Value {
	days := map[string]map[string]jsjson.Value{}
	// A night without data comes as a row of nulls; its "" date is never read.
	put := func(date jsjson.Value, name string, v jsjson.Value) {
		if days[date.S] == nil {
			days[date.S] = map[string]jsjson.Value{}
		}
		days[date.S][name] = v
	}
	each := func(name string, size int, fn func(jsjson.Value)) {
		for _, ch := range chunks(start, end, size) {
			if c.Stopped() != nil {
				return
			}
			fn(parse(c.Safe(ctx, endpoint(name), gconnect.Params{Start: ch[0], End: ch[1]})))
		}
	}
	each("personal_daily_summaries", 90, func(v jsjson.Value) {
		for _, d := range list(v.Get("data")) {
			for name, val := range fromSummary(d) {
				put(d.Get("calendarDate"), name, val)
			}
		}
	})
	each("personal_hrv", 90, func(v jsjson.Value) {
		for _, h := range list(v.Get("hrvSummaries")) {
			o := jsjson.NewObj()
			o.Set("hrvSummary", h)
			put(h.Get("calendarDate"), "hrv", obj(o))
		}
	})
	each("personal_body_battery", 31, func(v jsjson.Value) {
		for _, b := range list(v) {
			put(b.Get("date"), "body_battery", jsjson.Value{Kind: jsjson.Array, A: []jsjson.Value{b}})
		}
	})
	each("personal_sleep", 31, func(v jsjson.Value) {
		for _, s := range list(v) {
			date := s.Get("calendarDate")
			o := jsjson.NewObj()
			o.Set("dailySleepDTO", s)
			if date.Kind == jsjson.String {
				day := days[date.S]
				// The legacy sleep answer carried these two next to the DTO.
				if hrv := pick(day["hrv"], "hrvSummary", "lastNightAvg"); !hrv.IsNullish() {
					o.Set("avgOvernightHrv", hrv)
				}
				if rhr := day["heart_rates"].Get("restingHeartRate"); !rhr.IsNullish() {
					o.Set("restingHeartRate", rhr)
				}
			}
			put(date, "sleep", obj(o))
		}
	})
	return days
}
