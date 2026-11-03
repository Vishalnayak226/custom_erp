package engines

import (
	"custom_erp/db"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

// Stage 47.13.5: merging a duplicate supplier or item into the record kept
// (the duplicate-suppliers / duplicate-items reports propose; this carries it
// out).
//
//   - The references are found from the record-type metadata, not a list kept
//     by hand: every Link field whose target is the master (doctype_fields),
//     on any record type, posted ones included. Only the link moves. A posted
//     document keeps every other field it was posted with (the name, GSTIN
//     and amounts as they were), so its printed snapshot is unchanged.
//   - Each moved reference is recorded (master_merge_rows) with the value it
//     held, so UnmergeMasters puts those values back - and leaves a record
//     alone if somebody changed that field again since.
//   - The duplicate is never deleted: it is marked Merged with merged_into
//     naming the survivor, and an unmerge returns its old status.
//   - Admin only (admin.master-merges), the level of the other master
//     clean-ups; every merge and unmerge is an audit row.
//
// Not moved: table-backed rows that name the master by code outside the
// record types (stock balances, ledger lines) and JSON line-item columns. An
// item that still holds stock is refused, so no balance is stranded.

var mergeableMasters = map[string]string{"Vendor": "supplier", "Item": "item"}

// MasterMergeResult says what a merge or unmerge did.
type MasterMergeResult struct {
	ID      string         `json:"id"`
	Doctype string         `json:"doctype"`
	KeepID  string         `json:"keep_id"`
	MergeID string         `json:"merge_id"`
	Status  string         `json:"status"`
	Moved   int            `json:"moved"`
	Skipped int            `json:"skipped,omitempty"`
	ByType  map[string]int `json:"by_type"`
}

// MasterMergeRecord is one row of the merge history.
type MasterMergeRecord struct {
	ID         string  `json:"id"`
	Doctype    string  `json:"doctype"`
	KeepID     string  `json:"keep_id"`
	MergeID    string  `json:"merge_id"`
	Reason     string  `json:"reason"`
	Status     string  `json:"status"`
	Moved      int     `json:"moved"`
	MergedBy   string  `json:"merged_by"`
	MergedAt   string  `json:"merged_at"`
	UnmergedBy *string `json:"unmerged_by"`
	UnmergedAt *string `json:"unmerged_at"`
}

type mergeLinkField struct{ doctype, field string }

type mergeQuerier interface {
	Query(query string, args ...interface{}) (*sql.Rows, error)
	QueryRow(query string, args ...interface{}) *sql.Row
}

func mergeErr(format string, a ...interface{}) error {
	return &ValidationError{Code: "GLOBAL-0002", Message: fmt.Sprintf(format, a...)}
}

// masterLinkFields lists every Link field that points at the master doctype.
func masterLinkFields(q mergeQuerier, schema, master string) ([]mergeLinkField, error) {
	rows, err := q.Query(fmt.Sprintf(`SELECT doctype_name, fieldname FROM %s.doctype_fields
		WHERE fieldtype = 'Link' AND options = $1 ORDER BY doctype_name, fieldname`, schema), master)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []mergeLinkField
	for rows.Next() {
		var f mergeLinkField
		if err := rows.Scan(&f.doctype, &f.field); err != nil {
			return nil, err
		}
		out = append(out, f)
	}
	return out, rows.Err()
}

// checkMergePair refuses a pair that must not merge; it returns the
// duplicate's current status.
func checkMergePair(q mergeQuerier, schema, master, keepID, mergeID string) (string, error) {
	what, ok := mergeableMasters[master]
	if !ok {
		return "", mergeErr("only suppliers (Vendor) and items (Item) can be merged")
	}
	if keepID == "" || mergeID == "" {
		return "", mergeErr("both the record to keep and the duplicate are required")
	}
	if keepID == mergeID {
		return "", mergeErr("the record to keep and the duplicate must be different")
	}
	status := map[string]string{}
	for _, id := range []string{keepID, mergeID} {
		var st string
		var deleted bool
		err := q.QueryRow(fmt.Sprintf(`SELECT status, deleted_at IS NOT NULL FROM %s.documents WHERE id = $1 AND doctype = $2`, schema), id, master).Scan(&st, &deleted)
		if err == sql.ErrNoRows {
			return "", mergeErr("%s %s not found", what, id)
		}
		if err != nil {
			return "", err
		}
		if deleted {
			return "", mergeErr("%s %s is deleted and cannot be merged", what, id)
		}
		if st == "Merged" || st == "Cancelled" {
			return "", mergeErr("%s %s is %s and cannot be merged", what, id, strings.ToLower(st))
		}
		status[id] = st
	}
	if master == "Item" {
		var stock int64
		if err := q.QueryRow(fmt.Sprintf(`SELECT COALESCE(SUM(ABS(on_hand) + ABS(committed) + ABS(reserved) + ABS(in_transit)), 0)
			FROM %s.inventory_availability WHERE sku = $1`, schema), mergeID).Scan(&stock); err != nil {
			return "", err
		}
		if stock != 0 {
			return "", mergeErr("item %s still holds stock - move or adjust it to zero first", mergeID)
		}
	}
	return status[mergeID], nil
}

func execCount(tx *sql.Tx, q string, args ...interface{}) (int, error) {
	r, err := tx.Exec(q, args...)
	if err != nil {
		return 0, err
	}
	n, _ := r.RowsAffected()
	return int(n), nil
}

// MergeMasters re-points every reference to the duplicate onto the record
// kept, in one transaction.
func MergeMasters(tenantID, master, keepID, mergeID, reason, actor string) (*MasterMergeResult, error) {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return nil, err
	}
	tx, err := db.DB.Begin()
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	prev, err := checkMergePair(tx, schema, master, keepID, mergeID)
	if err != nil {
		return nil, err
	}
	fields, err := masterLinkFields(tx, schema, master)
	if err != nil {
		return nil, err
	}
	id := NewDocID("MMG")
	if _, err := tx.Exec(fmt.Sprintf(`INSERT INTO %s.master_merges (id, master_doctype, keep_id, merge_id, reason, status, prev_status, merged_by)
		VALUES ($1, $2, $3, $4, $5, 'Merged', $6, $7)`, schema), id, master, keepID, mergeID, strings.TrimSpace(reason), prev, actor); err != nil {
		return nil, err
	}
	res := &MasterMergeResult{ID: id, Doctype: master, KeepID: keepID, MergeID: mergeID, Status: "Merged", ByType: map[string]int{}}
	for _, f := range fields {
		// the duplicate's own row is marked below, not re-pointed
		n, err := execCount(tx, fmt.Sprintf(`WITH moved AS (
			UPDATE %[1]s.documents SET data = jsonb_set(data, ARRAY[$3::text], to_jsonb($4::text)), updated_at = CURRENT_TIMESTAMP, updated_by = $6
			 WHERE doctype = $1 AND data->>$3 = $2 AND id <> $5 RETURNING id)
			INSERT INTO %[1]s.master_merge_rows (merge_id, doc_id, doctype, fieldname, old_value, new_value)
			SELECT $7, id, $1, $3, $2, $4 FROM moved`, schema), f.doctype, mergeID, f.field, keepID, mergeID, actor, id)
		if err != nil {
			return nil, fmt.Errorf("re-pointing %s.%s: %w", f.doctype, f.field, err)
		}
		if n > 0 {
			res.Moved += n
			res.ByType[f.doctype] += n
		}
	}
	if _, err := tx.Exec(fmt.Sprintf(`UPDATE %s.documents SET status = 'Merged', data = data || jsonb_build_object('merged_into', $2::text, 'status', 'Merged'),
		updated_at = CURRENT_TIMESTAMP, updated_by = $3 WHERE id = $1 AND doctype = $4`, schema), mergeID, keepID, actor, master); err != nil {
		return nil, err
	}
	if _, err := tx.Exec(fmt.Sprintf(`UPDATE %s.master_merges SET moved_count = $2 WHERE id = $1`, schema), id, res.Moved); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	detail, _ := json.Marshal(map[string]interface{}{"merge": id, "doctype": master, "kept": keepID, "duplicate": mergeID, "moved": res.Moved, "reason": reason})
	LogAuditEvent(tenantID, actor, "MASTER_MERGE", "SUCCESS", string(detail))
	return res, nil
}

