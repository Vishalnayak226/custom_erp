package engines

import (
	"custom_erp/db"
	"encoding/json"
	"fmt"
	"reflect"
	"regexp"
	"strings"
	"sync"
	"time"
)

// Stage 75.2.7 / 75.2.10: what may change once an IRN exists, cancellation and
// manual import under maker-checker, the 30-day rule's warnings, the ageing
// alerts, the daily reconciliation and the three catalog reports.

const (
	IRNCancellationDoctype = "IRNCancellation"
	IRNManualImportDoctype = "IRNManualImport"
)

// irpTaxFields are what an acknowledged document can no longer change: the
// IRN certifies them. Anything else (a due date, dunning) still saves.
var irpTaxFields = map[string][]string{
	"SalesInvoice": {"invoice_number", "customer", "location", "items", "taxable_amount", "non_taxable", "cgst", "sgst", "igst",
		"total_tax", "total_amount", "currency", "exchange_rate", "supply_type", "sales_order_id", "invoice_date"},
	"CreditNote": {"note_number", "customer_id", "amount", "sales_invoice_id", "taxable_amount", "cgst", "sgst", "igst", "note_date"},
}

// validateEInvoiceLock refuses a change to the tax fields of a document whose
// IRN has been generated (or is being cancelled). Runs at the generic save's
// choke point (ValidateTransactionalRules).
func validateEInvoiceLock(tenantID, doctype, docID string, priorData, payload map[string]interface{}) error {
	fields, ok := irpTaxFields[doctype]
	if !ok || docID == "" || priorData == nil {
		return nil
	}
	r, err := GetIRPRecordFor(tenantID, doctype, docID)
	if err != nil || r == nil {
		return nil // no e-invoice tables, or not registered: nothing to lock
	}
	switch r.State {
	case IRPStateAcknowledged, IRPStateCancelPending, IRPStateCancelUnknown, IRPStateSubmitting, IRPStateUnknown, IRPStateManualImport:
	default:
		return nil
	}
	for _, f := range fields {
		next, sent := payload[f]
		if !sent {
			continue
		}
		if !reflect.DeepEqual(normalizeForCompare(priorData[f]), normalizeForCompare(next)) {
			return &ValidationError{Code: "EINV-0983", SubFor: f, Message: fmt.Sprintf("%s %s has an IRN (%s) - its %s cannot change. Cancel the IRN within 24 hours, or issue a credit / debit note", doctype, r.DocNumber, r.State, f)}
		}
	}
	return nil
}

func normalizeForCompare(v interface{}) interface{} {
	switch t := v.(type) {
	case nil:
		return ""
	case float64:
		return fmt.Sprintf("%.2f", t)
	case int:
		return fmt.Sprintf("%.2f", float64(t))
	case string:
		return strings.TrimSpace(t)
	}
	b, _ := json.Marshal(v)
	return string(b)
}

// irpTaxRole is who may ask for a cancel or a manual import.
func irpTaxRole(role string) bool {
	return HasFullAccess(role) || RoleHasCapability(role, "finance.statements")
}

var irpCancelCodes = map[string]bool{"1": true, "2": true, "3": true, "4": true}

