/** Dark hero card: state badge, headline, progress rings, legend and the two CTAs. */
export class HeroPanel {
  #page;

  constructor(page) {
    this.#page = page;
    this.root = page.locator('#hero');
    this.badge = page.locator('#badge');
    this.headline = page.locator('#h1');
    this.subline = page.locator('#h2');
    this.cta = page.locator('#cta');
    this.ctaLabel = page.locator('#cta-label');
    this.llmButton = page.locator('#llm-btn');
    this.centerNumber = page.locator('#ring-n');
    this.centerLabel = page.locator('#ring-l');
  }

  /** Progress arc of one phase: daily, aggregated, activities, csv or mfp. */
  ring(phase) {
    return this.root.locator(`.ring[data-phase="${phase}"]`);
  }

  legend(phase) {
    return this.#page.locator(`#lg-${phase}`);
  }

  async state() {
    return this.root.getAttribute('data-state');
  }

  async pressCta() {
    await this.cta.click();
  }
}
