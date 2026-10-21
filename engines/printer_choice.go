package engines

import (
	"custom_erp/db"
	"encoding/json"
	"fmt"
	"strings"
)

// Stage 98.4 (user, 2026-10-19): "ERP will be installed in 100 stores. All
// printer will be different. Need auto call to those printer ... It should ask
// one time which printer for what txn once then nver ask. But user can change
// it."
//
// Each computer keeps its own printer for each kind of print (decision D3),
// on the server: computer (the name QZ Tray reports) + kind -> a Printer
// record. The first print of a kind on a computer picks the printer by itself
// when the choice is clear, or asks once (public/print-routing.js), and saves
// it here; every later print of that kind goes straight to it.

// PrintKinds are the kinds of print a computer chooses a printer for.
var PrintKinds = []string{"Purchase Order", "Goods Receipt", "Transfer", "Sales Invoice", "Returns and Notes", "Receipt", "Sticker", "Shipping Label", "Pick List", "Other Documents"}

// PrintKindForJob is the kind of print a QZ job is: the caller's kind, else
// its job type. The legacy "Invoice" job is the Sales Invoice; a "Document"
// with no kind is a marketplace label, as it always was.
func PrintKindForJob(jobType, kind string) string {
	if k := strings.TrimSpace(kind); k != "" {
		return k
	}
	switch strings.TrimSpace(jobType) {
	case "Invoice":
		return "Sales Invoice"
	case "Document":
		return "Shipping Label"
	}
	return strings.TrimSpace(jobType)
}

// PrintRolesForKind are the Printer "Default For" values that serve a kind -
// the kind itself, and the older role name it had before 98.4.
func PrintRolesForKind(kind string) []string {
	if kind == "Sales Invoice" {
		return []string{"Sales Invoice", "Invoice"}
	}
	return []string{kind}
}

// PrinterChoice is one computer's printer for one kind of print.
type PrinterChoice struct {
	ID       string    `json:"id"`
	Computer string    `json:"computer"`
	Kind     string    `json:"print_kind"`
	Location string    `json:"location"`
	Printer  QZPrinter `json:"printer"`
}

// PrinterChoices lists a computer's choices ("" lists every computer's).
func PrinterChoices(tenantID, computer string) ([]PrinterChoice, error) {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return nil, err
	}
	printers, err := ListQZPrinters(tenantID)
	if err != nil {
		return nil, err
	}
	rows, err := db.DB.Query(fmt.Sprintf(`
		SELECT id, COALESCE(data->>'computer', ''), COALESCE(data->>'print_kind', ''),
		       COALESCE(data->>'location', ''), COALESCE(data->>'printer', '')
		  FROM %s.documents
		 WHERE doctype = 'PrinterAssignment' AND deleted_at IS NULL
		   AND COALESCE(data->>'status', 'Active') = 'Active'
		   AND ($1 = '' OR lower(data->>'computer') = lower($1))
		 ORDER BY data->>'computer', data->>'print_kind'`, schema), strings.TrimSpace(computer))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []PrinterChoice{}
	for rows.Next() {
		var c PrinterChoice
		var printerRef string
		if err := rows.Scan(&c.ID, &c.Computer, &c.Kind, &c.Location, &printerRef); err != nil {
			return nil, err
		}
		for _, p := range printers {
			if p.ID == printerRef || p.Code == printerRef {
				c.Printer = p
			}
		}
		if c.Printer.ID != "" { // an assignment to a deleted or inactive printer is no choice
			out = append(out, c)
		}
	}
	return out, rows.Err()
}

// ChosenPrinter is the computer's printer for a kind of print.
func ChosenPrinter(tenantID, computer, kind string) (QZPrinter, bool) {
	if strings.TrimSpace(computer) == "" || kind == "" {
		return QZPrinter{}, false
	}
	choices, err := PrinterChoices(tenantID, computer)
	if err != nil {
		return QZPrinter{}, false
	}
	for _, c := range choices {
		if c.Kind == kind {
			return c.Printer, true
		}
	}
	return QZPrinter{}, false
}

// ChoosePrinter records that a computer prints a kind on an OS printer. The
// Printer record for that OS printer at the store is reused, or made with the
// language the page guessed (editable on the record) and no "Default For": a
// printer one computer chose must not become every other computer's store
// default - that stays an admin's deliberate setting.
func ChoosePrinter(tenantID, userID, computer, location, kind, osPrinter, language string) (QZPrinter, error) {
	computer, location, osPrinter = strings.TrimSpace(computer), strings.TrimSpace(location), strings.TrimSpace(osPrinter)
	if computer == "" || osPrinter == "" {
		return QZPrinter{}, &ValidationError{Code: "GLOBAL-0002", SubFor: "Printer", Message: "the computer and the printer are both needed to remember a choice"}
	}
	if !containsString(PrintKinds, kind) {
		return QZPrinter{}, &ValidationError{Code: "GLOBAL-0002", SubFor: "Kind of print", Message: fmt.Sprintf("%q is not a kind of print - expected one of %s", kind, strings.Join(PrintKinds, ", "))}
	}
	switch language {
	case "PDF", "ZPL", "TSPL", "ESC-POS", "HTML":
	default:
		language = "PDF"
	}
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return QZPrinter{}, err
	}
	printers, err := ListQZPrinters(tenantID)
	if err != nil {
		return QZPrinter{}, err
	}
	var printer QZPrinter
	for _, p := range printers {
		if strings.EqualFold(p.QZPrinterName, osPrinter) && (p.Location == location || p.Location == "") {
			printer = p
			if p.Location == location {
				break
			}
		}
	}
	if printer.ID == "" {
		id := NewDocID("PRN")
		data, _ := json.Marshal(map[string]interface{}{"code": id, "name": osPrinter, "location": location, "status": "Active",
			"qz_printer_name": osPrinter, "print_role": "", "printer_language": language})
		if _, err := db.DB.Exec(fmt.Sprintf(`INSERT INTO %s.documents (id, doctype, data, status, created_by) VALUES ($1, 'Printer', $2::jsonb, 'Active', $3)`, schema),
			id, string(data), userID); err != nil {
			return QZPrinter{}, err
		}
		printer = QZPrinter{ID: id, Code: id, Name: osPrinter, QZPrinterName: osPrinter, Language: language, Location: location}
	}

	data, _ := json.Marshal(map[string]interface{}{"computer": computer, "print_kind": kind, "printer": printer.ID, "location": location, "status": "Active"})
	res, err := db.DB.Exec(fmt.Sprintf(`UPDATE %s.documents SET data = $1::jsonb, updated_at = NOW()
		 WHERE doctype = 'PrinterAssignment' AND deleted_at IS NULL AND lower(data->>'computer') = lower($2) AND data->>'print_kind' = $3`, schema),
		string(data), computer, kind)
	if err != nil {
		return QZPrinter{}, err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		if _, err := db.DB.Exec(fmt.Sprintf(`INSERT INTO %s.documents (id, doctype, data, status, created_by) VALUES ($1, 'PrinterAssignment', $2::jsonb, 'Active', $3)`, schema),
			NewDocID("PRA"), string(data), userID); err != nil {
			return QZPrinter{}, err
		}
	}
	LogAuditEvent(tenantID, userID, "PRINTER_CHOSEN", "SUCCESS", fmt.Sprintf("computer=%s kind=%s printer=%s (%s) store=%s", computer, kind, printer.Code, osPrinter, location))
	return printer, nil
}
