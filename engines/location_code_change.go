package engines

import (
	"custom_erp/db"
	"database/sql"
	"fmt"
	"regexp"
	"strings"

	"github.com/lib/pq"
)

// Stage 123.3: a store's own code, typed on its Edit form.
//
// The user, 2026-10-19, on prod Setup -> Location: "I want to edit user's code
// of store" - BLR Warehouse was still Location/HQ/2026/000001, and the
// Renumber panel only offers a prefix + running digits. Decided 2026-10-21: a
// Code box on the store's Edit form, any time, stores only. A store's code is
// its id, so a new code is the renumber engine's job (recodeInSchemaTx): every
// reference rewritten in one transaction, the old -> new pair kept so a till
// or an integration still sending the old code finds the store, and the same
// backup rule, since it rewrites a key across the whole client.

// locationCodePattern: capital letters and digits, - and _ between, no "/"
// (a store code travels in addresses) and no spaces.
var locationCodePattern = regexp.MustCompile(`^[A-Z0-9][A-Z0-9_-]{0,29}$`)

// LocationCodeChange is the preview and, after Apply, what was done.
type LocationCodeChange struct {
	Old        string              `json:"old"`
	New        string              `json:"new"`
	Name       string              `json:"name"`
	References int                 `json:"references"`
	Touches    []MasterRecodeTouch `json:"touches,omitempty"`
	Backup     MasterRecodeBackup  `json:"backup"`
}

// PlanLocationCodeChange checks the typed code and counts what it rewrites;
// detail adds every column with its row count.
func PlanLocationCodeChange(tenantID, id, code string, detail bool) (LocationCodeChange, error) {
	ch := LocationCodeChange{Old: strings.TrimSpace(id), New: strings.ToUpper(strings.TrimSpace(code))}
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return ch, err
	}
	err = db.DB.QueryRow(fmt.Sprintf(`SELECT COALESCE(data->>'name', '') FROM %s.documents
		WHERE doctype = 'Location' AND id = $1 AND deleted_at IS NULL`, pq.QuoteIdentifier(schema)), ch.Old).Scan(&ch.Name)
	if err == sql.ErrNoRows {
		return ch, &ValidationError{Code: "MASTER-0992", SubFor: "Location", Message: fmt.Sprintf("store %s was not found", ch.Old)}
	}
	if err != nil {
		return ch, err
	}
	if ch.New == strings.ToUpper(ch.Old) { // the box as it opened
		ch.New = ch.Old
		return ch, nil
	}
	if !locationCodePattern.MatchString(ch.New) {
		return ch, &ValidationError{Code: "MASTER-0992", SubFor: "Location", Message: "a store code is 1 to 30 capital letters or digits, with - or _ between them - no spaces or /"}
	}
	var used string
	err = db.DB.QueryRow(fmt.Sprintf(`SELECT doctype FROM %s.documents
		WHERE id = $1 OR (doctype = 'Location' AND deleted_at IS NULL AND upper(data->>'code') = $1) LIMIT 1`, pq.QuoteIdentifier(schema)), ch.New).Scan(&used)
	if err == nil {
		what := "another store"
		if used != "Location" {
			what = "a " + used + " record"
		}
		return ch, &ValidationError{Code: "MASTER-0993", SubFor: "Location", Message: fmt.Sprintf("%s already has the code %s", what, ch.New)}
	}
	if err != sql.ErrNoRows {
		return ch, err
	}
	ch.Backup = recodeBackupStatus(schema)
	_ = db.DB.QueryRow(fmt.Sprintf(`SELECT COUNT(*) FROM %s.documents
		WHERE deleted_at IS NULL AND id <> $1 AND strpos(data::text, $2) > 0`, pq.QuoteIdentifier(schema)),
		ch.Old, quotedJSONStrings([]string{ch.Old})[0]).Scan(&ch.References)
	if detail {
		ch.Touches, err = countRecodeTouches(schema, "Location", []string{ch.Old})
		return ch, err
	}
	return ch, nil
}

// ApplyLocationCodeChange gives the store its typed code and rewrites every
// reference, in one transaction - only with a backup from the last 24 hours.
func ApplyLocationCodeChange(tenantID, id, code, userID string) (LocationCodeChange, error) {
	ch, err := PlanLocationCodeChange(tenantID, id, code, false)
	if err != nil || ch.New == ch.Old {
		return ch, err
	}
	if !ch.Backup.OK {
		return ch, &ValidationError{Code: "MASTER-0990", SubFor: "Location", Message: ch.Backup.Note}
	}
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return ch, err
	}
	tx, err := db.DB.Begin()
	if err != nil {
		return ch, err
	}
	defer tx.Rollback()
	if err := recodeInSchemaTx(tx, schema, "Location", []MasterRecodeRow{{Old: ch.Old, New: ch.New, Name: ch.Name}}, userID); err != nil {
		return ch, fmt.Errorf("nothing was changed: %v", err)
	}
	if err := tx.Commit(); err != nil {
		return ch, err
	}
	InvalidateLiveUserStatesForTenant(tenantID) // users' home stores read from users
	LogAuditEvent(tenantID, userID, "LOCATION_CODE_CHANGED", "Success", fmt.Sprintf("store %s (%s) is now %s", ch.Old, ch.Name, ch.New))
	return ch, nil
}
