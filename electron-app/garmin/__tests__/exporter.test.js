#!/usr/bin/env node
/**
 * Tests for garmin/exporter.js
 *
 * Plain Node.js test runner using assert -- no frameworks needed.
 * Run: node electron-app/garmin/__tests__/exporter.test.js
 *
 * Mocks auth, client, cache, and csv-writer to test the orchestrator logic
 * without any network or file-system side effects.
 */

'use strict';

const assert = require('assert');
const path = require('path');

// ---------------------------------------------------------------------------
// Test helpers
// ---------------------------------------------------------------------------

let passed = 0;
let failed = 0;

async function test(name, fn) {
  try {
    await fn();
    passed++;
    console.log(`  \u2713 ${name}`);
  } catch (err) {
    failed++;
    console.error(`  \u2717 ${name}`);
    console.error(`    ${err.message}`);
    if (err.stack) {
      const line = err.stack.split('\n').find((l) => l.includes('exporter.test.js'));
      if (line) console.error(`   ${line.trim()}`);
    }
  }
}

// ---------------------------------------------------------------------------
// Module-level mocking via require cache manipulation
// ---------------------------------------------------------------------------

// We need to intercept require() calls from exporter.js.
// Strategy: replace modules in require.cache before loading exporter.

const authModulePath = require.resolve('../auth');
const clientModulePath = require.resolve('../client');
const cacheModulePath = require.resolve('../cache');
const csvWriterModulePath = require.resolve('../csv-writer');
const endpointsModulePath = require.resolve('../endpoints');

// Keep real endpoints since we need getEndpointsByType
const realEndpoints = require('../endpoints');

// Store originals
const origModules = {};
for (const p of [authModulePath, clientModulePath, cacheModulePath, csvWriterModulePath]) {
  origModules[p] = require.cache[p];
}

/**
 * Build mock modules and load a fresh exporter instance.
 * @param {object} mockConfig
 * @returns {{ exporter, mocks }}
 */
