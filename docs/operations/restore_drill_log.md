# Restore drill log

Append-only history of every restore drill run (manual or via `.\manage.ps1 restore-drill`), oldest first. Each row is written automatically by `Invoke-RestoreDrill` in `manage.ps1` — do not hand-edit past entries, only append. See [`backup_restore.md`](backup_restore.md) for the runbook this log supports.

- 2026-07-19T05:02:30Z (approx.) | backup=custom_erp_20260719T050230Z.dump | target=test/custom_erp_test | duration=~8s | tenants=2 doctype_meta=44 | verifier=manual (pre-automation) | result=PASS
- 2026-07-20T18:04:13Z | backup=custom_erp_20260720T180402Z.dump.enc | target=test/custom_erp_test | duration=3.6s | tenants=2 doctype_meta=62 | verifier=automated (manage.ps1 restore-drill) | result=PASS
