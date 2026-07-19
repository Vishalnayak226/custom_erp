package engines

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strings"
	"time"

	"custom_erp/db"
)

// Magento Open Source / Adobe Commerce connector (Stage 16.4). One
// implementation covering both editions, since they share the same core
// REST /V1/products shape (Adobe Commerce is Magento under the hood) -
// they differ only in how the bearer token was obtained and whether native
// webhooks exist. Credential fields: "base_url" (e.g. "mystore.example.com",
// no scheme/path), "access_token" (a Magento Integration access token for
// Open Source, or an already-issued Adobe IMS access token for Adobe
// Commerce Cloud - this connector does not implement the IMS OAuth
// token-acquisition/refresh flow itself, stated as a limitation: the
// credential is expected to already be a valid bearer token, refreshed
// externally for now), "auth_mode" ("OpenSource" or "AdobeCommerce" - only
// affects whether the polling worker treats this channel as
// webhook-less), "store_view_code" (optional, defaults to "default").
//
// Scope simplification, stated explicitly, same as Shopify/BigCommerce:
// each ERP Item publishes as its own standalone Magento simple product -
// no ERP-parent-to-Magento-configurable-product grouping in this pass.

type magentoConnector struct{}

func init() {
	registerConnector("Magento", magentoConnector{})
	registerConnector("AdobeCommerce", magentoConnector{})
}

func (magentoConnector) RateLimit() (int, time.Duration) {
	// Magento/Adobe Commerce REST does not document a fixed cost-based
	// limit the way Shopify/BigCommerce do - this is a conservative,
	// generic floor rather than a platform-documented number.
	return 30, time.Minute
}

// magentoBaseURL is a var (not a func) so tests can point the
// connector at an httptest.Server instead of the real platform.
var magentoBaseURL = func(cred map[string]string) string {
	storeView := cred["store_view_code"]
	if storeView == "" {
		storeView = "default"
	}
	return fmt.Sprintf("https://%s/rest/%s/V1", cred["base_url"], storeView)
}

type magentoErrorResponse struct {
	Message string `json:"message"`
}

func (magentoConnector) PublishProduct(ctx context.Context, cred map[string]string, payload ChannelProductPayload) (string, error) {
	baseURL := cred["base_url"]
	accessToken := cred["access_token"]
	if baseURL == "" || accessToken == "" {
		return "", fmt.Errorf("magento credential missing base_url/access_token, configure it via POST /api/v1/pim/channels/{code}/credentials")
	}

	customAttributes := []map[string]interface{}{
		{"attribute_code": "description", "value": payload.Description},
	}
	for target, value := range payload.Attributes {
		customAttributes = append(customAttributes, map[string]interface{}{
			"attribute_code": target,
			"value":          value,
		})
	}

	body := map[string]interface{}{
		"product": map[string]interface{}{
			"sku":               payload.ItemCode,
			"name":              payload.Title,
			"price":             0,
			"status":            1,
			"visibility":        4,
			"type_id":           "simple",
			"attribute_set_id":  4, // default "Default" attribute set - a real setup may need this configurable per family
			"custom_attributes": customAttributes,
		},
	}
	reqBody, err := json.Marshal(body)
	if err != nil {
		return "", err
	}

	headers := map[string]string{
		"Content-Type":  "application/json",
		"Authorization": "Bearer " + accessToken,
	}
	url := magentoBaseURL(cred) + "/products"
	status, respBody, err := doConnectorRequest(ctx, 20*time.Second, http.MethodPost, url, headers, reqBody)
	if err != nil {
		return "", fmt.Errorf("magento request failed: %v", err)
	}
	if status < 200 || status >= 300 {
		return "", fmt.Errorf("magento rejected the product (HTTP %d): %s", status, magentoErrorMessage(respBody))
	}

	var result struct {
		SKU string `json:"sku"`
	}
	if err := json.Unmarshal(respBody, &result); err != nil {
		return "", fmt.Errorf("failed to parse magento response: %v", err)
	}
	if result.SKU == "" {
		return "", fmt.Errorf("magento did not return a product sku")
	}

	if len(payload.Images) > 0 {
		if err := uploadMagentoMedia(ctx, cred, result.SKU, payload.Images); err != nil {
			log.Printf("[MAGENTO] product %s created but media upload failed: %v", result.SKU, err)
		}
	}

	return result.SKU, nil
}

func magentoErrorMessage(respBody []byte) string {
	var errResp magentoErrorResponse
	if err := json.Unmarshal(respBody, &errResp); err == nil && errResp.Message != "" {
		return errResp.Message
	}
	return string(respBody)
}

