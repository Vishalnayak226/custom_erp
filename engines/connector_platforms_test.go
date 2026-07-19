package engines

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// Platform-connector tests (Stage 16.2-16.4). Each stands up an
// httptest.Server playing the platform's API and asserts the connector
// sends the right auth header and payload shape, and correctly parses
// success and rejection responses. This is the plan's "code-complete but
// unverified" standard made concrete: the request/response contract is
// exercised for real, but no live Shopify/BigCommerce/Magento store is
// ever touched - that final verification is deferred until real
// credentials exist.

func TestShopifyConnectorPublishProduct(t *testing.T) {
	var gotToken, gotQuery string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotToken = r.Header.Get("X-Shopify-Access-Token")
		var req shopifyGraphQLRequest
		_ = json.NewDecoder(r.Body).Decode(&req)
		gotQuery = req.Query
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"data": map[string]interface{}{
				"productSet": map[string]interface{}{
					"product":    map[string]string{"id": "gid://shopify/Product/12345"},
					"userErrors": []interface{}{},
				},
			},
		})
	}))
	defer server.Close()

	origURL := shopifyGraphQLURL
	shopifyGraphQLURL = func(shopDomain string) string { return server.URL }
	defer func() { shopifyGraphQLURL = origURL }()

	externalID, err := shopifyConnector{}.PublishProduct(context.Background(),
		map[string]string{"access_token": "shpat_test", "shop_domain": "test.myshopify.com"},
		ChannelProductPayload{ItemCode: "SKU-1", Title: "Test Product", Description: "Desc", Attributes: map[string]string{"vendor": "TestCo"}})
	if err != nil {
		t.Fatalf("PublishProduct failed: %v", err)
	}
	if externalID != "gid://shopify/Product/12345" {
		t.Fatalf("wrong external id: %q", externalID)
	}
	if gotToken != "shpat_test" {
		t.Fatalf("expected X-Shopify-Access-Token header, got %q", gotToken)
	}
	if !strings.Contains(gotQuery, "productSet") {
		t.Fatalf("expected a productSet mutation, got: %s", gotQuery)
	}
}

func TestShopifyConnectorSurfacesUserErrors(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"data": map[string]interface{}{
				"productSet": map[string]interface{}{
					"product": map[string]string{},
					"userErrors": []map[string]interface{}{
						{"field": []string{"title"}, "message": "Title can't be blank"},
					},
				},
			},
		})
	}))
	defer server.Close()

	origURL := shopifyGraphQLURL
	shopifyGraphQLURL = func(shopDomain string) string { return server.URL }
	defer func() { shopifyGraphQLURL = origURL }()

	_, err := shopifyConnector{}.PublishProduct(context.Background(),
		map[string]string{"access_token": "t", "shop_domain": "d"},
		ChannelProductPayload{ItemCode: "SKU-1"})
	if err == nil || !strings.Contains(err.Error(), "Title can't be blank") {
		t.Fatalf("expected the platform's userError message to surface, got: %v", err)
	}
}

func TestShopifyConnectorRequiresCredentials(t *testing.T) {
	_, err := shopifyConnector{}.PublishProduct(context.Background(), map[string]string{}, ChannelProductPayload{ItemCode: "X"})
	if err == nil || !strings.Contains(err.Error(), "credential missing") {
		t.Fatalf("expected a missing-credential error, got: %v", err)
	}
}

