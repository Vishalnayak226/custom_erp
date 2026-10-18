package engines

import (
	"custom_erp/db"
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
//
// StoreFields (Stage 76.8) names the document's own store, tried in order: the
// number comes from that store's series, not the maker's session store, so a
// PO raised at HO for Jayanagar carries Jayanagar's number.
//
// KeepID (Stage 76.11) marks a doctype whose id stays internal: a record-form
// create fills NumberFields but not the id, the way the engines that create
// these documents keep theirs (routes, links and replay keys use the id).
type documentNumberSeries struct {
	SeriesKey    string
	NumberFields []string
	StoreFields  []string
	KeepID       bool
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
	"PurchaseOrder":   {SeriesKey: "PO", NumberFields: []string{"po_number", "code"}, StoreFields: []string{"location", "target_warehouse"}},
	"GRN":             {SeriesKey: "GRN", NumberFields: []string{"code"}, StoreFields: []string{"location"}},
	"ASN":             {SeriesKey: "ASN", NumberFields: []string{"asn_number"}, StoreFields: []string{"location"}},
	"RFQ":             {SeriesKey: "RFQ", NumberFields: []string{"code"}, StoreFields: []string{"location"}},
	"VendorQuote":     {SeriesKey: "QTN", NumberFields: []string{"code"}},
	"TransferOrder":   {SeriesKey: "TO", NumberFields: []string{"transfer_number"}, StoreFields: []string{"from_warehouse"}},
	"ExpenseClaim":    {SeriesKey: "EXP", NumberFields: []string{"code"}, StoreFields: []string{"location"}},
	"Leave":           {SeriesKey: "LV", NumberFields: []string{"code"}},
	"EmployeeLoan":    {SeriesKey: "LOAN", NumberFields: []string{"code"}},
	"Grievance":       {SeriesKey: "GRV", NumberFields: []string{"code"}},
	"ProductionOrder": {SeriesKey: "PRO", NumberFields: []string{"code"}, StoreFields: []string{"location"}},
	"Attendance":      {SeriesKey: "ATT", NumberFields: []string{"code"}, StoreFields: []string{"location"}},
	"PurchaseReturn":  {SeriesKey: "PRT", NumberFields: []string{"code"}, StoreFields: []string{"location"}},
	// Stage 76.9: tax documents. A manual note or invoice on the record form
	// is issued its number, not typed; the ones engines create keep their
	// internal id and take the number through IssueDocumentNumber.
	"SalesInvoice":     {SeriesKey: "SI", NumberFields: []string{"invoice_number"}, StoreFields: []string{"location"}},
	"CreditNote":       {SeriesKey: "CN", NumberFields: []string{"note_number"}},
	"DebitNote":        {SeriesKey: "DN", NumberFields: []string{"note_number"}},
	"VendorCreditNote": {SeriesKey: "VCN", NumberFields: []string{"note_number"}},
	// Stage 76.11: documents engines create under an internal NewDocID. The
	// id stays; the printed number goes in code (and a JV's voucher_number).
	"SalesOrder":        {SeriesKey: "SO", NumberFields: []string{"code"}, KeepID: true},
	"ReturnRequest":     {SeriesKey: "SR", NumberFields: []string{"code"}, StoreFields: []string{"return_location"}, KeepID: true},
	"RefundRequest":     {SeriesKey: "RF", NumberFields: []string{"code"}, KeepID: true},
	"JournalVoucher":    {SeriesKey: "JV", NumberFields: []string{"code", "voucher_number"}, KeepID: true},
	"PhysicalInventory": {SeriesKey: "PI", NumberFields: []string{"code"}, StoreFields: []string{"location"}, KeepID: true},
	"GatePass":          {SeriesKey: "GP", NumberFields: []string{"code"}, StoreFields: []string{"location_code"}, KeepID: true},
	"ConsumptionEntry":  {SeriesKey: "CE", NumberFields: []string{"code"}, StoreFields: []string{"location"}, KeepID: true},
	"BundleAssembly":    {SeriesKey: "KIT", NumberFields: []string{"code"}, StoreFields: []string{"location_code"}, KeepID: true},
	"LandedCostVoucher": {SeriesKey: "LCV", NumberFields: []string{"code"}, KeepID: true},
}

// DocumentNumber (Stage 76.11) is the number a person reads for a document
// kept under an internal id - its code - or the id when it has none.
func DocumentNumber(tenantID, doctype, id string) string {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return id
	}
	var n string
	if err := db.DB.QueryRow(fmt.Sprintf(`SELECT COALESCE(NULLIF(data->>'code', ''), id) FROM %s.documents WHERE doctype = $1 AND id = $2`, schema), doctype, id).Scan(&n); err != nil || n == "" {
		return id
	}
	return n
}

