---
doc_id: DOC-0E98B31FD2
title: Business UAT — run sheet and closure log
type: template
status: draft
owner: qa-owner
approvers: [documentation-maintainer, qa-owner]
audience: [maintainers, qa-owner]
applies_to: source documentation; scoped release acceptance required
authority: proposed-policy
confidentiality: internal
last_verified: 2026-09-09
review_by: 2026-10-09
supersedes: none
superseded_by: none
verification_scope: metadata and lifecycle classification; domain acceptance pending
---

> Reusable draft template. Fill in the customer, release, roles and evidence, then obtain the accountable owner’s approval before operational use. Existing check marks or examples are not acceptance of a new deployment.

# Business UAT — run sheet and closure log

**Status: DRAFT, ready to run once real business users are available.** Written
2026-08-06 against checklist item 26.11.5.

This is the *process wrapper* around the existing
[`docs/guides/uat-checklist.md`](../guides/uat-checklist.md) (22 sections of
screen-by-screen test cases). That file says **what to click**. This file says
**who runs it, in what order, what counts as a pass, and how it gets signed off**
— which is what 26.11.5 actually asks for and what the checklist alone can't
provide.

Do not duplicate test cases here. If a case is missing, add it to
`uat-checklist.md` where it belongs.

---

## 1. What makes this "business" UAT

The screen-by-screen checklist has been walked by AI sessions repeatedly. That
proves screens function. It does **not** prove the system supports how the
business actually works — which is the only question UAT exists to answer.

So this run sheet is organised by **business process end-to-end**, cutting
across screens, and must be executed by **people who do the job**, not by
whoever built it. A tester who knows the intended path will unconsciously avoid
the ways a real user goes wrong.

---

## 2. Participants

| Role | Who | Responsibility |
|---|---|---|
| UAT coordinator | _TBD_ | Schedules sessions, keeps the defect log, chases sign-off |
| Store/cashier user | _TBD_ | Scenarios A, F |
| Warehouse/stock user | _TBD_ | Scenarios B, C |
| Buyer/procurement user | _TBD_ | Scenario C |
| Finance user | _TBD_ | Scenarios D, G |
| Manager/approver | _TBD_ | Scenario E, plus approvals throughout |
| Warehouse user at a second location | _TBD_ | Scenario H (receiving end) |
| Production planner / shop floor / quality | _TBD_ | Scenario I |
| HR and payroll user | _TBD_ | Scenario J |
| Merchandiser / catalog user | _TBD_ | Scenario K |
| Business owner | _TBD_ | Final sign-off (§6) |

One person may hold several roles — but **the approver must be a different
person from the maker** for Scenario E to test anything real, since
maker-checker self-approval is blocked by design.

---

## 3. Prerequisites

Complete `uat-checklist.md` §0 first, plus:

1. A dedicated **UAT tenant** with its own data — not the pilot tenant, and not
   `tenant_default`. Provision via Settings → Tenant Entitlements (26.1.4).
2. One user account **per participant**, each with only the role they actually
   hold in the business. Shared logins invalidate the approval scenarios.
3. Realistic master data: at least 20 real SKUs with real prices/HSN/GST, 3
   vendors, 2 locations. Toy data ("Test Item 1") hides real problems — HSN
   validation, GST rounding, and name-length layout issues only surface with
   real values.
4. A known-good backup taken immediately before UAT starts, so the tenant can be
   reset between full runs.
5. **Print/hardware check** if the pilot uses them: receipt printer, barcode
   scanner, payment terminal. These are the most common source of "works in the
   demo, fails in the shop".

---

## 4. Scenarios — run in this order

Order matters: each builds on the previous one's data, mirroring a real
operating cycle. Record every scenario in the log in §5.

### Scenario A — A day of retail trade *(store/cashier)*
Open POS session → 5 sales across cash/card/UPI → one discounted sale requiring
approval → one return → close session and reconcile cash.
**Pass:** cash variance computed correctly at close; every sale on the GL with
the right payment-mode account; the discount actually blocked until approved.

### Scenario B — Receiving stock *(warehouse)*
Raise a purchase requisition → convert to PO → approve → receive against it via
GRN, including one short-received line and one rejected line → putaway to bins.
**Pass:** stock rises by exactly the accepted quantity, not the ordered one;
the rejected line is visible with its reason; bins reflect the putaway.

### Scenario C — Procure-to-pay *(buyer + finance)*
Vendor invoice against Scenario B's GRN → deliberately mismatch it → observe the
hold → override with a reason → route through approval → pay.
**Pass:** the mismatch is caught without anyone looking for it; the override is
attributed to a named person with their stated reason.

### Scenario D — Period close *(finance)*
Trial balance → confirm it balances → close the period → attempt a backdated
posting into the closed period → approve the backdated exception.
**Pass:** trial balance balances; the backdated posting is refused with a clear
message (FIN-0260), not a raw error, and only proceeds via explicit approval.

### Scenario E — Approvals and separation of duties *(maker + approver)*
Maker submits a document above the approval threshold → maker attempts to
approve their own → approver approves → maker edits the approved document.
**Pass:** self-approval is refused; the edit resets it to Pending Approval
rather than silently keeping the approval.

