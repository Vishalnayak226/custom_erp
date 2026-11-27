package engines

import (
	"reflect"
	"sort"
	"testing"

	"custom_erp/db"
)

// 283.2: dueSchemas sends a worker only to the clients with a due row, and
// falls back to every client when it cannot tell.
func TestWorkerSweepDueSchemasVisitsOnlyDueClients(t *testing.T) {
	db.InitDB(testConnStr())
	a, b := "sweep283_due_a", "sweep283_due_b"
	drop := func() {
		_, _ = db.DB.Exec(`DROP SCHEMA IF EXISTS sweep283_due_a CASCADE`)
		_, _ = db.DB.Exec(`DROP SCHEMA IF EXISTS sweep283_due_b CASCADE`)
	}
	drop()
	t.Cleanup(drop)
	exec := func(q string) {
		t.Helper()
		if _, err := db.DB.Exec(q); err != nil {
			t.Fatalf("%s: %v", q, err)
		}
	}
	for _, s := range []string{a, b} {
		exec(`CREATE SCHEMA ` + s)
		exec(`CREATE TABLE ` + s + `.q (status text)`)
	}
	exec(`INSERT INTO ` + a + `.q VALUES ('Due'), ('Done')`)
	exec(`INSERT INTO ` + b + `.q VALUES ('Done')`)
	const due = `status = 'Due'`

	if got := dueSchemas([]string{a, b}, "q", due); !reflect.DeepEqual(got, []string{a}) {
		t.Fatalf("due clients = %v, want only %s", got, a)
	}
	if got := dueSchemas([]string{b}, "q", due); len(got) != 0 {
		t.Fatalf("a client with nothing due was still visited: %v", got)
	}
	// more clients than one statement holds
	many := make([]string, 0, 451)
	for i := 0; i < 450; i++ {
		many = append(many, b)
	}
	many = append(many, a)
	if got := dueSchemas(many, "q", due); !reflect.DeepEqual(got, []string{a}) {
		t.Fatalf("across chunks the due clients = %v, want only %s", got, a)
	}
	// cannot tell: every client is visited, as before 283.2
	for _, list := range [][]string{{a, b, "sweep283_missing"}, {a, "bad name"}} {
		if got := dueSchemas(list, "q", due); !reflect.DeepEqual(got, list) {
			t.Fatalf("on error dueSchemas returned %v, want every client %v", got, list)
		}
	}
}

// Each worker's due condition is valid SQL on a real client, and the outbox
// one finds a pending event and stops finding it once sent.
func TestWorkerSweepDueConditionsMatchTheQueues(t *testing.T) {
	db.InitDB(testConnStr())
	tenantID, schema := uniqueLifecycleTenant(t)
	if _, err := ProvisionTenantSchema(tenantID, schema, "0.1.0-test"); err != nil {
		t.Fatalf("ProvisionTenantSchema: %v", err)
	}
	t.Cleanup(func() { dropLifecycleTenant(tenantID, schema) })
	for table, where := range map[string]string{
		"integration_event_outbox": outboxDueWhere,
		"async_jobs":               jobDueWhere,
		"pim_publish_queue":        publishQueueDueWhere,
		"documents":                reportExportDueWhere,
	} {
		if _, err := findDueSchemas([]string{"tenant_default", schema}, table, where); err != nil {
			t.Errorf("%s due condition: %v", table, err)
		}
	}
	var id string
	if err := db.DB.QueryRow(`INSERT INTO `+schema+`.integration_event_outbox (event_name, payload, status) VALUES ('sweep.test', '{}', 'Pending') RETURNING id`).Scan(&id); err != nil {
		t.Fatalf("insert outbox row: %v", err)
	}
	if got, err := findDueSchemas([]string{schema}, "integration_event_outbox", outboxDueWhere); err != nil || !reflect.DeepEqual(got, []string{schema}) {
		t.Fatalf("a pending event was not found: %v %v", got, err)
	}
	if _, err := db.DB.Exec(`UPDATE `+schema+`.integration_event_outbox SET status = 'Dispatched' WHERE id = $1`, id); err != nil {
		t.Fatal(err)
	}
	if got, err := findDueSchemas([]string{schema}, "integration_event_outbox", outboxDueWhere); err != nil || len(got) != 0 {
		t.Fatalf("a dispatched event still made the client due: %v %v", got, err)
	}
}

