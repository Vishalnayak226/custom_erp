package engines

import (
	"context"
	"custom_erp/db"
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"strings"
	"sync"
	"time"
)

// Stage 75.2.6: the IRN queue. One irp_documents row per tax document; the
// worker claims due rows with FOR UPDATE SKIP LOCKED and a lease, so two
// workers never submit one document, and a submission that never finished
// (the server restarted mid-call) is treated as UNKNOWN and looked up before
// anything is sent again. UNKNOWN is never shown as success or failure.

const (
	irpLease       = 2 * time.Minute
	irpMaxAttempts = 8
	irpClaimBatch  = 20
	irpCallTimeout = 30 * time.Second
)

// IRPRecord is one irp_documents row, as the screens show it.
type IRPRecord struct {
	ID            int64             `json:"id"`
	Environment   string            `json:"environment"`
	GSTIN         string            `json:"gstin"`
	FiscalYear    string            `json:"fiscal_year"`
	DocType       string            `json:"doc_type"`
	DocNumber     string            `json:"doc_number"`
	DocDate       string            `json:"doc_date"`
	SourceDoctype string            `json:"source_doctype"`
	SourceID      string            `json:"source_id"`
	State         string            `json:"state"`
	Reason        string            `json:"reason"`
	RuleVersion   int               `json:"rule_version"`
	PayloadHash   string            `json:"payload_hash"`
	IRN           string            `json:"irn"`
	AckNo         string            `json:"ack_no"`
	AckDate       *time.Time        `json:"ack_date,omitempty"`
	SignedQR      string            `json:"signed_qr,omitempty"`
	Source        string            `json:"source"`
	Attempts      int               `json:"attempts"`
	NextAttemptAt *time.Time        `json:"next_attempt_at,omitempty"`
	LastError     string            `json:"last_error"`
	Issues        []EInvoiceIssue   `json:"issues,omitempty"`
	CancelReason  string            `json:"cancel_reason,omitempty"`
	CancelledAt   *time.Time        `json:"cancelled_at,omitempty"`
	CreatedAt     time.Time         `json:"created_at"`
	UpdatedAt     time.Time         `json:"updated_at"`
	AgeMinutes    int               `json:"age_minutes"`
	Extra         map[string]string `json:"extra,omitempty"`
}

const irpRecordColumns = `id, environment, gstin, fiscal_year, doc_type, doc_number, COALESCE(doc_date::text, ''), source_doctype, source_id,
	state, reason, rule_version, COALESCE(payload_hash, ''), COALESCE(irn, ''), COALESCE(ack_no, ''), ack_date, COALESCE(signed_qr, ''),
	source, attempts, next_attempt_at, last_error, cancel_reason, cancelled_at, created_at, updated_at,
	GREATEST(EXTRACT(EPOCH FROM (NOW() - updated_at)) / 60, 0)::int`

func scanIRPRecord(row interface{ Scan(...interface{}) error }) (IRPRecord, error) {
	var r IRPRecord
	var ack, next, cancelled sql.NullTime
	err := row.Scan(&r.ID, &r.Environment, &r.GSTIN, &r.FiscalYear, &r.DocType, &r.DocNumber, &r.DocDate, &r.SourceDoctype, &r.SourceID,
		&r.State, &r.Reason, &r.RuleVersion, &r.PayloadHash, &r.IRN, &r.AckNo, &ack, &r.SignedQR, &r.Source, &r.Attempts, &next,
		&r.LastError, &r.CancelReason, &cancelled, &r.CreatedAt, &r.UpdatedAt, &r.AgeMinutes)
	if err != nil {
		return r, err
	}
	if ack.Valid {
		r.AckDate = &ack.Time
	}
	if next.Valid {
		r.NextAttemptAt = &next.Time
	}
	if cancelled.Valid {
		r.CancelledAt = &cancelled.Time
	}
	if strings.HasPrefix(r.LastError, "[") {
		_ = json.Unmarshal([]byte(r.LastError), &r.Issues)
	}
	return r, nil
}