// UnmergeMasters puts back what a merge moved: each reference gets the value
// it held, unless it has been changed again since (then it is left and
// counted as skipped), and the duplicate returns to its old status.
func UnmergeMasters(tenantID, mergeRecordID, actor string) (*MasterMergeResult, error) {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return nil, err
	}
	tx, err := db.DB.Begin()
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	var master, keepID, mergeID, status, prev string
	err = tx.QueryRow(fmt.Sprintf(`SELECT master_doctype, keep_id, merge_id, status, prev_status FROM %s.master_merges WHERE id = $1 FOR UPDATE`, schema), mergeRecordID).
		Scan(&master, &keepID, &mergeID, &status, &prev)
	if err == sql.ErrNoRows {
		return nil, mergeErr("merge %s not found", mergeRecordID)
	}
	if err != nil {
		return nil, err
	}
	if status != "Merged" {
		return nil, mergeErr("merge %s is already undone", mergeRecordID)
	}
	var dupStatus string
	if err := tx.QueryRow(fmt.Sprintf(`SELECT status FROM %s.documents WHERE id = $1 AND doctype = $2 AND deleted_at IS NULL`, schema), mergeID, master).Scan(&dupStatus); err != nil {
		return nil, mergeErr("the merged record %s is no longer there", mergeID)
	}
	res := &MasterMergeResult{ID: mergeRecordID, Doctype: master, KeepID: keepID, MergeID: mergeID, Status: "Unmerged", ByType: map[string]int{}}
	rows, err := tx.Query(fmt.Sprintf(`SELECT id, doc_id, doctype, fieldname, old_value, new_value FROM %s.master_merge_rows
		WHERE merge_id = $1 AND restored_at IS NULL ORDER BY id`, schema), mergeRecordID)
	if err != nil {
		return nil, err
	}
	type moved struct {
		id                                    int64
		doc, doctype, field, oldVal, newValue string
	}
	var list []moved
	for rows.Next() {
		var m moved
		if err := rows.Scan(&m.id, &m.doc, &m.doctype, &m.field, &m.oldVal, &m.newValue); err != nil {
			rows.Close()
			return nil, err
		}
		list = append(list, m)
	}
	rows.Close()
	for _, m := range list {
		n, err := execCount(tx, fmt.Sprintf(`UPDATE %s.documents SET data = jsonb_set(data, ARRAY[$2::text], to_jsonb($3::text)), updated_at = CURRENT_TIMESTAMP, updated_by = $6
			WHERE id = $1 AND data->>$2 = $4 AND doctype = $5`, schema), m.doc, m.field, m.oldVal, m.newValue, m.doctype, actor)
		if err != nil {
			return nil, err
		}
		if n == 0 {
			res.Skipped++ // changed again since the merge: not ours to overwrite
		} else {
			res.Moved++
			res.ByType[m.doctype]++
		}
		if _, err := tx.Exec(fmt.Sprintf(`UPDATE %s.master_merge_rows SET restored_at = CURRENT_TIMESTAMP WHERE id = $1`, schema), m.id); err != nil {
			return nil, err
		}
	}
	if _, err := tx.Exec(fmt.Sprintf(`UPDATE %s.documents SET status = $2::text, data = (data - 'merged_into') || jsonb_build_object('status', $2::text),
		updated_at = CURRENT_TIMESTAMP, updated_by = $3 WHERE id = $1 AND doctype = $4`, schema), mergeID, prev, actor, master); err != nil {
		return nil, err
	}
	if _, err := tx.Exec(fmt.Sprintf(`UPDATE %s.master_merges SET status = 'Unmerged', unmerged_by = $2, unmerged_at = CURRENT_TIMESTAMP WHERE id = $1`, schema), mergeRecordID, actor); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	detail, _ := json.Marshal(map[string]interface{}{"merge": mergeRecordID, "doctype": master, "kept": keepID, "duplicate": mergeID, "restored": res.Moved, "skipped": res.Skipped})
	LogAuditEvent(tenantID, actor, "MASTER_UNMERGE", "SUCCESS", string(detail))
	return res, nil
}

// ListMasterMerges is the merge history, newest first.
func ListMasterMerges(tenantID string) ([]MasterMergeRecord, error) {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return nil, err
	}
	rows, err := db.DB.Query(fmt.Sprintf(`SELECT id, master_doctype, keep_id, merge_id, reason, status, moved_count, merged_by, merged_at, unmerged_by, unmerged_at
		FROM %s.master_merges ORDER BY merged_at DESC, id DESC LIMIT 200`, schema))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []MasterMergeRecord{}
	for rows.Next() {
		var r MasterMergeRecord
		var at time.Time
		var uby sql.NullString
		var uat sql.NullTime
		if err := rows.Scan(&r.ID, &r.Doctype, &r.KeepID, &r.MergeID, &r.Reason, &r.Status, &r.Moved, &r.MergedBy, &at, &uby, &uat); err != nil {
			return nil, err
		}
		r.MergedAt = at.Format("2006-01-02 15:04")
		if uby.Valid {
			r.UnmergedBy = &uby.String
		}
		if uat.Valid {
			s := uat.Time.Format("2006-01-02 15:04")
			r.UnmergedAt = &s
		}
		out = append(out, r)
	}
	return out, rows.Err()
}
