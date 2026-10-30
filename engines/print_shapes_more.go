package engines

import (
	"custom_erp/db"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"
)

// Stage 130.17 (127.3.2): the standard prints still on the generic layout.
//
//   - Shapes (lines and title on the generic print): payment advice (a
//     payment proposal's bills), material issue slip (a production order's
//     BOM, and 130.15's material issue), count sheet (a physical inventory's
//     count lines, blind), purchase requisition, request for quotation, and
//     the job work order on its 74.3.3 lines.
//   - Prints with no document of their own: a receipt voucher per receipt on
//     a sales invoice and a payment voucher per payment on a vendor bill (the
//     id is "<invoice id>~<settlement reference>"), and a customer / vendor
//     statement of account (the id is the customer or vendor).

const (
	PrintTxnReceiptVoucher    = "ReceiptVoucher"
	PrintTxnPaymentVoucher    = "PaymentVoucher"
	PrintTxnCustomerStatement = "CustomerStatement"
	PrintTxnVendorStatement   = "VendorStatement"
)

func init() {
	PrintTransactions = append(PrintTransactions,
		struct{ Key, Label, Doctype string }{PrintTxnReceiptVoucher, "Receipt voucher (one receipt on a sales invoice)", "SalesInvoice"},
		struct{ Key, Label, Doctype string }{PrintTxnPaymentVoucher, "Payment voucher (one payment on a vendor bill)", "VendorInvoice"},
		struct{ Key, Label, Doctype string }{PrintTxnCustomerStatement, "Customer statement of account", "Customer"},
		struct{ Key, Label, Doctype string }{PrintTxnVendorStatement, "Vendor statement of account", "Vendor"})
	printShapes["SubcontractOrder"] = printShape{title: "Job Work Order", partyTitle: "Job worker", lines: jobWorkOrderPrintLines, columns: []genericPrintColumn{
		{Key: "direction", Label: "Goods"}, {Key: "sku", Label: "SKU"}, {Key: "item_name", Label: "Item"}, {Key: "qty", Label: "Qty"},
		{Key: "net_weight", Label: "Net wt"}, {Key: "fine_weight", Label: "Fine wt"}}}
	printShapes["PaymentProposal"] = printShape{title: "Payment Advice", hide: []string{"invoice_ids"}, lines: paymentAdviceLines, columns: []genericPrintColumn{
		{Key: "vendor", Label: "Vendor"}, {Key: "invoice_number", Label: "Bill no"}, {Key: "invoice_date", Label: "Bill date"}, {Key: "amount", Label: "Amount"}}}
	printShapes["ProductionOrder"] = printShape{title: "Material Issue Slip", lines: productionIssueSlipLines, columns: []genericPrintColumn{
		{Key: "sku", Label: "Component"}, {Key: "item_name", Label: "Item"}, {Key: "per_unit", Label: "Per unit"}, {Key: "qty", Label: "To issue"}, {Key: "issued_by", Label: "Issued by"}}}
	printShapes[MaterialIssueDoctype] = printShape{title: "Material Issue Slip", partyTitle: "Department", lines: materialIssueSlipLines, columns: []genericPrintColumn{
		{Key: "sku", Label: "SKU"}, {Key: "item_name", Label: "Item"}, {Key: "qty", Label: "Asked"}, {Key: "issued_qty", Label: "Issued"}, {Key: "returned_qty", Label: "Returned"}}}
	printShapes["PhysicalInventory"] = printShape{title: "Count Sheet", lines: countSheetLines, columns: []genericPrintColumn{
		{Key: "bin", Label: "Bin"}, {Key: "sku", Label: "SKU"}, {Key: "item_name", Label: "Item"}, {Key: "counted", Label: "Counted"}, {Key: "counter", Label: "Counted by"}}}
	printShapes["PurchaseRequisition"] = printShape{title: "Purchase Requisition", hide: []string{"description", "quantity"}, lines: requisitionLines, columns: []genericPrintColumn{
		{Key: "description", Label: "What is needed"}, {Key: "quantity", Label: "Qty"}, {Key: "amount", Label: "Estimated value"}}}
	printShapes["RFQ"] = printShape{title: "Request for Quotation", hide: []string{"description", "quantity"}, lines: rfqLines, columns: []genericPrintColumn{
		{Key: "sku", Label: "SKU"}, {Key: "description", Label: "Item / description"}, {Key: "quantity", Label: "Qty"}, {Key: "target_date", Label: "Needed by"}, {Key: "rate", Label: "Your rate"}}}
}

