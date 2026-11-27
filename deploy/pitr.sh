#!/usr/bin/env bash
# Point-in-time recovery (item 290.2): put the database back as it was at any
# minute of the last week or two, not only at last night's 02:00 dump.
#
# Postgres hands every finished WAL segment (its log of each change) to this
# script, which keeps it compressed and sealed with the backup key. Once a week
# a base backup (a copy of the whole cluster) is taken. A restore unpacks the
# newest base backup older than the chosen minute into a NEW folder and replays
# the archived segments up to that minute, on its own port. The live database
# is never touched; you take what you need from the copy.
#
#   pitr.sh archive <%p> <%f>   Postgres's archive_command: seal one segment into the archive
#   pitr.sh fetch <%f> <%p>     a restore's restore_command: open one segment from the archive
#   pitr.sh base                take a base backup, prune what no kept base backup needs (weekly cron)
#   pitr.sh sync                copy new archive files off-box, if a bucket is set (cron, every 5 minutes)
#   pitr.sh restore --to "<time with zone>" --into <new folder> [--port 5499] [--from <folder>] [--base <file>]
#   pitr.sh status              what is archived, the oldest minute you can go back to, what is off-box
#
# deploy/install_pitr.sh (run once as root) turns archiving on, copies this
# script to /usr/local/lib/erp-pitr, writes /etc/erp/pitr.env for the postgres
# user and installs the two cron lines. Everything here runs as postgres.
# docs/operations/backup-restore.md has the procedure.
#
# Sealed exactly like backup.sh's dumps (openssl AES-256-CBC, PBKDF2, salted,
# BACKUP_ENCRYPTION_KEY via the environment, never the command line): one key
# opens every backup. Nothing is ever written to disk unsealed.
set -euo pipefail
umask 077

ENV_FILE="${PITR_ENV_FILE:-/etc/erp/pitr.env}"
if [ -r "$ENV_FILE" ]; then set -a; . "$ENV_FILE"; set +a; fi
WAL_ARCHIVE_DIR="${WAL_ARCHIVE_DIR:-/var/lib/erp-pitr/wal}"
PITR_BASE_DIR="${PITR_BASE_DIR:-/var/lib/erp-pitr/base}"
PITR_BASE_KEEP="${PITR_BASE_KEEP:-2}"
if [ -n "${PG_BIN:-}" ]; then PATH="$PG_BIN:$PATH"; fi
SELF="$(cd "$(dirname "$0")" && pwd)/$(basename "$0")"

die() { echo "pitr: $*" >&2; exit 1; }
need_key() {
  [ -n "${BACKUP_ENCRYPTION_KEY:-}" ] || die "BACKUP_ENCRYPTION_KEY is not set (is $ENV_FILE readable?)"
  export BACKUP_ENCRYPTION_KEY
}
seal() { openssl enc -aes-256-cbc -pbkdf2 -salt -pass env:BACKUP_ENCRYPTION_KEY; }
unseal() { openssl enc -d -aes-256-cbc -pbkdf2 -pass env:BACKUP_ENCRYPTION_KEY -in "$1" | gunzip -c; }

# Same webhook and payload as backup.sh's alert_ops; keep the two in step.
alert_ops() {
  local severity="$1" message="$2" host text escaped
  echo "[$severity] $message" >&2
  [ -n "${OPS_ALERT_WEBHOOK_URL:-}" ] || return 0
  host="$(hostname -s 2>/dev/null || echo unknown-host)"
  text=":rotating_light: [$severity] pitr.sh on $host: $message"
  escaped="$(printf '%s' "$text" | sed -e 's/\\/\\\\/g' -e 's/"/\\"/g' | tr -d '\000-\037')"
  curl -fsS -m 10 -X POST -H 'Content-Type: application/json' \
    --data "{\"text\":\"$escaped\"}" "$OPS_ALERT_WEBHOOK_URL" >/dev/null 2>&1 \
    || echo "[WARN] pitr alert webhook delivery failed" >&2
  return 0
}
# Postgres retries a failed archive every minute and cron runs sync every five,
# so one fault would page hundreds of times: the webhook goes once an hour per
# kind of fault (every try still lands in the log), and a success re-arms it.
alert_hourly() {
  local stamp="$WAL_ARCHIVE_DIR/.alert-$1"
  if [ -n "$(find "$stamp" -mmin -60 2>/dev/null)" ]; then echo "[$1] $2" >&2; return 0; fi
  touch "$stamp" 2>/dev/null || true
  alert_ops "$1" "$2"
}
rearm() { if [ -e "$WAL_ARCHIVE_DIR/.alert-$1" ]; then rm -f -- "$WAL_ARCHIVE_DIR/.alert-$1"; fi; }

