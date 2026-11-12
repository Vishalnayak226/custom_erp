'use strict';

// 49.10.5 browser memory check. Local QA tooling only (no ERP dependency; Playwright is
// resolved like docs/guides/capture-screenshots.js does) against a LOOPBACK scratch server.
// A long session that switches between screens must not grow without bound: after each
// round through the same list of screens it forces a garbage collection and reads the
// page's JS heap, DOM nodes and event listeners (Chrome DevTools Protocol). The first
// rounds warm caches and lazy modules; the check compares the last round to the first
// settled one and fails when any figure keeps climbing past the allowance.
//
//   node docs/qa/browser-memory-growth.cjs --base http://127.0.0.1:8740 --tokens tokens.txt \
//        [--user manager1] [--role "Store Manager"] [--rounds 10] [--pause 2200]
//        [--heap-mb 8] [--nodes 400] [--listeners 600] [--out result.json]
//
// Exit 0 = flat within the allowances, 1 = growth past an allowance, 2 = could not run.
// stdout ends with one line: MEMORY-GROWTH RESULT: PASS|FAIL ...

const fs = require('node:fs');
const path = require('node:path');
const { resolvePlaywright } = require('../guides/capture-screenshots.js');

const VIEWS = ['home', 'purchase-orders', 'inventory', 'reports', 'pos', 'grn', 'crm', 'finance'];

function parseArgs(argv) {
  const a = { user: 'manager1', role: 'Store Manager', tenant: 'default', rounds: '10', pause: '2200', 'heap-mb': '8', nodes: '400', listeners: '600' };
  for (let i = 0; i < argv.length; i++) a[argv[i].replace(/^--/, '')] = argv[++i];
  return a;
}

async function sample(cdp) {
  await cdp.send('HeapProfiler.collectGarbage');
  const { metrics } = await cdp.send('Performance.getMetrics');
  const m = Object.fromEntries(metrics.map(x => [x.name, x.value]));
  return { heapMB: +(m.JSHeapUsedSize / 1048576).toFixed(2), nodes: m.Nodes, listeners: m.JSEventListeners, docs: m.Documents };
}

async function main() {
  const args = parseArgs(process.argv.slice(2));
  if (!/^https?:\/\/(127\.0\.0\.1|localhost|\[::1\])(:|\/|$)/.test(args.base || '')) { console.error('--base must be a loopback scratch server'); process.exit(2); }
  const tokens = fs.readFileSync(args.tokens, 'utf8').split('\n').map(s => s.trim()).filter(Boolean);
  if (!tokens.length) { console.error('no tokens'); process.exit(2); }
  const playwright = resolvePlaywright();
  const browser = await playwright.chromium.launch(args.channel ? { channel: args.channel } : {});
  const context = await browser.newContext({ viewport: { width: 1440, height: 900 } });
  await context.addInitScript(({ token, user, role, tenant }) => {
    try { localStorage.setItem('erp_token', token); localStorage.setItem('erp_username', user); localStorage.setItem('erp_role', role); localStorage.setItem('erp_tenant_id', tenant); } catch (e) { /* blocked */ }
  }, { token: tokens[0], user: args.user, role: args.role, tenant: args.tenant });
  const page = await context.newPage();
  const pageErrors = [];
  let n429 = 0;
  page.on('pageerror', e => pageErrors.push(String(e).slice(0, 200)));
  page.on('response', r => { if (r.status() === 429) n429++; });
  const cdp = await context.newCDPSession(page);
  await cdp.send('Performance.enable');
  await page.goto(`${args.base}/?r=1#/view/home`, { waitUntil: 'load' });
  await page.waitForTimeout(2500);

  const rounds = Number(args.rounds), pause = Number(args.pause), series = [];
  for (let r = 1; r <= rounds; r++) {
    for (const v of VIEWS) {
      await page.evaluate(h => { location.hash = h; }, `#/view/${v}`);
      await page.waitForTimeout(pause);
    }
    const s = await sample(cdp);
    series.push(s);
    console.log(`[memory] round ${r}/${rounds}: heap ${s.heapMB} MB, nodes ${s.nodes}, listeners ${s.listeners}`);
  }
  await browser.close();

  // Round 3 is the baseline (lazy modules and caches are loaded by then).
  const base = series[Math.min(2, series.length - 1)], last = series[series.length - 1];
  const grew = { heapMB: +(last.heapMB - base.heapMB).toFixed(2), nodes: last.nodes - base.nodes, listeners: last.listeners - base.listeners };
  const allow = { heapMB: Number(args['heap-mb']), nodes: Number(args.nodes), listeners: Number(args.listeners) };
  const failed = Object.keys(allow).filter(k => grew[k] > allow[k]);
  const verdict = failed.length ? 'FAIL' : 'PASS';
  const result = { verdict, rounds, views: VIEWS, series, baseline: base, last, grew, allow, failed, pageErrors, rate429: n429 };
  if (args.out) fs.writeFileSync(args.out, JSON.stringify(result, null, 2));
  console.log(`MEMORY-GROWTH RESULT: ${verdict} heapGrowth=${grew.heapMB}MB nodes+${grew.nodes} listeners+${grew.listeners} pageErrors=${pageErrors.length} 429s=${n429}${failed.length ? ' over=' + failed.join(',') : ''}`);
  process.exit(failed.length ? 1 : 0);
}
main().catch(e => { console.error(e); process.exit(2); });
