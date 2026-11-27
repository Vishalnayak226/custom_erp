'use strict';

// 47.9.4 browser performance budget gate. Local QA tooling only; it is never part of
// the ERP runtime and adds no dependency (Playwright is resolved like
// docs/guides/capture-screenshots.js does). It reads docs/qa/perf-budgets.json
// (profiles, views, budgets, ratchet), measures against a LOOPBACK scratch server
// only, and exits non-zero when a budget is missed.
//
//   node docs/qa/perf-budget.cjs --base http://127.0.0.1:8740 --tokens tokens.txt \
//        [--user manager1] [--role "Store Manager"] [--tenant default]
//        [--profiles desktop-cable,throttled-4x,phone-low] [--runs 3] [--probe-reps 30]
//        [--budgets docs/qa/perf-budgets.json] [--out result.json] [--channel msedge]
//        [--ratchet] [--no-server] [--record-gate <repo root>]
//
// tokens.txt: one bearer token per line, at least (runs x profiles) + 4 lines (each
// run uses its own token because the limiter is 60 calls/min per token; the server
// probe uses the rest). Mint them with `go run ./cmd/minttoken <user> "<role>"`.
//
// Per profile and run (fresh browser context, empty cache): COLD load (transfer bytes,
// requests, FCP, LCP, CLS, long tasks, DOM nodes), CACHED reload in the same context
// (bytes over the network, LCP), real interactions scored with Event Timing, cached
// view switches, then a sweep of views for DOM nodes, horizontal overflow and the own
// accessibility checks. HTTP >= 400, console errors and page errors are counted over
// the whole run (429 is the harness's pacing, counted apart, never an error).
//
// INP is the browser's Event Timing "interaction" value (the same quantity web-vitals
// reports as INP) taken over the scripted interactions of one run (the worst, as INP
// does for < 50 interactions); the p75 is taken over the runs. It is a lab proxy:
// real users do not click this script.
// Server time: after the browser phase, every distinct GET /api/ URL the pages called
// is replayed --probe-reps times from Node over loopback with no throttle; the figure
// is request start to last body byte, per endpoint (path with ids folded to :id). The
// server sends no Server-Timing header, so this is client-measured at loopback, where
// transport is ~0.2 ms: it is the server's time within that error.
// Accessibility: NOT axe-core (none is vendored). Own checks only: controls without an
// accessible name, form fields without a label, images without alt, duplicate ids,
// and text contrast on up to 400 text elements (flat backgrounds only; elements over
// gradients/images are skipped). Not covered: focus order, keyboard traps, ARIA role
// validity, heading structure, live regions, screen-reader output, reflow/zoom.
//
// Output contract (for the release script): stdout ends with one line
//   PERF-BUDGET RESULT: PASS|MISS|KNOWN|ERROR misses=<n> known=<n> out=<file>
// and the process exits 0 = every budget met (or, with --ratchet, every miss is a known
// miss at or under its ceiling), 1 = a budget missed / a metric unmeasured,
// 2 = the harness could not run. --out writes the full JSON: { verdict, misses[],
// known[], profiles{}, server{}, env{} }.
//
// --record-gate <repo root> writes the run as the release gate's `browser_budget`
// evidence (release/evidence/browser_budget.json, through `releasegate record`):
// PASS is pass, MISS or KNOWN is fail (a known miss is carried by a P2 exception that
// `covers` the kind, never by a pass), ERROR records nothing, and a run of fewer
// profiles than the budgets declare, or with --no-server, is not recorded because it
// does not stand for the candidate. The commit recorded is the repo root's HEAD.

const fs = require('node:fs');
const path = require('node:path');
const { spawnSync } = require('node:child_process');
const { resolvePlaywright } = require('../guides/capture-screenshots.js');

// gateEvidence maps a finished run to the release gate's evidence: { result, note }, or
// { skip } when the run cannot stand for the candidate. Pure, so a test can call it.
function gateEvidence(result, declaredProfiles, ranProfiles, withServer) {
  if (!withServer) return { skip: 'a run with --no-server is not release evidence (the server budgets were not measured)' };
  const missing = declaredProfiles.filter(p => !ranProfiles.includes(p));
  if (missing.length) return { skip: `a run without the declared profile(s) ${missing.join(', ')} is not release evidence` };
  const parts = [`perf-budget.cjs ${result.verdict}`, `${result.misses.length} miss(es)`, `${result.known.length} known`, `profiles ${ranProfiles.join('/')}`, `browser ${(result.env || {}).browser}`];
  const first = result.misses[0] || result.known[0];
  if (first) parts.push(`first: ${first.scope}.${first.key}=${first.value}`);
  return { result: result.verdict === 'PASS' ? 'pass' : 'fail', note: parts.join('; ') };
}

