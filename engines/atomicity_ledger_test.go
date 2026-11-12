package engines

// The ledger posts in the caller's transaction (docs/security/atomicity-failure-injection.md):
// each path that used to post the GL in a transaction of its own and then
// change a document in another is run through the same failure-injection matrix
// as the 49.5.2 commands - fail at each write, nothing changes, retry, effect
// exactly once, replay moves nothing.

import (
	"custom_erp/db"
	"custom_erp/internal/testfault"
	"encoding/json"
	"fmt"
	"testing"
)

// fiPrepaidStart: the upfront outlay of a prepaid schedule and the stamp that
// starts its recognition are one transaction.
func fiPrepaidStart(t *testing.T) *fiScenario {
	e := fiNewEnv(t)
	id := "PPS-" + e.marker
	e.doc("PrepaidExpenseSchedule", id, "Approved", fmt.Sprintf(`{"code":%q,"total_amount":1200,"term_months":12,"expense_account_code":"5190"}`, id))
	stamped, _ := json.Marshal(map[string]interface{}{
		"code": id, "total_amount": 1200, "term_months": 12, "expense_account_code": "5190",
		"recognized_months": 0, "next_recognition_date": "2099-01-01",
	})
	return &fiScenario{
		env: e,
		run: func() (string, error) {
			return "", postPrepaidScheduleTx("default", e.schema, id, 120000, stamped)
		},
		effect: func(t *testing.T) {
			if n := e.count("gl_postings", `document_id = $1`, id); n != 2 {
				t.Errorf("GL lines: %d, want 2 (one balanced pair)", n)
			}
			if n := e.count("documents", `id = $1 AND data->>'next_recognition_date' = '2099-01-01'`, id); n != 1 {
				t.Errorf("the recognition start was not stamped")
			}
		},
	}
}

func TestAtomicityPrepaidScheduleStart(t *testing.T) {
	build := func(t *testing.T) *fiScenario { return fiPrepaidStart(t) }
	fiMatrix(t, []testfault.Step{
		{Label: "gl", Table: "gl_postings", On: "INSERT"},
		{Label: "stamp", Table: "documents", Doctype: "PrepaidExpenseSchedule", On: "UPDATE"},
	}, build)
	t.Run("race", func(t *testing.T) { fiRace(t, build) })
}

// fiAmortization: one monthly recognition and the schedule's advance are one
// transaction, so a failed advance never leaves the month posted twice.
func fiAmortization(t *testing.T) *fiScenario {
	e := fiNewEnv(t)
	id := "DRS-" + e.marker
	e.doc("DeferredRevenueSchedule", id, "Active", fmt.Sprintf(`{"code":%q,"total_amount":1200,"term_months":12,"recognized_months":0,"next_recognition_date":"2000-01-01"}`, id))
	s := amortizationSchedule{id: id, totalAmount: 1200, termMonths: 12, recognizedMonths: 0}
	return &fiScenario{
		env: e,
		run: func() (string, error) {
			return "", recognizeAmortizationTx("default", e.schema, "DeferredRevenueSchedule", s,
				map[string]int64{"2600": 10000}, map[string]int64{"4100": 10000})
		},
		effect: func(t *testing.T) {
			if n := e.count("gl_postings", `document_id = $1`, id); n != 2 {
				t.Errorf("GL lines: %d, want 2 (one month recognised once)", n)
			}
			if n := e.count("documents", `id = $1 AND (data->>'recognized_months')::int = 1`, id); n != 1 {
				t.Errorf("the schedule did not advance to month 1")
			}
		},
	}
}

func TestAtomicityAmortizationRecognition(t *testing.T) {
	build := func(t *testing.T) *fiScenario { return fiAmortization(t) }
	fiMatrix(t, []testfault.Step{
		{Label: "gl", Table: "gl_postings", On: "INSERT"},
		{Label: "advance", Table: "documents", Doctype: "DeferredRevenueSchedule", On: "UPDATE"},
	}, build)
	t.Run("race", func(t *testing.T) { fiRace(t, build) })
}

