package engines

import (
	"custom_erp/db"
	"encoding/json"
	"testing"
	"time"
)

// Stage 73 / 55.10: part-settlement of receivables and payables, credit notes
// that reduce the invoice they name, and the ageing reports that must still
// tie to 1300 and 2100 afterwards.

const t5510 = "T5510-"

func t5510Doc(t *testing.T, schema, id, doctype, status string, data map[string]interface{}) {
	t.Helper()
	data["status"] = status
	b, _ := json.Marshal(data)
	if _, err := db.DB.Exec("INSERT INTO "+schema+".documents (id, doctype, data, status, created_by) VALUES ($1, $2, $3, $4, 'system')",
		id, doctype, string(b), status); err != nil {
		t.Fatalf("insert %s %s: %v", doctype, id, err)
	}
}

func t5510Data(t *testing.T, schema, id string) (string, map[string]interface{}) {
	t.Helper()
	var dataStr, status string
	if err := db.DB.QueryRow("SELECT data, status FROM "+schema+".documents WHERE id = $1", id).Scan(&dataStr, &status); err != nil {
		t.Fatalf("read %s: %v", id, err)
	}
	var data map[string]interface{}
	_ = json.Unmarshal([]byte(dataStr), &data)
	return status, data
}

// t5510Net is debit - credit, in paise, on one account across documents.
func t5510Net(t *testing.T, schema, account string, docIDs ...string) int64 {
	t.Helper()
	var net int64
	for _, id := range docIDs {
		var v int64
		if err := db.DB.QueryRow("SELECT COALESCE(SUM(debit),0) - COALESCE(SUM(credit),0) FROM "+schema+".gl_postings WHERE account_code = $1 AND document_id = $2",
			account, id).Scan(&v); err != nil {
			t.Fatalf("net %s %s: %v", account, id, err)
		}
		net += v
	}
	return net
}

func t5510Code(err error) string {
	if verr, ok := err.(*ValidationError); ok {
		return verr.Code
	}
	return ""
}

