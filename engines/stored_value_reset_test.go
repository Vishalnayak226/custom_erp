package engines

import (
	"custom_erp/db"
	"encoding/json"
	"testing"
)

// Stage 47.16.4g: a client with gift card events can be reset - the gift
// card ledger's append-only trigger accepts the reset's sanctioned purge, as
// gl_postings_guard does - while any other delete or edit of an event is
// still refused. Runs on the connection production has (no sanction), so
// only the reset's own SET LOCAL can open the door.
func TestStage47164gResetWithGiftCardEvents(t *testing.T) {
	db.InitDB(testConnStrUnsanctioned())
	defer db.InitDB(testConnStr())
	tenantID, schema := uniqueLifecycleTenant(t)
	defer dropLifecycleTenant(tenantID, schema)
	if _, err := ProvisionTenantSchema(tenantID, schema, "0.1.0-test"); err != nil {
		t.Fatalf("provision: %v", err)
	}
	defer db.DB.Exec(`DELETE FROM public.backup_runs WHERE tenant_schema = $1`, schema)
	const loc = "STORE-4G"
	store := func() {
		t.Helper()
		raw, _ := json.Marshal(map[string]interface{}{"code": loc, "name": "Store 4G", "type": "Store", "sellable": "Yes"})
		if _, err := db.DB.Exec(`INSERT INTO `+schema+`.documents (id, doctype, data, status, created_by) VALUES ($1, 'Location', $2, 'Active', 'system')`, loc, raw); err != nil {
			t.Fatal(err)
		}
	}
	store()
	issue := func(key string) {
		t.Helper()
		reg, err := RegisterStoredValueCards(tenantID, "manager", RegisterCardsRequest{Profile: "Standard Gift Card", Generate: 1})
		if err != nil || len(reg.Numbers) != 1 {
			t.Fatalf("register a card: %v", err)
		}
		if _, err := IssueStoredValue(tenantID, "cashier", StoredValueIssueRequest{CardNumber: reg.Numbers[0], Amount: 1000, PaymentMode: "Cash", Location: loc, IdempotencyKey: key}); err != nil {
			t.Fatalf("issue a card: %v", err)
		}
	}
	events := func() int {
		var n int
		db.DB.QueryRow(`SELECT COUNT(*) FROM ` + schema + `.stored_value_ledger`).Scan(&n)
		return n
	}
	issue("k-4g-1")
	if events() == 0 {
		t.Fatal("issuing a card wrote no gift card event")
	}
	// Unsanctioned, an event is neither deleted nor edited.
	if _, err := db.DB.Exec(`DELETE FROM ` + schema + `.stored_value_ledger`); err == nil {
		t.Fatal("the gift card ledger accepted an unsanctioned DELETE")
	}
	if _, err := db.DB.Exec(`UPDATE ` + schema + `.stored_value_ledger SET amount_paise = 0`); err == nil {
		t.Fatal("the gift card ledger accepted an UPDATE")
	}

	// The reset empties it.
	if err := SuspendTenant(tenantID, "test", "reset"); err != nil {
		t.Fatalf("suspend: %v", err)
	}
	if _, err := db.DB.Exec(`INSERT INTO public.backup_runs (scope, tenant_schema, outcome, finished_at) VALUES ('tenant', $1, 'Success', NOW())`, schema); err != nil {
		t.Fatal(err)
	}
	if _, err := ResetTenantData(tenantID, "test", "clean slate with gift cards", approvedClientRemoval(t, tenantID, ClientRemovalReset)); err != nil {
		t.Fatalf("reset of a client with gift card events: %v", err)
	}
	if n := events(); n != 0 {
		t.Fatalf("the reset left %d gift card event(s)", n)
	}

	// After the reset the guard still holds, and an UPDATE under the purge
	// sanction is still refused - the sanction opens DELETE only.
	if err := ResumeTenant(tenantID, "test", "clean slate ready"); err != nil {
		t.Fatalf("resume: %v", err)
	}
	store() // the reset removed the store with the rest
	issue("k-4g-2")
	if _, err := db.DB.Exec(`DELETE FROM ` + schema + `.stored_value_ledger`); err == nil {
		t.Fatal("after the reset the gift card ledger accepted an unsanctioned DELETE")
	}
	tx, err := db.DB.Begin()
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	if err := sanctionWriteTx(tx, sanctionPurge); err != nil {
		t.Fatal(err)
	}
	if _, err := tx.Exec(`UPDATE ` + schema + `.stored_value_ledger SET amount_paise = 0`); err == nil {
		t.Fatal("the purge sanction let an UPDATE through")
	}
}
