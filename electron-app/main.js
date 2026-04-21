const { app, BrowserWindow, ipcMain, dialog, shell } = require('electron');
const path  = require('path');
const fs    = require('fs');
const os    = require('os');
const https = require('https');

const { exportHealth }  = require('./garmin/exporter');
const { initCache }     = require('./garmin/cache');
const { redactString }  = require('./util/redact');

let mainWindow;
let exportInProgress = false;

// ── Window ────────────────────────────────────────────────────────────────────
function createWindow() {
  mainWindow = new BrowserWindow({
    width: 960,
    height: 760,
    minWidth: 800,
    minHeight: 600,
    backgroundColor: '#0a0908',
    titleBarStyle: 'hiddenInset',
    vibrancy: 'under-window',
    webPreferences: {
      preload: path.join(__dirname, 'preload.js'),
      nodeIntegration: false,
      contextIsolation: true,
    },
    icon: path.join(__dirname, 'assets', 'icon.png'),
  });

  mainWindow.loadFile(path.join(__dirname, 'renderer', 'index.html'));

  if (process.argv.includes('--dev')) {
    mainWindow.webContents.openDevTools();
  }
}

app.whenReady().then(createWindow);

app.on('window-all-closed', () => {
  if (process.platform !== 'darwin') app.quit();
});

app.on('activate', () => {
  if (BrowserWindow.getAllWindows().length === 0) createWindow();
});

// ── IPC: default output dir ───────────────────────────────────────────────────
ipcMain.handle('default-output-dir', () => {
  return path.join(app.getPath('documents'), 'GarminExport');
});

// ── IPC: choose output directory ──────────────────────────────────────────────
ipcMain.handle('choose-dir', async () => {
  const result = await dialog.showOpenDialog(mainWindow, {
    properties: ['openDirectory', 'createDirectory'],
    defaultPath: path.join(os.homedir(), 'Documents'),
  });
  return result.canceled ? null : result.filePaths[0];
});

// ── IPC: open folder in Finder ────────────────────────────────────────────────
ipcMain.handle('open-folder', async (_, folderPath) => {
  shell.openPath(folderPath);
});

// ── IPC: open external URL ────────────────────────────────────────────────────
ipcMain.handle('open-url', async (_, url) => {
  try {
    const u = new URL(url);
    if (u.protocol !== 'http:' && u.protocol !== 'https:') return;
    shell.openExternal(url);
  } catch {
    // Invalid URL — silently drop
  }
});

// ── IPC: get app version ──────────────────────────────────────────────────────
ipcMain.handle('get-version', () => app.getVersion());

// ── IPC: send bug report ──────────────────────────────────────────────────────
// Always writes a diagnostic bundle file under userData/diagnostics/ and then
// opens the user's default mail client with a pre-filled message addressed to
// the maintainer. The `mailto:` body is truncated to stay within cross-client
// URL length limits; the full log lives in the bundle file whose path is
// referenced in the body so the user can attach it manually.
const BUG_REPORT_EMAIL = 'danisnowman@gmail.com';
// Cap the ENCODED mailto URL, not the raw body — percent-encoding inflates
// newlines/brackets, and most mail clients cap the full URL around 2 KB.
const MAILTO_URL_MAX = 1800;
// Hard caps to prevent a compromised/buggy renderer from DoS-ing disk or
// memory via send-bug-report.
const MAX_LOG_ENTRIES = 500;
const MAX_MSG_LEN = 4000;
const MAX_META_BYTES = 16 * 1024;
// Rate limit: one bug report per N ms per session.
const BUG_REPORT_MIN_INTERVAL_MS = 2000;
let lastBugReportAt = 0;

function redactLogLine(line) {
  return redactString(line);
}

function clipStr(s, max) {
  if (s == null) return '';
  const str = String(s);
  return str.length > max ? str.slice(0, max) + '…[clipped]' : str;
}

function safeSerializeMeta(meta) {
  if (!meta) return null;
  let json;
  try {
    json = JSON.stringify(meta, null, 2);
  } catch {
    return '(unserializable)';
  }
  if (json.length > MAX_META_BYTES) json = json.slice(0, MAX_META_BYTES) + '…[clipped]';
  return redactString(json);
}

