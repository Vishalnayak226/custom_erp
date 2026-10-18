---
doc_id: DOC-ASSURANCE-ERP-FUNCTIONAL-AUDIT-20261005
title: Organization-operated ERP functional audit, first UI execution
type: record
status: draft
owner: qa-owner
approvers: [product-owner, operations-owner, finance-owner, engineering-owner]
audience: [product, engineering, QA, implementation]
applies_to: isolated local build and tenant on 2026-10-05
authority: historical
confidentiality: internal
last_verified: 2026-10-05
review_by: 2026-11-05
supersedes: none
superseded_by: none
verification_scope: one Chromium reviewer operating selected UI journeys; not all-module acceptance
---

# Organization-operated functional audit: first execution

This is **an audit and repair backlog, not a completion certificate**. The user asked to
inspect actual ERP work today and defer fixing bugs until the ERP build is complete. The
[20-module operating plan](../../product/erp-functional-verification-plan-2026-10-04.md)
remains the acceptance specification. The earlier [screen register](erp-usability-screen-register-2026-10-04.md)
inventoried routes and forms; it did not submit or reconcile transactions.

## Test boundary and proof standard

One tester used three lenses—first-time operator, supervisor/finance reviewer, and
engineering/security reviewer—not three independent people. Chromium drove visible
form fields, buttons, login/MFA, lists and dialogs against a newly created disposable
`custom_erp_org_20261005` database on loopback Postgres and a scratch server on port
8111 (stopped after capture; the isolated database and scratch evidence are retained).
No direct API write was used for business records. Read-only source inspection
and browser-captured responses were used to explain outcomes. The server binary was
built from the then-current dirty tree; static frontend assets were served from the
concurrently edited worktree, so this is a dated local snapshot, not a frozen release
or deployed-build result. Sensitive browser state and screenshots are kept outside the
repository in the session scratchpad; this record contains no credentials or tokens.

**Added review angles for the next pass:** the Account Head must follow AP, AR, stock
valuation, cash/bank, payroll and asset effects into the subledgers, GL, corrections and
period close; a qualified CA must independently challenge applicable tax/statutory
treatment, source-to-return totals, cutoffs and audit/export evidence. These are
separate roles, not synonyms for the tester's supervisor/finance lens. Neither person
participated in this run, and no financial or statutory sign-off is claimed.

The run is **not** a user study, an accessibility/performance certification, a legal/tax
policy approval, a full Go regression, a production test or real vendor/hardware
acceptance. No bug was repaired in this audit. In particular, a saved form is only a
partial pass until numbering, readback, lineage, stock/GL effects, permission and
negative paths are independently checked.

## What an organization actually managed through the UI

| Activity and role | UI outcome / readback | Boundary still open |
|---|---|---|
| Admin establishes entity, two departments, cost center, DC, store, vendor, customer, raw material and zone | Ten masters saved with HTTP 200, then all ten found in a fresh list and Edit modal. Their generated IDs were retained; two friendly Location/Zone labels correctly resolved to stored IDs in select fields. | Tax/HSN lookup emitted a 404 during Item entry though Item saved; duplicate, field-validation, active/inactive and sequence-concurrency cases untested. |
| Admin creates DC bin | Typing the visible Zone name returned HTTP 422, “Selected Zone does not exist or is inactive.” Typing its generated `Zone/HQ/2026/000001` ID saved the Bin. | The form expects a technical identifier without a reliable name-to-ID path; no bin-level stock putaway proved. |
| Requester creates requisition for 100 units | Auto number `PR/HO/26-27/000001` saved and reappeared on fresh UI readback with description, department, quantity and estimate. | RFQ/vendor competition and requisition-to-PO lineage not tested. |
| Buyer creates PO for 100 at 10, approver reviews | Vendor and location name pickers worked. Item suggestion was rendered outside the clickable viewport/under the environment banner; typing the generated Item ID manually allowed PO `PO/HO/26-27/000001` at 1,180 including GST. Admin could submit but could not approve own PO (expected maker-checker). Scoped manager at DC approved it after assignment. | Item-picker keyboard/click recovery, empty supplier tax geography, policy-appropriate error wording and dual-review rules need dedicated tests. The maker-checker rejection returned generic “Correct the field format,” which does not explain separation of duties. |
| Receiver posts partial PO receipt | Loaded 100-unit line had no editable quantity cells; removing it and re-entering the technical Item ID allowed 80 received, 5 rejected and 75 accepted. `GRN/HO/26-27/000001` posted. Inventory showed physical on-hand 80 and available 75 at DC. | Confirmation said “80 unit(s) will be added to stock” without distinguishing accepted 75 from rejected 5; rejected disposition, PO open qty, bin/lot and stock ledger reconciliation still need proof. |
| AP enters supplier invoice and matches | Draft invoice `SUP-ORG-0001` saved with Vendor, PO, GRN and 885 amount. Match then asked for the same PO/GRN identifiers again. It returned HTTP 200 with `matched:false`, `MismatchHold`. | This is a genuine source-to-pay blocker for the fixture: `Match3Way` compares the PO's tax-inclusive `total_amount` (1,180) with a GRN quantity × untaxed PO line rate (80 × 10 = 800), and also compares the partial invoice with the full PO. Even an economically correct partial bill cannot satisfy both within 2%; AP posting/payment and GL remain unverified. Tax and partial-bill policy must be made explicit during repair. |
| Store/DC operator creates transfer | Name search displayed locations; Draft `TO/HO/26-27/000001` saved with names and one line. Clicking its visible **Mark Approved** action sent a request that returned HTTP 405 and left it Draft. | Dispatch, receipt, transit stock and maker-checker acceptance are blocked. The current UI code directly edits status and explicitly says no TransferOrder approval rule exists; that is a control/design gap even after the 405 is repaired. |
| Online desk creates one manual order, retries same reference | `ORG-ONLINE-001` created one SalesOrder, `SO-1791171035091498800-kzkmgizy`, status Reserved and 2-unit reservation. Re-entering the same reference returned the *same* ID and one list row: idempotency passed for this case. | The duplicate response still says “Order created.” The order detail has no fulfillment tasks; Fulfillment screen remains empty and says tasks appear once an order is released, but a Reserved order offers no obvious release-for-picking action. Shipment, notification delivery, invoice and refund are not proved. |
| Inventory reader checks after reservation | Screen showed on-hand 80, available 75, reserved 2. | Subtitle says available excludes reserved stock, yet displayed available did not fall after the 2-unit reservation. Determine whether it is a calculation defect or mislabeled metric; expose accepted/rejected/allocatable quantities clearly. |

