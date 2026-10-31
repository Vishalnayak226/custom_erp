package engines

import (
	"custom_erp/db"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"
)

// Stage 49.10.6b (1): several records just under an approval limit, by one
// user on one day, show on the threshold-splitting report - and records
// clear of the limit, over it, or spread over days and people do not.
func TestStage49106bThresholdSplittingReport(t *testing.T) {
	db.InitDB(testConnStr())
	suffix := fmt.Sprintf("%d", time.Now().UnixNano()%1_000_000_000)
	doctype := "PurchaseOrder"
	splitter, other := "split"+suffix, "other"+suffix
	var limit float64
	if err := db.DB.QueryRow(`SELECT COALESCE(MIN(min_amount), 0) FROM tenant_default.approval_rules WHERE doctype = $1 AND min_amount > 0`, doctype).Scan(&limit); err != nil || limit <= 0 {
		t.Skipf("no purchase order approval slab above zero on this database (%v)", err)
	}
	for _, u := range []string{splitter, other} {
		if _, err := db.DB.Exec(`INSERT INTO tenant_default.users (id, username, password_hash, email, role, status) VALUES ($1, $1, 'x', $2, 'Store Manager', 'Active')`, u, u+"@test.local"); err != nil {
			t.Fatalf("user %s: %v", u, err)
		}
	}
	var ids []string
	defer func() {
		db.DB.Exec(`DELETE FROM tenant_default.documents WHERE id = ANY($1)`, pqStringArray(ids))
		db.DB.Exec(`DELETE FROM tenant_default.users WHERE id = ANY($1)`, pqStringArray([]string{splitter, other}))
	}()
	add := func(by string, amount float64, daysAgo int, extra map[string]interface{}) string {
		t.Helper()
		id := fmt.Sprintf("T49106B-%s-%d", suffix, len(ids))
		data := map[string]interface{}{"code": id, "total_amount": amount, "location": "STORE-SPLIT"}
		for k, v := range extra {
			data[k] = v
		}
		raw, _ := json.Marshal(data)
		if _, err := db.DB.Exec(`INSERT INTO tenant_default.documents (id, doctype, data, status, created_by, created_at)
			VALUES ($1, $2, $3, 'Approved', $4, LOCALTIMESTAMP - make_interval(days => $5))`, id, doctype, raw, by, daysAgo); err != nil {
			t.Fatalf("seed %s: %v", id, err)
		}
		ids = append(ids, id)
		return id
	}
	a := add(splitter, limit*0.95, 0, nil)
	b := add(splitter, limit*0.98, 0, nil)
	add(splitter, limit*0.5, 0, nil)                                                               // well under: not part of it
	add(splitter, limit, 0, nil)                                                                   // at the limit: it was approved as such
	add(splitter, limit*0.97, 3, nil)                                                              // another day
	add(other, limit*0.96, 0, nil)                                                                 // another person, alone
	add(splitter, limit*0.99, 0, map[string]interface{}{"total_amount": fmt.Sprint(limit * 0.99)}) // not a number

	find := func(params map[string]string) map[string]interface{} {
		t.Helper()
		rows, err := thresholdSplittingRows("default", params)
		if err != nil {
			t.Fatalf("report %v: %v", params, err)
		}
		var hit map[string]interface{}
		for _, r := range rows {
			if r["user"] == splitter || r["user"] == other {
				if hit != nil {
					t.Fatalf("more than one row for the seeded records: %v and %v", hit, r)
				}
				hit = r
			}
		}
		return hit
	}
	row := find(map[string]string{})
	if row == nil {
		t.Fatal("two purchase orders just under the limit by one user in a day did not show")
	}
	if row["user"] != splitter || row["doctype"] != doctype || row["count"] != 2 || row["limit"] != limit ||
		row["over_limit"] != "Yes" || row["documents"] != a+", "+b || row["location"] != "STORE-SPLIT" {
		t.Errorf("row = %v, want %s's two records %s, %s, together over the %.0f limit", row, splitter, a, b, limit)
	}
	if row := find(map[string]string{"min_count": "3"}); row != nil {
		t.Errorf("asked for three or more a day, got %v", row)
	}
	if row := find(map[string]string{"band_pct": "3"}); row != nil {
		t.Errorf("within 3%% of the limit only one record qualifies, got %v", row)
	}
	today := time.Now().Format("2006-01-02")
	if row := find(map[string]string{"from": "2020-01-01", "to": "2020-01-31"}); row != nil {
		t.Errorf("a range without the day still showed %v", row)
	}
	if row := find(map[string]string{"from": today, "to": today}); row == nil {
		t.Error("the day itself, asked for, did not show")
	}
	var ve *ValidationError
	if _, err := thresholdSplittingRows("default", map[string]string{"from": "yesterday"}); !errors.As(err, &ve) {
		t.Errorf("a from that is not a date: %v", err)
	}

	def, ok := reportRegistry["threshold-splitting"]
	if !ok || def.ModuleKey != "finance" || ReportAllowedForRole(def, RoleCashier) || !ReportAllowedForRole(def, RoleAdmin) {
		t.Errorf("the report must be a finance report an admin can run and a cashier cannot: %+v", def.ModuleKey)
	}
}

