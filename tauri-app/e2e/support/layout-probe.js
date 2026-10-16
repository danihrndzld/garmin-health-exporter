/**
 * Reads rendered geometry. Kept apart from the page objects: they say where a
 * component is, this says how it is drawn.
 */
export class LayoutProbe {
  #page;

  constructor(page) {
    this.#page = page;
  }

  async resize(width, height) {
    await this.#page.setViewportSize({ width, height });
  }

  async viewport() {
    return this.#page.viewportSize();
  }

  async box(locator) {
    return locator.boundingBox();
  }

  /** Content taller than the box: the element scrolls or clips. */
  async overflowY(locator) {
    return locator.evaluate((el) => el.scrollHeight - el.clientHeight);
  }

  /** Rendered diameter and stroke of every visible progress ring, outermost first. */
  async rings(heroLocator) {
    return heroLocator.locator('.ring').evaluateAll((gs) => gs
      .filter((g) => getComputedStyle(g).display !== 'none')
      .map((g) => {
        const c = g.querySelector('circle.v');
        const b = c.getBoundingClientRect();
        const scale = b.width / c.getBBox().width; // SVG units → px
        return { phase: g.dataset.phase, diameter: b.width, height: b.height, stroke: parseFloat(getComputedStyle(c).strokeWidth) * scale };
      })
      .sort((a, b) => b.diameter - a.diameter));
  }

  async isDisplayed(locator) {
    return locator.evaluate((el) => getComputedStyle(el).display !== 'none');
  }

  async borderRadius(locator) {
    return locator.evaluate((el) => getComputedStyle(el).borderTopLeftRadius);
  }

  /** True when the element is one text line tall (under 1.5 line heights; "normal" = 1.2em). */
  async isSingleLine(locator) {
    return locator.evaluate((el) => {
      const cs = getComputedStyle(el);
      const line = cs.lineHeight === 'normal' ? parseFloat(cs.fontSize) * 1.2 : parseFloat(cs.lineHeight);
      return el.getBoundingClientRect().height < line * 1.5;
    });
  }
}
