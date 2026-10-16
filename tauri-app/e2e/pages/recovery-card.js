/** Recovery preview card: sleep score donut and the three stats. */
export class RecoveryCard {
  constructor(page) {
    this.root = page.locator('.preview.sleep');
    this.donut = page.locator('#sleep-donut');
    this.score = page.locator('#sleep-score');
    this.stats = this.root.locator('.stats li');
  }
}
