'use strict';

const assert = require('assert');
const fs = require('fs');
const path = require('path');
const os = require('os');
const { initCache } = require('../cache');

let tmpDir;
let dbPath;

function makeTmpDir() {
  tmpDir = fs.mkdtempSync(path.join(os.tmpdir(), 'garmin-cache-test-'));
  dbPath = path.join(tmpDir, 'test-cache.db');
}

function cleanup() {
  if (tmpDir && fs.existsSync(tmpDir)) {
    fs.rmSync(tmpDir, { recursive: true, force: true });
  }
}

async function testDailySetGet() {
  makeTmpDir();
  try {
    const cache = await initCache(dbPath);
    const data = { steps: 10000, calories: 2500 };
    cache.setCachedDaily('2025-01-15', 'dailySummary', data);
    const result = cache.getCachedDaily('2025-01-15', 'dailySummary');
    assert.deepStrictEqual(result, data);
    // miss returns null
    assert.strictEqual(cache.getCachedDaily('2025-01-16', 'dailySummary'), null);
    assert.strictEqual(cache.getCachedDaily('2025-01-15', 'otherEndpoint'), null);
    cache.close();
    console.log('  PASS: daily set/get');
  } finally {
    cleanup();
  }
}

async function testActivitySetGet() {
  makeTmpDir();
  try {
    const cache = await initCache(dbPath);
    const data = { avgHR: 145, maxHR: 180 };
    cache.setCachedActivity('12345', 'heartRate', data);
    const result = cache.getCachedActivity('12345', 'heartRate');
    assert.deepStrictEqual(result, data);
    assert.strictEqual(cache.getCachedActivity('12345', 'splits'), null);
    assert.strictEqual(cache.getCachedActivity('99999', 'heartRate'), null);
    cache.close();
    console.log('  PASS: activity set/get');
  } finally {
    cleanup();
  }
}

async function testIsWithinRefreshWindow() {
  makeTmpDir();
  try {
    const cache = await initCache(dbPath);
    const today = new Date();
    const todayStr = today.toISOString().slice(0, 10);
    assert.strictEqual(cache.isWithinRefreshWindow(todayStr, 3), true);

    const tenDaysAgo = new Date(today);
    tenDaysAgo.setDate(tenDaysAgo.getDate() - 10);
    const tenDaysAgoStr = tenDaysAgo.toISOString().slice(0, 10);
    assert.strictEqual(cache.isWithinRefreshWindow(tenDaysAgoStr, 3), false);

    // boundary: exactly 3 days ago should NOT be within window (diffDays == 3, not < 3)
    const threeDaysAgo = new Date(today);
    threeDaysAgo.setDate(threeDaysAgo.getDate() - 3);
    const threeDaysAgoStr = threeDaysAgo.toISOString().slice(0, 10);
    assert.strictEqual(cache.isWithinRefreshWindow(threeDaysAgoStr, 3), false);

    cache.close();
    console.log('  PASS: isWithinRefreshWindow');
  } finally {
    cleanup();
  }
}

async function testClearAll() {
  makeTmpDir();
  try {
    const cache = await initCache(dbPath);
    cache.setCachedDaily('2025-01-15', 'steps', { steps: 5000 });
    cache.setCachedActivity('111', 'hr', { hr: 70 });
    cache.clearAll();
    assert.strictEqual(cache.getCachedDaily('2025-01-15', 'steps'), null);
    assert.strictEqual(cache.getCachedActivity('111', 'hr'), null);
    cache.close();
    console.log('  PASS: clearAll');
  } finally {
    cleanup();
  }
}

async function testGetStats() {
  makeTmpDir();
  try {
    const cache = await initCache(dbPath);
    assert.deepStrictEqual(cache.getStats(), { dailyCount: 0, activityCount: 0 });
    cache.setCachedDaily('2025-01-15', 'steps', {});
    cache.setCachedDaily('2025-01-16', 'steps', {});
    cache.setCachedActivity('1', 'hr', {});
    assert.deepStrictEqual(cache.getStats(), { dailyCount: 2, activityCount: 1 });
    cache.close();
    console.log('  PASS: getStats');
  } finally {
    cleanup();
  }
}

async function testDuplicateSetOverwrites() {
  makeTmpDir();
  try {
    const cache = await initCache(dbPath);
    cache.setCachedDaily('2025-01-15', 'steps', { steps: 1000 });
    cache.setCachedDaily('2025-01-15', 'steps', { steps: 9999 });
    const result = cache.getCachedDaily('2025-01-15', 'steps');
    assert.deepStrictEqual(result, { steps: 9999 });
    assert.deepStrictEqual(cache.getStats(), { dailyCount: 1, activityCount: 0 });
    cache.close();
    console.log('  PASS: duplicate set overwrites');
  } finally {
    cleanup();
  }
}

async function testNewDatabaseCreatedWhenMissing() {
  makeTmpDir();
  try {
    assert.strictEqual(fs.existsSync(dbPath), false);
    const cache = await initCache(dbPath);
    cache.setCachedDaily('2025-01-15', 'steps', { steps: 42 });
    cache.save();
    assert.strictEqual(fs.existsSync(dbPath), true);
    cache.close();
    console.log('  PASS: creates new database when file missing');
  } finally {
    cleanup();
  }
}

async function testSchemaVersionMismatch() {
  makeTmpDir();
  try {
    // Create a database with a wrong schema version
    const cache1 = await initCache(dbPath);
    cache1.setCachedDaily('2025-01-15', 'steps', { steps: 100 });
    // Tamper with schema version
    cache1._db.run("UPDATE meta SET value = '999' WHERE key = 'schema_version'");
    cache1.save();
    cache1._db.close();
    cache1._db = null;

    // Reopen — should detect mismatch and recreate
    const cache2 = await initCache(dbPath);
    // Old data should be gone
    assert.strictEqual(cache2.getCachedDaily('2025-01-15', 'steps'), null);
    assert.deepStrictEqual(cache2.getStats(), { dailyCount: 0, activityCount: 0 });
    cache2.close();
    console.log('  PASS: schema version mismatch recreates database');
  } finally {
    cleanup();
  }
}

async function testPersistenceAcrossCloseReopen() {
  makeTmpDir();
  try {
    const cache1 = await initCache(dbPath);
    cache1.setCachedDaily('2025-01-15', 'steps', { steps: 7777 });
    cache1.setCachedActivity('42', 'splits', { laps: [1, 2, 3] });
    cache1.close(); // save + close

    const cache2 = await initCache(dbPath);
    assert.deepStrictEqual(cache2.getCachedDaily('2025-01-15', 'steps'), { steps: 7777 });
    assert.deepStrictEqual(cache2.getCachedActivity('42', 'splits'), { laps: [1, 2, 3] });
    assert.deepStrictEqual(cache2.getStats(), { dailyCount: 1, activityCount: 1 });
    cache2.close();
    console.log('  PASS: data persists across close/reopen');
  } finally {
    cleanup();
  }
}

async function main() {
  console.log('Running cache tests...\n');
  await testDailySetGet();
  await testActivitySetGet();
  await testIsWithinRefreshWindow();
  await testClearAll();
  await testGetStats();
  await testDuplicateSetOverwrites();
  await testNewDatabaseCreatedWhenMissing();
  await testSchemaVersionMismatch();
  await testPersistenceAcrossCloseReopen();
  console.log('\nAll cache tests passed!');
}

main().catch(err => {
  console.error('TEST FAILURE:', err);
  process.exit(1);
});
