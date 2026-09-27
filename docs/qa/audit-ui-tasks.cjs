'use strict';
const fs=require('node:fs'),path=require('node:path');
const [source,evidence]=process.argv.slice(2),base='http://127.0.0.1:8178';
const {resolvePlaywright}=require(path.join(source,'docs/guides/capture-screenshots.js'));
const token=fs.readFileSync(path.join(evidence,'ui-token.txt'),'utf8').replace(/^\uFEFF/,'').trim();
(async()=>{
 const browser=await resolvePlaywright().chromium.launch();const results=[];
 try{
  for(let round=1;round<=3;round++){
   const context=await browser.newContext({viewport:round===2?{width:390,height:844}:{width:1440,height:900},reducedMotion:'reduce'});
   await context.addInitScript(token=>{localStorage.setItem('erp_token',token);localStorage.setItem('erp_username','admin');localStorage.setItem('erp_role','Super Admin');localStorage.setItem('erp_tenant_id','default');},token);
   const page=await context.newPage();page.setDefaultTimeout(12000);const record={round,errors:[]};
   page.on('pageerror',e=>record.errors.push(e.message));
   try{
    await page.goto(base+'/#/setup/Vendor',{waitUntil:'networkidle'});
    await page.locator('button[onclick="openDynamicModal()"]').click();
    await page.locator('#dynamic-modal.open').waitFor({state:'visible'});
    const client=await context.newCDPSession(page);
    const ax=await client.send('Accessibility.getFullAXTree');
    record.unnamedAXControls=ax.nodes.filter(n=>!n.ignored&&['textbox','combobox','spinbutton'].includes(n.role?.value)&&!n.name?.value).map(n=>({role:n.role.value,backendDOMNodeId:n.backendDOMNodeId}));
    record.formLabels=await page.locator('#dynamic-modal').evaluate(e=>[...e.querySelectorAll('input,select,textarea')].map(i=>({name:i.name,labelCount:i.labels?.length||0,aria:i.getAttribute('aria-label'),required:i.required,disabled:i.disabled})));
    record.initialFocus=await page.evaluate(()=>({tag:document.activeElement.tagName,name:document.activeElement.getAttribute('name')}));
    await page.locator('#dynamic-modal-form [name="contact_email"]').fill('invalid-email');
    record.invalidEmailRejected=await page.locator('#dynamic-modal-form [name="contact_email"]').evaluate(e=>!e.checkValidity());
    await page.locator('#dynamic-modal-form [name="contact_email"]').fill('audit@example.invalid');
    const name=`Audit ${round} नमस्ते العربية 中文 🧾 <img src=x onerror="window.__auditXSS=true">`;
    await page.locator('#dynamic-modal-form [name="name"]').fill(name);
    await page.locator('#dynamic-modal-form [name="status"]').selectOption('Active');
    await page.screenshot({path:path.join(evidence,`vendor-form-${round}.png`),animations:'disabled'});
    const request=page.waitForResponse(r=>r.url().includes('/api/v1/doc/Vendor')&&r.request().method()==='POST');
    await page.locator('#dynamic-modal-form button[type="submit"]').click();
    const response=await request;record.saveStatus=response.status();record.saveBody=await response.json();
    await page.waitForTimeout(900);
    record.modalClosed=await page.locator('#dynamic-modal').evaluate(e=>!e.classList.contains('open'));
    record.storedXSSExecuted=await page.evaluate(()=>window.__auditXSS===true);
    record.nameVisible=(await page.locator('#view-root').innerText()).includes(name);
    record.returnFocus=await page.evaluate(()=>({tag:document.activeElement.tagName,text:document.activeElement.textContent.trim().slice(0,60)}));
    record.documentOverflow=await page.evaluate(()=>document.documentElement.scrollWidth>innerWidth+1);
    // A fresh read proves the server stored the Unicode value, rather than only showing optimistic UI.
    record.serverRead=await page.evaluate(async name=>{const r=await fetch('/api/v1/doc/Vendor?limit=100',{headers:{Authorization:'Bearer '+localStorage.getItem('erp_token')}});return {status:r.status,containsName:JSON.stringify(await r.json()).includes(JSON.stringify(name).slice(1,-1))};},name);
   }catch(error){record.failure=error.message.split('\n')[0];}
   results.push(record);fs.writeFileSync(path.join(evidence,'ui-task-results.json'),JSON.stringify(results,null,2));console.log(JSON.stringify(record));
   await context.close();await new Promise(r=>setTimeout(r,12000));
  }
 }finally{await browser.close();}
})().catch(e=>{console.error(e.stack);process.exitCode=1;});
