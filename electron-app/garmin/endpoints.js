/**
 * Garmin Connect API Endpoint Definitions
 *
 * Maps all endpoint names to URL-builder functions.
 * URLs sourced from the python-garminconnect library.
 *
 * Each endpoint: { name, type, buildUrl(params) }
 *   - type: 'daily' | 'aggregated' | 'activity' | 'list'
 *   - params vary by type (see JSDoc on each)
 */

const { API_BASE } = require('./auth');

// ---------------------------------------------------------------------------
// Per-day endpoints (type: 'daily')
// params: { date: 'YYYY-MM-DD', displayName: string }
// ---------------------------------------------------------------------------

const dailyEndpoints = [
  {
    name: 'stats',
    type: 'daily',
    buildUrl: ({ date, displayName }) =>
      `${API_BASE}/usersummary-service/usersummary/daily/${displayName}?calendarDate=${date}`,
  },
  {
    name: 'user_summary',
    type: 'daily',
    buildUrl: ({ date, displayName }) =>
      `${API_BASE}/usersummary-service/usersummary/daily/${displayName}?calendarDate=${date}`,
  },
  {
    name: 'heart_rates',
    type: 'daily',
    buildUrl: ({ date, displayName }) =>
      `${API_BASE}/wellness-service/wellness/dailyHeartRate/${displayName}?date=${date}`,
  },
  {
    name: 'rhr',
    type: 'daily',
    buildUrl: ({ date, displayName }) =>
      `${API_BASE}/userstats-service/wellness/daily/${displayName}?fromDate=${date}&untilDate=${date}&metricId=60`,
  },
  {
    name: 'hrv',
    type: 'daily',
    buildUrl: ({ date }) =>
      `${API_BASE}/hrv-service/hrv/${date}`,
  },
  {
    name: 'stress',
    type: 'daily',
    buildUrl: ({ date }) =>
      `${API_BASE}/wellness-service/wellness/dailyStress/${date}`,
  },
  {
    name: 'all_day_stress',
    type: 'daily',
    buildUrl: ({ date }) =>
      `${API_BASE}/wellness-service/wellness/dailyStress/${date}`,
  },
  {
    name: 'sleep',
    type: 'daily',
    buildUrl: ({ date, displayName }) =>
      `${API_BASE}/wellness-service/wellness/dailySleepData/${displayName}?date=${date}&nonSleepBufferMinutes=60`,
  },
  {
    name: 'body_battery',
    type: 'daily',
    buildUrl: ({ date }) =>
      `${API_BASE}/wellness-service/wellness/bodyBattery/reports/daily?startDate=${date}&endDate=${date}`,
  },
  {
    name: 'body_battery_events',
    type: 'daily',
    buildUrl: ({ date }) =>
      `${API_BASE}/wellness-service/wellness/bodyBattery/events/${date}`,
  },
  {
    name: 'respiration',
    type: 'daily',
    buildUrl: ({ date }) =>
      `${API_BASE}/wellness-service/wellness/daily/respiration/${date}`,
  },
  {
    name: 'spo2',
    type: 'daily',
    buildUrl: ({ date }) =>
      `${API_BASE}/wellness-service/wellness/daily/spo2/${date}`,
  },
  {
    name: 'steps',
    type: 'daily',
    buildUrl: ({ date, displayName }) =>
      `${API_BASE}/wellness-service/wellness/dailySummaryChart/${displayName}?date=${date}`,
  },
  {
    name: 'intensity_minutes',
    type: 'daily',
    buildUrl: ({ date }) =>
      `${API_BASE}/wellness-service/wellness/daily/im/${date}`,
  },
  {
    name: 'hydration',
    type: 'daily',
    buildUrl: ({ date }) =>
      `${API_BASE}/usersummary-service/usersummary/hydration/daily/${date}`,
  },
  {
    name: 'training_readiness',
    type: 'daily',
    buildUrl: ({ date }) =>
      `${API_BASE}/metrics-service/metrics/trainingreadiness/${date}`,
  },
  {
    name: 'morning_training_readiness',
    type: 'daily',
    // Same URL as training_readiness — filtered client-side for AFTER_WAKEUP_RESET
    buildUrl: ({ date }) =>
      `${API_BASE}/metrics-service/metrics/trainingreadiness/${date}`,
  },
  {
    name: 'training_status',
    type: 'daily',
    buildUrl: ({ date }) =>
      `${API_BASE}/metrics-service/metrics/trainingstatus/aggregated/${date}`,
  },
  {
    name: 'weigh_ins',
    type: 'daily',
    buildUrl: ({ date }) =>
      `${API_BASE}/weight-service/weight/dayview/${date}?includeAll=true`,
  },
  {
    name: 'max_metrics',
    type: 'daily',
    buildUrl: ({ date }) =>
      `${API_BASE}/metrics-service/metrics/maxmet/daily/${date}/${date}`,
  },
  {
    name: 'endurance_score',
    type: 'daily',
    buildUrl: ({ date }) =>
      `${API_BASE}/metrics-service/metrics/endurancescore?calendarDate=${date}`,
  },
  {
    name: 'running_tolerance',
    type: 'daily',
    buildUrl: ({ date }) =>
      `${API_BASE}/metrics-service/metrics/runningtolerance/stats?startDate=${date}&endDate=${date}&aggregation=daily`,
  },
];

