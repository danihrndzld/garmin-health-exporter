'use strict';
// Integration tests: the real auth, client, cache, exporter and CSV writer talk HTTP
// to a local fake Garmin (helpers/fake-garmin.js). Test IDs follow the technique that
// chose each case: DT = decision table (login outcomes), ST = state transition (token
// lifecycle), EP / BVA = status classes and their limits, EG = error guessing,
// AC = acceptance criteria (CSV output equals the golden files).
const test = require('node:test');
const assert = require('node:assert');
const fs = require('fs');
const os = require('os');
const path = require('path');
const { startFakeGarmin, jwt } = require('./helpers/fake-garmin');
const { compareDirs } = require('../../scripts/compare-csv');

const FIXTURE = JSON.parse(fs.readFileSync(path.join(__dirname, 'fixtures', 'export-sample.json'), 'utf8'));
const GOLDEN = path.join(__dirname, 'golden');
const CREDS = { email: 'test@example.invalid', password: 'right-password' };

let fake;
let mods;

test.before(async () => {
  fake = await startFakeGarmin(FIXTURE);
  process.env.GARMIN_FAKE_SERVER = fake.base; // auth.js reads it on every request
  mods = {
    auth: require('../auth'),
    client: require('../client'),
    exporter: require('../exporter'),
  };
  // Guard: never let this suite talk to the real Garmin hosts.
  assert.ok(mods.auth.apiBase().startsWith('http://127.0.0.1:'), 'API base must be the fake server');
});
test.after(async () => { await fake.close(); delete process.env.GARMIN_FAKE_SERVER; });

function reset(overrides = {}) {
  Object.assign(fake.control, {
    password: 'right-password', mobileLogin: 'ok', diauth: 'ok', tokenTtl: 3600, dataStatus: {}, failOnce: {}, ...overrides,
  });
  fake.calls.length = 0;
}
const tmp = (p) => fs.mkdtempSync(path.join(os.tmpdir(), p));
const callsTo = (p) => fake.calls.filter((c) => c.path === p);
const tokensIn = (dir) => JSON.parse(fs.readFileSync(path.join(dir, 'tokens.json'), 'utf8'));

// ---------------------------------------------------------------------------
// Login decision table: (mobile login answer) x (diauth answer) -> outcome
// ---------------------------------------------------------------------------

test('DT-R1 mobile login and DI exchange succeed: DI tokens, native headers on API calls', async () => {
  reset();
  const dir = tmp('dt1-');
  const auth = mods.auth.createClient(dir);
  const res = await auth.login(CREDS.email, CREDS.password);
  assert.deepStrictEqual(res, { ok: true, method: 'di' });
  assert.ok(tokensIn(dir).di.access_token);
  assert.strictEqual(callsTo('/sso/signin').length, 0, 'no fallback to the widget login');
  const client = mods.client.createGarminClient(auth, { delay: 0, log: () => {} });
  const profile = await client.fetch('social_profile');
  assert.strictEqual(profile.ok, true);
  const apiCall = callsTo('/userprofile-service/socialProfile').at(-1);
  assert.strictEqual(apiCall.headers['x-garmin-client-platform'], 'Android');
  assert.strictEqual(apiCall.headers['user-agent'], 'GCM-Android-5.23');
});

test('DT-R2 wrong password stops the chain: no widget login, no retry', async () => {
  reset({ password: 'other' });
  const res = await mods.auth.createClient(tmp('dt2-')).login(CREDS.email, CREDS.password);
  assert.strictEqual(res.ok, false);
  assert.strictEqual(res.errorCode, 'BAD_PASSWORD');
  assert.strictEqual(callsTo('/mobile/api/login').length, 1);
  assert.strictEqual(callsTo('/sso/signin').length, 0);
});

test('DT-R3 MFA required stops the chain with a clear message', async () => {
  reset({ mobileLogin: 'mfa' });
  const res = await mods.auth.createClient(tmp('dt3-')).login(CREDS.email, CREDS.password);
  assert.strictEqual(res.errorCode, 'MFA_REQUIRED');
  assert.match(res.error, /two-step verification/);
  assert.strictEqual(callsTo('/sso/signin').length, 0);
});