# One public.backup_runs row per base backup, scope 'pitr' - the same table
# backup.sh writes, so the server's backup monitor sees it (engines/alerting.go).
started_ms="$(date +%s%3N)"
OFFSITE_KEY=""
OFFSITE_OUTCOME=""
record_run() {
  local outcome="$1" file_name="${2:-}" size_bytes="${3:-}" sha="${4:-}" detail="${5:-}" duration_ms
  [ -n "${DATABASE_URL:-}" ] || return 0
  duration_ms=$(( $(date +%s%3N) - started_ms ))
  # The database URL goes last (Windows psql ignores options after it).
  psql -X -q -v ON_ERROR_STOP=1 \
      -v outcome="$outcome" -v file_name="$file_name" -v size_bytes="$size_bytes" -v duration_ms="$duration_ms" \
      -v sha="$sha" -v detail="$detail" -v host="$(hostname -s 2>/dev/null || echo unknown-host)" \
      -v started_ms="$started_ms" -v offsite_key="$OFFSITE_KEY" -v offsite_outcome="$OFFSITE_OUTCOME" \
      "$DATABASE_URL" >/dev/null 2>&1 <<'SQL' || alert_ops BACKUP_RECORD_FAILED "base backup ran ($1) but its row could not be written to public.backup_runs"
INSERT INTO public.backup_runs
  (scope, outcome, file_name, size_bytes, duration_ms, sha256, detail, host, started_at, finished_at,
   offsite_key, offsite_outcome)
VALUES
  ('pitr', :'outcome', NULLIF(:'file_name', ''), NULLIF(:'size_bytes', '')::bigint, :'duration_ms'::bigint,
   NULLIF(:'sha', ''), NULLIF(:'detail', ''), :'host', to_timestamp(:'started_ms'::bigint / 1000.0), now(),
   NULLIF(:'offsite_key', ''), NULLIF(:'offsite_outcome', ''));
SQL
  return 0
}

# Off-box, the same bucket and signing as backup.sh (OFFSITE_S3_*), under
# <prefix>/pitr/wal/ and <prefix>/pitr/base/. Keep in step with backup.sh.
offsite_enabled() {
  [ -n "${OFFSITE_S3_ENDPOINT:-}" ] && [ -n "${OFFSITE_S3_BUCKET:-}" ] \
    && [ -n "${OFFSITE_S3_ACCESS_KEY_ID:-}" ] && [ -n "${OFFSITE_S3_SECRET_ACCESS_KEY:-}" ]
}
offsite_prefix() { printf '%s' "${OFFSITE_S3_PREFIX:-$(hostname -s 2>/dev/null || echo erp)}"; }
offsite_put() { # LOCAL_FILE OBJECT_KEY
  printf 'user = "%s:%s"\n' "$OFFSITE_S3_ACCESS_KEY_ID" "$OFFSITE_S3_SECRET_ACCESS_KEY" \
    | curl -fsS -m 900 --retry 3 --retry-delay 5 -K - \
        --aws-sigv4 "aws:amz:${OFFSITE_S3_REGION:-auto}:s3" \
        -H "x-amz-content-sha256: UNSIGNED-PAYLOAD" \
        -T "$1" "${OFFSITE_S3_ENDPOINT%/}/$OFFSITE_S3_BUCKET/$2" >/dev/null
}

# --- archive ------------------------------------------------------------------
# Postgres keeps a segment until this answers 0, so an answer of 0 must mean the
# sealed copy is on disk. A segment archived before (a retry after a crash) is
# accepted only if its content is the same; anything else stops archiving and
# alerts, as the Postgres manual asks.
cmd_archive() {
  local src="$1" name="$2" dest tmp
  need_key
  dest="$WAL_ARCHIVE_DIR/$name.gz.enc"
  if [ -e "$dest" ]; then
    if unseal "$dest" 2>/dev/null | cmp -s - "$src"; then return 0; fi
    alert_hourly BACKUP_WAL_ARCHIVE_FAILED "$name is already archived with different content - archiving has stopped and the database's log folder is growing; see backup-restore.md"
    return 1
  fi
  tmp="$WAL_ARCHIVE_DIR/.$name.$$.tmp"
  if gzip -c -- "$src" | seal > "$tmp" && { sync -- "$tmp" 2>/dev/null || true; } && mv -f -- "$tmp" "$dest"; then
    sync -- "$WAL_ARCHIVE_DIR" 2>/dev/null || true
    rearm BACKUP_WAL_ARCHIVE_FAILED
    return 0
  fi
  rm -f -- "$tmp"
  alert_hourly BACKUP_WAL_ARCHIVE_FAILED "could not archive $name (disk full? key missing?) - archiving has stopped and the database's log folder is growing; see backup-restore.md"
  return 1
}

