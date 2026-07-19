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
		return "", fmt.Errorf("bigcommerce credential missing access_token/store_hash, configure it via POST /api/v1/pim/channels/{code}/credentials")
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
	status, respBody, err := doConnectorRequest(ctx, 20*time.Second, http.MethodPost, url, bigCommerceHeaders(accessToken), reqBody)
	if err != nil {
		return "", fmt.Errorf("bigcommerce request failed: %v", err)
	}
	if status < 200 || status >= 300 {
		return "", fmt.Errorf("bigcommerce rejected the product (HTTP %d): %s", status, bigCommerceErrorMessage(respBody))
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
		status, respBody, err := doConnectorRequest(ctx, 30*time.Second, http.MethodPost, url, headers, body.Bytes())
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
