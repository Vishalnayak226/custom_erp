// Screenshot capture for the user-facing guides (Stage 30.4.7).
//
// All four guides had zero images, which was the largest single gap against
// help.sap.com. The decision taken with the user was scripted captures rather
// than hand-taken ones, for the obvious reason: screenshots go stale faster
// than anything else in a manual, and a hand-taken set has no way to be
// refreshed except by someone remembering to do it. This script re-takes the
// whole set in about a minute, so refreshing them is a chore nobody has to
// think about.
//
// USAGE
//   1. Have a server running with data in it (a dev instance is fine):
//        PORT=8152 go run ./cmd/server
//   2. Mint a token for a full-access user. Any method works; the simplest is
//      to log in through the app and copy `erp_token` out of localStorage.
//   3. node docs/guides/capture-screenshots.js --token "<token>" --base http://localhost:8152
//
// Options
//   --token   REQUIRED. A session token for a user who can see every screen.
//   --base    Server URL. Default http://localhost:8152
//   --out     Output directory. Default docs/guides/img
//   --only    Comma-separated shot ids, to re-take just a few.
//   --theme   light | dark. Default light.
//
// Playwright is not a project dependency and never should be (this repo has
// no build step and no frontend framework - see CLAUDE.md's first principle).
// The script resolves it from wherever it already is on the machine, and says
// so clearly if it isn't installed. Nothing in the app or the build depends on
// this file; it is a documentation tool that happens to live in the repo.
//
// WINDOWS NOTE: writing under Documents\ can be blocked by Controlled Folder
// Access, which reports itself as "cannot find the file specified". If that
// happens, pass --out to somewhere under %TEMP% and copy the files in with
// PowerShell, exactly as docs/brain/update-brain.ps1 does.

const fs = require('fs');
const path = require('path');

function resolvePlaywright() {
  const candidates = [
    'playwright',
    path.join(process.env.USERPROFILE || process.env.HOME || '', 'node_modules', 'playwright'),
    path.join(process.env.APPDATA || '', 'npm', 'node_modules', 'playwright'),
  ];
  for (const c of candidates) {
    try { return require(c); } catch (e) { /* try the next one */ }
  }
  console.error('Playwright not found.\n' +
    'Install it anywhere on this machine (it is deliberately not a project dependency):\n' +
    '  npm install -g playwright && npx playwright install chromium\n' +
    'then re-run this script.');
  process.exit(2);
}

const args = {};
process.argv.slice(2).forEach((a, i, all) => {
  if (a.startsWith('--')) args[a.slice(2)] = (all[i + 1] && !all[i + 1].startsWith('--')) ? all[i + 1] : true;
});

const BASE = args.base || 'http://localhost:8152';
const OUT = args.out || path.join(__dirname, 'img');
const THEME = args.theme === 'dark' ? 'dark' : 'light';
const ONLY = args.only ? String(args.only).split(',').map(s => s.trim()) : null;

if (!args.token) {
  console.error('--token is required. See the header of this file for how to get one.');
  process.exit(2);
}

