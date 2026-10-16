/**
 * Scenario for ui/dev-mock.js, the browser stand-in for the Tauri bridge.
 * The only module that knows the mock's localStorage switches: page objects
 * and specs describe intent ("MFP connected"), this class maps it to keys.
 */
export class MockBackend {
  #keys = {};

  mfpConnected() { return this.#set('mock_mfp', String(Date.UTC(2026, 9, 1))); }
  mfpSessionExpired() { return this.#set('mock_mfp_expired', '1'); }
  activities(count) { return this.#set('mock_acts', String(count)); }
  packFails() { return this.#set('mock_pack_fail', '1'); }
  summaryHangs() { return this.#set('mock_summary_hangs', '1'); }

  /** Seeds localStorage once per tab, before the app script runs; reloads keep what the app changed. */
  async install(page) {
    await page.addInitScript((keys) => {
      if (sessionStorage.getItem('e2e_seeded')) return;
      localStorage.clear();
      for (const [k, v] of Object.entries(keys)) localStorage.setItem(k, v);
      sessionStorage.setItem('e2e_seeded', '1');
    }, this.#keys);
  }

  #set(key, value) {
    this.#keys[key] = value;
    return this;
  }
}

/** Login the mock rejects (dev-mock.js: "fail@x.com"). */
export const REJECTED_EMAIL = 'fail@x.com';
export const VALID_LOGIN = { email: 'runner@example.com', password: 'correct horse' };
