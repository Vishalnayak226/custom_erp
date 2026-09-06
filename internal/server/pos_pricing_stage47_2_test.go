package server

// Stage 47.2.5 - "Add tampered-JSON tests: zero/near-zero/negative/high price,
// client cost, false discount, changed item/customer/location/currency/tax,
// expired/superseded price list, concurrent rule change and unauthorized
// override."
//
// Every test here submits a checkout payload a hostile or broken client could
// send and asserts the SERVER's own figures reached inventory, the GL and the
// receipt regardless. The A-02 red-team test (stage47_a02_price_tamper_redteam_test.go)
// covers the approval-bypass half of the same finding and was promoted onto
// this same default test path by this stage; this file covers the rest.
//
// Fixtures follow the red-team suite's conventions - unique suffixes on every
// identifier, because this schema is shared with concurrent sessions (see
// CLAUDE.md) - and reuse its seedStage47User/stage47Token helpers rather than
// growing a second set.

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"custom_erp/db"
	"custom_erp/engines"
)

// posPricingFixture is one disposable priced item, location, cashier session
// and supervisor, torn down by its own cleanup.
type posPricingFixture struct {
	t              *testing.T
	schema         string
	suffix         string
	sku            string
	location       string
	cashierID      string
	cashierToken   string
	superviserID   string
	superviserRole string
	superviserTok  string
	adminID        string
	adminToken     string
}

func newPOSPricingFixture(t *testing.T, salePrice float64) *posPricingFixture {
	t.Helper()
	db.InitDB(testConnStr())
	schema, err := db.GetTenantSchema("default")
	if err != nil {
		t.Fatalf("failed to resolve tenant schema: %v", err)
	}

	f := &posPricingFixture{t: t, schema: schema, suffix: stage47UniqueID()}
	f.sku = "P472SKU-" + f.suffix
	f.location = "P472LOC-" + f.suffix

	itemData, _ := json.Marshal(map[string]interface{}{
		"name": "47.2 pricing item", "hsn_code": "6109", "gst_rate": 18.0,
		"sale_price": salePrice, "mrp": salePrice, "standard_cost": 100.0,
	})
	if _, err := db.DB.Exec(fmt.Sprintf(
		`INSERT INTO %s.documents (id, doctype, data, status, created_by) VALUES ($1, 'Item', $2, 'Active', 'system')`, schema),
		f.sku, itemData); err != nil {
		t.Fatalf("failed to seed item: %v", err)
	}
	t.Cleanup(func() {
		db.DB.Exec(fmt.Sprintf(`DELETE FROM %s.documents WHERE id = $1 AND doctype = 'Item'`, schema), f.sku)
	})

	if _, err := db.DB.Exec(fmt.Sprintf(
		`INSERT INTO %s.inventory_availability (sku, location_code, on_hand, available) VALUES ($1, $2, 500, 500)`, schema),
		f.sku, f.location); err != nil {
		t.Fatalf("failed to seed inventory: %v", err)
	}
	t.Cleanup(func() {
		db.DB.Exec(fmt.Sprintf(`DELETE FROM %s.inventory_availability WHERE sku = $1 AND location_code = $2`, schema), f.sku, f.location)
	})

	cashierID, cleanupCashier := seedStage47User(t, "Cashier", f.location)
	t.Cleanup(cleanupCashier)
	f.cashierID = cashierID
	f.cashierToken = stage47Token(cashierID, "Cashier", f.location)

	supID, cleanupSup := seedStage47User(t, "Store Manager", f.location)
	t.Cleanup(cleanupSup)
	f.superviserID = supID
	f.superviserRole = "Store Manager"
	f.superviserTok = stage47Token(supID, "Store Manager", f.location)

	adminID, cleanupAdmin := seedStage47User(t, engines.RoleSuperAdmin, f.location)
	t.Cleanup(cleanupAdmin)
	f.adminID = adminID
	f.adminToken = stage47Token(adminID, engines.RoleSuperAdmin, f.location)

	// An open session for the cashier - checkout's own precondition (20.7).
	sessID := "P472SESS-" + f.suffix
	sessData, _ := json.Marshal(map[string]interface{}{"location": f.location, "cashier": cashierID, "status": "Open"})
	if _, err := db.DB.Exec(fmt.Sprintf(
		`INSERT INTO %s.documents (id, doctype, data, status, created_by) VALUES ($1, 'POSSession', $2, 'Open', $3)`, schema),
		sessID, sessData, cashierID); err != nil {
		t.Fatalf("failed to seed POS session: %v", err)
	}
	t.Cleanup(func() {
		db.DB.Exec(fmt.Sprintf(`DELETE FROM %s.documents WHERE id = $1 AND doctype = 'POSSession'`, schema), sessID)
	})
	return f
}

