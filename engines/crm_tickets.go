package engines

import (
	"custom_erp/db"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
)

// Stage 101.5.2 - tickets tied to the customer (the blueprint's M32), built
// on the Service Desk's ServiceTicket (37.8) rather than beside it. A ticket
// carries the customer, the bill and the store; it reads as
//
//	New -> Open -> Waiting on customer / Waiting internally -> Resolved -> Closed
//
// over ServiceTicket's own statuses (Draft = New, Assigned / InProgress =
// Open, waiting_on marks the waits), so its transition rules stay as they
// are. First-response and resolution times count working hours only
// (crm.business_hours / crm.business_days). Private notes stay inside the
// business; a reply is sent to the customer through the send gate. Tickets
// are on the customer's timeline.

// CRMTicketInput opens a ticket.
type CRMTicketInput struct {
	CustomerID  string `json:"customer_id"`
	BillID      string `json:"bill_id"`
	Store       string `json:"store"`
	Channel     string `json:"channel"`
	Description string `json:"description"`
	Priority    string `json:"priority"`
}

// CRMTicket is a ticket as the CRM shows it.
type CRMTicket struct {
	ID                string          `json:"id"`
	Status            string          `json:"status"`
	State             string          `json:"state"`
	Customer          string          `json:"customer"`
	CustomerName      string          `json:"customer_name"`
	BillID            string          `json:"bill_id,omitempty"`
	Store             string          `json:"store,omitempty"`
	Channel           string          `json:"channel,omitempty"`
	Description       string          `json:"description"`
	Priority          string          `json:"priority"`
	AssignedTo        string          `json:"assigned_to,omitempty"`
	CreatedAt         time.Time       `json:"created_at"`
	FirstResponseDue  time.Time       `json:"first_response_due"`
	FirstResponseAt   string          `json:"first_response_at,omitempty"`
	FirstResponseLate bool            `json:"first_response_late"`
	ResolveDue        time.Time       `json:"resolve_due"`
	ResolvedAt        string          `json:"resolved_at,omitempty"`
	ResolveLate       bool            `json:"resolve_late"`
	Notes             []CRMTicketNote `json:"notes,omitempty"`
}

// CRMTicketNote is a private note or a reply to the customer.
type CRMTicketNote struct {
	ID        int64     `json:"id"`
	Note      string    `json:"note"`
	Private   bool      `json:"private"`
	CreatedBy string    `json:"created_by"`
	CreatedAt time.Time `json:"created_at"`
}

// crmBusinessWindow parses the desk's hours and days.
func crmBusinessWindow(schema string) (open, close int, days map[time.Weekday]bool) {
	open, close = 10*60, 20*60
	parts := strings.Split(GetSettingStringForSchema(schema, "crm.business_hours"), "-")
	if len(parts) == 2 {
		if o, ok := crmClock(parts[0]); ok {
			if c, ok := crmClock(parts[1]); ok && c > o {
				open, close = o, c
			}
		}
	}
	days = map[time.Weekday]bool{}
	for _, ch := range GetSettingStringForSchema(schema, "crm.business_days") {
		if ch >= '1' && ch <= '7' {
			days[time.Weekday(int(ch-'0')%7)] = true
		}
	}
	if len(days) == 0 {
		for d := time.Sunday; d <= time.Saturday; d++ {
			days[d] = true
		}
	}
	return
}

// crmAddBusinessHours is start plus hours of desk time.
func crmAddBusinessHours(schema string, start time.Time, hours int) time.Time {
	loc := crmLocation(schema)
	open, close, days := crmBusinessWindow(schema)
	t := start.In(loc)
	left := time.Duration(hours) * time.Hour
	for i := 0; i < 400 && left > 0; i++ {
		dayStart := time.Date(t.Year(), t.Month(), t.Day(), open/60, open%60, 0, 0, loc)
		dayEnd := time.Date(t.Year(), t.Month(), t.Day(), close/60, close%60, 0, 0, loc)
		if !days[t.Weekday()] || !t.Before(dayEnd) {
			t = time.Date(t.Year(), t.Month(), t.Day()+1, open/60, open%60, 0, 0, loc)
			continue
		}
		if t.Before(dayStart) {
			t = dayStart
		}
		if avail := dayEnd.Sub(t); avail >= left {
			return t.Add(left)
		} else {
			left -= avail
			t = time.Date(t.Year(), t.Month(), t.Day()+1, open/60, open%60, 0, 0, loc)
		}
	}
	return t
}

