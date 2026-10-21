package engines

import (
	"custom_erp/db"
	"encoding/json"
	"fmt"
)

// Stage 127.3.1 (user, 2026-10-21: "see the std practice and bring those
// also"): a print shape gives a transaction its standard print - title and
// lines - where the generic print would show it poorly because it keeps its
// lines some other way: a journal voucher's accounts in paise, a payslip's pay
// heads as header fields, a job-work challan's goods sent and to come back, a
// purchase bill's payments, a load's cartons. Everything else about the print
// (header rows, party, store, totals of quantity and amount columns, layout)
// is the generic print's.
type printShape struct {
	title      string
	partyTitle string
	hide       []string // header fields the lines already show
	columns    []genericPrintColumn
	lines      func(tenantID, id string, data map[string]interface{}) []map[string]interface{}
}

var printShapes = map[string]printShape{
	"JournalVoucher": {title: "Journal Voucher", lines: jvPrintLines, columns: []genericPrintColumn{
		{Key: "account_code", Label: "Account"}, {Key: "account_name", Label: "Account name"}, {Key: "debit", Label: "Debit"}, {Key: "credit", Label: "Credit"}}},
	"Payslip": {title: "Payslip", partyTitle: "Employee", hide: []string{"gross_pay", "pf_deduction", "esi_deduction", "pt_deduction", "tds_deduction", "loan_deduction"}, lines: payslipPrintLines, columns: []genericPrintColumn{
		{Key: "component", Label: "Pay head"}, {Key: "earning", Label: "Earnings"}, {Key: "deduction", Label: "Deductions"}}},
	"SubcontractOrder": {title: "Job Work Challan (Rule 55)", partyTitle: "Job worker", lines: jobWorkPrintLines, columns: []genericPrintColumn{
		{Key: "direction", Label: "Goods"}, {Key: "sku", Label: "SKU"}, {Key: "item_name", Label: "Item"}, {Key: "qty", Label: "Qty"}}},
	"VendorInvoice": {title: "Purchase Bill", lines: vendorBillPaymentLines, columns: []genericPrintColumn{
		{Key: "date", Label: "Paid on"}, {Key: "kind", Label: "How"}, {Key: "reference", Label: "Reference"}, {Key: "amount", Label: "Amount"}, {Key: "by", Label: "By"}}},
	"LoadingTask": {title: "Bill of Lading", lines: bolPrintLines, columns: []genericPrintColumn{
		{Key: "package_code", Label: "Package"}, {Key: "order_id", Label: "Order"}, {Key: "weight_kg", Label: "Weight (kg)"}}},
}

// jvPrintLines: each account by its name, debit and credit in rupees (the
// voucher keeps them in paise).
func jvPrintLines(tenantID, _ string, data map[string]interface{}) []map[string]interface{} {
	var lines []JournalVoucherLine
	raw, _ := data["lines"].(string)
	if json.Unmarshal([]byte(raw), &lines) != nil {
		return nil
	}
	schema, _ := db.GetTenantSchema(tenantID)
	out := make([]map[string]interface{}, 0, len(lines))
	for _, l := range lines {
		name := ""
		if schema != "" {
			_ = db.DB.QueryRow(fmt.Sprintf(`SELECT COALESCE(account_name, '') FROM %s.gl_accounts WHERE account_code = $1`, schema), l.AccountCode).Scan(&name)
		}
		row := map[string]interface{}{"account_code": l.AccountCode, "account_name": name}
		if l.Debit > 0 {
			row["debit"] = PaiseToRupees(int64(l.Debit))
		}
		if l.Credit > 0 {
			row["credit"] = PaiseToRupees(int64(l.Credit))
		}
		out = append(out, row)
	}
	return out
}

// payslipPrintLines: gross pay as earnings, each deduction the slip carries.
func payslipPrintLines(_, _ string, data map[string]interface{}) []map[string]interface{} {
	out := []map[string]interface{}{{"component": "Gross pay", "earning": round2(numFromInterface(data["gross_pay"]))}}
	for _, d := range []struct{ key, label string }{
		{"pf_deduction", "Provident Fund"}, {"esi_deduction", "ESI"}, {"pt_deduction", "Professional Tax"},
		{"tds_deduction", "TDS"}, {"loan_deduction", "Loan recovery"},
	} {
		if v := numFromInterface(data[d.key]); v != 0 {
			out = append(out, map[string]interface{}{"component": d.label, "deduction": round2(v)})
		}
	}
	return out
}

// jobWorkPrintLines: the goods sent to the job worker and what comes back
// (Rule 55 challan), each item by its name.
func jobWorkPrintLines(tenantID, _ string, data map[string]interface{}) []map[string]interface{} {
	name := func(sku string) string {
		if item, _, err := fetchDocData(tenantID, "Item", sku); err == nil {
			return firstNonEmpty(strField(item, "name"), strField(item, "item_name"))
		}
		return ""
	}
	var out []map[string]interface{}
	add := func(direction, sku string, qty float64) {
		if sku != "" && qty > 0 {
			out = append(out, map[string]interface{}{"direction": direction, "sku": sku, "item_name": name(sku), "qty": qty})
		}
	}
	add("Sent", strField(data, "sent_item_id"), numFromInterface(data["sent_qty"]))
	add("To come back", strField(data, "received_item_id"), numFromInterface(data["expected_received_qty"]))
	add("Received back", strField(data, "received_item_id"), numFromInterface(data["actual_received_qty"]))
	return out
}

// vendorBillPaymentLines: the bill's payments, as its payment history keeps
// them.
func vendorBillPaymentLines(_, _ string, data map[string]interface{}) []map[string]interface{} {
	entries, err := readSettlementEntries(data, "payments")
	if err != nil {
		return nil
	}
	out := make([]map[string]interface{}, 0, len(entries))
	for _, e := range entries {
		out = append(out, map[string]interface{}{"date": e.Date, "kind": e.Kind, "reference": e.Reference, "amount": round2(e.Amount), "by": e.By})
	}
	return out
}

// bolPrintLines: the cartons loaded on the trailer.
func bolPrintLines(tenantID, id string, _ map[string]interface{}) []map[string]interface{} {
	cartons, err := BillOfLading(tenantID, id)
	if err != nil {
		return nil
	}
	out := make([]map[string]interface{}, 0, len(cartons))
	for _, c := range cartons {
		out = append(out, map[string]interface{}{"package_code": c.PackageCode, "order_id": c.OrderID, "weight_kg": c.WeightKg})
	}
	return out
}
