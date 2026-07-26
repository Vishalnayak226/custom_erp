package engines

import (
	"custom_erp/db"
	"fmt"
	"time"
)

// Stage 26.10 (Reports and BI Sprint).
//
// 26.10.1: the Stock Ledger report Stage 20.40 explicitly deferred pending
// StockLedgerEntry actually being wired (see engines/inventory.go's
// WriteStockLedgerEntry/PostInventoryLedgerWithVoucher, and the call sites
// in wms.go/wms_putaway_ext.go/wms_receiving.go/transfer_orders.go/
// pos_checkout.go). All params are optional filters; RunningBalance is only
// meaningful (and only populated) when both Sku and LocationCode narrow the
// result to a single item/warehouse card - it's a cumulative sum of Qty
// across the filtered rows in ascending date order, not the item's true
// all-time balance, unless Start is left empty so the sum starts from the
// very first entry ever written.
//
// 26.10.5: exception queues (stale approvals, failed syncs, negative-stock
// flags) as three catalog reports, reusing Stage 17.10's GetSLABreaches
// monitor pattern (query open/pending rows, flag ones past a threshold)
// rather than a new engine - the checklist's own instruction. Each is also
// what the 26.10.3 dashboard's exception-queue widget calls for its counts.

func init() {
	RegisterReport(ReportDefinition{
		ID: "stock-ledger", Label: "Stock Ledger", Category: "Inventory",
		Columns: []ReportColumn{
			{Key: "created_at", Label: "Date"}, {Key: "item_id", Label: "SKU"},
			{Key: "warehouse_id", Label: "Location"}, {Key: "qty", Label: "Qty Delta"},
			{Key: "running_balance", Label: "Running Balance"},
			{Key: "voucher_type", Label: "Voucher Type"}, {Key: "voucher_id", Label: "Voucher ID"},
			{Key: "from_location_id", Label: "From Location/Bin"}, {Key: "to_location_id", Label: "To Location/Bin"},
			{Key: "from_status", Label: "From Condition"}, {Key: "to_status", Label: "To Condition"},
			{Key: "user_id", Label: "User"},
		},
		Params: []ReportParam{
			{Key: "sku", Label: "SKU (optional)", Type: "text"},
			{Key: "location_code", Label: "Location (optional)", Type: "text"},
			{Key: "voucher_type", Label: "Voucher Type (optional)", Type: "text"},
			{Key: "start", Label: "From (optional)", Type: "date"},
			{Key: "end", Label: "To (optional)", Type: "date"},
		},
		Run: func(tenantID string, params map[string]string) ([]map[string]interface{}, error) {
			return GetStockLedgerReport(tenantID, params["sku"], params["location_code"], params["voucher_type"], params["start"], params["end"])
		},
	})

	RegisterReport(ReportDefinition{
		ID: "exception-stale-approvals", Label: "Stale Approvals", Category: "Exceptions",
		Columns: []ReportColumn{
			{Key: "doctype", Label: "Doctype"}, {Key: "id", Label: "Document"},
			{Key: "submitted_at", Label: "Submitted"}, {Key: "hours_pending", Label: "Hours Pending"},
		},
		Params: []ReportParam{{Key: "threshold_hours", Label: "Threshold Hours (default 24)", Type: "text"}},
		Run: func(tenantID string, params map[string]string) ([]map[string]interface{}, error) {
			threshold := 24.0
			if v := numFromInterface(params["threshold_hours"]); v > 0 {
				threshold = v
			}
			rows, err := GetStaleApprovals(tenantID, threshold)
			if err != nil {
				return nil, err
			}
			return structsToRows(rows)
		},
	})

	RegisterReport(ReportDefinition{
		ID: "exception-failed-syncs", Label: "Failed Syncs", Category: "Exceptions",
		Columns: []ReportColumn{
			{Key: "id", Label: "Event ID"}, {Key: "event_name", Label: "Event"},
			{Key: "attempts", Label: "Attempts"}, {Key: "created_at", Label: "Queued"},
		},
		Run: func(tenantID string, params map[string]string) ([]map[string]interface{}, error) {
			rows, err := GetFailedSyncs(tenantID)
			if err != nil {
				return nil, err
			}
			return structsToRows(rows)
		},
	})

	RegisterReport(ReportDefinition{
		ID: "exception-negative-stock", Label: "Negative Stock Flags", Category: "Exceptions",
		Columns: []ReportColumn{
			{Key: "id", Label: "Flag ID"}, {Key: "sku", Label: "SKU"}, {Key: "location", Label: "Location"},
			{Key: "cart_number", Label: "Cart"}, {Key: "shortfall_qty", Label: "Shortfall"},
			{Key: "resulting_available", Label: "Resulting Available"}, {Key: "created_at", Label: "Flagged"},
		},
		Run: func(tenantID string, params map[string]string) ([]map[string]interface{}, error) {
			rows, err := GetNegativeStockFlags(tenantID)
			if err != nil {
				return nil, err
			}
			return structsToRows(rows)
		},
	})
}

