'use strict';
// Real Chromium regression. No mocks. Department writes require an explicit
// opt-in and are permitted only on the user's disposable loopback instance.
// ERP_TOKEN_FILE=<file> ERP_AUDIT_OUT=<scratch dir> node <this file>
// ERP_AUDIT_WRITE_FIXTURE=1 additionally verifies actual create/update requests.
const fs = require('node:fs');
const path = require('node:path');
const assert = require('node:assert/strict');
const {resolvePlaywright} = require('../guides/capture-screenshots.js');
const origin = process.env.ERP_BASE_URL || 'http://127.0.0.1:8101';
const token = fs.readFileSync(process.env.ERP_TOKEN_FILE, 'utf8').trim();
const out = process.env.ERP_AUDIT_OUT;
if (!out) throw Error('ERP_AUDIT_OUT must name a session scratch directory');
const writes = process.env.ERP_AUDIT_WRITE_FIXTURE === '1';
const filter = process.env.ERP_AUDIT_CHECK_FILTER ? new RegExp(process.env.ERP_AUDIT_CHECK_FILTER) : null;
if (writes && origin !== 'http://127.0.0.1:8101') throw Error('Fixture writes require disposable loopback port 8101');
fs.mkdirSync(out, {recursive:true});
const result = {startedAt:new Date().toISOString(), origin, fixtureWrites:writes, filter:filter?.source, checks:[], blockedWrites:[]};
let nextRequest = 0;
const save = () => fs.writeFileSync(path.join(out, 'stage55-results.json'), JSON.stringify(result, null, 2));

