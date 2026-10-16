// End to end: the sidecar as the app runs it (one JSON line on stdin, JSON lines out),
// with every Garmin host pointed at the local fake server. Checks the whole path from
// sign in to CSV files, and that the CSV files equal the golden files.
import { test, expect, beforeAll, afterAll } from 'bun:test';
import fs from 'node:fs';
import os from 'node:os';
import path from 'node:path';
import { createRequire } from 'node:module';

const require = createRequire(import.meta.url);
const T = path.resolve(import.meta.dir, '../../electron-app/garmin/__tests__');
const { startFakeGarmin } = require(path.join(T, 'helpers/fake-garmin.js'));
const { compareDirs } = require(path.resolve(import.meta.dir, '../../electron-app/scripts/compare-csv.js'));
const FIXTURE = JSON.parse(fs.readFileSync(path.join(T, 'fixtures/export-sample.json'), 'utf8'));

let fake;
const cleanup = [];
beforeAll(async () => { fake = await startFakeGarmin(FIXTURE); });
afterAll(async () => { await fake.close(); for (const d of cleanup) fs.rmSync(d, { recursive: true, force: true }); });

async function sidecar(req) {
  const proc = Bun.spawn(['bun', path.join(import.meta.dir, 'cli.js')], {
    stdin: 'pipe', stdout: 'pipe', stderr: 'pipe',
    env: { ...process.env, GARMIN_FAKE_SERVER: fake.base },
  });
  proc.stdin.write(JSON.stringify(req) + '\n');
  proc.stdin.end();
  const lines = (await new Response(proc.stdout).text()).trim().split('\n').map((l) => JSON.parse(l));
  await proc.exited;
  return { result: lines.find((l) => l.kind === 'result'), logs: lines.filter((l) => l.kind === 'log') };
}

function dirs() {
  // The sidecar only writes inside $HOME, so the output folder lives there during the test.
  const out = fs.mkdtempSync(path.join(os.homedir(), '.garmin-export-e2e-'));
  const data = fs.mkdtempSync(path.join(os.tmpdir(), 'garmin-e2e-data-'));
  cleanup.push(out, data);
  return { out, data };
}

test('AC-04 export through the sidecar signs in with DI and writes the golden CSV files', async () => {
  const { out, data } = dirs();
  const { result, logs } = await sidecar({
    cmd: 'export', email: 'e2e@example.invalid', password: 'right-password',
    startDate: FIXTURE.date_range.start, endDate: FIXTURE.date_range.end,
    outputDir: out, dataDir: data, refreshWindow: 3,
  });
  expect(result.ok).toBe(true);
  expect(logs.some((l) => /Login successful \(Garmin mobile sign in\)/.test(l.msg))).toBe(true);
  expect(compareDirs(path.join(T, 'golden'), result.path)).toEqual({ addedFiles: [], removedFiles: [], files: {} });
  expect(result.pack.days).toBe(Object.keys(FIXTURE.daily).length);
  expect(logs.filter((l) => l.type === 'warn')).toEqual([]);
  expect(JSON.parse(fs.readFileSync(path.join(data, 'tokens.json'), 'utf8')).di).toBeTruthy();
}, 60000);

test('AC-05 Bun sends the Sec-Fetch-Site header on the goals request', async () => {
  fake.calls.length = 0;
  const { out, data } = dirs();
  await sidecar({
    cmd: 'export', email: 'e2e@example.invalid', password: 'right-password',
    startDate: FIXTURE.date_range.start, endDate: FIXTURE.date_range.end, outputDir: out, dataDir: data,
  });
  const goals = fake.calls.find((c) => c.path === '/goal-service/goal/goals');
  expect(goals.headers['sec-fetch-site']).toBe('same-origin');
}, 60000);

test('AC-06 the legacy fallback works under Bun (cookies from getSetCookie)', async () => {
  fake.control.mobileLogin = 'cloudflare';
  try {
    const { out, data } = dirs();
    const { result, logs } = await sidecar({
      cmd: 'export', email: 'e2e@example.invalid', password: 'right-password',
      startDate: FIXTURE.date_range.start, endDate: FIXTURE.date_range.end, outputDir: out, dataDir: data,
    });
    expect(result.ok).toBe(true);
    expect(logs.some((l) => /older Garmin sign in/.test(l.msg))).toBe(true);
    const signin = fake.calls.filter((c) => c.path === '/sso/signin' && c.method === 'POST').at(-1);
    expect(signin.headers.cookie).toContain('GARMIN-SSO=1');
  } finally {
    fake.control.mobileLogin = 'ok';
  }
}, 60000);

test('AC-07 a wrong password ends with a clear error and no CSV files', async () => {
  const { out, data } = dirs();
  const { result } = await sidecar({
    cmd: 'export', email: 'e2e@example.invalid', password: 'nope',
    startDate: FIXTURE.date_range.start, endDate: FIXTURE.date_range.end, outputDir: out, dataDir: data,
  });
  expect(result.ok).toBe(false);
  expect(result.error).toMatch(/Wrong email or password/);
  expect(fs.readdirSync(out)).toEqual([]);
}, 60000);

const SIGN_INS = ['/mobile/api/login', '/sso/signin'];

test('AC-10 export runs from a saved login without email or password', async () => {
  const { out, data } = dirs();
  const login = await sidecar({ cmd: 'export', email: 'e2e@example.invalid', password: 'right-password',
    startDate: FIXTURE.date_range.start, endDate: FIXTURE.date_range.end, outputDir: out, dataDir: data });
  expect(login.result.ok).toBe(true);
  fake.calls.length = 0;
  const { result } = await sidecar({ cmd: 'export', startDate: FIXTURE.date_range.start, endDate: FIXTURE.date_range.end, outputDir: out, dataDir: data });
  expect(result.ok).toBe(true);
  expect(fake.calls.filter((c) => SIGN_INS.includes(c.path))).toEqual([]);
}, 60000);

