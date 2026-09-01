package engines

import (
	"custom_erp/db"
	"encoding/json"
	"testing"
	"time"
)

func TestStage378ServiceManagement(t *testing.T) {
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
			db.DB.Exec("DELETE FROM "+schema+".documents WHERE id = $1", id)
		}
	}

	t.Run("full ServiceTicket lifecycle: create -> assign -> start -> resolve -> close, each guard enforced", func(t *testing.T) {
		ticketID, err := CreateServiceTicket(tenantID, "TEST378A-CUST", "AC not cooling", "High", "", "", "", "", "system")
		if err != nil {
			t.Fatalf("CreateServiceTicket: %v", err)
		}
		defer cleanupIDs(ticketID)

		if err := StartServiceTicket(tenantID, ticketID); err == nil {
			t.Fatalf("expected starting a Draft ticket (not yet Assigned) to be refused")
		}
		if err := ResolveServiceTicket(tenantID, ticketID, "fixed"); err == nil {
			t.Fatalf("expected resolving a Draft ticket to be refused")
		}

		if err := AssignServiceTicket(tenantID, ticketID, "tech1"); err != nil {
			t.Fatalf("AssignServiceTicket: %v", err)
		}
		_, status, err := fetchServiceTicket(schema, ticketID)
		if err != nil {
			t.Fatalf("fetchServiceTicket: %v", err)
		}
		if status != "Assigned" {
			t.Fatalf("expected status=Assigned, got %s", status)
		}

		if err := ResolveServiceTicket(tenantID, ticketID, "fixed"); err == nil {
			t.Fatalf("expected resolving an Assigned (not yet InProgress) ticket to be refused")
		}
		if err := StartServiceTicket(tenantID, ticketID); err != nil {
			t.Fatalf("StartServiceTicket: %v", err)
		}
		if err := ResolveServiceTicket(tenantID, ticketID, ""); err == nil {
			t.Fatalf("expected resolving with no resolution_notes to be refused")
		}
		if err := ResolveServiceTicket(tenantID, ticketID, "replaced capacitor"); err != nil {
			t.Fatalf("ResolveServiceTicket: %v", err)
		}
		if err := CloseServiceTicket(tenantID, ticketID); err != nil {
			t.Fatalf("CloseServiceTicket: %v", err)
		}
		data, status, err := fetchServiceTicket(schema, ticketID)
		if err != nil {
			t.Fatalf("fetchServiceTicket: %v", err)
		}
		if status != "Closed" {
			t.Fatalf("expected status=Closed, got %s", status)
		}
		if data["resolution_notes"] != "replaced capacitor" {
			t.Fatalf("expected resolution_notes to be persisted, got %v", data["resolution_notes"])
		}

		if err := CancelServiceTicket(tenantID, ticketID, "too late"); err == nil {
			t.Fatalf("expected cancelling an already-Closed (terminal) ticket to be refused")
		}
	})

	t.Run("CancelServiceTicket requires a reason and refuses on a terminal ticket", func(t *testing.T) {
		ticketID, err := CreateServiceTicket(tenantID, "TEST378B-CUST", "Leak", "Low", "", "", "", "", "system")
		if err != nil {
			t.Fatalf("CreateServiceTicket: %v", err)
		}
		defer cleanupIDs(ticketID)

		if err := CancelServiceTicket(tenantID, ticketID, ""); err == nil {
			t.Fatalf("expected a blank cancellation_reason to be refused")
		}
		if err := CancelServiceTicket(tenantID, ticketID, "customer withdrew request"); err != nil {
			t.Fatalf("CancelServiceTicket: %v", err)
		}
		_, status, err := fetchServiceTicket(schema, ticketID)
		if err != nil {
			t.Fatalf("fetchServiceTicket: %v", err)
		}
		if status != "Cancelled" {
			t.Fatalf("expected status=Cancelled, got %s", status)
		}
	})

	t.Run("ServiceContract: validation, visit consumption, exhaustion, and closing a ticket consumes a visit", func(t *testing.T) {
		const contractID = "TEST378C-CONTRACT"
		cleanupIDs(contractID)
		defer cleanupIDs(contractID)

		if err := ValidateServiceContractDocument(tenantID, map[string]interface{}{"start_date": "2026-12-01", "end_date": "2026-01-01"}); err == nil {
			t.Fatalf("expected end_date before start_date to be rejected")
		}
		if err := ValidateServiceContractDocument(tenantID, map[string]interface{}{"visits_included": 0.0}); err == nil {
			t.Fatalf("expected a non-positive visits_included to be rejected")
		}
		if err := ValidateServiceContractDocument(tenantID, map[string]interface{}{"recurring_sales_contract_id": "TEST378C-NO-SUCH-CONTRACT"}); err == nil {
			t.Fatalf("expected an unregistered billing contract to be rejected")
		}

		insert(contractID, "ServiceContract", "Active", map[string]interface{}{
			"code": contractID, "customer": "TEST378C-CUST", "start_date": "2026-01-01", "end_date": "2026-12-31",
			"visits_included": 2.0, "visits_used": 0.0,
		})

		if err := ConsumeServiceContractVisit(tenantID, contractID); err != nil {
			t.Fatalf("ConsumeServiceContractVisit (1st): %v", err)
		}
		if err := ConsumeServiceContractVisit(tenantID, contractID); err != nil {
			t.Fatalf("ConsumeServiceContractVisit (2nd): %v", err)
		}
		if err := ConsumeServiceContractVisit(tenantID, contractID); err == nil {
			t.Fatalf("expected a 3rd visit against a 2-visit contract to be refused")
		}

		var visitsUsed float64
		db.DB.QueryRow("SELECT (data->>'visits_used')::numeric FROM "+schema+".documents WHERE id=$1", contractID).Scan(&visitsUsed)
		if visitsUsed != 2 {
			t.Fatalf("expected visits_used=2, got %v", visitsUsed)
		}

		// Reset to 0 used so a real ticket-close flow can consume one.
		db.DB.Exec("UPDATE "+schema+".documents SET data = jsonb_set(data, '{visits_used}', to_jsonb(0)) WHERE id=$1", contractID)
		ticketID, err := CreateServiceTicket(tenantID, "TEST378C-CUST", "Annual checkup", "Medium", "", contractID, "", "", "system")
		if err != nil {
			t.Fatalf("CreateServiceTicket: %v", err)
		}
		defer cleanupIDs(ticketID)
		if err := AssignServiceTicket(tenantID, ticketID, "tech2"); err != nil {
			t.Fatalf("AssignServiceTicket: %v", err)
		}
		if err := StartServiceTicket(tenantID, ticketID); err != nil {
			t.Fatalf("StartServiceTicket: %v", err)
		}
		if err := ResolveServiceTicket(tenantID, ticketID, "annual service done"); err != nil {
			t.Fatalf("ResolveServiceTicket: %v", err)
		}
		if err := CloseServiceTicket(tenantID, ticketID); err != nil {
			t.Fatalf("CloseServiceTicket: %v", err)
		}
		db.DB.QueryRow("SELECT (data->>'visits_used')::numeric FROM "+schema+".documents WHERE id=$1", contractID).Scan(&visitsUsed)
		if visitsUsed != 1 {
			t.Fatalf("expected closing a contract-linked ticket to consume exactly 1 visit, got visits_used=%v", visitsUsed)
		}
	})

	t.Run("GetServiceSLABreaches finds an overdue Draft (response breach) and an overdue InProgress (resolution breach), excludes not-yet-due and terminal tickets", func(t *testing.T) {
		const overdueDraft, overdueInProgress, notYetDue, resolvedButOverdue = "TEST378D-1", "TEST378D-2", "TEST378D-3", "TEST378D-4"
		cleanupIDs(overdueDraft, overdueInProgress, notYetDue, resolvedButOverdue)
		defer cleanupIDs(overdueDraft, overdueInProgress, notYetDue, resolvedButOverdue)

		yesterday := time.Now().AddDate(0, 0, -1).Format("2006-01-02")
		tomorrow := time.Now().AddDate(0, 0, 1).Format("2006-01-02")

		insert(overdueDraft, "ServiceTicket", "Draft", map[string]interface{}{"code": overdueDraft, "customer": "C1", "respond_by_date": yesterday})
		insert(overdueInProgress, "ServiceTicket", "InProgress", map[string]interface{}{"code": overdueInProgress, "customer": "C2", "resolve_by_date": yesterday})
		insert(notYetDue, "ServiceTicket", "Assigned", map[string]interface{}{"code": notYetDue, "customer": "C3", "resolve_by_date": tomorrow})
		insert(resolvedButOverdue, "ServiceTicket", "Resolved", map[string]interface{}{"code": resolvedButOverdue, "customer": "C4", "resolve_by_date": yesterday})

		breaches, err := GetServiceSLABreaches(tenantID)
		if err != nil {
			t.Fatalf("GetServiceSLABreaches: %v", err)
		}
		found := map[string]string{}
		for _, b := range breaches {
			found[b["ticket_id"].(string)] = b["breach_type"].(string)
		}
		if found[overdueDraft] != "Response" {
			t.Fatalf("expected %s to be a Response breach, got %q", overdueDraft, found[overdueDraft])
		}
		if found[overdueInProgress] != "Resolution" {
			t.Fatalf("expected %s to be a Resolution breach, got %q", overdueInProgress, found[overdueInProgress])
		}
		if _, ok := found[notYetDue]; ok {
			t.Fatalf("expected a not-yet-due ticket to be excluded from breaches")
		}
		if _, ok := found[resolvedButOverdue]; ok {
			t.Fatalf("expected a Resolved (terminal) ticket to be excluded from breaches even if its date has passed")
		}
	})
}
