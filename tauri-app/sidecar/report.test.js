import { test, expect } from 'bun:test';
import { buildReport, scrub } from './report.js';

test('scrub removes secrets, emails and home paths', () => {
  const s = scrub('user dani@example.com token=abc Authorization: Bearer xyz /Users/dani/Documents');
  expect(s).not.toContain('dani@example.com');
  expect(s).not.toContain('abc');
  expect(s).not.toContain('xyz');
  expect(s).toContain('~/Documents');
});

test('report URLs stay under client limits and keep the newest lines', () => {
  const log = Array.from({ length: 300 }, (_, i) => ({ ts: '10:00', type: 'dim', msg: `line ${i} ` + 'x'.repeat(60) }));
  const { issueUrl, mailtoUrl } = buildReport({ error: 'Auth failed for a@b.com', log, version: '4.0.0', os: 'macOS' });
  expect(issueUrl.length).toBeLessThanOrEqual(7500);
  expect(mailtoUrl.length).toBeLessThanOrEqual(1800);
  expect(issueUrl.startsWith('https://github.com/danihrndzld/garmin-health-exporter/issues/new?title=')).toBe(true);
  expect(mailtoUrl.startsWith('mailto:danielernesto@ufm.edu?subject=')).toBe(true);
  expect(decodeURIComponent(issueUrl)).toContain('line 299');
  expect(decodeURIComponent(issueUrl)).not.toContain('a@b.com');
});
