# Business UAT — run sheet and closure log

**Status: DRAFT, ready to run once real business users are available.** Written
2026-08-06 against checklist item 26.11.5.

This is the *process wrapper* around the existing
[`docs/guides/UAT_CHECKLIST.md`](../guides/UAT_CHECKLIST.md) (22 sections of
screen-by-screen test cases). That file says **what to click**. This file says
**who runs it, in what order, what counts as a pass, and how it gets signed off**
— which is what 26.11.5 actually asks for and what the checklist alone can't
provide.

Do not duplicate test cases here. If a case is missing, add it to
`UAT_CHECKLIST.md` where it belongs.

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
| Business owner | _TBD_ | Final sign-off (§6) |

One person may hold several roles — but **the approver must be a different
person from the maker** for Scenario E to test anything real, since
maker-checker self-approval is blocked by design.

---

## 3. Prerequisites

Complete `UAT_CHECKLIST.md` §0 first, plus:

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
[`incident_runbook.md`](incident_runbook.md) §1.

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
   ([`hypercare_plan.md`](hypercare_plan.md) §5) — do not let them fall between
   the two documents. That gap is the usual way agreed fixes get lost.
3. Reset or archive the UAT tenant so its data can never be mistaken for real.
