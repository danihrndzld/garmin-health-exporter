// Pins the JavaScript pack, report and redaction outputs to golden files that the Go port
// reproduces byte for byte (stage 3). Regenerate only on purpose:
//   TZ=America/Guatemala UPDATE_GOLDEN=1 bun test sidecar/golden.test.js
// The time zone is fixed because the context's "Exported" line prints the IANA zone name.
import { test, expect } from 'bun:test';
import fs from 'node:fs';
import os from 'node:os';
import path from 'node:path';
import { createRequire } from 'node:module';
import { previewPack, buildPack } from './llm-pack.js';
import { buildReport } from './report.js';

const require = createRequire(import.meta.url);
const { redactString, redactUrl } = require('../../electron-app/util/redact.js');

const CORE = path.resolve(import.meta.dir, '../core/internal');
const PACK = path.join(CORE, 'pack/testdata');
const REPORT = path.join(CORE, 'report/testdata');
const UPDATE = !!process.env.UPDATE_GOLDEN;

function pin(file, text) {
  if (UPDATE) { fs.mkdirSync(path.dirname(file), { recursive: true }); fs.writeFileSync(file, text); }
  expect(fs.readFileSync(file, 'utf8')).toBe(text);
}
const json = (v) => JSON.stringify(v, null, 2) + '\n';

// The user's Mac is in America/Guatemala (/etc/localtime -> /var/db/timezone/zoneinfo/America/Guatemala,
// checked 2026-10-16), so the file also passes inside a plain `bun test sidecar`. On another zone run it
// with TZ=America/Guatemala; the gates in Tasks 5 and 6 say so.
test('the zone is fixed, so the Exported line is reproducible', () => {
  expect(Intl.DateTimeFormat().resolvedOptions().timeZone).toBe('America/Guatemala');
});

const packOpts = (csvDir) => ({
  csvDir, startDate: '2024-02-17', endDate: '2024-02-21',
  warnings: ['hill_score skipped (404)', '[skip] endurance_score: HTTP 204'],
  now: new Date(2026, 9, 15, 20, 53), // local 2026-10-15 20:53
});

test('AC-18 pack preview and both one-file packs equal the golden files', () => {
  const csvDir = path.join(PACK, 'csv_2024-02-21');
  const p = previewPack(packOpts(csvDir));
  pin(path.join(PACK, 'golden/CONTEXT.es.md'), p.langs.es.context);
  pin(path.join(PACK, 'golden/CONTEXT.en.md'), p.langs.en.context);
  const { csvDir: _dir, langs, ...rest } = p; // csvDir is machine specific; context is in the .md files
  pin(path.join(PACK, 'golden/preview.json'), json({ ...rest, langs: Object.fromEntries(Object.entries(langs).map(([l, v]) => [l, { bytes: v.bytes, tokens: v.tokens, singleName: v.singleName }])) }));
  for (const lang of ['es', 'en']) {
    const work = fs.mkdtempSync(path.join(os.tmpdir(), 'golden-pack-'));
    const copy = path.join(work, 'csv_2024-02-21');
    fs.cpSync(csvDir, copy, { recursive: true });
    const b = buildPack({ ...packOpts(copy), lang, mode: 'single' });
    pin(path.join(PACK, 'golden', path.basename(b.file)), fs.readFileSync(b.file, 'utf8'));
    const f = buildPack({ ...packOpts(copy), lang, mode: 'files' });
    expect(fs.readFileSync(path.join(f.dir, `CONTEXT.${lang}.md`), 'utf8')).toBe(p.langs[lang].context);
    expect(f.count).toBe(22);
    fs.rmSync(work, { recursive: true, force: true });
  }
});

test('AC-19 the bug report URLs equal the golden file', () => {
  const log = Array.from({ length: 300 }, (_, i) => ({
    ts: `10:00:${String(i % 60).padStart(2, '0')}`, type: i % 2 ? 'warn' : 'dim',
    msg: `line ${i} ${'x'.repeat(40)} token=t${i} user${i}@example.com /Users/dani/Documents/x${i}`,
  }));
  const input = {
    error: 'Auth failed for a@b.com: Authorization: Bearer eyJhbGciOiJIUzI1NiJ9.eyJzdWIiOiIxMjM0In0.sig password=hunter2 in /Users/dani/Library',
    log, version: '4.3.0', os: 'Mac OS X 15.6',
  };
  pin(path.join(REPORT, 'input.json'), json(input));
  pin(path.join(REPORT, 'golden.json'), json(buildReport(input)));
});

test('AC-20 redaction cases equal the golden file', () => {
  const inputs = [
    'https://api.example.com/x?token=abc&access_token=def&authCode=ghi&secret=s&password=p&refresh_token=rt&sig=zz&keep=ok',
    'plain text', '',
    'Authorization: Bearer eyJ0.abc.def', 'auth: bearer abc123.def-ghi', 'Authorization: Basic dXNlcjpwYXNz',
    'Cookie: SESSIONID=abc; other=xyz', 'Set-Cookie: JWT=eyJ...; Path=/',
    'error with token eyJhbGciOiJIUzI1NiJ9.eyJzdWIiOiIxMjM0In0.sig embedded',
    '{"password":"hunter2","token":"abc","keep":"ok"}',
    'GET https://x/y?access_token=abc&name=me failed',
    'HTTP 500: {"error":"bad","password":"x"} Bearer abc.def.ghi at /Users/dani/x',
    'two lines\nAuthorization: Bearer one.two.three\nCookie: a=b',
    'acentos: contraseña=secreta y "clientSecret":"é"',
  ];
  const cases = [];
  for (const s of inputs) cases.push({ fn: 'string', in: s, out: redactString(s) }, { fn: 'url', in: s, out: redactUrl(s) });
  pin(path.join(REPORT, 'cases.json'), json(cases));
});
