package engines

import (
	"context"
	"custom_erp/db"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"time"
)

// PO auto-cancel (the user, 2026-10-17: "a days config for PO auto cancel" -
// counted from approval; a partly received PO has the rest short-closed).
//
// procurement.po_auto_cancel_days = N > 0: an Approved PO whose approval is N
// days old is cancelled when nothing has been received against it, and its
// open balance short-closed when something has - a PO with a receipt is never
// cancelled or deleted (Stage 65.2). The approval is the PO's latest
// approval_log "Approved" row; a PO approved without an approval rule has none
// and counts from its creation. A fully received PO is left as it is.

// poAutoCancelReason is the short-close reason the sweep records.
const poAutoCancelReason = "AUTO-EXPIRY"

// POAutoCancelResult is what one run did for a tenant.
type POAutoCancelResult struct {
	Cancelled   []string `json:"cancelled"`
	ShortClosed []string `json:"short_closed"`
	Errors      []string `json:"errors,omitempty"`
}

// RunPOAutoCancel applies procurement.po_auto_cancel_days to one tenant.
func RunPOAutoCancel(tenantID string) (*POAutoCancelResult, error) {
	out := &POAutoCancelResult{}
	days := GetSettingInt(tenantID, "procurement.po_auto_cancel_days")
	if days <= 0 {
		return out, nil
	}
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return nil, err
	}
	rows, err := db.DB.Query(fmt.Sprintf(`SELECT d.id, COALESCE(d.data->>'location', '') FROM %[1]s.documents d
		WHERE d.doctype = 'PurchaseOrder' AND d.status = 'Approved' AND d.deleted_at IS NULL
		AND COALESCE((SELECT MAX(l.created_at) FROM %[1]s.approval_log l
			WHERE l.doctype = 'PurchaseOrder' AND l.document_id = d.id AND l.action = 'Approved'), d.created_at)
			<= LOCALTIMESTAMP - make_interval(days => $1)
		ORDER BY d.created_at`, schema), days)
	if err != nil {
		return nil, err
	}
	type duePO struct{ id, location string }
	var due []duePO
	for rows.Next() {
		var p duePO
		if err := rows.Scan(&p.id, &p.location); err != nil {
			rows.Close()
			return nil, err
		}
		due = append(due, p)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}

	for _, p := range due {
		receipts, err := PurchaseOrderReceipts(tenantID, p.id)
		if err != nil {
			out.Errors = append(out.Errors, fmt.Sprintf("%s: %v", p.id, err))
			continue
		}
		if len(receipts) == 0 {
			ok, err := autoCancelPurchaseOrder(schema, p.id, days)
			if err != nil {
				out.Errors = append(out.Errors, fmt.Sprintf("%s: %v", p.id, err))
			} else if ok {
				out.Cancelled = append(out.Cancelled, p.id)
				LogAuditEvent(tenantID, "system", "PURCHASE_ORDER_AUTO_CANCELLED", "SUCCESS", fmt.Sprintf(
					"PO %s cancelled: nothing received within %d days of approval", p.id, days))
			}
			continue
		}
		if _, err := shortClosePurchaseOrder(tenantID, p.id, "system", poAutoCancelReason,
			fmt.Sprintf("auto-closed %d days after approval", days), p.location); err != nil {
			var v *ValidationError
			if errors.As(err, &v) && v.Code == "PURCHA-0488" {
				continue // fully received, or no longer Approved
			}
			out.Errors = append(out.Errors, fmt.Sprintf("%s: %v", p.id, err))
			continue
		}
		out.ShortClosed = append(out.ShortClosed, p.id)
	}
	return out, nil
}

// autoCancelPurchaseOrder cancels an Approved PO in one statement that also
// re-checks no receipt exists, so a GRN saved since the check keeps it open.
func autoCancelPurchaseOrder(schema, poID string, days int) (bool, error) {
	patch, _ := json.Marshal(map[string]interface{}{
		"status": "Cancelled", "auto_cancelled": true, "cancelled_by": "system",
		"cancelled_at":  time.Now().UTC().Format(time.RFC3339),
		"cancel_reason": fmt.Sprintf("Auto-cancelled: nothing received within %d days of approval", days),
	})
	res, err := db.DB.Exec(fmt.Sprintf(`UPDATE %[1]s.documents SET data = data || $1::jsonb, status = 'Cancelled', updated_at = CURRENT_TIMESTAMP
		WHERE doctype = 'PurchaseOrder' AND id = $2 AND status = 'Approved'
		AND NOT EXISTS (SELECT 1 FROM %[1]s.documents g WHERE g.doctype = 'GRN' AND g.status != 'Cancelled' AND g.deleted_at IS NULL
			AND (g.data->>'po_id' = $2 OR (',' || COALESCE(g.data->>'po_ids', '') || ',') LIKE '%%,' || $2 || ',%%'))`, schema), patch, poID)
	if err != nil {
		return false, err
	}
	n, _ := res.RowsAffected()
	return n == 1, nil
}

// StartPOAutoCancelWorker runs RunPOAutoCancel for every tenant each tick -
// the StartDunningWorker shape, so a new tenant is picked up without a restart.
func StartPOAutoCancelWorker(ctx context.Context, interval time.Duration) {
	ticker := time.NewTicker(interval)
	go func() {
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				if !IsSchedulerLeader() { // 134.7: one server runs each sweep
					continue
				}
				if db.DB == nil {
					continue
				}
				schemas, err := listTenantSchemas()
				if err != nil {
					log.Printf("[PO-AUTO-CANCEL] Failed to list tenant schemas: %v", err)
					continue
				}
				for _, schema := range schemas {
					tenantID, err := tenantIDForSchema(schema)
					if err != nil {
						continue
					}
					res, err := RunPOAutoCancel(tenantID)
					if err != nil {
						log.Printf("[PO-AUTO-CANCEL] %s: %v", schema, err)
						continue
					}
					if len(res.Cancelled)+len(res.ShortClosed) > 0 {
						log.Printf("[PO-AUTO-CANCEL] %s: cancelled %v, short-closed %v", schema, res.Cancelled, res.ShortClosed)
					}
					for _, e := range res.Errors {
						log.Printf("[PO-AUTO-CANCEL] %s: %s", schema, e)
					}
				}
			}
		}
	}()
}
