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
#   0 2 * * *  /bin/bash -c 'set -a; . /etc/erp/erp.env; set +a; /opt/erp/deploy/backup.sh'
#
# Install that cron line with deploy/install_backup_cron.sh rather than by
# hand -- it also verifies the environment file and does a first run.
#
# `set -a` is not optional and this comment used to get it wrong. /etc/erp/erp.env
# holds bare KEY=value lines with no `export` (it has to -- systemd reads the same
# file via EnvironmentFile=, which rejects an `export ` prefix), so a plain
# `source erp.env && backup.sh` leaves DATABASE_URL as a *shell* variable that this
# script -- a child process -- never sees, and the job dies on the `: "${DATABASE_URL:?...}"`
# check below. The old form sat in this header for months and would have failed
# every night had anyone pasted it into a crontab; see docs/operations/restore_drill_log.md
# (2026-08-07).
#
# Restore a whole-database backup:
#   openssl enc -d -aes-256-cbc -pbkdf2 -in FILE.dump.enc -pass pass:"$BACKUP_ENCRYPTION_KEY" \
#     | pg_restore -d "$DATABASE_URL" --clean --if-exists --no-owner
#
# ---------------------------------------------------------------------------
# Tenant-scoped export (26.1.6)
# ---------------------------------------------------------------------------
# Every tenant's data lives in its own `tenant_*` schema, so a single tenant
# can be exported (and restored) without touching anyone else's rows:
#
#   TENANT_SCHEMA=tenant_acme /opt/erp/deploy/backup.sh
#   # or:  /opt/erp/deploy/backup.sh --tenant tenant_acme
#
# This is deliberately the SAME script rather than a parallel one: the
# encryption, checksum sidecar and retention rules must not drift between a
# whole-DB backup and a per-tenant export, and a second script is exactly how
# that drift starts.
#
# The nightly cron above stays whole-database. Per-tenant exports are
# on-demand (an offboarding tenant asking for their data, a support copy
# before a risky data fix) -- there is no separate per-tenant schedule,
# because a whole-DB dump already contains every tenant, so a nightly
# per-tenant run would only duplicate it.
#
# Restore a tenant export -- into a scratch database first, never straight
# over a live schema:
#   openssl enc -d -aes-256-cbc -pbkdf2 -in FILE.dump.enc -pass pass:"$BACKUP_ENCRYPTION_KEY" \
#     | pg_restore -d "$SCRATCH_DATABASE_URL" --no-owner
set -euo pipefail

TENANT_SCHEMA="${TENANT_SCHEMA:-}"
while [ $# -gt 0 ]; do
  case "$1" in
    --tenant)
      TENANT_SCHEMA="${2:?--tenant needs a schema name, e.g. tenant_acme}"
      shift 2
      ;;
    --tenant=*)
      TENANT_SCHEMA="${1#--tenant=}"
      shift
      ;;
    *)
      echo "unknown argument: $1" >&2
      echo "usage: backup.sh [--tenant <schema>]" >&2
      exit 2
      ;;
  esac
done

: "${DATABASE_URL:?set DATABASE_URL (source /etc/erp/erp.env)}"
: "${BACKUP_ENCRYPTION_KEY:?set BACKUP_ENCRYPTION_KEY in /etc/erp/erp.env}"

BACKUP_DIR="${BACKUP_DIR:-/opt/erp/backups}"
RETAIN_DAYS="${RETAIN_DAYS:-14}"
mkdir -p "$BACKUP_DIR"

ts="$(date -u +%Y%m%dT%H%M%SZ)"

if [ -n "$TENANT_SCHEMA" ]; then
  # Guard the schema name before it reaches pg_dump. Two separate reasons:
  # a typo would otherwise produce a silently EMPTY dump (pg_dump does not
  # fail on a schema that matches nothing), and restricting the pattern keeps
  # anything shell-special out of the argument entirely.
  case "$TENANT_SCHEMA" in
    tenant_[a-zA-Z0-9_]*) ;;
    *)
      echo "refusing to export '$TENANT_SCHEMA': tenant schemas are named tenant_<something>" >&2
      exit 2
      ;;
  esac
  exists="$(psql "$DATABASE_URL" -tAc "SELECT 1 FROM information_schema.schemata WHERE schema_name = '$TENANT_SCHEMA'")"
  if [ "$exists" != "1" ]; then
    echo "refusing to export '$TENANT_SCHEMA': no such schema in this database" >&2
    exit 2
  fi
  dump="$BACKUP_DIR/${TENANT_SCHEMA}_$ts.dump"
else
  dump="$BACKUP_DIR/custom_erp_$ts.dump"
fi
enc="$dump.enc"

if [ -n "$TENANT_SCHEMA" ]; then
  pg_dump "$DATABASE_URL" --schema="$TENANT_SCHEMA" -F c -f "$dump"
else
  pg_dump "$DATABASE_URL" -F c -f "$dump"
fi
openssl enc -aes-256-cbc -pbkdf2 -salt -in "$dump" -out "$enc" -pass pass:"$BACKUP_ENCRYPTION_KEY"
rm -f "$dump"
sha256sum "$enc" > "$enc.sha256"

# Retention: drop encrypted backups (and their sidecars) older than
# RETAIN_DAYS. Scoped by filename prefix so a one-off tenant export never
# ages out the nightly whole-database backups, and vice versa.
if [ -n "$TENANT_SCHEMA" ]; then
  prune_glob="${TENANT_SCHEMA}_*"
else
  prune_glob='custom_erp_*'
fi
find "$BACKUP_DIR" -name "$prune_glob.enc" -mtime +"$RETAIN_DAYS" -delete
find "$BACKUP_DIR" -name "$prune_glob.enc.sha256" -mtime +"$RETAIN_DAYS" -delete

echo "backup complete: $enc"
