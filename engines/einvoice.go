package engines

import (
	"crypto/sha256"
	"custom_erp/db"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"regexp"
	"sort"
	"strings"
	"time"
)

// Stage 75.2: e-invoice (IRN) readiness - docs/product/einvoice-iris-irp-plan-2026-10-17.md.
//
// When a client must e-invoice, turning it on is configuration plus the IRP's
// production approval, not a build. Everything here runs against the fake
// provider (einvoice_provider.go) until the IRIS adapter (75.2.11) exists.
//
// This file: the GSTIN profiles and their eligibility rules (75.2.1), the
// canonical tax document frozen at posting (75.2.2) and its validation before
// submission (75.2.3). The queue and worker are einvoice_worker.go; locks,
// cancellation, the 30-day rule, reconciliation and reports are
// einvoice_controls.go.

// IRN states (75.2.6). Kept apart from the document's own status: an invoice
// is Approved or Paid whatever its IRN is doing.
const (
	IRPStateNotRequired   = "NOT_REQUIRED"
	IRPStateReady         = "READY"
	IRPStateQueued        = "QUEUED"
	IRPStateSubmitting    = "SUBMITTING"
	IRPStateAcknowledged  = "ACKNOWLEDGED"
	IRPStateRejected      = "REJECTED"
	IRPStateUnknown       = "UNKNOWN"
	IRPStateReconcile     = "RECONCILE"
	IRPStateCancelPending = "CANCEL_PENDING"
	IRPStateCancelled     = "CANCELLED"
	IRPStateCancelUnknown = "CANCEL_UNKNOWN"
	IRPStateManualReview  = "MANUAL_REVIEW"
	IRPStateManualImport  = "MANUAL_IMPORT"
	IRPStateBlockedConfig = "BLOCKED_CONFIG"
)

// EInvoiceDocTypes maps the ERP's outward tax documents onto the IRP's types.
// A debit note here is issued to a vendor (purchase side), so nothing in this
// ERP produces an outward DBN yet; the type is accepted on a profile for when
// one does.
var EInvoiceDocTypes = map[string]string{"SalesInvoice": "INV", "CreditNote": "CRN"}

// IRPEnvironment is this server's IRP environment: "production" only where
// IRP_ENVIRONMENT says so; anything else is the sandbox. It is part of every
// unique key, so a sandbox document can never be retried in production.
func IRPEnvironment() string {
	if strings.EqualFold(strings.TrimSpace(os.Getenv("IRP_ENVIRONMENT")), "production") {
		return "production"
	}
	return "sandbox"
}

// IRPProfile is one GSTIN's e-invoice set-up (irp_profiles).
type IRPProfile struct {
	ID            int64      `json:"id"`
	Environment   string     `json:"environment"`
	GSTIN         string     `json:"gstin"`
	LegalEntity   string     `json:"legal_entity"`
	Provider      string     `json:"provider"`
	Enabled       bool       `json:"enabled"`
	AutoSubmit    bool       `json:"auto_submit"`
	TurnoverBand  string     `json:"turnover_band"` // 5-10CR, 10CR+
	DocumentTypes []string   `json:"document_types"`
	SupplyTypes   []string   `json:"supply_types"`
	InterGSTIN    string     `json:"inter_gstin"` // REQUIRED / NOT_REQUIRED
	WhitelistedIP string     `json:"whitelisted_ip"`
	TestStatus    string     `json:"test_status"`
	TestedAt      *time.Time `json:"tested_at,omitempty"`
	EffectiveFrom string     `json:"effective_from"`
	RuleVersion   int        `json:"rule_version"`
	ActivatedAt   *time.Time `json:"activated_at,omitempty"`
	UpdatedBy     string     `json:"updated_by"`
	UpdatedAt     time.Time  `json:"updated_at"`
	// CredentialSet is whether the provider credentials are stored. The
	// credentials themselves never leave the server.
	CredentialSet bool `json:"credential_set"`
}

var irpTurnoverBands = map[string]bool{"5-10CR": true, "10CR+": true}
var irpSupplyTypes = map[string]bool{"B2B": true, "SEZWP": true, "SEZWOP": true, "EXPWP": true, "EXPWOP": true, "DEXP": true}
var irpDocTypes = map[string]bool{"INV": true, "CRN": true, "DBN": true}

const irpProfileColumns = `id, environment, gstin, legal_entity, provider, enabled, auto_submit, turnover_band, document_types,
	supply_types, inter_gstin, whitelisted_ip, test_status, tested_at, effective_from::text, rule_version, activated_at, updated_by, updated_at`

