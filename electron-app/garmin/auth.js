/**
 * Garmin Connect Auth Module
 *
 * Login, token storage (mode 0600) and token refresh. Two token kinds share tokens.json:
 *
 *   di      Garmin's current login, as in python-garminconnect 0.3.x: the SSO mobile JSON
 *           login gives a CAS ticket, and diauth.garmin.com trades it for a JWT plus a
 *           refresh token. API calls must carry the native Android app headers.
 *   legacy  The older garth flow (sso/embed widget + OAuth1 consumer + exchange/user/2.0).
 *           Kept as a fallback, and existing legacy token files keep working.
 *
 * login() tries the mobile login -> DI token. When that is blocked (HTTP 403 or 429,
 * captcha, a page that is not JSON) or the DI exchange fails, it tries the legacy flow
 * once. A wrong password or MFA stops at once. A 429 from the legacy flow or from diauth
 * stops and starts a 15 minute cooldown, so the app never retries a sign in on its own.
 *
 * Usage:
 *   const auth = require('./auth');
 *   const client = auth.createClient('/path/to/data/dir');
 *   await client.login(email, password);
 *   const token = await client.getAccessToken();
 */

const crypto = require('crypto');
const fs = require('fs');
const path = require('path');
const querystring = require('querystring');

// ---------------------------------------------------------------------------
// Constants
// ---------------------------------------------------------------------------
// GARMIN_FAKE_SERVER points every Garmin host at a local test server. Only loopback
// URLs are accepted, so the variable can never send credentials anywhere else.
// GARMIN_FAKE_SERVER points every Garmin host at a local test server. Only loopback
// URLs are accepted, so the variable can never send credentials anywhere else. It is
// read on every call, so a test that sets it late cannot fall back to the real hosts.
function fakeServer() {
  const v = process.env.GARMIN_FAKE_SERVER || '';
  return /^http:\/\/(127\.0\.0\.1|localhost):\d+$/.test(v) ? v : null;
}
function urls() {
  const host = (real) => fakeServer() || real;
  const sso = host('https://sso.garmin.com');
  const api = host('https://connectapi.garmin.com');
  return {
    OAUTH_CONSUMER_URL: `${host('https://thegarth.s3.amazonaws.com')}/oauth_consumer.json`,
    SSO_ORIGIN: sso,
    SSO_EMBED: `${sso}/sso/embed`,
    SSO_SIGNIN: `${sso}/sso/signin`,
    API_BASE: api,
    OAUTH_BASE: `${api}/oauth-service/oauth`,
    DI_TOKEN_URL: `${host('https://diauth.garmin.com')}/di-oauth2-service/oauth/token`,
    MOBILE_LOGIN_URL: `${sso}/mobile/api/login`,
  };
}
const apiBase = () => urls().API_BASE;
const GC_MODERN = 'https://connect.garmin.com/modern';
// The ticket is only valid for the service it was issued for, so the exchange repeats it.
const MOBILE_SERVICE = 'https://mobile.integration.garmin.com/gcm/ios';
const DI_GRANT_TICKET = 'https://connectapi.garmin.com/di-oauth2-service/oauth/grant/service_ticket';
// Tried in order; Garmin retires old ids over time (python-garminconnect client.py:174-179).
const DI_CLIENT_IDS = [
  'GARMIN_CONNECT_MOBILE_ANDROID_DI_2025Q2',
  'GARMIN_CONNECT_MOBILE_ANDROID_DI_2024Q4',
  'GARMIN_CONNECT_MOBILE_ANDROID_DI',
  'GARMIN_CONNECT_MOBILE_IOS_DI',
];
const USER_AGENT_IOS =
  'Mozilla/5.0 (iPhone; CPU iPhone OS 18_7 like Mac OS X) AppleWebKit/605.1.15 (KHTML, like Gecko) Mobile/15E148';
