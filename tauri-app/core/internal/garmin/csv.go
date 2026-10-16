package garmin

import (
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/danihrndzld/garmin-health-exporter/tauri-app/core/internal/jsjson"
)

// Ported from electron-app/garmin/csv-writer.js, one function per JS function, in its order.
// Rows are *jsjson.Obj so the header keeps JavaScript key order.

type value = jsjson.Value

var null = value{Kind: jsjson.Null}

// truthy is JavaScript's ToBoolean, for `x || y` and `if (!x)`.
func truthy(v value) bool {
	switch v.Kind {
	case jsjson.Bool:
		return v.B
	case jsjson.Number:
		return v.N != 0 && v.N == v.N // 0, -0 and NaN are falsy
	case jsjson.String:
		return v.S != ""
	case jsjson.Array, jsjson.Object:
		return true
	}
	return false
}

// orNull is both `x !== undefined ? x : null` and `x ?? null`: each prints an empty cell.
func orNull(v value) value {
	if v.IsNullish() {
		return null
	}
	return v
}

// coalesce is `a ?? b ?? null`.
func coalesce(vs ...value) value {
	for _, v := range vs {
		if !v.IsNullish() {
			return v
		}
	}
	return null
}

// list is `(x || [])` iterated with for...of.
// ponytail: a non array is an empty list; JavaScript throws on a truthy object or number and
// iterates a string's characters. Garmin sends arrays here; port the throw if it ever does not.
func list(v value) []value {
	if v.Kind == jsjson.Array {
		return v.A
	}
	return nil
}

// entries is Object.entries(daily) over an object, in JavaScript key order.
func entries(v value, fn func(date string, d value)) {
	if v.Kind != jsjson.Object {
		return
	}
	for _, k := range v.O.Keys {
		fn(k, v.O.Get(k))
	}
}

func row(kv ...any) *jsjson.Obj {
	r := jsjson.NewObj()
	for i := 0; i < len(kv); i += 2 {
		r.Set(kv[i].(string), kv[i+1].(value))
	}
	return r
}

// copyKeys is `for (const key of keys) row[prefix+key] = src[key] !== undefined ? src[key] : null`.
func copyKeys(r *jsjson.Obj, src value, prefix string, keys ...string) {
	for _, k := range keys {
		r.Set(prefix+k, orNull(src.Get(k)))
	}
}

// csv-writer.js:9-20
var typeGroups = []struct {
	name string
	keys []string
}{
	{"caminar", []string{"walking", "hiking"}},
	{"correr", []string{"running", "treadmill_running"}},
	{"gym", []string{"strength_training", "indoor_cardio"}},
}

func groupFor(typeKey value) string {
	if typeKey.Kind != jsjson.String { // Set.has matches strings only
		return ""
	}
	for _, g := range typeGroups {
		for _, k := range g.keys {
			if k == typeKey.S {
				return g.name
			}
		}
	}
	return ""
}

// csv-writer.js:31-48
func flatten(obj value, prefix string) *jsjson.Obj {
	out := jsjson.NewObj()
	if obj.Kind != jsjson.Object {
		return out
	}
	for _, k := range obj.O.Keys {
		v := obj.O.Get(k)
		key := k
		if prefix != "" {
			key = prefix + "_" + k
		}
		switch v.Kind {
		case jsjson.Object:
			sub := flatten(v, key)
			for _, sk := range sub.Keys {
				out.Set(sk, sub.Get(sk))
			}
		case jsjson.Array:
			out.Set(key, jsjson.Str(jsjson.Stringify(v)))
		default:
			out.Set(key, v)
		}
	}
	return out
}

// csv-writer.js:55-62
func formatCsvField(v value) string {
	if v.IsNullish() {
		return ""
	}
	s := jsjson.Cell(v)
	if strings.ContainsAny(s, ",\"\n\r") {
		return `"` + strings.ReplaceAll(s, `"`, `""`) + `"`
	}
	return s
}