// fiTillClose: a close within tolerance posts its cash variance in the same
// transaction as the status change; a variance that cannot post keeps the till
// open instead of closing it with the ledger short.
func fiTillClose(t *testing.T) *fiScenario {
	e := fiNewEnv(t)
	id := "POSSESS-" + e.marker
	e.doc("POSSession", id, "Open", fmt.Sprintf(`{"location":%q,"cashier":"fi-%s","opening_cash":100,"status":"Open"}`, e.loc, e.marker))
	cashier := "fi-" + e.marker
	return &fiScenario{
		env:                 e,
		tolerateReplayError: true, // "session is not open" is the refusal
		run: func() (string, error) {
			res, err := ClosePOSSession("default", id, cashier, 95, "")
			if err != nil {
				return "", err
			}
			return res.Status, nil
		},
		effect: func(t *testing.T) {
			if st := e.docStatus("POSSession", id); st != "Closed" {
				t.Errorf("session status %q, want Closed", st)
			}
			if n := e.count("gl_postings", `document_id = $1`, id); n != 2 {
				t.Errorf("GL lines: %d, want 2 (the 5 short, once)", n)
			}
		},
	}
}

func TestAtomicityTillCloseVariance(t *testing.T) {
	build := func(t *testing.T) *fiScenario {
		return fiTillClose(t)
	}
	fiMatrix(t, []testfault.Step{
		{Label: "gl", Table: "gl_postings", On: "INSERT"},
		{Label: "status", Table: "documents", Doctype: "POSSession", On: "UPDATE", When: "NEW.status = 'Closed'"},
	}, build)
	t.Run("race", func(t *testing.T) { fiRace(t, build) })
}

// fiEarlyDiscount: the early-payment discount posts with the bill's settlement
// record, not before it in a transaction of its own.
func fiEarlyDiscount(t *testing.T) *fiScenario {
	e := fiNewEnv(t)
	term, po, id := "PT-"+e.marker, "PO-"+e.marker, "VI-"+e.marker
	e.doc("PaymentTerm", term, "Active", fmt.Sprintf(`{"code":%q,"discount_days":10,"discount_pct":5}`, term))
	e.doc("PurchaseOrder", po, "Approved", fmt.Sprintf(`{"code":%q,"payment_term":%q}`, po, term))
	e.doc("VendorInvoice", id, "Matched", fmt.Sprintf(`{"code":%q,"invoice_amount":1000,"status":"Matched","vendor_id":"FIV-%s","po_id":%q,"invoice_date":%q}`, id, e.marker, po, BusinessToday()))
	return &fiScenario{
		env: e,
		run: func() (string, error) {
			_, err := applyEarlyPaymentDiscount("default", "fi", id)
			return "", err
		},
		tolerateReplayError: true,
		effect: func(t *testing.T) {
			if n := e.count("gl_postings", `document_id = $1 AND idempotency_key LIKE '%:EPD'`, id); n != 2 {
				t.Errorf("discount GL lines: %d, want 2 (taken once)", n)
			}
			if n := e.count("documents", `id = $1 AND data::text LIKE '%EPD:%'`, id); n != 1 {
				t.Errorf("the bill carries no early-payment settlement record")
			}
		},
	}
}

func TestAtomicityEarlyPaymentDiscount(t *testing.T) {
	build := func(t *testing.T) *fiScenario { return fiEarlyDiscount(t) }
	fiMatrix(t, []testfault.Step{
		{Label: "gl", Table: "gl_postings", On: "INSERT"},
		{Label: "bill-settlement", Table: "documents", Doctype: "VendorInvoice", On: "UPDATE"},
	}, build)
}

