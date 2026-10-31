package engines

import (
	"context"
	"custom_erp/db"
	"errors"
	"fmt"
	"strings"
	"time"
)

// 49.8.5: Disconnect removes a connector's credential and the state that
// would otherwise keep calling the provider with it - the queued and leased
// background jobs of that channel / provider, and the Shopify webhook
// registration stamp (the scheduled sync and the webhook receiver already
// stop on their own once the credential is gone: RunShopifyScheduled and
// the inbound verifier both read it first). Mappings, links and templates
// (shopify_links, unicommerce_order_mapping, channel settings) stay, so a
// reconnect picks up where it left off. No token is cached in memory by any
// connector (each call reads the credential), so nothing else to clear.
//
// 49.8.5 (rest): a Shopify channel's webhooks are removed at the shop first,
// while the token still exists (deregisterShopifyWebhooks) - through the
// connector transport, so the outside-calls gate and the outbound guard
// apply. A failed removal never blocks the disconnect: it is reported as the
// result's Warning (remove them in the Shopify admin; until then each
// delivery is refused, the secret being gone).

// shopifyDeregisterTimeout bounds the whole removal, so a shop that does not
// answer cannot hold the disconnect for each call's own timeout in turn.
const shopifyDeregisterTimeout = 15 * time.Second

// DisconnectResult is what a disconnect cleaned, and what it could not.
type DisconnectResult struct {
	Cleaned []string `json:"cleaned"`
	Warning string   `json:"warning,omitempty"`
}

// DisconnectConnectorCredential removes one credential. kind is "channel"
// (a channel code, or any channel_credentials code such as courier:delhivery),
// "courier" (a provider), "pinelabs" (a terminal id), "clevertap" (an account
// id) or "unicommerce" (a store code). It returns what was cleaned.
func DisconnectConnectorCredential(tenantID, kind, code, actor string) ([]string, error) {
	res, err := DisconnectConnectorCredentialContext(context.Background(), tenantID, kind, code, actor)
	return res.Cleaned, err
}

