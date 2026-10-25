/** Garmin Connect row of the Setup card and its connect form (API key first, email as the alternative). */
export class GarminConnection {
  #page;

  constructor(page) {
    this.#page = page;
    this.state = page.locator('#garmin-state');
    this.button = page.locator('#garmin-btn');
    this.include = page.locator('#garmin-on');
    /** "add API key" or "add email login", shown while only one credential is live. */
    this.addOther = page.locator('#garmin-add');
    this.form = page.locator('#garmin-form');
    this.apiKey = page.locator('#apikey');
    this.email = page.locator('#email');
    this.password = page.locator('#password');
    this.error = page.locator('#garmin-err');
    this.signIn = page.locator('#garmin-signin');
    this.modeSwitch = page.locator('#garmin-mode');
    this.cancel = page.locator('#garmin-cancel');
  }

  /** connect, disconnect and reconnect are the same button; its label says which. */
  async pressButton() {
    await this.button.click();
  }

  /** connect or reconnect opens the form on the API key; this switches it to email. */
  async openEmailForm() {
    await this.pressButton();
    await this.modeSwitch.click();
  }

  /** Opens the form, switches to email and signs in. */
  async connect(login) {
    await this.openEmailForm();
    await this.signInWith(login);
  }

  /** Signs in on a form that already shows the email fields. */
  async signInWith({ email, password }) {
    await this.email.fill(email);
    await this.password.fill(password);
    await this.signIn.click();
  }

  /** Opens the form (it starts on the API key) and saves the key. */
  async connectWithKey(key) {
    await this.pressButton();
    await this.apiKey.fill(key);
    await this.signIn.click();
  }

  /** With the API key live, adds the email login through the "add email login" link. */
  async addEmailLogin(login) {
    await this.addOther.click();
    await this.signInWith(login);
  }

  /** Fills the open email form and presses Enter in the password field instead of "sign in". */
  async connectWithEnter({ email, password }) {
    await this.email.fill(email);
    await this.password.fill(password);
    await this.password.press('Enter');
  }

  /** Fills the open email form and presses Enter twice in a row, as an impatient user would. */
  async connectWithDoubleEnter({ email, password }) {
    await this.email.fill(email);
    await this.password.fill(password);
    await this.password.press('Enter');
    await this.#page.keyboard.press('Enter');
  }
}
