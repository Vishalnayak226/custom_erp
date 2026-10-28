package engines

import (
	"context"
	"crypto/md5"
	"custom_erp/db"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Stage 101.9 - the send gate (the blueprint's M14, M36, M37 budgets): the one
// place every marketing, journey, feedback, reminder and store-staff message
// passes. Whoever wants a customer messaged queues a crm_sends row - one per
// run x customer x channel (a unique key refuses a second) - and the CRM
// worker decides it right before the provider, in this order:
//
//	 1 stopped / ended   the campaign was stopped, the journey ended
//	 2 expired           past the message's own end
//	 3 duplicate         the customer was merged and the kept record has it
//	 4 erased            the customer was erased
//	 5 consent           consent now (crmEffectiveConsent), not at queue time
//	 6 hold-out          in the campaign's hold-out group (101.3.5)
//	 7 contact/template  an address, an active sender, a ready template,
//	                     every required value (TEMPLATE_DATA_MISSING), the
//	                     WhatsApp reply window for free text, DLT IDs for SMS
//	 8 goal met          the customer already did what the campaign wanted
//	 9 quiet hours       marketing waits until morning, or expires
//	10 limits            per-customer marketing limits (D8), taken atomically
//	11 budget            the run's budget, reserved from an estimate
//
// then, outside production or for a test send, only allowlisted contacts.
// Every message that does not go keeps its reason code. A message is marked
// Sending (committed) before the provider is called, so a crash between the
// two leaves Sending -> Unknown, never a blind resend (101.9.3).

// CRM send statuses.
const (
	crmSendQueued    = "Queued"
	crmSendDeferred  = "Deferred"
	crmSendSending   = "Sending"
	crmSendAccepted  = "Accepted"
	crmSendDelivered = "Delivered"
	crmSendRead      = "Read"
	crmSendFailed    = "Failed"
	crmSendUnknown   = "Unknown"
	crmSendSuppress  = "Suppressed"
	crmSendExpired   = "Expired"
	crmSendCancelled = "Cancelled"
	crmSendHeldOut   = "HeldOut"
	crmSendSimulated = "Simulated"
)

// crmCountedStatuses are sends that used a contact slot and budget.
const crmCountedStatusesSQL = `('Sending', 'Accepted', 'Delivered', 'Read', 'Unknown', 'Simulated')`

// CRMReasonLabels explains each reason code on screen.
var CRMReasonLabels = map[string]string{
	"CAMPAIGN_STOPPED":      "The campaign was stopped",
	"JOURNEY_ENDED":         "The journey ended for this customer",
	"EXPIRED":               "It was not sent before its end time",
	"DUPLICATE":             "The same message was already going to this customer",
	"MERGED_DUPLICATE":      "The same message was waiting for the customer kept in a merge",
	"ERASED":                "The customer was erased",
	"GLOBAL_OPT_OUT":        "The customer stopped all messages",
	"CHANNEL_OPT_OUT":       "The customer stopped this channel",
	"TOPIC_OPT_OUT":         "The customer said no to this kind of message",
	"NO_CONSENT":            "No recorded consent for offers on this channel",
	"CONSENT_WITHDRAWN":     "Consent was withdrawn before it was sent",
	"SUPPRESSED_ERASED":     "This address belonged to an erased customer",
	"SUPPRESSED_BOUNCED":    "This email address bounced",
	"SUPPRESSED_COMPLAINED": "This address complained (marked as spam)",
	"HOLD_OUT":              "In the campaign's hold-out group (not sent, on purpose)",
	"NO_CONTACT":            "No phone number or email to send to",
	"NO_SENDER":             "No active sender for this channel",
	"TEMPLATE_NOT_READY":    "The template is not ready (switched off, not approved, no DLT ID)",
	"TEMPLATE_DATA_MISSING": "A value the template needs was blank",
	"WINDOW_CLOSED":         "WhatsApp free text needs the customer to have written in the last 24 hours - use a template",
	"DLT_INCOMPLETE":        "The SMS sender or template is missing its DLT IDs",
	"GOAL_MET":              "The customer already bought - no need to send",
	"QUIET_HOURS":           "Waiting for quiet hours to end (offers only)",
	"MIN_GAP":               "Waiting for the gap between offers to pass",
	"QUIET_HOURS_EXPIRED":   "Quiet hours lasted past the message's end time",
	"CAP_REACHED":           "The customer already had the most offers allowed for now",
	"BUDGET_EXHAUSTED":      "The campaign's budget was used up",
	"NOT_ALLOWLISTED":       "Test send or test server: only allowlisted numbers and emails",
	"PROVIDER_UNREACHABLE":  "The provider could not be reached",
	"PROVIDER_REFUSED":      "The provider refused it",
}

// CRMSendRequest asks for one message.
type CRMSendRequest struct {
	RunKind         string // campaign, journey, staff, feedback, reminder, test
	RunID           string // the run, journey step, or a fresh id
	CustomerID      string
	Channel         string
	Purpose         string
	TemplateID      string
	TemplateVersion int    // 0 = current at queue time
	Body            string // free text (staff); used when there is no template
	Vars            map[string]string
	DelaySeconds    int
	ExpiresInHours  int
	ExpiresAtSQL    string // optional SQL expression for the end (a campaign's end)
	CreatedBy       string
	Status          string // "" = Queued; HeldOut for a hold-out row
	ReasonCode      string
}

type crmExecQuerier interface {
	Exec(string, ...interface{}) (sql.Result, error)
	QueryRow(string, ...interface{}) *sql.Row
}

// crmQueueSend writes one send row (once per run x customer x channel) and
// returns its id and whether this call created it.
func crmQueueSend(q crmExecQuerier, schema string, r CRMSendRequest) (string, bool, error) {
	r.Channel, r.Purpose = crmChannel(r.Channel), crmPurpose(r.Purpose)
	if r.Channel == "" || r.Purpose == "" || strings.TrimSpace(r.CustomerID) == "" || r.RunKind == "" || r.RunID == "" {
		return "", false, crmErr(CRMCodeMessage, "a message needs a customer, a channel and what it is for")
	}
	if r.TemplateID != "" && r.TemplateVersion <= 0 {
		var cur int
		if err := q.QueryRow(fmt.Sprintf(`SELECT current_version FROM %s.crm_templates WHERE id = $1`, schema), r.TemplateID).Scan(&cur); err != nil {
			return "", false, crmNotFound("template", r.TemplateID)
		}
		r.TemplateVersion = cur
	}
	status := firstNonEmpty(r.Status, crmSendQueued)
	expires := "NULL"
	switch {
	case r.ExpiresAtSQL != "":
		expires = r.ExpiresAtSQL
	case r.ExpiresInHours > 0:
		expires = fmt.Sprintf("LOCALTIMESTAMP + interval '%d hours'", r.ExpiresInHours)
	}
	id := NewDocID("SND")
	var got string
	var tv interface{}
	if r.TemplateVersion > 0 {
		tv = r.TemplateVersion
	}
	err := q.QueryRow(fmt.Sprintf(`INSERT INTO %s.crm_sends (id, run_kind, run_id, customer_id, channel, purpose, template_id, template_version, body_override, vars,
		status, reason_code, due_at, expires_at, created_by, history, final_at)
		VALUES ($1, $2, $3, $4, $5, $6, NULLIF($7, ''), $8, NULLIF($9, ''), $10, $11::text, NULLIF($12, ''), LOCALTIMESTAMP + make_interval(secs => $13), %s, $14, $15,
		        CASE WHEN $11::text = 'Queued' THEN NULL ELSE LOCALTIMESTAMP END)
		ON CONFLICT (run_kind, run_id, customer_id, channel) DO NOTHING RETURNING id`, schema, expires),
		id, r.RunKind, r.RunID, r.CustomerID, r.Channel, r.Purpose, r.TemplateID, tv, r.Body, crmJSON(r.Vars), status, r.ReasonCode,
		float64(r.DelaySeconds), r.CreatedBy, crmHistoryEntry(status, firstNonEmpty(r.ReasonCode, "queued"))).Scan(&got)
	if errors.Is(err, sql.ErrNoRows) {
		if err := q.QueryRow(fmt.Sprintf(`SELECT id FROM %s.crm_sends WHERE run_kind = $1 AND run_id = $2 AND customer_id = $3 AND channel = $4`, schema),
			r.RunKind, r.RunID, r.CustomerID, r.Channel).Scan(&got); err != nil {
			return "", false, err
		}
		return got, false, nil
	}
	if err != nil {
		return "", false, err
	}
	return got, true, nil
}

// --- the worker -----------------------------------------------------------------

// crmTicks run on every CRM worker tick, per client schema: each part of the
// CRM (journeys, campaigns, feedback, loyalty) registers its own.
var (
	crmTicksMu sync.Mutex
	crmTicks   []struct {
		name string
		fn   func(schema string)
	}
	crmDailyTicks []struct {
		name string
		fn   func(schema string)
	}
)

func registerCRMTick(name string, fn func(schema string)) {
	crmTicksMu.Lock()
	defer crmTicksMu.Unlock()
	crmTicks = append(crmTicks, struct {
		name string
		fn   func(schema string)
	}{name, fn})
}

// registerCRMDaily runs fn once a day per schema (first tick after midnight
// in the client's time zone).
func registerCRMDaily(name string, fn func(schema string)) {
	crmTicksMu.Lock()
	defer crmTicksMu.Unlock()
	crmDailyTicks = append(crmDailyTicks, struct {
		name string
		fn   func(schema string)
	}{name, fn})
}

var crmDailyDone sync.Map // schema|name -> "2006-01-02"

// StartCRMWorker runs the CRM: events into journeys, due campaign runs, the
// send gate, unknown-outcome sweeps, alerts and the daily jobs.
func StartCRMWorker(ctx context.Context, interval time.Duration) {
	ticker := time.NewTicker(interval)
	go func() {
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				if !IsSchedulerLeader() { // 134.7: one server runs each sweep
					continue
				}
				if db.DB == nil {
					continue
				}
				schemas, err := listTenantSchemas("crm_loyalty")
				if err != nil {
					log.Printf("[CRM] failed to list tenant schemas: %v", err)
					continue
				}
				for _, schema := range schemas {
					if ctx.Err() != nil {
						return
					}
					RunCRMTickForSchema(schema)
				}
			}
		}
	}()
}

