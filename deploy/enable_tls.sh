#!/usr/bin/env bash
# Activate the real Caddy config: substitute the domain, ACME email and TLS
# strategy into deploy/Caddyfile, verify DNS actually points where this mode
# expects, validate the generated config, install it, reload Caddy and confirm
# TLS came up.
#
# This is the one supported way to put the ERP on a public hostname (Stage
# 26.1.1). Until it is run, the box serves deploy/Caddyfile.holding, which does
# not expose the app at all -- see that file for why plaintext exposure is
# refused.
#
# Two postures, and the flag picks which one is generated:
#
#   DIRECT (default) -- DNS points at this box, Caddy gets its own Let's
#   Encrypt certificates, per-tenant hostnames are issued on demand behind the
#   app's ask gate. This is the posture to bring up first.
#
#     sudo bash /opt/erp/deploy/enable_tls.sh app.example.com you@example.com
#
#   BEHIND CLOUDFLARE (--cloudflare) -- DNS is orange-clouded, so Cloudflare
#   terminates TLS for the browser and this box only ever talks to Cloudflare.
#   Caddy serves a Cloudflare Origin CA certificate instead of using ACME,
#   because once the origin firewall admits only Cloudflare ranges, Let's
#   Encrypt can no longer reach this box to validate a renewal -- TLS-ALPN-01
#   cannot work through a proxy that terminates TLS, and HTTP-01 through the
#   proxy is unreliable. The DNS-01 alternative needs a provider plugin and so
#   a custom Caddy binary (xcaddy), which is the trade 26.1.3a already refused.
#
#     sudo bash /opt/erp/deploy/enable_tls.sh app.example.com you@example.com \
#          --cloudflare --origin-cert /etc/caddy/origin.pem \
#          --origin-key /etc/caddy/origin.key
#
#   --cloudflare also does the half of the trust boundary that lives outside
#   Caddy: it writes Cloudflare's published CIDRs into TRUSTED_PROXY_CIDRS in
#   /etc/erp/erp.env and restarts the app. Those two lists describe one trust
#   boundary from two sides, and the failure mode when they drift is silent --
#   every visitor collapses into a single per-IP rate-limit bucket behind the
#   Cloudflare edge address. One command owns both so they cannot drift.
#
# Re-runnable: it backs up the current /etc/caddy/Caddyfile before replacing it
# and rolls back automatically if Caddy fails to reload.
set -euo pipefail

DOMAIN="${1:?usage: enable_tls.sh <domain> <acme-email> [--cloudflare --origin-cert <pem> --origin-key <key>]}"
EMAIL="${2:?usage: enable_tls.sh <domain> <acme-email> [--cloudflare --origin-cert <pem> --origin-key <key>]}"
shift 2

CLOUDFLARE=0
ORIGIN_CERT=""
ORIGIN_KEY=""
while [[ $# -gt 0 ]]; do
	case "$1" in
		--cloudflare)  CLOUDFLARE=1; shift ;;
		--origin-cert) ORIGIN_CERT="${2:?--origin-cert needs a path}"; shift 2 ;;
		--origin-key)  ORIGIN_KEY="${2:?--origin-key needs a path}"; shift 2 ;;
		*) echo "FAIL: unknown argument '$1'"; exit 1 ;;
	esac
done

SRC="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)/Caddyfile"
DEST=/etc/caddy/Caddyfile
ERP_ENV=/etc/erp/erp.env
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

if [[ "$CLOUDFLARE" -eq 1 ]]; then
	# Fail here rather than generating a config that would make Caddy fall
	# back to ACME for a hostname Let's Encrypt can no longer reach.
	[[ -n "$ORIGIN_CERT" && -n "$ORIGIN_KEY" ]] || {
		echo "FAIL: --cloudflare requires --origin-cert and --origin-key."
		echo "      Generate them in the Cloudflare dashboard under"
		echo "      SSL/TLS -> Origin Server -> Create Certificate, for"
		echo "      '${DOMAIN#*.}' and '*.${DOMAIN#*.}'."
		exit 1
	}
	[[ -f "$ORIGIN_CERT" ]] || { echo "FAIL: origin certificate '$ORIGIN_CERT' not found"; exit 1; }
	[[ -f "$ORIGIN_KEY"  ]] || { echo "FAIL: origin key '$ORIGIN_KEY' not found"; exit 1; }
