package engines

import (
	"custom_erp/db"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"strings"
	"sync"
	"time"
)

// Stage 101.4.2 - the journey engine (the blueprint's M13, D9). A journey is a
// trigger and a list of steps with yes / no branches:
//
//	wait        wait N hours
//	wait_event  wait up to N hours for an event; yes = it happened, no = it did not
//	check       a segment rule on the customer; yes / no
//	send        a message, queued for the send gate
//	task        a task for staff (the store worklist)
//	end
//
// Triggers: an event (a bill, a new customer, a return, a cart left on the
// web shop...), a date (birthday, anniversary, points about to expire), not
// seen for N days, or being in a segment. A customer has at most one live
// run of a journey (per cart for cart journeys - a partial unique index), may
// not re-enter within its cool-down, and leaves early when the goal happens
// (usually a purchase). Every wait is a time in the database, and every send
// is keyed by run and step, so a restart in the middle of a 24-hour wait
// resumes it and never sends twice (the blueprint's M13-A01). A published
// journey is frozen: editing makes a new version and customers already in it
// finish the version they started.

// CRMJourneyTrigger starts a run.
type CRMJourneyTrigger struct {
	Type          string `json:"type"` // event, date, inactive, segment
	Event         string `json:"event,omitempty"`
	FirstBillOnly bool   `json:"first_bill_only,omitempty"`
	Date          string `json:"date,omitempty"` // birthday, anniversary, points_expiring
	DaysBefore    int    `json:"days_before,omitempty"`
	Days          int    `json:"days,omitempty"`
	SegmentID     string `json:"segment_id,omitempty"`
	StaleHours    int    `json:"stale_hours,omitempty"`
}

// CRMJourneyGoal ends a run early.
type CRMJourneyGoal struct {
	Event string `json:"event"`
}

// CRMJourneyStep is one step; Yes / No are step numbers (1-based) to go to,
// 0 = the next step, -1 = end.
type CRMJourneyStep struct {
	Type       string                 `json:"type"`
	Hours      int                    `json:"hours,omitempty"`
	Event      string                 `json:"event,omitempty"`
	Yes        int                    `json:"yes,omitempty"`
	No         int                    `json:"no,omitempty"`
	Rule       *CRMSegmentRule        `json:"rule,omitempty"`
	Channel    string                 `json:"channel,omitempty"`
	TemplateID string                 `json:"template_id,omitempty"`
	Purpose    string                 `json:"purpose,omitempty"`
	Voucher    map[string]interface{} `json:"voucher,omitempty"`
	Kind       string                 `json:"kind,omitempty"`
	Title      string                 `json:"title,omitempty"`
	Owner      string                 `json:"owner,omitempty"` // preferred_salesman, store, or a username
	DueHours   int                    `json:"due_hours,omitempty"`
}

// CRMJourneyDefinition is a whole journey.
type CRMJourneyDefinition struct {
	Trigger      CRMJourneyTrigger `json:"trigger"`
	Goal         *CRMJourneyGoal   `json:"goal,omitempty"`
	CooldownDays int               `json:"cooldown_days"`
	PerCart      bool              `json:"per_cart,omitempty"`
	Steps        []CRMJourneyStep  `json:"steps"`
}

// CRMJourney is a journey with its draft, published version and counts.
type CRMJourney struct {
	ID             string                `json:"id"`
	Name           string                `json:"name"`
	Description    string                `json:"description,omitempty"`
	ReadyMade      string                `json:"ready_made,omitempty"`
	Enabled        bool                  `json:"enabled"`
	CurrentVersion int                   `json:"current_version"`
	Draft          *CRMJourneyDefinition `json:"draft,omitempty"`
	Published      *CRMJourneyDefinition `json:"published,omitempty"`
	Unpublished    bool                  `json:"unpublished_changes"`
	Live           int                   `json:"live"`
	Completed      int                   `json:"completed"`
	ExitedGoal     int                   `json:"exited_goal"`
	Sent           int                   `json:"sent"`
	StepCounts     map[int]int           `json:"step_counts,omitempty"`
	UpdatedAt      time.Time             `json:"updated_at"`
}

// --- events into handlers ---------------------------------------------------------

type crmEvent struct {
	ID         int64
	CustomerID string
	Type       string
	RefID      string
	Payload    map[string]interface{}
	At         time.Time
	AgeHours   float64
}

var (
	crmEventHandlersMu sync.Mutex
	crmEventHandlers   = map[string][]func(schema string, ev crmEvent){}
)

// registerCRMEventHandler runs fn for every event of a type (feedback,
// referrals and pending points use it; journeys run after them).
func registerCRMEventHandler(eventType string, fn func(schema string, ev crmEvent)) {
	crmEventHandlersMu.Lock()
	defer crmEventHandlersMu.Unlock()
	crmEventHandlers[eventType] = append(crmEventHandlers[eventType], fn)
}

func init() {
	registerCRMTick("events", crmProcessEvents)
	registerCRMTick("journeys", crmAdvanceJourneys)
	registerCRMDaily("journey date triggers", crmJourneyDailyTriggers)
}

