package engines

import (
	"encoding/json"
	"errors"
	"testing"

	"custom_erp/db"
)

// 185.1: a store's UPI ID (or a UPI tender's own) reaches the till with the
// store's name, only on UPI tenders; a UPI ID that is not name@provider, or
// one on a card tender, is refused at save.
func TestUPITenderQRPayee(t *testing.T) {
	var ve *ValidationError
	for _, bad := range []string{"store", "store@", "@ybl", "st ore@ybl", "store@1bank", "a@b@c"} {
		if err := ValidateUPIID("Location", map[string]interface{}{"upi_id": bad}); !errors.As(err, &ve) || ve.Code != "POSOFF-0878" || ve.SubFor != "upi_id" {
			t.Errorf("UPI ID %q: want POSOFF-0878 on upi_id, got %v", bad, err)
		}
	}
	for _, good := range []string{"", "  ", "store@ybl", "bharatpe.90012345@fbpe", "q12-34_x@paytm", "shop.name@okhdfcbank"} {
		if err := ValidateUPIID("Location", map[string]interface{}{"upi_id": good}); err != nil {
			t.Errorf("UPI ID %q: %v", good, err)
		}
	}
	if err := ValidateUPIID("POSTender", map[string]interface{}{"kind": "Card", "upi_id": "store@ybl"}); !errors.As(err, &ve) || ve.Code != "POSOFF-0878" {
		t.Errorf("a UPI ID on a card tender: want POSOFF-0878, got %v", err)
	}

	db.InitDB(testConnStr())
	tenantID, schema := uniqueLifecycleTenant(t)
	defer dropLifecycleTenant(tenantID, schema)
	if _, err := ProvisionTenantSchema(tenantID, schema, "0.1.0-test"); err != nil {
		t.Fatalf("ProvisionTenantSchema: %v", err)
	}
	// The save path runs the check (doctype.go), not only the helper.
	if err := ValidateDocument(tenantID, "POSTender", map[string]interface{}{"name": "Card 2", "kind": "Card", "status": "Active", "upi_id": "store@ybl"}); !errors.As(err, &ve) || ve.Code != "POSOFF-0878" {
		t.Errorf("ValidateDocument, UPI ID on a card tender: want POSOFF-0878, got %v", err)
	}
	if _, err := db.DB.Exec("DELETE FROM " + schema + ".documents WHERE doctype = 'POSTender'"); err != nil {
		t.Fatal(err)
	}
	put := func(id, doctype string, data map[string]interface{}) {
		raw, _ := json.Marshal(data)
		if _, err := db.DB.Exec("INSERT INTO "+schema+".documents (id, doctype, data, status, created_by) VALUES ($1, $2, $3, 'Active', 'system')", id, doctype, raw); err != nil {
			t.Fatalf("%s: %v", id, err)
		}
	}
	put("UPI-S1", "Location", map[string]interface{}{"code": "UPI-S1", "name": "Andheri Store", "type": "Store", "upi_id": "andheri@fbpe"})
	put("UPI-S2", "Location", map[string]interface{}{"code": "UPI-S2", "name": "Bandra Store", "type": "Store"})

	byName := func(store string) map[string]TenderDef {
		list, err := StoreTenders(tenantID, store)
		if err != nil {
			t.Fatalf("StoreTenders(%s): %v", store, err)
		}
		out := map[string]TenderDef{}
		for _, td := range list {
			out[td.Name] = td
		}
		return out
	}
	// No tender on file: the built-in UPI takes the store's UPI ID.
	got := byName("UPI-S1")
	if u := got["UPI"]; u.UPIID != "andheri@fbpe" || u.Payee != "Andheri Store" {
		t.Errorf("built-in UPI at a store with a UPI ID: %+v", u)
	}
	if c := got["Cash"]; c.UPIID != "" || c.Payee != "" {
		t.Errorf("cash carries no UPI ID: %+v", c)
	}
	if u := byName("UPI-S2")["UPI"]; u.UPIID != "" || u.Payee != "" {
		t.Errorf("a store with no UPI ID draws no QR: %+v", u)
	}

	// The client's own tenders: one UPI on the store's ID, one with its own,
	// and a card tender whose stray UPI ID never reaches the till.
	put("T-BPE", "POSTender", map[string]interface{}{"name": "BharatPe", "kind": "UPI", "needs_reference": "Yes", "display_order": 1, "status": "Active"})
	put("T-PP", "POSTender", map[string]interface{}{"name": "PhonePe", "kind": "UPI", "needs_reference": "Yes", "upi_id": "andheri.pp@ybl", "display_order": 2, "status": "Active"})
	put("T-CARD", "POSTender", map[string]interface{}{"name": "HDFC Card", "kind": "Card", "upi_id": "stray@ybl", "display_order": 3, "status": "Active"})
	got = byName("UPI-S1")
	if b := got["BharatPe"]; b.UPIID != "andheri@fbpe" || b.Payee != "Andheri Store" {
		t.Errorf("a UPI tender with no UPI ID of its own: %+v", b)
	}
	if p := got["PhonePe"]; p.UPIID != "andheri.pp@ybl" || p.Payee != "Andheri Store" {
		t.Errorf("a UPI tender with its own UPI ID: %+v", p)
	}
	if c := got["HDFC Card"]; c.UPIID != "" || c.Payee != "" {
		t.Errorf("a card tender: %+v", c)
	}
	s2 := byName("UPI-S2")
	if b := s2["BharatPe"]; b.UPIID != "" {
		t.Errorf("BharatPe at a store with no UPI ID: %+v", b)
	}
	if p := s2["PhonePe"]; p.UPIID != "andheri.pp@ybl" || p.Payee != "Bandra Store" {
		t.Errorf("PhonePe's own UPI ID at the second store: %+v", p)
	}
}
