package garmin

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestTrust(t *testing.T) {
	day := "2026-10-01"
	start, _ := time.ParseInLocation("2006-01-02", day, time.Local)
	at := func(d time.Duration) string { return start.Add(d).UTC().Format(time.RFC3339) }
	data := json.RawMessage(`{"a":1}`)
	t.Run("BVA-321 ports BVA-05 not final at 6 days 23 hours, final at exactly 7 days", func(t *testing.T) {
		if Trust(Entry{FetchedAt: at(7*24*time.Hour - time.Hour), Data: data}, day) {
			t.Fatal("6d23h must not be trusted")
		}
		if !Trust(Entry{FetchedAt: at(7 * 24 * time.Hour), Data: data}, day) {
			t.Fatal("7d must be trusted")
		}
	})
	t.Run("BVA-322 ports BVA-05 null or missing data is never trusted", func(t *testing.T) {
		old := at(30 * 24 * time.Hour)
		for _, d := range []json.RawMessage{json.RawMessage(`null`), nil} {
			if Trust(Entry{FetchedAt: old, Data: d}, day) {
				t.Fatalf("trusted %q", d)
			}
		}
	})
	t.Run("EG-328 Trust counts elapsed hours across DST, as JS does", func(t *testing.T) {
		ny, err := time.LoadLocation("America/New_York")
		if err != nil {
			t.Skip("no tz database")
		}
		old := time.Local
		time.Local = ny
		t.Cleanup(func() { time.Local = old })
		// Spring forward 2026-03-08: midnight to midnight 7 calendar days later is 6d23h
		// (node: TZ=America/New_York gives 6.958 days).
		f := time.Date(2026, 3, 15, 0, 0, 0, 0, ny).UTC().Format(time.RFC3339)
		if Trust(Entry{FetchedAt: f, Data: data}, "2026-03-08") {
			t.Fatal("6d23h elapsed must not be trusted")
		}
	})
	t.Run("EG-325 an unreadable timestamp or date is not trusted", func(t *testing.T) {
		if Trust(Entry{FetchedAt: "yesterday", Data: data}, day) || Trust(Entry{FetchedAt: at(30 * 24 * time.Hour), Data: data}, "bad") {
			t.Fatal("trusted bad input")
		}
	})
}

func TestInRefreshWindow(t *testing.T) {
	now := time.Date(2026, 10, 15, 14, 30, 0, 0, time.Local)
	t.Run("BVA-323 ports BVA-06 refreshDays 3 covers 10-15, 10-14, 10-13 and not 10-12", func(t *testing.T) {
		for date, want := range map[string]bool{"2026-10-15": true, "2026-10-14": true, "2026-10-13": true, "2026-10-12": false} {
			if got := InRefreshWindow(date, 3, now); got != want {
				t.Errorf("%s: got %v want %v", date, got, want)
			}
		}
	})
	t.Run("BVA-324 counts calendar days across a DST change", func(t *testing.T) {
		ny, err := time.LoadLocation("America/New_York")
		if err != nil {
			t.Skip("no tz database")
		}
		old := time.Local
		time.Local = ny
		t.Cleanup(func() { time.Local = old })
		// Spring forward 2026-03-08: 03-07 to 03-10 is 71 hours but 3 calendar days.
		if InRefreshWindow("2026-03-07", 3, time.Date(2026, 3, 10, 0, 0, 0, 0, ny)) {
			t.Error("3 calendar days before is outside a window of 3")
		}
		if !InRefreshWindow("2026-03-08", 3, time.Date(2026, 3, 10, 0, 0, 0, 0, ny)) {
			t.Error("2 calendar days before is inside a window of 3")
		}
		// Fall back 2026-11-01: 10-31 to 11-03 is 73 hours, still 3 days.
		if InRefreshWindow("2026-10-31", 3, time.Date(2026, 11, 3, 0, 0, 0, 0, ny)) {
			t.Error("fall back: 3 calendar days before is outside")
		}
	})
	t.Run("EG-326 a malformed date is outside the window", func(t *testing.T) {
		if InRefreshWindow("nope", 3, now) {
			t.Fatal("malformed date inside window")
		}
	})
}

