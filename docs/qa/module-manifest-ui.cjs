'use strict';
// BLD-021 browser contract test. Uses the real navigation registry, dispatcher,
// entitlement loader and DOM hiding functions. Leaf screen renderers are spies:
// this checks entry boundaries, not the BLD-022 business journeys.
// Run: node docs/qa/module-manifest-ui.cjs <absolute evidence directory>
const assert = require('node:assert/strict');
const fs = require('node:fs');
const path = require('node:path');
const { resolvePlaywright } = require('../guides/capture-screenshots.js');
const out = process.argv[2];
assert(out && path.isAbsolute(out), 'An explicit absolute evidence directory is required');
const source = fs.readFileSync(path.join(__dirname, '../../public/app.js'), 'utf8').replace(/\r\n/g, '\n');
function between(start, end) {
  const a = source.indexOf(start), b = source.indexOf(end, a);
  assert(a >= 0 && b > a, `Missing production source boundary: ${start}`);
  return source.slice(a, b);
}
const registry = source.match(/const MENU_MODULE_MAP = (\{[\s\S]*?\n\});/);
assert(registry, 'Production navigation registry missing');
const dispatch = between('async function renderViewContent(view, root) {', '// Translate labels');
const leaves = [...new Set([...dispatch.matchAll(/\b(render\w+)\(root/g)].map(m => m[1]))];
const production = registry[0] + '\n'
  + between('function isMenuModuleVisible(', '// applyProductPathRouting')
  + '\n' + dispatch;

(async () => {
  const browser = await resolvePlaywright().chromium.launch();
  const results = [];
  try {
    const context = await browser.newContext({ viewport: { width: 1280, height: 800 } });
    const page = await context.newPage();
    await page.setContent('<main id="root"></main><ul id="navigation"></ul>');
    await page.addScriptTag({ content: `
      let state = { modules: { enabled: null, loaded: false }, activeDoctypes: [] };
      let currentView = '', currentDoctype = 'BoundaryType', calls = [];
      let response = { ok: false };
      function saveNavState() {}
      function renderProductSwitcher() {}
      async function apiFetch() { if (response instanceof Error) throw response; return response; }
      ${leaves.map(name => `async function ${name}(root) { calls.push('${name}'); root.textContent = 'Rendered'; }`).join('\n')}
      ${production}
    ` });
    const result = await page.evaluate(async () => {
      const check = (condition, message) => { if (!condition) throw new Error(message); };
      const root = document.getElementById('root');
      let denied = 0, allowed = 0, hidden = 0;
      const keys = [...new Set(Object.values(MENU_MODULE_MAP).map(item => item.module))];
      for (const [id, item] of Object.entries(MENU_MODULE_MAP)) {
        const li = document.createElement('li'), a = document.createElement('a');
        a.id = id; li.append(a); document.getElementById('navigation').append(li);
        for (const view of item.views) {
          state.activeDoctypes = [{ name: currentDoctype, module_key: item.module }];
          state.modules = { enabled: new Set(keys.filter(key => key !== item.module)), loaded: true };
          calls = []; await renderViewContent(view, root);
          check(calls.length === 0 && root.textContent.includes('Unavailable'), `Disabled view rendered: ${view}`); denied++;
          state.modules.enabled.add(item.module);
          calls = []; await renderViewContent(view, root);
          check(calls.length === 1 && root.textContent === 'Rendered', `Enabled view blocked: ${view}`); allowed++;
        }
      }
      state.modules = { enabled: new Set(), loaded: true }; applyModuleEntitlements();
      for (const id of Object.keys(MENU_MODULE_MAP)) {
        check(document.getElementById(id).parentElement.classList.contains('module-hidden'), `Disabled menu visible: ${id}`); hidden++;
      }
      state.modules.enabled = new Set(keys); applyModuleEntitlements();
      for (const id of Object.keys(MENU_MODULE_MAP)) check(!document.getElementById(id).parentElement.classList.contains('module-hidden'), `Enabled menu hidden: ${id}`);
      for (const view of ['__unknown_view__', 'doctype-table']) {
        currentDoctype = '__unknown_type__'; calls = []; await renderViewContent(view, root);
        check(calls.length === 0 && root.textContent.includes('Unavailable'), `Unknown path rendered: ${view}`);
      }
      check(!isMenuModuleVisible('__unknown_module__'), 'Unknown module allowed');
      for (const failure of [{ ok: false }, new Error('fixture offline')]) {
        state.modules = { enabled: null, loaded: false }; response = failure;
        calls = []; await renderViewContent('hr', root);
        check(calls.length === 0 && root.textContent.includes('Unavailable'), 'Unavailable entitlement service failed open');
      }
      response = { ok: true, json: async () => ({ enabled_modules: ['hr'] }) };
      calls = []; await renderViewContent('hr', root);
      check(calls.length === 1 && root.textContent === 'Rendered', 'Entitlement load did not recover');
      return { denied, allowed, hidden, unknownPaths: 3, failedLoads: 2, recovered: true };
    });
    results.push({ fixture: 'isolated Chromium DOM; production entry functions; leaf renderer spies', ...result });
    fs.mkdirSync(out, { recursive: true });
    fs.writeFileSync(path.join(out, 'module-ui.json'), JSON.stringify(results, null, 2) + '\n');
    console.log(JSON.stringify(results));
    await context.close();
  } finally { await browser.close(); }
})().catch(error => { console.error(error); process.exitCode = 1; });