elif [[ -n "$ORIGIN_CERT$ORIGIN_KEY" ]]; then
	echo "FAIL: --origin-cert/--origin-key are only meaningful with --cloudflare."
	echo "      Without it this box gets its own Let's Encrypt certificates."
	exit 1
fi

# --- 2. DNS must already point where this mode expects --------------------
# Direct mode: at this box, or ACME fails and burns Let's Encrypt's hard weekly
# cap on failed authorizations per account -- a premature run is not free, it
# can lock out the real attempt for days.
#
# Cloudflare mode: at Cloudflare. The check is inverted rather than skipped,
# because installing an Origin CA certificate while the record is still
# grey-clouded points browsers straight at a certificate only Cloudflare
# trusts, and takes the site down with a certificate error.
PUBLIC_IP="$(curl -fsS --max-time 10 https://api.ipify.org || true)"
RESOLVED="$(getent ahostsv4 "$DOMAIN" 2>/dev/null | awk 'NR==1{print $1}' || true)"

if [[ -z "$RESOLVED" ]]; then
	# NOTE: no apostrophes inside this ${VAR:-default}. A single quote in the
	# default word of a parameter expansion opens a quote bash never closes,
	# and the whole script then dies at parse time with "unexpected EOF" -
	# pointing at the last line, not at this one. That is exactly what happened
	# on this script's first real run (2026-08-14); it had been unexecutable
	# since it was written because no domain existed to run it against.
	echo "FAIL: '$DOMAIN' does not resolve. Create an A record pointing at ${PUBLIC_IP:-the public IP of this box} and wait for it to propagate."
	exit 1
fi

# ip_to_int/in_cidr: enough IPv4 arithmetic to test membership of a CIDR
# without pulling in ipcalc or python. Bash only, deliberately -- this script
# runs on a box whose whole point is that it carries no extra tooling.
ip_to_int() {
	local IFS=.; read -r a b c d <<<"$1"
	echo $(( (a << 24) + (b << 16) + (c << 8) + d ))
}
in_cidr() {
	local ip="$1" cidr="$2" net bits mask ip_i net_i
	net="${cidr%/*}"; bits="${cidr#*/}"
	[[ "$net" == *.*.*.* ]] || return 1          # skip IPv6 entries
	mask=$(( 0xFFFFFFFF << (32 - bits) & 0xFFFFFFFF ))
	ip_i=$(ip_to_int "$ip"); net_i=$(ip_to_int "$net")
	(( (ip_i & mask) == (net_i & mask) ))
}

CF_V4=""; CF_V6=""
if [[ "$CLOUDFLARE" -eq 1 ]]; then
	# Fetched, never hardcoded: Cloudflare adds ranges, and a stale list is
	# not a cosmetic problem -- an unlisted edge node is mistaken for the
	# client, and every visitor behind it shares one rate-limit bucket.
	CF_V4="$(curl -fsS --max-time 15 https://www.cloudflare.com/ips-v4 || true)"
	CF_V6="$(curl -fsS --max-time 15 https://www.cloudflare.com/ips-v6 || true)"
	if [[ -z "$CF_V4" || -z "$CF_V6" ]]; then
		# Fail closed. A partial trust list is worse than no change at all:
		# it silently mis-attributes client IPs instead of erroring.
		echo "FAIL: could not fetch Cloudflare's published IP ranges."
		echo "      Refusing to install a config with an incomplete trust list."
		exit 1
	fi

	MATCHED=0
	while read -r cidr; do
		[[ -n "$cidr" ]] || continue
		if in_cidr "$RESOLVED" "$cidr"; then MATCHED=1; break; fi
	done <<<"$CF_V4"
	if [[ "$MATCHED" -ne 1 ]]; then
		echo "FAIL: '$DOMAIN' resolves to $RESOLVED, which is not a Cloudflare address."
		echo "      The record is still grey-cloud (DNS only). Turn on the orange cloud"
		echo "      first -- installing an Origin CA certificate now would serve browsers"
		echo "      a certificate only Cloudflare trusts, and take the site down."
		exit 1
	fi
	echo "OK: $DOMAIN -> $RESOLVED (a Cloudflare address, as --cloudflare expects)"
