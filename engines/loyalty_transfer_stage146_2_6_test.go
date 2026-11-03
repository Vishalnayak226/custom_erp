package engines

import (
	"encoding/json"
	"testing"
	"time"

	"custom_erp/db"
)

// Stage 146.2.6 - an approved transfer of usable points between customers.
func TestStage14626PointsTransfer(t *testing.T) {
	db.InitDB(testConnStr())
	tenantID, schemaName := uniqueLifecycleTenant(t)
	defer dropLifecycleTenant(tenantID, schemaName)
	if _, err := ProvisionTenantSchema(tenantID, schemaName, "0.1.0-test"); err != nil {
		t.Fatalf("ProvisionTenantSchema: %v", err)
	}
	for _, c := range []string{"CUST-TA", "CUST-TB"} {
		raw, _ := json.Marshal(map[string]interface{}{"code": c, "name": c})
		if _, err := db.DB.Exec("INSERT INTO "+schemaName+".documents (id, doctype, data, status, created_by) VALUES ($1, 'Customer', $2, 'Active', 'system')", c, raw); err != nil {
			t.Fatal(err)
		}
	}
	code := func(err error) string {
		if ve, ok := err.(*ValidationError); ok {
			return ve.Code
		}
		return ""
	}
	soon, late, past := time.Now().AddDate(0, 0, 10), time.Now().AddDate(0, 0, 300), time.Now().AddDate(0, 0, -1)
	seed := func(cust string, pts int, exp *time.Time) {
		t.Helper()
		if err := insertLoyaltyLedgerEntryInSchema(schemaName, cust, "Earn", pts, "Seed", "SEED", exp); err != nil {
			t.Fatal(err)
		}
	}
	seed("CUST-TA", 100, &soon)
	seed("CUST-TA", 200, &late)
	seed("CUST-TA", 40, &past) // expired, not swept yet
	seed("CUST-TA", 50, nil)
	if _, err := db.DB.Exec("UPDATE " + schemaName + ".loyalty_point_ledger SET pending_until = NOW() + interval '5 days' WHERE customer_id = 'CUST-TA' AND points = 50"); err != nil {
		t.Fatal(err)
	}
	bal := func(c string) int { b, _ := GetLoyaltyBalance(tenantID, c); return b }
	// A's balance counts the 40 expired-unswept; what can be sent is 100 + 200.
	startA := bal("CUST-TA")
	approve := func(id string) string {
		var ch string
		_ = db.DB.QueryRow("SELECT data->>'challenge_id' FROM "+schemaName+".documents WHERE id = $1", id).Scan(&ch)
		if _, err := db.DB.Exec("UPDATE "+schemaName+".loyalty_redemption_otp_challenges SET otp_hash = $1 WHERE id = $2", hashOTP("123456"), ch); err != nil {
			t.Fatal(err)
		}
		return "123456"
	}

	// Off by default.
	if _, err := RequestLoyaltyPointsTransfer(tenantID, "CUST-TA", "CUST-TB", 10, "mgr"); code(err) != "CUSTOM-0621" {
		t.Fatalf("transfer is off by default: %v", err)
	}
	if err := SetSetting(tenantID, "loyalty.points_transfer", "true", "tester"); err != nil {
		t.Fatal(err)
	}
	// Pending and expired points cannot move: 301 > 300 usable and unexpired.
	if _, err := RequestLoyaltyPointsTransfer(tenantID, "CUST-TA", "CUST-TB", 301, "mgr"); code(err) != "CUSTOM-0622" {
		t.Errorf("pending and expired points are refused: %v", err)
	}
	if _, err := RequestLoyaltyPointsTransfer(tenantID, "CUST-TA", "CUST-TA", 10, "mgr"); code(err) != "CUSTOM-0621" {
		t.Errorf("same customer refused: %v", err)
	}

	// 150 points: moves once the sender's code is right; expiry dates follow.
	tr, err := RequestLoyaltyPointsTransfer(tenantID, "CUST-TA", "CUST-TB", 150, "mgr")
	if err != nil || tr.Status != "Awaiting OTP" {
		t.Fatalf("request: %v %+v", err, tr)
	}
	if bal("CUST-TB") != 0 {
		t.Fatal("nothing moves before the code")
	}
	if _, err := ConfirmLoyaltyPointsTransfer(tenantID, tr.ID, "000000", "mgr", "Store Manager"); code(err) != "CUSTOM-0624" {
		t.Errorf("wrong code refused: %v", err)
	}
	otp := approve(tr.ID)
	done, err := ConfirmLoyaltyPointsTransfer(tenantID, tr.ID, otp, "mgr", "Store Manager")
	if err != nil || done.Status != "Posted" {
		t.Fatalf("confirm: %v %+v", err, done)
	}
	if bal("CUST-TA") != startA-150 || bal("CUST-TB") != 150 {
		t.Errorf("balances A %d (want %d) B %d (want 150)", bal("CUST-TA"), startA-150, bal("CUST-TB"))
	}
	rows, err := db.DB.Query("SELECT points, expires_at FROM " + schemaName + ".loyalty_point_ledger WHERE customer_id = 'CUST-TB' ORDER BY expires_at")
	if err != nil {
		t.Fatal(err)
	}
	var got [][2]interface{}
	for rows.Next() {
		var p int
		var e time.Time
		_ = rows.Scan(&p, &e)
		got = append(got, [2]interface{}{p, e.Format("2006-01-02")})
	}
	rows.Close()
	if len(got) != 2 || got[0][0] != 100 || got[0][1] != soon.Format("2006-01-02") || got[1][0] != 50 || got[1][1] != late.Format("2006-01-02") {
		t.Errorf("target rows keep the source expiry, earliest first: %v", got)
	}
	// A code is used once.
	if _, err := ConfirmLoyaltyPointsTransfer(tenantID, tr.ID, otp, "mgr", "Store Manager"); err == nil {
		t.Error("a posted transfer cannot be confirmed again")
	}

	// Daily cap: 150 sent already, cap 200 -> 100 more is over.
	if err := SetSetting(tenantID, "loyalty.points_transfer_daily_cap", "200", "tester"); err != nil {
		t.Fatal(err)
	}
	if _, err := RequestLoyaltyPointsTransfer(tenantID, "CUST-TA", "CUST-TB", 100, "mgr"); code(err) != "CUSTOM-0623" {
		t.Errorf("daily cap enforced: %v", err)
	}
	if _, err := RequestLoyaltyPointsTransfer(tenantID, "CUST-TA", "CUST-TB", 50, "mgr"); err != nil {
		t.Errorf("up to the cap is allowed: %v", err)
	}

	// Above the approval value (500) it waits for a manager.
	if err := SetSetting(tenantID, "loyalty.points_transfer_daily_cap", "0", "tester"); err != nil {
		t.Fatal(err)
	}
	seed("CUST-TA", 600, nil)
	before := bal("CUST-TA")
	big, err := RequestLoyaltyPointsTransfer(tenantID, "CUST-TA", "CUST-TB", 500, "mgr")
	if err != nil {
		t.Fatal(err)
	}
	waiting, err := ConfirmLoyaltyPointsTransfer(tenantID, big.ID, approve(big.ID), "mgr", "Store Manager")
	if err != nil || waiting.Status != "Pending Approval" || waiting.RequiredRole != "Store Manager" {
		t.Fatalf("500 points waits for approval: %v %+v", err, waiting)
	}
	if bal("CUST-TA") != before || bal("CUST-TB") != 150 {
		t.Errorf("nothing moves before approval: A %d (want %d) B %d", bal("CUST-TA"), before, bal("CUST-TB"))
	}
	if err := DecideApproval(tenantID, loyaltyTransferDoctype, big.ID, "mgr-2", "Store Manager", "", "Approved", "ok"); err != nil {
		t.Fatalf("approve: %v", err)
	}
	if bal("CUST-TA") != before-500 || bal("CUST-TB") != 650 {
		t.Errorf("approved transfer moves: A %d (want %d) B %d (want 650)", bal("CUST-TA"), before-500, bal("CUST-TB"))
	}
}
