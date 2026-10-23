package engines

import (
	"custom_erp/db"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"
)

// Stage 130.16 (74.3.4, BRD 4.11, DEC-17/20): the rest of print profiles.
//
//   - Template versions. Every save of a print template is a numbered
//     version; an old one can be restored (as a new version). With
//     print.template_approval on, a saved change waits - the template keeps
//     printing its active version - until someone else approves it. The print
//     log records which version printed (74.1.4).
//   - A profile can be for a legal entity and a channel (POS, Online, B2B,
//     ERP) as well as a document and store; the most specific wins:
//     document + store, store, entity + channel, entity, all.
//   - A tax document from a store whose legal entity has no GSTIN is refused
//     when print.tax_document_needs_gstin is on - and a client with no GSTIN
//     anywhere (not registered) prints a Bill of Supply instead.
//   - The till receipt lists every tender, the change, and the return policy.

const (
	SettingTemplateApproval      = "print.template_approval"
	SettingTaxDocumentNeedsGSTIN = "print.tax_document_needs_gstin"
	SettingReceiptReturnPolicy   = "pos.receipt_return_policy"
	PrintTemplateVersionDoctype  = "PrintTemplateVersion"
)

// taxDocumentTxns are the prints that are tax documents.
var taxDocumentTxns = map[string]bool{PrintTxnSalesInvoice: true, PrintTxnOMSBill: true, PrintTxnPOSTaxInvoice: true,
	PrintTxnCreditNote: true, PrintTxnDebitNote: true}

func init() {
	RegisterSetting(SettingDefinition{
		Key: SettingTemplateApproval, Module: "Printing",
		Label: "Template changes need approval", Type: SettingTypeBool, Default: "false",
		Description: "On: a saved change to a print template waits for someone else to approve it (Setup > Printing > Templates > Versions); the template keeps printing its approved version until then. Off: every save prints at once. Either way each save is a version you can restore.",
	})
	RegisterSetting(SettingDefinition{
		Key: SettingTaxDocumentNeedsGSTIN, Module: "Printing",
		Label: "Tax documents need the store's GSTIN", Type: SettingTypeBool, Default: "false",
		Description: "On: a tax invoice, credit or debit note is not printed from a store whose legal entity has no GSTIN (set it under Setup > Legal Entities). A business with no GSTIN at all prints a Bill of Supply instead of a tax invoice.",
	})
	RegisterSetting(SettingDefinition{
		Key: SettingReceiptReturnPolicy, Module: "Point of Sale",
		Label: "Return policy on the till receipt", Type: SettingTypeString, Default: "",
		Description: "A line printed at the foot of every till receipt, e.g. \"Exchange within 7 days with this bill. No cash refund.\" Blank prints none.",
	})
	RegisterReport(ReportDefinition{ID: "print-template-coverage", Label: "Print template coverage", Category: "Admin",
		Columns: []ReportColumn{{Key: "transaction", Label: "Print"}, {Key: "doctype", Label: "Record"}, {Key: "templates", Label: "Templates"},
			{Key: "default_template", Label: "Default template"}, {Key: "profiles", Label: "Profiles"}, {Key: "copy_labels", Label: "Copies"}, {Key: "prints_30d", Label: "Printed (30 days)"},
			{Key: "pending_versions", Label: "Changes awaiting approval"}},
		Run: printTemplateCoverage, StoreKey: "-"})
	RegisterReport(ReportDefinition{ID: "print-failures", Label: "Failed prints and fallbacks", Category: "Admin",
		Params: []ReportParam{{Key: "from", Label: "From", Type: "date"}, {Key: "to", Label: "To", Type: "date"}},
		Columns: []ReportColumn{{Key: "printed_at", Label: "When"}, {Key: "job", Label: "Print"}, {Key: "document", Label: "Document"}, {Key: "printer", Label: "Printer"},
			{Key: "outcome", Label: "Outcome"}, {Key: "detail", Label: "Detail"}, {Key: "printed_by", Label: "By"}},
		Run: printFailures, StoreKey: "-"})
	RegisterReport(ReportDefinition{ID: "missing-regulatory-print-data", Label: "Missing regulatory print data", Category: "Admin",
		Columns: []ReportColumn{{Key: "record", Label: "Record"}, {Key: "code", Label: "Code"}, {Key: "name", Label: "Name"}, {Key: "missing", Label: "Missing"}, {Key: "why", Label: "Why it matters"}},
		Run:     missingRegulatoryPrintData, StoreKey: "-"})
}

// --- template versions --------------------------------------------------------

