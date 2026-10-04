---
doc_id: DOC-BROWSER-BOUNDARY-INVENTORY
title: Browser boundary inventory
type: reference
status: draft
owner: security-owner
approvers: [security-owner, engineering-owner]
audience: [security, engineering, QA]
applies_to: native browser shell and lazy view modules
authority: source
confidentiality: internal
last_verified: 2026-10-04
review_by: 2026-10-18
supersedes: none
superseded_by: none
---

# Browser boundary inventory

Scope is the ERP-owned native browser shell (`public/app.js`, `public/index.html`) and its 18 lazy
view modules (`public/view-*.js`). The vendored `qrcode.min.js` is excluded from application-owned
counts; it remains subject to the existing supply-chain controls.

This revision records a **completed inline-handler migration and an enforced strict `script-src`**,
and an **incomplete HTML-sink classification**. The two are reported separately because only the
first is finished.

Reproduce every count in this page from the repository root:

```powershell
# inline event-handler attributes (expect zero)
rg --count '\son(click|change|input|submit|keydown|keyup|mouseover|mouseout|focus|blur|load|error)\s*=' public -g app.js -g 'view-*.js' -g index.html
# dynamic HTML insertion sinks
rg --count '\b(innerHTML|outerHTML)\s*=|insertAdjacentHTML\s*\(' public -g app.js -g 'view-*.js'
```

## 1. Inline handlers — migrated, zero remaining

All **126** inline event-handler attributes are gone: 109 across `app.js` and the 18 view modules,
plus 17 in `index.html`. They were replaced by a single delegated dispatcher
(`handleDelegatedAction` in `public/app.js`) driven by `data-act` / `data-act-args` attributes that
`actionAttrs()` emits.

| Former location | Handlers migrated |
|---|---:|
| `index.html` | 17 |
| `view-admin.js` | 15 |
| `view-finance.js` | 11 |
| `app.js` | 10 |
| `view-warehouse.js` | 10 |
| `view-oms.js` | 9 |
| `view-procurement.js` | 9 |
| `view-pos.js` | 8 |
| `view-manufacturing.js` | 7 |
| `view-transfers.js` | 7 |
| `view-documents.js` | 6 |
| `view-hr.js` | 4 |
| `view-expenses.js` | 3 |
| `view-printing.js` | 3 |
| `view-rf-traceability.js` | 3 |
| `view-reports.js` | 2 |
| `view-help.js` | 1 |
| `view-returns.js` | 1 |
| **Total** | **126** |

Event types involved: 105 `click`, 4 `change`, 17 `submit` (all in `index.html`). Shapes: 107 were a
single function call; two were special-cased into named functions (`goToDefaultView`,
`selectFieldText`) because the dispatcher calls one function per action and has no statement
sequencing — deliberately, since sequencing would mean evaluating source.

### Why this was a security fix and not only a CSP enabler

An inline handler interpolated its arguments as **JavaScript source inside an HTML attribute**:

```js
`<button onclick="deleteRow('${row.id}')">`   // before
`<button ${actionAttrs('deleteRow', [row.id])}>`  // after
```

A `row.id` containing a quote did not merely break the handler — it closed the attribute and the
string, and anything after it executed. Wherever that value came from imported master data, a
connector payload, a report row or a CSV cell, it was a live injection sink. Arguments now travel
JSON-encoded and HTML-escaped, so a hostile value can only ever arrive as data. Proven by
`docs/qa/browser-xss-action-boundary.cjs`, which drives seven real breakout payloads through the
rendering path and asserts each one reaches the handler byte-for-byte while executing nothing.

### Dispatcher hardening

`resolveActionFunction` refuses to be a general-purpose call primitive. A plain `window[name]`
lookup was not sufficient: `window['eval']` is a function, so dispatching a name with a string
argument would have executed that string. Found by the harness above, which asserts a hostile
`data-act` name is inert. Three guards: the name must be a plain identifier, must be an **own**
property of `window` (excluding `constructor`, `__proto__`, `toString`), and must not be a native
built-in (which excludes `eval`, `Function`, `alert`, `fetch`, `open` by construction rather than
by a denylist that has to track the platform). No renderer can currently influence a name — they
are all string literals in this repository — so this is defence in depth, not a patched hole.

