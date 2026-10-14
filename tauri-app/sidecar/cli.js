// Sidecar for the Tauri shell. Reuses the tested Node core in electron-app/garmin.
// Protocol: one JSON request on stdin (password never in argv), JSON lines on stdout:
//   {kind:"log", type, msg, ts, errorCode?} | {kind:"progress", current, total, phase} | {kind:"result", ok, ...}
// This process is the trust boundary now: all validation from the old main.js lives here.
import fs from 'node:fs';
import os from 'node:os';
import path from 'node:path';
import { writePack, readPack } from './llm-pack.js';
import { buildReport } from './report.js';
import wasmPath from '../../electron-app/node_modules/sql.js/dist/sql-wasm.wasm' with { type: 'file' };

process.env.GARMIN_SQL_WASM ||= wasmPath;

const { exportHealth } = require('../../electron-app/garmin/exporter');
const { initCache } = require('../../electron-app/garmin/cache');
const { validateRange } = require('../../electron-app/renderer/date-range');

const emit = (o) => process.stdout.write(JSON.stringify(o) + '\n');
const ts = () => new Date().toLocaleTimeString();
const fail = (error, extra = {}) => ({ ok: false, error, ...extra });

function resolveDataDir(dataDir) {
  if (typeof dataDir !== 'string' || !path.isAbsolute(dataDir)) throw new Error('dataDir must be absolute.');
  fs.mkdirSync(dataDir, { recursive: true });
  return dataDir;
}

// Output must stay inside $HOME; realpath defeats symlinks pointing outside.
function resolveOutputDir(outputDir) {
  let resolved = path.resolve(String(outputDir || ''));
  if (fs.existsSync(resolved)) resolved = fs.realpathSync(resolved);
  const rel = path.relative(os.homedir(), resolved);
  if (rel.startsWith('..') || path.isAbsolute(rel)) return null;
  return resolved;
}

async function runExport(req) {
  const { email, password, daysBack, startDate, endDate, outputDir, refreshWindow } = req;
  if (!email || !password) return fail('Email and password are required.');

  let range;
  if (startDate != null && endDate != null) {
    const check = validateRange({ startDate, endDate, maxSpanDays: 90 });
    if (!check.ok) return fail(check.message, { errorCode: check.errorCode });
    range = { startDate: check.startDate, endDate: check.endDate };
  } else {
    const n = parseInt(daysBack, 10);
    if (!Number.isInteger(n) || n < 1 || n > 90) return fail('daysBack must be an integer between 1 and 90.');
    range = { daysBack: n };
  }

  const rw = refreshWindow != null ? parseInt(refreshWindow, 10) : 3;
  if (!Number.isInteger(rw) || rw < 1 || rw > 7) return fail('refreshWindow must be an integer between 1 and 7.');

  const out = resolveOutputDir(outputDir);
  if (!out) return fail('Output directory must be within your home directory.');

  const warnings = [];
  const res = await exportHealth({
    email, password, ...range,
    outputDir: out,
    refreshWindow: rw,
    dataDir: resolveDataDir(req.dataDir),
    onProgress: (p) => emit({ kind: 'progress', ...p }),
    onLog: (l) => (l.type === 'warn' && warnings.push(l.message), emit({ kind: 'log', type: l.type, msg: l.message, ts: ts(), ...(l.errorCode && { errorCode: l.errorCode }) })),
  });
  if (!res.ok) return fail(res.error);
  const summary = summarize(res.jsonPath);
  let pack = null;
  try {
    const range = summary?.range || {};
    writePack({ csvDir: res.csvDir, startDate: range.start, endDate: range.end, warnings });
    pack = readPack(res.csvDir);
  } catch (err) {
    emit({ kind: 'log', type: 'warn', msg: `LLM pack failed: ${err.message}`, ts: ts() });
  }
  return { ok: true, path: res.csvDir, jsonPath: res.jsonPath, summary, pack };
}

// Small per-day series for the preview cards; the full data stays in the CSVs.
function summarize(jsonPath) {
  try {
    const data = JSON.parse(fs.readFileSync(jsonPath, 'utf8'));
    const days = Object.entries(data.daily || {}).sort(([a], [b]) => a.localeCompare(b)).map(([date, d]) => ({
      date,
      steps: d.stats?.totalSteps ?? null,
      rhr: d.heart_rates?.restingHeartRate ?? null,
      sleepScore: d.sleep?.dailySleepDTO?.sleepScores?.overall?.value ?? null,
      sleepSec: d.sleep?.dailySleepDTO?.sleepTimeSeconds ?? null,
      hrv: d.hrv?.hrvSummary?.lastNightAvg ?? null,
    }));
    const types = {};
    for (const a of data.activities || []) {
      const k = a.activityType?.typeKey || 'other';
      types[k] = (types[k] || 0) + 1;
    }
    return { range: data.date_range, days, activities: (data.activities || []).length, types };
  } catch {
    return null;
  }
}

async function summary(req) {
  const p = resolveOutputDir(req.jsonPath);
  if (!p || !p.endsWith('.json')) return fail('jsonPath must be a .json file within your home directory.');
  const s = summarize(p);
  const dir = req.csvDir && resolveOutputDir(req.csvDir);
  return s ? { ok: true, summary: s, pack: dir ? readPack(dir) : null } : fail('Could not read export.');
}

async function clearCache(req) {
  const cache = await initCache(path.join(resolveDataDir(req.dataDir), 'garmin_cache.db'));
  cache.clearAll();
  cache.save();
  cache.close();
  return { ok: true };
}

const COMMANDS = { export: runExport, 'clear-cache': clearCache, summary, report: async (req) => ({ ok: true, ...buildReport(req) }) };

async function main() {
  // First line only: Tauri's shell Child can write() but cannot close stdin.
  let raw = '';
  for await (const chunk of process.stdin) {
    raw += chunk;
    if (raw.includes('\n')) break;
  }
  raw = raw.split('\n')[0];
  let req;
  try { req = JSON.parse(raw); } catch { return emit({ kind: 'result', ...fail('Invalid request.') }); }
  const cmd = COMMANDS[req && req.cmd];
  if (!cmd) return emit({ kind: 'result', ...fail(`Unknown command: ${req && req.cmd}`) });
  try {
    emit({ kind: 'result', ...(await cmd(req)) });
  } catch (err) {
    emit({ kind: 'result', ...fail(err.message) });
  }
}

main();
