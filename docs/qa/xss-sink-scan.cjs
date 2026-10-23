#!/usr/bin/env node
// Stage 134.8 (47.8.1): the HTML-sink inventory, reproducible. Lists every
// ${...} placed inside an HTML template literal in public/*.js and classifies
// it mechanically; what it cannot prove safe is a candidate for a person to
// judge. Run: node docs/qa/xss-sink-scan.cjs [--json out.json] [--candidates]
//
// Classes:
//   escaped     - wrapped in an escaping helper (escapeHTMLText, esc, cfgEsc,
//                 encodeURIComponent, ...)
//   number      - a count, sum, toFixed/toLocaleString, Number(...), Math.*
//   literal     - a ternary or || whose every branch is a plain literal
//   markup      - a nested template or a helper that returns escaped markup
//                 (classified where it is built)
//   candidate   - none of the above: a person decides
'use strict';
const fs = require('fs');
const path = require('path');

const root = path.join(__dirname, '..', '..', 'public');
// Any helper named *esc* / *Escape* (crmEsc, hrEsc, cfgEsc, escapeHTMLText ...)
// plus actionAttrs, which escapes the action arguments it writes.
const ESCAPERS = /^(?:window\.)?(?:\w*(?:[Ee]sc|[Ee]scape)\w*|encodeURIComponent|actionAttrs|safeText)\s*\(/;
// Formatters whose output is digits, a date or a fixed unit.
const FORMATTERS = /^(?:window\.)?(?:\w*[Mm]oney|\w*[Aa]mount|\w*[Qq]ty|num|fmt\w*|format\w*|\w*[Dd]ate\w*|pct|percent\w*|inr|rupees?)\s*\(/;
// Helpers that return markup: judged where they are built.
const MARKUP_HELPERS = /^(?:window\.)?\w*(?:Field|Select|Button|Btn|HTML|Html|Cell|Row|Rows|Tile|Section|Badge|Chip|Pill|Picker\w*|Fact|Table|Card|Icon|Options|Input|Link|Hint|Help)\s*\(/;
const NUMBER = /(?:\.length\b|\.toFixed\(|\.toLocaleString\(|^Number\(|^parseInt\(|^parseFloat\(|^Math\.|^\d+(?:\.\d+)?$|^-?\d|\bcount\b|Count\b|^\w+\s*[-+*/%]\s*\w+$|\bidx\b|\bindex\b|^i$|^j$|^n$)/;
const LITERAL_BRANCH = /^\s*(?:'[^']*'|"[^"]*"|`[^`$]*`|\d+|true|false|null|undefined)\s*$/;

function splitTopLevel(expr, sep) {
  const parts = []; let depth = 0, quote = null, cur = '';
  for (let i = 0; i < expr.length; i++) {
    const c = expr[i];
    if (quote) { cur += c; if (c === '\\') { cur += expr[++i] || ''; continue; } if (c === quote) quote = null; continue; }
    if (c === "'" || c === '"' || c === '`') { quote = c; cur += c; continue; }
    if ('([{'.includes(c)) depth++;
    if (')]}'.includes(c)) depth--;
    if (depth === 0 && expr.startsWith(sep, i)) { parts.push(cur); cur = ''; i += sep.length - 1; continue; }
    cur += c;
  }
  parts.push(cur);
  return parts;
}

function classify(expr) {
  const e = expr.trim();
  if (ESCAPERS.test(e)) return 'escaped';
  if (FORMATTERS.test(e)) return 'number';
  if (MARKUP_HELPERS.test(e) || /^(?:kv|fact|tile|section|field|cell|row|opt|h)\s*\(/.test(e)) return 'markup';
  if (e.includes('`')) return 'markup';
  if (/^[\w.]+\.(?:map|join)\(/.test(e) || /\)\.join\(/.test(e)) return 'markup';
  if (/^(?:icon|svgIcon|badgeHTML|statusBadge|copyableCell|\w+HTML|\w+Html|render\w*|smallBtn|\w+Btn)\b/.test(e)) return 'markup';
  if (NUMBER.test(e)) return 'number';
  const q = splitTopLevel(e, '?');
  if (q.length === 2) {
    const branches = splitTopLevel(q[1], ':');
    if (branches.length >= 2 && branches.every(b => LITERAL_BRANCH.test(b) || ESCAPERS.test(b.trim()) || b.trim().includes('`'))) return 'literal';
  }
  const or = splitTopLevel(e, '||');
  if (or.length > 1 && or.every(b => LITERAL_BRANCH.test(b) || ESCAPERS.test(b.trim()))) return 'literal';
  return 'candidate';
}

// Template literals whose text holds an HTML tag; their ${} expressions.
function scanFile(file) {
  const src = fs.readFileSync(file, 'utf8');
  const out = [];
  let i = 0;
  const lineAt = idx => src.slice(0, idx).split('\n').length;
  function readTemplate(start) { // start at the backtick; returns end index (after closing backtick)
    let j = start + 1; const pieces = []; let text = '';
    while (j < src.length) {
      const c = src[j];
      if (c === '\\') { text += src.slice(j, j + 2); j += 2; continue; }
      if (c === '`') { return { end: j + 1, pieces, text }; }
      if (c === '$' && src[j + 1] === '{') {
        let depth = 1, k = j + 2, quote = null;
        while (k < src.length && depth > 0) {
          const d = src[k];
          if (quote) { if (d === '\\') { k += 2; continue; } if (d === quote) quote = null; else if (quote === '`' && d === '$' && src[k + 1] === '{') { const inner = readTemplateFrom(k); k = inner; continue; } k++; continue; }
          if (d === "'" || d === '"') { quote = d; k++; continue; }
          if (d === '`') { const inner = readTemplate(k); k = inner.end; continue; }
          if (d === '{') depth++;
          if (d === '}') depth--;
          k++;
        }
        pieces.push({ expr: src.slice(j + 2, k - 1), at: j });
        text += '\u0000';
        j = k; continue;
      }
      text += c; j++;
    }
    return { end: j, pieces, text };
  }
  function readTemplateFrom(k) { let depth = 1; k += 2; while (k < src.length && depth > 0) { if (src[k] === '{') depth++; else if (src[k] === '}') depth--; k++; } return k; }
  while (i < src.length) {
    const c = src[i];
    if (c === '/' && src[i + 1] === '/') { const nl = src.indexOf('\n', i); i = nl < 0 ? src.length : nl; continue; }
    if (c === '/' && src[i + 1] === '*') { const e = src.indexOf('*/', i + 2); i = e < 0 ? src.length : e + 2; continue; }
    if (c === "'" || c === '"') { let k = i + 1; while (k < src.length && src[k] !== c) { if (src[k] === '\\') k++; k++; } i = k + 1; continue; }
    if (c === '`') {
      const t = readTemplate(i);
      if (/<[a-zA-Z][^>]*>|<\/[a-zA-Z]/.test(t.text) || /<[a-zA-Z]/.test(t.text) && t.pieces.length) {
        for (const p of t.pieces) {
          // An attribute value or text content; skip ${} inside style/class names built from literals.
          out.push({ file: path.basename(file), line: lineAt(p.at), expr: p.expr.replace(/\s+/g, ' ').slice(0, 160), class: classify(p.expr) });
        }
      }
      i = t.end; continue;
    }
    i++;
  }
  return out;
}

const files = fs.readdirSync(root).filter(f => f.endsWith('.js')).map(f => path.join(root, f));
const all = files.flatMap(scanFile);
const counts = all.reduce((m, r) => { m[r.class] = (m[r.class] || 0) + 1; return m; }, {});
const perFile = {};
for (const r of all) { perFile[r.file] = perFile[r.file] || { total: 0, candidate: 0 }; perFile[r.file].total++; if (r.class === 'candidate') perFile[r.file].candidate++; }
const args = process.argv.slice(2);
const jsonAt = args.indexOf('--json');
if (jsonAt >= 0) fs.writeFileSync(args[jsonAt + 1], JSON.stringify({ generated: new Date().toISOString().slice(0, 10), total: all.length, counts, perFile, candidates: all.filter(r => r.class === 'candidate') }, null, 1));
if (args.includes('--candidates')) for (const r of all.filter(r => r.class === 'candidate')) console.log(`${r.file}:${r.line}\t${r.expr}`);
else console.log(JSON.stringify({ total: all.length, counts }, null, 1));
