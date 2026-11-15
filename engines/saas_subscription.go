package engines

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"strings"
	"time"

	"custom_erp/db"
)

// 50.12 / BLD-026 (ask 193, ADR-009): the platform's own SaaS subscription,
// billed through Razorpay behind this small adapter.
//
// Provider facts, from Razorpay's public documentation (never guessed):
//   - Plans:         POST /v1/plans {period, interval, item{name, amount, currency, description}, notes}
//     https://razorpay.com/docs/api/payments/subscriptions/create-plan/
//   - Subscriptions: POST /v1/subscriptions {plan_id, total_count, customer_notify, start_at, notes};
//     status is created | authenticated | active | pending | halted | cancelled | completed | expired
//     https://razorpay.com/docs/api/payments/subscriptions/create-subscription/
//   - Fetch:         GET /v1/subscriptions/:id  (reconciliation)
//     https://razorpay.com/docs/api/payments/subscriptions/
//   - Webhooks:      subscription.authenticated | activated | charged | completed | updated |
//     pending | halted | cancelled | paused | resumed; body carries event, created_at (unix),
//     payload.subscription.entity {id, plan_id, status, current_start, current_end, notes, ...}
//     and, when a payment was attempted, payload.payment.entity {id, amount, currency, status,
//     error_description}. https://razorpay.com/docs/webhooks/subscriptions/
//   - Signature:     HMAC-SHA256 of the raw body with the webhook secret, header
//     X-Razorpay-Signature; header X-Razorpay-Event-Id is unique per event.
//     https://razorpay.com/docs/webhooks/validate-test/
//
// Reused from engines/finance_payment_links.go: the key store (the platform's
// own Razorpay keys are the "razorpay" channel credential of the billing
// client, SAAS_BILLING_TENANT, default "default"), razorpayCall (the outside
// call gate) and the HMAC check and event claim in the HTTP handler.
//
// Policy (BLD-026): currency INR; the platform issues its own GST invoice for
// each captured payment (not Razorpay's receipt); the fee is GST-inclusive at
// 18%. Razorpay owns the card mandate and collection. A failed payment
// (subscription.pending) starts a 7-day grace with full access; if it is not
// paid by then, or Razorpay halts the subscription, the client is suspended
// (data kept, resumes the moment a payment is charged). Cancellation keeps
// access to the end of the paid period, then suspends. Refunds are made in
// the Razorpay dashboard by the owner; they do not move entitlements.
// Webhooks only move the internal state; events older than the last applied
// one are recorded and not applied; a repeated event id or payment id does
// nothing twice.

const (
	SaaSStatePending   = "pending"
	SaaSStateActive    = "active"
	SaaSStatePastDue   = "past_due"
	SaaSStateSuspended = "suspended"
	SaaSStateCancelled = "cancelled"
	SaaSStateExpired   = "expired"

	saasGSTRate = 18
)

// saasGraceDays is how long a failed payment keeps full access.
var saasGraceDays = 7

// saasNow is the clock; a test moves it.
var saasNow = time.Now

// saasBillingTenantOverride lets a test pick the client that holds the keys.
var saasBillingTenantOverride string

func saasBillingTenant() string {
	if saasBillingTenantOverride != "" {
		return saasBillingTenantOverride
	}
	if v := strings.TrimSpace(os.Getenv("SAAS_BILLING_TENANT")); v != "" {
		return v
	}
	return "default"
}

// SaaSBillingTenant is the client whose Razorpay keys bill the platform's fee.
func SaaSBillingTenant() string { return saasBillingTenant() }

func saasFail(format string, args ...interface{}) error {
	return &ValidationError{Code: "SAAS-0191", Message: fmt.Sprintf(format, args...)}
}

// SaaSPlan is a sellable subscription.
type SaaSPlan struct {
	PlanKey        string           `json:"plan_key"`
	DisplayName    string           `json:"display_name"`
	PackageKeys    []string         `json:"package_keys"`
	AmountPaise    int64            `json:"amount_paise"`
	TrialDays      int              `json:"trial_days"`
	Quotas         map[string]int64 `json:"quotas"`
	OveragePaise   map[string]int64 `json:"overage_paise"`
	RazorpayPlanID string           `json:"razorpay_plan_id,omitempty"`
	Active         bool             `json:"active"`
}

