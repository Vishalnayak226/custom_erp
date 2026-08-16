package engines

import (
	"custom_erp/db"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"time"
)

// Stage 26.5.6 (WMS Enterprise Maturity Sprint): wave/batch pick-list
// grouping, extending Stage 20.18's GenerateBinPickList (engines/wms.go)
// without changing it - GenerateBinPickList still answers "which bins for
// this one task," this file answers "which bins for every still-open task
// in a wave, visited once, then split back per order." Deliberately reads
// FulfillmentTask.items generically (its own small sku/qty/picked_qty/
// short_qty parse below) rather than importing engines/fulfillment_pickpack.go's
// unexported task-loading helpers, keeping this Stage's files independent of
// each other's internals.

// waveTaskItemRemainder is one task's still-open need for one SKU.
type waveTaskItemRemainder struct {
	TaskID    string
	CreatedAt time.Time
	Sku       string
	Remaining int
}

// AssignTasksToWave (26.5.6) tags a batch of still-open FulfillmentTasks
// with a shared wave_id - a plain additive JSON key (db/migrations_stage26_5
// _wms_enterprise.sql registers it for generic-table visibility only, it's
// not a new column). Tasks already Packed/Dispatched/Rejected are skipped
// rather than erroring the whole batch, so one already-finished task in a
// batch doesn't block tagging the rest.
func AssignTasksToWave(tenantID, waveID string, taskIDs []string, userID string) (tagged int, err error) {
	if waveID == "" {
		return 0, errors.New("wave id is required")
	}
	if len(taskIDs) == 0 {
		return 0, errors.New("at least one task id is required")
	}
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return 0, err
	}
	for _, taskID := range taskIDs {
		res, err := db.DB.Exec(fmt.Sprintf(`
			UPDATE %s.documents SET data = data || jsonb_build_object('wave_id', $1::text), updated_at = CURRENT_TIMESTAMP
			WHERE doctype = 'FulfillmentTask' AND id = $2 AND status IN ('Pending', 'Picking')`, schema),
			waveID, taskID)
		if err != nil {
			return tagged, err
		}
		if n, _ := res.RowsAffected(); n > 0 {
			tagged++
		}
	}
	LogAuditEvent(tenantID, userID, "WMS_WAVE_ASSIGN", "SUCCESS", fmt.Sprintf("Tagged %d of %d task(s) into wave %s", tagged, len(taskIDs), waveID))
	return tagged, nil
}

// WavePickLine is one consolidated "go to this bin, pick this much of this
// SKU" instruction covering every order in the wave that needs it - a
// picker visits each bin once per wave, not once per order.
type WavePickLine struct {
	Sku     string `json:"sku"`
	BinCode string `json:"bin_code"`
	Zone    string `json:"zone"`
	Aisle   string `json:"aisle"`
	Rack    string `json:"rack"`
	PickQty int    `json:"pick_qty"`
	// Stage 42.1.5: which lot to take, and when it expires. Both omitempty and
	// both empty for a FIFO (non-batch-tracked) allocation, so an existing
	// consumer of this payload sees exactly the JSON it saw before.
	BatchNo    string `json:"batch_no,omitempty"`
	ExpiryDate string `json:"expiry_date,omitempty"`
}

// WaveOrderAllocation is how much of a wave-picked SKU actually ended up
// allocated to one specific task, and how much fell short.
type WaveOrderAllocation struct {
	TaskID       string `json:"task_id"`
	Sku          string `json:"sku"`
	AllocatedQty int    `json:"allocated_qty"`
	Shortfall    int    `json:"shortfall,omitempty"`
}

