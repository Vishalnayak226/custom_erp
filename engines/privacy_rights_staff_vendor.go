package engines

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
	"time"

	"custom_erp/db"
	"github.com/lib/pq"
)

// Stage 170.39: export and erase (anonymise) for an Employee and a Vendor, the
// same request flow as a Customer. Payroll, tax and ledger records are kept:
// bank, PAN, UAN, ESIC and the employee code (Employee) and name, GSTIN and
// bank (Vendor) stay because those documents still refer to them; the
// personal fields not needed for them go.

// vendorOpenStatuses: a vendor with a document in one of these cannot be
// anonymised yet (doctype -> field that names the vendor -> open statuses).
var vendorOpenDocs = []struct {
	Doctype, Field string
	Open           []string
}{
	{"PurchaseOrder", "vendor", []string{"Draft", "Pending Approval", "Approved"}},
	{"VendorInvoice", "vendor_id", []string{"Draft", "Pending Approval", "Approved", "Posted", "Unpaid", "Partially Paid"}},
	{"GRN", "vendor", []string{"Pending"}},
	{"PurchaseReturn", "vendor_id", []string{"Draft", "Pending Approval", "Approved"}},
}

// Fields kept for statutory reasons, never anonymised here.
var staffKeptFields = map[string]bool{
	"code": true, "bank_account_no": true, "bank_ifsc": true, "pan": true, "uan": true, "esic_no": true,
	"date_of_joining": true, "date_of_exit": true, "status": true, "department": true, "designation": true,
}
var vendorKeptFields = map[string]bool{
	"name": true, "gstin": true, "pan": true, "bank_account_number": true, "bank_ifsc": true, "status": true, "code": true,
}

func privacyRefusal(tenantID, actor, code, msg, detail string) error {
	LogAuditEvent(tenantID, actor, "data_subject_erasure_refused", "Refused", detail)
	return &ValidationError{Code: code, Message: msg}
}

func loadSubject(schema, doctype, id string) (map[string]interface{}, error) {
	var raw []byte
	if err := db.DB.QueryRow(fmt.Sprintf(`SELECT data FROM %s.documents WHERE doctype = $1 AND id = $2 AND deleted_at IS NULL`, schema), doctype, id).Scan(&raw); err != nil {
		return nil, &ValidationError{Code: "SEC-1709", Message: fmt.Sprintf("%s %s was not found.", doctype, id)}
	}
	var data map[string]interface{}
	if err := json.Unmarshal(raw, &data); err != nil {
		return nil, err
	}
	return data, nil
}

// ExportStaffVendorData is the access / export answer for the master record.
func ExportStaffVendorData(tenantID, doctype, id string) (map[string]interface{}, error) {
	if doctype != "Employee" && doctype != "Vendor" {
		return nil, &ValidationError{Code: "SEC-1709", Message: "Only an employee or a vendor can be exported here."}
	}
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return nil, err
	}
	return loadSubject(schema, doctype, id)
}

// AnonymizeStaffOrVendor erases the personal fields of an Employee or Vendor
// or refuses with the reason (legal hold, still active, inside the statutory
// keep window, open documents).
func AnonymizeStaffOrVendor(tenantID, doctype, id, actor, reason string) error {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return err
	}
	data, err := loadSubject(schema, doctype, id)
	if err != nil {
		return err
	}
	if hold, _ := data["legal_hold"].(bool); hold {
		why, _ := data["legal_hold_reason"].(string)
		return privacyRefusal(tenantID, actor, "SEC-1705", "Under legal hold: "+why,
			fmt.Sprintf("refused: %s %s is under legal hold (%s)", doctype, id, why))
	}
	if held, _ := tenantLegalHold(tenantID); held {
		return privacyRefusal(tenantID, actor, "SEC-1705", "This client is under legal hold.",
			fmt.Sprintf("refused: client under legal hold, %s %s", doctype, id))
	}
	kept := vendorKeptFields
	if doctype == "Employee" {
		kept = staffKeptFields
		status, _ := data["status"].(string)
		if status == "" || strings.EqualFold(status, "Active") {
			return privacyRefusal(tenantID, actor, "SEC-1706", "Employee is still active.",
				fmt.Sprintf("refused: employee %s is still active", id))
		}
		exit, _ := data["date_of_exit"].(string)
		keepDays := RetentionDays(tenantID, "Employee", "", 0)
		if t, perr := time.Parse("2006-01-02", privacyDateHead(exit, 10)); perr == nil && keepDays > 0 &&
			time.Now().Before(t.AddDate(0, 0, keepDays)) {
			return privacyRefusal(tenantID, actor, "SEC-1707", "Payroll records must be kept until "+t.AddDate(0, 0, keepDays).Format("2006-01-02")+".",
				fmt.Sprintf("refused: employee %s inside the retention window", id))
		}
	} else {
		for _, d := range vendorOpenDocs {
			var n int
			if err := db.DB.QueryRow(fmt.Sprintf(`SELECT COUNT(*) FROM %s.documents WHERE doctype = $1 AND deleted_at IS NULL AND data->>$2 = $3 AND status = ANY($4)`, schema),
				d.Doctype, d.Field, id, pq.Array(d.Open)).Scan(&n); err == nil && n > 0 {
				return privacyRefusal(tenantID, actor, "SEC-1708", fmt.Sprintf("Vendor has %d open %s.", n, d.Doctype),
					fmt.Sprintf("refused: vendor %s has %d open %s", id, n, d.Doctype))
			}
		}
	}
	patch := map[string]interface{}{
		"anonymized_at": time.Now().UTC().Format(time.RFC3339), "anonymized_by": actor,
	}
	if doctype == "Employee" {
		patch["name"] = "Redacted Employee"
	}
	for k, v := range data {
		if kept[k] || k == "name" || strings.HasPrefix(k, "legal_hold") || strings.HasPrefix(k, "anonymized") {
			continue
		}
		if _, isStr := v.(string); !isStr && v != nil {
			continue
		}
		if statutoryFieldPattern.MatchString(k) {
			continue
		}
		if k == "user_id" || k == "reporting_manager" || k == "location" || personalDataFieldPattern.MatchString(k) || vendorPersonalField(k) {
			if v != nil && v != "" {
				patch[k] = ""
			}
		}
	}
	b, err := json.Marshal(patch)
	if err != nil {
		return err
	}
	if _, err := db.DB.Exec(fmt.Sprintf(`UPDATE %s.documents SET data = data || $1::jsonb, updated_at = CURRENT_TIMESTAMP, version = version + 1,
		updated_by = $4, edit_reason = 'personal data erasure' WHERE doctype = $2 AND id = $3`, schema), string(b), doctype, id, actor); err != nil {
		return err
	}
	LogAuditEvent(tenantID, actor, "data_subject_erasure_completed", "Success",
		fmt.Sprintf("%s %s anonymised, payroll/tax/ledger fields kept (reason: %s) (170.39)", doctype, id, reason))
	return nil
}

// statutoryFieldPattern: payroll, tax and bank fields are kept whatever else matches.
var statutoryFieldPattern = regexp.MustCompile(`(?i)(^|_)(bank|ifsc|pan|gstin|uan|esic|salary|account|tax|tds|gst|pf)($|_)`)

func vendorPersonalField(k string) bool {
	switch k {
	case "contact_person", "contact_phone", "contact_email", "address", "address_line2", "city", "pincode", "phone", "email":
		return true
	}
	return false
}

func privacyDateHead(s string, n int) string {
	if len(s) > n {
		return s[:n]
	}
	return s
}
