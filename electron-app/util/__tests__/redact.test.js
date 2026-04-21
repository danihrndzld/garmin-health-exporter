const assert = require('node:assert/strict');
const { test } = require('node:test');
const { redactUrl, redactString, isSecretKey } = require('../redact');

test('redactUrl scrubs token, access_token, auth, secret, password, and refresh_token', () => {
  const url = 'https://api.example.com/x?token=abc&access_token=def&authCode=ghi&secret=s&password=p&refresh_token=rt&sig=zz&keep=ok';
  const out = redactUrl(url);
  assert.match(out, /token=REDACTED/);
  assert.match(out, /access_token=REDACTED/);
  assert.match(out, /authCode=REDACTED/);
  assert.match(out, /secret=REDACTED/);
  assert.match(out, /password=REDACTED/);
  assert.match(out, /refresh_token=REDACTED/);
  assert.match(out, /sig=REDACTED/);
  assert.match(out, /keep=ok/);
});

test('redactUrl leaves non-URL strings untouched', () => {
  assert.equal(redactUrl(''), '');
  assert.equal(redactUrl(null), null);
  assert.equal(redactUrl('plain text'), 'plain text');
});

test('redactString scrubs Bearer tokens (any case)', () => {
  assert.match(redactString('Authorization: Bearer eyJ0.abc.def'), /Bearer REDACTED/);
  assert.match(redactString('auth: bearer abc123.def-ghi'), /bearer REDACTED/i);
});

test('redactString scrubs Basic auth', () => {
  assert.match(redactString('Authorization: Basic dXNlcjpwYXNz'), /Basic REDACTED/);
});

test('redactString scrubs Cookie / Set-Cookie headers', () => {
  assert.match(redactString('Cookie: SESSIONID=abc; other=xyz'), /Cookie: REDACTED/i);
  assert.match(redactString('Set-Cookie: JWT=eyJ...; Path=/'), /Set-Cookie: REDACTED/i);
});

test('redactString scrubs JWT-shaped tokens anywhere in the string', () => {
  const out = redactString('error with token eyJhbGciOiJIUzI1NiJ9.eyJzdWIiOiIxMjM0In0.sig embedded');
  assert.match(out, /REDACTED_JWT/);
  assert.doesNotMatch(out, /eyJhbGciOi/);
});

test('redactString scrubs JSON-style sensitive key/value pairs', () => {
  const out = redactString('{"password":"hunter2","token":"abc","keep":"ok"}');
  assert.match(out, /"password":"REDACTED"/);
  assert.match(out, /"token":"REDACTED"/);
  assert.match(out, /"keep":"ok"/);
});

test('redactString scrubs secret query params in embedded URLs', () => {
  const out = redactString('GET https://x/y?access_token=abc&name=me failed');
  assert.match(out, /access_token=REDACTED/);
  assert.match(out, /name=me/);
});

test('redactString passes through null/empty', () => {
  assert.equal(redactString(null), null);
  assert.equal(redactString(undefined), undefined);
  assert.equal(redactString(''), '');
});

test('isSecretKey recognizes variants', () => {
  assert.equal(isSecretKey('token'), true);
  assert.equal(isSecretKey('Access_Token'), true);
  assert.equal(isSecretKey('clientSecret'), true);
  assert.equal(isSecretKey('X-Auth-Token'), true);
  assert.equal(isSecretKey('jwt'), true);
  assert.equal(isSecretKey('username'), false);
  assert.equal(isSecretKey('date'), false);
});