func scanIRPProfile(row interface{ Scan(...interface{}) error }) (IRPProfile, error) {
	var p IRPProfile
	var docTypes, supplyTypes string
	var tested, activated sql.NullTime
	err := row.Scan(&p.ID, &p.Environment, &p.GSTIN, &p.LegalEntity, &p.Provider, &p.Enabled, &p.AutoSubmit, &p.TurnoverBand,
		&docTypes, &supplyTypes, &p.InterGSTIN, &p.WhitelistedIP, &p.TestStatus, &tested, &p.EffectiveFrom, &p.RuleVersion,
		&activated, &p.UpdatedBy, &p.UpdatedAt)
	if err != nil {
		return p, err
	}
	p.DocumentTypes, p.SupplyTypes = splitCSVList(docTypes), splitCSVList(supplyTypes)
	if tested.Valid {
		p.TestedAt = &tested.Time
	}
	if activated.Valid {
		p.ActivatedAt = &activated.Time
	}
	return p, nil
}

func splitCSVList(s string) []string {
	out := []string{}
	for _, v := range strings.Split(s, ",") {
		if v = strings.ToUpper(strings.TrimSpace(v)); v != "" {
			out = append(out, v)
		}
	}
	return out
}

// irpCredentialChannel is where a GSTIN's provider credentials are stored,
// encrypted, in channel_credentials (the same store and key as the channel
// connectors).
func irpCredentialChannel(environment, gstin string) string {
	return "IRP:" + environment + ":" + gstin
}

// ListIRPProfiles lists this tenant's GSTIN profiles for this server's
// environment.
func ListIRPProfiles(tenantID string) ([]IRPProfile, error) {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return nil, err
	}
	rows, err := db.DB.Query(fmt.Sprintf(`SELECT %s FROM %s.irp_profiles WHERE environment = $1 ORDER BY gstin`, irpProfileColumns, schema), IRPEnvironment())
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []IRPProfile{}
	for rows.Next() {
		p, err := scanIRPProfile(rows)
		if err != nil {
			return nil, err
		}
		p.CredentialSet, _ = HasChannelCredential(tenantID, irpCredentialChannel(p.Environment, p.GSTIN))
		out = append(out, p)
	}
	return out, rows.Err()
}

// irpProfileFor is the profile of a GSTIN in this environment, or nil.
func irpProfileFor(schema, gstin string) (*IRPProfile, error) {
	p, err := scanIRPProfile(db.DB.QueryRow(fmt.Sprintf(`SELECT %s FROM %s.irp_profiles WHERE environment = $1 AND gstin = $2`, irpProfileColumns, schema), IRPEnvironment(), gstin))
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &p, nil
}

func einvoiceError(code, field, msg string) error {
	return &ValidationError{Code: code, SubFor: field, Message: msg}
}

