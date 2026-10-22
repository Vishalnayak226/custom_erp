package engines

import (
	"crypto/hmac"
	"crypto/sha256"
	"custom_erp/db"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"
)

// Stage 101 - the CRM platform, and 101.7.3's acceptance pack: the
// blueprint's cases that apply here, each in a client schema of its own so
// nothing touches shared data. Case names follow the blueprint (E04, M14-A01
// ...) where it has one.

type crmTestEnv struct {
	t        *testing.T
	tenantID string
	schema   string
	seq      int
}

func newCRMTestEnv(t *testing.T) *crmTestEnv {
	t.Helper()
	db.InitDB(testConnStr())
	tenantID, schema := uniqueLifecycleTenant(t)
	if _, err := ProvisionTenantSchema(tenantID, schema, "0.1.0-test"); err != nil {
		t.Fatalf("ProvisionTenantSchema: %v", err)
	}
	t.Cleanup(func() { dropLifecycleTenant(tenantID, schema) })
	if !crmReady(schema) {
		t.Skip("the Stage 101 migration is not applied to this database (crm_sends missing)")
	}
	e := &crmTestEnv{t: t, tenantID: tenantID, schema: schema}
	// no quiet hours, generous limits, unless a test says otherwise
	e.set("crm.quiet_start", "00:00")
	e.set("crm.quiet_end", "00:00")
	e.set("crm.limit_per_day", "10")
	e.set("crm.limit_per_week", "50")
	e.set("crm.min_gap_hours", "0")
	return e
}

func (e *crmTestEnv) set(key, value string) {
	e.t.Helper()
	if err := SetSetting(e.tenantID, key, value, "test"); err != nil {
		e.t.Fatalf("SetSetting %s: %v", key, err)
	}
}

func (e *crmTestEnv) exec(q string, args ...interface{}) {
	e.t.Helper()
	if _, err := db.DB.Exec(strings.ReplaceAll(q, "{s}", e.schema), args...); err != nil {
		e.t.Fatalf("exec %s: %v", q, err)
	}
}

func (e *crmTestEnv) count(q string, args ...interface{}) int {
	e.t.Helper()
	var n int
	if err := db.DB.QueryRow(strings.ReplaceAll(q, "{s}", e.schema), args...).Scan(&n); err != nil {
		e.t.Fatalf("count %s: %v", q, err)
	}
	return n
}

func (e *crmTestEnv) str(q string, args ...interface{}) string {
	e.t.Helper()
	var s string
	if err := db.DB.QueryRow(strings.ReplaceAll(q, "{s}", e.schema), args...).Scan(&s); err != nil {
		e.t.Fatalf("str %s: %v", q, err)
	}
	return s
}

// customer adds a customer with a unique Indian mobile; extra fields merge in.
func (e *crmTestEnv) customer(name string, extra map[string]interface{}) (id, phone string) {
	e.t.Helper()
	e.seq++
	id = fmt.Sprintf("CUS-T%d", e.seq)
	phone = fmt.Sprintf("98%08d", (time.Now().UnixNano()/1000+int64(e.seq*7919))%100000000)
	data := map[string]interface{}{"code": id, "name": name, "phone": phone, "email": strings.ToLower(strings.ReplaceAll(name, " ", ".")) + fmt.Sprintf("%d@example.com", e.seq)}
	for k, v := range extra {
		data[k] = v
	}
	e.exec(`INSERT INTO {s}.documents (id, doctype, data, status, created_by) VALUES ($1, 'Customer', $2, 'Active', 'test')`, id, crmJSON(data))
	return id, phone
}

func (e *crmTestEnv) e164(phone string) string { return "+91" + phone }

// bill adds a paid bill for a customer, daysAgo old.
func (e *crmTestEnv) bill(customerID string, amount float64, daysAgo int, lines int) string {
	e.t.Helper()
	e.seq++
	id := fmt.Sprintf("CART-T%d", e.seq)
	items := []map[string]interface{}{}
	for i := 0; i < lines; i++ {
		items = append(items, map[string]interface{}{"sku": "SKU-T", "qty": 1, "sale_price": amount / float64(lines)})
	}
	e.exec(`INSERT INTO {s}.documents (id, doctype, data, status, created_by, created_at) VALUES ($1, 'POSCart', $2, 'Paid', 'test', LOCALTIMESTAMP - make_interval(days => $3))`,
		id, crmJSON(map[string]interface{}{"customer_id": customerID, "amount_paid": amount, "items": items, "location": "ST-T", "bill_number": "B-" + id}), daysAgo)
	return id
}

func (e *crmTestEnv) grant(customerID, channel string) {
	e.t.Helper()
	if _, err := RecordCRMConsent(e.tenantID, CRMConsentInput{CustomerID: customerID, Channel: channel, Purpose: CRMPurposeMarketing, Action: CRMConsentGranted, Source: "form", RecordedBy: "test"}); err != nil {
		e.t.Fatalf("grant: %v", err)
	}
}

func (e *crmTestEnv) mockSender(channel, mode string) string {
	e.t.Helper()
	s, err := SaveCRMSender(e.tenantID, "", CRMSenderInput{Channel: channel, Provider: CRMProviderMock, Name: "Mock " + channel, Config: map[string]interface{}{"mode": mode}}, "test")
	if err != nil {
		e.t.Fatalf("SaveCRMSender: %v", err)
	}
	return s.ID
}

func (e *crmTestEnv) template(channel, purpose, body string) string {
	e.t.Helper()
	t, err := SaveCRMTemplate(e.tenantID, "", CRMTemplateInput{Name: "T " + body[:min(12, len(body))], Channel: channel, Purpose: purpose, Body: body, Subject: "Subject", DLTTemplateID: "1107000000000000001"}, "test")
	if err != nil {
		e.t.Fatalf("SaveCRMTemplate: %v", err)
	}
	return t.ID
}

func (e *crmTestEnv) allow(phones ...string) {
	e.set("crm.test_allowlist", strings.Join(phones, ","))
}

func (e *crmTestEnv) queue(r CRMSendRequest) string {
	e.t.Helper()
	id, _, err := crmQueueSend(db.DB, e.schema, r)
	if err != nil {
		e.t.Fatalf("queue: %v", err)
	}
	return id
}

func (e *crmTestEnv) sendStatus(id string) (string, string) {
	e.t.Helper()
	var st, reason string
	if err := db.DB.QueryRow(fmt.Sprintf(`SELECT status, COALESCE(reason_code, '') FROM %s.crm_sends WHERE id = $1`, e.schema), id).Scan(&st, &reason); err != nil {
		e.t.Fatalf("send %s: %v", id, err)
	}
	return st, reason
}

// --- 101.8 consent ------------------------------------------------------------------