// UpsertSaaSPlan saves a plan. Its packages must be a supported combination.
func UpsertSaaSPlan(p SaaSPlan) error {
	p.PlanKey = strings.TrimSpace(p.PlanKey)
	if p.PlanKey == "" || p.DisplayName == "" || p.AmountPaise < 0 {
		return saasFail("a plan needs a key, a name and a fee of zero or more")
	}
	if ok, why := ValidatePackageBundle(p.PackageKeys); !ok {
		return saasFail("%s", why)
	}
	pk, _ := json.Marshal(p.PackageKeys)
	q, _ := json.Marshal(nz64(p.Quotas))
	o, _ := json.Marshal(nz64(p.OveragePaise))
	_, err := db.DB.Exec(`INSERT INTO public.saas_plans (plan_key, display_name, package_keys, amount_paise, trial_days, quotas, overage_paise, active)
		VALUES ($1, $2, $3, $4, $5, $6, $7, TRUE)
		ON CONFLICT (plan_key) DO UPDATE SET display_name = EXCLUDED.display_name, package_keys = EXCLUDED.package_keys,
		  amount_paise = EXCLUDED.amount_paise, trial_days = EXCLUDED.trial_days, quotas = EXCLUDED.quotas, overage_paise = EXCLUDED.overage_paise,
		  razorpay_plan_id = CASE WHEN public.saas_plans.amount_paise = EXCLUDED.amount_paise THEN public.saas_plans.razorpay_plan_id ELSE NULL END`,
		p.PlanKey, p.DisplayName, string(pk), p.AmountPaise, p.TrialDays, string(q), string(o))
	return err
}

func nz64(m map[string]int64) map[string]int64 {
	if m == nil {
		return map[string]int64{}
	}
	return m
}

// GetSaaSPlan reads one plan.
func GetSaaSPlan(planKey string) (*SaaSPlan, error) {
	var p SaaSPlan
	var pk, q, o string
	var rz sql.NullString
	err := db.DB.QueryRow(`SELECT plan_key, display_name, package_keys::text, amount_paise, trial_days, quotas::text, overage_paise::text, razorpay_plan_id, active
		FROM public.saas_plans WHERE plan_key = $1`, planKey).Scan(&p.PlanKey, &p.DisplayName, &pk, &p.AmountPaise, &p.TrialDays, &q, &o, &rz, &p.Active)
	if err == sql.ErrNoRows {
		return nil, saasFail("there is no plan %q", planKey)
	}
	if err != nil {
		return nil, err
	}
	_ = json.Unmarshal([]byte(pk), &p.PackageKeys)
	_ = json.Unmarshal([]byte(q), &p.Quotas)
	_ = json.Unmarshal([]byte(o), &p.OveragePaise)
	p.RazorpayPlanID = rz.String
	return &p, nil
}

// SaaSSubscription is a client's subscription as the platform holds it.
type SaaSSubscription struct {
	TenantID       string     `json:"tenant_id"`
	PlanKey        string     `json:"plan_key"`
	State          string     `json:"state"`
	RazorpayID     string     `json:"razorpay_subscription_id,omitempty"`
	ShortURL       string     `json:"short_url,omitempty"`
	PeriodStart    *time.Time `json:"period_start,omitempty"`
	PeriodEnd      *time.Time `json:"period_end,omitempty"`
	GraceUntil     *time.Time `json:"grace_until,omitempty"`
	AccessUntil    *time.Time `json:"access_until,omitempty"`
	BillingSuspend bool       `json:"billing_suspended"`
	LastEventAt    int64      `json:"last_event_at"`
	Explanation    string     `json:"explanation"`
}

type rowScanner interface{ Scan(...interface{}) error }

const saasSubCols = `tenant_id, plan_key, state, COALESCE(razorpay_subscription_id,''), short_url, period_start, period_end, grace_until, access_until, billing_suspended, last_event_at, explanation`

