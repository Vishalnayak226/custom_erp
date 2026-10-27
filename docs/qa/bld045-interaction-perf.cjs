'use strict';

// BLD-045 browser interaction performance harness. Local QA tooling only;
// never bundled into the ERP runtime. Measures with the browser's own
// observers (LCP, layout-shift, Event Timing, longtask, long-animation-frame),
// not with script sleeps:
//
//   - cold boot to the signed-in Home view: LCP (and which element), what the
//     main thread did before it (long tasks, long animation frames with
//     script attribution, resources), boot CLS;
//   - cached view switches between common screens (each visited once first);
//   - real interactions (search typing on the 1,000-Item list, clicks, Tab)
//     for candidate INP;
//   - an optional long session (N paced view switches) sampling heap after a
//     forced GC, DOM nodes, listeners and long tasks per segment.
//
// Profile: 1365x768, CPU x4 (CDP Emulation.setCPUThrottlingRate), 40 ms RTT /
// 5 Mbps (CDP Network.emulateNetworkConditions), one fresh browser context per
// run so every cold boot starts with an empty cache.
//
// Run (Playwright is resolved like docs/guides/capture-screenshots.js does):
//   node docs/qa/bld045-interaction-perf.cjs --base http://127.0.0.1:8345 \
//     --tokens <file: one bearer token per line> [--user manager1]
//     [--role "Store Manager"] [--tenant default] [--contexts 3] [--rounds 5]
//     [--long 48] [--cpu 4] [--channel chrome] [--out <evidence.json>]
// Calibration note (2026-10-26, i7-8665U laptop): CDP's "x4" slowed plain JS
// ~3.5x but DOM/layout/font work ~8x on this machine, so compare runs only
// against runs from the same machine and profile; --cpu 1 gives the
// unthrottled reference.
// Each context (and the long session) uses its own token line, because the
// server's request limiter is keyed per token (60/min); the harness also paces
// itself under that limit and reports any 429 it still sees, so a run that
// touched the limiter is visible rather than silently scored.

const fs = require('node:fs');
const path = require('node:path');
const { resolvePlaywright } = require('../guides/capture-screenshots.js');

function parseArgs(argv) {
  const args = { contexts: 3, rounds: 5, long: 0, cpu: 4, channel: '', user: 'manager1', role: 'Store Manager', tenant: 'default' };
  for (let i = 0; i < argv.length; i++) {
    const key = argv[i].replace(/^--/, '');
    args[key] = argv[i + 1];
    i++;
  }
  for (const k of ['contexts', 'rounds', 'long', 'cpu']) args[k] = Number(args[k]);
  return args;
}

const args = parseArgs(process.argv.slice(2));
if (!args.base || !args.tokens) throw new Error('--base <loopback origin> and --tokens <file> are required.');
const origin = new URL(args.base);
if (origin.protocol !== 'http:' || !['127.0.0.1', 'localhost'].includes(origin.hostname) || origin.pathname !== '/') {
  throw new Error('Only a loopback scratch-server origin is allowed.');
}
const BASE = origin.origin;
const tokens = fs.readFileSync(args.tokens, 'utf8').replace(/^﻿/, '').split(/\r?\n/).map(s => s.trim()).filter(Boolean);
const needed = args.contexts + (args.long > 0 ? 1 : 0);
if (tokens.length < needed) throw new Error(`Need ${needed} token lines (one per context${args.long ? ' plus one for the long session' : ''}); got ${tokens.length}.`);

const PROFILE = { viewport: { width: 1365, height: 768 }, cpu: args.cpu, latencyMs: 40, bytesPerSec: 5e6 / 8 };
const BUDGET = { lcp: 1500, inp: 200, switchP95: 250, cls: 0.1 };
const SWITCH_VIEWS = ['#/view/home', '#/setup/Item', '#/view/purchase-orders', '#/view/reports'];
const LONG_VIEWS = ['#/view/home', '#/setup/Item', '#/view/purchase-orders', '#/view/reports', '#/view/inventory', '#/view/pos'];
const API_PER_MIN = 46; // stay well under the server's 60/min per-token bucket

