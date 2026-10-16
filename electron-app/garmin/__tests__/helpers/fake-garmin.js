'use strict';
// A local stand-in for every Garmin host the app talks to: SSO (old widget flow
// and new mobile login), diauth (DI OAuth tokens), the S3 OAuth consumer file,
// connectapi's oauth-service (legacy OAuth1 -> OAuth2) and the connectapi data
// endpoints, which replay a scrubbed export (fixtures/export-sample.json).
//
// Tests point the app at it with GARMIN_FAKE_SERVER=http://127.0.0.1:<port>.
// `server.control` changes behavior per test, and `server.calls` records every request.
const http = require('http');
const { getEndpoint, dailyEndpoints, aggregatedEndpoints } = require('../../endpoints');


function jwt(claims) {
  const b64 = (o) => Buffer.from(JSON.stringify(o)).toString('base64url');
  return `${b64({ alg: 'RS256', typ: 'JWT' })}.${b64(claims)}.sig`;
}

/** Map every data URL the exporter will request for the fixture to its response body. */
function routesFor(fixture) {
  const routes = new Map();
  const add = (url, data) => routes.set(url.replace(/^https?:\/\/[^/]+/, ''), data);
  const dn = 'displayName-redacted';
  for (const [date, day] of Object.entries(fixture.daily)) {
    for (const ep of dailyEndpoints) add(ep.buildUrl({ date, displayName: dn }), day[ep.name] ?? null);
  }
  const { start, end } = fixture.date_range;
  for (const ep of aggregatedEndpoints) {
    if (ep.name === 'daily_steps') continue; // chunked: handled by prefix below
    add(ep.buildUrl({ startDate: start, endDate: end, displayName: dn }), fixture.aggregated[ep.name] ?? null);
  }
  for (const a of fixture.activities) {
    add(getEndpoint('activity_splits').buildUrl({ activityId: a.activityId }), a.splits ?? null);
    for (const name of ['activity_split_summaries', 'activity_typed_splits']) {
      const ep = getEndpoint(name);
      if (ep) add(ep.buildUrl({ activityId: a.activityId }), a.typed_splits ?? null);
    }
  }
  return routes;
}