### Scenario F — Stock accuracy *(warehouse)*
Cycle count with a deliberate variance → import counts by CSV → reconcile →
approve the variance adjustment.
**Pass:** the CSV imports the quantities *as counted* (this is where the Stage
20.20 parser bug lived — worth watching); the adjustment posts once, correctly
signed.

### Scenarios H–K — the four further reference journeys (73.J.1)

Added from the transaction-gap PDF's journey set (J3, J6, J8, J9) so that
transfer, manufacturing, hire-to-pay and product-to-channel are accepted the
same way as A–F. Run them after F and before G. **Evidence, for each step:**
the actor and role; the screen (or API) used; the source document ids and the
numbers the system generated; quantity and money before and after; the stock,
GL, points or tender effect; every approval; any retry; every denied path that
was tried; and the final reconciliation. A screenshot of a successful screen is
not evidence on its own.

### Scenario H — Inventory transfer (J3) *(warehouse, two locations)*
Raise a transfer for a batch-tracked item → pick it FEFO at the source →
dispatch (stock in transit) → receive part of it at the destination → record
the shortfall or damage with a reason → count both locations.
**Pass:** source on-hand falls at dispatch, destination rises only by what was
received, the in-transit balance closes to the discrepancy line, the same lot
arrives that left, and the count shows no unexplained difference.

### Scenario I — Manufacture (J6) *(planner + shop floor + quality + finance)*
Run MRP → create a production order from its proposal → issue materials →
confirm part of the output → record scrap and a rework → pass QC → receive
finished goods → close the order and read its variance.
**Pass:** materials leave stock once, finished goods enter once, scrap and
rework are visible with their cost, QC gates the receipt, and the variance on
the order agrees with what the GL shows for it.

### Scenario J — Hire to pay (J8) *(HR + manager + payroll + finance)*
Onboard an employee → record attendance and an approved leave → run payroll →
generate the bank file → post to the GL → offboard the employee.
**Pass:** gross, deductions and net reconcile from payslip to bank file to GL;
the leave reduced the right balance; and after offboarding the employee's
login no longer works.

### Scenario K — Product to channel (J9) *(merchandiser + approver + order desk)*
Create a product family → a variant → its media → get the content approved →
print its label → set price and tax → publish to a channel → re-import the
channel's listing → take an order for it.
**Pass:** the order line resolves to the same SKU and barcode that was
published and labelled, the price and tax on the order match what was set,
and nothing was published before approval.

### Scenario G — The awkward cases *(any user — do not skip)*
Deliberately misuse the system:
- Enter a negative quantity, and a quantity of zero.
- Enter a 500-character product name; enter a name with an apostrophe.
- Enter non-Latin text (Hindi/Tamil) in a name field.
- Submit a form twice quickly (double-click Save).
- Let a session sit idle past the timeout, then act.
- Lose network mid-save (turn off Wi-Fi), then retry.

**Pass:** every case produces a *comprehensible message*, and no case produces a
duplicate document, a raw database error, or a blank screen. This scenario finds
more real defects than A–F combined, and it is the one testers skip.

---

## 5. Defect log

Every issue, including cosmetic. Severity uses
[`incident-runbook.md`](incident-runbook.md) §1.

| ID | Date | Scenario | Raised by | Severity | Description | Expected vs actual | Decision | Fixed in | Retested |
|---|---|---|---|---|---|---|---|---|---|
| U-001 | | | | | | | | | |

**Decision** is one of: *Fix before go-live* / *Fix during hypercare* /
*Accepted as-is* / *Not a defect (training)*. Every row needs one before §6 can
be signed — that requirement is what makes this a closure log rather than a
list of complaints.

---

## 6. Exit criteria and sign-off

UAT passes when:

1. All seven scenarios have been executed end-to-end by a real business user.
2. Zero open P0 defects; zero P1 defects marked *Fix before go-live* still open.
3. Every defect-log row carries a decision.
4. The business owner confirms the system supports the process **as the business
   actually runs it** — not merely that the software did what it was told.

| Role | Name | Verdict (Pass / Pass with conditions / Fail) | Date | Signature |
|---|---|---|---|---|
| UAT coordinator | | | | |
| Store/cashier user | | | | |
| Warehouse user | | | | |
| Finance user | | | | |
| Business owner | | | | |

**Conditions, if "Pass with conditions":**

_(list the specific defects accepted into hypercare, by ID)_

---

## 7. After sign-off

1. Attach this completed sheet to the go-live decision record.
2. Carry every *Fix during hypercare* defect into the hypercare defect log
   ([`hypercare-plan.md`](hypercare-plan.md) §5) — do not let them fall between
   the two documents. That gap is the usual way agreed fixes get lost.
3. Reset or archive the UAT tenant so its data can never be mistaken for real.

---

## 8. BRD acceptance cases (74.U.1) - the 68 cases of the Transaction Modules BRD section 10

Source: *Enterprise Transaction Modules Master BRD*, section 10 (UAT / SIT test
matrix). The step and expected-result columns are the BRD's own wording; the
other columns say what to check. Money is in rupees (Rs). Case ids map to the
module FR ids in the BRD.

**A case passes only with all seven pieces of evidence:** the UI action; a
readback of the saved document; the stock, custody and GL reconciliation
(figures from the database or the stock ledger, not from the screen that made
the change); the print output where the case has one; a denied-role attempt
(the same action by a user who lacks the permission, refused, with nothing
saved); and a replay (the same action repeated after a timeout or double
click, creating nothing twice). Use an isolated test company and the provider
sandbox. A screenshot of the success screen is not evidence on its own.

