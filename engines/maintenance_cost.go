package engines

import (
	"custom_erp/db"
	"database/sql"
	"fmt"
	"math"
	"sort"
	"strings"
)

// Stage 170.8: maintenance order cost.
//
// The order always records its parts (item, qty, location), labour hours x
// rate and downtime hours. With the setting assets.maintenance_costing on,
// completing the order also issues the parts from stock at the location named
// on each line and posts the cost in the same transaction:
//
//	Dr 5314 Repairs & Maintenance Expense  (parts at valuated cost + labour)
//	  Cr 1200 Inventory                    (parts)
//	  Cr 5315 Maintenance Labour Absorbed  (hours x rate)
//
// The default is off: completion then only records, exactly as before.

const (
	SettingMaintenanceCosting = "assets.maintenance_costing"

	maintenanceExpenseAccount  = "5314"
	maintenanceLabourAccount   = "5315"
	maintenanceInventoryAcc    = "1200"
	maintenanceCostCode        = "ASSET-1728"
	maintenanceCostAccountHint = "Run the migrations so account 5314 and 5315 exist."
)

func init() {
	RegisterSetting(SettingDefinition{
		Key: SettingMaintenanceCosting, Module: "Assets",
		Label: "Cost of maintenance orders", Type: SettingTypeBool, Default: "false",
		Description: "Completing a maintenance order issues its parts from stock and posts the parts and labour cost.",
	})
}

func maintenanceCostRefused(sub, format string, args ...interface{}) error {
	return &ValidationError{Code: maintenanceCostCode, SubFor: sub, Message: fmt.Sprintf(format, args...)}
}

// checkMaintenancePartsForCosting refuses, before anything is posted, a part
// with no location or a fractional quantity when costing is on.
func checkMaintenancePartsForCosting(tenantID string, parts []MaintenancePart) error {
	for _, p := range parts {
		if strings.TrimSpace(p.Location) == "" {
			return maintenanceCostRefused("Location", "Choose the store %s is taken from.", p.Sku)
		}
		if p.Qty != math.Trunc(p.Qty) {
			return maintenanceCostRefused("Qty", "%s: stock is issued in whole units.", p.Sku)
		}
		if loc, _, err := fetchDocData(tenantID, "Location", strings.TrimSpace(p.Location)); err != nil || loc == nil {
			return maintenanceCostRefused("Location", "No store %q.", p.Location)
		}
	}
	return nil
}

// postMaintenanceCostTx issues the parts and posts the cost inside the
// completion transaction. It does nothing while the setting is off.
func postMaintenanceCostTx(tx *sql.Tx, tenantID, schema, orderID string, data map[string]interface{}, parts []MaintenancePart, hours, rate float64) error {
	if !GetSettingBool(tenantID, SettingMaintenanceCosting) {
		return nil
	}
	if _, done := data["cost_posted"]; done {
		return nil // posted already: a completion is not repeated
	}
	if err := db.SetSearchPath(tx, schema); err != nil {
		return err
	}
	byLocation := map[string][]interface{}{}
	var partsCost int64
	for _, p := range parts {
		item, err := ResolveItemBySKU(tenantID, p.Sku)
		if err != nil || item == nil {
			return maintenanceCostRefused("Part", "No item %q.", p.Sku)
		}
		if err := RefuseNonStockItems(tenantID, []string{p.Sku}, "issued to a maintenance order"); err != nil {
			return err
		}
		loc := strings.TrimSpace(p.Location)
		book, err := BookStockAt(tenantID, p.Sku, loc)
		if err != nil {
			return err
		}
		if float64(book) < p.Qty {
			return maintenanceCostRefused("Qty", "%s: %.0f needed but store %s has %d.", p.Sku, p.Qty, loc, book)
		}
		byLocation[loc] = append(byLocation[loc], map[string]interface{}{"sku": p.Sku, "qty": -int(p.Qty)})
		if ItemPolicyFor(tenantID, item.Data).Valuated() {
			unit, _ := ResolveQuoteUnitCostPaise(tenantID, p.Sku)
			partsCost += unit * int64(p.Qty)
		}
	}
	locs := make([]string, 0, len(byLocation))
	for l := range byLocation {
		locs = append(locs, l)
	}
	sort.Strings(locs)
	for _, loc := range locs {
		_, ledger, err := PostInventoryLedgerWithVoucherTx(tx, schema, loc, byLocation[loc], false)
		if err != nil {
			return err
		}
		if err := writeStockLedgerLinesInTx(tx, schema, loc, "MaintenanceOrder", orderID, strField(data, "completed_by"), ledger); err != nil {
			return err
		}
	}
	labourCost := RupeesToPaise(hours * rate)
	total := partsCost + labourCost
	if total > 0 {
		credits := map[string]int64{}
		if partsCost > 0 {
			credits[maintenanceInventoryAcc] = partsCost
		}
		if labourCost > 0 {
			credits[maintenanceLabourAccount] = labourCost
		}
		if err := PostDoubleEntryTx(tx, tenantID, schema, "MaintenanceOrder", orderID,
			map[string]int64{maintenanceExpenseAccount: total}, credits, "",
			"MaintenanceOrder:"+orderID+":COST"); err != nil {
			return err
		}
	}
	data["cost_posted"] = PaiseToRupees(total)
	data["parts_cost"] = PaiseToRupees(partsCost)
	data["labour_cost"] = PaiseToRupees(labourCost)
	return nil
}
