package mfp

import (
	"bytes"
	"encoding/csv"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func loadFixtureDays(t *testing.T) []DayData {
	var days []DayData
	for _, d := range []string{"2026-10-04", "2026-10-05", "2026-10-06", "2026-10-07"} {
		var out struct{ Items []DiaryItem }
		if b, err := os.ReadFile(filepath.Join("testdata", "diary", d+".json")); err == nil {
			if err := json.Unmarshal(b, &out); err != nil {
				t.Fatal(err)
			}
		}
		days = append(days, DayData{Date: d, Items: out.Items})
	}
	return days
}

func fixtureGoals() Goals {
	f := func(v float64) *float64 { return &v }
	return Goals{EnergyKcal: f(2200), Protein: f(150), Carbohydrate: f(220), Fat: f(73), Sodium: f(2300), Sugar: f(50)}
}

func TestWriteFilesMatchesGolden(t *testing.T) {
	dir := t.TempDir()
	logged, err := WriteFiles(dir, loadFixtureDays(t), fixtureGoals())
	if err != nil {
		t.Fatal(err)
	}
	if logged != 2 {
		t.Fatalf("logged = %d, want 2 (Oct 5 and Oct 7)", logged)
	}
	for _, f := range []string{"mfp_food.csv", "mfp_daily.csv", "mfp_exercise.csv"} {
		got, _ := os.ReadFile(filepath.Join(dir, f))
		golden := filepath.Join("testdata", "golden", f)
		if os.Getenv("UPDATE_GOLDEN") != "" {
			os.MkdirAll(filepath.Dir(golden), 0o755)
			os.WriteFile(golden, got, 0o644)
		}
		want, _ := os.ReadFile(golden)
		if !bytes.Equal(got, want) {
			t.Errorf("%s differs from the golden file:\n%s", f, got)
		}
	}
}

func TestCSVRules(t *testing.T) {
	dir := t.TempDir()
	if _, err := WriteFiles(dir, loadFixtureDays(t), fixtureGoals()); err != nil {
		t.Fatal(err)
	}
	read := func(f string) []map[string]string {
		fh, _ := os.Open(filepath.Join(dir, f))
		defer fh.Close()
		rows, err := csv.NewReader(fh).ReadAll()
		if err != nil {
			t.Fatalf("%s is not valid CSV: %v", f, err)
		}
		var out []map[string]string
		for _, r := range rows[1:] {
			m := map[string]string{}
			for i, h := range rows[0] {
				m[h] = r[i]
			}
			out = append(out, m)
		}
		return out
	}
	food := read("mfp_food.csv")
	if food[0]["description"] != `Pan "Bimbo", integral` || food[1]["description"] != "Arroz con pollo\nde la casa" {
		t.Errorf("quotes, commas or line breaks lost: %q %q", food[0]["description"], food[1]["description"])
	}
	if food[1]["added_sugars_g"] != "" || food[1]["potassium_mg"] != "" {
		t.Error("a null nutrient must be an empty cell, not 0")
	}
	if food[2]["energy_kcal"] != "104.9" {
		t.Errorf("439 kJ must be 104.9 kcal, got %q", food[2]["energy_kcal"])
	}
	daily := read("mfp_daily.csv")
	if len(daily) != 4 {
		t.Fatalf("mfp_daily.csv must have one row per date in the range, got %d", len(daily))
	}
	byDate := map[string]map[string]string{}
	for _, d := range daily {
		byDate[d["date"]] = d
	}
	if d := byDate["2026-10-04"]; d["logged"] != "no" || d["energy_kcal"] != "" || d["goal_energy_kcal"] != "2200" {
		t.Errorf("a day without a log must have empty totals and still show goals: %v", d)
	}
	if d := byDate["2026-10-06"]; d["logged"] != "no" || d["exercise_kcal_mfp"] != "210" {
		t.Errorf("exercise only day: %v", d)
	}
	if d := byDate["2026-10-05"]; d["logged"] != "yes" || d["meals_logged"] != "2" || d["foods_logged"] != "2" || d["energy_kcal"] != "755" {
		t.Errorf("logged day totals: %v", d)
	}
	if ex := read("mfp_exercise.csv"); len(ex) != 2 || ex[0]["duration_min"] != "45" {
		t.Errorf("exercise rows: %v", ex)
	}
}
