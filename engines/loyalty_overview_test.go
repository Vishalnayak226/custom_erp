package engines

import (
	"custom_erp/db"
	"encoding/json"
	"strings"
	"testing"
	"time"
)

// Stage 55.17 - vouchers and campaigns in words: a voucher's state is the
// till's own verdict (usable / not yet valid / expired / used up / inactive),
// with whom and where it is for and what it took off paid bills; a campaign
// shows who it would pick today (the worker's own selection), its message as
// sent, and its results. Own throwaway tenant.
func TestStage5517LoyaltyOverview(t *testing.T) {
	db.InitDB(testConnStr())
	tenantID, schemaName := uniqueLifecycleTenant(t)
	defer dropLifecycleTenant(tenantID, schemaName)
	if _, err := ProvisionTenantSchema(tenantID, schemaName, "0.1.0-test"); err != nil {
		t.Fatalf("ProvisionTenantSchema: %v", err)
	}
	insert := func(id, doctype, status string, data map[string]interface{}) {
		t.Helper()
		raw, _ := json.Marshal(data)
		if _, err := db.DB.Exec("INSERT INTO "+schemaName+".documents (id, doctype, data, status, created_by) VALUES ($1, $2, $3, $4, 'system')", id, doctype, raw, status); err != nil {
			t.Fatalf("insert %s: %v", id, err)
		}
	}
	today := time.Now()
	day := func(d int) string { return today.AddDate(0, 0, d).Format("2006-01-02") }
	insert("CUST-OV", "Customer", "Active", map[string]interface{}{"code": "CUST-OV", "name": "Asha", "date_of_birth": today.AddDate(-30, 0, 0).Format("2006-01-02"), "consent_whatsapp": true})
	insert("CUST-OV2", "Customer", "Active", map[string]interface{}{"code": "CUST-OV2", "name": "Ravi", "date_of_birth": today.AddDate(-25, 0, 0).Format("2006-01-02")})
	insert("CUST-OLD", "Customer", "Active", map[string]interface{}{"code": "CUST-OLD", "name": "Old Buyer"})
	voucher := func(code string, extra map[string]interface{}) {
		data := map[string]interface{}{"code": code, "discount_type": "Flat", "discount_value": 100.0, "max_uses": 1.0, "used_count": 0.0, "status": "Active"}
		for k, v := range extra {
			data[k] = v
		}
		insert(code, "Voucher", "Active", data)
	}
	voucher("OV-OK", map[string]interface{}{"customer_id": "CUST-OV", "channel": "POS", "expiry_date": day(30), "used_count": 0.0})
	voucher("OV-SOON", map[string]interface{}{"valid_from": day(5), "discount_type": "Percentage", "discount_value": 10.0})
	voucher("OV-OLD", map[string]interface{}{"expiry_date": day(-1)})
	voucher("OV-USED", map[string]interface{}{"used_count": 1.0})
	voucher("OV-OFF", map[string]interface{}{"status": "Inactive"})
	insert("POS-OV-1", "POSCart", "Paid", map[string]interface{}{"customer_id": "CUST-OV", "amount_paid": 900.0,
		"applied_offers": []map[string]interface{}{{"offer_id": "VOUCHER:OV-USED", "offer_type": "Voucher", "discount": 100.0}, {"offer_id": "OFF-1", "offer_type": "Percentage Off", "discount": 50.0}}})
	if _, err := db.DB.Exec(`INSERT INTO ` + schemaName + `.documents (id, doctype, data, status, created_by, created_at)
		VALUES ('POS-OV-OLD', 'POSCart', '{"customer_id":"CUST-OLD","amount_paid":500}', 'Paid', 'system', NOW() - INTERVAL '200 days')`); err != nil {
		t.Fatal(err)
	}

	list, err := ListVoucherOverview(tenantID)
	if err != nil {
		t.Fatalf("ListVoucherOverview: %v", err)
	}
	by := map[string]VoucherOverview{}
	for _, v := range list {
		by[v.Code] = v
	}
	if v := by["OV-OK"]; !v.Usable || v.State != "Usable now" || v.For != "Asha" || v.Where != "At the till only" || v.Gives != "100 off the bill" {
		t.Errorf("usable voucher: %+v", v)
	}
	if v := by["OV-SOON"]; v.Usable || !strings.Contains(v.State, "can be used from "+day(5)) || v.Gives != "10% off the bill" || v.For != "Any customer" {
		t.Errorf("not-yet-valid voucher: %+v", v)
	}
	if v := by["OV-OLD"]; v.Usable || !strings.Contains(v.State, "expired on") {
		t.Errorf("expired voucher: %+v", v)
	}
	if v := by["OV-USED"]; v.Usable || !strings.Contains(v.State, "maximum uses") || v.DiscountGiven != 100 {
		t.Errorf("used-up voucher, with what it gave: %+v", v)
	}
	if v := by["OV-OFF"]; v.Usable || !strings.Contains(v.State, "not active") {
		t.Errorf("inactive voucher: %+v", v)
	}

	insert("CMP-BDAY", "Campaign", "Active", map[string]interface{}{"name": "Birthday treat", "trigger_type": "Birthday", "message_template": "Happy birthday {{customer_name}}!", "cost": 500.0, "channel": "WhatsApp"})
	insert("CMP-LAPSE", "Campaign", "Active", map[string]interface{}{"name": "Come back", "trigger_type": "Lapsed Customer", "lapsed_days": 90.0, "message_template": "We miss you", "channel": "SMS"})
	insert("CMP-NOCH", "Campaign", "Active", map[string]interface{}{"name": "No channel", "trigger_type": "Birthday", "message_template": "Hi"})
	camps, err := ListCampaignOverview(tenantID)
	if err != nil {
		t.Fatalf("ListCampaignOverview: %v", err)
	}
	cb := map[string]CampaignOverview{}
	for _, c := range camps {
		cb[c.ID] = c
	}
	// Two birthdays today; only Asha agreed to WhatsApp offers (55.17a).
	if c := cb["CMP-BDAY"]; c.PicksToday != 1 || c.NoConsent != 1 || c.MessagePreview != "Happy birthday Asha!" || c.Trigger != "On the customer's birthday" || c.Cost != 500 || c.Channel != "WhatsApp" {
		t.Errorf("birthday campaign: %+v", c)
	}
	// The customer last seen 200 days ago matches but never agreed to SMS.
	if c := cb["CMP-LAPSE"]; c.PicksToday != 0 || c.NoConsent != 1 || c.Trigger != "No purchase in 90 days" {
		t.Errorf("lapsed campaign: %+v", c)
	}
	if c := cb["CMP-NOCH"]; c.PicksToday != 0 || c.Note == "" {
		t.Errorf("a campaign without a channel picks no one and says so: %+v", c)
	}
	// The preview's picks are the worker's: after a run, what was sent matches.
	runCampaignsForSchema(schemaName)
	camps, _ = ListCampaignOverview(tenantID)
	for _, c := range camps {
		if c.SentTo != c.PicksToday {
			t.Errorf("%s: previewed %d, sent to %d", c.ID, c.PicksToday, c.SentTo)
		}
	}
}