// RunCRMTickForSchema is one worker pass over one client (tests call it).
func RunCRMTickForSchema(schema string) {
	if !crmReady(schema) {
		return
	}
	crmTicksMu.Lock()
	ticks := append(crmTicks[:0:0], crmTicks...)
	daily := append(crmDailyTicks[:0:0], crmDailyTicks...)
	crmTicksMu.Unlock()
	safely := func(name string, fn func(string)) {
		defer func() {
			if r := recover(); r != nil {
				log.Printf("[CRM] %s panicked in %s: %v", name, schema, r)
			}
		}()
		fn(schema)
	}
	for _, t := range ticks {
		safely(t.name, t.fn)
	}
	today := time.Now().In(crmLocation(schema)).Format("2006-01-02")
	for _, t := range daily {
		key := schema + "|" + t.name
		if last, ok := crmDailyDone.Load(key); ok && last.(string) == today {
			continue
		}
		crmDailyDone.Store(key, today)
		safely(t.name, t.fn)
	}
	safely("dispatch", func(s string) { crmDispatchDue(s, 200) })
	safely("unknown sweep", crmSweepUnknown)
	safely("alerts", crmCheckAlerts)
}

func init() {
	registerCRMDaily("message retention", crmApplyRetention)
}

// crmDispatchDue decides and sends up to limit due messages.
func crmDispatchDue(schema string, limit int) {
	for i := 0; i < limit; i++ {
		more, err := crmDispatchNext(schema, "")
		if err != nil {
			log.Printf("[CRM] dispatch in %s: %v", schema, err)
			return
		}
		if !more {
			return
		}
	}
}

type crmSendRow struct {
	ID, RunKind, RunID, CustomerID, Channel, Purpose, TemplateID, Body, CreatedBy string
	TemplateVersion                                                               int
	Vars                                                                          map[string]string
	Expired                                                                       bool
	Attempts                                                                      int
}

