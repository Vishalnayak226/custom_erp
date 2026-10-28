package server

// Stage 49.5.6a: offline sales are validated before they post. A checkout the
// till sends with offline_synced is held (pos_offline_sales), never posted;
// POS > Offline Sales validates it - every line scanned with that item's own
// barcode, by someone other than its cashier, with the capability - and only
// then does it post, once, through the normal checkout. Reject never posts.

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"custom_erp/db"
)

type offlineSaleFixture struct {
	*posPricingFixture
	barcode string
	soldAt  string // the till's queuedAt: fixed for one queue entry
}

func newOfflineSaleFixture(t *testing.T) *offlineSaleFixture {
	t.Helper()
	f := newPOSPricingFixture(t, 1000)
	var present bool
	if err := db.DB.QueryRow(`SELECT to_regclass('tenant_default.pos_offline_sales') IS NOT NULL`).Scan(&present); err != nil || !present {
		t.Skip("migrations_stage49_5_6a_offline_sale_validation.sql is not applied to this database")
	}
	o := &offlineSaleFixture{posPricingFixture: f, barcode: "8904956" + f.suffix, soldAt: time.Now().Add(-time.Hour).UTC().Format(time.RFC3339)}
	if _, err := db.DB.Exec(fmt.Sprintf(`UPDATE %s.documents SET data = data || jsonb_build_object('barcode', $2::text) WHERE id = $1 AND doctype = 'Item'`, f.schema), f.sku, o.barcode); err != nil {
		t.Fatalf("give the item a barcode: %v", err)
	}
	t.Cleanup(func() {
		db.DB.Exec(fmt.Sprintf(`DELETE FROM %s.pos_offline_sales WHERE location = $1`, f.schema), f.location)
		db.DB.Exec(fmt.Sprintf(`DELETE FROM %s.documents WHERE doctype = 'StockLedgerEntry' AND data->>'item_id' = $1`, f.schema), f.sku)
		db.DB.Exec(fmt.Sprintf(`DELETE FROM %s.gl_postings WHERE document_id LIKE $1`, f.schema), "%"+f.suffix+"%")
	})
	return o
}

// offlineCheckout is the till's queue replaying one sale.
func (o *offlineSaleFixture) offlineCheckout(token, cart, scanned string, qty int) (int, map[string]interface{}) {
	o.t.Helper()
	return o.checkout(token, map[string]interface{}{
		"cart_number": cart, "location": o.location, "payment_mode": "Cash",
		"offline_synced": true, "sold_at": o.soldAt,
		"items": []map[string]interface{}{{"sku": o.sku, "qty": qty, "scanned": scanned}},
	})
}

// call drives a handler through apiMiddleware with a path value, as the mux would.
func (o *offlineSaleFixture) call(handler http.HandlerFunc, method, path, id, token string, body interface{}) (int, map[string]interface{}) {
	o.t.Helper()
	b, _ := json.Marshal(body)
	req := httptest.NewRequest(method, path, bytes.NewReader(b))
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	if id != "" {
		req.SetPathValue("id", id)
	}
	rec := httptest.NewRecorder()
	apiMiddleware(handler)(rec, req)
	var resp map[string]interface{}
	_ = json.Unmarshal(rec.Body.Bytes(), &resp)
	return rec.Code, resp
}

func (o *offlineSaleFixture) decide(action, id, token, reason string) (int, map[string]interface{}) {
	h := handleValidateOfflineSale
	if action == "reject" {
		h = handleRejectOfflineSale
	}
	return o.call(h, http.MethodPost, "/api/v1/pos/offline-sales/"+id+"/"+action, id, token, map[string]string{"reason": reason})
}

func (o *offlineSaleFixture) hold(cart, scanned string, qty int) string {
	o.t.Helper()
	status, resp := o.offlineCheckout(o.cashierToken, cart, scanned, qty)
	if status != http.StatusAccepted || resp["status"] != "held_for_validation" {
		o.t.Fatalf("offline checkout = %d %v, want 202 held_for_validation", status, resp)
	}
	id, _ := resp["offline_sale_id"].(string)
	return id
}

