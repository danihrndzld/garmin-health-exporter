/**
 * Garmin Connect API Client
 *
 * Authenticated HTTP client with:
 *   - Auto-injection of Bearer token and mobile User-Agent
 *   - Configurable inter-request delay (R12)
 *   - 429: honors Retry-After, at most 2 retries, then the run stops (rateLimited)
 *     (python-garminconnect never retries a 429; garth removed it in #177)
 *   - 401: refresh the token once and retry; clear tokens only if that fails
 *   - 403: skip that endpoint and keep the tokens; stop after 5 in a row or on the profile call
 *   - 204 or an empty body: success with data null ("no data yet", e.g. an unsynced day)
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
const { redactUrl, redactString } = require('../util/redact');

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

/** Retry-After in ms (seconds or an HTTP date), capped at 60 s; null when absent. */
function retryAfterMs(resp) {
  const raw = resp.headers && typeof resp.headers.get === 'function' ? resp.headers.get('retry-after') : null;
  if (raw == null || raw === '') return null;
  const secs = Number(raw);
  const ms = Number.isFinite(secs) ? secs * 1000 : Date.parse(raw) - Date.now();
  return Number.isFinite(ms) ? Math.min(Math.max(ms, 0), 60000) : null;
}

function trimPreview(text, max = 200) {
  if (text == null) return '';
  // Redact first so secrets don't survive the length trim.
  const s = redactString(String(text)).replace(/\s+/g, ' ').trim();
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
      return `Empty response body${ep} — Garmin returned HTTP ${meta.status} with no content. Usually no data for that day yet (the watch has not synced) or a metric this device does not record.`;
    case 'BAD_JSON':
      return `Malformed JSON${ep} — HTTP ${meta.status} returned a body that is not valid JSON: ${meta.bodyPreview || '(no preview)'}`;
    case 'TIMEOUT':
      return `Timeout after ${meta.elapsedMs}ms${ep}`;
    case 'NETWORK':
      return `Network error${ep}: ${meta.errorClass || 'Error'}: ${meta.errorMessage || ''}`.trim();
    case 'AUTH':
      return `Auth failed (${meta.status}) — tokens cleared, re-login required`;
    case 'FORBIDDEN':
      return `HTTP 403${ep}: Garmin refused this request. The saved login was kept.`;
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
    this.maxRetries = opts.maxRetries ?? 5;               // network errors and timeouts
    this.maxRateLimitRetries = opts.maxRateLimitRetries ?? 2; // HTTP 429
    this.timeoutMs = opts.timeoutMs ?? 30000;
    this._fetch = opts.fetch || globalThis.fetch;
    this._log = opts.log || console.log;
    this._sleep = opts.sleep || sleep;
    this._lastRequestTime = 0;
    this.authFailed = false;
    this.rateLimited = false;
    this.stopReason = null;
    this._forbiddenStreak = 0;
  }

  /** Stop the whole run (exporter checks client.authFailed / rateLimited after each step). */
  _stop(kind, reason) {
    if (kind === 'auth') this.authFailed = true;
    if (kind === 'rate') this.rateLimited = true;
    this.stopReason ||= reason;
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
  async fetchUrl(url, endpointName = null, extraHeaders = {}) {
    const redactedUrl = redactUrl(url);
    const baseMeta = { endpoint: endpointName, url: redactedUrl };

    if (this.authFailed || this.rateLimited) {
      const meta = { ...baseMeta };
      return {
        ok: false,
        errorCode: this.authFailed ? 'AUTH' : 'RATE_LIMIT',
        error: `${this.stopReason || 'Run stopped'} — aborting further requests`,
        meta,
      };
    }

    await this._throttle();

    let token;
    try {
      token = await this.auth.getAccessToken();
    } catch (err) {
      this._stop('auth', `Auth error: ${err.message}`);
      const meta = { ...baseMeta, errorClass: err.name, errorMessage: err.message };
      return {
        ok: false,
        errorCode: 'AUTH',
        error: `Auth error: ${err.message}`,
        meta,
      };
    }

    const buildHeaders = (t) => ({
      ...(typeof this.auth.apiHeaders === 'function'
        ? this.auth.apiHeaders(t)
        : { Authorization: `Bearer ${t}`, 'User-Agent': USER_AGENT_MOBILE, Accept: 'application/json' }),
      ...extraHeaders,
    });
    let headers = buildHeaders(token);
    let refreshed = false;
    let rateLimitAttempt = 0;

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

        // 401: the token is not accepted. Refresh once and retry; only a second 401
        // (or a failed refresh) clears the tokens and stops the run.
        if (resp.status === 401) {
          if (!refreshed && typeof this.auth.refresh === 'function') {
            refreshed = true;
            const r = await this.auth.refresh();
            if (r && r.ok) {
              headers = buildHeaders(await this.auth.getAccessToken());
              this._log(`[client] 401${endpointName ? ` on ${endpointName}` : ''}: token refreshed, retrying`);
              attempt--; // the retry after a refresh does not count against maxRetries
              continue;
            }
          }
          this.auth.clearTokens();
          this._stop('auth', 'Garmin no longer accepts the saved login');
          const meta = {
            ...baseMeta, status: resp.status, contentType,
            attempt: attempt + 1, elapsedMs: Date.now() - startedAt,
          };
          return { ok: false, errorCode: 'AUTH', error: formatErrorMessage('AUTH', meta), meta };
        }

        // 403: Garmin (or Cloudflare) refused this one request. Keep the tokens: deleting
        // them would force a new sign in, which is the step most likely to be blocked.
        if (resp.status === 403) {
          await resp.text().catch(() => '');
          this._forbiddenStreak++;
          const meta = {
            ...baseMeta, status: 403, contentType,
            attempt: attempt + 1, elapsedMs: Date.now() - startedAt,
          };
          if (endpointName === 'social_profile' || this._forbiddenStreak >= 5) {
            this._stop('auth', `Garmin refused ${this._forbiddenStreak} request(s) in a row (HTTP 403). The saved login was kept; try again later or from another network.`);
          }
          return { ok: false, errorCode: 'FORBIDDEN', error: formatErrorMessage('FORBIDDEN', meta), meta };
        }
        this._forbiddenStreak = 0;

        // 429: wait (Retry-After when Garmin sends it) and retry; then stop the run.
        if (resp.status === 429) {
          await resp.text().catch(() => '');
          if (rateLimitAttempt < this.maxRateLimitRetries) {
            const wait = retryAfterMs(resp) ?? backoffDelay(rateLimitAttempt);
            rateLimitAttempt++;
            attempt--; // 429 retries have their own budget
            this._log(`[client] 429 rate-limited${endpointName ? ` on ${endpointName}` : ''} (retry ${rateLimitAttempt}/${this.maxRateLimitRetries}), waiting ${wait}ms`);
            await this._sleep(wait);
            continue;
          }
          this._stop('rate', 'Garmin is rate limiting requests (HTTP 429). The data so far is cached; try again in an hour.');
          const meta = {
            ...baseMeta, status: 429, contentType,
            attempt: rateLimitAttempt + 1, elapsedMs: Date.now() - startedAt,
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

        // Success. 204 or an empty body means Garmin has no data (yet) for this request,
        // e.g. a day the watch has not synced; upstream maps it to {} the same way.
        const text = await resp.text();
        if (!text || text.trim() === '') {
          return { ok: true, data: null, empty: true, status: resp.status };
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

    return this.fetchUrl(url, endpointName, endpoint.headers || {});
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
        this._log(`[skip] ${endpointName}: ${result.error}`, {
          errorCode: result.errorCode,
          meta: result.meta,
        });
        return null;
      }
      if (result.empty) this._log(`[no data] ${endpointName} (HTTP ${result.status})`);
      return result.data;
    } catch (err) {
      this._log(`[skip] ${endpointName}: ${err.message}`, {
        errorCode: 'UNCAUGHT',
        meta: { endpoint: endpointName, errorClass: err.name, errorMessage: err.message },
      });
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
  retryAfterMs,
  redactUrl,
  trimPreview,
  formatErrorMessage,
};
