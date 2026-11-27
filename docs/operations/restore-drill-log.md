# Restore drill log

Append-only history of every restore drill run (manual or via `.\manage.ps1 restore-drill`), oldest first. Each row is written automatically by `Invoke-RestoreDrill` in `manage.ps1` — do not hand-edit past entries, only append. See [`backup-restore.md`](backup-restore.md) for the runbook this log supports.

- 2026-07-19T05:02:30Z (approx.) | backup=custom_erp_20260719T050230Z.dump | target=test/custom_erp_test | duration=~8s | tenants=2 doctype_meta=44 | verifier=manual (pre-automation) | result=PASS
- 2026-07-20T18:04:13Z | backup=custom_erp_20260720T180402Z.dump.enc | target=test/custom_erp_test | duration=3.6s | tenants=2 doctype_meta=62 | verifier=automated (manage.ps1 restore-drill) | result=PASS
- 2026-08-07T03:09:48Z | backup=custom_erp_20260805T020820Z.dump.enc | target=**production droplet** 139.59.17.16, scratch db erp_restore_drill_20260807030948 | rows documents=210/210 users=4/4 gl_postings=0/0 | verifier=deploy/restore_drill.sh | result=PASS
- 2026-11-26T15:29:43Z | backup=custom_erp_20261126T020001Z.dump.enc (116 MB) | target=**production droplet**, scratch db dropped after | duration=25s | rows live/restored documents=18032/16909 users=4/4 gl_postings=403/391 | verifier=deploy/restore_drill.sh + erp-server -verify-restore (commit 9db21d3) | result=PASS
- 2026-11-27T02:44:38Z | backup=point-in-time: base_20261127T024428Z + archived WAL, target minute 08:14:38 IST | target=**local scratch cluster** (Postgres 16.3, ports 5497/5498), S3 stub | rows live/restored drill_rows=50/200 (want 200) | verifier=deploy/pitr.sh drill (item 290.2) | result=PASS

## 2026-08-07 — first drill against the real production box (Stage 26.11.3)

The two entries above ran on the Windows dev machine against a *test* database. This is the first time a production backup has been restored on the production host, which is what 26.11.3 actually asked for. It passed — but only after three things had to be fixed or worked around, all of which meant the drill **could not have been run at all** as previously documented. Recording them here because each one would have been discovered during a real incident otherwise.

**1. `restore_drill.sh` was never deployed.** `/opt/erp/deploy/` on the droplet contained `backup.sh` and `migrate.sh` only. `install_backup_cron.sh` was missing too, and the deployed `backup.sh` was an older 1,527-byte copy against the repo's current 4,541 bytes — `deploy.ps1` ships the binary and `public/`, not the `deploy/` scripts. Copied up by hand for this run.

**2. The app's DB role cannot create databases — the script's documented invocation always fails.** `restore_drill.sh`'s header says to run it as `sudo -u erp` with `DATABASE_URL` from `/etc/erp/erp.env`, and it derived its admin connection from that same URL by swapping the database name. Production's `erp` role has `rolcreatedb=false, rolsuper=false` (correct least-privilege), so the drill died on `ERROR: permission denied to create database` before reaching a restore. Granting `erp` CREATEDB would weaken the running system in order to test a backup, so instead the script now takes an optional **`DRILL_ADMIN_URL`** override for the create/drop/restore connection, falling back to the old derive-from-`DATABASE_URL` behaviour where the app role does hold CREATEDB. Its two `sed` patterns also changed `[^/]+` → `[^/]*` so socket-style URIs (empty host section) rewrite correctly.

**3. The superuser cannot read the backup directory.** With `DRILL_ADMIN_URL` pointing at the local superuser over the unix socket (the only way in: `pg_hba` gives `postgres` peer auth on `local` and scram over TCP, and no TCP password for `postgres` exists), the drill then found no backups — `/opt/erp` is `drwxr-x--- erp:erp`, which the `postgres` user cannot traverse. Worked around by staging a copy of the newest backup plus its `.sha256` into `/var/lib/postgresql/drill_stage` and pointing `BACKUP_DIR` at it. The sidecar also needed its **absolute** path rewritten to a bare filename, because `sha256sum -c` resolves the path recorded inside the file, not the one it was handed — so a staged copy fails its own checksum with a misleading "the backup file is corrupted".

### The invocation that actually works today

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
  bash /tmp/restore_drill.sh

