package engines

import (
	"custom_erp/db"
	"fmt"
	"strings"
	"time"
)

// Stages 148.31 / 148.32: wholesale selling (module wholesale, off by default,
// decision D7).
//
//   - 148.31 One invoice per customer per period: summary_invoice_stage148_31.go
//   - 148.32 Drop-ship: dropship_stage148_32.go
//
// This file holds what both share: the switch, the codes and the fields the
// screens show only while the switch is on.
//
// Off means off. With the module off the fields below are left out of the
// meta every screen reads, a save that carries one is refused, the routes
// refuse (moduleGate) and the invoice, order and PO paths run exactly as they
// did. A client who switches it off with a drop-ship order still open keeps
// that order's PO and confirmation, because those follow the order, not the
// switch.

const (
	ModuleWholesale = "wholesale"

	wsOffCode          = "SALESP-0710" // wholesale is off
	wsBillToCode       = "SALESP-0711" // bill-to is the customer itself, missing, or itself billed elsewhere
	wsGroupBlockedCode = "SALESP-0712" // a group cannot be one invoice
	wsNothingCode      = "SALESP-0713" // nothing chosen / nothing to invoice
	wsInstalmentCode   = "SALESP-0714" // instalments do not add up
	wsNoVendorCode     = "SALESP-0715" // a drop-ship item without a vendor
	wsMixedOrderCode   = "SALESP-0716" // drop-ship and stock lines on one order
	wsPOLockedCode     = "SALESP-0717" // the PO is past Draft: the change is refused
	wsNotDropShipCode  = "SALESP-0718" // not a drop-ship order / line
	wsAlreadyCode      = "SALESP-0719" // already confirmed
	wsNoGRNCode        = "PURCHA-5105" // a goods receipt against a drop-ship PO
	wsPONotReadyCode   = "PURCHA-5106" // confirmation before the PO is approved
	wsBillCode         = "PURCHA-5107" // the vendor bill could not be made
)

// wholesaleServerSet are the fields only the engine writes: a form or an
// import may not forge them.
var wholesaleServerSet = map[string]map[string]bool{
	"PurchaseOrder": {"dropship_order_id": true, "ship_to_address": true},
}

// engineOnlyFields are the marks a trusted flow reads back (drop-ship
// confirmation bills the line's qty from the PO the engine linked; a summary
// invoice posts without stock; an assistant draft is told by its mark). A
// generic save keeps what is stored and a new record carries none.
var engineOnlyFields = map[string][]string{
	"SalesOrderLine": {"dropship", "vendor_id", "po_id", "po_line", "dropship_confirmed_at", "dropship_confirmed_by"},
	"SalesOrder":     {"dropship", "made_by_assistant", "assistant_user", "assistant_client", "submitted_from_draft", "submitted_order", "submitted_by"},
	"SalesInvoice":   {"summary_order_ids", "summary_order_amounts", "summary_lines", "delivery_challans"},
}

// KeepEngineOnlyFields is the generic save's guard for engineOnlyFields (beside
// KeepServerSetFields148, same call site).
func KeepEngineOnlyFields(tenantID, doctype, id string, payload map[string]interface{}) {
	keys := engineOnlyFields[doctype]
	if len(keys) == 0 {
		return
	}
	var prior map[string]interface{}
	if id != "" {
		prior, _, _ = fetchDocData(tenantID, doctype, id)
	}
	for _, k := range keys {
		delete(payload, k)
		if v, ok := prior[k]; ok {
			payload[k] = v
		}
	}
}

func wsErr(code, format string, args ...interface{}) error {
	return &ValidationError{Code: code, Message: fmt.Sprintf(format, args...)}
}

// wholesaleOn says the tenant has the module switched on.
func wholesaleOn(tenantID string) bool {
	on, _ := IsModuleEnabled(tenantID, ModuleWholesale)
	return on
}

func requireWholesale(tenantID string) error {
	if !wholesaleOn(tenantID) {
		return wsErr(wsOffCode, "Wholesale is off.")
	}
	return nil
}