function sanitizePayload(payload) {
  const p = payload && typeof payload === 'object' ? payload : {};
  const out = {
    appVersion: clipStr(p.appVersion, 64),
    platform: clipStr(p.platform, 64),
    osRelease: clipStr(p.osRelease, 64),
    arch: clipStr(p.arch, 32),
    electronVersion: clipStr(p.electronVersion, 64),
    chromeVersion: clipStr(p.chromeVersion, 64),
    nodeVersion: clipStr(p.nodeVersion, 64),
    lastError: null,
    recentLog: [],
  };
  if (p.lastError && typeof p.lastError === 'object') {
    const le = p.lastError;
    out.lastError = {
      ts: clipStr(le.ts, 64),
      type: clipStr(le.type, 32),
      errorCode: clipStr(le.errorCode, 64),
      msg: clipStr(le.msg, MAX_MSG_LEN),
      meta: le.meta,
    };
  }
  const log = Array.isArray(p.recentLog) ? p.recentLog.slice(-MAX_LOG_ENTRIES) : [];
  out.recentLog = log.map((e) => ({
    ts: clipStr(e && e.ts, 64),
    type: clipStr(e && e.type, 32),
    errorCode: clipStr(e && e.errorCode, 64),
    msg: clipStr(e && e.msg, MAX_MSG_LEN),
  }));
  return out;
}

function formatDiagnosticBundle(payload) {
  const lines = [];
  lines.push('Garmin Data Exporter — diagnostic bundle');
  lines.push(`Generated: ${new Date().toISOString()}`);
  lines.push('');
  lines.push('## Environment');
  lines.push(`App version: ${payload.appVersion || 'unknown'}`);
  lines.push(`Platform:    ${payload.platform || process.platform} ${payload.osRelease || os.release()}`);
  lines.push(`Arch:        ${payload.arch || process.arch}`);
  lines.push(`Electron:    ${payload.electronVersion || process.versions.electron}`);
  lines.push(`Chrome:      ${payload.chromeVersion || process.versions.chrome}`);
  lines.push(`Node:        ${payload.nodeVersion || process.versions.node}`);
  lines.push('');
  lines.push('## Last error');
  if (payload.lastError) {
    const le = payload.lastError;
    lines.push(`Time:     ${le.ts || 'unknown'}`);
    lines.push(`Severity: ${le.type || 'unknown'}`);
    lines.push(`Code:     ${le.errorCode || '(none)'}`);
    lines.push(`Message:  ${le.msg || ''}`);
    const metaStr = safeSerializeMeta(le.meta);
    if (metaStr) {
      lines.push('Meta:');
      lines.push(metaStr);
    }
  } else {
    lines.push('(no recent errors recorded)');
  }
  lines.push('');
  lines.push('## Recent log');
  const entries = Array.isArray(payload.recentLog) ? payload.recentLog : [];
  if (entries.length === 0) {
    lines.push('(log is empty)');
  } else {
    for (const e of entries) {
      const ts = e.ts || '';
      const type = e.type || '';
      lines.push(`${ts} [${type}] ${e.msg || ''}`);
    }
  }
  // Final defense-in-depth pass: scrub the whole blob so secrets that slipped
  // through per-field redaction (e.g., embedded in meta) are caught.
  return redactString(lines.join('\n'));
}

function formatMailSummary(payload, bundlePath) {
  const parts = [];
  parts.push('Hi — I hit a bug in Garmin Data Exporter. Details below.');
  parts.push('');
  parts.push(`App version:   ${payload.appVersion || 'unknown'}`);
  parts.push(`Platform:      ${payload.platform || process.platform} ${payload.osRelease || os.release()}`);
  parts.push(`Electron:      ${payload.electronVersion || process.versions.electron}`);
  parts.push('');
  if (payload.lastError) {
    const le = payload.lastError;
    parts.push('Last error:');
    parts.push(`  ${le.ts || ''} [${le.errorCode || le.type || 'error'}]`);
    parts.push(`  ${le.msg || ''}`);
    parts.push('');
  } else {
    parts.push('Last error: (no recent errors recorded)');
    parts.push('');
  }
  const entries = Array.isArray(payload.recentLog) ? payload.recentLog : [];
  const tail = entries.slice(-30);
  if (tail.length > 0) {
    parts.push('Recent log (last ' + tail.length + ' lines):');
    for (const e of tail) {
      parts.push(`  ${e.ts || ''} [${e.type || ''}] ${e.msg || ''}`);
    }
    parts.push('');
  }
  if (bundlePath) {
    parts.push(`Full diagnostics saved to: ${bundlePath}`);
    parts.push('(Please attach that file to this email if possible.)');
  }
  return redactString(parts.join('\n'));
}

