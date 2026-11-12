package engines

// Stage 49.5.2 - failure injection over the engine commands that move stock or
// money. Each scenario names the command's significant writes; the runner in
// atomicity_fi_harness_test.go fails the command at each one and
// proves nothing was left behind, that the retry lands exactly once and that a
// replay and a race move nothing further.

import (
	"custom_erp/db"
	"custom_erp/internal/testfault"
	"encoding/json"
	"fmt"
	"testing"
)

// fiAdjustment seeds a Draft stock adjustment taking 3 of 10 off, built the way
// CreateInventoryAdjustment builds it, ready for PostInventoryAdjustment.
func fiAdjustment(t *testing.T) (*fiScenario, string) {
	e := fiNewEnv(t)
	e.seedItem(10)
	b, err := buildInventoryAdjustment("default", InventoryAdjustmentInput{
		Location: e.loc, Lines: []InventoryAdjustmentLine{{SKU: e.sku, Qty: -3, ReasonCode: "IA-DAMAGE"}},
	})
	if err != nil {
		t.Fatalf("build adjustment: %v", err)
	}
	id := "ADJ-" + e.marker
	lines, _ := json.Marshal(b.Lines)
	data, _ := json.Marshal(map[string]interface{}{
		"code": id, "location": b.Location, "items": string(lines), "loss_class": b.LossClass,
		"increase_value": b.Increase, "decrease_value": b.Decrease, "net_value": b.Increase - b.Decrease,
		"amount": b.Increase + b.Decrease, "requested_by": "fi", "status": "Draft",
	})
	e.doc(InventoryAdjustmentDoctype, id, "Draft", string(data))
	return &fiScenario{
		env: e,
		run: func() (string, error) { return "", PostInventoryAdjustment("default", id, "fi") },
		effect: func(t *testing.T) {
			if on, av, _ := e.avail(e.sku, e.loc); on != 7 || av != 7 {
				t.Errorf("stock after the adjustment: on hand %d available %d, want 7 / 7 (taken off once)", on, av)
			}
			if n := e.count("documents", `doctype = 'StockLedgerEntry' AND data::text LIKE '%'||$1||'%'`, e.marker); n != 1 {
				t.Errorf("stock ledger entries: %d, want 1", n)
			}
			if n := e.count("gl_postings", `document_id = $1`, id); n != 2 {
				t.Errorf("GL lines: %d, want 2 (one balanced pair)", n)
			}
			if s := e.docStatus(InventoryAdjustmentDoctype, id); s != "Posted" {
				t.Errorf("adjustment status %q, want Posted", s)
			}
		},
	}, id
}

var adjustmentSteps = []testfault.Step{
	{Label: "availability", Table: "inventory_availability"},
	{Label: "stock-ledger", Table: "documents", Doctype: "StockLedgerEntry", On: "INSERT"},
	{Label: "gl", Table: "gl_postings", On: "INSERT"},
	{Label: "document-posted", Table: "documents", Doctype: InventoryAdjustmentDoctype, On: "UPDATE", When: "NEW.status = 'Posted'"},
}

func TestAtomicityInventoryAdjustmentPost(t *testing.T) {
	fiMatrix(t, adjustmentSteps, func(t *testing.T) *fiScenario {
		s, _ := fiAdjustment(t)
		return s
	})
	t.Run("race", func(t *testing.T) {
		fiRace(t, func(t *testing.T) *fiScenario {
			s, _ := fiAdjustment(t)
			return s
		})
	})
}

// fiTransfer seeds an Approved transfer of 30 of 100 from e.loc to a second
// store, optionally already dispatched.
func fiTransfer(t *testing.T, dispatched bool) (*fiScenario, string, string) {
	e := fiNewEnv(t)
	e.seedItem(100)
	to := "FITO-" + e.marker
	e.doc("Location", to, "Active", fmt.Sprintf(`{"code":%q,"name":"FI to"}`, to))
	id := "TO-" + e.marker
	items, _ := json.Marshal([]map[string]interface{}{{"sku": e.sku, "qty": 30}})
	data, _ := json.Marshal(map[string]interface{}{
		"id": id, "transfer_number": id, "from_warehouse": e.loc, "to_warehouse": to,
		"status": "Approved", "items": string(items),
	})
	e.doc("TransferOrder", id, "Approved", string(data))
	if dispatched {
		if err := DispatchTransferOrder("default", id, "fi"); err != nil {
			t.Fatalf("fixture dispatch: %v", err)
		}
	}
	return &fiScenario{env: e}, id, to
}

