package engines

import (
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log"
	"sort"
	"strings"
	"time"

	"custom_erp/db"
)

// Stage 89.8 - correcting an item's treatment after it has stock (D5, user
// 2026-10-18: "Admin change + correction ... Tell the impact in detail and show
// preview of impact and Admin approve and it will change ... Are you sure one
// ... May case come admin identify later after GRN" and "User can't do change
// if already posted. Only admin can initiate that. User will intimate.").
//
// Once an item has stock history, a change to its stock tracking, valuation
// or accounting treatment (directly, or by changing its kind of item / Item
// Type) is no longer a plain edit (MASTER-0407 keeps the generic save from
// doing it). An admin corrects it here instead:
//
//  1. Preview - old vs new, stock on hand per store, its value at moving-average
//     cost, the journal that will post, what changes from then on, open
//     documents touched, and anything that stops it.
//  2. Two confirmations - the reviewed preview with a reason, then the item
//     code typed. A token ties the confirmation to the numbers previewed: if
//     stock moved in between, the admin is asked to review again.
//  3. Applied in one transaction - stock written off / value moved through the
//     journal, the moving-average cost reset to match, the item (or type)
//     changed, and an ItemTreatmentCorrection record kept with the reason, the
//     preview and the entries. With "A second admin approves item treatment
//     corrections" on, the record waits in Approvals instead and is applied
//     when another admin approves it (the approval engine's maker-checker).

const (
	TreatmentCorrectionDoctype = "ItemTreatmentCorrection"
	accountInventoryControl    = "1200"
	accountFixedAssets         = "1400"
)

// TreatmentChange says what an item or an Item Type should become.
type TreatmentChange struct {
	Target         string   `json:"target"` // "Item" (default) or "ItemType"
	Ref            string   `json:"ref"`
	ItemType       string   `json:"item_type,omitempty"`
	StockTracking  string   `json:"stock_tracking,omitempty"`
	Valuation      string   `json:"valuation,omitempty"`
	Accounting     string   `json:"accounting_treatment,omitempty"`
	ExpenseAccount *string  `json:"expense_account,omitempty"`
	UnitValue      *float64 `json:"unit_value,omitempty"` // rupees per unit, when stock comes into Inventory
}

// TreatmentStore is one store's stock of an item.
type TreatmentStore struct {
	Location string `json:"location"`
	OnHand   int    `json:"on_hand"`
	Free     int    `json:"free"` // on hand less reserved, blocked, held, in QC or damaged
}

// TreatmentJournalLine is one line of the correcting journal.
type TreatmentJournalLine struct {
	Account string  `json:"account"`
	Name    string  `json:"name"`
	Debit   float64 `json:"debit"`
	Credit  float64 `json:"credit"`
}

// TreatmentItemImpact is what the correction does to one item.
type TreatmentItemImpact struct {
	Code            string           `json:"code"`
	Name            string           `json:"name"`
	Stores          []TreatmentStore `json:"stores"`
	OnHand          int              `json:"on_hand"`
	UnitValue       float64          `json:"unit_value"`
	UnitValueSource string           `json:"unit_value_source,omitempty"`
	Value           float64          `json:"value"`
	WriteOff        bool             `json:"write_off,omitempty"`
}

// TreatmentPreview is everything the admin reviews before confirming.
type TreatmentPreview struct {
	Target             string                 `json:"target"`
	Ref                string                 `json:"ref"`
	Code               string                 `json:"code"`
	Label              string                 `json:"label"`
	Before             map[string]string      `json:"before"`
	After              map[string]string      `json:"after"`
	Changes            string                 `json:"changes"`
	NoCorrectionNeeded bool                   `json:"no_correction_needed,omitempty"`
	Items              []TreatmentItemImpact  `json:"items"`
	Journal            []TreatmentJournalLine `json:"journal"`
	TotalValue         float64                `json:"total_value"`
	NeedsUnitValue     bool                   `json:"needs_unit_value,omitempty"`
	FromNow            []string               `json:"from_now"`
	OpenDocuments      []string               `json:"open_documents"`
	Blockers           []string               `json:"blockers"`
	SecondAdmin        bool                   `json:"second_admin"`
	Token              string                 `json:"token"`
}

