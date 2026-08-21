package engines

import (
	"custom_erp/db"
	"fmt"
)

// Stage 42.2.10 - the warehouse cockpit: the single operational console both
// SAP EWM and Infor WMS have and this repo, before this item, did not. Every
// section below reads data that already exists (WarehouseTask from 42.2.1-
// 42.2.9, ASN from 26.5.1, TaskCompletionLog from 26.5.13, bin_stock/Bin
// from Stage 20) - this file is purely aggregation, no new storage. The
// plan's own "dock/appointment strip (populated in 42.3)" section is
// deliberately omitted rather than faked: 42.3 (dock scheduling) doesn't
// exist yet, and a strip with nothing real behind it would be worse than
// not showing one.

// CockpitTaskTypeSummary is one (task_type, status) bucket of currently-open
// WarehouseTasks, with the oldest one's age - "open tasks by type/age" from
// the plan.
type CockpitTaskTypeSummary struct {
	TaskType      string `json:"task_type"`
	Status        string `json:"status"`
	Count         int    `json:"count"`
	OldestAgeMins int    `json:"oldest_age_mins"`
}

// CockpitExceptionItem is one Exception-status WarehouseTask, with the
// reason code's own process_step/follow_on_action surfaced so a manager can
// see what it's waiting on without opening the record.
type CockpitExceptionItem struct {
	TaskID         string `json:"task_id"`
	TaskType       string `json:"task_type"`
	Item           string `json:"item,omitempty"`
	BinCode        string `json:"bin_code,omitempty"`
	ReasonCode     string `json:"reason_code,omitempty"`
	ProcessStep    string `json:"process_step,omitempty"`
	FollowOnAction string `json:"follow_on_action,omitempty"`
	AgeMins        int    `json:"age_mins"`
}

// CockpitWaveSummary is one wave's task counts by status, tagged via
// AssignTasksToWave (26.5.6/42.2's own wave_id key on FulfillmentTask).
type CockpitWaveSummary struct {
	WaveID string `json:"wave_id"`
	Status string `json:"status"`
	Count  int    `json:"count"`
}

// CockpitInboundItem is one ASN (26.5.1) expected at this location today.
type CockpitInboundItem struct {
	ASNNumber    string `json:"asn_number"`
	Vendor       string `json:"vendor,omitempty"`
	ExpectedDate string `json:"expected_date"`
	Status       string `json:"status"`
}

// CockpitBinUtilization is one bin with a configured capacity, and how full
// it currently is - only bins that opted into 42.2.6 capacity tracking can
// appear here, the same "criterion is only as strict/informative as the
// data behind it" posture the rest of this Stage takes.
type CockpitBinUtilization struct {
	BinCode string  `json:"bin_code"`
	UsedQty int     `json:"used_qty"`
	MaxQty  float64 `json:"max_qty"`
	PctUsed float64 `json:"pct_used"`
}

// WarehouseCockpit is the whole console's data for one location.
type WarehouseCockpit struct {
	LocationCode   string                   `json:"location_code"`
	OpenTasks      []CockpitTaskTypeSummary `json:"open_tasks"`
	ExceptionQueue []CockpitExceptionItem   `json:"exception_queue"`
	WaveStatus     []CockpitWaveSummary     `json:"wave_status"`
	// WaveMonitor (Stage 42.4.2) is the registered-Wave lifecycle view - a
	// real Wave document per row, with status/age - alongside WaveStatus
	// above, which is 26.5.6's older per-FulfillmentTask-status breakdown and
	// is kept unchanged since some tenants still only ever use the free-text
	// wave_id tag with no registered Wave document at all.
	WaveMonitor     []WaveMonitorRow        `json:"wave_monitor"`
	InboundDueToday []CockpitInboundItem    `json:"inbound_due_today"`
	Throughput      []LaborProductivity     `json:"throughput_today"`
	BinUtilization  []CockpitBinUtilization `json:"bin_utilization"`
}

// GetWarehouseCockpit (42.2.10) assembles every section as a best-effort
// independent query - one section failing (e.g. no ASN doctype rows yet)
// never blanks out the rest of the screen, matching this codebase's
// existing dashboard/report conventions of degrading gracefully rather than
// erroring the whole page.
func GetWarehouseCockpit(tenantID, locationCode string) (WarehouseCockpit, error) {
	out := WarehouseCockpit{LocationCode: locationCode}
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return out, err
	}

	out.OpenTasks = cockpitOpenTasks(schema, locationCode)
	out.ExceptionQueue = cockpitExceptionQueue(schema, locationCode)
	out.WaveStatus = cockpitWaveStatus(schema, locationCode)
	if wm, werr := GetWaveMonitor(tenantID, locationCode); werr == nil {
		out.WaveMonitor = wm
	} else {
		out.WaveMonitor = []WaveMonitorRow{}
	}
	out.InboundDueToday = cockpitInboundDueToday(schema, locationCode)
	if prod, err := GetLaborProductivity(tenantID, today().Format(isoDate), today().Format(isoDate)); err == nil {
		out.Throughput = prod
	} else {
		out.Throughput = []LaborProductivity{}
	}
	out.BinUtilization = cockpitBinUtilization(schema, locationCode)
	return out, nil
}

