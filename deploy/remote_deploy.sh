#!/usr/bin/env bash
# Swap in the new binary + public/, migrate, restart, health-check, and
# automatically roll back to the pre-deploy build if the new one does not
# come up healthy -- so a bad deploy degrades to "briefly retried, then back
# to the last-known-good build" instead of an unattended crash loop.
#
# Written after the 2026-09-13 incident (docs/ai_handover.md SS6,
# docs/micro_checklist.md's "Production deploy blocker" section): a deploy
# hit engines/security_baseline.go's production fail-fast gate, the new
# binary refused to boot, systemd's Restart=on-failure crash-looped the box
# for ~90s, and the rollback that ended it was done by hand over SSH because
# nothing here could do it automatically. This script is that missing net.
#
# Kept as its own file rather than a PowerShell here-string in deploy.ps1 on
# purpose: deploy.ps1's existing heredoc comments already document two
# separate bugs from bash logic living inside a PowerShell double-quoted
# string (CRLF line endings, and `$var` being eaten by PowerShell
# interpolation before bash ever sees it). A real, LF-normalized file
# (.gitattributes already forces LF for deploy/*.sh, same as migrate.sh and
# backup.sh) sidesteps both classes of bug entirely.
#
# Invoked by deploy.ps1 over ssh, after it has already scp'd
# erp-server.new / public/ / db/ / this file into REMOTE_DIR:
#   REMOTE_DIR=/opt/erp DEPLOY_COMMIT=abc1234 bash remote_deploy.sh
#
# Exits 0 only on DEPLOY-OK. Exits 1 on DEPLOY-ROLLED-BACK (new build was
# unhealthy but the previous one is confirmed serving again) or
# DEPLOY-ROLLBACK-FAILED (worse: needs a human on the box right now) --
# deploy.ps1 treats both as a failed deploy, distinguished only for whoever
# is reading the output.
set -euo pipefail

REMOTE_DIR="${REMOTE_DIR:?set REMOTE_DIR, e.g. /opt/erp}"
DEPLOY_COMMIT="${DEPLOY_COMMIT:-unknown}"
HEALTH_URL="http://127.0.0.1:8080/"
HEALTH_RETRIES=15
HEALTH_INTERVAL=1

cd "$REMOTE_DIR"

# Same Slack-compatible webhook convention as backup.sh's alert_ops -- one
# OPS_ALERT_WEBHOOK_URL (already sourced from /etc/erp/erp.env by the caller)
# covers app alerts, backup alerts and now deploy alerts. Unset = silent
# no-op, so dev/staging boxes need no configuration.
alert_ops() {
  [ -n "${OPS_ALERT_WEBHOOK_URL:-}" ] || return 0
  host="$(hostname -s 2>/dev/null || echo unknown-host)"
  text=":rotating_light: [DEPLOY] $host: $1"
  escaped="$(printf '%s' "$text" | sed -e 's/\\/\\\\/g' -e 's/"/\\"/g' | tr -d '\000-\037')"
  curl -fsS -m 10 -X POST -H 'Content-Type: application/json' \
    --data "{\"text\":\"$escaped\"}" "$OPS_ALERT_WEBHOOK_URL" >/dev/null 2>&1 \
    || echo "[WARN] deploy alert webhook delivery failed" >&2
  return 0
}

# Real signal, not a single sleep-then-check guess: a crash-looping unit can
# read back "activating" or even a stale "active" a moment after systemd's
# Restart=on-failure fires it again, so poll both the unit state and an
# actual HTTP response for a few seconds before deciding either way. Every
# branch below uses `if`, never a bare `cmd && cmd` statement, because a
# false left-hand side in a bare AND-list still trips `set -e` -- the one
# other footgun this file exists to avoid, alongside the CRLF/interpolation
# ones described above.
#
# Stage 50/BLD-006: takes an optional expected-commit argument. A 200 alone
# used to be treated as "the new deploy is healthy" - but if `systemctl
# restart` itself silently failed (see the guarded calls below), the OLD
# process can still be the one answering 200, and this would have reported
# DEPLOY-OK for a deploy that never actually took effect (found via
# docs/qa/audit-deploy-mock.py's restart-fails case - a real false success,
# not a hypothetical). /api/v1/health now echoes git_commit; when an expected
# commit is given, a live 200 from a DIFFERENT commit keeps polling/fails
# exactly like an unhealthy response would, instead of being counted as
# success. Called with no argument for the rollback's own check below, since
# this script does not currently track the prior build's commit hash to
# compare against - that narrower gap is not this fix's scope.
wait_healthy() {
  local expected_commit="${1:-}"
  local attempt=0
  while [ "$attempt" -lt "$HEALTH_RETRIES" ]; do
    sleep "$HEALTH_INTERVAL"
    attempt=$((attempt + 1))
    if ! systemctl is-active --quiet erp; then
      continue
    fi
    body="$(curl -s -m 3 "$HEALTH_URL" || true)"
    code="$(curl -s -o /dev/null -w '%{http_code}' -m 3 "$HEALTH_URL" || true)"
    if [ "$code" != "200" ]; then
      continue
    fi
    if [ -z "$expected_commit" ]; then
      return 0
    fi
    case "$body" in
      *"\"git_commit\":\"$expected_commit\""*) return 0 ;;
    esac
  done
  return 1
}

