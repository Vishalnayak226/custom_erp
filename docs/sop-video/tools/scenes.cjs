'use strict';

const { showRow } = require('./scene-helpers.cjs');

function docScene(doctype, nameField, values, key) {
  return async (page, h, state) => {
    await h.open(`/#/setup/${doctype}`, `Open ${doctype} under Setup. Confirm this master before starting a transaction.`);
    await h.step(`Choose New ${doctype}.`, () => page.locator('#doc-create-button').click(), '#doc-create-button');
    await page.locator('#dynamic-modal.open').waitFor();
    for (const [field, value] of Object.entries(values(state))) {
      const selector = `#dyn-field-${doctype}-${field}`;
      await h.step(`Enter ${field.replaceAll('_', ' ')}: ${value}.`, async () => {
        const element = page.locator(selector);
        if (await element.evaluate(e => e.tagName === 'SELECT')) await element.selectOption({ label: String(value) });
        else await h.type(selector, String(value));
      }, selector);
    }
    const reply = await h.step(`Save ${doctype}, then check the record appears in the list.`, () => h.saveDoc(doctype), '#dynamic-modal-form button[type="submit"]');
    state[key] = { name: values(state)[nameField], id: reply.id };
    await page.locator('#view-root tbody tr').filter({ hasText: state[key].name }).first().waitFor({ timeout: 12000 });
    await h.note(`Saved ${state[key].name}. The system generated its code; the name is what people should search for.`);
    return `${doctype} saved and read back: ${state[key].name}`;
  };
}