// fiGRNEvent: the inventory.stock_changed outbox row is written inside the
// receipt's transaction - the stock and its event stand or fall together.
func fiGRNEvent(t *testing.T) *fiScenario {
	e := fiNewEnv(t)
	e.seedItem(0)
	id := "GRN-" + e.marker
	e.doc("GRN", id, "Draft", fmt.Sprintf(`{"code":%q,"location":%q}`, id, e.loc))
	items := []interface{}{map[string]interface{}{"sku": e.sku, "qty": 10.0, "accepted_qty": 10.0}}
	return &fiScenario{
		env:                 e,
		tolerateReplayError: true,
		run: func() (string, error) {
			_, err := PostGRNReceiptAndPublish("default", e.loc, items, "fi", id)
			return "", err
		},
		effect: func(t *testing.T) {
			if on, _, _ := e.avail(e.sku, e.loc); on != 10 {
				t.Errorf("on hand %d, want 10 (received once)", on)
			}
			if n := e.count("integration_event_outbox", `event_name = 'inventory.stock_changed' AND payload->>'grn_id' = $1`, id); n != 1 {
				t.Errorf("stock-changed events: %d, want exactly 1", n)
			}
		},
	}
}

func TestAtomicityGRNStockChangedEvent(t *testing.T) {
	build := func(t *testing.T) *fiScenario { return fiGRNEvent(t) }
	fiMatrix(t, []testfault.Step{
		{Label: "availability", Table: "inventory_availability"},
		{Label: "event", Table: "integration_event_outbox", On: "INSERT"},
	}, build)
	t.Run("race", func(t *testing.T) { fiRace(t, build) })
}

// fiReverseJV: the reversal voucher and the original's reversed_by mark are one
// command - no orphan Draft reversal, and a repeat or a race raises one only.
func fiReverseJV(t *testing.T) *fiScenario {
	e := fiNewEnv(t)
	id := "JV-" + e.marker
	lines, _ := json.Marshal([]JournalVoucherLine{{AccountCode: "5190", Debit: 500}, {AccountCode: "1200", Credit: 500}})
	data, _ := json.Marshal(map[string]interface{}{
		"id": id, "code": id, "voucher_number": id, "voucher_date": BusinessToday(), "narration": "FI " + e.marker,
		"lines": string(lines), "total_amount": 500, "status": "Posted",
	})
	e.doc("JournalVoucher", id, "Posted", string(data))
	return &fiScenario{
		env:                 e,
		tolerateReplayError: true, // "already being reversed" is the refusal
		run: func() (string, error) {
			_, err := ReverseJournalVoucher("default", id, "manager1")
			return "", err
		},
		effect: func(t *testing.T) {
			if n := e.count("documents", `doctype = 'JournalVoucher' AND data->>'reversed_from' = $1`, id); n != 1 {
				t.Errorf("reversal vouchers: %d, want exactly 1", n)
			}
			if n := e.count("documents", `doctype = 'JournalVoucher' AND id = $1 AND COALESCE(data->>'reversed_by', '') <> ''`, id); n != 1 {
				t.Errorf("the original is not marked as being reversed")
			}
		},
	}
}

func TestAtomicityReverseJournalVoucher(t *testing.T) {
	build := func(t *testing.T) *fiScenario { return fiReverseJV(t) }
	fiMatrix(t, []testfault.Step{
		{Label: "reversal-voucher", Table: "documents", Doctype: "JournalVoucher", On: "INSERT"},
		{Label: "original-marked", Table: "documents", Doctype: "JournalVoucher", On: "UPDATE"},
	}, build)
	t.Run("race", func(t *testing.T) { fiRace(t, build) })
}

