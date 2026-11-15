package engines

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"custom_erp/db"
)

// 50.12 / BLD-024..027 (ask 193): package transitions, downgrade and
// re-enable, the Razorpay subscription lifecycle against a local fake, and
// metering with Indian-time period boundaries.

func saasCleanup(t *testing.T, tenantID string, planKeys ...string) {
	t.Helper()
	t.Cleanup(func() {
		for _, q := range []string{
			`DELETE FROM public.saas_subscription_events WHERE tenant_id = $1 OR event_id LIKE '%' || $1`,
			`DELETE FROM public.saas_invoices WHERE tenant_id = $1`,
			`DELETE FROM public.saas_usage WHERE tenant_id = $1`,
			`DELETE FROM public.saas_package_transitions WHERE tenant_id = $1`,
			`DELETE FROM public.saas_subscriptions WHERE tenant_id = $1`,
		} {
			_, _ = db.DB.Exec(q, tenantID)
		}
		for _, p := range planKeys {
			_, _ = db.DB.Exec(`DELETE FROM public.saas_plans WHERE plan_key = $1`, p)
		}
	})
}

func moduleSet(t *testing.T, tenantID string) map[string]bool {
	t.Helper()
	schema, _ := db.GetTenantSchema(tenantID)
	on, _, err := enabledOptionalModules(db.DB, schema)
	if err != nil {
		t.Fatal(err)
	}
	return on
}

func inSchemas(t *testing.T, schema, module string) bool {
	t.Helper()
	list, err := listTenantSchemas(module)
	if err != nil {
		t.Fatal(err)
	}
	for _, s := range list {
		if s == schema {
			return true
		}
	}
	return false
}

