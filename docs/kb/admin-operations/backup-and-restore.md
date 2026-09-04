---
title: Backup & Restore
section: Admin & Operations
order: 10
summary: Back up every environment on a schedule, prove the backups actually restore, and know exactly what to run when you need one back.
audience: admin
last_verified: 2026-09-03
screens: [system-status]
---

# Backup & Restore

A backup nobody has ever restored is a hope, not a plan. Everything on this
page exists to close that gap: encryption at rest, a scheduled monthly drill
that actually restores a real backup into a real database, and one
documented, confirmation-gated command for the day you need it for real.

## What gets backed up, and how

`manage.ps1 backup` creates a PostgreSQL custom-format dump for every
configured environment that currently has a database (`dev`, `test`, `live`),
then encrypts it (AES-256) as a `.dump.enc` file with a SHA-256 sidecar next
to it, under the git-ignored `backups/<environment>/` directory. An
environment with no database yet is reported as a clear skip, not an error.

**Keep backups for at least 30 days**, with a monthly copy stored somewhere
other than the machine that made it — a copy sitting next to the database it
backs up survives a bad deploy but not a lost disk.

### The encryption key

Resolved the same way the session-signing secret is: `BACKUP_ENCRYPTION_KEY`
if you set it (hashed to 32 bytes), otherwise a random 32-byte key is
generated once and persisted outside the repository
(`%USERPROFILE%\.erp-backup-key` on the dev machine) so it is never
accidentally committed. **Back that key file up separately from the backups
themselves** — lose it, and every encrypted backup made under it becomes
unrecoverable. An older, unencrypted `.dump` file made before this key
existed still restores directly; nothing needs migrating.

## Restoring

Restore is intentionally destructive and always scoped to one named
environment:

```powershell
.\manage.ps1 stop -Env test
.\manage.ps1 restore -Env test -File .\backups\test\custom_erp_test_YYYYMMDDTHHMMSSZ.dump.enc
```

The command refuses to run while that environment's server is still
listening, and asks for the exact confirmation text `RESTORE <environment>`
before it touches anything. For a scripted, non-interactive restore, pass
that same confirmation string explicitly rather than trying to bypass it:

```powershell
.\manage.ps1 restore -Env test -File <path> -ConfirmRestore "RESTORE test"
```

Start the environment again only after the command reports success.

## The monthly restore drill

`.\manage.ps1 restore-drill` turns "perform a restore drill monthly" into a
real, scriptable action rather than a note on a calendar: it restores the
**newest dev backup** into `test` (stopping `test` first if it is running),
runs the same sanity checks a manual drill would — real row counts on
`public.tenants` and `tenant_default.doctype_meta` — and appends a pass/fail
line to the drill log, with the failure reason if it did not pass. **It
refuses to ever target `live`.** A failed drill pages the same way a failed
backup does, if alerting is configured (see
[Incident Response & Alerting](incident-response.md)).

### Registering the schedule

`.\manage.ps1 register-schedule` registers two Windows Scheduled Tasks in one
step — a daily backup at 02:00 and a monthly restore drill on the 1st at
03:00. It is idempotent: running it again just replaces the existing task
definitions rather than duplicating them. Verify a task exists with
`schtasks /Query /TN ERP-DailyBackup`, or open `taskschd.msc` and look.

## Exporting a single tenant

Every tenant's data lives in its own Postgres schema, so one tenant can be
exported without ever touching another tenant's rows — for offboarding, for
handing a customer their own data, or for a scoped safety copy before a risky
fix.

```powershell
.\manage.ps1 export-tenant -TenantSchema tenant_acme
```

This refuses a schema name that is not shaped like `tenant_<something>`
**and** one that does not actually exist, rather than silently producing an
empty file for a typo'd name. It is on-demand only — there is no separate
per-tenant schedule, since the nightly whole-database backup already
contains every tenant. Every run (backup, restore, tenant export) is
recorded and shows up on the **System Status** screen.

> [!WARNING]
> **Restore a tenant export into a scratch database, never straight over a
> live schema.** There is no built-in guard stopping you from pointing a
> tenant restore at a schema that is still in active use — treat the
> confirmation prompt on the whole-database restore command as the model to
> follow, not a step this narrower path skips for you.

## The production Linux host is different tooling

Everything above is the Windows dev/test/live stack, driven by `manage.ps1`.
The production host is a separate machine with its own installer
(`deploy/install_backup_cron.sh`) and its own retention policy: **14 days,
kept on that machine only** — there is deliberately no off-host copy today,
because that would need storage credentials that do not exist yet in this
deployment. Losing the host loses its backups with it, which is the
trade-off that decision accepts. If your deployment needs a longer or
off-host retention policy, that is a configuration change to make
deliberately, not something this page can decide for you.

The installer is idempotent and runs one real backup immediately as part of
installing itself — a cron line existing is not evidence that a backup
actually works, so it does not take that on faith.

## Troubleshooting

**A restore is refused with the server still listening.** Stop that
environment first (`.\manage.ps1 stop -Env <env>`) — a restore never runs
against a live connection.

**A restore is refused asking for exact confirmation text.** This is
deliberate, not a bug — type `RESTORE <environment>` exactly, or supply it
via `-ConfirmRestore` for a scripted run.

**An old, unencrypted `.dump` file needs restoring.** It still works
directly through the same restore command; the encryption step only applies
to backups made after the encryption key was introduced.

**You are not sure the last backup is good.** Run the restore drill —
`.\manage.ps1 restore-drill` on the dev/test/live stack — rather than
assuming a file existing on disk means it restores cleanly. That is exactly
the gap the drill exists to close.