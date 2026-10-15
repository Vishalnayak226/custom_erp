'use strict';
const fs = require('node:fs');
const path = require('node:path');
const { validate } = require('./validate.cjs');
const scenes = require('./scenes.cjs');
const root = path.resolve(__dirname, '..');
const project = JSON.parse(fs.readFileSync(path.join(root, 'project.json'), 'utf8'));
const sceneId = process.argv[2];
const segment = project.segments.find(s => s.id === sceneId);
if (!segment || !scenes[sceneId]) throw new Error('Choose a known scene id from project.json.');
const base = new URL(process.env.SOP_BASE_URL || 'http://127.0.0.1:8111');
if (base.protocol !== 'http:' || !['127.0.0.1', 'localhost'].includes(base.hostname)) throw new Error('Recorder refuses non-loopback ERP URLs.');
const authPath = process.env.SOP_STORAGE_STATE;
if (!authPath || !fs.existsSync(authPath)) throw new Error('Set SOP_STORAGE_STATE to a private local Playwright storage-state file.');
const outputDir = path.resolve(process.env.SOP_OUTPUT_DIR || path.join(process.env.TEMP || '.', 'erp-sop-video'));
const fixtureDir = path.resolve(process.env.SOP_FIXTURE_DIR || outputDir);
if (outputDir.startsWith(root + path.sep) || fixtureDir.startsWith(root + path.sep)) throw new Error('Generate recordings and fixture state outside the project, then review/copy in.');
fs.mkdirSync(outputDir, { recursive: true }); fs.mkdirSync(fixtureDir, { recursive: true });
const stateFile = path.join(fixtureDir, 'run-state.local.json');
const state = fs.existsSync(stateFile) ? JSON.parse(fs.readFileSync(stateFile, 'utf8')) : { tag: process.env.SOP_RUN_TAG || 'Oct 2026 A' };
const playwright = require(require.resolve('playwright', { paths: [process.cwd(), process.env.USERPROFILE || process.env.HOME || ''] }));

async function main() {
  const browser = await playwright.chromium.launch({ headless: true });
  const context = await browser.newContext({ storageState: authPath, viewport: { width: 1280, height: 720 }, recordVideo: { dir: outputDir, size: { width: 1280, height: 720 } }, reducedMotion: 'reduce' });
  const page = await context.newPage(); page.setDefaultTimeout(18000);
  const apiErrors = [];
  page.on('response', r => { if (new URL(r.url()).pathname.startsWith('/api/') && r.status() >= 400) apiErrors.push({ path: new URL(r.url()).pathname, status: r.status() }); });
  const h = {
    async overlay(kind, message) {
      await page.evaluate(({ kind, message }) => {
        let el = document.getElementById('sop-film-overlay');
        if (!el) { el = document.createElement('div'); el.id = 'sop-film-overlay'; document.body.appendChild(el); }
        el.textContent = message; el.dataset.kind = kind;
        Object.assign(el.style, kind === 'card' ? {
          position: 'fixed', inset: '0', zIndex: '2147483647', display: 'grid', placeItems: 'center',
          background: '#17243a', color: '#fff', font: '700 38px/1.3 system-ui,sans-serif',
          textAlign: 'center', padding: '90px', whiteSpace: 'pre-wrap', pointerEvents: 'none'
        } : {
          position: 'fixed', inset: 'auto 24px 18px 24px', zIndex: '2147483647', display: 'block',
          background: '#17243aef', color: '#fff', font: '600 21px/1.3 system-ui,sans-serif',
          borderRadius: '12px', padding: '15px 20px', textAlign: 'left', whiteSpace: 'normal', pointerEvents: 'none'
        });
      }, { kind, message });
    },
    async open(route, message) {
      await page.goto(base.origin + route, { waitUntil: 'domcontentloaded' });
      await page.locator('#app-root:not(.hidden)').waitFor();
      await page.locator('#view-root').waitFor();
      await this.note(message);
    },
    async note(message) { await this.overlay('caption', message); await page.waitForTimeout(1800); },
    async step(message, action, highlight) {
      await this.overlay('caption', message);
      if (highlight) {
        const target = page.locator(highlight).first();
        // Centred, not merely "in view": the caption bar covers the bottom of
        // the frame, and a target just above the fold sat underneath it.
        await target.evaluate(e => e.scrollIntoView({ block: 'center', inline: 'nearest' })).catch(() => {});
        await target.evaluate(e => { e.style.outline = '3px solid #f6c85f'; e.style.outlineOffset = '3px'; }).catch(() => {});
      }
      await page.waitForTimeout(600);
      const result = await action();
      await page.waitForTimeout(1350);
      return result;
    },
    async type(selector, value) {
      const element = page.locator(selector).first();
      await element.fill(''); await element.pressSequentially(value, { delay: 55 });
    },
    async post(pathname, action) {
      const pending = page.waitForResponse(r => r.request().method() === 'POST' && new URL(r.url()).pathname === pathname, { timeout: 16000 });
      await action(); const response = await pending; const body = await response.json().catch(() => ({}));
      if (!response.ok()) throw new Error(`${pathname} returned ${response.status()}: ${JSON.stringify(body).slice(0, 300)}`);
      return body;
    },
    async saveDoc(doctype) {
      const body = await this.post(`/api/v1/doc/${doctype}`, () => page.locator('#dynamic-modal-form button[type="submit"]').click());
      await page.waitForFunction(() => !document.querySelector('#dynamic-modal')?.classList.contains('open'));
      return body;
    }
  };
  let result, videoPath;
  try {
    await page.goto(base.origin + '/#/view/home', { waitUntil: 'domcontentloaded' });
    await page.locator('#app-root:not(.hidden)').waitFor({ timeout: 12000 });
    if (await page.locator('#login-container:not(.hidden)').count()) throw new Error('Login state expired; refresh the private storage state.');
    await h.overlay('card', `START  •  ${segment.title}\n${segment.actor}`);
    await page.waitForTimeout(2700);
    result = await scenes[sceneId](page, h, state);
    await h.overlay('card', `END  •  ${segment.title}\n${result}`);
    await page.waitForTimeout(2800);
    videoPath = await page.video().path();
    fs.writeFileSync(stateFile, JSON.stringify(state, null, 2));
  } finally {
    await context.close(); await browser.close();
  }
  const target = path.join(outputDir, `${sceneId.replaceAll('_', '-')}.webm`);
  fs.copyFileSync(videoPath, target);
  const meta = { scene: sceneId, file: target, bytes: fs.statSync(target).size, recorded_at: new Date().toISOString(), result, api_errors: apiErrors };
  fs.writeFileSync(path.join(outputDir, `${sceneId}.json`), JSON.stringify(meta, null, 2));
  console.log(JSON.stringify(meta));
}
if (validate(project).length) throw new Error('project.json is invalid; run validate.cjs first.');
main().catch(error => { console.error(error.stack || error); process.exitCode = 1; });
