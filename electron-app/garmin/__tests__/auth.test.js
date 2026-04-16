#!/usr/bin/env node
/**
 * Tests for garmin/auth.js
 *
 * Plain Node.js test runner using assert — no frameworks needed.
 * Run: node electron-app/garmin/__tests__/auth.test.js
 */

const assert = require('assert');
const fs = require('fs');
const path = require('path');
const os = require('os');
const querystring = require('querystring');

const { createClient, GarminAuth } = require('../auth');

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

let tmpDirs = [];

function makeTmpDir() {
  const dir = fs.mkdtempSync(path.join(os.tmpdir(), 'garmin-auth-test-'));
  tmpDirs.push(dir);
  return dir;
}

function cleanup() {
  for (const dir of tmpDirs) {
    try {
      fs.rmSync(dir, { recursive: true, force: true });
    } catch {}
  }
  tmpDirs = [];
}

/** Build a valid token data object with configurable expiry offsets. */
function makeTokenData(overrides = {}) {
  const now = Math.floor(Date.now() / 1000);
  return {
    oauth1: {
      oauth_token: 'test-oauth1-token',
      oauth_token_secret: 'test-oauth1-secret',
    },
    oauth2: {
      access_token: 'test-access-token-abc123',
      token_type: 'Bearer',
      refresh_token: 'test-refresh-token',
      expires_at: now + 86400, // +24h (valid)
      refresh_token_expires_at: now + 2592000, // +30d (valid)
      ...overrides,
    },
    consumer: {
      consumer_key: 'test-consumer-key',
      consumer_secret: 'test-consumer-secret',
    },
  };
}

/**
 * Create a mock fetch that responds to URLs in sequence or by pattern.
 * Each handler is { match, response, method? } where match is a string/regex,
 * method is an optional HTTP method filter, and response is { ok, status, body, headers }.
 */
function createMockFetch(handlers) {
  const calls = [];
  const fn = async (url, opts) => {
    calls.push({ url, opts });
    const reqMethod = (opts && opts.method) || 'GET';
    for (const h of handlers) {
      const urlMatches =
        typeof h.match === 'string' ? url.includes(h.match) : h.match.test(url);
      const methodMatches = !h.method || h.method === reqMethod;
      if (urlMatches && methodMatches) {
        const r = h.response;
        return {
          ok: r.ok !== undefined ? r.ok : true,
          status: r.status || 200,
          headers: {
            getSetCookie: () => r.cookies || [],
          },
          text: async () =>
            typeof r.body === 'string' ? r.body : JSON.stringify(r.body || ''),
          json: async () => (typeof r.body === 'object' ? r.body : JSON.parse(r.body)),
        };
      }
    }
    return {
      ok: false,
      status: 404,
      headers: { getSetCookie: () => [] },
      text: async () => 'no mock handler matched',
      json: async () => ({}),
    };
  };
  fn.calls = calls;
  return fn;
}

/**
 * Build a mock fetch that simulates the full SSO login flow (Steps 1-4).
 */
function createLoginMockFetch() {
  return createMockFetch([
    // Step 1: OAuth consumer
    {
      match: 'oauth_consumer.json',
      response: {
        body: { consumer_key: 'ck_test', consumer_secret: 'cs_test' },
      },
    },
    // Step 2a: SSO embed
    {
      match: '/sso/embed',
      response: {
        body: '<html>embed</html>',
        cookies: ['GARMIN-SSO-GUID=abc123; Path=/'],
      },
    },
    // Step 2b: GET signin (CSRF)
    {
      match: /\/sso\/signin/,
      method: 'GET',
      response: {
        body: '<input name="_csrf" value="csrf-token-xyz">',
        cookies: ['GARMIN-SSO-CUST-GUID=def456; Path=/'],
      },
    },
    // Step 2c: POST signin (ticket)
    {
      match: /\/sso\/signin/,
      method: 'POST',
      response: {
        body: '<html>ticket=ST-12345-abcdef"</html>',
        cookies: ['CASTGC=TGT-abc; Path=/'],
      },
    },
    // Step 3: OAuth1 preauthorized
    {
      match: 'preauthorized',
      response: {
        body: 'oauth_token=ot_test&oauth_token_secret=ots_test',
      },
    },
    // Step 4: OAuth1 -> OAuth2 exchange
    {
      match: 'exchange/user/2.0',
      response: {
        body: {
          access_token: 'bearer-token-fresh',
          token_type: 'Bearer',
          refresh_token: 'refresh-token-fresh',
          expires_in: 95760,
          refresh_token_expires_in: 2592000,
        },
      },
    },
  ]);
}

