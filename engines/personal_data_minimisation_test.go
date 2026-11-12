package engines

import (
	"custom_erp/db"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"
)

// Stage 49.6.2a - the privacy owner's answers to the 49.6.2 register's
// minimisation questions (2026-10-29), held to the code.

// Customer birthday, anniversary and gender are marketing data held on
// consent, and PAN is asked for only on a high-value sale: none of the four is
// required by the record type, and every register question has its answer.
func TestStage4962aMinimisationAnswers(t *testing.T) {
	db.InitDB(testConnStr())
	for _, field := range []string{"date_of_birth", "date_of_anniversary", "gender", "pan"} {
		var mandatory bool
		if err := db.DB.QueryRow(`SELECT mandatory FROM tenant_default.doctype_fields WHERE doctype_name = 'Customer' AND fieldname = $1`, field).Scan(&mandatory); err != nil {
			t.Fatalf("Customer.%s: %v", field, err)
		}
		if mandatory {
			t.Errorf("Customer.%s is required - 49.6.2a keeps it optional", field)
		}
		row, ok := LookupPersonalDataField("Customer", field)
		if !ok {
			t.Fatalf("Customer.%s has no register row", field)
		}
		if field != "pan" && (row.LegalBasis != BasisConsent || !strings.HasPrefix(row.Purpose, "Marketing")) {
			t.Errorf("Customer.%s: basis %q, purpose %q - want marketing data held on consent", field, row.LegalBasis, row.Purpose)
		}
	}
	if row, _ := LookupPersonalDataField("Customer", "pan"); !strings.Contains(row.Purpose, "114B") {
		t.Errorf("Customer.pan purpose %q does not say it is asked for only over the Rule 114B threshold", row.Purpose)
	}
	if row, _ := LookupPersonalDataField("Employee", "designation"); !strings.Contains(row.Purpose, "HR letters") {
		t.Errorf("Employee.designation purpose %q - kept for HR letters", row.Purpose)
	}
	if q := PersonalDataReviewQuestions(); len(q) > 0 {
		t.Errorf("register questions still open: %v", q)
	}
	for _, c := range personalFieldClearings {
		row, ok := LookupPersonalDataField(c.Doctype, c.Field)
		if !ok || !strings.Contains(row.Retention, fmt.Sprintf("%d days", c.Days)) {
			t.Errorf("%s.%s is cleared after %d days but its register row says %q", c.Doctype, c.Field, c.Days, row.Retention)
		}
	}
}

// PAN is asked for on a sale over Rs 2,00,000 and not at or below it; Form 97
// stands in for a buyer with none.
func TestStage4962aPANOnlyOverTheThreshold(t *testing.T) {
	db.InitDB(testConnStr())
	const tenantID = "default"
	if p := HighValuePolicyFor(tenantID); p.Country != "IN" || p.PANAbove != 200000 || p.CashLimit != 200000 {
		t.Skipf("this database's high-value policy is not India's default: %+v", p)
	}
	id := fmt.Sprintf("T4962A-%d", time.Now().UnixNano())
	data, _ := json.Marshal(map[string]interface{}{"code": id, "name": "Threshold Buyer", "phone": "9876500001",
		"address": "1 Main Road", "city": "Bengaluru", "state": "Karnataka", "pincode": "560001"})
	if _, err := db.DB.Exec(`INSERT INTO tenant_default.documents (id, doctype, data, status, created_by) VALUES ($1, 'Customer', $2, 'Active', 'admin')`, id, data); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.DB.Exec(`DELETE FROM tenant_default.documents WHERE id = $1`, id) })

	if err := CheckHighValueSale(tenantID, id, 200000, 0, false); err != nil {
		t.Errorf("a bill of exactly Rs 2,00,000 asked for the PAN: %v", err)
	}
	if got := sodCode(CheckHighValueSale(tenantID, id, 200000.01, 0, false)); got != "POSOFF-0867" {
		t.Errorf("a bill over Rs 2,00,000 with no PAN: %q, want POSOFF-0867", got)
	}
	if err := CheckHighValueSale(tenantID, id, 250000, 0, true); err != nil {
		t.Errorf("a signed Form 97 in place of the PAN: %v", err)
	}
	if _, err := db.DB.Exec(`UPDATE tenant_default.documents SET data = data || '{"pan":"ABCDE1234F"}'::jsonb WHERE id = $1`, id); err != nil {
		t.Fatal(err)
	}
	if err := CheckHighValueSale(tenantID, id, 250000, 0, false); err != nil {
		t.Errorf("a buyer with a PAN on file: %v", err)
	}
}

