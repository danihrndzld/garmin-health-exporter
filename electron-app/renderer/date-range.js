/**
 * Pure helpers for the Date Range section.
 *
 * Shared by the renderer (instant UI feedback) and the main process
 * (defense-in-depth on the `download-health` IPC boundary) so one function
 * governs what counts as a valid range and what error a user sees.
 *
 * All dates are `YYYY-MM-DD` strings parsed at local midnight — matching the
 * pattern used in `electron-app/garmin/cache.js` so range math stays TZ-safe.
 */

const DATE_RE = /^\d{4}-\d{2}-\d{2}$/;
const MS_PER_DAY = 86400000;

function isValidDateString(s) {
  if (typeof s !== 'string' || !DATE_RE.test(s)) return false;
  const d = new Date(s + 'T00:00:00');
  if (Number.isNaN(d.getTime())) return false;
  // Round-trip check catches values like 2026-02-30 that Date silently rolls forward.
  return toLocalYmd(d) === s;
}

function toLocalYmd(date) {
  const y = date.getFullYear();
  const m = String(date.getMonth() + 1).padStart(2, '0');
  const d = String(date.getDate()).padStart(2, '0');
  return `${y}-${m}-${d}`;
}

function parseYmd(s) {
  return new Date(s + 'T00:00:00');
}

function daysBetween(startStr, endStr) {
  const start = parseYmd(startStr).getTime();
  const end = parseYmd(endStr).getTime();
  return Math.round((end - start) / MS_PER_DAY) + 1;
}

/**
 * Validate an inclusive `[startDate, endDate]` range.
 *
 * Returns either:
 *   { ok: true, startDate, endDate, spanDays }
 * or:
 *   { ok: false, errorCode, message, meta }
 *
 * errorCodes:
 *   BAD_RANGE_FORMAT  — missing or not `YYYY-MM-DD`
 *   BAD_RANGE_ORDER   — `startDate` is after `endDate`
 *   BAD_RANGE_FUTURE  — `endDate` is after today
 *   BAD_RANGE_SPAN    — inclusive span < 1 or > `maxSpanDays`
 */
function validateRange({ startDate, endDate, maxSpanDays = 90, today } = {}) {
  if (!isValidDateString(startDate) || !isValidDateString(endDate)) {
    return {
      ok: false,
      errorCode: 'BAD_RANGE_FORMAT',
      message: 'Both From and To must be valid YYYY-MM-DD dates.',
      meta: { startDate, endDate },
    };
  }

  const todayStr = toLocalYmd(today instanceof Date ? today : new Date());

  if (startDate > endDate) {
    return {
      ok: false,
      errorCode: 'BAD_RANGE_ORDER',
      message: 'From must be on or before To.',
      meta: { startDate, endDate },
    };
  }

  if (endDate > todayStr) {
    return {
      ok: false,
      errorCode: 'BAD_RANGE_FUTURE',
      message: 'To cannot be in the future.',
      meta: { endDate, today: todayStr },
    };
  }

  const spanDays = daysBetween(startDate, endDate);

  if (spanDays < 1) {
    return {
      ok: false,
      errorCode: 'BAD_RANGE_SPAN',
      message: 'Range must cover at least one day.',
      meta: { spanDays, maxSpanDays },
    };
  }

  if (spanDays > maxSpanDays) {
    return {
      ok: false,
      errorCode: 'BAD_RANGE_SPAN',
      message: `Range can cover at most ${maxSpanDays} days (got ${spanDays}).`,
      meta: { spanDays, maxSpanDays },
    };
  }

  return { ok: true, startDate, endDate, spanDays };
}

/**
 * Seed values for the first switch into Custom Range mode so the user starts
 * from the same window they were already looking at on the slider.
 *
 *   daysBack = 7  →  { startDate: today - 6, endDate: today } (inclusive span = 7)
 */
function defaultCustomRange({ daysBack, today } = {}) {
  const n = Math.max(1, Math.floor(Number(daysBack) || 1));
  const base = today instanceof Date ? new Date(today.getTime()) : new Date();
  base.setHours(0, 0, 0, 0);
  const end = new Date(base.getTime());
  const start = new Date(base.getTime() - (n - 1) * MS_PER_DAY);
  return { startDate: toLocalYmd(start), endDate: toLocalYmd(end) };
}

/**
 * Human-readable span readout for the UI. Returns an empty string when the
 * range is not well-formed so callers can bind it directly to innerText.
 */
function formatSpan({ startDate, endDate } = {}) {
  if (!isValidDateString(startDate) || !isValidDateString(endDate)) return '';
  if (startDate > endDate) return '';
  const spanDays = daysBetween(startDate, endDate);
  const unit = spanDays === 1 ? 'day' : 'days';
  return `${spanDays} ${unit} · ${startDate} → ${endDate}`;
}

const api = {
  validateRange,
  defaultCustomRange,
  formatSpan,
  isValidDateString,
  toLocalYmd,
};

if (typeof module !== 'undefined' && module.exports) {
  module.exports = api;
}
if (typeof window !== 'undefined') {
  window.DateRange = api;
}
