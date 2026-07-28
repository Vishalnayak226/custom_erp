package engines

import (
	"custom_erp/db"
	"encoding/json"
	"fmt"
	"time"
)

// Stage 26.9.10 (Manufacturing/MRP Sprint P2 follow-up): finite/infinite
// capacity scheduling. Go-ahead given 2026-07-27 for all five P2 bundles
// previously deferred pending a real pilot customer - this is scoped as a
// read-only scheduling suggestion (same no-auto-created-documents precedent
// GetReplenishmentSuggestions/GetMRPSuggestions/GetABCCycleCountPlan already
// set), sequencing every open, routed Production Order's operations against
// each work center's existing capacity_hours_per_day (26.9.3).
//
// Finite mode pushes an operation to the first future day its work center
// actually has room for it, earliest-due-order-first. Infinite mode ignores
// capacity entirely and schedules every operation on its order's own ideal
// start day - the two are deliberately returned side by side so the caller
// can see exactly how much a work center's capacity is actually stretching
// the schedule out, which is the whole point of offering both.
type ScheduleEntry struct {
	OrderID       string  `json:"order_id"`
	Seq           int     `json:"seq"`
	WorkCenterID  string  `json:"work_center_id"`
	NeededMinutes float64 `json:"needed_minutes"`
	FiniteDate    string  `json:"finite_date"`
	InfiniteDate  string  `json:"infinite_date"`
	Overflow      bool    `json:"overflow"` // true if FiniteDate had to move past the order's own due_date
}

type openProductionOrder struct {
	ID        string
	RoutingID string
	Quantity  float64
	DueDate   string
	CreatedAt time.Time
}

func fetchOpenRoutedProductionOrders(tenantID string) ([]openProductionOrder, error) {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return nil, err
	}
	rows, err := db.DB.Query(fmt.Sprintf(`
		SELECT id, COALESCE(data->>'routing_id', ''), COALESCE((data->>'quantity')::numeric, 0),
		       COALESCE(data->>'due_date', ''), created_at
		FROM %s.documents
		WHERE doctype = 'ProductionOrder' AND status IN ('Draft', 'Material Issued', 'In Process')
		  AND COALESCE(data->>'routing_id', '') != ''
		ORDER BY (data->>'due_date') NULLS LAST, created_at ASC`, schema))
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []openProductionOrder{}
	for rows.Next() {
		var o openProductionOrder
		if err := rows.Scan(&o.ID, &o.RoutingID, &o.Quantity, &o.DueDate, &o.CreatedAt); err != nil {
			continue
		}
		out = append(out, o)
	}
	// Orders with no due_date sort after ones that have it, oldest-created
	// first within each group - "NULLS LAST" above only applies to a real
	// SQL NULL, and an empty string isn't one, so re-sort in Go instead of
	// fighting the JSONB text-comparison ordering for that edge case.
	withDue, withoutDue := []openProductionOrder{}, []openProductionOrder{}
	for _, o := range out {
		if o.DueDate != "" {
			withDue = append(withDue, o)
		} else {
			withoutDue = append(withoutDue, o)
		}
	}
	return append(withDue, withoutDue...), nil
}

func GetProductionSchedule(tenantID string) ([]ScheduleEntry, error) {
	orders, err := fetchOpenRoutedProductionOrders(tenantID)
	if err != nil {
		return nil, err
	}

	today := time.Now().Truncate(24 * time.Hour)
	// finiteUsed tracks cumulative minutes already booked per work-center/day
	// as orders are processed earliest-due-first - a simple greedy bin-pack,
	// not an optimizer, matching this codebase's other suggestion engines
	// (e.g. 26.5.12's slotting optimizer is the same class of tool).
	finiteUsed := map[string]float64{} // key: workCenterID + "|" + date
	capacityCache := map[string]float64{}

	entries := []ScheduleEntry{}
	for _, o := range orders {
		ops, err := fetchRoutingOperations(tenantID, o.RoutingID)
		if err != nil {
			continue // an order with a broken/inactive routing is skipped, not fatal to the whole schedule
		}
		infiniteStart := today
		if o.DueDate != "" {
			if d, perr := time.Parse("2006-01-02", o.DueDate); perr == nil {
				infiniteStart = d
			}
		}
		for _, op := range ops {
			needed := op.SetupTimeMins + op.RunTimeMinsPerUnit*o.Quantity
			capMins, ok := capacityCache[op.WorkCenterID]
			if !ok {
				capMins, _ = workCenterDailyCapacityMins(tenantID, op.WorkCenterID)
				capacityCache[op.WorkCenterID] = capMins
			}

			// Infinite: no capacity ceiling, every operation lands on the
			// order's own ideal day regardless of what else is scheduled.
			infiniteDate := infiniteStart.Format("2006-01-02")

			// Finite: walk forward from today until this work center has
			// room left in a day for this operation's minutes. A single
			// operation that alone needs more than a full day's capacity
			// (or a work center with no capacity configured) would never
			// satisfy "fits under the cap" - schedule it on the first day
			// that's otherwise empty instead of looping forever chasing a
			// day that can never exist.
			finiteDate := today
			for {
				key := op.WorkCenterID + "|" + finiteDate.Format("2006-01-02")
				used := finiteUsed[key]
				if capMins <= 0 || used == 0 || used+needed <= capMins {
					finiteUsed[key] += needed
					break
				}
				finiteDate = finiteDate.AddDate(0, 0, 1)
			}
			finiteDateStr := finiteDate.Format("2006-01-02")

			entries = append(entries, ScheduleEntry{
				OrderID: o.ID, Seq: op.Seq, WorkCenterID: op.WorkCenterID, NeededMinutes: needed,
				FiniteDate: finiteDateStr, InfiniteDate: infiniteDate,
				Overflow: o.DueDate != "" && finiteDateStr > o.DueDate,
			})
		}
	}
	return entries, nil
}