func TestBLD024PackageTransitionContract(t *testing.T) {
	tenantID, schema, insert := stage140Tenant(t)
	saasCleanup(t, tenantID)
	insert("C-A8", "Customer", "Active", "system", map[string]interface{}{"code": "C-A8", "name": "A8"})

	// BLD-023: add-ons alone, nothing, and unknown packages are unsupported.
	for _, bad := range [][]string{{}, {"hr"}, {"hr", "crm"}, {"nonsense"}} {
		if ok, _ := ValidatePackageBundle(bad); ok {
			t.Fatalf("%v accepted; want unsupported", bad)
		}
	}
	for _, good := range [][]string{{"erp_full"}, {"pim", "hr"}, {"wms", "oms", "procurement"}} {
		if ok, why := ValidatePackageBundle(good); !ok {
			t.Fatalf("%v refused: %s", good, why)
		}
	}

	// Refused: nothing changes and the refusal is on the record.
	before := moduleSet(t, tenantID)
	if _, err := TransitionTenantPackages(tenantID, []string{"hr"}, "op", "req-bad", true); err == nil {
		t.Fatal("an add-on alone was applied")
	}
	if got := moduleSet(t, tenantID); fmt.Sprint(saasSortedKeys(got)) != fmt.Sprint(saasSortedKeys(before)) {
		t.Fatalf("a refused transition changed modules: %v -> %v", saasSortedKeys(before), saasSortedKeys(got))
	}
	var refused int
	_ = db.DB.QueryRow(`SELECT count(*) FROM public.saas_package_transitions WHERE tenant_id = $1 AND status = 'refused'`, tenantID).Scan(&refused)
	if refused != 1 {
		t.Fatalf("refusals on record = %d; want 1", refused)
	}
	// An unknown package in a mixed list is atomic: the known one is not applied either.
	if _, err := TransitionTenantPackages(tenantID, []string{"pim", "nonsense"}, "op", "req-mixed", false); err == nil {
		t.Fatal("unknown package applied")
	}
	if got := moduleSet(t, tenantID); fmt.Sprint(saasSortedKeys(got)) != fmt.Sprint(saasSortedKeys(before)) {
		t.Fatalf("partial application: %v", saasSortedKeys(got))
	}

	// Full ERP, then an impact preview of the downgrade to OMS with an open HR draft.
	if _, err := TransitionTenantPackages(tenantID, []string{"erp_full"}, "op", "req-full", true); err != nil {
		t.Fatal(err)
	}
	var hrDoctype string
	if err := db.DB.QueryRow(fmt.Sprintf(`SELECT name FROM %s.doctype_meta WHERE module_key = 'hr' ORDER BY name LIMIT 1`, schema)).Scan(&hrDoctype); err != nil {
		t.Fatalf("no hr doctype: %v", err)
	}
	insert("HRDRAFT-A8", hrDoctype, "Draft", "system", map[string]interface{}{"note": "a8"})
	imp, err := ImpactOfPackages(tenantID, []string{"oms"})
	if err != nil {
		t.Fatal(err)
	}
	foundHR := false
	for _, w := range imp.OpenWork {
		if w.Module == "hr" && w.Doctype == hrDoctype && w.Count == 1 {
			foundHR = true
		}
	}
	if !imp.Supported || !foundHR || len(imp.Disable) == 0 || !strings.Contains(imp.Retention, "keep every record") {
		t.Fatalf("impact = %+v; want OMS supported, hr disabled with 1 open %s", imp, hrDoctype)
	}
	if !moduleSet(t, tenantID)["hr"] {
		t.Fatal("a preview changed the tenant")
	}

	// A retry with the same request id does nothing twice and says so.
	first, err := TransitionTenantPackages(tenantID, []string{"oms"}, "op", "req-oms", true)
	if err != nil || first.Replayed {
		t.Fatalf("first = %+v %v", first, err)
	}
	if _, err := TransitionTenantPackages(tenantID, []string{"erp_full"}, "someone-else", "req-oms", true); err != nil {
		t.Fatal(err)
	}
	if got := moduleSet(t, tenantID); got["hr"] || !got["oms"] {
		t.Fatalf("the replay re-applied a different plan: %v", saasSortedKeys(got))
	}
	var applied int
	_ = db.DB.QueryRow(`SELECT count(*) FROM public.saas_package_transitions WHERE tenant_id = $1 AND request_id = 'req-oms'`, tenantID).Scan(&applied)
	if applied != 1 {
		t.Fatalf("rows for one request id = %d", applied)
	}

	// Concurrent different requests: each is one whole transaction; the end state is exactly one of the two plans.
	var wg sync.WaitGroup
	for i, pk := range [][]string{{"wms"}, {"pim", "hr"}} {
		wg.Add(1)
		go func(i int, pk []string) {
			defer wg.Done()
			_, _ = TransitionTenantPackages(tenantID, pk, "op", fmt.Sprintf("req-conc-%d", i), true)
		}(i, pk)
	}
	wg.Wait()
	got := fmt.Sprint(saasSortedKeys(moduleSet(t, tenantID)))
	wantA := fmt.Sprint(saasSortedKeys(targetModules([]string{"wms"})))
	wantB := fmt.Sprint(saasSortedKeys(targetModules([]string{"pim", "hr"})))
	if got != wantA && got != wantB {
		t.Fatalf("concurrent transitions left a mixed state %s; want %s or %s", got, wantA, wantB)
	}
}

