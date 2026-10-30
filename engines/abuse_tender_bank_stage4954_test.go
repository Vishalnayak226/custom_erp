package engines

import (
	"custom_erp/db"
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

// 49.5.4: a duplicate tender is refused at the checkout choke point, and a
// bank change made by CSV import writes the same signed EDIT_Vendor row the
// record save writes, so the detection sees it.

func TestStage4954DuplicateTenderRefusedAtCheckout(t *testing.T) {
	spInitDB()
	schema, err := db.GetTenantSchema("default")
	if err != nil {
		t.Fatal(err)
	}
	const loc = "T4954-STORE"
	cleanup := func() {
		_, _ = db.DB.Exec(`DELETE FROM `+schema+`.documents WHERE doctype='POSCart' AND (data->>'location'=$1 OR id LIKE 'T4954-BILL-%')`, loc)
		_ = SetSetting("default", SettingDuplicateTenderWindow, "10", "admin")
		_, _ = db.DB.Exec(`DELETE FROM `+schema+`.system_settings WHERE key=$1`, SettingDuplicateTenderWindow)
	}
	cleanup()
	t.Cleanup(cleanup)
	paid := func(id, when string, tenders []POSTender) {
		t.Helper()
		data, _ := json.Marshal(map[string]interface{}{"location": loc, "payment_mode": "Card", "tenders": tenders})
		if _, err := db.DB.Exec(`INSERT INTO `+schema+`.documents (id, doctype, data, status, created_by, updated_at) VALUES ($1,'POSCart',$2,'Paid','cashier1', CURRENT_TIMESTAMP - $3::interval)`, id, data, when); err != nil {
			t.Fatal(err)
		}
	}
	paid("T4954-BILL-1", "2 minutes", []POSTender{{Mode: "Card", Amount: 1250, Reference: "SLIP-4954-A"}, {Mode: "Cash", Amount: 100}})
	paid("T4954-BILL-OLD", "30 minutes", []POSTender{{Mode: "UPI", Amount: 500, Reference: "UTR-4954-OLD"}})
	paid("T4954-BILL-ELSEWHERE", "1 minute", []POSTender{{Mode: "UPI", Amount: 700, Reference: "UTR-4954-X"}})
	if _, err := db.DB.Exec(`UPDATE ` + schema + `.documents SET data = data || '{"location":"T4954-OTHER"}' WHERE id='T4954-BILL-ELSEWHERE'`); err != nil {
		t.Fatal(err)
	}
	check := func(label string, cart posCart) error {
		t.Helper()
		tx, err := db.DB.Begin()
		if err != nil {
			t.Fatal(err)
		}
		defer tx.Rollback()
		return refuseDuplicateTenderTx(tx, schema, "T4954-NEW", cart)
	}
	wantRefused := func(label string, err error) {
		t.Helper()
		var verr *ValidationError
		if !errors.As(err, &verr) || verr.Code != DuplicateTenderCode || !strings.Contains(verr.Message, "T4954-BILL-1") {
			t.Fatalf("%s: want %s naming the earlier bill, got %v", label, DuplicateTenderCode, err)
		}
	}
	wantRefused("same card slip, same amount, 2 minutes ago", check("dup", posCart{Location: loc, Tenders: []POSTender{{Mode: "Card", Amount: 1250, Reference: "SLIP-4954-A"}}}))
	wantRefused("the duplicate among other tenders", check("dup-mixed", posCart{Location: loc, Tenders: []POSTender{{Mode: "Cash", Amount: 50}, {Mode: "Card", Amount: 1250, Reference: "SLIP-4954-A"}}}))
	for label, cart := range map[string]posCart{
		"marked a second payment":   {Location: loc, Tenders: []POSTender{{Mode: "Card", Amount: 1250, Reference: "SLIP-4954-A", SecondPayment: true}}},
		"a different amount":        {Location: loc, Tenders: []POSTender{{Mode: "Card", Amount: 1251, Reference: "SLIP-4954-A"}}},
		"a different reference":     {Location: loc, Tenders: []POSTender{{Mode: "Card", Amount: 1250, Reference: "SLIP-4954-B"}}},
		"a different tender kind":   {Location: loc, Tenders: []POSTender{{Mode: "UPI", Amount: 1250, Reference: "SLIP-4954-A"}}},
		"cash has no reference":     {Location: loc, Tenders: []POSTender{{Mode: "Cash", Amount: 100}}},
		"outside the window":        {Location: loc, Tenders: []POSTender{{Mode: "UPI", Amount: 500, Reference: "UTR-4954-OLD"}}},
		"taken at another store":    {Location: loc, Tenders: []POSTender{{Mode: "UPI", Amount: 700, Reference: "UTR-4954-X"}}},
		"no tenders (payment mode)": {Location: loc, PaymentMode: "Card"},
	} {
		if err := check(label, cart); err != nil {
			t.Fatalf("%s must pass: %v", label, err)
		}
	}
	// The window is a setting; 0 switches the check off.
	if err := SetSetting("default", SettingDuplicateTenderWindow, "0", "admin"); err != nil {
		t.Fatal(err)
	}
	if err := check("off", posCart{Location: loc, Tenders: []POSTender{{Mode: "Card", Amount: 1250, Reference: "SLIP-4954-A"}}}); err != nil {
		t.Fatalf("window 0 switches the check off: %v", err)
	}
	if err := SetSetting("default", SettingDuplicateTenderWindow, "60", "admin"); err != nil {
		t.Fatal(err)
	}
	err = check("wide", posCart{Location: loc, Tenders: []POSTender{{Mode: "UPI", Amount: 500, Reference: "UTR-4954-OLD"}}})
	var verr *ValidationError
	if !errors.As(err, &verr) || verr.Code != DuplicateTenderCode || !strings.Contains(verr.Message, "T4954-BILL-OLD") {
		t.Fatalf("a 60-minute window reaches the old bill: %v", err)
	}
}

func TestStage4954ImportBankChangeWritesSignedEdit(t *testing.T) {
	spInitDB()
	schema, err := db.GetTenantSchema("default")
	if err != nil {
		t.Fatal(err)
	}
	const vendor = "T4954-VENDOR"
	cleanup := func() {
		_, _ = db.DB.Exec(`DELETE FROM `+schema+`.documents WHERE doctype='Vendor' AND (id=$1 OR data->>'code'=$1)`, vendor)
		_, _ = db.DB.Exec(`DELETE FROM ` + schema + `.audit_logs WHERE action='EDIT_Vendor' AND details LIKE '%` + vendor + `%'`)
	}
	cleanup()
	t.Cleanup(cleanup)
	if res, err := BulkImportCSV("default", "Vendor", strings.NewReader("code,name,state,bank_account_number,bank_ifsc\n"+vendor+",T4954 Vendor,Karnataka,111122223333,HDFC0000001\n"), "system", "HR/Admin", false); err != nil || res.FailedRows != 0 {
		t.Fatalf("create by import: %v %+v", err, res)
	}
	var edits int
	_ = db.DB.QueryRow(`SELECT COUNT(*) FROM ` + schema + `.audit_logs WHERE action='EDIT_Vendor' AND details LIKE 'Edited Vendor ` + vendor + `:%'`).Scan(&edits)
	if edits != 0 {
		t.Fatalf("a create is not an edit: %d rows", edits)
	}
	// A dry run changes nothing and writes no row.
	csv := "id,code,name,state,bank_account_number,bank_ifsc\n" + vendor + "," + vendor + ",T4954 Vendor,Karnataka,999988887777,HDFC0000001\n"
	if res, err := BulkImportCSV("default", "Vendor", strings.NewReader(csv), "system", "HR/Admin", true); err != nil || res.FailedRows != 0 {
		t.Fatalf("dry run: %v %+v", err, res)
	}
	_ = db.DB.QueryRow(`SELECT COUNT(*) FROM ` + schema + `.audit_logs WHERE action='EDIT_Vendor' AND details LIKE 'Edited Vendor ` + vendor + `:%'`).Scan(&edits)
	if edits != 0 {
		t.Fatalf("a dry run writes no edit row: %d rows", edits)
	}
	res, err := BulkImportCSV("default", "Vendor", strings.NewReader(csv), "system", "HR/Admin", false)
	if err != nil || res.FailedRows != 0 || len(res.UpdatedIDs) != 1 {
		t.Fatalf("update by import: %v %+v", err, res)
	}
	var details, user string
	var signed bool
	if err := db.DB.QueryRow(`SELECT details, user_id, signature IS NOT NULL FROM `+schema+`.audit_logs WHERE action='EDIT_Vendor' AND details LIKE 'Edited Vendor `+vendor+`:%' ORDER BY id DESC LIMIT 1`).Scan(&details, &user, &signed); err != nil {
		t.Fatalf("the import's bank change has no EDIT_Vendor row: %v", err)
	}
	if !signed || user != "system" || !strings.Contains(details, "bank_account_number") || strings.Contains(details, "bank_ifsc") || !strings.Contains(details, "CSV import") {
		t.Fatalf("row = %q by %q signed=%v: want the changed field only, the importer, signed", details, user, signed)
	}
	// The same file again: nothing changed, no second row.
	if _, err := BulkImportCSV("default", "Vendor", strings.NewReader(csv), "system", "HR/Admin", false); err != nil {
		t.Fatal(err)
	}
	_ = db.DB.QueryRow(`SELECT COUNT(*) FROM ` + schema + `.audit_logs WHERE action='EDIT_Vendor' AND details LIKE 'Edited Vendor ` + vendor + `:%'`).Scan(&edits)
	if edits != 1 {
		t.Fatalf("an unchanged re-import writes no row: %d rows", edits)
	}
	// The detection's own regex sees the row the same way it sees a save.
	var seen bool
	_ = db.DB.QueryRow(`SELECT EXISTS(SELECT 1 FROM `+schema+`.audit_logs WHERE action='EDIT_Vendor' AND signature IS NOT NULL AND details ~ '\m(bank_account_number|bank_ifsc)\M' AND substring(details from '^Edited Vendor ([^:]+):') = $1)`, vendor).Scan(&seen)
	if !seen {
		t.Fatal("detectVendorBankThenPaid's query does not match the import's row")
	}
}
