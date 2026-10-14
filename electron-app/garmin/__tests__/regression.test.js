'use strict';
// Regression tests for the CSV output. The fixture is a scrubbed real export
// (scripts/make-fixture.js), and golden/ holds the CSV files that the current
// code writes for it. Any change to a CSV file fails here until the golden
// files are updated on purpose with: UPDATE_GOLDEN=1 node --test garmin/__tests__/regression.test.js
const test = require('node:test');
const assert = require('node:assert');
const fs = require('fs');
const os = require('os');
const path = require('path');
const { generateCsvs } = require('../csv-writer');
const { compareDirs } = require('../../scripts/compare-csv');

const FIXTURE = path.join(__dirname, 'fixtures', 'export-sample.json');
const GOLDEN = path.join(__dirname, 'golden');

function writeCsvs() {
  const out = fs.mkdtempSync(path.join(os.tmpdir(), 'golden-'));
  return generateCsvs(JSON.parse(fs.readFileSync(FIXTURE, 'utf8')), out).csvDir;
}

test('AC-01 CSV files match the golden files cell by cell', () => {
  const csvDir = writeCsvs();
  if (process.env.UPDATE_GOLDEN) {
    fs.rmSync(GOLDEN, { recursive: true, force: true });
    fs.cpSync(csvDir, GOLDEN, { recursive: true });
  }
  const diff = compareDirs(GOLDEN, csvDir);
  assert.deepStrictEqual(diff, { addedFiles: [], removedFiles: [], files: {} }, JSON.stringify(diff, null, 2));
  // Cell equality ignores quoting, BOM and line endings; the files must also match byte for byte.
  for (const f of fs.readdirSync(GOLDEN)) {
    assert.ok(fs.readFileSync(path.join(GOLDEN, f)).equals(fs.readFileSync(path.join(csvDir, f))), `${f} differs in bytes`);
  }
});

test('AC-02 columns that Garmin fills are not empty in the output', () => {
  // Fields that used to come out empty because of wrong key names (4.1.1 fixes).
  const csvDir = writeCsvs();
  const { parseCsv } = require('../../scripts/compare-csv');
  const filled = (file, col) => {
    const [h, ...rows] = parseCsv(fs.readFileSync(path.join(csvDir, file), 'utf8'));
    const i = h.indexOf(col);
    assert.ok(i >= 0, `${file} has no ${col} column`);
    return rows.filter((r) => r[i] !== '').length;
  };
  for (const [file, col] of [
    ['training.csv', 'vo2max'], ['training.csv', 'trainingStatus'], ['training.csv', 'trainingLoad_acute'],
    ['hrv.csv', 'lastNightAvg'], ['running_tolerance.csv', 'acuteImpactLoad'],
    ['daily_summary.csv', 'stepGoal'], ['daily_summary.csv', 'moderateIntensityMinutes'],
    ['misc_metrics.csv', 'lactateThresholdHeartRate'],
  ]) assert.ok(filled(file, col) > 0, `${file} ${col} is empty on every row`);
});
