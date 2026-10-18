package engines

import (
	"custom_erp/db"
	"strings"
	"testing"
)

// Stage 76.11: documents the engines create under an internal NewDocID keep
// that id and take a printed number in code from their store's series; a
// record-form create of one fills the number but leaves the id internal; the
// JV series is company-level (no store segment).
func TestStage7611EngineDocumentNumbers(t *testing.T) {
	db.InitDB(testConnStr())
	tenantID := "default"
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		t.Fatalf("schema: %v", err)
	}
	const store = "T7611"
	const grnID = "GRN-T7611-SCRATCH"
	cleanup := func() {
		db.DB.Exec("DELETE FROM "+schema+".sequence_counters WHERE store_code = $1", store)
		db.DB.Exec("DELETE FROM "+schema+".documents WHERE doctype = 'Location' AND id = $1", store)
		db.DB.Exec("DELETE FROM "+schema+".documents WHERE doctype = 'GRN' AND id = $1", grnID)
		db.DB.Exec("DELETE FROM "+schema+".documents WHERE doctype = 'LandedCostVoucher' AND data->>'grn_reference' = $1", grnID)
	}
	cleanup()
	defer cleanup()
	if _, err := db.DB.Exec("INSERT INTO "+schema+".documents (id, doctype, data, status, created_by) VALUES ($1, 'Location', $2, 'Active', 'admin')",
		store, `{"code":"T7611","name":"Stage 76.11 test store"}`); err != nil {
		t.Fatalf("scratch store: %v", err)
	}

	for doctype, key := range map[string]string{"SalesOrder": "SO", "ReturnRequest": "SR", "RefundRequest": "RF", "JournalVoucher": "JV",
		"PhysicalInventory": "PI", "GatePass": "GP", "ConsumptionEntry": "CE", "BundleAssembly": "KIT", "LandedCostVoucher": "LCV"} {
		s, ok := documentNumberSeriesByDoctype[doctype]
		if !ok || s.SeriesKey != key || !s.KeepID {
			t.Fatalf("%s: want series %s with its id kept, got %+v (registered %v)", doctype, key, s, ok)
		}
	}

	// The record form: the number is issued into code, the id stays unset
	// (the generic save gives it an internal id).
	gp := map[string]interface{}{"location_code": store, "vehicle_number": "KA01AB1234"}
	if err := PrepareDocumentNumber(tenantID, "HQ", "GatePass", true, gp); err != nil {
		t.Fatalf("gate pass: %v", err)
	}
	if _, has := gp["id"]; has {
		t.Fatalf("a KeepID doctype must not take the number as its id, got id %v", gp["id"])
	}
	if code, _ := gp["code"].(string); !strings.HasPrefix(code, "GP/"+store+"/") {
		t.Fatalf("gate pass at %s: want a GP/%s/ number in code, got %q", store, store, code)
	}

	// The JV series has no store segment.
	jv := map[string]interface{}{}
	issueNumberInto(jv, tenantID, "JournalVoucher", "JV-INTERNAL-1", store)
	if code, _ := jv["code"].(string); !strings.HasPrefix(code, "JV/") || strings.Contains(code, store) || jv["voucher_number"] != code {
		t.Fatalf("JV: want a company-level JV/ number in code and voucher_number, got code %q voucher_number %v", code, jv["voucher_number"])
	}

	// An engine-made document: the landed cost voucher keeps its internal id
	// and takes its GRN's store's number.
	if _, err := db.DB.Exec("INSERT INTO "+schema+".documents (id, doctype, data, status, created_by) VALUES ($1, 'GRN', $2, 'Draft', 'admin')",
		grnID, `{"code":"GRN-T7611-SCRATCH","location":"T7611"}`); err != nil {
		t.Fatalf("scratch GRN: %v", err)
	}
	lcvID, err := CreateLandedCostVoucher(tenantID, grnID, []map[string]interface{}{{"charge_type": "Freight", "amount": 100}}, "admin")
	if err != nil {
		t.Fatalf("landed cost voucher: %v", err)
	}
	data, _, err := fetchDocData(tenantID, "LandedCostVoucher", lcvID)
	if err != nil {
		t.Fatalf("read LCV: %v", err)
	}
	if code, _ := data["code"].(string); !strings.HasPrefix(lcvID, "LCV-") || !strings.HasPrefix(code, "LCV/"+store+"/") {
		t.Fatalf("LCV: want internal id LCV-... and an LCV/%s/ number in code, got id %q code %q", store, lcvID, code)
	}
}

// Stage 76.3: a manual order's split address reads back whole (with its PIN)
// for validation and sourcing; a channel order's one-string address is
// unchanged, and its state is not repeated.
func TestStage763ShippingAddressText(t *testing.T) {
	channel := map[string]interface{}{"shipping_address": "12 MG Road, Bengaluru, Karnataka 560001", "shipping_state": "Karnataka"}
	if got := ShippingAddressText(channel); got != "12 MG Road, Bengaluru, Karnataka 560001" {
		t.Fatalf("channel order: want its address unchanged, got %q", got)
	}
	split := map[string]interface{}{"shipping_address": "Flat 4, 2nd Cross", "shipping_address_line2": "Indiranagar",
		"shipping_city": "Bengaluru", "shipping_state": "Karnataka", "shipping_pincode": "560038"}
	got := ShippingAddressText(split)
	if got != "Flat 4, 2nd Cross, Indiranagar, Bengaluru, Karnataka 560038" || !pincodeShapeRe.MatchString(got) {
		t.Fatalf("split address: want the parts joined with the PIN, got %q", got)
	}
}
