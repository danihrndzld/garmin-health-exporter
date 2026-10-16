package pack

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// The frozen inputs and outputs of the JavaScript llm-pack, ported from the JavaScript sidecar (removed in stage 3; see tag archive/electron-app and git history).
const fixtureDir = "testdata/csv_2024-02-21"

func opts(csvDir string) Options {
	return Options{CSVDir: csvDir, StartDate: "2024-02-17", EndDate: "2024-02-21",
		Warnings: []string{"hill_score skipped (404)", "[skip] endurance_score: HTTP 204"},
		Now:      time.Date(2026, 10, 15, 20, 53, 0, 0, time.Local), TZ: "America/Guatemala"}
}

func golden(t *testing.T, name string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("testdata/golden", name))
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func copyFixture(t *testing.T) string {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "csv_2024-02-21")
	if err := os.CopyFS(dir, os.DirFS(fixtureDir)); err != nil {
		t.Fatal(err)
	}
	return dir
}

func TestPreviewEqualsTheJavaScriptGoldens(t *testing.T) {
	t.Run("AC-602 the context of both languages and the preview numbers equal the frozen files", func(t *testing.T) {
		p, err := BuildPreview(opts(fixtureDir))
		if err != nil || p == nil {
			t.Fatalf("preview: %v %v", p, err)
		}
		for _, lang := range Langs {
			if got, want := p.Langs[lang].Context, golden(t, "CONTEXT."+lang+".md"); got != want {
				t.Errorf("CONTEXT.%s.md differs at byte %d:\n got %q\nwant %q", lang, firstDiff(got, want), got, want)
			}
		}
		var want struct {
			StartDate, EndDate string
			Days               int
			Files              []File
			Warnings           []string
			Langs              map[string]struct {
				Bytes, Tokens int
				SingleName    string
			}
		}
		if err := json.Unmarshal([]byte(golden(t, "preview.json")), &want); err != nil {
			t.Fatal(err)
		}
		if p.Days != want.Days || len(p.Files) != 21 || len(p.Files) != len(want.Files) {
			t.Fatalf("days %d files %d, want %d and %d", p.Days, len(p.Files), want.Days, len(want.Files))
		}
		for i, f := range want.Files {
			if p.Files[i].Name != f.Name || p.Files[i].Rows != f.Rows {
				t.Errorf("file %d: got %+v want %+v", i, p.Files[i], f)
			}
		}
		for lang, w := range want.Langs {
			g := p.Langs[lang]
			if g.Bytes != w.Bytes || g.Tokens != w.Tokens || g.SingleName != w.SingleName {
				t.Errorf("%s: got %d %d %s, want %d %d %s", lang, g.Bytes, g.Tokens, g.SingleName, w.Bytes, w.Tokens, w.SingleName)
			}
		}
	})
}

func TestBuildWritesTheGoldenPacks(t *testing.T) {
	t.Run("AC-603 single writes the frozen one-file pack and files writes the context plus byte equal CSV copies, removing the v4.0.0 root files", func(t *testing.T) {
		for _, lang := range Langs {
			dir := copyFixture(t)
			os.WriteFile(filepath.Join(dir, "CONTEXT.es.md"), []byte("legacy"), 0o644) // v4.0.0 root files
			os.WriteFile(filepath.Join(dir, "garmin_2026-01-01_2026-01-02.en.md"), []byte("legacy"), 0o644)
			r, err := Build(opts(dir), lang, "single")
			if err != nil || r.Count != 1 || r.Dir != filepath.Join(dir, "llm-"+lang+"-one-file") {
				t.Fatalf("single: %+v %v", r, err)
			}
			got, _ := os.ReadFile(r.File)
			if want := golden(t, filepath.Base(r.File)); string(got) != want {
				t.Errorf("%s differs at byte %d", filepath.Base(r.File), firstDiff(string(got), want))
			}
			r, err = Build(opts(dir), lang, "files")
			if err != nil || r.Count != 22 || r.File != "" || r.Dir != filepath.Join(dir, "llm-"+lang) {
				t.Fatalf("files: %+v %v", r, err)
			}
			ctx, _ := os.ReadFile(filepath.Join(r.Dir, "CONTEXT."+lang+".md"))
			if string(ctx) != golden(t, "CONTEXT."+lang+".md") {
				t.Error("CONTEXT in the files folder differs from the preview context")
			}
			if b, _ := os.ReadFile(filepath.Join(r.Dir, "sleep.csv")); !bytes.Equal(b, mustRead(t, filepath.Join(dir, "sleep.csv"))) {
				t.Error("sleep.csv was not copied byte for byte")
			}
			entries, _ := os.ReadDir(dir)
			var names []string
			for _, e := range entries {
				if strings.HasSuffix(e.Name(), ".md") {
					names = append(names, e.Name())
				}
			}
			if len(names) != 0 {
				t.Errorf("legacy root files kept: %v", names)
			}
		}
	})
}

