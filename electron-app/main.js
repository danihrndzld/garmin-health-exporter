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

  try {
    const { GarminConnect } = require('garmin-connect');
    const gc = new GarminConnect({ username: email, password });

    send('info', `Connecting as ${email}…`);
    await gc.login(email, password);
    send('success', 'Login successful.');

    const today = new Date();
    const fmt = (d) => d.toISOString().split('T')[0];
    const dates = Array.from({ length: daysBack }, (_, i) => {
      const d = new Date(today);
      d.setDate(d.getDate() - i);
      return fmt(d);
    });
    const startDate = dates[dates.length - 1];
    const endDate = dates[0];

    send('info', `Pulling ${daysBack} days: ${startDate} → ${endDate}`);

    const result = {
      export_date: endDate,
      date_range: { start: startDate, end: endDate, days: daysBack },
      daily: {},
      aggregated: {},
      activities: [],
      goals: [],
    };

    // Per-day metrics
    for (const d of dates) {
      send('dim', `  ${d}`);
      const daily = {};
      const safe = async (label, fn) => {
        try { return await fn(); }
        catch (e) { send('dim', `    [skip] ${label}: ${e.message}`); return null; }
      };

      daily.heartRate  = await safe('heartRate',  () => gc.getHeartRate(d));
      daily.sleepData  = await safe('sleepData',  () => gc.getSleepData(d));
      daily.steps      = await safe('steps',      () => gc.getSteps(d));
      daily.weight     = await safe('weight',     () => gc.getDailyWeightData(d));
      daily.hydration  = await safe('hydration',  () => gc.getDailyHydration(d));

      result.daily[d] = daily;

      // Report progress
      const idx = dates.indexOf(d) + 1;
      if (!event.sender.isDestroyed())
        event.sender.send('progress', { current: idx, total: daysBack, phase: 'daily' });
    }

    // Aggregated
    send('info', 'Pulling aggregated metrics…');
    const safeAgg = async (label, fn) => {
      try { return await fn(); }
      catch (e) { send('dim', `    [skip] ${label}: ${e.message}`); return null; }
    };

    result.aggregated.userSettings    = await safeAgg('userSettings',    () => gc.getUserSettings());
    result.aggregated.userProfile     = await safeAgg('userProfile',     () => gc.getUserProfile());

    // Activities
    send('info', 'Pulling activities…');
    const activities = await safeAgg('activities', () => gc.getActivities(0, 100)) || [];
    const filtered = activities.filter(a => {
      const d = (a.startTimeLocal || '').split(' ')[0];
      return d >= startDate && d <= endDate;
    });
    send('info', `Found ${filtered.length} activities in range.`);

    for (let i = 0; i < filtered.length; i++) {
      const act = filtered[i];
      const name = act.activityName || act.activityType?.typeKey || 'unknown';
      send('dim', `  [${i + 1}/${filtered.length}] ${(act.startTimeLocal || '').split(' ')[0]} | ${name}`);
      try {
        // getActivity returns the activity summary (same structure as list item, with more fields)
        const details = await gc.getActivity({ activityId: act.activityId });
        act._activityDetail = details;
      } catch (e) {
        send('dim', `    [skip] details: ${e.message}`);
      }
      if (!event.sender.isDestroyed())
        event.sender.send('progress', { current: i + 1, total: filtered.length, phase: 'activities' });
    }
    result.activities = filtered;

    // Save JSON
    const outDir = path.join(outputDir);
    fs.mkdirSync(outDir, { recursive: true });
    const outFile = path.join(outDir, `garmin_health_${endDate}.json`);
    fs.writeFileSync(outFile, JSON.stringify(result, null, 2));
    send('success', `JSON saved → ${path.basename(outFile)}`);

    // Convert JSON → CSVs via Python script
    send('info', 'Converting to CSVs…');
    if (!event.sender.isDestroyed())
      event.sender.send('progress', { current: 1, total: 2, phase: 'csv' });

    const scriptPath = app.isPackaged
      ? path.join(process.resourcesPath, 'scripts', 'json_to_csv.py')
      : path.join(__dirname, 'scripts', 'json_to_csv.py');

    const csvDir = path.join(outDir, `csv_${endDate}`);

    await new Promise((resolve, reject) => {
      const py = spawn('python3', [scriptPath, outFile, outDir]);
      py.stdout.on('data', d => {
        for (const line of d.toString().split('\n').filter(l => l.trim())) {
          send('dim', line.trim());
        }
      });
      py.stderr.on('data', d => {
        const msg = d.toString().trim();
        if (msg) send('warn', msg);
      });
      py.on('close', code => code === 0 ? resolve() : reject(new Error(`python3 exited ${code}`)));
      py.on('error', err => reject(new Error(`Could not run python3: ${err.message}`)));
    });

    if (!event.sender.isDestroyed())
      event.sender.send('progress', { current: 2, total: 2, phase: 'csv' });

    send('success', `Done. ${daysBack} days | ${filtered.length} activities | CSVs → csv_${endDate}/`);
    return { ok: true, path: csvDir };
  } catch (err) {
    return { ok: false, error: err.message };
  }
});
