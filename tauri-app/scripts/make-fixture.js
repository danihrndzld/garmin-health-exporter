#!/usr/bin/env node
// Build the scrubbed test fixture from a real export JSON.
//   bun tauri-app/scripts/make-fixture.js <garmin_health_*.json> tauri-app/core/internal/garmin/testdata/fixtures/export-sample.json
// Keeps a few days and one activity per type, so internal/garmin/csv_test.go sees real
// Garmin response shapes. Personal data is removed or changed before anything
// is written:
//   - ids, names, GUIDs and URLs are replaced with fixed fake values
//   - GPS coordinates, polylines and location names are removed
//   - every date and epoch timestamp moves back by a random 700 to 1400 days
//   - other numbers change by up to 30 percent
// The shift and the jitter come from a random secret made on each run and never
// saved, so the committed fixture cannot be turned back into the real values.
const fs = require('fs');
const crypto = require('crypto');

const SECRET = crypto.randomBytes(32);
const SHIFT_DAYS = 700 + crypto.randomInt(700);
const SHIFT_MS = SHIFT_DAYS * 864e5;
const KEEP_DAYS = 5;

// Any key ending in Id/Pk/Uuid/Guid (userDailySummaryId, deviceId, ...) is an identifier.
const ID_KEY = /(Id|ID|Pk|PK|pk|Uuid|UUID|Guid|GUID)$|^(id|pk)$/;
const SECRET_KEY = /displayName|fullName|userName|email|Email|profileImage|ImageUrl|garminGUID|uuid|UUID|Uuid|ownerFullName|ownerDisplayName|socialProfile|location|Location|description|activityName|deviceName|productDisplayName/;
const GPS_KEY = /latitude|longitude|Latitude|Longitude|polyline|Polyline|geoPolyline|startLat|startLon|endLat|endLon|^lat$|^lon$|^lng$/;
const KEEP_NUMBER_KEY = /(version|sequence|classification|Classification|status|Status|level|Level|typeKey|phrase|Phrase|FeedbackPhrase|feedback|Feedback|Index|index|zone|Zone|^sport|SubSport|lapIndex|Type$|Enum|Code$)/;

function shiftDateString(s) {
  return s.replace(/\b(\d{4})-(\d{2})-(\d{2})/g, (m) => {
    const d = new Date(m + 'T12:00:00Z');
    if (isNaN(d)) return m;
    return new Date(d.getTime() - SHIFT_MS).toISOString().slice(0, 10);
  });
}

const fakeIds = new Map();
function fakeId(v) {
  const k = String(v);
  if (!fakeIds.has(k)) fakeIds.set(k, 1000 + fakeIds.size);
  return typeof v === 'number' ? fakeIds.get(k) : String(fakeIds.get(k));
}

function jitter(n, path) {
  if (!Number.isFinite(n) || n === 0) return n;
  const h = crypto.createHmac('sha256', SECRET).update(path).digest().readUInt32BE(0) / 0xffffffff;
  const v = n * (0.7 + h * 0.6);
  if (Number.isInteger(n)) return Math.round(v);
  const decimals = (String(n).split('.')[1] || '').length;
  return Number(v.toFixed(Math.min(decimals, 6)));
}

function scrub(value, key = '', path = '') {
  // ponytail: long intraday series (sleep levels, stress samples) are cut to 24 items; the CSV
  // writer only reads summaries from them, and the golden files are built from this same fixture.
  if (Array.isArray(value)) return value.slice(0, 24).map((v) => scrub(v, key, `${path}[]`));
  if (value && typeof value === 'object') {
    const out = {};
    for (const [k, v] of Object.entries(value)) {
      if (GPS_KEY.test(k)) continue;
      const outKey = /^\d{7,}$/.test(k) ? String(fakeId(Number(k))) : shiftDateString(k); // device-id map keys
      out[outKey] = scrub(v, k, `${path}.${k.replace(/\d{4}-\d{2}-\d{2}/, 'DATE')}`);
    }
    return out;
  }
  if (typeof value === 'string') {
    if (SECRET_KEY.test(key)) return key.match(/name|Name/) ? `${key}-redacted` : 'redacted';
    if (ID_KEY.test(key)) return fakeId(value);
    if (/^https?:\/\//.test(value)) return 'https://example.invalid/redacted';
    return shiftDateString(value);
  }
  if (typeof value === 'number') {
    if (/^(timezoneOffset|timeZoneId|timeOffset)$/i.test(key)) return 0; // would reveal the region
    if (ID_KEY.test(key)) return fakeId(value);
    if (value > 1.0e12 && value < 2.5e12) return value - SHIFT_MS; // epoch ms
    if (value > 1.0e9 && value < 2.5e9 && /time|Time|stamp|Stamp|date|Date|version|sequence/.test(key)) return value - SHIFT_DAYS * 86400;
    if (KEEP_NUMBER_KEY.test(key)) return value;
    return jitter(value, path);
  }
  return value;
}

function trim(data) {
  const days = Object.keys(data.daily).sort().reverse();
  // the newest KEEP_DAYS days in a row: an export of that range against the fake
  // server must return exactly these days. The newest two are often not synced yet.
  const keep = days.slice(0, KEEP_DAYS);
  const daily = Object.fromEntries(keep.map((d) => [d, data.daily[d]]));
  const seen = new Set();
  const activities = (data.activities || []).filter((a) => {
    const t = a.activityType?.typeKey;
    if (seen.has(t)) return false;
    seen.add(t);
    return true;
  });
  const agg = { ...data.aggregated };
  const inKept = (e) => keep.includes(e?.calendarDate || e?.date);
  if (Array.isArray(agg.daily_steps)) agg.daily_steps = agg.daily_steps.filter(inKept);
  if (Array.isArray(agg.weekly_steps)) agg.weekly_steps = agg.weekly_steps.slice(-3);
  if (Array.isArray(agg.weekly_intensity)) agg.weekly_intensity = agg.weekly_intensity.slice(-3);
  if (agg.hill_score?.hillScoreDTOList) agg.hill_score = { ...agg.hill_score, hillScoreDTOList: agg.hill_score.hillScoreDTOList.filter(inKept) };
  if (agg.progress_summary && Array.isArray(agg.progress_summary)) agg.progress_summary = agg.progress_summary.slice(-3);
  const sorted = [...keep].sort();
  return {
    ...data,
    date_range: { start: sorted[0], end: sorted.at(-1), days: keep.length },
    daily,
    aggregated: agg,
    activities,
    goals: data.goals || [],
  };
}

if (require.main === module) {
  const [src, out] = process.argv.slice(2);
  if (!src || !out) {
    console.error('usage: node scripts/make-fixture.js <garmin_health_*.json> <out.json>');
    process.exit(2);
  }
  const fixture = scrub(trim(JSON.parse(fs.readFileSync(src, 'utf8'))));
  // These pairs come from the same URL, so Garmin's answers are identical; keep them
  // identical after the per-path number jitter too.
  for (const day of Object.values(fixture.daily)) {
    for (const [alias, base] of [['user_summary', 'stats'], ['all_day_stress', 'stress'], ['morning_training_readiness', 'training_readiness']]) {
      if (base in day) day[alias] = day[base];
    }
  }
  fs.writeFileSync(out, JSON.stringify(fixture, null, 1) + '\n');
  console.log(`${out}: ${Object.keys(fixture.daily).length} days, ${fixture.activities.length} activities, ${(fs.statSync(out).size / 1024).toFixed(0)} KB`);
}

module.exports = { scrub, trim, shiftDateString };
