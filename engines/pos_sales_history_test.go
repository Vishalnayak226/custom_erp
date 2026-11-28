package engines

import (
	"context"
	"custom_erp/db"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"
)

// 215.12/215.13: Sales History lists store bills, online bills and returns,
// and finds any of them by what a customer brings to the counter - the AWB,
// the channel's order number, a "+91 98765 ..." mobile, a B2B GSTIN.
func TestSearchSalesHistory(t *testing.T) {
	db.InitDB(testConnStr())
	tenantID := "default"
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		t.Fatalf("schema: %v", err)
	}
	const p = "T215SH-"
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
	const store, other = p + "ST1", p + "ST2"
	insert("CU1", "Customer", "Active", map[string]interface{}{"name": "Asha T215SH", "phone": "9876500215"})
	insert("CART1", "POSCart", "Paid", map[string]interface{}{"location": store, "bill_number": p + "B0001", "customer_id": p + "CU1",
		"amount_paid": 900, "item_discount": 50, "offer_discount": 30, "bill_discount": 20})
	insert("CART2", "POSCart", "Paid", map[string]interface{}{"location": store, "bill_number": p + "B0002", "amount_paid": 1180,
		"b2b": map[string]interface{}{"gstin": "29AABCT2150S1Z5", "legal_name": "T215SH Traders"}})
	insert("CART3", "POSCart", "Open", map[string]interface{}{"location": store, "bill_number": p + "B0003", "amount_paid": 1})
	insert("SO1", "SalesOrder", "Shipped", map[string]interface{}{"code": p + "SO-77", "channel": "Shopify", "channel_order_id": "#T215SH1001",
		"customer_name": "Ravi Online", "customer_phone": "+91 98765 43215"})
	insert("SINV1", "SalesInvoice", "Posted", map[string]interface{}{"sales_order_id": p + "SO1", "invoice_number": p + "OB-0009",
		"oms_bill_number": p + "OB-0009", "location": p + "WH", "total_amount": 1499, "shipping_package_id": p + "PK1"})
	insert("SINV2", "SalesInvoice", "Draft", map[string]interface{}{"sales_order_id": p + "SO1", "invoice_number": p + "OB-DRAFT", "location": p + "WH"})
	insert("BK1", "LogisticsBooking", "Booked", map[string]interface{}{"order_id": p + "SO1", "shipping_package_id": p + "PK1", "awb_number": "T215SHAWB42"})
	insert("RR1", "ReturnRequest", "Requested", map[string]interface{}{"code": p + "RMA-1", "rma_no": p + "RMA-1", "original_order_id": p + "CART1",
		"return_location": store, "items": []map[string]interface{}{{"sku": "X", "qty": 2, "original_unit_price": 150}}})
	insert("RR2", "ReturnRequest", "Requested", map[string]interface{}{"code": p + "RMA-2", "original_order_id": p + "SINV1",
		"return_location": other, "items": []map[string]interface{}{{"sku": "Y", "qty": 1, "original_unit_price": 1499}}})

	search := func(f SalesHistoryFilter) *SalesHistory {
		t.Helper()
		h, err := SearchSalesHistory(context.Background(), tenantID, f)
		if err != nil {
			t.Fatalf("search %+v: %v", f, err)
		}
		return h
	}
	ids := func(h *SalesHistory) map[string]SalesHistoryRow {
		m := map[string]SalesHistoryRow{}
		for _, r := range h.Rows {
			m[r.ID] = r
		}
		return m
	}

	t.Run("a store's list: its bills and returns, with totals", func(t *testing.T) {
		h := search(SalesHistoryFilter{Stores: []string{store}})
		got := ids(h)
		for _, id := range []string{"CART1", "CART2", "RR1"} {
			if _, ok := got[p+id]; !ok {
				t.Errorf("%s missing from the store's list: %v", id, got)
			}
		}
		for _, id := range []string{"CART3", "SINV1", "SINV2", "RR2"} {
			if _, ok := got[p+id]; ok {
				t.Errorf("%s must not be in the store's list", id)
			}
		}
		if h.Sales != 2 || h.SalesTotal != 2080 || h.Returns != 1 || h.ReturnsTotal != 300 {
			t.Errorf("totals: %d sales %.2f, %d returns %.2f", h.Sales, h.SalesTotal, h.Returns, h.ReturnsTotal)
		}
		if h.ItemDiscount != 50 || h.OfferDiscount != 30 || h.BillDiscount != 20 {
			t.Errorf("discounts: item %.2f offer %.2f bill %.2f", h.ItemDiscount, h.OfferDiscount, h.BillDiscount)
		}
		c1 := got[p+"CART1"]
		if !c1.Returned || c1.Customer != "Asha T215SH" || c1.Phone != "9876500215" || c1.Number != p+"B0001" {
			t.Errorf("CART1 row: %+v", c1)
		}
		if r := got[p+"RR1"]; r.Against != p+"B0001" || r.Source != "Store" || r.Customer != "Asha T215SH" {
			t.Errorf("RR1 row: %+v", r)
		}
		if c2 := got[p+"CART2"]; c2.Customer != "T215SH Traders" || c2.GSTIN != "29AABCT2150S1Z5" || c2.Returned {
			t.Errorf("CART2 row: %+v", c2)
		}
	})

	t.Run("an online bill is found at any store by what the customer has", func(t *testing.T) {
		for _, q := range []string{"t215shawb42", "#T215SH1001", "9876543215", "+91 98765 43215", p + "OB-0009", p + "SO-77", "ravi online"} {
			h := search(SalesHistoryFilter{Stores: []string{store}, Q: q, Kind: "Sale"})
			r, ok := ids(h)[p+"SINV1"]
			if !ok {
				t.Errorf("%q did not find the online bill: %+v", q, h.Rows)
				continue
			}
			if r.Source != "Online" || r.AWB != "T215SHAWB42" || r.ChannelOrderID != "#T215SH1001" || r.Total != 1499 || !r.Returned {
				t.Errorf("%q row: %+v", q, r)
			}
			if _, draft := ids(h)[p+"SINV2"]; draft {
				t.Errorf("%q found the draft invoice", q)
			}
		}
		if _, ok := ids(search(SalesHistoryFilter{Stores: []string{store}, Source: "Online"}))[p+"SINV1"]; ok {
			t.Error("with nothing typed an online bill shows only at its own location")
		}
	})

	tillSearch := func(t *testing.T) {
		for q, want := range map[string]string{"29aabct2150s1z5": "CART2", p + "B0001": "CART1", "98765 00215": "CART1", "+91-9876500215": "CART1"} {
			bills, err := SearchStoreBills(context.Background(), tenantID, []string{store}, q, "")
			if err != nil {
				t.Fatalf("%q: %v", q, err)
			}
			found := false
			for _, b := range bills {
				found = found || b.CartNumber == p+want
			}
			if !found {
				t.Errorf("%q did not find %s: %+v", q, want, bills)
			}
		}
		bills, _ := SearchStoreBills(context.Background(), tenantID, []string{store}, "T215SHAWB42", "")
		if len(bills) != 1 || bills[0].CartNumber != p+"SINV1" || bills[0].Kind != "Online" {
			t.Errorf("AWB at the till: %+v", bills)
		}
	}
	t.Run("the till's search: GSTIN, bill number, mobile", tillSearch)

	// 297.4: a book of over a lakh bills finds a store bill only by what an
	// index holds - bill number, cart id, GSTIN, the customer's full mobile.
	t.Run("a big book: the same finds, by number only", func(t *testing.T) {
		was := salesHistoryIsBig
		salesHistoryIsBig = func(string) bool { return true }
		defer func() { salesHistoryIsBig = was }()
		tillSearch(t)
		h := search(SalesHistoryFilter{Stores: []string{store}, Q: strings.ToLower(p + "CART1")})
		if _, ok := ids(h)[p+"CART1"]; !ok || h.SearchScope != "number" {
			t.Errorf("cart id in small letters: scope %q, %+v", h.SearchScope, h.Rows)
		}
		for _, q := range []string{"Asha", p + "B000", "9876500"} {
			if _, ok := ids(search(SalesHistoryFilter{Stores: []string{store}, Q: q}))[p+"CART1"]; ok {
				t.Errorf("%q found a store bill on a big book: a name or part of a number reads every bill", q)
			}
		}
		if h := search(SalesHistoryFilter{Stores: []string{store}}); h.SearchScope != "" || h.Sales != 2 {
			t.Errorf("nothing typed: scope %q, %d sales", h.SearchScope, h.Sales)
		}
	})

	t.Run("a big book: totals skipped over the cap, rows still shown", func(t *testing.T) {
		wasBig, wasCap := salesHistoryIsBig, salesHistoryTotalsCap
		salesHistoryIsBig, salesHistoryTotalsCap = func(string) bool { return true }, 0
		defer func() { salesHistoryIsBig, salesHistoryTotalsCap = wasBig, wasCap }()
		h := search(SalesHistoryFilter{Stores: []string{store}})
		if !h.TotalsSkipped || h.Sales != 0 || len(h.Rows) != 3 {
			t.Errorf("skipped=%v sales=%d rows=%d", h.TotalsSkipped, h.Sales, len(h.Rows))
		}
		salesHistoryIsBig = wasBig
		if h := search(SalesHistoryFilter{Stores: []string{store}}); h.TotalsSkipped || h.Sales != 2 {
			t.Errorf("a small book always totals: skipped=%v sales=%d", h.TotalsSkipped, h.Sales)
		}
	})

	t.Run("too slow: POSOFF-0884; the caller gone: no message", func(t *testing.T) {
		was := salesHistoryDeadline
		salesHistoryDeadline = time.Nanosecond
		_, err := SearchSalesHistory(context.Background(), tenantID, SalesHistoryFilter{Stores: []string{store}})
		salesHistoryDeadline = was
		var ve *ValidationError
		if !errors.As(err, &ve) || ve.Code != "POSOFF-0884" {
			t.Errorf("past the deadline: %v", err)
		}
		gone, cancel := context.WithCancel(context.Background())
		cancel()
		_, err = SearchSalesHistory(gone, tenantID, SalesHistoryFilter{Stores: []string{store}})
		if !errors.Is(err, context.Canceled) {
			t.Errorf("caller gone: %v", err)
		}
	})

	t.Run("returns only, an online return found by its order", func(t *testing.T) {
		h := search(SalesHistoryFilter{Stores: []string{store}, Q: "#T215SH1001", Kind: "Return"})
		r, ok := ids(h)[p+"RR2"]
		if !ok || len(h.Rows) != 1 || r.Source != "Online" || r.Against != p+"OB-0009" || r.Total != 1499 || r.Customer != "Ravi Online" {
			t.Errorf("online return: %+v", h.Rows)
		}
	})

	t.Run("dates", func(t *testing.T) {
		var today string
		if err := db.DB.QueryRow(`SELECT public.erp_today()::text`).Scan(&today); err != nil {
			t.Fatal(err)
		}
		if h := search(SalesHistoryFilter{Stores: []string{store}, From: today, To: today}); h.Sales != 2 {
			t.Errorf("today: %d sales", h.Sales)
		}
		if h := search(SalesHistoryFilter{Stores: []string{store}, From: "2001-01-01", To: "2001-01-02"}); len(h.Rows) != 0 {
			t.Errorf("2001: %+v", h.Rows)
		}
		_, err := SearchSalesHistory(context.Background(), tenantID, SalesHistoryFilter{From: "16/11/2026"})
		var ve *ValidationError
		if !errors.As(err, &ve) || ve.Code != "POSOFF-0883" {
			t.Errorf("bad date: %v", err)
		}
	})

	t.Run("paging", func(t *testing.T) {
		h := search(SalesHistoryFilter{Stores: []string{store}, Limit: 2})
		if len(h.Rows) != 2 || !h.More || h.Sales != 2 || h.Returns != 1 {
			t.Errorf("page 1: %d rows more=%v totals %d/%d", len(h.Rows), h.More, h.Sales, h.Returns)
		}
		h = search(SalesHistoryFilter{Stores: []string{store}, Limit: 2, Offset: 2})
		if len(h.Rows) != 1 || h.More {
			t.Errorf("page 2: %d rows more=%v", len(h.Rows), h.More)
		}
		// 297.4: the screen asks for its rows, then for the totals apart.
		h = search(SalesHistoryFilter{Stores: []string{store}, Limit: 2, NoTotals: true})
		if len(h.Rows) != 2 || !h.More || h.Sales != 0 || h.Returns != 0 {
			t.Errorf("rows part: %d rows more=%v totals %d/%d", len(h.Rows), h.More, h.Sales, h.Returns)
		}
		h = search(SalesHistoryFilter{Stores: []string{store}, Limit: 2, Offset: 2, TotalsOnly: true})
		if len(h.Rows) != 0 || h.More || h.Sales != 2 || h.Returns != 1 {
			t.Errorf("totals part: %d rows more=%v totals %d/%d", len(h.Rows), h.More, h.Sales, h.Returns)
		}
	})
}
