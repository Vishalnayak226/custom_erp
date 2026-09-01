package engines

import (
	"custom_erp/db"
	"encoding/json"
	"testing"
	"time"
)

func TestStage377ProjectsJobCosting(t *testing.T) {
	db.InitDB(testConnStr())
	tenantID := "default"
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		t.Fatalf("schema: %v", err)
	}

	insert := func(id, doctype, status string, data map[string]interface{}) {
		raw, _ := json.Marshal(data)
		db.DB.Exec("INSERT INTO "+schema+".documents (id, doctype, data, status, created_by) VALUES ($1, $2, $3, $4, 'system') "+
			"ON CONFLICT (id) DO UPDATE SET doctype = $2, data = $3, status = $4", id, doctype, raw, status)
	}
	cleanupIDs := func(ids ...string) {
		for _, id := range ids {
			db.DB.Exec("DELETE FROM "+schema+".gl_postings WHERE document_id = $1", id)
			db.DB.Exec("DELETE FROM "+schema+".approval_log WHERE document_id = $1", id)
			db.DB.Exec("DELETE FROM "+schema+".documents WHERE id = $1", id)
		}
	}

	t.Run("validateProjectReference rejects unregistered/inactive, accepts Active, and a tagged voucher posts gl_postings.project", func(t *testing.T) {
		const projID = "TEST377A-PROJ"
		cleanupIDs(projID)
		defer cleanupIDs(projID)

		if err := validateProjectReferenceInSchema(schema, "TEST377A-NOPE"); err == nil {
			t.Fatalf("expected an unregistered project to be rejected")
		}
		if err := validateProjectReferenceInSchema(schema, ""); err != nil {
			t.Fatalf("expected an empty project to always be valid, got %v", err)
		}
		insert(projID, "Project", "Inactive", map[string]interface{}{"code": projID, "name": "Test Project", "status": "Inactive"})
		if err := validateProjectReferenceInSchema(schema, projID); err == nil {
			t.Fatalf("expected an Inactive project to be rejected")
		}
		insert(projID, "Project", "Active", map[string]interface{}{"code": projID, "name": "Test Project", "status": "Active"})
		if err := validateProjectReferenceInSchema(schema, projID); err != nil {
			t.Fatalf("expected an Active project to be accepted, got %v", err)
		}

		today := time.Now().Format("2006-01-02")
		voucherID, err := CreateJournalVoucher(tenantID, today, "TEST377A voucher", []JournalVoucherLine{{AccountCode: "5100", Debit: 800}, {AccountCode: "1100", Credit: 800}}, "manager1", JournalVoucherOptions{Project: projID})
		if err != nil {
			t.Fatalf("CreateJournalVoucher: %v", err)
		}
		defer cleanupIDs(voucherID)
		if err := SubmitForApproval(tenantID, "JournalVoucher", voucherID, "manager1", "Store Manager"); err != nil {
			t.Fatalf("SubmitForApproval: %v", err)
		}
		if err := DecideApproval(tenantID, "JournalVoucher", voucherID, "admin", "HR/Admin", "HO", "Approved", "ok"); err != nil {
			t.Fatalf("DecideApproval: %v", err)
		}

		var gotProject string
		if err := db.DB.QueryRow("SELECT project FROM "+schema+".gl_postings WHERE document_type='JournalVoucher' AND document_id=$1 AND account_code='5100'", voucherID).Scan(&gotProject); err != nil {
			t.Fatalf("query gl_postings.project: %v", err)
		}
		if gotProject != projID {
			t.Fatalf("expected gl_postings.project=%s, got %s", projID, gotProject)
		}

		rangeStart := time.Now().AddDate(0, 0, -1).Format("2006-01-02")
		rangeEnd := time.Now().AddDate(0, 0, 1).Format("2006-01-02")
		rows, err := GetProjectPL(tenantID, rangeStart, rangeEnd)
		if err != nil {
			t.Fatalf("GetProjectPL: %v", err)
		}
		found := false
		for _, r := range rows {
			if r["project"] == projID && r["account_type"] == "Expense" {
				found = true
				if amt, _ := r["amount"].(float64); amt != 800 {
					t.Fatalf("expected project %s Expense amount=800, got %v", projID, amt)
				}
			}
		}
		if !found {
			t.Fatalf("expected GetProjectPL to include a row for project %s, got %+v", projID, rows)
		}

		drill, err := dimensionPLDrillDown(tenantID, "project", projID, rangeStart, rangeEnd)
		if err != nil {
			t.Fatalf("dimensionPLDrillDown: %v", err)
		}
		if len(drill) != 1 || drill[0]["account_code"] != "5100" {
			t.Fatalf("expected exactly 1 drill-down row on 5100 for project %s, got %+v", projID, drill)
		}
	})

	t.Run("PayExpenseClaim now actually posts department and project to gl_postings, closing the pre-existing gap", func(t *testing.T) {
		const projID, deptID, claimID = "TEST377B-PROJ", "TEST377B-DEPT", "TEST377B-CLAIM"
		cleanupIDs(projID, deptID, claimID)
		defer cleanupIDs(projID, deptID, claimID)
		insert(projID, "Project", "Active", map[string]interface{}{"code": projID, "name": "Test Project B", "status": "Active"})
		insert(deptID, "Department", "Active", map[string]interface{}{"code": deptID, "name": "Test Dept B", "status": "Active"})
		insert(claimID, "ExpenseClaim", "Verified", map[string]interface{}{
			"code": claimID, "employee_id": "system", "department": deptID, "project": projID,
			"category": "Travel", "amount": 500.0, "gst_amount": 0.0, "purpose": "test",
		})

		if _, err := PayExpenseClaim(tenantID, claimID); err != nil {
			t.Fatalf("PayExpenseClaim: %v", err)
		}

		var gotDepartment, gotProject string
		if err := db.DB.QueryRow("SELECT department, project FROM "+schema+".gl_postings WHERE document_type='ExpenseClaim' AND document_id=$1 AND account_code='5400'", claimID).Scan(&gotDepartment, &gotProject); err != nil {
			t.Fatalf("query gl_postings department/project: %v", err)
		}
		if gotDepartment != deptID {
			t.Fatalf("expected gl_postings.department=%s (the pre-existing gap this stage closes), got %q", deptID, gotDepartment)
		}
		if gotProject != projID {
			t.Fatalf("expected gl_postings.project=%s, got %q", projID, gotProject)
		}
	})
}
