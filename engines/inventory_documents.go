package engines

import (
	"context"
	"custom_erp/db"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"strings"
	"time"
)

// Stage 88.5 (D9, D13, D14): Inventory as an ERP module - its dashboard, and
// the two documents it had none of: the stock adjustment and the MRP Change.
//
// The adjustment is 74.2.3's InventoryAdjustment on 76.12's ADJ series, built
// here to D13 (a reason per line, the approval rules, stock ledger + GL at the
// moving-average cost); 74.2.3 extends it - lots / serials / bins, the preview
// and stale-snapshot refusal, reversal, posting profiles (74.1.3) in place of
// the fixed 1200 / 5190 below, and the cycle count's GL leg.
// Both are engine-written (their routes validate and post; the generic record
// API only reads them) and both are list-first pages with + New.

const (
	InventoryAdjustmentDoctype = "InventoryAdjustment"
	MRPChangeDoctype           = "MRPChange"

	// accountStockAdjustment carries what an adjustment adds to or takes from
	// stock value (migrations_stage88_5_inventory_adjustment.sql).
	accountStockAdjustment = "5190"
	accountInventory       = "1200"

	mrpChangeJobType = "inventory_mrp_change_apply"
)

// InventoryAdjustmentLine is one item's change: a signed quantity (+ found or
// received outside a document, - lost, damaged, expired) and its reason.
// Stage 130.14 (74.2.3b): optionally the bin, the lot and the serial numbers
// it is, and Condition "Damaged" - a damaged unit still owned moves out of
// sellable stock into Damaged (qty < 0) or back (qty > 0), with no write-off.
type InventoryAdjustmentLine struct {
	SKU        string   `json:"sku"`
	Qty        int      `json:"qty"`
	ReasonCode string   `json:"reason_code"`
	Note       string   `json:"note"`
	Bin        string   `json:"bin,omitempty"`
	BatchNo    string   `json:"batch_no,omitempty"`
	Serials    []string `json:"serials,omitempty"`
	Condition  string   `json:"condition,omitempty"`
}

type InventoryAdjustmentInput struct {
	Location string                    `json:"location"`
	Note     string                    `json:"note"`
	Lines    []InventoryAdjustmentLine `json:"lines"`
	// LossClass (Stage 74.2.3): Shrinkage, Damage, Expiry or Theft - a Damage or
	// Expiry loss posts through its own posting-profile event.
	LossClass string `json:"loss_class,omitempty"`
	// ReversalOf (Stage 130.14) is the posted adjustment this one reverses.
	ReversalOf string `json:"-"`
}

// AdjustmentConditionDamaged is the one condition an adjustment line moves
// stock into (or back out of) without writing it off.
const AdjustmentConditionDamaged = "Damaged"

// builtAdjustment is an adjustment worked out but not yet saved - what Create
// saves and Preview shows.
type builtAdjustment struct {
	Location  string                   `json:"location"`
	Lines     []map[string]interface{} `json:"lines"`
	Increase  float64                  `json:"increase_value"`
	Decrease  float64                  `json:"decrease_value"`
	LossClass string                   `json:"loss_class"`
}

