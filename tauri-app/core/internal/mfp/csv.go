package mfp

import (
	"encoding/csv"
	"math"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strconv"
	"strings"
)

type DayData struct {
	Date  string
	Items []DiaryItem
}

var foodHeader = []string{"date", "meal", "meal_position", "consumed_at", "logged_at", "description", "brand", "verified",
	"servings", "serving_size", "serving_unit", "grams", "energy_kcal", "protein_g", "carbohydrates_g", "fiber_g", "sugar_g",
	"added_sugars_g", "fat_g", "saturated_fat_g", "trans_fat_g", "cholesterol_mg", "sodium_mg", "potassium_mg",
	"calcium", "iron", "vitamin_d"}

var dailyHeader = []string{"date", "logged", "meals_logged", "foods_logged", "energy_kcal", "protein_g", "carbohydrates_g",
	"fat_g", "fiber_g", "sugar_g", "sodium_mg", "goal_energy_kcal", "goal_protein_g", "goal_carbohydrates_g", "goal_fat_g",
	"goal_sodium_mg", "goal_sugar_g", "exercise_kcal_mfp"}

var exerciseHeader = []string{"date", "name", "type", "duration_min", "energy_kcal", "source"}

// num formats a value with at most one decimal, and nil as an empty cell.
func num(v *float64) string {
	if v == nil {
		return ""
	}
	return strconv.FormatFloat(math.Round(*v*10)/10, 'f', -1, 64)
}

// grams is value times servings for gram portions only; gram_weight was unreliable in the real API.
func grams(unit string, value, servings float64) string {
	switch strings.ToLower(strings.TrimSpace(unit)) {
	case "g", "gram", "grams", "gramo", "gramos":
		return num(f(value * servings))
	}
	return ""
}

func f(v float64) *float64 { return &v }

func writeCSV(path string, header []string, rows [][]string) error {
	fh, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o644)
	if err != nil {
		return err
	}
	w := csv.NewWriter(fh)
	w.Write(header)
	w.WriteAll(rows)
	if err := w.Error(); err != nil {
		fh.Close()
		return err
	}
	return fh.Close()
}

// sum adds the values that are present; it stays nil when every value is nil.
func sum(acc **float64, v *float64) {
	if v == nil {
		return
	}
	if *acc == nil {
		*acc = f(0)
	}
	**acc += *v
}

// WriteFiles writes the three MFP CSV files and returns the number of days with food.
func WriteFiles(dir string, days []DayData, g Goals) (int, error) {
	days = slices.Clone(days) // sort a copy; the caller's order is not ours to change
	sort.Slice(days, func(i, j int) bool { return days[i].Date < days[j].Date })
	var food, daily, exercise [][]string
	logged := 0
	for _, d := range days {
		var kcal, protein, carbs, fat, fiber, sugar, sodium, exKcal *float64
		meals := map[string]bool{}
		foods := 0
		for _, it := range d.Items {
			switch it.Type {
			case "food_entry":
				n := it.Nutrients
				foods++
				meals[it.MealName] = true
				food = append(food, []string{d.Date, it.MealName, strconv.Itoa(it.MealPosition), it.ConsumedAt, it.LoggedAt,
					it.Food.Description, it.Food.BrandName, strconv.FormatBool(it.Food.Verified),
					num(f(it.Servings)), num(f(it.ServingSize.Value)), it.ServingSize.Unit, grams(it.ServingSize.Unit, it.ServingSize.Value, it.Servings),
					num(n.Energy.Kcal()), num(n.Protein), num(n.Carbohydrate), num(n.Fiber), num(n.Sugar), num(n.AddedSugars),
					num(n.Fat), num(n.SaturatedFat), num(n.TransFat), num(n.Cholesterol), num(n.Sodium), num(n.Potassium),
					num(n.Calcium), num(n.Iron), num(n.VitaminD)})
				sum(&kcal, n.Energy.Kcal())
				sum(&protein, n.Protein)
				sum(&carbs, n.Carbohydrate)
				sum(&fat, n.Fat)
				sum(&fiber, n.Fiber)
				sum(&sugar, n.Sugar)
				sum(&sodium, n.Sodium)
			case "exercise_entry":
				exercise = append(exercise, []string{d.Date, it.Exercise.Description, it.Exercise.Type,
					num(f(it.DurationSec / 60)), num(it.Energy.Kcal()), it.Source})
				sum(&exKcal, it.Energy.Kcal())
			}
		}
		yes := "no"
		if foods > 0 {
			yes = "yes"
			logged++
		}
		daily = append(daily, []string{d.Date, yes, strconv.Itoa(len(meals)), strconv.Itoa(foods),
			num(kcal), num(protein), num(carbs), num(fat), num(fiber), num(sugar), num(sodium),
			num(g.EnergyKcal), num(g.Protein), num(g.Carbohydrate), num(g.Fat), num(g.Sodium), num(g.Sugar), num(exKcal)})
	}
	files := []struct {
		name string
		h    []string
		rows [][]string
	}{
		{"mfp_food.csv", foodHeader, food},
		{"mfp_daily.csv", dailyHeader, daily},
		{"mfp_exercise.csv", exerciseHeader, exercise},
	}
	for _, part := range files {
		if err := writeCSV(filepath.Join(dir, part.name), part.h, part.rows); err != nil {
			return logged, err
		}
	}
	return logged, nil
}
