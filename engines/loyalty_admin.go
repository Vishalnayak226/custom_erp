package engines

import (
	"custom_erp/db"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

// Stage 73.2.11 - manual point adjustment, and 73.2.12 - loyalty liability.
//
// A manual adjustment is a new ledger row with a reason, never an edit. It is
// raised as a LoyaltyPointAdjustment; at or above the approval value (the
// LoyaltyPointAdjustment approval rule, shipped 500+ Store Manager) it waits
// for a manager who is not the requester. A credit is stored as an Earn row
// tagged with the adjustment (it expires like earned points), a debit as an
// "Adjustment" row - so every existing balance query counts both correctly
// and a debit is never mistaken for a redemption by the daily limit.
//
// Liability (loyalty.liability_accounting = "accrue"): the value of points
// earned or credited is booked Dr 5260 Loyalty Points Expense / Cr 2630
// Loyalty Points Liability, and released (Dr 2630) when points are redeemed,
// reversed, debited or expire; a return giving points back books it again.
// With the default ("redemption") nothing changes: points are expensed to
// 5250 when redeemed, as before. Either way the Loyalty Points Movement
// report gives earned / redeemed / restored / reversed / adjusted / expired
// and the closing balance, reconciled to the ledger (and to 2630 when
// accruing).

const (
	LoyaltyTxnAdjustment      = "Adjustment"
	loyaltyAdjustmentDoctype  = "LoyaltyPointAdjustment"
	loyaltyAccountingAccrue   = "accrue"
	accountLoyaltyLiability   = "2630"
	accountLoyaltyExpense     = "5260"
	accountLoyaltyRedeemedExp = "5250"
)

func loyaltyAccruesForSchema(schema string) bool {
	return GetSettingStringForSchema(schema, "loyalty.liability_accounting") == loyaltyAccountingAccrue
}

// loyaltyPointValuePaise is what one point is worth, in paise.
func loyaltyPointValuePaise(tenantID string) int64 {
	v := redemptionValuePerPointFor(tenantID)
	if v <= 0 {
		v = 1
	}
	return RupeesToPaise(float64(v))
}

// postLoyaltyValueTx books points' value moving into (accrue: Dr other /
// Cr 2630) or out of (release: Dr 2630 / Cr other) the liability, when the
// tenant accrues. other is 5250 where the sale or refund already booked the
// points there, and 5260 (blank) for everything else.
func postLoyaltyValueTx(tx *sql.Tx, tenantID, schema, docType, docID, key string, valuePaise int64, accrue bool, other string) error {
	if valuePaise <= 0 || !loyaltyAccruesForSchema(schema) {
		return nil
	}
	if other == "" {
		other = accountLoyaltyExpense
	}
	debits, credits := map[string]int64{other: valuePaise}, map[string]int64{accountLoyaltyLiability: valuePaise}
	if !accrue {
		debits, credits = map[string]int64{accountLoyaltyLiability: valuePaise}, map[string]int64{other: valuePaise}
	}
	return PostDoubleEntryTx(tx, tenantID, schema, docType, docID, debits, credits, "", key)
}

// postLoyaltyValue is postLoyaltyValueTx in a transaction of its own, for the
// post-commit earn.
func postLoyaltyValue(tenantID, docType, docID, key string, valuePaise int64, accrue bool, other string) error {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return err
	}
	if valuePaise <= 0 || !loyaltyAccruesForSchema(schema) {
		return nil
	}
	tx, err := db.DB.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err := postLoyaltyValueTx(tx, tenantID, schema, docType, docID, key, valuePaise, accrue, other); err != nil {
		return err
	}
	return tx.Commit()
}

// LoyaltyAdjustment is one manual adjustment.
type LoyaltyAdjustment struct {
	ID           string  `json:"id"`
	CustomerID   string  `json:"customer_id"`
	Points       int     `json:"points"`
	PointsValue  float64 `json:"points_value"`
	ReasonCode   string  `json:"reason_code"`
	Note         string  `json:"note,omitempty"`
	Status       string  `json:"status"`
	RequiredRole string  `json:"required_role,omitempty"`
	CreatedAt    string  `json:"created_at,omitempty"`
}

