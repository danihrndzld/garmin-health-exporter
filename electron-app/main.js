const { app, BrowserWindow, ipcMain, dialog, shell } = require('electron');
const path            = require('path');
const fs              = require('fs');
const os              = require('os');
const { spawn, spawnSync } = require('child_process');

let mainWindow;

// ── uv resolution ─────────────────────────────────────────────────────────────
function resolveUv() {
  const candidates = [
    path.join(os.homedir(), '.local', 'bin', 'uv'),
    path.join(os.homedir(), '.cargo', 'bin', 'uv'),
    '/opt/homebrew/bin/uv',
    '/usr/local/bin/uv',
  ];
  for (const p of candidates) {
    try { fs.accessSync(p, fs.constants.X_OK); return p; } catch {}
  }
  return 'uv'; // fallback: let spawn try PATH
}

function uvOk() {
  const r = spawnSync(resolveUv(), ['--version'], { encoding: 'utf8' });
  return r.status === 0;
}

function brewBin() {
  for (const p of ['/opt/homebrew/bin/brew', '/usr/local/bin/brew']) {
    try { fs.accessSync(p, fs.constants.X_OK); return p; } catch {}
  }
  return 'brew';
}

function cltInstalled() {
  const r = spawnSync('xcode-select', ['-p'], { encoding: 'utf8' });
  return r.status === 0 && r.stdout.trim().length > 0;
}

function patchPath() {
  const extra = [
    path.join(os.homedir(), '.local', 'bin'),
    path.join(os.homedir(), '.cargo', 'bin'),
    '/opt/homebrew/bin',
    '/usr/local/bin',
  ];
  const current = (process.env.PATH || '').split(':');
  const toAdd = extra.filter(p => !current.includes(p));
  if (toAdd.length) process.env.PATH = toAdd.join(':') + ':' + process.env.PATH;
}

// Helper: spawn a process and stream stdout/stderr to a callback
function runStreamed(bin, args, opts, onLine) {
  return new Promise((resolve, reject) => {
    const proc = spawn(bin, args, { env: { ...process.env, ...opts.env }, shell: opts.shell });
    const emit = chunk => {
      for (const line of chunk.toString().split('\n')) {
        const l = line.trim(); if (l) onLine(l);
      }
    };
    proc.stdout.on('data', emit);
    proc.stderr.on('data', emit);
    proc.on('close', code => resolve(code));
    proc.on('error', err => reject(err));
  });
}

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

  // Signal renderer if uv is missing
  mainWindow.webContents.once('did-finish-load', () => {
    patchPath();
    if (!uvOk()) {
      mainWindow.webContents.send('setup-required');
    }
  });
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

// ── IPC: check deps ───────────────────────────────────────────────────────────
ipcMain.handle('check-deps', () => {
  patchPath();
  return { uvOk: uvOk() };
});

// ── IPC: install deps ─────────────────────────────────────────────────────────
ipcMain.handle('install-deps', async (event) => {
  const log = (msg, type = 'dim') => {
    if (!event.sender.isDestroyed())
      event.sender.send('setup-log', { msg, type });
  };

  try {
    patchPath();

    // ── Try curl installer first (no CLT/brew needed) ──────────────────────
    log('Installing uv via official installer…', 'info');
    const curlCode = await runStreamed(
      '/bin/sh', ['-c', 'curl -LsSf https://astral.sh/uv/install.sh | sh'],
      { env: { HOME: os.homedir() } },
      line => log(line)
    );

    patchPath();
    if (uvOk()) {
      log('uv installed successfully.', 'success');
      return { ok: true };
    }

    // ── Fallback: brew ─────────────────────────────────────────────────────
    log('curl installer did not work, trying Homebrew…', 'info');

    const brew = brewBin();
    const brewAvailable = spawnSync(brew, ['--version'], { encoding: 'utf8' }).status === 0;

    if (!brewAvailable) {
      // Need CLT before brew
      if (!cltInstalled()) {
        log('Installing Xcode Command Line Tools — a dialog will appear, click Install.', 'info');
        spawn('xcode-select', ['--install']);

        // Poll until CLT is ready (up to 15 min)
        const deadline = Date.now() + 15 * 60 * 1000;
        while (!cltInstalled()) {
          if (Date.now() > deadline) throw new Error('Xcode CLT install timed out (15 min)');
          log('Waiting for Xcode CLT… check the system dialog.');
          await new Promise(r => setTimeout(r, 5000));
        }
        log('Xcode CLT ready.', 'success');
      }

      log('Installing Homebrew…', 'info');
      await runStreamed(
        '/bin/bash',
        ['-c', 'curl -fsSL https://raw.githubusercontent.com/Homebrew/install/HEAD/install.sh | bash'],
        { env: { NONINTERACTIVE: '1' } },
        line => log(line)
      );
      patchPath();
    }

    log('Installing uv via Homebrew…', 'info');
    await runStreamed(brewBin(), ['install', 'uv'], {}, line => log(line));
    patchPath();

    if (uvOk()) {
      log('uv installed successfully.', 'success');
      return { ok: true };
    }

    throw new Error('uv still not found after install attempts.');

  } catch (err) {
    return { ok: false, error: err.message };
  }
});