test('DT-R4 HTTP 429 on the new login: one try of the legacy flow, which signs in', async () => {
  reset({ mobileLogin: '429' });
  const dir = tmp('dt4-');
  const res = await mods.auth.createClient(dir).login(CREDS.email, CREDS.password);
  assert.deepStrictEqual(res, { ok: true, method: 'legacy' });
  assert.strictEqual(callsTo('/mobile/api/login').length, 1);
  assert.strictEqual(callsTo('/sso/signin').filter((c) => c.method === 'POST').length, 1);
});

test('DT-R9 429 on both logins stops, starts a cooldown, and the next login sends nothing', async () => {
  reset({ mobileLogin: '429' });
  fake.control.widget429 = true;
  const dir = tmp('dt9-');
  try {
    const first = await mods.auth.createClient(dir).login(CREDS.email, CREDS.password);
    assert.strictEqual(first.errorCode, 'RATE_LIMITED');
    assert.ok(fs.existsSync(path.join(dir, 'sso-cooldown.json')));
    reset();
    const second = await mods.auth.createClient(dir).login(CREDS.email, CREDS.password);
    assert.strictEqual(second.errorCode, 'RATE_LIMITED');
    assert.strictEqual(fake.calls.length, 0, 'no request during the cooldown');
  } finally {
    fake.control.widget429 = false;
  }
});

test('DT-R5 new login blocked by Cloudflare (403): falls back to the legacy flow', async () => {
  reset({ mobileLogin: 'cloudflare' });
  const dir = tmp('dt5-');
  const res = await mods.auth.createClient(dir).login(CREDS.email, CREDS.password);
  assert.deepStrictEqual(res, { ok: true, method: 'legacy' });
  const t = tokensIn(dir);
  assert.ok(t.oauth1 && t.oauth2 && t.consumer && !t.di);
});

test('DT-R6 captcha on the new login: falls back to the legacy flow', async () => {
  reset({ mobileLogin: 'captcha' });
  const res = await mods.auth.createClient(tmp('dt6-')).login(CREDS.email, CREDS.password);
  assert.deepStrictEqual(res, { ok: true, method: 'legacy' });
});

test('DT-R7 DI exchange refused (400 for every client id): falls back to the legacy flow', async () => {
  reset({ diauth: 'fail' });
  const res = await mods.auth.createClient(tmp('dt7-')).login(CREDS.email, CREDS.password);
  assert.deepStrictEqual(res, { ok: true, method: 'legacy' });
  assert.strictEqual(callsTo('/di-oauth2-service/oauth/token').length, 4, 'all four client ids tried once');
});

test('DT-R8 DI exchange answers 429: stops without the legacy flow', async () => {
  reset({ diauth: '429' });
  const res = await mods.auth.createClient(tmp('dt8-')).login(CREDS.email, CREDS.password);
  assert.strictEqual(res.errorCode, 'RATE_LIMITED');
  assert.strictEqual(callsTo('/sso/signin').length, 0);
  assert.strictEqual(callsTo('/di-oauth2-service/oauth/token').length, 1);
});

// ---------------------------------------------------------------------------
// Token lifecycle (state transitions)
// ---------------------------------------------------------------------------

test('ST-T1 DI token within 15 minutes of expiry is refreshed before use', async () => {
  reset({ tokenTtl: 600 }); // issued tokens expire in 10 minutes: inside the margin
  const dir = tmp('st1-');
  const auth = mods.auth.createClient(dir);
  await auth.login(CREDS.email, CREDS.password);
  const before = tokensIn(dir).di.access_token;
  fake.calls.length = 0;
  const loaded = await mods.auth.createClient(dir).loadTokens();
  assert.strictEqual(loaded.ok, true);
  const refreshCall = callsTo('/di-oauth2-service/oauth/token')[0];
  assert.match(refreshCall.body, /grant_type=refresh_token/);
  assert.notStrictEqual(tokensIn(dir).di.access_token, before);
});

