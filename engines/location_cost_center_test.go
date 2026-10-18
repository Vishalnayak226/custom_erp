package engines

import (
	"custom_erp/db"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"
)

// Stage 93: a cost center for every location, and postings tagged with it.
func TestLocationCostCenterMadeLinkedAndPosted(t *testing.T) {
	db.InitDB(testConnStr())
	schema, err := db.GetTenantSchema("default")
	if err != nil {
		t.Fatal(err)
	}
	preserveSeries(t, schema, "CostCenter")
	tag := fmt.Sprintf("T93%d", time.Now().UnixNano()%1_000_000_000)
	var made []string // cost centers this test created, removed at the end
	t.Cleanup(func() {
		_, _ = db.DB.Exec("DELETE FROM "+schema+".gl_postings WHERE document_id LIKE $1", tag+"%")
		_, _ = db.DB.Exec("DELETE FROM "+schema+".documents WHERE id LIKE $1", tag+"%")
		for _, id := range made {
			_, _ = db.DB.Exec("DELETE FROM "+schema+".documents WHERE doctype = 'CostCenter' AND id = $1", id)
		}
	})
	insertDoc := func(id, doctype string, data map[string]interface{}) {
		t.Helper()
		raw, _ := json.Marshal(data)
		status, _ := data["status"].(string)
		if status == "" {
			status = "Active"
		}
		if _, err := db.DB.Exec("INSERT INTO "+schema+".documents (id, doctype, data, status, created_by) VALUES ($1, $2, $3, $4, 'system')", id, doctype, raw, status); err != nil {
			t.Fatalf("insert %s %s: %v", doctype, id, err)
		}
	}
	ccName := func(id string) (name, status string) {
		_ = db.DB.QueryRow("SELECT data->>'name', status FROM "+schema+".documents WHERE doctype = 'CostCenter' AND id = $1", id).Scan(&name, &status)
		return
	}

	// 93.1 - a new location with no cost center gets one: its own name, a
	// short master code, Active.
	storeName := tag + " Jayanagar"
	store := map[string]interface{}{"code": tag + "-S1", "name": storeName, "type": "Store", "status": "Active"}
	if err := PrepareLocationCostCenter("default", tag+"-S1", "system", store); err != nil {
		t.Fatal(err)
	}
	cc1, _ := store["cost_center"].(string)
	made = append(made, cc1)
	if !shortMasterCode.MatchString(cc1) {
		t.Errorf("the new cost center's code %q should be a short master code (CC00001), not a long series number", cc1)
	}
	if name, status := ccName(cc1); name != storeName || status != "Active" {
		t.Errorf("cost center %s = %q / %s, want the location's name, Active", cc1, name, status)
	}
	insertDoc(tag+"-S1", "Location", store)

	// A cost center the user picked is kept.
	picked := map[string]interface{}{"name": tag + " Picked", "cost_center": cc1}
	if err := PrepareLocationCostCenter("default", tag+"-S9", "system", picked); err != nil || picked["cost_center"] != cc1 {
		t.Errorf("a picked cost center must be kept: %v (%v)", picked["cost_center"], err)
	}

	// An Active, unlinked cost center of the same name is linked, not
	// duplicated (the user's existing "JP Nagar"); one already linked to
	// another location is not shared.
	insertDoc(tag+"-CCJP", "CostCenter", map[string]interface{}{"code": tag + "-CCJP", "name": tag + " jp nagar", "status": "Active"})
	jp := map[string]interface{}{"code": tag + "-S2", "name": tag + " JP Nagar", "type": "Store", "status": "Active"}
	if err := PrepareLocationCostCenter("default", tag+"-S2", "system", jp); err != nil || jp["cost_center"] != tag+"-CCJP" {
		t.Errorf("a same-name cost center should be linked: got %v (%v)", jp["cost_center"], err)
	}
	insertDoc(tag+"-S2", "Location", jp)
	twin := map[string]interface{}{"code": tag + "-S3", "name": tag + " JP Nagar", "type": "Warehouse", "status": "Active"}
	if err := PrepareLocationCostCenter("default", tag+"-S3", "system", twin); err != nil {
		t.Fatal(err)
	}
	made = append(made, twin["cost_center"].(string))
	if twin["cost_center"] == tag+"-CCJP" {
		t.Errorf("a cost center already linked to another location must not be shared by a new one")
	}

	// 93.2 / 93.6 - a saved location with no cost center - never had the key,
	// or saved blank - is backfilled; a location's cost center is never blank.
	insertDoc(tag+"-S4", "Location", map[string]interface{}{"code": tag + "-S4", "name": tag + " HO", "type": "HO", "status": "Active"})
	insertDoc(tag+"-S5", "Location", map[string]interface{}{"code": tag + "-S5", "name": tag + " Cleared", "type": "Store", "status": "Active", "cost_center": ""})
	var pending []string
	rows, err := db.DB.Query("SELECT id FROM "+schema+".documents WHERE doctype = 'Location' AND id LIKE $1 AND COALESCE(data->>'cost_center', '') = '' ORDER BY id", tag+"%")
	if err != nil {
		t.Fatal(err)
	}
	for rows.Next() {
		var id string
		_ = rows.Scan(&id)
		pending = append(pending, id)
	}
	rows.Close()
	if strings.Join(pending, ",") != tag+"-S4,"+tag+"-S5" {
		t.Errorf("both locations without a cost center are backfilled, got %v", pending)
	}
	cc5, err := EnsureLocationCostCenter("default", tag+"-S5", "system")
	if err != nil || cc5 == "" {
		t.Fatalf("a cleared cost center is refilled: %q (%v)", cc5, err)
	}
	made = append(made, cc5)
	// An edit that clears it is filled again before the save.
	edited := map[string]interface{}{"code": tag + "-S5", "name": tag + " Cleared", "cost_center": ""}
	if err := PrepareLocationCostCenter("default", tag+"-S5", "system", edited); err != nil || edited["cost_center"] != cc5 {
		t.Errorf("an edit clearing the cost center should get its own back (%s), got %v (%v)", cc5, edited["cost_center"], err)
	}
	cc4, err := EnsureLocationCostCenter("default", tag+"-S4", "system")
	if err != nil {
		t.Fatal(err)
	}
	made = append(made, cc4)
	var stored string
	_ = db.DB.QueryRow("SELECT data->>'cost_center' FROM "+schema+".documents WHERE id = $1", tag+"-S4").Scan(&stored)
	if stored != cc4 || cc4 == "" {
		t.Errorf("the backfilled location should carry %q, has %q", cc4, stored)
	}
	if again, _ := EnsureLocationCostCenter("default", tag+"-S4", "system"); again != cc4 {
		t.Errorf("a second run must leave the location as it is: %q then %q", cc4, again)
	}

	// 93.3 - a posting with no cost center takes its document's location's,
	// directly or through its parent; an explicit one is kept; an Inactive
	// one leaves the posting untagged and never refuses it.
	insertDoc(tag+"-INV", "SalesInvoice", map[string]interface{}{"location": tag + "-S1"})
	insertDoc(tag+"-CN", "CreditNote", map[string]interface{}{"sales_invoice_id": tag + "-INV"})
	insertDoc(tag+"-BYCODE", "SalesInvoice", map[string]interface{}{"store": tag + "-S2"})
	post := func(docType, docID string, opts ...PostingOptions) string {
		t.Helper()
		if err := PostDoubleEntry("default", docType, docID, map[string]int64{"1300": 1000}, map[string]int64{"4100": 1000}, "", "", opts...); err != nil {
			t.Fatalf("post %s: %v", docID, err)
		}
		var cc *string
		if err := db.DB.QueryRow("SELECT cost_center FROM "+schema+".gl_postings WHERE document_id = $1 LIMIT 1", docID).Scan(&cc); err != nil {
			t.Fatalf("read the posting for %s: %v", docID, err)
		}
		if cc == nil {
			return ""
		}
		return *cc
	}
	if got := post("SalesInvoice", tag+"-INV"); got != cc1 {
		t.Errorf("an invoice posting should carry its store's cost center %q, got %q", cc1, got)
	}
	if got := post("CreditNote", tag+"-CN"); got != cc1 {
		t.Errorf("a credit note should carry its invoice's store's cost center %q, got %q", cc1, got)
	}
	if got := post("SalesInvoice", tag+"-BYCODE"); got != tag+"-CCJP" {
		t.Errorf("a document naming its store under 'store' should be tagged %q, got %q", tag+"-CCJP", got)
	}
	// The Cost Center P&L names each short code.
	today := time.Now().Format("2006-01-02")
	pl, err := GetCostCenterPL("default", today, today)
	if err != nil {
		t.Fatal(err)
	}
	named := false
	for _, row := range pl {
		if row["cost_center"] == tag+"-CCJP" && row["cost_center_name"] == tag+" jp nagar" {
			named = true
		}
	}
	if !named {
		t.Errorf("the Cost Center P&L should name %s %q", tag+"-CCJP", tag+" jp nagar")
	}
	insertDoc(tag+"-JV", "JournalVoucher", map[string]interface{}{"location": tag + "-S1"})
	if got := post("JournalVoucher", tag+"-JV", PostingOptions{CostCenter: tag + "-CCJP"}); got != tag+"-CCJP" {
		t.Errorf("an explicit cost center must win over the location's, got %q", got)
	}
	if _, err := db.DB.Exec("UPDATE "+schema+".documents SET status = 'Inactive' WHERE id = $1", cc1); err != nil {
		t.Fatal(err)
	}
	insertDoc(tag+"-INV2", "SalesInvoice", map[string]interface{}{"location": tag + "-S1"})
	if got := post("SalesInvoice", tag+"-INV2"); got != "" {
		t.Errorf("an Inactive cost center should leave the posting untagged, got %q", got)
	}
	insertDoc(tag+"-NOLOC", "SalesInvoice", map[string]interface{}{"customer_id": "x"})
	if got := post("SalesInvoice", tag+"-NOLOC"); got != "" {
		t.Errorf("a document with no location stays untagged, got %q", got)
	}
}

