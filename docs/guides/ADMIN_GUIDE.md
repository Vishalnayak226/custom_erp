---
doc_id: DOC-C44AC5B356
title: Admin Guide
type: procedure
status: draft
owner: documentation-maintainer
approvers: [documentation-maintainer, documentation-maintainer]
audience: [maintainers, documentation-maintainer]
applies_to: source documentation; scoped release acceptance required
authority: transition-copy
confidentiality: internal
last_verified: 2026-09-09
review_by: 2026-10-09
supersedes: none
superseded_by: none
verification_scope: metadata and lifecycle classification; domain acceptance pending
---

# Admin Guide

A complete, standalone operator manual — written so a person can pick up this system with **zero AI assistance**, starting from a bare Windows machine, and get it running, keep it running, and grow with it. It's organized in layers: start at §1 if you've never touched this system before; skip ahead if you already know the basics and need a specific procedure.

This guide reuses, rather than duplicates, the deeper operational docs that already exist — it's the table of contents and the walkthrough that ties them together. Where a topic has its own detailed doc, this guide says so and points there.

*Need literal click-by-click steps for an admin screen or a maker-checker/approval workflow? See **[ADMIN_SOP.md](ADMIN_SOP.md)** — same operator-manual voice, one section per screen and per workflow.*

---

## Part A — Foundation (for someone who has never seen this system)

### A.1 What is this, in one paragraph?

This is business software (an ERP — Enterprise Resource Planning system) that runs as one program (`erp-server.exe`) plus one database (PostgreSQL). It handles point-of-sale, inventory, purchasing, accounting, and more, for one or more businesses ("tenants") at once. Everything lives in two things: the program, and the database it talks to. There's no cloud dependency, no external service required to run it locally.

### A.2 What you need before you start

- **Go** (the programming language this is written in) — a "portable" install (no installer needed, just extracted files) lives at `%USERPROFILE%\go-portable\go`.
- **PostgreSQL** (the database) — a portable install lives at `%USERPROFILE%\pg-portable\pgsql`, with its data at `%USERPROFILE%\pg-data`.
- **PowerShell** — comes with Windows; this is the "terminal" you'll type commands into.
- **Git** — for pulling/pushing code changes.

If any of these aren't already installed on the machine you're setting up, download the portable/zip versions of Go and PostgreSQL (not the installer versions) and extract them to the paths above — the scripts in this repo assume those exact locations.

### A.3 Starting the system for the first time

Open PowerShell, navigate to the repository folder, and run:

```powershell
.\manage.ps1
```

This opens an interactive menu. Choose **1) Start** — it starts the database, waits for it to be ready, then starts the ERP server. Choose **4) Status** any time to see what's currently running.

Once it says running, open a web browser and go to `http://localhost:8080`. You'll see a login screen. Development login credentials live in `DEV_CREDENTIALS.local.txt` at the project root (this file is intentionally excluded from version control — never commit real credentials).

### A.4 Stopping the system

From the same menu, choose **2) Stop**. Or run `.\manage.ps1 stop` directly. This is safe to do any time — it shuts down cleanly.

### A.5 The single most important safety rule

**Never delete the database data folder** (`%USERPROFILE%\pg-data`) unless you have a recent, verified backup (see §C.3) and you mean to start over. That folder *is* the business's data — every sale, every stock count, every ledger entry.

---

## Part B — Day-to-Day Operation

### B.1 The `manage.ps1` command reference

Run these from the repository root in PowerShell:

| Command | What it does |
|---|---|
| `.\manage.ps1` | Interactive menu (safest option if unsure). |
| `.\manage.ps1 start` | Start database + server. |
| `.\manage.ps1 stop` | Stop server + database. |
| `.\manage.ps1 restart` | Stop then start. |
| `.\manage.ps1 status` | Show what's currently running and its port. |
| `.\manage.ps1 logs` | Show the last lines of the server and database logs. |
| `.\manage.ps1 release` | Rebuild the server as an optimized, smaller binary (stops the server first if running; does not restart it — run `start` after). |
| `.\manage.ps1 backup` | Back up every environment's database (see §C.3). |
| `.\manage.ps1 restore -Env <env> -File <path>` | Restore a database from a backup — destructive, requires typed confirmation (see §C.3). |
| `.\manage.ps1 fleet-status` | One-shot report across dev/test/live: which are up, their version, last deployment. |

Add `-Env test` or `-Env live` to target an environment other than the default (`dev`) — see §D for what environments are.

### B.2 User and Role Management

