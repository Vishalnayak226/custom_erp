package engines

import (
	"custom_erp/db"
	"database/sql"
	"encoding/json"
	"fmt"
	"math"
	"sort"
	"strings"
)

// Stage 168.1: blanket / rate contracts with release orders.
//
// A PurchaseContract fixes a vendor's rates for a window (a Rate contract) and,
// for a Blanket, also caps what may be released against it, in value and/or
// per-item quantity. A release order is an ordinary purchase order whose
// `contract` field names the contract:
//   - a blank line rate is filled from the contract; a rate above it is refused;
//   - the PO date must fall inside the window, the vendor (and store, when the
//     contract names one) must match, and the contract must be Active;
//   - what has been released is never a stored counter. It is read from the POs
//     that name the contract and are committed (Pending Approval, Approved,
//     Closed, less the closed quantity), so a cancelled or rejected PO gives its
//     release back by itself and replaying a submit cannot count it twice;
//   - the cap is checked when a PO goes for approval, under a per-contract
//     lock, so two releases racing for the last of the cap cannot both pass.
//
// Money on the contract is in rupees, the unit a PO line's rate and the PO
// total already use; the cap arithmetic runs in whole paise.
//
// A PO that names no contract goes through none of this.

const (
	PurchaseContractDoctype = "PurchaseContract"

	contractKindRate    = "Rate contract"
	contractKindBlanket = "Blanket"

	contractDraft  = "Draft"
	contractActive = "Active"
	contractClosed = "Closed"

	// Error codes (catalog: internal/server/error_catalog_extensions.go).
	codeContractUnusable  = "PURCHA-6100" // the contract cannot take this PO
	codeContractRate      = "PURCHA-6101" // a line's rate is above the contract's
	codeContractCap       = "PURCHA-6102" // the release would pass the cap
	codeContractSave      = "PURCHA-6103" // the contract itself is not valid or is locked
	codeContractAction    = "PURCHA-6104" // activate / close refused
	contractTolerance     = 0.005
	contractCommittedList = `'Pending Approval','Approved','Closed'`
)

func contractErr(code, format string, a ...interface{}) error {
	return &ValidationError{Code: code, Message: fmt.Sprintf(format, a...)}
}

type contractLine struct {
	Sku    string  `json:"sku"`
	Rate   float64 `json:"rate"`
	CapQty float64 `json:"cap_qty,omitempty"`
}

// docTableOf reads a JSONTable value, kept as JSON text (or already an array).
func docTableOf(v interface{}) []map[string]interface{} {
	var raw []byte
	switch t := v.(type) {
	case string:
		if strings.TrimSpace(t) == "" {
			return nil
		}
		raw = []byte(t)
	case []interface{}, []map[string]interface{}:
		raw, _ = json.Marshal(t)
	default:
		return nil
	}
	var out []map[string]interface{}
	if json.Unmarshal(raw, &out) != nil {
		return nil
	}
	return out
}

func parseContractLines(v interface{}) []contractLine {
	rows := docTableOf(v)
	out := make([]contractLine, 0, len(rows))
	for _, r := range rows {
		out = append(out, contractLine{
			Sku:    strings.TrimSpace(fmt.Sprintf("%v", orBlank(r["sku"]))),
			Rate:   numFromInterface(r["rate"]),
			CapQty: numFromInterface(r["cap_qty"]),
		})
	}
	return out
}

func orBlank(v interface{}) interface{} {
	if v == nil {
		return ""
	}
	return v
}

func sameSku(a, b string) bool { return strings.EqualFold(strings.TrimSpace(a), strings.TrimSpace(b)) }

