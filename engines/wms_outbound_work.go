package engines

import (
	"custom_erp/db"
	"fmt"
	"sort"
	"strings"
)

// Stage 55.8 - outbound work an operator can pick instead of remember.
//
// A wave was built by typing a wave id and a comma-separated list of task
// ids; mobile picking and loading asked for ids again. OutboundWork reads the
// same records back as a queue for one location: the fulfillment tasks that
// can still go into a wave (Pending or Picking, in no wave yet), every open
// wave with how many of its tasks are at each step, the loads that are open
// at a door, and the invoiced packages not yet on a trailer. CreateWave turns
// a selection into a wave - a new one, numbered for the location and day, or
// an open one at the same location - and says which tasks it would not take.
// Nothing here moves stock: picking reserves nothing new and dispatch still
// posts through the fulfillment transition, exactly once.

// OutboundTask is one fulfillment task that can go into a wave.
type OutboundTask struct {
	ID       string  `json:"id"`
	OrderID  string  `json:"order_id"`
	Customer string  `json:"customer,omitempty"`
	Status   string  `json:"status"`
	Lines    int     `json:"lines"`
	Units    float64 `json:"units"`
	OwnerID  string  `json:"owner_id,omitempty"`
}

// OutboundWave is an open wave and where its tasks are.
type OutboundWave struct {
	WaveID    string         `json:"wave_id"`
	Status    string         `json:"status,omitempty"`
	Location  string         `json:"location"`
	Tasks     int            `json:"tasks"`
	Units     float64        `json:"units"`
	ByStatus  map[string]int `json:"by_status"`
	Remaining int            `json:"remaining"`
	TaskIDs   []string       `json:"task_ids"`
}

// ReadyPackage is an invoiced package not yet scanned onto a trailer.
type ReadyPackage struct {
	ID         string `json:"id"`
	OrderID    string `json:"order_id,omitempty"`
	ManifestID string `json:"manifest_id,omitempty"`
	Location   string `json:"location,omitempty"`
}

// OutboundWorkList is the outbound queue for a location.
type OutboundWorkList struct {
	Location string            `json:"location"`
	Eligible []OutboundTask    `json:"eligible"`
	Waves    []OutboundWave    `json:"waves"`
	Loads    []LoadingTaskInfo `json:"loads"`
	Ready    []ReadyPackage    `json:"ready_to_load"`
}

func outboundTaskOpen(status string) bool {
	return status == "Pending" || status == "Picking" || status == "Packed"
}

// OutboundWork lists a location's outbound queue (all locations when blank).
func OutboundWork(tenantID, location string) (OutboundWorkList, error) {
	out := OutboundWorkList{Location: location, Eligible: []OutboundTask{}, Waves: []OutboundWave{}, Loads: []LoadingTaskInfo{}, Ready: []ReadyPackage{}}
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return out, err
	}
	tasks, err := docsByType(schema, "FulfillmentTask")
	if err != nil {
		return out, err
	}
	orders := map[string]string{}
	if orderDocs, err := docsByType(schema, "SalesOrder"); err == nil {
		for _, o := range orderDocs {
			orders[strField(o, "id")] = strField(o, "customer_name")
		}
	}
	waves := map[string]*OutboundWave{}
	for _, t := range tasks {
		loc, status, wave := strField(t, "location_code"), strField(t, "status"), strField(t, "wave_id")
		if location != "" && loc != location {
			continue
		}
		var lines int
		var units float64
		for _, it := range jsonRows(t["items"]) {
			lines++
			units += numFromInterface(it["qty"])
		}
		if wave == "" {
			if status == "Pending" || status == "Picking" {
				out.Eligible = append(out.Eligible, OutboundTask{ID: strField(t, "id"), OrderID: strField(t, "order_id"), Customer: orders[strField(t, "order_id")],
					Status: status, Lines: lines, Units: units, OwnerID: strField(t, "owner_id")})
			}
			continue
		}
		w := waves[wave]
		if w == nil {
			w = &OutboundWave{WaveID: wave, Location: loc, ByStatus: map[string]int{}}
			waves[wave] = w
		}
		w.Tasks++
		w.Units += units
		w.ByStatus[status]++
		w.TaskIDs = append(w.TaskIDs, strField(t, "id"))
		if status == "Pending" || status == "Picking" {
			w.Remaining++
		}
	}
	for _, w := range waves {
		if wave, err := getWave(schema, w.WaveID); err == nil && wave != nil {
			w.Status = wave.Status
			if wave.Status == "Closed" {
				continue
			}
		}
		open := false
		for st := range w.ByStatus {
			if outboundTaskOpen(st) {
				open = true
			}
		}
		if open {
			sort.Strings(w.TaskIDs)
			out.Waves = append(out.Waves, *w)
		}
	}
	sort.Slice(out.Waves, func(i, j int) bool { return out.Waves[i].WaveID > out.Waves[j].WaveID })
	sort.Slice(out.Eligible, func(i, j int) bool { return out.Eligible[i].ID < out.Eligible[j].ID })

	loads, err := docsByType(schema, "LoadingTask")
	if err == nil {
		for _, l := range loads {
			st := strField(l, "status")
			if st != "Planned" && st != "Loading" && st != "Loaded" {
				continue
			}
			if info, err := getLoadingTask(schema, strField(l, "id")); err == nil && info != nil {
				out.Loads = append(out.Loads, *info)
			}
		}
	}
	rows, err := db.DB.Query(fmt.Sprintf(`
		SELECT p.id, COALESCE(p.data->>'order_id',''), COALESCE(b.data->>'manifest_id',''), COALESCE(p.data->>'location_code','')
		  FROM %s.documents p
		  LEFT JOIN %s.documents b ON b.doctype = 'LogisticsBooking' AND b.deleted_at IS NULL AND b.data->>'shipping_package_id' = p.id
		 WHERE p.doctype = 'ShippingPackage' AND p.deleted_at IS NULL AND p.status = 'Invoiced'
		   AND COALESCE(p.data->>'loaded_at','') = ''
		   AND ($1 = '' OR COALESCE(p.data->>'location_code','') IN ('', $1))
		 ORDER BY 3, 1`, schema, schema), location)
	if err != nil {
		return out, err
	}
	defer rows.Close()
	for rows.Next() {
		var p ReadyPackage
		if err := rows.Scan(&p.ID, &p.OrderID, &p.ManifestID, &p.Location); err != nil {
			return out, err
		}
		out.Ready = append(out.Ready, p)
	}
	return out, rows.Err()
}

