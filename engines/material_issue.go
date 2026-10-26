package engines

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"custom_erp/db"
)

// Stage 130.15 (74.2.4, BRD 4.3; DEC-06 / DEC-07 at the BRD defaults): the
// internal Material Issue and Material Return. A department asks for stock
// for its own use (request → approve → issue, MI series) and gives back what
// it did not use (MR series, against the issue's lines).
//
//   - Each request carries department, employee, cost centre, project and
//     purpose; the GL row carries the cost centre and department.
//   - Item behaviour comes from 72.1's dimensions: a valuated item leaves
//     stock and books Dr its expense (the material_issue posting profile, else
//     the item's expense account) / Cr 1200 at the moving-average cost; a
//     non-valuated one moves quantity only; a non-stock item is refused, so no
//     phantom on-hand.
//   - One source per consumption (UAT-MAT-05): an item consumed by auto
//     backflush is refused here, and in the Consumption Entry too.
//   - The requester never approves their own request (dual control); an
//     approval rule's slabs route on the value.
//   - A return is at most what was issued less what came back, per line; a
//     damaged return goes to QC hold, not to sellable stock; it posts Dr 1200
//     / Cr the original expense at the issue's cost.

const (
	MaterialIssueDoctype  = "MaterialIssue"
	MaterialReturnDoctype = "MaterialReturn"
)

func init() {
	RegisterApprovalDecisionHook(MaterialIssueDoctype, true, func(tenantID, docID, decision, actorUserID string) {})
	RegisterReport(ReportDefinition{ID: "consumption-by-cost-centre", Label: "Consumption by cost centre", Category: "Inventory",
		Params:  []ReportParam{{Key: "from", Label: "From", Type: "date"}, {Key: "to", Label: "To", Type: "date"}},
		Columns: []ReportColumn{{Key: "cost_center", Label: "Cost centre"}, {Key: "department", Label: "Department"}, {Key: "issued", Label: "Issued"}, {Key: "returned", Label: "Returned"}, {Key: "net", Label: "Net consumption"}},
		Run:     consumptionByCostCentre})
	RegisterReport(ReportDefinition{ID: "material-issue-register", Label: "Material issue and return register", Category: "Inventory",
		Params: []ReportParam{{Key: "from", Label: "From", Type: "date"}, {Key: "to", Label: "To", Type: "date"}},
		Columns: []ReportColumn{{Key: "date", Label: "Date"}, {Key: "number", Label: "Document"}, {Key: "type", Label: "Type"}, {Key: "location", Label: "Store"},
			{Key: "department", Label: "Department"}, {Key: "cost_center", Label: "Cost centre"}, {Key: "sku", Label: "Item"}, {Key: "qty", Label: "Qty"}, {Key: "value", Label: "Value"}, {Key: "status", Label: "Status"}},
		Run: materialIssueRegister})
}

// MaterialRequestLine is one item asked for.
type MaterialRequestLine struct {
	SKU string `json:"sku"`
	Qty int    `json:"qty"`
}

// MaterialRequestInput is a department's request for stock.
type MaterialRequestInput struct {
	Location   string                `json:"location"`
	Department string                `json:"department"`
	Employee   string                `json:"employee"`
	CostCenter string                `json:"cost_center"`
	Project    string                `json:"project"`
	Purpose    string                `json:"purpose"`
	Lines      []MaterialRequestLine `json:"lines"`
}

func materialFail(sub, msg string) error {
	return &ValidationError{Code: "INVENT-1314", SubFor: sub, Message: msg}
}

