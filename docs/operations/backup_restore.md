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

Everything above is the Windows dev/test/live stack. The production droplet is a different machine with different tooling, and until **2026-08-07** it had **no scheduled backup at all** — `deploy/backup.sh` documented its own cron line in a header comment that nobody ever ran, which is exactly how `/opt/erp/backups` was found empty on 2026-08-04. Writing `install_backup_cron.sh` on 2026-08-05 did not change that; the script existed in the repo but was never run on the box, and was not even shipped to it (`deploy.ps1` sends the binary and `public/`, not `deploy/`). **The nightly cron was finally installed and verified on 2026-08-07** — see the run notes below.

Install it with the script, not by hand:

```bash
sudo bash /opt/erp/deploy/install_backup_cron.sh
```

It is idempotent (a marker comment identifies its own crontab line, so re-running replaces that line and never touches other jobs), it preflights the things that actually fail — missing `/etc/erp/erp.env`, missing `DATABASE_URL` or `BACKUP_ENCRYPTION_KEY`, missing `pg_dump`/`openssl`, missing `erp` user — and it **runs one backup immediately**, because a cron line existing is not evidence that a backup works.

**Retention is 14 days, on-box only** (decision taken 2026-08-05). Override with `RETAIN_DAYS=` if that changes. There is deliberately no off-box copy: it would need storage credentials that do not exist yet. Note what that means — losing the droplet loses the backups with it.

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