test('ST-T2 legacy token past its 30 day refresh expiry is renewed through OAuth1, not a new login', async () => {
  reset();
  const dir = tmp('st2-');
  const past = Math.floor(Date.now() / 1000) - 10;
  fs.writeFileSync(path.join(dir, 'tokens.json'), JSON.stringify({
    oauth1: { oauth_token: 'ot', oauth_token_secret: 'os' },
    oauth2: { access_token: 'old', expires_at: past, refresh_token_expires_at: past },
    consumer: { consumer_key: 'ck', consumer_secret: 'cs' },
  }));
  const loaded = await mods.auth.createClient(dir).loadTokens();
  assert.strictEqual(loaded.ok, true);
  assert.strictEqual(callsTo('/oauth-service/oauth/exchange/user/2.0').length, 1);
  assert.strictEqual(callsTo('/mobile/api/login').length, 0);
});

// ---------------------------------------------------------------------------
// Client status handling
// ---------------------------------------------------------------------------

async function signedInClient(opts = {}) {
  const dir = tmp('cl-');
  const auth = mods.auth.createClient(dir);
  await auth.login(CREDS.email, CREDS.password);
  const logs = [];
  const waits = [];
  const client = mods.client.createGarminClient(auth, {
    delay: 0, log: (m, extra) => logs.push({ m, extra }), sleep: async (ms) => { waits.push(ms); }, ...opts,
  });
  return { dir, auth, client, logs, waits };
}
const DAY = Object.keys(FIXTURE.daily).find((d) => FIXTURE.daily[d].hrv === null);

test('EP-01 HTTP 204 is success with no data, logged as [no data] and not as a warning', async () => {
  reset();
  const { client, logs } = await signedInClient();
  const data = await client.safe('hrv', { date: DAY });
  assert.strictEqual(data, null);
  assert.ok(logs.some((l) => /^\[no data\] hrv \(HTTP 204\)/.test(l.m) && !l.extra));
  assert.ok(!logs.some((l) => /\[skip\]/.test(l.m)));
});

test('EP-02 one 401 refreshes the token and retries; tokens are kept', async () => {
  reset();
  const { dir, client } = await signedInClient();
  fake.control.failOnce['/userprofile-service/socialProfile'] = 401;
  const res = await client.fetch('social_profile');
  assert.strictEqual(res.ok, true);
  assert.ok(fs.existsSync(path.join(dir, 'tokens.json')));
  assert.strictEqual(client.authFailed, false);
});

test('EP-03 401 again after a refresh clears the tokens and stops the run', async () => {
  reset();
  const { dir, client } = await signedInClient();
  fake.control.dataStatus['/userprofile-service/socialProfile'] = 401;
  const res = await client.fetch('social_profile');
  assert.strictEqual(res.errorCode, 'AUTH');
  assert.strictEqual(client.authFailed, true);
  assert.ok(!fs.existsSync(path.join(dir, 'tokens.json')));
});

test('EP-04 one 403 skips that endpoint only; tokens stay and the run goes on', async () => {
  reset();
  const { dir, client } = await signedInClient();
  fake.control.failOnce['/hrv-service/'] = 403;
  const res = await client.fetch('hrv', { date: DAY });
  assert.strictEqual(res.errorCode, 'FORBIDDEN');
  assert.strictEqual(client.authFailed, false);
  assert.ok(fs.existsSync(path.join(dir, 'tokens.json')));
  assert.strictEqual((await client.fetch('social_profile')).ok, true);
});

test('BVA-01 four 403 answers in a row do not stop the run', async () => {
  reset();
  const { client } = await signedInClient();
  fake.control.dataStatus['/hrv-service/'] = 403;
  for (let i = 0; i < 4; i++) await client.fetch('hrv', { date: DAY });
  assert.strictEqual(client.authFailed, false);
});