func scanSaaSSub(r rowScanner) (*SaaSSubscription, error) {
	var s SaaSSubscription
	var ps, pe, gu, au sql.NullTime
	if err := r.Scan(&s.TenantID, &s.PlanKey, &s.State, &s.RazorpayID, &s.ShortURL, &ps, &pe, &gu, &au, &s.BillingSuspend, &s.LastEventAt, &s.Explanation); err != nil {
		return nil, err
	}
	if ps.Valid {
		s.PeriodStart = &ps.Time
	}
	if pe.Valid {
		s.PeriodEnd = &pe.Time
	}
	if gu.Valid {
		s.GraceUntil = &gu.Time
	}
	if au.Valid {
		s.AccessUntil = &au.Time
	}
	return &s, nil
}

// GetSaaSSubscription returns a client's subscription, or nil when it has none.
func GetSaaSSubscription(tenantID string) (*SaaSSubscription, error) {
	s, err := scanSaaSSub(db.DB.QueryRow(`SELECT `+saasSubCols+` FROM public.saas_subscriptions WHERE tenant_id = $1`, tenantID))
	if err == sql.ErrNoRows {
		return nil, nil
	}
	return s, err
}

// ensureRazorpayPlan makes the plan at Razorpay once and remembers its id.
func ensureRazorpayPlan(ctx context.Context, p *SaaSPlan) error {
	if p.RazorpayPlanID != "" {
		return nil
	}
	out, err := razorpayCall(ctx, saasBillingTenant(), "POST", "/v1/plans", map[string]interface{}{
		"period": "monthly", "interval": 1,
		"item":  map[string]interface{}{"name": p.DisplayName, "amount": p.AmountPaise, "currency": "INR", "description": "ERP subscription " + p.PlanKey},
		"notes": map[string]string{"plan_key": p.PlanKey},
	})
	if err != nil {
		return err
	}
	id := strings.TrimSpace(fmt.Sprint(out["id"]))
	if id == "" || id == "<nil>" {
		return saasFail("Razorpay made no plan")
	}
	if _, err := db.DB.Exec(`UPDATE public.saas_plans SET razorpay_plan_id = $1 WHERE plan_key = $2 AND razorpay_plan_id IS NULL`, id, p.PlanKey); err != nil {
		return err
	}
	p.RazorpayPlanID = id
	return nil
}

// StartSaaSSubscription subscribes a client to a plan: it makes the plan and
// the subscription at Razorpay and gives back the page where the client's
// owner authorises the payment. Asking again while one is open gives the same
// subscription; it never makes a second one at Razorpay.
func StartSaaSSubscription(ctx context.Context, tenantID, planKey, actor string) (*SaaSSubscription, error) {
	plan, err := GetSaaSPlan(planKey)
	if err != nil {
		return nil, err
	}
	if !plan.Active {
		return nil, saasFail("plan %s is not on sale", planKey)
	}
	if _, err := db.GetTenantSchema(tenantID); err != nil {
		return nil, err
	}
	tx, err := db.DB.Begin()
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	if _, err := tx.Exec(`SELECT pg_advisory_xact_lock(hashtext($1))`, "saassub:"+tenantID); err != nil {
		return nil, err
	}
	if cur, err := scanSaaSSub(tx.QueryRow(`SELECT `+saasSubCols+` FROM public.saas_subscriptions WHERE tenant_id = $1`, tenantID)); err == nil {
		if cur.State == SaaSStatePending || cur.State == SaaSStateActive || cur.State == SaaSStatePastDue {
			if cur.PlanKey != planKey {
				return nil, saasFail("%s already has a subscription to %s; change or cancel it first", tenantID, cur.PlanKey)
			}
			return cur, nil
		}
	} else if err != sql.ErrNoRows {
		return nil, err
	}
	if err := ensureRazorpayPlan(ctx, plan); err != nil {
		return nil, err
	}
	body := map[string]interface{}{
		"plan_id": plan.RazorpayPlanID, "total_count": 120, "customer_notify": true,
		"notes": map[string]string{"tenant_id": tenantID, "plan_key": planKey},
	}
	var trialEnd *time.Time
	if plan.TrialDays > 0 {
		t := saasNow().AddDate(0, 0, plan.TrialDays)
		trialEnd = &t
		body["start_at"] = t.Unix() // Razorpay: first charge at start_at
	}
	out, err := razorpayCall(ctx, saasBillingTenant(), "POST", "/v1/subscriptions", body)
	if err != nil {
		return nil, err
	}
	subID := strings.TrimSpace(fmt.Sprint(out["id"]))
	if subID == "" || subID == "<nil>" {
		return nil, saasFail("Razorpay made no subscription")
	}
	short := ""
	if s, ok := out["short_url"].(string); ok {
		short = s
	}
	row := tx.QueryRow(`INSERT INTO public.saas_subscriptions (tenant_id, plan_key, state, razorpay_subscription_id, short_url, explanation)
		VALUES ($1, $2, 'pending', $3, $4, 'Waiting for the payment method to be authorised.')
		ON CONFLICT (tenant_id) DO UPDATE SET plan_key = EXCLUDED.plan_key, state = 'pending', razorpay_subscription_id = EXCLUDED.razorpay_subscription_id,
		  short_url = EXCLUDED.short_url, period_start = NULL, period_end = NULL, grace_until = NULL, access_until = NULL, last_event_at = 0,
		  explanation = EXCLUDED.explanation, updated_at = now()
		RETURNING `+saasSubCols, tenantID, planKey, subID, short)
	sub, err := scanSaaSSub(row)
	if err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	if trialEnd != nil {
		if _, err := SetTenantTrial(tenantID, plan.TrialDays, actor, "subscription trial"); err != nil {
			log.Printf("[SAAS] %s: trial not set: %v", tenantID, err)
		}
	}
	LogAuditEvent(tenantID, actor, "saas.subscription_started", "Created", subID+" "+planKey)
	return sub, nil
}

