package engines

// Stage 73.4.2.2: backorders and preorders. An order taken for stock that is
// not in hand is held as ALLOCATION_FAILED ("Waiting for stock") with its
// promise date (73.4.2.1) telling the customer when. ReleaseBackorders retries
// those orders oldest first, so the earliest customer gets the stock that
// lands; with orders.backorder_auto_release on, a worker does it on its own.
//
// It only calls ReleaseOrderHold, the same release a person's Retry runs, so a
// backorder can never be reserved by a rule the manual path would refuse.

import (
	"context"
	"fmt"
	"log"
	"time"

	"custom_erp/db"
)

const SettingBackorderAutoRelease = "orders.backorder_auto_release"

func init() {
	RegisterSetting(SettingDefinition{Key: SettingBackorderAutoRelease, Module: "Orders", Label: "Release backorders when stock lands",
		Type: SettingTypeBool, Default: "false",
		Description: "On: orders waiting for stock are retried every 15 minutes, oldest first, and reserved once stock covers them."})
}

// ReleaseBackorders retries every order waiting for stock, oldest first, and
// returns how many are now reserved and how many still wait.
func ReleaseBackorders(tenantID, actor string) (released, waiting int, err error) {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return 0, 0, err
	}
	rows, err := db.DB.Query(fmt.Sprintf(`SELECT id FROM %s.documents
		WHERE doctype = 'SalesOrder' AND deleted_at IS NULL
		  AND data->>'order_status' = 'On Hold' AND data->>'hold_reason' = $1
		ORDER BY created_at, id LIMIT 500`, schema), HoldAllocationFailed)
	if err != nil {
		return 0, 0, err
	}
	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return 0, 0, err
		}
		ids = append(ids, id)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return 0, 0, err
	}
	for _, id := range ids {
		if err := ReleaseOrderHold(tenantID, id); err != nil {
			waiting++
			continue
		}
		if status, _, err := GetOrderStatus(tenantID, id); err == nil && status == "Reserved" {
			released++
			LogAuditEvent(tenantID, actor, "ORDER_BACKORDER_RELEASED", "SUCCESS", id)
			continue
		}
		waiting++
	}
	return released, waiting, nil
}

// StartBackorderReleaser runs ReleaseBackorders for every tenant that has the
// setting on and an order waiting for stock.
func StartBackorderReleaser(ctx context.Context, interval time.Duration) {
	ticker := time.NewTicker(interval)
	go func() {
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				if !IsSchedulerLeader() || db.DB == nil {
					continue
				}
				schemas, err := listTenantSchemas()
				if err != nil {
					continue
				}
				for _, schema := range schemas {
					if !GetSettingBoolForSchema(schema, SettingBackorderAutoRelease) {
						continue
					}
					var waiting bool
					if db.DB.QueryRow(fmt.Sprintf(`SELECT EXISTS(SELECT 1 FROM %s.documents WHERE doctype = 'SalesOrder' AND deleted_at IS NULL
						AND data->>'order_status' = 'On Hold' AND data->>'hold_reason' = $1)`, schema), HoldAllocationFailed).Scan(&waiting) != nil || !waiting {
						continue
					}
					tenantID, err := tenantIDForSchema(schema)
					if err != nil {
						continue
					}
					if _, _, err := ReleaseBackorders(tenantID, "system"); err != nil {
						log.Printf("[BACKORDER] %s: %v", schema, err)
					}
				}
			}
		}
	}()
}
