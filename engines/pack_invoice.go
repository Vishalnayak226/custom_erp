package engines

import (
	"custom_erp/db"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
)

// Stage 35.4.2: generate a SalesInvoice from a completed pack.
//
// This closes the item 26.12.3 deliberately deferred. Its stated reason was
// that "there is nothing real to generate the invoice from yet" - no
// order->shipment->invoice chain for SalesOrder, and no object representing the
// pack. R1 built the first and 35.4.1 built the second, so the reason has
// expired.
//
// What this invoices, and why it matters that it is the package and not the
// order: the package holds what is physically in the box after picking,
// short-picking and any split. Invoicing the order would bill a customer for a
// short-picked unit that is not in the parcel they receive. One package, one
// invoice - which is also what makes a split order's two parcels each carry
// their own correct tax document.
//
// Posting stays a separate, explicit finance action. This function produces a
// Draft; PostSalesInvoice recognises the receivable. Warehouse automation
// creating GL postings on its own is exactly the coupling the existing
// CreateSalesInvoiceFromOrder comment already refuses, and this follows it.

// PackInvoiceOptions carries the few things the pack floor cannot derive.
type PackInvoiceOptions struct {
	// Interstate overrides the derived place of supply. Nil means "derive it".
	// An override exists because a bill-to/ship-to split is real and neither
	// the order nor the location master captures it - the same escape hatch
	// ApplyPlaceOfSupply already gives the purchase side.
	Interstate *bool
}

// GenerateInvoiceForPackage creates one Draft SalesInvoice for a Draft package
// and moves the package to Invoiced.
//
// Idempotent on the package, not on the order: calling it twice returns the
// first invoice. A split order legitimately produces two invoices, so
// de-duplicating on order id would be wrong - the package is the unit.
func GenerateInvoiceForPackage(tenantID, packageID, userID string, opts PackInvoiceOptions) (string, error) {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return "", err
	}
	p, err := loadShippingPackage(schema, packageID)
	if err != nil {
		return "", err
	}
	if p.SalesInvoiceID != "" {
		return p.SalesInvoiceID, nil
	}
	if p.Status == "Cancelled" {
		return "", fmt.Errorf("shipping package %s is cancelled and cannot be invoiced", packageID)
	}
	if err := shippingPackageMutableStatus(p.Status); err != nil {
		return "", err
	}
	if len(p.Items) == 0 {
		return "", fmt.Errorf("shipping package %s has no contents to invoice", packageID)
	}

	// Prices come from the order's own lines, never from the item master. The
	// customer agreed a price when they placed the order; re-reading today's
	// sale_price would silently re-price a shipment mid-flight, which is the
	// classic version of this bug.
	prices, err := orderLineUnitPrices(schema, p.OrderID)
	if err != nil {
		return "", err
	}

	lines := make([]GSTLineInput, 0, len(p.Items))
	invoiceLines := make([]map[string]interface{}, 0, len(p.Items))
	grossTotal := 0.0
	for _, it := range p.Items {
		unit, ok := prices[it.SKU]
		if !ok {
			return "", fmt.Errorf("sku %s is in package %s but not on order %s - cannot price it", it.SKU, packageID, p.OrderID)
		}
		lineTotal := round2(unit * float64(it.Qty))
		grossTotal = round2(grossTotal + lineTotal)
		lines = append(lines, GSTLineInput{Sku: it.SKU, Qty: it.Qty, UnitRate: unit})
		invoiceLines = append(invoiceLines, map[string]interface{}{
			"sku": it.SKU, "qty": it.Qty, "unit_price": unit, "line_total": lineTotal,
		})
	}

	interstate, basis := resolvePackInterstate(tenantID, schema, p, opts)
	breakdown, err := ComputeGSTForLines(tenantID, lines, interstate)
	if err != nil {
		return "", fmt.Errorf("cannot invoice package %s: %v", packageID, err)
	}
	if breakdown.TotalAmount <= 0 || grossTotal <= 0 {
		return "", fmt.Errorf("shipping package %s prices to zero - nothing to invoice", packageID)
	}
	amounts := reconcileInvoiceAmounts(breakdown, grossTotal, interstate)

	customerName, _ := orderFieldString(schema, p.OrderID, "customer_name")
	invoiceID := NewDocID("SINV")
	invoiceData := map[string]interface{}{
		"code":                invoiceID,
		"invoice_number":      invoiceID,
		"sales_order_id":      p.OrderID,
		"shipping_package_id": p.ID,
		"customer":            customerName,
		"location":            p.LocationCode,
		// total_amount keeps exactly the meaning every existing reader gives
		// it - what the customer pays, tax included. PostSalesInvoice and the
		// receivables ageing report are untouched by this stage.
		"total_amount":   amounts.Total,
		"taxable_amount": amounts.Taxable,
		"non_taxable":    amounts.NonTaxable,
		"cgst":           amounts.CGST,
		"sgst":           amounts.SGST,
		"igst":           amounts.IGST,
		"total_tax":      amounts.TotalTax,
		"interstate":     interstate,
		"gst_basis":      basis,
		"status":         "Draft",
	}
	if encodedLines, err := json.Marshal(invoiceLines); err == nil {
		invoiceData["items"] = string(encodedLines)
	}
	encoded, err := json.Marshal(invoiceData)
	if err != nil {
		return "", err
	}

	// Both writes in one transaction. A package marked Invoiced with no
	// invoice, or an invoice against a package still open for edits, are both
	// states 35.4.3's ordering rule would then read wrongly.
	tx, err := db.DB.Begin()
	if err != nil {
		return "", err
	}
	defer tx.Rollback()
	if err := db.SetSearchPath(tx, schema); err != nil {
		return "", err
	}
	if _, err := tx.Exec(fmt.Sprintf(
		`INSERT INTO %s.documents (id, doctype, data, status, created_by) VALUES ($1, 'SalesInvoice', $2, 'Draft', 'system')`, schema),
		invoiceID, encoded); err != nil {
		return "", err
	}
	if err := ValidateStatusTransition(tenantID, "ShippingPackage", p.Status, "Invoiced", nil); err != nil {
		return "", err
	}
	p.Status = "Invoiced"
	p.SalesInvoiceID = invoiceID
	if err := p.save(tx, schema); err != nil {
		return "", err
	}
	if err := tx.Commit(); err != nil {
		return "", err
	}

	LogAuditEvent(tenantID, userID, "CREATE_SALES_INVOICE", "SUCCESS",
		fmt.Sprintf("Created draft invoice %s from shipping package %s (order %s, total=%.2f, %s)", invoiceID, packageID, p.OrderID, breakdown.TotalAmount, basis))
	return invoiceID, nil
}