test('AC-11 without a saved login, export asks to connect Garmin and sends no sign in', async () => {
  const { out, data } = dirs();
  fake.calls.length = 0;
  const { result } = await sidecar({ cmd: 'export', startDate: FIXTURE.date_range.start, endDate: FIXTURE.date_range.end, outputDir: out, dataDir: data });
  expect(result).toMatchObject({ ok: false, errorCode: 'GARMIN_SESSION', error: 'Connect Garmin first.' });
  expect(fake.calls.filter((c) => SIGN_INS.includes(c.path))).toEqual([]);
}, 60000);

// A DI login that expires within the 15 minute refresh margin, so the export refreshes it first.
function expiringLogin(data) {
  const now = Math.floor(Date.now() / 1000);
  fs.writeFileSync(path.join(data, 'tokens.json'), JSON.stringify({ di: {
    access_token: 'old', refresh_token: 'refresh-old', client_id: 'GARMIN_CONNECT_MOBILE_ANDROID_DI_2025Q2',
    expires_at: now + 600, refresh_token_expires_at: now + 86400,
  } }));
}

test('AC-12 a rejected refresh of a saved login asks to connect Garmin and sends no sign in', async () => {
  const { out, data } = dirs();
  expiringLogin(data);
  fake.control.diauth = 'fail';
  fake.calls.length = 0;
  try {
    const { result } = await sidecar({ cmd: 'export', startDate: FIXTURE.date_range.start, endDate: FIXTURE.date_range.end, outputDir: out, dataDir: data });
    expect(result).toMatchObject({ ok: false, errorCode: 'GARMIN_SESSION', error: 'Connect Garmin first.' });
    expect(fake.calls.some((c) => c.path.startsWith('/di-oauth2-service'))).toBe(true);
    expect(fake.calls.filter((c) => SIGN_INS.includes(c.path))).toEqual([]);
  } finally {
    fake.control.diauth = 'ok';
  }
}, 60000);

test('AC-13 a refresh that fails with HTTP 500 ends with GARMIN_ERROR, keeps tokens.json and sends no sign in', async () => {
  const { out, data } = dirs();
  expiringLogin(data);
  const before = fs.readFileSync(path.join(data, 'tokens.json'), 'utf8');
  fake.control.diauth = '500';
  fake.calls.length = 0;
  try {
    const { result } = await sidecar({ cmd: 'export', startDate: FIXTURE.date_range.start, endDate: FIXTURE.date_range.end, outputDir: out, dataDir: data });
    expect(result).toMatchObject({ ok: false, errorCode: 'GARMIN_ERROR' });
    expect(result.error).toMatch(/^Garmin did not answer/);
    expect(fake.calls.some((c) => c.path.startsWith('/di-oauth2-service'))).toBe(true);
    expect(fake.calls.filter((c) => SIGN_INS.includes(c.path))).toEqual([]);
    expect(fs.readFileSync(path.join(data, 'tokens.json'), 'utf8')).toBe(before);
  } finally {
    fake.control.diauth = 'ok';
  }
}, 60000);

test('a Garmin export removes MyFitnessPal files left in the csv folder by an earlier run', async () => {
  const { out, data } = dirs();
  const req = {
    cmd: 'export', email: 'e2e@example.invalid', password: 'right-password',
    startDate: FIXTURE.date_range.start, endDate: FIXTURE.date_range.end,
    outputDir: out, dataDir: data, refreshWindow: 3,
  };
  const first = await sidecar(req);
  expect(first.result.ok).toBe(true);
  const stale = ['mfp_food.csv', 'mfp_daily.csv', 'mfp_exercise.csv'].map((f) => path.join(first.result.path, f));
  for (const f of stale) fs.writeFileSync(f, 'old\n');
  const second = await sidecar(req);
  expect(second.result.ok).toBe(true);
  expect(second.result.path).toBe(first.result.path);
  for (const f of stale) expect(fs.existsSync(f)).toBe(false);
}, 120000);

test('a stale MyFitnessPal file that cannot be removed skips the pack preview, not the export', async () => {
  const { out, data } = dirs();
  const req = {
    cmd: 'export', email: 'e2e@example.invalid', password: 'right-password',
    startDate: FIXTURE.date_range.start, endDate: FIXTURE.date_range.end,
    outputDir: out, dataDir: data, refreshWindow: 3,
  };
  const first = await sidecar(req);
  fs.mkdirSync(path.join(first.result.path, 'mfp_food.csv')); // rmSync without recursive fails on a directory
  const { result, logs } = await sidecar(req);
  expect(result.ok).toBe(true);
  expect(result.pack).toBeNull();
  expect(logs.some((l) => l.type === 'warn' && /^LLM pack preview failed/.test(l.msg))).toBe(true);
}, 120000);

test('clear-cache also removes the MyFitnessPal day cache', async () => {
  const { data } = dirs();
  fs.mkdirSync(path.join(data, 'mfp-cache'));
  fs.writeFileSync(path.join(data, 'mfp-cache', '2026-10-05.json'), '{}');
  const { result } = await sidecar({ cmd: 'clear-cache', dataDir: data });
  expect(result.ok).toBe(true);
  expect(fs.existsSync(path.join(data, 'mfp-cache'))).toBe(false);
});
