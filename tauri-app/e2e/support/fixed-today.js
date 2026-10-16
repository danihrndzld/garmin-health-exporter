/**
 * Pins "today" for the date rules; timers and animation frames stay real, so
 * Playwright's actionability checks and the app's rAF animations behave as in the app.
 */
export class FixedToday {
  static DATE = '2026-10-15';

  #page;

  constructor(page) {
    this.#page = page;
  }

  async install() {
    await this.#page.clock.setFixedTime(new Date(`${FixedToday.DATE}T10:00:00`));
  }
}

/** YYYY-MM-DD `days` after (or before, if negative) FixedToday.DATE. */
export function dayFromToday(days) {
  const d = new Date(`${FixedToday.DATE}T12:00:00Z`);
  d.setUTCDate(d.getUTCDate() + days);
  return d.toISOString().slice(0, 10);
}
