// Stage 47.14.6 browser contract: the empty, error and permission states look
// different. Usage: BASE=http://localhost:8631 TOKEN=<admin token> node docs/qa/state-contract.cjs
// (NODE_PATH must reach playwright). Exits non-zero on any failed assertion.
const { chromium } = require('playwright');
const BASE = process.env.BASE || 'http://localhost:8631';
const TOKEN = process.env.TOKEN;
const fail = [];
const check = (ok, msg) => { if (!ok) fail.push(msg); console.log((ok ? 'ok   ' : 'FAIL ') + msg); };
(async () => {
  const b = await chromium.launch({ channel: 'chrome' });
  const p = await b.newPage({ viewport: { width: 1280, height: 800 } });
  const errs = []; p.on('pageerror', e => errs.push(e.message));
  await p.goto(BASE + '/');
  await p.evaluate(t => { localStorage.erp_token = t; localStorage.erp_role = 'Super Admin'; localStorage.erp_username = 'admin'; localStorage.erp_tenant_id = 'default'; }, TOKEN);
  await p.goto(BASE + '/?r=1#/');
  await p.waitForSelector('#view-root', { timeout: 20000 });
  await p.waitForTimeout(1500);
  const look = await p.evaluate(() => {
    const root = document.getElementById('view-root');
    const style = el => { const s = getComputedStyle(el); return { cls: el.className, border: s.borderTopStyle, borderColor: s.borderLeftColor, bg: s.backgroundColor, color: s.color }; };
    const box = () => { const x = document.createElement('div'); root.appendChild(x); return x; };
    const a = box(), c = box(), m = box(), out = {};
    // The real renderer for permission (403), error (500) and missing (404); a lone "No ..." row for empty.
    renderErrorPanel(a, 'Not allowed', null, 403);
    renderErrorPanel(c, 'Failed', () => {}, 500);
    renderErrorPanel(m, 'Missing', null, 404);
    out.denied = style(a.firstElementChild); out.error = style(c.firstElementChild); out.missing = style(m.firstElementChild);
    const tbl = document.createElement('table');
    tbl.innerHTML = '<thead><tr><th>A</th></tr></thead><tbody><tr><td colspan="1">No records yet</td></tr></tbody>';
    root.appendChild(tbl);
    return new Promise(r => setTimeout(() => { out.empty = style(tbl.querySelector('td')); r(out); }, 300));
  });
  check(look.denied.cls.includes('state-denied'), 'permission panel has state-denied');
  check(look.error.cls.includes('state-error'), 'error panel has state-error');
  check(look.missing.cls.includes('state-missing'), 'missing panel has state-missing');
  check(look.empty.cls.includes('state-empty'), 'a lone "No ..." row is tagged state-empty');
  check(look.empty.border === 'dashed', 'empty is dashed, got ' + look.empty.border);
  const sigs = ['denied', 'error', 'empty'].map(k => [look[k].borderColor, look[k].bg, look[k].color].join('|'));
  check(new Set(sigs).size === 3, 'denied / error / empty differ in border, background or text colour');
  check(look.denied.borderColor !== look.error.borderColor, 'denied and error accent edges differ');
  check(errs.length === 0, 'no page errors ' + JSON.stringify(errs));
  await b.close();
  process.exit(fail.length ? 1 : 0);
})();
