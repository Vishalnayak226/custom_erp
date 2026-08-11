package engines

import (
	"bytes"
	"encoding/json"
	"fmt"
	"log"
	"mime/multipart"
	"net/http"
	"net/textproto"
	"strings"
	"time"

	"context"
)

// BigCommerce connector (Stage 16.3). Uses the REST v3 Catalog API
// (simplest of the three REST models researched). Auth is a private "API
// account" token (per the user decision: no OAuth app-install flow) -
// expected credential fields: "access_token", "store_hash" (from the store
// URL, e.g. "abc123"). Real native webhooks exist on BigCommerce for
// product/inventory events; subscribing to them is a one-time admin action
// against BigCommerce's own webhook API, documented here rather than
// automated, since it only needs to happen once per store, not per
// publish.
//
// Scope simplification, stated explicitly, same as the Shopify connector:
// each ERP Item publishes as its own standalone BigCommerce product - no
// ERP-parent-to-BigCommerce-variant grouping in this pass.

type bigCommerceConnector struct{}

func init() {
	registerConnector("BigCommerce", bigCommerceConnector{})
}

func (bigCommerceConnector) RateLimit() (int, time.Duration) {
	// BigCommerce documents 150-450 requests/30s depending on plan tier -
	// 100/30s is a safe floor under even the lowest tier.
	return 100, 30 * time.Second
}

type bigCommerceErrorResponse struct {
	Status int                    `json:"status"`
	Title  string                 `json:"title"`
	Errors map[string]interface{} `json:"errors"`
}

// bigCommerceBaseURL is a var (not a func) so tests can point the
// connector at an httptest.Server instead of the real platform.
var bigCommerceBaseURL = func(storeHash string) string {
	return fmt.Sprintf("https://api.bigcommerce.com/stores/%s/v3", storeHash)
}

func bigCommerceHeaders(accessToken string) map[string]string {
	return map[string]string{
		"Content-Type": "application/json",
		"Accept":       "application/json",
		"X-Auth-Token": accessToken,
	}
}

func (bigCommerceConnector) PublishProduct(ctx context.Context, cred map[string]string, payload ChannelProductPayload) (string, error) {
	accessToken := cred["access_token"]
	storeHash := cred["store_hash"]
	if accessToken == "" || storeHash == "" {
		// CONN-0224 (Stage 25.6): "Live connector credentials missing."
		return "", &ValidationError{Code: "CONN-0224", Message: "bigcommerce credential missing access_token/store_hash, configure it via POST /api/v1/pim/channels/{code}/credentials"}
	}

	customFields := []map[string]string{}
	for target, value := range payload.Attributes {
		customFields = append(customFields, map[string]string{"name": target, "value": value})
	}

	body := map[string]interface{}{
		"name":        payload.Title,
		"type":        "physical",
		"weight":      0,
		"price":       0,
		"description": payload.Description,
		"sku":         payload.ItemCode,
	}
	if len(customFields) > 0 {
		body["custom_fields"] = customFields
	}

	reqBody, err := json.Marshal(body)
	if err != nil {
		return "", err
	}
	url := bigCommerceBaseURL(storeHash) + "/catalog/products"
	status, respBody, err := doConnectorRequest(ctx, 20*time.Second, http.MethodPost, url, bigCommerceHeaders(accessToken), reqBody, "bigcommerce")
	if err != nil {
		// CONN-0226 below, except a circuit-breaker-open error (CONN-0225) -
		// preserved as-is rather than flattened into a new plain error.
		if verr, ok := err.(*ValidationError); ok {
			return "", verr
		}
		return "", &ValidationError{Code: "CONN-0226", Message: fmt.Sprintf("bigcommerce request failed: %v", err)}
	}
	if status < 200 || status >= 300 {
		// 26.4.8: classified against the platform's own error title, falling
		// back to CONN-0226 ("Channel publish failed") same as before this
		// dictionary existed.
		platformMsg := bigCommerceErrorMessage(respBody)
		return "", &ValidationError{Code: classifyConnectorError(platformMsg), Message: fmt.Sprintf("bigcommerce rejected the product (HTTP %d): %s", status, platformMsg)}
	}

	var result struct {
		Data struct {
			ID int `json:"id"`
		} `json:"data"`
	}
	if err := json.Unmarshal(respBody, &result); err != nil {
		return "", fmt.Errorf("failed to parse bigcommerce response: %v", err)
	}
	if result.Data.ID == 0 {
		return "", fmt.Errorf("bigcommerce did not return a product id")
	}
	externalID := fmt.Sprintf("%d", result.Data.ID)

	if len(payload.Images) > 0 {
		if err := uploadBigCommerceMedia(ctx, storeHash, accessToken, result.Data.ID, payload.Images); err != nil {
			// Same non-fatal treatment as the Shopify connector - the
			// product itself was created successfully.
			log.Printf("[BIGCOMMERCE] product %s created but media upload failed: %v", externalID, err)
		}
	}

	return externalID, nil
}

