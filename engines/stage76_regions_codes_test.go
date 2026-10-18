package engines

import (
	"custom_erp/db"
	"regexp"
	"strings"
	"testing"
)

// Stage 76: states, tax IDs and master codes.

func TestGSTINCheckDigit(t *testing.T) {
	for _, g := range []string{"27AAPFU0939F1ZV", "29AABCU9603R1ZJ", "27aapfu0939f1zv"} {
		if !ValidGSTINChecksum(g) {
			t.Errorf("%s should pass the check digit", g)
		}
	}
	// One character wrong - the shape is fine, the number is not real.
	for _, g := range []string{"29AABCU9603R1ZM", "27AAPFU0939F1ZW", "27AAPFU0939F1Z"} {
		if ValidGSTINChecksum(g) {
			t.Errorf("%s should fail the check digit", g)
		}
	}
	spec, _ := DetectFieldFormat("gstin")
	err := ValidateFieldFormat(spec, "GSTIN", "29AABCU9603R1ZM")
	if err == nil || !strings.Contains(err.Error(), "check digit") {
		t.Errorf("a mistyped GSTIN must be refused for its check digit, got %v", err)
	}
}

func TestTaxIDFormatsPerCountry(t *testing.T) {
	if fieldFormats[0].Key != "gstin" {
		t.Fatalf("TaxIDFormat assumes fieldFormats[0] is the gstin entry, got %q", fieldFormats[0].Key)
	}
	cases := []struct {
		country, value string
		ok             bool
	}{
		{"IN", "27AAPFU0939F1ZV", true},
		{"IN", "100123456700003", false}, // a UAE TRN is not a GSTIN
		{"AE", "100123456700003", true},
		{"AE", "27AAPFU0939F1ZV", false},
		{"SA", "310122393500003", true},
		{"SA", "410122393500003", false},
		{"AU", "51824753556", true},
		{"AU", "51824753557", false}, // ABN checksum
		{"GB", "GB123456789", true},
		{"DE", "DE123456789", true},
		{"DE", "DE12345678", false},
		{"KE", "P051234567X", true}, // no rule: letters and digits, 5-20
		{"KE", "AB", false},
	}
	for _, c := range cases {
		spec := TaxIDFormat(c.country)
		err := ValidateFieldFormat(spec, spec.Label, NormalizeFieldFormatValue(spec, c.value))
		if (err == nil) != c.ok {
			t.Errorf("%s %s: ok=%v, got err %v", c.country, c.value, c.ok, err)
		}
	}
	if TaxIDFormat("AE").Label != "TRN" || TaxIDFormat("IN").Label != "GSTIN" {
		t.Errorf("the tax ID is named by the country: AE %q, IN %q", TaxIDFormat("AE").Label, TaxIDFormat("IN").Label)
	}
	specs := FieldFormatSpecsFor("IN")
	if regions, _ := specs["regions"].([]Region); len(regions) != 37 {
		t.Errorf("India offers 36 states/UTs plus Other Territory, got %d", len(regions))
	}
}

func TestResolveRegion(t *testing.T) {
	for in, want := range map[string]string{
		"Karnataka": "Karnataka", " karnataka ": "Karnataka", "KA": "Karnataka", "29": "Karnataka",
		"Orissa": "Odisha", "OR": "Odisha", "TG": "Telangana", "TS": "Telangana",
		"Jammu & Kashmir": "Jammu and Kashmir", "New Delhi": "Delhi", "Pondicherry": "Puducherry",
	} {
		r, ok := ResolveRegion("IN", in)
		if !ok || r.Name != want {
			t.Errorf("ResolveRegion(%q) = %q/%v, want %q", in, r.Name, ok, want)
		}
	}
	if r, ok := ResolveRegion("IN", "Karnataka"); !ok || r.Short != "KA" || r.TaxCode != "29" {
		t.Errorf("Karnataka short/tax code = %q/%q", r.Short, r.TaxCode)
	}
	// Legacy codes are recognised on old registrations but never offered.
	if r, ok := RegionFromTaxCode("IN", "25"); !ok || !r.Legacy {
		t.Errorf("code 25 should resolve as legacy")
	}
	for _, r := range CountryRegions("IN") {
		if r.Legacy {
			t.Errorf("legacy region %q offered in the dropdown", r.Name)
		}
	}
	if StateCodeFromName("ka") != "29" || StateCodeFromName("Tamil Nadu") != "33" || StateCodeFromName("Atlantis") != "" {
		t.Errorf("StateCodeFromName lost a spelling")
	}
	if CountryRegions("ZZ") != nil {
		t.Errorf("a country with no list keeps a free-text state")
	}
}

