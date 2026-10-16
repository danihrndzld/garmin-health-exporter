// MyFitnessPal. Decision Table over what decides the MFP step of a run
// (connected × include × session valid × cancel during MFP), and State
// Transition Testing over the Setup row: Not connected ↔ Connected → Session expired.
// Oracle: docs/superpowers/specs/2026-10-14-go-core-mfp-design.md (UI section and
// "A failure in MFP never changes the result of the Garmin export").
import { test, expect } from './fixtures.js';

test.describe('decision table: MFP step of a run', () => {
  test('DT-R1 not connected: no MFP step, Include off and disabled', async ({ app }) => {
    await app.open();
    await expect(app.mfp.include).toBeDisabled();
    await expect(app.mfp.include).not.toBeChecked();
    await app.startExport();
    await app.waitForRunEnd();
    await expect(app.hero.badge).toHaveText('Done');
    await expect(app.run.row('mfp').root).toBeHidden();
    await expect(app.hero.ring('mfp')).toBeHidden();
  });

  test('DT-R2 connected, Include off: no MFP step, no MFP ring', async ({ app, scenario }) => {
    scenario.mfpConnected();
    await app.open();
    await app.mfp.include.uncheck();
    await app.startExport();
    await app.waitForRunEnd();
    await expect(app.hero.badge).toHaveText('Done');
    await expect(app.run.row('mfp').root).toBeHidden();
    await expect(app.hero.ring('mfp')).toBeHidden();
    await expect(app.hero.legend('mfp')).toBeHidden();
  });

  test('DT-R3 connected, included, valid session: food logged on 17 of 30 days', async ({ app, scenario }) => {
    scenario.mfpConnected();
    await app.open();
    await app.startExport();
    await app.waitForRunEnd();
    await expect(app.hero.badge).toHaveText('Done');
    await expect(app.run.row('mfp').root).toHaveAttribute('data-st', 'ok');
    await expect(app.run.row('mfp').detail).toHaveText('Food logged on 17 of 30 days');
    await expect(app.hero.legend('mfp')).toHaveText('17/30');
    await expect(app.hero.ring('mfp')).toBeVisible();
  });

  test('DT-R4 connected, included, expired session: MFP skipped, Garmin still Done', async ({ app, scenario }) => {
    scenario.mfpConnected().mfpSessionExpired();
    await app.open();
    await app.startExport();
    await app.waitForRunEnd();
    await expect(app.hero.badge).toHaveText('Done');
    await expect(app.run.row('mfp').root).toHaveAttribute('data-st', 'warn');
    await expect(app.run.row('mfp').detail).toHaveText('Skipped: MyFitnessPal session expired');
    await expect(app.hero.legend('mfp')).toHaveText('skipped');
    await expect(app.hero.llmButton).toBeVisible();
  });

  test('DT-R5 connected, included, valid, cancelled during MFP: MFP cancelled, Garmin still Done', async ({ app, scenario }) => {
    scenario.mfpConnected();
    await app.open();
    await app.startExport();
    await app.waitForPhase('mfp');
    await app.pressEscape();
    await app.waitForRunEnd();
    await expect(app.hero.badge).toHaveText('Done');
    await expect(app.run.row('mfp').detail).toHaveText('Cancelled.');
    await expect(app.hero.legend('mfp')).toHaveText('cancelled');
    for (const phase of ['daily', 'csv']) await expect(app.run.row(phase).root).toHaveAttribute('data-st', /^(ok|warn)$/);
  });

  test('DT-R9 Garmin off, MFP session expired: the run fails and the MFP row says Session expired', async ({ app, scenario }) => {
    scenario.mfpConnected().mfpSessionExpired();
    await app.open();
    await app.garmin.include.uncheck();
    await app.startExport();
    await app.waitForRunEnd();
    await expect(app.hero.badge).toHaveText('Failed');
    await expect(app.run.row('mfp').root).toHaveAttribute('data-st', 'err');
    await expect(app.mfp.state).toHaveText('Session expired');
    await expect(app.mfp.button).toHaveText('reconnect');
    await expect(app.run.report).toBeHidden();
    await expect(app.hero.legend('mfp')).toHaveText('failed');
    await expect(app.run.logLines.filter({ hasText: 'skipped' })).toHaveCount(0);
  });

  test('DT-R10 Garmin on, MFP session already expired: no MFP request, the MFP row says skipped, the run is Done', async ({ app, scenario }) => {
    scenario.mfpConnected().mfpSessionExpired();
    await app.open();
    await app.startExport();
    await app.waitForRunEnd();
    await expect(app.mfp.state).toHaveText('Session expired');
    expect(await scenario.mfpExportCalls()).toBe(1);
    await app.startExport();
    await app.waitForRunEnd();
    await expect(app.hero.badge).toHaveText('Done');
    expect(await scenario.mfpExportCalls()).toBe(1);
    await expect(app.run.row('mfp').root).toHaveAttribute('data-st', 'warn');
    await expect(app.run.row('mfp').detail).toHaveText('Skipped: the MyFitnessPal session expired. Reconnect MyFitnessPal.');
    await expect(app.hero.legend('mfp')).toHaveText('skipped');
  });
});