// PrintTemplateVersion is one saved version of a template.
type PrintTemplateVersion struct {
	TemplateID string    `json:"template_id"`
	Version    int       `json:"version"`
	Status     string    `json:"status"` // Active, Pending, Superseded, Rejected
	SavedBy    string    `json:"saved_by"`
	SavedAt    time.Time `json:"saved_at"`
	ApprovedBy string    `json:"approved_by,omitempty"`
	Note       string    `json:"note,omitempty"`
	Layout     string    `json:"layout,omitempty"`
}

// PreparePrintTemplateVersion runs on every save of a PrintTemplate: the
// saved layout becomes the next version - active at once, or (template
// approval on, an existing template) pending while the template keeps its
// active layout.
func PreparePrintTemplateVersion(tenantID, templateID, userID string, isCreate bool, prior, payload map[string]interface{}) error {
	layout, sent := payload["layout"]
	if !sent || templateID == "" {
		return nil
	}
	newLayout := fmt.Sprint(layout)
	if !isCreate && newLayout == strField(prior, "layout") {
		return nil
	}
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return err
	}
	var next int
	_ = db.DB.QueryRow(fmt.Sprintf(`SELECT COALESCE(MAX(version), 0) + 1 FROM %s.print_template_versions WHERE template_id = $1`, schema), templateID).Scan(&next)
	pending := !isCreate && GetSettingBool(tenantID, SettingTemplateApproval)
	status := "Active"
	if pending {
		status = "Pending"
		payload["layout"] = strField(prior, "layout")
		payload["pending_version"] = next
	} else {
		payload["version"] = next
		payload["pending_version"] = nil
		if _, err := db.DB.Exec(fmt.Sprintf(`UPDATE %s.print_template_versions SET status = 'Superseded' WHERE template_id = $1 AND status IN ('Active', 'Pending')`, schema), templateID); err != nil {
			return err
		}
	}
	_, err = db.DB.Exec(fmt.Sprintf(`INSERT INTO %s.print_template_versions (template_id, version, layout, status, saved_by, saved_at)
		VALUES ($1, $2, $3, $4, $5, $6) ON CONFLICT (template_id, version) DO NOTHING`, schema), templateID, next, newLayout, status, userID, time.Now().UTC())
	return err
}

// ListPrintTemplateVersions is a template's versions, newest first.
func ListPrintTemplateVersions(tenantID, templateID string) ([]PrintTemplateVersion, error) {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return nil, err
	}
	rows, err := db.DB.Query(fmt.Sprintf(`SELECT template_id, version, status, COALESCE(saved_by, ''), saved_at, COALESCE(approved_by, ''), COALESCE(note, '')
		FROM %s.print_template_versions WHERE template_id = $1 ORDER BY version DESC`, schema), templateID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []PrintTemplateVersion{}
	for rows.Next() {
		var v PrintTemplateVersion
		if err := rows.Scan(&v.TemplateID, &v.Version, &v.Status, &v.SavedBy, &v.SavedAt, &v.ApprovedBy, &v.Note); err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, rows.Err()
}

func templateVersionFail(msg string) error {
	return &ValidationError{Code: "GLOBAL-1319", SubFor: "template", Message: msg}
}

// DecidePrintTemplateVersion approves (it prints from now on) or rejects a
// pending version. The person who saved it cannot decide it.
func DecidePrintTemplateVersion(tenantID, templateID string, version int, approve bool, userID, note string) error {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return err
	}
	tx, err := db.DB.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var layout, status, savedBy string
	if err := tx.QueryRow(fmt.Sprintf(`SELECT layout, status, COALESCE(saved_by, '') FROM %s.print_template_versions WHERE template_id = $1 AND version = $2 FOR UPDATE`, schema),
		templateID, version).Scan(&layout, &status, &savedBy); err != nil {
		return templateVersionFail(fmt.Sprintf("version %d of this template was not found", version))
	}
	if status != "Pending" {
		return templateVersionFail(fmt.Sprintf("version %d is %s - only a change waiting for approval is decided", version, status))
	}
	if savedBy == userID {
		return templateVersionFail("the person who saved a change cannot approve it")
	}
	if !approve {
		if _, err := tx.Exec(fmt.Sprintf(`UPDATE %s.print_template_versions SET status = 'Rejected', approved_by = $3, note = $4 WHERE template_id = $1 AND version = $2`, schema),
			templateID, version, userID, note); err != nil {
			return err
		}
		if _, err := tx.Exec(fmt.Sprintf(`UPDATE %s.documents SET data = data - 'pending_version', updated_at = CURRENT_TIMESTAMP WHERE doctype = 'PrintTemplate' AND id = $1`, schema), templateID); err != nil {
			return err
		}
		return tx.Commit()
	}
	if _, err := tx.Exec(fmt.Sprintf(`UPDATE %s.print_template_versions SET status = 'Superseded' WHERE template_id = $1 AND status = 'Active'`, schema), templateID); err != nil {
		return err
	}
	if _, err := tx.Exec(fmt.Sprintf(`UPDATE %s.print_template_versions SET status = 'Active', approved_by = $3, note = $4 WHERE template_id = $1 AND version = $2`, schema),
		templateID, version, userID, note); err != nil {
		return err
	}
	patch, _ := json.Marshal(map[string]interface{}{"layout": layout, "version": version})
	if _, err := tx.Exec(fmt.Sprintf(`UPDATE %s.documents SET data = (data - 'pending_version') || $2::jsonb, updated_at = CURRENT_TIMESTAMP WHERE doctype = 'PrintTemplate' AND id = $1`, schema),
		templateID, string(patch)); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	LogAuditEvent(tenantID, userID, "APPROVE_PRINT_TEMPLATE", "SUCCESS", fmt.Sprintf("%s version %d approved", templateID, version))
	return nil
}