function recordGate(root, ev) {
  const r = spawnSync('go', ['run', './cmd/releasegate', 'record', '-root', '.', '-kind', 'browser_budget', '-result', ev.result, '-by', 'perf-budget.cjs', '-note', ev.note], { cwd: root, encoding: 'utf8' });
  return { code: r.status, out: ((r.stdout || '') + (r.stderr || '')).trim() };
}

const FLAGS = new Set(['ratchet', 'no-server']);
function parseArgs(argv) {
  const a = { user: 'manager1', role: 'Store Manager', tenant: 'default', 'probe-reps': '30', budgets: path.join(__dirname, 'perf-budgets.json') };
  for (let i = 0; i < argv.length; i++) {
    const k = argv[i].replace(/^--/, '');
    if (FLAGS.has(k)) a[k] = true; else a[k] = argv[++i];
  }
  return a;
}

const sleep = ms => new Promise(r => setTimeout(r, ms));
const pct = (xs, p) => { if (!xs.length) return null; const s = [...xs].sort((x, y) => x - y); return s[Math.min(s.length - 1, Math.ceil(p / 100 * s.length) - 1)]; };
const r1 = x => x == null ? null : Math.round(x * 10) / 10;
const r4 = x => x == null ? null : Math.round(x * 10000) / 10000;
const sum = xs => xs.reduce((a, b) => a + b, 0);
const API_PER_MIN = 46; // under the server's 60/min per-token bucket

// ----------------------------------------------------------------- in page
function pageSide({ token, user, role, tenant }) {
  try {
    localStorage.setItem('erp_token', token); localStorage.setItem('erp_username', user);
    localStorage.setItem('erp_role', role); localStorage.setItem('erp_tenant_id', tenant);
  } catch (e) { /* storage blocked */ }
  const S = window.__pb = { lcp: [], shifts: [], longtasks: [], events: [], sw: null, rootSeen: 0 };
  const desc = n => {
    if (!n) return '(removed)';
    if (n.nodeType !== 1) return n.nodeName;
    let s = n.tagName.toLowerCase();
    if (n.id) s += '#' + n.id;
    if (n.classList && n.classList.length) s += '.' + [...n.classList].slice(0, 3).join('.');
    const t = (n.textContent || '').trim().replace(/\s+/g, ' ').slice(0, 50);
    return s + (t ? ` "${t}"` : '');
  };
  const obs = (type, fn, extra) => { try { new PerformanceObserver(l => l.getEntries().forEach(fn)).observe(Object.assign({ type, buffered: true }, extra || {})); } catch (e) { /* unsupported */ } };
  obs('largest-contentful-paint', e => S.lcp.push({ t: e.startTime, size: e.size, el: desc(e.element) }));
  obs('layout-shift', e => S.shifts.push({ t: e.startTime, v: e.value, input: e.hadRecentInput }));
  obs('longtask', e => S.longtasks.push({ t: e.startTime, d: e.duration }));
  obs('event', e => { if (e.interactionId) S.events.push({ name: e.name, d: e.duration, id: e.interactionId, target: desc(e.target) }); }, { durationThreshold: 16 });
  // View switch: t0 right before the hash changes; "settled" = the frame after the last
  // #view-root mutation (the harness decides when it has been quiet long enough).
  const afterPaint = cb => requestAnimationFrame(() => setTimeout(() => cb(performance.now()), 0));
  const watch = () => {
    const root = document.getElementById('view-root');
    if (!root) { setTimeout(watch, 10); return; }
    new MutationObserver(() => {
      const hasContent = root.children.length && !root.querySelector('.view-loading');
      if (hasContent && !S.rootSeen) afterPaint(t => { if (!S.rootSeen) S.rootSeen = t; });
      const sw = S.sw; if (!sw) return;
      sw.lastRaw = performance.now(); sw.mutations++;
      afterPaint(t => { sw.last = Math.max(sw.last, t); });
    }).observe(root, { childList: true, subtree: true, characterData: true });
  };
  if (document.readyState === 'loading') document.addEventListener('DOMContentLoaded', watch); else watch();
  window.__pbSwitch = hash => { S.sw = { hash, t0: performance.now(), last: 0, lastRaw: 0, mutations: 0 }; location.hash = hash; };
}

