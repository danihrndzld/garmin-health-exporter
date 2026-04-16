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
  let loggedIn = false;

  // Try loading saved tokens first
  const tokenResult = await auth.loadTokens();
  if (tokenResult.ok) {
    loggedIn = true;
    onLog({ type: 'dim', message: 'Loaded saved tokens' });
  } else {
    // Fall back to credential login
    onLog({ type: 'dim', message: `Token load failed (${tokenResult.error}), logging in...` });
    const loginResult = await auth.login(email, password);
    if (!loginResult.ok) {
      return { ok: false, error: `Auth failed: ${loginResult.error}` };
    }
    loggedIn = true;
    onLog({ type: 'success', message: 'Login successful' });
  }

  // -- 2. Client -------------------------------------------------------------
  const client = createGarminClient(auth, {
    log: (msg) => onLog({ type: 'dim', message: msg }),
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
  }

  // -- 6. Aggregated endpoints (always re-fetched per R8) --------------------
  onLog({ type: 'info', message: 'Pulling aggregated metrics...' });
  const aggEps = getEndpointsByType('aggregated');
  const aggregatedData = {};

  for (let i = 0; i < aggEps.length; i++) {
    const ep = aggEps[i];
    const result = await client.safe(ep.name, {
      startDate,
      endDate,
      displayName,
    });
    aggregatedData[ep.name] = result;
    onProgress({ current: i + 1, total: aggEps.length, phase: 'aggregated' });
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
  }

  // -- Goals -----------------------------------------------------------------
  const goals = await client.safe('goals', { status: 'active' }) || [];

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
  const csvOutputDir = path.join(outputDir, `csv_${exportDate}`);
  try {
    const csvResult = generateCsvs(jsonData, outputDir);
    onProgress({ current: 1, total: 1, phase: 'csv' });
    onLog({ type: 'success', message: `CSVs written to ${csvResult.csvDir}` });
  } catch (err) {
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

  onLog({ type: 'success', message: `Done. ${daysBack} days | ${allActivities.length} activities` });
  return { ok: true, jsonPath, csvDir: csvOutputDir };
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
};