// RequestLoyaltyAdjustment raises a manual credit (points > 0) or debit
// (points < 0). Below the approval value it posts at once; at or above it,
// it goes to a manager. A debit can never take the balance below zero.
func RequestLoyaltyAdjustment(tenantID, customerID string, points int, reasonCode, note, userID, role string) (*LoyaltyAdjustment, error) {
	customerID, reasonCode = strings.TrimSpace(customerID), strings.TrimSpace(reasonCode)
	if customerID == "" || points == 0 {
		return nil, &ValidationError{Code: "CUSTOM-0614", Message: "an adjustment needs a customer and a number of points other than zero"}
	}
	if err := requireActiveReasonCode(tenantID, reasonCode, "Loyalty Adjustment"); err != nil {
		return nil, &ValidationError{Code: "CUSTOM-0614", Message: err.Error()}
	}
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return nil, err
	}
	var exists bool
	_ = db.DB.QueryRow(fmt.Sprintf(`SELECT EXISTS(SELECT 1 FROM %s.documents WHERE doctype = 'Customer' AND (id = $1 OR data->>'code' = $1) AND deleted_at IS NULL)`, schema), customerID).Scan(&exists)
	if !exists {
		return nil, &ValidationError{Code: "CUSTOM-0614", Message: fmt.Sprintf("no customer %q", customerID)}
	}
	if points < 0 {
		if bal, _ := GetLoyaltyBalance(tenantID, customerID); bal+points < 0 {
			return nil, &ValidationError{Code: "CUSTOM-0615", Message: fmt.Sprintf("%s has %d point(s) - a debit of %d would take the balance below zero", customerID, bal, -points)}
		}
	}
	abs := points
	if abs < 0 {
		abs = -abs
	}
	value := PaiseToRupees(int64(abs) * loyaltyPointValuePaise(tenantID))
	approver, err := RequiredApproverRoleForAmount(tenantID, loyaltyAdjustmentDoctype, value)
	if err != nil {
		return nil, err
	}
	adj := &LoyaltyAdjustment{ID: NewDocID("LPA"), CustomerID: customerID, Points: points, PointsValue: value, ReasonCode: reasonCode,
		Note: strings.TrimSpace(note), Status: "Draft", RequiredRole: approver}
	data, _ := json.Marshal(map[string]interface{}{"code": adj.ID, "customer_id": customerID, "points": points, "points_value": value,
		"reason_code": reasonCode, "note": adj.Note, "status": "Draft"})
	if _, err := db.DB.Exec(fmt.Sprintf(`INSERT INTO %s.documents (id, doctype, data, status, created_by) VALUES ($1, $2, $3, 'Draft', $4)`, schema),
		adj.ID, loyaltyAdjustmentDoctype, data, userID); err != nil {
		return nil, err
	}
	if approver != "" {
		if err := SubmitForApproval(tenantID, loyaltyAdjustmentDoctype, adj.ID, userID, role); err != nil {
			return nil, err
		}
		adj.Status = "Pending Approval"
		return adj, nil
	}
	if err := postLoyaltyAdjustment(tenantID, adj.ID); err != nil {
		return nil, err
	}
	adj.Status = "Posted"
	return adj, nil
}

