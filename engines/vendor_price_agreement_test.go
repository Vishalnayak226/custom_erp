package engines

import (
	"custom_erp/db"
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

// Stage 148.6 fixtures, shared with the 148.7 and 146.4.2 tests.
type agreementFixture struct {
	t      *testing.T
	schema string
	ids    []string
}

func newAgreementFixture(t *testing.T) *agreementFixture {
	db.InitDB(testConnStr())
	schema, err := db.GetTenantSchema("default")
	if err != nil {
		t.Fatalf("schema: %v", err)
	}
	return &agreementFixture{t: t, schema: schema}
}

func (f *agreementFixture) insert(id, doctype, status string, data map[string]interface{}) {
	f.t.Helper()
	raw, _ := json.Marshal(data)
	f.ids = append(f.ids, id)
	if _, err := db.DB.Exec("INSERT INTO "+f.schema+".documents (id, doctype, data, status, created_by) VALUES ($1, $2, $3, $4, 'system') ON CONFLICT (id) DO UPDATE SET data = EXCLUDED.data, status = EXCLUDED.status", id, doctype, raw, status); err != nil {
		f.t.Fatalf("insert %s: %v", id, err)
	}
}

func (f *agreementFixture) item(sku, design, category string, gst float64) {
	f.insert(sku, "Item", "Active", map[string]interface{}{"code": sku, "name": sku + " name", "hsn_code": "6204", "gst_rate": gst,
		"tax_treatment": "Taxable", "parent_product_code": design, "category": category})
}

func (f *agreementFixture) agreement(id, vendor, from, to string, rows ...map[string]interface{}) {
	raw, _ := json.Marshal(rows)
	f.insert(id, "PriceListVersion", "Approved", map[string]interface{}{"code": id, "price_list_code": id, "vendor": vendor,
		"effective_from": from, "effective_to": to, "currency": "INR", "status": "Approved", "items": string(raw)})
}

func (f *agreementFixture) cleanup() {
	for _, id := range f.ids {
		db.DB.Exec("DELETE FROM "+f.schema+".gl_postings WHERE document_id = $1", id)
		db.DB.Exec("DELETE FROM "+f.schema+".documents WHERE doctype = 'StockLedgerEntry' AND data->>'voucher_id' = $1", id)
		db.DB.Exec("DELETE FROM "+f.schema+".item_cost WHERE item_code = $1", id)
		db.DB.Exec("DELETE FROM "+f.schema+".inventory_availability WHERE sku = $1", id)
		db.DB.Exec("DELETE FROM "+f.schema+".documents WHERE id = $1", id)
	}
}

func itemsJSON(rows ...map[string]interface{}) string {
	raw, _ := json.Marshal(rows)
	return string(raw)
}

func poLinesOf(t *testing.T, payload map[string]interface{}) []map[string]interface{} {
	t.Helper()
	var out []map[string]interface{}
	if err := json.Unmarshal([]byte(payload["items"].(string)), &out); err != nil {
		t.Fatalf("items: %v", err)
	}
	return out
}

func TestStage1486VendorAgreementPricesAndStatesTheRule(t *testing.T) {
	f := newAgreementFixture(t)
	defer f.cleanup()
	const (
		i1, i2, i3 = "TEST1486-I1", "TEST1486-I2", "TEST1486-I3"
		v, vExp    = "TEST1486-V", "TEST1486-VEXP"
		vTie, vNo  = "TEST1486-VTIE", "TEST1486-VNO"
	)
	f.item(i1, "TEST1486-DES", "TEST1486-CAT", 5)
	f.item(i2, "TEST1486-DES2", "TEST1486-CAT", 5)
	f.item(i3, "TEST1486-DES3", "TEST1486-CAT3", 5)
	for _, vend := range []string{v, vExp, vTie, vNo} {
		f.insert(vend, "Vendor", "Active", map[string]interface{}{"code": vend, "name": vend})
	}
	f.agreement("TEST1486-A1", v, "2026-01-01", "",
		map[string]interface{}{"sku": i1, "min_qty": 1, "max_qty": 9, "price": 100, "vendor_code": "VA-12"},
		map[string]interface{}{"sku": i1, "min_qty": 10, "price": 95, "vendor_code": "VA-12"},
		map[string]interface{}{"design": "TEST1486-DES", "price": 90},
		map[string]interface{}{"category": "TEST1486-CAT", "price": 80})
	f.agreement("TEST1486-A2", vExp, "2026-01-01", "",
		map[string]interface{}{"sku": i1, "price": 70, "valid_to": "2026-10-12"})
	f.agreement("TEST1486-A3", vTie, "2026-01-01", "",
		map[string]interface{}{"sku": i1, "price": 100, "valid_from": "2026-01-01"},
		map[string]interface{}{"sku": i1, "price": 90, "valid_from": "2026-06-01"})

	apply := func(vendor string, lines ...map[string]interface{}) map[string]interface{} {
		p := map[string]interface{}{"vendor": vendor, "order_date": "2026-11-03", "gst_mode": GSTModeExclusive, "items": itemsJSON(lines...)}
		if err := ApplyPurchaseOrderTotals("default", p); err != nil {
			t.Fatalf("ApplyPurchaseOrderTotals: %v", err)
		}
		return p
	}

	t.Run("most specific rule wins and the line says which", func(t *testing.T) {
		p := apply(v,
			map[string]interface{}{"sku": i1, "qty": 5},
			map[string]interface{}{"sku": i1, "qty": 12},
			map[string]interface{}{"sku": i2, "qty": 3},
			map[string]interface{}{"sku": i3, "qty": 2, "rate": 50})
		ls := poLinesOf(t, p)
		want := []float64{100, 95, 80, 50}
		for i, w := range want {
			if numFromInterface(ls[i]["rate"]) != w {
				t.Fatalf("line %d rate = %v, want %v (%v)", i, ls[i]["rate"], w, ls[i])
			}
		}
		if n, _ := ls[0]["rule_note"].(string); !strings.Contains(n, "VA-12") {
			t.Fatalf("line 0 note %q must name the vendor code", n)
		}
		if n, _ := ls[1]["rule_note"].(string); !strings.Contains(n, "10+ break") {
			t.Fatalf("line 1 note %q must name the break", n)
		}
		if n, _ := ls[3]["rule_note"].(string); !strings.Contains(n, "no agreement") {
			t.Fatalf("line 3 note %q must say why none matched", n)
		}
		// 5x100 + 12x95 + 3x80 + 2x50 = 500 + 1140 + 240 + 100
		if numFromInterface(p["total_amount"]) != 1980 {
			t.Fatalf("total_amount = %v, want 1980", p["total_amount"])
		}
	})
	t.Run("a typed rate is kept", func(t *testing.T) {
		ls := poLinesOf(t, apply(v, map[string]interface{}{"sku": i1, "qty": 5, "rate": 120}))
		if numFromInterface(ls[0]["rate"]) != 120 {
			t.Fatalf("typed rate overwritten: %v", ls[0])
		}
	})
	t.Run("a tie goes to the latest valid-from", func(t *testing.T) {
		ls := poLinesOf(t, apply(vTie, map[string]interface{}{"sku": i1, "qty": 1}))
		if numFromInterface(ls[0]["rate"]) != 90 {
			t.Fatalf("rate = %v, want 90 from the later valid-from", ls[0]["rate"])
		}
	})
	t.Run("an expired rule says so", func(t *testing.T) {
		ls := poLinesOf(t, apply(vExp, map[string]interface{}{"sku": i1, "qty": 1, "rate": 150}))
		if n, _ := ls[0]["rule_note"].(string); n != "no agreement: expired 12 Oct" {
			t.Fatalf("note = %q", n)
		}
		if numFromInterface(ls[0]["rate"]) != 150 {
			t.Fatalf("an expired rule must not price the line: %v", ls[0])
		}
	})
	t.Run("a vendor with no agreement prices exactly as before", func(t *testing.T) {
		in := itemsJSON(map[string]interface{}{"sku": i3, "qty": 2, "rate": 50.5})
		p := map[string]interface{}{"vendor": vNo, "order_date": "2026-11-03", "gst_mode": GSTModeExclusive, "items": in}
		if err := ApplyPurchaseOrderTotals("default", p); err != nil {
			t.Fatal(err)
		}
		if p["items"] != in {
			t.Fatalf("items changed:\n%v\n%v", in, p["items"])
		}
		if numFromInterface(p["total_amount"]) != 101 || numFromInterface(p["grand_total"]) != 106 {
			t.Fatalf("totals %v / %v", p["total_amount"], p["grand_total"])
		}
	})
	t.Run("the vendor item code finds the item in an upload", func(t *testing.T) {
		ls := poLinesOf(t, apply(v, map[string]interface{}{"vendor_code": "VA-12", "qty": 2}))
		if ls[0]["sku"] != i1 {
			t.Fatalf("sku = %v, want %s", ls[0]["sku"], i1)
		}
	})
	t.Run("overlaps, gaps and unscoped rows are refused on save", func(t *testing.T) {
		code := func(rows ...map[string]interface{}) string {
			err := ValidatePriceListVersionDocument("default", map[string]interface{}{"vendor": v, "effective_from": "2026-01-01", "items": itemsJSON(rows...)})
			var ve *ValidationError
			if errors.As(err, &ve) {
				return ve.Code
			}
			return ""
		}
		a := map[string]interface{}{"sku": i1, "min_qty": 1, "max_qty": 10, "price": 1}
		if got := code(a, map[string]interface{}{"sku": i1, "min_qty": 5, "max_qty": 20, "price": 1}); got != "PURCHA-4970" {
			t.Fatalf("overlap: %q", got)
		}
		if got := code(a, map[string]interface{}{"sku": i1, "min_qty": 20, "price": 1}); got != "PURCHA-4971" {
			t.Fatalf("gap: %q", got)
		}
		if got := code(map[string]interface{}{"price": 1}); got != "PURCHA-4972" {
			t.Fatalf("no scope: %q", got)
		}
		if got := code(a, map[string]interface{}{"sku": i1, "min_qty": 11, "price": 1}); got != "" {
			t.Fatalf("contiguous breaks refused: %q", got)
		}
		if err := ValidatePriceListVersionDocument("default", map[string]interface{}{"effective_from": "2026-01-01", "items": itemsJSON(map[string]interface{}{"sku": i1, "price": 5})}); err != nil {
			t.Fatalf("a sales price list must still save: %v", err)
		}
	})
}
