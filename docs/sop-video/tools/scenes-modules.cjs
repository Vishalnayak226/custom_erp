'use strict';

// Module chapters beyond the first procurement edition. Same contract as
// scenes.cjs: each scene films one replaceable segment under one login,
// reads its fixtures from `state`, writes what later scenes need back into
// it, and returns the one-line result shown on the end card. Every scene ends
// on its saved record, read back in the app.

const { showRow, showRowById, outlineRow, openNew, pick, pickItem, newItem, confirmDialog, slug } = require('./scene-helpers.cjs');

// Order Management re-renders its queue after every action and dialog, which
// resets the scroll position - so settle first, then centre and outline the
// order's row, then caption it.
async function showOrderRow(page, h, orderId, status, message) {
  await page.waitForTimeout(1200);
  const row = page.locator('tr', { has: page.locator(`[data-open-order="${orderId}"]`) }).first();
  await row.waitFor();
  if (status) await row.getByText(status).first().waitFor();
  await page.evaluate(() => { const root = document.getElementById('view-root'); if (root) root.style.paddingBottom = '320px'; });
  await row.evaluate(e => { e.scrollIntoView({ block: 'center' }); e.style.outline = '3px solid #f6c85f'; e.style.outlineOffset = '-3px'; });
  await page.waitForTimeout(400);
  await h.note(message);
}

// Every POS chapter starts at the right shop: the till works at the store
// chosen in the top bar. Skipped when the login already works there; a login
// tied to one store has no switch to press.
async function chooseShop(page, h, state) {
  const label = page.locator('#store-switch .store-switch-label');
  await label.waitFor({ state: 'attached' });
  if ((await label.innerText()).trim() === state.store.name) return;
  const btn = page.locator('#store-switch-btn');
  if (!(await btn.count())) throw new Error(`This login works only at ${(await label.innerText()).trim()}, not ${state.store.name}.`);
  await h.step(`Choose the shop in the top bar: ${state.store.name}.`, async () => {
    await btn.click();
    await h.post('/api/v1/me/working-store', () => page.locator('#store-switch-pop .store-switch-opt').filter({ hasText: state.store.name }).first().click());
  }, '#store-switch-btn');
  await page.locator('#store-switch .store-switch-label', { hasText: state.store.name }).waitFor({ state: 'attached' });
}