// E04: a withdrawal while 1,000 messages are queued stops every one, an
// import saying "yes" cannot undo it, and only the customer can opt back in.
func TestCRMConsentWithdrawalWhileQueued(t *testing.T) {
	e := newCRMTestEnv(t)
	c, phone := e.customer("Asha Rao", map[string]interface{}{"consent_sms": true})
	states, err := CRMConsentFor(e.tenantID, c)
	if err != nil {
		t.Fatal(err)
	}
	for _, s := range states {
		if s.Channel == CRMChannelSMS && s.Purpose == CRMPurposeMarketing && (!s.Reachable || s.Source != "legacy") {
			t.Fatalf("a tick with no ledger row reads as a legacy grant: %+v", s)
		}
		if s.Channel == CRMChannelWhatsApp && s.Purpose == CRMPurposeMarketing && s.Reason != "NO_CONSENT" {
			t.Fatalf("no recorded consent is not reachable (D7): %+v", s)
		}
		if s.Purpose == CRMPurposeService && !s.Reachable {
			t.Fatalf("service messages are allowed unless refused (D6): %+v", s)
		}
	}
	e.grant(c, CRMChannelSMS)
	e.exec(`INSERT INTO {s}.crm_sends (id, run_kind, run_id, customer_id, channel, purpose) SELECT 'Q-' || g, 'campaign', 'RUN-E04-' || g, $1, 'SMS', 'marketing' FROM generate_series(1, 1000) g`, c)
	if _, err := RecordCRMConsent(e.tenantID, CRMConsentInput{CustomerID: c, Channel: CRMChannelSMS, Purpose: CRMPurposeMarketing, Action: CRMConsentWithdrawn, Source: "form", RecordedBy: "test"}); err != nil {
		t.Fatal(err)
	}
	if n := e.count(`SELECT COUNT(*) FROM {s}.crm_sends WHERE customer_id = $1 AND status = 'Cancelled' AND reason_code = 'CONSENT_WITHDRAWN'`, c); n != 1000 {
		t.Fatalf("withdrawal cancelled %d of 1000 queued messages", n)
	}
	if v := e.str(`SELECT data->>'consent_sms' FROM {s}.documents WHERE id = $1`, c); v != "false" {
		t.Fatalf("the tick follows the ledger, got %q", v)
	}
	// repeating the withdrawal records nothing more
	before := e.count(`SELECT COUNT(*) FROM {s}.crm_consent_ledger WHERE customer_id = $1`, c)
	_, _ = RecordCRMConsent(e.tenantID, CRMConsentInput{CustomerID: c, Channel: CRMChannelSMS, Purpose: CRMPurposeMarketing, Action: CRMConsentWithdrawn, Source: "stop_reply", RecordedBy: "customer"})
	if after := e.count(`SELECT COUNT(*) FROM {s}.crm_consent_ledger WHERE customer_id = $1`, c); after != before {
		t.Fatalf("a repeated withdrawal added %d row(s)", after-before)
	}
	// an import cannot re-grant
	if _, err := RecordCRMConsent(e.tenantID, CRMConsentInput{CustomerID: c, Channel: CRMChannelSMS, Purpose: CRMPurposeMarketing, Action: CRMConsentGranted, Source: "import"}); err == nil {
		t.Fatal("an import re-granted consent the customer withdrew")
	}
	row := map[string]interface{}{"id": c, "name": "Asha Rao", "consent_sms": true}
	if err := CRMCheckImportedCustomer(e.tenantID, false, row); err != nil || truthy(row["consent_sms"]) {
		t.Fatalf("an imported 'yes' must be dropped for a customer who said no: err=%v row=%v", err, row)
	}
	// the customer, on their own preference page, may opt back in
	token, err := crmIssuePrefToken(e.schema, c, "")
	if err != nil {
		t.Fatal(err)
	}
	yes := true
	ch := CRMPreferenceChange{}
	ch.Channels = append(ch.Channels, struct {
		Channel   string `json:"channel"`
		Marketing *bool  `json:"marketing,omitempty"`
		Service   *bool  `json:"service,omitempty"`
	}{Channel: CRMChannelSMS, Marketing: &yes})
	p, err := SetCRMPreferences(e.tenantID, token, ch)
	if err != nil {
		t.Fatal(err)
	}
	if p.FirstName != "Asha" || len(p.Channels) == 0 || !p.Channels[0].Marketing || strings.Contains(p.Channels[0].Contact, phone[2:8]) {
		t.Fatalf("preference page: %+v", p)
	}
	// STOP from the provider withdraws again
	if n, err := crmHandleStop(e.schema, CRMChannelSMS, e.e164(phone), "Replied STOP"); err != nil || n != 1 {
		t.Fatalf("STOP: n=%d err=%v", n, err)
	}
	states, _ = CRMConsentFor(e.tenantID, c)
	for _, s := range states {
		if s.Channel == CRMChannelSMS && s.Purpose == CRMPurposeMarketing && s.Reachable {
			t.Fatal("STOP did not withdraw SMS marketing")
		}
	}
	// stop everything blocks every channel, service too
	_, _ = RecordCRMConsent(e.tenantID, CRMConsentInput{CustomerID: c, Channel: "All", Purpose: "all", Action: CRMConsentWithdrawn, Source: "preference_page", RecordedBy: "customer"})
	states, _ = CRMConsentFor(e.tenantID, c)
	for _, s := range states {
		if s.Reachable || s.Reason != "GLOBAL_OPT_OUT" {
			t.Fatalf("stop everything: %+v", s)
		}
	}
}

// --- 101.9 the send gate -----------------------------------------------------------

func (e *crmTestEnv) reachableCustomer(name, channel string) (string, string) {
	c, phone := e.customer(name, nil)
	e.grant(c, channel)
	return c, phone
}

// M14-A01: two workers cannot both take a customer's last contact slot.
func TestCRMSendGateCapRace(t *testing.T) {
	e := newCRMTestEnv(t)
	e.mockSender(CRMChannelSMS, "accept")
	tpl := e.template(CRMChannelSMS, CRMPurposeMarketing, "Hi {{customer.first_name|there}}, new arrivals are in.")
	c, phone := e.reachableCustomer("Ravi Kumar", CRMChannelSMS)
	e.allow(e.e164(phone))
	e.set("crm.limit_per_day", "1")
	a := e.queue(CRMSendRequest{RunKind: "campaign", RunID: "RUN-A", CustomerID: c, Channel: CRMChannelSMS, Purpose: CRMPurposeMarketing, TemplateID: tpl})
	b := e.queue(CRMSendRequest{RunKind: "campaign", RunID: "RUN-B", CustomerID: c, Channel: CRMChannelSMS, Purpose: CRMPurposeMarketing, TemplateID: tpl})
	var wg sync.WaitGroup
	for _, id := range []string{a, b} {
		wg.Add(1)
		go func(id string) {
			defer wg.Done()
			if _, err := crmDispatchNext(e.schema, id); err != nil {
				t.Errorf("dispatch %s: %v", id, err)
			}
		}(id)
	}
	wg.Wait()
	sa, ra := e.sendStatus(a)
	sb, rb := e.sendStatus(b)
	sent := 0
	for _, s := range []string{sa, sb} {
		if s == crmSendAccepted {
			sent++
		}
	}
	if sent != 1 || (ra != "CAP_REACHED" && rb != "CAP_REACHED") {
		t.Fatalf("cap race: %s/%s and %s/%s - exactly one may go", sa, ra, sb, rb)
	}
	// the unique key: the same run cannot queue the customer twice
	again, created, _ := crmQueueSend(db.DB, e.schema, CRMSendRequest{RunKind: "campaign", RunID: "RUN-A", CustomerID: c, Channel: CRMChannelSMS, Purpose: CRMPurposeMarketing, TemplateID: tpl})
	if created || again != a {
		t.Fatalf("a second send for the same run x customer x channel was created")
	}
}

// M14-A02: a provider timeout after sending is Unknown, never resent, and the
// later callback settles it.
func TestCRMSendGateTimeoutThenCallback(t *testing.T) {
	e := newCRMTestEnv(t)
	e.mockSender(CRMChannelWhatsApp, "accept")
	tpl := e.template(CRMChannelWhatsApp, CRMPurposeService, "Your alteration is ready, {{customer.first_name|there}}.")
	c, phone := e.customer("Meena Iyer", nil)
	e.allow(e.e164(phone))
	calls := 0
	crmMockHook = func(d crmDispatch) (crmProviderResult, error) {
		calls++
		return crmProviderResult{}, &errCRMUnknown{msg: "timed out"}
	}
	defer func() { crmMockHook = nil }()
	id := e.queue(CRMSendRequest{RunKind: "staff", RunID: "MSG-1", CustomerID: c, Channel: CRMChannelWhatsApp, Purpose: CRMPurposeService, TemplateID: tpl})
	RunCRMTickForSchema(e.schema)
	if st, _ := e.sendStatus(id); st != crmSendUnknown {
		t.Fatalf("after a timeout the message is %s, want Unknown", st)
	}
	RunCRMTickForSchema(e.schema)
	if calls != 1 {
		t.Fatalf("an unknown message was handed to the provider %d times", calls)
	}
	if err := crmApplyStatusCallback(e.schema, "wamid.LATE", id, "delivered", "", nil, "whatsapp"); err != nil {
		t.Fatal(err)
	}
	if st, _ := e.sendStatus(id); st != crmSendDelivered {
		t.Fatalf("the late callback left it %s", st)
	}
	_ = crmApplyStatusCallback(e.schema, "wamid.LATE", "", "read", "", nil, "whatsapp")
	_ = crmApplyStatusCallback(e.schema, "wamid.LATE", "", "sent", "", nil, "whatsapp")
	if st, _ := e.sendStatus(id); st != crmSendRead {
		t.Fatalf("status went backwards or did not move to Read: %s", st)
	}
}

