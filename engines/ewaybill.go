package engines

import (
	"context"
	"crypto/sha256"
	"custom_erp/db"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"
)

// Stage 130.16 (74.4.1-74.4.7, the e-way bill side; the IRN side is 75.2):
// the e-way bill, provider-free where it can be.
//
//   - Decision: each delivery challan, sales invoice and stock transfer gets
//     its own decision - consignment value against gst.ewb_threshold, goods
//     only (services and the client's exempt HSNs, DEC-16, are left out),
//     job work to another state whatever the value - stored with its rule
//     version (74.4.1).
//   - The e-way bill's status lives in ewb_documents, apart from the
//     document's: NotRequired, Ready, Submitted, Generated, Failed, Expired,
//     Cancelled, Reconciled (74.4.4).
//   - Before anything goes out: the consignor's GSTIN and PIN, the state
//     codes, HSN on every line, a 16-character number, the distance and Part
//     B (a vehicle number or the transporter's ID) (74.4.2).
//   - Generate, update the vehicle (Part B), extend near expiry, cancel within
//     24 hours; validity is a day per 200 km and is watched (74.4.6).
//   - A provider sits behind EWBProvider (the IRP pattern): a timeout is
//     looked up by document before any retry, never resubmitted blind. Only a
//     test provider exists until credentials (20.30 / 20.31); until then an
//     e-way bill made on the government portal is recorded here by its
//     number, so the register and the expiry watch still hold (74.4.3).
//   - The workbench (Required / Generated / Expiring / Failed), the R-TX-10
//     register and reconciliation against the provider (74.4.7).

const (
	SettingEWBProvider   = "gst.ewb_provider"
	SettingEWBExemptHSN  = "gst.ewb_exempt_hsn"
	ewbDecisionVersion   = "EWB-R138-2026.1"
	EWBNotRequired       = "NotRequired"
	EWBReady             = "Ready"
	EWBSubmitted         = "Submitted"
	EWBGenerated         = "Generated"
	EWBFailed            = "Failed"
	EWBExpired           = "Expired"
	EWBCancelled         = "Cancelled"
	EWBReconciled        = "Reconciled"
	ewbKmPerDay          = 200
	ewbExtendWindowHours = 8
)

var ewbDoctypes = []string{DeliveryChallanDoctype, "SalesInvoice", "TransferOrder"}

func init() {
	RegisterSetting(SettingDefinition{
		Key: SettingEWBProvider, Module: "Inventory",
		Label: "E-way bill provider", Type: SettingTypeSelect, Default: "",
		Options:     []SettingOption{{Value: "", Label: "None - make e-way bills on the government portal and record their numbers here"}, {Value: "fake", Label: "Test provider (no real e-way bills - for training and testing)"}},
		Description: "Who generates e-way bills from the E-Way Bills screen. Until a provider is connected, generate each bill on the e-way bill portal and use Record from portal so the register and the expiry watch still know it.",
	})
	RegisterSetting(SettingDefinition{
		Key: SettingEWBExemptHSN, Module: "Inventory",
		Label: "HSN codes that need no e-way bill", Type: SettingTypeString, Default: "",
		Description: "Comma-separated HSN codes (or their first digits) your goods are exempt for (Rule 138(14)); their value is left out of the consignment value. Services (HSN 99...) never need one.",
	})
	RegisterReport(ReportDefinition{ID: "ewaybill-register", Label: "E-way bill register", Category: "Inventory",
		Params: []ReportParam{{Key: "from", Label: "From", Type: "date"}, {Key: "to", Label: "To", Type: "date"}},
		Columns: []ReportColumn{{Key: "date", Label: "Date"}, {Key: "document", Label: "Document"}, {Key: "doctype", Label: "Type"}, {Key: "from", Label: "From"},
			{Key: "to", Label: "To"}, {Key: "value", Label: "Value"}, {Key: "status", Label: "E-way bill"}, {Key: "ewb_no", Label: "EWB no"},
			{Key: "valid_upto", Label: "Valid until"}, {Key: "vehicle_no", Label: "Vehicle"}, {Key: "reason", Label: "Why"}},
		Run: ewbRegister, StoreKey: "-"})
}

func ewbFail(msg string) error {
	return &ValidationError{Code: "TAXGST-1320", SubFor: "e-way bill", Message: msg}
}

// --- the provider -------------------------------------------------------------

// EWBRequest is what goes to the provider.
type EWBRequest struct {
	GSTIN         string   `json:"gstin"`
	DocType       string   `json:"doc_type"`
	DocNumber     string   `json:"doc_number"`
	DocDate       string   `json:"doc_date"`
	ToGSTIN       string   `json:"to_gstin"`
	FromState     string   `json:"from_state"`
	ToState       string   `json:"to_state"`
	FromPin       string   `json:"from_pin"`
	ToPin         string   `json:"to_pin"`
	Value         float64  `json:"value"`
	DistanceKm    int      `json:"distance_km"`
	TransportMode string   `json:"transport_mode"`
	VehicleNo     string   `json:"vehicle_no"`
	TransporterID string   `json:"transporter_id"`
	HSN           []string `json:"hsn"`
}