else
	if [[ -n "$PUBLIC_IP" && "$RESOLVED" != "$PUBLIC_IP" ]]; then
		echo "FAIL: '$DOMAIN' resolves to $RESOLVED but this box is $PUBLIC_IP."
		echo "      Fix the A record first. Running anyway would burn Let's Encrypt failure quota."
		echo "      (If the record is orange-clouded on Cloudflare, you want --cloudflare.)"
		exit 1
	fi
	echo "OK: $DOMAIN -> $RESOLVED (matches this box)"
fi

# --- 3. Generate, validate, then install ----------------------------------
# Three substitutions carry the posture difference, so one Caddyfile describes
# both rather than two files drifting apart.
if [[ "$CLOUDFLARE" -eq 1 ]]; then
	# One long trusted_proxies line: Caddy takes any number of CIDRs, and
	# keeping it on one line avoids indentation-sensitive multi-line editing.
	CF_ALL="$(printf '%s\n%s' "$CF_V4" "$CF_V6" | tr '\n' ' ' | sed 's/  */ /g; s/ $//')"
	TRUSTED_PROXIES="trusted_proxies static private_ranges ${CF_ALL}\n\t\ttrusted_proxies_strict"
	PLATFORM_TLS="tls ${ORIGIN_CERT} ${ORIGIN_KEY}"
	TENANT_TLS="tls ${ORIGIN_CERT} ${ORIGIN_KEY}"
else
	TRUSTED_PROXIES="trusted_proxies static private_ranges"
	PLATFORM_TLS=""
	TENANT_TLS="tls {\n\t\ton_demand\n\t}"
fi

TMP="$(mktemp)"
trap 'rm -f "$TMP"' EXIT
sed -e "s|ERP_DOMAIN_PLACEHOLDER|${DOMAIN}|g" \
    -e "s|ERP_ACME_EMAIL_PLACEHOLDER|${EMAIL}|g" \
    -e "s|ERP_TRUSTED_PROXIES_PLACEHOLDER|${TRUSTED_PROXIES}|g" \
    -e "s|ERP_PLATFORM_TLS_PLACEHOLDER|${PLATFORM_TLS}|g" \
    -e "s|ERP_TENANT_TLS_PLACEHOLDER|${TENANT_TLS}|g" "$SRC" > "$TMP"

if grep -q "PLACEHOLDER" "$TMP"; then
	echo "FAIL: a placeholder survived substitution -- refusing to install."; exit 1
fi

mkdir -p /var/log/caddy

if ! caddy validate --config "$TMP" --adapter caddyfile >/dev/null 2>&1; then
	echo "FAIL: generated config is invalid. Details:"
	caddy validate --config "$TMP" --adapter caddyfile || true
	exit 1
fi
echo "OK: generated config validates"

# Chown AFTER validating, not before -- this ordering is load-bearing.
# `caddy validate` does not merely parse: it provisions every module, which
# includes OPENING the access log. This script runs as root, so validation
# leaves behind a root-owned 0600 /var/log/caddy/erp-access.log that the caddy
# user cannot write, and the reload below then dies with "permission denied"
# on a box where the directory, the config and DNS are all correct. Chowning
# the directory first (as this did until 2026-08-14) does not help, because
# the offending file does not exist yet at that point.
chown -R caddy:caddy /var/log/caddy 2>/dev/null || true

if [[ "$CLOUDFLARE" -eq 1 ]]; then
	# The private key is readable by the caddy user and nobody else.
	chown root:caddy "$ORIGIN_CERT" "$ORIGIN_KEY" 2>/dev/null || true
	chmod 0640 "$ORIGIN_KEY" 2>/dev/null || true
	chmod 0644 "$ORIGIN_CERT" 2>/dev/null || true
fi

if [[ -f "$DEST" ]]; then
	cp -a "$DEST" "${DEST}.bak.${STAMP}"
	echo "OK: backed up existing config to ${DEST}.bak.${STAMP}"