// RequestIRNCancellation (75.2.7) asks to cancel an acknowledged IRN: within
// 24 hours of acknowledgement, by the tax role, approved by another person.
// After 24 hours the IRP refuses a cancel - the record goes to Manual review
// and the correction is a credit note.
func RequestIRNCancellation(tenantID string, recordID int64, code, reason, userID, role string) (IRPRecord, error) {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return IRPRecord{}, err
	}
	r, err := getIRPRecord(schema, recordID)
	if err != nil {
		return r, err
	}
	if !irpTaxRole(role) {
		return r, einvoiceError("EINV-0984", "Role", "only the tax role (Accountant) or an admin can ask to cancel an IRN")
	}
	if r.State != IRPStateAcknowledged {
		return r, einvoiceError("EINV-0984", "State", fmt.Sprintf("only an acknowledged IRN can be cancelled (this one is %s)", r.State))
	}
	code = strings.TrimSpace(strings.SplitN(code, " ", 2)[0])
	if !irpCancelCodes[code] {
		return r, einvoiceError("EINV-0984", "Reason code", "choose a reason code: 1 Duplicate, 2 Data entry mistake, 3 Order cancelled, 4 Others")
	}
	if reason = strings.TrimSpace(reason); len(reason) < 5 {
		return r, einvoiceError("EINV-0984", "Reason", "give the reason for cancelling (at least a few words)")
	}
	if r.AckDate != nil && time.Since(*r.AckDate) > 24*time.Hour {
		irpTransition(schema, r, IRPStateManualReview, "CANCEL_TOO_LATE", reason, "cancellation asked "+time.Since(*r.AckDate).Round(time.Hour).String()+" after acknowledgement - past the IRP's 24 hours: issue a credit note", 0, "")
		return getIRPRecord(schema, r.ID)
	}
	docID := NewDocID("IRNCAN")
	data, _ := json.Marshal(map[string]interface{}{"irp_document": fmt.Sprint(r.ID), "document_number": r.DocNumber, "irn": r.IRN,
		"cancel_code": code, "reason": reason, "requested_by": userID, "status": "Draft"})
	if _, err := db.DB.Exec(fmt.Sprintf(`INSERT INTO %s.documents (id, doctype, data, status, created_by) VALUES ($1, $2, $3, 'Draft', $4)`, schema), docID, IRNCancellationDoctype, data, userID); err != nil {
		return r, err
	}
	if err := SubmitForApproval(tenantID, IRNCancellationDoctype, docID, userID, role); err != nil {
		return r, err
	}
	db.DB.Exec(fmt.Sprintf(`UPDATE %s.irp_documents SET cancel_reason = $2, updated_at = NOW() WHERE id = $1`, schema), r.ID, code+"|"+reason)
	irpEvent(schema, r.ID, r.GSTIN, "CANCEL_REQUESTED", r.State, r.State, docID+": "+reason, userID)
	return getIRPRecord(schema, r.ID)
}

// RequestIRNManualImport (75.2.9) records an IRN made on the IRP's portal
// during an outage. A second person verifies it against the portal before it
// counts (dual verification).
func RequestIRNManualImport(tenantID string, recordID int64, irn, ackNo, ackDate, signedQR, reason, userID, role string) (IRPRecord, error) {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return IRPRecord{}, err
	}
	r, err := getIRPRecord(schema, recordID)
	if err != nil {
		return r, err
	}
	if !irpTaxRole(role) {
		return r, einvoiceError("EINV-0985", "Role", "only the tax role (Accountant) or an admin can enter a portal IRN")
	}
	switch r.State {
	case IRPStateReady, IRPStateQueued, IRPStateRejected, IRPStateUnknown, IRPStateBlockedConfig, IRPStateManualReview:
	default:
		return r, einvoiceError("EINV-0985", "State", fmt.Sprintf("a %s e-invoice cannot take a portal IRN", r.State))
	}
	irn = strings.ToLower(strings.TrimSpace(irn))
	if !regexp.MustCompile(`^[0-9a-f]{64}$`).MatchString(irn) {
		return r, einvoiceError("EINV-0985", "IRN", "the IRN is 64 letters and digits (0-9, a-f), as the portal shows it")
	}
	if strings.TrimSpace(ackNo) == "" || strings.TrimSpace(signedQR) == "" || strings.TrimSpace(reason) == "" {
		return r, einvoiceError("EINV-0985", "Details", "enter the Ack No, Ack Date, the signed QR text and why it was made on the portal")
	}
	if _, err := parseIRPAckDate(ackDate); err != nil {
		return r, einvoiceError("EINV-0985", "Ack Date", "the Ack Date is a date and time, as the portal shows it (YYYY-MM-DD HH:MM)")
	}
	docID := NewDocID("IRNIMP")
	data, _ := json.Marshal(map[string]interface{}{"irp_document": fmt.Sprint(r.ID), "document_number": r.DocNumber, "irn": irn, "ack_no": strings.TrimSpace(ackNo),
		"ack_date": strings.TrimSpace(ackDate), "signed_qr": strings.TrimSpace(signedQR), "reason": strings.TrimSpace(reason), "requested_by": userID,
		"previous_state": r.State, "status": "Draft"})
	if _, err := db.DB.Exec(fmt.Sprintf(`INSERT INTO %s.documents (id, doctype, data, status, created_by) VALUES ($1, $2, $3, 'Draft', $4)`, schema), docID, IRNManualImportDoctype, data, userID); err != nil {
		return r, err
	}
	if err := SubmitForApproval(tenantID, IRNManualImportDoctype, docID, userID, role); err != nil {
		return r, err
	}
	irpTransition(schema, r, IRPStateManualImport, "MANUAL_IMPORT_REQUESTED", docID, "waiting for a second person to check the portal IRN ("+docID+")", 0, "")
	return getIRPRecord(schema, r.ID)
}

