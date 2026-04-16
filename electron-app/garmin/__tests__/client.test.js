#!/usr/bin/env node
/**
 * Tests for garmin/client.js and garmin/endpoints.js
 *
 * Plain Node.js test runner using assert — no frameworks needed.
 * Run: node electron-app/garmin/__tests__/client.test.js
 */

const assert = require('assert');
const { createGarminClient, GarminClient, backoffDelay } = require('../client');
const {
  getEndpoint,
  getEndpointNames,
  getEndpointsByType,
  dailyEndpoints,
  aggregatedEndpoints,
  activityEndpoints,
  listEndpoints,
  allEndpoints,
} = require('../endpoints');
const { API_BASE } = require('../auth');

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
      const line = err.stack.split('\n').find((l) => l.includes('client.test.js'));
      if (line) console.error(`   ${line.trim()}`);
    }
  }
}

/** Create a mock auth instance. */
function mockAuth(overrides = {}) {
  return {
    getAccessToken: overrides.getAccessToken || (async () => 'mock-token-abc123'),
    clearTokens: overrides.clearTokens || (() => ({ ok: true })),
    loadTokens: overrides.loadTokens || (async () => ({ ok: true })),
  };
}

/**
 * Create a mock fetch that returns predefined responses.
 * @param {Array<{status, body, ok?}>} responses  Queue of responses (consumed in order)
 * @returns {{ fetch: function, calls: Array }}
 */
function mockFetch(responses) {
  const queue = [...responses];
  const calls = [];
  const fn = async (url, opts) => {
    calls.push({ url, opts });
    const resp = queue.shift() || { status: 200, body: {}, ok: true };
    return {
      ok: resp.ok !== undefined ? resp.ok : resp.status >= 200 && resp.status < 300,
      status: resp.status,
      json: async () => resp.body,
      text: async () => (typeof resp.body === 'string' ? resp.body : JSON.stringify(resp.body)),
      headers: new Map(),
    };
  };
  return { fetch: fn, calls };
}

// ---------------------------------------------------------------------------
// Endpoint definition tests
// ---------------------------------------------------------------------------

