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

# ---------------------------------------------------------------------------
# Failure alerting (Stage 43.2, closing 26.11.7's "nothing alerts if the
# nightly backup fails")
# ---------------------------------------------------------------------------
# Until now this script's only output was an appended line in
# /var/log/erp-backup.log, so a failed 02:00 run was invisible until somebody
# thought to look -- and hypercare_plan.md's "two consecutive failed nightly
# backups" rollback trigger cannot fire on a signal nobody receives.
#
# Posts to the same OPS_ALERT_WEBHOOK_URL the Go server uses
# (engines/alerting.go), with the same Slack-compatible {"text": ...} payload,
# so one webhook covers app alerts and backup alerts alike. Unset = silent
# no-op, exactly as SendOpsAlert behaves, so dev and CI need no configuration.
#
# Note this only covers a run that STARTS and then fails. A cron that never
# fires at all cannot report its own absence, which is why the Go server also
# watches the age of the newest run this script records in public.backup_runs
# (engines/alerting.go's StartBackupFreshnessMonitor, Stage 56.9) -- the two halves catch
# different failures and neither replaces the other.
alert_ops() {
  severity="$1"
  message="$2"
  echo "[$severity] $message" >&2
  [ -n "${OPS_ALERT_WEBHOOK_URL:-}" ] || return 0
  host="$(hostname -s 2>/dev/null || echo unknown-host)"
  text=":rotating_light: [$severity] backup.sh on $host: $message"
  # Escape backslashes then double quotes, and drop control characters, so the
  # payload is valid JSON without pulling in jq as a dependency.
  escaped="$(printf '%s' "$text" | sed -e 's/\\/\\\\/g' -e 's/"/\\"/g' | tr -d '\000-\037')"
  curl -fsS -m 10 -X POST -H 'Content-Type: application/json' \
    --data "{\"text\":\"$escaped\"}" "$OPS_ALERT_WEBHOOK_URL" >/dev/null 2>&1 \
    || echo "[WARN] backup alert webhook delivery failed" >&2
  return 0
}

# The ERR trap records *where* it broke; the EXIT trap is what actually sends,
# so a plain `exit 2` from the argument/schema guards below is reported too
# (ERR does not fire for `exit`). Only the first word of the failing command is
# reported -- enough to tell pg_dump from openssl from psql, while keeping
# credential-bearing arguments out of a payload that leaves this machine, the
# same restraint engines/alerting.go applies to stack traces.
failed_stage=""
failed_line=""
trap 'failed_stage="${BASH_COMMAND%% *}"; failed_line="$LINENO"' ERR

# ---------------------------------------------------------------------------
# Run record (Stage 56.9)
# ---------------------------------------------------------------------------
# Every run, successful or failed, writes one row to public.backup_runs. The
# Go server's backup monitor (engines/alerting.go's CheckBackupFreshness)
# reads the newest successful 'database' row instead of looking at this
# script's output folder, which a PaaS app service cannot see. Tenant exports
# are recorded with scope 'tenant' and never count as the nightly (26.1.6).
#
# Best-effort by design: a backup that completed is not turned into a failure
# because its record could not be written. A missing record is still caught,
# because the monitor then sees no fresh row and alerts BACKUP_STALE.
started_ms="$(date +%s%3N)"
# RUN_SCOPE overrides the scope ('media' for the media copy, Stage 75.1.5);
# OFFSITE_KEY / OFFSITE_OUTCOME say where this run's off-box copy went and
# whether it got there (Stage 75.1.4) - both blank when no bucket is set.
RUN_SCOPE=""
OFFSITE_KEY=""
OFFSITE_OUTCOME=""
record_run() {
  local outcome="$1" file_name="${2:-}" size_bytes="${3:-}" sha="${4:-}" detail="${5:-}"
  local scope=database duration_ms
  [ -n "${DATABASE_URL:-}" ] || return 0
  if [ -n "${TENANT_SCHEMA:-}" ]; then scope=tenant; fi
  if [ -n "$RUN_SCOPE" ]; then scope="$RUN_SCOPE"; fi
  duration_ms=$(( $(date +%s%3N) - started_ms ))
  # The database URL goes last: psql and pg_dump built without GNU argument
  # reordering (the Windows builds) ignore every option after it.
  if ! psql -X -q -v ON_ERROR_STOP=1 \
      -v scope="$scope" -v tenant_schema="${TENANT_SCHEMA:-}" -v outcome="$outcome" \
      -v file_name="$file_name" -v size_bytes="$size_bytes" -v duration_ms="$duration_ms" \
      -v sha="$sha" -v detail="$detail" -v host="$(hostname -s 2>/dev/null || echo unknown-host)" \
      -v started_ms="$started_ms" -v offsite_key="$OFFSITE_KEY" -v offsite_outcome="$OFFSITE_OUTCOME" \
      "$DATABASE_URL" >/dev/null 2>&1 <<'SQL'
INSERT INTO public.backup_runs
  (scope, tenant_schema, outcome, file_name, size_bytes, duration_ms, sha256, detail, host, started_at, finished_at,
   offsite_key, offsite_outcome)
VALUES
  (:'scope', NULLIF(:'tenant_schema', ''), :'outcome', NULLIF(:'file_name', ''),
   NULLIF(:'size_bytes', '')::bigint, :'duration_ms'::bigint, NULLIF(:'sha', ''),
   NULLIF(:'detail', ''), :'host', to_timestamp(:'started_ms'::bigint / 1000.0), now(),
   NULLIF(:'offsite_key', ''), NULLIF(:'offsite_outcome', ''));
SQL
  then
    alert_ops BACKUP_RECORD_FAILED "${TENANT_SCHEMA:-whole-database} backup ran ($outcome) but its row could not be written to public.backup_runs -- has the 56.9 migration been applied?"
  fi
  return 0
}