// ---------------------------------------------------------------- in page
function instrument({ token, user, role, tenant }) {
  try {
    localStorage.setItem('erp_token', token);
    localStorage.setItem('erp_username', user);
    localStorage.setItem('erp_role', role);
    localStorage.setItem('erp_tenant_id', tenant);
  } catch (e) { /* storage blocked */ }
  const S = window.__b45 = { lcp: [], shifts: [], longtasks: [], loaf: [], events: [], unsupported: [], sw: null, rootSeen: 0 };
  const desc = n => {
    if (!n) return null;
    if (n.nodeType !== 1) return n.nodeName;
    let s = n.tagName.toLowerCase();
    if (n.id) s += '#' + n.id;
    if (n.classList && n.classList.length) s += '.' + [...n.classList].slice(0, 3).join('.');
    return s;
  };
  const where = n => {
    const parts = [];
    for (let x = n; x && x.nodeType === 1 && parts.length < 5; x = x.parentElement) parts.unshift(desc(x));
    const text = n && n.textContent ? n.textContent.trim().replace(/\s+/g, ' ').slice(0, 70) : '';
    return parts.join(' > ') + (text ? ` "${text}"` : '');
  };
  const rect = r => r ? [Math.round(r.x), Math.round(r.y), Math.round(r.width), Math.round(r.height)] : null;
  const obs = (type, fn, extra) => {
    try { new PerformanceObserver(list => list.getEntries().forEach(fn)).observe(Object.assign({ type, buffered: true }, extra || {})); }
    catch (e) { S.unsupported.push(type); }
  };
  obs('largest-contentful-paint', e => S.lcp.push({ t: e.startTime, render: e.renderTime, load: e.loadTime, size: e.size, url: e.url, el: e.element ? where(e.element) : '(removed)' }));
  obs('layout-shift', e => S.shifts.push({ t: e.startTime, v: e.value, input: e.hadRecentInput, sources: (e.sources || []).map(s => ({ node: s.node ? where(s.node).slice(0, 160) : null, prev: rect(s.previousRect), cur: rect(s.currentRect) })) }));
  obs('longtask', e => S.longtasks.push({ t: e.startTime, d: e.duration }));
  obs('long-animation-frame', e => S.loaf.push({
    t: e.startTime, d: e.duration, block: e.blockingDuration, render: e.renderStart, layout: e.styleAndLayoutStart,
    scripts: (e.scripts || []).map(s => ({ d: Math.round(s.duration), forced: Math.round(s.forcedStyleAndLayoutDuration || 0), inv: String(s.invoker).slice(0, 80), type: s.invokerType, src: (s.sourceURL || '').replace(location.origin, ''), fn: s.sourceFunctionName, at: s.sourceCharPosition })),
  }));
  obs('event', e => { if (e.interactionId) S.events.push({ name: e.name, t: e.startTime, d: e.duration, id: e.interactionId, ps: e.processingStart, pe: e.processingEnd, target: desc(e.target) }); }, { durationThreshold: 16 });

  // Switch timing: t0 is taken right before the hash changes; "first" is the
  // frame after the new screen's content replaces the Loading placeholder;
  // "settled" is the frame after the last #view-root mutation the switch
  // caused (the harness decides when it has been quiet long enough).
  const afterPaint = cb => requestAnimationFrame(() => setTimeout(() => cb(performance.now()), 0));
  const watch = () => {
    const root = document.getElementById('view-root');
    if (!root) { setTimeout(watch, 10); return; }
    new MutationObserver(() => {
      const sw = S.sw;
      const hasContent = root.children.length && !root.querySelector('.view-loading');
      if (hasContent && !S.rootSeen) afterPaint(t => { if (!S.rootSeen) S.rootSeen = t; });
      if (!sw) return;
      sw.lastRaw = performance.now();
      sw.mutations++;
      afterPaint(t => { sw.last = Math.max(sw.last, t); });
      if (hasContent && !sw.firstRaw) { sw.firstRaw = performance.now(); afterPaint(t => { if (!sw.first) sw.first = t; }); }
    }).observe(root, { childList: true, subtree: true, characterData: true });
  };
  if (document.readyState === 'loading') document.addEventListener('DOMContentLoaded', watch); else watch();
  window.__b45switch = hash => {
    S.sw = { hash, t0: performance.now(), first: 0, firstRaw: 0, last: 0, lastRaw: 0, mutations: 0 };
    location.hash = hash;
  };
}

