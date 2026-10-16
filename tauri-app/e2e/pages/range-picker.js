/** Range segment in the title bar + the Range field of the Setup card. */
export class RangePicker {
  #page;

  constructor(page) {
    this.#page = page;
    this.readout = page.locator('#range-read');
    this.readoutHalves = page.locator('#range-read span');
    this.error = page.locator('#range-err');
    this.from = page.getByLabel('From', { exact: true });
    this.to = page.getByLabel('To', { exact: true });
  }

  preset(label) {
    return this.#page.locator('#range-seg').getByRole('radio', { name: label, exact: true });
  }

  async choose(label) {
    await this.preset(label).click();
  }

  async custom(from, to) {
    await this.choose('Custom');
    await this.from.fill(from);
    await this.to.fill(to);
  }
}
