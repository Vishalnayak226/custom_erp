package engines

import (
	"custom_erp/db"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
)

// Stage 73.2.5 - hold and resume a bill.
//
// A cashier parks the cart in front of them (the customer went to fetch
// something, a price check is pending) and serves the next one. The parked
// bill keeps its cart number: that number is the sale's identity - the
// checkout claims it before anything posts, and the receipt carries it - so
// a resumed bill checks out under it, and the same bill can never post
// twice however many times it is held and resumed. Resuming is a single
// conditional update (Held -> Resumed), so two tills cannot both take it.
// Holding reserves no stock; prices are quoted again when it is resumed.

// HeldBillLine is one parked cart line. UnitPrice is kept only for a line the
// tenant has not priced (assisted mode), exactly as the till sends it.
type HeldBillLine struct {
	SKU       string  `json:"sku"`
	Name      string  `json:"name,omitempty"`
	Qty       int     `json:"qty"`
	UnitPrice float64 `json:"unit_price,omitempty"`
}

// HeldBill is a parked cart.
type HeldBill struct {
	ID          string         `json:"id"`
	CartNumber  string         `json:"cart_number"`
	Location    string         `json:"location"`
	Cashier     string         `json:"cashier"`
	CustomerID  string         `json:"customer_id,omitempty"`
	CouponCodes []string       `json:"coupon_codes,omitempty"`
	Lines       []HeldBillLine `json:"lines"`
	LineCount   int            `json:"line_count"`
	Total       float64        `json:"total"`
	Note        string         `json:"note,omitempty"`
	Status      string         `json:"status"`
	HeldAt      string         `json:"held_at,omitempty"`
}

func heldBillID(cartNumber string) string { return "HOLD-" + cartNumber }

// HoldPOSBill parks a cart. A bill that is already being checked out or has
// posted cannot be held; a bill that was held and resumed can be held again.
func HoldPOSBill(tenantID, cashier, userID string, bill HeldBill) (*HeldBill, error) {
	bill.CartNumber = strings.TrimSpace(bill.CartNumber)
	bill.Location = strings.TrimSpace(bill.Location)
	if bill.CartNumber == "" || bill.Location == "" {
		return nil, &ValidationError{Code: "POSOFF-0609", Message: "a bill to hold needs its cart number and store"}
	}
	var lines []HeldBillLine
	for _, l := range bill.Lines {
		if strings.TrimSpace(l.SKU) != "" && l.Qty > 0 {
			lines = append(lines, l)
		}
	}
	if len(lines) == 0 {
		return nil, &ValidationError{Code: "POSOFF-0609", Message: "there is nothing on the bill to hold"}
	}
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return nil, err
	}
	var cartStatus string
	if err := db.DB.QueryRow(fmt.Sprintf(`SELECT status FROM %s.documents WHERE doctype = 'POSCart' AND id = $1`, schema), bill.CartNumber).Scan(&cartStatus); err == nil {
		return nil, &ValidationError{Code: "POSOFF-0609", Message: fmt.Sprintf("bill %s is already %s - it cannot be held", bill.CartNumber, strings.ToLower(cartStatus))}
	} else if err != sql.ErrNoRows {
		return nil, err
	}
	bill.ID, bill.Lines, bill.LineCount = heldBillID(bill.CartNumber), lines, len(lines)
	bill.Cashier, bill.Status, bill.Note = cashier, "Held", strings.TrimSpace(bill.Note)
	data, _ := json.Marshal(map[string]interface{}{
		"code": bill.ID, "cart_number": bill.CartNumber, "location": bill.Location, "cashier": cashier,
		"customer_id": bill.CustomerID, "coupon_codes": bill.CouponCodes, "lines": lines, "line_count": len(lines),
		"total": round2(bill.Total), "note": bill.Note, "status": "Held",
	})
	var id string
	err = db.DB.QueryRow(fmt.Sprintf(`
		INSERT INTO %s.documents (id, doctype, data, status, created_by) VALUES ($1, 'POSHeldBill', $2, 'Held', $3)
		ON CONFLICT (id) DO UPDATE SET data = EXCLUDED.data, status = 'Held', updated_at = CURRENT_TIMESTAMP
		WHERE %s.documents.status = 'Resumed'
		RETURNING id`, schema, schema), bill.ID, data, userID).Scan(&id)
	if err == sql.ErrNoRows {
		return nil, &ValidationError{Code: "POSOFF-0609", Message: fmt.Sprintf("bill %s is already held", bill.CartNumber)}
	}
	if err != nil {
		return nil, err
	}
	LogAuditEvent(tenantID, cashier, "POS_BILL", "HELD", fmt.Sprintf("Bill %s held at %s (%d line(s))", bill.CartNumber, bill.Location, len(lines)))
	return &bill, nil
}