type treatmentPlan struct {
	id, code, name string
	oldData        map[string]interface{}
	newData        map[string]interface{} // the Item as it will be saved (Item target only)
	before, after  ItemPolicy
	stores         []TreatmentStore
	onHand         int
	unitPaise      int64
	unitSource     string
	valuePaise     int64
	writeOff       bool
	debits         map[string]int64
	credits        map[string]int64
	costReset      *int64 // new avg unit cost in paise, when the item's cost must be reset
}

func inInventoryAccount(p ItemPolicy) bool {
	return p.Tracked() && p.Valuated() && p.Accounting == AccountingInventoryAsset
}

// valueAccount is where an item's stock value sits when it is not in Inventory.
func valueAccount(p ItemPolicy) string {
	if p.Accounting == AccountingFixedAsset {
		return accountFixedAssets
	}
	return p.ExpenseAccount
}

func copyData(m map[string]interface{}) map[string]interface{} {
	out := make(map[string]interface{}, len(m))
	for k, v := range m {
		out[k] = v
	}
	return out
}

// applyTreatmentDims writes the change's dimension fields onto a record.
func applyTreatmentDims(data map[string]interface{}, ch TreatmentChange) {
	set := func(k, v string) {
		if strings.TrimSpace(v) != "" {
			data[k] = strings.TrimSpace(v)
		}
	}
	set("stock_tracking", ch.StockTracking)
	set("valuation", ch.Valuation)
	set("accounting_treatment", ch.Accounting)
	if ch.ExpenseAccount != nil {
		data["expense_account"] = strings.TrimSpace(*ch.ExpenseAccount)
	}
}

func policySummary(p ItemPolicy, kind string) map[string]string {
	return map[string]string{
		"kind": kind, "commercial_class": policyLabel(p.Class), "stock_tracking": policyLabel(p.StockTracking),
		"valuation": policyLabel(p.Valuation), "accounting_treatment": policyLabel(p.Accounting),
		"sellability": policyLabel(p.Sellability), "expense_account": p.ExpenseAccount,
	}
}

func itemStores(schema, sku string) ([]TreatmentStore, int, error) {
	rows, err := db.DB.Query(fmt.Sprintf(`SELECT location_code, on_hand,
		GREATEST(available - blocked - hold_qty - qc_hold - damaged, 0)
		FROM %s.inventory_availability WHERE sku = $1 AND on_hand <> 0 ORDER BY location_code`, schema), sku)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	var out []TreatmentStore
	total := 0
	for rows.Next() {
		var s TreatmentStore
		if err := rows.Scan(&s.Location, &s.OnHand, &s.Free); err != nil {
			return nil, 0, err
		}
		if s.Free > s.OnHand {
			s.Free = s.OnHand
		}
		out = append(out, s)
		total += s.OnHand
	}
	return out, total, rows.Err()
}

func glAccountName(schema, code string) string {
	var name sql.NullString
	_ = db.DB.QueryRow(fmt.Sprintf(`SELECT account_name FROM %s.gl_accounts WHERE account_code = $1`, schema), code).Scan(&name)
	return name.String
}

