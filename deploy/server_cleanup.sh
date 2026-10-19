#!/usr/bin/env bash
# Stage 109.3: the ERP server keeps itself lean. Run daily by root cron
# (deploy/install_cleanup_cron.sh, 03:30 - after the 02:00 backup) and once
# after every Deploy click. Idempotent, and it only ever removes clutter:
#   - rollback_* copies beyond the newest KEEP_ROLLBACKS (default 3)
#   - erp-server.failed-* builds older than 7 days
#   - files in the ERP folder whose names hold control characters (an old
#     CRLF script once left "erp-server\r" there, 12 MB)
#   - erp-server.new / public.new left by a deploy that died over a day ago
#   - downloaded package files (apt-get clean)
#   - system logs above 100 MB (journald is also capped by its own config,
#     which this script puts back if it goes missing)
#   - rotated failed-login logs, and the live one above 20 MB (SSH bots)
# It never touches backups/, erp-server, erp-server.prev, public,
# public.prev, db/, deploy/ or PostgreSQL. On 2026-10-19 the box was 66%
# full while the ERP itself used ~150 MB; this keeps it from drifting back.
#
#   bash /opt/erp/deploy/server_cleanup.sh            # clean
#   bash /opt/erp/deploy/server_cleanup.sh --dry-run  # say what it would do
set -uo pipefail
cd /

REMOTE_DIR="${REMOTE_DIR:-/opt/erp}"
KEEP_ROLLBACKS="${KEEP_ROLLBACKS:-3}"
JOURNAL_MAX="${JOURNAL_MAX:-100M}"
DISK_ALERT_PCT="${DISK_ALERT_PCT:-80}"
LOG_FILE="${LOG_FILE:-/var/log/erp-cleanup.log}"
ERP_ENV_FILE="${ERP_ENV_FILE:-/etc/erp/erp.env}"
DRY=0
[ "${1:-}" = "--dry-run" ] && DRY=1

[ "$(id -u)" = "0" ] || { echo "server_cleanup.sh must run as root" >&2; exit 1; }

# Same webhook convention as backup.sh / remote_deploy.sh; unset = silent.
# `set -a` because erp.env is a systemd EnvironmentFile (no `export`).
if [ -f "$ERP_ENV_FILE" ]; then set -a; . "$ERP_ENV_FILE" 2>/dev/null; set +a; fi
alert_ops() {
  echo "[ALERT] $1" >&2
  [ -n "${OPS_ALERT_WEBHOOK_URL:-}" ] || return 0
  host="$(hostname -s 2>/dev/null || echo unknown-host)"
  escaped="$(printf ':warning: server_cleanup.sh on %s: %s' "$host" "$1" | sed -e 's/\\/\\\\/g' -e 's/"/\\"/g' | tr -d '\000-\037')"
  curl -fsS -m 10 -X POST -H 'Content-Type: application/json' \
    --data "{\"text\":\"$escaped\"}" "$OPS_ALERT_WEBHOOK_URL" >/dev/null 2>&1 \
    || echo "[WARN] cleanup alert webhook delivery failed" >&2
}

used_kb() { df -Pk / | awk 'NR==2{print $3}'; }
before_kb="$(used_kb)"
actions=0

# remove PATH... - deletes (or, with --dry-run, lists) the given paths.
remove() {
  for p in "$@"; do
    [ -e "$p" ] || continue
    size="$(du -sh "$p" 2>/dev/null | cut -f1)"
    shown="$(printf '%q' "$p")"   # a name with a CR in it must not read as the live build
    if [ "$DRY" = 1 ]; then echo "would remove $shown ($size)"; else rm -rf -- "$p" && echo "removed $shown ($size)"; fi
    actions=$((actions + 1))
  done
}