# --- fetch --------------------------------------------------------------------
# A missing segment answers 1: that is how a restore learns it reached the end
# of the archive. A segment that will not open answers 1 too; Postgres then
# stops with "recovery ended before configured recovery target was reached"
# rather than handing back a copy short of the minute asked for.
cmd_fetch() {
  local name="$1" dest="$2" src
  src="${PITR_FETCH_DIR:-$WAL_ARCHIVE_DIR}/$name.gz.enc"
  [ -f "$src" ] || return 1
  need_key
  if unseal "$src" > "$dest.pitr-tmp" && mv -f -- "$dest.pitr-tmp" "$dest"; then return 0; fi
  rm -f -- "$dest.pitr-tmp"
  echo "pitr: $src would not open (wrong key or damaged)" >&2
  return 1
}

# --- sync ---------------------------------------------------------------------
# Upload-only, like backup.sh's media copy: each file goes up once and its key
# is written to .offsite_sent. The bucket's lifecycle rule ages objects out.
cmd_sync() {
  offsite_enabled || return 0
  local lock="$WAL_ARCHIVE_DIR/.sync.lock" rc=0
  if ! mkdir "$lock" 2>/dev/null; then
    # A run killed part-way leaves its lock; one older than an hour is stale.
    [ -n "$(find "$lock" -maxdepth 0 -mmin +60 2>/dev/null)" ] || return 0
    rmdir "$lock" 2>/dev/null || true
    mkdir "$lock" 2>/dev/null || return 0
  fi
  sync_unlocked || rc=$?
  rmdir "$lock" 2>/dev/null || true
  return "$rc"
}
sync_unlocked() {
  local sent="$WAL_ARCHIVE_DIR/.offsite_sent" list prefix n=0 failed=0 key path
  touch "$sent"
  prefix="$(offsite_prefix)"
  list="$(mktemp "$WAL_ARCHIVE_DIR/.sync.XXXXXX")"
  { for path in "$WAL_ARCHIVE_DIR"/*.gz.enc; do [ -f "$path" ] && printf '%s/pitr/wal/%s\t%s\n' "$prefix" "${path##*/}" "$path"; done
    for path in "$PITR_BASE_DIR"/base_*.tar.gz.enc; do [ -f "$path" ] && printf '%s/pitr/base/%s\t%s\n' "$prefix" "${path##*/}" "$path"; done
  } > "$list"
  while IFS=$'\t' read -r key path; do
    if offsite_put "$path" "$key"; then printf '%s\n' "$key" >> "$sent"; n=$((n + 1)); else failed=$((failed + 1)); fi
  # FILENAME, not NR == FNR: the first file can be empty (nothing sent yet).
  done < <(awk -F'\t' 'FILENAME == ARGV[1] { s[$0] = 1; next } !($1 in s)' "$sent" "$list")
  # Keys of files pruned here since are dropped, so the list stays short.
  awk -F'\t' 'FILENAME == ARGV[1] { k[$1] = 1; next } ($0 in k)' "$list" "$sent" > "$sent.new" && mv -f -- "$sent.new" "$sent"
  rm -f -- "$list"
  if [ "$failed" -gt 0 ]; then
    alert_hourly BACKUP_PITR_OFFSITE_FAILED "$failed archive file(s) could not be copied off-box ($n copied) - will retry every 5 minutes"
    return 1
  fi
  rearm BACKUP_PITR_OFFSITE_FAILED
  [ "$n" -eq 0 ] || echo "$(date -u +%FT%TZ) sync: $n file(s) copied off-box"
  return 0
}

