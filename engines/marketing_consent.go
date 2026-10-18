package engines

import (
	"custom_erp/db"
	"fmt"
	"strings"
	"time"
)

// Stage 55.17a - marketing consent. The rule (user, 2026-10-18): no consent,
// no SMS / WhatsApp. A Customer carries one consent per channel -
// consent_sms and consent_whatsapp, ticked only when the customer has agreed
// to offers on it - and when and by whom it was last recorded
// (consent_updated_at, consent_source). A campaign names its channel, and
// sends only to customers who agreed to that channel; a campaign without a
// channel sends nothing. A customer with no consent recorded has not agreed.
//
// Transactional messages (order and return updates, invoices, the redemption
// one-time code) are not offers and are not gated by this.

// Marketing channels a campaign can be sent by.
const (
	ConsentChannelSMS      = "SMS"
	ConsentChannelWhatsApp = "WhatsApp"
)

// consentField is the Customer field holding consent for a channel; blank
// for anything else, which is never consented.
func consentField(channel string) string {
	switch {
	case strings.EqualFold(strings.TrimSpace(channel), ConsentChannelSMS):
		return "consent_sms"
	case strings.EqualFold(strings.TrimSpace(channel), ConsentChannelWhatsApp):
		return "consent_whatsapp"
	}
	return ""
}

// marketingConsentSQL is the SQL test that the Customer row under alias has
// agreed to offers on channel. A tick from the form is a JSON true; a CSV
// import may write "Yes" / "1"; both count. An unknown channel is FALSE.
func marketingConsentSQL(alias, channel string) string {
	f := consentField(channel)
	if f == "" {
		return "FALSE"
	}
	return fmt.Sprintf(`LOWER(COALESCE(%s.data->>'%s', '')) IN ('true', '1', 'yes', 'on')`, alias, f)
}

// HasMarketingConsent reports whether a Customer record agreed to offers on
// channel.
func HasMarketingConsent(customer map[string]interface{}, channel string) bool {
	f := consentField(channel)
	return f != "" && truthy(customer[f])
}

// StampMarketingConsent records when and by whom a Customer's consent changed,
// on the save that changes it (called from the generic document save, so the
// Customer form and the till's quick-add are both covered). A save that
// leaves both consents as they were keeps the previous stamp.
func StampMarketingConsent(doctype string, prior, payload map[string]interface{}, actor string) {
	if doctype != "Customer" || payload == nil {
		return
	}
	changed := false
	for _, f := range []string{"consent_sms", "consent_whatsapp"} {
		if truthy(payload[f]) != truthy(prior[f]) {
			changed = true
		}
	}
	if !changed {
		return
	}
	payload["consent_updated_at"] = time.Now().Format("2006-01-02 15:04")
	payload["consent_source"] = "Recorded by " + firstNonEmpty(strings.TrimSpace(actor), "system")
}

// WithdrawMarketingConsent clears both consents on a Customer - a data
// subject's consent withdrawal (privacy_rights.go) - and stamps why.
func WithdrawMarketingConsent(tenantID, customerID, source string) error {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return err
	}
	res, err := db.DB.Exec(fmt.Sprintf(`UPDATE %s.documents
		SET data = data || jsonb_build_object('consent_sms', false, 'consent_whatsapp', false, 'consent_updated_at', $2::text, 'consent_source', $3::text),
		    updated_at = CURRENT_TIMESTAMP
		WHERE doctype = 'Customer' AND id = $1`, schema), customerID, time.Now().Format("2006-01-02 15:04"), source)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return fmt.Errorf("customer %s not found", customerID)
	}
	return nil
}