**Status is against the code, not assumed.** *Ready*: the feature exists and a
test or a scratch-server rehearsal exercised it. *Partial*: it exists but the
gap is named. *Waits on*: it needs another item, a device or a provider
account. Ready means the case can be run now; it does not mean it has passed.
The Result column is blank for the business testers.

Companion checks: [`uat-checklist.md`](../guides/uat-checklist.md) section 27
(screens of Stage 130.15 / 130.16) and the scenarios in section 4 above. These
settings ship **off**, so a case that depends on one needs it switched on
first: `reasons.codes_required`, `print.reprint_control`,
`print.tax_document_needs_gstin`, `procurement.po_terms_required`,
`print.template_approval`.

Columns: **Id | Step | Expected result | Reconciliation (S stock, C custody, G GL) | Print | Denied role | Replay | Status | Result**.

### 8.1 Delivery challan (UAT-DC)

| Id | Step | Expected result | Reconciliation | Print | Denied role | Replay | Status | Result |
|---|---|---|---|---|---|---|---|---|
| UAT-DC-01 | DC with 3 separate SKUs and returnable purpose | One DC number, 3 lines, all Rule 55 fields, no sale/AR | S: on hand unchanged, available down by the quantity. C: with-party up by it. G: no posting | Challan, original copy | Cashier cannot create | Post the same body twice and check how many challans exist (the create has no command key; see decision 1 in 8.13) | Ready (rehearsed) | |
| UAT-DC-02 | Issue all three challan copies | Correct consignee/transporter/consignor labels with same commercial document | None (print only) | Original, duplicate, triplicate: same number and totals, three labels | User without print right refused | Print the set twice: copies are evidence rows, not new challans | Ready (TestStage130PrintIssue, TestStage130StandardPrints) | |
| UAT-DC-03 | Dispatch to external processor and part-return | Source and processor-owned stock totals reconcile through 2 returns | S: on hand unchanged, available back by each return. C: with-party falls by each return and stays at the remainder | Challan after each return shows the balance | Cashier cannot receive a return | Same return key twice: one receipt | Ready (rehearsed with one return; TestStage130ChallanReturnRace) | |
| UAT-DC-04 | Try return qty > remaining | Reject without any inventory/GL event | S and C unchanged; G no rows | None | Repeat as Cashier for the permission refusal | Repeat the over-return: same refusal, nothing moves | Ready (rehearsed) | |
| UAT-DC-05 | Try to edit issued/physically dispatched document | Reject edit; show linked reversal/return action | Document body and S/C unchanged | n/a | Cashier edit refused | Repeat the edit: same refusal | Ready (a PUT or a save of a dispatched challan answers 422 "can no longer be edited - receive a return or cancel the challan"; 162.3, TestLockedRecordSaysWhyOnGenericPutAndEdit) | |
| UAT-DC-06 | Jobwork inter-State small-value movement | EWB decision uses jobwork exception rule; evidence retained | C unchanged by the decision; evidence row on the challan | Challan shows the EWB state | Cashier cannot request an EWB | Decide twice: one evidence row | Ready (fake provider; TestStage130EWayBill) | |

### 8.2 Customer advance receipt (UAT-ADV)

| Id | Step | Expected result | Reconciliation | Print | Denied role | Replay | Status | Result |
|---|---|---|---|---|---|---|---|---|
| UAT-ADV-01 | Collect Rs 2,000, print advance receipt | No Sales Invoice number or sales revenue; correct liability | G: Dr cash or bank, Cr customer-advance liability Rs 2,000, no revenue line | Advance receipt (no invoice number) | Role without the advance right refused | Same receipt twice: one advance | Ready (TestCustomerAdvanceLifecycle) | |
| UAT-ADV-02 | Split tender cash + UPI advance | Exactly one collection per confirmed tender; POS drawer reconciles | G: one line per tender. Drawer: only the cash part in the till count | Receipt shows both tenders | As above | Re-confirm the same UPI tender: one collection | Partial (the lifecycle test covers one advance; the drawer tie-out of a split tender has no test) | |
| UAT-ADV-03 | Apply Rs 750 and Rs 1,000 to separate bills | Remaining Rs 250; invoice retains SI number; no duplicate posting | G: liability falls by 750 then 1,000, balance 250 | Both invoices keep their own SI numbers | Role without the apply right refused | Apply the same amount twice to one bill: once | Ready (TestCustomerAdvanceLifecycle) | |
| UAT-ADV-04 | Two tills apply same remaining Rs 250 | At most one valid debit of balance; loser receives conflict | G: liability debited once. Balance 0, never negative | n/a | n/a | Both tills at once: one wins, one gets the conflict message | Partial (no concurrency test found for the advance apply; confirm the balance guard holds under two simultaneous calls) | |
| UAT-ADV-05 | Refund residual Rs 250 | Voucher/approval and bank/cash liability reduction once | G: Dr liability, Cr cash or bank, once | Refund voucher | Maker cannot approve own refund | Refund twice: once | Ready (TestCustomerAdvanceLifecycle) | |
| UAT-ADV-06 | Cross-entity advance use | Blocked unless explicit approved intercompany design | None: nothing may move | n/a | n/a | Repeat: same block | Partial (no legal-entity check found in the advance code; confirm with the tax owner whether the block is needed on day one) | |
| UAT-ADV-07 | Advance payment captured but ERP times out | Payment provider checked/reconciled; no duplicate collection | G: one collection per provider transaction | Receipt printed once | n/a | Retry after the timeout: the provider status is read first | Waits on a live payment-provider sandbox (20.30 / 20.31 credentials) | |

