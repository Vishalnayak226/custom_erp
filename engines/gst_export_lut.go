package engines

import (
	"custom_erp/db"
	"fmt"
	"strings"
	"time"
)

// Ask 170.37: exports under LUT. A LutRegister row is one Letter of
// Undertaking (LUT / ARN number, financial year, valid from / to). A Sales
// Invoice names its export type, shipping bill, port and destination. Behind
// finance.require_lut_for_export (off) an invoice "Export under LUT" is
// refused when no LUT is valid on its date. With the setting off nothing is
// checked and no existing invoice changes.

const (
	LutRegisterDoctype         = "LutRegister"
	SettingRequireLUTForExport = "finance.require_lut_for_export"

	ExportTypeLUT     = "Export under LUT"
	ExportTypeWithPay = "Export with payment of tax"

	lutMissingCode = "TAXGST-1720" // export under LUT with no valid LUT on its date
	lutInvalidCode = "TAXGST-1721" // LUT register row not valid
)

func init() {
	RegisterSetting(SettingDefinition{
		Key: SettingRequireLUTForExport, Module: "Finance",
		Label: "Refuse an export under LUT without a valid LUT", Type: SettingTypeBool, Default: "false",
		Description: "Ask 170.37: an invoice with export type \"Export under LUT\" is refused when the LUT register has no active LUT covering the invoice date. Off: the register is only a record.",
	})
}

// validLUTOn returns the LUT / ARN number of an active LUT covering date
// (yyyy-mm-dd), "" when there is none.
func validLUTOn(tenantID, date string) string {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return ""
	}
	var number string
	_ = db.DB.QueryRow(fmt.Sprintf(`SELECT COALESCE(NULLIF(data->>'lut_number', ''), id) FROM %s.documents
		WHERE doctype = $1 AND deleted_at IS NULL AND COALESCE(status, '') <> 'Withdrawn'
		  AND data->>'valid_from' <= $2 AND data->>'valid_to' >= $2
		ORDER BY data->>'valid_to' DESC LIMIT 1`, schema), LutRegisterDoctype, date).Scan(&number)
	return number
}

// validateExportInvoice is the save-time check on a Sales Invoice.
func validateExportInvoice(tenantID string, data map[string]interface{}) error {
	if !strings.EqualFold(strings.TrimSpace(strField(data, "export_type")), ExportTypeLUT) {
		return nil
	}
	if !GetSettingBool(tenantID, SettingRequireLUTForExport) {
		return nil
	}
	date := firstNonEmpty(strField(data, "invoice_date"), BusinessToday())
	if len(date) > 10 {
		date = date[:10]
	}
	if validLUTOn(tenantID, date) == "" {
		return &ValidationError{Code: lutMissingCode, Message: fmt.Sprintf("no valid LUT on %s", date)}
	}
	return nil
}

// validateLutRegister keeps a LUT's dates in order.
func validateLutRegister(data map[string]interface{}) error {
	from, to := strField(data, "valid_from"), strField(data, "valid_to")
	f, e1 := time.Parse("2006-01-02", from)
	t, e2 := time.Parse("2006-01-02", to)
	if e1 != nil || e2 != nil || t.Before(f) {
		return &ValidationError{Code: lutInvalidCode, Message: "valid to must be on or after valid from"}
	}
	return nil
}

// exportPrintFields fills the lines a tax invoice prints for an export.
func exportPrintFields(tenantID string, data map[string]interface{}, fields map[string]interface{}) {
	typ := strings.TrimSpace(strField(data, "export_type"))
	if typ == "" || strings.EqualFold(typ, "None") {
		return
	}
	date := firstNonEmpty(strField(data, "invoice_date"), BusinessToday())
	if len(date) > 10 {
		date = date[:10]
	}
	if strings.EqualFold(typ, ExportTypeLUT) {
		lut := validLUTOn(tenantID, date)
		fields["export_declaration"] = strings.TrimSpace("Supply meant for export under LUT " + lut + " without payment of IGST")
	} else {
		fields["export_declaration"] = "Supply meant for export on payment of IGST"
	}
	var parts []string
	if v := strField(data, "shipping_bill_no"); v != "" {
		s := "Shipping bill " + v
		if d := strField(data, "shipping_bill_date"); d != "" {
			s += " dt " + gstr1ISO(d)
		}
		parts = append(parts, s)
	}
	if v := strField(data, "port_code"); v != "" {
		parts = append(parts, "Port "+v)
	}
	if v := strField(data, "destination_country"); v != "" {
		parts = append(parts, "Destination "+v)
	}
	fields["export_details"] = strings.Join(parts, ", ")
}
