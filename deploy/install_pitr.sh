#!/usr/bin/env bash
# Turn on point-in-time recovery (item 290.2) on a box whose Postgres runs on
# the same machine. Run as root:
#
#   sudo bash /opt/erp/deploy/install_pitr.sh            turn it on, take the first base backup
#   sudo bash /opt/erp/deploy/install_pitr.sh --refresh  copy the current pitr.sh, key and cron lines
#   sudo bash /opt/erp/deploy/install_pitr.sh --off      turn archiving off (keeps what is archived)
#
# Turning it on restarts Postgres once if archiving was off: a few seconds in
# which the app reconnects; run it in a quiet hour. --refresh never restarts
# anything and does nothing until PITR is on; every deploy runs it
# (remote_deploy.sh), so a new pitr.sh or a rotated backup key reaches the box.
#
# What it sets up (docs/operations/backup-restore.md, "Point-in-time recovery"):
#   /usr/local/lib/erp-pitr/pitr.sh   the script Postgres calls (outside /opt/erp,
#                                     whose deploys replace files while Postgres runs)
#   /etc/erp/pitr.env                 key, bucket and folders for the postgres user (0440)
#   /var/lib/erp-pitr/{wal,base}      the sealed archive and base backups (postgres, 0700)
#   /var/log/erp-pitr.log             what the two cron lines print
#   postgres crontab                  sync every 5 minutes, base backup Sundays 01:30
set -euo pipefail
cd /

MODE="${1:-install}"
ERP_ENV_FILE="${ERP_ENV_FILE:-/etc/erp/erp.env}"
SOURCE_SCRIPT="${PITR_SOURCE:-$(cd "$(dirname "$0")" && pwd)/pitr.sh}"
LIB_DIR=/usr/local/lib/erp-pitr
SCRIPT="$LIB_DIR/pitr.sh"
PITR_ENV=/etc/erp/pitr.env
DATA_ROOT="${PITR_DATA_ROOT:-/var/lib/erp-pitr}"
LOG=/var/log/erp-pitr.log
PG_OS_USER=postgres
CRON_MARKER="# erp-pitr (managed by deploy/install_pitr.sh)"
SYNC_LINE="*/5 * * * * $SCRIPT sync >> $LOG 2>&1 $CRON_MARKER"
# Sunday 01:30: before the 02:00 nightly dump, so the two never race.
BASE_LINE="${PITR_BASE_SCHEDULE:-30 1 * * 0} $SCRIPT base >> $LOG 2>&1 $CRON_MARKER"

fail() { echo "ERROR: $*" >&2; exit 1; }
[ "$(id -u)" = "0" ] || fail "run as root"
id "$PG_OS_USER" >/dev/null 2>&1 || fail "no '$PG_OS_USER' user - point-in-time recovery needs Postgres on this box"
pg() { sudo -u "$PG_OS_USER" psql -X -A -t -q -v ON_ERROR_STOP=1 -d postgres -c "$1"; }

copy_script() {
  [ -f "$SOURCE_SCRIPT" ] || fail "$SOURCE_SCRIPT not found - is the repo deployed?"
  install -d -m 0755 -o root -g root "$LIB_DIR"
  install -m 0750 -o root -g "$PG_OS_USER" "$SOURCE_SCRIPT" "$SCRIPT.new"
  mv -f "$SCRIPT.new" "$SCRIPT"
}

# The postgres user cannot read /etc/erp/erp.env (the erp user's), so the few
# values pitr.sh needs are copied into a file only root and postgres can read.
write_env() {
  set +u; set -a; . "$ERP_ENV_FILE"; set +a; set -u
  [ -n "${BACKUP_ENCRYPTION_KEY:-}" ] || fail "BACKUP_ENCRYPTION_KEY is not set in $ERP_ENV_FILE - the archive must not be written unsealed"
  local v tmp="$PITR_ENV.new"
  ( umask 0277
    {
      echo "# written by deploy/install_pitr.sh from $ERP_ENV_FILE - re-run it with --refresh after changing that file"
      for v in BACKUP_ENCRYPTION_KEY DATABASE_URL OPS_ALERT_WEBHOOK_URL OFFSITE_S3_ENDPOINT OFFSITE_S3_BUCKET \
               OFFSITE_S3_REGION OFFSITE_S3_ACCESS_KEY_ID OFFSITE_S3_SECRET_ACCESS_KEY OFFSITE_S3_PREFIX; do
        if [ -n "${!v:-}" ]; then printf '%s=%q\n' "$v" "${!v}"; fi
      done
      printf '%s=%q\n' WAL_ARCHIVE_DIR "$DATA_ROOT/wal" PITR_BASE_DIR "$DATA_ROOT/base" \
        PITR_BASE_KEEP "${PITR_BASE_KEEP:-2}" PG_BIN "$PG_BIN"
    } > "$tmp" )
  chown root:"$PG_OS_USER" "$tmp"
  chmod 0440 "$tmp"
  mv -f "$tmp" "$PITR_ENV"
}

write_cron() {
  local existing filtered
  existing="$(crontab -u "$PG_OS_USER" -l 2>/dev/null || true)"
  filtered="$(printf '%s\n' "$existing" | grep -v -F "$CRON_MARKER" || true)"
  if [ -n "${1:-}" ]; then
    printf '%s\n%s\n%s\n' "$filtered" "$SYNC_LINE" "$BASE_LINE" | sed '/^$/d' | crontab -u "$PG_OS_USER" -
  else
    printf '%s\n' "$filtered" | sed '/^$/d' | crontab -u "$PG_OS_USER" -
  fi
}

