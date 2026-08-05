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
powershell.exe -NoProfile -ExecutionPolicy Bypass -File "C:\Users\ABCD\Documents\Antigravity Projects\ERP\manage.ps1" backup
```

Review the task result and the newest `.dump.enc`/`.sha256` sidecars after every run.

## The self-hosted Linux box (production)

Everything above is the Windows dev/test/live stack. The production droplet is a different machine with different tooling, and until 2026-08-05 it had **no scheduled backup at all** — `deploy/backup.sh` documented its own cron line in a header comment that nobody ever ran, which is exactly how `/opt/erp/backups` was found empty on 2026-08-04.

Install it with the script, not by hand:

```bash
sudo bash /opt/erp/deploy/install_backup_cron.sh
```

It is idempotent (a marker comment identifies its own crontab line, so re-running replaces that line and never touches other jobs), it preflights the things that actually fail — missing `/etc/erp/erp.env`, missing `DATABASE_URL` or `BACKUP_ENCRYPTION_KEY`, missing `pg_dump`/`openssl`, missing `erp` user — and it **runs one backup immediately**, because a cron line existing is not evidence that a backup works.

**Retention is 14 days, on-box only** (decision taken 2026-08-05). Override with `RETAIN_DAYS=` if that changes. There is deliberately no off-box copy: it would need storage credentials that do not exist yet. Note what that means — losing the droplet loses the backups with it.

Prove a backup actually restores:

```bash
sudo -u erp /bin/bash -c 'source /etc/erp/erp.env && /opt/erp/deploy/restore_drill.sh'
```

This is the Linux counterpart of `manage.ps1 restore-drill`, which only ever existed on the Windows dev machine. It verifies the sha256 sidecar, restores the newest whole-database backup into a **throwaway** database, compares row counts against live, and drops the scratch database. It never writes to the live database.

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