// GetIRPRecordFor is a document's e-invoice record in this environment, or nil.
func GetIRPRecordFor(tenantID, doctype, id string) (*IRPRecord, error) {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return nil, err
	}
	r, err := scanIRPRecord(db.DB.QueryRow(fmt.Sprintf(`SELECT %s FROM %s.irp_documents WHERE environment = $1 AND source_doctype = $2 AND source_id = $3`, irpRecordColumns, schema),
		IRPEnvironment(), doctype, id))
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &r, nil
}

func getIRPRecord(schema string, id int64) (IRPRecord, error) {
	return scanIRPRecord(db.DB.QueryRow(fmt.Sprintf(`SELECT %s FROM %s.irp_documents WHERE id = $1`, irpRecordColumns, schema), id))
}

// RegisterEInvoiceDocument freezes a posted tax document's canonical payload
// and records the eligibility decision (75.2.1 / 75.2.2). A document already
// registered is returned as it is: the payload is frozen once.
func RegisterEInvoiceDocument(tenantID, doctype, id, actor string) (*IRPRecord, error) {
	if existing, err := GetIRPRecordFor(tenantID, doctype, id); err != nil || existing != nil {
		return existing, err
	}
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return nil, err
	}
	doc, err := BuildCanonicalTaxDocument(tenantID, doctype, id)
	if err != nil {
		return nil, err
	}
	profile, err := irpProfileFor(schema, doc.Meta.SellerGSTIN)
	if err != nil {
		return nil, err
	}
	decision := DecideEInvoice(profile, doc.Meta.DocType, doc.Meta.SellerGSTIN, doc.Meta.BuyerGSTIN, doc.TranDtls.SupTyp, doc.Meta.Date)
	state, lastError := IRPStateNotRequired, ""
	var payload []byte
	var hash string
	if decision.Required {
		payload, hash, err = doc.CanonicalJSON()
		if err != nil {
			return nil, err
		}
		state = IRPStateReady
		if issues := ValidateCanonicalTaxDocument(doc, profile.TurnoverBand); len(issues) > 0 {
			b, _ := json.Marshal(issues)
			lastError = string(b)
		} else if profile.AutoSubmit {
			state = IRPStateQueued
		}
	}
	gstin := firstNonEmpty(doc.Meta.SellerGSTIN, "UNREGISTERED")
	number := doc.Meta.Number
	if !decision.Required {
		// A document that is not sent keeps its own legal key out of the way
		// of one that is (two stores can print the same number under
		// different GSTINs; neither collides here).
		number = doctype + ":" + id
	}
	var payloadArg interface{}
	if payload != nil {
		payloadArg = string(payload)
	}
	var newID int64
	err = db.DB.QueryRow(fmt.Sprintf(`
		INSERT INTO %s.irp_documents (environment, gstin, fiscal_year, doc_type, doc_number, doc_date, source_doctype, source_id, state, reason,
			rule_version, payload, payload_hash, last_error, next_attempt_at)
		VALUES ($1,$2,$3,$4,$5,$6::date,$7,$8,$9::text,$10,$11,$12::jsonb,NULLIF($13::text,''),$14, CASE WHEN $9::text = 'QUEUED' THEN NOW() END)
		ON CONFLICT (environment, source_doctype, source_id) DO NOTHING
		RETURNING id`, schema),
		IRPEnvironment(), gstin, doc.Meta.FiscalYear, doc.Meta.DocType, number, doc.Meta.Date.Format("2006-01-02"), doctype, id, state,
		decision.Reason, decision.RuleVersion, payloadArg, hash, lastError).Scan(&newID)
	if err == sql.ErrNoRows {
		return GetIRPRecordFor(tenantID, doctype, id)
	}
	if err != nil {
		// The legal-identity key (its name is truncated by Postgres).
		if strings.Contains(err.Error(), "irp_documents_environment_gstin_fiscal_year") {
			return nil, einvoiceError("EINV-0981", "Document number", fmt.Sprintf("%s %s was already used by another e-invoice under GSTIN %s this year - a number is never reused, even after a cancellation", doc.Meta.DocType, number, gstin))
		}
		return nil, err
	}
	irpEvent(schema, newID, gstin, "REGISTERED", "", state, decision.Reason, actor)
	r, err := getIRPRecord(schema, newID)
	return &r, err
}