func TestBLD025DowngradeKeepsDataAndReenables(t *testing.T) {
	tenantID, schema, insert := stage140Tenant(t)
	saasCleanup(t, tenantID)
	var hrDoctype string
	if _, err := TransitionTenantPackages(tenantID, []string{"erp_full"}, "op", "r1", true); err != nil {
		t.Fatal(err)
	}
	if err := db.DB.QueryRow(fmt.Sprintf(`SELECT name FROM %s.doctype_meta WHERE module_key = 'hr' ORDER BY name LIMIT 1`, schema)).Scan(&hrDoctype); err != nil {
		t.Fatal(err)
	}
	insert("HR-KEEP-A8", hrDoctype, "Pending Approval", "system", map[string]interface{}{"note": "keep me"})
	if !inSchemas(t, schema, "hr") {
		t.Fatal("hr worker should serve a client that has hr")
	}

	if _, err := TransitionTenantPackages(tenantID, []string{"oms"}, "op", "r2", true); err != nil {
		t.Fatal(err)
	}
	// Future work stops: gate, module check and the module's workers.
	if on, _ := IsModuleEnabled(tenantID, "hr"); on {
		t.Fatal("hr still enabled after the downgrade")
	}
	if err := RequireModules(tenantID, "hr"); err == nil {
		t.Fatal("RequireModules let a switched-off module through")
	}
	if inSchemas(t, schema, "hr") {
		t.Fatal("hr workers still serve a client without hr")
	}
	// Data is kept, untouched, still pending.
	var status string
	var n int
	_ = db.DB.QueryRow(fmt.Sprintf(`SELECT count(*), COALESCE(max(status), '') FROM %s.documents WHERE id = 'HR-KEEP-A8' AND deleted_at IS NULL`, schema)).Scan(&n, &status)
	if n != 1 || status != "Pending Approval" {
		t.Fatalf("downgrade changed the record: n=%d status=%q", n, status)
	}

	// Interrupted/retry: asking for the same downgrade again is the same state.
	if _, err := TransitionTenantPackages(tenantID, []string{"oms"}, "op", "r2", true); err != nil {
		t.Fatal(err)
	}
	// Re-enable restores the module and the record is exactly as it was.
	if _, err := TransitionTenantPackages(tenantID, []string{"erp_full"}, "op", "r3", true); err != nil {
		t.Fatal(err)
	}
	if on, _ := IsModuleEnabled(tenantID, "hr"); !on || RequireModules(tenantID, "hr") != nil || !inSchemas(t, schema, "hr") {
		t.Fatal("re-enable did not restore hr")
	}
	_ = db.DB.QueryRow(fmt.Sprintf(`SELECT status FROM %s.documents WHERE id = 'HR-KEEP-A8'`, schema)).Scan(&status)
	if status != "Pending Approval" {
		t.Fatalf("record after re-enable = %q", status)
	}
	// No privilege carried over to a module never sold: OMS-only client has no hr/procurement.
	if _, err := TransitionTenantPackages(tenantID, []string{"oms"}, "op", "r4", true); err != nil {
		t.Fatal(err)
	}
	for _, m := range []string{"hr", "procurement", "manufacturing", "crm_loyalty", "assets", "expenses", "wms", "pim"} {
		if on, _ := IsModuleEnabled(tenantID, m); on {
			t.Fatalf("%s carried over to an OMS-only client", m)
		}
	}
}

// fakeSaaSRazorpay is Razorpay's plans/subscriptions API as documented.
type fakeSaaSRazorpay struct {
	mu          sync.Mutex
	plans, subs int
	status      string // what GET /v1/subscriptions/:id reports
	lastSub     map[string]interface{}
}

func (f *fakeSaaSRazorpay) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	var in map[string]interface{}
	_ = json.NewDecoder(r.Body).Decode(&in)
	switch {
	case r.Method == "POST" && r.URL.Path == "/v1/plans":
		item, _ := in["item"].(map[string]interface{})
		if in["period"] != "monthly" || in["interval"] != 1.0 || item["currency"] != "INR" || item["amount"] == nil {
			w.WriteHeader(400)
			_, _ = w.Write([]byte(`{"error":{"description":"bad plan body"}}`))
			return
		}
		f.plans++
		_ = json.NewEncoder(w).Encode(map[string]interface{}{"id": fmt.Sprintf("plan_A8%d", f.plans), "entity": "plan"})
	case r.Method == "POST" && r.URL.Path == "/v1/subscriptions":
		if in["plan_id"] == nil || in["total_count"] == nil {
			w.WriteHeader(400)
			_, _ = w.Write([]byte(`{"error":{"description":"bad subscription body"}}`))
			return
		}
		f.subs++
		f.lastSub = in
		_ = json.NewEncoder(w).Encode(map[string]interface{}{"id": fmt.Sprintf("sub_A8%d", f.subs), "status": "created", "short_url": fmt.Sprintf("https://rzp.io/i/a8%d", f.subs)})
	case r.Method == "GET" && strings.HasPrefix(r.URL.Path, "/v1/subscriptions/"):
		_ = json.NewEncoder(w).Encode(map[string]interface{}{"id": strings.TrimPrefix(r.URL.Path, "/v1/subscriptions/"), "status": f.status})
	default:
		w.WriteHeader(404)
	}
}

func saasEventBody(event string, at int64, subID, tenantID, payID string, amount int64, errDesc string, start, end int64) []byte {
	pay := map[string]interface{}{}
	if payID != "" {
		pay = map[string]interface{}{"entity": map[string]interface{}{"id": payID, "amount": amount, "currency": "INR", "status": "captured", "error_description": errDesc}}
	}
	b, _ := json.Marshal(map[string]interface{}{
		"entity": "event", "event": event, "created_at": at,
		"payload": map[string]interface{}{
			"subscription": map[string]interface{}{"entity": map[string]interface{}{
				"id": subID, "plan_id": "plan_x", "status": "active", "current_start": start, "current_end": end,
				"notes": map[string]string{"tenant_id": tenantID}}},
			"payment": pay,
		},
	})
	return b
}

