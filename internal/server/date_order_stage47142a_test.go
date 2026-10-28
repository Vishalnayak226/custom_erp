package server

import (
	"custom_erp/db"
	"custom_erp/engines"
	"encoding/json"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"
)

// TestStage47142aSaveReadsTheClientsDateOrder: through the real record save,
// the same typed 03/02/2026 is stored as 3 February for a day-first client
// (the default) and as 2 March once the client is month first; each order
// refuses a date that does not fit it, and the browser is told the order.
func TestStage47142aSaveReadsTheClientsDateOrder(t *testing.T) {
	db.InitDB(testConnStr())
	tenantID := "default"
	schema, _ := db.GetTenantSchema(tenantID)
	cleanup := func() {
		db.DB.Exec("DELETE FROM " + schema + ".documents WHERE doctype = 'NonConformanceReport' AND data->>'code' LIKE 'T47142A-%'")
	}
	cleanup()
	defer cleanup()
	_, _ = db.DB.Exec("DELETE FROM "+schema+".system_settings WHERE key = $1", engines.SettingKeyDateOrder)
	defer func() {
		_ = engines.SetSetting(tenantID, engines.SettingKeyDateOrder, engines.DateOrderDMY, "system")
		_, _ = db.DB.Exec("DELETE FROM "+schema+".system_settings WHERE key = $1", engines.SettingKeyDateOrder)
	}()
	setOrder := func(order string) {
		if err := engines.SetSetting(tenantID, engines.SettingKeyDateOrder, order, "system"); err != nil {
			t.Fatalf("set date order %s: %v", order, err)
		}
	}
	// save creates a record with due_date typed as given and returns the
	// status, the stored due_date and the response body.
	save := func(code, typed string) (int, string, string) {
		raw, _ := json.Marshal(map[string]interface{}{"code": code, "description": "date order", "status": "Draft", "due_date": typed})
		r := httptest.NewRequest("POST", "/api/v1/doc/NonConformanceReport", strings.NewReader(string(raw)))
		r.SetPathValue("doctype", "NonConformanceReport")
		r.Header.Set("Resolved-Tenant-ID", tenantID)
		r.Header.Set("Resolved-Role", engines.RoleSuperAdmin)
		r.Header.Set("Resolved-User-ID", "admin")
		r.Header.Set("Resolved-Username", "admin")
		r.Header.Set("Resolved-Location", "HO")
		w := httptest.NewRecorder()
		handleGenericDoc(w, r)
		var stored string
		_ = db.DB.QueryRow("SELECT COALESCE(data->>'due_date', '') FROM "+schema+".documents WHERE doctype = 'NonConformanceReport' AND data->>'code' = $1", code).Scan(&stored)
		return w.Code, stored, w.Body.String()
	}
	orderSent := func() string {
		r := httptest.NewRequest("GET", "/api/v1/localization", nil)
		r.Header.Set("Resolved-Tenant-ID", tenantID)
		w := httptest.NewRecorder()
		handleGetLocalization(w, r)
		var out struct {
			DateOrder string `json:"date_order"`
		}
		_ = json.Unmarshal(w.Body.Bytes(), &out)
		return out.DateOrder
	}

	// Day first unless set.
	if o := engines.ClientDateOrder(tenantID); o != engines.DateOrderDMY {
		t.Fatalf("an unset client reads dates %s, want day first", o)
	}
	if o := orderSent(); o != engines.DateOrderDMY {
		t.Errorf("GET /api/v1/localization date_order = %q, want DMY", o)
	}
	if code, stored, body := save("T47142A-DMY", "03/02/2026"); code >= 300 || stored != "2026-02-03" {
		t.Fatalf("day first: %d stored %q (%s); want 2026-02-03", code, stored, body)
	}

	setOrder(engines.DateOrderMDY)
	if o := orderSent(); o != engines.DateOrderMDY {
		t.Errorf("GET /api/v1/localization date_order = %q, want MDY", o)
	}
	if code, stored, body := save("T47142A-MDY", "03/02/2026"); code >= 300 || stored != "2026-03-02" {
		t.Fatalf("month first: %d stored %q (%s); want 2026-03-02", code, stored, body)
	}
	if code, stored, body := save("T47142A-MDY-BAD", "13/02/2026"); code < 400 || stored != "" || !strings.Contains(body, "month first") {
		t.Errorf("month first 13/02/2026: %d stored %q (%s); want refused, saying month first", code, stored, body)
	}

	setOrder(engines.DateOrderYMD)
	if code, stored, body := save("T47142A-YMD", "2026/02/03"); code >= 300 || stored != "2026-02-03" {
		t.Fatalf("year first: %d stored %q (%s); want 2026-02-03", code, stored, body)
	}
	if code, stored, body := save("T47142A-YMD-BAD", "03/02/2026"); code < 400 || stored != "" || !strings.Contains(body, "year first") {
		t.Errorf("year first 03/02/2026: %d stored %q (%s); want refused, saying year first", code, stored, body)
	}
	// ISO is read the same whatever the order.
	if code, stored, body := save("T47142A-ISO", "2026-02-03"); code >= 300 || stored != "2026-02-03" {
		t.Errorf("ISO under year first: %d stored %q (%s)", code, stored, body)
	}
}

