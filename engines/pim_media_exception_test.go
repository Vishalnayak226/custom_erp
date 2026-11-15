package engines

import (
	"custom_erp/db"
	"encoding/json"
	"strings"
	"testing"
	"time"
)

// 55.11 (ask 179): an Approved, in-date MediaRuleException lowers a channel's
// Min Images rule for one item on one channel; nothing else does.
func TestStage5511MediaRuleExceptionWaivesMinImages(t *testing.T) {
	db.InitDB(testConnStr())
	var registered bool
	if err := db.DB.QueryRow(`SELECT EXISTS(SELECT 1 FROM tenant_default.doctype_meta WHERE name = 'MediaRuleException')`).Scan(&registered); err != nil || !registered {
		t.Skip("migrations_stage55_11_media_rule_exception.sql is not applied to this database")
	}
	tenantID, schema := uniqueLifecycleTenant(t)
	defer dropLifecycleTenant(tenantID, schema)
	if _, err := ProvisionTenantSchema(tenantID, schema, "0.1.0-test"); err != nil {
		t.Fatalf("ProvisionTenantSchema: %v", err)
	}
	insert := func(id, doctype, status string, data map[string]interface{}) {
		t.Helper()
		raw, _ := json.Marshal(data)
		if _, err := db.DB.Exec("INSERT INTO "+schema+".documents (id, doctype, data, status, created_by) VALUES ($1, $2, $3, $4, 'system')", id, doctype, raw, status); err != nil {
			t.Fatalf("insert %s: %v", id, err)
		}
	}
	setStatus := func(id, status string) {
		t.Helper()
		if _, err := db.DB.Exec("UPDATE "+schema+".documents SET status = $2 WHERE id = $1", id, status); err != nil {
			t.Fatal(err)
		}
	}
	check := func(item, channel string) []string {
		t.Helper()
		failures, err := evaluateChannelValidationRules(tenantID, item, channel, "en")
		if err != nil {
			t.Fatalf("evaluateChannelValidationRules: %v", err)
		}
		return failures
	}
	minImageFailure := func(failures []string) bool {
		for _, f := range failures {
			if strings.HasPrefix(f, "Min Images") {
				return true
			}
		}
		return false
	}

	insert("CVR-1", "ChannelValidationRule", "Active", map[string]interface{}{"channel": "CH-WEB", "rule_type": "Min Images", "rule_value": "3"})
	insert("CVR-2", "ChannelValidationRule", "Active", map[string]interface{}{"channel": "CH-MKT", "rule_type": "Min Images", "rule_value": "3"})
	insert("PM-1", "ProductMedia", "Active", map[string]interface{}{"item": "ITM-1", "media_role": "Main Image", "file_type": "image/jpeg"})

	if !minImageFailure(check("ITM-1", "CH-WEB")) {
		t.Fatal("one image against a 3-image rule should fail with no exception")
	}

	insert("MRE-1", "MediaRuleException", "Draft", map[string]interface{}{"item_code": "ITM-1", "channel": "CH-WEB", "min_images": 1, "reason": "Single-shot product"})
	if !minImageFailure(check("ITM-1", "CH-WEB")) {
		t.Fatal("a Draft exception must not waive the rule")
	}
	setStatus("MRE-1", "Pending Approval")
	if !minImageFailure(check("ITM-1", "CH-WEB")) {
		t.Fatal("a Pending exception must not waive the rule")
	}
	setStatus("MRE-1", "Approved")
	if minImageFailure(check("ITM-1", "CH-WEB")) {
		t.Fatal("an Approved exception for 1 image should let a 1-image item pass")
	}
	if !minImageFailure(check("ITM-1", "CH-MKT")) {
		t.Fatal("the exception is for CH-WEB only; CH-MKT keeps its rule")
	}

	// An exception asking for more images than the item has does not help.
	insert("MRE-2", "MediaRuleException", "Approved", map[string]interface{}{"item_code": "ITM-1", "channel": "CH-MKT", "min_images": 2, "reason": "Two-angle only"})
	if !minImageFailure(check("ITM-1", "CH-MKT")) {
		t.Fatal("an exception for 2 images should not pass a 1-image item")
	}

	// Expiry: yesterday no longer counts, today still does.
	yesterday := time.Now().In(businessLocation()).AddDate(0, 0, -1).Format("2006-01-02")
	if _, err := db.DB.Exec("UPDATE "+schema+".documents SET data = data || jsonb_build_object('valid_until', $2::text) WHERE id = $1", "MRE-1", yesterday); err != nil {
		t.Fatal(err)
	}
	if !minImageFailure(check("ITM-1", "CH-WEB")) {
		t.Fatal("an expired exception must not waive the rule")
	}
	if _, err := db.DB.Exec("UPDATE "+schema+".documents SET data = data || jsonb_build_object('valid_until', $2::text) WHERE id = $1", "MRE-1", BusinessToday()); err != nil {
		t.Fatal(err)
	}
	if minImageFailure(check("ITM-1", "CH-WEB")) {
		t.Fatal("an exception whose last day is today still counts")
	}

	// A deleted or rejected exception does not count.
	if _, err := db.DB.Exec("UPDATE "+schema+".documents SET deleted_at = NOW() WHERE id = $1", "MRE-1"); err != nil {
		t.Fatal(err)
	}
	if !minImageFailure(check("ITM-1", "CH-WEB")) {
		t.Fatal("a deleted exception must not waive the rule")
	}
	insert("MRE-3", "MediaRuleException", "Rejected", map[string]interface{}{"item_code": "ITM-1", "channel": "CH-WEB", "min_images": 0, "reason": "x"})
	if !minImageFailure(check("ITM-1", "CH-WEB")) {
		t.Fatal("a Rejected exception must not waive the rule")
	}
}