func TestBLD026SubscriptionLifecycle(t *testing.T) {
	tenantID, schema, _ := stage140Tenant(t)
	planKey := "a8plan" + tenantID
	saasCleanup(t, tenantID, planKey)
	ctx := context.Background()

	fake := &fakeSaaSRazorpay{status: "active"}
	server := httptest.NewServer(fake)
	defer server.Close()
	oldBase, oldPrivate, oldTenant, oldNow := razorpayAPIBase, outboundAllowPrivate, saasBillingTenantOverride, saasNow
	razorpayAPIBase, outboundAllowPrivate, saasBillingTenantOverride = server.URL, true, tenantID
	rehearsal := GetSettingBoolForSchema(schema, SettingRequireRehearsal)
	if err := SetSetting(tenantID, SettingRequireRehearsal, "false", "system"); err != nil {
		t.Fatal(err)
	}
	clock := time.Date(2026, 11, 1, 10, 0, 0, 0, istZone)
	saasNow = func() time.Time { return clock }
	t.Cleanup(func() {
		razorpayAPIBase, outboundAllowPrivate, saasBillingTenantOverride, saasNow = oldBase, oldPrivate, oldTenant, oldNow
		_ = SetSetting(tenantID, SettingRequireRehearsal, fmt.Sprint(rehearsal), "system")
		_, _ = db.DB.Exec("DELETE FROM "+schema+".channel_credentials WHERE channel_code = $1", RazorpayCredentialCode)
	})
	if err := SaveRazorpayCredential(tenantID, "rzp_test_a8", "sec", "whsec", "op", ""); err != nil {
		t.Fatal(err)
	}
	saasInvoiceSetup(t, tenantID, "29AAACE7520K1ZI", "999999")

	if err := UpsertSaaSPlan(SaaSPlan{PlanKey: planKey, DisplayName: "A8 Hr", PackageKeys: []string{"hr"}, AmountPaise: 118000}); err == nil {
		t.Fatal("a plan of an add-on alone was saved")
	}
	if err := UpsertSaaSPlan(SaaSPlan{PlanKey: planKey, DisplayName: "A8 PIM + HR", PackageKeys: []string{"pim", "hr"}, AmountPaise: 118000}); err != nil {
		t.Fatal(err)
	}

	// Start: one plan and one subscription at Razorpay, however many times it is asked.
	sub, err := StartSaaSSubscription(ctx, tenantID, planKey, "op")
	if err != nil || sub.State != SaaSStatePending || sub.RazorpayID != "sub_A8" + "1" || sub.ShortURL == "" {
		t.Fatalf("start = %+v %v", sub, err)
	}
	if fake.lastSub["customer_notify"] != true || fake.lastSub["total_count"] != 120.0 {
		t.Fatalf("subscription body = %v", fake.lastSub)
	}
	if again, err := StartSaaSSubscription(ctx, tenantID, planKey, "op"); err != nil || again.RazorpayID != sub.RazorpayID || fake.subs != 1 || fake.plans != 1 {
		t.Fatalf("asking again: %+v %v, plans %d subs %d; want one of each", again, err, fake.plans, fake.subs)
	}
	subID := sub.RazorpayID
	end := clock.AddDate(0, 1, 0).Unix()
	send := func(id, event string, at int64, pay string, amount int64, why string) string {
		t.Helper()
		res, err := HandleSaaSSubscriptionWebhook(id, saasEventBody(event, at, subID, tenantID, pay, amount, why, clock.Unix(), end))
		if err != nil {
			t.Fatalf("%s: %v", event, err)
		}
		return res
	}
	stateNow := func() *SaaSSubscription {
		s, err := GetSaaSSubscription(tenantID)
		if err != nil || s == nil {
			t.Fatalf("subscription: %v", err)
		}
		return s
	}
	invoices := func() (n int, gross, taxable, gst int64) {
		_ = db.DB.QueryRow(`SELECT count(*), COALESCE(sum(gross_paise),0), COALESCE(sum(taxable_paise),0), COALESCE(sum(gst_paise),0) FROM public.saas_invoices WHERE tenant_id = $1`, tenantID).Scan(&n, &gross, &taxable, &gst)
		return
	}
	status := func() string { l, _ := GetTenantLifecycle(tenantID); return l.Status }

	// An event for a subscription that is not ours is answered and left.
	if r, err := HandleSaaSSubscriptionWebhook("evt-foreign-"+tenantID, saasEventBody("subscription.activated", 50, "sub_not_ours", "nobody", "", 0, "", 0, 0)); err != nil || r != "ignored" {
		t.Fatalf("foreign = %q %v", r, err)
	}
	if r := send("evt-1", "subscription.authenticated", 100, "", 0, ""); r != "applied" || stateNow().State != SaaSStatePending {
		t.Fatalf("authenticated: %q state %s", r, stateNow().State)
	}
	send("evt-2", "subscription.activated", 200, "", 0, "")
	if s := stateNow(); s.State != SaaSStateActive || s.PeriodEnd == nil {
		t.Fatalf("activated: %+v", s)
	}
	if got, want := fmt.Sprint(saasSortedKeys(moduleSet(t, tenantID))), fmt.Sprint(saasSortedKeys(targetModules([]string{"pim", "hr"}))); got != want {
		t.Fatalf("modules after activation = %s; want the plan's %s", got, want)
	}

	// Charged: one GST invoice (118000 gross = 100000 + 18000 at 18%); a duplicate event and a replayed payment add nothing.
	if r := send("evt-3", "subscription.charged", 300, "pay_A8_1", 118000, ""); !strings.HasPrefix(r, "applied, invoice SA/26-27/") {
		t.Fatalf("charged = %q", r)
	}
	if r := send("evt-3", "subscription.charged", 300, "pay_A8_1", 118000, ""); r != "duplicate" {
		t.Fatalf("same event id = %q; want duplicate", r)
	}
	send("evt-3b", "subscription.charged", 300, "pay_A8_1", 118000, "") // Razorpay re-sent under a new event id
	if n, gross, taxable, gst := invoices(); n != 1 || gross != 118000 || taxable != 100000 || gst != 18000 {
		t.Fatalf("invoices = %d gross %d taxable %d gst %d; want 1 / 118000 / 100000 / 18000", n, gross, taxable, gst)
	}
	if _, err := HandleSaaSSubscriptionWebhook("evt-usd", saasEventBody("subscription.charged", 310, subID, tenantID, "pay_usd", 100, "", 0, 0)); err != nil {
		// INR payments only is exercised below with the explicit currency.
		t.Fatal(err)
	}
	bad := strings.Replace(string(saasEventBody("subscription.charged", 320, subID, tenantID, "pay_usd2", 100, "", 0, 0)), `"INR"`, `"USD"`, 1)
	if _, err := HandleSaaSSubscriptionWebhook("evt-usd2", []byte(bad)); err == nil {
		t.Fatal("a USD payment was invoiced")
	}

	// A failed payment: grace with full access, in plain words; then the late older "charged" arrives.
	send("evt-4", "subscription.pending", 500, "pay_A8_fail", 118000, "Card declined")
	s := stateNow()
	if s.State != SaaSStatePastDue || s.GraceUntil == nil || !strings.Contains(s.Explanation, "Card declined") || !strings.Contains(s.Explanation, "8 Nov 2026") || status() != "active" {
		t.Fatalf("pending: %+v tenant %s", s, status())
	}
	if r := send("evt-5", "subscription.charged", 400, "pay_A8_2", 118000, ""); !strings.HasPrefix(r, "stale") && r != "applied" {
		// the payment is real money: it is invoiced, but it must not undo the newer failure
		t.Fatalf("old charged = %q", r)
	}
	if stateNow().State != SaaSStatePastDue {
		t.Fatalf("an older charged event undid the newer failure: %s", stateNow().State)
	}
	if n, _, _, _ := invoices(); n != 3 { // pay_A8_1, pay_usd (valid INR 100), pay_A8_2
		t.Fatalf("invoices = %d; want 3 (the stale event's payment is still invoiced)", n)
	}

	// Grace not over: nothing moves. Grace over: suspended, data kept.
	clock = clock.AddDate(0, 0, 3)
	if n, err := EnforceSaaSBilling(); err != nil || n != 0 || status() != "active" {
		t.Fatalf("day 3 sweep moved %d (%v), tenant %s", n, err, status())
	}
	clock = clock.AddDate(0, 0, 5)
	if n, err := EnforceSaaSBilling(); err != nil || n != 1 {
		t.Fatalf("day 8 sweep moved %d (%v); want 1", n, err)
	}
	if s := stateNow(); s.State != SaaSStateSuspended || !s.BillingSuspend || status() != "suspended" || !strings.Contains(s.Explanation, "data is safe") {
		t.Fatalf("after grace: %+v tenant %s", s, status())
	}
	if n, _ := EnforceSaaSBilling(); n != 0 {
		t.Fatalf("a second sweep moved %d", n)
	}
	// Paying resumes it, and only because billing suspended it.
	send("evt-6", "subscription.charged", 600, "pay_A8_3", 118000, "")
	if s := stateNow(); s.State != SaaSStateActive || s.BillingSuspend || s.GraceUntil != nil || status() != "active" {
		t.Fatalf("after payment: %+v tenant %s", s, status())
	}
	// An operator's own suspension is not undone by a payment.
	if err := SuspendTenant(tenantID, "op", "legal review"); err != nil {
		t.Fatal(err)
	}
	send("evt-7", "subscription.charged", 700, "pay_A8_4", 118000, "")
	if status() != "suspended" {
		t.Fatalf("a payment lifted an operator suspension: %s", status())
	}
	if err := ResumeTenant(tenantID, "op", "cleared"); err != nil {
		t.Fatal(err)
	}

	// Reconciliation: a module switched on outside the plan is switched back.
	schemaMods, _, _ := enabledOptionalModules(db.DB, schema)
	if schemaMods["manufacturing"] {
		t.Fatal("setup: manufacturing should not be on")
	}
	if err := SetModuleEntitlement(tenantID, "manufacturing", true, "someone"); err != nil {
		t.Fatal(err)
	}
	drift, err := ReconcileSaaSSubscriptions(ctx, false)
	if err != nil {
		t.Fatal(err)
	}
	fixed := false
	for _, d := range drift {
		if d.TenantID == tenantID && d.Kind == "modules" && d.Fixed {
			fixed = true
		}
	}
	if !fixed || moduleSet(t, tenantID)["manufacturing"] {
		t.Fatalf("reconcile did not repair module drift: %+v", drift)
	}
	// Provider says halted, we say active: applied through the same event path, once.
	fake.status = "halted"
	drift, err = ReconcileSaaSSubscriptions(ctx, true)
	if err != nil {
		t.Fatal(err)
	}
	if s := stateNow(); s.State != SaaSStateSuspended || status() != "suspended" {
		t.Fatalf("after provider reconcile: %+v tenant %s drift %+v", s, status(), drift)
	}
	if again, _ := ReconcileSaaSSubscriptions(ctx, true); len(again) != 0 {
		t.Fatalf("a second reconcile found drift: %+v", again)
	}

	// Cancellation keeps access to the end of the paid period, then ends.
	fake.status = "active"
	send("evt-8", "subscription.charged", saasNow().Unix()+10, "pay_A8_5", 118000, "")
	cancelAt := saasNow().Unix() + 20
	send("evt-9", "subscription.cancelled", cancelAt, "", 0, "")
	if s := stateNow(); s.State != SaaSStateCancelled || s.AccessUntil == nil || status() != "active" {
		t.Fatalf("cancelled: %+v tenant %s", s, status())
	}
	clock = clock.AddDate(0, 2, 0)
	if n, err := EnforceSaaSBilling(); err != nil || n != 1 || stateNow().State != SaaSStateExpired || status() != "suspended" {
		t.Fatalf("end of paid period: moved %d (%v), state %s tenant %s", n, err, stateNow().State, status())
	}
}