// uploadMagentoMedia attaches images via base64-encoded content inline in
// the JSON body - Magento's media endpoint accepts this directly, unlike
// Shopify's separate staged-upload flow or BigCommerce's multipart POST.
func uploadMagentoMedia(ctx context.Context, cred map[string]string, sku string, images []ChannelImage) error {
	headers := map[string]string{
		"Content-Type":  "application/json",
		"Authorization": "Bearer " + cred["access_token"],
	}
	url := fmt.Sprintf("%s/products/%s/media", magentoBaseURL(cred), sku)

	var lastErr error
	for i, img := range images {
		entry := map[string]interface{}{
			"entry": map[string]interface{}{
				"media_type": "image",
				"label":      img.Filename,
				"position":   i,
				"disabled":   false,
				"types":      []string{},
				"content": map[string]interface{}{
					"base64_encoded_data": base64.StdEncoding.EncodeToString(img.Bytes),
					"type":                img.MIMEType,
					"name":                img.Filename,
				},
			},
		}
		reqBody, err := json.Marshal(entry)
		if err != nil {
			lastErr = err
			continue
		}
		status, respBody, err := doConnectorRequest(ctx, 30*time.Second, http.MethodPost, url, headers, reqBody)
		if err != nil {
			lastErr = err
			continue
		}
		if status < 200 || status >= 300 {
			lastErr = fmt.Errorf("magento media upload returned HTTP %d: %s", status, magentoErrorMessage(respBody))
		}
	}
	return lastErr
}

// StartMagentoPollWorker polls every Channel configured with
// platform=Magento and auth_mode=OpenSource (Adobe Commerce Cloud gets
// real webhooks instead, see Part A.7's note) for orders modified since
// the last poll - the substitute for native webhooks Magento Open Source
// does not have. Mirrors the same ticker shape as
// engines.StartOutboxWorker/StartPublishQueueWorker. Scope note, stated
// explicitly, same as the BigCommerce webhook handler: this detects and
// logs changed orders; it does not drive a full order-import pipeline the
// way the existing Shopify order webhook does - that is deferred, not
// silently skipped.
func StartMagentoPollWorker(interval time.Duration) {
	ticker := time.NewTicker(interval)
	lastPoll := time.Now().Add(-interval)
	go func() {
		for range ticker.C {
			if db.DB == nil {
				continue
			}
			schemas, err := listTenantSchemas()
			if err != nil {
				log.Printf("[MAGENTO-POLL] failed to list tenant schemas: %v", err)
				continue
			}
			since := lastPoll
			lastPoll = time.Now()
			for _, schema := range schemas {
				pollMagentoChannels(schema, since)
			}
		}
	}()
}

func pollMagentoChannels(schema string, since time.Time) {
	rows, err := db.DB.Query(fmt.Sprintf(
		"SELECT id FROM %s.documents WHERE doctype = 'Channel' AND data->>'platform' IN ('Magento','AdobeCommerce')", schema))
	if err != nil {
		return
	}
	var channelCodes []string
	for rows.Next() {
		var code string
		if err := rows.Scan(&code); err == nil {
			channelCodes = append(channelCodes, code)
		}
	}
	rows.Close()

	tenantID, err := tenantIDForSchema(schema)
	if err != nil {
		return
	}

	for _, channelCode := range channelCodes {
		cred, err := getChannelCredential(tenantID, channelCode)
		if err != nil || cred["auth_mode"] != "OpenSource" {
			continue // AdobeCommerce channels use real webhooks, not polling
		}
		checkMagentoOrdersSince(cred, channelCode, since)
	}
}

func checkMagentoOrdersSince(cred map[string]string, channelCode string, since time.Time) {
	if cred["base_url"] == "" || cred["access_token"] == "" {
		return
	}
	filter := fmt.Sprintf("searchCriteria[filter_groups][0][filters][0][field]=updated_at&searchCriteria[filter_groups][0][filters][0][value]=%s&searchCriteria[filter_groups][0][filters][0][condition_type]=gt",
		since.UTC().Format("2006-01-02 15:04:05"))
	url := magentoBaseURL(cred) + "/orders?" + filter
	headers := map[string]string{"Authorization": "Bearer " + cred["access_token"]}

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	status, respBody, err := doConnectorRequest(ctx, 15*time.Second, http.MethodGet, url, headers, nil)
	if err != nil {
		log.Printf("[MAGENTO-POLL] channel %s: request failed: %v", channelCode, err)
		return
	}
	if status < 200 || status >= 300 {
		log.Printf("[MAGENTO-POLL] channel %s: HTTP %d: %s", channelCode, status, strings.TrimSpace(string(respBody)))
		return
	}

	var result struct {
		Items []struct {
			IncrementID string `json:"increment_id"`
		} `json:"items"`
	}
	if err := json.Unmarshal(respBody, &result); err != nil {
		return
	}
	if len(result.Items) > 0 {
		log.Printf("[MAGENTO-POLL] channel %s: %d order(s) changed since last poll", channelCode, len(result.Items))
	}
}
