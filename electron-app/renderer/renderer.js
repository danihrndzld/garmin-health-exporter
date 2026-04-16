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
  const welcomeEl    = $('welcome');
  const credsForm    = $('creds-form');

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

  // ── Helpers ──────────────────────────────────────────────────────────────
  function appendLog(type, msg, ts) {
    if (welcomeEl) welcomeEl.classList.add('hidden');
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
    statusDot.className   = state || '';
    statusLabel.className = state || '';
    statusPill.className  = 'status-pill ' + (state || '');
    statusLabel.textContent = state === 'connected' ? 'ONLINE' : state === 'error' ? 'ERROR' : 'OFFLINE';
  }

  function setRunning(val) {
    isRunning = val;
    btnHealth.disabled = val;
    sbStatus.textContent = val ? 'RUNNING' : 'IDLE';
    progressWrap.classList.toggle('active', val);
    if (!val) {
      progressFill.style.width = '0%';
      progressFill.setAttribute('aria-valuenow', '0');
    }
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

  function getOpts() {
    return {
      email:         emailEl.value.trim(),
      password:      passEl.value,
      daysBack:      parseInt(daysSlider.value, 10),
      refreshWindow: parseInt(refreshSel.value, 10),
      outputDir:     outputDir || dirDisplay.textContent || '.',
    };
  }

  function validate() {
    const { email, password } = getOpts();
    if (!email || !password) { appendLog('error', 'Email and password are required.'); return false; }
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
  function progressHandler({ current, total, phase }) {
    const pct = Math.round((current / total) * 100);
    progressFill.style.width = pct + '%';
    progressFill.setAttribute('aria-valuenow', pct);
    progressPct.textContent  = pct + '%';
    const labels = { daily: 'Daily Metrics', activities: 'Activities', csv: 'Building CSVs', 'activity-details': 'Activity Details' };
    const phaseLabel = (labels[phase] || phase) + ' — ' + current + '/' + total;
    progressPhase.textContent = phaseLabel;
    progressFill.setAttribute('aria-label', 'Export progress: ' + phaseLabel + ' (' + pct + '%)');
  }

  function logHandler({ type, msg, ts }) {
    appendLog(type, msg, ts);
  }

  window.garmin.onProgress(progressHandler);
  window.garmin.onLog(logHandler);

  // ── Download Health Data + Convert to CSVs ───────────────────────────────
  async function runHealthDownload(ev) {
    if (ev) ev.preventDefault();
    if (isRunning || !validate()) return;
    resultBanner.className = '';
    setRunning(true);
    setConnStatus('');

    const opts = getOpts();
    appendLog('info', 'Starting download — ' + opts.daysBack + ' days back…');

    const res = await window.garmin.downloadHealth(opts);
    setRunning(false);

    if (res.ok) {
      setConnStatus('connected');
      setOutputPath(res.path, true);
      showBanner(true, 'Done — click to open output folder');
    } else {
      setConnStatus('error');
      appendLog('error', res.error || 'Unknown error');
      showBanner(false, res.error || 'Download failed');
    }
  }

  btnHealth.addEventListener('click', runHealthDownload);
  if (credsForm) credsForm.addEventListener('submit', runHealthDownload);

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

})();
