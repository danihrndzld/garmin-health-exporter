'use strict';

const assert = require('assert');
const fs = require('fs');
const os = require('os');
const path = require('path');

const {
  flatten,
  formatCsvField,
  writeCsv,
  generateCsvs,
  flattenDetails,
  flattenLaps,
  groupFor,
  TYPE_GROUPS,
} = require('../csv-writer');

let tmpDir;
let passed = 0;
let failed = 0;

function setup() {
  tmpDir = fs.mkdtempSync(path.join(os.tmpdir(), 'csv-writer-test-'));
}

function cleanup() {
  fs.rmSync(tmpDir, { recursive: true, force: true });
}

function test(name, fn) {
  try {
    fn();
    passed++;
    console.log(`  PASS: ${name}`);
  } catch (err) {
    failed++;
    console.log(`  FAIL: ${name}`);
    console.log(`        ${err.message}`);
  }
}

// ---------------------------------------------------------------------------
// flatten() tests
// ---------------------------------------------------------------------------

test('flatten: nested objects produce underscore-joined keys', () => {
  const result = flatten({ a: { b: 1, c: { d: 2 } } });
  assert.deepStrictEqual(result, { a_b: 1, a_c_d: 2 });
});

test('flatten: array values become JSON strings', () => {
  const result = flatten({ a: [1, 2, 3] });
  assert.strictEqual(result.a, '[1,2,3]');
});

test('flatten: mixed nested with arrays', () => {
  const result = flatten({ x: { y: [4, 5] }, z: 'hello' });
  assert.strictEqual(result.x_y, '[4,5]');
  assert.strictEqual(result.z, 'hello');
});

test('flatten: null/undefined values pass through', () => {
  const result = flatten({ a: null, b: undefined, c: 3 });
  assert.strictEqual(result.a, null);
  assert.strictEqual(result.b, undefined);
  assert.strictEqual(result.c, 3);
});

test('flatten: empty object returns empty', () => {
  assert.deepStrictEqual(flatten({}), {});
});

test('flatten: with prefix', () => {
  const result = flatten({ x: 1 }, 'pre');
  assert.deepStrictEqual(result, { pre_x: 1 });
});

// ---------------------------------------------------------------------------
// formatCsvField() tests
// ---------------------------------------------------------------------------

test('formatCsvField: null renders as empty string', () => {
  assert.strictEqual(formatCsvField(null), '');
});

test('formatCsvField: undefined renders as empty string', () => {
  assert.strictEqual(formatCsvField(undefined), '');
});

test('formatCsvField: plain string unchanged', () => {
  assert.strictEqual(formatCsvField('hello'), 'hello');
});

test('formatCsvField: field with comma is quoted', () => {
  assert.strictEqual(formatCsvField('a,b'), '"a,b"');
});

test('formatCsvField: field with double quote is escaped and quoted', () => {
  assert.strictEqual(formatCsvField('say "hi"'), '"say ""hi"""');
});

test('formatCsvField: field with newline is quoted', () => {
  assert.strictEqual(formatCsvField('line1\nline2'), '"line1\nline2"');
});

test('formatCsvField: number is converted to string', () => {
  assert.strictEqual(formatCsvField(42), '42');
});

// ---------------------------------------------------------------------------
// writeCsv() tests
// ---------------------------------------------------------------------------

test('writeCsv: generates correct header and data rows', () => {
  setup();
  const filePath = path.join(tmpDir, 'test.csv');
  writeCsv([
    { name: 'Alice', age: 30 },
    { name: 'Bob', age: 25 },
  ], filePath);
  const content = fs.readFileSync(filePath, 'utf8');
  const lines = content.trim().split('\n');
  assert.strictEqual(lines[0], 'name,age');
  assert.strictEqual(lines[1], 'Alice,30');
  assert.strictEqual(lines[2], 'Bob,25');
  cleanup();
});

test('writeCsv: null/undefined values render as empty strings in output', () => {
  setup();
  const filePath = path.join(tmpDir, 'nulls.csv');
  writeCsv([
    { a: 1, b: null, c: undefined },
  ], filePath);
  const content = fs.readFileSync(filePath, 'utf8');
  const lines = content.trim().split('\n');
  assert.strictEqual(lines[0], 'a,b,c');
  assert.strictEqual(lines[1], '1,,');
  cleanup();
});