// EWBAck is a generated (or found) e-way bill.
type EWBAck struct {
	EWBNo     string
	EWBDate   time.Time
	ValidUpto time.Time
}

// EWBError is a classified provider error (the IRP kinds).
type EWBError struct {
	Kind    IRPErrorKind
	Message string
}

func (e *EWBError) Error() string { return fmt.Sprintf("%s: %s", e.Kind, e.Message) }

// EWBProvider is one e-way bill system or GSP.
type EWBProvider interface {
	Code() string
	Generate(ctx context.Context, req EWBRequest) (EWBAck, error)
	GetByDocument(ctx context.Context, gstin, docType, docNumber, docDate string) (EWBAck, error)
	UpdatePartB(ctx context.Context, gstin, ewbNo, vehicleNo, transporterID string) error
	Extend(ctx context.Context, gstin, ewbNo string, remainingKm int, reason string) (time.Time, error)
	Cancel(ctx context.Context, gstin, ewbNo, reason string) error
}

var (
	ewbProvidersMu sync.RWMutex
	ewbProviders   = map[string]EWBProvider{}
)

// RegisterEWBProvider adds a provider by its code.
func RegisterEWBProvider(p EWBProvider) {
	ewbProvidersMu.Lock()
	defer ewbProvidersMu.Unlock()
	ewbProviders[strings.ToLower(p.Code())] = p
}

func ewbProviderFor(tenantID string) EWBProvider {
	code := strings.ToLower(strings.TrimSpace(GetSettingString(tenantID, SettingEWBProvider)))
	ewbProvidersMu.RLock()
	defer ewbProvidersMu.RUnlock()
	return ewbProviders[code]
}

func init() { RegisterEWBProvider(&fakeEWB{bills: map[string]EWBAck{}}) }

// fakeEWB is the test provider: it numbers bills from the document, refuses a
// vehicle number "FAIL", and times out (having accepted) on "TIMEOUT".
type fakeEWB struct {
	mu    sync.Mutex
	bills map[string]EWBAck
}

func (f *fakeEWB) Code() string { return "fake" }

func (f *fakeEWB) key(gstin, docType, docNumber string) string {
	return gstin + "|" + docType + "|" + docNumber
}

func (f *fakeEWB) Generate(_ context.Context, req EWBRequest) (EWBAck, error) {
	if strings.EqualFold(req.VehicleNo, "FAIL") {
		return EWBAck{}, &EWBError{Kind: IRPRejected, Message: "vehicle number is not valid"}
	}
	sum := sha256.Sum256([]byte(f.key(req.GSTIN, req.DocType, req.DocNumber)))
	n := strings.Map(func(r rune) rune {
		if r >= '0' && r <= '9' {
			return r
		}
		return -1
	}, hex.EncodeToString(sum[:]))
	now := time.Now()
	ack := EWBAck{EWBNo: (n + "000000000000")[:12], EWBDate: now, ValidUpto: ewbValidUpto(now, req.DistanceKm)}
	f.mu.Lock()
	f.bills[f.key(req.GSTIN, req.DocType, req.DocNumber)] = ack
	f.mu.Unlock()
	if strings.EqualFold(req.VehicleNo, "TIMEOUT") {
		return EWBAck{}, &EWBError{Kind: IRPMaybeAccepted, Message: "no answer in time"}
	}
	return ack, nil
}

func (f *fakeEWB) GetByDocument(_ context.Context, gstin, docType, docNumber, _ string) (EWBAck, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if a, ok := f.bills[f.key(gstin, docType, docNumber)]; ok {
		return a, nil
	}
	return EWBAck{}, &EWBError{Kind: IRPNotFound, Message: "no e-way bill for that document"}
}

func (f *fakeEWB) UpdatePartB(_ context.Context, _, _, vehicleNo, _ string) error {
	if strings.EqualFold(vehicleNo, "FAIL") {
		return &EWBError{Kind: IRPRejected, Message: "vehicle number is not valid"}
	}
	return nil
}

func (f *fakeEWB) Extend(_ context.Context, _, _ string, remainingKm int, _ string) (time.Time, error) {
	return ewbValidUpto(time.Now(), remainingKm), nil
}

func (f *fakeEWB) Cancel(context.Context, string, string, string) error { return nil }

// ewbValidUpto is a day per 200 km (or part), ending at midnight.
func ewbValidUpto(from time.Time, km int) time.Time {
	days := int(math.Ceil(float64(maxInt(km, 1)) / ewbKmPerDay))
	y, m, d := from.Date()
	return time.Date(y, m, d, 23, 59, 59, 0, from.Location()).AddDate(0, 0, days)
}

// --- the record -----------------------------------------------------------------

