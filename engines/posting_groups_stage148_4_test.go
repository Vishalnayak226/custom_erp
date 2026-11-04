package engines

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"custom_erp/db"
)

// Stage 148.4: revenue, cost of goods and goods-receipt inventory post through
// ResolvePostingAccounts by item / customer / vendor group.

// stage1484Posted is what a document posted, as "D:<account>" / "C:<account>"
// to paise, read from the GL.
func stage1484Posted(t *testing.T, schema, docType, docID string) map[string]int64 {
	t.Helper()
	rows, err := db.DB.Query("SELECT account_code, COALESCE(SUM(debit),0), COALESCE(SUM(credit),0) FROM "+schema+".gl_postings WHERE document_type = $1 AND document_id = $2 GROUP BY account_code", docType, docID)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	out := map[string]int64{}
	for rows.Next() {
		var code string
		var d, c int64
		if err := rows.Scan(&code, &d, &c); err != nil {
			t.Fatal(err)
		}
		if d > 0 {
			out["D:"+code] = d
		}
		if c > 0 {
			out["C:"+code] = c
		}
	}
	return out
}

type stage1484Fixture struct {
	tenantID, schema string
	insert           func(id, doctype, status, createdBy string, data map[string]interface{})
}

func stage1484Setup(t *testing.T) stage1484Fixture {
	t.Helper()
	tenantID, schema, insert := stage140Tenant(t)
	const loc = "S1484"
	insert(loc, "Location", "Active", "system", map[string]interface{}{"code": loc, "name": "Store 1484", "type": "Store", "sellable": "Yes"})
	insert("TEE1484", "Item", "Active", "system", map[string]interface{}{"code": "TEE1484", "name": "Tee", "category": "Apparel", "sale_price": 100.0, "standard_cost": 40.0})
	insert("SHOE1484", "Item", "Active", "system", map[string]interface{}{"code": "SHOE1484", "name": "Shoe", "category": "Footwear", "sale_price": 100.0, "standard_cost": 60.0})
	if _, err := PostInventoryLedger(tenantID, loc, []interface{}{
		map[string]interface{}{"sku": "TEE1484", "qty": 20.0}, map[string]interface{}{"sku": "SHOE1484", "qty": 20.0}}, false); err != nil {
		t.Fatal(err)
	}
	for _, a := range [][2]string{{"4150", "Footwear Sales"}, {"4160", "Wholesale Sales"}, {"5110", "Footwear COGS"}, {"1210", "Imported Stock"}} {
		if _, err := db.DB.Exec("INSERT INTO "+schema+".gl_accounts (account_code, account_name, account_type) VALUES ($1, $2, 'Asset') ON CONFLICT DO NOTHING", a[0], a[1]); err != nil {
			t.Fatal(err)
		}
	}
	return stage1484Fixture{tenantID, schema, insert}
}

func (f stage1484Fixture) profile(t *testing.T, id string, data map[string]interface{}) {
	t.Helper()
	data["code"], data["status"] = id, "Approved"
	f.insert(id, "PostingProfile", "Approved", "system", data)
}

func (f stage1484Fixture) invoice(t *testing.T, id, customer string, lines []map[string]interface{}, total float64) {
	t.Helper()
	items, _ := json.Marshal(lines)
	f.insert(id, "SalesInvoice", "Draft", "manager1", map[string]interface{}{"customer": customer, "location": "S1484", "items": string(items), "total_amount": total})
}

func stage1484Line(sku string, qty int, amount float64) map[string]interface{} {
	return map[string]interface{}{"sku": sku, "qty": qty, "rate": amount / float64(qty), "amount": amount}
}

func (f stage1484Fixture) cart(t *testing.T, id string, customer string) {
	t.Helper()
	data, _ := json.Marshal(map[string]interface{}{
		"location": "S1484", "payment_mode": "Cash", "customer_id": customer,
		"items": []map[string]interface{}{{"sku": "TEE1484", "qty": 1, "sale_price": 100.0}, {"sku": "SHOE1484", "qty": 1, "sale_price": 100.0}},
	})
	if _, err := db.DB.Exec("INSERT INTO "+f.schema+".documents (id, doctype, data, status, created_by) VALUES ($1, 'POSCart', $2, 'Processing', 'cashier1484')", id, data); err != nil {
		t.Fatal(err)
	}
}