// ---------------------------------------------------------------------------
// Webhook
// ---------------------------------------------------------------------------

type saasEvent struct {
	Event     string `json:"event"`
	CreatedAt int64  `json:"created_at"`
	Payload   struct {
		Subscription struct {
			Entity struct {
				ID           string          `json:"id"`
				PlanID       string          `json:"plan_id"`
				Status       string          `json:"status"`
				CurrentStart int64           `json:"current_start"`
				CurrentEnd   int64           `json:"current_end"`
				Notes        json.RawMessage `json:"notes"` // Razorpay sends [] when empty
			} `json:"entity"`
		} `json:"subscription"`
		Payment struct {
			Entity struct {
				ID               string `json:"id"`
				Amount           int64  `json:"amount"`
				Currency         string `json:"currency"`
				Status           string `json:"status"`
				ErrorDescription string `json:"error_description"`
			} `json:"entity"`
		} `json:"payment"`
	} `json:"payload"`
}

func (e *saasEvent) noteTenant() string {
	var m map[string]interface{}
	if json.Unmarshal(e.Payload.Subscription.Entity.Notes, &m) == nil {
		if t, ok := m["tenant_id"].(string); ok {
			return t
		}
	}
	return ""
}

func unixTime(sec int64) *time.Time {
	if sec <= 0 {
		return nil
	}
	t := time.Unix(sec, 0)
	return &t
}

// SaaSEventID is the id a webhook is deduplicated on: Razorpay's own event id
// header, else a hash of the body.
func SaaSEventID(header string, body []byte) string {
	if h := strings.TrimSpace(header); h != "" {
		return h
	}
	sum := sha256.Sum256(body)
	return "sha256:" + hex.EncodeToString(sum[:])
}

// indianFiscalYear names the April-March year a moment falls in, in IST.
func indianFiscalYear(t time.Time) string {
	t = t.In(istZone)
	y := t.Year()
	if t.Month() < time.April {
		y--
	}
	return fmt.Sprintf("%d-%02d", y, (y+1)%100)
}

