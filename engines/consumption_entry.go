package engines

import (
	"crypto/sha256"
	"custom_erp/db"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"
)

// Consumption Entry (Stage 72.6, the user's blueprint 9.6 and FR-012): for
// what is consumed without a sale to hang it on.
//
//   - Periodic: butter paper, tape, ribbon. The store counts what is left;
//     consumption = book stock - counted closing stock (book already nets
//     opening, receipts, transfers and adjustments), posted in one go.
//   - Manual issue: the quantity a store took for its own use.
//
// Each line leaves stock through the normal ledger; a valuated item moves its
// moving-average cost from 1200 Inventory to its profile's expense account, a
// non-valuated one moves quantity only (expensed when received). The entry
// keeps its count reference and who posted (approved) it.

const (
	ConsumptionModePeriodic = "Periodic count"
	ConsumptionModeManual   = "Manual issue"
)

// ConsumptionEntryRequest is one store's entry.
type ConsumptionEntryRequest struct {
	Location       string                 `json:"location"`
	Mode           string                 `json:"mode"`
	CountReference string                 `json:"count_reference"`
	Reason         string                 `json:"reason"`
	IdempotencyKey string                 `json:"idempotency_key"`
	Lines          []ConsumptionEntryLine `json:"lines"`
}

// ConsumptionEntryLine is one item: the counted closing quantity (periodic) or
// the quantity used (manual issue).
type ConsumptionEntryLine struct {
	SKU        string `json:"sku"`
	CountedQty *int   `json:"counted_qty,omitempty"`
	IssuedQty  int    `json:"issued_qty,omitempty"`
}

// ConsumptionEntryResult is what was posted.
type ConsumptionEntryResult struct {
	ID        string                   `json:"id"`
	Number    string                   `json:"number,omitempty"` // Stage 76.11: the CE series number
	Location  string                   `json:"location"`
	Mode      string                   `json:"mode"`
	Lines     []map[string]interface{} `json:"lines"`
	CostTotal float64                  `json:"cost_total"`
	Replayed  bool                     `json:"replayed,omitempty"`
}

// BookStockAt is the store's on-hand-available quantity of a SKU.
func BookStockAt(tenantID, sku, location string) (int, error) {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return 0, err
	}
	var v int
	err = db.DB.QueryRow(fmt.Sprintf(`SELECT available FROM %s.inventory_availability WHERE sku = $1 AND location_code = $2`, schema), sku, location).Scan(&v)
	if err == sql.ErrNoRows {
		return 0, nil
	}
	return v, err
}

