package engines

import (
	"custom_erp/db"
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

// Stage 170.7: impairment and revaluation of a Capitalised asset.
//
//	Impairment             Dr 5313 Impairment loss / Cr 1410 Accumulated depreciation
//	Revaluation up         Dr 1400 Fixed assets / Cr 3500 Revaluation reserve (cost goes up)
//	Revaluation down       Dr 3500 (up to this asset's own reserve) and Dr 5313 (the rest) / Cr 1410
//
// Both reset the depreciation base: what is left to carry is spread over the
// months of the life that remain, from the month after the last one posted. The
// asset keeps its history in `asset_adjustments`, the 1410 credits the
// adjustments made in `adjustment_accumulated_paise` (disposal clears them with
// the depreciation) and the reserve it holds in `revaluation_reserve_paise`.
// There is no approval flow on asset adjustments today, so each one is
// audit-logged with the reason and the user.

const (
	assetAccRevalReserve = "3500"
	assetAccImpairment   = "5313"
	// Ask 176: disposal moves the asset's remaining reserve here.
	assetAccRetainedEarnings = "3100"

	assetAdjInputCode       = "ASSET-1725" // kind, amount, reason or date not usable
	assetAdjStateCode       = "ASSET-1726" // asset not in a state to be adjusted
	assetReserveReleaseCode = "ASSET-1730" // 3100 is missing or not Equity, so the reserve cannot be released
)

const (
	AssetAdjImpairment  = "Impairment"
	AssetAdjRevaluation = "Revaluation"
)

type AssetAdjustmentInput struct {
	Kind           string  `json:"kind"`
	Amount         float64 `json:"amount"`    // impairment: the loss
	NewValue       float64 `json:"new_value"` // revaluation: what the asset is carried at from now
	Reason         string  `json:"reason"`
	Date           string  `json:"date"`
	IdempotencyKey string  `json:"idempotency_key"`
}

type AssetAdjustmentResult struct {
	AssetID        string  `json:"asset_id"`
	Kind           string  `json:"kind"`
	CarryingBefore float64 `json:"carrying_before"`
	CarryingAfter  float64 `json:"carrying_after"`
	Loss           float64 `json:"loss"`
	ToReserve      float64 `json:"to_reserve"`
	FromReserve    float64 `json:"from_reserve"`
	Reserve        float64 `json:"reserve"`
	MonthlyCharge  float64 `json:"monthly_charge"`
	Replayed       bool    `json:"replayed,omitempty"`
}

func assetAdjRefused(code, sub, format string, args ...interface{}) error {
	return &ValidationError{Code: code, SubFor: sub, Message: fmt.Sprintf(format, args...)}
}

// assetAdjustmentAccumulated is what the asset's adjustments credited to 1410.
func assetAdjustmentAccumulated(data map[string]interface{}) int64 {
	return int64(numFromInterface(data["adjustment_accumulated_paise"]))
}

// AdjustAsset posts one impairment or revaluation and resets the depreciation
// base. The same idempotency key again returns the first result.
func AdjustAsset(tenantID, assetID, userID string, in AssetAdjustmentInput) (*AssetAdjustmentResult, error) {
	kind := strings.TrimSpace(in.Kind)
	switch strings.ToLower(kind) {
	case "impairment":
		kind = AssetAdjImpairment
	case "revaluation":
		kind = AssetAdjRevaluation
	default:
		return nil, assetAdjRefused(assetAdjInputCode, "Kind", "Choose Impairment or Revaluation.")
	}
	reason := strings.TrimSpace(in.Reason)
	if reason == "" {
		return nil, assetAdjRefused(assetAdjInputCode, "Reason", "Write the reason.")
	}
	date := strings.TrimSpace(in.Date)
	if date == "" {
		date = BusinessToday()
	} else if _, err := time.Parse("2006-01-02", date); err != nil {
		return nil, assetAdjRefused(assetAdjInputCode, "Date", "Use a date like 2026-11-04.")
	}
	if kind == AssetAdjImpairment && in.Amount <= 0 {
		return nil, assetAdjRefused(assetAdjInputCode, "Amount", "Enter the loss above nought.")
	}
	if kind == AssetAdjRevaluation && in.NewValue < 0 {
		return nil, assetAdjRefused(assetAdjInputCode, "New value", "The new value cannot be negative.")
	}
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return nil, err
	}
	data, status, err := fetchAssetData(tenantID, assetID)
	if err != nil {
		return nil, assetAdjRefused(assetAdjStateCode, "Asset", "That asset is not on file.")
	}
	if status != "Capitalised" {
		return nil, assetAdjRefused(assetAdjStateCode, "Asset", "Only a Capitalised asset can be adjusted (it is %s).", status)
	}
	// What is due is posted first, so the base resets from a book value that is up to date.
	outcome, err := depreciateAsset(tenantID, schema, assetID, data, time.Now())
	if err != nil {
		LogSystemError(tenantID, "", "ERROR", "AdjustAsset", err.Error(), "")
		return nil, assetAdjRefused(assetAdjStateCode, "Asset", "The depreciation due could not be posted, so nothing was adjusted.")
	}
	if len(outcome.Skipped) > 0 {
		return nil, assetAdjRefused(assetAdjStateCode, "Asset", "Depreciation for %s is still to post (closed period). Post it first.", outcome.Skipped[0].Month)
	}

	tx, err := db.DB.Begin()
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	var raw string
	if err := tx.QueryRow(fmt.Sprintf(`SELECT data, status FROM %s.documents WHERE doctype = 'Asset' AND id = $1 FOR UPDATE`, schema), assetID).Scan(&raw, &status); err != nil {
		return nil, assetAdjRefused(assetAdjStateCode, "Asset", "That asset is not on file.")
	}
	data = map[string]interface{}{}
	if err := json.Unmarshal([]byte(raw), &data); err != nil {
		return nil, err
	}
	if status != "Capitalised" {
		return nil, assetAdjRefused(assetAdjStateCode, "Asset", "Only a Capitalised asset can be adjusted (it is %s).", status)
	}
	history, _ := data["asset_adjustments"].([]interface{})
	if key := strings.TrimSpace(in.IdempotencyKey); key != "" {
		for _, h := range history {
			if m, _ := h.(map[string]interface{}); m != nil && strField(m, "key") == key {
				res := &AssetAdjustmentResult{AssetID: assetID, Kind: strField(m, "kind"), Replayed: true,
					CarryingBefore: numFromInterface(m["carrying_before"]), CarryingAfter: numFromInterface(m["carrying_after"]),
					Loss: numFromInterface(m["loss"]), ToReserve: numFromInterface(m["to_reserve"]), FromReserve: numFromInterface(m["from_reserve"]),
					Reserve: numFromInterface(data["revaluation_reserve_paise"]) / 100, MonthlyCharge: numFromInterface(m["monthly_charge"])}
				return res, nil
			}
		}
	}

	plan, ok := assetDepreciationPlan(data, "")
	if !ok {
		return nil, assetAdjRefused(assetAdjStateCode, "Asset", "This asset has no useful life, cost or capitalisation date to depreciate from.")
	}
	idx := plan.monthsDue(time.Now()) + 1
	if idx > plan.months {
		return nil, assetAdjRefused(assetAdjStateCode, "Asset", "This asset is fully depreciated - there is no life left to spread over.")
	}
	var accumulated int64
	if err := tx.QueryRow(fmt.Sprintf(`SELECT COALESCE(SUM(credit - debit), 0) FROM %s.gl_postings
		WHERE document_type = 'Asset' AND document_id = $1 AND account_code = $2`, schema), assetID, assetAccAccumulated).Scan(&accumulated); err != nil {
		return nil, err
	}
	cost := assetCostPaise(data)
	carrying := cost - accumulated
	reserve := int64(numFromInterface(data["revaluation_reserve_paise"]))

	debits, credits := map[string]int64{}, map[string]int64{}
	var loss, toReserve, fromReserve, credit1410 int64
	switch kind {
	case AssetAdjImpairment:
		loss = RupeesToPaise(in.Amount)
		if loss > carrying {
			return nil, assetAdjRefused(assetAdjInputCode, "Amount", "The loss cannot be more than the book value, %.2f.", PaiseToRupees(carrying))
		}
		debits[assetAccImpairment], credits[assetAccAccumulated] = loss, loss
		credit1410 = loss
	default:
		diff := RupeesToPaise(in.NewValue) - carrying
		switch {
		case diff == 0:
			return nil, assetAdjRefused(assetAdjInputCode, "New value", "That is the value the asset is carried at already.")
		case diff > 0:
			toReserve = diff
			debits[assetAccFixed], credits[assetAccRevalReserve] = diff, diff
			cost += diff
			reserve += diff
			data["original_cost_paise"] = float64(assetCostBaseline(data))
			data["capitalised_paise"] = float64(cost)
		default:
			down := -diff
			fromReserve = down
			if fromReserve > reserve {
				fromReserve = reserve
			}
			loss = down - fromReserve
			if fromReserve > 0 {
				debits[assetAccRevalReserve] = fromReserve
			}
			if loss > 0 {
				debits[assetAccImpairment] = loss
			}
			credits[assetAccAccumulated] = down
			credit1410 = down
			reserve -= fromReserve
		}
	}

	seq := len(history) + 1
	if err := PostDoubleEntryTx(tx, tenantID, schema, "Asset", assetID, debits, credits, date,
		fmt.Sprintf("Asset:%s:ADJ:%d", assetID, seq)); err != nil {
		return nil, err
	}

	newAccum := accumulated + credit1410
	newCarrying := cost - newAccum
	remaining := int64(plan.months - idx + 1)
	per := newCarrying / remaining
	last := newCarrying - per*(remaining-1)
	rebases, _ := data["dep_rebases"].([]interface{})
	data["dep_rebases"] = append(rebases, map[string]interface{}{"idx": float64(idx), "per": float64(per), "last": float64(last)})
	data["adjustment_accumulated_paise"] = float64(assetAdjustmentAccumulated(data) + credit1410)
	data["revaluation_reserve_paise"] = float64(reserve)
	entry := map[string]interface{}{
		"seq": seq, "kind": kind, "date": date, "reason": reason, "by": userID, "key": strings.TrimSpace(in.IdempotencyKey),
		"carrying_before": PaiseToRupees(carrying), "carrying_after": PaiseToRupees(newCarrying),
		"loss": PaiseToRupees(loss), "to_reserve": PaiseToRupees(toReserve), "from_reserve": PaiseToRupees(fromReserve),
		"monthly_charge": PaiseToRupees(per), "from_month_index": idx,
	}
	data["asset_adjustments"] = append(history, entry)
	out, err := json.Marshal(data)
	if err != nil {
		return nil, err
	}
	if _, err := tx.Exec(fmt.Sprintf(`UPDATE %s.documents SET data = $1, updated_at = CURRENT_TIMESTAMP WHERE doctype = 'Asset' AND id = $2`, schema), out, assetID); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	LogAuditEvent(tenantID, userID, "ASSET_"+strings.ToUpper(kind), "SUCCESS",
		fmt.Sprintf("Asset %s %s: book value %.2f -> %.2f (%s)", assetID, strings.ToLower(kind), PaiseToRupees(carrying), PaiseToRupees(newCarrying), reason))
	return &AssetAdjustmentResult{AssetID: assetID, Kind: kind, CarryingBefore: PaiseToRupees(carrying), CarryingAfter: PaiseToRupees(newCarrying),
		Loss: PaiseToRupees(loss), ToReserve: PaiseToRupees(toReserve), FromReserve: PaiseToRupees(fromReserve),
		Reserve: PaiseToRupees(reserve), MonthlyCharge: PaiseToRupees(per)}, nil
}

// assetCostBaseline is the cost the plan's first months were planned on: the
// one already remembered, else the cost now (before this adjustment moves it).
func assetCostBaseline(data map[string]interface{}) int64 {
	if o := int64(numFromInterface(data["original_cost_paise"])); o > 0 {
		return o
	}
	return assetCostPaise(data)
}
