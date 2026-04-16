#!/usr/bin/env node
/**
 * Auth Spike — Unit 0
 * Proves that Garmin Connect SSO can be replicated in pure JS using fetch.
 *
 * Flow (derived from garmin-connect npm package HttpClient.js):
 * 1. Fetch OAuth consumer key/secret from S3
 * 2. Web SSO: GET embed → GET signin (extract CSRF) → POST credentials (extract ticket)
 * 3. OAuth1: GET /preauthorized?ticket=... with HMAC-SHA1 → get OAuth1 token
 * 4. Exchange: POST OAuth1→OAuth2 → get Bearer token
 * 5. Test: Make one authenticated API call (user settings)
 *
 * Usage: node auth-spike.js <email> <password>
 */

const crypto = require('crypto');
const querystring = require('querystring');

// --- Constants ---
const OAUTH_CONSUMER_URL = 'https://thegarth.s3.amazonaws.com/oauth_consumer.json';
const SSO_ORIGIN = 'https://sso.garmin.com';
const SSO_EMBED = `${SSO_ORIGIN}/sso/embed`;
const SSO_SIGNIN = `${SSO_ORIGIN}/sso/signin`;
const GC_MODERN = 'https://connect.garmin.com/modern';
const API_BASE = 'https://connectapi.garmin.com';
const OAUTH_BASE = `${API_BASE}/oauth-service/oauth`;

const USER_AGENT_BROWSER = 'Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/117.0.0.0 Safari/537.36';
const USER_AGENT_MOBILE = 'com.garmin.android.apps.connectmobile';

