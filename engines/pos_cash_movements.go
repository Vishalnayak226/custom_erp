package engines

import (
	"custom_erp/db"
	"database/sql"
	"encoding/json"
	"fmt"
	"math"
	"strings"
)

// Stage 73.2.7 - till cash movements, an approved close above the tolerance,
// and shift handover, so a day reconciles:
//
//	opening float + cash sales - cash refunds + paid in - paid out - safe drops = counted
//
// Paid In is cash brought into the till from the store's safe and a Safe Drop
// cash taken out to it: both stay inside 1100 (Cash/Bank), so they post
// nothing - they are the drawer's own record. A Paid Out is a petty expense
// paid from the till: Dr 5290 Till Petty Cash Expenses / Cr 1100. Every
// movement names a Cash Movement reason code and is written only here.

const (
	CashPaidIn   = "Paid In"
	CashPaidOut  = "Paid Out"
	CashSafeDrop = "Safe Drop"
)

// POSSessionCash is where a session's cash stands right now.
type POSSessionCash struct {
	SessionID   string  `json:"session_id"`
	Location    string  `json:"location"`
	Cashier     string  `json:"cashier"`
	Status      string  `json:"status"`
	Opening     float64 `json:"opening_cash"`
	CashSales   float64 `json:"cash_sales"`
	CashRefunds float64 `json:"cash_refunds"`
	PaidIn      float64 `json:"paid_in"`
	PaidOut     float64 `json:"paid_out"`
	SafeDrops   float64 `json:"safe_drops"`
	// Stage 85.D3: customer advances taken and refunded in cash here.
	AdvancesIn  float64 `json:"advances_in"`
	AdvancesOut float64 `json:"advances_out"`
	// Tenders (Stage 85.E5) is what the session's bills took by tender, so
	// the close shows card and UPI beside the counted cash.
	Tenders  map[string]float64 `json:"tenders"`
	Expected float64            `json:"expected_cash"`
}

// SessionCashPosition reads one session's drawer, every figure from the
// server's own records.
func SessionCashPosition(tenantID, sessionID string) (*POSSessionCash, error) {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return nil, err
	}
	var dataStr, status string
	if err := db.DB.QueryRow(fmt.Sprintf(
		`SELECT data, status FROM %s.documents WHERE doctype = 'POSSession' AND id = $1`, schema), sessionID).Scan(&dataStr, &status); err != nil {
		return nil, fmt.Errorf("session not found: %v", err)
	}
	var data map[string]interface{}
	_ = json.Unmarshal([]byte(dataStr), &data)
	out := &POSSessionCash{SessionID: sessionID, Location: strField(data, "location"), Cashier: strField(data, "cashier"),
		Status: status, Opening: numFromInterface(data["opening_cash"])}
	if out.CashSales, err = sessionCashSales(schema, sessionID); err != nil {
		return nil, err
	}
	if out.CashRefunds, err = sessionCashRefunds(schema, sessionID); err != nil {
		return nil, err
	}
	moves, err := sessionCashMovements(schema, sessionID)
	if err != nil {
		return nil, err
	}
	out.PaidIn, out.PaidOut, out.SafeDrops = moves[CashPaidIn], moves[CashPaidOut], moves[CashSafeDrop]
	out.CashSales = round2(out.CashSales)
	out.AdvancesIn, out.AdvancesOut = sessionAdvanceCash(schema, sessionID)
	if t, terr := TenderTotals(tenantID, sessionID, "", "", ""); terr == nil {
		out.Tenders = t
	}
	out.Expected = round2(out.Opening + out.CashSales - out.CashRefunds + out.PaidIn - out.PaidOut - out.SafeDrops + out.AdvancesIn - out.AdvancesOut)
	return out, nil
}