# --- base ---------------------------------------------------------------------
# pg_basebackup writes one gzipped tar (the cluster plus the WAL it needs) into
# a work folder, which is sealed and then deleted. Not through stdout: on
# Windows stdout is text mode and the tar comes out with \r\n inside it (found
# by the 290.2 drill), and one path everywhere is the path the drill proves.
# The work folder is 0700 postgres like the data folder it copies, and a
# leftover one from a crash is removed at the next run. The file name
# carries the start time and the first WAL segment it needs:
#   base_<UTC yyyymmddThhmmssZ>_<segment>.tar.gz.enc
# After a good one, only the newest PITR_BASE_KEEP are kept, and archived
# segments older than the oldest kept one's first segment are removed.
cmd_base() {
  local stamp work tmp log lsn seg final size sha detail=""
  need_key
  mkdir -p "$PITR_BASE_DIR"
  rm -rf -- "$PITR_BASE_DIR"/.base_*.d
  stamp="$(date -u +%Y%m%dT%H%M%SZ)"
  work="$PITR_BASE_DIR/.base_$stamp.d"
  tmp="$PITR_BASE_DIR/.base_$stamp.tmp"
  log="$PITR_BASE_DIR/.base_$stamp.log"
  if ! pg_basebackup -D "$work" -Ft -z -X fetch -c fast -v 2> "$log"; then
    detail="pg_basebackup failed: $( { grep -v '^pg_basebackup: \(initiating\|checkpoint\|write-ahead log\|waiting\|base backup\|syncing\)' "$log" || true; } | tail -n 1 | cut -c1-200)"
  elif [ ! -f "$work/base.tar.gz" ] || [ "$(ls -1 "$work" | grep -c '\.tar\.gz$')" -ne 1 ]; then
    detail="the cluster has tablespaces; point-in-time recovery covers the main data folder only"
  elif ! seal < "$work/base.tar.gz" > "$tmp"; then
    detail="the base backup could not be sealed"
  fi
  rm -rf -- "$work"
  if [ -n "$detail" ]; then
    rm -f -- "$tmp" "$log"
    record_run Failed "" "" "" "$detail"
    alert_ops BACKUP_PITR_BASE_FAILED "$detail"
    return 1
  fi
  lsn="$(sed -n 's/.*write-ahead log start point: \([0-9A-Fa-f]*\/[0-9A-Fa-f]*\).*/\1/p' "$log" | head -n 1)"
  rm -f -- "$log"
  seg=""
  [ -n "$lsn" ] && seg="$(psql -X -A -t -d postgres -c "SELECT pg_walfile_name('$lsn')" 2>/dev/null | tr -d '[:space:]')" || true
  if [ -z "$seg" ]; then
    rm -f -- "$tmp"
    record_run Failed "" "" "" "base backup made but its start segment could not be read"
    alert_ops BACKUP_PITR_BASE_FAILED "base backup made but its start segment could not be read"
    return 1
  fi
  final="$PITR_BASE_DIR/base_${stamp}_${seg}.tar.gz.enc"
  mv -f -- "$tmp" "$final"
  size="$(wc -c < "$final" | tr -d ' ')"
  sha="$(sha256sum "$final" | cut -d' ' -f1)"
  echo "$(date -u +%FT%TZ) base: ${final##*/} ($size bytes)"

  # Keep the newest PITR_BASE_KEEP, then drop the segments none of them needs.
  local all keep oldest
  mapfile -t all < <(ls -1 "$PITR_BASE_DIR"/base_*.tar.gz.enc 2>/dev/null | sort)
  keep="$PITR_BASE_KEEP"; [ "$keep" -ge 1 ] 2>/dev/null || keep=2
  if [ "${#all[@]}" -gt "$keep" ]; then
    for old in "${all[@]:0:${#all[@]}-keep}"; do rm -f -- "$old"; echo "base: removed ${old##*/}"; done
  fi
  oldest="$(ls -1 "$PITR_BASE_DIR"/base_*.tar.gz.enc | sort | head -n 1)"
  oldest="${oldest##*_}"; oldest="${oldest%%.*}"
  if [ "${#oldest}" -eq 24 ]; then
    pg_archivecleanup -x .gz.enc "$WAL_ARCHIVE_DIR" "$oldest" || echo "pitr: pg_archivecleanup failed - old segments stay until the next base backup" >&2
  fi

  if offsite_enabled; then
    OFFSITE_KEY="$(offsite_prefix)/pitr/base/${final##*/}"
    if cmd_sync; then OFFSITE_OUTCOME=Success; else OFFSITE_OUTCOME=Failed; fi
  fi
  record_run Success "${final##*/}" "$size" "$sha"
}