# 1. Rollback copies: keep the newest KEEP_ROLLBACKS (deploy makes one each time).
if [ -d "$REMOTE_DIR" ]; then
  mapfile -t old < <(ls -1dt "$REMOTE_DIR"/rollback_* 2>/dev/null | tail -n +"$((KEEP_ROLLBACKS + 1))")
  remove "${old[@]}"

  # 2. Builds that failed their health check, once a week old.
  mapfile -t failed < <(find "$REMOTE_DIR" -maxdepth 1 -name 'erp-server.failed-*' -mtime +7 2>/dev/null)
  remove "${failed[@]}"

  # 3. Names with control characters (CR, tab, ...) - never a real ERP file.
  mapfile -t odd < <(find "$REMOTE_DIR" -maxdepth 1 -name "*[[:cntrl:]]*" 2>/dev/null)
  remove "${odd[@]}"

  # 4. Staging leftovers from a deploy that died more than a day ago.
  mapfile -t stale < <(find "$REMOTE_DIR" -maxdepth 1 \( -name 'erp-server.new' -o -name 'public.new' \) -mtime +1 2>/dev/null)
  remove "${stale[@]}"
fi

# 5. Downloaded .deb files (unattended-upgrades keeps them otherwise).
if command -v apt-get >/dev/null 2>&1; then
  debs="$(find /var/cache/apt/archives -name '*.deb' 2>/dev/null | wc -l)"
  if [ "$debs" -gt 0 ]; then
    if [ "$DRY" = 1 ]; then echo "would run apt-get clean ($debs package files)"; else apt-get clean && echo "apt-get clean ($debs package files)"; fi
    actions=$((actions + 1))
  fi
fi

# 6. System logs: the cap config, then trim to it.
cap=/etc/systemd/journald.conf.d/erp-size.conf
if [ ! -f "$cap" ]; then
  if [ "$DRY" = 1 ]; then echo "would restore $cap"; else
    mkdir -p "$(dirname "$cap")" && printf '[Journal]\nSystemMaxUse=%s\n' "$JOURNAL_MAX" > "$cap" && systemctl restart systemd-journald && echo "restored $cap"
  fi
  actions=$((actions + 1))
fi
if [ "$DRY" = 0 ] && command -v journalctl >/dev/null 2>&1; then
  journalctl --vacuum-size="$JOURNAL_MAX" >/dev/null 2>&1 || true
fi

# 7. Failed-login logs: rotated copies go; the live one is emptied above 20 MB.
mapfile -t btmps < <(ls -1 /var/log/btmp.* 2>/dev/null)
remove "${btmps[@]}"
if [ -f /var/log/btmp ] && [ "$(stat -c %s /var/log/btmp)" -gt 20971520 ]; then
  if [ "$DRY" = 1 ]; then echo "would empty /var/log/btmp"; else : > /var/log/btmp && echo "emptied /var/log/btmp"; fi
  actions=$((actions + 1))
fi

# 8. Keep snapd out (purged 2026-10-19; Ubuntu meta-packages would pull it back).
pin=/etc/apt/preferences.d/no-snapd
if [ ! -f "$pin" ]; then
  if [ "$DRY" = 1 ]; then echo "would restore $pin"; else printf 'Package: snapd\nPin: release a=*\nPin-Priority: -10\n' > "$pin" && echo "restored $pin"; fi
  actions=$((actions + 1))
fi

# 9. Report: one line a run, and an alert when the disk is filling anyway.
after_kb="$(used_kb)"
freed_mb=$(( (before_kb - after_kb) / 1024 ))
pct="$(df -P / | awk 'NR==2{gsub("%","",$5); print $5}')"
line="$(date -u +%Y-%m-%dT%H:%M:%SZ) actions=$actions freed=${freed_mb}MB disk=${pct}%"
[ "$DRY" = 1 ] && line="$line (dry run)"
echo "$line"
[ "$DRY" = 1 ] || echo "$line" >> "$LOG_FILE"
if [ "$pct" -ge "$DISK_ALERT_PCT" ]; then
  alert_ops "disk is ${pct}% full after cleanup (alert at ${DISK_ALERT_PCT}%) - something other than clutter is growing; check du -xsh /* on the box."
fi
exit 0