// connectapi answers 403 to a DI token without these (measured 2026-10-14).
const NATIVE_HEADERS = {
  'User-Agent': 'GCM-Android-5.23',
  'X-Garmin-User-Agent': 'com.garmin.android.apps.connectmobile/5.23; ; Google/sdk_gphone64_arm64/google; Android/33; Dalvik/2.1.0',
  'X-Garmin-Paired-App-Version': '10861',
  'X-Garmin-Client-Platform': 'Android',
  'X-App-Ver': '10861',
  'X-Lang': 'en',
  'X-GCExperience': 'GC5',
  'Accept-Language': 'en-US,en;q=0.9',
};
// Refresh this many seconds before expiry, like upstream (client.py:1432-1452).
const REFRESH_MARGIN_S = 900;
// After a sign in gets HTTP 429, refuse new sign ins for this long (no automatic retries).
const SSO_COOLDOWN_S = 15 * 60;

const USER_AGENT_BROWSER =
  'Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/117.0.0.0 Safari/537.36';
const USER_AGENT_MOBILE = 'com.garmin.android.apps.connectmobile';

const CSRF_RE = /name="_csrf"\s+value="(.+?)"/;
const TICKET_RE = /ticket=([^"]+)"/;

const TOKEN_FILE = 'tokens.json';

// ---------------------------------------------------------------------------
// Cookie Jar
// ---------------------------------------------------------------------------
class CookieJar {
  constructor() {
    this.cookies = new Map();
  }

  /** Parse Set-Cookie headers from a fetch Response and store them. */
  capture(response) {
    const raw = response.headers.getSetCookie?.() ?? [];
    for (const h of raw) {
      const name = h.split('=')[0].trim();
      const value = h.split(';')[0]; // name=value
      this.cookies.set(name, value);
    }
  }

  /** Return a Cookie header string for outgoing requests. */
  header() {
    return [...this.cookies.values()].join('; ');
  }
}

// ---------------------------------------------------------------------------
// OAuth 1.0a Signing (HMAC-SHA1)
// ---------------------------------------------------------------------------
function percentEncode(str) {
  return encodeURIComponent(str)
    .replace(/!/g, '%21')
    .replace(/\*/g, '%2A')
    .replace(/'/g, '%27')
    .replace(/\(/g, '%28')
    .replace(/\)/g, '%29');
}

function buildBaseString(method, url, params) {
  const sorted = Object.keys(params)
    .sort()
    .map((k) => `${percentEncode(k)}=${percentEncode(params[k])}`)
    .join('&');
  return `${method.toUpperCase()}&${percentEncode(url)}&${percentEncode(sorted)}`;
}

function hmacSha1(baseString, signingKey) {
  return crypto.createHmac('sha1', signingKey).update(baseString).digest('base64');
}

function signOAuth1(method, url, oauthParams, consumerSecret, tokenSecret = '') {
  const baseString = buildBaseString(method, url, oauthParams);
  const signingKey = `${percentEncode(consumerSecret)}&${percentEncode(tokenSecret)}`;
  return hmacSha1(baseString, signingKey);
}

function oauthHeader(oauthParams) {
  const parts = Object.keys(oauthParams)
    .sort()
    .map((k) => `${percentEncode(k)}="${percentEncode(oauthParams[k])}"`);
  return `OAuth ${parts.join(', ')}`;
}

function generateNonce() {
  return crypto.randomBytes(16).toString('hex');
}

function nowSeconds() {
  return Math.floor(Date.now() / 1000);
}

/** Read a JWT's payload without verifying it (we only need exp and client_id). */
function jwtClaims(token) {
  try {
    return JSON.parse(Buffer.from(String(token).split('.')[1], 'base64url').toString('utf8'));
  } catch {
    return {};
  }
}

/** A login step result that must stop the whole chain (bad password, MFA, 429). */
const fatal = (error, errorCode) => ({ ok: false, fatal: true, error, errorCode });
const RATE_LIMITED = 'Garmin is rate limiting sign ins (HTTP 429). Wait 15 minutes and try again.';

// ---------------------------------------------------------------------------
// GarminAuth class
// ---------------------------------------------------------------------------
class GarminAuth {
  /**
   * @param {string} dataDir  Directory where tokens.json will be stored.
   * @param {object} [opts]
   * @param {function} [opts.fetch]  Optional fetch override (for testing).
   */
  constructor(dataDir, opts = {}) {
    this.dataDir = dataDir;
    this.tokenPath = path.join(dataDir, TOKEN_FILE);
    this._fetch = opts.fetch || globalThis.fetch;
    this._log = opts.log || (() => {});
    this._cachedTokens = null;
  }

  // -----------------------------------------------------------------------
  // Public API
  // -----------------------------------------------------------------------

  /**
   * Log in and persist tokens. See the chain in the header comment.
   * @returns {{ ok: true, method: string } | { ok: false, error: string }}
   */
  async login(email, password) {
    try {
      const cooldown = this._cooldownLeft();
      if (cooldown > 0) {
        return { ok: false, errorCode: 'RATE_LIMITED', error: `Garmin rate limited the last sign in. Wait ${Math.ceil(cooldown / 60)} more minutes before trying again.` };
      }
      const result = await this._loginChain(email, password);
      if (result.errorCode === 'RATE_LIMITED') this._startCooldown();
      return result;
    } catch (err) {
      return { ok: false, error: err.message };
    }
  }

  async _loginChain(email, password) {
    const mobile = await this._loginMobile(email, password);
    // A 429 on the mobile login does not stop the chain: on 2026-10-14 the mobile endpoint
    // answered 429 two minutes after the widget login had worked, so Garmin limits them
    // separately (python-garminconnect #453). The widget flow is tried once; a second 429 stops.
    if (mobile.fatal && mobile.errorCode !== 'RATE_LIMITED') return { ok: false, errorCode: mobile.errorCode, error: mobile.error };
    if (mobile.ok) {
      const di = await this._exchangeTicketForDi(mobile.ticket, MOBILE_SERVICE);
      if (di.fatal) return { ok: false, errorCode: di.errorCode, error: di.error };
      if (di.ok) {
        this._writeTokens({ di: di.di });
        return { ok: true, method: 'di' };
      }
      this._log(`[auth] DI token exchange failed (${di.error}); trying the older sign in`);
    } else {
      this._log(`[auth] new sign in blocked (${mobile.error}); trying the older sign in`);
    }
    // Only a blocked new login reaches here: never after a wrong password, MFA or 429.
    const widget = await this._loginWidget(email, password);
    if (!widget.ok) return { ok: false, errorCode: widget.errorCode, error: widget.error };
    const legacy = await this._legacyFromTicket(widget.ticket);
    if (!legacy.ok) return legacy;
    return { ok: true, method: 'legacy' };
  }

  // One process per export, so the cooldown after a 429 lives in a file.
  _cooldownPath() { return path.join(this.dataDir, 'sso-cooldown.json'); }
  _cooldownLeft() {
    try {
      const { until } = JSON.parse(fs.readFileSync(this._cooldownPath(), 'utf8'));
      return Math.min(SSO_COOLDOWN_S, Math.max(0, until - nowSeconds())); // a bad clock or edit cannot block longer
    } catch {
      return 0;
    }
  }
  _startCooldown() {
    try {
      fs.mkdirSync(this.dataDir, { recursive: true });
      fs.writeFileSync(this._cooldownPath(), JSON.stringify({ until: nowSeconds() + SSO_COOLDOWN_S }));
    } catch { /* best effort */ }
  }

  /** New login: one JSON POST, no cookies or CSRF. Returns a CAS ticket. */
  async _loginMobile(email, password) {
    const qs = querystring.stringify({ clientId: 'GCM_IOS_DARK', locale: 'en-US', service: MOBILE_SERVICE });
    let resp;
    try {
      resp = await this._fetch(`${urls().MOBILE_LOGIN_URL}?${qs}`, {
        method: 'POST',
        headers: {
          'User-Agent': USER_AGENT_IOS,
          Accept: 'application/json, text/plain, */*',
          'Content-Type': 'application/json',
          Origin: urls().SSO_ORIGIN,
        },
        body: JSON.stringify({ username: email, password, rememberMe: true, captchaToken: '' }),
      });
    } catch (err) {
      return { ok: false, error: `network: ${err.message}` };
    }
    if (resp.status === 429) return fatal(RATE_LIMITED, 'RATE_LIMITED');
    const text = await resp.text();
    let body = null;
    try { body = JSON.parse(text); } catch { /* Cloudflare page or HTML */ }
    if (!body) return { ok: false, error: `HTTP ${resp.status}, not JSON` };
    const type = body.responseStatus && body.responseStatus.type;
    if (type === 'SUCCESSFUL' && body.serviceTicketId) return { ok: true, ticket: body.serviceTicketId };
    if (type === 'INVALID_USERNAME_PASSWORD') return fatal('Wrong email or password.', 'BAD_PASSWORD');
    if (type === 'MFA_REQUIRED') return fatal('This account uses two-step verification, which Garmin Export does not support yet.', 'MFA_REQUIRED');
    if (body.error && String(body.error['status-code']) === '429') return fatal(RATE_LIMITED, 'RATE_LIMITED');
    return { ok: false, error: type || `HTTP ${resp.status}` };
  }

  /** Trade a CAS ticket for a DI token, trying each known client id. */
  async _exchangeTicketForDi(ticket, serviceUrl) {
    let lastError = 'no client id accepted the ticket';
    for (const clientId of DI_CLIENT_IDS) {
      const res = await this._diTokenRequest(clientId, {
        client_id: clientId,
        service_ticket: ticket,
        grant_type: DI_GRANT_TICKET,
        service_url: serviceUrl,
      });
      if (res.status === 429) return fatal(RATE_LIMITED, 'RATE_LIMITED');
      if (res.ok) return { ok: true, di: res.di };
      lastError = res.error;
    }
    return { ok: false, error: lastError };
  }

  async _diTokenRequest(clientId, form) {
    let resp;
    try {
      resp = await this._fetch(urls().DI_TOKEN_URL, {
        method: 'POST',
        headers: {
          ...NATIVE_HEADERS,
          Authorization: `Basic ${Buffer.from(`${clientId}:`).toString('base64')}`,
          Accept: 'application/json',
          'Content-Type': 'application/x-www-form-urlencoded',
          'Cache-Control': 'no-cache',
        },
        body: querystring.stringify(form),
      });
    } catch (err) {
      return { ok: false, status: 0, error: `network: ${err.message}` };
    }
    const text = await resp.text();
    if (!resp.ok) return { ok: false, status: resp.status, error: `diauth HTTP ${resp.status}` };
    let body;
    try { body = JSON.parse(text); } catch { return { ok: false, status: resp.status, error: 'diauth answer is not JSON' }; }
    if (!body.access_token) return { ok: false, status: resp.status, error: 'diauth answer has no access_token' };
    const claims = jwtClaims(body.access_token);
    const now = nowSeconds();
    return {
      ok: true,
      status: resp.status,
      di: {
        access_token: body.access_token,
        refresh_token: body.refresh_token || form.refresh_token || null,
        client_id: claims.client_id || clientId,
        expires_at: claims.exp || now + (body.expires_in || 3600),
        refresh_token_expires_at: body.refresh_token_expires_in ? now + body.refresh_token_expires_in : null,
      },
    };
  }

  /** Older widget login (sso/embed + signin form). Returns a CAS ticket for urls().SSO_EMBED. */
  async _loginWidget(email, password) {
    try {
      const jar = new CookieJar();
      const f = this._fetch;

      // Step 2a: GET SSO embed page (establish cookies)
      const embedParams = querystring.stringify({
        clientId: 'GarminConnect',
        locale: 'en',
        service: GC_MODERN,
      });
      const embedResp = await f(`${urls().SSO_EMBED}?${embedParams}`, {
        headers: { 'User-Agent': USER_AGENT_BROWSER },
        redirect: 'manual',
      });
      jar.capture(embedResp);
      await embedResp.text();

      // Step 2b: GET signin page (extract CSRF token)
      const signinGetParams = querystring.stringify({
        id: 'gauth-widget',
        embedWidget: true,
        locale: 'en',
        gauthHost: urls().SSO_EMBED,
      });
      const signinGetResp = await f(`${urls().SSO_SIGNIN}?${signinGetParams}`, {
        headers: {
          'User-Agent': USER_AGENT_BROWSER,
          Cookie: jar.header(),
        },
        redirect: 'manual',
      });
      jar.capture(signinGetResp);
      const signinHtml = await signinGetResp.text();
      const csrfMatch = CSRF_RE.exec(signinHtml);
      if (!csrfMatch) {
        return { ok: false, error: 'CSRF token not found in signin page' };
      }
      const csrf = csrfMatch[1];

      // Step 2c: POST credentials + CSRF -> extract ticket
      const signinPostParams = querystring.stringify({
        id: 'gauth-widget',
        embedWidget: true,
        clientId: 'GarminConnect',
        locale: 'en',
        gauthHost: urls().SSO_EMBED,
        service: urls().SSO_EMBED,
        source: urls().SSO_EMBED,
        redirectAfterAccountLoginUrl: urls().SSO_EMBED,
        redirectAfterAccountCreationUrl: urls().SSO_EMBED,
      });
      const formBody = querystring.stringify({
        username: email,
        password: password,
        embed: 'true',
        _csrf: csrf,
      });
      const signinPostResp = await f(`${urls().SSO_SIGNIN}?${signinPostParams}`, {
        method: 'POST',
        headers: {
          'Content-Type': 'application/x-www-form-urlencoded',
          'User-Agent': USER_AGENT_BROWSER,
          Origin: urls().SSO_ORIGIN,
          Referer: urls().SSO_SIGNIN,
          Cookie: jar.header(),
          Dnt: '1',
        },
        body: formBody,
        redirect: 'manual',
      });
      jar.capture(signinPostResp);
      const postHtml = await signinPostResp.text();

      if (/var\s+status\s*=\s*"/.test(postHtml)) {
        return { ok: false, error: 'Account locked — unlock at connect.garmin.com' };
      }
      if (postHtml.includes('Update Phone Number')) {
        return { ok: false, error: 'Garmin requires phone number update — visit connect.garmin.com' };
      }

      if (signinPostResp.status === 429) return fatal(RATE_LIMITED, 'RATE_LIMITED');
      if (signinPostResp.status !== 200) {
        return { ok: false, errorCode: 'BLOCKED', error: `Garmin blocked the sign in (HTTP ${signinPostResp.status}). Try again later or from another network.` };
      }
      const ticketMatch = TICKET_RE.exec(postHtml);
      if (!ticketMatch) {
        return { ok: false, errorCode: 'BAD_PASSWORD', error: 'Wrong email or password, or the account needs two-step verification.' };
      }
      return { ok: true, ticket: ticketMatch[1] };
    } catch (err) {
      return { ok: false, error: err.message };
    }
  }

  /** Legacy garth path: S3 consumer + preauthorized (OAuth1) + exchange/user/2.0 (OAuth2). */
  async _legacyFromTicket(ticket) {
    try {
      const f = this._fetch;
      const consumerResp = await f(urls().OAUTH_CONSUMER_URL);
      if (!consumerResp.ok) {
        return { ok: false, error: `Failed to fetch OAuth consumer: ${consumerResp.status}` };
      }
      const consumer = await consumerResp.json();

      // Step 3: Exchange ticket for OAuth1 token
      const preAuthQueryParams = {
        ticket,
        'login-url': urls().SSO_EMBED,
        'accepts-mfa-tokens': 'true',
      };
      const preAuthUrl = `${urls().OAUTH_BASE}/preauthorized?${querystring.stringify(preAuthQueryParams)}`;

      const nonce1 = generateNonce();
      const ts1 = String(nowSeconds());
      const allParamsForSig = {
        oauth_consumer_key: consumer.consumer_key,
        oauth_nonce: nonce1,
        oauth_signature_method: 'HMAC-SHA1',
        oauth_timestamp: ts1,
        oauth_version: '1.0',
        ...preAuthQueryParams,
      };
      const sig1 = signOAuth1('GET', `${urls().OAUTH_BASE}/preauthorized`, allParamsForSig, consumer.consumer_secret);
      const authHeaderParams = {
        oauth_consumer_key: consumer.consumer_key,
        oauth_nonce: nonce1,
        oauth_signature: sig1,
        oauth_signature_method: 'HMAC-SHA1',
        oauth_timestamp: ts1,
        oauth_version: '1.0',
      };

      const preAuthResp = await f(preAuthUrl, {
        headers: {
          Authorization: oauthHeader(authHeaderParams),
          'User-Agent': USER_AGENT_MOBILE,
        },
      });
      if (!preAuthResp.ok) {
        const errText = await preAuthResp.text();
        return { ok: false, error: `OAuth1 preauthorized failed: ${preAuthResp.status} - ${errText.slice(0, 200)}` };
      }
      const oauth1Text = await preAuthResp.text();
      const oauth1Token = querystring.parse(oauth1Text);

      // Step 4: Exchange OAuth1 -> OAuth2
      const oauth2Result = await this._exchangeOAuth1ForOAuth2(consumer, oauth1Token);
      if (!oauth2Result.ok) return oauth2Result;

      // Persist tokens
      const tokenData = {
        oauth1: {
          oauth_token: oauth1Token.oauth_token,
          oauth_token_secret: oauth1Token.oauth_token_secret,
        },
        oauth2: oauth2Result.oauth2,
        consumer: {
          consumer_key: consumer.consumer_key,
          consumer_secret: consumer.consumer_secret,
        },
      };
      this._writeTokens(tokenData);

      return { ok: true };
    } catch (err) {
      return { ok: false, error: err.message };
    }
  }

  /**
   * Load tokens from disk and refresh them REFRESH_MARGIN_S before they expire.
   * @returns {{ ok: true, tokens: object } | { ok: false, error: string }}
   */
  async loadTokens() {
    try {
      let data = this._cachedTokens;
      if (!data) {
        if (!fs.existsSync(this.tokenPath)) {
          return { ok: false, error: 'No token file found — login required' };
        }
        try {
          data = JSON.parse(fs.readFileSync(this.tokenPath, 'utf8'));
        } catch {
          return { ok: false, error: 'Token file is corrupted — login required' };
        }
      }

      if (data.di && data.di.access_token) {
        if (nowSeconds() >= (data.di.expires_at || 0) - REFRESH_MARGIN_S) {
          const refreshed = await this.refresh(data);
          if (!refreshed.ok) return refreshed;
          data = refreshed.tokens;
        }
        this._cachedTokens = data;
        return { ok: true, tokens: data };
      }

      if (!data.oauth2 || !data.oauth1 || !data.consumer) {
        return { ok: false, error: 'Token file is incomplete — login required' };
      }
      // Legacy tokens: the OAuth1 token lasts about a year and mints new OAuth2 tokens,
      // so the 30 day OAuth2 refresh_token expiry is not a reason to sign in again.
      if (nowSeconds() >= (data.oauth2.expires_at || 0) - REFRESH_MARGIN_S) {
        const refreshed = await this.refresh(data);
        if (!refreshed.ok) return refreshed;
        data = refreshed.tokens;
      }
      this._cachedTokens = data;
      return { ok: true, tokens: data };
    } catch (err) {
      return { ok: false, error: err.message };
    }
  }

  /**
   * Get a new access token now (also used after a 401). Rewrites tokens.json.
   * @returns {{ ok: true, tokens: object } | { ok: false, error: string }}
   */
  async refresh(data = this._cachedTokens) {
    if (!data) {
      const loaded = await this.loadTokens();
      if (!loaded.ok) return loaded;
      data = loaded.tokens;
    }
    if (data.di) {
      if (!data.di.refresh_token) return { ok: false, error: 'No refresh token — login required' };
      const res = await this._diTokenRequest(data.di.client_id, {
        grant_type: 'refresh_token',
        client_id: data.di.client_id,
        refresh_token: data.di.refresh_token,
      });
      if (!res.ok) return { ok: false, error: `Token refresh failed (${res.error}) — login required` };
      const next = { di: res.di };
      this._writeTokens(next);
      return { ok: true, tokens: next };
    }
    const oauth2Result = await this._exchangeOAuth1ForOAuth2(data.consumer, data.oauth1);
    if (!oauth2Result.ok) return oauth2Result;
    const next = { ...data, oauth2: oauth2Result.oauth2 };
    this._writeTokens(next);
    return { ok: true, tokens: next };
  }

  /**
   * Return a valid access_token string, or throw.
   * @returns {Promise<string>}
   */
  async getAccessToken() {
    const result = await this.loadTokens();
    if (!result.ok) {
      throw new Error(result.error);
    }
    return result.tokens.di ? result.tokens.di.access_token : result.tokens.oauth2.access_token;
  }

  /** Headers for connectapi data calls: DI tokens need the native app headers. */
  apiHeaders(token) {
    const di = !!(this._cachedTokens && this._cachedTokens.di);
    return di
      ? { ...NATIVE_HEADERS, Authorization: `Bearer ${token}`, Accept: 'application/json' }
      : { Authorization: `Bearer ${token}`, 'User-Agent': USER_AGENT_MOBILE, Accept: 'application/json' };
  }

  /** 'di', 'legacy' or null, for logs. */
  tokenKind() {
    const d = this._cachedTokens;
    return d ? (d.di ? 'di' : 'legacy') : null;
  }

  /**
   * Delete the token file.
   * @returns {{ ok: true } | { ok: false, error: string }}
   */
  clearTokens() {
    try {
      this._cachedTokens = null;
      if (fs.existsSync(this.tokenPath)) {
        fs.unlinkSync(this.tokenPath);
      }
      return { ok: true };
    } catch (err) {
      return { ok: false, error: err.message };
    }
  }

  // -----------------------------------------------------------------------
  // Private helpers
  // -----------------------------------------------------------------------

  /**
   * Exchange OAuth1 token for OAuth2 bearer token.
   * This is Step 4 from the auth spike: POST to /oauth/exchange/user/2.0
   */
  async _exchangeOAuth1ForOAuth2(consumer, oauth1Token) {
    try {
      const f = this._fetch;
      const exchangeUrl = `${urls().OAUTH_BASE}/exchange/user/2.0`;
      const nonce = generateNonce();
      const ts = String(nowSeconds());
      const exchangeOAuthParams = {
        oauth_consumer_key: consumer.consumer_key,
        oauth_token: oauth1Token.oauth_token,
        oauth_nonce: nonce,
        oauth_signature_method: 'HMAC-SHA1',
        oauth_timestamp: ts,
        oauth_version: '1.0',
      };
      const sig = signOAuth1(
        'POST',
        exchangeUrl,
        exchangeOAuthParams,
        consumer.consumer_secret,
        oauth1Token.oauth_token_secret
      );
      exchangeOAuthParams.oauth_signature = sig;

      const exchangeQs = querystring.stringify(exchangeOAuthParams);
      const exchangeResp = await f(`${exchangeUrl}?${exchangeQs}`, {
        method: 'POST',
        headers: {
          'User-Agent': USER_AGENT_MOBILE,
          'Content-Type': 'application/x-www-form-urlencoded',
        },
      });
      if (!exchangeResp.ok) {
        const errText = await exchangeResp.text();
        return { ok: false, error: `OAuth2 exchange failed: ${exchangeResp.status} - ${errText.slice(0, 200)}` };
      }
      const oauth2 = await exchangeResp.json();
      const now = nowSeconds();

      return {
        ok: true,
        oauth2: {
          access_token: oauth2.access_token,
          token_type: oauth2.token_type,
          refresh_token: oauth2.refresh_token,
          expires_at: now + (oauth2.expires_in || 95760),
          refresh_token_expires_at: now + (oauth2.refresh_token_expires_in || 2592000),
        },
      };
    } catch (err) {
      return { ok: false, error: `OAuth2 exchange error: ${err.message}` };
    }
  }

  /** Write token data to disk with mode 0600 (enforced on both create and overwrite). */
  _writeTokens(data) {
    fs.mkdirSync(this.dataDir, { recursive: true });
    fs.writeFileSync(this.tokenPath, JSON.stringify(data, null, 2), { mode: 0o600 });
    try { fs.chmodSync(this.tokenPath, 0o600); } catch (_e) { /* best-effort on platforms w/o chmod */ }
    this._cachedTokens = data;
  }
}

// ---------------------------------------------------------------------------
// Factory
// ---------------------------------------------------------------------------
function createClient(dataDir, opts) {
  return new GarminAuth(dataDir, opts);
}

// ---------------------------------------------------------------------------
// Exports
// ---------------------------------------------------------------------------
module.exports = {
  createClient,
  GarminAuth,
  // Expose utilities for other modules that need them
  CookieJar,
  percentEncode,
  signOAuth1,
  oauthHeader,
  generateNonce,
  jwtClaims,
  fakeServer,
  apiBase,
  get API_BASE() { return apiBase(); },
  // Constants
  USER_AGENT_MOBILE,
  NATIVE_HEADERS,
};
