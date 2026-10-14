// LLM pack: after each export, write CONTEXT.<lang>.md (what the data is, no ask)
// and garmin_<start>_<end>.<lang>.md (context + every CSV inline) next to the CSVs.
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
};

const UNITS = [
  [/TimeInZone/, 's'], [/SpO2HR/i, 'bpm'],
  [/Milliseconds$|InMilliseconds$/i, 'ms'], [/Seconds$|InSeconds$|^duration$|Duration$/, 's'],
  [/Meters$|^distance$|Distance$/, 'm'], [/Kilocalories$|^calories$|Calories$/, 'kcal'],
  [/Hrv$|HRV|hrv/, 'ms'], [/HeartRate$|Hr$|^hr|Bpm$/i, 'bpm'], [/Ml$|InML$/i, 'ml'],
  [/Percent$|Pct$|SpO2/i, '%'], [/C$/, '°C'], [/Speed$/, 'm/s'], [/[Ww]eight$/, 'g'],
];
const unitOf = (col) => (UNITS.find(([re]) => re.test(col)) || [])[1];

const T = {
  es: {
    title: '# Datos de Garmin Connect', range: 'Rango', days: 'días', exported: 'Exportado',
    note: 'Cada archivo es un CSV con encabezado. Fechas locales en formato YYYY-MM-DD. Nombres de columnas tal como los entrega Garmin; la unidad va entre paréntesis.',
    files: '## Archivos', rows: 'filas', cols: 'Columnas', missing: '## Faltantes y avisos', none: '- Ninguno.',
  },
  en: {
    title: '# Garmin Connect data', range: 'Range', days: 'days', exported: 'Exported',
    note: 'Each file is a CSV with a header row. Local dates as YYYY-MM-DD. Column names are as Garmin returns them; the unit is in parentheses.',
    files: '## Files', rows: 'rows', cols: 'Columns', missing: '## Missing data and warnings', none: '- None.',
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
  out.push('', t.missing, ...(warnings.length ? warnings.map((w) => `- ${w}`) : [t.none]));
  return out.join('\n') + '\n';
}

/** Write the 4 pack files into csvDir. Returns nothing; read back with readPack. */
export function writePack({ csvDir, startDate, endDate, warnings = [], now = new Date() }) {
  const files = csvFiles(csvDir).map((name) => ({ name, ...scanCsv(path.join(csvDir, name)) }));
  const tz = Intl.DateTimeFormat().resolvedOptions().timeZone;
  const exportedAt = `${now.toLocaleDateString('sv-SE')} ${now.toTimeString().slice(0, 5)}`;
  for (const old of fs.readdirSync(csvDir)) if (/^(CONTEXT|garmin_).*\.(es|en)\.md$/.test(old)) fs.rmSync(path.join(csvDir, old));
  for (const lang of LANGS) {
    const context = buildContext({ lang, startDate, endDate, exportedAt, tz, files, warnings });
    fs.writeFileSync(path.join(csvDir, `CONTEXT.${lang}.md`), context);
    const single = [context, ...files.map((f) => `## \`${f.name}\`\n\n\`\`\`csv\n${f.text.trimEnd()}\n\`\`\`\n`)].join('\n');
    fs.writeFileSync(path.join(csvDir, `garmin_${startDate}_${endDate}.${lang}.md`), single);
  }
}

/** Read the pack back from disk (works for any past export that has one). */
export function readPack(csvDir) {
  if (!csvDir || !fs.existsSync(csvDir)) return null;
  const names = fs.readdirSync(csvDir);
  const singles = names.map((n) => n.match(/^garmin_(\d{4}-\d{2}-\d{2})_(\d{4}-\d{2}-\d{2})\.es\.md$/)).filter(Boolean);
  if (!singles.length || !names.includes('CONTEXT.es.md')) return null;
  const [, startDate, endDate] = singles[0];
  const langs = {};
  for (const lang of LANGS) {
    const contextPath = path.join(csvDir, `CONTEXT.${lang}.md`);
    const singlePath = path.join(csvDir, `garmin_${startDate}_${endDate}.${lang}.md`);
    if (!fs.existsSync(contextPath) || !fs.existsSync(singlePath)) continue;
    langs[lang] = { contextPath, singlePath, context: fs.readFileSync(contextPath, 'utf8'), single: fs.readFileSync(singlePath, 'utf8') };
  }
  const files = csvFiles(csvDir).map((name) => ({ name, rows: scanCsv(path.join(csvDir, name)).rows }));
  const bytes = Buffer.byteLength(langs.es?.single || '');
  // ponytail: ~3 bytes/token for numeric CSV; good enough for a "fits / doesn't fit" meter.
  return { csvDir, startDate, endDate, days: daysBetween(startDate, endDate), files, bytes, tokens: Math.round(bytes / 3), langs };
}