// TestStage47142aDatesShowInTheClientsOrder is the static half: formatDay
// keeps the month as a word and lays the parts out in the order the
// localization endpoint sends - 03 Feb 2026, Feb 03, 2026 or 2026 Feb 03.
func TestStage47142aDatesShowInTheClientsOrder(t *testing.T) {
	app := moduleSource(t, "../../public/app.js")
	body := regexp.MustCompile(`(?s)function formatDay\(value\) \{(.*?)\n\}`).FindStringSubmatch(app)
	if body == nil {
		t.Fatal("public/app.js has no formatDay(value) helper")
	}
	for _, want := range []string{"state.localization?.date_order", "order === 'MDY' ? `${mon} ${m[3]}, ${m[1]}`",
		"order === 'YMD' ? `${m[1]} ${mon} ${m[3]}`", ": `${m[3]} ${mon} ${m[1]}`"} {
		if !strings.Contains(body[1], want) {
			t.Errorf("formatDay does not contain %q", want)
		}
	}
	if !strings.Contains(app, "state.localization = await res.json();") {
		t.Error("app.js no longer keeps /api/v1/localization in state.localization")
	}
}

// TestStage47142aBulkPurchaseReadsTheClientsDateOrder: a Bills sheet's typed
// 03/02/2026 is 3 February for a day-first client and 2 March for a
// month-first one; a date that does not fit the order is refused, and an
// Excel date cell is read the same in every order.
func TestStage47142aBulkPurchaseReadsTheClientsDateOrder(t *testing.T) {
	file := swBook(t, "", map[string][][]string{
		"Purchase": {bpTestPurchaseHead},
		"Bills": {
			{"Check", "Vendor Invoice No *", "Vendor *", "Invoice Date *", "Invoice Total (₹) *"},
			{"", "INV1", "V One", "03/02/2026", "180"},
			{"", "INV2", "V One", "13/02/2026", "180"},
			{"", "INV3", "V One", "46315", "180"},
			{"", "INV4", "V One", "3-Feb-2026", "180"},
		},
	}, "Purchase", "Bills")
	for order, want := range map[string][4]string{
		engines.DateOrderDMY: {"2026-02-03", "2026-02-13", "2026-10-20", "2026-02-03"},
		engines.DateOrderMDY: {"2026-03-02", "", "2026-10-20", "2026-02-03"},
	} {
		_, _, bills, _, err := bpRead(file, order)
		if err != nil {
			t.Fatal(err)
		}
		if len(bills) != 4 {
			t.Fatalf("%s: read %d bills, want 4", order, len(bills))
		}
		for i, b := range bills {
			if b.Date != want[i] || (want[i] == "") != (b.err != "") {
				t.Errorf("%s: bill %s read as date %q, problem %q; want %q", order, b.Invoice, b.Date, b.err, want[i])
			}
		}
	}
	if _, _, bills, _, _ := bpRead(file, engines.DateOrderMDY); !strings.Contains(bills[1].err, "type it like 10-20-2026") {
		t.Errorf("month first refusal %q should show a month-first example", bills[1].err)
	}
}

// TestStage47142aPrintDateFormatFollowsTheOrder is the static half for
// prints: a template's Date format lays the parts out in the client's order.
func TestStage47142aPrintDateFormatFollowsTheOrder(t *testing.T) {
	src := moduleSource(t, "../../public/trade-docs.js")
	body := regexp.MustCompile(`(?s)case 'date': \{(.*?)\n    \}`).FindStringSubmatch(src)
	if body == nil {
		t.Fatal("trade-docs.js formatValue has no 'date' case")
	}
	for _, want := range []string{"state.localization?.date_order", "order === 'MDY' ? `${m[2]}-${m[3]}-${m[1]}`",
		"order === 'YMD' ? `${m[1]}-${m[2]}-${m[3]}`", ": `${m[3]}-${m[2]}-${m[1]}`"} {
		if !strings.Contains(body[1], want) {
			t.Errorf("the print Date format does not contain %q", want)
		}
	}
}
