package server

import (
	"fmt"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"custom_erp/db"
	"custom_erp/engines"
)

// Stage 128: the Setup Workbook against a real database - a row creates,
// the same file again changes nothing, a changed cell updates only that
// field, a password is never changed, and codes from another organisation
// never find a record here.

func swTestCaller(t *testing.T) *swCaller {
	t.Helper()
	db.InitDB(testConnStr())
	if db.DB == nil || db.DB.Ping() != nil {
		t.Skip("no test database")
	}
	r := httptest.NewRequest("POST", "/api/v1/admin/setup-workbook/preview", nil)
	r.Header.Set("Resolved-Tenant-ID", "default")
	r.Header.Set("Resolved-Role", "Super Admin")
	r.Header.Set("Resolved-User-ID", "admin")
	r.Header.Set("Resolved-Username", "admin")
	r.Header.Set("Resolved-Location", "HO")
	return newSWCaller(r)
}

// swBook writes a workbook: the Instructions stamp, then each sheet as a
// header row, a hint row and the data rows.
func swBook(t *testing.T, stamp string, sheets map[string][][]string, order ...string) []byte {
	t.Helper()
	book := []engines.XLSXSheet{{Name: "Instructions", Rows: [][]engines.XLSXCell{{swT("Organisation"), swT(stamp)}}}}
	for _, name := range order {
		rows := sheets[name]
		var out [][]engines.XLSXCell
		for i, r := range rows {
			var cells []engines.XLSXCell
			for _, c := range r {
				cells = append(cells, swT(c))
			}
			out = append(out, cells)
			if i == 0 {
				out = append(out, []engines.XLSXCell{swHint("# what each column takes")})
			}
		}
		book = append(book, engines.XLSXSheet{Name: name, Rows: out})
	}
	file, err := engines.WriteXLSXBook(book)
	if err != nil {
		t.Fatal(err)
	}
	return file
}

func swRunBook(t *testing.T, c *swCaller, file []byte, apply bool) map[string]*swSheetResult {
	t.Helper()
	tables, stamp, err := swParse(file, swHeaderWords(c.tenantID))
	if err != nil {
		t.Fatal(err)
	}
	run, err := newSWRun(c, stamp != "" && stamp == c.tenantID, apply)
	if err != nil {
		t.Fatal(err)
	}
	out := map[string]*swSheetResult{}
	for _, r := range run.process(tables, nil) {
		out[r.Sheet] = r
		for _, row := range r.Rows {
			if row.Action == "error" || row.Result == "failed" {
				t.Logf("%s row %d %s: %s", r.Sheet, row.Row, row.Key, row.Message)
			}
		}
	}
	return out
}

func swCleanup(t *testing.T, doctype, field, value string) {
	schema, _ := db.GetTenantSchema("default")
	_, _ = db.DB.Exec(fmt.Sprintf(`DELETE FROM %s.documents WHERE doctype = $1 AND data->>$2 = $3`, schema), doctype, field, value)
}

func TestSetupWorkbookCreatesThenChangesNothingThenUpdatesOneField(t *testing.T) {
	c := swTestCaller(t)
	tag := fmt.Sprint(time.Now().UnixNano())
	dept := "SW Dept " + tag
	vendor := "SW Vendor " + tag
	t.Cleanup(func() {
		swCleanup(t, "Department", "name", dept)
		swCleanup(t, "Vendor", "name", vendor)
	})
	sheets := map[string][][]string{
		"Departments": {{"Department Name"}, {dept}},
		"Vendors":     {{"Vendor Name", "Contact Phone", "State"}, {vendor, "9876543210", "Karnataka"}},
	}
	file := swBook(t, "", sheets, "Departments", "Vendors")

	preview := swRunBook(t, c, file, false)
	if preview["Departments"].New != 1 || preview["Vendors"].New != 1 {
		t.Fatalf("preview: departments %+v, vendors %+v", preview["Departments"], preview["Vendors"])
	}
	applied := swRunBook(t, c, file, true)
	if applied["Departments"].Done != 1 || applied["Vendors"].Done != 1 {
		t.Fatalf("apply: departments %+v, vendors %+v", applied["Departments"].Rows[0], applied["Vendors"].Rows[0])
	}

	again := swRunBook(t, c, file, false)
	if again["Departments"].Same != 1 || again["Vendors"].Same != 1 {
		t.Fatalf("the same file again must change nothing: %+v %+v", again["Departments"].Rows[0], again["Vendors"].Rows[0])
	}

	// Only the phone is filled: the state stays as it is.
	sheets["Vendors"] = [][]string{{"Vendor Name", "Contact Phone"}, {vendor, "9123456780"}}
	file = swBook(t, "", sheets, "Vendors")
	changed := swRunBook(t, c, file, true)
	row := changed["Vendors"].Rows[0]
	if row.Action != "change" || row.Result != "done" || len(row.Changes) != 1 || row.Changes[0].From != "9876543210" {
		t.Fatalf("phone change = %+v", row)
	}
	schema, _ := db.GetTenantSchema("default")
	var phone, state string
	if err := db.DB.QueryRow(fmt.Sprintf(`SELECT data->>'contact_phone', data->>'state' FROM %s.documents WHERE doctype = 'Vendor' AND data->>'name' = $1 AND deleted_at IS NULL`, schema), vendor).Scan(&phone, &state); err != nil {
		t.Fatal(err)
	}
	if phone != "9123456780" || state != "Karnataka" {
		t.Fatalf("vendor now %s / %s, want the new phone and the state kept", phone, state)
	}
}