// RestorePrintTemplateVersion brings an old version back as a new one -
// active at once, or waiting for approval when changes need it.
func RestorePrintTemplateVersion(tenantID, templateID string, version int, userID string) (int, error) {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return 0, err
	}
	var layout string
	if err := db.DB.QueryRow(fmt.Sprintf(`SELECT layout FROM %s.print_template_versions WHERE template_id = $1 AND version = $2`, schema), templateID, version).Scan(&layout); err != nil {
		return 0, templateVersionFail(fmt.Sprintf("version %d of this template was not found", version))
	}
	prior, _, err := fetchDocData(tenantID, "PrintTemplate", templateID)
	if err != nil || prior == nil {
		return 0, templateVersionFail("no such print template")
	}
	payload := map[string]interface{}{"layout": layout}
	if err := PreparePrintTemplateVersion(tenantID, templateID, userID, false, prior, payload); err != nil {
		return 0, err
	}
	n := int(numFromInterface(firstNonNil(payload["version"], payload["pending_version"])))
	if err := mergeDocData(schema, "PrintTemplate", templateID, payload); err != nil {
		return 0, err
	}
	_, _ = db.DB.Exec(fmt.Sprintf(`UPDATE %s.print_template_versions SET note = $3 WHERE template_id = $1 AND version = $2`, schema), templateID, n, fmt.Sprintf("restored from version %d", version))
	LogAuditEvent(tenantID, userID, "RESTORE_PRINT_TEMPLATE", "SUCCESS", fmt.Sprintf("%s version %d restored as version %d", templateID, version, n))
	return n, nil
}

func firstNonNil(vals ...interface{}) interface{} {
	for _, v := range vals {
		if v != nil {
			return v
		}
	}
	return nil
}

// templateVersionLabel is "<id> v<version>" for the print log.
func templateVersionLabel(tenantID, templateID string) string {
	if templateID == "" {
		return ""
	}
	data, _, err := fetchDocData(tenantID, "PrintTemplate", templateID)
	if err != nil || data == nil || numFromInterface(data["version"]) <= 0 {
		return templateID
	}
	return fmt.Sprintf("%s v%d", templateID, int(numFromInterface(data["version"])))
}

// --- profile precedence -------------------------------------------------------

// printChannel is the sales channel a print belongs to.
func printChannel(txn string) string {
	switch txn {
	case PrintTxnPOSTaxInvoice:
		return "POS"
	case PrintTxnOMSBill, PrintTxnPickList:
		return "Online"
	case PrintTxnSalesInvoice:
		return "B2B"
	}
	return "ERP"
}

// profileScore is how specifically a profile fits a print: document 8,
// store 4, legal entity 2, channel 1; -1 when it names something else.
func profileScore(data map[string]interface{}, txn, doctype, store, entity, channel string) int {
	score := 0
	switch t := strings.TrimSpace(strField(data, "transaction")); {
	case t == "" || strings.EqualFold(t, "All"):
	case t == txn || t == doctype:
		score += 8
	default:
		return -1
	}
	switch s := strings.TrimSpace(strField(data, "store")); {
	case s == "":
	case s == store:
		score += 4
	default:
		return -1
	}
	switch e := strings.TrimSpace(strField(data, "legal_entity")); {
	case e == "":
	case e == entity:
		score += 2
	default:
		return -1
	}
	switch c := strings.TrimSpace(strField(data, "channel")); {
	case c == "" || strings.EqualFold(c, "All"):
	case strings.EqualFold(c, channel):
		score++
	default:
		return -1
	}
	return score
}

