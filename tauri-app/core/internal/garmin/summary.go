package garmin

import (
	"bytes"
	"encoding/json"
	"maps"
	"slices"
)

// Summary is the per day series for the preview cards, ported from cli.js:summarize. The UI
// reads range and days only, so types and activities from the JavaScript are not ported.
type Summary struct {
	Range json.RawMessage `json:"range"` // the file's date_range as it is; null when absent
	Days  []Day           `json:"days"`
}

// Day holds each value as the JSON the export file has, or null when any step of the path is missing.
type Day struct {
	Date       string          `json:"date"`
	Steps      json.RawMessage `json:"steps"`
	RHR        json.RawMessage `json:"rhr"`
	SleepScore json.RawMessage `json:"sleepScore"`
	SleepSec   json.RawMessage `json:"sleepSec"`
	HRV        json.RawMessage `json:"hrv"`
}

var jsonNull = json.RawMessage("null")

// Summarize reads an export file. The JavaScript returned null for anything that was not JSON.
func Summarize(raw []byte) (*Summary, error) {
	var data struct {
		DateRange json.RawMessage            `json:"date_range"`
		Daily     map[string]json.RawMessage `json:"daily"`
	}
	if err := json.Unmarshal(raw, &data); err != nil {
		return nil, err
	}
	s := &Summary{Range: jsonNull, Days: make([]Day, 0, len(data.Daily))}
	if len(data.DateRange) > 0 {
		// The export file is indented; keep the range compact, as JSON.stringify sends it.
		var b bytes.Buffer
		_ = json.Compact(&b, data.DateRange) // valid: Unmarshal checked it
		s.Range = b.Bytes()
	}
	for _, d := range slices.Sorted(maps.Keys(data.Daily)) {
		x := data.Daily[d]
		s.Days = append(s.Days, Day{Date: d,
			Steps: dig(x, "stats", "totalSteps"), RHR: dig(x, "heart_rates", "restingHeartRate"),
			SleepScore: dig(x, "sleep", "dailySleepDTO", "sleepScores", "overall", "value"),
			SleepSec:   dig(x, "sleep", "dailySleepDTO", "sleepTimeSeconds"), HRV: dig(x, "hrv", "hrvSummary", "lastNightAvg")})
	}
	return s, nil
}

// Dates reads start and end from the range, for the pack; empty when the range has none.
func (s *Summary) Dates() (start, end string) {
	var r struct{ Start, End string }
	_ = json.Unmarshal(s.Range, &r)
	return r.Start, r.End
}

// dig is the optional chain x.a?.b?.c ?? null: null as soon as a step is not an object or the key is absent.
func dig(raw json.RawMessage, keys ...string) json.RawMessage {
	for _, k := range keys {
		var m map[string]json.RawMessage
		if json.Unmarshal(raw, &m) != nil || m[k] == nil {
			return jsonNull
		}
		raw = m[k]
	}
	return raw
}