// buildInventoryAdjustment validates the input and works out each line's
// before / after quantity (stock on hand at the store now) and value.
func buildInventoryAdjustment(tenantID string, in InventoryAdjustmentInput) (*builtAdjustment, error) {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return nil, err
	}
	location := DocumentStoreCode(tenantID, in.Location)
	if location == "" {
		return nil, &ValidationError{Code: "GLOBAL-0001", SubFor: "Store", Message: "choose the store whose stock is adjusted"}
	}
	switch strings.TrimSpace(in.LossClass) {
	case "", "Shrinkage", "Damage", "Expiry", "Theft":
	default:
		return nil, &ValidationError{Code: "GLOBAL-0002", SubFor: "Loss class", Message: fmt.Sprintf("%q is not a loss class (Shrinkage, Damage, Expiry or Theft)", in.LossClass)}
	}
	if len(in.Lines) == 0 {
		return nil, &ValidationError{Code: "GLOBAL-0001", SubFor: "Items", Message: "add at least one item"}
	}
	out := &builtAdjustment{Location: location, LossClass: strings.TrimSpace(in.LossClass)}
	seen := map[string]bool{}
	for i, l := range in.Lines {
		n := i + 1
		item, err := ResolveItemBySKU(tenantID, strings.TrimSpace(l.SKU))
		if err != nil || item == nil {
			return nil, &ValidationError{Code: "GLOBAL-0004", SubFor: "Item", Message: fmt.Sprintf("line %d: item %q not found", n, l.SKU)}
		}
		sku := firstNonEmpty(strField(item.Data, "code"), item.ID)
		bin, batch := strings.TrimSpace(l.Bin), strings.TrimSpace(l.BatchNo)
		condition := strings.TrimSpace(l.Condition)
		if condition != "" && !strings.EqualFold(condition, AdjustmentConditionDamaged) {
			return nil, &ValidationError{Code: "GLOBAL-0002", SubFor: "Condition", Message: fmt.Sprintf("line %d: a line moves stock to Damaged or is a plain adjustment - %q is neither", n, condition)}
		}
		if condition != "" {
			condition = AdjustmentConditionDamaged
		}
		key := sku + "|" + bin + "|" + batch + "|" + condition
		if seen[key] {
			return nil, &ValidationError{Code: "GLOBAL-0003", SubFor: "Item", Message: fmt.Sprintf("line %d: %s is already on this adjustment - change that line instead", n, sku)}
		}
		seen[key] = true
		if l.Qty == 0 {
			return nil, &ValidationError{Code: "GLOBAL-0001", SubFor: "Quantity", Message: fmt.Sprintf("line %d: enter how many to add (+) or take off (-)", n)}
		}
		reason, err := ResolveReasonCode(tenantID, strings.TrimSpace(l.ReasonCode), ReasonInventoryAdjust)
		if err != nil {
			var verr *ValidationError
			if errors.As(err, &verr) {
				return nil, &ValidationError{Code: verr.Code, SubFor: "Reason", Message: fmt.Sprintf("line %d: %s", n, verr.Message)}
			}
			return nil, err
		}
		if len(l.Serials) > 0 && len(l.Serials) != absInt(l.Qty) {
			return nil, &ValidationError{Code: "GLOBAL-0002", SubFor: "Serials", Message: fmt.Sprintf("line %d: %d serial number(s) for a quantity of %d", n, len(l.Serials), absInt(l.Qty))}
		}
		switch {
		case condition != "" && l.Qty > 0:
			if dmg := damagedStock(schema, sku, location); dmg < l.Qty {
				return nil, &ValidationError{Code: "INVENT-0101", SubFor: "Quantity", Message: fmt.Sprintf("line %d: only %d of %s are Damaged at %s to move back", n, dmg, sku, location)}
			}
		case l.Qty < 0:
			if free := freeStock(schema, sku, location); free+l.Qty < 0 {
				return nil, &ValidationError{Code: "INVENT-0101", SubFor: "Quantity", Message: fmt.Sprintf("line %d: only %d of %s are free at %s to take off", n, free, sku, location)}
			}
			if bin != "" {
				if inBin := binGoodStock(schema, bin, sku); inBin+l.Qty < 0 {
					return nil, &ValidationError{Code: "INVENT-0101", SubFor: "Bin", Message: fmt.Sprintf("line %d: bin %s holds %d of %s, not %d", n, bin, inBin, sku, -l.Qty)}
				}
			}
		}
		before := onHandAt(schema, sku, location)
		after := before + l.Qty
		unit := adjustmentUnitCost(tenantID, sku, item.Data)
		value := round2(unit * math.Abs(float64(l.Qty)))
		if condition != "" {
			// Still owned: nothing is written off or added, so no value moves.
			after, value = before, 0
		} else if l.Qty > 0 {
			out.Increase = round2(out.Increase + value)
		} else {
			out.Decrease = round2(out.Decrease + value)
		}
		line := map[string]interface{}{
			"sku": sku, "item_name": strField(item.Data, "name"), "qty": l.Qty,
			"reason_code": reason.Code, "reason": reason.Label, "note": strings.TrimSpace(l.Note),
			"unit_cost": unit, "value": value, "before_qty": before, "after_qty": after,
		}
		if bin != "" {
			line["bin"] = bin
		}
		if batch != "" {
			line["batch_no"] = batch
		}
		if len(l.Serials) > 0 {
			line["serials"] = l.Serials
		}
		if condition != "" {
			line["condition"] = condition
		}
		out.Lines = append(out.Lines, line)
	}
	return out, nil
}

