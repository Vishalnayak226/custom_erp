package engines

import (
	"custom_erp/db"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"time"
)

// Stage 26.5 (WMS Enterprise Maturity Sprint): 26.5.8 cartonization at pack
// step, 26.5.9 ABC cycle-count planner, 26.5.10 blind recount + variance
// root-cause codes.

// ------------------------------------------------------------------
// 26.5.8: Cartonization at pack step
// ------------------------------------------------------------------

// CartonizationItem is one sku/qty line to be split across boxes. UOM is
// Stage 42.1.10's addition: optional, and blank means exactly what it always
// meant before that Stage existed - qty is already in eaches. When set, qty
// is converted to its each-equivalent via ConvertUOMQty before packing, so a
// caller can hand this function "2 CASE of SKU-X" as naturally as "24 EA".
type CartonizationItem struct {
	Sku string `json:"sku"`
	Qty int    `json:"qty"`
	UOM string `json:"uom,omitempty"`
}

// SuggestedCarton is one box in a cartonization suggestion - shaped so it
// can be handed straight to the existing PackTransferOrder's boxes
// parameter (box_id/items) after the packer confirms or edits it.
type SuggestedCarton struct {
	BoxID        string              `json:"box_id"`
	CartonType   string              `json:"carton_type"`
	Items        []CartonizationItem `json:"items"`
	UsedCapacity int                 `json:"used_capacity"`
	MaxCapacity  int                 `json:"max_capacity"`
}

// SuggestCartonization (26.5.8) extends Stage 20.19's pack/dispatch mapping
// (PackTransferOrder, engines/transfer_orders.go), which already accepts a
// free-form boxes array but never suggested one - this fills that gap with
// a deliberately simple qty-capacity first-fit-decreasing packer (not 3D
// dimensional bin-packing, per this repo's lightweight-first rule): items
// are packed largest-qty-first, each unit going into the first box with
// spare capacity, a new box opened only once every existing box is full.
// Doctype-agnostic (plain sku/qty in, boxes out) so the same function packs
// both a TransferOrder and (in a later Stage, if picked up) a
// FulfillmentTask pack step.
func SuggestCartonization(tenantID, cartonTypeCode string, items []CartonizationItem) ([]SuggestedCarton, error) {
	if len(items) == 0 {
		return nil, errors.New("at least one item line is required")
	}
	capacity, err := getCartonCapacity(tenantID, cartonTypeCode)
	if err != nil {
		return nil, err
	}
	if capacity <= 0 {
		return nil, fmt.Errorf("carton type %s has no usable capacity configured", cartonTypeCode)
	}

	sorted := make([]CartonizationItem, len(items))
	copy(sorted, items)
	for i := range sorted {
		if sorted[i].UOM == "" {
			continue
		}
		eaches, err := ConvertUOMQty(tenantID, sorted[i].Sku, float64(sorted[i].Qty), sorted[i].UOM, "EA")
		if err != nil {
			return nil, err
		}
		sorted[i].Qty = int(eaches)
		sorted[i].UOM = ""
	}
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].Qty > sorted[j].Qty })

	var boxes []SuggestedCarton
	for _, it := range sorted {
		if it.Sku == "" || it.Qty <= 0 {
			continue
		}
		remaining := it.Qty
		for remaining > 0 {
			placedIdx := -1
			for bi := range boxes {
				if capacity-boxes[bi].UsedCapacity > 0 {
					placedIdx = bi
					break
				}
			}
			if placedIdx == -1 {
				boxes = append(boxes, SuggestedCarton{
					BoxID: fmt.Sprintf("BOX%d", len(boxes)+1), CartonType: cartonTypeCode, MaxCapacity: capacity,
				})
				placedIdx = len(boxes) - 1
			}
			room := capacity - boxes[placedIdx].UsedCapacity
			take := remaining
			if take > room {
				take = room
			}
			boxes[placedIdx].Items = append(boxes[placedIdx].Items, CartonizationItem{Sku: it.Sku, Qty: take})
			boxes[placedIdx].UsedCapacity += take
			remaining -= take
		}
	}
	return boxes, nil
}