func printItemName(tenantID, sku string) string {
	if item, _, err := fetchDocData(tenantID, "Item", sku); err == nil && item != nil {
		return firstNonEmpty(strField(item, "name"), strField(item, "item_name"))
	}
	return ""
}

// jobWorkOrderPrintLines: each line sent, and what comes back (74.3.3). The
// statutory challan is the order's delivery challan.
func jobWorkOrderPrintLines(tenantID, _ string, data map[string]interface{}) []map[string]interface{} {
	bal, _ := jobWorkState(data)
	var out []map[string]interface{}
	for _, l := range jobWorkLines(data) {
		out = append(out, map[string]interface{}{"direction": "Sent", "sku": l.SKU, "item_name": printItemName(tenantID, l.SKU), "qty": float64(l.Qty),
			"net_weight": l.NetWeight, "fine_weight": l.FineWeight})
		back := firstNonEmpty(l.OutputSKU, l.SKU)
		out = append(out, map[string]interface{}{"direction": "To come back", "sku": back, "item_name": printItemName(tenantID, back), "qty": round2(float64(l.Qty) * l.yield())})
		if b := findJobWorkBalance(bal, l.LineNo); b != nil && b.Accepted+b.OutputQty > 0 {
			got := float64(b.Accepted)
			if l.transforms() {
				got = float64(b.OutputQty)
			}
			out = append(out, map[string]interface{}{"direction": "Received back", "sku": back, "item_name": printItemName(tenantID, back), "qty": got,
				"net_weight": b.BackW[1], "fine_weight": b.BackW[2]})
		}
	}
	return out
}

// paymentAdviceLines: the bills a payment proposal pays.
func paymentAdviceLines(tenantID, _ string, data map[string]interface{}) []map[string]interface{} {
	var out []map[string]interface{}
	for _, id := range strings.Split(strField(data, "invoice_ids"), ",") {
		id = strings.TrimSpace(id)
		if id == "" {
			continue
		}
		bill, _, err := fetchDocData(tenantID, "VendorInvoice", id)
		if err != nil || bill == nil {
			out = append(out, map[string]interface{}{"invoice_number": id})
			continue
		}
		out = append(out, map[string]interface{}{"vendor": strField(bill, "vendor_id"), "invoice_number": firstNonEmpty(strField(bill, "invoice_number"), id),
			"invoice_date": strField(bill, "invoice_date"), "amount": round2(numFromInterface(bill["invoice_amount"]))})
	}
	return out
}

// productionIssueSlipLines: the order's BOM, each component for the order's
// quantity with its scrap.
func productionIssueSlipLines(tenantID, _ string, data map[string]interface{}) []map[string]interface{} {
	bom, _, err := fetchDocData(tenantID, "BOM", strField(data, "bom_id"))
	if err != nil || bom == nil {
		return nil
	}
	qty := numFromInterface(data["quantity"])
	var out []map[string]interface{}
	for _, c := range jsonLines(bom["components"]) {
		sku := firstNonEmpty(strField(c, "sku"), strField(c, "item"), strField(c, "component"))
		per := numFromInterface(firstPresent(c, "qty", "quantity", "qty_per_unit"))
		scrap := numFromInterface(firstPresent(c, "scrap_pct", "scrap_percent", "scrap"))
		out = append(out, map[string]interface{}{"sku": sku, "item_name": printItemName(tenantID, sku), "per_unit": per,
			"qty": float64(wholeUnitsUp(per * qty * (1 + scrap/100))), "issued_by": ""})
	}
	return out
}

// materialIssueSlipLines: 130.15's request, what was issued and returned.
func materialIssueSlipLines(_, _ string, data map[string]interface{}) []map[string]interface{} {
	var out []map[string]interface{}
	for _, l := range jsonLines(data["items"]) {
		out = append(out, map[string]interface{}{"sku": strField(l, "sku"), "item_name": strField(l, "item_name"), "qty": numFromInterface(l["qty"]),
			"issued_qty": numFromInterface(l["issued_qty"]), "returned_qty": numFromInterface(l["returned_qty"])})
	}
	return out
}