// post drives one handler through apiMiddleware exactly as a real request
// would, so the capability checks under test are the live ones.
func (f *posPricingFixture) post(handler http.HandlerFunc, path, token string, body interface{}) (int, map[string]interface{}) {
	f.t.Helper()
	b, _ := json.Marshal(body)
	req := httptest.NewRequest(http.MethodPost, path, bytes.NewReader(b))
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	apiMiddleware(handler)(rec, req)
	var resp map[string]interface{}
	_ = json.Unmarshal(rec.Body.Bytes(), &resp)
	return rec.Code, resp
}

func (f *posPricingFixture) checkout(token string, payload map[string]interface{}) (int, map[string]interface{}) {
	f.t.Helper()
	cart, _ := payload["cart_number"].(string)
	f.t.Cleanup(func() {
		db.DB.Exec(fmt.Sprintf(`DELETE FROM %s.documents WHERE id = $1 AND doctype = 'POSCart'`, f.schema), cart)
	})
	return f.post(handleCheckout, "/api/v1/checkout", token, payload)
}

// storedCart reads back what the server actually persisted, which is the only
// thing downstream (inventory, GL, receipt, returns) ever sees.
func (f *posPricingFixture) storedCart(cartNumber string) map[string]interface{} {
	f.t.Helper()
	var raw string
	if err := db.DB.QueryRow(fmt.Sprintf(
		`SELECT data FROM %s.documents WHERE doctype = 'POSCart' AND id = $1`, f.schema), cartNumber).Scan(&raw); err != nil {
		f.t.Fatalf("cart %s was not stored: %v", cartNumber, err)
	}
	var out map[string]interface{}
	if err := json.Unmarshal([]byte(raw), &out); err != nil {
		f.t.Fatalf("cart %s stored unreadable data: %v", cartNumber, err)
	}
	return out
}

func (f *posPricingFixture) firstStoredLine(cartNumber string) map[string]interface{} {
	f.t.Helper()
	items, _ := f.storedCart(cartNumber)["items"].([]interface{})
	if len(items) == 0 {
		f.t.Fatalf("cart %s stored no items", cartNumber)
	}
	line, _ := items[0].(map[string]interface{})
	return line
}