func getCartonCapacity(tenantID, cartonTypeCode string) (int, error) {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return 0, err
	}
	var dataStr string
	err = db.DB.QueryRow(fmt.Sprintf(
		`SELECT data FROM %s.documents WHERE doctype = 'CartonType' AND (id = $1 OR data->>'code' = $1) AND status = 'Active'`, schema),
		cartonTypeCode).Scan(&dataStr)
	if err == sql.ErrNoRows {
		return 0, fmt.Errorf("no Active CartonType %q found", cartonTypeCode)
	} else if err != nil {
		return 0, err
	}
	var data map[string]interface{}
	if err := json.Unmarshal([]byte(dataStr), &data); err != nil {
		return 0, err
	}
	return int(numFromInterface(data["max_qty_capacity"])), nil
}

// ------------------------------------------------------------------
// 26.5.9: ABC cycle-count planner
// ------------------------------------------------------------------

// ABCCycleCountSuggestion is one SKU's velocity tier and whether it's due
// for its next count.
type ABCCycleCountSuggestion struct {
	Sku                 string  `json:"sku"`
	LocationCode        string  `json:"location_code"`
	Tier                string  `json:"tier"` // A, B, or C
	DailyVelocity       float64 `json:"daily_velocity"`
	DaysSinceLastCount  int     `json:"days_since_last_count"` // -1 = never counted
	IntervalDays        int     `json:"interval_days"`
	Due                 bool    `json:"due"`
}

// GetABCCycleCountPlan (26.5.9) classifies every SKU on hand at
// locationCode into an A/B/C velocity tier (Stage 10's
// CalculateSalesVelocity as the ranking signal - top 20% of SKUs by
// velocity are A, the next 30% are B, the remaining 50% are C, the
// classic Pareto ABC split) and reports whether each is due for its
// tier's recount interval. A SKU with zero sales velocity (new, or genuinely
// slow-moving) lands in C by construction - this is why no separate naive
// random-bin sampler is needed as a fallback (micro_checklist.md's own
// "placeholder if true ABC isn't ready" carve-out): every SKU always gets a
// real tier and interval from this one function, velocity data or not.
// Read-only, same "suggestion report, no auto-created documents" precedent
// GetReplenishmentSuggestions (Stage 10) already set.
func GetABCCycleCountPlan(tenantID, locationCode string, tierAIntervalDays, tierBIntervalDays, tierCIntervalDays int) ([]ABCCycleCountSuggestion, error) {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return nil, err
	}
	if tierAIntervalDays <= 0 {
		tierAIntervalDays = GetSettingInt(tenantID, "wms.cycle_count_tier_a_interval_days")
	}
	if tierBIntervalDays <= 0 {
		tierBIntervalDays = GetSettingInt(tenantID, "wms.cycle_count_tier_b_interval_days")
	}
	if tierCIntervalDays <= 0 {
		tierCIntervalDays = GetSettingInt(tenantID, "wms.cycle_count_tier_c_interval_days")
	}

	rows, err := db.DB.Query(fmt.Sprintf(
		`SELECT sku FROM %s.inventory_availability WHERE location_code = $1 AND on_hand > 0`, schema), locationCode)
	if err != nil {
		return nil, err
	}
	var skus []string
	for rows.Next() {
		var sku string
		if err := rows.Scan(&sku); err != nil {
			rows.Close()
			return nil, err
		}
		skus = append(skus, sku)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if len(skus) == 0 {
		return nil, nil
	}

	type skuVelocity struct {
		Sku      string
		Velocity float64
	}
	velocities := make([]skuVelocity, 0, len(skus))
	for _, sku := range skus {
		v, err := CalculateSalesVelocity(tenantID, locationCode, sku, 30)
		if err != nil {
			v = 0
		}
		velocities = append(velocities, skuVelocity{Sku: sku, Velocity: v})
	}
	sort.SliceStable(velocities, func(i, j int) bool { return velocities[i].Velocity > velocities[j].Velocity })

	n := len(velocities)
	aCut := (n*20 + 99) / 100 // ceil(20%)
	bCut := (n*50 + 99) / 100 // ceil(50%) - top B boundary
	if aCut < 1 {
		aCut = 1
	}
	if bCut < aCut {
		bCut = aCut
	}

	suggestions := make([]ABCCycleCountSuggestion, 0, n)
	for i, sv := range velocities {
		tier, interval := "C", tierCIntervalDays
		if i < aCut {
			tier, interval = "A", tierAIntervalDays
		} else if i < bCut {
			tier, interval = "B", tierBIntervalDays
		}

		var lastCounted sql.NullTime
		_ = db.DB.QueryRow(fmt.Sprintf(`
			SELECT MAX(created_at) FROM %s.documents
			WHERE doctype = 'CycleCountLine' AND status = 'Posted' AND data->>'sku' = $1 AND data->>'location' = $2`, schema),
			sv.Sku, locationCode).Scan(&lastCounted)

		daysSince := -1
		due := true
		if lastCounted.Valid {
			daysSince = int(time.Since(lastCounted.Time).Hours() / 24)
			due = daysSince >= interval
		}
		suggestions = append(suggestions, ABCCycleCountSuggestion{
			Sku: sv.Sku, LocationCode: locationCode, Tier: tier, DailyVelocity: sv.Velocity,
			DaysSinceLastCount: daysSince, IntervalDays: interval, Due: due,
		})
	}

	sort.SliceStable(suggestions, func(i, j int) bool {
		if suggestions[i].Due != suggestions[j].Due {
			return suggestions[i].Due
		}
		if suggestions[i].Tier != suggestions[j].Tier {
			return suggestions[i].Tier < suggestions[j].Tier
		}
		return suggestions[i].DaysSinceLastCount > suggestions[j].DaysSinceLastCount
	})
	return suggestions, nil
}