func (f stage1484Fixture) grn(t *testing.T, id, vendor string) map[string]int64 {
	t.Helper()
	recv, _ := json.Marshal([]map[string]interface{}{
		{"sku": "TEE1484", "qty": 5, "accepted_qty": 5, "invoice_rate": 30.0},
		{"sku": "SHOE1484", "qty": 5, "accepted_qty": 5, "invoice_rate": 50.0},
	})
	f.insert(id, "GRN", "Received", "system", map[string]interface{}{"code": id, "received_items": string(recv), "vendor": vendor})
	tx, err := db.DB.Begin()
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	if err := recordGRNReceiptCostingPartTx(tx, f.tenantID, f.schema, id, "", []struct {
		SKU string
		Qty float64
	}{{"TEE1484", 5}, {"SHOE1484", 5}}); err != nil {
		t.Fatalf("GRN costing: %v", err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	return stage1484Posted(t, f.schema, "GRN", id)
}

// With no PostingProfile for the new events every posting is exactly the
// account it always was - one check per event.
func TestStage1484NoRulePostingsUnchanged(t *testing.T) {
	f := stage1484Setup(t)

	f.invoice(t, "SI-1484-A", "C1484", []map[string]interface{}{stage1484Line("TEE1484", 1, 100), stage1484Line("SHOE1484", 1, 100)}, 200)
	if _, err := PostSalesInvoice(f.tenantID, "SI-1484-A", "manager1"); err != nil {
		t.Fatalf("post invoice: %v", err)
	}
	want := map[string]int64{"D:1300": 20000, "C:4100": 20000, "D:5100": 10000, "C:1200": 10000}
	if got := stage1484Posted(t, f.schema, "SalesInvoice", "SI-1484-A"); !reflect.DeepEqual(got, want) {
		t.Fatalf("sales invoice revenue and COGS: got %v, want %v", got, want)
	}

	f.cart(t, "CART-1484-A", "")
	if _, _, err := FinalizePOSCheckout(f.tenantID, "CART-1484-A", ""); err != nil {
		t.Fatalf("POS checkout: %v", err)
	}
	wantPOS := map[string]int64{"D:1100": 20000, "C:4100": 20000, "D:5100": 10000, "C:1200": 10000}
	if got := stage1484Posted(t, f.schema, "POSCart", "CART-1484-A"); !reflect.DeepEqual(got, wantPOS) {
		t.Fatalf("POS revenue and COGS: got %v, want %v", got, wantPOS)
	}

	wantGRN := map[string]int64{"D:1200": 40000, "C:2100": 40000}
	if got := f.grn(t, "GRN-1484-A", "V1484"); !reflect.DeepEqual(got, wantGRN) {
		t.Fatalf("GRN receipt: got %v, want %v", got, wantGRN)
	}
}

// A rule for one item group moves that group's revenue and nothing else; a
// customer-group rule moves a whole invoice; the other events follow suit.
func TestStage1484RuleMovesOneItemGroupRevenue(t *testing.T) {
	f := stage1484Setup(t)
	f.profile(t, "PP-1484-REV", map[string]interface{}{"event": "sales_invoice_revenue", "item_group": "Footwear", "credit_account": "4150"})
	f.profile(t, "PP-1484-COGS", map[string]interface{}{"event": "sales_invoice_cogs", "item_group": "Footwear", "debit_account": "5110"})
	f.profile(t, "PP-1484-WS", map[string]interface{}{"event": "sales_invoice_revenue", "customer_group": "Wholesale", "credit_account": "4160"})
	f.insert("C1484W", "Customer", "Active", "system", map[string]interface{}{"code": "C1484W", "name": "Wholesale Co", "customer_group": "Wholesale"})

	// Retail customer: Footwear revenue and cost move, Apparel stays on 4100/5100.
	f.invoice(t, "SI-1484-B", "C1484", []map[string]interface{}{stage1484Line("TEE1484", 1, 100), stage1484Line("SHOE1484", 1, 100)}, 200)
	if _, err := PostSalesInvoice(f.tenantID, "SI-1484-B", "manager1"); err != nil {
		t.Fatalf("post invoice: %v", err)
	}
	want := map[string]int64{"D:1300": 20000, "C:4100": 10000, "C:4150": 10000, "D:5100": 4000, "D:5110": 6000, "C:1200": 10000}
	if got := stage1484Posted(t, f.schema, "SalesInvoice", "SI-1484-B"); !reflect.DeepEqual(got, want) {
		t.Fatalf("item-group rule: got %v, want %v", got, want)
	}

	// Wholesale customer: the customer group (32) outweighs nothing here, but
	// the item-group rule (64) is the more specific for Footwear lines; the
	// Apparel line takes the customer-group rule.
	f.invoice(t, "SI-1484-C", "C1484W", []map[string]interface{}{stage1484Line("TEE1484", 1, 100), stage1484Line("SHOE1484", 1, 100)}, 200)
	if _, err := PostSalesInvoice(f.tenantID, "SI-1484-C", "manager1"); err != nil {
		t.Fatalf("post wholesale invoice: %v", err)
	}
	wantW := map[string]int64{"D:1300": 20000, "C:4160": 10000, "C:4150": 10000, "D:5100": 4000, "D:5110": 6000, "C:1200": 10000}
	if got := stage1484Posted(t, f.schema, "SalesInvoice", "SI-1484-C"); !reflect.DeepEqual(got, wantW) {
		t.Fatalf("customer-group rule: got %v, want %v", got, wantW)
	}

	// POS and GRN events.
	f.profile(t, "PP-1484-POS", map[string]interface{}{"event": "pos_sale_revenue", "item_group": "Footwear", "credit_account": "4150"})
	f.profile(t, "PP-1484-POSC", map[string]interface{}{"event": "pos_sale_cogs", "item_group": "Footwear", "debit_account": "5110"})
	f.cart(t, "CART-1484-B", "")
	if _, _, err := FinalizePOSCheckout(f.tenantID, "CART-1484-B", ""); err != nil {
		t.Fatalf("POS checkout: %v", err)
	}
	wantPOS := map[string]int64{"D:1100": 20000, "C:4100": 10000, "C:4150": 10000, "D:5100": 4000, "D:5110": 6000, "C:1200": 10000}
	if got := stage1484Posted(t, f.schema, "POSCart", "CART-1484-B"); !reflect.DeepEqual(got, wantPOS) {
		t.Fatalf("POS item-group rules: got %v, want %v", got, wantPOS)
	}

	f.profile(t, "PP-1484-GRN", map[string]interface{}{"event": "grn_receipt", "vendor_group": "Imports", "debit_account": "1210"})
	f.insert("V1484I", "Vendor", "Active", "system", map[string]interface{}{"code": "V1484I", "name": "Importer", "vendor_group": "Imports"})
	if got, wantG := f.grn(t, "GRN-1484-B", "V1484I"), (map[string]int64{"D:1210": 40000, "C:2100": 40000}); !reflect.DeepEqual(got, wantG) {
		t.Fatalf("vendor-group rule: got %v, want %v", got, wantG)
	}
	// Another vendor keeps 1200.
	if got, wantG := f.grn(t, "GRN-1484-C", "V1484"), (map[string]int64{"D:1200": 40000, "C:2100": 40000}); !reflect.DeepEqual(got, wantG) {
		t.Fatalf("other vendor: got %v, want %v", got, wantG)
	}
}

// A rule that lands on a blocked or missing account stops the posting: the
// invoice stays Draft, no GL row is written and the message says why.
func TestStage1484RuleToBlockedOrMissingAccountFails(t *testing.T) {
	f := stage1484Setup(t)
	f.profile(t, "PP-1484-BLK", map[string]interface{}{"event": "sales_invoice_revenue", "item_group": "Footwear", "credit_account": "4150"})
	if _, err := db.DB.Exec("UPDATE " + f.schema + ".gl_accounts SET blocked = TRUE WHERE account_code = '4150'"); err != nil {
		t.Fatal(err)
	}
	f.invoice(t, "SI-1484-D", "C1484", []map[string]interface{}{stage1484Line("SHOE1484", 1, 100)}, 100)
	_, err := PostSalesInvoice(f.tenantID, "SI-1484-D", "manager1")
	if err == nil || !strings.Contains(err.Error(), "blocked") || !strings.Contains(err.Error(), "4150") {
		t.Fatalf("a rule to a blocked account must fail clearly, got %v", err)
	}
	var status string
	_ = db.DB.QueryRow("SELECT status FROM " + f.schema + ".documents WHERE id = 'SI-1484-D'").Scan(&status)
	if status != "Draft" || len(stage1484Posted(t, f.schema, "SalesInvoice", "SI-1484-D")) != 0 {
		t.Fatalf("a failed posting must leave the invoice Draft with no GL rows: status %q, posted %v", status, stage1484Posted(t, f.schema, "SalesInvoice", "SI-1484-D"))
	}

	// A profile naming an account that is not in the chart (written around the save check).
	f.profile(t, "PP-1484-MISS", map[string]interface{}{"event": "sales_invoice_revenue", "item_group": "Apparel", "credit_account": "4999"})
	f.invoice(t, "SI-1484-E", "C1484", []map[string]interface{}{stage1484Line("TEE1484", 1, 100)}, 100)
	if _, err := PostSalesInvoice(f.tenantID, "SI-1484-E", "manager1"); err == nil || !strings.Contains(err.Error(), "not in the chart") {
		t.Fatalf("a rule to a missing account must fail clearly, got %v", err)
	}
}
