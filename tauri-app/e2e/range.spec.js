// Date range: Equivalence Partitioning over the four range modes and the
// invalid classes of validateRange (electron-app/renderer/date-range.js), and
// 3-value Boundary Value Analysis on the 90-day span, the 1-day span and today.
import { test, expect } from './fixtures.js';
import { dayFromToday, FixedToday } from './support/fixed-today.js';

const TODAY = FixedToday.DATE;

test.beforeEach(async ({ app }) => { await app.open(); });

test.describe('presets', () => {
  const presets = [
    { id: 'EP-01', preset: '7D', from: '2026-10-09', cta: 'Pull 7 days' },
    { id: 'EP-02', preset: '30D', from: '2026-09-16', cta: 'Pull 30 days' },
    { id: 'EP-03', preset: '90D', from: '2026-07-18', cta: 'Pull 90 days' },
  ];
  for (const { id, preset, from, cta } of presets) {
    test(`${id} preset ${preset} ends today and offers "${cta}"`, async ({ app }) => {
      await app.range.choose(preset);
      await expect(app.range.preset(preset)).toHaveAttribute('aria-checked', 'true');
      await expect(app.range.readout).toHaveText(`${from} → ${TODAY}`);
      await expect(app.hero.ctaLabel).toHaveText(cta);
      await expect(app.hero.cta).toBeEnabled();
    });
  }
});

test.describe('custom range', () => {
  const ORDER = 'From must be on or before To.';
  const FUTURE = 'To cannot be in the future.';
  const FORMAT = 'Both From and To must be valid YYYY-MM-DD dates.';
  const cases = [
    { id: 'EP-04', what: 'valid 10-day range', from: '2026-10-01', to: '2026-10-10', cta: 'Pull 10 days' },
    { id: 'EP-05', what: 'inverted dates', from: '2026-10-10', to: '2026-10-01', error: ORDER },
    { id: 'EP-06', what: 'To in the future', from: '2026-10-01', to: '2026-11-20', error: FUTURE },
    { id: 'EP-07', what: 'empty From', from: '', to: '2026-10-10', error: FORMAT },
    { id: 'BVA-01', what: 'span 89 days', from: dayFromToday(-88), to: TODAY, cta: 'Pull 89 days' },
    { id: 'BVA-02', what: 'span 90 days (max)', from: dayFromToday(-89), to: TODAY, cta: 'Pull 90 days' },
    { id: 'BVA-03', what: 'span 91 days', from: dayFromToday(-90), to: TODAY, error: 'Range can cover at most 90 days (got 91).' },
    { id: 'BVA-04', what: 'span 2 days', from: '2026-10-09', to: '2026-10-10', cta: 'Pull 2 days' },
    { id: 'BVA-05', what: 'span 1 day (From = To)', from: '2026-10-10', to: '2026-10-10', cta: 'Pull 1 day' },
    { id: 'BVA-06', what: 'From one day after To', from: '2026-10-11', to: '2026-10-10', error: ORDER },
    { id: 'BVA-07', what: 'To yesterday', from: '2026-10-01', to: dayFromToday(-1), cta: 'Pull 14 days' },
    { id: 'BVA-08', what: 'To today', from: '2026-10-01', to: TODAY, cta: 'Pull 15 days' },
    { id: 'BVA-09', what: 'To tomorrow', from: '2026-10-01', to: dayFromToday(1), error: FUTURE },
  ];
  for (const c of cases) {
    test(`${c.id} ${c.what}: ${c.error ? 'rejected' : `"${c.cta}"`}`, async ({ app }) => {
      await app.range.custom(c.from, c.to);
      if (c.error) {
        await expect(app.range.error).toHaveText(c.error);
        await expect(app.hero.ctaLabel).toHaveText('Fix the range');
        await expect(app.hero.cta).toBeDisabled();
      } else {
        await expect(app.range.error).toBeHidden();
        await expect(app.hero.ctaLabel).toHaveText(c.cta);
        await expect(app.hero.cta).toBeEnabled();
      }
    });
  }
});