// crmProcessEvents hands new events to their handlers and to journeys, at
// least once each (every handler is idempotent: keyed by bill, run or
// trigger), then marks them processed.
func crmProcessEvents(schema string) {
	tx, err := db.DB.Begin()
	if err != nil {
		return
	}
	defer tx.Rollback()
	rows, err := tx.Query(fmt.Sprintf(`SELECT id, customer_id, event_type, COALESCE(ref_id, ''), payload, occurred_at, EXTRACT(EPOCH FROM (LOCALTIMESTAMP - occurred_at)) / 3600
		FROM %s.crm_events WHERE processed_at IS NULL ORDER BY id LIMIT 200 FOR UPDATE SKIP LOCKED`, schema))
	if err != nil {
		return
	}
	var evs []crmEvent
	for rows.Next() {
		var e crmEvent
		var raw []byte
		if rows.Scan(&e.ID, &e.CustomerID, &e.Type, &e.RefID, &raw, &e.At, &e.AgeHours) == nil {
			_ = json.Unmarshal(raw, &e.Payload)
			if e.Payload == nil {
				e.Payload = map[string]interface{}{}
			}
			evs = append(evs, e)
		}
	}
	rows.Close()
	if len(evs) == 0 {
		return
	}
	crmEventHandlersMu.Lock()
	handlers := map[string][]func(string, crmEvent){}
	for k, v := range crmEventHandlers {
		handlers[k] = append([]func(string, crmEvent){}, v...)
	}
	crmEventHandlersMu.Unlock()
	for _, e := range evs {
		for _, h := range handlers[e.Type] {
			func() {
				defer func() {
					if r := recover(); r != nil {
						log.Printf("[CRM] event handler for %s panicked: %v", e.Type, r)
					}
				}()
				h(schema, e)
			}()
		}
		crmJourneysOnEvent(schema, e)
		if _, err := tx.Exec(fmt.Sprintf(`UPDATE %s.crm_events SET processed_at = LOCALTIMESTAMP WHERE id = $1`, schema), e.ID); err != nil {
			return
		}
	}
	_ = tx.Commit()
}

// --- journey records ----------------------------------------------------------------

func crmValidateJourney(schema string, def *CRMJourneyDefinition) error {
	t := def.Trigger
	switch t.Type {
	case "event":
		if !crmKnownEvent(t.Event) {
			return crmErr(CRMCodeJourney, "choose the event that starts the journey")
		}
	case "date":
		if t.Date != "birthday" && t.Date != "anniversary" && t.Date != "points_expiring" {
			return crmErr(CRMCodeJourney, "a date journey starts on a birthday, an anniversary or points about to expire")
		}
		if t.DaysBefore < 0 || t.DaysBefore > 60 {
			return crmErr(CRMCodeJourney, "start 0 to 60 days before the date")
		}
	case "inactive":
		if t.Days < 7 {
			return crmErr(CRMCodeJourney, "\"not seen for\" needs at least 7 days")
		}
	case "segment":
		if _, err := crmGetSegment(schema, t.SegmentID, 0); err != nil {
			return crmErr(CRMCodeJourney, "choose the segment that starts the journey")
		}
	default:
		return crmErr(CRMCodeJourney, "choose how the journey starts")
	}
	if def.Goal != nil && def.Goal.Event != "" && !crmKnownEvent(def.Goal.Event) {
		return crmErr(CRMCodeJourney, "the goal is an event the CRM knows")
	}
	if def.PerCart && (t.Type != "event" || (t.Event != CRMEventCartAbandoned)) {
		return crmErr(CRMCodeJourney, "only a journey that starts on a left cart runs per cart")
	}
	if len(def.Steps) == 0 || len(def.Steps) > 30 {
		return crmErr(CRMCodeJourney, "a journey has 1 to 30 steps")
	}
	n := len(def.Steps)
	sends := 0
	for i, s := range def.Steps {
		at := fmt.Sprintf("step %d: ", i+1)
		for _, target := range []int{s.Yes, s.No} {
			if target < -1 || target > n || target == i+1 {
				return crmErr(CRMCodeJourney, "%sa branch goes to a step that does not exist (or to itself)", at)
			}
		}
		switch s.Type {
		case "wait":
			if s.Hours < 1 || s.Hours > 24*90 {
				return crmErr(CRMCodeJourney, "%swait 1 hour to 90 days", at)
			}
		case "wait_event":
			if !crmKnownEvent(s.Event) || s.Hours < 1 || s.Hours > 24*90 {
				return crmErr(CRMCodeJourney, "%schoose the event and how long to wait for it (1 hour to 90 days)", at)
			}
		case "check":
			if s.Rule == nil {
				return crmErr(CRMCodeJourney, "%sa check needs a rule", at)
			}
			b := &crmSQLBuilder{schema: schema, zone: crmZoneName(schema), start: 2}
			if _, err := b.compileRule(s.Rule, 0); err != nil {
				return crmErr(CRMCodeJourney, "%s%v", at, err)
			}
		case "send":
			sends++
			if crmChannel(s.Channel) == "" || s.TemplateID == "" {
				return crmErr(CRMCodeJourney, "%sa message needs a channel and a template", at)
			}
			tpl, err := crmGetTemplate(schema, s.TemplateID)
			if err != nil {
				return crmErr(CRMCodeJourney, "%sthe template no longer exists", at)
			}
			if tpl.Channel != crmChannel(s.Channel) {
				return crmErr(CRMCodeJourney, "%sthe template is for %s", at, tpl.Channel)
			}
			if crmPurpose(s.Purpose) == "" {
				return crmErr(CRMCodeJourney, "%sa message is marketing or service", at)
			}
		case "task":
			if strings.TrimSpace(s.Title) == "" {
				return crmErr(CRMCodeJourney, "%sa task needs a title", at)
			}
		case "end":
		default:
			return crmErr(CRMCodeJourney, "%s%q is not a step", at, s.Type)
		}
		if i > 0 && (s.Type == "wait" || s.Type == "wait_event") {
			continue
		}
		// A branch back to an earlier step must pass a wait, or a run could
		// loop forever on one tick.
		for _, target := range []int{s.Yes, s.No} {
			if target > 0 && target <= i {
				hasWait := false
				for j := target - 1; j <= i; j++ {
					if def.Steps[j].Type == "wait" || def.Steps[j].Type == "wait_event" {
						hasWait = true
					}
				}
				if !hasWait {
					return crmErr(CRMCodeJourney, "%sa branch back to step %d must pass a wait", at, target)
				}
			}
		}
	}
	if def.CooldownDays < 0 || def.CooldownDays > 3650 {
		return crmErr(CRMCodeJourney, "the cool-down is 0 to 3,650 days")
	}
	_ = sends
	return nil
}