test.describe('state transitions: Setup row', () => {
  test('ST-T9 Not connected → Connected after sign in', async ({ app }) => {
    await app.open();
    await expect(app.mfp.state).toHaveText('Not connected');
    await app.mfp.pressButton();
    await expect(app.mfp.state).toHaveText('Connected');
    await expect(app.mfp.button).toHaveText('disconnect');
    await expect(app.mfp.include).toBeChecked();
    await expect(app.run.row('mfp').root).toBeVisible();
  });

  test('ST-T10 Connected → Not connected on disconnect', async ({ app, scenario }) => {
    scenario.mfpConnected();
    await app.open();
    await app.mfp.pressButton();
    await expect(app.toast).toHaveText('MyFitnessPal disconnected.');
    await expect(app.mfp.state).toHaveText('Not connected');
    await expect(app.mfp.include).toBeDisabled();
    await expect(app.run.row('mfp').root).toBeHidden();
  });

  test('ST-T11 Connected → Session expired when a run finds the session dead', async ({ app, scenario }) => {
    scenario.mfpConnected().mfpSessionExpired();
    await app.open();
    await expect(app.mfp.state).toHaveText('Connected');
    await app.startExport();
    await app.waitForRunEnd();
    await expect(app.mfp.state).toHaveText('Session expired');
    await expect(app.mfp.button).toHaveText('reconnect');
  });

  test('ST-T12 Session expired → Connected after reconnecting', async ({ app, scenario }) => {
    scenario.mfpConnected().mfpSessionExpired();
    await app.open();
    await app.startExport();
    await app.waitForRunEnd();
    await app.mfp.pressButton();
    await expect(app.mfp.state).toHaveText('Connected');
    await expect(app.mfp.button).toHaveText('disconnect');
  });

  test('ST-I7 Running + Include toggle: disabled', async ({ app, scenario }) => {
    scenario.mfpConnected();
    await app.open();
    await app.startExport();
    await expect(app.mfp.include).toBeDisabled();
    await expect(app.mfp.button).toBeDisabled();
  });

  test('ST-I9 Esc while the pack refreshes after MFP: the run ends Done and MFP stays ok', async ({ app, scenario }) => {
    scenario.mfpConnected().summaryHangs();
    await app.open();
    await app.startExport();
    await app.waitForPhase('mfp', 'ok');
    await expect(app.hero.badge).toHaveText('Running');
    await app.pressEscape();
    await app.waitForRunEnd();
    await expect(app.hero.badge).toHaveText('Done');
    await expect(app.run.row('mfp').detail).toHaveText('Food logged on 17 of 30 days');
    await expect(app.hero.legend('mfp')).toHaveText('17/30');
  });
});