func TestASecondPressReplacesTheFolder(t *testing.T) {
	t.Run("ST-T601 files, a stray file in llm-en, files again: the stray file is gone", func(t *testing.T) {
		dir := copyFixture(t)
		r, err := Build(opts(dir), "en", "files")
		if err != nil {
			t.Fatal(err)
		}
		os.WriteFile(filepath.Join(r.Dir, "stale.txt"), []byte("x"), 0o644)
		if _, err := Build(opts(dir), "en", "files"); err != nil {
			t.Fatal(err)
		}
		if _, err := os.Stat(filepath.Join(r.Dir, "stale.txt")); !os.IsNotExist(err) {
			t.Error("stale file survived the second press")
		}
	})
}

func TestLangAndModePartitions(t *testing.T) {
	t.Run("EP-601 fr and zip are refused with the cli.js sentences before anything is deleted", func(t *testing.T) {
		dir := copyFixture(t)
		os.WriteFile(filepath.Join(dir, "CONTEXT.en.md"), []byte("legacy"), 0o644)
		if _, err := Build(opts(dir), "fr", "files"); err == nil || err.Error() != "lang must be es or en." {
			t.Errorf("fr: %v", err)
		}
		if _, err := Build(opts(dir), "es", "zip"); err == nil || err.Error() != "mode must be files or single." {
			t.Errorf("zip: %v", err)
		}
		if _, err := os.Stat(filepath.Join(dir, "CONTEXT.en.md")); err != nil {
			t.Error("a refused request deleted a file")
		}
	})
}

func TestMissingFolderIsNilAndEmptyFolderHasAnEmptyList(t *testing.T) {
	t.Run("EG-601 a missing folder or a missing date gives nil; an empty folder gives files [] and 0 files in the context", func(t *testing.T) {
		o := opts(filepath.Join(t.TempDir(), "none"))
		if p, err := BuildPreview(o); p != nil || err != nil {
			t.Fatalf("missing folder: %v %v", p, err)
		}
		for _, o := range []Options{{CSVDir: fixtureDir, EndDate: "2024-02-21"}, {CSVDir: fixtureDir, StartDate: "2024-02-17"}, {StartDate: "2024-02-17", EndDate: "2024-02-21"}} {
			if p, _ := BuildPreview(o); p != nil {
				t.Errorf("preview without a folder and both dates must be nil")
			}
		}
		empty := filepath.Join(t.TempDir(), "csv_2024-02-21")
		os.Mkdir(empty, 0o755)
		p, err := BuildPreview(opts(empty))
		if err != nil || p == nil {
			t.Fatal(err)
		}
		b, _ := json.Marshal(p)
		if !strings.Contains(string(b), `"files":[]`) || !strings.Contains(string(b), `"warnings":[`) {
			t.Errorf("empty folder must serialize files as []: %s", b)
		}
		if !strings.Contains(p.Langs["en"].Context, "## Files (0)") {
			t.Error("an empty folder lists 0 files")
		}
		if p, _ := BuildPreview(Options{CSVDir: empty, StartDate: "2024-02-17", EndDate: "2024-02-21"}); p == nil || p.Warnings == nil {
			t.Error("nil Warnings in must still be [] out")
		}
	})
}

func TestLineEndingsQuotedHeaderAndEmptyFile(t *testing.T) {
	t.Run("EG-602 CRLF lines, a quoted header cell and an empty file scan as the JavaScript did", func(t *testing.T) {
		dir := filepath.Join(t.TempDir(), "csv_2026-05-12")
		os.Mkdir(dir, 0o755)
		os.WriteFile(filepath.Join(dir, "a.csv"), []byte("date,\"sleepTimeSeconds\",x\r\n2026-05-12,1,2\r\n\r\n2026-05-11,3,4\r\n"), 0o644)
		os.WriteFile(filepath.Join(dir, "b.csv"), []byte(""), 0o644)
		files, err := csvFiles(dir)
		if err != nil {
			t.Fatal(err)
		}
		a, b := files[0], files[1]
		if a.Rows != 2 || a.first != "2026-05-11" || a.last != "2026-05-12" || a.header[1] != "sleepTimeSeconds" {
			t.Errorf("a: %+v", a)
		}
		if b.Rows != 0 || len(b.header) != 1 || b.header[0] != "" {
			t.Errorf("b: %+v (JavaScript gives one empty column name for an empty file)", b)
		}
		ctx := Context("en", Options{StartDate: "2026-05-11", EndDate: "2026-05-12", TZ: "UTC"}, files)
		if !strings.Contains(ctx, "- `a.csv`: a. 2 rows, 2026-05-11 → 2026-05-12.\n  Columns: date, sleepTimeSeconds (s), x\n") {
			t.Errorf("context:\n%s", ctx)
		}
		if !strings.Contains(ctx, "- `b.csv`: b. 0 rows.\n  Columns: \n") {
			t.Errorf("empty file line:\n%s", ctx)
		}
	})
}

