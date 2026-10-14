import { test, expect } from 'bun:test';
import fs from 'node:fs';
import os from 'node:os';
import path from 'node:path';
import { writePack, readPack } from './llm-pack.js';

test('writePack + readPack round-trip', () => {
  const dir = fs.mkdtempSync(path.join(os.tmpdir(), 'pack-'));
  fs.writeFileSync(path.join(dir, 'sleep.csv'), 'date,sleepTimeSeconds,restingHeartRate,hrTimeInZone_2,averageSpO2HRSleep\n2026-05-12,25200,52,600,55\n2026-05-11,24000,54,300,56\n');
  fs.writeFileSync(path.join(dir, 'correr.csv'), 'activityId,startTimeLocal,distance\n1,"2026-05-20 07:00:00",5000\n');
  // A stale pack from an earlier same-day run must be replaced, not duplicated.
  fs.writeFileSync(path.join(dir, 'garmin_2026-01-01_2026-01-02.es.md'), 'old');

  writePack({ csvDir: dir, startDate: '2026-05-11', endDate: '2026-07-09', warnings: ['hill_score skipped (404)'] });
  const pack = readPack(dir);

  expect(pack.days).toBe(60);
  expect(pack.files).toEqual([{ name: 'correr.csv', rows: 1 }, { name: 'sleep.csv', rows: 2 }]);
  expect(fs.existsSync(path.join(dir, 'garmin_2026-01-01_2026-01-02.es.md'))).toBe(false);
  expect(pack.langs.es.context).toContain('Rango: 2026-05-11 → 2026-07-09 (60 días)');
  expect(pack.langs.es.context).toContain('2 filas, 2026-05-11 → 2026-05-12');
  expect(pack.langs.es.context).toContain('sleepTimeSeconds (s)');
  expect(pack.langs.es.context).toContain('restingHeartRate (bpm)');
  expect(pack.langs.es.context).toContain('hrTimeInZone_2 (s)');
  expect(pack.langs.es.context).toContain('averageSpO2HRSleep (bpm)');
  expect(pack.langs.en.context).toContain('- hill_score skipped (404)');
  expect(pack.langs.en.single).toContain('```csv\nactivityId,startTimeLocal,distance');
  expect(pack.tokens).toBeGreaterThan(0);
  expect(readPack(fs.mkdtempSync(path.join(os.tmpdir(), 'nopack-')))).toBe(null);
});