// SaveIRPProfile creates or changes a GSTIN's profile. Every change to the
// rules raises rule_version, so each eligibility decision can say which rules
// it was made under; the change is recorded in irp_events.
func SaveIRPProfile(tenantID string, in IRPProfile, credentials map[string]string, userID string) (IRPProfile, error) {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return in, err
	}
	in.GSTIN = strings.ToUpper(strings.TrimSpace(in.GSTIN))
	if len(in.GSTIN) != 15 || !ValidGSTINChecksum(in.GSTIN) {
		return in, einvoiceError("EINV-0980", "GSTIN", fmt.Sprintf("%q is not a valid GSTIN (15 characters, check digit)", in.GSTIN))
	}
	in.Environment = IRPEnvironment()
	in.Provider = strings.ToLower(strings.TrimSpace(in.Provider))
	if in.Provider == "" {
		in.Provider = "fake"
	}
	if _, ok := irpProviderFor(in.Provider); !ok {
		return in, einvoiceError("EINV-0980", "Provider", fmt.Sprintf("provider %q is not available on this server - the IRIS adapter is built once sandbox access exists (75.2.11)", in.Provider))
	}
	if in.Provider == "fake" && in.Environment == "production" {
		return in, einvoiceError("EINV-0980", "Provider", "the test provider cannot be used in production")
	}
	if in.TurnoverBand == "" {
		in.TurnoverBand = "5-10CR"
	}
	if !irpTurnoverBands[in.TurnoverBand] {
		return in, einvoiceError("EINV-0980", "Turnover", "turnover must be 5-10CR or 10CR+")
	}
	for _, d := range in.DocumentTypes {
		if !irpDocTypes[d] {
			return in, einvoiceError("EINV-0980", "Document types", fmt.Sprintf("unknown document type %q (INV, CRN, DBN)", d))
		}
	}
	for _, s := range in.SupplyTypes {
		if !irpSupplyTypes[s] {
			return in, einvoiceError("EINV-0980", "Supply types", fmt.Sprintf("unknown supply type %q", s))
		}
	}
	if len(in.DocumentTypes) == 0 {
		in.DocumentTypes = []string{"INV", "CRN", "DBN"}
	}
	if len(in.SupplyTypes) == 0 {
		in.SupplyTypes = []string{"B2B", "SEZWP", "SEZWOP", "EXPWP", "EXPWOP", "DEXP"}
	}
	if in.InterGSTIN != "NOT_REQUIRED" {
		in.InterGSTIN = "REQUIRED"
	}
	if strings.TrimSpace(in.EffectiveFrom) == "" {
		in.EffectiveFrom = time.Now().Format("2006-01-02")
	}
	if _, err := time.Parse("2006-01-02", in.EffectiveFrom); err != nil {
		return in, einvoiceError("EINV-0980", "Effective from", "effective from must be a date (YYYY-MM-DD)")
	}
	before, err := irpProfileFor(schema, in.GSTIN)
	if err != nil {
		return in, err
	}
	if in.Enabled && in.Provider != "fake" && len(credentials) == 0 {
		if has, _ := HasChannelCredential(tenantID, irpCredentialChannel(in.Environment, in.GSTIN)); !has {
			return in, einvoiceError("EINV-0980", "Credentials", "enter the provider credentials before switching e-invoicing on")
		}
	}
	if len(credentials) > 0 {
		if err := SaveChannelCredential(tenantID, irpCredentialChannel(in.Environment, in.GSTIN), credentials); err != nil {
			return in, err
		}
		irpForgetToken(in.Environment, in.GSTIN)
	}
	_, err = db.DB.Exec(fmt.Sprintf(`
		INSERT INTO %s.irp_profiles (environment, gstin, legal_entity, provider, enabled, auto_submit, turnover_band, document_types,
			supply_types, inter_gstin, whitelisted_ip, effective_from, rule_version, activated_at, updated_by, updated_at)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,1,CASE WHEN $5 THEN NOW() END,$13,NOW())
		ON CONFLICT (environment, gstin) DO UPDATE SET
			legal_entity = EXCLUDED.legal_entity, provider = EXCLUDED.provider, enabled = EXCLUDED.enabled,
			auto_submit = EXCLUDED.auto_submit, turnover_band = EXCLUDED.turnover_band, document_types = EXCLUDED.document_types,
			supply_types = EXCLUDED.supply_types, inter_gstin = EXCLUDED.inter_gstin, whitelisted_ip = EXCLUDED.whitelisted_ip,
			effective_from = EXCLUDED.effective_from, rule_version = %s.irp_profiles.rule_version + 1,
			activated_at = CASE WHEN EXCLUDED.enabled AND %s.irp_profiles.activated_at IS NULL THEN NOW() ELSE %s.irp_profiles.activated_at END,
			updated_by = EXCLUDED.updated_by, updated_at = NOW()`, schema, schema, schema, schema),
		in.Environment, in.GSTIN, strings.TrimSpace(in.LegalEntity), in.Provider, in.Enabled, in.AutoSubmit, in.TurnoverBand,
		strings.Join(in.DocumentTypes, ","), strings.Join(in.SupplyTypes, ","), in.InterGSTIN, strings.TrimSpace(in.WhitelistedIP),
		in.EffectiveFrom, userID)
	if err != nil {
		return in, err
	}
	after, err := irpProfileFor(schema, in.GSTIN)
	if err != nil || after == nil {
		return in, err
	}
	detail := fmt.Sprintf("rules v%d: enabled=%v auto_submit=%v turnover=%s types=%s supply=%s inter_gstin=%s from=%s provider=%s",
		after.RuleVersion, after.Enabled, after.AutoSubmit, after.TurnoverBand, strings.Join(after.DocumentTypes, ","),
		strings.Join(after.SupplyTypes, ","), after.InterGSTIN, after.EffectiveFrom, after.Provider)
	event := "PROFILE_SAVED"
	if before == nil {
		event = "PROFILE_CREATED"
	}
	irpEvent(schema, 0, after.GSTIN, event, "", "", detail, userID)
	LogAuditEvent(tenantID, userID, "EINVOICE_PROFILE_SAVED", "SUCCESS", after.GSTIN+" "+detail)
	after.CredentialSet, _ = HasChannelCredential(tenantID, irpCredentialChannel(after.Environment, after.GSTIN))
	return *after, nil
}

// irpEvent appends to irp_events (append-only). Never fails its caller.
func irpEvent(schema string, documentID int64, gstin, event, from, to, detail, actor string) {
	var doc interface{}
	if documentID > 0 {
		doc = documentID
	}
	if _, err := db.DB.Exec(fmt.Sprintf(`INSERT INTO %s.irp_events (document_id, gstin, event, from_state, to_state, detail, actor) VALUES ($1,$2,$3,$4,$5,$6,$7)`, schema),
		doc, gstin, event, from, to, redactAndBoundLogField(detail, 2000), actor); err != nil {
		irpLog(schema, "irp_events write failed: "+err.Error())
	}
}

// ---------------------------------------------------------------------------
// 75.2.1 Eligibility
// ---------------------------------------------------------------------------

// EInvoiceDecision is whether one tax document goes to the IRP, and why.
type EInvoiceDecision struct {
	Required    bool   `json:"required"`
	DocType     string `json:"doc_type"`
	SupplyType  string `json:"supply_type"`
	GSTIN       string `json:"gstin"`
	Reason      string `json:"reason"`
	RuleVersion int    `json:"rule_version"`
}