on_exit() {
  rc=$?
  [ "$rc" -eq 0 ] && return 0
  detail="exit code $rc"
  [ -n "$failed_stage" ] && detail="$detail during '$failed_stage' (line $failed_line)"
  record_run Failed "" "" "" "$detail" || true
  alert_ops BACKUP_FAILED "${TENANT_SCHEMA:-whole-database} backup FAILED -- $detail. See /var/log/erp-backup.log" || true
}
trap on_exit EXIT

# ---------------------------------------------------------------------------
# Off-box copy (Stage 75.1.4) - the dumps used to stay on the droplet, so
# losing the droplet lost every backup with it.
# ---------------------------------------------------------------------------
# Set in /etc/erp/erp.env to turn it on (any S3-compatible bucket; Cloudflare
# R2 is the plan - a private bucket, a token scoped to it, a 30-day lifecycle
# rule):
#   OFFSITE_S3_ENDPOINT           e.g. https://<account-id>.r2.cloudflarestorage.com
#   OFFSITE_S3_BUCKET             e.g. erp-backups
#   OFFSITE_S3_ACCESS_KEY_ID
#   OFFSITE_S3_SECRET_ACCESS_KEY
#   OFFSITE_S3_REGION             optional, "auto" (R2) by default
#   OFFSITE_S3_PREFIX             optional, this host's short name by default
# Uploads are signed by curl itself (--aws-sigv4, curl 7.75+) - no new tool.
# The key pair goes to curl on stdin, never on its command line, so it does
# not show in `ps`. Unset = no off-box copy, exactly as before.
offsite_enabled() {
  [ -n "${OFFSITE_S3_ENDPOINT:-}" ] && [ -n "${OFFSITE_S3_BUCKET:-}" ] \
    && [ -n "${OFFSITE_S3_ACCESS_KEY_ID:-}" ] && [ -n "${OFFSITE_S3_SECRET_ACCESS_KEY:-}" ]
}
offsite_prefix() {
  printf '%s' "${OFFSITE_S3_PREFIX:-$(hostname -s 2>/dev/null || echo erp)}"
}
# offsite_put LOCAL_FILE OBJECT_KEY
offsite_put() {
  printf 'user = "%s:%s"\n' "$OFFSITE_S3_ACCESS_KEY_ID" "$OFFSITE_S3_SECRET_ACCESS_KEY" \
    | curl -fsS -m 900 --retry 3 --retry-delay 5 -K - \
        --aws-sigv4 "aws:amz:${OFFSITE_S3_REGION:-auto}:s3" \
        -H "x-amz-content-sha256: UNSIGNED-PAYLOAD" \
        -T "$1" "${OFFSITE_S3_ENDPOINT%/}/$OFFSITE_S3_BUCKET/$2" >/dev/null
}

