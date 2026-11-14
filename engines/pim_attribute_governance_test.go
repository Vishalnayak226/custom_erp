package engines

import (
	"context"
	"strings"
	"testing"

	"custom_erp/db"
)

// 168.13 Multi-select, 168.14 units, 168.15 regulated values.

func govFixture(t *testing.T) *creditFixture {
	f := newCreditFixture(t)
	f.seed("PUR", "ProductAttributeDef", "Active", map[string]interface{}{"code": "PUR", "label": "Purity", "value_type": "Data", "regulated": "Yes"})
	f.seed("COL", "ProductAttributeDef", "Active", map[string]interface{}{"code": "COL", "label": "Colour", "value_type": "Data"})
	f.seed("WT", "ProductAttributeDef", "Active", map[string]interface{}{"code": "WT", "label": "Weight", "value_type": "Number", "uom": "g"})
	f.seed("TAGS", "ProductAttributeDef", "Active", map[string]interface{}{"code": "TAGS", "label": "Tags", "value_type": "Multi-select", "value_options": "Red,Green,Blue"})
	f.seed("IT-G", "Item", "Active", map[string]interface{}{"code": "IT-G", "name": "Ring"})
	f.seed("IT-G::PUR", "ProductAttributeValue", "Active", map[string]interface{}{"item": "IT-G", "attribute": "PUR", "value": "18k"})
	f.seed("IT-G::COL", "ProductAttributeValue", "Active", map[string]interface{}{"item": "IT-G", "attribute": "COL", "value": "Red"})
	return f
}

func stored(f *creditFixture, attr string) string {
	return f.field("ProductAttributeValue", "IT-G::"+attr, "value")
}

func change(f *creditFixture, attr, value, user, role string) (bool, error) {
	prior := map[string]interface{}{"item": "IT-G", "attribute": attr, "value": stored(f, attr)}
	payload := map[string]interface{}{"item": "IT-G", "attribute": attr, "value": value}
	held, err := HoldRegulatedAttributeChange(f.tenantID, prior, payload, user, role)
	if held && payload["value"] != prior["value"] {
		f.t.Fatalf("a held change must put the stored value back, got %v", payload["value"])
	}
	return held, err
}

func pendingChange(t *testing.T, f *creditFixture) string {
	t.Helper()
	var id string
	if err := db.DB.QueryRow("SELECT id FROM " + f.schema + ".documents WHERE doctype = 'AttributeValueChange' ORDER BY created_at DESC LIMIT 1").Scan(&id); err != nil {
		t.Fatalf("no change filed: %v", err)
	}
	return id
}

func TestRegulatedChangeNotHeldWhenSettingOffOrNotRegulatedOrFirst(t *testing.T) {
	f := govFixture(t)
	if held, err := change(f, "PUR", "22k", "maker1", "Store Manager"); held || err != nil {
		t.Fatalf("setting off: %v %v", held, err)
	}
	if err := SetSetting(f.tenantID, SettingHoldRegulatedAttributeChanges, "true", "test"); err != nil {
		t.Fatal(err)
	}
	if held, _ := change(f, "COL", "Green", "maker1", "Store Manager"); held {
		t.Fatal("a non-regulated attribute must not be held")
	}
	held, err := HoldRegulatedAttributeChange(f.tenantID, nil, map[string]interface{}{"item": "IT-G", "attribute": "PUR", "value": "24k"}, "maker1", "Store Manager")
	if held || err != nil {
		t.Fatalf("a first value must not be held: %v %v", held, err)
	}
	if held, _ := change(f, "PUR", "18k", "maker1", "Store Manager"); held {
		t.Fatal("an unchanged value must not be held")
	}
}