func TestBigCommerceConnectorPublishProduct(t *testing.T) {
	var gotToken, gotPath string
	var gotBody map[string]interface{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotToken = r.Header.Get("X-Auth-Token")
		gotPath = r.URL.Path
		_ = json.NewDecoder(r.Body).Decode(&gotBody)
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"data": map[string]interface{}{"id": 777},
		})
	}))
	defer server.Close()

	origURL := bigCommerceBaseURL
	bigCommerceBaseURL = func(storeHash string) string { return server.URL + "/stores/" + storeHash + "/v3" }
	defer func() { bigCommerceBaseURL = origURL }()

	externalID, err := bigCommerceConnector{}.PublishProduct(context.Background(),
		map[string]string{"access_token": "bc_test_token", "store_hash": "abc123"},
		ChannelProductPayload{ItemCode: "SKU-2", Title: "BC Product", Description: "BC Desc", Attributes: map[string]string{"material": "gold"}})
	if err != nil {
		t.Fatalf("PublishProduct failed: %v", err)
	}
	if externalID != "777" {
		t.Fatalf("wrong external id: %q", externalID)
	}
	if gotToken != "bc_test_token" {
		t.Fatalf("expected X-Auth-Token header, got %q", gotToken)
	}
	if !strings.HasSuffix(gotPath, "/catalog/products") {
		t.Fatalf("expected POST to /catalog/products, got %s", gotPath)
	}
	if gotBody["name"] != "BC Product" || gotBody["sku"] != "SKU-2" {
		t.Fatalf("payload shape wrong: %v", gotBody)
	}
}

func TestBigCommerceConnectorSurfacesRejection(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnprocessableEntity)
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"status": 422, "title": "The product name is a duplicate",
		})
	}))
	defer server.Close()

	origURL := bigCommerceBaseURL
	bigCommerceBaseURL = func(storeHash string) string { return server.URL }
	defer func() { bigCommerceBaseURL = origURL }()

	_, err := bigCommerceConnector{}.PublishProduct(context.Background(),
		map[string]string{"access_token": "t", "store_hash": "h"},
		ChannelProductPayload{ItemCode: "SKU-DUP"})
	if err == nil || !strings.Contains(err.Error(), "duplicate") {
		t.Fatalf("expected the platform's rejection message to surface, got: %v", err)
	}
}

func TestMagentoConnectorPublishProduct(t *testing.T) {
	var gotAuth, gotPath string
	var gotBody map[string]interface{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		gotPath = r.URL.Path
		_ = json.NewDecoder(r.Body).Decode(&gotBody)
		_ = json.NewEncoder(w).Encode(map[string]interface{}{"sku": "SKU-3"})
	}))
	defer server.Close()

	origURL := magentoBaseURL
	magentoBaseURL = func(cred map[string]string) string { return server.URL + "/rest/default/V1" }
	defer func() { magentoBaseURL = origURL }()

	externalID, err := magentoConnector{}.PublishProduct(context.Background(),
		map[string]string{"access_token": "mag_token", "base_url": "store.example.com", "auth_mode": "OpenSource"},
		ChannelProductPayload{ItemCode: "SKU-3", Title: "Mag Product", Description: "Mag Desc", Attributes: map[string]string{"color": "red"}})
	if err != nil {
		t.Fatalf("PublishProduct failed: %v", err)
	}
	if externalID != "SKU-3" {
		t.Fatalf("wrong external id: %q", externalID)
	}
	if gotAuth != "Bearer mag_token" {
		t.Fatalf("expected Bearer auth header, got %q", gotAuth)
	}
	if !strings.HasSuffix(gotPath, "/products") {
		t.Fatalf("expected POST to /products, got %s", gotPath)
	}
	product, _ := gotBody["product"].(map[string]interface{})
	if product == nil || product["sku"] != "SKU-3" || product["type_id"] != "simple" {
		t.Fatalf("payload shape wrong: %v", gotBody)
	}
}

func TestVerifyBigCommerceWebhook(t *testing.T) {
	secret := "webhook-secret-123"
	payload := []byte(`{"scope":"store/product/updated","data":{"id":99}}`)

	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(payload)
	validSig := hex.EncodeToString(mac.Sum(nil))

	if !VerifyBigCommerceWebhook(payload, validSig, secret) {
		t.Fatalf("valid signature was rejected")
	}
	if VerifyBigCommerceWebhook(payload, validSig, "wrong-secret") {
		t.Fatalf("signature verified against the wrong secret")
	}
	tampered := append([]byte{}, payload...)
	tampered[0] ^= 0xFF
	if VerifyBigCommerceWebhook(tampered, validSig, secret) {
		t.Fatalf("tampered payload passed verification")
	}
}
