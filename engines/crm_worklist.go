package engines

import (
	"custom_erp/db"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"
)

// Stage 101.12 (continued from crm_clienteling.go) - the store worklist,
// customer appointments and hold requests.

func init() {
	RegisterSetting(SettingDefinition{Key: "crm.customer_hold_code", Module: "CRM", Label: "Hold code for customer holds", Type: SettingTypeString, Default: "",
		Description: "A Hold Code (Inventory > Hold Codes) used when a store confirms a customer's hold: the stock is then held and no till can sell it; releasing it goes through Approvals like any hold. Blank: a confirmed hold is a note that the item was set aside, and the stock is not held."})
}

// --- the store worklist (101.12.1) -----------------------------------------------

// CRMTaskKinds are the worklist's kinds of task.
var CRMTaskKinds = map[string]string{
	"custom_order": "Custom order", "arrival": "Product arrival", "alteration": "Alteration", "follow_up": "Follow-up",
	"call": "Call", "meeting": "Meeting", "hold": "Hold request", "task": "Task",
}

// CRMTask is one task on the worklist.
type CRMTask struct {
	ID            string     `json:"id"`
	Kind          string     `json:"kind"`
	KindLabel     string     `json:"kind_label"`
	Title         string     `json:"title"`
	CustomerID    string     `json:"customer_id,omitempty"`
	CustomerName  string     `json:"customer_name,omitempty"`
	LeadID        string     `json:"lead_id,omitempty"`
	OpportunityID string     `json:"opportunity_id,omitempty"`
	Store         string     `json:"store,omitempty"`
	Owner         string     `json:"owner,omitempty"`
	DueAt         *time.Time `json:"due_at,omitempty"`
	Overdue       bool       `json:"overdue"`
	Status        string     `json:"status"`
	Outcome       string     `json:"outcome,omitempty"`
	RefDoctype    string     `json:"ref_doctype,omitempty"`
	RefID         string     `json:"ref_id,omitempty"`
	CreatedBy     string     `json:"created_by,omitempty"`
	CreatedAt     time.Time  `json:"created_at"`
	DoneAt        *time.Time `json:"done_at,omitempty"`
}

// CRMTaskInput creates or edits a task. DueAt is the client's local time.
type CRMTaskInput struct {
	Kind          string `json:"kind"`
	Title         string `json:"title"`
	CustomerID    string `json:"customer_id"`
	LeadID        string `json:"lead_id"`
	OpportunityID string `json:"opportunity_id"`
	Store         string `json:"store"`
	Owner         string `json:"owner"`
	DueAt         string `json:"due_at"`
}

// SaveCRMTask creates (id "") or edits an open task.
func SaveCRMTask(tenantID, id string, in CRMTaskInput, actor string) (*CRMTask, error) {
	schema, err := crmSchema(tenantID)
	if err != nil {
		return nil, err
	}
	in.Title = strings.TrimSpace(in.Title)
	if _, ok := CRMTaskKinds[in.Kind]; !ok {
		in.Kind = "task"
	}
	if in.Title == "" {
		return nil, crmErr(CRMCodeTask, "say what has to be done")
	}
	if in.CustomerID != "" {
		c, err := crmLoadCustomer(schema, in.CustomerID)
		if err != nil {
			return nil, crmNotFound("customer", in.CustomerID)
		}
		in.CustomerID = c.ID
	}
	due, err := crmParseLocal(schema, in.DueAt)
	if err != nil {
		return nil, crmErr(CRMCodeTask, "the due time is not a date and time")
	}
	// $9 is always bound; blank means no due time.
	dueSQL := "CASE WHEN $9::text = '' THEN NULL END::timestamp"
	if due != "" {
		dueSQL = crmLocalToDBSQL("$9", crmZoneName(schema))
	}
	if id == "" {
		id = NewDocID("TSK")
		_, err = db.DB.Exec(fmt.Sprintf(`INSERT INTO %s.crm_tasks (id, kind, title, customer_id, lead_id, opportunity_id, store, owner, due_at, created_by)
			VALUES ($1, $2, $3, NULLIF($4, ''), NULLIF($5, ''), NULLIF($6, ''), NULLIF($7, ''), NULLIF($8, ''), %s, $10)`, schema, dueSQL),
			id, in.Kind, truncateRunes(in.Title, 300), in.CustomerID, in.LeadID, in.OpportunityID, in.Store, in.Owner, due, actor)
	} else {
		var res sql.Result
		res, err = db.DB.Exec(fmt.Sprintf(`UPDATE %s.crm_tasks SET kind = $2, title = $3, customer_id = NULLIF($4, ''), lead_id = NULLIF($5, ''), opportunity_id = NULLIF($6, ''),
			store = NULLIF($7, ''), owner = NULLIF($8, ''), due_at = %s WHERE id = $1 AND status = 'Open' AND $10::text IS NOT NULL`, schema, dueSQL),
			id, in.Kind, truncateRunes(in.Title, 300), in.CustomerID, in.LeadID, in.OpportunityID, in.Store, in.Owner, due, actor)
		if err == nil {
			if k, _ := res.RowsAffected(); k == 0 {
				return nil, crmErr(CRMCodeTask, "only an open task can be changed")
			}
		}
	}
	if err != nil {
		return nil, err
	}
	return GetCRMTask(tenantID, id)
}

