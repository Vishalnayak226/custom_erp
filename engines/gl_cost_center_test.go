package engines

import (
	"custom_erp/db"
	"encoding/json"
	"testing"
	"time"
)

func TestCostCenterPostings(t *testing.T) {
	db.InitDB("postgres://postgres@localhost:5435/custom_erp?sslmode=disable")
	tenantID := "default"
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		t.Fatalf("schema: %v", err)
	}

	const ccID = "TEST-CC-MARKETING"

	cleanup := func() {
		db.DB.Exec("DELETE FROM " + schema + ".gl_postings WHERE document_type = 'JournalVoucher' AND document_id IN (SELECT id FROM " + schema + ".documents WHERE doctype = 'JournalVoucher' AND data->>'narration' LIKE 'TEST-CC%')")
		db.DB.Exec("DELETE FROM " + schema + ".approval_log WHERE document_id IN (SELECT id FROM " + schema + ".documents WHERE doctype = 'JournalVoucher' AND data->>'narration' LIKE 'TEST-CC%')")
		db.DB.Exec("DELETE FROM " + schema + ".documents WHERE doctype = 'JournalVoucher' AND data->>'narration' LIKE 'TEST-CC%'")
		db.DB.Exec("DELETE FROM " + schema + ".documents WHERE doctype = 'CostCenter' AND id = '" + ccID + "'")
	}
	cleanup()
	defer cleanup()

	t.Run("rejects an unregistered or inactive cost center, accepts an Active one", func(t *testing.T) {
		cleanup()
		if err := validateCostCenterReferenceInSchema(schema, "TEST-CC-DOES-NOT-EXIST"); err == nil {
			t.Fatalf("expected an unregistered cost center to be rejected")
		}
		if err := validateCostCenterReferenceInSchema(schema, ""); err != nil {
			t.Fatalf("expected an empty cost center to always be valid, got %v", err)
		}

		data, _ := json.Marshal(map[string]interface{}{"id": ccID, "code": ccID, "name": "Marketing", "status": "Inactive"})
		if _, err := db.DB.Exec("INSERT INTO "+schema+".documents (id, doctype, data, status, created_by) VALUES ($1, 'CostCenter', $2, 'Inactive', 'system')", ccID, data); err != nil {
			t.Fatalf("seed inactive cost center: %v", err)
		}
		if err := validateCostCenterReferenceInSchema(schema, ccID); err == nil {
			t.Fatalf("expected an Inactive cost center to be rejected")
		}

		if _, err := db.DB.Exec("UPDATE "+schema+".documents SET status='Active', data=jsonb_set(data,'{status}','\"Active\"') WHERE id=$1", ccID); err != nil {
			t.Fatalf("activate cost center: %v", err)
		}
		if err := validateCostCenterReferenceInSchema(schema, ccID); err != nil {
			t.Fatalf("expected an Active cost center to be accepted, got %v", err)
		}
	})

	t.Run("a journal voucher tagged with a cost center posts gl_postings rows carrying it, and the cost-center P&L report reads it back", func(t *testing.T) {
		cleanup()
		data, _ := json.Marshal(map[string]interface{}{"id": ccID, "code": ccID, "name": "Marketing", "status": "Active"})
		if _, err := db.DB.Exec("INSERT INTO "+schema+".documents (id, doctype, data, status, created_by) VALUES ($1, 'CostCenter', $2, 'Active', 'system')", ccID, data); err != nil {
			t.Fatalf("seed cost center: %v", err)
		}

		today := time.Now().Format("2006-01-02")
		voucherID, err := CreateJournalVoucher(tenantID, today, "TEST-CC voucher", []JournalVoucherLine{{AccountCode: "5100", Debit: 900}, {AccountCode: "1100", Credit: 900}}, "manager1", JournalVoucherOptions{CostCenter: ccID})
		if err != nil {
			t.Fatalf("CreateJournalVoucher: %v", err)
		}
		if err := SubmitForApproval(tenantID, "JournalVoucher", voucherID, "manager1", "Store Manager"); err != nil {
			t.Fatalf("SubmitForApproval: %v", err)
		}
		if err := DecideApproval(tenantID, "JournalVoucher", voucherID, "admin", "HR/Admin", "HO", "Approved", "ok"); err != nil {
			t.Fatalf("DecideApproval: %v", err)
		}

		var status string
		if err := db.DB.QueryRow("SELECT status FROM "+schema+".documents WHERE id=$1", voucherID).Scan(&status); err != nil {
			t.Fatalf("query status: %v", err)
		}
		if status != "Posted" {
			t.Fatalf("expected Posted, got %s", status)
		}

		var storedCC string
		if err := db.DB.QueryRow("SELECT cost_center FROM "+schema+".gl_postings WHERE document_type='JournalVoucher' AND document_id=$1 AND account_code='5100'", voucherID).Scan(&storedCC); err != nil {
			t.Fatalf("query gl_postings cost_center: %v", err)
		}
		if storedCC != ccID {
			t.Fatalf("expected gl_postings.cost_center=%s, got %s", ccID, storedCC)
		}

		// A wide window (yesterday to tomorrow), not exactly today - this
		// dev Postgres session's naive timestamp columns can read back
		// several hours off from Go's time.Now() (see project_ledger.md
		// §52's precedent), which could otherwise push the posting's real
		// created_at across a day boundary from what Go's clock computed.
		rangeStart := time.Now().AddDate(0, 0, -1).Format("2006-01-02")
		rangeEnd := time.Now().AddDate(0, 0, 1).Format("2006-01-02")
		rows, err := GetCostCenterPL(tenantID, rangeStart, rangeEnd)
		if err != nil {
			t.Fatalf("GetCostCenterPL: %v", err)
		}
		found := false
		for _, r := range rows {
			if r["cost_center"] == ccID && r["account_type"] == "Expense" {
				found = true
				if amt, _ := r["amount"].(float64); amt != 900 {
					t.Fatalf("expected cost center %s Expense amount=900, got %v", ccID, r["amount"])
				}
			}
		}
		if !found {
			t.Fatalf("expected GetCostCenterPL to include a row for cost center %s, got %+v", ccID, rows)
		}
	})

	t.Run("rejects creating a voucher with an unregistered cost center", func(t *testing.T) {
		cleanup()
		if _, err := CreateJournalVoucher(tenantID, "2026-07-25", "TEST-CC bad", []JournalVoucherLine{{AccountCode: "5100", Debit: 10}, {AccountCode: "1100", Credit: 10}}, "manager1", JournalVoucherOptions{CostCenter: "TEST-CC-NOPE"}); err == nil {
			t.Fatalf("expected an unregistered cost center to reject voucher creation")
		}
	})
}