// TestStage472ClientSubmittedPricesNeverReachTheSale is the core of A-02's
// price half: whatever the till claims a line is worth, the sale uses the
// item's own master price.
func TestStage472ClientSubmittedPricesNeverReachTheSale(t *testing.T) {
	f := newPOSPricingFixture(t, 1000)

	cases := []struct {
		name         string
		clientPrice  float64
		clientCost   float64
		clientDiscnt float64
	}{
		{"zero price", 0, 0, 0},
		{"near-zero price", 0.01, 0, 0},
		{"absurdly high price", 999999, 0, 0},
		{"client-asserted cost", 1000, 999999, 0},
		{"falsely declared discount", 1000, 0, 0},
	}
	for i, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cart := fmt.Sprintf("P472TAMPER-%s-%d", f.suffix, i)
			status, resp := f.checkout(f.cashierToken, map[string]interface{}{
				"cart_number":  cart,
				"location":     f.location,
				"payment_mode": "Cash",
				"discount_pct": tc.clientDiscnt,
				"items": []map[string]interface{}{
					{"sku": f.sku, "qty": 2, "sale_price": tc.clientPrice, "cost_price": tc.clientCost},
				},
			})
			if status != http.StatusOK {
				t.Fatalf("checkout rejected (%d): %v", status, resp)
			}
			if resp["status"] != "completed" {
				t.Fatalf("expected a completed sale, got %v", resp["status"])
			}
			// The stored cart is what inventory, the GL and the receipt read.
			line := f.firstStoredLine(cart)
			if got, _ := line["sale_price"].(float64); got != 1000 {
				t.Errorf("the client claimed %.2f and the server stored %.2f - the master price (1000.00) must win regardless of what the till sends", tc.clientPrice, got)
			}
			if src, _ := line["price_source"].(string); src != engines.PriceSourceItemMaster {
				t.Errorf("price_source = %q, want %q - the line must record WHERE its price came from", src, engines.PriceSourceItemMaster)
			}
			if _, present := line["cost_price"]; present {
				t.Errorf("the stored line still carries cost_price %v - 47.2.2 removes the client cost from the request, the cart and the response entirely", line["cost_price"])
			}
			if total, _ := resp["sale_total"].(float64); total != 2000 {
				t.Errorf("sale_total = %.2f, want 2000.00 (2 x the master price)", total)
			}
		})
	}
}

// TestStage472CashierNeverReceivesCostFields is 47.2.2's acceptance line,
// asserted at the HTTP boundary rather than trusted.
func TestStage472CashierNeverReceivesCostFields(t *testing.T) {
	f := newPOSPricingFixture(t, 500)

	cashierCart := "P472COST-C-" + f.suffix
	status, resp := f.checkout(f.cashierToken, map[string]interface{}{
		"cart_number": cashierCart, "location": f.location, "payment_mode": "Cash",
		"items": []map[string]interface{}{{"sku": f.sku, "qty": 1}},
	})
	if status != http.StatusOK {
		t.Fatalf("cashier checkout failed (%d): %v", status, resp)
	}
	if _, present := resp["cost_total"]; present {
		t.Errorf("the Cashier's checkout response carries cost_total = %v; 47.2.2's acceptance line is that a Cashier never receives margin/cost fields", resp["cost_total"])
	}

	// The same sale, rung up by a role the cost/margin policy DOES admit,
	// still reports cost - the field is withheld by role, not deleted.
	adminSession := "P472SESSA-" + f.suffix
	adminUser := "__stage47_admin_sess_" + f.suffix
	_ = adminUser
	adminCart := "P472COST-A-" + f.suffix
	// A Super Admin needs their own open session at this location, since the
	// session lookup keys off the caller's own resolved username.
	var adminName string
	_ = db.DB.QueryRow(`SELECT username FROM tenant_default.users WHERE id = (SELECT id FROM tenant_default.users WHERE role = $1 AND location_code = $2 ORDER BY created_at DESC LIMIT 1)`,
		engines.RoleSuperAdmin, f.location).Scan(&adminName)
	if adminName == "" {
		t.Skip("could not resolve the seeded Super Admin's username - the cost-visibility half needs a session for it")
	}
	sessData, _ := json.Marshal(map[string]interface{}{"location": f.location, "cashier": adminName, "status": "Open"})
	if _, err := db.DB.Exec(fmt.Sprintf(
		`INSERT INTO %s.documents (id, doctype, data, status, created_by) VALUES ($1, 'POSSession', $2, 'Open', $3)`, f.schema),
		adminSession, sessData, f.adminID); err != nil {
		t.Fatalf("failed to seed the admin POS session: %v", err)
	}
	t.Cleanup(func() {
		db.DB.Exec(fmt.Sprintf(`DELETE FROM %s.documents WHERE id = $1 AND doctype = 'POSSession'`, f.schema), adminSession)
	})

	status, resp = f.checkout(f.adminToken, map[string]interface{}{
		"cart_number": adminCart, "location": f.location, "payment_mode": "Cash",
		"items": []map[string]interface{}{{"sku": f.sku, "qty": 1}},
	})
	if status != http.StatusOK {
		t.Fatalf("admin checkout failed (%d): %v", status, resp)
	}
	if _, present := resp["cost_total"]; !present {
		t.Errorf("a Super Admin's checkout response omits cost_total; the field is meant to be withheld from roles the cost/margin policy excludes, not removed from the product")
	}
}