// materialLinePolicy refuses what an internal issue may not take and returns
// the item's policy.
func materialLinePolicy(tenantID, sku string) (ItemPolicy, map[string]interface{}, string, error) {
	item, err := ResolveItemBySKU(tenantID, strings.TrimSpace(sku))
	if err != nil || item == nil {
		return ItemPolicy{}, nil, "", materialFail(sku, fmt.Sprintf("no item %q", sku))
	}
	code := firstNonEmpty(strField(item.Data, "code"), item.ID)
	p := ItemPolicyFor(tenantID, item.Data)
	switch {
	case p.StockTracking == StockNotTracked:
		return p, nil, code, materialFail(code, fmt.Sprintf("%s is not a stock item - nothing to issue from the store", code))
	case p.Consumption == ConsumeAutoBackflush:
		return p, nil, code, materialFail(code, fmt.Sprintf("%s is consumed by auto backflush - it is taken out of stock with what uses it, never issued by hand", code))
	}
	return p, item.Data, code, nil
}

// CreateMaterialRequest saves a request and, when an approval rule covers
// MaterialIssue, sends it for approval; with none it is Approved at once,
// ready to issue.
func CreateMaterialRequest(tenantID string, in MaterialRequestInput, userID, role string) (string, string, error) {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return "", "", err
	}
	location := DocumentStoreCode(tenantID, in.Location)
	if location == "" {
		return "", "", materialFail("location", "choose the store the material comes from")
	}
	if strings.TrimSpace(in.Department) == "" || strings.TrimSpace(in.Purpose) == "" {
		return "", "", materialFail("department", "say which department it is for, and what for")
	}
	// The GL takes only a registered, Active department and cost centre:
	// checked here, not found out at issue.
	if err := validateDepartmentReferenceInSchema(schema, strings.TrimSpace(in.Department)); err != nil {
		return "", "", materialFail("department", "choose the department from the Department list ("+err.Error()+")")
	}
	if err := validateCostCenterReferenceInSchema(schema, strings.TrimSpace(in.CostCenter)); err != nil {
		return "", "", materialFail("cost_center", "choose the cost centre from the Cost Centre list ("+err.Error()+")")
	}
	if cc := strings.TrimSpace(in.CostCenter); cc != "" {
		if ccData, _, err := fetchDocData(tenantID, "CostCenter", cc); err == nil && ccData != nil {
			if ent := strField(ccData, "legal_entity"); ent != "" && ent != locationLegalEntity(tenantID, location) {
				return "", "", materialFail("cost_center", "that cost centre belongs to another legal entity - move the stock there with a transfer instead")
			}
		}
	}
	if len(in.Lines) == 0 {
		return "", "", materialFail("lines", "add at least one item")
	}
	var lines []map[string]interface{}
	var value float64
	seen := map[string]bool{}
	for _, l := range in.Lines {
		p, item, sku, err := materialLinePolicy(tenantID, l.SKU)
		if err != nil {
			return "", "", err
		}
		if seen[sku] {
			return "", "", materialFail(sku, fmt.Sprintf("%s is on the request twice - put it on one line", sku))
		}
		seen[sku] = true
		if l.Qty <= 0 {
			return "", "", materialFail(sku, fmt.Sprintf("enter how many %s are needed", sku))
		}
		unit := 0.0
		if p.Valuated() {
			unit = adjustmentUnitCost(tenantID, sku, item)
		}
		lineValue := round2(unit * float64(l.Qty))
		value = round2(value + lineValue)
		lines = append(lines, map[string]interface{}{"sku": sku, "item_name": strField(item, "name"), "qty": l.Qty, "issued_qty": 0, "returned_qty": 0,
			"unit_cost": unit, "value": lineValue, "valuated": p.Valuated(), "expense_account": firstNonEmpty(p.ExpenseAccount, AccountConsumablesExpense)})
	}
	id := NewDocID("MI")
	encoded, _ := json.Marshal(lines)
	data := map[string]interface{}{"code": id, "location": location, "department": strings.TrimSpace(in.Department), "employee": strings.TrimSpace(in.Employee),
		"cost_center": strings.TrimSpace(in.CostCenter), "project": strings.TrimSpace(in.Project), "purpose": strings.TrimSpace(in.Purpose),
		"items": string(encoded), "amount": value, "requested_by": userID, "status": "Draft"}
	issueNumberInto(data, tenantID, MaterialIssueDoctype, id, "", location)
	raw, _ := json.Marshal(data)
	if _, err := db.DB.Exec(fmt.Sprintf(`INSERT INTO %s.documents (id, doctype, data, status, created_by) VALUES ($1, $2, $3, 'Draft', $4)`, schema),
		id, MaterialIssueDoctype, raw, userID); err != nil {
		return "", "", err
	}
	LogAuditEvent(tenantID, userID, "CREATE_MATERIAL_REQUEST", "SUCCESS", fmt.Sprintf("%s for %s (%s): %d line(s), %.2f", id, data["department"], data["purpose"], len(lines), value))
	if gated, _ := IsApprovalGated(tenantID, MaterialIssueDoctype); gated {
		if err := SubmitForApproval(tenantID, MaterialIssueDoctype, id, userID, role); err != nil {
			return id, "Draft", err
		}
		return id, "Pending Approval", nil
	}
	if err := setDocumentStatus(tenantID, MaterialIssueDoctype, id, "Approved", data); err != nil {
		return id, "Draft", err
	}
	return id, "Approved", nil
}

