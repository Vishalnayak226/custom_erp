package engines

import (
	"custom_erp/db"
	"encoding/json"
	"testing"
	"time"
)

// Stage 55.18: the service desk lists open tickets with due state, contract
// coverage and next step; a contract lists the tickets it covers; a project
// traces its revenue and expense to the documents that posted them.
func TestServiceDeskAndProjectLedger(t *testing.T) {
	db.InitDB(testConnStr())
	tenantID, schema := uniqueLifecycleTenant(t)
	defer dropLifecycleTenant(tenantID, schema)
	if _, err := ProvisionTenantSchema(tenantID, schema, "0.1.0-test"); err != nil {
		t.Fatalf("ProvisionTenantSchema: %v", err)
	}
	doc := func(id, doctype, status string, data map[string]interface{}) {
		t.Helper()
		raw, _ := json.Marshal(data)
		if _, err := db.DB.Exec("INSERT INTO "+schema+".documents (id, doctype, data, status, created_by) VALUES ($1, $2, $3, $4, 'system')", id, doctype, raw, status); err != nil {
			t.Fatalf("seed %s: %v", id, err)
		}
	}
	today := time.Now()
	doc("SC-1", "ServiceContract", "Active", map[string]interface{}{
		"code": "SC-1", "customer": "Acme", "start_date": today.AddDate(0, -1, 0).Format("2006-01-02"),
		"end_date": today.AddDate(0, 1, 0).Format("2006-01-02"), "visits_included": 2, "visits_used": 1,
	})
	late, err := CreateServiceTicket(tenantID, "Acme", "Pump leaking", "High", "", "SC-1", "", today.AddDate(0, 0, -2).Format("2006-01-02"), "desk1")
	if err != nil {
		t.Fatal(err)
	}
	if err := AssignServiceTicket(tenantID, late, "tech1"); err != nil {
		t.Fatal(err)
	}
	other, err := CreateServiceTicket(tenantID, "Beta", "Noise", "Low", "", "", "", "", "desk1")
	if err != nil {
		t.Fatal(err)
	}
	if err := AssignServiceTicket(tenantID, other, "tech2"); err != nil {
		t.Fatal(err)
	}

	all, err := ServiceDesk(tenantID, "tech1", false)
	if err != nil || len(all) != 2 {
		t.Fatalf("all open tickets: %v %v", all, err)
	}
	first := all[0]
	if first["id"] != late || first["overdue"] != true || first["next_step"] != "Start work" {
		t.Fatalf("the overdue ticket leads with its next step, got %v", first)
	}
	contract, _ := first["contract"].(map[string]interface{})
	if contract["covered"] != true || contract["visits_remaining"].(float64) != 1 {
		t.Fatalf("contract coverage: %v", contract)
	}
	mine, _ := ServiceDesk(tenantID, "tech1", true)
	if len(mine) != 1 || mine[0]["id"] != late {
		t.Fatalf("mine shows the viewer's tickets (and unassigned), got %v", mine)
	}

	view, err := ServiceContractView(tenantID, "SC-1")
	if err != nil {
		t.Fatal(err)
	}
	if tickets, _ := view["tickets"].([]map[string]interface{}); len(tickets) != 1 || tickets[0]["id"] != late {
		t.Fatalf("contract lists its ticket: %v", view["tickets"])
	}

	doc("PRJ-1", "Project", "Inactive", map[string]interface{}{"code": "PRJ-1", "name": "Fit-out", "status": "Inactive"})
	var expenseAccount string
	if err := db.DB.QueryRow("SELECT account_code FROM " + schema + ".gl_accounts WHERE account_type = 'Expense' ORDER BY account_code LIMIT 1").Scan(&expenseAccount); err != nil {
		t.Fatalf("expense account: %v", err)
	}
	for _, p := range []struct {
		account, docID string
		debit, credit  int
	}{{"4100", "INV-9", 0, 10000}, {expenseAccount, "JV-9", 4000, 0}} {
		if _, err := db.DB.Exec("INSERT INTO "+schema+".gl_postings (account_code, debit, credit, document_type, document_id, project) VALUES ($1, $2, $3, 'Test', $4, 'PRJ-1')", p.account, p.debit, p.credit, p.docID); err != nil {
			t.Fatalf("seed posting: %v", err)
		}
	}
	ledger, err := ProjectLedger(tenantID, "PRJ-1")
	if err != nil {
		t.Fatalf("an inactive project's ledger is still readable: %v", err)
	}
	if ledger["revenue"].(float64) != 100 || ledger["expense"].(float64) != 40 || ledger["net"].(float64) != 60 {
		t.Fatalf("project totals: %v", ledger)
	}
	if docs, _ := ledger["documents"].([]map[string]interface{}); len(docs) != 2 {
		t.Fatalf("each figure traces to its document: %v", ledger["documents"])
	}
	if _, err := ProjectLedger(tenantID, "NOPE"); err == nil {
		t.Fatal("an unknown project is a not-found, not an empty ledger")
	}
}
