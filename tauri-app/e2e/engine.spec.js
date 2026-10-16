// The export run on the Go core: `export` answers with file names only and the UI asks
// `summary` for the rest, so the run ends on the same screen as before.
import { test, expect } from './fixtures.js';

test('ST-T19 Running → Done with the csv count, the summary and the warnings in the pack', async ({ app }) => {
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

test('ST-T21 go engine with MyFitnessPal: the diary runs on the Go range, then the pack is built', async ({ app, scenario }) => {
  scenario.mfpConnected();
  await app.open();
  await app.startExport();
  await app.waitForRunEnd();
  await expect(app.hero.badge).toHaveText('Done');
  await expect(app.run.row('mfp').detail).toHaveText('Food logged on 17 of 30 days');
  await expect(app.stepsBars).toHaveCount(14);
  await expect(app.hero.llmButton).toBeVisible();
});

test('EG-11 go engine: when the summary of a later run fails, the previews drop the earlier run\'s data', async ({ app, scenario }) => {
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