// PreviewInventoryAdjustment (Stage 130.14) shows what an adjustment would do
// - each line's before and after quantity and value, and its GL legs - and
// saves nothing.
func PreviewInventoryAdjustment(tenantID string, in InventoryAdjustmentInput) (map[string]interface{}, error) {
	b, err := buildInventoryAdjustment(tenantID, in)
	if err != nil {
		return nil, err
	}
	pctx := PostingContext{LegalEntity: locationLegalEntity(tenantID, b.Location), ReasonCode: commonLineReason(b.Lines), LossClass: b.LossClass}
	var gl []map[string]interface{}
	if b.Increase > 0 {
		acc := ResolvePostingAccounts(tenantID, PostingEventAdjustmentGain, pctx, accountInventory, accountStockAdjustment)
		gl = append(gl, map[string]interface{}{"account": acc.Debit, "debit": b.Increase}, map[string]interface{}{"account": acc.Credit, "credit": b.Increase})
	}
	if b.Decrease > 0 {
		acc := ResolvePostingAccounts(tenantID, adjustmentLossEvent(b.LossClass), pctx, accountStockAdjustment, accountInventory)
		gl = append(gl, map[string]interface{}{"account": acc.Debit, "debit": b.Decrease}, map[string]interface{}{"account": acc.Credit, "credit": b.Decrease})
	}
	return map[string]interface{}{"location": b.Location, "lines": b.Lines, "increase_value": b.Increase, "decrease_value": b.Decrease,
		"net_value": round2(b.Increase - b.Decrease), "gl": gl}, nil
}

// adjustmentLossEvent is the posting-profile event a loss of this class books.
func adjustmentLossEvent(lossClass string) string {
	switch lossClass {
	case "Damage":
		return PostingEventAdjustmentDamage
	case "Expiry":
		return PostingEventAdjustmentExpiry
	}
	return PostingEventAdjustmentLoss
}

// CreateInventoryAdjustment validates and saves an adjustment, then (D13) sends
// it through the approval rules: with a rule for InventoryAdjustment it waits as
// Pending Approval and posts when approved (DecideApproval's hook); with none
// it posts at once. Returns the id and the resulting status.
func CreateInventoryAdjustment(tenantID string, in InventoryAdjustmentInput, userID, role string) (string, string, error) {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return "", "", err
	}
	b, err := buildInventoryAdjustment(tenantID, in)
	if err != nil {
		return "", "", err
	}
	location, lines, increase, decrease := b.Location, b.Lines, b.Increase, b.Decrease

	id := NewDocID("ADJ")
	encodedLines, _ := json.Marshal(lines)
	data := map[string]interface{}{
		"code": id, "location": location, "note": strings.TrimSpace(in.Note), "items": string(encodedLines), "loss_class": b.LossClass,
		"increase_value": increase, "decrease_value": decrease, "net_value": round2(increase - decrease),
		// The approval rules route on the size of the change, either way.
		"amount": round2(increase + decrease), "requested_by": userID, "status": "Draft",
	}
	if in.ReversalOf != "" {
		data["reversal_of"] = in.ReversalOf
	}
	issueNumberInto(data, tenantID, InventoryAdjustmentDoctype, id, "", location)
	encoded, err := json.Marshal(data)
	if err != nil {
		return "", "", err
	}
	if _, err := db.DB.Exec(fmt.Sprintf(
		`INSERT INTO %s.documents (id, doctype, data, status, created_by) VALUES ($1, $2, $3, 'Draft', 'system')`, schema),
		id, InventoryAdjustmentDoctype, encoded); err != nil {
		return "", "", err
	}
	LogAuditEvent(tenantID, userID, "CREATE_INVENTORY_ADJUSTMENT", "SUCCESS", fmt.Sprintf("%s at %s: %d line(s), +%.2f / -%.2f", id, location, len(lines), increase, decrease))

	if gated, _ := IsApprovalGated(tenantID, InventoryAdjustmentDoctype); gated {
		if err := SubmitForApproval(tenantID, InventoryAdjustmentDoctype, id, userID, role); err != nil {
			return id, "Draft", err
		}
		return id, "Pending Approval", nil
	}
	if err := PostInventoryAdjustment(tenantID, id, userID); err != nil {
		return id, "Draft", err
	}
	return id, "Posted", nil
}

