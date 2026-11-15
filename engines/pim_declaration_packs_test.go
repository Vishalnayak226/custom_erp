package engines

import (
	"custom_erp/db"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"
)

// 47.13.6 (ask 185): a declaration pack counts only once Approved and in date,
// for its category and the categories under it, on its channel; approving it
// adds the pack's attributes the client lacks; the publish check and the
// Declaration gaps report then name the required fields an item still lacks.
func TestDeclarationPacksGatePublish(t *testing.T) {
	db.InitDB(testConnStr())

	// The save check needs no database.
	good := map[string]interface{}{"pack": "packaged-goods-in", "category": "CAT-FOOD", "effective_from": "2026-11-01", "reviewer": "A. Reviewer (LM consultant)", "evidence": "Opinion letter 2026-11-01"}
	wantRefusal := func(data map[string]interface{}, field string) {
		t.Helper()
		var ve *ValidationError
		if err := ValidateDeclarationPack(data); !errors.As(err, &ve) || ve.Code != "PIM-9362" || ve.SubFor != field {
			t.Fatalf("want PIM-9362 on %s, got %v", field, err)
		}
	}
	with := func(k string, v interface{}) map[string]interface{} {
		out := map[string]interface{}{}
		for key, val := range good {
			out[key] = val
		}
		out[k] = v
		return out
	}
	wantRefusal(with("pack", "made-up"), "pack")
	wantRefusal(with("reviewer", "  "), "reviewer")
	wantRefusal(with("evidence", ""), "reviewer")
	wantRefusal(with("effective_to", "2026-10-31"), "effective_to")
	if err := ValidateDeclarationPack(with("effective_to", "2026-11-01")); err != nil {
		t.Fatalf("a last day equal to the effective date is fine: %v", err)
	}

	var registered bool
	if err := db.DB.QueryRow(`SELECT EXISTS(SELECT 1 FROM tenant_default.doctype_meta WHERE name = 'DeclarationPack')`).Scan(&registered); err != nil || !registered {
		t.Skip("migrations_stage47_13_6_declaration_packs.sql is not applied to this database")
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
	setData := func(id, key, value string) {
		t.Helper()
		if _, err := db.DB.Exec("UPDATE "+schema+".documents SET data = data || jsonb_build_object($2::text, $3::text) WHERE id = $1", id, key, value); err != nil {
			t.Fatal(err)
		}
	}
	failures := func(item, channel string) []string {
		t.Helper()
		data, _, err := fetchItemDoc(tenantID, item)
		if err != nil {
			t.Fatalf("fetchItemDoc %s: %v", item, err)
		}
		out, err := declarationPublishFailures(tenantID, item, channel, data)
		if err != nil {
			t.Fatalf("declarationPublishFailures: %v", err)
		}
		return out
	}

	insert("CAT-FOOD", "Category", "Active", map[string]interface{}{"name": "Food"})
	insert("CAT-SNACK", "Category", "Active", map[string]interface{}{"name": "Snacks", "parent": "CAT-FOOD"})
	insert("CAT-TOY", "Category", "Active", map[string]interface{}{"name": "Toys"})
	insert("CH-WEB", "Channel", "Active", map[string]interface{}{"name": "Web", "default_locale": "en"})
	insert("CH-MKT", "Channel", "Active", map[string]interface{}{"name": "Marketplace", "default_locale": "en"})
	// ITM-1 names its category by name, ITM-3 by id; ITM-2 is outside the tree.
	insert("ITM-1", "Item", "Active", map[string]interface{}{"name": "Salted chips", "category": "Snacks", "mrp": 50, "country_of_origin": "India"})
	insert("ITM-2", "Item", "Active", map[string]interface{}{"name": "Toy car", "category": "CAT-TOY"})
	insert("ITM-3", "Item", "Active", map[string]interface{}{"name": "Peanuts", "category": "CAT-SNACK"})
	// The client already has a generic-name attribute under its own id.
	insert("ATTR-GEN", "ProductAttributeDef", "Active", map[string]interface{}{"code": "lm_generic_name", "label": "Generic name", "value_type": "Data", "status": "Active"})

	if f := failures("ITM-1", "CH-WEB"); len(f) != 0 {
		t.Fatalf("no pack is on yet, got %v", f)
	}

	pack := with("channel", "CH-WEB")
	pack["effective_from"] = BusinessToday()
	pack["status"] = "Draft"
	insert("DPK-1", DeclarationPackDoctype, "Draft", pack)
	if f := failures("ITM-1", "CH-WEB"); len(f) != 0 {
		t.Fatalf("a Draft pack must not count, got %v", f)
	}
	if err := SubmitForApproval(tenantID, DeclarationPackDoctype, "DPK-1", "steward", "Store Manager"); err != nil {
		t.Fatalf("SubmitForApproval: %v", err)
	}
	if f := failures("ITM-1", "CH-WEB"); len(f) != 0 {
		t.Fatalf("a Pending pack must not count, got %v", f)
	}
	if err := DecideApproval(tenantID, DeclarationPackDoctype, "DPK-1", "admin", RoleSuperAdmin, "", "Approved", ""); err != nil {
		t.Fatalf("DecideApproval: %v", err)
	}

	// Approving added the pack's attributes, but not one the client already has.
	var valueType, options string
	if err := db.DB.QueryRow("SELECT data->>'value_type', COALESCE(data->>'value_options','') FROM "+schema+".documents WHERE doctype = 'ProductAttributeDef' AND id = 'LM_NET_QTY_UNIT'").Scan(&valueType, &options); err != nil {
		t.Fatalf("LM_NET_QTY_UNIT was not added: %v", err)
	}
	if valueType != "Select" || !strings.Contains(options, "kg") {
		t.Fatalf("LM_NET_QTY_UNIT: type %q options %q", valueType, options)
	}
	var dupes int
	db.DB.QueryRow("SELECT COUNT(*) FROM " + schema + ".documents WHERE doctype = 'ProductAttributeDef' AND id = 'LM_GENERIC_NAME'").Scan(&dupes)
	if dupes != 0 {
		t.Fatal("the client's own lm_generic_name attribute should have been reused, not duplicated")
	}

	f := failures("ITM-1", "CH-WEB")
	if len(f) != 1 || !strings.HasPrefix(f[0], "declarations (Packaged goods") {
		t.Fatalf("want one declarations line, got %v", f)
	}
	for _, want := range []string{"Manufacturer", "Net quantity unit", "Unit sale price", "Label artwork reference", "Common or generic name"} {
		if !strings.Contains(f[0], want) {
			t.Fatalf("%q missing from %q", want, f[0])
		}
	}
	for _, filled := range []string{"Country of origin", "MRP", "Best before"} {
		if strings.Contains(f[0], filled) {
			t.Fatalf("%q is filled or optional but listed: %q", filled, f[0])
		}
	}
	if f := failures("ITM-1", "CH-MKT"); len(f) != 0 {
		t.Fatalf("the pack is on for CH-WEB only, got %v on CH-MKT", f)
	}
	if f := failures("ITM-2", "CH-WEB"); len(f) != 0 {
		t.Fatalf("toys are outside the food tree, got %v", f)
	}

	// The publish check carries the line.
	readiness, err := CheckPublishReadiness(tenantID, "ITM-3", "CH-WEB")
	if err != nil {
		t.Fatalf("CheckPublishReadiness: %v", err)
	}
	found := false
	for _, m := range readiness.MissingFields {
		found = found || strings.HasPrefix(m, "declarations (")
	}
	if !found || readiness.Ready {
		t.Fatalf("ITM-3 should not be ready, missing %v", readiness.MissingFields)
	}

	// Filling every required field clears ITM-1; the client's own attribute counts.
	for i, code := range []string{"LM_MAKER", "LM_NET_QTY", "LM_NET_QTY_UNIT", "LM_MFG_DATE", "LM_UNIT_SALE_PRICE", "LM_CONSUMER_CARE", "LM_ARTWORK_REF", "ATTR-GEN"} {
		insert("PAV-"+string(rune('A'+i)), "ProductAttributeValue", "Active", map[string]interface{}{"item": "ITM-1", "attribute": code, "value": "x"})
	}
	if f := failures("ITM-1", "CH-WEB"); len(f) != 0 {
		t.Fatalf("every required field is filled, got %v", f)
	}

	// The report lists ITM-3 and not ITM-1; a category filter outside the tree is empty.
	rows, err := ListDeclarationGaps(tenantID, "")
	if err != nil {
		t.Fatalf("ListDeclarationGaps: %v", err)
	}
	if len(rows) != 1 || rows[0].ItemCode != "ITM-3" || rows[0].Channel != "CH-WEB" || rows[0].Record != "DPK-1" {
		t.Fatalf("want one ITM-3 row from DPK-1 on CH-WEB, got %+v", rows)
	}
	if rows, _ := ListDeclarationGaps(tenantID, "CAT-TOY"); len(rows) != 0 {
		t.Fatalf("the Toys filter should be empty, got %+v", rows)
	}
	if rows, _ := ListDeclarationGaps(tenantID, "Snacks"); len(rows) != 1 {
		t.Fatalf("the Snacks filter (by name) should keep ITM-3, got %+v", rows)
	}

	// Out of date no longer counts: last day yesterday, or starting tomorrow.
	day := func(offset int) string {
		return time.Now().In(businessLocation()).AddDate(0, 0, offset).Format("2006-01-02")
	}
	setData("DPK-1", "effective_to", day(-1))
	if f := failures("ITM-3", "CH-WEB"); len(f) != 0 {
		t.Fatalf("a pack past its last day must not count, got %v", f)
	}
	setData("DPK-1", "effective_to", "")
	setData("DPK-1", "effective_from", day(1))
	if f := failures("ITM-3", "CH-WEB"); len(f) != 0 {
		t.Fatalf("a pack starting tomorrow must not count yet, got %v", f)
	}
	setData("DPK-1", "effective_from", day(0))
	if f := failures("ITM-3", "CH-WEB"); len(f) != 1 {
		t.Fatalf("back in date, want one line, got %v", f)
	}
}
