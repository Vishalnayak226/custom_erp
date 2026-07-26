package engines

import (
	"custom_erp/db"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"math"
	"strings"
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

// RecordOfflineHeartbeat (24.36) upserts the calling cashier's currently-
// queued offline cart_numbers for this session - a best-effort beacon the
// frontend fires whenever it has any network path (see public/app.js's
// sendOfflineQueueHeartbeat), so the server has a last-known-state
// checkpoint from *before* a gap, not just whatever eventually syncs. This
// can't see a cart queued and lost with literally zero connectivity ever in
// between (nothing can - the server was never told), but it closes the far
// more common case of a queue that existed for a while, with at least one
// connectivity blip, before being wiped.
func RecordOfflineHeartbeat(tenantID, sessionID, cashier, location string, cartNumbers []string) error {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return err
	}
	if sessionID == "" {
		return errors.New("session_id is required")
	}
	marshaled, err := json.Marshal(cartNumbers)
	if err != nil {
		return err
	}
	_, err = db.DB.Exec(fmt.Sprintf(`
		INSERT INTO %s.pos_offline_heartbeats (session_id, cashier, location, cart_numbers, updated_at)
		VALUES ($1, $2, $3, $4, CURRENT_TIMESTAMP)
		ON CONFLICT (session_id) DO UPDATE SET
			cashier = EXCLUDED.cashier, location = EXCLUDED.location,
			cart_numbers = EXCLUDED.cart_numbers, updated_at = CURRENT_TIMESTAMP`, schema),
		sessionID, cashier, location, marshaled)
	return err
}

// detectOfflineQueueGap (24.36) diffs the last heartbeat this session ever
// sent against which of those cart_numbers actually landed as a real
// POSCart document (any status - existence alone proves the server saw it;
// only a cart_number that never arrived at all counts as missing). Called
// from ClosePOSSession, not blocking - same "flag for review, don't block
// the shift from closing" posture Stage 20.13 already established for
// offline-sync stock shortfalls (POSOfflineSyncVariance).
func detectOfflineQueueGap(tenantID, schema, sessionID string) (missing []string, err error) {
	var cartNumbersJSON string
	var cashier, location string
	err = db.DB.QueryRow(fmt.Sprintf(`
		SELECT cart_numbers, cashier, location FROM %s.pos_offline_heartbeats WHERE session_id = $1`, schema), sessionID).
		Scan(&cartNumbersJSON, &cashier, &location)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var heartbeatCarts []string
	if err := json.Unmarshal([]byte(cartNumbersJSON), &heartbeatCarts); err != nil || len(heartbeatCarts) == 0 {
		return nil, nil
	}
	for _, cartNumber := range heartbeatCarts {
		var exists bool
		if err := db.DB.QueryRow(fmt.Sprintf(
			`SELECT EXISTS(SELECT 1 FROM %s.documents WHERE doctype = 'POSCart' AND id = $1)`, schema),
			cartNumber).Scan(&exists); err != nil {
			return nil, err
		}
		if !exists {
			missing = append(missing, cartNumber)
		}
	}
	if len(missing) == 0 {
		return nil, nil
	}

	missingJSON, _ := json.Marshal(missing)
	gapID := fmt.Sprintf("POSGAP-%d", time.Now().UnixNano())
	gapData := map[string]interface{}{
		"session_id":            sessionID,
		"cashier":               cashier,
		"location":              location,
		"missing_cart_numbers":  string(missingJSON),
		"missing_count":         float64(len(missing)),
		"status":                "Open",
	}
	marshaled, _ := json.Marshal(gapData)
	if _, err := db.DB.Exec(fmt.Sprintf(`
		INSERT INTO %s.documents (id, doctype, data, status, created_by)
		VALUES ($1, 'POSOfflineQueueGap', $2, 'Active', $3)`, schema), gapID, marshaled, cashier); err != nil {
		return missing, err
	}
	LogAuditEvent(tenantID, cashier, "POS_OFFLINE_QUEUE_GAP", "DETECTED",
		fmt.Sprintf("Session %s closed with %d offline sale(s) heartbeated but never synced: %v", sessionID, len(missing), missing))
	DispatchNotification(tenantID, "pos.offline_queue_gap", sessionID, map[string]string{
		"cashier":       cashier,
		"location":      location,
		"missing_count": fmt.Sprintf("%d", len(missing)),
	})
	return missing, nil
}