// Stage 93.7 / 93.9 / 93.10 / 93.11: postings carry the entity and the
// department, a person's department is picked and follows HR, and an old
// long code is cleaned with every reference.
func TestPostingEntityDepartmentAndCodeCleanup(t *testing.T) {
	db.InitDB(testConnStr())
	schema, err := db.GetTenantSchema("default")
	if err != nil {
		t.Fatal(err)
	}
	// The shared dev database may not have run this stage's migration yet
	// (users.department, master_code_changes); a migrated one always has.
	var migrated bool
	_ = db.DB.QueryRow(`SELECT EXISTS(SELECT 1 FROM information_schema.columns WHERE table_schema = $1 AND table_name = 'users' AND column_name = 'department')
		AND EXISTS(SELECT 1 FROM information_schema.tables WHERE table_schema = $1 AND table_name = 'master_code_changes')`, schema).Scan(&migrated)
	if !migrated {
		t.Skip("needs migrations_stage93_location_cost_center.sql - run erp-server -migrate on this database")
	}
	tag := fmt.Sprintf("T93B%d", time.Now().UnixNano()%1_000_000_000)
	user := tag + "-user"
	t.Cleanup(func() {
		_, _ = db.DB.Exec("DELETE FROM "+schema+".gl_postings WHERE document_id LIKE $1", tag+"%")
		_, _ = db.DB.Exec("DELETE FROM "+schema+".documents WHERE id LIKE $1 OR data->>'code' LIKE $1", tag+"%")
		_, _ = db.DB.Exec("DELETE FROM "+schema+".users WHERE id = $1", user)
		_, _ = db.DB.Exec("DELETE FROM "+schema+".master_code_changes WHERE old_code LIKE $1", tag+"%")
	})
	insertDoc := func(id, doctype, createdBy string, data map[string]interface{}) {
		t.Helper()
		raw, _ := json.Marshal(data)
		if _, err := db.DB.Exec("INSERT INTO "+schema+".documents (id, doctype, data, status, created_by) VALUES ($1, $2, $3, 'Active', $4)", id, doctype, raw, createdBy); err != nil {
			t.Fatalf("insert %s %s: %v", doctype, id, err)
		}
	}
	insertDoc(tag+"-LE", "LegalEntity", "system", map[string]interface{}{"code": tag + "-LE", "name": "Entity", "gstin": "29AABCU9603R1ZJ"})
	insertDoc(tag+"-CC", "CostCenter", "system", map[string]interface{}{"code": tag + "-CC", "name": "Store CC"})
	insertDoc(tag+"-SALES", "Department", "system", map[string]interface{}{"code": tag + "-SALES", "name": tag + " Sales"})
	insertDoc(tag+"-ACCTS", "Department", "system", map[string]interface{}{"code": tag + "-ACCTS", "name": tag + " Accounts"})
	insertDoc(tag+"-LOC", "Location", "system", map[string]interface{}{"code": tag + "-LOC", "name": "Store", "legal_entity": tag + "-LE", "cost_center": tag + "-CC"})
	if _, err := db.DB.Exec("INSERT INTO "+schema+".users (id, username, password_hash, role, status, department) VALUES ($1, $1, 'x', 'Cashier', 'Active', $2)", user, tag+"-SALES"); err != nil {
		t.Fatalf("seed user: %v", err)
	}
	type dims struct{ cc, entity, dept string }
	str := func(p *string) string {
		if p == nil {
			return ""
		}
		return *p
	}
	post := func(docType, docID string) dims {
		t.Helper()
		if err := PostDoubleEntry("default", docType, docID, map[string]int64{"1300": 500}, map[string]int64{"4100": 500}, "", ""); err != nil {
			t.Fatalf("post %s: %v", docID, err)
		}
		var cc, entity, dept *string
		if err := db.DB.QueryRow("SELECT cost_center, entity, department FROM "+schema+".gl_postings WHERE document_id = $1 LIMIT 1", docID).Scan(&cc, &entity, &dept); err != nil {
			t.Fatalf("read posting %s: %v", docID, err)
		}
		return dims{str(cc), str(entity), str(dept)}
	}

	// 93.7 + 93.10: the store's entity; the department of the cashier who raised it.
	insertDoc(tag+"-CART", "POSCart", user, map[string]interface{}{"location": tag + "-LOC"})
	if got := post("POSCart", tag+"-CART"); got != (dims{tag + "-CC", tag + "-LE", tag + "-SALES"}) {
		t.Errorf("a sale should carry its store's cost center and entity and the cashier's department, got %+v", got)
	}
	// The document's own department wins over the creator's.
	insertDoc(tag+"-EXP", "ExpenseClaim", user, map[string]interface{}{"location": tag + "-LOC", "department": tag + "-ACCTS"})
	if got := post("ExpenseClaim", tag+"-EXP"); got.dept != tag+"-ACCTS" {
		t.Errorf("a claim's own department should win, got %q", got.dept)
	}
	// A payslip takes its employee's store and department, through the parent.
	insertDoc(tag+"-EMP", "Employee", "system", map[string]interface{}{"code": tag + "-EMP", "name": "E", "department": tag + "-ACCTS", "location": tag + "-LOC"})
	insertDoc(tag+"-PAY", "Payslip", user, map[string]interface{}{"employee_id": tag + "-EMP"})
	if got := post("Payslip", tag+"-PAY"); got != (dims{tag + "-CC", tag + "-LE", tag + "-ACCTS"}) {
		t.Errorf("a payslip should carry its employee's store and department, got %+v", got)
	}
	// A journal voucher is booked by accounts for anyone: no creator department.
	insertDoc(tag+"-JV", "JournalVoucher", user, map[string]interface{}{})
	if got := post("JournalVoucher", tag+"-JV"); got.dept != "" {
		t.Errorf("a journal voucher must not take its creator's department, got %q", got.dept)
	}

	// 93.9: a department is picked from the list (id, code or name), Active only.
	if id, err := ResolveDepartmentReference("default", tag+" sales"); err != nil || id != tag+"-SALES" {
		t.Errorf("a department named the way it was typed resolves to its record: %q (%v)", id, err)
	}
	if _, err := ResolveDepartmentReference("default", tag+" Nowhere"); err == nil {
		t.Errorf("a department that is not on the list must be refused")
	}
	// A user with no employee: set here. Linked to an employee: HR leads.
	if dept, err := SetUserDepartment("default", user, tag+"-ACCTS"); err != nil || dept != tag+"-ACCTS" {
		t.Errorf("setting an unlinked user's department: %q (%v)", dept, err)
	}
	if _, err := db.DB.Exec("UPDATE "+schema+".documents SET data = data || jsonb_build_object('user_id', $2::text) WHERE id = $1", tag+"-EMP", user); err != nil {
		t.Fatal(err)
	}
	if _, err := SetUserDepartment("default", user, tag+"-SALES"); err == nil {
		t.Errorf("a user linked to an employee must take the department from HR")
	}
	if err := SyncUserDepartmentFromEmployee("default", user, tag+"-SALES"); err != nil {
		t.Fatal(err)
	}
	var userDept string
	_ = db.DB.QueryRow("SELECT department FROM "+schema+".users WHERE id = $1", user).Scan(&userDept)
	if userDept != tag+"-SALES" {
		t.Errorf("the employee's department should reach the user, user has %q", userDept)
	}

	// 93.11: an old long code renumbered, with every reference - a JSON field,
	// a GL dimension, a user column - and text that merely contains it kept.
	oldDept := tag + "/HQ/2026/000001"
	insertDoc(oldDept, "Department", "system", map[string]interface{}{"code": oldDept, "name": tag + " Purchase"})
	insertDoc(tag+"-JV2", "JournalVoucher", user, map[string]interface{}{"department": oldDept, "lines": []map[string]interface{}{{"note": oldDept + " is not a whole value"}}})
	if err := PostDoubleEntry("default", "JournalVoucher", tag+"-JV2", map[string]int64{"1300": 100}, map[string]int64{"4100": 100}, "", "", PostingOptions{Department: oldDept}); err != nil {
		t.Fatal(err)
	}
	if _, err := db.DB.Exec("UPDATE "+schema+".users SET department = $1 WHERE id = $2", oldDept, user); err != nil {
		t.Fatal(err)
	}
	plan, err := PlanMasterRecode("default", "Department", nil)
	if err != nil {
		t.Fatal(err)
	}
	listed := false
	for _, r := range plan.Rows {
		if r.Old == oldDept && r.New != "" && !strings.Contains(r.New, "/") {
			listed = true
		}
	}
	if !listed {
		t.Errorf("the preview should list %s with a short new code: %+v", oldDept, plan.Rows)
	}
	// The rewrite itself, on this test's record only (Apply would also clean
	// any other long-coded department the shared test tenant holds).
	newDept := tag + "-D1"
	tx, err := db.DB.Begin()
	if err != nil {
		t.Fatal(err)
	}
	if err := recodeInSchemaTx(tx, schema, "Department", []MasterRecodeRow{{Old: oldDept, New: newDept}}, "system"); err != nil {
		tx.Rollback()
		t.Fatal(err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	var code, jvDept, note, glDept string
	_ = db.DB.QueryRow("SELECT data->>'code' FROM "+schema+".documents WHERE id = $1 AND doctype = 'Department'", newDept).Scan(&code)
	_ = db.DB.QueryRow("SELECT data->>'department', data->'lines'->0->>'note' FROM "+schema+".documents WHERE id = $1", tag+"-JV2").Scan(&jvDept, &note)
	_ = db.DB.QueryRow("SELECT department FROM "+schema+".gl_postings WHERE document_id = $1 LIMIT 1", tag+"-JV2").Scan(&glDept)
	_ = db.DB.QueryRow("SELECT department FROM "+schema+".users WHERE id = $1", user).Scan(&userDept)
	if code != newDept || jvDept != newDept || glDept != newDept || userDept != newDept {
		t.Errorf("every whole-value reference should move to %s: record code %q, voucher %q, GL %q, user %q", newDept, code, jvDept, glDept, userDept)
	}
	if note != oldDept+" is not a whole value" {
		t.Errorf("text that only contains the old code must be left alone, got %q", note)
	}
	var stillOld int
	_ = db.DB.QueryRow("SELECT COUNT(*) FROM "+schema+".documents WHERE id = $1", oldDept).Scan(&stillOld)
	if stillOld != 0 {
		t.Errorf("the old id must be gone")
	}
	var recorded string
	_ = db.DB.QueryRow("SELECT new_code FROM "+schema+".master_code_changes WHERE doc_type = 'Department' AND old_code = $1", oldDept).Scan(&recorded)
	if recorded != newDept {
		t.Errorf("the change should be recorded, got %q", recorded)
	}
	// A store code cleaned the same way still resolves under its old code.
	oldLoc := tag + "/HQ/2026/000009"
	insertDoc(tag+"-L2", "Location", "system", map[string]interface{}{"code": tag + "-L2", "name": tag + " Renamed store"})
	if _, err := db.DB.Exec("INSERT INTO "+schema+".master_code_changes (doc_type, old_code, new_code, changed_by) VALUES ('Location', $1, $2, 'system')", oldLoc, tag+"-L2"); err != nil {
		t.Fatal(err)
	}
	if id, err := ResolveLocationReference("default", oldLoc); err != nil || id != tag+"-L2" {
		t.Errorf("an old store code should resolve to its new one: %q (%v)", id, err)
	}
}