// validatePurchaseContractSave is the contract's own save rule, run at
// ValidateTransactionalRules (the generic record form and an import both pass it).
func validatePurchaseContractSave(tenantID, docID, priorStatus string, priorData, payload map[string]interface{}) error {
	written := resolveWrittenStatus(payload)
	if priorData == nil && written != "" && written != contractDraft {
		return contractErr(codeContractSave, "a new contract starts as Draft; Activate it from its page")
	}
	if priorData != nil && written != "" && written != priorStatus {
		return contractErr(codeContractSave, "a contract becomes Active or Closed only through Activate or Close on its page")
	}
	// Once active, what was agreed is fixed; only the notes may change.
	if priorData != nil && priorStatus != contractDraft && priorStatus != "" {
		for _, f := range []string{"vendor", "contract_kind", "valid_from", "valid_to", "location"} {
			if strField(priorData, f) != strField(payload, f) {
				return contractErr(codeContractSave, "contract %s is %s: its %s cannot change - close it and raise a new one", docID, priorStatus, f)
			}
		}
		if math.Abs(numFromInterface(priorData["cap_value"])-numFromInterface(payload["cap_value"])) > 0.0001 ||
			!contractLinesEqual(parseContractLines(priorData["lines"]), parseContractLines(payload["lines"])) {
			return contractErr(codeContractSave, "contract %s is %s: its items and cap cannot change - close it and raise a new one", docID, priorStatus)
		}
		return nil
	}
	return checkContractTerms(tenantID, payload)
}

func contractLinesEqual(a, b []contractLine) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i].Sku != b[i].Sku || math.Abs(a[i].Rate-b[i].Rate) > 0.0001 || math.Abs(a[i].CapQty-b[i].CapQty) > 0.0001 {
			return false
		}
	}
	return true
}

// checkContractTerms holds what a contract needs to be usable at all.
func checkContractTerms(tenantID string, d map[string]interface{}) error {
	from, to := dateOnly(strField(d, "valid_from")), dateOnly(strField(d, "valid_to"))
	if from != "" && to != "" && to < from {
		return contractErr(codeContractSave, "the contract's end date is before its start date")
	}
	capValue := numFromInterface(d["cap_value"])
	if capValue < 0 {
		return contractErr(codeContractSave, "the cap value cannot be negative")
	}
	lines := parseContractLines(d["lines"])
	if len(lines) == 0 {
		return contractErr(codeContractSave, "add at least one item with its rate")
	}
	seen := map[string]bool{}
	anyQtyCap := false
	for i, l := range lines {
		if l.Sku == "" {
			return contractErr(codeContractSave, "item %d has no item code", i+1)
		}
		key := strings.ToLower(l.Sku)
		if seen[key] {
			return contractErr(codeContractSave, "item %s is on the contract twice", l.Sku)
		}
		seen[key] = true
		if l.Rate <= 0 {
			return contractErr(codeContractSave, "item %s needs a rate above zero", l.Sku)
		}
		if l.CapQty < 0 {
			return contractErr(codeContractSave, "item %s: the cap quantity cannot be negative", l.Sku)
		}
		if l.CapQty > 0 {
			anyQtyCap = true
		}
		if _, err := ResolveItemBySKU(tenantID, l.Sku); err != nil {
			return contractErr(codeContractSave, "item %s is not in the item master", l.Sku)
		}
	}
	if strField(d, "contract_kind") == contractKindBlanket && capValue <= 0 && !anyQtyCap {
		return contractErr(codeContractSave, "a blanket contract needs a cap value or a cap quantity on an item")
	}
	return nil
}

// loadPurchaseContract reads a contract and its stored status.
func loadPurchaseContract(tenantID, id string) (map[string]interface{}, string, error) {
	data, status, err := fetchDocData(tenantID, PurchaseContractDoctype, id)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, "", contractErr(codeContractUnusable, "contract %s was not found", id)
		}
		return nil, "", err
	}
	return data, status, nil
}