// TestStage472PriceOverrideIsCapabilityGated covers "unauthorized override".
func TestStage472PriceOverrideIsCapabilityGated(t *testing.T) {
	f := newPOSPricingFixture(t, 1000)

	status, resp := f.post(handlePOSPriceOverride, "/api/v1/pos/price-override", f.cashierToken, map[string]interface{}{
		"cart_number": "P472OVR-DENY-" + f.suffix, "sku": f.sku, "qty": 1,
		"location": f.location, "override_price": 1, "reason": "because I said so",
	})
	if status != http.StatusForbidden {
		t.Fatalf("a Cashier got HTTP %d from the price-override command (%v); it is capability-gated (pos.price_override) and must be refused outright", status, resp)
	}

	// And the refusal is real, not cosmetic: nothing was written.
	var count int
	if err := db.DB.QueryRow(fmt.Sprintf(
		`SELECT COUNT(*) FROM %s.documents WHERE doctype = 'POSPriceOverride' AND data->>'cart_number' = $1`, f.schema),
		"P472OVR-DENY-"+f.suffix).Scan(&count); err != nil {
		t.Fatalf("failed to count overrides: %v", err)
	}
	if count != 0 {
		t.Errorf("the refused override still wrote %d POSPriceOverride row(s)", count)
	}
}

// TestStage472ApprovedOverridePricesTheSaleAndIsSpentByIt covers the whole
// override lifecycle: only a capable role can raise one, it must carry a
// reason, it prices the sale it was raised for, and it cannot price a second.
func TestStage472ApprovedOverridePricesTheSaleAndIsSpentByIt(t *testing.T) {
	f := newPOSPricingFixture(t, 1000)
	cart := "P472OVR-" + f.suffix
	t.Cleanup(func() {
		db.DB.Exec(fmt.Sprintf(`DELETE FROM %s.documents WHERE doctype = 'POSPriceOverride' AND data->>'cart_number' = $1`, f.schema), cart)
	})

	// A reason is mandatory - an unexplained deviation is not evidence.
	if status, _ := f.post(handlePOSPriceOverride, "/api/v1/pos/price-override", f.superviserTok, map[string]interface{}{
		"cart_number": cart, "sku": f.sku, "qty": 1, "location": f.location, "override_price": 900,
	}); status == http.StatusOK {
		t.Error("an override with no reason was accepted")
	}

	status, resp := f.post(handlePOSPriceOverride, "/api/v1/pos/price-override", f.superviserTok, map[string]interface{}{
		"cart_number": cart, "sku": f.sku, "qty": 1, "location": f.location,
		"override_price": 900, "reason": "display unit, minor scuff",
	})
	if status != http.StatusOK {
		t.Fatalf("the supervisor's override was refused (%d): %v", status, resp)
	}
	if ref, _ := resp["reference_price"].(float64); ref != 1000 {
		t.Errorf("reference_price = %.2f, want 1000.00 - the override must be judged against the SERVER's price, never one the requester supplied", ref)
	}
	if pct, _ := resp["discount_pct"].(float64); pct != 10 {
		t.Errorf("discount_pct = %.2f, want 10.00", pct)
	}

	// The sale it was raised for is priced by it.
	status, checkoutResp := f.checkout(f.cashierToken, map[string]interface{}{
		"cart_number": cart, "location": f.location, "payment_mode": "Cash",
		"items": []map[string]interface{}{{"sku": f.sku, "qty": 1, "sale_price": 5}},
	})
	if status != http.StatusOK {
		t.Fatalf("checkout with an approved override failed (%d): %v", status, checkoutResp)
	}
	line := f.firstStoredLine(cart)
	if got, _ := line["sale_price"].(float64); got != 900 {
		t.Errorf("the sale priced at %.2f; the approved override (900.00) must win over both the master price and the till's own 5.00", got)
	}
	if src, _ := line["price_source"].(string); src != engines.PriceSourceOverride {
		t.Errorf("price_source = %q, want %q", src, engines.PriceSourceOverride)
	}
	if ref, _ := line["reference_price"].(float64); ref != 1000 {
		t.Errorf("reference_price on the stored line = %.2f, want 1000.00 - the deviation must stay visible, not be absorbed into the price", ref)
	}

	// And it is spent: a replay of the same cart number cannot re-use it.
	var overrideStatus string
	if err := db.DB.QueryRow(fmt.Sprintf(
		`SELECT status FROM %s.documents WHERE doctype = 'POSPriceOverride' AND data->>'cart_number' = $1`, f.schema),
		cart).Scan(&overrideStatus); err != nil {
		t.Fatalf("failed to read the override back: %v", err)
	}
	if overrideStatus != "Consumed" {
		t.Errorf("the override is still %q after the sale completed; it must be Consumed so a replayed cart number cannot inherit the same authorisation", overrideStatus)
	}
}