function startFakeGarmin(fixture, opts = {}) {
  const routes = routesFor(fixture);
  const activities = fixture.activities.map(({ splits, typed_splits, ...a }) => a);
  const control = {
    password: 'right-password',
    mobileLogin: 'ok',      // 'ok' | 'mfa' | 'cloudflare' (403) | '429' | 'captcha'
    diauth: 'ok',           // 'ok' | '429' | 'fail' (400) | '500'
    tokenTtl: 3600,
    dataStatus: {},         // { '<endpoint path prefix>': status } to force 401/403/429/500 on data calls
    failOnce: {},           // { '<path prefix>': status } answers once, then normal
    ...opts,
  };
  const calls = [];
  let issued = 0;
  const validTokens = new Set();

  const send = (res, status, body, headers = {}) => {
    const text = body === undefined || body === null ? '' : typeof body === 'string' ? body : JSON.stringify(body);
    res.writeHead(status, { 'content-type': typeof body === 'string' ? 'text/html' : 'application/json', ...headers });
    res.end(status === 204 ? undefined : text);
  };

  const newDiToken = () => {
    const access = jwt({ client_id: 'GARMIN_CONNECT_MOBILE_ANDROID_DI_2025Q2', exp: Math.floor(Date.now() / 1000) + control.tokenTtl, n: ++issued });
    validTokens.add(access);
    return { access_token: access, token_type: 'bearer', refresh_token: `refresh-${issued}`, expires_in: control.tokenTtl, refresh_token_expires_in: 2592000, scope: 'x', jti: String(issued) };
  };

  const server = http.createServer(async (req, res) => {
    const body = await new Promise((r) => { let b = ''; req.on('data', (c) => (b += c)); req.on('end', () => r(b)); });
    const url = new URL(req.url, 'http://x');
    const p = url.pathname;
    calls.push({ method: req.method, path: p, search: url.search, headers: req.headers, body });

    // --- New login: SSO mobile JSON API ---------------------------------
    if (p === '/mobile/api/login' && req.method === 'POST') {
      if (control.mobileLogin === '429') return send(res, 429, { error: 'Too Many Requests' });
      if (control.mobileLogin === 'cloudflare') return send(res, 403, '<html>Just a moment...</html>');
      if (control.mobileLogin === 'captcha') return send(res, 200, { responseStatus: { type: 'CAPTCHA_REQUIRED' } });
      const { password } = JSON.parse(body || '{}');
      if (password !== control.password) return send(res, 200, { responseStatus: { type: 'INVALID_USERNAME_PASSWORD' } });
      if (control.mobileLogin === 'mfa') return send(res, 200, { responseStatus: { type: 'MFA_REQUIRED' }, customerMfaInfo: { mfaLastMethodUsed: 'email' } });
      return send(res, 200, { responseStatus: { type: 'SUCCESSFUL' }, serviceTicketId: 'ST-mobile-1' });
    }

    // --- DI OAuth token service -----------------------------------------
    if (p === '/di-oauth2-service/oauth/token' && req.method === 'POST') {
      if (control.diauth === '429') return send(res, 429, { error: 'rate' });
      if (control.diauth === 'fail') return send(res, 400, { error: 'invalid_grant' });
      if (control.diauth === '500') return send(res, 500, { error: 'server_error' });
      const form = new URLSearchParams(body);
      if (form.get('grant_type') === 'refresh_token') {
        if (!/^refresh-/.test(form.get('refresh_token') || '')) return send(res, 400, { error: 'invalid_grant' });
        return send(res, 200, newDiToken());
      }
      if (!/^ST-/.test(form.get('service_ticket') || '')) return send(res, 400, { error: 'invalid_grant' });
      return send(res, 200, newDiToken());
    }

    // --- Old login: S3 consumer, widget SSO, OAuth1 -> OAuth2 ------------
    if (p === '/oauth_consumer.json') return send(res, 200, { consumer_key: 'ck', consumer_secret: 'cs' });
    if (p === '/sso/embed') return send(res, 200, '<html>embed</html>', { 'set-cookie': 'GARMIN-SSO=1; Path=/' });
    if (p === '/sso/signin' && req.method === 'GET') return send(res, 200, '<input name="_csrf" value="csrf-1">');
    if (p === '/sso/signin' && req.method === 'POST') {
      if (control.widget429) return send(res, 429, '<html>Too Many Requests</html>');
      if (control.widgetStatus) return send(res, control.widgetStatus, '<html>Blocked</html>');
      const form = new URLSearchParams(body);
      if (form.get('password') !== control.password) return send(res, 200, '<title>GARMIN Authentication Application</title> invalid');
      return send(res, 200, '<title>Success</title><script>var response_url = "https://sso.garmin.com/sso/embed?ticket=ST-widget-1";</script>');
    }
    if (p === '/oauth-service/oauth/preauthorized') return send(res, 200, 'oauth_token=ot&oauth_token_secret=os');
    if (p === '/oauth-service/oauth/exchange/user/2.0') {
      const access = `legacy-${++issued}`;
      validTokens.add(access);
      return send(res, 200, { access_token: access, token_type: 'Bearer', refresh_token: 'lr', expires_in: control.tokenTtl, refresh_token_expires_in: 2592000 });
    }

    // --- connectapi data endpoints ----------------------------------------
    const token = (req.headers.authorization || '').replace(/^Bearer /, '');
    if (!validTokens.has(token)) return send(res, 401, { message: 'token not active' });
    for (const [prefix, status] of Object.entries(control.failOnce)) {
      if (p.startsWith(prefix)) { delete control.failOnce[prefix]; return send(res, status, { message: 'forced once' }, status === 429 ? { 'retry-after': '1' } : {}); }
    }
    for (const [prefix, status] of Object.entries(control.dataStatus)) {
      if (p.startsWith(prefix)) return send(res, status, { message: 'forced' }, status === 429 ? { 'retry-after': '1' } : {});
    }
    if (p === '/userprofile-service/socialProfile') return send(res, 200, { displayName: 'displayName-redacted', id: 1 });
    if (p === '/activitylist-service/activities/search/activities') {
      const start = Number(url.searchParams.get('start') || 0), limit = Number(url.searchParams.get('limit') || 20);
      return send(res, 200, activities.slice(start, start + limit));
    }
    if (p.startsWith('/usersummary-service/stats/steps/daily/')) return send(res, 200, fixture.aggregated.daily_steps || []);
    if (p.startsWith('/goal-service/goal/goals')) return send(res, 200, fixture.goals || []);
    const key = p + url.search;
    if (routes.has(key)) {
      const data = routes.get(key);
      return data === null ? send(res, 204) : send(res, 200, data);
    }
    return send(res, 404, { message: `fake garmin has no route for ${key}` });
  });

  return new Promise((resolve) => server.listen(0, '127.0.0.1', () => {
    const base = `http://127.0.0.1:${server.address().port}`;
    resolve({ base, control, calls, validTokens, close: () => new Promise((r) => server.close(r)), newDiToken });
  }));
}

module.exports = { startFakeGarmin, jwt };
