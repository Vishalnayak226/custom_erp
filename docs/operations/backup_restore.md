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

## Latest verified restore drill

See [`restore_drill_log.md`](restore_drill_log.md) for the full append-only history. Most recent entry as of this writing:

- Date: 2026-07-20
- Backup: `custom_erp_20260720T180402Z.dump.enc` (dev, encrypted)
- Target: `custom_erp_test`
- Result: PASS in 3.6s; `public.tenants` (2) and `tenant_default.doctype_meta` (62) read back correctly after decrypt + restore
- Verifier: automated (`manage.ps1 restore-drill`)
