package engines

import (
	"encoding/json"
	"os"
	"testing"
	"time"

	"custom_erp/db"
)

// 215.19: a B2B till bill gets an IRN through the same engine as a sales
// invoice; a B2C till bill leaves no e-invoice row at all.
func TestTillB2BBillGetsAnIRN(t *testing.T) {
	db.InitDB(testConnStr())
	tenantID, schema := uniqueLifecycleTenant(t)
	defer dropLifecycleTenant(tenantID, schema)
	if _, err := ProvisionTenantSchema(tenantID, schema, "0.1.0-test"); err != nil {
		t.Fatalf("ProvisionTenantSchema: %v", err)
	}
	if sql, err := os.ReadFile("../db/migrations/migrations_stage75_2_einvoice.sql"); err == nil {
		if _, err := db.DB.Exec(string(sql)); err != nil {
			t.Fatalf("e-invoice migration: %v", err)
		}
	}
	kick := irpKickWorker
	irpKickWorker = func(string) {}
	defer func() { irpKickWorker = kick }()
	FakeIRP.Reset()

	doc := func(id, doctype, status string, data map[string]interface{}) {
		t.Helper()
		b, _ := json.Marshal(data)
		if _, err := db.DB.Exec("INSERT INTO "+schema+".documents (id, doctype, data, status, created_by) VALUES ($1,$2,$3,$4,'tester')", id, doctype, b, status); err != nil {
			t.Fatal(err)
		}
	}
	const seller, buyer, buyerOut = "29AAACE7520K1ZI", "29BBBCF1234L1ZK", "27BBBCF1234L1ZO"
	doc("LE-T", "LegalEntity", "Active", map[string]interface{}{"code": "LE-T", "name": "Test Seller Pvt Ltd", "gstin": seller, "state": "Karnataka", "address": "1 MG Road", "city": "Bengaluru", "pincode": "560001"})
	doc("LOC-T", "Location", "Active", map[string]interface{}{"code": "LOC-T", "name": "Test Store", "legal_entity": "LE-T", "address": "1 MG Road", "city": "Bengaluru", "pincode": "560001", "state": "Karnataka"})
	doc("SKU-T1", "Item", "Active", map[string]interface{}{"code": "SKU-T1", "sku": "SKU-T1", "name": "Cotton shirt", "hsn_code": "610910", "gst_rate": 5, "uom": "PCS"})
	b2b := func(gstin, delivery, place string) map[string]interface{} {
		return map[string]interface{}{"gstin": gstin, "legal_name": "Buyer Traders", "address": "2 Brigade Road", "city": "Bengaluru",
			"pincode": "560025", "state": "Karnataka", "delivery": delivery, "place_of_supply": place, "po_number": "PO-9", "po_date": "2026-11-15"}
	}
	cart := func(id, number string, block map[string]interface{}) {
		data := map[string]interface{}{"location": "LOC-T", "bill_number": number, "amount_paid": 2100, "payment_mode": "Cash",
			"items": []map[string]interface{}{{"sku": "SKU-T1", "qty": 2, "sale_price": 1100, "offer_share": 100}}}
		if block != nil {
			data["b2b"] = block
		}
		doc(id, "POSCart", "Paid", data)
	}
	cart("CART-B2B", "TB/T/0001", b2b(buyer, "collect", "29"))
	cart("CART-SHIP", "TB/T/0002", b2b(buyerOut, "ship", "27"))
	cart("CART-B2C", "TB/T/0003", nil)

	if _, err := SaveIRPProfile(tenantID, IRPProfile{GSTIN: seller, Enabled: true, Provider: "fake", InterGSTIN: "NOT_REQUIRED",
		EffectiveFrom: time.Now().AddDate(0, 0, -1).Format("2006-01-02")}, nil, "admin"); err != nil {
		t.Fatalf("profile: %v", err)
	}
	if err := RunEInvoiceWorkerOnce(tenantID); err != nil {
		t.Fatal(err)
	}
	rec := func(id string) *IRPRecord {
		t.Helper()
		r, err := GetIRPRecordFor(tenantID, "POSCart", id)
		if err != nil {
			t.Fatal(err)
		}
		return r
	}
	if r := rec("CART-B2C"); r != nil {
		t.Fatalf("a B2C till bill must leave no e-invoice row, got %+v", *r)
	}
	payload := func(r *IRPRecord) IRPDocument {
		t.Helper()
		var raw string
		db.DB.QueryRow("SELECT payload::text FROM "+schema+".irp_documents WHERE id = $1", r.ID).Scan(&raw)
		var out IRPDocument
		json.Unmarshal([]byte(raw), &out)
		return out
	}

	// Collected at the counter: intra-state, the buyer from the bill's own block.
	r := rec("CART-B2B")
	if r == nil || r.State != IRPStateReady || len(r.Issues) != 0 {
		t.Fatalf("B2B till bill: want READY with no issues, got %+v", r)
	}
	p := payload(r)
	if p.DocDtls.Typ != "INV" || p.DocDtls.No != "TB/T/0001" || p.BuyerDtls.Gstin != buyer || p.BuyerDtls.LglNm != "Buyer Traders" || p.BuyerDtls.Pos != "29" ||
		p.ValDtls.AssVal != 2000 || p.ValDtls.CgstVal != 50 || p.ValDtls.SgstVal != 50 || p.ValDtls.IgstVal != 0 || p.ValDtls.TotInvVal != 2100 || p.ItemList[0].HsnCd != "610910" {
		t.Fatalf("B2B till bill payload: %+v", p)
	}

	// Shipped to the buyer in another state: IGST, place of supply the buyer's.
	if r := rec("CART-SHIP"); r == nil || r.State != IRPStateReady {
		t.Fatalf("shipped B2B till bill: want READY, got %+v", r)
	} else if p := payload(r); p.BuyerDtls.Pos != "27" || p.ValDtls.IgstVal != 100 || p.ValDtls.CgstVal != 0 {
		t.Fatalf("shipped bill: want IGST 100 to place 27, got pos %q %+v", p.BuyerDtls.Pos, p.ValDtls)
	}

	// Sent and acknowledged, the IRN reaches the bill's print.
	if _, err := SubmitEInvoice(tenantID, r.ID, "acc"); err != nil {
		t.Fatal(err)
	}
	RunEInvoiceWorkerOnce(tenantID)
	if r = rec("CART-B2B"); r.State != IRPStateAcknowledged || len(r.IRN) != 64 {
		t.Fatalf("after sending: want ACKNOWLEDGED with an IRN, got %s %q", r.State, r.IRN)
	}
	d := PrintDocument{Fields: map[string]interface{}{}}
	d.addIRNFields(tenantID, "POSCart", "CART-B2B")
	if d.Fields["irn"] != r.IRN || d.Fields["irn_signed_qr"] == "" {
		t.Fatalf("print fields: want the IRN and QR, got %+v", d.Fields)
	}
}