### 8.3 Internal material issue and receipt (UAT-MAT)

| Id | Step | Expected result | Reconciliation | Print | Denied role | Replay | Status | Result |
|---|---|---|---|---|---|---|---|---|
| UAT-MAT-01 | Issue 10 valuated packs to cost centre | Stock -10, expense debited with correct cost centre | S: on hand -10. G: Dr expense 5180 (cost centre and department), Cr inventory 1200, at cost | Material issue slip | A role with no create right on the material issue (the seeded Cashier role has it; see decision 2 in 8.13) | Issue again: still once | Ready (rehearsed; the denied-role leg was not met by Cashier) | |
| UAT-MAT-02 | Return 4 unused packs | Stock +4, net consumption 6; financial reversal/clearing approved | S: +4. G: Dr 1200, Cr 5180 for the 4 at the issue cost; net expense is 6 x cost | Material return slip | As above | Return 7 when 6 can come back: refused | Ready (rehearsed) | |
| UAT-MAT-03 | Issue nonvaluated tracked accessories | Qty reduces but no second expense/GL duplicate | S: down. G: no second expense line | Slip | As above | Issue twice: once | Ready (TestStage130MaterialIssue; not rehearsed) | |
| UAT-MAT-04 | Attempt MI on nonstock item | No phantom on-hand; appropriate expense path | S: unchanged | n/a | n/a | Repeat: same refusal | Ready (the engine says "issue stock items only"; not rehearsed) | |
| UAT-MAT-05 | Manual MI duplicates packaging backflush | System refuses duplicate source event/consumption | S and G: only the backflush | n/a | n/a | Repeat: same refusal | Ready (the engine refuses backflushed items; not rehearsed) | |

### 8.4 Inventory adjustment (UAT-ADJ)

| Id | Step | Expected result | Reconciliation | Print | Denied role | Replay | Status | Result |
|---|---|---|---|---|---|---|---|---|
| UAT-ADJ-01 | Post gain of +3 items at Rs 400 cost | Inventory +3 and Dr Inventory/Cr Gain Rs 1,200 | S +3. G: Dr 1200 / Cr 5190 = 120000 paise | Adjustment slip | Cashier refused (403) | Same Idempotency-Key: same document | Ready (rehearsed) | |
| UAT-ADJ-02 | Post loss of -2 items at Rs 400 cost | Inventory -2 and Dr Loss/Cr Inventory Rs 800 | S -2. G: Dr 5190 / Cr 1200 = 80000 paise | Slip | Cashier refused | Same key: same document | Ready (rehearsed) | |
| UAT-ADJ-03 | Wrong serial or insufficient stock adjustment | Reject atomically; no partial stock/GL | S and G unchanged; no document saved | n/a | n/a | Repeat: same refusal | Ready (rehearsed with insufficient stock; a wrong serial is covered by TestStage130StockAdjustmentRest) | |
| UAT-ADJ-04 | Two different makers race on same available qty | Stale revision conflict; latest qty recomputed | S: posted once, to the recomputed quantity | n/a | Maker cannot approve own | Post the stale one twice | Ready (TestStage130StockAdjustmentRest, stale snapshot) | |
| UAT-ADJ-05 | Post and reverse original adjustment | Original intact, linked reverse offsets stock and GL | S back. G: the mirror pair; the original rows unchanged | Reversal slip | Cashier refused | Reverse twice: the second is refused, naming the first reversal | Ready (rehearsed; the reverse needs a reason) | |
| UAT-ADJ-06 | Inactive reason or unauthorized approval | No posting | S and G unchanged | n/a | Unauthorised approver refused | Repeat: same | Ready (TestStage130StockAdjustmentRest; not rehearsed) | |

### 8.5 Job work outward and return (UAT-JW)

| Id | Step | Expected result | Reconciliation | Print | Denied role | Replay | Status | Result |
|---|---|---|---|---|---|---|---|---|
| UAT-JW-01 | Send 3 SKUs with lot/serial and weights | One order/multiple lines; vendor-held qty and weight correct | C: vendor-held qty and weight per line. G: none on dispatch | Job work challan | Cashier cannot create | Dispatch twice: once | Ready (TestStage130JobWork) | |
| UAT-JW-02 | Receive 5 of 12 then 4 of 12 | Remaining 3; no double stock or premature Close | S: +5 then +4. C: 12, 7, 3. Order stays open | Receipt note | Cashier cannot receive | Same receipt key twice | Ready (TestStage130JobWork) | |
| UAT-JW-03 | Receive scrap and accepted pieces | Separate locations/valuation, weight variance explained | S: accepted and scrap in separate locations. G: scrap valued apart | Receipt note | As above | As above | Ready (TestStage130JobWork) | |
| UAT-JW-04 | Output SKU transformed from source input | Approved item link/yield with retained genealogy | S: output item up, source lot kept beside it | Receipt note | As above | As above | Ready (job_work.go documents the yield and genealogy; TestStage130JobWork) | |
| UAT-JW-05 | Vendor processing bill allocated to returned items | WIP/FG cost increases properly, AP matches vendor invoice | G: Dr item cost, Cr AP = the vendor bill. Item cost up by the allocation | Vendor bill | Role without bill right refused | Post the charge twice: once | Ready (PostJobWorkCharge; TestStage130JobWork) | |
| UAT-JW-06 | Due date exceeded and loss above tolerance | Exception + independent approval; report includes vendor/line | G: nothing until approved | Exception on the order | Maker cannot approve own | Approve twice: once | Partial (the tolerance setting and the yield report exist; confirm an over-tolerance loss waits for a different approver and is not only a flag) | |