// ---------------------------------------------------------------------------
// Test runner
// ---------------------------------------------------------------------------

const tests = [];
function test(name, fn) {
  tests.push({ name, fn });
}

async function run() {
  let passed = 0;
  let failed = 0;

  for (const t of tests) {
    try {
      await t.fn();
      console.log(`  PASS  ${t.name}`);
      passed++;
    } catch (err) {
      console.log(`  FAIL  ${t.name}`);
      console.log(`        ${err.message}`);
      if (err.stack) {
        const lines = err.stack.split('\n').slice(1, 3);
        for (const l of lines) console.log(`        ${l.trim()}`);
      }
      failed++;
    }
  }

  cleanup();
  console.log(`\n${passed + failed} tests: ${passed} passed, ${failed} failed`);
  process.exit(failed > 0 ? 1 : 0);
}

// ---------------------------------------------------------------------------
// Tests
// ---------------------------------------------------------------------------

test('createClient returns a GarminAuth instance', () => {
  const dir = makeTmpDir();
  const client = createClient(dir);
  assert.ok(client instanceof GarminAuth);
  assert.strictEqual(client.tokenPath, path.join(dir, 'tokens.json'));
});

test('login stores token file at expected path with mode 0600', async () => {
  const dir = makeTmpDir();
  const mockFetch = createLoginMockFetch();
  const client = createClient(dir, { fetch: mockFetch });

  const result = await client.login('user@test.com', 'pass123');
  assert.strictEqual(result.ok, true, `login failed: ${result.error}`);

  const tokenPath = path.join(dir, 'tokens.json');
  assert.ok(fs.existsSync(tokenPath), 'tokens.json should exist');

  // Check file permissions (mode 0600 = owner rw only)
  const stat = fs.statSync(tokenPath);
  const mode = stat.mode & 0o777;
  assert.strictEqual(mode, 0o600, `Expected mode 0600, got ${mode.toString(8)}`);

  // Check contents
  const data = JSON.parse(fs.readFileSync(tokenPath, 'utf8'));
  assert.ok(data.oauth1, 'should have oauth1');
  assert.ok(data.oauth2, 'should have oauth2');
  assert.ok(data.consumer, 'should have consumer');
  assert.strictEqual(data.oauth2.access_token, 'bearer-token-fresh');
  assert.ok(data.oauth2.expires_at > 0, 'should have expires_at');
  assert.ok(data.oauth2.refresh_token_expires_at > 0, 'should have refresh_token_expires_at');
});

test('login returns error on bad credentials (no ticket)', async () => {
  const dir = makeTmpDir();
  const mockFetch = createMockFetch([
    {
      match: 'oauth_consumer.json',
      response: { body: { consumer_key: 'ck', consumer_secret: 'cs' } },
    },
    {
      match: '/sso/embed',
      response: { body: '<html></html>', cookies: ['A=1; Path=/'] },
    },
    {
      match: /\/sso\/signin/,
      response: {
        body: '<input name="_csrf" value="tok"><html>Invalid credentials</html>',
        cookies: ['B=2; Path=/'],
      },
    },
  ]);
  const client = createClient(dir, { fetch: mockFetch });
  const result = await client.login('bad@test.com', 'wrong');
  assert.strictEqual(result.ok, false);
  assert.ok(result.error.includes('Ticket not found'), `Unexpected error: ${result.error}`);
});

