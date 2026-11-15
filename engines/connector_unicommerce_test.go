package engines

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// Item 146.4.1: a mock Uniware plays the documented API; no live call is made.
func unicommerceMock(t *testing.T, adjustBodies *[]map[string]interface{}, adjustFacilities *[]string) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("/oauth/token", func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		if q.Get("grant_type") != "password" || q.Get("client_id") != "my-trusted-client" || q.Get("username") != "u" || q.Get("password") != "p" {
			http.Error(w, "bad grant", http.StatusUnauthorized)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]interface{}{"access_token": "tok-1", "token_type": "bearer", "expires_in": 3600})
	})
	authed := func(h http.HandlerFunc) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			if r.Header.Get("Authorization") != "bearer tok-1" {
				http.Error(w, "no token", http.StatusUnauthorized)
				return
			}
			h(w, r)
		}
	}
	mux.HandleFunc("/services/rest/v1/oms/saleOrder/search", authed(func(w http.ResponseWriter, r *http.Request) {
		var in map[string]interface{}
		_ = json.NewDecoder(r.Body).Decode(&in)
		_ = json.NewEncoder(w).Encode(map[string]interface{}{"successful": true, "totalRecords": 4, "elements": []map[string]string{
			{"code": "SO1", "status": "PROCESSING"}, {"code": "SO2", "status": "CANCELLED"}, {"code": "SO3", "status": "PENDING_VERIFICATION"}, {"code": "SO4", "status": "COMPLETE"},
		}})
	}))
	mux.HandleFunc("/services/rest/v1/oms/saleorder/get", authed(func(w http.ResponseWriter, r *http.Request) {
		var in map[string]interface{}
		_ = json.NewDecoder(r.Body).Decode(&in)
		if in["code"] != "SO1" {
			_ = json.NewEncoder(w).Encode(map[string]interface{}{"successful": false, "errors": []map[string]string{{"code": "NOT_FOUND", "description": "no such order"}}})
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]interface{}{"successful": true, "saleOrderDTO": map[string]interface{}{
			"code": "SO1", "status": "PROCESSING", "cod": true,
			"addresses": []map[string]string{{"name": "Asha", "addressLine1": "1 Main Rd", "city": "Pune", "state": "MH", "pincode": "411001", "phone": "9999999999"}},
			"saleOrderItems": []map[string]interface{}{
				{"code": "I1", "itemSku": "SKU-A", "facilityCode": "WH2", "sellingPrice": 100.5, "statusCode": "CREATED", "shippingPackageCode": "SP1"},
				{"code": "I2", "itemSku": "SKU-A", "facilityCode": "WH2", "sellingPrice": "100.5", "statusCode": "CREATED", "shippingPackageCode": "SP1"},
				{"code": "I3", "itemSku": "SKU-B", "facilityCode": "WH2", "sellingPrice": 40, "statusCode": "CANCELLED"},
			}}})
	}))
	mux.HandleFunc("/services/rest/v1/inventory/adjust/bulk", authed(func(w http.ResponseWriter, r *http.Request) {
		var in map[string]interface{}
		_ = json.NewDecoder(r.Body).Decode(&in)
		*adjustBodies = append(*adjustBodies, in)
		*adjustFacilities = append(*adjustFacilities, r.Header.Get("Facility"))
		if r.Header.Get("Facility") == "BAD" {
			_ = json.NewEncoder(w).Encode(map[string]interface{}{"successful": false, "message": "facility closed"})
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]interface{}{"successful": true})
	}))
	mux.HandleFunc("/services/rest/v1/invoice/details/get", authed(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]interface{}{"successful": true, "invoice": map[string]string{"code": "INV-9", "displayCode": "UC/2627/0009", "shippingPackageCode": "SP1"}})
	}))
	return httptest.NewServer(mux)
}

