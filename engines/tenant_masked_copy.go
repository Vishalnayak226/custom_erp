package engines

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"

	"custom_erp/db"
)

// Stage 170.40: a masked copy of a client in a NEW sandbox client. The copy
// reads the source in a READ ONLY transaction (so it cannot write to it) and
// writes only to a sandbox. Every field the sensitive-field list and the
// personal-data register classify as personal or protected is replaced with a
// stable fake; cost fields are business data and stay. Only the documents
// table is copied: users, passwords, sessions, credentials and logs never are.
// CopyTenantDataIntoSandbox (the unmasked copy) still always refuses.

// MaskedCopyResult says what the copy did.
type MaskedCopyResult struct {
	Source        string         `json:"source"`
	Target        string         `json:"target"`
	Copied        int            `json:"copied"`
	Skipped       int            `json:"skipped"`
	FieldsMasked  int            `json:"fields_masked"`
	SkippedTypes  []string       `json:"skipped_doctypes"`
	MaskedByField map[string]int `json:"masked_by_field"`
}

// maskSkipDoctypes are never copied: they exist to hold a secret.
var maskSkipDoctypes = map[string]bool{
	"WebhookSubscription": true, "RoboticsIntegrationCredential": true, "IntegrationCredential": true,
	"ChannelCredential": true, "ApiKey": true,
}

var maskProductionNames = map[string]bool{"default": true, "app": true, "minn": true, "prod": true, "production": true}

// MaskedFieldRule reports whether doctype.field must be masked in a copy, and
// from which list: the sensitive-field registry (every category but cost), the
// personal-data register (any kind but business), or the personal-name pattern.
func MaskedFieldRule(doctype, field string) (string, bool) {
	if r, ok := sensitiveFields[doctype][field]; ok && r.Category != SensitiveCategoryCost {
		return "sensitive:" + r.Category, true
	}
	if f, ok := LookupPersonalDataField(doctype, field); ok {
		if f.Kind != "business" {
			return "register:" + f.Kind, true
		}
		return "", false
	}
	lf := strings.ToLower(field)
	if personDoctypes[doctype] && (lf == "name" || lf == "contact_person" || lf == "full_name") {
		return "person-name", true
	}
	if lf == "contact_person" || lf == "owner_name" {
		return "person-name", true
	}
	if personalDataFieldPattern.MatchString(field) {
		return "pattern", true
	}
	return "", false
}

func maskString(field, v string) string {
	if v == "" {
		return v
	}
	h := sha256.Sum256([]byte("masked-copy|" + field + "|" + v))
	hx := hex.EncodeToString(h[:])
	lf := strings.ToLower(field)
	switch {
	case strings.Contains(lf, "email"):
		return "masked-" + hx[:10] + "@example.invalid"
	case strings.Contains(lf, "phone") || strings.Contains(lf, "mobile") || strings.Contains(lf, "whatsapp"):
		n := uint64(0)
		for _, c := range hx[:12] {
			n = n*10 + uint64(c%10)
		}
		return fmt.Sprintf("90%010d", n%10000000000)
	}
	return "MASKED-" + hx[:10]
}

// maskValue masks one value of a masked field: strings become a stable fake,
// numbers become 0, lists and objects are masked all the way down.
func maskValue(field string, v interface{}, count *int) interface{} {
	switch t := v.(type) {
	case string:
		if t == "" {
			return t
		}
		*count++
		return maskString(field, t)
	case float64:
		*count++
		return float64(0)
	case []interface{}:
		out := make([]interface{}, len(t))
		for i, e := range t {
			out[i] = maskValue(field, e, count)
		}
		return out
	case map[string]interface{}:
		out := map[string]interface{}{}
		for k, e := range t {
			out[k] = maskValue(k, e, count)
		}
		return out
	}
	return v
}

// maskDocumentData masks a document's JSON for the copy. Fields that are not
// masked are descended into, so a masked key nested in a line is found.
func maskDocumentData(doctype string, data map[string]interface{}, byField map[string]int) int {
	total := 0
	var walk func(m map[string]interface{})
	walk = func(m map[string]interface{}) {
		for k, v := range m {
			if _, mask := MaskedFieldRule(doctype, k); mask {
				n := 0
				m[k] = maskValue(k, v, &n)
				if n > 0 {
					total += n
					byField[doctype+"."+k] += n
				}
				continue
			}
			switch t := v.(type) {
			case map[string]interface{}:
				walk(t)
			case []interface{}:
				for _, e := range t {
					if em, ok := e.(map[string]interface{}); ok {
						walk(em)
					}
				}
			}
		}
	}
	walk(data)
	return total
}

