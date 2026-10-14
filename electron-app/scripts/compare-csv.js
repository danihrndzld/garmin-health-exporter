#!/usr/bin/env node
// Compare two csv_* folders cell by cell. Prints added/removed files and columns,
// and every shared column whose values differ. Exit 1 when anything differs.
// usage: node compare-csv.js <oldDir> <newDir> [--json]
const fs = require('fs');
const path = require('path');

function parseCsv(text) {
  const rows = [];
  let row = [], cell = '', q = false;
  for (let i = 0; i < text.length; i++) {
    const c = text[i];
    if (q) {
      if (c === '"' && text[i + 1] === '"') { cell += '"'; i++; } else if (c === '"') q = false; else cell += c;
    } else if (c === '"') q = true;
    else if (c === ',') { row.push(cell); cell = ''; }
    else if (c === '\n' || c === '\r') { if (c === '\r' && text[i + 1] === '\n') i++; row.push(cell); rows.push(row); row = []; cell = ''; }
    else cell += c;
  }
  if (cell || row.length) { row.push(cell); rows.push(row); }
  return rows.filter((r) => r.length > 1 || r[0] !== '');
}

function load(file) {
  const [header = [], ...rows] = parseCsv(fs.readFileSync(file, 'utf8').replace(/^﻿/, ''));
  return { header, rows };
}

function compareDirs(oldDir, newDir) {
  const list = (d) => fs.readdirSync(d).filter((f) => f.endsWith('.csv')).sort();
  const a = list(oldDir), b = list(newDir);
  const out = { addedFiles: b.filter((f) => !a.includes(f)), removedFiles: a.filter((f) => !b.includes(f)), files: {} };
  for (const f of a.filter((x) => b.includes(x))) {
    const o = load(path.join(oldDir, f)), n = load(path.join(newDir, f));
    const res = {
      addedColumns: n.header.filter((h) => !o.header.includes(h)),
      removedColumns: o.header.filter((h) => !n.header.includes(h)),
      rows: [o.rows.length, n.rows.length],
      changedColumns: {},
    };
    for (const col of o.header.filter((h) => n.header.includes(h))) {
      const oi = o.header.indexOf(col), ni = n.header.indexOf(col);
      let changed = 0, example = null;
      for (let r = 0; r < Math.max(o.rows.length, n.rows.length); r++) {
        const ov = o.rows[r]?.[oi], nv = n.rows[r]?.[ni];
        if (ov !== nv) { changed++; example ||= { row: r + 1, old: ov, new: nv }; }
      }
      if (changed) res.changedColumns[col] = { changed, example };
    }
    if (res.addedColumns.length || res.removedColumns.length || res.rows[0] !== res.rows[1] || Object.keys(res.changedColumns).length) out.files[f] = res;
  }
  return out;
}

module.exports = { compareDirs, parseCsv };

if (require.main === module) {
  const [oldDir, newDir, flag] = process.argv.slice(2);
  const res = compareDirs(oldDir, newDir);
  if (flag === '--json') console.log(JSON.stringify(res, null, 2));
  else {
    for (const f of res.addedFiles) console.log(`+ file ${f}`);
    for (const f of res.removedFiles) console.log(`- file ${f}`);
    for (const [f, r] of Object.entries(res.files)) {
      console.log(`~ ${f} (rows ${r.rows.join(' -> ')})`);
      for (const c of r.addedColumns) console.log(`    + ${c}`);
      for (const c of r.removedColumns) console.log(`    - ${c}`);
      for (const [c, d] of Object.entries(r.changedColumns)) console.log(`    ~ ${c}: ${d.changed} cells, e.g. row ${d.example.row}: ${JSON.stringify(d.example.old)} -> ${JSON.stringify(d.example.new)}`);
    }
  }
  const same = !res.addedFiles.length && !res.removedFiles.length && !Object.keys(res.files).length;
  if (same) console.log('identical');
  process.exit(same ? 0 : 1);
}
