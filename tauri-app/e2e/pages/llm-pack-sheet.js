/** LLM pack dialog: language switch, context preview and the two build buttons. */
export class LlmPackSheet {
  #page;

  constructor(page) {
    this.#page = page;
    this.dialog = page.getByRole('dialog');
    this.title = page.locator('#sh-h1');
    this.folderButton = page.locator('#copy-ctx');
    this.folderLabel = page.locator('#copy-ctx-l');
    this.folderTarget = page.locator('#copy-ctx-s');
    this.oneFileButton = page.locator('#copy-one');
    this.oneFileLabel = page.locator('#copy-one-l');
    /** Shown instead of the pack when the export could not be read. */
    this.emptyNote = page.locator('#sh-empty');
    this.context = page.locator('#ctx-pre');
  }

  /** The keyboard shortcut the app documents on the button (<kbd>L</kbd>). */
  async openWithShortcut() {
    await this.#page.keyboard.press('l');
  }

  async language(code) {
    await this.dialog.getByRole('radio', { name: code.toUpperCase(), exact: true }).click();
  }

  async buildFolder() {
    await this.folderButton.click();
  }

  async buildOneFile() {
    await this.oneFileButton.click();
  }

  async closeWithEscape() {
    await this.#page.keyboard.press('Escape');
  }
}