// applyPOContract is the release-order rule, run on every PO save. It stamps a
// blank line rate from the contract, refuses a rate above it, and checks the
// contract is Active, the vendor, store and date window; when the PO is being
// committed it also checks the cap. A PO with no contract is untouched.
func applyPOContract(tenantID, docID, priorStatus string, priorData, payload map[string]interface{}) error {
	contractID := strField(payload, "contract")
	prior := ""
	if priorData != nil {
		prior = strField(priorData, "contract")
	}
	if contractID == "" {
		if prior != "" && priorStatus != "" && priorStatus != "Draft" {
			return contractErr(codeContractUnusable, "PO %s is a release against contract %s; the contract cannot be taken off it once it has gone for approval", docID, prior)
		}
		return nil
	}
	if prior != "" && prior != contractID && priorStatus != "" && priorStatus != "Draft" {
		return contractErr(codeContractUnusable, "PO %s is a release against contract %s; it cannot be moved to another contract after approval was asked", docID, prior)
	}
	itemsNow, _ := payload["items"].(string)
	changed := docID == "" || priorData == nil || prior != contractID ||
		itemsNow != strField(priorData, "items") ||
		firstNonEmpty(strField(payload, "vendor"), strField(payload, "vendor_id")) != firstNonEmpty(strField(priorData, "vendor"), strField(priorData, "vendor_id")) ||
		dateOnly(strField(payload, "order_date")) != dateOnly(strField(priorData, "order_date"))

	contract, status, err := loadPurchaseContract(tenantID, contractID)
	if err != nil {
		return err
	}
	if changed {
		if status != contractActive {
			return contractErr(codeContractUnusable, "contract %s is %s: releases are raised only against an Active contract", contractID, status)
		}
		if v := strField(contract, "vendor"); !strings.EqualFold(v, firstNonEmpty(strField(payload, "vendor"), strField(payload, "vendor_id"))) {
			return contractErr(codeContractUnusable, "contract %s is with vendor %s; this PO is for another vendor", contractID, v)
		}
		if loc := strField(contract, "location"); loc != "" && loc != strField(payload, "location") {
			return contractErr(codeContractUnusable, "contract %s is for store %s only", contractID, loc)
		}
		day := dateOnly(strField(payload, "order_date"))
		if day == "" {
			day = BusinessToday()
		}
		if from := dateOnly(strField(contract, "valid_from")); from != "" && day < from {
			return contractErr(codeContractUnusable, "the PO date %s is before contract %s starts (%s)", day, contractID, from)
		}
		if to := dateOnly(strField(contract, "valid_to")); to != "" && day > to {
			return contractErr(codeContractUnusable, "the PO date %s is after contract %s ended (%s)", day, contractID, to)
		}
		if err := stampPOContractRates(contract, contractID, payload); err != nil {
			return err
		}
	}
	// The cap is judged when the PO is committed: going for approval, or saved
	// straight as Approved on an ungated tenant.
	written := resolveWrittenStatus(payload)
	if (written == "Approved" || written == "Pending Approval") && priorStatus != written {
		return checkContractCap(tenantID, contractID, docID, payload)
	}
	return nil
}

// stampPOContractRates fills a blank rate and refuses a rate above the contract's.
func stampPOContractRates(contract map[string]interface{}, contractID string, payload map[string]interface{}) error {
	itemsStr, _ := payload["items"].(string)
	if strings.TrimSpace(itemsStr) == "" {
		return nil
	}
	var lines []map[string]interface{}
	if json.Unmarshal([]byte(itemsStr), &lines) != nil {
		return nil // a malformed list is the PO's own validation's to refuse
	}
	byKey := map[string]contractLine{}
	for _, l := range parseContractLines(contract["lines"]) {
		byKey[strings.ToLower(l.Sku)] = l
	}
	stamped := false
	for _, l := range lines {
		sku := strings.TrimSpace(fmt.Sprintf("%v", orBlank(l["sku"])))
		if sku == "" {
			continue
		}
		cl, ok := byKey[strings.ToLower(sku)]
		if !ok {
			return contractErr(codeContractUnusable, "item %s is not on contract %s", sku, contractID)
		}
		rate := numFromInterface(l["rate"])
		switch {
		case rate <= 0:
			l["rate"] = cl.Rate
			stamped = true
		case rate > cl.Rate+contractTolerance:
			return contractErr(codeContractRate, "item %s: the rate %.2f is above contract %s's rate %.2f", sku, rate, contractID, cl.Rate)
		}
	}
	if stamped {
		b, err := json.Marshal(lines)
		if err != nil {
			return err
		}
		payload["items"] = string(b)
	}
	return nil
}

