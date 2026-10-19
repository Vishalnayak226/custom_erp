package server

import (
	"custom_erp/db"
	"custom_erp/engines"
	"testing"
)

// Stage 98.4: a print goes to this computer's own printer for its kind, else a
// printer at the job's own store, else one anywhere (the old rule) - so one
// store never prints on another store's printer.
func TestStage98PrinterChoiceAndStoreAwareResolve(t *testing.T) {
	db.InitDB(testConnStr())
	const tenant = "default"
	cleanup := func() {
		_, _ = db.DB.Exec(`DELETE FROM tenant_default.documents
			WHERE (doctype = 'PrinterAssignment' AND data->>'computer' LIKE 'TEST-98-%')
			   OR (doctype = 'Printer' AND data->>'qz_printer_name' LIKE 'TEST-98-%')`)
	}
	cleanup()
	defer cleanup()
	for _, p := range [][3]string{{"TEST98-PRN-A", "TEST98A", "TEST-98-A"}, {"TEST98-PRN-B", "TEST98B", "TEST-98-B"}} {
		if _, err := db.DB.Exec(`INSERT INTO tenant_default.documents (id, doctype, data, status, created_by)
			VALUES ($1::text, 'Printer', jsonb_build_object('code', $1::text, 'name', $3::text, 'location', $2::text, 'status', 'Active',
			  'qz_printer_name', $3::text, 'print_role', 'Goods Receipt', 'printer_language', 'PDF'), 'Active', 'admin')`, p[0], p[1], p[2]); err != nil {
			t.Fatal(err)
		}
	}

	resolve := func(computer, location string) string {
		t.Helper()
		p, err := resolveQZPrinter(tenant, "", "Document", "Goods Receipt", computer, location)
		if err != nil {
			t.Fatalf("resolve(%q, %q): %v", computer, location, err)
		}
		return p.QZPrinterName
	}
	if got := resolve("", "TEST98B"); got != "TEST-98-B" {
		t.Errorf("store B prints on %q, want its own TEST-98-B", got)
	}
	if got := resolve("", "TEST98A"); got != "TEST-98-A" {
		t.Errorf("store A prints on %q, want its own TEST-98-A", got)
	}

	if _, err := engines.ChoosePrinter(tenant, "admin", "TEST-98-PC1", "TEST98A", "Goods Receipt", "TEST-98-BACKOFFICE", "PDF"); err != nil {
		t.Fatal(err)
	}
	if got := resolve("TEST-98-PC1", "TEST98A"); got != "TEST-98-BACKOFFICE" {
		t.Errorf("the computer's own choice must win: got %q", got)
	}
	if got := resolve("TEST-98-PC2", "TEST98A"); got != "TEST-98-A" {
		t.Errorf("another computer keeps the store's printer: got %q", got)
	}
	if _, err := engines.ChoosePrinter(tenant, "admin", "test-98-pc1", "TEST98A", "Goods Receipt", "TEST-98-A", "PDF"); err != nil {
		t.Fatal(err)
	}
	if got := resolve("TEST-98-PC1", "TEST98A"); got != "TEST-98-A" {
		t.Errorf("a changed choice must replace the old one (computer names ignore case): got %q", got)
	}
	if choices, _ := engines.PrinterChoices(tenant, "TEST-98-PC1"); len(choices) != 1 || choices[0].Printer.Code != "TEST98-PRN-A" {
		t.Errorf("one choice per computer and kind, reusing the store's Printer record; got %+v", choices)
	}
	if _, err := engines.ChoosePrinter(tenant, "admin", "TEST-98-PC1", "TEST98A", "Payslip", "TEST-98-A", "PDF"); err == nil {
		t.Error("an unknown kind of print must be refused")
	}
}