async function runEndpointTests() {
  console.log('\nEndpoint definitions:');

  await test('all endpoint names are defined and non-empty', () => {
    const names = getEndpointNames();
    assert.ok(names.length >= 36, `Expected >= 36 endpoints, got ${names.length}`);
    for (const name of names) {
      assert.ok(typeof name === 'string' && name.length > 0, `Invalid name: ${name}`);
    }
  });

  await test('no duplicate endpoint names', () => {
    const names = getEndpointNames();
    const unique = new Set(names);
    assert.strictEqual(names.length, unique.size, `Duplicate names found: ${names.filter((n, i) => names.indexOf(n) !== i)}`);
  });

  await test('daily endpoints have correct count', () => {
    const daily = getEndpointsByType('daily');
    assert.ok(daily.length >= 22, `Expected >= 22 daily endpoints, got ${daily.length}`);
  });

  await test('aggregated endpoints have correct count', () => {
    const agg = getEndpointsByType('aggregated');
    assert.ok(agg.length >= 12, `Expected >= 12 aggregated endpoints, got ${agg.length}`);
  });

  await test('activity endpoints have correct count', () => {
    const act = getEndpointsByType('activity');
    assert.ok(act.length >= 3, `Expected >= 3 activity endpoints, got ${act.length}`);
  });

  await test('list endpoints have correct count', () => {
    const list = getEndpointsByType('list');
    assert.ok(list.length >= 2, `Expected >= 2 list endpoints, got ${list.length}`);
  });

  await test('getEndpoint returns correct endpoint by name', () => {
    const ep = getEndpoint('stats');
    assert.ok(ep, 'stats endpoint not found');
    assert.strictEqual(ep.name, 'stats');
    assert.strictEqual(ep.type, 'daily');
    assert.strictEqual(typeof ep.buildUrl, 'function');
  });

  await test('getEndpoint returns undefined for unknown name', () => {
    const ep = getEndpoint('nonexistent_endpoint_xyz');
    assert.strictEqual(ep, undefined);
  });

  await test('all daily endpoints buildUrl with date and displayName', () => {
    const params = { date: '2026-04-15', displayName: 'testuser' };
    for (const ep of dailyEndpoints) {
      const url = ep.buildUrl(params);
      assert.ok(url.startsWith(API_BASE), `${ep.name}: URL does not start with API_BASE`);
      assert.ok(url.includes('2026-04-15'), `${ep.name}: URL does not contain date`);
    }
  });

  await test('daily endpoints that need displayName include it in URL', () => {
    const params = { date: '2026-04-15', displayName: 'testuser123' };
    const needsDisplayName = ['stats', 'user_summary', 'heart_rates', 'rhr', 'sleep', 'steps'];
    for (const name of needsDisplayName) {
      const ep = getEndpoint(name);
      const url = ep.buildUrl(params);
      assert.ok(url.includes('testuser123'), `${name}: URL should contain displayName`);
    }
  });

  await test('aggregated endpoints buildUrl with startDate and endDate', () => {
    const params = { startDate: '2026-04-01', endDate: '2026-04-15', displayName: 'testuser' };
    for (const ep of aggregatedEndpoints) {
      const url = ep.buildUrl(params);
      assert.ok(url.startsWith(API_BASE), `${ep.name}: URL does not start with API_BASE`);
    }
  });

  await test('activity endpoints buildUrl with activityId', () => {
    const params = { activityId: '12345678' };
    for (const ep of activityEndpoints) {
      const url = ep.buildUrl(params);
      assert.ok(url.startsWith(API_BASE), `${ep.name}: URL does not start with API_BASE`);
      assert.ok(url.includes('12345678'), `${ep.name}: URL does not contain activityId`);
    }
  });

  await test('activity_details includes maxChartSize', () => {
    const ep = getEndpoint('activity_details');
    const url = ep.buildUrl({ activityId: '99999' });
    assert.ok(url.includes('maxChartSize=2000'), 'activity_details URL missing maxChartSize');
  });

  await test('activities_by_date supports pagination params', () => {
    const ep = getEndpoint('activities_by_date');
    const url = ep.buildUrl({ startDate: '2026-04-01', endDate: '2026-04-15', start: 20, limit: 20 });
    assert.ok(url.includes('start=20'), 'Missing start param');
    assert.ok(url.includes('limit=20'), 'Missing limit param');
  });

  await test('goals endpoint defaults to active status', () => {
    const ep = getEndpoint('goals');
    const url = ep.buildUrl({});
    assert.ok(url.includes('status=active'), 'goals URL missing status=active');
  });

  await test('social_profile endpoint exists for displayName lookup', () => {
    const ep = getEndpoint('social_profile');
    assert.ok(ep, 'social_profile endpoint not found');
    const url = ep.buildUrl();
    assert.ok(url.includes('socialProfile'), 'URL missing socialProfile path');
  });

  await test('specific URL patterns match garminconnect Python library', () => {
    // Spot-check critical URLs against known patterns
    const checks = [
      {
        name: 'stats',
        params: { date: '2026-04-15', displayName: 'user1' },
        contains: '/usersummary-service/usersummary/daily/user1?calendarDate=2026-04-15',
      },
      {
        name: 'heart_rates',
        params: { date: '2026-04-15', displayName: 'user1' },
        contains: '/wellness-service/wellness/dailyHeartRate/user1?date=2026-04-15',
      },
      {
        name: 'hrv',
        params: { date: '2026-04-15' },
        contains: '/hrv-service/hrv/2026-04-15',
      },
      {
        name: 'sleep',
        params: { date: '2026-04-15', displayName: 'user1' },
        contains: '/wellness-service/wellness/dailySleepData/user1?date=2026-04-15',
      },
      {
        name: 'body_battery',
        params: { date: '2026-04-15' },
        contains: '/wellness-service/wellness/bodyBattery/reports/daily',
      },
      {
        name: 'blood_pressure',
        params: { startDate: '2026-04-01', endDate: '2026-04-15' },
        contains: '/bloodpressure-service/bloodpressure/range/2026-04-01/2026-04-15',
      },
      {
        name: 'lactate_threshold',
        params: {},
        contains: '/biometric-service/biometric/latestLactateThreshold',
      },
      {
        name: 'cycling_ftp',
        params: {},
        contains: '/biometric-service/biometric/latestFunctionalThresholdPower/CYCLING',
      },
      {
        name: 'activity_splits',
        params: { activityId: '555' },
        contains: '/activity-service/activity/555/splits',
      },
    ];

    for (const { name, params, contains } of checks) {
      const ep = getEndpoint(name);
      assert.ok(ep, `Endpoint ${name} not found`);
      const url = ep.buildUrl(params);
      assert.ok(url.includes(contains), `${name}: URL ${url} does not contain expected pattern: ${contains}`);
    }
  });
}

