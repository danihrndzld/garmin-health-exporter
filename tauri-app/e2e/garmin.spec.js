// Garmin Connect row. Decision Table over what lets a run start (connected ×
// Include), and State Transition Testing over the Setup row:
// Not connected ↔ Email login / API key / API key + email login → expired → reconnected.
// Oracles: docs/superpowers/specs/2026-10-16-go-core-garmin-design.md, "Connection model and UI",
// and workflows/personal_api.md (the API key is preferred; the email login adds training data).
import { test, expect } from './fixtures.js';
import { REJECTED_EMAIL, REJECTED_KEY, VALID_KEY, VALID_LOGIN } from './support/mock-backend.js';

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

  test('DT-R12 Garmin on but expired, MFP on: Garmin is skipped with the reason, MFP logs 17 of 30 days, the run is Done', async ({ app, scenario }) => {
    scenario.garminExpiredAtStart().mfpConnected();
    await app.open();
    await app.startExport();
    await app.waitForRunEnd();
    await expect(app.hero.badge).toHaveText('Done');
    await expect(app.run.row('auth').detail).toHaveText('Skipped: the Garmin session expired. Reconnect Garmin.');
    for (const phase of ['daily', 'aggregated', 'activities', 'csv']) await expect(app.run.row(phase).detail).toHaveText('Skipped');
    await expect(app.run.row('mfp').detail).toHaveText('Food logged on 17 of 30 days');
    await expect(app.garmin.state).toHaveText('Session expired');
  });

  test('DT-R13 API key only: the run reaches Done and the log names what the Personal API lacks', async ({ app, scenario }) => {
    scenario.garminNotConnected().garminKeyConnected();
    await app.open();
    await app.startExport();
    await app.waitForRunEnd();
    await expect(app.hero.badge).toHaveText('Done');
    await expect(app.run.logLines.filter({ hasText: "API key only: Garmin's Personal API has no training readiness" })).toHaveCount(1);
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
  test('ST-T15 Not connected → Email login after sign in; the password field is empty afterwards', async ({ app, scenario }) => {
    scenario.garminNotConnected();
    await app.open();
    await app.garmin.connect(VALID_LOGIN);
    await expect(app.toast).toHaveText('Garmin connected.');
    await expect(app.garmin.state).toHaveText('Email login');
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

  test('ST-T7 wrong password → Email login after fixing the login, then a run reaches Done', async ({ app, scenario }) => {
    scenario.garminNotConnected();
    await app.open();
    await app.garmin.connect({ ...VALID_LOGIN, email: REJECTED_EMAIL });
    await expect(app.garmin.error).toBeVisible();
    await app.garmin.signInWith(VALID_LOGIN);
    await expect(app.garmin.state).toHaveText('Email login');
    await app.startExport();
    await app.waitForRunEnd();
    await expect(app.hero.badge).toHaveText('Done');
  });

  test('ST-T16 Email login → Not connected on disconnect', async ({ app }) => {
    await app.open();
    await expect(app.garmin.state).toHaveText('Email login');
    await app.garmin.pressButton();
    await expect(app.garmin.state).toHaveText('Not connected');
    await expect(app.garmin.button).toHaveText('connect');
    await expect(app.garmin.include).toBeDisabled();
  });

  test('ST-T17 Email login → Session expired when a run ends with GARMIN_SESSION; reconnect → Email login', async ({ app, scenario }) => {
    scenario.garminSessionExpired();
    await app.open();
    await expect(app.garmin.state).toHaveText('Email login');
    await app.startExport();
    await app.waitForRunEnd();
    await expect(app.hero.badge).toHaveText('Failed');
    await expect(app.run.report).toBeHidden();
    await expect(app.garmin.state).toHaveText('Session expired');
    await expect(app.garmin.button).toHaveText('reconnect');
    await app.garmin.connect(VALID_LOGIN);
    await expect(app.garmin.state).toHaveText('Email login');
    await expect(app.garmin.button).toHaveText('disconnect');
  });

  test('ST-T22 start with the expired marker: Session expired, reconnect, Include on', async ({ app, scenario }) => {
    scenario.garminExpiredAtStart();
    await app.open();
    await expect(app.garmin.state).toHaveText('Session expired');
    await expect(app.garmin.button).toHaveText('reconnect');
    await expect(app.garmin.include).toBeEnabled();
    await expect(app.garmin.include).toBeChecked();
  });

  test('ST-T23 Not connected → API key: the key is saved, Include is on, the form closes and the key field is empty', async ({ app, scenario }) => {
    scenario.garminNotConnected();
    await app.open();
    await app.garmin.connectWithKey(VALID_KEY);
    await expect(app.toast).toHaveText('Garmin API key saved.');
    await expect(app.garmin.state).toHaveText('API key');
    await expect(app.garmin.button).toHaveText('disconnect');
    await expect(app.garmin.include).toBeChecked();
    await expect(app.garmin.form).toBeHidden();
    await expect(app.garmin.apiKey).toHaveValue('');
  });

  test('ST-T24 a key Garmin refuses: the form stays open, names where a new key comes from, and the state stays Not connected', async ({ app, scenario }) => {
    scenario.garminNotConnected();
    await app.open();
    await app.garmin.connectWithKey(REJECTED_KEY);
    await expect(app.garmin.error).toContainText('connect.garmin.com/app/settings/personalApiToken');
    await expect(app.garmin.form).toBeVisible();
    await expect(app.garmin.apiKey).toHaveValue('');
    await expect(app.garmin.state).toHaveText('Not connected');
  });

  test('ST-T25 API key → API key + email login through "add email login"; the link then hides', async ({ app, scenario }) => {
    scenario.garminNotConnected().garminKeyConnected();
    await app.open();
    await expect(app.garmin.state).toHaveText('API key');
    await expect(app.garmin.addOther).toHaveText('add email login');
    await app.garmin.addEmailLogin(VALID_LOGIN);
    await expect(app.garmin.state).toHaveText('API key + email login');
    await expect(app.garmin.addOther).toBeHidden();
    await expect(app.garmin.form).toBeHidden();
  });

  test('ST-T26 start with an expired key: API key expired, reconnect saves a new key → API key', async ({ app, scenario }) => {
    scenario.garminNotConnected().garminKeyExpiredAtStart();
    await app.open();
    await expect(app.garmin.state).toHaveText('API key expired');
    await expect(app.garmin.button).toHaveText('reconnect');
    await app.garmin.connectWithKey(VALID_KEY);
    await expect(app.garmin.state).toHaveText('API key');
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
    await app.garmin.openEmailForm();
    await app.garmin.connectWithEnter(VALID_LOGIN);
    await expect(app.garmin.state).toHaveText('Email login');
    await expect(app.hero.badge).toHaveText('Ready');
    await expect(app.toast).toHaveText('Garmin connected.');
  });

  test('EG-05 Enter pressed twice while signing in sends one sign in', async ({ app, scenario }) => {
    scenario.garminNotConnected();
    await app.open();
    await app.garmin.openEmailForm();
    await app.garmin.connectWithDoubleEnter(VALID_LOGIN);
    await expect(app.garmin.state).toHaveText('Email login');
    expect(await scenario.garminConnectCalls()).toBe(1);
  });

  // The spec's error table gives the "Report it" box to GARMIN_ERROR only: a 429 or a 403 is Garmin's normal answer.
  for (const [id, code, error] of [
    ['EG-09', 'GARMIN_RATE_LIMITED', 'try again in an hour'],
    ['EG-10', 'GARMIN_BLOCKED', 'Garmin refused the profile request'],
  ]) {
    test(`${id} a run that ends with ${code}: the error shows, no "Report it" box, the login stays`, async ({ app, scenario }) => {
      scenario[code === 'GARMIN_BLOCKED' ? 'garminExportBlocked' : 'garminExportRateLimited']();
      await app.open();
      await app.startExport();
      await app.waitForRunEnd();
      await expect(app.hero.badge).toHaveText('Failed');
      await expect(app.toast).toContainText(error);
      await expect(app.run.report).toBeHidden();
      await expect(app.garmin.state).toHaveText('Email login');
    });
  }
});