// DecideEInvoice applies the seller GSTIN's rules to a posted document. Never
// sent: a sale to a buyer with no GSTIN (B2C, including the till's bills), a
// supply between two places of the same GSTIN, a type or supply the rules do
// not cover, and anything dated before the rules took effect. Advances and
// store credit are not tax documents of these types and never reach here.
func DecideEInvoice(profile *IRPProfile, docType, sellerGSTIN, buyerGSTIN, supplyType string, docDate time.Time) EInvoiceDecision {
	d := EInvoiceDecision{DocType: docType, GSTIN: sellerGSTIN, SupplyType: supplyType}
	no := func(why string) EInvoiceDecision { d.Reason = why; return d }
	if profile == nil || !profile.Enabled {
		return no("e-invoicing is not switched on for GSTIN " + firstNonEmpty(sellerGSTIN, "(none)"))
	}
	d.RuleVersion = profile.RuleVersion
	if from, err := time.Parse("2006-01-02", profile.EffectiveFrom); err == nil && !docDate.IsZero() && docDate.Before(from) {
		return no("dated before e-invoicing started for this GSTIN (" + profile.EffectiveFrom + ")")
	}
	if !containsString(profile.DocumentTypes, docType) {
		return no(docType + " is not among the document types this GSTIN e-invoices")
	}
	buyerGSTIN = strings.ToUpper(strings.TrimSpace(buyerGSTIN))
	if buyerGSTIN == "" || buyerGSTIN == "URP" {
		return no("B2C - the buyer has no GSTIN")
	}
	if buyerGSTIN == sellerGSTIN {
		return no("supply within the same GSTIN")
	}
	if len(buyerGSTIN) == 15 && len(sellerGSTIN) == 15 && buyerGSTIN[2:12] == sellerGSTIN[2:12] && profile.InterGSTIN == "NOT_REQUIRED" {
		return no("supply to another GSTIN of the same PAN - this GSTIN's rules do not e-invoice those")
	}
	if supplyType == "" {
		d.SupplyType = "B2B"
	}
	if !containsString(profile.SupplyTypes, d.SupplyType) {
		return no(d.SupplyType + " supplies are not among this GSTIN's supply types")
	}
	d.Required = true
	d.Reason = "B2B " + docType + " under rules v" + fmt.Sprint(profile.RuleVersion)
	return d
}

// ---------------------------------------------------------------------------
// 75.2.2 Canonical tax document (the IRP's e-invoice JSON, schema 1.1)
// ---------------------------------------------------------------------------

type IRPDocument struct {
	Version    string           `json:"Version"`
	TranDtls   IRPTranDtls      `json:"TranDtls"`
	DocDtls    IRPDocDtls       `json:"DocDtls"`
	SellerDtls IRPParty         `json:"SellerDtls"`
	BuyerDtls  IRPParty         `json:"BuyerDtls"`
	ItemList   []IRPItem        `json:"ItemList"`
	ValDtls    IRPValDtls       `json:"ValDtls"`
	RefDtls    *IRPRefDtls      `json:"RefDtls,omitempty"`
	Paise      IRPPaiseTotals   `json:"-"`
	Lines      []irpLinePaise   `json:"-"`
	Meta       IRPDocumentIdent `json:"-"`
}

type IRPTranDtls struct {
	TaxSch      string `json:"TaxSch"`
	SupTyp      string `json:"SupTyp"`
	RegRev      string `json:"RegRev"`
	IgstOnIntra string `json:"IgstOnIntra"`
}

type IRPDocDtls struct {
	Typ string `json:"Typ"`
	No  string `json:"No"`
	Dt  string `json:"Dt"`
}

type IRPParty struct {
	Gstin string `json:"Gstin"`
	LglNm string `json:"LglNm"`
	Pos   string `json:"Pos,omitempty"`
	Addr1 string `json:"Addr1"`
	Addr2 string `json:"Addr2,omitempty"`
	Loc   string `json:"Loc"`
	Pin   int    `json:"Pin"`
	Stcd  string `json:"Stcd"`
}

type IRPItem struct {
	SlNo       string  `json:"SlNo"`
	PrdDesc    string  `json:"PrdDesc,omitempty"`
	IsServc    string  `json:"IsServc"`
	HsnCd      string  `json:"HsnCd"`
	Qty        float64 `json:"Qty"`
	Unit       string  `json:"Unit"`
	UnitPrice  float64 `json:"UnitPrice"`
	TotAmt     float64 `json:"TotAmt"`
	Discount   float64 `json:"Discount"`
	AssAmt     float64 `json:"AssAmt"`
	GstRt      float64 `json:"GstRt"`
	IgstAmt    float64 `json:"IgstAmt"`
	CgstAmt    float64 `json:"CgstAmt"`
	SgstAmt    float64 `json:"SgstAmt"`
	TotItemVal float64 `json:"TotItemVal"`
}

type IRPValDtls struct {
	AssVal    float64 `json:"AssVal"`
	CgstVal   float64 `json:"CgstVal"`
	SgstVal   float64 `json:"SgstVal"`
	IgstVal   float64 `json:"IgstVal"`
	RndOffAmt float64 `json:"RndOffAmt"`
	TotInvVal float64 `json:"TotInvVal"`
}