// M14-A03: a quiet-hours wait that would pass the message's end expires it;
// otherwise it waits.
func TestCRMSendGateQuietHoursPastExpiry(t *testing.T) {
	e := newCRMTestEnv(t)
	e.mockSender(CRMChannelSMS, "accept")
	tpl := e.template(CRMChannelSMS, CRMPurposeMarketing, "Sale ends tonight.")
	c, phone := e.reachableCustomer("Kiran Shah", CRMChannelSMS)
	e.allow(e.e164(phone))
	now := time.Now().In(crmLocation(e.schema))
	e.set("crm.quiet_start", now.Add(-time.Hour).Format("15:04"))
	e.set("crm.quiet_end", now.Add(3*time.Hour).Format("15:04"))
	soon := e.queue(CRMSendRequest{RunKind: "campaign", RunID: "RUN-Q1", CustomerID: c, Channel: CRMChannelSMS, Purpose: CRMPurposeMarketing, TemplateID: tpl, ExpiresInHours: 1})
	later := e.queue(CRMSendRequest{RunKind: "campaign", RunID: "RUN-Q2", CustomerID: c, Channel: CRMChannelSMS, Purpose: CRMPurposeMarketing, TemplateID: tpl, ExpiresInHours: 12})
	service := e.queue(CRMSendRequest{RunKind: "reminder", RunID: "APT-Q", CustomerID: c, Channel: CRMChannelSMS, Purpose: CRMPurposeService, TemplateID: tpl})
	for _, id := range []string{soon, later, service} {
		_, _ = crmDispatchNext(e.schema, id)
	}
	if st, r := e.sendStatus(soon); st != crmSendExpired || r != "QUIET_HOURS_EXPIRED" {
		t.Fatalf("quiet hours past expiry: %s %s", st, r)
	}
	if st, r := e.sendStatus(later); st != crmSendDeferred || r != "QUIET_HOURS" {
		t.Fatalf("quiet hours: %s %s, want Deferred", st, r)
	}
	if st, _ := e.sendStatus(service); st != crmSendAccepted {
		t.Fatalf("a service message is not held by quiet hours: %s", st)
	}
}

// 101.9.4 + 101.3.4: a server that is not production reaches only the
// allowlist; a blank required value suppresses rather than sends a gap.
func TestCRMSendGateAllowlistAndMissingData(t *testing.T) {
	e := newCRMTestEnv(t)
	e.mockSender(CRMChannelSMS, "accept")
	c, _ := e.reachableCustomer("Lata Menon", CRMChannelSMS)
	ok := e.template(CRMChannelSMS, CRMPurposeMarketing, "Hello from the store.")
	gap := e.template(CRMChannelSMS, CRMPurposeMarketing, "Your bill {{bill.number}} is ready.")
	e.allow("+919000000001")
	a := e.queue(CRMSendRequest{RunKind: "campaign", RunID: "RUN-AL", CustomerID: c, Channel: CRMChannelSMS, Purpose: CRMPurposeMarketing, TemplateID: ok})
	_, _ = crmDispatchNext(e.schema, a)
	if st, r := e.sendStatus(a); st != crmSendSuppress || r != "NOT_ALLOWLISTED" {
		t.Fatalf("a contact not on the allowlist: %s %s", st, r)
	}
	b := e.queue(CRMSendRequest{RunKind: "campaign", RunID: "RUN-GAP", CustomerID: c, Channel: CRMChannelSMS, Purpose: CRMPurposeMarketing, TemplateID: gap})
	_, _ = crmDispatchNext(e.schema, b)
	if st, r := e.sendStatus(b); st != crmSendSuppress || r != "TEMPLATE_DATA_MISSING" {
		t.Fatalf("missing value: %s %s", st, r)
	}
	if parts, unicode := crmSMSParts(strings.Repeat("a", 161)); parts != 2 || unicode {
		t.Fatalf("161 GSM characters are 2 parts, got %d", parts)
	}
	if parts, unicode := crmSMSParts("नमस्ते " + strings.Repeat("a", 64)); parts != 2 || !unicode {
		t.Fatalf("71 characters with Hindi are 2 unicode parts, got %d %v", parts, unicode)
	}
}

// --- 101.4 journeys -------------------------------------------------------------------

// M13-A01: a restart during a 24-hour wait resumes it and sends once.
func TestCRMJourneyRestartDuringWait(t *testing.T) {
	e := newCRMTestEnv(t)
	tpl := e.template(CRMChannelWhatsApp, CRMPurposeService, "Thanks for joining, {{customer.first_name|there}}.")
	j, err := SaveCRMJourney(e.tenantID, "", "Welcome test", "", CRMJourneyDefinition{Trigger: CRMJourneyTrigger{Type: "event", Event: CRMEventCustomerCreated},
		Steps: []CRMJourneyStep{{Type: "wait", Hours: 24}, {Type: "send", Channel: CRMChannelWhatsApp, TemplateID: tpl, Purpose: CRMPurposeService}}}, "test")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := SetCRMJourneyEnabled(e.tenantID, j.ID, true, false, "test"); err == nil {
		t.Fatal("an unpublished journey was switched on")
	}
	if _, err := PublishCRMJourney(e.tenantID, j.ID, "test"); err != nil {
		t.Fatal(err)
	}
	if _, err := SetCRMJourneyEnabled(e.tenantID, j.ID, true, false, "test"); err != nil {
		t.Fatal(err)
	}
	c, _ := e.customer("Nisha Verma", nil)
	if err := RecordCRMEvent(e.schema, c, CRMEventCustomerCreated, c, nil); err != nil {
		t.Fatal(err)
	}
	_ = RecordCRMEvent(e.schema, c, CRMEventCustomerCreated, c, nil) // a duplicate event is one event
	crmProcessEvents(e.schema)
	crmAdvanceJourneys(e.schema)
	if n := e.count(`SELECT COUNT(*) FROM {s}.crm_journey_runs WHERE journey_id = $1 AND customer_id = $2 AND state = 'Waiting'`, j.ID, c); n != 1 {
		t.Fatalf("%d runs waiting, want 1", n)
	}
	// 24 hours pass; the "restarted" server advances the same run twice at once
	e.exec(`UPDATE {s}.crm_journey_runs SET wait_until = LOCALTIMESTAMP - interval '1 minute' WHERE journey_id = $1`, j.ID)
	runID := e.str(`SELECT id FROM {s}.crm_journey_runs WHERE journey_id = $1`, j.ID)
	var wg sync.WaitGroup
	for i := 0; i < 3; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); _ = crmAdvanceRun(e.schema, runID) }()
	}
	wg.Wait()
	_ = crmAdvanceRun(e.schema, runID)
	if n := e.count(`SELECT COUNT(*) FROM {s}.crm_sends WHERE run_kind = 'journey' AND run_id LIKE $1 || ':%'`, runID); n != 1 {
		t.Fatalf("the journey queued %d messages, want exactly 1", n)
	}
	if st := e.str(`SELECT state FROM {s}.crm_journey_runs WHERE id = $1`, runID); st != "Completed" {
		t.Fatalf("run is %s, want Completed", st)
	}
	// a published version is frozen: editing the draft leaves the run's version alone
	if _, err := SaveCRMJourney(e.tenantID, j.ID, "Welcome test", "", CRMJourneyDefinition{Trigger: CRMJourneyTrigger{Type: "event", Event: CRMEventCustomerCreated},
		Steps: []CRMJourneyStep{{Type: "send", Channel: CRMChannelWhatsApp, TemplateID: tpl, Purpose: CRMPurposeService}}}, "test"); err != nil {
		t.Fatal(err)
	}
	if got, _ := GetCRMJourney(e.tenantID, j.ID); got.CurrentVersion != 1 || !got.Unpublished {
		t.Fatalf("a draft edit must not change the published version: %+v", got)
	}
}