// invoiceAmounts is one invoice's money, rounded to paise and guaranteed to
// add up: Taxable + NonTaxable + TotalTax == Total, and CGST + SGST + IGST ==
// TotalTax, both exactly.
type invoiceAmounts struct {
	Total      float64
	Taxable    float64
	NonTaxable float64
	CGST       float64
	SGST       float64
	IGST       float64
	TotalTax   float64
}

// reconcileInvoiceAmounts turns a GSTBreakdown into figures an invoice can
// actually carry.
//
// This exists because the shared breakdown is not internally consistent to the
// paisa, and on a tax document that matters. Two drifts, both real and both
// observed on a plain two-line 18% order totalling 900:
//
//   - CGST and SGST are each round2(lineTax/2) per line and then summed, so
//     they came to 137.30 against a TotalTax of 137.28. An invoice whose tax
//     columns do not add up to its own tax total is a defect on a filed
//     document, not a display nicety.
//   - TaxableAmount is accumulated unrounded, so it was stored as
//     762.7118644067798 - a value that reconciles against nothing.
//
// The fix is deliberately confined here rather than applied to
// ComputeGSTForLinesMode. That function's own contract is that every existing
// sale-side caller stays byte-identical, and POS checkout is one of them;
// changing the shared aggregation to fix an invoicing concern would silently
// move numbers on a path this stage has no business touching.
//
// Rounding policy, in order:
//   - Total is the agreed gross - the sum of the line totals the customer
//     ordered at. It is not re-derived from tax, because the customer agreed a
//     price and a rounding residue must not change it.
//   - TotalTax is the engine's tax, rounded once.
//   - The components are split so they sum to TotalTax exactly - the second
//     one absorbs the half-paisa rather than being rounded independently, the
//     same technique ConvertPostingToFunctional uses to keep a voucher
//     balanced.
//   - Taxable is what is left after non-taxable turnover and tax. Deriving it
//     this way keeps exempt/nil/zero-rated turnover OUT of the taxable value,
//     which is a filed figure on GSTR-3B 3.1(a), while still reconciling.
func reconcileInvoiceAmounts(b GSTBreakdown, grossTotal float64, interstate bool) invoiceAmounts {
	out := invoiceAmounts{
		Total:      round2(grossTotal),
		NonTaxable: b.NonTaxableAmount(),
		TotalTax:   round2(b.TotalTax),
	}
	if interstate {
		out.IGST = out.TotalTax
	} else {
		out.CGST = round2(out.TotalTax / 2)
		out.SGST = round2(out.TotalTax - out.CGST)
	}
	out.Taxable = round2(out.Total - out.NonTaxable - out.TotalTax)
	return out
}