// CreateCRMTicket opens a ticket for a customer, with its SLA due times.
func CreateCRMTicket(tenantID, actor string, in CRMTicketInput) (string, error) {
	schema, err := crmSchema(tenantID)
	if err != nil {
		return "", err
	}
	c, err := crmLoadCustomer(schema, in.CustomerID)
	if err != nil {
		return "", crmNotFound("customer", in.CustomerID)
	}
	desc := strings.TrimSpace(in.Description)
	if desc == "" {
		return "", crmErr(CRMCodeTicket, "say what the customer needs")
	}
	channel := firstNonEmpty(in.Channel, "Store")
	if !map[string]bool{"Store": true, "Phone": true, "WhatsApp": true, "Email": true, "Feedback": true}[channel] {
		return "", crmErr(CRMCodeTicket, "raised by Store, Phone, WhatsApp, Email or Feedback")
	}
	if in.BillID != "" {
		var owner string
		if err := db.DB.QueryRow(fmt.Sprintf(`SELECT COALESCE(data->>'customer_id', '') FROM %s.documents WHERE id = $1 AND doctype = 'POSCart'`, schema), in.BillID).Scan(&owner); err == nil && owner != c.ID {
			return "", crmErr(CRMCodeTicket, "that bill belongs to another customer")
		}
		if in.Store == "" {
			_ = db.DB.QueryRow(fmt.Sprintf(`SELECT COALESCE(data->>'location', '') FROM %s.documents WHERE id = $1`, schema), in.BillID).Scan(&in.Store)
		}
	}
	now := time.Now()
	respond := crmAddBusinessHours(schema, now, GetSettingIntForSchema(schema, "crm.ticket_first_response_hours"))
	resolve := crmAddBusinessHours(schema, now, GetSettingIntForSchema(schema, "crm.ticket_resolution_hours"))
	loc := crmLocation(schema)
	id, err := CreateServiceTicket(tenantID, c.ID, desc, firstNonEmpty(in.Priority, "Medium"), "", "", respond.In(loc).Format("2006-01-02"), resolve.In(loc).Format("2006-01-02"), actor)
	if err != nil {
		return "", crmErr(CRMCodeTicket, "%v", err)
	}
	patch := crmJSON(map[string]interface{}{"bill_id": in.BillID, "store": in.Store, "channel": channel,
		"first_response_due": respond.UTC().Format(time.RFC3339), "resolve_due": resolve.UTC().Format(time.RFC3339)})
	_, err = db.DB.Exec(fmt.Sprintf(`UPDATE %s.documents SET data = data || $2::jsonb WHERE id = $1`, schema), id, patch)
	return id, err
}

func crmTicketState(status, waiting string) string {
	switch status {
	case "Draft":
		return "New"
	case "Assigned", "InProgress":
		switch waiting {
		case "Customer":
			return "Waiting on customer"
		case "Internal":
			return "Waiting internally"
		}
		return "Open"
	}
	return status
}

func crmTicketFrom(schema, id, status string, raw []byte, created time.Time) CRMTicket {
	var d map[string]interface{}
	_ = json.Unmarshal(raw, &d)
	t := CRMTicket{ID: id, Status: status, State: crmTicketState(status, strField(d, "waiting_on")), Customer: strField(d, "customer"),
		BillID: strField(d, "bill_id"), Store: strField(d, "store"), Channel: strField(d, "channel"), Description: strField(d, "description"),
		Priority: strField(d, "priority"), AssignedTo: strField(d, "assigned_to"), CreatedAt: created,
		FirstResponseAt: strField(d, "first_response_at"), ResolvedAt: strField(d, "resolved_at")}
	parse := func(k string, def time.Time) time.Time {
		if v, err := time.Parse(time.RFC3339, strField(d, k)); err == nil {
			return v
		}
		return def
	}
	t.FirstResponseDue = parse("first_response_due", crmAddBusinessHours(schema, created, GetSettingIntForSchema(schema, "crm.ticket_first_response_hours")))
	t.ResolveDue = parse("resolve_due", crmAddBusinessHours(schema, created, GetSettingIntForSchema(schema, "crm.ticket_resolution_hours")))
	now := time.Now()
	if t.FirstResponseAt == "" {
		t.FirstResponseLate = now.After(t.FirstResponseDue) && !serviceTicketTerminalStatuses[status]
	} else if at, err := time.Parse(time.RFC3339, t.FirstResponseAt); err == nil {
		t.FirstResponseLate = at.After(t.FirstResponseDue)
	}
	if t.ResolvedAt == "" {
		t.ResolveLate = now.After(t.ResolveDue) && !serviceTicketTerminalStatuses[status]
	} else if at, err := time.Parse(time.RFC3339, t.ResolvedAt); err == nil {
		t.ResolveLate = at.After(t.ResolveDue)
	}
	return t
}