// The blueprint's cart fixture: stale events do not start a run, a purchase
// of the same cart ends it, another cart's purchase does not.
func TestCRMJourneyCartRecovery(t *testing.T) {
	e := newCRMTestEnv(t)
	tpl := e.template(CRMChannelWhatsApp, CRMPurposeMarketing, "Your cart is waiting.")
	j, _ := SaveCRMJourney(e.tenantID, "", "Cart", "", CRMJourneyDefinition{Trigger: CRMJourneyTrigger{Type: "event", Event: CRMEventCartAbandoned, StaleHours: 24}, PerCart: true,
		Goal: &CRMJourneyGoal{Event: CRMEventCartPurchased}, CooldownDays: 1,
		Steps: []CRMJourneyStep{{Type: "wait", Hours: 1}, {Type: "send", Channel: CRMChannelWhatsApp, TemplateID: tpl, Purpose: CRMPurposeMarketing}}}, "test")
	if _, err := PublishCRMJourney(e.tenantID, j.ID, "test"); err != nil {
		t.Fatal(err)
	}
	_, _ = SetCRMJourneyEnabled(e.tenantID, j.ID, true, false, "test")
	c, _ := e.customer("Arjun Das", nil)
	e.exec(`INSERT INTO {s}.crm_events (customer_id, event_type, ref_id, dedupe_key, occurred_at) VALUES ($1, 'cart_abandoned', 'CART-OLD', 'cart_abandoned:CART-OLD', LOCALTIMESTAMP - interval '30 hours')`, c)
	_ = RecordCRMEvent(e.schema, c, CRMEventCartAbandoned, "CART-1", nil)
	_ = RecordCRMEvent(e.schema, c, CRMEventCartAbandoned, "CART-2", nil)
	crmProcessEvents(e.schema)
	if n := e.count(`SELECT COUNT(*) FROM {s}.crm_journey_runs WHERE journey_id = $1`, j.ID); n != 2 {
		t.Fatalf("%d cart runs, want 2 (the stale one held)", n)
	}
	_ = RecordCRMEvent(e.schema, c, CRMEventCartPurchased, "CART-1", nil)
	crmProcessEvents(e.schema)
	if st := e.str(`SELECT state || ':' || COALESCE(exit_reason, '') FROM {s}.crm_journey_runs WHERE journey_id = $1 AND cart_key = 'CART-1'`, j.ID); st != "Exited:goal" {
		t.Fatalf("buying the cart ends its run, got %s", st)
	}
	if st := e.str(`SELECT state FROM {s}.crm_journey_runs WHERE journey_id = $1 AND cart_key = 'CART-2'`, j.ID); st != "Active" {
		t.Fatalf("another cart's run must continue, got %s", st)
	}
}

// --- 101.11 loyalty and offers -----------------------------------------------------

// M27-A01: two simultaneous redemptions of 80 points from 100 give one success.
func TestCRMLoyaltyRedemptionRace(t *testing.T) {
	e := newCRMTestEnv(t)
	e.set("loyalty.redemption_requires_otp", "false")
	c, _ := e.customer("Farah Khan", nil)
	e.exec(`INSERT INTO {s}.loyalty_point_ledger (customer_id, transaction_type, points, reference_doctype, reference_id) VALUES ($1, 'Earn', 100, 'POSCart', 'SEED')`, c)
	var wg sync.WaitGroup
	var mu sync.Mutex
	ok := 0
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			if _, err := RedeemLoyaltyPoints(e.tenantID, c, 80, fmt.Sprintf("CART-R%d", i)); err == nil {
				mu.Lock()
				ok++
				mu.Unlock()
			}
		}(i)
	}
	wg.Wait()
	bal, _ := GetLoyaltyBalance(e.tenantID, c)
	if ok != 1 || bal != 20 {
		t.Fatalf("%d redemptions succeeded, balance %d - want 1 and 20", ok, bal)
	}
}

// M28-A02: two simultaneous uses of one code give one; a code held by one
// checkout cannot be taken by another.
func TestCRMVoucherOneUseRace(t *testing.T) {
	e := newCRMTestEnv(t)
	c, _ := e.customer("Gita Pillai", nil)
	code, _ := crmCode("V", 10)
	e.exec(`INSERT INTO {s}.documents (id, doctype, data, status, created_by) VALUES ($1, 'Voucher', $2, 'Active', 'test')`, "VCH-T1",
		crmJSON(map[string]interface{}{"code": code, "discount_type": "Flat", "discount_value": 100, "expiry_date": time.Now().AddDate(0, 1, 0).Format("2006-01-02"), "max_uses": 1, "used_count": 0, "status": "Active"}))
	var wg sync.WaitGroup
	var mu sync.Mutex
	ok := 0
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			if _, err := RedeemVoucher(e.tenantID, code, c, fmt.Sprintf("CART-V%d", i), 1000); err == nil {
				mu.Lock()
				ok++
				mu.Unlock()
			}
		}(i)
	}
	wg.Wait()
	if ok != 1 {
		t.Fatalf("%d uses of a single-use code succeeded", ok)
	}
	code2, _ := crmCode("V", 10)
	e.exec(`INSERT INTO {s}.documents (id, doctype, data, status, created_by) VALUES ($1, 'Voucher', $2, 'Active', 'test')`, "VCH-T2",
		crmJSON(map[string]interface{}{"code": code2, "discount_type": "Flat", "discount_value": 100, "expiry_date": time.Now().AddDate(0, 1, 0).Format("2006-01-02"), "max_uses": 1, "used_count": 0, "status": "Active"}))
	if _, err := ReserveCRMVoucher(e.tenantID, code2, VoucherChannelOnline, "SHOP-CHECKOUT-1", c); err != nil {
		t.Fatal(err)
	}
	if _, err := ReserveCRMVoucher(e.tenantID, code2, VoucherChannelPOS, "CART-OTHER", c); err == nil {
		t.Fatal("a second checkout reserved a code the web shop holds")
	}
	if _, err := RedeemVoucher(e.tenantID, code2, c, "CART-OTHER", 1000); err == nil {
		t.Fatal("the till took a code the web shop holds")
	}
	if _, err := RedeemVoucher(e.tenantID, code2, c, "SHOP-CHECKOUT-1", 1000); err != nil {
		t.Fatalf("the holder could not use its code: %v", err)
	}
	if n := e.count(`SELECT COUNT(*) FROM {s}.crm_voucher_reservations WHERE code = $1 AND status = 'Captured'`, code2); n != 1 {
		t.Fatalf("the hold was not captured")
	}
}

// 101.11.2 with Shopify: a code typed at a web-shop checkout is held from the
// till; the paid order uses it; a code the till used first is raised for a
// person rather than dropped.
func TestCRMShopifyCheckoutHoldsVoucher(t *testing.T) {
	e := newCRMTestEnv(t)
	c, phone := e.customer("Hema Rao", nil)
	voucher := func(id string) string {
		code, _ := crmCode("V", 10)
		e.exec(`INSERT INTO {s}.documents (id, doctype, data, status, created_by) VALUES ($1, 'Voucher', $2, 'Active', 'test')`, id,
			crmJSON(map[string]interface{}{"code": code, "discount_type": "Flat", "discount_value": 100, "expiry_date": time.Now().AddDate(0, 1, 0).Format("2006-01-02"), "max_uses": 1, "used_count": 0, "status": "Active"}))
		return code
	}
	code := voucher("VCH-S1")
	body, _ := json.Marshal(map[string]interface{}{"token": "CHK-S1", "phone": phone, "discount_codes": []map[string]string{{"code": code}, {"code": "SHOPONLY10"}}})
	if _, err := handleShopifyCheckout(e.tenantID, body); err != nil {
		t.Fatal(err)
	}
	if _, err := RedeemVoucher(e.tenantID, code, c, "CART-TILL", 1000); err == nil {
		t.Fatal("the till took a code an open web-shop checkout holds")
	}
	if p := RedeemCRMVoucherForChannelOrder(e.tenantID, code, c, "CHK-S1", "SO-S1"); p != "" {
		t.Fatalf("the paid order could not use its own held code: %s", p)
	}
	if n := e.count(`SELECT COUNT(*) FROM {s}.crm_voucher_reservations WHERE code = $1 AND ref_id = 'CHK-S1' AND status = 'Captured'`, code); n != 1 {
		t.Fatal("the checkout's hold was not captured")
	}
	if p := RedeemCRMVoucherForChannelOrder(e.tenantID, "SHOPONLY10", c, "CHK-S1", "SO-S1"); p != "" {
		t.Fatalf("a code made in the shop was treated as an ERP voucher: %s", p)
	}
	used := voucher("VCH-S2")
	if _, err := RedeemVoucher(e.tenantID, used, c, "CART-TILL-2", 1000); err != nil {
		t.Fatal(err)
	}
	if p := RedeemCRMVoucherForChannelOrder(e.tenantID, used, c, "CHK-S2", "SO-S2"); p == "" || !strings.Contains(p, strings.ToUpper(used)) {
		t.Fatalf("a code the till used first was not raised: %q", p)
	}
}