// SaveCRMJourney creates or edits a journey's draft (never the published
// version).
func SaveCRMJourney(tenantID, id, name, description string, def CRMJourneyDefinition, actor string) (*CRMJourney, error) {
	schema, err := crmSchema(tenantID)
	if err != nil {
		return nil, err
	}
	name = strings.TrimSpace(name)
	if name == "" {
		return nil, crmErr(CRMCodeJourney, "name the journey")
	}
	if id == "" {
		id = NewDocID("JRN")
		_, err = db.DB.Exec(fmt.Sprintf(`INSERT INTO %s.crm_journeys (id, name, description, draft, created_by) VALUES ($1, $2, NULLIF($3, ''), $4, $5)`, schema),
			id, name, strings.TrimSpace(description), crmJSON(def), actor)
	} else {
		var res sql.Result
		res, err = db.DB.Exec(fmt.Sprintf(`UPDATE %s.crm_journeys SET name = $2, description = NULLIF($3, ''), draft = $4, updated_at = NOW() WHERE id = $1`, schema),
			id, name, strings.TrimSpace(description), crmJSON(def))
		if err == nil {
			if n, _ := res.RowsAffected(); n == 0 {
				return nil, crmNotFound("journey", id)
			}
		}
	}
	if err != nil {
		return nil, err
	}
	return GetCRMJourney(tenantID, id)
}

// PublishCRMJourney freezes the draft as a new version.
func PublishCRMJourney(tenantID, id, actor string) (*CRMJourney, error) {
	schema, err := crmSchema(tenantID)
	if err != nil {
		return nil, err
	}
	j, err := GetCRMJourney(tenantID, id)
	if err != nil {
		return nil, err
	}
	if j.Draft == nil {
		return nil, crmErr(CRMCodeJourney, "nothing to publish")
	}
	if err := crmValidateJourney(schema, j.Draft); err != nil {
		return nil, err
	}
	if j.Published != nil && string(crmJSON(j.Published)) == string(crmJSON(j.Draft)) {
		return j, nil
	}
	tx, err := db.DB.Begin()
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	next := j.CurrentVersion + 1
	if _, err := tx.Exec(fmt.Sprintf(`INSERT INTO %s.crm_journey_versions (journey_id, version, definition, published_by) VALUES ($1, $2, $3, $4)`, schema), id, next, crmJSON(j.Draft), actor); err != nil {
		return nil, err
	}
	if _, err := tx.Exec(fmt.Sprintf(`UPDATE %s.crm_journeys SET current_version = $2, updated_at = NOW() WHERE id = $1`, schema), id, next); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	LogAuditEvent(tenantID, actor, "CRM_JOURNEY_PUBLISHED", "SUCCESS", fmt.Sprintf("%s version %d", id, next))
	return GetCRMJourney(tenantID, id)
}

// SetCRMJourneyEnabled switches a published journey on or off. Off stops new
// customers entering; stopLive also ends the runs already in it.
func SetCRMJourneyEnabled(tenantID, id string, enabled, stopLive bool, actor string) (*CRMJourney, error) {
	schema, err := crmSchema(tenantID)
	if err != nil {
		return nil, err
	}
	j, err := GetCRMJourney(tenantID, id)
	if err != nil {
		return nil, err
	}
	if enabled && j.CurrentVersion == 0 {
		return nil, crmErr(CRMCodeJourney, "publish the journey before switching it on")
	}
	if _, err := db.DB.Exec(fmt.Sprintf(`UPDATE %s.crm_journeys SET enabled = $2, updated_at = NOW() WHERE id = $1`, schema), id, enabled); err != nil {
		return nil, err
	}
	if !enabled && stopLive {
		if _, err := db.DB.Exec(fmt.Sprintf(`UPDATE %s.crm_journey_runs SET state = 'Exited', exit_reason = 'switched off', ended_at = LOCALTIMESTAMP, updated_at = LOCALTIMESTAMP
			WHERE journey_id = $1 AND state IN ('Active', 'Waiting')`, schema), id); err != nil {
			return nil, err
		}
	}
	LogAuditEvent(tenantID, actor, "CRM_JOURNEY_SWITCHED", "SUCCESS", fmt.Sprintf("%s enabled=%v stop_live=%v", id, enabled, stopLive))
	return GetCRMJourney(tenantID, id)
}

