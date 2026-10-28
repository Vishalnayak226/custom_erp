package engines

import (
	"custom_erp/db"
	"encoding/json"
	"errors"
	"testing"
	"time"
)

// TestRFQToPOStage732 runs 73.2.1-73.2.3 on a provisioned throwaway client:
// an RFQ with an item line and a free-text line, three vendors (one declines,
// one quotes in boxes), quotes compared on landed total (D06), a revised
// quote superseding the first, and the award creating the PO exactly once
// with lineage both ways.
func TestRFQToPOStage732(t *testing.T) {
	db.InitDB(testConnStr())
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
	code := func(err error) string {
		var v *ValidationError
		if errors.As(err, &v) {
			return v.Code
		}
		return ""
	}
	day := func(d int) string { return time.Now().AddDate(0, 0, d).Format("2006-01-02") }
	insert("RFQ-X", "Item", "Active", map[string]interface{}{"code": "RFQ-X", "name": "Steel bolt", "hsn_code": "7318", "gst_rate": 18.0, "tax_treatment": "Taxable"})
	insert("RFQ-TAPE", "Item", "Active", map[string]interface{}{"code": "RFQ-TAPE", "name": "Packing tape", "hsn_code": "3919", "gst_rate": 18.0, "tax_treatment": "Taxable"})
	for _, v := range []string{"V1", "V2", "V3"} {
		insert("VND-"+v, "Vendor", "Active", map[string]interface{}{"code": v, "name": "Vendor " + v, "state": "Maharashtra"}) // 49.5.6b
	}
	insert("VND-OLD", "Vendor", "Inactive", map[string]interface{}{"code": "OLD", "name": "Old vendor"})
	insert("UOM-RFQ-X", "UOMConversion", "Active", map[string]interface{}{"item": "RFQ-X", "from_uom": "Box", "to_uom": "Nos", "factor": 10})

	// 73.2.1 - lines and invitations.
	if _, err := SaveRFQ(tenantID, "buyer", "HQ", RFQInput{}); code(err) != "RFQ-0551" {
		t.Fatalf("an RFQ without lines: want RFQ-0551, got %v", err)
	}
	rfq, err := SaveRFQ(tenantID, "buyer", "HQ", RFQInput{TargetDate: day(20), DueDate: day(5), Lines: []RFQLine{
		{Sku: "RFQ-X", UOM: "Nos", Qty: 20},
		{Description: "Packing tape, 48mm", UOM: "Roll", Qty: 5},
	}})
	if err != nil {
		t.Fatalf("SaveRFQ: %v", err)
	}
	data, status, _ := fetchDocData(tenantID, "RFQ", rfq)
	if status != "Draft" || numFromInterface(data["quantity"]) != 25 || str(data, "description") != "Steel bolt (+1 more)" {
		t.Fatalf("RFQ header: %s %v %q", status, data["quantity"], str(data, "description"))
	}
	if _, err := InviteRFQVendors(tenantID, rfq, "buyer", "", []string{"V1", "OLD"}); code(err) != "RFQ-0553" {
		t.Fatalf("inviting an inactive vendor: want RFQ-0553, got %v", err)
	}
	inv, err := InviteRFQVendors(tenantID, rfq, "buyer", day(4), []string{"V1", "V2"})
	if err != nil || len(inv) != 2 || inv[0].Status != RFQInvited || inv[0].DueDate != day(4) {
		t.Fatalf("invite: %v %+v", err, inv)
	}
	if err := DeclineRFQ(tenantID, rfq, "V2", "no capacity", "buyer"); err != nil {
		t.Fatalf("decline: %v", err)
	}

	// 73.2.2 - comparable quotes.
	q := func(vendor string, valid string, freight float64, lines ...VendorQuoteLine) (string, error) {
		return RecordVendorQuote(tenantID, "buyer", "HQ", QuoteInput{RFQID: rfq, Vendor: vendor, ValidUntil: valid, Freight: freight, PaymentTerms: "30 days", Lines: lines})
	}
	if _, err := q("V1", day(-1), 0, VendorQuoteLine{LineNo: 1, UnitPrice: 100, TaxRate: 18}); code(err) != "RFQ-0554" {
		t.Fatalf("an expired quote: want RFQ-0554, got %v", err)
	}
	if _, err := q("V1", day(10), 0, VendorQuoteLine{LineNo: 1, UOM: "Dozen", UnitPrice: 1000, TaxRate: 18}); code(err) != "RFQ-0554" {
		t.Fatalf("a unit with no conversion: want RFQ-0554, got %v", err)
	}
	q1, err := q("V1", day(10), 50, VendorQuoteLine{LineNo: 1, UnitPrice: 100, TaxRate: 18, LeadTimeDays: 7}, VendorQuoteLine{LineNo: 2, UnitPrice: 40, TaxRate: 18})
	if err != nil {
		t.Fatalf("V1 quote: %v", err)
	}
	quote, _, _ := fetchDocData(tenantID, "VendorQuote", q1)
	// 20 x 100 + 5 x 40 = 2200, tax 18% = 396, freight 50 -> 2646.
	if numFromInterface(quote["landed_total"]) != 2646 || numFromInterface(quote["quoted_price"]) != 2646 {
		t.Fatalf("V1 landed total %v", quote["landed_total"])
	}
	// V3 was not invited, quotes the bolts per box of 10 (master conversion) and skips the tape.
	q3, err := q("V3", day(10), 0, VendorQuoteLine{LineNo: 1, UOM: "Box", UnitPrice: 900, TaxRate: 18}, VendorQuoteLine{LineNo: 2, NotQuoted: true})
	if err != nil {
		t.Fatalf("V3 quote: %v", err)
	}
	// V1 revises the bolts down: the first quote is superseded.
	q1b, err := q("V1", day(10), 50, VendorQuoteLine{LineNo: 1, UnitPrice: 95, TaxRate: 18}, VendorQuoteLine{LineNo: 2, UnitPrice: 40, TaxRate: 18})
	if err != nil {
		t.Fatalf("V1 revision: %v", err)
	}
	if _, st, _ := fetchDocData(tenantID, "VendorQuote", q1); st != "Superseded" {
		t.Fatalf("V1's first quote is %s, want Superseded", st)
	}
	cmp, err := GetRFQComparison(tenantID, rfq)
	if err != nil {
		t.Fatalf("comparison: %v", err)
	}
	if cmp.Status != "Sent" || len(cmp.Quotes) != 2 {
		t.Fatalf("comparison status %s, %d quotes", cmp.Status, len(cmp.Quotes))
	}
	byV := map[string]RFQComparisonQuote{}
	for _, c := range cmp.Quotes {
		byV[c.Vendor] = c
	}
	// V3 is cheaper (1800 + 324 = 2124) but quotes one line of two: V1 (2528) is recommended.
	if !byV["V1"].Recommended || byV["V3"].Recommended || byV["V3"].Complete || byV["V3"].Landed != 2124 || byV["V1"].Landed != 2528 {
		t.Fatalf("recommendation: V1 %+v / V3 %+v", byV["V1"], byV["V3"])
	}
	if cmp.BestByLine[1] != q3 || byV["V3"].LandedUnit[1] != 106.2 {
		t.Fatalf("best bolt price should be V3's 90 + 18%% = 106.2 per Nos, got %v from %s", byV["V3"].LandedUnit[1], cmp.BestByLine[1])
	}
	statusOf := map[string]string{}
	for _, i := range cmp.Invitations {
		statusOf[i.Vendor] = i.Status
	}
	if statusOf["V1"] != RFQResponded || statusOf["V2"] != RFQDeclined || statusOf["V3"] != RFQResponded {
		t.Fatalf("invitation states %v", statusOf)
	}

	// 73.2.3 - award once.
	if _, err := AwardRFQ(tenantID, rfq, q1b, "buyer", "HQ", AwardOptions{}); code(err) != "RFQ-0556" {
		t.Fatalf("free-text line without an item: want RFQ-0556, got %v", err)
	}
	res, err := AwardRFQ(tenantID, rfq, q1b, "buyer", "HQ", AwardOptions{LineItems: map[int]string{2: "RFQ-TAPE"}})
	if err != nil {
		t.Fatalf("award: %v", err)
	}
	po, poStatus, err := fetchDocData(tenantID, "PurchaseOrder", res.POID)
	if err != nil || poStatus != "Draft" || str(po, "vendor") != "V1" || str(po, "source_rfq_id") != rfq || str(po, "source_quote_id") != q1b {
		t.Fatalf("PO %s: %v %s %v", res.POID, err, poStatus, po)
	}
	var items []POLineInput
	json.Unmarshal([]byte(str(po, "items")), &items)
	if len(items) != 2 || items[0].SKU != "RFQ-X" || items[0].Qty != 20 || items[0].Rate != 95 || items[1].SKU != "RFQ-TAPE" || items[1].Rate != 40 {
		t.Fatalf("PO lines %+v", items)
	}
	if numFromInterface(po["freight_amount"]) != 50 || str(po, "payment_terms") != "30 days" || numFromInterface(po["total_amount"]) != 2100 {
		t.Fatalf("PO money/terms: %v %q %v", po["freight_amount"], str(po, "payment_terms"), po["total_amount"])
	}
	data, status, _ = fetchDocData(tenantID, "RFQ", rfq)
	if status != "Closed" || str(data, "awarded_po_id") != res.POID || str(data, "awarded_quote_id") != q1b {
		t.Fatalf("RFQ after award: %s %v", status, data)
	}
	winner, wst, _ := fetchDocData(tenantID, "VendorQuote", q1b)
	if _, lst, _ := fetchDocData(tenantID, "VendorQuote", q3); wst != "Selected" || str(winner, "po_id") != res.POID || lst != "Rejected" {
		t.Fatalf("quotes after award: winner %s (%s), V3 %s", wst, str(winner, "po_id"), lst)
	}
	again, err := AwardRFQ(tenantID, rfq, q1b, "buyer", "HQ", AwardOptions{LineItems: map[int]string{2: "RFQ-TAPE"}})
	if err != nil || !again.Replayed || again.POID != res.POID {
		t.Fatalf("repeated award must return the same PO: %v %+v", err, again)
	}
	if _, err := AwardRFQ(tenantID, rfq, q3, "buyer", "HQ", AwardOptions{LeaveOut: []int{2}}); code(err) != "RFQ-0555" {
		t.Fatalf("a second award to another quote: want RFQ-0555, got %v", err)
	}
	var pos int
	db.DB.QueryRow("SELECT COUNT(*) FROM "+schema+".documents WHERE doctype = 'PurchaseOrder' AND data->>'source_rfq_id' = $1", rfq).Scan(&pos)
	if pos != 1 {
		t.Fatalf("want exactly one PO from the RFQ, got %d", pos)
	}
	// Generic saves cannot select a quote or change an awarded RFQ.
	if err := validateVendorQuoteRules("Submitted", map[string]interface{}{"status": "Selected"}); code(err) != "RFQ-0555" {
		t.Fatalf("generic select: want RFQ-0555, got %v", err)
	}
	if err := validateRFQEditRules(data); code(err) != "RFQ-0555" {
		t.Fatalf("editing an awarded RFQ: want RFQ-0555, got %v", err)
	}
}
