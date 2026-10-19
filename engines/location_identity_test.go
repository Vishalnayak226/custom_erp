package engines

import (
	"custom_erp/db"
	"encoding/json"
	"fmt"
	"strconv"
	"testing"
	"time"
)

// Stage 108: the standard Short Code, and a new location's blank Short /
// Facility Code filled in.
func TestSuggestLocationShortCode(t *testing.T) {
	for name, want := range map[string]string{
		"JP Nagar":                         "JPN",
		"MG Road":                          "MGR",
		"Koramangala":                      "KOR",
		"Main Warehouse":                   "MWA",
		"HO":                               "HO",
		"Indira Nagar 2nd Stage Bangalore": "IN2S",
		"jayanagar 4th block":              "J4B",
		"  ":                               "",
	} {
		if got := SuggestLocationShortCode(name); got != want {
			t.Errorf("SuggestLocationShortCode(%q) = %q, want %q", name, got, want)
		}
	}
}

func TestPrepareNewLocationCodes(t *testing.T) {
	db.InitDB(testConnStr())
	schema, err := db.GetTenantSchema("default")
	if err != nil {
		t.Fatal(err)
	}
	tag := fmt.Sprintf("T108%d", time.Now().UnixNano()%1_000_000_000)
	t.Cleanup(func() { _, _ = db.DB.Exec("DELETE FROM "+schema+".documents WHERE id LIKE $1", tag+"%") })
	insert := func(id string, data map[string]interface{}) {
		t.Helper()
		raw, _ := json.Marshal(data)
		if _, err := db.DB.Exec("INSERT INTO "+schema+".documents (id, doctype, data, status, created_by) VALUES ($1, 'Location', $2, 'Active', 'system')", id, raw); err != nil {
			t.Fatal(err)
		}
	}

	first := map[string]interface{}{"name": "Qzx Vwy", "type": "Store"}
	if err := PrepareNewLocationCodes("default", first); err != nil {
		t.Fatal(err)
	}
	short1, _ := first["short_code"].(string)
	if short1 == "" || short1[:3] != "QVW" {
		t.Errorf("short_code = %q, want the suggestion QVW (a digit added only on a clash)", short1)
	}
	fac1, err := strconv.Atoi(fmt.Sprint(first["facility_code"]))
	if err != nil || fac1 < 1 {
		t.Fatalf("facility_code = %v, want a number from 1 up", first["facility_code"])
	}
	insert(tag+"-A", first)

	second := map[string]interface{}{"name": "Qzx Vwy", "type": "Store"}
	if err := PrepareNewLocationCodes("default", second); err != nil {
		t.Fatal(err)
	}
	if second["short_code"] == short1 {
		t.Errorf("a second store with the same name got the same short code %q", short1)
	}
	if second["facility_code"] != strconv.Itoa(fac1+1) {
		t.Errorf("facility_code = %v, want the next number %d", second["facility_code"], fac1+1)
	}

	// Codes the user typed are kept.
	typed := map[string]interface{}{"name": "Qzx Vwy", "short_code": "MINE", "facility_code": "F-7"}
	if err := PrepareNewLocationCodes("default", typed); err != nil {
		t.Fatal(err)
	}
	if typed["short_code"] != "MINE" || typed["facility_code"] != "F-7" {
		t.Errorf("typed codes changed: %v / %v", typed["short_code"], typed["facility_code"])
	}
}
