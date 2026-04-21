const { app, BrowserWindow, ipcMain, dialog, shell } = require('electron');
const path  = require('path');
const fs    = require('fs');
const os    = require('os');
const https = require('https');

const { exportHealth }  = require('./garmin/exporter');
const { initCache }     = require('./garmin/cache');

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
const MAILTO_BODY_MAX = 1800;

function redactLogLine(line) {
  if (!line) return line;
  return String(line)
    .replace(/Bearer\s+[A-Za-z0-9._~+/=-]+/g, 'Bearer REDACTED')
    .replace(/([?&])(token|access_token)=[^&\s]*/gi, '$1$2=REDACTED');
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
    lines.push(`Time:        ${le.ts || 'unknown'}`);
    lines.push(`Type:        ${le.type || 'unknown'}`);
    lines.push(`Code:        ${le.errorCode || '(none)'}`);
    lines.push(`Message:     ${redactLogLine(le.msg || '')}`);
    if (le.meta) {
      try {
        lines.push('Meta:');
        lines.push(JSON.stringify(le.meta, null, 2));
      } catch {
        lines.push('Meta: (unserializable)');
      }
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
      lines.push(`${ts} [${type}] ${redactLogLine(e.msg || '')}`);
    }
  }
  return lines.join('\n');
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
    parts.push(`  ${redactLogLine(le.msg || '')}`);
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
      parts.push(`  ${e.ts || ''} [${e.type || ''}] ${redactLogLine(e.msg || '')}`);
    }
    parts.push('');
  }
  if (bundlePath) {
    parts.push(`Full diagnostics saved to: ${bundlePath}`);
    parts.push('(Please attach that file to this email if possible.)');
  }
  return parts.join('\n');
}

function truncateForMailto(body) {
  if (body.length <= MAILTO_BODY_MAX) return body;
  return body.slice(0, MAILTO_BODY_MAX - 80) + '\n\n…truncated. See full diagnostics in the bundle file above.';
}

ipcMain.handle('send-bug-report', async (_, payload = {}) => {
  const results = { ok: true, bundlePath: null, warning: null, mailOpened: false };

  // 1) Always try to write the diagnostic bundle first.
  try {
    const dir = path.join(app.getPath('userData'), 'diagnostics');
    fs.mkdirSync(dir, { recursive: true });
    const now = new Date();
    const pad = (n) => String(n).padStart(2, '0');
    const stamp = `${now.getFullYear()}-${pad(now.getMonth() + 1)}-${pad(now.getDate())}-${pad(now.getHours())}${pad(now.getMinutes())}${pad(now.getSeconds())}`;
    const filePath = path.join(dir, `diagnostic-${stamp}.txt`);
    fs.writeFileSync(filePath, formatDiagnosticBundle(payload), 'utf8');
    results.bundlePath = filePath;
  } catch (err) {
    results.warning = `Could not write diagnostic bundle: ${err.message}`;
  }

  // 2) Compose and open the mailto link.
  try {
    const code = payload.lastError && payload.lastError.errorCode
      ? payload.lastError.errorCode
      : 'general';
    const version = payload.appVersion || app.getVersion();
    const subject = `Garmin Data Exporter bug — ${code} — v${version}`;
    const body = truncateForMailto(formatMailSummary(payload, results.bundlePath));
    const url = `mailto:${BUG_REPORT_EMAIL}?subject=${encodeURIComponent(subject)}&body=${encodeURIComponent(body)}`;
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
