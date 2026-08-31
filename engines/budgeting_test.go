package engines

import (
	"custom_erp/db"
	"encoding/json"
	"testing"
	"time"
)

func TestStage374BudgetingCreditDunning(t *testing.T) {
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
			db.DB.Exec("DELETE FROM "+schema+".gl_postings WHERE document_type='SalesInvoice' AND document_id = $1", id)
			db.DB.Exec("DELETE FROM "+schema+".documents WHERE id = $1", id)
		}
	}

	t.Run("ValidateBudgetDocument rejects a bad cost center, account, period and non-positive amount", func(t *testing.T) {
		const ccID = "TEST374A-CC"
		cleanupIDs(ccID)
		defer cleanupIDs(ccID)
		insert(ccID, "CostCenter", "Active", map[string]interface{}{"code": ccID, "name": "Test CC", "status": "Active"})

		if err := ValidateBudgetDocument(tenantID, map[string]interface{}{"cost_center": "TEST374A-NOPE", "account_code": "5100", "period_start": "2026-01-01", "period_end": "2026-12-31", "planned_amount": 1000.0}); err == nil {
			t.Fatalf("expected an unregistered cost center to be rejected")
		}
		if err := ValidateBudgetDocument(tenantID, map[string]interface{}{"cost_center": ccID, "account_code": "NOPE-ACCT", "period_start": "2026-01-01", "period_end": "2026-12-31", "planned_amount": 1000.0}); err == nil {
			t.Fatalf("expected an unregistered account_code to be rejected")
		}
		if err := ValidateBudgetDocument(tenantID, map[string]interface{}{"account_code": "5100", "period_start": "2026-12-31", "period_end": "2026-01-01", "planned_amount": 1000.0}); err == nil {
			t.Fatalf("expected period_end before period_start to be rejected")
		}
		if err := ValidateBudgetDocument(tenantID, map[string]interface{}{"account_code": "5100", "period_start": "2026-01-01", "period_end": "2026-12-31", "planned_amount": 0.0}); err == nil {
			t.Fatalf("expected a non-positive planned_amount to be rejected")
		}
		if err := ValidateBudgetDocument(tenantID, map[string]interface{}{"cost_center": ccID, "account_code": "5100", "period_start": "2026-01-01", "period_end": "2026-12-31", "planned_amount": 1000.0}); err != nil {
			t.Fatalf("expected a valid budget to be accepted, got %v", err)
		}
	})

	t.Run("GetBudgetVarianceReport compares an Approved budget against real gl_postings for the same account/cost_center", func(t *testing.T) {
		const ccID, budgetID, jvID = "TEST374B-CC", "TEST374B-BUDGET", "TEST374B-JV"
		cleanupIDs(ccID, budgetID, jvID)
		defer cleanupIDs(ccID, budgetID, jvID)

		insert(ccID, "CostCenter", "Active", map[string]interface{}{"code": ccID, "name": "Test CC", "status": "Active"})
		insert(budgetID, "Budget", "Approved", map[string]interface{}{
			"code": budgetID, "cost_center": ccID, "account_code": "5100",
			"period_start": "2020-01-01", "period_end": "2099-12-31", "planned_amount": 5000.0,
		})

		today := time.Now().Format("2006-01-02")
		voucherID, err := CreateJournalVoucher(tenantID, today, "TEST374B voucher", []JournalVoucherLine{{AccountCode: "5100", Debit: 900}, {AccountCode: "1100", Credit: 900}}, "manager1", JournalVoucherOptions{CostCenter: ccID})
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

		rows, err := GetBudgetVarianceReport(tenantID, "2020-01-01", "2099-12-31")
		if err != nil {
			t.Fatalf("GetBudgetVarianceReport: %v", err)
		}
		found := false
		for _, r := range rows {
			if r["cost_center"] == ccID && r["account_code"] == "5100" {
				found = true
				if planned, _ := r["planned_amount"].(float64); planned != 5000 {
					t.Fatalf("expected planned_amount=5000, got %v", planned)
				}
				if actual, _ := r["actual_amount"].(float64); actual < 900 {
					t.Fatalf("expected actual_amount to include the 900 posted, got %v", actual)
				}
			}
		}
		if !found {
			t.Fatalf("expected a variance row for (%s, 5100), got %+v", ccID, rows)
		}
	})

	t.Run("CheckCustomerCreditLimit is a no-op when unset, and refuses once outstanding + new amount exceeds it", func(t *testing.T) {
		const custID, inv1 = "TEST374C-CUST", "TEST374C-INV1"
		cleanupIDs(custID, inv1)
		defer cleanupIDs(custID, inv1)

		if err := CheckCustomerCreditLimit(tenantID, "TEST374C-NO-SUCH-CUSTOMER", 999999); err != nil {
			t.Fatalf("expected an unregistered customer to be a no-op, got %v", err)
		}

		insert(custID, "Customer", "Active", map[string]interface{}{"code": custID, "name": custID, "status": "Active"})
		if err := CheckCustomerCreditLimit(tenantID, custID, 999999); err != nil {
			t.Fatalf("expected a blank credit_limit to be unlimited, got %v", err)
		}

		insert(custID, "Customer", "Active", map[string]interface{}{"code": custID, "name": custID, "status": "Active", "credit_limit": 1000.0})
		insert(inv1, "SalesInvoice", "Approved", map[string]interface{}{"code": inv1, "customer": custID, "total_amount": 700.0})

		if err := CheckCustomerCreditLimit(tenantID, custID, 200); err != nil {
			t.Fatalf("expected 700+200=900 (under the 1000 limit) to pass, got %v", err)
		}
		if err := CheckCustomerCreditLimit(tenantID, custID, 400); err == nil {
			t.Fatalf("expected 700+400=1100 (over the 1000 limit) to be refused")
		}
	})

	t.Run("PostSalesInvoice refuses when it would push the customer over their credit limit", func(t *testing.T) {
		const custID, inv1, inv2 = "TEST374D-CUST", "TEST374D-INV1", "TEST374D-INV2"
		cleanupIDs(custID, inv1, inv2)
		defer cleanupIDs(custID, inv1, inv2)

		insert(custID, "Customer", "Active", map[string]interface{}{"code": custID, "name": custID, "status": "Active", "credit_limit": 500.0})
		insert(inv1, "SalesInvoice", "Draft", map[string]interface{}{"code": inv1, "customer": custID, "total_amount": 600})
		if _, err := PostSalesInvoice(tenantID, inv1, "system"); err == nil {
			t.Fatalf("expected posting a Rs 600 invoice against a Rs 500 limit to be refused")
		}

		insert(inv2, "SalesInvoice", "Draft", map[string]interface{}{"code": inv2, "customer": custID, "total_amount": 400})
		amount, err := PostSalesInvoice(tenantID, inv2, "system")
		if err != nil {
			t.Fatalf("expected posting a Rs 400 invoice against a Rs 500 limit to succeed, got %v", err)
		}
		if amount != 400 {
			t.Fatalf("expected posted amount 400, got %d", amount)
		}
	})

	t.Run("dunning: tier classification, notification once per newly-crossed tier, and the dunning queue report", func(t *testing.T) {
		const custID, invOverdue, invFresh, invNoDueDate = "TEST374E-CUST", "TEST374E-INV-OVERDUE", "TEST374E-INV-FRESH", "TEST374E-INV-NODATE"
		cleanupIDs(custID, invOverdue, invFresh, invNoDueDate)
		defer cleanupIDs(custID, invOverdue, invFresh, invNoDueDate)

		if got := dunningTierFor(3, 7, 30); got != "" {
			t.Fatalf("expected 3 days overdue (under the 7-day reminder threshold) to have no tier, got %q", got)
		}
		if got := dunningTierFor(10, 7, 30); got != "Reminder" {
			t.Fatalf("expected 10 days overdue to be Reminder, got %q", got)
		}
		if got := dunningTierFor(35, 7, 30); got != "Escalation" {
			t.Fatalf("expected 35 days overdue to be Escalation, got %q", got)
		}

		insert(custID, "Customer", "Active", map[string]interface{}{"code": custID, "name": custID, "status": "Active"})
		overdueDate := time.Now().AddDate(0, 0, -10).Format("2006-01-02")
		freshDate := time.Now().AddDate(0, 0, 5).Format("2006-01-02")
		insert(invOverdue, "SalesInvoice", "Approved", map[string]interface{}{"code": invOverdue, "customer": custID, "total_amount": 1000.0, "due_date": overdueDate})
		insert(invFresh, "SalesInvoice", "Approved", map[string]interface{}{"code": invFresh, "customer": custID, "total_amount": 500.0, "due_date": freshDate})
		insert(invNoDueDate, "SalesInvoice", "Approved", map[string]interface{}{"code": invNoDueDate, "customer": custID, "total_amount": 300.0})

		notified, err := runDunningForSchema(tenantID, schema)
		if err != nil {
			t.Fatalf("runDunningForSchema: %v", err)
		}
		if notified != 1 {
			t.Fatalf("expected exactly 1 notification (only the overdue invoice), got %d", notified)
		}

		var tier string
		if err := db.DB.QueryRow("SELECT COALESCE(data->>'dunning_last_notified_tier','') FROM "+schema+".documents WHERE id=$1", invOverdue).Scan(&tier); err != nil {
			t.Fatalf("query dunning tier: %v", err)
		}
		if tier != "Reminder" {
			t.Fatalf("expected the overdue invoice to be stamped Reminder, got %q", tier)
		}

		// Re-running immediately must not re-notify the same tier.
		notified2, err := runDunningForSchema(tenantID, schema)
		if err != nil {
			t.Fatalf("runDunningForSchema (second run): %v", err)
		}
		if notified2 != 0 {
			t.Fatalf("expected zero re-notifications on an unchanged tier, got %d", notified2)
		}

		queue, err := GetDunningQueue(tenantID)
		if err != nil {
			t.Fatalf("GetDunningQueue: %v", err)
		}
		foundOverdue, foundFresh := false, false
		for _, r := range queue {
			if r["invoice_id"] == invOverdue {
				foundOverdue = true
				if days, _ := r["days_overdue"].(int); days < 9 || days > 11 {
					t.Fatalf("expected ~10 days overdue, got %v", r["days_overdue"])
				}
			}
			if r["invoice_id"] == invFresh {
				foundFresh = true
			}
		}
		if !foundOverdue {
			t.Fatalf("expected the dunning queue to include the overdue invoice, got %+v", queue)
		}
		if foundFresh {
			t.Fatalf("expected a not-yet-due invoice to be excluded from the dunning queue")
		}
	})

	t.Run("cash flow forecast: opening balance plus projected inflow/outflow on the right day", func(t *testing.T) {
		const custID, vendID, siID, viID = "TEST374F-CUST", "TEST374F-VEND", "TEST374F-SI", "TEST374F-VI"
		cleanupIDs(custID, vendID, siID, viID)
		defer cleanupIDs(custID, vendID, siID, viID)

		asOf := "2027-01-01"
		dueIn5 := "2027-01-06"
		insert(siID, "SalesInvoice", "Approved", map[string]interface{}{"code": siID, "customer": custID, "total_amount": 2000.0, "due_date": dueIn5})
		insert(viID, "VendorInvoice", "Approved", map[string]interface{}{"code": viID, "vendor_id": vendID, "invoice_amount": 800.0, "due_date": dueIn5})

		rows, err := GetCashFlowForecast(tenantID, asOf, 10)
		if err != nil {
			t.Fatalf("GetCashFlowForecast: %v", err)
		}
		if len(rows) != 11 {
			t.Fatalf("expected 11 day-rows (0..10 inclusive), got %d", len(rows))
		}
		var dueDayRow map[string]interface{}
		for _, r := range rows {
			if r["date"] == dueIn5 {
				dueDayRow = r
			}
		}
		if dueDayRow == nil {
			t.Fatalf("expected a row for %s in the forecast", dueIn5)
		}
		if in, _ := dueDayRow["expected_inflow"].(float64); in != 2000 {
			t.Fatalf("expected expected_inflow=2000 on %s, got %v", dueIn5, in)
		}
		if out, _ := dueDayRow["expected_outflow"].(float64); out != 800 {
			t.Fatalf("expected expected_outflow=800 on %s, got %v", dueIn5, out)
		}
	})
}
