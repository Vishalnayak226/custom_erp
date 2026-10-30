package server

import (
	"custom_erp/db"
	"fmt"
	"strings"
	"testing"
	"time"
)

// 49.5.4 (b): a vendor bank change applied from the Setup Workbook goes
// through the record save, so it writes the signed EDIT_Vendor row the
// bank-change detection reads.
func TestStage4954WorkbookBankChangeWritesSignedEdit(t *testing.T) {
	c := swTestCaller(t)
	tag := fmt.Sprint(time.Now().UnixNano())
	vendor := "SW Bank Vendor " + tag
	schema, _ := db.GetTenantSchema("default")
	t.Cleanup(func() {
		swCleanup(t, "Vendor", "name", vendor)
		_, _ = db.DB.Exec(fmt.Sprintf(`DELETE FROM %s.audit_logs WHERE action='EDIT_Vendor' AND details LIKE $1`, schema), "%"+tag+"%")
	})
	sheets := map[string][][]string{
		"Vendors": {{"Vendor Name", "State", "Bank Account Number", "Bank IFSC"}, {vendor, "Karnataka", "111122223333", "HDFC0000001"}},
	}
	if applied := swRunBook(t, c, swBook(t, "", sheets, "Vendors"), true); applied["Vendors"].Done != 1 {
		t.Fatalf("apply: %+v", applied["Vendors"].Rows[0])
	}
	var id string
	if err := db.DB.QueryRow(fmt.Sprintf(`SELECT id FROM %s.documents WHERE doctype='Vendor' AND data->>'name'=$1 AND deleted_at IS NULL`, schema), vendor).Scan(&id); err != nil {
		t.Fatal(err)
	}
	sheets["Vendors"] = [][]string{{"Vendor Name", "Bank Account Number"}, {vendor, "999988887777"}}
	changed := swRunBook(t, c, swBook(t, "", sheets, "Vendors"), true)
	if row := changed["Vendors"].Rows[0]; row.Action != "change" || row.Result != "done" {
		t.Fatalf("bank change = %+v", row)
	}
	var details, user string
	var signed bool
	if err := db.DB.QueryRow(fmt.Sprintf(`SELECT details, user_id, signature IS NOT NULL FROM %s.audit_logs WHERE action='EDIT_Vendor' AND details LIKE 'Edited Vendor %s:%%' ORDER BY id DESC LIMIT 1`, schema, id)).Scan(&details, &user, &signed); err != nil {
		t.Fatalf("the workbook's bank change has no EDIT_Vendor row: %v", err)
	}
	if !signed || user != "admin" || !strings.Contains(details, "bank_account_number") {
		t.Fatalf("row = %q by %q signed=%v", details, user, signed)
	}
}