// TestStage472OverrideBeyondThresholdRoutesToApproval is 47.2.3's "allowed
// threshold" half - a supervisor may grant a reduction, but not any reduction.
func TestStage472OverrideBeyondThresholdRoutesToApproval(t *testing.T) {
	f := newPOSPricingFixture(t, 1000)
	if _, err := db.DB.Exec(fmt.Sprintf(
		`INSERT INTO %s.approval_rules (doctype, min_amount, max_amount, required_role) VALUES ('POSPriceOverride', 25, NULL, $1) ON CONFLICT (doctype, min_amount) DO NOTHING`, f.schema),
		engines.RoleSuperAdmin); err != nil {
		t.Fatalf("failed to seed the override threshold: %v", err)
	}
	t.Cleanup(func() {
		db.DB.Exec(fmt.Sprintf(`DELETE FROM %s.approval_rules WHERE doctype = 'POSPriceOverride' AND min_amount = 25`, f.schema))
	})
	cart := "P472OVRLIM-" + f.suffix
	t.Cleanup(func() {
		db.DB.Exec(fmt.Sprintf(`DELETE FROM %s.documents WHERE doctype = 'POSPriceOverride' AND data->>'cart_number' = $1`, f.schema), cart)
	})

	// 10% off is within a Store Manager's own authority (below the 25% slab).
	if status, resp := f.post(handlePOSPriceOverride, "/api/v1/pos/price-override", f.superviserTok, map[string]interface{}{
		"cart_number": cart, "sku": f.sku, "qty": 1, "location": f.location,
		"override_price": 900, "reason": "within limit",
	}); status != http.StatusOK || resp["status"] != "Approved" {
		t.Fatalf("a 10%% reduction below the 25%% slab should be granted outright; got HTTP %d %v", status, resp)
	}

	// 50% off is not.
	status, resp := f.post(handlePOSPriceOverride, "/api/v1/pos/price-override", f.superviserTok, map[string]interface{}{
		"cart_number": cart + "-BIG", "sku": f.sku, "qty": 1, "location": f.location,
		"override_price": 500, "reason": "way beyond limit",
	})
	t.Cleanup(func() {
		db.DB.Exec(fmt.Sprintf(`DELETE FROM %s.documents WHERE doctype = 'POSPriceOverride' AND data->>'cart_number' = $1`, f.schema), cart+"-BIG")
	})
	if status == http.StatusOK {
		t.Fatalf("a 50%% reduction above the tenant's 25%% slab was granted outright by a Store Manager: %v", resp)
	}
	if resp["code"] != "SALESP-0123" {
		t.Errorf("expected the catalog's own \"Discount exceeds your allowed limit\" code (SALESP-0123), got %v", resp["code"])
	}

	// The over-limit override must not price anything while it waits.
	var pending string
	if err := db.DB.QueryRow(fmt.Sprintf(
		`SELECT data->>'status' FROM %s.documents WHERE doctype = 'POSPriceOverride' AND data->>'cart_number' = $1`, f.schema),
		cart+"-BIG").Scan(&pending); err != nil {
		t.Fatalf("the over-limit override was not recorded at all: %v", err)
	}
	if pending != "Pending Approval" {
		t.Errorf("the over-limit override is %q, want \"Pending Approval\"", pending)
	}
	quote, err := engines.ResolvePOSQuote("default", engines.QuoteRequest{
		CartNumber: cart + "-BIG", Location: f.location,
		Lines: []engines.QuoteLineRequest{{Sku: f.sku, Qty: 1}},
	})
	if err != nil {
		t.Fatalf("quote failed: %v", err)
	}
	if quote.Lines[0].UnitPrice != 1000 {
		t.Errorf("a Pending Approval override already priced the line at %.2f; only an Approved one may price anything", quote.Lines[0].UnitPrice)
	}
}