func cockpitOpenTasks(schema, locationCode string) []CockpitTaskTypeSummary {
	out := []CockpitTaskTypeSummary{}
	rows, err := db.DB.Query(fmt.Sprintf(`
		SELECT data->>'task_type', status, COUNT(*), COALESCE(EXTRACT(EPOCH FROM (CURRENT_TIMESTAMP - MIN(created_at))) / 60, 0)
		FROM %s.documents
		WHERE doctype = 'WarehouseTask' AND data->>'location_code' = $1
		  AND status IN ('Pending', 'Assigned', 'In Progress', 'Exception')
		GROUP BY data->>'task_type', status
		ORDER BY data->>'task_type', status`, schema), locationCode)
	if err != nil {
		return out
	}
	defer rows.Close()
	for rows.Next() {
		var s CockpitTaskTypeSummary
		var ageMins float64
		if rows.Scan(&s.TaskType, &s.Status, &s.Count, &ageMins) == nil {
			s.OldestAgeMins = int(ageMins)
			out = append(out, s)
		}
	}
	return out
}

func cockpitExceptionQueue(schema, locationCode string) []CockpitExceptionItem {
	out := []CockpitExceptionItem{}
	rows, err := db.DB.Query(fmt.Sprintf(`
		SELECT wt.id, COALESCE(wt.data->>'task_type', ''), COALESCE(wt.data->>'item', ''),
		       COALESCE(NULLIF(wt.data->>'to_bin', ''), wt.data->>'from_bin'),
		       COALESCE(wt.data->>'reason_code', ''),
		       COALESCE(rc.data->>'process_step', ''), COALESCE(rc.data->>'follow_on_action', ''),
		       COALESCE(EXTRACT(EPOCH FROM (CURRENT_TIMESTAMP - wt.updated_at)) / 60, 0)
		FROM %s.documents wt
		LEFT JOIN %s.documents rc ON rc.doctype = 'ReasonCode' AND rc.id = wt.data->>'reason_code'
		WHERE wt.doctype = 'WarehouseTask' AND wt.status = 'Exception' AND wt.data->>'location_code' = $1
		ORDER BY wt.updated_at ASC`, schema, schema), locationCode)
	if err != nil {
		return out
	}
	defer rows.Close()
	for rows.Next() {
		var e CockpitExceptionItem
		var ageMins float64
		if rows.Scan(&e.TaskID, &e.TaskType, &e.Item, &e.BinCode, &e.ReasonCode, &e.ProcessStep, &e.FollowOnAction, &ageMins) == nil {
			e.AgeMins = int(ageMins)
			out = append(out, e)
		}
	}
	return out
}

func cockpitWaveStatus(schema, locationCode string) []CockpitWaveSummary {
	out := []CockpitWaveSummary{}
	rows, err := db.DB.Query(fmt.Sprintf(`
		SELECT data->>'wave_id', status, COUNT(*)
		FROM %s.documents
		WHERE doctype = 'FulfillmentTask' AND COALESCE(data->>'wave_id', '') != '' AND data->>'location_code' = $1
		GROUP BY data->>'wave_id', status
		ORDER BY data->>'wave_id', status`, schema), locationCode)
	if err != nil {
		return out
	}
	defer rows.Close()
	for rows.Next() {
		var w CockpitWaveSummary
		if rows.Scan(&w.WaveID, &w.Status, &w.Count) == nil {
			out = append(out, w)
		}
	}
	return out
}

func cockpitInboundDueToday(schema, locationCode string) []CockpitInboundItem {
	out := []CockpitInboundItem{}
	rows, err := db.DB.Query(fmt.Sprintf(`
		SELECT id, COALESCE(data->>'vendor', ''), COALESCE(data->>'expected_date', ''), status
		FROM %s.documents
		WHERE doctype = 'ASN' AND data->>'location' = $1 AND status = 'Expected'
		  AND data->>'expected_date' = $2
		ORDER BY id`, schema), locationCode, today().Format(isoDate))
	if err != nil {
		return out
	}
	defer rows.Close()
	for rows.Next() {
		var a CockpitInboundItem
		if rows.Scan(&a.ASNNumber, &a.Vendor, &a.ExpectedDate, &a.Status) == nil {
			out = append(out, a)
		}
	}
	return out
}

func cockpitBinUtilization(schema, locationCode string) []CockpitBinUtilization {
	out := []CockpitBinUtilization{}
	rows, err := db.DB.Query(fmt.Sprintf(`
		SELECT b.data->>'bin_code', COALESCE(NULLIF(b.data->>'capacity', '')::numeric, 0),
		       COALESCE((SELECT SUM(bs.qty) FROM %s.bin_stock bs WHERE bs.bin_code = b.data->>'bin_code'), 0)
		FROM %s.documents b
		WHERE b.doctype = 'Bin' AND b.status = 'Active' AND b.data->>'location' = $1
		  AND COALESCE(NULLIF(b.data->>'capacity', '')::numeric, 0) > 0
		ORDER BY b.data->>'bin_code'`, schema, schema), locationCode)
	if err != nil {
		return out
	}
	defer rows.Close()
	for rows.Next() {
		var u CockpitBinUtilization
		var used int
		if rows.Scan(&u.BinCode, &u.MaxQty, &used) == nil {
			u.UsedQty = used
			if u.MaxQty > 0 {
				u.PctUsed = roundTo2(float64(used) / u.MaxQty * 100)
			}
			out = append(out, u)
		}
	}
	return out
}