// fiVendorCreditNote: a vendor credit note's goods and GST postings and its
// Posted stamp are one transaction.
func fiVendorCreditNote(t *testing.T) *fiScenario {
	e := fiNewEnv(t)
	id := "VCN-" + e.marker
	e.doc("VendorCreditNote", id, "Draft", fmt.Sprintf(`{"code":%q,"amount":100,"gst_amount":18,"status":"Draft"}`, id))
	return &fiScenario{
		env:                 e,
		tolerateReplayError: true, // "already Posted" is the refusal
		run: func() (string, error) {
			n, err := PostVendorCreditNote("default", id, "fi")
			return fmt.Sprint(n), err
		},
		effect: func(t *testing.T) {
			if n := e.count("gl_postings", `document_id = $1`, id); n != 4 {
				t.Errorf("GL lines: %d, want 4 (goods pair and GST pair)", n)
			}
			if st := e.docStatus("VendorCreditNote", id); st != "Posted" {
				t.Errorf("vendor credit note status %q, want Posted", st)
			}
		},
	}
}

func TestAtomicityVendorCreditNotePost(t *testing.T) {
	build := func(t *testing.T) *fiScenario { return fiVendorCreditNote(t) }
	fiMatrix(t, []testfault.Step{
		{Label: "gl", Table: "gl_postings", On: "INSERT"},
		{Label: "document-posted", Table: "documents", Doctype: "VendorCreditNote", On: "UPDATE", When: "NEW.status = 'Posted'"},
	}, build)
	t.Run("race", func(t *testing.T) { fiRace(t, build) })
}

// fiPayslipPost: a payroll run's payslip posts its journal and its Posted stamp
// in one transaction (the Draft is made by RunPayroll in a single write).
func fiPayslipPost(t *testing.T) *fiScenario {
	e := fiNewEnv(t)
	emp, id := "EMP-"+e.marker, "PS-"+e.marker
	e.doc("Employee", emp, "Active", fmt.Sprintf(`{"name":"FI","bank_account_no":"000111","code":%q}`, emp))
	e.doc("Payslip", id, "Draft", fmt.Sprintf(`{"code":%q,"employee_id":%q,"period_from":%q,"period_to":%q,"gross_pay":1000,"pf_deduction":120,"pt_deduction":20,"net_pay":860}`, id, emp, BusinessToday(), BusinessToday()))
	return &fiScenario{
		env:                 e,
		tolerateReplayError: true, // "only a Draft payslip can be posted" is the refusal
		run:                 func() (string, error) { return "", PostPayslipToGL("default", id, "fi") },
		effect: func(t *testing.T) {
			if n := e.count("gl_postings", `document_id = $1`, id); n != 4 {
				t.Errorf("GL lines: %d, want 4 (gross, net, PF, PT)", n)
			}
			if st := e.docStatus("Payslip", id); st != "Posted" {
				t.Errorf("payslip status %q, want Posted", st)
			}
		},
	}
}

func TestAtomicityPayslipPost(t *testing.T) {
	build := func(t *testing.T) *fiScenario { return fiPayslipPost(t) }
	fiMatrix(t, []testfault.Step{
		{Label: "gl", Table: "gl_postings", On: "INSERT"},
		{Label: "document-posted", Table: "documents", Doctype: "Payslip", On: "UPDATE", When: "NEW.status = 'Posted'"},
	}, build)
	t.Run("race", func(t *testing.T) { fiRace(t, build) })
}

// fiCycleCount: a posted count variance moves the stock, books its value,
// writes the ledger line and stamps the line Posted in one transaction.
func fiCycleCount(t *testing.T) *fiScenario {
	e := fiNewEnv(t)
	e.seedItem(10)
	id := "CCL-" + e.marker
	e.doc("CycleCountLine", id, "Approved", fmt.Sprintf(`{"code":%q,"sku":%q,"location":%q,"system_qty":10,"counted_qty":8,"variance":-2,"variance_reason_code":"DAMAGE","status":"Approved"}`, id, e.sku, e.loc))
	return &fiScenario{
		env:                 e,
		tolerateReplayError: true, // "already Posted" is the refusal
		run:                 func() (string, error) { return "", PostCycleCountAdjustment("default", id, "fi") },
		effect: func(t *testing.T) {
			if on, _, _ := e.avail(e.sku, e.loc); on != 8 {
				t.Errorf("on hand %d, want 8 (the variance of -2 applied once)", on)
			}
			if n := e.count("gl_postings", `document_id = $1`, id); n != 2 {
				t.Errorf("GL lines: %d, want 2 (the shortage, once)", n)
			}
			if n := e.count("documents", `doctype = 'StockLedgerEntry' AND data->>'voucher_id' = $1`, id); n != 1 {
				t.Errorf("stock ledger entries: %d, want 1", n)
			}
			if st := e.docStatus("CycleCountLine", id); st != "Posted" {
				t.Errorf("count line status %q, want Posted", st)
			}
		},
	}
}