### Known dangling references (pre-existing)

Four action names are referenced only by `index.html` and defined nowhere in the tree:
`closeEditPrefixModal`, `submitEditPrefix`, `closeAddLabelModal`, `submitAddLabel`. They belong to
the static `#edit-prefix-modal` and `#add-label-modal` overlays, which are dead markup — the real
Prefix-Configuration and Label-Replacement editors are prompt-based flows in `view-admin.js`
(`editPrefixConfig`, `addNewLabelReplacement`). These buttons threw a silent `ReferenceError`
before this migration and now log `[action] no such action function` instead, so diagnosability
improved and behaviour did not change. **Not fixed here**: removing the dead overlays or
implementing the four functions is a UI decision, not boundary work.

## 2. Content Security Policy — strict `script-src` enforced

The enforced policy is now:

```
default-src 'self'; script-src 'self'; style-src 'self' 'unsafe-inline'; font-src 'self';
img-src 'self' data: blob:; connect-src 'self' ws://localhost:* wss://localhost.qz.io:*;
object-src 'none'; base-uri 'self'; form-action 'self'; frame-ancestors 'none'
```

`'unsafe-inline'` is gone from `script-src`. The report-only header has moved on to the next
candidate, strict `style-src`.

**No nonce and no hash list**, deliberately. A nonce exists to permit inline `<script>` elements and
there are none — `index.html` loads six external scripts and contains no inline block — so a nonce
would add a per-request value to generate and thread through the HTML, defeating static caching of
`index.html`, in order to permit something this app does not do. It would also not have addressed
the actual blocker: `script-src` nonces do not whitelist inline event-handler **attributes**; that
needs `'unsafe-hashes'`, which is a weaker position than removing them. `'self'` with zero inline
script is the strictest and simplest of the three options.

### Two-phase rollout evidence

Both phases were measured in Chromium across all 29 dispatched views, collecting
`securitypolicyviolation` events:

| Phase | Enforced `script-src` | Violations (enforced) | `script-src` violations | Page errors |
|---|---|---:|---:|---:|
| 1 — report-only candidate | `'self' 'unsafe-inline'` | 0 | 0 | 0 |
| 2 — flipped to blocking | `'self'` | 0 | 0 | 0 |
| 3 — after dispatcher hardening | `'self'` | 0 | 0 | 0 |

Phase 1 is what authorised the flip: the strict candidate reported **zero** violations of any
directive before it was enforced. The 629 report-only violations now visible are all
`style-src-attr`, i.e. the next migration this mechanism is queued to carry, not a regression.

Separately, all **128** `data-act` reference sites were checked statically against every way this
codebase publishes a function to `window` (`app.js` top-level declarations, `window.X =`, and view
module export lists, which `loadViewModule` publishes). All resolve except the four dangling names
above. Zero cross-module references remain.

## 3. Dynamic HTML sinks — classification incomplete

**461** insertion sinks contain **637** interpolated expressions. Mechanical classification:

| Class | Count | Meaning |
|---|---:|---|
| `safe-text` | 192 | Escaped at the sink (`escapeHTMLText`, its alias `cfgEsc`, `encodeURIComponent`, `actionAttrs`) |
| `no-interpolation` | 115 | Literal application-owned markup; nothing interpolated |
| `trusted` | 74 | Numbers, aggregates, fixed enum/class maps, markup-returning helpers |
| `markup-loop` | 43 | `.map(...).join('')` building rows from parts classified where they are built |
| `trusted-config` | 17 | Tab/column/param descriptors defined as literals in this repository |
| `assembled-markup` | 11 | A local holding markup concatenated earlier in the same renderer |
| **`data` (candidates)** | **300** | **Flagged for review — see the accuracy note below** |

### Accuracy of the 300

**The 300 figure is a candidate list, not a defect count.** A systematic 16-item sample (every
~19th finding) was judged by hand against the source: **6** were genuine unescaped data
interpolations or needed a human call, **10** were false positives — fixed colour values
(`data.balanced ? '#10b981' : '#ef4444'`), fixed labels (`rule ? 'Edit' : 'New'`), counts
(`atCapCount`), markup helpers (`autoNumberField(...)`), assembled-markup locals (`smallBtn`), and
nested templates whose inner interpolation *is* escaped. On that sample the real population is on
the order of **110**, but the sample is small and the per-expression judgement is what matters.

