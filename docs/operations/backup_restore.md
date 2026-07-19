# Backup and restore runbook

`manage.ps1 backup` creates a PostgreSQL custom-format dump and SHA-256 sidecar for every configured environment whose database currently exists (`dev`, `test`, and `live`). It reports a clear skip for an environment that has not been provisioned yet. Files are written beneath the ignored `backups/<environment>/` directory. Keep backups for at least 30 days, with a monthly copy stored off the machine.

Restore is intentionally destructive and environment-specific:

```powershell
.\manage.ps1 stop -Env test
.\manage.ps1 restore -Env test -File .\backups\test\custom_erp_test_YYYYMMDDTHHMMSSZ.dump
```

The command refuses to restore while the target ERP server is listening and requires the exact confirmation `RESTORE <environment>`. It uses `pg_restore --clean --if-exists --no-owner`; start the environment only after the command reports success.

For a documented, non-interactive restore drill, pass that same exact value explicitly rather than bypassing confirmation:

```powershell
.\manage.ps1 restore -Env test -File .\backups\dev\custom_erp_YYYYMMDDTHHMMSSZ.dump -ConfirmRestore "RESTORE test"
```

For a daily Windows Task Scheduler backup, create a task that runs as the account owning the portable PostgreSQL installation, with **Start in** set to the repository root and this program/script invocation:

```powershell
powershell.exe -NoProfile -ExecutionPolicy Bypass -File "C:\Users\ABCD\Documents\Antigravity Projects\ERP\manage.ps1" backup
```

Schedule it daily outside trading hours. Review the task result and the newest dump/`.sha256` sidecars after every run. Perform a restore drill into `test` at least monthly; record the date, backup filename, duration, verifier, and result in the operational log before treating a backup policy as reliable.

## Latest verified restore drill

- Date: 2026-07-19
- Backup: `custom_erp_20260719T050230Z.dump` (dev)
- Target: newly provisioned `custom_erp_test`; target ERP server was stopped
- Result: success in under 8 seconds; `public.tenants` (2) and `tenant_default.doctype_meta` (44) matched dev after restore
- Verifier: local automated command/runbook verification