type IRPRefDtls struct {
	PrecDocDtls []IRPPrecDoc `json:"PrecDocDtls"`
}

type IRPPrecDoc struct {
	InvNo string `json:"InvNo"`
	InvDt string `json:"InvDt"`
}

// irpLinePaise / IRPPaiseTotals are the same figures in paise, the unit the
// totals check runs in (75.2.3: "in paise integers, not floats").
type irpLinePaise struct {
	Gross, Discount, Assessable, CGST, SGST, IGST, Total int64
}

type IRPPaiseTotals struct {
	Assessable, CGST, SGST, IGST, RoundOff, Invoice int64
	Posted                                          int64
}

// IRPDocumentIdent is what identifies the document legally and in the ERP.
type IRPDocumentIdent struct {
	SourceDoctype string
	SourceID      string
	DocType       string
	Number        string
	Date          time.Time
	FiscalYear    string
	SellerGSTIN   string
	BuyerGSTIN    string
	Interstate    bool
}

// irpQuantityUnits are GST's unit quantity codes (UQC). An item's unit that is
// not one of them is refused at validation, naming the item.
var irpQuantityUnits = map[string]bool{
	"BAG": true, "BAL": true, "BDL": true, "BKL": true, "BOU": true, "BOX": true, "BTL": true, "BUN": true, "CAN": true,
	"CBM": true, "CCM": true, "CMS": true, "CTN": true, "DOZ": true, "DRM": true, "GGK": true, "GMS": true, "GRS": true,
	"GYD": true, "KGS": true, "KLR": true, "KME": true, "LTR": true, "MLT": true, "MTR": true, "MTS": true, "NOS": true,
	"OTH": true, "PAC": true, "PCS": true, "PRS": true, "QTL": true, "ROL": true, "SET": true, "SQF": true, "SQM": true,
	"SQY": true, "TBS": true, "TGM": true, "THD": true, "TON": true, "TUB": true, "UGS": true, "UNT": true, "YDS": true,
}

// irpUnitAliases maps the units items commonly carry onto a UQC.
var irpUnitAliases = map[string]string{"PC": "PCS", "PIECE": "PCS", "PIECES": "PCS", "NO": "NOS", "NUMBERS": "NOS", "KG": "KGS",
	"G": "GMS", "GM": "GMS", "GRAM": "GMS", "L": "LTR", "LITRE": "LTR", "ML": "MLT", "M": "MTR", "METRE": "MTR", "PAIR": "PRS", "DOZEN": "DOZ", "": "PCS"}

func irpUnit(raw string) string {
	u := strings.ToUpper(strings.TrimSpace(raw))
	if a, ok := irpUnitAliases[u]; ok {
		return a
	}
	return u
}

func irpDate(t time.Time) string { return t.Format("02/01/2006") }

// documentTaxDate is a tax document's date: its own invoice / posting date,
// else the day it was made.
func documentTaxDate(data map[string]interface{}, createdAt time.Time) time.Time {
	for _, k := range []string{"invoice_date", "note_date", "posting_date", "date"} {
		if v := strField(data, k); v != "" {
			for _, layout := range []string{"2006-01-02", time.RFC3339, "2006-01-02T15:04:05"} {
				if t, err := time.Parse(layout, v); err == nil {
					return t
				}
			}
		}
	}
	return createdAt
}

// irpFiscalYear is the Indian financial year of a date, "2026-27".
func irpFiscalYear(t time.Time) string {
	y := t.Year()
	if t.Month() < time.April {
		y--
	}
	return fmt.Sprintf("%d-%02d", y, (y+1)%100)
}

// einvoiceLineSource is one billed line before tax: the item and its
// tax-inclusive value, the way every invoice in this ERP is priced.
type einvoiceLineSource struct {
	SKU      string
	Qty      float64
	Gross    int64 // paise, tax-inclusive
	Discount int64
}

// salesInvoiceLines are the lines an invoice billed: its own items when it
// stores them, else its order's lines (billed = shipped quantity when the
// line records one, as the invoice was made).
func salesInvoiceLines(tenantID, schema string, data map[string]interface{}) []einvoiceLineSource {
	var out []einvoiceLineSource
	if raw, ok := data["items"].([]interface{}); ok {
		for _, r := range raw {
			m, ok := r.(map[string]interface{})
			if !ok {
				continue
			}
			qty := numFromInterface(m["qty"])
			rate := firstPositive(numFromInterface(m["rate"]), numFromInterface(m["unit_price"]), numFromInterface(m["sale_price"]))
			value := firstPositive(numFromInterface(m["net_total"]), numFromInterface(m["amount"]), rate*qty)
			if sku := firstNonEmpty(strField(m, "sku"), strField(m, "item"), strField(m, "item_code")); sku != "" && qty > 0 {
				out = append(out, einvoiceLineSource{SKU: sku, Qty: qty, Gross: RupeesToPaise(value), Discount: RupeesToPaise(numFromInterface(m["discount"]))})
			}
		}
		if len(out) > 0 {
			return out
		}
	}
	orderID := strField(data, "sales_order_id")
	if orderID == "" {
		return nil
	}
	rows, err := db.DB.Query(fmt.Sprintf(`SELECT data FROM %s.documents WHERE doctype = 'SalesOrderLine' AND data->>'order_id' = $1 AND deleted_at IS NULL ORDER BY id`, schema), orderID)
	if err != nil {
		return nil
	}
	defer rows.Close()
	for rows.Next() {
		var raw []byte
		var line map[string]interface{}
		if rows.Scan(&raw) != nil || json.Unmarshal(raw, &line) != nil {
			continue
		}
		if strField(line, "line_status") == "Cancelled" {
			continue
		}
		billed := numFromInterface(line["qty"])
		if _, has := line["shipped_qty"]; has {
			billed = numFromInterface(line["shipped_qty"])
		}
		if sku := strField(line, "sku"); sku != "" && billed > 0 {
			out = append(out, einvoiceLineSource{SKU: sku, Qty: billed, Gross: RupeesToPaise(numFromInterface(line["unit_price"]) * billed)})
		}
	}
	return out
}