// GetCRMJourney returns a journey with its counts.
func GetCRMJourney(tenantID, id string) (*CRMJourney, error) {
	schema, err := crmSchema(tenantID)
	if err != nil {
		return nil, err
	}
	var j CRMJourney
	var draft []byte
	var desc, ready sql.NullString
	err = db.DB.QueryRow(fmt.Sprintf(`SELECT id, name, description, ready_made, enabled, current_version, draft, updated_at FROM %s.crm_journeys WHERE id = $1`, schema), id).
		Scan(&j.ID, &j.Name, &desc, &ready, &j.Enabled, &j.CurrentVersion, &draft, &j.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, crmNotFound("journey", id)
	}
	if err != nil {
		return nil, err
	}
	j.Description, j.ReadyMade = desc.String, ready.String
	if len(draft) > 0 && string(draft) != "null" {
		var d CRMJourneyDefinition
		if json.Unmarshal(draft, &d) == nil {
			j.Draft = &d
		}
	}
	if j.CurrentVersion > 0 {
		if p, err := crmJourneyVersion(schema, id, j.CurrentVersion); err == nil {
			j.Published = p
		}
	}
	j.Unpublished = j.Draft != nil && (j.Published == nil || string(crmJSON(j.Published)) != string(crmJSON(j.Draft)))
	_ = db.DB.QueryRow(fmt.Sprintf(`SELECT COUNT(*) FILTER (WHERE state IN ('Active', 'Waiting')), COUNT(*) FILTER (WHERE state = 'Completed'),
		COUNT(*) FILTER (WHERE state = 'Exited' AND exit_reason = 'goal') FROM %s.crm_journey_runs WHERE journey_id = $1`, schema), id).Scan(&j.Live, &j.Completed, &j.ExitedGoal)
	_ = db.DB.QueryRow(fmt.Sprintf(`SELECT COUNT(*) FROM %s.crm_sends s JOIN %s.crm_journey_runs r ON s.run_kind = 'journey' AND split_part(s.run_id, ':', 1) = r.id
		WHERE r.journey_id = $1 AND s.status IN `+crmCountedStatusesSQL, schema, schema), id).Scan(&j.Sent)
	j.StepCounts = map[int]int{}
	rows, err := db.DB.Query(fmt.Sprintf(`SELECT step, COUNT(*) FROM %s.crm_journey_runs WHERE journey_id = $1 AND state IN ('Active', 'Waiting') GROUP BY step`, schema), id)
	if err == nil {
		for rows.Next() {
			var s, n int
			if rows.Scan(&s, &n) == nil {
				j.StepCounts[s+1] = n
			}
		}
		rows.Close()
	}
	return &j, nil
}

// ListCRMJourneys lists journeys.
func ListCRMJourneys(tenantID string) ([]CRMJourney, error) {
	schema, err := crmSchema(tenantID)
	if err != nil {
		return nil, err
	}
	rows, err := db.DB.Query(fmt.Sprintf(`SELECT id FROM %s.crm_journeys ORDER BY enabled DESC, name`, schema))
	if err != nil {
		return nil, err
	}
	var ids []string
	for rows.Next() {
		var id string
		if rows.Scan(&id) == nil {
			ids = append(ids, id)
		}
	}
	rows.Close()
	out := []CRMJourney{}
	for _, id := range ids {
		if j, err := GetCRMJourney(tenantID, id); err == nil {
			j.StepCounts = nil
			out = append(out, *j)
		}
	}
	return out, nil
}

var crmJourneyVersionCache sync.Map // schema|id|v -> *CRMJourneyDefinition (versions never change)

func crmJourneyVersion(schema, id string, version int) (*CRMJourneyDefinition, error) {
	key := fmt.Sprintf("%s|%s|%d", schema, id, version)
	if v, ok := crmJourneyVersionCache.Load(key); ok {
		return v.(*CRMJourneyDefinition), nil
	}
	var raw []byte
	if err := db.DB.QueryRow(fmt.Sprintf(`SELECT definition FROM %s.crm_journey_versions WHERE journey_id = $1 AND version = $2`, schema), id, version).Scan(&raw); err != nil {
		return nil, err
	}
	var d CRMJourneyDefinition
	if err := json.Unmarshal(raw, &d); err != nil {
		return nil, err
	}
	crmJourneyVersionCache.Store(key, &d)
	return &d, nil
}

type crmLiveJourney struct {
	id, name string
	version  int
	def      *CRMJourneyDefinition
}

func crmEnabledJourneys(schema string) []crmLiveJourney {
	rows, err := db.DB.Query(fmt.Sprintf(`SELECT id, name, current_version FROM %s.crm_journeys WHERE enabled AND current_version > 0`, schema))
	if err != nil {
		return nil
	}
	var out []crmLiveJourney
	for rows.Next() {
		var j crmLiveJourney
		if rows.Scan(&j.id, &j.name, &j.version) == nil {
			out = append(out, j)
		}
	}
	rows.Close()
	for i := range out {
		out[i].def, _ = crmJourneyVersion(schema, out[i].id, out[i].version)
	}
	return out
}

