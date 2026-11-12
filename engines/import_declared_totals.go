package engines

import (
	"custom_erp/db"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
)

// Declared control totals and the ImportJob of a staged import (47.13.3,
// 49.8.2). The person staging a file (an opening-balance sheet above all)
// may state what each Number column must total - "amount=125000.50". The
// batch is refused at approval when the file does not tie to it, and the
// reconcile lists any column whose committed rows differ. Once a batch is
// reconciled it also writes its ImportJob (control totals, rules version and
// file lineage), exactly once, so a staged import is as visible in the job
// list as a direct one.

// ImportDeclaredTotal is one column's stated total.
type ImportDeclaredTotal struct {
	Field    string  `json:"field"`
	Label    string  `json:"label,omitempty"`
	Expected float64 `json:"expected"`
}

// ParseImportDeclaredTotals reads "field=amount, field=amount" (a comma or
// semicolon between pairs).
func ParseImportDeclaredTotals(raw string) ([]ImportDeclaredTotal, error) {
	var out []ImportDeclaredTotal
	seen := map[string]bool{}
	for _, part := range strings.FieldsFunc(raw, func(r rune) bool { return r == ',' || r == ';' || r == '\n' }) {
		f, v, ok := strings.Cut(part, "=")
		f, v = strings.TrimSpace(f), strings.ReplaceAll(strings.TrimSpace(v), " ", "")
		if !ok || f == "" {
			return nil, importBatchErr("DATAIM-0487", "state totals as column=amount, for example amount=125000.50")
		}
		n, err := strconv.ParseFloat(v, 64)
		if err != nil {
			return nil, importBatchErr("DATAIM-0487", "%s: %q is not a number", f, v)
		}
		if seen[f] {
			return nil, importBatchErr("DATAIM-0487", "%s is stated twice", f)
		}
		seen[f] = true
		out = append(out, ImportDeclaredTotal{Field: f, Expected: roundControl(n)})
	}
	return out, nil
}

// SetImportBatchDeclaredTotals records the stated totals on a batch that has
// not been approved. Each must name a Number column the file has.
func SetImportBatchDeclaredTotals(tenantID, batchID, raw string) (*ImportBatchView, error) {
	decl, err := ParseImportDeclaredTotals(raw)
	if err != nil {
		return nil, err
	}
	b, status, err := loadImportBatch(tenantID, batchID)
	if err != nil {
		return nil, err
	}
	if status != "Draft" || b.Stage == ImportStageApproved || b.Stage == ImportStageCommitted || b.Stage == ImportStageReconciled {
		return nil, importBatchErr("DATAIM-0481", "batch %s is %s; totals are stated before approval", b.ID, strings.ToLower(b.Stage))
	}
	for i := range decl {
		found := false
		for _, t := range b.FileTotals {
			if t.Field == decl[i].Field {
				decl[i].Label, found = t.Label, true
			}
		}
		if !found {
			return nil, importBatchErr("DATAIM-0487", "%s is not a number column of this file", decl[i].Field)
		}
	}
	b.Declared = decl
	if err := saveImportBatch(tenantID, b, status); err != nil {
		return nil, err
	}
	return importBatchView(b), nil
}

// importDeclaredMismatch names the stated totals the file does not tie to.
func importDeclaredMismatch(b *ImportBatchData) []string {
	var out []string
	for _, d := range b.Declared {
		for _, t := range b.FileTotals {
			if t.Field == d.Field && roundControl(t.Read) != roundControl(d.Expected) {
				out = append(out, fmt.Sprintf("%s: stated %v, file holds %v", firstNonEmpty(d.Label, d.Field), d.Expected, t.Read))
			}
		}
	}
	return out
}

// recordStagedImportJob writes the batch's ImportJob once, after reconcile.
func recordStagedImportJob(tenantID string, b *ImportBatchData, rc *ImportBatchReconcile, actorID string) error {
	if b.ImportJobID != "" {
		return nil
	}
	res := &ImportResult{
		TotalRows: rc.RowsInFile, SuccessRows: rc.Committed, FailedRows: rc.Rejected,
		RulesVersion: b.RulesVersion,
		Source:       &ImportSource{FileName: b.FileName, SizeBytes: b.SizeBytes, SHA256: b.SHA256, UploadedBy: b.UploadedBy, UploadedAt: b.UploadedAt},
		Controls: &ImportControlTotals{RowsRead: rc.RowsInFile, RowsAccepted: rc.Committed, RowsRejected: rc.Rejected,
			Created: rc.Created, Updated: rc.Updated, Balanced: rc.Balanced, Columns: rc.Columns},
	}
	id, err := RecordImportJob(tenantID, b.DoctypeName, res, firstNonEmpty(b.UploadedBy, actorID))
	if err != nil {
		return err
	}
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return err
	}
	extra, _ := json.Marshal(map[string]interface{}{"batch_id": b.ID, "staged": true})
	if _, err := db.DB.Exec(fmt.Sprintf(`UPDATE %s.documents SET data = data || $2::jsonb WHERE doctype = 'ImportJob' AND id = $1`, schema), id, string(extra)); err != nil {
		return err
	}
	b.ImportJobID = id
	return nil
}