// csv-writer.js:69-100
func writeCsv(rows []*jsjson.Obj, filePath string) error {
	if len(rows) == 0 {
		return nil
	}
	seen := map[string]bool{}
	var keys []string
	for _, r := range rows {
		for _, k := range r.Keys {
			if !seen[k] {
				seen[k] = true
				keys = append(keys, k)
			}
		}
	}
	var sb strings.Builder
	for i, k := range keys {
		if i > 0 {
			sb.WriteByte(',')
		}
		sb.WriteString(formatCsvField(jsjson.Str(k)))
	}
	sb.WriteByte('\n')
	for _, r := range rows {
		for i, k := range keys {
			if i > 0 {
				sb.WriteByte(',')
			}
			sb.WriteString(formatCsvField(r.Get(k)))
		}
		sb.WriteByte('\n')
	}
	if err := os.MkdirAll(filepath.Dir(filePath), 0o755); err != nil {
		return err
	}
	return os.WriteFile(filePath, []byte(sb.String()), 0o644)
}

// csv-writer.js:106-145
func extractDailySummary(daily value) []*jsjson.Obj {
	var rows []*jsjson.Obj
	entries(daily, func(date string, d value) {
		r := row("date", jsjson.Str(date))
		stats := d.Get("stats")
		copyKeys(r, stats, "", "totalKilocalories", "activeKilocalories", "bmrKilocalories",
			"totalSteps", "totalDistanceMeters", "wellnessKilocalories",
			"remainingKilocalories", "floorsAscended", "floorsDescended",
			"durationInMilliseconds")
		r.Set("stepGoal", orNull(stats.Get("dailyStepGoal")))
		hr := d.Get("heart_rates")
		copyKeys(r, hr, "", "maxHeartRate", "minHeartRate", "restingHeartRate", "lastSevenDaysAvgRestingHeartRate")
		stress := d.Get("stress")
		copyKeys(r, stress, "", "maxStressLevel", "avgStressLevel")
		im := d.Get("intensity_minutes")
		r.Set("moderateIntensityMinutes", coalesce(im.Get("moderateMinutes"), stats.Get("moderateIntensityMinutes")))
		r.Set("vigorousIntensityMinutes", coalesce(im.Get("vigorousMinutes"), stats.Get("vigorousIntensityMinutes")))
		// `d.steps || {}` then `typeof steps === 'object' && !Array.isArray(steps)`: these columns exist
		// only when steps is an object or falsy.
		if steps := d.Get("steps"); steps.Kind == jsjson.Object || !truthy(steps) {
			r.Set("stepsGoal", orNull(steps.Get("dailyStepGoal")))
			r.Set("stepsWellness", orNull(steps.Get("totalSteps")))
		}
		hyd := d.Get("hydration")
		r.Set("hydrationGoalMl", orNull(hyd.Get("goalInML")))
		r.Set("hydrationValueMl", orNull(hyd.Get("valueInML")))
		r.Set("hydrationSweatLossMl", orNull(hyd.Get("sweatLossInML")))
		r.Set("hydrationActivityIntakeMl", orNull(hyd.Get("activityIntakeInML")))
		rows = append(rows, r)
	})
	return rows
}

// hasKeys is Object.keys(x || {}).length > 0.
func hasKeys(v value) bool {
	switch v.Kind {
	case jsjson.Object:
		return len(v.O.Keys) > 0
	case jsjson.Array:
		return len(v.A) > 0
	case jsjson.String:
		return v.S != ""
	}
	return false
}

// csv-writer.js:147-180
func extractSleep(daily value) []*jsjson.Obj {
	var rows []*jsjson.Obj
	entries(daily, func(date string, d value) {
		sleep := d.Get("sleep")
		dto := sleep.Get("dailySleepDTO")
		if !hasKeys(dto) {
			return
		}
		r := row("date", jsjson.Str(date))
		for _, k := range []string{
			"sleepTimeSeconds", "napTimeSeconds", "unmeasurableSeconds",
			"deepSleepSeconds", "lightSleepSeconds", "remSleepSeconds",
			"awakeSleepSeconds", "averageSpO2Value", "lowestSpO2Value",
			"highestSpO2Value", "averageSpO2HRSleep", "averageRespirationValue",
			"lowestRespirationValue", "highestRespirationValue",
			"avgSleepStress", "sleepScores", "sleepResultType",
			"sleepStartTimestampGMT", "sleepEndTimestampGMT",
			"sleepStartTimestampLocal", "sleepEndTimestampLocal",
			"awakeCount", "avgHeartRate", "sleepScoreFeedback",
			"sleepScoreInsight", "sleepNeed", "nextSleepNeed",
			"breathingDisruptionSeverity", "sleepAlignment",
			"sleepWindowConfirmed",
		} {
			v := orNull(dto.Get(k))
			if v.Kind == jsjson.Object || v.Kind == jsjson.Array {
				v = jsjson.Str(jsjson.Stringify(v))
			}
			r.Set(k, v)
		}
		copyKeys(r, sleep, "", "restlessMomentsCount", "avgOvernightHrv", "avgSkinTempDeviationC", "restingHeartRate", "bodyBatteryChange")
		rows = append(rows, r)
	})
	return rows
}

