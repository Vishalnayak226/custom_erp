'use strict';
// 47.8.4: DOM-XSS regression checks for the delegated-action boundary and the
// remaining named untrusted-data surfaces.
//
// The companion file browser-xss-boundary.cjs covers the render sinks for
// logs, report rows, PIM reports, imported master data and CSV preview
// errors. This file covers what 47.8.2 changed and what that file does not
// reach:
//
//   1. the delegated-action argument channel - the replacement for the inline
//      `onclick="fn('${row.id}')"` attributes, where an interpolated record
//      id used to be unescaped JavaScript source;
//   2. the action NAME channel, which must not become an arbitrary-function
//      call primitive;
//   3. connector/webhook failure messages, API error envelopes and attachment
//      filenames, as text-only sinks;
//   4. a durable guard that no inline handler attribute returns to the
//      shipped sources, since one would silently require 'unsafe-inline'
//      again and the CSP would then refuse it in production rather than in a
//      test.
//
// Run: node docs/qa/browser-xss-action-boundary.cjs <absolute evidence directory>
const assert = require('node:assert/strict');
const fs = require('node:fs');
const http = require('node:http');
const path = require('node:path');
const { resolvePlaywright } = require('../guides/capture-screenshots.js');

const out = process.argv[2];
assert(out && path.isAbsolute(out), 'An explicit absolute evidence directory is required');
const repo = path.resolve(__dirname, '../..');
const publicRoot = path.join(repo, 'public');
const appSource = fs.readFileSync(path.join(publicRoot, 'app.js'), 'utf8').replace(/\r\n/g, '\n');

// --- the production choke point under test, sliced from the real source ----
function slice(startMarker, endMarker) {
  const a = appSource.indexOf(startMarker);
  assert(a >= 0, `Missing production source boundary: ${startMarker}`);
  const b = appSource.indexOf(endMarker, a);
  assert(b > a, `Missing production source boundary: ${endMarker}`);
  return appSource.slice(a, b + endMarker.length);
}

const escapeHelper = slice('function escapeHTMLText(s) {', "\n}");
const dispatcher = slice(
  "const ACTION_ARG_ELEMENT = '$el';",
  "document.addEventListener('submit', handleDelegatedAction);"
);

// --- guard: no inline handler attribute may return ------------------------
const INLINE_HANDLER_RE = /\son(click|change|input|submit|keydown|keyup|mouseover|mouseout|focus|blur|load|error)\s*=/g;
const shippedBrowserFiles = ['app.js', 'index.html', 'db.js', 'qz-print.js', 'theme-boot.js']
  .concat(fs.readdirSync(publicRoot).filter(f => /^view-.*\.js$/.test(f)))
  .filter(f => fs.existsSync(path.join(publicRoot, f)));

const inlineOffenders = [];
for (const file of shippedBrowserFiles) {
  const body = fs.readFileSync(path.join(publicRoot, file), 'utf8');
  const hits = body.match(INLINE_HANDLER_RE);
  if (hits) inlineOffenders.push({ file, count: hits.length });
}

const server = http.createServer((req, res) => {
  res.writeHead(200, { 'Content-Type': 'text/html; charset=utf-8' });
  res.end('<!doctype html><html><body><main id="root"></main></body></html>');
});

