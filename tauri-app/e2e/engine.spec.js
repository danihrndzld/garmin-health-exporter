// Export engine switch (ge_engine), Equivalence Partitioning over the two
// engines. Oracle: the design spec, "Engine switch and fallback": Bun answers
// `export` with summary and pack, Go with file names only, and the UI asks Bun's
// `summary` for the rest, so both engines end on the same screen.
import { test, expect } from './fixtures.js';

for (const [id, engine] of [['ST-T19', 'go'], ['ST-T20', 'bun']]) {
  test(`${id} ${engine} engine: Running → Done with the csv count, the summary and the warnings in the pack`, async ({ app, scenario }) => {
    scenario.engine(engine);
    await app.open();
    await app.startExport();
    await app.waitForRunEnd();
    await expect(app.hero.badge).toHaveText('Done');
    await expect(app.run.row('csv').detail).toHaveText(/^10 files/);
    await expect(app.stepsBars).toHaveCount(14);
    await expect(app.hero.llmButton).toBeVisible();
    await app.sheet.openWithShortcut();
    await expect(app.sheet.context).toContainText('hill_score skipped (404)');
  });
}

test('ST-T21 go engine with MyFitnessPal: the diary runs on the Go range, then the pack is built', async ({ app, scenario }) => {
  scenario.engine('go').mfpConnected();
  await app.open();
  await app.startExport();
  await app.waitForRunEnd();
  await expect(app.hero.badge).toHaveText('Done');
  await expect(app.run.row('mfp').detail).toHaveText('Food logged on 17 of 30 days');
  await expect(app.stepsBars).toHaveCount(14);
  await expect(app.hero.llmButton).toBeVisible();
});

test('EG-11 go engine: when the summary of a later run fails, the previews drop the earlier run\'s data', async ({ app, scenario }) => {
  scenario.engine('go');
  await app.open();
  await app.startExport();
  await app.waitForRunEnd();
  await expect(app.stepsBars).toHaveCount(14);
  await scenario.summaryStartsFailing();
  await app.startExport();
  await app.waitForRunEnd();
  await expect(app.hero.badge).toHaveText('Done');
  for (const preview of await app.previews.all()) await expect(preview).toHaveAttribute('data-empty', '');
});
