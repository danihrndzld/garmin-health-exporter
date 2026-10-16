package mfp

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestCacheFinalRule(t *testing.T) {
	t.Parallel()
	food := []DiaryItem{{Type: "food_entry"}}
	exercise := []DiaryItem{{Type: "exercise_entry"}}
	cases := []struct {
		name      string
		items     []DiaryItem
		fetchedAt time.Duration // after local midnight of the diary day
		final     bool
	}{
		{"BVA-115 food fetched 6 days 23 h 59 min after its date is not final", food, 7*24*time.Hour - time.Minute, false},
		{"BVA-116 food fetched exactly 7 days after its date is final", food, 7 * 24 * time.Hour, true},
		{"BVA-117 food fetched 7 days and 1 minute after its date is final", food, 7*24*time.Hour + time.Minute, true},
		{"EP-114 a day fetched on its own date is not final", food, 12 * time.Hour, false},
		{"EP-115 exercise without food is not final even 30 days later", exercise, 30 * 24 * time.Hour, false},
		{"EP-116 a day with no entries is not final even 30 days later", nil, 30 * 24 * time.Hour, false},
		{"EP-117 food mixed with exercise is final 8 days later", append(append([]DiaryItem{}, exercise...), food...), 8 * 24 * time.Hour, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			c := Cache{Dir: t.TempDir()}
			day, _ := time.ParseInLocation("2006-01-02", "2026-10-01", time.Local) // the diary day is the user's local day
			if err := c.Put("2026-10-01", tc.items, day.Add(tc.fetchedAt)); err != nil {
				t.Fatal(err)
			}
			_, final, ok := c.Get("2026-10-01", time.Now())
			if !ok || final != tc.final {
				t.Fatalf("ok = %v, final = %v, want final = %v", ok, final, tc.final)
			}
		})
	}
}

func TestCacheGetOfUnreadableDays(t *testing.T) {
	t.Parallel()
	write := func(t *testing.T, dir, body string) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(dir, "2026-10-01.json"), []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	cases := []struct {
		name      string
		body      string // "" means no file at all
		wantOK    bool
		wantFinal bool
	}{
		{"EG-110 a day that was never cached is reported as not cached", "", false, false},
		{"EG-111 a corrupt cache file is reported as not cached", "{broken", false, false},
		{"EG-112 an unparsable fetched_at keeps the items but is never final", `{"fetched_at":"yesterday","items":[{"type":"food_entry"}]}`, true, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			dir := t.TempDir()
			if tc.body != "" {
				write(t, dir, tc.body)
			}
			_, final, ok := Cache{Dir: dir}.Get("2026-10-01", time.Now())
			if ok != tc.wantOK || final != tc.wantFinal {
				t.Fatalf("ok = %v, final = %v", ok, final)
			}
		})
	}
}

func TestCachePutFile(t *testing.T) {
	t.Parallel()
	dir := filepath.Join(t.TempDir(), "mfp-cache")
	now := time.Date(2026, 10, 12, 8, 30, 0, 0, time.UTC)
	if err := (Cache{Dir: dir}).Put("2026-10-05", nil, now); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(filepath.Join(dir, "2026-10-05.json"))
	if err != nil {
		t.Fatal(err)
	}
	t.Run("CL-106 the cache file holds fetched_at and an items array", func(t *testing.T) {
		var raw map[string]json.RawMessage
		if err := json.Unmarshal(b, &raw); err != nil || len(raw) != 2 || string(raw["items"]) != "[]" || string(raw["fetched_at"]) != `"2026-10-12T08:30:00Z"` {
			t.Fatalf("cache file = %s (%v)", b, err)
		}
	})
	t.Run("CL-107 the cache file is private and leaves no temporary file behind", func(t *testing.T) {
		entries, _ := os.ReadDir(dir)
		st, _ := os.Stat(filepath.Join(dir, "2026-10-05.json"))
		if len(entries) != 1 || st.Mode().Perm() != 0o600 {
			t.Fatalf("entries = %d, mode = %v", len(entries), st.Mode().Perm())
		}
	})
}