(async () => {
  await new Promise(resolve => server.listen(0, '127.0.0.1', resolve));
  const { port } = server.address();
  const browser = await resolvePlaywright().chromium.launch();
  const results = [];
  try {
    const page = await browser.newPage();
    const pageErrors = [];
    page.on('pageerror', e => pageErrors.push(String(e.message)));
    await page.goto(`http://127.0.0.1:${port}/`);
    await page.addScriptTag({ content: `window.__xss = 0;\n${escapeHelper}\n${dispatcher}` });

    // --- 1. hostile values through the action ARGUMENT channel -------------
    // Each payload is a real breakout attempt against the attribute the old
    // inline handlers built: close the attribute, close the string, open a
    // new handler.
    const argumentPayloads = [
      `" onclick="window.__xss++" x="`,
      `'); window.__xss++; ('`,
      `<img src=x onerror="window.__xss++">`,
      `" autofocus onfocus="window.__xss++" "`,
      `\\'); window.__xss++; //`,
      `</button><img src=x onerror="window.__xss++">`,
      `" data-act="__evil" "`,
    ];
    const argumentCheck = await page.evaluate((payloads) => {
      const received = [];
      window.__probe = (...args) => received.push(args);
      window.__evil = () => { window.__xss++; };
      const host = document.createElement('div');
      document.body.append(host);
      // Built exactly the way a renderer builds a row action.
      host.innerHTML = payloads
        .map(p => `<button ${window.actionAttrs('__probe', [p, 'second'])}>go</button>`)
        .join('');
      const buttons = Array.from(host.querySelectorAll('button'));
      buttons.forEach(b => b.click());
      const result = {
        buttonsRendered: buttons.length,
        receivedExactly: received.map(a => a[0]),
        secondArgIntact: received.every(a => a[1] === 'second'),
        // No payload may have produced an element or attribute of its own.
        noInjectedImage: !host.querySelector('img'),
        noInjectedHandlerAttribute: buttons.every(b => !b.hasAttribute('onclick') && !b.hasAttribute('onfocus')),
        // The breakout that tries to inject a competing data-act must not
        // have created a second action element.
        actionElementCount: host.querySelectorAll('[data-act]').length,
        xss: window.__xss,
      };
      host.remove();
      return result;
    }, argumentPayloads);

    assert.equal(argumentCheck.buttonsRendered, argumentPayloads.length,
      'Every hostile payload must still render exactly one button');
    assert.deepEqual(argumentCheck.receivedExactly, argumentPayloads,
      'Each hostile value must reach the handler verbatim, as data');
    assert.equal(argumentCheck.secondArgIntact, true, 'A hostile first argument must not corrupt later arguments');
    assert.equal(argumentCheck.noInjectedImage, true, 'A payload injected an element');
    assert.equal(argumentCheck.noInjectedHandlerAttribute, true, 'A payload injected an event-handler attribute');
    assert.equal(argumentCheck.actionElementCount, argumentPayloads.length, 'A payload injected an extra action element');
    assert.equal(argumentCheck.xss, 0, 'A hostile action argument executed');
    results.push({ fixture: 'delegated action arguments', ...argumentCheck });

    // --- 2. the action NAME channel is not a call primitive ---------------
    // The dispatcher looks a name up on window. It must reach only what a
    // renderer deliberately named - never an arbitrary global, and never a
    // non-function property that would throw on apply.
    const nameCheck = await page.evaluate(() => {
      window.__xss = 0;
      const host = document.createElement('div');
      document.body.append(host);
      const attempts = [
        'eval',
        'Function',
        'constructor',
        '__proto__',
        'alert',
        'notDefinedAnywhere',
      ];
      // A renderer would never write these, but a hostile value reaching the
      // name position (through a bug elsewhere) must still be inert.
      host.innerHTML = attempts
        .map(n => `<button data-act="${n}" data-act-args="[&quot;window.__xss++&quot;]">x</button>`)
        .join('');
      const before = window.__xss;
      Array.from(host.querySelectorAll('button')).forEach(b => b.click());
      const after = window.__xss;
      // A string arg handed to eval would increment; prove it did not.
      const result = { attempted: attempts.length, xssBefore: before, xssAfter: after };
      host.remove();
      return result;
    });
    assert.equal(nameCheck.xssAfter, 0,
      'A hostile data-act name reached an executing global - the dispatcher must not be a call primitive');
    results.push({ fixture: 'delegated action names', ...nameCheck });

    // --- 3. malformed args must fail closed -------------------------------
    const malformedCheck = await page.evaluate(() => {
      window.__xss = 0;
      let called = 0;
      window.__probeMalformed = () => { called++; };
      const host = document.createElement('div');
      document.body.append(host);
      host.innerHTML = [
        `<button data-act="__probeMalformed" data-act-args="not json">a</button>`,
        `<button data-act="__probeMalformed" data-act-args="{&quot;a&quot;:1}">b</button>`,
        `<button data-act="__probeMalformed" data-act-args="[1,2]">c</button>`,
      ].join('');
      Array.from(host.querySelectorAll('button')).forEach(b => b.click());
      const result = { called, xss: window.__xss };
      host.remove();
      return result;
    });
    // Only the well-formed array may dispatch; the other two fail closed.
    assert.equal(malformedCheck.called, 1, 'Malformed data-act-args must not dispatch');
    assert.equal(malformedCheck.xss, 0, 'Malformed data-act-args executed something');
    results.push({ fixture: 'malformed action arguments fail closed', ...malformedCheck });

    // --- 4. connector messages, API errors, attachment names as text ------
    // These three are rendered by production code paths that escape through
    // the same helper; the check is that the helper actually neutralises the
    // shapes those sources produce, placed in both text and attribute
    // position, and that the result parses as inert markup.
    const textSinkCheck = await page.evaluate(() => {
      window.__xss = 0;
      const sources = {
        connectorMessage: `Shopify sync failed: <script>window.__xss++</script>`,
        connectorPayload: `{"sku":"<img src=x onerror=window.__xss++>"}`,
        apiError: `<svg onload="window.__xss++">`,
        attachmentName: `invoice" onerror="window.__xss++.png`,
        csvCell: `=cmd|' /C calc'!A0<img src=x onerror=window.__xss++>`,
      };
      const host = document.createElement('div');
      document.body.append(host);
      const rendered = {};
      for (const [k, v] of Object.entries(sources)) {
        // Text position and quoted-attribute position, the two the app uses.
        host.innerHTML = `<div title="${window.escapeHTMLText(v)}">${window.escapeHTMLText(v)}</div>`;
        const div = host.firstElementChild;
        rendered[k] = {
          textIsExact: div.textContent === v,
          titleIsExact: div.getAttribute('title') === v,
          noChildElements: div.children.length === 0,
        };
      }
      const result = { rendered, noImage: !host.querySelector('img'), xss: window.__xss };
      host.remove();
      return result;
    });
    for (const [k, v] of Object.entries(textSinkCheck.rendered)) {
      assert.equal(v.textIsExact, true, `${k}: must render as its exact literal text`);
      assert.equal(v.titleIsExact, true, `${k}: must survive quoted-attribute position exactly`);
      assert.equal(v.noChildElements, true, `${k}: produced markup instead of text`);
    }
    assert.equal(textSinkCheck.xss, 0, 'A connector/error/attachment/CSV payload executed');
    results.push({ fixture: 'connector, API error, attachment and CSV text sinks', ...textSinkCheck });

    assert.deepEqual(pageErrors, [], 'The boundary harness must not produce page errors');

    // --- 5. the inline-handler guard --------------------------------------
    assert.deepEqual(inlineOffenders, [],
      'An inline event-handler attribute is back in a shipped browser file. ' +
      "The enforced CSP is script-src 'self' with no inline exception, so this " +
      'handler will not run in production. Convert it with actionAttrs() ' +
      '(public/app.js) instead of relaxing the policy.');
    results.push({ fixture: 'no inline handler attributes in shipped sources', filesScanned: shippedBrowserFiles.length, offenders: inlineOffenders });

    fs.mkdirSync(out, { recursive: true });
    fs.writeFileSync(path.join(out, 'browser-xss-action-boundary.json'), JSON.stringify(results, null, 2) + '\n');
    console.log(JSON.stringify(results, null, 2));
  } finally {
    await browser.close();
    await new Promise(resolve => server.close(resolve));
  }
})().catch(error => { console.error(error); process.exitCode = 1; });