// crmDispatchNext claims one due message (or the one named) and runs it
// through the gate. Returns whether one was found.
func crmDispatchNext(schema, onlyID string) (bool, error) {
	tx, err := db.DB.Begin()
	if err != nil {
		return false, err
	}
	defer tx.Rollback()
	var s crmSendRow
	var tid, body sql.NullString
	var tv sql.NullInt64
	var vars []byte
	filter := `s.status IN ('Queued', 'Deferred') AND s.due_at <= LOCALTIMESTAMP
		AND NOT EXISTS (SELECT 1 FROM %[1]s.crm_campaign_runs r WHERE s.run_kind = 'campaign' AND r.id = s.run_id AND r.status = 'Paused')`
	args := []interface{}{}
	if onlyID != "" {
		filter = `s.id = $1 AND s.status IN ('Queued', 'Deferred')`
		args = append(args, onlyID)
	}
	err = tx.QueryRow(fmt.Sprintf(`SELECT s.id, s.run_kind, s.run_id, s.customer_id, s.channel, s.purpose, s.template_id, s.template_version, s.body_override, s.vars,
		COALESCE(s.created_by, ''), (s.expires_at IS NOT NULL AND s.expires_at < LOCALTIMESTAMP), s.attempts
		FROM %[1]s.crm_sends s WHERE `+filter+` ORDER BY s.due_at LIMIT 1 FOR UPDATE SKIP LOCKED`, schema), args...).
		Scan(&s.ID, &s.RunKind, &s.RunID, &s.CustomerID, &s.Channel, &s.Purpose, &tid, &tv, &body, &vars, &s.CreatedBy, &s.Expired, &s.Attempts)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	s.TemplateID, s.Body, s.TemplateVersion = tid.String, body.String, int(tv.Int64)
	_ = json.Unmarshal(vars, &s.Vars)
	if s.Vars == nil {
		s.Vars = map[string]string{}
	}
	d, err := crmDecide(tx, schema, &s)
	if err != nil {
		return true, err
	}
	if d.final != "" {
		if err := crmMarkTx(tx, schema, s.ID, d.final, d.code, d.detail, nil); err != nil {
			return true, err
		}
		return true, tx.Commit()
	}
	if d.deferSeconds > 0 {
		if _, err := tx.Exec(fmt.Sprintf(`UPDATE %s.crm_sends SET status = 'Deferred', due_at = LOCALTIMESTAMP + make_interval(secs => $2), reason_code = $3, reason_detail = $4,
			history = history || $5::jsonb WHERE id = $1`, schema), s.ID, float64(d.deferSeconds), d.code, d.detail, crmHistoryEntry("Deferred", d.code+": "+d.detail)); err != nil {
			return true, err
		}
		return true, tx.Commit()
	}
	// Passed: record what goes, as Sending, before the provider sees it.
	if _, err := tx.Exec(fmt.Sprintf(`UPDATE %s.crm_sends SET status = 'Sending', reason_code = NULL, reason_detail = NULL, decided_at = LOCALTIMESTAMP, sent_at = LOCALTIMESTAMP,
		rendered_subject = NULLIF($2, ''), rendered_body = $3, parts = $4, cost_estimate = $5, contact_hash = $6, contact_masked = $7, sender_id = $8, provider = $9,
		template_version = COALESCE(template_version, NULLIF($10, 0)), attempts = attempts + 1, history = history || $11::jsonb WHERE id = $1`, schema),
		s.ID, d.dispatch.Subject, d.dispatch.Body, d.parts, d.estimate, d.contactHash, d.contactMasked, d.sender.ID, d.sender.Provider, d.templateVersion,
		crmHistoryEntry("Sending", "passed the gate; handed to "+d.sender.Provider)); err != nil {
		return true, err
	}
	if err := tx.Commit(); err != nil {
		return true, err
	}
	crmHandOver(schema, &s, d)
	return true, nil
}

type crmDecision struct {
	final, code, detail string
	deferSeconds        int
	dispatch            crmDispatch
	sender              *crmSenderRow
	parts               int
	estimate            float64
	contactHash         string
	contactMasked       string
	templateVersion     int
}

func crmSuppressed(final, code string) crmDecision {
	return crmDecision{final: final, code: code, detail: CRMReasonLabels[code]}
}