// planTreatment works out one item's correction; blockers are appended.
func planTreatment(tenantID, schema string, p *treatmentPlan, ch TreatmentChange, blockers *[]string) error {
	label := p.name
	if label == "" {
		label = p.code
	}
	if (p.before.Accounting == AccountingStoredValue) != (p.after.Accounting == AccountingStoredValue) {
		*blockers = append(*blockers, fmt.Sprintf("%s: a gift card's issued balances are a liability to customers - it cannot become an ordinary item, or the other way round. Create a new item instead.", label))
	}
	stores, onHand, err := itemStores(schema, p.code)
	if err != nil {
		return err
	}
	p.stores, p.onHand = stores, onHand
	p.debits, p.credits = map[string]int64{}, map[string]int64{}
	if onHand < 0 {
		*blockers = append(*blockers, fmt.Sprintf("%s shows %d on hand somewhere - count it to the real quantity first.", label, onHand))
		return nil
	}
	wasIn, willBeIn := inInventoryAccount(p.before), inInventoryAccount(p.after)

	// Stock that keeps no stock any more leaves every store.
	if p.before.Tracked() && !p.after.Tracked() && onHand > 0 {
		p.writeOff = true
		for _, s := range stores {
			if s.Free < s.OnHand {
				*blockers = append(*blockers, fmt.Sprintf("%s: %d of the %d at %s are reserved, on hold, in QC or damaged - release or resolve them first.", label, s.OnHand-s.Free, s.OnHand, s.Location))
			}
		}
	}

	switch {
	case wasIn && !willBeIn && onHand > 0:
		unit, has, _ := GetItemUnitCost(tenantID, p.code)
		p.unitSource = "moving-average cost"
		if !has {
			if std, _ := parityNumber(p.oldData["standard_cost"]); std > 0 {
				unit, p.unitSource = RupeesToPaise(std), "standard cost (no receipt has been costed)"
			} else {
				unit, p.unitSource = 0, "no cost on record"
			}
		}
		p.unitPaise = unit
		p.valuePaise = unit * int64(onHand)
		if p.valuePaise > 0 {
			to := valueAccount(p.after)
			p.debits[to] += p.valuePaise
			p.credits[accountInventoryControl] += p.valuePaise
		}
		zero := int64(0)
		p.costReset = &zero
	case !wasIn && willBeIn && onHand > 0:
		if p.before.Accounting == AccountingFixedAsset {
			*blockers = append(*blockers, fmt.Sprintf("%s: fixed assets sit on the asset register - dispose of them there before they can become stock.", label))
			break
		}
		switch {
		case ch.UnitValue != nil && *ch.UnitValue >= 0:
			p.unitPaise, p.unitSource = RupeesToPaise(*ch.UnitValue), "entered"
		default:
			if std, _ := parityNumber(p.oldData["standard_cost"]); std > 0 {
				p.unitPaise, p.unitSource = RupeesToPaise(std), "standard cost"
			} else if unit, has, _ := GetItemUnitCost(tenantID, p.code); has && unit > 0 {
				p.unitPaise, p.unitSource = unit, "last costed receipt"
			} else {
				p.unitSource = "needed"
			}
		}
		p.valuePaise = p.unitPaise * int64(onHand)
		if p.valuePaise > 0 {
			from := valueAccount(p.before)
			p.debits[accountInventoryControl] += p.valuePaise
			p.credits[from] += p.valuePaise
		}
		unit := p.unitPaise
		p.costReset = &unit
	}
	for acct := range p.debits {
		if err := requireGLAccount(tenantID, acct); err != nil {
			*blockers = append(*blockers, fmt.Sprintf("%s: account %s is not in the chart of accounts - add it or choose another expense account.", label, acct))
		}
	}
	for acct := range p.credits {
		if err := requireGLAccount(tenantID, acct); err != nil {
			*blockers = append(*blockers, fmt.Sprintf("%s: account %s is not in the chart of accounts - add it or choose another expense account.", label, acct))
		}
	}
	return nil
}