func (o *offlineSaleFixture) onHand() int {
	var n int
	_ = db.DB.QueryRow(fmt.Sprintf(`SELECT on_hand FROM %s.inventory_availability WHERE sku = $1 AND location_code = $2`, o.schema), o.sku, o.location).Scan(&n)
	return n
}

func (o *offlineSaleFixture) count(query string, args ...interface{}) int {
	var n int
	if err := db.DB.QueryRow(fmt.Sprintf(query, o.schema), args...).Scan(&n); err != nil {
		o.t.Fatalf("count: %v", err)
	}
	return n
}

// posted reports what a sale left behind: its cart, stock ledger rows, GL rows.
func (o *offlineSaleFixture) posted(cart string) (carts, ledger, gl int) {
	carts = o.count(`SELECT COUNT(*) FROM %s.documents WHERE doctype = 'POSCart' AND id = $1 AND status <> 'Failed'`, cart)
	ledger = o.count(`SELECT COUNT(*) FROM %s.documents WHERE doctype = 'StockLedgerEntry' AND data->>'item_id' = $1`, o.sku)
	gl = o.count(`SELECT COUNT(*) FROM %s.gl_postings WHERE document_id = $1`, cart)
	return
}

func errCode(resp map[string]interface{}) string {
	c, _ := resp["code"].(string)
	return c
}

func TestStage4956aOfflineCheckoutIsHeldNotPosted(t *testing.T) {
	o := newOfflineSaleFixture(t)
	cart := "OFS-HELD-" + o.suffix
	id := o.hold(cart, o.barcode, 2)
	// The queue replays the same entry (a lost answer): the same row, no second.
	status, resp := o.offlineCheckout(o.cashierToken, cart, o.barcode, 2)
	if status != http.StatusAccepted || resp["offline_sale_id"] != id || resp["already_held"] != true {
		t.Fatalf("replay = %d %v, want 202 with the same offline sale %s", status, resp, id)
	}
	if n := o.count(`SELECT COUNT(*) FROM %s.pos_offline_sales WHERE cart_number = $1`, cart); n != 1 {
		t.Fatalf("%d held rows for one sale replayed twice, want 1", n)
	}
	if carts, ledger, gl := o.posted(cart); carts+ledger+gl != 0 {
		t.Fatalf("a held offline sale posted: %d cart, %d stock ledger, %d GL rows", carts, ledger, gl)
	}
	if got := o.onHand(); got != 500 {
		t.Fatalf("on hand %d after a held sale, want 500 (nothing deducted until validated)", got)
	}
	var cashier, session string
	var soldAt *time.Time
	_ = db.DB.QueryRow(fmt.Sprintf(`SELECT cashier, pos_session, sold_at FROM %s.pos_offline_sales WHERE id = $1`, o.schema), id).Scan(&cashier, &session, &soldAt)
	if cashier != o.cashierID || session == "" || soldAt == nil {
		t.Fatalf("held row cashier=%q session=%q sold_at=%v - want the cashier, the open session and the till's sale time", cashier, session, soldAt)
	}
}