// crmDecide runs the ordered checks inside the claiming transaction.
func crmDecide(tx *sql.Tx, schema string, s *crmSendRow) (crmDecision, error) {
	// 1 stopped / ended
	switch s.RunKind {
	case "campaign":
		var st string
		if err := tx.QueryRow(fmt.Sprintf(`SELECT status FROM %s.crm_campaign_runs WHERE id = $1`, schema), s.RunID).Scan(&st); err == nil && (st == "Stopped" || st == "Cancelled") {
			return crmSuppressed(crmSendCancelled, "CAMPAIGN_STOPPED"), nil
		}
	case "journey":
		runID := s.RunID
		if i := strings.LastIndex(runID, ":"); i > 0 {
			runID = runID[:i]
		}
		var st, why string
		if err := tx.QueryRow(fmt.Sprintf(`SELECT state, COALESCE(exit_reason, '') FROM %s.crm_journey_runs WHERE id = $1`, schema), runID).Scan(&st, &why); err == nil && st != "Active" && st != "Waiting" {
			if why == "goal" {
				return crmSuppressed(crmSendSuppress, "GOAL_MET"), nil
			}
			return crmSuppressed(crmSendCancelled, "JOURNEY_ENDED"), nil
		}
	}
	// 2 expired
	if s.Expired {
		return crmSuppressed(crmSendExpired, "EXPIRED"), nil
	}
	c, err := crmLoadCustomer(schema, s.CustomerID)
	if errors.Is(err, ErrCRMNotFound) {
		return crmSuppressed(crmSendCancelled, "NO_CONTACT"), nil
	}
	if err != nil {
		return crmDecision{}, err
	}
	// 3 duplicate (merged away: the kept record either has it or takes it)
	if c.Status == "Merged" {
		survivor := strField(c.Data, "merged_into")
		var exists bool
		_ = tx.QueryRow(fmt.Sprintf(`SELECT EXISTS(SELECT 1 FROM %s.crm_sends WHERE run_kind = $1 AND run_id = $2 AND customer_id = $3 AND channel = $4)`, schema),
			s.RunKind, s.RunID, survivor, s.Channel).Scan(&exists)
		if exists || survivor == "" {
			return crmSuppressed(crmSendCancelled, "DUPLICATE"), nil
		}
		if _, err := tx.Exec(fmt.Sprintf(`UPDATE %s.crm_sends SET customer_id = $2 WHERE id = $1`, schema), s.ID, survivor); err != nil {
			return crmDecision{}, err
		}
		if c, err = crmLoadCustomer(schema, survivor); err != nil {
			return crmSuppressed(crmSendCancelled, "DUPLICATE"), nil
		}
		s.CustomerID = c.ID
	}
	// 4 erased
	if crmCustomerErased(c) {
		return crmSuppressed(crmSendCancelled, "ERASED"), nil
	}
	// 5 consent, now
	states, err := crmConsentStates(schema, c)
	if err != nil {
		return crmDecision{}, err
	}
	for _, st := range states {
		if st.Channel == s.Channel && st.Purpose == s.Purpose && !st.Reachable {
			return crmSuppressed(crmSendSuppress, st.Reason), nil
		}
	}
	// 6 hold-out (rows are normally created HeldOut; this keeps a rerun honest)
	var run *crmRunInfo
	if s.RunKind == "campaign" {
		if run, err = crmLoadRunInfo(tx, schema, s.RunID); err != nil {
			return crmDecision{}, err
		}
		if run != nil && run.HoldoutPct > 0 && crmInHoldout(run.CampaignID, c.ID, run.HoldoutPct) {
			return crmSuppressed(crmSendHeldOut, "HOLD_OUT"), nil
		}
	}
	// 7 contact, sender, template, values
	to, masked, hash, ok := crmContact(schema, c.Data, s.Channel)
	if !ok {
		return crmSuppressed(crmSendSuppress, "NO_CONTACT"), nil
	}
	sender, err := crmActiveSender(schema, s.Channel)
	if err != nil {
		return crmDecision{}, err
	}
	if sender == nil {
		return crmSuppressed(crmSendSuppress, "NO_SENDER"), nil
	}
	d := crmDecision{sender: sender, contactHash: hash, contactMasked: masked}
	d.dispatch = crmDispatch{SendID: s.ID, Channel: s.Channel, Purpose: s.Purpose, To: to, FromName: crmBusinessName(schema)}
	vals := crmCustomerVars(schema, c)
	for k, v := range s.Vars {
		vals[k] = v
	}
	if s.Purpose == CRMPurposeMarketing || strings.Contains(s.Body, "link.preferences") {
		token, err := crmIssuePrefToken(schema, c.ID, s.ID)
		if err != nil {
			return crmDecision{}, err
		}
		vals["link.preferences"] = crmPublicLink(schema, "/p/", token)
		if s.Channel == CRMChannelEmail {
			d.dispatch.UnsubscribeURL = crmPublicLink(schema, "/api/v1/crm/public/unsubscribe", token)
		}
	}
	if s.TemplateID != "" {
		t, err := crmGetTemplate(schema, s.TemplateID)
		if err != nil {
			return crmSuppressed(crmSendSuppress, "TEMPLATE_NOT_READY"), nil
		}
		version := s.TemplateVersion
		if version <= 0 {
			version = t.CurrentVersion
		}
		v, err := crmTemplateVersion(schema, t.ID, version)
		if err != nil || t.Status != "Active" {
			return crmSuppressed(crmSendSuppress, "TEMPLATE_NOT_READY"), nil
		}
		d.templateVersion = version
		if s.Channel == CRMChannelWhatsApp {
			if v.WATemplateName == "" {
				d.dispatch.FreeForm = true
			} else if !strings.EqualFold(v.WAStatus, "APPROVED") && sender.Provider == CRMProviderWhatsApp {
				return crmSuppressed(crmSendSuppress, "TEMPLATE_NOT_READY"), nil
			}
			d.dispatch.WATemplate, d.dispatch.WALanguage = v.WATemplateName, firstNonEmpty(v.WALanguage, t.Language)
		}
		if s.Channel == CRMChannelSMS {
			d.dispatch.DLTTemplateID = v.DLTTemplateID
		}
		r := crmRender(v, vals)
		if len(r.Missing) > 0 {
			detail := "blank: " + strings.Join(r.Missing, ", ")
			for _, m := range r.Missing {
				if strings.HasPrefix(m, "link.") {
					detail += " (links need the client's web address: Configuration > CRM > Web address for customer links)"
					break
				}
			}
			return crmDecision{final: crmSendSuppress, code: "TEMPLATE_DATA_MISSING", detail: detail}, nil
		}
		d.dispatch.Subject, d.dispatch.Body, d.dispatch.Params, d.parts = r.Subject, r.Body, r.Params, r.Parts
	} else {
		if strings.TrimSpace(s.Body) == "" {
			return crmSuppressed(crmSendSuppress, "TEMPLATE_DATA_MISSING"), nil
		}
		d.dispatch.Body, d.dispatch.FreeForm = s.Body, true
		d.dispatch.Subject = firstNonEmpty(s.Vars["subject"], "A message from "+d.dispatch.FromName)
		d.parts, _ = crmSMSParts(s.Body)
		if s.Channel == CRMChannelSMS && sender.Provider == CRMProviderSMS {
			// DLT: every SMS to an Indian number must match a registered template.
			return crmSuppressed(crmSendSuppress, "DLT_INCOMPLETE"), nil
		}
	}
	if s.Channel == CRMChannelWhatsApp && d.dispatch.FreeForm && sender.Provider == CRMProviderWhatsApp && !crmWhatsAppWindowOpen(schema, hash) {
		return crmSuppressed(crmSendSuppress, "WINDOW_CLOSED"), nil
	}
	if s.Channel == CRMChannelSMS && sender.Provider == CRMProviderSMS && (d.dispatch.DLTTemplateID == "" || sender.cfg("entity_value") == "" || sender.cfg("sender_value") == "") {
		return crmSuppressed(crmSendSuppress, "DLT_INCOMPLETE"), nil
	}
	if s.Channel == CRMChannelEmail && s.Purpose == CRMPurposeMarketing && vals["link.preferences"] != "" && !strings.Contains(d.dispatch.Body, vals["link.preferences"]) {
		d.dispatch.Body += "\n\n--\nTo change what you receive or stop these emails: " + vals["link.preferences"]
	}
	// 8 goal met
	if run != nil && run.SkipIfBought {
		var bought bool
		_ = tx.QueryRow(fmt.Sprintf(`SELECT EXISTS(SELECT 1 FROM %s.documents WHERE doctype = 'POSCart' AND status = 'Paid' AND data->>'customer_id' = $1 AND created_at >= $2)`, schema),
			c.ID, run.StartedAt).Scan(&bought)
		if bought {
			return crmSuppressed(crmSendSuppress, "GOAL_MET"), nil
		}
	}
	if s.Purpose == CRMPurposeMarketing {
		// 9 quiet hours
		if wait := crmQuietWait(schema, time.Now()); wait > 0 {
			var pastEnd bool
			_ = tx.QueryRow(fmt.Sprintf(`SELECT expires_at IS NOT NULL AND expires_at < LOCALTIMESTAMP + make_interval(secs => $2) FROM %s.crm_sends WHERE id = $1`, schema),
				s.ID, wait.Seconds()).Scan(&pastEnd)
			if pastEnd {
				return crmSuppressed(crmSendExpired, "QUIET_HOURS_EXPIRED"), nil
			}
			return crmDecision{deferSeconds: int(wait.Seconds()) + 1, code: "QUIET_HOURS", detail: "goes at " + crmResumeAt(schema, wait)}, nil
		}
		// 10 limits, held under the customer's lock so two workers cannot
		// both take the last slot
		if _, err := tx.Exec(`SELECT pg_advisory_xact_lock(hashtext($1))`, "crmcap:"+schema+":"+c.ID); err != nil {
			return crmDecision{}, err
		}
		var day, week int
		var gapLeft sql.NullFloat64
		if err := tx.QueryRow(fmt.Sprintf(`SELECT
			COUNT(*) FILTER (WHERE sent_at > LOCALTIMESTAMP - interval '24 hours'),
			COUNT(*) FILTER (WHERE sent_at > LOCALTIMESTAMP - interval '7 days'),
			EXTRACT(EPOCH FROM (MAX(sent_at) + make_interval(hours => $3) - LOCALTIMESTAMP))
			FROM %s.crm_sends WHERE customer_id = $1 AND purpose = 'marketing' AND id <> $2 AND status IN `+crmCountedStatusesSQL+` AND sent_at > LOCALTIMESTAMP - interval '7 days'`, schema),
			c.ID, s.ID, GetSettingIntForSchema(schema, "crm.min_gap_hours")).Scan(&day, &week, &gapLeft); err != nil {
			return crmDecision{}, err
		}
		if lim := GetSettingIntForSchema(schema, "crm.limit_per_day"); day >= lim {
			return crmDecision{final: crmSendSuppress, code: "CAP_REACHED", detail: fmt.Sprintf("%d offer(s) in the last 24 hours (limit %d)", day, lim)}, nil
		}
		if lim := GetSettingIntForSchema(schema, "crm.limit_per_week"); week >= lim {
			return crmDecision{final: crmSendSuppress, code: "CAP_REACHED", detail: fmt.Sprintf("%d offer(s) in the last 7 days (limit %d)", week, lim)}, nil
		}
		if gapLeft.Valid && gapLeft.Float64 > 0 {
			var pastEnd bool
			_ = tx.QueryRow(fmt.Sprintf(`SELECT expires_at IS NOT NULL AND expires_at < LOCALTIMESTAMP + make_interval(secs => $2) FROM %s.crm_sends WHERE id = $1`, schema),
				s.ID, gapLeft.Float64).Scan(&pastEnd)
			if pastEnd {
				return crmDecision{final: crmSendSuppress, code: "CAP_REACHED", detail: "the gap between offers lasted past the message's end"}, nil
			}
			return crmDecision{deferSeconds: int(gapLeft.Float64) + 1, code: "MIN_GAP", detail: "goes at " + crmResumeAt(schema, time.Duration(gapLeft.Float64*float64(time.Second)))}, nil
		}
	}
	// 11 budget
	d.estimate = crmEstimateCost(schema, s.Channel, d.parts)
	if run != nil && run.Budget.Valid {
		if _, err := tx.Exec(`SELECT pg_advisory_xact_lock(hashtext($1))`, "crmbudget:"+schema+":"+s.RunID); err != nil {
			return crmDecision{}, err
		}
		var used float64
		if err := tx.QueryRow(fmt.Sprintf(`SELECT COALESCE(SUM(COALESCE(cost_actual, cost_estimate, 0)), 0) FROM %s.crm_sends WHERE run_kind = 'campaign' AND run_id = $1 AND status IN `+crmCountedStatusesSQL, schema),
			s.RunID).Scan(&used); err != nil {
			return crmDecision{}, err
		}
		if used+d.estimate > run.Budget.Float64+0.00001 {
			return crmDecision{final: crmSendSuppress, code: "BUDGET_EXHAUSTED", detail: fmt.Sprintf("%.2f of %.2f used", used, run.Budget.Float64)}, nil
		}
	}
	// Test sends, and every send from a server that is not production, reach
	// only the allowlist (101.9.4).
	if s.RunKind == "test" || os.Getenv("ENV") != "production" {
		if !crmAllowlisted(schema, to) {
			return crmSuppressed(crmSendSuppress, "NOT_ALLOWLISTED"), nil
		}
	}
	return d, nil
}

