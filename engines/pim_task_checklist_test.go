package engines

import (
	"testing"

	"custom_erp/db"
)

// Stage 146.5.10: a task lists attributes to fill for a product set and
// completes itself when they are filled.

func TestStage146510ChecklistShowsWhatIsLeftAndCompletesItself(t *testing.T) {
	schema, itemA, cleanup := pimTestFixture(t, "S146510")
	defer cleanup()
	itemB := "S146510-B"
	pimInsertDoc(t, schema, itemB, "Item", "Active", map[string]interface{}{"code": itemB, "name": "b", "family": ""})
	for _, a := range []string{"S146510-ONE", "S146510-TWO"} {
		pimInsertDoc(t, schema, a, "ProductAttributeDef", "Active", map[string]interface{}{"code": a, "label": a, "value_type": "Data"})
	}
	pimInsertDoc(t, schema, "S146510-GRP", "PIMProductGroup", "Active", map[string]interface{}{
		"code": "S146510-GRP", "name": "g", "group_type": "Static",
		"members": []map[string]interface{}{{"item_code": itemA}, {"item_code": itemB}}})
	defer db.DB.Exec("DELETE FROM " + schema + ".documents WHERE id LIKE 'S146510%'")

	id, err := CreatePIMChecklistTask("default", "system", PIMChecklistRequest{
		Title: "Fill basics", ScopeType: "Product Group", ScopeRef: "S146510-GRP", Attributes: []string{"S146510-ONE", "S146510-TWO"}})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	defer db.DB.Exec("DELETE FROM "+schema+".documents WHERE id=$1", id)

	cl, err := PIMTaskChecklistFor("default", "system", id)
	if err != nil {
		t.Fatal(err)
	}
	if cl.Products != 2 || cl.Complete != 0 || cl.Status != "Open" || len(cl.Rows) != 2 || len(cl.Rows[0].Missing) != 2 {
		t.Fatalf("start: %+v", cl)
	}
	set := func(item, attr, v string) {
		pimInsertDoc(t, schema, "S146510-V-"+item+attr, "ProductAttributeValue", "Active", map[string]interface{}{"item": item, "attribute": attr, "value": v})
	}
	set(itemA, "S146510-ONE", "x")
	set(itemA, "S146510-TWO", "y")
	set(itemB, "S146510-ONE", "z")
	cl, _ = PIMTaskChecklistFor("default", "system", id)
	if cl.Complete != 1 || cl.Status != "Open" {
		t.Fatalf("half way: %+v", cl)
	}
	for _, r := range cl.Rows {
		if r.Item == itemB && (len(r.Missing) != 1 || r.Missing[0] != "S146510-TWO") {
			t.Fatalf("item B should still miss TWO: %+v", r)
		}
	}
	set(itemB, "S146510-TWO", "w")
	cl, _ = PIMTaskChecklistFor("default", "system", id)
	if cl.Complete != 2 || cl.Status != "Done" {
		t.Fatalf("all filled: %+v (task should have completed itself)", cl)
	}
	var st string
	_ = db.DB.QueryRow("SELECT status FROM "+schema+".documents WHERE id=$1", id).Scan(&st)
	if st != "Done" {
		t.Fatalf("stored status %q", st)
	}
}

func TestStage146510ChecklistRefusesEmptyOrUnknown(t *testing.T) {
	_, itemA, cleanup := pimTestFixture(t, "S146510R")
	defer cleanup()
	if _, err := CreatePIMChecklistTask("default", "system", PIMChecklistRequest{Title: "t", ScopeType: "Product", ScopeRef: itemA}); err == nil {
		t.Fatal("no attributes should be refused")
	}
	if _, err := CreatePIMChecklistTask("default", "system", PIMChecklistRequest{Title: "t", ScopeType: "Product", ScopeRef: itemA, Attributes: []string{"NOPE"}}); err == nil {
		t.Fatal("unknown attribute should be refused")
	}
}
