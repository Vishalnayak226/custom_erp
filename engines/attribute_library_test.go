package engines

import (
	"custom_erp/db"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"
)

func TestAttributeLibraryLoadsAndClassifies(t *testing.T) {
	if got := len(LibraryIndustries()); got < 10 {
		t.Fatalf("industries = %d, want the workbook's 10", got)
	}
	jew := LibraryAttributesFor("jew")
	if len(jew) < 60 {
		t.Fatalf("JEW attributes = %d, want its own plus the shared CORE ones", len(jew))
	}
	if jew[0].Industry != "JEW" || jew[len(jew)-1].Industry != LibraryCoreIndustry {
		t.Fatalf("want the industry's own attributes first and CORE last, got %s .. %s", jew[0].ID, jew[len(jew)-1].ID)
	}

	colour, ok := LibraryAttributeByID("JEW_PRIMARY_COLOUR")
	if !ok {
		t.Fatal("JEW_PRIMARY_COLOUR missing")
	}
	if use := LibraryAttributeUse(colour); use != "variant" {
		t.Errorf("Primary Colour use = %q, want variant", use)
	}
	if vt := LibraryValueType(colour.Type); vt != "Select" {
		t.Errorf("Primary Colour value type = %q, want Select", vt)
	}
	if values := LibraryValues(colour.ValueSet); len(values) == 0 || values[0].Label == "" {
		t.Errorf("Primary Colour has no dropdown values: %v", values)
	}

	for id, want := range map[string]string{
		"JEW_SURFACE_FINISH":     "variant-candidate", // polish
		"JEW_STONE_COLOUR":       "variant-candidate",
		"JEW_TOTAL_STONE_WEIGHT": "lot", // per serial unit
		"CORE_GTIN":              "covered",
		"CORE_SHORT_NAME":        "product",
	} {
		a, ok := LibraryAttributeByID(id)
		if !ok {
			t.Errorf("%s missing", id)
			continue
		}
		if got := LibraryAttributeUse(a); got != want {
			t.Errorf("%s use = %q, want %q", id, got, want)
		}
	}
	if LibraryCoveredBy("CORE_GTIN") == "" {
		t.Error("CORE_GTIN should name the Item field that already holds it")
	}
}

// Applying the library inside a transaction that is rolled back, so the shared
// test database keeps nothing.
func TestApplyAttributeLibraryAddsOnceAndKeepsWhatTheClientHas(t *testing.T) {
	db.InitDB(testConnStr())
	schema, err := db.GetTenantSchema("default")
	if err != nil {
		t.Fatalf("schema: %v", err)
	}
	tx, err := db.DB.Begin()
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	defer tx.Rollback()
	// Start from none of these, whatever the shared database holds.
	ids := []string{"JEW_PRIMARY_COLOUR", "JEW_SURFACE_FINISH", "JEW_JEWELRY_TYPE"}
	for _, id := range ids {
		if _, err := tx.Exec(fmt.Sprintf(`DELETE FROM %s.documents WHERE id = $1`, schema), id); err != nil {
			t.Fatalf("clear %s: %v", id, err)
		}
	}

	picks := []AttributeLibraryPick{
		{ID: "JEW_PRIMARY_COLOUR", UsedFor: AttributeUsedForVariant},
		{ID: "JEW_SURFACE_FINISH"}, // a variant candidate: blank use defaults to Variant
		{ID: "JEW_JEWELRY_TYPE", UsedFor: AttributeUsedForProduct},
		{ID: "CORE_GTIN"}, // the Item already has GTIN - never copied
	}
	res, err := applyAttributeLibraryTx(tx, schema, "test", picks)
	if err != nil {
		t.Fatalf("apply: %v", err)
	}
	if len(res.Added) != 3 || len(res.Skipped) != 1 || res.Skipped[0] != "CORE_GTIN" {
		t.Fatalf("first apply = %+v, want 3 added and CORE_GTIN skipped", res)
	}
	var raw string
	if err := tx.QueryRow(fmt.Sprintf(`SELECT data::text FROM %s.documents WHERE id = 'JEW_PRIMARY_COLOUR' AND doctype = 'ProductAttributeDef'`, schema)).Scan(&raw); err != nil {
		t.Fatalf("read colour: %v", err)
	}
	var d map[string]interface{}
	_ = json.Unmarshal([]byte(raw), &d)
	if d["used_for"] != AttributeUsedForVariant || d["library_id"] != "JEW_PRIMARY_COLOUR" || d["value_type"] != "Select" ||
		!strings.Contains(fmt.Sprint(d["value_options"]), "Gold") || fmt.Sprint(d["group"]) == "" {
		t.Fatalf("colour record = %v", d)
	}
	var finishUse string
	_ = tx.QueryRow(fmt.Sprintf(`SELECT data->>'used_for' FROM %s.documents WHERE id = 'JEW_SURFACE_FINISH'`, schema)).Scan(&finishUse)
	if finishUse != AttributeUsedForVariant {
		t.Errorf("surface finish used_for = %q, want Variant", finishUse)
	}

	// The client switches colour off and trims its values; a second apply
	// must leave that exactly as it is.
	if _, err := tx.Exec(fmt.Sprintf(`UPDATE %s.documents SET data = data || '{"status":"Inactive","value_options":"Gold"}'::jsonb WHERE id = 'JEW_PRIMARY_COLOUR'`, schema)); err != nil {
		t.Fatalf("client edit: %v", err)
	}
	res, err = applyAttributeLibraryTx(tx, schema, "test", picks)
	if err != nil {
		t.Fatalf("second apply: %v", err)
	}
	if len(res.Added) != 0 || len(res.Skipped) != 4 {
		t.Fatalf("second apply = %+v, want everything skipped", res)
	}
	var status, options string
	_ = tx.QueryRow(fmt.Sprintf(`SELECT data->>'status', data->>'value_options' FROM %s.documents WHERE id = 'JEW_PRIMARY_COLOUR'`, schema)).Scan(&status, &options)
	if status != "Inactive" || options != "Gold" {
		t.Fatalf("after re-apply colour = %s / %s, want the client's Inactive / Gold kept", status, options)
	}

	if _, err := applyAttributeLibraryTx(tx, schema, "test", []AttributeLibraryPick{{ID: "NOPE_X"}}); !errors.Is(err, ErrUnknownLibraryAttribute) {
		t.Fatalf("unknown id error = %v, want ErrUnknownLibraryAttribute", err)
	}
}
