package engines

import (
	"context"
	"errors"
	"strings"
	"testing"

	"custom_erp/db"
)

// Stage 146.5.4: one wide product sheet - Item fields, attributes and content
// in one file, one preview, one all-or-nothing commit.

const s14654P = "S14654"

func s14654Setup(t *testing.T) string {
	schema, _, cleanup := pimTestFixture(t, s14654P)
	t.Cleanup(cleanup)
	pimInsertDoc(t, schema, s14654P+"-ATTR", "ProductAttributeDef", "Active", map[string]interface{}{"code": s14654P + "-ATTR", "label": "Material"})
	return schema
}

func s14654Sheet(rows ...string) string {
	return "id,name,hsn_code,gst_rate,attr:" + s14654P + "-ATTR,content:en:title,content:en:short_desc,bogus_col\n" + strings.Join(rows, "\n") + "\n"
}

func s14654Val(t *testing.T, schema, doctype, id, key string) (string, bool) {
	var v *string
	err := db.DB.QueryRow("SELECT data->>$3 FROM "+schema+".documents WHERE doctype=$1 AND id=$2 AND deleted_at IS NULL", doctype, id, key).Scan(&v)
	if err != nil || v == nil {
		return "", false
	}
	return *v, true
}

func TestStage14654PreviewWritesNothingThenCommitWritesAllThree(t *testing.T) {
	schema := s14654Setup(t)
	sheet := s14654Sheet(s14654P + "-N1,Ring One,7117,3,Gold,Ring title,Short text,x")
	pv, err := ImportProductSheet(context.Background(), "default", strings.NewReader(sheet), "system", "HR/Admin", false)
	if err != nil {
		t.Fatal(err)
	}
	if pv.Committed || len(pv.Errors) != 0 || pv.Items == nil || pv.Items.FailedRows != 0 {
		t.Fatalf("preview: %+v items=%+v", pv, pv.Items)
	}
	if len(pv.UnknownHeaders) != 1 || pv.UnknownHeaders[0] != "bogus_col" {
		t.Fatalf("unknown headers = %v", pv.UnknownHeaders)
	}
	if pv.Attributes != 1 || pv.Contents != 1 {
		t.Fatalf("preview counts attr=%d content=%d", pv.Attributes, pv.Contents)
	}
	if _, ok := s14654Val(t, schema, "Item", s14654P+"-N1", "name"); ok {
		t.Fatal("the preview wrote the item")
	}
	res, err := ImportProductSheet(context.Background(), "default", strings.NewReader(sheet), "system", "HR/Admin", true)
	if err != nil || !res.Committed {
		t.Fatalf("commit: %v %+v", err, res)
	}
	if v, _ := s14654Val(t, schema, "Item", s14654P+"-N1", "name"); v != "Ring One" {
		t.Fatalf("item name = %q", v)
	}
	if v, _ := s14654Val(t, schema, "ProductAttributeValue", s14654P+"-N1::"+s14654P+"-ATTR", "value"); v != "Gold" {
		t.Fatalf("attribute = %q", v)
	}
	if v, _ := s14654Val(t, schema, "ProductContent", s14654P+"-N1::en", "title"); v != "Ring title" {
		t.Fatalf("content title = %q", v)
	}
	var status string
	_ = db.DB.QueryRow("SELECT status FROM "+schema+".documents WHERE doctype='ProductContent' AND id=$1", s14654P+"-N1::en").Scan(&status)
	if status != "Draft" {
		t.Fatalf("content status = %q, want Draft (never Approved from a sheet)", status)
	}
}