// IssueMaterial issues an Approved request: stock out and, for a valuated
// item, its cost from 1200 to the expense - in one transaction. Issuing an
// issued request again does nothing.
func IssueMaterial(tenantID, id, userID string) error {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return err
	}
	tx, err := db.DB.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err := db.SetSearchPath(tx, schema); err != nil {
		return err
	}
	var raw, status string
	if err := tx.QueryRow(fmt.Sprintf(`SELECT data, status FROM %s.documents WHERE doctype = $1 AND id = $2 FOR UPDATE`, schema), MaterialIssueDoctype, id).Scan(&raw, &status); err != nil {
		return materialFail(id, "no such material request")
	}
	if status == "Issued" || status == "Partly Returned" || status == "Returned" {
		return nil
	}
	if status != "Approved" {
		return materialFail(id, fmt.Sprintf("this request is %s - only an approved request is issued", status))
	}
	var data map[string]interface{}
	if err := json.Unmarshal([]byte(raw), &data); err != nil {
		return err
	}
	location := strField(data, "location")
	lines := jsonLines(data["items"])
	var moves []interface{}
	byAccount := map[string]int64{}
	for _, l := range lines {
		sku, qty := strField(l, "sku"), int(numFromInterface(l["qty"]))
		moves = append(moves, map[string]interface{}{"sku": sku, "qty": float64(-qty)})
		l["issued_qty"] = qty
		if truthy(l["valuated"]) {
			// The cost at issue, as an adjustment takes it; the request's own
			// estimate when the item has none today.
			itemData, _, _ := fetchDocData(tenantID, "Item", sku)
			unit := RupeesToPaise(firstPositive(adjustmentUnitCost(tenantID, sku, itemData), numFromInterface(l["unit_cost"])))
			cost := unit * int64(qty)
			l["unit_cost"], l["value"] = PaiseToRupees(unit), PaiseToRupees(cost)
			byAccount[strField(l, "expense_account")] += cost
		}
	}
	_, ledger, err := PostInventoryLedgerWithVoucherTx(tx, schema, location, moves, false)
	if err != nil {
		var short *InsufficientStockError
		if errors.As(err, &short) {
			return materialFail(short.SKU, fmt.Sprintf("only %d of %s are at %s to issue", short.Available, short.SKU, location))
		}
		return err
	}
	number := firstNonEmpty(strField(data, "code"), id)
	if err := WriteStockLedgerLinesTx(tx, schema, location, MaterialIssueDoctype, number, userID, "", ledger); err != nil {
		return err
	}
	opts := PostingOptions{CostCenter: strField(data, "cost_center"), Department: strField(data, "department")}
	pctx := PostingContext{LegalEntity: locationLegalEntity(tenantID, location)}
	for account, cost := range byAccount {
		if cost <= 0 {
			continue
		}
		acc := ResolvePostingAccounts(tenantID, PostingEventMaterialIssue, pctx, account, accountInventory)
		if err := PostDoubleEntryTx(tx, tenantID, schema, MaterialIssueDoctype, id, map[string]int64{acc.Debit: cost}, map[string]int64{acc.Credit: cost}, "",
			fmt.Sprintf("MaterialIssue:%s:%s", id, account), opts); err != nil {
			return err
		}
	}
	encoded, _ := json.Marshal(lines)
	patch, _ := json.Marshal(map[string]interface{}{"items": string(encoded), "status": "Issued", "issued_by": userID, "issued_at": time.Now().UTC().Format(time.RFC3339)})
	if _, err := tx.Exec(fmt.Sprintf(`UPDATE %s.documents SET status = 'Issued', data = data || $3::jsonb, updated_at = CURRENT_TIMESTAMP WHERE doctype = $1 AND id = $2`, schema),
		MaterialIssueDoctype, id, string(patch)); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	LogAuditEvent(tenantID, userID, "ISSUE_MATERIAL", "SUCCESS", fmt.Sprintf("%s issued at %s", number, location))
	return nil
}