// buildTreatmentPlans resolves the target and works out every affected item.
func buildTreatmentPlans(tenantID string, ch TreatmentChange) (*TreatmentPreview, []*treatmentPlan, map[string]interface{}, error) {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return nil, nil, nil, err
	}
	pv := &TreatmentPreview{Target: firstNonEmpty(ch.Target, "Item"), Ref: strings.TrimSpace(ch.Ref), Items: []TreatmentItemImpact{},
		Journal: []TreatmentJournalLine{}, FromNow: []string{}, OpenDocuments: []string{}, Blockers: []string{},
		SecondAdmin: GetSettingBool(tenantID, SettingTreatmentSecondAdmin)}
	var plans []*treatmentPlan
	var typeNew map[string]interface{}
	kindLabel := func(code string) string {
		if w := itemKindWords[strings.ToLower(code)]; w != "" {
			return w
		}
		return code
	}
	switch pv.Target {
	case "Item":
		item, err := ResolveItemBySKU(tenantID, pv.Ref)
		if err != nil || item == nil {
			return nil, nil, nil, &ValidationError{Code: "MASTER-0894", Message: fmt.Sprintf("There is no item %q.", pv.Ref)}
		}
		code := firstNonEmpty(strings.TrimSpace(pimString(item.Data["code"])), item.ID)
		newData := copyData(item.Data)
		if strings.TrimSpace(ch.ItemType) != "" {
			newData["item_type"] = strings.TrimSpace(ch.ItemType)
		}
		applyTreatmentDims(newData, ch)
		p := &treatmentPlan{id: item.ID, code: code, name: pimString(item.Data["name"]), oldData: item.Data, newData: newData,
			before: ItemPolicyFor(tenantID, item.Data), after: ItemPolicyFor(tenantID, newData)}
		plans = append(plans, p)
		pv.Code, pv.Label = code, firstNonEmpty(p.name, code)
		pv.Before = policySummary(p.before, kindLabel(ItemTypeName(item.Data)))
		pv.After = policySummary(p.after, kindLabel(ItemTypeName(newData)))
	case "ItemType":
		old, _, err := fetchDocData(tenantID, "ItemType", pv.Ref)
		if err != nil || old == nil {
			return nil, nil, nil, &ValidationError{Code: "MASTER-0894", Message: fmt.Sprintf("There is no item type %q.", pv.Ref)}
		}
		typeNew = copyData(old)
		applyTreatmentDims(typeNew, ch)
		oldRules, newRules := itemTypeRulesFromRecord(pv.Ref, old), itemTypeRulesFromRecord(pv.Ref, typeNew)
		pv.Code, pv.Label = pv.Ref, kindLabel(pv.Ref)
		pv.Before = policySummary(itemPolicyWithRules(map[string]interface{}{}, oldRules), pv.Label)
		pv.After = policySummary(itemPolicyWithRules(map[string]interface{}{}, newRules), pv.Label)
		rows, err := db.DB.Query(fmt.Sprintf(`SELECT id, data::text FROM %s.documents WHERE doctype = 'Item' AND deleted_at IS NULL
			AND lower(CASE WHEN COALESCE(data->>'item_type', '') IN ('', 'Stock') THEN 'Product' ELSE data->>'item_type' END) = lower($1)`, schema), pv.Ref)
		if err != nil {
			return nil, nil, nil, err
		}
		for rows.Next() {
			var id, raw string
			if err := rows.Scan(&id, &raw); err != nil {
				rows.Close()
				return nil, nil, nil, err
			}
			var d map[string]interface{}
			if json.Unmarshal([]byte(raw), &d) != nil {
				continue
			}
			p := &treatmentPlan{id: id, code: firstNonEmpty(strings.TrimSpace(pimString(d["code"])), id), name: pimString(d["name"]), oldData: d,
				before: itemPolicyWithRules(d, oldRules), after: itemPolicyWithRules(d, newRules)}
			if postingTreatmentChanged(p.before, p.after) {
				plans = append(plans, p)
			}
		}
		rows.Close()
	default:
		return nil, nil, nil, &ValidationError{Code: "MASTER-0894", Message: "Choose an item or an item type to correct."}
	}

	var bp, ap ItemPolicy
	if len(plans) > 0 {
		bp, ap = plans[0].before, plans[0].after
	}
	if pv.Target == "Item" && !postingTreatmentChanged(bp, ap) {
		pv.NoCorrectionNeeded = true
	}
	if pv.Target == "ItemType" && len(plans) == 0 {
		pv.NoCorrectionNeeded = true
	}
	pv.Changes = describeTreatmentChange(bp, ap)
	if pv.Target == "ItemType" {
		pv.Changes = describeTreatmentChange(itemPolicyWithRules(map[string]interface{}{}, ItemTypeRulesFor(tenantID, pv.Ref)),
			itemPolicyWithRules(map[string]interface{}{}, itemTypeRulesFromRecord(pv.Ref, typeNew)))
	}

	byAccount := map[string][2]int64{}
	var total int64
	for _, p := range plans {
		if err := planTreatment(tenantID, schema, p, ch, &pv.Blockers); err != nil {
			return nil, nil, nil, err
		}
		if p.unitSource == "needed" && p.onHand > 0 {
			pv.NeedsUnitValue = true
		}
		pv.Items = append(pv.Items, TreatmentItemImpact{Code: p.code, Name: p.name, Stores: p.stores, OnHand: p.onHand,
			UnitValue: PaiseToRupees(p.unitPaise), UnitValueSource: p.unitSource, Value: PaiseToRupees(p.valuePaise), WriteOff: p.writeOff})
		for a, v := range p.debits {
			e := byAccount[a]
			e[0] += v
			byAccount[a] = e
		}
		for a, v := range p.credits {
			e := byAccount[a]
			e[1] += v
			byAccount[a] = e
		}
		total += p.valuePaise
	}
	accts := make([]string, 0, len(byAccount))
	for a := range byAccount {
		accts = append(accts, a)
	}
	sort.Strings(accts)
	for _, a := range accts {
		e := byAccount[a]
		pv.Journal = append(pv.Journal, TreatmentJournalLine{Account: a, Name: glAccountName(schema, a), Debit: PaiseToRupees(e[0]), Credit: PaiseToRupees(e[1])})
	}
	pv.TotalValue = PaiseToRupees(total)
	if pv.NeedsUnitValue {
		pv.Blockers = append(pv.Blockers, "Enter the value per unit of the stock coming into Inventory - there is no standard cost or costed receipt to propose.")
	}
	pv.FromNow = treatmentFromNow(ap)
	pv.OpenDocuments = treatmentOpenDocuments(schema, plans)

	h := sha256.New()
	fmt.Fprintf(h, "%s|%s|%s|%s", pv.Target, pv.Ref, pv.Changes, pv.Before["kind"]+">"+pv.After["kind"])
	for _, p := range plans {
		fmt.Fprintf(h, "|%s:%d:%d:%d:%v", p.code, p.onHand, p.unitPaise, p.valuePaise, p.stores)
	}
	pv.Token = hex.EncodeToString(h.Sum(nil))[:20]
	return pv, plans, typeNew, nil
}