// crmStartJourneyRun enters a customer into a journey unless they are in it,
// were in it within its cool-down, or this trigger already started it.
func crmStartJourneyRun(schema string, j crmLiveJourney, customerID, cartKey, triggerRef string, ctxVals map[string]interface{}) (bool, error) {
	if j.def == nil {
		return false, nil
	}
	var blocked bool
	err := db.DB.QueryRow(fmt.Sprintf(`SELECT EXISTS(SELECT 1 FROM %s.crm_journey_runs WHERE journey_id = $1 AND customer_id = $2
		AND ((NULLIF($3, '') IS NOT NULL AND trigger_ref = $3)
		  OR ($4 > 0 AND COALESCE(ended_at, started_at) > LOCALTIMESTAMP - make_interval(days => $4) AND cart_key = $5)))`, schema),
		j.id, customerID, triggerRef, j.def.CooldownDays, cartKey).Scan(&blocked)
	if err != nil || blocked {
		return false, err
	}
	if c, err := crmLoadCustomer(schema, customerID); err != nil || crmCustomerErased(c) {
		return false, nil
	}
	res, err := db.DB.Exec(fmt.Sprintf(`INSERT INTO %s.crm_journey_runs (id, journey_id, version, customer_id, cart_key, state, step, trigger_ref, context, history)
		VALUES ($1, $2, $3, $4, $5, 'Active', 0, NULLIF($6, ''), $7, $8)
		ON CONFLICT (journey_id, customer_id, cart_key) WHERE state IN ('Active', 'Waiting') DO NOTHING`, schema),
		NewDocID("JRUN"), j.id, j.version, customerID, cartKey, triggerRef, crmJSON(ctxVals), crmHistoryEntry("Active", "started: "+firstNonEmpty(triggerRef, "trigger")))
	if err != nil {
		return false, err
	}
	n, _ := res.RowsAffected()
	return n == 1, nil
}

// crmJourneysOnEvent: goals end runs, waits for the event resume, triggers
// start new runs.
func crmJourneysOnEvent(schema string, e crmEvent) {
	journeys := crmEnabledJourneys(schema)
	// goals and waits apply to every live run, whatever version it is on
	rows, err := db.DB.Query(fmt.Sprintf(`SELECT id, journey_id, version, cart_key, state, COALESCE(wait_event, ''), step FROM %s.crm_journey_runs
		WHERE customer_id = $1 AND state IN ('Active', 'Waiting') AND started_at <= $2`, schema), e.CustomerID, e.At)
	if err == nil {
		type live struct {
			id, journey, cart, state, waitEvent string
			version, step                       int
		}
		var runs []live
		for rows.Next() {
			var l live
			if rows.Scan(&l.id, &l.journey, &l.version, &l.cart, &l.state, &l.waitEvent, &l.step) == nil {
				runs = append(runs, l)
			}
		}
		rows.Close()
		for _, r := range runs {
			def, err := crmJourneyVersion(schema, r.journey, r.version)
			if err != nil {
				continue
			}
			sameCart := !def.PerCart || r.cart == e.RefID || r.cart == crmString(e.Payload["cart"])
			if def.Goal != nil && def.Goal.Event == e.Type && sameCart {
				_, _ = db.DB.Exec(fmt.Sprintf(`UPDATE %s.crm_journey_runs SET state = 'Exited', exit_reason = 'goal', ended_at = LOCALTIMESTAMP, updated_at = LOCALTIMESTAMP,
					history = history || $2::jsonb WHERE id = $1 AND state IN ('Active', 'Waiting')`, schema), r.id, crmHistoryEntry("Exited", "goal: "+e.Type))
				continue
			}
			if r.state == "Waiting" && r.waitEvent == e.Type && sameCart && r.step < len(def.Steps) {
				next := crmJourneyTarget(def.Steps[r.step].Yes, r.step)
				_, _ = db.DB.Exec(fmt.Sprintf(`UPDATE %s.crm_journey_runs SET state = 'Active', step = $2, wait_until = NULL, wait_event = NULL, updated_at = LOCALTIMESTAMP,
					history = history || $3::jsonb WHERE id = $1 AND state = 'Waiting'`, schema), r.id, next, crmHistoryEntry("Active", "event happened: "+e.Type))
			}
		}
	}
	for _, j := range journeys {
		if j.def == nil || j.def.Trigger.Type != "event" || j.def.Trigger.Event != e.Type {
			continue
		}
		if j.def.Trigger.StaleHours > 0 && e.AgeHours > float64(j.def.Trigger.StaleHours) {
			continue // the blueprint's stale-data hold: too old to act on
		}
		if j.def.Trigger.FirstBillOnly && e.Type == CRMEventBillPaid {
			var bills int
			_ = db.DB.QueryRow(fmt.Sprintf(`SELECT COUNT(*) FROM %s.documents WHERE doctype = 'POSCart' AND status = 'Paid' AND data->>'customer_id' = $1`, schema), e.CustomerID).Scan(&bills)
			if bills != 1 {
				continue
			}
		}
		cart := ""
		if j.def.PerCart {
			cart = firstNonEmpty(crmString(e.Payload["cart"]), e.RefID)
		}
		vals := map[string]interface{}{"event": e.Type, "ref": e.RefID}
		for k, v := range e.Payload {
			vals[k] = v
		}
		if _, err := crmStartJourneyRun(schema, j, e.CustomerID, cart, fmt.Sprintf("event:%d", e.ID), vals); err != nil {
			log.Printf("[CRM] journey %s did not start for %s: %v", j.id, e.CustomerID, err)
		}
	}
}