// ---------------------------------------------------------------------------
// Client tests
// ---------------------------------------------------------------------------

async function runClientTests() {
  console.log('\nGarminClient:');

  await test('happy path: fetch returns parsed JSON', async () => {
    const { fetch, calls } = mockFetch([
      { status: 200, body: { totalSteps: 8000, calendarDate: '2026-04-15' } },
    ]);
    const client = createGarminClient(mockAuth(), { fetch, delay: 0 });
    const result = await client.fetch('stats', { date: '2026-04-15', displayName: 'user1' });

    assert.strictEqual(result.ok, true);
    assert.strictEqual(result.data.totalSteps, 8000);
    assert.strictEqual(calls.length, 1);
    assert.ok(calls[0].url.includes('usersummary-service'));
    assert.ok(calls[0].opts.headers.Authorization.startsWith('Bearer '));
    assert.strictEqual(calls[0].opts.headers['User-Agent'], 'com.garmin.android.apps.connectmobile');
  });

  await test('happy path: inter-request delay is respected', async () => {
    const { fetch } = mockFetch([
      { status: 200, body: { a: 1 } },
      { status: 200, body: { b: 2 } },
    ]);
    const delay = 100;
    const client = createGarminClient(mockAuth(), { fetch, delay, log: () => {} });

    const t0 = Date.now();
    await client.fetch('stats', { date: '2026-04-15', displayName: 'u' });
    await client.fetch('hrv', { date: '2026-04-15' });
    const elapsed = Date.now() - t0;

    // Second call should have waited at least `delay` ms after the first
    assert.ok(elapsed >= delay - 10, `Expected >= ${delay}ms between calls, got ${elapsed}ms`);
  });

  await test('error path: 429 triggers backoff and retries', async () => {
    const { fetch, calls } = mockFetch([
      { status: 429, body: 'Too Many Requests' },
      { status: 429, body: 'Too Many Requests' },
      { status: 200, body: { recovered: true } },
    ]);
    const logs = [];
    const client = createGarminClient(mockAuth(), {
      fetch,
      delay: 0,
      maxRetries: 5,
      log: (msg) => logs.push(msg),
      sleep: async () => {},
    });

    const result = await client.fetch('stats', { date: '2026-04-15', displayName: 'u' });

    assert.strictEqual(result.ok, true);
    assert.strictEqual(result.data.recovered, true);
    assert.strictEqual(calls.length, 3); // 2 retries + 1 success
    assert.ok(logs.some((l) => l.includes('429')), 'Should log 429 retry message');
  });

  await test('error path: 429 exhausts all retries', async () => {
    // Create enough 429 responses to exhaust maxRetries + 1
    const responses = Array.from({ length: 7 }, () => ({ status: 429, body: 'Too Many Requests' }));
    const { fetch } = mockFetch(responses);
    const client = createGarminClient(mockAuth(), {
      fetch,
      delay: 0,
      maxRetries: 5,
      log: () => {},
      sleep: async () => {},
    });

    const result = await client.fetch('stats', { date: '2026-04-15', displayName: 'u' });

    assert.strictEqual(result.ok, false);
    assert.ok(result.error.includes('429'), 'Error message should mention 429');
    assert.ok(result.error.includes('6 attempts'), `Error should mention attempt count: ${result.error}`);
  });

  await test('error path: 401 clears tokens and returns auth error immediately', async () => {
    const { fetch, calls } = mockFetch([
      { status: 401, body: 'Unauthorized' },
    ]);
    let tokenCleared = false;
    const auth = mockAuth({
      clearTokens: () => { tokenCleared = true; return { ok: true }; },
    });
    const client = createGarminClient(auth, { fetch, delay: 0, log: () => {} });

    const result = await client.fetch('stats', { date: '2026-04-15', displayName: 'u' });

    assert.strictEqual(result.ok, false);
    assert.ok(result.error.includes('401'), 'Error should mention 401');
    assert.ok(tokenCleared, 'Tokens should be cleared on 401');
    assert.strictEqual(calls.length, 1, 'Should NOT retry on 401');
  });

  await test('error path: 403 clears tokens and returns auth error immediately', async () => {
    const { fetch, calls } = mockFetch([
      { status: 403, body: 'Forbidden' },
    ]);
    let tokenCleared = false;
    const auth = mockAuth({
      clearTokens: () => { tokenCleared = true; return { ok: true }; },
    });
    const client = createGarminClient(auth, { fetch, delay: 0, log: () => {} });

    const result = await client.fetch('stats', { date: '2026-04-15', displayName: 'u' });

    assert.strictEqual(result.ok, false);
    assert.ok(tokenCleared, 'Tokens should be cleared on 403');
    assert.strictEqual(calls.length, 1, 'Should NOT retry on 403');
  });

  await test('error path: network error returns error without crashing', async () => {
    const fetch = async () => { throw new Error('ECONNREFUSED'); };
    const client = createGarminClient(mockAuth(), {
      fetch,
      delay: 0,
      maxRetries: 0,
      log: () => {},
    });

    const result = await client.fetch('stats', { date: '2026-04-15', displayName: 'u' });

    assert.strictEqual(result.ok, false);
    assert.ok(result.error.includes('ECONNREFUSED'), `Error should contain original message: ${result.error}`);
  });

  await test('error path: auth.getAccessToken() failure returns error', async () => {
    const auth = mockAuth({
      getAccessToken: async () => { throw new Error('Refresh token expired — login required'); },
    });
    const { fetch } = mockFetch([]);
    const client = createGarminClient(auth, { fetch, delay: 0 });

    const result = await client.fetch('stats', { date: '2026-04-15', displayName: 'u' });

    assert.strictEqual(result.ok, false);
    assert.ok(result.error.includes('Auth error'), `Expected auth error: ${result.error}`);
  });

  await test('error path: unknown endpoint name returns error', async () => {
    const { fetch } = mockFetch([]);
    const client = createGarminClient(mockAuth(), { fetch, delay: 0 });

    const result = await client.fetch('nonexistent_xyz', {});

    assert.strictEqual(result.ok, false);
    assert.ok(result.error.includes('Unknown endpoint'), result.error);
  });

  await test('happy path: safe() returns data on success', async () => {
    const { fetch } = mockFetch([
      { status: 200, body: { value: 42 } },
    ]);
    const client = createGarminClient(mockAuth(), { fetch, delay: 0 });

    const data = await client.safe('stats', { date: '2026-04-15', displayName: 'u' });

    assert.deepStrictEqual(data, { value: 42 });
  });

  await test('happy path: safe() returns null on error instead of throwing', async () => {
    const { fetch } = mockFetch([
      { status: 500, body: 'Internal Server Error' },
    ]);
    const logs = [];
    const client = createGarminClient(mockAuth(), {
      fetch,
      delay: 0,
      maxRetries: 0,
      log: (msg) => logs.push(msg),
    });

    const data = await client.safe('stats', { date: '2026-04-15', displayName: 'u' });

    assert.strictEqual(data, null);
    assert.ok(logs.some((l) => l.includes('[skip]')), 'Should log skip message');
  });

  await test('happy path: safe() returns null on unknown endpoint', async () => {
    const { fetch } = mockFetch([]);
    const logs = [];
    const client = createGarminClient(mockAuth(), {
      fetch,
      delay: 0,
      log: (msg) => logs.push(msg),
    });

    const data = await client.safe('bogus_endpoint', {});

    assert.strictEqual(data, null);
  });

  await test('happy path: getDisplayName fetches social profile', async () => {
    const { fetch } = mockFetch([
      { status: 200, body: { displayName: 'myuser123', userName: 'test@example.com' } },
    ]);
    const client = createGarminClient(mockAuth(), { fetch, delay: 0 });

    const result = await client.getDisplayName();

    assert.strictEqual(result.ok, true);
    assert.strictEqual(result.displayName, 'myuser123');
  });

  await test('error path: getDisplayName fails if profile has no displayName', async () => {
    const { fetch } = mockFetch([
      { status: 200, body: { userName: 'test@example.com' } },
    ]);
    const client = createGarminClient(mockAuth(), { fetch, delay: 0 });

    const result = await client.getDisplayName();

    assert.strictEqual(result.ok, false);
    assert.ok(result.error.includes('displayName'));
  });

  await test('happy path: fetchUrl works with raw URL', async () => {
    const { fetch, calls } = mockFetch([
      { status: 200, body: { raw: true } },
    ]);
    const client = createGarminClient(mockAuth(), { fetch, delay: 0 });

    const result = await client.fetchUrl('https://example.com/api/test');

    assert.strictEqual(result.ok, true);
    assert.strictEqual(result.data.raw, true);
    assert.strictEqual(calls[0].url, 'https://example.com/api/test');
  });
}

