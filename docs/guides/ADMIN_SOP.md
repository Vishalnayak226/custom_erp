# Admin SOP — Step-by-Step Procedures

This is the deep companion to **[ADMIN_GUIDE.md](ADMIN_GUIDE.md)**. The Guide covers getting the system running (Part A), the operator/platform level (Part C), and developer/CTO level (Part D) as a reference manual. This SOP picks up where its Part B leaves off and gives literal, click-by-click procedures for **every admin-only screen** (the **Settings** sidebar module) and **every maker-checker / approval-gated workflow** in the system — the two categories the task that produced this doc specifically asked to cover in depth.

This document assumes you're already logged in as an **HR/Admin** role (ADMIN_GUIDE §A.3) and know the basics from **[USER_SOP.md](USER_SOP.md) §1** (record-list screen conventions, error handling). It does not repeat those.

---

## Part A — Settings Screens

All five of these live under the **Settings** flyout in the sidebar, plus the **Activity Log** already covered here too. Every action on every one of these screens is enforced **server-side** as HR/Admin-only — a non-admin role gets a 403 even though the menu items themselves are currently visible to everyone (see USER_GUIDE.md §3's note on this).

### A.1 Users — creating and managing accounts

Sidebar: **Settings** → **Users**.

1. Fill in **Username**, **Password** (8+ characters), **Email**, pick a **Role** from the dropdown (populated from every role the system knows about), and optionally a **Location Code** (defaults to whatever you type — leave blank if not location-scoped).
2. Click **Create User**. It appears in the list below, status **Active**, immediately usable to log in.
3. **Deactivating a user**: click **Deactivate** on their row, confirm. Their login is then rejected exactly like a wrong password — they are not deleted, just locked out. You cannot deactivate the account you're currently logged in as.
4. **Reactivating**: click **Reactivate** on an Inactive user's row, confirm.
5. **Changing a user's location**: click **Set Location** on their row, type the new location code when prompted. This matters because location-scoped authorization on several screens (POS, Transfers, Expenses) checks the acting user's own location — a user stuck on the wrong location code will see permission errors that look unrelated to location until you check this.
6. **MFA reset**: if an HR/Admin (or another MFA-required role) loses their authenticator device, there's no button for this on the Users screen — see ADMIN_GUIDE §B.2: run `cmd/reset_mfa` (`go run ./cmd/reset_mfa`) or ask a developer.
7. **Lockouts**: if a user is locked out after repeated failed logins, wait for the automatic lockout window to expire, or clear it directly at the database level (there's no in-app "unlock" button as of this writing).

### A.2 Roles — the permission grant matrix

Sidebar: **Settings** → **Roles**. This controls what each non-admin role can Read/Create/Update/Delete, per record type. **HR/Admin itself always has full access everywhere and never needs a row here.**

1. Pick a **Role** and a **Record Type** from the two dropdowns.
2. Tick whichever of **Read / Create / Update / Delete** should apply (Read is ticked by default).
3. Click **Save Grant**.
4. The table above the form always shows every currently-granted (role, record type) pair with checkmarks/dashes for each permission — this is your source of truth for "what can Role X actually do."
5. **A role with no row at all for a given record type has zero access to it — it fails closed, not open.** If a role should be able to see/use something and can't, the fix is almost always: add a grant row here for that exact (role, record type) pair, rather than a code change.
6. There is no Edit or Delete action on an existing grant row — to change one, just submit the form again with the same Role + Record Type and the new checkbox combination; it upserts (creates or replaces) that row.

### A.3 Prefix Configs — document numbering formats

Sidebar: **Settings** → **Prefix Configs**. Controls the auto-generated number format (invoice numbers, PO numbers, etc.) for every record type that uses a Code sequence.

1. The list shows every configured record type with its current **Prefix**, **Separator**, **Padding**, **Reset Interval**, and **Status**.
2. Click **Edit** on a row. You'll be prompted, one field at a time, for the new **Prefix**, **Separator**, **Padding Width** (a number), and **Reset Frequency** (type exactly `ANNUAL`, `MONTHLY`, or `NEVER`).
3. Confirming each prompt saves the whole updated configuration in one call — there's no separate Save step after the prompts.
4. **Reset Frequency** controls when the running sequence number restarts at 1: `ANNUAL` at the start of a financial year, `MONTHLY` at the start of a month, `NEVER` for a sequence that just keeps counting up forever.

### A.4 Dynamic Labels — renaming vocabulary

Sidebar: **Settings** → **Dynamic Labels**. Lets you overlay your industry's own terminology on top of the system's built-in labels (e.g. show "Design Number" everywhere the system would otherwise say "SKU"), without any code change.

1. Click **Add Translation Rule**.
2. Enter the **original word/label** exactly as it currently appears on screen (case-insensitive match, e.g. `Brand`), then the **replacement overlay label** (e.g. `Material Grade`).
3. It takes effect immediately across the app — every label, table header, and button matching the original text switches to your replacement the next time a screen renders.
4. To remove a mapping, click **Remove** on its row and confirm; the original built-in label comes back.

### A.5 Database Schema Design — registering new record types and fields

Sidebar: **Settings** → **Database Schema Design** (internally still called "DocType Builder" in the code/docs history — same thing). This is how you add a brand-new master or transaction record type, or a custom field on an existing one, **without any code change**.

1. **Registering a new record type**: click **Register New Record Type** (top right). You'll be prompted for:
   - **Record Type Name** — the technical identifier (e.g. `WarrantyClaim`).
   - **Module Group** — which sidebar/module grouping it belongs to (e.g. `Procurement`).
   - **Document Type** — type exactly `Master` (a reference list, gets an automatic Setup entry) or `Transaction` (a business document, needs its own screen wired up separately to be reachable — see the note in Part D of this doc about what "Transaction" without a screen looks like today).
2. **Configuring fields on a record type**: on the left, hover a **Module** to reveal its record types in a flyout (works exactly like the sidebar's own module flyouts), then click one to load its field list on the right.
3. Click **Add Field**. You'll be prompted in sequence for:
   - **Field name** (technical identifier, e.g. `material_weight`)
   - **Label** (display text, e.g. `Material Weight`)
   - **Fieldtype** — type exactly one of `Data` (short text), `Number`, `Select` (dropdown), `Check` (boolean), `Date`, or `Link` (foreign key to another record type)
   - Whether it's **mandatory** (confirm/cancel)
   - **Options** — for `Select`, a comma-separated choice list; for `Link`, the target record type's name; otherwise leave blank
4. Confirming the last prompt saves the field immediately — it appears in the table and is live on that record type's create form right away.
5. **Deleting a field**: click **Delete** on its row in the field table, confirm. This removes the field definition (existing saved data in that field on old records is not retroactively stripped, it just stops showing).
6. This is different from adding an actual *record* of an existing type (a new Vendor, a new Brand) — that's a normal business-user task done from that record type's own screen (USER_SOP.md §1).

### A.6 Activity Log — audit trail and error console

Sidebar: **Settings** → **Activity Log** (internally still called "Log Hub"). Three tabs.

1. **Audit Logs** (default tab): every logged user action — who, what action, details, and timestamp. Read-only, no filters beyond what's on screen.
2. **System Errors**: every recovered panic/error the server has caught, with severity, module, and message. Click any row to see its full **stack trace** in a dialog — use this when investigating a "Something Went Wrong" screen a user reported, especially if they gave you a correlation ID.
3. **Integration Payloads**: every outbound webhook/integration event, its status (Dispatched/Success/Failed/etc.), attempt count, and payload preview. A **Failed** row shows a **Retry** button — click it, confirm, and the event is re-queued for delivery.
4. **Test Panic Recovery** button (top right, next to the page title): deliberately triggers a backend panic to verify the recovery middleware is catching panics and the server stays up. Use this after any deployment change you're unsure about, not routinely — it's a diagnostic tool, not something to click during normal operation. A response (even an error response) means recovery worked; only a dropped connection means it didn't.

### A.7 Industry Selector — switching the active industry profile

Not under Settings — it's the dropdown in the header bar next to the Sync button, but it's an admin-level action (it changes tenant-wide configuration).

1. Pick an industry from the dropdown (Jewelry, Food & Beverage, Automobile, Clothing, Pharmaceuticals & Biotechnology, Metal & Steel Fabrication, Construction & Contracting, Medical Devices, Semiconductors, Agriculture & Perishable Goods).
2. Confirm the dialog warning that this reloads preset table field configurations.
3. The app re-fetches labels and registered record types and lands you back on the Dashboard with the new profile's preset fields active.
4. There's no "current industry" indicator to read back later — this is a one-time overlay action, not a persistent setting you can review afterward from within the app (the browser just remembers your last selection locally).

---

## Part B — The Maker-Checker Engine and Every Approval-Gated Workflow

### B.1 How it works, generically

Every approval-gated document follows the same shape: a maker creates it as **Draft**, submits it (an explicit "Submit for Approval" action, or automatically as part of another action like POS checkout), it becomes **Pending Approval**, and it shows up for a checker on the **Approvals** screen (USER_SOP.md §6) — filtered to whichever role is required for that specific document's amount. The checker clicks **Approve** or **Reject** (optionally with a note on rejection). Once decided, it's permanent and logged (`approval_log`) — there's no silent re-open.

Which role is required, and above what amount, is controlled by rows in an internal `approval_rules` table (per-tenant, per-doctype, banded by a min/max amount). As of this writing there is a read/write API for this (`GET`/`POST /api/v1/approval/rules`, HR/Admin-only for writes) but **no screen in the app to view or edit these rules** — see §B.9 below for how to change a threshold today.

### B.2 Purchase Order approval

- Maker flow: USER_SOP.md §15 (create Draft, Submit for Approval).
- **Default routing**: 0 – 49,999 → **Store Manager**; 50,000 and above → **HR/Admin**. (These are the seeded defaults — see §B.9 to change them.)
- Checker flow: Approvals screen, Approve/Reject as in §B.1.

### B.3 Purchase Requisition approval

A `PurchaseRequisition` record type exists with the same default amount routing as Purchase Orders (0–49,999 → Store Manager, 50,000+ → HR/Admin), and it's already wired into the approval engine at the data layer.

**However, there is currently no screen anywhere in the app to create, view, or submit a Purchase Requisition** — no sidebar entry, no bespoke view, and it isn't a Master-type record so it never appears under Setup either. If your process depends on requisitions preceding a PO, this step is not yet usable from the UI; flag it to a developer if you need it working end-to-end.

### B.4 Expense Claim approval

- Maker flow: USER_SOP.md §25 (create Draft, Submit for Approval).
- **Default routing**: 0 – 4,999 → **Store Manager**; 5,000 and above → **HR/Admin**.
- After the manager approval decision, the claim still needs the separate **Finance Verify** and **Mark Paid** steps (USER_SOP.md §25) — those are not part of the maker-checker engine, they're plain status-transition buttons any authorized user can click without a second sign-off.

### B.5 POS discount-approval gate

- This is the mechanism USER_SOP.md §3.2 describes from the cashier's side: a sale whose discount is high enough routes to Pending Approval instead of completing immediately.
- **Default routing**: any `POSCart` with a discount at or above the configured threshold → **Store Manager**. Note this routes on the *discount percentage/amount*, not the cart's rupee total, unlike every other amount-routed doctype.
- Checker flow: the sale shows up on the Approvals screen like anything else. Approving it finalizes the sale (posts stock and GL entries) exactly as if the cashier's original checkout had gone straight through; rejecting it cancels the sale — it never posts.

### B.6 Cycle-count variance approval

- A `CycleCountLine` record with any non-zero quantity variance routes to **Store Manager** for approval — this is the "cycle-count variance approval" gate.
- **Gap to know about**: the count itself (physically counting a bin and recording the variance) and its reconciliation (`POST /api/v1/wms/cycle-count/reconcile`) are backend-only — there is no screen to *start* a cycle count or enter counted quantities. If a `CycleCountLine` record does exist (created via direct API/database work), it will correctly show up on the Approvals screen and can be decided normally — it's only the *initiation* step that has no UI today.

### B.7 PIM Content approval (ProductContent)

- Maker flow: USER_SOP.md §27.2 (Workbench tab → item detail panel → Content → Submit for Approval).
- **Default routing**: any amount → **HR/Admin** (ProductContent has no meaningful rupee amount to band on, so it's a single flat rule).
- Checker flow: same Approvals screen, or jump straight to the underlying `ProductContent` record list from the PIM Dashboard tab's "Pending approval" card.

### B.8 Vendor Invoice override-payment approval

- Normally a `VendorInvoice` is paid directly once **Matched** (USER_SOP.md §7) — no approval needed for that path.
- The backend also supports an **override**: paying an invoice that is *not* Matched (e.g. stuck in `MismatchHold`) by supplying an `override_reason`, which claims it as Pending Approval instead of paying immediately, routed by the same engine (default: any amount → **HR/Admin**).
- **Gap to know about**: the app's Vendor Invoices screen never sends an `override_reason` — its **Pay**/**Pay w/ TDS** buttons only appear once an invoice is already **Matched**, and a `MismatchHold` invoice's only available action is **Match** again. There is currently no UI path to actually trigger this override. If a MismatchHold invoice genuinely needs to be paid without a clean 3-way match, that requires direct API access today, not a button in the app.

### B.9 Changing approval-rule thresholds/roles (no screen — API only)

Since there is no admin screen for `approval_rules` yet, changing a threshold or required role (e.g. raising the PO auto-approval-required amount, or changing who approves POS discounts) means calling the API directly. Any logged-in HR/Admin can do this from PowerShell:

```powershell
# 1. Log in and capture the bearer token (replace credentials)
$login = Invoke-RestMethod -Method Post -Uri "http://localhost:8080/api/v1/login" `
  -Headers @{ "Content-Type" = "application/json"; "X-Tenant-ID" = "default" } `
  -Body '{"username":"<admin-username>","password":"<admin-password>"}'
$token = $login.token

# 2. View current rules for a doctype
Invoke-RestMethod -Uri "http://localhost:8080/api/v1/approval/rules" `
  -Headers @{ "Authorization" = "Bearer $token"; "X-Tenant-ID" = "default" }

# 3. Create or update a rule (id omitted = new row; pass the existing id to edit one)
Invoke-RestMethod -Method Post -Uri "http://localhost:8080/api/v1/approval/rules" `
  -Headers @{ "Content-Type" = "application/json"; "Authorization" = "Bearer $token"; "X-Tenant-ID" = "default" } `
  -Body '{"doctype":"PurchaseOrder","min_amount":0,"max_amount":49999,"required_role":"Store Manager"}'
```

`max_amount` can be omitted/`null` for an open-ended top band. The save endpoint runs an overlap check against existing bands for that doctype and rejects a conflicting range with an error message explaining the conflict. Every change here is written to the Activity Log's Audit Logs tab (`SAVE_APPROVAL_RULE`) automatically, same as everything else in this system — so even though it's API-only, it's not untracked.

---

## Part C — Finance Admin Procedures

### C.1 Accounting Periods — open and close

Covered from the general-user angle in USER_SOP.md §5.2; the admin-relevant points:

1. Create periods (Period Name, Start/End Date) ahead of when they're needed — there's no requirement they be contiguous or non-overlapping enforced by the form itself, so be deliberate about your naming/date convention.
2. Before closing, always click **Close Checklist** first and actually read it — it flags things like unposted documents that would otherwise be silently locked out of that period once closed.
3. **Closing is permanent — there is no reopen.** Treat it as the same class of operation as the database restore in ADMIN_GUIDE §C.3: only do it once you're sure, and only after everything that belongs in the period has been posted.

### C.2 Executing a Payment Proposal run

Covered from the general angle in USER_SOP.md §8. Operationally: build a proposal from every Matched invoice you intend to pay in one batch (e.g. "everything due this Friday"), then **Execute** it once — this pays every invoice in the batch through the normal vendor-invoice payment path in one action, and reports per-invoice failures individually rather than failing the whole batch. Re-running Execute on an already-Executed proposal isn't offered (the button only shows while Draft), so if some invoices in a batch failed, pay those specific ones individually from the Vendor Invoices screen rather than trying to re-run the proposal.

### C.3 TDS-aware vendor invoice payment

Covered in USER_SOP.md §7 step 3. Admin-relevant setup: the TDS sections offered in the **Pay w/ TDS** dropdown come from the `TDSSection` record type (Finance module) — create these first via Setup-style record-list access if the dropdown is empty (fields: Section Code, Description, Threshold Amount, Rate %, Status). There's no automatic threshold enforcement tying a specific section to a specific invoice amount — the person paying picks the section manually, so make sure whoever has this role knows which section code applies to which vendor/transaction type.

### C.4 Running a Bank Reconciliation

Covered in USER_SOP.md §9. Admin-relevant points: reconciliation only *matches* existing `BankStatementLine` records against GL postings — it does not create statement lines from a bank file automatically beyond what Bulk Import CSV brings in. If a reconciliation run reports many unmatched lines, the likely causes are: the statement CSV wasn't imported for the right account/date range, or GL postings for that period haven't happened yet (e.g. an unposted Sales Invoice or unpaid Vendor Invoice).

### C.5 GST validation on Purchase Orders and POS checkout

- On the **Purchase Orders** screen (USER_SOP.md §15), the **Calculate GST** button is a pure calculator against `POST /api/v1/gst/calculate` — it shows the CGST/SGST (intrastate) or IGST (interstate) breakdown for the amount/rate/interstate flag entered, but it does **not** change what gets saved to `total_amount` on the PO (which this system treats as the taxable value throughout its accounting). It's there so a maker can sanity-check the tax math before submitting, not to record a separate tax liability line.
- On **POS checkout**, GST is calculated and posted automatically per line based on each item's HSN-classified rate — there's no separate admin step for this; it's already live for every sale.

### C.6 Report export job lifecycle (queue → poll → download)

Covered from the user's perspective in USER_SOP.md §14.2. The admin-relevant mechanics: clicking **Export in Background** on the Report Catalog tab queues an async job (`POST /api/v1/reports/export`) rather than generating the CSV inline — this matters for large reports that would otherwise time out a normal request. The screen polls `GET /api/v1/reports/export/{id}` automatically every 2 seconds until the job's status leaves `Pending`; a `Failed` job shows as failed with no further automatic retry (re-run the export from scratch). The actual CSV download (`?download=1`) is fetched as an authenticated blob and handed to the browser as a local file download, not a direct link — this is why you can't just paste an export URL into a new tab and expect it to work; it must go through the app's own authenticated fetch.

---

## Part D — Known Gaps: Backend Capability With No Screen Yet

For completeness (and so you don't spend time hunting for a button that doesn't exist), here's everything found, while writing this SOP, that has real backend/API support but **no UI entry point** as of this session:

| Capability | Backend evidence | What to do today |
|---|---|---|
| **GRN (Goods Receipt Note)** creation | `GRN` doctype registered (Transaction type), referenced by Vendor Invoice matching | No screen anywhere creates one. If your process needs GRN numbers, they must be created via direct API/database access. |
| **Purchase Requisition** | `PurchaseRequisition` doctype + approval-rule band already seeded | No sidebar item, no view, not Master-type (so not in Setup either). API/database only. |
| **Putaway** (assigning received stock to a bin) | `POST /api/v1/wms/putaway` | API only. |
| **Pick List** (bin-driven picking) | `GET /api/v1/wms/pick-list` | API only. (Fulfillment's Pending → Picking → Packed → Dispatched screen, USER_SOP.md §12, covers order-level pick/pack/dispatch status but not bin-level pick-list generation.) |
| **Bin condition transition** (e.g. Good → Damaged) | `POST /api/v1/wms/condition-transition` | API only. |
| **Cycle count initiation/counting** | `POST /api/v1/wms/cycle-count/reconcile` | API only — see §B.6 above; the resulting variance line's *approval* does work from the Approvals screen once it exists. |
| **Approval Rules configuration screen** | `GET`/`POST /api/v1/approval/rules` | API only — see §B.9 above for the exact calls. |
| **Vendor Invoice override-pay** (paying a non-Matched invoice) | `PayVendorInvoice`'s `overrideReason` parameter | No UI trigger sends this — see §B.8 above. |
| **Per-record Edit on a generic record-list screen** | n/a — verified absent in the UI code itself | Only Delete-and-recreate, or (for Item/PIM record types only) multi-select Bulk Edit — see USER_SOP.md §1 and §27.4. |

None of these are things a user is doing wrong — they're genuine gaps between what the engines already support and what's wired into the frontend. If any of them become priorities, they're each a frontend-only addition (the backend logic and validation already exist and are already tested), not new engine work.

---

## Glossary Addendum

Terms used in this SOP not already in ADMIN_GUIDE.md's glossary:

| Term | Plain-language meaning |
|---|---|
| **Amount slab / band** | A min–max amount range in `approval_rules` that determines which role must approve a document in that range. |
| **Maker-checker slab routing** | The general mechanism: the amount (or, for POSCart/CycleCountLine, a non-amount value like discount % or variance qty) on a document is checked against configured slabs to decide which role's Approvals inbox it lands in. |
| **Override (Vendor Invoice)** | Paying an invoice that hasn't cleared normal 3-way match, gated behind its own approval instead of being blocked outright. |
| **Async export job** | A background-processed report export, tracked by a job ID, so a large report doesn't have to complete within one HTTP request's timeout. |

---

*See [ADMIN_GUIDE.md](ADMIN_GUIDE.md) for everything outside this SOP's scope: initial setup, `manage.ps1`/`promote.ps1` operations, backup/restore, incident response, and the developer/architecture reference.*
