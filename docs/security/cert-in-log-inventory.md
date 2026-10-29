---
doc_id: SEC-CERTIN-001
title: CERT-In security-log inventory and six-hour reporting
type: normative
status: draft
owner: security-owner
approvers: [security-owner]
audience: [engineering, operations, security-owner, customers]
applies_to: source release 0.18.0; every India deployment
authority: proposed-policy
confidentiality: internal
last_verified: 2026-10-28
review_by: 2027-01-28
supersedes: none
superseded_by: none
---

# CERT-In security-log inventory and six-hour reporting

Stage 47.16.3. CERT-In's directions of 28 April 2022 (IT Act section 70B(6)) ask every body corporate to (1) report listed cyber incidents to CERT-In within **six hours** of noticing them, (2) keep the logs of its ICT systems for a **rolling 180 days within India**, and (3) synchronise system clocks to the NTP servers of NIC or NPL, or to servers traceable to them. This page lists every security-relevant log the product writes, where it lives, how long it stays and which clock stamps it, and sets out the six-hour workflow. It is a product control map, not legal advice: whether and how the directions bind a given client is that client's counsel's call (Stage 47.16 header). The row-by-row legal mapping is the [compliance control matrix](compliance-control-matrix.md).

## 1. Log inventory

Read from source and the schema on 2026-10-28. "Floor" means the 180-day minimum enforced in `retentionDelete` (`engines/security_log_retention.go`): whatever the retention setting says, a row on this list younger than 180 days is never swept. A setting of 0 still keeps everything; a client under legal hold keeps everything. Every in-database store is also on `SecurityStateStores` (`engines/bounded_state.go`).

| Log | Where | What it records | Retention | Clock |
|---|---|---|---|---|
| Audit trail | `audit_logs`, per client schema | sign-in, MFA, lockout, login abuse, privilege and setting changes, exports, detections, emergency controls, job give-ups (full taxonomy: [audit-and-logging-policy.md](audit-and-logging-policy.md)) | never deleted; sealed windows move to a signed, encrypted archive no earlier than 30 days (`RunAuditArchive`, floor `minArchiveAfterDays`); the archive is never deleted | database time; production PostgreSQL runs in UTC, and signed rows carry UTC |
| Audit archives and checkpoints | `audit_archives`, `audit_checkpoints`; archive files under the server's archive directory | hourly sealed digests of the audit trail; archived windows | kept as evidence, never swept | UTC |
| Public API traffic | `api_request_log` | credential, method, path, scope, status, duration, client address, correlation id | `platform.public_api_log_retention_days` (default 30) **raised to the 180-day floor** | `timestamptz` (absolute) |
| System errors | `system_error_logs` | severity, module, message, stack trace, correlation id | `platform.error_log_retention_days` (default 180), floor 180 | database time (UTC in production) |
| Integration deliveries | `integration_event_log` | deliveries to and from connected systems | `platform.integration_log_retention_days` (default 90) **raised to the 180-day floor** | database time |
| Extension hook calls | `extension_hook_log` | each call to a client-configured hook: hook, payload hash, latency | same setting, floor 180 | database time |
| Approvals | `approval_log` | every maker-checker submit and decision | evidence, never swept | database time |
| Job history | `async_jobs` | background jobs, attempts, last error | `platform.job_runner_retention_days` (default 30) - **not** floored: a give-up is also an audit row (`JOB` / `DEAD_LETTERED`) | database time |
| Payload copies | `integration_event_outbox`, `clevertap_event_log`, `pim_publish_log` | copies of business payloads (customer events, catalogue snapshots), not access records | their own setting, **deliberately not floored** - keeping customer event data longer than needed is the over-retention 49.6.2 forbids | database time |
| Web access log (host) | `/var/log/caddy/erp-access.log` | every HTTPS request: time, client address, method, path, status | `roll_size 50MB`, `roll_keep 10` and Caddy's default 90-day age limit - **gap, see §3** | absolute (epoch) |
| Server log (host) | systemd journal, `erp.service` | startup banner, security-baseline findings, job and sweep failures | journald capped at 100 MB (`deploy/server_cleanup.sh`) - **gap, see §3** | host clock |
| SSH and system sign-ins (host) | `/var/log/auth.log` | operator SSH access, sudo | rotated logs removed by `deploy/server_cleanup.sh` - **gap, see §3** | host clock |
| Backups and drills | `public.backup_runs`, `public.ops_run_log` | each backup, off-box copy and restore drill | evidence, never swept | database time |
| Releases | `public.deployments` | the release live at any moment | never swept | database time |