module.exports = {
  department_setup: docScene('Department', 'name', state => ({ name: `SOP Buying ${state.tag}` }), 'department'),
  vendor_setup: docScene('Vendor', 'name', state => ({ name: `SOP Supply ${state.tag}`, state: 'Karnataka' }), 'vendor'),
  item_setup: docScene('Item', 'name', state => ({ name: `SOP Raw Material ${state.tag}`, barcode: `SOP-RM-${state.tag.replace(/[^A-Za-z0-9]/g, '')}`, hsn_code: '9999', gst_rate: '18', standard_cost: '10' }), 'item'),
  location_check: async (page, h, state) => {
    await h.open('/#/setup/Location', 'Before a PO or receipt, confirm the receiving warehouse is active and belongs to the right entity.');
    const row = page.locator('#view-root tbody tr').filter({ hasText: 'Audit Distribution Center' }).first();
    await row.waitFor();
    await h.step('Find the receiving warehouse by its name.', async () => { await row.scrollIntoViewIfNeeded(); }, '#view-root tbody');
    await h.step('Open the record to check its type, entity and status.', () => row.getByRole('button', { name: /edit/i }).click());
    await page.locator('#dynamic-modal.open').waitFor();
    state.location = { name: 'Audit Distribution Center', id: 'Location/HQ/2026/000001' };
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
    state.requisition.number = (await row.locator('td').first().innerText()).trim();
    await h.note(`Read back requisition ${state.requisition.number} in the list. The buyer can now review it.`);
    return `Numbered Draft requisition saved: ${state.requisition.number}`;
  },
  po_create: async (page, h, state) => {
    if (!state.vendor?.name || !state.item?.name || !state.location?.name) throw new Error('Record Vendor, Item and Location setup first.');
    await h.open('/#/view/purchase-orders', 'A purchase order needs a vendor, item and destination. This chapter explains each prerequisite before this step.');
    await h.step(`Find the supplier ${state.vendor.name}.`, async () => {
      await h.type('#po-vendor input', 'SOP Supply'); await page.waitForTimeout(900); // human reading time; lets the suggestion list settle
      await page.locator('#po-vendor .item').filter({ hasText: state.vendor.name }).first().click();
    }, '#po-vendor');
    for (const [selector, label] of [['#po-location', 'billing location'], ['#po-warehouse', 'receiving warehouse']]) {
      await h.step(`Choose the ${label} by name.`, async () => {
        await h.type(selector, 'Audit'); await page.waitForTimeout(900); // human reading time; lets the suggestion list settle
        await page.locator('.typeahead-item').filter({ hasText: state.location.name }).first().click();
      }, selector);
    }
    await h.step(`Add ${state.item.name}, quantity 100.`, async () => {
      await page.locator('#po-lines-body').evaluate(e => e.scrollIntoView({ block: 'center' })); await page.waitForTimeout(400); // keep the lines above the caption bar
      const input = page.locator('#po-lines-body [data-po-field="sku"]').first();
      await h.type('#po-lines-body [data-po-field="sku"]', 'SOP Raw Material'); await page.waitForTimeout(900); // human reading time; lets the suggestion list settle
      await page.locator('.typeahead-item').filter({ hasText: state.item.name }).first().click({ timeout: 2500 });
      await page.locator('#po-lines-body [data-po-field="qty"]').first().fill('100');
    }, '#po-lines-body');
    await h.step('Enter the agreed rate and review the displayed total.', async () => {
      await page.locator('#po-lines-body').evaluate(e => e.scrollIntoView({ block: 'center' })); await page.waitForTimeout(400); // keep the lines above the caption bar
      await h.type('#po-lines-body [data-po-field="rate"]', '10');
      await page.locator('#po-lines-body [data-po-field="rate"]').first().blur();
      await page.locator('#po-create-btn').waitFor();
    }, '#po-lines-body');
    await h.step('Create the Draft PO. Do not receive stock until it is approved.', async () => {
      const reply = await h.post('/api/v1/doc/PurchaseOrder', () => page.locator('#po-create-btn').click());
      state.po = { id: reply.id };
    }, '#po-create-btn');
    await showRow(page, h, state.po.id, `Draft PO ${state.po.id} is saved and listed. A different authorized person must approve it.`);
    return `Draft PO saved: ${state.po.id}`;
  },
  // Recorded as the BUYER. Split from po_approval so each is filmed under its
  // own login: the buyer cannot approve their own order (maker-checker).
  po_submit: async (page, h, state) => {
    if (!state.po?.id) throw new Error('Record po_create first.');
    await h.open('/#/view/purchase-orders', 'The buyer sends the Draft PO for approval. It cannot be received until someone else approves it.');
    const row = await showRow(page, h, state.po.id, `Find Draft PO ${state.po.id} in the list.`);
    await h.step('Submit the Draft PO for approval.', async () => { await h.post('/api/v1/approval/submit', () => row.getByRole('button', { name: 'Submit for Approval' }).click()); });
    await showRow(page, h, state.po.id, `PO ${state.po.id} is now Pending Approval. The buyer's part is done.`);
    return `Submitted for approval: ${state.po.id}`;
  },
  // Recorded as the APPROVER (a different person - Store Manager by default
  // for orders under 50,000).
  po_approval: async (page, h, state) => {
    if (!state.po?.id) throw new Error('Record po_create and po_submit first.');
    await h.open('/#/view/approvals', 'The approver signs in with their own account. Orders waiting for them are listed here, and on Home.');
    const row = page.locator('#view-root tbody tr').filter({ hasText: state.po.id }).first(); await row.waitFor();
    await h.step('Review the supplier, destination, amount and status in the approval queue.', async () => { await row.scrollIntoViewIfNeeded(); });
    await h.step('Approve as an authorized person who did not create the PO.', async () => {
      const reply = await h.post('/api/v1/approval/decide', () => row.getByRole('button', { name: 'Approve' }).click());
      if (reply.decision !== 'Approved') throw new Error('PO approval did not complete.');
    });
    await h.note(`PO ${state.po.id} is approved and may be used for Goods Receipt.`);
    return `PO approved by separate account: ${state.po.id}`;
  },
  grn_post: async (page, h, state) => {
    if (!state.po?.id || !state.location?.name) throw new Error('Record approved PO and Location setup first.');
    await h.open('/#/view/grn', 'Before posting, reconfirm the vendor, item, warehouse and approved PO. These setup steps are repeated in this chapter.');
    await h.step(`Enter approved PO ${state.po.id}.`, () => h.type('#grn-po', state.po.id), '#grn-po');
    await h.step('Choose the receiving warehouse by name.', async () => {
      await h.type('#grn-location', 'Audit'); await page.waitForTimeout(900); // human reading time; lets the suggestion list settle
      await page.locator('.typeahead-item').filter({ hasText: state.location.name }).first().click();
    }, '#grn-location');
    await h.step('Load remaining ordered lines from the PO.', () => page.getByRole('button', { name: 'Load Items from PO' }).click());
    await page.locator('#grn-lines-list tbody tr').first().waitFor();
    await h.note('Compare the physical delivery with the PO line before posting. This normal-case lesson receives the full ordered quantity.');
    await h.step('Post the receipt and confirm the stock-impact message.', async () => {
      await page.locator('#grn-create-btn').click();
      await page.locator('#custom-dialog-container:not(.hidden)').waitFor();
      state.grn = await h.post('/api/v1/doc/GRN', () => page.locator('#custom-dialog-container').getByRole('button', { name: /^(OK|Confirm|Post)$/i }).last().click());
    }, '#grn-create-btn');
    await page.locator('#view-root tbody tr').filter({ hasText: state.po.id }).first().waitFor({ timeout: 12000 });
    await h.open('/#/view/inventory', 'Finish by checking the received item at the correct warehouse in Inventory.');
    await h.note(`GRN ${state.grn.id} posted. Compare the stock row with the accepted quantity before ending the process.`);
    return `Goods Receipt posted: ${state.grn.id}`;
  }
};

// Module chapters beyond the first procurement edition.
Object.assign(module.exports, require('./scenes-modules.cjs'));