// 101.5.3: a WhatsApp reply comes in through the signed callback and lands in
// one conversation for the customer (a repeat delivery counted once); a
// forged signature is refused; a reply is never consent; STOP withdraws;
// take-over and hand-back switch the thread between a person and auto.
func TestCRMWhatsAppInbox(t *testing.T) {
	e := newCRMTestEnv(t)
	c, phone := e.customer("Isha Menon", nil)
	s, err := SaveCRMSender(e.tenantID, "", CRMSenderInput{Channel: CRMChannelWhatsApp, Provider: CRMProviderWhatsApp, Name: "WA", Status: "Active",
		Config: map[string]interface{}{"phone_number_id": "123"}, Secrets: map[string]string{"access_token": "t", "app_secret": "shh"}}, "test")
	if err != nil {
		t.Fatal(err)
	}
	u, _ := url.Parse(s.CallbackURL)
	key := u.Query().Get("k")
	msg := func(id, text string) []byte {
		b, _ := json.Marshal(map[string]interface{}{"entry": []interface{}{map[string]interface{}{"changes": []interface{}{map[string]interface{}{"field": "messages",
			"value": map[string]interface{}{"messages": []interface{}{map[string]interface{}{"from": "91" + phone, "id": id, "type": "text", "text": map[string]string{"body": text}}}}}}}}})
		return b
	}
	sign := func(b []byte) string {
		mac := hmac.New(sha256.New, []byte("shh"))
		mac.Write(b)
		return "sha256=" + hex.EncodeToString(mac.Sum(nil))
	}
	b := msg("wamid.1", "Is the blue kurta back in my size?")
	if _, err := HandleCRMWhatsAppWebhook(e.tenantID, key, b, "sha256=00"); err == nil {
		t.Fatal("a forged signature was accepted")
	}
	for i := 0; i < 2; i++ {
		if _, err := HandleCRMWhatsAppWebhook(e.tenantID, key, b, sign(b)); err != nil {
			t.Fatal(err)
		}
	}
	convs, err := ListCRMConversations(e.tenantID, "", "")
	if err != nil || len(convs) != 1 || convs[0].Unread != 1 || convs[0].CustomerID != c {
		t.Fatalf("want one conversation for %s with one unread, got %+v (%v)", c, convs, err)
	}
	if n := e.count(`SELECT COUNT(*) FROM {s}.crm_consent_ledger WHERE customer_id = $1 AND action = 'granted'`, c); n != 0 {
		t.Fatal("a reply was taken as consent")
	}
	stop := msg("wamid.2", "STOP")
	if _, err := HandleCRMWhatsAppWebhook(e.tenantID, key, stop, sign(stop)); err != nil {
		t.Fatal(err)
	}
	if n := e.count(`SELECT COUNT(*) FROM {s}.crm_consent_ledger WHERE customer_id = $1 AND action = 'withdrawn'`, c); n == 0 {
		t.Fatal("STOP did not withdraw consent")
	}
	id := convs[0].ID
	if err := SetCRMConversation(e.tenantID, id, "take_over", "", "agent1"); err != nil {
		t.Fatal(err)
	}
	if m := e.str(`SELECT mode FROM {s}.crm_conversations WHERE id = $1`, id); m != "human" {
		t.Fatalf("take over left mode %q", m)
	}
	if err := SetCRMConversation(e.tenantID, id, "hand_back", "", "agent1"); err != nil {
		t.Fatal(err)
	}
	if m := e.str(`SELECT mode FROM {s}.crm_conversations WHERE id = $1`, id); m != "auto" {
		t.Fatalf("hand back left mode %q", m)
	}
}

// 101.11.1: pending points cannot be used until the return window passes; a
// return inside it cancels them out.
func TestCRMPendingPoints(t *testing.T) {
	e := newCRMTestEnv(t)
	e.set("loyalty.pending_points", "true")
	e.set("sales.return_window_days", "15")
	e.set("loyalty.rupees_per_point", "100")
	c, _ := e.customer("Hari Nair", nil)
	if err := EarnLoyaltyPoints(e.tenantID, c, 5000, "CART-P1"); err != nil {
		t.Fatal(err)
	}
	if bal, _ := GetLoyaltyBalance(e.tenantID, c); bal != 0 {
		t.Fatalf("pending points counted at once: %d", bal)
	}
	if f, err := crmCustomerFacts(e.schema, c); err != nil || f.PointsPending <= 0 {
		t.Fatalf("pending points not shown: %+v %v", f, err)
	}
	e.exec(`UPDATE {s}.loyalty_point_ledger SET pending_until = NOW() - interval '1 minute' WHERE customer_id = $1`, c)
	if bal, _ := GetLoyaltyBalance(e.tenantID, c); bal <= 0 {
		t.Fatalf("points did not become usable after the window: %d", bal)
	}
}

// 101.11.3: a referral pays once the friend's first bill passes the window;
// a self-referral is flagged.
func TestCRMReferral(t *testing.T) {
	e := newCRMTestEnv(t)
	e.set("crm.referral_reward_points", "50")
	ref, _ := e.customer("Isha Bose", nil)
	friend, _ := e.customer("Jai Bose", nil)
	code, err := CRMReferralCode(e.tenantID, ref)
	if err != nil {
		t.Fatal(err)
	}
	if again, _ := CRMReferralCode(e.tenantID, ref); again != code {
		t.Fatal("a customer has one referral code")
	}
	r, err := RecordCRMReferral(e.tenantID, friend, code, "test")
	if err != nil || r.Status != "Waiting" {
		t.Fatalf("referral: %+v %v", r, err)
	}
	bill := e.bill(friend, 1200, 0, 1)
	crmReferralOnBill(e.schema, crmEvent{CustomerID: friend, RefID: bill})
	e.exec(`UPDATE {s}.crm_referrals SET qualifies_on = CURRENT_DATE - 1 WHERE id = $1`, r.ID)
	crmReleaseReferrals(e.schema)
	crmReleaseReferrals(e.schema)
	if n := e.count(`SELECT COALESCE(SUM(points), 0) FROM {s}.loyalty_point_ledger WHERE customer_id = $1 AND reference_doctype = 'CRMReferral'`, ref); n != 50 {
		t.Fatalf("referrer got %d points, want 50 once", n)
	}
	self, err := RecordCRMReferral(e.tenantID, ref, code, "test")
	if err == nil && self.Status != "Flagged" {
		t.Fatalf("a self-referral must be flagged: %+v", self)
	}
}

// --- 101.8.3 erasure and other clients -------------------------------------------------

// The blueprint's erasure-then-re-import case: queued messages and journeys
// stop first; the phone and email cannot come back.
func TestCRMErasureThenReimport(t *testing.T) {
	e := newCRMTestEnv(t)
	c, phone := e.reachableCustomer("Kavya Rao", CRMChannelSMS)
	email := e.str(`SELECT data->>'email' FROM {s}.documents WHERE id = $1`, c)
	q := e.queue(CRMSendRequest{RunKind: "campaign", RunID: "RUN-ER", CustomerID: c, Channel: CRMChannelSMS, Purpose: CRMPurposeMarketing, Body: "x"})
	e.exec(`INSERT INTO {s}.crm_journey_runs (id, journey_id, version, customer_id, state) VALUES ('JR-ER', 'J-ER', 1, $1, 'Waiting')`, c)
	if err := AnonymizeCustomer(e.tenantID, c, "test", "customer asked"); err != nil {
		t.Fatal(err)
	}
	if st, r := e.sendStatus(q); st != crmSendCancelled || r != "ERASED" {
		t.Fatalf("queued message after erasure: %s %s", st, r)
	}
	if st := e.str(`SELECT state FROM {s}.crm_journey_runs WHERE id = 'JR-ER'`); st != "Cancelled" {
		t.Fatalf("live journey after erasure: %s", st)
	}
	if n := e.count(`SELECT COUNT(*) FROM {s}.crm_consent_ledger WHERE customer_id = $1`, c); n != 0 {
		t.Fatalf("%d consent rows survived erasure", n)
	}
	for _, contact := range [][2]string{{phone, ""}, {"+91" + phone, ""}, {"", email}} {
		if blocked, err := CRMErasedContactBlocked(e.tenantID, contact[0], contact[1]); err != nil || !blocked {
			t.Fatalf("erased contact %v not blocked (err %v)", contact, err)
		}
	}
	if err := CRMCheckImportedCustomer(e.tenantID, true, map[string]interface{}{"name": "Kavya Rao", "phone": phone, "consent_sms": true}); err == nil {
		t.Fatal("an import re-created an erased customer")
	}
	if _, err := PrepareCRMCustomerSave(e.tenantID, "Customer", "Cashier", true, nil, map[string]interface{}{"name": "K", "email": email}, "test", "till"); err == nil {
		t.Fatal("the till re-created an erased customer")
	}
}