test('BVA-02 the fifth 403 in a row stops the run, and the tokens are still kept', async () => {
  reset();
  const { dir, client } = await signedInClient();
  fake.control.dataStatus['/hrv-service/'] = 403;
  for (let i = 0; i < 5; i++) await client.fetch('hrv', { date: DAY });
  assert.strictEqual(client.authFailed, true);
  assert.match(client.stopReason, /refused 5 request/);
  assert.ok(fs.existsSync(path.join(dir, 'tokens.json')));
});

test('BVA-03 a 429 that clears after 2 retries succeeds and waits Retry-After each time', async () => {
  reset();
  const { client, waits } = await signedInClient();
  fake.control.failOnce['/userprofile-service/socialProfile'] = 429;
  const first = await client.fetch('social_profile');
  assert.strictEqual(first.ok, true);
  assert.deepStrictEqual(waits, [1000]);
});

test('BVA-04 a third 429 (over the 2 retry budget) stops the run', async () => {
  reset();
  const { client, waits } = await signedInClient();
  fake.control.dataStatus['/userprofile-service/socialProfile'] = 429;
  const res = await client.fetch('social_profile');
  assert.strictEqual(res.errorCode, 'RATE_LIMIT');
  assert.strictEqual(client.rateLimited, true);
  assert.deepStrictEqual(waits, [1000, 1000]);
  assert.strictEqual(callsTo('/userprofile-service/socialProfile').length, 3);
});

test('EG-01 Retry-After given as an HTTP date is honored and capped at 60 s', () => {
  const at = (v) => ({ headers: { get: () => v } });
  const { retryAfterMs } = mods.client;
  assert.ok(Math.abs(retryAfterMs(at(new Date(Date.now() + 5000).toUTCString())) - 5000) < 1100);
  assert.strictEqual(retryAfterMs(at('120')), 60000);
  assert.strictEqual(retryAfterMs(at(null)), null);
});

// ---------------------------------------------------------------------------
// Whole export against the fake server
// ---------------------------------------------------------------------------

async function runExport(dataDir, outputDir, extra = {}) {
  const logs = [];
  const res = await mods.exporter.exportHealth({
    ...CREDS, startDate: FIXTURE.date_range.start, endDate: FIXTURE.date_range.end,
    outputDir, dataDir, refreshWindow: 3, requestDelayMs: 0, onLog: (l) => logs.push(l), ...extra,
  });
  return { res, logs };
}

test('AC-03 a full export through HTTP writes CSV files equal to the golden files', async () => {
  reset();
  const { res, logs } = await runExport(tmp('reg3-data-'), tmp('reg3-out-'));
  assert.strictEqual(res.ok, true, res.error);
  assert.deepStrictEqual(compareDirs(GOLDEN, res.csvDir), { addedFiles: [], removedFiles: [], files: {} });
  assert.ok(!logs.some((l) => l.type === 'warn'), JSON.stringify(logs.filter((l) => l.type === 'warn')));
});

test('EG-02 duplicate URLs are requested once per day, and split summaries not at all', async () => {
  reset();
  await runExport(tmp('eg2-data-'), tmp('eg2-out-'));
  const days = Object.keys(FIXTURE.daily).length;
  assert.strictEqual(fake.calls.filter((c) => c.path.startsWith('/usersummary-service/usersummary/daily/')).length, days);
  assert.strictEqual(fake.calls.filter((c) => c.path.startsWith('/wellness-service/wellness/dailyStress/')).length, days);
  assert.strictEqual(fake.calls.filter((c) => c.path.startsWith('/metrics-service/metrics/trainingreadiness/')).length, days);
  assert.strictEqual(fake.calls.filter((c) => c.path.endsWith('/split_summaries')).length, 0);
});

