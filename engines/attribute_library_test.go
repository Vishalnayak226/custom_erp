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
// test database keeps nothing. Stage 86.3: a variant attribute lands in the
// ERP's ItemAttribute list and, for a client with PIM, in PIM's list too,
// mapped onto it; product information lands in PIM's list only.
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
		if _, err := tx.Exec(fmt.Sprintf(`DELETE FROM %s.documents WHERE id IN ($1, $2)`, schema), id, itemAttributeID(id)); err != nil {
			t.Fatalf("clear %s: %v", id, err)
		}
	}

	picks := []AttributeLibraryPick{
		{ID: "JEW_PRIMARY_COLOUR", UsedFor: AttributeUsedForVariant},
		{ID: "JEW_SURFACE_FINISH"}, // a variant candidate: blank use defaults to Variant
		{ID: "JEW_JEWELRY_TYPE", UsedFor: AttributeUsedForProduct},
		{ID: "CORE_GTIN"}, // the Item already has GTIN - never copied
	}
	res, err := applyAttributeLibraryTx(tx, schema, "test", picks, true)
	if err != nil {
		t.Fatalf("apply: %v", err)
	}
	if len(res.Added) != 3 || len(res.Skipped) != 1 || res.Skipped[0] != "CORE_GTIN" {
		t.Fatalf("first apply = %+v, want 3 added and CORE_GTIN skipped", res)
	}
	read := func(id, doctype string) map[string]interface{} {
		var raw string
		if err := tx.QueryRow(fmt.Sprintf(`SELECT data::text FROM %s.documents WHERE id = $1 AND doctype = $2`, schema), id, doctype).Scan(&raw); err != nil {
			t.Fatalf("read %s %s: %v", doctype, id, err)
		}
		var d map[string]interface{}
		_ = json.Unmarshal([]byte(raw), &d)
		return d
	}
	erp := read(itemAttributeID("JEW_PRIMARY_COLOUR"), ItemAttributeDoctype)
	if erp["code"] != "JEW_PRIMARY_COLOUR" || erp["library_id"] != "JEW_PRIMARY_COLOUR" || erp["value_type"] != "Select" ||
		!strings.Contains(fmt.Sprint(erp["value_options"]), "Gold") || erp["group"] != nil || erp["used_for"] != nil {
		t.Fatalf("ERP colour record = %v", erp)
	}
	pim := read("JEW_PRIMARY_COLOUR", "ProductAttributeDef")
	if pim["used_for"] != AttributeUsedForVariant || pim["erp_attribute"] != itemAttributeID("JEW_PRIMARY_COLOUR") || fmt.Sprint(pim["group"]) == "" {
		t.Fatalf("PIM colour record = %v, want Variant, mapped onto the ERP one, with its group", pim)
	}
	if d := read("JEW_JEWELRY_TYPE", "ProductAttributeDef"); d["used_for"] != AttributeUsedForProduct || d["erp_attribute"] != nil {
		t.Fatalf("PIM jewellery type = %v, want product information with no ERP attribute", d)
	}
	var erpType bool
	_ = tx.QueryRow(fmt.Sprintf(`SELECT EXISTS (SELECT 1 FROM %s.documents WHERE id = $1)`, schema), itemAttributeID("JEW_JEWELRY_TYPE")).Scan(&erpType)
	if erpType {
		t.Errorf("product information reached the ERP list")
	}
	_ = read(itemAttributeID("JEW_SURFACE_FINISH"), ItemAttributeDoctype)

	// The client switches colour off and trims its values; a second apply
	// must leave that exactly as it is.
	if _, err := tx.Exec(fmt.Sprintf(`UPDATE %s.documents SET data = data || '{"status":"Inactive","value_options":"Gold"}'::jsonb WHERE id = $1`, schema), itemAttributeID("JEW_PRIMARY_COLOUR")); err != nil {
		t.Fatalf("client edit: %v", err)
	}
	res, err = applyAttributeLibraryTx(tx, schema, "test", picks, true)
	if err != nil {
		t.Fatalf("second apply: %v", err)
	}
	if len(res.Added) != 0 || len(res.Skipped) != 4 {
		t.Fatalf("second apply = %+v, want everything skipped", res)
	}
	if d := read(itemAttributeID("JEW_PRIMARY_COLOUR"), ItemAttributeDoctype); d["status"] != "Inactive" || d["value_options"] != "Gold" {
		t.Fatalf("after re-apply ERP colour = %v, want the client's Inactive / Gold kept", d)
	}

	// A client without PIM: the variant goes to the ERP list alone, and
	// product information has nowhere to go.
	for _, id := range ids {
		if _, err := tx.Exec(fmt.Sprintf(`DELETE FROM %s.documents WHERE id IN ($1, $2)`, schema), id, itemAttributeID(id)); err != nil {
			t.Fatalf("clear %s: %v", id, err)
		}
	}
	res, err = applyAttributeLibraryTx(tx, schema, "test", picks[:3], false)
	if err != nil {
		t.Fatalf("apply without PIM: %v", err)
	}
	if len(res.Added) != 2 || len(res.Skipped) != 1 || res.Skipped[0] != "JEW_JEWELRY_TYPE" {
		t.Fatalf("apply without PIM = %+v, want the two variants added and the product one skipped", res)
	}
	var pimRows int
	_ = tx.QueryRow(fmt.Sprintf(`SELECT COUNT(*) FROM %s.documents WHERE doctype = 'ProductAttributeDef' AND id = ANY($1)`, schema), "{"+strings.Join(ids, ",")+"}").Scan(&pimRows)
	if pimRows != 0 {
		t.Errorf("%d PIM attribute(s) made for a client without PIM", pimRows)
	}

	if _, err := applyAttributeLibraryTx(tx, schema, "test", []AttributeLibraryPick{{ID: "NOPE_X"}}, true); !errors.Is(err, ErrUnknownLibraryAttribute) {
		t.Fatalf("unknown id error = %v, want ErrUnknownLibraryAttribute", err)
	}
}