// MaterialReturnLine is one item coming back.
type MaterialReturnLine struct {
	SKU       string `json:"sku"`
	Qty       int    `json:"qty"`
	Condition string `json:"condition"` // Good (default) or Damaged
}

// ReturnMaterial takes back unused material against an issue's lines - at
// most what was issued less what came back - as a Material Return. Good
// stock is sellable again; Damaged goes to QC hold. A valuated line posts Dr
// 1200 / Cr its original expense at the issue's cost.
func ReturnMaterial(tenantID, issueID string, in []MaterialReturnLine, userID string) (string, error) {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return "", err
	}
	if len(in) == 0 {
		return "", materialFail("lines", "say what is coming back")
	}
	tx, err := db.DB.Begin()
	if err != nil {
		return "", err
	}
	defer tx.Rollback()
	if err := db.SetSearchPath(tx, schema); err != nil {
		return "", err
	}
	var raw, status string
	if err := tx.QueryRow(fmt.Sprintf(`SELECT data, status FROM %s.documents WHERE doctype = $1 AND id = $2 FOR UPDATE`, schema), MaterialIssueDoctype, issueID).Scan(&raw, &status); err != nil {
		return "", materialFail(issueID, "no such material issue")
	}
	if status != "Issued" && status != "Partly Returned" {
		return "", materialFail(issueID, fmt.Sprintf("this request is %s - only issued material comes back", status))
	}
	var data map[string]interface{}
	if err := json.Unmarshal([]byte(raw), &data); err != nil {
		return "", err
	}
	location := strField(data, "location")
	issueLines := jsonLines(data["items"])
	bySku := map[string]map[string]interface{}{}
	for _, l := range issueLines {
		bySku[strField(l, "sku")] = l
	}
	var moves []interface{}
	var damaged []map[string]interface{}
	byAccount := map[string]int64{}
	var mrLines []map[string]interface{}
	for _, r := range in {
		l := bySku[strings.TrimSpace(r.SKU)]
		if l == nil {
			return "", materialFail(r.SKU, fmt.Sprintf("%s was not issued on this request", r.SKU))
		}
		open := int(numFromInterface(l["issued_qty"]) - numFromInterface(l["returned_qty"]))
		if r.Qty <= 0 || r.Qty > open {
			return "", materialFail(r.SKU, fmt.Sprintf("%s: %d can come back (issued %v, returned %v)", r.SKU, open, l["issued_qty"], l["returned_qty"]))
		}
		cond := "Good"
		if strings.EqualFold(strings.TrimSpace(r.Condition), "Damaged") {
			cond = "Damaged"
			damaged = append(damaged, map[string]interface{}{"sku": r.SKU, "qty": r.Qty})
		}
		moves = append(moves, map[string]interface{}{"sku": r.SKU, "qty": float64(r.Qty)})
		l["returned_qty"] = int(numFromInterface(l["returned_qty"])) + r.Qty
		value := 0.0
		if truthy(l["valuated"]) {
			cost := RupeesToPaise(numFromInterface(l["unit_cost"])) * int64(r.Qty)
			byAccount[strField(l, "expense_account")] += cost
			value = PaiseToRupees(cost)
		}
		mrLines = append(mrLines, map[string]interface{}{"sku": r.SKU, "item_name": l["item_name"], "qty": r.Qty, "condition": cond, "value": value})
	}
	_, ledger, err := PostInventoryLedgerWithVoucherTx(tx, schema, location, moves, false)
	if err != nil {
		return "", err
	}
	for _, d := range damaged {
		// Back on hand but not sellable: it waits in QC.
		if _, err := tx.Exec(fmt.Sprintf(`UPDATE %s.inventory_availability SET available = available - $3, qc_hold = COALESCE(qc_hold, 0) + $3, updated_at = CURRENT_TIMESTAMP
			WHERE sku = $1 AND location_code = $2`, schema), d["sku"], location, d["qty"]); err != nil {
			return "", err
		}
	}
	mrID := NewDocID("MR")
	mr := map[string]interface{}{"code": mrID, "issue_id": issueID, "issue_number": firstNonEmpty(strField(data, "code"), issueID), "location": location,
		"department": data["department"], "cost_center": data["cost_center"], "returned_by": userID, "status": "Posted"}
	enc, _ := json.Marshal(mrLines)
	mr["items"] = string(enc)
	issueNumberInto(mr, tenantID, MaterialReturnDoctype, mrID, "", location)
	mrRaw, _ := json.Marshal(mr)
	if _, err := tx.Exec(fmt.Sprintf(`INSERT INTO %s.documents (id, doctype, data, status, created_by) VALUES ($1, $2, $3, 'Posted', $4)`, schema), mrID, MaterialReturnDoctype, mrRaw, userID); err != nil {
		return "", err
	}
	if err := WriteStockLedgerLinesTx(tx, schema, location, MaterialReturnDoctype, firstNonEmpty(strField(mr, "code"), mrID), userID, "", ledger); err != nil {
		return "", err
	}
	opts := PostingOptions{CostCenter: strField(data, "cost_center"), Department: strField(data, "department")}
	pctx := PostingContext{LegalEntity: locationLegalEntity(tenantID, location)}
	for account, cost := range byAccount {
		if cost <= 0 {
			continue
		}
		acc := ResolvePostingAccounts(tenantID, PostingEventMaterialReturn, pctx, accountInventory, account)
		if err := PostDoubleEntryTx(tx, tenantID, schema, MaterialReturnDoctype, mrID, map[string]int64{acc.Debit: cost}, map[string]int64{acc.Credit: cost}, "",
			fmt.Sprintf("MaterialReturn:%s:%s", mrID, account), opts); err != nil {
			return "", err
		}
	}
	newStatus := "Returned"
	for _, l := range issueLines {
		if numFromInterface(l["returned_qty"]) < numFromInterface(l["issued_qty"]) {
			newStatus = "Partly Returned"
		}
	}
	encoded, _ := json.Marshal(issueLines)
	patch, _ := json.Marshal(map[string]interface{}{"items": string(encoded), "status": newStatus})
	if _, err := tx.Exec(fmt.Sprintf(`UPDATE %s.documents SET status = $3, data = data || $4::jsonb, updated_at = CURRENT_TIMESTAMP WHERE doctype = $1 AND id = $2`, schema),
		MaterialIssueDoctype, issueID, newStatus, string(patch)); err != nil {
		return "", err
	}
	if err := tx.Commit(); err != nil {
		return "", err
	}
	LogAuditEvent(tenantID, userID, "RETURN_MATERIAL", "SUCCESS", fmt.Sprintf("%s: %d line(s) back against %s", mrID, len(mrLines), issueID))
	return mrID, nil
}

