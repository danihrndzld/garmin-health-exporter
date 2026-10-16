/** Setup card: Garmin credentials. */
export class SetupForm {
  constructor(page) {
    this.email = page.locator('#email');
    this.password = page.locator('#password');
  }

  async fillCredentials({ email, password }) {
    await this.email.fill(email);
    await this.password.fill(password);
  }
}