func TestAtomicityCycleCountPost(t *testing.T) {
	build := func(t *testing.T) *fiScenario { return fiCycleCount(t) }
	fiMatrix(t, []testfault.Step{
		{Label: "availability", Table: "inventory_availability"},
		{Label: "gl", Table: "gl_postings", On: "INSERT"},
		{Label: "stock-ledger", Table: "documents", Doctype: "StockLedgerEntry", On: "INSERT"},
		{Label: "line-posted", Table: "documents", Doctype: "CycleCountLine", On: "UPDATE", When: "NEW.status = 'Posted'"},
	}, build)
	t.Run("race", func(t *testing.T) { fiRace(t, build) })
}

// fiPurchaseReturn: 3 accepted units go back to the vendor - stock out, ledger,
// debit note and the Posted stamp in one transaction.
func fiPurchaseReturn(t *testing.T) *fiScenario {
	e := fiNewEnv(t)
	e.seedItem(0)
	vendor, po, grn, ret := "FIV-"+e.marker, "PO-"+e.marker, "GRN-"+e.marker, "PRET-"+e.marker
	e.doc("Vendor", vendor, "Active", fmt.Sprintf(`{"code":%q,"name":"FI vendor"}`, vendor))
	poItems, _ := json.Marshal([]map[string]interface{}{{"sku": e.sku, "qty": 10, "rate": 100.0}})
	e.doc("PurchaseOrder", po, "Approved", fmt.Sprintf(`{"code":%q,"vendor":%q,"gst_mode":%q,"total_amount":1000,"grand_total":1180,"items":%q}`, po, vendor, GSTModeExclusive, string(poItems)))
	recv, _ := json.Marshal([]map[string]interface{}{{"sku": e.sku, "qty": 10, "accepted_qty": 10}})
	e.doc("GRN", grn, "Active", fmt.Sprintf(`{"code":%q,"po_id":%q,"location":%q,"received_items":%q}`, grn, po, e.loc, string(recv)))
	if _, err := PostGRNReceiptWithQC("default", e.loc, []interface{}{map[string]interface{}{"sku": e.sku, "qty": 10.0, "accepted_qty": 10.0}}, "fi", grn); err != nil {
		t.Fatalf("fixture receipt: %v", err)
	}
	items, _ := json.Marshal([]map[string]interface{}{{"sku": e.sku, "stock_bucket": "Accepted", "qty": 3}})
	e.doc("PurchaseReturn", ret, "Draft", fmt.Sprintf(`{"code":%q,"grn_id":%q,"reason":"FI wrong size","status":"Draft","vendor_id":%q,"location":%q,"return_items":%q}`, ret, grn, vendor, e.loc, string(items)))
	return &fiScenario{
		env:                 e,
		tolerateReplayError: true,                          // PURCHA-0119 "already posted" is the refusal
		skip:                []string{"sequence_counters"}, // the debit note number is drawn before the post; a failure leaves a numbering gap, nothing else
		run: func() (string, error) {
			_, err := PostPurchaseReturn("default", ret, "manager1", "HQ")
			return "", err
		},
		effect: func(t *testing.T) {
			if on, _, _ := e.avail(e.sku, e.loc); on != 7 {
				t.Errorf("on hand %d, want 7 (3 of 10 returned once)", on)
			}
			if n := e.count("documents", `doctype = 'StockLedgerEntry' AND data->>'voucher_id' = $1`, ret); n != 1 {
				t.Errorf("return stock ledger entries: %d, want 1", n)
			}
			if st := e.docStatus("PurchaseReturn", ret); st != "Posted" {
				t.Errorf("purchase return status %q, want Posted", st)
			}
		},
	}
}