function buildExporter(mockConfig = {}) {
  const {
    loadTokensOk = true,
    loginOk = true,
    loginError = 'bad credentials',
    displayName = 'testuser',
    displayNameOk = true,
    // Map of endpointName -> data (or function(params) -> data)
    endpointData = {},
    // Cache state: map of `${date}:${endpoint}` -> data
    dailyCacheData = {},
    // Cache state: map of `${activityId}:${dataType}` -> data
    activityCacheData = {},
    cacheInitFails = false,
    csvGenerateFails = false,
  } = mockConfig;

  // Track calls
  const calls = {
    authLogin: [],
    authLoadTokens: [],
    clientSafe: [],
    clientFetch: [],
    clientGetDisplayName: [],
    cacheGetDaily: [],
    cacheSetDaily: [],
    cacheGetActivity: [],
    cacheSetActivity: [],
    cacheIsWithinRefreshWindow: [],
    cacheSave: [],
    csvGenerate: [],
    fsWriteFileSync: [],
    fsMkdirSync: [],
  };

  // -- Mock auth module
  const mockAuthModule = {
    createClient: (dataDir) => ({
      loadTokens: async () => {
        calls.authLoadTokens.push({ dataDir });
        if (loadTokensOk) return { ok: true, tokens: {} };
        return { ok: false, error: 'No token file found' };
      },
      login: async (email, password) => {
        calls.authLogin.push({ email, password });
        if (loginOk) return { ok: true };
        return { ok: false, error: loginError };
      },
      getAccessToken: async () => 'mock-token',
      clearTokens: () => ({ ok: true }),
    }),
  };

  // -- Mock client module
  const mockClientModule = {
    createGarminClient: (auth, opts) => ({
      safe: async (endpointName, params) => {
        calls.clientSafe.push({ endpointName, params });
        const handler = endpointData[endpointName];
        if (typeof handler === 'function') return handler(params);
        if (handler !== undefined) return handler;
        return { mock: endpointName };
      },
      fetch: async (endpointName, params) => {
        calls.clientFetch.push({ endpointName, params });
        return { ok: true, data: endpointData[endpointName] || {} };
      },
      getDisplayName: async () => {
        calls.clientGetDisplayName.push({});
        if (displayNameOk) return { ok: true, displayName };
        return { ok: false, error: 'no display name' };
      },
    }),
  };

  // -- Mock cache module
  // Track what dates are within the refresh window
  const _refreshWindowDates = new Set(mockConfig.refreshWindowDates || []);

  const mockCacheModule = {
    initCache: async (dbPath) => {
      if (cacheInitFails) throw new Error('Cache init boom');
      return {
        getCachedDaily: (date, epName) => {
          calls.cacheGetDaily.push({ date, epName });
          const key = `${date}:${epName}`;
          return dailyCacheData[key] !== undefined ? dailyCacheData[key] : null;
        },
        setCachedDaily: (date, epName, data) => {
          calls.cacheSetDaily.push({ date, epName, data });
        },
        getCachedActivity: (activityId, dataType) => {
          calls.cacheGetActivity.push({ activityId, dataType });
          const key = `${activityId}:${dataType}`;
          return activityCacheData[key] !== undefined ? activityCacheData[key] : null;
        },
        setCachedActivity: (activityId, dataType, data) => {
          calls.cacheSetActivity.push({ activityId, dataType, data });
        },
        isWithinRefreshWindow: (date, refreshDays) => {
          calls.cacheIsWithinRefreshWindow.push({ date, refreshDays });
          return _refreshWindowDates.has(date);
        },
        save: () => {
          calls.cacheSave.push({});
        },
        close: () => {},
      };
    },
  };

  // -- Mock csv-writer module
  const mockCsvWriterModule = {
    generateCsvs: (jsonData, outputDir) => {
      calls.csvGenerate.push({ jsonData, outputDir });
      if (csvGenerateFails) throw new Error('CSV boom');
      return { csvDir: path.join(outputDir, 'csv_test'), written: [] };
    },
  };

  // -- Mock fs (we patch it in the exporter module after loading)
  // Actually, we need to intercept fs calls. We'll do this by patching fs
  // in the exporter module's scope. Since we can't easily do that with require
  // cache, we'll instead override at the module level.

  // Replace modules in require cache
  require.cache[authModulePath] = {
    id: authModulePath,
    filename: authModulePath,
    loaded: true,
    exports: mockAuthModule,
  };
  require.cache[clientModulePath] = {
    id: clientModulePath,
    filename: clientModulePath,
    loaded: true,
    exports: mockClientModule,
  };
  require.cache[cacheModulePath] = {
    id: cacheModulePath,
    filename: cacheModulePath,
    loaded: true,
    exports: mockCacheModule,
  };
  require.cache[csvWriterModulePath] = {
    id: csvWriterModulePath,
    filename: csvWriterModulePath,
    loaded: true,
    exports: mockCsvWriterModule,
  };

  // Force fresh load of exporter
  const exporterPath = require.resolve('../exporter');
  delete require.cache[exporterPath];
  const exporter = require('../exporter');

  return { exporter, calls };
}

/**
 * Restore original modules after tests.
 */
function restoreModules() {
  for (const [p, mod] of Object.entries(origModules)) {
    if (mod) {
      require.cache[p] = mod;
    } else {
      delete require.cache[p];
    }
  }
}

// We also need to mock fs.writeFileSync and fs.mkdirSync in exporter.
// Since exporter uses `const fs = require('fs')` at the top, the simplest
// approach is to monkey-patch fs temporarily.
const fs = require('fs');
const origWriteFileSync = fs.writeFileSync;
const origMkdirSync = fs.mkdirSync;
let capturedWrites = [];
let capturedMkdirs = [];

function mockFs() {
  capturedWrites = [];
  capturedMkdirs = [];
  fs.writeFileSync = (filePath, content, opts) => {
    capturedWrites.push({ filePath, content, opts });
  };
  fs.mkdirSync = (dir, opts) => {
    capturedMkdirs.push({ dir, opts });
  };
}

function restoreFs() {
  fs.writeFileSync = origWriteFileSync;
  fs.mkdirSync = origMkdirSync;
}

