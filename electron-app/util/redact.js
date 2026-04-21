/**
 * Shared secret redaction.
 *
 * Used by both the Garmin HTTP client (for log lines and error meta) and the
 * main process (for diagnostic bundles and the mailto body) so that one
 * denylist governs what leaves the user's machine.
 */

// Query-param keys whose values are always stripped.
const SECRET_QUERY_KEYS = [
  'token', 'access_token', 'refresh_token', 'id_token',
  'code', 'sig', 'signature', 'key', 'apikey', 'api_key',
  'jwt', 'sessionid', 'session_id', 'authorization',
];

// Key substrings that force redaction even with variant spellings.
const SECRET_SUBSTRINGS = ['auth', 'secret', 'password', 'passwd', 'pwd'];

function isSecretKey(key) {
  const k = key.toLowerCase();
  if (SECRET_QUERY_KEYS.includes(k)) return true;
  return SECRET_SUBSTRINGS.some((s) => k.includes(s));
}

/** Redact secret query parameters in a URL string. */
function redactUrl(url) {
  if (!url || typeof url !== 'string') return url;
  return url.replace(/([?&])([^=&]+)=([^&]*)/g, (match, sep, key) => {
    if (isSecretKey(key)) return `${sep}${key}=REDACTED`;
    return match;
  });
}

/**
 * Redact secrets from an arbitrary log line or serialized blob. Handles:
 *   - Bearer / Basic Authorization values
 *   - Cookie / Set-Cookie header dumps
 *   - Secret-looking query params (via redactUrl)
 *   - JWT-shaped tokens anywhere in the string
 *   - JSON-style "key":"value" pairs for sensitive keys
 */
function redactString(input) {
  if (input == null) return input;
  let s = String(input);

  // Authorization: Bearer / Basic <token>
  s = s.replace(/(Authorization:\s*)(Bearer|Basic)\s+\S+/gi, '$1$2 REDACTED');
  s = s.replace(/\b(Bearer|Basic)\s+[A-Za-z0-9._~+/=-]+/gi, '$1 REDACTED');

  // Cookie / Set-Cookie header dumps (single line)
  s = s.replace(/((?:set-)?cookie:\s*)[^\r\n]+/gi, '$1REDACTED');

  // JWT-shaped tokens (three base64url segments)
  s = s.replace(/eyJ[A-Za-z0-9_-]+\.[A-Za-z0-9_-]+\.[A-Za-z0-9_-]+/g, 'REDACTED_JWT');

  // JSON-style sensitive key/value pairs
  s = s.replace(
    /"([^"]+)"\s*:\s*"([^"]*)"/g,
    (match, key, value) => (isSecretKey(key) ? `"${key}":"REDACTED"` : match),
  );

  // Query-param secrets (covers any URL embedded in the string)
  s = s.replace(/([?&])([^=&\s"']+)=([^&\s"']*)/g, (match, sep, key) => {
    if (isSecretKey(key)) return `${sep}${key}=REDACTED`;
    return match;
  });

  return s;
}

module.exports = { redactUrl, redactString, isSecretKey };
