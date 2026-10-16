package garmin

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"time"

	"github.com/danihrndzld/garmin-health-exporter/tauri-app/core/internal/atomicfile"
)

// FinalAfterDays: a day fetched this many days or more after itself is final (see exporter.js).
const FinalAfterDays = 7

var (
	activityIDRe = regexp.MustCompile(`^\d+$`)
	dateRe       = regexp.MustCompile(`^\d{4}-\d{2}-\d{2}$`)
)

// Cache keeps one JSON file per day and one per activity under Dir (dataDir/garmin-cache).
type Cache struct{ Dir string }

// Entry is one endpoint's answer. Data stays raw so key order and number text survive.
type Entry struct {
	FetchedAt string          `json:"fetched_at"`
	Data      json.RawMessage `json:"data"`
}

func (c Cache) dayPath(date string) (string, error) {
	if !dateRe.MatchString(date) {
		return "", fmt.Errorf("invalid cache date %q", date)
	}
	return filepath.Join(c.Dir, "daily", date+".json"), nil
}

func (c Cache) activityPath(id string) (string, error) {
	if !activityIDRe.MatchString(id) {
		return "", fmt.Errorf("invalid activity id %q", id)
	}
	return filepath.Join(c.Dir, "activity", id+".json"), nil
}

// Day returns the cached entries of a day: empty on a miss, a corrupt file or an invalid date.
func (c Cache) Day(date string) map[string]Entry {
	out := map[string]Entry{}
	p, err := c.dayPath(date)
	if err != nil {
		return out
	}
	b, err := os.ReadFile(p)
	if err != nil || json.Unmarshal(b, &out) != nil || out == nil {
		return map[string]Entry{}
	}
	return out
}

func (c Cache) PutDay(date string, entries map[string]Entry) error {
	p, err := c.dayPath(date)
	if err != nil {
		return err
	}
	return c.write(p, entries)
}

func (c Cache) Splits(activityID string) (json.RawMessage, bool) {
	p, err := c.activityPath(activityID)
	if err != nil {
		return nil, false
	}
	b, err := os.ReadFile(p)
	if err != nil {
		return nil, false
	}
	var m map[string]Entry
	if json.Unmarshal(b, &m) != nil {
		return nil, false
	}
	e, ok := m["splits"]
	if !ok || len(e.Data) == 0 {
		return nil, false
	}
	return e.Data, true
}

func (c Cache) PutSplits(activityID string, data json.RawMessage) error {
	p, err := c.activityPath(activityID)
	if err != nil {
		return err
	}
	return c.write(p, map[string]Entry{"splits": {FetchedAt: time.Now().UTC().Format(time.RFC3339), Data: data}})
}

func (c Cache) write(path string, v any) error {
	b, err := json.Marshal(v)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	return atomicfile.WriteFile(path, b)
}

// Trust reports whether a cached day answer may be used: fetched FinalAfterDays or more
// after the day (local midnight), and not null.
func Trust(e Entry, date string) bool {
	if len(e.Data) == 0 || string(e.Data) == "null" {
		return false
	}
	day, err := time.ParseInLocation("2006-01-02", date, time.Local)
	if err != nil {
		return false
	}
	fetched, err := time.Parse(time.RFC3339, e.FetchedAt)
	if err != nil {
		return false
	}
	return fetched.Sub(day) >= FinalAfterDays*24*time.Hour
}

// InRefreshWindow: the day is fewer than refreshDays calendar days before today.
// Calendar days, not elapsed hours, so a 23 or 25 hour DST day counts as one.
func InRefreshWindow(date string, refreshDays int, now time.Time) bool {
	d, err := time.Parse("2006-01-02", date)
	if err != nil {
		return false
	}
	y, m, dd := now.Date()
	today := time.Date(y, m, dd, 0, 0, 0, 0, time.UTC)
	return int(today.Sub(d)/(24*time.Hour)) < refreshDays
}