func bigCommerceErrorMessage(respBody []byte) string {
	var errResp bigCommerceErrorResponse
	if err := json.Unmarshal(respBody, &errResp); err == nil && errResp.Title != "" {
		return errResp.Title
	}
	return string(respBody)
}

// uploadBigCommerceMedia uploads each image directly as multipart binary to
// BigCommerce's product image endpoint - unlike Shopify, BigCommerce
// accepts a direct file upload in one step, no staged-upload dance needed.
func uploadBigCommerceMedia(ctx context.Context, storeHash, accessToken string, productID int, images []ChannelImage) error {
	url := fmt.Sprintf("%s/catalog/products/%d/images", bigCommerceBaseURL(storeHash), productID)
	var lastErr error
	for _, img := range images {
		var body bytes.Buffer
		writer := multipart.NewWriter(&body)
		header := textproto.MIMEHeader{}
		header.Set("Content-Disposition", fmt.Sprintf("form-data; name=\"image_file\"; filename=\"%s\"", img.Filename))
		header.Set("Content-Type", img.MIMEType)
		part, err := writer.CreatePart(header)
		if err != nil {
			lastErr = err
			continue
		}
		if _, err := part.Write(img.Bytes); err != nil {
			lastErr = err
			continue
		}
		if err := writer.Close(); err != nil {
			lastErr = err
			continue
		}

		headers := map[string]string{
			"Accept":       "application/json",
			"X-Auth-Token": accessToken,
			"Content-Type": writer.FormDataContentType(),
		}
		status, respBody, err := doConnectorRequest(ctx, 30*time.Second, http.MethodPost, url, headers, body.Bytes(), "bigcommerce")
		if err != nil {
			lastErr = err
			continue
		}
		if status < 200 || status >= 300 {
			lastErr = fmt.Errorf("bigcommerce image upload returned HTTP %d: %s", status, bigCommerceErrorMessage(respBody))
			continue
		}
	}
	return lastErr
}

// VerifyBigCommerceWebhook checks an inbound BigCommerce webhook using the
// generic engines.VerifyWebhookHMAC helper (BigCommerce signs with hex
// HMAC-SHA256, unlike Shopify's base64 convention) - see
// engines/webhook_verify.go. secret is the per-channel webhook secret
// configured when the store's webhook subscription was created (one-time
// setup against BigCommerce's own /v3/hooks endpoint, done directly in the
// store admin/API - not automated by this connector, since it only needs
// to happen once).
func VerifyBigCommerceWebhook(payload []byte, signatureHeader, secret string) bool {
	return VerifyWebhookHMAC(payload, strings.TrimSpace(signatureHeader), secret, "hex")
}

// bigCommerceV2BaseURL is the legacy-but-current REST v2 host, which is where
// BigCommerce keeps orders (v3 covers the catalogue only). A var, like its v3
// sibling, so tests can point it at an httptest.Server.
var bigCommerceV2BaseURL = func(storeHash string) string {
	return fmt.Sprintf("https://api.bigcommerce.com/stores/%s/v2", storeHash)
}

// bigCommercePaidPaymentStatuses are the order payment_status values that mean
// money has actually been taken. Anything else - "pending", "authorized",
// "void", the refund states - imports as Pending, which routes the order to a
// PAYMENT_UNCONFIRMED hold via validateOrderChain rather than being rejected.
var bigCommercePaidPaymentStatuses = map[string]bool{
	"captured":           true,
	"paid":               true,
	"partially refunded": true,
}