// countSheetLines: a physical inventory's count lines, blind - no system
// quantity, a box to write the count.
func countSheetLines(tenantID, id string, _ map[string]interface{}) []map[string]interface{} {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return nil
	}
	rows, err := db.DB.Query(fmt.Sprintf(`SELECT COALESCE(data->>'bin', ''), COALESCE(data->>'sku', '') FROM %s.documents
		WHERE doctype = 'CycleCountLine' AND deleted_at IS NULL AND data->>'physical_inventory' = $1 ORDER BY 1, 2 LIMIT 5000`, schema), id)
	if err != nil {
		return nil
	}
	defer rows.Close()
	var out []map[string]interface{}
	for rows.Next() {
		var bin, sku string
		if rows.Scan(&bin, &sku) == nil {
			out = append(out, map[string]interface{}{"bin": bin, "sku": sku, "item_name": printItemName(tenantID, sku), "counted": "", "counter": ""})
		}
	}
	return out
}

func requisitionLines(_, _ string, data map[string]interface{}) []map[string]interface{} {
	if lines := jsonLines(data["items"]); len(lines) > 0 {
		return lines
	}
	return []map[string]interface{}{{"description": strField(data, "description"), "quantity": numFromInterface(data["quantity"]), "amount": round2(numFromInterface(data["total_amount"]))}}
}

func rfqLines(tenantID, _ string, data map[string]interface{}) []map[string]interface{} {
	var out []map[string]interface{}
	for _, l := range jsonLines(data["lines"]) {
		sku := firstNonEmpty(strField(l, "sku"), strField(l, "item"))
		out = append(out, map[string]interface{}{"sku": sku, "description": firstNonEmpty(strField(l, "description"), strField(l, "item_name"), printItemName(tenantID, sku)),
			"quantity": numFromInterface(firstPresent(l, "qty", "quantity")), "target_date": firstNonEmpty(strField(l, "target_date"), strField(data, "target_date")), "rate": ""})
	}
	if len(out) == 0 {
		out = append(out, map[string]interface{}{"description": strField(data, "description"), "quantity": numFromInterface(data["quantity"]), "target_date": strField(data, "target_date"), "rate": ""})
	}
	return out
}

// --- prints with no document of their own --------------------------------------------

// buildSpecialPrint fills a voucher or a statement; false when txn is not one.
func buildSpecialPrint(tenantID, txn, id string, d *PrintDocument) (bool, string, error) {
	switch txn {
	case PrintTxnReceiptVoucher, PrintTxnPaymentVoucher:
		store, err := buildSettlementVoucher(tenantID, txn, id, d)
		return true, store, err
	case PrintTxnCustomerStatement, PrintTxnVendorStatement:
		store, err := buildStatement(tenantID, txn, id, d)
		return true, store, err
	}
	return false, "", nil
}

func specialPrintColumns(d *PrintDocument, header, columns []genericPrintColumn, money []string) {
	var totals []genericPrintColumn
	for _, k := range money {
		sum := 0.0
		for _, l := range d.Lines {
			sum += numFromInterface(l[k])
		}
		d.Fields["total__"+k] = round2(sum)
		for _, c := range columns {
			if c.Key == k {
				totals = append(totals, genericPrintColumn{Key: "fields.total__" + k, Label: "Total " + strings.ToLower(c.Label), Format: "money"})
			}
		}
	}
	d.Fields["generic_print"] = map[string]interface{}{"header": header, "columns": columns, "totals": totals}
}

