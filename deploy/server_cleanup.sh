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
# With the disk over TIGHT_DISK_PCT (70%) it keeps one rollback copy fewer.
# Stage 109.12: --deep (root cron at 04:15 with --if-due, so every
# DEEP_EVERY_DAYS = 10 days) also removes
#   - packages nothing needs any more, old kernels among them
#   - temp files untouched for 10 days, crash dumps over 10 days old
#   - rotated system logs over 180 days old (CERT-In keeps 180 days)
# and says what is growing: each area's size against the last deep pass, and
# how many days until the disk is full at that rate (alert under 30 days).
# It never touches backups/, erp-server, erp-server.prev, public,
# public.prev, db/, deploy/ or PostgreSQL - the deep pass only measures them.
# On 2026-10-19 the box was 66% full while the ERP itself used ~150 MB; this
# keeps it from drifting back.
#
#   bash /opt/erp/deploy/server_cleanup.sh                  # clean
#   bash /opt/erp/deploy/server_cleanup.sh --dry-run        # say what it would do
#   bash /opt/erp/deploy/server_cleanup.sh --deep           # clean, deep pass now
#   bash /opt/erp/deploy/server_cleanup.sh --deep --if-due  # deep pass if 10 days are up
set -uo pipefail
cd /

REMOTE_DIR="${REMOTE_DIR:-/opt/erp}"
KEEP_ROLLBACKS="${KEEP_ROLLBACKS:-3}"
JOURNAL_MAX="${JOURNAL_MAX:-100M}"
DISK_ALERT_PCT="${DISK_ALERT_PCT:-80}"
TIGHT_DISK_PCT="${TIGHT_DISK_PCT:-70}"
DEEP_EVERY_DAYS="${DEEP_EVERY_DAYS:-10}"
LOG_KEEP_DAYS="${LOG_KEEP_DAYS:-180}"
FULL_ALERT_DAYS="${FULL_ALERT_DAYS:-30}"
STATE_DIR="${STATE_DIR:-/var/lib/erp-cleanup}"
LOG_FILE="${LOG_FILE:-/var/log/erp-cleanup.log}"
ERP_ENV_FILE="${ERP_ENV_FILE:-/etc/erp/erp.env}"
DRY=0; DEEP=0; IF_DUE=0
for arg in "$@"; do
  case "$arg" in
    --dry-run) DRY=1 ;;
    --deep) DEEP=1 ;;
    --if-due) IF_DUE=1 ;;
    *) echo "unknown option $arg (--dry-run, --deep, --if-due)" >&2; exit 2 ;;
  esac
done

[ "$(id -u)" = "0" ] || { echo "server_cleanup.sh must run as root" >&2; exit 1; }

# The 04:15 cron asks every day; the deep pass runs once its last run is
# DEEP_EVERY_DAYS old (an hour's slack for cron's start time), so it keeps a
# true 10-day rhythm instead of a calendar's 1st / 11th / 21st / 31st.
now="$(date +%s)"
stamp="$STATE_DIR/deep.last"
if [ "$DEEP" = 1 ] && [ "$IF_DUE" = 1 ] && [ -f "$stamp" ]; then
  last="$(cat "$stamp" 2>/dev/null)"
  case "$last" in ''|*[!0-9]*) last=0 ;; esac
  [ $((now - last)) -ge $((DEEP_EVERY_DAYS * 86400 - 3600)) ] || exit 0
fi

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
avail_kb() { df -Pk / | awk 'NR==2{print $4}'; }
disk_pct() { df -P / | awk 'NR==2{gsub("%","",$5); print $5}'; }
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

# prune WHAT FIND-ARGS... - deletes (or, with --dry-run, counts) the files a
# find selects, in one summary line rather than one line per file.
prune() {
  local what="$1" n kb; shift
  read -r n kb < <(find "$@" -printf '%k\n' 2>/dev/null | awk '{n++; s+=$1} END{print n+0, s+0}')
  [ "$n" -gt 0 ] || return 0
  if [ "$DRY" = 1 ]; then echo "would remove $n $what (${kb} KB)"; else find "$@" -delete 2>/dev/null; echo "removed $n $what (${kb} KB)"; fi
  actions=$((actions + 1))
}

# 1. Rollback copies: keep the newest KEEP_ROLLBACKS (deploy makes one each
# time); one fewer, never under 2, once the disk is over TIGHT_DISK_PCT.
keep="$KEEP_ROLLBACKS"
if [ "$(disk_pct)" -ge "$TIGHT_DISK_PCT" ] && [ "$keep" -gt 2 ]; then
  keep=$((keep - 1))
  echo "disk over ${TIGHT_DISK_PCT}%: keeping $keep rollback copies"
fi
if [ -d "$REMOTE_DIR" ]; then
  mapfile -t old < <(ls -1dt "$REMOTE_DIR"/rollback_* 2>/dev/null | tail -n +"$((keep + 1))")
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

