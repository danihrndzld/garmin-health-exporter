/** MyFitnessPal row of the Setup card. */
export class MfpConnection {
  constructor(page) {
    this.state = page.locator('#mfp-state');
    this.button = page.locator('#mfp-btn');
    this.include = page.locator('#mfp-on');
  }

  /** connect, disconnect and reconnect are the same button; its label says which. */
  async pressButton() {
    await this.button.click();
  }
}
