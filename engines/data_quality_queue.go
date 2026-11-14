package engines

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"custom_erp/db"
)

// Stage 170.41: one queue for master-data problems, DataQualityIssue, beside
// Stage 72's ItemException queue (which it does not touch). A scan finds
// duplicate customers / vendors / items, items with no HSN and an invalid
// GSTIN or PAN. Each problem is one row with a fixed id, so a rescan never
// duplicates an open row; a row whose problem is gone becomes Fixed; a Fixed
// row whose problem returns reopens; an Ignored row stays ignored.

const (
	dataQualityDoctype = "DataQualityIssue"
	DQStatusOpen       = "Open"
	DQStatusFixed      = "Fixed"
	DQStatusIgnored    = "Ignored"
)

// DataQualityFinding is one detected problem.
type DataQualityFinding struct {
	RecordDoctype, RecordID, Code, Problem string
}

// DataQualityScanResult says what a scan changed.
type DataQualityScanResult struct {
	Found    int `json:"found"`
	Opened   int `json:"opened"`
	Reopened int `json:"reopened"`
	Fixed    int `json:"fixed"`
}

func dataQualityID(tenantID string, f DataQualityFinding) string {
	sum := sha256.Sum256([]byte(tenantID + "|" + f.RecordDoctype + "|" + f.RecordID + "|" + f.Code))
	return "DQI-" + strings.ToUpper(hex.EncodeToString(sum[:8]))
}

// findDataQualityProblems runs every check read-only.
func findDataQualityProblems(schema string) ([]DataQualityFinding, error) {
	var out []DataQualityFinding
	dupes := []struct {
		doctype, code, problem, expr string
	}{
		{"Customer", "dup_phone", "Same phone as another customer", `NULLIF(regexp_replace(COALESCE(data->>'phone',''), '[^0-9]', '', 'g'), '')`},
		{"Customer", "dup_email", "Same email as another customer", `NULLIF(lower(trim(COALESCE(data->>'email',''))), '')`},
		{"Vendor", "dup_gstin", "Same GSTIN as another vendor", `NULLIF(upper(trim(COALESCE(data->>'gstin',''))), '')`},
		{"Vendor", "dup_name", "Same name as another vendor", `NULLIF(lower(trim(COALESCE(data->>'name',''))), '')`},
		{"Item", "dup_name", "Same name as another item", `NULLIF(lower(trim(COALESCE(NULLIF(data->>'item_name',''), data->>'name', ''))), '')`},
	}
	for _, d := range dupes {
		rows, err := db.DB.Query(fmt.Sprintf(`SELECT id FROM (SELECT id, %[2]s AS k, COUNT(*) OVER (PARTITION BY %[2]s) AS n
			FROM %[1]s.documents WHERE doctype = $1 AND deleted_at IS NULL AND COALESCE(data->>'anonymized_at','') = '') x
			WHERE k IS NOT NULL AND n > 1`, schema, d.expr), d.doctype)
		if err != nil {
			return nil, err
		}
		for rows.Next() {
			var id string
			if rows.Scan(&id) == nil {
				out = append(out, DataQualityFinding{d.doctype, id, d.code, d.problem})
			}
		}
		rows.Close()
	}
	rows, err := db.DB.Query(fmt.Sprintf(`SELECT id FROM %s.documents WHERE doctype = 'Item' AND deleted_at IS NULL
		AND COALESCE(trim(data->>'hsn_code'), '') = ''`, schema))
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var id string
		if rows.Scan(&id) == nil {
			out = append(out, DataQualityFinding{"Item", id, "missing_hsn", "No HSN code"})
		}
	}
	rows.Close()
	for _, dt := range []string{"Vendor", "Customer"} {
		rows, err := db.DB.Query(fmt.Sprintf(`SELECT id, upper(trim(COALESCE(data->>'gstin',''))), upper(trim(COALESCE(data->>'pan',''))) FROM %s.documents
			WHERE doctype = $1 AND deleted_at IS NULL AND (COALESCE(trim(data->>'gstin'),'') <> '' OR COALESCE(trim(data->>'pan'),'') <> '')`, schema), dt)
		if err != nil {
			return nil, err
		}
		for rows.Next() {
			var id, gstin, pan string
			if rows.Scan(&id, &gstin, &pan) != nil {
				continue
			}
			if gstin != "" && (!gstinPattern.MatchString(gstin) || !ValidGSTINChecksum(gstin)) {
				out = append(out, DataQualityFinding{dt, id, "bad_gstin", "GSTIN is not valid"})
			}
			if pan != "" && !panPattern.MatchString(pan) {
				out = append(out, DataQualityFinding{dt, id, "bad_pan", "PAN is not valid"})
			}
		}
		rows.Close()
	}
	return out, nil
}

