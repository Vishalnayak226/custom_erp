package engines

import (
	"custom_erp/db"
	"encoding/json"
	"testing"
	"time"
)

func TestCampaignTriggersAndROI(t *testing.T) {
	db.InitDB("postgres://postgres@localhost:5435/custom_erp?sslmode=disable")
	tenantID := "default"
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		t.Fatalf("schema: %v", err)
	}

	const birthdayCampaignID = "TEST-CAMPAIGN-BDAY"
	const lapsedCampaignID = "TEST-CAMPAIGN-LAPSED"
	const bdayCustID = "TEST-CAMPAIGN-CUST-BDAY"
	const lapsedCustID = "TEST-CAMPAIGN-CUST-LAPSED"

	cleanup := func() {
		db.DB.Exec("DELETE FROM " + schema + ".clevertap_event_log WHERE campaign_id IN ('" + birthdayCampaignID + "', '" + lapsedCampaignID + "')")
		db.DB.Exec("DELETE FROM " + schema + ".documents WHERE doctype = 'Campaign' AND id IN ('" + birthdayCampaignID + "', '" + lapsedCampaignID + "')")
		db.DB.Exec("DELETE FROM " + schema + ".documents WHERE doctype = 'Customer' AND id IN ('" + bdayCustID + "', '" + lapsedCustID + "')")
		db.DB.Exec("DELETE FROM " + schema + ".documents WHERE doctype = 'POSCart' AND id LIKE 'TEST-CAMPAIGN-CART-%'")
	}
	cleanup()
	defer cleanup()

	seedDoc := func(id, doctype string, data map[string]interface{}, status string) {
		data["id"] = id
		bytes, err := json.Marshal(data)
		if err != nil {
			t.Fatalf("marshal %s: %v", id, err)
		}
		if _, err := db.DB.Exec(
			"INSERT INTO "+schema+".documents (id, doctype, data, status, created_by) VALUES ($1, $2, $3, $4, 'system')",
			id, doctype, bytes, status); err != nil {
			t.Fatalf("seed %s: %v", id, err)
		}
	}

	t.Run("birthday campaign matches today's month-day and is idempotent per day", func(t *testing.T) {
		cleanup()
		todayMMDD := time.Now().Format("01-02")
		seedDoc(bdayCustID, "Customer", map[string]interface{}{
			"code": bdayCustID, "name": "Birthday Customer", "date_of_birth": "1990-" + todayMMDD,
		}, "Active")
		seedDoc(birthdayCampaignID, "Campaign", map[string]interface{}{
			"name": "Birthday Blast", "trigger_type": "Birthday", "message_template": "Happy Birthday {{customer_name}}!",
		}, "Active")

		runCampaignsForSchema(schema)

		var count int
		if err := db.DB.QueryRow("SELECT COUNT(*) FROM "+schema+".clevertap_event_log WHERE campaign_id=$1 AND customer_id=$2", birthdayCampaignID, bdayCustID).Scan(&count); err != nil {
			t.Fatalf("query event log: %v", err)
		}
		if count != 1 {
			t.Fatalf("expected exactly one birthday event logged, got %d", count)
		}
		var eventDataStr string
		if err := db.DB.QueryRow("SELECT event_data::text FROM "+schema+".clevertap_event_log WHERE campaign_id=$1 AND customer_id=$2", birthdayCampaignID, bdayCustID).Scan(&eventDataStr); err != nil {
			t.Fatalf("query event_data: %v", err)
		}
		var eventData map[string]interface{}
		json.Unmarshal([]byte(eventDataStr), &eventData)
		if eventData["message"] != "Happy Birthday Birthday Customer!" {
			t.Fatalf("expected rendered message with the customer's name, got %v", eventData["message"])
		}

		// A second run the same day must not send a duplicate.
		runCampaignsForSchema(schema)
		db.DB.QueryRow("SELECT COUNT(*) FROM "+schema+".clevertap_event_log WHERE campaign_id=$1 AND customer_id=$2", birthdayCampaignID, bdayCustID).Scan(&count)
		if count != 1 {
			t.Fatalf("expected still exactly one birthday event after a second same-day run, got %d", count)
		}
	})

	t.Run("lapsed-customer campaign matches a customer whose last Paid POSCart is older than lapsed_days", func(t *testing.T) {
		cleanup()
		staleCart := map[string]interface{}{"cart_number": "TEST-CAMPAIGN-CART-1", "customer_id": lapsedCustID, "amount_paid": 500, "location": "HO", "payment_mode": "Cash"}
		seedDoc("TEST-CAMPAIGN-CART-1", "POSCart", staleCart, "Paid")
		if _, err := db.DB.Exec("UPDATE "+schema+".documents SET created_at = CURRENT_TIMESTAMP - INTERVAL '100 days' WHERE id='TEST-CAMPAIGN-CART-1'"); err != nil {
			t.Fatalf("backdate cart: %v", err)
		}
		seedDoc(lapsedCampaignID, "Campaign", map[string]interface{}{
			"name": "Win Back", "trigger_type": "Lapsed Customer", "lapsed_days": 90, "message_template": "We miss you!",
		}, "Active")

		runCampaignsForSchema(schema)

		var count int
		if err := db.DB.QueryRow("SELECT COUNT(*) FROM "+schema+".clevertap_event_log WHERE campaign_id=$1 AND customer_id=$2", lapsedCampaignID, lapsedCustID).Scan(&count); err != nil {
			t.Fatalf("query event log: %v", err)
		}
		if count != 1 {
			t.Fatalf("expected exactly one lapsed-customer event logged, got %d", count)
		}
	})

	t.Run("recent customer does not match the lapsed-customer trigger", func(t *testing.T) {
		cleanup()
		recentCart := map[string]interface{}{"cart_number": "TEST-CAMPAIGN-CART-2", "customer_id": lapsedCustID, "amount_paid": 500, "location": "HO", "payment_mode": "Cash"}
		seedDoc("TEST-CAMPAIGN-CART-2", "POSCart", recentCart, "Paid")
		seedDoc(lapsedCampaignID, "Campaign", map[string]interface{}{
			"name": "Win Back", "trigger_type": "Lapsed Customer", "lapsed_days": 90, "message_template": "We miss you!",
		}, "Active")

		runCampaignsForSchema(schema)

		var count int
		db.DB.QueryRow("SELECT COUNT(*) FROM "+schema+".clevertap_event_log WHERE campaign_id=$1 AND customer_id=$2", lapsedCampaignID, lapsedCustID).Scan(&count)
		if count != 0 {
			t.Fatalf("expected no event for a recently-active customer, got %d", count)
		}
	})

	t.Run("campaign ROI attributes post-campaign Paid POSCart revenue from targeted customers", func(t *testing.T) {
		cleanup()
		seedDoc(birthdayCampaignID, "Campaign", map[string]interface{}{
			"name": "Birthday Blast", "trigger_type": "Birthday", "message_template": "Happy Birthday!", "cost": 100,
		}, "Active")
		if err := logCleverTapEventInSchema(schema, "Birthday Campaign", bdayCustID, map[string]interface{}{"campaign_id": birthdayCampaignID}); err != nil {
			t.Fatalf("seed event log: %v", err)
		}
		seedDoc("TEST-CAMPAIGN-CART-3", "POSCart", map[string]interface{}{
			"cart_number": "TEST-CAMPAIGN-CART-3", "customer_id": bdayCustID, "amount_paid": 750, "location": "HO", "payment_mode": "Cash",
		}, "Paid")

		rows, err := GetCampaignROIReport(tenantID)
		if err != nil {
			t.Fatalf("GetCampaignROIReport: %v", err)
		}
		found := false
		for _, r := range rows {
			if r.CampaignID == birthdayCampaignID {
				found = true
				if r.CustomersTargeted != 1 {
					t.Fatalf("expected customers_targeted=1, got %d", r.CustomersTargeted)
				}
				if r.AttributedRevenue != 750 {
					t.Fatalf("expected attributed_revenue=750, got %v", r.AttributedRevenue)
				}
				if r.ROI == nil || *r.ROI != 6.5 {
					t.Fatalf("expected roi=(750-100)/100=6.5, got %v", r.ROI)
				}
			}
		}
		if !found {
			t.Fatalf("expected campaign %s in the ROI report", birthdayCampaignID)
		}
	})
}
