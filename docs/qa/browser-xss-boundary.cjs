'use strict';
// Focused DOM-XSS regression checks for the browser-boundary migration.
// Run: node docs/qa/browser-xss-boundary.cjs <absolute evidence directory>
const assert = require('node:assert/strict');
const fs = require('node:fs');
const http = require('node:http');
const path = require('node:path');
const { resolvePlaywright } = require('../guides/capture-screenshots.js');

const out = process.argv[2];
assert(out && path.isAbsolute(out), 'An explicit absolute evidence directory is required');
const repo = path.resolve(__dirname, '../..');
const publicRoot = path.join(repo, 'public');
const appSource = fs.readFileSync(path.join(publicRoot, 'app.js'), 'utf8').replace(/\r\n/g, '\n');

function sourceFunction(start, end) {
  const a = appSource.indexOf(start);
  const b = appSource.indexOf(end, a);
  assert(a >= 0 && b > a, `Missing production source boundary: ${start}`);
  return appSource.slice(a, b);
}

const escStart = appSource.indexOf('function escapeHTMLText(s) {');
const escEnd = appSource.indexOf('\n}\n', escStart) + 2;
assert(escStart >= 0 && escEnd > escStart, 'Production HTML escape helper missing');
const escapeHelper = appSource.slice(escStart, escEnd);
const csvPreview = sourceFunction(
  'window.handleBulkImportPreview = async function() {',
  '\n};\n\n// ---------------------------------------------------------------------------\n// Knowledge Center'
) + '\n};';

const server = http.createServer((req, res) => {
  const urlPath = new URL(req.url, 'http://localhost').pathname;
  if (urlPath === '/') {
    res.writeHead(200, { 'Content-Type': 'text/html; charset=utf-8' });
    res.end('<!doctype html><html><body><main id="root"></main></body></html>');
    return;
  }
  const file = path.resolve(publicRoot, `.${decodeURIComponent(urlPath)}`);
  if (!file.startsWith(publicRoot + path.sep)) {
    res.writeHead(404).end();
    return;
  }
  fs.readFile(file, (err, body) => {
    if (err) { res.writeHead(404).end(); return; }
    res.writeHead(200, { 'Content-Type': 'text/javascript; charset=utf-8' });
    res.end(body);
  });
});

