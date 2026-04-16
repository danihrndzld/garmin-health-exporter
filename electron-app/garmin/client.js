/**
 * Garmin Connect API Client
 *
 * Authenticated HTTP client with:
 *   - Auto-injection of Bearer token and mobile User-Agent
 *   - Configurable inter-request delay (R12)
 *   - Exponential backoff + jitter on 429 (R11)
 *   - Immediate fail + token clear on 401/403
 *   - safe() wrapper matching Python's pattern
 *
 * Usage:
 *   const auth = require('./auth').createClient(dataDir);
 *   const { createGarminClient } = require('./client');
 *   const client = createGarminClient(auth);
 *   const result = await client.fetch('stats', { date: '2026-04-15', displayName: 'abc' });
 *   const safe   = await client.safe('stats', { date: '2026-04-15', displayName: 'abc' });
 */

const { USER_AGENT_MOBILE } = require('./auth');
const { getEndpoint } = require('./endpoints');

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

function sleep(ms) {
  return new Promise((resolve) => setTimeout(resolve, ms));
}

/**
 * Calculate backoff delay for 429 retries.
 * Formula: min(5000 * 2^attempt + random(0,1000), 60000)
 * @param {number} attempt  Zero-based attempt index
 * @returns {number} Delay in milliseconds
 */
function backoffDelay(attempt) {
  const base = 5000 * Math.pow(2, attempt);
  const jitter = Math.floor(Math.random() * 1000);
  return Math.min(base + jitter, 60000);
}

// ---------------------------------------------------------------------------
// GarminClient
// ---------------------------------------------------------------------------

class GarminClient {
  /**
   * @param {object} auth           Auth instance from auth.js createClient()
   * @param {object} [opts]
   * @param {number} [opts.delay]       Inter-request delay in ms (default 500)
   * @param {number} [opts.maxRetries]  Max 429 retries (default 5)
   * @param {function} [opts.fetch]     Custom fetch function (for testing)
   * @param {function} [opts.log]       Logger function (default console.log)
   * @param {function} [opts.sleep]     Custom sleep function (for testing)
   */
  constructor(auth, opts = {}) {
    this.auth = auth;
    this.delay = opts.delay ?? 500;
    this.maxRetries = opts.maxRetries ?? 5;
    this._fetch = opts.fetch || globalThis.fetch;
    this._log = opts.log || console.log;
    this._sleep = opts.sleep || sleep;
    this._lastRequestTime = 0;
  }

  /**
   * Enforce inter-request delay.
   * Waits until at least `this.delay` ms have passed since the last request.
   */
  async _throttle() {
    const now = Date.now();
    const elapsed = now - this._lastRequestTime;
    if (this._lastRequestTime > 0 && elapsed < this.delay) {
      await this._sleep(this.delay - elapsed);
    }
    this._lastRequestTime = Date.now();
  }