// ---------------------------------------------------------------------------
// Aggregated endpoints (type: 'aggregated')
// params: { startDate, endDate, displayName? }
// ---------------------------------------------------------------------------

const aggregatedEndpoints = [
  {
    name: 'blood_pressure',
    type: 'aggregated',
    buildUrl: ({ startDate, endDate }) =>
      `${API_BASE}/bloodpressure-service/bloodpressure/range/${startDate}/${endDate}?includeAll=true`,
  },
  {
    name: 'agg_weigh_ins',
    type: 'aggregated',
    buildUrl: ({ startDate, endDate }) =>
      `${API_BASE}/weight-service/weight/range/${startDate}/${endDate}?includeAll=true`,
  },
  {
    name: 'daily_steps',
    type: 'aggregated',
    // NOTE: 28-day max range — caller must chunk
    buildUrl: ({ startDate, endDate }) =>
      `${API_BASE}/usersummary-service/stats/steps/daily/${startDate}/${endDate}`,
  },
  {
    name: 'weekly_steps',
    type: 'aggregated',
    // Python uses end + weeks, but we pass startDate/endDate and compute weeks
    buildUrl: ({ endDate, weeks }) =>
      `${API_BASE}/usersummary-service/stats/steps/weekly/${endDate}/${weeks || 52}`,
  },
  {
    name: 'weekly_intensity',
    type: 'aggregated',
    buildUrl: ({ startDate, endDate }) =>
      `${API_BASE}/usersummary-service/stats/im/weekly/${startDate}/${endDate}`,
  },
  {
    name: 'body_composition',
    type: 'aggregated',
    buildUrl: ({ startDate, endDate }) =>
      `${API_BASE}/weight-service/weight/dateRange?startDate=${startDate}&endDate=${endDate}`,
  },
  {
    name: 'progress_summary',
    type: 'aggregated',
    buildUrl: ({ startDate, endDate }) =>
      `${API_BASE}/fitnessstats-service/activity?startDate=${startDate}&endDate=${endDate}&aggregation=lifetime&groupByParentActivityType=true&metric=duration`,
  },
  {
    name: 'race_predictions',
    type: 'aggregated',
    buildUrl: ({ displayName }) =>
      `${API_BASE}/metrics-service/metrics/racepredictions/latest/${displayName}`,
  },
  {
    name: 'lactate_threshold',
    type: 'aggregated',
    buildUrl: () =>
      `${API_BASE}/biometric-service/biometric/latestLactateThreshold`,
  },
  {
    name: 'cycling_ftp',
    type: 'aggregated',
    buildUrl: () =>
      `${API_BASE}/biometric-service/biometric/latestFunctionalThresholdPower/CYCLING`,
  },
  {
    name: 'fitness_age',
    type: 'aggregated',
    buildUrl: ({ endDate }) =>
      `${API_BASE}/fitnessage-service/fitnessage/${endDate}`,
  },
  {
    name: 'hill_score',
    type: 'aggregated',
    buildUrl: ({ startDate, endDate }) =>
      `${API_BASE}/metrics-service/metrics/hillscore/stats?startDate=${startDate}&endDate=${endDate}&aggregation=daily`,
  },
];

