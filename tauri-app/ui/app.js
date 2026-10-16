(function () {
  'use strict';

  const T = window.__TAURI__;
  const DR = window.DateRange;
  const $ = (id) => document.getElementById(id);
  const store = {
    get: (k, d = null) => { try { return localStorage.getItem('ge_' + k) ?? d; } catch { return d; } },
    set: (k, v) => { try { localStorage.setItem('ge_' + k, v); } catch { /* private mode */ } },
  };

  // ── Platform bridge ─────────────────────────────────────────────────────
  // Every backend call goes to the bundled Go core `garmin-export-core`; in a plain
  // browser (design preview) dev-mock.js stands in for it.
  const platform = T ? {
    async defaults() {
      return {
        outputDir: await T.path.join(await T.path.documentDir(), 'GarminExport'),
        dataDir: await T.path.appDataDir(),
      };
    },
    version: () => T.app.getVersion(),
    chooseDir: (defaultPath) => T.dialog.open({ directory: true, defaultPath }),
    openPath: (p) => T.opener.openPath(p),
    openUrl: (u) => T.opener.openUrl(u),
    reveal: (p) => T.opener.revealItemInDir(p),
    copyText: (t) => T.clipboardManager.writeText(t),
    invoke: (cmd, args) => T.core.invoke(cmd, args),
    listen: (ev, cb) => T.event.listen(ev, (e) => cb(e.payload)),
    onFocus: (cb) => T.window.getCurrentWindow().onFocusChanged(({ payload }) => payload && cb()),
    run(req, onEvent, bin = 'binaries/garmin-export-core') {
      let child = null;
      let killed = false;
      const done = new Promise((resolve) => {
        const cmd = T.shell.Command.sidecar(bin);
        let result = null;
        cmd.stdout.on('data', (line) => {
          let o;
          try { o = JSON.parse(String(line).trim()); } catch { return; }
          if (o.kind === 'result') result = o; else onEvent(o);
        });
        cmd.stderr.on('data', (line) => onEvent({ kind: 'log', type: 'dim', msg: String(line).trim() }));
        cmd.on('error', (e) => resolve({ ok: false, error: String(e) }));
        cmd.on('close', ({ code }) => resolve(result || {
          ok: false, cancelled: killed, error: killed ? 'Cancelled.' : `Core exited with code ${code}.`,
        }));
        cmd.spawn().then((c) => { child = c; return c.write(JSON.stringify(req) + '\n'); })
          .catch((e) => resolve({ ok: false, error: String(e) }));
      });
      return { done, cancel: () => { killed = true; if (child) child.kill(); } };
    },
  } : window.devMock;

  // ── DOM ─────────────────────────────────────────────────────────────────
  const hero = $('hero'), h1 = $('h1'), h2 = $('h2'), headline = h1.parentElement;
  const badge = $('badge'), ringN = $('ring-n'), ringL = $('ring-l'), bar = $('progressbar');
  const cta = $('cta'), ctaLabel = $('cta-label'), ctaKbd = $('cta-kbd');
  const emailEl = $('email'), passEl = $('password'), eye = $('eye');
  const rangeSeg = $('range-seg'), rangeField = $('range-field'), rangeRead = $('range-read');
  const fromEl = $('from'), toEl = $('to'), rangeErr = $('range-err');
  const refreshSeg = $('refresh-seg'), outBtn = $('out'), outPath = $('out-path');
  const clearCacheBtn = $('clear-cache'), openOut = $('open-out');
  const rowsEl = $('rows'), elapsedEl = $('elapsed'), logEl = $('log'), logN = $('log-n');
  const toastEl = $('toast'), llmBtn = $('llm-btn'), sheet = $('sheet');
  document.querySelectorAll('.ring .v').forEach((c) => c.setAttribute('pathLength', '100'));

  // ── State ───────────────────────────────────────────────────────────────
  const PHASES = ['daily', 'aggregated', 'activities', 'csv'];
  const WEIGHT = { daily: 0.45, aggregated: 0.15, activities: 0.35, csv: 0.05 };
  const VERB = { auth: 'signing in', daily: 'daily metrics', aggregated: 'aggregates', activities: 'activities', csv: 'building CSVs', mfp: 'MyFitnessPal' };
  const isMac = /Mac/.test(navigator.platform || navigator.userAgent);
  let paths = { outputDir: null, dataDir: null };
  let days = store.get('days', '30');
  let refresh = store.get('refresh', '3');
  let running = null;   // { cancel, phase, phases: {}, t0 }
  let rangeCheck = { ok: true };
  let lastOutDir = store.get('last_out');
  let pack = null;
  let lang = store.get('llm_lang', 'es');

  // ── Helpers ─────────────────────────────────────────────────────────────
  function toast(msg, ms = 3200) {
    toastEl.textContent = msg;
    toastEl.classList.add('on');
    clearTimeout(toast.t);
    toast.t = setTimeout(() => toastEl.classList.remove('on'), ms);
  }

  function setHeadline(a, b) {
    if (h1.textContent === a && h2.textContent === b) return;
    headline.classList.add('swap');
    setTimeout(() => { h1.textContent = a; h2.textContent = b; headline.classList.remove('swap'); }, 160);
  }

  function countTo(el, to, fmt = (n) => String(Math.round(n))) {
    const from = parseFloat(el.dataset.v || '0') || 0;
    el.dataset.v = to;
    const t0 = performance.now();
    cancelAnimationFrame(el._raf);
    const step = (t) => {
      const k = Math.min(1, (t - t0) / 600);
      el.textContent = fmt(from + (to - from) * (1 - Math.pow(1 - k, 3)));
      if (k < 1) el._raf = requestAnimationFrame(step);
    };
    el._raf = requestAnimationFrame(step);
  }

  const clock = (ms) => {
    const s = Math.floor(ms / 1000);
    return String(Math.floor(s / 60)).padStart(2, '0') + ':' + String(s % 60).padStart(2, '0');
  };
  const short = (ms) => (ms < 60000 ? (ms / 1000).toFixed(1) + 's' : clock(ms));
  const tilde = (p) => (p || '').replace(/^\/Users\/[^/]+/, '~');
  const mean = (xs) => { const v = xs.filter((x) => typeof x === 'number'); return v.length ? v.reduce((a, b) => a + b, 0) / v.length : null; };

  function selectRadio(group, attr, value) {
    group.querySelectorAll('[role="radio"]').forEach((b) => b.setAttribute('aria-checked', String(b.dataset[attr] === value)));
  }

  // ── Range ───────────────────────────────────────────────────────────────
  function spanDays() {
    return days === 'custom' ? (rangeCheck.ok ? rangeCheck.spanDays : null) : parseInt(days, 10);
  }

  function renderRange() {
    selectRadio(rangeSeg, 'days', days);
    const custom = days === 'custom';
    rangeField.toggleAttribute('data-custom', custom);
    if (custom) {
      if (!fromEl.value) {
        const seed = DR.defaultCustomRange({ daysBack: 30 });
        fromEl.value = store.get('from', seed.startDate);
        toEl.value = store.get('to', seed.endDate);
      }
      rangeCheck = DR.validateRange({ startDate: fromEl.value, endDate: toEl.value, maxSpanDays: 90 });
      rangeErr.hidden = rangeCheck.ok;
      rangeErr.textContent = rangeCheck.ok ? '' : rangeCheck.message;
    } else {
      rangeCheck = { ok: true };
      rangeErr.hidden = true;
      const r = DR.defaultCustomRange({ daysBack: parseInt(days, 10) });
      // Two unbreakable halves: a narrow window wraps after the arrow, never inside a date.
      const half = (t) => Object.assign(document.createElement('span'), { textContent: t });
      rangeRead.replaceChildren(half(`${r.startDate} →`), ' ', half(r.endDate));
    }
    renderIdle();
  }

  function renderIdle() {
    if (running) return;
    const n = spanDays();
    ctaLabel.textContent = n ? `Pull ${n} day${n === 1 ? '' : 's'}` : 'Fix the range';
    ctaKbd.textContent = isMac ? '⌘ ↵' : 'Ctrl ↵';
    cta.disabled = !rangeCheck.ok;
    if (hero.dataset.state === 'idle') setHeadline(n ? `${n} days` : 'Hmm.', n ? 'ready to pull.' : 'check the dates.');
  }

  rangeSeg.addEventListener('click', (e) => {
    const b = e.target.closest('[data-days]');
    if (!b || running) return;
    days = b.dataset.days;
    store.set('days', days);
    if (hero.dataset.state !== 'running') hero.dataset.state = 'idle';
    renderRange();
  });
  [fromEl, toEl].forEach((el) => el.addEventListener('input', () => {
    store.set('from', fromEl.value);
    store.set('to', toEl.value);
    renderRange();
  }));

  refreshSeg.addEventListener('click', (e) => {
    const b = e.target.closest('[data-v]');
    if (!b || running) return;
    refresh = b.dataset.v;
    store.set('refresh', refresh);
    selectRadio(refreshSeg, 'v', refresh);
  });

  // ── Accent swatches ─────────────────────────────────────────────────────
  function setAccent(a) {
    document.documentElement.dataset.accent = a;
    selectRadio($('swatches'), 'accent', a);
    store.set('accent', a);
  }
  $('swatches').addEventListener('click', (e) => { const b = e.target.closest('[data-accent]'); if (b) setAccent(b.dataset.accent); });

  // ── Setup fields ────────────────────────────────────────────────────────
  emailEl.value = store.get('email', '');
  emailEl.addEventListener('change', () => store.set('email', emailEl.value.trim()));
  eye.addEventListener('click', () => {
    const show = passEl.type === 'password';
    passEl.type = show ? 'text' : 'password';
    eye.textContent = show ? 'hide' : 'show';
    eye.setAttribute('aria-label', show ? 'Hide password' : 'Show password');
  });

  outBtn.addEventListener('click', async () => {
    const dir = await platform.chooseDir(paths.outputDir);
    if (typeof dir === 'string' && dir) {
      paths.outputDir = dir;
      outPath.textContent = tilde(dir);
      store.set('dir', dir);
    }
  });

  let armTimer = null;
  clearCacheBtn.addEventListener('click', async () => {
    if (!clearCacheBtn.classList.contains('armed')) {
      clearCacheBtn.classList.add('armed');
      clearCacheBtn.textContent = 'confirm?';
      armTimer = setTimeout(disarm, 3000);
      return;
    }
    disarm();
    const res = await platform.run({ cmd: 'clear-cache', dataDir: paths.dataDir }, () => {}).done;
    toast(res.ok ? 'Cache cleared.' : res.error);
  });
  function disarm() { clearTimeout(armTimer); clearCacheBtn.classList.remove('armed'); clearCacheBtn.textContent = 'clear cache'; }
  clearCacheBtn.addEventListener('blur', disarm);

  // ── Log ─────────────────────────────────────────────────────────────────
  const LOG_MAX = 500;
  const logLines = [];
  function log(type, msg, ts) {
    logLines.push({ type, msg: String(msg), ts: ts || new Date().toLocaleTimeString() });
    if (logLines.length > LOG_MAX) logLines.shift();
    const d = document.createElement('div');
    d.className = type || '';
    const t = document.createElement('time');
    t.textContent = ts || new Date().toLocaleTimeString();
    d.append(t, String(msg));
    logEl.append(d);
    while (logEl.childElementCount > LOG_MAX) logEl.firstElementChild.remove();
    logN.textContent = logEl.childElementCount;
    logEl.scrollTop = logEl.scrollHeight;
  }

  // ── Run rows + rings ────────────────────────────────────────────────────
  const row = (p) => rowsEl.querySelector(`[data-phase="${p}"]`);
  const defaultSubs = {};
  rowsEl.querySelectorAll('li').forEach((li) => { defaultSubs[li.dataset.phase] = li.querySelector('.sub').textContent; });

  function setRow(p, st, sub, time) {
    const li = row(p);
    if (st !== undefined) {
      if (st) li.dataset.st = st; else delete li.dataset.st;
    }
    if (sub !== undefined) { const el = li.querySelector('.sub'); el.textContent = sub; el.title = sub; }
    if (time !== undefined) li.querySelector('.t').textContent = time;
  }

  function setRing(p, frac) {
    hero.querySelector(`.ring[data-phase="${p}"] .v`).style.strokeDashoffset = String(100 - Math.max(0, Math.min(1, frac)) * 100);
  }

  function resetRun() {
    rowsEl.querySelectorAll('li').forEach((li) => setRow(li.dataset.phase, null, defaultSubs[li.dataset.phase], '—'));
    [...PHASES, 'mfp'].forEach((p) => { setRing(p, 0); $('lg-' + p).textContent = '—'; });
    ringN.dataset.v = 0;
    ringN.textContent = '—';
    ringL.textContent = 'standby';
    elapsedEl.textContent = '00:00';
    bar.setAttribute('aria-valuenow', '0');
  }

  // What a finished row says; null keeps the last line (auth: "Loaded saved tokens").
  const plural = (n, w) => `${n} ${w}${n === 1 ? '' : 's'}`;
  const SUMMARY = {
    daily: (s) => s.total && plural(s.total, 'day'),
    aggregated: (s) => s.total && plural(s.total, 'metric'),
    activities: (s) => { const n = s.count ?? s.total ?? 0; return n === 0 ? 'No activities' : n === 1 ? '1 activity' : `${n} activities`; },
    csv: (s) => s.files && plural(s.files, 'file'),
  };

  function finishPhase(p) {
    const s = running.phases[p];
    if (!s || s.end) return;
    s.end = performance.now();
    const sum = SUMMARY[p]?.(s);
    setRow(p, s.warns ? 'warn' : 'ok', sum ? sum + (s.warns ? ` · ${plural(s.warns, 'warning')}` : '') : undefined, short(s.end - s.start));
  }

  function enterPhase(p) {
    const r = running;
    if (r.phase === p) return;
    if (r.phase) finishPhase(r.phase);
    r.phase = p;
    r.phases[p] = r.phases[p] || { start: performance.now(), frac: 0 };
    setRow(p, 'run');
  }

  function overall() {
    return PHASES.reduce((sum, p) => sum + WEIGHT[p] * (running.phases[p]?.frac || 0), 0);
  }

  function onProgress({ current, total, phase }) {
    if (!PHASES.includes(phase) || !total) return;
    enterPhase(phase);
    const frac = current / total;
    running.phases[phase].frac = frac;
    running.phases[phase].total = total;
    setRing(phase, frac);
    $('lg-' + phase).textContent = `${current}/${total}`;
    const pct = Math.round(overall() * 100);
    if (phase !== 'csv') { countTo(ringN, current); ringL.textContent = phase; }
    bar.setAttribute('aria-valuenow', String(pct));
    setHeadline(`${pct}%`, `pulling ${VERB[phase]}.`);
  }

  function onLog({ type, msg, ts }) {
    log(type, msg, ts);
    if (running?.phase === 'auth' && /^(Loaded saved tokens|Login successful|\[skip\])/.test(msg)) {
      const skip = msg.startsWith('[skip]');
      if (!skip) setRow('auth', undefined, msg);
      enterPhase('daily');
      if (!skip) return;
    }
    if (!running?.phase) return;
    // The core logs a phase's opening line before that phase's first progress event
    // (and a phase with nothing to fetch, like 0 activities, sends none at all), so
    // these lines move the run to the next row; otherwise they land on the previous one.
    const next = PHASE_START.find(([re]) => re.test(msg))?.[1];
    if (next) enterPhase(next);
    const p = running.phase, s = running.phases[p];
    const found = /^Found (\d+) activities/.exec(msg);
    if (found && p === 'activities') s.count = Number(found[1]);
    if (type === 'warn') s.warns = (s.warns || 0) + 1;
    // Lines with an absolute path ("JSON saved: /Users/…") stay in the log only.
    if (type !== 'dim' && !/(^|\s)\//.test(msg)) setRow(p, undefined, msg);
  }
  const PHASE_START = [[/^Pulling aggregated/, 'aggregated'], [/^Pulling activities/, 'activities'], [/^JSON saved/, 'csv']];

  // ── Export ──────────────────────────────────────────────────────────────
  function lock(on) {
    [emailEl, passEl, eye, outBtn, clearCacheBtn, fromEl, toEl, mfpBtn, mfpOn, garminBtn, garminOn, signInBtn, cancelBtn].forEach((el) => { el.disabled = on; });
    if (!on) { mfpOn.disabled = !mfp.connected; garminOn.disabled = !garmin.connected; }
    rangeSeg.querySelectorAll('button').forEach((b) => { b.disabled = on; });
    refreshSeg.querySelectorAll('button').forEach((b) => { b.disabled = on; });
  }

  async function startExport() {
    if (running) return;
    const useGarmin = usable(garmin, garminOn);
    if (!useGarmin && !usable(mfp, mfpOn)) { toast('Switch on and connect Garmin or MyFitnessPal first.'); return; }
    if (!rangeCheck.ok) { toast(rangeCheck.message); return; }

    const req = {
      cmd: 'export', refreshWindow: parseInt(refresh, 10),
      outputDir: paths.outputDir, dataDir: paths.dataDir,
      ...(days === 'custom' ? { startDate: fromEl.value, endDate: toEl.value } : { daysBack: parseInt(days, 10) }),
    };
    const n = spanDays();

    resetRun();
    logEl.textContent = '';
    logLines.length = 0;
    $('report').hidden = true;
    log('info', days === 'custom' ? `Range ${fromEl.value} → ${toEl.value}` : `Last ${n} days`);
    hero.dataset.state = 'running';
    badge.textContent = 'Running';
    ctaLabel.textContent = 'Cancel';
    ctaKbd.textContent = 'esc';
    openOut.hidden = true;
    closeSheet();
    llmBtn.hidden = true;
    cta.classList.remove('ghost');
    lock(true);
    setHeadline('0%', useGarmin ? 'signing in.' : 'reading diary.');

    running = { phase: null, phases: {}, t0: performance.now() };
    const tick = setInterval(() => {
      const now = performance.now();
      elapsedEl.textContent = clock(now - running.t0);
      const s = running.phases[running.phase];
      if (s && !s.end) row(running.phase).querySelector('.t').textContent = short(now - s.start);
    }, 250);

    const res = useGarmin ? await runGarmin(req, n) : await runWithoutGarmin(req);
    clearInterval(tick);

    if (res.ok) {
      hero.dataset.state = 'done';
      badge.textContent = 'Done';
      setHeadline(`${n} days`, `done in ${short(performance.now() - running.t0)}.`);
      countTo(ringN, n);
      ringL.textContent = 'days';
      store.set('last_warnings', JSON.stringify(pack?.warnings || []));
      toast('Done. CSVs are in ' + tilde(res.path));
    } else {
      if (running.phase) setRow(running.phase, res.cancelled ? 'warn' : 'err', res.error);
      hero.dataset.state = 'error';
      badge.textContent = res.cancelled ? 'Cancelled' : 'Failed';
      setHeadline(res.cancelled ? 'Cancelled.' : 'Stopped.', res.cancelled ? 'cache kept.' : 'see the log.');
      log('error', res.error || 'Unknown error');
      // Only an unexpected failure is a bug: an expired login, a 429 or a 403 is Garmin's normal answer.
      const reportable = !res.errorCode || res.errorCode === 'GARMIN_ERROR' || res.errorCode === 'MFP_ERROR';
      if (!res.cancelled) { toast(res.error || 'Export failed.', 6000); if (reportable) showReport(res.error); }
      if (res.errorCode === 'GARMIN_SESSION') { garmin.expired = garmin.connected; renderGarmin(); }
    }
    running = null;
    lock(false);
    ctaLabel.textContent = 'Pull again';
    ctaKbd.textContent = isMac ? '⌘ ↵' : 'Ctrl ↵';
    arrangeCtas(res.ok);
  }

  // Garmin on: the Go core pulls Garmin, then MyFitnessPal runs; an MFP failure is only a warning row.
  async function runGarmin(req, n) {
    enterPhase('auth');
    const warnings = []; // the pack's "Missing data and warnings" section
    const job = platform.run(req, (e) => {
      if (e.kind === 'progress') return onProgress(e);
      if (e.kind !== 'log') return;
      if (e.type === 'warn') warnings.push(e.msg);
      onLog(e);
    });
    running.cancel = job.cancel;
    const res = await job.done;
    if (!res.ok) return res;
    const files = res.files;
    if (running.phases.csv && files) running.phases.csv.files = files.length;
    finishPhase(running.phase);
    PHASES.forEach((p) => {
      // A phase with nothing to fetch (0 activities) sends no progress: full ring, "0" in the legend.
      // Its opening log line may still have entered the phase, so check for a total, not for the phase.
      if (!running.phases[p]) setRow(p, 'ok', SUMMARY[p]?.({}) || undefined, '—');
      if (!running.phases[p]?.total) $('lg-' + p).textContent = '0';
      setRing(p, 1);
    });
    bar.setAttribute('aria-valuenow', '100');
    log('complete', 'Export complete → ' + res.path);
    lastOutDir = res.path;
    store.set('last_out', res.path);
    if (res.jsonPath) store.set('last_json', res.jsonPath);
    openOut.hidden = false;
    // The hero stays "Running" (and the clock ticks) until MyFitnessPal is done too.
    if (mfpOn.checked) setHeadline(`${n} days`, 'reading diary.');
    let m = null;
    try { m = await runMfp(res.path, { startDate: res.startDate, endDate: res.endDate }); } catch (e) {
      finishPhase('mfp');
      setRow('mfp', 'warn', 'Skipped: ' + (e?.message || e));
      $('lg-mfp').textContent = 'skipped';
      log('warn', 'MyFitnessPal skipped: ' + (e?.message || e));
    }
    // The Go core answers without summary and pack: the previews and the pack stay empty until this summary fills them.
    pack = null;
    document.querySelectorAll('.preview').forEach((el) => el.setAttribute('data-empty', ''));
    const s = await refreshPack({ csvDir: res.path, jsonPath: res.jsonPath, warnings });
    if (s.ok && s.summary) renderSummary(s.summary);
    return res;
  }

  // Garmin off or expired: the Go core only makes the csv folder. MyFitnessPal is the whole run, so its failure fails the run.
  async function runWithoutGarmin(req) {
    if (garminOn.checked) { // switched on but expired: skipped without a request, as runMfp does
      PHASES.forEach((p) => setRow(p, null, 'Skipped', '—'));
      setRow('auth', 'warn', 'Skipped: the Garmin session expired. Reconnect Garmin.', '—');
      log('warn', 'Garmin skipped: the session expired.');
    } else ['auth', ...PHASES].forEach((p) => setRow(p, null, 'Off', '—'));
    const job = platform.run({ ...req, garmin: false }, () => {});
    running.cancel = job.cancel;
    const res = await job.done;
    if (!res.ok) return res;
    pack = null;
    document.querySelectorAll('.preview').forEach((el) => el.setAttribute('data-empty', ''));
    const range = { startDate: res.startDate, endDate: res.endDate };
    const m = await runMfp(res.path, range, true);
    if (!m.ok) return m;
    await refreshPack({ csvDir: res.path, ...range });
    log('complete', 'Export complete → ' + res.path);
    lastOutDir = res.path;
    store.set('last_out', res.path);
    store.set('last_json', ''); // the last Garmin JSON does not belong to this folder
    store.set('last_range', JSON.stringify({ startDate: res.startDate, endDate: res.endDate })); // the pack reloads from it on the next start
    openOut.hidden = false;
    return res;
  }

  function cancelExport() {
    if (running && running.cancel) running.cancel();
  }

  $('setup').addEventListener('submit', (e) => {
    e.preventDefault();
    // Enter in the Garmin form signs in; it must not start a run.
    if (garminForm.contains(document.activeElement)) signIn();
    else running ? cancelExport() : startExport();
  });
  document.addEventListener('keydown', (e) => {
    const typing = /^(INPUT|TEXTAREA)$/.test(document.activeElement?.tagName);
    if (!sheet.hidden) {
      if (e.key === 'Escape') { e.preventDefault(); closeSheet(); }
      return;
    }
    if ((isMac ? e.metaKey : e.ctrlKey) && e.key === 'Enter') { e.preventDefault(); startExport(); }
    if (e.key === 'Escape' && running) cancelExport();
    if (e.key.toLowerCase() === 'l' && !typing && !e.metaKey && !e.ctrlKey && !llmBtn.hidden) { e.preventDefault(); openSheet(); }
  });
  openOut.addEventListener('click', () => lastOutDir && platform.openPath(lastOutDir));

  // ── Preview ─────────────────────────────────────────────────────────────
  function renderSummary(s) {
    const recent = s.days.slice(-14);
    const steps = recent.map((d) => d.steps);
    const max = Math.max(1, ...steps.filter((x) => typeof x === 'number'));
    const bars = $('steps-bars');
    bars.textContent = '';
    recent.forEach((d, i) => {
      const cell = document.createElement('div');
      cell.title = `${d.date} · ${d.steps ?? '—'} steps`;
      const fill = document.createElement('i');
      fill.style.height = ((d.steps || 0) / max) * 100 + '%';
      fill.style.animationDelay = i * 30 + 'ms';
      cell.append(fill);
      bars.append(cell);
    });
    const avg = mean(steps);
    countTo($('steps-total'), avg || 0, (n) => Math.round(n).toLocaleString());
    $('steps-range').textContent = recent.length ? `${recent[0].date.slice(5)} → ${recent.at(-1).date.slice(5)}` : '';

    const week = s.days.slice(-7);
    const score = mean(week.map((d) => d.sleepScore));
    $('sleep-donut').style.setProperty('--p', score || 0);
    countTo($('sleep-score'), score || 0);
    const sec = mean(week.map((d) => d.sleepSec));
    const min = Math.round(sec / 60); // round once, so 6h 59.6m reads 7h 00m, not 6h 60m
    $('st-sleep').textContent = sec ? `${Math.floor(min / 60)}h ${String(min % 60).padStart(2, '0')}m` : '—';
    const rhr = mean(week.map((d) => d.rhr));
    $('st-rhr').textContent = rhr ? Math.round(rhr) + ' bpm' : '—';
    const hrv = mean(week.map((d) => d.hrv));
    $('st-hrv').textContent = hrv ? Math.round(hrv) + ' ms' : '—';

    document.querySelectorAll('.preview').forEach((el) => el.removeAttribute('data-empty'));
  }

  async function loadLastSummary() {
    document.querySelectorAll('.preview').forEach((el) => el.setAttribute('data-empty', ''));
    const jsonPath = store.get('last_json');
    let range = null, warnings = [];
    try { range = JSON.parse(store.get('last_range', 'null')); } catch { /* old or missing */ }
    // A Garmin off run leaves no JSON: its pack comes from the folder and the range.
    if (!jsonPath && !(range && lastOutDir)) return;
    try { warnings = JSON.parse(store.get('last_warnings', '[]')); } catch { /* old or missing */ }
    const req = jsonPath ? { jsonPath, csvDir: lastOutDir, warnings } : { csvDir: lastOutDir, ...range, warnings };
    const res = await platform.run({ cmd: 'summary', ...req }, () => {}).done;
    if (res.ok && res.summary) renderSummary(res.summary);
    pack = (res.ok && res.pack) || null;
    arrangeCtas(false);
  }

  // ── LLM pack ────────────────────────────────────────────────────────────
  // Export writes nothing extra. Each button asks the Go core to build ONE language
  // into its own subfolder (llm-<lang>/ or llm-<lang>-one-file/) and opens it, so
  // dragging the whole folder never sends the same data twice.
  const STR = {
    es: {
      h1: 'Pásaselo a un LLM.', sub: (p) => `${p.days} días · ${p.files.length} archivos.`,
      fits: 'cabe en Claude · 200k', over: 'excede 200k · usa los CSV sueltos', note: 'solo contexto, sin pedido',
      ctx: 'Armar carpeta: contexto + CSV', one: 'Armar un solo archivo',
      open: 'abrir', reveal: 'carpeta del export', files: 'Archivos en la carpeta',
      building: 'Armando…', done: (n) => `Listo · ${n} ${n === 1 ? 'archivo' : 'archivos'}`,
      tCtx: 'Arrastra todo lo de la carpeta al chat.', tOne: 'Arrastra el archivo al chat.', fail: 'No se pudo armar: ',
      emptyH: 'aún no hay paquete.', empty: 'No se pudo leer este export. Haz "Pull again" para generarlo.',
    },
    en: {
      h1: 'Hand it to an LLM.', sub: (p) => `${p.days} days · ${p.files.length} files.`,
      fits: 'fits Claude · 200k', over: 'over 200k · use the loose CSVs', note: 'context only, no ask',
      ctx: 'Build folder: context + CSVs', one: 'Build one file',
      open: 'open', reveal: 'export folder', files: 'Files in the folder',
      building: 'Building…', done: (n) => `Ready · ${n} ${n === 1 ? 'file' : 'files'}`,
      tCtx: 'Drag everything in the folder into the chat.', tOne: 'Drag the file into the chat.', fail: 'Could not build: ',
      emptyH: 'no pack yet.', empty: 'This export could not be read. Hit "Pull again" to build one.',
    },
  };
  const CONTEXT_WINDOW = 200000;
  const others = () => document.querySelectorAll('.setup, .tasks, .preview');

  function arrangeCtas(justFinished) {
    llmBtn.hidden = !lastOutDir;
    const llmFirst = justFinished && !!pack;
    llmBtn.classList.toggle('ghost', !llmFirst);
    cta.classList.toggle('ghost', llmFirst);
  }

  function renderSheet() {
    const t = STR[lang];
    selectRadio($('lang-seg'), 'lang', lang);
    $('sh-h1').textContent = t.h1;
    sheet.toggleAttribute('data-empty', !pack);
    const empty = $('sh-empty');
    empty.hidden = !!pack;
    if (!pack) { $('sh-h2').textContent = t.emptyH; empty.textContent = t.empty; return; }
    const L = pack.langs[lang] || pack.langs.es;
    $('sh-h2').textContent = t.sub(pack);
    const over = L.tokens > CONTEXT_WINDOW;
    $('ctx-meter').classList.toggle('over', over);
    requestAnimationFrame(() => { $('ctx-fill').style.width = Math.min(100, (L.tokens / CONTEXT_WINDOW) * 100) + '%'; });
    $('ctx-tokens').textContent = `≈ ${Math.round(L.tokens / 1000)}k tokens`;
    $('ctx-kb').textContent = `${Math.round(L.bytes / 1024)} KB`;
    $('ctx-fit').textContent = over ? t.over : t.fits;
    $('ctx-name').textContent = `CONTEXT.${lang}.md`;
    $('ctx-note').textContent = t.note;
    const pre = $('ctx-pre');
    pre.textContent = '';
    for (const line of L.context.split('\n')) {
      const el = document.createElement(line.startsWith('#') ? 'b' : 'span');
      el.textContent = line + '\n';
      pre.append(el);
    }
    const files = $('files');
    files.textContent = '';
    const lbl = document.createElement('span');
    lbl.className = 'lbl2';
    lbl.textContent = t.files;
    files.append(lbl);
    pack.files.forEach((f, i) => {
      const chip = document.createElement('span');
      const n = document.createElement('em');
      n.textContent = f.rows;
      chip.append(f.name.replace(/\.csv$/, ''), n);
      chip.style.setProperty('--i', i);
      files.append(chip);
    });
    $('copy-ctx-l').textContent = t.ctx;
    $('copy-ctx-s').textContent = `llm-${lang}/`;
    $('copy-one-l').textContent = t.one;
    $('copy-one-s').textContent = `llm-${lang}-one-file/`;
    $('open-l').textContent = t.open;
    $('reveal-one').textContent = t.reveal;
  }

  function openSheet() {
    if (running || !lastOutDir) return;
    renderSheet();
    sheet.hidden = false;
    others().forEach((el) => { el.inert = true; });
    (pack ? $('copy-ctx') : $('sheet-x')).focus();
  }

  function closeSheet() {
    if (sheet.hidden) return;
    sheet.hidden = true;
    others().forEach((el) => { el.inert = false; });
    $('ctx-fill').style.width = '0';
    llmBtn.focus();
  }

  // Build → open. The button holds a "building" state for at least MIN_BUILD_MS so
  // the motion reads as feedback even when the Go core answers in 50 ms.
  const MIN_BUILD_MS = 700;
  const wait = (ms) => new Promise((r) => setTimeout(r, ms));
  const calm = matchMedia('(prefers-reduced-motion: reduce)');
  function swapText(el, text) {
    el.textContent = text;
    if (!calm.matches) el.animate([{ opacity: 0, transform: 'translateY(4px)' }, { opacity: 1, transform: 'none' }], { duration: 220, easing: 'cubic-bezier(.25,1,.5,1)' });
  }
  async function buildFlow(btn) {
    if (!pack || btn.dataset.state === 'building') return;
    const t = STR[lang], mode = btn.dataset.mode;
    const label = btn.querySelector('.stack > span'), prev = t[mode === 'single' ? 'one' : 'ctx'];
    const live = $('pk-live');
    clearTimeout(btn._reset);
    btn.dataset.state = 'building';
    btn.setAttribute('aria-busy', 'true');
    swapText(label, t.building);
    live.textContent = t.building;
    $('files').classList.remove('packing');
    void $('files').offsetWidth; // restart the chip stagger
    $('files').classList.add('packing');
    const [res] = await Promise.all([
      platform.run({ cmd: 'pack', mode, lang, csvDir: pack.csvDir, startDate: pack.startDate, endDate: pack.endDate, warnings: pack.warnings || [] }, () => {}).done
        .catch((err) => ({ ok: false, error: err?.message || String(err) })),
      wait(MIN_BUILD_MS),
    ]);
    btn.removeAttribute('aria-busy');
    if (res.ok) {
      btn.dataset.state = 'done';
      swapText(label, t.done(res.count));
      live.textContent = label.textContent;
      if (mode === 'single') platform.reveal(res.file); else platform.openPath(res.dir);
      toast(mode === 'single' ? t.tOne : t.tCtx);
    } else {
      btn.dataset.state = 'error';
      swapText(label, prev);
      live.textContent = t.fail + res.error;
      toast(t.fail + res.error, 6000);
    }
    btn._reset = setTimeout(() => { delete btn.dataset.state; swapText(label, STR[lang][mode === 'single' ? 'one' : 'ctx']); }, 1800);
  }

  $('copy-ctx').addEventListener('click', () => buildFlow($('copy-ctx')));
  $('copy-one').addEventListener('click', () => buildFlow($('copy-one')));
  $('reveal-one').addEventListener('click', () => pack && platform.openPath(pack.csvDir));
  sheet.querySelectorAll('[data-url]').forEach((b) => b.addEventListener('click', () => platform.openUrl(b.dataset.url)));
  $('lang-seg').addEventListener('click', (e) => {
    const b = e.target.closest('[data-lang]');
    if (!b) return;
    lang = b.dataset.lang;
    store.set('llm_lang', lang);
    renderSheet();
  });
  llmBtn.addEventListener('click', openSheet);
  $('sheet-x').addEventListener('click', closeSheet);

  // ── Bug report ──────────────────────────────────────────────────────────
  // The Go core scrubs secrets / emails / home paths and builds both links.
  let reportLinks = null;
  async function showReport(error) {
    const res = await platform.run({
      cmd: 'report', error, log: logLines, version,
      os: navigator.userAgent.match(/Mac OS X [\d_]+/)?.[0]?.replace(/_/g, '.') || navigator.platform,
    }, () => {}).done;
    if (!res.ok) return;
    reportLinks = res;
    $('report').hidden = false;
  }
  $('report-gh').addEventListener('click', () => reportLinks && platform.openUrl(reportLinks.issueUrl));
  $('report-mail').addEventListener('click', () => reportLinks && platform.openUrl(reportLinks.mailtoUrl));

  // ── MyFitnessPal ────────────────────────────────────────────────────────
  // Rust owns the sign in window and the session file; the Go core reads it.
  const mfpState = $('mfp-state'), mfpBtn = $('mfp-btn'), mfpOn = $('mfp-on');
  let mfp = { connected: false, expired: false };
  function renderMfp() {
    mfpState.textContent = mfp.expired ? 'Session expired' : mfp.connected ? 'Connected' : 'Not connected';
    mfpBtn.textContent = mfp.expired ? 'reconnect' : mfp.connected ? 'disconnect' : 'connect';
    mfpOn.disabled = !!running || !mfp.connected;
    mfpOn.checked = mfp.connected && store.get('mfp_on', '1') === '1';
    row('mfp').hidden = !mfpOn.checked;
    // The attribute, not el.hidden: the ring is an SVG <g>, which has no hidden property.
    hero.querySelectorAll('[data-phase="mfp"]').forEach((el) => el.toggleAttribute('hidden', !mfpOn.checked));
    rowsEl.classList.toggle('six', mfpOn.checked);
  }
  async function refreshMfp() {
    let s;
    try { s = await platform.invoke('mfp_status'); } catch { s = { connected: false }; }
    // Expired stays until mfp-connected or a disconnect: reconnecting always goes through mfp_connect.
    const keep = mfp.expired && s.connected;
    mfp = { ...s, expired: keep };
    renderMfp();
  }
  mfpBtn.addEventListener('click', async () => {
    try {
      if (mfp.connected && !mfp.expired) { await platform.invoke('mfp_disconnect'); await refreshMfp(); toast('MyFitnessPal disconnected.'); return; }
      toast('Sign in to MyFitnessPal in the new window.');
      await platform.invoke('mfp_connect');
    } catch (err) { toast('MyFitnessPal: ' + (err?.message || err)); }
  });
  mfpOn.addEventListener('change', () => { store.set('mfp_on', mfpOn.checked ? '1' : '0'); renderMfp(); });
  platform.listen('mfp-connected', (s) => { mfp = { ...s, expired: false }; renderMfp(); toast('MyFitnessPal connected.'); });

  // Runs after the Garmin export, or alone with Garmin off (alone = true); the caller decides
  // what a failure means and refreshes the pack.
  async function runMfp(csvDir, range, alone = false) {
    if (!mfpOn.checked) return { ok: true, skipped: true };
    if (!usable(mfp, mfpOn)) { // switched on but expired: skipped without a request
      setRow('mfp', 'warn', 'Skipped: the MyFitnessPal session expired. Reconnect MyFitnessPal.');
      $('lg-mfp').textContent = 'skipped';
      return { ok: true, skipped: true };
    }
    enterPhase('mfp');
    // MFP is outside the Garmin percentage (PHASES/WEIGHT): its own ring, legend and row.
    const onMfpProgress = ({ current, total }) => {
      if (!total) return;
      setRing('mfp', current / total);
      $('lg-mfp').textContent = `${current}/${total}`;
      countTo(ringN, current);
      ringL.textContent = 'diary days';
      setRow('mfp', undefined, `${current} of ${total} days`);
    };
    const job = platform.run({ cmd: 'mfp-export', ...range, dataDir: paths.dataDir, csvDir },
      (e) => (e.kind === 'progress' ? onMfpProgress(e) : e.kind === 'log' && onLog(e)));
    running.cancel = job.cancel;
    const res = await job.done;
    if (res.ok) {
      finishPhase('mfp');
      setRow('mfp', 'ok', `Food logged on ${res.loggedDays} of ${res.days} days`);
      setRing('mfp', 1);
      $('lg-mfp').textContent = `${res.loggedDays}/${res.days}`;
    } else {
      finishPhase('mfp');
      if (res.errorCode === 'MFP_SESSION') { mfp.expired = mfp.connected; renderMfp(); }
      // With Garmin off the caller fails the run with this error; here it would only repeat it.
      $('lg-mfp').textContent = res.cancelled ? 'cancelled' : alone ? 'failed' : 'skipped';
      if (alone) return res;
      setRow('mfp', 'warn', res.cancelled ? 'Cancelled.' : 'Skipped: ' + (res.error || 'unknown error'));
      log('warn', 'MyFitnessPal ' + (res.cancelled ? 'cancelled' : 'skipped: ' + (res.error || 'unknown error')));
    }
    return res;
  }

  // Asks the Go core's `summary` for the pack (and, from the Garmin JSON, the summary) of a finished csv folder.
  // Esc here cancels only the pack refresh: the files are written and the run ends Done.
  async function refreshPack(req) {
    const job = platform.run({ cmd: 'summary', ...req }, () => {});
    running.cancel = job.cancel;
    const s = await job.done;
    if (s.ok) pack = s.pack || pack;
    return s;
  }

  // ── Garmin ──────────────────────────────────────────────────────────────
  // The Go core signs in once and keeps tokens.json; every export runs from it without a password.
  const garminState = $('garmin-state'), garminBtn = $('garmin-btn'), garminOn = $('garmin-on');
  const garminForm = $('garmin-form'), garminErr = $('garmin-err'), signInBtn = $('garmin-signin'), cancelBtn = $('garmin-cancel');
  let garmin = { connected: false, expired: false };
  const usable = (s, sw) => sw.checked && s.connected && !s.expired;
  const goRun = (req) => platform.run(req, () => {}).done.catch((e) => ({ ok: false, error: String(e?.message || e) }));
  function renderGarmin() {
    garminState.textContent = garmin.expired ? 'Session expired' : garmin.connected ? 'Connected' : 'Not connected';
    garminBtn.textContent = garmin.expired ? 'reconnect' : garmin.connected ? 'disconnect' : 'connect';
    garminOn.disabled = !!running || !garmin.connected;
    garminOn.checked = garmin.connected && store.get('garmin_on', '1') === '1';
    if (garmin.connected && !garmin.expired) garminForm.hidden = true;
  }
  function setGarmin(s) {
    garmin = { connected: !!s.connected, expired: !!s.expired, savedAt: s.savedAt };
    renderGarmin();
  }
  function showGarminError(msg) {
    garminErr.textContent = msg;
    garminErr.hidden = !msg;
  }
  async function refreshGarmin() {
    const s = await goRun({ cmd: 'garmin-status', dataDir: paths.dataDir });
    // Expired stays until a sign in or a disconnect, as for MyFitnessPal.
    if (s.ok) setGarmin({ ...s, expired: s.expired || (garmin.expired && s.connected) });
  }
  async function signIn() {
    if (signInBtn.disabled) return; // Enter twice must not send two of Garmin's few sign ins
    const email = emailEl.value.trim(), password = passEl.value;
    if (!email || !password) { showGarminError('Enter your email and password.'); (email ? passEl : emailEl).focus(); return; }
    store.set('email', email);
    const busy = [signInBtn, emailEl, passEl];
    busy.forEach((el) => { el.disabled = true; });
    let res;
    try { res = await goRun({ cmd: 'garmin-connect', email, password, dataDir: paths.dataDir }); } finally { passEl.value = ''; busy.forEach((el) => { el.disabled = false; }); }
    if (!res.ok) { showGarminError(res.error); passEl.focus(); return; }
    showGarminError('');
    setGarmin(res);
    toast('Garmin connected.');
  }
  garminBtn.addEventListener('click', async () => {
    if (garmin.connected && !garmin.expired) {
      const res = await goRun({ cmd: 'garmin-disconnect', dataDir: paths.dataDir });
      if (!res.ok) { toast(res.error); return; }
      setGarmin(res);
      toast('Garmin disconnected.');
      return;
    }
    showGarminError('');
    garminForm.hidden = false;
    emailEl.focus();
  });
  signInBtn.addEventListener('click', signIn);
  cancelBtn.addEventListener('click', () => { garminForm.hidden = true; passEl.value = ''; showGarminError(''); });
  garminOn.addEventListener('change', () => { store.set('garmin_on', garminOn.checked ? '1' : '0'); renderGarmin(); });

  // ── Version + update check ──────────────────────────────────────────────
  const verEl = $('ver');
  let version = '';
  async function checkUpdate() {
    if (verEl.dataset.url) return platform.openUrl(verEl.dataset.url);
    verEl.textContent = 'checking…';
    try {
      const r = await fetch('https://api.github.com/repos/danihrndzld/garmin-health-exporter/releases/latest', { signal: AbortSignal.timeout(8000) });
      if (!r.ok) throw new Error('GitHub ' + r.status);
      const data = await r.json();
      const latest = String(data.tag_name || '').replace(/^v/, '');
      if (latest && latest.localeCompare(version, undefined, { numeric: true }) > 0) {
        verEl.textContent = `v${latest} ↗`;
        verEl.classList.add('new');
        verEl.dataset.url = data.html_url;
      } else {
        verEl.textContent = `v${version} ✓`;
        setTimeout(() => { verEl.textContent = 'v' + version; }, 2500);
      }
    } catch (err) {
      verEl.textContent = 'v' + version;
      toast('Update check failed: ' + err.message);
    }
  }
  verEl.addEventListener('click', checkUpdate);
  verEl.addEventListener('keydown', (e) => { if (e.key === 'Enter' || e.key === ' ') { e.preventDefault(); checkUpdate(); } });

  // ── Boot ────────────────────────────────────────────────────────────────
  (async function boot() {
    setAccent(store.get('accent', 'volt'));
    selectRadio(refreshSeg, 'v', refresh);
    resetRun();
    renderRange();
    paths = await platform.defaults();
    const savedDir = store.get('dir');
    if (savedDir) paths.outputDir = savedDir;
    outPath.textContent = tilde(paths.outputDir);
    version = await platform.version();
    await refreshGarmin(); // before the version shows, so the e2e boot wait also covers the Garmin status
    verEl.textContent = 'v' + version;
    if (lastOutDir) openOut.hidden = false;
    loadLastSummary();
    refreshMfp();
    platform.onFocus(refreshMfp);
    platform.onFocus(refreshGarmin);
  })();
})();
