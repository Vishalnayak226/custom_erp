package engines

import (
	"custom_erp/db"
	"encoding/json"
	"errors"
	"fmt"
	"testing"
	"time"
)

// Ask 215.7/215.8: the new offer types (Buy A Get B, slabs, fixed price,
// combo, get at a % off, Design List) and the conditions (exclusions, days
// and hours, stores, use limits), on the shared dev DB with its own prefix.
func TestPOSOfferTypes(t *testing.T) {
	db.InitDB(testConnStr())
	tenantID := "default"
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		t.Fatalf("schema: %v", err)
	}
	const p = "T215OFF-"
	cleanup := func() {
		db.DB.Exec(fmt.Sprintf(`DELETE FROM %s.documents WHERE id LIKE $1`, schema), p+"%")
	}
	cleanup()
	defer cleanup()
	insert := func(id, doctype, status string, data map[string]interface{}) {
		t.Helper()
		raw, _ := json.Marshal(data)
		if _, err := db.DB.Exec(fmt.Sprintf(`INSERT INTO %s.documents (id, doctype, data, status, created_by) VALUES ($1, $2, $3, $4, 'system')`, schema), p+id, doctype, string(raw), status); err != nil {
			t.Fatalf("seed %s: %v", id, err)
		}
	}
	clearOffers := func() {
		db.DB.Exec(fmt.Sprintf(`DELETE FROM %s.documents WHERE id LIKE $1 AND doctype = 'Offer'`, schema), p+"%")
	}
	offer := func(id string, f map[string]interface{}) {
		t.Helper()
		clearOffers()
		if _, ok := f["stackable"]; !ok {
			f["stackable"] = "No"
		}
		insert(id, "Offer", "Active", f)
	}
	type in struct {
		customer, location, cart string
	}
	eval := func(lines []OfferCartLine, o in) *OfferEvaluation {
		t.Helper()
		e, err := EvaluatePOSOffers(tenantID, OfferEvaluationInput{Lines: lines, CustomerID: o.customer, Location: o.location, CartNumber: o.cart})
		if err != nil {
			t.Fatalf("evaluate: %v", err)
		}
		return e
	}
	cart := func() []OfferCartLine { // 2 x 500 + 1 x 200 = 1200
		return []OfferCartLine{{Sku: "SKU-A", Qty: 2, SalePrice: 500}, {Sku: "SKU-B", Qty: 1, SalePrice: 200}}
	}

	// A design (ProductFamily) with two items - one pointing at the family's
	// id, one at its code - and an item outside it.
	insert("FAM1", "ProductFamily", "Active", map[string]interface{}{"code": p + "FAMCODE", "design_number": "D-215", "name": "Design 215"})
	insert("IT1", "Item", "Active", map[string]interface{}{"code": "T215-SKU1", "name": "one", "family": p + "FAM1"})
	insert("IT2", "Item", "Active", map[string]interface{}{"code": "T215-SKU2", "name": "two", "parent_product_code": p + "FAMCODE"})
	insert("IT3", "Item", "Active", map[string]interface{}{"code": "T215-SKU3", "name": "three"})
	designCart := func() []OfferCartLine {
		return []OfferCartLine{{Sku: "T215-SKU1", Qty: 2, SalePrice: 500}, {Sku: "T215-SKU2", Qty: 1, SalePrice: 300}, {Sku: "T215-SKU3", Qty: 1, SalePrice: 200}}
	}

	t.Run("Design List takes every item of a listed design", func(t *testing.T) {
		offer("DESIGN", map[string]interface{}{"name": "Design deal", "offer_type": "Percentage Off", "scope": "Design List", "scope_value": "d-215", "discount_pct": 10})
		e := eval(designCart(), in{})
		if e.TotalDiscount != 130 || e.LineDiscounts[2] != 0 {
			t.Fatalf("10%% of 1000 + 300 = 130, none on the item outside: got %v %v", e.TotalDiscount, e.LineDiscounts)
		}
	})

	t.Run("Buy X Get Y at a % off instead of free", func(t *testing.T) {
		offer("BXGY50", map[string]interface{}{"name": "B1G1 half", "offer_type": "Buy X Get Y", "scope": "Bill", "buy_qty": 1, "get_qty": 1, "get_discount_pct": 50})
		if e := eval(cart(), in{}); e.TotalDiscount != 100 {
			t.Fatalf("3 units = 1 group, cheapest 200 at 50%% = 100; got %v", e.TotalDiscount)
		}
	})

	t.Run("Buy A Get B: list B discounted, only with enough of list A", func(t *testing.T) {
		offer("BAGB", map[string]interface{}{"name": "Buy 2 shirts get a tie", "offer_type": "Buy A Get B", "scope": "SKU List", "scope_value": "T215-SKU1", "get_list": "T215-SKU3", "buy_qty": 2, "get_qty": 1})
		lines := []OfferCartLine{{Sku: "T215-SKU1", Qty: 2, SalePrice: 500}, {Sku: "T215-SKU3", Qty: 2, SalePrice: 200}}
		e := eval(lines, in{})
		if e.TotalDiscount != 200 || e.LineDiscounts[0] != 0 || e.LineDiscounts[1] != 200 {
			t.Fatalf("one tie free (200), on the tie line: %v %v", e.TotalDiscount, e.LineDiscounts)
		}
		lines[0].Qty = 1
		if e := eval(lines, in{}); e.TotalDiscount != 0 {
			t.Fatalf("one shirt is not enough: %v", e.TotalDiscount)
		}
		// A unit on both lists is never bought and got at once.
		offer("BAGB2", map[string]interface{}{"name": "Any 1 get a tie", "offer_type": "Buy A Get B", "scope": "Bill", "get_list": "T215-SKU3", "buy_qty": 1, "get_qty": 1, "get_discount_pct": 50})
		if e := eval([]OfferCartLine{{Sku: "T215-SKU3", Qty: 1, SalePrice: 200}}, in{}); e.TotalDiscount != 0 {
			t.Fatalf("one tie cannot be its own buy: %v", e.TotalDiscount)
		}
		if e := eval([]OfferCartLine{{Sku: "T215-SKU3", Qty: 2, SalePrice: 200}}, in{}); e.TotalDiscount != 100 {
			t.Fatalf("two ties: one bought, one at 50%% = 100; got %v", e.TotalDiscount)
		}
		// The get list takes designs too.
		offer("BAGB3", map[string]interface{}{"name": "Tie gets design", "offer_type": "Buy A Get B", "scope": "SKU List", "scope_value": "T215-SKU3", "get_list": "D-215", "buy_qty": 1, "get_qty": 1})
		if e := eval(designCart(), in{}); e.TotalDiscount != 300 {
			t.Fatalf("the cheapest unit of the design (300) free: %v", e.TotalDiscount)
		}
	})

	t.Run("Spend and quantity slabs take the highest slab reached", func(t *testing.T) {
		offer("SPEND", map[string]interface{}{"name": "Spend more", "offer_type": "Spend Slabs", "scope": "Bill", "slabs": "1000=100; 2,000=10%"})
		if e := eval(cart(), in{}); e.TotalDiscount != 100 {
			t.Fatalf("1200 reaches the 1000 slab: %v", e.TotalDiscount)
		}
		if e := eval([]OfferCartLine{{Sku: "SKU-A", Qty: 5, SalePrice: 500}}, in{}); e.TotalDiscount != 250 {
			t.Fatalf("2500 reaches 10%%: %v", e.TotalDiscount)
		}
		if e := eval([]OfferCartLine{{Sku: "SKU-A", Qty: 1, SalePrice: 900}}, in{}); e.TotalDiscount != 0 {
			t.Fatalf("900 reaches no slab: %v", e.TotalDiscount)
		}
		offer("QTY", map[string]interface{}{"name": "Buy more", "offer_type": "Quantity Slabs", "scope": "Bill", "slabs": "3=5%\n5=15%"})
		if e := eval(cart(), in{}); e.TotalDiscount != 60 {
			t.Fatalf("3 units = 5%% of 1200: %v", e.TotalDiscount)
		}
	})

	t.Run("Fixed Price and Combo", func(t *testing.T) {
		offer("FIXED", map[string]interface{}{"name": "All at 300", "offer_type": "Fixed Price", "scope": "SKU List", "scope_value": "SKU-A, SKU-B", "offer_price": 300})
		if e := eval(cart(), in{}); e.TotalDiscount != 400 {
			t.Fatalf("A 500 -> 300 twice; B 200 stays: %v", e.TotalDiscount)
		}
		offer("COMBO", map[string]interface{}{"name": "Shirt + tie 600", "offer_type": "Combo", "combo_lists": "SKU-A\nSKU-B", "offer_price": 600})
		e := eval(cart(), in{})
		if e.TotalDiscount != 100 || e.LineDiscounts[0]+e.LineDiscounts[1] != 100 {
			t.Fatalf("one combo 700 -> 600: %v %v", e.TotalDiscount, e.LineDiscounts)
		}
		if e := eval([]OfferCartLine{{Sku: "SKU-A", Qty: 3, SalePrice: 500}}, in{}); e.TotalDiscount != 0 {
			t.Fatalf("no combo without the second list: %v", e.TotalDiscount)
		}
	})

	t.Run("Exclusions stay out, also from a whole-bill offer", func(t *testing.T) {
		offer("EXCL", map[string]interface{}{"name": "10% but B", "offer_type": "Percentage Off", "scope": "Bill", "discount_pct": 10, "exclude_list": "sku-b"})
		e := eval(cart(), in{})
		if e.TotalDiscount != 100 || e.LineDiscounts[1] != 0 {
			t.Fatalf("10%% of A's 1000 only: %v %v", e.TotalDiscount, e.LineDiscounts)
		}
		offer("EXCLD", map[string]interface{}{"name": "10% but the design", "offer_type": "Percentage Off", "scope": "Bill", "discount_pct": 10, "exclude_list": "D-215"})
		if e := eval(designCart(), in{}); e.TotalDiscount != 20 {
			t.Fatalf("only the item outside the design (200): %v", e.TotalDiscount)
		}
	})

	t.Run("Days and hours", func(t *testing.T) {
		old := offerClock
		defer func() { offerClock = old }()
		at := func(day, h, m int) {
			offerClock = func() time.Time { return time.Date(2026, 11, day, h, m, 0, 0, BusinessLocation()) }
		}
		at(18, 19, 0)
		if offerClock().Weekday() != time.Wednesday {
			t.Fatalf("2026-11-18 should be a Wednesday")
		}
		offer("HAPPY", map[string]interface{}{"name": "Weekday evening", "offer_type": "Flat Off", "scope": "Bill", "discount_amount": 50, "days": "Mon-Fri", "hour_from": "18:00", "hour_to": "21:00"})
		if e := eval(cart(), in{}); e.TotalDiscount != 50 {
			t.Fatalf("Wednesday 19:00 is inside: %v", e.TotalDiscount)
		}
		at(18, 21, 0)
		if e := eval(cart(), in{}); e.TotalDiscount != 0 {
			t.Fatalf("21:00 is the end: %v", e.TotalDiscount)
		}
		at(21, 19, 0) // Saturday
		if e := eval(cart(), in{}); e.TotalDiscount != 0 {
			t.Fatalf("Saturday is not Mon-Fri: %v", e.TotalDiscount)
		}
		offer("NIGHT", map[string]interface{}{"name": "Midnight sale", "offer_type": "Flat Off", "scope": "Bill", "discount_amount": 50, "hour_from": "22:00", "hour_to": "02:00"})
		at(18, 23, 30)
		if e := eval(cart(), in{}); e.TotalDiscount != 50 {
			t.Fatalf("23:30 is inside 22:00-02:00: %v", e.TotalDiscount)
		}
		at(18, 12, 0)
		if e := eval(cart(), in{}); e.TotalDiscount != 0 {
			t.Fatalf("noon is outside 22:00-02:00: %v", e.TotalDiscount)
		}
		offer("BROKEN", map[string]interface{}{"name": "Bad days", "offer_type": "Flat Off", "scope": "Bill", "discount_amount": 50, "days": "Funday"})
		if e := eval(cart(), in{}); e.TotalDiscount != 0 {
			t.Fatalf("days that do not read must never widen an offer: %v", e.TotalDiscount)
		}
	})

	t.Run("Stores limit every offer type", func(t *testing.T) {
		offer("STORES", map[string]interface{}{"name": "Two stores", "offer_type": "Flat Off", "scope": "Bill", "discount_amount": 50, "stores": "S1, S2"})
		if e := eval(cart(), in{location: "s2"}); e.TotalDiscount != 50 {
			t.Fatalf("S2 is listed: %v", e.TotalDiscount)
		}
		if e := eval(cart(), in{location: "S3"}); e.TotalDiscount != 0 {
			t.Fatalf("S3 is not: %v", e.TotalDiscount)
		}
		offer("GIFT", map[string]interface{}{"name": "Gift at S1", "offer_type": "Gift With Purchase", "scope": "Bill", "gift_item": "T215-SKU3", "stores": "S1"})
		if e := eval(cart(), in{location: "S2"}); len(e.Gifts) != 0 {
			t.Fatalf("no gift at S2: %v", e.Gifts)
		}
		if e := eval(cart(), in{location: "S1"}); len(e.Gifts) != 1 {
			t.Fatalf("a gift at S1: %v", e.Gifts)
		}
	})

	t.Run("Use limits count paid bills, never the bill being priced", func(t *testing.T) {
		offer("LIMIT", map[string]interface{}{"name": "First one only", "offer_type": "Flat Off", "scope": "Bill", "discount_amount": 50, "max_uses": 1})
		if e := eval(cart(), in{cart: p + "NEW"}); e.TotalDiscount != 50 {
			t.Fatalf("unused: %v", e.TotalDiscount)
		}
		insert("CART1", "POSCart", "Paid", map[string]interface{}{"customer_id": "C1", "applied_offers": []map[string]interface{}{{"offer_id": p + "LIMIT", "discount": 50}}})
		if e := eval(cart(), in{cart: p + "NEW"}); e.TotalDiscount != 0 {
			t.Fatalf("used up: %v", e.TotalDiscount)
		}
		if e := eval(cart(), in{cart: p + "CART1"}); e.TotalDiscount != 50 {
			t.Fatalf("the paid bill itself, re-priced, keeps it: %v", e.TotalDiscount)
		}
		offer("LIMIT", map[string]interface{}{"name": "Once each", "offer_type": "Flat Off", "scope": "Bill", "discount_amount": 50, "max_uses_per_customer": 1})
		for _, c := range []struct {
			customer string
			want     float64
		}{{"C1", 0}, {"C2", 50}, {"", 0}} {
			if e := eval(cart(), in{customer: c.customer, cart: p + "NEW"}); e.TotalDiscount != c.want {
				t.Errorf("customer %q: %v, want %v", c.customer, e.TotalDiscount, c.want)
			}
		}
	})

	clearOffers()
}