// ---------------------------------------------------------------------------
// Per-activity endpoints (type: 'activity')
// params: { activityId }
// ---------------------------------------------------------------------------

const activityEndpoints = [
  {
    name: 'activity_details',
    type: 'activity',
    buildUrl: ({ activityId }) =>
      `${API_BASE}/activity-service/activity/${activityId}/details?maxChartSize=2000&maxPolylineSize=4000`,
  },
  {
    name: 'activity_splits',
    type: 'activity',
    buildUrl: ({ activityId }) =>
      `${API_BASE}/activity-service/activity/${activityId}/splits`,
  },
  {
    name: 'activity_typed_splits',
    type: 'activity',
    buildUrl: ({ activityId }) =>
      `${API_BASE}/activity-service/activity/${activityId}/split_summaries`,
  },
];

// ---------------------------------------------------------------------------
// List endpoints (type: 'list')
// params vary per endpoint
// ---------------------------------------------------------------------------

const listEndpoints = [
  {
    name: 'activities_by_date',
    type: 'list',
    // Pagination: caller should increment `start` by `limit` until results < limit
    buildUrl: ({ startDate, endDate, start = 0, limit = 20, activityType = '' }) => {
      let url = `${API_BASE}/activitylist-service/activities/search/activities?startDate=${startDate}&endDate=${endDate}&start=${start}&limit=${limit}`;
      if (activityType) url += `&activityType=${activityType}`;
      return url;
    },
  },
  {
    name: 'goals',
    type: 'list',
    buildUrl: ({ status = 'active', start = 0, limit = 100 }) =>
      `${API_BASE}/goal-service/goal/goals?status=${status}&start=${start}&limit=${limit}&sortOrder=asc`,
  },
  {
    name: 'social_profile',
    type: 'list',
    buildUrl: () =>
      `${API_BASE}/userprofile-service/socialProfile`,
  },
];

// ---------------------------------------------------------------------------
// Build lookup map
// ---------------------------------------------------------------------------

const allEndpoints = [
  ...dailyEndpoints,
  ...aggregatedEndpoints,
  ...activityEndpoints,
  ...listEndpoints,
];

/** @type {Map<string, {name: string, type: string, buildUrl: function}>} */
const endpointMap = new Map();
for (const ep of allEndpoints) {
  endpointMap.set(ep.name, ep);
}

/**
 * Get an endpoint definition by name.
 * @param {string} name
 * @returns {{ name: string, type: string, buildUrl: function } | undefined}
 */
function getEndpoint(name) {
  return endpointMap.get(name);
}

/**
 * Get all endpoint names.
 * @returns {string[]}
 */
function getEndpointNames() {
  return allEndpoints.map((ep) => ep.name);
}

/**
 * Get endpoints filtered by type.
 * @param {'daily'|'aggregated'|'activity'|'list'} type
 * @returns {Array<{ name: string, type: string, buildUrl: function }>}
 */
function getEndpointsByType(type) {
  return allEndpoints.filter((ep) => ep.type === type);
}

module.exports = {
  getEndpoint,
  getEndpointNames,
  getEndpointsByType,
  // Re-export arrays for direct iteration
  dailyEndpoints,
  aggregatedEndpoints,
  activityEndpoints,
  listEndpoints,
  allEndpoints,
};