### 8.6 Store credit / credit voucher (UAT-SC)

| Id | Step | Expected result | Reconciliation | Print | Denied role | Replay | Status | Result |
|---|---|---|---|---|---|---|---|---|
| UAT-SC-01 | Return bill paid half with credit; choose credit refund | No cash over-refund; issued liability equals approved split | G: cash refund + credit liability = the return value | Credit voucher | Role without refund right refused | Refund twice: once | Ready (TestStage130StoreCredit) | |
| UAT-SC-02 | Rs 800 credit issued, Rs 300 redeemed | Remaining Rs 500 and GL liability Rs 500 | G: liability 800 - 300 = 500 | Voucher balance | As above | Redeem the same sale twice: once | Ready (TestStage130StoreCredit) | |
| UAT-SC-03 | Two concurrent POS redemptions | Available credit never negative, exactly-once | Balance >= 0; one redemption posted | n/a | n/a | Both tills at once | Partial (no concurrent-redemption test found; the challan return has one, the credit voucher does not) | |
| UAT-SC-04 | Refund new sale paid with store credit | Restoration follows original-tender policy, not default cash | G: the credit liability back up, no cash | Voucher | As above | Refund twice: once | Partial (TestStage130StoreCredit covers it in part; confirm with a sale paid wholly by credit) | |
| UAT-SC-05 | Distinguish GST credit note and credit voucher | Separate source/number/tax/accounting and print formats | G: the credit note reverses revenue and tax; the voucher is a liability only | Both formats | n/a | n/a | Ready (TestStage130StoreCredit, TestStage130StandardPrints) | |
| UAT-SC-06 | Inactive/expired token validation | Denied with meaningful reason and no cash/GL activity | None | n/a | n/a | Repeat: same refusal | Ready (TestStage130StoreCredit) | |

### 8.7 GST e-invoice (IRN/QR) and e-way bill (UAT-GST)

Everything here runs against the **fake provider** today. The live IRP and
e-way bill sandbox wait on 75.2.0 / 75.2.11 / 75.2.12 and the 20.30 / 20.31
credentials; a case that needs the real provider says so.

| Id | Step | Expected result | Reconciliation | Print | Denied role | Replay | Status | Result |
|---|---|---|---|---|---|---|---|---|
| UAT-GST-01 | Eligible B2B SI submit to IRP sandbox | IRN/Ack/signed QR stored; generated exactly once | G unchanged by the IRN. Invoice holds one IRN | Invoice with IRN and QR | Role without e-invoice right refused | Submit twice: one IRN | Ready on the fake provider; live sandbox waits on 75.2.0 / 75.2.11 / 20.30 | |
| UAT-GST-02 | B2C sale not eligible for IRN | NotRequired state and no provider call | Provider call count 0 | Plain invoice | n/a | Repeat: still no call | Ready (fake provider; TestStage752EInvoice) | |
| UAT-GST-03 | IRP receives request but response times out | Look up provider status before retry; no dual economic posting | One IRN, one posting | Invoice | n/a | Retry after the timeout: the status is read first | Ready on the fake provider (TestStage752EInvoice); live behaviour waits on 75.2.11 | |
| UAT-GST-04 | IRN rejection for HSN or total mismatch | Pending queue resolves to Rejected with precise fix path | Invoice stays unposted to the IRP; the error text names the field | n/a | n/a | Resubmit after the fix: one IRN | Ready (fake provider; TestStage752EInvoice) | |
| UAT-GST-05 | Credit note original reference and e-invoice | Only eligible legal CN processed; source link correct | CN links to the original invoice and IRN | Credit note | As above | Submit twice: one | Ready (fake provider; TestStage752EInvoice) | |
| UAT-GST-06 | Eligible high-value movement triggers EWB | Correct transport metadata and EWB ref stored | EWB ref on the challan or invoice; C and S unchanged by it | Document with EWB | Cashier refused | Request twice: one EWB | Ready (fake provider; TestStage130EWayBill) | |
| UAT-GST-07 | Inter-State jobwork below Rs 50k | Provider evaluation respects statutory special condition | Evidence row retained | Challan | As above | As above | Ready (TestStage130EWayBill; threshold is a setting) | |
| UAT-GST-08 | EWB exempt item/mixed goods case | Rule version and exemption evidence retained, tax owner approves | Evidence with the rule version | n/a | Non-tax-owner approval refused | Approve twice: once | Partial (exemption evidence is kept; confirm the tax-owner approval step for mixed goods exists and is not a free-text note) | |
| UAT-GST-09 | Cancellation outside provider window | No false success; controlled correction path exposed | Document still shows the IRN; no reversal posted | n/a | n/a | Repeat: same refusal | Partial (the window check is covered on the fake provider; the correction path wording needs a check by the tax owner) | |
| UAT-GST-10 | 30-day reporting restriction for affected taxpayer | Warning/block follows current rule, configurable effective date | Late invoice is warned or blocked per the rule | Warning shown | n/a | Repeat: same | Ready (the 30-day rule is in the e-invoice worker; confirm the effective date is a setting, not a constant) | |