(async () => {
  const browser = await resolvePlaywright().chromium.launch({headless:true});
  result.browser = browser.version();
  async function fresh(view = 'home', sessionToken = token) {
    const context = await browser.newContext({viewport:{width:1280,height:900}, reducedMotion:'reduce'});
    await context.addInitScript(t => {
      localStorage.setItem('erp_token', t);
      localStorage.setItem('erp_tenant_id', 'default');
    }, sessionToken);
    const page = await context.newPage();
    page.setDefaultTimeout(30000);
    const pending = new Set(), errors = [], requests = [], apiErrors = [];
    await page.route('**/api/**', async route => {
      const request = route.request(), url = new URL(request.url());
      const write = !['GET','HEAD','OPTIONS'].includes(request.method());
      const departmentSequence = url.pathname === '/api/v1/sequence' && request.postDataJSON()?.doc_type === 'Department';
      if (write && !(writes && request.method() === 'POST' && (departmentSequence || /^\/api\/v1\/doc\/Department(?:\/|$)/.test(url.pathname)))) {
        result.blockedWrites.push({method:request.method(), path:url.pathname});
        return route.abort();
      }
      const at = Math.max(Date.now(), nextRequest);
      nextRequest = at + 1050; // stay inside the real API rate limit
      if (at > Date.now()) await new Promise(resolve => setTimeout(resolve, at - Date.now()));
      await route.continue();
    });
    page.on('request', r => {
      requests.push({method:r.method(), path:new URL(r.url()).pathname});
      if (r.url().includes('/api/')) pending.add(r);
    });
    page.on('requestfinished', r => pending.delete(r));
    page.on('requestfailed', r => pending.delete(r));
    page.on('pageerror', e => errors.push(e.message));
    page.on('response', r => {if (r.url().includes('/api/') && r.status() >= 400) apiErrors.push({path:new URL(r.url()).pathname,status:r.status()});});
    async function idle() {
      const deadline = Date.now() + 90000;
      let quiet = Date.now();
      while (Date.now() < deadline) {
        if (pending.size) quiet = Date.now();
        if (!pending.size && Date.now() - quiet > 350) {
          await page.evaluate(() => new Promise(resolve => requestAnimationFrame(() => requestAnimationFrame(resolve))));
          return;
        }
        await new Promise(resolve => setTimeout(resolve, 50));
      }
      throw Error('API did not settle');
    }
    await page.goto(origin + '/#/view/' + view);
    await page.locator('#app-root:not(.hidden)').waitFor();
    await idle();
    return {context,page,idle,errors,requests,apiErrors};
  }
  async function check(name, fn) {
    if (filter && !filter.test(name)) return;
    const record = {name};
    try {record.evidence = await fn(); record.passed = true;}
    catch (error) {record.passed = false; record.error = error.stack;}
    result.checks.push(record); save(); console.log(JSON.stringify(record));
  }
  async function doc(page, idle, type) {
    await page.evaluate(t => {location.hash = '#/setup/' + t;}, type);
    await page.waitForFunction(t => currentDoctype === t && !!document.querySelector('#doc-create-button'), type);
    await idle();
  }
  async function openNew(page, idle) {
    await page.locator('#doc-create-button').click();
    await page.locator('#dynamic-modal.open').waitFor();
    await idle();
    assert.match(await page.locator('#dynamic-modal-title').innerText(), /^New /);
    assert.equal(await page.evaluate(() => editingDocID), null);
  }
  try {
    for (const view of ['finance','reports','stickers','hr','manufacturing','pim','audit-logs']) await check('secondary tabs from fresh '+view, async () => {
      const s = await fresh(view);
      try {
        const selector = 'button[data-finance-tab],button[data-report-tab],button[data-sticker-tab],button[data-hr-tab],button[data-mfg-tab],button[data-pim-tab],button.log-hub-tab[data-tab]';
        const tabs = await s.page.locator(selector).evaluateAll(els => els.map(el => {
          const attr = [...el.attributes].find(a=>/^data-(?:(?:finance|report|sticker|hr|mfg|pim)-)?tab$/.test(a.name));
          return {attr:attr.name,value:attr.value};
        }));
        assert.ok(tabs.length>0);
        for (const tab of tabs) {
          await s.page.locator('button['+tab.attr+'="'+tab.value+'"]').click();
          await s.idle();
          assert.equal(await s.page.locator('#view-root #error-panel-retry-btn').count(),0,view+'/'+tab.value);
          assert.deepEqual(s.errors,[],view+'/'+tab.value);
        }
        assert.deepEqual(s.apiErrors,[]);
        return {freshContextPerParent:true,tabs:tabs.map(t=>t.value),pageErrors:[],apiErrors:[]};
      } finally {await s.context.close();}
    });
    for (const [view, attr, tabs] of [
      ['hr','data-hr-tab',{roster:'ShiftAssignment',onboarding:'OnboardingChecklist',appraisals:'Appraisal',training:'TrainingRecord',grievances:'Grievance'}],
      ['manufacturing','data-mfg-tab',{quality:'QualityInspection',subcontracting:'SubcontractOrder'}]
    ]) for (const [tab,type] of Object.entries(tabs)) await check('cold '+view+'/'+tab, async () => {
      const s = await fresh(view);
      try {
        assert.equal(await s.page.evaluate(() => typeof window.renderDocTableView), 'undefined');
        await s.page.locator('['+attr+'="'+tab+'"]').click();
        await s.page.locator('#doc-create-button').waitFor(); await s.idle();
        assert.equal(await s.page.evaluate(() => currentDoctype), type);
        await openNew(s.page,s.idle); await s.page.keyboard.press('Escape');
        await s.page.evaluate(() => renderView('home')); await s.idle();
        await s.page.evaluate(v => renderView(v), view); await s.page.locator('#doc-create-button').waitFor(); await s.idle();
        assert.equal(await s.page.evaluate(() => currentDoctype), type);
        assert.deepEqual(s.errors, []);
        return {type, coldEntry:true, newAndEscape:true, navigateAwayAndBack:true, apiErrors:s.apiErrors};
      } finally {await s.context.close();}
    });
    await check('nested chunk failure then live-parent Retry', async () => {
      const s = await fresh('hr');
      try {
        let attempts = 0;
        await s.page.route('**/view-documents.js*', route => ++attempts === 1 ? route.abort() : route.continue());
        await s.page.locator('[data-hr-tab="roster"]').click();
        await s.page.locator('#error-panel-retry-btn').waitFor();
        await s.page.locator('#error-panel-retry-btn').click();
        await s.page.locator('#doc-create-button').waitFor(); await s.idle();
        assert.equal(await s.page.evaluate(() => currentView), 'hr');
        assert.equal(attempts, 2); assert.deepEqual(s.errors, []);
        return {attempts,recoveredView:'hr'};
      } finally {await s.context.close();}
    });
    await check('wave input and phone input semantics', async () => {
      const s = await fresh('mobile-picking');
      try {
        const input = s.page.locator('#mobile-pick-wave-id');
        await input.pressSequentially('WAVE-0001'); await input.press('Tab');
        assert.equal(await input.inputValue(), 'WAVE-0001');
        assert.notEqual(await input.getAttribute('inputmode'), 'tel');
        assert.equal(await s.page.locator('#view-root .phone-rule-hint').count(), 0);
        const matrix = await s.page.evaluate(() => ({wave:isPhoneFieldName('mobile-pick-wave-id'),lot:isPhoneFieldName('lot_no'),serial:isPhoneFieldName('serial_no'),barcode:isPhoneFieldName('barcode'),phone:isPhoneFieldName('customer_phone'),derived:isPhoneFieldName('phone_country')}));
        assert.deepEqual(matrix,{wave:false,lot:false,serial:false,barcode:false,phone:true,derived:false});
        await s.page.screenshot({path:path.join(out,'wave-preserved.png')});
        await s.page.setViewportSize({width:390,height:844});
        await input.fill(''); await input.pressSequentially('WAVE-0001'); await input.press('Tab');
        assert.equal(await input.inputValue(),'WAVE-0001');
        await s.page.screenshot({path:path.join(out,'wave-preserved-mobile.png')});
        await doc(s.page,s.idle,'Customer'); await openNew(s.page,s.idle);
        const phone = s.page.locator('#dynamic-modal [name="phone"]');
        await phone.pressSequentially('+91 (98765) 43210'); await phone.press('Tab');
        assert.equal(await phone.inputValue(), '+919876543210');
        assert.equal(await phone.getAttribute('inputmode'), 'tel');
        return {matrix,wave:'WAVE-0001',mobileWave:'WAVE-0001',phone:'+919876543210'};
      } finally {await s.context.close();}
    });
    await check('New defaults, field kinds, keyboard containment and Escape', async () => {
      const s = await fresh();
      const inspected = [];
      try {
        for (const type of ['Department','Item','Vendor','PurchaseRequisition','Attendance','JournalVoucher','InspectionPlan','ReportFilterPreset','PIMImportTemplate']) {
          await doc(s.page,s.idle,type); await openNew(s.page,s.idle);
          assert.equal(await s.page.evaluate(() => document.getElementById('dynamic-modal').contains(document.activeElement)), true);
          if (type === 'Item') assert.equal(await s.page.locator('#dynamic-modal [name="type"]').inputValue(), '');
          const generated = await s.page.locator('#dynamic-modal input[readonly]').evaluateAll(els => els.map(e=>({name:e.name,value:e.value,required:e.required,placeholder:e.placeholder})));
          assert.ok(generated.every(f=>!f.required));
          if (type === 'Department') {
            for (const key of ['Tab','Shift+Tab']) for (let i=0;i<18;i++) {
              await s.page.keyboard.press(key);
              assert.equal(await s.page.evaluate(() => document.getElementById('dynamic-modal').contains(document.activeElement)), true);
            }
            assert.equal(await s.page.locator('#dynamic-modal [role="dialog"]').getAttribute('aria-labelledby'),'dynamic-modal-title');
          }
          inspected.push({type,title:await s.page.locator('#dynamic-modal-title').innerText(),generated});
          await s.page.keyboard.press('Escape');
          assert.equal(await s.page.locator('#dynamic-modal').evaluate(e=>e.classList.contains('open')),false);
          assert.equal(await s.page.evaluate(()=>document.activeElement.id),'doc-create-button');
        }
        assert.deepEqual(s.errors, []); return inspected;
      } finally {await s.context.close();}
    });
    await check('password reset history uses supported Users action', async () => {
      const s = await fresh();
      try {
        await s.page.evaluate(() => {location.hash='#/setup/PasswordResetRequest';});
        await s.page.locator('#doc-reset-users-button').waitFor(); await s.idle();
        assert.equal(await s.page.locator('#doc-create-button,#doc-import-button,[data-doc-action="edit"],[data-doc-action="delete"]').count(),0);
        await s.page.locator('#doc-reset-users-button').click();
        await s.page.locator('[data-act="resetUserPassword"]').first().waitFor(); await s.idle();
        await s.page.locator('[data-act="resetUserPassword"]').first().click();
        await s.page.locator('#custom-dialog-container:not(.hidden)').waitFor();
        assert.match(await s.page.locator('#custom-dialog-title').innerText(),/Reset Password/);
        await s.page.locator('#custom-dialog-cancel-btn').click();
        await s.page.locator('#custom-dialog-container').waitFor({state:'hidden'});
        assert.equal(s.requests.filter(r=>r.path==='/api/v1/doc/User').length,0);
        assert.equal(s.requests.filter(r=>r.method==='POST'&&r.path.includes('/reset-password')).length,0);
        assert.deepEqual(s.errors, []);
        return {usersLoaded:true,resetPromptOpened:true,promptCancelled:true,genericUserLookup:0,resetWrites:0};
      } finally {await s.context.close();}
    });
    if (process.env.ERP_RESTRICTED_TOKEN_FILE) await check('restricted role cannot list reset targets', async () => {
      const restricted = fs.readFileSync(process.env.ERP_RESTRICTED_TOKEN_FILE,'utf8').trim();
      const s = await fresh('home',restricted);
      try {
        const access = await s.page.evaluate(async () => {
          const me = await apiFetch('/api/v1/me'); const identity = await me.json();
          const users = await apiFetch('/api/v1/admin/users');
          return {identityStatus:me.status,role:identity.role,usersStatus:users.status,usersMenuVisible:isMenuRuleVisible(MENU_PERMISSION_MAP['menu-users'])};
        });
        assert.equal(access.identityStatus,200); assert.equal(access.usersStatus,403);
        assert.equal(access.usersMenuVisible,false); return access;
      } finally {await s.context.close();}
    });
    if (writes) await check('real Department create, edit, and new-after-edit', async () => {
      const s = await fresh();
      try {
        const name = 'UX55-' + Date.now();
        await doc(s.page,s.idle,'Department'); await openNew(s.page,s.idle);
        await s.page.locator('#dynamic-modal [name="name"]').fill(name);
        await s.page.locator('#dynamic-modal [name="status"]').selectOption('Active');
        const created = s.page.waitForResponse(r=>r.request().method()==='POST' && new URL(r.url()).pathname==='/api/v1/doc/Department');
        await s.page.locator('#dynamic-modal button[type="submit"]').click();
        const response = await created; assert.ok(response.ok(),await response.text());
        const payload = await response.json();
        await s.idle(); await s.page.locator('#dynamic-modal.open').waitFor({state:'hidden'});
        const search = s.page.locator('#doc-table-search'); await search.fill(name);
        const row = s.page.locator('tr').filter({hasText:name});
        await row.locator('[data-doc-action="edit"]').waitFor(); await s.idle();
        const id = await row.locator('[data-doc-action="edit"]').getAttribute('data-doc-id');
        await row.locator('[data-doc-action="edit"]').click(); await s.page.locator('#dynamic-modal.open').waitFor(); await s.idle();
        assert.match(await s.page.locator('#dynamic-modal-title').innerText(),/^Edit /);
        assert.equal(await s.page.locator('#dynamic-modal [name="name"]').inputValue(),name);
        const corrected = name + ' corrected'; await s.page.locator('#dynamic-modal [name="name"]').fill(corrected);
        const updated = s.page.waitForResponse(r=>r.request().method()==='POST' && new URL(r.url()).pathname==='/api/v1/doc/Department/'+encodeURIComponent(id));
        await s.page.locator('#dynamic-modal button[type="submit"]').click();
        const update = await updated; assert.ok(update.ok(),await update.text());
        await s.idle(); await s.page.locator('#dynamic-modal.open').waitFor({state:'hidden'});
        const stored = await s.page.evaluate(async id => {const r=await apiFetch('/api/v1/doc/Department/'+encodeURIComponent(id));return r.json();},id);
        assert.equal(stored.name,corrected); assert.equal(stored.id,id);
        await openNew(s.page,s.idle); assert.equal(await s.page.locator('#dynamic-modal [name="name"]').inputValue(),'');
        await s.page.keyboard.press('Escape'); assert.deepEqual(s.errors, []);
        return {id,createStatus:response.status(),editStatus:update.status(),createdPayload:payload,newAfterEdit:true,fixtureRetained:true};
      } finally {await s.context.close();}
    });
  } finally {await browser.close();}
  result.finishedAt = new Date().toISOString(); save();
  if (result.checks.some(c=>!c.passed)) process.exitCode=1;
})().catch(error=>{result.fatal=error.stack;save();console.error(error);process.exitCode=1;});