  /**
   * Make an authenticated GET request to a raw URL.
   * Handles 429 backoff and 401/403 token clearing.
   *
   * @param {string} url  Full URL to fetch
   * @returns {Promise<{ok: true, data: any} | {ok: false, error: string}>}
   */
  async fetchUrl(url) {
    await this._throttle();

    let token;
    try {
      token = await this.auth.getAccessToken();
    } catch (err) {
      return { ok: false, error: `Auth error: ${err.message}` };
    }

    const headers = {
      Authorization: `Bearer ${token}`,
      'User-Agent': USER_AGENT_MOBILE,
      Accept: 'application/json',
    };

    for (let attempt = 0; attempt <= this.maxRetries; attempt++) {
      try {
        const resp = await this._fetch(url, { headers });

        // 401/403: clear tokens, fail immediately
        if (resp.status === 401 || resp.status === 403) {
          this.auth.clearTokens();
          return {
            ok: false,
            error: `Auth failed (${resp.status}) — tokens cleared, re-login required`,
          };
        }

        // 429: backoff + retry
        if (resp.status === 429) {
          if (attempt < this.maxRetries) {
            const wait = backoffDelay(attempt);
            this._log(`[client] 429 rate-limited on attempt ${attempt + 1}/${this.maxRetries + 1}, retrying in ${wait}ms`);
            await this._sleep(wait);
            this._lastRequestTime = Date.now();
            continue;
          }
          return {
            ok: false,
            error: `Rate limited (429) after ${this.maxRetries + 1} attempts — try again later`,
          };
        }

        // Other non-OK status
        if (!resp.ok) {
          const body = await resp.text().catch(() => '');
          return {
            ok: false,
            error: `HTTP ${resp.status}: ${body.slice(0, 200)}`,
          };
        }

        // Success — parse JSON
        const data = await resp.json();
        return { ok: true, data };
      } catch (err) {
        // Network error on last attempt
        if (attempt >= this.maxRetries) {
          return { ok: false, error: `Network error: ${err.message}` };
        }
        // Network error — retry with backoff (treat like transient failure)
        const wait = backoffDelay(attempt);
        this._log(`[client] Network error on attempt ${attempt + 1}, retrying in ${wait}ms: ${err.message}`);
        await this._sleep(wait);
        this._lastRequestTime = Date.now();
      }
    }

    // Should not reach here, but just in case
    return { ok: false, error: 'Unexpected error — exhausted all retries' };
  }

  /**
   * Fetch a named endpoint with parameters.
   *
   * @param {string} endpointName  Name from endpoints.js (e.g. 'stats', 'sleep')
   * @param {object} params        Parameters for the endpoint's buildUrl
   * @returns {Promise<{ok: true, data: any} | {ok: false, error: string}>}
   */
  async fetch(endpointName, params = {}) {
    const endpoint = getEndpoint(endpointName);
    if (!endpoint) {
      return { ok: false, error: `Unknown endpoint: ${endpointName}` };
    }

    let url;
    try {
      url = endpoint.buildUrl(params);
    } catch (err) {
      return { ok: false, error: `Failed to build URL for ${endpointName}: ${err.message}` };
    }

    return this.fetchUrl(url);
  }

  /**
   * Safe wrapper — catches errors and returns null on failure.
   * Matches Python's safe(fn, *args, label="") pattern.
   *
   * @param {string} endpointName  Name from endpoints.js
   * @param {object} params        Parameters for the endpoint's buildUrl
   * @returns {Promise<any|null>}  Parsed JSON data on success, null on failure
   */
  async safe(endpointName, params = {}) {
    try {
      const result = await this.fetch(endpointName, params);
      if (!result.ok) {
        this._log(`[skip] ${endpointName}: ${result.error}`);
        return null;
      }
      return result.data;
    } catch (err) {
      this._log(`[skip] ${endpointName}: ${err.message}`);
      return null;
    }
  }

  /**
   * Fetch the user's display name from the social profile endpoint.
   * Needed by many endpoints that include displayName in the URL.
   *
   * @returns {Promise<{ok: true, displayName: string} | {ok: false, error: string}>}
   */
  async getDisplayName() {
    const result = await this.fetch('social_profile');
    if (!result.ok) return result;
    const displayName = result.data?.displayName;
    if (!displayName) {
      return { ok: false, error: 'displayName not found in social profile response' };
    }
    return { ok: true, displayName };
  }
}

// ---------------------------------------------------------------------------
// Factory
// ---------------------------------------------------------------------------

/**
 * Create a GarminClient instance.
 *
 * @param {object} auth   Auth instance from auth.js createClient()
 * @param {object} [opts] Options: { delay, maxRetries, fetch, log }
 * @returns {GarminClient}
 */
function createGarminClient(auth, opts) {
  return new GarminClient(auth, opts);
}

module.exports = {
  createGarminClient,
  GarminClient,
  // Expose for testing
  backoffDelay,
};