const crmTaskSelect = `SELECT t.id, t.kind, t.title, COALESCE(t.customer_id, ''), COALESCE(c.data->>'name', ''), COALESCE(t.lead_id, ''), COALESCE(t.opportunity_id, ''),
	COALESCE(t.store, ''), COALESCE(t.owner, ''), t.due_at, (t.status = 'Open' AND t.due_at < LOCALTIMESTAMP), t.status, COALESCE(t.outcome, ''),
	COALESCE(t.ref_doctype, ''), COALESCE(t.ref_id, ''), COALESCE(t.created_by, ''), t.created_at, t.done_at
	FROM %[1]s.crm_tasks t LEFT JOIN %[1]s.documents c ON c.id = t.customer_id AND c.doctype = 'Customer'`

func crmScanTask(rows interface{ Scan(...interface{}) error }) (CRMTask, error) {
	var t CRMTask
	var due, done sql.NullTime
	var overdue sql.NullBool
	err := rows.Scan(&t.ID, &t.Kind, &t.Title, &t.CustomerID, &t.CustomerName, &t.LeadID, &t.OpportunityID, &t.Store, &t.Owner, &due, &overdue, &t.Status, &t.Outcome,
		&t.RefDoctype, &t.RefID, &t.CreatedBy, &t.CreatedAt, &done)
	if due.Valid {
		t.DueAt = &due.Time
	}
	if done.Valid {
		t.DoneAt = &done.Time
	}
	t.Overdue = overdue.Bool
	t.KindLabel = CRMTaskKinds[t.Kind]
	return t, err
}

// GetCRMTask returns one task.
func GetCRMTask(tenantID, id string) (*CRMTask, error) {
	schema, err := crmSchema(tenantID)
	if err != nil {
		return nil, err
	}
	t, err := crmScanTask(db.DB.QueryRow(fmt.Sprintf(crmTaskSelect+` WHERE t.id = $1`, schema), id))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, crmNotFound("task", id)
	}
	return &t, err
}