// csv-writer.js:182-200
func extractHrv(daily value) []*jsjson.Obj {
	var rows []*jsjson.Obj
	entries(daily, func(date string, d value) {
		summary := d.Get("hrv").Get("hrvSummary")
		if !hasKeys(summary) {
			return
		}
		r := row("date", jsjson.Str(date))
		copyKeys(r, summary, "", "weeklyAvg", "lastNightAvg", "lastNight5MinHigh", "status", "feedbackPhrase")
		b := summary.Get("baseline")
		r.Set("baselineLowUpper", orNull(b.Get("lowUpper")))
		r.Set("baselineBalancedLow", orNull(b.Get("balancedLow")))
		r.Set("baselineBalancedUpper", orNull(b.Get("balancedUpper")))
		rows = append(rows, r)
	})
	return rows
}

// csv-writer.js:202-215
func extractBodyBattery(daily value) []*jsjson.Obj {
	var rows []*jsjson.Obj
	entries(daily, func(date string, d value) {
		// ponytail: `(d.body_battery || [])` with for...of throws in JavaScript on a non array object;
		// list() treats it as empty instead.
		for _, e := range list(d.Get("body_battery")) {
			r := row("date", jsjson.Str(date))
			copyKeys(r, e, "", "charged", "drained", "startTimestampLocal", "endTimestampLocal")
			rows = append(rows, r)
		}
	})
	return rows
}

// csv-writer.js:218-221
func primaryDevice(m value) value {
	var vals []value
	switch m.Kind {
	case jsjson.Object:
		for _, k := range m.O.Keys {
			vals = append(vals, m.O.Get(k))
		}
	case jsjson.Array:
		vals = m.A
	}
	for _, e := range vals {
		if truthy(e.Get("primaryTrainingDevice")) {
			return e
		}
	}
	if len(vals) > 0 {
		return vals[0] // a falsy entries[0] gives {}, and Get on it is Undefined as well
	}
	return value{}
}

// last is `Array.isArray(x) && x.length ? x[x.length - 1] : (object ? x : {})`.
func last(v value) value {
	if v.Kind == jsjson.Array {
		if len(v.A) > 0 {
			return v.A[len(v.A)-1]
		}
		return value{}
	}
	return v // Get on a non object is Undefined, as on {}
}

// csv-writer.js:223-266
func extractTraining(daily value) []*jsjson.Obj {
	var rows []*jsjson.Obj
	entries(daily, func(date string, d value) {
		r := row("date", jsjson.Str(date))
		tr := last(d.Get("training_readiness"))
		r.Set("readiness_level", orNull(tr.Get("level")))
		r.Set("readiness_feedbackShort", orNull(tr.Get("feedbackShort")))
		r.Set("readiness_feedbackLong", orNull(tr.Get("feedbackLong")))
		mr := last(d.Get("morning_training_readiness"))
		r.Set("morning_readiness_level", orNull(mr.Get("level")))
		r.Set("morning_readiness_feedback", orNull(mr.Get("feedbackShort")))
		r.Set("morning_readiness_recoveryTime", orNull(mr.Get("recoveryTime")))
		r.Set("morning_readiness_sleepScoreFactor", orNull(mr.Get("sleepScoreFactorPercent")))
		r.Set("morning_readiness_recoveryTimeFactor", orNull(mr.Get("recoveryTimeFactorPercent")))
		ts := d.Get("training_status")
		vo2 := ts.Get("mostRecentVO2Max").Get("generic")
		r.Set("vo2max", coalesce(vo2.Get("vo2MaxPreciseValue"), vo2.Get("vo2MaxValue")))
		status := primaryDevice(ts.Get("mostRecentTrainingStatus").Get("latestTrainingStatusData"))
		acute := status.Get("acuteTrainingLoadDTO")
		r.Set("trainingStatus", coalesce(status.Get("trainingStatusFeedbackPhrase"), status.Get("trainingStatus")))
		r.Set("trainingLoad_acute", orNull(acute.Get("dailyTrainingLoadAcute")))
		r.Set("trainingLoad_chronic", orNull(acute.Get("dailyTrainingLoadChronic")))
		r.Set("trainingLoad_ratio", orNull(acute.Get("dailyAcuteChronicWorkloadRatio")))
		r.Set("trainingLoad_ratioStatus", orNull(acute.Get("acwrStatus")))
		balance := primaryDevice(ts.Get("mostRecentTrainingLoadBalance").Get("metricsTrainingLoadBalanceDTOMap"))
		r.Set("monthlyLoad_aerobicLow", orNull(balance.Get("monthlyLoadAerobicLow")))
		r.Set("monthlyLoad_aerobicHigh", orNull(balance.Get("monthlyLoadAerobicHigh")))
		r.Set("monthlyLoad_anaerobic", orNull(balance.Get("monthlyLoadAnaerobic")))
		r.Set("loadBalance_feedback", orNull(balance.Get("trainingBalanceFeedbackPhrase")))
		end := d.Get("endurance_score")
		r.Set("enduranceScore", orNull(end.Get("overallScore")))
		r.Set("enduranceScore_classification", orNull(end.Get("classification")))
		rows = append(rows, r)
	})
	return rows
}