fi
install -m 0644 "$TMP" "$DEST"

# --- 4. The other half of the trust boundary ------------------------------
# Caddy's trusted_proxies and the app's TRUSTED_PROXY_CIDRS must list the same
# ranges. Doing it here, in the same command, is the only reliable way to keep
# them together -- the drift is silent when it happens.
if [[ "$CLOUDFLARE" -eq 1 ]]; then
	if [[ -f "$ERP_ENV" ]]; then
		CF_CSV="$(printf '%s\n%s' "$CF_V4" "$CF_V6" | grep -v '^$' | paste -sd, -)"
		cp -a "$ERP_ENV" "${ERP_ENV}.bak.${STAMP}"
		# Drop any previous line, then append the current one, so re-running
		# after Cloudflare publishes a new range updates rather than duplicates.
		sed -i '/^TRUSTED_PROXY_CIDRS=/d' "$ERP_ENV"
		echo "TRUSTED_PROXY_CIDRS=${CF_CSV}" >> "$ERP_ENV"
		echo "OK: TRUSTED_PROXY_CIDRS synced in $ERP_ENV (backup ${ERP_ENV}.bak.${STAMP})"
		systemctl restart erp && echo "OK: erp restarted to pick it up"
	else
		echo "WARN: $ERP_ENV not found -- set TRUSTED_PROXY_CIDRS by hand or every"
		echo "      visitor will share one per-IP rate-limit bucket."
	fi
fi

# --- 5. Reload, rolling back if Caddy refuses it ---------------------------
if ! systemctl reload caddy; then
	echo "FAIL: caddy reload failed -- rolling back."
	[[ -f "${DEST}.bak.${STAMP}" ]] && cp -a "${DEST}.bak.${STAMP}" "$DEST" && systemctl reload caddy || true
	exit 1
fi
echo "OK: caddy reloaded"

# --- 6. Confirm TLS actually came up --------------------------------------
# Certificate issuance is asynchronous; give ACME a moment before judging it.
# (In --cloudflare mode the certificate already exists, so this normally
# answers on the first attempt -- it is still worth asking, because it is the
# only end-to-end proof that the browser -> Cloudflare -> Caddy -> Go path
# works with the certificate that was just installed.)
echo -n "Waiting for a certificate"
for _ in $(seq 1 20); do
	sleep 3; echo -n "."
	if curl -fsS -o /dev/null --max-time 5 "https://${DOMAIN}/api/v1/health" 2>/dev/null; then
		echo; echo "OK: https://${DOMAIN}/api/v1/health responded over TLS"
		echo
		if [[ "$CLOUDFLARE" -eq 1 ]]; then
			echo "Remaining steps, in order:"
			echo "  1. Set Cloudflare SSL/TLS encryption mode to Full (strict)."
			echo "  2. Restrict the origin firewall (DigitalOcean cloud firewall, not"
			echo "     just ufw) so 80/443 accept only the ranges now listed in"
			echo "     TRUSTED_PROXY_CIDRS. Until then the WAF is bypassable by IP."
			echo "  3. Confirm a request shows the real client IP, not a Cloudflare"
			echo "     edge address, in /var/log/caddy/erp-access.log."
		else
			echo "Remaining steps, in order:"
			echo "  1. Set the browser origin allow-list, then restart the app:"
			echo "       echo 'CORS_ALLOWED_ORIGINS=https://${DOMAIN}' >> /etc/erp/erp.env"
			echo "       systemctl restart erp"
			echo "  2. For per-tenant hostnames, set TENANT_BASE_DOMAIN in /etc/erp/erp.env"
			echo "     and give each tenant a host_slug (Stage 44)."
			echo "  3. For a real edge WAF, put Cloudflare in front and re-run this"
			echo "     script with --cloudflare (go_live_decisions.md section 5)."
		fi
		exit 0
	fi
done
echo
echo "WARN: config installed and Caddy reloaded, but https://${DOMAIN}/api/v1/health"
echo "      did not answer within 60s. Certificate issuance can lag; check with:"
echo "        journalctl -u caddy -n 50 --no-pager"
exit 1