// Own accessibility checks (see header for what they do not cover).
function a11yScan() {
  const vis = el => { const r = el.getBoundingClientRect(), s = getComputedStyle(el); return r.width > 0 && r.height > 0 && s.visibility !== 'hidden' && s.display !== 'none'; };
  const where = el => { let s = el.tagName.toLowerCase(); if (el.id) s += '#' + el.id; else if (el.classList.length) s += '.' + [...el.classList].slice(0, 2).join('.'); return s; };
  const byIds = (el, attr) => (el.getAttribute(attr) || '').split(/\s+/).map(i => document.getElementById(i)).filter(Boolean).map(e => (e.textContent || '').trim()).join(' ').trim();
  const hasName = el => !!((el.getAttribute('aria-label') || '').trim() || byIds(el, 'aria-labelledby') || (el.innerText || el.textContent || '').trim()
    || (el.getAttribute('title') || '').trim() || (el.value && /^(button|submit|reset)$/i.test(el.type || '') ? String(el.value).trim() : '')
    || [...el.querySelectorAll('img[alt]')].some(i => i.alt.trim()) || [...el.querySelectorAll('svg title')].some(t => t.textContent.trim()));
  const unnamed = [], unlabelled = [];
  for (const el of document.querySelectorAll('button, a[href], [role=button], [role=tab], [role=link], [role=menuitem], input[type=button], input[type=submit]')) {
    if (el.closest('[aria-hidden=true], [hidden]') || !vis(el)) continue;
    if (!hasName(el)) unnamed.push(where(el));
  }
  for (const el of document.querySelectorAll('input:not([type=hidden]):not([type=button]):not([type=submit]):not([type=reset]), select, textarea')) {
    if (el.closest('[aria-hidden=true], [hidden]') || !vis(el)) continue;
    const labelled = (el.id && document.querySelector(`label[for="${CSS.escape(el.id)}"]`)) || el.closest('label') || (el.getAttribute('aria-label') || '').trim() || byIds(el, 'aria-labelledby') || (el.getAttribute('title') || '').trim();
    if (!labelled) unlabelled.push(where(el));
  }
  const noAlt = [...document.querySelectorAll('img')].filter(i => vis(i) && !i.hasAttribute('alt')).map(where);
  const seen = new Set(), dup = new Set();
  for (const el of document.querySelectorAll('[id]')) { if (seen.has(el.id)) dup.add(el.id); seen.add(el.id); }
  // Contrast on flat backgrounds (WCAG AA: 4.5, large text 3).
  const cv = document.createElement('canvas'); cv.width = cv.height = 1;
  const ctx = cv.getContext('2d', { willReadFrequently: true });
  const rgba = c => { ctx.clearRect(0, 0, 1, 1); ctx.fillStyle = '#000'; ctx.fillStyle = c; ctx.fillRect(0, 0, 1, 1); const d = ctx.getImageData(0, 0, 1, 1).data; return [d[0], d[1], d[2], d[3] / 255]; };
  const lum = ([r, g, b]) => { const f = v => { v /= 255; return v <= 0.03928 ? v / 12.92 : Math.pow((v + 0.055) / 1.055, 2.4); }; return 0.2126 * f(r) + 0.7152 * f(g) + 0.0722 * f(b); };
  let checked = 0, skipped = 0; const low = [];
  for (const el of document.querySelectorAll('#view-root *, #app-sidebar *')) {
    if (checked >= 400) break;
    if (![...el.childNodes].some(n => n.nodeType === 3 && n.textContent.trim().length > 1)) continue;
    if (!vis(el) || el.closest('[disabled], [aria-disabled=true]')) continue;
    const st = getComputedStyle(el);
    let bg = null, flat = true;
    for (let p = el; p; p = p.parentElement) {
      const ps = getComputedStyle(p);
      if (ps.backgroundImage !== 'none') { flat = false; break; }
      const c = rgba(ps.backgroundColor);
      if (c[3] > 0.95) { bg = c; break; }
    }
    if (!flat) { skipped++; continue; }
    bg = bg || [255, 255, 255, 1];
    const f = rgba(st.color); const a = f[3] * (+st.opacity || 1);
    const fg = [0, 1, 2].map(i => f[i] * a + bg[i] * (1 - a));
    const L1 = lum(fg), L2 = lum(bg), ratio = (Math.max(L1, L2) + 0.05) / (Math.min(L1, L2) + 0.05);
    const px = parseFloat(st.fontSize), large = px >= 24 || (px >= 18.66 && +st.fontWeight >= 700);
    checked++;
    if (ratio < (large ? 3 : 4.5)) low.push(`${where(el)} ${ratio.toFixed(2)}`);
  }
  return { unnamed, unlabelled, noAlt, dupIds: [...dup], contrast: { checked, skipped, low } };
}

