package engines

import (
	"custom_erp/db"
	"encoding/json"
	"strings"
	"sync"
	"testing"
)

// Stage 148.14 - a requisition with item lines, each with its own store; the
// open requests of one vendor combine into a PO per store, every line traced.

type reqLinesFixture struct {
	*agreementFixture
}

func newReqLinesFixture(t *testing.T) *reqLinesFixture {
	f := &reqLinesFixture{newAgreementFixture(t)}
	for _, loc := range []string{"TEST14814-S1", "TEST14814-S2"} {
		f.insert(loc, "Location", "Active", map[string]interface{}{"code": loc, "name": loc, "state": "Maharashtra"})
	}
	f.insert("TEST14814-V", "Vendor", "Active", map[string]interface{}{"code": "TEST14814-V", "name": "Req Vendor", "state": "Maharashtra"})
	f.insert("TEST14814-W", "Vendor", "Active", map[string]interface{}{"code": "TEST14814-W", "name": "Other Vendor", "state": "Maharashtra"})
	for _, sku := range []string{"TEST14814-A", "TEST14814-B"} {
		f.insert(sku, "Item", "Active", map[string]interface{}{"code": sku, "name": sku, "hsn_code": "6204", "gst_rate": 5.0,
			"tax_treatment": "Taxable", "standard_cost": 25.0})
	}
	return f
}

func (f *reqLinesFixture) request(id string, lines ...map[string]interface{}) {
	raw, _ := json.Marshal(lines)
	f.insert(id, "PurchaseRequisition", "Approved", map[string]interface{}{"code": id, "description": "lines", "quantity": 1.0,
		"department": "Ops", "total_amount": 1.0, "items": string(raw)})
}

func reqLine(sku string, qty int, store, vendor string) map[string]interface{} {
	return map[string]interface{}{"sku": sku, "qty": qty, "store": store, "vendor_id": vendor}
}

func (f *reqLinesFixture) cleanup2(extra ...string) {
	f.ids = append(f.ids, extra...)
	f.cleanup()
}

func TestStage14814CombineMakesOnePOPerStoreWithTraces(t *testing.T) {
	f := newReqLinesFixture(t)
	f.request("TEST14814-R1", reqLine("TEST14814-A", 10, "TEST14814-S1", "TEST14814-V"), reqLine("TEST14814-B", 4, "TEST14814-S2", "TEST14814-V"))
	f.request("TEST14814-R2", reqLine("TEST14814-A", 5, "TEST14814-S1", "TEST14814-V"), reqLine("TEST14814-B", 7, "TEST14814-S1", "TEST14814-W"))
	var pos []string
	defer func() { f.cleanup2(pos...) }()

	res, err := CombineRequisitionLines("default", "system", "TEST14814-V", "")
	if err != nil {
		t.Fatalf("combine: %v", err)
	}
	for _, p := range res.Documents {
		pos = append(pos, p.POID)
	}
	if len(res.Documents) != 2 {
		t.Fatalf("one PO per store expected (2), got %+v", res.Documents)
	}
	var s1 *CombinedPO
	for i := range res.Documents {
		if res.Documents[i].Store == "TEST14814-S1" {
			s1 = &res.Documents[i]
		}
	}
	if s1 == nil || s1.Lines != 2 {
		t.Fatalf("store 1 PO should hold the two vendor lines (R1 line 1, R2 line 1): %+v", res.Documents)
	}
	po, st, _ := fetchDocData("default", "PurchaseOrder", s1.POID)
	if st != "Draft" || str(po, "vendor") != "TEST14814-V" || str(po, "target_warehouse") != "TEST14814-S1" {
		t.Fatalf("PO header %v status %s", po, st)
	}
	if !strings.Contains(str(po, "source_requisition_ids"), "TEST14814-R1") || !strings.Contains(str(po, "source_requisition_ids"), "TEST14814-R2") {
		t.Fatalf("PO does not name its requests: %v", po["source_requisition_ids"])
	}
	lines, _ := parsePOLineMaps(str(po, "items"))
	if len(lines) != 2 || str(lines[0], "requisition_id") != "TEST14814-R1" || int(numFromInterface(lines[0]["requisition_line"])) != 1 ||
		str(lines[1], "requisition_id") != "TEST14814-R2" {
		t.Fatalf("PO lines carry no trace: %v", lines)
	}
	// and back: the request line names the PO and its line number
	r1, st1, _ := fetchDocData("default", "PurchaseRequisition", "TEST14814-R1")
	ls, _ := parsePOLineMaps(str(r1, "items"))
	if str(ls[0], "po_id") != s1.POID || int(numFromInterface(ls[0]["po_line"])) != 1 || str(ls[1], "po_id") == "" {
		t.Fatalf("request lines carry no trace: %v", ls)
	}
	if st1 != "Converted" {
		t.Fatalf("R1 has every line on a PO, want Converted, got %s", st1)
	}
	// R2's other vendor line stays open: the request is not converted.
	_, st2, _ := fetchDocData("default", "PurchaseRequisition", "TEST14814-R2")
	if st2 != "Approved" {
		t.Fatalf("R2 still has a line for another vendor, want Approved, got %s", st2)
	}
	// nothing left for the vendor
	if _, err := CombineRequisitionLines("default", "system", "TEST14814-V", ""); lcCode(err) != "PURCHA-5013" {
		t.Fatalf("second press: want PURCHA-5013, got %v", err)
	}
}