func parseIRPAckDate(s string) (time.Time, error) {
	s = strings.TrimSpace(s)
	for _, layout := range []string{"2006-01-02 15:04:05", "2006-01-02 15:04", "2006-01-02T15:04", "2006-01-02T15:04:05Z07:00", "02/01/2006 15:04:05", "02/01/2006 15:04"} {
		if t, err := time.Parse(layout, s); err == nil {
			return t, nil
		}
	}
	return time.Time{}, fmt.Errorf("not a date and time")
}

func init() {
	RegisterApprovalDecisionHook(IRNCancellationDoctype, true, applyIRNCancellationDecision)
	RegisterApprovalDecisionHook(IRNManualImportDoctype, true, applyIRNManualImportDecision)
}

func irpRequestDoc(tenantID, doctype, docID string) (map[string]interface{}, string, int64, IRPRecord, bool) {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return nil, "", 0, IRPRecord{}, false
	}
	data, _, err := fetchDocData(tenantID, doctype, docID)
	if err != nil || data == nil {
		return nil, schema, 0, IRPRecord{}, false
	}
	var id int64
	fmt.Sscanf(strField(data, "irp_document"), "%d", &id)
	r, err := getIRPRecord(schema, id)
	if err != nil {
		return nil, schema, 0, r, false
	}
	return data, schema, id, r, true
}

// applyIRNCancellationDecision: approved inside the window, the cancel is
// sent; approved too late, the record goes to Manual review.
func applyIRNCancellationDecision(tenantID, docID, decision, actor string) {
	data, schema, _, r, ok := irpRequestDoc(tenantID, IRNCancellationDoctype, docID)
	if !ok {
		return
	}
	if decision != "Approved" {
		irpEvent(schema, r.ID, r.GSTIN, "CANCEL_REJECTED", r.State, r.State, docID, actor)
		return
	}
	if r.State != IRPStateAcknowledged {
		return
	}
	if r.AckDate != nil && time.Since(*r.AckDate) > 24*time.Hour {
		irpTransition(schema, r, IRPStateManualReview, "CANCEL_TOO_LATE", docID, "the cancellation was approved after the IRP's 24 hours - issue a credit note", 0, "")
		return
	}
	irpTransition(schema, r, IRPStateCancelPending, "CANCEL_APPROVED", docID+" by "+actor, "", time.Millisecond, ", cancel_reason = $5", strField(data, "cancel_code")+"|"+strField(data, "reason"))
	irpKickWorker(schema)
}

