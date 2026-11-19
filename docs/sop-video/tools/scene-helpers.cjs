'use strict';

// Helpers shared by scenes.cjs and scenes-modules.cjs.

// Ends a step on the saved record itself, centred and outlined in its list,
// so the viewer sees the readback in the app, not only on the end card.
async function showRow(page, h, text, message) {
  return outlineRow(page, h, page.locator('#view-root tbody tr').filter({ hasText: text }).first(), message);
}

// The same, for lists that show a document number instead of the record id:
// the row is the one whose buttons carry the id.
async function showRowById(page, h, id, message) {
  return outlineRow(page, h, page.locator('#view-root tbody tr').filter({ has: page.locator(`[data-act-args*="${id}"]`) }).first(), message);
}

async function outlineRow(page, h, row, message) {
  await row.waitFor({ timeout: 12000 });
  // A list at the bottom of the page cannot scroll its last row above the
  // caption bar, so give the page some room below it first.
  await page.evaluate(() => { const root = document.getElementById('view-root'); if (root) root.style.paddingBottom = '240px'; });
  await row.evaluate(e => { e.scrollIntoView({ block: 'center' }); e.style.outline = '3px solid #f6c85f'; e.style.outlineOffset = '-3px'; });
  await page.waitForTimeout(500);
  await h.note(message);
  return row;
}

// Types into a picker at a human pace, waits for the suggestions to settle,
// then picks the one matching `option`.
async function pick(page, h, selector, typed, option) {
  await h.type(selector, typed);
  await page.waitForTimeout(900); // human reading time; lets the list settle
  await page.locator('.typeahead-menu .typeahead-item').filter({ hasText: option }).first().click();
  await page.waitForTimeout(300);
}

// Item pickers search an item's code and barcode, never its name (147.2), so
// a person scans or types the barcode; the list then shows the item by name.
async function pickItem(page, h, selector, item) {
  await pick(page, h, selector, item.barcode, item.name);
}

// New Item, on the open Item list: pick its Item Name first (that brings its
// category and product type), then the SKU details; ends on the saved row.
async function newItem(page, h, { itemName, name, barcode, cost }) {
  const f = n => `#dyn-field-Item-${n}`;
  await h.step('Choose New Item.', () => page.locator('#doc-create-button').click(), '#doc-create-button');
  await page.locator('#dynamic-modal.open').waitFor();
  await h.step(`Item name, picked from the list: ${itemName}.`, () => pick(page, h, f('item_name'), itemName, itemName), f('item_name'));
  await h.step(`Name: ${name}.`, () => h.type(f('name'), name), f('name'));
  await h.step(`Barcode: ${barcode}.`, () => h.type(f('barcode'), barcode), f('barcode'));
  await h.step('HSN 9999 and GST 18 percent.', async () => {
    await h.type(f('hsn_code'), '9999');
    await h.type(f('gst_rate'), '18');
  }, f('hsn_code'));
  await h.step(`Standard cost: ${cost}.`, () => h.type(f('standard_cost'), String(cost)), f('standard_cost'));
  const reply = await h.step('Save the item.', () => h.saveDoc('Item'), '#dynamic-modal-form button[type="submit"]');
  await showRow(page, h, name, `${name} is saved. The system gave it its code; people search by name.`);
  return { name, id: reply.id, barcode };
}

// Opens a list-first screen's New page: its "+ New ..." button (txnNewButton).
async function openNew(page, h, screen, label, pane = 'new') {
  const sel = `[data-act-args*="${screen}:${pane}"]`;
  await h.step(`Choose + ${label}.`, () => page.locator(sel).first().click(), sel);
}

// Confirms the app's own dialog (showCustomConfirm).
async function confirmDialog(page) {
  await page.locator('#custom-dialog-container:not(.hidden) #custom-dialog-ok-btn').click();
}

// A tag safe for usernames and codes ("Oct 2026 A" -> "oct2026a").
function slug(tag) {
  return String(tag || '').toLowerCase().replace(/[^a-z0-9]/g, '');
}

module.exports = { showRow, showRowById, outlineRow, openNew, pick, pickItem, newItem, confirmDialog, slug };