// postLoyaltyAdjustment writes an adjustment's ledger row (and, when
// accruing, its value) - once: the document moves to Posted in the same
// transaction, and a debit is re-checked against the balance under the
// customer's lock.
func postLoyaltyAdjustment(tenantID, adjID string) error {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return err
	}
	tx, err := db.DB.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var dataStr, status string
	if err := tx.QueryRow(fmt.Sprintf(`SELECT data, status FROM %s.documents WHERE doctype = $1 AND id = $2 FOR UPDATE`, schema), loyaltyAdjustmentDoctype, adjID).Scan(&dataStr, &status); err != nil {
		return err
	}
	if status == "Posted" {
		return nil
	}
	var data map[string]interface{}
	_ = json.Unmarshal([]byte(dataStr), &data)
	customerID, points := strField(data, "customer_id"), int(numFromInterface(data["points"]))
	balance, err := lockedLoyaltyBalanceTx(tx, schema, customerID)
	if err != nil {
		return err
	}
	value := int64(points) * loyaltyPointValuePaise(tenantID)
	if points > 0 {
		expires := time.Now().AddDate(0, 0, GetSettingInt(tenantID, "loyalty.point_expiry_days"))
		if _, err := tx.Exec(fmt.Sprintf(`INSERT INTO %s.loyalty_point_ledger (customer_id, transaction_type, points, reference_doctype, reference_id, expires_at)
			VALUES ($1, 'Earn', $2, $3, $4, $5)`, schema), customerID, points, loyaltyAdjustmentDoctype, adjID, expires); err != nil {
			return err
		}
		if err := postLoyaltyValueTx(tx, tenantID, schema, loyaltyAdjustmentDoctype, adjID, loyaltyAdjustmentDoctype+":"+adjID+":ACCRUE", value, true, ""); err != nil {
			return err
		}
	} else {
		if balance+points < 0 {
			return &ValidationError{Code: "CUSTOM-0615", Message: fmt.Sprintf("%s has %d point(s) now - the debit of %d would take the balance below zero", customerID, balance, -points)}
		}
		if _, err := tx.Exec(fmt.Sprintf(`INSERT INTO %s.loyalty_point_ledger (customer_id, transaction_type, points, reference_doctype, reference_id)
			VALUES ($1, $2, $3, $4, $5)`, schema), customerID, LoyaltyTxnAdjustment, -points, loyaltyAdjustmentDoctype, adjID); err != nil {
			return err
		}
		if err := postLoyaltyValueTx(tx, tenantID, schema, loyaltyAdjustmentDoctype, adjID, loyaltyAdjustmentDoctype+":"+adjID+":RELEASE", -value, false, accountLoyaltyExpense); err != nil {
			return err
		}
	}
	data["status"] = "Posted"
	marshaled, _ := json.Marshal(data)
	if _, err := tx.Exec(fmt.Sprintf(`UPDATE %s.documents SET data = $1, status = 'Posted', updated_at = CURRENT_TIMESTAMP WHERE id = $2`, schema), marshaled, adjID); err != nil {
		return err
	}
	return tx.Commit()
}

// postApprovedLoyaltyAdjustment is the approval hook.
func postApprovedLoyaltyAdjustment(tenantID, adjID string) {
	if err := postLoyaltyAdjustment(tenantID, adjID); err != nil {
		LogSystemError(tenantID, "", "ERROR", "postApprovedLoyaltyAdjustment", fmt.Sprintf("adjustment %s approved but not posted: %v", adjID, err), "")
	}
}

// ListLoyaltyAdjustments lists a customer's adjustments, newest first.
func ListLoyaltyAdjustments(tenantID, customerID string) ([]LoyaltyAdjustment, error) {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return nil, err
	}
	rows, err := db.DB.Query(fmt.Sprintf(`SELECT id, data, status, to_char(created_at, 'YYYY-MM-DD HH24:MI') FROM %s.documents
		WHERE doctype = $1 AND data->>'customer_id' = $2 ORDER BY created_at DESC`, schema), loyaltyAdjustmentDoctype, customerID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []LoyaltyAdjustment{}
	for rows.Next() {
		var id, dataStr, status, at string
		if err := rows.Scan(&id, &dataStr, &status, &at); err != nil {
			return nil, err
		}
		var a LoyaltyAdjustment
		_ = json.Unmarshal([]byte(dataStr), &a)
		a.ID, a.Status, a.CreatedAt = id, status, at
		out = append(out, a)
	}
	return out, rows.Err()
}

// --- 73.2.12: the points movement report -------------------------------------

// loyaltyMovementRow is one line of the movement report.
type loyaltyMovementRow struct {
	key, label string
	sign       int // +1 adds to the balance, -1 takes from it, 0 is a total
}

var loyaltyMovementRows = []loyaltyMovementRow{
	{"earned", "Earned on sales", 1},
	{"restored", "Given back on returns (points paid with)", 1},
	{"adjusted_in", "Credited by adjustment", 1},
	{"redeemed", "Redeemed at the till and online", -1},
	{"reversed", "Taken back on returns (points earned)", -1},
	{"adjusted_out", "Debited by adjustment", -1},
	{"expired", "Expired (breakage)", -1},
}

// loyaltyMovementSQL classifies every ledger row into one movement.
const loyaltyMovementSQL = `CASE
	WHEN transaction_type = 'Earn' AND reference_doctype = 'RefundRequest' THEN 'restored'
	WHEN transaction_type = 'Earn' AND reference_doctype = 'LoyaltyPointAdjustment' THEN 'adjusted_in'
	WHEN transaction_type = 'Earn' AND COALESCE(reference_id, '') LIKE '%%:REDEMPTION-REVERSAL' THEN 'redeemed_back'
	WHEN transaction_type = 'Earn' AND reference_doctype = 'POSCart' THEN 'earned'
	WHEN transaction_type = 'Earn' THEN 'other_in'
	WHEN transaction_type = 'Burn' AND reference_doctype = 'LoyaltyExpiry' THEN 'expired'
	WHEN transaction_type = 'Burn' THEN 'redeemed'
	WHEN transaction_type = 'Reversal' THEN 'reversed'
	WHEN transaction_type = 'Adjustment' THEN 'adjusted_out'
	ELSE 'other' END`