// PostInventoryAdjustment moves the stock (availability, then the stock ledger)
// and books its value: a gain Dr Inventory / Cr Stock Adjustment, a loss the
// other way, at each item's moving-average cost. A Draft (no approval rule)
// or an Approved adjustment posts; anything else is refused. Posting twice is
// a no-op. A take-off that no longer has the stock is refused and the reason
// kept on the document, which stays Approved for a retry.
func PostInventoryAdjustment(tenantID, id, userID string) error {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return err
	}
	if _, _, _, err := fetchDocument(tenantID, InventoryAdjustmentDoctype, id); err != nil {
		return fmt.Errorf("stock adjustment %s not found", id)
	}
	number, location := id, ""
	// Stage 74.2.3 / 74.1.5: quantity and value in ONE transaction on the
	// command spine - availability, the stock ledger (refusing a closed
	// period) and the GL commit together or not at all. Before this the GL
	// was booked after the commit and a failure left stock moved with its
	// value unbooked (gl_error).
	_, err = RunEconomicCommand(tenantID, EconomicCommand{Command: "inventory.adjustment.post", Doctype: InventoryAdjustmentDoctype, DocID: id},
		func(tx *sql.Tx, schema string, data map[string]interface{}) (map[string]interface{}, error) {
			var status string
			if err := tx.QueryRow(fmt.Sprintf(`SELECT status FROM %s.documents WHERE doctype = $1 AND id = $2`, schema), InventoryAdjustmentDoctype, id).Scan(&status); err != nil {
				return nil, err
			}
			if status == "Posted" {
				return map[string]interface{}{"posted": false}, nil
			}
			if status != "Draft" && status != "Approved" {
				return nil, fmt.Errorf("stock adjustment %s is %s - only a Draft or Approved adjustment posts", id, status)
			}
			location = strField(data, "location")
			number = firstNonEmpty(strField(data, "code"), id)
			lines := jsonLines(data["items"])
			// Stage 130.14: the stock it was worked out from must still hold.
			if err := checkAdjustmentSnapshotTx(tx, schema, location, lines); err != nil {
				return nil, err
			}
			moves := make([]interface{}, 0, len(lines))
			for _, l := range lines {
				if strField(l, "condition") != "" {
					continue // a condition move, below - still owned, on hand unchanged
				}
				m := map[string]interface{}{"sku": strField(l, "sku"), "qty": numFromInterface(l["qty"])}
				if b := strField(l, "batch_no"); b != "" {
					m["batch_no"] = b
				}
				moves = append(moves, m)
			}
			_, posted, err := PostInventoryLedgerWithVoucherTx(tx, schema, location, moves, false)
			if err != nil {
				return nil, err
			}
			if err := WriteStockLedgerLinesTx(tx, schema, location, InventoryAdjustmentDoctype, number, userID, "", posted); err != nil {
				return nil, err
			}
			// Stage 130.14: condition moves, bins and serial numbers.
			if err := applyAdjustmentLinesTx(tx, schema, location, number, lines); err != nil {
				return nil, err
			}
			// Stage 74.1.3: the accounts come from an approved posting profile
			// when one matches (the store's legal entity, the reason all lines
			// share, the document's loss class), else the fixed 1200 / 5190.
			lossClass := strField(data, "loss_class")
			pctx := PostingContext{LegalEntity: locationLegalEntity(tenantID, location), ReasonCode: commonLineReason(lines), LossClass: lossClass}
			if gain := RupeesToPaise(numFromInterface(data["increase_value"])); gain > 0 {
				acc := ResolvePostingAccounts(tenantID, PostingEventAdjustmentGain, pctx, accountInventory, accountStockAdjustment)
				if err := PostDoubleEntryTx(tx, tenantID, schema, InventoryAdjustmentDoctype, id, map[string]int64{acc.Debit: gain}, map[string]int64{acc.Credit: gain}, "", "InventoryAdjustment:"+id+":IN"); err != nil {
					return nil, err
				}
			}
			if loss := RupeesToPaise(numFromInterface(data["decrease_value"])); loss > 0 {
				acc := ResolvePostingAccounts(tenantID, adjustmentLossEvent(lossClass), pctx, accountStockAdjustment, accountInventory)
				if err := PostDoubleEntryTx(tx, tenantID, schema, InventoryAdjustmentDoctype, id, map[string]int64{acc.Debit: loss}, map[string]int64{acc.Credit: loss}, "", "InventoryAdjustment:"+id+":OUT"); err != nil {
					return nil, err
				}
			}
			update, _ := json.Marshal(map[string]interface{}{"status": "Posted", "posted_by": userID, "posted_at": time.Now().UTC().Format(time.RFC3339), "post_error": "", "gl_error": ""})
			if _, err := tx.Exec(fmt.Sprintf(`UPDATE %s.documents SET status = 'Posted', data = data || $1::jsonb, updated_at = CURRENT_TIMESTAMP WHERE doctype = $2 AND id = $3`, schema),
				string(update), InventoryAdjustmentDoctype, id); err != nil {
				return nil, err
			}
			return map[string]interface{}{"posted": true}, nil
		})
	if err != nil {
		// Nothing moved; the reason stays on the document for a retry.
		_ = mergeDocData(schema, InventoryAdjustmentDoctype, id, map[string]interface{}{"post_error": err.Error()})
		return err
	}
	LogAuditEvent(tenantID, userID, "POST_INVENTORY_ADJUSTMENT", "SUCCESS", fmt.Sprintf("%s posted at %s", number, location))
	return nil
}