rm -rf "$STAGE"
```

The staging dance in steps 1–4 is a workaround, not a design. The proper fix is for the script to run as `root` (which can read both `/etc/erp/erp.env` and `/opt/erp/backups`) and shell out to `sudo -u postgres psql` for the create/drop/restore, which would remove the copy, the `chown` and the sidecar rewrite entirely. Left as a follow-up rather than done here, because it is a larger change to a DR script than the drill itself warranted and the drill needed to happen first.

### Result

`DRILL PASSED` — checksum verified, `pg_restore` clean, and all three sampled tables matched live exactly (`documents` 210/210, `users` 4/4, `gl_postings` 0/0). The scratch database was dropped by the script's own `trap`, the staging directory removed, and production confirmed healthy afterwards (`systemctl is-active erp` → `active`, `/api/v1/health` → 200). The live database was never written to.

### ⚠️ The bigger finding: nightly backups are not running

The newest backup on the box was **2026-08-05**, two days before this drill, and `/opt/erp/backups/` holds exactly two files — one from 2026-08-03 owned by `erp`, one from 2026-08-05 owned by `root`. Both look like manual one-offs taken around deploys, not scheduled runs. `crontab -l` is empty for root, `/etc/cron.d/` carries only `e2scrub_all` and `sysstat`, and there is no systemd timer for backups. **`install_backup_cron.sh` has never been run on this droplet.**

This matters more than anything else in this log. A backup that restores is only useful if a recent one exists, and `hypercare-plan.md`'s rollback trigger "two consecutive failed nightly backups" cannot fire when there is no nightly backup to fail. Installing the cron is a production configuration change and is left for explicit sign-off rather than done as part of a read-only drill — see `micro_checklist.md` 26.11.3.

> **RESOLVED 2026-08-07T03:36Z — user signed off, cron installed and verified.** Details in the section below.

## 2026-08-07 — nightly backup cron installed on production

Sign-off given the same day the drill reported the gap. `install_backup_cron.sh` has now been run on the droplet and the nightly job is live: `0 2 * * *` as user `erp`, 14-day on-box retention, logging to `/var/log/erp-backup.log`.

Shipping the scripts was a prerequisite, not a detail — the drill had already found `/opt/erp/deploy/` was missing `install_backup_cron.sh` entirely and carried a stale 1,527-byte `backup.sh` (repo: 4,541). Both were copied up, the old `backup.sh` preserved as `backup.sh.pre20260807.bak`. The stale copy's retention step was also quietly wrong — `find "$BACKUP_DIR" -name '*.enc' -mtime +14 -delete`, unscoped, so it would have aged out on-demand tenant exports along with the nightly dumps; the current version prunes by filename prefix instead.

### Two bugs the install found — both of which would have failed silently at 02:00

**1. The cron line could never have worked as written.** The line documented in `backup.sh`'s header comment since day one, and reproduced by `install_backup_cron.sh`, was `source /etc/erp/erp.env && ... backup.sh`. `/etc/erp/erp.env` holds bare `KEY=value` lines with **no `export`** — and it has to, because systemd loads the same file via `EnvironmentFile=`, which rejects an `export ` prefix. So `source` sets `DATABASE_URL` as a *shell* variable only; `backup.sh` runs as a **child process**, never inherits it, and dies on `set DATABASE_URL (source /etc/erp/erp.env)`. Fixed in `install_backup_cron.sh` by loading the env with `set -a; . file; set +a`, which marks everything sourced for export without touching the file systemd depends on.

This is the entire justification for the script's step 5 existing. Had the header comment been pasted into a crontab by hand — the documented procedure until today — the job would have failed every single night, into a log nobody reads, and the failure would have surfaced only when someone needed a backup that did not exist.

**2. The installer failed under its own documented `sudo` invocation.** Run as root from `/root` (mode 0700), the step-5 verification drops to `erp`, which cannot traverse that cwd: `pg_dump` warns `could not change directory to "/root"` and the retention `find` exits non-zero with `Failed to restore initial working directory`, failing the whole run under `set -e` **even though the dump itself succeeded**. Cron is unaffected (it starts jobs in the user's home, `/opt/erp`), so this was purely an artifact of how the installer is invoked — but it made a working install look broken. Fixed with a `cd /` at the top of the script.

### Verification — the cron line existing is still not evidence

The installer's own immediate run proves `backup.sh` works, but not that *cron* can run it, which is a different claim: `erp`'s shell is `/usr/sbin/nologin` and cron's `PATH` is only `/usr/bin:/bin`, far narrower than the `sudo` environment the manual run inherits. So a temporary `* * * * *` entry was installed alongside the real one, pointed at a scratch `BACKUP_DIR` so the real backup set stayed clean, and left to fire on its own:

```
Aug  7 03:38:01 CRON[131799]: (erp) CMD (/bin/bash -c 'set -a; . /etc/erp/erp.env; set +a; ... backup.sh' ...)
```

It produced a valid backup. The `nologin` shell is no obstacle (cron uses `/bin/sh`, not the passwd shell), and `pg_dump`/`openssl`/`sha256sum`/`find` all resolve under cron's `PATH`. The temporary entry and its scratch directory were removed; `crontab -u erp -l` now shows the single managed line.

The newest backup was then verified end-to-end rather than merely counted: `sha256sum -c` → `OK`, and decrypting it with the key from `erp.env` and piping to `pg_restore --list` yields a valid archive header (`dbname: custom_erp`, 286 TOC entries, 297 archive entries). That last check matters because it is the only one that proves `BACKUP_ENCRYPTION_KEY` on the box actually decrypts what the box is writing — a checksum only proves the file is intact, not that it is recoverable.

### Resting state

- `crontab -u erp -l` → one line, `0 2 * * *`, marker-tagged.
- `/opt/erp/backups` `mode=700 erp:erp`; `/var/log/erp-backup.log` `mode=640 erp:erp`.
- Backups present: the two pre-existing manual dumps (2026-08-03, 2026-08-05) plus two from today's install runs. Retention will age all of them out on schedule.
- `systemctl is-active erp` → `active`, `caddy` → `active`, `/api/v1/health` → 200. Nothing was restarted; no application config was touched.

### Still open

**Nothing alerts if the nightly backup fails.** The job appends to `/var/log/erp-backup.log` and that is all — `hypercare-plan.md`'s "two consecutive failed nightly backups" trigger still depends on a human looking. Note that an *empty* log is a distinct failure from an *error* in the log: a successful run appends one line per night, so no new line means the job never ran. Wiring this into the 20.2 alert webhook is the real fix and is not done. Off-box copies remain deliberately absent (no storage credentials) — losing the droplet still loses the backups with it.

## 2026-11-26 — drill with the restored-copy check (item 267.6, ask 278)

Run on the owner's "approved all", by Claude over ssh as root. The invocation above still holds, with two additions: the server binary is staged too (`/opt/erp` is not readable by `postgres`, and `ERP_BIN` must point at a copy), and `DRILL_EVIDENCE_FILE` points into the stage, then the evidence is copied back with `install -o erp -g erp -m 0640` to `/opt/erp/backups/restore-drill-evidence.json`, where the Deploy click reads it. The first attempt, as `sudo -u erp`, failed on `permission denied to create database`, as recorded on 2026-08-07; the app role was left without CREATEDB.

### Result

`DRILL PASSED`, 25 s from decrypt to a checked copy. `erp-server -verify-restore` on the restored copy:

- migrations: every file applied, checksums match;
- default: the ledger balances (29,397,565.00 each side); 1,757 signed audit rows and 751 checkpoints verify with the server's key; the stock-ledger reconciliation shows the same difference the live Reconciliation screen shows;
- minn: the ledger balances (12,682,056.00); 1,744 signed rows and 62 checkpoints verify; all 22 reconciliations tie.

The row counts differ from live because the backup is from 02:00 UTC and the drill ran at 15:29 UTC: live held 1,123 documents and 12 GL rows more.

- **RTO** for the database on the host: 25 s for 116 MB.
- **RPO:** up to 24 h (one backup a night at 02:00 UTC).

### Cleanup and findings

- The scratch database was dropped by the script, the staging folder removed, and `erp` stayed active (`/api/v1/health` 200). The live database was never written to.
- `-verify-restore` creates the key files it expects when they are missing, so running it as `postgres` left three throwaway keys in `/var/lib/postgresql/.config/custom_erp/`. They were removed after checking their timestamps matched the drill.
- **No off-box copy.** No `OFFSITE_S3_*` is set, so the backups and the key that opens them sit only on the droplet. This waits on the owner's storage account.
- **Key files outside the backup:** `audit_archive_key.local` and `web_push_key.local` live on the host only. A copy belongs with the off-box backups.

## 2026-11-27 — point-in-time recovery drill (item 290.2, ask 305)

Run locally by Claude before point-in-time recovery reaches any server. Two scratch Postgres 16.3 clusters on a Windows laptop (Git Bash), an S3 stub for the off-box copy, and `deploy/pitr.sh` exactly as shipped. Production was not touched; PITR there waits on the owner running `install_pitr.sh`.

### Result

- **Base backup:** sealed, sized, checksummed and copied off-box; one `backup_runs` row each (`scope=pitr`, `Success`, offsite `Success`).
- **The minute back:** 200 rows were written, the target minute noted, then 150 deleted (live: 50). `pitr.sh restore --to "<minute> +05:30"` stopped "before commit of transaction 735" and the copy on port 5498 held 200 rows, max id 200.
- **Archive retry:** a segment sent twice with the same content is accepted (0); with different content it is refused (1), so a bad copy never overwrites a good one.
- **Off-box sync:** two syncs in a row copy each file once (4 objects, then 4; manifest 4 lines).
- **Refusals:** a non-empty target folder and a port already in use are both refused before anything starts.
- **Prune:** with two kept, a third base backup removed the oldest and the segments only it needed.
- **Status:** archiving on, segments counted, window and off-box count shown.
- `go test -run TestPITRAlerts ./engines/`: the four alerts fire as designed.

### Finding fixed during the drill

The first run failed to restore: "the base backup would not open". `pg_basebackup` wrote the tar to standard output, and on Windows that output is text mode, so every newline inside `backup_label` became CR LF and the tar broke. `pitr.sh base` now writes into a private work folder (`-D <folder> -Ft -z`), seals `base.tar.gz` from there and removes the folder. A cluster with tablespaces is refused with a plain message. Linux was never affected, but the folder form is the same on both.

- **RPO with PITR on:** up to five minutes on the host (`archive_timeout = 5min` from `install_pitr.sh`); the drill used one minute to run faster.
- **RTO** for the drill database: under a minute from `restore` to a running copy; on the host it grows with the base backup size and the WAL to replay since it.
