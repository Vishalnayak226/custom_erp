'use strict';

const { showRow, showRowById, newItem, slug } = require('./scene-helpers.cjs');

function docScene(doctype, nameField, values, key, label = doctype) {
  return async (page, h, state) => {
    await h.open(`/#/setup/${doctype}`, `Open ${label} under Setup. Confirm this master before starting a transaction.`);
    await h.step(`Choose New ${label}.`, () => page.locator('#doc-create-button').click(), '#doc-create-button');
    await page.locator('#dynamic-modal.open').waitFor();
    for (const [field, value] of Object.entries(values(state))) {
      const selector = `#dyn-field-${doctype}-${field}`;
      await h.step(`Enter ${field.replaceAll('_', ' ')}: ${value}.`, async () => {
        const element = page.locator(selector);
        if (await element.evaluate(e => e.tagName === 'SELECT')) await element.selectOption({ label: String(value) });
        else await h.type(selector, String(value));
      }, selector);
    }
    const reply = await h.step(`Save ${label}, then check the record appears in the list.`, () => h.saveDoc(doctype), '#dynamic-modal-form button[type="submit"]');
    state[key] = { name: values(state)[nameField], id: reply.id };
    await page.locator('#view-root tbody tr').filter({ hasText: state[key].name }).first().waitFor({ timeout: 12000 });
    await h.note(`Saved ${state[key].name}. The system generated its code; the name is what people should search for.`);
    return `${doctype} saved and read back: ${state[key].name}`;
  };
}