// ListCRMTasks lists tasks: mine (owner = me, or nobody's at my store), a
// store's, a customer's, a lead's or an opportunity's.
func ListCRMTasks(tenantID string, f map[string]string) ([]CRMTask, error) {
	schema, err := crmSchema(tenantID)
	if err != nil {
		return nil, err
	}
	where := []string{"TRUE"}
	args := []interface{}{}
	add := func(cond string, v interface{}) {
		args = append(args, v)
		where = append(where, fmt.Sprintf(cond, len(args)))
	}
	switch f["status"] {
	case "", "open":
		where = append(where, "t.status = 'Open'")
	case "done":
		where = append(where, "t.status = 'Done'")
	case "all":
	default:
		add("t.status = $%d", f["status"])
	}
	if v := f["mine"]; v != "" {
		args = append(args, v, f["store"])
		where = append(where, fmt.Sprintf("(t.owner = $%d OR (t.owner IS NULL AND ($%d = '' OR t.store = $%d)))", len(args)-1, len(args), len(args)))
	} else if v := f["store"]; v != "" {
		add("t.store = $%d", v)
	}
	for _, k := range []string{"customer_id", "lead_id", "opportunity_id", "kind", "owner"} {
		if v := f[k]; v != "" {
			add("t."+k+" = $%d", v)
		}
	}
	rows, err := db.DB.Query(fmt.Sprintf(crmTaskSelect+` WHERE `+strings.Join(where, " AND ")+` ORDER BY (t.status = 'Open') DESC, t.due_at NULLS LAST, t.created_at DESC LIMIT 300`, schema), args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []CRMTask{}
	for rows.Next() {
		t, err := crmScanTask(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

// FinishCRMTask marks a task done (with its outcome), cancels, reopens or
// takes it. An arrival marked done tells journeys the item arrived.
func FinishCRMTask(tenantID, id, action, outcome, actor string) (*CRMTask, error) {
	schema, err := crmSchema(tenantID)
	if err != nil {
		return nil, err
	}
	t, err := GetCRMTask(tenantID, id)
	if err != nil {
		return nil, err
	}
	outcome = strings.TrimSpace(outcome)
	switch action {
	case "done":
		if t.Status != "Open" {
			return nil, crmErr(CRMCodeTask, "the task is already %s", strings.ToLower(t.Status))
		}
		if outcome == "" {
			return nil, crmErr(CRMCodeTask, "say how it went")
		}
		_, err = db.DB.Exec(fmt.Sprintf(`UPDATE %s.crm_tasks SET status = 'Done', outcome = $2, done_at = LOCALTIMESTAMP, done_by = $3 WHERE id = $1 AND status = 'Open'`, schema), id, outcome, actor)
		if err == nil && t.Kind == "arrival" && t.CustomerID != "" {
			_ = RecordCRMEvent(schema, t.CustomerID, CRMEventProductArrived, id, map[string]interface{}{"store": t.Store, "item.name": t.Title})
		}
	case "cancel":
		_, err = db.DB.Exec(fmt.Sprintf(`UPDATE %s.crm_tasks SET status = 'Cancelled', outcome = NULLIF($2, ''), done_at = LOCALTIMESTAMP, done_by = $3 WHERE id = $1 AND status = 'Open'`, schema), id, outcome, actor)
	case "reopen":
		_, err = db.DB.Exec(fmt.Sprintf(`UPDATE %s.crm_tasks SET status = 'Open', done_at = NULL, done_by = NULL WHERE id = $1 AND status <> 'Open'`, schema), id)
	case "take":
		_, err = db.DB.Exec(fmt.Sprintf(`UPDATE %s.crm_tasks SET owner = $2 WHERE id = $1 AND status = 'Open'`, schema), id, actor)
	default:
		return nil, crmErr(CRMCodeTask, "unknown action %q", action)
	}
	if err != nil {
		return nil, err
	}
	return GetCRMTask(tenantID, id)
}

// CRMTaskInboxItems are a user's open CRM tasks for the task inbox (mine,
// or nobody's at my store).
func CRMTaskInboxItems(tenantID, username, store string) ([]TaskInboxItem, error) {
	tasks, err := ListCRMTasks(tenantID, map[string]string{"mine": username, "store": store})
	if err != nil {
		return nil, err
	}
	var out []TaskInboxItem
	for _, t := range tasks {
		it := TaskInboxItem{Source: "CRM", Title: t.KindLabel + ": " + t.Title, DocType: "CRMTask", DocID: t.ID, View: "crm", Queue: "worklist", WaitingSince: t.CreatedAt, Overdue: t.Overdue}
		if t.CustomerName != "" {
			it.Reason = "for " + t.CustomerName
		}
		if t.Owner == "" {
			it.Reason = strings.TrimSpace(it.Reason + " (anyone at " + firstNonEmpty(t.Store, "the store") + ")")
		}
		if t.DueAt != nil {
			it.DueDate = t.DueAt.Format("2006-01-02 15:04")
		}
		out = append(out, it)
	}
	return out, nil
}

// --- appointments (101.12.2) -----------------------------------------------------

// CRMStoreSlots is a store's appointment hours and reminder.
type CRMStoreSlots struct {
	Store              string `json:"store"`
	SlotMinutes        int    `json:"slot_minutes"`
	Capacity           int    `json:"capacity"`
	Opens              string `json:"opens"`
	Closes             string `json:"closes"`
	Days               string `json:"days"`
	ReminderChannel    string `json:"reminder_channel"`
	ReminderTemplateID string `json:"reminder_template_id,omitempty"`
}

// GetCRMStoreSlots returns a store's appointment hours (defaults if unset).
func GetCRMStoreSlots(tenantID, store string) (*CRMStoreSlots, error) {
	schema, err := crmSchema(tenantID)
	if err != nil {
		return nil, err
	}
	s := &CRMStoreSlots{Store: store, SlotMinutes: 30, Capacity: 1, Opens: "10:00", Closes: "20:00", Days: "1234567", ReminderChannel: CRMChannelWhatsApp}
	var tpl sql.NullString
	err = db.DB.QueryRow(fmt.Sprintf(`SELECT slot_minutes, capacity, opens, closes, days, reminder_channel, reminder_template_id FROM %s.crm_store_slots WHERE store = $1`, schema), store).
		Scan(&s.SlotMinutes, &s.Capacity, &s.Opens, &s.Closes, &s.Days, &s.ReminderChannel, &tpl)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return nil, err
	}
	s.ReminderTemplateID = tpl.String
	return s, nil
}

// SaveCRMStoreSlots sets a store's appointment hours.
func SaveCRMStoreSlots(tenantID string, s CRMStoreSlots, actor string) (*CRMStoreSlots, error) {
	schema, err := crmSchema(tenantID)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(s.Store) == "" {
		return nil, crmErr(CRMCodeSlotFull, "choose the store")
	}
	o, ok1 := crmClock(s.Opens)
	c, ok2 := crmClock(s.Closes)
	if !ok1 || !ok2 || c <= o {
		return nil, crmErr(CRMCodeSlotFull, "opening and closing times are HH:MM, closing after opening")
	}
	if s.SlotMinutes < 10 || s.SlotMinutes > 240 || s.Capacity < 1 || s.Capacity > 50 {
		return nil, crmErr(CRMCodeSlotFull, "a slot is 10-240 minutes and takes 1-50 bookings")
	}
	if s.ReminderChannel = crmChannel(s.ReminderChannel); s.ReminderChannel == "" {
		s.ReminderChannel = CRMChannelWhatsApp
	}
	_, err = db.DB.Exec(fmt.Sprintf(`INSERT INTO %s.crm_store_slots (store, slot_minutes, capacity, opens, closes, days, reminder_channel, reminder_template_id, updated_by, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, NULLIF($8, ''), $9, NOW()) ON CONFLICT (store) DO UPDATE SET slot_minutes = EXCLUDED.slot_minutes, capacity = EXCLUDED.capacity,
		opens = EXCLUDED.opens, closes = EXCLUDED.closes, days = EXCLUDED.days, reminder_channel = EXCLUDED.reminder_channel, reminder_template_id = EXCLUDED.reminder_template_id,
		updated_by = EXCLUDED.updated_by, updated_at = NOW()`, schema), s.Store, s.SlotMinutes, s.Capacity, s.Opens, s.Closes, firstNonEmpty(s.Days, "1234567"), s.ReminderChannel, s.ReminderTemplateID, actor)
	if err != nil {
		return nil, err
	}
	return GetCRMStoreSlots(tenantID, s.Store)
}

// CRMSlot is one bookable slot.
type CRMSlot struct {
	Start  string `json:"start"` // YYYY-MM-DD HH:MM, client time zone
	Booked int    `json:"booked"`
	Left   int    `json:"left"`
}

// ListCRMSlots lists a store's slots on a day with what is left.
func ListCRMSlots(tenantID, store, day string) ([]CRMSlot, error) {
	schema, err := crmSchema(tenantID)
	if err != nil {
		return nil, err
	}
	cfg, err := GetCRMStoreSlots(tenantID, store)
	if err != nil {
		return nil, err
	}
	loc := crmLocation(schema)
	d, err := time.ParseInLocation("2006-01-02", strings.TrimSpace(day), loc)
	if err != nil {
		return nil, crmErr(CRMCodeSlotFull, "the day is YYYY-MM-DD")
	}
	wd := int(d.Weekday())
	if wd == 0 {
		wd = 7
	}
	out := []CRMSlot{}
	if !strings.Contains(cfg.Days, fmt.Sprint(wd)) {
		return out, nil
	}
	o, _ := crmClock(cfg.Opens)
	c, _ := crmClock(cfg.Closes)
	booked := map[string]int{}
	local := crmDocLocalSQL("slot_start", crmZoneName(schema))
	rows, err := db.DB.Query(fmt.Sprintf(`SELECT to_char(%s, 'YYYY-MM-DD HH24:MI'), COUNT(*) FROM %s.crm_appointments
		WHERE store = $1 AND status = 'Booked' AND %s::date = $2::date GROUP BY 1`, local, schema, local), store, d.Format("2006-01-02"))
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var k string
		var n int
		if rows.Scan(&k, &n) == nil {
			booked[k] = n
		}
	}
	rows.Close()
	now := time.Now().In(loc)
	for m := o; m+cfg.SlotMinutes <= c; m += cfg.SlotMinutes {
		start := time.Date(d.Year(), d.Month(), d.Day(), m/60, m%60, 0, 0, loc)
		if start.Before(now) {
			continue
		}
		k := start.Format("2006-01-02 15:04")
		out = append(out, CRMSlot{Start: k, Booked: booked[k], Left: cfg.Capacity - booked[k]})
	}
	return out, nil
}

// CRMAppointment is one booking.
type CRMAppointment struct {
	ID           string    `json:"id"`
	CustomerID   string    `json:"customer_id"`
	CustomerName string    `json:"customer_name"`
	Store        string    `json:"store"`
	Kind         string    `json:"kind"`
	SlotStart    string    `json:"slot_start"`
	SlotMinutes  int       `json:"slot_minutes"`
	Status       string    `json:"status"`
	Note         string    `json:"note,omitempty"`
	Staff        string    `json:"staff,omitempty"`
	CreatedBy    string    `json:"created_by,omitempty"`
	CreatedAt    time.Time `json:"created_at"`
	CancelReason string    `json:"cancel_reason,omitempty"`
}

// CRMAppointmentInput books one.
type CRMAppointmentInput struct {
	CustomerID string `json:"customer_id"`
	Store      string `json:"store"`
	Kind       string `json:"kind"`
	SlotStart  string `json:"slot_start"`
	Note       string `json:"note"`
	Staff      string `json:"staff"`
}

// BookCRMAppointment books a slot. Two bookings cannot take its last place:
// the slot is locked while it is counted. Reminders are queued for a day and
// two hours before (service messages, through the gate); cancelling stops them.
func BookCRMAppointment(tenantID string, in CRMAppointmentInput, actor string) (*CRMAppointment, error) {
	schema, err := crmSchema(tenantID)
	if err != nil {
		return nil, err
	}
	c, err := crmLoadCustomer(schema, in.CustomerID)
	if err != nil {
		return nil, crmNotFound("customer", in.CustomerID)
	}
	kind := map[string]string{"visit": "Visit", "trial": "Trial", "fitting": "Fitting"}[strings.ToLower(strings.TrimSpace(in.Kind))]
	if kind == "" {
		kind = "Visit"
	}
	slot, err := crmParseLocal(schema, in.SlotStart)
	if err != nil || slot == "" {
		return nil, crmErr(CRMCodeSlotFull, "choose a slot")
	}
	slots, err := ListCRMSlots(tenantID, in.Store, slot[:10])
	if err != nil {
		return nil, err
	}
	found := false
	for i := range slots {
		found = found || slots[i].Start == slot
	}
	if !found {
		return nil, crmErr(CRMCodeSlotFull, "%s is not a slot at this store (closed, past, or not on the slot grid)", slot)
	}
	cfg, _ := GetCRMStoreSlots(tenantID, in.Store)
	zone := crmZoneName(schema)
	tx, err := db.DB.Begin()
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	if _, err := tx.Exec(`SELECT pg_advisory_xact_lock(hashtext($1))`, "crmslot:"+schema+":"+in.Store+":"+slot); err != nil {
		return nil, err
	}
	var booked int
	if err := tx.QueryRow(fmt.Sprintf(`SELECT COUNT(*) FROM %s.crm_appointments WHERE store = $1 AND status = 'Booked' AND slot_start = %s`, schema, crmLocalToDBSQL("$2", zone)),
		in.Store, slot).Scan(&booked); err != nil {
		return nil, err
	}
	if booked >= cfg.Capacity {
		return nil, crmErr(CRMCodeSlotFull, "that slot was just taken - choose another")
	}
	id := NewDocID("APT")
	if _, err := tx.Exec(fmt.Sprintf(`INSERT INTO %s.crm_appointments (id, customer_id, store, kind, slot_start, slot_minutes, note, staff, created_by)
		VALUES ($1, $2, $3, $4, %s, $6, NULLIF($7, ''), NULLIF($8, ''), $9)`, schema, crmLocalToDBSQL("$5", zone)),
		id, c.ID, in.Store, kind, slot, cfg.SlotMinutes, strings.TrimSpace(in.Note), in.Staff, actor); err != nil {
		return nil, err
	}
	when, _ := time.ParseInLocation("2006-01-02 15:04", slot, crmLocation(schema))
	storeName := crmStoreName(schema, in.Store)
	vars := map[string]string{"appointment.time": when.Format("Mon 2 Jan, 3:04 pm"), "appointment.store": storeName, "store.name": storeName}
	text := ""
	if cfg.ReminderTemplateID == "" {
		text = fmt.Sprintf("A reminder of your %s at %s on %s.", strings.ToLower(kind), storeName, vars["appointment.time"])
	}
	for _, lead := range []struct {
		key   string
		hours int
	}{{"24h", 24}, {"2h", 2}} {
		delay := int(time.Until(when.Add(-time.Duration(lead.hours) * time.Hour)).Seconds())
		if delay < 0 {
			continue
		}
		if _, _, err := crmQueueSend(tx, schema, CRMSendRequest{RunKind: "reminder", RunID: id + ":" + lead.key, CustomerID: c.ID, Channel: cfg.ReminderChannel,
			Purpose: CRMPurposeService, TemplateID: cfg.ReminderTemplateID, Body: text, Vars: vars, DelaySeconds: delay,
			ExpiresAtSQL: crmLocalToDBSQL("'"+slot+"'", zone), CreatedBy: actor}); err != nil {
			return nil, err
		}
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return GetCRMAppointment(tenantID, id)
}

func crmAppointmentSelect(schema string) string {
	return fmt.Sprintf(`SELECT a.id, a.customer_id, COALESCE(c.data->>'name', ''), a.store, a.kind, to_char(%s, 'YYYY-MM-DD HH24:MI'), a.slot_minutes, a.status,
		COALESCE(a.note, ''), COALESCE(a.staff, ''), COALESCE(a.created_by, ''), a.created_at, COALESCE(a.cancel_reason, '')
		FROM %s.crm_appointments a LEFT JOIN %s.documents c ON c.id = a.customer_id AND c.doctype = 'Customer'`, crmDocLocalSQL("a.slot_start", crmZoneName(schema)), schema, schema)
}

func crmScanAppointment(r interface{ Scan(...interface{}) error }) (CRMAppointment, error) {
	var a CRMAppointment
	err := r.Scan(&a.ID, &a.CustomerID, &a.CustomerName, &a.Store, &a.Kind, &a.SlotStart, &a.SlotMinutes, &a.Status, &a.Note, &a.Staff, &a.CreatedBy, &a.CreatedAt, &a.CancelReason)
	return a, err
}

// GetCRMAppointment returns one booking.
func GetCRMAppointment(tenantID, id string) (*CRMAppointment, error) {
	schema, err := crmSchema(tenantID)
	if err != nil {
		return nil, err
	}
	a, err := crmScanAppointment(db.DB.QueryRow(crmAppointmentSelect(schema)+` WHERE a.id = $1`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, crmNotFound("appointment", id)
	}
	return &a, err
}

// ListCRMAppointments lists bookings for a store from a day, or a customer's.
func ListCRMAppointments(tenantID, store, fromDay, customerID string) ([]CRMAppointment, error) {
	schema, err := crmSchema(tenantID)
	if err != nil {
		return nil, err
	}
	where := []string{"TRUE"}
	args := []interface{}{}
	if store != "" {
		args = append(args, store)
		where = append(where, fmt.Sprintf("a.store = $%d", len(args)))
	}
	if customerID != "" {
		args = append(args, customerID)
		where = append(where, fmt.Sprintf("a.customer_id = $%d", len(args)))
	}
	if fromDay != "" {
		args = append(args, fromDay)
		where = append(where, fmt.Sprintf("%s::date >= $%d::date", crmDocLocalSQL("a.slot_start", crmZoneName(schema)), len(args)))
	}
	rows, err := db.DB.Query(crmAppointmentSelect(schema)+` WHERE `+strings.Join(where, " AND ")+` ORDER BY a.slot_start LIMIT 300`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []CRMAppointment{}
	for rows.Next() {
		a, err := crmScanAppointment(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

// SetCRMAppointmentStatus cancels a booking (its reminders stop), or marks it
// done or a no-show.
func SetCRMAppointmentStatus(tenantID, id, action, reason, actor string) (*CRMAppointment, error) {
	schema, err := crmSchema(tenantID)
	if err != nil {
		return nil, err
	}
	status := map[string]string{"cancel": "Cancelled", "done": "Done", "no_show": "No-show"}[action]
	if status == "" {
		return nil, crmErr(CRMCodeSlotFull, "cancel, done or no_show")
	}
	if status == "Cancelled" && strings.TrimSpace(reason) == "" {
		return nil, crmErr(CRMCodeSlotFull, "say why it is cancelled")
	}
	tx, err := db.DB.Begin()
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	res, err := tx.Exec(fmt.Sprintf(`UPDATE %s.crm_appointments SET status = $2::text, cancel_reason = NULLIF($3, ''), cancelled_at = CASE WHEN $2::text = 'Cancelled' THEN LOCALTIMESTAMP END
		WHERE id = $1 AND status = 'Booked'`, schema), id, status, strings.TrimSpace(reason))
	if err != nil {
		return nil, err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return nil, crmErr(CRMCodeSlotFull, "only a booked appointment can change")
	}
	if _, err := tx.Exec(fmt.Sprintf(`UPDATE %s.crm_sends SET status = 'Cancelled', reason_code = 'JOURNEY_ENDED', reason_detail = 'The appointment is ' || lower($2),
		decided_at = LOCALTIMESTAMP, final_at = LOCALTIMESTAMP WHERE run_kind = 'reminder' AND run_id LIKE $1 || ':%%' AND status IN ('Queued', 'Deferred')`, schema), id, status); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return GetCRMAppointment(tenantID, id)
}

// --- hold requests (101.12.3) ----------------------------------------------------

// CRMHoldRequest is a customer's request to keep an item.
type CRMHoldRequest struct {
	ID            string     `json:"id"`
	CustomerID    string     `json:"customer_id"`
	CustomerName  string     `json:"customer_name"`
	ItemCode      string     `json:"item_code"`
	ItemName      string     `json:"item_name,omitempty"`
	Store         string     `json:"store"`
	Qty           float64    `json:"qty"`
	Status        string     `json:"status"`
	HoldUntil     string     `json:"hold_until,omitempty"`
	Note          string     `json:"note,omitempty"`
	RequestedBy   string     `json:"requested_by,omitempty"`
	DecidedBy     string     `json:"decided_by,omitempty"`
	DeclineReason string     `json:"decline_reason,omitempty"`
	ReadySendID   string     `json:"ready_send_id,omitempty"`
	CreatedAt     time.Time  `json:"created_at"`
	DecidedAt     *time.Time `json:"decided_at,omitempty"`
	StockHere     *float64   `json:"stock_here,omitempty"`
}

// RequestCRMHold asks a store to keep an item for a customer; the store's
// worklist gets a hold task.
func RequestCRMHold(tenantID, customerID, itemCode, store string, qty float64, holdUntil, note, actor string) (*CRMHoldRequest, error) {
	schema, err := crmSchema(tenantID)
	if err != nil {
		return nil, err
	}
	c, err := crmLoadCustomer(schema, customerID)
	if err != nil {
		return nil, crmNotFound("customer", customerID)
	}
	item, err := ResolveItemBySKU(tenantID, strings.TrimSpace(itemCode))
	if err != nil {
		return nil, crmErr(CRMCodeHold, "no item %q", itemCode)
	}
	if strings.TrimSpace(store) == "" {
		return nil, crmErr(CRMCodeHold, "choose the store that keeps it")
	}
	if qty <= 0 {
		qty = 1
	}
	var until interface{}
	if h := strings.TrimSpace(holdUntil); h != "" {
		if _, err := time.Parse("2006-01-02", h); err != nil {
			return nil, crmErr(CRMCodeHold, "keep-until is a date (YYYY-MM-DD)")
		}
		until = h
	}
	code := strField(item.Data, "code")
	id := NewDocID("HLD")
	if _, err := db.DB.Exec(fmt.Sprintf(`INSERT INTO %s.crm_hold_requests (id, customer_id, item_code, store, qty, hold_until, note, requested_by) VALUES ($1, $2, $3, $4, $5, $6, NULLIF($7, ''), $8)`, schema),
		id, c.ID, code, store, qty, until, strings.TrimSpace(note), actor); err != nil {
		return nil, err
	}
	if _, err := db.DB.Exec(fmt.Sprintf(`INSERT INTO %s.crm_tasks (id, kind, title, customer_id, store, ref_doctype, ref_id, created_by) VALUES ($1, 'hold', $2, $3, $4, 'CRMHoldRequest', $5, $6)`, schema),
		NewDocID("TSK"), truncateRunes(fmt.Sprintf("Keep %s for %s", firstNonEmpty(strField(item.Data, "name"), code), c.Name()), 300), c.ID, store, id, actor); err != nil {
		return nil, err
	}
	return GetCRMHold(tenantID, id)
}

// GetCRMHold returns one hold request.
func GetCRMHold(tenantID, id string) (*CRMHoldRequest, error) {
	list, err := ListCRMHolds(tenantID, map[string]string{"id": id})
	if err != nil {
		return nil, err
	}
	if len(list) == 0 {
		return nil, crmNotFound("hold request", id)
	}
	return &list[0], nil
}

// ListCRMHolds lists hold requests for a store, a customer, a status or one id.
func ListCRMHolds(tenantID string, f map[string]string) ([]CRMHoldRequest, error) {
	schema, err := crmSchema(tenantID)
	if err != nil {
		return nil, err
	}
	where := []string{"TRUE"}
	args := []interface{}{}
	for _, kv := range [][2]string{{"id", "h.id"}, {"store", "h.store"}, {"customer_id", "h.customer_id"}, {"status", "h.status"}} {
		if v := f[kv[0]]; v != "" {
			args = append(args, v)
			where = append(where, fmt.Sprintf("%s = $%d", kv[1], len(args)))
		}
	}
	rows, err := db.DB.Query(fmt.Sprintf(`SELECT h.id, h.customer_id, COALESCE(c.data->>'name', ''), h.item_code, COALESCE(i.data->>'name', i.data->>'item_name', ''), h.store, h.qty, h.status,
		COALESCE(to_char(h.hold_until, 'YYYY-MM-DD'), ''), COALESCE(h.note, ''), COALESCE(h.requested_by, ''), COALESCE(h.decided_by, ''), COALESCE(h.decline_reason, ''),
		COALESCE(h.ready_send_id, ''), h.created_at, h.decided_at
		FROM %[1]s.crm_hold_requests h LEFT JOIN %[1]s.documents c ON c.id = h.customer_id AND c.doctype = 'Customer'
		LEFT JOIN %[1]s.documents i ON i.doctype = 'Item' AND i.data->>'code' = h.item_code AND i.deleted_at IS NULL
		WHERE %[2]s ORDER BY (h.status = 'Requested') DESC, h.created_at DESC LIMIT 300`, schema, strings.Join(where, " AND ")), args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []CRMHoldRequest{}
	for rows.Next() {
		var h CRMHoldRequest
		var decided sql.NullTime
		if err := rows.Scan(&h.ID, &h.CustomerID, &h.CustomerName, &h.ItemCode, &h.ItemName, &h.Store, &h.Qty, &h.Status, &h.HoldUntil, &h.Note, &h.RequestedBy,
			&h.DecidedBy, &h.DeclineReason, &h.ReadySendID, &h.CreatedAt, &decided); err != nil {
			return nil, err
		}
		if decided.Valid {
			h.DecidedAt = &decided.Time
		}
		out = append(out, h)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	for i := range out {
		if access, err := crmFullStoreAccess(); err == nil {
			// Stage 49.3.2: the caller's own client - a schema lookup that
			// failed used to hand "" (the default client's stores) on.
			if stock, _, err := storeAppStock(tenantID, schema, access, out[i].Store, out[i].ItemCode); err == nil {
				for _, st := range stock {
					if st.Store == out[i].Store {
						v := float64(st.Available)
						out[i].StockHere = &v
					}
				}
			}
		}
	}
	return out, nil
}

// crmFullStoreAccess is store access to every store, for reading stock on a
// hold request (the request already names its store).
func crmFullStoreAccess() (StoreAccess, error) {
	return StoreAccess{All: true}, nil
}

// DecideCRMHold is the store's answer: confirm (the item is kept, and held in
// stock when a customer hold code is set) or decline with a reason.
func DecideCRMHold(tenantID, id string, confirm bool, reason, actor string) (*CRMHoldRequest, error) {
	schema, err := crmSchema(tenantID)
	if err != nil {
		return nil, err
	}
	h, err := GetCRMHold(tenantID, id)
	if err != nil {
		return nil, err
	}
	if h.Status != "Requested" {
		return nil, crmErr(CRMCodeHold, "this request is already %s", strings.ToLower(h.Status))
	}
	if !confirm {
		if strings.TrimSpace(reason) == "" {
			return nil, crmErr(CRMCodeHold, "say why it cannot be kept")
		}
		_, err = db.DB.Exec(fmt.Sprintf(`UPDATE %s.crm_hold_requests SET status = 'Declined', decline_reason = $2, decided_by = $3, decided_at = LOCALTIMESTAMP WHERE id = $1 AND status = 'Requested'`, schema),
			id, strings.TrimSpace(reason), actor)
	} else {
		if code := strings.TrimSpace(GetSettingString(tenantID, "crm.customer_hold_code")); code != "" {
			if _, err := PlaceHold(tenantID, code, h.ItemCode, h.Store, "", int(h.Qty), "Kept for customer "+h.CustomerName+" ("+id+")", actor); err != nil {
				return nil, crmErr(CRMCodeHold, "the stock could not be held: %v", err)
			}
		} else if h.StockHere != nil && *h.StockHere < h.Qty {
			return nil, crmErr(CRMCodeHold, "the store shows %.0f available - receive or move stock first, or decline", *h.StockHere)
		}
		_, err = db.DB.Exec(fmt.Sprintf(`UPDATE %s.crm_hold_requests SET status = 'Confirmed', decided_by = $2, decided_at = LOCALTIMESTAMP WHERE id = $1 AND status = 'Requested'`, schema), id, actor)
	}
	if err != nil {
		return nil, err
	}
	outcome := "Kept"
	if !confirm {
		outcome = "Declined: " + strings.TrimSpace(reason)
	}
	_, _ = db.DB.Exec(fmt.Sprintf(`UPDATE %s.crm_tasks SET status = 'Done', outcome = $2, done_at = LOCALTIMESTAMP, done_by = $3
		WHERE ref_doctype = 'CRMHoldRequest' AND ref_id = $1 AND status = 'Open'`, schema), id, outcome, actor)
	return GetCRMHold(tenantID, id)
}

// SendCRMHoldReady tells the customer their item is kept - only once the
// store has confirmed the hold ("no ready message until confirmed").
func SendCRMHoldReady(tenantID, id, channel, templateID, actor string) (*CRMSend, error) {
	schema, err := crmSchema(tenantID)
	if err != nil {
		return nil, err
	}
	h, err := GetCRMHold(tenantID, id)
	if err != nil {
		return nil, err
	}
	if h.Status != "Confirmed" {
		return nil, crmErr(CRMCodeHold, "the store has not confirmed this hold yet - nothing is sent to the customer until it does")
	}
	if h.ReadySendID != "" {
		return nil, crmErr(CRMCodeHold, "the customer was already told")
	}
	text := ""
	if templateID == "" {
		text = fmt.Sprintf("%s is kept for you at %s", firstNonEmpty(h.ItemName, h.ItemCode), crmStoreName(schema, h.Store))
		if h.HoldUntil != "" {
			text += " until " + h.HoldUntil
		}
		text += "."
	}
	send, err := SendCRMStaffMessage(tenantID, actor, CRMStaffMessage{CustomerID: h.CustomerID, Channel: channel, Purpose: CRMPurposeService, TemplateID: templateID, Text: text,
		Vars: map[string]string{"item.name": firstNonEmpty(h.ItemName, h.ItemCode), "store.name": crmStoreName(schema, h.Store)}})
	if err != nil {
		return nil, err
	}
	_, _ = db.DB.Exec(fmt.Sprintf(`UPDATE %s.crm_hold_requests SET ready_send_id = $2 WHERE id = $1`, schema), id, send.ID)
	_ = RecordCRMEvent(schema, h.CustomerID, CRMEventProductArrived, id, map[string]interface{}{"store": h.Store, "item.name": firstNonEmpty(h.ItemName, h.ItemCode)})
	return send, nil
}
