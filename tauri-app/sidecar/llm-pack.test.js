import { test, expect } from 'bun:test';
import fs from 'node:fs';
import os from 'node:os';
import path from 'node:path';
import { previewPack, buildPack } from './llm-pack.js';

function exportDir() {
  const dir = fs.mkdtempSync(path.join(os.tmpdir(), 'pack-'));
  fs.writeFileSync(path.join(dir, 'sleep.csv'), 'date,sleepTimeSeconds,restingHeartRate,hrTimeInZone_2,averageSpO2HRSleep\n2026-05-12,25200,52,600,55\n2026-05-11,24000,54,300,56\n');
  fs.writeFileSync(path.join(dir, 'correr.csv'), 'activityId,startTimeLocal,distance\n1,"2026-05-20 07:00:00",5000\n');
  return dir;
}
const opts = (csvDir) => ({ csvDir, startDate: '2026-05-11', endDate: '2026-07-09', warnings: ['hill_score skipped (404)'] });

test('previewPack builds both languages in memory and writes nothing', () => {
  const dir = exportDir();
  const p = previewPack(opts(dir));
  expect(fs.readdirSync(dir).sort()).toEqual(['correr.csv', 'sleep.csv']);
  expect(p.days).toBe(60);
  expect(p.files).toEqual([{ name: 'correr.csv', rows: 1 }, { name: 'sleep.csv', rows: 2 }]);
  expect(p.langs.es.context).toContain('Rango: 2026-05-11 → 2026-07-09 (60 días)');
  expect(p.langs.es.context).toContain('2 filas, 2026-05-11 → 2026-05-12');
  for (const c of ['sleepTimeSeconds (s)', 'restingHeartRate (bpm)', 'hrTimeInZone_2 (s)', 'averageSpO2HRSleep (bpm)']) expect(p.langs.es.context).toContain(c);
  expect(p.langs.en.context).toContain('- hill_score skipped (404)');
  expect(p.langs.en.tokens).toBeGreaterThan(0);
  expect(previewPack({ ...opts(dir), startDate: undefined })).toBe(null);
});

test('buildPack writes one language per subfolder with no duplicated data', () => {
  const dir = exportDir();
  fs.writeFileSync(path.join(dir, 'CONTEXT.es.md'), 'legacy'); // v4.0.0 leftovers get removed
  fs.writeFileSync(path.join(dir, 'garmin_2026-01-01_2026-01-02.en.md'), 'legacy');

  const a = buildPack({ ...opts(dir), lang: 'en', mode: 'files' });
  expect(a.dir).toBe(path.join(dir, 'llm-en'));
  expect(fs.readdirSync(a.dir).sort()).toEqual(['CONTEXT.en.md', 'correr.csv', 'sleep.csv']);
  expect(fs.readFileSync(path.join(a.dir, 'CONTEXT.en.md'), 'utf8')).not.toContain('```csv');

  const b = buildPack({ ...opts(dir), lang: 'es', mode: 'single' });
  expect(fs.readdirSync(b.dir)).toEqual(['garmin_2026-05-11_2026-07-09.es.md']);
  expect(fs.readFileSync(b.file, 'utf8')).toContain('```csv\nactivityId,startTimeLocal,distance');

  expect(fs.readdirSync(dir).sort()).toEqual(['correr.csv', 'llm-en', 'llm-es-one-file', 'sleep.csv']);
  fs.writeFileSync(path.join(a.dir, 'stale.txt'), 'x');
  buildPack({ ...opts(dir), lang: 'en', mode: 'files' }); // a second press replaces the folder
  expect(fs.existsSync(path.join(a.dir, 'stale.txt'))).toBe(false);
  expect(() => buildPack({ ...opts(dir), lang: 'fr', mode: 'files' })).toThrow();
});

test('MyFitnessPal files get descriptions, units and the logged days line', () => {
  const dir = exportDir();
  fs.writeFileSync(path.join(dir, 'mfp_daily.csv'), 'date,logged,meals_logged,foods_logged,energy_kcal,protein_g\n2026-05-11,yes,2,4,1800,120\n2026-05-12,no,0,0,,\n2026-05-13,yes,3,6,2100,140\n');
  fs.writeFileSync(path.join(dir, 'mfp_food.csv'), 'date,meal,description,grams,energy_kcal,sodium_mg,calcium_pct_dv\n2026-05-11,Breakfast,Oats,40,150,2,2\n');
  const p = previewPack(opts(dir));
  const en = p.langs.en.context, es = p.langs.es.context;
  expect(en).toContain('MyFitnessPal: food logged on 2 of 3 days. Days with logged=no have no data, which does not mean that the person did not eat. Join with the Garmin files by date.');
  expect(es).toContain('MyFitnessPal: comida registrada en 2 de 3 días.');
  for (const c of ['energy_kcal (kcal)', 'protein_g (g)', 'sodium_mg (mg)', 'grams (g)', 'calcium_pct_dv (% DV)']) expect(en).toContain(c);
  expect(en).toContain('Food diary from MyFitnessPal, one row per food');
});
