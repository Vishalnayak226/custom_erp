package engines

import (
	"custom_erp/db"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

// Stage 146.2.5 - a blocked member neither earns nor redeems (the balance is
// kept); customer tags listed in loyalty.never_earn_tags never earn; an offer
// can target a customer tag. Nobody is blocked and no tag is listed by default.

// loyaltyCustomerFacts is what the earn rules read off a Customer record.
type loyaltyCustomerFacts struct {
	Blocked     bool
	BlockReason string
	Tags        map[string]bool // lower-cased
	TagList     []string
}

func loyaltyFactsFromData(data map[string]interface{}) loyaltyCustomerFacts {
	f := loyaltyCustomerFacts{Tags: map[string]bool{}}
	f.Blocked = strings.EqualFold(strField(data, "loyalty_blocked"), "Yes")
	f.BlockReason = strings.TrimSpace(strField(data, "loyalty_block_reason"))
	for _, t := range crmSplitTags(strField(data, "tags")) {
		f.Tags[strings.ToLower(t)] = true
		f.TagList = append(f.TagList, t)
	}
	return f
}

func loyaltyCustomerFactsFor(schema, customerID string) loyaltyCustomerFacts {
	var raw []byte
	if err := db.DB.QueryRow(fmt.Sprintf(`SELECT data FROM %s.documents WHERE doctype = 'Customer' AND id = $1`, schema), customerID).Scan(&raw); err != nil {
		return loyaltyCustomerFacts{Tags: map[string]bool{}}
	}
	var data map[string]interface{}
	_ = json.Unmarshal(raw, &data)
	return loyaltyFactsFromData(data)
}

// loyaltyNeverEarnTags is the loyalty.never_earn_tags list, lower-cased.
func loyaltyNeverEarnTags(tenantID string) []string {
	var out []string
	for _, t := range crmSplitTags(GetSettingString(tenantID, "loyalty.never_earn_tags")) {
		out = append(out, strings.ToLower(t))
	}
	return out
}

// loyaltyEarnBlockReason is why this customer earns nothing, or "" when they
// earn: a blocked member (the stored reason) or a tag that never earns.
func loyaltyEarnBlockReason(tenantID string, f loyaltyCustomerFacts) string {
	if f.Blocked {
		if f.BlockReason == "" {
			return "Blocked"
		}
		return f.BlockReason
	}
	for _, t := range loyaltyNeverEarnTags(tenantID) {
		if f.Tags[t] {
			return "Tag " + t + " never earns"
		}
	}
	return ""
}

// guardLoyaltyMemberTx is the one check at the top of the redeem path: a
// blocked member cannot burn points. The balance is untouched.
func guardLoyaltyMemberTx(tx *sql.Tx, schema, customerID string) error {
	var blocked, reason sql.NullString
	err := tx.QueryRow(fmt.Sprintf(`SELECT data->>'loyalty_blocked', data->>'loyalty_block_reason' FROM %s.documents WHERE doctype = 'Customer' AND id = $1`, schema), customerID).Scan(&blocked, &reason)
	if err != nil || !strings.EqualFold(blocked.String, "Yes") {
		return nil
	}
	return &ValidationError{Code: "CUSTOM-0691", Message: "this customer is blocked from loyalty points" + loyaltyReasonSuffix(reason.String)}
}

func loyaltyReasonSuffix(reason string) string {
	if reason = strings.TrimSpace(reason); reason != "" {
		return ": " + reason
	}
	return ""
}

// offersForCustomerTags drops the offers aimed at a customer tag this customer
// does not carry. An offer with no tag is for everyone.
func offersForCustomerTags(rules []offerRule, tenantID, customerID string) []offerRule {
	tagged := false
	for _, r := range rules {
		if r.customerTag != "" {
			tagged = true
			break
		}
	}
	if !tagged {
		return rules
	}
	var tags map[string]bool
	if schema, err := db.GetTenantSchema(tenantID); err == nil && customerID != "" {
		tags = loyaltyCustomerFactsFor(schema, customerID).Tags
	}
	out := make([]offerRule, 0, len(rules))
	for _, r := range rules {
		if r.customerTag == "" || tags[strings.ToLower(r.customerTag)] {
			out = append(out, r)
		}
	}
	return out
}

// validateLoyaltyBlockFields: blocking a member needs a reason.
func validateLoyaltyBlockFields(payload map[string]interface{}) error {
	if strings.EqualFold(strField(payload, "loyalty_blocked"), "Yes") && strings.TrimSpace(strField(payload, "loyalty_block_reason")) == "" {
		return &ValidationError{Code: "CUSTOM-0692", SubFor: "loyalty_block_reason", Message: "say why this customer is blocked from loyalty points"}
	}
	return nil
}

// StampLoyaltyBlock records who blocked a customer and when, on the Customer
// itself, the moment the block is switched on; switching it off clears the
// stamp (the document's audit trail keeps the history). Called from the
// document save path before the record is written.
func StampLoyaltyBlock(tenantID, docID, actor, doctype string, payload map[string]interface{}) {
	if doctype != "Customer" {
		return
	}
	if _, present := payload["loyalty_blocked"]; !present {
		return
	}
	if !strings.EqualFold(strField(payload, "loyalty_blocked"), "Yes") {
		delete(payload, "loyalty_blocked_by")
		delete(payload, "loyalty_blocked_at")
		return
	}
	if schema, err := db.GetTenantSchema(tenantID); err == nil && docID != "" {
		var raw []byte
		if db.DB.QueryRow(fmt.Sprintf(`SELECT data FROM %s.documents WHERE doctype = 'Customer' AND id = $1`, schema), docID).Scan(&raw) == nil {
			var stored map[string]interface{}
			if json.Unmarshal(raw, &stored) == nil && strings.EqualFold(strField(stored, "loyalty_blocked"), "Yes") {
				payload["loyalty_blocked_by"], payload["loyalty_blocked_at"] = stored["loyalty_blocked_by"], stored["loyalty_blocked_at"]
				return // already blocked: who and when stay as first set
			}
		}
	}
	payload["loyalty_blocked_by"] = actor
	payload["loyalty_blocked_at"] = time.Now().In(businessLocation()).Format("2006-01-02 15:04")
}