// RevalidateEInvoice rebuilds the payload from the masters as they are now
// and checks it again - the way to clear a REJECTED or not-yet-valid document
// after fixing an HSN, a PIN or a GSTIN. Never after acknowledgement.
func RevalidateEInvoice(tenantID string, recordID int64, actor string) (IRPRecord, error) {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return IRPRecord{}, err
	}
	r, err := getIRPRecord(schema, recordID)
	if err != nil {
		return r, err
	}
	switch r.State {
	case IRPStateReady, IRPStateRejected, IRPStateBlockedConfig, IRPStateNotRequired:
	default:
		return r, einvoiceError("EINV-0982", "State", fmt.Sprintf("a %s e-invoice cannot be rebuilt", r.State))
	}
	doc, err := BuildCanonicalTaxDocument(tenantID, r.SourceDoctype, r.SourceID)
	if err != nil {
		return r, err
	}
	profile, err := irpProfileFor(schema, doc.Meta.SellerGSTIN)
	if err != nil {
		return r, err
	}
	decision := DecideEInvoice(profile, doc.Meta.DocType, doc.Meta.SellerGSTIN, doc.Meta.BuyerGSTIN, doc.TranDtls.SupTyp, doc.Meta.Date)
	if !decision.Required {
		_, err = db.DB.Exec(fmt.Sprintf(`UPDATE %s.irp_documents SET state = 'NOT_REQUIRED', reason = $2, rule_version = $3, last_error = '', updated_at = NOW() WHERE id = $1`, schema), r.ID, decision.Reason, decision.RuleVersion)
		irpEvent(schema, r.ID, r.GSTIN, "REVALIDATED", r.State, IRPStateNotRequired, decision.Reason, actor)
		if err != nil {
			return r, err
		}
		return getIRPRecord(schema, r.ID)
	}
	payload, hash, err := doc.CanonicalJSON()
	if err != nil {
		return r, err
	}
	lastError := ""
	if issues := ValidateCanonicalTaxDocument(doc, profile.TurnoverBand); len(issues) > 0 {
		b, _ := json.Marshal(issues)
		lastError = string(b)
	}
	if _, err := db.DB.Exec(fmt.Sprintf(`UPDATE %s.irp_documents SET state = 'READY', gstin = $2, doc_number = $3, payload = $4::jsonb,
		payload_hash = $5, reason = $6, rule_version = $7, last_error = $8, attempts = 0, updated_at = NOW() WHERE id = $1`, schema),
		r.ID, doc.Meta.SellerGSTIN, doc.Meta.Number, string(payload), hash, decision.Reason, decision.RuleVersion, lastError); err != nil {
		return r, err
	}
	irpEvent(schema, r.ID, r.GSTIN, "REVALIDATED", r.State, IRPStateReady, fmt.Sprintf("payload %s -> %s", shortHash(r.PayloadHash), shortHash(hash)), actor)
	return getIRPRecord(schema, r.ID)
}

func shortHash(h string) string {
	if len(h) > 12 {
		return h[:12]
	}
	return firstNonEmpty(h, "-")
}

