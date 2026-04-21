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

/**
 * Strip secrets from URLs before logging or bundling.
 * Redacts any query parameter whose name looks like a token or secret.
 */
function redactUrl(url) {
  if (!url || typeof url !== 'string') return url;
  return url.replace(/([?&])([^=&]+)=([^&]*)/g, (match, sep, key, value) => {
    const k = key.toLowerCase();
    if (k === 'token' || k === 'access_token' || k.includes('auth') || k.includes('secret') || k.includes('password')) {
      return `${sep}${key}=REDACTED`;
    }
    return match;
  });
}

function trimPreview(text, max = 200) {
  if (text == null) return '';
  const s = String(text).replace(/\s+/g, ' ').trim();
  return s.length > max ? s.slice(0, max) + '…' : s;
}

/**
 * Build a human-readable error message from an errorCode + meta.
 * Preserves legacy prefixes ("Auth failed", "Rate limited (429) after N attempts",
 * "HTTP <status>", "Timeout", "Network error") for backwards compatibility with
 * callers and tests that match on those substrings.
 */
function formatErrorMessage(errorCode, meta) {
  const ep = meta.endpoint ? ` [${meta.endpoint}]` : '';
  switch (errorCode) {
    case 'EMPTY_BODY':
      return `Empty response body${ep} — Garmin returned HTTP ${meta.status} with no content. Often a transient backend hiccup or a deprecated endpoint.`;
    case 'BAD_JSON':
      return `Malformed JSON${ep} — HTTP ${meta.status} returned a body that is not valid JSON: ${meta.bodyPreview || '(no preview)'}`;
    case 'TIMEOUT':
      return `Timeout after ${meta.elapsedMs}ms${ep}`;
    case 'NETWORK':
      return `Network error${ep}: ${meta.errorClass || 'Error'}: ${meta.errorMessage || ''}`.trim();
    case 'AUTH':
      return `Auth failed (${meta.status}) — tokens cleared, re-login required`;
    case 'RATE_LIMIT':
      return `Rate limited (429) after ${meta.attempt} attempts — try again later`;
    case 'HTTP_4XX':
    case 'HTTP_5XX':
      return `HTTP ${meta.status}${ep}: ${meta.bodyPreview || ''}`.trim();
    default:
      return `${errorCode}${ep}`;
  }
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
    this.timeoutMs = opts.timeoutMs ?? 30000;
    this._fetch = opts.fetch || globalThis.fetch;
    this._log = opts.log || console.log;
    this._sleep = opts.sleep || sleep;
    this._lastRequestTime = 0;
    this.authFailed = false;
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
   * @param {string} url                 Full URL to fetch
   * @param {string} [endpointName]      Optional endpoint label for diagnostics
   * @returns {Promise<{ok: true, data: any} | {ok: false, error: string, errorCode: string, meta: object}>}
   */
  async fetchUrl(url, endpointName = null) {
    const redactedUrl = redactUrl(url);
    const baseMeta = { endpoint: endpointName, url: redactedUrl };

    if (this.authFailed) {
      const meta = { ...baseMeta };
      return {
        ok: false,
        errorCode: 'AUTH',
        error: 'Auth previously failed — aborting further requests',
        meta,
      };
    }

    await this._throttle();

    let token;
    try {
      token = await this.auth.getAccessToken();
    } catch (err) {
      this.authFailed = true;
      const meta = { ...baseMeta, errorClass: err.name, errorMessage: err.message };
      return {
        ok: false,
        errorCode: 'AUTH',
        error: `Auth error: ${err.message}`,
        meta,
      };
    }

    const headers = {
      Authorization: `Bearer ${token}`,
      'User-Agent': USER_AGENT_MOBILE,
      Accept: 'application/json',
    };

    for (let attempt = 0; attempt <= this.maxRetries; attempt++) {
      const controller = new AbortController();
      const timer = setTimeout(() => controller.abort(), this.timeoutMs);
      const startedAt = Date.now();
      try {
        const resp = await this._fetch(url, { headers, signal: controller.signal });
        clearTimeout(timer);
        const contentType = resp.headers && typeof resp.headers.get === 'function'
          ? resp.headers.get('content-type')
          : null;

        // 401/403: clear tokens, fail immediately, block subsequent requests
        if (resp.status === 401 || resp.status === 403) {
          this.auth.clearTokens();
          this.authFailed = true;
          const meta = {
            ...baseMeta, status: resp.status, contentType,
            attempt: attempt + 1, elapsedMs: Date.now() - startedAt,
          };
          return {
            ok: false,
            errorCode: 'AUTH',
            error: formatErrorMessage('AUTH', meta),
            meta,
          };
        }

        // 429: backoff + retry
        if (resp.status === 429) {
          if (attempt < this.maxRetries) {
            const wait = backoffDelay(attempt);
            this._log(`[client] 429 rate-limited${endpointName ? ` on ${endpointName}` : ''} on attempt ${attempt + 1}/${this.maxRetries + 1}, retrying in ${wait}ms`);
            await this._sleep(wait);
            continue;
          }
          const meta = {
            ...baseMeta, status: 429, contentType,
            attempt: this.maxRetries + 1, elapsedMs: Date.now() - startedAt,
          };
          return {
            ok: false,
            errorCode: 'RATE_LIMIT',
            error: formatErrorMessage('RATE_LIMIT', meta),
            meta,
          };
        }

        // Other non-OK status
        if (!resp.ok) {
          const body = await resp.text().catch(() => '');
          const errorCode = resp.status >= 500 ? 'HTTP_5XX' : 'HTTP_4XX';
          const meta = {
            ...baseMeta, status: resp.status, contentType,
            bodyPreview: trimPreview(body), attempt: attempt + 1,
            elapsedMs: Date.now() - startedAt,
          };
          return {
            ok: false,
            errorCode,
            error: `HTTP ${resp.status}: ${body.slice(0, 200)}`,
            meta,
          };
        }

        // Success — text-then-parse so we can distinguish EMPTY_BODY from BAD_JSON
        const text = await resp.text();
        if (!text || text.trim() === '') {
          const meta = {
            ...baseMeta, status: resp.status, contentType,
            bodyPreview: '', attempt: attempt + 1,
            elapsedMs: Date.now() - startedAt,
          };
          return {
            ok: false,
            errorCode: 'EMPTY_BODY',
            error: formatErrorMessage('EMPTY_BODY', meta),
            meta,
          };
        }
        let data;
        try {
          data = JSON.parse(text);
        } catch (parseErr) {
          const meta = {
            ...baseMeta, status: resp.status, contentType,
            bodyPreview: trimPreview(text), attempt: attempt + 1,
            elapsedMs: Date.now() - startedAt,
            errorClass: parseErr.name, errorMessage: parseErr.message,
          };
          return {
            ok: false,
            errorCode: 'BAD_JSON',
            error: formatErrorMessage('BAD_JSON', meta),
            meta,
          };
        }
        return { ok: true, data };
      } catch (err) {
        clearTimeout(timer);
        const isTimeout = err.name === 'AbortError';
        const errorCode = isTimeout ? 'TIMEOUT' : 'NETWORK';
        const elapsedMs = Date.now() - startedAt;
        const meta = {
          ...baseMeta, attempt: attempt + 1, elapsedMs,
          errorClass: err.name, errorMessage: err.message,
        };
        // Fail on last attempt
        if (attempt >= this.maxRetries) {
          // Preserve legacy message prefix shape for existing consumers.
          const legacy = isTimeout
            ? `Timeout after ${this.timeoutMs}ms`
            : `Network error: ${err.message}`;
          return {
            ok: false,
            errorCode,
            error: legacy,
            meta,
          };
        }
        const wait = backoffDelay(attempt);
        const label = isTimeout ? `Timeout after ${this.timeoutMs}ms` : `Network error: ${err.message}`;
        this._log(`[client] ${label}${endpointName ? ` on ${endpointName}` : ''} on attempt ${attempt + 1}, retrying in ${wait}ms`);
        await this._sleep(wait);
      }
    }

    // Should not reach here, but just in case
    return {
      ok: false,
      errorCode: 'NETWORK',
      error: 'Unexpected error — exhausted all retries',
      meta: { ...baseMeta },
    };
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
      return {
        ok: false,
        errorCode: 'UNKNOWN_ENDPOINT',
        error: `Unknown endpoint: ${endpointName}`,
        meta: { endpoint: endpointName },
      };
    }

    let url;
    try {
      url = endpoint.buildUrl(params);
    } catch (err) {
      return {
        ok: false,
        errorCode: 'BUILD_URL',
        error: `Failed to build URL for ${endpointName}: ${err.message}`,
        meta: { endpoint: endpointName, errorClass: err.name, errorMessage: err.message },
      };
    }

    return this.fetchUrl(url, endpointName);
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
  redactUrl,
  trimPreview,
  formatErrorMessage,
};
