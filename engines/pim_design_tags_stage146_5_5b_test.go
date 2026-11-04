package engines

import (
	"errors"
	"testing"

	"custom_erp/db"
)

// Stage 146.5.5 (tags part): add or remove a tag on the content of many
// products at once.

func s146555tags(t *testing.T, schema, item string) string {
	t.Helper()
	var v string
	_ = db.DB.QueryRow("SELECT COALESCE(data->>'tags','') FROM "+schema+".documents WHERE id=$1", "S146555T-C-"+item).Scan(&v)
	return v
}

func s146555tagSetup(t *testing.T) (*s14655Fixture, string) {
	f := s14655Setup(t)
	for _, it := range []string{f.varA1, f.varA2, f.varB1, f.designA} {
		pimInsertDoc(t, f.schema, "S146555T-C-"+it, "ProductContent", "Draft",
			map[string]interface{}{"code": "S146555T-C-" + it, "product_id": it, "language": "en", "title": "t", "tags": "red, Summer"})
	}
	t.Cleanup(func() {
		_, _ = db.DB.Exec("DELETE FROM " + f.schema + ".documents WHERE id LIKE 'S146555T-%'")
	})
	return f, f.schema
}

func TestStage146555TagsAddAndRemoveAcrossGroup(t *testing.T) {
	f, schema := s146555tagSetup(t)
	res, err := BulkTagItems("default", "system", "Super Admin", ItemTagRequest{Group: "S14655-GRP", Action: "add", Tags: "Sale, summer, new"})
	if err != nil {
		t.Fatalf("add: %v", err)
	}
	if res.Changed != 3 {
		t.Fatalf("changed = %d, want 3 (%+v)", res.Changed, res)
	}
	if got := s146555tags(t, schema, f.varA1); got != "red,Summer,Sale,new" {
		t.Fatalf("tags = %q (summer already there, case-insensitive)", got)
	}
	if got := s146555tags(t, schema, f.designA); got != "red, Summer" {
		t.Fatalf("design not in the group was touched: %q", got)
	}
	again, _ := BulkTagItems("default", "system", "Super Admin", ItemTagRequest{Group: "S14655-GRP", Action: "add", Tags: "Sale"})
	if again.Changed != 0 || again.Unchanged != 3 {
		t.Fatalf("second add = %+v, want 0 changed 3 unchanged", again)
	}
	rm, err := BulkTagItems("default", "system", "Super Admin", ItemTagRequest{Group: "S14655-GRP", Action: "remove", Tags: "RED,sale"})
	if err != nil || rm.Changed != 3 {
		t.Fatalf("remove = %+v, %v", rm, err)
	}
	if got := s146555tags(t, schema, f.varB1); got != "Summer,new" {
		t.Fatalf("after remove = %q", got)
	}
}

func TestStage146555TagsOnDesignsReachVariants(t *testing.T) {
	f, schema := s146555tagSetup(t)
	res, err := BulkTagItems("default", "system", "Super Admin", ItemTagRequest{Designs: []string{f.designA}, Action: "add", Tags: "gift"})
	if err != nil || res.Changed != 3 {
		t.Fatalf("designs add = %+v, %v (design + its 2 variants)", res, err)
	}
	if got := s146555tags(t, schema, f.varB1); got != "red, Summer" {
		t.Fatalf("other design touched: %q", got)
	}
}

func TestStage146555TagsRefusalsAndCap(t *testing.T) {
	f, schema := s146555tagSetup(t)
	code := func(err error) string {
		var ve *ValidationError
		if errors.As(err, &ve) {
			return ve.Code
		}
		return ""
	}
	if _, err := BulkTagItems("default", "system", "Super Admin", ItemTagRequest{Group: "S14655-GRP", Action: "flip", Tags: "x"}); code(err) != "PIM-0970" {
		t.Fatalf("bad action: %v", err)
	}
	if _, err := BulkTagItems("default", "system", "Super Admin", ItemTagRequest{Group: "S14655-GRP", Action: "add", Tags: " , "}); code(err) != "PIM-0970" {
		t.Fatalf("no tags: %v", err)
	}
	if _, err := BulkTagItems("default", "system", "Super Admin", ItemTagRequest{Action: "add", Tags: "x"}); code(err) != "PIM-0959" {
		t.Fatalf("no targets: %v", err)
	}
	if err := SetSetting("default", "pim.max_bulk_edit_documents", "2", "test"); err != nil {
		t.Fatal(err)
	}
	defer SetSetting("default", "pim.max_bulk_edit_documents", "100", "test")
	if _, err := BulkTagItems("default", "system", "Super Admin", ItemTagRequest{Group: "S14655-GRP", Action: "add", Tags: "x"}); code(err) != "PIM-0962" {
		t.Fatalf("over cap: %v", err)
	}
	if got := s146555tags(t, schema, f.varA1); got != "red, Summer" {
		t.Fatalf("refused call wrote %q", got)
	}
}