func TestAtomicityPurchaseReturnPost(t *testing.T) {
	build := func(t *testing.T) *fiScenario { return fiPurchaseReturn(t) }
	fiMatrix(t, []testfault.Step{
		{Label: "availability", Table: "inventory_availability"},
		{Label: "stock-ledger", Table: "documents", Doctype: "StockLedgerEntry", On: "INSERT"},
		{Label: "return-posted", Table: "documents", Doctype: "PurchaseReturn", On: "UPDATE", When: "NEW.status = 'Posted'"},
	}, build)
	t.Run("race", func(t *testing.T) { fiRace(t, build) })
}

// fiCancelInvoice: cancelling an Approved sales invoice reverses its GL and
// stamps the invoice Cancelled in one transaction.
func fiCancelInvoice(t *testing.T) *fiScenario {
	e := fiNewEnv(t)
	id := "SI-" + e.marker
	e.doc("SalesInvoice", id, "Approved", fmt.Sprintf(`{"code":%q,"customer":"FI buyer","status":"Approved","grand_total":118}`, id))
	if err := PostDoubleEntry("default", "SalesInvoice", id, map[string]int64{"1200": 11800}, map[string]int64{"2100": 11800}, "", "SalesInvoice:"+id); err != nil {
		t.Fatalf("fixture posting: %v", err)
	}
	return &fiScenario{
		env: e,
		run: func() (string, error) { return "", CancelSalesInvoice("default", id, "manager1", "FI cancel") },
		effect: func(t *testing.T) {
			var dr, cr int64
			_ = db.DB.QueryRow(`SELECT COALESCE(SUM(debit),0), COALESCE(SUM(credit),0) FROM `+e.schema+`.gl_postings WHERE document_id = $1`, id).Scan(&dr, &cr)
			if dr != 23600 || cr != 23600 {
				t.Errorf("GL after cancel: debit %d credit %d, want 23600 each (posting plus one reversal)", dr, cr)
			}
			if st := e.docStatus("SalesInvoice", id); st != "Cancelled" {
				t.Errorf("invoice status %q, want Cancelled", st)
			}
		},
	}
}

func TestAtomicityCancelSalesInvoice(t *testing.T) {
	build := func(t *testing.T) *fiScenario { return fiCancelInvoice(t) }
	fiMatrix(t, []testfault.Step{
		{Label: "gl", Table: "gl_postings", On: "INSERT"},
		{Label: "document-cancelled", Table: "documents", Doctype: "SalesInvoice", On: "UPDATE", When: "NEW.status = 'Cancelled'"},
	}, build)
	t.Run("race", func(t *testing.T) { fiRace(t, build) })
}

