#!/usr/bin/env bash
# Install the daily server cleanup (Stage 109.3) as a root cron entry.
#
#   sudo bash /opt/erp/deploy/install_cleanup_cron.sh
#
# remote_deploy.sh runs this on every deploy, so the box always carries the
# current schedule and script. Idempotent: a marker comment identifies our
# line, so re-running replaces it and never touches anyone else's cron jobs
# (same pattern as install_backup_cron.sh).
set -euo pipefail
cd /

CLEANUP_SCRIPT="${CLEANUP_SCRIPT:-/opt/erp/deploy/server_cleanup.sh}"
# 03:30 - after the 02:00 nightly backup, so a backup is never racing it.
CRON_SCHEDULE="${CRON_SCHEDULE:-30 3 * * *}"
CRON_MARKER="# erp-daily-cleanup (managed by deploy/install_cleanup_cron.sh)"
CRON_LINE="$CRON_SCHEDULE /bin/bash $CLEANUP_SCRIPT >/dev/null 2>&1 $CRON_MARKER"
# Stage 109.12: the deep pass asks daily at 04:15 (after the 03:30 run) and
# runs once its last run is 10 days old - the script keeps that count.
DEEP_SCHEDULE="${DEEP_SCHEDULE:-15 4 * * *}"
DEEP_MARKER="# erp-deep-cleanup (managed by deploy/install_cleanup_cron.sh)"
DEEP_LINE="$DEEP_SCHEDULE /bin/bash $CLEANUP_SCRIPT --deep --if-due >/dev/null 2>&1 $DEEP_MARKER"

[ "$(id -u)" = "0" ] || { echo "ERROR: run as root (the cleanup removes root-owned files)" >&2; exit 1; }
[ -f "$CLEANUP_SCRIPT" ] || { echo "ERROR: $CLEANUP_SCRIPT not found - is the repo deployed to /opt/erp?" >&2; exit 1; }
chmod 755 "$CLEANUP_SCRIPT"
touch /var/log/erp-cleanup.log
chmod 644 /var/log/erp-cleanup.log

existing="$(crontab -l 2>/dev/null || true)"
filtered="$(printf '%s\n' "$existing" | grep -v -F -e "$CRON_MARKER" -e "$DEEP_MARKER" || true)"
printf '%s\n%s\n%s\n' "$filtered" "$CRON_LINE" "$DEEP_LINE" | sed '/^$/d' | crontab -

echo "Installed daily cleanup and the 10-day deep pass for root:"
crontab -l | grep -F -e "$CRON_MARKER" -e "$DEEP_MARKER"
