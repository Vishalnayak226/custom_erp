package engines

import (
	"custom_erp/db"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"
)

type clientRemovalAdmin struct{ id, username string }

// clientRemovalAdmins makes two throwaway admins of the operator's client
// (default: ERP_PLATFORM_OPERATORS is cleared for the test), removed again
// with the requests they made.
func clientRemovalAdmins(t *testing.T) (asker, approver clientRemovalAdmin) {
	t.Helper()
	t.Setenv("ERP_PLATFORM_OPERATORS", "")
	schema, err := db.GetTenantSchema(ClientRemovalTenant())
	if err != nil {
		t.Fatalf("operator client: %v", err)
	}
	suffix := fmt.Sprintf("%d", time.Now().UnixNano()%1_000_000_000_000)
	asker = clientRemovalAdmin{"cdra" + suffix, "cdra" + suffix}
	approver = clientRemovalAdmin{"cdrb" + suffix, "cdrb" + suffix}
	ids := []string{asker.id, approver.id}
	for _, u := range []clientRemovalAdmin{asker, approver} {
		if _, err := db.DB.Exec(`INSERT INTO `+schema+`.users (id, username, password_hash, email, role, status) VALUES ($1, $2, 'x', $3, 'Super Admin', 'Active')`,
			u.id, u.username, u.username+"@test.local"); err != nil {
			t.Fatalf("admin %s: %v", u.username, err)
		}
	}
	t.Cleanup(func() {
		db.DB.Exec(`DELETE FROM `+schema+`.approval_log WHERE doctype = $1 AND document_id IN (SELECT id FROM `+schema+`.documents WHERE doctype = $1 AND created_by = ANY($2))`,
			ClientDataRemovalDoctype, pqStringArray(ids))
		db.DB.Exec(`DELETE FROM `+schema+`.documents WHERE doctype = $1 AND created_by = ANY($2)`, ClientDataRemovalDoctype, pqStringArray(ids))
		db.DB.Exec(`DELETE FROM `+schema+`.users WHERE id = ANY($1)`, pqStringArray(ids))
	})
	return asker, approver
}

// approvedClientRemoval files a request for action on tenantID and has a
// second admin approve it, returning its id - what a reset or purge needs.
func approvedClientRemoval(t *testing.T, tenantID, action string) string {
	t.Helper()
	asker, approver := clientRemovalAdmins(t)
	id, err := RequestClientDataRemoval(tenantID, action, "test "+action, asker.username)
	if err != nil {
		t.Fatalf("request %s: %v", action, err)
	}
	if err := DecideApproval(ClientRemovalTenant(), ClientDataRemovalDoctype, id, approver.id, RoleSuperAdmin, "", "Approved", "ok"); err != nil {
		t.Fatalf("approve %s: %v", id, err)
	}
	return id
}

