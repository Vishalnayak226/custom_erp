'use strict';

// Helpers shared by scenes.cjs and scenes-modules.cjs.

// Ends a step on the saved record itself, centred and outlined in its list,
// so the viewer sees the readback in the app, not only on the end card.
async function showRow(page, h, text, message) {
  const row = page.locator('#view-root tbody tr').filter({ hasText: text }).first();
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

// Confirms the app's own dialog (showCustomConfirm).
async function confirmDialog(page) {
  await page.locator('#custom-dialog-container:not(.hidden) #custom-dialog-ok-btn').click();
}

// A tag safe for usernames and codes ("Oct 2026 A" -> "oct2026a").
function slug(tag) {
  return String(tag || '').toLowerCase().replace(/[^a-z0-9]/g, '');
}

module.exports = { showRow, pick, confirmDialog, slug };
