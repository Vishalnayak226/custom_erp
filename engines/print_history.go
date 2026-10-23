package engines

import (
	"custom_erp/db"
	"fmt"
	"strings"
	"time"
)

// Stage 127.6: every document print is recorded - Print, PDF, Send or
// Reprint - in the print job log, so Setup > Printing > Reprint lists what
// was printed and prints it again. Which rows a person sees (the client's
// modules, their own read permission) is the handler's filter.

// Document print actions.
const (
	PrintActionPrint   = "Print"
	PrintActionPDF     = "PDF"
	PrintActionSend    = "Send"
	PrintActionReprint = "Reprint"
)

// DocumentPrint is one document print.
type DocumentPrint struct {
	ID            int       `json:"id"`
	Txn           string    `json:"txn"`
	Doctype       string    `json:"doctype"`
	DocID         string    `json:"doc_id"`
	Number        string    `json:"number"`
	Title         string    `json:"title"`
	Store         string    `json:"store"`
	Action        string    `json:"action"`
	ReprintReason string    `json:"reprint_reason,omitempty"`
	Printer       string    `json:"printer,omitempty"`
	PrintedBy     string    `json:"printed_by"`
	PrintedAt     time.Time `json:"printed_at"`
	// Stage 130.16 (74.1.4): the issued-output log.
	Revision   int    `json:"revision,omitempty"`
	Reprinted  bool   `json:"reprinted"`
	Copies     int    `json:"copies,omitempty"`
	CopyLabels string `json:"copy_labels,omitempty"`
	TemplateID string `json:"template_id,omitempty"`
	ReasonCode string `json:"reason_code,omitempty"`
	ApprovalID string `json:"approval_id,omitempty"`
	ApprovedBy string `json:"approved_by,omitempty"`
	Checksum   string `json:"checksum,omitempty"`
	Outcome    string `json:"outcome,omitempty"`
	Channel    string `json:"-"` // qz or browser, as the page printed it
	MayReprint bool   `json:"-"` // the handler's DocumentReprint permission
	CheckOnly  bool   `json:"-"` // check reprint control only: nothing is frozen or logged
}

// RecordDocumentPrint writes one document print. A reprint needs its reason.
func RecordDocumentPrint(tenantID, printedBy string, p DocumentPrint) error {
	switch p.Action {
	case PrintActionPrint, PrintActionPDF, PrintActionSend:
	case PrintActionReprint:
		if strings.TrimSpace(p.ReprintReason) == "" {
			return &ValidationError{Code: "GLOBAL-0002", SubFor: "Reprint reason", Message: "a reprint needs its reason"}
		}
	default:
		return &ValidationError{Code: "GLOBAL-0002", SubFor: "Action", Message: fmt.Sprintf("%q is not a print action (Print, PDF, Send or Reprint)", p.Action)}
	}
	if err := recordIssuedPrint(tenantID, printedBy, &p); err != nil || p.CheckOnly {
		return err
	}
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return err
	}
	if p.Copies < 1 {
		p.Copies = 1
	}
	clip := func(s string, n int) string {
		s = strings.TrimSpace(s)
		if len(s) > n {
			return s[:n]
		}
		return s
	}
	_, err = db.DB.Exec(fmt.Sprintf(`
		INSERT INTO %s.print_job_log
		  (job_type, document_ref, qz_printer_name, copies, printed_by, status, txn, doctype, doc_id, doc_number, doc_title, store, print_action, reprint_reason,
		   doc_revision, is_reprint, template_id, copy_labels, reason_code, approval_id, approved_by, payload_checksum, requested_count, outcome)
		VALUES ('Document', $1, NULLIF($2, ''), $12, $3, 'Submitted', $4, $5, $6, $7, $8, NULLIF($9, ''), $10, NULLIF($11, ''),
		   NULLIF($13, 0), $14, NULLIF($15, ''), NULLIF($16, ''), NULLIF($17, ''), NULLIF($18, ''), NULLIF($19, ''), NULLIF($20, ''), $12, $21)`, schema),
		clip(firstNonEmpty(p.Number, p.DocID), 200), clip(p.Printer, 255), clip(printedBy, 200), clip(p.Txn, 100), clip(p.Doctype, 100),
		clip(p.DocID, 200), clip(p.Number, 200), clip(p.Title, 200), clip(p.Store, 100), p.Action, clip(p.ReprintReason, 500),
		p.Copies, p.Revision, p.Reprinted, clip(p.TemplateID, 100), clip(p.CopyLabels, 300), clip(p.ReasonCode, 40), clip(p.ApprovalID, 100),
		clip(p.ApprovedBy, 200), p.Checksum, p.Outcome)
	return err
}

// DocumentPrintFilter narrows the history: one doctype, a number or text.
type DocumentPrintFilter struct {
	Doctype string
	Query   string
	Limit   int
}

// ListDocumentPrints returns document prints, newest first.
func ListDocumentPrints(tenantID string, f DocumentPrintFilter) ([]DocumentPrint, error) {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return nil, err
	}
	if f.Limit <= 0 || f.Limit > 500 {
		f.Limit = 300
	}
	q := "%" + strings.ToLower(strings.TrimSpace(f.Query)) + "%"
	rows, err := db.DB.Query(fmt.Sprintf(`
		SELECT id, COALESCE(txn, ''), doctype, COALESCE(doc_id, ''), COALESCE(doc_number, ''), COALESCE(doc_title, ''),
		       COALESCE(store, ''), COALESCE(print_action, ''), COALESCE(reprint_reason, ''), COALESCE(qz_printer_name, ''),
		       COALESCE(printed_by, ''), printed_at, COALESCE(doc_revision, 0), is_reprint, copies, COALESCE(copy_labels, ''),
		       COALESCE(approved_by, ''), COALESCE(payload_checksum, ''), COALESCE(outcome, '')
		  FROM %s.print_job_log
		 WHERE doctype IS NOT NULL AND ($1 = '' OR doctype = $1)
		   AND ($2 = '%%%%' OR LOWER(COALESCE(doc_number, '') || ' ' || COALESCE(doc_id, '') || ' ' || COALESCE(doc_title, '') || ' ' || COALESCE(printed_by, '')) LIKE $2)
		 ORDER BY printed_at DESC, id DESC
		 LIMIT $3`, schema), strings.TrimSpace(f.Doctype), q, f.Limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []DocumentPrint{}
	for rows.Next() {
		var p DocumentPrint
		if err := rows.Scan(&p.ID, &p.Txn, &p.Doctype, &p.DocID, &p.Number, &p.Title, &p.Store, &p.Action, &p.ReprintReason,
			&p.Printer, &p.PrintedBy, &p.PrintedAt, &p.Revision, &p.Reprinted, &p.Copies, &p.CopyLabels, &p.ApprovedBy, &p.Checksum, &p.Outcome); err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

// EnabledModuleDoctypes is the doctypes a client's modules include - exactly
// what the menus show (handleGetDocTypes' rule): those whose module is enabled.
func EnabledModuleDoctypes(tenantID string) (map[string]bool, error) {
	list, err := GetDocTypes(tenantID)
	if err != nil {
		return nil, err
	}
	entitlements, err := ListModuleEntitlements(tenantID)
	if err != nil {
		return nil, err
	}
	enabled := map[string]bool{}
	for _, m := range entitlements {
		enabled[m.ModuleKey] = m.Enabled
	}
	out := map[string]bool{}
	for _, d := range list {
		name, _ := d["name"].(string)
		key, _ := d["module_key"].(string)
		if enabled[key] {
			out[name] = true
		}
	}
	return out, nil
}