// BuildCanonicalTaxDocument (75.2.2) builds the IRP's e-invoice JSON for a
// posted Sales Invoice or Credit Note, from the stored document and the
// masters as they are now - which is why it is built once, at posting, and
// frozen with its hash: a retry or a reprint never reads live data again.
func BuildCanonicalTaxDocument(tenantID, doctype, id string) (*IRPDocument, error) {
	docType, ok := EInvoiceDocTypes[doctype]
	if !ok {
		return nil, fmt.Errorf("%s is not an e-invoice document type", doctype)
	}
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return nil, err
	}
	var raw []byte
	var createdAt time.Time
	if err := db.DB.QueryRow(fmt.Sprintf(`SELECT data, created_at FROM %s.documents WHERE doctype = $1 AND id = $2 AND deleted_at IS NULL`, schema), doctype, id).Scan(&raw, &createdAt); err != nil {
		return nil, fmt.Errorf("%s %s not found: %v", doctype, id, err)
	}
	var data map[string]interface{}
	if err := json.Unmarshal(raw, &data); err != nil {
		return nil, err
	}
	doc := &IRPDocument{Version: "1.1", TranDtls: IRPTranDtls{TaxSch: "GST", SupTyp: firstNonEmpty(strings.ToUpper(strField(data, "supply_type")), "B2B"), RegRev: "N", IgstOnIntra: "N"}}
	doc.Meta = IRPDocumentIdent{SourceDoctype: doctype, SourceID: id, DocType: docType, Date: documentTaxDate(data, createdAt)}

	var store, customerRef string
	var lines []einvoiceLineSource
	var posted int64
	switch doctype {
	case "SalesInvoice":
		doc.Meta.Number = firstNonEmpty(strField(data, "invoice_number"), id)
		store, customerRef = strField(data, "location"), strField(data, "customer")
		lines = salesInvoiceLines(tenantID, schema, data)
		posted = RupeesToPaise(numFromInterface(data["total_amount"]))
	case "CreditNote":
		doc.Meta.Number = firstNonEmpty(strField(data, "note_number"), strField(data, "code"), id)
		customerRef = strField(data, "customer_id")
		posted = RupeesToPaise(numFromInterface(data["amount"]))
		// A credit note stores one amount. Its lines are the original
		// invoice's, in proportion, so each keeps its HSN and rate.
		if inv, _, errI := fetchDocData(tenantID, "SalesInvoice", strField(data, "sales_invoice_id")); errI == nil && inv != nil {
			store = strField(inv, "location")
			if customerRef == "" {
				customerRef = strField(inv, "customer")
			}
			invDate := documentTaxDate(inv, createdAt)
			doc.RefDtls = &IRPRefDtls{PrecDocDtls: []IRPPrecDoc{{InvNo: firstNonEmpty(strField(inv, "invoice_number"), strField(data, "sales_invoice_id")), InvDt: irpDate(invDate)}}}
			invLines := salesInvoiceLines(tenantID, schema, inv)
			var invGross int64
			for _, l := range invLines {
				invGross += l.Gross - l.Discount
			}
			if invGross > 0 {
				var given int64
				for i, l := range invLines {
					share := (l.Gross - l.Discount) * posted / invGross
					if i == len(invLines)-1 {
						share = posted - given // the last line takes the paisa left by rounding
					}
					given += share
					lines = append(lines, einvoiceLineSource{SKU: l.SKU, Qty: l.Qty, Gross: share})
				}
			}
		}
	}
	doc.Meta.Number = strings.ToUpper(strings.TrimSpace(doc.Meta.Number))
	doc.Meta.FiscalYear = irpFiscalYear(doc.Meta.Date)
	doc.DocDtls = IRPDocDtls{Typ: docType, No: doc.Meta.Number, Dt: irpDate(doc.Meta.Date)}

	seller := LoadPrintStore(tenantID, store)
	buyer := loadPrintCustomer(tenantID, customerRef)
	doc.Meta.SellerGSTIN, doc.Meta.BuyerGSTIN = strings.ToUpper(seller.GSTIN), strings.ToUpper(buyer.GSTIN)
	doc.SellerDtls = irpPartyFrom(seller)
	doc.BuyerDtls = irpPartyFrom(buyer)
	doc.BuyerDtls.Pos = doc.BuyerDtls.Stcd // goods delivered to the buyer's registered state
	doc.Meta.Interstate = doc.SellerDtls.Stcd != "" && doc.BuyerDtls.Pos != "" && doc.SellerDtls.Stcd != doc.BuyerDtls.Pos

	cache := map[string]map[string]interface{}{}
	var t IRPPaiseTotals
	for i, l := range lines {
		item := printItemFields(tenantID, l.SKU, cache)
		rate := numFromInterface(item["gst_rate"])
		net := l.Gross - l.Discount
		assessable := int64(float64(net)/(1+rate/100) + 0.5)
		tax := net - assessable
		lp := irpLinePaise{Gross: l.Gross, Discount: l.Discount, Assessable: assessable, Total: net}
		if doc.Meta.Interstate {
			lp.IGST = tax
		} else {
			lp.CGST = tax / 2
			lp.SGST = tax - lp.CGST
		}
		// Gross is tax-inclusive; the IRP wants it before tax.
		grossBeforeTax := assessable + int64(float64(l.Discount)/(1+rate/100)+0.5)
		discountBeforeTax := grossBeforeTax - assessable
		hsn := strings.TrimSpace(strField(item, "hsn_code"))
		doc.ItemList = append(doc.ItemList, IRPItem{
			SlNo: fmt.Sprint(i + 1), PrdDesc: firstNonEmpty(strField(item, "name"), l.SKU), IsServc: map[bool]string{true: "Y", false: "N"}[strings.HasPrefix(hsn, "99")],
			HsnCd: hsn, Qty: l.Qty, Unit: irpUnit(strField(item, "uom")),
			UnitPrice: round2(PaiseToRupees(grossBeforeTax) / maxFloat(l.Qty, 1)), TotAmt: PaiseToRupees(grossBeforeTax), Discount: PaiseToRupees(discountBeforeTax),
			AssAmt: PaiseToRupees(assessable), GstRt: rate, IgstAmt: PaiseToRupees(lp.IGST), CgstAmt: PaiseToRupees(lp.CGST), SgstAmt: PaiseToRupees(lp.SGST),
			TotItemVal: PaiseToRupees(net),
		})
		doc.Lines = append(doc.Lines, lp)
		t.Assessable += assessable
		t.CGST += lp.CGST
		t.SGST += lp.SGST
		t.IGST += lp.IGST
	}
	t.Posted = posted
	t.Invoice = posted
	t.RoundOff = posted - (t.Assessable + t.CGST + t.SGST + t.IGST)
	doc.Paise = t
	doc.ValDtls = IRPValDtls{AssVal: PaiseToRupees(t.Assessable), CgstVal: PaiseToRupees(t.CGST), SgstVal: PaiseToRupees(t.SGST),
		IgstVal: PaiseToRupees(t.IGST), RndOffAmt: PaiseToRupees(t.RoundOff), TotInvVal: PaiseToRupees(t.Invoice)}
	return doc, nil
}

