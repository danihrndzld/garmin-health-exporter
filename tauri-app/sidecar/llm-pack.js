// LLM pack. Export writes nothing extra; previewPack builds the text in memory for the sheet.
// buildPack runs on a button press and writes ONE language into its own subfolder, so the user
// can drag everything in that folder without sending the same data twice:
//   llm-<lang>/           CONTEXT.<lang>.md + copies of the CSVs
//   llm-<lang>-one-file/  garmin_<start>_<end>.<lang>.md (context + every CSV inline)
import fs from 'node:fs';
import path from 'node:path';

const LANGS = ['es', 'en'];
const DATE_RE = /\b(\d{4}-\d{2}-\d{2})/;

const DESCRIBE = {
  daily_summary: ['Resumen diario: pasos, calorías, distancia, FC, estrés, minutos de intensidad, hidratación', 'Daily summary: steps, calories, distance, heart rate, stress, intensity minutes, hydration'],
  sleep: ['Sueño, una fila por noche: duración por fase, SpO2, respiración, puntaje', 'Sleep, one row per night: stage durations, SpO2, respiration, score'],
  hrv: ['Variabilidad de la frecuencia cardiaca (HRV) nocturna', 'Overnight heart rate variability (HRV)'],
  body_battery: ['Body Battery: carga, descarga y nivel por día', 'Body Battery: charged, drained and level per day'],
  training: ['Estado y preparación de entrenamiento, carga aguda, VO2 máx', 'Training status and readiness, acute load, VO2 max'],
  spo2_respiration: ['Saturación de oxígeno y respiración diarias', 'Daily blood oxygen and respiration'],
  activities: ['Todas las actividades: tipo, duración, distancia, FC, calorías', 'All activities: type, duration, distance, heart rate, calories'],
  weight_body_comp: ['Peso y composición corporal', 'Weight and body composition'],
  blood_pressure: ['Presión arterial', 'Blood pressure'],
  misc_metrics: ['Métricas agregadas: edad fitness, predicciones de carrera, umbral de lactato, FTP', 'Aggregated metrics: fitness age, race predictions, lactate threshold, FTP'],
  running_tolerance: ['Tolerancia de carrera (carga de impacto)', 'Running tolerance (impact load)'],
  daily_steps_aggregated: ['Pasos diarios con meta y distancia', 'Daily steps with goal and distance'],
  weekly_intensity: ['Minutos de intensidad por semana', 'Intensity minutes per week'],
  hill_score_daily: ['Hill score diario', 'Daily hill score'],
  caminar: ['Caminatas, una fila por actividad', 'Walks, one row per activity'],
  correr: ['Carreras, una fila por actividad', 'Runs, one row per activity'],
  gym: ['Fuerza / gimnasio, una fila por actividad', 'Strength / gym, one row per activity'],
  caminar_laps: ['Vueltas (laps) de cada caminata', 'Laps of each walk'],
  correr_laps: ['Vueltas (laps) de cada carrera', 'Laps of each run'],
  gym_laps: ['Series / vueltas de cada sesión de gimnasio', 'Sets / laps of each gym session'],
  mfp_food: ['Diario de comida de MyFitnessPal, una fila por alimento: comida del día, hora, porción y nutrientes. Calcio, hierro y vitamina D van como los guarda MyFitnessPal para cada alimento, que puede ser porcentaje del valor diario o miligramos.', 'Food diary from MyFitnessPal, one row per food: meal, time, serving and nutrients. Calcium, iron and vitamin D are as MyFitnessPal stores them for each food, which can be a percent of daily value or milligrams.'],
  mfp_daily: ['MyFitnessPal por día: si hay registro, totales, metas y calorías de ejercicio registradas en MFP', 'MyFitnessPal per day: whether food was logged, totals, goals and exercise calories logged in MFP'],
  mfp_exercise: ['Ejercicio registrado en MyFitnessPal; puede repetir ejercicio que Garmin ya tiene', 'Exercise logged in MyFitnessPal; it can repeat exercise that Garmin already has'],
};