# --- restore ------------------------------------------------------------------
cmd_restore() {
  local to="" into="" port=5499 from="" base="" wal_dir base_dir target target_stamp b bstamp conf
  while [ $# -gt 0 ]; do
    case "$1" in
      --to) to="$2"; shift 2 ;;
      --into) into="$2"; shift 2 ;;
      --port) port="$2"; shift 2 ;;
      --from) from="$2"; shift 2 ;;
      --base) base="$2"; shift 2 ;;
      *) die "unknown option $1 (see the top of this file)" ;;
    esac
  done
  [ -n "$to" ] && [ -n "$into" ] || die 'restore needs --to "<time with zone, e.g. 2026-11-27 14:05 +05:30>" and --into <new folder>'
  need_key
  # Without a zone the time would be read in this machine's zone (UTC on the
  # server) - give it the way you read clocks, e.g. +05:30.
  target="$(date -u -d "$to" '+%Y-%m-%d %H:%M:%S+00')" || die "could not read the time \"$to\""
  target_stamp="$(date -u -d "$to" +%Y%m%dT%H%M%SZ)"
  if [ -n "$from" ]; then wal_dir="$from/wal"; base_dir="$from/base"; else wal_dir="$WAL_ARCHIVE_DIR"; base_dir="$PITR_BASE_DIR"; fi
  [ -d "$wal_dir" ] || die "no archive folder $wal_dir"
  if [ -z "$base" ]; then
    # The newest base backup that started before the minute asked for.
    local bases
    mapfile -t bases < <(ls -1 "$base_dir"/base_*.tar.gz.enc 2>/dev/null | sort -r)
    for b in "${bases[@]}"; do
      bstamp="${b##*/base_}"; bstamp="${bstamp%%_*}"
      if [[ "$bstamp" < "$target_stamp" ]]; then base="$b"; break; fi
    done
    [ -n "$base" ] || die "no base backup older than $target - the oldest minute you can go back to is in: $SELF status"
  fi
  [ -f "$base" ] || die "no base backup $base"
  if [ -e "$into" ] && [ -n "$(ls -A "$into" 2>/dev/null)" ]; then die "$into is not empty - give a new folder"; fi
  if pg_isready -q -h localhost -p "$port" 2>/dev/null; then die "something already answers on port $port - give another --port"; fi
  mkdir -p "$into"; chmod 700 "$into"
  into="$(cd "$into" && pwd)"
  echo "Restoring to $target (UTC) from ${base##*/} into $into ..."
  unseal "$base" | tar -xf - -C "$into" || die "the base backup would not open (wrong key or damaged)"
  # A Debian/Ubuntu cluster keeps its settings outside the data folder, so the
  # copy has none: a minimal set lets it start on its own, local only.
  if [ ! -f "$into/postgresql.conf" ]; then
    printf '%s\n' "# written by pitr.sh restore" > "$into/postgresql.conf"
    printf '%s\n' "local all all peer" "host all all 127.0.0.1/32 scram-sha-256" "host all all ::1/128 scram-sha-256" > "$into/pg_hba.conf"
    : > "$into/pg_ident.conf"
  fi
  rm -f -- "$into/postmaster.pid" "$into/standby.signal"
  conf="$into/postgresql.auto.conf"
  {
    echo ""
    echo "# pitr.sh restore $(date -u +%FT%TZ): a copy of the database at $target, never the live one"
    echo "port = $port"
    echo "listen_addresses = 'localhost'"
    echo "archive_mode = 'off'"
    echo "archive_command = ''"
    echo "restore_command = '\"${PITR_BASH:-$(command -v bash)}\" \"$SELF\" fetch \"%f\" \"%p\"'"
    echo "recovery_target_time = '$target'"
    echo "recovery_target_action = 'promote'"
  } >> "$conf"
  : > "$into/recovery.signal"
  export PITR_FETCH_DIR="$wal_dir"
  # A minute inside the base backup's own run (its first few minutes) cannot be
  # reached from it: Postgres stops with "before consistent recovery point".
  local hint="if its log says the stop point is before the consistent recovery point, run again with --base <the older base backup>"
  pg_ctl -D "$into" -l "$into/pitr_restore.log" -w -t 3600 start >/dev/null || {
    tail -n 5 "$into/pitr_restore.log" >&2; die "the restored copy did not start - its log is $into/pitr_restore.log; $hint"; }
  local i state=""
  for i in $(seq 1 720); do
    state="$(psql -X -A -t -h localhost -p "$port" -d postgres -c 'SELECT pg_is_in_recovery()' 2>/dev/null | tr -d '[:space:]')" || state=""
    [ "$state" = f ] && break
    if ! pg_ctl -D "$into" status >/dev/null 2>&1; then
      tail -n 5 "$into/pitr_restore.log" >&2; die "the restored copy stopped - its log is $into/pitr_restore.log; $hint"
    fi
    sleep 5
  done
  [ "$state" = f ] || die "the restored copy is still replaying after an hour - its log is $into/pitr_restore.log"
  echo ""
  grep -h "recovery stopping\|last completed transaction" "$into/pitr_restore.log" | sed 's/^.*\(recovery stopping\|last completed\)/  \1/' || true
  cat <<EOF