// ----------------------------------------------------------------- harness
class ApiTracker {
  constructor(page, sink) {
    this.pending = new Set(); this.times = []; this.n429 = 0;
    page.on('request', req => { const u = new URL(req.url()); if (u.pathname.startsWith('/api/')) { this.pending.add(req); this.times.push(Date.now()); if (req.method() === 'GET') sink.add(u.pathname + u.search); } });
    const done = req => this.pending.delete(req);
    page.on('requestfinished', done); page.on('requestfailed', done);
    page.on('response', res => { if (res.status() === 429) this.n429++; });
  }
  inLastMinute() { const cut = Date.now() - 60000; this.times = this.times.filter(t => t > cut); return this.times.length; }
  async pace(reserve = 6) { while (this.inLastMinute() + reserve > API_PER_MIN) await sleep(250); }
  async quiet(ms = 300, timeout = 15000) {
    const until = Date.now() + timeout; let last = Date.now();
    while (Date.now() < until) { if (this.pending.size) last = Date.now(); else if (Date.now() - last >= ms) return true; await sleep(25); }
    return false;
  }
}

async function openPage(context, profile, urls, errs) {
  const page = await context.newPage();
  page.setDefaultTimeout(30000);
  const cdp = await context.newCDPSession(page);
  await cdp.send('Network.enable');
  await cdp.send('Network.emulateNetworkConditions', { offline: false, latency: profile.latencyMs, downloadThroughput: profile.downKbps * 125, uploadThroughput: profile.upKbps * 125 });
  await cdp.send('Emulation.setCPUThrottlingRate', { rate: profile.cpu });
  page.on('pageerror', e => errs.page.push(e.message.split('\n')[0]));
  page.on('console', m => { if (m.type() === 'error' && !/Failed to load resource.*(4\d\d|5\d\d)/.test(m.text())) errs.console.push(m.text().slice(0, 160)); });
  page.on('response', res => {
    const s = res.status();
    if (s === 429) return;
    if (s >= 400) errs.http.push(`${s} ${res.request().method()} ${new URL(res.url()).pathname}`);
  });
  page.on('requestfailed', r => { const why = r.failure() && r.failure().errorText; if (!/ERR_ABORTED/.test(why || '')) errs.failed.push(`${r.method()} ${new URL(r.url()).pathname}: ${why}`); });
  const api = new ApiTracker(page, urls);
  return { page, cdp, api };
}

async function viewReady(page, api, quietMs = 500) {
  await page.waitForFunction(() => { const r = document.getElementById('view-root'); return r && r.children.length && !r.querySelector('.view-loading'); }, null, { timeout: 30000 });
  for (let i = 0; i < 80; i++) {
    await api.quiet(300);
    const idle = await page.evaluate(q => { const sw = window.__pb.sw; return !sw || performance.now() - sw.lastRaw >= q; }, quietMs);
    if (idle && !api.pending.size) return;
    await sleep(100);
  }
}

async function switchTo(page, api, hash) {
  await api.pace();
  await page.evaluate(h => window.__pbSwitch(h), hash);
  await viewReady(page, api);
  await sleep(120);
  return page.evaluate(() => { const sw = window.__pb.sw; window.__pb.sw = null; return { hash: sw.hash, settled: sw.last ? sw.last - sw.t0 : null }; });
}

const clsOf = shifts => { // session window: gap < 1 s, window <= 5 s
  let best = 0, cur = 0, start = 0, prev = -Infinity;
  for (const s of shifts.filter(x => !x.input).sort((a, b) => a.t - b.t)) {
    if (s.t - prev > 1000 || s.t - start > 5000) { cur = 0; start = s.t; }
    cur += s.v; prev = s.t; best = Math.max(best, cur);
  }
  return best;
};

const loadMetrics = page => page.evaluate(() => {
  const S = window.__pb, nav = performance.getEntriesByType('navigation')[0];
  const res = performance.getEntriesByType('resource');
  const bytes = (nav ? nav.transferSize : 0) + res.reduce((a, r) => a + (r.transferSize || 0), 0);
  const fcp = performance.getEntriesByName('first-contentful-paint')[0];
  const lcpAll = S.lcp.map(e => ({ t: e.t, el: e.el }));
  const lcp = lcpAll.at(-1) || null;
  // Non-production servers show a SIMULATED-environment strip that can be the LCP; production does not.
  const lcpProd = lcpAll.filter(e => !/env(ironment)?-banner/.test(e.el)).at(-1) || null;
  return {
    bytes, requests: res.length + 1, fromCache: res.filter(r => r.transferSize === 0 && r.decodedBodySize > 0).length,
    fcp: fcp ? fcp.startTime : null, lcp: lcp && lcp.t, lcpEl: lcp && lcp.el, lcpProd: lcpProd && lcpProd.t, lcpProdEl: lcpProd && lcpProd.el,
    cls: S.shifts, longTasks: S.longtasks.map(t => t.d), dom: document.getElementsByTagName('*').length,
  };
});

