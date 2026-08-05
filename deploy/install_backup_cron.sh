#!/usr/bin/env bash
# Install the nightly backup cron entry on a self-hosted ERP box (Stage 32.3).
#
# Why this exists: deploy/backup.sh has always documented its own cron line in
# a header comment, and on 2026-08-04 the production droplet was found with an
# EMPTY /opt/erp/backups and an empty crontab -- i.e. no backups at all,
# because that comment was never actually acted on. A comment is not an
# install step; this script is.
#
# Run it once per box, as root or via sudo:
#
#   sudo bash /opt/erp/deploy/install_backup_cron.sh
#
# Idempotent: re-running replaces the ERP backup line rather than appending a
# second one, so it is safe to include in a deploy or to run again after
# changing RETAIN_DAYS.
set -euo pipefail

ERP_USER="${ERP_USER:-erp}"
ERP_ENV_FILE="${ERP_ENV_FILE:-/etc/erp/erp.env}"
BACKUP_SCRIPT="${BACKUP_SCRIPT:-/opt/erp/deploy/backup.sh}"
BACKUP_DIR="${BACKUP_DIR:-/opt/erp/backups}"
# 14 days on-box, decided 2026-08-05. Two full weeks covers "we noticed on
# Monday that Friday's data is wrong" without needing off-box storage or new
# credentials; see docs/operations/backup_restore.md.
RETAIN_DAYS="${RETAIN_DAYS:-14}"
CRON_SCHEDULE="${CRON_SCHEDULE:-0 2 * * *}"

# A marker comment is what makes this idempotent -- it identifies our line in
# a crontab that may also hold entries this script knows nothing about, so
# re-running never clobbers someone else's job.
CRON_MARKER="# erp-nightly-backup (managed by deploy/install_backup_cron.sh)"
CRON_LINE="$CRON_SCHEDULE /bin/bash -c 'source $ERP_ENV_FILE && RETAIN_DAYS=$RETAIN_DAYS BACKUP_DIR=$BACKUP_DIR $BACKUP_SCRIPT' >> /var/log/erp-backup.log 2>&1 $CRON_MARKER"

fail() { echo "ERROR: $*" >&2; exit 1; }

# 1. Preflight. Every one of these was a real failure mode on the droplet:
# the env file is where DATABASE_URL and the encryption key live, and without
# the key backup.sh exits non-zero from cron every night, silently.
[ -f "$ERP_ENV_FILE" ] || fail "$ERP_ENV_FILE not found - backup.sh needs DATABASE_URL and BACKUP_ENCRYPTION_KEY from it."
[ -f "$BACKUP_SCRIPT" ] || fail "$BACKUP_SCRIPT not found - is the repo deployed to /opt/erp?"
id "$ERP_USER" >/dev/null 2>&1 || fail "user '$ERP_USER' does not exist."
command -v pg_dump >/dev/null 2>&1 || fail "pg_dump is not on PATH - install the postgresql-client package."
command -v openssl >/dev/null 2>&1 || fail "openssl is not on PATH."

# shellcheck disable=SC1090
set +u; . "$ERP_ENV_FILE"; set -u
[ -n "${DATABASE_URL:-}" ] || fail "DATABASE_URL is not set in $ERP_ENV_FILE."
[ -n "${BACKUP_ENCRYPTION_KEY:-}" ] || fail "BACKUP_ENCRYPTION_KEY is not set in $ERP_ENV_FILE - backups must not be written unencrypted."

# 2. Backup directory, owned by the user cron will run as.
mkdir -p "$BACKUP_DIR"
chown "$ERP_USER":"$ERP_USER" "$BACKUP_DIR"
chmod 700 "$BACKUP_DIR"

# 3. The log file cron appends to, likewise owned by that user.
touch /var/log/erp-backup.log
chown "$ERP_USER":"$ERP_USER" /var/log/erp-backup.log
chmod 640 /var/log/erp-backup.log

# 4. Install the cron line, replacing any previous copy of ours.
existing="$(crontab -u "$ERP_USER" -l 2>/dev/null || true)"
filtered="$(printf '%s\n' "$existing" | grep -v -F "$CRON_MARKER" || true)"
printf '%s\n%s\n' "$filtered" "$CRON_LINE" | sed '/^$/d' | crontab -u "$ERP_USER" -

echo "Installed nightly backup for user '$ERP_USER':"
crontab -u "$ERP_USER" -l | grep -F "$CRON_MARKER"

# 5. Prove it works NOW rather than discovering at 02:00 that it does not.
# This is the step whose absence let the droplet run with zero backups: the
# cron line existing is not evidence that the backup succeeds.
echo
echo "Running one backup immediately to verify..."
sudo -u "$ERP_USER" /bin/bash -c "source $ERP_ENV_FILE && RETAIN_DAYS=$RETAIN_DAYS BACKUP_DIR=$BACKUP_DIR $BACKUP_SCRIPT"

echo
echo "Backups now present in $BACKUP_DIR:"
ls -lh "$BACKUP_DIR"
echo
echo "Retention: $RETAIN_DAYS days, on-box only."
echo "Next: run deploy/restore_drill.sh to prove a backup actually restores."