// SubmitEInvoice queues a valid READY or REJECTED-then-fixed document, or
// asks for a status check of an UNKNOWN one.
func SubmitEInvoice(tenantID string, recordID int64, actor string) (IRPRecord, error) {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return IRPRecord{}, err
	}
	r, err := getIRPRecord(schema, recordID)
	if err != nil {
		return r, err
	}
	switch r.State {
	case IRPStateReady:
		if len(r.Issues) > 0 {
			return r, einvoiceError("EINV-0982", "State", fmt.Sprintf("%s has %d problem(s) to fix first - see the list, fix them and press Validate", r.DocNumber, len(r.Issues)))
		}
	case IRPStateUnknown, IRPStateBlockedConfig, IRPStateManualReview:
	default:
		return r, einvoiceError("EINV-0982", "State", fmt.Sprintf("a %s e-invoice cannot be submitted", r.State))
	}
	next := IRPStateQueued
	if r.State == IRPStateUnknown {
		next = IRPStateUnknown // checked, never re-sent blind
	}
	if _, err := db.DB.Exec(fmt.Sprintf(`UPDATE %s.irp_documents SET state = $2::text, next_attempt_at = NOW(), attempts = CASE WHEN $2::text = 'QUEUED' THEN 0 ELSE attempts END, updated_at = NOW() WHERE id = $1`, schema), r.ID, next); err != nil {
		return r, err
	}
	irpEvent(schema, r.ID, r.GSTIN, "SUBMITTED", r.State, next, "", actor)
	irpKickWorker(schema)
	return getIRPRecord(schema, r.ID)
}

// ---------------------------------------------------------------------------
// The worker
// ---------------------------------------------------------------------------

var irpSchemaRun sync.Map // schema -> *sync.Mutex: one pass per schema at a time in this process

// RunEInvoiceWorkerForSchema runs one pass for a tenant: discover newly
// posted documents, then work the due queue.
func RunEInvoiceWorkerForSchema(schema string) {
	m, _ := irpSchemaRun.LoadOrStore(schema, &sync.Mutex{})
	mu := m.(*sync.Mutex)
	if !mu.TryLock() {
		return
	}
	defer mu.Unlock()
	var enabled bool
	if err := db.DB.QueryRow(fmt.Sprintf(`SELECT EXISTS(SELECT 1 FROM %s.irp_profiles WHERE environment = $1 AND enabled)`, schema), IRPEnvironment()).Scan(&enabled); err != nil {
		return // no e-invoice tables in this schema yet
	}
	tenantID, err := tenantIDForSchema(schema)
	if err != nil || tenantID == "" {
		return
	}
	if enabled {
		discoverEInvoiceDocuments(tenantID, schema)
	}
	for _, id := range claimDueIRPDocuments(schema) {
		processIRPDocument(tenantID, schema, id)
	}
	if enabled {
		irpCheckAlerts(schema)
		irpDailyReconcile(tenantID, schema)
	}
}

// RunEInvoiceWorkerOnce is the pass for one tenant (tests and the workbench).
func RunEInvoiceWorkerOnce(tenantID string) error {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return err
	}
	RunEInvoiceWorkerForSchema(schema)
	return nil
}

// StartEInvoiceWorker ticks every interval over every tenant. A tenant with
// no enabled GSTIN costs one EXISTS query.
func StartEInvoiceWorker(ctx context.Context, interval time.Duration) {
	ticker := time.NewTicker(interval)
	go func() {
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				if db.DB == nil {
					continue
				}
				schemas, err := listTenantSchemas()
				if err != nil {
					log.Printf("[EINVOICE] listing tenant schemas: %v", err)
					continue
				}
				for _, s := range schemas {
					RunEInvoiceWorkerForSchema(s)
				}
			}
		}
	}()
}

// irpPostedStatuses are the statuses at which each document has posted
// (posted_amounts.go's rules - one definition of "posted").
var irpPostedStatuses = map[string][]string{"SalesInvoice": {"Approved", "Paid"}, "CreditNote": {"Posted"}}