// treatmentFromNow says, in plain words, how the item behaves after the change.
func treatmentFromNow(p ItemPolicy) []string {
	var out []string
	switch {
	case !p.Tracked():
		out = append(out, fmt.Sprintf("No stock is kept. A GRN posts its value straight to %s.", valueAccount(p)))
	case p.Valuated() && p.Accounting == AccountingInventoryAsset:
		out = append(out, "Stock is kept with its value in Inventory (1200) at moving-average cost; a sale moves that cost to cost of goods sold.")
	default:
		out = append(out, fmt.Sprintf("Quantity is kept but carries no value: a GRN expenses it to %s, and an issue costs nothing.", valueAccount(p)))
	}
	out = append(out, fmt.Sprintf("At the till: %s.", strings.ToLower(policyLabel(p.Sellability))))
	out = append(out, fmt.Sprintf("Consumption: %s.", strings.ToLower(policyLabel(p.Consumption))))
	out = append(out, "Bills, receipts and journals already posted stay as they were.")
	return out
}

// treatmentOpenDocuments lists unfinished documents that name the items.
func treatmentOpenDocuments(schema string, plans []*treatmentPlan) []string {
	out := []string{}
	for _, p := range plans {
		rows, err := db.DB.Query(fmt.Sprintf(`SELECT doctype, id, COALESCE(status, '') FROM %s.documents
			WHERE doctype IN ('GRN', 'PurchaseOrder', 'TransferOrder', 'SalesOrder', 'PurchaseReturn')
			  AND deleted_at IS NULL AND COALESCE(status, '') NOT IN ('Cancelled', 'Closed', 'Completed', 'Posted', 'Received', 'Rejected', 'Delivered', 'Fulfilled')
			  AND position($1 in data::text) > 0
			ORDER BY created_at DESC LIMIT 10`, schema), `"`+p.code+`"`)
		if err != nil {
			continue
		}
		for rows.Next() {
			var dt, id, st string
			if rows.Scan(&dt, &id, &st) == nil {
				out = append(out, fmt.Sprintf("%s %s (%s) - %s", dt, id, firstNonEmpty(st, "open"), p.code))
			}
		}
		rows.Close()
	}
	return out
}

