// Browser-only stand-in for the Tauri bridge so the UI can be previewed with
// `open ui/index.html`. Inside the app window.__TAURI__ exists and this is inert.
(function () {
  if (window.__TAURI__) return;
  const sleep = (ms) => new Promise((r) => setTimeout(r, ms));
  const fakeSummary = () => {
    const days = [];
    for (let i = 29; i >= 0; i--) {
      const d = new Date(Date.now() - i * 864e5);
      days.push({ date: d.toISOString().slice(0, 10), steps: 4000 + Math.round(Math.random() * 9000),
        rhr: 50 + Math.round(Math.random() * 6), sleepScore: 65 + Math.round(Math.random() * 25),
        sleepSec: 6 * 3600 + Math.round(Math.random() * 7200), hrv: 40 + Math.round(Math.random() * 20) });
    }
    return { days, activities: 41, types: { running: 12, walking: 20, strength_training: 9 } };
  };
  const fakePack = () => {
    const files = [['activities', 59], ['body_battery', 60], ['caminar', 26], ['correr', 6], ['correr_laps', 24], ['daily_summary', 60], ['gym', 27], ['hrv', 60], ['sleep', 60], ['training', 60]]
      .map(([n, rows]) => ({ name: n + '.csv', rows }));
    const ctx = (es) => [es ? '# Datos de Garmin Connect' : '# Garmin Connect data', '', (es ? 'Rango' : 'Range') + ': 2026-09-14 → 2026-10-13 (30 ' + (es ? 'días' : 'days') + ')', '',
      es ? '## Archivos' : '## Files', ...files.map((f) => `- \`${f.name}\`: ${f.rows} ${es ? 'filas' : 'rows'}.`), '', es ? '## Faltantes y avisos' : '## Missing data and warnings', '- hill_score skipped (404)'].join('\n');
    const dir = '/Users/you/Documents/GarminExport/csv_2026-10-13';
    const lang = (l) => ({ context: ctx(l === 'es'), bytes: 121000, tokens: 40300, singleName: `garmin_2026-09-14_2026-10-13.${l}.md` });
    return { csvDir: dir, startDate: '2026-09-14', endDate: '2026-10-13', days: 30, files, warnings: ['hill_score skipped (404)'], langs: { es: lang('es'), en: lang('en') } };
  };
  window.devMock = {
    defaults: async () => ({ outputDir: '/Users/you/Documents/GarminExport', dataDir: '/tmp' }),
    version: async () => '4.0.0',
    chooseDir: async () => null,
    openPath: async () => {},
    openUrl: async (u) => window.open(u),
    reveal: async () => {},
    copyText: (t) => navigator.clipboard.writeText(t),
    invoke: async (cmd) => {
      if (cmd === 'mfp_connect') { localStorage.setItem('mock_mfp', String(Date.now())); localStorage.removeItem('mock_mfp_expired'); window.dispatchEvent(new CustomEvent('mock-mfp', { detail: { connected: true, saved_at: Number(localStorage.getItem('mock_mfp')), session_expires: null } })); }
      if (cmd === 'mfp_disconnect') localStorage.removeItem('mock_mfp');
      return { connected: !!localStorage.getItem('mock_mfp'), saved_at: Number(localStorage.getItem('mock_mfp')) || 0, session_expires: null };
    },
    listen: (ev, cb) => window.addEventListener('mock-' + ev.replace('-connected', ''), (e) => cb(e.detail)),
    onFocus: (cb) => window.addEventListener('focus', cb),
    run(req, onEvent) {
      let cancelled = false;
      const ts = () => new Date().toLocaleTimeString();
      const done = (async () => {
        if (req.cmd === 'summary') return { ok: true, summary: fakeSummary(), pack: fakePack() };
        if (req.cmd === 'pack') {
          await sleep(180);
          if (localStorage.getItem('mock_pack_fail')) return { ok: false, error: 'EACCES: permission denied' };
          const dir = `${req.csvDir}/llm-${req.lang}${req.mode === 'single' ? '-one-file' : ''}`;
          return { ok: true, dir, file: req.mode === 'single' ? `${dir}/garmin_${req.startDate}_${req.endDate}.${req.lang}.md` : undefined, count: req.mode === 'single' ? 1 : 11 };
        }
        if (req.cmd === 'mfp-export') {
          if (localStorage.getItem('mock_mfp_expired')) return { ok: false, errorCode: 'MFP_SESSION', error: 'MyFitnessPal session expired' };
          for (let i = 1; i <= 3; i++) { if (cancelled) return { ok: false, cancelled: true, error: 'Cancelled.' }; await sleep(1000); onEvent({ kind: 'progress', phase: 'mfp', current: i * 10, total: 30 }); }
          return { ok: true, days: 30, loggedDays: 17, files: ['mfp_food.csv', 'mfp_daily.csv', 'mfp_exercise.csv'] };
        }
        if (req.cmd === 'report') return { ok: true, issueUrl: 'https://github.com/x/issues/new', mailtoUrl: 'mailto:x' };
        if (req.cmd !== 'export') return { ok: true };
        if (req.email === 'fail@x.com') {
          onEvent({ kind: 'log', type: 'warn', msg: '[skip] endurance_score: Empty response body [endurance_score] — Garmin returned HTTP 204 with no content. Usually no data for that day yet (the watch has not synced) or a metric this device does not record.', ts: ts() });
          await sleep(300);
          return { ok: false, error: 'Auth failed: Account locked — unlock at connect.garmin.com' };
        }
        onEvent({ kind: 'log', type: 'dim', msg: 'Loaded saved tokens', ts: ts() });
        await sleep(300);
        onEvent({ kind: 'log', type: 'warn', msg: '[skip] endurance_score: Empty response body [endurance_score] — Garmin returned HTTP 204 with no content. Usually no data for that day yet (the watch has not synced) or a metric this device does not record.', ts: ts() });
        await sleep(300);
        // Same log lines as electron-app/garmin/exporter.js, in the same order around the progress events.
        // localStorage.mock_acts = '0' reproduces a range with no activities (no activities progress at all).
        const nDays = req.daysBack || 22, nActs = Number(localStorage.getItem('mock_acts') ?? 41);
        const say = (type, msg) => onEvent({ kind: 'log', type, msg, ts: ts() });
        const plan = [['daily', nDays, `Pulling ${nDays} days`], ['aggregated', 12, 'Pulling aggregated metrics...'], ['activities', nActs, 'Pulling activities...'], ['csv', 1, 'JSON saved: /Users/you/Documents/GarminExport/garmin_health.json']];
        for (const [phase, total, opening] of plan) {
          say(phase === 'csv' ? 'success' : 'info', opening);
          if (phase === 'activities') say('info', `Found ${total} activities`);
          for (let i = 1; i <= total; i++) {
            if (cancelled) return { ok: false, cancelled: true, error: 'Cancelled.' };
            onEvent({ kind: 'progress', phase, current: i, total });
            if (phase === 'aggregated' && i === 9) say('warn', 'hill_score skipped (404)');
            await sleep(phase === 'activities' ? 70 : 110);
          }
        }
        say('success', 'CSVs written to /Users/you/Documents/GarminExport/csv_2026-10-13');
        say('success', `Done. ${nDays} days | ${nActs} activities`);
        return { ok: true, path: '/Users/you/Documents/GarminExport/csv_2026-10-13', jsonPath: '/Users/you/x.json', summary: fakeSummary(), pack: fakePack() };
      })();
      return { done, cancel: () => { cancelled = true; } };
    },
  };
})();