// contractRelease is what one committed PO has taken from a contract.
type contractRelease struct {
	ID     string
	Status string
	Date   string
	Value  int64 // paise, at the contract's rates
	Qty    map[string]float64
}

// releaseOfPO values a PO's lines at the contract's rates, the closed part
// (short-closed quantity) not counted.
func releaseOfPO(id, status string, data map[string]interface{}, byKey map[string]contractLine) contractRelease {
	r := contractRelease{ID: id, Status: status, Date: dateOnly(strField(data, "order_date")), Qty: map[string]float64{}}
	for _, l := range docTableOf(data["items"]) {
		sku := strings.TrimSpace(fmt.Sprintf("%v", orBlank(l["sku"])))
		qty := numFromInterface(l["qty"]) - numFromInterface(l["closed_qty"])
		if sku == "" || qty <= 0 {
			continue
		}
		rate := numFromInterface(l["rate"])
		if cl, ok := byKey[strings.ToLower(sku)]; ok {
			rate = cl.Rate
		}
		r.Qty[strings.ToLower(sku)] += qty
		r.Value += RupeesToPaise(qty * rate)
	}
	return r
}

func contractLineIndex(contract map[string]interface{}) map[string]contractLine {
	byKey := map[string]contractLine{}
	for _, l := range parseContractLines(contract["lines"]) {
		byKey[strings.ToLower(l.Sku)] = l
	}
	return byKey
}

// contractReleases returns every PO naming the contract, other than skip.
func contractReleases(tenantID, contractID, skip string, committedOnly bool, byKey map[string]contractLine) ([]contractRelease, error) {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return nil, err
	}
	q := fmt.Sprintf(`SELECT id, status, data FROM %s.documents
		WHERE doctype = 'PurchaseOrder' AND deleted_at IS NULL AND data->>'contract' = $1 AND id <> $2`, schema)
	if committedOnly {
		q += ` AND status IN (` + contractCommittedList + `)`
	}
	rows, err := db.DB.Query(q+` ORDER BY created_at, id`, contractID, skip)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []contractRelease
	for rows.Next() {
		var id, status, dataStr string
		if err := rows.Scan(&id, &status, &dataStr); err != nil {
			return nil, err
		}
		var data map[string]interface{}
		if json.Unmarshal([]byte(dataStr), &data) != nil {
			continue
		}
		out = append(out, releaseOfPO(id, status, data, byKey))
	}
	return out, rows.Err()
}

// checkContractCap refuses a PO whose release, added to what the other
// committed POs already took, passes the contract's cap. The PO itself is
// excluded from "what is taken", so asking twice does not count it twice.
func checkContractCap(tenantID, contractID, poID string, po map[string]interface{}) error {
	contract, _, err := loadPurchaseContract(tenantID, contractID)
	if err != nil {
		return err
	}
	byKey := contractLineIndex(contract)
	others, err := contractReleases(tenantID, contractID, poID, true, byKey)
	if err != nil {
		return err
	}
	var takenValue int64
	takenQty := map[string]float64{}
	for _, o := range others {
		takenValue += o.Value
		for k, q := range o.Qty {
			takenQty[k] += q
		}
	}
	this := releaseOfPO(poID, "", po, byKey)
	if capPaise := RupeesToPaise(numFromInterface(contract["cap_value"])); capPaise > 0 && takenValue+this.Value > capPaise {
		return contractErr(codeContractCap, "contract %s allows %.2f; %.2f is already released, so this PO's %.2f would pass the cap by %.2f",
			contractID, PaiseToRupees(capPaise), PaiseToRupees(takenValue), PaiseToRupees(this.Value), PaiseToRupees(takenValue+this.Value-capPaise))
	}
	for key, cl := range byKey {
		if cl.CapQty > 0 && takenQty[key]+this.Qty[key] > cl.CapQty+0.0001 {
			return contractErr(codeContractCap, "contract %s allows %g of %s; %g is already released, so this PO's %g would pass it",
				contractID, cl.CapQty, cl.Sku, takenQty[key], this.Qty[key])
		}
	}
	return nil
}

