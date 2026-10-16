// Package pack builds the LLM pack: CONTEXT.<lang>.md next to the CSV files, or one Markdown
// file with every CSV inline. Port of llm-pack.js, ported from the JavaScript sidecar (removed in stage 3; see tag archive/electron-app and git history).
// The files in testdata/golden were written by that JavaScript and pin the bytes.
package pack

import (
	"errors"
	"math"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf16"
)

// Options is one pack request. Now and TZ fill the Exported line; cmd resolves both.
type Options struct {
	CSVDir, StartDate, EndDate string
	Warnings                   []string
	Now                        time.Time // the Exported line; cmd passes time.Now()
	TZ                         string    // the zone name in the Exported line; cmd resolves it (os.Getenv is forbidden here)
}

// File is one scanned CSV. Only Name and Rows reach the wire.
type File struct {
	Name              string `json:"name"`
	Rows              int    `json:"rows"`
	header            []string
	text, first, last string
}

// Lang is the preview of one language.
type Lang struct {
	Context    string `json:"context"`
	Bytes      int    `json:"bytes"`
	Tokens     int    `json:"tokens"`
	SingleName string `json:"singleName"`
}

// Preview is what previewPack returned.
type Preview struct {
	CSVDir    string          `json:"csvDir"`
	StartDate string          `json:"startDate"`
	EndDate   string          `json:"endDate"`
	Days      int             `json:"days"`
	Files     []File          `json:"files"`    // never nil
	Warnings  []string        `json:"warnings"` // never nil
	Langs     map[string]Lang `json:"langs"`
}

// Result is what buildPack returned.
type Result struct {
	Dir   string `json:"dir"`
	File  string `json:"file,omitempty"` // single mode only, as buildPack
	Count int    `json:"count"`
}

// llm-pack.js lines 9-10
var Langs = []string{"es", "en"}

var dateRe = regexp.MustCompile(`\b(\d{4}-\d{2}-\d{2})`)

// llm-pack.js lines 12-36: DESCRIBE, [es, en] per file base name.
var describe = map[string][2]string{
	"daily_summary":          {"Resumen diario: pasos, calorías, distancia, FC, estrés, minutos de intensidad, hidratación", "Daily summary: steps, calories, distance, heart rate, stress, intensity minutes, hydration"},
	"sleep":                  {"Sueño, una fila por noche: duración por fase, SpO2, respiración, puntaje", "Sleep, one row per night: stage durations, SpO2, respiration, score"},
	"hrv":                    {"Variabilidad de la frecuencia cardiaca (HRV) nocturna", "Overnight heart rate variability (HRV)"},
	"body_battery":           {"Body Battery: carga, descarga y nivel por día", "Body Battery: charged, drained and level per day"},
	"training":               {"Estado y preparación de entrenamiento, carga aguda, VO2 máx", "Training status and readiness, acute load, VO2 max"},
	"spo2_respiration":       {"Saturación de oxígeno y respiración diarias", "Daily blood oxygen and respiration"},
	"activities":             {"Todas las actividades: tipo, duración, distancia, FC, calorías", "All activities: type, duration, distance, heart rate, calories"},
	"weight_body_comp":       {"Peso y composición corporal", "Weight and body composition"},
	"blood_pressure":         {"Presión arterial", "Blood pressure"},
	"misc_metrics":           {"Métricas agregadas: edad fitness, predicciones de carrera, umbral de lactato, FTP", "Aggregated metrics: fitness age, race predictions, lactate threshold, FTP"},
	"running_tolerance":      {"Tolerancia de carrera (carga de impacto)", "Running tolerance (impact load)"},
	"daily_steps_aggregated": {"Pasos diarios con meta y distancia", "Daily steps with goal and distance"},
	"weekly_intensity":       {"Minutos de intensidad por semana", "Intensity minutes per week"},
	"hill_score_daily":       {"Hill score diario", "Daily hill score"},
	"caminar":                {"Caminatas, una fila por actividad", "Walks, one row per activity"},
	"correr":                 {"Carreras, una fila por actividad", "Runs, one row per activity"},
	"gym":                    {"Fuerza / gimnasio, una fila por actividad", "Strength / gym, one row per activity"},
	"caminar_laps":           {"Vueltas (laps) de cada caminata", "Laps of each walk"},
	"correr_laps":            {"Vueltas (laps) de cada carrera", "Laps of each run"},
	"gym_laps":               {"Series / vueltas de cada sesión de gimnasio", "Sets / laps of each gym session"},
	"mfp_food":               {"Diario de comida de MyFitnessPal, una fila por alimento: comida del día, hora, porción y nutrientes. Calcio, hierro y vitamina D van como los guarda MyFitnessPal para cada alimento, que puede ser porcentaje del valor diario o miligramos.", "Food diary from MyFitnessPal, one row per food: meal, time, serving and nutrients. Calcium, iron and vitamin D are as MyFitnessPal stores them for each food, which can be a percent of daily value or milligrams."},
	"mfp_daily":              {"MyFitnessPal por día: si hay registro, totales, metas y calorías de ejercicio registradas en MFP", "MyFitnessPal per day: whether food was logged, totals, goals and exercise calories logged in MFP"},
	"mfp_exercise":           {"Ejercicio registrado en MyFitnessPal; puede repetir ejercicio que Garmin ya tiene", "Exercise logged in MyFitnessPal; it can repeat exercise that Garmin already has"},
}

