package engines

import (
	"custom_erp/db"
	"encoding/json"
	"strings"
	"testing"
	"time"
)

// Stage 55.17a - marketing consent: no consent, no SMS / WhatsApp. A save that
// changes a Customer's consent stamps when and by whom (and one that does not
// keeps the stamp); a consent withdrawal clears both consents, after which no
// campaign picks the customer. Own throwaway tenant.
func TestStage5517aMarketingConsent(t *testing.T) {
	// The stamp, on the generic save's payload.
	payload := map[string]interface{}{"name": "Asha", "consent_whatsapp": true}
	StampMarketingConsent("Customer", nil, payload, "cashier1")
	if payload["consent_source"] != "Recorded by cashier1" || payload["consent_updated_at"] == nil {
		t.Errorf("a new customer who agreed is stamped: %+v", payload)
	}
	same := map[string]interface{}{"name": "Asha", "consent_whatsapp": true, "consent_source": "Recorded by cashier1", "consent_updated_at": "2026-01-01 10:00"}
	StampMarketingConsent("Customer", map[string]interface{}{"consent_whatsapp": true}, same, "manager1")
	if same["consent_source"] != "Recorded by cashier1" || same["consent_updated_at"] != "2026-01-01 10:00" {
		t.Errorf("a save that leaves consent alone keeps the stamp: %+v", same)
	}
	none := map[string]interface{}{"name": "Walk-in"}
	StampMarketingConsent("Customer", nil, none, "cashier1")
	if _, stamped := none["consent_source"]; stamped {
		t.Errorf("no consent given, nothing to stamp: %+v", none)
	}
	item := map[string]interface{}{"consent_sms": true}
	StampMarketingConsent("Item", nil, item, "x")
	if _, stamped := item["consent_source"]; stamped {
		t.Errorf("only a Customer is stamped")
	}
	if !HasMarketingConsent(map[string]interface{}{"consent_sms": "Yes"}, "sms") || HasMarketingConsent(map[string]interface{}{"consent_sms": true}, "WhatsApp") || HasMarketingConsent(map[string]interface{}{"consent_sms": true}, "") {
		t.Errorf("consent is per channel, CSV 'Yes' counts, no channel is never consented")
	}

	db.InitDB(testConnStr())
	tenantID, schemaName := uniqueLifecycleTenant(t)
	defer dropLifecycleTenant(tenantID, schemaName)
	if _, err := ProvisionTenantSchema(tenantID, schemaName, "0.1.0-test"); err != nil {
		t.Fatalf("ProvisionTenantSchema: %v", err)
	}
	raw, _ := json.Marshal(map[string]interface{}{"code": "CUST-CN", "name": "Asha", "date_of_birth": time.Now().AddDate(-30, 0, 0).Format("2006-01-02"), "consent_whatsapp": true, "consent_sms": true})
	if _, err := db.DB.Exec("INSERT INTO "+schemaName+".documents (id, doctype, data, status, created_by) VALUES ('CUST-CN', 'Customer', $1, 'Active', 'system')", raw); err != nil {
		t.Fatal(err)
	}
	if picks, _ := birthdayCampaignMatches(schemaName, "WhatsApp", true); len(picks) != 1 {
		t.Fatalf("a consenting birthday customer is picked, got %d", len(picks))
	}
	if err := WithdrawMarketingConsent(tenantID, "CUST-CN", "Consent withdrawal DSR-1"); err != nil {
		t.Fatalf("WithdrawMarketingConsent: %v", err)
	}
	for _, ch := range []string{"WhatsApp", "SMS"} {
		if picks, _ := birthdayCampaignMatches(schemaName, ch, true); len(picks) != 0 {
			t.Errorf("after withdrawal no %s campaign picks them, got %d", ch, len(picks))
		}
	}
	var source string
	_ = db.DB.QueryRow("SELECT data->>'consent_source' FROM " + schemaName + ".documents WHERE id = 'CUST-CN'").Scan(&source)
	if !strings.Contains(source, "DSR-1") {
		t.Errorf("the withdrawal is recorded on the customer: %q", source)
	}
	if err := WithdrawMarketingConsent(tenantID, "NOPE", "x"); err == nil {
		t.Errorf("an unknown customer is an error, not a silent success")
	}
}
