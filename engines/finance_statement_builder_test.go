package engines

import (
	"custom_erp/db"
	"encoding/json"
	"testing"
	"time"
)

func TestStage375FinancialStatementBuilder(t *testing.T) {
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
			db.DB.Exec("DELETE FROM "+schema+".gl_postings WHERE document_type='JournalVoucher' AND document_id = $1", id)
			db.DB.Exec("DELETE FROM "+schema+".documents WHERE id = $1", id)
		}
	}
	postJV := func(narration, cc, department string, amount int) string {
		today := time.Now().Format("2006-01-02")
		voucherID, err := CreateJournalVoucher(tenantID, today, narration, []JournalVoucherLine{{AccountCode: "5100", Debit: amount}, {AccountCode: "1100", Credit: amount}}, "manager1", JournalVoucherOptions{CostCenter: cc, Department: department})
		if err != nil {
			t.Fatalf("CreateJournalVoucher: %v", err)
		}
		if err := SubmitForApproval(tenantID, "JournalVoucher", voucherID, "manager1", "Store Manager"); err != nil {
			t.Fatalf("SubmitForApproval: %v", err)
		}
		if err := DecideApproval(tenantID, "JournalVoucher", voucherID, "admin", "HR/Admin", "HO", "Approved", "ok"); err != nil {
			t.Fatalf("DecideApproval: %v", err)
		}
		return voucherID
	}

	t.Run("a cost-center-filtered P&L only sees that cost center's postings, and drill-down shows the right row", func(t *testing.T) {
		const ccA, ccB = "TEST375A-CC-A", "TEST375A-CC-B"
		cleanupIDs(ccA, ccB)
		defer cleanupIDs(ccA, ccB)
		insert(ccA, "CostCenter", "Active", map[string]interface{}{"code": ccA, "name": "A", "status": "Active"})
		insert(ccB, "CostCenter", "Active", map[string]interface{}{"code": ccB, "name": "B", "status": "Active"})

		vA := postJV("TEST375A voucher A", ccA, "", 500)
		vB := postJV("TEST375A voucher B", ccB, "", 300)
		defer cleanupIDs(vA, vB)

		rangeStart := time.Now().AddDate(0, 0, -1).Format("2006-01-02")
		rangeEnd := time.Now().AddDate(0, 0, 1).Format("2006-01-02")

		rows, err := GetProfitAndLoss(tenantID, rangeStart, rangeEnd, FinancialReportFilter{CostCenter: ccA})
		if err != nil {
			t.Fatalf("GetProfitAndLoss (filtered): %v", err)
		}
		var five100Amount float64
		for _, r := range rows {
			if r["account_code"] == "5100" {
				five100Amount, _ = r["amount"].(float64)
			}
		}
		if five100Amount != 500 {
			t.Fatalf("expected the cost-center-A-filtered P&L to show only 500 on 5100, got %v", five100Amount)
		}

		drill, err := financialStatementDrillDown(tenantID, "5100", rangeStart, rangeEnd, FinancialReportFilter{CostCenter: ccA})
		if err != nil {
			t.Fatalf("financialStatementDrillDown: %v", err)
		}
		if len(drill) != 1 {
			t.Fatalf("expected exactly 1 drill-down row for cost center A's 5100 postings, got %d: %+v", len(drill), drill)
		}
		if debit, _ := drill[0]["debit"].(float64); debit != 500 {
			t.Fatalf("expected the drill-down row's debit to be 500, got %v", debit)
		}
	})

	t.Run("GetDepartmentPL and its drill-down", func(t *testing.T) {
		const deptA = "TEST375B-DEPT-A"
		cleanupIDs(deptA)
		defer cleanupIDs(deptA)
		insert(deptA, "Department", "Active", map[string]interface{}{"code": deptA, "name": "Dept A", "status": "Active"})

		v1 := postJV("TEST375B tagged", "", deptA, 250)
		defer cleanupIDs(v1)

		rangeStart := time.Now().AddDate(0, 0, -1).Format("2006-01-02")
		rangeEnd := time.Now().AddDate(0, 0, 1).Format("2006-01-02")
		rows, err := GetDepartmentPL(tenantID, rangeStart, rangeEnd)
		if err != nil {
			t.Fatalf("GetDepartmentPL: %v", err)
		}
		found := false
		for _, r := range rows {
			if r["department"] == deptA && r["account_type"] == "Expense" {
				found = true
				if amt, _ := r["amount"].(float64); amt != 250 {
					t.Fatalf("expected department %s Expense amount=250, got %v", deptA, amt)
				}
			}
		}
		if !found {
			t.Fatalf("expected GetDepartmentPL to include a row for department %s, got %+v", deptA, rows)
		}

		drill, err := dimensionPLDrillDown(tenantID, "department", deptA, rangeStart, rangeEnd)
		if err != nil {
			t.Fatalf("dimensionPLDrillDown: %v", err)
		}
		if len(drill) != 1 || drill[0]["account_code"] != "5100" {
			t.Fatalf("expected exactly 1 drill-down row on 5100 for department %s, got %+v", deptA, drill)
		}
	})

	t.Run("a Balance-Sheet-filtered by entity narrows correctly and its drill-down works with no start date", func(t *testing.T) {
		const entityA = "TEST375C-ENTITY-A"
		cleanupIDs(entityA)
		defer cleanupIDs(entityA)
		insert(entityA, "LegalEntity", "Active", map[string]interface{}{"code": entityA, "name": "Entity A", "status": "Active"})

		today := time.Now().Format("2006-01-02")
		voucherID, err := CreateJournalVoucher(tenantID, today, "TEST375C entity voucher", []JournalVoucherLine{{AccountCode: "1400", Debit: 900}, {AccountCode: "1100", Credit: 900}}, "manager1", JournalVoucherOptions{Entity: entityA})
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

		asOf := time.Now().AddDate(0, 0, 1).Format("2006-01-02")
		rows, err := GetBalanceSheet(tenantID, asOf, FinancialReportFilter{Entity: entityA})
		if err != nil {
			t.Fatalf("GetBalanceSheet (filtered): %v", err)
		}
		var fixedAssetAmount float64
		for _, r := range rows {
			if r["account_code"] == "1400" {
				fixedAssetAmount, _ = r["amount"].(float64)
			}
		}
		if fixedAssetAmount != 900 {
			t.Fatalf("expected the entity-A-filtered Balance Sheet to show 900 on 1400, got %v", fixedAssetAmount)
		}

		drill, err := financialStatementDrillDown(tenantID, "1400", "", asOf, FinancialReportFilter{Entity: entityA})
		if err != nil {
			t.Fatalf("financialStatementDrillDown (balance sheet, no start date): %v", err)
		}
		if len(drill) != 1 {
			t.Fatalf("expected exactly 1 drill-down row, got %d: %+v", len(drill), drill)
		}
	})
}
