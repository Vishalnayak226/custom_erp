// 310.2: a store-default printer this computer does not have is not used -
// chosenPrinter says "none chosen" and installedHere says no, so the print
// chooses this computer's own. minn's store default named the owner's
// "LD0801 Label Printer"; the store PC has "DP27 Label Printer (Copy 1)".
// Run: node --test public/print-routing.test.mjs
import test from 'node:test';
import assert from 'node:assert/strict';

const storeDefault = { code: 'PRI00001', name: 'Jewellery', os_printer_name: 'LD0801 Label Printer' };
const tray = { connected: true, printers: ['Microsoft Print to PDF', 'DP27 Label Printer (Copy 1)'], fails: false };

globalThis.localStorage = { getItem: () => 'DESKTOP-TEST', setItem: () => {} };
globalThis.window = {
  erpWorkingStore: { code: 'LOC-0001' },
  apiFetch: async () => ({ ok: true, json: async () => ({ printer: storeDefault }) }),
  PrintAgent: {
    isConnected: () => tray.connected,
    listOSPrinters: async () => { if (tray.fails) throw new Error('old tray'); return tray.printers; },
  },
};

const { chosenPrinter, installedHere } = await import('./print-routing.js');

const reset = () => Object.assign(tray, { connected: true, printers: ['Microsoft Print to PDF', 'DP27 Label Printer (Copy 1)'], fails: false });

test('a store default this computer does not have is not chosen', async () => {
  reset();
  assert.equal(await installedHere(storeDefault), false);
  assert.equal(await chosenPrinter('Sticker'), null);
});

test('a printer this computer has is chosen, whatever the case of its name', async () => {
  reset();
  tray.printers = ['microsoft print to pdf', 'ld0801 label printer'];
  assert.equal(await installedHere(storeDefault), true);
  assert.deepEqual(await chosenPrinter('Sticker'), storeDefault);
});

test('no tray, no list or a failing list: the printer stands and the print says what is wrong', async () => {
  reset(); tray.connected = false;
  assert.equal(await installedHere(storeDefault), true);
  reset(); tray.printers = [];
  assert.equal(await installedHere(storeDefault), true);
  reset(); tray.fails = true;
  assert.equal(await installedHere(storeDefault), true);
});

test('a record with no Windows printer name is left to the Printer Not Mapped message', async () => {
  reset();
  assert.equal(await installedHere({ code: 'PRI00009', os_printer_name: '' }), true);
  assert.equal(await installedHere(null), true);
});
