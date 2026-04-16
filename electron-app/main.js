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
    backgroundColor: '#080c08',
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
  const resolvedOutput = path.resolve(outputDir);
  const home = os.homedir();
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
          event.sender.send('log', {
            type: data.type,
            msg: data.message,
            ts: new Date().toLocaleTimeString(),
          });
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