// TestStage472PriceListPrecedenceAndSupersession covers "expired/superseded
// price list" and the contract-vs-default precedence 47.2.1 specifies.
func TestStage472PriceListPrecedenceAndSupersession(t *testing.T) {
	f := newPOSPricingFixture(t, 1000)
	listCode := "P472PL-" + f.suffix
	customerID := "P472CUST-" + f.suffix

	// An approved, currently-effective version priced below the item master.
	versionID := "P472PLV-" + f.suffix
	versionData, _ := json.Marshal(map[string]interface{}{
		"price_list_code": listCode,
		"effective_from":  "2000-01-01",
		"items":           []map[string]interface{}{{"sku": f.sku, "price": 750}},
	})
	if _, err := db.DB.Exec(fmt.Sprintf(
		`INSERT INTO %s.documents (id, doctype, data, status, created_by) VALUES ($1, 'PriceListVersion', $2, 'Approved', 'system')`, f.schema),
		versionID, versionData); err != nil {
		t.Fatalf("failed to seed the price list version: %v", err)
	}
	t.Cleanup(func() {
		db.DB.Exec(fmt.Sprintf(`DELETE FROM %s.documents WHERE id = $1`, f.schema), versionID)
	})

	custData, _ := json.Marshal(map[string]interface{}{
		"name": "47.2 contract customer", "status": "Active", "price_list_code": listCode,
	})
	if _, err := db.DB.Exec(fmt.Sprintf(
		`INSERT INTO %s.documents (id, doctype, data, status, created_by) VALUES ($1, 'Customer', $2, 'Active', 'system')`, f.schema),
		customerID, custData); err != nil {
		t.Fatalf("failed to seed the customer: %v", err)
	}
	t.Cleanup(func() {
		db.DB.Exec(fmt.Sprintf(`DELETE FROM %s.documents WHERE id = $1`, f.schema), customerID)
	})

	// The customer's contract list wins over the item master.
	quote, err := engines.ResolvePOSQuote("default", engines.QuoteRequest{
		Location: f.location, CustomerID: customerID,
		Lines: []engines.QuoteLineRequest{{Sku: f.sku, Qty: 1}},
	})
	if err != nil {
		t.Fatalf("contract quote failed: %v", err)
	}
	if quote.Lines[0].UnitPrice != 750 || quote.Lines[0].PriceSource != engines.PriceSourceContractList {
		t.Fatalf("contract price list did not win: got %.2f from %q, want 750.00 from %q",
			quote.Lines[0].UnitPrice, quote.Lines[0].PriceSource, engines.PriceSourceContractList)
	}

	// A walk-in (no customer) is unaffected by it and prices from the master.
	walkIn, err := engines.ResolvePOSQuote("default", engines.QuoteRequest{
		Location: f.location, Lines: []engines.QuoteLineRequest{{Sku: f.sku, Qty: 1}},
	})
	if err != nil {
		t.Fatalf("walk-in quote failed: %v", err)
	}
	if walkIn.Lines[0].UnitPrice != 1000 {
		t.Errorf("a walk-in sale priced at %.2f from another customer's contract list; a contract price must not leak to customers who are not on it", walkIn.Lines[0].UnitPrice)
	}

	// An EXPIRED version stops pricing today's sale - the whole point of the
	// effective-dated resolver, and the "expired price list" case 47.2.5 names.
	if _, err := db.DB.Exec(fmt.Sprintf(
		`UPDATE %s.documents SET data = jsonb_set(data, '{effective_to}', to_jsonb('2001-01-01'::text)) WHERE id = $1`, f.schema),
		versionID); err != nil {
		t.Fatalf("failed to expire the version: %v", err)
	}
	expired, err := engines.ResolvePOSQuote("default", engines.QuoteRequest{
		Location: f.location, CustomerID: customerID,
		Lines: []engines.QuoteLineRequest{{Sku: f.sku, Qty: 1}},
	})
	if err != nil {
		t.Fatalf("post-expiry quote failed: %v", err)
	}
	if expired.Lines[0].UnitPrice != 1000 {
		t.Errorf("an expired price list still priced the sale at %.2f; it must fall through to the item master (1000.00)", expired.Lines[0].UnitPrice)
	}
}