// EWBRecord is one document's e-way bill.
type EWBRecord struct {
	ID            int                    `json:"id"`
	Doctype       string                 `json:"doctype"`
	DocID         string                 `json:"doc_id"`
	DocNumber     string                 `json:"doc_number"`
	GSTIN         string                 `json:"gstin"`
	Status        string                 `json:"status"`
	Decision      map[string]interface{} `json:"decision"`
	Value         float64                `json:"value"`
	FromState     string                 `json:"from_state"`
	ToState       string                 `json:"to_state"`
	DistanceKm    int                    `json:"distance_km"`
	TransportMode string                 `json:"transport_mode"`
	VehicleNo     string                 `json:"vehicle_no"`
	TransporterID string                 `json:"transporter_id"`
	EWBNo         string                 `json:"ewb_no"`
	EWBDate       *time.Time             `json:"ewb_date,omitempty"`
	ValidUpto     *time.Time             `json:"valid_upto,omitempty"`
	Extensions    int                    `json:"extensions"`
	Source        string                 `json:"source"`
	LastError     string                 `json:"last_error,omitempty"`
	CreatedAt     time.Time              `json:"created_at"`
	Expiring      bool                   `json:"expiring"`
}

const ewbColumns = `id, doctype, doc_id, COALESCE(doc_number, ''), COALESCE(gstin, ''), status, COALESCE(decision::text, '{}'), COALESCE(value, 0),
	COALESCE(from_state, ''), COALESCE(to_state, ''), COALESCE(distance_km, 0), COALESCE(transport_mode, ''), COALESCE(vehicle_no, ''), COALESCE(transporter_id, ''),
	COALESCE(ewb_no, ''), ewb_date, valid_upto, extensions, COALESCE(source, ''), COALESCE(last_error, ''), created_at`

func scanEWB(row interface{ Scan(...interface{}) error }) (EWBRecord, error) {
	var r EWBRecord
	var decision string
	var ewbDate, validUpto sql.NullTime
	if err := row.Scan(&r.ID, &r.Doctype, &r.DocID, &r.DocNumber, &r.GSTIN, &r.Status, &decision, &r.Value, &r.FromState, &r.ToState, &r.DistanceKm,
		&r.TransportMode, &r.VehicleNo, &r.TransporterID, &r.EWBNo, &ewbDate, &validUpto, &r.Extensions, &r.Source, &r.LastError, &r.CreatedAt); err != nil {
		return r, err
	}
	_ = json.Unmarshal([]byte(decision), &r.Decision)
	if ewbDate.Valid {
		t := ewbDate.Time
		r.EWBDate = &t
	}
	if validUpto.Valid {
		t := validUpto.Time
		r.ValidUpto = &t
		r.Expiring = r.Status == EWBGenerated && time.Until(t) < ewbExtendWindowHours*time.Hour && time.Until(t) > -ewbExtendWindowHours*time.Hour
	}
	return r, nil
}

func loadEWB(q queryRower, schema, doctype, docID string) (*EWBRecord, error) {
	r, err := scanEWB(q.QueryRow(fmt.Sprintf(`SELECT %s FROM %s.ewb_documents WHERE doctype = $1 AND doc_id = $2`, ewbColumns, schema), doctype, docID))
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &r, nil
}

// ewbFacts is what a document says about its consignment.
type ewbFacts struct {
	number, date, fromStore, toGSTIN, toState, toPin, purpose string
	value                                                     float64
	hsn                                                       []string
	servicesOnly                                              bool
}

func ewbExempt(tenantID, hsn string) bool {
	hsn = strings.TrimSpace(hsn)
	if strings.HasPrefix(hsn, "99") {
		return true
	}
	for _, p := range strings.Split(GetSettingString(tenantID, SettingEWBExemptHSN), ",") {
		if p = strings.TrimSpace(p); p != "" && strings.HasPrefix(hsn, p) {
			return true
		}
	}
	return false
}