func TestBLD027MeteringAndQuota(t *testing.T) {
	tenantID, schema, insert := stage140Tenant(t)
	planKey := "a8meter" + tenantID
	saasCleanup(t, tenantID, planKey)
	if err := UpsertSaaSPlan(SaaSPlan{PlanKey: planKey, DisplayName: "A8 metered", PackageKeys: []string{"oms"}, AmountPaise: 59000,
		Quotas: map[string]int64{"documents": 100, "users": 2}, OveragePaise: map[string]int64{"documents": 200}}); err != nil {
		t.Fatal(err)
	}
	if _, err := db.DB.Exec(`INSERT INTO public.saas_subscriptions (tenant_id, plan_key, state) VALUES ($1, $2, 'active')`, tenantID, planKey); err != nil {
		t.Fatal(err)
	}
	oldNow := saasNow
	t.Cleanup(func() { saasNow = oldNow })

	// Indian-time month edges: 23:59:59 IST on 30 Nov is November; the next second is December.
	nov := time.Date(2026, 11, 30, 23, 59, 59, 0, istZone)
	dec := time.Date(2026, 12, 1, 0, 0, 0, 0, istZone)
	oct := time.Date(2026, 10, 15, 12, 0, 0, 0, istZone)
	if SaaSUsagePeriod(nov) != "2026-11" || SaaSUsagePeriod(dec) != "2026-12" || SaaSUsagePeriod(nov.UTC()) != "2026-11" || SaaSUsagePeriod(dec.UTC()) != "2026-12" {
		t.Fatalf("period edges: %s %s %s %s", SaaSUsagePeriod(nov), SaaSUsagePeriod(dec), SaaSUsagePeriod(nov.UTC()), SaaSUsagePeriod(dec.UTC()))
	}
	// 18:29 UTC on 30 Nov is already... 23:59 IST (November); 18:30 UTC is 00:00 IST on 1 Dec.
	if SaaSUsagePeriod(time.Date(2026, 11, 30, 18, 29, 59, 0, time.UTC)) != "2026-11" || SaaSUsagePeriod(time.Date(2026, 11, 30, 18, 30, 0, 0, time.UTC)) != "2026-12" {
		t.Fatal("a UTC instant just after IST midnight was put in the wrong month")
	}

	// Three periods, replay safe.
	for i := 0; i < 2; i++ { // the second pass is a replayed batch
		for _, f := range []struct {
			key string
			at  time.Time
			qty int64
		}{{"o1", oct, 30}, {"n1", nov, 120}, {"d1", dec, 7}} {
			ok, err := RecordSaaSUsage(tenantID, "documents", f.key, f.qty, f.at)
			if err != nil || ok != (i == 0) {
				t.Fatalf("record %s pass %d = %v %v", f.key, i, ok, err)
			}
		}
	}
	// A correction (-10 to November) once; repeated id is ignored; a bad one is refused.
	if ok, err := CorrectSaaSUsage(tenantID, "documents", "fix-1", -10, nov, "duplicate import"); err != nil || !ok {
		t.Fatalf("correction = %v %v", ok, err)
	}
	if ok, _ := CorrectSaaSUsage(tenantID, "documents", "fix-1", -10, nov, "duplicate import"); ok {
		t.Fatal("a correction applied twice")
	}
	if _, err := CorrectSaaSUsage(tenantID, "documents", "fix-2", -1, nov, ""); err == nil {
		t.Fatal("a correction without a reason was accepted")
	}
	if _, err := RecordSaaSUsage(tenantID, "bogus", "k", 1, nov); err == nil {
		t.Fatal("an unknown meter was recorded")
	}
	used := func(period string) (int64, int64, int64) {
		st, err := SaaSUsageStatement(tenantID, period)
		if err != nil {
			t.Fatal(err)
		}
		for _, l := range st.Meters {
			if l.Meter == "documents" {
				return l.Used, l.OveragePaise, st.TotalPaise
			}
		}
		return -1, 0, 0
	}
	if u, o, tot := used("2026-10"); u != 30 || o != 0 || tot != 59000 {
		t.Fatalf("Oct = %d over %d total %d", u, o, tot)
	}
	// Nov: 120 - 10 = 110 used, 10 over quota 100 at 200 paise = 2000; total 59000 + 2000.
	if u, o, tot := used("2026-11"); u != 110 || o != 2000 || tot != 61000 {
		t.Fatalf("Nov = %d over %d total %d; want 110 / 2000 / 61000", u, o, tot)
	}
	if u, _, _ := used("2026-12"); u != 7 {
		t.Fatalf("Dec = %d", u)
	}

	// Quota decisions in the current (clock) month.
	saasNow = func() time.Time { return nov }
	if err := CheckSaaSQuota(tenantID, "documents", 5); err != nil {
		t.Fatalf("documents over quota but with an overage rate should pass: %v", err)
	}
	for i := 0; i < 2; i++ {
		_, _ = RecordSaaSUsage(tenantID, "users", fmt.Sprintf("u%d", i), 1, nov)
	}
	if err := CheckSaaSQuota(tenantID, "users", 0); err != nil {
		t.Fatalf("at the quota exactly is allowed: %v", err)
	}
	err := CheckSaaSQuota(tenantID, "users", 1)
	if v, ok := err.(*ValidationError); !ok || v.Code != "SAAS-0193" {
		t.Fatalf("a meter with no overage rate over quota = %v; want SAAS-0193", err)
	}
	if err := CheckSaaSQuota("no-such-tenant-a8", "users", 99); err != nil {
		t.Fatalf("a client with no subscription is unmetered: %v", err)
	}

	// Document snapshot: a re-run refreshes the day, it does not add to it.
	var day string
	_ = db.DB.QueryRow(`SELECT CURRENT_DATE::text`).Scan(&day)
	for i := 0; i < 3; i++ {
		insert(fmt.Sprintf("C-M%d", i), "Customer", "Active", "system", map[string]interface{}{"code": fmt.Sprintf("C-M%d", i), "name": "m"})
	}
	if err := SnapshotSaaSDocumentUsage(tenantID, day); err != nil {
		t.Fatal(err)
	}
	insert("C-M9", "Customer", "Active", "system", map[string]interface{}{"code": "C-M9", "name": "m"})
	if err := SnapshotSaaSDocumentUsage(tenantID, day); err != nil {
		t.Fatal(err)
	}
	var snap int64
	_ = db.DB.QueryRow(`SELECT COALESCE(sum(qty),0) FROM public.saas_usage WHERE tenant_id = $1 AND meter = 'documents' AND dedupe_key = $2`, tenantID, "snap:docs:"+day).Scan(&snap)
	var real int64
	_ = db.DB.QueryRow(fmt.Sprintf(`SELECT count(*) FROM %s.documents WHERE created_at >= public.erp_day_start($1::date) AND created_at < public.erp_day_start($1::date + 1)`, schema), day).Scan(&real)
	if snap != real || snap < 4 {
		t.Fatalf("snapshot = %d, documents made that day = %d; want equal and at least 4", snap, real)
	}

	// The daily pass also takes the users and storage gauges: one keyed row per
	// meter and month, overwritten by the latest reading; a past day is skipped.
	jan := time.Date(2027, 1, 10, 9, 0, 0, 0, istZone)
	saasNow = func() time.Time { return jan }
	var activeUsers int64
	_ = db.DB.QueryRow(fmt.Sprintf(`SELECT count(*) FROM %s.users WHERE COALESCE(status,'Active') = 'Active'`, schema)).Scan(&activeUsers)
	for i := 0; i < 2; i++ { // the second pass is a replay
		if n, err := SnapshotSaaSUsageAll(); err != nil || n < 1 {
			t.Fatalf("daily pass %d covered %d clients: %v", i, n, err)
		}
	}
	gauge := func(meter string) (rows, qty int64) {
		_ = db.DB.QueryRow(`SELECT count(*), COALESCE(sum(qty),0) FROM public.saas_usage WHERE tenant_id = $1 AND meter = $2 AND period_key = '2027-01'`, tenantID, meter).Scan(&rows, &qty)
		return
	}
	if r, q := gauge("users"); r != 1 || q != activeUsers {
		t.Fatalf("users gauge: %d rows, %d; want 1 row of %d", r, q, activeUsers)
	}
	if r, q := gauge("storage_mb"); r != 1 || q < 1 {
		t.Fatalf("storage gauge: %d rows, %d MB; want 1 row of at least 1", r, q)
	}
	if err := SnapshotSaaSGaugeUsage(tenantID, "2027-01-02"); err != nil {
		t.Fatal(err)
	}
	if r, _ := gauge("users"); r != 1 {
		t.Fatalf("a past day wrote a second gauge row: %d", r)
	}
	if st, err := SaaSUsageStatement(tenantID, "2027-01"); err != nil {
		t.Fatal(err)
	} else {
		for _, l := range st.Meters {
			if l.Meter == "users" && l.Used != activeUsers {
				t.Fatalf("statement users = %d; want %d", l.Used, activeUsers)
			}
		}
	}

	// Per-tenant costs list every subscribed client.
	costs, err := SaaSTenantCosts("2026-11")
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, c := range costs {
		if c.TenantID == tenantID && c.TotalPaise >= 61000 && c.BasePaise == 59000 {
			found = true
		}
	}
	if !found {
		t.Fatalf("tenant cost list lacks %s (base 59000, total at least 61000)", tenantID)
	}
}
