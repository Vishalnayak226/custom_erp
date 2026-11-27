---
doc_id: DOC-C44AC5B356-05
title: Admin Guide: Security events and operations
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

# Admin Guide: Security events and operations

Part 5 of 6 of the [Admin Guide](admin-guide.md). Previous: [Finance, integrations and go-live](admin-guide-finance-integrations-and-go-live.md). Next: [Developer reference and glossary](admin-guide-developer-and-glossary.md).

*Continues [Part B — Day-to-Day Operation](admin-guide-foundation-users-and-settings.md#part-b--day-to-day-operation).*

### B.4a Settings → Monitoring → Security Events (Stage 49.11-49.13)

Administrators (Super Admin, Admin) see **Security Events** under Settings → Monitoring. It covers this business only, never another client.

**The top row.**
- **Checks last ran**: the security checks run every 5 minutes. Red over 15 minutes means they stopped.
- **Audit evidence**: *Sealed*, or *Unsealed N h* when the hourly checkpoint has not run for 3 hours while events waited.
- **Verifier failures (7 days)** and **Audit writes lost**: either above 0 needs a look (`incident-runbook.md` §7.8).
- **Verifier last ran**: the audit-chain verifier's last run and its outcome - *Passed*, *Failed* or *Could not run*. A failure is also an `AUDIT_VERIFY` row in the audit log, as are a sensitive list or report read (`SENSITIVE_READ`) and a refused or flagged invariant (`INVARIANT`).
- **Urgent alerts sent**: *Not set up* until the operator sets `OPS_ALERT_WEBHOOK_URL`. Until then, urgent detections show only here.
- **Abuse checks that follow your size (Stage 49.5.4).** Under Settings → Configuration → Security, **Busy-client level for abuse checks** (Off by default) raises the limits of the burst checks (returns, discounts, offline sales, stock adjustments, voids, refusals, exports) up to 5 times while your own 14-day average of new documents a day is above that level. **Abuse check limits by role** takes pairs such as `Store Manager=200, Cashier=75`: 200 means that role needs twice as many events before a check fires. Blank keeps the fixed limits shown above.
- **Test alert delivered** (only when **Send a test alert every** is on, under Settings → Configuration → Security; Off by default, 24 hours, 3 days or 7 days): the time a message marked TEST last got through the alert webhook. A failed test is tried again hourly and raises *The scheduled test alert was not delivered* (Review) once a day. A test is never sent as an urgent alert.
- **Exports**: *Open* or *Frozen*.
- **Integrations**: *Running* or *Frozen* (below).

**Detections (14 days).** Each row says what happened, who owns it, the first thing to do, and the runbook section. It also names the audit rows behind it (action, user, window and up to three references for **Trace a request**). *Urgent* rows also page the operator; *Review* rows wait here. A detection does not repeat for the same user, vendor or store within its window (1 hour to 1 day). **Run checks now** runs them at once, after a fix or for a drill. The checks:

| Detection | Fires on | Level |
|---|---|---|
| Password spray or distributed guessing | a *LOGIN_ABUSE* row (§B.2 sign-in) | Urgent |
| Many wrong passwords | 25 wrong passwords in 15 minutes | Review |
| Several accounts locked | 3 locks in an hour | Review |
| One user refused again and again | 10 permission refusals by one user in 10 minutes | Review |
| The scheduled test alert was not delivered | A test alert failed to reach the webhook | Review |
| A session from another client used here | any | Urgent |
| Many role or user changes by one person | 5 in 10 minutes | Review |
| Many exports by one user | 20 exports or report downloads in 10 minutes | Urgent |
| Many cancels, voids or price overrides by one user | 10 in an hour | Review |
| Background jobs giving up | 5 dead-lettered jobs in an hour | Review |
| A disabled user acted after being disabled | any | Urgent |
| Audit evidence failed or could not be verified | the verifier's failure | Urgent |
| Audit checkpoints stopped while events waited | 3 hours without a checkpoint | Urgent |
| Many returns by one user | 10 return requests in an hour | Review |
| Many large bill discounts by one cashier | 10 bills in an hour above **Discount needs a reason above** (Settings > Point of Sale) | Review |
| Many sales sent as offline by one cashier | 20 in an hour | Review |
| Many stock adjustments by one user | 10 posted or reversed in an hour, posted cycle-count variances included | Review |
| Many sales below zero stock at one store | 5 negative-stock records in an hour | Review |
| A vendor paid soon after its bank account changed | a payment within 3 days of a bank account or IFSC change — made on screen, by CSV import or by the Setup Workbook | Urgent |
| The same vendor invoice entered twice | one vendor, the same number ignoring case, spaces and punctuation | Review |
| Documents split to stay under an approval limit | 2 or more by one person for one party in a day, each between half the approval limit and the limit, together over it | Review |
| A closed accounting period was reopened | any | Review |
| An approval rule was deleted | any | Review |
| Many configuration changes by one person | 15 settings, approval rule, number series or API key changes in 30 minutes | Review |
| Many webhook calls with a bad signature | 10 in 15 minutes, any channel or courier | Urgent |
| Many repeated webhook deliveries | 20 in 15 minutes | Review |

**Webhook and stock detections (Stage 49.11.4).** Every inbound webhook (Shopify, BigCommerce, couriers) refused for its signature is a `WEBHOOK_INBOUND` / `SIGNATURE_REFUSED` audit row, and every repeat of a delivery already handled is `WEBHOOK_INBOUND` / `REPLAYED`. Each names the source (e.g. `shopify:web`) and the address it came from, never the body. Nothing refused or repeated is processed. A bad-signature burst usually means the platform's webhook secret changed: save the new one on the channel. Repeats from an address that is not the platform's mean a signed delivery was copied: change the secret. At most 100 such rows per kind per 15 minutes are kept for a business. A posted cycle-count variance is now an audit row too (`POST_CYCLE_COUNT_ADJUSTMENT`) and counts toward *Many stock adjustments by one user*.

**Where an event came from (Stage 49.11.2).** A security event written during a request now also holds the request's address, a short device name (*Chrome on Windows* - never the full browser string), the permission the request needed and the version of the permission rules that decided it. **Trace a request** shows the address and device after the user; the **Evidence pack** carries all four. The address and device are personal data: they are cleared from events older than 180 days by the hourly retention sweep (kept under a legal hold), and they are not part of the event's signature, so clearing them never breaks the evidence.

**To review.** Administrators without two-factor, accounts with no sign-in for 90 days (or for the period set in *Disable accounts not signed in for*, marked *auto-disable on* — see Users above), API keys that expire within 30 days or were never or not recently used, and background jobs that gave up in the last 7 days. **Sign-in friction (14 days)** counts what got in people's way: accounts and store-phone PINs locked by wrong attempts, password resets (asked for, or done by an administrator), lost authenticators (a recovery code used, or two-factor reset by an administrator) and the *No* answers to "Was this helpful?" on the sign-in help articles (*Your first sign-in*, *Security, Roles & Approvals*). A rising count means a setting is too strict or people need help - look before tightening anything further. The security scorecard points here for this row.

**Trace a request.** Paste the reference from an error message (or from a detection). It lists every audit row, system error, background job and integration event that request left.

**Evidence pack.** Choose From and To (at most 31 days) and **Download**, then type a passphrase of at least 12 characters twice. The file saves as `evidence-<from>-to-<to>.zip.enc`, encrypted with that passphrase (AES-256, 49.12.4); the passphrase is not stored, so send it to the recipient separately. They open it with openssl (it comes with Git for Windows, macOS and Linux): `openssl enc -d -aes-256-cbc -pbkdf2 -iter 600000 -md sha256 -in evidence.zip.enc -out evidence.zip`, then type the passphrase. The zip holds the window's audit rows with their signatures, system errors, audit checkpoints, the releases that were live, every applied migration's checksum, `config.json` (this business's settings as they stood when the pack was made, with free-text secrets shown as [REDACTED]; its SHA-256 is the manifest's `config_hash`, so a later change to the settings is provable), and `manifest.json` with each file's SHA-256, signed in `manifest.sig`. Building it changes nothing. It is itself recorded as *EVIDENCE_EXPORTED*. Keep it somewhere safe; it can hold personal data.

**Seal old rows (47.7.4).** In the Evidence pack panel, **Seal old rows** writes one signed checkpoint over audit rows that were written before row signing existed. It claims nothing about who wrote them: the coverage statement still says those rows are not counted as verified. What it adds is that any later change to them shows, because their digest is now fixed. It is an HR admin or Super Admin action (`POST /api/v1/admin/audit-logs/seal-legacy`), it asks to confirm, and it says **Nothing to seal.** when no new rows exist, so pressing it twice is safe.

**Token digests are hidden from everyone.** The stored digest of a catalogue share link (`PIMCatalog.share_token_hash`) and of an import schedule's hook (`PIMImportSchedule.hook_token_hash`) is not shown to any role, Super Admin included, and cannot be written through a screen or the API. Other secrets, such as a webhook's `secret`, stay readable by Super Admin as before.

**Emergency.** Each button asks for a reason (recorded in the audit log) and **your own password**:
- **Sign out everywhere** (choose a user): every session of that person ends at their next click, and their remembered devices must enter the authenticator code again. Their password does not change; reset it from Users if it may be known.
- **Sign out everyone**: the same for every user of this business except you.
- **Freeze exports**: every export and report download for this business is refused (*Exports are paused…*) and logged, until you **Unfreeze exports**. Scheduled sends stop too: a PIM feed, a scheduled report or a dashboard digest skips its run and shows *Refused*, a webhook carrying one that was queued before the freeze is not sent, and unfreezing sends no backlog. Audit evidence exports stay open. The setting behind it is Settings → Security → *Freeze exports (emergency)*. Use this tab instead, so a reason is recorded.
- **Freeze integrations**: every outside call for this business stops at once - connector and courier calls (reads too), webhook deliveries, extension hooks, emailed documents - and its inbound webhooks (Shopify, BigCommerce, couriers, CRM) are answered *423 Locked* with the header `X-Refused: INTEGRATIONS_FROZEN` and logged, so the sender retries later. Nothing is lost on this side: jobs wait and show as simulated with the reason. **Unfreeze integrations** puts it all back. Another business on the same server is not touched, and platform sign-in mail still goes. Settings → Security → *Freeze integrations (emergency)* is the same switch; use this tab so a reason is recorded.
- **Read-only** (49.12.3): the whole business can look but not change. People can still sign in, sign out, change their own password and two-factor settings, open every screen and report, run previews and dashboards, and export (the export freeze governs exports). Every other change - a bill, a save, an upload, an API client's write, an inbound webhook - is refused (*This business is read-only for now…*, `423 Locked` with `X-Refused: READ_ONLY`) and logged as *REFUSED_READ_ONLY*. Everyone sees a *Read-only · changes paused* strip at the foot of the window. A till keeps the bills it holds offline and sends them once read-only ends. Jobs the server runs on its own (scheduled reports, sweeps) keep running; add **Freeze integrations** to stop outside calls, or suspend the business to stop everything. The emergency controls on this tab stay open. **End read-only** (reason and your password again) opens changes at once. Settings → Security → *Read-only mode (emergency)* is the same switch; use this tab so a reason is recorded.

**Type the name to confirm (49.14.2).** The riskiest buttons first ask you to type a name before **OK** turns on. The name is the one you know the thing by, never an internal id; capitals do not matter.
- **Sign out everyone**, **Freeze exports** (turning it on), **Freeze integrations** (turning it on) and **Read-only** (turning it on): type your business name, as set in Settings → Configuration → Platform → *Company name*. With no company name, type your client id. The same name sits in the header beside the logo on screens wide enough to show it.
- **Pause client** in the Control Panel (platform operators): type the client's name.
- **Remove personal data** on a privacy request: type the customer's id.
- **Merge customer**: type the id of the duplicate being merged away.
- **Delete purchase order**: type its PO number.

This guard is in the browser only. The reason and password checks above, and every server rule, still apply.

**Bank changes (49.14.2).** A vendor's bank account change does not hold payments. The detection *A vendor paid soon after its bank account changed* (table above) raises an Urgent alert when a bill is paid within 3 days of a change, and the change is in the audit log with its reason. Check that alert before approving the payment.

**Privileged sessions (Stage 49.11.7).** A table above *Trace a request* lists each recent sign-in of a Super Admin or Admin: who, when, last seen, the address and device, and its state - *Active*, *Ended* (by an administrator), *Signed out* (their password changed or you used Sign out everywhere) or *Expired*. **End** on an active row asks for a reason and your own password; that one session stops at its next click and the person's other sessions carry on. Rows are kept 30 days. Sessions of other roles are not listed.

**Event export (Stage 49.11.6).** At the bottom of Settings → Monitoring → Security Events, *Event export* sends this business's security events to a log or SIEM address you choose. It is off until you turn it on. Enter the https address and a signing secret (16 characters or more), tick *On*, press *Save* and give your password. Press *Test send* to check the address; it sends one empty test event. Each batch (up to 500 events) is signed with the secret in the `X-Webhook-Signature` header (HMAC-SHA256 of the body) and the timestamped `X-Webhook-Signature-256`, and carries the user, action, result, redacted detail and reference - never the address or device. Only events from the moment you turn it on are sent. A batch counts as sent only when the address answers 2xx; if it is down the screen shows *Failing*, the error and the number waiting, and it retries on its own, later each time, up to an hour. Nothing is stored aside from the events already in the log. Events go strictly in order: anything under about 30 seconds old waits, and a batch stops at an event another save is still writing, so none is skipped. If old events were archived before they were sent, the screen says which ones. It sends nothing while *Outside calls* is off, integrations are frozen, or the business is a sandbox. *Send history* (Stage 49.11.6): with the export on, pick a *History from* date (at most 400 days back and 500,000 events), press *Send history* and give your password. The export goes back to the first event on that date and sends everything from there to where it was, in order, in the same batches; the screen shows *History from ... events left* until it catches up. Only one history send runs at a time, and it never slows what people are doing. Batches sent again carry `"backfill": true`; each event's `seq` is its key, so the receiver can drop repeats. Events that left the live log before they were sent are reported once as a gap, whether archived or not.

**Notification decisions (Stage 49.12.5).** Below the export, *Notification decisions* lists, for each incident, who may need telling and the time left. *Matrix* is the list of rules (CERT-In and DPDP are there to start, both marked *Needs counsel review*; the DPDP row has no time limit until counsel sets one). Every save makes a new version and write what changed; decisions already opened keep the version they started under. *New incident* takes a name and when you noticed it, and starts the assessment and target clocks for the rows you tick. *Decide* records Notify, Do not notify or Pending, with a reason, an evidence reference and a named approver (an Admin, a Super Admin or a Privacy Approver, other than you; the list offers only active people who qualify). The named approver then presses *Approve* (the decision closes and no longer changes) or *Send back* with a note. If the decision changed after the approver opened it, the screen says to reload and read it again. Each clock is checked against the server time (49.12.5): a deadline shows **Due** in the last quarter of its window and **Overdue** once passed; a closed decision shows neither. *Incident runbook* above the list opens the incident response article (containment, evidence pack, timeline). The system never decides that anyone must be told; a person does.

**Every export is now recorded.** Each delivered export or report download is an audit row *DATA_EXPORT* (who and which export), which is what *Many exports by one user* counts.

**Threshold splitting report (49.10.6b).** **Reports → Finance → Threshold splitting** lists the days one user made several records just under an approval limit - the sign of one purchase, discount or refund cut into pieces to stay below the approver it would need. The limits are your own **Settings → Approval Rules**: every rule whose *from* amount is above zero is a limit for its record type (as shipped: a purchase order or requisition at 50,000, an expense claim at 5,000, a till discount at 10%, a loyalty redemption at 500). Add a rule for returns or refunds and they are checked too. A row shows the day, the user, the record type, the store, the limit, how many records and their total (*Together over limit* says whether they add up past it) and their numbers. **Within % under the limit** (default 10) and **At least this many a day** (default 2) tune it; the dates default to the last 30 days. It is a finance report: Admins and finance roles can run it.

**Reports refused as busy (GLOBAL-0310).** A report a person walks away from keeps running on the server until its query ends. So the server counts every live report run per business (half its request slots, 7 by default) and in total (20). Past that, a new report answers *Too many reports are still running…*; try again in a minute, or use Export for a large one.

**Report size.** On screen a report pages 500 rows at a time. A run or export goes to 20,000 rows, because a run holds its rows in memory on the server. Past that a report answers *This report is too large to show* (REPORT-0161) and an export fails (REPORT-0285). Scheduled reports, dashboards and drill-downs stop sooner, at **Maximum rows for a scheduled report or dashboard** (`platform.max_sync_report_rows`, 5,000 by default). A value above 20,000 lifts pages and exports only for reports with no read limit of their own; most stop at 20,000.

**Restore check.** `deploy/restore_drill.sh` now also runs `erp-server -verify-restore` on its throwaway copy. For each client it checks that the ledger balances, the audit evidence verifies with this server's key, and the reconciliations run. A *FAIL* line stops the drill; a *note* line is a difference live's Reconciliation screen would also show. The command runs only against the database in `DATABASE_URL`, so point it at a restored copy, never at live.

**Release evidence from the drill.** Every `restore_drill.sh` run, pass or fail, writes `$BACKUP_DIR/restore-drill-evidence.json`. The Deploy click (step D5) copies it into the release gate as the backup-restore record for the build it deployed, and the next Release ships it. Deploy also runs step D0e first: it stops, before anything changes on the server, when the release-candidate record for the commit is missing or failing. Fix the named check, or run Release.cmd again.

---

**PO approval rules and delegates (148.39).** (148.47: every role a rule names must exist in this client - a typo is refused with `PURCHA-5100`, the role field offers the real roles, and a saved rule whose role was since removed is marked in the list; an inactive rule is not checked.) Under Setup, **PO routing rule** holds one row per rule: an amount band (from, up to), optionally a vendor group and an item category, and the approver role; the most specific rule that matches wins, and a PO no rule matches goes by **Settings → Approval Rules** exactly as before. A rule may name **Escalate after days** and an **Escalate to role**; the existing PO reminder job stamps the escalation and sends the "PO Approval Escalation" notice, and that role may then decide the PO. **Approval delegate** names who covers an approver (by user) between two dates; a delegate never decides a PO they created or sent for approval, and **Who can approve their own documents** is unchanged. Every way a PO is submitted or decided uses these rules.

**Delegates and waiting approvals for every document (73.4.9).** An **Approval delegate** row now covers every approval, not only POs: inside its dates the delegate sees in **My work**, and may decide, every document routed to the approver's role, but never one they created or sent for approval. A document that always needs a second admin (a Super Admin password reset, a treatment correction) or dual control is never covered. To chase documents that wait, set these in **Settings → Security** (all off by default):

- **Remind approvers of a waiting document after** (`approvals.reminder_hours`) and **Escalate a waiting document after** (`approvals.escalation_hours`), hours, 0 = off. A document still *Pending Approval* after that long sends **Approval Reminder** / **Approval Escalation** once per wait; a document edited and resubmitted waits afresh. Checked hourly. Template variables: `doc_id`, `doctype`, `code`, `amount`, `hours_waiting`, and `escalated_to` on the escalation. As with every notice, nothing goes out until a **Notification Template** exists for the event; until then it is logged as skipped.
- **Escalate waiting documents to role** (`approvals.escalate_to_role`, blank = the escalation is a notice only). Once a document has waited past the escalation hours, this role may decide it too, and sees it in **My work**. The role must exist here; a name that is not a role is refused with `APPROV-0157`.

Purchase orders keep their own settings and rules above (**Remind approvers of a waiting PO after**, routing-rule escalation) and are left out of these.

**Goods received before the PO (148.38).** **Allow goods to be received before the PO** (Settings, Procurement & GRN, off as shipped) lets a goods receipt be saved without a PO when it carries a reason code of the category **Received Before PO** (Reason codes; three starter codes are added). **Retro PO due within** (7 days as shipped) sets the date the PO is due. The retro PO is an ordinary PO with **Received before PO: receipt** set; it goes to approval, and on approval the receipt takes its number. No vendor bill for such a receipt is paid - by Pay, part payment, TDS payment, an approved override or the receipt's own payment step - until then (PURCHA-5032). **Reports, Goods received before PO** shows waiting, overdue and linked receipts. With the setting off a receipt must name a PO as always.

**Inspection by rule (148.33) and by stock age (146.3.11).** Two settings, both off as shipped (Settings, Quality). **Inspect lots by rule** turns on the **Quality required** and **QC mode** fields on Items and Item types (the item's own value wins), the **Inspection rules** screen and **Inspection lots**. A receipt line is held in QC hold, in the same posting as the receipt, when its item is marked **Quality required = Yes** (with QC mode **Warehouse may decide** the lot is still held, and can be waived with a reason), or when a rule applies: **New vendor** (no earlier receipt from that vendor), **Vendor reject rate** (failed share of the vendor's last 20 lots above the threshold %), or **Skip lot** (after N clean lots in a row the vendor is sampled at the rule's smaller %). **Sample % of the lot** (10 as shipped) sizes the sample. A rule may name an **Inspection plan** (tests with min and max, one **acceptance %**; the plan's *Applies to* is *Incoming*). Results are entered in the Store app, Inspect; the lines total the lot, a blank result is Fail, and karat or weight go to the batch. A vendor bill is refused while a lot of that receipt is waiting or still holds failed units; the Inspect screen's **Scrap** (stock adjustment, needs a reason code) or **Release** (needs a note) clears them. Nothing is posted to the ledger by the hold or release; they move units between *available* and *QC hold* only. The hold takes whole units per line, the same as stock does (Stage 148.56). Turn the setting off and receipts behave as before; lots already held stay held until their results are entered. **Re-inspect stock by age** adds the *Stock age* rules (a category, or an item that overrides it, and the days between checks), a daily job that files one **Stock age inspection** per store per age band 7 days ahead (**Inspections filed ahead by**), and the report **Inspections due by stock age**. The outcomes are Pass, Re-polish (job-work order), Markdown (a flag) and Scrap (a stock adjustment, so it follows the usual approval and posting). The age is the receipt-date barcode's where the item has one, else the last receipt into the store (the report column *Age source* says which); the decision screen is the *Stock age inspections* section of Manufacturing, Quality Inspections. HR/Admin and Store Manager may decide.

## Part C — Operator / Platform-Team Level

### C.1 Environment Layout

This system can run up to three independent copies side by side, sharing the same PostgreSQL server but each with its own database and port:

| Environment | Purpose | Default port |
|---|---|---|
| `dev` | The main working copy — this repository folder itself. | 8080 |
| `test` | A staging copy for verifying a change before it goes live. | per `environments.json` |
| `live` | The real, production environment. | per `environments.json` |

`test` and `live` live in their own separate folders (git "worktrees") created by `promote.ps1` (§D.1) — they are not manually maintained copies, they're produced by the promotion process itself.

**The business day (Stage 132.19).** The production server and its database run on UTC; the business runs on India time. Every report, day book, trial balance as-of, GST period and "today" counts days in the **business time zone** — Asia/Kolkata unless set otherwise — so a sale at 00:30 on the 1st is on the 1st, not the last day of the month before. Stored times are unchanged (screens already show them in local time). Two settings name another zone, and must agree: the database setting `erp.business_tz` (`ALTER DATABASE custom_erp SET erp.business_tz = 'Asia/Dubai'`) for days counted in SQL (`public.erp_day_start`, `erp_today`, `erp_local_date`), and the server environment `ERP_BUSINESS_TZ` for dates the server stamps. A new report or query that filters by date uses `created_at >= public.erp_day_start($1::date) AND created_at < public.erp_day_start($2::date + 1)` and `public.erp_today()` — never `$1::date` against a timestamp or `CURRENT_DATE` — and Go code uses `engines.BusinessToday()`, never `time.Now().Format("2006-01-02")`.

### C.2 Deployment Pipeline

See §D for the full deployment procedure. In short: a change is tested in `dev`, promoted to `test`, verified, then promoted to `live` — never edited directly in `live`.

**The release binary is signed (49.10.6b.3).** `Deploy.cmd` (through `deploy\deploy.ps1`) signs the server binary with the release key on the release machine — `%USERPROFILE%\.erp-release\signing.key`, or the file `ERP_RELEASE_SIGNING_KEY` names — and the server checks the signature with its own openssl before the new binary runs at all. A binary that was not signed, was changed after signing, or was signed with another key is refused before the swap (`DEPLOY-REFUSED` in the deploy output; production stays on the live build, and the refused file is kept as `erp-server.failed-unsigned-<time>` for a week). What to know:

- **The first deploy after this change creates the key** and writes `release\signing.pub` into the repo; the server pins that public key at `/etc/erp/release-signing.pub` on the same deploy. Commit `release\signing.pub` with the next release (Release.cmd ships it like any new file).
- **Keep a safe copy of `signing.key` off the release machine** (a password manager or an encrypted USB key). It is never committed, printed or sent anywhere. Releasing from a new machine means copying that file there; `Deploy.cmd` stops at D0c if the key is missing while `release\signing.pub` is committed — it does not make a new one, because the server would refuse it.
- **The server needs OpenSSL 3** (Ubuntu 22.04 or later). `Deploy.cmd -Check` (gate D0c, nothing changed) shows the server's openssl version and whether a key is pinned.
- **Replacing the key on purpose:** make the new key, put its public key in `release\signing.pub` and, by hand on the server, at `/etc/erp/release-signing.pub` before the next deploy — a deploy signed with a key the server does not trust is refused with *the release key changed*.

**Where a release is tested (177).** `Release.cmd` no longer runs the test suite on this computer. It runs the quick checks, commits the release and sends it to GitHub on a branch named `candidate/<version>`. GitHub's machines then build it, set up an empty database and run every test of that exact commit, split across 16 machines (about ten minutes). Only when every test passes does GitHub move `main` to the release. The run's link is in the release report; open it to watch, or start `Release.cmd -Wait` to wait in the window. `Deploy.cmd` waits for the same run before it changes the server, and stops when a test failed, so a failed release never reaches a client. Do not push `main` by hand while a release is being tested. To run everything on this computer instead, as before, start `pwsh release\scripts\Invoke-Release.ps1 -LocalTests`. It takes about 50 minutes. The code map in `docs/brain` lists every file in the release commit, but its counts come from the last release's code graph. The graph takes minutes to rebuild, so after the push a background job rebuilds it at low priority and redraws the map. `Deploy.cmd` then copies the redrawn map into the working tree (step D6), to be committed with the working records.

**The browser check (158.4) runs with `-LocalTests` only.** Gate G10a builds the release, starts it on a copy of the dev data (`custom_erp_smoke`) and signs in as Super Admin and as Cashier. It opens the POS screen, makes one sale end to end (PO, GRN, sale, same-day return) and checks that the sale appears in the Sales Register. This takes about two and a half minutes. The release stops on an uncaught browser error, a failed request, a step that could not be done, a server panic or a setup failure. A figure that differs between two screens is only a warning. The release report links the run's report. The gate is skipped, with a WARNING note, on a machine without the Guide inspector, Node, Playwright or Edge.

**Deploy warns when alerts go nowhere (158.2).** `Deploy.cmd` (and `Deploy.cmd -Check`) prints an *alerts* line. It shows **WARNING** when `OPS_ALERT_WEBHOOK_URL` is not set in `/etc/erp/erp.env`, because urgent alerts are then only written to the server log. It does not stop the deploy. Set the variable as in §C.4.

#### C.2.1 The version number users see, and how to bump it

Every signed-in user sees a small grey number in the bottom-right corner of their profile chip at the bottom of the sidebar (beside their role, under the arrow) — the release number (for example **`0.4.0`**) and nothing else. Hovering, focusing or tapping it opens a small popup carrying the version and the release date in `D/M/YY` form (for example **`0.4.0`** / **`19/10/26`**), so the date is one hover or one tap away without competing with the username above it. The same values are available unauthenticated at **`GET /api/v1/version`**:

```json
{"version":"0.4.0","release_date":"2026-10-19","build_time":"2026-10-19T05:02:09Z","git_commit":"e9b183a"}
```

The UI deliberately surfaces only `version` and `release_date`. `git_commit` and `build_time` stay on the endpoint for you and for health checks — they are what tells you *which* build is actually on a box, which is the question that matters after a deploy that may or may not have restarted cleanly.

**Both halves are automatic on a production release (Stage 90):**

| Shown as | Comes from | Who updates it |
|---|---|---|
| `0.2.0` | the release number `deploy\deploy.ps1` works out and stamps into the binary (`-ldflags`) | Automatic, by the rule below |
| `18/10/26` | the `buildTime` value stamped in at build with `-ldflags` | Automatic — every build stamps it |

**The numbering rule** (semantic versioning, from the commit types this repo uses). `deploy.ps1` asks the live server for its version and commit (`/api/v1/version`), and `cmd/releasever` reads the commits between that commit and the one being deployed:

| The release contains | Number | Example |
|---|---|---|
| any `feat:` commit | MINOR up, PATCH to 0 | 0.1.0 → 0.2.0 |
| only `fix:` / `docs:` / `chore:` / `refactor:` commits | PATCH up | 0.2.0 → 0.2.1 |
| nothing new (a redeploy of the live commit) | unchanged | 0.2.1 → 0.2.1 |
| go-live, or a deliberate breaking release | MAJOR, **only by hand**: `.\deploy\deploy.ps1 -Target ... -Version 1.0.0` | 0.9.4 → 1.0.0 |

Once the new build passes its health check, `remote_deploy.sh` runs `erp-server -record-release`. It writes the release to the ledger (`public.deployments`: environment, version, commit, who, when, `passed`) and sets every client's version to it (`public.tenants.app_version`, except a client pinned to another version). The **Control Panel** (§C.8) shows both. Confirm with `curl -s https://<host>/api/v1/version` before telling anyone it shipped.

`deploy.ps1` stops if it cannot read the live version (the server is down, or it reports `dev`) or if the live commit is not in your history. Pass `-Version` to set the number yourself. To record an earlier build in the ledger without touching the clients, run `erp-server -record-release -version 0.1.0 -commit 840909b -at 2026-10-18T04:45:51Z -notes "..."` on the server, with `/etc/erp/erp.env` loaded.

`internal/server/VERSION` (`go:embed`) is now only the number an **unstamped** build reports: a developer's `go build`, `manage.ps1`, `promote.ps1`, Docker. `Release.cmd` writes it (release gate G4b) with the number it is releasing, and `Deploy.cmd` deploys exactly that number, so production, an unstamped build and the manuals' `release:` line always agree.

**Things worth knowing:**

- **Never set the version by hand anywhere else.** It is stamped into the binary at build time precisely so it cannot drift from the binary that reports it. A number typed into a config file or a database row would eventually lie.
- **`dev build` instead of a date in the popup** means the binary was built with a plain `go build`, with no ldflags. That is correct and expected on a developer's machine; on a server it means someone built it outside the normal scripts, and you should not trust that deploy — rebuild it with `deploy/deploy.ps1`, `manage.ps1 release`, `promote.ps1` or CI, all of which stamp it.
- **The release date is derived from the build timestamp, not stored separately**, so the two can never disagree.
- **A user reporting a bug should be asked for this line first.** It is the cheapest way to find out whether they are on the build you think they are.
- Roughly 47 documents under `docs/` carry `applies_to: source release 0.1.0` in their frontmatter. That records the release they were last checked against, not the current one, so they are not changed at each release; `cmd/doclint` only checks that the key is present.

**Open tabs pick up a release by themselves (297.1).** Nobody needs a hard refresh after a deploy.
- **How a tab knows.** Every API reply carries `X-ERP-Build`, the `VIEW_MODULE_VERSION` of the `public/app.js` on disk. The server looks at the file again within 10 seconds of a change, so new files are seen without a restart.
- **When it reloads.** A tab on an older build reloads at a moment that loses nothing: the next sidebar entry or Back/Forward, after being hidden a minute, or after ten minutes untouched at the top of a screen. A tab back from five minutes away asks `/api/v1/version` once.
- **When it waits.** While a save is on its way, a dialog is open, the till has lines, a field the user typed in is unsaved, or a purchase order, RFQ or return being built has lines. A screen that keeps work across screen changes registers itself: `globalThis.holdUpdateWhile?.('name', () => true while unsaved)`.
- **Why a plain reload is enough.** Every file carries the release's `?v=`. Gate G4 bumps `app.js`, `db.js` and the view modules together, and the store app's one stamp when a store file changed. Modules import each other with their own `?v=`. `TestEveryAppScriptCarriesTheReleaseVersion` fails on an unstamped script. A script or stylesheet loaded without `?v=` is sent `no-cache` (a 304 when unchanged).
- **Nothing cached is lost.** Offline sales, the session, saved filters and drafts live in browser storage and survive the reload. **Reset** in the user menu is still there for a browser in a bad state.

**Slow and dropped lines (297.6).** The app is measured on a 2G phone (256 kbps down, 128 up, 400 ms each way, 4x slower CPU), the `phone-2g` profile of the browser budgets.
- **Retry rule.** A call the network cut (no answer at all) is sent again after 1, 2, 4 and 8 seconds (while the browser reports itself offline, as soon as the line is back, up to 30 seconds a try), and a "Connection lost · retrying" strip shows meanwhile. Only GETs and writes sent with an `Idempotency-Key` (`apiFetch(..., { commandKey })`) are resent; the server answers a repeated key once. Every other write is tried once and shows the connection error, so nothing posts twice. Leaving the screen stops the retry.
- **Fewer round trips.** A view module starts all its helper modules at once instead of one after another; record names in a list are looked up four at a time; Home's Today tiles draw their last values at once and refresh behind (kept only while the tab has saved nothing and the sign-in is the same).
- **Budgets.** `docs/qa/perf-budgets.json` sets the 2G targets: first visit 5 s, revisit 2.5 s, 32 KB on a revisit, a screen change within 1 s. See [the browser budgets](../operations/browser-performance-budgets.md).
- **Tab size (297.7).** After a session through the main screens the tab must stay under 12 MB of script memory and 10,000 page nodes (measured: about 6 MB and 6,400). Caches a feature needs are kept: the POS offline queue, the session, saved filters, drafts, lookups and loaded screens. The item-code lookup keeps the last 1000 codes used.

#### C.2.2 Data Healer: catching old records up after a release (ask 207)

A release never changes a client's data. When existing records must catch up to a change (207.4: items and item names saved before 0.25.0 have no stored Item Alias), the release ships a **heal**, and each client runs it once from **Settings → Data Healer** (shortcut `DH`). How users see it is in the user guide, §8i.

- **Who.** Read on *DataHeal* sees a heal's records; update runs it. Shipped: Super Admin and HR/Admin run, Store Manager reads. Everyone signed in sees the strip.
- **Waiting and Done.** A heal waits while records need it. A client with nothing to fix has it recorded Done by *system* the first time anyone looks, so clients set up after the release never see it. Done is final and records who ran it, when, how many records were fixed and how many values were typed; a record made blank later does not bring the heal back.
- **Pushing it.** While one waits, a strip that cannot be closed sits above every screen, the covered lists show **Run heal**, and the covered forms will not save until it runs. That hold is on the screen only: the server still accepts saves, so imports and integrations keep working. An item imported with a blank alias prints the alias made from its name and stores it on its next save.
- **Runs once.** Run heal takes a lock per client and heal, checks Done again, writes only records still blank, and stores the Done record and an Activity Log row (`DATA_HEAL_RUN`) together. If two admins click at once, one runs and the other sees *already healed*. A typed value over the field's limit refuses the whole run (`PIM-9363`) with nothing written.
- **Shipped heals.** *Item aliases* (`item-alias`, 207.4) stores the alias on Items and Item Names. *Printer names* (`printer-name`, 263.4) moves each Printer's operating-system printer name from the field used before ask 263 (`qz_printer_name`) to **OS Printer Name** (`os_printer_name`) and drops the old field; a typed name over 255 characters refuses the run (`GLOBAL-0002`). Printing never waits on it, because until it runs the server reads either field; only the Printer form waits.
- **API.** `GET /api/v1/heals`, `GET /api/v1/heals/{id}/records` (the first 500) and `POST /api/v1/heals/{id}/run` with `{"typed": {"Item/<id>": "Short name"}}`. Errors: `MASTER-1703` (no such heal), `MASTER-1704` (a Data Heal record created or changed by hand). Extension tokens are refused.
- **Adding a heal (developers).** Add one `healDef` to `dataHeals()` in `engines/data_healer.go`, with its count, records, check and apply functions in its own `engines/data_healer_<what>.go` and a test. Its `Doctypes` are the record types whose lists show the panel and whose forms wait.

#### C.2.3 Past names: changing an item's name on documents already made (item 201.4)

Renaming an item in the Item master changes every screen and every new print, because most lines keep only the SKU and read today's name. Two places keep the old name: lines that copy it (Delivery Challan, Material Issue and Return, stock adjustment: *item name*; RFQ and Vendor Quote: *description*; Consumption: *name*) and the frozen first print of each document (item 130.16). **Settings → Data Healer → Past names** changes both, for a Super Admin, without SQL.

1. Rename the item in the Item master first.
2. Open **Past names**. Type the **Old name** and the **New name**. **From** and **To** are optional and match the day each document was made.
3. **Preview** lists each record type with its documents, lines and prints, then the first 200 documents. Untick a type to leave it out.
4. **Apply**, type **YES**. The toast says how many documents changed. The preview runs again and should show *Nothing to change*.

- **What matches.** The whole name, ignoring case and spaces at the ends, on a line that has an item (a SKU or item code). A print description *Old name · Variant* keeps its variant. A note or remark that mentions the name is left alone, and so are masters.
- **What is never changed.** A document filed with GST (an IRN or an e-way bill number) is listed as *filed, not changed*. Deleted documents are skipped.
- **Record.** Each changed document gets a new version with the edit reason *Name changed on past documents: old -> new* and an Activity Log row `PAST_NAME_CHANGE` listing every path it changed (for example `items[0].item_name`, `po#1:lines[0].item_name`). One `PAST_NAME_CHANGE_RUN` row sums the run. A frozen print's checksum is made again over what it now says. To undo, run it again with the names swapped.
- **Limits.** One run changes at most 20,000 documents (`MASTER-1708`: narrow the dates). Two runs at once wait for each other, so each document changes once.
- **API.** `POST /api/v1/heals/past-names/preview` and `POST /api/v1/heals/past-names/apply` with `{"old", "new", "from", "to", "doctypes": [...], "confirm": "YES"}`. Errors: `MASTER-1705` (names missing, the same or too long), `MASTER-1706` (dates), `MASTER-1707` (no YES), `MASTER-1708`. Super Admin only; extension tokens are refused.

### C.3 Backup and Restore

Full procedure and the latest verified restore-drill record: **[`../operations/backup-restore.md`](../operations/backup-restore.md)**. Summary:

- `.\manage.ps1 backup` creates a timestamped, SHA-256-verified backup of every environment's database that currently exists. Do this on a schedule (a Windows Task Scheduler recipe is in the linked doc) — not just before risky changes.
- Restoring is deliberately hard to do by accident: `.\manage.ps1 restore -Env <env> -File <backup>` requires the target server to be stopped and an exact typed confirmation (`RESTORE <environment>`).
- **Perform an actual restore drill monthly**, not just backups — a backup you've never restored from is unverified. Record the date, file, duration, and result (the linked doc shows the format).
- **On a self-hosted Linux server the above does not apply** — that box has its own scripts. Install the nightly job once with `sudo bash /opt/erp/deploy/install_backup_cron.sh` (14-day on-box retention; it also runs one backup straight away to prove it works), and prove restores with `deploy/restore_drill.sh`. **Do not assume this has been done**: a server with no scheduled backup looks exactly like one that has them, right up until you need one. *(This is not hypothetical — a 2026-08-07 inspection of the production droplet found exactly that: an empty crontab, no timer, and two hand-taken dumps that looked at a glance like a working backup history.)*
  - **`deploy.ps1` ships the binary and `public/`, not `deploy/`.** So a script existing in the repo does **not** mean it exists on the server. Check with `ls /opt/erp/deploy/` before assuming you can run one, and `scp` it up if it's missing.
  - **`restore_drill.sh` needs a connection that may create databases, which the application's own role deliberately is not.** A correctly hardened box gives the app role no `CREATEDB`, so the script's default "derive the admin connection from `DATABASE_URL`" fails with `permission denied to create database`. Pass **`DRILL_ADMIN_URL`** instead — typically the local superuser over the unix socket, e.g. `DRILL_ADMIN_URL='postgresql://postgres@/postgres?host=/var/run/postgresql'`. **Do not grant the app role `CREATEDB` to make the drill pass** — that weakens the running system in order to test a backup.
  - **The superuser usually cannot read `/opt/erp/backups`** (`/opt/erp` is `drwxr-x--- erp:erp`), so stage a copy of the newest backup somewhere it can, point `BACKUP_DIR` at it, and **rewrite the `.sha256` sidecar's absolute path to a bare filename** — `sha256sum -c` resolves the path recorded inside the file, so a staged copy otherwise fails its own checksum and reports a perfectly good backup as corrupted. The full working invocation is in **[`../operations/restore-drill-log.md`](../operations/restore-drill-log.md)**.
- **A copy off the server (Stage 75.1).** The nightly backup on the Linux server also uploads each dump — and new product images and other uploads — to a private cloud bucket once the bucket's details (`OFFSITE_S3_*`) are in `/etc/erp/erp.env`. Until then every backup sits on the server it protects. Set-up, what is uploaded and the two alerts (`BACKUP_OFFSITE_FAILED`, `BACKUP_OFFSITE_STALE`): [`../operations/backup-restore.md`](../operations/backup-restore.md) → *Off-box copy*. Keep a copy of the backup key in a password manager — the bucket's files cannot be restored without it.
  - **Uploads leave encrypted too** (Stage 49.10.6a). Product images, expense receipts and vendor invoice copies are sealed with the same backup key before they go up (`<file>.enc` in the bucket). Without the key nothing goes up and you get `BACKUP_OFFSITE_FAILED`. To get them back, download the bucket's `media` folder to the server and run `deploy/restore_media.sh <that folder> /opt/erp/media_store` (the linked doc → *Restore the media files*). A file already there is kept; a wrong key restores nothing.
  - **Keep `media/` out of the bucket's 30-day rule** (Stage 49.10.6c.1). A product image or receipt goes up once and is not sent again while it is unchanged, so a rule that deletes everything after 30 days leaves the bucket with only the last month of uploads. Scope the rule to `<prefix>/database/` and `<prefix>/tenant/` in the provider's console (the linked doc has the exact steps), then check it on the server with `deploy/backup.sh --check-media-retention`. The nightly checks it too and alerts `BACKUP_MEDIA_EXPIRES` while a rule would delete media.
  - **A full disk stops cleanly** (Stage 49.13.7). A backup that runs out of room fails with `BACKUP_FAILED` saying *the disk under /opt/erp/backups is full*, and removes the half-written files — never leaving the plain dump behind. An upload into the app on a full disk is refused with *the server's disk is full - the file was not saved*, and nothing half-saved is kept. Free space (`Cleanup.cmd`) and try again. The same holds for the audit archive and the brand logo (Stage 49.10.5): on a full disk the audit rows are kept, the half-written archive file is removed, and the old logo stays.
- **Go back to a minute, not only to last night (item 290.2).** The nightly backup returns the database as it was at 02:00, so a bad import at 14:05 would lose the whole day's work. Point-in-time recovery returns it as it was at any minute of the last one to two weeks.
  - **Turn it on once** on the Linux server: `sudo bash /opt/erp/deploy/install_pitr.sh`. It restarts the database once for a few seconds, so run it in a quiet hour. It takes the first base backup itself and ends with `PITR-ON`. After that, every deploy keeps it current.
  - **Bring back a minute:** `cd / && sudo -u postgres /usr/local/lib/erp-pitr/pitr.sh restore --to "2026-11-27 14:04 +05:30" --into <new folder>`. Give the time with its zone. This starts a separate copy on port 5499 and never touches the live database. Take the client's schema from that copy with `pg_dump` into a scratch database, then stop the copy.
  - **Check it:** `pitr.sh status` shows the window you can go back to and any failure.
  - **Alerts:** `BACKUP_WAL_ARCHIVE_FAILED` (fix it the same day, or the disk fills), `BACKUP_PITR_BASE_FAILED`, `BACKUP_PITR_STALE` and `BACKUP_PITR_OFFSITE_FAILED`.
  - Full steps: [`../operations/backup-restore.md`](../operations/backup-restore.md) → *Point-in-time recovery*.
- **To export a single tenant** rather than the whole database — offboarding, handing a customer their data, or a scoped copy before a risky fix — use `.\manage.ps1 export-tenant -TenantSchema tenant_<name>` (or `deploy/backup.sh --tenant tenant_<name>` on Linux). Same encryption and checksum as a full backup; restore it into a scratch database, never over a live one.

### C.4 Incident Response and Alerting

Full procedure: **[`../operations/incident-runbook.md`](../operations/incident-runbook.md)** — severity levels (P0-P3), escalation contacts, rollback steps, and every log location in one place. Automated alerting is built and only needs `OPS_ALERT_WEBHOOK_URL` set to your real destination — see that doc §3 for exact setup. Four things alert: the system panicking, a nightly backup **failing**, a nightly backup **not existing at all**, and a spike in errors. Errors in users' browsers count toward the spike too (158.3, see below).

**Is alerting actually on?** **Settings → System Status → Incident alerts** says which: **Delivered to …** (alerts reach your webhook and responder), **Delivered - no responder**, **Configured, not sent** (this environment has outbound delivery switched off), or **Never configured** — with the server settings still missing (`OPS_ALERT_WEBHOOK_URL`, `OPS_ALERT_RESPONDER`) and the runbook. On the same screen a backup or restore drill that has never run shows **Never recorded** as a warning, not as healthy.

The third one is worth understanding, because it is the failure the other three cannot catch. A backup job that fails tells you it failed; a backup job that was never installed, or that stopped being scheduled, says nothing — and silence looks exactly like success. The server therefore checks every hour when the last successful whole-database backup was **recorded in the database** (`deploy/backup.sh` writes one row per run to `public.backup_runs`), and alerts if there is none or it is more than 36 hours old, independently of whether any job ran. Single-tenant exports never count as the nightly backup. On a production server (`ENV=production`) "no backup recorded at all" is also printed at every start as finding **SB-026** — it is never silent there. To see the record yourself: `psql "$DATABASE_URL" -c "SELECT finished_at, scope, outcome, size_bytes FROM public.backup_runs ORDER BY finished_at DESC LIMIT 5"`. **Until you set the webhook URL, all four are written to the server log only and nobody is notified.**

### C.4a Guide's key: automated sign-ins (195.1)

Guide is the owner's own test robot. It drives a real browser, so the site would treat it like any user: 60 calls a minute, which makes a long Guide run wait. Guide therefore carries a hidden key, and the site can refuse every *other* automated browser.

1. On the owner's PC: `pwsh Guide.ps1 -NewKey -Site app` (then `-Site minn`). The key is saved encrypted for that Windows user only and is never shown. The command prints one line: `ERP_AUTOMATION_KEY_SHA256=<64 characters>`. That is only the key's fingerprint; the key itself never leaves the PC.
2. On that site's server: add the line to its env file (`deploy/erp.env.example` lists it) and restart the service.
3. Done. Guide's calls now skip the per-minute limits (sign-in keeps its 5 a minute, so a leaked key cannot guess passwords) and Guide stops pausing.

| With the line set | What happens |
|---|---|
| Guide, with its key | Signs in and works without the per-minute limits. |
| Any other automated browser (Playwright, Selenium, Puppeteer, headless Chrome) | Refused at sign-in with the same message as a wrong password. The Activity Log gets a *LOGIN* row with status *AUTOMATED_SIGN_IN_REFUSED*, the address and the user name tried. |
| People, API clients, PrintBridge | Nothing changes. |

Without the line nothing changes anywhere, which is how local and test servers run. To replace the key, run `-NewKey` again and put the new line on the server; until then Guide notices the 429 and paces as before. A bot written to hide that it is automated can only be stopped at the edge (Cloudflare bot controls, 26.1.3b).

### C.5 Connector / Integration Verification

If you're enabling a real Shopify/BigCommerce/Magento connection for a tenant: **[`../operations/connector-live-verification.md`](../operations/connector-live-verification.md)** — the exact credentials format and a script that verifies the connection against the real platform before you trust it.

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
- Logging in with it still forces **TOTP enrollment first**, because the client's `admin` is an **Admin** (Stage 125 — a client never gets a Super Admin), an MFA-mandatory role. The password alone reaches nothing. This login is the client's own; you do not need it to set the client up — open the client from the **Control Panel** instead (§C.8).
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

Traffic stops at once and the **retention clock** starts (30 days by default; `-retention-days N` for an agreed different period, `TENANT_RETENTION_DAYS` for the policy default). Nothing is deleted by this step. Sign-ins, open sessions, API keys, OAuth tokens and background workers all stop with the client, whether it is suspended, past its trial or being offboarded; a **resume** brings back only what was working (a deactivated user or revoked key stays off).

Then, once the window has elapsed and **after** you have taken and verified a tenant export (§C.3), a second person approves the purge (see *A second person for a reset or purge* below) and you run it with the approved request:

```
go run ./cmd/tenantctl request-removal -tenant acme -action purge -reason "contract ended 2026-09-30" -actor <your login>
go run ./cmd/tenantctl purge -tenant acme -backup-ref "tenant_acme_20260930.dump.enc (verified)" -request <request id> -yes
```

The purge **refuses** — and records the refusal — if the tenant was never deprovisioned, if a legal hold is in force, if the retention window has not elapsed, if no backup reference is supplied, or if no approved request is given. When it does run, it drops the schema and the registry row in one transaction and then **proves the removal**: no registry row, no schema, no hostname mapping, no row in any shared table, no cached session. That proof is printed and recorded.

**Starting a client again from a clean slate** (Stage 91) — when a client wants everything they entered removed but keeps using the ERP with the same logins:

```
go run ./cmd/tenantctl reset-data -tenant acme                       # the plan: every table, rows, kept or removed
go run ./cmd/tenantctl request-removal -tenant acme -action reset -reason "clean slate, ticket ERP-1502" -actor <your login>
                                                                      # another admin approves it in Approvals
go run ./cmd/tenantctl suspend    -tenant acme -reason "clean slate" -yes
go run ./cmd/tenantctl reset-data -tenant acme -reason "clean slate, ticket ERP-1502" -request <request id> -yes
go run ./cmd/tenantctl resume     -tenant acme -reason "clean slate ready" -yes
```

- **Removed:** every record — transactions, stock, the ledger, masters (items, vendors, customers), company setup (stores, legal entities, cost centers), print and sticker templates, integrations (channel, Pine Labs, CleverTap, Unicommerce, API keys, extension hooks), settings (every one back to its default), renamed labels and the chosen industry. Numbering starts again from 1, and number formats go back to the shipped ones (the template client `default` keeps its own formats).
- **Kept, unchanged:** the logins — users with their **password hashes untouched**, their two-factor setup, roles and permissions; record definitions, the chart of accounts, approval rules, loyalty tiers and stock location types; the shipped reference records (status rules, item types, TDS sections, currencies, tenders, the HSN list, the core item attributes); and every piece of evidence — the audit trail, privacy requests and e-invoice (IRN) history. Attached files stay in storage; their links go.
- **It refuses** unless the client is **suspended** (nobody writes during it), a backup of it from the **last 24 hours** is recorded (`public.backup_runs`, the same rule as renumbering old codes), no legal hold is in force, and every table the client has is classified as kept or removed — a table added by a later release stops it until `tenantctl` is updated for it - and unless another admin approved the request (below).
- It runs in **one transaction** and writes a `data_reset` row to the evidence trail with the counts, your reason and the two people who asked and approved. The way back is the backup.
- **A client with gift cards can be reset (47.16.4g).** The gift card ledger refuses any change or deletion of an event, as the general ledger does; the reset is the one delete both accept (it names itself a sanctioned purge inside its own transaction). A reset used to stop on a client that had issued or redeemed a gift card.
- Users' home stores and departments pointed at records that are now gone: pick them again on the **Users** screen once the new stores exist.

**A second person for a reset or purge (49.10.6b).** Neither runs on one person's say-so. You ask with `tenantctl request-removal -tenant <id> -action reset|purge -reason "..." -actor <your login>`, where `-actor` is your own Admin or Super Admin login on the operator's client (the client named first in `ERP_PLATFORM_OPERATORS`, else `default`). That files a **Client Data Removal** request there and sends it to **Approvals**, showing *Reset acme: your reason*. **Another** admin of the operator's client signs in and approves it (it is decided on its own, never ticked with others); you cannot approve your own, whatever the self-approval setting says (APPROV-0840). Then run `reset-data` or `purge` with `-request <request id>`. The request is good for that client and that act only, once, within 7 days of the approval; otherwise the command refuses and says why. The request, the approval (or rejection) and the reset or purge each write a row to the client's evidence trail (`tenantctl events`, which outlives a purge) and to the operator client's Activity Log (`CLIENT_DATA_REMOVAL`).

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

#### C.6.2 Search on very large item, customer and vendor lists (Stage 47.10)

- **The index.** `-migrate` adds the standard Postgres extension `pg_trgm` and a search index, `idx_documents_master_search_trgm`, over the identifying fields of Items, Customers and Vendors, in every client. A host without the extension skips this with a NOTICE; search still works, only slower on a very large list.
- **Check it:** `SELECT schemaname FROM pg_indexes WHERE indexname = 'idx_documents_master_search_trgm';` lists one row per client.
- **What users see:** a record whose code, ID or barcode is exactly the search comes first. On a list of over 100,000 records the search box covers the identifying fields only - Item: ID, code, name, item name, barcode, design code, family; Customer: ID, code, name, phone, e-mail, PAN, GSTIN; Vendor: ID, code, name, GSTIN, contact phone and e-mail. Over 10,000 matches the count reads *about 250,000*.
- **API callers:** `q_fields` searches the fields it names at any size. An estimated total comes with `X-Total-Count-Estimated: true`.
- **Building it** on an existing client holds new saves of that client's records for a few seconds at today's sizes; run `-migrate` in the release window as usual.
- **Lookup indexes (item 248.2).** `migrations_stage248_2_lookup_indexes.sql` adds nine more indexes in every client: item code, item barcode, product family code, stock ledger item, paid till carts by store and time, sales invoice customer (two fields), vendor invoice GRN, and bin code. A scan, a stock card, an item upload and a GRN's bill check read through them instead of the whole record table. On a 51,000-record client an item code lookup went from 51 ms to 0.2 ms. The migration is additive and safe to run twice; it builds like the search index above. **Check it:** `SELECT schemaname FROM pg_indexes WHERE indexname = 'idx_documents_item_code';` lists one row per client.

#### C.6.3 Export a client, import it into a new one, reconcile (50.12, BLD-030)

Three more `tenantctl` commands. They are command-line only, for platform operators; there is no web page or API route for them.

- **Export:** `tenantctl export -tenant <id> -dir <new folder> -reason "<why>"`. The folder must not exist or must be empty. It gets one `.jsonl` file per table, `config`, `attachments-index.jsonl` (each file's reference and sha256), `evidence/` (the client's lifecycle events and package changes), and `manifest.json`, written last: format version, migration list, row count and sha256 of every file. A folder with no manifest is a failed export; use nothing from it.
- **Left out on purpose** (listed in the manifest with the reason): secrets and tokens (API credentials, channel keys, OAuth tokens), and transient queues (jobs, outbox, publish queue, request log, idempotency keys). **Queued work stops the export** until it has finished; add `-allow-pending-queue` only when you accept that unfinished jobs and outbox rows are not carried.
- **Files and photos are not copied.** The index lists them with their hashes; copy the media folder separately and check the hashes.
- **Import:** `tenantctl import -dir <folder> -tenant <new id> -schema <new schema> -reason "<why>" -yes`. Without `-yes` it checks the export and prints what it would do. It creates a **new** client and refuses an id or schema that exists. Record ids are kept, so links between records and the numbering carry over. Any failure removes the new client and leaves nothing behind.
- **Refused with a clear message:** a missing, edited or newer-format manifest; a missing, shortened or changed file; an export from a newer migration set than this server.
- **Older export, newer server:** the new client gets the newer additive columns and tables empty (the report lists them). Nothing is rewritten.
- **Reconcile:** `tenantctl reconcile -dir <folder> -tenant <id>` recounts and rehashes the client's tables on its own and compares them with the manifest. It says **OK** only when every table matches; any difference is listed and the command fails. Import runs it for you and suspends the new client if it fails.
- **Upgrade and queued work:** see *API and event deprecation* in [service operations](../operations/service-operations.md).

---

### C.7 Data Console and cross-client control — platform operators only (Stage 60.6)

**Admin → Data Console** lets a platform operator open any client and fix any record directly. It is for repairs that have no screen of their own; prefer the normal screen whenever one exists, because the console does not re-run the business rules a screen does.

**Who can use it — and every other cross-client screen.** Being a Super Admin is not enough: the support user each client holds for the Control Panel's **Open** (§C.8) is a Super Admin of that client too, and must never reach another one. A **platform operator** is a Super Admin whose client and username appear in the server setting `ERP_PLATFORM_OPERATORS`, a comma-separated list of `client:username` pairs:

```
ERP_PLATFORM_OPERATORS=default:admin
```

Set it in the server's environment (`/etc/erp/erp.env` on the droplet) and restart the service. It cannot be set from inside the app, so no client user can grant it to themselves. Use a login that belongs to you, not to a client (the `default` client's admin, or a dedicated user). Only operators can: use the Data Console; list clients; see **Tenant Usage**; change a client's modules or plan (**Tenant Entitlements**); set a client's hostname; create clients; pause or resume a client and set its free trial (**Control Panel**, §C.8); create, reset or delete sandbox clients. A client's own administrator — before Stage 125 its Super Admin, now only the support user you **Open** it with, never its Admin — may still **read** its own client's module list and version record, never another client's. Anyone else gets `PLATFORM-0317`.