async function oneRun(browser, profile, token, args, cfg) {
  const ctxOpts = { viewport: profile.viewport, deviceScaleFactor: profile.mobile ? 2 : 1, locale: 'en-IN', colorScheme: 'light', ...(profile.mobile ? { isMobile: true, hasTouch: true } : {}) };
  const context = await browser.newContext(ctxOpts);
  await context.addInitScript(pageSide, { token, user: args.user, role: args.role, tenant: args.tenant });
  const errs = { http: [], console: [], page: [], failed: [] };
  const urls = new Set();
  let n429 = 0;
  try {
    // COLD: empty cache.
    let { page, api, cdp } = await openPage(context, profile, urls, errs);
    await page.goto(args.base + '/?r=1#/view/home', { waitUntil: 'load' });
    await viewReady(page, api);
    await sleep(1500);
    const coldM = await loadMetrics(page);
    const cold = { bytes: coldM.bytes, requests: coldM.requests, fcp: coldM.fcp, lcp: coldM.lcp, lcpEl: coldM.lcpEl, lcpProd: coldM.lcpProd, lcpProdEl: coldM.lcpProdEl,
      cls: clsOf(coldM.cls), longTasks: { n: coldM.longTasks.length, totalMs: sum(coldM.longTasks), maxMs: Math.max(0, ...coldM.longTasks) }, dom: coldM.dom };
    n429 += api.n429;
    await page.close();
    // CACHED: same context (warm HTTP cache), a fresh page load.
    ({ page, api, cdp } = await openPage(context, profile, urls, errs));
    await page.goto(args.base + '/?r=1#/view/home', { waitUntil: 'load' });
    await viewReady(page, api);
    await sleep(1500);
    const warmM = await loadMetrics(page);
    const cached = { bytes: warmM.bytes, requests: warmM.requests, fromCache: warmM.fromCache, fcp: warmM.fcp, lcp: warmM.lcp, lcpProd: warmM.lcpProd, cls: clsOf(warmM.cls) };
    // Cached view switches: every view visited once (modules loaded), then measured rounds.
    const sw = cfg.views.switch;
    for (const h of sw.slice(1).concat(sw[0])) await switchTo(page, api, h);
    const switches = [];
    for (let r = 0; r < (+args.rounds || 4); r++) for (const h of sw.slice(1).concat(sw[0])) switches.push(await switchTo(page, api, h));
    // Real interactions for Event Timing: sidebar clicks, typing in the list and the top search, paging.
    const interactions = { done: [] };
    const step = async (label, act) => { await api.pace(8); try { await act(); interactions.done.push(label); } catch (e) { interactions.done.push(label + ' (skipped: ' + e.message.split('\n')[0].slice(0, 60) + ')'); } await sleep(450); await viewReady(page, api).catch(() => { }); };
    await switchTo(page, api, '#/setup/Item');
    const t = { timeout: 4000 };
    await step('list search', async () => { await page.locator('#doc-table-search').click(t); await page.keyboard.type('shirt', { delay: 140 }); await page.keyboard.press('Control+A'); await page.keyboard.press('Backspace'); });
    await step('list next', () => page.locator('#doc-table-next-btn:not([disabled])').click(t));
    await step('top search', async () => { await page.locator('#global-search').click(t); await page.keyboard.type('pur', { delay: 140 }); await page.keyboard.press('Escape'); await page.locator('#global-search').fill('', t); });
    const menu = await page.evaluate(() => [...document.querySelectorAll('#app-sidebar a.menu-item:not(.menu-item-group)')].filter(a => a.id && a.offsetParent !== null).slice(0, 6).map(a => a.id));
    for (const id of menu) await step('menu ' + id, () => page.locator('#' + id).click(t));
    const ev = await page.evaluate(() => window.__pb.events);
    const byId = new Map(); for (const e of ev) { const c = byId.get(e.id); if (!c || e.d > c.d) byId.set(e.id, e); }
    const worst = [...byId.values()].sort((a, b) => b.d - a.d);
    const inp = { value: worst.length ? worst[0].d : null, interactions: worst.length, worst: worst.slice(0, 3).map(e => `${e.name} ${e.target} ${Math.round(e.d)} ms`), done: interactions.done };
    // Sweep: DOM nodes, overflow and the own a11y checks per view.
    const sweep = [];
    for (const h of cfg.views.sweep) {
      await switchTo(page, api, h);
      await sleep(200);
      const v = await page.evaluate(() => ({ dom: document.getElementsByTagName('*').length, overflow: document.documentElement.scrollWidth > innerWidth + 2 ? document.documentElement.scrollWidth - innerWidth : 0 }));
      const a = await page.evaluate(a11yScan);
      sweep.push({ hash: h, dom: v.dom, overflowPx: v.overflow, unnamed: a.unnamed, unlabelled: a.unlabelled, noAlt: a.noAlt, dupIds: a.dupIds, contrast: a.contrast });
    }
    const tail = await page.evaluate(() => ({ shifts: window.__pb.shifts, longtasks: window.__pb.longtasks.map(x => x.d) }));
    // 297.7: the tab's size after the whole session - its JS heap, and every
    // node it still holds, shown or not - read after a garbage collection.
    await cdp.send('Performance.enable');
    await cdp.send('HeapProfiler.collectGarbage');
    const pm = Object.fromEntries((await cdp.send('Performance.getMetrics')).metrics.map(x => [x.name, x.value]));
    const memory = { heapMB: +(pm.JSHeapUsedSize / 1048576).toFixed(1), nodes: pm.Nodes, listeners: pm.JSEventListeners };
    n429 += api.n429;
    return {
      cold, cached, inp, memory,
      switches: switches.filter(s => s.settled != null).map(s => ({ hash: s.hash, ms: r1(s.settled) })),
      sweep, cls: clsOf(tail.shifts), longTasks: { n: tail.longtasks.length, totalMs: Math.round(sum(tail.longtasks)), maxMs: Math.round(Math.max(0, ...tail.longtasks)) },
      errors: { http: errs.http, console: errs.console, page: errs.page, failed: errs.failed }, rateLimited: n429, apiUrls: [...urls],
    };
  } finally { await context.close(); }
}