// Stage 49.10.6b (4): a data reset or a purge needs a second person through
// the approval engine - one admin of the operator's client asks, another
// approves in Approvals, and only then does it run, once, for the client and
// the act asked for. Both people are on the evidence trail.
func TestStage49106bRemovalNeedsASecondPerson(t *testing.T) {
	db.InitDB(testConnStr())
	tenantID, schema := uniqueLifecycleTenant(t)
	defer dropLifecycleTenant(tenantID, schema)
	if _, err := ProvisionTenantSchema(tenantID, schema, "0.1.0-test"); err != nil {
		t.Fatalf("provision: %v", err)
	}
	defer db.DB.Exec(`DELETE FROM public.backup_runs WHERE tenant_schema = $1`, schema)
	if err := SuspendTenant(tenantID, "test", "reset"); err != nil {
		t.Fatalf("suspend: %v", err)
	}
	if _, err := db.DB.Exec(`INSERT INTO public.backup_runs (scope, tenant_schema, outcome, finished_at) VALUES ('tenant', $1, 'Success', NOW())`, schema); err != nil {
		t.Fatal(err)
	}
	asker, approver := clientRemovalAdmins(t)
	home := ClientRemovalTenant()

	// One person alone gets nowhere.
	if _, err := ResetTenantData(tenantID, "test", "clean slate", ""); err == nil || !strings.Contains(err.Error(), "second person") {
		t.Fatalf("a reset with no approved request: %v", err)
	}
	if _, err := RequestClientDataRemoval(tenantID, ClientRemovalReset, "clean slate", "cashier1"); err == nil {
		t.Fatal("a cashier's login asked for a reset")
	}
	if _, err := RequestClientDataRemoval(tenantID, "drop", "clean slate", asker.username); err == nil {
		t.Fatal("an unknown action was taken")
	}
	if _, err := RequestClientDataRemoval(tenantID, ClientRemovalReset, " ", asker.username); err == nil {
		t.Fatal("a request with no reason was taken")
	}

	id, err := RequestClientDataRemoval(tenantID, ClientRemovalReset, "clean slate", asker.username)
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	if _, status, _, _ := fetchDocument(home, ClientDataRemovalDoctype, id); status != "Pending Approval" {
		t.Fatalf("the request is %q, want it in Approvals", status)
	}
	if _, err := ResetTenantData(tenantID, "test", "clean slate", id); err == nil || !strings.Contains(err.Error(), "Pending Approval") {
		t.Fatalf("a request nobody approved ran: %v", err)
	}
	// The asker cannot approve it, whatever the self-approval setting says.
	err = DecideApproval(home, ClientDataRemovalDoctype, id, asker.id, RoleSuperAdmin, "", "Approved", "")
	var ve *ValidationError
	if !errors.As(err, &ve) || ve.Code != "APPROV-0840" {
		t.Fatalf("the asker approving their own request: %v, want APPROV-0840", err)
	}
	if err := DecideApproval(home, ClientDataRemovalDoctype, id, approver.id, RoleSuperAdmin, "", "Approved", "checked the ticket"); err != nil {
		t.Fatalf("the second admin's approval: %v", err)
	}

	// The approval covers this client and this act only.
	for _, c := range []struct{ tenant, action string }{{tenantID, ClientRemovalPurge}, {"someone-else", ClientRemovalReset}} {
		tx, err := db.DB.Begin()
		if err != nil {
			t.Fatal(err)
		}
		if _, err := spendClientRemovalApprovalTx(tx, c.tenant, c.action, id, "test"); err == nil {
			t.Errorf("a reset approval for %s was spent on a %s of %s", tenantID, c.action, c.tenant)
		}
		tx.Rollback()
	}

	if done, err := ResetTenantData(tenantID, "test", "clean slate", id); err != nil || !done.Applied {
		t.Fatalf("the approved reset: %v", err)
	}
	var detail string
	db.DB.QueryRow(`SELECT detail FROM public.tenant_lifecycle_events WHERE tenant_id = $1 AND event = $2 ORDER BY id DESC LIMIT 1`, tenantID, TenantEventDataReset).Scan(&detail)
	if !strings.Contains(detail, asker.username) || !strings.Contains(detail, approver.username) || !strings.Contains(detail, id) {
		t.Errorf("the reset's evidence %q must name both people and the request", detail)
	}
	events := eventNames(t, tenantID)
	if !hasEvent(events, TenantEventRemovalRequested) || !hasEvent(events, TenantEventRemovalApproved) {
		t.Errorf("the request and its approval are not on the client's trail: %v", events)
	}
	var audited int
	db.DB.QueryRow(`SELECT COUNT(*) FROM tenant_default.audit_logs WHERE action = 'CLIENT_DATA_REMOVAL' AND details LIKE '%' || $1 || '%'`, id).Scan(&audited)
	if audited < 2 {
		t.Errorf("the request and the approval wrote %d audit row(s), want both", audited)
	}

	// Spent: it does not run twice.
	if _, err := ResetTenantData(tenantID, "test", "clean slate", id); err == nil {
		t.Fatal("a spent request ran a second reset")
	}

	// An approval goes stale after a week; a rejection is evidenced.
	stale, err := RequestClientDataRemoval(tenantID, ClientRemovalReset, "again", asker.username)
	if err != nil {
		t.Fatal(err)
	}
	if err := DecideApproval(home, ClientDataRemovalDoctype, stale, approver.id, RoleSuperAdmin, "", "Approved", ""); err != nil {
		t.Fatal(err)
	}
	db.DB.Exec(`UPDATE tenant_default.approval_log SET created_at = created_at - INTERVAL '8 days' WHERE doctype = $1 AND document_id = $2 AND action = 'Approved'`, ClientDataRemovalDoctype, stale)
	if _, err := ResetTenantData(tenantID, "test", "again", stale); err == nil || !strings.Contains(err.Error(), "days ago") {
		t.Fatalf("a week-old approval: %v", err)
	}
	refused, err := RequestClientDataRemoval(tenantID, ClientRemovalPurge, "contract ended", asker.username)
	if err != nil {
		t.Fatal(err)
	}
	if err := DecideApproval(home, ClientDataRemovalDoctype, refused, approver.id, RoleSuperAdmin, "", "Rejected", "no ticket"); err != nil {
		t.Fatal(err)
	}
	if !hasEvent(eventNames(t, tenantID), TenantEventRemovalRejected) {
		t.Error("a rejected request left no evidence")
	}
}