// TestAtomicityPeriodClose: closing a period is the status change and its log
// line in one transaction; a failed close leaves the period Open, a replay is
// refused and adds no second log line. (Test only - period close is unchanged.)
func TestAtomicityPeriodClose(t *testing.T) {
	e := fiNewEnv(t)
	var periodID string
	name := "FI period " + e.marker
	if err := db.DB.QueryRow(`INSERT INTO `+e.schema+`.accounting_periods (period_name, start_date, end_date, created_by) VALUES ($1, '1990-01-01', '1990-01-31', 'manager1') RETURNING id`, name).Scan(&periodID); err != nil {
		t.Fatalf("fixture period: %v", err)
	}
	t.Cleanup(func() {
		_, _ = db.DB.Exec(`DELETE FROM `+e.schema+`.period_close_log WHERE period_id = $1`, periodID)
		_, _ = db.DB.Exec(`DELETE FROM `+e.schema+`.accounting_periods WHERE id = $1`, periodID)
	})
	if err := ensurePeriodCloseTasks(db.DB, e.schema, periodID, 1); err != nil {
		t.Fatal(err)
	}
	e.exec(`UPDATE ` + e.schema + `.period_close_tasks SET status = 'Done', owner = 'manager1', signed_by = 'manager1', signed_at = CURRENT_TIMESTAMP WHERE period_id = '` + periodID + `'`)
	status := func() string {
		var s string
		_ = db.DB.QueryRow(`SELECT status FROM `+e.schema+`.accounting_periods WHERE id = $1`, periodID).Scan(&s)
		return s
	}
	logged := func() int {
		var n int
		_ = db.DB.QueryRow(`SELECT COUNT(*) FROM `+e.schema+`.period_close_log WHERE period_id = $1 AND event = 'CLOSED'`, periodID).Scan(&n)
		return n
	}

	disarm := testfault.Arm(t, db.DB, e.schema, testfault.Step{Label: "period-closed", Table: "accounting_periods", On: "UPDATE", When: "NEW.status = 'Closed'"}, e.marker)
	err := CloseAccountingPeriod("default", periodID, "manager1")
	if !testfault.Hit(err, "period-closed") {
		t.Fatalf("close at the injected write: %v", err)
	}
	if status() != "Open" || logged() != 0 {
		t.Errorf("after the failed close: status %q, close log lines %d, want Open and 0", status(), logged())
	}
	disarm()
	if err := CloseAccountingPeriod("default", periodID, "manager1"); err != nil {
		t.Fatalf("retry: %v", err)
	}
	if status() != "Closed" || logged() != 1 {
		t.Errorf("after the retry: status %q, close log lines %d, want Closed and 1", status(), logged())
	}
	if err := CloseAccountingPeriod("default", periodID, "manager1"); err == nil {
		t.Error("a second close was accepted")
	}
	if logged() != 1 {
		t.Errorf("replay added a close log line: %d", logged())
	}
}

// fiRefund: processing an approved refund posts the revenue reversal and marks
// the request Processed in one transaction (47.4.4); a replay is refused.
func fiRefund(t *testing.T) *fiScenario {
	e := fiNewEnv(t)
	id := "RF-" + e.marker
	e.doc("RefundRequest", id, "Approved", fmt.Sprintf(`{"code":%q,"status":"Approved","amount":100}`, id))
	return &fiScenario{
		env:                 e,
		tolerateReplayError: true, // "is not Approved (currently Processed)" is the refusal
		run:                 func() (string, error) { return "", ProcessRefundRequest("default", id, "manager1", "Cash") },
		effect: func(t *testing.T) {
			var dr, cr int64
			_ = db.DB.QueryRow(`SELECT COALESCE(SUM(debit),0), COALESCE(SUM(credit),0) FROM `+e.schema+`.gl_postings WHERE document_id = $1`, id).Scan(&dr, &cr)
			if dr != 10000 || cr != 10000 {
				t.Errorf("refund GL: debit %d credit %d, want 10000 each (once)", dr, cr)
			}
			if st := e.docStatus("RefundRequest", id); st != "Processed" {
				t.Errorf("refund status %q, want Processed", st)
			}
		},
	}
}

func TestAtomicityProcessRefund(t *testing.T) {
	build := func(t *testing.T) *fiScenario { return fiRefund(t) }
	fiMatrix(t, []testfault.Step{
		{Label: "gl", Table: "gl_postings", On: "INSERT"},
		{Label: "document-processed", Table: "documents", Doctype: "RefundRequest", On: "UPDATE", When: "NEW.status = 'Processed'"},
	}, build)
	t.Run("race", func(t *testing.T) { fiRace(t, build) })
}
