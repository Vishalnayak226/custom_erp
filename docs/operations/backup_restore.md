---
doc_id: DOC-C732315BC0
title: Backup and restore runbook
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

# Backup and restore runbook

`manage.ps1 backup` creates a PostgreSQL custom-format dump for every configured environment whose database currently exists (`dev`, `test`, and `live`), then **encrypts it with AES-256** (`.dump.enc`) and writes a SHA-256 sidecar over the encrypted file. It reports a clear skip for an environment that has not been provisioned yet. Files are written beneath the ignored `backups/<environment>/` directory. Keep backups for at least 30 days, with a monthly copy stored off the machine.

**Encryption key** (Stage 12.2, 2026-07-20): resolved the same way `engines/auth.go` resolves `JWT_SECRET` — `BACKUP_ENCRYPTION_KEY` env var if set (hashed to 32 bytes via SHA-256), otherwise a random 32-byte key is generated once and persisted to `%USERPROFILE%\.erp-backup-key` (outside the repo, never git-tracked). Back that key file up separately from the backups themselves — losing it makes every encrypted backup unrecoverable. Uses .NET's built-in `System.Security.Cryptography` (AES), no new dependency. Older plaintext `.dump` files created before this change still restore directly; nothing needs migrating.

Restore is intentionally destructive and environment-specific:

```powershell
.\manage.ps1 stop -Env test
.\manage.ps1 restore -Env test -File .\backups\test\custom_erp_test_YYYYMMDDTHHMMSSZ.dump.enc
```

The command refuses to restore while the target ERP server is listening and requires the exact confirmation `RESTORE <environment>`. A `.enc` file is transparently decrypted to a temp file, restored via `pg_restore --clean --if-exists --no-owner`, then the temp file is deleted; start the environment only after the command reports success.

For a documented, non-interactive restore, pass that same exact value explicitly rather than bypassing confirmation:

```powershell
.\manage.ps1 restore -Env test -File .\backups\dev\custom_erp_YYYYMMDDTHHMMSSZ.dump.enc -ConfirmRestore "RESTORE test"
```

## Automated monthly restore drill

`.\manage.ps1 restore-drill` (Stage 12.2, 2026-07-20) is the "perform a restore drill monthly" instruction turned into a real, scriptable action: it always restores the **newest dev backup** into `test` (stopping `test`'s ERP server first if it's running), runs the same sanity checks the first manual drill used (`public.tenants` / `tenant_default.doctype_meta` row counts), and appends a result line to [`restore_drill_log.md`](restore_drill_log.md) — pass or fail, with the failure reason if it didn't. It refuses to ever target `live`. On failure it also pings `OPS_ALERT_WEBHOOK_URL` the same way a failed `backup` does.

## Registering the schedule

`.\manage.ps1 register-schedule` registers two Windows Scheduled Tasks via `schtasks.exe` — `ERP-DailyBackup` (daily, 02:00) and `ERP-MonthlyRestoreDrill` (1st of the month, 03:00) — turning the recipe below into one command. Run it once per machine (idempotent — re-running just overwrites the existing task definitions). Verify with `schtasks /Query /TN ERP-DailyBackup` or `taskschd.msc`.

Equivalent manual recipe, if you'd rather create the tasks by hand: a task running as the account owning the portable PostgreSQL installation, with **Start in** set to the repository root and this invocation:

```powershell
powershell.exe -NoProfile -ExecutionPolicy Bypass -File "$env:ERP_REPO_ROOT/manage.ps1" backup
```

Review the task result and the newest `.dump.enc`/`.sha256` sidecars after every run.

## The self-hosted Linux box (production)

Everything above is the Windows dev/test/live stack. The production droplet is a different machine with different tooling, and until **2026-08-07** it had **no scheduled backup at all** — `deploy/backup.sh` documented its own cron line in a header comment that nobody ever ran, which is exactly how `/opt/erp/backups` was found empty on 2026-08-04. Writing `install_backup_cron.sh` on 2026-08-05 did not change that; the script existed in the repo but was never run on the box, and was not even shipped to it (`deploy.ps1` sends the binary and `public/`, not `deploy/`). **The nightly cron was finally installed and verified on 2026-08-07** — see the run notes below.

Install it with the script, not by hand:

