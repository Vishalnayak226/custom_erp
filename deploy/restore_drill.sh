#!/usr/bin/env bash
# Prove that the newest encrypted backup actually restores (Stage 32.3).
#
# The Linux counterpart of manage.ps1's Invoke-RestoreDrill, which only ever
# ran on the Windows dev machine -- so the production droplet had no way to
# check its own backups at all. An untested backup is not a backup; this
# restores into a THROWAWAY database and compares row counts against the live
# one, then drops the scratch database again.
#
#   sudo -u erp /bin/bash -c 'source /etc/erp/erp.env && /opt/erp/deploy/restore_drill.sh'
#
# It never writes to the live database. The only destructive act is dropping
# its own scratch database, whose name it also creates.
set -euo pipefail

: "${DATABASE_URL:?set DATABASE_URL (source /etc/erp/erp.env)}"
: "${BACKUP_ENCRYPTION_KEY:?set BACKUP_ENCRYPTION_KEY in /etc/erp/erp.env}"

BACKUP_DIR="${BACKUP_DIR:-/opt/erp/backups}"
DRILL_DB="${DRILL_DB:-erp_restore_drill_$(date -u +%Y%m%d%H%M%S)}"

fail() { echo "DRILL FAILED: $*" >&2; exit 1; }

# The newest whole-database backup. Tenant exports (tenant_*.enc, see
# backup.sh --tenant) are deliberately excluded: they contain one schema, so
# row counts would never match the live database and the drill would "fail"
# on a perfectly good file.
newest="$(find "$BACKUP_DIR" -name 'custom_erp_*.dump.enc' -type f -printf '%T@ %p\n' 2>/dev/null | sort -rn | head -1 | cut -d' ' -f2-)"
[ -n "$newest" ] || fail "no whole-database backups found in $BACKUP_DIR - has the nightly cron ever run? (deploy/install_backup_cron.sh)"
echo "Newest backup: $newest"

# Checksum first, before spending time on a restore. A corrupted file found
# here is exactly the outcome this drill exists to surface early - the same
# check manage.ps1 added in Stage 25.8 (DR-0212).
if [ -f "$newest.sha256" ]; then
  echo "Verifying checksum..."
  (cd "$(dirname "$newest")" && sha256sum -c "$(basename "$newest").sha256") || fail "checksum mismatch - the backup file is corrupted."
else
  echo "WARNING: no .sha256 sidecar for this backup; skipping checksum verification."
fi

# Server-level connection able to CREATE/DROP the scratch database.
#
# This used to be derived from DATABASE_URL by swapping the database name,
# which assumes the application's own role may create databases. On a
# correctly hardened box it may not: production's `erp` role has
# rolcreatedb=false and rolsuper=false (verified 2026-08-07, Stage 26.11.3),
# so the drill died on "permission denied to create database" before it ever
# reached a restore. Granting the app role CREATEDB to make the drill work
# would weaken the running system to test a backup, which is backwards.
#
# So: point DRILL_ADMIN_URL at a connection that may create databases -
# typically the local superuser over the unix socket, which needs no password
# because pg_hba maps it by peer:
#
#   DRILL_ADMIN_URL='postgresql://postgres@/postgres?host=/var/run/postgresql'
#
# Unset, it falls back to the previous derive-from-DATABASE_URL behaviour,
# which is still correct anywhere the app role does hold CREATEDB.
#
# `[^/]*` rather than `[^/]+` so socket-style URIs, which have an empty host
# section, are rewritten correctly too.
admin_url="${DRILL_ADMIN_URL:-$(printf '%s' "$DATABASE_URL" | sed -E 's#(://[^/]*)/[^?]*#\1/postgres#')}"
# Derived from admin_url, not DATABASE_URL: whoever creates the scratch
# database is also who must restore into it.
drill_url="$(printf '%s' "$admin_url" | sed -E "s#(://[^/]*)/[^?]*#\1/$DRILL_DB#")"

cleanup() {
  echo "Dropping scratch database $DRILL_DB..."
  psql "$admin_url" -q -c "DROP DATABASE IF EXISTS \"$DRILL_DB\"" || true
}
trap cleanup EXIT

echo "Creating scratch database $DRILL_DB..."
# Explicit encoding, and TEMPLATE template0 rather than the default template1:
# a bare CREATE DATABASE inherits whatever template1 is, and a WIN1252 template1
# silently produces a scratch database that cannot hold the UTF8 bytes in the
# dump - the drill would then fail (or worse, mangle) for a reason that has
# nothing to do with the backup being tested. template0 is the one template a
# differing encoding may legally be copied from. LC_* are 'C' because that
# locale exists on every platform; collation order does not affect whether a
# restore succeeds, which is all this drill asserts.
psql "$admin_url" -q -c "CREATE DATABASE \"$DRILL_DB\" WITH TEMPLATE template0 ENCODING 'UTF8' LC_COLLATE 'C' LC_CTYPE 'C'" || fail "could not create the scratch database."

echo "Restoring..."
# --no-owner because the scratch database's roles need not match production's;
# pg_restore reports non-fatal warnings on extensions/comments, so its exit
# code is checked but stderr is kept for the operator to read.
openssl enc -d -aes-256-cbc -pbkdf2 -in "$newest" -pass pass:"$BACKUP_ENCRYPTION_KEY" \
  | pg_restore -d "$drill_url" --no-owner --clean --if-exists \
  || fail "pg_restore reported an error restoring $newest."

# Compare a few high-value tables against live. Row counts drifting by a
# little is expected (the backup is hours old and the app kept writing); the
# failure this catches is a restore that lands EMPTY, which is what a broken
# backup actually looks like.
echo
echo "Comparing row counts (live vs restored):"
ok=1
for tbl in tenant_default.documents tenant_default.users tenant_default.gl_postings; do
  live_n="$(psql "$DATABASE_URL" -tAc "SELECT COUNT(*) FROM $tbl" 2>/dev/null || echo "n/a")"
  drill_n="$(psql "$drill_url" -tAc "SELECT COUNT(*) FROM $tbl" 2>/dev/null || echo "n/a")"
  printf '  %-32s live=%-10s restored=%-10s\n' "$tbl" "$live_n" "$drill_n"
  if [ "$drill_n" = "n/a" ]; then
    echo "    ^ table missing from the restore" >&2
    ok=0
  elif [ "$live_n" != "n/a" ] && [ "$live_n" -gt 0 ] && [ "$drill_n" -eq 0 ]; then
    echo "    ^ restored empty while live has rows" >&2
    ok=0
  fi
done

echo
[ "$ok" -eq 1 ] || fail "the restored database is missing data - this backup would NOT have saved you."
echo "DRILL PASSED: $newest restores cleanly and carries data."