func TestDocumentRegions(t *testing.T) {
	fields := []FieldMeta{
		{Fieldname: "gstin", Fieldtype: "Data", Label: "GSTIN"},
		{Fieldname: "state", Fieldtype: "Data", Label: "State", Mandatory: true},
		{Fieldname: "state_code", Fieldtype: "Data", Label: "State Code"},
		{Fieldname: "payment_state", Fieldtype: "Select", Label: "Payment State"},
	}
	run := func(strict bool, doc map[string]interface{}) error {
		return applyDocumentRegions("default", "Vendor", "IN", strict, fields, doc)
	}

	doc := map[string]interface{}{"gstin": "29AABCU9603R1ZJ"}
	if err := run(true, doc); err != nil || doc["state"] != "Karnataka" || doc["state_code"] != "KA" {
		t.Errorf("a GSTIN fills a blank state: err %v, state %v, code %v", err, doc["state"], doc["state_code"])
	}
	doc = map[string]interface{}{"gstin": "29aabcu9603r1zj", "state": "ka"}
	if err := run(true, doc); err != nil || doc["state"] != "Karnataka" {
		t.Errorf("a short code is stored as the name: err %v, state %v", err, doc["state"])
	}
	doc = map[string]interface{}{"gstin": "29AABCU9603R1ZJ", "state": "Maharashtra"}
	if err := run(true, doc); err == nil || !strings.Contains(err.Error(), "registered in Karnataka") {
		t.Errorf("a state contradicting the GSTIN must be refused, got %v", err)
	}
	doc = map[string]interface{}{"state": "Atlantis"}
	if err := run(true, doc); err == nil {
		t.Errorf("a master must refuse a state that is not on the list")
	}
	doc = map[string]interface{}{"state": "Atlantis"}
	if err := run(false, doc); err != nil || doc["state"] != "Atlantis" {
		t.Errorf("a transaction keeps a state it cannot resolve: err %v, state %v", err, doc["state"])
	}
	// A wrong GSTIN fills nothing - the format check reports it later.
	doc = map[string]interface{}{"gstin": "29AABCU9603R1ZM"}
	if err := run(true, doc); err != nil || doc["state"] != nil {
		t.Errorf("a GSTIN with a bad check digit must not choose a state: err %v, state %v", err, doc["state"])
	}
	// A channel order from abroad is checked against its own country.
	doc = map[string]interface{}{"state": "California", "customer_country": "US"}
	if err := applyDocumentRegions("default", "SalesOrder", "IN", false, []FieldMeta{{Fieldname: "state", Fieldtype: "Data"}}, doc); err != nil || doc["state"] != "California" {
		t.Errorf("a foreign order keeps its state: err %v, state %v", err, doc["state"])
	}
}