const UNITS = [
  [/_kcal(_mfp)?$/, 'kcal'], [/_g$|^grams$/, 'g'], [/_mg$/, 'mg'], [/_min$/, 'min'],
  [/TimeInZone/, 's'], [/SpO2HR/i, 'bpm'],
  [/Milliseconds$|InMilliseconds$/i, 'ms'], [/Seconds$|InSeconds$|^duration$|Duration$/, 's'],
  [/Meters$|^distance$|Distance$/, 'm'], [/Kilocalories$|^calories$|Calories$/, 'kcal'],
  [/Hrv$|HRV|hrv/, 'ms'], [/HeartRate$|Hr$|^hr|Bpm$/i, 'bpm'], [/Ml$|InML$/i, 'ml'],
  [/^lastNight|^baseline(Low|Balanced)/, 'ms'], [/^vo2max$/i, 'ml/kg/min'], [/Percent$|Pct$|SpO2/i, '%'], [/C$/, '°C'], [/Speed$/, 'm/s'], [/[Ww]eight$/, 'g'],
];
const unitOf = (col) => (UNITS.find(([re]) => re.test(col)) || [])[1];

const T = {
  es: {
    title: '# Datos de Garmin Connect', range: 'Rango', days: 'días', exported: 'Exportado',
    note: 'Cada archivo es un CSV con encabezado. Fechas locales en formato YYYY-MM-DD. Nombres de columnas tal como los entrega Garmin; la unidad va entre paréntesis.',
    files: '## Archivos', rows: 'filas', cols: 'Columnas', missing: '## Faltantes y avisos', none: '- Ninguno.',
    mfp: (n, total) => `MyFitnessPal: comida registrada en ${n} de ${total} días. Los días con logged=no no tienen datos, lo que no significa que la persona no comió. Une con los archivos de Garmin por fecha.`,
  },
  en: {
    title: '# Garmin Connect data', range: 'Range', days: 'days', exported: 'Exported',
    note: 'Each file is a CSV with a header row. Local dates as YYYY-MM-DD. Column names are as Garmin returns them; the unit is in parentheses.',
    files: '## Files', rows: 'rows', cols: 'Columns', missing: '## Missing data and warnings', none: '- None.',
    mfp: (n, total) => `MyFitnessPal: food logged on ${n} of ${total} days. Days with logged=no have no data, which does not mean that the person did not eat. Join with the Garmin files by date.`,
  },
};

function daysBetween(a, b) {
  return Math.round((new Date(b + 'T00:00:00') - new Date(a + 'T00:00:00')) / 864e5) + 1;
}

function scanCsv(file) {
  const text = fs.readFileSync(file, 'utf8');
  const lines = text.split(/\r?\n/).filter(Boolean);
  const header = (lines[0] || '').split(',').map((h) => h.replace(/^"|"$/g, ''));
  let first = null, last = null;
  for (const line of lines.slice(1)) {
    const m = line.match(DATE_RE);
    if (!m) continue;
    if (!first || m[1] < first) first = m[1];
    if (!last || m[1] > last) last = m[1];
  }
  return { text, header, rows: Math.max(0, lines.length - 1), first, last };
}

function csvFiles(csvDir) {
  return fs.readdirSync(csvDir).filter((f) => f.endsWith('.csv')).sort();
}