```bash
sudo bash /opt/erp/deploy/install_backup_cron.sh
```

It is idempotent (a marker comment identifies its own crontab line, so re-running replaces that line and never touches other jobs), it preflights the things that actually fail — missing `/etc/erp/erp.env`, missing `DATABASE_URL` or `BACKUP_ENCRYPTION_KEY`, missing `pg_dump`/`openssl`, missing `erp` user — and it **runs one backup immediately**, because a cron line existing is not evidence that a backup works.

**Retention is 14 days, on-box only** (decision taken 2026-08-05). Override with `RETAIN_DAYS=` if that changes. Until Stage 75.1 there was no off-box copy, so losing the droplet lost the backups with it.

### Off-box copy (Stage 75.1.4-75.1.6)

`deploy/backup.sh` copies every encrypted dump and its `.sha256` to an S3-compatible bucket (Cloudflare R2 is the plan) as soon as these are set in `/etc/erp/erp.env`; with them unset it behaves exactly as before:

| Variable | Value |
|---|---|
| `OFFSITE_S3_ENDPOINT` | `https://<account-id>.r2.cloudflarestorage.com` |
| `OFFSITE_S3_BUCKET` | the private bucket, e.g. `erp-backups` |
| `OFFSITE_S3_ACCESS_KEY_ID` / `OFFSITE_S3_SECRET_ACCESS_KEY` | an API token scoped to that bucket only |
| `OFFSITE_S3_REGION` | optional, `auto` (R2) by default |
| `OFFSITE_S3_PREFIX` | optional, the host's short name by default |

- **What goes up:** `<prefix>/database/custom_erp_<ts>.dump.enc` (+ `.sha256`) each night; a tenant export goes under `<prefix>/tenant/`. The nightly also copies new or changed files under `/opt/erp/media_store` to `<prefix>/media/…` — upload-only, driven by `media_manifest.tsv` in the backup folder (a file goes up once, again only if its size or time changes; nothing is deleted off-box). Give the bucket a **30-day lifecycle rule** (and an object lock if offered) — that is the off-box retention.
- **No new tool:** curl signs the requests itself (`--aws-sigv4`, curl 7.75+). The key pair is handed to curl on stdin, so it never appears in `ps`.
- **What is recorded:** each run's `public.backup_runs` row carries `offsite_key` and `offsite_outcome` (`Success` / `Failed`, empty when no bucket is set); the media copy is its own row, scope `media`. Needs `db/migrations_stage75_1_offsite_backup.sql`.
- **Alerts:** a failed upload → `BACKUP_OFFSITE_FAILED` (the local backup still completed); no successful off-box copy of a whole-database run for 26 h → `BACKUP_OFFSITE_STALE` (`incident_runbook.md`, trigger 3).
- **Remember `deploy.ps1` does not ship `deploy/`** — copy the new `backup.sh` to `/opt/erp/deploy/` by hand after the release that brings it.
- **Keep the backup key off the droplet too** — a copy of `BACKUP_ENCRYPTION_KEY` in a password manager. The bucket holds encrypted dumps; without the key they cannot be restored.
- **Restore from the bucket alone** (the drill to record below, 75.1.7): download the dump and its `.sha256`, `sha256sum -c`, then decrypt and `pg_restore` into a scratch database exactly as for an on-box file.

### Backup threat model (Stage 49.13.4)

The question is not "did the backup run" but "who could destroy or alter both production and every usable backup". Read 2026-10-26 from `deploy/backup.sh`, the monitors in `engines/alerting.go` and the restore drill.