func ewbFactsFor(tenantID, doctype, docID string) (*ewbFacts, string, error) {
	data, status, err := fetchDocData(tenantID, doctype, docID)
	if err != nil || data == nil {
		return nil, "", ewbFail(fmt.Sprintf("%s %s was not found", doctype, docID))
	}
	f := &ewbFacts{servicesOnly: true}
	addLine := func(sku, hsn string, value float64) {
		if hsn == "" {
			if item, _, err := fetchDocData(tenantID, "Item", sku); err == nil && item != nil {
				hsn = strField(item, "hsn_code")
			}
		}
		f.hsn = append(f.hsn, hsn)
		if ewbExempt(tenantID, hsn) && hsn != "" {
			return
		}
		f.servicesOnly = false
		f.value = round2(f.value + value)
	}
	switch doctype {
	case DeliveryChallanDoctype:
		f.number, f.date, f.fromStore = firstNonEmpty(strField(data, "code"), docID), firstNonEmpty(strField(data, "dispatch_date"), BusinessToday()), strField(data, "location")
		f.toGSTIN, f.toState, f.purpose = strField(data, "party_gstin"), stateCodeOf(strField(data, "party_gstin"), strField(data, "party_state")), strField(data, "purpose")
		for _, l := range jsonLines(data["items"]) {
			addLine(strField(l, "sku"), strField(l, "hsn_code"), numFromInterface(l["declared_value"]))
		}
	case "SalesInvoice":
		f.number, f.date, f.fromStore = firstNonEmpty(strField(data, "invoice_number"), docID), firstNonEmpty(strField(data, "invoice_date"), BusinessToday()), strField(data, "location")
		f.toGSTIN, f.toState = strField(data, "customer_gstin"), firstNonEmpty(strField(data, "place_of_supply"), stateCodeOf(strField(data, "customer_gstin"), ""))
		lines := jsonLines(data["items"])
		if len(lines) == 0 {
			f.value, f.servicesOnly = numFromInterface(data["total_amount"]), false
		}
		for _, l := range lines {
			addLine(strField(l, "sku"), strField(l, "hsn_code"), firstPositive(numFromInterface(l["amount"]), numFromInterface(l["taxable"])))
		}
	case "TransferOrder":
		f.number, f.date, f.fromStore = firstNonEmpty(strField(data, "transfer_number"), strField(data, "code"), docID), BusinessToday(), strField(data, "from_warehouse")
		to := LoadPrintStore(tenantID, strField(data, "to_warehouse"))
		f.toGSTIN, f.toState, f.toPin = to.GSTIN, firstNonEmpty(to.StateCode, stateCodeOf(to.GSTIN, "")), to.Pincode
		v, _ := GetTransferValue(tenantID, docID)
		f.value, f.servicesOnly = v.Value, false
		for _, l := range jsonLines(data["items"]) {
			f.hsn = append(f.hsn, strField(printItemFields(tenantID, strField(l, "sku"), map[string]map[string]interface{}{}), "hsn_code"))
		}
	default:
		return nil, status, ewbFail(doctype + " does not carry an e-way bill")
	}
	return f, status, nil
}

// EWBDecisionFor works out whether a document needs an e-way bill.
func EWBDecisionFor(tenantID, doctype, docID string) (map[string]interface{}, *ewbFacts, error) {
	f, _, err := ewbFactsFor(tenantID, doctype, docID)
	if err != nil {
		return nil, nil, err
	}
	threshold := GetSettingFloat(tenantID, SettingEWBThreshold)
	from := LoadPrintStore(tenantID, f.fromStore)
	fromState := firstNonEmpty(from.StateCode, stateCodeOf(from.GSTIN, ""))
	interstate := fromState != "" && f.toState != "" && fromState != f.toState
	d := map[string]interface{}{"rule_version": ewbDecisionVersion, "value": f.value, "threshold": threshold, "interstate": interstate, "required": false}
	switch {
	case f.servicesOnly:
		d["reason"] = "services or exempt goods only"
	case f.purpose == "Job work" && interstate:
		d["required"], d["reason"] = true, "job work sent to another state needs an e-way bill whatever the value"
	case f.value >= threshold:
		d["required"], d["reason"] = true, fmt.Sprintf("goods worth %.2f, at or over %.2f", f.value, threshold)
	default:
		d["reason"] = fmt.Sprintf("goods worth %.2f, under %.2f", f.value, threshold)
	}
	return d, f, nil
}

// EnsureEWBRecord records a document's decision (once; a Ready record is
// re-decided while nothing has gone out).
func EnsureEWBRecord(tenantID, doctype, docID string) (*EWBRecord, error) {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return nil, err
	}
	existing, err := loadEWB(db.DB, schema, doctype, docID)
	if err != nil {
		return nil, err
	}
	if existing != nil && existing.Status != EWBReady && existing.Status != EWBNotRequired {
		return existing, nil
	}
	decision, f, err := EWBDecisionFor(tenantID, doctype, docID)
	if err != nil {
		return nil, err
	}
	status := EWBNotRequired
	if decision["required"] == true {
		status = EWBReady
	}
	from := LoadPrintStore(tenantID, f.fromStore)
	raw, _ := json.Marshal(decision)
	if _, err := db.DB.Exec(fmt.Sprintf(`INSERT INTO %s.ewb_documents (doctype, doc_id, doc_number, gstin, status, decision, value, from_state, to_state)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
		ON CONFLICT (doctype, doc_id) DO UPDATE SET doc_number = EXCLUDED.doc_number, gstin = EXCLUDED.gstin, status = EXCLUDED.status, decision = EXCLUDED.decision,
		  value = EXCLUDED.value, from_state = EXCLUDED.from_state, to_state = EXCLUDED.to_state, updated_at = CURRENT_TIMESTAMP
		WHERE %s.ewb_documents.status IN ('Ready', 'NotRequired')`, schema, schema),
		doctype, docID, f.number, from.GSTIN, status, string(raw), f.value, firstNonEmpty(from.StateCode, stateCodeOf(from.GSTIN, "")), f.toState); err != nil {
		return nil, err
	}
	return loadEWB(db.DB, schema, doctype, docID)
}