// HandleSaaSSubscriptionWebhook acts on one signature-verified subscription
// webhook. It returns a short result for the response.
func HandleSaaSSubscriptionWebhook(eventID string, body []byte) (string, error) {
	var ev saasEvent
	if err := json.Unmarshal(body, &ev); err != nil {
		return "", saasFail("the webhook body could not be read")
	}
	if !strings.HasPrefix(ev.Event, "subscription.") {
		return "ignored", nil
	}
	subID := strings.TrimSpace(ev.Payload.Subscription.Entity.ID)
	if eventID == "" {
		eventID = SaaSEventID("", body)
	}
	tx, err := db.DB.Begin()
	if err != nil {
		return "", err
	}
	defer tx.Rollback()
	if _, err := tx.Exec(`SELECT pg_advisory_xact_lock(hashtext($1))`, "saassubevt:"+subID); err != nil {
		return "", err
	}
	res, err := tx.Exec(`INSERT INTO public.saas_subscription_events (event_id, event, event_at, outcome) VALUES ($1, $2, $3, 'received') ON CONFLICT (event_id) DO NOTHING`,
		eventID, ev.Event, ev.CreatedAt)
	if err != nil {
		return "", err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return "duplicate", nil
	}
	record := func(tenantID, outcome, detail string) error {
		_, err := tx.Exec(`UPDATE public.saas_subscription_events SET tenant_id = $1, outcome = $2, detail = $3 WHERE event_id = $4`, tenantID, outcome, detail, eventID)
		return err
	}
	sub, err := scanSaaSSub(tx.QueryRow(`SELECT `+saasSubCols+` FROM public.saas_subscriptions WHERE razorpay_subscription_id = $1 FOR UPDATE`, subID))
	if err == sql.ErrNoRows && ev.noteTenant() != "" {
		sub, err = scanSaaSSub(tx.QueryRow(`SELECT `+saasSubCols+` FROM public.saas_subscriptions WHERE tenant_id = $1 FOR UPDATE`, ev.noteTenant()))
		if err == nil && sub.RazorpayID != "" && sub.RazorpayID != subID {
			err = sql.ErrNoRows // that client's subscription is a different one
		}
	}
	if err == sql.ErrNoRows {
		if err := record("", "ignored", "no subscription of ours is "+subID); err != nil {
			return "", err
		}
		return tx2commit(tx, "ignored")
	}
	if err != nil {
		return "", err
	}
	tenantID := sub.TenantID
	ent := ev.Payload.Subscription.Entity
	pay := ev.Payload.Payment.Entity
	ps, pe := unixTime(ent.CurrentStart), unixTime(ent.CurrentEnd)

	// A captured payment is money received: it gets its invoice whatever the
	// order of events, once.
	invoiceNo := ""
	if ev.Event == "subscription.charged" {
		if pay.ID == "" || pay.Amount <= 0 {
			return "", saasFail("the charged webhook for %s names no payment", subID)
		}
		if pay.Currency != "" && pay.Currency != "INR" {
			return "", saasFail("payment %s is in %s - subscriptions are billed in rupees only", pay.ID, pay.Currency)
		}
		invoiceNo, _, err = issueSaaSInvoiceTx(tx, tenantID, pay.ID, pay.Amount, ps, pe)
		if err != nil {
			return "", err
		}
	}
	// An event older than the last one applied must not undo it.
	if ev.CreatedAt < sub.LastEventAt {
		if err := record(tenantID, "stale", fmt.Sprintf("%s at %d is older than the applied %d%s", ev.Event, ev.CreatedAt, sub.LastEventAt, withInvoice(invoiceNo))); err != nil {
			return "", err
		}
		return tx2commit(tx, "stale")
	}
	now := saasNow()
	state, expl := sub.State, sub.Explanation
	grace, access := sub.GraceUntil, sub.AccessUntil
	periodStart, periodEnd := sub.PeriodStart, sub.PeriodEnd
	switch ev.Event {
	case "subscription.authenticated":
		expl = "Payment method authorised; the first charge is next."
	case "subscription.activated", "subscription.charged":
		state, grace, access = SaaSStateActive, nil, nil
		if ps != nil {
			periodStart = ps
		}
		if pe != nil {
			periodEnd = pe
		}
		expl = "Active."
		if periodEnd != nil {
			expl = "Paid up to " + periodEnd.In(istZone).Format("2 Jan 2006") + "."
		}
	case "subscription.pending":
		state = SaaSStatePastDue
		if grace == nil || sub.State != SaaSStatePastDue {
			g := now.AddDate(0, 0, saasGraceDays)
			grace = &g
		}
		why := ""
		if pay.ErrorDescription != "" {
			why = " (" + pay.ErrorDescription + ")"
		}
		expl = "The last payment failed" + why + ". Everything keeps working until " + grace.In(istZone).Format("2 Jan 2006") + "; pay by then to avoid suspension."
	case "subscription.halted":
		state = SaaSStateSuspended
		expl = "Payments kept failing, so the subscription was stopped. Your data is safe; pay to resume."
	case "subscription.cancelled", "subscription.completed":
		state = SaaSStateCancelled
		a := now
		if periodEnd != nil && periodEnd.After(now) {
			a = *periodEnd
		}
		access = &a
		expl = "Cancelled. Access continues until " + a.In(istZone).Format("2 Jan 2006") + "."
	case "subscription.paused", "subscription.resumed", "subscription.updated":
		if err := record(tenantID, "noted", ev.Event+" does not change access"); err != nil {
			return "", err
		}
		return tx2commit(tx, "noted")
	default:
		if err := record(tenantID, "ignored", ev.Event); err != nil {
			return "", err
		}
		return tx2commit(tx, "ignored")
	}
	if _, err := tx.Exec(`UPDATE public.saas_subscriptions SET state = $1, explanation = $2, grace_until = $3, access_until = $4, period_start = $5, period_end = $6,
		last_event_at = $7, razorpay_subscription_id = COALESCE(NULLIF(razorpay_subscription_id, ''), $8), updated_at = now() WHERE tenant_id = $9`,
		state, expl, grace, access, periodStart, periodEnd, ev.CreatedAt, subID, tenantID); err != nil {
		return "", err
	}
	if err := record(tenantID, "applied", ev.Event+withInvoice(invoiceNo)); err != nil {
		return "", err
	}
	if err := tx.Commit(); err != nil {
		return "", err
	}
	// The tenant's own lifecycle and modules follow the state. It is
	// idempotent; if it fails the event is un-claimed so Razorpay's retry
	// runs it again.
	if err := SyncTenantToSubscription(tenantID, "razorpay"); err != nil {
		_, _ = db.DB.Exec(`DELETE FROM public.saas_subscription_events WHERE event_id = $1`, eventID)
		return "", err
	}
	if invoiceNo != "" {
		return "applied, invoice " + invoiceNo, nil
	}
	return "applied", nil
}