export function buildContext({ lang, startDate, endDate, exportedAt, tz, files, warnings }) {
  const t = T[lang];
  const out = [t.title, '', `${t.range}: ${startDate} → ${endDate} (${daysBetween(startDate, endDate)} ${t.days})`,
    `${t.exported}: ${exportedAt} (${tz})`, '', t.note, '', `${t.files} (${files.length})`];
  for (const f of files) {
    const base = f.name.replace(/\.csv$/, '');
    const desc = DESCRIBE[base] ? DESCRIBE[base][lang === 'es' ? 0 : 1] : base;
    const span = f.first ? `, ${f.first} → ${f.last}` : '';
    out.push(`- \`${f.name}\`: ${desc}. ${f.rows} ${t.rows}${span}.`);
    out.push(`  ${t.cols}: ${f.header.map((c) => (unitOf(c) ? `${c} (${unitOf(c)})` : c)).join(', ')}`);
  }
  const daily = files.find((f) => f.name === 'mfp_daily.csv');
  if (daily) {
    const rows = daily.text.split(/\r?\n/).slice(1).filter(Boolean);
    const logged = rows.filter((r) => r.split(',')[1] === 'yes').length;
    out.push('', t.mfp(logged, rows.length));
  }
  out.push('', t.missing, ...(warnings.length ? warnings.map((w) => `- ${w}`) : [t.none]));
  return out.join('\n') + '\n';
}

const LEGACY_RE = /^(CONTEXT|garmin_).*\.(es|en)\.md$/; // v4.0.0 wrote these next to the CSVs

function texts({ csvDir, startDate, endDate, warnings = [], now = new Date() }, lang) {
  const files = csvFiles(csvDir).map((name) => ({ name, ...scanCsv(path.join(csvDir, name)) }));
  const tz = Intl.DateTimeFormat().resolvedOptions().timeZone;
  const exportedAt = `${now.toLocaleDateString('sv-SE')} ${now.toTimeString().slice(0, 5)}`;
  const context = buildContext({ lang, startDate, endDate, exportedAt, tz, files, warnings });
  const single = [context, ...files.map((f) => `## \`${f.name}\`\n\n\`\`\`csv\n${f.text.trimEnd()}\n\`\`\`\n`)].join('\n');
  return { files, context, single };
}

/** In-memory preview for the sheet: both languages, no files written. */
export function previewPack(opts) {
  const { csvDir, startDate, endDate, warnings = [] } = opts;
  if (!csvDir || !startDate || !endDate || !fs.existsSync(csvDir)) return null;
  const langs = {};
  let files = [];
  for (const lang of LANGS) {
    const t = texts(opts, lang);
    files = t.files.map((f) => ({ name: f.name, rows: f.rows }));
    const bytes = Buffer.byteLength(t.single);
    // ponytail: ~3 bytes/token for numeric CSV; good enough for a "fits / doesn't fit" meter.
    langs[lang] = { context: t.context, bytes, tokens: Math.round(bytes / 3), singleName: `garmin_${startDate}_${endDate}.${lang}.md` };
  }
  return { csvDir, startDate, endDate, days: daysBetween(startDate, endDate), files, warnings, langs };
}

/** Write one language, one mode, into its own subfolder (replaced on every press). */
export function buildPack({ lang, mode, ...opts }) {
  if (!LANGS.includes(lang)) throw new Error('lang must be es or en.');
  if (mode !== 'files' && mode !== 'single') throw new Error('mode must be files or single.');
  const { csvDir, startDate, endDate } = opts;
  for (const old of fs.readdirSync(csvDir)) if (LEGACY_RE.test(old)) fs.rmSync(path.join(csvDir, old));
  const t = texts(opts, lang);
  const dir = path.join(csvDir, mode === 'files' ? `llm-${lang}` : `llm-${lang}-one-file`);
  fs.rmSync(dir, { recursive: true, force: true });
  fs.mkdirSync(dir);
  if (mode === 'files') {
    fs.writeFileSync(path.join(dir, `CONTEXT.${lang}.md`), t.context);
    for (const f of t.files) fs.copyFileSync(path.join(csvDir, f.name), path.join(dir, f.name));
    return { dir, count: t.files.length + 1 };
  }
  const file = path.join(dir, `garmin_${startDate}_${endDate}.${lang}.md`);
  fs.writeFileSync(file, t.single);
  return { dir, file, count: 1 };
}