// ListHeldPOSBills lists the bills held at a store, oldest first.
func ListHeldPOSBills(tenantID, location string) ([]HeldBill, error) {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return nil, err
	}
	rows, err := db.DB.Query(fmt.Sprintf(`
		SELECT id, data, status, to_char(updated_at, 'YYYY-MM-DD HH24:MI') FROM %s.documents
		WHERE doctype = 'POSHeldBill' AND status = 'Held' AND data->>'location' = $1 AND deleted_at IS NULL
		ORDER BY updated_at`, schema), location)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []HeldBill{}
	for rows.Next() {
		var id, dataStr, status, at string
		if err := rows.Scan(&id, &dataStr, &status, &at); err != nil {
			return nil, err
		}
		var b HeldBill
		_ = json.Unmarshal([]byte(dataStr), &b)
		b.ID, b.Status, b.HeldAt = id, status, at
		out = append(out, b)
	}
	return out, rows.Err()
}

// ResumePOSBill takes a held bill back onto a till at the same store. Only
// one till can: the status moves Held -> Resumed in one statement.
func ResumePOSBill(tenantID, heldID, location, cashier string) (*HeldBill, error) {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return nil, err
	}
	var dataStr string
	err = db.DB.QueryRow(fmt.Sprintf(`
		UPDATE %s.documents SET status = 'Resumed', data = data || jsonb_build_object('status', 'Resumed', 'resumed_by', $3::text),
		  updated_at = CURRENT_TIMESTAMP
		WHERE doctype = 'POSHeldBill' AND id = $1 AND status = 'Held' AND data->>'location' = $2
		RETURNING data`, schema), heldID, location, cashier).Scan(&dataStr)
	if err == sql.ErrNoRows {
		return nil, &ValidationError{Code: "POSOFF-0610", Message: "this bill is no longer held here - it was resumed or discarded, or is held at another store"}
	}
	if err != nil {
		return nil, err
	}
	var b HeldBill
	_ = json.Unmarshal([]byte(dataStr), &b)
	b.ID, b.Status = heldID, "Resumed"
	LogAuditEvent(tenantID, cashier, "POS_BILL", "RESUMED", fmt.Sprintf("Bill %s resumed at %s", b.CartNumber, location))
	return &b, nil
}

// DiscardPOSBill drops a held bill the customer will not come back for.
func DiscardPOSBill(tenantID, heldID, location, cashier, reason string) error {
	if strings.TrimSpace(reason) == "" {
		return &ValidationError{Code: "POSOFF-0609", Message: "say why the held bill is being discarded"}
	}
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return err
	}
	res, err := db.DB.Exec(fmt.Sprintf(`
		UPDATE %s.documents SET status = 'Discarded', data = data || jsonb_build_object('status', 'Discarded', 'discarded_by', $3::text, 'discard_reason', $4::text),
		  updated_at = CURRENT_TIMESTAMP
		WHERE doctype = 'POSHeldBill' AND id = $1 AND status = 'Held' AND data->>'location' = $2`, schema), heldID, location, cashier, strings.TrimSpace(reason))
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return &ValidationError{Code: "POSOFF-0610", Message: "this bill is no longer held here"}
	}
	LogAuditEvent(tenantID, cashier, "POS_BILL", "DISCARDED", fmt.Sprintf("Held bill %s discarded: %s", heldID, reason))
	return nil
}