func buildSettlementVoucher(tenantID, txn, id string, d *PrintDocument) (string, error) {
	cut := strings.LastIndex(id, "~")
	if cut < 0 {
		return "", &ValidationError{Code: "GLOBAL-0004", Message: "a voucher is printed from one receipt or payment on its invoice"}
	}
	invoiceID, ref := id[:cut], id[cut+1:]
	doctype, field := "SalesInvoice", "settlements"
	if txn == PrintTxnPaymentVoucher {
		doctype, field = "VendorInvoice", "payments"
	}
	data, _, err := fetchDocData(tenantID, doctype, invoiceID)
	if err != nil || data == nil {
		return "", &ValidationError{Code: "GLOBAL-0004", Message: fmt.Sprintf("%s %s was not found", doctype, invoiceID)}
	}
	// The voucher is the invoice's store's (a vendor bill: its PO's).
	store := strField(data, "location")
	if store == "" {
		if po, _, err := fetchDocData(tenantID, "PurchaseOrder", strField(data, "po_id")); err == nil && po != nil {
			store = firstNonEmpty(strField(po, "location"), strField(po, "target_warehouse"))
		}
	}
	entries, _ := readSettlementEntries(data, field)
	for n, e := range entries {
		if e.Reference != ref {
			continue
		}
		invoiceNo := firstNonEmpty(strField(data, "invoice_number"), invoiceID)
		d.Number, d.Date = fmt.Sprintf("%s/%d", invoiceNo, n+1), e.Date
		if txn == PrintTxnReceiptVoucher {
			d.Title = "Receipt Voucher"
			d.Party, d.PartyTitle = loadPrintCustomer(tenantID, strField(data, "customer")), "Received from"
		} else {
			d.Title = "Payment Voucher"
			d.Party, d.PartyTitle = loadPrintVendor(tenantID, strField(data, "vendor_id")), "Paid to"
		}
		d.Fields["against"], d.Fields["mode"], d.Fields["reference"], d.Fields["by"] = invoiceNo, e.Kind, e.Reference, e.By
		d.Lines = []map[string]interface{}{{"sr": 1, "description": fmt.Sprintf("%s against %s", e.Kind, invoiceNo), "amount": round2(e.Amount)}}
		specialPrintColumns(d, []genericPrintColumn{{Key: "against", Label: "Against invoice"}, {Key: "mode", Label: "How"}, {Key: "by", Label: "Recorded by"}},
			[]genericPrintColumn{{Key: "sr", Label: "#"}, {Key: "description", Label: "Particulars"}, {Key: "amount", Label: "Amount"}}, []string{"amount"})
		d.AmountInWords = AmountInWords(e.Amount)
		return store, nil
	}
	return "", &ValidationError{Code: "GLOBAL-0004", Message: "that receipt or payment is not on the invoice"}
}