func TestStage73PartialSettlement(t *testing.T) {
	db.InitDB(testConnStr())
	tenantID := "default"
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		t.Fatalf("schema: %v", err)
	}
	cleanup := func() {
		db.DB.Exec("DELETE FROM " + schema + ".gl_postings WHERE document_id LIKE 'T5510-%' OR document_type = 'T5510Other'")
		db.DB.Exec("DELETE FROM " + schema + ".documents WHERE id LIKE 'T5510-%'")
	}
	cleanup()
	defer cleanup()

	newPostedInvoice := func(id string, total float64) {
		t.Helper()
		t5510Doc(t, schema, id, "SalesInvoice", "Draft", map[string]interface{}{"code": id, "invoice_number": id, "customer": "T5510 Customer", "total_amount": total})
		if _, err := PostSalesInvoice(tenantID, id, "system"); err != nil {
			t.Fatalf("post %s: %v", id, err)
		}
	}

	t.Run("two part-receipts, a retry and a refusal, then the last receipt closes the invoice and 1300 nets to zero", func(t *testing.T) {
		inv := t5510 + "SI1"
		_, arBefore := runWithControls(t, tenantID, "receivables-ageing", RoleSuperAdmin, nil)
		newPostedInvoice(inv, 1000)

		res, err := SettleSalesInvoiceAmount(tenantID, inv, "system", 400, "R1")
		if err != nil || res.Status != "Approved" || !near(res.OutstandingAmount, 600) || !near(res.SettledAmount, 400) {
			t.Fatalf("first part-receipt: %+v %v", res, err)
		}
		// Receivables ageing, its drill-down and its control totals age the
		// outstanding 600, and still tie to 1300.
		_, arMid := runWithControls(t, tenantID, "receivables-ageing", RoleSuperAdmin, nil)
		if !near(arMid.SummaryTotal-arBefore.SummaryTotal, 600) || !near(*arMid.LedgerTotal-*arBefore.LedgerTotal, 600) ||
			!near(*arMid.SummaryVsLedger, *arBefore.SummaryVsLedger) || !near(arMid.DetailTotal, arMid.SummaryTotal) {
			t.Fatalf("receivables controls after a part-receipt: before %+v mid %+v", arBefore, arMid)
		}
		drill, err := ageingBucketDrillDown(tenantID, "SalesInvoice", "Approved", "total_amount", "0-30 days")
		if err != nil {
			t.Fatalf("drill: %v", err)
		}
		found := false
		for _, r := range drill {
			if r["id"] == inv {
				found = true
				if !near(r["outstanding_amount"].(float64), 600) {
					t.Fatalf("drill-down outstanding = %v, want 600", r["outstanding_amount"])
				}
			}
		}
		if !found {
			t.Fatalf("part-paid invoice missing from its bucket's drill-down")
		}

		// A retry with the same reference posts nothing.
		again, err := SettleSalesInvoiceAmount(tenantID, inv, "system", 400, "R1")
		if err != nil || !again.Replayed || !near(again.OutstandingAmount, 600) {
			t.Fatalf("retry: %+v %v", again, err)
		}
		var rows int
		db.DB.QueryRow("SELECT COUNT(*) FROM "+schema+".gl_postings WHERE idempotency_key = $1", "SalesInvoice:"+inv+":SETTLE:R1").Scan(&rows)
		if rows != 2 {
			t.Fatalf("retry posted again: %d rows under the R1 key, want 2", rows)
		}
		if _, err := SettleSalesInvoiceAmount(tenantID, inv, "system", 500, "R1"); t5510Code(err) != "FIN-0519" {
			t.Fatalf("same reference, different amount: want FIN-0519, got %v", err)
		}
		if _, err := SettleSalesInvoiceAmount(tenantID, inv, "system", 600.01, "R2"); t5510Code(err) != "FIN-0517" {
			t.Fatalf("over-settlement: want FIN-0517, got %v", err)
		}
		if _, err := SettleSalesInvoiceAmount(tenantID, inv, "system", 0, "bad ref!"); t5510Code(err) != "FIN-0520" {
			t.Fatalf("bad reference: want FIN-0520, got %v", err)
		}
		if res, err := SettleSalesInvoiceAmount(tenantID, inv, "system", 250.5, "R2"); err != nil || !near(res.OutstandingAmount, 349.5) {
			t.Fatalf("second part-receipt: %+v %v", res, err)
		}
		// The old whole-invoice call settles exactly what is left.
		amount, err := SettleSalesInvoice(tenantID, inv, "system")
		if err != nil || amount != 350 {
			t.Fatalf("final settlement: amount=%d err=%v (want 350, rounded from 349.50)", amount, err)
		}
		status, data := t5510Data(t, schema, inv)
		if status != "Paid" || numFromInterface(data["outstanding_amount"]) != 0 || !near(numFromInterface(data["settled_amount"]), 1000) {
			t.Fatalf("after the last receipt: status=%s data=%v", status, data)
		}
		if hist, _ := readSettlementEntries(data, "settlements"); len(hist) != 3 {
			t.Fatalf("settlement history has %d lines, want 3", len(hist))
		}
		if net := t5510Net(t, schema, "1300", inv); net != 0 {
			t.Fatalf("1300 for the invoice nets to %d paise, want 0", net)
		}
		if net := t5510Net(t, schema, "1100", inv); net != 100000 {
			t.Fatalf("cash received = %d paise, want 100000", net)
		}
		if _, err := SettleSalesInvoiceAmount(tenantID, inv, "system", 0, "R9"); err == nil {
			t.Fatalf("a Paid invoice accepted another receipt")
		}
		_, arAfter := runWithControls(t, tenantID, "receivables-ageing", RoleSuperAdmin, nil)
		if !near(arAfter.SummaryTotal, arBefore.SummaryTotal) || !near(*arAfter.SummaryVsLedger, *arBefore.SummaryVsLedger) {
			t.Fatalf("a settled invoice left a trace in receivables: before %+v after %+v", arBefore, arAfter)
		}
	})

	t.Run("a one-shot full settlement keeps its old posting key", func(t *testing.T) {
		inv := t5510 + "SI2"
		newPostedInvoice(inv, 700)
		if amount, err := SettleSalesInvoice(tenantID, inv, "system"); err != nil || amount != 700 {
			t.Fatalf("full settlement: %d %v", amount, err)
		}
		var rows int
		db.DB.QueryRow("SELECT COUNT(*) FROM "+schema+".gl_postings WHERE idempotency_key = $1", "SalesInvoice:"+inv+":SETTLE").Scan(&rows)
		if rows != 2 {
			t.Fatalf("legacy key rows = %d, want 2", rows)
		}
	})

	t.Run("a foreign-currency invoice settles in full only", func(t *testing.T) {
		inv := t5510 + "SI3"
		t5510Doc(t, schema, inv, "SalesInvoice", "Approved", map[string]interface{}{
			"code": inv, "customer": "T5510 Customer", "total_amount": 100, "currency": "USD", "exchange_rate": 83, "base_total_amount": 8300})
		if _, err := SettleSalesInvoiceAmount(tenantID, inv, "system", 40, "F1"); t5510Code(err) != "FIN-0518" {
			t.Fatalf("foreign part-receipt: want FIN-0518, got %v", err)
		}
	})

	t.Run("credit notes reduce the invoice they name, never below zero", func(t *testing.T) {
		inv := t5510 + "SI4"
		newPostedInvoice(inv, 1000)
		if _, err := SettleSalesInvoiceAmount(tenantID, inv, "system", 300, "C1"); err != nil {
			t.Fatalf("receipt: %v", err)
		}
		cn1, cn2, cn3 := t5510+"CN1", t5510+"CN2", t5510+"CN3"
		t5510Doc(t, schema, cn1, "CreditNote", "Draft", map[string]interface{}{"code": cn1, "note_number": cn1, "amount": 500, "reason": "test", "sales_invoice_id": inv})
		if _, err := PostCreditNote(tenantID, cn1, "system"); err != nil {
			t.Fatalf("post cn1: %v", err)
		}
		status, data := t5510Data(t, schema, inv)
		if status != "Approved" || !near(numFromInterface(data["outstanding_amount"]), 200) {
			t.Fatalf("after cn1: status=%s outstanding=%v", status, data["outstanding_amount"])
		}
		if n := t5510Net(t, schema, "1100", cn1); n != 0 {
			t.Fatalf("cn1 refunded cash %d paise, want none - it reduces the receivable", n)
		}
		// The second note is larger than what is left: 200 clears the
		// invoice, the other 100 is a refund.
		t5510Doc(t, schema, cn2, "CreditNote", "Draft", map[string]interface{}{"code": cn2, "note_number": cn2, "amount": 300, "reason": "test", "sales_invoice_id": inv})
		if _, err := PostCreditNote(tenantID, cn2, "system"); err != nil {
			t.Fatalf("post cn2: %v", err)
		}
		status, data = t5510Data(t, schema, inv)
		if status != "Paid" || numFromInterface(data["outstanding_amount"]) != 0 {
			t.Fatalf("after cn2: status=%s outstanding=%v", status, data["outstanding_amount"])
		}
		_, note := t5510Data(t, schema, cn2)
		if !near(numFromInterface(note["applied_to_invoice"]), 200) || !near(numFromInterface(note["refunded_amount"]), 100) {
			t.Fatalf("cn2 split: %v", note)
		}
		if net := t5510Net(t, schema, "1300", inv, cn1, cn2); net != 0 {
			t.Fatalf("1300 across invoice and notes nets to %d paise, want 0", net)
		}
		if n := t5510Net(t, schema, "1100", cn2); n != -10000 {
			t.Fatalf("cn2 refund = %d paise, want -10000", n)
		}
		// Against a Paid invoice a note is a plain refund, as before.
		t5510Doc(t, schema, cn3, "CreditNote", "Draft", map[string]interface{}{"code": cn3, "note_number": cn3, "amount": 50, "reason": "test", "sales_invoice_id": inv})
		if _, err := PostCreditNote(tenantID, cn3, "system"); err != nil {
			t.Fatalf("post cn3: %v", err)
		}
		if n := t5510Net(t, schema, "1100", cn3); n != -5000 {
			t.Fatalf("cn3 against a Paid invoice: cash %d paise, want -5000", n)
		}
		// A foreign open invoice refuses the note rather than mixing currencies.
		fx, cn4 := t5510+"SI5", t5510+"CN4"
		t5510Doc(t, schema, fx, "SalesInvoice", "Approved", map[string]interface{}{"code": fx, "total_amount": 100, "currency": "USD", "exchange_rate": 83, "base_total_amount": 8300})
		t5510Doc(t, schema, cn4, "CreditNote", "Draft", map[string]interface{}{"code": cn4, "note_number": cn4, "amount": 10, "reason": "test", "sales_invoice_id": fx})
		if _, err := PostCreditNote(tenantID, cn4, "system"); t5510Code(err) != "FIN-0522" {
			t.Fatalf("credit note on a foreign invoice: want FIN-0522, got %v", err)
		}
	})

	// A PO with 18% GST, a GRN receiving all of it, and the receipt's 2100
	// credit at taxable cost - what RecordGRNReceiptCosting posts.
	seedReceipt := func(n string) (poID, grnID string) {
		t.Helper()
		poID, grnID = t5510+"PO"+n, t5510+"GRN"+n
		items, _ := json.Marshal([]map[string]interface{}{{"sku": t5510 + "SKU", "qty": 10, "rate": 100}})
		t5510Doc(t, schema, poID, "PurchaseOrder", "Approved", map[string]interface{}{"code": poID, "total_amount": 1000, "grand_total": 1180, "items": string(items), "vendor_id": "T5510-V"})
		recv, _ := json.Marshal([]map[string]interface{}{{"sku": t5510 + "SKU", "qty": 10}})
		t5510Doc(t, schema, grnID, "GRN", "Received", map[string]interface{}{"code": grnID, "po_id": poID, "received_items": string(recv), "vendor": "T5510-V"})
		if err := PostDoubleEntry(tenantID, "GRN", grnID, map[string]int64{"1200": 100000}, map[string]int64{"2100": 100000}, "", "GRN:"+grnID+":RECEIPT_COST"); err != nil {
			t.Fatalf("receipt GL: %v", err)
		}
		return poID, grnID
	}
	newMatchedBill := func(n, poID, grnID, due string) string {
		t.Helper()
		id := t5510 + "VI" + n
		t5510Doc(t, schema, id, "VendorInvoice", "Draft", map[string]interface{}{"invoice_number": id, "vendor_id": "T5510-V", "po_id": poID, "grn_id": grnID, "invoice_amount": 1180, "due_date": due})
		if m, err := MatchVendorInvoice(tenantID, id, "", "", 2); err != nil || !m.Matched {
			t.Fatalf("match %s: %+v %v", id, m, err)
		}
		return id
	}

	t.Run("vendor part-payments clear 2100 and input GST exactly, and Payables Ageing ties to 2100 throughout", func(t *testing.T) {
		_, apBefore := runWithControls(t, tenantID, "payables-ageing", RoleSuperAdmin, nil)
		poID, grnID := seedReceipt("1")
		due := time.Now().AddDate(0, 0, -45).Format("2006-01-02")
		bill := newMatchedBill("1", poID, grnID, due)

		// Received, billed, nothing paid: the receipt's taxable cost is owed,
		// aged from the bill's due date 45 days ago.
		_, apMid := runWithControls(t, tenantID, "payables-ageing", RoleSuperAdmin, nil)
		if !apMid.Tied || !near(apMid.SummaryTotal-apBefore.SummaryTotal, 1000) || !near(*apMid.LedgerTotal-*apBefore.LedgerTotal, 1000) {
			t.Fatalf("payables after the receipt: before %+v mid %+v", apBefore, apMid)
		}
		drillHas := func(bucket string, want float64) {
			t.Helper()
			rows, err := payablesAgeingDrillDown(tenantID, bucket)
			if err != nil {
				t.Fatalf("payables drill: %v", err)
			}
			for _, r := range rows {
				if r["id"] == grnID {
					if !near(r["outstanding_amount"].(float64), want) || r["aged_from"] != "due date" {
						t.Fatalf("drill row %v, want outstanding %.2f aged from due date", r, want)
					}
					return
				}
			}
			t.Fatalf("GRN %s missing from the %s drill-down", grnID, bucket)
		}
		drillHas("31-60 days", 1000)

		res, err := PayVendorInvoiceAmount(tenantID, bill, "system", 500, "P1")
		if err != nil || res.Status != "Matched" || !near(res.OutstandingAmount, 680) {
			t.Fatalf("first part-payment: %+v %v", res, err)
		}
		// 500 splits 1000:180 -> 423.73 goods, 76.27 GST.
		if g, s := t5510Net(t, schema, "1500", bill), t5510Net(t, schema, "2100", bill); g != 7627 || s != 42373 {
			t.Fatalf("first split: 1500=%d 2100=%d paise, want 7627 / 42373", g, s)
		}
		drillHas("31-60 days", 576.27)
		if _, err := PayVendorInvoiceAmount(tenantID, bill, "system", 680.01, "P2"); t5510Code(err) != "FIN-0517" {
			t.Fatalf("over-payment: want FIN-0517, got %v", err)
		}
		if again, err := PayVendorInvoiceAmount(tenantID, bill, "system", 500, "P1"); err != nil || !again.Replayed {
			t.Fatalf("retry: %+v %v", again, err)
		}
		if _, err := PayVendorInvoiceAmount(tenantID, bill, "system", 333.33, "P2"); err != nil {
			t.Fatalf("second part-payment: %v", err)
		}
		// TDS is refused on a part-paid bill.
		if _, _, err := PayVendorInvoiceWithTDS(tenantID, bill, "T5510-NO-SECTION", "system"); t5510Code(err) != "FIN-0521" {
			t.Fatalf("TDS on a part-paid bill: want FIN-0521, got %v", err)
		}
		// The old whole-bill call (what a payment proposal runs) pays the rest.
		paid, pending, err := PayVendorInvoice(tenantID, bill, "system", "HR/Admin", "")
		if err != nil || pending || paid != 347 {
			t.Fatalf("final payment: paid=%d pending=%v err=%v (want 347, rounded from 346.67)", paid, pending, err)
		}
		status, data := t5510Data(t, schema, bill)
		if status != "Paid" || numFromInterface(data["outstanding_amount"]) != 0 || !near(numFromInterface(data["paid_amount"]), 1180) {
			t.Fatalf("after the last payment: %s %v", status, data)
		}
		if g, s, c := t5510Net(t, schema, "1500", bill), t5510Net(t, schema, "2100", bill), t5510Net(t, schema, "1100", bill); g != 18000 || s != 100000 || c != -118000 {
			t.Fatalf("legs after full payment: 1500=%d 2100=%d 1100=%d paise, want 18000 / 100000 / -118000", g, s, c)
		}
		// The receipt is cleared, so it drops out of Payables Ageing and the
		// report and ledger are back where they started.
		_, apAfter := runWithControls(t, tenantID, "payables-ageing", RoleSuperAdmin, nil)
		if !apAfter.Tied || !near(apAfter.SummaryTotal, apBefore.SummaryTotal) || !near(*apAfter.LedgerTotal, *apBefore.LedgerTotal) {
			t.Fatalf("payables after full payment: before %+v after %+v", apBefore, apAfter)
		}

		// A 2100 posting with no goods receipt behind it is its own open item.
		if err := PostDoubleEntry(tenantID, "T5510Other", t5510+"JV", map[string]int64{"1200": 12345}, map[string]int64{"2100": 12345}, "", ""); err != nil {
			t.Fatalf("unlinked 2100 posting: %v", err)
		}
		_, apOther := runWithControls(t, tenantID, "payables-ageing", RoleSuperAdmin, nil)
		if !apOther.Tied || !near(apOther.SummaryTotal-apBefore.SummaryTotal, 123.45) {
			t.Fatalf("unlinked 2100 posting not aged: before %+v after %+v", apBefore, apOther)
		}
	})

	t.Run("a whole-bill payment with no reference keeps its old key and records its history", func(t *testing.T) {
		poID, grnID := seedReceipt("2")
		bill := newMatchedBill("2", poID, grnID, "")
		if paid, _, err := PayVendorInvoice(tenantID, bill, "system", "HR/Admin", ""); err != nil || paid != 1180 {
			t.Fatalf("full payment: %d %v", paid, err)
		}
		var rows int
		db.DB.QueryRow("SELECT COUNT(*) FROM "+schema+".gl_postings WHERE idempotency_key = $1", "VendorInvoice:"+bill+":PAY").Scan(&rows)
		if rows != 3 {
			t.Fatalf("legacy key rows = %d, want 3 (2100, 1500, 1100)", rows)
		}
		_, data := t5510Data(t, schema, bill)
		if hist, _ := readSettlementEntries(data, "payments"); len(hist) != 1 || numFromInterface(data["outstanding_amount"]) != 0 {
			t.Fatalf("history after a whole-bill payment: %v", data["payments"])
		}
		if _, err := PayVendorInvoiceAmount(tenantID, bill, "system", 0, "P9"); err == nil {
			t.Fatalf("a Paid bill accepted another payment")
		}
	})

	t.Run("splitVendorPayment never strands a paisa", func(t *testing.T) {
		open := map[string]int64{"2100": 100000, "1500": 18000}
		total := int64(118000)
		var g, s int64
		for _, pay := range []int64{33333, 33333, 33333} {
			legs := splitVendorPayment(open, total, pay)
			open["2100"] -= legs["2100"]
			open["1500"] -= legs["1500"]
			total -= pay
			g += legs["1500"]
			s += legs["2100"]
		}
		last := splitVendorPayment(open, total, total)
		g += last["1500"]
		s += last["2100"]
		if g != 18000 || s != 100000 {
			t.Fatalf("three thirds then the rest: gst=%d goods=%d, want 18000 / 100000", g, s)
		}
	})
}
