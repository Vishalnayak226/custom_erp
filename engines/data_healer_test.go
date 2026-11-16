package engines

import (
	"encoding/json"
	"fmt"
	"sync"
	"testing"

	"custom_erp/db"
)

// healTestTenant is a scratch client with the DataHeal type (207.1), so the
// test runs on a database the migration has not reached yet as well.
func healTestTenant(t *testing.T) (tenantID, schema string) {
	t.Helper()
	db.InitDB(testConnStr())
	tenantID, schema = uniqueLifecycleTenant(t)
	t.Cleanup(func() { dropLifecycleTenant(tenantID, schema) })
	if _, err := ProvisionTenantSchema(tenantID, schema, "0.1.0-test"); err != nil {
		t.Fatalf("ProvisionTenantSchema: %v", err)
	}
	if _, err := db.DB.Exec(fmt.Sprintf(`INSERT INTO %s.doctype_meta (name, module, module_key, document_type)
		VALUES ('DataHeal', 'Master Data', 'master_data', 'Master') ON CONFLICT (name) DO NOTHING`, schema)); err != nil {
		t.Fatalf("DataHeal type: %v", err)
	}
	return tenantID, schema
}

func healSeed(t *testing.T, schema, doctype, id string, data map[string]interface{}) {
	t.Helper()
	raw, _ := json.Marshal(data)
	if _, err := db.DB.Exec(fmt.Sprintf(`INSERT INTO %s.documents (id, doctype, data, status, created_by)
		VALUES ($1, $2, $3, 'Active', 'tester')`, schema), id, doctype, raw); err != nil {
		t.Fatalf("seed %s %s: %v", doctype, id, err)
	}
}

func healAlias(t *testing.T, schema, id string) string {
	t.Helper()
	var a string
	if err := db.DB.QueryRow(fmt.Sprintf(`SELECT COALESCE(data->>'item_alias', '') FROM %s.documents WHERE id = $1`, schema), id).Scan(&a); err != nil {
		t.Fatalf("read %s: %v", id, err)
	}
	return a
}

func healStateFor(t *testing.T, tenant string) HealState {
	t.Helper()
	list, err := ListHeals(tenant)
	if err != nil {
		t.Fatalf("ListHeals: %v", err)
	}
	for _, h := range list {
		if h.ID == healItemAlias {
			return h
		}
	}
	t.Fatal("the item-alias heal is not listed")
	return HealState{}
}

func TestDataHealNothingToFixIsDoneAtFirstLook(t *testing.T) {
	tenant, schema := healTestTenant(t)
	st := healStateFor(t, tenant)
	if st.Status != HealStatusDone || st.DoneBy != "system" || st.Fixed != 0 {
		t.Fatalf("a client with no items should show the heal done by system, got %+v", st)
	}
	// An item saved later without an alias does not bring the heal back.
	healSeed(t, schema, "Item", "HEAL-T-LATE", map[string]interface{}{"name": "Late Item"})
	if st := healStateFor(t, tenant); st.Status != HealStatusDone {
		t.Fatalf("a Done heal came back: %+v", st)
	}
}