// crmJourneyTarget turns a 1-based branch target into the next step index.
func crmJourneyTarget(target, current int) int {
	switch {
	case target == 0:
		return current + 1
	case target < 0:
		return 1 << 20 // past the end: the run completes
	}
	return target - 1
}

// crmJourneyDailyTriggers starts date, inactivity and segment journeys.
func crmJourneyDailyTriggers(schema string) {
	zone := crmZoneName(schema)
	today := crmLocalTodaySQL(zone)
	stamp := time.Now().In(crmLocation(schema)).Format("2006-01-02")
	th := crmLifecycleThresholds(schema)
	for _, j := range crmEnabledJourneys(schema) {
		if j.def == nil {
			continue
		}
		t := j.def.Trigger
		var q string
		args := []interface{}{}
		switch t.Type {
		case "date":
			switch t.Date {
			case "birthday", "anniversary":
				col := map[string]string{"birthday": "dob", "anniversary": "anniversary"}[t.Date]
				q = fmt.Sprintf(` SELECT id, '{}'::jsonb FROM cf WHERE %[1]s IS NOT NULL AND to_char(%[1]s, 'MM-DD') = to_char(%[2]s + %[3]d, 'MM-DD')`, col, today, t.DaysBefore)
			case "points_expiring":
				q = fmt.Sprintf(` SELECT l.customer_id, jsonb_build_object('points.expiring', SUM(l.points)::text, 'points.expiry_date', to_char(MIN(l.expires_at), 'DD Mon'))
					FROM %s.loyalty_point_ledger l JOIN cf ON cf.id = l.customer_id
					WHERE l.transaction_type = 'Earn' AND l.cancelled_at IS NULL AND l.expires_at::date = %s + %d GROUP BY l.customer_id`, schema, today, t.DaysBefore)
			}
		case "inactive":
			args = append(args, t.Days)
			q = ` SELECT id, '{}'::jsonb FROM cf WHERE days_since = $1`
		case "segment":
			seg, err := crmGetSegment(schema, t.SegmentID, 0)
			if err != nil {
				continue
			}
			pred, segArgs, err := crmCompileSegment(schema, &seg.Definition, 1)
			if err != nil {
				continue
			}
			args = segArgs
			q = ` SELECT id, '{}'::jsonb FROM cf WHERE ` + pred
		default:
			continue
		}
		if q == "" {
			continue
		}
		rows, err := db.DB.Query(crmFactsCTE(schema, th, false)+q, args...)
		if err != nil {
			log.Printf("[CRM] journey %s daily trigger in %s: %v", j.id, schema, err)
			continue
		}
		type hit struct {
			id   string
			vals map[string]interface{}
		}
		var hits []hit
		for rows.Next() {
			var h hit
			var raw []byte
			if rows.Scan(&h.id, &raw) == nil {
				_ = json.Unmarshal(raw, &h.vals)
				hits = append(hits, h)
			}
		}
		rows.Close()
		for _, h := range hits {
			ref := t.Type + ":" + t.Date + ":" + stamp
			if t.Type == "segment" || t.Type == "inactive" {
				ref = "" // the cool-down decides re-entry
			}
			if _, err := crmStartJourneyRun(schema, j, h.id, "", ref, h.vals); err != nil {
				log.Printf("[CRM] journey %s start for %s: %v", j.id, h.id, err)
			}
		}
	}
}

// crmAdvanceJourneys moves every run that is due.
func crmAdvanceJourneys(schema string) {
	rows, err := db.DB.Query(fmt.Sprintf(`SELECT id FROM %s.crm_journey_runs WHERE state = 'Active' OR (state = 'Waiting' AND wait_until <= LOCALTIMESTAMP)
		ORDER BY updated_at LIMIT 300`, schema))
	if err != nil {
		return
	}
	var ids []string
	for rows.Next() {
		var id string
		if rows.Scan(&id) == nil {
			ids = append(ids, id)
		}
	}
	rows.Close()
	for _, id := range ids {
		if err := crmAdvanceRun(schema, id); err != nil {
			log.Printf("[CRM] journey run %s in %s: %v", id, schema, err)
		}
	}
}

// crmAdvanceRun runs steps until the run waits or ends. Each step commits on
// its own, so a crash between steps resumes at the step that was next.
func crmAdvanceRun(schema, runID string) error {
	for i := 0; i < 50; i++ {
		done, err := crmAdvanceOneStep(schema, runID)
		if err != nil || done {
			return err
		}
	}
	return nil
}