func TestUnicommerceConnector(t *testing.T) {
	var bodies []map[string]interface{}
	var facilities []string
	srv := unicommerceMock(t, &bodies, &facilities)
	defer srv.Close()
	cred := map[string]string{"base_url": srv.URL, "username": "u", "password": "p", "facility_code": "WH1, WH2", "shelf_code": "SH1"}
	ctx := context.Background()
	c := unicommerceConnector{}

	if err := c.ValidateCredentials(cred); err != nil {
		t.Fatalf("valid credential rejected: %v", err)
	}
	if err := c.ValidateCredentials(map[string]string{"tenant": "x", "username": "u", "password": "p"}); err == nil {
		t.Fatal("a credential without a facility must be refused")
	}

	// Orders in: cancelled, unverified and completed orders are skipped; the
	// two units of SKU-A merge, the cancelled item is dropped, the order lands
	// at its item's facility and COD is carried.
	page, err := c.PullOrders(ctx, cred, ConnectorPullRequest{UpdatedAfter: time.Now().Add(-time.Hour), Limit: 10})
	if err != nil {
		t.Fatalf("pull: %v", err)
	}
	if len(page.Orders) != 1 || page.NextCursor != "" {
		t.Fatalf("want one order and no next page, got %+v", page)
	}
	o := page.Orders[0]
	if o.MarketplaceInvoiceNo != "UC/2627/0009" {
		t.Fatalf("pull must carry the invoice Uniware issued, got %q", o.MarketplaceInvoiceNo)
	}
	if o.ChannelOrderID != "SO1" || o.LocationCode != "WH2" || o.PaymentStatus != "COD" || o.ShippingState != "MH" {
		t.Fatalf("order mapped wrong: %+v", o)
	}
	if len(o.Lines) != 1 || o.Lines[0].ChannelSKU != "SKU-A" || o.Lines[0].Quantity != 2 || o.Lines[0].UnitPrice != 100.5 {
		t.Fatalf("lines mapped wrong: %+v", o.Lines)
	}
	if !strings.Contains(o.ShippingAddress, "Pune") {
		t.Fatalf("address lost: %q", o.ShippingAddress)
	}

	// Stock out: one call per facility, REPLACE, the Facility header set.
	err = c.PushInventory(ctx, cred, []ConnectorInventoryUpdate{
		{ChannelSKU: "SKU-A", Quantity: 7, LocationCode: "WH2"}, {ChannelSKU: "SKU-B", Quantity: 3}, {ChannelSKU: "SKU-C", Quantity: 1, LocationCode: "WH2"},
	})
	if err != nil {
		t.Fatalf("push inventory: %v", err)
	}
	if len(facilities) != 2 || facilities[0] != "WH1" || facilities[1] != "WH2" {
		t.Fatalf("want one call per facility (WH1, WH2), got %v", facilities)
	}
	adj := bodies[1]["inventoryAdjustments"].([]interface{})
	first := adj[0].(map[string]interface{})
	if len(adj) != 2 || first["adjustmentType"] != "REPLACE" || first["shelfCode"] != "SH1" || first["itemSKU"] != "SKU-A" {
		t.Fatalf("adjustment wrong: %v", adj)
	}

	// A refusal inside a 200 response is an error; a missing shelf is caught early.
	bad := map[string]string{"base_url": srv.URL, "username": "u", "password": "p", "facility_code": "BAD", "shelf_code": "SH1"}
	if err := c.PushInventory(ctx, bad, []ConnectorInventoryUpdate{{ChannelSKU: "X", Quantity: 1}}); err == nil || !strings.Contains(err.Error(), "facility closed") {
		t.Fatalf("a refused stock update must surface, got %v", err)
	}
	if err := c.PushInventory(ctx, map[string]string{"base_url": srv.URL, "access_token": "tok-1", "facility_code": "WH1"}, nil); err == nil {
		t.Fatal("stock push without shelf_code must fail")
	}

	// A wrong password is an auth error, not a panic.
	if _, err := c.PullOrders(ctx, map[string]string{"base_url": srv.URL, "username": "u", "password": "wrong", "facility_code": "WH1"}, ConnectorPullRequest{}); err == nil {
		t.Fatal("a wrong password must fail")
	}

	// Status back is read, not pushed; the invoice number is Uniware's own.
	if err := c.PushOrderStatus(ctx, cred, ConnectorStatusUpdate{ChannelOrderID: "SO1", Status: "Shipped"}); err != ErrOrderDecisionPushUnsupported {
		t.Fatalf("status push should be unsupported, got %v", err)
	}
	st, err := UnicommerceOrderStatus(ctx, cred, "SO1")
	if err != nil || st.Status != "PROCESSING" || len(st.Packages) != 1 || st.Packages[0].InvoiceDisplayCode != "UC/2627/0009" {
		t.Fatalf("status/invoice read wrong: %+v err=%v", st, err)
	}
	if _, err := UnicommerceOrderStatus(ctx, cred, "NOPE"); err == nil || !strings.Contains(err.Error(), "no such order") {
		t.Fatalf("an unknown order must surface Uniware's reason, got %v", err)
	}
}