// freeStock is what a sale could take: available less what is held inside it.
func freeStock(schema, sku, location string) int {
	var free int
	_ = db.DB.QueryRow(fmt.Sprintf(`SELECT available - blocked - hold_qty FROM %s.inventory_availability WHERE sku = $1 AND location_code = $2`, schema),
		sku, location).Scan(&free)
	return free
}

// adjustmentUnitCost is the moving-average cost, else the item's own cost.
func adjustmentUnitCost(tenantID, sku string, item map[string]interface{}) float64 {
	if paise, ok, err := GetItemUnitCost(tenantID, sku); err == nil && ok {
		return PaiseToRupees(paise)
	}
	return firstPositive(numFromInterface(item["cost_price"]), numFromInterface(item["standard_cost"]))
}

// mergeDocData adds top-level fields to a document's data.
func mergeDocData(schema, doctype, id string, fields map[string]interface{}) error {
	encoded, err := json.Marshal(fields)
	if err != nil {
		return err
	}
	_, err = db.DB.Exec(fmt.Sprintf(`UPDATE %s.documents SET data = data || $1::jsonb, updated_at = CURRENT_TIMESTAMP WHERE doctype = $2 AND id = $3`, schema),
		string(encoded), doctype, id)
	return err
}

// ---------------------------------------------------------------------------
// 88.5.5 MRP Change (D14): item-level, from an effective date.
// ---------------------------------------------------------------------------

type MRPChangeLine struct {
	SKU    string  `json:"sku"`
	NewMRP float64 `json:"new_mrp"`
}

type MRPChangeInput struct {
	EffectiveDate string          `json:"effective_date"` // YYYY-MM-DD; blank or today applies now
	Reason        string          `json:"reason"`
	Lines         []MRPChangeLine `json:"lines"`
}