Sound separation of trusted configuration (`t.label`) from untrusted data (`line.sku`) needs data-
flow analysis the classifier does not do, and a wrong classification is worse than an acknowledged
unknown — which is why this section is reported as incomplete rather than resolved.

Per-file candidate distribution, for whoever works the list:

| File | Sinks | `safe-text` | `data` candidates |
|---|---:|---:|---:|
| `view-warehouse.js` | 80 | 7 | 49 |
| `view-oms.js` | 36 | 29 | 46 |
| `view-pim.js` | 47 | 41 | 34 |
| `view-procurement.js` | 25 | 14 | 26 |
| `view-reports.js` | 37 | 4 | 24 |
| `view-pos.js` | 18 | 15 | 21 |
| `view-admin.js` | 42 | 12 | 20 |
| `app.js` | 52 | 16 | 19 |
| `view-printing.js` | 17 | 14 | 17 |
| `view-documents.js` | 23 | 13 | 16 |
| `view-finance.js` | 24 | 5 | 14 |
| `view-help.js` | 12 | 9 | 6 |
| `view-hr.js` | 17 | 0 | 4 |
| `view-manufacturing.js` | 14 | 0 | 1 |
| `view-returns.js` | 3 | 5 | 1 |
| `view-rf-traceability.js` | 3 | 7 | 1 |
| `view-transfers.js` | 5 | 1 | 1 |
| `view-assets.js` | 3 | 0 | 0 |
| `view-expenses.js` | 3 | 0 | 0 |

### One escaping choke point, not two

`cfgEsc` was a second, byte-for-byte identical copy of `escapeHTMLText` with 55 call sites across
five view modules. A change to the escape rules would have had to be made twice. It is now a thin
alias delegating to `escapeHTMLText`, so there is one implementation; renaming the 55 callers is a
purely mechanical follow-up with no behavioural content.

## 4. Sink classes and their disposition

- **Safe text.** `escapeHTMLText` covers HTML metacharacters (`& < > " '`) and is correct for HTML
  text and quoted-attribute positions. It does **not** make a URL, a CSS value, JavaScript source
  or raw SVG safe; those need their own contextual handling and none is claimed here.
- **Trusted templates.** Literal application-owned markup, fixed enum-to-class mappings and
  generated help pages. Help HTML is trusted only because the KB generator escapes content and
  validates links before embedding it — the trust is in the generator, not the sink.
- **User/external data.** Imported master values, report rows and labels, connector payloads, API
  errors, attachment names, CSV validation messages and tenant-authored data must stay text.
- **Action arguments.** Now safe by construction (§1). This class previously had no control at all.

## 5. Payload regression coverage

Two committed harnesses, both driving real Chromium:

- `docs/qa/browser-xss-boundary.cjs` — integration/audit/system log rows, report columns and rows,
  PIM reports, PIM workbench (imported master data), CSV preview errors, attachment-link URL
  encoding.
- `docs/qa/browser-xss-action-boundary.cjs` — delegated action arguments (7 breakout payloads),
  action names (6 call-primitive attempts), malformed `data-act-args` failing closed, and
  connector messages / API error envelopes / attachment filenames / CSV cells in both text and
  attribute position. It also carries a **guard that fails if any inline handler attribute returns
  to a shipped browser file**, since one would be silently refused by the now-enforced policy.

## 6. Remaining acceptance work

This revision closes the inline-handler migration and the CSP enforcement step. It does **not**
close 47.8.1 or 47.8.4 in full:

- **47.8.1** — adjudicate the 300 candidate interpolations in §3 per expression, and consider an
  escape-by-default tagged-template sink helper so new renderers cannot reintroduce the class.
- **47.8.4** — payload coverage exists for the named surfaces but not for *every* shared renderer;
  the "stolen/revoked session is unusable" proof remains separate and unstarted here.
- **style-src** — 629 inline-style violations are reported, not enforced; retiring them is the next
  step this mechanism is set up for.
- **47.8.3** (session cookie-versus-token topology) and **47.8.5** (vendor assessment, tracked as
  26.11.1) stay excluded under their own existing gates.
