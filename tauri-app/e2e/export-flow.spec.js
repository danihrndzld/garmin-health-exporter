// Export run lifecycle, State Transition Testing over the hero states
// Ready → Running → Done | Cancelled | Failed, one invalid event per ST-I test,
// plus Error Guessing on inputs the mock makes cheap to reach.
import { test, expect } from './fixtures.js';
import { REJECTED_EMAIL, VALID_LOGIN } from './support/mock-backend.js';

const GARMIN_PHASES = ['auth', 'daily', 'aggregated', 'activities', 'csv'];

test.describe('valid transitions', () => {
  test.beforeEach(async ({ app }) => { await app.open(); });

  test('ST-T1 Ready → Running on submit: Cancel offered, setup locked', async ({ app }) => {
    await app.startExport(VALID_LOGIN);
    await expect(app.hero.badge).toHaveText('Running');
    await expect(app.hero.ctaLabel).toHaveText('Cancel');
    await expect(app.setup.email).toBeDisabled();
    await expect(app.range.preset('90D')).toBeDisabled();
  });

  test('ST-T2 Running → Done: 30 days, every Garmin row finished, LLM pack offered', async ({ app }) => {
    await app.startExport(VALID_LOGIN);
    await app.waitForRunEnd();
    await expect(app.hero.badge).toHaveText('Done');
    await expect(app.hero.headline).toHaveText('30 days');
    await expect(app.hero.centerNumber).toHaveText('30');
    await expect(app.hero.centerLabel).toHaveText('days');
    await expect(app.hero.legend('daily')).toHaveText('30/30');
    await expect(app.hero.legend('activities')).toHaveText('41/41');
    for (const phase of GARMIN_PHASES) await expect(app.run.row(phase).root).toHaveAttribute('data-st', /^(ok|warn)$/);
    await expect(app.hero.ctaLabel).toHaveText('Pull again');
    await expect(app.hero.llmButton).toBeVisible();
  });

  test('ST-T3 Running → Cancelled with Esc: cache kept', async ({ app }) => {
    await app.startExport(VALID_LOGIN);
    await app.waitForPhase('daily');
    await app.pressEscape();
    await app.waitForRunEnd();
    await expect(app.hero.badge).toHaveText('Cancelled');
    await expect(app.hero.headline).toHaveText('Cancelled.');
    await expect(app.hero.subline).toHaveText('cache kept.');
  });

  test('ST-T4 Running → Cancelled with the Cancel button', async ({ app }) => {
    await app.startExport(VALID_LOGIN);
    await app.waitForPhase('aggregated');
    await app.hero.pressCta();
    await app.waitForRunEnd();
    await expect(app.hero.badge).toHaveText('Cancelled');
    await expect(app.run.row('aggregated').root).toHaveAttribute('data-st', 'warn');
  });

  test('ST-T5 Running → Failed on a rejected login: report box shown', async ({ app }) => {
    await app.startExport({ ...VALID_LOGIN, email: REJECTED_EMAIL });
    await app.waitForRunEnd();
    await expect(app.hero.badge).toHaveText('Failed');
    await expect(app.hero.headline).toHaveText('Stopped.');
    await expect(app.run.report).toBeVisible();
  });

  test('ST-T6 Done → Running with Pull again', async ({ app }) => {
    await app.startExport(VALID_LOGIN);
    await app.waitForRunEnd();
    await app.hero.pressCta();
    await expect(app.hero.badge).toHaveText('Running');
    await expect(app.hero.legend('daily')).not.toHaveText('30/30');
  });

  test('ST-T7 Failed → Running → Done after fixing the login', async ({ app }) => {
    await app.startExport({ ...VALID_LOGIN, email: REJECTED_EMAIL });
    await app.waitForRunEnd();
    await app.startExport(VALID_LOGIN);
    await expect(app.run.report).toBeHidden();
    await app.waitForRunEnd();
    await expect(app.hero.badge).toHaveText('Done');
  });

  test('ST-T8 Cancelled → Running with Pull again', async ({ app }) => {
    await app.startExport(VALID_LOGIN);
    await app.waitForPhase('daily');
    await app.pressEscape();
    await app.waitForRunEnd();
    await app.hero.pressCta();
    await expect(app.hero.badge).toHaveText('Running');
  });
});

test.describe('invalid events (one per test)', () => {
  test.beforeEach(async ({ app }) => { await app.open(); });

  test('ST-I1 Ready + submit without password: stays Ready, asks for credentials', async ({ app }) => {
    await app.startExport({ ...VALID_LOGIN, password: '' });
    await expect(app.toast).toHaveText('Email and password are required.');
    await expect(app.hero.badge).toHaveText('Ready');
    await expect(app.setup.password).toBeFocused();
  });

  test('ST-I2 Running + start shortcut: no second run', async ({ app }) => {
    await app.startExport(VALID_LOGIN);
    await app.waitForPhase('daily');
    await app.pressStartShortcut();
    await app.waitForRunEnd();
    await expect(app.hero.badge).toHaveText('Done');
    await expect(app.run.logLines.filter({ hasText: 'Last 30 days' })).toHaveCount(1);
  });

  test('ST-I3 Ready + Esc: stays Ready', async ({ app }) => {
    await app.pressEscape();
    await expect(app.hero.badge).toHaveText('Ready');
    await expect(app.hero.ctaLabel).toHaveText('Pull 30 days');
  });

  test('ST-I4 Running + range preset click: range unchanged', async ({ app }) => {
    await app.startExport(VALID_LOGIN);
    await app.range.preset('90D').click({ force: true });
    await expect(app.range.preset('30D')).toHaveAttribute('aria-checked', 'true');
    await expect(app.range.preset('90D')).toHaveAttribute('aria-checked', 'false');
  });

  test('ST-I5 Ready before any export + L: no LLM pack sheet', async ({ app }) => {
    await app.sheet.openWithShortcut();
    await expect(app.sheet.dialog).toBeHidden();
  });

  test('ST-I6 Done + Esc: stays Done', async ({ app }) => {
    await app.startExport(VALID_LOGIN);
    await app.waitForRunEnd();
    await app.pressEscape();
    await expect(app.hero.badge).toHaveText('Done');
  });
});

test.describe('error guessing', () => {
  test('EG-01 range with 0 activities: "No activities" and 0 in the legend', async ({ app, scenario }) => {
    scenario.activities(0);
    await app.open();
    await app.startExport(VALID_LOGIN);
    await app.waitForRunEnd();
    await expect(app.hero.badge).toHaveText('Done');
    await expect(app.run.row('activities').detail).toHaveText('No activities');
    await expect(app.hero.legend('activities')).toHaveText('0');
  });

  test('EG-02 email with surrounding spaces is accepted', async ({ app }) => {
    await app.open();
    await app.startExport({ ...VALID_LOGIN, email: `  ${VALID_LOGIN.email}  ` });
    await expect(app.hero.badge).toHaveText('Running');
  });
});