// discoverEInvoiceDocuments registers every posted invoice and credit note
// since the earliest enabled GSTIN's start that has no record yet - whatever
// made it (an order, a parcel, a recurring contract, the record form).
func discoverEInvoiceDocuments(tenantID, schema string) {
	var from sql.NullTime
	if err := db.DB.QueryRow(fmt.Sprintf(`SELECT MIN(effective_from)::timestamp FROM %s.irp_profiles WHERE environment = $1 AND enabled`, schema), IRPEnvironment()).Scan(&from); err != nil || !from.Valid {
		return
	}
	for doctype, statuses := range irpPostedStatuses {
		rows, err := db.DB.Query(fmt.Sprintf(`
			SELECT d.id FROM %s.documents d
			WHERE d.doctype = $1 AND d.deleted_at IS NULL AND d.status = ANY($2) AND d.created_at >= $3::timestamp - INTERVAL '1 day'
			  AND NOT EXISTS (SELECT 1 FROM %s.irp_documents x WHERE x.environment = $4 AND x.source_doctype = $1 AND x.source_id = d.id)
			ORDER BY d.created_at LIMIT 100`, schema, schema), doctype, pqStringArray(statuses), from.Time, IRPEnvironment())
		if err != nil {
			irpLog(schema, "e-invoice discovery: "+err.Error())
			continue
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
			if _, err := RegisterEInvoiceDocument(tenantID, doctype, id, "system"); err != nil {
				irpLog(schema, fmt.Sprintf("e-invoice: %s %s could not be registered: %v", doctype, id, err))
			}
		}
	}
}

// claimDueIRPDocuments claims due rows (SKIP LOCKED + lease) and returns them.
// A SUBMITTING row whose lease ran out was being sent when its worker stopped:
// it becomes UNKNOWN, so the next step is a lookup, never a second send.
func claimDueIRPDocuments(schema string) []int64 {
	tx, err := db.DB.Begin()
	if err != nil {
		return nil
	}
	defer tx.Rollback()
	rows, err := tx.Query(fmt.Sprintf(`
		SELECT id, state FROM %s.irp_documents
		WHERE environment = $1 AND (
			(state IN ('QUEUED','UNKNOWN','CANCEL_PENDING','CANCEL_UNKNOWN') AND (next_attempt_at IS NULL OR next_attempt_at <= NOW()) AND (lease_until IS NULL OR lease_until < NOW()))
			OR (state = 'SUBMITTING' AND lease_until < NOW()))
		ORDER BY next_attempt_at NULLS FIRST
		LIMIT %d
		FOR UPDATE SKIP LOCKED`, schema, irpClaimBatch), IRPEnvironment())
	if err != nil {
		return nil
	}
	type claim struct {
		id    int64
		state string
	}
	var claims []claim
	for rows.Next() {
		var c claim
		if rows.Scan(&c.id, &c.state) == nil {
			claims = append(claims, c)
		}
	}
	rows.Close()
	var ids []int64
	for _, c := range claims {
		next := c.state
		switch c.state {
		case IRPStateQueued:
			next = IRPStateSubmitting
		case IRPStateSubmitting: // RET-04: interrupted mid-send
			next = IRPStateUnknown
		}
		if _, err := tx.Exec(fmt.Sprintf(`UPDATE %s.irp_documents SET state = $2, lease_until = NOW() + INTERVAL '%d seconds', updated_at = NOW() WHERE id = $1`, schema, int(irpLease.Seconds())), c.id, next); err != nil {
			return nil
		}
		if next != c.state {
			irpEvent(schema, c.id, "", "CLAIMED", c.state, next, "", "worker")
		}
		ids = append(ids, c.id)
	}
	if tx.Commit() != nil {
		return nil
	}
	return ids
}

// irpAuthFailures counts consecutive authentication failures per GSTIN.
var irpAuthFailures sync.Map

