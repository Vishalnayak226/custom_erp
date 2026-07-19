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

Three triggers, deliberately not everything that gets logged (to avoid alert fatigue — see `system_error_logs`'s much wider set of `LogSystemError` call sites across `internal/server`'s handler files, most of which are single-request failures, not incidents):

1. **Panic recovery** — every `PANIC`-severity `engines.LogSystemError` call (from `internal/server/middleware.go`'s panic-recovery middleware) alerts immediately, one alert per panic.
2. **Failed backup** — `manage.ps1 backup` (`Backup-Databases`) alerts on any failure (Postgres down, `pg_dump` failure, etc.) via the same webhook, through `Send-OpsAlert` in `manage.ps1`.
3. **Sustained error rate** — `engines.StartAlertMonitor` polls every tenant schema's `system_error_logs` once a minute; if a schema logs 20+ rows (any severity) within a rolling 5-minute window, it alerts once, then waits out that 5-minute cooldown before alerting again for the same schema (so a stuck-broken schema pages once per window, not once per poll tick).

**What's sent**: severity, source (module/schema), and a truncated (300-char) message only — never a full stack trace or request body, since the payload leaves this process for a third-party webhook. Full detail stays in `system_error_logs` / the Log Hub, one hop away via the correlation id already alongside it in `erp-server.out.log`.

**To activate**: set `OPS_ALERT_WEBHOOK_URL` in the environment the server and `manage.ps1` both run in (same variable name for both — one webhook covers Go-side and script-side alerts). A Slack incoming webhook URL looks like `https://hooks.slack.com/services/T000/B000/XXXX`; a Teams classic incoming webhook looks like `https://<tenant>.webhook.office.com/webhookb2/...`.

---

## 4. Log locations

| What | Where |
|---|---|
| Server stdout/stderr (dev) | `logs/erp-server.out.log`, `logs/erp-server.err.log` (repo root) — `.\manage.ps1 logs` tails all three |
| Server stdout/stderr (test/live) | Same filenames under that environment's worktree (`environments.json` → `worktree` path) |
| Postgres log | `logs/postgres.log` |
| Application error/panic trace | `system_error_logs` table, per tenant schema — query directly or via the Log Hub UI (integration log query/retry endpoints, `engines/logs.go` + `engines/outbox.go`) |
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

## 6. Acceptance gate — how this was verified

Per `micro_checklist.md` 17.10's acceptance gate ("a throwaway failure produces both a system log and an approved-destination alert without leaking secrets"): verified live against a throwaway server instance with `OPS_ALERT_WEBHOOK_URL` pointed at a local mock webhook receiver (not a real Slack/Teams destination, since none was available at build time) — see `docs/project_ledger.md`'s Stage 17.10 entry for the exact steps and what was observed. The code path is identical for a real destination; only the URL changes.
