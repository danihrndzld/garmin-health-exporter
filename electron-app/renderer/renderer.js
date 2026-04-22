(function () {
  'use strict';

  // ── State ────────────────────────────────────────────────────────────────
  let outputDir      = localStorage.getItem('garmin_dir') || null;
  let lastOutputPath = null;
  let isRunning      = false;

  // ── DOM ──────────────────────────────────────────────────────────────────
  const $ = id => document.getElementById(id);
  const emailEl      = $('email');
  const passEl       = $('password');
  const eyeBtn       = $('eye-btn');
  const daysSlider   = $('days-slider');
  const daysVal      = $('days-val');
  const refreshSel   = $('refresh-window');
  const dirDisplay   = $('dir-display');
  const chooseDirBtn = $('choose-dir-btn');
  const btnHealth    = $('btn-health');
  const clearCacheBtn= $('clear-cache-btn');
  const logEl        = $('log');
  const clearBtn     = $('clear-btn');
  const bugReportBtn = $('bug-report-btn');
  const progressWrap = $('progress-wrap');
  const progressPhase= $('progress-phase');
  const progressPct  = $('progress-pct');
  const progressFill = $('progress-fill');
  const resultBanner = $('result-banner');
  const statusDot    = $('status-dot');
  const statusLabel  = $('status-label');
  const statusPill   = $('status-pill');
  const sbStatus     = $('sb-status');
  const openSection  = $('open-section');
  const openFolderBtn= $('open-folder-btn');
  const sbLast       = $('sb-last');
  const sbLastrun    = $('sb-lastrun');
  const sbLastrunSep = $('sb-lastrun-sep');
  const sbLastrunVal = $('sb-lastrun-val');
  const welcomeEl    = $('welcome');
  const credsForm    = $('creds-form');
  const rangeSection = $('range-section');
  const rangeModeDaysBtn   = $('range-mode-days');
  const rangeModeCustomBtn = $('range-mode-custom');
  const rangeFromEl  = $('range-from');
  const rangeToEl    = $('range-to');
  const rangeSpanEl  = $('range-span');
  const rangeErrorEl = $('range-error');
  const DR = (typeof window !== 'undefined' && window.DateRange) ? window.DateRange : null;

  // ── Init saved values ────────────────────────────────────────────────────
  const savedEmail = localStorage.getItem('garmin_email');
  if (savedEmail) emailEl.value = savedEmail;

  // Security: clear any previously stored password
  localStorage.removeItem('garmin_pass');

  // Restore saved refresh window (default 3)
  const savedRefresh = localStorage.getItem('garmin_refresh_window');
  if (savedRefresh) refreshSel.value = savedRefresh;

  if (outputDir) {
    dirDisplay.textContent = outputDir;
  } else {
    window.garmin.defaultOutputDir().then(d => {
      dirDisplay.textContent = d;
    });
  }

  // Update slider gradient on init
  function updateSliderGradient(slider) {
    const pct = ((slider.value - slider.min) / (slider.max - slider.min)) * 100;
    slider.style.background = `linear-gradient(to right, var(--red-mid) ${pct}%, var(--border-mid) ${pct}%)`;
  }
  updateSliderGradient(daysSlider);

  // ── Log state (for bug reports) ───────────────────────────────────────────
  const LOG_BUFFER_MAX = 200;
  const logBuffer = [];
  let lastError = null;

  function recordLog(type, msg, ts, errorCode, meta) {
    const entry = { type, msg: String(msg), ts: ts || new Date().toLocaleTimeString() };
    if (errorCode) entry.errorCode = errorCode;
    if (meta) entry.meta = meta;
    logBuffer.push(entry);
    if (logBuffer.length > LOG_BUFFER_MAX) logBuffer.shift();
    // Only capture genuine failures. Benign `warn` lines (e.g., per-endpoint
    // skips) no longer overwrite the root-cause error the user is reporting.
    const isTerminal = errorCode === 'AUTH'
      || errorCode === 'HTTP_4XX'
      || errorCode === 'HTTP_5XX'
      || errorCode === 'BUILD_URL'
      || errorCode === 'UNKNOWN_ENDPOINT';
    if (type === 'error' || isTerminal) {
      lastError = entry;
    }
  }

  // ── Helpers ──────────────────────────────────────────────────────────────
  function appendLog(type, msg, ts, errorCode, meta) {
    if (welcomeEl) welcomeEl.classList.add('hidden');
    recordLog(type, msg, ts, errorCode, meta);
    const entry = document.createElement('div');
    entry.className = `log-entry log-${type}`;
    const tsEl = document.createElement('span');
    tsEl.className = 'log-ts';
    tsEl.textContent = ts || new Date().toLocaleTimeString();
    const msgEl = document.createElement('span');
    msgEl.className = 'log-msg';
    msgEl.textContent = String(msg);
    entry.appendChild(tsEl);
    entry.appendChild(msgEl);
    logEl.appendChild(entry);
    logEl.scrollTop = logEl.scrollHeight;
  }

  function setConnStatus(state) {
    // Preserve .running (driven by setRunning) across connection changes.
    const wasRunning = statusDot.classList.contains('running');
    statusDot.className   = (state || '') + (wasRunning ? ' running' : '');
    statusLabel.className = state || '';
    statusPill.className  = 'status-pill ' + (state || '');
    statusLabel.textContent = state === 'connected' ? 'ONLINE' : state === 'error' ? 'ERROR' : 'OFFLINE';
  }

  const lockableInputs = [emailEl, passEl, daysSlider, refreshSel, chooseDirBtn, clearCacheBtn];

  function setRunning(val) {
    isRunning = val;
    btnHealth.disabled = val;
    btnHealth.classList.toggle('running', val);
    const label = btnHealth.querySelector('.btn-label');
    if (label) label.textContent = val ? 'Exporting…' : 'Download Health Data';
    lockableInputs.forEach(el => { if (el) el.disabled = val; });
    sbStatus.textContent = val ? 'RUNNING' : 'IDLE';
    progressWrap.classList.toggle('active', val);
    statusDot.classList.toggle('running', val);
    if (!val) {
      progressFill.style.transform = 'scaleX(0)';
      progressFill.setAttribute('aria-valuenow', '0');
      lastPhase = null;
      progressPhase.classList.remove('phase-settling');
      // Re-assert range validity: setRunning(false) just cleared btn.disabled.
      if (rangeMode === 'custom' && lastRangeCheck && lastRangeCheck.ok === false) {
        btnHealth.disabled = true;
      }
    }
  }

  // ── Last run (statusbar) ─────────────────────────────────────────────────
  let lastRunAt = null;
  let lastRunTimer = null;

  function formatAgo(ms) {
    const s = Math.max(0, Math.floor(ms / 1000));
    if (s < 10) return 'just now';
    if (s < 60) return s + 's ago';
    const m = Math.floor(s / 60);
    if (m < 60) return m + 'm ago';
    const h = Math.floor(m / 60);
    if (h < 24) return h + 'h ago';
    return Math.floor(h / 24) + 'd ago';
  }

  function renderLastRun() {
    if (!lastRunAt) return;
    sbLastrunVal.textContent = formatAgo(Date.now() - lastRunAt);
  }

  function markLastRun() {
    lastRunAt = Date.now();
    sbLastrun.hidden = false;
    sbLastrunSep.hidden = false;
    renderLastRun();
    if (lastRunTimer) clearInterval(lastRunTimer);
    lastRunTimer = setInterval(renderLastRun, 15000);
  }

  function setOutputPath(p, isDir) {
    lastOutputPath = p;
    const short = p.split('/').slice(-2).join('/');
    sbLast.textContent = '…/' + short;
    sbLast.title = p;
    sbLast.setAttribute('aria-label', 'Open last output folder: ' + p);
    openSection.style.display = '';
    const openFolder = () => window.garmin.openFolder(isDir ? p : p.replace(/\/[^/]+$/, ''));
    openFolderBtn.onclick = openFolder;
    sbLast.onclick = openFolder;
    sbLast.onkeydown = e => { if (e.key === 'Enter' || e.key === ' ') { e.preventDefault(); openFolder(); } };
  }

  function makeBannerIcon(kind) {
    const svg = document.createElementNS('http://www.w3.org/2000/svg', 'svg');
    svg.setAttribute('viewBox', '0 0 16 16');
    svg.setAttribute('aria-hidden', 'true');
    const path = document.createElementNS('http://www.w3.org/2000/svg', 'path');
    path.setAttribute('d', kind === 'ok' ? 'M3 8.5 L6.5 12 L13 4' : 'M4 4 L12 12 M12 4 L4 12');
    svg.appendChild(path);
    return svg;
  }

  function showBanner(ok, msg) {
    resultBanner.className = `active ${ok ? 'success' : 'error'}`;
    resultBanner.textContent = '';
    resultBanner.appendChild(makeBannerIcon(ok ? 'ok' : 'err'));
    const text = document.createElement('span');
    text.textContent = msg;
    resultBanner.appendChild(text);
    resultBanner.setAttribute('tabindex', '0');
    resultBanner.setAttribute('role', 'alert');
  }

  resultBanner.addEventListener('keydown', e => {
    if (e.key === 'Enter' || e.key === ' ') {
      e.preventDefault();
      resultBanner.click();
    }
  });

  // ── Date range: mode toggle + custom range ─────────────────────────────
  let rangeMode = localStorage.getItem('garmin_date_mode') === 'custom' ? 'custom' : 'daysBack';
  let customSeeded = false;
  let lastRangeCheck = { ok: true };

  function applyRangeMode(mode) {
    rangeMode = mode;
    rangeSection.setAttribute('data-mode', mode);
    rangeModeDaysBtn.setAttribute('aria-checked', mode === 'daysBack' ? 'true' : 'false');
    rangeModeCustomBtn.setAttribute('aria-checked', mode === 'custom' ? 'true' : 'false');
    localStorage.setItem('garmin_date_mode', mode);
    if (mode === 'custom') {
      if (!customSeeded && DR) {
        const days = parseInt(daysSlider.value, 10) || 7;
        const seed = DR.defaultCustomRange({ daysBack: days });
        rangeFromEl.value = seed.startDate;
        rangeToEl.value   = seed.endDate;
        customSeeded = true;
      }
      updateCustomRangeUI();
    } else {
      // Leaving custom mode — clear error so it doesn't gate Export in days mode.
      rangeErrorEl.hidden = true;
      rangeErrorEl.textContent = '';
      rangeSection.classList.remove('range-invalid');
      lastRangeCheck = { ok: true };
      btnHealth.disabled = isRunning;
    }
  }

  function updateCustomRangeUI() {
    if (!DR) return;
    const startDate = rangeFromEl.value;
    const endDate   = rangeToEl.value;
    const check = DR.validateRange({ startDate, endDate, maxSpanDays: 90 });
    lastRangeCheck = check;
    if (check.ok) {
      rangeSpanEl.textContent = DR.formatSpan({ startDate, endDate });
      rangeErrorEl.hidden = true;
      rangeErrorEl.textContent = '';
      rangeSection.classList.remove('range-invalid');
      btnHealth.disabled = isRunning;
    } else {
      rangeSpanEl.textContent = '';
      rangeErrorEl.textContent = check.message;
      rangeErrorEl.hidden = false;
      rangeSection.classList.add('range-invalid');
      btnHealth.disabled = true;
    }
  }

  if (rangeModeDaysBtn && rangeModeCustomBtn) {
    rangeModeDaysBtn.addEventListener('click',   () => applyRangeMode('daysBack'));
    rangeModeCustomBtn.addEventListener('click', () => applyRangeMode('custom'));
    applyRangeMode(rangeMode);
  }
  if (rangeFromEl) rangeFromEl.addEventListener('input', updateCustomRangeUI);
  if (rangeToEl)   rangeToEl.addEventListener('input',   updateCustomRangeUI);

  function getOpts() {
    const base = {
      email:         emailEl.value.trim(),
      password:      passEl.value,
      refreshWindow: parseInt(refreshSel.value, 10),
      outputDir:     outputDir || dirDisplay.textContent || '.',
    };
    if (rangeMode === 'custom') {
      return { ...base, startDate: rangeFromEl.value, endDate: rangeToEl.value };
    }
    return { ...base, daysBack: parseInt(daysSlider.value, 10) };
  }

  function validate() {
    const opts = getOpts();
    if (!opts.email || !opts.password) { appendLog('error', 'Email and password are required.'); return false; }
    if (rangeMode === 'custom' && !lastRangeCheck.ok) {
      appendLog('error', lastRangeCheck.message || 'Invalid date range.');
      return false;
    }
    return true;
  }

  // ── Slider ───────────────────────────────────────────────────────────────
  daysSlider.addEventListener('input', () => {
    daysVal.textContent = daysSlider.value;
    daysSlider.setAttribute('aria-valuenow', daysSlider.value);
    daysSlider.setAttribute('aria-valuetext', daysSlider.value + ' days');
    updateSliderGradient(daysSlider);
  });

  // ── Eye toggle ────────────────────────────────────────────────────────────
  const eyeOpenSvg  = eyeBtn.querySelector('.icon-eye');
  const eyeCloseSvg = eyeBtn.querySelector('.icon-eye-off');
  eyeBtn.addEventListener('click', () => {
    const show = passEl.type === 'password';
    passEl.type = show ? 'text' : 'password';
    eyeOpenSvg.hidden  = show;
    eyeCloseSvg.hidden = !show;
    eyeBtn.setAttribute('aria-label', show ? 'Hide password' : 'Show password');
  });

  // ── Choose dir ────────────────────────────────────────────────────────────
  chooseDirBtn.addEventListener('click', async () => {
    const dir = await window.garmin.chooseDir();
    if (dir) {
      outputDir = dir;
      dirDisplay.textContent = dir;
      localStorage.setItem('garmin_dir', dir);
    }
  });

  // ── Clear log ─────────────────────────────────────────────────────────────
  clearBtn.addEventListener('click', () => {
    // Remove all log entries; keep welcome node in place.
    Array.from(logEl.querySelectorAll('.log-entry')).forEach(n => n.remove());
    // Also clear the bug-report buffer so "Clear log" is truthful.
    logBuffer.length = 0;
    lastError = null;
    resultBanner.className = '';
    if (welcomeEl) welcomeEl.classList.remove('hidden');
  });

  // ── Save email on change (password is never persisted) ─────────────────
  emailEl.addEventListener('change', () => {
    localStorage.setItem('garmin_email', emailEl.value.trim());
  });

  // ── Refresh window ──────────────────────────────────────────────────────
  refreshSel.addEventListener('change', () => {
    localStorage.setItem('garmin_refresh_window', refreshSel.value);
  });

  // ── Clear cache (two-step confirm) ───────────────────────────────────────
  const CACHE_ARM_MS = 3000;
  let cacheArmTimer = null;
  const cacheIdleLabel  = 'Clear cache';
  const cacheArmedLabel = 'Confirm clear?';

  function disarmCache() {
    clearCacheBtn.classList.remove('armed');
    clearCacheBtn.textContent = cacheIdleLabel;
    if (cacheArmTimer) { clearTimeout(cacheArmTimer); cacheArmTimer = null; }
  }

  clearCacheBtn.addEventListener('click', async () => {
    if (!clearCacheBtn.classList.contains('armed')) {
      clearCacheBtn.classList.add('armed');
      clearCacheBtn.textContent = cacheArmedLabel;
      cacheArmTimer = setTimeout(disarmCache, CACHE_ARM_MS);
      return;
    }
    disarmCache();
    await window.garmin.clearCache();
    appendLog('success', 'Cache cleared.');
  });

  clearCacheBtn.addEventListener('blur', disarmCache);

  // ── IPC listeners ─────────────────────────────────────────────────────────
  let lastPhase = null;
  function progressHandler({ current, total, phase }) {
    const pct = Math.round((current / total) * 100);
    progressFill.style.transform = 'scaleX(' + (pct / 100) + ')';
    progressFill.setAttribute('aria-valuenow', pct);
    progressPct.textContent  = pct + '%';
    const labels = { daily: 'Daily Metrics', activities: 'Activities', csv: 'Building CSVs', 'activity-details': 'Activity Details' };
    const phaseLabel = (labels[phase] || phase) + ' — ' + current + '/' + total;
    // Mechanical re-lock on channel change: defocus then snap back crisp.
    if (phase !== lastPhase && lastPhase !== null) {
      progressPhase.classList.add('phase-settling');
      // Text swap happens mid-blur so the eye can't read a half-rendered frame.
      requestAnimationFrame(() => {
        progressPhase.textContent = phaseLabel;
        requestAnimationFrame(() => progressPhase.classList.remove('phase-settling'));
      });
    } else {
      progressPhase.textContent = phaseLabel;
    }
    lastPhase = phase;
    progressFill.setAttribute('aria-label', 'Export progress: ' + phaseLabel + ' (' + pct + '%)');
  }

  function logHandler({ type, msg, ts, errorCode, meta }) {
    appendLog(type, msg, ts, errorCode, meta);
  }

  window.garmin.onProgress(progressHandler);
  window.garmin.onLog(logHandler);

  // ── Download Health Data + Convert to CSVs ───────────────────────────────
  async function runHealthDownload(ev) {
    if (ev) ev.preventDefault();
    if (isRunning || !validate()) return;
    // Fire-confirm burst — 180ms of glow before the button locks into running.
    btnHealth.classList.add('firing');
    setTimeout(() => btnHealth.classList.remove('firing'), 200);
    resultBanner.className = '';
    setRunning(true);
    setConnStatus('');

    const opts = getOpts();
    const startMsg = opts.startDate
      ? 'Starting download — ' + opts.startDate + ' → ' + opts.endDate + '…'
      : 'Starting download — ' + opts.daysBack + ' days back…';
    appendLog('info', startMsg);

    const res = await window.garmin.downloadHealth(opts);
    setRunning(false);

    if (res.ok) {
      setConnStatus('connected');
      setOutputPath(res.path, true);
      markLastRun();
      // Delight: terminal-handshake line before the banner.
      const completeMsg = opts.startDate
        ? 'Export complete · ' + opts.startDate + ' → ' + opts.endDate
        : 'Export complete · ' + opts.daysBack + ' days captured';
      appendLog('complete', completeMsg);
      showBanner(true, 'Done — click to open output folder');
    } else {
      setConnStatus('error');
      appendLog('error', res.error || 'Unknown error');
      showBanner(false, res.error || 'Download failed');
    }
  }

  btnHealth.addEventListener('click', runHealthDownload);
  if (credsForm) credsForm.addEventListener('submit', runHealthDownload);

  // ── Delight: platform-aware shortcut hint + global ⌘/Ctrl+Enter ─────────
  const isMac = /Mac|iPhone|iPad/.test(navigator.platform || navigator.userAgent || '');
  const kbdMod = document.getElementById('kbd-hint-mod');
  if (kbdMod) kbdMod.textContent = isMac ? '⌘' : 'Ctrl';

  document.addEventListener('keydown', (e) => {
    const mod = isMac ? e.metaKey : e.ctrlKey;
    if (mod && e.key === 'Enter' && !isRunning) {
      e.preventDefault();
      runHealthDownload();
    }
  });

  // ── Bug report ───────────────────────────────────────────────────────────
  if (bugReportBtn) {
    bugReportBtn.addEventListener('click', async () => {
      bugReportBtn.disabled = true;
      const originalLabel = bugReportBtn.textContent;
      bugReportBtn.textContent = 'Preparing…';
      try {
        const appVersion = await window.garmin.getVersion();
        const payload = {
          appVersion,
          lastError,
          recentLog: logBuffer.slice(),
        };
        const res = await window.garmin.sendBugReport(payload);
        if (res && res.bundlePath) {
          appendLog('dim', 'Diagnostic bundle: ' + res.bundlePath);
        }
        if (res && res.warning) {
          appendLog('warn', res.warning);
        }
        if (res && res.truncated) {
          appendLog('dim', 'Mail body was truncated to fit client URL limit — please attach the bundle file.');
        }
        if (res && res.ok === false) {
          const recipient = res.recipient ? ' Email the bundle to ' + res.recipient + ' manually.' : '';
          appendLog('warn', (res.error || 'Could not open mail client.') + recipient);
        }
      } catch (err) {
        appendLog('warn', 'Bug report failed: ' + (err && err.message ? err.message : String(err)));
      } finally {
        bugReportBtn.textContent = originalLabel;
        bugReportBtn.disabled = false;
      }
    });
  }

  // ── Version + Update check ───────────────────────────────────────────────
  const updateBtn   = $('update-btn');
  const appVersionEl= $('app-version');
  const sbVersionEl = $('sb-version');

  let currentVersion = '';
  let updateInFlight = false;

  function renderUpdateIdle() {
    updateBtn.className = '';
    updateBtn.textContent = 'v' + currentVersion;
    updateBtn.title = 'Check for updates';
    updateBtn.onclick = handleUpdateClick;
  }

  function renderUpdateUpToDate() {
    updateBtn.className = 'up-to-date';
    updateBtn.textContent = 'v' + currentVersion + ' ✓';
    updateBtn.title = 'Up to date';
    setTimeout(renderUpdateIdle, 3000);
  }

  function renderUpdateAvailable(latest, url) {
    updateBtn.className = 'update-available';
    updateBtn.textContent = 'v' + latest + ' available';
    updateBtn.title = 'Click to open release page';
    updateBtn.onclick = () => window.garmin.openUrl(url);
  }

  async function handleUpdateClick() {
    if (updateInFlight) return;
    updateInFlight = true;
    updateBtn.className = 'checking';
    updateBtn.textContent = 'Checking…';

    try {
      const res = await window.garmin.checkForUpdates();
      if (!res.ok) {
        appendLog('warn', 'Update check failed: ' + res.error);
        renderUpdateIdle();
        return;
      }

      const isNewer = res.latest && res.latest !== currentVersion &&
        res.latest.localeCompare(currentVersion, undefined, { numeric: true, sensitivity: 'version' }) > 0;

      if (isNewer) renderUpdateAvailable(res.latest, res.url);
      else         renderUpdateUpToDate();
    } finally {
      updateInFlight = false;
    }
  }

  window.garmin.getVersion().then(v => {
    currentVersion = v;
    appVersionEl.textContent = v;
    sbVersionEl.textContent  = v;
  });

  updateBtn.addEventListener('click', handleUpdateClick);

  // ── Delight: console signature for anyone who opens DevTools ────────────
  try {
    const redLine = 'color:#ff1a30; font-family:JetBrains Mono, monospace; line-height:1.1;';
    const tag     = 'color:#f2ede6; font-weight:600; letter-spacing:.18em; font-family:JetBrains Mono, monospace;';
    const dim     = 'color:#8a857f; font-family:JetBrains Mono, monospace; letter-spacing:.04em;';
    console.log(
      '%c──────────┐  ┌──────────────────\n' +
      '          │  │\n' +
      '          └──┘',
      redLine
    );
    console.log('%cGARMIN  ·  HEALTH SYNC TERMINAL', tag);
    console.log('%cvitals nominal  ·  signals clean', dim);
  } catch (_) { /* no-op */ }

})();
