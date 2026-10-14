// Bug report links: a prefilled GitHub issue and a mailto fallback.
// Everything is scrubbed here (sidecar = trust boundary) before it leaves the machine.
const { redactString, isSecretKey } = require('../../electron-app/util/redact');

export const REPO = 'danihrndzld/garmin-health-exporter';
export const EMAIL = 'danielernesto@ufm.edu';
const ISSUE_URL_MAX = 7500; // GitHub rejects much longer prefilled URLs
const MAILTO_URL_MAX = 1800; // most mail clients cap the whole URL near 2 KB

export function scrub(s) {
  return redactString(String(s ?? ''))
    .replace(/\b([\w-]+)=([^\s&"']+)/g, (m, k) => (isSecretKey(k) ? `${k}=REDACTED` : m))
    .replace(/[\w.+-]+@[\w-]+(\.[\w-]+)+/g, '[email]')
    .replace(/\/Users\/[^/\s]+/g, '~');
}

// Keep the newest lines: drop from the top until the encoded URL fits.
function fit(prefix, head, lines, max) {
  let tail = lines.slice();
  const url = () => prefix + encodeURIComponent(head + tail.join('\n') + (tail.length ? '\n```\n' : ''));
  while (tail.length && url().length > max) tail.shift();
  return url();
}

export function buildReport({ error, log = [], version = '?', os = '?' }) {
  const err = scrub(error || 'Unknown error');
  const lines = log.slice(-200).map((l) => `${scrub(l.ts)} [${scrub(l.type)}] ${scrub(l.msg)}`);
  const title = `Export failed: ${err}`.slice(0, 120);
  const head = `**Error:** ${err}\n\n**App:** v${scrub(version)} · ${scrub(os)}\n\n**Log (newest last):**\n\`\`\`\n`;
  const issueUrl = fit(`https://github.com/${REPO}/issues/new?title=${encodeURIComponent(title)}&body=`, head, lines, ISSUE_URL_MAX);
  const mailHead = `Error: ${err}\nApp: v${scrub(version)} · ${scrub(os)}\n\nLog:\n\`\`\`\n`;
  const mailtoUrl = fit(`mailto:${EMAIL}?subject=${encodeURIComponent('Garmin Export: ' + title)}&body=`, mailHead, lines, MAILTO_URL_MAX);
  return { issueUrl, mailtoUrl };
}