func irpBackoff(attempts int) time.Duration {
	d := 30 * time.Second << uint(minInt(attempts, 7))
	if d > time.Hour {
		d = time.Hour
	}
	return d
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// irpTransition moves a record and records why.
func irpTransition(schema string, r IRPRecord, to, event, detail, lastError string, retryIn time.Duration, sets string, args ...interface{}) {
	var next interface{}
	if retryIn > 0 {
		next = time.Now().Add(retryIn)
	}
	q := fmt.Sprintf(`UPDATE %s.irp_documents SET state = $2, last_error = $3, next_attempt_at = $4, lease_until = NULL, updated_at = NOW()%s WHERE id = $1`, schema, sets)
	all := append([]interface{}{r.ID, to, lastError, next}, args...)
	if _, err := db.DB.Exec(q, all...); err != nil {
		irpLog(schema, fmt.Sprintf("e-invoice %s: state %s -> %s not saved: %v", r.DocNumber, r.State, to, err))
		return
	}
	irpEvent(schema, r.ID, r.GSTIN, event, r.State, to, detail, "worker")
}

func irpAttempt(schema string, r IRPRecord, action, outcome string, err error, started time.Time) {
	code, msg := "", ""
	if err != nil {
		code, msg = string(irpErrorKind(err)), redactAndBoundLogField(err.Error(), 500)
	}
	db.DB.Exec(fmt.Sprintf(`INSERT INTO %s.irp_attempts (document_id, gstin, action, outcome, error_code, error_message, duration_ms) VALUES ($1,$2,$3,$4,$5,$6,$7)`, schema),
		r.ID, r.GSTIN, action, outcome, code, msg, int(time.Since(started).Milliseconds()))
}

// processIRPDocument does one step for one claimed record.
func processIRPDocument(tenantID, schema string, id int64) {
	r, err := getIRPRecord(schema, id)
	if err != nil {
		return
	}
	profile, err := irpProfileFor(schema, r.GSTIN)
	if err != nil || profile == nil || !profile.Enabled {
		irpTransition(schema, r, IRPStateBlockedConfig, "BLOCKED", "e-invoicing is not switched on for this GSTIN", "e-invoicing is switched off for GSTIN "+r.GSTIN, 0, "")
		return
	}
	provider, ok := irpProviderFor(profile.Provider)
	if !ok {
		irpTransition(schema, r, IRPStateBlockedConfig, "BLOCKED", "provider not available", "provider "+profile.Provider+" is not available on this server", 0, "")
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), irpCallTimeout)
	defer cancel()
	token, err := irpTokenFor(ctx, tenantID, provider, r.Environment, r.GSTIN)
	if err != nil {
		irpAttempt(schema, r, "auth", "failed", err, time.Now())
		n := 1
		if v, ok := irpAuthFailures.Load(r.GSTIN); ok {
			n = v.(int) + 1
		}
		irpAuthFailures.Store(r.GSTIN, n)
		if n == 5 {
			SendOpsAlert("IRP_AUTH_FAILING", "einvoice", fmt.Sprintf("%s: GSTIN %s failed to sign in to the IRP 5 times in a row - check its credentials", schema, r.GSTIN))
		}
		back := r.State
		if back == IRPStateSubmitting {
			back = IRPStateQueued // nothing was sent
		}
		irpTransition(schema, r, back, "AUTH_FAILED", err.Error(), "could not sign in to the IRP: "+err.Error(), irpBackoff(r.Attempts), ", attempts = attempts + 1")
		return
	}
	irpAuthFailures.Delete(r.GSTIN)

	switch r.State {
	case IRPStateSubmitting:
		irpGenerate(ctx, schema, r, profile, provider, token)
	case IRPStateUnknown:
		irpLookup(ctx, schema, r, provider, token)
	case IRPStateCancelPending, IRPStateCancelUnknown:
		irpCancel(ctx, schema, r, provider, token)
	}
}