// Another client's customer is never readable.
func TestCRMOtherClientAccessRefused(t *testing.T) {
	a := newCRMTestEnv(t)
	b := newCRMTestEnv(t)
	c, _ := a.customer("Leela Joshi", nil)
	if _, err := GetCRMCustomerPage(b.tenantID, "Admin", c); err == nil {
		t.Fatal("client B read client A's customer")
	}
	list, err := SearchCRMCustomers(b.tenantID, "Admin", "Leela", "", 1, 50)
	if err != nil {
		t.Fatal(err)
	}
	if list.Total != 0 {
		t.Fatalf("client B found %d of client A's customers", list.Total)
	}
}

// --- 101.2 segments ------------------------------------------------------------------

// The blueprint's examples: average spend over 100, blank and 200 is 150; a
// bill with three lines is one visit; someone who never bought is "Never
// bought", not the lowest spender.
func TestCRMSegmentExamples(t *testing.T) {
	e := newCRMTestEnv(t)
	a, _ := e.customer("Mohan A", map[string]interface{}{"gender": "Male", "date_of_birth": time.Now().Format("2006-01-02")})
	b, _ := e.customer("Mohan B", nil)
	c, _ := e.customer("Mohan C", nil)
	e.bill(a, 100, 10, 1)
	e.bill(c, 200, 5, 3)
	var avg float64
	if err := db.DB.QueryRow(crmFactsCTE(e.schema, crmDefaultLifecycle, false) + ` SELECT AVG(avg_bill) FROM cf`).Scan(&avg); err != nil {
		t.Fatal(err)
	}
	if avg != 150 {
		t.Fatalf("average of 100, blank and 200 is %v, want 150", avg)
	}
	if f, _ := crmCustomerFacts(e.schema, c); f.Visits != 1 {
		t.Fatalf("a bill with three lines is %d visits", f.Visits)
	}
	if f, _ := crmCustomerFacts(e.schema, b); f.Lifecycle != "Never bought" || f.RFM != "" || f.AvgBill.Valid {
		t.Fatalf("never bought: %+v", f)
	}
	def := CRMSegmentGroup{Op: "or", Rules: []CRMSegmentRule{
		{Field: "spend", Cmp: ">=", Value: 150.0},
		{Group: &CRMSegmentGroup{Op: "and", Rules: []CRMSegmentRule{{Field: "gender", Value: "Male"}, {Field: "birthday_month", Value: 0.0}}}},
	}}
	cnt, err := CountCRMSegment(e.tenantID, "Admin", def, CRMChannelSMS)
	if err != nil {
		t.Fatal(err)
	}
	if cnt.Matched != 2 || cnt.NoConsent != 2 {
		t.Fatalf("segment: matched %d, no consent %d - want 2 and 2", cnt.Matched, cnt.NoConsent)
	}
	why, err := ExplainCRMSegment(e.tenantID, def, b)
	if err != nil || why.Matched {
		t.Fatalf("B is not in it: %+v %v", why, err)
	}
	notBought := CRMSegmentGroup{Op: "and", Rules: []CRMSegmentRule{{Field: "not_bought", Days: 7}, {Field: "lifecycle", Cmp: "!=", Value: "Never bought"}}}
	if cnt, _ := CountCRMSegment(e.tenantID, "Admin", notBought, ""); cnt.Matched != 1 {
		t.Fatalf("did not buy in 7 days (and bought before): %d, want 1 (A)", cnt.Matched)
	}
	s, err := SaveCRMSegment(e.tenantID, "", "Big spenders", "", def, "test")
	if err != nil {
		t.Fatal(err)
	}
	s2, _ := SaveCRMSegment(e.tenantID, s.ID, "Big spenders", "", def, "test")
	s3, _ := SaveCRMSegment(e.tenantID, s.ID, "Big spenders", "", notBought, "test")
	if s2.CurrentVersion != 1 || s3.CurrentVersion != 2 {
		t.Fatalf("versions: unchanged %d, changed %d", s2.CurrentVersion, s3.CurrentVersion)
	}
	if _, err := SaveCRMSegment(e.tenantID, "", "Bad", "", CRMSegmentGroup{Rules: []CRMSegmentRule{{Field: "spend; DROP TABLE x", Cmp: ">", Value: 1.0}}}, "test"); err == nil {
		t.Fatal("an unknown field was accepted")
	}
}

// --- 101.1 merge, masking -------------------------------------------------------------

func TestCRMMergeKeepsOneWaitingCopy(t *testing.T) {
	e := newCRMTestEnv(t)
	keep, _ := e.customer("Om Prakash", nil)
	dup, _ := e.customer("Om Prakash", nil)
	e.grant(dup, CRMChannelSMS)
	e.queue(CRMSendRequest{RunKind: "campaign", RunID: "RUN-M1", CustomerID: keep, Channel: CRMChannelSMS, Body: "x"})
	d1 := e.queue(CRMSendRequest{RunKind: "campaign", RunID: "RUN-M1", CustomerID: dup, Channel: CRMChannelSMS, Body: "x"})
	e.queue(CRMSendRequest{RunKind: "campaign", RunID: "RUN-M2", CustomerID: dup, Channel: CRMChannelSMS, Body: "x"})
	p, err := PreviewCRMMerge(e.tenantID, "Admin", keep, dup)
	if err != nil || p.QueuedMessages != 2 || p.MessagesDropped != 1 || p.ConsentRows != 1 {
		t.Fatalf("preview: %+v %v", p, err)
	}
	if err := MergeCustomers(e.tenantID, keep, dup, "test"); err != nil {
		t.Fatal(err)
	}
	if st, r := e.sendStatus(d1); st != crmSendCancelled || r != "MERGED_DUPLICATE" {
		t.Fatalf("the duplicate copy: %s %s", st, r)
	}
	if n := e.count(`SELECT COUNT(*) FROM {s}.crm_sends WHERE customer_id = $1 AND status = 'Queued'`, keep); n != 2 {
		t.Fatalf("the kept customer has %d waiting messages, want 2 (one per run)", n)
	}
	if v := e.str(`SELECT data->>'consent_sms' FROM {s}.documents WHERE id = $1`, keep); v != "true" {
		t.Fatalf("the duplicate's consent did not move: %s", v)
	}
}

func TestCRMContactMasking(t *testing.T) {
	e := newCRMTestEnv(t)
	if !RoleSeesContacts(e.tenantID, "Cashier") || RoleSeesContacts(e.tenantID, "Accountant") || !RoleSeesContacts(e.tenantID, "Admin") {
		t.Fatal("default contact access: Cashier and full-access roles see contacts, others do not")
	}
	data := map[string]interface{}{"phone": "9876543210", "email": "asha@example.com"}
	MaskCustomerContacts(e.tenantID, "Accountant", data)
	if data["phone"] != "98******10" || data["email"] != "a***@example.com" {
		t.Fatalf("masked: %v", data)
	}
	payload := map[string]interface{}{"phone": "98******10", "email": "a***@example.com", "name": "Asha"}
	crmRestoreMaskedContacts(e.tenantID, "Accountant", map[string]interface{}{"phone": "9876543210", "email": "asha@example.com"}, payload)
	if payload["phone"] != "9876543210" || payload["email"] != "asha@example.com" {
		t.Fatalf("saving a masked record must keep the real contacts: %v", payload)
	}
	rows := maskContactColumns(e.tenantID, []ReportColumn{{Key: "phone"}, {Key: "email"}}, []map[string]interface{}{{"phone": "9876543210", "email": "x@y.z"}}, "Accountant")
	if rows[0]["phone"] != "98******10" {
		t.Fatalf("report export not masked: %v", rows)
	}
	if hidden, _ := HiddenFieldsForRole(e.tenantID, "Accountant", "Customer"); !strings.Contains(strings.Join(hidden, ","), "phone") {
		t.Fatal("a masked phone must not be searchable")
	}
}

