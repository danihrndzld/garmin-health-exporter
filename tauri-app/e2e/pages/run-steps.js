/** Run card: one row per phase, the error report box and the log. */
export class RunSteps {
  constructor(page) {
    this.list = page.locator('#rows');
    this.report = page.locator('#report');
    this.logCount = page.locator('#log-n');
    this.logLines = page.locator('#log > div');
  }

  /** Row of a phase: auth, daily, aggregated, activities, csv or mfp. */
  row(phase) {
    const li = this.list.locator(`li[data-phase="${phase}"]`);
    return { root: li, icon: li.locator('.st'), detail: li.locator('.sub'), time: li.locator('.t') };
  }

  async status(phase) {
    return this.row(phase).root.getAttribute('data-st');
  }
}