// ScanDataQuality fills and closes the queue. Safe to run any number of times.
func ScanDataQuality(tenantID, actor string) (DataQualityScanResult, error) {
	var res DataQualityScanResult
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return res, err
	}
	found, err := findDataQualityProblems(schema)
	if err != nil {
		return res, err
	}
	res.Found = len(found)
	seen := map[string]bool{}
	now := time.Now().UTC().Format(time.RFC3339)
	for _, f := range found {
		id := dataQualityID(tenantID, f)
		seen[id] = true
		data, _ := json.Marshal(map[string]interface{}{
			"record_doctype": f.RecordDoctype, "record_id": f.RecordID, "problem_code": f.Code,
			"problem": f.Problem, "found_at": now, "status": DQStatusOpen,
		})
		r, err := db.DB.Exec(fmt.Sprintf(`INSERT INTO %s.documents (id, doctype, data, status, created_by)
			VALUES ($1, $2, $3, $4, 'system') ON CONFLICT DO NOTHING`, schema), id, dataQualityDoctype, data, DQStatusOpen)
		if err != nil {
			return res, err
		}
		if n, _ := r.RowsAffected(); n > 0 {
			res.Opened++
			continue
		}
		// Exists: a Fixed row whose problem is back reopens, with a new found-at.
		r, err = db.DB.Exec(fmt.Sprintf(`UPDATE %s.documents SET status = 'Open', data = data || jsonb_build_object('status','Open','found_at',$2::text),
			updated_at = CURRENT_TIMESTAMP, version = version + 1 WHERE id = $1 AND doctype = $3 AND status = 'Fixed'`, schema), id, now, dataQualityDoctype)
		if err != nil {
			return res, err
		}
		if n, _ := r.RowsAffected(); n > 0 {
			res.Reopened++
		}
	}
	// Close open rows whose problem is gone.
	rows, err := db.DB.Query(fmt.Sprintf(`SELECT id FROM %s.documents WHERE doctype = $1 AND status = 'Open' AND deleted_at IS NULL`, schema), dataQualityDoctype)
	if err != nil {
		return res, err
	}
	var gone []string
	for rows.Next() {
		var id string
		if rows.Scan(&id) == nil && !seen[id] {
			gone = append(gone, id)
		}
	}
	rows.Close()
	for _, id := range gone {
		if _, err := db.DB.Exec(fmt.Sprintf(`UPDATE %s.documents SET status = 'Fixed', data = data || '{"status":"Fixed"}'::jsonb,
			updated_at = CURRENT_TIMESTAMP, version = version + 1 WHERE id = $1 AND doctype = $2 AND status = 'Open'`, schema), id, dataQualityDoctype); err != nil {
			return res, err
		}
		res.Fixed++
	}
	LogAuditEvent(tenantID, actor, "DATA_QUALITY_SCAN", "Success",
		fmt.Sprintf("%d problem(s) found, %d opened, %d reopened, %d fixed (170.41)", res.Found, res.Opened, res.Reopened, res.Fixed))
	return res, nil
}