The copy at $target is running on port $port (local only, archiving off).
  Look:        $(command -v psql) -h localhost -p $port -d <database>
  Take a part: $(command -v pg_dump) -h localhost -p $port -d <database> -n <tenant schema> -Fc -f <file>.dump
               then load it as backup-restore.md "Point-in-time recovery" says
  Stop it:     $(command -v pg_ctl) -D $into stop    (then delete $into)
EOF
}

# --- status -------------------------------------------------------------------
cmd_status() {
  local bases first last
  echo "Archiving:  $(psql -X -A -t -d postgres -c "SELECT 'mode ' || current_setting('archive_mode') || ', every ' || current_setting('archive_timeout')" 2>/dev/null || echo 'database not reachable')"
  psql -X -A -t -F' ' -d postgres -c "SELECT 'Archived:   ' || archived_count || ' segments, last ' || COALESCE(last_archived_wal, '-') || ' at ' || COALESCE(to_char(last_archived_time AT TIME ZONE 'UTC', 'YYYY-MM-DD HH24:MI:SS \"UTC\"'), '-'), CASE WHEN last_failed_time IS NOT NULL AND (last_archived_time IS NULL OR last_failed_time > last_archived_time) THEN E'\nFAILING:    ' || failed_count || ' failures, last on ' || COALESCE(last_failed_wal, '-') || ' at ' || to_char(last_failed_time AT TIME ZONE 'UTC', 'YYYY-MM-DD HH24:MI:SS \"UTC\"') ELSE '' END FROM pg_stat_archiver" 2>/dev/null || true
  echo "Archive:    $WAL_ARCHIVE_DIR - $(find "$WAL_ARCHIVE_DIR" -maxdepth 1 -name '*.gz.enc' 2>/dev/null | wc -l | tr -d ' ') files, $(du -sh "$WAL_ARCHIVE_DIR" 2>/dev/null | cut -f1)"
  mapfile -t bases < <(ls -1 "$PITR_BASE_DIR"/base_*.tar.gz.enc 2>/dev/null | sort)
  echo "Base:       ${#bases[@]} kept (newest $PITR_BASE_KEEP)"
  for b in "${bases[@]}"; do echo "            ${b##*/}  $(du -h "$b" | cut -f1)"; done
  if [ "${#bases[@]}" -gt 0 ]; then
    first="${bases[0]##*/base_}"; first="${first%%_*}"
    echo "Window:     from $(date -u -d "$(echo "$first" | sed 's/\(....\)\(..\)\(..\)T\(..\)\(..\)\(..\)Z/\1-\2-\3 \4:\5:\6Z/')" '+%Y-%m-%d %H:%M UTC') to the last archived segment"
  else
    echo "Window:     none yet - no base backup"
  fi
  if offsite_enabled; then
    echo "Off-box:    $(wc -l < "$WAL_ARCHIVE_DIR/.offsite_sent" 2>/dev/null | tr -d ' ' || echo 0) files copied to $(offsite_prefix)/pitr/"
  else
    echo "Off-box:    no bucket set (OFFSITE_S3_*)"
  fi
}

cmd="${1:-}"; [ $# -gt 0 ] && shift
case "$cmd" in
  archive) [ $# -eq 2 ] || die "archive <%p> <%f>"; cmd_archive "$1" "$2" ;;
  fetch)   [ $# -eq 2 ] || die "fetch <%f> <%p>"; cmd_fetch "$1" "$2" ;;
  sync)    cmd_sync ;;
  base)    cmd_base ;;
  restore) cmd_restore "$@" ;;
  status)  cmd_status ;;
  *) die "usage: pitr.sh archive|fetch|sync|base|restore|status (see the top of this file)" ;;
esac