// The shot list. `id` becomes the filename and is what --only matches.
// `nav` is run in the page to get to the screen; every screen in this app is
// reachable by calling its own view function, which is far more robust than
// hovering flyouts and clicking through a menu that may be permission-trimmed.
//
// ADD A SHOT: append an entry here. Keep the id stable once a guide links to
// it, because the filename is the link.
const SHOTS = [
  { id: 'sidebar', caption: 'The twelve top-level sidebar entries',
    nav: () => window.renderView('dashboard'), clip: 'sidebar' },

  { id: 'setup-menu', caption: 'The Setup flyout: grouped by module, with a filter box and an Advanced divider',
    nav: async () => { window.renderView('dashboard'); },
    after: async (page) => {
      await page.hover('#menu-master-definition');
      await page.waitForTimeout(800);
    }, clip: 'sidebar-wide' },

  { id: 'pos-billing', caption: 'POS / Billing with the session bar at the top',
    nav: () => window.renderView('pos') },

  { id: 'purchase-order', caption: 'Purchase Order: the number field is greyed out and auto-issued',
    nav: () => window.renderView('purchase-orders') },

  { id: 'goods-receipt', caption: 'Goods Receipt: load lines from a PO, record accepted/short/damaged',
    nav: () => window.renderView('grn') },

  { id: 'inventory', caption: 'Inventory: on hand versus actually free to sell',
    nav: () => window.renderView('inventory') },

  { id: 'trial-balance', caption: 'Finance / GL: the Trial Balance and its As Of Date',
    nav: () => window.renderView('finance') },

  { id: 'approvals', caption: 'Approvals: documents waiting on you',
    nav: () => window.renderView('approvals') },

  { id: 'reports', caption: 'The report catalog',
    nav: () => window.renderView('reports') },

  { id: 'record-list', caption: 'A record list, with New / Bulk Import and per-row Edit and Delete',
    nav: () => window.openSetupDoctype('Vendor') },

  { id: 'json-line-editor', caption: 'A line editor replacing what used to be a hand-typed JSON field',
    nav: () => window.openSetupDoctype('BOM'),
    after: async (page) => {
      await page.evaluate(() => window.openDynamicModal());
      await page.waitForTimeout(1200);
    }, clip: 'modal' },

  { id: 'configuration', caption: 'Configuration: every operational setting, per module',
    nav: () => window.renderView('configuration') },

  { id: 'roles', caption: 'Roles: the permission grant matrix',
    nav: () => window.renderView('roles') },
];

const CLIPS = {
  sidebar: { x: 0, y: 0, width: 300, height: 900 },
  'sidebar-wide': { x: 0, y: 0, width: 760, height: 900 },
  modal: null, // full page - the modal is centred and the backdrop is informative
};

(async () => {
  const { chromium } = resolvePlaywright();
  fs.mkdirSync(OUT, { recursive: true });

  const browser = await chromium.launch();
  const ctx = await browser.newContext({
    viewport: { width: 1440, height: 900 },
    deviceScaleFactor: 2, // legible when a reader zooms; roughly doubles file size
    colorScheme: THEME,
  });
  await ctx.addInitScript(([t, theme]) => {
    localStorage.setItem('erp_token', t);
    localStorage.setItem('erp_username', 'admin');
    localStorage.setItem('erp-theme', theme);
  }, [args.token, THEME]);

  const page = await ctx.newPage();
  const consoleErrors = [];
  page.on('pageerror', e => consoleErrors.push(String(e)));

  await page.goto(BASE, { waitUntil: 'networkidle' });
  await page.waitForTimeout(2500);

  const manifest = [];
  let failures = 0;

  for (const shot of SHOTS) {
    if (ONLY && !ONLY.includes(shot.id)) continue;
    try {
      await page.evaluate(shot.nav);
      await page.waitForTimeout(1800);
      if (shot.after) await shot.after(page);

      const file = path.join(OUT, `${shot.id}.png`);
      const clip = shot.clip ? CLIPS[shot.clip] : null;
      await page.screenshot(clip ? { path: file, clip } : { path: file, fullPage: false });

      manifest.push({ id: shot.id, file: path.basename(file), caption: shot.caption });
      console.log(`  [ok]   ${shot.id}.png`);
    } catch (err) {
      failures++;
      console.error(`  [fail] ${shot.id}: ${err.message}`);
    }
  }

  await browser.close();

  // A manifest so the guides can reference shots by id, and so a reviewer can
  // see at a glance what the set is meant to contain.
  fs.writeFileSync(path.join(OUT, 'MANIFEST.md'),
    `# Screenshot manifest\n\n` +
    `<!-- Generated by docs/guides/capture-screenshots.js. Re-take with that script; do not edit. -->\n\n` +
    `Captured ${new Date().toISOString().slice(0, 10)} against \`${BASE}\`, ${THEME} theme, 1440x900 @2x.\n\n` +
    `| Shot | File | Shows |\n|---|---|---|\n` +
    manifest.map(m => `| \`${m.id}\` | [${m.file}](${m.file}) | ${m.caption} |`).join('\n') + '\n');

  if (consoleErrors.length) {
    console.warn(`\nNote: ${consoleErrors.length} page error(s) during capture — the shots may show an error state:`);
    consoleErrors.slice(0, 5).forEach(e => console.warn('  ' + e));
  }
  console.log(`\n${manifest.length} shot(s) written to ${OUT}${failures ? `, ${failures} failed` : ''}`);
  process.exit(failures ? 1 : 0);
})();