func TestAtomicityTransferDispatch(t *testing.T) {
	build := func(t *testing.T) *fiScenario {
		s, id, _ := fiTransfer(t, false)
		e := s.env
		s.run = func() (string, error) { return "", DispatchTransferOrder("default", id, "fi") }
		s.tolerateReplayError = true // a second dispatch is refused: the order is no longer Approved
		s.effect = func(t *testing.T) {
			if on, av, tr := e.avail(e.sku, e.loc); on != 70 || av != 70 || tr != 30 {
				t.Errorf("source stock on hand %d available %d in transit %d, want 70 / 70 / 30 (dispatched once)", on, av, tr)
			}
			if st := e.docStatus("TransferOrder", id); st != "Dispatched" {
				t.Errorf("transfer status %q, want Dispatched", st)
			}
		}
		return s
	}
	fiMatrix(t, []testfault.Step{
		{Label: "availability", Table: "inventory_availability"},
		{Label: "document-dispatched", Table: "documents", Doctype: "TransferOrder", On: "UPDATE", When: "NEW.status = 'Dispatched'"},
	}, build)
	t.Run("race", func(t *testing.T) { fiRace(t, build) })
}

func TestAtomicityTransferReceive(t *testing.T) {
	build := func(t *testing.T) *fiScenario {
		s, id, to := fiTransfer(t, true)
		e := s.env
		req := TransferReceiptRequest{ReceiptKey: "RK-" + e.marker, Lines: []TransferReceiptLine{{Sku: e.sku, Qty: 30}}}
		s.run = func() (string, error) {
			r, err := ReceiveTransferOrderLines("default", id, "fi", req)
			if err != nil {
				return "", err
			}
			return fmt.Sprintf("%s/%d", r.Status, r.EventNo), nil
		}
		s.effect = func(t *testing.T) {
			if on, av, _ := e.avail(e.sku, to); on != 30 || av != 30 {
				t.Errorf("destination stock on hand %d available %d, want 30 / 30 (received once)", on, av)
			}
			if _, _, tr := e.avail(e.sku, e.loc); tr != 0 {
				t.Errorf("source still shows %d in transit, want 0", tr)
			}
			if st := e.docStatus("TransferOrder", id); st != "Received" {
				t.Errorf("transfer status %q, want Received", st)
			}
			if n := e.count("documents", `doctype = 'StockLedgerEntry' AND data::text LIKE '%TO-RECEIVE:'||$1||'%'`, id); n != 1 {
				t.Errorf("receipt ledger entries: %d, want 1", n)
			}
		}
		return s
	}
	fiMatrix(t, []testfault.Step{
		{Label: "availability", Table: "inventory_availability"},
		{Label: "document-received", Table: "documents", Doctype: "TransferOrder", On: "UPDATE", When: "NEW.status = 'Received'"},
		{Label: "stock-ledger", Table: "documents", Doctype: "StockLedgerEntry", On: "INSERT"},
	}, build)
	t.Run("race", func(t *testing.T) { fiRace(t, build) })
}

// fiVoucher seeds an Approved journal voucher of 500 between two accounts.
func fiVoucher(t *testing.T) *fiScenario {
	e := fiNewEnv(t)
	id := "JV-" + e.marker
	lines, _ := json.Marshal([]JournalVoucherLine{{AccountCode: "5190", Debit: 500}, {AccountCode: "1200", Credit: 500}})
	data, _ := json.Marshal(map[string]interface{}{
		"id": id, "code": id, "voucher_number": id, "voucher_date": BusinessToday(), "narration": "FI " + e.marker,
		"lines": string(lines), "total_amount": 500, "status": "Approved",
	})
	e.doc("JournalVoucher", id, "Approved", string(data))
	return &fiScenario{
		env:                 e,
		tolerateReplayError: true, // only an Approved voucher posts; a repeat is refused
		run:                 func() (string, error) { return "", fiPostVoucher(id) },
		effect: func(t *testing.T) {
			if n := e.count("gl_postings", `document_id = $1`, id); n != 2 {
				t.Errorf("GL lines: %d, want 2 (one balanced pair)", n)
			}
			if st := e.docStatus("JournalVoucher", id); st != "Posted" {
				t.Errorf("voucher status %q, want Posted", st)
			}
		},
	}
}

func TestAtomicityJournalVoucherPost(t *testing.T) {
	build := func(t *testing.T) *fiScenario { return fiVoucher(t) }
	fiMatrix(t, []testfault.Step{
		{Label: "gl", Table: "gl_postings", On: "INSERT"},
		{Label: "document-posted", Table: "documents", Doctype: "JournalVoucher", On: "UPDATE", When: "NEW.status = 'Posted'"},
	}, build)
	t.Run("race", func(t *testing.T) { fiRace(t, build) })
}