// PreviewTreatmentCorrection is step 1.
func PreviewTreatmentCorrection(tenantID string, ch TreatmentChange) (*TreatmentPreview, error) {
	pv, _, _, err := buildTreatmentPlans(tenantID, ch)
	return pv, err
}

// TreatmentApplyRequest is step 2: the change, the reason and the typed code.
type TreatmentApplyRequest struct {
	TreatmentChange
	Reason  string `json:"reason"`
	Confirm string `json:"confirm"`
	Token   string `json:"token"`
}

// TreatmentApplyResult says what happened.
type TreatmentApplyResult struct {
	ID      string            `json:"id"`
	Status  string            `json:"status"` // "Applied" or "Pending Approval"
	Preview *TreatmentPreview `json:"preview"`
}

// ApplyTreatmentCorrection checks the two confirmations and applies - or, with
// a second admin required, files the request for approval.
func ApplyTreatmentCorrection(tenantID, userID, userName, role string, req TreatmentApplyRequest) (*TreatmentApplyResult, error) {
	pv, _, _, err := buildTreatmentPlans(tenantID, req.TreatmentChange)
	if err != nil {
		return nil, err
	}
	if pv.NoCorrectionNeeded {
		return nil, &ValidationError{Code: "MASTER-0893", Message: "Nothing to correct - this change does not touch stock tracking, valuation or accounting. Save it on the form."}
	}
	if len(pv.Blockers) > 0 {
		return nil, &ValidationError{Code: "MASTER-0894", Message: strings.Join(pv.Blockers, " ")}
	}
	if strings.TrimSpace(req.Token) == "" || req.Token != pv.Token {
		return nil, &ValidationError{Code: "MASTER-0895", Message: "The stock or cost changed since you reviewed the impact - review it again before confirming."}
	}
	if strings.TrimSpace(req.Reason) == "" {
		return nil, &ValidationError{Code: "MASTER-0893", SubFor: "reason", Message: "Give the reason for the correction."}
	}
	if !strings.EqualFold(strings.TrimSpace(req.Confirm), pv.Code) {
		return nil, &ValidationError{Code: "MASTER-0893", SubFor: "confirm", Message: fmt.Sprintf("Type %s exactly to confirm.", pv.Code)}
	}
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return nil, err
	}
	id := NewDocIDCompact("ITC")
	changeJSON, _ := json.Marshal(req.TreatmentChange)
	previewJSON, _ := json.Marshal(pv)
	record := map[string]interface{}{
		"target": pv.Target, "item": pv.Code, "item_name": pv.Label, "change_summary": pv.Changes,
		"from_kind": pv.Before["kind"], "to_kind": pv.After["kind"], "stock_value": pv.TotalValue,
		"reason": strings.TrimSpace(req.Reason), "requested_by": userName, "requested_at": time.Now().UTC().Format(time.RFC3339),
		"change": string(changeJSON), "preview": string(previewJSON),
	}
	if pv.SecondAdmin {
		record["status"] = "Draft"
		raw, _ := json.Marshal(record)
		if _, err := db.DB.Exec(fmt.Sprintf(`INSERT INTO %s.documents (id, doctype, data, status, created_by) VALUES ($1, $2, $3::jsonb, 'Draft', $4)`, schema),
			id, TreatmentCorrectionDoctype, string(raw), userID); err != nil {
			return nil, err
		}
		if err := SubmitForApproval(tenantID, TreatmentCorrectionDoctype, id, userID, role); err != nil {
			return nil, err
		}
		LogAuditEvent(tenantID, userID, "ITEM_TREATMENT_CORRECTION_REQUESTED", "SUCCESS", fmt.Sprintf("%s %s: %s (%s) - waiting for a second admin", pv.Target, pv.Code, pv.Changes, record["reason"]))
		return &TreatmentApplyResult{ID: id, Status: "Pending Approval", Preview: pv}, nil
	}
	applied, err := commitTreatmentCorrection(tenantID, id, userID, userName, req.TreatmentChange, record, false)
	if err != nil {
		return nil, err
	}
	return &TreatmentApplyResult{ID: id, Status: "Applied", Preview: applied}, nil
}

