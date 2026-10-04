'use strict';

// Read-only browser audit for BLD-041. The token and evidence directory are
// explicit inputs so neither credentials nor generated results enter source.
// Run: node docs/qa/bld041-lazy-view-audit.cjs <source-root> <evidence-dir>
//      <scratch-origin> <token-file>
const fs = require('node:fs');
const path = require('node:path');
const { resolvePlaywright } = require('../guides/capture-screenshots.js');

const [source, evidence, base = 'http://127.0.0.1:8101', tokenFile] = process.argv.slice(2);
if (!source || !evidence || !tokenFile) throw new Error('Source, scratch evidence directory, scratch origin, and token file are required.');
const origin = new URL(base);
if (origin.protocol !== 'http:' || !['127.0.0.1', 'localhost'].includes(origin.hostname) || origin.username || origin.password || origin.pathname !== '/') {
  throw new Error('Only a loopback scratch-server origin is allowed.');
}
const token = fs.readFileSync(tokenFile, 'utf8').replace(/^\uFEFF/, '').trim();
if (!token) throw new Error('The explicit scratch token file is empty.');
fs.mkdirSync(evidence, { recursive: true });

const appSource = fs.readFileSync(path.join(source, 'public/app.js'), 'utf8');
const moduleFiles = fs.readdirSync(path.join(source, 'public')).filter(name => /^view-.*\.js$/.test(name)).sort();
const exportOwners = new Map();
const modulesWithPrintCode = new Set();
const staticImports = [];
for (const file of moduleFiles) {
  const body = fs.readFileSync(path.join(source, 'public', file), 'utf8');
  for (const block of body.match(/export\s*\{[^}]+\}/g) || []) {
    for (const item of block.slice(block.indexOf('{') + 1, -1).split(',')) {
      const name = item.trim().split(/\s+as\s+/).at(-1);
      if (!name) continue;
      const owners = exportOwners.get(name) || [];
      owners.push(file);
      exportOwners.set(name, owners);
    }
  }
  if (/\b(?:window\.)?print\s*\(|\bqzTryPrint\s*\(/.test(body)) modulesWithPrintCode.add(file);
  if (/^\s*import\s+.+?\s+from\s+['"][^'"]+['"]/m.test(body)) staticImports.push(file);
}
const duplicateExports = [...exportOwners].filter(([, owners]) => owners.length > 1).map(([name, owners]) => ({ name, owners }));
const lazyBlock = appSource.match(/const LAZY_VIEW_MODULES = (\{[\s\S]*?\n\});/);
const menuBlock = appSource.match(/const MENU_MODULE_MAP = (\{[\s\S]*?\n\});/);
if (!lazyBlock || !menuBlock) throw new Error('Could not read the lazy-view and entitlement registries.');
const registryKeys = [...lazyBlock[1].matchAll(/^\s*'?([\w-]+)'?:\s*\[/gm)].map(match => match[1]);
const duplicateKeys = [...new Set(registryKeys.filter((key, index) => registryKeys.indexOf(key) !== index))];
const lazy = Function(`return (${lazyBlock[1]})`)();
const moduleMap = Function(`return (${menuBlock[1]})`)();
const ownerByView = new Map();
for (const item of Object.values(moduleMap)) for (const view of item.views || []) {
  ownerByView.set(view, (ownerByView.get(view) || []).concat(item.module));
}
const entries = Object.entries(lazy).map(([view, [file, entrypoint, label]]) => ({
  view, file, entrypoint, label, owners: ownerByView.get(view) || [],
}));
const duplicateOwners = entries.filter(entry => entry.owners.length !== 1);
const chunksByOwner = {};
for (const entry of entries) {
  chunksByOwner[entry.file] ||= {};
  const owner = entry.owners[0] || '(none)';
  chunksByOwner[entry.file][owner] = (chunksByOwner[entry.file][owner] || 0) + 1;
}
const mixedEntitlementChunks = Object.entries(chunksByOwner)
  .filter(([, owners]) => Object.keys(owners).length > 1)
  .map(([file, owners]) => ({ file, owners }));

async function waitForApiQuiescence(pending, timeoutMs = 12000) {
  const until = Date.now() + timeoutMs;
  let lastActivity = Date.now();
  while (Date.now() < until) {
    if (pending.size) lastActivity = Date.now();
    if (!pending.size && Date.now() - lastActivity >= 200) return;
    await new Promise(resolve => setTimeout(resolve, 50));
  }
  if (pending.size) throw new Error(`API requests did not quiesce: ${[...pending].join(', ')}`);
}

async function attachToken(context) {
  await context.addInitScript(value => {
    localStorage.setItem('erp_token', value);
    localStorage.setItem('erp_tenant_id', 'default');
  }, token);
}

let nextApiRequestAt = 0;
let disabledModule = '';
async function installApiPacer(page) {
  await page.route('**/api/**', async route => {
    const delay = Math.max(0, nextApiRequestAt - Date.now());
    if (delay) await new Promise(resolve => setTimeout(resolve, delay));
    nextApiRequestAt = Date.now() + 1250; // stay below the scratch API's real 60/minute/IP limit
    const url = new URL(route.request().url());
    if (route.request().method() === 'GET' && /^\/api\/v1\/admin\/extension\/hooks\/[^/]+\/log$/.test(url.pathname)) {
      await route.fulfill({ status: 200, contentType: 'application/json', body: '[]' });
      return;
    }
    if (url.pathname === '/api/v1/me/modules' && disabledModule) {
      const response = await route.fetch();
      const data = await response.json();
      data.enabled_modules = (data.enabled_modules || []).filter(key => key !== disabledModule);
      await route.fulfill({ response, body: JSON.stringify(data) });
      return;
    }
    await route.continue();
  });
}

async function main() {
  const { chromium } = resolvePlaywright();
  const browser = await chromium.launch({ headless: true });
  const result = {
    generatedAt: new Date().toISOString(),
    browser: browser.version(),
    origin: origin.origin,
    viewCount: entries.length,
    moduleCount: moduleFiles.length,
    sourceChecks: {
      duplicateRegistryKeys: duplicateKeys,
      duplicateExportNames: duplicateExports,
      staticInterModuleImports: staticImports,
      duplicateEntitlementOwners: duplicateOwners,
      mixedEntitlementChunks,
      printCapableChunks: [...modulesWithPrintCode].sort(),
    },
    screens: [],
    printChecks: [],
    moduleLoadRecovery: [],
    entitlementGates: [],
    unauthenticated: null,
  };

  try {
    // One real Super Admin session traverses every registered lazy entrypoint.
    const context = await browser.newContext({ viewport: { width: 1440, height: 900 }, reducedMotion: 'reduce' });
    await attachToken(context);
    const page = await context.newPage();
    page.setDefaultTimeout(12000);
    const pageErrors = [];
    const pendingApi = new Set();
    const viewRequests = [];
    await installApiPacer(page);
    page.on('pageerror', error => pageErrors.push(error.message));
    page.on('request', request => {
      const url = new URL(request.url());
      if (url.pathname.startsWith('/api/')) pendingApi.add(request.url());
      if (/^\/view-.*\.js$/.test(url.pathname)) viewRequests.push(url.pathname.slice(1));
    });
    page.on('requestfinished', request => pendingApi.delete(request.url()));
    page.on('requestfailed', request => pendingApi.delete(request.url()));
    await page.goto(origin.origin + '/', { waitUntil: 'domcontentloaded' });
    await page.locator('#app-root:not(.hidden)').waitFor({ state: 'visible' });
    await page.waitForFunction(() => state.modules.loaded);
    const identity = await page.evaluate(async () => {
      const response = await fetch('/api/v1/me', { headers: { Authorization: `Bearer ${localStorage.getItem('erp_token')}` } });
      const value = await response.json();
      return { status: response.status, role: value.role, username: value.username };
    });
    if (identity.status !== 200 || !/admin/i.test(identity.role || '')) throw new Error(`Scratch identity check failed: ${JSON.stringify(identity)}`);
    result.identity = { status: identity.status, role: identity.role };

    // Test one live entitlement response per non-core module before any of
    // those modules have been imported. Each response is fetched from the
    // server and has exactly one module removed in-flight; no DB state changes.
    const samples = new Map();
    for (const entry of entries) {
      const owner = entry.owners[0];
      if (!owner || owner === 'core') continue;
      if (!samples.has(owner)) samples.set(owner, entry);
    }
    for (const [owner, entry] of samples) {
      disabledModule = owner;
      const beforeChunks = viewRequests.length;
      await page.evaluate(async view => {
        state.modules.loaded = false;
        await renderView(view);
      }, entry.view);
      await waitForApiQuiescence(pendingApi);
      const gate = await page.evaluate(() => ({
        unavailable: /\bUnavailable\b/.test(document.querySelector('#view-root')?.innerText || ''),
        message: (document.querySelector('#view-root')?.innerText || '').trim().slice(0, 160),
      }));
      result.entitlementGates.push({ owner, view: entry.view, file: entry.file, unavailable: gate.unavailable, chunkRequests: viewRequests.length - beforeChunks, message: gate.message });
      process.stdout.write(`entitlement ${owner}/${entry.view}: ${gate.unavailable && viewRequests.length === beforeChunks ? 'denied before chunk load' : 'FAILED'}\n`);
    }
    disabledModule = '';
    await page.evaluate(async () => { state.modules.loaded = false; await renderView('reports'); });
    await waitForApiQuiescence(pendingApi);

    for (const entry of entries) {
      pageErrors.length = 0;
      const beforeChunks = viewRequests.length;
      let state;
      let renderError = '';
      try {
        await page.evaluate(async ({ view }) => {
          if (view === 'doctype-table') currentDoctype = 'Vendor';
          if (view === 'extension-hook-log') currentExtensionHookLogId = 'bld041-audit-fixture';
          await renderView(view);
        }, entry);
        await waitForApiQuiescence(pendingApi);
        state = await page.evaluate(({ view, entrypoint }) => {
          const helpButton = view === 'rf-traceability' ? document.querySelector('.rf-help-btn') : document.getElementById('help-btn');
          return ({
            entrypointPresent: typeof window[entrypoint] === 'function',
            currentView,
            unavailable: /\bUnavailable\b/.test(document.querySelector('#view-root')?.innerText || ''),
            errorPanel: !!document.querySelector('#view-root #error-panel-retry-btn'),
            text: (document.querySelector('#view-root')?.innerText || '').trim().slice(0, 180),
            helpButtonVisible: !!helpButton && !!helpButton.getClientRects().length && getComputedStyle(helpButton).visibility !== 'hidden',
            helpButtonSelector: view === 'rf-traceability' ? '.rf-help-btn' : '#help-btn',
            printActions: [...(document.querySelector('#view-root')?.querySelectorAll('button,a,[role=button]') || [])]
              .filter(element => /print/i.test([element.textContent, element.getAttribute('aria-label'), element.title].join(' ')))
              .map(element => (element.textContent || element.getAttribute('aria-label') || element.title || '').trim().slice(0, 60)),
          });
        }, entry);
      } catch (error) {
        renderError = error.message;
        state = { entrypointPresent: false, currentView: '', unavailable: false, errorPanel: false, text: '', helpButtonVisible: false, helpButtonSelector: '', printActions: [] };
      }
      await waitForApiQuiescence(pendingApi);
      let help = { opened: false, mappedArticle: false, noArticle: false, failed: false, message: '' };
      if (entry.view === 'purchase-orders' && state.printActions.some(action => action === 'Print')) {
        await page.evaluate(() => {
          window.QZPrint = undefined; // prevent any QZ Tray / physical-device connection in this browser proof
          window.__bld041PrintCalls = 0;
          window.print = () => { window.__bld041PrintCalls++; };
        });
        const printButton = page.getByRole('button', { name: 'Print', exact: true }).first();
        const buttonCount = await page.getByRole('button', { name: 'Print', exact: true }).count();
        if (buttonCount) await printButton.click();
        await waitForApiQuiescence(pendingApi);
        const printed = await page.evaluate(() => ({
          printCalls: window.__bld041PrintCalls || 0,
          sheetRendered: !!document.querySelector('#invoice-print-area .po-print'),
        }));
        result.printChecks.push({ view: entry.view, buttonCount, browserFallback: true, ...printed });
      }
      if (state.helpButtonVisible) await page.locator(state.helpButtonSelector).click();
      else await page.evaluate(view => openHelpDrawer(view), entry.view);
      try {
        await page.locator('#kb-drawer-body').waitFor({ state: 'visible', timeout: 5000 });
        await waitForApiQuiescence(pendingApi);
        help = await page.locator('#kb-drawer-body').evaluate(element => ({
          opened: true,
          mappedArticle: !!element.querySelector('h4'),
          noArticle: /No help article is mapped/.test(element.innerText),
          failed: /could not be loaded/i.test(element.innerText),
          message: element.innerText.trim().slice(0, 120),
        }));
        await page.locator('#kb-drawer .modal-close').click();
      } catch (error) { help = { ...help, failed: true, message: error.message }; }
      const record = {
        view: entry.view, file: entry.file, entrypoint: entry.entrypoint, owners: entry.owners,
        rendered: state.entrypointPresent && state.currentView === entry.view && !state.unavailable && !state.errorPanel && state.text.length > 0,
        titleOrContent: state.text, helpButtonVisible: state.helpButtonVisible, help,
        printCodeInChunk: modulesWithPrintCode.has(entry.file), visiblePrintActions: state.printActions,
        lazyChunkRequestedOnRender: viewRequests.length > beforeChunks,
        renderError, pageErrors: [...pageErrors],
      };
      result.screens.push(record);
      fs.writeFileSync(path.join(evidence, 'bld041-lazy-view-audit.partial.json'), JSON.stringify(result, null, 2) + '\n');
      process.stdout.write(`screen ${result.screens.length}/${entries.length} ${entry.view}: ${record.rendered ? 'rendered' : 'FAILED'}, help=${help.mappedArticle ? 'mapped' : help.noArticle ? 'no-article' : help.failed ? 'failed' : 'unknown'}\n`);
    }
    // Every distinct chunk gets a fresh, one-shot network failure followed by
    // the real Try Again control. The shared loader owns this recovery path.
    const firstEntryByFile = new Map();
    for (const entry of entries) if (!firstEntryByFile.has(entry.file)) firstEntryByFile.set(entry.file, entry);
    for (const [moduleIndex, [file, entry]] of [...firstEntryByFile].entries()) {
      const errors = [];
      let failedOnce = false;
      let failedRequests = 0;
      let retryRequests = 0;
      const onPageError = error => errors.push(error.message);
      const routePattern = '**/' + file + '*';
      const routeHandler = async route => {
        if (!failedOnce) { failedOnce = true; failedRequests++; await route.abort(); }
        else { retryRequests++; await route.continue(); }
      };
      pageErrors.length = 0;
      page.on('pageerror', onPageError);
      // Force a unique import URL even though this same browser context loaded
      // the module above; ESM caches namespaces by URL, including query.
      const src = `/${file}?v=1`;
      await page.evaluate(({ src, attempt }) => {
        loadedViewModules.delete(src);
        viewModuleLoadAttempts.set(src, attempt);
      }, { src, attempt: 100 + moduleIndex * 2 });
      await page.route(routePattern, routeHandler);
      let retryMessage = '';
      let recovered = false;
      let recoveryError = '';
      try {
        await page.evaluate(({ view }) => {
          if (view === 'doctype-table') currentDoctype = 'Vendor';
          return renderView(view);
        }, entry);
        await page.locator('#error-panel-retry-btn').waitFor({ state: 'visible' });
        retryMessage = await page.locator('#view-root').innerText();
        await page.locator('#error-panel-retry-btn').click({ force: true });
        await page.waitForFunction(({ view, entrypoint }) => currentView === view && typeof window[entrypoint] === 'function' && !document.querySelector('#view-root #error-panel-retry-btn'), entry, { timeout: 12000 });
        await waitForApiQuiescence(pendingApi);
        recovered = true;
      } catch (error) { recoveryError = error.message; }
      await page.unroute(routePattern, routeHandler);
      const record = {
        file, firstView: entry.view, failedOnce, failedRequests, retryRequests,
        showedRetryPanel: /Something Went Wrong|Failed to load|could not be started/i.test(retryMessage),
        recovered, recoveryError, pageErrors: [...pageErrors],
      };
      result.moduleLoadRecovery.push(record);
      fs.writeFileSync(path.join(evidence, 'bld041-lazy-view-audit.partial.json'), JSON.stringify(result, null, 2) + '\n');
      process.stdout.write(`recovery ${file}: ${record.recovered ? 'passed' : 'FAILED'}\n`);
      page.off('pageerror', onPageError);
    }
    await context.close();

    // No screen chunk should be needed to show the unauthenticated login page.
    const anonymous = await browser.newPage();
    const anonymousChunks = [];
    anonymous.on('request', request => { if (/\/view-.*\.js(?:\?|$)/.test(request.url())) anonymousChunks.push(request.url()); });
    await anonymous.goto(origin.origin + '/#/view/pos', { waitUntil: 'domcontentloaded' });
    await anonymous.locator('#login-screen:not(.hidden)').waitFor({ state: 'visible' });
    result.unauthenticated = { loginVisible: true, screenChunkRequests: anonymousChunks.length };
    await anonymous.close();

    const failedScreens = result.screens.filter(screen => !screen.rendered || screen.pageErrors.length || screen.help.failed || !screen.help.opened || !screen.help.mappedArticle || !screen.helpButtonVisible);
    const failedRecovery = result.moduleLoadRecovery.filter(record => !record.failedOnce || !record.showedRetryPanel || !record.recovered || record.pageErrors.length);
    const failedGates = result.entitlementGates.filter(record => !record.unavailable || record.chunkRequests > 0);
    const failedPrintChecks = result.printChecks.filter(record => record.buttonCount < 1 || record.printCalls !== 1 || !record.sheetRendered);
    result.summary = {
      renderedScreens: result.screens.filter(screen => screen.rendered).length,
      helpOpened: result.screens.filter(screen => screen.help.opened && !screen.help.failed).length,
      helpButtonsVisible: result.screens.filter(screen => screen.helpButtonVisible).length,
      helpNoArticle: result.screens.filter(screen => screen.help.noArticle).map(screen => screen.view),
      screensWithPrintActions: result.screens.filter(screen => screen.visiblePrintActions.length).map(screen => ({ view: screen.view, actions: screen.visiblePrintActions })),
      printChecks: result.printChecks,
      recoveredChunks: result.moduleLoadRecovery.filter(record => record.recovered).length,
      entitlementGatesPassed: result.entitlementGates.filter(record => record.unavailable && record.chunkRequests === 0).length,
      failedScreens: failedScreens.map(screen => ({ view: screen.view, text: screen.titleOrContent, pageErrors: screen.pageErrors, help: screen.help })),
      failedRecovery: failedRecovery.map(record => record.file),
      failedGates: failedGates.map(record => ({ owner: record.owner, view: record.view, chunkRequests: record.chunkRequests })),
    };
    fs.writeFileSync(path.join(evidence, 'bld041-lazy-view-audit.json'), JSON.stringify(result, null, 2) + '\n');
    process.stdout.write(JSON.stringify(result.summary, null, 2) + '\n');
    if (failedScreens.length || failedRecovery.length || failedGates.length || failedPrintChecks.length || result.unauthenticated.screenChunkRequests || duplicateKeys.length || duplicateExports.length || staticImports.length || duplicateOwners.length) process.exitCode = 1;
  } finally {
    await browser.close();
  }
}

main().catch(error => { process.stderr.write(`${error.stack || error}\n`); process.exitCode = 1; });