test('loadTokens reads existing valid token file', async () => {
  const dir = makeTmpDir();
  const tokenPath = path.join(dir, 'tokens.json');
  const data = makeTokenData();
  fs.writeFileSync(tokenPath, JSON.stringify(data), { mode: 0o600 });

  const client = createClient(dir);
  const result = await client.loadTokens();
  assert.strictEqual(result.ok, true);
  assert.strictEqual(result.tokens.oauth2.access_token, 'test-access-token-abc123');
});

test('loadTokens with expired access_token but valid refresh triggers exchange', async () => {
  const dir = makeTmpDir();
  const now = Math.floor(Date.now() / 1000);
  const data = makeTokenData({
    expires_at: now - 100, // expired
    refresh_token_expires_at: now + 2592000, // still valid
  });
  fs.writeFileSync(path.join(dir, 'tokens.json'), JSON.stringify(data), { mode: 0o600 });

  // Mock the OAuth1->OAuth2 exchange call
  const mockFetch = createMockFetch([
    {
      match: 'exchange/user/2.0',
      response: {
        body: {
          access_token: 'refreshed-bearer-token',
          token_type: 'Bearer',
          refresh_token: 'refreshed-refresh-token',
          expires_in: 95760,
          refresh_token_expires_in: 2592000,
        },
      },
    },
  ]);

  const client = createClient(dir, { fetch: mockFetch });
  const result = await client.loadTokens();
  assert.strictEqual(result.ok, true, `loadTokens failed: ${result.error}`);
  assert.strictEqual(result.tokens.oauth2.access_token, 'refreshed-bearer-token');

  // Verify updated file on disk
  const onDisk = JSON.parse(fs.readFileSync(path.join(dir, 'tokens.json'), 'utf8'));
  assert.strictEqual(onDisk.oauth2.access_token, 'refreshed-bearer-token');

  // Verify the exchange endpoint was called
  assert.strictEqual(mockFetch.calls.length, 1);
  assert.ok(mockFetch.calls[0].url.includes('exchange/user/2.0'));
});

test('loadTokens with expired refresh_token returns error', async () => {
  const dir = makeTmpDir();
  const now = Math.floor(Date.now() / 1000);
  const data = makeTokenData({
    expires_at: now - 100, // expired
    refresh_token_expires_at: now - 50, // also expired
  });
  fs.writeFileSync(path.join(dir, 'tokens.json'), JSON.stringify(data), { mode: 0o600 });

  const client = createClient(dir);
  const result = await client.loadTokens();
  assert.strictEqual(result.ok, false);
  assert.ok(result.error.includes('Refresh token expired'), `Unexpected error: ${result.error}`);
});

test('clearTokens deletes the file', () => {
  const dir = makeTmpDir();
  const tokenPath = path.join(dir, 'tokens.json');
  fs.writeFileSync(tokenPath, '{}', { mode: 0o600 });
  assert.ok(fs.existsSync(tokenPath));

  const client = createClient(dir);
  const result = client.clearTokens();
  assert.strictEqual(result.ok, true);
  assert.ok(!fs.existsSync(tokenPath), 'token file should be deleted');
});

test('clearTokens on missing file returns ok', () => {
  const dir = makeTmpDir();
  const client = createClient(dir);
  const result = client.clearTokens();
  assert.strictEqual(result.ok, true);
});

test('getAccessToken returns the token string from a valid file', async () => {
  const dir = makeTmpDir();
  const data = makeTokenData();
  fs.writeFileSync(path.join(dir, 'tokens.json'), JSON.stringify(data), { mode: 0o600 });

  const client = createClient(dir);
  const token = await client.getAccessToken();
  assert.strictEqual(token, 'test-access-token-abc123');
});

test('getAccessToken throws when no token file exists', async () => {
  const dir = makeTmpDir();
  const client = createClient(dir);
  await assert.rejects(() => client.getAccessToken(), /login required/i);
});