func TestStage14654BlankKeepsAndClearMarkerErases(t *testing.T) {
	schema := s14654Setup(t)
	first := s14654Sheet(s14654P + "-N2,Ring Two,7117,3,Gold,Title two,Short two,")
	if r, err := ImportProductSheet(context.Background(), "default", strings.NewReader(first), "system", "HR/Admin", true); err != nil || !r.Committed {
		t.Fatalf("first: %v %+v", err, r)
	}
	blank := s14654Sheet(s14654P + "-N2,Ring Two,7117,3,,,,")
	if r, err := ImportProductSheet(context.Background(), "default", strings.NewReader(blank), "system", "HR/Admin", true); err != nil || !r.Committed {
		t.Fatalf("blank: %v %+v", err, r)
	}
	if v, _ := s14654Val(t, schema, "ProductAttributeValue", s14654P+"-N2::"+s14654P+"-ATTR", "value"); v != "Gold" {
		t.Fatalf("a blank cell erased the attribute: %q", v)
	}
	if v, _ := s14654Val(t, schema, "ProductContent", s14654P+"-N2::en", "title"); v != "Title two" {
		t.Fatalf("a blank cell erased the title: %q", v)
	}
	clear := s14654Sheet(s14654P + "-N2,Ring Two,7117,3,<clear>,,<clear>,")
	if r, err := ImportProductSheet(context.Background(), "default", strings.NewReader(clear), "system", "HR/Admin", true); err != nil || !r.Committed {
		t.Fatalf("clear: %v %+v", err, r)
	}
	if _, ok := s14654Val(t, schema, "ProductAttributeValue", s14654P+"-N2::"+s14654P+"-ATTR", "value"); ok {
		t.Fatal("the clear marker left the attribute")
	}
	if v, _ := s14654Val(t, schema, "ProductContent", s14654P+"-N2::en", "short_desc"); v != "" {
		t.Fatalf("the clear marker left short_desc %q", v)
	}
	if v, _ := s14654Val(t, schema, "ProductContent", s14654P+"-N2::en", "title"); v != "Title two" {
		t.Fatalf("clearing one field changed another: %q", v)
	}
}

func TestStage14654OneBadRowWritesNothing(t *testing.T) {
	schema := s14654Setup(t)
	// Row 3 has an attribute but no id: it cannot be attached to an item.
	sheet := s14654Sheet(s14654P+"-N3,Ring Three,7117,3,Gold,,,", ",Nameless,7117,3,Silver,,,")
	res, err := ImportProductSheet(context.Background(), "default", strings.NewReader(sheet), "system", "HR/Admin", true)
	if err != nil {
		t.Fatal(err)
	}
	if res.Committed || len(res.Errors) == 0 || res.Errors[0].RowNumber != 3 {
		t.Fatalf("expected a refused commit with a row-3 error: %+v", res)
	}
	if _, ok := s14654Val(t, schema, "Item", s14654P+"-N3", "name"); ok {
		t.Fatal("a good row was written although another row failed")
	}
}

func TestStage14654ApprovedContentIsNotOverwritten(t *testing.T) {
	schema := s14654Setup(t)
	pimInsertDoc(t, schema, s14654P+"-N4", "Item", "Active", map[string]interface{}{"code": s14654P + "-N4", "name": "Ring Four", "hsn_code": "7117", "gst_rate": "3"})
	pimInsertDoc(t, schema, s14654P+"-N4::en", "ProductContent", "Approved", map[string]interface{}{"id": s14654P + "-N4::en", "product_id": s14654P + "-N4", "language": "en", "title": "Live title", "status": "Approved"})
	sheet := s14654Sheet(s14654P + "-N4,Ring Four,7117,3,,New title,,")
	res, err := ImportProductSheet(context.Background(), "default", strings.NewReader(sheet), "system", "HR/Admin", true)
	if err != nil {
		t.Fatal(err)
	}
	if res.Committed || len(res.Errors) == 0 {
		t.Fatalf("approved content was overwritten: %+v", res)
	}
	if v, _ := s14654Val(t, schema, "ProductContent", s14654P+"-N4::en", "title"); v != "Live title" {
		t.Fatalf("title = %q", v)
	}
}

func TestStage14654SheetNeedsAnIDColumn(t *testing.T) {
	s14654Setup(t)
	_, err := ImportProductSheet(context.Background(), "default", strings.NewReader("name,hsn_code\nRing,7117\n"), "system", "HR/Admin", false)
	if err == nil || !strings.Contains(err.Error(), "id") {
		t.Fatalf("a sheet without an id column was accepted: %v", err)
	}
}

// Review fix (S4): content approved between the preview read and the write is
// not overwritten, and the sheet says so instead of committing.
func TestStage14654ContentApprovedMidImportIsNotOverwritten(t *testing.T) {
	schema := s14654Setup(t)
	id := s14654P + "-N9::en"
	pimInsertDoc(t, schema, id, "ProductContent", "Approved", map[string]interface{}{"id": id, "title": "Live title"})
	tx, err := db.DB.Begin()
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	err = writeSheetContent(tx, schema, id, map[string]interface{}{"id": id, "title": "New"}, "system")
	if err == nil {
		t.Fatal("approved content was overwritten without an error")
	}
	if !errors.Is(err, errSheetContentMoved) {
		t.Fatalf("err = %v", err)
	}
	tx.Rollback()
	if v, _ := s14654Val(t, schema, "ProductContent", id, "title"); v != "Live title" {
		t.Fatalf("title = %q", v)
	}
}
