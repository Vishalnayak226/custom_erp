package engines

import (
	"custom_erp/db"
	"encoding/json"
	"strings"
	"testing"
)

func TestPIMPublishReadinessRequiresProductFamily(t *testing.T) {
	db.InitDB(testConnStr())
	schema, err := db.GetTenantSchema("default")
	if err != nil {
		t.Fatal(err)
	}
	const item = "PIM-FAMILY-GATE-ITEM"
	const channel = "PIM-FAMILY-GATE-CHANNEL"
	ids := []string{item, item + "::en", item + "::profile", channel, "PIM-FAMILY-GATE-MAP"}
	cleanup := func() {
		for _, id := range ids {
			_, _ = db.DB.Exec("DELETE FROM "+schema+".documents WHERE id = $1", id)
		}
	}
	cleanup()
	defer cleanup()
	insert := func(id, doctype, status string, data map[string]interface{}) {
		t.Helper()
		encoded, err := json.Marshal(data)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := db.DB.Exec("INSERT INTO "+schema+".documents (id, doctype, data, status, created_by) VALUES ($1,$2,$3,$4,'system')", id, doctype, encoded, status); err != nil {
			t.Fatal(err)
		}
	}
	insert(item, "Item", "Active", map[string]interface{}{"code": item, "name": "Family-less item", "barcode": "1234567890128", "category": "Rings", "hsn_code": "7113", "gst_rate": 3})
	insert(item+"::en", "ProductContent", "Approved", map[string]interface{}{"product_id": item, "language": "en", "title": "Approved title"})
	insert(channel, "Channel", "Active", map[string]interface{}{"code": channel, "name": "Test channel", "default_locale": "en"})
	insert("PIM-FAMILY-GATE-MAP", "ChannelCategoryMap", "Active", map[string]interface{}{"channel": channel, "erp_category": "Rings", "channel_category": "Jewellery > Rings"})

	completeness, err := CalculateCompleteness("default", item, "en", channel)
	if err != nil {
		t.Fatal(err)
	}
	if completeness.Score >= 100 || len(completeness.MissingFields) != 1 || completeness.MissingFields[0] != "Product Family" {
		t.Fatalf("family-less item must name its missing family, got score %.1f and %v", completeness.Score, completeness.MissingFields)
	}
	readiness, err := CheckPublishReadiness("default", item, channel)
	if err != nil {
		t.Fatal(err)
	}
	if readiness.Ready || !strings.Contains(strings.Join(readiness.MissingFields, ", "), "Product Family") {
		t.Fatalf("family-less item must not publish even with approved content and mapped category: %+v", readiness)
	}
}

// TestPIMPublishChannelByCode (319.10): the Workbench sends a Channel's code,
// and an older Channel's id is a UUID. Readiness and the platform lookup must
// find it by code as well as by id; a deleted Channel is not found by either.
func TestPIMPublishChannelByCode(t *testing.T) {
	db.InitDB(testConnStr())
	schema, err := db.GetTenantSchema("default")
	if err != nil {
		t.Fatal(err)
	}
	const item = "PIM-CHREF-ITEM"
	ids := []string{item, "PIM-CHREF-UUID-0001", "PIM-CHREF-UUID-0002"}
	cleanup := func() {
		for _, id := range ids {
			_, _ = db.DB.Exec("DELETE FROM "+schema+".documents WHERE id = $1", id)
		}
	}
	cleanup()
	defer cleanup()
	insert := func(id, doctype string, data map[string]interface{}) {
		t.Helper()
		encoded, _ := json.Marshal(data)
		if _, err := db.DB.Exec("INSERT INTO "+schema+".documents (id, doctype, data, status, created_by) VALUES ($1,$2,$3,'Active','system')", id, doctype, encoded); err != nil {
			t.Fatal(err)
		}
	}
	insert(item, "Item", map[string]interface{}{"code": item, "name": "Channel ref item"})
	insert("PIM-CHREF-UUID-0001", "Channel", map[string]interface{}{"code": "PIM-CHREF-CODE", "name": "Legacy channel", "platform": "Myntra", "default_locale": "en"})
	insert("PIM-CHREF-UUID-0002", "Channel", map[string]interface{}{"code": "PIM-CHREF-GONE", "name": "Deleted channel", "default_locale": "en"})
	if _, err := db.DB.Exec("UPDATE " + schema + ".documents SET deleted_at = NOW() WHERE id = 'PIM-CHREF-UUID-0002'"); err != nil {
		t.Fatal(err)
	}

	for _, ref := range []string{"PIM-CHREF-UUID-0001", "PIM-CHREF-CODE"} {
		if _, err := CheckPublishReadiness("default", item, ref); err != nil {
			t.Errorf("CheckPublishReadiness(%q): %v, want the channel found", ref, err)
		}
		if platform, err := fetchChannelPlatform("default", ref); err != nil || platform != "Myntra" {
			t.Errorf("fetchChannelPlatform(%q) = %q, %v, want Myntra", ref, platform, err)
		}
	}
	if _, err := CheckPublishReadiness("default", item, "PIM-CHREF-GONE"); err == nil || !strings.Contains(err.Error(), "channel not found") {
		t.Errorf("a deleted channel must not be found, got %v", err)
	}
}
