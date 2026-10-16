package mfp

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

var csvNames = []string{"mfp_food.csv", "mfp_daily.csv", "mfp_exercise.csv"}

func loadFixtureDays(t *testing.T) []DayData {
	t.Helper()
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

// fixtureCSVs writes the fixture days (the account of the spike, scrubbed) once for read-only subtests.
func fixtureCSVs(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	if _, err := WriteFiles(dir, loadFixtureDays(t), fixtureGoals()); err != nil {
		t.Fatal(err)
	}
	return dir
}

func TestWriteFilesMatchesGolden(t *testing.T) {
	t.Parallel()
	dir := fixtureCSVs(t)
	for _, f := range csvNames {
		name := map[string]string{
			"mfp_food.csv":     "CL-112 mfp_food.csv equals the golden file",
			"mfp_daily.csv":    "CL-113 mfp_daily.csv equals the golden file",
			"mfp_exercise.csv": "CL-114 mfp_exercise.csv equals the golden file",
		}[f]
		t.Run(name, func(t *testing.T) {
			t.Parallel()
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
		})
	}
}

// The headers are typed out from the spec's "Output files" section, not read from the code.
func TestHeadersMatchTheSpec(t *testing.T) {
	t.Parallel()
	dir := fixtureCSVs(t)
	cases := []struct {
		name, file string
		want       []string
	}{
		{"CL-115 mfp_food.csv has the 27 columns of the spec, in order", "mfp_food.csv", strings.Split(
			"date,meal,meal_position,consumed_at,logged_at,description,brand,verified,servings,serving_size,serving_unit,grams,"+
				"energy_kcal,protein_g,carbohydrates_g,fiber_g,sugar_g,added_sugars_g,fat_g,saturated_fat_g,trans_fat_g,"+
				"cholesterol_mg,sodium_mg,potassium_mg,calcium,iron,vitamin_d", ",")},
		{"CL-116 mfp_daily.csv has the 18 columns of the spec, in order", "mfp_daily.csv", strings.Split(
			"date,logged,meals_logged,foods_logged,energy_kcal,protein_g,carbohydrates_g,fat_g,fiber_g,sugar_g,sodium_mg,"+
				"goal_energy_kcal,goal_protein_g,goal_carbohydrates_g,goal_fat_g,goal_sodium_mg,goal_sugar_g,exercise_kcal_mfp", ",")},
		{"CL-117 mfp_exercise.csv has the 6 columns of the spec, in order", "mfp_exercise.csv",
			[]string{"date", "name", "type", "duration_min", "energy_kcal", "source"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if h, _ := readCSV(t, filepath.Join(dir, tc.file)); !reflect.DeepEqual(h, tc.want) {
				t.Fatalf("header = %v\nwant     %v", h, tc.want)
			}
		})
	}
}

func TestSpecRulesOnTheFixture(t *testing.T) {
	t.Parallel()
	dir := fixtureCSVs(t)
	food := readCSVMaps(t, filepath.Join(dir, "mfp_food.csv"))
	daily := readCSVMaps(t, filepath.Join(dir, "mfp_daily.csv"))
	exercise := readCSVMaps(t, filepath.Join(dir, "mfp_exercise.csv"))
	byDate := map[string]map[string]string{}
	for _, d := range daily {
		byDate[d["date"]] = d
	}
	t.Run("CL-118 quotes, commas and line breaks in a description survive a CSV round trip", func(t *testing.T) {
		t.Parallel()
		if food[0]["description"] != `Pan "Bimbo", integral` || food[1]["description"] != "Arroz con pollo\nde la casa" {
			t.Errorf("descriptions = %q %q", food[0]["description"], food[1]["description"])
		}
	})
	t.Run("CL-119 a null nutrient is an empty cell, not 0", func(t *testing.T) {
		t.Parallel()
		if food[1]["added_sugars_g"] != "" || food[1]["potassium_mg"] != "" || food[0]["vitamin_d"] != "" {
			t.Errorf("row = %v", food[1])
		}
	})
	t.Run("CL-120 an entry in kilojoules (439 kJ) is written as 104.9 kcal", func(t *testing.T) {
		t.Parallel()
		if food[2]["energy_kcal"] != "104.9" {
			t.Errorf("energy_kcal = %q", food[2]["energy_kcal"])
		}
	})
	t.Run("CL-121 nutrients are totals for the logged amount and are not multiplied by servings", func(t *testing.T) {
		t.Parallel()
		if food[0]["servings"] != "2" || food[0]["energy_kcal"] != "140" || food[0]["protein_g"] != "6" {
			t.Errorf("2 servings of a 140 kcal entry: %v", food[0])
		}
	})
	t.Run("CL-122 mfp_daily.csv has one row for every date in the range", func(t *testing.T) {
		t.Parallel()
		if len(daily) != 4 {
			t.Fatalf("rows = %d, want 4", len(daily))
		}
	})
	t.Run("CL-123 a day with logged=no has empty totals and still shows the goals", func(t *testing.T) {
		t.Parallel()
		d := byDate["2026-10-04"]
		if d["logged"] != "no" || d["energy_kcal"] != "" || d["protein_g"] != "" || d["sodium_mg"] != "" || d["goal_energy_kcal"] != "2200" {
			t.Errorf("day = %v", d)
		}
	})
	t.Run("CL-124 a day with only exercise is logged=no and keeps exercise_kcal_mfp", func(t *testing.T) {
		t.Parallel()
		if d := byDate["2026-10-06"]; d["logged"] != "no" || d["foods_logged"] != "0" || d["exercise_kcal_mfp"] != "210" {
			t.Errorf("day = %v", d)
		}
	})
	t.Run("CL-125 a logged day counts meals and foods and sums the energy", func(t *testing.T) {
		t.Parallel()
		if d := byDate["2026-10-05"]; d["logged"] != "yes" || d["meals_logged"] != "2" || d["foods_logged"] != "2" || d["energy_kcal"] != "755" {
			t.Errorf("day = %v", d)
		}
	})
	t.Run("CL-126 mfp_exercise.csv has one row per exercise entry with the duration in minutes", func(t *testing.T) {
		t.Parallel()
		if len(exercise) != 2 || exercise[0]["duration_min"] != "45" || exercise[1]["source"] != "Garmin" {
			t.Errorf("exercise rows = %v", exercise)
		}
	})
}

func TestGramsOnlyFromGramPortions(t *testing.T) {
	t.Parallel()
	cases := []struct{ name, unit, value, servings, want string }{
		{"EP-142 unit g gives value times servings", "g", "1", "74", "74"},
		{"EP-143 unit gram gives value times servings", "gram", "100", "1.5", "150"},
		{"EP-144 unit grams gives value times servings", "grams", "50", "2", "100"},
		{"EP-145 unit gramo gives value times servings", "gramo", "30", "2", "60"},
		{"EP-146 unit gramos gives value times servings", "gramos", "215", "2", "430"},
		{"EP-147 the unit is lowercased and trimmed first", " GRAMOS ", "1", "26", "26"},
		{"EP-148 unit ml leaves grams empty", "ml", "1", "712", ""},
		{"EP-149 a household unit (slice) leaves grams empty", "slice", "1", "2", ""},
		{"EP-150 kg is not in the list, so grams stay empty", "kg", "1", "1", ""},
		{"EP-151 an empty unit leaves grams empty", "", "1", "1", ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			items := parseItems(t, `{"type":"food_entry","serving_size":{"unit":"`+tc.unit+`","value":`+tc.value+`,"gram_weight":0.01},"servings":`+tc.servings+`}`)
			dir := t.TempDir()
			if _, err := WriteFiles(dir, []DayData{{Date: "2026-10-14", Items: items}}, Goals{}); err != nil {
				t.Fatal(err)
			}
			if got := readCSVMaps(t, filepath.Join(dir, "mfp_food.csv"))[0]["grams"]; got != tc.want {
				t.Fatalf("grams = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestDiaryItemClasses(t *testing.T) {
	t.Parallel()
	food := `{"type":"food_entry","meal_name":"Lunch","servings":1,"serving_size":{"unit":"g","value":10},"nutritional_contents":{"energy":{"unit":"calories","value":100}}}`
	cases := []struct {
		name  string
		items []string
		foods int // rows expected in mfp_food.csv
		exer  int // rows expected in mfp_exercise.csv
		daily map[string]string
	}{
		{"EP-152 steps_aggregate items add no row to any file", []string{`{"type":"steps_aggregate","date":"2026-10-14"}`}, 0, 0,
			map[string]string{"logged": "no", "foods_logged": "0", "energy_kcal": "", "exercise_kcal_mfp": ""}},
		{"EP-153 an unknown item type is ignored", []string{`{"type":"diary_meal"}`, food}, 1, 0,
			map[string]string{"logged": "yes", "foods_logged": "1", "energy_kcal": "100"}},
		{"EP-154 two foods in the same meal count as one meal", []string{food, food}, 2, 0,
			map[string]string{"meals_logged": "1", "foods_logged": "2", "energy_kcal": "200"}},
		{"EP-155 foods in two different meals count as two meals", []string{food, strings.Replace(food, "Lunch", "Dinner", 1)}, 2, 0,
			map[string]string{"meals_logged": "2", "foods_logged": "2"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			dir := t.TempDir()
			if _, err := WriteFiles(dir, []DayData{{Date: "2026-10-14", Items: parseItems(t, tc.items...)}}, Goals{}); err != nil {
				t.Fatal(err)
			}
			if n := len(readCSVMaps(t, filepath.Join(dir, "mfp_food.csv"))); n != tc.foods {
				t.Errorf("food rows = %d, want %d", n, tc.foods)
			}
			if n := len(readCSVMaps(t, filepath.Join(dir, "mfp_exercise.csv"))); n != tc.exer {
				t.Errorf("exercise rows = %d, want %d", n, tc.exer)
			}
			d := readCSVMaps(t, filepath.Join(dir, "mfp_daily.csv"))[0]
			for k, v := range tc.daily {
				if d[k] != v {
					t.Errorf("%s = %q, want %q", k, d[k], v)
				}
			}
		})
	}
}

func TestDayIsKeyedByTheDiaryDate(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	// Logged at 23:50 on the 5th and consumed after midnight: the diary day is still the 5th.
	items := parseItems(t, `{"type":"food_entry","date":"2026-10-05","meal_name":"Late","consumed_at":"2026-10-06T00:10:00-06:00","servings":1}`)
	if _, err := WriteFiles(dir, []DayData{{Date: "2026-10-05", Items: items}, {Date: "2026-10-06"}}, Goals{}); err != nil {
		t.Fatal(err)
	}
	t.Run("CL-127 a food consumed after midnight stays on the diary date, not on consumed_at", func(t *testing.T) {
		food := readCSVMaps(t, filepath.Join(dir, "mfp_food.csv"))
		daily := readCSVMaps(t, filepath.Join(dir, "mfp_daily.csv"))
		if food[0]["date"] != "2026-10-05" || daily[0]["foods_logged"] != "1" || daily[1]["foods_logged"] != "0" {
			t.Fatalf("food = %v, daily = %v", food, daily)
		}
	})
}

func TestWriteFilesFaults(t *testing.T) {
	t.Parallel()
	t.Run("EG-118 unicode food names (accents, CJK, emoji) are written unchanged", func(t *testing.T) {
		t.Parallel()
		name := "Café con leche ☕ 豆腐 año"
		items := parseItems(t, `{"type":"food_entry","food":{"description":"`+name+`","brand_name":"Ñandú"}}`)
		dir := t.TempDir()
		if _, err := WriteFiles(dir, []DayData{{Date: "2026-10-14", Items: items}}, Goals{}); err != nil {
			t.Fatal(err)
		}
		if r := readCSVMaps(t, filepath.Join(dir, "mfp_food.csv"))[0]; r["description"] != name || r["brand"] != "Ñandú" {
			t.Fatalf("row = %v", r)
		}
	})
	t.Run("EG-119 a description that looks like a spreadsheet formula stays one verbatim cell", func(t *testing.T) {
		t.Parallel()
		// The spec asks for values as they are (the files go to an LLM, not to a spreadsheet), so no escaping is expected.
		for _, d := range []string{`=HYPERLINK("http://evil","x")`, "+1+1", "-2+3", "@SUM(A1)", "\t=1"} {
			items := parseItems(t, `{"type":"food_entry","food":{"description":`+mustJSON(d)+`}}`)
			dir := t.TempDir()
			if _, err := WriteFiles(dir, []DayData{{Date: "2026-10-14", Items: items}}, Goals{}); err != nil {
				t.Fatal(err)
			}
			h, rows := readCSV(t, filepath.Join(dir, "mfp_food.csv"))
			if len(rows) != 1 || len(rows[0]) != len(h) || rows[0][5] != d {
				t.Errorf("%q came back as %q (%d columns)", d, rows[0][5], len(rows[0]))
			}
		}
	})
	t.Run("EP-156 an exercise energy in kilojoules is written as kcal", func(t *testing.T) {
		t.Parallel()
		dir := t.TempDir()
		items := parseItems(t, `{"type":"exercise_entry","exercise":{"description":"Run"},"duration":1800,"energy":{"unit":"kilojoules","value":2092}}`)
		if _, err := WriteFiles(dir, []DayData{{Date: "2026-10-14", Items: items}}, Goals{}); err != nil {
			t.Fatal(err)
		}
		if ex := readCSVMaps(t, filepath.Join(dir, "mfp_exercise.csv"))[0]; ex["energy_kcal"] != "500" {
			t.Fatalf("exercise = %v", ex)
		}
		if d := readCSVMaps(t, filepath.Join(dir, "mfp_daily.csv"))[0]; d["exercise_kcal_mfp"] != "500" {
			t.Fatalf("daily = %v", d)
		}
	})
	t.Run("EG-120 an entry with no nutritional_contents gives empty nutrient cells and no panic", func(t *testing.T) {
		t.Parallel()
		dir := t.TempDir()
		items := parseItems(t, `{"type":"food_entry","meal_name":"Snack","servings":1}`, `{"type":"exercise_entry","exercise":{"description":"Run"},"duration":600}`)
		if _, err := WriteFiles(dir, []DayData{{Date: "2026-10-14", Items: items}}, Goals{}); err != nil {
			t.Fatal(err)
		}
		food := readCSVMaps(t, filepath.Join(dir, "mfp_food.csv"))[0]
		ex := readCSVMaps(t, filepath.Join(dir, "mfp_exercise.csv"))[0]
		if food["energy_kcal"] != "" || food["protein_g"] != "" || ex["energy_kcal"] != "" || ex["duration_min"] != "10" {
			t.Fatalf("food = %v, exercise = %v", food, ex)
		}
	})
	t.Run("EG-121 days given out of order are written oldest first", func(t *testing.T) {
		t.Parallel()
		dir := t.TempDir()
		if _, err := WriteFiles(dir, []DayData{{Date: "2026-10-07"}, {Date: "2026-10-05"}, {Date: "2026-10-06"}}, Goals{}); err != nil {
			t.Fatal(err)
		}
		var got []string
		for _, r := range readCSVMaps(t, filepath.Join(dir, "mfp_daily.csv")) {
			got = append(got, r["date"])
		}
		if !reflect.DeepEqual(got, []string{"2026-10-05", "2026-10-06", "2026-10-07"}) {
			t.Fatalf("dates = %v", got)
		}
	})
	t.Run("EG-122 no days at all still writes the three files with only a header", func(t *testing.T) {
		t.Parallel()
		dir := t.TempDir()
		logged, err := WriteFiles(dir, nil, Goals{})
		if err != nil || logged != 0 {
			t.Fatalf("logged = %d, err = %v", logged, err)
		}
		for _, f := range csvNames {
			if _, rows := readCSV(t, filepath.Join(dir, f)); len(rows) != 0 {
				t.Errorf("%s has %d rows", f, len(rows))
			}
		}
	})
	t.Run("EG-123 a csv dir that does not exist is an error, not a panic", func(t *testing.T) {
		t.Parallel()
		if _, err := WriteFiles(filepath.Join(t.TempDir(), "missing"), nil, Goals{}); err == nil {
			t.Fatal("expected an error")
		}
	})
}

func mustJSON(s string) string {
	b, _ := json.Marshal(s)
	return string(b)
}
