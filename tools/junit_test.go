package main

// The converters must write what tools/go_junit.py wrote: testdata/go-junit.xml and
// testdata/go-coverage.json are that script's output for testdata/go-test.json and
// testdata/cover.out (a subtest parent, a failure with characters to escape, a skip, TestMain,
// a line that is not JSON, and a cover block listed twice).

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

func golden(t *testing.T, got, want string) {
	t.Helper()
	g, err1 := os.ReadFile(got)
	w, err2 := os.ReadFile(want)
	if err1 != nil || err2 != nil {
		t.Fatal(err1, err2)
	}
	if !bytes.Equal(g, w) {
		t.Errorf("%s differs from %s:\n%s\nwant:\n%s", got, want, g, w)
	}
}

func TestGoJUnitMatchesThePythonOutput(t *testing.T) {
	out, log := filepath.Join(t.TempDir(), "junit.xml"), &bytes.Buffer{}
	if err := goJUnit("testdata/go-test.json", out, log); err != nil {
		t.Fatal(err)
	}
	golden(t, out, "testdata/go-junit.xml")
	if log.String() != "3 leaf tests\n" {
		t.Errorf("log = %q", log)
	}
}

func TestGoCoverMatchesThePythonOutput(t *testing.T) {
	out, log := filepath.Join(t.TempDir(), "coverage.json"), &bytes.Buffer{}
	if err := goCover("testdata/cover.out", out, "m/", log); err != nil {
		t.Fatal(err)
	}
	golden(t, out, "testdata/go-coverage.json")
	want := "a/x.go                              3/4     75.00%\nb/y.go                              0/2      0.00%\ntotal                               3/6     50.00%\n"
	if log.String() != want {
		t.Errorf("log = %q", log)
	}
}

func TestPyPct(t *testing.T) {
	for _, c := range []struct {
		cov, tot int
		want     string
	}{{3, 87, "3.45"}, {59, 59, "100.0"}, {0, 11, "0.0"}, {0, 0, "100"}, {1, 3, "33.33"}, {2, 3, "66.67"}} {
		if got := pyPct(c.cov, c.tot); got != c.want {
			t.Errorf("pyPct(%d, %d) = %s, want %s", c.cov, c.tot, got, c.want)
		}
	}
}

func TestFindRootWalksUpToTauriApp(t *testing.T) {
	dir := t.TempDir()
	deep := filepath.Join(dir, "tools", "a", "b")
	if err := os.MkdirAll(filepath.Join(dir, "tauri-app"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(deep, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, start := range []string{dir, deep} {
		got, err := findRoot(start)
		if err != nil {
			t.Fatal(err)
		}
		if want, _ := filepath.EvalSymlinks(dir); mustEval(got) != want {
			t.Errorf("findRoot(%s) = %s, want %s", start, got, dir)
		}
	}
	if _, err := findRoot(t.TempDir()); err == nil {
		t.Error("findRoot found a root in a directory without tauri-app/")
	}
}

func mustEval(p string) string {
	r, _ := filepath.EvalSymlinks(p)
	return r
}

func TestShjoinQuotesLikeShlex(t *testing.T) {
	got := shjoin([]string{"cp", "a/Garmin Export.app", "", "it's", "--x=1,2"})
	want := `cp 'a/Garmin Export.app' '' 'it'"'"'s' --x=1,2`
	if got != want {
		t.Errorf("shjoin = %s, want %s", got, want)
	}
}
