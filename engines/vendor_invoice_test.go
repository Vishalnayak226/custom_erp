package engines

import (
	"custom_erp/db"
	"encoding/json"
	"testing"
)

func TestVendorInvoice3WayMatchAndPayment(t *testing.T) {
	db.InitDB("postgres://postgres@localhost:5435/custom_erp?sslmode=disable")
	tenantID := "default"
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		t.Fatalf("schema: %v", err)
	}

	const sku = "TEST-VI-SKU"
	const poID = "TEST-VI-PO"
	const grnID = "TEST-VI-GRN"

	cleanup := func() {
		db.DB.Exec("DELETE FROM " + schema + ".documents WHERE id LIKE 'TEST-VI-%'")
		db.DB.Exec("DELETE FROM " + schema + ".gl_postings WHERE document_type = 'VendorInvoice' AND document_id LIKE 'TEST-VI-%'")
	}
	cleanup()
	defer cleanup()

	seedPOAndGRN := func(poAmount float64, grnQty int) {
		poItems, _ := json.Marshal([]map[string]interface{}{{"sku": sku, "qty": 10, "rate": poAmount / 10}})
		poData := map[string]interface{}{"id": poID, "code": poID, "total_amount": poAmount, "items": string(poItems), "status": "Approved"}
		poBytes, _ := json.Marshal(poData)
		if _, err := db.DB.Exec("INSERT INTO "+schema+".documents (id, doctype, data, status, created_by) VALUES ($1, 'PurchaseOrder', $2, 'Approved', 'system')", poID, poBytes); err != nil {
			t.Fatalf("seed PO: %v", err)
		}
		grnItems, _ := json.Marshal([]map[string]interface{}{{"sku": sku, "qty": grnQty}})
		grnData := map[string]interface{}{"id": grnID, "code": grnID, "po_id": poID, "received_items": string(grnItems), "status": "Received"}
		grnBytes, _ := json.Marshal(grnData)
		if _, err := db.DB.Exec("INSERT INTO "+schema+".documents (id, doctype, data, status, created_by) VALUES ($1, 'GRN', $2, 'Received', 'system')", grnID, grnBytes); err != nil {
			t.Fatalf("seed GRN: %v", err)
		}
	}

	seedInvoice := func(id string, amount float64, vendor string, invoiceNumber string, fy string) {
		data := map[string]interface{}{
			"id": id, "invoice_number": invoiceNumber, "vendor_id": vendor, "po_id": poID, "grn_id": grnID,
			"invoice_amount": amount, "financial_year": fy, "status": "Draft",
		}
		bytes, _ := json.Marshal(data)
		if _, err := db.DB.Exec("INSERT INTO "+schema+".documents (id, doctype, data, status, created_by) VALUES ($1, 'VendorInvoice', $2, 'Draft', 'system')", id, bytes); err != nil {
			t.Fatalf("seed invoice %s: %v", id, err)
		}
	}

	t.Run("a matching trio (PO=GRN=Invoice) matches within tolerance", func(t *testing.T) {
		cleanup()
		seedPOAndGRN(1000, 10)
		seedInvoice("TEST-VI-INV1", 1000, "VEND01", "INV-001", "TEST-FY")

		matched, err := Match3Way(tenantID, poID, grnID, "TEST-VI-INV1", 2.0)
		if err != nil {
			t.Fatalf("Match3Way: %v", err)
		}
		if !matched {
			t.Fatalf("expected a match for an exact trio")
		}
		var status string
		db.DB.QueryRow("SELECT status FROM "+schema+".documents WHERE id='TEST-VI-INV1'").Scan(&status)
		if status != "Matched" {
			t.Fatalf("expected status Matched, got %s", status)
		}
	})

	t.Run("a mismatched invoice amount holds for review", func(t *testing.T) {
		cleanup()
		seedPOAndGRN(1000, 10)
		seedInvoice("TEST-VI-INV2", 1500, "VEND01", "INV-002", "TEST-FY")

		matched, err := Match3Way(tenantID, poID, grnID, "TEST-VI-INV2", 2.0)
		if err != nil {
			t.Fatalf("Match3Way: %v", err)
		}
		if matched {
			t.Fatalf("expected a mismatch for a 50%% over-billed invoice")
		}
		var status string
		db.DB.QueryRow("SELECT status FROM "+schema+".documents WHERE id='TEST-VI-INV2'").Scan(&status)
		if status != "MismatchHold" {
			t.Fatalf("expected status MismatchHold, got %s", status)
		}
	})

	t.Run("a short-received GRN also causes a mismatch even if invoice equals PO", func(t *testing.T) {
		cleanup()
		seedPOAndGRN(1000, 5) // only half received
		seedInvoice("TEST-VI-INV3", 1000, "VEND01", "INV-003", "TEST-FY")

		matched, err := Match3Way(tenantID, poID, grnID, "TEST-VI-INV3", 2.0)
		if err != nil {
			t.Fatalf("Match3Way: %v", err)
		}
		if matched {
			t.Fatalf("expected a mismatch when GRN received value is half the invoice amount")
		}
	})

	t.Run("Matched invoice can be paid and posts a balanced GL entry", func(t *testing.T) {
		cleanup()
		seedPOAndGRN(1000, 10)
		seedInvoice("TEST-VI-INV4", 1000, "VEND01", "INV-004", "TEST-FY")
		if _, err := Match3Way(tenantID, poID, grnID, "TEST-VI-INV4", 2.0); err != nil {
			t.Fatalf("Match3Way: %v", err)
		}

		paid, pending, err := PayVendorInvoice(tenantID, "TEST-VI-INV4", "system", "HR/Admin", "")
		if err != nil {
			t.Fatalf("PayVendorInvoice: %v", err)
		}
		if pending {
			t.Fatalf("a Matched invoice should pay immediately, not route to approval")
		}
		if paid != 1000 {
			t.Fatalf("expected paid=1000, got %d", paid)
		}
		var cash, suspense int
		db.DB.QueryRow("SELECT COALESCE(SUM(debit),0) FROM "+schema+".gl_postings WHERE document_id='TEST-VI-INV4' AND account_code='2100'").Scan(&suspense)
		db.DB.QueryRow("SELECT COALESCE(SUM(credit),0) FROM "+schema+".gl_postings WHERE document_id='TEST-VI-INV4' AND account_code='1100'").Scan(&cash)
		if suspense != 1000 || cash != 1000 {
			t.Fatalf("expected balanced GL debit/credit of 1000, got suspense_debit=%d cash_credit=%d", suspense, cash)
		}

		if _, _, err := PayVendorInvoice(tenantID, "TEST-VI-INV4", "system", "HR/Admin", ""); err == nil {
			t.Fatalf("expected a second payment attempt to be rejected")
		}
	})

	// 24.11: overriding a MismatchHold invoice no longer pays inline - it
	// routes through the same maker-checker approval engine PurchaseOrder
	// (Stage 13.8) already uses, and only actually pays once a *different*
	// user approves it via DecideApproval/FinalizeVendorInvoiceOverridePayment.
	t.Run("MismatchHold cannot be paid without an override reason; with one it routes to approval and pays once approved", func(t *testing.T) {
		cleanup()
		seedPOAndGRN(1000, 10)
		seedInvoice("TEST-VI-INV5", 1500, "VEND01", "INV-005", "TEST-FY")
		if _, err := Match3Way(tenantID, poID, grnID, "TEST-VI-INV5", 2.0); err != nil {
			t.Fatalf("Match3Way: %v", err)
		}

		if _, _, err := PayVendorInvoice(tenantID, "TEST-VI-INV5", "system", "Store Manager", ""); err == nil {
			t.Fatalf("expected payment without override to be rejected for a MismatchHold invoice")
		}

		// Real seeded user IDs (db/migration.sql) - documents.created_by has
		// a foreign key to tenant_default.users, and PayVendorInvoice's
		// override branch reassigns created_by to the requesting user (see
		// its own comment for why), so this must be a real user id, not an
		// arbitrary string.
		const makerID = "manager1"   // Store Manager
		const checkerID = "admin"    // HR/Admin
		paid, pending, err := PayVendorInvoice(tenantID, "TEST-VI-INV5", makerID, "Store Manager", "Vendor confirmed price increase via email")
		if err != nil {
			t.Fatalf("expected an override submission to succeed: %v", err)
		}
		if !pending || paid != 0 {
			t.Fatalf("expected the override to be claimed for approval (paid=0, pending=true), got paid=%d pending=%v", paid, pending)
		}
		var status string
		db.DB.QueryRow("SELECT status FROM " + schema + ".documents WHERE id='TEST-VI-INV5'").Scan(&status)
		if status != "Pending Approval" {
			t.Fatalf("expected status Pending Approval after override submission, got %s", status)
		}

		// Same maker cannot approve their own override (maker-checker), a
		// different HR/Admin checker can, and only then does the payment
		// actually post.
		if err := DecideApproval(tenantID, "VendorInvoice", "TEST-VI-INV5", makerID, "HR/Admin", "HO", "Approved", ""); err == nil {
			t.Fatalf("expected the maker to be blocked from approving their own override")
		}
		if err := DecideApproval(tenantID, "VendorInvoice", "TEST-VI-INV5", checkerID, "HR/Admin", "HO", "Approved", "looks legitimate"); err != nil {
			t.Fatalf("expected a different HR/Admin checker to approve: %v", err)
		}
		finalPaid, err := FinalizeVendorInvoiceOverridePayment(tenantID, "TEST-VI-INV5", checkerID)
		if err != nil {
			t.Fatalf("FinalizeVendorInvoiceOverridePayment: %v", err)
		}
		if finalPaid != 1500 {
			t.Fatalf("expected finalized payment of 1500, got %d", finalPaid)
		}
		db.DB.QueryRow("SELECT status FROM " + schema + ".documents WHERE id='TEST-VI-INV5'").Scan(&status)
		if status != "Paid" {
			t.Fatalf("expected status Paid after finalization, got %s", status)
		}
	})

	t.Run("duplicate vendor+invoice_number+financial_year is rejected at the database level", func(t *testing.T) {
		cleanup()
		seedPOAndGRN(1000, 10)
		seedInvoice("TEST-VI-INV6A", 1000, "VEND01", "INV-DUP", "TEST-FY")
		data := map[string]interface{}{
			"id": "TEST-VI-INV6B", "invoice_number": "INV-DUP", "vendor_id": "VEND01", "po_id": poID, "grn_id": grnID,
			"invoice_amount": 999, "financial_year": "TEST-FY", "status": "Draft",
		}
		bytes, _ := json.Marshal(data)
		_, err := db.DB.Exec("INSERT INTO "+schema+".documents (id, doctype, data, status, created_by) VALUES ($1, 'VendorInvoice', $2, 'Draft', 'system')", "TEST-VI-INV6B", bytes)
		if err == nil {
			t.Fatalf("expected duplicate vendor+invoice_number+financial_year to be rejected")
		}
	})
}