func loyaltyMovementReport(tenantID string, params map[string]string) ([]map[string]interface{}, error) {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return nil, err
	}
	from, to := strings.TrimSpace(params["from_date"]), strings.TrimSpace(params["to_date"])
	if from == "" {
		from = "1900-01-01"
	}
	if to == "" {
		to = BusinessToday()
	}
	var opening int64
	if err := db.DB.QueryRow(fmt.Sprintf(`SELECT COALESCE(SUM(CASE WHEN transaction_type = 'Earn' THEN points ELSE -points END), 0)
		FROM %s.loyalty_point_ledger WHERE created_at < public.erp_day_start($1::date)`, schema), from).Scan(&opening); err != nil {
		return nil, err
	}
	rows, err := db.DB.Query(fmt.Sprintf(`SELECT `+loyaltyMovementSQL+` AS kind, COALESCE(SUM(points), 0) FROM %s.loyalty_point_ledger
		WHERE created_at >= public.erp_day_start($1::date) AND created_at < public.erp_day_start($2::date + 1) GROUP BY 1`, schema), from, to)
	if err != nil {
		return nil, err
	}
	moves := map[string]int64{}
	for rows.Next() {
		var kind string
		var pts int64
		if err := rows.Scan(&kind, &pts); err != nil {
			rows.Close()
			return nil, err
		}
		moves[kind] += pts
	}
	rows.Close()
	moves["redeemed"] -= moves["redeemed_back"] // points the till handed straight back
	v := loyaltyPointValuePaise(tenantID)
	out := []map[string]interface{}{}
	add := func(label string, pts int64) {
		out = append(out, map[string]interface{}{"movement": label, "points": pts, "value": PaiseToRupees(pts * v)})
	}
	add("Opening balance", opening)
	closing := opening + moves["other_in"] - moves["other"]
	for _, r := range loyaltyMovementRows {
		add(r.label, int64(r.sign)*moves[r.key])
		closing += int64(r.sign) * moves[r.key]
	}
	if moves["other_in"] != 0 {
		add("Other credits (opening balances, imports)", moves["other_in"])
	}
	if moves["other"] != 0 {
		add("Other debits", -moves["other"])
	}
	add("Closing balance", closing)
	var ledgerNow int64
	_ = db.DB.QueryRow(fmt.Sprintf(`SELECT COALESCE(SUM(CASE WHEN transaction_type = 'Earn' THEN points ELSE -points END), 0)
		FROM %s.loyalty_point_ledger WHERE created_at < public.erp_day_start($1::date + 1)`, schema), to).Scan(&ledgerNow)
	if ledgerNow != closing {
		add("DIFFERENCE from the ledger (investigate)", ledgerNow-closing)
	}
	if loyaltyAccruesForSchema(schema) {
		var liability int64
		_ = db.DB.QueryRow(fmt.Sprintf(`SELECT COALESCE(SUM(credit - debit), 0) FROM %s.gl_postings WHERE account_code = $1 AND created_at < public.erp_day_start($2::date + 1)`, schema),
			accountLoyaltyLiability, to).Scan(&liability)
		out = append(out, map[string]interface{}{"movement": "Liability in the books (2630)", "points": nil, "value": PaiseToRupees(liability)})
		out = append(out, map[string]interface{}{"movement": "Books less points outstanding", "points": nil, "value": PaiseToRupees(liability - closing*v)})
	}
	return out, nil
}

func init() {
	RegisterReport(ReportDefinition{
		ID: "loyalty-points-movement", Label: "Loyalty Points Movement", Category: "CRM", ModuleKey: "crm_loyalty",
		Columns: []ReportColumn{{Key: "movement", Label: "Movement"}, {Key: "points", Label: "Points"}, {Key: "value", Label: "Value", Sensitive: true}},
		Params:  []ReportParam{{Key: "from_date", Label: "From", Type: "date"}, {Key: "to_date", Label: "To", Type: "date"}},
		Run:     loyaltyMovementReport,
	})
}
