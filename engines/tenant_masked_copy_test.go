package engines

import (
	"strings"
	"testing"

	"custom_erp/db"
)

func TestMaskedCopyLeavesNoSourceValueAndNeverWritesSource(t *testing.T) {
	db.InitDB(testConnStr())
	srcID, srcSchema, _, err := ProvisionSandboxTenant("", 1)
	if err != nil {
		t.Skipf("cannot provision a sandbox here: %v", err)
	}
	defer DeleteSandboxTenant(srcID)
	dstID, dstSchema, _, err := ProvisionSandboxTenant("", 1)
	if err != nil {
		t.Fatalf("provision target: %v", err)
	}
	defer DeleteSandboxTenant(dstID)

	secrets := []string{"9876501234", "secret.person@example.com", "Zaheer Quraishi", "27AAAPL1234C1ZV", "AAAPL1234C"}
	if _, err := db.DB.Exec(`INSERT INTO ` + srcSchema + `.documents (id, doctype, data, status, created_by) VALUES
		('I170-MC-CUST','Customer','{"code":"C-MC","name":"Zaheer Quraishi","phone":"9876501234","email":"secret.person@example.com","gstin":"27AAAPL1234C1ZV","pan":"AAAPL1234C"}','Active','system'),
		('I170-MC-VEND','Vendor','{"code":"V-MC","name":"Acme Traders","contact_person":"Zaheer Quraishi","phone":"9876501234"}','Active','system')`); err != nil {
		t.Fatalf("seed source: %v", err)
	}
	var beforeN int
	var beforeSum string
	q := `SELECT COUNT(*), COALESCE(md5(string_agg(id || data::text, ',' ORDER BY id)),'') FROM ` + srcSchema + `.documents`
	db.DB.QueryRow(q).Scan(&beforeN, &beforeSum)

	res, err := CopyTenantMaskedIntoSandbox(srcID, dstID, "tester")
	if err != nil {
		t.Fatalf("copy: %v", err)
	}
	if res.Copied < 2 || res.FieldsMasked == 0 {
		t.Fatalf("copy did too little: %+v", res)
	}
	rows, err := db.DB.Query(`SELECT data::text FROM ` + dstSchema + `.documents`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var all strings.Builder
	for rows.Next() {
		var s string
		rows.Scan(&s)
		all.WriteString(s)
	}
	for _, s := range secrets {
		if strings.Contains(all.String(), s) {
			t.Errorf("source value %q survived in the masked copy", s)
		}
	}
	if !strings.Contains(all.String(), "V-MC") {
		t.Error("a business field (the vendor code) was masked or the row is missing")
	}
	var afterN int
	var afterSum string
	db.DB.QueryRow(q).Scan(&afterN, &afterSum)
	if afterN != beforeN || afterSum != beforeSum {
		t.Error("the source client changed during the copy")
	}
}

func TestMaskedCopyRefusals(t *testing.T) {
	db.InitDB(testConnStr())
	wantValidationCode(t, func() error { _, e := CopyTenantMaskedIntoSandbox("default", "default", "t"); return e }(), "SEC-1710")
	wantValidationCode(t, func() error { _, e := CopyTenantMaskedIntoSandbox("default", "minn", "t"); return e }(), "SEC-1710")
	srcID, _, _, err := ProvisionSandboxTenant("", 1)
	if err != nil {
		t.Skipf("cannot provision a sandbox here: %v", err)
	}
	defer DeleteSandboxTenant(srcID)
	wantValidationCode(t, func() error { _, e := CopyTenantMaskedIntoSandbox(srcID, srcID, "t"); return e }(), "SEC-1711")
}