# 9. Deep pass (Stage 109.12, --deep): what the nightly run leaves behind,
# then what is growing.
deep_note=""
if [ "$DEEP" = 1 ]; then
  # Packages nothing depends on any more - old kernels mostly; apt keeps the
  # running kernel and the newest. Waits out unattended-upgrades' lock.
  if command -v apt-get >/dev/null 2>&1; then
    pkgs="$(apt-get -s autoremove --purge 2>/dev/null | grep -cE '^(Purg|Remv) ')"
    if [ "$pkgs" -gt 0 ]; then
      if [ "$DRY" = 1 ]; then echo "would autoremove $pkgs unused package(s)"
      elif DEBIAN_FRONTEND=noninteractive apt-get -y -q -o DPkg::Lock::Timeout=600 autoremove --purge >/dev/null 2>&1; then echo "autoremoved $pkgs unused package(s)"
      else echo "[WARN] apt-get autoremove failed" >&2; fi
      actions=$((actions + 1))
    fi
  fi
  prune "temp file(s) untouched for $DEEP_EVERY_DAYS days" /tmp /var/tmp -xdev -mindepth 1 -type f -mtime +"$DEEP_EVERY_DAYS" -not -path '*/systemd-private-*'
  prune "crash dump(s) over $DEEP_EVERY_DAYS days old" /var/crash /var/lib/systemd/coredump -xdev -type f -mtime +"$DEEP_EVERY_DAYS"
  prune "rotated log(s) over $LOG_KEEP_DAYS days old" /var/log -xdev -type f \( -name '*.gz' -o -name '*.[0-9]' -o -name '*.old' \) -mtime +"$LOG_KEEP_DAYS"

  # Each area's size against the last deep pass (sizes.tsv; _used / _at are
  # the disk's used KB and the time then), and the days until the disk is
  # full at the rate since. Backups, the build and PostgreSQL are only measured.
  sizes="$STATE_DIR/sizes.tsv"
  declare -A was=()
  if [ -f "$sizes" ]; then
    while IFS=$'\t' read -r k v; do [ -n "$k" ] && was["$k"]="$v"; done < "$sizes"
  fi
  cur=""; grew=""; grew_kb=0
  echo "sizes (change since the last deep pass):"
  for a in "$REMOTE_DIR/backups" "$REMOTE_DIR" /var/lib/postgresql /var/log /var/cache /tmp /var/tmp /root /home /boot /usr; do
    [ -d "$a" ] || continue
    kb="$(du -sxk "$a" 2>/dev/null | cut -f1)"
    [ -n "$kb" ] || continue
    cur+="$a"$'\t'"$kb"$'\n'
    change=""
    if [ -n "${was[$a]:-}" ]; then
      d=$((kb - ${was[$a]}))
      change="$(printf ' (%+d MB)' $((d / 1024)))"
      if [ "$d" -ge 1024 ] && [ "$d" -gt "$grew_kb" ]; then grew_kb=$d; grew="$a"; fi
    fi
    printf '  %-24s %7d MB%s\n' "$a" $((kb / 1024)) "$change"
  done
  used="$(used_kb)"; full=""
  if [ -z "${was[_at]:-}" ]; then
    deep_note=" deep=baseline"
    echo "first deep pass: sizes recorded, growth shows from the next one"
  else
    deep_note=" deep"
    [ -n "$grew" ] && deep_note+=" grew=$grew:+$((grew_kb / 1024))MB"
    secs=$((now - ${was[_at]})); days=$(( (secs + 43200) / 86400 ))
    if [ "$secs" -ge 86400 ]; then
      rate=$(( (used - ${was[_used]:-$used}) * 86400 / secs ))   # KB a day
      if [ "$rate" -gt 0 ]; then full=$(( $(avail_kb) / rate )); fi
    fi
    if [ -n "$full" ]; then
      deep_note+=" full_in=${full}d"
      echo "at the last $days days' rate the disk is full in about $full days"
    else
      echo "the disk is not filling (last $days days)"
    fi
  fi
  if [ -n "$full" ] && [ "$full" -lt "$FULL_ALERT_DAYS" ]; then
    msg="disk full in about $full days at the last $days days' rate ($(disk_pct)% now); grew most: ${grew:-nothing measured} (+$((grew_kb / 1024)) MB)."
    if [ "$DRY" = 1 ]; then echo "would alert: $msg"; else alert_ops "$msg"; fi
  fi
  if [ "$DRY" = 0 ]; then
    mkdir -p "$STATE_DIR"
    printf '%s_used\t%s\n_at\t%s\n' "$cur" "$used" "$now" > "$sizes"
    echo "$now" > "$stamp"
  fi
fi

# 10. Report: one line a run, and an alert when the disk is filling anyway.
after_kb="$(used_kb)"
freed_mb=$(( (before_kb - after_kb) / 1024 ))
pct="$(disk_pct)"
line="$(date -u +%Y-%m-%dT%H:%M:%SZ) actions=$actions freed=${freed_mb}MB disk=${pct}%$deep_note"
[ "$DRY" = 1 ] && line="$line (dry run)"
echo "$line"
[ "$DRY" = 1 ] || echo "$line" >> "$LOG_FILE"
if [ "$pct" -ge "$DISK_ALERT_PCT" ]; then
  alert_ops "disk is ${pct}% full after cleanup (alert at ${DISK_ALERT_PCT}%) - something other than clutter is growing; check du -xsh /* on the box."
fi
exit 0
