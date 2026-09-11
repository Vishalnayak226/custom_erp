// Documentation-only tooling; see ../governance/screenshot-capture.md.
'use strict';
const fs = require('node:fs');
const path = require('node:path');
const crypto = require('node:crypto');
function resolvePlaywright() {
  for (const candidate of ['playwright', path.join(process.env.USERPROFILE || process.env.HOME || '', 'node_modules/playwright'), path.join(process.env.APPDATA || '', 'npm/node_modules/playwright')]) {
    try { return require(candidate); } catch { /* optional local tooling */ }
  }
  throw new Error('Playwright is unavailable locally. It is not an ERP dependency.');
}
const SHOTS = [
  { id: 'sidebar', view: 'reports', caption: 'Main navigation', clip: { x: 0, y: 0, width: 300, height: 900 } },
  { id: 'setup-menu', view: 'reports', caption: 'Setup navigation', after: 'setup', clip: { x: 0, y: 0, width: 760, height: 900 } },
  { id: 'pos-billing', view: 'pos', caption: 'POS billing and cashier session' },
  { id: 'purchase-order', view: 'purchase-orders', caption: 'Purchase orders' },
  { id: 'goods-receipt', view: 'grn', caption: 'Goods receipt' },
  { id: 'inventory', view: 'inventory', caption: 'Inventory availability' },
  { id: 'trial-balance', view: 'finance', caption: 'Finance and trial balance' },
  { id: 'approvals', view: 'approvals', caption: 'Pending approvals' },
  { id: 'reports', view: 'reports', caption: 'Report catalog' },
  { id: 'record-list', doctype: 'Vendor', caption: 'Vendor records' },
  { id: 'json-line-editor', doctype: 'BOM', after: 'modal', allowedOverlay: '#dynamic-modal', caption: 'BOM line editor' },
  { id: 'configuration', view: 'configuration', caption: 'Tenant configuration' },
  { id: 'roles', view: 'roles', caption: 'Role permissions' },
  { id: 'returns', view: 'returns', caption: 'Return inspection and refund workflow' },
];
function parseArgs(argv) {
  const args = {};
  for (let i = 0; i < argv.length; i += 2) {
    if (!/^--[a-z-]+$/.test(argv[i]) || !argv[i + 1] || argv[i + 1].startsWith('--')) throw new Error('Options require an explicit value. See screenshot-capture.md.');
    const key = argv[i].slice(2);
    if (!['base', 'out', 'storage-state', 'release', 'role', 'tenant', 'fixture', 'theme', 'locale', 'viewport', 'only', 'interval-ms'].includes(key) || key in args) throw new Error('Unknown or repeated capture option.');
    args[key] = argv[i + 1];
  }
  for (const key of ['out', 'storage-state', 'release', 'role', 'tenant', 'fixture']) if (!args[key]?.trim()) throw new Error(`--${key} is required.`);
  const base = new URL(args.base || 'http://localhost:8152');
  if (!['http:', 'https:'].includes(base.protocol) || base.username || base.password || base.search || base.hash || base.pathname !== '/') throw new Error('Base must be an HTTP(S) origin without credentials, path, query or fragment.');
  const viewport = /^(\d+)x(\d+)$/.exec(args.viewport || '1440x900');
  if (!viewport || +viewport[1] < 320 || +viewport[2] < 400 || +viewport[1] > 3840 || +viewport[2] > 2160) throw new Error('Viewport must be WIDTHxHEIGHT within 320–3840 by 400–2160.');
  const theme = args.theme || 'light';
  if (!['light', 'dark'].includes(theme)) throw new Error('Theme must be light or dark.');
  const ids = args.only ? args.only.split(',').map(s => s.trim()) : SHOTS.map(s => s.id);
  const intervalMs = args['interval-ms'] === undefined ? 30000 : Number(args['interval-ms']);
  if (!Number.isInteger(intervalMs) || intervalMs < 0 || intervalMs > 60000) throw new Error('Capture interval must be 0–60000 milliseconds.');
  if (new Set(ids).size !== ids.length || ids.some(id => !SHOTS.some(s => s.id === id))) throw new Error('The shot selection contains an unknown or duplicate id.');
  return { base: base.origin, out: path.resolve(args.out), storageState: path.resolve(args['storage-state']), release: args.release,
    role: args.role, tenant: args.tenant, fixture: args.fixture, theme, locale: args.locale || 'en-IN',
    viewport: { width: +viewport[1], height: +viewport[2] }, shots: SHOTS.filter(s => ids.includes(s.id)), timeout: 15000, intervalMs };
}
// Browser-side DOM checks: a hidden ancestor can clip controls even when the
// document reports no overflow. Scrollable areas may legitimately extend below it.
function inspectScreen(allowedOverlay) {
  const visible = el => {
    if (!el) return false;
    const r = el.getBoundingClientRect(), s = getComputedStyle(el);
    return r.width > 0 && r.height > 0 && s.visibility !== 'hidden' && s.display !== 'none' && +s.opacity !== 0;
  };
  if (visible(document.querySelector('#login-screen'))) return 'Authentication screen is visible.';
  const root = document.querySelector('#view-root');
  if (!visible(root) || root.innerText.trim().length < 15) return 'The requested screen is empty.';
  for (const el of document.querySelectorAll('.modal-overlay, [role="dialog"], .loading-overlay, .view-loading, [aria-busy="true"]')) {
    if (visible(el) && !(allowedOverlay && (el.matches(allowedOverlay) || el.closest(allowedOverlay)))) return 'Unexpected dialog or loading overlay.';
  }
  for (const el of document.querySelectorAll('.login-error, .alert-danger, .view-error, [role="alert"]')) if (visible(el) && el.innerText.trim()) return 'The screen contains an error or alert.';
  if (/access denied|not authorized|not permitted|permission denied|session expired/i.test(root.innerText)) return 'The requested screen reports an authorization failure.';
  if (document.documentElement.scrollWidth > innerWidth + 2) return 'The page overflows the viewport horizontally.';
  const scope = allowedOverlay ? document.querySelector(allowedOverlay) : root;
  if (!visible(scope)) return 'The requested dialog is not visible.';
  for (const el of scope.querySelectorAll('button,input,select,textarea,th,td,h1,h2,h3,label,a')) {
    if (!visible(el)) continue;
    const r = el.getBoundingClientRect();
    let scrollX = false, scrollY = false;
    for (let parent = el.parentElement; parent; parent = parent.parentElement) {
      const s = getComputedStyle(parent), p = parent.getBoundingClientRect();
      scrollX ||= /auto|scroll/.test(s.overflowX);
      scrollY ||= /auto|scroll/.test(s.overflowY);
      if (!scrollX && /hidden|clip/.test(s.overflowX) && (r.left < p.left - 3 || r.right > p.right + 3)) return 'A control or table cell is clipped horizontally.';
      if (!scrollY && /hidden|clip/.test(s.overflowY) && (r.top < p.top - 3 || r.bottom > p.bottom + 3)) return 'A control or table cell is clipped vertically.';
    }
  }
  return null;
}
async function capture(options, playwright = resolvePlaywright()) {
  if (fs.existsSync(options.out)) throw new Error('Output already exists. Use a new review directory.');
  fs.mkdirSync(path.dirname(options.out), { recursive: true });
  const stage = fs.mkdtempSync(path.join(path.dirname(options.out), '.erp-shots-'));
  let browser;
  try {
    browser = await playwright.chromium.launch();
    const context = await browser.newContext({ storageState: options.storageState, viewport: options.viewport, deviceScaleFactor: 2, colorScheme: options.theme, locale: options.locale });
    await context.addInitScript(theme => localStorage.setItem('erp-theme', theme), options.theme);
    const records = [];
    for (const shot of options.shots) {
      // A new page runs the app's startup requests. Pace full-page captures
      // within the real server limits; never disable or retry past a refusal.
      if (records.length && options.intervalMs) await new Promise(resolve => setTimeout(resolve, options.intervalMs));
      const page = await context.newPage();
      page.setDefaultTimeout(options.timeout);
      const errors = [];
      page.on('pageerror', () => errors.push('Uncaught page error.'));
      page.on('console', message => { if (message.type() === 'error') errors.push('Browser console error.'); });
      page.on('response', response => { if (response.status() >= 400) errors.push(`HTTP failure (${response.status()}).`); });
      page.on('requestfailed', () => errors.push('Network request failed.'));
      const fragment = shot.doctype ? `#/setup/${encodeURIComponent(shot.doctype)}` : `#/view/${encodeURIComponent(shot.view)}`;
      await page.goto(options.base + '/' + fragment, { waitUntil: 'networkidle', timeout: options.timeout });
      await page.locator('#view-root').waitFor({ state: 'visible' });
      await page.waitForFunction(() => !document.querySelector('#view-root .view-loading'));
      if (new URL(page.url()).hash !== fragment) throw new Error(`${shot.id}: navigation fell back to a different screen.`);
      const identityOK = await page.evaluate(async expected => {
        const token = localStorage.getItem('erp_token'), tenant = localStorage.getItem('erp_tenant_id') || 'default';
        if (!token || tenant !== expected.tenant || localStorage.getItem('erp_role') !== expected.role) return false;
        const response = await fetch('/api/v1/me', { headers: { Authorization: `Bearer ${token}`, 'X-Tenant-ID': tenant } });
        if (!response.ok) return false;
        const profile = await response.json();
        return profile.role === expected.role && profile.username === localStorage.getItem('erp_username');
      }, { role: options.role, tenant: options.tenant });
      if (!identityOK) throw new Error(`${shot.id}: session identity does not match the requested role and tenant.`);
      if (shot.after === 'setup') {
        await page.locator('#menu-master-definition').hover();
        await page.locator('#submenu-master').waitFor({ state: 'visible' });
      }
      if (shot.after === 'modal') {
        await page.evaluate(async () => {
          if (typeof window.openDynamicModal !== 'function') throw new Error('Document editor is unavailable.');
          await window.openDynamicModal();
        });
        await page.locator('#dynamic-modal.open').waitFor({ state: 'visible' });
        await page.waitForFunction(() => Number(getComputedStyle(document.querySelector('#dynamic-modal')).opacity) >= 0.99);
      }
      await page.evaluate(() => document.fonts.ready);
      const problem = await page.evaluate(inspectScreen, shot.allowedOverlay || null);
      if (problem || errors.length) throw new Error(`${shot.id}: ${problem || errors[0]}`);
      if (shot.clip && (shot.clip.width > options.viewport.width || shot.clip.height > options.viewport.height)) throw new Error(`${shot.id}: crop exceeds the chosen viewport.`);
      const filename = `${shot.id}.png`;
      await page.screenshot({ path: path.join(stage, filename), ...(shot.clip ? { clip: shot.clip } : { fullPage: false }), animations: 'disabled' });
      if (errors.length) throw new Error(`${shot.id}: ${errors[0]}`);
      records.push({ id: shot.id, file: filename, caption: shot.caption, route: fragment, sha256: crypto.createHash('sha256').update(fs.readFileSync(path.join(stage, filename))).digest('hex') });
      await page.close();
      if (options.onProgress) options.onProgress(shot.id);
    }
    const manifest = { schema_version: 1, status: 'pending-human-review', release: options.release, role: options.role, tenant_fixture: options.tenant, fixture: options.fixture,
      viewport: options.viewport, device_scale: 2, locale: options.locale, theme: options.theme, interval_ms: options.intervalMs || 0, captured_at: new Date().toISOString(), screenshots: records };
    fs.writeFileSync(path.join(stage, 'manifest.json'), JSON.stringify(manifest, null, 2) + '\n');
    fs.writeFileSync(path.join(stage, 'MANIFEST.md'), '# Screenshot review set\n\nAutomated checks passed. Human content, privacy, accessibility and clipping review is pending.\n\n' + records.map(r => `- [${r.caption}](${r.file})`).join('\n') + '\n');
    await browser.close();
    browser = null;
    if (fs.existsSync(options.out)) throw new Error('Output appeared during capture; no set published.');
    fs.renameSync(stage, options.out);
    return manifest;
  } finally {
    try { if (browser) await browser.close(); } finally {
      // Only the fresh, resolved staging child can be removed; old assets are untouched.
      if (path.dirname(stage) === path.dirname(options.out) && path.basename(stage).startsWith('.erp-shots-')) fs.rmSync(stage, { recursive: true, force: true });
    }
  }
}
module.exports = { parseArgs, inspectScreen, capture, resolvePlaywright, SHOTS };
if (require.main === module) {
  Promise.resolve().then(() => capture({ ...parseArgs(process.argv.slice(2)), onProgress: id => console.log(`Checked ${id}; staged for review.`) }))
    .then(result => console.log(`${result.screenshots.length} screenshots staged for human review.`))
    // Playwright exceptions may contain URLs or page content. Do not print them.
    .catch(() => { console.error('Screenshot capture failed; no set published. Check the fixture, session, selected screens and local browser installation.'); process.exitCode = 1; });
}