// sweepTenantSchemas returns what listTenantSchemas returns, and a change
// made through the app is seen at once.
func TestWorkerSweepViewMatchesListAndForgets(t *testing.T) {
	db.InitDB(testConnStr())
	t.Setenv("AUTH_STATE_CACHE_SECONDS", "300")
	forgetSweepView()
	t.Cleanup(forgetSweepView)
	has := func(schema string, keys ...string) bool {
		t.Helper()
		list, err := sweepTenantSchemas(keys...)
		if err != nil {
			t.Fatalf("sweepTenantSchemas: %v", err)
		}
		return containsString(list, schema)
	}

	// the view is primed before the client exists; provisioning drops it
	has("tenant_default")
	tenantID, schema := uniqueLifecycleTenant(t)
	if _, err := ProvisionTenantSchema(tenantID, schema, "0.1.0-test"); err != nil {
		t.Fatalf("ProvisionTenantSchema: %v", err)
	}
	t.Cleanup(func() { dropLifecycleTenant(tenantID, schema) })
	if !has(schema) {
		t.Fatalf("a newly provisioned client is missing from the workers' view")
	}

	for _, keys := range [][]string{nil, {"hr"}, {"crm_loyalty"}, {"pim", "reports"}} {
		want, err := listTenantSchemas(keys...)
		if err != nil {
			t.Fatal(err)
		}
		got, err := sweepTenantSchemas(keys...)
		if err != nil {
			t.Fatal(err)
		}
		sort.Strings(want)
		sort.Strings(got)
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("keys %v: view %v, listTenantSchemas %v", keys, got, want)
		}
	}

	before := has(schema, "hr")
	setRaw := func(on bool) {
		t.Helper()
		if _, err := db.DB.Exec(`UPDATE `+schema+`.module_entitlements SET enabled = $1 WHERE module_key = 'hr'`, on); err != nil {
			t.Fatal(err)
		}
	}
	setRaw(!before)
	if has(schema, "hr") != before {
		t.Fatalf("a raw database change was seen inside the window - the view is not shared")
	}
	forgetSweepView()
	if has(schema, "hr") == before {
		t.Fatalf("after forgetSweepView the module switch is still the old one")
	}
	if err := SetModuleEntitlement(tenantID, "hr", before, "test"); err != nil {
		t.Fatalf("SetModuleEntitlement: %v", err)
	}
	if has(schema, "hr") != before {
		t.Fatalf("a module switched in the app was not seen on the next tick")
	}

	// 0 turns the view off: every call reads afresh
	t.Setenv("AUTH_STATE_CACHE_SECONDS", "0")
	setRaw(!before)
	if has(schema, "hr") == before {
		t.Fatalf("with AUTH_STATE_CACHE_SECONDS=0 a change was not seen at once")
	}
}

// The CRM worker skips the ticks with nothing to act on, and only then.
func TestCRMWorkerIdleSkipsOnlyIdleTicks(t *testing.T) {
	crmTicksMu.Lock()
	registered := map[string]bool{}
	for _, tick := range crmTicks {
		registered[tick.name] = true
	}
	crmTicksMu.Unlock()
	for name := range crmIdleSkipTicks {
		if !registered[name] {
			t.Errorf("crmIdleSkipTicks names %q, which is not a registered CRM tick", name)
		}
	}

	e := newCRMTestEnv(t)
	if crmWorkInFlight(e.schema) {
		t.Fatalf("a new client with no CRM rows reads as busy")
	}
	if !crmWorkInFlight("sweep283_no_such_schema") {
		t.Fatalf("a failed check must read as busy, so every tick runs")
	}
	e.mockSender(CRMChannelWhatsApp, "accept")
	tpl := e.template(CRMChannelWhatsApp, CRMPurposeService, "Your order is ready, {{customer.first_name|there}}.")
	c, phone := e.customer("Asha Rao", nil)
	e.allow(e.e164(phone))
	id := e.queue(CRMSendRequest{RunKind: "staff", RunID: "MSG-283", CustomerID: c, Channel: CRMChannelWhatsApp, Purpose: CRMPurposeService, TemplateID: tpl})
	if !crmWorkInFlight(e.schema) {
		t.Fatalf("a queued message does not read as work in flight")
	}
	RunCRMTickForSchema(e.schema)
	if st, _ := e.sendStatus(id); st == crmSendQueued {
		t.Fatalf("the worker tick did not dispatch a queued message")
	}
}