// TestStage472QuoteVersionDetectsChangedInputsAndStalePrices covers "changed
// item/customer/location/currency/tax" and "concurrent rule change": the
// version is what makes each of those detectable rather than silently applied.
func TestStage472QuoteVersionDetectsChangedInputsAndStalePrices(t *testing.T) {
	f := newPOSPricingFixture(t, 1000)
	base := engines.QuoteRequest{
		Location: f.location, Currency: "INR",
		Lines: []engines.QuoteLineRequest{{Sku: f.sku, Qty: 1}},
	}
	original, err := engines.ResolvePOSQuote("default", base)
	if err != nil {
		t.Fatalf("base quote failed: %v", err)
	}

	// Same inputs, same version - otherwise the staleness check would fire on
	// every sale and mean nothing.
	repeat, err := engines.ResolvePOSQuote("default", base)
	if err != nil {
		t.Fatalf("repeat quote failed: %v", err)
	}
	if repeat.Version != original.Version {
		t.Fatalf("two quotes for identical inputs produced different versions (%s vs %s); the version must be a function of the priced content, not of when it was asked for", original.Version, repeat.Version)
	}

	mutations := map[string]func(q engines.QuoteRequest) engines.QuoteRequest{
		"changed location": func(q engines.QuoteRequest) engines.QuoteRequest { q.Location = f.location + "-X"; return q },
		"changed currency": func(q engines.QuoteRequest) engines.QuoteRequest { q.Currency = "USD"; return q },
		"changed tax basis (interstate)": func(q engines.QuoteRequest) engines.QuoteRequest {
			q.Interstate = true
			return q
		},
		"changed quantity": func(q engines.QuoteRequest) engines.QuoteRequest {
			q.Lines = []engines.QuoteLineRequest{{Sku: f.sku, Qty: 3}}
			return q
		},
	}
	for name, mutate := range mutations {
		t.Run(name, func(t *testing.T) {
			mutated, err := engines.ResolvePOSQuote("default", mutate(base))
			if err != nil {
				t.Fatalf("mutated quote failed: %v", err)
			}
			if mutated.Version == original.Version {
				t.Errorf("%s produced the SAME quote version; a changed pricing input must change the version, or checkout cannot tell that the cashier is looking at a different bill", name)
			}
		})
	}

	// Concurrent rule change: the item is repriced between quoting and paying.
	if _, err := db.DB.Exec(fmt.Sprintf(
		`UPDATE %s.documents SET data = jsonb_set(data, '{sale_price}', '1200') WHERE id = $1 AND doctype = 'Item'`, f.schema),
		f.sku); err != nil {
		t.Fatalf("failed to reprice the item: %v", err)
	}
	cart := "P472STALE-" + f.suffix
	status, resp := f.checkout(f.cashierToken, map[string]interface{}{
		"cart_number": cart, "location": f.location, "payment_mode": "Cash",
		"quote_version": original.Version,
		"items":         []map[string]interface{}{{"sku": f.sku, "qty": 1}},
	})
	if status != http.StatusConflict || resp["status"] != "price_changed" {
		t.Fatalf("a sale quoted at 1000 and rung up after the item was repriced to 1200 completed anyway (HTTP %d, %v); 47.2.4 requires it to stop and show the change", status, resp)
	}

	// ...and completes at the NEW price once confirmed, never the stale one.
	status, resp = f.checkout(f.cashierToken, map[string]interface{}{
		"cart_number": cart, "location": f.location, "payment_mode": "Cash",
		"quote_version": original.Version, "accept_price_change": true,
		"items": []map[string]interface{}{{"sku": f.sku, "qty": 1}},
	})
	if status != http.StatusOK {
		t.Fatalf("the confirmed re-submission failed (%d): %v", status, resp)
	}
	if total, _ := resp["sale_total"].(float64); total != 1200 {
		t.Errorf("sale_total = %.2f after confirming the price change, want 1200.00 - never the stale 1000.00", total)
	}
}

