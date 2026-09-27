package server

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"custom_erp/db"
	"custom_erp/engines"
)

// BLD-031 - providers, public API and extensions: the "bad signatures" local
// failure campaign for the one real inbound webhook this codebase verifies a
// provider-side signature on (verifyShopifyWebhookSignature, middleware.go).
// Before this file, neither that function nor handleShopifyOrderWebhook/
// handleShopifyProductMap (handlers_operations.go) had ANY test coverage at
// all - a real gap for the one HMAC-checked inbound-webhook contract in the
// codebase. The generic HMAC helper (engines.VerifyWebhookHMAC) and the
// BigCommerce connector's use of it are covered elsewhere
// (TestVerifyBigCommerceWebhook); this is Shopify's own hand-rolled check.
//
// TestBLD031ShopifyWebhookDuplicateDeliveryCreatesExactlyOneSalesOrder also
// closes the loop on engines/channel_orders.go's BLD-031 concurrency fix
// (acquireChannelOrderLock) end to end through the real HTTP path a genuine
// Shopify redelivery would take - signature verification included, not just
// the underlying engine call.

func bld031ShopifyFixture(t *testing.T) http.Handler {
	t.Helper()
	db.InitDB(testConnStr())
	previous := http.DefaultServeMux
	http.DefaultServeMux = http.NewServeMux()
	registerRoutes()
	mux := http.DefaultServeMux
	t.Cleanup(func() { http.DefaultServeMux = previous })
	return mux
}

// bld031SetShopifySecret overrides the package-level shopifyWebhookSecret for
// the duration of one test and restores it on cleanup - the same technique
// verifyShopifyWebhookSignature's own doc comment describes the variable
// being sourced from (SHOPIFY_WEBHOOK_SECRET at process start), made
// controllable here since a real env var can't be changed per-subtest.
func bld031SetShopifySecret(t *testing.T, secret string) {
	t.Helper()
	previous := shopifyWebhookSecret
	shopifyWebhookSecret = secret
	t.Cleanup(func() { shopifyWebhookSecret = previous })
}

func bld031ShopifySign(body []byte, secret string) string {
	h := hmac.New(sha256.New, []byte(secret))
	h.Write(body)
	return base64.StdEncoding.EncodeToString(h.Sum(nil))
}

func bld031ShopifyOrderPayload(orderID, sku string) []byte {
	body, _ := json.Marshal(map[string]interface{}{
		"id": orderID,
		"customer": map[string]interface{}{
			"first_name": "BLD031", "last_name": "Shopify",
		},
		"shipping_address": map[string]interface{}{
			"address1": "1 Shopify Road", "address2": "", "city": "Bengaluru", "zip": "560001",
		},
		"financial_status": "paid",
		"line_items": []map[string]interface{}{
			{"sku": sku, "qty": 2, "price": "55.00"},
		},
	})
	return body
}

// TestBLD031ShopifyWebhookRejectsBadSignatures is the bad-signature campaign:
// a missing signature header, a well-formed but wrong signature, and a
// correct signature computed over a body that was then tampered with (the
// signature no longer matches what actually arrived) must all be refused
// with the same generic 401 - and a genuinely correct signature over the
// real body must be accepted.
func TestBLD031ShopifyWebhookRejectsBadSignatures(t *testing.T) {
	handler := bld031ShopifyFixture(t)
	bld031SetShopifySecret(t, "bld031-test-shopify-secret")
	schema, err := db.GetTenantSchema("default")
	if err != nil {
		t.Fatal(err)
	}

	sku := engines.NewDocID("BLD031SHOPSKU")
	orderID := engines.NewDocID("BLD031SHOPORDER")
	seedShopifyItem(t, schema, sku)
	defer cleanupShopifyOrder(schema, orderID, sku)

	body := bld031ShopifyOrderPayload(orderID, sku)

	post := func(reqBody []byte, sigHeader string) *httptest.ResponseRecorder {
		req := httptest.NewRequest("POST", "/api/v1/integration/shopify/order", bytes.NewReader(reqBody))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("X-Tenant-ID", "default")
		if sigHeader != "" {
			req.Header.Set("X-Shopify-Hmac-Sha256", sigHeader)
		}
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		return rec
	}

	if r := post(body, ""); r.Code != http.StatusUnauthorized {
		t.Fatalf("missing signature: got %d %s, want 401", r.Code, r.Body.String())
	}
	if r := post(body, bld031ShopifySign(body, "the-wrong-secret")); r.Code != http.StatusUnauthorized {
		t.Fatalf("wrong-secret signature: got %d %s, want 401", r.Code, r.Body.String())
	}
	if r := post(body, base64.StdEncoding.EncodeToString([]byte("not-a-real-hmac-but-valid-base64"))); r.Code != http.StatusUnauthorized {
		t.Fatalf("malformed signature: got %d %s, want 401", r.Code, r.Body.String())
	}
	validSig := bld031ShopifySign(body, "bld031-test-shopify-secret")
	tamperedBody := bld031ShopifyOrderPayload(orderID+"-TAMPERED", sku)
	if r := post(tamperedBody, validSig); r.Code != http.StatusUnauthorized {
		t.Fatalf("signature computed over a different body: got %d %s, want 401 (the signature must be checked against the body that actually arrived)", r.Code, r.Body.String())
	}

	// The genuine article must go through.
	r := post(body, validSig)
	if r.Code != http.StatusOK {
		t.Fatalf("correctly signed request: got %d %s, want 200", r.Code, r.Body.String())
	}
	var count int
	if err := db.DB.QueryRow("SELECT count(*) FROM "+schema+".documents WHERE doctype = 'SalesOrder' AND data->>'channel_order_id' = $1", orderID).Scan(&count); err != nil {
		t.Fatalf("count: %v", err)
	}
	if count != 1 {
		t.Fatalf("expected the correctly-signed webhook to create exactly 1 SalesOrder, got %d", count)
	}
}

