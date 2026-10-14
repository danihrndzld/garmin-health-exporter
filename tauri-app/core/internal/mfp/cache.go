package mfp

import (
	"encoding/json"
	"os"
	"path/filepath"
	"time"
)

// FinalAfterDays: a day with food is final once it was fetched this many days after
// its date. Days without food are always fetched again, because the user may log late.
const FinalAfterDays = 7

type Cache struct{ Dir string }

type cachedDay struct {
	FetchedAt string      `json:"fetched_at"`
	Items     []DiaryItem `json:"items"`
}

func HasFood(items []DiaryItem) bool {
	for _, it := range items {
		if it.Type == "food_entry" {
			return true
		}
	}
	return false
}

func (c Cache) path(date string) string { return filepath.Join(c.Dir, date+".json") }

func (c Cache) Get(date string, now time.Time) (items []DiaryItem, final bool, ok bool) {
	b, err := os.ReadFile(c.path(date))
	if err != nil {
		return nil, false, false
	}
	var d cachedDay
	if json.Unmarshal(b, &d) != nil {
		return nil, false, false
	}
	day, err1 := time.Parse("2006-01-02", date)
	fetched, err2 := time.Parse(time.RFC3339, d.FetchedAt)
	final = err1 == nil && err2 == nil && HasFood(d.Items) &&
		fetched.Sub(day) >= FinalAfterDays*24*time.Hour
	return d.Items, final, true
}

func (c Cache) Put(date string, items []DiaryItem, now time.Time) error {
	if err := os.MkdirAll(c.Dir, 0o700); err != nil {
		return err
	}
	if items == nil {
		items = []DiaryItem{}
	}
	b, err := json.Marshal(cachedDay{FetchedAt: now.UTC().Format(time.RFC3339), Items: items})
	if err != nil {
		return err
	}
	return writeFileAtomic(c.path(date), b)
}