// Ask 215.7/215.8: the offer form refuses what its type cannot run with,
// and leaves other types' fields alone.
func TestValidateOfferTypes(t *testing.T) {
	cases := []struct {
		name string
		doc  map[string]interface{}
		ok   bool
	}{
		{"plain percentage", map[string]interface{}{"offer_type": "Percentage Off", "scope": "Bill", "discount_pct": 10}, true},
		{"buy A get B without B", map[string]interface{}{"offer_type": "Buy A Get B", "buy_qty": 2, "get_qty": 1}, false},
		{"buy A get B", map[string]interface{}{"offer_type": "Buy A Get B", "buy_qty": 2, "get_qty": 1, "get_list": "T1, T2"}, true},
		{"buy X get Y at 120%", map[string]interface{}{"offer_type": "Buy X Get Y", "buy_qty": 1, "get_qty": 1, "get_discount_pct": 120}, false},
		{"another type's hidden get %", map[string]interface{}{"offer_type": "Percentage Off", "scope": "Bill", "discount_pct": 10, "get_discount_pct": 120}, true},
		{"slabs missing", map[string]interface{}{"offer_type": "Spend Slabs"}, false},
		{"slabs bad", map[string]interface{}{"offer_type": "Spend Slabs", "slabs": "2000-200"}, false},
		{"slabs over 100%", map[string]interface{}{"offer_type": "Quantity Slabs", "slabs": "3=150%"}, false},
		{"slabs twice from 2000", map[string]interface{}{"offer_type": "Spend Slabs", "slabs": "2000=100; 2,000=200"}, false},
		{"slabs", map[string]interface{}{"offer_type": "Spend Slabs", "slabs": "2,000=200; 5000=10%"}, true},
		{"fixed price without price", map[string]interface{}{"offer_type": "Fixed Price"}, false},
		{"combo with one list", map[string]interface{}{"offer_type": "Combo", "combo_lists": "A, B", "offer_price": 999}, false},
		{"combo", map[string]interface{}{"offer_type": "Combo", "combo_lists": "A, B\nC", "offer_price": 999}, true},
		{"design list empty", map[string]interface{}{"offer_type": "Percentage Off", "scope": "Design List", "discount_pct": 10}, false},
		{"bad day", map[string]interface{}{"offer_type": "Flat Off", "days": "Funday"}, false},
		{"bad time", map[string]interface{}{"offer_type": "Flat Off", "hour_from": "25:00"}, false},
		{"days and times", map[string]interface{}{"offer_type": "Flat Off", "days": "Weekdays, Sat", "hour_from": "09:30", "hour_to": "24:00"}, true},
		{"negative limit", map[string]interface{}{"offer_type": "Flat Off", "max_uses": -1}, false},
	}
	for _, c := range cases {
		err := validateOffer(c.doc)
		var ve *ValidationError
		switch {
		case c.ok && err != nil:
			t.Errorf("%s: refused: %v", c.name, err)
		case !c.ok && (!errors.As(err, &ve) || ve.Code != "POSOFF-0882"):
			t.Errorf("%s: want POSOFF-0882, got %v", c.name, err)
		}
	}
	if d, _ := parseOfferDays("Fri-Mon"); d == nil || !d[5] || !d[6] || !d[0] || !d[1] || d[3] {
		t.Errorf("Fri-Mon wraps over the weekend: %v", d)
	}
	if m, err := parseOfferTime("24:00"); err != nil || m != 1440 {
		t.Errorf("24:00 is the end of the day: %v %v", m, err)
	}
}