type crmRunInfo struct {
	CampaignID   string
	HoldoutPct   int
	Budget       sql.NullFloat64
	SkipIfBought bool
	StartedAt    time.Time
}

func crmLoadRunInfo(q crmExecQuerier, schema, runID string) (*crmRunInfo, error) {
	var r crmRunInfo
	var opts []byte
	var started sql.NullTime
	err := q.QueryRow(fmt.Sprintf(`SELECT campaign_id, holdout_pct, budget, options, COALESCE(started_at, created_at) FROM %s.crm_campaign_runs WHERE id = $1`, schema), runID).
		Scan(&r.CampaignID, &r.HoldoutPct, &r.Budget, &opts, &started)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var o map[string]interface{}
	_ = json.Unmarshal(opts, &o)
	r.SkipIfBought = truthy(o["skip_if_bought"])
	r.StartedAt = started.Time
	return &r, nil
}

// crmQuietWait is how long marketing must wait for quiet hours to end, or 0.
func crmQuietWait(schema string, now time.Time) time.Duration {
	start, okS := crmClock(GetSettingStringForSchema(schema, "crm.quiet_start"))
	end, okE := crmClock(GetSettingStringForSchema(schema, "crm.quiet_end"))
	if !okS || !okE || start == end {
		return 0
	}
	local := now.In(crmLocation(schema))
	mins := local.Hour()*60 + local.Minute()
	in := false
	if start > end {
		in = mins >= start || mins < end
	} else {
		in = mins >= start && mins < end
	}
	if !in {
		return 0
	}
	resume := time.Date(local.Year(), local.Month(), local.Day(), end/60, end%60, 0, 0, local.Location())
	if !resume.After(local) {
		resume = resume.AddDate(0, 0, 1)
	}
	return resume.Sub(local)
}

// crmResumeAt is when a held-back message goes, in the client's time zone.
func crmResumeAt(schema string, wait time.Duration) string {
	loc := crmLocation(schema)
	return time.Now().Add(wait).In(loc).Format("2 Jan 15:04") + " (" + loc.String() + ")"
}

// crmClock parses "HH:MM" into minutes after midnight.
func crmClock(s string) (int, bool) {
	t, err := time.Parse("15:04", strings.TrimSpace(s))
	if err != nil {
		return 0, false
	}
	return t.Hour()*60 + t.Minute(), true
}

func crmEstimateCost(schema, channel string, parts int) float64 {
	switch channel {
	case CRMChannelSMS:
		if parts < 1 {
			parts = 1
		}
		return float64(parts) * GetSettingFloatForSchema(schema, "crm.sms_cost_per_part")
	case CRMChannelWhatsApp:
		return GetSettingFloatForSchema(schema, "crm.whatsapp_cost")
	}
	return GetSettingFloatForSchema(schema, "crm.email_cost")
}

// crmAllowlisted reports whether a phone (E.164) or email is on the client's
// test allowlist.
func crmAllowlisted(schema, contact string) bool {
	contact = strings.ToLower(strings.TrimSpace(contact))
	digits := strings.TrimPrefix(contact, "+")
	for _, raw := range strings.Split(GetSettingStringForSchema(schema, "crm.test_allowlist"), ",") {
		a := strings.ToLower(strings.TrimSpace(raw))
		if a == "" {
			continue
		}
		if a == contact {
			return true
		}
		ad := strings.Map(func(r rune) rune {
			if r >= '0' && r <= '9' {
				return r
			}
			return -1
		}, a)
		if !strings.Contains(a, "@") && len(ad) >= 8 && strings.HasSuffix(digits, ad) {
			return true
		}
	}
	return false
}