func withInvoice(no string) string {
	if no == "" {
		return ""
	}
	return " invoice " + no
}

func tx2commit(tx *sql.Tx, result string) (string, error) {
	if err := tx.Commit(); err != nil {
		return "", err
	}
	return result, nil
}

// ---------------------------------------------------------------------------
// State -> tenant (the one place billing touches a client's access)
// ---------------------------------------------------------------------------

// saasAccessAllowed says whether a subscription state keeps the client in.
func saasAccessAllowed(s *SaaSSubscription, now time.Time) bool {
	switch s.State {
	case SaaSStateActive:
		return true
	case SaaSStatePastDue:
		return s.GraceUntil != nil && now.Before(*s.GraceUntil)
	case SaaSStateCancelled:
		return s.AccessUntil != nil && now.Before(*s.AccessUntil)
	}
	return false
}

func sameModuleSet(a map[string]bool, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for _, m := range b {
		if !a[m] {
			return false
		}
	}
	return true
}

// SyncTenantToSubscription makes the client's lifecycle and modules match its
// subscription, and only touches what differs. Safe to run any number of
// times, from the webhook, the sweep and the reconciliation alike.
func SyncTenantToSubscription(tenantID, actor string) error {
	sub, err := GetSaaSSubscription(tenantID)
	if err != nil || sub == nil {
		return err
	}
	now := saasNow()
	life, err := GetTenantLifecycle(tenantID)
	if err != nil {
		return err
	}
	if saasAccessAllowed(sub, now) {
		if sub.BillingSuspend && life.Status == TenantStatusSuspended {
			if err := ResumeTenant(tenantID, actor, "subscription paid"); err != nil {
				return err
			}
		}
		if sub.BillingSuspend {
			if _, err := db.DB.Exec(`UPDATE public.saas_subscriptions SET billing_suspended = FALSE WHERE tenant_id = $1`, tenantID); err != nil {
				return err
			}
		}
		var trial sql.NullTime
		_ = db.DB.QueryRow(`SELECT trial_ends_at FROM public.tenants WHERE tenant_id = $1`, tenantID).Scan(&trial)
		if sub.State == SaaSStateActive && trial.Valid {
			if _, err := SetTenantTrial(tenantID, 0, actor, "subscription active"); err != nil {
				return err
			}
		}
		return syncSubscriptionModules(tenantID, sub, actor)
	}
	switch sub.State {
	case SaaSStatePastDue, SaaSStateSuspended, SaaSStateCancelled, SaaSStateExpired:
		if life.Status == TenantStatusActive {
			if err := SuspendTenant(tenantID, actor, "subscription: "+sub.State); err != nil {
				return err
			}
			_, err = db.DB.Exec(`UPDATE public.saas_subscriptions SET billing_suspended = TRUE WHERE tenant_id = $1`, tenantID)
			return err
		}
	}
	return nil
}

