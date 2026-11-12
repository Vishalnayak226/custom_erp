package engines

import (
	"context"
	"custom_erp/db"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"
)

// 49.3.4 (rest): an import UPDATE row cannot take over a record of another
// store. The row is checked against the stored record as well as against the
// merged result, or a row naming its own store would move another store's
// record into it.
func TestStage4934ImportUpdateCannotTakeOverAnotherStoresRecord(t *testing.T) {
	db.InitDB(testConnStr())
	if scope, ok := ScopeForDoctype("Bin"); !ok || len(scope.LocationFields) == 0 {
		t.Fatal("Bin is no longer location-scoped")
	}
	sfx := fmt.Sprintf("%d", time.Now().UnixNano())
	seed := func(tag, loc string) string {
		id := "S4934-" + tag + "-" + sfx
		raw, _ := json.Marshal(map[string]interface{}{"bin_code": id, "location": loc, "status": "Active"})
		if _, err := db.DB.Exec(`INSERT INTO tenant_default.documents (id, doctype, data, status, created_by) VALUES ($1, 'Bin', $2, 'Active', 'system')`, id, raw); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { db.DB.Exec(`DELETE FROM tenant_default.documents WHERE id = $1`, id) })
		return id
	}
	mine, theirs, theirs2 := seed("M", "HO"), seed("T", "ELSEWHERE"), seed("T2", "ELSEWHERE")
	location := func(id string) (v string) {
		db.DB.QueryRow(`SELECT COALESCE(data->>'location','') FROM tenant_default.documents WHERE id = $1`, id).Scan(&v)
		return
	}
	nl := "\n"
	csv := "id,bin_code,location,status" + nl +
		mine + "," + mine + ",HO,Active" + nl + // own record, own store: fine
		theirs + "," + theirs + ",HO,Active" + nl + // takes the other store's bin by naming HO
		theirs2 + "," + theirs2 + ",," + nl // names no store: the stored one is another store's
	ctx := WithSessionStore(context.Background(), "HO")
	res, err := BulkImportCSVContext(ctx, "default", "Bin", strings.NewReader(csv), "u-store", RoleStoreManager, false)
	if err != nil {
		t.Fatal(err)
	}
	if res.SuccessRows != 1 || res.FailedRows != 2 {
		t.Fatalf("want only the own-store row imported, got %d ok / %d failed: %+v", res.SuccessRows, res.FailedRows, res.Errors)
	}
	for _, e := range res.Errors {
		if !strings.Contains(fmt.Sprint(e), "ELSEWHERE") {
			t.Errorf("a refused row should name the record's store, got %v", e)
		}
	}
	if location(theirs) != "ELSEWHERE" || location(theirs2) != "ELSEWHERE" {
		t.Fatalf("another store's bins were taken over: %q %q", location(theirs), location(theirs2))
	}

	// Full access keeps its reach: an Admin's file may update any store's bin.
	res, err = BulkImportCSVContext(ctx, "default", "Bin", strings.NewReader("id,bin_code,location,status"+nl+theirs+","+theirs+",HO,Active"+nl), "u-admin", RoleAdmin, false)
	if err != nil || res.SuccessRows != 1 {
		t.Fatalf("Admin updating another store's bin: %v %+v", err, res)
	}
}