// Stage 49.10.6b (2): a client provisioned from now on starts with
// self-approval off - whoever submits a document never decides it - while a
// client that never chose keeps the shipped default, so no live client moves.
func TestStage49106bSelfApprovalOffForNewClients(t *testing.T) {
	db.InitDB(testConnStr())
	if d := settingsRegistry[SettingSelfApproval].Default; d != SelfApprovalAnyone {
		t.Fatalf("the registered default is %q - every existing client that never chose would change", d)
	}
	tenantID, schema := uniqueLifecycleTenant(t)
	defer dropLifecycleTenant(tenantID, schema)
	if _, err := ProvisionTenantSchema(tenantID, schema, "0.1.0-test"); err != nil {
		t.Fatalf("provision: %v", err)
	}
	if got := GetSettingString(tenantID, SettingSelfApproval); got != SelfApprovalNobody {
		t.Fatalf("a new client's self-approval = %q, want %q", got, SelfApprovalNobody)
	}

	// Its admin submits a budget and cannot approve it.
	raw, _ := json.Marshal(map[string]interface{}{"code": "BUD-6B", "total_amount": 1000, "status": "Draft"})
	if _, err := db.DB.Exec(`INSERT INTO `+schema+`.documents (id, doctype, data, status, created_by) VALUES ('BUD-6B', 'Budget', $1, 'Draft', 'admin')`, raw); err != nil {
		t.Fatal(err)
	}
	if err := SubmitForApproval(tenantID, "Budget", "BUD-6B", "admin", RoleAdmin); err != nil {
		t.Fatalf("submit: %v", err)
	}
	err := DecideApproval(tenantID, "Budget", "BUD-6B", "admin", RoleAdmin, "", "Approved", "")
	var ve *ValidationError
	if !errors.As(err, &ve) || ve.Code != "APPROV-0840" {
		t.Fatalf("a new client's admin approving their own budget: %v, want APPROV-0840", err)
	}

	// The client can still choose otherwise.
	if err := SetSetting(tenantID, SettingSelfApproval, SelfApprovalAnyone, "admin"); err != nil {
		t.Fatal(err)
	}
	if !SubmitterMayDecideOwn(tenantID, "Budget", RoleAdmin) {
		t.Error("after choosing Anyone the submitter still may not decide")
	}

	// A client that never saved a choice reads the shipped default.
	var saved int
	db.DB.QueryRow(`SELECT COUNT(*) FROM tenant_default.system_settings WHERE key = $1`, SettingSelfApproval).Scan(&saved)
	if saved == 0 && !strings.EqualFold(GetSettingString("default", SettingSelfApproval), SelfApprovalAnyone) {
		t.Errorf("an existing client with no saved choice reads %q", GetSettingString("default", SettingSelfApproval))
	}
}