func maxFloat(a, b float64) float64 {
	if a > b {
		return a
	}
	return b
}

func irpPartyFrom(p PrintStore) IRPParty {
	party := IRPParty{Gstin: strings.ToUpper(strings.TrimSpace(p.GSTIN)), LglNm: firstNonEmpty(p.LegalName, p.Name), Addr1: p.Address, Loc: firstNonEmpty(p.City, p.State)}
	if len(party.Addr1) > 100 {
		party.Addr1, party.Addr2 = party.Addr1[:100], party.Addr1[100:]
	}
	fmt.Sscanf(strings.TrimSpace(p.Pincode), "%d", &party.Pin)
	if len(party.Gstin) >= 2 {
		party.Stcd = party.Gstin[:2]
	} else if r, ok := ResolveRegion("IN", p.State); ok {
		party.Stcd = r.TaxCode
	}
	return party
}

// CanonicalJSON is the frozen payload and its SHA-256.
func (d *IRPDocument) CanonicalJSON() ([]byte, string, error) {
	b, err := json.Marshal(d)
	if err != nil {
		return nil, "", err
	}
	sum := sha256.Sum256(b)
	return b, hex.EncodeToString(sum[:]), nil
}

// ---------------------------------------------------------------------------
// 75.2.3 Validation before submission
// ---------------------------------------------------------------------------

// EInvoiceIssue is one problem, naming the field and the fix.
type EInvoiceIssue struct {
	Field   string `json:"field"`
	Problem string `json:"problem"`
	Fix     string `json:"fix"`
}

var irpNumberPattern = regexp.MustCompile(`^[A-Z1-9][A-Z0-9/-]*$`)
var irpHSNPattern = regexp.MustCompile(`^(\d{4}|\d{6}|\d{8})$`)

