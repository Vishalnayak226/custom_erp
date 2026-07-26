package engines

import (
	"custom_erp/db"
	"encoding/json"
	"strings"
	"testing"
)

func TestPaymentFileAndUTR(t *testing.T) {
	db.InitDB("postgres://postgres@localhost:5435/custom_erp?sslmode=disable")
	tenantID := "default"
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		t.Fatalf("schema: %v", err)
	}

	const vendorID = "TEST-PF-VENDOR"
	const invoiceID = "TEST-PF-INVOICE"
	const proposalID = "TEST-PF-PROPOSAL"

	cleanup := func() {
		db.DB.Exec("DELETE FROM " + schema + ".payment_utr_log WHERE proposal_id = '" + proposalID + "'")
		db.DB.Exec("DELETE FROM " + schema + ".documents WHERE id IN ('" + vendorID + "', '" + invoiceID + "', '" + proposalID + "')")
	}
	cleanup()
	defer cleanup()

	seedDoc := func(id, doctype string, data map[string]interface{}, status string) {
		data["id"] = id
		bytes, err := json.Marshal(data)
		if err != nil {
			t.Fatalf("marshal %s: %v", id, err)
		}
		if _, err := db.DB.Exec(
			"INSERT INTO "+schema+".documents (id, doctype, data, status, created_by) VALUES ($1, $2, $3, $4, 'system')",
			id, doctype, bytes, status); err != nil {
			t.Fatalf("seed %s: %v", id, err)
		}
	}

	t.Run("generates a CSV with resolved vendor bank details, rejects if not Executed", func(t *testing.T) {
		cleanup()
		seedDoc(vendorID, "Vendor", map[string]interface{}{
			"code": vendorID, "name": "Test Payee Ltd", "bank_account_number": "1234567890", "bank_ifsc": "TEST0001234",
		}, "Active")
		seedDoc(invoiceID, "VendorInvoice", map[string]interface{}{
			"vendor_id": vendorID, "invoice_amount": 5000, "status": "Paid",
		}, "Paid")

		resultsJSON, _ := json.Marshal([]PaymentProposalResultLine{{InvoiceID: invoiceID, Paid: true, Amount: 5000}})
		seedDoc(proposalID, "PaymentProposal", map[string]interface{}{
			"invoice_ids": "[]", "total_amount": 5000, "total_paid": 5000,
			"execution_results": json.RawMessage(resultsJSON),
		}, "Draft")

		if _, err := GeneratePaymentFile(tenantID, proposalID); err == nil {
			t.Fatalf("expected a Draft (not Executed) proposal to be rejected")
		}

		if _, err := db.DB.Exec("UPDATE "+schema+".documents SET status='Executed' WHERE id=$1", proposalID); err != nil {
			t.Fatalf("mark Executed: %v", err)
		}

		csvText, err := GeneratePaymentFile(tenantID, proposalID)
		if err != nil {
			t.Fatalf("GeneratePaymentFile: %v", err)
		}
		if !strings.Contains(csvText, "Test Payee Ltd") || !strings.Contains(csvText, "1234567890") || !strings.Contains(csvText, "TEST0001234") || !strings.Contains(csvText, "5000") {
			t.Fatalf("expected the CSV to contain the vendor's bank details and amount, got:\n%s", csvText)
		}
	})

	t.Run("missing vendor bank details is rejected", func(t *testing.T) {
		cleanup()
		seedDoc(vendorID, "Vendor", map[string]interface{}{"code": vendorID, "name": "No Bank Details Ltd"}, "Active")
		seedDoc(invoiceID, "VendorInvoice", map[string]interface{}{"vendor_id": vendorID, "invoice_amount": 100, "status": "Paid"}, "Paid")
		resultsJSON, _ := json.Marshal([]PaymentProposalResultLine{{InvoiceID: invoiceID, Paid: true, Amount: 100}})
		seedDoc(proposalID, "PaymentProposal", map[string]interface{}{
			"execution_results": json.RawMessage(resultsJSON),
		}, "Executed")
		if _, err := GeneratePaymentFile(tenantID, proposalID); err == nil {
			t.Fatalf("expected rejection when the vendor has no bank_account_number/bank_ifsc")
		}
	})

	t.Run("duplicate UTR is rejected, distinct UTRs are not", func(t *testing.T) {
		cleanup()
		seedDoc(proposalID, "PaymentProposal", map[string]interface{}{}, "Executed")
		if err := RecordPaymentUTR(tenantID, proposalID, invoiceID, "TESTUTR000001"); err != nil {
			t.Fatalf("RecordPaymentUTR (first): %v", err)
		}
		if err := RecordPaymentUTR(tenantID, proposalID, invoiceID, "TESTUTR000001"); err == nil {
			t.Fatalf("expected a duplicate UTR to be rejected")
		}
		if err := RecordPaymentUTR(tenantID, proposalID, invoiceID+"-2", "TESTUTR000002"); err != nil {
			t.Fatalf("RecordPaymentUTR (distinct UTR): %v", err)
		}
		utrs, err := ListPaymentUTRs(tenantID, proposalID)
		if err != nil {
			t.Fatalf("ListPaymentUTRs: %v", err)
		}
		if len(utrs) != 2 {
			t.Fatalf("expected 2 recorded UTRs, got %d", len(utrs))
		}
	})
}
