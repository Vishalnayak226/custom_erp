package engines

import (
	"custom_erp/db"
	"errors"
	"fmt"
	"strings"
	"time"
)

// Stage 101.1.2 - one timeline per customer: bills, returns, online orders,
// invoices, points, vouchers, messages with their send decision, tickets,
// notes, consent changes, feedback, appointments and journeys, by when they
// happened, newest first, each opening its own record. Paged by time: the
// next page asks for entries before the last one shown.

// CRMTimelineEntry is one thing that happened.
type CRMTimelineEntry struct {
	At         time.Time `json:"at"`
	Kind       string    `json:"kind"`
	Title      string    `json:"title"`
	Detail     string    `json:"detail,omitempty"`
	Amount     *float64  `json:"amount,omitempty"`
	RefDoctype string    `json:"ref_doctype,omitempty"`
	RefID      string    `json:"ref_id,omitempty"`
}

// CRMTimeline is one page of entries.
type CRMTimeline struct {
	Entries []CRMTimelineEntry `json:"entries"`
	More    bool               `json:"more"`
	Next    string             `json:"next,omitempty"`
}

// GetCRMTimeline returns up to limit entries before `before` (RFC 3339; blank
// for the newest), optionally only of the given kinds.
func GetCRMTimeline(tenantID, customerID, before string, limit int, kinds []string) (*CRMTimeline, error) {
	schema, err := crmSchema(tenantID)
	if err != nil {
		return nil, err
	}
	c, err := crmLoadCustomer(schema, customerID)
	if errors.Is(err, ErrCRMNotFound) {
		return nil, crmNotFound("customer", customerID)
	}
	if err != nil {
		return nil, err
	}
	if limit < 1 || limit > 100 {
		limit = 30
	}
	cutoff := time.Date(9999, 1, 1, 0, 0, 0, 0, time.UTC)
	if before = strings.TrimSpace(before); before != "" {
		t, err := time.Parse(time.RFC3339Nano, before)
		if err != nil {
			return nil, crmErr(CRMCodeCustomer, "the page marker is not a time")
		}
		cutoff = t
	}
	phone := strings.TrimSpace(strField(c.Data, "phone"))
	s := schema
	parts := map[string]string{
		"bill": fmt.Sprintf(`SELECT b.created_at, 'bill', 'Bill ' || COALESCE(NULLIF(b.data->>'bill_number', ''), b.id),
			COALESCE(NULLIF(b.data->>'location', '') || ' · ', '') || jsonb_array_length(%s)::text || ' line(s)', %s, 'POSCart', b.id
			FROM %s.documents b WHERE b.doctype = 'POSCart' AND b.status = 'Paid' AND b.deleted_at IS NULL AND b.data->>'customer_id' = $1`,
			crmItemsSQL("b"), crmBillAmountSQL("b"), s),
		"return": fmt.Sprintf(`SELECT r.created_at, 'return', 'Return ' || r.id, 'Bill ' || COALESCE(NULLIF(c.data->>'bill_number', ''), c.id) || ' · ' || r.status,
			%s, 'ReturnRequest', r.id
			FROM %s.documents r JOIN %s.documents c ON c.id = r.data->>'original_order_id' AND c.doctype = 'POSCart'
			WHERE r.doctype = 'ReturnRequest' AND r.deleted_at IS NULL AND c.data->>'customer_id' = $1`, crmNumSQL("r.data->>'total_refund_eligible'"), s, s),
		"invoice": fmt.Sprintf(`SELECT i.created_at, 'invoice', 'Invoice ' || COALESCE(NULLIF(i.data->>'invoice_number', ''), i.id), i.status,
			%s, 'SalesInvoice', i.id FROM %s.documents i WHERE i.doctype = 'SalesInvoice' AND i.deleted_at IS NULL AND i.data->>'customer' = $1`,
			crmNumSQL("i.data->>'total_amount'"), s),
		"points": fmt.Sprintf(`SELECT l.created_at, 'points',
			CASE WHEN l.transaction_type = 'Earn' THEN 'Earned ' ELSE 'Used ' END || l.points::text || ' point(s)',
			COALESCE(l.reference_doctype, '') || ' ' || COALESCE(l.reference_id, '') || CASE WHEN l.pending_until > NOW() THEN ' · pending until ' || to_char(l.pending_until, 'DD Mon') ELSE '' END,
			NULL::numeric, NULL::text, NULL::text FROM %s.loyalty_point_ledger l WHERE l.customer_id = $1 AND l.cancelled_at IS NULL`, s),
		"voucher": fmt.Sprintf(`SELECT v.created_at, 'voucher', 'Voucher ' || COALESCE(v.data->>'code', v.id),
			COALESCE(v.data->>'discount_value', '') || CASE WHEN v.data->>'discount_type' = 'Flat' THEN ' off' ELSE '%% off' END || ' · ' || v.status,
			NULL::numeric, 'Voucher', v.id FROM %s.documents v WHERE v.doctype = 'Voucher' AND v.deleted_at IS NULL AND v.data->>'customer_id' = $1`, s),
		"message": fmt.Sprintf(`SELECT m.created_at, 'message', m.channel || ' ' || m.purpose || ' message · ' || m.status,
			COALESCE(m.reason_code || ': ' || COALESCE(m.reason_detail, ''), left(COALESCE(m.rendered_body, ''), 160)),
			m.cost_actual, 'CRMSend', m.id FROM %s.crm_sends m WHERE m.customer_id = $1`, s),
		"ticket": fmt.Sprintf(`SELECT t.created_at, 'ticket', 'Ticket ' || COALESCE(NULLIF(t.data->>'code', ''), t.id) || ' · ' || t.status,
			left(COALESCE(t.data->>'description', ''), 160), NULL::numeric, 'ServiceTicket', t.id
			FROM %s.documents t WHERE t.doctype = 'ServiceTicket' AND t.deleted_at IS NULL AND t.data->>'customer' = $1`, s),
		"note": fmt.Sprintf(`SELECT n.created_at, 'note', 'Note by ' || COALESCE(n.created_by, 'staff'), left(n.note, 300), NULL::numeric, NULL::text, NULL::text
			FROM %s.crm_customer_notes n WHERE n.customer_id = $1`, s),
		"consent": fmt.Sprintf(`SELECT g.created_at, 'consent', initcap(g.action) || ': ' || g.channel || ' ' || g.purpose, g.source || COALESCE(' · ' || g.recorded_by, ''),
			NULL::numeric, NULL::text, NULL::text FROM %s.crm_consent_ledger g WHERE g.customer_id = $1`, s),
		"feedback": fmt.Sprintf(`SELECT COALESCE(f.responded_at, f.created_at), 'feedback',
			CASE WHEN f.score IS NULL THEN 'Feedback asked' ELSE 'Feedback score ' || f.score::text END, left(COALESCE(f.comment, ''), 200),
			NULL::numeric, 'POSCart', f.bill_id FROM %s.crm_feedback f WHERE f.customer_id = $1`, s),
		"appointment": fmt.Sprintf(`SELECT a.slot_start, 'appointment', a.kind || ' at ' || a.store || ' · ' || a.status, COALESCE(a.note, ''), NULL::numeric, 'CRMAppointment', a.id
			FROM %s.crm_appointments a WHERE a.customer_id = $1`, s),
		"task": fmt.Sprintf(`SELECT k.created_at, 'task', initcap(replace(k.kind, '_', ' ')) || ': ' || k.title, k.status || COALESCE(' · ' || k.outcome, ''), NULL::numeric, 'CRMTask', k.id
			FROM %s.crm_tasks k WHERE k.customer_id = $1`, s),
		"journey": fmt.Sprintf(`SELECT j.started_at, 'journey', 'Journey ' || COALESCE(jj.name, j.journey_id) || ' · ' || j.state, COALESCE(j.exit_reason, ''), NULL::numeric, 'CRMJourney', j.journey_id
			FROM %s.crm_journey_runs j LEFT JOIN %s.crm_journeys jj ON jj.id = j.journey_id WHERE j.customer_id = $1`, s, s),
	}
	if phone != "" {
		parts["order"] = fmt.Sprintf(`SELECT o.created_at, 'order', 'Order ' || COALESCE(NULLIF(o.data->>'channel_order_id', ''), o.id) || ' · ' || COALESCE(o.data->>'channel', ''),
			COALESCE(o.data->>'order_status', o.status), %s, 'SalesOrder', o.id
			FROM %s.documents o WHERE o.doctype = 'SalesOrder' AND o.deleted_at IS NULL
			  AND right(regexp_replace(COALESCE(o.data->>'customer_phone', ''), '[^0-9]', '', 'g'), 10) = right(regexp_replace($2, '[^0-9]', '', 'g'), 10)
			  AND length(regexp_replace($2, '[^0-9]', '', 'g')) >= 8`, crmNumSQL("o.data->>'total_amount'"), s)
	}
	want := map[string]bool{}
	for _, k := range kinds {
		if k = strings.TrimSpace(k); k != "" {
			want[k] = true
		}
	}
	var selects []string
	for _, k := range []string{"bill", "return", "order", "invoice", "points", "voucher", "message", "ticket", "note", "consent", "feedback", "appointment", "task", "journey"} {
		q, ok := parts[k]
		if !ok || (len(want) > 0 && !want[k]) {
			continue
		}
		selects = append(selects, "("+q+")")
	}
	if len(selects) == 0 {
		return &CRMTimeline{Entries: []CRMTimelineEntry{}}, nil
	}
	args := []interface{}{c.ID}
	if phone != "" {
		args = append(args, phone)
	} else {
		args = append(args, "")
	}
	args = append(args, cutoff.UTC().Format("2006-01-02 15:04:05.999999"), limit+1)
	// $2 is referenced only by the order part; keep it bound either way.
	query := fmt.Sprintf(`SELECT at, kind, title, COALESCE(detail, ''), amount, COALESCE(ref_doctype, ''), COALESCE(ref_id, '') FROM (%s) t(at, kind, title, detail, amount, ref_doctype, ref_id)
		WHERE at < $3::timestamp AND ($2::text IS NOT NULL) ORDER BY at DESC LIMIT $4`, strings.Join(selects, " UNION ALL "))
	rows, err := db.DB.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := &CRMTimeline{Entries: []CRMTimelineEntry{}}
	for rows.Next() {
		var e CRMTimelineEntry
		var amount *float64
		if err := rows.Scan(&e.At, &e.Kind, &e.Title, &e.Detail, &amount, &e.RefDoctype, &e.RefID); err != nil {
			return nil, err
		}
		if amount != nil {
			v := round2(*amount)
			e.Amount = &v
		}
		out.Entries = append(out.Entries, e)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if len(out.Entries) > limit {
		out.Entries = out.Entries[:limit]
		out.More = true
		out.Next = out.Entries[limit-1].At.Format(time.RFC3339Nano)
	}
	return out, nil
}