// ImportBigCommerceOrder fetches one order from BigCommerce and imports it
// through the shared SalesOrder intake (Stage 35.1.2).
//
// This exists because a BigCommerce webhook body carries only a scope and an
// order id - unlike Shopify's, it does not contain the order itself - so the
// order has to be read back over the API before it can be imported. That read
// needs the store's outbound access token, and getChannelCredential is
// deliberately package-private so a decrypted token never leaves engines/.
// Hence a function here that takes an id and returns an order id, rather than
// a credential accessor the HTTP layer could call.
//
// Credential-gated in exactly the 26.2.x sense: code-complete, and inert until
// a channel has "store_hash"/"access_token" configured.
func ImportBigCommerceOrder(tenantID, channelCode string, bcOrderID int64) (string, error) {
	cred, err := getChannelCredential(tenantID, channelCode)
	if err != nil {
		return "", err
	}
	if cred["store_hash"] == "" || cred["access_token"] == "" {
		return "", fmt.Errorf("channel %q has no store_hash/access_token configured, cannot fetch order %d", channelCode, bcOrderID)
	}
	base := bigCommerceV2BaseURL(cred["store_hash"])
	headers := bigCommerceHeaders(cred["access_token"])

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	get := func(path string, out interface{}) error {
		status, body, err := doConnectorRequest(ctx, 15*time.Second, http.MethodGet, base+path, headers, nil, "bigcommerce")
		if err != nil {
			return err
		}
		// 204 is BigCommerce's "this sub-resource is empty" answer (an order
		// with no shipping address, e.g. a digital-only order) and carries no
		// body, so it must not be fed to the JSON decoder.
		if status == http.StatusNoContent {
			return nil
		}
		if status < 200 || status >= 300 {
			return fmt.Errorf("bigcommerce GET %s: HTTP %d: %s", path, status, bigCommerceErrorMessage(body))
		}
		return json.Unmarshal(body, out)
	}

	var order struct {
		ID             int64  `json:"id"`
		PaymentStatus  string `json:"payment_status"`
		BillingAddress struct {
			FirstName string `json:"first_name"`
			LastName  string `json:"last_name"`
			Phone     string `json:"phone"`
		} `json:"billing_address"`
	}
	if err := get(fmt.Sprintf("/orders/%d", bcOrderID), &order); err != nil {
		return "", err
	}

	var products []struct {
		SKU      string `json:"sku"`
		Name     string `json:"name"`
		Quantity int    `json:"quantity"`
		// BigCommerce serialises money as a decimal string ("12.3400").
		BasePrice string `json:"base_price"`
	}
	if err := get(fmt.Sprintf("/orders/%d/products", bcOrderID), &products); err != nil {
		return "", err
	}
	if len(products) == 0 {
		return "", fmt.Errorf("bigcommerce order %d has no line items", bcOrderID)
	}

	var shipping []struct {
		Street1 string `json:"street_1"`
		Street2 string `json:"street_2"`
		City    string `json:"city"`
		Zip     string `json:"zip"`
		Phone   string `json:"phone"`
	}
	// A missing shipping address is not fatal: an empty address routes the
	// order to an ADDR_INVALID hold, which is a queue a human can clear -
	// strictly better than dropping the order, which is what this handler did
	// before 35.1.2.
	_ = get(fmt.Sprintf("/orders/%d/shipping_addresses", bcOrderID), &shipping)

	lines := make([]SalesOrderLineInput, 0, len(products))
	for _, p := range products {
		sku := strings.TrimSpace(p.SKU)
		if sku == "" {
			// BigCommerce allows SKU-less products. Fall back to the product
			// name so the line still reaches the SKU_MAPPING_FAILED hold queue
			// with something a human can identify, instead of vanishing.
			sku = strings.TrimSpace(p.Name)
		}
		lines = append(lines, SalesOrderLineInput{
			SKU:       sku,
			Qty:       p.Quantity,
			UnitPrice: numericFromAny(p.BasePrice),
		})
	}

	var addressParts []string
	phone := strings.TrimSpace(order.BillingAddress.Phone)
	if len(shipping) > 0 {
		addressParts = []string{shipping[0].Street1, shipping[0].Street2, shipping[0].City, shipping[0].Zip}
		if phone == "" {
			phone = strings.TrimSpace(shipping[0].Phone)
		}
	}
	paymentStatus := "Pending"
	if bigCommercePaidPaymentStatuses[strings.ToLower(strings.TrimSpace(order.PaymentStatus))] {
		paymentStatus = "Confirmed"
	}

	return ImportChannelSalesOrder(tenantID, ChannelOrderInput{
		Channel:         channelCode,
		ChannelOrderID:  fmt.Sprint(bcOrderID),
		CustomerName:    strings.TrimSpace(order.BillingAddress.FirstName + " " + order.BillingAddress.LastName),
		ShippingAddress: strings.TrimSpace(strings.Join(addressParts, " ")),
		PaymentStatus:   paymentStatus,
		CustomerPhone:   phone,
		Lines:           lines,
	})
}