// posDrawerVarianceTolerance (POSOFF-0240, Stage 25.7) is the flat rupee
// threshold beyond which a counted-vs-expected cash variance needs a
// reason on record before the session can close - same "hardcoded-constant
// default" shape as vendor_invoice.go's defaultVendorInvoiceTolerancePercent.
// A flat amount rather than a percentage, since a drawer variance is a cash
// counting error, not proportional to the day's total sales the way a
// vendor invoice mismatch is proportional to the PO amount.
const posDrawerVarianceTolerance = 50.0

// ClosePOSSession computes the expected-cash figure server-side (never
// trusts a client-supplied expectation), stores the counted-vs-expected
// variance, and closes the session. Refuses to close a session that isn't
// this cashier's own Open session. varianceReason is required only when the
// variance exceeds posDrawerVarianceTolerance (POSOFF-0240) - same
// "silently record if small, require a reason if not" shape as
// TRN-0259's short-receive check.
//
// offlineGapCartNumbers (24.36) is non-nil when this session's last known
// offline-queue heartbeat named cart_numbers that never actually landed as
// real POSCart documents - see detectOfflineQueueGap. Never blocks the
// close (same "flag for review" posture as the cash-variance/stock-
// shortfall paths already use), it's returned purely so the caller can
// surface a warning.
func ClosePOSSession(tenantID, sessionID, cashier string, countedCash float64, varianceReason string) (expected float64, variance float64, offlineGapCartNumbers []string, err error) {
	if countedCash < 0 {
		return 0, 0, nil, errors.New("counted cash cannot be negative")
	}
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return 0, 0, nil, err
	}

	var dataStr, status string
	err = db.DB.QueryRow(fmt.Sprintf(`
		SELECT data, status FROM %s.documents WHERE doctype = 'POSSession' AND id = $1`, schema), sessionID).
		Scan(&dataStr, &status)
	if err != nil {
		return 0, 0, nil, fmt.Errorf("session not found: %v", err)
	}
	if status != "Open" {
		return 0, 0, nil, fmt.Errorf("session is not open (current status: %s)", status)
	}
	var data map[string]interface{}
	if err := json.Unmarshal([]byte(dataStr), &data); err != nil {
		return 0, 0, nil, err
	}
	if sessionCashier, _ := data["cashier"].(string); sessionCashier != cashier {
		return 0, 0, nil, errors.New("only the cashier who opened this session can close it")
	}

	openingCash := 0.0
	if v, ok := data["opening_cash"].(float64); ok {
		openingCash = v
	}
	expected, err = expectedCashForSession(tenantID, sessionID, openingCash)
	if err != nil {
		return 0, 0, nil, err
	}
	variance = countedCash - expected

	if math.Abs(variance) > posDrawerVarianceTolerance && strings.TrimSpace(varianceReason) == "" {
		// POSOFF-0240 (Stage 25.7): "Drawer variance exceeds tolerance."
		return expected, variance, nil, &ValidationError{Code: "POSOFF-0240", Message: fmt.Sprintf("cash variance of %.2f exceeds the %.2f tolerance - a reason is required to close this session", variance, posDrawerVarianceTolerance)}
	}

	data["status"] = "Closed"
	data["expected_cash"] = expected
	data["closing_counted_cash"] = countedCash
	data["variance"] = variance
	if strings.TrimSpace(varianceReason) != "" {
		data["variance_reason"] = varianceReason
	}
	marshaled, err := json.Marshal(data)
	if err != nil {
		return 0, 0, nil, err
	}
	_, err = db.DB.Exec(fmt.Sprintf(`
		UPDATE %s.documents SET data = $1, status = 'Closed', updated_at = CURRENT_TIMESTAMP
		WHERE id = $2`, schema), marshaled, sessionID)
	if err != nil {
		return 0, 0, nil, err
	}

	offlineGapCartNumbers, gapErr := detectOfflineQueueGap(tenantID, schema, sessionID)
	if gapErr != nil {
		log.Printf("[WARN] offline queue gap detection failed for session %s: %v", sessionID, gapErr)
	}
	return expected, variance, offlineGapCartNumbers, nil
}