// applyIRNManualImportDecision: verified, the portal IRN is the document's;
// refused, the record returns to where it was.
func applyIRNManualImportDecision(tenantID, docID, decision, actor string) {
	data, schema, _, r, ok := irpRequestDoc(tenantID, IRNManualImportDoctype, docID)
	if !ok || r.State != IRPStateManualImport {
		return
	}
	if decision != "Approved" {
		irpTransition(schema, r, firstNonEmpty(strField(data, "previous_state"), IRPStateManualReview), "MANUAL_IMPORT_REJECTED", docID+" by "+actor, "the portal IRN was not confirmed by the checker", 0, "")
		return
	}
	at, _ := parseIRPAckDate(strField(data, "ack_date"))
	irpTransition(schema, r, IRPStateAcknowledged, "MANUAL_IMPORT_VERIFIED", docID+" verified by "+actor, "", 0,
		", irn = $5, ack_no = $6, ack_date = $7, signed_qr = $8, source = 'manual'", strField(data, "irn"), strField(data, "ack_no"), at, strField(data, "signed_qr"))
}

// ---------------------------------------------------------------------------
// Alerts and the daily reconciliation (75.2.10)
// ---------------------------------------------------------------------------

// irpAlertOnce sends an alert for one record and condition once: the event
// log is the memory, so a restart does not repeat it.
func irpAlertOnce(schema string, r IRPRecord, code, message string) {
	var sent bool
	if err := db.DB.QueryRow(fmt.Sprintf(`SELECT EXISTS(SELECT 1 FROM %s.irp_events WHERE document_id = $1 AND event = $2)`, schema), r.ID, "ALERT_"+code).Scan(&sent); err != nil || sent {
		return
	}
	irpEvent(schema, r.ID, r.GSTIN, "ALERT_"+code, r.State, r.State, message, "worker")
	SendOpsAlert(code, "einvoice", schema+": "+message)
}

// irpCheckAlerts raises IRP_UNKNOWN_AGING (> 2 min), IRP_QUEUE_AGING
// (> 5 min), IRP_CANCEL_WINDOW (< 2 h of the 24 left on an asked cancel) and
// IRP_30DAY_RISK (7, 25 and 28 days on a 10CR+ GSTIN's unreported document).
func irpCheckAlerts(schema string) {
	rows, err := db.DB.Query(fmt.Sprintf(`
		SELECT %s FROM %s.irp_documents d
		WHERE environment = $1 AND (
			(state = 'UNKNOWN' AND updated_at < NOW() - INTERVAL '2 minutes')
			OR (state = 'QUEUED' AND created_at < NOW() - INTERVAL '5 minutes')
			OR (state IN ('ACKNOWLEDGED','CANCEL_PENDING','CANCEL_UNKNOWN') AND cancel_reason <> '' AND cancelled_at IS NULL AND ack_date < NOW() - INTERVAL '22 hours')
			OR (state IN ('READY','QUEUED','REJECTED','UNKNOWN','BLOCKED_CONFIG') AND doc_date <= public.erp_today() - 7
				AND EXISTS (SELECT 1 FROM %s.irp_profiles p WHERE p.environment = d.environment AND p.gstin = d.gstin AND p.turnover_band = '10CR+')))
		LIMIT 200`, irpRecordColumns, schema, schema), IRPEnvironment())
	if err != nil {
		return
	}
	var recs []IRPRecord
	for rows.Next() {
		if r, err := scanIRPRecord(rows); err == nil {
			recs = append(recs, r)
		}
	}
	rows.Close()
	for _, r := range recs {
		switch {
		case r.State == IRPStateUnknown && time.Since(r.UpdatedAt) > 2*time.Minute:
			irpAlertOnce(schema, r, "IRP_UNKNOWN_AGING", fmt.Sprintf("%s %s has been UNKNOWN for %d min - the IRP did not answer; it is being looked up", r.DocType, r.DocNumber, r.AgeMinutes))
		case r.State == IRPStateQueued && time.Since(r.CreatedAt) > 5*time.Minute:
			irpAlertOnce(schema, r, "IRP_QUEUE_AGING", fmt.Sprintf("%s %s has waited in the IRN queue over 5 min (%d attempts)", r.DocType, r.DocNumber, r.Attempts))
		case r.CancelReason != "" && r.CancelledAt == nil && r.AckDate != nil:
			irpAlertOnce(schema, r, "IRP_CANCEL_WINDOW", fmt.Sprintf("%s %s: under 2 hours left to cancel its IRN (asked, not yet cancelled)", r.DocType, r.DocNumber))
		}
		if d, err := time.Parse("2006-01-02", r.DocDate); err == nil {
			age := int(time.Since(d).Hours() / 24)
			for _, mark := range []int{28, 25, 7} {
				if age >= mark {
					irpAlertOnce(schema, r, fmt.Sprintf("IRP_30DAY_RISK_%d", mark), fmt.Sprintf("%s %s is %d days old without an IRN - the IRP refuses it after 30 days", r.DocType, r.DocNumber, age))
					break
				}
			}
		}
	}
}