var vehicleNoPattern = regexp.MustCompile(`^[A-Z]{2}[0-9]{1,2}[A-Z]{0,3}[0-9]{4}$`)

// EWBPartB is the transport of a generation or a vehicle update.
type EWBPartB struct {
	TransportMode string `json:"transport_mode"`
	VehicleNo     string `json:"vehicle_no"`
	TransporterID string `json:"transporter_id"`
	DistanceKm    int    `json:"distance_km"`
}

// validateEWB is 74.4.2: everything the bill needs, checked here first.
func validateEWB(tenantID string, f *ewbFacts, from PrintStore, b EWBPartB) []string {
	var problems []string
	if from.GSTIN == "" {
		problems = append(problems, "the store's legal entity has no GSTIN")
	}
	if from.Pincode == "" {
		problems = append(problems, "the store has no PIN code")
	}
	if f.toState == "" {
		problems = append(problems, "the receiver's state is not known")
	}
	if n := len([]rune(f.number)); n == 0 || n > 16 {
		problems = append(problems, fmt.Sprintf("the document number %q is not 1-16 characters", f.number))
	}
	for _, h := range f.hsn {
		if h == "" {
			problems = append(problems, "a line has no HSN code")
			break
		}
	}
	if f.value <= 0 {
		problems = append(problems, "the consignment has no value")
	}
	if b.DistanceKm <= 0 {
		problems = append(problems, "enter the distance in km")
	}
	vehicle := strings.ToUpper(strings.ReplaceAll(b.VehicleNo, " ", ""))
	switch {
	case vehicle != "" && !vehicleNoPattern.MatchString(vehicle) && vehicle != "FAIL" && vehicle != "TIMEOUT":
		problems = append(problems, fmt.Sprintf("vehicle number %q does not look like one (e.g. KA01AB1234)", b.VehicleNo))
	case vehicle == "" && len(strings.TrimSpace(b.TransporterID)) != 15:
		problems = append(problems, "give the vehicle number, or the transporter's 15-character ID")
	}
	return problems
}

// GenerateEWB generates a document's e-way bill through the provider. A
// timeout is looked up by document before it is ever sent again.
func GenerateEWB(tenantID, doctype, docID, userID string, b EWBPartB) (*EWBRecord, error) {
	rec, err := EnsureEWBRecord(tenantID, doctype, docID)
	if err != nil {
		return nil, err
	}
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return nil, err
	}
	switch rec.Status {
	case EWBGenerated, EWBReconciled:
		return rec, nil
	case EWBNotRequired:
		return rec, ewbFail("this document does not need an e-way bill (" + strField(rec.Decision, "reason") + ")")
	case EWBCancelled, EWBExpired:
		return rec, ewbFail("this e-way bill is " + strings.ToLower(rec.Status) + " - a new one is a new document")
	}
	p := ewbProviderFor(tenantID)
	if p == nil {
		return rec, ewbFail("no e-way bill provider is connected - make the bill on the e-way bill portal and use Record from portal")
	}
	_, f, err := EWBDecisionFor(tenantID, doctype, docID)
	if err != nil {
		return rec, err
	}
	from := LoadPrintStore(tenantID, f.fromStore)
	if problems := validateEWB(tenantID, f, from, b); len(problems) > 0 {
		_ = ewbSave(schema, rec.ID, map[string]interface{}{"last_error": strings.Join(problems, "; ")})
		return rec, ewbFail("fix before generating: " + strings.Join(problems, "; "))
	}
	req := EWBRequest{GSTIN: from.GSTIN, DocType: doctype, DocNumber: f.number, DocDate: f.date, ToGSTIN: f.toGSTIN, FromState: rec.FromState, ToState: f.toState,
		FromPin: from.Pincode, ToPin: f.toPin, Value: f.value, DistanceKm: b.DistanceKm, TransportMode: firstNonEmpty(b.TransportMode, "Road"),
		VehicleNo: strings.ToUpper(strings.ReplaceAll(b.VehicleNo, " ", "")), TransporterID: strings.TrimSpace(b.TransporterID), HSN: f.hsn}
	payload, _ := json.Marshal(req)
	sum := sha256.Sum256(payload)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	patch := map[string]interface{}{"distance_km": b.DistanceKm, "transport_mode": req.TransportMode, "vehicle_no": req.VehicleNo, "transporter_id": req.TransporterID,
		"payload_hash": hex.EncodeToString(sum[:]), "source": p.Code(), "attempts": "+1"}
	if rec.Status == EWBSubmitted {
		// It may already exist: look before sending again.
		if ack, err := p.GetByDocument(ctx, req.GSTIN, doctype, req.DocNumber, req.DocDate); err == nil {
			return ewbGenerated(schema, rec, ack, patch, userID, tenantID)
		}
	}
	ack, err := p.Generate(ctx, req)
	if err != nil {
		var e *EWBError
		kind := IRPProviderFailed
		if errors.As(err, &e) {
			kind = e.Kind
		}
		patch["last_error"] = err.Error()
		if kind == IRPMaybeAccepted || kind == IRPRetryLater {
			patch["status"] = EWBSubmitted
			_ = ewbSave(schema, rec.ID, patch)
			if found, lerr := p.GetByDocument(ctx, req.GSTIN, doctype, req.DocNumber, req.DocDate); lerr == nil {
				return ewbGenerated(schema, rec, found, patch, userID, tenantID)
			}
			return loadEWB(db.DB, schema, doctype, docID)
		}
		patch["status"] = EWBFailed
		_ = ewbSave(schema, rec.ID, patch)
		out, _ := loadEWB(db.DB, schema, doctype, docID)
		return out, ewbFail("the e-way bill was refused: " + err.Error())
	}
	return ewbGenerated(schema, rec, ack, patch, userID, tenantID)
}

