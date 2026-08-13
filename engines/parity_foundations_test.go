package engines

import (
	"custom_erp/db"
	"encoding/json"
	"math"
	"strings"
	"testing"
)

func TestParityFoundationValidation(t *testing.T) {
	t.Run("dynamic group needs a supported filter", func(t *testing.T) {
		valid := map[string]interface{}{
			"group_type":                "Dynamic",
			"filter_completeness_below": 80,
		}
		if err := ValidatePIMProductGroupDocument("default", valid); err != nil {
			t.Fatalf("valid dynamic group rejected: %v", err)
		}
		invalid := map[string]interface{}{
			"group_type":                "Dynamic",
			"filter_completeness_below": 101,
		}
		if err := ValidatePIMProductGroupDocument("default", invalid); err == nil {
			t.Fatal("out-of-range dynamic filter was accepted")
		}
	})

	t.Run("currency normalizes and validates ISO shape", func(t *testing.T) {
		currency := map[string]interface{}{"code": " usd ", "decimal_places": 2.0}
		if err := ValidateCurrencyDocument(currency); err != nil {
			t.Fatalf("valid currency rejected: %v", err)
		}
		if currency["code"] != "USD" {
			t.Fatalf("currency code not normalized: %#v", currency["code"])
		}
		if err := ValidateCurrencyDocument(map[string]interface{}{"code": "US", "decimal_places": 2}); err == nil {
			t.Fatal("two-letter currency code was accepted")
		}
	})

	t.Run("exchange rate rejects invalid windows and unaudited imports", func(t *testing.T) {
		base := map[string]interface{}{
			"from_currency": "USD", "to_currency": "INR", "rate": 83.25,
			"effective_from": "2026-01-01", "effective_to": "2026-12-31",
			"source": "Manual",
		}
		if err := ValidateExchangeRateDocument(base); err != nil {
			t.Fatalf("valid exchange rate rejected: %v", err)
		}
		badWindow := map[string]interface{}{
			"from_currency": "USD", "to_currency": "INR", "rate": 83.25,
			"effective_from": "2026-12-31", "effective_to": "2026-01-01", "source": "Manual",
		}
		if err := ValidateExchangeRateDocument(badWindow); err == nil {
			t.Fatal("backwards effective window was accepted")
		}
		imported := map[string]interface{}{
			"from_currency": "USD", "to_currency": "INR", "rate": 83.25,
			"effective_from": "2026-01-01", "source": "Imported",
		}
		if err := ValidateExchangeRateDocument(imported); err == nil {
			t.Fatal("Imported rate without source_reference was accepted")
		}
	})
}