func TestCache(t *testing.T) {
	newCache := func(t *testing.T) Cache { return Cache{Dir: filepath.Join(t.TempDir(), "garmin-cache")} }
	t.Run("EG-321 ports EG-12 a corrupt day file is a miss, not an error", func(t *testing.T) {
		c := newCache(t)
		os.MkdirAll(filepath.Join(c.Dir, "daily"), 0o700)
		os.WriteFile(filepath.Join(c.Dir, "daily", "2026-10-01.json"), []byte("{not json"), 0o600)
		if m := c.Day("2026-10-01"); m == nil || len(m) != 0 {
			t.Fatalf("got %#v", m)
		}
		if m := c.Day("2026-10-02"); m == nil || len(m) != 0 {
			t.Fatalf("missing file: got %#v", m)
		}
	})
	t.Run("EG-322 ports EG-12 PutDay leaves no temporary file, writes mode 0600 and keeps raw bytes", func(t *testing.T) {
		c := newCache(t)
		raw := json.RawMessage(`{"z":1,"a":72.0}`)
		if err := c.PutDay("2026-10-01", map[string]Entry{"hrv": {FetchedAt: "2026-10-09T00:00:00Z", Data: raw}}); err != nil {
			t.Fatal(err)
		}
		p := filepath.Join(c.Dir, "daily", "2026-10-01.json")
		if st, err := os.Stat(p); err != nil || st.Mode().Perm() != 0o600 {
			t.Fatalf("stat %v %v", st, err)
		}
		if m, _ := filepath.Glob(filepath.Join(c.Dir, "daily", ".tmp-*")); len(m) != 0 {
			t.Fatalf("temporary files left: %v", m)
		}
		got := c.Day("2026-10-01")["hrv"]
		if !bytes.Equal(got.Data, raw) || got.FetchedAt != "2026-10-09T00:00:00Z" {
			t.Fatalf("got %s %s", got.Data, got.FetchedAt)
		}
	})
	t.Run("EG-323 splits round trip byte for byte", func(t *testing.T) {
		c := newCache(t)
		raw := json.RawMessage(`{"lapDTOs":[{"x":10.0}]}`)
		if _, ok := c.Splits("123"); ok {
			t.Fatal("hit on empty cache")
		}
		if err := c.PutSplits("123", raw); err != nil {
			t.Fatal(err)
		}
		got, ok := c.Splits("123")
		if !ok || !bytes.Equal(got, raw) {
			t.Fatalf("got %s %v", got, ok)
		}
	})
	t.Run("EG-324 an activity id that is not digits never reaches a path", func(t *testing.T) {
		c := newCache(t)
		for _, id := range []string{"../x", "1/2", "", "12a", "-1"} {
			if c.PutSplits(id, json.RawMessage(`{}`)) == nil {
				t.Errorf("PutSplits accepted %q", id)
			}
			if _, ok := c.Splits(id); ok {
				t.Errorf("Splits hit for %q", id)
			}
		}
		if _, err := os.Stat(c.Dir); err == nil {
			t.Error("cache dir created by a rejected id")
		}
	})
	t.Run("EG-327 a date that is not YYYY-MM-DD never reaches a path", func(t *testing.T) {
		c := newCache(t)
		for _, d := range []string{"../2026-10-01", "2026-10-1", "", "2026-10-01/x"} {
			if c.PutDay(d, map[string]Entry{}) == nil {
				t.Errorf("PutDay accepted %q", d)
			}
			if len(c.Day(d)) != 0 {
				t.Errorf("Day hit for %q", d)
			}
		}
		if _, err := os.Stat(c.Dir); err == nil {
			t.Error("cache dir created by a rejected date")
		}
	})
	t.Run("EG-329 a null splits answer is neither stored nor served", func(t *testing.T) {
		c := newCache(t)
		for _, d := range []json.RawMessage{nil, json.RawMessage(`null`), json.RawMessage(" null\n")} {
			if c.PutSplits("7", d) == nil {
				t.Errorf("PutSplits accepted %q", d)
			}
		}
		if _, err := os.Stat(filepath.Join(c.Dir, "activity", "7.json")); err == nil {
			t.Error("a file was written")
		}
		os.MkdirAll(filepath.Join(c.Dir, "activity"), 0o700)
		os.WriteFile(filepath.Join(c.Dir, "activity", "8.json"), []byte(`{"splits":{"fetched_at":"x","data":null}}`), 0o600)
		if _, ok := c.Splits("8"); ok {
			t.Error("hand-written null splits served")
		}
	})
	t.Run("EG-330 PutDay stores only the non-null entries", func(t *testing.T) {
		c := newCache(t)
		err := c.PutDay("2026-10-01", map[string]Entry{
			"hrv":   {FetchedAt: "2026-10-09T00:00:00Z", Data: json.RawMessage(`{"a":1}`)},
			"sleep": {FetchedAt: "2026-10-09T00:00:00Z", Data: json.RawMessage(`null`)},
			"none":  {FetchedAt: "2026-10-09T00:00:00Z"},
		})
		if err != nil {
			t.Fatal(err)
		}
		b, _ := os.ReadFile(filepath.Join(c.Dir, "daily", "2026-10-01.json"))
		var m map[string]Entry
		json.Unmarshal(b, &m)
		if len(m) != 1 || m["hrv"].Data == nil {
			t.Fatalf("stored %s", b)
		}
		os.WriteFile(filepath.Join(c.Dir, "daily", "2026-10-02.json"), []byte(`{"x":{"fetched_at":"y","data":null}}`), 0o600)
		if len(c.Day("2026-10-02")) != 0 {
			t.Error("stored null served from Day")
		}
	})
}