// --- tax documents and the GSTIN (DEC-20) -------------------------------------

// applyTaxDocumentRule refuses a tax document from a store with no GSTIN, or
// titles it a Bill of Supply for a business with no GSTIN at all - when the
// client asks for it.
func (d *PrintDocument) applyTaxDocumentRule(tenantID, txn string) error {
	if !taxDocumentTxns[txn] || strings.TrimSpace(d.Store.GSTIN) != "" || !GetSettingBool(tenantID, SettingTaxDocumentNeedsGSTIN) {
		return nil
	}
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return err
	}
	var registered bool
	_ = db.DB.QueryRow(fmt.Sprintf(`SELECT EXISTS (SELECT 1 FROM %s.documents WHERE doctype = 'LegalEntity' AND deleted_at IS NULL AND COALESCE(data->>'gstin', '') <> '')`, schema)).Scan(&registered)
	if !registered {
		if txn == PrintTxnSalesInvoice || txn == PrintTxnOMSBill || txn == PrintTxnPOSTaxInvoice {
			d.Title = "Bill of Supply"
			d.Fields["unregistered_note"] = "Supplier not registered under GST - no tax charged"
		}
		return nil
	}
	return &ValidationError{Code: "GLOBAL-1319", SubFor: "GSTIN", Message: fmt.Sprintf(
		"%s cannot be printed: store %s has no GSTIN on its legal entity - add it under Setup > Legal Entities", d.Title, firstNonEmpty(d.Store.Name, d.Store.Code))}
}

// --- the till receipt ----------------------------------------------------------

// receiptTenders is every tender of a paid sale, from its allocation.
func receiptTenders(data map[string]interface{}) ([]map[string]interface{}, int) {
	var lines []SaleLineAllocation
	switch a := data["allocation"].(type) {
	case string:
		_ = json.Unmarshal([]byte(a), &lines)
	default:
		raw, _ := json.Marshal(a)
		_ = json.Unmarshal(raw, &lines)
	}
	by := map[string]int64{}
	points := 0
	for _, l := range lines {
		for k, v := range l.Tenders {
			by[k] += v
		}
		points += l.Points
	}
	var out []map[string]interface{}
	for k, v := range by {
		if v != 0 {
			out = append(out, map[string]interface{}{"mode": k, "amount": PaiseToRupees(v)})
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i]["amount"].(float64) > out[j]["amount"].(float64) })
	return out, points
}

// --- reports ------------------------------------------------------------------

func printTemplateCoverage(tenantID string, _ map[string]string) ([]map[string]interface{}, error) {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return nil, err
	}
	templates, defaults, profiles, labels, prints, pending := map[string]int{}, map[string]string{}, map[string]int{}, map[string]string{}, map[string]int{}, map[string]int{}
	rows, err := db.DB.Query(fmt.Sprintf(`SELECT doctype, id, data FROM %s.documents WHERE doctype IN ('PrintTemplate', 'PrintProfile') AND deleted_at IS NULL`, schema))
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var doctype, id, raw string
		if rows.Scan(&doctype, &id, &raw) != nil {
			continue
		}
		var data map[string]interface{}
		_ = json.Unmarshal([]byte(raw), &data)
		t := firstNonEmpty(strField(data, "transaction"), "All")
		if doctype == "PrintTemplate" {
			templates[t]++
			if truthy(data["is_default"]) {
				defaults[t] = firstNonEmpty(strField(data, "name"), id)
			}
			if data["pending_version"] != nil {
				pending[t]++
			}
		} else {
			profiles[t]++
			if l := strField(data, "copy_labels"); l != "" {
				labels[t] = l
			}
		}
	}
	rows.Close()
	r2, err := db.DB.Query(fmt.Sprintf(`SELECT COALESCE(txn, ''), COUNT(*) FROM %s.print_job_log WHERE doctype IS NOT NULL AND printed_at > NOW() - INTERVAL '30 days' GROUP BY 1`, schema))
	if err == nil {
		for r2.Next() {
			var t string
			var n int
			if r2.Scan(&t, &n) == nil {
				prints[t] = n
			}
		}
		r2.Close()
	}
	var out []map[string]interface{}
	for _, t := range PrintTransactions {
		row := map[string]interface{}{"transaction": t.Label, "doctype": t.Doctype, "templates": templates[t.Key] + templates["All"],
			"default_template": firstNonEmpty(defaults[t.Key], "Standard layout (built-in)"), "profiles": profiles[t.Key] + profiles["All"],
			"copy_labels": firstNonEmpty(labels[t.Key], labels["All"]), "prints_30d": prints[t.Key], "pending_versions": pending[t.Key]}
		if t.Key == PrintTxnDeliveryChallan && row["copy_labels"] == "" {
			row["copy_labels"] = "Original / Duplicate / Triplicate (Rule 55, built in)"
		}
		out = append(out, row)
	}
	return out, nil
}