// GetStockLedgerReport returns StockLedgerEntry rows matching the given
// optional filters, oldest first, with a cumulative RunningBalance computed
// across exactly the rows returned (see this file's package doc comment for
// why that's only a true all-time balance when both sku/locationCode are set
// and start is empty).
type stockLedgerRow struct {
	CreatedAt      time.Time `json:"created_at"`
	ItemID         string    `json:"item_id"`
	WarehouseID    string    `json:"warehouse_id"`
	Qty            float64   `json:"qty"`
	RunningBalance float64   `json:"running_balance"`
	VoucherType    string    `json:"voucher_type"`
	VoucherID      string    `json:"voucher_id"`
	FromLocationID string    `json:"from_location_id"`
	ToLocationID   string    `json:"to_location_id"`
	FromStatus     string    `json:"from_status"`
	ToStatus       string    `json:"to_status"`
	UserID         string    `json:"user_id"`
}

func GetStockLedgerReport(tenantID, sku, locationCode, voucherType, start, end string) ([]map[string]interface{}, error) {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return nil, err
	}

	rows, err := db.DB.Query(fmt.Sprintf(`
		SELECT created_at,
		       COALESCE(data->>'item_id', ''), COALESCE(data->>'warehouse_id', ''),
		       COALESCE((data->>'qty')::numeric, 0),
		       COALESCE(data->>'voucher_type', ''), COALESCE(data->>'voucher_id', ''),
		       COALESCE(data->>'from_location_id', ''), COALESCE(data->>'to_location_id', ''),
		       COALESCE(data->>'from_status', ''), COALESCE(data->>'to_status', ''),
		       COALESCE(data->>'user_id', '')
		FROM %s.documents
		WHERE doctype = 'StockLedgerEntry'
		  AND ($1 = '' OR data->>'item_id' = $1)
		  AND ($2 = '' OR data->>'warehouse_id' = $2)
		  AND ($3 = '' OR data->>'voucher_type' = $3)
		  AND ($4 = '' OR created_at >= $4::date)
		  AND ($5 = '' OR created_at < ($5::date + interval '1 day'))
		ORDER BY created_at ASC`, schema),
		sku, locationCode, voucherType, start, end)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	trackBalance := sku != "" && locationCode != ""
	var runningBalance float64
	out := []map[string]interface{}{}
	for rows.Next() {
		var r stockLedgerRow
		if err := rows.Scan(&r.CreatedAt, &r.ItemID, &r.WarehouseID, &r.Qty,
			&r.VoucherType, &r.VoucherID, &r.FromLocationID, &r.ToLocationID,
			&r.FromStatus, &r.ToStatus, &r.UserID); err != nil {
			continue
		}
		if trackBalance {
			runningBalance += r.Qty
			r.RunningBalance = runningBalance
		}
		out = append(out, map[string]interface{}{
			"created_at": r.CreatedAt, "item_id": r.ItemID, "warehouse_id": r.WarehouseID,
			"qty": r.Qty, "running_balance": r.RunningBalance,
			"voucher_type": r.VoucherType, "voucher_id": r.VoucherID,
			"from_location_id": r.FromLocationID, "to_location_id": r.ToLocationID,
			"from_status": r.FromStatus, "to_status": r.ToStatus, "user_id": r.UserID,
		})
	}
	return out, nil
}