module.exports = {
  // --- Administration: roles and users ------------------------------------
  role_setup: async (page, h, state) => {
    const role = `SOP Store Keeper ${state.tag}`;
    // Stage 79: list first; + New Role is its own page, and creating a role
    // opens its access page - every module, ticked and saved at once.
    await h.open('/#/view/roles', 'Roles decide what each person can see and do. Create the role first, then give it access.');
    const newRole = '#roles-screen .page-section-actions .btn-primary';
    await h.step('Start a new role.', () => page.locator(newRole).click(), newRole);
    await h.step(`Name the new role: ${role}.`, () => h.type('#new-role-name', role), '#new-role-name');
    await h.step('Create the role. It opens on its access page - a new role can see nothing until it is given access.',
      () => h.post('/api/v1/admin/roles', () => page.locator('#roles-new-form button[type="submit"]').click()), '#roles-new-form button[type="submit"]');
    const inventoryCreate = '#roles-matrix-body input[data-mod="Inventory"][data-col="c"]';
    await page.locator(inventoryCreate).waitFor();
    await h.step('Every module is listed. Tick Create on the Inventory module - Read is ticked with it, for every Inventory record type.',
      () => page.locator(inventoryCreate).check(), inventoryCreate);
    await h.step('Save the access.', () => h.post('/api/v1/admin/role-permissions', () => page.locator('#roles-save').click()), '#roles-save');
    await page.locator('#roles-screen .txn-crumb').click();
    await showRow(page, h, role, `${role} can now use the Inventory module, and nothing else.`);
    state.role = role;
    return `Role created with Inventory access: ${role}`;
  },

  user_setup: async (page, h, state) => {
    if (!state.role) throw new Error('Record role_setup first.');
    const username = `sop.keeper.${slug(state.tag)}`;
    await h.open('/#/view/users', 'Now create the person. Every user needs a role; the role must already exist.');
    const newUser = '[data-act-args*="users-screen:new"]';
    await h.step('Choose + New User.', () => page.locator(newUser).first().click(), newUser);
    await page.locator('#user-username').waitFor();
    await h.step(`Enter the username: ${username}.`, () => h.type('#user-username', username), '#user-username');
    await h.step('Enter a temporary password. Give it to the person yourself; they replace it at their first sign-in.',
      () => h.type('#user-password', 'Training-Temp-2026!'), '#user-password');
    await h.step(`Choose the role: ${state.role}.`, () => page.locator('#user-role').selectOption({ label: state.role }), '#user-role');
    await h.step(`Choose their department: ${state.department.name}.`, async () => {
      const sel = page.locator('#user-department');
      await sel.selectOption(await sel.locator('option').filter({ hasText: state.department.name }).first().getAttribute('value'));
    }, '#user-department');
    await h.step('Create the user.', () => h.post('/api/v1/admin/users', () => page.locator('#user-create-btn').click()), '#user-create-btn');
    await showRow(page, h, username, `${username} is Active. At first sign-in they must set their own password, and they see only the Inventory module.`);
    state.user = username;
    return `User created: ${username} (${state.role})`;
  },

  // --- Master data ----------------------------------------------------------
  bin_zone_setup: async (page, h, state) => {
    const bin = `SOP-A1-${slug(state.tag).toUpperCase()}`;
    const zone = `Cold Room ${state.tag}`;
    await h.open('/#/setup/Bin', 'A bin is a shelf position inside a warehouse. Its zone groups bins for putaway and picking.');
    await h.step('Choose New Bin.', () => page.locator('#doc-create-button').click(), '#doc-create-button');
    await page.locator('#dynamic-modal.open').waitFor();
    await h.step(`Enter the bin code: ${bin}.`, () => h.type('#dyn-field-Bin-bin_code', bin), '#dyn-field-Bin-bin_code');
    await h.step(`Choose the warehouse by name: ${state.location.name}.`,
      () => page.locator('#dyn-field-Bin-location').selectOption({ label: state.location.name }), '#dyn-field-Bin-location');
    await h.step(`Type the zone. "${zone}" does not exist yet, so create it right here.`, async () => {
      await h.type('#dyn-field-Bin-zone', zone);
      await page.waitForTimeout(900);
      await page.locator('.typeahead-menu a.empty-state-link').dispatchEvent('mousedown');
      await page.locator('.quick-create-overlay').waitFor();
    }, '#dyn-field-Bin-zone');
    await h.step('Only what a zone needs is asked. Its name is already filled in; create it.',
      () => h.post('/api/v1/doc/Zone', () => page.locator('.quick-create-overlay button[type="submit"]').click()),
      '.quick-create-overlay .modal-container');
    await page.locator('.quick-create-overlay').waitFor({ state: 'detached' });
    await h.note('The new zone is filled into the Bin form, which never closed.');
    await h.step('Save the bin.', () => h.saveDoc('Bin'), '#dynamic-modal-form button[type="submit"]');
    await showRow(page, h, bin, `Bin ${bin} is saved in ${zone} at ${state.location.name}.`);
    state.bin = bin; state.zone = zone;
    return `Bin ${bin} saved, with zone ${zone} created in place`;
  },

  // --- Stock: move stock between locations ----------------------------------
  transfer_stock: async (page, h, state) => {
    if (!state.store?.name || !state.item?.name) throw new Error('Needs the warehouse, shop and item fixtures.');
    await h.open('/#/view/transfers', 'Move stock from the warehouse to the shop: create the transfer, approve it, dispatch it, and receive it at the shop.');
    await h.step('Choose + New Transfer.', () => page.locator('[data-act-args*="transfer-screen:new"]').first().click(), '[data-act-args*="transfer-screen:new"]');
    await page.locator('#transfer-from').waitFor();
    await h.step(`From: ${state.location.name}.`, () => pick(page, h, '#transfer-from', 'Audit', state.location.name), '#transfer-from');
    await h.step(`To: ${state.store.name}.`, () => pick(page, h, '#transfer-to', 'Audit', state.store.name), '#transfer-to');
    await h.step(`Scan the item's barcode: the list shows ${state.item.name}. Quantity 20.`, async () => {
      await pickItem(page, h, '#transfer-line-sku', state.item);
      await h.type('#transfer-line-qty', '20');
      await page.locator('#transfer-add-line-btn').click();
    }, '#transfer-line-sku');
    const created = await h.step('Create the transfer. It starts as Draft.',
      () => h.post('/api/v1/doc/TransferOrder', () => page.locator('#transfer-create-btn').click()), '#transfer-create-btn');
    const id = created.id;
    // The list shows the transfer number; its buttons carry the id.
    const number = created.transfer_number || created.code || id;
    const showTransfer = async message => {
      const r = page.locator('#view-root tbody tr').filter({ has: page.locator(`[data-act-args*="${id}"]`) }).first();
      await r.waitFor({ timeout: 12000 });
      await r.evaluate(e => { e.scrollIntoView({ block: 'center' }); e.style.outline = '3px solid #f6c85f'; e.style.outlineOffset = '-3px'; });
      await h.note(message);
      return r;
    };
    let row = await showTransfer(`Transfer ${number} is saved as Draft.`);
    await h.step('Approve it now: its value is below the limit a store manager approves alone.', async () => {
      await row.getByRole('button', { name: 'Approve now' }).click();
      await h.post('/api/v1/transfer/approve-direct', () => confirmDialog(page));
    });
    row = await showTransfer('Approved. Now pick and dispatch it from the warehouse.');
    await h.step('Dispatch it. Stock leaves the warehouse and is in transit.', async () => {
      await row.getByRole('button', { name: 'Dispatch', exact: true }).click();
      await h.post('/api/v1/transfer/dispatch', () => confirmDialog(page));
    });
    row = await showTransfer('Dispatched. At the shop, receive what arrived.');
    await h.step('Receive it at the shop: 20 arrived in good condition.', async () => {
      await row.getByRole('button', { name: 'Receive' }).click();
      await page.locator('#transfer-dialog .trcv-good').first().waitFor();
      await page.waitForTimeout(1200);
      await h.post('/api/v1/transfer/receive', () => page.locator('#transfer-dialog-submit').click());
    });
    await showTransfer(`Transfer ${number} is Received.`);
    await h.open('/#/view/inventory', 'Check Inventory: 20 moved from the warehouse to the shop.');
    await showRow(page, h, state.store.name, `${state.store.name} now holds the 20 received units.`);
    state.transfer = { id, number };
    return `Transfer ${number} created, approved, dispatched and received`;
  },

  // --- POS: a cashier's shift (recorded as the CASHIER) ---------------------
  pos_open_till: async (page, h, state) => {
    if (!state.store?.name) throw new Error('Needs the shop fixture.');
    await h.open('/#/view/pos', 'Start of shift. The cashier opens the till at their shop before the first sale.');
    await chooseShop(page, h, state);
    await h.step('Open the session and count the opening cash float: 2,000.', async () => {
      await page.locator('#pos-session-open-btn').click();
      await page.locator('#custom-dialog-prompt-input').fill('2000');
      await h.post('/api/v1/pos/session/open', () => confirmDialog(page));
    }, '#pos-session-open-btn');
    await page.locator('#pos-session-close-btn:not(.hidden)').waitFor();
    await h.note('The session is open: the till can now sell, and every sale is counted into this session.');
    state.float = 2000;
    state.sales_cash = 0; // a new session starts its own cash count
    return `Till opened at ${state.store.name} with a 2,000 float`;
  },

  pos_sale: async (page, h, state) => {
    await h.open('/#/view/pos', 'Ring up a sale: find the item, check the price the system gives, take payment.');
    await chooseShop(page, h, state);
    await page.locator('#pos-session-close-btn:not(.hidden)').waitFor();
    await h.note('The till finds the session already open at this shop.');
    await h.step('Scan the barcode, or type part of the name and press Enter.', async () => {
      await h.type('#pos-sku-input', 'SOP Raw');
      await page.keyboard.press('Enter');
      // One match goes straight into the cart; several show a list to pick from.
      await page.locator('#pos-search-results .pos-result-row, #pos-cart-body tr').first().waitFor();
    }, '#pos-sku-input');
    const list = page.locator('#pos-search-results .pos-result-row').filter({ hasText: state.item.name });
    if (await list.count()) {
      await h.step(`Pick ${state.item.name}.`, () => list.first().click(), '#pos-search-results');
    } else {
      await h.note(`Only one product matches, so ${state.item.name} goes straight into the cart.`);
    }
    await page.locator('#pos-cart-body tr').first().waitFor();
    // Every bill has a customer: their mobile, or the store's walk-in customer.
    await h.step('The customer does not give a mobile number: choose Walk-in.',
      () => h.post('/api/v1/pos/walk-in-customer', () => page.locator('#pos-customer-walkin-btn').click()), '#pos-customer-walkin-btn');
    await page.waitForTimeout(1500); // the price quote lands from the server
    const total = Number((await page.locator('#pos-cart-total').innerText()).replace(/[^0-9.]/g, '')) || 0;
    await h.note(`The price, offers and tax come from the system, not the cashier. Total: ${total.toFixed(2)}.`);
    await h.step('Cash payment: enter the amount handed over.', () => h.type('#pos-cash-tendered', String(Math.ceil(total / 100) * 100 || 100)), '#pos-cash-tendered');
    const sale = await h.step('Complete the sale.', () => h.post('/api/v1/checkout', () => page.locator('#pos-checkout-btn').click()), '#pos-checkout-btn');
    await page.locator('#pos-completed-sale:not(.hidden)').waitFor().catch(() => {});
    await h.note('Sale complete: the till shows the change due and can print the receipt. Stock at the shop went down by one.');
    state.sales_cash = (state.sales_cash || 0) + total;
    state.sale = { id: sale.order_id || sale.id || sale.invoice_id || '', total };
    return `Sale completed for ${total.toFixed(2)} in cash`;
  },

  pos_close_till: async (page, h, state) => {
    const expected = (state.float || 0) + (state.sales_cash || 0);
    await h.open('/#/view/pos', 'End of shift. Count the cash in the drawer and close the session.');
    await chooseShop(page, h, state);
    await page.locator('#pos-session-close-btn:not(.hidden)').waitFor();
    await h.step(`Close the session. Count the drawer: ${expected.toFixed(2)} (float plus cash sales).`, async () => {
      await page.locator('#pos-session-close-btn').click();
      await page.locator('#custom-dialog-prompt-input').fill(expected.toFixed(2));
      await h.post('/api/v1/pos/session/close', () => confirmDialog(page));
    }, '#pos-session-close-btn');
    await h.step('Read the summary: expected cash, counted cash and the variance.', async () => {
      await page.locator('#custom-dialog-container:not(.hidden)').filter({ hasText: 'Variance' }).waitFor();
      await page.waitForTimeout(2500);
      await confirmDialog(page);
    }, '#custom-dialog-container');
    await page.locator('#pos-session-open-btn:not(.hidden)').waitFor();
    await h.note('The session is closed. Any difference between counted and expected cash is recorded for the manager.');
    return `Till closed; counted ${expected.toFixed(2)}`;
  },

  // --- Procurement: request for quotation -----------------------------------
  rfq_quotes: async (page, h, state) => {
    if (!state.vendor?.name || !state.item?.name) throw new Error('Record vendor_setup and item_setup first.');
    const where = state.location?.name || 'Audit Distribution Center';
    await h.open('/#/view/rfq', 'Ask suppliers for prices before ordering: list the items, invite the vendors, record their quotes, award one.');
    await h.step('Choose + New RFQ.', () => page.locator('[data-act-args*="rfq-screen:new"]').first().click(), '[data-act-args*="rfq-screen:new"]');
    await page.locator('#rfq-location').waitFor();
    await h.step(`Deliver to: ${where}.`, () => pick(page, h, '#rfq-location', 'Audit Dis', where), '#rfq-location');
    await h.step(`Item, picked from the list: ${state.item.name}.`, () => pick(page, h, '#rfq-line-item', 'SOP Raw', state.item.name), '#rfq-line-item');
    await h.step('Quantity: 500. Add the line.', async () => {
      await h.type('#rfq-line-qty', '500');
      await page.locator('#rfq-line-add').click();
      await page.locator('#rfq-lines tbody tr').first().waitFor();
    }, '#rfq-line-qty');
    const saved = await h.step('Create the RFQ. It starts as Draft.', () => h.post('/api/v1/rfq/save', () => page.locator('#rfq-create-btn').click()), '#rfq-create-btn');
    const base = `/api/v1/rfq/${encodeURIComponent(saved.id)}`;
    const detail = page.locator('#rfq-detail');
    await page.locator('#rfq-invite-vendor').waitFor();
    await h.step(`Invite ${state.vendor.name}.`, async () => {
      await pick(page, h, '#rfq-invite-vendor', 'SOP Supply', state.vendor.name);
      await h.post(`${base}/invite`, () => page.locator('#rfq-invite-btn').click());
    }, '#rfq-invite-vendor');
    const vendorRow = detail.locator('tr').filter({ hasText: state.vendor.name }).filter({ has: page.getByRole('button', { name: 'Key reply' }) }).first();
    await vendorRow.waitFor();
    await h.step('Mark the RFQ as Sent once you have sent it to them.', () =>
      h.post(`/api/v1/doc/RFQ/${encodeURIComponent(saved.id)}`, () => detail.getByRole('button', { name: 'Mark as Sent' }).click()));
    await detail.locator('.badge', { hasText: 'Sent' }).first().waitFor();
    await h.step(`${state.vendor.name} replied by phone. Choose Key reply on their row.`, () => vendorRow.getByRole('button', { name: 'Key reply' }).click());
    await page.locator('#quote-vendor').waitFor({ state: 'visible' });
    await h.step('Their price: 18.50 per unit, 7 days, 30 days to pay, valid for a month.', async () => {
      await page.locator('#rfq-quotes tr[data-line] .ql-price').first().fill('18.50');
      await page.locator('#rfq-quotes tr[data-line] .ql-lead').first().fill('7');
      await page.locator('#quote-valid').fill(new Date(Date.now() + 30 * 864e5).toISOString().slice(0, 10));
      await h.type('#quote-lead', '7');
      await h.type('#quote-terms', '30 days');
    }, '#rfq-quotes tr[data-line]');
    await h.step('Record the quote.', () => h.post(`${base}/quote`, () => page.locator('#quote-submit-btn').click()), '#quote-submit-btn');
    const award = detail.getByRole('button', { name: 'Award', exact: true }).first();
    await award.waitFor();
    await h.note('The comparison shows every quote side by side: landed cost, lead time, terms and the vendor\'s track record.');
    await h.step('Award the best quote.', () => award.click());
    await page.locator('#rfq-award-dialog.open #award-submit').waitFor();
    const won = await h.step('Create the purchase order. Other quotes are rejected.', () => h.post(`${base}/award`, () => page.locator('#award-submit').click()), '#award-submit');
    await page.locator('#custom-dialog-container:not(.hidden)').waitFor();
    await h.note(`Purchase order ${won.po_id} is created as a Draft from the quote.`);
    await confirmDialog(page);
    const notice = detail.locator('.notice', { hasText: 'Awarded' }).first();
    await notice.waitFor();
    await notice.evaluate(e => { e.scrollIntoView({ block: 'center' }); e.style.outline = '3px solid #f6c85f'; });
    await h.note('The RFQ is Closed and points to its purchase order. The buyer reviews it and sends it for approval.');
    state.rfq = { id: saved.id, po_id: won.po_id };
    return `RFQ awarded; Draft purchase order ${won.po_id}`;
  },

  // --- Accounts payable: the supplier's bill (recorded as the AP clerk) -----
  // The supplier's bill is keyed on the goods receipt (Stage 94); posting the
  // receipt makes the bill and matches it, so AP finds it Matched and pays it.
  supplier_invoice: async (page, h, state) => {
    if (!state.grn?.invoice_no) throw new Error('Record grn_post first.');
    const invoiceNo = state.grn.invoice_no;
    await h.open('/#/view/vendor-invoices', "The supplier's bill was keyed on the goods receipt. Posting the receipt recorded it here and matched it against the order and the receipt.");
    const row = await showRow(page, h, invoiceNo, `Bill ${invoiceNo} is Matched: it agrees with what was ordered and accepted, both including tax.`);
    await h.step('Pay it. The whole outstanding amount is offered.', async () => {
      await row.locator('[data-vi-pay]').click();
      await page.locator('#custom-dialog-container:not(.hidden)').waitFor();
      await page.waitForTimeout(1500);
      await h.post('/api/v1/procurement/vendor-invoice/pay', () => confirmDialog(page));
    }, `[data-vi-pay]`);
    await page.locator('#custom-dialog-container:not(.hidden)').filter({ hasText: 'Paid' }).waitFor();
    await h.step('The payment is recorded.', async () => { await page.waitForTimeout(1500); await confirmDialog(page); }, '#custom-dialog-container');
    await showRow(page, h, invoiceNo, 'Paid. The GST in the bill went to GST Input Credit, to be claimed back; the rest cleared the goods received.');
    state.vendor_invoice = { number: invoiceNo, amount: state.grn.amount };
    return `Supplier bill ${invoiceNo} paid (${Number(state.grn.amount || 0).toFixed(2)})`;
  },

  // --- Online orders: order desk -> warehouse -> accounts ----------------------
  oms_manual_order: async (page, h, state) => {
    if (!state.item?.name) throw new Error('Needs the item fixture.');
    const customer = `SOP Online Customer ${state.tag}`;
    const ref = `WEB-${slug(state.tag).toUpperCase()}-${Date.now().toString().slice(-5)}`;
    await h.open('/#/view/oms', 'A customer orders by phone. The order desk enters it as a manual order - it goes through the same checks a marketplace order does.');
    await h.step('Choose + New Order.', () => page.locator('[data-act-args*="oms-screen:new"]').first().click(), '[data-act-args*="oms-screen:new"]');
    await page.locator('#mo-customer').waitFor();
    await h.step(`Customer name: ${customer}.`, () => h.type('#mo-customer', customer), '#mo-customer');
    await h.step('Customer phone.', () => h.type('#mo-phone', '9876543210'), '#mo-phone');
    await h.step(`Your own reference for this order: ${ref}. Sending the same reference twice gives back the same order, never a duplicate.`, () => h.type('#mo-ref', ref), '#mo-ref');
    await h.step('Payment: Confirmed (the customer has paid). Cash on delivery also goes straight through; Pending holds the order.', () => page.locator('#mo-payment').selectOption('Confirmed'), '#mo-payment');
    await h.step('Shipping address: street, city, state and PIN code.', async () => {
      await h.type('#mo-address', '12 MG Road');
      await h.type('#mo-city', 'Bengaluru');
      const st = page.locator('#mo-state');
      if (await st.evaluate(e => e.tagName === 'SELECT')) await st.selectOption({ label: 'Karnataka' });
      else await h.type('#mo-state', 'Karnataka');
      await h.type('#mo-pin', '560001');
    }, '#mo-address');
    await h.step(`Scan the item's barcode: the list shows ${state.item.name}.`, () => pickItem(page, h, '#mo-lines-body [data-mo-field="sku"]', state.item), '#mo-lines-body');
    await h.step('Quantity 2, at the agreed price 25.', async () => {
      await h.type('#mo-lines-body [data-mo-field="qty"]', '2');
      await h.type('#mo-lines-body [data-mo-field="unit_price"]', '25');
    }, '#mo-lines-body');
    const created = await h.step('Create the order. You keyed it in, so it is accepted at once: its stock is reserved and it goes to Pick.', () => h.post('/api/v1/orders', () => page.locator('#mo-create').click()), '#mo-create');
    await page.locator('#custom-dialog-container:not(.hidden)').waitFor();
    await page.waitForTimeout(1500);
    await confirmDialog(page);
    await showOrderRow(page, h, created.order_id, 'Released', 'The order is in the queue as Released: the warehouse has its pick work, and it shows its source and your reference.');
    state.oms_order = { id: created.order_id, customer, ref };
    return `Manual order ${ref} created and Released to Pick`;
  },

  // Stage 88.2 retired the Fulfillment screen: a released order is worked on
  // the Order Management steps Pick -> Pack -> Ship. The training business has
  // one courier set up for every pincode (CourierServiceArea SOP-ALL-PIN), so
  // Pack books it; Ship makes the manifest, the gate pass and the handover.
  fulfillment_dispatch: async (page, h, state) => {
    if (!state.oms_order?.id) throw new Error('Record oms_manual_order first.');
    const o = state.oms_order;
    await h.open('/#/view/oms:pick', 'In the warehouse: a released order waits on Pick, at the location holding its stock.');
    const pickRow = page.locator('#view-root tbody tr').filter({ hasText: o.customer }).filter({ has: page.locator('[data-pick-act="scan"]') }).first();
    await outlineRow(page, h, pickRow, `The pick task for ${o.customer}: how many units to pick and how long it has waited.`);
    await h.step('Choose Pick. The scan panel lists what to pick.', () => pickRow.locator('[data-pick-act="scan"]').click());
    const scanPanel = page.locator('.oms-scan-panel');
    await scanPanel.locator('[data-scan-lines] tbody tr').first().waitFor();
    // The line shows the item's name; a scanner reads its barcode (147.2).
    const sku = state.item.barcode;
    // One scan is one unit: scan until the step offers its next move.
    const scanUntil = async (step, done) => {
      for (let i = 0; i < 8 && !(await page.locator(done).count()); i++) {
        await h.post(`/api/v1/wms/${step}-scan`, async () => {
          await h.type('.oms-scan-panel [data-scan-input]', sku);
          await page.keyboard.press('Enter');
        });
        await page.waitForTimeout(900);
      }
      await page.locator(done).first().waitFor();
    };
    await h.step(`Scan each unit as it comes off the shelf. Here its barcode, ${sku}, is typed into the scan box; the Picked count goes up with each scan.`,
      () => scanUntil('pick', '[data-to-pack]'), '.oms-scan-panel [data-scan-input]');
    await h.step('Every line is picked. Go to Pack.', () => page.locator('[data-to-pack]').click(), '[data-to-pack]');
    await page.locator('.oms-scan-panel', { hasText: 'Packing' }).waitFor();
    await h.step('At the packing bench, scan each unit into the box.', () => scanUntil('pack', '[data-pack-finish]'), '.oms-scan-panel [data-scan-input]');
    await h.step('Weigh the parcel, 0.8 kg, and tick that the bill and documents are in the box.', async () => {
      await h.type('[data-pack-weight]', '0.8');
      await page.locator('[data-pack-docs]').check();
    }, '.oms-pack-finish');
    await h.step('Pack, bill and label: the box is closed, its GST bill is made, and the courier for the delivery pincode is booked.', () =>
      h.post('/api/v1/marketplace/logistics/book', () => page.locator('[data-pack-finish]').click()), '[data-pack-finish]');
    await page.locator('[data-to-ship]').waitFor();
    const done = page.locator('.oms-pack-steps');
    await done.evaluate(e => { e.scrollIntoView({ block: 'center' }); e.style.outline = '3px solid #f6c85f'; e.style.outlineOffset = '3px'; });
    await h.note(`Done: ${(await done.innerText()).split('\n').map(s => s.trim()).filter(Boolean).join(' · ')}. The bill goes in the box.`);
    // A packed parcel joins a manifest only once its label is printed
    // (Stage 35.4.3); opening the label is what records the print.
    await h.step('Print label: the label opens with the courier and the AWB. Print it and stick it on the box.', async () => {
      await page.locator('[data-print-label]').click();
      await page.locator('#shipping-label-modal').waitFor();
      await page.waitForTimeout(1800);
    }, '[data-print-label]');
    await page.locator('#shipping-label-modal .modal-footer .btn-secondary').click();
    await page.locator('#shipping-label-modal').waitFor({ state: 'detached' });
    await h.step('Go to Ship.', () => page.locator('[data-to-ship]').click(), '[data-to-ship]');
    const carrier = page.locator('[data-manifest-carrier]').first();
    await carrier.waitFor();
    const made = await h.step('The booked parcels are grouped by courier and location. Make the manifest: the list the courier signs for.', () =>
      h.post('/api/v1/marketplace/logistics/manifest', () => carrier.click()), '[data-manifest-carrier]');
    const mid = made.manifest_id;
    const gp = page.locator(`[data-gp-new="${mid}"]`);
    await gp.waitFor();
    await h.step('Raise the gate pass for the vehicle collecting it: vehicle number, then the driver.', async () => {
      await gp.click();
      await page.locator('#custom-dialog-prompt-input').fill('KA01AB1234');
      await page.waitForTimeout(700);
      await confirmDialog(page);
      await page.locator('#custom-dialog-container:not(.hidden)', { hasText: 'Driver' }).waitFor();
      await page.locator('#custom-dialog-prompt-input').fill('Ravi 9876543210');
      await page.waitForTimeout(700);
      await h.post('/api/v1/oms/gate-passes', () => confirmDialog(page));
    }, `[data-gp-new="${mid}"]`);
    const manifestRow = page.locator('#view-root tr', { has: page.locator(`[data-handover="${mid}"]`) }).first();
    await manifestRow.getByText('Issued').waitFor();
    await outlineRow(page, h, manifestRow, `Manifest ${mid}: its gate pass is Issued to vehicle KA01AB1234.`);
    await h.step('Hand over to the courier as the vehicle leaves. The parcels are dispatched: stock leaves the books and the order moves to Shipped.', async () => {
      await page.locator(`[data-handover="${mid}"]`).click();
      await page.locator('#custom-dialog-container:not(.hidden)').waitFor();
      await page.waitForTimeout(1200);
      await h.post('/api/v1/marketplace/logistics/manifest/handover', () => confirmDialog(page));
    }, `[data-handover="${mid}"]`);
    const onTheWay = page.locator('#view-root tr').filter({ hasText: o.customer }).filter({ has: page.locator('[data-track="In-Transit"]') }).first();
    await outlineRow(page, h, onTheWay, 'The parcel is On the way. Mark it In transit and Delivered as the courier reports it.');
    await h.open('/#/view/oms', 'Back in Order Management, the order itself has moved on.');
    // Pack made the order's GST invoice and posted it. SalesInvoice has no
    // sales_order_id filter, so find it in the list rows, which carry the order id.
    const invoices = await page.evaluate(async () => {
      const r = await fetch('/api/v1/doc/SalesInvoice?limit=50', { headers: { Authorization: `Bearer ${localStorage.getItem('erp_token')}` } });
      return r.ok ? r.json() : [];
    });
    const invoice = (Array.isArray(invoices) ? invoices : []).find(v => v.sales_order_id === o.id && v.status !== 'Cancelled');
    if (!invoice) throw new Error(`No sales invoice found for order ${o.id}.`);
    o.invoice_id = invoice.id;
    await showOrderRow(page, h, o.id, 'Shipped', 'Shipped. Its GST invoice was made and posted at Pack; accounts settles it when the customer pays.');
    return `Order ${o.ref} picked, packed, handed to the courier and Shipped`;
  },

  sales_invoice_post: async (page, h, state) => {
    if (!state.oms_order?.id) throw new Error('Record fulfillment_dispatch first.');
    if (!state.oms_order.invoice_id) throw new Error('Record fulfillment_dispatch again: it finds the order\'s invoice.');
    const invoiceId = state.oms_order.invoice_id;
    await h.open('/#/view/sales-invoices', 'Accounts: the shipped order\'s invoice was made and posted at Pack.');
    // The row shows the invoice number; its status cell carries the id.
    const row = () => page.locator('#view-root tbody tr', { has: page.locator(`[data-einv-id="${invoiceId}"]`) }).first();
    await outlineRow(page, h, row(), 'Approved: billed for what went in the box, with its GST, and ageing as a receivable.');
    await h.step('When the customer\'s payment arrives, Settle it. The whole amount owed is offered.', async () => {
      await row().locator(`[data-si-settle="${invoiceId}"]`).click();
      await page.locator('#custom-dialog-container:not(.hidden)').waitFor();
      await page.waitForTimeout(1200);
      await h.post(`/api/v1/finance/sales-invoice/${encodeURIComponent(invoiceId)}/settle`, () => confirmDialog(page));
    });
    // The receipt is confirmed in its own alert; the list reloads once it is closed.
    await h.step('The receipt is recorded: the invoice is fully settled.', async () => {
      await page.locator('#custom-dialog-container:not(.hidden) #custom-dialog-ok-btn').waitFor();
      await page.waitForTimeout(1500);
      await confirmDialog(page);
    });
    await row().getByText('Paid').waitFor();
    await outlineRow(page, h, row(), 'Paid. The order is complete from order desk to cash.');
    return `Invoice for order ${state.oms_order.ref} settled`;
  },

  // --- Finance: a manual journal (accountant raises, controller approves) --
  jv_create: async (page, h, state) => {
    const narration = `Accrue October salaries ${state.tag}`;
    await h.open('/#/view/journal-vouchers', 'Month end: salaries for October are owed but not paid until next month. Record the cost now with a journal voucher.');
    const newJv = '[data-act-args*="jv-screen:new"]';
    await h.step('Choose + New Journal Voucher.', () => page.locator(newJv).first().click(), newJv);
    await page.locator('#jv-lines-body tr').first().waitFor();
    await h.step(`Narration - say what the entry is for: ${narration}.`, () => h.type('#jv-narration', narration), '#jv-narration');
    const line = (i, field) => `#jv-lines-body [data-jv-line="${i}"][data-jv-field="${field}"]`;
    await h.step('Line 1: debit 5500 Salary Expense, 45,000.', async () => {
      await h.type(line(0, 'account'), '5500');
      await h.type(line(0, 'debit'), '45000');
    }, '#jv-lines-body');
    await h.step('Line 2: credit 2400 Salary Payable, 45,000. The account name appears under each code.', async () => {
      await h.type(line(1, 'account'), '2400');
      await h.type(line(1, 'credit'), '45000');
    }, '#jv-lines-body');
    await h.step('Debits equal credits - the voucher shows Balanced.', async () => { await page.locator('#jv-balance-note .badge-success').waitFor(); }, '#jv-balance-note');
    const created = await h.step('Save as Draft.', () => h.post('/api/v1/finance/journal-voucher', () => page.locator('#jv-save-btn').click()), '#jv-save-btn');
    await page.locator('#view-root tbody tr').filter({ hasText: narration }).first().waitFor();
    const row = () => page.locator('#view-root tbody tr').filter({ hasText: narration }).first();
    await showRow(page, h, narration, 'Draft: nothing is posted yet.');
    await h.step('Submit it for approval. A second person must approve before it reaches the ledger.', () =>
      h.post('/api/v1/approval/submit', () => row().getByRole('button', { name: 'Submit for Approval' }).click()));
    await showRow(page, h, narration, 'Pending Approval: the controller sees it on their Approvals screen.');
    state.jv = { id: created.id, narration };
    return `Journal voucher saved and submitted: ${narration}`;
  },

  jv_approve: async (page, h, state) => {
    if (!state.jv?.id) throw new Error('Record jv_create first.');
    await h.open('/#/view/approvals', 'The controller signs in with their own account. The voucher is waiting in their approval queue.');
    const row = page.locator('#inbox-approvals tbody tr').filter({ has: page.locator(`[data-act-args*="${state.jv.id}"]`) }).first();
    await row.waitFor();
    await h.step('Check the voucher in the queue: who raised it and the amount.', async () => {
      await row.evaluate(e => { e.scrollIntoView({ block: 'center' }); e.style.outline = '3px solid #f6c85f'; });
    });
    await h.step('Approve. The person who raised a voucher can never approve it.', async () => {
      const reply = await h.post('/api/v1/approval/decide', () => row.getByRole('button', { name: 'Approve' }).click());
      if (reply.decision !== 'Approved') throw new Error('JV approval did not complete.');
    });
    await h.open('/#/view/journal-vouchers', 'Approval posted the voucher to the ledger.');
    await showRow(page, h, state.jv.narration, 'Posted. If it was wrong, Reverse creates the mirror entry - nothing is ever edited after posting.');
    return `Journal voucher approved and posted: ${state.jv.narration}`;
  },

  // --- Stock: bins, putaway and moving stock between bins -----------------
  bin_second_setup: async (page, h, state) => {
    if (!state.zone || !state.location?.name) throw new Error('Record bin_zone_setup first.');
    const bin = `SOP-B1-${slug(state.tag).toUpperCase()}`;
    await h.open('/#/setup/Bin', 'Add a second bin in the same zone - the one the stock will move to.');
    await h.step('Choose New Bin.', () => page.locator('#doc-create-button').click(), '#doc-create-button');
    await page.locator('#dynamic-modal.open').waitFor();
    await h.step(`Bin code: ${bin}.`, () => h.type('#dyn-field-Bin-bin_code', bin), '#dyn-field-Bin-bin_code');
    await h.step(`Warehouse, by name: ${state.location.name}.`,
      () => page.locator('#dyn-field-Bin-location').selectOption({ label: state.location.name }), '#dyn-field-Bin-location');
    await h.step(`Zone: pick the existing ${state.zone}.`, () => pick(page, h, '#dyn-field-Bin-zone', state.zone.split(' ')[0], state.zone), '#dyn-field-Bin-zone');
    await h.step('Save the bin.', () => h.saveDoc('Bin'), '#dynamic-modal-form button[type="submit"]');
    await showRow(page, h, bin, `Bin ${bin} is ready.`);
    state.bin2 = bin;
    return `Second bin ${bin} saved`;
  },

  putaway_stock: async (page, h, state) => {
    if (!state.bin || !state.item?.name) throw new Error('Needs the bin and item fixtures.');
    await h.open('/#/view/putaway', 'Received stock sits at the warehouse until it is put away into a bin. Record where it went.');
    await h.step(`Bin: ${state.bin}.`, () => pick(page, h, '#putaway-bin', state.bin.slice(0, 6), state.bin), '#putaway-bin');
    await h.step(`Item: scan its barcode. The list shows ${state.item.name}.`, () => pickItem(page, h, '#putaway-sku', state.item), '#putaway-sku');
    await h.step('Quantity: 30.', () => h.type('#putaway-qty', '30'), '#putaway-qty');
    await h.step('Put Away.', () => h.post('/api/v1/wms/putaway', () => page.locator('#putaway-submit-btn').click()), '#putaway-submit-btn');
    // A success toast repeats what went where; the form clears for the next scan.
    await page.locator('#toast-container .toast-success').first().waitFor();
    await h.step('The confirmation repeats what went where, and the form clears for the next scan.', () => page.waitForTimeout(2200), '#toast-container');
    await h.note(`30 of ${state.item.name} are now recorded in bin ${state.bin}.`);
    return `30 units put away into ${state.bin}`;
  },

  location_movement: async (page, h, state) => {
    if (!state.bin || !state.bin2) throw new Error('Record bin_second_setup and putaway_stock first.');
    await h.open('/#/view/location-movement', 'Re-slot stock inside the warehouse: move part of a bin to another bin. The stock stays at the same location.');
    await openNew(page, h, 'locmove-screen', 'New Movement');
    await h.step(`Pick the warehouse by name: ${state.location.name}.`, () => pick(page, h, '#locmove-location', 'Audit Dis', state.location.name), '#locmove-location');
    await page.locator('#locmove-result tbody tr').first().waitFor();
    const fromRow = page.locator('#locmove-result tbody tr').filter({ hasText: state.bin }).filter({ hasText: state.item.name }).first();
    await fromRow.waitFor();
    await h.step(`Every bin with stock is listed. Choose Move on ${state.bin}.`, () => fromRow.getByRole('button', { name: 'Move' }).click(), '#locmove-result');
    await page.locator('#locmove-form:not(.hidden)').waitFor();
    await h.step('Quantity to move: 12.', () => h.type('#locmove-qty', '12'), '#locmove-qty');
    await h.step(`To bin: ${state.bin2} - only bins at this warehouse are offered.`, async () => {
      await page.locator('#locmove-to-bin').click();
      await page.waitForTimeout(900);
      await page.locator('.typeahead-menu .typeahead-item').filter({ hasText: state.bin2 }).first().click();
    }, '#locmove-to-bin');
    await h.step('Reason, for the record.', () => h.type('#locmove-reason', 'Re-slot fast mover near packing'), '#locmove-reason');
    await h.step('Move Stock.', () => h.post('/api/v1/wms/bin-move', () => page.locator('#locmove-submit-btn').click()), '#locmove-submit-btn');
    // The screen returns to the movement history, the move on top.
    const moved = page.locator('#locmove-history tbody tr').filter({ hasText: state.bin }).filter({ hasText: state.bin2 }).first();
    await moved.waitFor();
    await outlineRow(page, h, moved, `The move is on top of the history: 12 units from ${state.bin} to ${state.bin2}, with the reason. Both bins are on the stock ledger record.`);
    return `12 units moved from ${state.bin} to ${state.bin2}`;
  },

  // --- Offers: a product group, an offer on it, and the till applying it ----
  product_group_setup: async (page, h, state) => {
    if (!state.item?.name) throw new Error('Needs the item fixture.');
    const group = `SOP Festive Range ${state.tag}`;
    await h.open('/#/setup/PIMProductGroup', 'A product group is a named set of products - the target for an offer, a bulk edit or an export.');
    await h.step('Choose New Product Group.', () => page.locator('#doc-create-button').click(), '#doc-create-button');
    await page.locator('#dynamic-modal.open').waitFor();
    const f = n => `#dyn-field-PIMProductGroup-${n}`;
    if (await page.locator(f('code')).count() && await page.locator(f('code')).isEditable()) {
      await h.step('Give it a short code.', () => h.type(f('code'), `FEST-${slug(state.tag).toUpperCase()}`), f('code'));
    }
    await h.step(`Name: ${group}.`, () => h.type(f('name'), group), f('name'));
    await h.step('Type: Static - you choose the products. (Dynamic follows rules instead.)', () => page.locator(f('group_type')).selectOption('Static'), f('group_type'));
    await h.step(`Add a product line and scan the item's barcode: ${state.item.name}.`, async () => {
      await page.locator('#dynamic-modal .json-line-add').first().click();
      await pickItem(page, h, '#dynamic-modal [data-line-key="item_code"]', state.item);
    }, '#dynamic-modal .json-line-editor');
    if (await page.locator(f('status')).count()) await page.locator(f('status')).selectOption('Active');
    const reply = await h.step('Save the group.', () => h.saveDoc('PIMProductGroup'), '#dynamic-modal-form button[type="submit"]');
    await showRow(page, h, group, `${group} holds the products chosen for the promotion.`);
    state.product_group = { name: group, id: reply.id };
    return `Product group saved: ${group}`;
  },

  offer_setup: async (page, h, state) => {
    if (!state.product_group?.name) throw new Error('Record product_group_setup first.');
    const offer = `Festive 10% ${state.tag}`;
    const coupon = `FEST${slug(state.tag).toUpperCase()}`;
    await h.open('/#/setup/Offer', 'Offers are records, not code: create one and every till applies it on the next sale.');
    await h.step('Choose New Offer.', () => page.locator('#doc-create-button').click(), '#doc-create-button');
    await page.locator('#dynamic-modal.open').waitFor();
    const f = n => `#dyn-field-Offer-${n}`;
    await h.step(`Name: ${offer}.`, () => h.type(f('name'), offer), f('name'));
    await h.step('Offer type: Percentage Off.', () => page.locator(f('offer_type')).selectOption('Percentage Off'), f('offer_type'));
    await h.step('Applies to: Product Group. The Scope Value box turns into a group picker.', () => page.locator(f('scope')).selectOption('Product Group'), f('scope'));
    await h.step(`Pick the group: ${state.product_group.name}.`, async () => {
      await page.locator(f('scope_value')).click();
      await page.waitForTimeout(900);
      await page.locator('.typeahead-menu .typeahead-item').filter({ hasText: state.product_group.name }).first().click();
    }, f('scope_value'));
    await h.step('Discount: 10 percent.', () => h.type(f('discount_pct'), '10'), f('discount_pct'));
    await h.step(`Coupon code: ${coupon} - the offer applies only when the cashier enters it.`, () => h.type(f('coupon_code'), coupon), f('coupon_code'));
    await h.step('Stackable: No. Status: Active.', async () => {
      await page.locator(f('stackable')).selectOption('No');
      await page.locator(f('status')).selectOption('Active');
    }, f('status'));
    await h.step('Save the offer.', () => h.saveDoc('Offer'), '#dynamic-modal-form button[type="submit"]');
    await showRow(page, h, offer, 'The offer is live at every till.');
    state.offer = { name: offer, coupon };
    return `Offer ${offer} live for ${state.product_group.name} with coupon ${coupon}`;
  },

  pos_sale_coupon: async (page, h, state) => {
    if (!state.offer?.coupon) throw new Error('Record offer_setup first.');
    await h.open('/#/view/pos', 'A customer brings the festive coupon. Ring up the sale and apply it.');
    await chooseShop(page, h, state);
    // The shift chapter ends with the till closed, so this sale opens a new session first.
    await page.locator('#pos-session-close-btn:not(.hidden), #pos-session-open-btn:not(.hidden)').first().waitFor();
    if (await page.locator('#pos-session-open-btn:not(.hidden)').count()) {
      await h.step('The till is closed: open the session with the 2,000 float first.', async () => {
        await page.locator('#pos-session-open-btn').click();
        await page.locator('#custom-dialog-prompt-input').fill('2000');
        await h.post('/api/v1/pos/session/open', () => confirmDialog(page));
      }, '#pos-session-open-btn');
      state.float = 2000;
      state.sales_cash = 0;
    }
    await page.locator('#pos-session-close-btn:not(.hidden)').waitFor();
    await h.step('Scan or search the item and press Enter.', async () => {
      await h.type('#pos-sku-input', 'SOP Raw');
      await page.keyboard.press('Enter');
      await page.locator('#pos-search-results .pos-result-row, #pos-cart-body tr').first().waitFor();
    }, '#pos-sku-input');
    const list = page.locator('#pos-search-results .pos-result-row').filter({ hasText: state.item.name });
    if (await list.count()) await h.step(`Pick ${state.item.name}.`, () => list.first().click(), '#pos-search-results');
    await page.locator('#pos-cart-body tr').first().waitFor();
    await h.step('No mobile number given: choose Walk-in.',
      () => h.post('/api/v1/pos/walk-in-customer', () => page.locator('#pos-customer-walkin-btn').click()), '#pos-customer-walkin-btn');
    await page.waitForTimeout(1500);
    const before = Number((await page.locator('#pos-cart-total').innerText()).replace(/[^0-9.]/g, '')) || 0;
    await h.step(`Type the coupon code: ${state.offer.coupon}.`, async () => {
      await h.type('#pos-coupon-code', state.offer.coupon);
      await page.locator('#pos-coupon-code').press('Tab');
      await page.locator('#pos-offers-row:not(.hidden)').filter({ hasText: state.offer.name }).waitFor();
      await page.waitForTimeout(1200);
    }, '#pos-coupon-code');
    const total = Number((await page.locator('#pos-cart-total').innerText()).replace(/[^0-9.]/g, '')) || 0;
    await h.step(`The offer appears above the total, by name, with what it took off: ${before.toFixed(2)} becomes ${total.toFixed(2)}.`, async () => {}, '#pos-offers-row');
    await h.step('Cash payment.', () => h.type('#pos-cash-tendered', String(Math.ceil(total / 100) * 100 || 100)), '#pos-cash-tendered');
    await h.step('Complete the sale. The discount is recomputed by the server - the till cannot inflate it.', () => h.post('/api/v1/checkout', () => page.locator('#pos-checkout-btn').click()), '#pos-checkout-btn');
    await page.locator('#pos-completed-sale:not(.hidden)').waitFor().catch(() => {});
    await h.note('Sale complete with the festive discount applied.');
    state.sales_cash = (state.sales_cash || 0) + total;
    return `Coupon ${state.offer.coupon} applied: ${before.toFixed(2)} -> ${total.toFixed(2)}`;
  },

  // --- HR: employee, salary, leave, attendance, payroll -------------------
  employee_setup: async (page, h, state) => {
    if (!state.department?.name) throw new Error('Record department_setup first.');
    const name = `SOP Store Associate ${state.tag}`;
    await h.open('/#/setup/Employee', 'Every HR and payroll action starts from the employee record.');
    await h.step('Choose New Employee.', () => page.locator('#doc-create-button').click(), '#doc-create-button');
    await page.locator('#dynamic-modal.open').waitFor();
    const f = n => `#dyn-field-Employee-${n}`;
    await h.step(`Name: ${name}.`, () => h.type(f('name'), name), f('name'));
    await h.step(`Department, picked from the list: ${state.department.name}. Then the designation.`, async () => {
      await pick(page, h, f('department'), 'SOP Buying', state.department.name);
      await h.type(f('designation'), 'Store Associate');
    }, f('department'));
    await h.step('Date of joining.', () => page.locator(f('date_of_joining')).fill('2026-10-01'), f('date_of_joining'));
    await h.step('Bank account and IFSC - payroll cannot post without them.', async () => {
      await h.type(f('bank_account_no'), '50100012345678');
      await h.type(f('bank_ifsc'), 'HDFC0001234');
    }, f('bank_account_no'));
    const reply = await h.step('Save. Status starts as Active.', () => h.saveDoc('Employee'), '#dynamic-modal-form button[type="submit"]');
    await showRow(page, h, name, `${name} is on the employee list; the system gave them a code.`);
    state.employee = { name, id: reply.id };
    return `Employee saved: ${name}`;
  },

  salary_structure_setup: async (page, h, state) => {
    if (!state.employee?.name) throw new Error('Record employee_setup first.');
    await h.open('/#/setup/SalaryStructure', 'A salary structure says what the employee earns and what is deducted. Payroll reads it every month.');
    await h.step('Choose New Salary Structure.', () => page.locator('#doc-create-button').click(), '#doc-create-button');
    await page.locator('#dynamic-modal.open').waitFor();
    const f = n => `#dyn-field-SalaryStructure-${n}`;
    await h.step(`Employee, by name: ${state.employee.name}.`, () => page.locator(f('employee_id')).selectOption({ label: state.employee.name }), f('employee_id'));
    await h.step('Basic 20,000 and HRA 8,000 a month.', async () => {
      await h.type(f('basic'), '20000');
      await h.type(f('hra'), '8000');
    }, f('basic'));
    await h.step('Provident fund 12 percent, professional tax 200.', async () => {
      await h.type(f('pf_percent'), '12');
      await h.type(f('pt_amount'), '200');
    }, f('pf_percent'));
    await h.step('Effective from 1 October.', () => page.locator(f('effective_from')).fill('2026-10-01'), f('effective_from'));
    await h.step('Save.', () => h.saveDoc('SalaryStructure'), '#dynamic-modal-form button[type="submit"]');
    await page.waitForTimeout(800);
    await h.note(`${state.employee.name} now has a salary structure; payroll can run.`);
    return `Salary structure saved for ${state.employee.name}`;
  },

  leave_request: async (page, h, state) => {
    if (!state.employee?.name) throw new Error('Record employee_setup first.');
    await h.open('/#/view/hr:leave', 'HR, Leave: an employee needs two days of casual leave.');
    await openNew(page, h, 'hr-leave', 'Apply Leave');
    await page.locator('#leave-type').waitFor();
    await h.step(`Employee, by name: ${state.employee.name}.`, () => pick(page, h, '#leave-employee', 'SOP Store', state.employee.name), '#leave-employee');
    await h.step('Casual leave, 12 to 13 October, 2 days.', async () => {
      await page.locator('#leave-type').selectOption('Casual');
      await page.locator('#leave-from').fill('2026-10-12');
      await page.locator('#leave-to').fill('2026-10-13');
      await h.type('#leave-days', '2');
    }, '#leave-type');
    const saved = await h.step('Apply.', () => h.post('/api/v1/doc/Leave', () => page.locator('#leave-save-btn').click()), '#leave-save-btn');
    // The new request is found by its own leave number, not by its dates.
    const code = saved.code || saved.id || saved.data?.code || saved.data?.id;
    if (!code) throw new Error('The leave was saved without a number.');
    const row = () => page.locator('#view-root tbody tr').filter({ hasText: code }).first();
    await showRow(page, h, code, `Leave ${code} is pending a decision.`);
    await h.step('The manager approves it.', async () => {
      await row().getByRole('button', { name: 'Approve' }).click();
      await row().filter({ hasText: 'Approved' }).waitFor();
    });
    await showRow(page, h, code, 'Approved. Approved leave is what payroll and the attendance view count.');
    return `Leave applied and approved for ${state.employee.name}`;
  },

  attendance_mark: async (page, h, state) => {
    if (!state.employee?.name) throw new Error('Record employee_setup first.');
    await h.open('/#/view/hr:attendance', 'HR, Attendance: mark the day\'s attendance.');
    await openNew(page, h, 'hr-att', 'Mark Attendance');
    await page.locator('#att-status').waitFor();
    await h.step(`Employee: ${state.employee.name}.`, () => pick(page, h, '#att-employee', 'SOP Store', state.employee.name), '#att-employee');
    await h.step('Date: 9 October. Status: Present.', async () => {
      await page.locator('#att-date').fill('2026-10-09');
      await page.locator('#att-status').selectOption('Present');
    }, '#att-date');
    if (state.store?.name) await h.step(`Location: ${state.store.name}.`, () => pick(page, h, '#att-location', 'Audit Retail', state.store.name), '#att-location');
    await h.step('Save.', () => h.post('/api/v1/doc/Attendance', () => page.locator('#att-save-btn').click()), '#att-save-btn');
    await showRow(page, h, '2026-10-09', `${state.employee.name} is marked Present.`);
    return `Attendance marked for ${state.employee.name}`;
  },

  payroll_run: async (page, h, state) => {
    if (!state.employee?.name) throw new Error('Record salary_structure_setup first.');
    await h.open('/#/view/hr:payroll', 'HR, Payroll: at month end, run payroll for the employee and post it to the accounts.');
    await openNew(page, h, 'hr-payroll', 'Run Payroll');
    await page.locator('#payroll-run-btn').waitFor();
    await h.step(`Employee: ${state.employee.name}.`, () => pick(page, h, '#payroll-run-employee', 'SOP Store', state.employee.name), '#payroll-run-employee');
    await h.step('Period: 1 to 31 October.', async () => {
      await page.locator('#payroll-run-from').fill('2026-10-01');
      await page.locator('#payroll-run-to').fill('2026-10-31');
    }, '#payroll-run-from');
    await h.step('Run Payroll. The payslip is worked out from the salary structure - earnings, PF, tax, any loan.', () =>
      h.post('/api/v1/hr/run-payroll', () => page.locator('#payroll-run-btn').click()), '#payroll-run-btn');
    const row = () => page.locator('#view-root tbody tr').filter({ hasText: 'Draft' }).filter({ has: page.getByRole('button', { name: 'Post to GL' }) }).last();
    await row().waitFor();
    await row().evaluate(e => { e.scrollIntoView({ block: 'center' }); e.style.outline = '3px solid #f6c85f'; });
    await h.note('A Draft payslip - check it before it reaches the accounts.');
    await h.step('Post to GL. Salary expense and the amounts owed (salary, PF, tax) are booked.', async () => {
      await row().getByRole('button', { name: 'Post to GL' }).click();
      await page.locator('#custom-dialog-container:not(.hidden)').waitFor();
      await h.post('/api/v1/hr/post-payslip', () => confirmDialog(page));
    });
    await page.waitForTimeout(1200);
    await h.note('Posted. The payslip and its accounting are done.');
    return `Payroll run and posted for ${state.employee.name}`;
  },

  // --- Manufacturing: finished good, bill of materials, production order ---
  fg_item_setup: async (page, h, state) => {
    if (!state.category?.name || !state.product_type?.name) throw new Error('Record item_name_setup first.');
    const name = `SOP Gift Box ${state.tag}`;
    const sku = `SOP-FG-${slug(state.tag).toUpperCase()}`;
    await h.open('/#/setup/ItemName', 'The finished product needs its own item name, then its own item record, before it can be made.');
    await h.step('Choose New Item Name.', () => page.locator('#doc-create-button').click(), '#doc-create-button');
    await page.locator('#dynamic-modal.open').waitFor();
    const n = f => `#dyn-field-ItemName-${f}`;
    await h.step(`Category ${state.category.name}, product type ${state.product_type.name}.`, async () => {
      await page.locator(n('category')).selectOption({ label: state.category.name });
      await page.locator(n('product_type')).selectOption({ label: state.product_type.name });
    }, n('category'));
    await h.step(`Name: ${name}. HSN 9999, GST 18 percent.`, async () => {
      await h.type(n('name'), name);
      await h.type(n('hsn_code'), '9999');
      await h.type(n('gst_rate'), '18');
    }, n('name'));
    await h.step('Save the item name.', () => h.saveDoc('ItemName'), '#dynamic-modal-form button[type="submit"]');
    await h.open('/#/setup/Item', 'Now the item record for the finished product.');
    state.fg_item = await newItem(page, h, { itemName: name, name, barcode: sku, cost: 60 });
    return `Finished-good item saved: ${name}`;
  },

  bom_create: async (page, h, state) => {
    if (!state.fg_item?.name || !state.item?.id) throw new Error('Needs the finished-good and raw-material items.');
    const bom = `BOM-${slug(state.tag).toUpperCase()}`;
    await h.open('/#/view/manufacturing', 'A bill of materials says what goes into one finished product.');
    const newBom = '[data-act-args*="mfg-orders:bom"]';
    await h.step('Choose + New BOM.', () => page.locator(newBom).first().click(), newBom);
    await page.locator('#bom-code').waitFor();
    await h.step(`BOM code: ${bom}.`, () => h.type('#bom-code', bom), '#bom-code');
    await h.step(`Finished product: scan its barcode. The list shows ${state.fg_item.name}.`, () => pickItem(page, h, '#bom-parent-item', state.fg_item), '#bom-parent-item');
    await h.step(`Component: 2 of ${state.item.name} per box.`, async () => {
      await pickItem(page, h, '#bom-lines tr:first-child .bom-line-sku', state.item);
      await h.type('#bom-lines tr:first-child .bom-line-qty', '2');
    }, '#bom-lines');
    await h.step('Create BOM.', () => h.post('/api/v1/doc/BOM', () => page.locator('#bom-create-btn').click()), '#bom-create-btn');
    // Read it back where it is used: the BOM list of a new production order.
    const newOrder = '[data-act-args*="mfg-orders:new"]';
    await page.locator(newOrder).first().waitFor();
    await h.step('Open + New Production Order: the BOM is offered there.', async () => {
      await page.locator(newOrder).first().click();
      const sel = page.locator('#po-mfg-bom');
      await sel.waitFor();
      await sel.selectOption(await sel.locator('option').filter({ hasText: bom }).first().getAttribute('value'));
      await sel.evaluate(e => { e.scrollIntoView({ block: 'center' }); e.style.outline = '3px solid #f6c85f'; });
    }, '#po-mfg-bom');
    await h.note(`${bom} is saved: one ${state.fg_item.name} uses 2 of ${state.item.name}. It is now offered when creating a production order.`);
    state.bom = bom;
    return `BOM ${bom} created`;
  },

  production_order: async (page, h, state) => {
    if (!state.bom) throw new Error('Record bom_create first.');
    await h.open('/#/view/manufacturing', 'Make 10 gift boxes at the warehouse.');
    const newOrder = '[data-act-args*="mfg-orders:new"]';
    await h.step('Choose + New Production Order.', () => page.locator(newOrder).first().click(), newOrder);
    await page.locator('#po-mfg-bom').waitFor();
    await h.step(`Choose the BOM: ${state.bom}.`, async () => {
      const sel = page.locator('#po-mfg-bom');
      const value = await sel.locator('option').filter({ hasText: state.bom }).first().getAttribute('value');
      await sel.selectOption(value);
    }, '#po-mfg-bom');
    await h.step('Quantity: 10.', () => h.type('#po-mfg-qty', '10'), '#po-mfg-qty');
    await h.step(`Location, by name: ${state.location.name}.`, () => pick(page, h, '#po-mfg-location', 'Audit Dis', state.location.name), '#po-mfg-location');
    const reply = await h.step('Create Order. It starts as a Draft.', () => h.post('/api/v1/doc/ProductionOrder', () => page.locator('#po-mfg-create-btn').click()), '#po-mfg-create-btn');
    const id = reply.id;
    const number = reply.code || id;
    // The list shows the order number; its buttons carry the id.
    const row = () => page.locator('#view-root tbody tr').filter({ has: page.locator(`[data-act-args*="${id}"]`) }).first();
    await showRowById(page, h, id, `${number} is a Draft.`);
    await h.step('Issue Material: the 20 units of raw material leave stock into production.', () =>
      h.post('/api/v1/manufacturing/issue-material', () => row().getByRole('button', { name: 'Issue Material' }).click()));
    await showRowById(page, h, id, 'Material Issued - the work is in progress.');
    await h.step('Complete (Receive FG) when the boxes are made: the 10 finished boxes come into stock.', async () => {
      await row().getByRole('button', { name: 'Complete (Receive FG)' }).click();
      await page.locator('#custom-dialog-container:not(.hidden)').waitFor();
      await h.post('/api/v1/manufacturing/complete', () => confirmDialog(page));
    });
    await showRow(page, h, number, 'Completed. Raw material went out, finished goods came in, each at its cost.');
    state.production_order = { id };
    return `Production order ${id}: 10 made, materials issued and finished goods received`;
  },

  // --- PIM: family, attribute, link, item in family, enrich -------------
  pim_family_setup: async (page, h, state) => {
    const family = `SOP Gift Hampers ${state.tag}`;
    await h.open('/#/setup/ProductFamily', 'PIM enriches products for selling. Step one: a product family - one per kind of product. Families are under Setup.');
    await h.step('Choose New.', () => page.locator('#doc-create-button').click(), '#doc-create-button');
    await page.locator('#dynamic-modal.open').waitFor();
    const f = n => `#dyn-field-ProductFamily-${n}`;
    if (await page.locator(f('code')).isEditable().catch(() => false)) await h.step('A short code.', () => h.type(f('code'), `HAMPER-${slug(state.tag).toUpperCase()}`), f('code'));
    await h.step(`Family name: ${family}.`, () => h.type(f('name'), family), f('name'));
    await h.step('Save.', () => h.saveDoc('ProductFamily'), '#dynamic-modal-form button[type="submit"]');
    await showRow(page, h, family, `${family} is the family every hamper is scored against.`);
    state.pim_family = { name: family };
    return `Product family saved: ${family}`;
  },

  pim_attribute_setup: async (page, h, state) => {
    await h.open('/#/setup/ProductAttributeDef', 'Step two: the attributes that describe the products - reusable across families. Attribute Definitions are under Setup.');
    await h.step('Choose New.', () => page.locator('#doc-create-button').click(), '#doc-create-button');
    await page.locator('#dynamic-modal.open').waitFor();
    const f = n => `#dyn-field-ProductAttributeDef-${n}`;
    const label = `Pack Size ${state.tag}`;
    if (await page.locator(f('code')).isEditable().catch(() => false)) await h.step('A short code.', () => h.type(f('code'), `PACK-${slug(state.tag).toUpperCase()}`), f('code'));
    await h.step(`Label: ${label}. Value type: Data (free text).`, async () => {
      await h.type(f('label'), label);
      await page.locator(f('value_type')).selectOption('Data');
    }, f('label'));
    await h.step('Save.', () => h.saveDoc('ProductAttributeDef'), '#dynamic-modal-form button[type="submit"]');
    await showRow(page, h, label, `${label} can now be used by any family.`);
    state.pim_attribute = { label };
    return `Attribute saved: ${label}`;
  },

  pim_family_attribute: async (page, h, state) => {
    if (!state.pim_family || !state.pim_attribute) throw new Error('Record the family and attribute first.');
    await h.open('/#/setup/ProductFamilyAttribute', 'Step three: say which attributes a family needs - this list, Family Attributes, is exactly what completeness checks.');
    await h.step('Choose New.', () => page.locator('#doc-create-button').click(), '#doc-create-button');
    await page.locator('#dynamic-modal.open').waitFor();
    const f = n => `#dyn-field-ProductFamilyAttribute-${n}`;
    if (await page.locator(f('code')).isEditable().catch(() => false)) await h.step('A short code.', () => h.type(f('code'), `HAMPER-PACK-${slug(state.tag).toUpperCase()}`), f('code'));
    await h.step(`Family: ${state.pim_family.name}. Attribute: ${state.pim_attribute.label} - both by name.`, async () => {
      await page.locator(f('family')).selectOption({ label: state.pim_family.name });
      await page.locator(f('attribute')).selectOption({ label: state.pim_attribute.label });
    }, f('family'));
    await h.step('Mandatory for completeness: Yes.', () => page.locator(f('mandatory')).selectOption('Yes'), f('mandatory'));
    await h.step('Save.', () => h.saveDoc('ProductFamilyAttribute'), '#dynamic-modal-form button[type="submit"]');
    await page.waitForTimeout(1000);
    await h.note('Every hamper must now have a pack size before it counts as complete.');
    return `${state.pim_attribute.label} is mandatory for ${state.pim_family.name}`;
  },

  pim_item_family: async (page, h, state) => {
    if (!state.fg_item?.name || !state.pim_family) throw new Error('Needs the finished-good item and family.');
    await h.open('/#/setup/Item', 'Step four: put the product in its family.');
    const row = page.locator('#view-root tbody tr').filter({ hasText: state.fg_item.name }).first();
    await row.waitFor();
    await h.step(`Find ${state.fg_item.name} and choose Edit.`, () => row.getByRole('button', { name: /edit/i }).click());
    await page.locator('#dynamic-modal.open').waitFor();
    await h.step(`Product family: ${state.pim_family.name}.`, () => page.locator('#dyn-field-Item-family').selectOption({ label: state.pim_family.name }), '#dyn-field-Item-family');
    await h.step('Save.', async () => {
      // An edit posts to /api/v1/doc/Item/<id>, not the create path saveDoc waits on.
      const saved = page.waitForResponse(r => r.request().method() === 'POST' && new URL(r.url()).pathname.startsWith('/api/v1/doc/Item/'));
      await page.locator('#dynamic-modal-form button[type="submit"]').click();
      const res = await saved;
      if (!res.ok()) throw new Error(`Item update returned ${res.status()}`);
      await page.waitForFunction(() => !document.querySelector('#dynamic-modal')?.classList.contains('open'));
    }, '#dynamic-modal-form button[type="submit"]');
    await h.note(`${state.fg_item.name} is now scored against ${state.pim_family.name}.`);
    return `${state.fg_item.name} placed in ${state.pim_family.name}`;
  },

  pim_workbench: async (page, h, state) => {
    if (!state.fg_item?.id || !state.pim_attribute) throw new Error('Needs the item in its family.');
    await h.open('/#/view/pim:workbench', 'Step five: the PIM Workbench - fill in the product until it is complete.');
    const row = page.locator(`.pim-workbench-row[data-item="${state.fg_item.id}"]`).first();
    await row.waitFor();
    await row.evaluate(e => { e.scrollIntoView({ block: 'center' }); e.style.outline = '3px solid #f6c85f'; });
    await h.note('The list shows each product\'s completeness and how many required values are missing.');
    await h.step(`Open ${state.fg_item.name}.`, () => row.click());
    await page.locator('#pim-attr-select').waitFor();
    await h.step(`Attribute ${state.pim_attribute.label}: "1 box of 6".`, async () => {
      await page.locator('#pim-attr-select').selectOption({ label: state.pim_attribute.label });
      await h.type('#pim-attr-value', '1 box of 6');
    }, '#pim-attr-select');
    await h.step('Save the value.', () => h.post('/api/v1/doc/ProductAttributeValue', () => page.locator('#pim-attr-save-btn').click()), '#pim-attr-save-btn');
    await page.locator('#pim-content-title').waitFor();
    await h.step('The selling content: title, short and long description.', async () => {
      await h.type('#pim-content-title', state.fg_item.name);
      await h.type('#pim-content-short', 'Six hand-packed treats in a reusable gift box');
      await h.type('#pim-content-long', 'A festive gift box with six assorted treats, packed by hand in a reusable box.');
    }, '#pim-content-title');
    await h.step('Submit for Approval - only approved content counts as complete.', () =>
      h.post('/api/v1/approval/submit', () => page.locator('#pim-content-submit-btn').click()), '#pim-content-submit-btn');
    await page.waitForTimeout(1500);
    await h.note('Once a reviewer approves the content, the product shows as complete on the Dashboard and in Reports.');
    return `${state.fg_item.name} enriched and its content submitted for approval`;
  },

  // --- Quality: inspect a production batch, approve the result -----------
  quality_inspection: async (page, h, state) => {
    if (!state.production_order?.id) throw new Error('Record production_order first.');
    const poId = state.production_order.id;
    await h.open('/#/view/manufacturing:quality', 'Before the boxes are sold, quality inspects the batch and records the result.');
    await openNew(page, h, 'qi-screen', 'New Inspection');
    await page.locator('#qi-type').waitFor();
    await h.step('Inspecting: production output.', () => page.locator('#qi-type').selectOption('Production'), '#qi-type');
    await h.step(`The production order: ${poId}. Its finished item and location come from the order.`, async () => {
      await h.type('#qi-source', poId);
      await page.waitForTimeout(900);
      const opt = page.locator('.typeahead-menu .typeahead-item').filter({ hasText: poId }).first();
      if (await opt.count()) await opt.click(); else await page.locator('#qi-qty').focus();
    }, '#qi-source');
    await h.step('Quantity inspected: 2.', () => h.type('#qi-qty', '2'), '#qi-qty');
    await h.step('One reading: seal, print and weight. OK, Pass.', async () => {
      const r = page.locator('#qi-readings tr').first();
      await r.locator('.rd-param').pressSequentially('Seal, print and weight', { delay: 40 });
      await r.locator('.rd-spec').pressSequentially('Intact, legible, 250 g', { delay: 40 });
      await r.locator('.rd-reading').pressSequentially('OK', { delay: 55 });
      await r.locator('.rd-result').selectOption('Pass');
    }, '#qi-readings');
    await h.step('Remarks: what was checked.', () => h.type('#qi-remarks', 'Sample of 2 checked'), '#qi-remarks');
    const saved = await h.step('Record the inspection. It is saved as Draft with its result.', () =>
      h.post('/api/v1/quality/inspection', () => page.locator('#qi-save').click()), '#qi-save');
    const qiId = saved.id;
    const submitSel = `[data-qi-act="submit"][data-id="${qiId}"]`;
    await page.locator(submitSel).waitFor({ timeout: 15000 });
    await outlineRow(page, h, page.locator('#view-root tbody tr').filter({ has: page.locator(submitSel) }).first(),
      `${qiId}: Pass, Draft. A second person signs off a production inspection.`);
    await h.step('Submit it for approval.', () => h.post('/api/v1/approval/submit', () => page.locator(submitSel).click()), submitSel);
    await page.waitForTimeout(1000);
    await h.note('Pending approval by the store manager.');
    state.qi = { id: qiId };
    return `Quality inspection recorded (Pass) for ${state.production_order.id}`;
  },

  quality_approve: async (page, h, state) => {
    if (!state.production_order?.id) throw new Error('Record quality_inspection first.');
    await h.open('/#/view/approvals', 'The store manager signs off the inspection.');
    const row = page.locator('#inbox-approvals tbody tr').filter({ hasText: 'Quality Inspection' }).first();
    await row.waitFor();
    await h.step('Check the inspection in the queue.', async () => {
      await row.evaluate(e => { e.scrollIntoView({ block: 'center' }); e.style.outline = '3px solid #f6c85f'; });
    });
    await h.step('Approve.', async () => {
      const reply = await h.post('/api/v1/approval/decide', () => row.getByRole('button', { name: 'Approve' }).click());
      if (reply.decision !== 'Approved') throw new Error('QI approval did not complete.');
    });
    await h.note('Approved - the batch is cleared for sale.');
    return 'Quality inspection approved';
  },

  // --- Fixed assets: register, capitalise, transfer ----------------------
  asset_register: async (page, h, state) => {
    if (!state.employee?.name || !state.location?.name) throw new Error('Needs the employee and location fixtures.');
    const assetNo = `AST-${slug(state.tag).toUpperCase()}-01`;
    await h.open('/#/view/assets', 'A new billing counter was bought for the warehouse. Record it as a fixed asset so it is depreciated and someone is responsible for it.');
    await openNew(page, h, 'asset-screen', 'New Asset');
    await page.locator('#asset-category').waitFor();
    if (await page.locator('#asset-code').isEditable()) await h.step(`Asset number: ${assetNo}.`, () => h.type('#asset-code', assetNo), '#asset-code');
    await h.step('Category and cost: Furniture, 60,000.', async () => {
      await h.type('#asset-category', 'Furniture');
      await h.type('#asset-cost', '60000');
    }, '#asset-category');
    await h.step('Useful life: 5 years - depreciation is spread over it.', () => h.type('#asset-useful-life', '5'), '#asset-useful-life');
    await h.step(`Location, by name: ${state.location.name}.`, () => pick(page, h, '#asset-location', 'Audit Dis', state.location.name), '#asset-location');
    await h.step(`Custodian - who looks after it: ${state.employee.name}.`, () => pick(page, h, '#asset-custodian', 'SOP Store', state.employee.name), '#asset-custodian');
    await h.step('Acquisition date.', () => page.locator('#asset-acquisition-date').fill('2026-10-01'), '#asset-acquisition-date');
    const reply = await h.step('Create. It starts as a Draft.', () => h.post('/api/v1/doc/Asset', () => page.locator('#asset-create-btn').click()), '#asset-create-btn');
    const id = reply.id;
    const row = () => page.locator('#view-root tbody tr').filter({ hasText: id }).first();
    await showRow(page, h, id, 'Draft: recorded, not yet on the books.');
    await h.step('Capitalise it.', () => row().getByRole('button', { name: 'Capitalise' }).click());
    // The confirmation shows the entry and the monthly depreciation before anything posts.
    await page.locator('#custom-dialog-container:not(.hidden)').filter({ hasText: 'Fixed Assets' }).waitFor();
    await h.step('The confirmation shows what posts: Fixed Assets against GRN Suspense, and the monthly depreciation. Confirm.', async () => {
      await page.waitForTimeout(2500);
      await h.post('/api/v1/assets/capitalize', () => confirmDialog(page));
    }, '#custom-dialog-container');
    await showRow(page, h, id, 'Capitalised - cost, accumulated depreciation and net book value are shown on the register.');
    state.asset = { id };
    return `Asset ${id} registered and capitalised`;
  },

  asset_transfer: async (page, h, state) => {
    if (!state.asset?.id || !state.store?.name) throw new Error('Record asset_register first.');
    await h.open('/#/view/assets', 'The counter is moving to the shop. Transfer it so the register shows where it is and who has it.');
    const row = () => page.locator('#view-root tbody tr').filter({ hasText: state.asset.id }).first();
    await showRow(page, h, state.asset.id, `The asset is at ${state.location.name}.`);
    await h.step('Choose Transfer.', () => row().getByRole('button', { name: 'Transfer' }).click());
    await page.locator('#asset-action-panel').waitFor();
    await h.step(`New location, by name: ${state.store.name}.`, () => pick(page, h, '#asset-transfer-location', 'Audit Retail', state.store.name), '#asset-transfer-location');
    await h.step(`Custodian stays ${state.employee.name}.`, () => pick(page, h, '#asset-transfer-custodian', 'SOP Store', state.employee.name), '#asset-transfer-custodian');
    await h.step('Confirm.', () => h.post('/api/v1/assets/transfer', () => page.locator('#asset-action-confirm').click()), '#asset-action-confirm');
    await showRow(page, h, state.asset.id, `Now at ${state.store.name}. Disposal works the same way - a panel, a choice, a confirmation.`);
    return `Asset ${state.asset.id} transferred to ${state.store.name}`;
  },

  // --- Expenses: claim, approve, verify, pay ------------------------------
  expense_claim: async (page, h, state) => {
    if (!state.employee?.name) throw new Error('Record employee_setup first.');
    await h.open('/#/view/expenses', 'An employee paid for a client visit taxi from their own pocket. Claim it back.');
    await openNew(page, h, 'exp-screen', 'New Expense Claim');
    await h.step(`Employee, by name: ${state.employee.name}.`, () => pick(page, h, '#exp-employee', 'SOP Store', state.employee.name), '#exp-employee');
    await h.step(`Location: ${state.location.name} - the claim goes to that location's manager for approval.`, () => pick(page, h, '#exp-location', 'Audit Dis', state.location.name), '#exp-location');
    await h.step('Date and category: 7 October, Conveyance.', async () => {
      await page.locator('#exp-date').fill('2026-10-07');
      await page.locator('#exp-category').selectOption('Conveyance');
    }, '#exp-category');
    await h.step('Amount 850; the GST on the bill, 0 for a taxi.', async () => {
      await h.type('#exp-amount', '850');
      await h.type('#exp-gst', '0');
    }, '#exp-amount');
    await h.step('Purpose, in a sentence.', () => h.type('#exp-purpose', 'Taxi to client site and back'), '#exp-purpose');
    const reply = await h.step('Create Draft.', () => h.post('/api/v1/doc/ExpenseClaim', () => page.locator('#exp-create-btn').click()), '#exp-create-btn');
    const row = () => page.locator('#view-root tbody tr').filter({ hasText: reply.id }).first();
    await showRow(page, h, reply.id, 'Draft.');
    await h.step('Submit for Approval. Under 5,000 a store manager decides; above it, an administrator.', () =>
      h.post('/api/v1/approval/submit', () => row().getByRole('button', { name: 'Submit for Approval' }).click()));
    await showRow(page, h, reply.id, 'Pending approval.');
    state.expense = { id: reply.id };
    return `Expense claim ${reply.id} submitted for approval`;
  },

  expense_approve: async (page, h, state) => {
    if (!state.expense?.id) throw new Error('Record expense_claim first.');
    await h.open('/#/view/approvals', 'The store manager reviews the claim in their approval queue.');
    const row = page.locator('#inbox-approvals tbody tr').filter({ has: page.locator(`[data-act-args*="${state.expense.id}"]`) }).first();
    await row.waitFor();
    await h.step('Check who claimed, for what and how much.', async () => {
      await row.evaluate(e => { e.scrollIntoView({ block: 'center' }); e.style.outline = '3px solid #f6c85f'; });
    });
    await h.step('Approve.', async () => {
      const reply = await h.post('/api/v1/approval/decide', () => row.getByRole('button', { name: 'Approve' }).click());
      if (reply.decision !== 'Approved') throw new Error('Expense approval did not complete.');
    });
    await h.note('Approved by the manager - it now goes to finance.');
    return `Expense claim ${state.expense.id} approved`;
  },

  expense_pay: async (page, h, state) => {
    if (!state.expense?.id) throw new Error('Record expense_approve first.');
    await h.open('/#/view/expenses', 'Finance checks the claim against the receipt, then pays it.');
    const row = () => page.locator('#view-root tbody tr').filter({ hasText: state.expense.id }).first();
    await showRow(page, h, state.expense.id, 'Approved - waiting for finance.');
    await h.step('Finance Verify, once the receipt has been checked.', () =>
      h.post('/api/v1/expenses/verify', () => row().getByRole('button', { name: 'Finance Verify' }).click()));
    await showRow(page, h, state.expense.id, 'Verified.');
    await h.step('Mark Paid when the money is sent. The expense and any GST credit are posted to the accounts.', async () => {
      await row().getByRole('button', { name: 'Mark Paid' }).click();
      await page.locator('#custom-dialog-container:not(.hidden)').waitFor();
      await h.post('/api/v1/expenses/pay', () => confirmDialog(page));
    });
    await showRow(page, h, state.expense.id, 'Paid.');
    return `Expense claim ${state.expense.id} verified and paid`;
  },

  // --- Procurement: sending goods back to the supplier --------------------
  purchase_return: async (page, h, state) => {
    if (!state.grn?.id) throw new Error('Record grn_post first.');
    const reason = `Not as ordered ${state.tag}`;
    await h.open('/#/view/purchase-returns', 'Some received goods are not as ordered. Send them back against the receipt they arrived on; the supplier owes us for them.');
    await openNew(page, h, 'prt-screen', 'New Purchase Return');
    await h.step(`Choose the goods receipt: ${state.grn.code || state.grn.id}.`, async () => {
      const sel = page.locator('#prt-grn');
      const values = await sel.locator('option').evaluateAll(os => os.map(o => o.value));
      const v = values.find(x => x === state.grn.id) || values.find(x => state.grn.code && x.startsWith(state.grn.code));
      if (!v) throw new Error(`GRN ${state.grn.id} is not in the return list.`);
      await sel.selectOption(v);
    }, '#prt-grn');
    await page.locator('#prt-lines .prt-qty').first().waitFor();
    await h.note('The lines show what was received and what can still be returned - you can never return more than arrived.');
    await h.step('Return 5 of the item.', () => h.type('#prt-lines .prt-qty', '5'), '#prt-lines');
    await h.step('Reason: Quality not as ordered, with a short note.', async () => {
      await page.locator('#prt-reason-code').selectOption('PR-QUALITY');
      await h.type('#prt-reason', reason);
    }, '#prt-reason-code');
    await h.step('Save the return. Nothing moves until it is posted.', () => h.post('/api/v1/doc/PurchaseReturn', () => page.locator('#prt-save-btn').click()), '#prt-save-btn');
    const row = () => page.locator('#view-root tbody tr').filter({ hasText: reason }).first();
    await showRow(page, h, reason, 'Saved as Draft.');
    await h.step('Post it. The 5 units leave stock, and a debit note for their value is raised against the supplier automatically.', async () => {
      await row().getByRole('button', { name: 'Post' }).click();
      const dlg = page.locator('#custom-dialog-container:not(.hidden)');
      if (await dlg.count() || await dlg.waitFor({ timeout: 2500 }).then(() => true).catch(() => false)) await confirmDialog(page);
    });
    await showRow(page, h, reason, 'Posted - with the debit note it raised shown on the row.');
    await h.open('/#/view/finance-notes', 'The debit note is in Debit / Credit Notes, already posted - accounts do not re-enter it.');
    await page.waitForTimeout(1000);
    await h.note('Stock, the supplier balance and the GST all move together from one action.');
    return `Purchase return of 5 posted against ${state.grn.id}, debit note raised`;
  },

  // --- Masters: a customer (same shape as the vendor/item setup segments) ---
  customer_setup: async (page, h, state) => {
    const name = `SOP Account Customer ${state.tag}`;
    await h.open('/#/setup/Customer', 'Open Customer under Setup. Confirm the customer exists before billing or refunding them.');
    await h.step('Choose New Customer.', () => page.locator('#doc-create-button').click(), '#doc-create-button');
    await page.locator('#dynamic-modal.open').waitFor();
    await h.step(`Enter the customer's name: ${name}.`, () => h.type('#dyn-field-Customer-name', name), '#dyn-field-Customer-name');
    await h.note('The code is generated by the system and Status starts as Active - the name is what people search for.');
    const reply = await h.step('Save the customer, then check it appears in the list.', () => h.saveDoc('Customer'), '#dynamic-modal-form button[type="submit"]');
    await showRow(page, h, name, `Saved ${name}.`);
    state.customer = { name, id: reply.id };
    return `Customer saved and read back: ${name}`;
  },

  // --- Finance: debit and credit notes -------------------------------------
  debit_note: async (page, h, state) => {
    if (!state.vendor?.name || !state.po?.id) throw new Error('Needs the vendor and PO fixtures.');
    let noteNo;
    await h.open('/#/view/finance-notes', 'The supplier agreed to a 100 rebate for late delivery. Record it as a debit note - money they owe us back.');
    await h.step('Choose + New Debit Note.', async () => {
      await page.locator('#dn-new-btn').click();
      await page.locator('#doc-create-button').click();
    }, '#dn-new-btn');
    await page.locator('#dynamic-modal.open').waitFor();
    const f = n => `#dyn-field-DebitNote-${n}`;
    await h.step(`Vendor: ${state.vendor.name}.`, () => page.locator(f('vendor_id')).selectOption({ label: state.vendor.name }), f('vendor_id'));
    await h.step(`The purchase order it relates to: ${state.po.id}.`, () => page.locator(f('reference_po')).selectOption(state.po.id), f('reference_po'));
    await h.step('Amount: 100.', () => h.type(f('amount'), '100'), f('amount'));
    await h.step('Reason, in plain words.', () => h.type(f('reason'), 'Rebate agreed for late delivery'), f('reason'));
    await h.step('Status starts as Draft.', () => page.locator(f('status')).selectOption('Draft'), f('status'));
    // The note number comes from the prefix setup when it is saved.
    await h.step('Save. The system gives the note its number.', async () => {
      const saved = await h.saveDoc('DebitNote');
      noteNo = saved.note_number || saved.id;
    }, '#dynamic-modal-form button[type="submit"]');
    await h.open('/#/view/finance-notes', 'Back on Debit / Credit Notes, the note is a Draft until it is posted.');
    const row = () => page.locator('#view-root tbody tr').filter({ hasText: noteNo }).first();
    await showRow(page, h, noteNo, 'Draft: nothing has reached the accounts yet.');
    await h.step('Post it. This reduces what we owe the supplier.', async () => {
      await row().getByRole('button', { name: 'Post' }).click();
      await page.locator('#custom-dialog-container:not(.hidden)').waitFor();
      await confirmDialog(page);
    });
    await showRow(page, h, noteNo, 'Posted. Once posted a note cannot be edited - a mistake is corrected with a new note.');
    return `Debit note ${noteNo} posted against ${state.vendor.name}`;
  },

  credit_note: async (page, h, state) => {
    if (!state.customer?.name) throw new Error('Needs the customer fixture.');
    let noteNo;
    await h.open('/#/view/finance-notes:customer-credit', 'A customer was overcharged by 50. Refund it with a credit note, under Customer Credit Notes.');
    await h.step('Choose + New Credit Note.', async () => {
      await page.locator('#cn-new-btn').click();
      await page.locator('#doc-create-button').click();
    }, '#cn-new-btn');
    await page.locator('#dynamic-modal.open').waitFor();
    const f = n => `#dyn-field-CreditNote-${n}`;
    await h.step(`Customer, by name: ${state.customer.name}.`, () => page.locator(f('customer_id')).selectOption({ label: state.customer.name }), f('customer_id'));
    await h.step('Amount: 50.', () => h.type(f('amount'), '50'), f('amount'));
    await h.step('Reason.', () => h.type(f('reason'), 'Overcharged on delivery fee'), f('reason'));
    await h.step('Status starts as Draft.', () => page.locator(f('status')).selectOption('Draft'), f('status'));
    // The note number comes from the prefix setup when it is saved.
    await h.step('Save. The system gives the note its number.', async () => {
      const saved = await h.saveDoc('CreditNote');
      noteNo = saved.note_number || saved.id;
    }, '#dynamic-modal-form button[type="submit"]');
    await h.open('/#/view/finance-notes:customer-credit', 'The credit note waits as a Draft.');
    const row = () => page.locator('#view-root tbody tr').filter({ hasText: noteNo }).first();
    await showRow(page, h, noteNo, 'Draft.');
    await h.step('Post it. The refund is booked against Sales Returns & Allowances and paid out.', async () => {
      await row().getByRole('button', { name: 'Post' }).click();
      await page.locator('#custom-dialog-container:not(.hidden)').waitFor();
      await confirmDialog(page);
    });
    await showRow(page, h, noteNo, 'Posted.');
    return `Credit note ${noteNo} posted for ${state.customer.name}`;
  },

  // --- Service: maintenance contract, then a ticket worked to closure -------
  service_contract_setup: async (page, h, state) => {
    if (!state.customer?.name || !state.asset?.id) throw new Error('Needs the customer and asset fixtures.');
    await h.open('/#/setup/ServiceContract', 'The customer bought an annual maintenance contract for their billing counter: four service visits in the year. Record it so each visit is counted.');
    await h.step('Choose New Service Contract.', () => page.locator('#doc-create-button').click(), '#doc-create-button');
    await page.locator('#dynamic-modal.open').waitFor();
    const f = n => `#dyn-field-ServiceContract-${n}`;
    await h.note('The contract number is generated on save.');
    await h.step(`Customer, by name: ${state.customer.name}.`, () => h.type(f('customer'), state.customer.name), f('customer'));
    await h.step(`The asset it covers: ${state.asset.id}.`, () => page.locator(f('asset')).selectOption(state.asset.id), f('asset'));
    await h.step('Cover from 1 October 2026 to 30 September 2027.', async () => {
      await page.locator(f('start_date')).fill('2026-10-01');
      await page.locator(f('end_date')).fill('2027-09-30');
    }, f('start_date'));
    await h.step('Visits included: 4. Visits Used is counted by the system as tickets close.', () => h.type(f('visits_included'), '4'), f('visits_included'));
    await h.step('Status: Active.', () => page.locator(f('status')).selectOption('Active'), f('status'));
    const reply = await h.step('Save.', () => h.saveDoc('ServiceContract'), '#dynamic-modal-form button[type="submit"]');
    await showRow(page, h, reply.id, 'The contract is active: 4 visits, none used.');
    state.service_contract = { id: reply.id };
    return `Service contract ${reply.id} active for ${state.customer.name}`;
  },

  service_ticket_raise: async (page, h, state) => {
    if (!state.service_contract?.id || !state.user) throw new Error('Record service_contract_setup first.');
    await h.open('/#/setup/ServiceTicket', 'The customer calls: the counter drawer is jammed. Log a service ticket, then hand it to a technician.');
    await h.step('Choose New Service Ticket.', () => page.locator('#doc-create-button').click(), '#doc-create-button');
    await page.locator('#dynamic-modal.open').waitFor();
    const f = n => `#dyn-field-ServiceTicket-${n}`;
    await h.step(`Customer: ${state.customer.name}.`, () => h.type(f('customer'), state.customer.name), f('customer'));
    await h.step(`Asset: ${state.asset.id}.`, () => page.locator(f('asset')).selectOption(state.asset.id), f('asset'));
    await h.step('What is wrong, in the customer\'s words.', () => h.type(f('description'), 'Cash drawer jammed, will not open'), f('description'));
    await h.step('Priority: High - the shop cannot take cash.', () => page.locator(f('priority')).selectOption('High'), f('priority'));
    await h.step('Respond by tomorrow, resolve within four days. Missed dates show on the SLA Breaches report.', async () => {
      await page.locator(f('respond_by_date')).fill('2026-10-11');
      await page.locator(f('resolve_by_date')).fill('2026-10-14');
    }, f('respond_by_date'));
    await h.step(`Service contract: ${state.service_contract.id} - this visit is counted against it.`, () => page.locator(f('service_contract_id')).selectOption(state.service_contract.id), f('service_contract_id'));
    await h.step('Status starts as Draft. Leave technician and notes empty - the buttons on the row fill them in.', () => page.locator(f('status')).selectOption('Draft'), f('status'));
    // The ticket number comes from the prefix setup when it is saved.
    const reply = await h.step('Save. The system gives the ticket its number.', () => h.saveDoc('ServiceTicket'), '#dynamic-modal-form button[type="submit"]');
    // The save reply carries the internal id; the ticket number is read back from the record.
    const saved = await page.evaluate(async id => {
      const r = await fetch(`/api/v1/doc/ServiceTicket/${encodeURIComponent(id)}`, { headers: { Authorization: `Bearer ${localStorage.getItem('erp_token')}` } });
      return r.ok ? r.json() : {};
    }, reply.id);
    const ticketNo = saved.code;
    if (!ticketNo) throw new Error(`Service ticket ${reply.id} has no number.`);
    const row = () => page.locator('#view-root tbody tr').filter({ hasText: ticketNo }).first();
    await showRow(page, h, ticketNo, 'Draft - logged, nobody on it yet.');
    await h.step(`Assign it to the technician by username: ${state.user}.`, async () => {
      await row().locator('[data-step="assign"]').click();
      await page.locator('#custom-dialog-prompt-input').waitFor();
      await h.type('#custom-dialog-prompt-input', state.user);
      await h.post(`/api/v1/service/ticket/${encodeURIComponent(reply.id)}/assign`, () => page.locator('#custom-dialog-ok-btn').click());
    });
    await showRow(page, h, ticketNo, `Assigned to ${state.user}.`);
    state.service_ticket = { id: reply.id, number: ticketNo };
    return `Service ticket ${ticketNo} logged and assigned to ${state.user}`;
  },

  service_ticket_resolve: async (page, h, state) => {
    if (!state.service_ticket?.id) throw new Error('Record service_ticket_raise first.');
    const { id, number } = state.service_ticket;
    await h.open('/#/setup/ServiceTicket', 'The technician reaches the shop. Each step is one button on the ticket\'s row.');
    const row = () => page.locator('#view-root tbody tr').filter({ hasText: number }).first();
    await showRow(page, h, number, 'Assigned.');
    await h.step('Start - work has begun.', () =>
      h.post(`/api/v1/service/ticket/${encodeURIComponent(id)}/start`, () => row().locator('[data-step="start"]').click()));
    await showRow(page, h, number, 'In progress.');
    await h.step('Resolve, saying what was done. A ticket cannot be resolved without a note.', async () => {
      await row().locator('[data-step="resolve"]').click();
      await page.locator('#custom-dialog-prompt-input').waitFor();
      await h.type('#custom-dialog-prompt-input', 'Replaced the drawer latch spring; tested 20 open-close cycles');
      await h.post(`/api/v1/service/ticket/${encodeURIComponent(id)}/resolve`, () => page.locator('#custom-dialog-ok-btn').click());
    });
    await showRow(page, h, number, 'Resolved - the fix is recorded on the ticket.');
    await h.step('Close it once the customer confirms. One visit is used from the contract.', async () => {
      await row().locator('[data-step="close"]').click();
      await page.locator('#custom-dialog-container:not(.hidden)').waitFor();
      await h.post(`/api/v1/service/ticket/${encodeURIComponent(id)}/close`, () => confirmDialog(page));
    });
    await showRow(page, h, number, 'Closed.');
    await h.open('/#/setup/ServiceContract', 'On the contract, Visits Used has gone from 0 to 1.');
    await showRow(page, h, state.service_contract.id, '1 of 4 visits used.');
    return `Service ticket ${number} resolved and closed; 1 contract visit used`;
  },

  // --- CRM: a birthday campaign and what it brought in ----------------------
  crm_customer_birthday: async (page, h, state) => {
    if (!state.customer?.name) throw new Error('Needs the customer fixture.');
    await h.open('/#/setup/Customer', 'A birthday campaign can only greet customers whose date of birth is on file. Add it to the customer.');
    const row = page.locator('#view-root tbody tr').filter({ hasText: state.customer.name }).first();
    await row.waitFor();
    await h.step(`Find ${state.customer.name} and choose Edit.`, () => row.getByRole('button', { name: /edit/i }).click());
    await page.locator('#dynamic-modal.open').waitFor();
    await h.step('Date of Birth: 10 October.', () => page.locator('#dyn-field-Customer-date_of_birth').fill('1990-10-10'), '#dyn-field-Customer-date_of_birth');
    await h.step('Save.', async () => {
      // An edit posts to /api/v1/doc/Customer/<id>, not the create path saveDoc waits on.
      const saved = page.waitForResponse(r => r.request().method() === 'POST' && new URL(r.url()).pathname.startsWith('/api/v1/doc/Customer/'));
      await page.locator('#dynamic-modal-form button[type="submit"]').click();
      const res = await saved;
      if (!res.ok()) throw new Error(`Customer update returned ${res.status()}`);
      await page.waitForFunction(() => !document.querySelector('#dynamic-modal')?.classList.contains('open'));
    }, '#dynamic-modal-form button[type="submit"]');
    await showRow(page, h, state.customer.name, 'Saved. Only the day and month are used - the greeting goes out every year.');
    return `${state.customer.name}'s date of birth recorded`;
  },

  campaign_setup: async (page, h, state) => {
    const name = `Birthday Treat ${state.tag}`;
    await h.open('/#/setup/Campaign', 'Set up a campaign once; the system sends it by itself. A birthday campaign greets each customer on their day.');
    await h.step('Choose New Campaign.', () => page.locator('#doc-create-button').click(), '#doc-create-button');
    await page.locator('#dynamic-modal.open').waitFor();
    const f = n => `#dyn-field-Campaign-${n}`;
    await h.step(`Campaign name: ${name}.`, () => h.type(f('name'), name), f('name'));
    await h.step('Trigger: Birthday. The other trigger, Lapsed Customer, reaches anyone who has not bought for a number of days you set.', () => page.locator(f('trigger_type')).selectOption('Birthday'), f('trigger_type'));
    await h.step('Channel: WhatsApp. SMS and Email work the same way.', () => page.locator(f('channel')).selectOption('WhatsApp'), f('channel'));
    await h.step('The message. {{customer_name}} is replaced with each customer\'s name.', () =>
      h.type(f('message_template'), 'Happy birthday {{customer_name}}! Enjoy 10% off in store this week.'), f('message_template'));
    await h.step('What the campaign costs, so the report can show its return: 500.', () => h.type(f('cost'), '500'), f('cost'));
    await h.step('Status: Active. Make it Inactive to pause it.', () => page.locator(f('status')).selectOption('Active'), f('status'));
    await h.step('Save.', () => h.saveDoc('Campaign'), '#dynamic-modal-form button[type="submit"]');
    await showRow(page, h, name, 'Active. Every hour the system finds customers whose birthday is today and sends the message once.');
    state.campaign = { name };
    return `Campaign ${name} active`;
  },

  campaign_roi: async (page, h, state) => {
    if (!state.campaign?.name) throw new Error('Record campaign_setup first.');
    await h.open('/#/view/reports:report-catalog', 'A day later, see who the campaign reached and what they spent. Open Reports, Report Catalog.');
    await page.locator('#rc-report-select').waitFor();
    await h.step('Report: Campaign ROI, under CRM.', () => page.locator('#rc-report-select').selectOption('campaign-roi'), '#rc-report-select');
    await h.step('Run.', () => page.locator('#rc-run-btn').click(), '#rc-run-btn');
    await showRow(page, h, state.campaign.name, 'Customers reached, the sales they made since, the cost, and the return on it.');
    return `Campaign ROI read for ${state.campaign.name}`;
  },

  // --- Reports: the GST position, and any report as a spreadsheet -----------
  report_gst_summary: async (page, h, state) => {
    await h.open('/#/view/reports:gst-return-summary', 'Before filing GST, check the month\'s position. Open Reports, GST Return Summary, under Finance.');
    await page.locator('#gst-start-date').waitFor();
    await h.step('The period: this month to date. Change the dates for any other period.', async () => {
      await page.locator('#gst-start-date').fill('2026-10-01');
      await page.locator('#gst-end-date').fill('2026-10-31');
    }, '#gst-start-date');
    await h.step('Run.', () => page.locator('#gst-summary-btn').click(), '#gst-summary-btn');
    const result = page.locator('#gst-summary-result');
    await result.locator('table, .stat-card, div').first().waitFor();
    await page.waitForTimeout(800);
    await result.evaluate(e => { e.scrollIntoView({ block: 'center' }); e.style.outline = '3px solid #f6c85f'; });
    await h.note('GST collected on sales, GST paid on purchases that can be claimed back, and the net to pay.');
    return 'GST position for October read';
  },

  report_catalog_export: async (page, h, state) => {
    if (!state.item?.id) throw new Error('Needs the item fixture.');
    await h.open('/#/view/reports:report-catalog', 'Every other report is in the Report Catalog, and any of them can be taken out as a spreadsheet.');
    await page.locator('#rc-report-select').waitFor();
    await h.step('Report: Stock Ledger - every stock movement, in and out.', () => page.locator('#rc-report-select').selectOption('stock-ledger'), '#rc-report-select');
    await h.step('Run.', () => page.locator('#rc-run-btn').click(), '#rc-run-btn');
    await showRow(page, h, state.item.id, 'Each row is one movement - receipts, transfers, sales - with the balance after it.');
    await h.step('Export in Background. A large report keeps running while you work on.', () => page.locator('#rc-export-btn').click(), '#rc-export-btn');
    await page.locator('#rc-download-btn').waitFor({ timeout: 30000 });
    await h.step('When it is ready, Download CSV. It opens in any spreadsheet.', async () => {
      const download = page.waitForEvent('download', { timeout: 15000 });
      await page.locator('#rc-download-btn').click();
      await download;
    }, '#rc-download-btn');
    return 'Stock Ledger run and exported to CSV';
  },

  // --- Stickers: a label printer, then labels for what arrived ---------------
  printer_setup: async (page, h, state) => {
    const name = `Receiving Label Printer ${state.tag}`;
    await h.open('/#/setup/Printer', 'Before the first label run, record the label printer once. Open Printer under Setup.');
    await h.step('Choose New Printer.', () => page.locator('#doc-create-button').click(), '#doc-create-button');
    await page.locator('#dynamic-modal.open').waitFor();
    const f = n => `#dyn-field-Printer-${n}`;
    await h.step(`A name people will recognise: ${name}.`, () => h.type(f('name'), name), f('name'));
    await h.step(`Where it sits, in words: the receiving bay at ${state.location.name}.`, () => h.type(f('location'), `Receiving bay, ${state.location.name}`), f('location'));
    await h.step('Status Active; used for Stickers.', async () => {
      await page.locator(f('status')).selectOption('Active');
      await page.locator(f('print_role')).selectOption('Sticker');
    }, f('print_role'));
    await h.step('A thermal label printer: language TSPL, 203 DPI, 50 by 25 mm labels.', async () => {
      await page.locator(f('printer_language')).selectOption('TSPL');
      await h.type(f('dpi'), '203');
      await h.type(f('label_width_mm'), '50');
      await h.type(f('label_height_mm'), '25');
    }, f('printer_language'));
    await h.note('The OS Printer Name is only needed for silent printing straight to the printer - until then labels open in the normal print dialog.');
    const reply = await h.step('Save.', () => h.saveDoc('Printer'), '#dynamic-modal-form button[type="submit"]');
    await showRow(page, h, name, 'Saved. It now appears in the Printer list on Sticker Printing.');
    state.printer = { name, id: reply.id };
    return `Printer ${name} recorded`;
  },

  sticker_print_grn: async (page, h, state) => {
    if (!state.grn?.id || !state.printer?.name) throw new Error('Needs the GRN and printer fixtures.');
    await h.open('/#/view/stickers', 'Goods have been received. Print their barcode labels from the receipt itself - nothing is typed by hand.');
    await openNew(page, h, 'sticker-print', 'Print Stickers');
    await h.step('Module: Goods Receipt (GRN).', () => page.locator('#sticker-source-doctype').selectOption('GRN'), '#sticker-source-doctype');
    await h.step(`Printer: ${state.printer.name}. This computer remembers it.`, () => page.locator('#sticker-printer').selectOption({ label: state.printer.name }), '#sticker-printer');
    const grnRef = state.grn.code || state.grn.id;
    await h.step(`Document: start typing the receipt number and pick ${grnRef}. Its labels open in a preview at once.`, () =>
      // The receipt itself, not one of its children (<number>/<key>).
      pick(page, h, '#sticker-source-doc', grnRef, new RegExp(grnRef.replace(/[.*+?^${}()|[\]\\/]/g, '\\$&') + '(?!/)')), '#sticker-source-doc');
    const print = page.locator('[data-stk-preview="print"]');
    await print.waitFor({ timeout: 15000 });
    await h.note('One label per accepted unit - rejected or damaged units get none. The preview shows exactly what prints.');
    await h.step('Print. The run is logged against the receipt, so a re-print later asks for a reason.', () =>
      h.post('/api/v1/stickers/print', () => print.click()), '[data-stk-preview="print"]');
    await page.waitForTimeout(1500);
    const lines = page.locator('#sticker-source-lines');
    await lines.evaluate(e => { e.scrollIntoView({ block: 'center' }); e.style.outline = '3px solid #f6c85f'; });
    await h.note('The labels go to the printer - straight to it once silent printing is set up, otherwise through the print dialog. The lines stay here to re-print a few.');
    return `Labels printed for ${state.grn.code || state.grn.id} on ${state.printer.name}`;
  },
};
