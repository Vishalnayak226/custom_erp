package engines

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"sort"
	"strings"
	"time"

	"custom_erp/db"
)

// Stage 148.41 - Reserve a piece for a customer.
//
// A named hold on a serial piece, with no order behind it: for a customer, with
// an optional advance and an optional expiry day. The till and picking skip a
// held piece for anyone else; the named customer's sale (or the pick of their
// order) uses it and the sale clears the hold. Expiry frees the piece by a job.
// An advance is taken through the existing till advance path and stays on the
// customer's account: expiry and release never refund it silently.
//
// The hold lives in its own fields (hold_*), not in reserved_for: reserved_for
// already means the voucher a piece was allocated to (TransitionSerialStatus
// checks it at pack), and a hold must not clobber that or be clobbered by it.
// A piece a voucher has reserved cannot be held, and a held piece is only
// allocated to an order of the customer it is held for.
//
// Off by default (inventory.piece_holds): nothing changes until someone places a
// hold, and with no hold on a piece every check below passes it through.

const SettingPieceHolds = "inventory.piece_holds"

func init() {
	RegisterSetting(SettingDefinition{Key: SettingPieceHolds, Module: "Inventory", Label: "Hold a piece for a customer",
		Type: SettingTypeBool, Default: "false",
		Description: "On: a serial piece can be held for a named customer, with an optional advance and expiry day. The till and picking skip a held piece for anyone else. Off: no holds can be placed."})
	RegisterReport(ReportDefinition{ID: "held-pieces", Label: "Held pieces", Category: "Inventory",
		Columns: []ReportColumn{{Key: "sku", Label: "Item"}, {Key: "serial_no", Label: "Piece"}, {Key: "customer_name", Label: "Customer"},
			{Key: "until", Label: "Held until"}, {Key: "advance", Label: "Advance"}, {Key: "store", Label: "Store"}, {Key: "held_by", Label: "Held by"}, {Key: "note", Label: "Note"}},
		Run: func(tenantID string, _ map[string]string) ([]map[string]interface{}, error) {
			views, err := ListPieceHolds(tenantID)
			if err != nil {
				return nil, err
			}
			out := make([]map[string]interface{}, 0, len(views))
			for _, v := range views {
				out = append(out, map[string]interface{}{"sku": v.SKU, "serial_no": v.SerialNo, "customer_name": v.CustomerName, "until": v.Until,
					"advance": v.Advance, "store": v.Location, "held_by": v.HeldBy, "note": v.Note})
			}
			return out, nil
		}, StoreKey: "store"})
}

// PieceHoldRequest places one hold.
type PieceHoldRequest struct {
	SKU           string  `json:"sku"`
	SerialNo      string  `json:"serial_no"`
	CustomerID    string  `json:"customer_id"`
	Until         string  `json:"until"`
	AdvanceRupees float64 `json:"advance"`
	PaymentMode   string  `json:"payment_mode"`
	Location      string  `json:"location"`
	Note          string  `json:"note"`
}

// PieceHoldView is a hold as the screen shows it.
type PieceHoldView struct {
	SKU          string `json:"sku"`
	SerialNo     string `json:"serial_no"`
	CustomerID   string `json:"customer_id"`
	CustomerName string `json:"customer_name"`
	Until        string `json:"until"`
	Advance      string `json:"advance"`
	Location     string `json:"location"`
	HeldBy       string `json:"held_by"`
	Note         string `json:"note"`
}

func holdRefused(code, format string, args ...interface{}) error {
	return &ValidationError{Code: code, Message: fmt.Sprintf(format, args...)}
}

// holdActive reports a hold that stops others: named, and not past its day.
func holdActive(piece SerialInfo) bool {
	return piece.HoldCustomer != "" && (piece.HoldUntil == "" || piece.HoldUntil >= BusinessToday())
}

// pieceHoldRefusal is the one check the till, picking and the allocation route
// ask. customerID is who the piece is being sold or picked for ("" for a
// walk-in or a non-customer voucher).
func pieceHoldRefusal(piece SerialInfo, customerID string) error {
	if !holdActive(piece) || piece.HoldCustomer == customerID {
		return nil
	}
	return holdRefused("INVENT-1326", "Piece %s of %s is held for another customer.", piece.SerialNo, piece.Item)
}

// pieceHoldClear is the patch that ends a hold; a sale applies it to the piece.
func pieceHoldClear() map[string]interface{} {
	return map[string]interface{}{"hold_customer": "", "hold_until": "", "hold_advance": "", "hold_note": "", "hold_by": "", "hold_at": ""}
}