// StaleApproval is one document sitting in "Pending Approval" past the
// threshold - a cross-doctype query (documents.status, not any one
// doctype's own field) since any approval-gated doctype can go stale.
type StaleApproval struct {
	Doctype      string    `json:"doctype"`
	ID           string    `json:"id"`
	SubmittedAt  time.Time `json:"submitted_at"`
	HoursPending float64   `json:"hours_pending"`
}

func GetStaleApprovals(tenantID string, thresholdHours float64) ([]StaleApproval, error) {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return nil, err
	}
	rows, err := db.DB.Query(fmt.Sprintf(
		`SELECT doctype, id, updated_at FROM %s.documents WHERE status = 'Pending Approval'`, schema))
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []StaleApproval{}
	for rows.Next() {
		var doctype, id string
		var updatedAt time.Time
		if err := rows.Scan(&doctype, &id, &updatedAt); err != nil {
			continue
		}
		hours := time.Since(updatedAt).Hours()
		if hours >= thresholdHours {
			out = append(out, StaleApproval{Doctype: doctype, ID: id, SubmittedAt: updatedAt, HoursPending: roundTo2(hours)})
		}
	}
	return out, nil
}

// FailedSync is one integration_event_outbox row that has exhausted or is
// still exhausting its retry budget (engines/outbox.go's processOutbox caps
// attempts at 5) - the "failed syncs" exception queue.
type FailedSync struct {
	ID        string    `json:"id"`
	EventName string    `json:"event_name"`
	Attempts  int       `json:"attempts"`
	CreatedAt time.Time `json:"created_at"`
}

func GetFailedSyncs(tenantID string) ([]FailedSync, error) {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return nil, err
	}
	rows, err := db.DB.Query(fmt.Sprintf(
		`SELECT id, event_name, attempts, created_at FROM %s.integration_event_outbox WHERE status = 'Failed' ORDER BY created_at DESC`, schema))
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []FailedSync{}
	for rows.Next() {
		var f FailedSync
		if err := rows.Scan(&f.ID, &f.EventName, &f.Attempts, &f.CreatedAt); err != nil {
			continue
		}
		out = append(out, f)
	}
	return out, nil
}

// NegativeStockFlag wraps the existing POSOfflineSyncVariance doctype
// (pos_checkout.go's recordOfflineSyncVariance, Stage 20.13) as the
// "negative-stock flags" exception queue - Open ones are still unreviewed.
type NegativeStockFlag struct {
	ID                 string    `json:"id"`
	Sku                string    `json:"sku"`
	Location           string    `json:"location"`
	CartNumber         string    `json:"cart_number"`
	ShortfallQty       float64   `json:"shortfall_qty"`
	ResultingAvailable float64   `json:"resulting_available"`
	CreatedAt          time.Time `json:"created_at"`
}

func GetNegativeStockFlags(tenantID string) ([]NegativeStockFlag, error) {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return nil, err
	}
	rows, err := db.DB.Query(fmt.Sprintf(`
		SELECT id, COALESCE(data->>'sku', ''), COALESCE(data->>'location', ''),
		       COALESCE(data->>'cart_number', ''),
		       COALESCE((data->>'shortfall_qty')::numeric, 0), COALESCE((data->>'resulting_available')::numeric, 0),
		       created_at
		FROM %s.documents WHERE doctype = 'POSOfflineSyncVariance' AND status = 'Open' ORDER BY created_at DESC`, schema))
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []NegativeStockFlag{}
	for rows.Next() {
		var f NegativeStockFlag
		if err := rows.Scan(&f.ID, &f.Sku, &f.Location, &f.CartNumber, &f.ShortfallQty, &f.ResultingAvailable, &f.CreatedAt); err != nil {
			continue
		}
		out = append(out, f)
	}
	return out, nil
}

func roundTo2(v float64) float64 {
	return float64(int(v*100)) / 100
}