// ------------------------------------------------------------------
// 26.5.10: Blind-count + recount workflow + variance root-cause codes
// ------------------------------------------------------------------

// RequestRecount (26.5.10) marks a CycleCountLine 'Recount Requested' (so
// ReconcileCycleCount's own not-yet-processed query, extended by this Stage
// to also exclude that status, never double-processes it) and inserts a
// fresh Draft line for the same count_session/location/bin/sku with
// recount_of pointing back at the original - counted_qty/system_qty/
// variance are deliberately never copied, so the second counter is blind to
// both the first count and the system quantity, satisfying the blind-count
// requirement for the recount itself (the *first* count was already blind
// by construction: system_qty is only ever filled in by ReconcileCycleCount,
// after counting, never before).
func RequestRecount(tenantID, lineID, userID string) (newLineID string, err error) {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return "", err
	}
	tx, err := db.DB.Begin()
	if err != nil {
		return "", err
	}
	defer tx.Rollback()
	if err := db.SetSearchPath(tx, schema); err != nil {
		return "", err
	}

	var status, dataStr string
	if err := tx.QueryRow(fmt.Sprintf(
		`SELECT status, data FROM %s.documents WHERE doctype = 'CycleCountLine' AND id = $1 FOR UPDATE`, schema), lineID).
		Scan(&status, &dataStr); err != nil {
		return "", fmt.Errorf("cycle count line not found: %v", err)
	}
	if status == "Posted" {
		return "", fmt.Errorf("cycle count line %s is already Posted - cannot recount", lineID)
	}
	if status == "Recount Requested" {
		return "", fmt.Errorf("cycle count line %s already has a recount pending", lineID)
	}
	var data map[string]interface{}
	if err := json.Unmarshal([]byte(dataStr), &data); err != nil {
		return "", err
	}

	newLineID = NewDocIDCompact("CCL")
	newData := map[string]interface{}{
		"id":            newLineID,
		"code":          newLineID,
		"count_session": data["count_session"],
		"location":      data["location"],
		"bin":           data["bin"],
		"sku":           data["sku"],
		"status":        "Draft",
		"recount_of":    lineID,
	}
	newMarshaled, err := json.Marshal(newData)
	if err != nil {
		return "", err
	}
	if _, err := tx.Exec(fmt.Sprintf(
		`INSERT INTO %s.documents (id, doctype, data, status, created_by) VALUES ($1, 'CycleCountLine', $2, 'Draft', $3)`, schema),
		newLineID, newMarshaled, userID); err != nil {
		return "", err
	}

	data["status"] = "Recount Requested"
	origMarshaled, _ := json.Marshal(data)
	if _, err := tx.Exec(fmt.Sprintf(
		`UPDATE %s.documents SET data = $1, status = 'Recount Requested', updated_at = CURRENT_TIMESTAMP WHERE id = $2`, schema),
		origMarshaled, lineID); err != nil {
		return "", err
	}

	if err := tx.Commit(); err != nil {
		return "", err
	}
	LogAuditEvent(tenantID, userID, "WMS_RECOUNT_REQUESTED", "SUCCESS",
		fmt.Sprintf("Requested blind recount of line %s as new line %s", lineID, newLineID))
	return newLineID, nil
}