func syncSubscriptionModules(tenantID string, sub *SaaSSubscription, actor string) error {
	plan, err := GetSaaSPlan(sub.PlanKey)
	if err != nil {
		return err
	}
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return err
	}
	now, _, err := enabledOptionalModules(db.DB, schema)
	if err != nil {
		return err
	}
	want := targetModules(plan.PackageKeys)
	if sameModuleSet(now, saasSortedKeys(want)) {
		return nil
	}
	_, err = TransitionTenantPackages(tenantID, plan.PackageKeys, actor, fmt.Sprintf("sub-sync-%s-%d", tenantID, saasNow().UnixNano()), true)
	return err
}

// EnforceSaaSBilling is the periodic sweep: a failed payment's grace that has
// run out suspends the client, and a cancelled subscription ends with its paid
// period. Returns how many subscriptions it moved.
func EnforceSaaSBilling() (int, error) {
	now := saasNow()
	rows, err := db.DB.Query(`SELECT tenant_id, state FROM public.saas_subscriptions
		WHERE (state = 'past_due' AND grace_until <= $1) OR (state = 'cancelled' AND access_until <= $1)`, now)
	if err != nil {
		return 0, err
	}
	type due struct{ tenant, state string }
	var list []due
	for rows.Next() {
		var d due
		if err := rows.Scan(&d.tenant, &d.state); err != nil {
			rows.Close()
			return 0, err
		}
		list = append(list, d)
	}
	rows.Close()
	moved := 0
	for _, d := range list {
		next, expl := SaaSStateSuspended, "The grace period for the failed payment ended, so access is paused. Your data is safe; pay to resume."
		if d.state == SaaSStateCancelled {
			next, expl = SaaSStateExpired, "The subscription ended. Your data is kept; subscribe again to return."
		}
		if _, err := db.DB.Exec(`UPDATE public.saas_subscriptions SET state = $1, explanation = $2, updated_at = now() WHERE tenant_id = $3 AND state = $4`, next, expl, d.tenant, d.state); err != nil {
			return moved, err
		}
		if err := SyncTenantToSubscription(d.tenant, "saas-sweep"); err != nil {
			return moved, err
		}
		moved++
	}
	return moved, nil
}

// StartSaaSBillingWorker runs EnforceSaaSBilling on a timer, on the
// scheduler leader only.
func StartSaaSBillingWorker(ctx context.Context, interval time.Duration) {
	ticker := time.NewTicker(interval)
	go func() {
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				if !IsSchedulerLeader() || db.DB == nil {
					continue
				}
				if n, err := EnforceSaaSBilling(); err != nil {
					log.Printf("[SAAS] billing sweep: %v", err)
				} else if n > 0 {
					log.Printf("[SAAS] billing sweep moved %d subscription(s)", n)
				}
				if _, err := SnapshotSaaSUsageAll(); err != nil {
					log.Printf("[SAAS] usage snapshot: %v", err)
				}
				if n, err := IssuePendingSaaSInvoices(); err != nil {
					log.Printf("[SAAS] pending invoices: %v", err)
				} else if n > 0 {
					log.Printf("[SAAS] numbered %d pending invoice(s)", n)
				}
			}
		}
	}()
}

// ---------------------------------------------------------------------------
// Reconciliation
// ---------------------------------------------------------------------------

// SaaSDrift is one difference reconciliation found.
type SaaSDrift struct {
	TenantID string `json:"tenant_id"`
	Kind     string `json:"kind"` // modules | provider
	Detail   string `json:"detail"`
	Fixed    bool   `json:"fixed"`
}