// csv-writer.js:268-282
func extractSpo2Respiration(daily value) []*jsjson.Obj {
	var rows []*jsjson.Obj
	entries(daily, func(date string, d value) {
		r := row("date", jsjson.Str(date))
		spo2 := d.Get("spo2")
		r.Set("spo2_avg", orNull(spo2.Get("averageSpO2")))
		r.Set("spo2_lowest", orNull(spo2.Get("lowestSpO2")))
		resp := d.Get("respiration")
		r.Set("respiration_avg", orNull(resp.Get("avgWakingRespirationValue")))
		r.Set("respiration_highest", orNull(resp.Get("highestRespirationValue")))
		r.Set("respiration_lowest", orNull(resp.Get("lowestRespirationValue")))
		rows = append(rows, r)
	})
	return rows
}

// activityType is `actType = act.activityType || {}; typeof actType === 'object' ? actType.typeKey : actType`.
func activityType(act value) value {
	at := act.Get("activityType")
	if !truthy(at) || at.Kind == jsjson.Object || at.Kind == jsjson.Array {
		return at.Get("typeKey")
	}
	return at
}

// csv-writer.js:284-314
func extractActivities(activities value) []*jsjson.Obj {
	var rows []*jsjson.Obj
	for _, act := range list(activities) {
		r := jsjson.NewObj()
		copyKeys(r, act, "",
			"activityId", "activityName", "startTimeLocal", "distance",
			"duration", "movingDuration", "elapsedDuration",
			"averageSpeed", "calories", "averageHR", "maxHR", "steps",
			"aerobicTrainingEffect", "anaerobicTrainingEffect",
			"activityTrainingLoad", "trainingEffectLabel",
			"moderateIntensityMinutes", "vigorousIntensityMinutes",
			"differenceBodyBattery", "lapCount",
			"hrTimeInZone_1", "hrTimeInZone_2", "hrTimeInZone_3",
			"hrTimeInZone_4", "hrTimeInZone_5",
			"minTemperature", "maxTemperature", "avgElevation",
			"elevationGain", "elevationLoss",
			"totalReps", "totalSets", "activeSets",
			"aerobicTrainingEffectMessage", "anaerobicTrainingEffectMessage",
			"vO2MaxValue", "avgStrideLength", "locationName",
			"startLatitude", "startLongitude", "endLatitude", "endLongitude",
			"averageRunningCadenceInStepsPerMinute",
			"maxRunningCadenceInStepsPerMinute")
		r.Set("activityType", activityType(act))
		rows = append(rows, r)
	}
	return rows
}

// csv-writer.js:316-327
func extractWeight(aggregated value) []*jsjson.Obj {
	var rows []*jsjson.Obj
	for _, e := range list(aggregated.Get("weigh_ins").Get("dateWeightList")) {
		rows = append(rows, flatten(e, ""))
	}
	for _, e := range list(aggregated.Get("body_composition").Get("dateWeightList")) {
		rows = append(rows, flatten(e, ""))
	}
	return rows
}