func TestLoggedColumnByHeaderNotPosition(t *testing.T) {
	t.Run("EG-603 the MyFitnessPal line counts the logged column wherever the header puts it", func(t *testing.T) {
		dir := filepath.Join(t.TempDir(), "csv_2026-05-12")
		os.Mkdir(dir, 0o755)
		os.WriteFile(filepath.Join(dir, "mfp_daily.csv"), []byte("date,meals_logged,logged\n2026-05-11,2,yes\n2026-05-12,0,no\n"), 0o644)
		files, _ := csvFiles(dir)
		ctx := Context("en", Options{StartDate: "2026-05-11", EndDate: "2026-05-12"}, files)
		if !strings.Contains(ctx, "MyFitnessPal: food logged on 1 of 2 days.") {
			t.Errorf("context:\n%s", ctx)
		}
	})
}

func TestUnitTable(t *testing.T) {
	t.Run("DT-R601 unitOf gives the UNITS answer for 25 column names, first match wins", func(t *testing.T) {
		cases := map[string]string{
			"sleepTimeSeconds": "s", "restingHeartRate": "bpm", "hrTimeInZone_2": "s", "averageSpO2HRSleep": "bpm",
			"averageSpO2Value": "%", "energy_kcal": "kcal", "exercise_kcal_mfp": "kcal", "protein_g": "g", "grams": "g",
			"sodium_mg": "mg", "duration_min": "min", "lastNightAvg": "ms", "baselineLowUpper": "ms", "vo2max": "ml/kg/min",
			"VO2Max": "ml/kg/min", "maxTempC": "°C", "averageSpeed": "m/s", "weight": "g", "calcium": "", "date": "",
			"distance": "m", "calories": "kcal", "hrv": "ms", "durationInMilliseconds": "ms", "hydrationMl": "ml",
		}
		for col, want := range cases {
			if got := unitOf(col); got != want {
				t.Errorf("unitOf(%q) = %q, want %q", col, got, want)
			}
		}
	})
}

func TestDaysBetween(t *testing.T) {
	t.Run("BVA-601 the same day is 1 and 2026-07-17 to 2026-10-14 is 90", func(t *testing.T) {
		if d := daysBetween("2026-10-15", "2026-10-15"); d != 1 {
			t.Errorf("same day = %d", d)
		}
		if d := daysBetween("2026-07-17", "2026-10-14"); d != 90 {
			t.Errorf("90 days = %d", d)
		}
	})
}

func firstDiff(a, b string) int {
	for i := 0; i < len(a) && i < len(b); i++ {
		if a[i] != b[i] {
			return i
		}
	}
	return min(len(a), len(b))
}

func mustRead(t *testing.T, p string) []byte {
	t.Helper()
	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func TestBuildUnlinksASymlinkedPackFolder(t *testing.T) {
	t.Run("EG-609 an llm-en symlink is removed as a link and the folder it points to keeps its files", func(t *testing.T) {
		dir := copyFixture(t)
		outside := t.TempDir()
		keep := filepath.Join(outside, "keep.txt")
		os.WriteFile(keep, []byte("keep"), 0o644)
		if err := os.Symlink(outside, filepath.Join(dir, "llm-en")); err != nil {
			t.Fatal(err)
		}
		if _, err := Build(opts(dir), "en", "files"); err != nil {
			t.Fatal(err)
		}
		if fi, err := os.Lstat(filepath.Join(dir, "llm-en")); err != nil || !fi.IsDir() {
			t.Errorf("llm-en is not a real folder after Build: %v %v", fi, err)
		}
		if b, err := os.ReadFile(keep); err != nil || string(b) != "keep" {
			t.Errorf("the target of the link lost keep.txt: %q %v", b, err)
		}
		if entries, _ := os.ReadDir(outside); len(entries) != 1 {
			t.Errorf("Build wrote into the target of the link: %d entries", len(entries))
		}
	})
}
