#!/usr/bin/env bash
# Nightly encrypted backup of the self-hosted ERP database -- the Linux
# counterpart of manage.ps1's Backup-Databases. pg_dump custom format, then
# AES-256 via openssl, with a sha256 sidecar and age-based retention.
#
# NOTE: the encryption format differs from manage.ps1's .NET AES, so these
# backups restore on Linux (via openssl below), not by the Windows manage.ps1.
# That's fine -- prod backups are restored on prod.
#
# Cron (as the erp user):
#   0 2 * * *  /bin/bash -c 'source /etc/erp/erp.env && /opt/erp/deploy/backup.sh'
#
# Restore a backup:
#   openssl enc -d -aes-256-cbc -pbkdf2 -in FILE.dump.enc -pass pass:"$BACKUP_ENCRYPTION_KEY" \
#     | pg_restore -d "$DATABASE_URL" --clean --if-exists --no-owner
set -euo pipefail

: "${DATABASE_URL:?set DATABASE_URL (source /etc/erp/erp.env)}"
: "${BACKUP_ENCRYPTION_KEY:?set BACKUP_ENCRYPTION_KEY in /etc/erp/erp.env}"

BACKUP_DIR="${BACKUP_DIR:-/opt/erp/backups}"
RETAIN_DAYS="${RETAIN_DAYS:-14}"
mkdir -p "$BACKUP_DIR"

ts="$(date -u +%Y%m%dT%H%M%SZ)"
dump="$BACKUP_DIR/custom_erp_$ts.dump"
enc="$dump.enc"

pg_dump "$DATABASE_URL" -F c -f "$dump"
openssl enc -aes-256-cbc -pbkdf2 -salt -in "$dump" -out "$enc" -pass pass:"$BACKUP_ENCRYPTION_KEY"
rm -f "$dump"
sha256sum "$enc" > "$enc.sha256"

# Retention: drop encrypted backups (and their sidecars) older than RETAIN_DAYS.
find "$BACKUP_DIR" -name '*.enc' -mtime +"$RETAIN_DAYS" -delete
find "$BACKUP_DIR" -name '*.enc.sha256' -mtime +"$RETAIN_DAYS" -delete

echo "backup complete: $enc"
