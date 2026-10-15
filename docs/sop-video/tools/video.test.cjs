'use strict';
const test = require('node:test');
const assert = require('node:assert/strict');
const fs = require('node:fs');
const http = require('node:http');
const path = require('node:path');
const { validate } = require('./validate.cjs');
const { server } = require('./serve.cjs');
const root = path.resolve(__dirname, '..');
const project = JSON.parse(fs.readFileSync(path.join(root, 'project.json'), 'utf8'));

test('manifest has valid, replaceable segment references', () => {
  assert.deepEqual(validate(project), []);
  const byId = id => project.chapters.find(c => c.id === id).segments;
  for (const id of ['vendor_setup', 'item_setup', 'location_check', 'po_create', 'po_approval']) {
    assert.ok(byId('purchase_order').includes(id));
    assert.ok(byId('goods_receipt').includes(id), `${id} must be reused in the GRN chapter`);
  }
  assert.ok(byId('goods_receipt').indexOf('vendor_setup') < byId('goods_receipt').indexOf('grn_post'));
  assert.ok(project.segments.some(s => s.status === 'ready'));
});

test('player serves seekable clips and distinguishes ready from pending chapters', async () => {
  await new Promise(resolve => server.listen(0, '127.0.0.1', resolve));
  const origin = `http://127.0.0.1:${server.address().port}`;
  const playwright = require(require.resolve('playwright', { paths: [process.cwd(), process.env.USERPROFILE || process.env.HOME || ''] }));
  const browser = await playwright.chromium.launch({ headless: true });
  try {
    const response = await new Promise((resolve, reject) => http.get(origin + '/media/department-setup.webm', { headers: { Range: 'bytes=0-99' } }, res => {
      const chunks = []; res.on('data', c => chunks.push(c)); res.on('end', () => resolve({ status: res.statusCode, length: Buffer.concat(chunks).length }));
    }).on('error', reject));
    assert.deepEqual(response, { status: 206, length: 100 });
    const page = await browser.newPage();
    await page.goto(origin + '/', { waitUntil: 'domcontentloaded' });
    await page.locator('.chapter').first().waitFor();
    // Derived from the manifest, not fixed counts, so the assertions hold as
    // chapters are added and segments move from pending to ready.
    assert.equal(await page.locator('.chapter').count(), project.chapters.length);
    assert.equal(await page.locator('#start').isEnabled(), true);
    await page.locator('#start').click();
    await page.waitForFunction(() => document.querySelector('video').src.endsWith('department-setup.webm'));
    const isReady = id => project.segments.find(s => s.id === id).status === 'ready';
    for (const c of project.chapters) {
      await page.locator(`.chapter[data-id="${c.id}"]`).click();
      assert.equal(await page.locator('#start').isEnabled(), c.segments.every(isReady), `${c.id} start`);
      assert.equal(await page.locator('#steps button').count(), c.segments.length, `${c.id} steps`);
      for (const [i, id] of c.segments.entries()) {
        assert.equal(await page.locator('#steps button').nth(i).isEnabled(), isReady(id), `${c.id} step ${id}`);
      }
    }
    await page.close();
  } finally { await browser.close(); await new Promise(resolve => server.close(resolve)); }
});