// contractApprovalGate says whether role may act as the PO approver for a
// value: the role the approval rules name for that amount (the lowest slab
// when there is no value), or full access.
func contractApprovalGate(tenantID, role string, amount float64) bool {
	if HasFullAccess(role) {
		return true
	}
	required, err := requiredApproverRole(tenantID, "PurchaseOrder", amount)
	if err == nil && required == "" {
		required, _, _ = LowestApproverRoleForDoctype(tenantID, "PurchaseOrder")
	}
	return required != "" && roleSatisfies(role, required)
}

// ActivatePurchaseContract moves a Draft contract to Active. Only the role that
// approves a PO of the contract's cap value may do it.
func ActivatePurchaseContract(tenantID, id, userID, role string) error {
	data, status, err := loadPurchaseContract(tenantID, id)
	if err != nil {
		return err
	}
	if status != contractDraft {
		return contractErr(codeContractAction, "contract %s is %s; only a Draft contract can be activated", id, status)
	}
	if !contractApprovalGate(tenantID, role, numFromInterface(data["cap_value"])) {
		return contractErr(codeContractAction, "activating a contract of this size needs the role that approves purchase orders of that value")
	}
	if err := checkContractTerms(tenantID, data); err != nil {
		return err
	}
	if to := dateOnly(strField(data, "valid_to")); to != "" && to < BusinessToday() {
		return contractErr(codeContractAction, "contract %s ended on %s; change its dates first", id, to)
	}
	return setContractStatus(tenantID, id, contractDraft, contractActive, userID, "activated")
}

// ClosePurchaseContract ends an Active contract; releases already raised stand.
func ClosePurchaseContract(tenantID, id, userID, role string) error {
	data, status, err := loadPurchaseContract(tenantID, id)
	if err != nil {
		return err
	}
	if status != contractActive {
		return contractErr(codeContractAction, "contract %s is %s; only an Active contract can be closed", id, status)
	}
	if !contractApprovalGate(tenantID, role, numFromInterface(data["cap_value"])) {
		return contractErr(codeContractAction, "closing a contract of this size needs the role that approves purchase orders of that value")
	}
	return setContractStatus(tenantID, id, contractActive, contractClosed, userID, "closed")
}

func setContractStatus(tenantID, id, from, to, userID, verb string) error {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return err
	}
	stamp := fmt.Sprintf(`{"status": %q, "%s_by": %q, "%s_on": %q}`, to, verb, userID, verb, BusinessToday())
	res, err := db.DB.Exec(fmt.Sprintf(`UPDATE %s.documents SET status = $1, data = data || $2::jsonb, updated_at = CURRENT_TIMESTAMP
		WHERE doctype = $3 AND id = $4 AND status = $5 AND deleted_at IS NULL`, schema), to, stamp, PurchaseContractDoctype, id, from)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return contractErr(codeContractAction, "contract %s is no longer %s", id, from)
	}
	LogAuditEvent(tenantID, userID, "PURCHASE_CONTRACT_"+strings.ToUpper(verb), "SUCCESS", fmt.Sprintf("contract %s %s", id, verb))
	return nil
}