// ---------------------------------------------------------------- helpers
const sleep = ms => new Promise(r => setTimeout(r, ms));
const pct = (xs, p) => { if (!xs.length) return null; const s = [...xs].sort((a, b) => a - b); return s[Math.min(s.length - 1, Math.ceil(p / 100 * s.length) - 1)]; };
const r1 = x => x == null ? null : Math.round(x * 10) / 10;
const r4 = x => x == null ? null : Math.round(x * 10000) / 10000;

function clsOf(shifts) { // max session window: gap < 1 s, window <= 5 s (web-vitals definition)
  let best = 0, cur = 0, start = 0, prev = -Infinity, bestWin = [];
  let win = [];
  for (const s of shifts.filter(x => !x.input).sort((a, b) => a.t - b.t)) {
    if (s.t - prev > 1000 || s.t - start > 5000) { cur = 0; start = s.t; win = []; }
    cur += s.v; prev = s.t; win.push(s);
    if (cur > best) { best = cur; bestWin = [...win]; }
  }
  return { value: best, window: bestWin };
}

function inpOf(events) {
  const byId = new Map();
  for (const e of events) {
    const cur = byId.get(e.id);
    if (!cur || e.d > cur.d) byId.set(e.id, e);
  }
  const worst = [...byId.values()].sort((a, b) => b.d - a.d);
  // fewer than 50 interactions: INP is the worst one
  const pick = worst.length >= 50 ? worst[Math.floor(worst.length / 50)] : worst[0];
  return { value: pick ? pick.d : 0, interactions: worst.length, worst: worst.slice(0, 5) };
}

class ApiTracker {
  constructor(page) {
    this.pending = new Set(); this.times = []; this.status429 = 0; this.errors = [];
    page.on('request', req => { if (new URL(req.url()).pathname.startsWith('/api/')) { this.pending.add(req); this.times.push(Date.now()); } });
    const done = req => this.pending.delete(req);
    page.on('requestfinished', done);
    page.on('requestfailed', done);
    page.on('response', res => {
      if (res.status() === 429) this.status429++;
      else if (res.status() >= 500) this.errors.push(`${res.status()} ${new URL(res.url()).pathname}`);
    });
  }
  inLastMinute() { const cut = Date.now() - 60000; this.times = this.times.filter(t => t > cut); return this.times.length; }
  async pace(reserve = 6) { // wait until the next action cannot push the bucket past API_PER_MIN
    while (this.inLastMinute() + reserve > API_PER_MIN) await sleep(250);
  }
  async quiet(ms = 300, timeout = 15000) {
    const until = Date.now() + timeout; let last = Date.now();
    while (Date.now() < until) {
      if (this.pending.size) last = Date.now();
      else if (Date.now() - last >= ms) return true;
      await sleep(25);
    }
    return false;
  }
}

async function newProfiledContext(browser, token) {
  const context = await browser.newContext({ viewport: PROFILE.viewport, deviceScaleFactor: 1, locale: 'en-IN', colorScheme: 'light' });
  await context.addInitScript(instrument, { token, user: args.user, role: args.role, tenant: args.tenant });
  const page = await context.newPage();
  page.setDefaultTimeout(30000);
  const cdp = await context.newCDPSession(page);
  await cdp.send('Network.enable');
  await cdp.send('Network.emulateNetworkConditions', { offline: false, latency: PROFILE.latencyMs, downloadThroughput: PROFILE.bytesPerSec, uploadThroughput: PROFILE.bytesPerSec });
  await cdp.send('Emulation.setCPUThrottlingRate', { rate: PROFILE.cpu });
  await cdp.send('Performance.enable');
  const pageErrors = [];
  page.on('pageerror', e => pageErrors.push(e.message.split('\n')[0]));
  const api = new ApiTracker(page);
  return { context, page, cdp, api, pageErrors };
}

async function cdpMetrics(cdp) {
  const { metrics } = await cdp.send('Performance.getMetrics');
  return Object.fromEntries(metrics.map(m => [m.name, m.value]));
}

async function waitViewReady(page, api, quietMs = 500) {
  await page.waitForFunction(() => { const r = document.getElementById('view-root'); return r && r.children.length && !r.querySelector('.view-loading'); }, null, { timeout: 30000 });
  for (let i = 0; i < 80; i++) {
    await api.quiet(300);
    const idle = await page.evaluate(q => { const sw = window.__b45.sw; return !sw || performance.now() - sw.lastRaw >= q; }, quietMs);
    if (idle && !api.pending.size) return;
    await sleep(100);
  }
}

