// End to end: the Go binary as the app runs it (one JSON line on stdin), against the
// fake MFP server from cmd/fake-mfp. The CSV files must equal the Go golden files.
import { test, expect, beforeAll, afterAll } from 'bun:test';
import fs from 'node:fs';
import os from 'node:os';
import path from 'node:path';

const CORE = path.resolve(import.meta.dir, '../core');
const TESTDATA = path.join(CORE, 'internal/mfp/testdata');
const BIN = path.join(os.tmpdir(), `garmin-export-core-e2e-${process.pid}`);
const FAKE_BIN = `${BIN}-fake-mfp`; // a built binary, because killing `go run` would leave its child serving
let fake, base;
const cleanup = [];

beforeAll(async () => {
  const build = Bun.spawnSync(['go', 'build', '-o', BIN, './cmd/garmin-export-core'], { cwd: CORE });
  if (build.exitCode !== 0) throw new Error(String(build.stderr));
  const buildFake = Bun.spawnSync(['go', 'build', '-o', FAKE_BIN, './cmd/fake-mfp'], { cwd: CORE });
  if (buildFake.exitCode !== 0) throw new Error(String(buildFake.stderr));
  fake = Bun.spawn([FAKE_BIN, TESTDATA], { cwd: CORE, stdout: 'pipe' });
  const reader = fake.stdout.getReader();
  base = new TextDecoder().decode((await reader.read()).value).trim();
});
afterAll(() => { fake?.kill(); fs.rmSync(BIN, { force: true }); fs.rmSync(FAKE_BIN, { force: true }); for (const d of cleanup) fs.rmSync(d, { recursive: true, force: true }); });

async function core(req) {
  const p = Bun.spawn([BIN], { stdin: 'pipe', stdout: 'pipe', env: { ...process.env, MFP_FAKE_SERVER: base } });
  p.stdin.write(JSON.stringify(req) + '\n');
  p.stdin.end();
  const lines = (await new Response(p.stdout).text()).trim().split('\n').map((l) => JSON.parse(l));
  return { result: lines.find((l) => l.kind === 'result'), progress: lines.filter((l) => l.kind === 'progress') };
}

function homeDirs() {
  const root = fs.mkdtempSync(path.join(os.homedir(), '.mfp-e2e-'));
  cleanup.push(root);
  const dataDir = path.join(root, 'data'), csvDir = path.join(root, 'csv_2026-10-07');
  fs.mkdirSync(dataDir); fs.mkdirSync(csvDir);
  return { dataDir, csvDir };
}

test('AC-08 mfp-export through the Go binary writes CSV files equal to the golden files', async () => {
  const { dataDir, csvDir } = homeDirs();
  fs.writeFileSync(path.join(dataDir, 'mfp_session.json'), JSON.stringify({ user_agent: 'e2e',
    cookies: [{ name: '__Secure-next-auth.session-token', value: 's1', domain: '.myfitnesspal.com', path: '/' }] }), { mode: 0o600 });
  const { result, progress } = await core({ cmd: 'mfp-export', startDate: '2026-10-04', endDate: '2026-10-07', dataDir, csvDir });
  expect(result.ok).toBe(true);
  expect(result.loggedDays).toBe(2);
  expect(progress.at(-1)).toMatchObject({ phase: 'mfp', current: 4, total: 4 });
  for (const f of ['mfp_food.csv', 'mfp_daily.csv', 'mfp_exercise.csv']) {
    expect(fs.readFileSync(path.join(csvDir, f)).equals(fs.readFileSync(path.join(TESTDATA, 'golden', f)))).toBe(true);
  }
}, 60000);

test('AC-09 without a session the binary asks to connect MyFitnessPal', async () => {
  const { dataDir, csvDir } = homeDirs();
  const { result } = await core({ cmd: 'mfp-export', startDate: '2026-10-04', endDate: '2026-10-07', dataDir, csvDir });
  expect(result).toMatchObject({ ok: false, errorCode: 'MFP_SESSION', error: 'Connect MyFitnessPal first.' });
}, 60000);