// ContractSummary is the open contract's consumed-versus-cap view.
type ContractSummary struct {
	ID             string                `json:"id"`
	Vendor         string                `json:"vendor"`
	Kind           string                `json:"kind"`
	Status         string                `json:"status"` // Draft, Active, Closed, or Expired (Active past its end date)
	ValidFrom      string                `json:"valid_from"`
	ValidTo        string                `json:"valid_to"`
	Location       string                `json:"location"`
	CapValue       float64               `json:"cap_value"`
	ConsumedValue  float64               `json:"consumed_value"`
	RemainingValue float64               `json:"remaining_value"`
	Lines          []ContractSummaryLine `json:"lines"`
	Releases       []ContractReleaseView `json:"releases"`
}

type ContractSummaryLine struct {
	Sku         string  `json:"sku"`
	Rate        float64 `json:"rate"`
	CapQty      float64 `json:"cap_qty"`
	ConsumedQty float64 `json:"consumed_qty"`
}

type ContractReleaseView struct {
	ID     string  `json:"id"`
	Status string  `json:"status"`
	Date   string  `json:"date"`
	Value  float64 `json:"value"`
}

// PurchaseContractSummary reads a contract with what has been released.
func PurchaseContractSummary(tenantID, id string) (ContractSummary, error) {
	var s ContractSummary
	data, status, err := loadPurchaseContract(tenantID, id)
	if err != nil {
		return s, err
	}
	byKey := contractLineIndex(data)
	all, err := contractReleases(tenantID, id, "", false, byKey)
	if err != nil {
		return s, err
	}
	s = ContractSummary{ID: id, Vendor: strField(data, "vendor"), Kind: strField(data, "contract_kind"), Status: status,
		ValidFrom: dateOnly(strField(data, "valid_from")), ValidTo: dateOnly(strField(data, "valid_to")),
		Location: strField(data, "location"), CapValue: numFromInterface(data["cap_value"]),
		Lines: []ContractSummaryLine{}, Releases: []ContractReleaseView{}}
	if status == contractActive && s.ValidTo != "" && s.ValidTo < BusinessToday() {
		s.Status = "Expired"
	}
	committed := map[string]bool{"Pending Approval": true, "Approved": true, "Closed": true}
	var taken int64
	takenQty := map[string]float64{}
	for _, r := range all {
		s.Releases = append(s.Releases, ContractReleaseView{ID: r.ID, Status: r.Status, Date: r.Date, Value: PaiseToRupees(r.Value)})
		if committed[r.Status] {
			taken += r.Value
			for k, q := range r.Qty {
				takenQty[k] += q
			}
		}
	}
	s.ConsumedValue = PaiseToRupees(taken)
	if s.CapValue > 0 {
		s.RemainingValue = math.Max(0, PaiseToRupees(RupeesToPaise(s.CapValue)-taken))
	}
	for _, l := range parseContractLines(data["lines"]) {
		s.Lines = append(s.Lines, ContractSummaryLine{Sku: l.Sku, Rate: l.Rate, CapQty: l.CapQty, ConsumedQty: takenQty[strings.ToLower(l.Sku)]})
	}
	sort.SliceStable(s.Releases, func(i, j int) bool { return s.Releases[i].Date > s.Releases[j].Date })
	return s, nil
}

func init() {
	// A release goes for approval one at a time per contract, so the cap
	// check below sees every release already waiting or approved.
	RegisterApprovalSubmitLock("PurchaseOrder", func(tenantID, docID string, data map[string]interface{}) string {
		if c := strField(data, "contract"); c != "" {
			return "purchase-contract:" + c
		}
		return ""
	})
	RegisterApprovalSubmitCheck("PurchaseOrder", func(tenantID, docID string, data map[string]interface{}) error {
		if c := strField(data, "contract"); c != "" {
			return checkContractCap(tenantID, c, docID, data)
		}
		return nil
	})
}