// llm-pack.js lines 38-45: UNITS in order; the first match wins. (?i) stands for the /i flag.
// ponytail: RE2's (?i) also folds s with U+017F and k with U+212A; no column name holds either.
var units = []struct {
	re   *regexp.Regexp
	unit string
}{
	{regexp.MustCompile(`_kcal(_mfp)?$`), "kcal"}, {regexp.MustCompile(`_g$|^grams$`), "g"}, {regexp.MustCompile(`_mg$`), "mg"}, {regexp.MustCompile(`_min$`), "min"},
	{regexp.MustCompile(`TimeInZone`), "s"}, {regexp.MustCompile(`(?i)SpO2HR`), "bpm"},
	{regexp.MustCompile(`(?i)Milliseconds$|InMilliseconds$`), "ms"}, {regexp.MustCompile(`Seconds$|InSeconds$|^duration$|Duration$`), "s"},
	{regexp.MustCompile(`Meters$|^distance$|Distance$`), "m"}, {regexp.MustCompile(`Kilocalories$|^calories$|Calories$`), "kcal"},
	{regexp.MustCompile(`Hrv$|HRV|hrv`), "ms"}, {regexp.MustCompile(`(?i)HeartRate$|Hr$|^hr|Bpm$`), "bpm"}, {regexp.MustCompile(`(?i)Ml$|InML$`), "ml"},
	{regexp.MustCompile(`^lastNight|^baseline(Low|Balanced)`), "ms"}, {regexp.MustCompile(`(?i)^vo2max$`), "ml/kg/min"}, {regexp.MustCompile(`(?i)Percent$|Pct$|SpO2`), "%"}, {regexp.MustCompile(`C$`), "°C"}, {regexp.MustCompile(`Speed$`), "m/s"}, {regexp.MustCompile(`[Ww]eight$`), "g"},
}

// llm-pack.js line 46: unitOf, "" where JavaScript gave undefined.
func unitOf(col string) string {
	for _, u := range units {
		if u.re.MatchString(col) {
			return u.unit
		}
	}
	return ""
}

// llm-pack.js lines 48-61: T.
type words struct {
	title, rangeW, days, exported, note, files, rows, cols, missing, none string
	mfp                                                                   func(n, total int) string
}

var t = map[string]words{
	"es": {
		title: "# Datos de Garmin Connect", rangeW: "Rango", days: "días", exported: "Exportado",
		note:  "Cada archivo es un CSV con encabezado. Fechas locales en formato YYYY-MM-DD. Nombres de columnas tal como los entrega Garmin; la unidad va entre paréntesis.",
		files: "## Archivos", rows: "filas", cols: "Columnas", missing: "## Faltantes y avisos", none: "- Ninguno.",
		mfp: func(n, total int) string {
			return "MyFitnessPal: comida registrada en " + strconv.Itoa(n) + " de " + strconv.Itoa(total) + " días. Los días con logged=no no tienen datos, lo que no significa que la persona no comió. Une con los archivos de Garmin por fecha."
		},
	},
	"en": {
		title: "# Garmin Connect data", rangeW: "Range", days: "days", exported: "Exported",
		note:  "Each file is a CSV with a header row. Local dates as YYYY-MM-DD. Column names are as Garmin returns them; the unit is in parentheses.",
		files: "## Files", rows: "rows", cols: "Columns", missing: "## Missing data and warnings", none: "- None.",
		mfp: func(n, total int) string {
			return "MyFitnessPal: food logged on " + strconv.Itoa(n) + " of " + strconv.Itoa(total) + " days. Days with logged=no have no data, which does not mean that the person did not eat. Join with the Garmin files by date."
		},
	},
}