// sessionCashMovements totals a session's movements by type.
func sessionCashMovements(schema, sessionID string) (map[string]float64, error) {
	rows, err := db.DB.Query(fmt.Sprintf(`
		SELECT data->>'movement_type', COALESCE(SUM((data->>'amount')::numeric), 0)
		FROM %s.documents WHERE doctype = 'POSCashMovement' AND data->>'session_id' = $1 AND deleted_at IS NULL
		GROUP BY 1`, schema), sessionID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]float64{}
	for rows.Next() {
		var kind string
		var amount float64
		if err := rows.Scan(&kind, &amount); err != nil {
			return nil, err
		}
		out[kind] = round2(amount)
	}
	return out, rows.Err()
}

// sessionCashRefunds is the cash paid back out of this session's drawer:
// the cash part of every processed refund stamped with the session, and
// (73.2.6) what an exchange paid back when the new items cost less.
func sessionCashRefunds(schema, sessionID string) (float64, error) {
	var paise int64
	err := db.DB.QueryRow(fmt.Sprintf(`
		SELECT COALESCE(SUM(credit), 0) FROM %s.gl_postings
		WHERE document_type IN ('RefundRequest', 'ExchangeCredit') AND account_code = '1100' AND document_id IN (
		  SELECT id FROM %s.documents WHERE doctype IN ('RefundRequest', 'ExchangeCredit') AND data->>'pos_session' = $1)`, schema, schema), sessionID).Scan(&paise)
	return PaiseToRupees(paise), err
}

// RecordPOSCashMovement records one movement against the cashier's own open
// session. A Paid Out or Safe Drop can never take out more cash than the
// drawer should hold.
func RecordPOSCashMovement(tenantID, sessionID, cashier, userID, movementType string, amount float64, reasonCode, reference string) (string, error) {
	if movementType != CashPaidIn && movementType != CashPaidOut && movementType != CashSafeDrop {
		return "", &ValidationError{Code: "POSOFF-0607", Message: fmt.Sprintf("%q is not a cash movement - use Paid In, Paid Out or Safe Drop", movementType)}
	}
	if amount <= 0 {
		return "", &ValidationError{Code: "POSOFF-0607", Message: "a cash movement needs an amount above zero"}
	}
	if err := requireActiveReasonCode(tenantID, reasonCode, "Cash Movement"); err != nil {
		return "", &ValidationError{Code: "POSOFF-0607", Message: err.Error()}
	}
	pos, err := SessionCashPosition(tenantID, sessionID)
	if err != nil {
		return "", err
	}
	if pos.Status != "Open" || pos.Cashier != cashier {
		return "", &ValidationError{Code: "POSOFF-0608", Message: "cash can only be moved in your own open session"}
	}
	if movementType != CashPaidIn && amount-pos.Expected > 0.004 {
		return "", &ValidationError{Code: "POSOFF-0607", Message: fmt.Sprintf("the drawer should hold %.2f - you cannot take out %.2f", pos.Expected, amount)}
	}
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return "", err
	}
	tx, err := db.DB.Begin()
	if err != nil {
		return "", err
	}
	defer tx.Rollback()
	if err := db.SetSearchPath(tx, schema); err != nil {
		return "", err
	}
	id := NewDocID("CASHMV")
	data, _ := json.Marshal(map[string]interface{}{
		"code": id, "session_id": sessionID, "location": pos.Location, "cashier": cashier,
		"movement_type": movementType, "amount": round2(amount), "reason_code": reasonCode,
		"reference": strings.TrimSpace(reference), "status": "Posted",
	})
	if _, err := tx.Exec(fmt.Sprintf(`INSERT INTO %s.documents (id, doctype, data, status, created_by) VALUES ($1, 'POSCashMovement', $2, 'Posted', $3)`, schema),
		id, data, userID); err != nil {
		return "", err
	}
	if movementType == CashPaidOut {
		p := RupeesToPaise(amount)
		if err := PostDoubleEntryTx(tx, tenantID, schema, "POSCashMovement", id, map[string]int64{"5290": p}, map[string]int64{"1100": p},
			"", fmt.Sprintf("POSCashMovement:%s:PAID_OUT", id)); err != nil {
			return "", err
		}
	}
	if err := tx.Commit(); err != nil {
		return "", err
	}
	LogAuditEvent(tenantID, cashier, "POS_CASH_MOVEMENT", strings.ToUpper(strings.ReplaceAll(movementType, " ", "_")),
		fmt.Sprintf("%s %.2f in session %s (%s)", movementType, amount, sessionID, reasonCode))
	return id, nil
}