# The server's own binaries (pg_basebackup, pg_archivecleanup, pg_ctl), of the
# server's major version: Debian/Ubuntu keep them in /usr/lib/postgresql/<N>/bin.
find_pg_bin() {
  local num major
  num="$(pg "SHOW server_version_num")" || fail "cannot reach Postgres as the '$PG_OS_USER' user"
  [ "$num" -ge 130000 ] || fail "Postgres $num is older than 13; point-in-time recovery here needs 13 or newer"
  major=$(( num / 10000 ))
  PG_BIN="/usr/lib/postgresql/$major/bin"
  if [ ! -x "$PG_BIN/pg_basebackup" ]; then PG_BIN="$(dirname "$(command -v pg_basebackup 2>/dev/null || echo /missing/pg_basebackup)")"; fi
  local b
  for b in pg_basebackup pg_archivecleanup pg_ctl pg_isready; do
    [ -x "$PG_BIN/$b" ] || fail "$b not found in $PG_BIN - install the postgresql-$major package's binaries"
  done
}

case "$MODE" in
  --refresh)
    # Off (no pitr.env) stays off: a deploy never turns archiving on.
    [ -f "$PITR_ENV" ] || { echo "PITR-NOT-ON"; exit 0; }
    find_pg_bin
    copy_script
    write_env
    write_cron on
    echo "PITR-REFRESHED"
    exit 0
    ;;
  --off)
    pg "ALTER SYSTEM SET archive_mode = 'off'"
    pg "ALTER SYSTEM RESET archive_command"
    pg "ALTER SYSTEM RESET archive_timeout"
    systemctl restart postgresql
    write_cron ""
    rm -f "$PITR_ENV"
    echo "Archiving is off. $DATA_ROOT is kept: delete it by hand once no restore will need it."
    echo "PITR-OFF"
    exit 0
    ;;
  install) ;;
  *) fail "usage: install_pitr.sh [--refresh|--off]" ;;
esac

# 1. Preflight.
[ -f "$ERP_ENV_FILE" ] || fail "$ERP_ENV_FILE not found"
for b in openssl gzip curl crontab; do command -v "$b" >/dev/null 2>&1 || fail "$b is not on PATH"; done
find_pg_bin

# 2. Folders, script, key, log.
install -d -m 0700 -o "$PG_OS_USER" -g "$PG_OS_USER" "$DATA_ROOT" "$DATA_ROOT/wal" "$DATA_ROOT/base"
touch "$LOG"; chown "$PG_OS_USER":"$PG_OS_USER" "$LOG"; chmod 640 "$LOG"
copy_script
write_env

# 3. Settings. wal_level and max_wal_senders are raised only if they are below
# what a base backup needs (they are already high enough by default).
[ "$(pg "SHOW wal_level")" != minimal ] || pg "ALTER SYSTEM SET wal_level = 'replica'"
[ "$(pg "SHOW max_wal_senders")" != 0 ] || pg "ALTER SYSTEM SET max_wal_senders = 3"
pg "ALTER SYSTEM SET archive_mode = 'on'"
pg "ALTER SYSTEM SET archive_command = '$SCRIPT archive \"%p\" \"%f\"'"
# A quiet hour still closes a segment every 5 minutes, so at most 5 minutes of
# changes are only on the server's own disk.
pg "ALTER SYSTEM SET archive_timeout = '5min'"
pg "SELECT pg_reload_conf()" >/dev/null
sleep 2
pending="$(pg "SELECT string_agg(name, ', ') FROM pg_settings WHERE pending_restart")"
if [ -n "$pending" ]; then
  echo "Restarting Postgres for: $pending"
  systemctl restart postgresql
  for _ in $(seq 1 30); do sudo -u "$PG_OS_USER" "$PG_BIN/pg_isready" -q && break; sleep 1; done
fi
[ "$(pg "SHOW archive_mode")" = on ] || fail "archive_mode is still off after the restart - check: journalctl -u postgresql"

# 4. Cron.
write_cron on
echo "Installed for '$PG_OS_USER':"
crontab -u "$PG_OS_USER" -l | grep -F "$CRON_MARKER"

# 5. Prove it now: close a segment and wait for it in the archive, then take
# the first base backup. A cron line existing is not evidence (install_backup_cron.sh).
echo
echo "Archiving one segment to check ..."
pg "SELECT txid_current()" >/dev/null
seg="$(pg "SELECT pg_walfile_name(pg_current_wal_lsn())")"
pg "SELECT pg_switch_wal()" >/dev/null
archived=""
for _ in $(seq 1 60); do
  if [ -f "$DATA_ROOT/wal/$seg.gz.enc" ]; then archived=yes; break; fi
  sleep 1
done
if [ -z "$archived" ]; then
  pg "SELECT 'last failed: ' || COALESCE(last_failed_wal, '-') || ' at ' || COALESCE(last_failed_time::text, '-') FROM pg_stat_archiver" >&2 || true
  fail "$seg did not reach $DATA_ROOT/wal within 60s - see the Postgres log (journalctl -u postgresql)"
fi
echo "Archived $seg."
echo
echo "Taking the first base backup ..."
sudo -u "$PG_OS_USER" "$SCRIPT" base
echo
sudo -u "$PG_OS_USER" "$SCRIPT" status
echo
echo "PITR-ON - restore with: sudo -u $PG_OS_USER $SCRIPT restore --to \"<time with zone>\" --into <new folder>"
