'use strict';

// 47.10.4 behavioural check: a table of more than 400 rows renders in chunks (shell-nav.js),
// and a screen's own DOM-row filter or sort must still see every row, with the "show more"
// button usable from the keyboard and named for a screen reader. Local QA tooling against a
// LOOPBACK scratch server (Playwright resolved like docs/guides/capture-screenshots.js).
//
//   node docs/qa/big-table-rows.cjs --base http://127.0.0.1:8740 --tokens tokens.txt \
//        [--user manager1] [--role "Store Manager"] [--rows 5200] [--channel msedge]
//
// The table is built inside #view-root with the markup shape the screens use, and the filter
// and sort are bubble-phase handlers like theirs (hide rows whose text lacks the query; reorder
// the DOM rows). Exit 0 = every check passed, 1 = a check failed, 2 = could not run.
// stdout ends with one line: BIG-TABLE RESULT: PASS|FAIL

const fs = require('node:fs');
const { resolvePlaywright } = require('../guides/capture-screenshots.js');

function parseArgs(argv) {
  const a = { user: 'manager1', role: 'Store Manager', tenant: 'default', rows: '5200' };
  for (let i = 0; i < argv.length; i++) a[argv[i].replace(/^--/, '')] = argv[++i];
  return a;
}

// Builds a table under #view-root. With `filter`, adds a text box and its handler;
// with `sort`, a header click handler that reverses the DOM rows by name.
function build(page, { id, n, filter, sort }) {
  return page.evaluate(({ id, n, filter, sort }) => {
    const root = document.getElementById('view-root');
    let rows = '';
    for (let i = 0; i < n; i++) rows += `<tr><td>row-${i}</td><td>${i % 50 === 0 ? 'target' : 'other'}</td></tr>`;
    const wrap = document.createElement('div');
    wrap.innerHTML = `<input id="${id}-start" aria-label="Start"><input id="${id}-q" aria-label="Filter"><table id="${id}"><thead><tr><th id="${id}-th">Name</th><th>Kind</th></tr></thead><tbody>${rows}</tbody></table>`;
    root.appendChild(wrap);
    const tb = wrap.querySelector('tbody');
    if (filter) {
      wrap.querySelector('#' + id + '-q').addEventListener('input', e => {
        const q = e.target.value.toLowerCase();
        tb.querySelectorAll('tr').forEach(r => { r.style.display = r.textContent.toLowerCase().includes(q) ? '' : 'none'; });
      });
    }
    if (sort) {
      wrap.querySelector('#' + id + '-th').addEventListener('click', () => {
        [...tb.rows].sort((a, b) => b.cells[0].textContent.localeCompare(a.cells[0].textContent, undefined, { numeric: true })).forEach(r => tb.appendChild(r));
      });
    }
  }, { id, n, filter, sort });
}

(async () => {
  const args = parseArgs(process.argv.slice(2));
  if (!args.base || !args.tokens) { console.error('need --base and --tokens'); process.exit(2); }
  const tokens = fs.readFileSync(args.tokens, 'utf8').split('\n').map(s => s.trim()).filter(Boolean);
  if (!tokens.length) { console.error('no tokens'); process.exit(2); }
  const N = Number(args.rows);
  const browser = await resolvePlaywright().chromium.launch(args.channel ? { channel: args.channel } : {});
  const context = await browser.newContext({ viewport: { width: 1440, height: 900 } });
  await context.addInitScript(({ token, user, role, tenant }) => {
    try { localStorage.setItem('erp_token', token); localStorage.setItem('erp_username', user); localStorage.setItem('erp_role', role); localStorage.setItem('erp_tenant_id', tenant); } catch (e) { /* blocked */ }
  }, { token: tokens[0], user: args.user, role: args.role, tenant: args.tenant });
  const page = await context.newPage();
  const fails = [];
  const check = (name, ok, detail) => { console.log((ok ? 'ok   ' : 'FAIL ') + name + (detail ? ' - ' + detail : '')); if (!ok) fails.push(name); };
  const fresh = async () => {
    await page.goto(args.base + '/?r=1#/view/home', { waitUntil: 'load' });
    await page.waitForSelector('#view-root', { timeout: 20000 });
    await page.waitForTimeout(2500);
  };
  try {
    await fresh();
    await build(page, { id: 'bt', n: N, filter: true });
    await page.waitForTimeout(600);
    const held = await page.evaluate(() => document.querySelectorAll('#bt tbody tr').length);
    check('large table is chunked', held < N, `${held} of ${N} rows in the DOM`);
    const label = await page.evaluate(() => document.querySelector('#bt .chunk-more')?.getAttribute('aria-label') || '');
    check('show-more button has an accessible name', label === `Show all ${N} rows`, label);
    await page.fill('#bt-q', 'target');
    await page.waitForTimeout(400);
    const want = Math.ceil(N / 50);
    const vis = await page.evaluate(() => ({ total: document.querySelectorAll('#bt tbody tr').length, shown: [...document.querySelectorAll('#bt tbody tr')].filter(r => r.style.display !== 'none').length }));
    check('filter sees the held rows', vis.total === N && vis.shown === want, `${vis.shown} shown of ${vis.total}; expected ${want}`);

    await fresh();
    await build(page, { id: 'bt2', n: N, sort: true });
    await page.waitForTimeout(600);
    await page.click('#bt2-th');
    await page.waitForTimeout(400);
    const sorted = await page.evaluate(() => ({ total: document.querySelectorAll('#bt2 tbody tr').length, first: document.querySelector('#bt2 tbody tr td').textContent }));
    check('sort sees the held rows', sorted.total === N && sorted.first === `row-${N - 1}`, `first row ${sorted.first}, ${sorted.total} rows`);

    await fresh();
    await build(page, { id: 'bt3', n: N });
    await page.waitForTimeout(600);
    await page.focus('#bt3-q');
    for (let i = 0; i < 400; i++) {
      await page.keyboard.press('Tab');
      if (await page.evaluate(() => document.activeElement?.classList.contains('chunk-more'))) break;
    }
    check('Tab reaches the show-more button', await page.evaluate(() => document.activeElement?.classList.contains('chunk-more')));
    await page.keyboard.press('Enter');
    await page.waitForTimeout(400);
    const kb = await page.evaluate(() => ({ total: document.querySelectorAll('#bt3 tbody tr').length, tag: document.activeElement?.tagName, text: document.activeElement?.textContent }));
    check('Enter shows every row and moves focus to the first new one', kb.total === N && kb.tag === 'TR' && kb.text.startsWith('row-200'), JSON.stringify(kb));
  } catch (e) {
    console.error('could not run: ' + e.message);
    await browser.close();
    process.exit(2);
  }
  await browser.close();
  console.log('BIG-TABLE RESULT: ' + (fails.length ? 'FAIL (' + fails.join('; ') + ')' : 'PASS'));
  process.exit(fails.length ? 1 : 0);
})();