# --- Snapshot the pre-deploy release before touching anything. db/ is
# deliberately not snapshotted: migrations are additive and applied below
# with the new binary regardless of whether the swap that follows succeeds,
# matching the accepted "schema stays forward, only the binary/static files
# roll back" posture from the 2026-09-13 incident.
rm -rf erp-server.prev public.prev
if [ -f erp-server ]; then
  cp -a erp-server erp-server.prev
fi
if [ -d public ]; then
  cp -a public public.prev
fi

chmod +x erp-server.new
ERP_BINARY="$REMOTE_DIR/erp-server.new" bash "$REMOTE_DIR/deploy/migrate.sh"
mv erp-server.new erp-server
chmod +x erp-server
# Stage 50/BLD-005: activate the staged frontend here, in the same place and
# the same way the binary swap above already does - deploy.ps1 now ships the
# new public/ to public.new rather than overwriting the live directory, so
# the snapshot above is a genuine pre-deploy copy and this activation (or its
# rollback further down) is what the binary/frontend pairing actually
# depends on staying in lockstep.
if [ -d public.new ]; then
  rm -rf public
  mv public.new public
fi
# Stage 50/BLD-006: this was a bare `sudo systemctl restart erp` under
# `set -e` - if systemctl itself failed (permission slip, unit file error,
# the service manager briefly unavailable), the WHOLE script died right
# here, before wait_healthy or any of the rollback logic below ever ran. The
# new binary/public were already activated, so that left the box in a
# half-deployed state with no rollback attempted and no DEPLOY-* marker
# printed at all - deploy.ps1 would report "remote deploy script did not
# reach any recognized end state", not a clean failure. `|| true` lets
# wait_healthy make the real call below: a restart that failed outright is
# just another way the service isn't healthy, and the existing rollback path
# already handles that correctly regardless of which symptom caused it.
sudo systemctl restart erp || true

if wait_healthy "$DEPLOY_COMMIT"; then
  echo DEPLOY-OK
  exit 0
fi

echo "[ROLLBACK] $DEPLOY_COMMIT did not become healthy within $((HEALTH_RETRIES * HEALTH_INTERVAL))s -- reverting to the pre-deploy build" >&2
journalctl -u erp -n 60 --no-pager 2>&1 | tail -n 60 || true

mv erp-server "erp-server.failed-$(date -u +%Y%m%dT%H%M%SZ)"
if [ ! -f erp-server.prev ]; then
  echo "[ROLLBACK] no erp-server.prev on this box -- this looks like the first deploy here, cannot auto-rollback" >&2
  alert_ops "deploy of $DEPLOY_COMMIT failed its health check and there is NO previous build to roll back to -- needs immediate manual attention."
  echo DEPLOY-ROLLBACK-FAILED
  exit 1
fi
mv erp-server.prev erp-server
chmod +x erp-server
if [ -d public.prev ]; then
  rm -rf public
  mv public.prev public
fi
# Stage 50/BLD-006: same bare-restart-under-set-e problem as the activation
# restart above, but here it is worse - this IS already the rollback path,
# so dying here on a restart failure skipped the DEPLOY-ROLLBACK-FAILED
# branch and its alert entirely, exiting via `set -e`'s own untrapped error
# instead. This is "the rollback's own restart failure" case: `|| true` lets
# the wait_healthy check below fail honestly and fall through to the
# DEPLOY-ROLLBACK-FAILED branch that already exists for exactly this.
sudo systemctl restart erp || true

if wait_healthy; then
  alert_ops "deploy of $DEPLOY_COMMIT failed its health check and was automatically rolled back -- service restored."
  echo DEPLOY-ROLLED-BACK
  exit 1
fi

alert_ops "deploy of $DEPLOY_COMMIT failed its health check AND the rollback did not come back healthy either -- needs immediate manual attention."
echo DEPLOY-ROLLBACK-FAILED
exit 1
