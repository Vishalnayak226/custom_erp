#!/usr/bin/env bash
# Activate the real Caddy config: substitute the domain + ACME email into
# deploy/Caddyfile, verify DNS actually points at this box, validate the
# generated config, install it, reload Caddy and confirm TLS came up.
#
# This is the one supported way to put the ERP on a public hostname (Stage
# 26.1.1). Until it is run, the box serves deploy/Caddyfile.holding, which does
# not expose the app at all -- see that file for why plaintext exposure is
# refused.
#
# Usage (as root, on the box):
#   sudo bash /opt/erp/deploy/enable_tls.sh erp.yourdomain.com you@yourdomain.com
#
# Re-runnable: it backs up the current /etc/caddy/Caddyfile before replacing it
# and rolls back automatically if Caddy fails to reload.
set -euo pipefail

DOMAIN="${1:?usage: enable_tls.sh <domain> <acme-email>}"
EMAIL="${2:?usage: enable_tls.sh <domain> <acme-email>}"

SRC="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)/Caddyfile"
DEST=/etc/caddy/Caddyfile
STAMP="$(date -u +%Y%m%dT%H%M%SZ)"

[[ -f "$SRC" ]] || { echo "FAIL: $SRC not found"; exit 1; }
command -v caddy >/dev/null || { echo "FAIL: caddy is not installed"; exit 1; }

# --- 1. Sanity-check the arguments before touching anything ---------------
if [[ ! "$DOMAIN" =~ ^[a-zA-Z0-9]([a-zA-Z0-9-]*[a-zA-Z0-9])?(\.[a-zA-Z0-9]([a-zA-Z0-9-]*[a-zA-Z0-9])?)+$ ]]; then
	echo "FAIL: '$DOMAIN' is not a valid hostname."; exit 1
fi
if [[ ! "$EMAIL" =~ ^[^@[:space:]]+@[^@[:space:]]+\.[^@[:space:]]+$ ]]; then
	echo "FAIL: '$EMAIL' is not a valid email address."; exit 1
fi

# --- 2. DNS must already point here, or ACME will fail and rate-limit us ---
# Let's Encrypt applies a hard weekly cap on failed authorizations per account,
# so a premature run is not free -- it can lock out the real attempt for days.
PUBLIC_IP="$(curl -fsS --max-time 10 https://api.ipify.org || true)"
RESOLVED="$(getent ahostsv4 "$DOMAIN" 2>/dev/null | awk 'NR==1{print $1}' || true)"

if [[ -z "$RESOLVED" ]]; then
	echo "FAIL: '$DOMAIN' does not resolve. Create an A record pointing at ${PUBLIC_IP:-this box's public IP} and wait for it to propagate."
	exit 1
fi
if [[ -n "$PUBLIC_IP" && "$RESOLVED" != "$PUBLIC_IP" ]]; then
	echo "FAIL: '$DOMAIN' resolves to $RESOLVED but this box is $PUBLIC_IP."
	echo "      Fix the A record first. Running anyway would burn Let's Encrypt failure quota."
	exit 1
fi
echo "OK: $DOMAIN -> $RESOLVED (matches this box)"

# --- 3. Generate, validate, then install ----------------------------------
TMP="$(mktemp)"
trap 'rm -f "$TMP"' EXIT
sed -e "s|ERP_DOMAIN_PLACEHOLDER|${DOMAIN}|g" \
    -e "s|ERP_ACME_EMAIL_PLACEHOLDER|${EMAIL}|g" "$SRC" > "$TMP"

if grep -q "PLACEHOLDER" "$TMP"; then
	echo "FAIL: a placeholder survived substitution -- refusing to install."; exit 1
fi

mkdir -p /var/log/caddy && chown caddy:caddy /var/log/caddy 2>/dev/null || true

if ! caddy validate --config "$TMP" --adapter caddyfile >/dev/null 2>&1; then
	echo "FAIL: generated config is invalid. Details:"
	caddy validate --config "$TMP" --adapter caddyfile || true
	exit 1
fi
echo "OK: generated config validates"

if [[ -f "$DEST" ]]; then
	cp -a "$DEST" "${DEST}.bak.${STAMP}"
	echo "OK: backed up existing config to ${DEST}.bak.${STAMP}"
fi
install -m 0644 "$TMP" "$DEST"

# --- 4. Reload, rolling back if Caddy refuses it ---------------------------
if ! systemctl reload caddy; then
	echo "FAIL: caddy reload failed -- rolling back."
	[[ -f "${DEST}.bak.${STAMP}" ]] && cp -a "${DEST}.bak.${STAMP}" "$DEST" && systemctl reload caddy || true
	exit 1
fi
echo "OK: caddy reloaded"

# --- 5. Confirm TLS actually came up --------------------------------------
# Certificate issuance is asynchronous; give ACME a moment before judging it.
echo -n "Waiting for a certificate"
for _ in $(seq 1 20); do
	sleep 3; echo -n "."
	if curl -fsS -o /dev/null --max-time 5 "https://${DOMAIN}/api/v1/health" 2>/dev/null; then
		echo; echo "OK: https://${DOMAIN}/api/v1/health responded over TLS"
		echo
		echo "Remaining steps, in order:"
		echo "  1. Set the browser origin allow-list, then restart the app:"
		echo "       echo 'CORS_ALLOWED_ORIGINS=https://${DOMAIN}' >> /etc/erp/erp.env"
		echo "       systemctl restart erp"
		echo "  2. Consider putting Cloudflare in front for a real edge WAF"
		echo "     (go_live_decisions.md section 5), then restrict ufw to its ranges."
		exit 0
	fi
done
echo
echo "WARN: config installed and Caddy reloaded, but https://${DOMAIN}/api/v1/health"
echo "      did not answer within 60s. Certificate issuance can lag; check with:"
echo "        journalctl -u caddy -n 50 --no-pager"
exit 1