// CreateMRPChange records old and new MRP per item. Effective today (or
// earlier) it applies at once; a later date waits as Scheduled and the job
// runner applies it that morning. Returns the id and status, and the stock on
// hand per item so the page can offer the sticker reprint.
func CreateMRPChange(tenantID string, in MRPChangeInput, userID string) (string, string, error) {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return "", "", err
	}
	today := BusinessToday()
	effective := strings.TrimSpace(in.EffectiveDate)
	if effective == "" {
		effective = today
	}
	when, err := time.ParseInLocation("2006-01-02", effective, time.Local)
	if err != nil {
		return "", "", &ValidationError{Code: "GLOBAL-0002", SubFor: "Effective date", Message: "enter the date the new MRP starts"}
	}
	if len(in.Lines) == 0 {
		return "", "", &ValidationError{Code: "GLOBAL-0001", SubFor: "Items", Message: "add at least one item"}
	}
	lines := make([]map[string]interface{}, 0, len(in.Lines))
	seen := map[string]bool{}
	for i, l := range in.Lines {
		n := i + 1
		item, err := ResolveItemBySKU(tenantID, strings.TrimSpace(l.SKU))
		if err != nil || item == nil {
			return "", "", &ValidationError{Code: "GLOBAL-0004", SubFor: "Item", Message: fmt.Sprintf("line %d: item %q not found", n, l.SKU)}
		}
		sku := firstNonEmpty(strField(item.Data, "code"), item.ID)
		if seen[sku] {
			return "", "", &ValidationError{Code: "GLOBAL-0003", SubFor: "Item", Message: fmt.Sprintf("line %d: %s is already on this change", n, sku)}
		}
		seen[sku] = true
		old := numFromInterface(item.Data["mrp"])
		if l.NewMRP <= 0 {
			return "", "", &ValidationError{Code: "GLOBAL-0001", SubFor: "New MRP", Message: fmt.Sprintf("line %d: enter the new MRP of %s", n, sku)}
		}
		if round2(l.NewMRP) == round2(old) {
			return "", "", &ValidationError{Code: "GLOBAL-0003", SubFor: "New MRP", Message: fmt.Sprintf("line %d: %s already has MRP %.2f", n, sku, old)}
		}
		lines = append(lines, map[string]interface{}{
			"sku": sku, "item_id": item.ID, "item_name": strField(item.Data, "name"), "old_mrp": old, "new_mrp": round2(l.NewMRP),
			"sale_price": numFromInterface(item.Data["sale_price"]), "on_hand": onHandAll(schema, sku),
		})
	}
	id := NewDocID("MRP")
	encodedLines, _ := json.Marshal(lines)
	status := "Scheduled"
	if effective <= today {
		status = "Draft" // applied below, then Applied
	}
	data := map[string]interface{}{"code": id, "effective_date": effective, "reason": strings.TrimSpace(in.Reason),
		"items": string(encodedLines), "requested_by": userID, "status": status}
	issueNumberInto(data, tenantID, MRPChangeDoctype, id, "")
	encoded, err := json.Marshal(data)
	if err != nil {
		return "", "", err
	}
	if _, err := db.DB.Exec(fmt.Sprintf(
		`INSERT INTO %s.documents (id, doctype, data, status, created_by) VALUES ($1, $2, $3, $4, 'system')`, schema),
		id, MRPChangeDoctype, encoded, status); err != nil {
		return "", "", err
	}
	LogAuditEvent(tenantID, userID, "CREATE_MRP_CHANGE", "SUCCESS", fmt.Sprintf("%s: %d item(s) from %s", id, len(lines), effective))
	if status == "Scheduled" {
		jobID, err := EnqueueJobContext(WithJobActor(context.Background(), userID), tenantID, mrpChangeJobType, map[string]interface{}{"tenant_id": tenantID, "mrp_change_id": id}, "mrp:"+id)
		if err == nil {
			_, err = db.DB.Exec(fmt.Sprintf(`UPDATE %s.async_jobs SET next_attempt_at = $1 WHERE id = $2`, schema), when, jobID)
		}
		if err != nil {
			return id, status, fmt.Errorf("MRP change %s is saved but not scheduled: %v", id, err)
		}
		return id, status, nil
	}
	if err := ApplyMRPChange(tenantID, id, userID); err != nil {
		return id, "Draft", err
	}
	return id, "Applied", nil
}

