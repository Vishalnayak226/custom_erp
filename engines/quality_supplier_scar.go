package engines

import (
	"custom_erp/db"
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

// 170.16 - supplier quality. A vendor non-conformance can open a SCAR
// (supplier corrective action request): the issue, a due date, then the root
// cause, the action and the verification, in that order. The vendor scorecard
// counts the open ones and shows defects per million beside the QC reject %.

const (
	scarDoctype = "SupplierCAR"

	ScarOpen      = "Open"
	ScarRootCause = "Root cause found"
	ScarAction    = "Action taken"
	ScarClosed    = "Closed"
)

// SCARInput is what opens a SCAR.
type SCARInput struct {
	Vendor  string `json:"vendor"`
	NCRID   string `json:"ncr_id"`
	Item    string `json:"item"`
	BatchNo string `json:"batch_no"`
	Issue   string `json:"issue"`
	DueDate string `json:"due_date"`
}

// OpenSCAR raises a supplier corrective action request. When it names a
// non-conformance report, that report must exist and carry no other open SCAR;
// a blank item / lot / issue is taken from it.
func OpenSCAR(tenantID string, in SCARInput, userID string) (string, error) {
	in.Vendor, in.NCRID = strings.TrimSpace(in.Vendor), strings.TrimSpace(in.NCRID)
	in.Item, in.BatchNo = strings.TrimSpace(in.Item), strings.TrimSpace(in.BatchNo)
	in.Issue, in.DueDate = strings.TrimSpace(in.Issue), strings.TrimSpace(in.DueDate)
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return "", err
	}
	if in.NCRID != "" {
		ncr, _, errN := fetchDocData(tenantID, "NonConformanceReport", in.NCRID)
		if errN != nil || ncr == nil {
			return "", qualityErr("QUALIT-1763", "non-conformance report %s not found", in.NCRID)
		}
		in.Item = firstNonEmpty(in.Item, strField(ncr, "item"))
		in.BatchNo = firstNonEmpty(in.BatchNo, strField(ncr, "batch_no"))
		in.Issue = firstNonEmpty(in.Issue, strField(ncr, "description"))
		var open int
		if errQ := db.DB.QueryRow(fmt.Sprintf(`SELECT COUNT(*) FROM %s.documents WHERE doctype = $1 AND deleted_at IS NULL
			AND data->>'ncr_id' = $2 AND COALESCE(status, '') <> $3`, schema), scarDoctype, in.NCRID, ScarClosed).Scan(&open); errQ != nil {
			return "", errQ
		}
		if open > 0 {
			return "", qualityErr("QUALIT-1764", "%s already has an open SCAR", in.NCRID)
		}
	}
	if in.Vendor == "" {
		return "", qualityErr("QUALIT-1763", "name the vendor this SCAR is for")
	}
	if v, _, errV := fetchDocData(tenantID, "Vendor", in.Vendor); errV != nil || v == nil {
		return "", qualityErr("QUALIT-1763", "vendor %s not found", in.Vendor)
	} else if c := strField(v, "code"); c != "" {
		in.Vendor = c
	}
	if in.Issue == "" {
		return "", qualityErr("QUALIT-1763", "describe the issue")
	}
	if _, errD := time.Parse("2006-01-02", in.DueDate); errD != nil {
		return "", qualityErr("QUALIT-1763", "give the date the vendor must reply by")
	}
	id := NewDocID("SCAR")
	data := map[string]interface{}{
		"id": id, "code": id, "vendor": in.Vendor, "ncr_id": in.NCRID, "item": in.Item, "batch_no": in.BatchNo,
		"issue": in.Issue, "due_date": in.DueDate, "status": ScarOpen, "opened_by": userID,
	}
	b, _ := json.Marshal(data)
	if _, err := db.DB.Exec(fmt.Sprintf(
		`INSERT INTO %s.documents (id, doctype, data, status, created_by) VALUES ($1, $2, $3, $4, $5)`, schema),
		id, scarDoctype, b, ScarOpen, userID); err != nil {
		return "", err
	}
	LogAuditEvent(tenantID, userID, "SCAR_OPENED", "SUCCESS", fmt.Sprintf("%s: vendor %s, %s", id, in.Vendor, in.Issue))
	return id, nil
}

// scarSteps: step -> (from status, to status, field it records).
var scarSteps = map[string]struct{ from, to, field string }{
	"root_cause":   {ScarOpen, ScarRootCause, "root_cause"},
	"action":       {ScarRootCause, ScarAction, "action"},
	"verification": {ScarAction, ScarClosed, "verification"},
}

// AdvanceSCAR records the next step of a SCAR: the root cause, the action,
// then the verification, which closes it. A step out of order or with no text
// is refused.
func AdvanceSCAR(tenantID, id, step, text, userID string) error {
	st, ok := scarSteps[step]
	text = strings.TrimSpace(text)
	if !ok {
		return qualityErr("QUALIT-1765", "the step must be root_cause, action or verification")
	}
	if text == "" {
		return qualityErr("QUALIT-1765", "write the %s", strings.ReplaceAll(step, "_", " "))
	}
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return err
	}
	tx, err := db.DB.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	data, status, err := lockDocTx(tx, schema, scarDoctype, id)
	if err != nil {
		return qualityErr("QUALIT-1765", "SCAR %s not found", id)
	}
	if status != st.from {
		return qualityErr("QUALIT-1765", "%s is %s - this step needs it to be %s", id, status, st.from)
	}
	data[st.field] = text
	data[st.field+"_by"] = userID
	data[st.field+"_on"] = time.Now().UTC().Format("2006-01-02")
	if err := writeDocTx(tx, schema, scarDoctype, id, data, st.to); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	LogAuditEvent(tenantID, userID, "SCAR_STEP", "SUCCESS", fmt.Sprintf("%s: %s -> %s", id, st.from, st.to))
	return nil
}

// openSCARCounts is the number of not-yet-closed SCARs per vendor.
func openSCARCounts(schema string) map[string]int {
	out := map[string]int{}
	rows, err := db.DB.Query(fmt.Sprintf(`SELECT COALESCE(data->>'vendor', ''), COUNT(*) FROM %s.documents
		WHERE doctype = $1 AND deleted_at IS NULL AND COALESCE(status, '') <> $2 GROUP BY 1`, schema), scarDoctype, ScarClosed)
	if err != nil {
		return out
	}
	defer rows.Close()
	for rows.Next() {
		var v string
		var n int
		if rows.Scan(&v, &n) == nil {
			out[v] = n
		}
	}
	return out
}

// defectsPerMillion is rejected quantity per million received; nil with nothing received.
func defectsPerMillion(received, accepted float64) *float64 {
	if received <= 0 {
		return nil
	}
	v := float64(int64((received-accepted)/received*1e6*10+0.5)) / 10
	return &v
}

func dpmText(p *float64) string {
	if p == nil {
		return "No data"
	}
	return fmt.Sprintf("%.0f", *p)
}