async function switchTo(page, api, hash) {
  await api.pace();
  await page.evaluate(h => window.__b45switch(h), hash);
  await waitViewReady(page, api);
  await sleep(120); // let the last afterPaint callback land
  return page.evaluate(() => {
    const sw = window.__b45.sw; window.__b45.sw = null;
    return { hash: sw.hash, first: sw.first ? sw.first - sw.t0 : null, settled: sw.last ? sw.last - sw.t0 : null, mutations: sw.mutations };
  });
}

// ---------------------------------------------------------------- cold boot
async function coldBoot(page, cdp, api) {
  await page.goto(BASE + '/?r=1#/view/home', { waitUntil: 'load' });
  await waitViewReady(page, api);
  await sleep(1500); // let a late LCP candidate land; no input has happened yet
  const metrics = await cdpMetrics(cdp);
  const data = await page.evaluate(() => {
    const S = window.__b45;
    const nav = performance.getEntriesByType('navigation')[0];
    const lcp = S.lcp.at(-1) || null;
    const lcpT = lcp ? lcp.t : Infinity;
    const res = performance.getEntriesByType('resource').filter(r => r.startTime < lcpT).map(r => ({
      name: r.name.replace(location.origin, ''), type: r.initiatorType, start: Math.round(r.startTime), end: Math.round(r.responseEnd), bytes: r.transferSize, blocking: r.renderBlockingStatus,
    }));
    const fcp = performance.getEntriesByName('first-contentful-paint')[0];
    return {
      nav: nav && { ttfb: Math.round(nav.responseStart), domInteractive: Math.round(nav.domInteractive), dcl: Math.round(nav.domContentLoadedEventEnd), load: Math.round(nav.loadEventEnd) },
      fcp: fcp ? Math.round(fcp.startTime) : null,
      rootSeen: Math.round(S.rootSeen),
      lcp, lcpAll: S.lcp.map(e => ({ t: Math.round(e.t), size: e.size, el: e.el.slice(0, 120) })),
      resources: res,
      longBeforeLcp: S.longtasks.filter(t => t.t < lcpT).map(t => ({ t: Math.round(t.t), d: Math.round(t.d) })),
      loafBeforeLcp: S.loaf.filter(f => f.t < lcpT && f.d > 50).map(f => ({ t: Math.round(f.t), d: Math.round(f.d), block: Math.round(f.block), scripts: f.scripts.filter(s => s.d >= 10).sort((a, b) => b.d - a.d).slice(0, 6) })),
      shifts: S.shifts.slice(),
      unsupported: S.unsupported,
      heap: performance.memory ? performance.memory.usedJSHeapSize : null,
    };
  });
  data.cdp = { ScriptDuration: r1(metrics.ScriptDuration * 1000), LayoutDuration: r1(metrics.LayoutDuration * 1000), RecalcStyleDuration: r1(metrics.RecalcStyleDuration * 1000), TaskDuration: r1(metrics.TaskDuration * 1000), Nodes: metrics.Nodes, JSEventListeners: metrics.JSEventListeners };
  data.cls = clsOf(data.shifts);
  return data;
}

// ---------------------------------------------------------------- interactions
async function interactions(page, api) {
  // Each step is a real input event, so Event Timing scores it. The table's
  // own search box is debounced and server-side (BLD-034); "settled" is the
  // frame after the last #view-root change the step caused.
  const out = {};
  const timed = async (label, act) => {
    await api.pace(8);
    await page.evaluate(l => { window.__b45.sw = { hash: l, t0: performance.now(), first: 0, firstRaw: 0, last: 0, lastRaw: 0, mutations: 0 }; }, label);
    await act();
    await sleep(450); // past the search box's 300 ms debounce before judging "quiet"
    await waitViewReady(page, api);
    await sleep(150);
    return page.evaluate(() => {
      const sw = window.__b45.sw; window.__b45.sw = null;
      return { settled: sw.last ? Math.round(sw.last - sw.t0) : null, rows: document.querySelectorAll('#view-root tbody tr').length };
    });
  };
  await switchTo(page, api, '#/setup/Item');
  out.itemRows = await page.evaluate(() => document.querySelectorAll('#view-root tbody tr').length);
  await page.locator('#doc-table-search').click();
  out.search = await timed('search', () => page.keyboard.type('shirt', { delay: 140 }));
  out.clear = await timed('clear', async () => { await page.keyboard.press('Control+A'); await page.keyboard.press('Backspace'); });
  out.next = await timed('next', () => page.locator('#doc-table-next-btn').click());
  out.prev = await timed('prev', () => page.locator('#doc-table-prev-btn').click());
  // The top search box while a record list is open (its handler re-renders
  // the list on every keystroke), scored separately below.
  await page.locator('#global-search').click();
  out.globalSearch = await timed('global', () => page.keyboard.type('pur', { delay: 140 }));
  await page.keyboard.press('Escape');
  await page.locator('#global-search').fill('');
  await sleep(400);
  await api.pace();
  await page.locator('#menu-home').click();
  await waitViewReady(page, api);
  await sleep(300);
  for (let i = 0; i < 6; i++) { await page.keyboard.press('Tab'); await sleep(120); }
  await sleep(500);
  return out;
}