func ewbGenerated(schema string, rec *EWBRecord, ack EWBAck, patch map[string]interface{}, userID, tenantID string) (*EWBRecord, error) {
	patch["status"], patch["ewb_no"], patch["ewb_date"], patch["valid_upto"], patch["last_error"] = EWBGenerated, ack.EWBNo, ack.EWBDate.UTC(), ack.ValidUpto.UTC(), ""
	if err := ewbSave(schema, rec.ID, patch); err != nil {
		return rec, err
	}
	LogAuditEvent(tenantID, userID, "EWB_GENERATED", "SUCCESS", fmt.Sprintf("%s %s: e-way bill %s", rec.Doctype, rec.DocNumber, ack.EWBNo))
	return loadEWB(db.DB, schema, rec.Doctype, rec.DocID)
}

// ewbSave updates a record's columns ("attempts": "+1" counts a try).
func ewbSave(schema string, id int, patch map[string]interface{}) error {
	var sets []string
	var args []interface{}
	keys := make([]string, 0, len(patch))
	for k := range patch {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		if k == "attempts" {
			sets = append(sets, "attempts = attempts + 1")
			continue
		}
		args = append(args, patch[k])
		sets = append(sets, fmt.Sprintf("%s = $%d", k, len(args)))
	}
	args = append(args, id)
	_, err := db.DB.Exec(fmt.Sprintf(`UPDATE %s.ewb_documents SET %s, updated_at = CURRENT_TIMESTAMP WHERE id = $%d`, schema, strings.Join(sets, ", "), len(args)), args...)
	return err
}

// RecordPortalEWB records an e-way bill made on the government portal.
func RecordPortalEWB(tenantID, doctype, docID, userID, ewbNo string, validUpto time.Time, b EWBPartB) (*EWBRecord, error) {
	ewbNo = strings.TrimSpace(ewbNo)
	if len(ewbNo) != 12 || strings.Trim(ewbNo, "0123456789") != "" {
		return nil, ewbFail("an e-way bill number is 12 digits")
	}
	if validUpto.Before(time.Now()) {
		return nil, ewbFail("that e-way bill has already expired")
	}
	rec, err := EnsureEWBRecord(tenantID, doctype, docID)
	if err != nil {
		return nil, err
	}
	if rec.Status == EWBGenerated || rec.Status == EWBReconciled {
		return rec, ewbFail("this document already has e-way bill " + rec.EWBNo)
	}
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return nil, err
	}
	return ewbGenerated(schema, rec, EWBAck{EWBNo: ewbNo, EWBDate: time.Now(), ValidUpto: validUpto},
		map[string]interface{}{"source": "portal", "distance_km": b.DistanceKm, "vehicle_no": strings.ToUpper(strings.TrimSpace(b.VehicleNo)), "transport_mode": firstNonEmpty(b.TransportMode, "Road")}, userID, tenantID)
}

func ewbGeneratedRecord(tenantID, doctype, docID string) (*EWBRecord, string, error) {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return nil, "", err
	}
	rec, err := loadEWB(db.DB, schema, doctype, docID)
	if err != nil {
		return nil, schema, err
	}
	if rec == nil || (rec.Status != EWBGenerated && rec.Status != EWBReconciled) {
		return rec, schema, ewbFail("this document has no live e-way bill")
	}
	return rec, schema, nil
}

