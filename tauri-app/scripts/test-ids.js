// Lists test IDs that name more than one test, and IDs that appear in a test
// file but in no test name (so they never reach the JUnit output).
// Usage: node scripts/test-ids.js   (or bun; exit 1 when it finds a problem). Part of `bun run test`.
import { readdirSync, readFileSync } from 'node:fs';
import { join, relative } from 'node:path';
import { fileURLToPath } from 'node:url';

const root = fileURLToPath(new URL('..', import.meta.url)); // node and bun
const ID = String.raw`(?:AC|BVA|CL|DT-R|EG|EP|ST-T|ST-I)-?\d+`;
const anyId = new RegExp(String.raw`(?<!ports )\b(${ID})\b`, 'g');
// A test name: the first string of test(), it() or describe(), a JS string that is only
// an ID (a data row whose title is built with \${id}), Go's t.Run(), or a Go string
// literal that starts with an ID and is not a t.Error/t.Fatal/t.Log message.
// ponytail: a Go row is trusted to reach t.Run(row.name); the script does not follow the loop.
const jsName = new RegExp(String.raw`\b(?:test|it|describe)(?:\.\w+)*\(\s*['"\x60](${ID})\b|['"](${ID})['"]`, 'g');
const goName = new RegExp(String.raw`(?<!\b(?:Errorf?|Fatalf?|Logf?|Skipf?|Sprintf)\()"(${ID})\b`, 'g');

const files = [];
(function walk(dir) {
  for (const e of readdirSync(dir, { withFileTypes: true })) {
    const p = join(dir, e.name);
    if (e.isDirectory()) { if (!['node_modules', 'target', 'test-results', 'playwright-report'].includes(e.name)) walk(p); }
    else if (/(_test\.go|\.test\.js|\.spec\.js)$/.test(e.name)) files.push(p);
  }
})(root);

const named = new Map(); // id -> ["file:line", ...]
const seen = new Map(); // id -> first "file:line"
for (const f of files) {
  readFileSync(f, 'utf8').split('\n').forEach((line, i) => {
    const at = `${relative(root, f)}:${i + 1}`;
    for (const [, id] of line.matchAll(anyId)) if (!seen.has(id)) seen.set(id, at);
    for (const [, a, b] of line.matchAll(f.endsWith('.go') ? goName : jsName)) { const id = a || b; named.set(id, [...(named.get(id) || []), at]); }
  });
}

const dups = [...named].filter(([, at]) => at.length > 1);
const missing = [...seen].filter(([id]) => !named.has(id));
for (const [id, at] of dups) console.log(`duplicate ${id}: ${at.join(', ')}`);
for (const [id, at] of missing) console.log(`not in a test name ${id}: ${at}`);
console.log(`${named.size} IDs in ${files.length} files, ${dups.length} duplicates, ${missing.length} not in a test name`);
process.exit(dups.length || missing.length ? 1 : 0);