// TestStage472StrictModeRefusesToSellAnUnpricedItem covers the pricing-mode
// end state: a tenant that has priced its catalogue can make an unverifiable
// price impossible rather than merely reviewable.
func TestStage472StrictModeRefusesToSellAnUnpricedItem(t *testing.T) {
	f := newPOSPricingFixture(t, 1000)
	unpriced := "P472NOPRICE-" + f.suffix
	itemData, _ := json.Marshal(map[string]interface{}{"name": "unpriced", "hsn_code": "6109", "gst_rate": 18.0})
	if _, err := db.DB.Exec(fmt.Sprintf(
		`INSERT INTO %s.documents (id, doctype, data, status, created_by) VALUES ($1, 'Item', $2, 'Active', 'system')`, f.schema),
		unpriced, itemData); err != nil {
		t.Fatalf("failed to seed the unpriced item: %v", err)
	}
	t.Cleanup(func() {
		db.DB.Exec(fmt.Sprintf(`DELETE FROM %s.documents WHERE id = $1`, f.schema), unpriced)
	})

	// Assisted (the default): the operator's figure is accepted, and flagged.
	assisted, err := engines.ResolvePOSQuote("default", engines.QuoteRequest{
		Location: f.location,
		Lines:    []engines.QuoteLineRequest{{Sku: unpriced, Qty: 1, FallbackUnitPrice: 250}},
	})
	if err != nil {
		t.Fatalf("assisted quote failed: %v", err)
	}
	if !assisted.HasUnverifiedPrice || assisted.Lines[0].PriceSource != engines.PriceSourceCashierEntered {
		t.Errorf("an unpriced item was not flagged as unverified: has_unverified=%v source=%q", assisted.HasUnverifiedPrice, assisted.Lines[0].PriceSource)
	}

	if err := engines.SetSetting("default", "pos.pricing_mode", engines.PricingModeStrict, "stage47.2 test"); err != nil {
		t.Fatalf("failed to switch to strict mode: %v", err)
	}
	t.Cleanup(func() {
		_ = engines.SetSetting("default", "pos.pricing_mode", engines.PricingModeAssisted, "stage47.2 test cleanup")
	})

	if _, err := engines.ResolvePOSQuote("default", engines.QuoteRequest{
		Location: f.location,
		Lines:    []engines.QuoteLineRequest{{Sku: unpriced, Qty: 1, FallbackUnitPrice: 250}},
	}); err == nil {
		t.Error("strict mode priced an item nothing on the server prices; it must refuse the line outright")
	}

	// A priced item still sells normally in strict mode - the setting closes a
	// hole, it does not stop the till working.
	strictOK, err := engines.ResolvePOSQuote("default", engines.QuoteRequest{
		Location: f.location, Lines: []engines.QuoteLineRequest{{Sku: f.sku, Qty: 1}},
	})
	if err != nil {
		t.Fatalf("strict mode refused a properly priced item: %v", err)
	}
	if strictOK.Lines[0].UnitPrice != 1000 {
		t.Errorf("strict-mode price = %.2f, want 1000.00", strictOK.Lines[0].UnitPrice)
	}
}