// ---------------------------------------------------------------- one context
async function runContext(browser, token, index) {
  const { context, page, cdp, api, pageErrors } = await newProfiledContext(browser, token);
  try {
    const boot = await coldBoot(page, cdp, api);
    const identity = await page.evaluate(async () => {
      const r = await fetch('/api/v1/me', { headers: { Authorization: 'Bearer ' + localStorage.getItem('erp_token') } });
      const j = await r.json().catch(() => ({}));
      return { status: r.status, role: j.role, username: j.username };
    });
    for (const h of SWITCH_VIEWS.slice(1).concat(SWITCH_VIEWS[0])) await switchTo(page, api, h); // warm every module once
    const switches = [];
    for (let r = 0; r < args.rounds; r++) {
      for (const h of SWITCH_VIEWS.slice(1).concat(SWITCH_VIEWS[0])) switches.push(await switchTo(page, api, h));
    }
    const inter = await interactions(page, api);
    const tail = await page.evaluate(() => ({ events: window.__b45.events, shifts: window.__b45.shifts, longtasks: window.__b45.longtasks }));
    const inp = inpOf(tail.events);
    // Observer callbacks can land after the step ends, so match by target:
    // the top search box is only typed into during that one step.
    const globalIds = new Set(tail.events.filter(e => String(e.target).startsWith('input#global-search')).map(e => e.id));
    const inpNormal = inpOf(tail.events.filter(e => !globalIds.has(e.id)));
    const cls = clsOf(tail.shifts);
    const settled = switches.map(s => s.settled).filter(x => x != null);
    const first = switches.map(s => s.first).filter(x => x != null);
    const result = {
      context: index + 1, identity,
      lcp: boot.lcp ? boot.lcp.t : null, lcpElement: boot.lcp ? boot.lcp.el : null, fcp: boot.fcp, rootSeen: boot.rootSeen, nav: boot.nav,
      // The SIMULATED-environment strip exists only on non-production
      // servers; this is the LCP a production page would report.
      lcpNoBanner: (boot.lcpAll.filter(e => !e.el.includes('environment-banner')).at(-1) || {}).t || null,
      lcpNoBannerElement: (boot.lcpAll.filter(e => !e.el.includes('environment-banner')).at(-1) || {}).el || null,
      bootCls: boot.cls.value, bootClsSources: boot.cls.window.flatMap(s => s.sources.map(x => x.node)).slice(0, 6),
      cls: cls.value, clsSources: cls.window.map(s => ({ t: Math.round(s.t), v: r4(s.v), nodes: s.sources.map(x => x.node) })).slice(0, 6),
      inp: inp.value, inpNoGlobalSearch: inpNormal.value, inpWorst: inp.worst.map(e => `${e.name} on ${e.target} ${Math.round(e.d)}ms (input delay ${Math.round(e.ps - e.t)}, processing ${Math.round(e.pe - e.ps)}, presentation ${Math.round(e.t + e.d - e.pe)})`),
      interactions: inp.interactions,
      switchSettledP95: pct(settled, 95), switchSettledP50: pct(settled, 50), switchFirstP95: pct(first, 95),
      switches: switches.map(s => ({ hash: s.hash, first: r1(s.first), settled: r1(s.settled) })),
      perView: Object.fromEntries(SWITCH_VIEWS.map(h => [h, r1(pct(switches.filter(s => s.hash === h && s.settled != null).map(s => s.settled), 95))])),
      longestTask: Math.max(0, ...tail.longtasks.map(t => t.d)),
      longestBootTask: Math.max(0, ...boot.longBeforeLcp.map(t => t.d)),
      itemRows: inter.itemRows, search: inter.search, clear: inter.clear, next: inter.next, prev: inter.prev, globalSearch: inter.globalSearch,
      boot,
      api429: api.status429, api5xx: api.errors, pageErrors,
    };
    return result;
  } finally {
    await context.close();
  }
}