// TestBLD031ShopifyWebhookFailsClosedWhenSecretUnset proves the fail-closed
// posture middleware.go's own comment states (24.35): an unset secret must
// reject every inbound call, never silently accept it as "verification not
// configured, trust it anyway".
func TestBLD031ShopifyWebhookFailsClosedWhenSecretUnset(t *testing.T) {
	handler := bld031ShopifyFixture(t)
	bld031SetShopifySecret(t, "")

	sku := engines.NewDocID("BLD031SHOPUNSETSKU")
	orderID := engines.NewDocID("BLD031SHOPUNSETORDER")
	body := bld031ShopifyOrderPayload(orderID, sku)
	// A signature computed with a blank secret is exactly what an attacker
	// who noticed the misconfiguration could also compute - it must still be
	// refused, not accepted because it "matches".
	sig := bld031ShopifySign(body, "")

	req := httptest.NewRequest("POST", "/api/v1/integration/shopify/order", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Tenant-ID", "default")
	req.Header.Set("X-Shopify-Hmac-Sha256", sig)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("with SHOPIFY_WEBHOOK_SECRET unset: got %d %s, want 401 (fail closed)", rec.Code, rec.Body.String())
	}
}

// TestBLD031ShopifyWebhookDuplicateDeliveryCreatesExactlyOneSalesOrder sends
// the identical, correctly-signed Shopify order webhook concurrently through
// the real HTTP path - closing the loop on the channel_orders.go
// acquireChannelOrderLock fix end to end, not just at the engine layer.
func TestBLD031ShopifyWebhookDuplicateDeliveryCreatesExactlyOneSalesOrder(t *testing.T) {
	handler := bld031ShopifyFixture(t)
	bld031SetShopifySecret(t, "bld031-test-shopify-secret-2")
	schema, err := db.GetTenantSchema("default")
	if err != nil {
		t.Fatal(err)
	}

	sku := engines.NewDocID("BLD031SHOPDUPSKU")
	orderID := engines.NewDocID("BLD031SHOPDUPORDER")
	seedShopifyItem(t, schema, sku)
	defer cleanupShopifyOrder(schema, orderID, sku)

	body := bld031ShopifyOrderPayload(orderID, sku)
	sig := bld031ShopifySign(body, "bld031-test-shopify-secret-2")

	const concurrency = 8
	var wg sync.WaitGroup
	codes := make([]int, concurrency)
	start := make(chan struct{})
	for i := 0; i < concurrency; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			<-start
			req := httptest.NewRequest("POST", "/api/v1/integration/shopify/order", bytes.NewReader(body))
			req.Header.Set("Content-Type", "application/json")
			req.Header.Set("X-Tenant-ID", "default")
			req.Header.Set("X-Shopify-Hmac-Sha256", sig)
			rec := httptest.NewRecorder()
			handler.ServeHTTP(rec, req)
			codes[i] = rec.Code
		}(i)
	}
	close(start)
	wg.Wait()

	for i, code := range codes {
		if code != http.StatusOK {
			t.Fatalf("goroutine %d: concurrent duplicate delivery got %d, want 200 on every retry", i, code)
		}
	}
	var count int
	if err := db.DB.QueryRow("SELECT count(*) FROM "+schema+".documents WHERE doctype = 'SalesOrder' AND data->>'channel_order_id' = $1", orderID).Scan(&count); err != nil {
		t.Fatalf("count: %v", err)
	}
	if count != 1 {
		t.Fatalf("counterexample: %d SalesOrder documents exist after %d concurrent identical Shopify webhook deliveries, want exactly 1", count, concurrency)
	}
}

func seedShopifyItem(t *testing.T, schema, sku string) {
	t.Helper()
	item, _ := json.Marshal(map[string]interface{}{"code": sku, "name": "BLD-031 Shopify webhook test item"})
	if _, err := db.DB.Exec("INSERT INTO "+schema+".documents (id, doctype, data, status, created_by) VALUES ($1, 'Item', $2, 'Active', 'system')", "ITEM-"+sku, item); err != nil {
		t.Fatalf("seed item: %v", err)
	}
	if _, err := db.DB.Exec("INSERT INTO "+schema+".inventory_availability (sku, location_code, on_hand, available) VALUES ($1, 'HO', 1000, 1000)", sku); err != nil {
		t.Fatalf("seed inventory: %v", err)
	}
}

func cleanupShopifyOrder(schema, orderID, sku string) {
	db.DB.Exec("DELETE FROM "+schema+".channel_order_mapping WHERE channel = 'Shopify' AND channel_order_id = $1", orderID)
	db.DB.Exec("DELETE FROM "+schema+".documents WHERE doctype = 'SalesOrderLine' AND data->>'order_id' IN (SELECT id FROM "+schema+".documents WHERE doctype = 'SalesOrder' AND data->>'channel_order_id' = $1)", orderID)
	db.DB.Exec("DELETE FROM "+schema+".documents WHERE doctype = 'SalesOrder' AND data->>'channel_order_id' = $1", orderID)
	db.DB.Exec("DELETE FROM "+schema+".inventory_availability WHERE sku = $1", sku)
	db.DB.Exec("DELETE FROM "+schema+".documents WHERE id = $1", "ITEM-"+sku)
}