/**
 * Build a mailto URL whose total encoded length stays under MAILTO_URL_MAX,
 * trimming the body from the tail as needed. Newlines in the body are stripped
 * so they cannot smuggle mail-client header injection through decoders that
 * unencode CRLF.
 */
function buildMailtoUrl(subject, body) {
  const cleanSubject = String(subject).replace(/[\r\n]+/g, ' ').trim();
  const cleanBody = String(body).replace(/\r\n/g, '\n');
  const base = `mailto:${BUG_REPORT_EMAIL}?subject=${encodeURIComponent(cleanSubject)}&body=`;
  const suffix = '\n\n…truncated. See full diagnostics in the bundle file above.';
  const budget = MAILTO_URL_MAX - base.length;

  let body2 = cleanBody;
  if (encodeURIComponent(body2).length <= budget) {
    return { url: base + encodeURIComponent(body2), truncated: false };
  }
  // Binary-trim raw length until the encoded version fits the budget.
  let lo = 0;
  let hi = body2.length;
  while (lo < hi) {
    const mid = Math.floor((lo + hi + 1) / 2);
    if (encodeURIComponent(body2.slice(0, mid) + suffix).length <= budget) lo = mid;
    else hi = mid - 1;
  }
  body2 = body2.slice(0, lo) + suffix;
  return { url: base + encodeURIComponent(body2), truncated: true };
}

ipcMain.handle('send-bug-report', async (_, payload = {}) => {
  // Rate-limit to prevent disk DoS / mail-client storm from a runaway renderer.
  const now = Date.now();
  if (now - lastBugReportAt < BUG_REPORT_MIN_INTERVAL_MS) {
    return {
      ok: false,
      error: 'Please wait a moment before sending another bug report.',
      bundlePath: null,
      mailOpened: false,
      recipient: BUG_REPORT_EMAIL,
    };
  }
  lastBugReportAt = now;

  const safe = sanitizePayload(payload);
  const results = {
    ok: true,
    bundlePath: null,
    warning: null,
    mailOpened: false,
    truncated: false,
    recipient: BUG_REPORT_EMAIL,
  };

  // 1) Always try to write the diagnostic bundle first (with unique filename).
  try {
    const dir = path.join(app.getPath('userData'), 'diagnostics');
    fs.mkdirSync(dir, { recursive: true });
    const d = new Date();
    const pad = (n) => String(n).padStart(2, '0');
    const stamp = `${d.getFullYear()}-${pad(d.getMonth() + 1)}-${pad(d.getDate())}-${pad(d.getHours())}${pad(d.getMinutes())}${pad(d.getSeconds())}-${pad(d.getMilliseconds())}`;
    const rand = Math.random().toString(36).slice(2, 8);
    const filePath = path.join(dir, `diagnostic-${stamp}-${rand}.txt`);
    fs.writeFileSync(filePath, formatDiagnosticBundle(safe), { encoding: 'utf8', flag: 'wx' });
    results.bundlePath = filePath;
  } catch (err) {
    results.warning = `Could not write diagnostic bundle: ${err.message}`;
  }

  // 2) Compose and open the mailto link.
  try {
    const code = (safe.lastError && safe.lastError.errorCode)
      ? safe.lastError.errorCode.replace(/[^A-Za-z0-9_]/g, '')
      : 'general';
    const version = safe.appVersion || app.getVersion();
    const subject = `Garmin Data Exporter bug — ${code || 'general'} — v${version}`;
    const body = formatMailSummary(safe, results.bundlePath);
    const { url, truncated } = buildMailtoUrl(subject, body);
    results.truncated = truncated;
    await shell.openExternal(url);
    results.mailOpened = true;
  } catch (err) {
    results.ok = false;
    results.error = `Could not open mail client: ${err.message}`;
  }

  return results;
});