### 8.8 Document reprint log (UAT-PRT)

| Id | Step | Expected result | Reconciliation | Print | Denied role | Replay | Status | Result |
|---|---|---|---|---|---|---|---|---|
| UAT-PRT-01 | Print original tax invoice | Job log stores user/version/checksum/copy type | One log row: user, template version, checksum, copy type | Original invoice | User without print right refused | Print again: a new log row, not a new invoice | Ready (TestStage130PrintIssue) | |
| UAT-PRT-02 | Reprint original with reason | New evidence event; source invoice unchanged | Invoice unchanged; one new event with the reason | Reprint marked | As above (needs `print.reprint_control` on) | Reprint twice: two events | Ready (TestStage130PrintIssue) | |
| UAT-PRT-03 | Challan original/duplicate/triplicate in same run | 3 copy labels recognized, not counted as unapproved reprints | Three rows, none counted as reprints | The three copies | As above | Run the set again: three more rows, still copies | Partial (the copy labels exist; confirm the same-run copies are not counted as reprints in the audit report) | |
| UAT-PRT-04 | Printer timeout after accepting job | Unknown rather than Printed; retry does not repost sale | Sale posted once; job state Unknown | Unknown until the printer confirms | n/a | Retry the print: no second sale | Waits on a real printer and the print agent (backend accepted vs printer confirmed) | |
| UAT-PRT-05 | Unauthorized user tries to reprint refund | Denied and auditable | No new print event, or a denied event recorded | Nothing printed | The case itself | Repeat: same denial | Partial (the reprint is denied; a record of the denied attempt was not found in the print code) | |

### 8.9 Central reason codes (UAT-RSN)

| Id | Step | Expected result | Reconciliation | Print | Denied role | Replay | Status | Result |
|---|---|---|---|---|---|---|---|---|
| UAT-RSN-01 | Reject 2 GRN lines with different reasons | Two line-specific structured reasons saved | Two lines, two reason codes | GRN shows both | n/a | Save twice: no extra lines | Ready (TestStage74_1_1ReasonCodes; needs `reasons.codes_required` on to be mandatory) | |
| UAT-RSN-02 | Create PR using active standardized reason | PR source line and reason preserved | Source line id and reason on the PR | n/a | n/a | Save twice: one PR | Ready (TestStage74_1_1ReasonCodes) | |
| UAT-RSN-03 | Transfer shortage with mandatory attachment | No post without proof | No stock or GL until the attachment exists | n/a | n/a | Post again without it: refused | Partial (the reason code and shortage flow exist; a mandatory attachment rule was not found in the reason code code) | |
| UAT-RSN-04 | Disable reason after historical use | Old reason still readable, new use denied | Old documents unchanged | Old print still shows the reason | User cannot reuse it | Use again: refused | Ready (TestStage74_1_1ReasonCodes) | |
| UAT-RSN-05 | Manipulate reason_code through API | 403/422 or appropriate error; no bypass | Nothing saved | n/a | The case itself | Repeat: same error | Ready (resolved at the one reason-code choke point) | |

### 8.10 PO and GRN commercial / logistics extensions (UAT-POGRN)

| Id | Step | Expected result | Reconciliation | Print | Denied role | Replay | Status | Result |
|---|---|---|---|---|---|---|---|---|
| UAT-POGRN-01 | Create PO with payment Net 30 and ETA | Approved snapshot and vendor print contain both | Terms and ETA in the approved snapshot | Vendor PO | Role without PO right refused | Save twice: one PO | Ready (TestStage130POTermsGRNLogistics) | |
| UAT-POGRN-02 | Create GRN from two POs | Every line retains source PO ID and accepted open qty | Each line carries its PO id; open qty per PO | GRN | Role without GRN right refused | Save twice: one GRN | Ready (TestStage130POTermsGRNLogistics) | |
| UAT-POGRN-03 | Receive 80 of 100, accept 75, reject 5 | Accepted stock +75, rejected segregated, ordered open correctly | S: +75 free, 5 in the rejected pool. PO open 20 | GRN | As above | Post the GRN twice: once | Ready (Scenario B plus TestStage130POTermsGRNLogistics) | |
| UAT-POGRN-04 | Fill challan/vehicle/LR/package refs | Persist/reload/print with no data loss | Fields identical after reload | GRN shows them | n/a | Save twice: same values | Ready (TestStage130POTermsGRNLogistics) | |
| UAT-POGRN-05 | Try duplicate vendor tax invoice | Detect duplicate within governed scope | Second invoice refused or flagged | n/a | n/a | Repeat: same | Ready (vendor_invoice_test duplicate hold) | |
| UAT-POGRN-06 | Edit an approved PO payment term | Reapproval required and audit captures before/after | PO back to Pending Approval; audit row has old and new term | Vendor PO shows the new term only once re-approved | Role without edit right refused | Edit twice: two audit rows | Partial (terms are versioned; confirm an edit of an Approved PO sends it back, the re-approval rule keys on the status change) | |