// EInvoiceGSTINSummary is one GSTIN's reconciliation line.
type EInvoiceGSTINSummary struct {
	GSTIN        string `json:"gstin"`
	Expected     int    `json:"expected"`
	Acknowledged int    `json:"acknowledged"`
	Pending      int    `json:"pending"`
	Unknown      int    `json:"unknown"`
	Rejected     int    `json:"rejected"`
	Cancelled    int    `json:"cancelled"`
	NeedsAction  int    `json:"needs_action"`
	NotRequired  int    `json:"not_required"`
}

// ReconcileEInvoices compares, per GSTIN, the documents that must have an IRN
// with those that do.
func ReconcileEInvoices(tenantID string) ([]EInvoiceGSTINSummary, error) {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return nil, err
	}
	rows, err := db.DB.Query(fmt.Sprintf(`
		SELECT gstin,
			COUNT(*) FILTER (WHERE state <> 'NOT_REQUIRED'),
			COUNT(*) FILTER (WHERE state = 'ACKNOWLEDGED'),
			COUNT(*) FILTER (WHERE state IN ('READY','QUEUED','SUBMITTING')),
			COUNT(*) FILTER (WHERE state IN ('UNKNOWN','CANCEL_UNKNOWN','RECONCILE')),
			COUNT(*) FILTER (WHERE state = 'REJECTED'),
			COUNT(*) FILTER (WHERE state = 'CANCELLED'),
			COUNT(*) FILTER (WHERE state IN ('MANUAL_REVIEW','BLOCKED_CONFIG','MANUAL_IMPORT','REJECTED')),
			COUNT(*) FILTER (WHERE state = 'NOT_REQUIRED')
		FROM %s.irp_documents WHERE environment = $1 GROUP BY gstin ORDER BY gstin`, schema), IRPEnvironment())
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []EInvoiceGSTINSummary{}
	for rows.Next() {
		var s EInvoiceGSTINSummary
		if err := rows.Scan(&s.GSTIN, &s.Expected, &s.Acknowledged, &s.Pending, &s.Unknown, &s.Rejected, &s.Cancelled, &s.NeedsAction, &s.NotRequired); err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	return out, rows.Err()
}

var irpLastReconcile sync.Map // schema -> time.Time

// irpDailyReconcile records the day's reconciliation once a day per tenant.
func irpDailyReconcile(tenantID, schema string) {
	if v, ok := irpLastReconcile.Load(schema); ok && time.Since(v.(time.Time)) < 24*time.Hour {
		return
	}
	irpLastReconcile.Store(schema, time.Now())
	sums, err := ReconcileEInvoices(tenantID)
	if err != nil {
		return
	}
	for _, s := range sums {
		gap := s.Expected - s.Acknowledged - s.Cancelled
		irpEvent(schema, 0, s.GSTIN, "RECONCILED", "", "", fmt.Sprintf("expected %d, acknowledged %d, cancelled %d, waiting %d, unknown %d, needs action %d", s.Expected, s.Acknowledged, s.Cancelled, s.Pending, s.Unknown, s.NeedsAction), "worker")
		if s.NeedsAction > 0 || s.Unknown > 0 {
			SendOpsAlert("IRP_RECONCILIATION", "einvoice", fmt.Sprintf("%s GSTIN %s: %d document(s) without an IRN (%d need action, %d unknown)", schema, s.GSTIN, gap, s.NeedsAction, s.Unknown))
		}
	}
}

