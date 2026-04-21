'use strict';

const fs = require('fs');
const path = require('path');
const { createClient } = require('./auth');
const { createGarminClient } = require('./client');
const { initCache } = require('./cache');
const { getEndpointsByType } = require('./endpoints');
const { generateCsvs } = require('./csv-writer');

// ---------------------------------------------------------------------------
// Date helpers
// ---------------------------------------------------------------------------

/**
 * Format a Date as 'YYYY-MM-DD'.
 */
function formatDate(d) {
  const y = d.getFullYear();
  const m = String(d.getMonth() + 1).padStart(2, '0');
  const day = String(d.getDate()).padStart(2, '0');
  return `${y}-${m}-${day}`;
}

/**
 * Format a Date as 'YYYY-MM-DD HH:MM:SS'.
 */
function formatDateTime(d) {
  const date = formatDate(d);
  const h = String(d.getHours()).padStart(2, '0');
  const min = String(d.getMinutes()).padStart(2, '0');
  const s = String(d.getSeconds()).padStart(2, '0');
  return `${date} ${h}:${min}:${s}`;
}

/**
 * Format a Date as 'HH-MM-SS' (for filenames).
 */
function formatTimeForFile(d) {
  const h = String(d.getHours()).padStart(2, '0');
  const min = String(d.getMinutes()).padStart(2, '0');
  const s = String(d.getSeconds()).padStart(2, '0');
  return `${h}-${min}-${s}`;
}

/**
 * Fetch `daily_steps` in 28-day chunks (Garmin API hard limit) and concat.
 * Returns a flat array of day-summary objects, newest chunk last.
 */
async function fetchDailyStepsChunked(client, startDate, endDate) {
  const MAX_DAYS = 28;
  const results = [];
  const start = new Date(startDate + 'T00:00:00');
  const end = new Date(endDate + 'T00:00:00');
  let cursor = new Date(start);
  while (cursor <= end) {
    const chunkEnd = new Date(cursor);
    chunkEnd.setDate(chunkEnd.getDate() + MAX_DAYS - 1);
    const capped = chunkEnd > end ? end : chunkEnd;
    const fmt = (d) => {
      const y = d.getFullYear();
      const m = String(d.getMonth() + 1).padStart(2, '0');
      const day = String(d.getDate()).padStart(2, '0');
      return `${y}-${m}-${day}`;
    };
    const chunk = await client.safe('daily_steps', {
      startDate: fmt(cursor),
      endDate: fmt(capped),
    });
    if (Array.isArray(chunk)) results.push(...chunk);
    else if (chunk && Array.isArray(chunk.values)) results.push(...chunk.values);
    cursor = new Date(capped);
    cursor.setDate(cursor.getDate() + 1);
    if (client.authFailed) break;
  }
  return results;
}

/**
 * Generate array of date strings from startDate to endDate inclusive,
 * most recent first (matching Python's date_range which counts down).
 * @param {string} startDate  'YYYY-MM-DD'
 * @param {string} endDate    'YYYY-MM-DD'
 * @returns {string[]}
 */
function generateDateRange(startDate, endDate) {
  const dates = [];
  const end = new Date(endDate + 'T00:00:00');
  const start = new Date(startDate + 'T00:00:00');
  const current = new Date(end);
  while (current >= start) {
    dates.push(formatDate(current));
    current.setDate(current.getDate() - 1);
  }
  return dates;
}

// ---------------------------------------------------------------------------
// Export pipeline
// ---------------------------------------------------------------------------

/**
 * Main export entry point. Orchestrates auth, client, cache, fetch, and CSV
 * generation to replicate the Python garmin_health_export.py pipeline.
 *
 * @param {object} opts
 * @param {string} opts.email
 * @param {string} opts.password
 * @param {number} opts.daysBack
 * @param {string} opts.outputDir
 * @param {number} [opts.refreshWindow=3]  Days within which cached daily data is re-fetched
 * @param {string} opts.dataDir            Directory for tokens + cache DB
 * @param {function} [opts.onProgress]     ({current, total, phase}) => void
 * @param {function} [opts.onLog]          ({type, message}) => void
 * @returns {Promise<{ok: true, jsonPath: string, csvDir: string} | {ok: false, error: string}>}
 */
