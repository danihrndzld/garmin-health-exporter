package guard

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

// homeTemp makes a scratch dir inside $HOME: the guard rejects everything outside it, t.TempDir() included.
func homeTemp(t *testing.T) string {
	t.Helper()
	home, err := os.UserHomeDir()
	if err != nil {
		t.Fatal(err)
	}
	dir, err := os.MkdirTemp(home, ".guard-test-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(dir) })
	return dir
}

func TestInsideHome(t *testing.T) {
	t.Parallel()
	home, _ := os.UserHomeDir()
	in := homeTemp(t)
	mustSymlink := func(target, name string) string {
		link := filepath.Join(in, name)
		if err := os.Symlink(target, link); err != nil {
			t.Fatal(err)
		}
		return link
	}
	inner := filepath.Join(in, "real")
	if err := os.Mkdir(inner, 0o755); err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		name string
		path string
		ok   bool
	}{
		{name: "EP-101 directory inside HOME is accepted", path: in, ok: true},
		{name: "EP-102 existing directory outside HOME is rejected", path: "/etc", ok: false},
		{name: "EP-103 sibling that shares the HOME prefix is rejected", path: home + "-evil", ok: false},
		{name: "EP-104 dot-dot escape out of HOME is rejected", path: filepath.Join(in, "..", "..", ".."), ok: false},
		{name: "EP-105 symlink inside HOME that points outside is rejected", path: mustSymlink("/etc", "out"), ok: false},
		{name: "EP-106 symlink inside HOME that points inside HOME is accepted", path: mustSymlink(inner, "within"), ok: true},
		{name: "EP-107 path that does not exist yet is checked through its nearest parent", path: filepath.Join(in, "new", "dir"), ok: true},
		{name: "EP-108 empty path is rejected", path: "", ok: false},
		{name: "BVA-101 HOME itself is accepted", path: home, ok: true},
		{name: "BVA-102 parent of HOME is rejected", path: filepath.Dir(home), ok: false},
		{name: "EG-101 not yet existing path below a symlink that leaves HOME is rejected", path: filepath.Join(mustSymlink("/etc", "out2"), "new"), ok: false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			_, err := InsideHome(tc.path)
			if (err == nil) != tc.ok {
				t.Fatalf("InsideHome(%q) err = %v, want ok = %v", tc.path, err, tc.ok)
			}
		})
	}
}

func TestValidateRange(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name       string
		start, end string
		ok         bool
	}{
		{name: "BVA-103 a one day range is accepted", start: "2026-10-01", end: "2026-10-01", ok: true},
		{name: "BVA-104 a range of 90 days is accepted", start: "2026-07-17", end: "2026-10-14", ok: true},
		{name: "BVA-105 a range of 91 days is rejected", start: "2026-07-16", end: "2026-10-14", ok: false},
		{name: "BVA-106 an end one day before the start is rejected", start: "2026-10-02", end: "2026-10-01", ok: false},
		{name: "BVA-107 90 days across a leap day are accepted", start: "2028-01-15", end: "2028-04-13", ok: true},
		{name: "BVA-108 91 days across a leap day are rejected", start: "2028-01-15", end: "2028-04-14", ok: false},
		{name: "BVA-109 a range across New Year is accepted", start: "2026-12-31", end: "2027-01-01", ok: true},
		{name: "BVA-110 29 February of a non leap year is rejected", start: "2026-02-29", end: "2026-02-29", ok: false},
		{name: "BVA-111 29 February of a leap year is accepted", start: "2028-02-29", end: "2028-02-29", ok: true},
		{name: "EP-109 month 13 is rejected", start: "2026-13-01", end: "2026-13-02", ok: false},
		{name: "EP-110 an empty start is rejected", start: "", end: "2026-10-01", ok: false},
		{name: "EP-111 an empty end is rejected", start: "2026-10-01", end: "", ok: false},
		{name: "EP-112 free text instead of a date is rejected", start: "yesterday", end: "today", ok: false},
		{name: "EP-113 a date without zero padding is rejected", start: "2026-10-1", end: "2026-10-2", ok: false},
		{name: "EG-102 a date with a time part is rejected", start: "2026-10-01T00:00:00Z", end: "2026-10-02T00:00:00Z", ok: false},
		{name: "EG-103 a date with trailing whitespace is rejected", start: "2026-10-01 ", end: "2026-10-02", ok: false},
		{name: "EG-104 slashes instead of dashes are rejected", start: "2026/10/01", end: "2026/10/02", ok: false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			err := ValidateRange(tc.start, tc.end, 90)
			if (err == nil) != tc.ok {
				t.Fatalf("ValidateRange(%q, %q) err = %v, want ok = %v", tc.start, tc.end, err, tc.ok)
			}
		})
	}
}

func TestValidateRangeNamesItsLimit(t *testing.T) {
	t.Parallel()
	t.Run("BVA-177 a range one day over a 7 day limit names 7 days in the error", func(t *testing.T) {
		t.Parallel()
		if err := ValidateRange("2026-10-01", "2026-10-08", 7); err == nil || err.Error() != "the range can be at most 7 days" {
			t.Fatalf("err = %v", err)
		}
	})
}

