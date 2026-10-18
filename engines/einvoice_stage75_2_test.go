package engines

import (
	"custom_erp/db"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
	"sync"
	"testing"
	"time"
)

// Stage 75.2 (75.2.1-75.2.10) against the fake provider, on a throwaway
// tenant: eligibility, the frozen canonical document, validation, the queue
// (UNKNOWN looked up before any retry, one submission across claims and a
// restart mid-send), rejection and re-validation, the lock after
// acknowledgement, cancel under maker-checker, the 24-hour window, a number
// never reused, an expired token, and no token in the attempt log.
func TestStage752EInvoice(t *testing.T) {
	db.InitDB(testConnStr())
	tenantID, schema := uniqueLifecycleTenant(t)
	defer dropLifecycleTenant(tenantID, schema)
	if _, err := ProvisionTenantSchema(tenantID, schema, "0.1.0-test"); err != nil {
		t.Fatalf("ProvisionTenantSchema: %v", err)
	}
	if sql, err := os.ReadFile("../db/migrations_stage75_2_einvoice.sql"); err == nil {
		if _, err := db.DB.Exec(string(sql)); err != nil {
			t.Fatalf("e-invoice migration: %v", err)
		}
	}
	kick := irpKickWorker
	irpKickWorker = func(string) {}
	defer func() { irpKickWorker = kick }()
	FakeIRP.Reset()

	code := func(err error) string {
		var v *ValidationError
		if errors.As(err, &v) {
			return v.Code
		}
		return ""
	}
	exec := func(q string, args ...interface{}) {
		t.Helper()
		if _, err := db.DB.Exec(q, args...); err != nil {
			t.Fatalf("%s: %v", q, err)
		}
	}
	doc := func(id, doctype, status string, data map[string]interface{}) {
		t.Helper()
		b, _ := json.Marshal(data)
		exec("INSERT INTO "+schema+".documents (id, doctype, data, status, created_by) VALUES ($1,$2,$3,$4,'tester')", id, doctype, b, status)
	}
	const seller, buyer, buyerOut, samePAN = "29AAACE7520K1ZI", "29BBBCF1234L1ZK", "27BBBCF1234L1ZO", "29AAACE7520K2ZH"
	doc("LE-T", "LegalEntity", "Active", map[string]interface{}{"code": "LE-T", "name": "Test Seller Pvt Ltd", "gstin": seller, "state": "Karnataka", "address": "1 MG Road", "city": "Bengaluru", "pincode": "560001"})
	doc("LOC-T", "Location", "Active", map[string]interface{}{"code": "LOC-T", "name": "Test Store", "legal_entity": "LE-T", "address": "1 MG Road", "city": "Bengaluru", "pincode": "560001", "state": "Karnataka"})
	customer := func(id, gstin string) {
		doc(id, "Customer", "Active", map[string]interface{}{"code": id, "name": "Buyer " + id, "gstin": gstin, "address": "2 Brigade Road", "city": "Bengaluru", "pincode": "560025", "state": "Karnataka"})
	}
	customer("C-B2B", buyer)
	customer("C-OUT", buyerOut)
	customer("C-SAME", samePAN)
	customer("C-B2C", "")
	doc("SKU-T1", "Item", "Active", map[string]interface{}{"code": "SKU-T1", "name": "Cotton shirt", "hsn_code": "610910", "gst_rate": 5, "uom": "PCS"})
	doc("SKU-NOHSN", "Item", "Active", map[string]interface{}{"code": "SKU-NOHSN", "name": "Unclassified", "gst_rate": 5, "uom": "PCS"})
	invoice := func(id, number, cust, sku string) {
		doc(id, "SalesInvoice", "Approved", map[string]interface{}{"invoice_number": number, "customer": cust, "location": "LOC-T",
			"items": []map[string]interface{}{{"sku": sku, "qty": 2, "rate": 1050}}, "total_amount": 2100, "taxable_amount": 2000, "cgst": 50, "sgst": 50, "total_tax": 100})
	}
	invoice("SI-1", "SI/T/2627/0001", "C-B2B", "SKU-T1")
	invoice("SI-2", "SI/T/2627/0002", "C-B2C", "SKU-T1")
	invoice("SI-3", "SI/T/2627/0003", "C-B2B", "SKU-NOHSN")
	invoice("SI-4", "SI/T/2627/0004", "C-B2B", "SKU-T1")
	invoice("SI-5", "SI/T/2627/0005", "C-OUT", "SKU-T1")
	invoice("SI-6", "SI/T/2627/0006", "C-SAME", "SKU-T1")

	// 75.2.1: a profile; same-PAN supplies not e-invoiced under these rules.
	if _, err := SaveIRPProfile(tenantID, IRPProfile{GSTIN: "29AAACE7520K1ZX"}, nil, "admin"); code(err) != "EINV-0980" {
		t.Fatalf("a GSTIN with a wrong check digit: want EINV-0980, got %v", err)
	}
	p, err := SaveIRPProfile(tenantID, IRPProfile{GSTIN: seller, Enabled: true, Provider: "fake", InterGSTIN: "NOT_REQUIRED",
		EffectiveFrom: time.Now().AddDate(0, 0, -1).Format("2006-01-02")}, nil, "admin")
	if err != nil || p.RuleVersion != 1 || p.AutoSubmit {
		t.Fatalf("profile: %+v err %v (auto-submit must start off)", p, err)
	}

	if err := RunEInvoiceWorkerOnce(tenantID); err != nil {
		t.Fatal(err)
	}
	rec := func(id string) IRPRecord {
		t.Helper()
		r, err := GetIRPRecordFor(tenantID, "SalesInvoice", id)
		if err != nil || r == nil {
			t.Fatalf("%s: no e-invoice record (%v)", id, err)
		}
		return *r
	}
	if r := rec("SI-2"); r.State != IRPStateNotRequired || !strings.Contains(r.Reason, "B2C") {
		t.Fatalf("B2C: want NOT_REQUIRED (B2C), got %s %q", r.State, r.Reason)
	}
	if r := rec("SI-6"); r.State != IRPStateNotRequired || !strings.Contains(r.Reason, "same PAN") {
		t.Fatalf("same PAN under NOT_REQUIRED rules: got %s %q", r.State, r.Reason)
	}
	r1 := rec("SI-1")
	if r1.State != IRPStateReady || len(r1.Issues) != 0 || len(r1.PayloadHash) != 64 || r1.RuleVersion != 1 {
		t.Fatalf("SI-1: want READY, valid, hashed, rules v1 - got %+v", r1)
	}
	// 75.2.2: the frozen payload.
	var payload IRPDocument
	var raw string
	db.DB.QueryRow("SELECT payload::text FROM "+schema+".irp_documents WHERE id = $1", r1.ID).Scan(&raw)
	json.Unmarshal([]byte(raw), &payload)
	if payload.ValDtls.AssVal != 2000 || payload.ValDtls.CgstVal != 50 || payload.ValDtls.SgstVal != 50 || payload.ValDtls.TotInvVal != 2100 ||
		payload.ItemList[0].HsnCd != "610910" || payload.SellerDtls.Stcd != "29" || payload.BuyerDtls.Pos != "29" || payload.DocDtls.No != "SI/T/2627/0001" {
		t.Fatalf("canonical payload: %s", raw)
	}
	if r := rec("SI-5"); r.State != IRPStateReady || len(r.Issues) != 0 {
		t.Fatalf("interstate SI-5: %+v", r)
	} else {
		db.DB.QueryRow("SELECT payload::text FROM "+schema+".irp_documents WHERE id = $1", r.ID).Scan(&raw)
		var out IRPDocument
		json.Unmarshal([]byte(raw), &out)
		if out.ValDtls.IgstVal != 100 || out.ValDtls.CgstVal != 0 {
			t.Fatalf("interstate: want IGST 100, got %+v", out.ValDtls)
		}
	}
	// 75.2.3: validation names the field and the fix; such a document is not sent.
	r3 := rec("SI-3")
	if len(r3.Issues) == 0 || !strings.Contains(r3.Issues[0].Field, "HSN") {
		t.Fatalf("SI-3 without HSN: want an HSN issue, got %+v", r3.Issues)
	}
	if _, err := SubmitEInvoice(tenantID, r3.ID, "acc"); code(err) != "EINV-0982" {
		t.Fatalf("submitting an invalid document: want EINV-0982, got %v", err)
	}

	// 75.2.6: timeout after the IRP accepted -> UNKNOWN -> looked up, never re-sent.
	FakeIRP.Script("SI/T/2627/0001", FakeIRPTimeoutAccepted)
	if _, err := SubmitEInvoice(tenantID, r1.ID, "acc"); err != nil {
		t.Fatal(err)
	}
	RunEInvoiceWorkerOnce(tenantID)
	if r := rec("SI-1"); r.State != IRPStateUnknown {
		t.Fatalf("after a timeout: want UNKNOWN, got %s", r.State)
	}
	exec("UPDATE "+schema+".irp_documents SET next_attempt_at = NOW() WHERE id = $1", r1.ID)
	RunEInvoiceWorkerOnce(tenantID)
	r1 = rec("SI-1")
	if r1.State != IRPStateAcknowledged || len(r1.IRN) != 64 || r1.AckNo == "" || r1.SignedQR == "" || FakeIRP.GenerateCalls("SI/T/2627/0001") != 1 {
		t.Fatalf("after the lookup: want ACKNOWLEDGED with IRN and one generate call, got %s irn %q calls %d", r1.State, r1.IRN, FakeIRP.GenerateCalls("SI/T/2627/0001"))
	}

	// 75.2.7: the IRN locks the tax fields; other fields still save.
	prior := map[string]interface{}{"customer": "C-B2B", "total_amount": 2100.0, "due_date": "2026-11-01"}
	if err := ValidateTransactionalRules(tenantID, "SalesInvoice", "SI-1", "Approved", prior, map[string]interface{}{"customer": "C-OUT", "status": "Approved", "edit_reason": "test"}); code(err) != "EINV-0983" {
		t.Fatalf("changing an acknowledged invoice's customer: want EINV-0983, got %v", err)
	}
	if err := validateEInvoiceLock(tenantID, "SalesInvoice", "SI-1", prior, map[string]interface{}{"due_date": "2026-12-01", "customer": "C-B2B"}); err != nil {
		t.Fatalf("a due date may still change: %v", err)
	}

	// RET-03 / RET-04: a claimed row is not claimed again; a send cut off by a
	// restart becomes UNKNOWN and is looked up first.
	r4 := rec("SI-4")
	SubmitEInvoice(tenantID, r4.ID, "acc")
	var first, second []int64
	var wg sync.WaitGroup
	wg.Add(2)
	go func() { defer wg.Done(); first = claimDueIRPDocuments(schema) }()
	go func() { defer wg.Done(); second = claimDueIRPDocuments(schema) }()
	wg.Wait()
	if len(first)+len(second) != 1 {
		t.Fatalf("two claimers: want the row claimed once, got %v and %v", first, second)
	}
	exec("UPDATE "+schema+".irp_documents SET lease_until = NOW() - INTERVAL '1 second' WHERE id = $1", r4.ID) // the worker died mid-send
	RunEInvoiceWorkerOnce(tenantID)                                                                            // -> UNKNOWN -> lookup: not registered -> QUEUED
	exec("UPDATE "+schema+".irp_documents SET next_attempt_at = NOW() WHERE id = $1", r4.ID)
	RunEInvoiceWorkerOnce(tenantID)
	if r := rec("SI-4"); r.State != IRPStateAcknowledged || FakeIRP.GenerateCalls("SI/T/2627/0004") != 1 {
		t.Fatalf("restart mid-send: want ACKNOWLEDGED after exactly one send, got %s, %d sends", r.State, FakeIRP.GenerateCalls("SI/T/2627/0004"))
	}

	// Rejection -> fix -> Validate -> resubmit; an expired token is refreshed.
	r5 := rec("SI-5")
	FakeIRP.Script("SI/T/2627/0005", FakeIRPReject)
	SubmitEInvoice(tenantID, r5.ID, "acc")
	RunEInvoiceWorkerOnce(tenantID)
	if r := rec("SI-5"); r.State != IRPStateRejected || r.LastError == "" {
		t.Fatalf("scripted rejection: want REJECTED with the error, got %s %q", r.State, r.LastError)
	}
	if r, err := RevalidateEInvoice(tenantID, r5.ID, "acc"); err != nil || r.State != IRPStateReady {
		t.Fatalf("validate after a rejection: %v %+v", err, r)
	}
	irpForgetToken(IRPEnvironment(), seller)
	FakeIRP.FailAuth(1)
	SubmitEInvoice(tenantID, r5.ID, "acc")
	RunEInvoiceWorkerOnce(tenantID) // sign-in fails, nothing sent
	if r := rec("SI-5"); r.State != IRPStateQueued || r.Attempts != 1 {
		t.Fatalf("a failed sign-in: want QUEUED with one attempt, got %s %d", r.State, r.Attempts)
	}
	exec("UPDATE "+schema+".irp_documents SET next_attempt_at = NOW() WHERE id = $1", r5.ID)
	RunEInvoiceWorkerOnce(tenantID)
	if r := rec("SI-5"); r.State != IRPStateAcknowledged {
		t.Fatalf("after signing in again: want ACKNOWLEDGED, got %s %q", r.State, r.LastError)
	}

	// Cancel: tax role, maker-checker, the worker sends it.
	if _, err := RequestIRNCancellation(tenantID, r1.ID, "2", "wrong rate entered", "cashier1", "Cashier"); code(err) != "EINV-0984" {
		t.Fatalf("a cashier asking to cancel: want EINV-0984, got %v", err)
	}
	if _, err := RequestIRNCancellation(tenantID, r1.ID, "2", "wrong rate entered", "acc1", "Accountant"); err != nil {
		t.Fatal(err)
	}
	var cancelDoc string
	db.DB.QueryRow("SELECT id FROM "+schema+".documents WHERE doctype = 'IRNCancellation' AND data->>'irp_document' = $1", fmt.Sprint(r1.ID)).Scan(&cancelDoc)
	if err := DecideApproval(tenantID, IRNCancellationDoctype, cancelDoc, "acc1", "Accountant", "", "Approved", "self"); err == nil {
		t.Fatal("the requester must not approve their own IRN cancellation")
	}
	if err := DecideApproval(tenantID, IRNCancellationDoctype, cancelDoc, "acc2", "Accountant", "", "Approved", "checked"); err != nil {
		t.Fatalf("second accountant approves: %v", err)
	}
	if r := rec("SI-1"); r.State != IRPStateCancelPending {
		t.Fatalf("approved cancel: want CANCEL_PENDING, got %s", r.State)
	}
	RunEInvoiceWorkerOnce(tenantID)
	if r := rec("SI-1"); r.State != IRPStateCancelled || r.CancelledAt == nil {
		t.Fatalf("after the worker: want CANCELLED, got %s", r.State)
	}
	// A cancelled number is never reused.
	invoice("SI-7", "SI/T/2627/0001", "C-B2B", "SKU-T1")
	if _, err := RegisterEInvoiceDocument(tenantID, "SalesInvoice", "SI-7", "acc"); code(err) != "EINV-0981" {
		t.Fatalf("a cancelled number again: want EINV-0981, got %v", err)
	}
	// After 24 hours: Manual review, and a credit note.
	exec("UPDATE "+schema+".irp_documents SET ack_date = NOW() - INTERVAL '25 hours' WHERE id = $1", r4.ID)
	if r, err := RequestIRNCancellation(tenantID, r4.ID, "4", "customer refused goods", "acc1", "Accountant"); err != nil || r.State != IRPStateManualReview {
		t.Fatalf("cancel after 24 h: want MANUAL_REVIEW, got %+v err %v", r, err)
	}

	// No token or credential reaches the attempt log; the history is append-only.
	var leaked int
	db.DB.QueryRow("SELECT COUNT(*) FROM " + schema + ".irp_attempts WHERE error_message ILIKE '%fake-token%'").Scan(&leaked)
	if leaked != 0 {
		t.Fatalf("%d attempt rows carry a token", leaked)
	}
	if _, err := db.DB.Exec("UPDATE " + schema + ".irp_events SET detail = 'x'"); err == nil {
		t.Fatal("irp_events must refuse UPDATE")
	}

	// A credit note: its lines are the original invoice's, in proportion, and
	// it references that invoice.
	doc("CN-1", "CreditNote", "Posted", map[string]interface{}{"note_number": "CN/T/2627/0001", "customer_id": "C-B2B", "sales_invoice_id": "SI-4", "amount": 1050})
	RunEInvoiceWorkerOnce(tenantID)
	cn, err := GetIRPRecordFor(tenantID, "CreditNote", "CN-1")
	if err != nil || cn == nil || cn.State != IRPStateReady || len(cn.Issues) != 0 || cn.DocType != "CRN" {
		t.Fatalf("credit note: want a valid READY CRN, got %+v (%v)", cn, err)
	}
	db.DB.QueryRow("SELECT payload::text FROM "+schema+".irp_documents WHERE id = $1", cn.ID).Scan(&raw)
	var crn IRPDocument
	json.Unmarshal([]byte(raw), &crn)
	if crn.RefDtls == nil || crn.RefDtls.PrecDocDtls[0].InvNo != "SI/T/2627/0004" || crn.ValDtls.TotInvVal != 1050 || crn.ValDtls.AssVal != 1000 || crn.ItemList[0].HsnCd != "610910" {
		t.Fatalf("credit note payload: %s", raw)
	}

	// 75.2.8a: the credit note's print data says IRN PENDING until the IRP
	// acknowledges it, then carries the IRN, Ack and signed QR for the
	// standard layout's QR element.
	if p, err := BuildPrintDocument(tenantID, PrintTxnCreditNote, "CN-1", "", false); err != nil || p.Fields["irn_status"] != "IRN PENDING" || p.Fields["irn"] != nil {
		t.Fatalf("credit note print before its IRN: %v %+v", err, p.Fields)
	}
	if _, err := SubmitEInvoice(tenantID, cn.ID, "acc"); err != nil {
		t.Fatal(err)
	}
	RunEInvoiceWorkerOnce(tenantID)
	cn, _ = GetIRPRecordFor(tenantID, "CreditNote", "CN-1")
	cnPrint, err := BuildPrintDocument(tenantID, PrintTxnCreditNote, "CN-1", "", false)
	if err != nil || cn.State != IRPStateAcknowledged || cn.SignedQR == "" || cnPrint.Fields["irn"] != cn.IRN ||
		cnPrint.Fields["irn_ack_no"] != cn.AckNo || cnPrint.Fields["irn_signed_qr"] != cn.SignedQR || cnPrint.Fields["irn_status"] != nil {
		t.Fatalf("credit note print after its IRN: %v state %s %+v", err, cn.State, cnPrint.Fields)
	}

	// 75.2.10: reconciliation and the register.
	sums, err := ReconcileEInvoices(tenantID)
	if err != nil || len(sums) == 0 {
		t.Fatalf("reconcile: %v %+v", err, sums)
	}
	_, rows, _, err := RunReport(tenantID, "einvoice-irn-register", "Super Admin", "admin", map[string]string{})
	if err != nil || len(rows) < 4 {
		t.Fatalf("IRN register: %v, %d rows", err, len(rows))
	}
}