// CopyTenantMaskedIntoSandbox copies sourceTenantID's documents into the
// existing sandbox, masked. It refuses a target that is not a sandbox
// or is a production name, and a source equal to the target. A fresh sandbox
// already holds the template's starter rows; the copy overwrites those by id.
func CopyTenantMaskedIntoSandbox(sourceTenantID, sandboxTenantID, actor string) (MaskedCopyResult, error) {
	res := MaskedCopyResult{Source: sourceTenantID, Target: sandboxTenantID, MaskedByField: map[string]int{}}
	if maskProductionNames[strings.ToLower(strings.TrimSpace(sandboxTenantID))] {
		return res, &ValidationError{Code: "SEC-1710", Message: "A copy can only go into a sandbox client."}
	}
	targetSchema, err := requireSandbox(db.DB, sandboxTenantID, false)
	if err != nil {
		return res, &ValidationError{Code: "SEC-1710", Message: "A copy can only go into a sandbox client."}
	}
	if strings.TrimSpace(sourceTenantID) == "" || sourceTenantID == sandboxTenantID {
		return res, &ValidationError{Code: "SEC-1711", Message: "Pick a different client to copy from."}
	}
	srcSchema, err := db.GetTenantSchema(sourceTenantID)
	if err != nil || srcSchema == targetSchema {
		return res, &ValidationError{Code: "SEC-1711", Message: "Pick a different client to copy from."}
	}
	ctx := context.Background()
	// READ ONLY: the source cannot be written through this transaction.
	srcTx, err := db.DB.BeginTx(ctx, &sql.TxOptions{ReadOnly: true, Isolation: sql.LevelRepeatableRead})
	if err != nil {
		return res, err
	}
	defer srcTx.Rollback()

	type docRow struct {
		id, doctype, status, createdBy string
		raw                            []byte
		created, updated               sql.NullTime
		version                        int
	}
	skipped := map[string]bool{}
	lastID := ""
	for {
		rows, err := srcTx.Query(fmt.Sprintf(`SELECT id, doctype, data, COALESCE(status,'Active'), created_by, created_at, updated_at, version
			FROM %s.documents WHERE deleted_at IS NULL AND id > $1 ORDER BY id LIMIT 500`, srcSchema), lastID)
		if err != nil {
			return res, err
		}
		var batch []docRow
		for rows.Next() {
			var d docRow
			if err := rows.Scan(&d.id, &d.doctype, &d.raw, &d.status, &d.createdBy, &d.created, &d.updated, &d.version); err != nil {
				rows.Close()
				return res, err
			}
			batch = append(batch, d)
		}
		rows.Close()
		if len(batch) == 0 {
			break
		}
		for _, d := range batch {
			lastID = d.id
			if maskSkipDoctypes[d.doctype] {
				skipped[d.doctype] = true
				res.Skipped++
				continue
			}
			var data map[string]interface{}
			if err := json.Unmarshal(d.raw, &data); err != nil {
				res.Skipped++
				continue
			}
			res.FieldsMasked += maskDocumentData(d.doctype, data, res.MaskedByField)
			out, err := json.Marshal(data)
			if err != nil {
				return res, err
			}
			r, err := db.DB.Exec(fmt.Sprintf(`INSERT INTO %s.documents (id, doctype, data, status, created_by, created_at, updated_at, version, edit_reason)
				VALUES ($1,$2,$3::jsonb,$4,$5,$6,$7,$8,'masked copy') ON CONFLICT (id) DO UPDATE SET doctype = EXCLUDED.doctype, data = EXCLUDED.data, status = EXCLUDED.status, updated_at = EXCLUDED.updated_at, version = EXCLUDED.version`, targetSchema),
				d.id, d.doctype, string(out), d.status, maskString("created_by", d.createdBy), d.created, d.updated, d.version)
			if err != nil {
				return res, err
			}
			if n, _ := r.RowsAffected(); n > 0 {
				res.Copied++
			} else {
				res.Skipped++
			}
		}
	}
	for k := range skipped {
		res.SkippedTypes = append(res.SkippedTypes, k)
	}
	LogAuditEvent(sandboxTenantID, actor, "MASKED_COPY", "Success",
		fmt.Sprintf("masked copy of %s: %d documents, %d values masked, %d skipped (170.40)", sourceTenantID, res.Copied, res.FieldsMasked, res.Skipped))
	return res, nil
}

// CreateMaskedCopySandbox makes a new sandbox client and fills it with a
// masked copy of sourceTenantID. It returns the new tenant id and the one-time
// admin password of the new sandbox (never a source password).
func CreateMaskedCopySandbox(sourceTenantID, appVersion string, ttlDays int, actor string) (string, string, MaskedCopyResult, error) {
	if strings.TrimSpace(sourceTenantID) == "" {
		return "", "", MaskedCopyResult{}, &ValidationError{Code: "SEC-1711", Message: "Pick a different client to copy from."}
	}
	if _, err := db.GetTenantSchema(sourceTenantID); err != nil {
		return "", "", MaskedCopyResult{}, &ValidationError{Code: "SEC-1711", Message: "Pick a different client to copy from."}
	}
	id, _, pw, err := ProvisionSandboxTenant(appVersion, ttlDays)
	if err != nil {
		return "", "", MaskedCopyResult{}, err
	}
	res, err := CopyTenantMaskedIntoSandbox(sourceTenantID, id, actor)
	return id, pw, res, err
}