// csv-writer.js:329-348
func extractAggregatedMisc(aggregated value) []*jsjson.Obj {
	r := jsjson.NewObj()
	for _, key := range []string{"race_predictions", "lactate_threshold", "cycling_ftp", "fitness_age", "hill_score"} {
		val := aggregated.Get(key)
		if val.IsNullish() {
			continue
		}
		switch {
		case key == "lactate_threshold" && val.Kind == jsjson.Array:
			// `.map((e) => e.speed).find((v) => v != null) ?? null`
			var speed, hr []value
			for _, e := range val.A {
				speed = append(speed, e.Get("speed"))
				hr = append(hr, e.Get("hearRate"))
			}
			r.Set("lactateThresholdSpeed", coalesce(speed...))
			r.Set("lactateThresholdHeartRate", coalesce(hr...))
		case val.Kind == jsjson.Object:
			flat := flatten(val, "")
			for _, k := range flat.Keys {
				r.Set(key+"_"+k, flat.Get(k))
			}
		default:
			r.Set(key, val)
		}
	}
	if len(r.Keys) == 0 {
		return nil
	}
	return []*jsjson.Obj{r}
}

// csv-writer.js:350-362
func extractRunningTolerance(daily value) []*jsjson.Obj {
	var rows []*jsjson.Obj
	entries(daily, func(date string, d value) {
		for _, e := range list(d.Get("running_tolerance")) {
			r := row("date", jsjson.Str(date))
			copyKeys(r, e, "", "acuteImpactLoad", "acuteDistance", "acuteTolerance", "runningToleranceFeedBackPhrase")
			rows = append(rows, r)
		}
	})
	return rows
}

// csv-writer.js:364-372
func extractAggregatedDailySteps(aggregated value) []*jsjson.Obj {
	var rows []*jsjson.Obj
	for _, e := range list(aggregated.Get("daily_steps")) {
		r := jsjson.NewObj()
		copyKeys(r, e, "", "calendarDate", "totalSteps", "totalDistance", "stepGoal")
		rows = append(rows, r)
	}
	return rows
}

// csv-writer.js:374-382
func extractWeeklyIntensity(aggregated value) []*jsjson.Obj {
	var rows []*jsjson.Obj
	for _, e := range list(aggregated.Get("weekly_intensity")) {
		r := jsjson.NewObj()
		copyKeys(r, e, "", "calendarDate", "weeklyGoal", "moderateValue", "vigorousValue")
		rows = append(rows, r)
	}
	return rows
}

// csv-writer.js:384-396
func extractHillScoreDaily(aggregated value) []*jsjson.Obj {
	var rows []*jsjson.Obj
	for _, e := range list(aggregated.Get("hill_score").Get("hillScoreDTOList")) {
		r := jsjson.NewObj()
		copyKeys(r, e, "", "calendarDate", "overallScore", "strengthScore", "enduranceScore",
			"hillScoreClassificationId", "hillScoreFeedbackPhraseId")
		rows = append(rows, r)
	}
	return rows
}

// csv-writer.js:398-405
func extractBloodPressure(aggregated value) []*jsjson.Obj {
	var rows []*jsjson.Obj
	for _, e := range list(aggregated.Get("blood_pressure").Get("measurementSummaries")) {
		rows = append(rows, flatten(e, ""))
	}
	return rows
}

// csv-writer.js:411-421
var summaryFields = []string{
	"activityId", "activityName", "startTimeLocal", "distance", "duration",
	"movingDuration", "calories", "averageHR", "maxHR", "steps",
	"aerobicTrainingEffect", "anaerobicTrainingEffect", "activityTrainingLoad",
	"trainingEffectLabel", "differenceBodyBattery",
	"hrTimeInZone_1", "hrTimeInZone_2", "hrTimeInZone_3", "hrTimeInZone_4", "hrTimeInZone_5",
	"moderateIntensityMinutes", "vigorousIntensityMinutes",
	"avgElevation", "minTemperature", "maxTemperature",
	"totalReps", "totalSets", "activeSets",
	"lapCount", "averageSpeed",
}

// csv-writer.js:426-438
func extractGroupSummary(acts []value) []*jsjson.Obj {
	var rows []*jsjson.Obj
	for _, act := range acts {
		r := jsjson.NewObj()
		copyKeys(r, act, "", summaryFields...)
		r.Set("activityType", activityType(act))
		rows = append(rows, r)
	}
	return rows
}

