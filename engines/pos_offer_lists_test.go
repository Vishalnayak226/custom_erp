package engines

import (
	"context"
	"custom_erp/db"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
)

// 215.10: an offer's item lists from a CSV of offer, list, code rows -
// preview writes nothing, apply replaces each named list, and an offer with
// any bad row (or one its own check refuses) is left as it was.
func TestImportOfferItemLists(t *testing.T) {
	db.InitDB(testConnStr())
	tenantID := "default"
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		t.Fatalf("schema: %v", err)
	}
	const p = "T215OL-"
	cleanup := func() {
		db.DB.Exec(fmt.Sprintf(`DELETE FROM %s.documents WHERE id LIKE $1`, schema), p+"%")
	}
	cleanup()
	defer cleanup()
	insert := func(id, doctype string, data map[string]interface{}) {
		t.Helper()
		data["status"] = "Active"
		raw, _ := json.Marshal(data)
		if _, err := db.DB.Exec(fmt.Sprintf(`INSERT INTO %s.documents (id, doctype, data, status, created_by) VALUES ($1, $2, $3, 'Active', 'system')`, schema), p+id, doctype, string(raw)); err != nil {
			t.Fatalf("seed %s: %v", id, err)
		}
	}
	insert("FAM", "ProductFamily", map[string]interface{}{"code": p + "FAMCODE", "design_number": "D-OL1", "name": "List design"})
	insert("IT1", "Item", map[string]interface{}{"code": "T215OL-SKU1", "name": "one", "barcode": "8900215001"})
	insert("IT2", "Item", map[string]interface{}{"code": "T215OL-SKU2", "name": "two"})
	insert("SKU", "Offer", map[string]interface{}{"name": "T215OL SKU deal", "offer_type": "Percentage Off", "scope": "SKU List", "scope_value": "OLD1, OLD2", "discount_pct": 10})
	insert("BAGB", "Offer", map[string]interface{}{"name": "T215OL Tie deal", "offer_type": "Buy A Get B", "scope": "Bill", "buy_qty": 1, "get_qty": 1, "get_list": "X"})
	insert("COMBO", "Offer", map[string]interface{}{"name": "T215OL Combo deal", "offer_type": "Combo", "scope": "Bill", "combo_lists": "A\nB", "offer_price": 999})
	insert("PCT", "Offer", map[string]interface{}{"name": "T215OL Pct deal", "offer_type": "Percentage Off", "scope": "Bill", "discount_pct": 5})

	run := func(csv string, dry bool) *OfferListImportResult {
		t.Helper()
		res, err := ImportOfferItemLists(context.Background(), tenantID, "system", "", strings.NewReader(csv), dry)
		if err != nil {
			t.Fatalf("import: %v", err)
		}
		return res
	}
	field := func(id, f string) string {
		t.Helper()
		var v string
		if err := db.DB.QueryRow(fmt.Sprintf(`SELECT COALESCE(data->>$2, '') FROM %s.documents WHERE id = $1`, schema), p+id, f).Scan(&v); err != nil {
			t.Fatalf("read %s.%s: %v", id, f, err)
		}
		return v
	}

	good := "offer,list,code\n" +
		p + "SKU,Applies to,8900215001\n" + // by id; a barcode becomes the item's code
		"t215ol sku deal,Applies to,t215ol-sku2\n" + // by name, any case
		"T215OL SKU deal,Applies to,T215OL-SKU2\n" + // said twice, kept once
		"T215OL Tie deal,Get,D-OL1\n" + // a design
		"T215OL Tie deal,Exclude,T215OL-SKU1\n" +
		"T215OL Combo deal,Combo 1,T215OL-SKU1\n" +
		"T215OL Combo deal,combo 2,D-OL1\n" +
		",,\n" // a blank line is not a row

	t.Run("preview writes nothing", func(t *testing.T) {
		res := run(good, true)
		if res.TotalRows != 7 || res.FailedRows != 0 || len(res.Errors) != 0 {
			t.Fatalf("want 7 rows, none failed: %+v", res)
		}
		got := map[string]string{}
		for _, c := range res.Changes {
			got[c.Offer+"/"+c.List] = fmt.Sprintf("%d>%d", c.Before, c.Codes)
		}
		want := map[string]string{
			"T215OL SKU deal/Applies to": "2>2", "T215OL Tie deal/Get": "1>1", "T215OL Tie deal/Exclude": "0>1",
			"T215OL Combo deal/Combo 1": "1>1", "T215OL Combo deal/Combo 2": "1>1",
		}
		if fmt.Sprint(got) != fmt.Sprint(want) {
			t.Fatalf("changes: got %v want %v", got, want)
		}
		if v := field("SKU", "scope_value"); v != "OLD1, OLD2" {
			t.Fatalf("preview wrote %q", v)
		}
	})

	t.Run("apply replaces each named list", func(t *testing.T) {
		res := run(good, false)
		if res.FailedRows != 0 || len(res.Changes) != 5 {
			t.Fatalf("apply: %+v", res)
		}
		for id, want := range map[string][2]string{
			"SKU":   {"scope_value", "T215OL-SKU1, T215OL-SKU2"},
			"BAGB":  {"get_list", "D-OL1"},
			"COMBO": {"combo_lists", "T215OL-SKU1\nD-OL1"},
		} {
			if v := field(id, want[0]); v != want[1] {
				t.Errorf("%s.%s = %q, want %q", id, want[0], v, want[1])
			}
		}
		if v := field("BAGB", "exclude_list"); v != "T215OL-SKU1" {
			t.Errorf("exclude_list = %q", v)
		}
		if v := field("BAGB", "name"); v != "T215OL Tie deal" {
			t.Errorf("the rest of the offer must stay: name %q", v)
		}
	})

	t.Run("bad rows leave their offer as it was", func(t *testing.T) {
		bad := "offer,list,code\n" +
			"Nope,Get,X\n" + // 2: no offer
			"T215OL Pct deal,Get,T215OL-SKU1\n" + // 3: Get is for Buy A Get B
			"T215OL SKU deal,Applies to,D-OL1\n" + // 4: a design in an SKU list
			"T215OL SKU deal,Applies to,T215OL-SKU1\n" + // 5: fine, but its offer is not
			"T215OL Tie deal,Get,NOPE-CODE\n" + // 6: unknown code
			"T215OL Tie deal,Bogus,X\n" + // 7: unknown list
			"T215OL Combo deal,Combo 1,T215OL-SKU2\n" // 8: one list - the offer's own check refuses
		res := run(bad, false)
		if res.FailedRows != 7 || len(res.Changes) != 0 {
			t.Fatalf("want all 7 rows failed, nothing changed: %+v", res)
		}
		msgs := map[int]string{}
		for _, e := range res.Errors {
			msgs[e.RowNumber] += e.Message + " | "
		}
		for row, part := range map[int]string{2: "No offer", 3: "Buy A Get B", 4: "is a design", 6: "No item or design", 7: "List must be", 8: "two lists"} {
			if !strings.Contains(msgs[row], part) {
				t.Errorf("row %d: want %q in %q", row, part, msgs[row])
			}
		}
		if v := field("SKU", "scope_value"); v != "T215OL-SKU1, T215OL-SKU2" {
			t.Errorf("SKU deal changed: %q", v)
		}
		if v := field("COMBO", "combo_lists"); v != "T215OL-SKU1\nD-OL1" {
			t.Errorf("Combo deal changed: %q", v)
		}
	})

	t.Run("missing column", func(t *testing.T) {
		_, err := ImportOfferItemLists(context.Background(), tenantID, "system", "", strings.NewReader("offer,code\nA,B\n"), true)
		var ve *ValidationError
		if err == nil || !strings.Contains(err.Error(), "list") {
			t.Fatalf("want missing list column, got %v (%T %v)", err, ve, ve)
		}
	})
}