func TestRegulatedChangeHeldApprovedAppliesRejectedLeavesOld(t *testing.T) {
	f := govFixture(t)
	if err := SetSetting(f.tenantID, SettingHoldRegulatedAttributeChanges, "true", "test"); err != nil {
		t.Fatal(err)
	}
	held, err := change(f, "PUR", "22k", "maker1", "Store Manager")
	if err != nil || !held {
		t.Fatalf("held=%v err=%v", held, err)
	}
	if v := stored(f, "PUR"); v != "18k" {
		t.Fatalf("old value must stay while waiting: %q", v)
	}
	id := pendingChange(t, f)
	if st := f.field("AttributeValueChange", id, "new_value"); st != "22k" {
		t.Fatalf("request new_value = %q", st)
	}
	if err := DecideApproval(f.tenantID, AttributeValueChangeDoctype, id, "checker1", "Store Manager", "", "Approved", ""); err != nil {
		t.Fatalf("approve: %v", err)
	}
	if v := stored(f, "PUR"); v != "22k" {
		t.Fatalf("approved change must apply, got %q", v)
	}
	// applied once: approving again changes nothing, and a later edit is not overwritten by a replay
	if err := applyAttributeValueChange(f.tenantID, id, "checker1"); err != nil {
		t.Fatal(err)
	}
	if _, err := db.DB.Exec("UPDATE " + f.schema + ".documents SET data = jsonb_set(data, '{value}', '\"24k\"') WHERE id = 'IT-G::PUR'"); err != nil {
		t.Fatal(err)
	}
	if err := applyAttributeValueChange(f.tenantID, id, "checker1"); err != nil || stored(f, "PUR") != "24k" {
		t.Fatalf("replay must not re-apply: %v %q", err, stored(f, "PUR"))
	}

	// rejected
	if held, err := change(f, "PUR", "999", "maker1", "Store Manager"); err != nil || !held {
		t.Fatalf("second: %v %v", held, err)
	}
	id2 := pendingChange(t, f)
	if err := DecideApproval(f.tenantID, AttributeValueChangeDoctype, id2, "checker1", "Store Manager", "", "Rejected", "wrong"); err != nil {
		t.Fatalf("reject: %v", err)
	}
	if v := stored(f, "PUR"); v != "24k" {
		t.Fatalf("rejected change must leave the old value, got %q", v)
	}
}

func TestRegulatedChangeSelfApprovalAndWrongRole(t *testing.T) {
	f := govFixture(t)
	if err := SetSetting(f.tenantID, SettingHoldRegulatedAttributeChanges, "true", "test"); err != nil {
		t.Fatal(err)
	}
	withSelfApproval(t, f.tenantID, SelfApprovalNobody)
	if held, err := change(f, "PUR", "22k", "maker1", "Store Manager"); err != nil || !held {
		t.Fatalf("%v %v", held, err)
	}
	id := pendingChange(t, f)
	if err := DecideApproval(f.tenantID, AttributeValueChangeDoctype, id, "maker1", "Store Manager", "", "Approved", ""); err == nil {
		t.Fatal("the maker must not approve their own change when self approval is nobody")
	}
	if err := DecideApproval(f.tenantID, AttributeValueChangeDoctype, id, "cashier9", "Cashier", "", "Approved", ""); err == nil {
		t.Fatal("a Cashier must not approve")
	}
	if v := stored(f, "PUR"); v != "18k" {
		t.Fatalf("still old: %q", v)
	}
	withSelfApproval(t, f.tenantID, SelfApprovalAnyone)
	if err := DecideApproval(f.tenantID, AttributeValueChangeDoctype, id, "maker1", "Store Manager", "", "Approved", ""); err != nil {
		t.Fatalf("self approval anyone: %v", err)
	}
	if v := stored(f, "PUR"); v != "22k" {
		t.Fatalf("applied: %q", v)
	}
}

func TestRegulatedSheetImportAndDeleteRefuse(t *testing.T) {
	f := govFixture(t)
	if err := SetSetting(f.tenantID, SettingHoldRegulatedAttributeChanges, "true", "test"); err != nil {
		t.Fatal(err)
	}
	err := RegulatedHoldRefusal(f.tenantID, "IT-G", "PUR", "", "", "24k")
	if ve, ok := err.(*ValidationError); !ok || ve.Code != "PIM-9306" {
		t.Fatalf("sheet: %v", err)
	}
	if err := RegulatedHoldRefusal(f.tenantID, "IT-G", "PUR", "", "", "18k"); err != nil {
		t.Fatalf("same value: %v", err)
	}
	if err := RegulatedHoldRefusal(f.tenantID, "IT-G", "COL", "", "", "Blue"); err != nil {
		t.Fatalf("not regulated: %v", err)
	}
	if err := CheckAttributeValueDelete(f.tenantID, "IT-G::PUR"); err == nil {
		t.Fatal("deleting a regulated value must be refused")
	}
	if err := CheckAttributeValueDelete(f.tenantID, "IT-G::COL"); err != nil {
		t.Fatalf("delete non-regulated: %v", err)
	}
}