### 8.11 Print profiles (UAT-PF)

| Id | Step | Expected result | Reconciliation | Print | Denied role | Replay | Status | Result |
|---|---|---|---|---|---|---|---|---|
| UAT-PF-01 | Preview company A/B legal identities | Correct GSTIN/logo/location, no cross-entity exposure | None | Preview per entity | A user of entity A cannot preview B | Preview twice: no change | Ready (TestStage130PrintProfilesRest) | |
| UAT-PF-02 | Print B2B IGST invoice | Place of supply, tax, original IRN signed QR correct | Printed tax = posted tax | Invoice | n/a | Reprint: the same values | Ready on the fake provider (live IRN waits on 75.2.x) | |
| UAT-PF-03 | Print on thermal 80mm / A4 | No clip/overlap of tax, tender and copy label data | None | Both widths | n/a | Print twice: same layout | Waits on the physical printers (a visual check) | |
| UAT-PF-04 | Change profile after document issue | Original commercial/tax snapshot preserved | Old invoice reprints with the old values | Old invoice unchanged | Role without template right refused | Change twice: old invoice still the same | Ready (print snapshots, TestStage130PrintIssue) | |
| UAT-PF-05 | Missing required GSTIN before issue | Actionable validation, not silent blank invoice | Nothing issued | n/a | n/a | Repeat: same message | Ready (needs `print.tax_document_needs_gstin` on) | |
| UAT-PF-06 | Print vendor PO and GRN with new fields | Payment, delivery and transport fields visible | None | PO and GRN | n/a | n/a | Ready (TestStage130StandardPrints) | |

### 8.12 Scratch-server rehearsal, 2026-11-07 (not the user's UAT)

An AI session ran the cases below on a throwaway server and a scratch
database (not a client tenant) to check that the steps can be followed. It is
**not** UAT, not acceptance and not a pass: the Result column above stays
blank. Each line is what was done and seen; "denied" means the Cashier role.

| Case | What ran | Seen |
|---|---|---|
| UAT-ADJ-01 | Gain of +3 for an item at cost Rs 400 | Document ADJ/UAT-LOC/2627/0001 Posted; on hand 8 to 11; GL Dr 1200 / Cr 5190 120000 paise. Same Idempotency-Key replayed returned the same document, stock and GL unchanged. Cashier: 403, nothing saved |
| UAT-ADJ-02 | Loss of -2 | Posted; on hand 11 to 9 at the time; GL Dr 5190 / Cr 1200 80000 paise |
| UAT-ADJ-03 | Loss of -500 | 422 "only 11 free", no document, stock and GL unchanged |
| UAT-ADJ-05 | Reverse the gain | A reverse with no reason is refused ("say why"); with a reason: new document 0003 Posted, on hand back by 3, mirror GL pair; the original rows untouched. A second reverse was refused and named the first reversal. Cashier: 403 |
| UAT-DC-01 | Job work challan, 3 SKUs (4, 5, 6) | One number DC/UAT-LOC/2627/00001, Approved; dispatch gave Dispatched; on hand unchanged, available down 4, 5, 6, with-party 4, 5, 6; no GL rows. Cashier create: 403 |
| UAT-DC-03 | Return 3 of 4 of one SKU | Status Partly Returned; available 4 to 7, with-party 4 to 1. The same return key replayed: no second receipt. Cashier return: 403 |
| UAT-DC-04 | Return 9 of 4, then 2 of the 1 left | 422 "4 can come back, not 9" and "1 can come back, not 2"; nothing moved |
| UAT-DC-05 | PUT on the dispatched challan through the generic route | 405 "Method not allowed", nothing saved; no pointer to return or cancel (so Partial) |
| UAT-MAT-01 | Request then issue 10 packs to a cost centre | Stock 20 to 10; GL Dr 5180 / Cr 1200 100000 paise tagged cost centre and department. A second issue call returned Issued and moved nothing. **The Cashier was able to create and issue** (see decision 2) |
| UAT-MAT-02 | Return 4, then 7 | Stock 10 to 14; GL Dr 1200 / Cr 5180 40000 paise (net expense Rs 600); the 7 refused ("6 can come back") |

Wording fixes made after the rehearsal: the reverse needs a reason; challan
purposes are the fixed list "Job work, Repair, Exhibition, Sample,
Returnable - other, Non-returnable, Other" (case-sensitive in the API); a
challan return needs a `return_key` to be replay-safe.

### 8.13 Decisions for the owner (with a recommended answer)

1. **Replay of the other command routes.** Only ten routes use the command key;
   the challan create, job-work create and the like rely on their own state
   gates. Recommended: put a `commandKeyed` wrapper on the BRD's remaining
   create routes (see 9.1, E2E-08) so a double click cannot make two documents.
2. **Cashier can issue material.** The seeded role matrix gives Cashier create
   on the material issue, and the issue and return actions reuse that single
   right. Recommended: keep "raise a request" for Cashier, and gate issue and
   return on the store-manager level.
3. **A generic edit of a dispatched challan** answers 405 with no guidance.
   Recommended: answer the usual locked-record message that names the return
   and cancel actions.
4. **Concurrency tests** for the advance apply (ADV-04) and the credit voucher
   redemption (SC-03). Recommended: add them, copying the challan return race.

## 9. End-to-end journeys (74.U.2) - E2E-01..16

