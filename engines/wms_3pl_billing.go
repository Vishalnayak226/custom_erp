package engines

import (
	"custom_erp/db"
	"fmt"
	"time"
)

// Stage 26.5.15 (WMS Enterprise Maturity Sprint P2 follow-up): 3PL
// multi-owner billing. Go-ahead given 2026-07-27 for all five P2 bundles
// previously deferred pending a real warehouse-scale pilot.
//
// A read-only billing report, same "compute a suggestion/figure, never
// auto-create an invoice" precedent as everything else added this pass
// (an invoice/GL integration is a real future step once someone actually
// wants to bill against this, not built speculatively here). Storage
// charge is a documented approximation, the same kind 26.7.3's loyalty-
// expiry sweep already accepts: it uses the CURRENT bin_stock snapshot for
// each owner's bins x the period's day-count, not a true day-by-day
// historical average (this codebase has no historical bin_stock ledger to
// average over - the Stock Ledger report is a delta log, not a balance
// history). Handling charge counts TaskCompletionLog (26.5.13) rows at the
// rate's location in the period - deliberately location-wide, not split
// per owner, since only Putaway's reference_id is a bin code; Pack/
// CycleCount completions aren't attributable to one owner's bins without
// a much larger restructure.
type StorageBillingReportRow struct {
	OwnerID        string  `json:"owner_id"`
	LocationCode   string  `json:"location_code"`
	CurrentUnits   int     `json:"current_units"`
	Days           int     `json:"days"`
	StorageCharge  float64 `json:"storage_charge"`
	HandlingTasks  int     `json:"handling_tasks"`
	HandlingCharge float64 `json:"handling_charge"`
	TotalCharge    float64 `json:"total_charge"`
}

func GetStorageBillingReport(tenantID, ownerID, start, end string) ([]StorageBillingReportRow, error) {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return nil, err
	}
	if start == "" || end == "" {
		return nil, &ValidationError{Code: "GLOBAL-0002", Message: "start and end dates are both required for a billing period"}
	}
	startT, err1 := time.Parse("2006-01-02", start)
	endT, err2 := time.Parse("2006-01-02", end)
	if err1 != nil || err2 != nil || !endT.After(startT) {
		return nil, &ValidationError{Code: "GLOBAL-0002", Message: "end date must be after start date"}
	}
	days := int(endT.Sub(startT).Hours()/24) + 1

	rateRows, err := db.DB.Query(fmt.Sprintf(`
		SELECT id, COALESCE(data->>'owner_id', ''), COALESCE(data->>'location_code', ''),
		       COALESCE((data->>'storage_rate_per_unit_per_day')::numeric, 0),
		       COALESCE((data->>'handling_rate_per_task')::numeric, 0)
		FROM %s.documents WHERE doctype = 'StorageBillingRate' AND status = 'Active'
		  AND ($1 = '' OR data->>'owner_id' = $1)`, schema), ownerID)
	if err != nil {
		return nil, err
	}
	type rate struct {
		OwnerID, LocationCode          string
		StorageRate, HandlingRate      float64
	}
	var rates []rate
	for rateRows.Next() {
		var id string
		var r rate
		if err := rateRows.Scan(&id, &r.OwnerID, &r.LocationCode, &r.StorageRate, &r.HandlingRate); err != nil {
			continue
		}
		rates = append(rates, r)
	}
	rateRows.Close()

	out := []StorageBillingReportRow{}
	for _, r := range rates {
		var currentUnits int
		if err := db.DB.QueryRow(fmt.Sprintf(`
			SELECT COALESCE(SUM(bs.qty), 0)
			FROM %s.bin_stock bs
			JOIN %s.documents b ON b.doctype = 'Bin' AND b.data->>'bin_code' = bs.bin_code
			WHERE b.data->>'owner_id' = $1 AND bs.location_code = $2 AND bs.condition = 'Good'`, schema, schema),
			r.OwnerID, r.LocationCode).Scan(&currentUnits); err != nil {
			currentUnits = 0
		}

		var handlingTasks int
		if err := db.DB.QueryRow(fmt.Sprintf(`
			SELECT COUNT(*) FROM %s.documents
			WHERE doctype = 'TaskCompletionLog' AND data->>'location_code' = $1
			  AND created_at >= $2::date AND created_at < ($3::date + interval '1 day')`, schema),
			r.LocationCode, start, end).Scan(&handlingTasks); err != nil {
			handlingTasks = 0
		}

		storageCharge := roundTo2(float64(currentUnits) * r.StorageRate * float64(days))
		handlingCharge := roundTo2(float64(handlingTasks) * r.HandlingRate)
		out = append(out, StorageBillingReportRow{
			OwnerID: r.OwnerID, LocationCode: r.LocationCode, CurrentUnits: currentUnits, Days: days,
			StorageCharge: storageCharge, HandlingTasks: handlingTasks, HandlingCharge: handlingCharge,
			TotalCharge: roundTo2(storageCharge + handlingCharge),
		})
	}
	return out, nil
}

func init() {
	RegisterReport(ReportDefinition{
		ID: "3pl-storage-billing", Label: "3PL Storage & Handling Billing", Category: "WMS",
		Columns: []ReportColumn{
			{Key: "owner_id", Label: "Owner"}, {Key: "location_code", Label: "Location"},
			{Key: "current_units", Label: "Current Units"}, {Key: "days", Label: "Days"},
			{Key: "storage_charge", Label: "Storage Charge", Sensitive: true},
			{Key: "handling_tasks", Label: "Handling Tasks"}, {Key: "handling_charge", Label: "Handling Charge", Sensitive: true},
			{Key: "total_charge", Label: "Total", Sensitive: true},
		},
		Params: []ReportParam{
			{Key: "owner_id", Label: "Owner (optional)", Type: "text"},
			{Key: "start", Label: "Period Start", Type: "date", Required: true},
			{Key: "end", Label: "Period End", Type: "date", Required: true},
		},
		Run: func(tenantID string, params map[string]string) ([]map[string]interface{}, error) {
			rows, err := GetStorageBillingReport(tenantID, params["owner_id"], params["start"], params["end"])
			if err != nil {
				return nil, err
			}
			return structsToRows(rows)
		},
	})
}
