---
doc_id: DOC-D65D6C4A50
title: Incident Runbook
type: procedure
status: draft
owner: operations-owner
approvers: [documentation-maintainer, operations-owner]
audience: [maintainers, operations-owner]
applies_to: source documentation; scoped release acceptance required
authority: canonical
confidentiality: internal
last_verified: 2026-10-26
review_by: 2026-12-26
supersedes: none
superseded_by: none
verification_scope: metadata and lifecycle classification; domain acceptance pending
---

# Incident Runbook

Operational response procedure for this ERP: severity levels, escalation, rollback, log locations, and the automated alerting that backs this up (Stage 17.10). Pair this with [`docs/operations/backup_restore.md`](backup_restore.md) (backup/restore mechanics) and [`docs/ai_handover.md`](../ai_handover.md) §1-§3 (environment/port map, start/stop commands).

---

## 1. Severity levels

| Level | Definition | Examples | Response target |
|---|---|---|---|
| **P0** | Live environment down, or data-integrity risk (wrong GL postings, corrupted stock, lost orders) | ERP server down/unreachable on `live`; Postgres down; a bug is posting unbalanced GL entries | Acknowledge within 15 min, begin mitigation immediately, all-hands until resolved |
| **P1** | Major function broken for `live`, no safe workaround | Checkout/POS billing failing; login broken; a whole module 500s | Acknowledge within 1 hour, fix or rollback same business day |
| **P2** | Degraded but workaround exists, or non-`live` environment down | A report is wrong but not GL-affecting; `test` env down; one connector (Shopify/BigCommerce/Magento) failing while others work | Fix within the current sprint/next few days |
| **P3** | Cosmetic, low-impact, or a documented scope gap | UI copy/label issue; a `[needs design decision]` item in `micro_checklist.md` | Backlog, no fixed SLA |

Severity is set by whoever first triages the incident, using best judgement against the table above — do not wait for a fixed threshold to be crossed before acting on an obvious P0.

---

## 2. Escalation contacts

**[NOT YET FILLED IN — placeholder, do not treat as live]**

This table needs real names/roles and a reachable contact method before Stage 17.10 can be marked closed. Fill in and remove this placeholder line once done.

| Role | Name | Contact (phone/email/Slack) | Hours |
|---|---|---|---|
| Primary on-call | _TBD_ | _TBD_ | _TBD_ |
| Secondary / escalation | _TBD_ | _TBD_ | _TBD_ |
| Business owner (sign-off on P0/P1 comms) | _TBD_ | _TBD_ | _TBD_ |

---

## 3. Automated alerting