// Stage 26.9.11: subcontracting/outside-processing. SendToSubcontractor
// moves the raw material out of stock (mirrors IssueProductionMaterial's
// negative-qty convention); ReceiveFromSubcontractor moves the processed/
// finished good back in - both through PostInventoryLedgerWithVoucher so
// they show up in the Stock Ledger report (26.10.1) like every other
// inventory movement, tagged voucher_type="SubcontractOrder".
func fetchSubcontractOrder(tenantID, id string) (map[string]interface{}, string, error) {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return nil, "", err
	}
	var dataStr, status string
	if err := db.DB.QueryRow(fmt.Sprintf(`SELECT data, status FROM %s.documents WHERE doctype = 'SubcontractOrder' AND id = $1`, schema), id).Scan(&dataStr, &status); err != nil {
		return nil, "", &ValidationError{Code: "MANUFA-0142", Message: fmt.Sprintf("subcontract order %s not found", id)}
	}
	var data map[string]interface{}
	if err := json.Unmarshal([]byte(dataStr), &data); err != nil {
		return nil, "", err
	}
	return data, status, nil
}

func saveSubcontractOrder(tenantID, id string, data map[string]interface{}, status string) error {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return err
	}
	data["status"] = status
	marshaled, err := json.Marshal(data)
	if err != nil {
		return err
	}
	_, err = db.DB.Exec(fmt.Sprintf(
		`UPDATE %s.documents SET data = $1, status = $2, updated_at = CURRENT_TIMESTAMP WHERE doctype = 'SubcontractOrder' AND id = $3`, schema),
		marshaled, status, id)
	return err
}

func SendToSubcontractor(tenantID, id, userID string) error {
	data, status, err := fetchSubcontractOrder(tenantID, id)
	if err != nil {
		return err
	}
	if status != "Draft" {
		return &ValidationError{Code: "MANUFA-0142", Message: fmt.Sprintf("only a Draft subcontract order can be sent (current status: %s)", status)}
	}
	sku, _ := data["sent_item_id"].(string)
	location, _ := data["location"].(string)
	qty := numFromInterface(data["sent_qty"])
	if sku == "" || location == "" || qty <= 0 {
		return &ValidationError{Code: "MANUFA-0142", Message: "sent_item_id, location, and a positive sent_qty are required to send a subcontract order"}
	}
	items := []interface{}{map[string]interface{}{"sku": sku, "qty": -qty}}
	if _, err := PostInventoryLedgerWithVoucher(tenantID, location, items, false, "SubcontractOrder", id, userID); err != nil {
		return fmt.Errorf("subcontract send failed: %v", err)
	}
	data["sent_date"] = time.Now().Format("2006-01-02")
	return saveSubcontractOrder(tenantID, id, data, "Sent")
}

func ReceiveFromSubcontractor(tenantID, id, userID string, actualQty float64) error {
	data, status, err := fetchSubcontractOrder(tenantID, id)
	if err != nil {
		return err
	}
	if status != "Sent" {
		return &ValidationError{Code: "MANUFA-0142", Message: fmt.Sprintf("only a Sent subcontract order can be received (current status: %s)", status)}
	}
	sku, _ := data["received_item_id"].(string)
	location, _ := data["location"].(string)
	if sku == "" || location == "" || actualQty <= 0 {
		return &ValidationError{Code: "MANUFA-0142", Message: "received_item_id, location, and a positive received qty are required to receive a subcontract order"}
	}
	items := []interface{}{map[string]interface{}{"sku": sku, "qty": actualQty}}
	if _, err := PostInventoryLedgerWithVoucher(tenantID, location, items, false, "SubcontractOrder", id, userID); err != nil {
		return fmt.Errorf("subcontract receive failed: %v", err)
	}
	data["actual_received_qty"] = actualQty
	data["received_date"] = time.Now().Format("2006-01-02")
	return saveSubcontractOrder(tenantID, id, data, "Received")
}
