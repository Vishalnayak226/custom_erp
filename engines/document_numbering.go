package engines

import (
	"fmt"
	"strings"
	"time"
)

// documentNumberSeries describes how one doctype draws its document number.
//
// SeriesKey is the prefix_configs.doc_type row that owns the series - it is
// deliberately not the doctype name, because several doctypes are known to
// users by a short code that already exists as a series ("PO", "TO"), and
// because the tenant's Prefix Configurations screen is keyed by it.
//
// NumberFields lists the payload fields that must carry the generated number.
// Most doctypes have exactly one; PurchaseOrder has two (po_number and code)
// because it accumulated two overlapping mandatory field registrations early in
// this project's history - db/migration.sql declared po_number, and
// db/migrations_phase3.sql declared code - and both are still mandatory, so
// both are still filled, exactly as the create form did by hand before.
type documentNumberSeries struct {
	SeriesKey    string
	NumberFields []string
}

// documentNumberSeriesByDoctype is the whole scope of server-side numbering:
// the doctypes whose create screens used to ask a human to type the number.
//
// Doctypes created by engines rather than by a person (SalesOrder, Shipment,
// Return, settlements, ...) are deliberately absent - they already mint their
// own identifiers through their own flows, and re-numbering them here would
// change live document ids for no user-facing gain.
//
// Note ASN maps only asn_number: its po_number field holds the *referenced
// PO's* number, not the ASN's own, and overwriting it would silently detach
// every ASN from its purchase order.
var documentNumberSeriesByDoctype = map[string]documentNumberSeries{
	"PurchaseOrder":   {SeriesKey: "PO", NumberFields: []string{"po_number", "code"}},
	"GRN":             {SeriesKey: "GRN", NumberFields: []string{"code"}},
	"ASN":             {SeriesKey: "ASN", NumberFields: []string{"asn_number"}},
	"RFQ":             {SeriesKey: "RFQ", NumberFields: []string{"code"}},
	"VendorQuote":     {SeriesKey: "QTN", NumberFields: []string{"code"}},
	"TransferOrder":   {SeriesKey: "TO", NumberFields: []string{"transfer_number"}},
	"ExpenseClaim":    {SeriesKey: "EXP", NumberFields: []string{"code"}},
	"Leave":           {SeriesKey: "LV", NumberFields: []string{"code"}},
	"EmployeeLoan":    {SeriesKey: "LOAN", NumberFields: []string{"code"}},
	"Grievance":       {SeriesKey: "GRV", NumberFields: []string{"code"}},
	"ProductionOrder": {SeriesKey: "PRO", NumberFields: []string{"code"}},
	"Attendance":      {SeriesKey: "ATT", NumberFields: []string{"code"}},
}

// autoNumberedFields is the set of a doctype's fields the server fills in with
// its generated number. GetDocTypeMeta stamps these onto the field metadata so
// the generic record form can render them read-only, driven by this registry
// rather than by a duplicate list in JavaScript.
func autoNumberedFields(doctype string) map[string]bool {
	series, ok := documentNumberSeriesByDoctype[doctype]
	if !ok {
		return nil
	}
	fields := make(map[string]bool, len(series.NumberFields))
	for _, name := range series.NumberFields {
		fields[name] = true
	}
	return fields
}

// DocumentNumberSeriesKey reports the numbering series a doctype draws from,
// and whether it has one at all. Exported for the import path, so a CSV row
// with no id gets the same series the UI would have given it.
func DocumentNumberSeriesKey(doctype string) (string, bool) {
	series, ok := documentNumberSeriesByDoctype[doctype]
	if !ok {
		return "", false
	}
	return series.SeriesKey, true
}

// PrepareDocumentNumber assigns a server-generated number to a new document.
//
// This is the counterpart to PreparePurchaseRequisition, generalized: the
// document number is not a value a browser may choose. Two makers on the same
// screen typing the same number used to mean the second save silently replaced
// the first, because the number was also the document id and the upsert keys
// on it. Drawing from the row-locked counter instead makes that impossible,
// and leaves the series shape in the tenant's own hands via Prefix Configs.
//
// Scope is deliberately "a create that names no document": isCreate covers the
// route (a path id means an update), and a payload that carries its own id is
// left alone too, because posting an id to the collection route is this API's
// long-standing upsert form - handleGenericDoc resolves its document id from
// payload["id"] when the path has none, ProductContent's composite-id save
// relies on it, and so does the Stage 29.8 status-transition test. Numbering
// those would silently turn an update into a second document.
//
// The guarantee this gives is therefore about the screens, which is where the
// problem was: no create form sends an id any more, so no user can choose or
// collide on a number. An integration that explicitly addresses a document by
// id keeps the upsert behavior it already had.
//
// A failure here must block the save. Falling back to a client value or a
// random id would put a document in the ledger outside its series, which is
// exactly what an auditor looks for.
func PrepareDocumentNumber(tenantID, location, doctype string, isCreate bool, payload map[string]interface{}) error {
	series, ok := documentNumberSeriesByDoctype[doctype]
	if !ok || !isCreate {
		return nil
	}
	if existing, present := payload["id"]; present && strings.TrimSpace(fmt.Sprintf("%v", existing)) != "" {
		return nil
	}

	// Store segment: the maker's resolved location, matching what
	// PreparePurchaseRequisition does. "HQ" is the fallback for a session with
	// no location (an HR/Admin acting centrally), not a silent failure - the
	// number still has to come from somewhere deterministic.
	storeCode := strings.TrimSpace(location)
	if storeCode == "" {
		storeCode = "HQ"
	}

	code, err := GenerateSequence(tenantID, series.SeriesKey, storeCode, documentFinancialYear(time.Now()))
	if err != nil {
		// Preserve a precise *ValidationError (e.g. ADMINC-0030, an
		// deactivated series) instead of flattening it into a plain error -
		// the caller maps it to a real catalog code and a fixable message.
		if verr, ok := err.(*ValidationError); ok {
			return verr
		}
		return fmt.Errorf("could not generate a %s number: %v", doctype, err)
	}

	payload["id"] = code
	for _, field := range series.NumberFields {
		payload[field] = code
	}
	return nil
}

// documentFinancialYear renders the Indian financial year (April-March) a date
// falls in, as "26-27". Shared by every generated number so a tenant's
// documents all agree on which year a given month belongs to.
func documentFinancialYear(now time.Time) string {
	startYear := now.Year()
	if now.Month() < time.April {
		startYear--
	}
	return fmt.Sprintf("%02d-%02d", startYear%100, (startYear+1)%100)
}
