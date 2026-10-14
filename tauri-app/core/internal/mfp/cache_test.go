package mfp

import (
	"testing"
	"time"
)

func TestCacheFinalRule(t *testing.T) {
	c := Cache{Dir: t.TempDir()}
	food := []DiaryItem{{Type: "food_entry"}}
	exerciseOnly := []DiaryItem{{Type: "exercise_entry"}}
	at := func(s string) time.Time { v, _ := time.Parse(time.RFC3339, s); return v }

	mid, _ := time.ParseInLocation("2006-01-02", "2026-10-01", time.Local) // the diary day is local
	_ = c.Put("2026-10-01", food, mid.Add(7*24*time.Hour-time.Minute))     // 6.99 days after
	if _, final, ok := c.Get("2026-10-01", at("2026-10-20T00:00:00Z")); !ok || final {
		t.Fatal("food fetched 6.99 days after must not be final")
	}
	_ = c.Put("2026-10-01", food, mid.Add(7*24*time.Hour)) // 7 days after
	if _, final, _ := c.Get("2026-10-01", at("2026-10-20T00:00:00Z")); !final {
		t.Fatal("food fetched 7 days after must be final")
	}
	_ = c.Put("2026-09-01", exerciseOnly, at("2026-10-01T00:00:00Z")) // 30 days after, no food
	if _, final, _ := c.Get("2026-09-01", at("2026-10-20T00:00:00Z")); final {
		t.Fatal("a day without food is never final: the user may fill it in later")
	}
	today := time.Now().UTC().Format("2006-01-02")
	_ = c.Put(today, food, time.Now())
	if _, final, _ := c.Get(today, time.Now()); final {
		t.Fatal("today is never final")
	}
	if _, _, ok := c.Get("2020-01-01", time.Now()); ok {
		t.Fatal("missing day reported as cached")
	}
}
