/**
 * Garmin Connect Auth Module
 *
 * Handles OAuth/SSO login, token persistence (mode 0600), and token refresh.
 * Replicates the mobile SSO flow proven in auth-spike.js.
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
const OAUTH_CONSUMER_URL = 'https://thegarth.s3.amazonaws.com/oauth_consumer.json';
const SSO_ORIGIN = 'https://sso.garmin.com';
const SSO_EMBED = `${SSO_ORIGIN}/sso/embed`;
const SSO_SIGNIN = `${SSO_ORIGIN}/sso/signin`;
const GC_MODERN = 'https://connect.garmin.com/modern';
const API_BASE = 'https://connectapi.garmin.com';
const OAUTH_BASE = `${API_BASE}/oauth-service/oauth`;

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
  }

  // -----------------------------------------------------------------------
  // Public API
  // -----------------------------------------------------------------------

  /**
   * Run the full SSO login flow and persist tokens.
   * @returns {{ ok: true } | { ok: false, error: string }}
   */
  async login(email, password) {
    try {
      const jar = new CookieJar();
      const f = this._fetch;

      // Step 1: Fetch OAuth consumer credentials
      const consumerResp = await f(OAUTH_CONSUMER_URL);
      if (!consumerResp.ok) {
        return { ok: false, error: `Failed to fetch OAuth consumer: ${consumerResp.status}` };
      }
      const consumer = await consumerResp.json();

      // Step 2a: GET SSO embed page (establish cookies)
      const embedParams = querystring.stringify({
        clientId: 'GarminConnect',
        locale: 'en',
        service: GC_MODERN,
      });
      const embedResp = await f(`${SSO_EMBED}?${embedParams}`, {
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
        gauthHost: SSO_EMBED,
      });
      const signinGetResp = await f(`${SSO_SIGNIN}?${signinGetParams}`, {
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
        gauthHost: SSO_EMBED,
        service: SSO_EMBED,
        source: SSO_EMBED,
        redirectAfterAccountLoginUrl: SSO_EMBED,
        redirectAfterAccountCreationUrl: SSO_EMBED,
      });
      const formBody = querystring.stringify({
        username: email,
        password: password,
        embed: 'true',
        _csrf: csrf,
      });
      const signinPostResp = await f(`${SSO_SIGNIN}?${signinPostParams}`, {
        method: 'POST',
        headers: {
          'Content-Type': 'application/x-www-form-urlencoded',
          'User-Agent': USER_AGENT_BROWSER,
          Origin: SSO_ORIGIN,
          Referer: SSO_SIGNIN,
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

      const ticketMatch = TICKET_RE.exec(postHtml);
      if (!ticketMatch) {
        return { ok: false, error: 'Ticket not found — likely bad credentials or MFA required' };
      }
      const ticket = ticketMatch[1];

      // Step 3: Exchange ticket for OAuth1 token
      const preAuthQueryParams = {
        ticket,
        'login-url': SSO_EMBED,
        'accepts-mfa-tokens': 'true',
      };
      const preAuthUrl = `${OAUTH_BASE}/preauthorized?${querystring.stringify(preAuthQueryParams)}`;

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
      const sig1 = signOAuth1('GET', `${OAUTH_BASE}/preauthorized`, allParamsForSig, consumer.consumer_secret);
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
        return { ok: false, error: `OAuth1 preauthorized failed: ${preAuthResp.status} - ${errText}` };
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
   * Load tokens from disk. Refreshes OAuth2 if access_token expired but
   * refresh_token is still valid.
   * @returns {{ ok: true, tokens: object } | { ok: false, error: string }}
   */
  async loadTokens() {
    try {
      if (!fs.existsSync(this.tokenPath)) {
        return { ok: false, error: 'No token file found — login required' };
      }

      let data;
      try {
        data = JSON.parse(fs.readFileSync(this.tokenPath, 'utf8'));
      } catch {
        return { ok: false, error: 'Token file is corrupted — login required' };
      }

      if (!data.oauth2 || !data.oauth1 || !data.consumer) {
        return { ok: false, error: 'Token file is incomplete — login required' };
      }

      const now = nowSeconds();

      // Check if refresh token has expired
      if (data.oauth2.refresh_token_expires_at && now >= data.oauth2.refresh_token_expires_at) {
        return { ok: false, error: 'Refresh token expired — login required' };
      }

      // Check if access token has expired
      if (data.oauth2.expires_at && now >= data.oauth2.expires_at) {
        // Refresh using OAuth1 -> OAuth2 exchange
        const oauth2Result = await this._exchangeOAuth1ForOAuth2(data.consumer, data.oauth1);
        if (!oauth2Result.ok) return oauth2Result;

        data.oauth2 = oauth2Result.oauth2;
        this._writeTokens(data);
      }

      return { ok: true, tokens: data };
    } catch (err) {
      return { ok: false, error: err.message };
    }
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
    return result.tokens.oauth2.access_token;
  }

  /**
   * Delete the token file.
   * @returns {{ ok: true } | { ok: false, error: string }}
   */
  clearTokens() {
    try {
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
      const exchangeUrl = `${OAUTH_BASE}/exchange/user/2.0`;
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
        return { ok: false, error: `OAuth2 exchange failed: ${exchangeResp.status} - ${errText}` };
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

  /** Write token data to disk with mode 0600. */
  _writeTokens(data) {
    fs.mkdirSync(this.dataDir, { recursive: true });
    fs.writeFileSync(this.tokenPath, JSON.stringify(data, null, 2), { mode: 0o600 });
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
  // Constants
  API_BASE,
  USER_AGENT_MOBILE,
};