// customerDoc reads a Customer's data by id (nil when it is not there).
func wsCustomer(tenantID, id string) map[string]interface{} {
	if strings.TrimSpace(id) == "" {
		return nil
	}
	data, _, err := fetchDocData(tenantID, "Customer", id)
	if err != nil {
		return nil
	}
	return data
}

func wsSchema(tenantID string) string {
	schema, _ := db.GetTenantSchema(tenantID)
	return schema
}

// wsInstalment is one "qty@date" piece of an order line.
type wsInstalment struct {
	Qty  float64
	Date string
}

// parseInstalments reads "5@2026-11-10, 5@2026-11-20".
func parseInstalments(s string) ([]wsInstalment, error) {
	var out []wsInstalment
	for _, part := range strings.Split(s, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		q, d, ok := strings.Cut(part, "@")
		var qty float64
		if _, err := fmt.Sscanf(strings.TrimSpace(q), "%g", &qty); !ok || err != nil || qty <= 0 {
			return nil, wsErr(wsInstalmentCode, "Instalment %q must read quantity@date, like 5@2026-11-10.", part)
		}
		d = strings.TrimSpace(d)
		if _, err := time.Parse("2006-01-02", d); err != nil {
			return nil, wsErr(wsInstalmentCode, "Instalment %q needs a date as YYYY-MM-DD.", part)
		}
		out = append(out, wsInstalment{qty, d})
	}
	return out, nil
}

// validateWholesaleDocument is ValidateDocument's one call. Off, a save that
// carries a wholesale field is refused; on, the rules of each field apply.
func validateWholesaleDocument(tenantID, doctype string, docData map[string]interface{}) error {
	used := false
	for f, mod := range moduleGatedFields[doctype] {
		if mod != ModuleWholesale {
			continue
		}
		if v, ok := docData[f]; ok && strings.TrimSpace(fmt.Sprint(v)) != "" && fmt.Sprint(v) != "<nil>" {
			used = true
		}
	}
	if !used {
		return nil
	}
	if !wholesaleOn(tenantID) {
		return wsErr(wsOffCode, "Wholesale is off.")
	}
	self := firstNonEmpty(strField(docData, "code"), strField(docData, "id"))
	switch doctype {
	case "Customer":
		bt := strField(docData, "bill_to_customer")
		if bt == "" {
			return nil
		}
		if bt == self {
			return wsErr(wsBillToCode, "A customer cannot be its own bill-to. Leave it blank.")
		}
		target := wsCustomer(tenantID, bt)
		if target == nil {
			return wsErr(wsBillToCode, "The bill-to customer was not found.")
		}
		if strField(target, "bill_to_customer") != "" {
			return wsErr(wsBillToCode, "The bill-to customer is itself billed to another. Pick the last one.")
		}
	case "Item":
		if strField(docData, "dropship") == "Yes" && strField(docData, "default_vendor") == "" {
			return wsErr(wsNoVendorCode, "A drop-ship item needs a default vendor.")
		}
	case "SalesOrderLine":
		parts, err := parseInstalments(strField(docData, "instalments"))
		if err != nil {
			return err
		}
		sum := 0.0
		for _, p := range parts {
			sum += p.Qty
		}
		if sum > numFromInterface(docData["qty"]) {
			return wsErr(wsInstalmentCode, "Instalments add up to %g, more than the quantity %g.", sum, numFromInterface(docData["qty"]))
		}
	case "PurchaseOrder":
		// The engine sets these two; a form may send back what is stored, never a change.
		prior, _, _ := fetchDocData(tenantID, "PurchaseOrder", self)
		for f := range wholesaleServerSet["PurchaseOrder"] {
			if strField(docData, f) != "" && (prior == nil || strField(prior, f) != strField(docData, f)) {
				return wsErr(wsNotDropShipCode, "The drop-ship link is set by the system.")
			}
		}
	}
	return nil
}