// commitTreatmentCorrection recomputes the plans from the current stock and
// applies them in one transaction. existing: the record is already filed
// (approved request); else it is written here, Applied.
func commitTreatmentCorrection(tenantID, docID, actorID, actorName string, ch TreatmentChange, record map[string]interface{}, existing bool) (*TreatmentPreview, error) {
	pv, plans, typeNew, err := buildTreatmentPlans(tenantID, ch)
	if err != nil {
		return nil, err
	}
	if len(pv.Blockers) > 0 {
		return nil, &ValidationError{Code: "MASTER-0894", Message: strings.Join(pv.Blockers, " ")}
	}
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return nil, err
	}
	tx, err := db.DB.Begin()
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	if err := db.SetSearchPath(tx, schema); err != nil {
		return nil, err
	}
	type postedAt struct {
		location string
		lines    []PostedStockLine
	}
	var posted []postedAt
	for i, p := range plans {
		if p.writeOff {
			for _, s := range p.stores {
				if s.OnHand <= 0 {
					continue
				}
				_, lines, err := PostInventoryLedgerWithVoucherTx(tx, schema, s.Location, []interface{}{map[string]interface{}{"sku": p.code, "qty": -s.OnHand}}, false)
				if err != nil {
					return nil, err
				}
				posted = append(posted, postedAt{s.Location, lines})
			}
		}
		if len(p.debits) > 0 {
			if err := PostDoubleEntryTx(tx, tenantID, schema, TreatmentCorrectionDoctype, docID, p.debits, p.credits, "",
				fmt.Sprintf("%s:%s:%s:%d", TreatmentCorrectionDoctype, docID, p.code, i)); err != nil {
				return nil, err
			}
		}
		if p.costReset != nil {
			qty := p.onHand
			if !p.after.Tracked() {
				qty = 0
			}
			if _, err := tx.Exec(fmt.Sprintf(`INSERT INTO %[1]s.item_cost (item_code, cumulative_qty_received, cumulative_value_received_paise, avg_unit_cost_paise)
				VALUES ($1, $2::numeric, $3::bigint, $4::bigint)
				ON CONFLICT (item_code) DO UPDATE SET cumulative_qty_received = EXCLUDED.cumulative_qty_received,
					cumulative_value_received_paise = EXCLUDED.cumulative_value_received_paise,
					avg_unit_cost_paise = EXCLUDED.avg_unit_cost_paise, updated_at = CURRENT_TIMESTAMP`, schema),
				p.code, qty, *p.costReset*int64(qty), *p.costReset); err != nil {
				return nil, err
			}
		}
		if p.newData != nil {
			raw, _ := json.Marshal(p.newData)
			if _, err := tx.Exec(fmt.Sprintf(`UPDATE %s.documents SET data = $2::jsonb, updated_at = CURRENT_TIMESTAMP, version = version + 1 WHERE id = $1`, schema), p.id, string(raw)); err != nil {
				return nil, err
			}
		}
	}
	if typeNew != nil {
		raw, _ := json.Marshal(typeNew)
		if _, err := tx.Exec(fmt.Sprintf(`UPDATE %s.documents SET data = $2::jsonb, updated_at = CURRENT_TIMESTAMP, version = version + 1 WHERE doctype = 'ItemType' AND id = $1`, schema), pv.Ref, string(raw)); err != nil {
			return nil, err
		}
	}
	appliedJSON, _ := json.Marshal(pv)
	record["applied"] = string(appliedJSON)
	record["applied_by"] = actorName
	record["applied_at"] = time.Now().UTC().Format(time.RFC3339)
	record["stock_value"] = pv.TotalValue
	if existing {
		raw, _ := json.Marshal(record)
		if _, err := tx.Exec(fmt.Sprintf(`UPDATE %s.documents SET data = data || $2::jsonb, updated_at = CURRENT_TIMESTAMP WHERE id = $1`, schema), docID, string(raw)); err != nil {
			return nil, err
		}
	} else {
		record["status"] = "Applied"
		raw, _ := json.Marshal(record)
		if _, err := tx.Exec(fmt.Sprintf(`INSERT INTO %s.documents (id, doctype, data, status, created_by) VALUES ($1, $2, $3::jsonb, 'Applied', $4)`, schema),
			docID, TreatmentCorrectionDoctype, string(raw), actorID); err != nil {
			return nil, err
		}
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	for _, pa := range posted {
		WriteStockLedgerLines(tenantID, pa.location, TreatmentCorrectionDoctype, docID, actorName, pa.lines)
	}
	if typeNew != nil {
		InvalidateItemTypeCache(tenantID)
	}
	LogAuditEvent(tenantID, actorID, "ITEM_TREATMENT_CORRECTED", "SUCCESS", fmt.Sprintf("%s %s: %s; %d item(s), stock value %.2f moved; reason: %v",
		pv.Target, pv.Code, pv.Changes, len(plans), pv.TotalValue, record["reason"]))
	return pv, nil
}

