package engines

import (
	"custom_erp/db"
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

// Stage 47.12.4: each guided-setup step's state history. A step's state is
// worked out from live data on every read (setup_readiness.go); what was
// missing is the record of how it got there. recordSetupStates writes a row
// when a step's state differs from the last one on record - who looked, when,
// from what to what, what was failing - and AddSetupStepNote lets a person
// add a note (and an optional attachment reference) to a step. Rows are only
// ever added: a database trigger refuses UPDATE and DELETE
// (db/migrations_stage47_12_4_setup_step_history.sql).
//
// A state change shows up when the setup screen or any reader of the path
// next loads it, and is credited to that reader; the "who" of the data change
// itself is in each record's own audit trail.
//
// Per-step owners are not stored (a decision, 47.12.4).

// SetupStepHistoryRow is one history entry.
type SetupStepHistoryRow struct {
	ID               int64        `json:"id"`
	StepID           string       `json:"step_id"`
	Kind             string       `json:"kind"` // State or Note
	FromState        string       `json:"from_state"`
	ToState          string       `json:"to_state"`
	Severity         string       `json:"severity,omitempty"`
	ValidatorVersion string       `json:"validator_version,omitempty"`
	Failing          []SetupCheck `json:"failing,omitempty"`
	Note             string       `json:"note,omitempty"`
	Attachment       string       `json:"attachment,omitempty"`
	ChangedBy        string       `json:"changed_by"`
	ChangedAt        string       `json:"changed_at"`
}

// recordSetupStates appends a State row for each step whose state is not the
// last one recorded. It never fails the read it rides on.
func recordSetupStates(schema string, steps []SetupPathStep, actor string) {
	if actor == "" {
		actor = "system"
	}
	for _, s := range steps {
		failing := []SetupCheck{}
		for _, c := range s.Checks {
			if !c.Passed {
				failing = append(failing, c)
			}
		}
		raw, _ := json.Marshal(failing)
		_, _ = db.DB.Exec(fmt.Sprintf(`INSERT INTO %[1]s.setup_step_history (step_id, kind, from_state, to_state, severity, validator_version, failing, changed_by)
			SELECT $1::text, 'State', COALESCE((SELECT to_state FROM %[1]s.setup_step_history WHERE step_id = $1 AND kind = 'State' ORDER BY id DESC LIMIT 1), ''),
			       $2::text, $3::text, $4::text, $5::jsonb, $6::text
			 WHERE COALESCE((SELECT to_state FROM %[1]s.setup_step_history WHERE step_id = $1 AND kind = 'State' ORDER BY id DESC LIMIT 1), '') <> $2::text`, schema),
			s.ID, s.State, s.Severity, s.ValidatorVersion, string(raw), actor)
	}
}

func validSetupStepID(id string) bool {
	for _, p := range setupPathPlan {
		if p.ID == id {
			return true
		}
	}
	return false
}

// AddSetupStepNote adds a note (and optionally an attachment reference - a
// file name or link) to a step's history.
func AddSetupStepNote(tenantID, stepID, note, attachment, actor string) error {
	note, attachment = strings.TrimSpace(note), strings.TrimSpace(attachment)
	if !validSetupStepID(stepID) {
		return &ValidationError{Code: "GLOBAL-0004", Message: fmt.Sprintf("setup step %q not found", stepID)}
	}
	if note == "" && attachment == "" {
		return &ValidationError{Code: "GLOBAL-0002", Message: "a note or an attachment is required"}
	}
	if len(note) > 2000 || len(attachment) > 500 {
		return &ValidationError{Code: "GLOBAL-0002", Message: "the note or attachment is too long"}
	}
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return err
	}
	_, err = db.DB.Exec(fmt.Sprintf(`INSERT INTO %s.setup_step_history (step_id, kind, note, attachment, changed_by) VALUES ($1, 'Note', $2, $3, $4)`, schema),
		stepID, note, attachment, actor)
	return err
}

// ListSetupStepHistory returns a step's history, newest first (up to 200).
// An empty stepID returns every step's.
func ListSetupStepHistory(tenantID, stepID string) ([]SetupStepHistoryRow, error) {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return nil, err
	}
	if stepID != "" && !validSetupStepID(stepID) {
		return nil, &ValidationError{Code: "GLOBAL-0004", Message: fmt.Sprintf("setup step %q not found", stepID)}
	}
	rows, err := db.DB.Query(fmt.Sprintf(`SELECT id, step_id, kind, from_state, to_state, severity, validator_version, failing::text, note, attachment, changed_by, changed_at
		FROM %s.setup_step_history WHERE ($1 = '' OR step_id = $1) ORDER BY id DESC LIMIT 200`, schema), stepID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []SetupStepHistoryRow{}
	for rows.Next() {
		var r SetupStepHistoryRow
		var failing string
		var at time.Time
		if err := rows.Scan(&r.ID, &r.StepID, &r.Kind, &r.FromState, &r.ToState, &r.Severity, &r.ValidatorVersion, &failing, &r.Note, &r.Attachment, &r.ChangedBy, &at); err != nil {
			return nil, err
		}
		_ = json.Unmarshal([]byte(failing), &r.Failing)
		r.ChangedAt = at.Format("2006-01-02 15:04")
		out = append(out, r)
	}
	return out, rows.Err()
}
