#!/usr/bin/env bash
# Install deploy/erp.service as the live unit, with automatic rollback
# (Stage 109.11). remote_deploy.sh runs it after every healthy deploy, so the
# droplet always runs the repo's unit; until 2026-10-19 it ran the first
# baseline unit while the repo's Stage 49.7 sandbox was never installed.
#
#   sudo bash /opt/erp/deploy/install_service_unit.sh
#
# Same unit already live: prints SERVICE-UNCHANGED and does nothing. Otherwise:
# creates the unit's ReadWritePaths= folders (owned by erp), checks the unit
# with systemd-analyze, keeps the old one as erp.service.pre-<time>, installs,
# restarts and waits for /api/v1/health. Not healthy in 30 s: the old unit goes
# back and the service is restarted on it (SERVICE-ROLLED-BACK). Healthy: proves
# inside the running sandbox that every writable folder takes a write and that
# the build itself is read-only, then prints SERVICE-INSTALLED.
set -uo pipefail
cd /

SRC="${SRC:-/opt/erp/deploy/erp.service}"
DST="${DST:-/etc/systemd/system/erp.service}"
HEALTH_URL="${HEALTH_URL:-http://127.0.0.1:8080/api/v1/health}"
ERP_USER="${ERP_USER:-erp}"

[ "$(id -u)" = "0" ] || { echo "ERROR: run as root" >&2; exit 1; }
[ -f "$SRC" ] || { echo "ERROR: $SRC not found" >&2; exit 1; }
if [ -f "$DST" ] && cmp -s "$SRC" "$DST"; then echo SERVICE-UNCHANGED; exit 0; fi

healthy() {
  for _ in $(seq 1 30); do
    curl -fs -m 2 "$HEALTH_URL" >/dev/null 2>&1 && return 0
    sleep 1
  done
  return 1
}

# The writable folders must exist first: ProtectSystem=strict fails a unit
# whose ReadWritePaths= entry is missing on some systemd versions.
paths="$(sed -n 's/^ReadWritePaths=//p' "$SRC" | tr ' ' '\n' | sed '/^$/d')"
for d in $paths; do
  mkdir -p "$d" && chown "$ERP_USER":"$ERP_USER" "$d"
done

if ! out="$(systemd-analyze verify "$SRC" 2>&1)"; then
  echo "ERROR: systemd-analyze rejects $SRC - nothing changed:" >&2
  echo "$out" >&2
  exit 1
fi

ts="$(date -u +%Y%m%dT%H%M%SZ)"
prev="$DST.pre-$ts"
[ -f "$DST" ] && cp -a "$DST" "$prev"
install -m 644 "$SRC" "$DST"
systemctl daemon-reload
systemctl restart erp || true

if ! healthy; then
  echo "[ROLLBACK] the ERP did not come back healthy on the new unit - restoring $prev" >&2
  journalctl -u erp -n 30 --no-pager 2>&1 | tail -n 30 >&2 || true
  if [ -f "$prev" ]; then cp -a "$prev" "$DST"; systemctl daemon-reload; systemctl restart erp || true; fi
  if healthy; then echo SERVICE-ROLLED-BACK; exit 1; fi
  echo SERVICE-ROLLBACK-FAILED
  exit 2
fi

# Inside the running service's own sandbox: each writable folder takes a write,
# and the build is read-only (the point of ProtectSystem=strict).
pid="$(systemctl show -p MainPID --value erp)"
bad=0
for d in $paths; do
  if nsenter -t "$pid" -m -- su -s /bin/sh "$ERP_USER" -c "touch '$d/.sandbox_probe' && rm -f '$d/.sandbox_probe'" 2>/dev/null; then
    echo "writable: $d"
  else
    echo "[WARN] not writable inside the sandbox: $d" >&2; bad=1
  fi
done
if nsenter -t "$pid" -m -- su -s /bin/sh "$ERP_USER" -c "touch /opt/erp/public/.sandbox_probe" 2>/dev/null; then
  rm -f /opt/erp/public/.sandbox_probe
  echo "[WARN] /opt/erp/public is writable inside the sandbox - ProtectSystem=strict is not in effect" >&2; bad=1
else
  echo "read-only: /opt/erp/public"
fi
systemd-analyze security erp.service --no-pager 2>/dev/null | tail -n 1
echo "SERVICE-INSTALLED (previous unit kept as $prev)"
[ "$bad" = 0 ] || exit 3
exit 0
