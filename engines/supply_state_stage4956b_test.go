package engines

import (
	"custom_erp/db"
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

// Stage 49.5.6b: a vendor is not saved without a state (a GSTIN fills it, a
// contradicting one is refused), a PO for a vendor with no state is refused on
// every path, and the supply type is always derived - never a client's tick.
func TestStage4956bVendorStateIsRequired(t *testing.T) {
	db.InitDB(testConnStr())
	const tenantID = "default"
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		t.Fatal(err)
	}
	// The rule holds even when the field's own Mandatory is switched off.
	var was bool
	if err := db.DB.QueryRow(`SELECT mandatory FROM ` + schema + `.doctype_fields WHERE doctype_name = 'Vendor' AND fieldname = 'state'`).Scan(&was); err != nil {
		t.Skipf("no Vendor.state field: %v", err)
	}
	if _, err := db.DB.Exec(`UPDATE ` + schema + `.doctype_fields SET mandatory = FALSE WHERE doctype_name = 'Vendor' AND fieldname = 'state'`); err != nil {
		t.Fatal(err)
	}
	defer db.DB.Exec(`UPDATE `+schema+`.doctype_fields SET mandatory = $1 WHERE doctype_name = 'Vendor' AND fieldname = 'state'`, was)

	code := func(err error) string {
		var v *ValidationError
		if errors.As(err, &v) {
			return v.Code + "|" + v.SubFor
		}
		return ""
	}
	vendor := func(extra map[string]interface{}) map[string]interface{} {
		v := map[string]interface{}{"code": "T4956B-V", "name": "T4956B Vendor", "status": "Active"}
		for k, x := range extra {
			v[k] = x
		}
		return v
	}

	if got := code(ValidateDocument(tenantID, "Vendor", vendor(nil))); got != "GLOBAL-0001|State" {
		t.Errorf("a vendor with no state: %q, want GLOBAL-0001|State", got)
	}
	filled := vendor(map[string]interface{}{"gstin": "29AABCU9603R1ZJ"})
	if err := ValidateDocument(tenantID, "Vendor", filled); err != nil || filled["state"] != "Karnataka" {
		t.Errorf("a GSTIN did not fill the state: err %v, state %v", err, filled["state"])
	}
	if err := ValidateDocument(tenantID, "Vendor", vendor(map[string]interface{}{"gstin": "29AABCU9603R1ZJ", "state": "Maharashtra"})); err == nil || !strings.Contains(err.Error(), "GSTIN") {
		t.Errorf("a state contradicting the GSTIN: %v", err)
	}
	if err := ValidateDocument(tenantID, "Vendor", vendor(map[string]interface{}{"state": "Maharashtra"})); err != nil {
		t.Errorf("a vendor with a state was refused: %v", err)
	}

	fields, err := GetDocTypeMeta(tenantID, "Vendor")
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range fields {
		if f.Fieldname == "state" && !f.Mandatory {
			t.Error("the Vendor form is not told State is required")
		}
	}

	// A CSV import is held to it too.
	res, err := BulkImportCSV(tenantID, "Vendor", strings.NewReader("code,name,status,state\nT4956B-VI,T4956B Import Vendor,Active,\n"), "admin", "", true)
	if err != nil {
		t.Fatal(err)
	}
	if res.FailedRows != 1 || res.SuccessRows != 0 {
		t.Errorf("import of a vendor with no state: %d ok, %d failed %+v", res.SuccessRows, res.FailedRows, res.Errors)
	}
}