// ApplyMRPChange writes each line's new MRP onto its item. Idempotent; a
// Cancelled change applies nothing.
func ApplyMRPChange(tenantID, id, userID string) error {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return err
	}
	data, status, _, err := fetchDocument(tenantID, MRPChangeDoctype, id)
	if err != nil {
		return fmt.Errorf("MRP change %s not found", id)
	}
	if status == "Applied" || status == "Cancelled" {
		return nil
	}
	tx, err := db.DB.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for _, l := range jsonLines(data["items"]) {
		if _, err := tx.Exec(fmt.Sprintf(
			`UPDATE %s.documents SET data = jsonb_set(data, '{mrp}', to_jsonb($1::numeric)), updated_at = CURRENT_TIMESTAMP
			  WHERE doctype = 'Item' AND id = $2 AND deleted_at IS NULL`, schema),
			numFromInterface(l["new_mrp"]), strField(l, "item_id")); err != nil {
			return err
		}
	}
	if _, err := tx.Exec(fmt.Sprintf(
		`UPDATE %s.documents SET status = 'Applied', data = data || jsonb_build_object('status', 'Applied', 'applied_at', $1::text, 'applied_by', $2::text), updated_at = CURRENT_TIMESTAMP
		  WHERE doctype = $3 AND id = $4`, schema), time.Now().UTC().Format(time.RFC3339), userID, MRPChangeDoctype, id); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	LogAuditEvent(tenantID, userID, "APPLY_MRP_CHANGE", "SUCCESS", fmt.Sprintf("%s applied", id))
	return nil
}

// CancelMRPChange withdraws a Scheduled change before its date; its queued
// job then finds it Cancelled and applies nothing.
func CancelMRPChange(tenantID, id, userID string) error {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return err
	}
	_, status, _, err := fetchDocument(tenantID, MRPChangeDoctype, id)
	if err != nil {
		return fmt.Errorf("MRP change %s not found", id)
	}
	if status != "Scheduled" {
		return &ValidationError{Code: "GLOBAL-0019", Message: fmt.Sprintf("MRP change %s is %s - only a Scheduled change can be cancelled; make a new change to undo an applied one", id, status)}
	}
	if _, err := db.DB.Exec(fmt.Sprintf(
		`UPDATE %s.documents SET status = 'Cancelled', data = data || jsonb_build_object('status', 'Cancelled', 'cancelled_by', $1::text), updated_at = CURRENT_TIMESTAMP
		  WHERE doctype = $2 AND id = $3`, schema), userID, MRPChangeDoctype, id); err != nil {
		return err
	}
	LogAuditEvent(tenantID, userID, "CANCEL_MRP_CHANGE", "SUCCESS", id)
	return nil
}

func init() {
	RegisterJobHandler(mrpChangeJobType, func(ctx context.Context, schema string, job Job) (map[string]interface{}, error) {
		tenantID, _ := job.Payload["tenant_id"].(string)
		id, _ := job.Payload["mrp_change_id"].(string)
		if err := ApplyMRPChange(tenantID, id, "system"); err != nil {
			return nil, err
		}
		return map[string]interface{}{"applied": id}, nil
	})
}

func onHandAll(schema, sku string) int {
	var n sql.NullInt64
	_ = db.DB.QueryRow(fmt.Sprintf(`SELECT SUM(on_hand) FROM %s.inventory_availability WHERE sku = $1`, schema), sku).Scan(&n)
	return int(n.Int64)
}

// ---------------------------------------------------------------------------
// 88.5.1 Dashboard
// ---------------------------------------------------------------------------