// A gate pass's driver phone is cleared 90 days after the gate pass, by the
// hourly retention sweep; the gate pass and the rest of it stay, a recent one
// keeps its phone, and a client under legal hold keeps everything.
func TestStage4962aGatePassDriverPhoneClearedAfter90Days(t *testing.T) {
	db.InitDB(testConnStr())
	tenantID, schema := uniqueLifecycleTenant(t)
	t.Cleanup(func() { dropLifecycleTenant(tenantID, schema) })
	if _, err := ProvisionTenantSchema(tenantID, schema, "0.1.0-test"); err != nil {
		t.Fatalf("ProvisionTenantSchema: %v", err)
	}
	insert := func(id string, ageDays int, phone string) {
		data, _ := json.Marshal(map[string]interface{}{"code": id, "location_code": "HO", "driver_name": "Ravi", "driver_phone": phone,
			"vehicle_number": "KA01AB1234", "status": "Completed"})
		if _, err := db.DB.Exec(fmt.Sprintf(`INSERT INTO %s.documents (id, doctype, data, status, created_by, created_at)
			VALUES ($1, 'GatePass', $2, 'Completed', 'admin', CURRENT_TIMESTAMP - make_interval(days => $3))`, schema), id, data, ageDays); err != nil {
			t.Fatal(err)
		}
	}
	insert("GP-OLD", 91, "9876500002")
	insert("GP-NEW", 10, "9876500003")
	stored := func(id string) map[string]interface{} {
		var raw []byte
		if err := db.DB.QueryRow(fmt.Sprintf(`SELECT data FROM %s.documents WHERE id = $1`, schema), id).Scan(&raw); err != nil {
			t.Fatal(err)
		}
		out := map[string]interface{}{}
		_ = json.Unmarshal(raw, &out)
		return out
	}

	db.DB.Exec(`UPDATE public.tenants SET legal_hold = TRUE WHERE tenant_id = $1`, tenantID)
	if swept, err := SweepPersonalFieldRetention(tenantID); err != nil || len(swept) != 0 || stored("GP-OLD")["driver_phone"] != "9876500002" {
		t.Fatalf("under legal hold: swept %v (err %v), old phone %v", swept, err, stored("GP-OLD")["driver_phone"])
	}
	db.DB.Exec(`UPDATE public.tenants SET legal_hold = FALSE WHERE tenant_id = $1`, tenantID)
	swept, err := SweepPersonalFieldRetention(tenantID)
	if err != nil {
		t.Fatal(err)
	}
	if swept["GatePass.driver_phone"] != 1 {
		t.Errorf("swept %v, want one gate pass", swept)
	}
	old := stored("GP-OLD")
	if _, kept := old["driver_phone"]; kept || old["driver_name"] != "Ravi" || old["vehicle_number"] != "KA01AB1234" || old["status"] != "Completed" {
		t.Errorf("the 91-day-old gate pass after the sweep: %v", old)
	}
	if stored("GP-NEW")["driver_phone"] != "9876500003" {
		t.Errorf("a 10-day-old gate pass lost its driver phone")
	}
	if again, _ := SweepPersonalFieldRetention(tenantID); len(again) != 0 {
		t.Errorf("a second sweep cleared %v again", again)
	}
}

// A salesman's phone is read and written only by the roles that manage
// salesmen - through the one field policy every read, form and import uses.
func TestStage4962aSalesmanPhoneOnlyForThoseWhoManageSalesmen(t *testing.T) {
	for _, role := range []string{RoleCashier, "Picker", "Accounts Receivable", "Night Lead"} {
		perm, ok := sensitiveFieldPolicy(role, "Salesman")["phone"]
		if !ok || perm.AllowRead || perm.AllowWrite {
			t.Errorf("%s: Salesman.phone read/write allowed (entry %v, %+v)", role, ok, perm)
		}
	}
	for _, role := range []string{RoleStoreManager, "Store Supervisor", RoleSetupOperator, RoleSuperAdmin, RoleAdmin} {
		if perm, ok := sensitiveFieldPolicy(role, "Salesman")["phone"]; ok {
			t.Errorf("%s manages salesmen but Salesman.phone is restricted: %+v", role, perm)
		}
	}
	db.InitDB(testConnStr())
	shown, err := FilterFieldsForRole("default", RoleCashier, "Salesman", map[string]interface{}{"name": "Asha", "store": "HO", "phone": "9876500004"})
	if err != nil {
		t.Fatal(err)
	}
	if _, leaked := shown["phone"]; leaked || shown["name"] != "Asha" {
		t.Errorf("a Cashier reading a salesman: %v", shown)
	}
	if err := RejectRestrictedFieldWrites("default", RoleCashier, "Salesman", map[string]interface{}{"phone": "9876500005"}); err == nil {
		t.Error("a Cashier may write a salesman's phone")
	}
	if row, _ := LookupPersonalDataField("Salesman", "phone"); !strings.Contains(row.Readers, "manage salesmen") {
		t.Errorf("Salesman.phone register readers %q", row.Readers)
	}
}