func TestUnitAttributeCheckAndExportHeader(t *testing.T) {
	f := govFixture(t)
	if _, err := CheckAttributeValue(f.tenantID, "WT", "12 g"); err != nil {
		t.Fatalf("setting off keeps the old behaviour: %v", err)
	}
	if err := SetSetting(f.tenantID, SettingCheckUnitAttributeValues, "true", "test"); err != nil {
		t.Fatal(err)
	}
	_, err := CheckAttributeValue(f.tenantID, "WT", "12 g")
	if ve, ok := err.(*ValidationError); !ok || ve.Code != "PIM-9305" {
		t.Fatalf("unit typed with the number: %v", err)
	}
	if _, err := CheckAttributeValue(f.tenantID, "WT", "12.5"); err != nil {
		t.Fatalf("plain number: %v", err)
	}
	f.seed("WT2", "ProductAttributeDef", "Active", map[string]interface{}{"code": "WT2", "label": "No unit", "value_type": "Number"})
	if _, err := CheckAttributeValue(f.tenantID, "WT2", "1"); err != nil {
		t.Fatalf("no unit behaves as before: %v", err)
	}
	if h := exportHeaderWithUnit(f.schema, "WT", "Weight"); h != "Weight (g)" {
		t.Fatalf("header %q", h)
	}
	if h := exportHeaderWithUnit(f.schema, "WT2", "No unit"); h != "No unit" {
		t.Fatalf("header %q", h)
	}
	if h := exportHeaderWithUnit(f.schema, "COL", "Colour"); h != "Colour" {
		t.Fatalf("header %q", h)
	}
}

func TestMultiSelectLibraryApplyAndCSVRoundTrip(t *testing.T) {
	f := newCreditFixture(t)
	res, err := ApplyAttributeLibrary(f.tenantID, "tester", []AttributeLibraryPick{{ID: "FNB_ALLERGEN_CONTENT", UsedFor: "Both"}})
	if err != nil {
		t.Fatalf("apply: %v %+v", err, res)
	}
	pimType := f.field("ProductAttributeDef", "FNB_ALLERGEN_CONTENT", "value_type")
	if pimType != "Multi-select" {
		t.Fatalf("PIM copy of a MultiEnum must be Multi-select, got %q; result %+v", pimType, res)
	}
	opts := f.field("ProductAttributeDef", "FNB_ALLERGEN_CONTENT", "value_options")
	if erp := f.field("ItemAttribute", "FNB_ALLERGEN_CONTENT", "value_type"); erp != "" && erp == "Multi-select" {
		t.Fatalf("the ERP copy keeps a type the ERP list knows, got %q", erp)
	}
	first := strings.Split(opts, ",")
	if len(first) < 2 {
		t.Fatalf("options %q", opts)
	}
	a, b := strings.TrimSpace(first[0]), strings.TrimSpace(first[1])
	f.seed("IT-M", "Item", "Active", map[string]interface{}{"code": "IT-M", "name": "Biscuit", "hsn_code": "1905", "gst_rate": 5})
	sheet := "id,name,hsn_code,gst_rate,attr:FNB_ALLERGEN_CONTENT\nIT-M,Biscuit,1905,5,\"" + b + "," + a + "\"\n"
	r, err := ImportProductSheet(context.Background(), f.tenantID, strings.NewReader(sheet), "system", "HR/Admin", true)
	if err != nil || !r.Committed {
		t.Fatalf("import: %v %+v", err, r)
	}
	got := f.field("ProductAttributeValue", "IT-M::FNB_ALLERGEN_CONTENT", "value")
	if !strings.Contains(got, a) || !strings.Contains(got, b) {
		t.Fatalf("both values must be stored, got %q", got)
	}
}
