package engines

import (
	"encoding/json"
	"fmt"
	"strings"
)

// Stage 148.50 - the vendor-advance refusals (FIN-0923 / FIN-0924 and their
// siblings) named documents by their raw ids. They now name the bill by the
// number on the vendor's invoice, the vendor by name, and the advance by its
// amount, vendor and date.

// docLabelData returns the stored data of one document, or nil.
func docLabelData(q docQuerier, schema, doctype, id string) map[string]interface{} {
	if strings.TrimSpace(id) == "" || q == nil {
		return nil
	}
	var raw []byte
	if q.QueryRow(fmt.Sprintf(`SELECT data FROM %s.documents WHERE doctype = $1 AND id = $2 AND deleted_at IS NULL`, schema), doctype, id).Scan(&raw) != nil {
		return nil
	}
	var d map[string]interface{}
	if json.Unmarshal(raw, &d) != nil {
		return nil
	}
	return d
}

// billLabel is "bill N-123" (the vendor's number), else the document id.
func billLabel(q docQuerier, schema, id string, bill map[string]interface{}) string {
	if bill == nil {
		bill = docLabelData(q, schema, "VendorInvoice", id)
	}
	return "bill " + firstNonEmpty(strField(bill, "invoice_number"), id)
}

// poLabel is "purchase order <number>".
func poLabel(q docQuerier, schema, id string) string {
	d := docLabelData(q, schema, "PurchaseOrder", id)
	return "purchase order " + firstNonEmpty(strField(d, "po_number"), strField(d, "number"), id)
}

// vendorLabel is the vendor's name, else the reference given.
func vendorLabel(q docQuerier, schema, ref string) string {
	if _, v, ok := findVendorRow(q, schema, ref); ok {
		return firstNonEmpty(strField(v, "name"), ref)
	}
	return ref
}

// advanceLabel is "the advance of 1500.00 to <vendor> dated <date>".
func advanceLabel(q docQuerier, schema string, adv map[string]interface{}) string {
	s := fmt.Sprintf("the advance of %.2f to %s", numFromInterface(adv["amount"]), vendorLabel(q, schema, strField(adv, "vendor_id")))
	if d := strField(adv, "advance_date"); d != "" {
		s += " dated " + d
	}
	return s
}