// ----------------------------------------------------------------- server probe
const endpointKey = p => p.replace(/\/[0-9a-f]{8}-[0-9a-f-]{27}/gi, '/:id').replace(/\/\d+(?=\/|$)/g, '/:id');
async function serverProbe(base, tokens, urls, reps, tenant) {
  const used = tokens.map(() => []); // per-token call times
  const samples = new Map(); let n429 = 0, errors = [];
  const grab = async () => { for (;;) { const now = Date.now(); for (let i = 0; i < tokens.length; i++) { used[i] = used[i].filter(t => now - t < 60000); if (used[i].length < 50) { used[i].push(now); return tokens[i]; } } await sleep(300); } };
  for (const u of urls) {
    for (let k = 0; k < reps; k++) {
      let ms = null;
      for (let attempt = 0; attempt < 4 && ms == null; attempt++) {
        const tok = await grab();
        const t0 = process.hrtime.bigint();
        const res = await fetch(base + u, { headers: { Authorization: 'Bearer ' + tok, 'X-Tenant-ID': tenant } });
        await res.arrayBuffer();
        const dt = Number(process.hrtime.bigint() - t0) / 1e6;
        if (res.status === 429) { n429++; await sleep(500); continue; }
        if (res.status >= 400) { errors.push(`${res.status} ${u}`); break; }
        ms = dt;
      }
      if (ms != null) { const key = endpointKey(u.split('?')[0]); if (!samples.has(key)) samples.set(key, []); samples.get(key).push(ms); }
    }
  }
  const endpoints = {};
  for (const [key, xs] of samples) endpoints[key] = { n: xs.length, p50: r1(pct(xs, 50)), p95: r1(pct(xs, 95)), p99: r1(pct(xs, 99)), max: r1(Math.max(...xs)) };
  return { reps, endpoints, rateLimited: n429, errors: [...new Set(errors)].slice(0, 20) };
}

// ----------------------------------------------------------------- scoring
function summarize(runs) {
  const per = f => runs.map(f);
  const allSw = runs.flatMap(r => r.switches.map(s => s.ms));
  const sweepMax = f => Math.max(0, ...runs.map(r => sum(r.sweep.map(f))));
  const lcpOf = k => per(r => r.cold[k]).filter(x => x != null);
  const inps = per(r => r.inp.value).filter(x => x != null);
  return {
    runs: runs.length,
    coldBytes: pct(per(r => r.cold.bytes), 50), cachedBytes: pct(per(r => r.cached.bytes), 50), coldRequests: pct(per(r => r.cold.requests), 50),
    lcpP75Ms: lcpOf('lcpProd').length ? Math.round(pct(lcpOf('lcpProd'), 75)) : null,
    lcpWithBannerP75Ms: lcpOf('lcp').length ? Math.round(pct(lcpOf('lcp'), 75)) : null,
    lcpElement: [...new Set(per(r => r.cold.lcpProdEl))].join(' | '),
    cachedLcpP75Ms: Math.round(pct(per(r => r.cached.lcpProd).filter(x => x != null), 75) || 0) || null,
    fcpP75Ms: Math.round(pct(per(r => r.cold.fcp).filter(x => x != null), 75) || 0) || null,
    inpP75Ms: inps.length ? Math.round(pct(inps, 75)) : null, inpInteractions: pct(per(r => r.inp.interactions), 50),
    switchP95Ms: allSw.length ? r1(pct(allSw, 95)) : null, switchP50Ms: r1(pct(allSw, 50)),
    clsMax: r4(Math.max(...per(r => Math.max(r.cold.cls, r.cached.cls, r.cls)))),
    longTasksColdMaxMs: Math.max(...per(r => r.cold.longTasks.maxMs)), longTasksColdTotalMs: Math.round(pct(per(r => r.cold.longTasks.totalMs), 50)),
    longTasksRunMaxMs: Math.max(...per(r => r.longTasks.maxMs)),
    domNodesMax: Math.max(...runs.flatMap(r => [r.cold.dom, ...r.sweep.map(s => s.dom)])),
    jsHeapMBMax: Math.max(...per(r => r.memory.heapMB)), tabNodesMax: Math.max(...per(r => r.memory.nodes)),
    httpErrors: Math.max(...per(r => r.errors.http.length + r.errors.failed.length)),
    consoleErrors: Math.max(...per(r => r.errors.console.length)),
    pageErrors: Math.max(...per(r => r.errors.page.length)),
    viewportOverflow: Math.max(...per(r => r.sweep.filter(s => s.overflowPx > 0).length)),
    unnamedControls: sweepMax(s => s.unnamed.length), unlabelledFields: sweepMax(s => s.unlabelled.length),
    imagesWithoutAlt: sweepMax(s => s.noAlt.length), duplicateIds: sweepMax(s => s.dupIds.length),
    lowContrast: sweepMax(s => s.contrast.low.length),
    rateLimited: sum(per(r => r.rateLimited)),
  };
}