- **The order to set people up in**: create the **role** first (Roles screen), give it access (usually a whole module), then create the **user** with that role. A user and a role are both required — there is no user without a role, and the role must already exist.
- **Creating a role (Stage 57.17, Stage 79)**: **Roles** screen → **+ New Role**. Give it a name people will recognise ("Purchase Clerk", "Store Keeper") and, optionally, a one-line description, then **Create Role**. The new role opens straight on its access page (below). A new role **can see nothing** until you give it access — the safe default while you are still setting it up. Names that mean the administrator role ("Super Admin", "HR/Admin") are refused, as is a name that differs from an existing role only by capitals.
- **Creating a user**: **Users** screen (Super Admin only — the menu entry and the API both refuse every other role). Fill in username, password, email, and role, then **Create User**. New accounts start **Active**. The role must be one that exists (a mistyped role is refused rather than creating a user who sees an empty system); capitals don't matter, the stored spelling is the role's own. The password must clear the same baseline (Stage 49.2.2) a self-service change/reset does: at least 12 characters by default (`security.password_min_length`, editable in Configuration), not one of the most common/breached passwords, not the username itself, and not a simple repeated or sequential pattern — the API rejects one that doesn't with a specific reason.
- **The password you type is temporary (Stage 57.17).** Give it to the user; the first time they sign in they get a **Set your own password** screen and cannot do anything else in the system — through the screens *or* the API — until they have chosen their own. Ordinary roles sign in with username and password only, no authenticator code; only Super Admin / HR/Admin accounts need two-factor.
- **Deactivating/reactivating a user**: same **Users** screen, the **Deactivate**/**Reactivate** action on each row. You can't deactivate the account you're currently logged in as. A deactivated user's login is rejected the same as a wrong password, **and any session they already have open stops working on their next click** — you do not have to wait for their sign-in to time out. The same applies to changing someone's role or location: it takes effect on their live session, not at their next login. (If you make the change directly in the database rather than through this screen, allow up to 30 seconds — see `AUTH_STATE_CACHE_SECONDS` in `deploy/erp.env.example`.)
- **Giving a role access (Stage 79)**: **Roles** screen (also Super Admin only). It opens on the list of roles — each with what it can reach ("3 modules · 24 record types") and who holds it. Click a role (or **Set access**) to open its access page: **every module** is listed with **Full access / Read / Create / Update / Delete** tick boxes.
  - Tick a box on a **module** row to give that right on every record type in the module at once (the usual case — e.g. *Procurement* covers Purchase Requisition, RFQ, Vendor Quote, Purchase Order, Goods Receipt, Vendor and Vendor Invoice in one go). A half-filled box means only some of its record types have that right; "3 of 12" beside the module name counts the record types with any access.
  - Click the module name (or **Expand all**) to see its record types and tick them one by one for a finer grant. **Find a module or record type** narrows the list.
  - **Full access** ticks all four. **Create, Update and Delete include Read** — ticking one of them ticks Read, and clearing Read clears them.
  - Nothing is stored until **Save access**: the bar at the bottom counts the record types changed, **Discard changes** puts the ticks back, and leaving the page with unsaved ticks asks first. Everything ticked is saved together, in one go.
  - Super Admin and HR/Admin show as **Full access (built in)** and have nothing to set. A role with no access to a record type gets **no access at all** to it (fails closed) — Super Admin always has full access and never needs a row here. See `../ERP_BLUEPRINT.md` §3 for how role checks are enforced (server-side, on every action — never trust a UI-only restriction).
- **What a role sees in the menu (Stage 57.17)**: a module appears once the role can read something in it, and only then. A "Store Keeper" granted the Inventory module sees **Home, Reports, Knowledge Center, Stock** and the matching **Setup** lists — not POS, Finance, Sales, Procurement, HRM or PIM. **Approvals** is shared by every module: an approver reaches it from the "waiting on you" card on Home even when their role has no Finance menu. (Before Stage 57 most module screens were shown to every role regardless of grants; the server still refused the data, but the menu did not reflect the role.)
- **Super Admin** and other privileged roles require MFA (Multi-Factor Authentication — a 6-digit code from an authenticator app).
- **Resetting a user's two-factor setup.** On the **Users** screen each row has a **Reset 2FA** action. Use it when someone has lost both their phone *and* their recovery codes. It clears their authenticator and every recovery code they hold, and forces them to set two-factor up again on a new device at their next login — it does **not** turn MFA off for that account. (The old route, the `cmd/reset_mfa` command-line utility, still exists for the case where nobody can log in at all; it needs shell and database access on the server.)
- **Users can normally recover themselves, and should be told so** — every reset you perform is one they could have avoided:
  - Ten **single-use recovery codes** are issued when they first enrol, shown once, and accepted on the login screen in place of a 6-digit code.
  - **My Profile → Two-Factor Recovery** lets them move their authenticator to a new phone (confirming with their password, not a code, since the old device may be gone) and generate a fresh set of codes. It also shows how many codes they have left.
  - Only a scrambled fingerprint of each code is stored, so **you cannot look up a user's codes for them** — if they have lost them, the choice is regenerate (if they can still sign in) or Reset 2FA (if they can't).
- **Resetting a user's password (Stage 49.2.4).** The same **Users** screen row also has a **Reset Password** action, for someone locked out who cannot reach their own **My Profile → Change Password** (forgotten password) or the emailed **Forgot Password** link (no working email on file). You'll be asked for a reason.
  - **Password Reset Request** under Setup is the history of that workflow, not a form to create or edit a reset manually. Its **Open Users for password reset** button takes authorized administrators to Users. Choose the named user's **Reset Password** action; cancel the reason prompt if you do not intend to proceed. Other roles must contact an administrator. History no longer offers generic New, Bulk Import, Edit or Delete controls; the existing server permissions and dual-approval rules remain unchanged.
  - For an ordinary account, the reset happens immediately: a one-time password is shown to you once (it is never emailed, and never shown again) — read or relay it to the user through a channel you already trust is really them, such as a phone call, not chat/email. It replaces their password immediately and signs out every session already open on that account. When they sign in with it they are taken straight to **Set your own password**, exactly as on a first sign-in (Stage 57.17).
  - **Disabling someone instead**: use **Deactivate** on their row (above). Their open sessions stop working on their next click and their password stops working; **Reactivate** restores the account with the same password.
  - **If the target is itself a Super Admin account, one admin acting alone cannot do this.** The request instead appears on the **Approvals** screen, where a *different* Super Admin (never the one who requested it) must approve it before anything changes — and the reason you gave is what they see when deciding. Only the approving admin ever sees the generated one-time password, in the approval decision's own confirmation dialog; the requester does not. This is deliberate: a single compromised or malicious admin account must not be able to both request and learn a privileged credential. You cannot use this action to reset your own password — use **My Profile** instead.
- **Worth doing before you need it: keep a second Super Admin account, enrolled on a different device.** With a single admin account, a lost phone plus lost codes means nobody left in the tenant can perform the reset, and recovery drops back to server shell access. It is also now a functional requirement for password recovery specifically: **with only one Super Admin account, that account's own forgotten password can only be recovered through the emailed reset link or server/database access** — there is no second admin available to approve a helpdesk reset.
- **Changing your own recovery email now requires your current password** (Stage 49.2.4), on **My Profile → Contact & Session**. This closes an account-takeover path: without it, anyone who got hold of a signed-in session (a stolen bearer token, an unattended browser) could silently redirect "Forgot Password" to an address they control. If you had an email on file before the change, it receives a notice — the new address does not, since this is a risk alert, not a confirmation step.

> **Supplier logins (PIM).** A supplier who submits product content is created here like any other user, with the role **Supplier** — there is no separate portal to set up. Their account must also be linked to the **Vendor** record they speak for; until it is, they can sign in but every screen refuses them, by design. A Supplier session can only ever see and edit submissions filed under their own vendor. See §PIM for the review side.
- If a user's login is locked out after too many failed attempts, wait for the automatic lockout window to expire, or have an admin clear it directly.

### B.3 Configuring the System (no code required)

Several things are configurable through the app's admin screens, not by editing code:

- **Operational settings, module by module** (return windows, tolerances, timeouts, limits, offer-free thresholds) — the **Configuration** screen. This is the main one; see §B.3.0. If you are looking for "where do I change *that number*", start here.
- **Document number formats** (invoice numbers, PO numbers, etc.) — **Prefix Configurations** screen. This is where every transaction number comes from; see §B.3.2, it has more to it than the name suggests.
- **Renaming terms** to match your industry's vocabulary (e.g. "Design Number" instead of "SKU") — **Dynamic Labels** screen.
- **The industry profile** (which industry's extra fields are overlaid onto the standard record types) — **Settings → Configuration → Industry**, for the org's admin only. **Once set, changing it needs a written reason**; see §B.3.2.1.
- **Adding new record types or custom fields** — **Database Schema Design** screen (this is the same "metadata-driven" engine described in `../architecture/framework_architecture.md` — new master/transaction types don't need a code change). This is different from adding an actual *record* of an existing type (a new Vendor, a new Brand, etc.) — that's a business-user task, see [User Guide](USER_GUIDE.md) §8.
- **Turning modules on/off per tenant** — module entitlements, admin-only.
- **Silent printing** (labels, barcode stickers, POS receipts, sales invoices going straight to the right printer with no browser dialog) — a **Printer** record per physical printer, plus QZ Tray on each PC that prints. Full steps in **[QZ_PRINTING_SETUP.md](QZ_PRINTING_SETUP.md)**. The field that makes it one-click is **Default For** (`Shipping Label` / `Invoice` / `Sticker` / `Receipt` / `General`) — the server resolves the job to the printer holding that role, so operators never pick a printer. **Nothing depends on QZ Tray being set up**: with no Printer records, or with QZ Tray not running, every one of those screens falls back to the browser print dialog exactly as before. The one exception is **Sticker Printing**, which needs at least one Printer record even for the browser fallback, because the print is logged against a printer — see §B.3.4.
- **Which status changes a document is allowed to make** — **Status Transition Rules** screen (a normal master, Super Admin to edit). Each row says: for this record type (**Entity / Doctype**), moving from **From Status** to **To Status** is **Allowed** yes/no, and optionally **Requires Reason Code**. See §B.3.1 below — this one has a rule about how it fails that's worth understanding before you edit it.

#### B.3.0 Configuration — every operational setting, in one place

**Settings → Configuration** (Super Admin only). The left rail lists modules; pick one and you get that module's settings, each with a plain-English description of what it actually controls. Change what you need, then **Save Changes**.

**Setup path (Stage 55.3).** At the top of the page, **Setup path** lists the core masters in the order they depend on each other — Legal entity → Stores and warehouses / Departments and cost centres → Units and HSN codes → Item types and categories → Items → Vendors and customers → Document numbering → Users and their roles — each marked **Done** (with how many active records), **Next**, **To do** or **Waiting** (and what it waits for). Each step links to its list or screen. It is worked out from the records you already have every time the page opens, so it picks up where you left off and never removes anything; once every step is done it folds away to one line.

Two things worth knowing:

- **A change takes effect immediately, everywhere, with no restart.** Every setting is read at the moment it is used, not cached when the server starts. Lower the sales return window and the very next return attempt is judged by the new value.
- **Out-of-range values are refused, not clamped.** Settings that could destabilise the server if set absurdly (page sizes, concurrency, batch sizes) carry a permitted range; a value outside it is rejected at save time with a message naming the limit. Nothing is silently "corrected" behind your back.

**Finding and changing a setting.** **Find a setting** (top of the page) searches every module at once — each result shows its module. An unsaved change survives switching module or searching, a setting you changed shows its **Default** underneath, and leaving the page (sidebar, Home, reload) with unsaved changes asks first; **Cancel** keeps you on the page with your changes.

**The Settings menu is grouped** (Stage 67.3): **Users & Access** (Users, Roles, Approval Rules), **Customize** (Configuration, Prefix Configs, Dynamic Labels, Database Schema Design, Extension Hooks), **Monitoring** (Activity Log, System Status) and **Platform** (Control Panel, Tenant Entitlements, Tenant Usage, Data Console). A heading only shows when you can open at least one screen under it. The **Setup** menu is grouped the same way, by module (CRM, Core, Finance, HR, Inventory...), with the filter box at the top.

**How long people stay signed in** (Stage 67). Two limits apply, and whichever comes first signs someone out:

- **Auto logout after inactivity**, chosen by each person under **My Profile → Contact & Session → Auto Logout**: Never, 15 min, 30 min, 1 hour, 2 hours or **12 hours**. New users start on **Security → Default idle-timeout for new users**, which is **12 hours** unless you change it. So does the first admin of a new client. Activity in any open tab counts, so a tab left in the background no longer signs out the one being worked in. When this shipped, everyone still on the old 30-minute starting value moved to 12 hours. Anyone who had picked a different value kept it. So did every client whose admin had set their own default.
- **Session token lifetime**: **Security → Session token lifetime**, 24 hours by default. This is a hard limit, however active the person is.

What lives where (37 settings across 13 modules):

| Module | Examples of what you can change |
|---|---|
| Sales & Returns | How many days after a sale a return is still accepted (0 = no returns at all) |
| Procurement | Vendor-invoice 3-way match tolerance %; **how many days a PO or a GRN stays editable** (0 = no time limit); **Purchase Order auto-cancel** — days after approval an Approved PO stays open, then (checked every hour) one with nothing received is cancelled and one partly received has its open balance short-closed, reason `AUTO-EXPIRY`, rejected goods put on a draft purchase return (0 = never; POs already older than the limit are handled on the first check after you set it; the approval date is the PO's approval in **Approvals**, or its creation when no approval rule applies); **Purchase return window** — days after a goods receipt its **accepted** stock may still be returned (PURCHA-0620; rejected / damaged goods can always go back; 0 = no limit) |
| Point of Sale | Cash-drawer variance a cashier may close a shift with before a written reason is required |
| Loyalty | Point expiry, spend-per-point earned, rupee value per point redeemed, tier-recompute toggle; **customer confirms every redemption with a one-time code**; how long points a return gives back stay valid (at least 30 days) — see *Loyalty redemption controls* below |
| Inventory | How long an online stock reservation is held before it releases |
| Manufacturing | Max BOM nesting depth, production cost-variance tolerance, default MRP lead time |
| HR & Payroll | ESI wage ceiling (change it here when the statutory figure changes — no code release needed) |
| CRM | Churn threshold, default lapsed-customer period |
| Warehouse | Cycle-count intervals for A/B/C items, task productivity alert threshold |
| PIM | Max documents per bulk edit, product thumbnail size |
| Security | Session length, password-reset link validity, failed-logins-before-lockout and lockout duration, two-factor clock tolerance |
| Platform | API page-size default and cap, per-tenant concurrency, CSV import batch size, max rows for an on-screen report, default field length cap |
| Localization | **Home country** — the country this business operates from. Sets the phone-number length every module validates against and the dialling code stripped when cleaning imported data. See §B.3.0.2. |

**Loyalty redemption controls** (Stage 73.1.4). Every way points are spent — the till, the `/loyalty/redeem` API, the OTP flow — passes the same checks at the moment the points burn, so none can be bypassed:

- **Daily limit** — **Security → Max loyalty redemptions per customer per day** (5 as shipped). Once a customer has redeemed that many times today, the next redemption is refused until tomorrow.
- **One-time code** — turn on **Loyalty → Customer confirms every redemption with a one-time code** to have the customer read back a code sent to them. It is sent through the **Loyalty Redemption OTP** notification event, so set up a **Notification Template** for that event *before* turning this on — without one the code never reaches the customer and redemptions cannot complete.
- **Manager approval above a value** — **Settings → Approval Rules**, document type **LoyaltyRedemptionRequest**. As shipped: points worth ₹500 or more need a **Store Manager**. The cashier picks a reason, the request appears under **Approvals**, and the manager who approves must be at the same store and must not be the cashier who raised it. The approval is spent by the one bill it was raised for.
- **Reasons** — add **Reason Codes** with the category **Loyalty Redemption** (Setup → Reason Codes). With none, a cashier cannot raise an approval request and is told to ask you.

**Loyalty administration** (Stages 73.2.11, 73.2.12, 73.3.1 — the **Loyalty** screen, User Guide §4.8).

- **Manual adjustments** need **Reason Codes** with the category **Loyalty Adjustment**. The approval value is the **LoyaltyPointAdjustment** approval rule (Settings → Approval Rules; shipped *500 and over → Store Manager*, measured on the points' value). Only a Store Manager or an administrator can adjust.
- **Tiers.** **Loyalty → Tier spend counted over the last** (days): 0 counts all-time spend, so a tier is never lost; 365 counts the last year, so ageing spend can lower a tier after its grace days. Spend is what each paid bill took (net of offers) less refunds. Earlier builds never recorded what a bill took, so every customer's spend read zero and nobody moved up a tier, and campaign revenue, the RFM report and Customer 360 showed zero spend too. Bills sold since this release record it; older bills are worked out from their lines.
- **Accounting for points.** **Loyalty → Account for loyalty points**: *As an expense when they are redeemed* (the default, unchanged — 5250 at the till) or *As a liability when they are earned*: the value of points earned or credited is booked *Dr 5260 Loyalty Points Expense / Cr 2630 Loyalty Points Liability*, released as points are redeemed, taken back on returns, debited or expire (breakage), and booked again when a return gives points back. **Switching to the liability method does not book the points already outstanding** — post that opening balance by journal (the movement report's *Books less points outstanding* line shows the amount). Report: **Loyalty Points Movement** (Reports → CRM, or the Loyalty screen).

**Vouchers and campaigns in words** (Stage 55.17, User Guide §4.8). **Loyalty → Vouchers & campaigns** is read-only. Vouchers and campaigns are still set up on their own records (**Add or edit …** opens them). A voucher's *Now* column is the till's own check, run for the customer the voucher is meant for. A campaign's *Would pick today* uses the same selection the daily campaign worker sends to. **Consent** (Stage 55.17a): no consent, no SMS / WhatsApp.
- Each Customer has **Agrees to offers by SMS** and **Agrees to offers by WhatsApp**. **Consent recorded on / by** are filled in by the save that changes them.
- The till's **+ New Customer** asks the customer, with both unticked.
- Each Campaign names what it is **Sent by** (WhatsApp or SMS) and goes only to customers who agreed to that channel. A campaign without one sends nothing, and the Vouchers & campaigns tab says so.
- Existing customers start with neither ticked, so campaigns reach no one until consent is recorded. A CSV import of customers can carry the two columns (Yes / No) and **consent_updated_at**.
- An approved **consent withdrawal** (data-subject request) clears both.
- Order, return and invoice messages and the redemption one-time code are not offers and are not affected.

**Till cash control** (Stage 73.2.7). Cashiers record **Paid In / Paid Out / Safe Drop** from **More… → Cash in / out** (User Guide §4.0). Set up:

- **Reason Codes** with the category **Cash Movement** (Setup → Reason Codes) — a movement cannot be recorded without one.
- A **Paid Out** posts *Dr 5290 Till Petty Cash Expenses / Cr 1100*. Paid In and Safe Drop move cash between the till and the safe, both inside 1100, so they post nothing — they are the drawer's record.
- **Point of Sale → Cash drawer variance tolerance** (₹50 as shipped) is still where a reason becomes necessary at close. Above it, the close also waits for the **POSSession** approval rule (**Settings → Approval Rules**, shipped as *0 and over → Store Manager*, applied only above the tolerance). The approving manager must be at the store and must not be the cashier. Delete the rule to go back to "reason only".
- Expected cash at close = opening float + cash taken in sales (after offers, points, gift cards and any split) − cash refunds processed while a session was open at the store the goods came back to + paid in − paid out − safe drops.

**Post-sale edit history** (Stage 73.2.8, User Guide §9B.4). An edit to an issued sale (a sales invoice or sales order past Draft, or a paid till bill) needs a reason, and each one is recorded. **Reports → Report Catalog → Post-Sale Edit History** has one row per changed field: when, the document and its number, its status at the time, the field, the before and after values, who changed it and why. Filter it by dates, document type or number. The records are read-only to every role and cannot be edited or deleted. Status steps made with their own buttons (**Post**, **Settle**, completing a till bill) are not edits and need no reason.

**Customer PO / reference on B2B sales** (Stage 73.2.9, User Guide §9A.1). Sales orders and sales invoices have an optional **Customer PO / Reference** field for a business customer's own PO number. It goes from an order onto every invoice raised from it (the whole-order invoice and the per-package invoices), and it prints on the invoice. **Sales by Customer Reference** (Report Catalog, Sales) lists the orders and invoices that have one. You can filter it by dates, reference (part of it is enough) and customer. Orders and invoices made before this stage have no reference. Nothing is backfilled.

**Second-person approval of returns and refunds** (Stage 55.5a). **Settings → OMS → A second person approves returns and refunds** (off as shipped). When on, the person who raised a return cannot **Approve** it, nor approve its refund. Anyone else with Returns can, and is refused with *"Another person must approve this"* if they raised it themselves. Who approved is taken from the signed-in user, not typed. Receiving, inspecting and paying the refund out are unchanged. An **exchange at the till** is still approved at the counter (it is one transaction there).

**Exchanges at the till** (Stage 73.2.6, User Guide §4.7). The returned items go through the ordinary return (raised, received and inspected by the cashier at the counter — so an exchange needs no manager), and their refund's money is credited to **2620 Exchange Credit Clearing** instead of being paid back. The new bill debits 2620 as a tender; any credit it does not need is paid out of the till (*Dr 2620 / Cr cash*). Every completed exchange leaves 2620 at zero — a balance on it is an exchange that was started and never finished: look up the **ExchangeCredit** documents with status *Open*.

How a **return** treats points and tenders is described for cashiers in the User Guide §4.7: points spent come back as points, points earned are reversed pro rata (a balance may go negative), money goes back by the tender each item was paid with, and a gift-card share goes back onto the card.

**Integrations** is the last entry in the rail. It holds the endpoint and credentials for the two external systems this ERP talks to — **Pine Labs** card terminals (one entry per terminal ID) and **Unicommerce**, the OMS middleware (one entry per store code). Fill in the fields and save; saving an entry whose terminal ID / store code already exists updates it in place. Existing entries are listed above the form with secrets masked. The background workers pick up a changed Base URL on their next call.

> **On the session-length setting**: if your deployment sets the `JWT_EXPIRY_HOURS` environment variable, that value applies until you explicitly change the setting on this screen — an edit here always wins from then on. So the control is never a dead knob, whatever the server is configured with.

#### B.3.0.2 Home country — what setting it actually changes

**Settings → Configuration → Localization → Home country.** Defaults to India, so an untouched system behaves exactly as it always did. Changing it changes three things at once:

1. **How long a phone number has to be.** Every phone field in the product — Customer, Vendor, Employee, a Location's contact number — is validated against the selected country's national length. India is 10 digits; the UAE is 9; Germany accepts 10 or 11. The browser enforces the same limit while typing, so the user is stopped at the eleventh digit rather than at Save.
2. **What gets stripped when a number is cleaned.** Every phone number saved anywhere is normalised first: spaces, dashes, dots, brackets and unicode dashes removed, a leading `+91`/`0091` or trunk `0` resolved away. This is unconditional and applies to data arriving from your sales channels as much as to something typed by hand. Two consequences worth knowing: the same customer written three different ways is now recognised as one (duplicate-mobile detection compares the cleaned number), and you no longer have to tell anyone how to format a phone number.
3. **What counts as a foreign order.** A number that carries an explicit `+<code>` for a different country is recognised as that country's, validated against *its* rules, and stored with a country stamp — `phone_country` on a Customer, `customer_country` on a Sales Order. That is the field to segment on if you want to target customers outside your home market.

**Two deliberate asymmetries you should not "fix":**

- **A master is strict; an order is not.** A Customer with a wrong-length number is refused, with a message naming the expected shape and an example. A Sales Order arriving from a channel is **never** rejected or put on hold over its phone number — it is cleaned as far as possible, tagged with whatever country could be resolved, and saved. The order-hold reasons exist to answer "can this be shipped?", and a contact number cannot answer that.
- **Changing the home country does not rewrite existing records.** Numbers already stored keep their stored value and their country stamp. The new setting applies from the next save onwards.

The country list covers 54 countries. If one you need is missing, that is a one-line addition to the table in `engines/phone.go` — the settings dropdown, the validation, and the browser's typing limit are all generated from it.

#### B.3.0.3a A store's four identities: Store Name, Store Code, Short Code, Facility Code (Stage 60.2)

Each **Location** (Setup → Core → Location) carries four ways to name it, each with one job:

| Field | Used for | Rule |
|---|---|---|
| **Store Name** | What every screen, receipt and invoice shows — "Bandra Store". | Required. |
| **Store Code** | The system's own key for the store. Stock, reservations and transactions reference it; it is never shown in place of the name. | Required, unique. |
| **Short Code** | Printed inside document numbers: `PO/BDR/26-27/00001` (B.3.2). The two or three letters staff say out loud. | Optional; unique among short codes and store codes (`MASTER-0312`). Blank prints the Store Code. |
| **Facility Code** | The code the OMS / marketplace uses for this store. A channel order naming a facility is routed to the store with that Facility Code. | Optional; unique (`MASTER-0313`). A channel order that sends the Store Code instead still works. |

Changing a Short Code changes how the **next** numbers read; numbers already issued keep their text and each store's counter carries on.

#### B.3.0.4 Which locations a till may sell from (`Location.sellable`, Stage 53)

Every **Location** record carries a **Sellable** flag (`Yes` / `No`), next to its Type. It answers one question: *may a POS till ring up a sale here?*

It is deliberately **not** the same question as **Type**. A warehouse with a trade counter does sell; a shop being fitted out does not. Type is what the place *is*; Sellable is what it may *do*. Set them independently.

**What it gates — three places, all of them:**

| Where | Behaviour when `Sellable = No` |
|---|---|
| The POS screen's **Store** picker | The location does not appear at all. It cannot be typed in either — keying the code and tabbing away clears the box rather than committing it. |
| **Open Session** | Refused, with error `POSOFF-0245`: *"… is not a selling location — a POS till cannot sell from it."* |
| **Complete Sale** | Refused with the same error, independently of the session check. |

Checkout is checked separately from session-open on purpose. An **offline-queued sale replays through checkout without ever re-opening a session**, and a Location can be switched to `No` while a session is still open — neither case would be caught by a session-time check alone.

**How an existing location got its value.** The Stage 53.1 migration backfilled every Location that had no value:

1. A location that has **demonstrably sold before** — a `POSCart` or `POSSession` exists against it — was set to **Yes**.
2. Otherwise, a location typed **Store** was set to **Yes**.
3. Everything else was set to **No**.

Rule 1 exists because of a trap worth understanding before you audit this. The Stage 17.9 migration seeded a Location row for every location code already in use with a **hardcoded Type of `Warehouse`** — so on a tenant whose shops were never re-typed by hand, *every* Location is a "Warehouse". A naive "Store sells, nothing else does" backfill would have left such a tenant with **zero sellable locations and every till refusing every sale**. Rule 1 is what makes the migration a no-op for any till that was working the day before it ran.

> **Check this after the migration, do not assume it.** Run through Setup → Core → **Location** and confirm each shop you actually trade from reads `Sellable = Yes`. The backfill is evidence-based, not clairvoyant: a **newly opened shop that has never rung a sale and was typed `Warehouse`** comes out as `No` and will refuse to open a till until you change it. That is a one-field edit, and it is the correct direction to be wrong in — a refused sale is visible and fixable in seconds, whereas a sale silently booked against head office is neither.

**Three more things to know:**

- **An unregistered location code still sells.** A location code that has no `Location` record at all is permitted. Location is still a free-text column on most doctypes (Stage 17.9 deliberately declined to convert them to Link fields), so refusing unregistered codes would break tenants that never populated the master. Only an explicit `No` — or a `Location` record that resolves to "not sellable" — refuses.
- **A blank flag is derived, not ignored.** The field is optional (making it mandatory would have rejected a save of every pre-existing Location the next time anyone edited one). A Location saved without it is **stamped on save** — `Store` → `Yes`, anything else → `No` — so in practice every row carries a real value. That is also what lets the POS picker filter on `sellable=Yes` and get exactly the set the server would accept; a picker that offered a location checkout would refuse is precisely the drift the stamp prevents.
- **An explicit value is never overwritten.** If you set `Yes` on a warehouse, it stays `Yes`.

#### B.3.0.5 What a POS receipt says about your business (Stage 53)

The receipt header is built from **two master records**, not from anything typed at the till:

- **Location** → the shop's name and code.
- **Location → Legal Entity → LegalEntity** → the company name, **GSTIN** and state.

Plus the bill number, the date and time, and the cashier's username (taken from their resolved identity at checkout, never from the browser).

**Fill in the Legal Entity to get a tax-compliant receipt.** Setup → Core → **LegalEntity**, set `name`, `gstin` (the **State** then fills itself from the GSTIN, and **State Code** shows its short code, e.g. KA) and the address lines, then set **Legal Entity** on each selling Location — a Location left without a State takes its Legal Entity's. Until you do, the receipt prints the shop name and bill details but carries **no company name and no GSTIN**.

This degrades rather than failing, deliberately: a missing Location record, a Location with no legal entity, or an entity with no GSTIN each simply leave that line off. A receipt must always print — the alternative is a cashier holding a paid sale and no paper.

Both print paths build the same header: the server-side ESC-POS/HTML rebuild (`engines/qz_payload.go`, used whenever QZ Tray and a Receipt printer are configured) and the browser fallback. If you ever see them disagree, that is a defect — report it.

#### B.3.0.6 Refresh and Reset — what each one actually clears (Stage 53.17)

These are the two buttons at the top right of the shell, and support calls go wrong when they are treated as the same button at two strengths. They are not.

**Refresh** (`sync-btn`) re-fetches labels and the registered-doctype list, then re-renders. Nothing else is touched. It is the right answer to *"I renamed a field and cannot see it"*.

**Reset** (`reset-btn`) clears, in this order:

1. Every `erp_swr_*` entry in **sessionStorage** — the stale-while-revalidate cache behind every record list and field-metadata read. This is the cache that actually makes a screen lie.
2. `erp_nav_state` in localStorage, so the sidebar rebuilds from the fresh doctype list rather than a remembered shape that may name retired screens.
3. The in-memory `state` caches (doctypes, field metadata, fetched rows, labels, permissions), re-fetched immediately afterwards.
4. The POS till's in-memory state, via `clearPOSTillState()` — called through `window` because `view-pos.js` is lazy-loaded (BLD-041) and may not be present at all.
5. **The lazy view modules themselves** — the registry is cleared *and* a `reset=<timestamp>` param is added to the next `import()`, because clearing the registry alone would re-fetch the identical URL straight out of the browser's HTTP cache.

**Tell users plainly that a browser reload is not a substitute.** F5 re-runs the app against the same sessionStorage and localStorage, so the stale copy is read straight back in. Reset is the only path that drops those copies before rebuilding.

**Three deliberate non-actions**, each of which has been asked about:

- **It does not sign the user out.** The token, role and username in localStorage are untouched. Resetting a screen must not eject a cashier mid-shift; that is what Sign Out is for.
- **It does not reset the theme.** A personal preference is not stale data.
- **It refuses to run while offline POS sales are queued.** `erp_pos_offline_queue` holds *completed sales that have not reached the server*. They are not cache, and clearing them would destroy them, so Reset blocks with a message naming the count rather than proceeding — the same posture `closePOSSessionFlow` already takes. Let them sync, then Reset.

> **Deploy note.** Lazy view modules are versioned by `VIEW_MODULE_VERSION` in `public/app.js`, which must be kept **in step with `index.html`'s `app.js?v=N`** on every release. Before Stage 53.17 the lazy modules had no cache-busting at all, so the shell could update while the screens a user actually looks at kept running the previous deploy's code. If you bump one, bump the other.

#### B.3.0.1 Setting up POS offers

Offers are ordinary records, not a code change: create an **Offer** (Super Admin or Store Manager) and it applies at every till on the next sale — there is nothing to deploy, restart, or push to the POS.

Fill in the name, pick the **Offer Type**, set **Applies To**, and set **Status** to Active. Only the fields belonging to your chosen type matter; leave the rest blank.

| Offer Type | Fill in | Means |
|---|---|---|
| Percentage Off | Discount % | Takes that % off whatever the offer applies to |
| Flat Off | Discount Amount | Takes a fixed rupee amount off (never more than the thing is worth) |
| Buy X Get Y | Buy Qty, Free Qty | "Buy 2 get 1 free". The **cheapest** qualifying units are the free ones |
| Bundle Price | Bundle Qty, Bundle Price | "Any 3 for ₹999". The **most expensive** qualifying units are bundled first |

**Applies To** decides which part of the sale the offer works on, and the **Scope Value** box changes to match as soon as you pick it:

| Applies To | Scope Value | The offer covers |
|---|---|---|
| Bill | (not used) | The whole sale |
| Item | Pick the item | That one SKU |
| Category | Pick from the categories already on your items | Every item whose **Category** is that value |
| Product Group | Pick a PIM **Product Group** | Every product in the group. A *Dynamic* group follows its rules, so products joining or leaving the group join or leave the offer automatically |
| SKU List | Type the SKU codes, separated by commas or one per line | Exactly those SKUs |

Set up a Product Group first under **PIM → Product Groups** if you want to target one. If a group is later deactivated or deleted, offers pointing at it simply stop applying (they never spread to the whole bill).

Everything else is an optional condition, and they all have to be true at once:

- **Minimum Bill Amount / Minimum Qty** — the "spend ₹2000 and get…" family.
- **Coupon Code** — leave blank and the offer applies automatically; fill it in and it only applies when the cashier types that code.
- **Customer Tier** — restricts the offer to one loyalty tier. Note a walk-in with no customer on the sale has no tier, so these never apply to anonymous sales.
- **Valid From / Valid To** — leave either blank for open-ended.
- **Maximum Discount Cap** — the safety net on a percentage offer ("20% off, up to ₹500").
- **Priority** — lower numbers are considered first.
- **Stackable** — *Yes* lets later offers apply on top; *No* means once this one applies, nothing after it does. Use **No** for a headline offer you don't want combined with anything else.

Three behaviours worth knowing before you design a promotion:

- **A non-stackable offer stops the stack.** Combined with Priority, that's how you say "this offer instead of the others, not as well as."
- **A sale can never go below zero.** If offers add up to more than the bill, the discount is capped at the bill.
- **To switch an offer off, set Status to Inactive** — it stops applying immediately, and you keep the record and its history. Don't delete it.


**Issued vouchers at the till** (Stage 73.2.10). A **Voucher** (CRM → Voucher, or a bulk CSV of codes) is a code you hand out: *Flat* or *Percentage*, with **Max Uses**, an **Expiry Date**, and optionally **Restricted to Customer**, **Valid From** and **Where it can be used** (*Any*, *POS* or *Online* — blank is Any). A cashier types it into the till's **Coupon code** box; a code no Offer claims is looked up as a voucher, applied after the offers (one voucher per bill), shared across the lines like an offer so it reaches the GST and any later return the same way, and used up only when the sale posts. The panel tells the cashier why a voucher was refused. Max Uses is enforced under a row lock, so the last use cannot be spent twice.

#### B.3.0.3 Database Schema Design — adding, editing and removing fields

**Settings → Database Schema Design.** Hover a module on the left, pick a record type, and you get its field list.

- **Add Field** opens a form: technical field name, display label, field type (Data / Number / Select / Check / Date / Link), whether it is mandatory, its options, and its position in the form. New fields land after everything already defined, so several added in a row do not collide on one position.
- **Edit** on any row opens the same form, pre-filled. Everything is editable, **including the field name**. Before this existed the only way to correct a mistake was Delete-then-Add — which throws away that field's stored value on every existing record. Use Edit.
- **Delete** removes the field from the form. As with a rename, the stored data is not erased.

**What renaming a field actually does.** The field list is a *view* over each record's stored data, not the data itself. Renaming changes which key the form reads, so values saved under the old name stop being displayed — they are not deleted, and renaming back restores them. The form warns you before you do it. This is worth knowing when a rename looks like it wiped a column: it did not.

**Two things are refused, with the reason named:** a field type the form cannot render, and renaming a field onto a name another field on that record type already uses.

**Fields the system fills in itself are shown read-only** — auto-generated document numbers, and derived companions such as **Phone Country** (which the phone engine resolves from the number itself). They display "Set automatically on save" rather than inviting a value that would be overwritten.

#### B.3.1 Status Transition Rules — how "strict" works

Out of the box, 64 record types are **strict**: for those, a status change is refused unless a rule explicitly permits it, and the error tells the user which statuses they *can* move to. Everything else is permissive — any status change is allowed unless a rule explicitly forbids it.

This matters when you edit these rules:

- **On a strict record type, deleting a rule takes the transition away.** If someone reports "I can't move this invoice to Paid any more", the first thing to check is whether its rule row still exists and is Active.
- **Adding a record type to the strict set is not a checkbox in the UI** — it's the `strict_status_transitions` flag on the record type. Ask a developer; and seed its full matrix *first*, or you will freeze existing documents in whatever status they're already in.
- **Nothing gets permanently stuck.** A document sitting in a status the record type doesn't even define (older data from before that type had a lifecycle) can always be moved back to a valid status.
- **To turn enforcement off entirely in an emergency**, one database statement does it without deleting any of your rules: `UPDATE tenant_default.doctype_meta SET strict_status_transitions = FALSE;`

Some deliberate examples of what the shipped rules block: a vendor invoice can't go straight from Draft to Paid (that would skip the 3-way match), a disposed asset can't be re-capitalised, and an approved goods receipt whose stock has already posted can only be cancelled with a reason code.

Two shipped judgement calls you may want to change: an **approved Leave** and a **selected Vendor Quote** are both treated as final. If your process needs to reverse either, add the row — it's one entry on this screen, no code change.

#### B.3.2 Prefix Configurations — the number series behind every transaction

Nobody types a document number in this system. Purchase Orders, Goods Receipts, Purchase Returns (`PRT`, and the Debit Notes they raise, `DN`), Fixed Assets raised from a goods receipt (`AST`), ASNs, RFQs, Vendor Quotes, Stock Transfers, Expense Claims, Leave, Employee Loans, Grievances, Production Orders and Attendance all draw their number from a **series** defined on this screen, at the moment the document is saved. The user sees a greyed-out box reading "Auto (PO series)" until then.

Each row is one series, and each transaction has its own: PO, GRN and Transfer numbers can all look different. The table shows the prefix, length, suffix, how often it restarts, whether the store is in the number, how many stores have their own prefix, and a **Next number looks like** example. **Edit** opens one form with every setting and a live preview:

You can also get here from the record itself: a **New** form for a numbered record type shows **Numbering for this record type** under its code, which opens this screen on that type's series with its next number previewed.

| Setting | What it does |
|---|---|
| **Prefix** | The leading text. `PO`, `GRN`, `TRF` — whatever your business calls it. Letters, digits, `-`, `_` and `.`; no spaces. |
| **Separator** | What goes between the parts. `/` by default; `-` is common. Up to 3 characters. |
| **Number length (digits)** | How many digits the running number is padded to. `5` gives `00042`. Shipped lengths: purchase series (PR, RFQ, Vendor Quote, PO, ASN, GRN, Purchase Return, Debit Note) and HR series (Leave, Attendance, Expense, Loan, Grievance, Payslip) **5**; Transfers, Sales Invoice, Production Order and Fixed Asset **6**. |
| **Suffix (optional)** | Text after the number: `PO/BDR/26-27/00001/A`. |
| **Restart numbering** | **Every financial year** → `PO/BDR/26-27/00001`. **Every month** → `PO/BDR/26-27-07/00001`. **Never** → `PO/BDR/00001`, one continuous series. |
| **Store in the number** | **Yes** prints the store's **Short Code** and numbers each store separately. **No** removes it and runs one series shared by every store. |
| **Status** | **Inactive** stops that transaction from being saved at all (`ADMINC-0030`). |
| **Store-wise prefix and suffix** | Every store is listed here as soon as it is created. Leave a store blank to use the series' own prefix and suffix, or give it its own: e.g. Bandra's POs as `BPO/BDR/26-27/00001`. |

**Which store text appears in the number.** The store's **Short Code** (Setup → Location), so a Bandra store with short code `BDR` prints `PO/BDR/...`. A store with no short code prints its Store Code. Short codes must be unique: the Location form refuses one that another store already uses as its short code or store code (`MASTER-0312`), because two stores printing the same segment would hand out the same number.

Things to understand before you change one:

- **Restart decides two things at once, on purpose.** It sets how often the counter restarts *and* whether the number shows the period. There's no way to restart yearly while hiding the year, because that would re-issue last year's numbers — and since the number is also the document's identifier, the second document would be refused. **Never** is the only way to get a number with no year in it.
- **Store in the number works the same way.** Turning it off doesn't just hide the store; it merges the per-store counters into one. Otherwise two stores would both be handed `PO/00001`.
- **Changes apply to the next document only.** Existing documents keep the numbers they were issued. Changing a prefix, length, suffix or short code does not renumber history, and the running counter carries on (it is kept per Store Code), so no number is ever issued twice.
- **Gaps in a series are normal.** A number is drawn before the document is fully validated, so a rejected save leaves a gap. That is standard behaviour for a sequence counter and is not data loss.
- **Two series cannot share a prefix.** Document numbers are unique across *every* record type, so the screen refuses a prefix (or a store-wise prefix) that another series already prints (`ADMINC-0310`).

##### Adding a series for a record type that isn't listed yet (New Series)

**Master record codes are short, and set up the first time (Stage 76.5).** A master record — Vendor, Customer, Item, Employee, Category, a design and the rest — is numbered `V00001`, `V00002` …: a prefix and a zero-padded number, with no store, no year and no separator, never restarting. The **first** time anyone creates a record of a kind, its New form (and the inline **+ New** dialog) shows **"… code format (asked once)"** with a suggested prefix (`V` vendors, `C` customers, `I` items, `D` designs, `LE` legal entities, otherwise the name's initials — never a prefix another series already prints) and **Digits** (3–8, default 5). Saving that first record confirms the format; from then on every record of that kind takes the next number and the question is gone. A CSV import or API call that arrives first gets the suggested format. To change a confirmed format later, edit its row here — it applies to the next record only.

- The server numbers every master itself, for every role: the forms no longer call the admin-only number endpoint, so a Store Manager can create a vendor.
- A save that is refused gives its number back; an import row is numbered only after it passes validation. A number already used by any record is skipped, never overwritten.
- **Currency, UOM and HSN Code** are not numbered: their code *is* the value (`INR`, `PCS`, `7113`) and is typed on the form.
- Codes issued before Stage 76 keep their long shape (`Vendor/HQ/2026/000006`) until they are re-coded (Stage 76.6, planned).

For a record type that has no row here yet, use **New Series** (the plus-icon button) at the top of the screen:

1. Click **New Series**. The same form opens, with the **Transaction** box editable.
2. **Transaction** — type the record type's name **exactly as the system spells it**: `Vendor`, `Item`, `Customer`, `PurchaseOrder`. This is the internal doctype name, not the friendly label, and it is case-sensitive. If you get it wrong you create a series that nothing will ever draw from — harmless, but it does nothing. Check the spelling on the **Database Schema Design** screen if you are unsure.
3. Fill in the rest of the form as for **Edit** and save. If that record type already has a series, saving updates it — there is only ever one series per record type.

One caution: **it applies to the next record only.** Adding a series for a record type that has been auto-numbering for months does **not** renumber the existing ones, and the counter starts from 1 — so if you choose a new prefix, choose one that cannot collide with what has already been issued. If in doubt, keep the default prefix (the record type's own name, which is what the engine was already using).

#### B.3.2.1 Choosing the industry, and changing it later

**Settings → Configuration → Industry** (the first entry in the list on the left). Only the org's admin sees it — Configuration is admin-only, and other roles never see the industry at all. Choosing an industry profile (Jewelry, Food & Beverage, Clothing, Pharmaceuticals and the rest) overlays that industry's extra fields onto the standard record types — see §B.3.0.3 and User Guide §8d. It is a wide-reaching change: it rewrites field definitions across the system and replaces the screen labels with the industry's own.

The panel shows the **current** industry, **who** set it and **when**, and how many times it has been changed since. Below it, every industry is listed by name with what it covers (for example *Fashion and fine jewellery; stones, precious metals, plating, workmanship*).

**Setting it the first time:** click an industry. The panel says what will happen — its fields are added to Item and the setup lists, the labels are replaced, its common HSN codes are added, and nothing that already exists changes. Press **Set industry to …** and confirm.

**Once set, the profile is locked**: changing it needs a written reason. The lock is real server-side state, not a browser setting — clearing your browser, using another machine, or signing in as a different admin will not unlock it.

**Choosing the industry also loads its HSN codes (Stage 66).** The industry's common HSN/SAC codes are added to **Setup → Core → HSN Code**, each with a description and, where the code has a single GST rate, that rate. For example, a jewellery client gets 7113 silver and gold jewellery, 7117 imitation jewellery, coins, bullion and diamonds; a clothing client gets fabrics, garments, made-ups and footwear. Codes already on the list are left exactly as they were, so a description or rate you set is never overwritten. An override to another industry adds that industry's codes too and removes none. From then on, the **HSN Code** box on every Item suggests codes ranked for the item's name and your industry (User Guide §8.1.2). Without an industry the suggestions still work from the item name, but the *Common in your industry* group is missing. Set the industry early.

**The HSN rates you set win.** If a code on your HSN Code list carries a **Default GST Rate**, that rate is what the item picker fills, and its button reads *Set on your HSN list*. The system's own rates (GST from 22 September 2025) appear beside it. A list that still holds an old rate (a pre-September-2025 12%, say) therefore keeps suggesting it until you correct it on the list. The full list the system suggests from ships with the system (`engines/hsn_master.json`, about 290 codes across the ten industries). It is suggestions only: an Item can carry any valid 4, 6 or 8-digit code.

**To change it:**

1. In **Settings → Configuration → Industry**, click the industry you are moving to. The panel says what the change does: the new industry's fields are added, the labels are replaced, its HSN codes are added — and **nothing is deleted**: the current industry's fields and every record stay.
2. **Enter the reason for the change.** It is **mandatory** — the button refuses without one. Write why the profile is genuinely changing ("client confirmed they sell apparel, not jewellery"), because this text is what an auditor reads later.
3. Press **Change industry to …** and confirm.

The system then records **who** changed it, **when**, **why**, and increments the change counter shown on the panel.

Notes:

- **Only a Super Admin can choose or change the profile.** The lock is an extra barrier on top of that permission, not a replacement for it. Other roles see neither the panel nor the industry, and the server refuses the change if one tries it anyway.
- **(Until Stage 82 the industry was a dropdown in every user's top bar with an Override button that asked for a typed code. Both are gone.)**
- **The counter is never reset.** Repeated overrides accumulate, so a profile that has been changed five times cannot later be presented as having been set once.
- **An override is not a rollback.** Switching profiles overlays the new industry's fields; it does not remove data already captured in the old industry's fields. Treat it as a forward change, and expect to tidy up now-unused fields on **Database Schema Design** afterwards.
- **Decide the profile before go-live if you can.** The lock exists precisely because this is not a setting to experiment with on a tenant that already holds real catalog data.

#### Competitor undercut alerts

If your buyers record competitor prices (User Guide §8a), the system can tell them when one of your SKUs is being materially undercut, instead of waiting for someone to run the report.

It is **off by default** and takes two pieces of setup:

1. **Set the threshold.** Admin → Settings → Platform → **Competitor undercut alert threshold** (`market.undercut_threshold_pct`). It is `0` on a fresh system, which means "never alert" — a business that doesn't track competitor prices is never bothered by this. Set it to the percentage gap you actually care about; `10` means "tell me when someone is 10% or more below what we last sold it for". The maximum accepted is `90`, because a competitor apparently selling at 95% off is a typo in the imported data, not a pricing signal.
2. **Author the message.** Setup → Advanced → **Notification Template**, with **Event** set to `Competitor Undercut`. This uses the same templating as every other notification — `{{sku}}`, `{{our_price}}`, `{{competitor_price}}`, `{{undercut_pct}}`, `{{platform}}` and `{{source_url}}` are substituted into the body. Delivery goes through the matching **Notification Channel Config** webhook, exactly like order and return notifications.

Points worth knowing:

- **A given observation alerts at most once.** The check runs hourly but only looks at competitor prices *recorded since the previous run*, so a competitor who stays cheap for a month produces one alert, not seven hundred. Re-import the same SKU at a new price and you'll get a fresh alert, which is the intent.
- **It never changes a price.** The worker only reads and notifies. Repricing stays a human decision — there is no code path in it that writes to a document or a setting.
- **No template configured means no alert is sent, but the attempt is still logged.** Look in **Notification Log** for `Skipped-NoTemplate` rows if you expected an alert and got nothing — that is the usual cause.
- **A SKU with no sales history is never alerted on.** The comparison needs a price of ours to compare against, and this system takes that from what you actually sold at (see the note in User Guide §8a). Items never sold appear in the report as "No price on file" and are skipped by the alert.

#### Tax-exempt, nil-rated and zero-rated goods

Most items are taxable and need a GST Rate above 0 — the system rejects a bare 0, because it cannot tell that apart from a rate nobody filled in. Items that genuinely carry no GST (fresh produce, unbranded grain, salt, books, exports) declare it instead, on the **Tax Treatment** field of the Item master: **Taxable**, **Exempt**, **Nil-Rated** or **Zero-Rated**. Users see this in User Guide §9 Step 3; what you need to know as an administrator:

- **Nothing has to be switched on.** The field appears on every Item form already, and leaving it blank means Taxable — every item that existed before this feature keeps behaving exactly as it did, including still being required to carry a positive GST Rate.
- **HSN Code stays mandatory on all four treatments.** It belongs on the invoice whatever the rate is, and GSTR-1 reports the nil/exempt table HSN-wise too.
- **Three GL accounts appear in the Chart of Accounts**: `4110` Exempt Sales Revenue, `4111` Nil-Rated Sales Revenue, `4112` Zero-Rated Sales Revenue. A sale of non-taxable goods still credits full revenue to `4100`, then moves that portion into whichever of the three applies. A tenant that never sells such goods never posts to them and will see them sitting at zero — that is expected, not a setup error.
- **Why they exist:** the **GST Return Summary** report reads `4100`'s balance as the period's *taxable* value. Without the reclass, exempt turnover would sit in `4100` and be reported as taxable — overstating GSTR-3B 3.1(a) and understating 3.1(c). The report now also shows Exempt / Nil-Rated / Zero-Rated / Total Non-Taxable, which is the split GSTR-1's nil-and-exempt table and GSTR-3B 3.1(b)/(c) are filed from. Those four figures are hidden on the screen when they are all zero.
- **Exempt and Nil-Rated are deliberately not merged**, and neither is Zero-Rated, because the returns report them in different boxes. If you are reconciling by hand, do not add them together before filing.
- **Zero-Rated here means the LUT/bond route** — exports supplied *without* payment of tax. The export-with-payment-then-claim-refund route is not modelled; the refund claim would need a workflow this system does not have.

#### Purchase Returns (return to vendor) — setup and accounting

**Procurement → Purchase Return** (User Guide §6.5) sends goods back against the GRN they arrived on. Nothing has to be configured for it to work. These are the settings you may want to change, and what posting a return does to the books.

1. **Who can use it.** The `PurchaseReturn` record type ships with full rights for HR/Admin and read/create/update for Store Manager. Role templates that grant the Procurement module cover it the same way they cover Goods Receipt. Change it under **Settings → Roles**. Posting a return needs *update* rights on `PurchaseReturn`. Location-scoped users only see and post returns for their own location.
2. **Requiring approval (optional).** To make returns go through maker-checker, open **Settings → Approval Rules** and add a rule for `PurchaseReturn`. The amount slab is matched against the return's value before GST (`total_amount`). From then on a Draft return shows **Submit for approval** instead of **Post**. Someone other than the submitter approves it in **Approvals**, and only an Approved return can be posted. Delete the rule to go back to direct posting.
3. **Numbering.** Returns draw from the `PRT` series and the Debit Notes they raise from the `DN` series (B.3.2). Both are shipped per tenant by `migrations_stage57_15_purchase_return.sql` and can be edited like any other series. A Debit Note entered by hand still takes the note number you type.
4. **What the stock does.** Accepted units leave *available* at the GRN's receiving location through the same floor-checked posting as every outbound movement. If they have already been sold, the post is refused. Units QC set aside at receipt (rejected → QC hold, damaged → damaged) leave those buckets instead. Every movement is in the stock ledger with voucher type `PurchaseReturn`. The item's moving-average cost has the returned receipt taken back out. The return does **not** touch WMS bin quantities: if the stock was put away into bins, adjust the bin as you would for any other non-WMS outbound movement.
5. **What the books do.** A Goods Receipt books Dr `1200` Inventory / Cr `2100` GRN Suspense at the PO's ex-GST rate for the accepted units. Posting a return reverses exactly that for the units returned, in two entries:
   - the return posts Dr `5150` Purchase Returns / Cr `1200` Inventory;
   - its Debit Note posts Dr `2100` / Cr `5150` through the same Post a hand-made Debit Note uses.

   `5150` nets to zero, and both what you hold and what you owe drop by the returned value. Live example: 8 accepted at ₹100 booked ₹800. Returning 3 left `2100` at Cr ₹500 and `1200` at Dr ₹500. Rejected or damaged units were never booked as owed, so returning them posts no GL entry and raises no Debit Note.
6. **The supplier's bill.** The Vendor Invoice three-way match values a GRN at its accepted units **less units on posted returns**. A bill for the units actually kept matches, and one still charging for returned units goes to MismatchHold. If the bill was paid before the return, `2100` is left in debit by the returned goods' value **including GST** (item 8): the supplier owes you that amount.
7. **If the Debit Note cannot be posted** (for example its accounting period is closed), the return still posts, because the goods have physically gone back. The Debit Note is left as a Draft, and the poster is told `PURCHA-0120` with the note number. Post it from **Financial Accounting → Debit / Credit Notes** once the cause is fixed.
8. **GST on the returned goods is reversed automatically** (since 2026-10-10). Input credit is booked to `1500` GST Input Credit when a supplier's bill is paid. When a return posts, the system checks whether the returned units were already on a bill: the GRN's Matched, Approved, Pending Approval or Paid bills now come to more than it is owed for the units kept.
   - **If they were billed,** the GST share of those units is reversed in the same posting: Dr `2100` / Cr `1500`, at the bill's own GST share (`gst_amount ÷ invoice_amount`), capped at the over-billed amount.
   - **If they were returned before any bill,** nothing is reversed. The supplier bills only the kept units, so their GST was never claimed.
   - **Not reversed:** rejected or damaged units, which were never billed, and a GRN billed in a foreign currency, whose import IGST is paid at customs.
   - **Billed but not yet paid:** the reversal posts at the return, the later payment claims the full credit, and `1500` nets to the right figure.

   Live example: a bill of ₹1,180 (₹180 GST) for 10 units at ₹100 was paid. Returning 3 reversed ₹54, leaving `1500` at ₹126 (GST on the 7 kept) and `2100` in debit by ₹354, the supplier's tax-inclusive refund. The Debit Note's own entry stays at the value before GST (₹300). The note also records `gst_amount` (₹54) and `total_with_gst` (₹354), its **Reason** ends with "GST 54.00 reversed; total with GST 354.00" (visible in Debit / Credit Notes), and the poster sees the reversed amount in the confirmation.

   **GST Return Summary:** Input Tax Credit is `1500`'s net for the period, so it falls by the reversal in the return's posting period, and Net Tax Payable rises by the same amount. There is no separate line to file by hand.

Error codes: `PURCHA-0116` (no reason), `PURCHA-0117` (more than can be returned, or a line the GRN never received), `PURCHA-0118` (no GRN, or a cancelled one whose stock never posted), `PURCHA-0119` (already on another return, or already posted), `PURCHA-0120` (Debit Note raised but not posted).

#### Item Types — what kind of item a SKU is (Stage 60.3/60.4)

Every Item has an **Item Type** (Setup → Item → Item Type). **Product** is the default: an Item saved with the field blank, or with the older value "Stock", is a Product. Item types are a list you manage under **Setup → Item Type**; seven ship with every client:

| Type | Barcode | Behaviour |
|---|---|---|
| **Product** | Client default | A normal stock item, bought and sold. |
| **Accessory** | Fixed per SKU | Sold alongside a main product; otherwise a normal stock item. |
| **Non-Asset** | Fixed per SKU | A stock item that is not a fixed asset (consumables, packing material). |
| **Free With Item** | Fixed per SKU | **Sells at zero price**, and only alongside its parent item: set **Free with item** on the Item to the main product. The till refuses the free item unless the parent is on the same bill in at least the same quantity (`POSOFF-0314`), and the Item form refuses a Free With Item SKU with no parent, or with itself as parent (`MASTER-0315`). |
| **Gift** | Fixed per SKU | **Sells at zero price** at the till. The line shows "Free (gift / free with item)" as its price source and does not count as a discount. |
| **Backflush** | Fixed per SKU | In production it is **not** issued at Material Issue. Each time finished goods are reported (partial or full completion), the BOM quantity × units finished is taken from stock at the order's location in the same posting as the finished goods. Short stock refuses the completion (`MANUFA-0143`). |
| **Fixed Asset** | Off | Bought on a PO and raised as an Asset, never sold (below). |

Each type is a record with these settings, so you can change the shipped ones or add your own: **Generate barcode at**, **Barcode per receipt date**, **Sell at zero price**, **Only with its parent item**, **Backflush**, **Fixed asset**, and **Status**. An Item picks its behaviour up from its type; only the two barcode settings can be overridden per Item. Zero price applies to the till's server-side pricing (POS quote and checkout); it cannot be overridden by a price list or a supervisor override.

**Since Stage 72 an item type is a full profile** with eight independent dimensions (commercial class, stock tracking, valuation, accounting, sellability, customer bill, POS capture, consumption) that every Item may override, six more shipped types, and Gift / Free With Item are now *Promo only* — see **§B.3.8**. The four flags above still work: where a type leaves a dimension blank, its flags decide it.

#### Fixed Asset items — bought on a PO, never sold (Stage 57.8)

An Item whose **Item Type** is **Fixed Asset** is equipment the business keeps, not stock it sells. User Guide §6.7 has the day-to-day steps. What an administrator should know:

1. **Turning an Item into a fixed asset** is the **Item Type** field on the Item (Setup → Item; blank means Product, so every existing Item is unchanged). Change it before the item is received. Stock already on hand from earlier receipts stays as stock, and that stock still cannot be sold once the type is Fixed Asset.
2. **Receiving.** When its GRN posts, the accepted units do not enter stock and nothing is booked to `1200` Inventory. Each unit becomes a **Draft** asset in HRM → Fixed Assets, numbered from the `AST` series (B.3.2; the location is the store segment), costed at the PO's rate before GST, and carrying the Item, GRN, PO, vendor, location and serial number. Units QC rejected or damaged go to QC hold / damaged as for any item. If an asset cannot be raised, the receipt still posts and an ERROR naming the GRN is written to the system error log, so the asset can be entered by hand.
3. **Accounting.** Capitalise books Dr `1400` Fixed Assets / Cr `2100` GRN Suspense, the same entry a hand-entered asset has always made. The supplier's bill is matched and paid against the GRN as usual, and paying it clears `2100`. Capitalise asks for the useful life when the asset has none, and stores it on the asset.
4. **Never sellable.** One check refuses a Fixed Asset item on every sale path: the server's sale-side tax computation, which POS quote and checkout, order and pack invoices and customer returns all use, and OMS order intake, both when an order is created and when a hold is released. The refusal is `ASSET-0273`. Picking needs an order line and stock, and a Fixed Asset item has neither.

#### GST on purchases (Input Tax Credit) and on credit sales

- **Supplier bills.** When a Vendor Invoice is matched, the system works out how much of the bill is GST, using the GST on the Purchase Order it matches (CGST/SGST or IGST in the PO's proportions), and stores it on the invoice. Paying the invoice posts the goods value to `2100` GRN Suspense and the GST to `1500` GST Input Credit Account. That holds for Pay, Pay w/ TDS and an approved Override & Pay alike. A foreign-currency bill, or one matched against a PO without GST, posts the whole amount to `2100` as before.
- **RFQ award policy still needs a product decision.** The current Vendor Quote captures one `quoted_price` and lead time, but no unit, item line, tax basis or delivery terms. Selecting the winning quote records the comparison result; it must not automatically price or issue a PO. Stage 55.4 now links existing source documents in the UI, while quote-to-PO field semantics and approval policy remain open. Configure and review a PO explicitly; do not treat the linked records alone as three-way-match acceptance.
- **Procurement handoff (Stage 55.4 continuation):** an approved PO's **Receive goods** action carries that PO into GRN; the posted GRN's **Record vendor invoice** action carries its GRN/PO/vendor into the generic Draft invoice form. Operators still enter the supplier invoice number, amount and financial year and run **Match** before paying. A local partial receipt of one unit at ₹100 plus 3% GST matched a ₹103 invoice; a second ₹110 bill on that same GRN was held as overbilling. Rejected/damaged replacement versus PO-fulfillment accounting remains a product decision; do not change tolerance/closure policy on an assumption.
- **TDS** on a bill whose GST is recorded is deducted on the value **excluding** GST (CBDT Circular 23/2017). The supplier still receives the full bill less TDS.
- **Credit and online sales.** A Sales Invoice drafted for a shipped order now carries its CGST/SGST/IGST, worked out the same way package invoices are. Posting it books the tax to `2200`/`2201`/`2202` GST Output Payable and only the rest to `4100` Sales Revenue, the same split a POS sale makes. Before this, credit sales put their whole value in revenue, so their GST never reached the GST return.
- **GST Return Summary** shows **Input Tax Credit** (the period's movement on `1500`, which includes GST on expense claims) and **Net GST Payable** (output tax minus input credit; a negative figure is credit carried forward).
- These follow standard Indian GST treatment. Have your CA confirm eligibility rules (blocked credits under Section 17(5), reversals) for your business; the system records all input GST as claimable.

#### Currency and exchange-rate setup (Stage 37 foundation)

After `db/migrations_stage37_1_currency_foundation.sql` is applied, two Finance Masters appear under Setup: **Currency** and **Exchange Rate**. INR is seeded because it was already the application's implicit functional currency; add only currencies the tenant actually trades in.

For each rate, set **From Currency**, **To Currency**, a positive Rate, the Rate Type (**Spot**, **Average**, or **Closing**), and its effective dates. Leave **Effective To** blank for an open-ended rate. Manual rates may have an optional note; an **Imported** rate must carry a Source Reference such as the provider and batch/date so it remains auditable. Generic Bulk Import can load repeated rate feeds without a separate importer.

The shared resolver chooses the most recently effective active rate whose date window includes the transaction date. If only the reverse pair exists, it safely uses the inverse; if neither exists it returns `FIN-0021` instead of inventing a value. Currency code and rate-window uniqueness are also enforced in the database.

**One rule that will otherwise cost you an afternoon: a Currency's ID must be its ISO code.** Create the US dollar with ID `USD`, not `CUR-USD` or an auto-generated value. Every document that selects a currency links to it by ID, so any other ID makes that currency unusable on invoices and orders. The system now refuses a mismatched ID at save time and tells you which one to use, but if you are importing currencies in bulk, set the ID column to the code.

#### Foreign-currency invoices, FX gain/loss, and period-end revaluation (Stage 37.1.3-37.1.5)

Set the tenant's reporting currency under **Configuration → Finance → Functional (reporting) currency** (default INR). Every ledger posting, report and approval threshold is expressed in it. Documents may be transacted in other currencies and are converted to it at their exchange rate. **Changing it does not re-translate existing postings** — it declares what they were always in — so treat it as a provisioning-time decision.

**Raising a foreign-currency invoice.** Set **Currency** on the invoice and leave **Exchange Rate** blank to have the rate resolved from your rate table *on the invoice's own date* (not today's — a back-dated invoice does not quietly pick up this morning's rate). Enter a rate explicitly to override it, which is what you want when a contract fixes the rate. The invoice keeps the amount you agreed in the foreign currency; the ledger holds its value in your functional currency.

**Settling one.** Post the invoice as usual. On settlement you may optionally supply:

- **Settlement date** — the date the money actually moved. Use it when a receipt is entered a few days late, so the rate quoted is the one that applied on the day, not today.
- **Exchange rate** — the rate the bank actually gave you. A bank confirmation beats any rate table; without it the difference just hides in your cash account.

The system books the cash at the settlement rate, clears the receivable at what the ledger was carrying, and posts the difference as a **realised** gain or loss. Four accounts are used and they are deliberately kept apart:

| Account | Name | Used for |
|---|---|---|
| 4200 | Realised FX Gain | Cash actually received/paid was worth more than carried |
| 5600 | Realised FX Loss | …worth less than carried |
| 4210 | Unrealised FX Gain | A still-open balance revalued upward at a period end |
| 5610 | Unrealised FX Loss | …revalued downward |

Realised means cash has moved and the result can never reverse. Unrealised is an opinion about an open balance that may well reverse next month. Never merge the two into one account — auditors and tax authorities treat them differently and the split cannot be recovered afterwards.

**Running a period-end revaluation.** Open foreign-currency receivables and payables are still carried at the rate they were booked at, which goes stale. To restate them:

1. **Preview first.** `GET /api/v1/finance/fx-revaluation?as_of=YYYY-MM-DD&rate_type=Closing` returns exactly what would be posted, per document, and **writes nothing**. Check the totals before committing.
2. **Commit.** `POST` the same endpoint with `{"as_of": "YYYY-MM-DD", "rate_type": "Closing"}`.

The rate type defaults to **Configuration → Finance → FX revaluation rate type** (Closing, which is what accounting standards name for balance-sheet items). Only genuinely booked balances are revalued — Approved sales invoices and Matched/Pending-Approval vendor invoices. Purchase orders and sales orders are *commitments*, not balances, and are deliberately left alone.

Running the same date twice is safe: the second run reports each document as *already revalued on that date* and posts nothing. A revaluation dated into a closed accounting period is refused, like any other posting.

**Reporting.** Three reports appear in the report catalog under Finance, with the usual CSV/PDF export and scheduling:

- **Open FX Exposure** — every open foreign balance and what it is worth at today's rate. This is the daily treasury view, and it is the same calculation the revaluation preview runs.
- **FX Gain/Loss Register** — every posting to the four accounts above, filterable to realised or unrealised. Use it to answer "where did this FX figure on the P&L come from".
- **Trial Balance in Presentation Currency** — your ledger restated into another currency at a chosen rate type.

⚠️ **The presentation trial balance applies one rate to every line.** A full IAS 21 translation uses the closing rate for balance-sheet items, the average for P&L items and historical rates for equity. This is an honest single-rate convenience translation — good for "roughly what does this look like in dollars", not a statutory consolidation. Every row carries the rate type used so it cannot be mistaken for one.

#### B.3.3 Turning on batch / lot traceability (Stage 42.1)

Batch tracking is **off for every item until you switch it on for that item**. Nothing changes for a tenant that never touches it — pick lists, receipts and stock figures behave exactly as before. Switch it on only for items where the lot genuinely matters: food, pharma, cosmetics, chemicals, anything with a shelf life or a recall obligation.

**Step 1 — configure the item.** Under **Setup → Items**, four fields appear on each item:

| Field | What it does |
| --- | --- |
| **Traceability** | `None` (default), `Batch`, `Serial`, or `Batch and Serial`. Only the two Batch modes have any effect today — `Serial` is accepted and stored, but the serial register itself is a later item (42.1.8). |
| **Shelf Life (days)** | Used to *derive* an expiry when a receipt gives a manufacture date but no expiry. Leave blank if cartons always print an expiry. |
| **Min Shelf Life on Receipt (days)** | Refuse a delivery arriving with less than this left. Your defence against a supplier clearing short-dated stock onto you. |
| **Min Shelf Life on Pick (days)** | Never allocate stock with less than this left. **This is the important one** — it's what stops goods expiring in a customer's hands. |

Two configurations are refused when you save, because both are silently unusable rather than merely odd: a negative number, and a receipt/pick minimum longer than the item's own shelf life (no delivery could ever be accepted, or no stock could ever be allocated, and nothing on a pick list would say why).

**Step 2 — there is no step 2.** Receiving clerks are prompted for a lot number automatically from then on, batches register themselves from the receipt, and pick lists switch from FIFO to FEFO for that item. See [User Guide](USER_GUIDE.md) §6.3 for what your users will see.

**What you can do that they can't.**

- **Hold or release a lot.** The `Batch` master (Setup → Batch, or via `POST /api/v1/wms/batch/status`) carries `Active` / `Quarantined` / `Blocked` / `Expired` / `Consumed`. Anything not `Active` is invisible to allocation. **Releasing a held lot back to Active requires a reason** — it's the first question a recall audit asks. `Consumed` is terminal on purpose: a fully-shipped lot never becomes Active again, a new receipt gets a new lot.
- **Run the expiry sweep.** `POST /api/v1/wms/batch/expiry-sweep` marks every past-expiry lot `Expired` and moves its remaining good stock into the existing **QC-Hold** bucket, which takes it out of `available` through the same path a damaged-goods call uses. It is **idempotent** — running it twice quarantines nothing twice — and it returns a count of what it did rather than changing things silently. It is deliberately operator-triggered rather than a background timer: it moves stock out of sellable, so someone should be able to point at a person and a time. If you want it on a schedule, call it from the existing scheduled-job path rather than expecting the server to do it on its own.
- **Correct a batch by hand.** `Batch` is an ordinary master, so bulk CSV import, field permissions and the audit trail all work on it. Note that a lot number only has to be unique **within its item** — two suppliers both shipping "LOT-001" for different SKUs is normal and allowed.

**One limitation worth knowing before you roll this out.** Turning on batch tracking for an item that *already* has stock on the shelves does not retroactively give that stock a lot — it can't, because nobody recorded one. FEFO will then only offer the stock that does carry a lot, and report the rest as short. That is the correct refusal (issuing untraceable units is what batch tracking exists to prevent), but it means **you should either switch an item over when its stock is low, or assign lots to the existing stock first** via `POST /api/v1/wms/batch/putaway`. The **Batch Stock Inquiry** report shows you exactly what has been assigned so far.

#### B.3.3.1 Barcodes generated automatically at goods receipt

**Stock availability review (Stage 55.6 partial).** Inventory and Current Stock
now expose `available_to_sell`, calculated by the reservation engine's shared
formula from base availability minus reserved, safety, blocked, QC-hold,
damaged, channel-buffer and active-hold quantities. `available` remains the
base bucket in the API for compatibility; do not use it alone to promise stock.
The browser check placed an actual Hold against a disposable stock row and
confirmed Available to sell fell while On hand did not. A later isolated
Chromium run followed Place Hold → linked Hold Release Request Draft → Submit
for Approval → a different Store Manager's Approve and confirmed available
stock returned only after approval. A transfer run exposed and fixed source
`on_hand` not decreasing at dispatch; the regression test and a second live
transfer show one unit leaving HO and entering the destination, without a
duplicate on-hand unit. Count/recount, lot/serial identity and independent
transfer-approval policy remain open Stage 55.6 gates.

Inventory's Transfer link now opens the real transfer composer with source and
item filled, rather than the generic TransferOrder record table. Its Count
link starts a scoped blind CycleCountLine with a generated session reference;
the new-line form omits system quantity, variance and lifecycle status, and
generic create validation accepts only Draft, nonnegative whole-unit counts
with no system-owned values. A Chromium run posted a zero-variance line.
Variance/recount approval and historical balance remediation remain unverified.

**Barcode policy (Stage 57.7)** — two settings under **Configuration → Inventory**:

| Setting | Choices |
|---|---|
| **Generate barcodes at** | **SKU creation** (default): a new Item saved without a barcode gets one immediately. **Goods receipt**: it gets one the first time it is received. **Off**: barcodes are only scanned, typed, or made with the **Generate** button. |
| **New barcode for each receipt date** | **No** (default): the same barcode for a SKU every time. **Yes**: each day a SKU is received it also gets that day's own barcode, printed on that receipt's stickers. |

Barcodes belong to the **SKU** — each Item, i.e. each variant. A design / Product Family has none. **Barcode** is no longer a field anyone has to type on the Item form; one you type or scan yourself is always kept, and nothing ever replaces an existing barcode.

**Receipt-date barcodes** are EAN-13s of the form `02` + `YYMMDD` + a 4-digit number for that day + check digit (up to 9,999 per day). They are listed under **Setup → Item Barcode** with the SKU, the date and the receipt(s) that used them. Scanning one anywhere — POS, pick/pack, sticker re-print — finds its SKU. One code per SKU per day: a second receipt of the same SKU on the same day reuses that day's code.

**Per item type and per item (Stage 60.3).** The two settings above are the client-wide default. Each **Item Type** (Setup → Item Type) can override them — **Generate barcode at** (Client default / SKU creation / Goods receipt / Off) and **Barcode per receipt date** (Client default / Fixed per SKU / New per receipt date) — and each **Item** can override its type with the same two fields ("Item type default" keeps the type's rule). The most specific one wins: **Item, then Item Type, then the client setting.** The shipped Accessory, Non-Asset, Free With Item, Gift and Backflush types are **Fixed per SKU**, so those SKUs keep one barcode even when the client issues a new one per receipt date; Fixed Asset issues none (assets carry an asset tag).

Receipt-time generation of the permanent barcode (below) still runs under **SKU creation** too, as the safety net for older, imported or API-created Items.

When a goods receipt is posted, every distinct SKU on it that has **no** barcode yet is issued one, as a valid **EAN-13** (correct check digit, so ordinary retail scanners read it). An item that already has a barcode is left untouched. It is idempotent: receiving the same SKU again, or the same SKU twice on one receipt, issues exactly one barcode and never changes an existing one.

What you need to know as the administrator:

- **It runs after the receipt has already posted, deliberately.** Stock movement is never held up or reversed by barcode generation. If generation fails, the receipt still stands and the item is simply left without a barcode. That failure is written to the **system error log** (§B.4) rather than shown to the receiving clerk, who cannot act on it anyway.
- **So check the log, not the user.** A clerk will not report this. If an item is mysteriously unlabelled, look for its entry in the system log, then generate the barcode by hand from the PIM screen.
- **It does not print.** Generation and printing are separate on purpose — nothing is sent to a printer by posting a receipt. Printing is the operator's explicit second step (§B.3.4, User Guide §7A.1).
- **Barcodes you have already assigned are safe.** If you imported your own barcodes, or your supplier's, receiving will not overwrite them.
- **This closed a real gap**: before it, a newly received item had no barcode, so its label had nothing to print and someone had to remember to generate one manually first.

**GRN batch numbers (Stage 61).** A fixed barcode cannot say which delivery a piece came from, so a GRN line left without a batch number gets one when the GRN is saved — before anything posts, so the GRN, its stickers, the stock ledger and the Batch record all carry the same value. A number the clerk types is always kept. What you control:

**How often (Settings → Inventory → GRN batch numbers, then Item Type, then Item).** The same precedence as the barcode rules: the Item's **GRN batch number** field wins, then its **Item Type**'s, then the client setting. "Client default" / "Item type default" (or blank) inherit, so existing records keep the client rule.

| Value | Effect |
|---|---|
| **One per SKU on each GRN** (default) | Lines of the same SKU on one GRN share a number; a number typed on one of them covers the others. |
| **One per GRN line** | Every blank line gets its own number. |
| **One per SKU per day** | Every GRN receiving the SKU at the same location on the same day shares one number; its Batch record adds up their quantities and lists every GRN. |
| **Off** | No number is filled in. |

**What it looks like (Admin → Prefix Configurations → "GRN batch number (GRNBatchSeq)").** An ordinary number series: **Prefix**, **Store in number** (the receiving store's short code), the period, the **Number** length and **Suffix**, joined by the **Separator**. **Restart numbering** here means **Every day** (the receipt date is in the number — B-261016-0001, the default), **Every month** (B-26-10-0001) or **Never** (no date — B-0001). The Next-number column previews it. Its prefix may repeat another series' prefix, since a batch number is never a record id. **Keep it short on small labels**: on a 50 × 25 label at 203 dpi the default fits with 2-dot bars, but a longer prefix (e.g. LOT) or a 5-digit number makes the bars 1 dot wide — the Sticker Studio's scan check warns when that happens; widen the barcode or shorten the format.

What else to know:

- **Two kinds of line are never numbered.** A **batch-tracked** item (Traceability = Batch) still needs the lot printed on the carton (INVENT-0115): that is a control you switched on per item, and an invented number would hide a missed entry. A **Fixed Asset** item never enters stock.
- **Each batch is registered** under **Setup → Batch** with its **Goods Receipt**, **Received On**, **Received At** (location), **Supplier** (the PO's vendor) and the received quantity.
- **Two reports** (Report Catalog → Inventory; User Guide §6.3): **Batch Lookup & Ageing** (scan a batch, barcode or SKU → receipt date, age, age band, GRN, supplier, received and estimated left) and **Stock Ageing (by GRN batch)** (each item's stock on hand in 0-30 / 31-60 / 61-90 / 91-180 / 180+ day bands). "Left" is a **first-in-first-out estimate**: a POS sale scans the SKU's barcode and records no batch, so the item's stock on hand across all locations is laid against its newest batches first. Stock beyond everything the batches received shows as **Before batch numbers**.
- **Labels.** The built-in label prints the batch as a second Code 128 barcode with the number under it. On custom templates, drag **Batch / Lot** from the Studio's Data panel onto a barcode element (§B.3.4).
- Fixed with it: two lines of the same SKU (and lot) on one document used to write only the first line to the **stock ledger** (the second looked like a replay), so the ledger under-counted while stock on hand was right. They now write one ledger entry for the combined quantity.

#### B.3.3.2 Bulk import: Master codes and Design-based SKUs

**Stage 51.10:** on **Setup → Item → Bulk Import** there are two template buttons. **Download Template CSV** is the full Item template, unchanged, with `id` and `code`. **Download Template (SKU from Design)** has no `id` or `code` column and puts `family` first, so a user who fills it in as downloaded gets Design-based SKUs. Variation goes in `variant_option_values` (`size:S`) unless the tenant's Items carry the jewellery attribute fields. Uploading a row for a SKU that already exists updates that Item (see the last paragraph below). User Guide §8d.5 has the steps.

Two things about CSV import that are worth knowing before you run a large load, both corrected as of Stage 51.8:

- **A Master record imported with a blank `id` now takes its `code` as its id.** Every Link field in this system resolves by *id*, so a master whose id and code disagree can never be selected by any Link field — an imported Vendor that exists but which no Purchase Order can reference. Import now applies the same id = code rule the on-screen save path does. **If you imported masters before this**, audit them with `scripts/audit_master_id_code_drift.sql` (read-only, see its header for how to run it); it reports how many rows are affected per record type, whether a fix would collide with anything, and how many transactions already point at the old ids.
- **An imported Item under a Design now gets a real SKU.** Give the file a `family` column naming the Product Family (Design), and import builds the Combination ID/SKU from that design plus the variant attributes exactly as the form does. Previously this only worked on the single-record screen, so bulk-imported variants got plain sequence numbers (`Item/HQ/2026/000123`) instead of SKUs. See User Guide §8d for what the resulting codes look like.
  **You can leave the `code` column out of the file entirely** in this case — normally an Item upload with no `code` column is rejected up front, but when a `family` column is present the server is generating the code, so it is no longer demanded. That exception is deliberately this narrow: an Item file *without* a family column, and every other record type, still requires `code`.

**Re-importing a Master by code now updates it rather than duplicating it**, which follows from the first point: the code resolves to the same id, so the row is matched and updated. Keep that in mind for a repeated load — it is usually what you want, but it does mean a second upload of the same file overwrites rather than being rejected.

#### B.3.4 Sticker templates, template rules and the Sticker Studio (Stages 52, 58, 70)

By default every item's barcode label uses the built-in **Standard label** (name, variant, barcode, SKU, HSN, batch barcode). A tenant that wants items to print differently defines **StickerTemplates** and rules for which items use them — designed by the business itself in the full-screen **Sticker Studio** (Stage 58), no code change and no developer. The end-user steps are in [User Guide](USER_GUIDE.md) §7A.2; this subsection is what you need to know as the administrator.

**Nothing has to be configured.** With no `StickerTemplate` records the resolver returns "no template" and every label prints the built-in 50 × 25 mm layout — the same content as always (name, barcode, SKU, HSN). Since Stage 58 it is drawn by the same engine as designed templates, so it now also prints correctly on a TSC (TSPL) printer, which previously received Zebra commands. No tenant is forced through a design step to keep printing.

**Who can design them.** `StickerTemplate` is an ordinary Master (module key `stickers`, enabled by default for a tenant and also granted by the `pim`, `wms` and `erp_full` packages; the doctype appears under **Inventory** in the Setup flyout), so the usual permission matrix applies:

| Role | Read | Create | Edit | Delete |
| --- | --- | --- | --- | --- |
| HR/Admin | ✓ | ✓ | ✓ | ✓ |
| Store Manager | ✓ | ✓ | ✓ | — |
| Cashier | ✓ | — | — | — |

**How a label picks its template** (Stage 70, `engines.ResolveStickerTemplateForItem` / `pickStickerTemplate`), first rule that applies:

1. **Item** — `Item.sticker_template` (a Link to StickerTemplate, on the Item form and in Item imports).
2. **Item type** — `ItemType.sticker_template`, read through the item-type cache (30 s, dropped when an ItemType is saved).
3. **Category** — the item's **Category** against each Active template's comma-separated `categories` list: case-insensitive, surrounding spaces ignored, otherwise an exact string match.
4. **Default** — the template flagged **Default for categories with no template of their own** (`is_default`).
5. Otherwise the built-in **Standard label** (`DEFAULT_STICKER_TEMPLATE` in `public/sticker-engine.js`: name, variant, barcode, SKU, HSN, batch barcode).

Templates are read in record-id order, so when two templates name one category, or two carry the default flag, the result is the same every time. A rule naming an Inactive or deleted template is skipped. Users manage rules 2-4 on **Sticker Printing → Template rules** (User Guide §7A.5). That screen writes only the existing records: ItemType, the templates' `categories`, and `is_default` on one template while clearing it on the others. Each save is a read-modify-write that sends `expected_version`, so an edit made meanwhile is refused rather than overwritten. Migration `migrations_stage70_1_sticker_template_rules.sql` adds the two Link fields; they start blank, so no existing item changes what it prints.

**Chosen at print.** On a GRN / Transfer Order print each line may carry `template_id` in the `lines` array of `POST /api/v1/stickers/print` (and the QZ payload endpoint): a StickerTemplate id, or `standard` for the built-in label. It beats the rules for that print only, and `sticker_print_log.template_id` records what actually printed. A chosen template that is not Active stops the run with a 422 instead of printing another design. The manual SKU panel always follows the rules.

Three consequences worth telling your users about before they design anything:

- **`Item.category` is free text.** Nothing validates it against a template's list, so a template naming `Earings` simply never matches, silently, and those items fall to the next rule. If categories matter operationally, standardise the spelling on the Item Masters, or set the template by Item type or on the Item instead (rules 1-2 are Links, so they cannot be misspelt).
- **`Status = Inactive` is the retire switch.** An Inactive template is skipped by the resolver entirely, which is the safe way to park a layout without deleting it (and without losing the audit rows that reference it).
- **Only one template should carry the default flag.** The studio's tick does not stop two; the resolver then takes the lowest record id. The **Template rules** screen's Default dropdown keeps it to one.

**How labels are rendered (Stage 58).** Every label — designed or built-in — is drawn in the browser by `public/sticker-engine.js`, the same code the studio canvas uses, so what was designed is what prints:

- **Thermal printers (TSPL or ZPL).** Each row of labels is drawn at the printer's DPI, turned into a 1-bit bitmap and sent as one TSPL `BITMAP` or ZPL `^GFA` graphic, with the page size (`SIZE`/`GAP`, `^PW`/`^LL`), darkness, speed and 180° flip from the template's **Label** settings. Fonts therefore never depend on what is installed in the printer. Barcodes (Code 128 with automatic numeric compaction, EAN-13) and QR codes are encoded by the engine and drawn with every bar a whole number of printer dots; the studio's scan check warns below 2 dots. Identical consecutive rows go as one image with a copy count, and jobs are sent in batches of 50 rows, well inside the print agent's 32 MB message cap.
- **Anything else** (a PDF/HTML printer, or no print agent reachable on that PC): the browser print dialog opens with the labels as images at 300 dpi or more — either **roll pages** (one row of labels per page, page sized to the roll, for a thermal printer driven by its Windows driver) or an **A4 sticker sheet** grid, per the template's **Office printer / PDF** setting.
- **DPI.** Dots are computed from the **Printer record's `Printer DPI`** when it is set, else the template's own DPI, else 203. Set it on every Printer record: a 300-dpi printer treated as 203 still prints at the right size (sizes are in mm), but bars may lose their scan margin.
- **Item data.** Each label carries every scalar field of its Item (`fields` on the `/api/v1/stickers/print` response), which is what lets the studio print weight, purity, size, MRP or any custom/industry field without code changes. Values longer than 500 characters and nested JSON are not included.
- **API clients** that still call the QZ payload endpoint with `job_type: "Sticker"` get the older server-built ZPL. It prints a template's text and barcode elements (including custom text, prefix/suffix and `{field}` tokens) and skips QR, line, box and logo elements, which only the browser engine draws.

**Storage limits.** A template's layout lives in its `elements` field (one JSON array) and its settings and logos in `design` (`{v: 2, settings, assets}`). `elements` is subject to `platform.field_max_length` (10,000 characters by default): the studio stores elements compactly, warns in its status line from 90% and refuses to save over the cap with an explanation. Logos are limited to 400 KB on upload and down-scaled to 800 px; unused logos are dropped on save.

**A Printer record is required on this screen**, unlike receipts and invoices: the Sticker Printing screen refuses to print with "Select a printer first" even when it is going to fall back to the browser dialog, because the print is logged against a printer. Any one Active Printer record is enough — this screen passes the chosen printer explicitly, so it does not depend on **Default For** being set.

**Audit trail.** `sticker_print_log` keeps every run (SKU, barcode, printer, user, copies, reprint reason, timestamp) and, for a run started from a document, additionally which `source_doctype`/`source_doc_id` it came from and which `template_id` rendered it. The **Print** tab's own history table shows this; `print_job_log` separately records each silent thermal job sent through the print agent (status Submitted/Failed). A studio **Test print** writes neither — nothing real is being labelled.

**Selecting one lot of a SKU.** A document's lines are unique by SKU *and* lot, so a SKU received on two lots is two rows, each selected, counted and printed independently; the review table shows a **Batch/Lot** column whenever that applies. Over the API, `POST /api/v1/stickers/print` and the QZ payload endpoint take this as a `lines` array of `{sku, batch_no, copies}`. The older `skus` + `copies_override` pair is still accepted and still means "every lot of this SKU", so existing callers and the manual scan flow are unaffected.

**Adding a third source document.** Printing from a transaction currently supports GRN and Transfer Order. A Purchase Order or Sales Order source is a small code change, not a redesign — one more `case` in `engines.ResolveDocumentStickerLines` and one more entry in `BULK_STICKER_SOURCE_DOCTYPES` in `public/view-printing.js`. It is tracked as an open item (52.7) rather than built speculatively.

#### B.3.5 Sending events to another system (Webhook Subscriptions)

A **webhook subscription** tells this system: "whenever *this kind* of thing happens, send the details to *that* address" — so another application (an accounting package, a warehouse robot, your own dashboard) hears about a sale or a stock change the moment it happens, without polling.

**To set one up**: Setup → type "webhook" in the filter → **Webhook Subscription** → **New**.

| Field | What to put |
|---|---|
| **Name** | What it is for, e.g. "Stock changes to warehouse dashboard". |
| **Delivery URL** | The `https://` address the other system gave you. |
| **Signing Secret** | A long random string you also give to the other system. Every delivery is signed with it so they can prove it came from you. |
| **Event Pattern** | Which events to send: an exact name such as `pos.sale.completed`, or a prefix ending in `*` such as `inventory.*` for every inventory event. |

**What the other system receives**: an HTTP `POST` whose body is `{"event": "<name>", "payload": {…}}`, with headers `X-Webhook-Event` (the event name), `X-Webhook-Delivery` (a unique id, so a repeat can be ignored) and `X-Webhook-Signature: sha256=<HMAC-SHA256 of the body, keyed with your Signing Secret>`. Events in use today include `pos.sale.completed`, `inventory.stock_changed` and `pim.publish.queued`.

**How it stays reliable**: an event is written in the same database transaction as the business change that caused it (the "outbox"), so an event is never sent for a sale that was rolled back, and none is lost if delivery fails — failed deliveries are retried by the job runner. A server running with external side effects switched off (development and training instances, see the startup log line `SIMULATED (ENV=development)`) records deliveries but does not actually send them.

**OMS connector retry (Stage 55.9):** the order desk prevents two simultaneous manual syncs of the same channel operation in one browser session. A successful pull reports orders *processed*, which can include previously imported channel order IDs; it does not mean that many new SalesOrders were created. Check the order's source and channel order ID before retrying, and use the integration exception/mapping queue to fix rejected items. This UI guard is not a substitute for the server's channel-order idempotency key or for a real connector acceptance run.

**OMS local handoff (Stage 55.9 partial, 2026-10-09):** a scratch Chromium journey created a manual SalesOrder, reserved a unit, released it to a FulfillmentTask, dispatched the task, booked a carrier from the order detail, handed over a manifest, and found a Shipped order with one Draft SalesInvoice. The booking action is suppressed once an active booking exists or the order is terminal. Both notification logs said `Skipped-NoTemplate`; no delivery is claimed. The settlement form now selects saved orders from the chosen channel instead of accepting comma-separated IDs; this is not proof of payout matching. Return/refund and connector replay remain open.

**Finance work home (Stage 55.10):** **Finance / GL → Work to review** links operators to AP, AR, payment proposals, bank reconciliation and period close. It shows no first-page status counts because the generic document API paginates; use source lists and reports for actual balances and exceptions. Do not treat the advisory close checklist as an Account Head signature or qualified CA statutory acceptance. Retain source-document, posting-identity and bank-reconciliation evidence for those separate reviews.

**Posting-identity gap (2026-10-09):** the Work panel now opens the existing full GL audit export directly; Chromium verified the exact report. Its GL rows carry source doctype/id and timestamp but no posting actor, and audit logs do not have a reliable entity link for every GL write. The link is a source review aid, **not** an accountable who-posted-what register. Add explicit actor capture to the shared posting path and reconcile historical `NULL`/unknown rows before 55.10 may be claimed complete. Account Head and qualified CA signoffs remain external gates.

#### B.3.5a Who can delete a purchase order (Stage 65.2)

**Settings → Roles → Purchase Order → Delete** decides it, per role. Super Admin and HR/Admin can always delete; any other role only when its Delete box is ticked (Store Manager, for example, ships without it). A role with it sees **Delete** on every PO row.

**What the system still refuses, whatever the role:** a PO that goods have been received against — a goods receipt that is not cancelled names it, directly or as one of the POs of a multi-PO receipt (§B.3.6). The user gets `PURCHA-0320` with the receipt number(s). Cancel the receipt first, then delete. Every other status can be deleted — draft, pending approval, approved, sent to the vendor — which is wider than other transactions, where an approved record can never be deleted. The delete is a soft delete: the record is hidden everywhere and logged as `SOFT_DELETE_PurchaseOrder` in the Activity Log with who did it; its row stays in the database for audit.

#### B.3.5b Item names, categories and product types (Stage 65.3)

Three small master lists sit above designs, so a buyer types an item's tax details once:

- **Category** (*Necklaces*, *Kurtas*) and **Product Type** (*Antique*, *Cotton*) — both under **Setup**: just a name. Their code is numbered by the system (`CAT/HO/26-27/000001`, `PTYPE/...`), whoever creates them, including from inside a PO.
- **Item Name** — under **Setup** (PIM): a Category plus a Product Type, the **name** (left blank, it becomes "*Category Product Type*"), the **HSN code** and the **GST rate**. Numbered `ITEMNAME/...`.

**Designs inherit from their item name.** A design (Product Family) created with an Item Name takes that item name's HSN and GST wherever its own are blank, and every combination created from the PO's **Add design** panel carries them too — so correcting an item name's HSN is the place to change it for future designs (existing items keep what they were created with). A design also stores the **Design Number** the buyer typed (the vendor's number, say); its code stays the system's own `Design/...` id.

**Who can do what.** Out of the box Super Admin and HR/Admin manage all three lists; Store Manager can read them. Creating an item name from the PO needs **create** on Item Name (and on Category / Product Type for new ones) — grant it under **Settings → Roles**. Adding a value to a product attribute from the PO needs **update** on the product attributes record type (`ProductAttributeDef`); adding a new attribute needs **create** on it. Both are recorded in the Activity Log (`DESIGN_ATTRIBUTE_VALUE_ADDED`, `DESIGN_ATTRIBUTE_CREATED`). A value with a comma, or a value for a typed (non-list) attribute, is refused with `PIM-0321`.

#### B.3.6 Designs from the PO, multi-PO goods receipts and price-difference notes (Stage 62)

**Design number mode.** **Configuration → Procurement → Design number on a PO's Add design** (`procurement.po_design_number_mode`):

- **New every time** (default) — every *Add design* on a PO creates a new design.
- **Reuse when it matches** — a design (ProductFamily) with the same name (case and surrounding spaces ignored) and exactly the same **design attributes** is reused; only combinations it lacks are created.

A pasted design number always loads that design, whichever mode is set.

**What a "design" is.** A design is a **ProductFamily** record, numbered from the `ProductFamily` series (`Design/<store>/<year>/000001`, editable under Prefix Configurations, §B.3.2). One created from a PO stores `design_attributes` (e.g. `fabric:cotton`), `vendor`, `hsn_code`, `gst_rate`, `mrp`, `purchase_rate`. Each combination is an ordinary **Item** with `family` = `parent_product_code` = the design and `variant_option_values` = its axis values; its code is the design number plus those values. The attribute matrix shown on the PO is every Active **ProductAttributeDef** — define Colour, Size and so on there (Value Type **Select**, comma-separated options). A design created without a code (the PO path) is numbered on save, so a buyer does not need the admin-only master-code endpoint; a typed code is kept.

**Duplicate combinations are refused.** Creating an Item whose generated Combination ID already exists is now rejected with a message naming the existing combination. Before Stage 62 such a create silently overwrote the existing Item.

**Multi-PO goods receipt rules.**

- One GRN may list several POs (`po_ids`); every line carries its own `po_id`, and the header `po_id` is the first PO, so every older report and integration still sees a PO.
- All POs must be the **same vendor** — otherwise **GOODSR-0318**.
- Approval, over-receipt tolerance, unexpected-item and fully-received checks run **per PO on that PO's own lines**.
- Received quantity is counted per line PO, so a later GRN loads only what is still open.
- **Decision D05 (Stage 73.2.4):** only **accepted** units fulfil a PO line (`fetchGRNAcceptedQuantities`; the over-receipt tolerance and "fully received" checks use accepted quantity), so rejected / damaged units stay open for replacement. The buyer ends that with **Short-close** (`POST /api/v1/procurement/purchase-order/{id}/short-close`, reason code category *Cancellation*, audited `PURCHASE_ORDER_SHORT_CLOSED`): status **Closed**, `short_closed_lines` recorded, further receipts refused (PURCHA-0489) and amendments refused (PURCHA-0489); every rejected / damaged unit still returnable is put on a Draft **PurchaseReturn** per receipt (`short_close_claims`). `GET /api/v1/procurement/purchase-order/{id}/reconciliation` is the PO balance view (ordered / received / accepted / rejected / damaged / returned / short-closed / open / invoiced / paid, plus money totals); invoiced and paid quantities are the accepted units of receipts carrying a billed or paid vendor invoice.

**PO rate vs invoice rate.**

- The server stamps each line's `po_rate` and `gst_rate` from its PO's own pricing (ex-GST per unit), ignoring any value the client sends.
- `invoice_rate` defaults to the PO rate. A negative rate is refused with **GOODSR-0319**.
- The GRN stores `po_value`, `invoice_value`, `invoice_gst`, `invoice_total`, the typed `vendor_invoice_amount` and `invoice_amount_difference`. The difference is a control figure only — it never blocks or holds the receipt.

**The accounting.** Amounts are on **accepted** quantity only; rejected and damaged units go back on a Purchase Return. The PO price is treated as the agreed price.

| Event | Entry |
|---|---|
| GRN posts | Dr 1200 Inventory / Cr 2100 GRN Suspense **at the invoice rate** (item cost too) |
| Invoice rate above PO → **Debit Note** (series `DN`) | Dr 2100 / Cr 5150 for the taxable difference, and Dr 2100 / Cr 1500 for its GST |
| Invoice rate below PO → **Vendor Credit Note** (series `VCN`, doctype `VendorCreditNote`) | Dr 5150 / Cr 2100 for the taxable difference, and Dr 1500 / Cr 2100 for its GST |

- Both notes are raised **and posted** as soon as the GRN's stock posts. They carry `grn_id`, `source_doc_id`, `note_lines`, `gst_amount` and `total_with_gst`, and are linked back on the GRN (`debit_note_id`, `vendor_credit_note_id`).
- Raising them again is a no-op, so a retry never double-posts.
- If a note cannot post, the receipt still stands. The user sees the reason, and the note can be posted from **Debit / Credit Notes**.
- Net effect: payable and purchase cost both settle at accepted quantity × PO price.
- The GST on a vendor credit note is input credit only as far as the vendor's own supplementary invoice supports it. Reconcile it with the vendor before filing.

**Vendor invoice matching.**

- A GRN whose lines all carry an invoice rate is matched at accepted quantity × invoice rate + GST, across all its POs.
- The PO over-billing check is skipped for such a GRN, because the debit note has already settled billing above the PO.
- Older GRNs keep the PO-rate basis.

**Permissions.** `VendorCreditNote` mirrors `DebitNote` (HR/Admin all; Store Manager read/create), and the Payables role template operates both.

#### B.3.7 Transaction print templates (Stages 63, 71)

**What can be printed.**

- Eleven transactions: Purchase Order, Vendor PO, Goods Receipt Note, **Vendor Copy** (`GRNVendorCopy`, a print of the same GRN), **Purchase Return** (`PurchaseReturn`), Transfer Out, Transfer In, Debit Note, Credit Note (to vendor), **Credit Note (to customer)** (`CreditNote`) and **Sales Return** (`SalesReturn`, prints a `ReturnRequest`). The four in bold were added in Stage 71; migration `migrations_stage71_1_print_formats.sql` adds them to `PrintTemplate.transaction`'s options, but only where that list is still the one Stage 63 shipped.
- **Standard layouts follow the company's print-format pack (Stage 71).** Each Standard layout has a common header (financial year, status), the pack's columns, reference lines that drop out when blank, totals, three signature lines and an audit line. The audit line reads `documents.created_by` / `created_at`, the last `approval_log` row with action `Approved`, and the printing user and time from the browser. Fields the ERP does not hold were left out: bin per line, cess, IRN / QR, e-way bill, vehicle / LR, packages, payment and delivery terms, reprint count. GRN and Transfer In standard prints are quantity-only. A tenant's own saved templates are untouched and still take precedence.
- **The Vendor Copy cannot leak cost.** `BuildPrintDocument` removes every rate, value, tax, cost field of the item, invoice amount, money total, tax summary and amount in words from its payload, so no template can print them.
- **Transfer In fix (Stage 71).** The receipt used to print its received quantity as "Sent", so a short line looked complete. Shipped now comes from `dispatched_items` (else `items`), and received, short and reason come from `received_items`.
- Each prints from one server payload, `GET /api/v1/print/document/{transaction}/{id}?store=&images=1`, which also returns the templates that fit. The payload holds brand, generating store, party, ship-to, every header field, every line with all its Item fields (and picture when `images=1`), the GST split per rate, totals and amount in words.
- The page is drawn in the browser by `public/trade-docs.js`, the same renderer the designer uses. It is printed through the browser print dialog; there is no thermal path.

**Brand.**

- **Configuration → Printing → Brand name on prints** (`print.brand_name`) is printed on the first page, with the rest of the header.
- Blank uses the generating store's legal entity name.
- A template that does not place the brand gets it at the top of the first page anyway.

**Multi-page documents.** With an items table, position decides the page, for the built-in layouts and every saved template alike: everything above the table prints on the first page only, everything below it (tax summary, totals, amount in words, signatures, audit line) on the last page only. Rows carry over with the column headings repeated; later pages start at the top margin, pages before the last run to the bottom margin, and the last page keeps at least one row with the totals. The page number, and anything beside the table, follow their own **Prints on**.

**Generating store.**

- Chosen at print time; defaults to the document's own store (PO: location; GRN: receiving location; Transfer Out: from; Transfer In: to; notes: their GRN's location).
- The store block shows the Location's name, address, city, PIN and phone, with the **state and GSTIN of its Legal Entity**. Make sure every Location points at a Legal Entity that has a GSTIN and state.

**Templates.**

- `PrintTemplate` records (module Core). Design them under **Stock → Sticker Printing → Document templates** (USER_GUIDE §7B.2).
- Fields: `transaction`, `is_default`, optional `vendor`, `item_families`, `item_categories` (comma-separated), `layout` (JSON, up to 1.5 MB including logos), `status`.
- Permissions: HR/Admin all; Store Manager and Cashier read.

**Which template a document gets.**

- A template mapped to a vendor, family or category is offered only when the document matches. The vendor is the PO's or GRN's vendor; families and categories come from the lines' Items.
- The best match is pre-selected, scored vendor 8, family 4, category 2, default 1.
- With no matching record, the built-in **Standard layout** is used.
- Setting a template as default clears the flag on others of that transaction.
- Retire a template with **Status = Inactive**.

**Transfers and tax.**

- Transfer value is quantity × the Item's `standard_cost` (else MRP).
- Between two Locations of the same GSTIN it prints as a delivery challan with no GST.
- Between different GSTINs, GST is computed at the Item's rate: IGST across states, CGST + SGST within one.

**Vendor PO in Excel.** `GET /api/v1/procurement/purchase-order/{id}/excel` returns an .xlsx built without any library: header, parties, lines with the item's thumbnail in the Image column, GST split and grand total. Item pictures come from PIM media (the active Main Image, else the first image).

**Silent printing.** A tenant with a QZ / print-agent printer mapped for job type **Purchase Order** keeps printing POs silently; the print window opens only when no such printer is mapped.

#### B.3.8 Item & Material Master — profiles, packaging backflush at the till, gifts with purchase, gift cards (Stage 72)

The rule behind all of this: **one list cannot answer six questions.** "Gift", "Backflush", "Asset" and "Sellable" are different kinds of answer, so every item now answers them separately, and behaviour is read from those answers — never from the name of an item or a type.

**1. The eight dimensions (Setup → Item Type, and on every Item).**

| Field | Values | What it controls |
|---|---|---|
| Commercial class | Merchandise, Accessory, Promotional / GWP, Packaging, Consumable, Service, Stored Value, Fixed Asset, Internal / MRO, Bundle / Kit | What it is - descriptive, for reports. Never drives behaviour on its own. |
| Stock tracking | Tracked / Not tracked | Not tracked: no store stock. A GRN posts no stock (value goes to expense), the till issues none, transfers and consumption refuse it (`INVENT-0421`). |
| Valuation | Valuated / Non-valuated / Not applicable | Non-valuated: quantity is kept but carries no value - a GRN debits the **Expense account** instead of 1200 Inventory and the item never enters the moving average, so issuing it costs nothing. |
| Accounting treatment | Inventory asset, Expense, Fixed asset, Stored value liability, Service | Must agree with valuation (below). |
| Sellability at POS | Normal sale / Promo only / Not sellable / Stored-value issue only | Not sellable: refused at the till (`POSOFF-0411`). Promo only: billed at 0, only up to what a Gift With Purchase offer (or the Free-With-Item parent) earns (`POSOFF-0412`). Stored-value issue only: refused as a bill line (`POSOFF-0413`) - issued through Gift Card. |
| Customer bill | Visible line / Hidden (internal) / Separate receipt | Hidden zero-value lines are not printed. |
| POS capture | Scan / Quick select / Auto / Manual / None | Descriptive guidance for the till. |
| Consumption | Sale issue / Auto backflush / Manual issue / Periodic / Production / None | Auto backflush: consumed by Backflush Rules at the till, and at production completion when it is a BOM component. Periodic: counted and posted with Consumption Entry. |
| Expense account | a GL code | Where an expensed or consumed item's cost goes. Blank: 5160 Packaging Consumption for packaging, 5170 Promotional / GWP Expense for gifts, else 5180 Consumables Expense. |

**Precedence:** the Item's own value → its Item Type's → what the type's Stage 60 flags meant → the company default (an ordinary merchandise line). Leave the Item's fields at *Item type default* unless that item is the exception. *Reports → Item Classification Register* shows every item's resolved answers and which ones the item overrides.

**Rules the system enforces on save** (Item, Item Type, CSV import and API alike):
- Valuated stock must be an Inventory asset (`MASTER-0401`); a not-tracked item is *Not applicable*, a tracked one is Valuated or Non-valuated (`MASTER-0402`).
- Fixed asset accounting only on a Fixed Asset class (`MASTER-0403`); Stored Value class ⇔ *Stored-value issue only* + *Stored value liability* (`MASTER-0404`).
- Auto backflush or Periodic consumption needs a stock-tracked item (`MASTER-0405`); the expense account must exist in the chart (`MASTER-0406`).
- **Once an item has stock history** (or, for an Item Type, once any of its items does), stock tracking, valuation and accounting treatment **cannot change** (`MASTER-0407`) - the quantities and values already posted were posted under the old treatment. Create a successor item / type.

**Shipped profiles.** Product, Accessory, Non-Asset (normal sale), **Gift** and **Free With Item** (Promotional / GWP, *Promo only*, cost to 5170), Backflush (Auto backflush), Fixed Asset (not tracked, fixed asset, not sellable), and new: **Packaging** (tracked, valuated, not sellable, hidden, auto backflush, 5160), **Consumable - Periodic** (tracked, **non-valuated**, expense, not sellable, periodic, 5180), **Service** (not tracked, sold), **Non-Stock Expense** (not tracked, not sold, 5180), **Gift Card** (stored value) and **Virtual Kit** (not tracked; the Product Bundle's components leave stock). The migration filled these values only on types whose dimensions were blank, so a type you had already customised keeps yours.

> **Behaviour change to tell the stores:** a **Gift** item now needs a *Gift With Purchase* offer to be billed (step 3). Before Stage 72 the till refused every zero-priced line anyway, so no store was able to bill gifts; now they can, under an offer.

**2. Packaging consumed with every sale — Backflush Rules (Setup → Inventory → Backflush Rule).** One row per *"this sells → this is used up"*:

- **Applies to:** *Item* (an item code), *Attribute* (`Size:Large`, matched against the item's variant options), *Category* (code or name), *Item Type*, or *All items*.
- **Store:** blank = every store; a store's own rule beats the all-stores one. Store managers see the rules of their own stores and the all-stores ones.
- **Consumed item** (must be stock tracked), **Qty per unit sold** (whole number, default 1), **Group** (*Box*, *Butter paper*…).
- **Which rule wins:** within a group the most specific wins - Item, then Attribute, then Category, then Item Type, then All items; a store's rule before the all-stores one at the same level; then the higher **Priority**. Rules in different groups add up (a box *and* a butter paper).
- **Cashier may change it** (default Yes): the till's **Packaging** button offers every consumed item of the same group set up for that store, plus **None** if **"None" allowed** (default Yes). **Reason needed when changed** makes the cashier type one (`POSOFF-0414`).
- **When the store is short:** *Allow and report* (default - the sale always goes ahead, stock may go negative, and an Item Exception opens), *Warn* (the till shows it, the sale goes ahead) or *Block* (the sale is refused until another option is chosen - `POSOFF-0416`).
- **Effective from / to** dates; **Status** Inactive stops a rule.
- Only lines **sold normally** trigger packaging - gifts, free items and gift cards never do. The consumption is worked out from the server's own priced lines, posted **in the sale's transaction** (stock out under voucher `POSConsumption` with the bill number; valuated packaging Dr expense / Cr 1200 at moving-average cost; non-valuated quantity only) and stored on the bill with each rule's id **and version**, so editing a rule later never changes a past bill. A retried sale never consumes twice.
- **Returns do not put packaging back** (the box usually stays with the customer); a box that physically comes back is received like any stock. *Reports → Backflush Consumption* lists every bill's consumption, what was changed and why.

**3. Gift With Purchase (Setup → Offer).** Offer type **Gift With Purchase**: the usual scope (bill / item / category / product group / SKU list), minimum bill or quantity, coupon, customer tier and validity, plus **Gift item** and **Gift qty** (per bill; or per **Buy Qty** qualifying units when Buy Qty is set - e.g. Buy Qty 1, Gift qty 1 = one plant per necklace). It is judged on the **paid** lines only, never uses up a discount slot, and makes the gift item eligible: the till prompts *"Gift eligible … scan it"*, and the scanned gift is billed at 0 with the offer recorded on the line. The gift's cost goes to its profile's expense account (5170). Tax is computed by the normal engine on the line's value; Finance should confirm the treatment of free supplies. *Reports → Gift With Purchase Register* lists every gift handed over.

**4. Gift cards (stored value).** A gift card is a liability to the holder, not merchandise.
- **Setup → Sales → Stored Value Profile.** The shipped *Standard Gift Card*: 16-digit numeric, any amount 100-50,000, valid 12 months, part use allowed, no add-value, liability account 2610. Fixed denominations: Amounts = *Fixed* and list them (`500,1000,2000`).
- **Register cards** (store manager): POS → **Gift Card → Register new cards** - paste pre-printed numbers, or generate N (numeric ones end in a check digit). Generated numbers are shown once for printing. A number already known is refused (`POSOFF-0432`).
- **Issue** happens only after payment: the card is funded, its first ledger event written and Dr payment clearing / Cr 2610 posted together, or not at all. **Redemption** is a tender at checkout, inside the sale's transaction: Dr 2610 / Cr the payment clearing account, capped at what the customer owes. Never on an offline-queued sale.
- Every balance change is an **append-only ledger event** (ACTIVATE, RELOAD, REDEEM, BLOCK, UNBLOCK, CANCEL) with the balance after it; the database refuses edits to the ledger, and the Data Console shows these tables read-only with card numbers masked. Screens and reports show only the last four digits.
- **Block / release** need a reason; **Cancel an unused issue** refunds by the original payment mode while the whole balance is unused.
- **Reports → Gift Card Liability** (outstanding by card; reconciles to account 2610) and **Gift Card Ledger** (every event).

**5. Consumption Entry (Inventory → Consumption Entry, store manager / admin).** *Periodic count* posts book − counted closing stock; *Manual issue* posts the quantity used. Refused: counted above book, used above stock (`INVENT-0422`), items that keep no stock (`INVENT-0421`). Valuated items: Dr expense / Cr 1200; non-valuated: quantity only. The entry keeps its count reference and who posted it; a retried post never consumes twice.

**6. Exceptions and reports.** **Setup → Inventory → Item Exception** (also *Reports → Item Exceptions*): every packaging shortfall (*Open* - fix it, then set Status = Resolved with a comment), and every refused non-sellable or unearned gift line at a till (*Logged*, one per bill and item). Other Stage 72 reports: **Item Classification Register**, **Item Master Completeness** (what each item still lacks for how it is classified), **Non-Valuated Stock**, **Inactive Items With Stock**.

**Not built yet - each needs your decision first** (checklist 72.D): item release maker-checker and a Draft / Blocked lifecycle; effective-dated item and tax versions; cancelling a posted bill (today: a return); per-store item policy overrides; the audit trail recording the editor and a reason; negative-stock policy per profile; gift return policy; gift card refund-to-card and transfer.

#### B.3.9 Stock transfers — approval, reason codes and tracked stock (Stage 73.1.7)

**Approval is maker-checker by default (decision D04).** The release adds one approval rule for `TransferOrder` — any value, approver role **Store Manager** — to every client that had none (Settings → Approval Rules; edit or add slabs there like any other rule). The person who created a transfer can never approve it. The rule routes by the transfer's **value at item cost** (moving-average cost, else the item's Standard Cost), written onto the transfer as `total_amount` when it is sent for approval.

**Direct approval below a limit.** **Settings → Configuration → Inventory → Transfer direct-approval limit** (`inventory.transfer_direct_approval_limit`, ₹, default **0 = off**). Above 0, a Store Manager of the sending or receiving store (or an administrator) may approve a transfer worth *less* than the limit alone, with **Approve now**. A transfer containing an item with no cost on record is never approved directly. Each direct approval is written to the approval log as "Direct approval: ₹X is below the ₹Y limit".

**Access.** The release grants **Store Manager** read / create / update (not delete) on `TransferOrder`, unless the client had already set that role's permission. Change it under Settings → Roles.

**Reason codes.** Damaged, lost and extra units at receipt, and a return to source, need a **Reason Code** (Masters → Reason Codes) of the new category **Transfer Discrepancy**; cancelling a transfer needs one of category **Cancellation**. Create a few before go-live (for example *Damaged in transit*, *Short shipped*, *Over shipped*, *Refused at delivery*). The receive and return dialogs link to the master when none exist.

**What moves where.**
- **Dispatch** takes stock off the sending store (`on_hand`, `available`) into its `in_transit`. A batch-tracked item leaves as named lots, picked earliest-expiry-first from the bins (or named at dispatch); its bins' lot quantities go down at the same moment. A serial-tracked item needs one serial number per unit; the units become *Allocated* to the transfer.
- **Receipt** moves each unit out of `in_transit` once: good units into the receiving store's `on_hand` and `available`; damaged units into `on_hand` and the `damaged` bucket (not sellable); lost units nowhere (they already left the sender's books at dispatch); extra units into `on_hand` and `available`. Serial units become *InStock* at the receiving store (lost ones *Scrapped*). Each receipt is stored as an event with who and when; a repeated receipt key is ignored.
- **Return to source** moves in-transit units back into the sending store's stock; serial units become *InStock* there again.
- Every movement writes stock-ledger lines, one per lot or serial number for tracked items.
- No GL is posted: a transfer moves stock between stores of the same business.

**Statuses** now include **Pending Approval**, **Partially Received**, **Returned** and **Cancelled** (added to the TransferOrder status list by the release migration).

#### B.3.10 Cycle count recount triggers (Stage 73.1.8)

**Settings → Configuration → Warehouse** has three triggers, each **0 = off** (the default, which keeps the old behaviour):
- **Recount when a count is off by** (`wms.recount_trigger_qty`, units)
- **… (percent)** (`wms.recount_trigger_pct`; a count against zero on the books counts as 100%)
- **… (value)** (`wms.recount_trigger_value`, ₹ at item cost — moving average, else Standard Cost; an item with no cost never trips this one)

When **Reconcile Session** meets a first count whose difference reaches any switched-on trigger, it does not send that line for approval: the line becomes **Recount Requested** (with `recount_trigger` saying why) and a blind recount line is created in the same session. Only the recount's difference is approved, and the recount line carries `first_count_qty` / `first_count_variance` for the approver. A recount is never itself re-triggered.

**Posting is one transaction.** An Approved line's difference and its **Posted** status are written together under the line's row lock, and only an **Approved** line posts (the retry button on the Cycle Count screen cannot post a line that was never approved). The variance root-cause reason (category *Cycle Count Variance*) is still required before it posts. The stock-ledger line carries the key `CYCLECOUNT:<line>`, so it is written once. Counts still adjust the location's stock, not individual bins.

#### B.3.11 Warehouses for several 3PL owners (Stage 59)

**The setting.** **Settings → Configuration → Warehouse → Stock ownership model** (`wms.stock_ownership_mode`):
- **One owner per warehouse** (default) — a location's first owner claims it (`warehouse_owner`) and any other owner's stock, bin or order there is refused (INVENT-0491).
- **Several owners per warehouse (stock kept apart per owner)** — supported since Stage 59. The pre-59 value `mixed_unsupported` is still read as this mode.

**Whose is a unit?** Per bin, item and condition: explicit owner slices (`bin_stock_owner`) if there are any (an unsplit remainder then belongs to nobody); else the bin's own **Owner** field; else the warehouse's dedicated owner; else nobody. When another owner's units join a bin whose stock is owned by implication, the existing units are first written down as their owner's slice, so they never lose their owner.

**What enforces it.**
- **Orders:** `SalesOrder.owner_id` (intake field `owner_id` on `POST /api/v1/orders`, or `POST /api/v1/wms/order-owner` before release, audited `ORDER_OWNER_SET`). At release each pick task gets the order's owner, else the warehouse's owner, else the only owner holding stock there; with several owners and no order owner, release is refused (INVENT-0492). After release the owner is fixed (INVENT-0493).
- **Sourcing:** a new owner's order is only planned at a location where that owner's own stock covers it (otherwise it is re-sourced on the owner's stock, or held for allocation). `GET /api/v1/wms/owner-availability?sku=&location=&owner_id=` reads one owner's available-to-sell. Re-allocating an order that was held does not yet re-check the owner — allocation and picking still do.
- **Allocation / picking:** reservations (`bin_allocation.owner_id`) and picks take only the task owner's units, FIFO / FEFO / nearest-bin order unchanged; scans and dispatch take them off that owner's slice.
- **Putaway / moves / condition changes:** carry the owner (fields on the screens, `owner_id` on the APIs; putaway also takes `grn_id` and uses the GRN's **Owner**). Refusals: a bin owned by someone else (INVENT-0495); several owners in the bin and no owner named (INVENT-0496).
- **Bin owner field:** cannot change while the bin holds stock it attributes (INVENT-0494); move the stock out or record whose it is first.
- **Counts** carry `CycleCountLine.owner_id` onto the stock ledger. Counts still adjust the location, not owner slices — reconcile slices with the exception queue.

**Exception queue.** **Reports → Owner Stock Exceptions** (`owner-stock-exceptions`). Fix a slice after counting the bin with `POST /api/v1/wms/owner-stock/correct` `{bin_code, sku, condition, owner_id, qty, reason}` (Store Manager or administrator; never more than the bin holds; audited `WMS_OWNER_STOCK_CORRECTED`).

**Banner.** The page-wide "Conditionally supported" banner no longer carries the 3PL caveat (47.5 is closed); the RF device certification and legacy audit coverage lines remain.

#### B.3.12 Finance controls — who posted, period close and reopen, bank reconciliation, control totals (Stage 73, 55.10–55.20)

Six migrations ship with this release and run with `-migrate` like any other: `migrations_stage73_1_9_gl_posting_attribution.sql`, `migrations_stage73_1_10_period_close_tasks.sql`, `migrations_stage73_2_16_bank_reconciliation_exceptions.sql`, `migrations_stage73_55_15_expense_receipt_reference.sql`, `migrations_stage73_55_15b_attachments.sql` and `migrations_stage73_55_16_asset_depreciation.sql`. All are additive; nothing existing is changed or removed. **Deploy them together with the binary** — without the new GL accounts, depreciation runs fail and disposals are refused (ASSET-0539).

- **Who posted each GL line.** Every new GL line records **Posted by** and **Source command**: the signed-in username and the screen's API route; `service:job` and `job:<type>` for background jobs; `service:api-key:<prefix>` for a public-API key; `service:inbound` for an unauthenticated webhook; `service:background` plus the engine function for anything else. Lines posted before the release stay blank and read **before attribution** — they are not back-filled, because a guess would be worse than a blank. Users review it in Reports → **GL Posting Review** and **General Journal** (User Guide §9B).
- **Who may close.** Creating, closing and requesting a reopen of an accounting period is allowed to **Accountant** and **Super Admin** (before this release, Super Admin only). The decision this follows (D09) names "Finance Head / Admin"; there is no Finance Head role, so the Accountant role template — the one that owns period close — plays it. A tenant role literally named *Finance Head* is treated the same.
- **The close tasks are mandatory.** A period cannot close until its five tasks are signed by their owners (FIN-0501). Owners must be active users (FIN-0505); only the owner signs (FIN-0508).
- **Reopen approval rule.** The release adds an approval rule for `PeriodReopenRequest` — any value, approver role **Accountant** (Super Admin may always decide) — in **Settings → Approval Rules**. Change the role there if a different role should approve reopens. The requester can never approve their own request. If an approval went through but the period did not reopen (the reopen runs just after the approval is saved; a failure is logged to System Status), the period's close board shows **Reopen now** to finish it.
- **Bank reconciliation.** A new GL account **5230 Card / UPI Settlement Fees** takes the acquirer's fee when a card or UPI payout is matched (clearing 1101 / 1102 is moved to the bank account's GL code). Closing an exception as **Explained** is limited to Accountant and Super Admin. Reconciliation of statement lines dated in a closed period is frozen (FIN-0511).
- **Control totals** on Inventory Valuation, Receivables Ageing, Payables Ageing, Sales Register, GST Return Summary and Loyalty Points Liability read the documents and the GL in one consistent snapshot at the cut-off they state. A role that may not see amounts (the sensitive-column rule) sees only whether they tie. **Payables Ageing** ages the 2100 GRN Suspense sub-ledger — one open item per goods receipt, cleared by bill payments, debit / vendor credit notes and purchase-return reversals — and ties to 2100; the former PO-based view is the **Open Purchase Commitments** report.
- **Part settlement.** Sales Invoice **Settle** and Vendor Invoice **Pay** take a part amount; each receipt or payment posts once under its own reference, so a retried request never double-posts. Refusals: FIN-0517 (more than outstanding), FIN-0518 (foreign currency: full only), FIN-0519 (reference reused for a different amount), FIN-0520 (invalid reference), FIN-0521 (TDS on a part-paid bill), FIN-0522 (credit note against an open foreign-currency invoice). A posted credit note naming an open invoice reduces its outstanding before any refund.
- **Scheduled reports and digests.** Reports → **Schedules** shows a Super Admin every ScheduledReport and DashboardDigest with owner, next run (the worker's own rule: Active, not deleted, next run date on or before today; checked hourly), last result and masked recipients. **Pause** sets the record Inactive; **Resume** sets it back; **Run now** (owner or Super Admin) sends through the same path as the worker and is refused while running or within a minute of the last run. All three are audited. A schedule deleted in Setup is no longer sent (before this release it kept going out).
- **Report exports are private.** A background export can be listed, downloaded and cancelled only by the person who requested it (or a Super Admin). Another user asking for it by id gets "not found".
- **Expense payments.** Paying a claim now credits the bank with what is actually reimbursed (claim + GST − advance adjusted) and clears the advance from **1600 Staff Loans & Advances**, where advances are booked (Loans/Advances). Before, the bank was credited with the full claim even when an advance was adjusted.
- **Receipts on expense claims are stored on disk, not in the database.** Files live under `media_store/attachments/<tenant schema>/` in the server's working directory (`/opt/erp/media_store` on Linux), under random names, with their details in each tenant's `document_attachments` table. JPEG, PNG, WebP and PDF only (checked from the file's bytes), 5 MB each, 5 per claim; changeable only while the claim is Draft or Rejected (a Super Admin can remove one later). **`deploy/backup.sh` and `manage.ps1 backup` dump only PostgreSQL**, so add `media_store/` to your backup (for example a nightly `tar` beside the dump, kept as long as the dumps) and restore it with the database; PIM product media live there too. A row whose file is missing downloads as "The stored file is missing".
- **Leave balance.** `GET /api/v1/hr/leave-balance` returns a person's own balance only, unless the role has HR self scope (Store Manager, Super Admin).
- **Fixed assets — depreciation and disposal.** New accounts `1410` Accumulated Depreciation, `5310` Depreciation Expense and `4300` Gain on Asset Disposal. Each ended month posts Dr 5310 / Cr 1410 under key `Asset:<id>:DEP:<YYYY-MM>`, dated at the month-end so period reports show it in its own month, and never twice. An `asset_depreciation` job is queued once per calendar month for every client with Fixed Assets on (checked hourly; the first check is an hour after the server starts); an Accountant or Super Admin can post at once with **Run depreciation**. **First run after this release catches up:** every Capitalised asset gets one posting per month ended since its capitalisation month (an asset capitalised two years ago gets about 24 at once); Draft and already-Disposed assets are untouched; months in a closed period are skipped until it is reopened. Disposal posts due depreciation first, then Dr 1410, Dr the cash/bank account for the proceeds, Cr 1400, and the difference to 5300 (loss) or 4300 (gain).
- **HR journeys and who sees what.** Appraisals route by the **Appraisal** approval rule (seeded: Store Manager) and grievances by the **Grievance** rule (seeded: Super Admin); whoever submitted can never decide. Grievance description and resolution notes are readable only by Super Admin; a Store Manager lists every case without its content; other roles see only their own employee's cases — `GET /api/v1/hr/grievance-access` returns exactly this and the screens print it. The Approvals inbox now applies the same self scope and hidden fields as the record API (before, it showed every pending grievance's text to every role). Employee Self-Service cannot read AppraisalCycle or TrainingProgram, so its screens use the cycles and programs on the employee's own records. For My Requests, set the Employee's **Linked ERP User ID**.

| Code | When you see it | What to do |
|---|---|---|
| FIN-0501 | Closing with unsigned close tasks | Have each owner sign their task, then close |
| FIN-0503 | Changing a task on a closed period | Request a reopen |
| FIN-0504 | A non-Accountant/Super Admin assigning tasks, requesting a reopen, or explaining a bank exception | Ask an Accountant or Super Admin |
| FIN-0509 | Requesting a reopen of an open period, or a second one while one waits | Check Approvals for the pending request |
| FIN-0511 | Changing reconciliation on a line in a closed period | Request a reopen of that period |
| FIN-0513 | Match amounts that do not agree | Post the bank charge / correction first, then match |

#### B.3.12 RFQ to purchase order (Stages 73.2.1-73.2.3, decision D06)

- **Screen and API.** Procurement → RFQ / Quotes is its own lazy module (`public/view-rfq.js`). APIs: `POST /api/v1/rfq/save` (create, or update a Draft's lines), `POST /api/v1/rfq/{id}/invite` and `/decline`, `POST /api/v1/rfq/{id}/quote`, `GET /api/v1/rfq/{id}/comparison`, `POST /api/v1/rfq/{id}/award` (`quote_id`, optional `location`, `line_items` for free-text lines, `leave_out`). The older `/api/v1/rfq/quotes` and `/rfq/select-quote` still work.
- **Stored on the RFQ:** `lines` (line_no, sku or free text, uom, qty), `vendor_invitations` (vendor, Invited / Responded / Declined, due date, quote id; the old `invited_vendors` list is kept in step), `location`, `response_due_date`, and after the award `awarded_po_id` / `awarded_quote_id`.
- **Stored on a VendorQuote:** priced `lines` (quoted unit, conversion to the RFQ unit, unit price, tax %, lead days, not quoted), `subtotal`, `tax_total`, `freight_amount`, `landed_total` (also written to the old `quoted_price`), `payment_terms`, `valid_until`. A unit conversion comes from the quote line or the item's **UOMConversion** master. A newer quote from the same vendor marks the earlier one **Superseded**.
- **Award** runs under the RFQ's row lock: it creates a **Draft** PurchaseOrder (numbered from the PO series; `items` at the quote's price per RFQ unit, ex-GST; `freight_amount`, `payment_terms`, `quote_valid_until`, `source_rfq_id`, `source_quote_id`, `source_requisition_id`), marks the quote Selected with `po_id`, the other live quotes Rejected, and the RFQ Closed. A repeat of the same award returns the same PO; any other is refused (RFQ-0555). PO quantities must be whole units; an expired quote cannot be awarded (RFQ-0556). The PO's GST is computed from the item master as for any PO - the quoted tax % is for comparison.
- Generic edits cannot select a quote or change an awarded RFQ (RFQ-0555). Codes: RFQ-0551 (lines), RFQ-0552 (state), RFQ-0553 (vendor), RFQ-0554 (quote terms), RFQ-0555/0556 (award).

#### B.3.13 Supply proposals (Stage 73.2.18)

`SupplyProposal` (module Inventory) is written only by `engines/supply_proposals.go`: `POST /api/v1/planning/proposals/generate` (`location`, `source` reorder | mrp, `lead_time_days`, `safety_stock`), `GET /api/v1/planning/proposals?location=&status=`, `POST .../approve` (`ids`, `action` Purchase | Transfer | Production, `vendor`, `from_location`, `quantities`, `note`) and `POST .../reject` (`ids`, `reason`). One open proposal per source, item and location (regenerating refreshes it). Approval locks the proposals, converts them in one transaction into Draft documents numbered from the PO / TO / PRO series that carry `source_proposal_ids`, and records `target_doctype` / `target_id`, quantity, decider and time on each proposal; a non-open proposal is refused (PLAN-0557). The release grants HR/Admin and Store Manager read/update on SupplyProposal; approving also needs create on the target doctype. The screen is a tab of Manufacturing, so a client without the Manufacturing module uses the API.

#### B.3.14 Quality and maintenance (Stage 55.13)

Screens: **Manufacturing → Quality Inspections / Certificates / Non-conformance / Maintenance** (lazy module `view-quality.js`; the endpoints are gated by the *quality* module, the tabs sit in Manufacturing). Set up before first use:

- **Inspection plans** (Setup → Inspection Plan): the item, sample size, **Applies to** (Incoming / Production / Outgoing) and the parameters with their specification. An inspection on a plan must record a Pass/Fail reading for every parameter.
- **A hold code of category Quality** (Setup → Hold Code) — used when a failed incoming or outgoing inspection holds stock. The first active one is the default.
- **Reason codes of category Quality** (Setup → Reason Code) — the root causes a non-conformance investigation picks from.
- **Maintenance schedules** (Maintenance → + New Schedule) with a checklist; the background scheduler raises a Scheduled order on each due date and copies the checklist onto it.

Rules the server enforces (`engines/quality_inspection.go`, codes QUALIT-0561..0566): an inspection names a source that exists (Incoming = a GRN and an item received on it; Production = a production order; Outgoing = a sales order) and, for Incoming/Outgoing, a location and quantity; the result is derived (one Fail fails it). A production inspection is decided only through Approvals (the order's QC gate). An incoming/outgoing one is decided once: Pass → Accept; Fail → **Hold stock** / **Rework** (a Hold for the inspected quantity, in the same transaction — released only by an approved hold release) or **Use as-is (concession)** (a note, and not by the inspector). Every failed inspection raises one NonConformanceReport linked to it. A report needs a Quality root cause, then a corrective action with an **owner** (and optional due date), then closing **evidence** (`closed_by` recorded). Maintenance orders go Draft → Scheduled (a date) → In progress (`started_by`) → Completed (notes, every checklist task ticked, parts with quantities, hours, `completed_by`/`completed_at`); parts are recorded, not issued from stock. Each action needs the role's create/update permission on the doctype (HR/Admin and Store Manager have them by default). A Certificate of Analysis records your own lot test results and is not a regulatory certificate; rejecting one moves the lot's binned stock to QC-Hold.

#### B.3.16 Outbound, labour and billing (Stage 55.8)

`GET /api/v1/wms/outbound-work?location=` (`engines/wms_outbound_work.go`, read on FulfillmentTask): Pending/Picking tasks in no wave, open waves with tasks per status and the registered Wave's lifecycle status, open LoadingTasks, and Invoiced ShippingPackages not yet loaded (with their booking's manifest). `POST /api/v1/wms/wave/from-selection` (`location`, optional `wave_id`, `task_ids`; update on FulfillmentTask) checks each task — same location, Pending/Picking, no other wave — and then uses the existing `CreateWave` (a registered Wave, Planned) or `AssignTasksToWave` for an open wave at that location (Planned / Released / In Progress); refused tasks come back by name, INVENT-0499 when none qualify. The wave lifecycle (Release, Complete) and pick-list allocation are unchanged. Mobile Picking's **Mark packed** uses the ordinary fulfillment transition (Pending → Picking → Packed) only for orders with no shortfall; nothing moves stock until Dispatch.

Labour: `GET /api/v1/wms/labor/standard-preview?code=&qty=` (`engines/wms_labour_explain.go`) returns each component's seconds next to `ResolveLaborStandardTime`'s total and an `agrees` flag; the masters (LaborOperation, LaborElement, TravelSection, LaborAllowance, LaborStandard) stay ordinary records, which the Cockpit's wizard creates in that order. Billing: `GET /api/v1/wms/billing/review?owner=&start=&end=` (read on CapturedCharge) explains each captured charge and storage rate and lists exceptions (missing snapshot days, minimum charge applied, no rate evidence); INVENT-0500 without an owner or a valid period. From this release every captured charge also stores its `base_rate`, `base_amount`, `markup_pct`, `discount_pct` and `minimum_charge`; charges captured earlier show as *no rate evidence*. CapturedCharge and StorageBalanceSnapshot remain read-only to every role (written only by the engine). Billing policy — which events are charged, at what rate, with which minimums — is the client's decision, configured in ChargeCode / RateGroup / ChargeContract / StorageBillingRate.

#### B.3.15 Inbound: yard, RF receiving and putaway work (Stage 55.7)

`GET /api/v1/wms/inbound-work?location=` (`engines/wms_inbound_work.go`, needs read on ASN) returns the Expected ASNs with their appointment (`Appointment.asn_id`) and yard record (`YardCheckIn.appointment_id`) and a stage, door-side first, plus *putaway waiting* = on hand − everything in `bin_stock`, for locations with at least one active Bin. Two yard actions are now server actions: `POST /api/v1/wms/yard/check-in` (a trailer not already in the yard; an optional Scheduled appointment becomes CheckedIn — INVENT-0498) and `POST /api/v1/wms/yard/{id}/door` (an active DockDoor with no other trailer AtDoor; the appointment becomes InProgress — INVENT-0497); check-out is unchanged. A GRN carrying `asn_id` is refused when that ASN is no longer Expected (GOODSR-0558, edits of an existing GRN excepted); after a GRN posts, the ASN becomes **Received** (and its appointment **Completed**) once the GRNs against it cover every expected line — a partial receipt leaves it Expected. Set up for operators: Dock Doors, Bins per location (and a putaway strategy for bin suggestions), and the supervisors who watch **FloorAssistRequest** (raised from RF Receiving's *Get a supervisor*). Physical RF-gun acceptance is still to be done on site.

### B.4 Where to Look When Something Seems Wrong

1. **`.\manage.ps1 logs`** — the fastest first check. Shows the server's own output and error logs, plus the database log.
2. **Activity Log** (in the app sidebar, formerly "Log Hub") — shows audit trails and recorded system errors from inside the running application.
3. **`docs/operations/incident_runbook.md`** — the full incident-response procedure: severity levels, escalation, rollback, and exactly where every kind of log lives. Read this before an incident happens, not during one.

---

## Part C — Operator / Platform-Team Level

### C.1 Environment Layout

This system can run up to three independent copies side by side, sharing the same PostgreSQL server but each with its own database and port:

| Environment | Purpose | Default port |
|---|---|---|
| `dev` | The main working copy — this repository folder itself. | 8080 |
| `test` | A staging copy for verifying a change before it goes live. | per `environments.json` |
| `live` | The real, production environment. | per `environments.json` |

`test` and `live` live in their own separate folders (git "worktrees") created by `promote.ps1` (§D.1) — they are not manually maintained copies, they're produced by the promotion process itself.

### C.2 Deployment Pipeline

See §D for the full deployment procedure. In short: a change is tested in `dev`, promoted to `test`, verified, then promoted to `live` — never edited directly in `live`.

#### C.2.1 The version number users see, and how to bump it

Every signed-in user sees a small grey number in the bottom-right corner of their profile chip at the bottom of the sidebar (beside their role, under the arrow) — **`0.1.0`** and nothing else. Hovering, focusing or tapping it opens a small popup carrying the version and the release date in `D/M/YY` form (**`0.1.0`** / **`4/10/26`**), so the date is one hover or one tap away without competing with the username above it. The same values are available unauthenticated at **`GET /api/v1/version`**:

```json
{"version":"0.1.0","release_date":"2026-10-04","build_time":"2026-10-04T03:23:45Z","git_commit":"afaf419"}
```

The UI deliberately surfaces only `version` and `release_date`. `git_commit` and `build_time` stay on the endpoint for you and for health checks — they are what tells you *which* build is actually on a box, which is the question that matters after a deploy that may or may not have restarted cleanly.

**The two halves come from different places, and only one of them is hand-maintained:**

| Shown as | Comes from | Who updates it |
|---|---|---|
| `v0.1.0` | `internal/server/VERSION`, compiled into the binary with `go:embed` | **You**, by editing that file |
| `Oct 4, 2026` | the `buildTime` value stamped in at build with `-ldflags` | Automatic — every build stamps it |

So **a deploy always refreshes the date on its own**; nobody has to remember anything. Bumping the *number* is a deliberate release decision:

1. Edit `internal/server/VERSION` — one line, just the number (`0.2.0`). No `v`, no date.
2. Deploy normally (`.\deploy\deploy.ps1`, `.\promote.ps1`, or CI). The embed picks it up at compile time, so there is no config to change and no migration.
3. Confirm with `curl -s https://<host>/api/v1/version` before telling anyone it shipped.

**Things worth knowing:**

- **Never set the version by hand anywhere else.** It is embedded at compile time precisely so it cannot drift from the binary that reports it. A number typed into a config file or a database row would eventually lie.
- **`dev build` instead of a date in the popup** means the binary was built with a plain `go build`, with no ldflags. That is correct and expected on a developer's machine; on a server it means someone built it outside the normal scripts, and you should not trust that deploy — rebuild it with `deploy/deploy.ps1`, `manage.ps1 release`, `promote.ps1` or CI, all of which stamp it.
- **The release date is derived from the build timestamp, not stored separately**, so the two can never disagree.
- **A user reporting a bug should be asked for this line first.** It is the cheapest way to find out whether they are on the build you think they are.
- Roughly 47 documents under `docs/` carry `applies_to: source release 0.1.0` in their frontmatter. They are documentation metadata, not read by the app, and `cmd/doclint` only checks that the key is present — so they will not break a build if left behind, but they are worth sweeping in the same change as a version bump so the docs and the product agree.

### C.3 Backup and Restore

Full procedure and the latest verified restore-drill record: **[`../operations/backup_restore.md`](../operations/backup_restore.md)**. Summary:

- `.\manage.ps1 backup` creates a timestamped, SHA-256-verified backup of every environment's database that currently exists. Do this on a schedule (a Windows Task Scheduler recipe is in the linked doc) — not just before risky changes.
- Restoring is deliberately hard to do by accident: `.\manage.ps1 restore -Env <env> -File <backup>` requires the target server to be stopped and an exact typed confirmation (`RESTORE <environment>`).
- **Perform an actual restore drill monthly**, not just backups — a backup you've never restored from is unverified. Record the date, file, duration, and result (the linked doc shows the format).
- **On a self-hosted Linux server the above does not apply** — that box has its own scripts. Install the nightly job once with `sudo bash /opt/erp/deploy/install_backup_cron.sh` (14-day on-box retention; it also runs one backup straight away to prove it works), and prove restores with `deploy/restore_drill.sh`. **Do not assume this has been done**: a server with no scheduled backup looks exactly like one that has them, right up until you need one. *(This is not hypothetical — a 2026-08-07 inspection of the production droplet found exactly that: an empty crontab, no timer, and two hand-taken dumps that looked at a glance like a working backup history.)*
  - **`deploy.ps1` ships the binary and `public/`, not `deploy/`.** So a script existing in the repo does **not** mean it exists on the server. Check with `ls /opt/erp/deploy/` before assuming you can run one, and `scp` it up if it's missing.
  - **`restore_drill.sh` needs a connection that may create databases, which the application's own role deliberately is not.** A correctly hardened box gives the app role no `CREATEDB`, so the script's default "derive the admin connection from `DATABASE_URL`" fails with `permission denied to create database`. Pass **`DRILL_ADMIN_URL`** instead — typically the local superuser over the unix socket, e.g. `DRILL_ADMIN_URL='postgresql://postgres@/postgres?host=/var/run/postgresql'`. **Do not grant the app role `CREATEDB` to make the drill pass** — that weakens the running system in order to test a backup.
  - **The superuser usually cannot read `/opt/erp/backups`** (`/opt/erp` is `drwxr-x--- erp:erp`), so stage a copy of the newest backup somewhere it can, point `BACKUP_DIR` at it, and **rewrite the `.sha256` sidecar's absolute path to a bare filename** — `sha256sum -c` resolves the path recorded inside the file, so a staged copy otherwise fails its own checksum and reports a perfectly good backup as corrupted. The full working invocation is in **[`../operations/restore_drill_log.md`](../operations/restore_drill_log.md)**.
- **To export a single tenant** rather than the whole database — offboarding, handing a customer their data, or a scoped copy before a risky fix — use `.\manage.ps1 export-tenant -TenantSchema tenant_<name>` (or `deploy/backup.sh --tenant tenant_<name>` on Linux). Same encryption and checksum as a full backup; restore it into a scratch database, never over a live one.

### C.4 Incident Response and Alerting

Full procedure: **[`../operations/incident_runbook.md`](../operations/incident_runbook.md)** — severity levels (P0-P3), escalation contacts, rollback steps, and every log location in one place. Automated alerting is built and only needs `OPS_ALERT_WEBHOOK_URL` set to your real destination — see that doc §3 for exact setup. Four things alert: the system panicking, a nightly backup **failing**, a nightly backup **not existing at all**, and a spike in errors.

**Is alerting actually on?** **Settings → System Status → Incident alerts** says which: **Delivered to …** (alerts reach your webhook and responder), **Delivered - no responder**, **Configured, not sent** (this environment has outbound delivery switched off), or **Never configured** — with the server settings still missing (`OPS_ALERT_WEBHOOK_URL`, `OPS_ALERT_RESPONDER`) and the runbook. On the same screen a backup or restore drill that has never run shows **Never recorded** as a warning, not as healthy.

The third one is worth understanding, because it is the failure the other three cannot catch. A backup job that fails tells you it failed; a backup job that was never installed, or that stopped being scheduled, says nothing — and silence looks exactly like success. The server therefore checks every hour when the last successful whole-database backup was **recorded in the database** (`deploy/backup.sh` writes one row per run to `public.backup_runs`), and alerts if there is none or it is more than 36 hours old, independently of whether any job ran. Single-tenant exports never count as the nightly backup. On a production server (`ENV=production`) "no backup recorded at all" is also printed at every start as finding **SB-026** — it is never silent there. To see the record yourself: `psql "$DATABASE_URL" -c "SELECT finished_at, scope, outcome, size_bytes FROM public.backup_runs ORDER BY finished_at DESC LIMIT 5"`. **Until you set the webhook URL, all four are written to the server log only and nobody is notified.**

### C.5 Connector / Integration Verification

If you're enabling a real Shopify/BigCommerce/Magento connection for a tenant: **[`../operations/connector_live_verification.md`](../operations/connector_live_verification.md)** — the exact credentials format and a script that verifies the connection against the real platform before you trust it.

### C.6 Creating and Removing a Tenant (`tenantctl`)

Creating and destroying tenants is done from the database host with **`go run ./cmd/tenantctl`** (or a built `tenantctl` binary), never through the web app. There is deliberately no admin page for it: no role inside the product should be able to reach `DROP SCHEMA`, and an offline tool cannot be reached from the internet at all.

Three rules apply to every command:

- **Nothing destructive happens without `-yes`.** Without it you get a dry run that prints exactly what would change.
- **Every state change needs `-reason`** (and takes `-actor`, defaulting to your Windows/Linux username). Both are written permanently to `public.tenant_lifecycle_events` — the evidence trail, which survives even the tenant's deletion.
- `DATABASE_URL` picks the database, same as every other tool here.

**Creating a tenant**

```
go run ./cmd/tenantctl provision -tenant acme -reason "signed 2026-09-06, ticket ERP-1421" -yes
```

The schema defaults to `tenant_<id>`; pass `-schema` to choose. The command prints a **one-time admin password, shown exactly once** — it is stored only as a hash and nothing can recover it. Hand it over securely.

That password is **not a standing credential**:

- It **expires 72 hours** after it is issued (`TENANT_BOOTSTRAP_TTL_HOURS` changes the window). After that, login is refused — with the same generic failure as a wrong password, so nothing is leaked — until an operator issues a new one.
- Logging in with it still forces **TOTP enrollment first**, because the tenant admin role is MFA-mandatory. The password alone reaches nothing.
- Every login while it is unrotated returns `must_rotate_password` and the deadline.
- Rotating the password (`Profile → Change Password`, or any other write to it) marks the credential consumed and is recorded.

If the handover credential is lost or has expired:

```
go run ./cmd/tenantctl rotate-bootstrap -tenant acme -reason "handover credential expired" -yes
```

**Checking state**

```
go run ./cmd/tenantctl list                 # every tenant, status, bootstrap state, holds
go run ./cmd/tenantctl show   -tenant acme  # one tenant in full
go run ./cmd/tenantctl events -tenant acme  # the evidence trail
```

`list` flags any tenant still sitting on an unrotated one-time credential — worth checking after any onboarding.

**Pausing a tenant** (non-payment, suspected compromise, customer request) — also one click on the **Control Panel** (§C.8), which runs the same change; the server commands are:

```
go run ./cmd/tenantctl suspend -tenant acme -reason "non-payment, invoice 2026-08" -yes
go run ./cmd/tenantctl resume  -tenant acme -reason "payment received" -yes
```

A suspension stops **new logins and every session already in progress** within ~30 seconds (`AUTH_STATE_CACHE_SECONDS`). Nothing is deleted, and resuming restores service immediately.

**Offboarding a tenant** is deliberately two separate acts, days or weeks apart:

```
go run ./cmd/tenantctl deprovision -tenant acme -reason "contract ended 2026-09-30" -yes
```

Traffic stops at once and the **retention clock** starts (30 days by default; `-retention-days N` for an agreed different period, `TENANT_RETENTION_DAYS` for the policy default). Nothing is deleted by this step.

Then, once the window has elapsed and **after** you have taken and verified a tenant export (§C.3):

```
go run ./cmd/tenantctl purge -tenant acme -backup-ref "tenant_acme_20260930.dump.enc (verified)" -yes
```

The purge **refuses** — and records the refusal — if the tenant was never deprovisioned, if a legal hold is in force, if the retention window has not elapsed, or if no backup reference is supplied. When it does run, it drops the schema and the registry row in one transaction and then **proves the removal**: no registry row, no schema, no hostname mapping, no row in any shared table, no cached session. That proof is printed and recorded.

**Legal hold** blocks any purge regardless of retention:

```
go run ./cmd/tenantctl legal-hold -tenant acme -on  -reason "matter 2026-14, counsel instruction" -yes
go run ./cmd/tenantctl legal-hold -tenant acme -off -reason "matter closed" -yes
```

**Verifying afterwards** (the registry row is gone, so name the schema):

```
go run ./cmd/tenantctl verify -tenant acme -schema tenant_acme
```

**One posture check worth running once per deployment:**

```
go run ./cmd/tenantctl db-privilege -schema tenant_default
```

It reports the database role the application actually connects as. If it says the app is a **superuser**, a single SQL-injection or code-execution bug reaches every database on that PostgreSQL server and the filesystem — fix that at the deployment level (an owner-only role), not in the app.

#### C.6.1 Every client kept in step with the template (Stage 64.1)

Each client has its own copy of the database tables, cloned from `tenant_default` when the client is created. Most upgrades (migrations) change only `tenant_default`, so a client created earlier used to miss whatever came later. On production, `tenant_minn` was missing 16 tables, 11 columns (audit writes failed because of one of them), 53 record types with their fields and permissions, its number series, and every status-change rule.

**This is now automatic.** Every `erp-server -migrate` (which `deploy\deploy.ps1` runs on each deploy) ends by comparing every client schema with `tenant_default` and adding what is missing. Provisioning a new client runs the same step. The deploy output shows one line per client that changed, then `Client schemas in step with tenant_default (N checked).`:

```
  [sync] tenant_minn: 16 tables, 11 columns, 3 indexes, 10 own sequences, 53 doctype_meta, 830 doctype_fields, ...
Client schemas in step with tenant_default (1 checked).
```

What it adds, and what it never touches:

- **Adds** missing tables, columns (with their defaults) and indexes; missing record types and their fields; the default permissions **for record types it adds** (a permission you removed from an existing record type is not handed back); missing number series; and the system seed records — status-change rules, the base currency, TDS sections.
- **Gives each client its own id counters.** A table cloned the old way took its ids from `tenant_default`'s counter; each such column now gets its own counter, starting after its highest id.
- **Never changes or removes** anything a client already has, and never copies business records, users, settings, module entitlements, approval rules or running number counters.
- A `[sync-warn]` line means one item could not be added (for example a unique index over duplicate data) and was skipped; everything else still applied. Fix the data through the Data Console (§C.7), then run `-migrate` again.

---

### C.7 Data Console and cross-client control — platform operators only (Stage 60.6)

**Admin → Data Console** lets a platform operator open any client and fix any record directly. It is for repairs that have no screen of their own; prefer the normal screen whenever one exists, because the console does not re-run the business rules a screen does.

**Who can use it — and every other cross-client screen.** Being a Super Admin is not enough: every client has its own Super Admin. A **platform operator** is a Super Admin whose client and username appear in the server setting `ERP_PLATFORM_OPERATORS`, a comma-separated list of `client:username` pairs:

```
ERP_PLATFORM_OPERATORS=default:admin
```

Set it in the server's environment (`/etc/erp/erp.env` on the droplet) and restart the service. It cannot be set from inside the app, so no client user can grant it to themselves. Use a login that belongs to you, not to a client (the `default` client's admin, or a dedicated user). Only operators can: use the Data Console; list clients; see **Tenant Usage**; change a client's modules or plan (**Tenant Entitlements**); set a client's hostname; create clients; pause or resume a client and set its free trial (**Control Panel**, §C.8); create, reset or delete sandbox clients. A client's own Super Admin may still **read** its own client's module list and version record, never another client's. Anyone else gets `PLATFORM-0317`.

**One table per transaction.** The **Table** list shows every record type of the chosen client as its own table, grouped by module — **PurchaseOrder**, **GRN**, **Vendor**, **Item**, **Location**, **SalesInvoice**, **TransferOrder** and every other one, each with its row count. Opening one shows one column per field (headed by the field's label, e.g. *PO Number*, *Vendor*, *Status*), plus the record's number (`id`), status and timestamps. Any value a record carries that its record type no longer defines is shown under **Other data** rather than hidden. Below the record types are the raw **Database tables** (see "Where things are").

**Fixing a record.** Choose the **Client** and the **Table**, type a number, code or name in **Find** (e.g. `PO/BDR/26-27/00001`), and click **Edit** on the row. Each field gets the right box: a dropdown for a choice field, a record picker for a field that points at another record (it shows names, saves the record's code), a large box for line items and other JSON. Tick **empty** to clear a field. **Save changes** asks you to confirm, then writes only the fields you changed. Numbers are saved as numbers and yes/no fields as true/false; a value that does not fit the field (letters in a price, broken JSON) is refused. The record's number cannot be changed.

**What it protects.**

- **Hidden values** show as `••••••` and cannot be changed: passwords, password-reset and MFA secrets, API and channel credentials, recovery codes, and **customer personal details** — phone, email, address, PIN code, PAN/Aadhaar, date of birth, bank details, and a Customer's name. Inside line items and other JSON the same keys are hidden; when you save, every hidden value keeps what is stored. Searching never looks inside hidden values.
- **Record numbers and table keys** cannot be changed, and a raw table with no key cannot be edited at all.
- **Audit tables** (`audit_*`, `system_error_logs`) are view only, so the trail cannot be rewritten — by anyone.
- **Every change is recorded** in that client's **Activity Log** as `PLATFORM_DATA_EDIT`, with the record or table, its number, the fields changed and their before/after values (hidden values stay hidden), and also in the operator's own client log.

**Finding the log for a data issue.** Every client keeps its own logs, and the console opens them for either client: **`system_error_logs`** holds every server error raised while that client was working, with its module and correlation id (rows are in id order, so the newest are on the last page), and **`audit_logs`** holds every change, including each console edit (`PLATFORM_DATA_EDIT`). Pick the client, open the table, and search by record number or correlation id; the same id appears in the error message the user saw. Both are view only.

**Where things are (raw database tables).** Every record type above is stored in the one `documents` table (by `doctype`); the record-type tables are the easier way to fix them. The other tables: `doctype_fields` / `doctype_meta` (each record type's fields and labels), `prefix_configs` and `sequence_counters` (number series and their running counters), `system_settings` (Configuration), `role_permissions` and `field_permissions` (access), `bin_stock*` and `inventory_*` (stock), `gl_postings` / `gl_accounts` (accounts), `approval_*` (approvals). Editing stock or ledger rows directly can leave stock and books out of balance; correct those with an adjustment or reversal through their screens where one exists.

### C.8 Control Panel — every client at a glance, pause and free trial (Stage 65.5, 65.6)

**Settings → Control Panel** is the platform operator's one page (operators only, the same `ERP_PLATFORM_OPERATORS` setting as §C.7; anyone else sees a short note saying so). Two changes are made right here — **pausing** a client and its **free trial** (below); everything else opens the screen that makes the change.

**The four cards at the top.**

- **Running release** — the version, the git commit and the release date of the binary that is serving the page right now, and the environment (`production` on the droplet).
- **Clients** — how many, how many are sandboxes, and how many have a problem (a missing or unusable schema — shown in red on the row).
- **Hosted at** — the domain each client's own address sits under (`TENANT_BASE_DOMAIN`, e.g. `wholeops.in`).
- **Last backup** — the most recent whole-database backup (time, outcome, size) from the nightly job's ledger; if it failed, the time of the last good one. "None recorded" means the backup job has never written its record on this server.

**The Clients table, one row per client.** Name, client id and schema; **Status** (active / suspended…, a *sandbox* or *legal hold* tag, and "admin not signed in yet" while the client's one-time admin login is unused); **Address** (opens it in a new tab — "main address" when the client has none of its own); **Created**; **Version** ("follows release" unless the client is pinned to one); **Modules** — how many are on and off; click it to list them; **Users** (active), **Records**, **Last activity** (its newest Activity Log entry) and **Errors (24h)** with the time of the last one (open them in the Data Console's `system_error_logs`, §C.7).

**Actions on each row.** **Modules** opens **Tenant Entitlements** with that client chosen — turn a module on or off there, or apply a plan. **Data** opens the **Data Console** on that client. **Attributes** gives the client its product attributes from the industry library (§C.9). Browser **Back** returns here. **Pause / Resume**, **Trial** and **Mark paid** are below. The row of the client you are signed in to says *your client* instead of those buttons: pausing it, or letting a trial run out on it, would lock you out of this page.

**Pausing a client** (non-payment, a customer's request, suspected misuse). Click **Pause**, type the reason, and click **OK**. Everyone at that client is signed out within a minute and cannot sign in again; nothing is deleted. The row shows *suspended* with your reason under it. **Resume** (again with a reason) lets them sign in straight away. Both go into the client's history (`tenant_lifecycle_events`) with your name, exactly as the server command `tenantctl suspend` / `resume` does (§C.6).

**Free trial.** A client on trial works normally until its end date, then is **paused automatically** — the same as **Pause**, nothing deleted — until you act. No job has to run: the end date is checked on every sign-in and every request.

- **Start a trial:** click **Trial** on the row, enter the number of days from today (30 is filled in), **OK**. The Status column then shows a *trial* tag and "trial ends 16 Nov 2026 · 30 days left". A new client can be given its trial when you create it: **Tenant Entitlements → Provision New Tenant → Free trial (days)**, 30 by default — clear it (or enter 0) for a client who is paying from day one.
- **Extend it:** click **Extend trial** and enter the days from today. This also gives a client whose trial has ended its access back at once.
- **They have paid:** click **Mark paid** and confirm. The trial ends with no end date; the client keeps full access.
- **When a trial ends:** the row shows *trial ended* and "paused by trial end" with the time, and the **Clients** card counts it under *paused*. Its users see the same sign-in failure as a wrong password (deliberately — the sign-in screen never says why a client is closed), so tell the client before the date if you can. Extend the trial or mark it paid to reopen it.

The **Clients** card counts how many are *on trial* and how many are *paused* (by you or by a trial end). A sandbox has its own expiry and takes no trial.

**Releases** lists the last ten entries of the release ledger (`public.deployments`: when, environment, version, commit, who, status). Only `promote.ps1` writes to it; releases shipped with `deploy\deploy.ps1` do not, so the running release in the first card is the reliable answer to "what is live".

### C.9 A client's product attributes, from the industry library (Stage 81)

Each client gets its product attributes from you, once — Colour, Polish, Size, Stone Colour for a jewellery client — each with its dropdown values already filled in. The client then uses them everywhere (the Item's variant options, PO designs, prints, PIM) without anyone being asked to invent an attribute. They stay with the client: it can switch an attribute off and remove values, but never delete one.

The attributes come from the **industry attribute library** that ships with the system — your workbook *ERP_Industry_Product_Attribute_Master.xlsx* (10 industries, about 670 attributes, about 190 dropdown lists), converted into the server by `go run ./cmd/genattrlib -xlsx "<path to the workbook>"`. Edit the workbook, rerun that command, and the next release carries the new library. Clients that already have attributes keep theirs; the next time you add from the library they get only what they do not have yet.

**To give a client its attributes:**

1. **Settings → Control Panel**, and on the client's row click **Attributes**.
2. **Industry** — the client's own industry is chosen (Jewelry when it has none yet). Pick another to see its list.
3. Three lists, decided by the workbook's own *Variant Driver* and *Value Scope* columns:
   - **Variant attributes** — what tells one sellable combination from another. The workbook's definite variant drivers (Primary Colour, Metal Tone, Metal Type, Purity, Ring Size, Bangle Size, Chain Length…) are ticked; the ones that are variant drivers only for some categories (Surface Finish — polish, Stone Colour, Plating, Clasp…) are listed but not ticked, so you tick what this client actually varies by.
   - **Product information (PIM)** — descriptive attributes for enrichment. All are ticked when the client has the PIM module.
   - **Not added** — attributes the ERP already keeps elsewhere (GTIN, HSN, shelf life, weight… on the Item) and ones recorded per batch or serial unit, not per item.
4. Each row shows its dropdown values, the workbook's note and whether the client **has it** already. **Use as** moves a row between Variant and Product information. A row marked *Similar: …* is not ticked because the client already has an attribute with a similar name — tick it only if you really want both.
5. **Add to <client>** and confirm. The summary says how many were added and how many it already had.

**What the client sees afterwards** (User Guide §8.1.2):

- **Item form → Variant Options** offers exactly the client's variant attributes and their values — there is no "Other…" to type a new one.
- **Purchase Order → Add by item name** offers the same attributes; a buyer can still add a *value* there (a new colour from a vendor), but not a new attribute — that is refused with *PIM-0691*.
- **PIM → Attribute Definitions** lists every attribute with **Used for** (Variant / Product information). The client can set **Status** to *Inactive* to switch one off, and remove or add values as chips. An item that already carries a removed value keeps it. Deleting an attribute that came from the library is refused (*PIM-0690*): its code and the *From the attribute library* field cannot be edited either.
- Adding from the library again never touches what the client has — an attribute it switched off stays off, values it removed stay removed.

**Overlap with the Jewellery industry profile.** That profile also puts free-text **Color**, **Polish**, **Size** and **Stone Type** fields on the Item. They are left as they are — clients already hold data in them — and work alongside the variant attributes. Which of the two a jewellery client should use going forward is open (checklist 81.6).

## Part D — Developer / CTO Level

### D.1 Deployment (`promote.ps1`)

```powershell
.\promote.ps1 -From dev -To test     # promote dev's current commit to test
.\promote.ps1 -From test -To live    # promote test's current commit to live
.\promote.ps1 -Rollback -Env live    # roll back to the previous known-good deployment
```

`promote.ps1` refuses to promote a "dirty" (uncommitted) tree, runs the full build/vet/test gate first and refuses to promote on any failure, applies any pending database migrations to the target, and records every promotion (and rollback) in a `deployments` table — `.\manage.ps1 fleet-status` reads from that table.

### D.2 Building and Testing

```powershell
# Build for local dev (keeps debug symbols)
& "$env:USERPROFILE\go-portable\go\bin\go.exe" build -o erp-server.exe ./cmd/server

# Build a stripped release binary (smaller, no debug symbols)
& "$env:USERPROFILE\go-portable\go\bin\go.exe" build -ldflags="-s -w" -o erp-server.exe ./cmd/server

# Run the full test suite (use -p 1: a known cross-package DB timing issue
# can cause a false failure without it - see micro_checklist.md's Stage 14
# testing note)
& "$env:USERPROFILE\go-portable\go\bin\go.exe" test ./... -p 1

# Static analysis
& "$env:USERPROFILE\go-portable\go\bin\go.exe" vet ./...
```

CI (`.github/workflows/ci.yml`) runs all of the above plus a vulnerability scan (`govulncheck`) and a secrets scan (`gitleaks`) on every push automatically.

### D.3 Project Layout

See `README.md`'s "Project Structure" section for the full current map. In short: `cmd/server/main.go` is the real entrypoint (a thin launcher); `internal/server/` holds all HTTP handlers and middleware, split into files by domain; `engines/` holds business logic (finance, inventory, approval workflow, etc.) as its own Go package; `db/` holds the connection pool and every SQL migration file; `public/` is the hand-written frontend, served as static files.

### D.4 Architecture Reference

- **[`../ERP_BLUEPRINT.md`](../ERP_BLUEPRINT.md)** — the full project snapshot: scope, architecture, build history, known gaps. Read this first for orientation.
- **[`../architecture/framework_architecture.md`](../architecture/framework_architecture.md)** — the metadata-driven Record Type engine every module is built on (internal/architectural docs still use the framework's original technical name, "DocType", when describing the underlying database tables and API shape — the UI and admin-facing docs use "Record Type" throughout).
- **[`../architecture/architecture_evaluation.md`](../architecture/architecture_evaluation.md)** — why Go/PostgreSQL/schema-per-tenant, with the cost/footprint reasoning.
- **[`../requirements/PRD.md`](../requirements/PRD.md)** — module-by-module functional requirements and built-vs-specified status.

### D.5 Security Posture

JWT bearer auth with expiry, TOTP MFA for privileged roles, server-side RBAC on every document operation, parameterized SQL throughout (no string-built queries), a request body size cap, per-category rate limiting, a CORS allowlist, HMAC-verified inbound webhooks, AES-256-GCM encrypted-at-rest channel credentials. Historical hardening record: **[`../operations/hardening_roadmap.md`](../operations/hardening_roadmap.md)** (closed, historical reference — not an active backlog).

**Every API request passes one shared check before it reaches any screen's logic** (the request envelope, Stage 49.4.4; `internal/server/request_envelope.go`). An integration or script that breaks a rule gets a clear error with a `correlation_id` to quote to support:

| Rule | Limit | Error |
|---|---|---|
| Method | GET, HEAD, POST, PUT, PATCH, DELETE, OPTIONS | 405 `GLOBAL-0303` |
| Body type | JSON or multipart file upload; a body with no `Content-Type` is read as JSON. The import hook also takes CSV/plain text; CSP reports take their browser types. Compressed bodies are refused. | 415 `GLOBAL-0304` |
| Body size | 2 MB by default. Item/bulk imports and import templates allow 16 MB, media upload 10 MB, bulk media upload 100 MB, and the import hook 5 MB. | 413 `GLOBAL-0305` |
| Headers | at most 100 fields and 32 KB | 431 `GLOBAL-0306` |
| JSON shape | one value, nested at most 32 levels, lists of at most 50,000 items, text values of at most 1 MB (2 MB for sticker templates, whose logos live in one field), numbers within ±9,007,199,254,740,991 and at most 64 characters long, and no field repeated in the same object | 400 `GLOBAL-0307` |
| Query string | each parameter at most once (`?status=a&status=b` is refused, because servers and proxies disagree on which value applies) | 400 `GLOBAL-0307` |
| Time | 55 seconds per request (the server cuts connections at 60) | 504 `GLOBAL-0308` |

A file at the import row limit (`platform.max_import_rows`, 20,000 by default) previews and imports well inside the time limit: a 20,000-row Item file took about 5 seconds each way in testing (Stage 49.4.4a). If you raise that setting far above the default, or a user still reports `GLOBAL-0308` on an import, have them split the file, as USER_GUIDE §8d.5 says.

**Rate limits and the per-tenant request cap (Stage 47.11.1).** Each API type has a per-minute budget, for example login 5, search 100 and reports 60, answered with 429 `SEC-0280`. Each signed-in session gets its own budget. Everything else from one address shares that address's budget, including a request carrying a token this server did not issue or that has expired. Separately, each tenant may have `platform.per_tenant_max_concurrent_requests` requests in flight at once (default 15). Both are kept in the server's memory and stay bounded:
- The limiter forgets callers a minute after their last request and never tracks more than 100,000 at once.
- If a flood of new addresses fills that table, new callers share one budget per API type until it drains. Callers already being tracked keep their own budget. The server log shows `[RATE-LIMIT] key table full` while this happens.

If you run more than one server process behind a load balancer, each process keeps its own counts. A caller spread across N processes can make up to N times a budget per minute, and each process applies the tenant cap to its own database connections. That is intended: these limits slow abuse and protect each process. The protections that must hold across every process are in the database: account lockout after failed logins, MFA and reset tokens, and session revocation.

Two additions worth knowing about (Stage 29.8):

- **Sessions are re-validated per request, not just at login.** Token claims are frozen when issued, so the middleware re-reads the user's active-status/role/location on each request (30s cache, `AUTH_STATE_CACHE_SECONDS`). Deactivating or demoting someone takes effect on their open session. A database failure here returns a retryable 503, deliberately *not* a 401 — a replica hiccup must not sign everyone out.
- **The signing key can be rotated without logging everyone out.** Set `JWT_SECRET_2` alongside `JWT_SECRET_1`; the highest number signs new tokens and every configured key still verifies. Wait one full token lifetime (`JWT_EXPIRY_HOURS`), then delete the old one. Full procedure in `deploy/erp.env.example`.
- **Audit evidence has its own signing key, `AUDIT_SIGNING_KEY`** (exactly 32 characters; rotate with `AUDIT_SIGNING_KEY_<n>` the same way). Rotating the JWT key above no longer touches audit evidence. Until 2026-10-09 audit rows and checkpoints were signed with the JWT key, so rotating it made all earlier evidence look tampered. Evidence signed that way still verifies as long as its JWT key stays configured. Set `AUDIT_SIGNING_KEY` in `/etc/erp/erp.env` and keep it with your other keys. If it is unset, the server generates one on its own disk (finding **SB-025**), and a database restored onto another server cannot verify its audit evidence.

### D.6 Extending the System

- **New master/transaction types**: use Database Schema Design (§B.3) — no code required for the common case.
- **New business logic**: add to `engines/` as its own file, following the existing one-file-per-module convention.
- **3rd-party integrations**: a scoped, read-only extension framework already exists (`extension-sdk/`, self-contained, meant to be handed to an external developer) for hooking into the platform without granting full core access.

#### D.6.1 Public API credentials (Stage 38 foundation)

These are different from extension tokens and browser sessions. Extension tokens are short-lived and read one doctype; a public API credential is a durable integration identity with explicit scopes, expiry, rotation and immediate revocation. It never carries a human role.

After `db/migrations_stage38_2_api_credentials.sql` is applied, a Super Admin can issue/list/rotate/revoke credentials through the private `/api/v1/admin/api-credentials` endpoints documented in `../specs/public_api_v1.md`. A newly issued or rotated key is shown once and the response is marked `Cache-Control: no-store`; copy it directly into the integrator's secret store. The database retains only a one-way digest and an operator-visible prefix, so a lost key cannot be recovered—rotate it.

The curated `/api/public/v1` routes (items, inventory, order status, notification delivery receipts) accept these credentials, behind the per-credential limiter (38.3) and the idempotency contract (38.5). Do not try to send an API key to the private `/api/v1` application routes; it is not a user session and will be rejected.

**The API access screen (Stage 38.2d / 55.21).** **Settings → Extension Hooks**, below the hooks, has **API access**: the list of every key and OAuth client with its type, client ID / key prefix, scopes, status, expiry and last use. **+ New API access** opens the form:

1. **Name** — who or what will use it (e.g. *Shopify stock sync*).
2. **Sign-in type** — **API key**: the integration sends the key on every call (`Authorization: Bearer erp_v1_…` plus `X-Tenant-ID`). **OAuth client**: for platforms that expect OAuth2 — the integration posts its client ID and client secret (HTTP Basic, or `client_id`/`client_secret` in the body) with `grant_type=client_credentials` and `X-Tenant-ID` to `/api/public/v1/oauth/token`, and gets back a token valid for 15 minutes (`Authorization: Bearer erp_at_…`). An optional `scope` narrows a token to some of the client's scopes. There is no password grant and no refresh token; an OAuth client's secret is refused if sent directly as a bearer.
3. **Expires on** (optional) and the **Scopes** — tick only what the integration needs; each endpoint names the one scope it requires (`notifications:write` is for a notification provider's delivery receipts).

**Issue** shows the secret once (and, for an OAuth client, its client ID and the token URL) — copy it into the integrator's secret store now. On the list: **Rotate** issues a new secret and immediately ends the old one *and every token it obtained*; **Revoke** ends the credential and its tokens for good; **Recent calls** lists the latest requests with their status, outcome and correlation ID (never the secret) — the first place to look when an integrator says "it stopped working". Expired token digests are swept with the API logs.

**Notification delivery (Stage 73.2.13).** Order and return notifications no longer report "Sent" on a fire-and-forget call. Each one is written **Queued** together with its delivery job; the job POSTs to the channel's **Notification Channel Config** webhook (HTTPS only, public addresses only) with a `delivery_id` and an `X-Notification-Delivery` header, retries with backoff on a network error, timeout, HTTP 408/429 or 5xx (**Retrying**), and stops at **Failed** after the last attempt — **Replay** on the job in **Integration Logs → Async Jobs** resends it. A 2xx answer makes it **Accepted**; if the provider's answer says delivered or failed, that is recorded straight away. Otherwise give the provider (Zapier, Make or your own service) an API key or OAuth client with `notifications:write` and have it call `POST /api/public/v1/notifications/{delivery_id}/delivery-status` with `{"status":"delivered"|"failed","provider_ref":"…"}` (plus an `Idempotency-Key`). Delivered is final; a repeated receipt is harmless. Every step is kept in the notification's history.

#### D.6.1a PIM setup and navigation (Stage 55.11)

The PIM rail now groups its 21 existing screens under **Products**, **My work**, **Media**, **Channels & publishing** and **Advanced setup**. The Product Dashboard carries the ordered path: Item master → Product Family → Attribute Definitions → required Family Attributes → assign Item family → Workbench content/media → completeness report → optional channel mappings/validation → publish from Workbench. This mirrors the module handbook. The grouped navigation does not change server permissions or create a new product master. A local ready score is not external channel acceptance; keep a channel-response and exception review before treating an item as live.

The Workbench's selected-channel check now uses the same publish-readiness rules as the queue gate (completeness, category map and channel validation rules). It disables Publish until locally ready and names missing fields; an external connector still requires its own receipt check. The manual UI walkthrough used a Product Family Design ID, an Item Combination SKU, a mandatory attribute and a separate ProductContent approval. A Store Manager cannot approve ProductContent merely because they can read it; the shipped approval rule requires **HR/Admin**, and the checker must differ from the submitter.

Product Family is a required core completeness check; a family-less Item cannot become publish-ready by filling unrelated fields. **Min Images** counts active customer-facing image assets in Main, Gallery, Variant Image or Lifestyle roles, not PDF certificates, Internal QC assets or video. A custom validation message retains its rule type prefix so the Workbench can show the relevant correction action. The isolated browser run verified rule block → Add an image → upload → ready; it does not prove a marketplace accepted the media.

For mapped imports, **Advanced setup → Import Templates** has a row action to inspect the resolved mapping, preview a CSV without writes, and import only a zero-error preview of the selected file. The resulting import job ID and authenticated error-row download are shown in the dialog. Rows that fail stay on the job: **Fix and resume** (on the import result, the **Import Job** list lookup, and **My work**) downloads them with their reasons and imports only the corrected rows, after a **Check** that saves nothing (User Guide §3, *When some rows of a Bulk Import fail*). Every **Import Schedule** has an **Owner**: its imports run and are recorded as that person, with their field-write restrictions (blank = whoever created the schedule, never "system"); a missing or inactive owner stops the run with the reason in **Last Run Status**, and a failed run lands in the owner's **My work**. **Supplier Submissions** Draft rows now have Submit for Approval. The shipped supplier-submission rule requires a **Super Admin** checker who differs from the maker; approval copies content to ProductContent as a Draft. ProductContent then needs its own separate **HR/Admin** checker and will load into Workbench for editing. The supplier-login/vendor isolation path itself remains to be browser-tested. Schedule owners and resume were built and browser-tested on 2026-10-18; the Stage 55.11 Done bar still waits on the Supplier-login run.

#### D.6.2 PIM tasks and workflows (Stage 36.2)

Four doctypes arrive with `db/migrations_stage36_2_pim_tasks.sql`: `PIMTaskTemplate` and `PIMWorkflowDefinition` (Masters, authored on the PIM → Task Templates / Workflows tabs) and `PIMTask` and `PIMWorkflowRun` (Transactions, driven from PIM → My Work). The migration is additive throughout and includes the standard backfill loop, so tenants provisioned before Stage 36.2 receive the same doctypes, fields, permissions and indexes as new ones.

**Default permissions**, adjustable per role as usual under Settings → Roles:

| Role | Templates & Workflows | Tasks | Runs |
|---|---|---|---|
| Super Admin | full | full | full |
| Store Manager | read/create/update (no delete) | full | read/create/update |
| Cashier | none | read + update (progress their own work) | read only |

Deleting a workflow definition is withheld from Store Manager on purpose: removing a definition out from under live runs is an authoring action, not an operating one.

Three things worth knowing before you support this module:

- **A task is not an approval, and the two are deliberately separate.** No task action can move a document's approval state, and completing a task is not an approval decision. If someone reports "the approval didn't happen when I finished the task", that is the design, not a bug.
- **`Done` and `Cancelled` are terminal.** A finished task cannot be re-opened, because finishing it may already have advanced a workflow onto its next stage. The supported answer is **Create follow-up**, which is offered in the task's own dialog.
- **Editing a live workflow definition can strand its runs.** A run whose stored stage group no longer exists in the edited definition is **stopped with a named reason** rather than restarted from the beginning — restarting would re-create every task the product had already been through. Cancel and restart such runs deliberately. Prefer creating a new definition over reshaping one that has runs in flight.

`GET /api/v1/pim/assignable-users` is a deliberately narrow endpoint returning active users' **username and role only**, gated on update rights over `PIMTask`. It exists because `/api/v1/admin/users` is Super Admin gated and a Store Manager still needs to see who they may assign work to. Do not widen it into a general user-listing endpoint.

For monitoring, three registered reports are available in the Report Catalog: **PIM Task Workload by Assignee**, **PIM Overdue Tasks** and **PIM Stalled Workflow Runs**. The last is the one to schedule — a blocked run has no open tasks by definition, so it never surfaces in anyone's inbox on its own.

### D.7 Governance Model

Per this project's own planning references: a small central team owns the core kernel and release process; module owners own their business rules and user acceptance; client/industry-specific needs are handled through configuration first (Database Schema Design, feature flags), scoped extension hooks second, and a core code change only when a genuinely reusable platform capability is missing — never a per-client fork of the codebase.

---

## Glossary (technical → plain language, both directions)

| Term | Plain-language meaning |
|---|---|
| **Tenant** | One business's private, isolated slice of the shared system. |
| **Schema-per-tenant** | The technical method keeping each tenant's data physically separate in the database, not just filtered in the app. |
| **Record Type** | A "kind of record" (e.g. Purchase Order, Item) defined as configuration, not hardcoded. |
| **RBAC** | Role-Based Access Control — what a user can do is determined by their role. |
| **JWT** | The digital "ID card" (token) a logged-in user's browser presents on every request. |
| **MFA / TOTP** | A second login check via a time-based code from an authenticator app. |
| **GL / Ledger** | The accounting record; "double-entry" means every transaction has a matching debit and credit that must balance. |
| **Idempotent / idempotency key** | A safeguard so that if the same request arrives twice (e.g. a network retry), it only takes effect once. |
| **Outbox pattern** | A way of making sure a slow external system (like a payment gateway) never blocks or breaks a user-facing action. |
| **Correlation ID** | A tracking code that lets you follow one user action across logs and error reports. |
| **Worktree** (git) | A separate folder holding a different checked-out version of the same repository — how `test`/`live` stay independent of `dev` without being separate copies of the whole repo history. |
| **CI (Continuous Integration)** | Automated checks (build, test, security scan) that run on every code change, before it's trusted. |
