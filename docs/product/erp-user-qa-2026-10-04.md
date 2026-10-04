---
doc_id: DOC-PRODUCT-USER-QA-20261004
title: User QA round triage — deployed-build feedback, 4 October 2026
type: reference
status: draft
owner: engineering-owner
approvers: [product-owner, qa-owner]
audience: [product, engineering, QA, implementation]
applies_to: deployed build as operated by the user on 2026-10-04; shared uncommitted tree
authority: historical
confidentiality: internal
last_verified: 2026-10-04
review_by: 2026-11-04
supersedes: none
superseded_by: none
verification_scope: root causes confirmed by source inspection; defect reproduction in a running instance not performed except where stated
format: work-register
---

# User QA round — 2026-10-04

Raw feedback from the user operating the **deployed** build, triaged. This is the
real-user acceptance that [Stage 55.0](../micro_checklist.md) explicitly disclaims
("one reviewer applied beginner/operator/administrator lenses; this is not real-user
acceptance" / "no transaction submission in the audit"). The user *did* submit
transactions — created a PO, a GRN, a Transfer Order, a Bin, a Vendor — which is why
this round surfaces defects the browser audit could not.

**Read the verdict first.** These are not all bugs: several are questions with real
answers, and one rests on a misreading of a screen rather than a defect.

## How this is filed

| Where | What goes there |
| --- | --- |
| Stage 55 sub-items | Defects that sharpen an already-open usability item. No new stage needed. |
| Stage 57 | Genuinely new scope Stage 55 does **not** authorize (barcode config, non-sellable assets, nav regroup, offer targeting, video). |
| This doc | The verbatim report, the verdict, and the root cause where it is known. |

## A. Confirmed defects

| # | Report | Root cause / verdict | Filed |
| --- | --- | --- | --- |
| A1 | "Why it is asking authorization while bulk download template?" | **Confirmed and fixed this pass.** `GET /api/v1/import/{doctype}/template` is wrapped in `apiMiddleware` (`internal/server/routes.go:1132`), so it requires the bearer token. `downloadImportTemplate` built a plain anchor `href`, which sends no `Authorization` header; the browser answered the 401 with its own credential prompt. It now fetches through `apiFetch` and saves a blob. | done |
| A2 | `field "from_warehouse": location 'Location/HQ/2026/000001' is not a registered Location` in TO | **Confirmed.** `engines/location_masters.go:31` validates against the Location **code**, but the UI submits the **document id** (`Location/HQ/2026/000001`). Same id-vs-code family as Stage 51.1. The fix belongs at the shared validator, not per-form. | 55.6 |
| A3 | "I have item and GRN is showing no item. Loading item from PO not working." | Not yet root-caused. Breaks the core procurement chain, so it outranks most of this list. | 55.4 |
| A4 | "Unable to change status of RFQ" / "How to assign to vendors RFQ?" | No vendor-assignment flow on RFQ; status is consequently stuck. 55.4 already owns "source-linked requisition → quote → PO". | 55.4 |
| A5 | "I created vendor but not poping up or suggesting in PO creation" | Previously reported and fixed in-tree as **Stage 51.1** (master `id`/`code` invariant) — but **Stage 51 is deliberately undeployed**, so the deployed build still shows it. Verify against the tree before writing any new code. | 51 (verify) |
| A6 | "Unable to create bin as no zone. It should ask to create zone or create automatically." | Bin creation hard-blocks with no path to create the missing Zone. Same shape as the Department case (B2). | 55.7 |
| A7 | "If i click catlog in PIM, the scroller is coming back to 1st icon." | Tab strip resets scroll position on selection. 55.11 already owns "repair reflow". | 55.11 |
| A8 | "I don't see edit in offer management." | Missing edit action on an existing record. (The *targeting* half is new scope — see C4.) | 55.17 |
| A9 | Knowledge centre pages render `<!-- GENERATED ARTICLE - DO NOT EDIT BY HAND. Regenerate: ... -->` | A generator comment is leaking into rendered customer-facing output on "a few pages". | 48 |
| A10 | "reference po is not coming in drop down in vendor credit note debite note." | Empty PO picker on both note types. Plausibly the same id-vs-code cause as A2 — check them together. | 55.10 |

## B. Cross-cutting invariants

Each of these is one change at one choke point, not a per-screen sweep. They are worth
more than any individual item in section A because each closes a whole class.

| # | Report | Shape of the fix |
| --- | --- | --- |
| B1 | "Always in All UI, It should Only the Name not the code (Ex- Department/HQ/2026/000001). Everywhere." | One display-resolution helper for Link fields, applied at the shared renderer. The id stays the submitted value; only the label changes. The single highest-leverage item in the dump — and the visible half of A2/A10. |
| B2 | "If I click *No Department has been set up yet*, it should show only that Not PO screen. One universal implementation. If I set up master and click back on browser it should come back to PO screen and again start. But it will tell me to set up dept again — always show me set up dept without mandatory in the 2nd one once dept created." | A universal missing-prerequisite pattern: full-screen takeover instead of a half-broken form, return-to-origin on browser Back, and the prompt degrades to non-blocking once the master exists. 55.3 owns "guided dependencies"; this is its acceptance criterion. |
| B3 | "Item / Requirement Description: If doesn't exist while typing create one, ask user create one, on the UI it self, ask required detail and create, and be on PO with that item name." Also raised for Vendor and for HSN Code. | Inline quick-create from the Link picker, returning to the origin form with the new record selected. 55.3 owns "lookup/quick-create". One implementation must serve Item, Vendor and HSN. |
| B4 | "All status (Status*) in all master setup should be default - Active" | A default on the `Select` field definition, applied once across master doctypes. |
| B5 | "I don't want *on this page* in header — rename it to better/std/best practice like a book/manual." | Label change in the docs shell. Trivial; grouped here because it is site-wide. |
| B6 | "Appointment Calendar — calender selection is normal, make it better/best and smooth. Change in all calender of erp." | One shared date/calendar control, replacing the current pickers everywhere. **No new dependency** — this must be built within the existing vanilla-JS constraint. |

## C. New scope — needs a decision before building

Stage 55's plan authorizes no new features, so these open **Stage 57**.

| # | Report | Decision needed |
| --- | --- | --- |
| C1 | "Why is barcode generate in Item level? It will be on variant level. I should have config to generate barcode at Item SKU level and at GRN level. And I should have config to generate barcode wrt date and same barcode for sku every time. I should be able to turn on." | Confirm the intended model: barcode identity moves to the variant, with two independent toggles — generation point (SKU / GRN) and stability (date-derived vs stable-per-SKU). GRN-level barcode generation was already raised in Stage 51; check the overlap. This touches stock identity, so it is not a small change. |
| C2 | "Also, I want to create non sellable asset as well." | Whether this is a new Item type on the existing Item master, or routes into the existing Asset module (55.16). Very different builds. |
| C3 | "gropu upder wms inside stock. stock will have inv, TO, location movement. rest all are wms." | Navigation regroup. Cheap, but pin the exact final tree before moving anything. |
| C4 | "Offer manager should have a group of design or sku to apply. Or category type. Its preety bad." | Offer targeting by SKU set / design group / category. Needs the targeting dimensions pinned down. |
| C5 | "PIMS is difficult. Make it easy for me. or explain me how to setup." | 55.11 already owns regrouping the 21 tabs. The "explain me" half is a guide gap — a PIM setup walkthrough in `USER_GUIDE.md`. Recommend the walkthrough first: it is cheap, and it tells us whether the UI or the docs was the real problem. |
| C6 | "I need a video of entire erp. One flow completely, not fast, normal speed, 1 video. With enhancement video will be refined. This video master will identify the change and replace that portion only, don't create whole video again and again end to end." | **Recommend deferring.** A segment-addressable video pipeline (scripted scene manifest, per-scene re-record, stitch) is a tooling project in its own right, and it would pull effort from a build that currently has confirmed defects in its core procurement chain. Worth doing — after section A is clear. |
| C7 | "10 of 10 left / Two-Factor Recovery / There should be no limit" | **Likely a misread, not a defect.** "10 of 10 left" is the count of *unused* single-use codes, not a cap you can hit. `engines/mfa_recovery.go:28` sets `RecoveryCodeCount = 10` (50 bits of entropy each); 10 is the industry norm (Google 10, GitHub 16), and Stage 49.2.3 deliberately requires them single-use and hashed. "Unlimited" is not something recovery codes can be — but **regenerating** a fresh set at any time is, and if that is the actual want, it may only need to be more discoverable. Confirm intent before touching this: it is a security control. |

## D. Questions — answered, and the doc gap each reveals

All six are answerable from the code today. That the user had to ask is itself the
finding: these belong in the guides, per the repo's second principle.

| Question | Answer | Gap |
| --- | --- | --- |
| "What is sale invpoce and vendor invoice?" | **SalesInvoice** (`db/migration.sql:334`, Sales/Transaction) is what *you bill a customer* — money owed to you. **VendorInvoice** (`db/migrations_stage17g_vendor_invoice.sql`, Procurement/Transaction) is the *supplier's bill to you* — money you owe. It links `po_id` **and** `grn_id`, because its job is the three-way match: what you ordered, what arrived, what you were charged. | Guide |
| "How to diiferntiate credit note debite note?" | Direction. **DebitNote** is vendor-facing: it reduces what you owe a supplier (purchase return, overbilling) and posts to `5150 Purchase Returns & Allowances` (`engines/notes.go`). **CreditNote** is customer-facing: it reduces what a customer owes you (`engines/cancellation_credit_note.go`). Rule of thumb: a debit note goes out to a vendor, a credit note goes out to a customer. | Guide |
| "Where is purchase return?" | **It does not exist as a flow.** `docs/specs/modules_overview.md:55` and `PRD.md` §4.4 describe a Purchase Return/RTV module, and `ERROR_CODES.md` reserves 5 codes for it, but the doctype was never built — already recorded in archived Stage 25.4 ("references a doctype that was never built"). Today only the *financial* half exists, via DebitNote. The inventory half (scan against the original GRN, move stock to RTV Pending) is genuinely absent. | **The docs claim a module that does not exist — fix the docs or build it.** |
| "what is pos profile?" | The till/terminal configuration a POS session opens against — `POSSession.pos_profile` is a Link to `POSProfile` (`db/migrations_stage20a_pos_maturity.sql:38`). It carries the register's defaults so a cashier can open a till without re-entering them. | Guide |
| "WHat is webhokk subscription - How to achieve this" | How an external system subscribes to ERP events. Internally: a business transaction calls `PublishEvent` inside its own DB transaction, which queues a row in the **outbox** (`engines/outbox.go`); `processOutbox` then enqueues a delivery job that POSTs to the subscribed URL, carrying the originating request's correlation id. The transactional outbox is what guarantees an event is never published for a transaction that rolled back. | Guide |
| "WHt is rate limit?" | A server-side throttle on request volume. What was seen is the `Rate Limit` toast (`public/app.js:869`) — the server rejected a burst and the UI surfaced it. It protects login and API endpoints from brute-force and runaway clients. Nothing is broken; it means slow down. | Guide |

## Recommended order

1. **A3** (GRN not loading PO items) — it breaks the core procurement chain.
2. **B1** (name not code) — one choke point, closes the most visible class, and is the display half of A2/A10.
3. **B2 + B3** (missing-prerequisite takeover + inline quick-create) — together these unblock Department, Zone (A6), Item, Vendor and HSN under one pattern.
4. **A2 + A10** (id-vs-code at the validator) — likely one fix for both.
5. **A5** — verify against the tree first; it may need no code at all, only a deploy.
6. The remaining section A items, then the section C decisions.
7. **D** — fold all six answers into the guides, and resolve the Purchase Return contradiction.
