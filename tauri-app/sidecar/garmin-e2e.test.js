// End to end, cross engine: the Go binary against the JavaScript fake Garmin that the Bun
// end to end test uses. The CSV files must equal the golden files, and tokens.json must
// work in both engines.
import { test, expect, beforeAll, afterAll } from 'bun:test';
import fs from 'node:fs';
import os from 'node:os';
import path from 'node:path';
import { createRequire } from 'node:module';

const require = createRequire(import.meta.url);
const T = path.resolve(import.meta.dir, '../../electron-app/garmin/__tests__');
const { startFakeGarmin } = require(path.join(T, 'helpers/fake-garmin.js'));
const { compareDirs } = require(path.resolve(import.meta.dir, '../../electron-app/scripts/compare-csv.js'));
const auth = require(path.resolve(import.meta.dir, '../../electron-app/garmin/auth.js'));
const FIXTURE = JSON.parse(fs.readFileSync(path.join(T, 'fixtures/export-sample.json'), 'utf8'));
const CORE = path.resolve(import.meta.dir, '../core');
const BIN = path.join(os.tmpdir(), `garmin-export-core-garmin-e2e-${process.pid}`);
let fake;
const cleanup = [];

beforeAll(async () => {
  const build = Bun.spawnSync(['go', 'build', '-o', BIN, './cmd/garmin-export-core'], { cwd: CORE });
  if (build.exitCode !== 0) throw new Error(String(build.stderr));
  fake = await startFakeGarmin(FIXTURE);
});
afterAll(async () => { await fake.close(); fs.rmSync(BIN, { force: true }); for (const d of cleanup) fs.rmSync(d, { recursive: true, force: true }); });

async function core(req) {
  const p = Bun.spawn([BIN], { stdin: 'pipe', stdout: 'pipe', env: { ...process.env, GARMIN_FAKE_SERVER: fake.base } });
  p.stdin.write(JSON.stringify(req) + '\n');
  p.stdin.end();
  const lines = (await new Response(p.stdout).text()).trim().split('\n').map((l) => JSON.parse(l));
  return { result: lines.find((l) => l.kind === 'result'), logs: lines.filter((l) => l.kind === 'log') };
}

function dirs() {
  // The Go core only accepts paths inside $HOME, the data dir included.
  const root = fs.mkdtempSync(path.join(os.homedir(), '.garmin-go-e2e-'));
  cleanup.push(root);
  const out = path.join(root, 'out'), data = path.join(root, 'data');
  fs.mkdirSync(out); fs.mkdirSync(data);
  return { out, data };
}

const range = { startDate: FIXTURE.date_range.start, endDate: FIXTURE.date_range.end };

test('AC-15 Go connects once and exports CSV files equal to the golden files', async () => {
  const { out, data } = dirs();
  const c = await core({ cmd: 'garmin-connect', email: 'e2e@example.invalid', password: 'right-password', dataDir: data });
  expect(c.result).toMatchObject({ ok: true, connected: true, loginKind: 'di' });
  const { result, logs } = await core({ cmd: 'export', garmin: true, ...range, outputDir: out, dataDir: data, refreshWindow: 3 });
  expect(result.ok).toBe(true);
  expect(compareDirs(path.join(T, 'golden'), result.path)).toEqual({ addedFiles: [], removedFiles: [], files: {} });
  for (const f of fs.readdirSync(path.join(T, 'golden'))) {
    expect(fs.readFileSync(path.join(result.path, f)).equals(fs.readFileSync(path.join(T, 'golden', f)))).toBe(true);
  }
  expect(logs.filter((l) => l.type === 'warn')).toEqual([]);
}, 120000);

test('AC-16 a tokens.json written by Go loads in Bun, and one written by Bun loads in Go', async () => {
  const { data } = dirs();
  await core({ cmd: 'garmin-connect', email: 'e2e@example.invalid', password: 'right-password', dataDir: data });
  const loaded = await auth.createClient(data).loadTokens();
  expect(loaded.ok).toBe(true);
  fs.rmSync(path.join(data, 'tokens.json'));
  process.env.GARMIN_FAKE_SERVER = fake.base;
  try {
    expect((await auth.createClient(data).login('e2e@example.invalid', 'right-password')).ok).toBe(true);
  } finally {
    delete process.env.GARMIN_FAKE_SERVER;
  }
  const { result } = await core({ cmd: 'garmin-status', dataDir: data });
  expect(result).toMatchObject({ ok: true, connected: true, expired: false, loginKind: 'di' });
}, 120000);