// orderLineUnitPrices maps sku -> agreed unit price for one order. A SKU
// appearing on two lines (a split order line, or the same product ordered
// twice) resolves to the first line's price; they are the same price in every
// path that creates them, and picking the first is deterministic.
func orderLineUnitPrices(schema, orderID string) (map[string]float64, error) {
	out := map[string]float64{}
	if strings.TrimSpace(orderID) == "" {
		return out, nil
	}
	rows, err := db.DB.Query(fmt.Sprintf(
		`SELECT data->>'sku', COALESCE(data->>'unit_price', '0')
		   FROM %s.documents
		  WHERE doctype = 'SalesOrderLine' AND deleted_at IS NULL AND data->>'order_id' = $1
		  ORDER BY created_at, id`, schema), orderID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var sku, price string
		if err := rows.Scan(&sku, &price); err != nil {
			return nil, err
		}
		if _, seen := out[sku]; seen {
			continue
		}
		out[sku] = numFromInterface(price)
	}
	return out, rows.Err()
}

func orderFieldString(schema, orderID, field string) (string, error) {
	if strings.TrimSpace(orderID) == "" {
		return "", nil
	}
	var v sql.NullString
	err := db.DB.QueryRow(fmt.Sprintf(
		`SELECT data->>'%s' FROM %s.documents WHERE doctype = 'SalesOrder' AND id = $1 AND deleted_at IS NULL`, field, schema),
		orderID).Scan(&v)
	if err == sql.ErrNoRows {
		return "", nil
	}
	return v.String, err
}

// resolvePackInterstate decides CGST+SGST versus IGST, and returns the basis it
// used so the invoice records how the decision was reached rather than leaving
// an auditor to guess.
//
// Honest about a real limitation: SalesOrder stores a free-text
// shipping_address and no state field, so there is nothing reliable to compare
// the seller's state against. Rather than parse an address - which would be
// wrong often enough to be worse than useless on a tax document - the order's
// optional `shipping_state` (written by the 35.3.2 order edit as a custom
// field, or by a channel adapter that has it) is used when present, and the
// default is intra-state, which is the correct default for the domestic retail
// case this repo serves. The override exists for everything else.
func resolvePackInterstate(tenantID, schema string, p *ShippingPackage, opts PackInvoiceOptions) (bool, string) {
	if opts.Interstate != nil {
		return *opts.Interstate, "operator override"
	}
	buyerState, err := shipToStateCode(schema, p.OrderID)
	if err == nil && buyerState != "" {
		sellerState, errS := buyerStateCode(tenantID, p.LocationCode)
		if errS == nil && sellerState != "" {
			return buyerState != sellerState, fmt.Sprintf("derived: ship-to %s vs dispatch %s", StateLabel(buyerState), StateLabel(sellerState))
		}
	}
	return false, "default intra-state (no ship-to state recorded on the order)"
}

// shipToStateCode reads whichever ship-to state the order actually carries.
// Both keys are checked because 35.3.2 namespaces operator-entered extras as
// custom_*, while an adapter that genuinely has the field writes it plainly.
func shipToStateCode(schema, orderID string) (string, error) {
	if strings.TrimSpace(orderID) == "" {
		return "", nil
	}
	var plain, custom sql.NullString
	err := db.DB.QueryRow(fmt.Sprintf(
		`SELECT data->>'shipping_state', data->>'custom_shipping_state'
		   FROM %s.documents WHERE doctype = 'SalesOrder' AND id = $1 AND deleted_at IS NULL`, schema),
		orderID).Scan(&plain, &custom)
	if err == sql.ErrNoRows {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	for _, candidate := range []string{plain.String, custom.String} {
		if code := StateCodeFromName(candidate); code != "" {
			return code, nil
		}
	}
	return "", nil
}