// --- reports ----------------------------------------------------------------

func materialDocs(tenantID string, p map[string]string, doctype string, each func(at time.Time, data map[string]interface{}, status string)) error {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return err
	}
	rows, err := db.DB.Query(fmt.Sprintf(`SELECT created_at, data, status FROM %s.documents WHERE doctype = $1 AND deleted_at IS NULL
		AND ($2 = '' OR created_at >= public.erp_day_start($2::date)) AND ($3 = '' OR created_at < public.erp_day_start($3::date + 1)) ORDER BY created_at DESC LIMIT 5000`, schema),
		doctype, strings.TrimSpace(p["from"]), strings.TrimSpace(p["to"]))
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var at time.Time
		var raw, status string
		if err := rows.Scan(&at, &raw, &status); err != nil {
			return err
		}
		var data map[string]interface{}
		if json.Unmarshal([]byte(raw), &data) == nil {
			each(at, data, status)
		}
	}
	return rows.Err()
}

// consumptionByCostCentre (R-TX-04) is what each cost centre / department
// consumed: issued value less returned value.
func consumptionByCostCentre(tenantID string, p map[string]string) ([]map[string]interface{}, error) {
	type agg struct{ issued, returned float64 }
	sums := map[[2]string]*agg{}
	var order [][2]string
	add := func(data map[string]interface{}, issued, returned float64) {
		k := [2]string{strField(data, "cost_center"), strField(data, "department")}
		a := sums[k]
		if a == nil {
			a = &agg{}
			sums[k] = a
			order = append(order, k)
		}
		a.issued, a.returned = round2(a.issued+issued), round2(a.returned+returned)
	}
	if err := materialDocs(tenantID, p, MaterialIssueDoctype, func(at time.Time, data map[string]interface{}, status string) {
		if status != "Issued" && status != "Partly Returned" && status != "Returned" {
			return
		}
		v := 0.0
		for _, l := range jsonLines(data["items"]) {
			v += numFromInterface(l["value"])
		}
		add(data, v, 0)
	}); err != nil {
		return nil, err
	}
	if err := materialDocs(tenantID, p, MaterialReturnDoctype, func(at time.Time, data map[string]interface{}, status string) {
		v := 0.0
		for _, l := range jsonLines(data["items"]) {
			v += numFromInterface(l["value"])
		}
		add(data, 0, v)
	}); err != nil {
		return nil, err
	}
	out := []map[string]interface{}{}
	for _, k := range order {
		a := sums[k]
		out = append(out, map[string]interface{}{"cost_center": k[0], "department": k[1], "issued": a.issued, "returned": a.returned, "net": round2(a.issued - a.returned)})
	}
	return out, nil
}

// materialIssueRegister lists every issue and return line.
func materialIssueRegister(tenantID string, p map[string]string) ([]map[string]interface{}, error) {
	out := []map[string]interface{}{}
	row := func(kind string) func(at time.Time, data map[string]interface{}, status string) {
		return func(at time.Time, data map[string]interface{}, status string) {
			for _, l := range jsonLines(data["items"]) {
				qty := numFromInterface(l["qty"])
				if kind == "Return" {
					qty = -qty
				}
				out = append(out, map[string]interface{}{"date": at.Format("2006-01-02"), "number": strField(data, "code"), "type": kind, "location": strField(data, "location"),
					"department": strField(data, "department"), "cost_center": strField(data, "cost_center"), "sku": strField(l, "sku"), "qty": qty,
					"value": numFromInterface(l["value"]), "status": status})
			}
		}
	}
	if err := materialDocs(tenantID, p, MaterialIssueDoctype, row("Issue")); err != nil {
		return nil, err
	}
	if err := materialDocs(tenantID, p, MaterialReturnDoctype, row("Return")); err != nil {
		return nil, err
	}
	return out, nil
}