func TestSuggestMasterPrefix(t *testing.T) {
	if p := suggestMasterPrefix("Vendor", map[string]bool{}); p != "V" {
		t.Errorf("Vendor -> %q, want V", p)
	}
	if p := suggestMasterPrefix("Category", map[string]bool{}); p != "CAT" {
		t.Errorf("Category -> %q, want CAT", p)
	}
	if p := suggestMasterPrefix("Category", map[string]bool{"CAT": true}); p != "CA" {
		t.Errorf("Category with CAT taken -> %q, want CA (C is kept for Customer)", p)
	}
	if p := suggestMasterPrefix("Color", map[string]bool{}); p != "CO" {
		t.Errorf("Color -> %q, want CO - C is Customer's even before any customer exists", p)
	}
	if p := suggestMasterPrefix("LegalEntity", map[string]bool{}); p != "LE" {
		t.Errorf("LegalEntity -> %q, want LE", p)
	}
	if p := suggestMasterPrefix("ProductFamily", map[string]bool{}); p != "D" {
		t.Errorf("ProductFamily (shown as Design) -> %q, want D", p)
	}
}

// TestMasterCodesShortAndAskedOnce numbers a scratch master doctype: the
// first record confirms the format it carries, later ones ignore a format and
// take the next number, a number already used as an id is skipped, and a
// typed-code master is never numbered.
func TestMasterCodesShortAndAskedOnce(t *testing.T) {
	db.InitDB(testConnStr())
	schema, err := db.GetTenantSchema("default")
	if err != nil {
		t.Fatal(err)
	}
	const dt = "Stage76TestMaster"
	cleanup := func() {
		for _, q := range []string{
			"DELETE FROM " + schema + ".documents WHERE doctype = $1 OR id LIKE 'ZQ%'",
			"DELETE FROM " + schema + ".prefix_configs WHERE doc_type = $1",
			"DELETE FROM " + schema + ".sequence_counters WHERE doc_type = $1",
			"DELETE FROM " + schema + ".doctype_fields WHERE doctype_name = $1",
			"DELETE FROM " + schema + ".doctype_meta WHERE name = $1",
		} {
			if strings.Contains(q, "$1") {
				_, _ = db.DB.Exec(q, dt)
			}
		}
	}
	cleanup()
	defer cleanup()
	if _, err := db.DB.Exec("INSERT INTO "+schema+".doctype_meta (name, module, document_type) VALUES ($1, 'Test', 'Master')", dt); err != nil {
		t.Fatalf("seed meta: %v", err)
	}

	series, err := GetMasterCodeSeries("default", dt)
	if err != nil || series.Configured || series.Length != 5 || series.Next != series.Prefix+"00001" {
		t.Fatalf("an unconfirmed master offers a suggestion: %+v, %v", series, err)
	}

	first := map[string]interface{}{"name": "One", "_code_format": map[string]interface{}{"prefix": "zq", "length": float64(4)}}
	if err := PrepareMasterNumber("default", "", dt, true, first); err != nil {
		t.Fatal(err)
	}
	if first["code"] != "ZQ0001" || first["id"] != "ZQ0001" {
		t.Errorf("first code = %v / id %v, want ZQ0001", first["code"], first["id"])
	}
	if _, kept := first["_code_format"]; kept {
		t.Errorf("_code_format must never be stored on the record")
	}

	// Asked once: a later format is ignored.
	second := map[string]interface{}{"_code_format": map[string]interface{}{"prefix": "XX", "length": float64(6)}}
	if err := PrepareMasterNumber("default", "", dt, true, second); err != nil || second["code"] != "ZQ0002" {
		t.Errorf("second code = %v (%v), want ZQ0002", second["code"], err)
	}

	// A number someone already used is skipped, never overwritten.
	if _, err := db.DB.Exec("INSERT INTO "+schema+".documents (id, doctype, data, status, created_by) VALUES ('ZQ0003', $1, '{}', 'Active', 'system')", dt); err != nil {
		t.Fatal(err)
	}
	third := map[string]interface{}{}
	if err := PrepareMasterNumber("default", "", dt, true, third); err != nil || third["code"] != "ZQ0004" {
		t.Errorf("third code = %v (%v), want ZQ0004 (ZQ0003 is taken)", third["code"], err)
	}

	// A refused save gives its number back - only the latest one can go.
	ReleaseMasterNumber("default", dt, "ZQ0002")
	ReleaseMasterNumber("default", dt, "ZQ0004")
	again := map[string]interface{}{}
	if err := PrepareMasterNumber("default", "", dt, true, again); err != nil || again["code"] != "ZQ0004" {
		t.Errorf("after giving ZQ0004 back the next code = %v (%v), want ZQ0004", again["code"], err)
	}

	// A typed code, an update and a typed-code master are left alone.
	typed := map[string]interface{}{"code": "MINE"}
	_ = PrepareMasterNumber("default", "", dt, true, typed)
	upd := map[string]interface{}{}
	_ = PrepareMasterNumber("default", "", dt, false, upd)
	cur := map[string]interface{}{"name": "Dirham"}
	_ = PrepareMasterNumber("default", "", "Currency", true, cur)
	if typed["code"] != "MINE" || upd["code"] != nil || cur["code"] != nil {
		t.Errorf("numbered something it must not: typed %v, update %v, currency %v", typed["code"], upd["code"], cur["code"])
	}

	if series, _ := GetMasterCodeSeries("default", dt); !series.Configured || series.Prefix != "ZQ" || series.Length != 4 {
		t.Errorf("the first record's format is the confirmed one: %+v", series)
	}

	// A bad first format is refused with the series' own message.
	if _, err := db.DB.Exec("DELETE FROM "+schema+".prefix_configs WHERE doc_type = $1", dt); err != nil {
		t.Fatal(err)
	}
	bad := map[string]interface{}{"_code_format": map[string]interface{}{"prefix": "9Z", "length": float64(4)}}
	if err := PrepareMasterNumber("default", "", dt, true, bad); err == nil {
		t.Errorf("a prefix starting with a digit must be refused")
	}
}

