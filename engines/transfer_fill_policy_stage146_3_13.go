package engines

import (
	"custom_erp/db"
	"database/sql"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
)

// Stage 146.3.13 (D22): the transfer fill policy. Today a transfer ships
// exactly its approved quantity or fails. With inventory.transfer_fill_policy
// on, a transfer has one of three policies:
//
//   - Exact: today - every line ships in full or the dispatch fails.
//   - Ship what is there: each line ships what the source can fill; the rest
//     is closed with a reason code and recorded on the transfer.
//   - Complete: nothing ships until every line can be filled; until then the
//     transfer waits, Approved, and the dispatch says what is short.
//
// An optional minimum fill % refuses a "ship what is there" dispatch that
// would send less than that share of the ordered units. The policy of a
// transfer is, in order: its own fill_policy field, the destination store's
// transfer_fill_policy, then its purpose (replenishment -> Ship what is
// there; new store / launch allocation -> Complete). Setting off = Exact for
// every transfer, whatever its fields say.

const (
	// TransferFillPolicySetting switches the fill policies on.
	TransferFillPolicySetting = "inventory.transfer_fill_policy"

	FillExact           = "Exact"
	FillShipWhatIsThere = "Ship what is there"
	FillComplete        = "Complete"

	transferPurposeReplenishment = "Replenishment"
)

func init() {
	RegisterSetting(SettingDefinition{
		Key: TransferFillPolicySetting, Module: "Warehouse",
		Label: "Transfers use a fill policy", Type: SettingTypeBool, Default: "false",
		Description: "A replenishment transfer ships what the source has and closes the rest with a reason; a new-store or launch transfer waits until every line can be filled. A store or one transfer can override. Off = every transfer ships its exact approved quantity.",
	})
}

// transferFillRule is a transfer's resolved policy.
type transferFillRule struct {
	Policy string
	MinPct float64
	Source string // transfer, store, purpose or setting off
}

func normaliseFillPolicy(s string) string {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "exact":
		return FillExact
	case "ship what is there", "ship what is there.", "partial":
		return FillShipWhatIsThere
	case "complete":
		return FillComplete
	}
	return ""
}

func fieldFloat(v interface{}) float64 {
	switch x := v.(type) {
	case float64:
		return x
	case string:
		f, _ := strconv.ParseFloat(strings.TrimSpace(x), 64)
		return f
	}
	return 0
}

// resolveTransferFill reads the setting, the transfer and its destination
// store. It never fails: an unreadable store simply has no override.
func resolveTransferFill(tenantID, schema string, data map[string]interface{}) (transferFillRule, error) {
	if !GetSettingBool(tenantID, TransferFillPolicySetting) {
		return transferFillRule{Policy: FillExact, Source: "setting off"}, nil
	}
	to, _ := data["to_warehouse"].(string)
	var storePolicy string
	var storeMin float64
	if to != "" {
		var raw []byte
		if err := db.DB.QueryRow(fmt.Sprintf(`SELECT data FROM %s.documents
			WHERE doctype = 'Location' AND deleted_at IS NULL AND data->>'code' = $1 ORDER BY created_at LIMIT 1`, schema), to).Scan(&raw); err == nil {
			var loc map[string]interface{}
			if json.Unmarshal(raw, &loc) == nil {
				s, _ := loc["transfer_fill_policy"].(string)
				storePolicy = normaliseFillPolicy(s)
				storeMin = fieldFloat(loc["transfer_min_fill_pct"])
			}
		}
	}
	rule := transferFillRule{MinPct: fieldFloat(data["min_fill_pct"])}
	if rule.MinPct <= 0 {
		rule.MinPct = storeMin
	}
	if p, _ := data["fill_policy"].(string); normaliseFillPolicy(p) != "" {
		rule.Policy, rule.Source = normaliseFillPolicy(p), "transfer"
		return rule, nil
	}
	if storePolicy != "" {
		rule.Policy, rule.Source = storePolicy, "store"
		return rule, nil
	}
	purpose, _ := data["purpose"].(string)
	switch strings.ToLower(strings.TrimSpace(purpose)) {
	case "new store", "launch allocation", "launch":
		rule.Policy = FillComplete
	default:
		rule.Policy = FillShipWhatIsThere
	}
	rule.Source = "purpose"
	return rule, nil
}

// ShortClosedLine is one line of a transfer closed short at dispatch.
type ShortClosedLine struct {
	Sku        string `json:"sku"`
	Ordered    int    `json:"ordered"`
	Shipped    int    `json:"shipped"`
	Short      int    `json:"short"`
	ReasonCode string `json:"reason_code,omitempty"`
}