// crmWhatsAppWindowOpen: WhatsApp allows free text only within 24 hours of
// the customer's last message to the business.
func crmWhatsAppWindowOpen(schema, phoneHash string) bool {
	var open bool
	_ = db.DB.QueryRow(fmt.Sprintf(`SELECT EXISTS(SELECT 1 FROM %s.crm_conversations WHERE channel = 'WhatsApp' AND phone_hash = $1 AND last_inbound_at > LOCALTIMESTAMP - interval '24 hours')`, schema), phoneHash).Scan(&open)
	return open
}

func crmMarkTx(tx *sql.Tx, schema, id, status, code, detail string, extra map[string]interface{}) error {
	_, err := tx.Exec(fmt.Sprintf(`UPDATE %s.crm_sends SET status = $2, reason_code = NULLIF($3, ''), reason_detail = NULLIF($4, ''), decided_at = LOCALTIMESTAMP,
		final_at = LOCALTIMESTAMP, history = history || $5::jsonb WHERE id = $1`, schema), id, status, code, detail, crmHistoryEntry(status, firstNonEmpty(code+": "+detail, status)))
	return err
}

// crmHandOver calls the provider for a message already marked Sending and
// records what it said.
func crmHandOver(schema string, s *crmSendRow, d crmDecision) {
	if d.sender.Provider != CRMProviderMock {
		if sb, _ := IsSandboxSchema(schema); sb {
			_ = crmRecordOutcome(schema, s.ID, crmSendSimulated, "", "simulated - a sandbox client does not send to real providers", nil, "system")
			return
		}
		if ok, reason := OutsideCallsAllowed(schema); !ok { // 49.8.7a: the client's own switch too
			_ = crmRecordOutcome(schema, s.ID, crmSendSimulated, "", "simulated - "+reason, nil, "system")
			return
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 25*time.Second)
	defer cancel()
	res, err := crmDeliver(ctx, d.sender, d.dispatch)
	var notSent *errCRMNotSent
	var unknown *errCRMUnknown
	switch {
	case errors.As(err, &notSent):
		backoff := []int{60, 300, 900, 3600}
		if s.Attempts+1 >= 5 {
			_ = crmRecordOutcome(schema, s.ID, crmSendFailed, "", "PROVIDER_UNREACHABLE: "+err.Error(), nil, "system")
			return
		}
		wait := backoff[s.Attempts%len(backoff)]
		_, _ = db.DB.Exec(fmt.Sprintf(`UPDATE %s.crm_sends SET status = 'Queued', due_at = LOCALTIMESTAMP + make_interval(secs => $2), reason_code = 'PROVIDER_UNREACHABLE',
			reason_detail = $3, history = history || $4::jsonb WHERE id = $1 AND status = 'Sending'`, schema), s.ID, float64(wait), err.Error(), crmHistoryEntry("Queued", "not sent, will try again: "+err.Error()))
	case errors.As(err, &unknown):
		_ = crmRecordOutcome(schema, s.ID, crmSendUnknown, "", err.Error(), nil, "system")
	case err != nil:
		_ = crmRecordOutcome(schema, s.ID, crmSendUnknown, "", err.Error(), nil, "system")
	default:
		state := res.State
		if state == "Failed" {
			_ = crmRecordOutcome(schema, s.ID, crmSendFailed, res.ProviderRef, "PROVIDER_REFUSED: "+res.Detail, res.Cost, "provider")
			return
		}
		_ = crmRecordOutcome(schema, s.ID, state, res.ProviderRef, res.Detail, res.Cost, "provider")
	}
}

var crmStatusRank = map[string]int{crmSendSending: 1, crmSendUnknown: 2, crmSendAccepted: 3, crmSendFailed: 4, crmSendDelivered: 5, crmSendRead: 6, crmSendSimulated: 7}

// crmRecordOutcome moves a sent message to what the provider said. It never
// goes backwards (Read stays Read); Failed can still become Delivered - a
// late delivery report is better evidence than a give-up.
func crmRecordOutcome(schema, sendID, status, ref, detail string, cost *float64, source string) error {
	tx, err := db.DB.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var cur string
	if err := tx.QueryRow(fmt.Sprintf(`SELECT status FROM %s.crm_sends WHERE id = $1 FOR UPDATE`, schema), sendID).Scan(&cur); err != nil {
		return err
	}
	if _, counted := crmStatusRank[cur]; !counted {
		return nil
	}
	if cur == status && ref == "" && cost == nil {
		return nil
	}
	if crmStatusRank[status] < crmStatusRank[cur] && !(cur == crmSendFailed && status == crmSendDelivered) && !(cur == crmSendUnknown && status == crmSendFailed) {
		status = cur
	}
	if cur == crmSendDelivered && status == crmSendFailed {
		status = cur
	}
	final := status == crmSendDelivered || status == crmSendRead || status == crmSendFailed || status == crmSendSimulated
	var c interface{}
	if cost != nil {
		c = *cost
	}
	if _, err := tx.Exec(fmt.Sprintf(`UPDATE %s.crm_sends SET status = $2::text, provider_ref = COALESCE(NULLIF($3, ''), provider_ref), reason_detail = NULLIF($4, ''),
		cost_actual = COALESCE($5, cost_actual), final_at = CASE WHEN $6 THEN LOCALTIMESTAMP ELSE final_at END,
		reason_code = CASE WHEN $2::text = 'Failed' THEN COALESCE(reason_code, 'PROVIDER_REFUSED') ELSE reason_code END,
		history = history || $7::jsonb WHERE id = $1`, schema), sendID, status, ref, detail, c, final, crmHistoryEntry(status, source+": "+detail)); err != nil {
		return err
	}
	return tx.Commit()
}

// crmSweepUnknown marks messages stuck in Sending as Unknown.
func crmSweepUnknown(schema string) {
	mins := GetSettingIntForSchema(schema, "crm.unknown_after_minutes")
	if mins < 1 {
		mins = 15
	}
	_, _ = db.DB.Exec(fmt.Sprintf(`UPDATE %s.crm_sends SET status = 'Unknown', reason_detail = 'no answer from the provider - it may or may not have arrived; not resent',
		history = history || $2::jsonb WHERE status = 'Sending' AND sent_at < LOCALTIMESTAMP - make_interval(mins => $1)`, schema), mins, crmHistoryEntry("Unknown", "no answer in time"))
}

// --- callbacks, manual resolution ------------------------------------------------

// crmFindSend finds the message a provider callback is about, by the
// provider's id or by our own send id echoed back.
func crmFindSend(schema, ref, sendID string) (string, error) {
	var id string
	err := db.DB.QueryRow(fmt.Sprintf(`SELECT id FROM %s.crm_sends WHERE (NULLIF($1, '') IS NOT NULL AND provider_ref = $1) OR (NULLIF($2, '') IS NOT NULL AND id = $2)
		ORDER BY (provider_ref = $1) DESC LIMIT 1`, schema), strings.TrimSpace(ref), strings.TrimSpace(sendID)).Scan(&id)
	return id, err
}

// crmApplyStatusCallback records a delivery report.
func crmApplyStatusCallback(schema, ref, sendID, rawStatus, detail string, cost *float64, source string) error {
	id, err := crmFindSend(schema, ref, sendID)
	if err != nil {
		return nil // a report for a message we never sent is ignored, not an error to the provider
	}
	status := normalizeProviderDeliveryState(rawStatus)
	switch strings.ToLower(strings.TrimSpace(rawStatus)) {
	case "read", "seen", "opened":
		status = crmSendRead
	case "sent", "accepted", "queued", "submitted", "enroute":
		status = crmSendAccepted
	}
	if status == "Delivered" {
		status = crmSendDelivered
	}
	return crmRecordOutcome(schema, id, status, ref, firstNonEmpty(detail, "provider reported "+strings.ToLower(rawStatus)), cost, source)
}

// ResolveCRMSend settles an Unknown message by hand (an operator checked the
// provider's own dashboard).
func ResolveCRMSend(tenantID, sendID, outcome, note, actor string) error {
	schema, err := crmSchema(tenantID)
	if err != nil {
		return err
	}
	var cur string
	if err := db.DB.QueryRow(fmt.Sprintf(`SELECT status FROM %s.crm_sends WHERE id = $1`, schema), sendID).Scan(&cur); err != nil {
		return crmNotFound("message", sendID)
	}
	if cur != crmSendUnknown {
		return crmErr(CRMCodeMessage, "only a message with an unknown outcome can be settled by hand (this one is %s)", cur)
	}
	status := map[string]string{"delivered": crmSendDelivered, "failed": crmSendFailed, "accepted": crmSendAccepted}[strings.ToLower(outcome)]
	if status == "" || strings.TrimSpace(note) == "" {
		return crmErr(CRMCodeMessage, "say what happened (delivered, accepted or failed) and how you know")
	}
	return crmRecordOutcome(schema, sendID, status, "", "settled by "+actor+": "+strings.TrimSpace(note), nil, "operator")
}

// --- alerts (101.9.5) -----------------------------------------------------------

var crmAlertLast sync.Map // schema|kind -> time.Time

func crmAlertOnce(schema, kind, severity, message string) {
	key := schema + "|" + kind
	if last, ok := crmAlertLast.Load(key); ok && time.Since(last.(time.Time)) < time.Hour {
		return
	}
	crmAlertLast.Store(key, time.Now())
	SendOpsAlert(severity, "crm/"+schema, message)
}

func crmCheckAlerts(schema string) {
	var unknown, failed int
	_ = db.DB.QueryRow(fmt.Sprintf(`SELECT COUNT(*) FILTER (WHERE status = 'Unknown' AND sent_at > LOCALTIMESTAMP - interval '24 hours'),
		COUNT(*) FILTER (WHERE status = 'Failed' AND final_at > LOCALTIMESTAMP - interval '15 minutes')
		FROM %s.crm_sends WHERE sent_at > LOCALTIMESTAMP - interval '24 hours'`, schema)).Scan(&unknown, &failed)
	if unknown > 0 {
		crmAlertOnce(schema, "unknown", "warning", fmt.Sprintf("%d CRM message(s) have an unknown outcome - check the provider and settle them under CRM > Messages", unknown))
	}
	if failed >= 5 {
		crmAlertOnce(schema, "failed", "warning", fmt.Sprintf("%d CRM messages failed in the last 15 minutes - the sender may be misconfigured or the provider down", failed))
	}
	rows, err := db.DB.Query(fmt.Sprintf(`SELECT r.id, r.budget, SUM(s.cost_actual) FROM %s.crm_campaign_runs r JOIN %s.crm_sends s ON s.run_kind = 'campaign' AND s.run_id = r.id
		WHERE r.budget IS NOT NULL AND s.cost_actual IS NOT NULL GROUP BY r.id, r.budget HAVING SUM(s.cost_actual) > r.budget`, schema, schema))
	if err == nil {
		for rows.Next() {
			var id string
			var budget, spent float64
			if rows.Scan(&id, &budget, &spent) == nil {
				crmAlertOnce(schema, "budget:"+id, "warning", fmt.Sprintf("campaign run %s spent %.2f against a budget of %.2f (the provider charged more than estimated)", id, spent, budget))
			}
		}
		rows.Close()
	}
	var stuck int
	_ = db.DB.QueryRow(fmt.Sprintf(`SELECT COUNT(*) FROM %s.crm_journey_runs WHERE state IN ('Active', 'Waiting') AND wait_until < LOCALTIMESTAMP - interval '1 hour'`, schema)).Scan(&stuck)
	if stuck > 0 {
		crmAlertOnce(schema, "stuck", "warning", fmt.Sprintf("%d CRM journey run(s) are more than an hour overdue - the CRM worker may be stopped", stuck))
	}
}

// crmApplyRetention deletes message records older than the client's retention
// (D12, 24 months by default); consent evidence is not touched. 49.18.2:
// batched through retentionDelete, and nothing while the client is on legal hold.
func crmApplyRetention(schema string) {
	months := GetSettingIntForSchema(schema, "crm.message_retention_months")
	if months < 1 {
		return
	}
	tenantID, err := tenantIDForSchema(schema)
	if err != nil {
		return
	}
	for _, q := range []struct{ table, where string }{
		{"crm_sends", `created_at < LOCALTIMESTAMP - make_interval(months => $1) AND status NOT IN ('Queued', 'Deferred', 'Sending')`},
		{"crm_pref_tokens", `created_at < LOCALTIMESTAMP - make_interval(months => $1)`},
		{"crm_events", `occurred_at < LOCALTIMESTAMP - make_interval(months => $1) AND processed_at IS NOT NULL`},
		{"crm_conversation_messages", `created_at < LOCALTIMESTAMP - make_interval(months => $1)`},
	} {
		if _, err := retentionDelete(tenantID, schema, q.table, q.where, months); err != nil {
			log.Printf("[CRM] retention in %s: %v", schema, err)
		}
	}
}

// --- reading messages --------------------------------------------------------------

// CRMSend is one message as the Messages screen shows it.
type CRMSend struct {
	ID          string          `json:"id"`
	RunKind     string          `json:"run_kind"`
	RunID       string          `json:"run_id"`
	CustomerID  string          `json:"customer_id"`
	Customer    string          `json:"customer"`
	Channel     string          `json:"channel"`
	Purpose     string          `json:"purpose"`
	Status      string          `json:"status"`
	Reason      string          `json:"reason,omitempty"`
	ReasonLabel string          `json:"reason_label,omitempty"`
	Detail      string          `json:"detail,omitempty"`
	Contact     string          `json:"contact,omitempty"`
	Body        string          `json:"body,omitempty"`
	Provider    string          `json:"provider,omitempty"`
	ProviderRef string          `json:"provider_ref,omitempty"`
	Parts       *int            `json:"parts,omitempty"`
	Estimate    *float64        `json:"cost_estimate,omitempty"`
	Cost        *float64        `json:"cost,omitempty"`
	CreatedAt   time.Time       `json:"created_at"`
	SentAt      *time.Time      `json:"sent_at,omitempty"`
	History     json.RawMessage `json:"history,omitempty"`
}

// ListCRMSends lists messages, newest first, filtered by status, channel,
// run or customer.
func ListCRMSends(tenantID string, f map[string]string, limit int) ([]CRMSend, error) {
	schema, err := crmSchema(tenantID)
	if err != nil {
		return nil, err
	}
	if limit < 1 || limit > 500 {
		limit = 100
	}
	where := []string{"TRUE"}
	args := []interface{}{}
	for _, k := range []string{"status", "channel", "run_kind", "run_id", "customer_id", "reason_code"} {
		if v := strings.TrimSpace(f[k]); v != "" {
			args = append(args, v)
			where = append(where, fmt.Sprintf("s.%s = $%d", k, len(args)))
		}
	}
	args = append(args, limit)
	rows, err := db.DB.Query(fmt.Sprintf(`SELECT s.id, s.run_kind, s.run_id, s.customer_id, COALESCE(c.data->>'name', ''), s.channel, s.purpose, s.status, COALESCE(s.reason_code, ''),
		COALESCE(s.reason_detail, ''), COALESCE(s.contact_masked, ''), COALESCE(s.rendered_body, s.body_override, ''), COALESCE(s.provider, ''), COALESCE(s.provider_ref, ''),
		s.parts, s.cost_estimate, s.cost_actual, s.created_at, s.sent_at, s.history
		FROM %s.crm_sends s LEFT JOIN %s.documents c ON c.id = s.customer_id AND c.doctype = 'Customer'
		WHERE %s ORDER BY s.created_at DESC LIMIT $%d`, schema, schema, strings.Join(where, " AND "), len(args)), args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []CRMSend{}
	for rows.Next() {
		var m CRMSend
		var parts sql.NullInt64
		var est, cost sql.NullFloat64
		var sent sql.NullTime
		var hist []byte
		if err := rows.Scan(&m.ID, &m.RunKind, &m.RunID, &m.CustomerID, &m.Customer, &m.Channel, &m.Purpose, &m.Status, &m.Reason, &m.Detail, &m.Contact, &m.Body,
			&m.Provider, &m.ProviderRef, &parts, &est, &cost, &m.CreatedAt, &sent, &hist); err != nil {
			return nil, err
		}
		if parts.Valid {
			p := int(parts.Int64)
			m.Parts = &p
		}
		if est.Valid {
			m.Estimate = &est.Float64
		}
		if cost.Valid {
			m.Cost = &cost.Float64
		}
		if sent.Valid {
			m.SentAt = &sent.Time
		}
		m.ReasonLabel = CRMReasonLabels[m.Reason]
		if f["history"] == "1" {
			m.History = hist
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

// --- staff and test messages ----------------------------------------------------------

// CRMStaffMessage is a message a person sends a customer (the store app's
// "send a WhatsApp", the customer page). It goes through the gate like
// everything else and is decided at once.
type CRMStaffMessage struct {
	CustomerID string            `json:"customer_id"`
	Channel    string            `json:"channel"`
	Purpose    string            `json:"purpose"`
	TemplateID string            `json:"template_id"`
	Text       string            `json:"text"`
	Subject    string            `json:"subject"`
	Vars       map[string]string `json:"vars"`
	Test       bool              `json:"test"`
}

// SendCRMStaffMessage queues and decides one message now, and returns what
// happened to it.
func SendCRMStaffMessage(tenantID, actor string, m CRMStaffMessage) (*CRMSend, error) {
	schema, err := crmSchema(tenantID)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(m.TemplateID) == "" && strings.TrimSpace(m.Text) == "" {
		return nil, crmErr(CRMCodeMessage, "choose a template or write the message")
	}
	if len([]rune(m.Text)) > 2000 {
		return nil, crmErr(CRMCodeMessage, "a message can be at most 2,000 characters")
	}
	c, err := crmLoadCustomer(schema, m.CustomerID)
	if err != nil {
		return nil, crmNotFound("customer", m.CustomerID)
	}
	kind := "staff"
	if m.Test {
		kind = "test"
	}
	vars := map[string]string{}
	for k, v := range m.Vars {
		if crmKnownVariable(k) {
			vars[k] = v
		}
	}
	if m.Subject != "" {
		vars["subject"] = m.Subject
	}
	if m.Text != "" {
		vars["message.text"] = m.Text
	}
	body := ""
	if m.TemplateID == "" {
		body = strings.TrimSpace(m.Text)
	}
	id, _, err := crmQueueSend(db.DB, schema, CRMSendRequest{RunKind: kind, RunID: NewDocID("MSG"), CustomerID: c.ID, Channel: m.Channel,
		Purpose: firstNonEmpty(m.Purpose, CRMPurposeService), TemplateID: m.TemplateID, Body: body, Vars: vars, ExpiresInHours: 24, CreatedBy: actor})
	if err != nil {
		return nil, err
	}
	if _, err := crmDispatchNext(schema, id); err != nil {
		return nil, err
	}
	list, err := ListCRMSends(tenantID, map[string]string{"run_kind": kind, "customer_id": c.ID}, 20)
	if err != nil {
		return nil, err
	}
	for i := range list {
		if list[i].ID == id {
			return &list[i], nil
		}
	}
	return nil, crmNotFound("message", id)
}

// crmInHoldout is the fixed random hold-out (101.3.5): the same customer and
// campaign always land on the same side, so a rerun keeps the groups. It is
// the same rule as crmHoldoutSQL (the first 28 bits of an MD5), so a run
// queued in SQL and a send checked here agree.
func crmInHoldout(campaignID, customerID string, pct int) bool {
	if pct <= 0 {
		return false
	}
	sum := md5.Sum([]byte(campaignID + "|" + customerID))
	n, _ := strconv.ParseUint(hex.EncodeToString(sum[:])[:7], 16, 32)
	return int(n%100) < pct
}

// crmHoldoutSQL is crmInHoldout as SQL; campaign and customer are SQL
// expressions, pct a number.
func crmHoldoutSQL(campaign, customer string, pct int) string {
	if pct <= 0 {
		return "FALSE"
	}
	return fmt.Sprintf(`((('x' || '0' || substr(md5(%s || '|' || %s), 1, 7))::bit(32)::int %% 100) < %d)`, campaign, customer, pct)
}
