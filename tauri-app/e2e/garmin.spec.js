// Garmin Connect row. Decision Table over what lets a run start (connected ×
// Include), and State Transition Testing over the Setup row:
// Not connected ↔ Connected → Session expired → Connected.
// Oracle: docs/superpowers/specs/2026-10-16-go-core-garmin-design.md, "Connection model and UI".
import { test, expect } from './fixtures.js';
import { REJECTED_EMAIL, VALID_LOGIN } from './support/mock-backend.js';

test.describe('decision table: can a run start', () => {
  test('DT-R6 not connected: Include off and disabled, Pull asks to switch on a source', async ({ app, scenario }) => {
    scenario.garminNotConnected();
    await app.open();
    await expect(app.garmin.state).toHaveText('Not connected');
    await expect(app.garmin.include).toBeDisabled();
    await expect(app.garmin.include).not.toBeChecked();
    await app.startExport();
    await expect(app.toast).toHaveText('Switch on and connect Garmin or MyFitnessPal first.');
    await expect(app.hero.badge).toHaveText('Ready');
  });

  test('DT-R7 Garmin off, MFP on: the Garmin rows say Off, MFP logs 17 of 30 days, the LLM pack is offered', async ({ app, scenario }) => {
    scenario.mfpConnected();
    await app.open();
    await app.garmin.include.uncheck();
    await app.startExport();
    await app.waitForRunEnd();
    await expect(app.hero.badge).toHaveText('Done');
    for (const phase of ['auth', 'daily', 'aggregated', 'activities', 'csv']) {
      await expect(app.run.row(phase).detail).toHaveText('Off');
      await expect(app.run.row(phase).root).not.toHaveAttribute('data-st');
    }
    await expect(app.hero.legend('daily')).toHaveText('—');
    await expect(app.run.row('mfp').detail).toHaveText('Food logged on 17 of 30 days');
    await expect(app.hero.llmButton).toBeVisible();
  });

  test('DT-R8 Garmin off, MFP not connected: Pull asks to switch on a source and the app stays Ready', async ({ app }) => {
    await app.open();
    await app.garmin.include.uncheck();
    await app.startExport();
    await expect(app.toast).toHaveText('Switch on and connect Garmin or MyFitnessPal first.');
    await expect(app.hero.badge).toHaveText('Ready');
  });
});

test.describe('state transitions: Setup row', () => {
  test('ST-T15 Not connected → Connected after sign in; the password field is empty afterwards', async ({ app, scenario }) => {
    scenario.garminNotConnected();
    await app.open();
    await app.garmin.connect(VALID_LOGIN);
    await expect(app.toast).toHaveText('Garmin connected.');
    await expect(app.garmin.state).toHaveText('Connected');
    await expect(app.garmin.button).toHaveText('disconnect');
    await expect(app.garmin.include).toBeChecked();
    await expect(app.garmin.form).toBeHidden();
    await expect(app.garmin.password).toHaveValue('');
  });

  test('ST-T5 wrong password: the form stays open and says "Wrong email or password."', async ({ app, scenario }) => {
    scenario.garminNotConnected();
    await app.open();
    await app.garmin.connect({ ...VALID_LOGIN, email: REJECTED_EMAIL });
    await expect(app.garmin.error).toHaveText('Wrong email or password.');
    await expect(app.garmin.form).toBeVisible();
    await expect(app.garmin.password).toHaveValue('');
    await expect(app.garmin.state).toHaveText('Not connected');
  });

  test('ST-T7 wrong password → Connected after fixing the login, then a run reaches Done', async ({ app, scenario }) => {
    scenario.garminNotConnected();
    await app.open();
    await app.garmin.connect({ ...VALID_LOGIN, email: REJECTED_EMAIL });
    await expect(app.garmin.error).toBeVisible();
    await app.garmin.connect(VALID_LOGIN);
    await expect(app.garmin.state).toHaveText('Connected');
    await app.startExport();
    await app.waitForRunEnd();
    await expect(app.hero.badge).toHaveText('Done');
  });

  test('ST-T16 Connected → Not connected on disconnect', async ({ app }) => {
    await app.open();
    await expect(app.garmin.state).toHaveText('Connected');
    await app.garmin.pressButton();
    await expect(app.garmin.state).toHaveText('Not connected');
    await expect(app.garmin.button).toHaveText('connect');
    await expect(app.garmin.include).toBeDisabled();
  });

  test('ST-T17 Connected → Session expired when a run ends with GARMIN_SESSION; reconnect → Connected', async ({ app, scenario }) => {
    scenario.garminSessionExpired();
    await app.open();
    await expect(app.garmin.state).toHaveText('Connected');
    await app.startExport();
    await app.waitForRunEnd();
    await expect(app.hero.badge).toHaveText('Failed');
    await expect(app.run.report).toBeHidden();
    await expect(app.garmin.state).toHaveText('Session expired');
    await expect(app.garmin.button).toHaveText('reconnect');
    await app.garmin.connect(VALID_LOGIN);
    await expect(app.garmin.state).toHaveText('Connected');
    await expect(app.garmin.button).toHaveText('disconnect');
  });

  test('ST-I10 Running: the Garmin button and Include switch are disabled', async ({ app }) => {
    await app.open();
    await app.startExport();
    await expect(app.garmin.button).toBeDisabled();
    await expect(app.garmin.include).toBeDisabled();
  });
});

test.describe('error guessing', () => {
  test('EG-04 Enter in the password field signs in and starts no run', async ({ app, scenario }) => {
    scenario.garminNotConnected();
    await app.open();
    await app.garmin.pressButton();
    await app.garmin.connectWithEnter(VALID_LOGIN);
    await expect(app.garmin.state).toHaveText('Connected');
    await expect(app.hero.badge).toHaveText('Ready');
    await expect(app.toast).toHaveText('Garmin connected.');
  });

  test('EG-05 Enter pressed twice while signing in sends one sign in', async ({ app, scenario }) => {
    scenario.garminNotConnected();
    await app.open();
    await app.garmin.pressButton();
    await app.garmin.connectWithDoubleEnter(VALID_LOGIN);
    await expect(app.garmin.state).toHaveText('Connected');
    expect(await scenario.garminConnectCalls()).toBe(1);
  });
});