// llm-pack.js lines 63-65: inclusive count of calendar days. Both dates parse as UTC, so no
// DST day is 23 or 25 hours and no rounding is needed.
func daysBetween(a, b string) int {
	s, _ := time.Parse("2006-01-02", a)
	e, _ := time.Parse("2006-01-02", b)
	return int(e.Sub(s).Hours()/24) + 1
}

// splitLines is text.split(/\r?\n/).filter(Boolean): a \r goes only when a \n follows it.
func splitLines(text string) []string {
	parts := strings.Split(text, "\n")
	out := []string{}
	for i, l := range parts {
		if i < len(parts)-1 {
			l = strings.TrimSuffix(l, "\r")
		}
		if l != "" {
			out = append(out, l)
		}
	}
	return out
}

// llm-pack.js lines 67-79: scanCsv. The header is the first line split on commas with one
// leading and one trailing quote removed.
func scanCSV(path string) (File, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return File{}, err
	}
	f := File{Name: filepath.Base(path), text: string(b)}
	lines := splitLines(f.text)
	first := ""
	if len(lines) > 0 {
		first = lines[0]
	}
	for _, h := range strings.Split(first, ",") { // "".split(",") is [""] in JavaScript: one empty column
		f.header = append(f.header, strings.TrimSuffix(strings.TrimPrefix(h, `"`), `"`))
	}
	f.Rows = max(0, len(lines)-1)
	for _, l := range lines[min(1, len(lines)):] {
		m := dateRe.FindStringSubmatch(l)
		if m == nil {
			continue
		}
		if f.first == "" || m[1] < f.first {
			f.first = m[1]
		}
		if f.last == "" || m[1] > f.last {
			f.last = m[1]
		}
	}
	return f, nil
}

// llm-pack.js lines 81-83: csvFiles, plus the scan of each file. Array.prototype.sort orders by
// UTF-16 code units, which differs from byte order for names above U+FFFF.
func csvFiles(dir string) ([]File, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	var names []string
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), ".csv") {
			names = append(names, e.Name())
		}
	}
	slices.SortFunc(names, func(a, b string) int {
		return slices.Compare(utf16.Encode([]rune(a)), utf16.Encode([]rune(b)))
	})
	files := make([]File, 0, len(names))
	for _, n := range names {
		f, err := scanCSV(filepath.Join(dir, n))
		if err != nil {
			return nil, err
		}
		files = append(files, f)
	}
	return files, nil
}

// Context ports buildContext (llm-pack.js lines 85-105). The exported line is
// toLocaleDateString('sv-SE') + toTimeString().slice(0, 5) of o.Now, with the zone name o.TZ.
func Context(lang string, o Options, files []File) string {
	w := t[lang]
	out := []string{w.title, "", w.rangeW + ": " + o.StartDate + " → " + o.EndDate + " (" + strconv.Itoa(daysBetween(o.StartDate, o.EndDate)) + " " + w.days + ")",
		w.exported + ": " + o.Now.Format("2006-01-02 15:04") + " (" + o.TZ + ")", "", w.note, "", w.files + " (" + strconv.Itoa(len(files)) + ")"}
	for _, f := range files {
		base := strings.TrimSuffix(f.Name, ".csv")
		desc := base
		if d, ok := describe[base]; ok {
			desc = d[1]
			if lang == "es" {
				desc = d[0]
			}
		}
		span := ""
		if f.first != "" {
			span = ", " + f.first + " → " + f.last
		}
		out = append(out, "- `"+f.Name+"`: "+desc+". "+strconv.Itoa(f.Rows)+" "+w.rows+span+".")
		cols := make([]string, len(f.header))
		for i, c := range f.header {
			cols[i] = c
			if u := unitOf(c); u != "" {
				cols[i] = c + " (" + u + ")"
			}
		}
		out = append(out, "  "+w.cols+": "+strings.Join(cols, ", "))
	}
	if i := slices.IndexFunc(files, func(f File) bool { return f.Name == "mfp_daily.csv" }); i >= 0 {
		daily := files[i]
		parts := strings.Split(daily.text, "\n")
		rows := splitLines(strings.Join(parts[1:], "\n")) // the first raw line goes before the filter
		col := slices.Index(daily.header, "logged")
		logged := 0
		for _, r := range rows {
			if cells := strings.Split(r, ","); col >= 0 && col < len(cells) && cells[col] == "yes" {
				logged++
			}
		}
		out = append(out, "", w.mfp(logged, len(rows)))
	}
	out = append(out, "", w.missing)
	if len(o.Warnings) == 0 {
		out = append(out, w.none)
	}
	for _, warn := range o.Warnings {
		out = append(out, "- "+warn)
	}
	return strings.Join(out, "\n") + "\n"
}