func TestStage14814TwoPeoplePressingAtOnceTakeALineOnce(t *testing.T) {
	f := newReqLinesFixture(t)
	f.request("TEST14814-R3", reqLine("TEST14814-A", 10, "TEST14814-S1", "TEST14814-V"))
	var mu sync.Mutex
	var pos []string
	defer func() { f.cleanup2(pos...) }()
	var wg sync.WaitGroup
	ok, refused := 0, 0
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			res, err := CombineRequisitionLines("default", "system", "TEST14814-V", "")
			mu.Lock()
			defer mu.Unlock()
			if err != nil {
				if lcCode(err) == "PURCHA-5013" {
					refused++
				} else {
					t.Errorf("unexpected: %v", err)
				}
				return
			}
			ok++
			for _, p := range res.Documents {
				pos = append(pos, p.POID)
			}
		}()
	}
	wg.Wait()
	if ok != 1 || refused != 1 {
		t.Fatalf("one press makes the PO, the other finds nothing open: ok=%d refused=%d", ok, refused)
	}
	n := 0
	for _, p := range pos {
		po, _, _ := fetchDocData("default", "PurchaseOrder", p)
		ls, _ := parsePOLineMaps(str(po, "items"))
		n += len(ls)
	}
	if n != 1 {
		t.Fatalf("the line is on %d PO lines, want 1", n)
	}
}

func TestStage14814ACancelledPOFreesItsLines(t *testing.T) {
	f := newReqLinesFixture(t)
	f.request("TEST14814-R4", reqLine("TEST14814-A", 2, "TEST14814-S1", "TEST14814-V"), reqLine("TEST14814-B", 2, "TEST14814-S1", "TEST14814-W"))
	var pos []string
	defer func() { f.cleanup2(pos...) }()
	res, err := CombineRequisitionLines("default", "system", "TEST14814-V", "")
	if err != nil {
		t.Fatalf("combine: %v", err)
	}
	pos = append(pos, res.Documents[0].POID)
	if _, err := db.DB.Exec("UPDATE "+f.schema+".documents SET status = 'Cancelled' WHERE id = $1", res.Documents[0].POID); err != nil {
		t.Fatal(err)
	}
	res2, err := CombineRequisitionLines("default", "system", "TEST14814-V", "")
	if err != nil {
		t.Fatalf("after the PO was cancelled the line is open again: %v", err)
	}
	pos = append(pos, res2.Documents[0].POID)
}