func crmAdvanceOneStep(schema, runID string) (bool, error) {
	tx, err := db.DB.Begin()
	if err != nil {
		return true, err
	}
	defer tx.Rollback()
	var journeyID, customerID, cartKey, state, waitEvent string
	var version, step int
	var waitDue bool
	var ctxRaw []byte
	var started time.Time
	err = tx.QueryRow(fmt.Sprintf(`SELECT journey_id, version, customer_id, cart_key, state, COALESCE(wait_event, ''), step,
		(wait_until IS NOT NULL AND wait_until <= LOCALTIMESTAMP), context, started_at
		FROM %s.crm_journey_runs WHERE id = $1 FOR UPDATE SKIP LOCKED`, schema), runID).
		Scan(&journeyID, &version, &customerID, &cartKey, &state, &waitEvent, &step, &waitDue, &ctxRaw, &started)
	if errors.Is(err, sql.ErrNoRows) {
		return true, nil
	}
	if err != nil {
		return true, err
	}
	if state != "Active" && !(state == "Waiting" && waitDue) {
		return true, nil
	}
	def, err := crmJourneyVersion(schema, journeyID, version)
	if err != nil {
		return true, err
	}
	finish := func(st, reason, detail string) (bool, error) {
		if _, err := tx.Exec(fmt.Sprintf(`UPDATE %s.crm_journey_runs SET state = $2, exit_reason = NULLIF($3, ''), ended_at = LOCALTIMESTAMP, updated_at = LOCALTIMESTAMP,
			wait_until = NULL, wait_event = NULL, history = history || $4::jsonb WHERE id = $1`, schema), runID, st, reason, crmHistoryEntry(st, detail)); err != nil {
			return true, err
		}
		return true, tx.Commit()
	}
	if c, err := crmLoadCustomer(schema, customerID); err != nil || crmCustomerErased(c) {
		return finish("Cancelled", "erased", "the customer is no longer messaged")
	}
	if def.Goal != nil && def.Goal.Event != "" {
		var met bool
		_ = tx.QueryRow(fmt.Sprintf(`SELECT EXISTS(SELECT 1 FROM %s.crm_events WHERE customer_id = $1 AND event_type = $2 AND occurred_at >= $3
			AND (NOT $4 OR ref_id = $5 OR payload->>'cart' = $5))`, schema), customerID, def.Goal.Event, started, def.PerCart, cartKey).Scan(&met)
		if met {
			return finish("Exited", "goal", "goal: "+def.Goal.Event)
		}
	}
	if state == "Waiting" {
		// The wait is over: a plain wait already points at the next step; a
		// wait for an event that did not come takes the "no" branch.
		if waitEvent != "" && step < len(def.Steps) {
			step = crmJourneyTarget(def.Steps[step].No, step)
		}
		if _, err := tx.Exec(fmt.Sprintf(`UPDATE %s.crm_journey_runs SET state = 'Active', step = $2, wait_until = NULL, wait_event = NULL, updated_at = LOCALTIMESTAMP WHERE id = $1`, schema), runID, step); err != nil {
			return true, err
		}
	}
	if step >= len(def.Steps) {
		return finish("Completed", "", "reached the end")
	}
	s := def.Steps[step]
	var ctxVals map[string]interface{}
	_ = json.Unmarshal(ctxRaw, &ctxVals)
	move := func(next int, detail string) (bool, error) {
		if _, err := tx.Exec(fmt.Sprintf(`UPDATE %s.crm_journey_runs SET step = $2, updated_at = LOCALTIMESTAMP, history = history || $3::jsonb WHERE id = $1`, schema),
			runID, next, crmHistoryEntry("Active", fmt.Sprintf("step %d %s: %s", step+1, s.Type, detail))); err != nil {
			return true, err
		}
		return false, tx.Commit()
	}
	switch s.Type {
	case "wait":
		if _, err := tx.Exec(fmt.Sprintf(`UPDATE %s.crm_journey_runs SET state = 'Waiting', step = $2, wait_until = LOCALTIMESTAMP + make_interval(hours => $3), updated_at = LOCALTIMESTAMP,
			history = history || $4::jsonb WHERE id = $1`, schema), runID, step+1, s.Hours, crmHistoryEntry("Waiting", fmt.Sprintf("step %d: wait %d hour(s)", step+1, s.Hours))); err != nil {
			return true, err
		}
		return true, tx.Commit()
	case "wait_event":
		if _, err := tx.Exec(fmt.Sprintf(`UPDATE %s.crm_journey_runs SET state = 'Waiting', wait_event = $2, wait_until = LOCALTIMESTAMP + make_interval(hours => $3), updated_at = LOCALTIMESTAMP,
			history = history || $4::jsonb WHERE id = $1`, schema), runID, s.Event, s.Hours, crmHistoryEntry("Waiting", fmt.Sprintf("step %d: wait for %s", step+1, s.Event))); err != nil {
			return true, err
		}
		return true, tx.Commit()
	case "check":
		ok, err := crmCustomerMatchesRule(schema, customerID, s.Rule)
		if err != nil {
			return finish("Failed", "check failed", err.Error())
		}
		if ok {
			return move(crmJourneyTarget(s.Yes, step), "yes")
		}
		return move(crmJourneyTarget(s.No, step), "no")
	case "send":
		vars := map[string]string{}
		for k, v := range ctxVals {
			if crmKnownVariable(k) {
				vars[k] = crmString(v)
			}
		}
		if b := crmString(ctxVals["ref"]); b != "" && crmString(ctxVals["event"]) == CRMEventBillPaid {
			vars["bill.number"] = crmBillNumber(schema, b)
			if a, ok := crmNumber(ctxVals["amount"]); ok {
				vars["bill.amount"] = fmt.Sprintf("%.2f", a)
			}
		}
		if st := crmString(ctxVals["store"]); st != "" {
			vars["store.name"] = crmStoreName(schema, st)
		}
		if len(s.Voucher) > 0 {
			code, value, expiry, err := crmIssueCampaignVoucherTx(tx, schema, journeyID, runID, customerID, s.Voucher)
			if err != nil {
				return true, err
			}
			vars["voucher.code"], vars["voucher.value"], vars["voucher.expiry"] = code, value, expiry
		}
		if _, _, err := crmQueueSend(tx, schema, CRMSendRequest{RunKind: "journey", RunID: fmt.Sprintf("%s:%d", runID, step), CustomerID: customerID,
			Channel: s.Channel, Purpose: firstNonEmpty(s.Purpose, CRMPurposeMarketing), TemplateID: s.TemplateID, Vars: vars, ExpiresInHours: 48, CreatedBy: "journey"}); err != nil {
			return finish("Failed", "send failed", err.Error())
		}
		return move(step+1, "message queued")
	case "task":
		owner := crmTaskOwner(schema, customerID, s.Owner)
		title := crmRender(&CRMTemplateVersion{Body: s.Title}, crmCustomerVars(schema, crmCustomer{ID: customerID, Data: crmCustomerData(schema, customerID)})).Body
		due := s.DueHours
		if due <= 0 {
			due = 24
		}
		if _, err := tx.Exec(fmt.Sprintf(`INSERT INTO %s.crm_tasks (id, kind, title, customer_id, store, owner, due_at, ref_doctype, ref_id, created_by)
			SELECT $1, $2, $3, $4, NULLIF($5, ''), NULLIF($6, ''), LOCALTIMESTAMP + make_interval(hours => $7), 'CRMJourneyRun', $8, 'journey'
			 WHERE NOT EXISTS (SELECT 1 FROM %s.crm_tasks WHERE ref_doctype = 'CRMJourneyRun' AND ref_id = $8)`, schema, schema),
			NewDocID("TSK"), firstNonEmpty(s.Kind, "follow_up"), truncateRunes(title, 300), customerID, crmString(ctxVals["store"]), owner, due, fmt.Sprintf("%s:%d", runID, step)); err != nil {
			return true, err
		}
		return move(step+1, "task for "+firstNonEmpty(owner, "the store"))
	case "end":
		return finish("Completed", "", "end step")
	}
	return finish("Failed", "unknown step", s.Type)
}