// DisconnectConnectorCredentialContext is DisconnectConnectorCredential for a
// caller with a request context, reporting a cleanup that failed as Warning.
func DisconnectConnectorCredentialContext(ctx context.Context, tenantID, kind, code, actor string) (DisconnectResult, error) {
	kind, code = strings.ToLower(strings.TrimSpace(kind)), strings.TrimSpace(code)
	if code == "" {
		return DisconnectResult{}, errors.New("code is required")
	}
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return DisconnectResult{}, err
	}
	table, keyColumn, jobKeys := "", "", []string{}
	switch kind {
	case "channel":
		table, keyColumn, jobKeys = "channel_credentials", "channel_code", []string{"channel", "channel_code"}
	case "courier":
		if _, err := courierAdapter(code); err != nil {
			return DisconnectResult{}, err
		}
		table, keyColumn, jobKeys = "channel_credentials", "channel_code", []string{"provider", "courier"}
		code = courierCredentialCode(code)
	case "pinelabs":
		table, keyColumn, jobKeys = "pinelabs_credentials", "terminal_id", []string{"terminal_id"}
	case "clevertap":
		table, keyColumn, jobKeys = "clevertap_credentials", "account_id", []string{"account_id"}
	case "unicommerce":
		table, keyColumn, jobKeys = "unicommerce_credentials", "store_code", []string{"store_code"}
	default:
		return DisconnectResult{}, errors.New("kind must be channel, courier, pinelabs, clevertap or unicommerce")
	}
	// A Shopify channel's webhooks go first, while its token can still ask.
	removedHooks, warning := "", ""
	if kind == "channel" {
		if ok, _ := HasChannelCredential(tenantID, code); ok {
			removedHooks, warning = deregisterShopifyWebhooks(ctx, tenantID, code)
		}
	}
	res, err := db.DB.Exec(fmt.Sprintf(`DELETE FROM %s.%s WHERE %s = $1`, schema, table, keyColumn), code)
	if err != nil {
		return DisconnectResult{}, err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return DisconnectResult{}, fmt.Errorf("no %s credential is saved for %q", kind, code)
	}
	cleaned := []string{"credential"}
	if removedHooks != "" {
		cleaned = append(cleaned, removedHooks)
	}

	// Queued / leased jobs that would call the provider with it.
	jobKey := strings.TrimPrefix(code, "courier:")
	where := make([]string, 0, len(jobKeys))
	for _, k := range jobKeys {
		where = append(where, fmt.Sprintf(`payload->>'%s' = $1`, k))
	}
	if r, err := db.DB.Exec(fmt.Sprintf(`UPDATE %s.async_jobs SET status = 'Cancelled', updated_at = CURRENT_TIMESTAMP, completed_at = CURRENT_TIMESTAMP,
		last_error = 'cancelled: connector disconnected'
		WHERE status IN ('Pending', 'Leased') AND (%s)`, schema, strings.Join(where, " OR ")), jobKey); err == nil {
		if n, _ := r.RowsAffected(); n > 0 {
			cleaned = append(cleaned, fmt.Sprintf("%d queued job(s)", n))
		}
	}
	// A Shopify channel's webhook registration stamp: the receiver refuses
	// without the secret anyway, and the next Connect registers again.
	if kind == "channel" {
		if r, err := db.DB.Exec(fmt.Sprintf(`UPDATE %s.documents SET data = (data #- '{shopify,webhooks_registered_at}') #- '{shopify,webhook_url}', updated_at = CURRENT_TIMESTAMP
			WHERE doctype = 'Channel' AND id = $1 AND data->'shopify' ? 'webhooks_registered_at'`, schema), code); err == nil {
			if n, _ := r.RowsAffected(); n > 0 {
				cleaned = append(cleaned, "webhook registration")
			}
		}
	}
	detail := fmt.Sprintf("kind=%s code=%s cleaned=%s", kind, code, strings.Join(cleaned, ", "))
	if warning != "" {
		detail += " warning=" + warning
	}
	LogAuditEvent(tenantID, actor, "CONNECTOR_DISCONNECTED", "SUCCESS", detail)
	return DisconnectResult{Cleaned: cleaned, Warning: warning}, nil
}

// deregisterShopifyWebhooks asks the shop to stop sending this ERP webhooks:
// the subscriptions at the address Register pointed them at. Nothing to do
// for a channel that is not Shopify or never registered. done says what was
// removed; warning, why nothing (or not all) was.
func deregisterShopifyWebhooks(ctx context.Context, tenantID, channel string) (done, warning string) {
	_, s, err := LoadShopifyChannel(tenantID, channel)
	if err != nil || strings.TrimSpace(s.WebhookURL) == "" {
		return "", ""
	}
	notRemoved := func(reason string) string {
		if len(reason) > 200 { // a provider's answer, bounded for the screen and the audit row
			reason = reason[:200] + "..."
		}
		return fmt.Sprintf("Shopify's webhooks were not removed (%s) - remove them in the Shopify admin (Settings > Notifications > Webhooks); until then each one is refused, as the secret is gone", reason)
	}
	cred, err := getChannelCredential(tenantID, channel)
	if err != nil {
		return "", notRemoved(err.Error())
	}
	if ctx == nil {
		ctx = context.Background()
	}
	ctx, cancel := context.WithTimeout(WithOutsideCallsTenant(ctx, tenantID), shopifyDeregisterTimeout)
	defer cancel()
	removed, err := shopifyConn().DeregisterWebhooks(ctx, cred, s.WebhookURL)
	if err != nil {
		if removed > 0 {
			return fmt.Sprintf("%d Shopify webhook(s) removed", removed), notRemoved(err.Error())
		}
		return "", notRemoved(err.Error())
	}
	return fmt.Sprintf("%d Shopify webhook(s) removed", removed), ""
}