// ValidateCanonicalTaxDocument (75.2.3) checks what the IRP would refuse, so
// the refusal happens here, with the fix, instead of at the portal.
func ValidateCanonicalTaxDocument(d *IRPDocument, turnoverBand string) []EInvoiceIssue {
	var out []EInvoiceIssue
	add := func(field, problem, fix string) {
		out = append(out, EInvoiceIssue{Field: field, Problem: problem, Fix: fix})
	}
	n := d.DocDtls.No
	if n == "" || len(n) > 16 {
		add("Document number", fmt.Sprintf("%q is %d characters - the IRP takes at most 16", n, len(n)), "Shorten its number series on Settings → Customize → Prefix Configs (2627 year, 4-5 digits)")
	} else if !irpNumberPattern.MatchString(n) {
		add("Document number", fmt.Sprintf("%q has characters the IRP refuses", n), "Use capitals, digits, / and - only, not starting with 0, / or -")
	}
	checkParty := func(label string, p IRPParty) {
		if len(p.Gstin) != 15 || !ValidGSTINChecksum(p.Gstin) {
			add(label+" GSTIN", fmt.Sprintf("%q is not a valid GSTIN", p.Gstin), "Correct the GSTIN on the "+strings.ToLower(label)+"'s record")
		}
		if p.Pin < 100000 || p.Pin > 999999 {
			add(label+" PIN code", "missing or not 6 digits", "Fill the PIN code on the "+strings.ToLower(label)+"'s record")
		}
		if strings.TrimSpace(p.Addr1) == "" {
			add(label+" address", "missing", "Fill Address Line 1 on the "+strings.ToLower(label)+"'s record")
		}
		if strings.TrimSpace(p.LglNm) == "" {
			add(label+" name", "missing", "Fill the legal name")
		}
		if len(p.Gstin) >= 2 && p.Stcd != "" && p.Stcd != p.Gstin[:2] {
			add(label+" state", fmt.Sprintf("state code %s does not match GSTIN %s", p.Stcd, p.Gstin), "Pick the state the GSTIN is registered in")
		}
	}
	checkParty("Seller", d.SellerDtls)
	checkParty("Buyer", d.BuyerDtls)
	if d.BuyerDtls.Pos == "" {
		add("Place of supply", "not known", "Fill the buyer's GSTIN or state")
	}
	if len(d.ItemList) == 0 {
		add("Lines", "the document has no item lines", "An e-invoice lists every item with its HSN; bill it from item lines")
	}
	// Every e-invoicing GSTIN has AATO above ₹5 crore, which needs a 6-digit
	// HSN (Notification 78/2020-CT).
	const minHSN = 6
	for i, it := range d.ItemList {
		line := fmt.Sprintf("Line %d (%s)", i+1, it.PrdDesc)
		if !irpHSNPattern.MatchString(it.HsnCd) || len(it.HsnCd) < minHSN {
			add(line+" HSN", fmt.Sprintf("%q is not a %d-digit-or-longer HSN", it.HsnCd, minHSN), "Set the item's HSN code (Setup → Item)")
		}
		if !irpQuantityUnits[it.Unit] {
			add(line+" unit", fmt.Sprintf("%q is not a GST unit (UQC)", it.Unit), "Set the item's unit to a UQC such as PCS, NOS, KGS, MTR")
		}
		if it.Qty <= 0 {
			add(line+" quantity", "zero or negative", "Correct the line")
		}
		if d.Meta.Interstate && (d.Lines[i].CGST != 0 || d.Lines[i].SGST != 0) || !d.Meta.Interstate && d.Lines[i].IGST != 0 {
			add(line+" tax", "CGST/SGST and IGST do not match the place of supply", "Check the seller and buyer states")
		}
	}
	p := d.Paise
	if sum := p.Assessable + p.CGST + p.SGST + p.IGST + p.RoundOff; sum != p.Invoice {
		add("Totals", fmt.Sprintf("lines + tax + round-off = %d paise, invoice total %d paise", sum, p.Invoice), "Re-post the document")
	}
	if p.RoundOff > 100 || p.RoundOff < -100 {
		add("Totals", fmt.Sprintf("the lines come to %.2f against a posted total of %.2f", PaiseToRupees(p.Invoice-p.RoundOff), PaiseToRupees(p.Invoice)),
			"The posted total and its lines differ by more than ₹1 - correct the lines (or the note's original invoice) before submitting")
	}
	if d.DocDtls.Typ == "CRN" || d.DocDtls.Typ == "DBN" {
		if d.RefDtls == nil || len(d.RefDtls.PrecDocDtls) == 0 {
			add("Original invoice", "a note must reference the invoice it adjusts", "Link the note to its sales invoice")
		}
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Field < out[j].Field })
	return out
}

// irpLog writes a warning to the tenant's System Errors from a worker that
// knows only the schema.
func irpLog(schema, msg string) {
	if tenantID, err := tenantIDForSchema(schema); err == nil && tenantID != "" {
		LogSystemError(tenantID, "", "WARN", "einvoice", msg, "")
	}
}
