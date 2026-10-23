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
last_verified: 2026-10-23
review_by: 2027-01-21
supersedes: none
superseded_by: none
---

# Browser boundary inventory

Scope is the ERP-owned native browser shell (`public/app.js`, `public/index.html`) and its 18 lazy
view modules (`public/view-*.js`). The vendored `qrcode.min.js` is excluded from application-owned
counts; it remains subject to the existing supply-chain controls.

This revision records a **completed inline-handler migration**, an **enforced strict `script-src`**
and (Stage 134.8, 2026-10-23) a **completed HTML-sink classification**: every interpolation the
scanner cannot prove escaped was judged by hand, and the unsafe ones were fixed.

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

## 3. Dynamic HTML sinks — classified (Stage 134.8, 2026-10-23)

`node docs/qa/xss-sink-scan.cjs` (in the repository, reproducible) lists every `${...}` inside an HTML
template literal in `public/*.js` - all modules, not only the 18 view files of the first revision - and
classifies it mechanically: **escaped** (an escaping helper: `escapeHTMLText`, any `*esc*` /
`*Escape*` helper such as `crmEsc` / `hrEsc` / `cfgEsc`, `encodeURIComponent`, `actionAttrs`),
**number** (counts, amounts, money / date formatters), **literal** (every branch a literal),
**markup** (a nested template or a helper that returns markup, judged where it is built) and
**candidate** (none of these).

On 2026-10-23 the scan found **4,010** interpolations, **993** of them candidates. Every candidate was
judged by hand against its source (four reviewers, one file group each):

| Judgement | Count | Meaning |
|---|---:|---|
| `SAFE-number` | 309 | A count, amount, date or boolean |
| `SAFE-fixed` | 289 | A literal, enum or class/width written in the source, or a label defined in Go (`RegisterReport`) |
| `SAFE-escaped` | 243 | Escaped where it was built, or assembled from escaped parts |
| **`UNSAFE` - fixed** | **152** | Server / user data written raw; now escaped at the source |
| `UNSURE` | 0 | |

The fixes include shared choke points, which cover callers the scanner could not see:
`renderErrorPanel` (every server error message shown in a panel), the typeahead's empty row and the
setup hints (admin-typed custom labels), the profile page, the dynamic form's labels and Select
options (admin-typed field definitions), the receipt's `money()` label (a typed gift-card number),
the print-template renderer (`trade-docs.js` style values from stored layout JSON), the warehouse
cockpit sections, finance account / period / TDS rows, HR records and the OMS settlement rows.
The judgements, with a reason for each, are in
`docs/security/browser-boundary-sink-classification.json` (`review.items`); line numbers are as
reviewed and drift with later edits.

A re-scan after the fixes reports 858 candidates: the 841 judged safe plus helpers the fixes now
route through. The scanner is mechanical by design - it is the list a reviewer works, not a verdict.
Known scanner gaps (found by the review): a template with no `<` in it that is still assigned to
`innerHTML` (typeahead rows), and markup assembled in a variable built elsewhere (`inner`,
`tableHead`) - both were fixed at the source.

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

This revision closes the inline-handler migration, the CSP enforcement step and the 47.8.1
classification. Still open:

- **New code** - run `node docs/qa/xss-sink-scan.cjs --candidates` on a change to `public/` and
  judge any new candidate; an escape-by-default tagged-template helper would retire the class.
- **47.8.4** - payload coverage exists for the named surfaces but not for *every* shared renderer;
  the revoked-session proof is `internal/server/stage49_2_2_session_revocation_test.go`.
- **style-src** - 629 inline-style violations are reported, not enforced; retiring them is the next
  step this mechanism is set up for.
- **47.8.3** (session cookie-versus-token topology) and **47.8.5** (vendor assessment, tracked as
  26.11.1) stay excluded under their own existing gates.
