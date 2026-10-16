package guard

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
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