// buildStatement is a party's account: each invoice, receipt / payment and
// credit note in date order with the running balance.
func buildStatement(tenantID, txn, partyID string, d *PrintDocument) (string, error) {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return "", err
	}
	type entry struct {
		date, doc, particulars string
		debit, credit          float64
	}
	var es []entry
	store := ""
	if txn == PrintTxnCustomerStatement {
		d.Title, d.Party, d.PartyTitle = "Statement of Account", loadPrintCustomer(tenantID, partyID), "Customer"
		rows, err := db.DB.Query(fmt.Sprintf(`SELECT id, data, created_at FROM %s.documents WHERE doctype = 'SalesInvoice' AND deleted_at IS NULL
			AND status NOT IN ('Draft', 'Cancelled') AND data->>'customer' = $1 ORDER BY created_at`, schema), partyID)
		if err != nil {
			return "", err
		}
		for rows.Next() {
			var id, raw string
			var at time.Time
			if rows.Scan(&id, &raw, &at) != nil {
				continue
			}
			var data map[string]interface{}
			_ = json.Unmarshal([]byte(raw), &data)
			no := firstNonEmpty(strField(data, "invoice_number"), id)
			store = firstNonEmpty(store, strField(data, "location"))
			es = append(es, entry{date: firstNonEmpty(strField(data, "invoice_date"), at.Format("2006-01-02")), doc: no, particulars: "Invoice", debit: numFromInterface(data["total_amount"])})
			settled, _ := readSettlementEntries(data, "settlements")
			for _, s := range settled {
				es = append(es, entry{date: s.Date, doc: no, particulars: firstNonEmpty(s.Kind, "Receipt"), credit: s.Amount})
			}
		}
		rows.Close()
		// 140.12.7: the rest of the customer's account - advances (2640) and
		// store credit (2650) taken, used and refunded, and till bills - so
		// the statement is the whole story, not receivables (1300) alone.
		// Money received is a credit; using or refunding it is a debit.
		sv, err := db.DB.Query(fmt.Sprintf(`SELECT l.created_at, l.instrument_id, i.profile, l.event_type, l.amount_paise,
			COALESCE(l.source_doctype, ''), COALESCE(l.source_id, '')
			FROM %s.stored_value_ledger l JOIN %s.stored_value_instruments i ON i.instrument_id = l.instrument_id
			WHERE i.customer_id = $1 AND i.profile IN ($2, $3) AND l.amount_paise <> 0 ORDER BY l.created_at, l.event_seq`, schema, schema),
			partyID, AdvanceProfileID, StoreCreditProfileID)
		if err != nil {
			return "", err
		}
		for sv.Next() {
			var at time.Time
			var inst, profile, event, srcType, srcID string
			var amount int64
			if sv.Scan(&at, &inst, &profile, &event, &amount, &srcType, &srcID) != nil {
				continue
			}
			what := map[bool]string{true: "Advance", false: "Store credit"}[profile == AdvanceProfileID]
			e := entry{date: at.Format("2006-01-02"), doc: inst}
			switch {
			case amount > 0:
				e.particulars, e.credit = what+" received", PaiseToRupees(amount)
			case event == SVEventRefund:
				e.particulars, e.debit = what+" refunded", PaiseToRupees(-amount)
			default:
				e.particulars, e.debit = strings.TrimSpace(what+" used "+srcID), PaiseToRupees(-amount)
			}
			es = append(es, e)
		}
		sv.Close()
		bills, err := db.DB.Query(fmt.Sprintf(`SELECT created_at, COALESCE(data->>'bill_number', id), COALESCE((data->>'amount_paid')::numeric, 0)
			FROM %s.documents WHERE doctype = 'POSCart' AND status = 'Paid' AND deleted_at IS NULL AND data->>'customer_id' = $1 ORDER BY created_at`, schema), partyID)
		if err != nil {
			return "", err
		}
		for bills.Next() {
			var at time.Time
			var bill string
			var amount float64
			if bills.Scan(&at, &bill, &amount) != nil || amount == 0 {
				continue
			}
			day := at.Format("2006-01-02")
			es = append(es, entry{date: day, doc: bill, particulars: "Till bill", debit: amount}, entry{date: day, doc: bill, particulars: "Paid at the till", credit: amount})
		}
		bills.Close()
	} else {
		d.Title, d.Party, d.PartyTitle = "Statement of Account", loadPrintVendor(tenantID, partyID), "Vendor"
		rows, err := db.DB.Query(fmt.Sprintf(`SELECT id, data, created_at FROM %s.documents WHERE doctype = 'VendorInvoice' AND deleted_at IS NULL
			AND status NOT IN ('Draft', 'Cancelled') AND data->>'vendor_id' = $1 ORDER BY created_at`, schema), partyID)
		if err != nil {
			return "", err
		}
		for rows.Next() {
			var id, raw string
			var at time.Time
			if rows.Scan(&id, &raw, &at) != nil {
				continue
			}
			var data map[string]interface{}
			_ = json.Unmarshal([]byte(raw), &data)
			no := firstNonEmpty(strField(data, "invoice_number"), id)
			es = append(es, entry{date: firstNonEmpty(strField(data, "invoice_date"), at.Format("2006-01-02")), doc: no, particulars: "Bill", credit: numFromInterface(data["invoice_amount"])})
			paid, _ := readSettlementEntries(data, "payments")
			for _, s := range paid {
				es = append(es, entry{date: s.Date, doc: no, particulars: firstNonEmpty(s.Kind, "Payment"), debit: s.Amount})
			}
		}
		rows.Close()
	}
	sort.SliceStable(es, func(i, j int) bool { return es[i].date < es[j].date })
	balance := 0.0
	d.Lines = []map[string]interface{}{}
	for i, e := range es {
		if txn == PrintTxnCustomerStatement {
			balance = round2(balance + e.debit - e.credit)
		} else {
			balance = round2(balance + e.credit - e.debit)
		}
		row := map[string]interface{}{"sr": i + 1, "date": e.date, "document": e.doc, "particulars": e.particulars, "balance": balance}
		if e.debit != 0 {
			row["debit"] = round2(e.debit)
		}
		if e.credit != 0 {
			row["credit"] = round2(e.credit)
		}
		d.Lines = append(d.Lines, row)
	}
	d.Number, d.Date = partyID, BusinessToday()
	d.Fields["closing_balance"] = balance
	d.Fields["balance_meaning"] = map[bool]string{true: "owed to us", false: "owed by us"}[txn == PrintTxnCustomerStatement]
	specialPrintColumns(d, []genericPrintColumn{{Key: "closing_balance", Label: "Closing balance"}, {Key: "balance_meaning", Label: "Balance is"}},
		[]genericPrintColumn{{Key: "sr", Label: "#"}, {Key: "date", Label: "Date"}, {Key: "document", Label: "Document"}, {Key: "particulars", Label: "Particulars"},
			{Key: "debit", Label: "Debit"}, {Key: "credit", Label: "Credit"}, {Key: "balance", Label: "Balance"}}, []string{"debit", "credit"})
	return store, nil
}