// SubmitRecountValue (26.5.10) lets the (blind) second counter record their
// counted qty against the placeholder RequestRecount created - the only way
// to fill in counted_qty on a recount line, since CycleCountLine.allow_update
// stays FALSE for the same reason it always has (system_qty/variance/status
// must never be hand-edited around the approval gate).
func SubmitRecountValue(tenantID, lineID string, countedQty float64, userID string) error {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return err
	}
	var status, dataStr string
	if err := db.DB.QueryRow(fmt.Sprintf(
		`SELECT status, data FROM %s.documents WHERE doctype = 'CycleCountLine' AND id = $1`, schema), lineID).
		Scan(&status, &dataStr); err != nil {
		return fmt.Errorf("cycle count line not found: %v", err)
	}
	if status != "Draft" {
		return fmt.Errorf("line %s is %s, not Draft - cannot record a recount value", lineID, status)
	}
	var data map[string]interface{}
	if err := json.Unmarshal([]byte(dataStr), &data); err != nil {
		return err
	}
	if _, isRecount := data["recount_of"]; !isRecount {
		return fmt.Errorf("line %s is not a recount line", lineID)
	}
	data["counted_qty"] = countedQty
	marshaled, _ := json.Marshal(data)
	_, err = db.DB.Exec(fmt.Sprintf(
		`UPDATE %s.documents SET data = $1, updated_at = CURRENT_TIMESTAMP WHERE id = $2`, schema),
		marshaled, lineID)
	return err
}

// SetCycleCountVarianceReason (26.5.10) records the mandatory root-cause
// ReasonCode (category 'Cycle Count Variance') for a variance before
// PostCycleCountAdjustment will allow it to post - a handler-only action
// (not a generic-update field write) for the same reason system_qty/
// variance/status already are.
func SetCycleCountVarianceReason(tenantID, lineID, reasonCode, userID string) error {
	if err := requireActiveReasonCode(tenantID, reasonCode, "Cycle Count Variance"); err != nil {
		return err
	}
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return err
	}
	var status, dataStr string
	if err := db.DB.QueryRow(fmt.Sprintf(
		`SELECT status, data FROM %s.documents WHERE doctype = 'CycleCountLine' AND id = $1`, schema), lineID).
		Scan(&status, &dataStr); err != nil {
		return fmt.Errorf("cycle count line not found: %v", err)
	}
	if status == "Posted" {
		return fmt.Errorf("cycle count line %s is already Posted", lineID)
	}
	var data map[string]interface{}
	if err := json.Unmarshal([]byte(dataStr), &data); err != nil {
		return err
	}
	data["variance_reason_code"] = reasonCode
	marshaled, _ := json.Marshal(data)
	_, err = db.DB.Exec(fmt.Sprintf(
		`UPDATE %s.documents SET data = $1, updated_at = CURRENT_TIMESTAMP WHERE id = $2`, schema),
		marshaled, lineID)
	if err != nil {
		return err
	}
	LogAuditEvent(tenantID, userID, "WMS_VARIANCE_REASON_SET", "SUCCESS", fmt.Sprintf("Set variance reason %s on line %s", reasonCode, lineID))
	return nil
}