// ── IPC: download health data + convert to CSVs ───────────────────────────────
ipcMain.handle('download-health', async (event, { email, password, daysBack, outputDir }) => {
  const send = (type, msg) => {
    if (!event.sender.isDestroyed())
      event.sender.send('log', { type, msg, ts: new Date().toLocaleTimeString() });
  };
  const sendProgress = (current, total, phase) => {
    if (!event.sender.isDestroyed())
      event.sender.send('progress', { current, total, phase });
  };

  try {
    fs.mkdirSync(outputDir, { recursive: true });

    const exportScript = app.isPackaged
      ? path.join(process.resourcesPath, 'scripts', 'garmin_health_export.py')
      : path.join(__dirname, 'scripts', 'garmin_health_export.py');

    const csvScript = app.isPackaged
      ? path.join(process.resourcesPath, 'scripts', 'json_to_csv.py')
      : path.join(__dirname, 'scripts', 'json_to_csv.py');

    const detailsScript = app.isPackaged
      ? path.join(process.resourcesPath, 'scripts', 'download_activity_details.py')
      : path.join(__dirname, 'scripts', 'download_activity_details.py');

    // ── Step 1: Download via Python ───────────────────────────────────────
    let jsonFile = null;
    const uv = resolveUv();

    await new Promise((resolve, reject) => {
      const py = spawn(uv, [
        'run', exportScript,
        '--email',    email,
        '--password', password,
        '--days',     String(daysBack),
        '--output',   outputDir,
      ]);

      py.stdout.on('data', chunk => {
        for (const line of chunk.toString().split('\n')) {
          const l = line.trim();
          if (!l) continue;
          if (l.startsWith('PROGRESS:')) {
            const [, phase, cur, tot] = l.split(':');
            sendProgress(parseInt(cur), parseInt(tot), phase);
          } else if (l.startsWith('JSON_PATH:')) {
            jsonFile = l.slice('JSON_PATH:'.length).trim();
            send('success', `JSON saved → ${path.basename(jsonFile)}`);
          } else {
            send('dim', l);
          }
        }
      });

      py.stderr.on('data', chunk => {
        const msg = chunk.toString().trim();
        if (msg) send('warn', msg);
      });

      py.on('close', code => code === 0 ? resolve() : reject(new Error(`Export script exited ${code}`)));
      py.on('error', err => reject(new Error(`Could not run uv (${uv}): ${err.message}`)));
    });

    if (!jsonFile) throw new Error('Export script did not return a JSON path.');

    // ── Step 2: Convert JSON → CSVs ───────────────────────────────────────
    send('info', 'Converting to CSVs…');
    sendProgress(1, 2, 'csv');

    const today = new Date().toISOString().split('T')[0];
    const csvDir = path.join(outputDir, `csv_${today}`);

    await new Promise((resolve, reject) => {
      const py = spawn('python3', [csvScript, jsonFile, outputDir]);
      py.stdout.on('data', chunk => {
        for (const line of chunk.toString().split('\n').filter(l => l.trim()))
          send('dim', line.trim());
      });
      py.stderr.on('data', chunk => {
        const msg = chunk.toString().trim();
        if (msg) send('warn', msg);
      });
      py.on('close', code => code === 0 ? resolve() : reject(new Error(`CSV script exited ${code}`)));
      py.on('error', err => reject(new Error(`Could not run python3: ${err.message}`)));
    });

    sendProgress(2, 2, 'csv');
    send('success', 'Health CSVs done.');

    // ── Step 3: Activity details (per-type CSVs with detailed metrics) ────
    send('info', 'Downloading activity details…');

    await new Promise((resolve, reject) => {
      const py = spawn(uv, [
        'run', detailsScript,
        '--email',    email,
        '--password', password,
        '--json',     jsonFile,
        '--output',   outputDir,
      ]);

      py.stdout.on('data', chunk => {
        for (const line of chunk.toString().split('\n')) {
          const l = line.trim();
          if (!l) continue;
          if (l.startsWith('PROGRESS:')) {
            const [, cur, tot] = l.split(':');
            sendProgress(parseInt(cur), parseInt(tot), 'activity-details');
          } else {
            send('dim', l);
          }
        }
      });

      py.stderr.on('data', chunk => {
        const msg = chunk.toString().trim();
        if (msg) send('warn', msg);
      });

      py.on('close', code => code === 0 ? resolve() : reject(new Error(`Activity details script exited ${code}`)));
      py.on('error', err => reject(new Error(`Could not run uv: ${err.message}`)));
    });

    send('success', `Done. ${daysBack} days | CSVs → csv_${today}/`);
    return { ok: true, path: csvDir };

  } catch (err) {
    return { ok: false, error: err.message };
  }
});