// TakeOverPOSSession is the shift handover: the next cashier counts the till,
// which closes the outgoing cashier's open session at this store against
// that count (an over-tolerance count needs a reason and goes for approval,
// exactly as a close does) and opens theirs with the counted cash as float.
// Both sessions name each other.
func TakeOverPOSSession(tenantID, location, newCashier, newUserID string, countedCash float64, varianceReason string) (*POSSessionCloseResult, string, error) {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return nil, "", err
	}
	var oldID, oldCashier, posProfile string
	err = db.DB.QueryRow(fmt.Sprintf(`
		SELECT id, data->>'cashier', COALESCE(data->>'pos_profile', '') FROM %s.documents
		WHERE doctype = 'POSSession' AND status = 'Open' AND data->>'location' = $1 LIMIT 1`, schema), location).Scan(&oldID, &oldCashier, &posProfile)
	if err == sql.ErrNoRows {
		return nil, "", &ValidationError{Code: "POSOFF-0608", Message: "no session is open at this store - open your own instead"}
	}
	if err != nil {
		return nil, "", err
	}
	if oldCashier == newCashier {
		return nil, "", &ValidationError{Code: "POSOFF-0608", Message: "this is already your session - close it, or carry on"}
	}
	res, err := closePOSSession(tenantID, oldID, oldCashier, countedCash, varianceReason, newCashier)
	if err != nil {
		return nil, "", err
	}
	newID, err := OpenPOSSession(tenantID, posProfile, location, newCashier, newUserID, countedCash)
	if err != nil {
		return res, "", err
	}
	if _, err := db.DB.Exec(fmt.Sprintf(
		`UPDATE %s.documents SET data = data || jsonb_build_object('taken_over_from', $2::text) WHERE id = $1`, schema), newID, oldID); err != nil {
		return res, newID, err
	}
	LogAuditEvent(tenantID, newCashier, "POS_SESSION", "TAKEN_OVER",
		fmt.Sprintf("Session %s (%s) handed over to %s as %s, counted %.2f, variance %.2f", oldID, oldCashier, newCashier, newID, countedCash, res.Variance))
	return res, newID, nil
}

// POSSessionCloseResult is what closing a session produced.
type POSSessionCloseResult struct {
	Expected          float64  `json:"expected_cash"`
	Variance          float64  `json:"variance"`
	Status            string   `json:"status"`
	OfflineGapCarts   []string `json:"offline_queue_gap,omitempty"`
	RequiredApprover  string   `json:"required_role,omitempty"`
	VarianceTolerance float64  `json:"variance_tolerance"`
}

// finishPOSSessionApproval is the approval hook: an approved over-tolerance
// close is Closed; a rejected one goes back to Open so the cashier recounts.
func finishPOSSessionApproval(tenantID, sessionID, decision string) {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return
	}
	next := "Closed"
	if decision != "Approved" {
		next = "Open"
	}
	if _, err := db.DB.Exec(fmt.Sprintf(
		`UPDATE %s.documents SET status = $2, data = jsonb_set(data, '{status}', to_jsonb($3::text)), updated_at = CURRENT_TIMESTAMP
		 WHERE doctype = 'POSSession' AND id = $1`, schema), sessionID, next, next); err != nil {
		LogSystemError(tenantID, "", "ERROR", "finishPOSSessionApproval", fmt.Sprintf("session %s %s but status update failed: %v", sessionID, decision, err), "")
	}
}

// varianceNeedsApproval is whether a close's variance goes to a manager.
func varianceNeedsApproval(tenantID string, variance float64) (string, error) {
	if math.Abs(variance) <= posDrawerVarianceToleranceFor(tenantID) {
		return "", nil
	}
	return RequiredApproverRoleForAmount(tenantID, "POSSession", math.Abs(variance))
}