**Clocks.** Every in-database timestamp is server time; production PostgreSQL runs in UTC (confirmed 2026-10-26). Business-day reports convert to the client's day (`engines.BusinessToday`, `public.erp_day_start`), never the stored timestamp. CERT-In reports and the incident timeline (incident runbook §7.12) are written in UTC with the IST equivalent beside it.

**Residency.** Every log above lives on the application host and its PostgreSQL; off-box backups (`OFFSITE_S3_*`) carry the database copies. Whether that host and that bucket are in India is a hosting decision per deployment, recorded on the [third-party risk register](third-party-risk-register.md) - the product cannot check it.

## 2. Six-hour reporting workflow

The clock starts when anyone first **notices** the incident - a detection in Activity Log → Security, a customer report, a provider notice. Who files depends on whose system it is: the client reports incidents in its own business (its users, its devices, its data); the platform operator reports incidents in the hosted service (the server, the database, the release). A cross-client incident is the operator's to file and every affected client's to know. Both need a point of contact registered with CERT-In beforehand - **open, see §3**.

| Hour | Do | Where |
|---|---|---|
| 0 | Note the time noticed (UTC). Classify: is it one of CERT-In's reportable types (table below)? If unsure, treat it as reportable. | incident record (runbook §7.12) |
| 0-1 | Contain - runbook §7.1-§7.10 for the class. Take the **evidence pack** for the window: Activity Log → Security → Evidence pack (signed manifest; collecting it changes nothing and is itself audited). | runbook §7.11 step 1 |
| 1-5 | Gather what the report form asks: affected systems, time noticed, symptoms, indicators (addresses, accounts, keys), actions taken. **Trace a request** joins the audit, error and job rows by correlation id. | Activity Log → Security |
| by 6 | File with CERT-In (incident@cert-in.org.in, or the form at cert-in.org.in) with what is known; say what is still being established. Record the filing time and reference in the incident record. | incident record |
| after | Answer CERT-In's follow-up; the 180-day logs and the pack are the evidence. Keep a copy of the pack off the ERP host. | runbook §7.12 |

| CERT-In reportable type (Annex I, abridged) | Runbook class (§7.0) | Product signal |
|---|---|---|
| Targeted scanning or probing of critical networks | Client crossover / access probing | `GLOBAL-0011` and `TENANT_ROUTING` audit rows, detection rules |
| Compromise of systems or data; identity theft, spoofing, phishing | Account | `LOGIN_ABUSE`, `SECURITY_ALERT`, MFA and lockout rows |
| Unauthorised access to IT systems or data | Account, Insider | audit trail, sensitive-field denials |
| Defacement, intrusion, malicious code (ransomware, botnets) | Integrity, Availability, Supply chain | audit verification failure, `security_detection.go` rules, host logs |
| Attacks on servers (database, mail, DNS) and network devices | Availability | system error log, backup monitor, host logs |
| Data breach or data leak | Data leaving | `DATA_EXPORT` rows, export-burst detection |
| Attacks on applications (e-governance, e-commerce, payments) | Provider, Integrity | integration logs, dead-lettered jobs, webhook refusals |
| Attacks on or through cloud, IoT, AI or mobile components | Provider | host and integration logs |

A personal-data breach may also need a separate notice under the DPDP Act (to the Data Protection Board and each affected person). That workflow is not built - Stage 49.12 / 47.16.2 - and is marked as a gap in the compliance control matrix.

## 3. Open items (not built here)

1. **Host logs below 180 days.** The web access log keeps Caddy's default 90 days; the journal is capped at 100 MB; rotated `auth.log` files are deleted. Proposed: add `roll_keep_for 4320h` to the `log` block in `deploy/Caddyfile` (size is still capped by `roll_size`/`roll_keep`), size the journal for 180 days of `erp.service` output and keep rotated `auth.log` for 180 days in `deploy/server_cleanup.sh` - a disk decision for the operator, to be checked on the host after the next deploy.
2. **Time synchronisation.** No deploy step points the host at NIC/NPL time. Proposed: `/etc/systemd/timesyncd.conf.d/erp-ntp.conf` with `NTP=samay1.nic.in samay2.nic.in time.nplindia.org`, installed by a deploy script, and a host check that `timedatectl show -p NTPSynchronized` is `yes`.
3. **Sign-in rows without the client address.** `LOGIN` success and failure rows record the account, not the address it came from (only `LOGIN_ABUSE` rows name addresses); for those requests the address is in the web access log, so item 1 decides how long it can be recovered. Adding it to the row is personal data in the audit trail and needs the privacy owner's agreement.
4. **CERT-In point of contact** for the operator and each client, and the alternates in incident runbook §2.
5. **DPDP breach notice** workflow (above).