async function exportHealth(opts) {
  const {
    email,
    password,
    daysBack,
    outputDir,
    refreshWindow = 3,
    dataDir,
    onProgress = () => {},
    onLog = () => {},
  } = opts;

  // -- 1. Auth ---------------------------------------------------------------
  const auth = createClient(dataDir);

  // Try loading saved tokens first
  const tokenResult = await auth.loadTokens();
  if (tokenResult.ok) {
    onLog({ type: 'dim', message: 'Loaded saved tokens' });
  } else {
    // Fall back to credential login
    onLog({ type: 'dim', message: `Token load failed (${tokenResult.error}), logging in...` });
    const loginResult = await auth.login(email, password);
    if (!loginResult.ok) {
      return { ok: false, error: `Auth failed: ${loginResult.error}` };
    }
    onLog({ type: 'success', message: 'Login successful' });
  }

  // -- 2. Client -------------------------------------------------------------
  const client = createGarminClient(auth, {
    log: (msg, extras) => {
      // Log lines that carry structured meta (e.g. skipped endpoints) are
      // surfaced as warnings so the renderer can tag them for bug reports.
      const hasMeta = extras && (extras.errorCode || extras.meta);
      onLog({
        type: hasMeta ? 'warn' : 'dim',
        message: msg,
        ...(hasMeta ? { errorCode: extras.errorCode, meta: extras.meta } : {}),
      });
    },
  });

  // -- 3. Cache --------------------------------------------------------------
  let cache = null;
  try {
    const dbPath = path.join(dataDir, 'garmin_cache.db');
    cache = await initCache(dbPath);
    onLog({ type: 'dim', message: 'Cache initialized' });
  } catch (err) {
    onLog({ type: 'warn', message: `Cache init failed: ${err.message} — continuing without cache` });
  }

  // -- 4. Display name -------------------------------------------------------
  const dnResult = await client.getDisplayName();
  let displayName = '';
  if (dnResult.ok) {
    displayName = dnResult.displayName;
    onLog({ type: 'dim', message: `Display name: ${displayName}` });
  } else {
    onLog({ type: 'warn', message: `Could not get display name: ${dnResult.error}` });
  }

  // -- Date range ------------------------------------------------------------
  const now = new Date();
  if (isNaN(now.getTime())) {
    return { ok: false, error: 'System clock returned an invalid date' };
  }
  const endDate = formatDate(now);
  const startDateObj = new Date(now);
  startDateObj.setDate(startDateObj.getDate() - (daysBack - 1));
  const startDate = formatDate(startDateObj);
  const dates = generateDateRange(startDate, endDate);

  onLog({ type: 'info', message: `Pulling ${daysBack} days: ${startDate} -> ${endDate}` });

  // -- 5. Daily endpoints ----------------------------------------------------
  const dailyEps = getEndpointsByType('daily');
  const dailyData = {};
  const totalDailyWork = dates.length;

  for (let i = 0; i < dates.length; i++) {
    const date = dates[i];
    const dayEntry = {};

    for (const ep of dailyEps) {
      const epName = ep.name;

      // Check cache (skip if outside refresh window and cached)
      if (cache) {
        const withinWindow = cache.isWithinRefreshWindow(date, refreshWindow);
        if (!withinWindow) {
          const cached = cache.getCachedDaily(date, epName);
          if (cached !== null) {
            dayEntry[epName] = cached;
            continue;
          }
        }
      }

      // Fetch
      const result = await client.safe(epName, { date, displayName });
      dayEntry[epName] = result;

      // Store in cache
      if (cache && result !== null) {
        try {
          cache.setCachedDaily(date, epName, result);
        } catch (err) {
          onLog({ type: 'warn', message: `Cache write failed for ${epName}/${date}: ${err.message}` });
        }
      }
    }

    dailyData[date] = dayEntry;
    onProgress({ current: i + 1, total: totalDailyWork, phase: 'daily' });
    onLog({ type: 'dim', message: `  ${date}` });

    // Periodic save so a crash or kill doesn't lose the whole daily pull.
    if (cache && (i + 1) % 10 === 0) {
      try { cache.save(); } catch (_e) {}
    }

    if (client.authFailed) {
      return { ok: false, error: 'Auth failed mid-export — re-login required' };
    }
  }

  // -- 6. Aggregated endpoints (always re-fetched per R8) --------------------
  onLog({ type: 'info', message: 'Pulling aggregated metrics...' });
  const aggEps = getEndpointsByType('aggregated');
  const aggregatedData = {};

  for (let i = 0; i < aggEps.length; i++) {
    const ep = aggEps[i];
    let result;
    if (ep.name === 'daily_steps') {
      // R13: chunk into 28-day windows and concat the per-chunk arrays
      result = await fetchDailyStepsChunked(client, startDate, endDate);
    } else {
      result = await client.safe(ep.name, { startDate, endDate, displayName });
    }
    aggregatedData[ep.name] = result;
    onProgress({ current: i + 1, total: aggEps.length, phase: 'aggregated' });

    if (client.authFailed) {
      return { ok: false, error: 'Auth failed mid-export — re-login required' };
    }
  }

  // -- 7. Activity list (with pagination) ------------------------------------
  onLog({ type: 'info', message: 'Pulling activities...' });
  let allActivities = [];
  const PAGE_SIZE = 20;
  let start = 0;

  while (true) {
    const page = await client.safe('activities_by_date', {
      startDate,
      endDate,
      start,
      limit: PAGE_SIZE,
    });
    if (!page || !Array.isArray(page) || page.length === 0) break;
    allActivities = allActivities.concat(page);
    if (page.length < PAGE_SIZE) break;
    start += PAGE_SIZE;
  }

  onLog({ type: 'info', message: `Found ${allActivities.length} activities` });

  // -- 8. Activity details (splits, typed_splits) ----------------------------
  for (let i = 0; i < allActivities.length; i++) {
    const act = allActivities[i];
    const activityId = act.activityId;
    const actName = act.activityName ||
      (act.activityType && act.activityType.typeKey) || 'unknown';

    onLog({
      type: 'dim',
      message: `  [${i + 1}/${allActivities.length}] ${(act.startTimeLocal || '').split(' ')[0]} | ${actName}`,
    });

    if (activityId) {
      // Splits
      let splits = null;
      if (cache) {
        splits = cache.getCachedActivity(activityId, 'splits');
      }
      if (splits === null) {
        splits = await client.safe('activity_splits', { activityId });
        if (cache && splits !== null) {
          try {
            cache.setCachedActivity(activityId, 'splits', splits);
          } catch (err) {
            onLog({ type: 'warn', message: `Cache write failed for splits/${activityId}: ${err.message}` });
          }
        }
      }
      act.splits = splits;

      // Typed splits
      let typedSplits = null;
      if (cache) {
        typedSplits = cache.getCachedActivity(activityId, 'typed_splits');
      }
      if (typedSplits === null) {
        typedSplits = await client.safe('activity_typed_splits', { activityId });
        if (cache && typedSplits !== null) {
          try {
            cache.setCachedActivity(activityId, 'typed_splits', typedSplits);
          } catch (err) {
            onLog({ type: 'warn', message: `Cache write failed for typed_splits/${activityId}: ${err.message}` });
          }
        }
      }
      act.typed_splits = typedSplits;
    }

    onProgress({ current: i + 1, total: allActivities.length, phase: 'activities' });

    // Periodic save so a crash mid-activity doesn't lose fetched splits.
    if (cache && (i + 1) % 10 === 0) {
      try { cache.save(); } catch (_e) {}
    }

    if (client.authFailed) {
      return { ok: false, error: 'Auth failed mid-export — re-login required' };
    }
  }

  // -- Goals -----------------------------------------------------------------
  const goalsRaw = await client.safe('goals', { status: 'active' });
  const goals = Array.isArray(goalsRaw) ? goalsRaw : [];

  // -- 9. Build output JSON --------------------------------------------------
  const exportDate = formatDate(now);
  const exportDateTime = formatDateTime(now);

  const jsonData = {
    export_date: exportDate,
    date_range: { start: startDate, end: endDate, days: daysBack },
    daily: dailyData,
    aggregated: aggregatedData,
    activities: allActivities,
    goals: goals,
  };

  // -- 10. Write JSON file ---------------------------------------------------
  fs.mkdirSync(outputDir, { recursive: true });
  const timeTag = formatTimeForFile(now);
  const jsonFileName = `garmin_health_${exportDate}_${timeTag}.json`;
  const jsonPath = path.join(outputDir, jsonFileName);
  fs.writeFileSync(jsonPath, JSON.stringify(jsonData, null, 2), 'utf8');
  onLog({ type: 'success', message: `JSON saved: ${jsonPath}` });

  // -- 11. Generate CSVs -----------------------------------------------------
  let csvDir = null;
  let csvError = null;
  try {
    const csvResult = generateCsvs(jsonData, outputDir);
    csvDir = csvResult.csvDir;
    onProgress({ current: 1, total: 1, phase: 'csv' });
    onLog({ type: 'success', message: `CSVs written to ${csvDir}` });
  } catch (err) {
    csvError = err.message;
    onLog({ type: 'warn', message: `CSV generation failed: ${err.message}` });
  }

  // -- 12. Save cache and return ---------------------------------------------
  if (cache) {
    try {
      cache.save();
    } catch (err) {
      onLog({ type: 'warn', message: `Cache save failed: ${err.message}` });
    }
  }

  if (csvError) {
    return { ok: false, error: `CSV generation failed: ${csvError}`, jsonPath };
  }

  onLog({ type: 'success', message: `Done. ${daysBack} days | ${allActivities.length} activities` });
  return { ok: true, jsonPath, csvDir };
}

// ---------------------------------------------------------------------------
// Exports
// ---------------------------------------------------------------------------
module.exports = {
  exportHealth,
  // Expose for testing
  formatDate,
  formatDateTime,
  formatTimeForFile,
  generateDateRange,
  fetchDailyStepsChunked,
};