test('writeCsv: fields with commas are properly quoted', () => {
  setup();
  const filePath = path.join(tmpDir, 'commas.csv');
  writeCsv([{ val: 'hello, world' }], filePath);
  const content = fs.readFileSync(filePath, 'utf8');
  const lines = content.trim().split('\n');
  assert.strictEqual(lines[1], '"hello, world"');
  cleanup();
});

test('writeCsv: fields with quotes are escaped (doubled)', () => {
  setup();
  const filePath = path.join(tmpDir, 'quotes.csv');
  writeCsv([{ val: 'say "hi"' }], filePath);
  const content = fs.readFileSync(filePath, 'utf8');
  const lines = content.trim().split('\n');
  assert.strictEqual(lines[1], '"say ""hi"""');
  cleanup();
});

test('writeCsv: fields with newlines are properly quoted', () => {
  setup();
  const filePath = path.join(tmpDir, 'newlines.csv');
  writeCsv([{ val: 'line1\nline2' }], filePath);
  const content = fs.readFileSync(filePath, 'utf8');
  // The header is 'val', then the data has the quoted multiline field
  assert.ok(content.includes('"line1\nline2"'));
  cleanup();
});

test('writeCsv: empty rows array produces no file', () => {
  setup();
  const filePath = path.join(tmpDir, 'empty.csv');
  writeCsv([], filePath);
  assert.strictEqual(fs.existsSync(filePath), false);
  cleanup();
});

test('writeCsv: collects keys from all rows (union)', () => {
  setup();
  const filePath = path.join(tmpDir, 'union.csv');
  writeCsv([
    { a: 1 },
    { a: 2, b: 3 },
  ], filePath);
  const content = fs.readFileSync(filePath, 'utf8');
  const lines = content.trim().split('\n');
  assert.strictEqual(lines[0], 'a,b');
  assert.strictEqual(lines[1], '1,');
  assert.strictEqual(lines[2], '2,3');
  cleanup();
});

// ---------------------------------------------------------------------------
// flattenDetails() tests
// ---------------------------------------------------------------------------

test('flattenDetails: produces avg/max/min for each metric', () => {
  const details = {
    metricDescriptors: [
      { metricsIndex: 0, key: 'directHeartRate' },
      { metricsIndex: 1, key: 'directSpeed' },
    ],
    activityDetailMetrics: [
      { metrics: [120, 3.5] },
      { metrics: [140, 4.0] },
      { metrics: [130, 3.0] },
    ],
  };
  const row = flattenDetails(details);
  assert.strictEqual(row.avg_directHeartRate, 130);
  assert.strictEqual(row.max_directHeartRate, 140);
  assert.strictEqual(row.min_directHeartRate, 120);
  assert.strictEqual(row.max_directSpeed, 4.0);
  assert.strictEqual(row.min_directSpeed, 3.0);
});

test('flattenDetails: skips directLatitude/Longitude/Timestamp', () => {
  const details = {
    metricDescriptors: [
      { metricsIndex: 0, key: 'directLatitude' },
      { metricsIndex: 1, key: 'directLongitude' },
      { metricsIndex: 2, key: 'directTimestamp' },
      { metricsIndex: 3, key: 'directHeartRate' },
    ],
    activityDetailMetrics: [
      { metrics: [40.0, -3.0, 1000, 120] },
    ],
  };
  const row = flattenDetails(details);
  assert.strictEqual(row.avg_directLatitude, undefined);
  assert.strictEqual(row.avg_directLongitude, undefined);
  assert.strictEqual(row.avg_directTimestamp, undefined);
  assert.strictEqual(row.avg_directHeartRate, 120);
});

test('flattenDetails: empty details returns empty object', () => {
  assert.deepStrictEqual(flattenDetails({}), {});
});

// ---------------------------------------------------------------------------
// flattenLaps() tests
// ---------------------------------------------------------------------------

test('flattenLaps: produces lap_ prefixed keys', () => {
  const splits = {
    lapDTOs: [
      { lapIndex: 0, distance: 1000, duration: 300, averageHR: 130 },
      { lapIndex: 1, distance: 1000, duration: 290, averageHR: 140 },
    ],
  };
  const rows = flattenLaps(splits);
  assert.strictEqual(rows.length, 2);
  assert.strictEqual(rows[0].lap_lapIndex, 0);
  assert.strictEqual(rows[0].lap_distance, 1000);
  assert.strictEqual(rows[1].lap_averageHR, 140);
});

test('flattenLaps: missing splits returns empty array', () => {
  assert.deepStrictEqual(flattenLaps({}), []);
});

// ---------------------------------------------------------------------------
// groupFor() / TYPE_GROUPS tests
// ---------------------------------------------------------------------------