// ---------------------------------------------------------------------------
// Workbench list and reports
// ---------------------------------------------------------------------------

// EInvoiceWorkbenchTabs are the workbench's tabs and the states each shows.
var EInvoiceWorkbenchTabs = map[string][]string{
	"needs_action": {IRPStateReady, IRPStateRejected, IRPStateManualReview, IRPStateBlockedConfig, IRPStateManualImport},
	"queued":       {IRPStateQueued, IRPStateSubmitting},
	"unknown":      {IRPStateUnknown, IRPStateCancelUnknown, IRPStateReconcile},
	"rejected":     {IRPStateRejected},
	"acknowledged": {IRPStateAcknowledged, IRPStateCancelPending},
	"cancelled":    {IRPStateCancelled},
}

// EInvoiceFilter narrows the workbench and the reports.
type EInvoiceFilter struct {
	Tab, GSTIN, From, To, Source, Search string
	MinAgeMinutes, Limit, Offset         int
}

// ListEInvoiceRecords is the workbench's list.
func ListEInvoiceRecords(tenantID string, f EInvoiceFilter) ([]IRPRecord, int, error) {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return nil, 0, err
	}
	where := []string{"environment = $1"}
	args := []interface{}{IRPEnvironment()}
	add := func(cond string, v interface{}) {
		args = append(args, v)
		where = append(where, fmt.Sprintf(cond, len(args)))
	}
	if states, ok := EInvoiceWorkbenchTabs[f.Tab]; ok {
		add("state = ANY($%d)", pqStringArray(states))
	} else if f.Tab == "not_required" {
		add("state = $%d", IRPStateNotRequired)
	} else {
		add("state <> $%d", IRPStateNotRequired)
	}
	if f.GSTIN != "" {
		add("gstin = $%d", strings.ToUpper(f.GSTIN))
	}
	if f.From != "" {
		add("doc_date >= $%d::date", f.From)
	}
	if f.To != "" {
		add("doc_date <= $%d::date", f.To)
	}
	if f.Source != "" {
		add("source_doctype = $%d", f.Source)
	}
	if f.Search != "" {
		add("(doc_number ILIKE '%%' || $%d || '%%' OR irn ILIKE $%[1]d || '%%')", f.Search)
	}
	if f.MinAgeMinutes > 0 {
		add("updated_at <= NOW() - ($%d || ' minutes')::interval", fmt.Sprint(f.MinAgeMinutes))
	}
	if f.Limit <= 0 || f.Limit > 200 {
		f.Limit = 50
	}
	cond := strings.Join(where, " AND ")
	var total int
	if err := db.DB.QueryRow(fmt.Sprintf(`SELECT COUNT(*) FROM %s.irp_documents WHERE %s`, schema, cond), args...).Scan(&total); err != nil {
		return nil, 0, err
	}
	rows, err := db.DB.Query(fmt.Sprintf(`SELECT %s FROM %s.irp_documents WHERE %s ORDER BY updated_at DESC LIMIT %d OFFSET %d`, irpRecordColumns, schema, cond, f.Limit, f.Offset), args...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	out := []IRPRecord{}
	for rows.Next() {
		r, err := scanIRPRecord(rows)
		if err != nil {
			return nil, 0, err
		}
		out = append(out, r)
	}
	return out, total, rows.Err()
}

// EInvoiceRecordDetail is one record with its attempts and history.
type EInvoiceRecordDetail struct {
	Record   IRPRecord                `json:"record"`
	Attempts []map[string]interface{} `json:"attempts"`
	Events   []map[string]interface{} `json:"events"`
	Payload  json.RawMessage          `json:"payload,omitempty"`
}

