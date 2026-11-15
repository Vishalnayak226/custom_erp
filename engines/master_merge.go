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

// MasterMergeDoctype is a supplier / item merge request (47.13.5, ask 179):
// with an approval rule for it (migrations_stage47_13_5b_master_merge_approval
// adds HR/Admin where none exists) a merge waits in Approvals and is carried
// out when approved, like CustomerMerge; with none it is done at once.
const MasterMergeDoctype = "MasterMerge"

// MasterMergeRequest says where a merge request went: Pending Approval, or
// Merged with what the merge did.
type MasterMergeRequest struct {
	ID     string             `json:"id"`
	Status string             `json:"status"`
	Merge  *MasterMergeResult `json:"merge,omitempty"`
}

// RequestMasterMerge records a MasterMerge and routes it. The pair is checked
// now, so a request that could never be carried out is refused at once, and
// again when it is carried out, since stock or status may change in between.
func RequestMasterMerge(tenantID, master, keepID, mergeID, reason, actor, role string) (*MasterMergeRequest, error) {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return nil, err
	}
	keepID, mergeID = strings.TrimSpace(keepID), strings.TrimSpace(mergeID)
	if _, err := checkMergePair(db.DB, schema, master, keepID, mergeID); err != nil {
		return nil, err
	}
	var open string
	err = db.DB.QueryRow(fmt.Sprintf(`SELECT id FROM %s.documents WHERE doctype = $1 AND status IN ('Draft', 'Pending Approval') AND deleted_at IS NULL
		AND data->>'master_doctype' = $2 AND (data->>'merge_id' IN ($3, $4) OR data->>'keep_id' = $4) LIMIT 1`, schema),
		MasterMergeDoctype, master, keepID, mergeID).Scan(&open)
	if err == nil {
		return nil, mergeErr("a merge of %s is already waiting for approval (%s)", mergeID, open)
	}
	if err != sql.ErrNoRows {
		return nil, err
	}
	id := NewDocID("MMR")
	data, _ := json.Marshal(map[string]interface{}{"code": id, "master_doctype": master, "keep_id": keepID, "merge_id": mergeID,
		"reason": strings.TrimSpace(reason), "status": "Draft", "title": fmt.Sprintf("Merge %s %s into %s", mergeableMasters[master], mergeID, keepID)})
	if _, err := db.DB.Exec(fmt.Sprintf(`INSERT INTO %s.documents (id, doctype, data, status, created_by) VALUES ($1, $2, $3, 'Draft', $4)`, schema),
		id, MasterMergeDoctype, data, actor); err != nil {
		return nil, err
	}
	approver, err := RequiredApproverRoleForAmount(tenantID, MasterMergeDoctype, 0)
	if err == nil && approver != "" {
		err = SubmitForApproval(tenantID, MasterMergeDoctype, id, actor, role)
		if err == nil {
			return &MasterMergeRequest{ID: id, Status: "Pending Approval"}, nil
		}
	}
	if err != nil {
		// never leave a request nobody can act on
		_, _ = db.DB.Exec(fmt.Sprintf(`UPDATE %s.documents SET deleted_at = CURRENT_TIMESTAMP WHERE id = $1 AND doctype = $2`, schema), id, MasterMergeDoctype)
		return nil, err
	}
	res, err := executeMasterMerge(tenantID, id, actor)
	if err != nil {
		return nil, err
	}
	return &MasterMergeRequest{ID: id, Status: "Merged", Merge: res}, nil
}

func init() {
	RegisterApprovalDecisionHook(MasterMergeDoctype, false, func(tenantID, docID, decision, actor string) {
		if decision != "Approved" {
			return
		}
		if _, err := executeMasterMerge(tenantID, docID, actor); err != nil {
			LogSystemError(tenantID, "", "ERROR", "master_merge", fmt.Sprintf("approved merge %s failed: %v", docID, err), "")
		}
	})
}

// executeMasterMerge carries out an approved (or rule-free) MasterMerge once.
// A merge that can no longer be done (the item took stock since, the
// duplicate was merged by another request) leaves the request Approved with
// the reason in merge_error.
func executeMasterMerge(tenantID, docID, actor string) (*MasterMergeResult, error) {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return nil, err
	}
	var raw []byte
	var status string
	if err := db.DB.QueryRow(fmt.Sprintf(`SELECT data, status FROM %s.documents WHERE id = $1 AND doctype = $2`, schema), docID, MasterMergeDoctype).Scan(&raw, &status); err != nil {
		return nil, err
	}
	if status == "Merged" {
		return nil, nil
	}
	var d map[string]interface{}
	_ = json.Unmarshal(raw, &d)
	res, err := MergeMasters(tenantID, strField(d, "master_doctype"), strField(d, "keep_id"), strField(d, "merge_id"), strField(d, "reason"), actor)
	if err != nil {
		note, _ := json.Marshal(map[string]string{"merge_error": err.Error()})
		_, _ = db.DB.Exec(fmt.Sprintf(`UPDATE %s.documents SET data = data || $2::jsonb, updated_at = CURRENT_TIMESTAMP WHERE id = $1`, schema), docID, string(note))
		return nil, err
	}
	done, _ := json.Marshal(map[string]string{"status": "Merged", "merge_record": res.ID, "merge_error": ""})
	_, err = db.DB.Exec(fmt.Sprintf(`UPDATE %s.documents SET status = 'Merged', data = data || $2::jsonb, updated_at = CURRENT_TIMESTAMP, updated_by = $3 WHERE id = $1`, schema),
		docID, string(done), actor)
	return res, err
}