func TestDataHealItemAliasLifecycle(t *testing.T) {
	tenant, schema := healTestTenant(t)
	healSeed(t, schema, "Category", "HEAL-T-CAT", map[string]interface{}{"code": "ANT", "name": "Antique"})
	healSeed(t, schema, "ItemName", "HEAL-T-IN1", map[string]interface{}{"code": "IN1", "name": "Antique Zircon Necklace Set", "category": "ANT"})
	healSeed(t, schema, "ItemName", "HEAL-T-IN2", map[string]interface{}{"code": "IN2", "name": "Ring", "item_alias": "RNG"})
	healSeed(t, schema, "ProductFamily", "HEAL-T-D1", map[string]interface{}{"code": "D1", "item_name": "IN2"})
	healSeed(t, schema, "Item", "HEAL-T-I1", map[string]interface{}{"code": "I1", "name": "Necklace one", "item_name": "IN1"})
	healSeed(t, schema, "Item", "HEAL-T-I2", map[string]interface{}{"code": "I2", "name": "Ring combination", "family": "D1"})
	healSeed(t, schema, "Item", "HEAL-T-I3", map[string]interface{}{"code": "I3", "name": "Loose", "item_alias": "Typed"})
	healSeed(t, schema, "Item", "HEAL-T-I4", map[string]interface{}{"code": "I4", "name": "Plain Bangle"})

	st := healStateFor(t, tenant)
	if st.Status != HealStatusWaiting || st.Count != 4 {
		t.Fatalf("want 4 records waiting (IN1, I1, I2, I4), got %+v", st)
	}

	// The screen's proposals are what a save would give each record today.
	_, recs, err := HealRecords(tenant, healItemAlias)
	if err != nil {
		t.Fatalf("HealRecords: %v", err)
	}
	if len(recs) != 4 || recs[0].Doctype != "ItemName" {
		t.Fatalf("want the item name first then 3 items, got %+v", recs)
	}
	short := itemAliasShortFormsForSchema(schema)
	for _, r := range recs {
		var raw string
		db.DB.QueryRow(fmt.Sprintf(`SELECT data::text FROM %s.documents WHERE id = $1`, schema), r.ID).Scan(&raw)
		var d map[string]interface{}
		json.Unmarshal([]byte(raw), &d)
		if want := autoItemAlias(schema, r.Doctype, d, short); r.Proposed != want {
			t.Errorf("%s proposed %q, a save would give %q", r.ID, r.Proposed, want)
		}
	}

	// A typed value over the limit is refused before anything is written.
	_, err = RunHeal(tenant, healItemAlias, "tester", map[string]string{"ItemName/HEAL-T-IN1": "Far too long for a sticker"})
	wantValidationCode(t, err, "PIM-9363")
	if st := healStateFor(t, tenant); st.Status != HealStatusWaiting || st.Count != 4 {
		t.Fatalf("a refused run changed something: %+v", st)
	}

	// Two admins press Run together: one heals, the other is told it is done.
	typed := map[string]string{"ItemName/HEAL-T-IN1": "AZ Necklace", "Item/NOT-THERE": "Ignored"}
	var wg sync.WaitGroup
	results := make([]HealRunResult, 2)
	errs := make([]error, 2)
	for i := range results {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			results[i], errs[i] = RunHeal(tenant, healItemAlias, fmt.Sprintf("admin%d", i), typed)
		}(i)
	}
	wg.Wait()
	for _, e := range errs {
		if e != nil {
			t.Fatalf("run: %v", e)
		}
	}
	ran, done := results[0], results[1]
	if ran.AlreadyDone {
		ran, done = done, ran
	}
	if ran.AlreadyDone || !done.AlreadyDone || ran.Fixed != 4 || done.Fixed != 0 {
		t.Fatalf("want one run fixing 4 and one already done, got %+v and %+v", results[0], results[1])
	}

	if a := healAlias(t, schema, "HEAL-T-IN1"); a != "AZ Necklace" {
		t.Errorf("item name took %q, want the typed value", a)
	}
	if a := healAlias(t, schema, "HEAL-T-I1"); a != "AZ Necklace" {
		t.Errorf("its item took %q, want its item name's typed alias", a)
	}
	if a := healAlias(t, schema, "HEAL-T-I2"); a != "RNG" {
		t.Errorf("design's item took %q, want RNG", a)
	}
	if a, want := healAlias(t, schema, "HEAL-T-I4"), MakeItemAlias("Plain Bangle", "", short); a != want || a == "" {
		t.Errorf("standalone item took %q, want %q", a, want)
	}
	if a := healAlias(t, schema, "HEAL-T-I3"); a != "Typed" {
		t.Errorf("an alias set by hand was changed to %q", a)
	}

	st = healStateFor(t, tenant)
	if st.Status != HealStatusDone || st.Fixed != 4 || st.Typed != 1 || st.DoneAt == "" {
		t.Fatalf("want Done with 4 fixed and 1 typed, got %+v", st)
	}
	var audits int
	db.DB.QueryRow(fmt.Sprintf(`SELECT COUNT(*) FROM %s.audit_logs WHERE action = 'DATA_HEAL_RUN'`, schema)).Scan(&audits)
	if audits != 1 {
		t.Errorf("want one audit row for the run, got %d", audits)
	}

	// Done is final: a new blank item stays as it is and Run changes nothing.
	healSeed(t, schema, "Item", "HEAL-T-I5", map[string]interface{}{"name": "After the heal"})
	again, err := RunHeal(tenant, healItemAlias, "tester", nil)
	if err != nil || !again.AlreadyDone || again.Fixed != 0 {
		t.Fatalf("a second run should change nothing, got %+v %v", again, err)
	}
	if a := healAlias(t, schema, "HEAL-T-I5"); a != "" {
		t.Errorf("a Done heal ran again and wrote %q", a)
	}
}

func TestDataHealRefusals(t *testing.T) {
	tenant, _ := healTestTenant(t)
	_, err := RunHeal(tenant, "no-such-heal", "tester", nil)
	wantValidationCode(t, err, "MASTER-1703")
	_, _, err = HealRecords(tenant, "no-such-heal")
	wantValidationCode(t, err, "MASTER-1703")
	wantValidationCode(t, ValidateMasterDataRules(tenant, "", "DataHeal", map[string]interface{}{"status": "Done"}), "MASTER-1704")
}