func TestResolvePIMProductGroups(t *testing.T) {
	db.InitDB(testConnStr())
	schema, err := db.GetTenantSchema("default")
	if err != nil {
		t.Fatalf("schema: %v", err)
	}
	ids := []string{
		"TEST-PG-ITEM-A", "TEST-PG-ITEM-B", "TEST-PG-DYNAMIC", "TEST-PG-STATIC",
		"TEST-PG-ITEM-A::profile", "TEST-PG-ITEM-B::profile",
	}
	var groupMetaAlreadyExists bool
	if err := db.DB.QueryRow("SELECT EXISTS(SELECT 1 FROM " + schema + ".doctype_meta WHERE name = 'PIMProductGroup')").Scan(&groupMetaAlreadyExists); err != nil {
		t.Fatalf("check Product Group metadata: %v", err)
	}
	if !groupMetaAlreadyExists {
		if _, err := db.DB.Exec("INSERT INTO " + schema + ".doctype_meta (name, module, module_key, document_type) VALUES ('PIMProductGroup','PIM','pim','Master')"); err != nil {
			t.Fatalf("seed Product Group metadata: %v", err)
		}
	}
	cleanup := func() {
		for _, id := range ids {
			_, _ = db.DB.Exec("DELETE FROM "+schema+".documents WHERE id = $1", id)
		}
		if !groupMetaAlreadyExists {
			_, _ = db.DB.Exec("DELETE FROM " + schema + ".doctype_meta WHERE name = 'PIMProductGroup'")
		}
	}
	for _, id := range ids {
		_, _ = db.DB.Exec("DELETE FROM "+schema+".documents WHERE id = $1", id)
	}
	defer cleanup()

	insert := func(id, doctype, status string, data map[string]interface{}) {
		t.Helper()
		encoded, marshalErr := json.Marshal(data)
		if marshalErr != nil {
			t.Fatalf("marshal %s: %v", id, marshalErr)
		}
		if _, execErr := db.DB.Exec("INSERT INTO "+schema+".documents (id, doctype, data, status, created_by) VALUES ($1,$2,$3,$4,'system')", id, doctype, encoded, status); execErr != nil {
			t.Fatalf("insert %s: %v", id, execErr)
		}
	}
	insert("TEST-PG-ITEM-A", "Item", "Active", map[string]interface{}{
		"code": "TEST-PG-ITEM-A", "name": "Group A", "family": "FAMILY-A",
	})
	insert("TEST-PG-ITEM-B", "Item", "Active", map[string]interface{}{
		"code": "TEST-PG-ITEM-B", "name": "Group B", "family": "FAMILY-B",
	})
	insert("TEST-PG-DYNAMIC", "PIMProductGroup", "Active", map[string]interface{}{
		"code": "TEST-GROUP-CODE", "name": "Incomplete family A", "group_type": "Dynamic",
		"filter_family": "FAMILY-A", "filter_completeness_below": 100,
	})
	insert("TEST-PG-STATIC", "PIMProductGroup", "Active", map[string]interface{}{
		"name": "Hand picked", "group_type": "Static",
		"members": `[{"item_code":"TEST-PG-ITEM-B"}]`,
	})

	dynamic, err := ResolvePIMProductGroup("default", "TEST-PG-DYNAMIC")
	if err != nil {
		t.Fatalf("resolve dynamic group: %v", err)
	}
	if dynamic.MemberCount != 1 || dynamic.Members[0].ItemCode != "TEST-PG-ITEM-A" {
		t.Fatalf("dynamic members = %#v, want only family A fixture", dynamic.Members)
	}
	reportRows, err := runPIMProductGroupReadinessReport("default", map[string]string{"group_id": "test-group-code"})
	if err != nil {
		t.Fatalf("run Product Group readiness report: %v", err)
	}
	if len(reportRows) != 1 || reportRows[0]["item_code"] != "TEST-PG-ITEM-A" {
		t.Fatalf("readiness report rows = %#v, want the resolved dynamic member", reportRows)
	}
	static, err := ResolvePIMProductGroup("default", "TEST-PG-STATIC")
	if err != nil {
		t.Fatalf("resolve static group: %v", err)
	}
	if static.MemberCount != 1 || static.Members[0].ItemCode != "TEST-PG-ITEM-B" {
		t.Fatalf("static members = %#v, want only hand-picked fixture", static.Members)
	}

	// Stage 36.1.3: the two production consumers of the resolver seam.
	targets, err := ResolvePIMBulkTargetIDs("default", "Item", "TEST-PG-STATIC", nil)
	if err != nil {
		t.Fatalf("resolve bulk targets from group: %v", err)
	}
	if len(targets) != 1 || targets[0] != "TEST-PG-ITEM-B" {
		t.Fatalf("bulk targets = %#v, want the static group's single member", targets)
	}
	if _, err := ResolvePIMBulkTargetIDs("default", "Item", "TEST-PG-STATIC", []string{"TEST-PG-ITEM-A"}); err == nil {
		t.Fatal("a group plus an explicit selection was accepted; the target set must be unambiguous")
	}
	if _, err := ResolvePIMBulkTargetIDs("default", "ProductContent", "TEST-PG-STATIC", nil); err == nil {
		t.Fatal("a product group was allowed to drive a bulk edit of a non-Item doctype")
	}
	passthrough, err := ResolvePIMBulkTargetIDs("default", "Item", "", []string{"TEST-PG-ITEM-A"})
	if err != nil || len(passthrough) != 1 || passthrough[0] != "TEST-PG-ITEM-A" {
		t.Fatalf("explicit selection without a group = %#v, %v; want it passed through untouched", passthrough, err)
	}

	groupID, csvBytes, err := ExportPIMProductGroupCSV("default", "TEST-PG-DYNAMIC")
	if err != nil {
		t.Fatalf("export product group: %v", err)
	}
	if groupID != "TEST-PG-DYNAMIC" {
		t.Fatalf("export group id = %q, want the canonical document id", groupID)
	}
	exported := string(csvBytes)
	if !strings.HasPrefix(exported, "group_id,group_name,item_code,") {
		t.Fatalf("export header = %q, want the group export column set", strings.SplitN(exported, "\n", 2)[0])
	}
	if !strings.Contains(exported, "TEST-PG-ITEM-A") {
		t.Fatalf("export did not contain the resolved member:\n%s", exported)
	}
	if strings.Contains(exported, "TEST-PG-ITEM-B") {
		t.Fatalf("export leaked a product outside the group:\n%s", exported)
	}
}