// UpdateEWBVehicle is Part B: the vehicle (or transporter) changed on the way.
func UpdateEWBVehicle(tenantID, doctype, docID, userID string, b EWBPartB) (*EWBRecord, error) {
	rec, schema, err := ewbGeneratedRecord(tenantID, doctype, docID)
	if err != nil {
		return rec, err
	}
	vehicle := strings.ToUpper(strings.ReplaceAll(b.VehicleNo, " ", ""))
	if !vehicleNoPattern.MatchString(vehicle) && len(strings.TrimSpace(b.TransporterID)) != 15 {
		return rec, ewbFail("give the new vehicle number (e.g. KA01AB1234) or the transporter's 15-character ID")
	}
	if rec.Source != "portal" {
		if p := ewbProviderFor(tenantID); p != nil {
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()
			if err := p.UpdatePartB(ctx, rec.GSTIN, rec.EWBNo, vehicle, b.TransporterID); err != nil {
				return rec, ewbFail("the vehicle update was refused: " + err.Error())
			}
		}
	}
	if err := ewbSave(schema, rec.ID, map[string]interface{}{"vehicle_no": vehicle, "transporter_id": strings.TrimSpace(b.TransporterID)}); err != nil {
		return rec, err
	}
	LogAuditEvent(tenantID, userID, "EWB_VEHICLE", "SUCCESS", fmt.Sprintf("e-way bill %s: vehicle %s", rec.EWBNo, vehicle))
	return loadEWB(db.DB, schema, doctype, docID)
}

// ExtendEWB extends a bill within 8 hours either side of its expiry, for the
// distance still to go.
func ExtendEWB(tenantID, doctype, docID, userID string, remainingKm int, reason string) (*EWBRecord, error) {
	rec, schema, err := ewbGeneratedRecord(tenantID, doctype, docID)
	if err != nil && !(rec != nil && rec.Status == EWBExpired) {
		return rec, err
	}
	if rec.ValidUpto == nil || math.Abs(time.Until(*rec.ValidUpto).Hours()) > ewbExtendWindowHours {
		return rec, ewbFail("an e-way bill is extended only within 8 hours before or after it expires")
	}
	if remainingKm <= 0 || strings.TrimSpace(reason) == "" {
		return rec, ewbFail("give the distance still to go and why")
	}
	until := ewbValidUpto(time.Now(), remainingKm)
	if p := ewbProviderFor(tenantID); p != nil && rec.Source != "portal" {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		if until, err = p.Extend(ctx, rec.GSTIN, rec.EWBNo, remainingKm, reason); err != nil {
			return rec, ewbFail("the extension was refused: " + err.Error())
		}
	}
	if _, err := db.DB.Exec(fmt.Sprintf(`UPDATE %s.ewb_documents SET valid_upto = $2, extensions = extensions + 1, status = 'Generated', updated_at = CURRENT_TIMESTAMP WHERE id = $1`, schema),
		rec.ID, until.UTC()); err != nil {
		return rec, err
	}
	LogAuditEvent(tenantID, userID, "EWB_EXTEND", "SUCCESS", fmt.Sprintf("e-way bill %s extended to %s: %s", rec.EWBNo, until.Format("2006-01-02 15:04"), reason))
	return loadEWB(db.DB, schema, doctype, docID)
}

// CancelEWB cancels a bill within 24 hours of its generation.
func CancelEWB(tenantID, doctype, docID, userID, reason string) (*EWBRecord, error) {
	rec, schema, err := ewbGeneratedRecord(tenantID, doctype, docID)
	if err != nil {
		return rec, err
	}
	if strings.TrimSpace(reason) == "" {
		return rec, ewbFail("say why the e-way bill is cancelled")
	}
	if rec.EWBDate != nil && time.Since(*rec.EWBDate) > 24*time.Hour {
		return rec, ewbFail("an e-way bill can be cancelled only within 24 hours of its generation")
	}
	if p := ewbProviderFor(tenantID); p != nil && rec.Source != "portal" {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		if err := p.Cancel(ctx, rec.GSTIN, rec.EWBNo, reason); err != nil {
			return rec, ewbFail("the cancellation was refused: " + err.Error())
		}
	}
	if err := ewbSave(schema, rec.ID, map[string]interface{}{"status": EWBCancelled, "last_error": "cancelled: " + strings.TrimSpace(reason)}); err != nil {
		return rec, err
	}
	LogAuditEvent(tenantID, userID, "EWB_CANCEL", "SUCCESS", fmt.Sprintf("e-way bill %s cancelled: %s", rec.EWBNo, reason))
	return loadEWB(db.DB, schema, doctype, docID)
}