// WaveResult is what CreateWaveFromSelection did.
type WaveResult struct {
	WaveID  string            `json:"wave_id"`
	Tagged  []string          `json:"tagged"`
	Refused map[string]string `json:"refused,omitempty"`
}

// CreateWaveFromSelection puts the chosen tasks into a wave - a new
// registered Wave (CreateWave, Planned) or an open one at the same location -
// after checking each task: at that location, still Pending or Picking, and
// in no other wave. A task that fails is named with the reason, not silently
// skipped.
func CreateWaveFromSelection(tenantID, location, waveID string, taskIDs []string, userID string) (WaveResult, error) {
	res := WaveResult{Refused: map[string]string{}}
	location, waveID = strings.TrimSpace(location), strings.TrimSpace(waveID)
	if location == "" || len(taskIDs) == 0 {
		return res, &ValidationError{Code: "INVENT-0499", Message: "choose the location and at least one task"}
	}
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return res, err
	}
	if waveID != "" {
		wave, err := getWave(schema, waveID)
		if err != nil || wave == nil {
			return res, &ValidationError{Code: "INVENT-0499", Message: fmt.Sprintf("wave %s not found", waveID)}
		}
		if wave.LocationCode != location {
			return res, &ValidationError{Code: "INVENT-0499", Message: fmt.Sprintf("wave %s is at %s, not %s", waveID, wave.LocationCode, location)}
		}
		if wave.Status != "Planned" && wave.Status != "Released" && wave.Status != "In Progress" {
			return res, &ValidationError{Code: "INVENT-0499", Message: fmt.Sprintf("wave %s is %s - start a new wave", waveID, wave.Status)}
		}
	}
	var ok []string
	for _, id := range taskIDs {
		data, status, err := fetchDocData(tenantID, "FulfillmentTask", id)
		switch {
		case err != nil || data == nil:
			res.Refused[id] = "not found"
		case strField(data, "location_code") != location:
			res.Refused[id] = "at " + strField(data, "location_code")
		case status != "Pending" && status != "Picking":
			res.Refused[id] = "already " + status
		case strField(data, "wave_id") != "" && strField(data, "wave_id") != waveID:
			res.Refused[id] = "in wave " + strField(data, "wave_id")
		default:
			ok = append(ok, id)
		}
	}
	if len(ok) == 0 {
		return res, &ValidationError{Code: "INVENT-0499", Message: "none of these tasks can go into a wave - they are at another location, already in a wave, or past picking"}
	}
	if waveID == "" {
		newID, _, err := CreateWave(tenantID, location, "", ok, "Manual", userID)
		if err != nil {
			return res, err
		}
		res.WaveID = newID
	} else {
		if _, err := AssignTasksToWave(tenantID, waveID, ok, userID); err != nil {
			return res, err
		}
		_, _ = db.DB.Exec(fmt.Sprintf(`UPDATE %s.documents SET data = data || jsonb_build_object('task_count',
			(SELECT COUNT(*) FROM %s.documents t WHERE t.doctype = 'FulfillmentTask' AND t.data->>'wave_id' = $1)) WHERE doctype = 'Wave' AND id = $1`, schema, schema), waveID)
		res.WaveID = waveID
	}
	res.Tagged = ok
	return res, nil
}