// ---------------------------------------------------------------------------
// Backoff calculation tests
// ---------------------------------------------------------------------------

async function runBackoffTests() {
  console.log('\nBackoff delay:');

  await test('backoff increases exponentially', () => {
    const origRandom = Math.random;
    Math.random = () => 0; // Zero jitter

    const d0 = backoffDelay(0); // 5000
    const d1 = backoffDelay(1); // 10000
    const d2 = backoffDelay(2); // 20000

    Math.random = origRandom;

    assert.strictEqual(d0, 5000);
    assert.strictEqual(d1, 10000);
    assert.strictEqual(d2, 20000);
  });

  await test('backoff caps at 60000ms', () => {
    const origRandom = Math.random;
    Math.random = () => 0;

    const d5 = backoffDelay(5); // 5000 * 32 = 160000 -> capped at 60000

    Math.random = origRandom;

    assert.strictEqual(d5, 60000);
  });

  await test('backoff includes jitter', () => {
    const origRandom = Math.random;
    Math.random = () => 0.5; // 500ms jitter

    const d0 = backoffDelay(0);

    Math.random = origRandom;

    assert.strictEqual(d0, 5500); // 5000 + 500
  });
}

// ---------------------------------------------------------------------------
// Run all tests
// ---------------------------------------------------------------------------

async function main() {
  console.log('Running client + endpoint tests...');

  await runEndpointTests();
  await runClientTests();
  await runBackoffTests();

  console.log(`\nResults: ${passed} passed, ${failed} failed`);
  if (failed > 0) process.exit(1);
}

main();