// RefreshEWBQueue decides recent documents not yet decided, marks lapsed
// bills Expired, and (with a provider) reconciles generated ones. The
// workbench runs it; so can the daily job.
func RefreshEWBQueue(tenantID string) error {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return err
	}
	rows, err := db.DB.Query(fmt.Sprintf(`SELECT d.doctype, d.id FROM %s.documents d
		WHERE d.doctype = ANY($1) AND d.deleted_at IS NULL AND d.created_at > NOW() - INTERVAL '30 days'
		  AND d.status IN ('Approved', 'Dispatched', 'Partly Returned', 'Posted', 'Paid', 'Received', 'In Transit')
		  AND NOT EXISTS (SELECT 1 FROM %s.ewb_documents e WHERE e.doctype = d.doctype AND e.doc_id = d.id)
		LIMIT 500`, schema, schema), pqStringArray(ewbDoctypes))
	if err != nil {
		return err
	}
	var todo [][2]string
	for rows.Next() {
		var dt, id string
		if rows.Scan(&dt, &id) == nil {
			todo = append(todo, [2]string{dt, id})
		}
	}
	rows.Close()
	for _, t := range todo {
		_, _ = EnsureEWBRecord(tenantID, t[0], t[1])
	}
	if _, err := db.DB.Exec(fmt.Sprintf(`UPDATE %s.ewb_documents SET status = 'Expired', updated_at = CURRENT_TIMESTAMP WHERE status IN ('Generated', 'Reconciled') AND valid_upto < $1`, schema), time.Now().UTC()); err != nil {
		return err
	}
	return ReconcileEWBs(tenantID)
}

// ReconcileEWBs checks each provider-generated bill against the provider:
// found and matching is Reconciled; missing or different is an exception.
func ReconcileEWBs(tenantID string) error {
	p := ewbProviderFor(tenantID)
	if p == nil {
		return nil
	}
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return err
	}
	rows, err := db.DB.Query(fmt.Sprintf(`SELECT %s FROM %s.ewb_documents WHERE status = 'Generated' AND source = $1 LIMIT 500`, ewbColumns, schema), p.Code())
	if err != nil {
		return err
	}
	var recs []EWBRecord
	for rows.Next() {
		if r, err := scanEWB(rows); err == nil {
			recs = append(recs, r)
		}
	}
	rows.Close()
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	for _, r := range recs {
		ack, err := p.GetByDocument(ctx, r.GSTIN, r.Doctype, r.DocNumber, "")
		switch {
		case err != nil:
			_ = ewbSave(schema, r.ID, map[string]interface{}{"last_error": "not found at the provider: " + err.Error()})
		case ack.EWBNo != r.EWBNo:
			_ = ewbSave(schema, r.ID, map[string]interface{}{"last_error": fmt.Sprintf("the provider has e-way bill %s for this document, not %s", ack.EWBNo, r.EWBNo)})
		default:
			_ = ewbSave(schema, r.ID, map[string]interface{}{"status": EWBReconciled, "last_error": ""})
		}
	}
	return nil
}

// ListEWBs is the workbench: tab Required (Ready + Submitted), Generated,
// Expiring, Failed, NotRequired or All.
func ListEWBs(tenantID, tab string) ([]EWBRecord, map[string]int, error) {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return nil, nil, err
	}
	rows, err := db.DB.Query(fmt.Sprintf(`SELECT %s FROM %s.ewb_documents ORDER BY created_at DESC LIMIT 1000`, ewbColumns, schema))
	if err != nil {
		return nil, nil, err
	}
	defer rows.Close()
	counts := map[string]int{}
	out := []EWBRecord{}
	for rows.Next() {
		r, err := scanEWB(rows)
		if err != nil {
			return nil, nil, err
		}
		key := map[string]string{EWBReady: "required", EWBSubmitted: "required", EWBGenerated: "generated", EWBReconciled: "generated", EWBFailed: "failed",
			EWBNotRequired: "not_required", EWBExpired: "expired", EWBCancelled: "cancelled"}[r.Status]
		counts[key]++
		counts["all"]++
		if r.Expiring {
			counts["expiring"]++
		}
		if tab == "" || tab == "all" || tab == key || (tab == "expiring" && r.Expiring) {
			out = append(out, r)
		}
	}
	return out, counts, rows.Err()
}

func ewbRegister(tenantID string, p map[string]string) ([]map[string]interface{}, error) {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return nil, err
	}
	rows, err := db.DB.Query(fmt.Sprintf(`SELECT %s FROM %s.ewb_documents WHERE ($1 = '' OR created_at >= public.erp_day_start($1::date)) AND ($2 = '' OR created_at < public.erp_day_start($2::date + 1)) ORDER BY created_at DESC LIMIT 5000`,
		ewbColumns, schema), strings.TrimSpace(p["from"]), strings.TrimSpace(p["to"]))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []map[string]interface{}
	for rows.Next() {
		r, err := scanEWB(rows)
		if err != nil {
			return nil, err
		}
		row := map[string]interface{}{"date": r.CreatedAt.Format("2006-01-02"), "document": r.DocNumber, "doctype": r.Doctype, "from": r.FromState, "to": r.ToState,
			"value": r.Value, "status": r.Status, "ewb_no": r.EWBNo, "vehicle_no": r.VehicleNo, "reason": strField(r.Decision, "reason"), "valid_upto": ""}
		if r.ValidUpto != nil {
			row["valid_upto"] = r.ValidUpto.Local().Format("2006-01-02 15:04")
		}
		out = append(out, row)
	}
	return out, rows.Err()
}