func TestSetupWorkbookCodesFromAnotherOrganisationAreIgnored(t *testing.T) {
	c := swTestCaller(t)
	tag := fmt.Sprint(time.Now().UnixNano())
	first, second := "SW Code A "+tag, "SW Code B "+tag
	t.Cleanup(func() {
		swCleanup(t, "Department", "name", first)
		swCleanup(t, "Department", "name", second)
	})
	swRunBook(t, c, swBook(t, "", map[string][][]string{"Departments": {{"Department Name"}, {first}}}, "Departments"), true)
	schema, _ := db.GetTenantSchema("default")
	var code string
	if err := db.DB.QueryRow(fmt.Sprintf(`SELECT COALESCE(data->>'code', id) FROM %s.documents WHERE doctype = 'Department' AND data->>'name' = $1`, schema), first).Scan(&code); err != nil {
		t.Fatal(err)
	}
	rename := map[string][][]string{"Departments": {{"Code", "Department Name"}, {code, second}}}

	other := swRunBook(t, c, swBook(t, "another-client", rename, "Departments"), false)
	if got := other["Departments"].Rows[0].Action; got != "new" {
		t.Fatalf("a code from another organisation found a record here: action %q", got)
	}
	same := swRunBook(t, c, swBook(t, "default", rename, "Departments"), false)
	if r := same["Departments"].Rows[0]; r.Action != "change" || r.Changes[0].From != first {
		t.Fatalf("this organisation's code must find its record for a rename: %+v", r)
	}

	// A later sheet may already use the new name (preview: nothing renamed yet).
	rename["Users"] = [][]string{{"Username", "Role", "Department", "First password"}, {"swrename" + tag[len(tag)-6:], "Store Manager", second, "Setup#Wb128rename"}}
	withUser := swRunBook(t, c, swBook(t, "default", rename, "Departments", "Users"), false)
	if r := withUser["Users"].Rows[0]; r.Action != "new" {
		t.Fatalf("a user naming the renamed department = %+v", r)
	}
}

func TestSetupWorkbookNeverChangesAPassword(t *testing.T) {
	c := swTestCaller(t)
	tag := fmt.Sprint(time.Now().UnixNano())
	username := "swuser" + tag[len(tag)-8:]
	dept := "SW Pw Dept " + tag
	schema, _ := db.GetTenantSchema("default")
	t.Cleanup(func() {
		_, _ = db.DB.Exec(fmt.Sprintf(`DELETE FROM %s.users WHERE username = $1`, schema), username)
		swCleanup(t, "Department", "name", dept)
	})
	book := func(password string) []byte {
		return swBook(t, "", map[string][][]string{
			"Departments": {{"Department Name"}, {dept}},
			"Users":       {{"Username", "Role", "Department", "First password"}, {username, "Store Manager", dept, password}},
		}, "Departments", "Users")
	}
	created := swRunBook(t, c, book("Setup#Wb128first"), true)
	if r := created["Users"].Rows[0]; r.Action != "new" || r.Result != "done" {
		t.Fatalf("user create = %+v", r)
	}
	var hash string
	var mustChange bool
	if err := db.DB.QueryRow(fmt.Sprintf(`SELECT password_hash, must_change_password FROM %s.users WHERE username = $1`, schema), username).Scan(&hash, &mustChange); err != nil {
		t.Fatal(err)
	}
	if !mustChange {
		t.Error("a user made from the workbook must set their own password at first sign-in")
	}
	again := swRunBook(t, c, book("Setup#Wb128second"), true)
	r := again["Users"].Rows[0]
	if r.Action == "error" || !strings.Contains(r.Message, "never changed") {
		t.Fatalf("second upload = %+v, want a note that the password is never changed", r)
	}
	var after string
	if err := db.DB.QueryRow(fmt.Sprintf(`SELECT password_hash FROM %s.users WHERE username = $1`, schema), username).Scan(&after); err != nil {
		t.Fatal(err)
	}
	if after != hash {
		t.Fatal("an existing user's password was changed by the workbook")
	}
}

// The request envelope caps bodies at 2 MiB by default; a workbook of up to
// the handler's own 8 MB must reach it.
func TestSetupWorkbookUploadsPassTheEnvelope(t *testing.T) {
	for _, p := range []string{"/api/v1/admin/setup-workbook/preview", "/api/v1/admin/setup-workbook/apply"} {
		if got := envelopePolicyFor(p).maxBody; got < swMaxUpload {
			t.Errorf("%s: envelope allows %d bytes, the handler takes up to %d", p, got, swMaxUpload)
		}
	}
}

// A blank template and this organisation's setup both download, and the
// downloaded setup, uploaded again, changes nothing.
func TestSetupWorkbookExportReappliesAsUnchanged(t *testing.T) {
	c := swTestCaller(t)
	run, err := newSWRun(c, true, false)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := run.buildWorkbook(false); err != nil {
		t.Fatalf("template: %v", err)
	}
	file, err := run.buildWorkbook(true)
	if err != nil {
		t.Fatalf("export: %v", err)
	}
	results := swRunBook(t, c, file, false)
	for name, r := range results {
		if r.New+r.Change+r.Errors > 0 {
			for _, row := range r.Rows {
				if row.Action != "same" && row.Action != "note" {
					t.Errorf("%s row %d %s: %s %s %+v", name, row.Row, row.Key, row.Action, row.Message, row.Changes)
				}
			}
		}
	}
}
