package garmin

import (
	"bytes"
	"encoding/csv"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/danihrndzld/garmin-health-exporter/tauri-app/core/internal/jsjson"
)

var (
	fixturePath = filepath.Join("..", "..", "..", "..", "electron-app", "garmin", "__tests__", "fixtures", "export-sample.json")
	goldenDir   = filepath.Join("..", "..", "..", "..", "electron-app", "garmin", "__tests__", "golden")
)

// firstDiff is the index of the first byte where a and b differ, or the shorter length.
func firstDiff(a, b []byte) int {
	for i := range min(len(a), len(b)) {
		if a[i] != b[i] {
			return i
		}
	}
	return min(len(a), len(b))
}

func writeFixtureCSVs(t *testing.T) (string, []string) {
	t.Helper()
	raw, err := os.ReadFile(fixturePath)
	if err != nil {
		t.Fatal(err)
	}
	export, err := jsjson.Parse(raw)
	if err != nil {
		t.Fatal(err)
	}
	csvDir, written, err := WriteCSVs(export, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	return csvDir, written
}

// sameFiles fails t for each file in wantDir that csvDir lacks or holds with other bytes.
func sameFiles(t *testing.T, csvDir, wantDir string, names []string) {
	t.Helper()
	for _, name := range names {
		want, err := os.ReadFile(filepath.Join(wantDir, name))
		if err != nil {
			t.Fatal(err)
		}
		got, err := os.ReadFile(filepath.Join(csvDir, name))
		if err != nil {
			t.Fatalf("%s not written", name)
		}
		if !bytes.Equal(got, want) {
			i := firstDiff(got, want)
			t.Errorf("%s differs from the expected file; first difference at byte %d\n got: %q\nwant: %q",
				name, i, got[max(0, i-60):min(len(got), i+60)], want[max(0, i-60):min(len(want), i+60)])
		}
	}
}

func TestAC301CSVFilesEqualTheGoldenFilesByteForByte(t *testing.T) {
	t.Run("AC-301 ports AC-01: the 18 CSV files equal the golden files byte for byte", func(t *testing.T) {
		csvDir, written := writeFixtureCSVs(t)
		golden, err := os.ReadDir(goldenDir)
		if err != nil {
			t.Fatal(err)
		}
		var names []string
		for _, g := range golden {
			names = append(names, g.Name())
		}
		if len(written) != len(golden) || len(golden) != 18 {
			t.Fatalf("wrote %v, golden has %d files", written, len(golden))
		}
		sameFiles(t, csvDir, goldenDir, names)
	})
}

func TestAC302ColumnsThatGarminFillsAreNotEmpty(t *testing.T) {
	csvDir, _ := writeFixtureCSVs(t)
	for _, c := range [][2]string{
		{"training.csv", "vo2max"}, {"training.csv", "trainingStatus"}, {"training.csv", "trainingLoad_acute"},
		{"hrv.csv", "lastNightAvg"}, {"running_tolerance.csv", "acuteImpactLoad"},
		{"daily_summary.csv", "stepGoal"}, {"daily_summary.csv", "moderateIntensityMinutes"},
		{"misc_metrics.csv", "lactateThresholdHeartRate"},
	} {
		t.Run("AC-302 ports AC-02: "+c[0]+" "+c[1]+" has a filled cell", func(t *testing.T) {
			fh, err := os.Open(filepath.Join(csvDir, c[0]))
			if err != nil {
				t.Fatal(err)
			}
			defer fh.Close()
			rows, err := csv.NewReader(fh).ReadAll()
			if err != nil {
				t.Fatal(err)
			}
			i := slices.Index(rows[0], c[1])
			if i < 0 {
				t.Fatalf("%s has no %s column", c[0], c[1])
			}
			for _, r := range rows[1:] {
				if r[i] != "" {
					return
				}
			}
			t.Fatalf("%s %s is empty on every row", c[0], c[1])
		})
	}
}

// matchJavaScript writes testdata/<name>/export.json and compares it with the files that the
// JavaScript writer made from the same input.
func matchJavaScript(t *testing.T, name string, files ...string) {
	dir := filepath.Join("testdata", name)
	raw, err := os.ReadFile(filepath.Join(dir, "export.json"))
	if err != nil {
		t.Fatal(err)
	}
	export, err := jsjson.Parse(raw)
	if err != nil {
		t.Fatal(err)
	}
	out := t.TempDir()
	csvDir, written, err := WriteCSVs(export, out)
	if err != nil {
		t.Fatal(err)
	}
	if csvDir != filepath.Join(out, "csv_2026-10-15") || !slices.Equal(written, files) {
		t.Fatalf("WriteCSVs = %q %v", csvDir, written)
	}
	sameFiles(t, csvDir, dir, written)
}

func TestWeightAndBloodPressureMatchJavaScript(t *testing.T) {
	t.Run("AC-303 weight_body_comp.csv and blood_pressure.csv equal the files the JavaScript writer made from testdata/weight_bp", func(t *testing.T) {
		matchJavaScript(t, "weight_bp", "weight_body_comp.csv", "blood_pressure.csv")
	})
}

// The fixture has steps as a list on every day, readiness lists of at most one item and one
// primary device, so these branches need their own input.
func TestDailyBranchesMatchJavaScript(t *testing.T) {
	t.Run("AC-304 steps as null, object or list, the last readiness item, and the device key 1001 before abc equal the JavaScript files", func(t *testing.T) {
		matchJavaScript(t, "daily_branches", "daily_summary.csv", "training.csv", "spo2_respiration.csv")
	})
}