func TestCRMHoldoutSameInSQLAndGo(t *testing.T) {
	e := newCRMTestEnv(t)
	for i := 0; i < 200; i++ {
		cust := fmt.Sprintf("CUST-%d", i)
		var inSQL bool
		if err := db.DB.QueryRow(`SELECT `+crmHoldoutSQL("$1::text", "$2::text", 10), "CMP-1", cust).Scan(&inSQL); err != nil {
			t.Fatal(err)
		}
		if inSQL != crmInHoldout("CMP-1", cust, 10) {
			t.Fatalf("hold-out disagrees for %s", cust)
		}
	}
	_ = e
}

// --- 101.3 campaigns end to end -----------------------------------------------------------

func TestCRMCampaignEndToEnd(t *testing.T) {
	e := newCRMTestEnv(t)
	e.mockSender(CRMChannelSMS, "accept")
	tpl := e.template(CRMChannelSMS, CRMPurposeMarketing, "{{customer.first_name|Hi}}, 10% off this weekend. Code {{voucher.code}}.")
	var allow []string
	var custs []string
	for i := 0; i < 3; i++ {
		c, phone := e.reachableCustomer(fmt.Sprintf("Priya %d", i), CRMChannelSMS)
		allow = append(allow, e.e164(phone))
		custs = append(custs, c)
	}
	e.customer("No consent", nil)
	e.allow(allow...)
	seg, err := SaveCRMSegment(e.tenantID, "", "SMS yes", "", CRMSegmentGroup{Op: "and", Rules: []CRMSegmentRule{{Field: "consent", Value: "SMS"}}}, "test")
	if err != nil {
		t.Fatal(err)
	}
	camp, err := SaveCRMCampaign(e.tenantID, "", CRMCampaignInput{Name: "Weekend", Channel: "SMS", SegmentID: seg.ID, TemplateID: tpl, Budget: 100,
		Voucher: &struct {
			DiscountType  string `json:"discount_type"`
			DiscountValue int    `json:"discount_value"`
			ValidDays     int    `json:"valid_days"`
			Channel       string `json:"channel"`
		}{DiscountType: "Percentage", DiscountValue: 10, ValidDays: 7}}, "maker")
	if err != nil {
		t.Fatal(err)
	}
	preview, err := PreviewCRMCampaign(e.tenantID, "Admin", camp.ID)
	if err != nil || preview.Final != 3 || len(preview.Problems) != 0 {
		t.Fatalf("pre-send waterfall: %+v %v", preview, err)
	}
	if err := e.approvalRule("Campaign"); err != nil {
		t.Fatal(err)
	}
	if _, err := SubmitCRMCampaign(e.tenantID, camp.ID, "maker", "Store Manager"); err != nil {
		t.Fatal(err)
	}
	e.set("crm.campaign_second_approver", "true")
	if SubmitterMayDecideOwn(e.tenantID, "Campaign", "Admin") {
		t.Fatal("D13: with the second-approver switch on, a submitter cannot approve their own campaign")
	}
	if err := DecideApproval(e.tenantID, "Campaign", camp.ID, "checker", "Admin", "", "Approved", "ok"); err != nil {
		t.Fatal(err)
	}
	if n := e.count(`SELECT COUNT(*) FROM {s}.crm_campaign_runs WHERE campaign_id = $1 AND status = 'Scheduled'`, camp.ID); n != 1 {
		t.Fatalf("approval scheduled %d runs", n)
	}
	RunCRMTickForSchema(e.schema)
	RunCRMTickForSchema(e.schema)
	runID := e.str(`SELECT id FROM {s}.crm_campaign_runs WHERE campaign_id = $1`, camp.ID)
	res, err := GetCRMCampaignResults(e.tenantID, runID)
	if err != nil {
		t.Fatal(err)
	}
	if res.Matched != 3 || res.Sent != 3 || res.VouchersIssued != 3 || res.Delivered != nil {
		t.Fatalf("results: %+v (SMS delivery not reported is blank, not 0)", res)
	}
	// D10: a bill the next day is credited once
	e.bill(custs[0], 999, 0, 1)
	e.exec(`UPDATE {s}.crm_sends SET sent_at = sent_at - interval '1 day' WHERE run_id = $1`, runID)
	if res, _ = GetCRMCampaignResults(e.tenantID, runID); res.BillsCredited != 1 || res.SalesCredited != 999 {
		t.Fatalf("credited: %d bills, %.2f", res.BillsCredited, res.SalesCredited)
	}
	if _, err := SaveCRMCampaign(e.tenantID, camp.ID, CRMCampaignInput{Name: "Weekend 2", Channel: "SMS"}, "maker"); err == nil {
		t.Fatal("a finished campaign was edited")
	}
}

// --- 101.5 feedback, tickets ---------------------------------------------------------------

func TestCRMFeedbackLowScoreOpensOneTicket(t *testing.T) {
	e := newCRMTestEnv(t)
	tpl := e.template(CRMChannelWhatsApp, CRMPurposeService, "How did we do? {{link.feedback}}")
	s, err := SaveCRMSurvey(e.tenantID, "", CRMSurvey{Name: "After a bill", Question: "Would you recommend us?", Scale: "nps", Channel: "WhatsApp", TemplateID: tpl, Enabled: true, CooldownDays: 30}, "test")
	if err != nil {
		t.Fatal(err)
	}
	c, _ := e.customer("Ruchi Sen", nil)
	bill := e.bill(c, 500, 0, 1)
	_ = RecordCRMEvent(e.schema, c, CRMEventBillPaid, bill, map[string]interface{}{"store": "ST-T"})
	crmProcessEvents(e.schema)
	bill2 := e.bill(c, 700, 0, 1)
	_ = RecordCRMEvent(e.schema, c, CRMEventBillPaid, bill2, nil)
	crmProcessEvents(e.schema)
	if n := e.count(`SELECT COUNT(*) FROM {s}.crm_feedback WHERE customer_id = $1`, c); n != 1 {
		t.Fatalf("%d feedback requests - the cool-down allows one", n)
	}
	token, _ := crmToken(12)
	e.exec(`UPDATE {s}.crm_feedback SET token_hash = $2 WHERE customer_id = $1`, c, crmTokenHash(token))
	if _, err := SubmitCRMFeedback(e.tenantID, token, 11, ""); err == nil {
		t.Fatal("a score of 11 on a 0-10 scale was accepted")
	}
	if _, err := SubmitCRMFeedback(e.tenantID, token, 3, "Rude at the counter"); err != nil {
		t.Fatal(err)
	}
	_, _ = SubmitCRMFeedback(e.tenantID, token, 2, "again")
	if n := e.count(`SELECT COUNT(*) FROM {s}.documents WHERE doctype = 'ServiceTicket' AND data->>'customer' = $1`, c); n != 1 {
		t.Fatalf("%d tickets for one low score", n)
	}
	sum, _ := GetCRMFeedbackSummary(e.tenantID, s.ID, "", "")
	if len(sum.Versions) != 1 || sum.Versions[0].NPS == nil || *sum.Versions[0].NPS != -100 {
		t.Fatalf("NPS: %+v", sum.Versions)
	}
}

func TestCRMTicketWorkingHours(t *testing.T) {
	e := newCRMTestEnv(t)
	e.set("crm.business_hours", "10:00-20:00")
	e.set("crm.business_days", "123456")
	loc := crmLocation(e.schema)
	fri := time.Date(2026, 10, 23, 19, 0, 0, 0, loc) // a Friday, 7 pm
	if got := crmAddBusinessHours(e.schema, fri, 4); !got.Equal(time.Date(2026, 10, 24, 13, 0, 0, 0, loc)) {
		t.Fatalf("Fri 19:00 + 4 working hours = %v, want Sat 13:00", got)
	}
	sat := time.Date(2026, 10, 24, 19, 30, 0, 0, loc)
	if got := crmAddBusinessHours(e.schema, sat, 2); !got.Equal(time.Date(2026, 10, 26, 11, 30, 0, 0, loc)) {
		t.Fatalf("Sat 19:30 + 2 working hours = %v, want Mon 11:30", got)
	}
}

