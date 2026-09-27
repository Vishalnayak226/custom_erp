'use strict';
// Optional local Playwright audit tooling; never bundled into the ERP runtime.
const fs = require('node:fs');
const path = require('node:path');
const { performance } = require('node:perf_hooks');
const [source, evidence, base = 'http://127.0.0.1:8178'] = process.argv.slice(2);
if (!source || !evidence || !/^http:\/\/(127\.0\.0\.1|localhost):8178$/.test(base)) throw Error('Explicit snapshot, evidence, and local scratch origin required.');
const { resolvePlaywright } = require(path.join(source, 'docs/guides/capture-screenshots.js'));
const app = fs.readFileSync(path.join(source, 'public/app.js'), 'utf8');
const dispatcher = app.slice(app.indexOf('async function renderViewContent('), app.indexOf('// Translate labels in DOM'));
const views = [...new Set([...dispatcher.matchAll(/view === '([^']+)'/g)].map(m => m[1]))].filter(v => !['doctype-table', 'dynamic-labels'].includes(v));
const token = fs.readFileSync(path.join(evidence, 'ui-token.txt'), 'utf8').replace(/^\uFEFF/, '').trim();
const results = [];
function inspect() {
  const visible = e => e.getClientRects().length && getComputedStyle(e).visibility !== 'hidden' && getComputedStyle(e).display !== 'none';
  const label = e => (e.getAttribute('aria-label') || (e.getAttribute('aria-labelledby') || '').split(/\s+/).map(id => document.getElementById(id)?.textContent || '').join(' ') || [...(e.labels || [])].map(l => l.textContent).join(' ') || (e.matches('button,a') ? e.textContent : '') || e.getAttribute('title') || '').trim();
  const controls = [...document.querySelectorAll('#view-root input:not([type=hidden]),#view-root select,#view-root textarea,#view-root button')].filter(visible);
  const unnamed = controls.filter(e => !label(e)).map(e => ({ tag: e.tagName, id: e.id, type: e.type, placeholder: e.getAttribute('placeholder') }));
  const small = controls.filter(e => { const r=e.getBoundingClientRect(); return r.width<24 || r.height<24; }).map(e=>({label:label(e).slice(0,60),tag:e.tagName}));
  const clipped = [...document.querySelectorAll('#view-root h1,#view-root h2,#view-root .page-header,#view-root .form-group')].filter(visible).filter(e=> {const r=e.getBoundingClientRect(); return r.left < -1 || r.right>innerWidth+1;}).map(e=>({tag:e.tagName,text:e.textContent.trim().slice(0,70)}));
  return { title: document.querySelector('#view-root h1,#view-root h2,.page-title')?.textContent?.trim(),
    text: document.querySelector('#view-root')?.innerText.slice(0,350), viewport:innerWidth,
    documentWidth:document.documentElement.scrollWidth, unnamed, small, clipped,
    horizontalScrollRegions: [...document.querySelectorAll('#view-root *')].filter(e=>visible(e)&&e.scrollWidth>e.clientWidth+2&&['auto','scroll'].includes(getComputedStyle(e).overflowX)).map(e=>({tag:e.tagName,tabIndex:e.tabIndex,role:e.getAttribute('role')})),
    rootVisible:!!document.querySelector('#view-root')&&!!visible(document.querySelector('#view-root')),
    focused: document.activeElement?.outerHTML.slice(0,180), lcp:window.__auditLCP || null };
}
(async()=>{
  const browser = await resolvePlaywright().chromium.launch();
  try {
    for (const config of [{id:'desktop-light',width:1440,height:900,theme:'light'},{id:'mobile-dark',width:390,height:844,theme:'dark'},{id:'tablet-keyboard',width:768,height:1024,theme:'light'}]) {
      const context=await browser.newContext({viewport:{width:config.width,height:config.height},colorScheme:config.theme,reducedMotion:'reduce',locale:'en-IN'});
      await context.addInitScript(({token,theme})=>{
        localStorage.setItem('erp_token',token);localStorage.setItem('erp_username','admin');localStorage.setItem('erp_role','Super Admin');localStorage.setItem('erp_tenant_id','default');localStorage.setItem('erp-theme',theme);
        new PerformanceObserver(list=>{window.__auditLCP=list.getEntries().at(-1)?.startTime;}).observe({type:'largest-contentful-paint',buffered:true});
      },{token,theme:config.theme});
      const page=await context.newPage(); page.setDefaultTimeout(12000);
      let errors=[];
      page.on('pageerror',e=>errors.push({kind:'exception',message:e.message}));
      page.on('response',r=>{if(r.status()>=400) errors.push({kind:'http',status:r.status(),url:new URL(r.url()).pathname});});
      await page.goto(base+'/#/view/reports',{waitUntil:'networkidle'});
      const identity=await page.evaluate(async()=>{const r=await fetch('/api/v1/me',{headers:{Authorization:'Bearer '+localStorage.getItem('erp_token')}});const j=await r.json();return {status:r.status,role:j.role,username:j.username};});
      if(identity.status!==200 || identity.role!=='Super Admin') throw Error('Scratch identity verification failed: '+JSON.stringify(identity));
      for(const view of views){
        errors=[];const start=performance.now();let error;
        try{
          await page.evaluate(view=>{location.hash='#/view/'+view;},view);
          await page.waitForTimeout(350);
          await page.waitForFunction(()=>!document.querySelector('#view-root .view-loading'));
          await page.waitForLoadState('networkidle',{timeout:10000});
        }catch(e){error=e.message.split('\n')[0];}
        const metrics=await page.evaluate(inspect);
        const record={context:config.id,view,url:page.url(),settled_ms:Math.round(performance.now()-start),error,errors:[...errors],...metrics};
        if(['pos','inventory','reports','configuration','pim','purchase-orders','rf-traceability','finance'].includes(view)){
          record.screenshot=config.id+'-'+view+'.png';await page.screenshot({path:path.join(evidence,record.screenshot),animations:'disabled'});
        }
        results.push(record);fs.writeFileSync(path.join(evidence,'browser-results.json'),JSON.stringify({browser:browser.version(),views,results},null,2));
        console.log(JSON.stringify({context:config.id,view,error,errorCount:errors.length,overflow:metrics.documentWidth>config.width,unnamed:metrics.unnamed.length,clipped:metrics.clipped.length}));
        // Keep each screen's normal requests beneath the real limiter; do not disable it.
        await page.waitForTimeout(1800);
      }
      await page.evaluate(()=>{location.hash='#/view/reports';});await page.waitForTimeout(700);
      const tabSequence=[];for(let i=0;i<14;i++){await page.keyboard.press('Tab');tabSequence.push(await page.evaluate(()=>({tag:document.activeElement.tagName,id:document.activeElement.id,text:document.activeElement.textContent.trim().slice(0,45)})));}
      fs.writeFileSync(path.join(evidence,config.id+'-keyboard.json'),JSON.stringify(tabSequence,null,2));
      await context.close();
    }
  } finally { await browser.close(); }
})().catch(e=>{console.error(e.stack);process.exitCode=1;});
