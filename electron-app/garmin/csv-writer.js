'use strict';

const fs = require('fs');
const path = require('path');

// ---------------------------------------------------------------------------
// Activity-type grouping (from download_activity_details.py)
// ---------------------------------------------------------------------------
const TYPE_GROUPS = {
  caminar: new Set(['walking', 'hiking']),
  correr:  new Set(['running', 'treadmill_running']),
  gym:     new Set(['strength_training', 'indoor_cardio']),
};

function groupFor(typeKey) {
  for (const [group, keys] of Object.entries(TYPE_GROUPS)) {
    if (keys.has(typeKey)) return group;
  }
  return null;
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

/**
 * Recursively flatten a nested object into a single-level object.
 * Keys are joined with `sep` (default "_").
 * Arrays become JSON strings. null/undefined pass through.
 */
function flatten(obj, prefix, sep) {
  if (prefix === undefined) prefix = '';
  if (sep === undefined) sep = '_';
  const out = {};
  if (obj == null || typeof obj !== 'object' || Array.isArray(obj)) return out;
  for (const [k, v] of Object.entries(obj)) {
    const key = prefix ? `${prefix}${sep}${k}` : k;
    if (v !== null && v !== undefined && typeof v === 'object' && !Array.isArray(v)) {
      Object.assign(out, flatten(v, key, sep));
    } else if (Array.isArray(v)) {
      out[key] = JSON.stringify(v);
    } else {
      out[key] = v;
    }
  }
  return out;
}

/**
 * Format a value for CSV output (QUOTE_MINIMAL behaviour).
 * - null/undefined -> empty string
 * - strings containing comma, double-quote, or newline are quoted
 * - internal double-quotes are doubled
 */
function formatCsvField(val) {
  if (val === null || val === undefined) return '';
  const s = String(val);
  if (s.includes(',') || s.includes('"') || s.includes('\n') || s.includes('\r')) {
    return '"' + s.replace(/"/g, '""') + '"';
  }
  return s;
}

/**
 * Write an array of row objects as CSV.
 * Headers are derived from the keys of all rows (preserving first-seen order).
 * Empty rows array produces no file (matches Python behaviour).
 */
function writeCsv(rows, filePath) {
  if (!rows || rows.length === 0) return;

  // Collect all keys in first-seen order across all rows
  const keySet = new Set();
  const keys = [];
  for (const row of rows) {
    for (const k of Object.keys(row)) {
      if (!keySet.has(k)) {
        keySet.add(k);
        keys.push(k);
      }
    }
  }

  const lines = [];
  // Header row
  lines.push(keys.map(formatCsvField).join(','));
  // Data rows
  for (const row of rows) {
    const vals = keys.map(k => {
      const v = row[k];
      if (v === null || v === undefined) return '';
      return formatCsvField(v);
    });
    lines.push(vals.join(','));
  }

  const dir = path.dirname(filePath);
  fs.mkdirSync(dir, { recursive: true });
  fs.writeFileSync(filePath, lines.join('\n') + '\n', 'utf8');
}

// ---------------------------------------------------------------------------
// Per-section extractors (matching json_to_csv.py)
// ---------------------------------------------------------------------------

function extractDailySummary(daily) {
  const rows = [];
  for (const [date, d] of Object.entries(daily)) {
    const row = { date };
    const stats = d.stats || {};
    for (const key of [
      'totalKilocalories', 'activeKilocalories', 'bmrKilocalories',
      'totalSteps', 'totalDistanceMeters', 'wellnessKilocalories',
      'remainingKilocalories', 'floorsAscended', 'floorsDescended',
      'durationInMilliseconds', 'stepGoal',
    ]) {
      row[key] = stats[key] !== undefined ? stats[key] : null;
    }
    const hr = d.heart_rates || {};
    row.maxHeartRate = hr.maxHeartRate !== undefined ? hr.maxHeartRate : null;
    row.minHeartRate = hr.minHeartRate !== undefined ? hr.minHeartRate : null;
    row.restingHeartRate = hr.restingHeartRate !== undefined ? hr.restingHeartRate : null;
    row.lastSevenDaysAvgRestingHeartRate = hr.lastSevenDaysAvgRestingHeartRate !== undefined ? hr.lastSevenDaysAvgRestingHeartRate : null;
    const stress = d.stress || {};
    row.maxStressLevel = stress.maxStressLevel !== undefined ? stress.maxStressLevel : null;
    row.avgStressLevel = stress.avgStressLevel !== undefined ? stress.avgStressLevel : null;
    const im = d.intensity_minutes || {};
    row.moderateIntensityMinutes = im.moderateIntensityMinutes !== undefined ? im.moderateIntensityMinutes : null;
    row.vigorousIntensityMinutes = im.vigorousIntensityMinutes !== undefined ? im.vigorousIntensityMinutes : null;
    const steps = d.steps || {};
    if (typeof steps === 'object' && !Array.isArray(steps)) {
      row.stepsGoal = steps.dailyStepGoal !== undefined ? steps.dailyStepGoal : null;
      row.stepsWellness = steps.totalSteps !== undefined ? steps.totalSteps : null;
    }
    const hyd = d.hydration || {};
    row.hydrationGoalMl = hyd.goalInML !== undefined ? hyd.goalInML : null;
    row.hydrationValueMl = hyd.valueInML !== undefined ? hyd.valueInML : null;
    row.hydrationSweatLossMl = hyd.sweatLossInML !== undefined ? hyd.sweatLossInML : null;
    row.hydrationDailyAverageMl = hyd.dailyAverageinML !== undefined ? hyd.dailyAverageinML : null;
    row.hydrationActivityIntakeMl = hyd.activityIntakeInML !== undefined ? hyd.activityIntakeInML : null;
    rows.push(row);
  }
  return rows;
}

function extractSleep(daily) {
  const rows = [];
  for (const [date, d] of Object.entries(daily)) {
    const sleep = d.sleep || {};
    const dto = sleep.dailySleepDTO || {};
    if (!Object.keys(dto).length) continue;
    const row = { date };
    for (const key of [
      'sleepTimeSeconds', 'napTimeSeconds', 'unmeasurableSeconds',
      'deepSleepSeconds', 'lightSleepSeconds', 'remSleepSeconds',
      'awakeSleepSeconds', 'averageSpO2Value', 'lowestSpO2Value',
      'highestSpO2Value', 'averageSpO2HRSleep', 'averageRespirationValue',
      'lowestRespirationValue', 'highestRespirationValue',
      'avgSleepStress', 'sleepScores', 'sleepResultType',
      'sleepStartTimestampGMT', 'sleepEndTimestampGMT',
      'sleepStartTimestampLocal', 'sleepEndTimestampLocal',
      'awakeCount', 'avgHeartRate', 'sleepScoreFeedback',
      'sleepScoreInsight', 'sleepNeed', 'nextSleepNeed',
      'breathingDisruptionSeverity', 'sleepAlignment',
      'sleepWindowConfirmed',
    ]) {
      let val = dto[key] !== undefined ? dto[key] : null;
      if (val !== null && typeof val === 'object') val = JSON.stringify(val);
      row[key] = val;
    }
    row.restlessMomentsCount = sleep.restlessMomentsCount !== undefined ? sleep.restlessMomentsCount : null;
    row.avgOvernightHrv = sleep.avgOvernightHrv !== undefined ? sleep.avgOvernightHrv : null;
    row.avgSkinTempDeviationC = sleep.avgSkinTempDeviationC !== undefined ? sleep.avgSkinTempDeviationC : null;
    row.restingHeartRate = sleep.restingHeartRate !== undefined ? sleep.restingHeartRate : null;
    row.bodyBatteryChange = sleep.bodyBatteryChange !== undefined ? sleep.bodyBatteryChange : null;
    rows.push(row);
  }
  return rows;
}

function extractHrv(daily) {
  const rows = [];
  for (const [date, d] of Object.entries(daily)) {
    const hrv = d.hrv || {};
    const summary = hrv.hrvSummary || {};
    if (!Object.keys(summary).length) continue;
    const row = { date };
    for (const key of [
      'weeklyAvg', 'lastNight', 'lastNight5MinHigh', 'lastNight5MinLow',
      'status', 'feedbackPhrase', 'startTimestampGMT', 'endTimestampGMT',
    ]) {
      row[key] = summary[key] !== undefined ? summary[key] : null;
    }
    rows.push(row);
  }
  return rows;
}

function extractBodyBattery(daily) {
  const rows = [];
  for (const [date, d] of Object.entries(daily)) {
    const bb = d.body_battery || [];
    for (const entry of bb) {
      const row = { date };
      for (const key of ['charged', 'drained', 'startTimestampLocal', 'endTimestampLocal']) {
        row[key] = entry[key] !== undefined ? entry[key] : null;
      }
      rows.push(row);
    }
  }
  return rows;
}

function extractTraining(daily) {
  const rows = [];
  for (const [date, d] of Object.entries(daily)) {
    const row = { date };
    // training readiness - may be list or dict
    let trRaw = d.training_readiness || {};
    let tr = Array.isArray(trRaw) && trRaw.length ? trRaw[trRaw.length - 1]
           : (typeof trRaw === 'object' && !Array.isArray(trRaw) ? trRaw : {});
    row.readiness_level = tr.level !== undefined ? tr.level : null;
    row.readiness_feedbackShort = tr.feedbackShort !== undefined ? tr.feedbackShort : null;
    row.readiness_feedbackLong = tr.feedbackLong !== undefined ? tr.feedbackLong : null;
    // morning readiness
    let mrRaw = d.morning_training_readiness || {};
    let mr = Array.isArray(mrRaw) && mrRaw.length ? mrRaw[mrRaw.length - 1]
           : (typeof mrRaw === 'object' && !Array.isArray(mrRaw) ? mrRaw : {});
    row.morning_readiness_level = mr.level !== undefined ? mr.level : null;
    row.morning_readiness_feedback = mr.feedbackShort !== undefined ? mr.feedbackShort : null;
    row.morning_readiness_recoveryTime = mr.recoveryTime !== undefined ? mr.recoveryTime : null;
    row.morning_readiness_sleepScoreFactor = mr.sleepScoreFactorPercent !== undefined ? mr.sleepScoreFactorPercent : null;
    row.morning_readiness_recoveryTimeFactor = mr.recoveryTimeFactorPercent !== undefined ? mr.recoveryTimeFactorPercent : null;
    // training status
    const ts = d.training_status || {};
    const vo2 = ts.mostRecentVO2Max || {};
    row.vo2max = vo2.vo2MaxPreciseValue || vo2.vo2MaxValue || null;
    const tlb = ts.mostRecentTrainingLoadBalance || {};
    row.trainingLoad_7day = tlb.sevenDayTrainingLoad !== undefined ? tlb.sevenDayTrainingLoad : null;
    row.trainingLoad_28day = tlb.twentyEightDayTrainingLoad !== undefined ? tlb.twentyEightDayTrainingLoad : null;
    const mts = ts.mostRecentTrainingStatus || {};
    row.trainingStatus = mts.trainingStatusPhrase || mts.trainingStatus || null;
    // endurance
    const end = d.endurance_score || {};
    row.enduranceScore = end.overallScore !== undefined ? end.overallScore : null;
    row.enduranceScore_label = end.overallScoreLabel !== undefined ? end.overallScoreLabel : null;
    rows.push(row);
  }
  return rows;
}

function extractSpo2Respiration(daily) {
  const rows = [];
  for (const [date, d] of Object.entries(daily)) {
    const row = { date };
    const spo2 = d.spo2 || {};
    row.spo2_avg = spo2.averageSpO2 !== undefined ? spo2.averageSpO2 : null;
    row.spo2_lowest = spo2.lowestSpO2 !== undefined ? spo2.lowestSpO2 : null;
    const resp = d.respiration || {};
    row.respiration_avg = resp.avgWakingRespirationValue !== undefined ? resp.avgWakingRespirationValue : null;
    row.respiration_highest = resp.highestRespirationValue !== undefined ? resp.highestRespirationValue : null;
    row.respiration_lowest = resp.lowestRespirationValue !== undefined ? resp.lowestRespirationValue : null;
    rows.push(row);
  }
  return rows;
}

function extractActivities(activities) {
  const rows = [];
  for (const act of activities) {
    const row = {};
    for (const key of [
      'activityId', 'activityName', 'startTimeLocal', 'distance',
      'duration', 'movingDuration', 'elapsedDuration',
      'averageSpeed', 'calories', 'averageHR', 'maxHR', 'steps',
      'aerobicTrainingEffect', 'anaerobicTrainingEffect',
      'activityTrainingLoad', 'trainingEffectLabel',
      'moderateIntensityMinutes', 'vigorousIntensityMinutes',
      'differenceBodyBattery', 'lapCount',
      'hrTimeInZone_1', 'hrTimeInZone_2', 'hrTimeInZone_3',
      'hrTimeInZone_4', 'hrTimeInZone_5',
      'minTemperature', 'maxTemperature', 'avgElevation',
      'elevationGain', 'elevationLoss',
      'totalReps', 'totalSets', 'activeSets',
      'aerobicTrainingEffectMessage', 'anaerobicTrainingEffectMessage',
      'vO2MaxValue', 'avgStrideLength', 'locationName',
      'startLatitude', 'startLongitude', 'endLatitude', 'endLongitude',
      'averageRunningCadenceInStepsPerMinute',
      'maxRunningCadenceInStepsPerMinute',
    ]) {
      row[key] = act[key] !== undefined ? act[key] : null;
    }
    const actType = act.activityType || {};
    row.activityType = (typeof actType === 'object' && actType !== null) ? actType.typeKey : actType;
    rows.push(row);
  }
  return rows;
}

function extractWeight(aggregated) {
  const rows = [];
  const weighIns = aggregated.weigh_ins || {};
  for (const entry of (weighIns.dateWeightList || [])) {
    rows.push(flatten(entry));
  }
  const bc = aggregated.body_composition || {};
  for (const entry of (bc.dateWeightList || [])) {
    rows.push(flatten(entry));
  }
  return rows;
}

function extractAggregatedMisc(aggregated) {
  const row = {};
  for (const key of ['race_predictions', 'lactate_threshold', 'cycling_ftp',
                      'fitness_age', 'hill_score']) {
    const val = aggregated[key];
    if (val === undefined || val === null) continue;
    if (typeof val === 'object' && !Array.isArray(val)) {
      for (const [k, v] of Object.entries(flatten(val))) {
        row[`${key}_${k}`] = v;
      }
    } else {
      row[key] = val;
    }
  }
  return Object.keys(row).length ? [row] : [];
}

function extractRunningTolerance(daily) {
  const rows = [];
  for (const [date, d] of Object.entries(daily)) {
    for (const entry of (d.running_tolerance || [])) {
      const row = { date };
      for (const key of ['totalImpactLoad', 'totalDistance', 'tolerance',
                          'startOfWeek', 'endOfWeek', 'weekIndex']) {
        row[key] = entry[key] !== undefined ? entry[key] : null;
      }
      rows.push(row);
    }
  }
  return rows;
}

function extractAggregatedDailySteps(aggregated) {
  return (aggregated.daily_steps || []).map(entry => {
    const row = {};
    for (const k of ['calendarDate', 'totalSteps', 'totalDistance', 'stepGoal']) {
      row[k] = entry[k] !== undefined ? entry[k] : null;
    }
    return row;
  });
}

function extractWeeklyIntensity(aggregated) {
  return (aggregated.weekly_intensity || []).map(entry => {
    const row = {};
    for (const k of ['calendarDate', 'weeklyGoal', 'moderateValue', 'vigorousValue']) {
      row[k] = entry[k] !== undefined ? entry[k] : null;
    }
    return row;
  });
}

function extractHillScoreDaily(aggregated) {
  const hs = aggregated.hill_score || {};
  const rows = [];
  for (const entry of (hs.hillScoreDTOList || [])) {
    const row = {};
    for (const key of ['calendarDate', 'overallScore', 'strengthScore', 'enduranceScore',
                        'hillScoreClassificationId', 'hillScoreFeedbackPhraseId']) {
      row[key] = entry[key] !== undefined ? entry[key] : null;
    }
    rows.push(row);
  }
  return rows;
}

function extractBloodPressure(aggregated) {
  const bp = aggregated.blood_pressure || {};
  const rows = [];
  for (const entry of (bp.measurementSummaries || [])) {
    rows.push(flatten(entry));
  }
  return rows;
}

// ---------------------------------------------------------------------------
// Activity details helpers (from download_activity_details.py)
// ---------------------------------------------------------------------------

const SUMMARY_FIELDS = [
  'activityId', 'activityName', 'startTimeLocal', 'distance', 'duration',
  'movingDuration', 'calories', 'averageHR', 'maxHR', 'steps',
  'aerobicTrainingEffect', 'anaerobicTrainingEffect', 'activityTrainingLoad',
  'trainingEffectLabel', 'differenceBodyBattery',
  'hrTimeInZone_1', 'hrTimeInZone_2', 'hrTimeInZone_3', 'hrTimeInZone_4', 'hrTimeInZone_5',
  'moderateIntensityMinutes', 'vigorousIntensityMinutes',
  'avgElevation', 'minTemperature', 'maxTemperature',
  'totalReps', 'totalSets', 'activeSets',
  'lapCount', 'averageSpeed',
];

/**
 * Flatten activity detail metrics into avg/max/min summary per metric key.
 */
function flattenDetails(details) {
  const row = {};
  const descriptors = details.metricDescriptors || [];
  const idxToKey = {};
  for (const d of descriptors) {
    idxToKey[d.metricsIndex] = d.key;
  }
  const metricEntries = details.activityDetailMetrics || [];
  if (metricEntries.length && Object.keys(idxToKey).length) {
    const sums = {};
    for (const entry of metricEntries) {
      const values = entry.metrics || [];
      for (let idx = 0; idx < values.length; idx++) {
        const key = idxToKey[idx];
        const val = values[idx];
        if (key && val !== null && val !== undefined) {
          if (!sums[key]) sums[key] = [];
          sums[key].push(val);
        }
      }
    }
    const skip = new Set(['directLatitude', 'directLongitude', 'directTimestamp']);
    for (const [key, vals] of Object.entries(sums)) {
      if (skip.has(key)) continue;
      const clean = vals.filter(v => v !== null && v !== undefined);
      if (!clean.length) continue;
      const sum = clean.reduce((a, b) => a + b, 0);
      row[`avg_${key}`] = Math.round((sum / clean.length) * 10000) / 10000;
      row[`max_${key}`] = Math.max(...clean);
      row[`min_${key}`] = Math.min(...clean);
    }
  }
  return row;
}

/**
 * Flatten lap split data into an array of row objects.
 */
function flattenLaps(splits) {
  const rows = [];
  for (const lap of (splits.lapDTOs || [])) {
    const row = {};
    for (const key of [
      'lapIndex', 'startTimeGMT', 'distance', 'duration', 'movingDuration',
      'averageSpeed', 'averageHR', 'maxHR', 'calories',
      'averageRunCadence', 'maxRunCadence',
      'averageTemperature', 'maxTemperature', 'minTemperature',
      'totalAscent', 'totalDescent',
    ]) {
      row[`lap_${key}`] = lap[key] !== undefined ? lap[key] : null;
    }
    rows.push(row);
  }
  return rows;
}

// ---------------------------------------------------------------------------
// Main entry point
// ---------------------------------------------------------------------------

/**
 * Generate all CSVs from the full JSON export data.
 * @param {object} jsonData - The parsed garmin_health JSON
 * @param {string} outputDir - Base output directory
 * @returns {string} Path to the csv_YYYY-MM-DD directory created
 */
function generateCsvs(jsonData, outputDir) {
  const dateTag = jsonData.export_date || 'export';
  const csvDir = path.join(outputDir, `csv_${dateTag}`);
  fs.mkdirSync(csvDir, { recursive: true });

  const daily = jsonData.daily || {};
  const aggregated = jsonData.aggregated || {};
  const activities = jsonData.activities || [];

  const csvFiles = [
    ['daily_summary',           extractDailySummary(daily)],
    ['sleep',                   extractSleep(daily)],
    ['hrv',                     extractHrv(daily)],
    ['body_battery',            extractBodyBattery(daily)],
    ['training',                extractTraining(daily)],
    ['spo2_respiration',        extractSpo2Respiration(daily)],
    ['activities',              extractActivities(activities)],
    ['weight_body_comp',        extractWeight(aggregated)],
    ['blood_pressure',          extractBloodPressure(aggregated)],
    ['misc_metrics',            extractAggregatedMisc(aggregated)],
    ['running_tolerance',       extractRunningTolerance(daily)],
    ['daily_steps_aggregated',  extractAggregatedDailySteps(aggregated)],
    ['weekly_intensity',        extractWeeklyIntensity(aggregated)],
    ['hill_score_daily',        extractHillScoreDaily(aggregated)],
  ];

  const written = [];
  for (const [name, rows] of csvFiles) {
    if (rows.length > 0) {
      const filePath = path.join(csvDir, `${name}.csv`);
      writeCsv(rows, filePath);
      written.push({ name: `${name}.csv`, rows: rows.length });
    }
  }

  return { csvDir, written };
}

// ---------------------------------------------------------------------------
// Exports
// ---------------------------------------------------------------------------
module.exports = {
  flatten,
  formatCsvField,
  writeCsv,
  generateCsvs,
  flattenDetails,
  flattenLaps,
  groupFor,
  TYPE_GROUPS,
  SUMMARY_FIELDS,
  // Individual extractors (for testing or selective use)
  extractDailySummary,
  extractSleep,
  extractHrv,
  extractBodyBattery,
  extractTraining,
  extractSpo2Respiration,
  extractActivities,
  extractWeight,
  extractAggregatedMisc,
  extractRunningTolerance,
  extractAggregatedDailySteps,
  extractWeeklyIntensity,
  extractHillScoreDaily,
  extractBloodPressure,
};