func TestResolveExchangeRateEffectiveDatingAndInverse(t *testing.T) {
	db.InitDB(testConnStr())
	schema, err := db.GetTenantSchema("default")
	if err != nil {
		t.Fatalf("schema: %v", err)
	}
	ids := []string{"TEST-CUR-USD", "TEST-CUR-EUR", "TEST-FX-OLD", "TEST-FX-NEW"}
	createdMeta := []string{}
	for _, meta := range []struct{ name, module string }{{"Currency", "Finance"}, {"ExchangeRate", "Finance"}} {
		var exists bool
		if err := db.DB.QueryRow("SELECT EXISTS(SELECT 1 FROM "+schema+".doctype_meta WHERE name = $1)", meta.name).Scan(&exists); err != nil {
			t.Fatalf("check %s metadata: %v", meta.name, err)
		}
		if !exists {
			if _, err := db.DB.Exec("INSERT INTO "+schema+".doctype_meta (name, module, module_key, document_type) VALUES ($1,$2,'finance','Master')", meta.name, meta.module); err != nil {
				t.Fatalf("seed %s metadata: %v", meta.name, err)
			}
			createdMeta = append(createdMeta, meta.name)
		}
	}
	cleanup := func() {
		for _, id := range ids {
			_, _ = db.DB.Exec("DELETE FROM "+schema+".documents WHERE id = $1", id)
		}
		for _, name := range createdMeta {
			_, _ = db.DB.Exec("DELETE FROM "+schema+".doctype_meta WHERE name = $1", name)
		}
	}
	for _, id := range ids {
		_, _ = db.DB.Exec("DELETE FROM "+schema+".documents WHERE id = $1", id)
	}
	defer cleanup()

	insert := func(id, doctype string, data map[string]interface{}) {
		t.Helper()
		encoded, _ := json.Marshal(data)
		if _, execErr := db.DB.Exec("INSERT INTO "+schema+".documents (id, doctype, data, status, created_by) VALUES ($1,$2,$3,'Active','system')", id, doctype, encoded); execErr != nil {
			t.Fatalf("insert %s: %v", id, execErr)
		}
	}
	insert("TEST-CUR-USD", "Currency", map[string]interface{}{"code": "USD", "name": "US Dollar", "decimal_places": 2})
	insert("TEST-CUR-EUR", "Currency", map[string]interface{}{"code": "EUR", "name": "Euro", "decimal_places": 2})
	insert("TEST-FX-OLD", "ExchangeRate", map[string]interface{}{
		"from_currency": "TEST-CUR-USD", "to_currency": "TEST-CUR-EUR", "rate": 0.90,
		"rate_type": "Spot", "effective_from": "2026-01-01", "effective_to": "2026-06-30", "source": "Manual",
	})
	insert("TEST-FX-NEW", "ExchangeRate", map[string]interface{}{
		"from_currency": "TEST-CUR-USD", "to_currency": "TEST-CUR-EUR", "rate": 0.80,
		"rate_type": "Spot", "effective_from": "2026-07-01", "source": "Imported", "source_reference": "ECB-2026-07",
	})

	oldRate, err := ResolveExchangeRate("default", "USD", "EUR", "2026-06-15", "Spot")
	if err != nil {
		t.Fatalf("resolve old direct rate: %v", err)
	}
	if oldRate.RateDocumentID != "TEST-FX-OLD" || math.Abs(oldRate.Rate-0.90) > 0.000001 {
		t.Fatalf("old rate = %#v", oldRate)
	}
	newRate, err := ResolveExchangeRate("default", "TEST-CUR-USD", "TEST-CUR-EUR", "2026-08-11", "")
	if err != nil {
		t.Fatalf("resolve new direct rate: %v", err)
	}
	if newRate.RateDocumentID != "TEST-FX-NEW" || math.Abs(newRate.Rate-0.80) > 0.000001 {
		t.Fatalf("new rate = %#v", newRate)
	}
	inverse, err := ResolveExchangeRate("default", "EUR", "USD", "2026-08-11", "Spot")
	if err != nil {
		t.Fatalf("resolve inverse rate: %v", err)
	}
	if !inverse.Inverted || math.Abs(inverse.Rate-1.25) > 0.000001 {
		t.Fatalf("inverse rate = %#v, want 1.25 inverted", inverse)
	}
	if _, err := ResolveExchangeRate("default", "EUR", "USD", "2025-12-31", "Spot"); err == nil {
		t.Fatal("missing historical rate was accepted")
	}
}
