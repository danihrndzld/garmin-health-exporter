const assert = require('node:assert/strict');
const { test } = require('node:test');
const {
  validateRange,
  defaultCustomRange,
  formatSpan,
  isValidDateString,
  toLocalYmd,
} = require('../date-range');

const TODAY = new Date('2026-04-22T00:00:00');

test('isValidDateString accepts YYYY-MM-DD and rejects malformed or impossible dates', () => {
  assert.equal(isValidDateString('2026-04-22'), true);
  assert.equal(isValidDateString('2024-02-29'), true); // leap day
  assert.equal(isValidDateString('2026-02-30'), false); // Date would roll forward
  assert.equal(isValidDateString('2026-13-01'), false);
  assert.equal(isValidDateString('2026-4-22'), false); // missing zero-pad
  assert.equal(isValidDateString(''), false);
  assert.equal(isValidDateString(null), false);
  assert.equal(isValidDateString(20260422), false);
});

test('validateRange accepts a single-day range (start == end)', () => {
  const res = validateRange({ startDate: '2026-04-22', endDate: '2026-04-22', today: TODAY });
  assert.equal(res.ok, true);
  assert.equal(res.spanDays, 1);
});

test('validateRange accepts a 90-day inclusive range at the ceiling', () => {
  const res = validateRange({ startDate: '2026-01-23', endDate: '2026-04-22', today: TODAY });
  assert.equal(res.ok, true);
  assert.equal(res.spanDays, 90);
});

test('validateRange rejects a 91-day span with BAD_RANGE_SPAN', () => {
  const res = validateRange({ startDate: '2026-01-22', endDate: '2026-04-22', today: TODAY });
  assert.equal(res.ok, false);
  assert.equal(res.errorCode, 'BAD_RANGE_SPAN');
  assert.equal(res.meta.spanDays, 91);
  assert.equal(res.meta.maxSpanDays, 90);
});

test('validateRange rejects start > end with BAD_RANGE_ORDER', () => {
  const res = validateRange({ startDate: '2026-04-22', endDate: '2026-04-10', today: TODAY });
  assert.equal(res.ok, false);
  assert.equal(res.errorCode, 'BAD_RANGE_ORDER');
});

test('validateRange rejects end in the future with BAD_RANGE_FUTURE', () => {
  const res = validateRange({ startDate: '2026-04-22', endDate: '2026-04-23', today: TODAY });
  assert.equal(res.ok, false);
  assert.equal(res.errorCode, 'BAD_RANGE_FUTURE');
});

test('validateRange rejects malformed inputs with BAD_RANGE_FORMAT', () => {
  assert.equal(validateRange({ startDate: '2026-4-1', endDate: '2026-04-22', today: TODAY }).errorCode, 'BAD_RANGE_FORMAT');
  assert.equal(validateRange({ startDate: null, endDate: '2026-04-22', today: TODAY }).errorCode, 'BAD_RANGE_FORMAT');
  assert.equal(validateRange({ startDate: '', endDate: '', today: TODAY }).errorCode, 'BAD_RANGE_FORMAT');
  assert.equal(validateRange({ startDate: '2026-02-30', endDate: '2026-03-01', today: TODAY }).errorCode, 'BAD_RANGE_FORMAT');
});

test('validateRange honors a custom maxSpanDays cap', () => {
  const ok = validateRange({ startDate: '2026-04-16', endDate: '2026-04-22', maxSpanDays: 7, today: TODAY });
  assert.equal(ok.ok, true);
  assert.equal(ok.spanDays, 7);

  const tooLong = validateRange({ startDate: '2026-04-15', endDate: '2026-04-22', maxSpanDays: 7, today: TODAY });
  assert.equal(tooLong.ok, false);
  assert.equal(tooLong.errorCode, 'BAD_RANGE_SPAN');
});

test('validateRange defaults today to real now when not provided', () => {
  // Yesterday relative to real now is always valid; far-future is always rejected.
  const now = new Date();
  const yesterday = new Date(now.getTime() - 86400000);
  const start = toLocalYmd(yesterday);
  const end = toLocalYmd(yesterday);
  assert.equal(validateRange({ startDate: start, endDate: end }).ok, true);

  const farFuture = validateRange({ startDate: '2999-01-01', endDate: '2999-01-02' });
  assert.equal(farFuture.ok, false);
  assert.equal(farFuture.errorCode, 'BAD_RANGE_FUTURE');
});

test('defaultCustomRange seeds the window to match the current slider position', () => {
  assert.deepEqual(
    defaultCustomRange({ daysBack: 7, today: TODAY }),
    { startDate: '2026-04-16', endDate: '2026-04-22' },
  );
  assert.deepEqual(
    defaultCustomRange({ daysBack: 1, today: TODAY }),
    { startDate: '2026-04-22', endDate: '2026-04-22' },
  );
  assert.deepEqual(
    defaultCustomRange({ daysBack: 90, today: TODAY }),
    { startDate: '2026-01-23', endDate: '2026-04-22' },
  );
});

test('defaultCustomRange clamps invalid daysBack to 1', () => {
  assert.deepEqual(
    defaultCustomRange({ daysBack: 0, today: TODAY }),
    { startDate: '2026-04-22', endDate: '2026-04-22' },
  );
  assert.deepEqual(
    defaultCustomRange({ daysBack: -5, today: TODAY }),
    { startDate: '2026-04-22', endDate: '2026-04-22' },
  );
  assert.deepEqual(
    defaultCustomRange({ daysBack: 'banana', today: TODAY }),
    { startDate: '2026-04-22', endDate: '2026-04-22' },
  );
});

test('formatSpan returns human-readable span with correct plural', () => {
  assert.equal(
    formatSpan({ startDate: '2026-04-22', endDate: '2026-04-22' }),
    '1 day · 2026-04-22 → 2026-04-22',
  );
  assert.equal(
    formatSpan({ startDate: '2026-03-23', endDate: '2026-04-22' }),
    '31 days · 2026-03-23 → 2026-04-22',
  );
});

test('formatSpan returns empty string for invalid or inverted ranges', () => {
  assert.equal(formatSpan({ startDate: 'bad', endDate: '2026-04-22' }), '');
  assert.equal(formatSpan({ startDate: '2026-04-22', endDate: '2026-04-10' }), '');
  assert.equal(formatSpan({}), '');
});

test('toLocalYmd uses local-time components (no UTC shift)', () => {
  const d = new Date(2026, 3, 22, 23, 59, 59); // Apr 22 2026 23:59:59 local
  assert.equal(toLocalYmd(d), '2026-04-22');
});