const CSRF_RE = /name="_csrf"\s+value="(.+?)"/;
const TICKET_RE = /ticket=([^"]+)"/;

// --- Cookie Jar ---
// fetch doesn't manage cookies; we track them manually.
class CookieJar {
  constructor() {
    this.cookies = new Map();
  }

  /** Parse Set-Cookie headers from a fetch Response and store them. */
  capture(response) {
    // In Node.js fetch, getSetCookie() returns an array of Set-Cookie values
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

// --- OAuth 1.0a Signing (HMAC-SHA1) ---

function percentEncode(str) {
  return encodeURIComponent(str)
    .replace(/!/g, '%21')
    .replace(/\*/g, '%2A')
    .replace(/'/g, '%27')
    .replace(/\(/g, '%28')
    .replace(/\)/g, '%29');
}

function buildBaseString(method, url, params) {
  const sorted = Object.keys(params).sort()
    .map(k => `${percentEncode(k)}=${percentEncode(params[k])}`)
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
  const parts = Object.keys(oauthParams).sort()
    .map(k => `${percentEncode(k)}="${percentEncode(oauthParams[k])}"`);
  return `OAuth ${parts.join(', ')}`;
}

function generateNonce() {
  return crypto.randomBytes(16).toString('hex');
}

function nowSeconds() {
  return Math.floor(Date.now() / 1000).toString();
}

// --- Main Auth Flow ---

async function authSpike(email, password) {
  const jar = new CookieJar();
  const log = (step, msg) => console.log(`[Step ${step}] ${msg}`);

  // Step 1: Fetch OAuth consumer credentials
  log(1, 'Fetching OAuth consumer key/secret from S3...');
  const consumerResp = await fetch(OAUTH_CONSUMER_URL);
  if (!consumerResp.ok) throw new Error(`Failed to fetch OAuth consumer: ${consumerResp.status}`);
  const consumer = await consumerResp.json();
  log(1, `Consumer key: ${consumer.consumer_key.slice(0, 8)}...`);

  // Step 2a: GET SSO embed page (establish cookies)
  log(2, 'GET SSO embed page...');
  const embedParams = querystring.stringify({
    clientId: 'GarminConnect',
    locale: 'en',
    service: GC_MODERN
  });
  const embedResp = await fetch(`${SSO_EMBED}?${embedParams}`, {
    headers: { 'User-Agent': USER_AGENT_BROWSER },
    redirect: 'manual'
  });
  jar.capture(embedResp);
  await embedResp.text();
  log(2, `Embed response: ${embedResp.status}, cookies: ${jar.cookies.size}`);

  // Step 2b: GET signin page (extract CSRF token)
  log(2, 'GET signin page for CSRF token...');
  const signinGetParams = querystring.stringify({
    id: 'gauth-widget',
    embedWidget: true,
    locale: 'en',
    gauthHost: SSO_EMBED
  });
  const signinGetResp = await fetch(`${SSO_SIGNIN}?${signinGetParams}`, {
    headers: {
      'User-Agent': USER_AGENT_BROWSER,
      Cookie: jar.header()
    },
    redirect: 'manual'
  });
  jar.capture(signinGetResp);
  const signinHtml = await signinGetResp.text();
  const csrfMatch = CSRF_RE.exec(signinHtml);
  if (!csrfMatch) {
    console.error('Signin HTML snippet:', signinHtml.slice(0, 500));
    throw new Error('CSRF token not found in signin page');
  }
  const csrf = csrfMatch[1];
  log(2, `CSRF token: ${csrf.slice(0, 12)}...`);

  // Step 2c: POST credentials + CSRF → extract ticket
  log(2, 'POST credentials to signin...');
  const signinPostParams = querystring.stringify({
    id: 'gauth-widget',
    embedWidget: true,
    clientId: 'GarminConnect',
    locale: 'en',
    gauthHost: SSO_EMBED,
    service: SSO_EMBED,
    source: SSO_EMBED,
    redirectAfterAccountLoginUrl: SSO_EMBED,
    redirectAfterAccountCreationUrl: SSO_EMBED
  });
  const formBody = querystring.stringify({
    username: email,
    password: password,
    embed: 'true',
    _csrf: csrf
  });
  const signinPostResp = await fetch(`${SSO_SIGNIN}?${signinPostParams}`, {
    method: 'POST',
    headers: {
      'Content-Type': 'application/x-www-form-urlencoded',
      'User-Agent': USER_AGENT_BROWSER,
      Origin: SSO_ORIGIN,
      Referer: SSO_SIGNIN,
      Cookie: jar.header(),
      Dnt: '1'
    },
    body: formBody,
    redirect: 'manual'
  });
  jar.capture(signinPostResp);
  const postHtml = await signinPostResp.text();

  // Check for account locked
  if (/var\s+status\s*=\s*"/.test(postHtml)) {
    throw new Error('Account locked — unlock at connect.garmin.com');
  }
  // Check for phone number update requirement
  if (postHtml.includes('Update Phone Number')) {
    throw new Error('Garmin requires phone number update — visit connect.garmin.com');
  }

  const ticketMatch = TICKET_RE.exec(postHtml);
  if (!ticketMatch) {
    console.error('Post-signin HTML snippet:', postHtml.slice(0, 1000));
    throw new Error('Ticket not found — likely bad credentials or MFA required');
  }
  const ticket = ticketMatch[1];
  log(2, `Ticket: ${ticket.slice(0, 16)}...`);

  // Step 3: Exchange ticket for OAuth1 token
  log(3, 'Exchanging ticket for OAuth1 token...');
  const preAuthQueryParams = {
    ticket,
    'login-url': SSO_EMBED,
    'accepts-mfa-tokens': 'true'
  };
  const preAuthUrl = `${OAUTH_BASE}/preauthorized?${querystring.stringify(preAuthQueryParams)}`;

  // Build OAuth1 signature — include query params in signature base string
  const nonce1 = generateNonce();
  const ts1 = nowSeconds();
  const allParamsForSig = {
    oauth_consumer_key: consumer.consumer_key,
    oauth_nonce: nonce1,
    oauth_signature_method: 'HMAC-SHA1',
    oauth_timestamp: ts1,
    oauth_version: '1.0',
    ...preAuthQueryParams
  };
  const sig1 = signOAuth1('GET', `${OAUTH_BASE}/preauthorized`, allParamsForSig, consumer.consumer_secret);
  const authHeaderParams = {
    oauth_consumer_key: consumer.consumer_key,
    oauth_nonce: nonce1,
    oauth_signature: sig1,
    oauth_signature_method: 'HMAC-SHA1',
    oauth_timestamp: ts1,
    oauth_version: '1.0'
  };

  const preAuthResp = await fetch(preAuthUrl, {
    headers: {
      Authorization: oauthHeader(authHeaderParams),
      'User-Agent': USER_AGENT_MOBILE
    }
  });
  if (!preAuthResp.ok) {
    const errText = await preAuthResp.text();
    throw new Error(`OAuth1 preauthorized failed: ${preAuthResp.status} - ${errText}`);
  }
  const oauth1Text = await preAuthResp.text();
  const oauth1Token = querystring.parse(oauth1Text);
  log(3, `OAuth1 token: ${oauth1Token.oauth_token?.slice(0, 12)}...`);

  // Step 4: Exchange OAuth1 → OAuth2
  log(4, 'Exchanging OAuth1 for OAuth2 Bearer token...');
  const exchangeUrl = `${OAUTH_BASE}/exchange/user/2.0`;
  const nonce2 = generateNonce();
  const ts2 = nowSeconds();
  const exchangeOAuthParams = {
    oauth_consumer_key: consumer.consumer_key,
    oauth_token: oauth1Token.oauth_token,
    oauth_nonce: nonce2,
    oauth_signature_method: 'HMAC-SHA1',
    oauth_timestamp: ts2,
    oauth_version: '1.0'
  };
  const sig2 = signOAuth1(
    'POST', exchangeUrl, exchangeOAuthParams,
    consumer.consumer_secret, oauth1Token.oauth_token_secret
  );
  exchangeOAuthParams.oauth_signature = sig2;

  // npm package passes all OAuth params as query string
  const exchangeQs = querystring.stringify(exchangeOAuthParams);
  const exchangeResp = await fetch(`${exchangeUrl}?${exchangeQs}`, {
    method: 'POST',
    headers: {
      'User-Agent': USER_AGENT_MOBILE,
      'Content-Type': 'application/x-www-form-urlencoded'
    }
  });
  if (!exchangeResp.ok) {
    const errText = await exchangeResp.text();
    throw new Error(`OAuth2 exchange failed: ${exchangeResp.status} - ${errText}`);
  }
  const oauth2 = await exchangeResp.json();
  log(4, `Bearer token: ${oauth2.access_token?.slice(0, 16)}...`);
  log(4, `Expires in: ${oauth2.expires_in}s, Refresh expires in: ${oauth2.refresh_token_expires_in}s`);

  // Step 5: Test authenticated API call
  log(5, 'Testing authenticated API call (user settings)...');
  const testResp = await fetch(`${API_BASE}/userprofile-service/userprofile/user-settings/`, {
    headers: {
      Authorization: `Bearer ${oauth2.access_token}`,
      'User-Agent': USER_AGENT_MOBILE
    }
  });
  if (!testResp.ok) {
    throw new Error(`API test call failed: ${testResp.status}`);
  }
  const userData = await testResp.json();
  const displayName = userData?.userData?.displayName ?? userData?.displayName ?? 'unknown';
  log(5, `Authenticated as: ${displayName}`);

  // Summary
  console.log('\n=== AUTH SPIKE RESULT: PASS ===');
  console.log('Findings:');
  console.log(`  - OAuth consumer: fetched from S3 (key: ${consumer.consumer_key.slice(0, 8)}...)`);
  console.log(`  - SSO flow: web embed (CSRF + form POST), NOT mobile API`);
  console.log(`  - Cookie management: manual jar required (${jar.cookies.size} cookies tracked)`);
  console.log(`  - OAuth1 signing: HMAC-SHA1 with consumer secret + token secret`);
  console.log(`  - OAuth2 token type: ${oauth2.token_type}, expires_in: ${oauth2.expires_in}s`);
  console.log(`  - Refresh token: present=${!!oauth2.refresh_token}, expires_in: ${oauth2.refresh_token_expires_in}s`);
  console.log(`  - API base: ${API_BASE} (NOT connect.garmin.com)`);
  console.log(`  - User-Agent: mobile app UA for OAuth/API, browser UA for SSO`);

  return {
    ok: true,
    oauth1Token,
    oauth2Token: oauth2,
    consumer,
    findings: {
      apiBase: API_BASE,
      ssoFlow: 'web-embed-csrf',
      cookiesRequired: true,
      oauth1SigningMethod: 'HMAC-SHA1',
      tokenExpiresIn: oauth2.expires_in,
      refreshTokenExpiresIn: oauth2.refresh_token_expires_in
    }
  };
}

// --- CLI Entry ---
if (require.main === module) {
  const [,, email, password] = process.argv;
  if (!email || !password) {
    console.error('Usage: node auth-spike.js <email> <password>');
    process.exit(1);
  }
  authSpike(email, password)
    .then(() => {
      console.log('\nSpike completed successfully.');
      process.exit(0);
    })
    .catch(err => {
      console.error('\n=== AUTH SPIKE RESULT: FAIL ===');
      console.error(err.message);
      process.exit(1);
    });
}

module.exports = { authSpike };