// fillableAtTx is what the source can ship of one line right now: Main stock
// less what is blocked or held (the same floor dispatch applies).
func fillableAtTx(tx *sql.Tx, schema, sku, location string) (int, error) {
	var avail int
	err := tx.QueryRow(fmt.Sprintf(
		`SELECT available - blocked - hold_qty FROM %s.inventory_availability WHERE sku = $1 AND location_code = $2 FOR UPDATE`, schema),
		sku, location).Scan(&avail)
	if err == sql.ErrNoRows {
		return 0, nil
	}
	if avail < 0 {
		avail = 0
	}
	return avail, err
}

// TransferFillLine is one line of a fill check.
type TransferFillLine struct {
	Sku     string `json:"sku"`
	Ordered int    `json:"ordered"`
	Fill    int    `json:"fill"`
	Short   int    `json:"short"`
}

// TransferFill answers "what would dispatch ship now" without moving stock.
type TransferFill struct {
	Policy  string             `json:"policy"`
	Source  string             `json:"source"`
	MinPct  float64            `json:"min_fill_pct"`
	FillPct float64            `json:"fill_pct"`
	CanShip bool               `json:"can_ship"`
	Lines   []TransferFillLine `json:"lines"`
}

// TransferFillCheck is the read-only form of the dispatch decision, for the
// transfer screen. Stock is read, never changed.
func TransferFillCheck(tenantID, transferOrderID string) (TransferFill, error) {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return TransferFill{}, err
	}
	data, _, err := fetchDocData(tenantID, "TransferOrder", transferOrderID)
	if err != nil {
		return TransferFill{}, fmt.Errorf("transfer order not found: %v", err)
	}
	itemsStr, _ := data["items"].(string)
	lines, err := parseTransferItems(itemsStr)
	if err != nil {
		return TransferFill{}, err
	}
	from, _ := data["from_warehouse"].(string)
	rule, _ := resolveTransferFill(tenantID, schema, data)
	out := TransferFill{Policy: rule.Policy, Source: rule.Source, MinPct: rule.MinPct}
	var ordered, shipped int
	for _, l := range lines {
		var avail int
		_ = db.DB.QueryRow(fmt.Sprintf(`SELECT GREATEST(available - blocked - hold_qty, 0) FROM %s.inventory_availability
			WHERE sku = $1 AND location_code = $2`, schema), l.Sku, from).Scan(&avail)
		fill := l.Qty
		if avail < fill {
			fill = avail
		}
		out.Lines = append(out.Lines, TransferFillLine{Sku: l.Sku, Ordered: l.Qty, Fill: fill, Short: l.Qty - fill})
		ordered += l.Qty
		shipped += fill
	}
	if ordered > 0 {
		out.FillPct = float64(shipped) * 100 / float64(ordered)
	}
	switch rule.Policy {
	case FillExact, FillComplete:
		out.CanShip = shipped == ordered
	default:
		out.CanShip = shipped > 0 && out.FillPct+1e-9 >= rule.MinPct
	}
	return out, nil
}

// applyTransferFill is the one decision point dispatch calls once it has read
// each line's fillable quantity. It returns the lines to ship (a line with
// nothing to ship is dropped) and what was closed short, or the refusal.
func applyTransferFill(tenantID string, rule transferFillRule, lines []transferLine, fill map[string]int, reasonCode string) ([]transferLine, []ShortClosedLine, error) {
	var ship []transferLine
	var shorts []ShortClosedLine
	var ordered, shipped int
	for _, l := range lines {
		f := fill[l.Sku]
		ordered += l.Qty
		shipped += f
		if f < l.Qty {
			shorts = append(shorts, ShortClosedLine{Sku: l.Sku, Ordered: l.Qty, Shipped: f, Short: l.Qty - f})
		}
		if f > 0 {
			nl := l
			nl.Qty = f
			ship = append(ship, nl)
		}
	}
	if len(shorts) == 0 {
		return ship, nil, nil
	}
	if rule.Policy == FillComplete {
		parts := make([]string, 0, len(shorts))
		for _, s := range shorts {
			parts = append(parts, fmt.Sprintf("%s short %d", s.Sku, s.Short))
		}
		return nil, nil, &ValidationError{Code: "STOCKT-0490", Message: "this transfer waits until every line can be filled: " + strings.Join(parts, ", ")}
	}
	pct := float64(shipped) * 100 / float64(ordered)
	if shipped == 0 || pct+1e-9 < rule.MinPct {
		return nil, nil, &ValidationError{Code: "STOCKT-0489", Message: fmt.Sprintf(
			"the source can fill %.0f%% of this transfer (%d of %d units); the minimum to ship is %.0f%%", pct, shipped, ordered, rule.MinPct)}
	}
	if err := requireTransferReason(tenantID, reasonCode, TransferDiscrepancyReasonCategory, "closing the quantity the source cannot fill"); err != nil {
		return nil, nil, err
	}
	for i := range shorts {
		shorts[i].ReasonCode = reasonCode
	}
	return ship, shorts, nil
}
