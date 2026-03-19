const { app, BrowserWindow, ipcMain, dialog, shell } = require('electron');
const path         = require('path');
const fs           = require('fs');
const os           = require('os');
const { spawn }    = require('child_process');

let mainWindow;

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

// ── IPC: choose output directory ─────────────────────────────────────────────
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

    // ── Step 1: Download via Python (full 22-endpoint suite) ─────────────────
    let jsonFile = null;

    await new Promise((resolve, reject) => {
      const py = spawn('uv', [
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

          // Structured progress lines
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
      py.on('error', err => reject(new Error(`Could not run uv: ${err.message}`)));
    });

    if (!jsonFile) throw new Error('Export script did not return a JSON path.');

    // ── Step 2: Convert JSON → CSVs ──────────────────────────────────────────
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
    send('success', `Done. ${daysBack} days exported | CSVs → csv_${today}/`);
    return { ok: true, path: csvDir };

  } catch (err) {
    return { ok: false, error: err.message };
  }
});