| Threat | What protects the backups today | Gap (owner's action) |
|---|---|---|
| Disk or droplet lost | nightly encrypted dump; off-box copy to the bucket when `OFFSITE_S3_*` is set; `BACKUP_OFFSITE_STALE` after 26 h | the off-box copy runs only once the bucket variables are set on each host |
| Someone with root on the ERP host | the dumps are encrypted, but the key is in `/etc/erp/erp.env` on the same host, and local files can be deleted | **make the off-box copy immutable**: a bucket lock (retention) rule so no object can be deleted or overwritten for 35 days; with that, a host compromise cannot reach the backups of the last month |
| Backup credential reused to destroy | the bucket token is scoped to one bucket; curl only uploads | the token can still delete; the bucket lock above is what stops it. Database backup and runtime use one role (risk R-07) |
| Restore credential misuse | restores and the drill use a separate admin connection (`DRILL_ADMIN_URL`), never the app role | - |
| Key lost | the dump cannot be restored without `BACKUP_ENCRYPTION_KEY` | keep a copy of `BACKUP_ENCRYPTION_KEY` **and `AUDIT_SIGNING_KEY`** in a password manager, off the host: without the second, a restored audit trail cannot be verified (`-verify-restore` says so) |
| Silent failure | failed run alert; no fresh whole-database run in 36 h alerts (and is a boot finding in production); size, duration and sha256 recorded per run in `public.backup_runs` | set `OPS_ALERT_WEBHOOK_URL`, or alerts stay in the server log |
| Corrupt or tampered dump | sha256 sidecar checked before restore; the restore drill now also proves ledgers balance and audit evidence verifies (`erp-server -verify-restore`, 49.13.5) | run the drill monthly; the last logged drill is 2026-08-07 (see the security scorecard) |
| Retention and legal hold | local dumps are kept `RETAIN_DAYS`; a client under legal hold cannot be purged (`SetTenantLegalHold`) | local pruning does not look at legal hold, and the bucket's own lifecycle rule is the owner's to set; while a hold is on, keep the relevant dumps by hand |

**Minimum safe set** (in order): bucket variables on both hosts → bucket lock 35 days → both keys in a password manager → alert webhook → one timed restore from the bucket alone, recorded below (this also gives the RTO the availability model asks for).

### Why the cron line sources the env file with `set -a`

The installed line is:

```
0 2 * * * /bin/bash -c 'set -a; . /etc/erp/erp.env; set +a; RETAIN_DAYS=14 BACKUP_DIR=/opt/erp/backups /opt/erp/deploy/backup.sh' >> /var/log/erp-backup.log 2>&1
```

`set -a` is load-bearing. `/etc/erp/erp.env` holds bare `KEY=value` lines with no `export`, and it **must** stay that way because systemd reads the same file via `EnvironmentFile=`, which rejects an `export ` prefix — so "just add `export`" would fix cron by breaking `erp.service`. Without `set -a`, `source`ing the file sets `DATABASE_URL` as a *shell* variable, `backup.sh` runs as a **child process** and never inherits it, and the job dies on `set DATABASE_URL (source /etc/erp/erp.env)`. The original cron line in `backup.sh`'s header comment had exactly this bug; it was caught on 2026-08-07 only because the installer runs a real backup instead of trusting the crontab entry. Had the header comment been pasted in by hand as originally intended, the job would have failed silently at 02:00 every night, into a log nobody reads.

### Checking that the nightly backup is actually running

`hypercare_plan.md`'s "two consecutive failed nightly backups" rollback trigger depends on someone looking. Nothing alerts on failure yet (that needs 20.2's webhook), so this is a manual check:

```bash
ssh root@<box> 'ls -lt /opt/erp/backups/custom_erp_*.dump.enc | head -3; tail -20 /var/log/erp-backup.log'
```

A healthy box shows a `custom_erp_<yesterday>T02*.dump.enc` at the top and a matching `backup complete:` line in the log. **An empty or stale log is itself the alarm** — a successful run appends one line per night, so no new line means the job did not run at all, which is a different failure from a run that errored.

**Since Stage 56.9 every run also writes a row to `public.backup_runs`** (scope `database` or `tenant`, outcome `Success`/`Failed`, file name, size, duration, sha256, host). The server's hourly monitor reads the newest successful `database` row — not the backup folder — so it works even where the app and the backup job do not share a disk. Check it directly with:

```bash
psql "$DATABASE_URL" -c "SELECT finished_at, scope, outcome, file_name, size_bytes, duration_ms FROM public.backup_runs ORDER BY finished_at DESC LIMIT 5"
```

A run whose row could not be written (for example, the 56.9 migration not yet applied) still keeps its backup and posts a `BACKUP_RECORD_FAILED` alert; the monitor then sees no fresh row and reports the backup as stale. `deploy.ps1` re-ships `deploy/backup.sh` on every deploy, so the box never runs a pre-56.9 copy that records nothing.