// applyApprovedTreatmentCorrection is the approval engine's hook: a second
// admin approved the request, so it is applied now, on today's stock.
func applyApprovedTreatmentCorrection(tenantID, docID, actorUserID string) {
	data, _, err := fetchDocData(tenantID, TreatmentCorrectionDoctype, docID)
	if err != nil || data == nil {
		log.Printf("[ITEM TREATMENT] approved correction %s not found: %v", docID, err)
		return
	}
	var ch TreatmentChange
	if json.Unmarshal([]byte(pimString(data["change"])), &ch) != nil {
		log.Printf("[ITEM TREATMENT] approved correction %s has no readable change", docID)
		return
	}
	if _, err := commitTreatmentCorrection(tenantID, docID, actorUserID, actorUserID, ch, data, true); err != nil {
		schema, serr := db.GetTenantSchema(tenantID)
		if serr == nil {
			msg, _ := json.Marshal(map[string]string{"apply_error": err.Error()})
			_, _ = db.DB.Exec(fmt.Sprintf(`UPDATE %s.documents SET data = data || $2::jsonb WHERE id = $1`, schema), docID, string(msg))
		}
		LogAuditEvent(tenantID, actorUserID, "ITEM_TREATMENT_CORRECTED", "FAILURE", fmt.Sprintf("%s approved but not applied: %v", docID, err))
	}
}

// ItemHasStockHistory says whether an item's treatment is locked to the
// correction (any stock ledger entry names it).
func ItemHasStockHistory(tenantID, ref string) bool {
	item, err := ResolveItemBySKU(tenantID, strings.TrimSpace(ref))
	if err != nil || item == nil {
		return false
	}
	return skusHaveStockHistory(tenantID, firstNonEmpty(strings.TrimSpace(pimString(item.Data["code"])), item.ID))
}

// ItemTypeHasStock says whether any item of this type has stock history.
func ItemTypeHasStock(tenantID, typeName string) bool {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return false
	}
	var used bool
	_ = db.DB.QueryRow(fmt.Sprintf(`
		SELECT EXISTS (
		  SELECT 1 FROM %[1]s.documents i
		  JOIN %[1]s.documents s ON s.doctype = 'StockLedgerEntry' AND s.data->>'item_id' = i.data->>'code'
		  WHERE i.doctype = 'Item' AND i.deleted_at IS NULL
		    AND lower(CASE WHEN COALESCE(i.data->>'item_type', '') IN ('', 'Stock') THEN 'Product' ELSE i.data->>'item_type' END) = lower($1))`, schema),
		strings.TrimSpace(typeName)).Scan(&used)
	return used
}