function score(summaries, server, cfg, ratchetOn) {
  const misses = [], known = [], rows = [];
  const check = (scope, key, value, budget) => {
    if (budget == null) return;
    const ok = value != null && value <= budget;
    rows.push({ scope, key, value, budget, ok });
    if (ok) return;
    const ceiling = cfg.ratchet[`${scope}.${key}`];
    const entry = { scope, key, value, budget, ...(value == null ? { unmeasured: true } : {}), ...(ceiling != null ? { ceiling } : {}) };
    if (ratchetOn && ceiling != null && value != null && value <= ceiling) known.push(entry); else misses.push(entry);
  };
  for (const [name, s] of Object.entries(summaries)) {
    const b = { ...cfg.budgets.default, ...(cfg.budgets.overrides[name] || {}) };
    for (const k of Object.keys(b)) if (!/^server/.test(k)) check(name, k, s[k], b[k]);
  }
  if (server) {
    const b = cfg.budgets.default;
    for (const [ep, v] of Object.entries(server.endpoints)) {
      check('server', `${ep} p95`, v.p95, b.serverP95Ms);
      check('server', `${ep} p99`, v.p99, b.serverP99Ms);
    }
  }
  return { misses, known, rows };
}

// ----------------------------------------------------------------- main
async function main() {
  const args = parseArgs(process.argv.slice(2));
  if (!args.base || !args.tokens) throw new Error('--base <loopback origin> and --tokens <file> are required.');
  const o = new URL(args.base);
  if (o.protocol !== 'http:' || !['127.0.0.1', 'localhost'].includes(o.hostname) || o.pathname !== '/') throw new Error('Only a loopback scratch-server origin is allowed.');
  args.base = o.origin;
  const cfg = JSON.parse(fs.readFileSync(args.budgets, 'utf8'));
  const names = (args.profiles ? args.profiles.split(',') : Object.keys(cfg.profiles)).map(s => s.trim());
  for (const n of names) if (!cfg.profiles[n]) throw new Error(`Unknown profile ${n}.`);
  const runs = +(args.runs || cfg.runs || 3);
  const tokens = fs.readFileSync(args.tokens, 'utf8').replace(/^﻿/, '').split(/\r?\n/).map(s => s.trim()).filter(Boolean);
  const need = names.length * runs + (args['no-server'] ? 0 : 4);
  if (tokens.length < need) throw new Error(`Need ${need} token lines (one per run, 4 for the server probe); got ${tokens.length}.`);
  const pw = resolvePlaywright();
  const browser = await pw.chromium.launch(args.channel ? { channel: args.channel } : {});
  const result = { when: new Date().toISOString(), env: { browser: browser.version(), channel: args.channel || 'chromium', runs, user: args.user, role: args.role }, profiles: {}, server: null };
  const summaries = {}; const urlSet = new Set(); let ti = 0;
  try {
    for (const name of names) {
      const profile = cfg.profiles[name]; const rs = [];
      for (let i = 0; i < runs; i++) {
        process.stderr.write(`[perf] ${name} run ${i + 1}/${runs}\n`);
        const r = await oneRun(browser, profile, tokens[ti++], args, cfg);
        r.apiUrls.forEach(u => urlSet.add(u));
        delete r.apiUrls; rs.push(r);
        process.stderr.write(`[perf]   LCP ${Math.round(r.cold.lcpProd)} ms cold, bytes ${r.cold.bytes}/${r.cached.bytes}, INP ${r.inp.value && Math.round(r.inp.value)} ms, heap ${r.memory.heapMB} MB, nodes ${r.memory.nodes}, errors ${r.errors.http.length + r.errors.console.length + r.errors.page.length}\n`);
      }
      summaries[name] = summarize(rs);
      result.profiles[name] = { label: profile.label, summary: summaries[name], runs: rs };
    }
    if (!args['no-server']) {
      process.stderr.write(`[perf] server probe: ${urlSet.size} GET endpoints x ${args['probe-reps']}\n`);
      result.server = await serverProbe(args.base, tokens.slice(ti), [...urlSet].sort(), +args['probe-reps'], args.tenant);
    }
  } finally { await browser.close(); }
  const sc = score(summaries, result.server, cfg, !!args.ratchet);
  result.misses = sc.misses; result.known = sc.known; result.checks = sc.rows;
  result.verdict = sc.misses.length ? 'MISS' : sc.known.length ? 'KNOWN' : 'PASS';
  if (args.out) fs.writeFileSync(path.resolve(args.out), JSON.stringify(result, null, 2));
  // Human summary
  for (const [n, s] of Object.entries(summaries)) {
    console.log(`\n${n}: ${cfg.profiles[n].label}`);
    console.log(`  bytes cold ${s.coldBytes} / cached ${s.cachedBytes}; requests ${s.coldRequests}; LCP p75 ${s.lcpP75Ms} ms (with env banner ${s.lcpWithBannerP75Ms}); cached LCP p75 ${s.cachedLcpP75Ms}; FCP p75 ${s.fcpP75Ms}`);
    console.log(`  LCP element: ${s.lcpElement}`);
    console.log(`  INP p75 ${s.inpP75Ms} ms (${s.inpInteractions} interactions); view switch p50 ${s.switchP50Ms} / p95 ${s.switchP95Ms} ms; CLS max ${s.clsMax}`);
    console.log(`  long tasks: cold max ${s.longTasksColdMaxMs} ms, cold total ${s.longTasksColdTotalMs} ms, run max ${s.longTasksRunMaxMs} ms; DOM nodes max ${s.domNodesMax}; tab after the session ${s.jsHeapMBMax} MB heap, ${s.tabNodesMax} nodes`);
    console.log(`  errors: http ${s.httpErrors}, console ${s.consoleErrors}, page ${s.pageErrors}; overflowing views ${s.viewportOverflow}; unnamed controls ${s.unnamedControls}; unlabelled fields ${s.unlabelledFields}; img no alt ${s.imagesWithoutAlt}; duplicate ids ${s.duplicateIds}; low contrast ${s.lowContrast}; rate-limited ${s.rateLimited}`);
  }
  if (result.server) {
    const eps = Object.entries(result.server.endpoints).sort((a, b) => b[1].p95 - a[1].p95);
    console.log(`\nserver (loopback, ${result.server.reps} reps, ${eps.length} endpoints) slowest by p95:`);
    for (const [ep, v] of eps.slice(0, 8)) console.log(`  ${ep.padEnd(52)} p50 ${v.p50}  p95 ${v.p95}  p99 ${v.p99}  max ${v.max} ms`);
  }
  for (const m of sc.misses) console.log(`MISS  ${m.scope}.${m.key} = ${m.value == null ? 'unmeasured' : m.value} (budget ${m.budget}${m.ceiling != null ? ', ceiling ' + m.ceiling : ''})`);
  for (const m of sc.known) console.log(`KNOWN ${m.scope}.${m.key} = ${m.value} (budget ${m.budget}, ceiling ${m.ceiling})`);
  console.log(`PERF-BUDGET RESULT: ${result.verdict} misses=${sc.misses.length} known=${sc.known.length} out=${args.out || '-'}`);
  if (args['record-gate']) {
    const ge = gateEvidence(result, Object.keys(cfg.profiles), names, !args['no-server']);
    if (ge.skip) console.log(`gate evidence: not recorded - ${ge.skip}`);
    else {
      const rr = recordGate(path.resolve(args['record-gate']), ge);
      console.log(`gate evidence: ${rr.code === 0 ? ge.result : 'NOT RECORDED'} - ${rr.out}`);
      if (rr.code !== 0) return 2;
    }
  }
  return sc.misses.length ? 1 : 0;
}

if (require.main === module) {
  main().then(c => { process.exitCode = c; }).catch(e => { console.error(e.stack || e.message); console.log('PERF-BUDGET RESULT: ERROR misses=0 known=0 out=-'); process.exitCode = 2; });
}
module.exports = { gateEvidence, summarize, score, endpointKey, pct, parseArgs, main };