# media_sync (Stage 75.1.5): an upload-only copy of media_store - product
# images and other uploads, in no backup before. MEDIA_MANIFEST lists, one
# "path<TAB>size<TAB>mtime" line each, the files already copied: a file goes
# up once, again only when its size or time changes, and nothing is ever
# deleted off-box (the bucket's lifecycle rule ages objects). Recorded as its
# own backup_runs row, scope 'media'.
media_sync() {
  local dir="${MEDIA_DIR:-/opt/erp/media_store}"
  local manifest="${MEDIA_MANIFEST:-$BACKUP_DIR/media_manifest.tsv}"
  local prefix sent=0 bytes=0 failed=0 current todo line rel size outcome
  [ -d "$dir" ] || return 0
  prefix="$(offsite_prefix)"
  touch "$manifest"
  current="$(mktemp)"
  todo="$(mktemp)"
  # Guarded: under set -e a failure here would end the script after the
  # database backup was recorded, and the exit trap would then report the
  # database run as failed.
  if ! (cd "$dir" && find . -type f -printf '%P\t%s\t%T@\n') | LC_ALL=C sort > "$current" \
     || ! LC_ALL=C sort -u "$manifest" | LC_ALL=C comm -13 - "$current" > "$todo"; then
    rm -f "$current" "$todo"
    alert_ops BACKUP_OFFSITE_FAILED "media copy: could not list $dir or read its manifest"
    return 0
  fi
  while IFS= read -r line; do
    rel="${line%%$'\t'*}"
    size="$(printf '%s' "$line" | cut -f2)"
    if offsite_put "$dir/$rel" "$prefix/media/$rel"; then
      printf '%s\n' "$line" >> "$manifest"
      sent=$((sent + 1))
      bytes=$((bytes + size))
    else
      failed=$((failed + 1))
    fi
  done < "$todo"
  rm -f "$current" "$todo"
  outcome=Success
  [ "$failed" -eq 0 ] || outcome=Failed
  RUN_SCOPE=media OFFSITE_KEY="$prefix/media/" OFFSITE_OUTCOME="$outcome"
  record_run "$outcome" "" "$bytes" "" "copied $sent new or changed file(s), $failed failed"
  RUN_SCOPE="" OFFSITE_KEY="" OFFSITE_OUTCOME=""
  if [ "$failed" -ne 0 ]; then
    alert_ops BACKUP_OFFSITE_FAILED "media copy: $failed file(s) could not be uploaded off-box ($sent copied)"
  fi
  return 0
}

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
  exists="$(psql -tAc "SELECT 1 FROM information_schema.schemata WHERE schema_name = '$TENANT_SCHEMA'" "$DATABASE_URL")"
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
  pg_dump --schema="$TENANT_SCHEMA" -F c -f "$dump" "$DATABASE_URL"
else
  pg_dump -F c -f "$dump" "$DATABASE_URL"
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

# Off-box copy of the dump and its checksum (Stage 75.1.4). A failed upload
# does not fail the backup - the local copy is good - but it is alerted and
# recorded, and the server's monitor alerts when no off-box copy is fresh.
if offsite_enabled; then
  kind=database
  [ -n "$TENANT_SCHEMA" ] && kind=tenant
  OFFSITE_KEY="$(offsite_prefix)/$kind/$(basename "$enc")"
  if offsite_put "$enc" "$OFFSITE_KEY" && offsite_put "$enc.sha256" "$OFFSITE_KEY.sha256"; then
    OFFSITE_OUTCOME=Success
  else
    OFFSITE_OUTCOME=Failed
    alert_ops BACKUP_OFFSITE_FAILED "${TENANT_SCHEMA:-whole-database} backup completed locally but could not be copied off-box"
  fi
fi

# sha256sum prefixes the hash with "\" when the file name needed escaping.
enc_sha="$(cut -d' ' -f1 "$enc.sha256")"
record_run Success "$(basename "$enc")" "$(wc -c < "$enc" | tr -d ' ')" "${enc_sha#\\}"
echo "backup complete: $enc${OFFSITE_OUTCOME:+ (off-box: $OFFSITE_OUTCOME)}"

# The nightly whole-database run also brings the media copy up to date.
if offsite_enabled && [ -z "$TENANT_SCHEMA" ]; then
  media_sync
fi