test('corrupted token file handled gracefully', async () => {
  const dir = makeTmpDir();
  fs.writeFileSync(path.join(dir, 'tokens.json'), 'not valid json{{{', { mode: 0o600 });

  const client = createClient(dir);
  const result = await client.loadTokens();
  assert.strictEqual(result.ok, false);
  assert.ok(result.error.includes('corrupted'), `Unexpected error: ${result.error}`);
});

test('incomplete token file handled gracefully', async () => {
  const dir = makeTmpDir();
  // Valid JSON but missing required fields
  fs.writeFileSync(path.join(dir, 'tokens.json'), JSON.stringify({ oauth2: {} }), {
    mode: 0o600,
  });

  const client = createClient(dir);
  const result = await client.loadTokens();
  assert.strictEqual(result.ok, false);
  assert.ok(result.error.includes('incomplete'), `Unexpected error: ${result.error}`);
});

test('login creates data directory if it does not exist', async () => {
  const base = makeTmpDir();
  const nested = path.join(base, 'sub', 'dir');
  const mockFetch = createLoginMockFetch();
  const client = createClient(nested, { fetch: mockFetch });

  const result = await client.login('user@test.com', 'pass123');
  assert.strictEqual(result.ok, true, `login failed: ${result.error}`);
  assert.ok(fs.existsSync(path.join(nested, 'tokens.json')));
});

test('loadTokens caches tokens in memory after first read (no second disk read)', async () => {
  const dir = makeTmpDir();
  const tokenPath = path.join(dir, 'tokens.json');
  const data = makeTokenData();
  fs.writeFileSync(tokenPath, JSON.stringify(data), { mode: 0o600 });

  const client = createClient(dir);
  const first = await client.loadTokens();
  assert.strictEqual(first.ok, true);

  // Mutate the file on disk — if we re-read it, we'd see the change.
  fs.writeFileSync(tokenPath, 'CORRUPTED-JUNK', { mode: 0o600 });

  const second = await client.loadTokens();
  assert.strictEqual(second.ok, true, 'second loadTokens should hit the in-memory cache');
  assert.strictEqual(second.tokens.oauth2.access_token, 'test-access-token-abc123');
});

test('clearTokens invalidates in-memory cache', async () => {
  const dir = makeTmpDir();
  const tokenPath = path.join(dir, 'tokens.json');
  const data = makeTokenData();
  fs.writeFileSync(tokenPath, JSON.stringify(data), { mode: 0o600 });

  const client = createClient(dir);
  const first = await client.loadTokens();
  assert.strictEqual(first.ok, true);

  client.clearTokens();
  // File is gone and memory cache cleared — next load should fail with missing file
  const second = await client.loadTokens();
  assert.strictEqual(second.ok, false);
});

test('login flow makes correct sequence of fetch calls', async () => {
  const dir = makeTmpDir();
  const mockFetch = createLoginMockFetch();
  const client = createClient(dir, { fetch: mockFetch });

  await client.login('user@test.com', 'pass123');

  // Should be 6 calls: consumer, embed, signin GET, signin POST, preauthorized, exchange
  assert.strictEqual(mockFetch.calls.length, 6, `Expected 6 calls, got ${mockFetch.calls.length}`);
  assert.ok(mockFetch.calls[0].url.includes('oauth_consumer.json'));
  assert.ok(mockFetch.calls[1].url.includes('/sso/embed'));
  assert.ok(mockFetch.calls[2].url.includes('/sso/signin'));
  assert.ok(mockFetch.calls[3].url.includes('/sso/signin'));
  assert.strictEqual(mockFetch.calls[3].opts.method, 'POST');
  assert.ok(mockFetch.calls[4].url.includes('preauthorized'));
  assert.ok(mockFetch.calls[5].url.includes('exchange/user/2.0'));
});

// ---------------------------------------------------------------------------
// Run
// ---------------------------------------------------------------------------
console.log('garmin/auth.js tests\n');
run();