func TestStage4956aValidatePostsOnceAndDeducts(t *testing.T) {
	o := newOfflineSaleFixture(t)
	cart := "OFS-OK-" + o.suffix
	id := o.hold(cart, o.barcode, 2)
	var heldSession string
	_ = db.DB.QueryRow(fmt.Sprintf(`SELECT pos_session FROM %s.pos_offline_sales WHERE id = $1`, o.schema), id).Scan(&heldSession)
	// The cashier closed the till before the manager got to it: the sale
	// still posts, into the session it was rung up in.
	db.DB.Exec(fmt.Sprintf(`UPDATE %s.documents SET status = 'Closed' WHERE id = $1 AND doctype = 'POSSession'`, o.schema), heldSession)

	status, resp := o.decide("validate", id, o.superviserTok, "")
	if status != http.StatusOK || resp["status"] != "Validated" || resp["bill_number"] == "" {
		t.Fatalf("validate = %d %v, want 200 Validated with a bill number", status, resp)
	}
	if got := o.onHand(); got != 498 {
		t.Fatalf("on hand %d after validating a sale of 2, want 498", got)
	}
	carts, ledger, gl := o.posted(cart)
	if carts != 1 || ledger == 0 || gl == 0 {
		t.Fatalf("validated sale left %d cart, %d stock ledger, %d GL rows - want it posted like any sale", carts, ledger, gl)
	}
	stored := o.storedCart(cart)
	if stored["pos_session"] != heldSession || stored["cashier"] != o.cashierID {
		t.Fatalf("posted cart session=%v cashier=%v, want the held session %s and the original cashier", stored["pos_session"], stored["cashier"], heldSession)
	}
	// A second click: refused, and nothing deducted again.
	status, resp = o.decide("validate", id, o.superviserTok, "")
	if status != http.StatusUnprocessableEntity || errCode(resp) != "POSOFF-1362" {
		t.Fatalf("second validate = %d %v, want 422 POSOFF-1362", status, resp)
	}
	if got := o.onHand(); got != 498 {
		t.Fatalf("on hand %d after a second validate, want 498 (posted once)", got)
	}
	if n := o.count(`SELECT COUNT(*) FROM %s.audit_logs WHERE action = 'POS_OFFLINE_SALE_VALIDATED' AND details LIKE $1`, "%"+id+"%"); n != 1 {
		t.Fatalf("%d validation audit rows, want 1", n)
	}
	// The till replaying the same entry after validation is still told it is held - never a second post.
	if status, _ := o.offlineCheckout(o.cashierToken, cart, o.barcode, 2); status != http.StatusOK && status != http.StatusAccepted {
		t.Fatalf("replay after validation = %d, want a success the queue drops", status)
	}
	if got := o.onHand(); got != 498 {
		t.Fatalf("on hand %d after the till replayed a validated sale, want 498", got)
	}
}

func TestStage4956aMismatchedOrUnscannedLineIsRefused(t *testing.T) {
	o := newOfflineSaleFixture(t)
	cases := []struct{ name, scanned string }{
		{"another code", "0000" + o.suffix}, {"not scanned", ""}, {"the SKU of a barcoded item", o.sku},
	}
	for i, c := range cases {
		scanned := c.scanned
		t.Run(c.name, func(t *testing.T) {
			cart := fmt.Sprintf("OFS-BAD-%s-%d", o.suffix, i)
			id := o.hold(cart, scanned, 1)
			status, resp := o.decide("validate", id, o.superviserTok, "")
			if status != http.StatusUnprocessableEntity || errCode(resp) != "POSOFF-1361" {
				t.Fatalf("validate a mismatched sale = %d %v, want 422 POSOFF-1361", status, resp)
			}
			if carts, _, gl := o.posted(cart); carts+gl != 0 {
				t.Fatalf("a refused sale posted: %d cart, %d GL rows", carts, gl)
			}
			if n := o.count(`SELECT COUNT(*) FROM %s.pos_offline_sales WHERE id = $1 AND status = 'Pending'`, id); n != 1 {
				t.Fatal("a refused validation changed the sale's status")
			}
		})
	}
	if got := o.onHand(); got != 500 {
		t.Fatalf("on hand %d after refused validations, want 500", got)
	}
}

func TestStage4956aRejectNeverPosts(t *testing.T) {
	o := newOfflineSaleFixture(t)
	cart := "OFS-REJ-" + o.suffix
	id := o.hold(cart, o.barcode, 1)
	if status, resp := o.decide("reject", id, o.superviserTok, "  "); status != http.StatusUnprocessableEntity || errCode(resp) != "POSOFF-1362" {
		t.Fatalf("reject with no reason = %d %v, want 422 POSOFF-1362", status, resp)
	}
	status, resp := o.decide("reject", id, o.superviserTok, "goods came back")
	if status != http.StatusOK || resp["status"] != "Rejected" || resp["reject_reason"] != "goods came back" {
		t.Fatalf("reject = %d %v, want 200 Rejected with the reason", status, resp)
	}
	if status, resp := o.decide("validate", id, o.superviserTok, ""); status != http.StatusUnprocessableEntity || errCode(resp) != "POSOFF-1362" {
		t.Fatalf("validate a rejected sale = %d %v, want 422 POSOFF-1362", status, resp)
	}
	if carts, ledger, gl := o.posted(cart); carts+ledger+gl != 0 || o.onHand() != 500 {
		t.Fatalf("a rejected sale posted: %d cart, %d ledger, %d GL rows, on hand %d", carts, ledger, gl, o.onHand())
	}
	if n := o.count(`SELECT COUNT(*) FROM %s.audit_logs WHERE action = 'POS_OFFLINE_SALE_REJECTED' AND details LIKE $1`, "%"+id+"%"); n != 1 {
		t.Fatalf("%d rejection audit rows, want 1", n)
	}
}

