package engines

import (
	"custom_erp/db"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"
)

func wantB2BCode(t *testing.T, what string, err error, code string) {
	t.Helper()
	var ve *ValidationError
	if !errors.As(err, &ve) || ve.Code != code {
		t.Errorf("%s: want %s, got %v", what, code, err)
	}
}

// Ask 215.2/215.3/215.4: a B2B bill at the till. Pinned: the buyer block's
// required fields (CGST Rule 46: GSTIN, name, address; the buyer's PO by
// default); the place of supply (collected: the store's state; shipped: the
// buyer's GSTIN state, s.10(1)(b)) and so IGST; the stored block reads back;
// a GSTIN finds its customer; a B2B bill needs no PAN or address prompt; and
// a named customer's cash is counted across the day (s.269ST).
func TestPOSB2BBill(t *testing.T) {
	db.InitDB(testConnStr())
	tenantID := "default"
	if TenantPhoneCountry(tenantID) != "IN" {
		t.Skip("the GST rules pinned here are India's")
	}
	if GetSettingBool(tenantID, "pos.b2b.po_optional") {
		t.Skip("this database makes the PO optional")
	}
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		t.Fatalf("schema: %v", err)
	}
	const store = "S215-STORE"
	cleanup := func() {
		db.DB.Exec("DELETE FROM " + schema + ".documents WHERE id LIKE 'S215-%'")
	}
	cleanup()
	defer cleanup()
	insert := func(id, doctype, status string, data map[string]interface{}) {
		t.Helper()
		raw, _ := json.Marshal(data)
		if _, err := db.DB.Exec("INSERT INTO "+schema+".documents (id, doctype, data, status, created_by) VALUES ($1, $2, $3, $4, 'system')", id, doctype, raw, status); err != nil {
			t.Fatalf("insert %s: %v", id, err)
		}
	}
	insert(store, "Location", "Active", map[string]interface{}{"code": store, "name": "S215 Store", "type": "Store", "status": "Active", "state": "Karnataka"})

	ka := gstinWithCheckDigit(t, "29AABCU9603R1Z") // the store's own state
	mh := gstinWithCheckDigit(t, "27AABCU9603R1Z")
	today := BusinessToday()
	buyer := func(gstin string) *POSB2BBuyer {
		return &POSB2BBuyer{GSTIN: strings.ToLower(gstin), LegalName: "S215 Traders Pvt Ltd", Address: "12 MG Road", City: "Bengaluru",
			Pincode: "560001", PONumber: "PO-77", PODate: today}
	}

	// Collected, same state: CGST+SGST, the block filled by the server.
	b := buyer(ka)
	inter, err := PreparePOSB2B(tenantID, store, b)
	if err != nil || inter || b.GSTIN != ka || b.StateCode != "29" || b.State == "" || b.PlaceOfSupply != "29" || b.ReverseCharge != "No" || b.Delivery != "collect" {
		t.Fatalf("collected KA buyer: inter=%v err=%v %+v", inter, err, b)
	}
	// Collected by an out-of-state buyer: still the store's state (s.10(1)(a)).
	b = buyer(mh)
	if inter, err := PreparePOSB2B(tenantID, store, b); err != nil || inter || b.PlaceOfSupply != "29" {
		t.Errorf("collected MH buyer: inter=%v err=%v place=%s", inter, err, b.PlaceOfSupply)
	}
	// Shipped on the order of an MH buyer to Goa: the buyer's state, IGST.
	b = buyer(mh)
	b.Delivery, b.ShipAddress, b.ShipCity, b.ShipPincode, b.ShipState = "Ship", "Plot 4", "Panaji", "403001", "Goa"
	if inter, err := PreparePOSB2B(tenantID, store, b); err != nil || !inter || b.PlaceOfSupply != "27" || b.ShipName != b.LegalName {
		t.Errorf("shipped MH buyer: inter=%v err=%v %+v", inter, err, b)
	}
	// Shipped for a KA buyer to Maharashtra: bill-to-ship-to keeps it intra-state.
	b = buyer(ka)
	b.Delivery, b.ShipAddress, b.ShipCity, b.ShipPincode, b.ShipState = "ship", "Unit 9", "Pune", "411001", "Maharashtra"
	if inter, err := PreparePOSB2B(tenantID, store, b); err != nil || inter || b.PlaceOfSupply != "29" {
		t.Errorf("shipped KA buyer to MH: inter=%v err=%v place=%s", inter, err, b.PlaceOfSupply)
	}
	// The running quote's answer, with nothing else filled in yet.
	if place, inter := POSB2BPlaceOfSupply(tenantID, store, &POSB2BBuyer{GSTIN: mh, Delivery: "ship"}); place != "27" || !inter {
		t.Errorf("quote place of supply: %s %v", place, inter)
	}

	// Refusals.
	b = buyer(ka)
	b.GSTIN = ka[:14] + map[bool]string{true: "0", false: "1"}[ka[14] != '0']
	_, err = PreparePOSB2B(tenantID, store, b)
	wantB2BCode(t, "bad check digit", err, "CUSTOM-0869")
	b = buyer(ka)
	b.Address = " "
	_, err = PreparePOSB2B(tenantID, store, b)
	wantB2BCode(t, "no address", err, "POSOFF-0879")
	b = buyer(ka)
	b.Pincode = "56001"
	_, err = PreparePOSB2B(tenantID, store, b)
	wantB2BCode(t, "5-digit PIN", err, "POSOFF-0879")
	b = buyer(ka)
	b.PONumber = ""
	_, err = PreparePOSB2B(tenantID, store, b)
	wantB2BCode(t, "no PO number", err, "POSOFF-0880")
	b = buyer(ka)
	b.PODate = time.Now().AddDate(0, 0, 2).Format("2006-01-02")
	_, err = PreparePOSB2B(tenantID, store, b)
	wantB2BCode(t, "PO dated after today", err, "POSOFF-0880")
	b = buyer(mh)
	b.Delivery, b.ShipAddress = "ship", "Plot 4"
	_, err = PreparePOSB2B(tenantID, store, b)
	wantB2BCode(t, "shipped without city, PIN and state", err, "POSOFF-0881")

	// The block a bill stores reads back as it was.
	b = buyer(mh)
	b.Delivery, b.ShipAddress, b.ShipCity, b.ShipPincode, b.ShipState = "ship", "Plot 4", "Panaji", "403001", "Goa"
	if _, err := PreparePOSB2B(tenantID, store, b); err != nil {
		t.Fatalf("prepare: %v", err)
	}
	raw, _ := json.Marshal(map[string]interface{}{"b2b": b})
	var stored map[string]interface{}
	_ = json.Unmarshal(raw, &stored)
	if got := POSB2BFromData(stored); got == nil || *got != *b {
		t.Errorf("stored block: %+v, want %+v", got, b)
	}
	if POSB2BFromData(map[string]interface{}{}) != nil || POSB2BFromData(map[string]interface{}{"b2b": map[string]interface{}{"gstin": ""}}) != nil {
		t.Error("a bill with no buyer GSTIN has no B2B block")
	}

	// A GSTIN finds its customer, typed in any case.
	insert("S215-CUST", "Customer", "Active", map[string]interface{}{"name": "S215 Traders", "gstin": mh, "phone": "9000021501"})
	if c, ok, err := LookupCustomerByGSTIN(tenantID, strings.ToLower(mh)); err != nil || !ok || c.ID != "S215-CUST" {
		t.Errorf("lookup by GSTIN: %+v %v %v", c, ok, err)
	}
	_, _, err = LookupCustomerByGSTIN(tenantID, "29ABC")
	wantB2BCode(t, "lookup with a bad GSTIN", err, "CUSTOM-0869")

	// A B2B bill: the GSTIN carries the PAN and the block the address.
	walkIn, err := EnsureWalkInCustomer(tenantID, store)
	if err != nil {
		t.Fatalf("walk-in: %v", err)
	}
	defer db.DB.Exec("DELETE FROM "+schema+".documents WHERE doctype = 'Customer' AND data->>'walk_in_store' = $1", store)
	if err := CheckHighValue(tenantID, HighValueSale{CustomerID: walkIn.ID, Total: 300000, B2B: b}); err != nil {
		t.Errorf("B2B bill of 3,00,000 to the walk-in record: %v", err)
	}
	wantB2BCode(t, "B2C bill of 3,00,000 to the walk-in record", CheckHighValue(tenantID, HighValueSale{CustomerID: walkIn.ID, Total: 300000}), "POSOFF-0866")

	// s.269ST: one customer's cash across the day's bills.
	alloc := func(cashPaise, cardPaise int64) []SaleLineAllocation {
		return []SaleLineAllocation{{SKU: "S215-SKU", Qty: 1, Tenders: map[string]int64{"Cash": cashPaise, "Card": cardPaise}}}
	}
	insert("S215-BILL1", "POSCart", "Paid", map[string]interface{}{"customer_id": "S215-CUST", "allocation": alloc(10000000, 5000000)})
	older, _ := json.Marshal(alloc(5000000, 0)) // an older cart stores it as a JSON string
	insert("S215-BILL2", "POSCart", "Paid", map[string]interface{}{"customer_id": "S215-CUST", "allocation": string(older)})
	insert("S215-BILL3", "POSCart", "Draft", map[string]interface{}{"customer_id": "S215-CUST", "allocation": alloc(9000000, 0)})
	if cash, err := CustomerCashToday(tenantID, "S215-CUST", ""); err != nil || cash != 150000 {
		t.Errorf("cash today: %v %v, want 150000", cash, err)
	}
	if cash, _ := CustomerCashToday(tenantID, "S215-CUST", "S215-BILL2"); cash != 100000 {
		t.Errorf("cash today without this bill: %v, want 100000", cash)
	}
	insert("S215-BILL4", "POSCart", "Paid", map[string]interface{}{"customer_id": walkIn.ID, "allocation": alloc(19000000, 0)})
	if cash, _ := CustomerCashToday(tenantID, walkIn.ID, ""); cash != 0 {
		t.Errorf("the walk-in record stands for many people: %v, want 0", cash)
	}
	if err := CheckHighValue(tenantID, HighValueSale{CustomerID: "S215-CUST", Total: 49999, Cash: 49999}); err != nil {
		t.Errorf("1,99,999 cash in the day: %v", err)
	}
	wantB2BCode(t, "2,00,000 cash in the day", CheckHighValue(tenantID, HighValueSale{CustomerID: "S215-CUST", Total: 50000, Cash: 50000}), "POSOFF-0868")
	wantB2BCode(t, "B2B bill, 2,00,000 cash in the day", CheckHighValue(tenantID, HighValueSale{CustomerID: "S215-CUST", Total: 50000, Cash: 50000, B2B: b}), "POSOFF-0868")
	if err := CheckHighValue(tenantID, HighValueSale{CustomerID: "S215-CUST", Total: 50000, Cash: 50000, CartNumber: "S215-BILL2"}); err != nil {
		t.Errorf("the bill being checked is not counted twice: %v", err)
	}
	if err := CheckHighValue(tenantID, HighValueSale{CustomerID: "S215-CUST", Total: 90000}); err != nil {
		t.Errorf("no cash on this bill: %v", err)
	}
}