// ---------------------------------------------------------------- long session
async function longSession(browser, token, n) {
  const { context, page, cdp, api, pageErrors } = await newProfiledContext(browser, token);
  try {
    await page.goto(BASE + '/?r=1#/view/home', { waitUntil: 'load' });
    await waitViewReady(page, api);
    await cdp.send('HeapProfiler.enable');
    const sample = async label => {
      await cdp.send('HeapProfiler.collectGarbage');
      const heap = await cdp.send('Runtime.getHeapUsage');
      const m = await cdpMetrics(cdp);
      return { label, heapMB: r1(heap.usedSize / 1048576), nodes: m.Nodes, listeners: m.JSEventListeners };
    };
    const samples = [await sample('start')];
    const segments = [];
    let seg = { switches: [], fromT: await page.evaluate(() => performance.now()) };
    const started = Date.now();
    for (let i = 0; i < n; i++) {
      const s = await switchTo(page, api, LONG_VIEWS[(i + 1) % LONG_VIEWS.length]);
      seg.switches.push(s.settled);
      if ((i + 1) % 8 === 0 || i === n - 1) {
        const toT = await page.evaluate(() => performance.now());
        const lt = await page.evaluate(([a, b]) => window.__b45.longtasks.filter(t => t.t >= a && t.t < b).map(t => t.d), [seg.fromT, toT]);
        const smp = await sample(`after ${i + 1}`);
        samples.push(smp);
        segments.push({ upTo: i + 1, switchP95: r1(pct(seg.switches.filter(x => x != null), 95)), longTasks: lt.length, longTaskMs: Math.round(lt.reduce((a, b) => a + b, 0)), longestTask: Math.round(Math.max(0, ...lt)) });
        seg = { switches: [], fromT: toT };
      }
    }
    return { switches: n, minutes: r1((Date.now() - started) / 60000), samples, segments, api429: api.status429, api5xx: api.errors, pageErrors };
  } finally {
    await context.close();
  }
}