// llm-pack.js line 107
var legacyRe = regexp.MustCompile(`^(CONTEXT|garmin_).*\.(es|en)\.md$`) // v4.0.0 wrote these next to the CSVs

// trimEnd ports String.prototype.trimEnd: Unicode White_Space plus U+FEFF, without U+0085.
func trimEnd(s string) string {
	return strings.TrimRightFunc(s, func(r rune) bool { return r != '\u0085' && unicode.IsSpace(r) || r == '\uFEFF' })
}

// single is the one-file text of texts (llm-pack.js line 114).
func single(ctx string, files []File) string {
	parts := []string{ctx}
	for _, f := range files {
		parts = append(parts, "## `"+f.Name+"`\n\n```csv\n"+trimEnd(f.text)+"\n```\n")
	}
	return strings.Join(parts, "\n")
}

func singleName(o Options, lang string) string {
	return "garmin_" + o.StartDate + "_" + o.EndDate + "." + lang + ".md"
}

// BuildPreview ports previewPack (llm-pack.js lines 118-132): both languages in memory, no files
// written. It returns nil, nil when previewPack returned null.
func BuildPreview(o Options) (*Preview, error) {
	if o.CSVDir == "" || o.StartDate == "" || o.EndDate == "" {
		return nil, nil
	}
	if _, err := os.Stat(o.CSVDir); err != nil {
		return nil, nil
	}
	files, err := csvFiles(o.CSVDir)
	if err != nil {
		return nil, err
	}
	p := &Preview{CSVDir: o.CSVDir, StartDate: o.StartDate, EndDate: o.EndDate, Days: daysBetween(o.StartDate, o.EndDate),
		Files: files, Warnings: o.Warnings, Langs: map[string]Lang{}}
	if p.Warnings == nil {
		p.Warnings = []string{}
	}
	for _, lang := range Langs {
		ctx := Context(lang, o, files)
		n := len(single(ctx, files))
		// ponytail: ~3 bytes/token for numeric CSV; good enough for a "fits / doesn't fit" meter.
		p.Langs[lang] = Lang{Context: ctx, Bytes: n, Tokens: int(math.Round(float64(n) / 3)), SingleName: singleName(o, lang)}
	}
	return p, nil
}

// Build ports buildPack (llm-pack.js lines 134-152): one language, one mode, written into its
// own subfolder, which is replaced on every press.
func Build(o Options, lang, mode string) (*Result, error) {
	if !slices.Contains(Langs, lang) {
		return nil, errors.New("lang must be es or en.")
	}
	if mode != "files" && mode != "single" {
		return nil, errors.New("mode must be files or single.")
	}
	entries, err := os.ReadDir(o.CSVDir)
	if err != nil {
		return nil, err
	}
	for _, e := range entries {
		if legacyRe.MatchString(e.Name()) {
			if err := os.Remove(filepath.Join(o.CSVDir, e.Name())); err != nil {
				return nil, err
			}
		}
	}
	files, err := csvFiles(o.CSVDir)
	if err != nil {
		return nil, err
	}
	ctx := Context(lang, o, files)
	dir := filepath.Join(o.CSVDir, "llm-"+lang+"-one-file")
	if mode == "files" {
		dir = filepath.Join(o.CSVDir, "llm-"+lang)
	}
	if err := os.RemoveAll(dir); err != nil {
		return nil, err
	}
	if err := os.Mkdir(dir, 0o755); err != nil {
		return nil, err
	}
	if mode == "files" {
		if err := os.WriteFile(filepath.Join(dir, "CONTEXT."+lang+".md"), []byte(ctx), 0o644); err != nil {
			return nil, err
		}
		for _, f := range files { // f.text holds the raw bytes, so this is copyFileSync
			if err := os.WriteFile(filepath.Join(dir, f.Name), []byte(f.text), 0o644); err != nil {
				return nil, err
			}
		}
		return &Result{Dir: dir, Count: len(files) + 1}, nil
	}
	file := filepath.Join(dir, singleName(o, lang))
	if err := os.WriteFile(file, []byte(single(ctx, files)), 0o644); err != nil {
		return nil, err
	}
	return &Result{Dir: dir, File: file, Count: 1}, nil
}