// csv-writer.js:444-454
func extractGroupLaps(acts []value) []*jsjson.Obj {
	var rows []*jsjson.Obj
	for _, act := range acts {
		splits := act.Get("splits")
		if !truthy(splits) {
			continue
		}
		name := act.Get("activityName")
		if !truthy(name) {
			name = null
		}
		for _, lap := range flattenLaps(splits) {
			r := row("activityId", act.Get("activityId"), "activityName", name)
			for _, k := range lap.Keys {
				r.Set(k, lap.Get(k))
			}
			rows = append(rows, r)
		}
	}
	return rows
}

// csv-writer.js:497-513. flattenDetails (459-492) is not ported: nothing calls it.
func flattenLaps(splits value) []*jsjson.Obj {
	var rows []*jsjson.Obj
	for _, lap := range list(splits.Get("lapDTOs")) {
		r := jsjson.NewObj()
		copyKeys(r, lap, "lap_",
			"lapIndex", "startTimeGMT", "distance", "duration", "movingDuration",
			"averageSpeed", "averageHR", "maxHR", "calories",
			"averageRunCadence", "maxRunCadence",
			"averageTemperature", "maxTemperature", "minTemperature",
			"totalAscent", "totalDescent")
		rows = append(rows, r)
	}
	return rows
}

var dayTag = regexp.MustCompile(`^\d{4}-\d{2}-\d{2}$`)

// WriteCSVs writes the Garmin CSV files into outputDir/csv_<export_date> and returns that
// folder and the names of the files written. A table with no rows writes no file.
// csv-writer.js:525-575 (generateCsvs)
func WriteCSVs(export jsjson.Value, outputDir string) (csvDir string, written []string, err error) {
	// `jsonData.export_date || 'export'`, but only a YYYY-MM-DD day may become a folder name.
	dateTag := "export"
	if tag := export.Get("export_date"); truthy(tag) {
		if tag.Kind != jsjson.String || !dayTag.MatchString(tag.S) {
			return "", nil, errors.New("export_date must be a YYYY-MM-DD date")
		}
		dateTag = tag.S
	}
	csvDir = filepath.Join(outputDir, "csv_"+dateTag)
	if filepath.Dir(csvDir) != filepath.Clean(outputDir) {
		return "", nil, errors.New("the CSV folder must be inside the output folder")
	}
	if err := os.MkdirAll(csvDir, 0o755); err != nil {
		return "", nil, err
	}

	daily := export.Get("daily")
	aggregated := export.Get("aggregated")
	activities := export.Get("activities")

	grouped := map[string][]value{}
	for _, act := range list(activities) {
		// `(act.activityType && act.activityType.typeKey) || act.activityType`
		at := act.Get("activityType")
		typeKey := at
		if truthy(at) {
			typeKey = at.Get("typeKey")
		}
		if !truthy(typeKey) {
			typeKey = at
		}
		if g := groupFor(typeKey); g != "" {
			grouped[g] = append(grouped[g], act)
		}
	}

	csvFiles := []struct {
		name string
		rows []*jsjson.Obj
	}{
		{"daily_summary", extractDailySummary(daily)},
		{"sleep", extractSleep(daily)},
		{"hrv", extractHrv(daily)},
		{"body_battery", extractBodyBattery(daily)},
		{"training", extractTraining(daily)},
		{"spo2_respiration", extractSpo2Respiration(daily)},
		{"activities", extractActivities(activities)},
		{"weight_body_comp", extractWeight(aggregated)},
		{"blood_pressure", extractBloodPressure(aggregated)},
		{"misc_metrics", extractAggregatedMisc(aggregated)},
		{"running_tolerance", extractRunningTolerance(daily)},
		{"daily_steps_aggregated", extractAggregatedDailySteps(aggregated)},
		{"weekly_intensity", extractWeeklyIntensity(aggregated)},
		{"hill_score_daily", extractHillScoreDaily(aggregated)},
		{"caminar", extractGroupSummary(grouped["caminar"])},
		{"correr", extractGroupSummary(grouped["correr"])},
		{"gym", extractGroupSummary(grouped["gym"])},
		{"caminar_laps", extractGroupLaps(grouped["caminar"])},
		{"correr_laps", extractGroupLaps(grouped["correr"])},
		{"gym_laps", extractGroupLaps(grouped["gym"])},
	}
	for _, f := range csvFiles {
		if len(f.rows) == 0 {
			continue
		}
		if err := writeCsv(f.rows, filepath.Join(csvDir, f.name+".csv")); err != nil {
			return "", nil, err
		}
		written = append(written, f.name+".csv")
	}
	return csvDir, written, nil
}
