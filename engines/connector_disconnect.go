package engines

import (
	"custom_erp/db"
	"errors"
	"fmt"
	"strings"
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

// DisconnectConnectorCredential removes one credential. kind is "channel"
// (a channel code, or any channel_credentials code such as courier:delhivery),
// "courier" (a provider), "pinelabs" (a terminal id), "clevertap" (an account
// id) or "unicommerce" (a store code). It returns what was cleaned.
func DisconnectConnectorCredential(tenantID, kind, code, actor string) ([]string, error) {
	kind, code = strings.ToLower(strings.TrimSpace(kind)), strings.TrimSpace(code)
	if code == "" {
		return nil, errors.New("code is required")
	}
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return nil, err
	}
	table, keyColumn, jobKeys := "", "", []string{}
	switch kind {
	case "channel":
		table, keyColumn, jobKeys = "channel_credentials", "channel_code", []string{"channel", "channel_code"}
	case "courier":
		if _, err := courierAdapter(code); err != nil {
			return nil, err
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
		return nil, errors.New("kind must be channel, courier, pinelabs, clevertap or unicommerce")
	}
	res, err := db.DB.Exec(fmt.Sprintf(`DELETE FROM %s.%s WHERE %s = $1`, schema, table, keyColumn), code)
	if err != nil {
		return nil, err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return nil, fmt.Errorf("no %s credential is saved for %q", kind, code)
	}
	cleaned := []string{"credential"}

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
	LogAuditEvent(tenantID, actor, "CONNECTOR_DISCONNECTED", "SUCCESS", fmt.Sprintf("kind=%s code=%s cleaned=%s", kind, code, strings.Join(cleaned, ", ")))
	return cleaned, nil
}
