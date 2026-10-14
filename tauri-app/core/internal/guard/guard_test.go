package guard

import (
	"os"
	"path/filepath"
	"testing"
)

func TestInsideHome(t *testing.T) {
	home, _ := os.UserHomeDir()
	in, err := os.MkdirTemp(home, ".guard-test-")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(in)
	if _, err := InsideHome(in); err != nil {
		t.Fatalf("dir in home rejected: %v", err)
	}
	for _, bad := range []string{"/etc", "/tmp", home + "-evil", filepath.Join(in, "..", "..", "..")} {
		if _, err := InsideHome(bad); err == nil {
			t.Errorf("%s accepted", bad)
		}
	}
	link := filepath.Join(in, "link")
	_ = os.Symlink("/etc", link)
	if _, err := InsideHome(link); err == nil {
		t.Error("symlink out of home accepted")
	}
	// A path that does not exist yet is checked through its nearest existing parent.
	if _, err := InsideHome(filepath.Join(in, "new", "dir")); err != nil {
		t.Errorf("new dir in home rejected: %v", err)
	}
}

func TestValidateRange(t *testing.T) {
	ok := [][2]string{{"2026-10-01", "2026-10-01"}, {"2026-07-17", "2026-10-14"}}
	for _, r := range ok {
		if err := ValidateRange(r[0], r[1], 90); err != nil {
			t.Errorf("%v: %v", r, err)
		}
	}
	bad := [][2]string{{"2026-07-16", "2026-10-14"}, {"2026-10-02", "2026-10-01"}, {"2026-13-01", "2026-13-02"}, {"", "2026-10-01"}}
	for _, r := range bad {
		if err := ValidateRange(r[0], r[1], 90); err == nil {
			t.Errorf("%v accepted", r)
		}
	}
	if d := Dates("2026-09-29", "2026-10-02"); len(d) != 4 || d[0] != "2026-09-29" || d[3] != "2026-10-02" {
		t.Errorf("Dates = %v", d)
	}
}