// issueNumberInto (Stage 76.11) writes IssueDocumentNumber's result into
// every number field of the doctype, so an engine fills the same fields a
// record-form create would.
func issueNumberInto(doc map[string]interface{}, tenantID, doctype, internalID, sessionStore string, documentStores ...string) {
	n := IssueDocumentNumber(tenantID, doctype, internalID, sessionStore, documentStores...)
	for _, f := range documentNumberSeriesByDoctype[doctype].NumberFields {
		doc[f] = n
	}
}

// IssueDocumentNumber (Stage 76.9) draws the printed number of a document an
// engine creates under an internal id of its own (the id stays the key), from
// the doctype's series at the document's store. A series that cannot issue
// (switched off) leaves fallback - today's internal number - and logs why, so
// a cascade such as a shipment's invoice never stops on numbering.
func IssueDocumentNumber(tenantID, doctype, fallback, sessionStore string, documentStores ...string) string {
	series, ok := documentNumberSeriesByDoctype[doctype]
	if !ok {
		return fallback
	}
	n, err := GenerateSequence(tenantID, series.SeriesKey, numberingStore(tenantID, sessionStore, documentStores...), documentFinancialYear(time.Now()))
	if err != nil || n == "" {
		LogSystemError(tenantID, "", "ERROR", "document-numbering", fmt.Sprintf("%s %s kept its internal number: the %s series could not issue one: %v", doctype, fallback, series.SeriesKey, err), "")
		return fallback
	}
	return n
}

// DocumentStoreCode (Stage 76.8) returns the first candidate that names a
// store - a Location, by id or code - as that Location's code, or "" when none
// does. A value that is not a store never reaches a number; the caller falls
// back to the maker's session store, as before.
func DocumentStoreCode(tenantID string, candidates ...string) string {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return ""
	}
	for _, c := range candidates {
		c = strings.TrimSpace(c)
		if c == "" {
			continue
		}
		var code string
		if err := db.DB.QueryRow(fmt.Sprintf(`SELECT COALESCE(NULLIF(TRIM(data->>'code'), ''), id) FROM %s.documents
			WHERE doctype = 'Location' AND deleted_at IS NULL AND (id = $1 OR data->>'code' = $1) LIMIT 1`, schema), c).Scan(&code); err == nil && code != "" {
			return code
		}
	}
	return ""
}

// numberingStore is the store a generated number is drawn from: the
// document's own (DocumentStoreCode), else the session's, else "HQ".
func numberingStore(tenantID, sessionStore string, documentStores ...string) string {
	if s := DocumentStoreCode(tenantID, documentStores...); s != "" {
		return s
	}
	if s := strings.TrimSpace(sessionStore); s != "" {
		return s
	}
	return "HQ"
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

	// Store segment (Stage 76.8): the document's own store; else the maker's
	// resolved location; "HQ" for a session with no location (an HR/Admin
	// acting centrally) - the number still has to come from somewhere
	// deterministic.
	own := make([]string, 0, len(series.StoreFields))
	for _, f := range series.StoreFields {
		own = append(own, strField(payload, f))
	}
	storeCode := numberingStore(tenantID, location, own...)

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

	if !series.KeepID {
		payload["id"] = code
	}
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