Prove a backup actually restores:

This is the Linux counterpart of `manage.ps1 restore-drill`, which only ever existed on the Windows dev machine. It verifies the sha256 sidecar, restores the newest whole-database backup into a **throwaway** database, compares row counts against live, and drops the scratch database. It never writes to the live database.

The invocation this file used to give — `sudo -u erp /bin/bash -c 'source /etc/erp/erp.env && restore_drill.sh'` — **cannot work on production and never could.** The `erp` role is correctly least-privileged (`rolcreatedb=false`, `rolsuper=false`), so it cannot create the scratch database, and the drill dies on `permission denied to create database` before restoring anything. Granting `erp` CREATEDB would weaken the running system in order to test a backup. Use the `DRILL_ADMIN_URL` override instead, with the newest backup staged where the `postgres` user can read it (`/opt/erp` is `drwxr-x--- erp:erp`, which `postgres` cannot traverse):

```bash
STAGE=/var/lib/postgresql/drill_stage
rm -rf "$STAGE"; mkdir -p "$STAGE"
cp /opt/erp/backups/$(ls -t /opt/erp/backups/custom_erp_*.dump.enc | head -1 | xargs basename)* "$STAGE"/
sed -i -E 's#[^ ]*/([^/ ]+\.dump\.enc)$#\1#' "$STAGE"/*.sha256   # absolute -> bare filename
chown -R postgres:postgres "$STAGE"

set -a; . /etc/erp/erp.env; set +a
sudo -u postgres env \
  DATABASE_URL="$DATABASE_URL" \
  BACKUP_ENCRYPTION_KEY="$BACKUP_ENCRYPTION_KEY" \
  BACKUP_DIR="$STAGE" \
  DRILL_ADMIN_URL='postgresql://postgres@/postgres?host=/var/run/postgresql' \
  bash /opt/erp/deploy/restore_drill.sh

rm -rf "$STAGE"
```

The sidecar rewrite matters: `sha256sum -c` resolves the path recorded *inside* the file, not the one it is handed, so a staged copy fails its own checksum with a misleading "the backup file is corrupted". The staging dance is a workaround, not a design — the proper fix is for the script to run as `root` and shell out to `sudo -u postgres psql` for the create/drop/restore. See `restore_drill_log.md` (2026-08-07) for the full account.

## Exporting a single tenant

Every tenant's data lives in its own `tenant_*` schema, so one tenant can be exported without touching anyone else's rows — for offboarding, for handing a customer their data, or for a scoped copy before a risky data fix.

```bash
TENANT_SCHEMA=tenant_acme /opt/erp/deploy/backup.sh      # or: backup.sh --tenant tenant_acme
```

```powershell
.\manage.ps1 export-tenant -TenantSchema tenant_acme      # Windows/dev, same encryption + sidecar
```

Both refuse a schema name that does not match `tenant_<something>` **and** one that does not actually exist — `pg_dump --schema` does not fail on a schema matching nothing, it silently produces an empty dump, which is a far worse outcome than an error. Retention pruning is scoped by filename prefix, so a one-off tenant export never ages out the nightly whole-database backups, and vice versa.

This is **on-demand only — there is no per-tenant schedule**, because the nightly whole-database backup already contains every tenant; a nightly per-tenant run would only duplicate it. Runs are recorded in `public.ops_run_log` as `tenant_export` and show up on the System Status screen alongside backups and restores.

**Restore a tenant export into a scratch database, never straight over a live schema:**

```bash
openssl enc -d -aes-256-cbc -pbkdf2 -in FILE.dump.enc -pass pass:"$BACKUP_ENCRYPTION_KEY" \
  | pg_restore -d "$SCRATCH_DATABASE_URL" --no-owner
```

## Latest verified restore drill

See [`restore_drill_log.md`](restore_drill_log.md) for the full append-only history. Most recent entry as of this writing:

- Date: 2026-07-20
- Backup: `custom_erp_20260720T180402Z.dump.enc` (dev, encrypted)
- Target: `custom_erp_test`
- Result: PASS in 3.6s; `public.tenants` (2) and `tenant_default.doctype_meta` (62) read back correctly after decrypt + restore
- Verifier: automated (`manage.ps1 restore-drill`)