// GetEInvoiceRecordDetail returns a record, its attempts and its events.
func GetEInvoiceRecordDetail(tenantID string, id int64) (EInvoiceRecordDetail, error) {
	var d EInvoiceRecordDetail
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return d, err
	}
	if d.Record, err = getIRPRecord(schema, id); err != nil {
		return d, err
	}
	var payload []byte
	_ = db.DB.QueryRow(fmt.Sprintf(`SELECT COALESCE(payload::text, '') FROM %s.irp_documents WHERE id = $1`, schema), id).Scan(&payload)
	if len(payload) > 0 {
		d.Payload = payload
	}
	d.Attempts = irpRowsAsMaps(fmt.Sprintf(`SELECT action, outcome, error_code, error_message, duration_ms, at FROM %s.irp_attempts WHERE document_id = $1 ORDER BY id DESC LIMIT 50`, schema), id)
	d.Events = irpRowsAsMaps(fmt.Sprintf(`SELECT event, from_state, to_state, detail, actor, at FROM %s.irp_events WHERE document_id = $1 ORDER BY id DESC LIMIT 100`, schema), id)
	return d, nil
}

func irpRowsAsMaps(q string, args ...interface{}) []map[string]interface{} {
	out := []map[string]interface{}{}
	rows, err := db.DB.Query(q, args...)
	if err != nil {
		return out
	}
	defer rows.Close()
	cols, _ := rows.Columns()
	for rows.Next() {
		vals := make([]interface{}, len(cols))
		ptrs := make([]interface{}, len(cols))
		for i := range vals {
			ptrs[i] = &vals[i]
		}
		if rows.Scan(ptrs...) != nil {
			continue
		}
		m := map[string]interface{}{}
		for i, c := range cols {
			if b, ok := vals[i].([]byte); ok {
				m[c] = string(b)
			} else {
				m[c] = vals[i]
			}
		}
		out = append(out, m)
	}
	return out
}

func irpReportRows(tenantID string, f EInvoiceFilter, row func(IRPRecord) map[string]interface{}) ([]map[string]interface{}, error) {
	f.Limit = 200
	var out []map[string]interface{}
	for {
		recs, total, err := ListEInvoiceRecords(tenantID, f)
		if err != nil {
			return nil, err
		}
		for _, r := range recs {
			if m := row(r); m != nil {
				out = append(out, m)
			}
		}
		f.Offset += len(recs)
		if len(recs) == 0 || f.Offset >= total || f.Offset >= 5000 {
			break
		}
	}
	return out, nil
}

func irpTimeText(t *time.Time) string {
	if t == nil {
		return ""
	}
	return t.Format("2006-01-02 15:04")
}