func irpGenerate(ctx context.Context, schema string, r IRPRecord, profile *IRPProfile, provider IRPProvider, token IRPToken) {
	// 75.2.7: the 30-day rule - a GSTIN with turnover of ₹10 crore or more
	// cannot report an invoice older than 30 days.
	if profile.TurnoverBand == "10CR+" && r.DocDate != "" {
		if d, err := time.Parse("2006-01-02", r.DocDate); err == nil && time.Since(d) > 30*24*time.Hour {
			irpTransition(schema, r, IRPStateManualReview, "THIRTY_DAY_RULE", "older than 30 days", "dated "+r.DocDate+": more than 30 days old, which the IRP refuses for this GSTIN - decide with your CA (late reporting)", 0, "")
			return
		}
	}
	if r.Attempts >= irpMaxAttempts {
		irpTransition(schema, r, IRPStateManualReview, "RETRIES_EXHAUSTED", "", fmt.Sprintf("%d attempts failed - see the attempts list", r.Attempts), 0, "")
		return
	}
	var payload []byte
	if err := db.DB.QueryRow(fmt.Sprintf(`SELECT payload::text FROM %s.irp_documents WHERE id = $1`, schema), r.ID).Scan(&payload); err != nil {
		irpTransition(schema, r, IRPStateQueued, "ERROR", err.Error(), err.Error(), irpBackoff(r.Attempts), ", attempts = attempts + 1")
		return
	}
	started := time.Now()
	ack, err := provider.GenerateIRN(ctx, token, r.GSTIN, payload)
	if err == nil {
		irpAttempt(schema, r, "generate", "acknowledged", nil, started)
		irpAcknowledge(schema, r, ack, "GENERATED")
		return
	}
	irpAttempt(schema, r, "generate", string(irpErrorKind(err)), err, started)
	switch irpErrorKind(err) {
	case IRPRejected:
		irpTransition(schema, r, IRPStateRejected, "REJECTED", err.Error(), err.Error(), 0, ", attempts = attempts + 1")
	case IRPDuplicate, IRPMaybeAccepted:
		// Registered (or perhaps registered): find it, never send it again.
		irpTransition(schema, r, IRPStateUnknown, "UNKNOWN", err.Error(), err.Error(), time.Second, ", attempts = attempts + 1")
	case IRPAuthFailed:
		irpForgetToken(r.Environment, r.GSTIN)
		irpTransition(schema, r, IRPStateQueued, "AUTH_EXPIRED", err.Error(), err.Error(), time.Second, ", attempts = attempts + 1")
	case IRPIPBlocked:
		SendOpsAlert("IRP_IP_BLOCKED", "einvoice", fmt.Sprintf("%s: the IRP refuses this server's IP for GSTIN %s - whitelist it with the IRP", schema, r.GSTIN))
		irpTransition(schema, r, IRPStateBlockedConfig, "IP_BLOCKED", err.Error(), "the IRP does not accept this server's IP address - it must be whitelisted (Tax Integration Settings shows the address on record)", 0, ", attempts = attempts + 1")
	default: // retry later, provider failure: nothing was registered
		irpTransition(schema, r, IRPStateQueued, "RETRY", err.Error(), err.Error(), irpBackoff(r.Attempts), ", attempts = attempts + 1")
	}
}

// irpLookup resolves an UNKNOWN: a lookup by document comes before any retry.
func irpLookup(ctx context.Context, schema string, r IRPRecord, provider IRPProvider, token IRPToken) {
	started := time.Now()
	docDate := ""
	if d, err := time.Parse("2006-01-02", r.DocDate); err == nil {
		docDate = irpDate(d)
	}
	ack, err := provider.GetByDocument(ctx, token, r.GSTIN, r.DocType, r.DocNumber, docDate)
	if err == nil {
		irpAttempt(schema, r, "lookup", "found", nil, started)
		irpAcknowledge(schema, r, ack, "FOUND_ON_LOOKUP")
		return
	}
	irpAttempt(schema, r, "lookup", string(irpErrorKind(err)), err, started)
	switch irpErrorKind(err) {
	case IRPNotFound:
		// Not registered after all: it may be sent.
		irpTransition(schema, r, IRPStateQueued, "NOT_REGISTERED", "lookup found no IRN", "", time.Second, "")
	case IRPAuthFailed:
		irpForgetToken(r.Environment, r.GSTIN)
		irpTransition(schema, r, IRPStateUnknown, "AUTH_EXPIRED", err.Error(), err.Error(), time.Second, ", attempts = attempts + 1")
	default:
		irpTransition(schema, r, IRPStateUnknown, "LOOKUP_FAILED", err.Error(), err.Error(), irpBackoff(r.Attempts), ", attempts = attempts + 1")
	}
}

func irpAcknowledge(schema string, r IRPRecord, ack IRPAck, event string) {
	irpTransition(schema, r, IRPStateAcknowledged, event, "IRN "+shortHash(ack.IRN)+" ack "+ack.AckNo, "", 0,
		", irn = $5, ack_no = $6, ack_date = $7, signed_invoice = $8, signed_qr = $9", ack.IRN, ack.AckNo, ack.AckDate, ack.SignedInvoice, ack.SignedQR)
}