// fiGRN seeds a GRN of 10 received into an empty store.
func fiGRN(t *testing.T) *fiScenario {
	e := fiNewEnv(t)
	e.seedItem(0)
	id := "GRN-" + e.marker
	e.doc("GRN", id, "Draft", fmt.Sprintf(`{"code":%q,"location":%q}`, id, e.loc))
	items := []interface{}{map[string]interface{}{"sku": e.sku, "qty": 10.0, "accepted_qty": 10.0}}
	return &fiScenario{
		env:                 e,
		tolerateReplayError: true,
		run: func() (string, error) {
			_, err := PostGRNReceiptWithQC("default", e.loc, items, "fi", id)
			return "", err
		},
		effect: func(t *testing.T) {
			if on, av, _ := e.avail(e.sku, e.loc); on != 10 || av != 10 {
				t.Errorf("stock after the receipt: on hand %d available %d, want 10 / 10 (received once)", on, av)
			}
			if n := e.count("documents", `doctype = 'StockLedgerEntry' AND data::text LIKE '%'||$1||'%'`, e.marker); n != 1 {
				t.Errorf("stock ledger entries: %d, want 1", n)
			}
		},
	}
}

func TestAtomicityGRNReceipt(t *testing.T) {
	build := func(t *testing.T) *fiScenario { return fiGRN(t) }
	fiMatrix(t, []testfault.Step{
		{Label: "availability", Table: "inventory_availability"},
		{Label: "stock-ledger", Table: "documents", Doctype: "StockLedgerEntry", On: "INSERT"},
	}, build)
	t.Run("race", func(t *testing.T) { fiRace(t, build) })
}

// fiCreditNote seeds a Draft credit note of 100 that is not tied to an invoice.
func fiCreditNote(t *testing.T) *fiScenario {
	e := fiNewEnv(t)
	id := "CN-" + e.marker
	e.doc("CreditNote", id, "Draft", fmt.Sprintf(`{"code":%q,"amount":100,"status":"Draft","reason":"FI"}`, id))
	return &fiScenario{
		env:                 e,
		tolerateReplayError: true, // "already Posted" is the refusal
		run: func() (string, error) {
			n, err := PostCreditNote("default", id, "fi")
			return fmt.Sprint(n), err
		},
		effect: func(t *testing.T) {
			if n := e.count("gl_postings", `document_id = $1`, id); n != 2 {
				t.Errorf("GL lines: %d, want 2 (one balanced pair)", n)
			}
			if st := e.docStatus("CreditNote", id); st != "Posted" {
				t.Errorf("credit note status %q, want Posted", st)
			}
		},
	}
}

func TestAtomicityCreditNotePost(t *testing.T) {
	build := func(t *testing.T) *fiScenario { return fiCreditNote(t) }
	fiMatrix(t, []testfault.Step{
		{Label: "gl", Table: "gl_postings", On: "INSERT"},
		{Label: "document-posted", Table: "documents", Doctype: "CreditNote", On: "UPDATE", When: "NEW.status = 'Posted'"},
	}, build)
	t.Run("race", func(t *testing.T) { fiRace(t, build) })
}

// fiPostVoucher is RetryPostApprovedJournalVoucher's transaction without its
// error message: the wrapper replaces any failure with "posting is still
// blocked", which hides where it failed. The body (lock, status check,
// postJournalVoucherTx, commit) is the same.
func fiPostVoucher(id string) error {
	schema, err := db.GetTenantSchema("default")
	if err != nil {
		return err
	}
	tx, err := db.DB.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	data, status, err := lockJournalVoucherTx(tx, schema, id)
	if err != nil {
		return err
	}
	if status != "Approved" {
		return fmt.Errorf("only an Approved journal voucher posts (current status: %s)", status)
	}
	if err := postJournalVoucherTx(tx, "default", schema, id, data); err != nil {
		return err
	}
	return tx.Commit()
}

// fiVendorInvoice seeds a Matched vendor bill of 1000 ready to pay.
func fiVendorInvoice(t *testing.T) *fiScenario {
	e := fiNewEnv(t)
	id := "VI-" + e.marker
	e.doc("VendorInvoice", id, "Matched", fmt.Sprintf(`{"code":%q,"invoice_amount":1000,"status":"Matched","vendor_id":"FIV-%s"}`, id, e.marker))
	return &fiScenario{
		env:                 e,
		tolerateReplayError: true, // "already Paid" is the refusal
		run: func() (string, error) {
			n, _, err := PayVendorInvoice("default", id, "fi", "Super Admin", "")
			return fmt.Sprint(n), err
		},
		effect: func(t *testing.T) {
			if n := e.count("gl_postings", `document_id = $1`, id); n < 2 || n%2 != 0 {
				t.Errorf("GL lines: %d, want one balanced payment (an even number, not zero)", n)
			}
			if st := e.docStatus("VendorInvoice", id); st != "Paid" {
				t.Errorf("vendor invoice status %q, want Paid", st)
			}
		},
	}
}

func TestAtomicityVendorInvoicePay(t *testing.T) {
	build := func(t *testing.T) *fiScenario { return fiVendorInvoice(t) }
	fiMatrix(t, []testfault.Step{
		{Label: "gl", Table: "gl_postings", On: "INSERT"},
		{Label: "document-paid", Table: "documents", Doctype: "VendorInvoice", On: "UPDATE", When: "NEW.status = 'Paid'"},
	}, build)
	t.Run("race", func(t *testing.T) { fiRace(t, build) })
}