func TestStage14814RequisitionRules(t *testing.T) {
	f := newReqLinesFixture(t)
	defer f.cleanup()
	// header-only request still needs its description, quantity and amount
	p := map[string]interface{}{"department": "Ops"}
	if got := lcCode(PrepareRequisitionLines("default", true, p)); got != "PURCHA-5011" {
		t.Fatalf("header-only with nothing: want PURCHA-5011, got %q", got)
	}
	// header-only with its three fields is as before
	p = map[string]interface{}{"description": "paper", "quantity": 3.0, "total_amount": 30.0}
	if err := PrepareRequisitionLines("default", true, p); err != nil {
		t.Fatalf("header-only: %v", err)
	}
	// a line needs an item, a quantity above zero and a store
	for name, l := range map[string]map[string]interface{}{
		"no item":   reqLine("", 1, "TEST14814-S1", ""),
		"unknown":   reqLine("NOPE-14814", 1, "TEST14814-S1", ""),
		"no qty":    reqLine("TEST14814-A", 0, "TEST14814-S1", ""),
		"no store":  reqLine("TEST14814-A", 1, "", ""),
		"bad store": reqLine("TEST14814-A", 1, "NOSTORE", ""),
	} {
		raw, _ := json.Marshal([]map[string]interface{}{l})
		if got := lcCode(PrepareRequisitionLines("default", true, map[string]interface{}{"items": string(raw)})); got != "PURCHA-5010" {
			t.Fatalf("%s: want PURCHA-5010, got %q", name, got)
		}
	}
	// lines fill the header
	raw, _ := json.Marshal([]map[string]interface{}{
		{"sku": "TEST14814-A", "qty": 4, "store": "TEST14814-S1", "est_rate": 10.0}, {"sku": "TEST14814-B", "qty": 6, "store": "TEST14814-S2"}})
	p = map[string]interface{}{"items": string(raw)}
	if err := PrepareRequisitionLines("default", true, p); err != nil {
		t.Fatalf("lines: %v", err)
	}
	if numFromInterface(p["quantity"]) != 10 || strField(p, "description") == "" || numFromInterface(p["total_amount"]) != 40 {
		t.Fatalf("header not filled from the lines: %v", p)
	}
}

func TestStage14814TakenLinesCannotBeEditedAndOldConvertRefusesLines(t *testing.T) {
	f := newReqLinesFixture(t)
	f.request("TEST14814-R5", reqLine("TEST14814-A", 2, "TEST14814-S1", "TEST14814-V"))
	var pos []string
	defer func() { f.cleanup2(pos...) }()
	// a request with lines cannot go through the one-shot PO conversion that would lose them
	if _, err := ConvertRequisitionToOrder("default", "TEST14814-R5", "PurchaseOrder", "HQ", "26-27", "system"); lcCode(err) != "PURCHA-5016" {
		t.Fatalf("convert to PO of a request with lines: want PURCHA-5016, got %v", err)
	}
	if _, err := ConvertRequisitionToOrder("default", "TEST14814-R5", "RFQ", "HQ", "26-27", "system"); lcCode(err) != "PURCHA-5016" {
		t.Fatalf("convert to RFQ of a request with lines: want PURCHA-5016, got %v", err)
	}
	res, err := CombineRequisitionLines("default", "system", "TEST14814-V", "")
	if err != nil {
		t.Fatal(err)
	}
	pos = append(pos, res.Documents[0].POID)
	r, _, _ := fetchDocData("default", "PurchaseRequisition", "TEST14814-R5")
	edited := map[string]interface{}{}
	for k, v := range r {
		edited[k] = v
	}
	// the form sends the lines back without the trace keys: they are put back
	back, _ := json.Marshal([]map[string]interface{}{reqLine("TEST14814-A", 2, "TEST14814-S1", "TEST14814-V")})
	edited["items"] = string(back)
	if err := validateRequisitionLineEdit(r, edited); err != nil {
		t.Fatalf("unchanged lines: %v", err)
	}
	if ls, _ := parsePOLineMaps(str(edited, "items")); str(ls[0], "po_id") == "" {
		t.Fatalf("trace not put back: %v", ls)
	}
	changed, _ := json.Marshal([]map[string]interface{}{reqLine("TEST14814-A", 99, "TEST14814-S1", "TEST14814-V")})
	edited["items"] = string(changed)
	if got := lcCode(validateRequisitionLineEdit(r, edited)); got != "PURCHA-5012" {
		t.Fatalf("changing a line already on a PO: want PURCHA-5012, got %q", got)
	}
}