// PostConsumptionEntry validates and posts an entry in one transaction.
func PostConsumptionEntry(tenantID, userName string, req ConsumptionEntryRequest) (*ConsumptionEntryResult, error) {
	fail := func(sub, msg string) error {
		return &ValidationError{Code: "INVENT-0422", SubFor: sub, Message: msg}
	}
	req.Location = strings.TrimSpace(req.Location)
	if req.Location == "" {
		return nil, fail("location", "choose the store whose stock was used")
	}
	if loc, _, err := fetchDocData(tenantID, "Location", req.Location); err != nil || loc == nil {
		return nil, fail("location", fmt.Sprintf("no store %q", req.Location))
	}
	switch policyKey(req.Mode) {
	case policyKey(ConsumptionModePeriodic), "periodic":
		req.Mode = ConsumptionModePeriodic
	case policyKey(ConsumptionModeManual), "manual":
		req.Mode = ConsumptionModeManual
	default:
		return nil, fail("mode", "choose Periodic count or Manual issue")
	}
	if len(req.Lines) == 0 {
		return nil, fail("lines", "add at least one item")
	}
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return nil, err
	}

	id := NewDocIDCompact("CONS")
	if key := strings.TrimSpace(req.IdempotencyKey); key != "" {
		sum := sha256.Sum256([]byte(tenantID + "|consumption|" + key))
		id = "CONS-" + strings.ToUpper(hex.EncodeToString(sum[:8]))
		if data, _, err := fetchDocData(tenantID, "ConsumptionEntry", id); err == nil && data != nil {
			return consumptionResultFromData(id, data, true), nil
		}
	}

	type plan struct {
		sku, name           string
		book, counted, used int
		policy              ItemPolicy
	}
	seen := map[string]bool{}
	var plans []plan
	for _, l := range req.Lines {
		resolved, err := ResolveItemBySKU(tenantID, strings.TrimSpace(l.SKU))
		if err != nil || resolved == nil {
			return nil, fail(l.SKU, fmt.Sprintf("no item %q", l.SKU))
		}
		sku := strings.TrimSpace(pimString(resolved.Data["code"]))
		if sku == "" {
			sku = resolved.ID
		}
		if seen[strings.ToLower(sku)] {
			return nil, fail(sku, fmt.Sprintf("%s is on the entry twice - put it on one line", sku))
		}
		seen[strings.ToLower(sku)] = true
		if err := RefuseNonStockItems(tenantID, []string{sku}, "consumed from a store"); err != nil {
			return nil, err
		}
		// Stage 130.15 (74.2.4, UAT-MAT-05): one source per consumption - an
		// item consumed by auto backflush is never entered by hand as well.
		if ItemPolicyFor(tenantID, resolved.Data).Consumption == ConsumeAutoBackflush {
			return nil, fail(sku, fmt.Sprintf("%s is consumed by auto backflush with what uses it - it is not entered here as well", sku))
		}
		book, err := BookStockAt(tenantID, sku, req.Location)
		if err != nil {
			return nil, err
		}
		p := plan{sku: sku, name: itemLabel(resolved.Data, sku), book: book, policy: ItemPolicyFor(tenantID, resolved.Data)}
		if req.Mode == ConsumptionModePeriodic {
			if l.CountedQty == nil || *l.CountedQty < 0 {
				return nil, fail(sku, fmt.Sprintf("enter the counted closing quantity of %s (0 or more)", p.name))
			}
			p.counted = *l.CountedQty
			if p.counted > book {
				return nil, fail(sku, fmt.Sprintf("%s: counted %d but the book shows %d - receive the difference (GRN) before posting consumption", p.name, p.counted, book))
			}
			p.used = book - p.counted
		} else {
			if l.IssuedQty <= 0 {
				return nil, fail(sku, fmt.Sprintf("enter how many %s were used", p.name))
			}
			if l.IssuedQty > book {
				return nil, fail(sku, fmt.Sprintf("%s: %d used but the store has %d", p.name, l.IssuedQty, book))
			}
			p.used = l.IssuedQty
		}
		plans = append(plans, p)
	}

	tx, err := db.DB.Begin()
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	if err := db.SetSearchPath(tx, schema); err != nil {
		return nil, err
	}
	var stock []interface{}
	byAccount := map[string]int64{}
	var costTotal int64
	lines := make([]map[string]interface{}, 0, len(plans))
	for _, p := range plans {
		line := map[string]interface{}{"sku": p.sku, "name": p.name, "book_qty": p.book, "consumed_qty": p.used}
		if req.Mode == ConsumptionModePeriodic {
			line["counted_qty"] = p.counted
		}
		if p.used > 0 {
			stock = append(stock, map[string]interface{}{"sku": p.sku, "qty": -p.used})
			if p.policy.Valuated() {
				unit, _ := ResolveQuoteUnitCostPaise(tenantID, p.sku)
				cost := unit * int64(p.used)
				byAccount[p.policy.ExpenseAccount] += cost
				costTotal += cost
				line["unit_cost"] = PaiseToRupees(unit)
				line["cost"] = PaiseToRupees(cost)
				line["expense_account"] = p.policy.ExpenseAccount
			} else {
				line["valuation"] = "Non-valuated (expensed at receipt)"
			}
		}
		lines = append(lines, line)
	}
	_, ledger, err := PostInventoryLedgerWithVoucherTx(tx, schema, req.Location, stock, false)
	if err != nil {
		return nil, err
	}
	accounts := make([]string, 0, len(byAccount))
	for a := range byAccount {
		accounts = append(accounts, a)
	}
	sort.Strings(accounts)
	for _, acct := range accounts {
		if byAccount[acct] <= 0 {
			continue
		}
		if err := PostDoubleEntryTx(tx, tenantID, schema, "ConsumptionEntry", id,
			map[string]int64{acct: byAccount[acct]}, map[string]int64{"1200": byAccount[acct]},
			"", fmt.Sprintf("ConsumptionEntry:%s:%s", id, acct)); err != nil {
			return nil, err
		}
	}
	data := map[string]interface{}{
		"location": req.Location, "mode": req.Mode, "count_reference": strings.TrimSpace(req.CountReference),
		"reason": strings.TrimSpace(req.Reason), "lines": lines, "cost_total": PaiseToRupees(costTotal),
		"posted_by": userName, "approved_by": userName, "posted_at": time.Now().UTC().Format(time.RFC3339), "status": "Posted",
	}
	// Stage 76.9: the replay key in a field of its own, so the id need not
	// carry it once the entry is numbered from a series.
	if key := strings.TrimSpace(req.IdempotencyKey); key != "" {
		data["idempotency_key"] = key
	}
	issueNumberInto(data, tenantID, "ConsumptionEntry", id, "", req.Location) // Stage 76.11
	raw, _ := json.Marshal(data)
	if _, err := tx.Exec(fmt.Sprintf(`INSERT INTO %s.documents (id, doctype, data, status, created_by) VALUES ($1, 'ConsumptionEntry', $2, 'Posted', $3)`, schema), id, raw, userName); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	WriteStockLedgerLines(tenantID, req.Location, "ConsumptionEntry", id, userName, ledger)
	return &ConsumptionEntryResult{ID: id, Number: pimString(data["code"]), Location: req.Location, Mode: req.Mode, Lines: lines, CostTotal: PaiseToRupees(costTotal)}, nil
}

func consumptionResultFromData(id string, data map[string]interface{}, replayed bool) *ConsumptionEntryResult {
	out := &ConsumptionEntryResult{ID: id, Number: pimString(data["code"]), Location: pimString(data["location"]), Mode: pimString(data["mode"]), CostTotal: numFromInterface(data["cost_total"]), Replayed: replayed}
	if arr, ok := data["lines"].([]interface{}); ok {
		for _, v := range arr {
			if m, ok := v.(map[string]interface{}); ok {
				out.Lines = append(out.Lines, m)
			}
		}
	}
	return out
}
