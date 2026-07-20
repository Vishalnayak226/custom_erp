package engines

import (
	"custom_erp/db"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"
)

// Stage 20 Track B.1 (20.7/20.8): cashier session lifecycle with cash
// variance on close. POSSession is registered in doctype_meta for read-only
// browsing (migrations_stage20a_pos_maturity.sql grants no create/update to
// any role), so every write goes through OpenPOSSession/ClosePOSSession -
// cashier identity and the expected-cash figure can never be spoofed via the
// generic doc API the way a hand-crafted POSCart write technically still
// could be (a pre-existing gap in that doctype, out of scope here).

// GetOpenSessionForCashier returns the id of the Open POSSession for this
// cashier+location, or "" if none exists. Used both to gate checkout (20.7)
// and by the POS screen to know whether to show the Open/Close Session UI.
func GetOpenSessionForCashier(tenantID, location, cashier string) (string, error) {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return "", err
	}
	var id string
	err = db.DB.QueryRow(fmt.Sprintf(`
		SELECT id FROM %s.documents
		WHERE doctype = 'POSSession' AND status = 'Open'
		  AND data->>'cashier' = $1 AND data->>'location' = $2
		LIMIT 1`, schema), cashier, location).Scan(&id)
	if err == sql.ErrNoRows {
		return "", nil
	}
	return id, err
}

// OpenPOSSession opens a new cashier session. cashier/userID are taken from
// the caller's resolved identity, never from the request body. Refuses a
// second concurrent Open session for the same cashier (any location) or the
// same location (any cashier) - one till, one active operator at a time.
func OpenPOSSession(tenantID, posProfile, location, cashier, userID string, openingCash float64) (string, error) {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return "", err
	}
	if location == "" || cashier == "" {
		return "", errors.New("location and cashier are required")
	}
	if openingCash < 0 {
		return "", errors.New("opening cash cannot be negative")
	}

	var existing string
	err = db.DB.QueryRow(fmt.Sprintf(`
		SELECT id FROM %s.documents
		WHERE doctype = 'POSSession' AND status = 'Open'
		  AND (data->>'cashier' = $1 OR data->>'location' = $2)
		LIMIT 1`, schema), cashier, location).Scan(&existing)
	if err == nil {
		return "", fmt.Errorf("a session is already open for this cashier or location (%s) - close it first", existing)
	} else if err != sql.ErrNoRows {
		return "", err
	}

	id := fmt.Sprintf("POSSESS-%d", time.Now().UnixNano())
	data := map[string]interface{}{
		"pos_profile":  posProfile,
		"location":     location,
		"cashier":      cashier,
		"opening_cash": openingCash,
		"status":       "Open",
	}
	marshaled, err := json.Marshal(data)
	if err != nil {
		return "", err
	}
	_, err = db.DB.Exec(fmt.Sprintf(`
		INSERT INTO %s.documents (id, doctype, data, status, created_by)
		VALUES ($1, 'POSSession', $2, 'Open', $3)`, schema), id, marshaled, userID)
	if err != nil {
		return "", err
	}
	return id, nil
}

// expectedCashForSession sums the Cash-mode sale totals of every Paid
// POSCart tagged with this session (handleCheckout stamps "pos_session" onto
// the cart's stored data at checkout time - see internal/server/handlers_pim_pos_finance.go),
// on top of the session's own opening float. A cart with no payment_mode at
// all (pre-Stage-20 data) is treated as Cash, matching the implicit-cash-only
// assumption that was true before 20.9.
func expectedCashForSession(tenantID, sessionID string, openingCash float64) (float64, error) {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return 0, err
	}
	rows, err := db.DB.Query(fmt.Sprintf(`
		SELECT data FROM %s.documents
		WHERE doctype = 'POSCart' AND status = 'Paid' AND data->>'pos_session' = $1`, schema), sessionID)
	if err != nil {
		return 0, err
	}
	defer rows.Close()

	total := openingCash
	for rows.Next() {
		var dataStr string
		if err := rows.Scan(&dataStr); err != nil {
			return 0, err
		}
		var cart struct {
			PaymentMode string `json:"payment_mode"`
			Items       []struct {
				Qty       float64 `json:"qty"`
				SalePrice float64 `json:"sale_price"`
			} `json:"items"`
		}
		if err := json.Unmarshal([]byte(dataStr), &cart); err != nil {
			continue
		}
		if cart.PaymentMode != "" && cart.PaymentMode != "Cash" {
			continue
		}
		for _, item := range cart.Items {
			total += item.Qty * item.SalePrice
		}
	}
	return total, rows.Err()
}

// ClosePOSSession computes the expected-cash figure server-side (never
// trusts a client-supplied expectation), stores the counted-vs-expected
// variance, and closes the session. Refuses to close a session that isn't
// this cashier's own Open session.
func ClosePOSSession(tenantID, sessionID, cashier string, countedCash float64) (expected float64, variance float64, err error) {
	if countedCash < 0 {
		return 0, 0, errors.New("counted cash cannot be negative")
	}
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return 0, 0, err
	}

	var dataStr, status string
	err = db.DB.QueryRow(fmt.Sprintf(`
		SELECT data, status FROM %s.documents WHERE doctype = 'POSSession' AND id = $1`, schema), sessionID).
		Scan(&dataStr, &status)
	if err != nil {
		return 0, 0, fmt.Errorf("session not found: %v", err)
	}
	if status != "Open" {
		return 0, 0, fmt.Errorf("session is not open (current status: %s)", status)
	}
	var data map[string]interface{}
	if err := json.Unmarshal([]byte(dataStr), &data); err != nil {
		return 0, 0, err
	}
	if sessionCashier, _ := data["cashier"].(string); sessionCashier != cashier {
		return 0, 0, errors.New("only the cashier who opened this session can close it")
	}

	openingCash := 0.0
	if v, ok := data["opening_cash"].(float64); ok {
		openingCash = v
	}
	expected, err = expectedCashForSession(tenantID, sessionID, openingCash)
	if err != nil {
		return 0, 0, err
	}
	variance = countedCash - expected

	data["status"] = "Closed"
	data["expected_cash"] = expected
	data["closing_counted_cash"] = countedCash
	data["variance"] = variance
	marshaled, err := json.Marshal(data)
	if err != nil {
		return 0, 0, err
	}
	_, err = db.DB.Exec(fmt.Sprintf(`
		UPDATE %s.documents SET data = $1, status = 'Closed', updated_at = CURRENT_TIMESTAMP
		WHERE id = $2`, schema), marshaled, sessionID)
	if err != nil {
		return 0, 0, err
	}
	return expected, variance, nil
}
