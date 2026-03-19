#!/usr/bin/env node
/**
 * Downloads font WOFF2 files from Google Fonts and saves them to
 * renderer/fonts/ so the app works offline and without network latency.
 *
 * Run manually:   node scripts/download-fonts.js
 * Or via npm:     npm run fonts
 *
 * Only downloads files that don't already exist (safe to re-run).
 * URLs are resolved dynamically from the Google Fonts CSS API so version
 * changes on Google's side don't break the script.
 */

const https = require('https');
const fs    = require('fs');
const path  = require('path');

const OUT_DIR = path.join(__dirname, '..', 'renderer', 'fonts');

// Fonts and weights we actually use (pruned from the full set).
const GOOGLE_FONTS_URL =
  'https://fonts.googleapis.com/css2' +
  '?family=Oxanium:wght@400;500;600;700' +
  '&family=JetBrains+Mono:wght@300;400' +
  '&family=Barlow+Condensed:wght@700' +
  '&display=swap';

// Output filename map: "Family weight" → file name
const FILE_MAP = {
  'Oxanium 400':          'oxanium-400.woff2',
  'Oxanium 500':          'oxanium-500.woff2',
  'Oxanium 600':          'oxanium-600.woff2',
  'Oxanium 700':          'oxanium-700.woff2',
  'JetBrains Mono 300':   'jetbrains-mono-300.woff2',
  'JetBrains Mono 400':   'jetbrains-mono-400.woff2',
  'Barlow Condensed 700': 'barlow-condensed-700.woff2',
};

function get(url, headers = {}) {
  return new Promise((resolve, reject) => {
    const opts = new URL(url);
    const ua = 'Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36';
    https.get({ host: opts.host, path: opts.pathname + opts.search, headers: { 'User-Agent': ua, ...headers } }, res => {
      if (res.statusCode >= 300 && res.statusCode < 400 && res.headers.location) {
        return get(res.headers.location, headers).then(resolve).catch(reject);
      }
      const chunks = [];
      res.on('data', d => chunks.push(d));
      res.on('end', () => resolve({ status: res.statusCode, body: Buffer.concat(chunks) }));
    }).on('error', reject);
  });
}

function downloadBinary(url, dest) {
  return new Promise((resolve, reject) => {
    if (fs.existsSync(dest)) {
      console.log(`  skip  ${path.basename(dest)} (already exists)`);
      return resolve(true);
    }
    get(url).then(({ status, body }) => {
      if (status !== 200) return reject(new Error(`HTTP ${status} for ${url}`));
      fs.writeFileSync(dest, body);
      resolve(false);
    }).catch(reject);
  });
}

function parseFontBlocks(css) {
  // Extract @font-face blocks and parse family, weight, src url
  const results = [];
  const blockRe = /@font-face\s*\{([^}]+)\}/g;
  let m;
  while ((m = blockRe.exec(css)) !== null) {
    const block = m[1];
    const family = (block.match(/font-family:\s*['"]?([^'";]+)['"]?/) || [])[1]?.trim();
    const weight = (block.match(/font-weight:\s*(\d+)/) || [])[1]?.trim();
    const url    = (block.match(/url\(([^)]+\.woff2)\)/) || [])[1]?.trim();
    if (family && weight && url) results.push({ family, weight, url });
  }
  return results;
}

(async () => {
  fs.mkdirSync(OUT_DIR, { recursive: true });

  console.log('Fetching Google Fonts CSS to resolve WOFF2 URLs…');
  const { status, body } = await get(GOOGLE_FONTS_URL);
  if (status !== 200) { console.error(`Failed to fetch font CSS: HTTP ${status}`); process.exit(1); }

  const css = body.toString('utf8');
  const blocks = parseFontBlocks(css);

  // Keep only the latin subset (last block per family+weight is typically latin)
  const seen = new Map();
  for (const b of blocks) {
    seen.set(`${b.family} ${b.weight}`, b); // later entries overwrite earlier (latin is last)
  }

  console.log(`\nDownloading ${seen.size} font files to renderer/fonts/\n`);
  let ok = 0, fail = 0;

  for (const [key, { url }] of seen) {
    const file = FILE_MAP[key];
    if (!file) { console.log(`  skip  (unmapped) ${key}`); continue; }
    const dest = path.join(OUT_DIR, file);
    try {
      process.stdout.write(`  fetch  ${file} … `);
      const skipped = await downloadBinary(url, dest);
      if (!skipped) {
        const kb = Math.round(fs.statSync(dest).size / 1024);
        console.log(`${kb} KB`);
      }
      ok++;
    } catch (e) {
      console.error(`FAILED: ${e.message}`);
      fail++;
    }
  }

  console.log(`\n${ok} fonts ready, ${fail} failed.`);
  if (fail > 0) process.exit(1);
})();