module.exports = {
  department_setup: docScene('Department', 'name', state => ({ name: `SOP Buying ${state.tag}` }), 'department'),
  vendor_setup: docScene('Vendor', 'name', state => ({ name: `SOP Supply ${state.tag}`, state: 'Karnataka' }), 'vendor'),
  // An item is named from the Item Name list, which sits under a category and
  // a product type. A new company has none of the three, so set them up first.
  item_name_setup: async (page, h, state) => {
    await docScene('Category', 'name', s => ({ name: `SOP Raw Materials ${s.tag}` }), 'category')(page, h, state);
    await docScene('ProductType', 'name', s => ({ name: `SOP Stock Goods ${s.tag}` }), 'product_type', 'Product Type')(page, h, state);
    await docScene('ItemName', 'name', s => ({ category: s.category.name, product_type: s.product_type.name, name: `SOP Raw Material ${s.tag}`, hsn_code: '9999', gst_rate: '18' }), 'item_name', 'Item Name')(page, h, state);
    return `Category, product type and item name saved: ${state.item_name.name}`;
  },
  item_setup: async (page, h, state) => {
    if (!state.item_name?.name) throw new Error('Record item_name_setup first.');
    await h.open('/#/setup/Item', 'Open Item under Setup. Each item is picked from the Item Name list, then given its barcode and cost.');
    state.item = await newItem(page, h, { itemName: state.item_name.name, name: `SOP Raw Material ${state.tag}`, barcode: `SOP-RM-${state.tag.replace(/[^A-Za-z0-9]/g, '')}`, cost: 10 });
    return `Item saved and read back: ${state.item.name}`;
  },
  location_check: async (page, h, state) => {
    await h.open('/#/setup/Location', 'Before a PO or receipt, confirm the receiving warehouse is active and belongs to the right entity.');
    // The list's own rows carry Edit; a code-change table above it lists the same names.
    const row = page.locator('#view-root tbody tr').filter({ hasText: 'Audit Distribution Center' }).filter({ has: page.getByRole('button', { name: /edit/i }) }).first();
    await row.waitFor();
    await h.step('Find the receiving warehouse by its name.', async () => { await row.scrollIntoViewIfNeeded(); }, '#view-root tbody');
    await h.step('Open the record to check its type, entity and status.', () => row.getByRole('button', { name: /edit/i }).click());
    await page.locator('#dynamic-modal.open').waitFor();
    state.location = { name: 'Audit Distribution Center', id: 'Location/HQ/2026/000001' };
    state.store = { name: 'Audit Retail Store', id: 'Location/HQ/2026/000002' }; // the training shop, used by the till and transfer chapters
    await h.note('This is an existing training warehouse. In a new company, an administrator creates it before ordering.');
    await h.step('Close without changing this already-configured warehouse.', () => page.locator('#dynamic-modal').getByRole('button', { name: /cancel/i }).click());
    return 'Receiving warehouse verified; no existing record changed.';
  },
  requisition_create: async (page, h, state) => {
    if (!state.department?.name) throw new Error('Record department_setup first, or supply the same fixture contract.');
    const description = `Raw material for training ${state.tag} ${Date.now().toString().slice(-6)}`;
    await h.open('/#/setup/PurchaseRequisition', 'Now raise the need. The department must already exist; this chapter showed that setup first.');
    await h.step('Choose New Purchase Requisition.', () => page.locator('#doc-create-button').click(), '#doc-create-button');
    await page.locator('#dynamic-modal.open').waitFor();
    await h.step('Describe the requirement in plain language.', () => h.type('#dyn-field-PurchaseRequisition-description', description), '#dyn-field-PurchaseRequisition-description');
    await h.step('Enter the requested quantity: 100.', () => h.type('#dyn-field-PurchaseRequisition-quantity', '100'), '#dyn-field-PurchaseRequisition-quantity');
    await h.step(`Select the responsible department: ${state.department.name}.`, () => h.type('#dyn-field-PurchaseRequisition-department', state.department.name), '#dyn-field-PurchaseRequisition-department');
    await h.step('Enter an estimated amount for review: 1,000.', () => h.type('#dyn-field-PurchaseRequisition-total_amount', '1000'), '#dyn-field-PurchaseRequisition-total_amount');
    await h.step('Leave status Draft and save the requisition.', async () => { await page.locator('#dyn-field-PurchaseRequisition-status').selectOption({ label: 'Draft' }); state.requisition = await h.saveDoc('PurchaseRequisition'); }, '#dynamic-modal-form button[type="submit"]');
    await h.open('/#/setup/PurchaseRequisition', 'Refresh the list to verify the saved request rather than trusting the save message alone.');
    const row = page.locator('#view-root tbody tr').filter({ hasText: description }).first();
    await row.waitFor({ timeout: 12000 });
    // The list leads with a select box; the number is the PR/... code in the row.
    state.requisition.number = state.requisition.code || ((await row.innerText()).match(/PR\/\S+/) || [''])[0];
    if (!state.requisition.number) throw new Error('No requisition number in the saved row.');
    await h.note(`Read back requisition ${state.requisition.number} in the list. The buyer can now review it.`);
    return `Numbered Draft requisition saved: ${state.requisition.number}`;
  },
  po_create: async (page, h, state) => {
    if (!state.vendor?.name || !state.item?.name || !state.location?.name) throw new Error('Record Vendor, Item and Location setup first.');
    await h.open('/#/view/purchase-orders', 'A purchase order needs a vendor, item and destination. This chapter explains each prerequisite before this step.');
    await h.step('Choose + New Purchase Order.', () => page.locator('[data-act="newPurchaseOrder"]').first().click(), '[data-act="newPurchaseOrder"]');
    await page.locator('#po-vendor').waitFor();
    await h.step(`Find the supplier ${state.vendor.name}.`, async () => {
      await h.type('#po-vendor', 'SOP Supply'); await page.waitForTimeout(900); // human reading time; lets the suggestion list settle
      await page.locator('.typeahead-menu .typeahead-item').filter({ hasText: state.vendor.name }).first().click();
    }, '#po-vendor');
    for (const [selector, label] of [['#po-location', 'billing location'], ['#po-warehouse', 'receiving warehouse']]) {
      await h.step(`Choose the ${label} by name.`, async () => {
        await h.type(selector, 'Audit'); await page.waitForTimeout(900); // human reading time; lets the suggestion list settle
        await page.locator('.typeahead-item').filter({ hasText: state.location.name }).first().click();
      }, selector);
    }
    // One entry row above the lines: the item, its qty and purchase price, then Add.
    // The search lists item names first and items (SKUs) after, so the item is the last match.
    await h.step(`Add ${state.item.name}: quantity 100 at the agreed purchase price 10.`, async () => {
      await page.locator('#po-entry').evaluate(e => e.scrollIntoView({ block: 'center' })); await page.waitForTimeout(400); // keep the entry row above the caption bar
      await h.type('#po-entry-sku', 'SOP Raw Material'); await page.waitForTimeout(900); // human reading time; lets the suggestion list settle
      await page.locator('.typeahead-menu .typeahead-item').filter({ hasText: state.item.name }).last().click();
      await page.waitForTimeout(700);
      await h.type('#po-entry-qty', '100');
      await h.type('#po-entry-rate', '10');
      await page.locator('#po-entry-add').click();
      await page.locator('#po-lines-body [data-po-line]').first().waitFor();
    }, '#po-entry');
    await h.step('Review the line and the displayed total.', async () => {
      await page.locator('#po-totals').evaluate(e => e.scrollIntoView({ block: 'center' })); await page.waitForTimeout(1200);
      await page.locator('#po-create-btn').waitFor();
    }, '#po-totals');
    await h.step('Create the Draft PO. Do not receive stock until it is approved.', async () => {
      const reply = await h.post('/api/v1/doc/PurchaseOrder', () => page.locator('#po-create-btn').click());
      state.po = { id: reply.id, number: reply.po_number || reply.code || reply.id };
    }, '#po-create-btn');
    // The list shows the PO number; the row is found by the id its buttons carry.
    await showRowById(page, h, state.po.id, `Draft PO ${state.po.number} is saved and listed. A different authorized person must approve it.`);
    return `Draft PO saved: ${state.po.number}`;
  },
  // Recorded as the BUYER. Split from po_approval so each is filmed under its
  // own login: the buyer cannot approve their own order (maker-checker).
  po_submit: async (page, h, state) => {
    if (!state.po?.id) throw new Error('Record po_create first.');
    await h.open('/#/view/purchase-orders', 'The buyer sends the Draft PO for approval. It cannot be received until someone else approves it.');
    const number = state.po.number || state.po.id;
    const row = await showRowById(page, h, state.po.id, `Find Draft PO ${number} in the list.`);
    await h.step('Submit the Draft PO for approval.', async () => { await h.post('/api/v1/approval/submit', () => row.getByRole('button', { name: 'Submit for Approval' }).click()); });
    await showRowById(page, h, state.po.id, `PO ${number} is now Pending Approval. The buyer's part is done.`);
    return `Submitted for approval: ${number}`;
  },
  // Recorded as the APPROVER (a different person - Store Manager by default
  // for orders under 50,000).
  po_approval: async (page, h, state) => {
    if (!state.po?.id) throw new Error('Record po_create and po_submit first.');
    await h.open('/#/view/approvals', 'The approver signs in with their own account. Orders waiting for them are listed here, and on Home.');
    const row = page.locator('#inbox-approvals tbody tr').filter({ has: page.locator(`[data-act-args*="${state.po.id}"]`) }).first(); await row.waitFor();
    await h.step('Review the supplier, destination, amount and status in the approval queue.', async () => { await row.scrollIntoViewIfNeeded(); });
    await h.step('Approve as an authorized person who did not create the PO.', async () => {
      const reply = await h.post('/api/v1/approval/decide', () => row.getByRole('button', { name: 'Approve' }).click());
      if (reply.decision !== 'Approved') throw new Error('PO approval did not complete.');
    });
    await h.note(`PO ${state.po.id} is approved and may be used for Goods Receipt.`);
    return `PO approved by separate account: ${state.po.id}`;
  },
  // The receipt goes in steps (Stage 94): Receive saves it, nothing is in
  // stock until Verify, Price check and Post are done. A first-time item
  // cannot take the one-click "Post all", so the lesson walks every step.
  grn_post: async (page, h, state) => {
    if (!state.po?.id || !state.location?.name) throw new Error('Record approved PO and Location setup first.');
    const number = state.po.number || state.po.id;
    await h.open('/#/view/purchase-orders', 'Goods arrive against an approved purchase order. Start the receipt from the PO itself.');
    const poRow = await showRowById(page, h, state.po.id, `PO ${number} is Approved, so its goods can be received.`);
    // The vendor's invoice total is the PO's grand total (incl. GST) in this normal case.
    const total = Number(((await poRow.locator('td').nth(6).innerText()).match(/[\d,]+(\.\d+)?/) || ['0'])[0].replace(/,/g, ''));
    if (!(total > 0)) throw new Error('No grand total on the PO row.');
    await h.step('Choose Receive goods on its row. The PO and its lines load.', () => poRow.getByRole('button', { name: 'Receive goods' }).click());
    await page.locator('#grn-lines-list tbody tr').first().waitFor({ timeout: 15000 });
    await h.note('Check the receiving location and compare each line with what physically arrived. This normal-case lesson receives the full ordered quantity.');
    const invoiceNo = `INV-${slug(state.tag).toUpperCase()}-${String(number).split('/').pop()}`;
    await h.step(`Key the vendor's invoice: number ${invoiceNo}, date and total ${total.toFixed(2)} incl. GST.`, async () => {
      await h.type('#grn-inv-no', invoiceNo);
      await page.locator('#grn-inv-date').fill(new Date().toISOString().slice(0, 10));
      await h.type('#grn-inv-amount', total.toFixed(2));
    }, '#grn-inv-no');
    const saved = await h.step('Save the receipt. Nothing is in stock yet.', () =>
      h.post('/api/v1/doc/GRN', () => page.locator('#grn-create-btn').click()), '#grn-create-btn');
    const base = `/api/v1/grn-steps/${encodeURIComponent(saved.id)}`;
    const code = saved.code || saved.grn_number || saved.id;
    const verify = page.locator('[data-gs="verify"]');
    await verify.waitFor({ timeout: 15000 });
    await h.step('Verify: the GRN quantity and price are what is really in hand.', () => h.post(`${base}/verify`, () => verify.click()), '[data-gs="verify"]');
    const price = page.locator('[data-gs="price"]');
    await price.waitFor();
    await h.note('Price check: each line against its last purchase price. A new item is marked "never bought".');
    await h.step('Check prices. It saves the check and goes on to MRP.', () => h.post(`${base}/price`, () => price.click()), '[data-gs="price"]');
    const postBtn = page.locator('[data-gs="post-child"]').first();
    await postBtn.waitFor();
    await h.step('Enter the MRP each unit is sold at: 25.', async () => {
      // Leaving an MRP box redraws the step with its margin, so leave each one
      // before the next click (a click that blurs it is lost: 230.2).
      const boxes = page.locator('.gs-child input[data-field="mrp"]');
      for (let i = 0, n = await boxes.count(); i < n; i++) {
        await boxes.nth(i).fill(''); await boxes.nth(i).pressSequentially('25', { delay: 55 });
        await boxes.nth(i).press('Tab');
        await page.waitForTimeout(300);
      }
    }, '.gs-child');
    const key = await postBtn.getAttribute('data-child');
    await h.step(`Save and post ${code}/${key}. The stock goes in now.`, () =>
      h.post(`${base}/post/${encodeURIComponent(key)}`, () => postBtn.click()), '[data-gs="post-child"]');
    const leave = page.locator('[data-gs="leave"]');
    await leave.waitFor({ timeout: 15000 });
    await h.note(`${code}/${key} is posted. The vendor's invoice now waits in Payables.`);
    await h.step('Leave it in Payables; finance pays it later.', () => leave.click(), '[data-gs="leave"]');
    state.grn = { id: saved.id, code, child: key, invoice_no: invoiceNo, amount: total };
    await h.open('/#/view/inventory', 'Finish by checking the received item at the correct warehouse in Inventory.');
    await h.note(`GRN ${code} posted. Compare the stock row with the accepted quantity before ending the process.`);
    return `Goods Receipt posted: ${code}/${key}`;
  }
};

// Module chapters beyond the first procurement edition.
Object.assign(module.exports, require('./scenes-modules.cjs'));
