package engines

import (
	"custom_erp/db"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/lib/pq"
)

// Stage 49.12.5: the notification decision workflow for a security incident.
//
// The system does NOT decide whether anyone must be told. It keeps:
//   - a versioned matrix of who may need telling (jurisdiction, contract,
//     customer, processor): what triggers it, who decides, the target time
//     and the contact. A save is always a new version; none is edited.
//   - for an incident, one decision per matrix row chosen, with the
//     discovery time, the assessment clock and the target deadline worked
//     out from that row, and a copy of the row as it was (so a later version
//     never changes an old decision).
//   - a person's decision (notify / do not notify / pending) with their
//     reason and evidence reference, and a named approver - someone other
//     than the decider - who must approve before it closes.
//
// Nothing here says "notification is required" or "is not required": the
// screen shows the matrix text and the clocks, and a person decides.
//
// Why not the maker-checker engine: that engine approves a business document
// against role-based rules; here the approver is a named person chosen per
// decision (counsel or the privacy owner), the evidence is free text, and the
// record must stay immutable once closed. A second approval engine is not
// built; the dual control is the two checks below (approver differs from the
// decider; only the named approver can approve), both audited.

const (
	NotifyKindJurisdiction = "jurisdiction"
	NotifyKindContract     = "contract"
	NotifyKindCustomer     = "customer"
	NotifyKindProcessor    = "processor"

	NotifyOpen     = "Open"
	NotifyAwaiting = "Awaiting approval"
	NotifyClosed   = "Closed"

	NotifyPending     = "pending"
	NotifyDoNotNotify = "do_not_notify"
	NotifyNotify      = "notify"

	maxNotifyRows = 60
)

var notifyKeyPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]{1,58}$`)

// NotifyMatrixRow is one line of the matrix.
type NotifyMatrixRow struct {
	Key             string `json:"key"`
	Kind            string `json:"kind"`
	Name            string `json:"name"`
	Trigger         string `json:"trigger"`
	Decider         string `json:"decider"`
	Contact         string `json:"contact"`
	TargetHours     int    `json:"target_hours"`     // 0 = no target set
	AssessmentHours int    `json:"assessment_hours"` // 0 = no assessment limit set
	Source          string `json:"source"`
	NeedsCounsel    bool   `json:"needs_counsel"`
}

// NotifyMatrix is one version.
type NotifyMatrix struct {
	Version   int               `json:"version"`
	Note      string            `json:"note"`
	CreatedBy string            `json:"created_by"`
	CreatedAt time.Time         `json:"created_at"`
	Entries   []NotifyMatrixRow `json:"entries"`
}

// seedNotifyMatrix is version 1. Both rows are flagged for counsel. The CERT-In
// six hours is the figure docs/security/cert-in-log-inventory.md records
// (CM-IN-009 in compliance-control-matrix.json); no DPDP hour limit is
// recorded in docs/, so none is stated - counsel sets it.
func seedNotifyMatrix() []NotifyMatrixRow {
	return []NotifyMatrixRow{
		{Key: "cert-in", Kind: NotifyKindJurisdiction, Name: "CERT-In (India)",
			Trigger: "A cyber incident of a type CERT-In lists is noticed", Decider: "Counsel or the data protection officer - to be named",
			Contact: "To be filled by the client", TargetHours: 6,
			Source: "docs/security/cert-in-log-inventory.md (CM-IN-009): six hours from noticing. Counsel to confirm it applies to this client.", NeedsCounsel: true},
		{Key: "dpdp", Kind: NotifyKindJurisdiction, Name: "DPDP Act 2023 - Data Protection Board and affected people (India)",
			Trigger: "A personal-data breach", Decider: "Counsel or the data protection officer - to be named",
			Contact: "To be filled by the client", TargetHours: 0,
			Source: "docs/security/compliance-control-matrix.json (s.8(6)). No time limit is recorded in docs/: counsel sets it.", NeedsCounsel: true},
	}
}

func validateNotifyRows(rows []NotifyMatrixRow) error {
	if len(rows) == 0 || len(rows) > maxNotifyRows {
		return fmt.Errorf("the matrix needs 1 to %d rows", maxNotifyRows)
	}
	seen := map[string]bool{}
	for i := range rows {
		r := &rows[i]
		r.Key = strings.ToLower(strings.TrimSpace(r.Key))
		r.Name, r.Trigger, r.Decider = strings.TrimSpace(r.Name), strings.TrimSpace(r.Trigger), strings.TrimSpace(r.Decider)
		r.Contact, r.Source = strings.TrimSpace(r.Contact), strings.TrimSpace(r.Source)
		if !notifyKeyPattern.MatchString(r.Key) {
			return fmt.Errorf("row %d: the key must be 2-59 letters, digits, - or _", i+1)
		}
		if seen[r.Key] {
			return fmt.Errorf("row %d: the key %q is used twice", i+1, r.Key)
		}
		seen[r.Key] = true
		switch r.Kind {
		case NotifyKindJurisdiction, NotifyKindContract, NotifyKindCustomer, NotifyKindProcessor:
		default:
			return fmt.Errorf("row %d: choose jurisdiction, contract, customer or processor", i+1)
		}
		if r.Name == "" || len(r.Name) > 200 || len(r.Trigger) > 500 || len(r.Decider) > 200 || len(r.Contact) > 300 || len(r.Source) > 500 {
			return fmt.Errorf("row %d: name is required and each field must be short", i+1)
		}
		if r.TargetHours < 0 || r.TargetHours > 24*365 || r.AssessmentHours < 0 || r.AssessmentHours > 24*365 {
			return fmt.Errorf("row %d: hours must be between 0 and 8760", i+1)
		}
	}
	return nil
}

func scanMatrix(row interface{ Scan(...interface{}) error }) (NotifyMatrix, error) {
	var m NotifyMatrix
	var raw []byte
	if err := row.Scan(&m.Version, &raw, &m.Note, &m.CreatedBy, &m.CreatedAt); err != nil {
		return m, err
	}
	if err := json.Unmarshal(raw, &m.Entries); err != nil {
		return m, err
	}
	m.CreatedAt = m.CreatedAt.UTC()
	return m, nil
}

// CurrentNotifyMatrix returns the newest version, writing the seed (version 1)
// the first time a client looks.
func CurrentNotifyMatrix(tenantID string) (NotifyMatrix, error) {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return NotifyMatrix{}, err
	}
	q := fmt.Sprintf(`SELECT version, entries, note, created_by, created_at FROM %s.notification_matrix_versions ORDER BY version DESC LIMIT 1`, schema)
	m, err := scanMatrix(db.DB.QueryRow(q))
	if err == sql.ErrNoRows {
		raw, _ := json.Marshal(seedNotifyMatrix())
		if _, err := db.DB.Exec(fmt.Sprintf(`INSERT INTO %s.notification_matrix_versions (version, entries, note, created_by)
			VALUES (1, $1, 'Starting rows - counsel to review', 'system') ON CONFLICT DO NOTHING`, schema), raw); err != nil {
			return NotifyMatrix{}, err
		}
		return scanMatrix(db.DB.QueryRow(q))
	}
	return m, err
}

// NotifyMatrixVersions lists every version, newest first, without the rows.
func NotifyMatrixVersions(tenantID string) ([]NotifyMatrix, error) {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return nil, err
	}
	rows, err := db.DB.Query(fmt.Sprintf(`SELECT version, note, created_by, created_at FROM %s.notification_matrix_versions ORDER BY version DESC LIMIT 100`, schema))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []NotifyMatrix{}
	for rows.Next() {
		var m NotifyMatrix
		if err := rows.Scan(&m.Version, &m.Note, &m.CreatedBy, &m.CreatedAt); err != nil {
			return nil, err
		}
		m.CreatedAt = m.CreatedAt.UTC()
		out = append(out, m)
	}
	return out, rows.Err()
}

// SaveNotifyMatrix stores the rows as the next version.
func SaveNotifyMatrix(tenantID, actor string, rows []NotifyMatrixRow, note string) (int, error) {
	if err := validateNotifyRows(rows); err != nil {
		return 0, err
	}
	note = strings.TrimSpace(note)
	if len(note) < 5 || len(note) > 300 {
		return 0, errors.New("write what changed (5 to 300 characters)")
	}
	cur, err := CurrentNotifyMatrix(tenantID) // also makes sure version 1 exists
	if err != nil {
		return 0, err
	}
	schema, _ := db.GetTenantSchema(tenantID)
	raw, _ := json.Marshal(rows)
	next := cur.Version + 1
	if _, err := db.DB.Exec(fmt.Sprintf(`INSERT INTO %s.notification_matrix_versions (version, entries, note, created_by) VALUES ($1, $2, $3, $4)`, schema),
		next, raw, redactAndBoundLogField(note, 300), actor); err != nil {
		var pe *pq.Error
		if errors.As(err, &pe) && pe.Code == "23505" {
			return 0, errors.New("the matrix changed while you were editing - reload and save again")
		}
		return 0, err
	}
	LogAuditEvent(tenantID, actor, "NOTIFY_MATRIX_VERSION", "SUCCESS", fmt.Sprintf("Notification matrix version %d saved (%d rows): %s", next, len(rows), redactAndBoundLogField(note, 200)))
	return next, nil
}

// NotifyDecision is one incident x matrix row, with its clocks.
type NotifyDecision struct {
	ID              string          `json:"id"`
	IncidentRef     string          `json:"incident_ref"`
	IncidentTitle   string          `json:"incident_title"`
	DiscoveredAt    time.Time       `json:"discovered_at"`
	OpenedBy        string          `json:"opened_by"`
	MatrixVersion   int             `json:"matrix_version"`
	Row             NotifyMatrixRow `json:"row"`
	AssessmentDueAt *time.Time      `json:"assessment_due_at"`
	TargetAt        *time.Time      `json:"target_at"`
	AssessState     string          `json:"assess_state"` // NotifyClock*: where the assessment deadline stands, set when listed
	TargetState     string          `json:"target_state"` // NotifyClock*: where the tell-by deadline stands, set when listed
	Status          string          `json:"status"`
	Decision        string          `json:"decision"`
	Reason          string          `json:"reason"`
	EvidenceRef     string          `json:"evidence_ref"`
	DecidedBy       string          `json:"decided_by"`
	DecidedAt       *time.Time      `json:"decided_at"`
	Approver        string          `json:"approver"`
	ApprovedBy      string          `json:"approved_by"`
	ApprovedAt      *time.Time      `json:"approved_at"`
	ApprovalNote    string          `json:"approval_note"`
}

// OpenNotifyIncident starts the clocks for an incident: one decision per
// chosen matrix row (all rows when keys is empty), each fixed to the version
// current now. discoveredAt is when the business noticed, as the person says.
func OpenNotifyIncident(tenantID, actor, title string, discoveredAt time.Time, keys []string) (string, error) {
	title = strings.TrimSpace(title)
	if len(title) < 3 || len(title) > 200 {
		return "", errors.New("name the incident (3 to 200 characters)")
	}
	now := time.Now().UTC()
	if discoveredAt.IsZero() || discoveredAt.After(now.Add(5*time.Minute)) || discoveredAt.Before(now.AddDate(-1, 0, 0)) {
		return "", errors.New("the discovery time must be within the last year and not in the future")
	}
	m, err := CurrentNotifyMatrix(tenantID)
	if err != nil {
		return "", err
	}
	chosen := m.Entries
	if len(keys) > 0 {
		want := map[string]bool{}
		for _, k := range keys {
			want[k] = true
		}
		chosen = nil
		for _, r := range m.Entries {
			if want[r.Key] {
				chosen = append(chosen, r)
			}
		}
	}
	if len(chosen) == 0 {
		return "", errors.New("choose at least one row of the matrix")
	}
	schema, _ := db.GetTenantSchema(tenantID)
	tx, err := db.DB.Begin()
	if err != nil {
		return "", err
	}
	defer tx.Rollback()
	var ref string
	if err := tx.QueryRow(`SELECT gen_random_uuid()::text`).Scan(&ref); err != nil {
		return "", err
	}
	for _, r := range chosen {
		var assess, target interface{}
		if r.AssessmentHours > 0 {
			assess = discoveredAt.UTC().Add(time.Duration(r.AssessmentHours) * time.Hour)
		}
		if r.TargetHours > 0 {
			target = discoveredAt.UTC().Add(time.Duration(r.TargetHours) * time.Hour)
		}
		raw, _ := json.Marshal(r)
		if _, err := tx.Exec(fmt.Sprintf(`INSERT INTO %s.notification_decisions
			(incident_ref, incident_title, discovered_at, opened_by, matrix_version, matrix_key, row_snapshot, assessment_due_at, target_at)
			VALUES ($1::uuid, $2, $3, $4, $5, $6, $7, $8, $9)`, schema),
			ref, redactAndBoundLogField(title, 200), discoveredAt.UTC(), actor, m.Version, r.Key, raw, assess, target); err != nil {
			return "", err
		}
	}
	if err := tx.Commit(); err != nil {
		return "", err
	}
	LogAuditEvent(tenantID, actor, "NOTIFY_INCIDENT_OPENED", "SUCCESS", fmt.Sprintf("Incident %q opened with %d notification decision(s) on matrix version %d; discovered %s UTC",
		redactAndBoundLogField(title, 100), len(chosen), m.Version, discoveredAt.UTC().Format("2006-01-02 15:04")))
	return ref, nil
}

const notifyDecisionColumns = `id::text, incident_ref::text, incident_title, discovered_at, opened_by, matrix_version, row_snapshot,
	assessment_due_at, target_at, status, decision, reason, evidence_ref, decided_by, decided_at, approver, approved_by, approved_at, approval_note`

func scanNotifyDecision(row interface{ Scan(...interface{}) error }) (NotifyDecision, error) {
	var d NotifyDecision
	var raw []byte
	var assess, target, decidedAt, approvedAt sql.NullTime
	if err := row.Scan(&d.ID, &d.IncidentRef, &d.IncidentTitle, &d.DiscoveredAt, &d.OpenedBy, &d.MatrixVersion, &raw, &assess, &target,
		&d.Status, &d.Decision, &d.Reason, &d.EvidenceRef, &d.DecidedBy, &decidedAt, &d.Approver, &d.ApprovedBy, &approvedAt, &d.ApprovalNote); err != nil {
		return d, err
	}
	_ = json.Unmarshal(raw, &d.Row)
	d.DiscoveredAt = d.DiscoveredAt.UTC()
	utc := func(t sql.NullTime) *time.Time {
		if !t.Valid {
			return nil
		}
		v := t.Time.UTC()
		return &v
	}
	d.AssessmentDueAt, d.TargetAt, d.DecidedAt, d.ApprovedAt = utc(assess), utc(target), utc(decidedAt), utc(approvedAt)
	return d, nil
}

// ListNotifyDecisions returns the newest 200 decisions.
func ListNotifyDecisions(tenantID string) ([]NotifyDecision, error) {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return nil, err
	}
	rows, err := db.DB.Query(fmt.Sprintf(`SELECT %s FROM %s.notification_decisions ORDER BY opened_at DESC, matrix_key LIMIT 200`, notifyDecisionColumns, schema))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []NotifyDecision{}
	now := time.Now().UTC()
	for rows.Next() {
		d, err := scanNotifyDecision(rows)
		if err != nil {
			return nil, err
		}
		closed := d.Status == "Closed"
		d.AssessState = NotifyClockState(d.DiscoveredAt, d.AssessmentDueAt, now, closed)
		d.TargetState = NotifyClockState(d.DiscoveredAt, d.TargetAt, now, closed)
		out = append(out, d)
	}
	return out, rows.Err()
}

func loadNotifyDecision(schema, id string) (NotifyDecision, error) {
	d, err := scanNotifyDecision(db.DB.QueryRow(fmt.Sprintf(`SELECT %s FROM %s.notification_decisions WHERE id::text = $1`, notifyDecisionColumns, schema), id))
	if err == sql.ErrNoRows {
		return d, errors.New("that decision was not found")
	}
	return d, err
}

// RecordNotifyDecision is the person's decision. It needs a reason, an
// evidence reference and a named approver who is an active user other than
// the decider. It moves the decision to Awaiting approval; it never closes it.
func RecordNotifyDecision(tenantID, actor, id, decision, reason, evidenceRef, approver string) error {
	switch decision {
	case NotifyNotify, NotifyDoNotNotify, NotifyPending:
	default:
		return errors.New("choose notify, do not notify or pending")
	}
	reason, evidenceRef, approver = strings.TrimSpace(reason), strings.TrimSpace(evidenceRef), strings.TrimSpace(approver)
	if len(reason) < 10 || len(reason) > 1000 {
		return errors.New("write the reason (10 to 1000 characters)")
	}
	if decision != NotifyPending && (len(evidenceRef) < 3 || len(evidenceRef) > 500) {
		return errors.New("give the evidence reference (3 to 500 characters)")
	}
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return err
	}
	d, err := loadNotifyDecision(schema, id)
	if err != nil {
		return err
	}
	if d.Status == NotifyClosed {
		return errors.New("this decision is closed and cannot be changed")
	}
	if decision != NotifyPending {
		if strings.EqualFold(approver, actor) {
			return errors.New("the approver must be someone other than the person deciding")
		}
		var active bool
		var role string
		if err := db.DB.QueryRow(fmt.Sprintf(`SELECT COALESCE(status, '') = 'Active', COALESCE(role, '') FROM %s.users WHERE lower(username) = lower($1)`, schema), approver).Scan(&active, &role); err != nil || !active {
			return errors.New("the approver must be an active user of this business")
		}
		if !CanApproveNotifyDecisions(role) { // an Admin, or a Privacy Approver (49.12.5)
			return errors.New("the approver must be an Admin or a Privacy Approver")
		}
	} else {
		approver = ""
	}
	status := NotifyAwaiting
	if decision == NotifyPending {
		status = NotifyOpen
	}
	if _, err := db.DB.Exec(fmt.Sprintf(`UPDATE %s.notification_decisions SET decision = $2, reason = $3, evidence_ref = $4, decided_by = $5,
		decided_at = now() AT TIME ZONE 'UTC', approver = $6, approved_by = '', approved_at = NULL, approval_note = '', status = $7
		WHERE id::text = $1 AND status <> 'Closed'`, schema), id, decision, redactAndBoundLogField(reason, 1000), redactAndBoundLogField(evidenceRef, 500), actor, approver, status); err != nil {
		return err
	}
	LogAuditEvent(tenantID, actor, "NOTIFY_DECISION_RECORDED", "SUCCESS", fmt.Sprintf("Decision on %q / %s: %s; approver %s", redactAndBoundLogField(d.IncidentTitle, 100), d.Row.Key, decision, approver))
	return nil
}

// ReviewNotifyDecision is the named approver's answer. Approving closes the
// decision for good; declining sends it back to Open with the approver's note.
func ReviewNotifyDecision(tenantID, actor, id string, approve bool, note, seenDecidedAt string) error {
	note = strings.TrimSpace(note)
	if !approve && len(note) < 5 {
		return errors.New("write why it is sent back (at least 5 characters)")
	}
	if len(note) > 500 {
		return errors.New("keep the note under 500 characters")
	}
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return err
	}
	d, err := loadNotifyDecision(schema, id)
	if err != nil {
		return err
	}
	if d.Status != NotifyAwaiting {
		return errors.New("this decision is not waiting for approval")
	}
	if strings.EqualFold(d.DecidedBy, actor) {
		return errors.New("the person who decided cannot approve their own decision")
	}
	if !strings.EqualFold(d.Approver, actor) {
		return errors.New("only the named approver (" + d.Approver + ") can approve this decision")
	}
	// The named person must still be an active Admin or Privacy Approver now:
	// a role taken away, or an account switched off, after they were named ends it.
	var active bool
	var role string
	if err := db.DB.QueryRow(fmt.Sprintf(`SELECT COALESCE(status, '') = 'Active', COALESCE(role, '') FROM %s.users WHERE lower(username) = lower($1)`, schema), actor).Scan(&active, &role); err != nil || !active || !CanApproveNotifyDecisions(role) {
		return errors.New("the named approver is no longer an active Admin or Privacy Approver")
	}
	// A new decision can replace the one on the approver's screen: they close the one they read.
	seen, perr := time.Parse(time.RFC3339Nano, strings.TrimSpace(seenDecidedAt))
	if perr != nil || d.DecidedAt == nil || !seen.Equal(*d.DecidedAt) {
		return errors.New("this decision changed - reload")
	}
	status, who := NotifyClosed, actor
	if !approve {
		status, who = NotifyOpen, ""
	}
	res, err := db.DB.Exec(fmt.Sprintf(`UPDATE %s.notification_decisions SET status = $2, approved_by = $3, approved_at = (CASE WHEN $5::boolean THEN now() AT TIME ZONE 'UTC' END),
		approval_note = $4 WHERE id::text = $1 AND status = 'Awaiting approval' AND lower(approver) = lower($6) AND decision = $7 AND decided_by = $8`, schema),
		id, status, who, redactAndBoundLogField(note, 500), approve, actor, d.Decision, d.DecidedBy)
	if err != nil {
		return err
	}
	if changed, _ := res.RowsAffected(); changed == 0 {
		return errors.New("this decision changed - reload")
	}
	verb := "approved"
	if !approve {
		verb = "sent back"
	}
	LogAuditEvent(tenantID, actor, "NOTIFY_DECISION_REVIEWED", "SUCCESS", fmt.Sprintf("Decision on %q / %s %s", redactAndBoundLogField(d.IncidentTitle, 100), d.Row.Key, verb))
	return nil
}

// NotifyApproverCandidates lists the active users who may be named approver
// (49.12.5): Admins, Super Admins and Privacy Approvers, never the person
// asking and never the platform's hidden support user. Role is judged by the
// same predicate RecordNotifyDecision enforces, so the picker and the rule
// cannot disagree.
func NotifyApproverCandidates(tenantID, exceptUsername string) ([]string, error) {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return nil, err
	}
	rows, err := db.DB.Query(fmt.Sprintf(`SELECT username, COALESCE(role, '') FROM %s.users
		WHERE COALESCE(status, '') = 'Active' AND %s AND lower(username) <> lower($1) ORDER BY lower(username)`, schema, NotSupportUserSQL("users")), exceptUsername)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []string{}
	for rows.Next() {
		var name, role string
		if err := rows.Scan(&name, &role); err != nil {
			return nil, err
		}
		if CanApproveNotifyDecisions(role) {
			out = append(out, name)
		}
	}
	return out, rows.Err()
}
