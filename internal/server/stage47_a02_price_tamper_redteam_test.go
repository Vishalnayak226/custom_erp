package server

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"custom_erp/db"
)

// Stage 47.0.1 / audit finding A-02 ("POS trusts the browser for sale price
// and cost price" - docs/audits/ERP_DEEP_PERSONA_AUDIT_2026-09-01.md lines
// 77-83). The audit's own reproducible claim: "Approval checks rely on
// submitted discount percentage, so a cashier can send a zero or abnormally
// low price with discount_pct = 0 and avoid discount approval."
//
// Exact mechanism (handlers_pim_pos_finance.go:490-497): handleCheckout only
// routes a sale through the discount-approval maker-checker flow when
// req.DiscountPct itself is above a configured approval_rules threshold for
// "POSCart" (engines.RequiredApproverRoleForAmount). discount_pct is a bare
// client-reported number with no relationship enforced to item.SalePrice -
// nothing recomputes it from a server-known reference price. So the exact
// same discounted sale_price can be submitted two ways: honestly declared
// (discount_pct set to match), which gates on approval, or silently
// (discount_pct left at 0), which does not - despite delivering the
// identical amount to the till and the identical figures to inventory/GL.
//
// This test asserts the SECURE/CORRECT outcome - the approval requirement
// depends on what the sale actually charges, not on a self-reported field.
//
// CLOSED by Stage 47.2 (2026-09-05) and PROMOTED out of the stage47redteam
// build tag per 47.0.1's own closure note, so it now runs on every ordinary
// `go test ./...`. What closes it: engines.ResolvePOSQuote prices every line
// from tenant master data, and handleCheckout gates on the larger of the
// client's declared discount and the server's own measured one
// (QuoteResult.ManualDiscountPct). The item this test seeds carries no server
// price at all, which is the residual case - handleCheckout routes such a cart
// to the lowest configured POSCart slab rather than letting an unverifiable
// price through, which is why BOTH submissions below now land in approval.
func TestA02DiscountApprovalBypassedByPriceTamperingInsteadOfDiscountPct(t *testing.T) {
	db.InitDB(testConnStr())
	tenantID := "default"
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		t.Fatalf("failed to resolve tenant schema: %v", err)
	}

	suffix := stage47UniqueID()
	sku := "A02SKU-" + suffix
	location := "A02LOC-" + suffix

	userID, cleanupUser := seedStage47User(t, "Cashier", location)
	defer cleanupUser()
	token := stage47Token(userID, "Cashier", location)

	itemData, _ := json.Marshal(map[string]interface{}{"name": "A-02 redteam item", "hsn_code": "6109", "gst_rate": 18.0})
	if _, err := db.DB.Exec(fmt.Sprintf(
		`INSERT INTO %s.documents (id, doctype, data, status, created_by) VALUES ($1, 'Item', $2, 'Active', 'system')`, schema),
		sku, itemData); err != nil {
		t.Fatalf("failed to seed item: %v", err)
	}
	defer db.DB.Exec(fmt.Sprintf(`DELETE FROM %s.documents WHERE id = $1 AND doctype = 'Item'`, schema), sku)

	if _, err := db.DB.Exec(fmt.Sprintf(
		`INSERT INTO %s.inventory_availability (sku, location_code, on_hand, available) VALUES ($1, $2, 100, 100)`, schema),
		sku, location); err != nil {
		t.Fatalf("failed to seed inventory: %v", err)
	}
	defer db.DB.Exec(fmt.Sprintf(`DELETE FROM %s.inventory_availability WHERE sku = $1 AND location_code = $2`, schema), sku, location)

	sessID := "A02SESS-" + suffix
	sessData, _ := json.Marshal(map[string]interface{}{"location": location, "cashier": userID, "status": "Open"})
	if _, err := db.DB.Exec(fmt.Sprintf(
		`INSERT INTO %s.documents (id, doctype, data, status, created_by) VALUES ($1, 'POSSession', $2, 'Open', $3)`, schema),
		sessID, sessData, userID); err != nil {
		t.Fatalf("failed to seed POS session: %v", err)
	}
	defer db.DB.Exec(fmt.Sprintf(`DELETE FROM %s.documents WHERE id = $1 AND doctype = 'POSSession'`, schema), sessID)

	// Stage 50/QA-DEF-01: this row is NOT test-only fixture data - Stage 20a
	// (db/migrations_stage20a_pos_maturity.sql) ships this exact
	// ('POSCart', 10, NULL, 'Store Manager') row as tenant_default's real
	// default policy via the same ON CONFLICT DO NOTHING shape, so on any
	// migrated schema this insert is always a no-op and the row already
	// there is real shipped configuration, not something this test created.
	// (The comment this replaced said db/migration.sql seeds no such row,
	// which is true but irrelevant - it never checked the later migration.)
	//
	// Stage 50/QA-DEF-01: the ON CONFLICT DO NOTHING above means this insert
	// is a no-op whenever a (POSCart, 10) rule already exists - a real
	// tenant's own configuration, or another test's row left behind by an
	// interrupted run. The unconditional DELETE that used to follow it threw
	// that row away regardless of who created it, corrupting whatever policy
	// was actually in force and producing exactly the order-sensitive
	// failures the audit reproduced (this test's own tamperedResp assertion,
	// and TestCheckoutToForecastIntegration's unrelated "Paid" assertion,
	// both depend on whether a POSCart rule happens to be active). Only
	// delete the row if this insert is the one that created it.
	result, err := db.DB.Exec(fmt.Sprintf(
		`INSERT INTO %s.approval_rules (doctype, min_amount, max_amount, required_role) VALUES ('POSCart', 10, NULL, 'Store Manager') ON CONFLICT (doctype, min_amount) DO NOTHING`, schema))
	if err != nil {
		t.Fatalf("failed to seed approval rule: %v", err)
	}
	if inserted, _ := result.RowsAffected(); inserted > 0 {
		defer db.DB.Exec(fmt.Sprintf(`DELETE FROM %s.approval_rules WHERE doctype = 'POSCart' AND min_amount = 10`, schema))
	}

	checkout := func(cartNumber string, discountPct float64) map[string]interface{} {
		reqBody := map[string]interface{}{
			"cart_number":  cartNumber,
			"location":     location,
			"payment_mode": "Cash",
			"discount_pct": discountPct,
			"items": []map[string]interface{}{
				{"sku": sku, "qty": 1, "sale_price": 500.0, "cost_price": 300.0},
			},
		}
		b, _ := json.Marshal(reqBody)
		req := httptest.NewRequest(http.MethodPost, "/api/v1/checkout", bytes.NewReader(b))
		req.Header.Set("Authorization", "Bearer "+token)
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		apiMiddleware(handleCheckout)(rec, req)
		var resp map[string]interface{}
		_ = json.Unmarshal(rec.Body.Bytes(), &resp)
		return resp
	}

	honestCart := "A02HONEST-" + suffix
	defer db.DB.Exec(fmt.Sprintf(`DELETE FROM %s.documents WHERE id = $1 AND doctype = 'POSCart'`, schema), honestCart)
	honestResp := checkout(honestCart, 20)
	if honestResp["status"] != "pending_approval" {
		t.Fatalf("sanity check failed: sale_price=500 honestly declared as a 20%%-discount (above the seeded 10%% threshold) did not land in pending_approval (got status=%v) - cannot evaluate the A-02 bypass without the approval gate itself working; check the approval_rules fixture", honestResp["status"])
	}

	tamperedCart := "A02TAMPER-" + suffix
	defer db.DB.Exec(fmt.Sprintf(`DELETE FROM %s.documents WHERE id = $1 AND doctype = 'POSCart'`, schema), tamperedCart)
	tamperedResp := checkout(tamperedCart, 0)
	if tamperedResp["status"] != "pending_approval" {
		t.Fatalf("A-02: the identical sale_price (500 for 1 unit) that required Store Manager approval when honestly declared as a 20%% discount completed immediately with NO approval (status=%v) when the same price was submitted with discount_pct=0 - the approval gate keys entirely on the client-reported discount_pct field, not on the price actually charged, so a cashier can under-report the discount and bypass approval outright. Must also land in pending_approval once 47.1.3/47.2 resolve price/discount server-side instead of trusting req.DiscountPct.", tamperedResp["status"])
	}
}