// --- 101.12 appointments, 101.6 pipeline -----------------------------------------------------

func TestCRMAppointmentCapacityRace(t *testing.T) {
	e := newCRMTestEnv(t)
	if _, err := SaveCRMStoreSlots(e.tenantID, CRMStoreSlots{Store: "ST-T", SlotMinutes: 30, Capacity: 1, Opens: "00:00", Closes: "23:59", Days: "1234567", ReminderChannel: "WhatsApp"}, "test"); err != nil {
		t.Fatal(err)
	}
	day := time.Now().In(crmLocation(e.schema)).AddDate(0, 0, 2).Format("2006-01-02")
	slot := day + " 11:00"
	a, _ := e.customer("Sara A", nil)
	b, _ := e.customer("Sara B", nil)
	var wg sync.WaitGroup
	var mu sync.Mutex
	var ok []string
	var full int
	for _, c := range []string{a, b} {
		wg.Add(1)
		go func(c string) {
			defer wg.Done()
			ap, err := BookCRMAppointment(e.tenantID, CRMAppointmentInput{CustomerID: c, Store: "ST-T", Kind: "fitting", SlotStart: slot}, "test")
			mu.Lock()
			defer mu.Unlock()
			var verr *ValidationError
			if err == nil {
				ok = append(ok, ap.ID)
			} else if errors.As(err, &verr) && verr.Code == CRMCodeSlotFull {
				full++
			} else {
				t.Errorf("book: %v", err)
			}
		}(c)
	}
	wg.Wait()
	if len(ok) != 1 || full != 1 {
		t.Fatalf("two bookings for the last place: %d booked, %d refused", len(ok), full)
	}
	if n := e.count(`SELECT COUNT(*) FROM {s}.crm_sends WHERE run_kind = 'reminder' AND run_id LIKE $1 || ':%' AND status = 'Queued'`, ok[0]); n != 2 {
		t.Fatalf("%d reminders queued, want 2", n)
	}
	if _, err := SetCRMAppointmentStatus(e.tenantID, ok[0], "cancel", "customer called", "test"); err != nil {
		t.Fatal(err)
	}
	if n := e.count(`SELECT COUNT(*) FROM {s}.crm_sends WHERE run_kind = 'reminder' AND run_id LIKE $1 || ':%' AND status = 'Queued'`, ok[0]); n != 0 {
		t.Fatalf("cancelling left %d reminders queued", n)
	}
}

func TestCRMLeadConvertOnceAndWinNeedsQuotation(t *testing.T) {
	e := newCRMTestEnv(t)
	e.exec(`INSERT INTO {s}.documents (id, doctype, data, status, created_by) VALUES ('SKU-Q1', 'Item', '{"code":"SKU-Q1","name":"Uniform shirt"}', 'Active', 'test')`)
	l, err := SaveCRMLead(e.tenantID, "", CRMLead{Name: "Ramesh", Company: "Acme School", Phone: fmt.Sprintf("97%08d", time.Now().UnixNano()%100000000), Source: "Exhibition"}, "test")
	if err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	results := make([]*CRMLead, 2)
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			results[i], _ = ConvertCRMLead(e.tenantID, l.ID, "Uniforms 2027", 0, "", "test")
		}(i)
	}
	wg.Wait()
	if results[0] == nil || results[1] == nil || results[0].OpportunityID == "" || results[0].OpportunityID != results[1].OpportunityID || results[0].CustomerID != results[1].CustomerID {
		t.Fatalf("converting twice must give one customer and one opportunity: %+v %+v", results[0], results[1])
	}
	if n := e.count(`SELECT COUNT(*) FROM {s}.crm_opportunities WHERE lead_id = $1`, l.ID); n != 1 {
		t.Fatalf("%d opportunities from one lead", n)
	}
	opp := results[0].OpportunityID
	stages, _ := ListCRMPipelineStages(e.tenantID)
	var won, lost string
	for _, s := range stages {
		if s.Kind == "won" {
			won = s.ID
		}
		if s.Kind == "lost" {
			lost = s.ID
		}
	}
	if _, err := MoveCRMOpportunity(e.tenantID, opp, won, "", "test"); err == nil {
		t.Fatal("a deal was won without an approved quotation")
	}
	if _, err := MoveCRMOpportunity(e.tenantID, opp, lost, "", "test"); err == nil {
		t.Fatal("a deal was lost without a reason")
	}
	if _, err := SaveCRMQuotation(e.tenantID, opp, []CRMQuotationLine{{SKU: "SKU-Q1", Qty: 200, Rate: 350}}, "", "", "test"); err != nil {
		t.Fatal(err)
	}
	if err := e.approvalRule("SalesQuotation"); err != nil {
		t.Fatal(err)
	}
	if _, err := SubmitCRMQuotation(e.tenantID, opp, "seller", "Store Manager"); err != nil {
		t.Fatal(err)
	}
	q, _ := GetCRMOpportunity(e.tenantID, opp)
	if err := DecideApproval(e.tenantID, "SalesQuotation", q.QuotationID, "boss", "Admin", "", "Approved", "ok"); err != nil {
		t.Fatal(err)
	}
	o, err := MoveCRMOpportunity(e.tenantID, opp, won, "", "test")
	if err != nil {
		t.Fatalf("won with an approved quotation: %v", err)
	}
	o2, _ := MoveCRMOpportunity(e.tenantID, opp, won, "", "test")
	if o.Status != "Won" || o.SalesOrderID == "" || o2.SalesOrderID != o.SalesOrderID {
		t.Fatalf("one Sales Order per won deal: %+v / %+v", o, o2)
	}
	if n := e.count(`SELECT COUNT(*) FROM {s}.documents WHERE doctype = 'SalesOrder' AND data->>'channel_order_id' = $1`, opp); n != 1 {
		t.Fatalf("%d sales orders for one deal", n)
	}
	if n := e.count(`SELECT COUNT(*) FROM {s}.crm_opportunity_history WHERE opportunity_id = $1`, opp); n < 3 {
		t.Fatalf("history has %d rows", n)
	}
	rows, _ := GetCRMForecast(e.tenantID)
	booked := 0.0
	for _, r := range rows {
		booked += r.Booked
	}
	if booked <= 0 {
		t.Fatalf("a won deal is booked in the forecast: %+v", rows)
	}
	snap, err := TakeCRMForecastSnapshot(e.tenantID, "", "test")
	if err != nil || len(snap.Rows) == 0 {
		t.Fatalf("snapshot: %+v %v", snap, err)
	}
}

// Ready-made journeys install once, published and off.
func TestCRMReadyMadeJourneys(t *testing.T) {
	e := newCRMTestEnv(t)
	list, err := InstallCRMReadyJourneys(e.tenantID, "test")
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != len(crmReadyJourneys) {
		t.Fatalf("%d journeys installed, want %d", len(list), len(crmReadyJourneys))
	}
	for _, j := range list {
		if j.Enabled || j.CurrentVersion != 1 {
			t.Fatalf("%s: enabled=%v version=%d - ready-made journeys are published and off", j.Name, j.Enabled, j.CurrentVersion)
		}
	}
	again, _ := InstallCRMReadyJourneys(e.tenantID, "test")
	if len(again) != len(list) {
		t.Fatalf("installing twice made %d journeys", len(again))
	}
	if n := e.count(`SELECT COUNT(*) FROM {s}.crm_surveys`); n != 1 {
		t.Fatalf("%d feedback surveys", n)
	}
}

// approvalRule makes sure a doctype has an approval rule (a new client
// schema already carries the migration's seeded ones).
func (e *crmTestEnv) approvalRule(doctype string) error {
	if role, err := RequiredApproverRoleForAmount(e.tenantID, doctype, 0); err == nil && role != "" {
		return nil
	}
	_, err := UpsertApprovalRule(e.tenantID, doctype, 0, nil, "Store Manager", nil)
	return err
}
