package engines

import (
	"custom_erp/db"
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

// Stage 49.5.6e (user, 2026-10-29: "all recommended"): a PO billed to a store
// with no state is refused like one for a vendor with no state, and the PO
// record form no longer shows the two supply-type ticks the server sets.
func TestStage4956ePOStoreStateIsRequired(t *testing.T) {
	db.InitDB(testConnStr())
	const tenantID = "default"
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		t.Fatal(err)
	}
	const (
		entity     = "T4956E-ENT" // no GSTIN, no state
		locNone    = "T4956E-LOCNONE"
		locOwn     = "T4956E-LOCOWN" // its own State
		locByGSTIN = "T4956E-LOCGST" // under an entity with a GSTIN
		entityGST  = "T4956E-ENTGST"
		vendor     = "T4956E-V"
		sku        = "T4956E-SKU"
		draft      = "T4956E-PO-DRAFT"
	)
	clean := func() { db.DB.Exec(`DELETE FROM ` + schema + `.documents WHERE id LIKE 'T4956E-%'`) }
	clean()
	defer clean()
	insert := func(id, doctype, status string, data map[string]interface{}) {
		raw, _ := json.Marshal(data)
		if _, err := db.DB.Exec(`INSERT INTO `+schema+`.documents (id, doctype, data, status, created_by) VALUES ($1, $2, $3, $4, 'admin')`, id, doctype, raw, status); err != nil {
			t.Fatalf("insert %s: %v", id, err)
		}
	}
	insert(entity, "LegalEntity", "Active", map[string]interface{}{"code": entity, "name": "T4956E Entity"})
	insert(entityGST, "LegalEntity", "Active", map[string]interface{}{"code": entityGST, "name": "T4956E GST Entity", "gstin": "27AAPFU0939F1ZV"})
	insert(locNone, "Location", "Active", map[string]interface{}{"code": locNone, "name": "T4956E Stateless", "type": "Store", "legal_entity": entity})
	insert(locOwn, "Location", "Active", map[string]interface{}{"code": locOwn, "name": "T4956E Own", "type": "Store", "legal_entity": entity, "state": "Karnataka"})
	insert(locByGSTIN, "Location", "Active", map[string]interface{}{"code": locByGSTIN, "name": "T4956E By GSTIN", "type": "Store", "legal_entity": entityGST})
	insert(vendor, "Vendor", "Active", map[string]interface{}{"code": vendor, "name": "T4956E Vendor", "state": "Maharashtra"})
	insert(sku, "Item", "Active", map[string]interface{}{"code": sku, "name": "T4956E Tee", "hsn_code": "6109", "gst_rate": 5.0, "tax_treatment": "Taxable"})

	code := func(err error) string {
		var v *ValidationError
		if errors.As(err, &v) {
			return v.Code
		}
		return ""
	}
	po := func(loc string) map[string]interface{} {
		return map[string]interface{}{"code": "T4956E-PO-X", "po_number": "T4956E-PO-X", "vendor": vendor, "vendor_id": vendor,
			"items": `[{"sku":"` + sku + `","qty":1,"rate":100}]`, "target_warehouse": loc, "location": loc, "total_amount": 100, "status": "Draft"}
	}

	// Save (and any ValidateDocument caller): refused, naming the store.
	if err := ValidateDocument(tenantID, "PurchaseOrder", po(locNone)); code(err) != POStoreNoStateCode || !strings.Contains(err.Error(), "T4956E Stateless") {
		t.Errorf("a PO billed to a store with no state: %v, want %s naming the store", err, POStoreNoStateCode)
	}
	// A store's own State, or its entity's GSTIN, is enough.
	for _, loc := range []string{locOwn, locByGSTIN} {
		p := po(loc)
		if err := ValidateDocument(tenantID, "PurchaseOrder", p); err != nil {
			t.Errorf("a PO billed to %s was refused: %v", loc, err)
		} else if p["interstate"] != (loc == locOwn) {
			t.Errorf("PO billed to %s: interstate %v - want it derived (vendor Maharashtra)", loc, p["interstate"])
		}
	}
	// A blank or unknown store is left to the mandatory and link checks.
	if RequirePOStoreState(tenantID, map[string]interface{}{"location": ""}) != nil ||
		RequirePOStoreState(tenantID, map[string]interface{}{"location": "T4956E-NOT-A-STORE"}) != nil {
		t.Error("RequirePOStoreState refused a blank or unknown store - those are the other checks' to name")
	}

	// Submit for approval: a draft saved before the rule is refused too.
	raw, _ := json.Marshal(po(locNone))
	if _, err := db.DB.Exec(`INSERT INTO `+schema+`.documents (id, doctype, data, status, created_by) VALUES ($1, 'PurchaseOrder', $2, 'Draft', 'manager1')`, draft, raw); err != nil {
		t.Fatal(err)
	}
	if got := code(SubmitForApproval(tenantID, "PurchaseOrder", draft, "manager1", RoleStoreManager)); got != POStoreNoStateCode {
		t.Errorf("submitting a PO billed to a store with no state: %q, want %s", got, POStoreNoStateCode)
	}

	// The record form drops the two ticks the server sets.
	fields, err := GetDocTypeMeta(tenantID, "PurchaseOrder")
	if err != nil {
		t.Fatal(err)
	}
	seen := 0
	for _, f := range fields {
		switch f.Fieldname {
		case "interstate", "interstate_override":
			seen++
			if !f.ServerSet {
				t.Errorf("PurchaseOrder.%s is not marked server-set, so the form still shows it", f.Fieldname)
			}
		case "vendor", "location", "place_of_supply":
			if f.ServerSet {
				t.Errorf("PurchaseOrder.%s is marked server-set", f.Fieldname)
			}
		}
	}
	if seen != 2 {
		t.Errorf("found %d of the two supply-type tick fields in the PO meta", seen)
	}
}
