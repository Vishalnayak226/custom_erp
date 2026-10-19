package engines

import (
	"custom_erp/db"
	"encoding/json"
	"math"
	"testing"
)

// Stage 75.3.7 Wave C on known data: a production order completed short of
// plan, an expense claim with no receipt, a payslip, a repeat customer, a
// pick that came up short and a count that missed one.
func TestCatalogWaveCFigures(t *testing.T) {
	db.InitDB(testConnStr())
	tenantID, schema := uniqueLifecycleTenant(t)
	defer dropLifecycleTenant(tenantID, schema)
	if _, err := ProvisionTenantSchema(tenantID, schema, "0.1.0-test"); err != nil {
		t.Fatalf("ProvisionTenantSchema: %v", err)
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
		exec("INSERT INTO "+schema+".documents (id, doctype, data, status, created_by) VALUES ($1,$2,$3,$4,'admin')", id, doctype, b, status)
	}
	doc("S1", "Location", "Active", map[string]interface{}{"code": "S1", "name": "Jayanagar"})
	doc("P1", "Item", "Active", map[string]interface{}{"code": "P1", "name": "Silver Necklace", "mrp": 1180, "gst_rate": 18})
	doc("C1", "Item", "Active", map[string]interface{}{"code": "C1", "name": "Silver 925", "standard_cost": 100})
	doc("WC1", "WorkCenter", "Active", map[string]interface{}{"code": "WC1", "name": "Casting"})
	doc("R1", "Routing", "Active", map[string]interface{}{"code": "R1", "operations": []map[string]interface{}{{"seq": 1, "work_center_id": "WC1"}}})
	doc("B1", "BOM", "Active", map[string]interface{}{"code": "B1", "parent_item": "P1", "standard_cost": 300,
		"components": []map[string]interface{}{{"sku": "C1", "qty": 2, "scrap_percent": 10}}})
	doc("PO1", "ProductionOrder", "Completed", map[string]interface{}{"code": "PRO/S1/0001", "bom_id": "B1", "routing_id": "R1", "location": "S1",
		"quantity": 10, "completed_qty": 8, "actual_cost": 2600})
	doc("D1", "Department", "Active", map[string]interface{}{"code": "D1", "name": "Marketing"})
	doc("E1", "Employee", "Active", map[string]interface{}{"code": "E1", "name": "Ravi Kumar", "department": "D1", "location": "S1"})
	doc("EC1", "ExpenseClaim", "Submitted", map[string]interface{}{"code": "EXP/S1/0001", "employee_id": "E1", "location": "S1",
		"expense_date": "2026-10-10", "category": "Travel", "amount": 1500})
	doc("PS1", "Payslip", "Paid", map[string]interface{}{"code": "PS1", "employee_id": "E1", "period_from": "2026-09-01", "period_to": "2026-09-30",
		"gross_pay": 30000, "pf_deduction": 1800, "pt_deduction": 200, "net_pay": 28000})
	doc("CU1", "Customer", "Active", map[string]interface{}{"code": "CU1", "name": "Asha Rao", "phone": "9876543210"})
	doc("BL1", "POSCart", "Paid", map[string]interface{}{"location": "S1", "bill_number": "S1/1", "customer_id": "CU1", "amount_paid": 4000})
	doc("BL2", "POSCart", "Paid", map[string]interface{}{"location": "S1", "bill_number": "S1/2", "customer_id": "CU1", "amount_paid": 1000})
	doc("WT1", "WarehouseTask", "Completed", map[string]interface{}{"code": "PICK-1", "task_type": "Pick", "item": "P1", "qty": 5, "location_code": "S1", "from_bin": "A-01"})
	exec("INSERT INTO " + schema + ".bin_allocation (task_id, sku, location_code, bin_code, qty, picked_qty, status) VALUES ('WT1', 'P1', 'S1', 'A-01', 5, 4, 'Picked')")
	doc("CC1", "CycleCountLine", "Posted", map[string]interface{}{"count_session": "CS-1", "location": "S1", "bin": "A-01", "sku": "P1", "system_qty": 10, "counted_qty": 9})

	run := func(id string) []map[string]interface{} {
		t.Helper()
		rows, err := reportRegistry[id].Run(tenantID, map[string]string{})
		if err != nil {
			t.Fatalf("%s: %v", id, err)
		}
		return rows
	}
	near := func(what string, got interface{}, want float64) {
		t.Helper()
		if math.Abs(catNum(got)-want) > 0.011 {
			t.Errorf("%s = %v, want %.2f", what, got, want)
		}
	}
	is := func(what string, got interface{}, want string) {
		t.Helper()
		if catStr(got) != want {
			t.Errorf("%s = %q, want %q", what, catStr(got), want)
		}
	}
	one := func(id string) map[string]interface{} {
		t.Helper()
		rows := run(id)
		if len(rows) != 1 {
			t.Fatalf("%s: %d rows, want 1: %v", id, len(rows), rows)
		}
		return rows[0]
	}

	pr := one("cat-production-order-register")
	is("parent", pr["parent_item"], "Silver Necklace")
	is("work center", pr["work_center"], "Casting")
	near("WIP of a completed order", pr["wip_qty"], 0)
	near("yield", one("cat-yield-scrap-analysis")["yield"], 80)
	use := one("cat-material-usage-variance")
	near("planned component qty", use["component_qty"], 22) // 2 x 10 + 10% scrap
	near("issued at standard", use["qty"], 17.6)            // 2 x 8 + 10%
	near("scrap allowance", use["variance_qty"], 1.6)
	cost := one("cat-standard-vs-actual-cost")
	near("standard", cost["standard_cost"], 2400)
	near("variance", cost["cost_variance"], 200)

	pend := one("cat-claims-pending-approval")
	is("employee", pend["employee_name"], "Ravi Kumar")
	is("department", pend["department"], "Marketing")
	if v := one("cat-policy-violation-exception-report"); catStr(v["policy_exception"]) == "" {
		t.Errorf("a claim with no receipt should be a policy exception: %v", v)
	}

	pay := one("cat-payroll-register")
	near("deductions", pay["deductions"], 2000)
	near("net pay", pay["net_pay"], 28000)
	if comps := run("cat-salary-component-detail"); len(comps) != 4 { // gross, PF, PT, net
		t.Errorf("salary components: %d, want 4: %v", len(comps), comps)
	}
	stat := run("cat-statutory-deduction-summary")
	if len(stat) != 2 {
		t.Fatalf("statutory summary: %d rows (PF, PT): %v", len(stat), stat)
	}

	c360 := one("cat-customer-360-summary")
	near("bills", c360["bill_count"], 2)
	near("net sales", c360["net_sales_amount"], 5000)
	if len(run("cat-repeat-purchase-report")) != 1 {
		t.Error("a customer with two bills is a repeat purchaser")
	}

	ex := one("cat-picking-exception-report")
	near("picked", ex["picked_qty"], 4)
	near("short", ex["variance_qty"], -1)

	acc := one("cat-warehouse-inventory-accuracy-summary")
	near("accuracy", acc["achievement"], 90)
}