func TestStage4956bPOVendorStateAndDerivedSupply(t *testing.T) {
	db.InitDB(testConnStr())
	const tenantID = "default"
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		t.Fatal(err)
	}
	const (
		entity = "T4956B-ENT" // Maharashtra (27), by GSTIN
		loc    = "T4956B-LOC"
		vMH    = "T4956B-VMH" // Maharashtra, by state
		vKA    = "T4956B-VKA" // Karnataka, by GSTIN
		vNone  = "T4956B-VNONE"
		sku    = "T4956B-SKU"
		draft  = "T4956B-PO-DRAFT"
	)
	clean := func() { db.DB.Exec(`DELETE FROM ` + schema + `.documents WHERE id LIKE 'T4956B-%'`) }
	clean()
	defer clean()
	insert := func(id, doctype, status string, data map[string]interface{}) {
		raw, _ := json.Marshal(data)
		if _, err := db.DB.Exec(`INSERT INTO `+schema+`.documents (id, doctype, data, status, created_by) VALUES ($1, $2, $3, $4, 'admin')`, id, doctype, raw, status); err != nil {
			t.Fatalf("insert %s: %v", id, err)
		}
	}
	insert(entity, "LegalEntity", "Active", map[string]interface{}{"code": entity, "name": "T4956B Entity", "gstin": "27AAPFU0939F1ZV"})
	insert(loc, "Location", "Active", map[string]interface{}{"code": loc, "name": "T4956B Store", "type": "Store", "legal_entity": entity})
	insert(vMH, "Vendor", "Active", map[string]interface{}{"code": vMH, "name": "T4956B Mumbai", "state": "Maharashtra"})
	insert(vKA, "Vendor", "Active", map[string]interface{}{"code": vKA, "name": "T4956B Bengaluru", "gstin": "29AABCU9603R1ZJ"})
	insert(vNone, "Vendor", "Active", map[string]interface{}{"code": vNone, "name": "T4956B Stateless"})
	insert(sku, "Item", "Active", map[string]interface{}{"code": sku, "name": "T4956B Tee", "hsn_code": "6109", "gst_rate": 5.0, "tax_treatment": "Taxable"})

	code := func(err error) string {
		var v *ValidationError
		if errors.As(err, &v) {
			return v.Code
		}
		return ""
	}
	po := func(vendor string, interstate bool) map[string]interface{} {
		return map[string]interface{}{"code": "T4956B-PO-X", "po_number": "T4956B-PO-X", "vendor": vendor, "vendor_id": vendor,
			"items": `[{"sku":"` + sku + `","qty":1,"rate":100}]`, "target_warehouse": loc, "location": loc, "total_amount": 100,
			"status": "Draft", "interstate": interstate, "interstate_override": true}
	}

	// Save (and any ValidateDocument caller).
	err = ValidateDocument(tenantID, "PurchaseOrder", po(vNone, false))
	if code(err) != POVendorNoStateCode || !strings.Contains(err.Error(), "T4956B Stateless") {
		t.Errorf("a PO for a vendor with no state: %v, want %s naming the vendor", err, POVendorNoStateCode)
	}
	// The supply type is derived whatever the client claims.
	same := po(vMH, true)
	if err := ValidateDocument(tenantID, "PurchaseOrder", same); err != nil {
		t.Fatalf("same-state PO: %v", err)
	}
	if same["interstate"] != false || same["interstate_override"] != false {
		t.Errorf("same state, client said inter-state with override: interstate %v, override %v", same["interstate"], same["interstate_override"])
	}
	other := po(vKA, false)
	if err := ValidateDocument(tenantID, "PurchaseOrder", other); err != nil {
		t.Fatalf("other-state PO: %v", err)
	}
	if other["interstate"] != true || other["interstate_override"] != false {
		t.Errorf("other state, client said intra-state with override: interstate %v, override %v", other["interstate"], other["interstate_override"])
	}

	// Submit: a Draft saved before the rule still cannot go for approval.
	insert(draft, "PurchaseOrder", "Draft", po(vNone, false))
	if got := code(SubmitForApproval(tenantID, "PurchaseOrder", draft, "manager1", RoleStoreManager)); got != POVendorNoStateCode {
		t.Errorf("submitting a PO for a vendor with no state: %q", got)
	}

	// CSV import (a preview, which runs the same row checks).
	csv := "code,po_number,vendor,vendor_id,items,target_warehouse,location,total_amount,status\n" +
		`T4956B-PO-I,T4956B-PO-I,` + vNone + `,` + vNone + `,"[{""sku"":""` + sku + `"",""qty"":1,""rate"":100}]",` + loc + `,` + loc + ",100,Draft\n"
	res, err := BulkImportCSV(tenantID, "PurchaseOrder", strings.NewReader(csv), "admin", "", true)
	if err != nil {
		t.Fatal(err)
	}
	if res.FailedRows != 1 || len(res.Errors) == 0 || !strings.Contains(res.Errors[0].Message, "has no state") {
		t.Errorf("import of a PO for a vendor with no state: %d ok, %d failed %+v", res.SuccessRows, res.FailedRows, res.Errors)
	}

	// An RFQ award and a supply proposal ask the same function.
	if code(RequirePOVendorState(tenantID, map[string]interface{}{"vendor": vNone})) != POVendorNoStateCode ||
		RequirePOVendorState(tenantID, map[string]interface{}{"vendor": vKA}) != nil ||
		RequirePOVendorState(tenantID, map[string]interface{}{"vendor": "T4956B-NOT-A-VENDOR"}) != nil {
		t.Error("RequirePOVendorState: stateless refused, GSTIN-only allowed, unknown left to the link check")
	}
}