// ── IPC: check for updates via GitHub releases ────────────────────────────────
ipcMain.handle('check-for-updates', () => {
  return new Promise((resolve) => {
    const opts = {
      hostname: 'api.github.com',
      path: '/repos/danihrndzld/garmin-health-exporter/releases/latest',
      headers: { 'User-Agent': 'garmin-data-exporter' },
    };
    https.get(opts, (res) => {
      let body = '';
      res.on('data', chunk => body += chunk);
      res.on('end', () => {
        try {
          const data = JSON.parse(body);
          const latest = (data.tag_name || '').replace(/^v/, '');
          resolve({ ok: true, latest, url: data.html_url || '' });
        } catch {
          resolve({ ok: false, error: 'Invalid response from GitHub' });
        }
      });
    }).on('error', err => resolve({ ok: false, error: err.message }));
  });
});

// ── IPC: download health data ─────────────────────────────────────────────────
ipcMain.handle('download-health', async (event, { email, password, daysBack, outputDir, refreshWindow }) => {
  // Guard against concurrent exports
  if (exportInProgress) {
    return { ok: false, error: 'An export is already in progress.' };
  }

  // Validate daysBack
  const days = parseInt(daysBack, 10);
  if (!Number.isInteger(days) || days < 1 || days > 90) {
    return { ok: false, error: 'daysBack must be an integer between 1 and 90.' };
  }

  // Validate refreshWindow
  const rw = refreshWindow != null ? parseInt(refreshWindow, 10) : 3;
  if (!Number.isInteger(rw) || rw < 1 || rw > 7) {
    return { ok: false, error: 'refreshWindow must be an integer between 1 and 7.' };
  }

  // Validate outputDir — must be strictly contained within home (path.relative
  // avoids the prefix-match bypass of startsWith, e.g. `/Users/dani-evil/...`).
  // Also resolve symlinks so a link inside $HOME pointing outside is rejected.
  let resolvedOutput = path.resolve(outputDir);
  const home = os.homedir();
  try {
    if (fs.existsSync(resolvedOutput)) {
      resolvedOutput = fs.realpathSync(resolvedOutput);
    }
  } catch (_e) {
    return { ok: false, error: 'Output directory could not be resolved.' };
  }
  const rel = path.relative(home, resolvedOutput);
  const escapesHome = rel.startsWith('..') || path.isAbsolute(rel);
  if (escapesHome) {
    return { ok: false, error: 'Output directory must be within your home directory.' };
  }

  exportInProgress = true;

  try {
    const dataDir = app.getPath('userData');

    const result = await exportHealth({
      email,
      password,
      daysBack: days,
      outputDir: resolvedOutput,
      refreshWindow: rw,
      dataDir,
      onProgress: (data) => {
        if (!event.sender.isDestroyed()) {
          event.sender.send('progress', data);
        }
      },
      onLog: (data) => {
        if (!event.sender.isDestroyed()) {
          const payload = {
            type: data.type,
            msg: data.message,
            ts: new Date().toLocaleTimeString(),
          };
          if (data.errorCode) payload.errorCode = data.errorCode;
          if (data.meta) payload.meta = data.meta;
          event.sender.send('log', payload);
        }
      },
    });

    if (result.ok) {
      return { ok: true, path: result.csvDir };
    }
    return { ok: false, error: result.error };

  } catch (err) {
    return { ok: false, error: err.message };
  } finally {
    exportInProgress = false;
  }
});

// ── IPC: clear cache ─────────────────────────────────────────────────────────
ipcMain.handle('clear-cache', async () => {
  if (exportInProgress) {
    return { ok: false, error: 'Cannot clear cache while an export is in progress.' };
  }
  try {
    const dbPath = path.join(app.getPath('userData'), 'garmin_cache.db');
    const cache = await initCache(dbPath);
    cache.clearAll();
    cache.save();
    cache.close();
    return { ok: true };
  } catch (err) {
    return { ok: false, error: err.message };
  }
});