test('groupFor: walking -> caminar', () => {
  assert.strictEqual(groupFor('walking'), 'caminar');
});

test('groupFor: hiking -> caminar', () => {
  assert.strictEqual(groupFor('hiking'), 'caminar');
});

test('groupFor: running -> correr', () => {
  assert.strictEqual(groupFor('running'), 'correr');
});

test('groupFor: treadmill_running -> correr', () => {
  assert.strictEqual(groupFor('treadmill_running'), 'correr');
});

test('groupFor: strength_training -> gym', () => {
  assert.strictEqual(groupFor('strength_training'), 'gym');
});

test('groupFor: indoor_cardio -> gym', () => {
  assert.strictEqual(groupFor('indoor_cardio'), 'gym');
});

test('groupFor: unknown type -> null', () => {
  assert.strictEqual(groupFor('cycling'), null);
});

// ---------------------------------------------------------------------------
// generateCsvs() integration test
// ---------------------------------------------------------------------------

test('generateCsvs: writes expected CSV files from sample data', () => {
  setup();
  const sampleData = {
    export_date: '2024-01-15',
    daily: {
      '2024-01-15': {
        stats: { totalSteps: 8000, totalKilocalories: 2200 },
        heart_rates: { maxHeartRate: 165, restingHeartRate: 55 },
        stress: { maxStressLevel: 80, avgStressLevel: 35 },
        intensity_minutes: { moderateIntensityMinutes: 30, vigorousIntensityMinutes: 15 },
        steps: { dailyStepGoal: 10000, totalSteps: 8000 },
        hydration: { goalInML: 2500, valueInML: 1800 },
        sleep: {
          dailySleepDTO: { sleepTimeSeconds: 28800, deepSleepSeconds: 7200 },
          restlessMomentsCount: 5,
          avgOvernightHrv: 42,
        },
        hrv: {
          hrvSummary: { weeklyAvg: 45, lastNight: 42, status: 'BALANCED' },
        },
        body_battery: [
          { charged: 60, drained: 45 },
        ],
        training_readiness: { level: 'PRIME' },
        morning_training_readiness: {},
        training_status: {
          mostRecentVO2Max: { vo2MaxPreciseValue: 48.5 },
        },
        spo2: { averageSpO2: 96 },
        respiration: { avgWakingRespirationValue: 16 },
        endurance_score: { overallScore: 72 },
      },
    },
    aggregated: {
      daily_steps: [
        { calendarDate: '2024-01-15', totalSteps: 8000, stepGoal: 10000 },
      ],
      weekly_intensity: [
        { calendarDate: '2024-01-15', weeklyGoal: 150, moderateValue: 30 },
      ],
    },
    activities: [
      {
        activityId: 123,
        activityName: 'Morning Run',
        startTimeLocal: '2024-01-15 07:00:00',
        distance: 5000,
        duration: 1800,
        activityType: { typeKey: 'running' },
      },
    ],
  };

  const result = generateCsvs(sampleData, tmpDir);
  assert.strictEqual(result.csvDir, path.join(tmpDir, 'csv_2024-01-15'));
  assert.ok(fs.existsSync(result.csvDir));

  // Check that expected files exist
  const expectedFiles = [
    'daily_summary.csv', 'sleep.csv', 'hrv.csv', 'body_battery.csv',
    'training.csv', 'spo2_respiration.csv', 'activities.csv',
    'daily_steps_aggregated.csv', 'weekly_intensity.csv',
  ];
  for (const f of expectedFiles) {
    const p = path.join(result.csvDir, f);
    assert.ok(fs.existsSync(p), `Expected file ${f} to exist`);
  }

  // Verify daily_summary content
  const dailyCsv = fs.readFileSync(path.join(result.csvDir, 'daily_summary.csv'), 'utf8');
  const headerLine = dailyCsv.split('\n')[0];
  assert.ok(headerLine.includes('date'));
  assert.ok(headerLine.includes('totalSteps'));
  assert.ok(headerLine.includes('totalKilocalories'));

  // Verify activities content
  const actCsv = fs.readFileSync(path.join(result.csvDir, 'activities.csv'), 'utf8');
  assert.ok(actCsv.includes('Morning Run'));
  assert.ok(actCsv.includes('running'));

  cleanup();
});

// ---------------------------------------------------------------------------
// Summary
// ---------------------------------------------------------------------------

console.log(`\nResults: ${passed} passed, ${failed} failed out of ${passed + failed} tests`);
if (failed > 0) process.exit(1);