func TestStage4956aCashierCannotDecideOwnSale(t *testing.T) {
	o := newOfflineSaleFixture(t)
	// The store manager rings a sale up offline at their own till...
	sess := "OFS-SESS-" + o.suffix
	data, _ := json.Marshal(map[string]interface{}{"location": o.location, "cashier": o.superviserID, "status": "Open"})
	if _, err := db.DB.Exec(fmt.Sprintf(`INSERT INTO %s.documents (id, doctype, data, status, created_by) VALUES ($1, 'POSSession', $2, 'Open', $3)`, o.schema), sess, data, o.superviserID); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.DB.Exec(fmt.Sprintf(`DELETE FROM %s.documents WHERE id = $1`, o.schema), sess) })
	cart := "OFS-OWN-" + o.suffix
	status, resp := o.offlineCheckout(o.superviserTok, cart, o.barcode, 1)
	if status != http.StatusAccepted {
		t.Fatalf("manager's offline checkout = %d %v", status, resp)
	}
	id, _ := resp["offline_sale_id"].(string)
	// ...and cannot validate or reject it themselves.
	for _, action := range []string{"validate", "reject"} {
		if status, resp := o.decide(action, id, o.superviserTok, "mine"); status != http.StatusUnprocessableEntity || errCode(resp) != "POSOFF-1362" {
			t.Fatalf("%s own sale = %d %v, want 422 POSOFF-1362", action, status, resp)
		}
	}
	if carts, _, _ := o.posted(cart); carts != 0 {
		t.Fatal("the manager's own offline sale posted on their own validation")
	}
	// Another manager can.
	if status, resp := o.decide("validate", id, o.adminToken, ""); status != http.StatusOK || resp["status"] != "Validated" {
		t.Fatalf("another manager's validate = %d %v, want 200 Validated", status, resp)
	}
}

func TestStage4956aOfflineSalesNeedTheCapability(t *testing.T) {
	o := newOfflineSaleFixture(t)
	id := o.hold("OFS-CAP-"+o.suffix, o.barcode, 1)
	checks := []struct {
		name    string
		handler http.HandlerFunc
		method  string
		path    string
	}{
		{"list", handleListOfflineSales, http.MethodGet, "/api/v1/pos/offline-sales"},
		{"open", handleGetOfflineSale, http.MethodGet, "/api/v1/pos/offline-sales/" + id},
		{"validate", handleValidateOfflineSale, http.MethodPost, "/api/v1/pos/offline-sales/" + id + "/validate"},
		{"reject", handleRejectOfflineSale, http.MethodPost, "/api/v1/pos/offline-sales/" + id + "/reject"},
	}
	for _, c := range checks {
		if status, resp := o.call(c.handler, c.method, c.path, id, o.cashierToken, map[string]string{"reason": "x"}); status != http.StatusForbidden {
			t.Errorf("a Cashier's %s = %d %v, want 403", c.name, status, resp)
		}
	}
	// The manager sees it, with its lines checked.
	status, resp := o.call(handleGetOfflineSale, http.MethodGet, "/api/v1/pos/offline-sales/"+id, id, o.superviserTok, nil)
	lines, _ := resp["lines"].([]interface{})
	if status != http.StatusOK || len(lines) != 1 {
		t.Fatalf("manager open = %d %v, want 200 with one line", status, resp)
	}
	if line, _ := lines[0].(map[string]interface{}); line["match"] != true || line["scanned"] != o.barcode {
		t.Fatalf("line = %v, want scanned %s matching", line, o.barcode)
	}
	if n := o.count(`SELECT COUNT(*) FROM %s.pos_offline_sales WHERE id = $1 AND status = 'Pending'`, id); n != 1 {
		t.Fatal("a Cashier's refused validate/reject changed the sale")
	}
}
