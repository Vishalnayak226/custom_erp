'use strict';
const { test } = require('node:test');
const assert = require('node:assert/strict');
const fs = require('node:fs');
const os = require('node:os');
const path = require('node:path');
const http = require('node:http');
const { capture, parseArgs, resolvePlaywright } = require('./capture-screenshots');

test('capture options require explicit scope and reject unknown shots and credentials', () => {
  const args = ['--out', 'unused', '--storage-state', 'session.json', '--release', 'test-release', '--role', 'Cashier', '--tenant', 'fixture', '--fixture', 'synthetic-v1'];
  assert.equal(parseArgs(args).role, 'Cashier');
  assert.equal(parseArgs(args).intervalMs, 30000);
  assert.throws(() => parseArgs([...args, '--interval-ms', '-1']), /interval/);
  assert.throws(() => parseArgs([]), /required/);
  assert.throws(() => parseArgs([...args, '--only', 'typo']), /unknown/);
  assert.throws(() => parseArgs([...args, '--base', 'http://user:password@localhost']), /credentials/);
  assert.throws(() => parseArgs([...args, '--theme', 'unknown']), /Theme/);
});

test('real browser captures only complete clean sets and does not leak session data', async t => {
  const playwright = resolvePlaywright();
  const temp = fs.mkdtempSync(path.join(os.tmpdir(), 'erp-capture-test-'));
  const server = http.createServer((req, res) => {
    if (req.url === '/api/v1/me') {
      if (req.headers.authorization !== 'Bearer capture-fixture-canary' || req.headers['x-tenant-id'] !== 'fixture') { res.writeHead(401).end(); return; }
      res.setHeader('Content-Type', 'application/json');
      res.end(JSON.stringify({ role: 'Cashier', username: 'capture-operator' })); return;
    }
    if (req.url === '/failure') { res.writeHead(401).end(); return; }
    res.setHeader('Content-Type', 'text/html');
    res.end(`<!doctype html><html><head><link rel="icon" href="data:,"></head><body><main id="view-root"><h1>Fixture transaction list</h1><button>Refresh records</button></main><script>
      const view = location.hash.split('/').pop();
      if (view === 'overlay') document.body.insertAdjacentHTML('beforeend', '<div role="dialog">Unexpected dialog</div>');
      if (view === 'denied') document.querySelector('main').textContent = 'Access denied to this screen';
      if (view === 'console') console.error('fixture error');
      if (view === 'exception') setTimeout(() => { throw new Error('fixture error'); }, 20);
      if (view === 'network') fetch('/failure');
      if (view === 'clipping') document.querySelector('main').innerHTML = '<h1>Fixture clipped screen</h1><div style="width:60px;overflow:hidden"><button style="width:250px">Clipped control</button></div>';
      if (view === 'scrolling') document.querySelector('main').innerHTML = '<h1>Fixture scrollable table</h1><div style="width:60px;overflow:auto"><button style="width:250px">Scrollable control</button></div>';
      if (view === 'fallback') location.hash = '#/view/reports';
      if (view === 'modal') window.openDynamicModal = async () => {
        document.body.insertAdjacentHTML('beforeend', '<div id="dynamic-modal" class="modal-overlay open" role="dialog" style="opacity:0;transition:opacity .15s"><h2>Fixture editor</h2><button>Save draft</button></div>');
        setTimeout(() => document.getElementById('dynamic-modal').style.opacity = '1', 30);
      };
    </script></body></html>`);
  });
  await new Promise(resolve => server.listen(0, '127.0.0.1', resolve));
  t.after(async () => {
    await new Promise(resolve => server.close(resolve));
    if (path.dirname(temp) === path.resolve(os.tmpdir()) && path.basename(temp).startsWith('erp-capture-test-')) fs.rmSync(temp, { recursive: true, force: true });
  });
  const base = `http://127.0.0.1:${server.address().port}`;
  const options = {
    base, out: path.join(temp, 'success'), timeout: 5000, release: 'test-release', role: 'Cashier', tenant: 'fixture', fixture: 'synthetic-v1', theme: 'light', locale: 'en-IN', viewport: { width: 960, height: 720 },
    storageState: { cookies: [], origins: [{ origin: base, localStorage: [
      { name: 'erp_token', value: 'capture-fixture-canary' }, { name: 'erp_role', value: 'Cashier' },
      { name: 'erp_username', value: 'capture-operator' }, { name: 'erp_tenant_id', value: 'fixture' },
    ] }] }, shots: [{ id: 'clean', view: 'reports', caption: 'Fixture records' }, { id: 'scrolling', view: 'scrolling', caption: 'Scrollable content' }],
  };
  const manifest = await capture(options, playwright);
  assert.equal(manifest.screenshots.length, 2);
  assert.equal(manifest.status, 'pending-human-review');
  const modal = await capture({ ...options, out: path.join(temp, 'modal'), shots: [{ id: 'editor', view: 'modal', after: 'modal', allowedOverlay: '#dynamic-modal', caption: 'Editor' }] }, playwright);
  assert.equal(modal.screenshots.length, 1);
  const saved = fs.readFileSync(path.join(options.out, 'manifest.json'), 'utf8');
  for (const secret of ['capture-fixture-canary', 'capture-operator', base]) assert.ok(!saved.includes(secret));
  const before = fs.readFileSync(path.join(options.out, 'clean.png'));
  await assert.rejects(capture(options, playwright), /already exists/);
  assert.deepEqual(fs.readFileSync(path.join(options.out, 'clean.png')), before);
  for (const view of ['overlay', 'denied', 'console', 'exception', 'network', 'clipping', 'fallback']) {
    const out = path.join(temp, view);
    await assert.rejects(capture({ ...options, out, shots: [options.shots[0], { id: view, view, caption: view }] }, playwright));
    assert.equal(fs.existsSync(out), false, `${view} published a partial set`);
    assert.equal(fs.readdirSync(temp).some(name => name.startsWith('.erp-shots-')), false, 'temporary set was not cleaned');
  }
  await assert.rejects(capture({ ...options, out: path.join(temp, 'wrong-role'), role: 'Super Admin' }, playwright), /identity/);
});