// ---------------------------------------------------------------------------
// Default opts helper
// ---------------------------------------------------------------------------
function defaultOpts(overrides = {}) {
  return {
    email: 'test@example.com',
    password: 'secret',
    daysBack: 2,
    outputDir: '/tmp/garmin-test-output',
    refreshWindow: 3,
    dataDir: '/tmp/garmin-test-data',
    onProgress: overrides.onProgress || (() => {}),
    onLog: overrides.onLog || (() => {}),
    ...overrides,
  };
}

// ---------------------------------------------------------------------------
// Tests
// ---------------------------------------------------------------------------

async function runTests() {
  console.log('\nexporter.js tests\n');

  // ---- Unit tests for date helpers ----------------------------------------
  console.log('Date helpers:');

  await test('formatDate produces YYYY-MM-DD', async () => {
    const { formatDate } = buildExporter().exporter;
    restoreModules();
    assert.strictEqual(formatDate(new Date(2026, 3, 16)), '2026-04-16');
    assert.strictEqual(formatDate(new Date(2026, 0, 1)), '2026-01-01');
  });

  await test('formatDateTime produces YYYY-MM-DD HH:MM:SS', async () => {
    const { formatDateTime } = buildExporter().exporter;
    restoreModules();
    const d = new Date(2026, 3, 16, 9, 5, 3);
    assert.strictEqual(formatDateTime(d), '2026-04-16 09:05:03');
  });

  await test('generateDateRange produces dates most-recent-first', async () => {
    const { generateDateRange } = buildExporter().exporter;
    restoreModules();
    const dates = generateDateRange('2026-04-14', '2026-04-16');
    assert.deepStrictEqual(dates, ['2026-04-16', '2026-04-15', '2026-04-14']);
  });

  // ---- Integration tests --------------------------------------------------
  console.log('\nExport pipeline:');

  await test('happy path: full export writes JSON and calls CSV generator', async () => {
    const { exporter, calls } = buildExporter({
      endpointData: {
        activities_by_date: [],
        goals: [],
      },
    });
    mockFs();
    try {
      const result = await exporter.exportHealth(defaultOpts({ daysBack: 1 }));
      assert.strictEqual(result.ok, true);
      assert.ok(result.jsonPath.includes('garmin_health_'));
      assert.ok(result.csvDir.includes('csv_'));
      // JSON was written
      assert.strictEqual(capturedWrites.length, 1);
      const written = JSON.parse(capturedWrites[0].content);
      assert.ok(written.export_date);
      assert.ok(written.date_range);
      assert.ok(written.daily);
      assert.ok(written.aggregated);
      assert.ok(Array.isArray(written.activities));
      assert.ok(Array.isArray(written.goals));
      // CSV generator was called
      assert.strictEqual(calls.csvGenerate.length, 1);
    } finally {
      restoreFs();
      restoreModules();
    }
  });

  await test('happy path: cached daily data is skipped when outside refresh window', async () => {
    const today = new Date();
    const todayStr = `${today.getFullYear()}-${String(today.getMonth() + 1).padStart(2, '0')}-${String(today.getDate()).padStart(2, '0')}`;

    // Pre-populate cache for every daily endpoint for today's date
    const dailyEps = realEndpoints.getEndpointsByType('daily');
    const dailyCache = {};
    for (const ep of dailyEps) {
      dailyCache[`${todayStr}:${ep.name}`] = { cached: true, ep: ep.name };
    }

    const { exporter, calls } = buildExporter({
      dailyCacheData: dailyCache,
      refreshWindowDates: [],  // today is NOT in refresh window (simulating old data)
      endpointData: {
        activities_by_date: [],
        goals: [],
      },
    });
    mockFs();
    try {
      await exporter.exportHealth(defaultOpts({ daysBack: 1 }));
      // All daily endpoints for today should have been fetched from cache
      // (no client.safe calls for daily endpoints)
      const dailyFetches = calls.clientSafe.filter((c) => {
        return dailyEps.some((ep) => ep.name === c.endpointName);
      });
      assert.strictEqual(dailyFetches.length, 0, 'No daily endpoints should be fetched when all cached');
      assert.ok(calls.cacheGetDaily.length > 0, 'Cache should have been consulted');
    } finally {
      restoreFs();
      restoreModules();
    }
  });

  await test('happy path: data within refresh window is re-fetched even if cached', async () => {
    const today = new Date();
    const todayStr = `${today.getFullYear()}-${String(today.getMonth() + 1).padStart(2, '0')}-${String(today.getDate()).padStart(2, '0')}`;

    const dailyEps = realEndpoints.getEndpointsByType('daily');
    const dailyCache = {};
    for (const ep of dailyEps) {
      dailyCache[`${todayStr}:${ep.name}`] = { cached: true, ep: ep.name };
    }

    const { exporter, calls } = buildExporter({
      dailyCacheData: dailyCache,
      refreshWindowDates: [todayStr],  // today IS in refresh window
      endpointData: {
        activities_by_date: [],
        goals: [],
      },
    });
    mockFs();
    try {
      await exporter.exportHealth(defaultOpts({ daysBack: 1 }));
      // Daily endpoints should have been fetched via client.safe (not from cache)
      const dailyFetches = calls.clientSafe.filter((c) => {
        return dailyEps.some((ep) => ep.name === c.endpointName);
      });
      assert.strictEqual(dailyFetches.length, dailyEps.length,
        `All ${dailyEps.length} daily endpoints should be re-fetched`);
    } finally {
      restoreFs();
      restoreModules();
    }
  });

  await test('happy path: aggregated endpoints are always fetched (never cached)', async () => {
    const aggEps = realEndpoints.getEndpointsByType('aggregated');
    const { exporter, calls } = buildExporter({
      endpointData: {
        activities_by_date: [],
        goals: [],
      },
    });
    mockFs();
    try {
      await exporter.exportHealth(defaultOpts({ daysBack: 1 }));
      const aggFetches = calls.clientSafe.filter((c) => {
        return aggEps.some((ep) => ep.name === c.endpointName);
      });
      assert.strictEqual(aggFetches.length, aggEps.length,
        `All ${aggEps.length} aggregated endpoints should be fetched`);
    } finally {
      restoreFs();
      restoreModules();
    }
  });

  await test('happy path: progress callbacks fire with correct current/total/phase', async () => {
    const progressCalls = [];
    const { exporter } = buildExporter({
      endpointData: {
        activities_by_date: [
          { activityId: '100', activityName: 'Run' },
        ],
        goals: [],
      },
    });
    mockFs();
    try {
      await exporter.exportHealth(defaultOpts({
        daysBack: 1,
        onProgress: (p) => progressCalls.push(p),
      }));

      // Check daily phase
      const dailyProgress = progressCalls.filter((p) => p.phase === 'daily');
      assert.ok(dailyProgress.length > 0, 'Should have daily progress');
      assert.strictEqual(dailyProgress[dailyProgress.length - 1].current, dailyProgress[dailyProgress.length - 1].total);

      // Check aggregated phase
      const aggProgress = progressCalls.filter((p) => p.phase === 'aggregated');
      assert.ok(aggProgress.length > 0, 'Should have aggregated progress');

      // Check activities phase
      const actProgress = progressCalls.filter((p) => p.phase === 'activities');
      assert.ok(actProgress.length > 0, 'Should have activities progress');
      assert.strictEqual(actProgress[0].current, 1);
      assert.strictEqual(actProgress[0].total, 1);

      // Check csv phase
      const csvProgress = progressCalls.filter((p) => p.phase === 'csv');
      assert.strictEqual(csvProgress.length, 1);
    } finally {
      restoreFs();
      restoreModules();
    }
  });

  await test('happy path: output JSON has correct schema', async () => {
    const { exporter } = buildExporter({
      endpointData: {
        activities_by_date: [],
        goals: [{ goalId: 1 }],
      },
    });
    mockFs();
    try {
      await exporter.exportHealth(defaultOpts({ daysBack: 1 }));
      const written = JSON.parse(capturedWrites[0].content);

      // Top-level keys
      assert.ok('export_date' in written, 'missing export_date');
      assert.ok('date_range' in written, 'missing date_range');
      assert.ok('daily' in written, 'missing daily');
      assert.ok('aggregated' in written, 'missing aggregated');
      assert.ok('activities' in written, 'missing activities');
      assert.ok('goals' in written, 'missing goals');

      // date_range structure
      assert.ok('start' in written.date_range);
      assert.ok('end' in written.date_range);
      assert.ok('days' in written.date_range);
      assert.strictEqual(written.date_range.days, 1);

      // export_date is YYYY-MM-DD
      assert.ok(/^\d{4}-\d{2}-\d{2}$/.test(written.export_date), 'export_date should be YYYY-MM-DD');

      // daily is object with date keys
      assert.strictEqual(typeof written.daily, 'object');
      assert.ok(!Array.isArray(written.daily));

      // activities is array
      assert.ok(Array.isArray(written.activities));

      // goals is array
      assert.ok(Array.isArray(written.goals));
      assert.strictEqual(written.goals.length, 1);
    } finally {
      restoreFs();
      restoreModules();
    }
  });

  await test('error path: auth failure returns {ok: false, error}', async () => {
    const { exporter } = buildExporter({
      loadTokensOk: false,
      loginOk: false,
      loginError: 'Invalid credentials',
    });
    mockFs();
    try {
      const result = await exporter.exportHealth(defaultOpts());
      assert.strictEqual(result.ok, false);
      assert.ok(result.error.includes('Invalid credentials'));
    } finally {
      restoreFs();
      restoreModules();
    }
  });

  await test('error path: single endpoint failure skips metric, logs warning, continues', async () => {
    const logCalls = [];
    const { exporter, calls } = buildExporter({
      endpointData: {
        // Make stats return null (simulates safe() returning null on failure)
        stats: null,
        activities_by_date: [],
        goals: [],
      },
    });
    mockFs();
    try {
      const result = await exporter.exportHealth(defaultOpts({
        daysBack: 1,
        onLog: (l) => logCalls.push(l),
      }));
      // Export should succeed overall
      assert.strictEqual(result.ok, true);
      // The daily data should contain null for stats
      const written = JSON.parse(capturedWrites[0].content);
      const dates = Object.keys(written.daily);
      assert.ok(dates.length > 0);
      assert.strictEqual(written.daily[dates[0]].stats, null);
    } finally {
      restoreFs();
      restoreModules();
    }
  });

  await test('happy path: activity pagination fetches multiple pages', async () => {
    let pageCallCount = 0;
    const { exporter, calls } = buildExporter({
      endpointData: {
        activities_by_date: (params) => {
          pageCallCount++;
          if (params.start === 0) {
            // First page: return 20 items (full page triggers next fetch)
            return Array.from({ length: 20 }, (_, i) => ({
              activityId: String(i + 1),
              activityName: `Activity ${i + 1}`,
            }));
          }
          if (params.start === 20) {
            // Second page: return 5 items (partial = last page)
            return Array.from({ length: 5 }, (_, i) => ({
              activityId: String(21 + i),
              activityName: `Activity ${21 + i}`,
            }));
          }
          return [];
        },
        goals: [],
      },
    });
    mockFs();
    try {
      const result = await exporter.exportHealth(defaultOpts({ daysBack: 1 }));
      assert.strictEqual(result.ok, true);
      // Should have fetched 2 pages
      assert.strictEqual(pageCallCount, 2);
      // Total activities should be 25
      const written = JSON.parse(capturedWrites[0].content);
      assert.strictEqual(written.activities.length, 25);
    } finally {
      restoreFs();
      restoreModules();
    }
  });

  await test('happy path: cached activity details are reused', async () => {
    const { exporter, calls } = buildExporter({
      endpointData: {
        activities_by_date: [
          { activityId: '42', activityName: 'Cached Run' },
        ],
        goals: [],
      },
      activityCacheData: {
        '42:splits': { cachedSplits: true },
        '42:typed_splits': { cachedTypedSplits: true },
      },
    });
    mockFs();
    try {
      const result = await exporter.exportHealth(defaultOpts({ daysBack: 1 }));
      assert.strictEqual(result.ok, true);

      // activity_splits and activity_typed_splits should NOT have been called via client.safe
      const splitFetches = calls.clientSafe.filter(
        (c) => c.endpointName === 'activity_splits' || c.endpointName === 'activity_typed_splits'
      );
      assert.strictEqual(splitFetches.length, 0, 'Should not fetch splits when cached');

      // But the data should be in the output
      const written = JSON.parse(capturedWrites[0].content);
      assert.deepStrictEqual(written.activities[0].splits, { cachedSplits: true });
      assert.deepStrictEqual(written.activities[0].typed_splits, { cachedTypedSplits: true });
    } finally {
      restoreFs();
      restoreModules();
    }
  });

  await test('fetchDailyStepsChunked splits 60-day window into 28-day chunks', async () => {
    const { exporter } = buildExporter();
    restoreModules();
    const { fetchDailyStepsChunked } = exporter;

    const calls = [];
    const client = {
      authFailed: false,
      safe: async (name, params) => {
        calls.push({ name, ...params });
        // Fake: return one entry per chunk
        return [{ startDate: params.startDate, endDate: params.endDate }];
      },
    };

    const result = await fetchDailyStepsChunked(client, '2026-01-01', '2026-03-01');
    // 60 days / 28-day chunks = 3 chunks (28 + 28 + rest)
    assert.strictEqual(calls.length, 3, `expected 3 chunks, got ${calls.length}`);
    assert.strictEqual(calls[0].startDate, '2026-01-01');
    assert.strictEqual(calls[0].endDate, '2026-01-28');
    assert.strictEqual(calls[1].startDate, '2026-01-29');
    assert.strictEqual(calls[1].endDate, '2026-02-25');
    assert.strictEqual(calls[2].startDate, '2026-02-26');
    assert.strictEqual(calls[2].endDate, '2026-03-01');
    assert.strictEqual(result.length, 3);
  });

  await test('fetchDailyStepsChunked short-circuits when authFailed becomes true', async () => {
    const { exporter } = buildExporter();
    restoreModules();
    const { fetchDailyStepsChunked } = exporter;

    const calls = [];
    const client = {
      authFailed: false,
      safe: async (name, params) => {
        calls.push(params);
        client.authFailed = true; // auth fails on first call
        return [];
      },
    };

    await fetchDailyStepsChunked(client, '2026-01-01', '2026-03-01');
    assert.strictEqual(calls.length, 1, 'should stop after first chunk when authFailed');
  });

  await test('periodic save: cache.save() fires every 10 activities during export', async () => {
    const activities = Array.from({ length: 25 }, (_, i) => ({
      activityId: String(i + 1),
      activityName: `Act ${i + 1}`,
    }));
    const { exporter, calls } = buildExporter({
      endpointData: {
        activities_by_date: (params) => {
          if (params.start === 0) return activities.slice(0, 20);
          if (params.start === 20) return activities.slice(20);
          return [];
        },
        goals: [],
      },
    });
    mockFs();
    try {
      const result = await exporter.exportHealth(defaultOpts({ daysBack: 1 }));
      assert.strictEqual(result.ok, true);
      // >=2 saves during activities (at 10 and 20) + 1 final save = at least 3
      assert.ok(calls.cacheSave.length >= 3,
        `expected >=3 cache.save() calls, got ${calls.cacheSave.length}`);
    } finally {
      restoreFs();
      restoreModules();
    }
  });

  await test('happy path: cache init failure does not break export', async () => {
    const { exporter } = buildExporter({
      cacheInitFails: true,
      endpointData: {
        activities_by_date: [],
        goals: [],
      },
    });
    mockFs();
    try {
      const result = await exporter.exportHealth(defaultOpts({ daysBack: 1 }));
      assert.strictEqual(result.ok, true, 'Export should succeed even without cache');
    } finally {
      restoreFs();
      restoreModules();
    }
  });

  // ---- Summary ------------------------------------------------------------
  console.log(`\n  ${passed} passed, ${failed} failed\n`);
  if (failed > 0) process.exit(1);
}

runTests().catch((err) => {
  console.error('Test runner error:', err);
  process.exit(1);
});