// GenerateWavePickList (26.5.6) implements the design note validated
// against the retired WMS prototype: aggregate each SKU's total still-open
// qty across every task in the wave, allocate that total FIFO/oldest-stock-
// first across Good-condition bin_stock (bins whose stock has sat longest,
// by updated_at ascending, get consumed first - real inventory rotation,
// not just walk-route convenience), then distribute the allocated qty back
// per order needing it in oldest-task-first order (so an older order isn't
// starved by a newer one when stock falls short), and finally sort the
// resulting consolidated pick lines by zone-then-aisle-then-rack for one
// S-shape walking route covering the whole wave.
func GenerateWavePickList(tenantID, waveID string) ([]WavePickLine, []WaveOrderAllocation, error) {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return nil, nil, err
	}

	rows, err := db.DB.Query(fmt.Sprintf(`
		SELECT id, data, created_at FROM %s.documents
		WHERE doctype = 'FulfillmentTask' AND data->>'wave_id' = $1 AND status IN ('Pending', 'Picking')`, schema), waveID)
	if err != nil {
		return nil, nil, err
	}
	var remainders []waveTaskItemRemainder
	locationCode := ""
	taskCount := 0
	for rows.Next() {
		var id, dataStr string
		var createdAt time.Time
		if err := rows.Scan(&id, &dataStr, &createdAt); err != nil {
			rows.Close()
			return nil, nil, err
		}
		var data map[string]interface{}
		if err := json.Unmarshal([]byte(dataStr), &data); err != nil {
			continue
		}
		taskLoc, _ := data["location_code"].(string)
		if locationCode == "" {
			locationCode = taskLoc
		} else if taskLoc != "" && taskLoc != locationCode {
			rows.Close()
			return nil, nil, fmt.Errorf("wave %s spans more than one location (%s and %s) - a wave must be single-location", waveID, locationCode, taskLoc)
		}
		taskCount++
		rawItems, _ := data["items"].([]interface{})
		for _, ri := range rawItems {
			m, ok := ri.(map[string]interface{})
			if !ok {
				continue
			}
			sku, _ := m["sku"].(string)
			if sku == "" {
				continue
			}
			remaining := int(numFromInterface(m["qty"]) - numFromInterface(m["picked_qty"]) - numFromInterface(m["short_qty"]))
			if remaining > 0 {
				remainders = append(remainders, waveTaskItemRemainder{TaskID: id, CreatedAt: createdAt, Sku: sku, Remaining: remaining})
			}
		}
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, nil, err
	}
	if taskCount == 0 {
		return nil, nil, fmt.Errorf("wave %s has no open (Pending/Picking) tasks", waveID)
	}

	// Aggregate demand per SKU across the whole wave.
	demandBySku := map[string]int{}
	var skus []string
	for _, r := range remainders {
		if _, seen := demandBySku[r.Sku]; !seen {
			skus = append(skus, r.Sku)
		}
		demandBySku[r.Sku] += r.Remaining
	}
	sort.Strings(skus)

	var pickLines []WavePickLine
	allocatedTotalBySku := map[string]int{}
	for _, sku := range skus {
		demand := demandBySku[sku]
		// Stage 42.1.5: the bin-selection query that used to live inline here is
		// now AllocateFromStock, which picks FIFO or FEFO from the item's own
		// tracking_mode and applies the expiry gates. FIFO's SQL is byte-for-byte
		// the query this block used to run, so a warehouse with no batch-tracked
		// item gets the same pick list it got before.
		candidates, shortfall, err := AllocateFromStock(tenantID, sku, locationCode, demand)
		if err != nil {
			return nil, nil, err
		}
		for _, c := range candidates {
			pickLines = append(pickLines, WavePickLine{
				Sku: sku, BinCode: c.BinCode, Zone: c.Zone, Aisle: c.Aisle, Rack: c.Rack,
				PickQty: c.Qty, BatchNo: c.BatchNo, ExpiryDate: c.ExpiryDate,
			})
		}
		allocatedTotalBySku[sku] = demand - shortfall
	}

	// Distribute each SKU's allocated total back to the tasks that need it,
	// oldest task first.
	sort.SliceStable(remainders, func(i, j int) bool { return remainders[i].CreatedAt.Before(remainders[j].CreatedAt) })
	remainingAllocBySku := map[string]int{}
	for sku, total := range allocatedTotalBySku {
		remainingAllocBySku[sku] = total
	}
	var allocations []WaveOrderAllocation
	for _, r := range remainders {
		give := remainingAllocBySku[r.Sku]
		if give > r.Remaining {
			give = r.Remaining
		}
		remainingAllocBySku[r.Sku] -= give
		allocations = append(allocations, WaveOrderAllocation{
			TaskID: r.TaskID, Sku: r.Sku, AllocatedQty: give, Shortfall: r.Remaining - give,
		})
	}

	sort.SliceStable(pickLines, func(i, j int) bool {
		a, b := pickLines[i], pickLines[j]
		if a.Zone != b.Zone {
			return a.Zone < b.Zone
		}
		if a.Aisle != b.Aisle {
			return a.Aisle < b.Aisle
		}
		if a.Rack != b.Rack {
			return a.Rack < b.Rack
		}
		if a.BinCode != b.BinCode {
			return a.BinCode < b.BinCode
		}
		// 42.1.5: two lots of one SKU can share a bin, so the walk-route sort
		// needs a final tiebreaker or their order is whatever the map iteration
		// happened to produce - and a pick list that reorders itself between two
		// identical calls is not one a picker can trust.
		return a.BatchNo < b.BatchNo
	})

	return pickLines, allocations, nil
}