func init() {
	dateParams := []ReportParam{{Key: "from", Label: "From", Type: "date"}, {Key: "to", Label: "To", Type: "date"}, {Key: "gstin", Label: "GSTIN", Type: "text"}}
	RegisterReport(ReportDefinition{
		ID: "einvoice-irn-register", Label: "E-Invoice / IRN Status Register", Category: "Finance",
		Description: "Every invoice and credit note that must have an IRN, its state, IRN and acknowledgement.",
		Columns: []ReportColumn{{Key: "doc_date", Label: "Date"}, {Key: "doc_type", Label: "Type"}, {Key: "doc_number", Label: "Number"},
			{Key: "gstin", Label: "Seller GSTIN"}, {Key: "state", Label: "IRN state"}, {Key: "irn", Label: "IRN"}, {Key: "ack_no", Label: "Ack No"},
			{Key: "ack_date", Label: "Ack Date"}, {Key: "source", Label: "Made by"}, {Key: "cancelled_at", Label: "Cancelled"}},
		Params: append(dateParams, ReportParam{Key: "state", Label: "State", Type: "select", Options: ",NEEDS ACTION,QUEUED,UNKNOWN,REJECTED,ACKNOWLEDGED,CANCELLED"}),
		Run: func(tenantID string, p map[string]string) ([]map[string]interface{}, error) {
			tab := map[string]string{"NEEDS ACTION": "needs_action", "QUEUED": "queued", "UNKNOWN": "unknown", "REJECTED": "rejected", "ACKNOWLEDGED": "acknowledged", "CANCELLED": "cancelled"}[p["state"]]
			return irpReportRows(tenantID, EInvoiceFilter{Tab: tab, GSTIN: p["gstin"], From: p["from"], To: p["to"]}, func(r IRPRecord) map[string]interface{} {
				return map[string]interface{}{"doc_date": r.DocDate, "doc_type": r.DocType, "doc_number": r.DocNumber, "gstin": r.GSTIN, "state": r.State,
					"irn": r.IRN, "ack_no": r.AckNo, "ack_date": irpTimeText(r.AckDate), "source": map[string]string{"manual": "Portal (manual)"}[r.Source] + map[bool]string{true: "API"}[r.Source != "manual"],
					"cancelled_at": irpTimeText(r.CancelledAt)}
			})
		},
	})
	RegisterReport(ReportDefinition{
		ID: "einvoice-errors", Label: "E-Invoice Error / Retry Report", Category: "Finance",
		Description: "Documents the IRP refused or that are still being retried, with the last error and the number of attempts.",
		Columns: []ReportColumn{{Key: "doc_date", Label: "Date"}, {Key: "doc_type", Label: "Type"}, {Key: "doc_number", Label: "Number"}, {Key: "gstin", Label: "Seller GSTIN"},
			{Key: "state", Label: "IRN state"}, {Key: "attempts", Label: "Attempts"}, {Key: "next_attempt_at", Label: "Next try"}, {Key: "last_error", Label: "Last error / what to fix"}},
		Params: dateParams,
		Run: func(tenantID string, p map[string]string) ([]map[string]interface{}, error) {
			return irpReportRows(tenantID, EInvoiceFilter{GSTIN: p["gstin"], From: p["from"], To: p["to"]}, func(r IRPRecord) map[string]interface{} {
				if r.LastError == "" && r.Attempts == 0 {
					return nil
				}
				if r.State == IRPStateAcknowledged || r.State == IRPStateCancelled {
					if r.LastError == "" {
						return nil
					}
				}
				msg := r.LastError
				if len(r.Issues) > 0 {
					parts := []string{}
					for _, is := range r.Issues {
						parts = append(parts, is.Field+": "+is.Problem+" - "+is.Fix)
					}
					msg = strings.Join(parts, "; ")
				}
				return map[string]interface{}{"doc_date": r.DocDate, "doc_type": r.DocType, "doc_number": r.DocNumber, "gstin": r.GSTIN, "state": r.State,
					"attempts": r.Attempts, "next_attempt_at": irpTimeText(r.NextAttemptAt), "last_error": msg}
			})
		},
	})
	RegisterReport(ReportDefinition{
		ID: "einvoice-manual-upload", Label: "IRN Portal Manual Upload Report", Category: "Finance",
		Description: "Documents to report on the IRP's portal by hand (the outage path), and the IRNs entered from the portal.",
		Columns: []ReportColumn{{Key: "doc_date", Label: "Date"}, {Key: "doc_type", Label: "Type"}, {Key: "doc_number", Label: "Number"}, {Key: "gstin", Label: "Seller GSTIN"},
			{Key: "state", Label: "IRN state"}, {Key: "why", Label: "Why"}, {Key: "irn", Label: "IRN (when entered)"}},
		Params: dateParams,
		Run: func(tenantID string, p map[string]string) ([]map[string]interface{}, error) {
			return irpReportRows(tenantID, EInvoiceFilter{GSTIN: p["gstin"], From: p["from"], To: p["to"]}, func(r IRPRecord) map[string]interface{} {
				stuck := (r.State == IRPStateQueued || r.State == IRPStateUnknown) && r.AgeMinutes >= 60
				if !(r.State == IRPStateManualReview || r.State == IRPStateBlockedConfig || r.State == IRPStateManualImport || r.Source == "manual" || stuck) {
					return nil
				}
				return map[string]interface{}{"doc_date": r.DocDate, "doc_type": r.DocType, "doc_number": r.DocNumber, "gstin": r.GSTIN, "state": r.State,
					"why": firstNonEmpty(r.LastError, map[bool]string{true: "IRN entered from the portal"}[r.Source == "manual"]), "irn": r.IRN}
			})
		},
	})
}