// InventoryDashboard is the Inventory module's first page: stock units and
// value per store, low stock, the latest movements and the adjustments
// waiting for approval. location narrows it to one store.
func InventoryDashboard(tenantID, location string) (map[string]interface{}, error) {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return nil, err
	}
	out := map[string]interface{}{}
	rows, err := db.DB.Query(fmt.Sprintf(`
		SELECT ia.location_code, SUM(ia.on_hand), COUNT(*) FILTER (WHERE ia.on_hand <> 0),
		       COALESCE(SUM(ia.on_hand * COALESCE(ic.avg_unit_cost_paise, 0)), 0)
		  FROM %s.inventory_availability ia LEFT JOIN %s.item_cost ic ON ic.item_code = ia.sku
		 WHERE ($1 = '' OR ia.location_code = $1)
		 GROUP BY ia.location_code ORDER BY ia.location_code`, schema, schema), location)
	if err != nil {
		return nil, err
	}
	byLocation := []map[string]interface{}{}
	var totalUnits, totalValue int64
	for rows.Next() {
		var loc string
		var units, items, valuePaise int64
		if err := rows.Scan(&loc, &units, &items, &valuePaise); err != nil {
			rows.Close()
			return nil, err
		}
		totalUnits += units
		totalValue += valuePaise
		byLocation = append(byLocation, map[string]interface{}{"location": loc, "units": units, "items": items, "value": PaiseToRupees(valuePaise)})
	}
	rows.Close()
	out["by_location"], out["total_units"], out["total_value"] = byLocation, totalUnits, PaiseToRupees(totalValue)

	lowStock, err := inventoryLowStock(schema, location, 20)
	if err != nil {
		return nil, err
	}
	out["low_stock"] = lowStock

	recent, err := queryMaps(fmt.Sprintf(`
		SELECT created_at, data->>'item_id' AS sku, data->>'warehouse_id' AS location, (data->>'qty')::numeric AS qty,
		       data->>'voucher_type' AS voucher_type, data->>'voucher_id' AS voucher_id
		  FROM %s.documents WHERE doctype = 'StockLedgerEntry' AND ($1 = '' OR data->>'warehouse_id' = $1)
		 ORDER BY created_at DESC, id DESC LIMIT 15`, schema), location)
	if err != nil {
		return nil, err
	}
	out["recent_movements"] = recent

	pending, err := queryMaps(fmt.Sprintf(`
		SELECT id, COALESCE(data->>'code', id) AS code, data->>'location' AS location, (data->>'amount')::numeric AS amount, created_at
		  FROM %s.documents WHERE doctype = 'InventoryAdjustment' AND status = 'Pending Approval' AND deleted_at IS NULL
		   AND ($1 = '' OR data->>'location' = $1) ORDER BY created_at`, schema), location)
	if err != nil {
		return nil, err
	}
	out["pending_adjustments"] = pending
	return out, nil
}

// queryMaps runs a one-argument read and returns each row as a map.
func queryMaps(query string, arg interface{}) ([]map[string]interface{}, error) {
	rows, err := db.DB.Query(query, arg)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	cols, _ := rows.Columns()
	out := []map[string]interface{}{}
	for rows.Next() {
		vals := make([]interface{}, len(cols))
		ptrs := make([]interface{}, len(cols))
		for i := range vals {
			ptrs[i] = &vals[i]
		}
		if err := rows.Scan(ptrs...); err != nil {
			return nil, err
		}
		m := map[string]interface{}{}
		for i, c := range cols {
			switch v := vals[i].(type) {
			case []byte:
				m[c] = string(v)
			default:
				m[c] = v
			}
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

// inventoryLowStock is the Inventory Dashboard's low-stock rule, shared with
// the store app's push alerts (Stage 102.1.3) so the two never disagree: free
// stock at or under its safety stock (the row's own, or a reorder-point
// config's), or none left at all. location "" = every location; limit 0 =
// every row.
func inventoryLowStock(schema, location string, limit int) ([]map[string]interface{}, error) {
	low, err := queryMaps(fmt.Sprintf(`
		SELECT ia.sku, ia.location_code, (ia.available - ia.blocked - ia.hold_qty) AS free,
		       GREATEST(ia.safety_stock, COALESCE((SELECT MAX(COALESCE((r.data->>'safety_stock_qty')::numeric, 0)) FROM %s.documents r
		         WHERE r.doctype = 'ReorderPointConfig' AND r.deleted_at IS NULL AND r.data->>'item' = ia.sku AND r.data->>'location_code' = ia.location_code), 0))::int AS safety
		  FROM %s.inventory_availability ia
		 WHERE ($1 = '' OR ia.location_code = $1)`, schema, schema), location)
	if err != nil {
		return nil, err
	}
	// The database hands integers back as int64, which numFromInterface reads
	// as 0 - so before Stage 102 every row with stock passed "free <= 0" and
	// the dashboard listed in-stock items as low. Read them as numbers here.
	num := func(v interface{}) float64 {
		if n, ok := v.(int64); ok {
			return float64(n)
		}
		return numFromInterface(v)
	}
	lowStock := []map[string]interface{}{}
	for _, r := range low {
		free, safety := num(r["free"]), num(r["safety"])
		r["free"], r["safety"] = free, safety
		if free <= 0 || (safety > 0 && free <= safety) {
			lowStock = append(lowStock, r)
		}
		if limit > 0 && len(lowStock) == limit {
			break
		}
	}
	return lowStock, nil
}
