// Layout checklist (Checklist-based testing) at the default window (1000×720)
// and the minimum window (860×620) from src-tauri/tauri.conf.json, with
// MyFitnessPal on, the busiest layout: six Run rows and five rings.
import { test, expect } from './fixtures.js';
import { VALID_LOGIN } from './support/mock-backend.js';

const WINDOWS = [
  { name: 'default 1000×720', width: 1000, height: 720, ids: ['CL-01', 'CL-02', 'CL-03', 'CL-04', 'CL-05', 'CL-06', 'CL-07', 'CL-08'] },
  { name: 'minimum 860×620', width: 860, height: 620, ids: ['CL-11', 'CL-12', 'CL-13', 'CL-14', 'CL-15', 'CL-16', 'CL-17', 'CL-18'] },
];

/** Opens the app with MFP connected and runs until `moment`: idle, mfp (MFP step running) or done. */
async function reach(app, scenario, moment) {
  scenario.mfpConnected();
  await app.open();
  if (moment === 'idle') return;
  await app.startExport(VALID_LOGIN);
  if (moment === 'mfp') await app.waitForPhase('mfp');
  else await app.waitForRunEnd();
}

for (const win of WINDOWS) {
  const [rows, rings, center, spinner, cta, donut, stats, range] = win.ids;

  test.describe(win.name, () => {
    test.use({ viewport: { width: win.width, height: win.height } });

    test(`${rows} six Run rows fit without scrolling`, async ({ app, scenario, layout }) => {
      await reach(app, scenario, 'mfp');
      expect(await layout.overflowY(app.run.list)).toBeLessThanOrEqual(0);
    });

    test(`${rings} five rings are round, visible and do not overlap`, async ({ app, scenario, layout }) => {
      await reach(app, scenario, 'mfp');
      const drawn = await layout.rings(app.hero.root);
      expect(drawn.map((r) => r.phase)).toEqual(['daily', 'aggregated', 'activities', 'csv', 'mfp']);
      for (const r of drawn) expect(Math.abs(r.diameter - r.height)).toBeLessThan(1);
      for (let i = 1; i < drawn.length; i++) {
        expect(drawn[i - 1].diameter - drawn[i].diameter, `${drawn[i - 1].phase} vs ${drawn[i].phase}`).toBeGreaterThanOrEqual(2 * drawn[i].stroke);
      }
    });

    test(`${center} ring center number and label fit inside the innermost ring`, async ({ app, scenario, layout }) => {
      await reach(app, scenario, 'mfp');
      const inner = (await layout.rings(app.hero.root)).at(-1);
      const hole = inner.diameter - 2 * inner.stroke;
      const label = await layout.box(app.hero.centerLabel);
      const number = await layout.box(app.hero.centerNumber);
      expect(label.width).toBeLessThan(hole);
      expect(number.height + label.height).toBeLessThan(hole);
    });

    test(`${spinner} running row icon is a circle`, async ({ app, scenario, layout }) => {
      await reach(app, scenario, 'mfp');
      const icon = app.run.row('mfp').icon;
      const box = await layout.box(icon);
      expect(box.width).toBeCloseTo(box.height, 0);
      expect(await layout.borderRadius(icon)).toBe('50%');
    });

    test(`${cta} Pull CTA sits fully inside the window`, async ({ app, scenario, layout }) => {
      await reach(app, scenario, 'idle');
      const box = await layout.box(app.hero.cta);
      expect(box.y + box.height).toBeLessThanOrEqual(win.height);
      await expect(app.hero.cta).toBeInViewport({ ratio: 1 });
    });

    test(`${donut} Recovery donut is round and holds its score`, async ({ app, scenario, layout }) => {
      await reach(app, scenario, 'done');
      const ring = await layout.box(app.recovery.donut);
      const score = await layout.box(app.recovery.score);
      expect(Math.abs(ring.width - ring.height)).toBeLessThan(1);
      expect(ring.width).toBeGreaterThanOrEqual(56);
      expect(score.width).toBeLessThan(ring.width * 0.76); // the hole is 76% of the donut
    });

    test(`${stats} Recovery stats take one line each`, async ({ app, scenario, layout }) => {
      await reach(app, scenario, 'done');
      const items = await app.recovery.stats.all();
      expect(items).toHaveLength(3);
      for (const li of items) expect(await layout.isSingleLine(li)).toBe(true);
    });

    test(`${range} Range readout never breaks inside a date`, async ({ app, scenario, layout }) => {
      await reach(app, scenario, 'idle');
      const halves = await app.range.readoutHalves.all();
      expect(halves).toHaveLength(2);
      for (const half of halves) expect(await layout.isSingleLine(half)).toBe(true);
    });
  });
}