(async () => {
  await new Promise(resolve => server.listen(0, '127.0.0.1', resolve));
  const address = server.address();
  const browser = await resolvePlaywright().chromium.launch();
  const results = [];
  try {
    const page = await browser.newPage();
    await page.addInitScript(() => {
      window.__xss = 0;
      window.__retryIds = [];
      window.__api = async (url) => {
        const payload = '<img src=x onerror="window.__xss++">';
        const attackID = `x');window.__xss++;('//`;
        const datasets = {
          '/api/v1/logs/audit': [{ user_id: payload, action: payload, details: payload, created_at: payload }],
          '/api/v1/logs/system': [{ log_id: attackID, severity: payload, module_source: payload, error_message: payload, created_at: payload, stack_trace: payload }],
          '/api/v1/integration/logs': [{ id: attackID, event_name: payload, status: 'Failed', attempts: 1, payload: { note: payload }, created_at: payload }],
          '/api/v1/jobs': { jobs: [] },
          '/api/v1/pim/reports/content-aging': [{ item_code: payload, issue: payload }],
          '/api/v1/doc/ProductFamily': [{ id: attackID, code: attackID, name: payload }],
          '/api/v1/pim/workbench': [{ item_code: attackID, name: payload, family: payload, status: payload, score: 50, missing_count: 1 }]
        };
        const key = Object.keys(datasets).find(candidate => url.startsWith(candidate));
        return { ok: true, headers: { get: () => null }, json: async () => key ? datasets[key] : [] };
      };
      window.apiFetch = window.__api;
      window.retryIntegrationEvent = async id => { window.__retryIds.push(id); };
      window.triggerPanicRecovery = () => {};
      window.showCustomConfirm = async () => false;
      window.showCustomAlert = async () => {};
      window.showApiError = async () => {};
      window.showToast = () => {};
      window.canCreateDoctype = () => false;
      window.currentDoctype = 'Item';
      window.currentPIMFamilyFilter = '';
      window.currentPIMSelectedItem = '';
      window.apiUpload = async () => window.__uploadResponse;
    });
    await page.goto(`http://127.0.0.1:${address.port}/`);
    await page.addScriptTag({ content: `${escapeHelper}\nwindow.__escapeReady = true;` });
    await page.addScriptTag({ content: csvPreview });
    await page.evaluate(async () => {
      window.__modulesAdmin = await import('/view-admin.js');
      window.__modulesPim = await import('/view-pim.js');
      window.__modulesReports = await import('/view-reports.js');
      window.retryIntegrationEvent = async id => { window.__retryIds.push(id); };
      await window.__modulesAdmin.renderLogHubView(document.getElementById('root'));
    });
    const payloadCheck = await page.evaluate(async () => {
      const payload = '<img src=x onerror="window.__xss++">';
      const attackID = `x');window.__xss++;('//`;
      document.querySelector('[data-tab="integration"]').click();
      const integrationMarkup = document.getElementById('log-hub-tab-content').innerHTML;
      const integrationHasImage = !!document.querySelector('#log-hub-tab-content img');
      const retry = document.querySelector('[data-integration-retry-id]');
      retry.click();
      document.querySelector('[data-tab="system"]').click();
      const systemHasImage = !!document.querySelector('#log-hub-tab-content img');
      const reportRoot = document.createElement('div');
      window.__modulesReports.renderReportCatalogResultTable(reportRoot, {
        id: 'sample', has_drill_down: false,
        columns: [{ key: 'message', label: payload }],
        rows: [{ message: payload }]
      }, {});
      document.body.append(reportRoot);
      const reportHasImage = !!reportRoot.querySelector('img');
      const pimRoot = document.createElement('div');
      document.body.append(pimRoot);
      await window.__modulesPim.renderPIMReportsTab(pimRoot);
      const pimHasImage = !!pimRoot.querySelector('img');
      const workbenchRoot = document.createElement('div');
      document.body.append(workbenchRoot);
      await window.__modulesPim.renderPIMWorkbenchTab(workbenchRoot);
      const workbenchHasImage = !!workbenchRoot.querySelector('img');
      const familyOption = workbenchRoot.querySelector('#pim-family-filter option[value]:not([value=""])');
      return {
        integrationHasImage, systemHasImage, reportHasImage, pimHasImage, workbenchHasImage,
        integrationPayloadVisibleAsText: integrationMarkup.includes('&lt;img'),
        retryAttributeRemoved: !retry.hasAttribute('onclick'),
        retryIDWasPassedAsData: retry.dataset.integrationRetryId === attackID,
        reportPayloadVisibleAsText: reportRoot.textContent.includes(payload),
        pimPayloadVisibleAsText: pimRoot.textContent.includes(payload),
        importedMasterPayloadVisibleAsText: workbenchRoot.textContent.includes(payload),
        importedMasterValueKeptAsData: familyOption.value === attackID
      };
    });
    assert.deepEqual(payloadCheck, {
      integrationHasImage: false, systemHasImage: false, reportHasImage: false, pimHasImage: false, workbenchHasImage: false,
      integrationPayloadVisibleAsText: true, retryAttributeRemoved: true, retryIDWasPassedAsData: true,
      reportPayloadVisibleAsText: true, pimPayloadVisibleAsText: true,
      importedMasterPayloadVisibleAsText: true, importedMasterValueKeptAsData: true
    });
    const retryIDs = await page.evaluate(() => window.__retryIds);
    assert.deepEqual(retryIDs, [`x');window.__xss++;('//`]);
    assert.equal(await page.evaluate(() => window.__xss), 0, 'A hostile log/report payload executed');
    results.push({ fixture: 'integration, audit/system logs, report rows, PIM reports', ...payloadCheck, retryIDs });

    await page.evaluate(() => {
      document.getElementById('root').innerHTML = '<input id="import-file-input" type="file"><div id="import-result-summary"></div>';
      window.__uploadResponse = {
        ok: true,
        json: async () => ({ total_rows: 1, created_ids: [], updated_ids: [], failed_rows: 1,
          errors: [{ row_number: 2, message: '<img src=x onerror="window.__xss++">' }] })
      };
    });
    await page.locator('#import-file-input').setInputFiles({ name: 'fixture.csv', mimeType: 'text/csv', buffer: Buffer.from('item\nA1\n') });
    await page.evaluate(() => window.handleBulkImportPreview());
    const csvCheck = await page.evaluate(() => ({
      hasImage: !!document.querySelector('#import-result-summary img'),
      payloadIsText: document.getElementById('import-result-summary').textContent.includes('<img src=x'),
      xss: window.__xss
    }));
    assert.deepEqual(csvCheck, { hasImage: false, payloadIsText: true, xss: 0 });
    assert.match(appSource, /encodeURIComponent\(result\.import_job_id\).*rel="noopener"/s, 'CSV error attachment URL must be encoded and isolated');
    const pimSource = fs.readFileSync(path.join(publicRoot, 'view-pim.js'), 'utf8');
    assert.match(pimSource, /escapeHTMLText\(o\.filename\)/, 'Imported attachment names must remain text');
    results.push({ fixture: 'CSV preview errors and attachment-link URL contract', ...csvCheck });

    fs.mkdirSync(out, { recursive: true });
    fs.writeFileSync(path.join(out, 'browser-xss-boundary.json'), JSON.stringify(results, null, 2) + '\n');
    console.log(JSON.stringify(results));
  } finally {
    await browser.close();
    await new Promise(resolve => server.close(resolve));
  }
})().catch(error => { console.error(error); process.exitCode = 1; });