`engines/alerting.go` posts to a Slack-compatible incoming webhook (Slack itself, or Microsoft Teams' classic "Incoming Webhook" connector — both accept a plain `{"text": ...}` payload) configured via the `OPS_ALERT_WEBHOOK_URL` environment variable. **Unset by default** — until it's set, alerts are logged locally only (`[ALERT] (no OPS_ALERT_WEBHOOK_URL configured, not sent) ...` in `erp-server.out.log`) and nothing goes out. This is the one piece of Stage 17.10 that only the user can supply — the code path is built and verified (§6), it just needs a real destination wired in.

Four triggers, deliberately not everything that gets logged (to avoid alert fatigue — see `system_error_logs`'s much wider set of `LogSystemError` call sites across `internal/server`'s handler files, most of which are single-request failures, not incidents):

1. **Panic recovery** — every `PANIC`-severity `engines.LogSystemError` call (from `internal/server/middleware.go`'s panic-recovery middleware) alerts immediately, one alert per panic.
2. **Failed backup** — on Windows/dev, `manage.ps1 backup` (`Backup-Databases`) alerts through `Send-OpsAlert`. **On production, `deploy/backup.sh` does the same as of Stage 43.2** (ERR/EXIT traps → the same webhook); before that the Linux nightly job had no alerting at all and a failure only ever appended to `/var/log/erp-backup.log`. The alert names the failing stage (`pg_dump`, `openssl`, `psql`) but deliberately not its arguments, which carry credentials.
3. **Missing or stale backup** (Stage 43.2; database record since 56.9) — `engines.StartBackupFreshnessMonitor` checks hourly that the newest successful whole-database run recorded in `public.backup_runs` (written by `deploy/backup.sh`) exists and is under 36 hours old, alerting once per window otherwise; on-demand tenant exports never count, and the alert says so when the latest run recorded a failure. **This is the trigger that catches what trigger 2 cannot**: a cron that was never installed, or never fired, produces no failed run to report — exactly the state 26.11.3 found on this droplet, where nightly backups had silently never run. 36h rather than 24h so one late run does not page anyone, while two consecutive missed nights always does (which is `hypercare_plan.md`'s rollback trigger). A dev machine with no recorded run stays quiet; under `ENV=production` no recorded backup is never silent — it alerts and is logged at every boot as security-baseline finding **SB-026**.
   **Off-box copy (Stage 75.1.4-75.1.6).** With `OFFSITE_S3_*` set in `/etc/erp/erp.env`, `deploy/backup.sh` also uploads each dump (and its checksum) to the bucket, and the nightly brings an upload-only copy of `media_store` up to date. A failed upload raises **`BACKUP_OFFSITE_FAILED`** (the local backup still counts — it completed), and the same hourly monitor raises **`BACKUP_OFFSITE_STALE`** when the newest whole-database run with a successful off-box copy is missing or older than **26 hours** — so an upload that keeps failing night after night, which trigger 3 alone would pass, is caught. Checked only where off-box copies are expected (the bucket is configured, or an off-box result was ever recorded). **What to do:** read the newest `public.backup_runs` rows (`offsite_outcome`, `detail`); check the bucket's token has not expired or lost write access, the droplet can reach the endpoint, and the bucket is not full; run `deploy/backup.sh` by hand and watch its last line (`off-box: Success`).
4. **Sustained error rate** — `engines.StartAlertMonitor` polls every tenant schema's `system_error_logs` once a minute; if a schema logs 20+ fault rows (any severity) within a rolling 5-minute window, it alerts once, then waits out that 5-minute cooldown before alerting again for the same schema (so a stuck-broken schema pages once per window, not once per poll tick). A fault is a background-worker error, a panic or a 5xx answer; a request the user was told to correct (a row starting with a catalog code the catalog answers with 2xx/4xx, e.g. `[GLOBAL-0001]`) is not counted (Stage 104.2), though it stays in the log.

**What's sent**: severity, source (module/schema), and a truncated (300-char) message only — never a full stack trace or request body, since the payload leaves this process for a third-party webhook. Full detail stays in `system_error_logs` / the Activity Log, one hop away via the correlation id already alongside it in `erp-server.out.log`.

**To activate**: set `OPS_ALERT_WEBHOOK_URL` in the environment the server, `manage.ps1` and `deploy/backup.sh` all run in (same variable name for all three — one webhook covers Go-side and script-side alerts). On production that means `/etc/erp/erp.env`, which both the `systemd` unit and the backup cron already load. A Slack incoming webhook URL looks like `https://hooks.slack.com/services/T000/B000/XXXX`; a Teams classic incoming webhook looks like `https://<tenant>.webhook.office.com/webhookb2/...`.

---

## 4. Log locations

| What | Where |
|---|---|
| Server stdout/stderr (dev) | `logs/erp-server.out.log`, `logs/erp-server.err.log` (repo root) — `.\manage.ps1 logs` tails all three |
| Server stdout/stderr (test/live) | Same filenames under that environment's worktree (`environments.json` → `worktree` path) |
| Postgres log | `logs/postgres.log` |
| Application error/panic trace | `system_error_logs` table, per tenant schema — query directly or via the Activity Log UI (integration log query/retry endpoints, `engines/logs.go` + `engines/outbox.go`) |
| Audit trail (document changes, approvals) | `audit_logs` table, per tenant schema (`engines/logs.go`'s `LogAuditEvent`) |
| Deployment history | `public.deployments` table — `.\manage.ps1 fleet-status` shows the latest per environment; `promote.ps1`'s `Record-Deployment` writes every promotion/rollback |
| Correlation id | Every panic response body and `system_error_logs` row carries one (`Resolved-Correlation-ID` header) — use it to jump from a user-reported error to its exact server-side trace |

---

## 5. Rollback

This project has no Docker/container deployment path (reverted by explicit decision, see `ai_handover.md` §6) — `promote.ps1`/`manage.ps1 -Env` is the one real deployment path.

**Roll back a bad promotion to `test` or `live`:**
```powershell
.\promote.ps1 -Rollback -Env live
```
Re-checks-out and restarts that environment's previous recorded-passing commit (from `public.deployments`), and records the rollback itself as a new deployment row.

**Roll back a bad database change** (only after the app-level rollback above, if the bad deploy also wrote bad data):
```powershell
.\manage.ps1 stop -Env live
.\manage.ps1 restore -Env live -File .\backups\live\<latest-good>.dump -ConfirmRestore "RESTORE live"
.\manage.ps1 start -Env live
```
See `docs/operations/backup_restore.md` for the full restore procedure and the confirmation-string safety gate.

**If the incident is a specific bad commit already merged to `main`:** `git revert` it (not `reset --hard` — this is a shared repo, see `ai_handover.md` §6 concurrent-session note) and promote the revert through `test` → `live` as normal, rather than hand-editing a running environment.

---

## 5a. Transaction runbook (Stage 130.16, BRD §14.1)

| Situation | What it means | Do | Never |
|---|---|---|---|
| **Provider unknown** - an IRN or e-way bill sent with no answer (IRN *UNKNOWN*, e-way bill *Submitted*) | It may already exist at the government system | Let the worker look it up (IRN workbench *Check now*; e-way bill **Generate** again looks up first); if the portal shows it, record it (*Record from portal*) | Resubmit blind, or change the document number to "try again" |
| **Payment unknown** - a card / UPI tender timed out at the till | The money may have moved | Check the terminal / UPI app first; use the till's pending-payment recovery; record the reference | Take the payment a second time before checking |
| **Paper jam / print lost** | The print log says *Sent to browser* or *Submitted* - nothing confirms paper | Reprint from the document or Setup → Printing → Reprint with a reason; with reprint control, the Reprint Request is approved first | Edit the document to print it "fresh" |
| **Rejected at the GRN** | Rejected units do not fulfil the PO line; they stay open for replacement | Send them back with a Purchase Return; the vendor's replacement comes on a new receipt | Delete the GRN or the PO line |
| **Partial receipt** | The PO stays open for the rest | Receive what came; short-close the PO with a reason if the rest will not come | Edit the received quantity up to the order |
| **Partial challan / job work return** | The rest stays *still out* | Receive what came; write off what will not come back (approval) | Close the challan with goods still out |

## 6. Acceptance gate — how this was verified

Per `micro_checklist.md` 17.10's acceptance gate ("a throwaway failure produces both a system log and an approved-destination alert without leaking secrets"): verified live against a throwaway server instance with `OPS_ALERT_WEBHOOK_URL` pointed at a local mock webhook receiver (not a real Slack/Teams destination, since none was available at build time) — see `docs/project_ledger.md`'s Stage 17.10 entry for the exact steps and what was observed. The code path is identical for a real destination; only the URL changes.

---

## 7. Security incidents (Stage 49.12)

Sections 1-6 are about the service being up. This section is for someone **attacking or misusing** it. Detections in **Activity Log → Security** name the section to open (§7.1-§7.8); the same sections apply when a person reports something.

### 7.0 Classes, severity and who decides (49.12.1)

| Class | Example | Severity |
|---|---|---|
| Account | stolen password or session, MFA bypass, a disabled user still acting | P1; P0 if an administrator |
| Client crossover | one client's session or data reaching another client | P0 |
| Data leaving | bulk export, an unknown API key reading data | P0 if personal or financial data left |
| Integrity | tampered audit evidence, unbalanced ledger, stock changed outside the app | P0 |
| Availability | flooding, a poison job, the database or disk full | P1 (P0 if sales stop) |
| Supply chain / release | a vulnerable dependency or a bad release live | P1; P0 if exploited |
| Provider | payment, courier, channel, mail or SMS provider compromised or down | P2; P1 if money or orders are wrong |
| Insider | an employee misusing their access (refunds, voids, exports) | P1 |

**Decide in this order:** (1) Is data leaving or being changed right now? Contain first (§7.1-§7.10), then investigate. (2) Does it cross clients? P0 at once. (3) Is money, stock or the audit trail affected? Integrity questions go to the business owner before anything is "fixed" in the data.

**Who:** today one person holds every role. The platform owner is incident commander and technical lead, and decides customer communication. The client's owner signs off on what their business tells its customers. Alternates, out-of-band phone numbers and legal or privacy contacts are §2's placeholders and need the owner's names. Until they are filled in, P0 response depends on one person. **Out-of-band:** if the ERP or its mail may be compromised, use the phone, not ERP notifications.

**Every incident:** take the evidence pack first (§7.11 step 1), write the timeline as you go (§7.12), and never repair evidence rows by hand.

### 7.1 Stolen password or session

- **Signs:** detections *Password spray*, *Many wrong passwords*, *Several accounts locked*; a user reports sign-ins they did not make.
- **Contain:** Activity Log → Security → choose the user → **Sign out everywhere** (asks a reason and your password; their remembered devices are forgotten too). Then Users → **Reset password** (dual control for an administrator) and **Reset 2FA** if the phone may be gone.
- **Keep:** the evidence pack for the days involved; the sign-in rows named in the detection.
- **Recover:** the user signs in with the new password and sets up 2FA again.
- **Check:** no new detections for that user for 24 hours; their recent work in the audit log is their own.

### 7.2 Privileged account misused, or a disabled user still acting

- **Signs:** *Many role or user changes by one person*; *A disabled user acted after being disabled* (that one is a control failure - the session should already be dead).
- **Contain:** sign that administrator out everywhere; if more than one account is involved, **Sign out everyone** (you stay signed in). Undo unexpected role changes from Users / Roles.
- **Keep:** evidence pack; the USER_MANAGEMENT and ROLE_PERMISSIONS rows (who changed what).
- **Recover:** reset the account's password and 2FA; review every role it changed.
- **Check:** Activity Log → Security shows no admin without 2FA; the disabled-user detection stays quiet.

### 7.3 Client crossover or access probing

- **Signs:** *A session from another client used here* (refused, but a token was carried across); *One user refused again and again*.
- **Contain:** for crossover, sign the session's user out everywhere **in their own client**, then treat it as P0 until it is explained. For probing, ask the user; sign them out if it was not them.
- **Keep:** evidence pack in **both** clients; the TENANT_ROUTING rows.
- **Recover:** if any record was read across clients, it is a data-leaving incident (§7.6) for the client whose data it was.

### 7.4 Exposed secret or API key

- **Contain:** an API key: Integrations → API credentials → **Revoke** (or **Rotate**). A server key (`JWT_SECRET`, `CHANNEL_CREDENTIAL_KEY`, `STORED_VALUE_KEY`, `AUDIT_SIGNING_KEY`): rotate with the numbered-key ring described in `deploy/erp.env.example` (add `_n`, restart, remove the old one), never by replacing the value in place, or sealed data can no longer be read. Rotating `JWT_SECRET` signs every user of every client out.
- **Keep:** where the secret was seen (file, chat, log line) and from when.
- **Check:** the old key fails; the provider's own console shows no use after rotation.

### 7.5 Malicious connector, webhook or poison job

- **Signs:** *Background jobs giving up*; a webhook burst in the integration log.
- **Contain:** disconnect the connector (Integrations); `ERP_DISABLE_EXTERNAL_SIDE_EFFECTS=true` stops every outbound effect on the server. Do not retry a poison job in a loop - Activity Log → Async Jobs shows it; fix the cause first.
- **Recover:** reconnect with a new credential; replay only the jobs you have read.

### 7.6 Data leaving (export burst)

- **Signs:** *Many exports by one user*; DATA_EXPORT rows you did not expect.
- **Contain:** Activity Log → Security → **Freeze exports** (every export and report download for this client is refused and logged; audit evidence exports stay open), then sign the user out everywhere. Revoke any API key involved (§7.4).
- **Keep:** evidence pack; the DATA_EXPORT rows say what was taken and when.
- **Recover:** **Unfreeze exports** once the account is safe. Whether customers or regulators must be told is a legal decision (49.12.5) - record the facts, do not decide it alone.

### 7.7 Till fraud

- **Signs:** *Many cancels, voids or price overrides by one user*.
- **Contain:** the store manager reviews the bills with the cashier before the till closes; sign the cashier out if it continues.
- **Keep:** the POS_BILL_CANCELLED / POS_SALE_VOIDED / POS_PRICE_OVERRIDE rows and the till session report.

### 7.8 Audit evidence failed or stopped

- **Signs:** *Audit evidence failed or could not be verified*; *Audit checkpoints stopped while events waited*; **Audit writes lost** above 0 on the Security pane.
- **Contain:** take a database backup **before** anything else - it is the evidence. Do not edit audit rows.
- **Investigate:** the verifier names the row ids. A tampered row with the business record unchanged points at someone with database access; a key mismatch after a restore points at a missing `AUDIT_SIGNING_KEY`. Checkpoints stopping: check the job runner and the scheduler leader (`SCHEDULER_LEADER`). Lost writes: the reason is on the Security pane; usually the database or the disk.

### 7.9 Vulnerable dependency or bad release

- **Contain:** a deploy that fails its health check rolls itself back (`deploy/README.md`). For a release that is healthy but dangerous, deploy the previous release commit with the Deploy click. The schema stays forward (migrations are additive), which is safe.
- **Fix:** patch, release through every gate, deploy. The release report's *Security impact* (gate G6b) shows what changed in the attack surface.

### 7.10 Ransomware or destructive database action

- **Contain:** stop the service (`systemctl stop erp`), take the host off the network if it is compromised, keep the disk.
- **Recover:** restore the newest backup taken **before** the compromise onto a clean host (`docs/operations/backup_restore.md`), check it with `erp-server -verify-restore` (the restore drill runs the same check) before serving it, and rotate every key (§7.4).
- **Check:** ledgers balance, audit evidence verifies, reconciliations tie; tell the client how much data was lost (from the backup's time).

### 7.11 Eradication and recovery (49.12.6)

1. **Evidence first.** Activity Log → Security → **Evidence pack** for the incident window: a zip of the audit rows with their signatures, system errors, checkpoints, the releases live then and every migration's checksum, with a signed manifest. Collecting it changes nothing and is itself logged. Keep a copy off the ERP host.
2. **Entry and blast radius.** From the pack and **Trace a request** (the reference in any error message): which account, key or route, which clients, which records, since when.
3. **Remove persistence.** Revoke every credential the attacker could have held - sessions (sign out everyone), API keys, connector credentials, and server keys if the host was reached. A compromised worker or integration is not reconnected until its credential is new.
4. **Rebuild from trusted state.** Code from a release commit on origin, deployed by the Deploy click. Data from a backup that passes `-verify-restore`, when a restore is needed.
5. **Reconcile.** Ledgers, stock, open orders and payments for the window (Reports → reconciliation; the restore check runs the same checks).
6. **Watch.** The detection that caught it stays quiet for a week; read the Security pane daily.

### 7.12 Incident record and lessons (49.12.7)

Write one record per incident in a private incident folder - never in this repository, and never with secrets or customer data:

| Field | Write |
|---|---|
| Timeline | detection, decisions, containment, recovery - times in UTC |
| What happened | entry point; accounts, keys and clients involved; what data; what money or stock |
| Evidence | the pack's file name and its `manifest.sig`; where it is kept |
| Customer commitments | what each client was told, when and by whom |
| Changes | each fix as a checklist item: a control, a test, a detection rule, a runbook line, a document |
| Closed when | every change item is done and verified |

Blameless: the record asks what let it happen, not who. A reusable lesson goes into the right test, detection rule or runbook section without the incident's evidence.