**One table per transaction.** The **Table** list shows every record type of the chosen client as its own table, grouped by module — **PurchaseOrder**, **GRN**, **Vendor**, **Item**, **Location**, **SalesInvoice**, **TransferOrder** and every other one, each with its row count. Opening one shows one column per field (headed by the field's label, e.g. *PO Number*, *Vendor*, *Status*), plus the record's number (`id`), status and timestamps. Any value a record carries that its record type no longer defines is shown under **Other data** rather than hidden. Below the record types are the raw **Database tables** (see "Where things are").

**Fixing a record.** Choose the **Client** and the **Table**, type a number, code or name in **Find** (e.g. `PO/BDR/26-27/00001`), and click **Edit** on the row. Each field gets the right box: a dropdown for a choice field, a record picker for a field that points at another record (it shows names, saves the record's code), a large box for line items and other JSON. Tick **empty** to clear a field. **Save changes** asks you to confirm, then writes only the fields you changed. Numbers are saved as numbers and yes/no fields as true/false; a value that does not fit the field (letters in a price, broken JSON) is refused. The record's number cannot be changed.

**What it protects.**

- **Hidden values** show as `••••••` and cannot be changed: passwords, password-reset and MFA secrets, API and channel credentials, recovery codes, and **customer personal details** — phone, email, address, PIN code, PAN/Aadhaar, date of birth, bank details, and a Customer's name. Inside line items and other JSON the same keys are hidden; when you save, every hidden value keeps what is stored. Searching never looks inside hidden values.
- **Record numbers and table keys** cannot be changed, and a raw table with no key cannot be edited at all.
- **Audit tables** (`audit_*`, `system_error_logs`) are view only, so the trail cannot be rewritten — by anyone.
- **The ledger** (`gl_postings`) and the gift card tables (`stored_value_*`) are view only too: a posting is corrected by a reversing entry (47.16.4b).
- **Every change is recorded** in that client's **Activity Log** as `PLATFORM_DATA_EDIT`, with the record or table, its number, the fields changed and their before/after values (hidden values stay hidden), and also in the operator's own client log.

**Finding the log for a data issue.** Every client keeps its own logs, and the console opens them for either client: **`system_error_logs`** holds every server error raised while that client was working, with its module and correlation id (rows are in id order, so the newest are on the last page). Since 158.3 it also holds errors from users' browsers: severity **BROWSER**, module *browser* plus the screen, and the message with the file and line. Only signed-in users' errors from the app's own files are sent: each message once, at most five per page load, at most 20 a minute per session. Messages and stack traces are cut short, and the screen address is sent without its query, never form contents. **`audit_logs`** holds every change, including each console edit (`PLATFORM_DATA_EDIT`). Pick the client, open the table, and search by record number or correlation id; the same id appears in the error message the user saw. Both are view only.

**Where things are (raw database tables).** Every record type above is stored in the one `documents` table (by `doctype`); the record-type tables are the easier way to fix them. The other tables: `doctype_fields` / `doctype_meta` (each record type's fields and labels), `prefix_configs` and `sequence_counters` (number series and their running counters), `system_settings` (Configuration), `role_permissions` and `field_permissions` (access), `bin_stock*` and `inventory_*` (stock), `gl_postings` / `gl_accounts` (accounts), `approval_*` (approvals). Editing stock or ledger rows directly can leave stock and books out of balance; correct those with an adjustment or reversal through their screens where one exists.

### C.8 Control Panel — every client at a glance, pause and free trial (Stage 65.5, 65.6)

**Settings → Platform** holds the platform operator's pages (operators only, the same `ERP_PLATFORM_OPERATORS` setting as §C.7; anyone else sees a short note saying so). Each job is its own page with its own entry (ask 288): **Control Panel** (the four cards), **Clients**, **Releases**, **Certificates**, **Slow queries**, **Query plan** and **Capability maturity**. Two changes are made on **Clients** — **pausing** a client and its **free trial** (below); everything else opens the screen that makes the change.

**The four cards (Control Panel).** Click **Running release**, **Clients** or **Hosted at** to open **Releases**, **Clients** or **Certificates**.

- **Running release** — the version, the git commit and the release date of the binary that is serving the page right now, and the environment (`production` on the droplet).
- **Clients** — how many, how many are sandboxes, and how many have a problem (a missing or unusable schema — shown in red on the row).
- **Hosted at** — the domain each client's own address sits under (`TENANT_BASE_DOMAIN`, e.g. `wholeops.in`).
- **Last backup** — the most recent whole-database backup (time, outcome, size) from the nightly job's ledger; if it failed, the time of the last good one. "None recorded" means the backup job has never written its record on this server.

**Clients (Settings → Platform → Clients), one row per client.** Name, client id and schema; **Status** (active / suspended…, a *sandbox* or *legal hold* tag, and "admin not signed in yet" while the client's one-time admin login is unused); **Address** (opens it in a new tab — "main address" when the client has none of its own); **Created**; **Version** (the release the client is on, set at every release; "follows release" for a client no release has stamped yet, "pinned …" for a pinned one); **Modules** — how many are on and off; click it to list them; **Users** (active), **Records**, **Last activity** (its newest Activity Log entry) and **Errors (24h)** with the time of the last one (open them in the Data Console's `system_error_logs`, §C.7). **Errors (24h)** counts faults only: a background job failing, a crash, or the server unable to answer. A message a user was shown and can fix (a required field left empty, an expired session, a refused approval) is not an error; it is still listed in that client's **Settings → Monitoring → System Errors**, with the address of the request it came from. A report with no rows for its filters is not logged at all.

**Open — work inside a client with your own login (Stage 125).** On an active client with its own address, **Open** signs you in to that client in a new tab, as its Super Admin. You use no password and no authenticator there: your sign-in to your own client, with its authenticator, is what lets you in, so one authenticator entry covers every client. Use it to set a client up (Configuration, Roles, Users, number series…) and to support it.

- Behind it is a hidden user, `wholeops-support`, that the first **Open** creates in the client. Nobody can sign in as it — it has no password, and the sign-in screen refuses it — and the client never sees it: it is left out of their Users list, user pickers, user reports and user counts, and every Users action aimed at it (deactivate, reset password or 2FA, stores, location, supplier) answers *User not found*, so a client's Admin cannot lock you out.
- **Open** asks the server for a single-use code valid for 60 seconds and opens `https://<client>/#operator_handoff=<code>`; that page spends the code for an ordinary session lasting as long as a normal sign-in. A link used twice, or after a minute, shows the sign-in screen with *This link has expired or was already used* — click **Open** again.
- Every opening is recorded with your name in the client's audit log (`OPERATOR_OPEN_ISSUED`, then `OPERATOR_SESSION`) and its history (`tenant_lifecycle_events`, event `operator_opened`). What you then do there is logged under `wholeops-support`.
- No **Open** on your own client's row, on a paused or closed client (resume it first), or on a client with no address of its own (give it a hostname first). If the browser blocks the new tab, allow pop-ups for this site and click **Open** again.

**Actions on each row.** **Modules** opens **Tenant Entitlements** with that client chosen — turn a module on or off there, or apply a plan. **Data** opens the **Data Console** on that client. **Attributes** gives the client its product attributes from the industry library (§C.9); its **Back** returns to **Clients**. **Pause / Resume**, **Trial** and **Mark paid** are below. The row of the client you are signed in to says *your client* instead of those buttons: pausing it, or letting a trial run out on it, would lock you out of this page.

**Pausing a client** (non-payment, a customer's request, suspected misuse). Click **Pause**, type the reason, and click **OK**. Everyone at that client is signed out within a minute and cannot sign in again; nothing is deleted. The row shows *suspended* with your reason under it. **Resume** (again with a reason) lets them sign in straight away. Both go into the client's history (`tenant_lifecycle_events`) with your name, exactly as the server command `tenantctl suspend` / `resume` does (§C.6). While a client is paused - or its trial has ended - nothing runs in its name in the background either: no campaigns, reminders or dunning mail, no e-invoice submission, no webhook deliveries, no scheduled reports or queued jobs. That work waits and catches up after **Resume**. Log retention, audit evidence and security monitoring keep running for a paused client.

**Free trial.** A client on trial works normally until its end date, then is **paused automatically** — the same as **Pause**, nothing deleted — until you act. No job has to run: the end date is checked on every sign-in and every request.

- **Start a trial:** click **Trial** on the row, enter the number of days from today (30 is filled in), **OK**. The Status column then shows a *trial* tag and "trial ends 16 Nov 2026 · 30 days left". A new client can be given its trial when you create it: **Tenant Entitlements → Provision New Tenant → Free trial (days)**, 30 by default — clear it (or enter 0) for a client who is paying from day one.
- **Extend it:** click **Extend trial** and enter the days from today. This also gives a client whose trial has ended its access back at once.
- **They have paid:** click **Mark paid** and confirm. The trial ends with no end date; the client keeps full access.
- **When a trial ends:** the row shows *trial ended* and "paused by trial end" with the time, and the **Clients** card counts it under *paused*. Its users see the same sign-in failure as a wrong password (deliberately — the sign-in screen never says why a client is closed), so tell the client before the date if you can. Extend the trial or mark it paid to reopen it.


**Packages and the SaaS subscription (50.12, BLD-024 to BLD-027).** Sold alone: Full ERP, PIM, WMS, OMS. HR, Procurement + RFQ, Manufacturing, CRM & Loyalty, Fixed Assets and Expenses are add-ons that need one of those four; any other mix is refused (SAAS-0191) and the refusal is recorded.

- **Change a client's packages.** `POST /api/v1/admin/tenant/package` runs as one transaction with an `Idempotency-Key`: all modules move or none do, a retry changes nothing, and each attempt is audited. Preview first with `POST /api/v1/admin/tenant/package/impact` (`tenant_id`, `packages`): it lists modules gained and lost and the drafts, pending approvals and open documents that stay frozen in a lost module.
- **Downgrade and re-enable.** A switched-off module stops future work (screens, API, its background jobs); no record is deleted. Switch it on again and everything is back as it was.
- **Subscription (platform owner only).** Razorpay behind a small internal adapter, INR. Set `SAAS_BILLING_TENANT` to the client whose Razorpay keys are the platform's; save plans with `POST /api/v1/admin/saas/plan`, start one with `POST /api/v1/admin/saas/subscribe` (the buyer gets Razorpay's payment link). Register `/api/v1/integration/razorpay/subscription-webhook?tenant_id=<billing client>` in the Razorpay dashboard with the webhook secret. Each paid charge makes one GST invoice (18% inside the amount); a repeated or older event never makes a second one.
- **Failed payment.** The client keeps full access for 7 days (grace), then it is paused automatically; paying resumes it. A pause you made yourself is never lifted by a payment. A cancelled subscription works to the end of the paid period.
- **Reconcile.** `POST /api/v1/admin/saas/reconcile` (add `?provider=1` to ask Razorpay) corrects modules or status that drifted from the plan. Refunds are done in the Razorpay dashboard.
- **Usage and cost.** Documents, active users and storage count per calendar month in Indian time. `GET /api/v1/saas/subscription` is the client's own view; `GET /api/v1/admin/saas/costs?period=YYYY-MM` is every client's cost. Over the plan quota, a meter with an overage price keeps working and is billed; one without is refused (SAAS-0193). Overage is shown, not yet charged.
- **Subscription invoice and GST.** Each captured payment gets a tax invoice from the platform, GST-inclusive at 18%. Within the supplier's State it is split CGST + SGST, across States it is IGST; the place of supply is the client's GSTIN State, else the State in its Legal Entity address, else the supplier's State. Set the platform's `SAAS_SUPPLIER_GSTIN`, `SAAS_SUPPLIER_NAME` and `SAAS_SUPPLIER_ADDRESS` in the server environment and the setting **Subscription invoice SAC** (`saas.invoice_sac`) on the billing client. With the GSTIN or the SAC blank a payment is kept as *Pending*, uses no invoice number, and is numbered by the hourly sweep once both are set. Serials look like `SA/26-27/000123` (16 characters or fewer, one series per financial year). The SAC and the 18% rate wait for the CA (`docs/operations/ca-confirmation-pack.md`, row 50.12).
- **Usage feed.** The hourly sweep keeps each subscribed client's documents (per day), active users and database storage (latest reading of the month) up to date; replays change nothing. Files kept outside the database are not counted.
- **Screens.** *Settings > Customize > Subscription* (a client Admin): status in plain words, plan, period end, this month's use against what the plan includes, and the invoices list. *Tenant Entitlements* (Super Admin): choosing a plan for a client first shows what the client gains and loses and which open work stays frozen; confirm to apply.
The **Clients** card counts how many are *on trial* and how many are *paused* (by you or by a trial end). A sandbox has its own expiry and takes no trial. **When a sandbox's expiry passes** (47.18.3) the hourly sweep suspends it - it is refused exactly like a paused client, and the suspension (reason *sandbox expired on …*) is on its evidence trail. **Reset** gives it a new expiry and brings it back. Its data is never deleted by the sweep: to destroy an expired sandbox, deprovision it and purge it like any other client (§C.8), with the same backup reference and legal-hold check.

**Releases** (Settings → Platform → Releases) lists the last ten entries of the release ledger (`public.deployments`: when, environment, version, commit, who, status, and **What changed**). Since Stage 90 every `deploy\deploy.ps1` release writes its row once the new build is healthy, with the number from the rule in §C.2.1; `promote.ps1` writes its own rows too. Since Stage 90.5 **What changed** says in a few words what the release carries since the live build: the headline of each title Release.cmd wrote into the release commits (its ledger sections or new stages, without the stage number, cut at the first colon, semicolon or comma) and of any hand-made feature or fix commit, the first three on one line, then "+N more"; a release with no titles says "N files changed", one with only guides and trackers "Guides and records only". `deploy.ps1 -ReleaseNote "..."` still sets the note by hand. The releases recorded before that (0.1.0-0.13.0) were rewritten to the same short form by migration `migrations_stage90_6_release_notes.sql` (Stage 90.6). Releases before 2026-10-18 were not recorded, except `840909b`, which was entered as `0.1.0`, the number it ran under.

**Capability maturity** (Settings → Platform → Capability maturity, Stage 47.17.5) lists each capability - a module in one configuration, such as *POS · online till* or *Courier · Delhivery* - with its level (*Experimental*, *Preview*, *Production*, *Certified* with an expiry), its known limits and the date of the evidence. The same list is `docs/product/capability-maturity.md`; it changes only in `engines/capability_maturity.go`.

**Backups and certificates (Stage 49.11.7).** The Clients table has a **Backup** column: how long ago the newest successful backup that holds that client was taken - the nightly whole-database backup, or an export of that client alone, whichever is newer. It is red when older than 36 hours (the backup alert's limit) or when there is none. **Certificates** (Settings → Platform → Certificates) lists each public address - every `https` address in `CORS_ALLOWED_ORIGINS` and every client's own address - with its certificate's expiry, days left and issuer, as a browser sees it. The server connects to each every 6 hours (production only; not under the emergency switch) and keeps nothing but the dates. Red is under 14 days or a certificate that could not be read or is not trusted. Under 14 days also pages the operator once a day (`CERT_EXPIRING`): automatic renewal starts at 30 days, so it has been failing for two weeks - check the TLS setup (`deploy/enable_tls.sh`). Behind Cloudflare this is Cloudflare's certificate, not the one on the server. Privileged sessions are listed per client under Settings → Monitoring → Security Events (§B.4a).

**Slowest statements (47.9.3).** **Settings → Platform → Slow queries** lists the slowest statement shapes, with every value taken out: calls, mean, max and total time, and shared buffers when the source is `pg_stat_statements` (installed only after a Postgres restart with it preloaded; that path is not tested on a live server). Without it, a sampler looks every 15 seconds for statements running longer than `ERP_SLOW_QUERY_MS` (server environment, 1000 ms by default; 0 turns it off) and the table shows the database's overall buffer hit rate instead of per-statement buffers. The shapes are kept in memory only. **Save as baseline** stores the current table (one saved run, replaced on each save); later views show each shape's mean time as a change against it, red above +20%.

**Query plan (47.9.3d).** **Settings → Platform → Query plan** shows how the database runs one statement: `EXPLAIN (ANALYZE, BUFFERS)`, with real times and buffer reads. Pick the client, paste a `SELECT` (or click a statement on **Slow queries**: Query plan opens with it filled in; then put a real value in place of each `?`; `tenant_?.` is read as the chosen client's schema), and click **Explain**. The statement really runs, so it is held to one read-only `SELECT`: no second statement, no comments, no `$1`, nothing that writes (a `DELETE` inside `WITH` is stopped), and no functions that act outside it (cancelling sessions, advisory locks, reading files, running SQL from text). It stops after 10 seconds, only one plan runs at a time, and everything it did is rolled back. Only the plan comes back, never rows. A mistake in a name keeps its message; any other failure shows only its SQLSTATE code, because the database's message can quote a stored value. Each run is in the security log with the statement's shape (no values). How much disk each kind of transaction takes, and the growth to plan for by client size, is in `docs/operations/storage-forecast.md`.

**Statement timeout and disk alerts (Stage 49.13.3).** Configuration, Platform: **Database statement timeout** (`platform.db_statement_timeout_seconds`) cancels any database statement that runs longer than this many seconds. It is 0 (off) by default, so nothing changes until you set it; 60 is a sensible value once the slowest-statements table looks healthy. Reports keep their own five-minute limit, and migrations and backups are never cut. A cancelled statement shows as an ordinary error and the transaction rolls back. **Alert when free disk is below** (`platform.disk_free_alert_percent`, 0 = off by default; 10 is a sensible value) and **Alert when the WAL is larger than** (`platform.wal_size_alert_gb`, 0 = off; 8 is a sensible start) are read from the default client. The server checks every hour and pages `DISK_LOW` or `WAL_LARGE` once a day while a limit is crossed, through the same alert webhook as the backup and certificate alerts. The WAL check needs the database login to read the WAL directory (the `pg_monitor` role); without it that check is skipped.

**Lighter idle server (283.2, 283.3).** Background jobs read the client list and each client's module switches at most once per `AUTH_STATE_CACHE_SECONDS` (server environment, 30 by default; 0 reads them every time). A client added, suspended or resumed, a package change or a module switched in the app reaches them on their next run; a change made straight in the database can take up to that many seconds. The outbox, job queue, PIM publishing, report exports and CRM visit a client only when it has something due. A report or list the user leaves (closes the tab or goes to another page) now stops its query in the database at once, so its connection and report slot come back instead of waiting for the query to end.

### C.9 A client's product attributes, from the industry library (Stage 81)

Each client gets its product attributes from you, once — Colour, Polish, Size, Stone Colour for a jewellery client — each with its dropdown values already filled in. The client then uses them everywhere (the Item's variant options, PO designs, prints, PIM) without anyone being asked to invent an attribute. They stay with the client: it can switch an attribute off and remove values, but never delete one.

The attributes come from the **industry attribute library** that ships with the system — your workbook *ERP_Industry_Product_Attribute_Master.xlsx* (10 industries, about 670 attributes, about 190 dropdown lists), converted into the server by `go run ./cmd/genattrlib -xlsx "<path to the workbook>"`. Edit the workbook, rerun that command, and the next release carries the new library. Clients that already have attributes keep theirs; the next time you add from the library they get only what they do not have yet.

**To give a client its attributes:**

1. **Settings → Platform → Clients**, and on the client's row click **Attributes**.
2. **Industry** — the client's own industry is chosen (Jewelry when it has none yet). Pick another to see its list.
3. Three lists, decided by the workbook's own *Variant Driver* and *Value Scope* columns:
   - **Variant attributes** — what tells one sellable combination from another. The workbook's definite variant drivers (Primary Colour, Metal Tone, Metal Type, Purity, Ring Size, Bangle Size, Chain Length…) are ticked; the ones that are variant drivers only for some categories (Surface Finish — polish, Stone Colour, Plating, Clasp…) are listed but not ticked, so you tick what this client actually varies by.
   - **Product information - PIM only** — descriptive attributes for enrichment. All are ticked when the client has the PIM module; for a client without PIM none are added.

   **Two lists, because the ERP and PIM are separate apps.** A variant attribute goes to the ERP's own list (**Setup → Item Attribute**) — and, when the client also has PIM, to PIM's list too, mapped onto the ERP one. A product-information attribute goes to PIM's list only. The section headings say which.
   - **Not added** — attributes the ERP already keeps elsewhere (GTIN, HSN, shelf life, weight… on the Item) and ones recorded per batch or serial unit, not per item.
4. Each row shows its dropdown values, the workbook's note and whether the client **has it** already. **Use as** moves a row between Variant and Product information. A row marked *Similar: …* is not ticked because the client already has an attribute with a similar name — tick it only if you really want both.
5. **Add to <client>** and confirm. The summary says how many were added and how many it already had.

**What the client sees afterwards** (User Guide §8.1.2):

- **Item form → Variant Options** offers exactly the client's variant attributes and their values — there is no "Other…" to type a new one.
- **Purchase Order → Add by item name** offers the same attributes; a buyer can still add a *value* there (a new colour from a vendor), but not a new attribute — that is refused with *PIM-0691*.
- **Setup → Item Attribute** (under *Inventory*) is the ERP's list: the basic attributes an item varies by. Every client has it, with or without PIM; the Item form, PO designs and prints read only this list.
- **Setup → PIM → Product Attribute Def** is PIM's richer list, with **Used for** (Variant / Product information) and **ERP item attribute** — the ERP attribute a PIM attribute maps onto. Only clients with PIM have it.
- **Clients that had attributes before the two lists existed:** every attribute their ERP screens used (Used for *Variant*, or blank) was copied into Item Attribute with the same code, so their items and designs keep their values; the PIM attribute stays and is mapped onto the copy. Nothing was moved or deleted. The client can set **Status** to *Inactive* to switch one off, and remove or add values as chips. An item that already carries a removed value keeps it. Deleting an attribute that came from the library is refused (*PIM-0690*): its code and the *From the attribute library* field cannot be edited either.
- Adding from the library again never touches what the client has — an attribute it switched off stays off, values it removed stay removed.

**Category rules, Multi-select, units and regulated values (168.8, 168.12 to 168.15).** Four additions to attributes, each safe for an existing client until you choose to use it.

- **Category rules.** Setup → PIM → **Category Attribute Rule** (also **PIM → Category Rules**) holds one rule per category and attribute: *Required*, *Optional* or *Hidden*, with an optional default. Set **Parent category** on a Category to inherit; the nearest category's rule wins. To start from the workbook, open the client under Control Panel → Attributes and click **Category rules**: it shows how many rules it would add, how many are already there, how many name a category or attribute the client lacks, and how many *Conditional* or *Regulatory* rows are left out (only *Mandatory* becomes Required, *Optional* stays Optional). Confirm to add; running it again adds nothing twice and never changes a rule the client set.
- **Setting `pim.enforce_category_attribute_rules`** (PIM, default off). Off: a missing Required value only lowers the completeness score. On: blanking a Required value is refused (*PIM-9300*), and so is saving an existing item that still lacks one (*PIM-9301*). A rule saved wrongly gives *PIM-9302* (unknown rule), *PIM-9303* (a default on a Hidden rule) or *PIM-9304* (a second rule for the same category and attribute).
- **Multi-select.** A library attribute whose list takes several values (type *MultiEnum* in the workbook) now arrives in the PIM list as **Multi-select**. The ERP's own Item Attribute list has no such type, so that copy stays a single-choice list. Attributes already added keep their type. To switch an existing one, open Setup → PIM → Product Attribute Def, set **Value Type** to *Multi-select* and keep the options; values already stored stay as they are.
- **Units.** Set **Unit** on a Number attribute. Setting `pim.check_unit_attribute_values` (default off): when on, the value must be a plain number or it is refused (*PIM-9305*). Export headers and the item form show the unit either way.
- **Regulated values.** Set **Regulated** to *Yes* on a Product Attribute Def. Setting `pim.hold_regulated_attribute_changes` (default off): when on, changing a stored value of such an attribute files an **Attribute Value Change** for approval (rule: Store Manager) through the usual maker-checker approvals, honouring `approvals.self_approval`. The old value stays until it is approved; rejecting leaves it. A first value is saved at once. A product-sheet import or delete that would change a regulated value is refused (*PIM-9306*).

**Overlap with the Jewellery industry profile.** That profile also puts free-text **Color**, **Polish**, **Size** and **Stone Type** fields on the Item. They are left as they are — clients already hold data in them — and work alongside the variant attributes. Which of the two a jewellery client should use going forward is open (checklist 81.6).

### Install a new host, and the sandbox rehearsal

Two operator tasks have their own page, [docs/operations/install-and-sandbox.md](../operations/install-and-sandbox.md):

- **Install from zero:** `installcheck -env /etc/erp/erp.env -init-secrets -backup-dir /opt/erp/backups -channel stable` checks the host (Postgres, disk, memory, ports, clock, Caddy, DNS, certificate, backup folder, restore script, release signature), creates the secrets file without ever replacing a secret you set, and prints a PASS / WARN / FAIL table with the next step for each row. Exit code 1 means not ready.
- **Upgrade channel:** the Deploy click reads `UPGRADE_CHANNEL` from the host's env file. `hold` takes no deploy at all, `stable` (or nothing set) takes only stable builds, `candidate` takes both. A refused deploy says why and changes nothing; to deploy anyway, edit the line by hand on the host. A build is a candidate only if `release/channel.txt` says `candidate`.
- **Secrets beyond the files:** `go run ./cmd/secretscan -history 200 -binary bin/erp-server.exe -backups /opt/erp/backups` scans the added lines of the last 200 commits (0 = all), the text inside a built binary and a backup folder (every file encrypted, mode 0600, no clear dump left). It prints the kind and place of a hit, never the value, and exits 1 on any. A real hit needs the secret rotated; deleting it from history does not un-leak it.
- **Sandbox:** `tenantctl sandbox-create`, `sandbox-effects`, `sandbox-reset`. A sandbox holds invented data, stops every outside call and records what it stopped. Reset refuses any client that is not a sandbox. **Settings > Platform > Require a sandbox rehearsal** makes switching on an un-rehearsed connector a refusal instead of an audit warning. A client created from now on starts with it **On** (162.19); a client that existed before keeps what it had (Off unless its admin chose On). Turn it off for a client only when its go-live does not include a rehearsal.

### C.10 Data governance: retention, masked copy, employee and vendor privacy (Stage 170.38-170.40)

**Retention per record type.** Settings -> Configuration -> Data governance (super admin only) lists how long each personal field is kept. The table starts with today's rules (driver phone on a gate pass 90 days, audit-log and sign-in addresses 180 days, customers, employees and vendors anonymised on request). The hourly sweep reads it; a client under legal hold is never swept. The same sweep removes the buyer's e-mail from a web-shop order once it is Delivered or Closed (the customer record keeps it), and an order stores a billing address only when it differs from the shipping address (49.6.2).

1. Open **Data governance**. The list shows record type, field, keep, then and legal basis.
2. Click **Edit** on a row, or **New**. Change *Keep for*, *Unit* or *Then* and click **Save rule**.
3. The audit-log and sign-in addresses cannot be set below 180 days (SEC-1701). A *clear field* rule needs a field name (SEC-1700).
4. *Keep for* 0 means the sweep does nothing for that row. The integration-log windows keep their own settings.

**Masked copy of a client.** Data governance -> *Masked copy of client* makes a NEW sandbox client holding that client's documents with every personal and protected field replaced by a stable fake (names, phones, emails, addresses, tax and bank ids). It never writes to the source and never copies users, passwords, sessions or credentials. From a terminal: `tenantctl sandbox-masked-copy -from <client> [-ttl-days N] -yes` (without `-yes` it only reports).

1. Type the client to copy and the days to keep the sandbox.
2. Click **Make masked copy**. The sandbox admin password shows once; store it.

**Privacy requests (customers, employees, vendors; 49.6.8).** An admin opens a person's requests from the **Privacy** button on their row in the Customer, Employee or Vendor list.

1. Click **Privacy**. The list shows each request's type, status, who opened it and the date.
2. Click **New**, pick the *Type*, add a *Reason* and click **Save**. A customer request is Access, Export, Correction, Erasure, Anonymise or Consent withdrawal; for a *Correction* type only the new values (name, phone, email, address, city, state, PIN code, PAN, GSTIN, birth date, anniversary, gender). An employee or vendor request is Access, Export, Erasure or Anonymise.
3. A second admin clicks **Approve**, or **Reject** with a reason. The admin who opened a request cannot decide it.
4. Click **Run** on an approved request. Erasure and Anonymise ask first and cannot be undone. Export and Access save the person's copy as a JSON file at once; it is not kept, so hand it over (a new request makes a new copy).

**Employee and vendor privacy requests.** Like Customer: export, then erase (anonymise). Payroll, tax and ledger fields (PAN, GSTIN, bank, UAN, ESIC, code, dates) are kept. A request is refused with a reason when the record is under legal hold (SEC-1705), an employee is still active (SEC-1706) or inside the statutory keep window (SEC-1707), or a vendor still has open orders, invoices or receipts (SEC-1708).