test('EG-03 goals are requested with start=1 and the Sec-Fetch-Site header', async () => {
  reset();
  await runExport(tmp('eg3-data-'), tmp('eg3-out-'));
  const goals = callsTo('/goal-service/goal/goals')[0];
  assert.match(goals.search, /start=1&limit=30/);
  assert.strictEqual(goals.headers['sec-fetch-site'], 'same-origin');
});

test('EG-04 a second export of the same old days reads the cache; only 204 days are asked again', async () => {
  reset();
  const dataDir = tmp('eg4-data-');
  await runExport(dataDir, tmp('eg4-out-'));
  fake.calls.length = 0;
  const { res } = await runExport(dataDir, tmp('eg4-out2-'));
  assert.strictEqual(res.ok, true);
  // Days with data come from the cache; days Garmin answered 204 are asked again,
  // because "no data" is never cached as final.
  const noDataDays = Object.values(FIXTURE.daily).filter((d) => d.hrv === null).length;
  assert.strictEqual(fake.calls.filter((c) => c.path.startsWith('/hrv-service/')).length, noDataDays);
  assert.deepStrictEqual(compareDirs(GOLDEN, res.csvDir), { addedFiles: [], removedFiles: [], files: {} });
});

test('EG-05 a persistent 429 stops the export with a clear reason and keeps the cache on disk', async () => {
  reset();
  const dataDir = tmp('eg5-data-');
  fake.control.dataStatus['/wellness-service/wellness/dailyHeartRate/'] = 429;
  const { res } = await runExport(dataDir, tmp('eg5-out-'));
  assert.strictEqual(res.ok, false);
  assert.match(res.error, /rate limiting/);
  assert.ok(fs.statSync(path.join(dataDir, 'garmin_cache.db')).size > 0, 'cache saved before stopping');
});

test('BVA-05 a cached day is final only when it was fetched 7 or more days after the day', () => {
  const { shouldTrustCached } = mods.exporter;
  const day = '2026-01-01';
  const at = (d) => new Date(new Date(day + 'T00:00:00').getTime() + d * 864e5).toISOString(); // stored with Z, like cache.js
  assert.strictEqual(shouldTrustCached({ data: { totalSteps: null }, fetchedAt: at(6.99) }, day), false);
  assert.strictEqual(shouldTrustCached({ data: { totalSteps: null }, fetchedAt: at(7) }, day), true);
  assert.strictEqual(shouldTrustCached({ data: { totalSteps: 9000 }, fetchedAt: at(1) }, day), false); // even with data
  assert.strictEqual(shouldTrustCached(null, day), false);
});

test('EG-07 HTTP 429 on the activity list stops the export instead of reporting success without activities', async () => {
  reset();
  fake.control.dataStatus['/activitylist-service/'] = 429;
  const { res } = await runExport(tmp('eg7-data-'), tmp('eg7-out-'));
  assert.strictEqual(res.ok, false);
  assert.match(res.error, /rate limiting/);
});

test('EG-08 HTTP 403 on the profile stops right away, before any daily request', async () => {
  reset();
  fake.control.dataStatus['/userprofile-service/socialProfile'] = 403;
  const { res } = await runExport(tmp('eg8-data-'), tmp('eg8-out-'));
  assert.strictEqual(res.ok, false);
  assert.match(res.error, /refused/);
  assert.strictEqual(fake.calls.filter((c) => c.path.startsWith('/hrv-service/')).length, 0);
});

test('DT-R10 both logins blocked (403): reports a block, not a wrong password', async () => {
  reset({ mobileLogin: 'cloudflare' });
  fake.control.widgetStatus = 403;
  try {
    const res = await mods.auth.createClient(tmp('dt10-')).login(CREDS.email, CREDS.password);
    assert.strictEqual(res.errorCode, 'BLOCKED');
    assert.match(res.error, /blocked the sign in \(HTTP 403\)/);
  } finally {
    fake.control.widgetStatus = null;
  }
});

test('EG-06 jwt helper builds tokens the auth module can read', () => {
  assert.strictEqual(mods.auth.jwtClaims(jwt({ exp: 5, client_id: 'X' })).client_id, 'X');
});