func irpCancel(ctx context.Context, schema string, r IRPRecord, provider IRPProvider, token IRPToken) {
	var code, remark string
	_ = db.DB.QueryRow(fmt.Sprintf(`SELECT COALESCE(cancel_reason, '') FROM %s.irp_documents WHERE id = $1`, schema), r.ID).Scan(&remark)
	code, remark = splitCancelReason(remark)
	started := time.Now()
	at, err := provider.CancelIRN(ctx, token, r.GSTIN, r.IRN, code, remark)
	if err == nil {
		irpAttempt(schema, r, "cancel", "cancelled", nil, started)
		irpTransition(schema, r, IRPStateCancelled, "CANCELLED", "", "", 0, ", cancelled_at = $5", at)
		return
	}
	irpAttempt(schema, r, "cancel", string(irpErrorKind(err)), err, started)
	switch irpErrorKind(err) {
	case IRPWindowExpired:
		irpTransition(schema, r, IRPStateManualReview, "CANCEL_TOO_LATE", err.Error(), "the 24-hour cancellation window has passed - issue a credit note instead", 0, "")
	case IRPRejected:
		irpTransition(schema, r, IRPStateManualReview, "CANCEL_REFUSED", err.Error(), err.Error(), 0, "")
	case IRPAuthFailed:
		irpForgetToken(r.Environment, r.GSTIN)
		irpTransition(schema, r, r.State, "AUTH_EXPIRED", err.Error(), err.Error(), time.Second, ", attempts = attempts + 1")
	default:
		irpTransition(schema, r, IRPStateCancelUnknown, "CANCEL_UNKNOWN", err.Error(), err.Error(), irpBackoff(r.Attempts), ", attempts = attempts + 1")
	}
}

// splitCancelReason reads "<code>|<remark>" as stored by an approved cancel.
func splitCancelReason(stored string) (string, string) {
	if i := strings.Index(stored, "|"); i > 0 {
		return stored[:i], stored[i+1:]
	}
	return "4", stored
}

// irpKickWorker runs a pass right after a user action instead of waiting for
// the next tick. Tests replace it to drive the worker step by step.
var irpKickWorker = func(schema string) { go RunEInvoiceWorkerForSchema(schema) }

// IRPBadge is what an invoice or note screen shows of its e-invoice.
type IRPBadge struct {
	State    string     `json:"state"`
	IRN      string     `json:"irn,omitempty"`
	AckNo    string     `json:"ack_no,omitempty"`
	AckDate  *time.Time `json:"ack_date,omitempty"`
	SignedQR string     `json:"signed_qr,omitempty"`
}

// EInvoiceBadges maps a doctype's documents (or one, by id) to their
// e-invoice state. The signed QR is included only for one document (print).
func EInvoiceBadges(tenantID, doctype, id string) (map[string]IRPBadge, error) {
	out := map[string]IRPBadge{}
	if _, ok := EInvoiceDocTypes[doctype]; !ok {
		return out, nil
	}
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return out, err
	}
	q := fmt.Sprintf(`SELECT source_id, state, COALESCE(irn, ''), COALESCE(ack_no, ''), ack_date, CASE WHEN $3 <> '' THEN COALESCE(signed_qr, '') ELSE '' END
		FROM %s.irp_documents WHERE environment = $1 AND source_doctype = $2 AND ($3 = '' OR source_id = $3)
		ORDER BY updated_at DESC LIMIT 1000`, schema)
	rows, err := db.DB.Query(q, IRPEnvironment(), doctype, id)
	if err != nil {
		return out, nil // no e-invoice tables yet: no badges
	}
	defer rows.Close()
	for rows.Next() {
		var src string
		var b IRPBadge
		var ack sql.NullTime
		if rows.Scan(&src, &b.State, &b.IRN, &b.AckNo, &ack, &b.SignedQR) == nil {
			if ack.Valid {
				b.AckDate = &ack.Time
			}
			out[src] = b
		}
	}
	return out, rows.Err()
}