// preserveSeries snapshots the number series (prefix_configs row and its
// never-resetting counter) of the given doctypes and puts them back exactly
// when the test ends. A test that numbers a master in the shared "default"
// tenant otherwise confirms that master's format for the developer's own
// database - the first record decides it for good (Stage 76.5).
func preserveSeries(t *testing.T, schema string, doctypes ...string) {
	t.Helper()
	snap := func(table, keyCol string) []string {
		rows, err := db.DB.Query("SELECT row_to_json(x)::text FROM "+schema+"."+table+" x WHERE "+keyCol+" = ANY($1)", pqStringArray(doctypes))
		if err != nil {
			t.Fatalf("snapshot %s: %v", table, err)
		}
		defer rows.Close()
		var out []string
		for rows.Next() {
			var s string
			if err := rows.Scan(&s); err != nil {
				t.Fatalf("snapshot %s: %v", table, err)
			}
			out = append(out, s)
		}
		return out
	}
	configs, counters := snap("prefix_configs", "doc_type"), snap("sequence_counters", "doc_type")
	t.Cleanup(func() {
		for _, table := range []string{"prefix_configs", "sequence_counters"} {
			_, _ = db.DB.Exec("DELETE FROM "+schema+"."+table+" WHERE doc_type = ANY($1)", pqStringArray(doctypes))
		}
		for table, rows := range map[string][]string{"prefix_configs": configs, "sequence_counters": counters} {
			for _, row := range rows {
				if _, err := db.DB.Exec("INSERT INTO "+schema+"."+table+" SELECT * FROM json_populate_record(NULL::"+schema+"."+table+", $1::json)", row); err != nil {
					t.Errorf("restore %s: %v", table, err)
				}
			}
		}
	})
}

// shortMasterCode is the shape every numbered master code takes since Stage
// 76.5: a 1-6 character prefix starting with a letter, then 3-8 digits.
var shortMasterCode = regexp.MustCompile(`^[A-Z][A-Z0-9]{0,5}[0-9]{3,8}$`)