// orderCustomerID is the customer a sales order belongs to; "" when it has none.
func orderCustomerID(tenantID, orderID string) string {
	if strings.TrimSpace(orderID) == "" {
		return ""
	}
	if _, data, err := fetchSalesOrder(tenantID, orderID); err == nil {
		return strField(data, "customer_id")
	}
	return ""
}

// PlacePieceHold holds an in-stock piece for a customer.
func PlacePieceHold(tenantID, user string, req PieceHoldRequest) (*PieceHoldView, error) {
	if !GetSettingBool(tenantID, SettingPieceHolds) {
		return nil, holdRefused("INVENT-1325", "Holding a piece for a customer is switched off.")
	}
	req.SKU, req.SerialNo, req.CustomerID = strings.TrimSpace(req.SKU), strings.TrimSpace(req.SerialNo), strings.TrimSpace(req.CustomerID)
	req.Until = strings.TrimSpace(req.Until)
	customer, found, err := LoadCustomerRef(tenantID, req.CustomerID)
	if err != nil {
		return nil, err
	}
	if req.CustomerID == "" || !found {
		return nil, holdRefused("INVENT-1327", "Pick the customer the piece is held for.")
	}
	if req.Until != "" {
		day, perr := time.Parse("2006-01-02", req.Until)
		if perr != nil || day.Format("2006-01-02") < BusinessToday() {
			return nil, holdRefused("INVENT-1329", "The hold must end today or later.")
		}
	}
	piece, err := GetSerial(tenantID, req.SKU, req.SerialNo)
	if err != nil {
		return nil, err
	}
	if piece == nil || piece.Status != SerialInStock || piece.ReservedFor != "" {
		return nil, holdRefused("INVENT-1327", "Only a piece in stock that no order or transfer has taken can be held.")
	}
	if holdActive(*piece) {
		return nil, holdRefused("INVENT-1328", "This piece is already held.")
	}
	advance := ""
	if req.AdvanceRupees > 0 {
		location := firstNonEmpty(req.Location, piece.LocationCode)
		adv, aerr := TakeCustomerAdvance(tenantID, user, "", TakeAdvanceRequest{Location: location, CustomerID: customer.ID, Amount: req.AdvanceRupees,
			PaymentMode: req.PaymentMode, Purpose: "Hold " + req.SerialNo, ExpectedDate: req.Until})
		if aerr != nil {
			return nil, aerr
		}
		advance = adv.Number
	}
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return nil, err
	}
	patch, _ := json.Marshal(map[string]interface{}{"hold_customer": customer.ID, "hold_until": req.Until, "hold_advance": advance,
		"hold_note": strings.TrimSpace(req.Note), "hold_by": user, "hold_at": time.Now().UTC().Format(time.RFC3339)})
	res, err := db.DB.Exec(fmt.Sprintf(`UPDATE %s.documents SET data = data || $1::jsonb, updated_at = CURRENT_TIMESTAMP
		WHERE doctype = 'SerialNumber' AND id = $2 AND status = 'InStock' AND COALESCE(data->>'reserved_for', '') = '' AND COALESCE(data->>'hold_customer', '') = ''`, schema),
		patch, piece.DocID)
	if err != nil {
		return nil, err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return nil, holdRefused("INVENT-1328", "This piece was taken a moment ago.")
	}
	LogAuditEvent(tenantID, user, "PIECE_HOLD", "SUCCESS", fmt.Sprintf("Piece %s of %s held for %s until %s%s", req.SerialNo, req.SKU, customer.ID,
		firstNonEmpty(req.Until, "released"), map[bool]string{true: "; advance " + advance, false: ""}[advance != ""]))
	return &PieceHoldView{SKU: req.SKU, SerialNo: req.SerialNo, CustomerID: customer.ID, CustomerName: customer.Name, Until: req.Until,
		Advance: advance, Location: piece.LocationCode, HeldBy: user, Note: strings.TrimSpace(req.Note)}, nil
}

// ReleasePieceHold ends a hold by hand. An advance stays on the customer's
// account; refunding it is the advance screen's decision.
func ReleasePieceHold(tenantID, user, sku, serialNo, reason string) error {
	piece, err := GetSerial(tenantID, strings.TrimSpace(sku), strings.TrimSpace(serialNo))
	if err != nil {
		return err
	}
	if piece == nil || piece.HoldCustomer == "" {
		return holdRefused("INVENT-1330", "This piece is not held.")
	}
	if err := clearPieceHold(tenantID, piece.DocID); err != nil {
		return err
	}
	LogAuditEvent(tenantID, user, "PIECE_HOLD_RELEASED", "SUCCESS", fmt.Sprintf("Piece %s of %s released from %s%s%s", serialNo, sku, piece.HoldCustomer,
		map[bool]string{true: "; advance " + piece.HoldAdvance + " stays on the account", false: ""}[piece.HoldAdvance != ""], reasonSuffix(reason)))
	return nil
}