func crmCustomerData(schema, id string) map[string]interface{} {
	c, err := crmLoadCustomer(schema, id)
	if err != nil {
		return map[string]interface{}{}
	}
	return c.Data
}

// crmCustomerMatchesRule evaluates one segment rule for one customer.
func crmCustomerMatchesRule(schema, customerID string, r *CRMSegmentRule) (bool, error) {
	b := &crmSQLBuilder{schema: schema, zone: crmZoneName(schema), start: 2}
	pred, err := b.compileRule(r, 0)
	if err != nil {
		return false, err
	}
	var ok bool
	err = db.DB.QueryRow(crmFactsCTE(schema, crmLifecycleThresholds(schema), false)+` SELECT COALESCE((`+pred+`), FALSE) FROM cf WHERE id = $1`,
		append([]interface{}{customerID}, b.args...)...).Scan(&ok)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	return ok, err
}

// crmTaskOwner resolves a task step's owner: the customer's preferred
// salesman, "store" (anyone at the store), or a named user.
func crmTaskOwner(schema, customerID, owner string) string {
	switch owner {
	case "", "store":
		return ""
	case "preferred_salesman":
		return strField(crmCustomerData(schema, customerID), "preferred_salesman")
	}
	return owner
}

// CRMJourneyRunRow is one customer's run, for the journey screen.
type CRMJourneyRunRow struct {
	ID         string          `json:"id"`
	CustomerID string          `json:"customer_id"`
	Customer   string          `json:"customer"`
	Version    int             `json:"version"`
	State      string          `json:"state"`
	Step       int             `json:"step"`
	WaitUntil  *time.Time      `json:"wait_until,omitempty"`
	ExitReason string          `json:"exit_reason,omitempty"`
	StartedAt  time.Time       `json:"started_at"`
	EndedAt    *time.Time      `json:"ended_at,omitempty"`
	History    json.RawMessage `json:"history"`
}

// ListCRMJourneyRuns lists a journey's runs, newest first.
func ListCRMJourneyRuns(tenantID, journeyID, state string, limit int) ([]CRMJourneyRunRow, error) {
	schema, err := crmSchema(tenantID)
	if err != nil {
		return nil, err
	}
	if limit < 1 || limit > 500 {
		limit = 100
	}
	rows, err := db.DB.Query(fmt.Sprintf(`SELECT r.id, r.customer_id, COALESCE(c.data->>'name', ''), r.version, r.state, r.step, r.wait_until, COALESCE(r.exit_reason, ''), r.started_at, r.ended_at, r.history
		FROM %s.crm_journey_runs r LEFT JOIN %s.documents c ON c.id = r.customer_id AND c.doctype = 'Customer'
		WHERE r.journey_id = $1 AND ($2 = '' OR r.state = $2) ORDER BY r.started_at DESC LIMIT $3`, schema, schema), journeyID, state, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []CRMJourneyRunRow{}
	for rows.Next() {
		var r CRMJourneyRunRow
		var wait, ended sql.NullTime
		if err := rows.Scan(&r.ID, &r.CustomerID, &r.Customer, &r.Version, &r.State, &r.Step, &wait, &r.ExitReason, &r.StartedAt, &ended, &r.History); err != nil {
			return nil, err
		}
		r.Step++
		if wait.Valid {
			r.WaitUntil = &wait.Time
		}
		if ended.Valid {
			r.EndedAt = &ended.Time
		}
		out = append(out, r)
	}
	return out, rows.Err()
}