func TestDates(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name       string
		start, end string
		want       []string
	}{
		{name: "BVA-112 a one day range lists exactly that day", start: "2026-10-01", end: "2026-10-01", want: []string{"2026-10-01"}},
		{name: "BVA-113 both ends are included, oldest first", start: "2026-09-29", end: "2026-10-02", want: []string{"2026-09-29", "2026-09-30", "2026-10-01", "2026-10-02"}},
		{name: "BVA-114 a leap day is listed", start: "2028-02-28", end: "2028-03-01", want: []string{"2028-02-28", "2028-02-29", "2028-03-01"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := Dates(tc.start, tc.end); !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("Dates = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestDaysBackEndsTodayInLocalTime(t *testing.T) {
	now := time.Date(2026, 10, 15, 23, 30, 0, 0, time.Local)
	for _, c := range []struct {
		id    string
		n     int
		start string
		ok    bool
	}{
		{"BVA-201 0 days is rejected", 0, "", false},
		{"BVA-202 1 day starts and ends today", 1, "2026-10-15", true},
		{"BVA-203 30 days starts 29 days back", 30, "2026-09-16", true},
		{"BVA-204 90 days is the largest accepted", 90, "2026-07-18", true},
		{"BVA-205 91 days is rejected", 91, "", false},
		{"BVA-206 a negative count is rejected", -1, "", false},
	} {
		t.Run(c.id, func(t *testing.T) {
			s, e, err := DaysBack(c.n, now)
			if !c.ok {
				if err == nil {
					t.Fatalf("DaysBack(%d) accepted", c.n)
				}
				return
			}
			if err != nil || s != c.start || e != "2026-10-15" {
				t.Fatalf("DaysBack(%d) = %s %s %v", c.n, s, e, err)
			}
		})
	}
}

func TestRefreshWindow(t *testing.T) {
	for _, c := range []struct {
		id string
		n  int
		ok bool
	}{
		{"BVA-207 0 is rejected", 0, false},
		{"BVA-208 1 is the smallest accepted", 1, true},
		{"BVA-209 7 is the largest accepted", 7, true},
		{"BVA-210 8 is rejected", 8, false},
	} {
		t.Run(c.id, func(t *testing.T) {
			if err := RefreshWindow(c.n); (err == nil) != c.ok {
				t.Fatalf("RefreshWindow(%d) = %v", c.n, err)
			}
		})
	}
}

func TestCredentials(t *testing.T) {
	secret := strings.Repeat("s3cret", 200)
	for _, c := range []struct {
		id, email, pw string
		ok            bool
	}{
		{"EP-201 a normal pair is accepted", "a@b.c", "pw", true},
		{"EP-202 an empty email is rejected", "", "pw", false},
		{"EP-203 an empty password is rejected", "a@b.c", "", false},
		{"BVA-211 an email of 320 bytes is accepted", strings.Repeat("a", 320), "pw", true},
		{"BVA-212 an email of 321 bytes is rejected", strings.Repeat("a", 321), "pw", false},
		{"BVA-213 a password of 1024 bytes is accepted", "a@b.c", strings.Repeat("p", 1024), true},
		{"BVA-214 a password of 1025 bytes is rejected", "a@b.c", strings.Repeat("p", 1025), false},
		{"EP-204 the error never contains the password", "a@b.c", secret, false},
		{"EP-205 the error never contains the email", strings.Repeat("e@x", 200), "pw", false},
	} {
		t.Run(c.id, func(t *testing.T) {
			err := Credentials(c.email, c.pw)
			if (err == nil) != c.ok {
				t.Fatalf("Credentials = %v", err)
			}
			if err != nil && (strings.Contains(err.Error(), "s3cret") || strings.Contains(err.Error(), "e@x") || strings.Contains(err.Error(), "ppp")) {
				t.Fatalf("error leaks input: %v", err)
			}
		})
	}
}

func TestNotFutureEndsToday(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 10, 15, 23, 30, 0, 0, time.Local)
	for _, c := range []struct {
		id, end string
		ok      bool
	}{
		{"BVA-215 an end date of today is accepted", "2026-10-15", true},
		{"BVA-216 an end date of tomorrow is rejected", "2026-10-16", false},
	} {
		t.Run(c.id, func(t *testing.T) {
			t.Parallel()
			if err := NotFuture(c.end, now); (err == nil) != c.ok {
				t.Fatalf("NotFuture(%q) = %v", c.end, err)
			}
		})
	}
}

func TestAPIKey(t *testing.T) {
	for _, c := range []struct {
		id, key string
		ok      bool
	}{
		{"EP-706 a key of letters, digits and dashes is accepted", "Ab3-x_y.z~9", true},
		{"EP-707 an empty key is rejected", "", false},
		{"BVA-706 a key of 512 bytes is accepted", strings.Repeat("k", 512), true},
		{"BVA-707 a key of 513 bytes is rejected", strings.Repeat("k", 513), false},
		{"EP-708 a key with a space is rejected", "abc def", false},
		{"EP-709 a key with a line break is rejected, so no header can be injected", "abc\r\nX-Evil: 1", false},
		{"EP-710 a key with a character beyond ASCII is rejected", "abcé", false},
		{"EP-711 the error never contains the key", strings.Repeat("s3cret ", 3), false},
	} {
		t.Run(c.id, func(t *testing.T) {
			err := APIKey(c.key)

			if (err == nil) != c.ok || (err != nil && strings.Contains(err.Error(), "s3cret")) {
				t.Fatalf("APIKey = %v", err)
			}
		})
	}
}