var saasProviderEvent = map[string]string{
	"active": "subscription.activated", "pending": "subscription.pending", "halted": "subscription.halted",
	"cancelled": "subscription.cancelled", "completed": "subscription.completed",
}

var saasProviderState = map[string]string{
	"active": SaaSStateActive, "pending": SaaSStatePastDue, "halted": SaaSStateSuspended,
	"cancelled": SaaSStateCancelled, "completed": SaaSStateCancelled,
}

// ReconcileSaaSSubscriptions compares every subscription with what the client
// really has (modules, lifecycle) and, when ctx allows outside calls, with
// Razorpay's own record. Drift in modules is fixed; a provider status that
// differs is applied through the same event path as a webhook, so it obeys
// the same ordering and idempotency.
func ReconcileSaaSSubscriptions(ctx context.Context, withProvider bool) ([]SaaSDrift, error) {
	rows, err := db.DB.Query(`SELECT tenant_id FROM public.saas_subscriptions ORDER BY tenant_id`)
	if err != nil {
		return nil, err
	}
	var tenants []string
	for rows.Next() {
		var t string
		if err := rows.Scan(&t); err != nil {
			rows.Close()
			return nil, err
		}
		tenants = append(tenants, t)
	}
	rows.Close()
	var out []SaaSDrift
	for _, t := range tenants {
		sub, err := GetSaaSSubscription(t)
		if err != nil || sub == nil {
			continue
		}
		if withProvider && sub.RazorpayID != "" && sub.State != SaaSStatePending {
			res, err := razorpayCall(ctx, saasBillingTenant(), "GET", "/v1/subscriptions/"+sub.RazorpayID, nil)
			if err != nil {
				out = append(out, SaaSDrift{TenantID: t, Kind: "provider", Detail: "could not fetch: " + err.Error()})
			} else if status, _ := res["status"].(string); saasProviderState[status] != "" && saasProviderState[status] != sub.State {
				d := SaaSDrift{TenantID: t, Kind: "provider", Detail: fmt.Sprintf("Razorpay says %s, we say %s", status, sub.State)}
				synth, _ := json.Marshal(map[string]interface{}{
					"event": saasProviderEvent[status], "created_at": saasNow().Unix(),
					"payload": map[string]interface{}{"subscription": map[string]interface{}{"entity": map[string]interface{}{
						"id": sub.RazorpayID, "status": status, "current_end": res["current_end"]}}},
				})
				if _, err := HandleSaaSSubscriptionWebhook(fmt.Sprintf("reconcile:%s:%s:%v", sub.RazorpayID, status, res["current_end"]), synth); err == nil {
					d.Fixed = true
				}
				out = append(out, d)
			}
		}
		before, _ := GetSaaSSubscription(t)
		drift, why := subscriptionDrift(t, before)
		if drift {
			d := SaaSDrift{TenantID: t, Kind: "modules", Detail: why}
			if err := SyncTenantToSubscription(t, "saas-reconcile"); err == nil {
				d.Fixed = true
			}
			out = append(out, d)
		}
	}
	return out, nil
}

// subscriptionDrift says whether the client's modules or lifecycle disagree
// with what its subscription allows.
func subscriptionDrift(tenantID string, sub *SaaSSubscription) (bool, string) {
	if sub == nil {
		return false, ""
	}
	life, err := GetTenantLifecycle(tenantID)
	if err != nil {
		return false, ""
	}
	allowed := saasAccessAllowed(sub, saasNow())
	if allowed {
		if sub.BillingSuspend && life.Status == TenantStatusSuspended {
			return true, "paid up but still suspended"
		}
		plan, err := GetSaaSPlan(sub.PlanKey)
		schema, serr := db.GetTenantSchema(tenantID)
		if err == nil && serr == nil {
			if now, _, e := enabledOptionalModules(db.DB, schema); e == nil && !sameModuleSet(now, saasSortedKeys(targetModules(plan.PackageKeys))) {
				return true, "modules differ from plan " + sub.PlanKey
			}
		}
		return false, ""
	}
	if sub.State != SaaSStatePending && life.Status == TenantStatusActive {
		return true, "not paid (" + sub.State + ") but still active"
	}
	return false, ""
}