func printFailures(tenantID string, p map[string]string) ([]map[string]interface{}, error) {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return nil, err
	}
	rows, err := db.DB.Query(fmt.Sprintf(`SELECT printed_at, COALESCE(txn, job_type), COALESCE(doc_number, document_ref, ''), COALESCE(qz_printer_name, printer_code, ''),
		COALESCE(outcome, status), COALESCE(error_detail, ''), COALESCE(printed_by, '')
		FROM %s.print_job_log WHERE (status IN ('Failed', 'Error') OR outcome IN ('Unknown', 'Failed') OR COALESCE(error_detail, '') <> '')
		  AND ($1 = '' OR printed_at >= $1::date) AND ($2 = '' OR printed_at < $2::date + 1)
		ORDER BY printed_at DESC LIMIT 2000`, schema), strings.TrimSpace(p["from"]), strings.TrimSpace(p["to"]))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []map[string]interface{}
	for rows.Next() {
		var at time.Time
		var job, doc, printer, outcome, detail, by string
		if rows.Scan(&at, &job, &doc, &printer, &outcome, &detail, &by) != nil {
			continue
		}
		if outcome == PrintOutcomeUnknown && detail == "" {
			detail = "printed without knowing whether it reached a printer (an older page or a print the browser could not report)"
		}
		out = append(out, map[string]interface{}{"printed_at": at.Format("2006-01-02 15:04"), "job": job, "document": doc, "printer": printer,
			"outcome": outcome, "detail": detail, "printed_by": by})
	}
	return out, rows.Err()
}

func missingRegulatoryPrintData(tenantID string, _ map[string]string) ([]map[string]interface{}, error) {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return nil, err
	}
	var out []map[string]interface{}
	add := func(record, code, name, missing, why string) {
		out = append(out, map[string]interface{}{"record": record, "code": code, "name": name, "missing": missing, "why": why})
	}
	scan := func(q string, each func(id string, data map[string]interface{})) error {
		rows, err := db.DB.Query(fmt.Sprintf(q, schema))
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var id, raw string
			if rows.Scan(&id, &raw) != nil {
				continue
			}
			var data map[string]interface{}
			_ = json.Unmarshal([]byte(raw), &data)
			each(id, data)
		}
		return rows.Err()
	}
	if err := scan(`SELECT id, data FROM %s.documents WHERE doctype = 'LegalEntity' AND deleted_at IS NULL`, func(id string, d map[string]interface{}) {
		var miss []string
		for _, f := range []struct{ k, l string }{{"gstin", "GSTIN"}, {"address", "registered address"}, {"state_code", "state code"}, {"pincode", "PIN code"}} {
			if strField(d, f.k) == "" {
				miss = append(miss, f.l)
			}
		}
		if len(miss) > 0 {
			add("Legal entity", firstNonEmpty(strField(d, "code"), id), strField(d, "name"), strings.Join(miss, ", "), "printed as the seller on every tax invoice, note and challan")
		}
	}); err != nil {
		return nil, err
	}
	if err := scan(`SELECT id, data FROM %s.documents WHERE doctype = 'Location' AND deleted_at IS NULL AND COALESCE(data->>'status', 'Active') <> 'Inactive'`, func(id string, d map[string]interface{}) {
		if strField(d, "legal_entity") == "" {
			add("Store", firstNonEmpty(strField(d, "code"), id), strField(d, "name"), "legal entity", "its prints carry no seller name, GSTIN or state")
		}
	}); err != nil {
		return nil, err
	}
	if err := scan(`SELECT id, data FROM %s.documents WHERE doctype = 'Item' AND deleted_at IS NULL AND status = 'Active' AND COALESCE(data->>'hsn_code', '') = '' LIMIT 500`, func(id string, d map[string]interface{}) {
		add("Item", firstNonEmpty(strField(d, "code"), id), strField(d, "name"), "HSN code", "HSN is printed per line on tax invoices and challans")
	}); err != nil {
		return nil, err
	}
	return out, nil
}