The browser harness initially timed out waiting for a modal to become CSS `hidden`
after several successful saves; it actually lost the `open` class and the records
persisted. This was a harness assertion mistake, **not** an ERP defect. Likewise,
the first transfer-approval probe blocked the write in its own request allowlist;
the permitted rerun returned the real HTTP 405. These runs are not counted twice.

## Release-blocking findings to repair after the build phase

1. **FA-20261005-01 — AP three-way math and partial billing.** Reproduce with the
   exact PO/GRN/invoice above; make tax basis and accepted-vs-received quantity
   consistent, preserve mismatch holds for real overbill, and prove matched AP/GL.
2. **FA-20261005-02 — transfer approval dead end/control.** The visible action
   returns 405; define the approval rule and valid transition, then prove one
   authorized approval and one denied maker/role, dispatch and partial receipt.
3. **FA-20261005-03 — OMS reserved-to-fulfillment dead end.** Trace the actual
   status transition, provide the operator's next action, and prove one pick,
   shipment, notification and invoice from the same SalesOrder without duplicate
   effects on retry.
4. **FA-20261005-04 — identity pickers and bin prerequisite.** Every Location,
   Zone, Item and warehouse field should accept a human label yet persist the
   canonical identifier; never require users to memorize `Location/HQ/...` or
   `Item/HQ/...`. Reproduce the Bin 422 and PO Item overlay interception.
5. **FA-20261005-05 — stock semantics.** Show accepted, rejected/quarantined,
   reserved and truly free-to-promise separately; reconcile the GRN confirmation
   and inventory caption with the stock ledger and concurrent reservation.
6. **FA-20261005-06 — actionable outcomes.** SoD rejection needs a maker-checker
   message, invoice match needs comparison details and a correction route, and
   idempotent OMS retry must say “existing order” rather than “created.”

The order above is risk-based: a valid payable, controlled stock movement and
order fulfillment are more important than cosmetic wording. None is marked fixed.

## Every-module disposition after this pass

`UI sample` is not a functional pass. The linked plan has a normal case,
exception case and Done bar for every module; this column states today's
**additional evidence only**.

| Module owner | 2026-10-05 additional UI evidence | Next proof needed |
|---|---|---|
| Core | Entity, departments, cost center and locations saved/read back | Series configuration, scoped uniqueness, permissions |
| Master data | Vendor, Customer, Item, Zone and Bin saved/read back; Bin name failed | All master classes, variants/UOM/tax, duplicate prevention |
| Procurement | PR, approved PO, partial GRN and draft supplier bill exercised | RFQ, source-linked conversion, open qty and return |
| RFQ | No submitted quote | Full invite/compare/select/convert workflow |
| Sales | OMS-created SalesOrder only | Credit sale, invoice, payment, return and POS close |
| Inventory | GRN balance and OMS reservation read | Ledger reconciliation, count/hold/transfer and ATP |
| WMS | Bin setup attempted; empty fulfillment observed | Putaway/bin movement, pick/pack/load and scanner paths |
| OMS | Manual order and same-reference retry; detail inspected | Release, task, shipment, notification, invoice/refund |
| Finance | Draft AP bill and mismatch action | Correct match, payment, GL, AR, bank and close |
| PIM | Workbench and tab affordances viewed | Product-family-to-channel publish journey |
| Manufacturing | Empty BOM/order composer viewed | Save BOM, issue material, output and cost reconciliation |
| Quality | No submitted inspection | Source/lot disposition and nonconformance |
| HR | Attendance/leave/payroll tabs viewed | Employee, leave/approval, payroll and privacy |
| Expenses | Empty draft composer viewed | Claim/receipt/approval/reimbursement |
| Assets | Empty asset composer viewed | Acquisition, custody, depreciation and GL |
| CRM/loyalty | Not operated in this pass | Campaign/offer, redemption and return effects |
| Service | Not operated in this pass | Ticket-to-resolution with access/history |
| Stickers | Not operated in this pass | Source/line print, identity and reprint audit |
| Reports | Catalog/parameter affordances viewed | Populated totals, drill/export/role scope |
| Integrations | OMS connector empty state viewed | Local intake/retry plus separate live-provider acceptance |

The remaining 199-type classification, 56-route populated journeys, role matrix,
independent operator/supervisor/Account Head/CA reviews, load/security review and month-end
reconciliation are **open**. No module is accepted from this pass alone. Continue
the [phased operational plan](../../product/erp-functional-verification-plan-2026-10-04.md)
after the build work, starting with the six findings above. For each repaired
finding, rerun from a fresh UI context and retain fixture, role, response, readback,
stock/GL delta and negative-path evidence before changing its checklist box.