// SetDataQualityStatus ignores (with a reason), reopens or closes one row.
func SetDataQualityStatus(tenantID, id, status, reason, actor string) error {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return err
	}
	if err := checkDataQualityStatus(status, reason); err != nil {
		return err
	}
	patch, _ := json.Marshal(map[string]string{"status": status, "ignore_reason": reason})
	r, err := db.DB.Exec(fmt.Sprintf(`UPDATE %s.documents SET status = $3, data = data || $4::jsonb, updated_at = CURRENT_TIMESTAMP,
		version = version + 1, updated_by = $5 WHERE id = $1 AND doctype = $2 AND deleted_at IS NULL`, schema),
		id, dataQualityDoctype, status, string(patch), actor)
	if err != nil {
		return err
	}
	if n, _ := r.RowsAffected(); n == 0 {
		return &ValidationError{Code: "MASTER-1702", Message: "That problem was not found."}
	}
	LogAuditEvent(tenantID, actor, "DATA_QUALITY_STATUS", "Success", fmt.Sprintf("%s set to %s (170.41)", id, status))
	return nil
}

func checkDataQualityStatus(status, reason string) error {
	switch status {
	case DQStatusOpen, DQStatusFixed:
		return nil
	case DQStatusIgnored:
		if strings.TrimSpace(reason) == "" {
			return &ValidationError{Code: "MASTER-1700", Message: "Say why this is ignored."}
		}
		return nil
	}
	return &ValidationError{Code: "MASTER-1701", Message: "Status must be Open, Fixed or Ignored."}
}

// validateDataQualityIssueRules is the ValidateMasterDataRules case: an edit
// from the generic screen follows the same rules as SetDataQualityStatus.
func validateDataQualityIssueRules(payload map[string]interface{}) error {
	if strField(payload, "status") == "" {
		return nil
	}
	return checkDataQualityStatus(strField(payload, "status"), strField(payload, "ignore_reason"))
}

// DataQualityRow is one queue line for the screen.
type DataQualityRow struct {
	ID            string `json:"id"`
	RecordDoctype string `json:"record_doctype"`
	RecordID      string `json:"record_id"`
	Problem       string `json:"problem"`
	FoundAt       string `json:"found_at"`
	Status        string `json:"status"`
	IgnoreReason  string `json:"ignore_reason"`
}

// ListDataQualityIssues returns the queue, newest first; status "" = all.
func ListDataQualityIssues(tenantID, status string) ([]DataQualityRow, error) {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return nil, err
	}
	rows, err := db.DB.Query(fmt.Sprintf(`SELECT id, COALESCE(data->>'record_doctype',''), COALESCE(data->>'record_id',''), COALESCE(data->>'problem',''),
		COALESCE(data->>'found_at',''), status, COALESCE(data->>'ignore_reason','')
		FROM %s.documents WHERE doctype = $1 AND deleted_at IS NULL AND ($2 = '' OR status = $2)
		ORDER BY data->>'found_at' DESC, id LIMIT 1000`, schema), dataQualityDoctype, status)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []DataQualityRow{}
	for rows.Next() {
		var r DataQualityRow
		if err := rows.Scan(&r.ID, &r.RecordDoctype, &r.RecordID, &r.Problem, &r.FoundAt, &r.Status, &r.IgnoreReason); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// DataQualityInboxItems is the My work line: one row with the open count.
func DataQualityInboxItems(tenantID string) ([]TaskInboxItem, error) {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return nil, err
	}
	var n int
	var oldest *time.Time
	if err := db.DB.QueryRow(fmt.Sprintf(`SELECT COUNT(*), MIN(created_at) FROM %s.documents WHERE doctype = $1 AND status = 'Open' AND deleted_at IS NULL`, schema),
		dataQualityDoctype).Scan(&n, &oldest); err != nil {
		return nil, err
	}
	if n == 0 {
		return nil, nil
	}
	since := time.Now()
	if oldest != nil {
		since = *oldest
	}
	return []TaskInboxItem{{Source: "Data quality", Title: fmt.Sprintf("Data quality: %d", n), Reason: "Duplicates, missing HSN, bad GSTIN or PAN",
		DocType: dataQualityDoctype, View: "dataquality", Count: n, WaitingSince: since}}, nil
}