// ---------------------------------------------------------------- main
(async () => {
  const browser = await resolvePlaywright().chromium.launch(args.channel ? { channel: args.channel } : {});
  const report = { when: new Date().toISOString(), browser: browser.version(), profile: PROFILE, budget: BUDGET, views: SWITCH_VIEWS, contexts: [] };
  try {
    for (let i = 0; i < args.contexts; i++) {
      process.stderr.write(`context ${i + 1}/${args.contexts}...\n`);
      const c = await runContext(browser, tokens[i], i);
      report.contexts.push(c);
      process.stderr.write(`  LCP ${Math.round(c.lcp)} ms (${c.lcpElement}); switch p95 ${r1(c.switchSettledP95)} ms; INP ${Math.round(c.inp)} ms; CLS ${r4(c.cls)}; longest task ${Math.round(c.longestTask)} ms; 429s ${c.api429}\n`);
    }
    if (args.long > 0) {
      process.stderr.write(`long session: ${args.long} switches...\n`);
      report.long = await longSession(browser, tokens[args.contexts], args.long);
    }
  } finally {
    await browser.close();
  }
  const cs = report.contexts;
  const allSettled = cs.flatMap(c => c.switches.map(s => s.settled)).filter(x => x != null);
  report.summary = {
    lcpP75: pct(cs.map(c => c.lcp), 75),
    lcpElements: [...new Set(cs.map(c => c.lcpElement))],
    lcpNoBannerP75: pct(cs.map(c => c.lcpNoBanner).filter(x => x != null), 75),
    lcpNoBannerElements: [...new Set(cs.map(c => c.lcpNoBannerElement))],
    fcpP75: pct(cs.map(c => c.fcp), 75),
    inpNoGlobalSearchP75: pct(cs.map(c => c.inpNoGlobalSearch), 75),
    perView: Object.fromEntries(SWITCH_VIEWS.map(h => [h, r1(pct(cs.flatMap(c => c.switches.filter(s => s.hash === h && s.settled != null).map(s => s.settled)), 95))])),
    inpP75: pct(cs.map(c => c.inp), 75),
    switchSettledP95: pct(allSettled, 95),
    switchSettledP50: pct(allSettled, 50),
    switchFirstP95: pct(cs.flatMap(c => c.switches.map(s => s.first)).filter(x => x != null), 95),
    maxCls: Math.max(...cs.map(c => c.cls)),
    maxBootCls: Math.max(...cs.map(c => c.bootCls)),
    longestTask: Math.max(...cs.map(c => c.longestTask)),
    longestBootTask: Math.max(...cs.map(c => c.longestBootTask)),
    search: cs.map(c => c.search && c.search.settled),
    paging: cs.map(c => c.next && c.next.settled),
    globalSearch: cs.map(c => c.globalSearch && c.globalSearch.settled),
    api429: cs.reduce((a, c) => a + c.api429, 0) + (report.long ? report.long.api429 : 0),
    pageErrors: cs.flatMap(c => c.pageErrors).concat(report.long ? report.long.pageErrors : []),
  };
  if (args.out) fs.writeFileSync(path.resolve(args.out), JSON.stringify(report, null, 2));

  const s = report.summary;
  const line = (name, v, budget, unit = 'ms') => console.log(`${name.padEnd(26)} ${String(v).padStart(9)} ${unit}${budget != null ? `   budget ${budget} ${unit}  ${v <= budget ? 'PASS' : 'MISS by ' + r1(v - budget) + ' ' + unit}` : ''}`);
  console.log(`\nBLD-045 (${args.channel || 'chromium'} ${report.browser};${PROFILE.viewport.width}x${PROFILE.viewport.height}, CPU x${PROFILE.cpu}, ${PROFILE.latencyMs} ms RTT, 5 Mbps; ${cs.length} contexts)`);
  console.log('per context LCP          ' + cs.map(c => Math.round(c.lcp)).join(' / ') + ' ms');
  line('LCP p75', Math.round(s.lcpP75), BUDGET.lcp);
  console.log('LCP element              ' + s.lcpElements.join(' | '));
  line('LCP p75 w/o dev banner', Math.round(s.lcpNoBannerP75), BUDGET.lcp);
  console.log('  element                ' + s.lcpNoBannerElements.join(' | '));
  line('FCP p75', Math.round(s.fcpP75));
  line('candidate INP p75', Math.round(s.inpP75), BUDGET.inp);
  line('  w/o top-search typing', Math.round(s.inpNoGlobalSearchP75), BUDGET.inp);
  console.log('  worst                  ' + cs.map(c => c.inpWorst[0]).join(' | '));
  line('cached switch p95 settled', r1(s.switchSettledP95), BUDGET.switchP95);
  line('cached switch p50 settled', r1(s.switchSettledP50));
  line('cached switch p95 first', r1(s.switchFirstP95));
  console.log('  p95 by view            ' + Object.entries(s.perView).map(([h, v]) => `${h.replace('#/', '')} ${v}`).join(', '));
  line('max CLS (session window)', r4(s.maxCls), BUDGET.cls, '');
  line('max boot CLS', r4(s.maxBootCls), BUDGET.cls, '');
  line('longest task', Math.round(s.longestTask));
  line('longest task before LCP', Math.round(s.longestBootTask));
  console.log('Item search settle       ' + s.search.join(' / ') + ' ms (from first key: typing + 300 ms debounce + fetch + render)');
  console.log('Item next-page settle    ' + s.paging.join(' / ') + ' ms');
  console.log('top search on the list   ' + s.globalSearch.join(' / ') + ' ms (3 keys)');
  console.log(`429 responses ${s.api429}; page errors ${s.pageErrors.length}${s.pageErrors.length ? ': ' + [...new Set(s.pageErrors)].join(' | ') : ''}`);
  if (report.long) {
    const L = report.long;
    console.log(`\nlong session: ${L.switches} switches in ${L.minutes} min`);
    for (const x of L.samples) console.log(`  ${x.label.padEnd(10)} heap ${x.heapMB} MB  nodes ${x.nodes}  listeners ${x.listeners}`);
    for (const g of L.segments) console.log(`  switches ..${g.upTo}: p95 ${g.switchP95} ms, long tasks ${g.longTasks} (${g.longTaskMs} ms, max ${g.longestTask} ms)`);
  }
})().catch(e => { console.error(e.stack); process.exitCode = 1; });
