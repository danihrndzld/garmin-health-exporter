/** Garmin Connect row of the Setup card and its sign in form. */
export class GarminConnection {
  #page;

  constructor(page) {
    this.#page = page;
    this.state = page.locator('#garmin-state');
    this.button = page.locator('#garmin-btn');
    this.include = page.locator('#garmin-on');
    this.form = page.locator('#garmin-form');
    this.email = page.locator('#email');
    this.password = page.locator('#password');
    this.error = page.locator('#garmin-err');
    this.signIn = page.locator('#garmin-signin');
    this.cancel = page.locator('#garmin-cancel');
  }

  /** connect, disconnect and reconnect are the same button; its label says which. */
  async pressButton() {
    await this.button.click();
  }

  /** Opens the form (connect or reconnect) and signs in. */
  async connect({ email, password }) {
    if (!(await this.form.isVisible())) await this.pressButton();
    await this.email.fill(email);
    await this.password.fill(password);
    await this.signIn.click();
  }

  /** Fills the open form and presses Enter in the password field instead of "sign in". */
  async connectWithEnter({ email, password }) {
    await this.email.fill(email);
    await this.password.fill(password);
    await this.password.press('Enter');
  }

  /** Fills the open form and presses Enter twice in a row, as an impatient user would. */
  async connectWithDoubleEnter({ email, password }) {
    await this.email.fill(email);
    await this.password.fill(password);
    await this.password.press('Enter');
    await this.#page.keyboard.press('Enter');
  }
}