func clearPieceHold(tenantID, docID string) error {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return err
	}
	patch, _ := json.Marshal(pieceHoldClear())
	_, err = db.DB.Exec(fmt.Sprintf(`UPDATE %s.documents SET data = data || $1::jsonb, updated_at = CURRENT_TIMESTAMP
		WHERE doctype = 'SerialNumber' AND id = $2`, schema), patch, docID)
	return err
}

// ListPieceHolds is every piece currently held, soonest expiry first.
func ListPieceHolds(tenantID string) ([]PieceHoldView, error) {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return nil, err
	}
	rows, err := db.DB.Query(fmt.Sprintf(`SELECT s.data->>'item', s.data->>'serial_no', s.data->>'hold_customer', COALESCE(c.data->>'name', s.data->>'hold_customer'),
		COALESCE(s.data->>'hold_until', ''), COALESCE(s.data->>'hold_advance', ''), COALESCE(s.data->>'location_code', ''),
		COALESCE(s.data->>'hold_by', ''), COALESCE(s.data->>'hold_note', '')
		FROM %[1]s.documents s LEFT JOIN %[1]s.documents c ON c.doctype = 'Customer' AND c.id = s.data->>'hold_customer'
		WHERE s.doctype = 'SerialNumber' AND s.deleted_at IS NULL AND COALESCE(s.data->>'hold_customer', '') <> ''`, schema))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []PieceHoldView{}
	for rows.Next() {
		var v PieceHoldView
		if err := rows.Scan(&v.SKU, &v.SerialNo, &v.CustomerID, &v.CustomerName, &v.Until, &v.Advance, &v.Location, &v.HeldBy, &v.Note); err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	sort.SliceStable(out, func(a, b int) bool {
		if (out[a].Until == "") != (out[b].Until == "") {
			return out[a].Until != ""
		}
		return out[a].Until < out[b].Until
	})
	return out, rows.Err()
}

// ExpirePieceHolds frees every piece whose hold day has passed and says in the
// audit trail which advance was left on the customer's account.
func ExpirePieceHolds(tenantID, user string) (int, error) {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return 0, err
	}
	rows, err := db.DB.Query(fmt.Sprintf(`SELECT id, data->>'item', data->>'serial_no', data->>'hold_customer', COALESCE(data->>'hold_advance', '')
		FROM %s.documents WHERE doctype = 'SerialNumber' AND deleted_at IS NULL AND COALESCE(data->>'hold_customer', '') <> ''
		  AND COALESCE(data->>'hold_until', '') <> '' AND data->>'hold_until' < $1`, schema), BusinessToday())
	if err != nil {
		return 0, err
	}
	type expired struct{ id, sku, serial, customer, advance string }
	var due []expired
	for rows.Next() {
		var e expired
		if err := rows.Scan(&e.id, &e.sku, &e.serial, &e.customer, &e.advance); err != nil {
			rows.Close()
			return 0, err
		}
		due = append(due, e)
	}
	rows.Close()
	done := 0
	for _, e := range due {
		if err := clearPieceHold(tenantID, e.id); err != nil {
			return done, err
		}
		LogAuditEvent(tenantID, user, "PIECE_HOLD_EXPIRED", "SUCCESS", fmt.Sprintf("Hold on piece %s of %s for %s ended%s", e.serial, e.sku, e.customer,
			map[bool]string{true: "; advance " + e.advance + " stays on the account", false: ""}[e.advance != ""]))
		done++
	}
	return done, nil
}

// StartPieceHoldWorker frees expired holds for every client that has any.
func StartPieceHoldWorker(ctx context.Context, interval time.Duration) {
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
				schemas, err := sweepTenantSchemas()
				if err != nil {
					continue
				}
				for _, schema := range schemas {
					var held bool
					if db.DB.QueryRow(fmt.Sprintf(`SELECT EXISTS(SELECT 1 FROM %s.documents WHERE doctype = 'SerialNumber' AND deleted_at IS NULL
						AND COALESCE(data->>'hold_until', '') <> '' AND COALESCE(data->>'hold_customer', '') <> '')`, schema)).Scan(&held) != nil || !held {
						continue
					}
					tenantID, err := tenantIDForSchema(schema)
					if err != nil {
						continue
					}
					if _, err := ExpirePieceHolds(tenantID, "system"); err != nil && err != sql.ErrNoRows {
						log.Printf("[PIECE-HOLD] %s: %v", schema, err)
					}
				}
			}
		}
	}()
}