The BRD's cross-module acceptance rows (section 10.12), written as reference
journeys in the same style as scenarios A-K. Each needs the pass rule of
section 8 for every document it touches. **Start state**, **steps**, **end
state**, **reconciliation** and **status** are given; status is against the
code, as in section 8. These extend the reference journeys of 47.17.1.

### 9.1 What "the eleven command APIs" are

The BRD never lists "11 command APIs" by name. Its section 8.4 gives 21 routes
(17 of them POST) for the eleven modules of this matrix. The reading used here
is **one economic command per module**: challan dispatch, advance collect,
material issue, adjustment post, job-work dispatch, store-credit issue,
e-invoice submit, e-way bill create, reprint, reason-code use, and PO / GRN
post. Their create and posting routes are wrapped in the command key (`commandKeyed`, 36 routes across the product; 162.7): challan create, dispatch, return and cancel; material request, issue and return; job-work send, receive and charge; e-invoice and e-way bill actions; reprint request; GRN post and pay; PO send; with the adjustment, advance and gift-card routes already keyed. Store-credit issue runs inside the POS return, which has its own return key; reason-code use has no route of its own. E2E-08 is **Ready** for the keyed routes (TestBRDCommandRoutesAreAllKeyed, TestBRDCommandCreateRoutesReplayOnce). The owner should confirm the
list of eleven.

| Id | Start state | Steps | End state | Reconciliation | Status |
|---|---|---|---|---|---|
| E2E-01 | A customer, a stock item, a custom-order item | Collect an advance; raise the customization request; receive the made item; sell it as a standard sale using the advance | One sale with a normal bill number; the advance fully settled | Same customer and request id on every document; advance liability 0; stock +1 then -1; GL revenue once | Ready in part (advance, custom orders and the sale exist; the single-lineage readback across them was not run) |
| E2E-02 | Vendor, PO for 100 | Approve; GRN 80 received, 75 accepted, 5 rejected with a reason; vendor invoice; purchase return of the 5 | Open PO qty 20; AP matches the invoice | Stock +75 then the return; reject reason kept; AP, stock and tax notes agree | Ready (Scenarios B and C plus the purchase return) |
| E2E-03 | Challan with 3 lines to a processor | Dispatch; two partial returns; vendor bill | Custody qty and weight 0 at closure; cost allocated | No revenue on dispatch; AP = bill; item cost up | Ready (TestStage130JobWork and TestStage130DeliveryChallan; the full chain not run end to end) |
| E2E-04 | A POS sale | Return it; GST credit note; issue store credit; new sale paid by the credit | No double refund | Tender splits, liability and tax agree; credit balance correct | Ready in part (each leg is tested; one continuous run was not) |
| E2E-05 | Stock on hand | Manual adjustment with reason and approval; the stock ledger; trial balance | Balanced trial balance with the adjustment visible | Ledger movement = GL pair | Ready (adjustment rehearsed; ledger and TB are existing screens) |
| E2E-06 | Inter-State challan or job work | Dispatch; create EWB; print the three copies | EWB state separate from the physical dispatch and from the print | Provider state vs posting vs print log | Ready on the fake provider; live waits on 75.2.x |
| E2E-07 | A user holding two GSTINs | Switch location; create an invoice, an advance, a stock move and print | Nothing posted to the wrong entity | Per-entity tax, stock, advance and print profile | Partial (location scoping is in place; the two-GSTIN switch was not walked through) |
| E2E-08 | The eleven commands, each run once | Replay each after a simulated timeout | No second posting, liability debit or provider call | Counts before and after | Ready (see 9.1) |
| E2E-09 | A posted invoice | Change the print template; reprint the old invoice | Old values and QR unchanged | Audit shows the newer render choice | Ready (print snapshots; TestStage130PrintIssue) |
| E2E-10 | A closed period | Post an adjustment, then a refund into it | Refused with the period message, or a governed reversing action in the open period | No backdated row | Ready (period close, Scenario D; FIN-0260) |
| E2E-11 | Every write endpoint of the eleven modules | Call each as an unauthorised role | 403 each, nothing saved | Table row counts unchanged | Ready in part (route capabilities and the attack-surface manifest cover the routes; the Cashier did pass on the material issue, decision 2) |
| E2E-12 | A job in flight (IRN, print) | Kill the worker; restart | The job resumes from the outbox; state Unknown, not lost or doubled | Outbox row count; one provider call | Ready on the fake provider (durable outbox); live waits on 75.2.11 |
| E2E-13 | A busy till | Fail the scanner and printer under load | The sale stays; a recoverable print-only error | Sale posted once | Waits on devices and a soak run (the failure-injection lab) |
| E2E-14 | Old one-SKU job-work records | Run the migration | One line each, balances unchanged | Before and after balances equal | Partial (130.16 migrated the records; rerun on a copy of the real data before go-live) |
| E2E-15 | Every register and export of the matrix | Run with a limited-scope user | Totals and source rows match that user's scope | Source ids and control totals | Partial (registers R-TX-01..16 exist; the all-registers scope check was not run) |
| E2E-16 | A sample tenant with provider and outbox rows | Back up; restore elsewhere | Liability, GL, stock, outstanding, IRN and print audit equal | Checksum compare | Partial (restore verify and the drill log exist; the checksum compare over provider and outbox rows is not automated) |