// ListCRMTickets lists tickets, open first, filtered by customer, store or
// state ("open" = every unresolved state, "late" = past an SLA).
func ListCRMTickets(tenantID string, f map[string]string) ([]CRMTicket, error) {
	schema, err := crmSchema(tenantID)
	if err != nil {
		return nil, err
	}
	where := []string{"t.doctype = 'ServiceTicket'", "t.deleted_at IS NULL"}
	args := []interface{}{}
	if v := f["customer_id"]; v != "" {
		args = append(args, v)
		where = append(where, fmt.Sprintf("t.data->>'customer' = $%d", len(args)))
	}
	if v := f["store"]; v != "" {
		args = append(args, v)
		where = append(where, fmt.Sprintf("t.data->>'store' = $%d", len(args)))
	}
	if f["state"] == "open" || f["state"] == "late" {
		where = append(where, "t.status NOT IN ('Resolved', 'Closed', 'Cancelled')")
	}
	rows, err := db.DB.Query(fmt.Sprintf(`SELECT t.id, t.status, t.data, t.created_at AT TIME ZONE current_setting('TimeZone'), COALESCE(c.data->>'name', '')
		FROM %s.documents t LEFT JOIN %s.documents c ON c.id = t.data->>'customer' AND c.doctype = 'Customer'
		WHERE %s ORDER BY (t.status IN ('Resolved', 'Closed', 'Cancelled')), t.created_at DESC LIMIT 300`, schema, schema, strings.Join(where, " AND ")), args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []CRMTicket{}
	for rows.Next() {
		var id, status, name string
		var raw []byte
		var created time.Time
		if err := rows.Scan(&id, &status, &raw, &created, &name); err != nil {
			return nil, err
		}
		t := crmTicketFrom(schema, id, status, raw, created)
		t.CustomerName = name
		if f["state"] == "late" && !t.FirstResponseLate && !t.ResolveLate {
			continue
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

// GetCRMTicket returns one ticket with its notes.
func GetCRMTicket(tenantID, id string) (*CRMTicket, error) {
	schema, err := crmSchema(tenantID)
	if err != nil {
		return nil, err
	}
	var status, name string
	var raw []byte
	var created time.Time
	err = db.DB.QueryRow(fmt.Sprintf(`SELECT t.status, t.data, t.created_at AT TIME ZONE current_setting('TimeZone'), COALESCE(c.data->>'name', '')
		FROM %s.documents t LEFT JOIN %s.documents c ON c.id = t.data->>'customer' AND c.doctype = 'Customer' WHERE t.id = $1 AND t.doctype = 'ServiceTicket'`, schema, schema), id).
		Scan(&status, &raw, &created, &name)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, crmNotFound("ticket", id)
	}
	if err != nil {
		return nil, err
	}
	t := crmTicketFrom(schema, id, status, raw, created)
	t.CustomerName = name
	rows, err := db.DB.Query(fmt.Sprintf(`SELECT id, note, private, COALESCE(created_by, ''), created_at FROM %s.crm_ticket_notes WHERE ticket_id = $1 ORDER BY id`, schema), id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	t.Notes = []CRMTicketNote{}
	for rows.Next() {
		var n CRMTicketNote
		if err := rows.Scan(&n.ID, &n.Note, &n.Private, &n.CreatedBy, &n.CreatedAt); err != nil {
			return nil, err
		}
		t.Notes = append(t.Notes, n)
	}
	return &t, rows.Err()
}

// AddCRMTicketNote adds a private note, or a reply that is sent to the
// customer by channel (through the gate). The first reply stamps the first
// response.
func AddCRMTicketNote(tenantID, ticketID, note string, private bool, channel, actor string) (*CRMTicket, error) {
	schema, err := crmSchema(tenantID)
	if err != nil {
		return nil, err
	}
	note = strings.TrimSpace(note)
	if note == "" {
		return nil, crmErr(CRMCodeTicket, "write the note first")
	}
	t, err := GetCRMTicket(tenantID, ticketID)
	if err != nil {
		return nil, err
	}
	if serviceTicketTerminalStatuses[t.Status] && !private {
		return nil, crmErr(CRMCodeTicket, "this ticket is %s - reopen it with a new ticket to reply", strings.ToLower(t.Status))
	}
	if !private {
		ch := crmChannel(channel)
		if ch == "" {
			return nil, crmErr(CRMCodeTicket, "choose how the reply goes to the customer (WhatsApp, SMS or Email)")
		}
		send, err := SendCRMStaffMessage(tenantID, actor, CRMStaffMessage{CustomerID: t.Customer, Channel: ch, Purpose: CRMPurposeService, Text: note,
			Subject: "About your request " + ticketID})
		if err != nil {
			return nil, err
		}
		if send.Status == crmSendSuppress || send.Status == crmSendCancelled || send.Status == crmSendExpired {
			return nil, crmErr(CRMCodeMessage, "the reply was not sent: %s", firstNonEmpty(send.ReasonLabel, send.Reason))
		}
	}
	if _, err := db.DB.Exec(fmt.Sprintf(`INSERT INTO %s.crm_ticket_notes (ticket_id, note, private, created_by) VALUES ($1, $2, $3, $4)`, schema), ticketID, note, private, actor); err != nil {
		return nil, err
	}
	if !private && t.FirstResponseAt == "" {
		_, _ = db.DB.Exec(fmt.Sprintf(`UPDATE %s.documents SET data = data || jsonb_build_object('first_response_at', $2::text) WHERE id = $1`, schema), ticketID, time.Now().UTC().Format(time.RFC3339))
	}
	return GetCRMTicket(tenantID, ticketID)
}

// SetCRMTicketState moves a ticket: assign, start, wait_customer,
// wait_internal, resume, resolve, close, cancel.
func SetCRMTicketState(tenantID, ticketID, action, value, actor string) (*CRMTicket, error) {
	schema, err := crmSchema(tenantID)
	if err != nil {
		return nil, err
	}
	t, err := GetCRMTicket(tenantID, ticketID)
	if err != nil {
		return nil, err
	}
	setField := func(k, v string) error {
		_, err := db.DB.Exec(fmt.Sprintf(`UPDATE %s.documents SET data = data || jsonb_build_object($2::text, $3::text), updated_at = CURRENT_TIMESTAMP WHERE id = $1`, schema), ticketID, k, v)
		return err
	}
	var opErr error
	switch action {
	case "assign":
		opErr = AssignServiceTicket(tenantID, ticketID, firstNonEmpty(strings.TrimSpace(value), actor))
	case "start":
		if t.Status == "Draft" {
			if opErr = AssignServiceTicket(tenantID, ticketID, actor); opErr != nil {
				break
			}
		}
		opErr = StartServiceTicket(tenantID, ticketID)
	case "wait_customer", "wait_internal":
		if t.Status != "Assigned" && t.Status != "InProgress" {
			opErr = fmt.Errorf("only an open ticket can wait")
			break
		}
		opErr = setField("waiting_on", map[string]string{"wait_customer": "Customer", "wait_internal": "Internal"}[action])
	case "resume":
		opErr = setField("waiting_on", "")
	case "resolve":
		if t.Status == "Assigned" {
			if opErr = StartServiceTicket(tenantID, ticketID); opErr != nil {
				break
			}
		}
		if opErr = ResolveServiceTicket(tenantID, ticketID, strings.TrimSpace(value)); opErr == nil {
			_ = setField("resolved_at", time.Now().UTC().Format(time.RFC3339))
			_ = setField("waiting_on", "")
		}
	case "close":
		opErr = CloseServiceTicket(tenantID, ticketID)
	case "cancel":
		opErr = CancelServiceTicket(tenantID, ticketID, strings.TrimSpace(value))
	default:
		opErr = fmt.Errorf("unknown action %q", action)
	}
	if opErr != nil {
		return nil, crmErr(CRMCodeTicket, "%v", opErr)
	}
	return GetCRMTicket(tenantID, ticketID)
}
