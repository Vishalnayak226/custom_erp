package engines

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"mime/multipart"
	"net/http"
	"net/textproto"
	"strings"
	"time"
)

// Shopify connector (Stage 16.2). Uses the Admin GraphQL API (REST product
// endpoints are legacy per current Shopify guidance) - productSet for
// create/update, a real 3-step staged-upload flow for media (Shopify does
// not accept inline binary in GraphQL). Auth is a private/custom-app Admin
// API access token (per the user decision: no OAuth authorize-redirect
// flow, since this ERP runs its own store rather than being distributed to
// other merchants) - expected credential fields: "access_token",
// "shop_domain" (e.g. "mystore.myshopify.com").
//
// Scope simplification, stated explicitly: each ERP Item (parent or
// variant) publishes as its own standalone Shopify product for this pass -
// grouping ERP parent+variant Items into one Shopify product with real
// Shopify-side variants would need a richer payload shape than
// ChannelProductPayload currently carries, and is deferred rather than
// guessed at.
//
// Rate limiting: RateLimit() returns a conservative static floor (well
// under the roughly 1000 cost-points/60s budget Shopify documents,
// assuming a generous per-mutation cost estimate). The actual GraphQL
// response extensions.cost.throttleStatus is parsed and logged on every
// call for visibility - a genuinely adaptive bucket driven by that live
// data is a documented future enhancement, not implemented here, since it
// cannot be meaningfully tuned without traffic against a real store.

const shopifyAPIVersion = "2025-01"

// shopifyGraphQLURL is a var (not inline) so tests can point the
// connector at an httptest.Server instead of the real platform.
var shopifyGraphQLURL = func(shopDomain string) string {
	return fmt.Sprintf("https://%s/admin/api/%s/graphql.json", shopDomain, shopifyAPIVersion)
}

type shopifyConnector struct{}

func init() {
	registerConnector("Shopify", shopifyConnector{})
}

func (shopifyConnector) RateLimit() (int, time.Duration) {
	return 20, time.Minute // conservative floor; see file header note
}

type shopifyGraphQLRequest struct {
	Query     string                 `json:"query"`
	Variables map[string]interface{} `json:"variables,omitempty"`
}

type shopifyThrottleStatus struct {
	MaximumAvailable   float64 `json:"maximumAvailable"`
	CurrentlyAvailable float64 `json:"currentlyAvailable"`
	RestoreRate        float64 `json:"restoreRate"`
}

type shopifyGraphQLResponse struct {
	Data   json.RawMessage `json:"data"`
	Errors []struct {
		Message string `json:"message"`
	} `json:"errors"`
	Extensions struct {
		Cost struct {
			RequestedQueryCost int                   `json:"requestedQueryCost"`
			ActualQueryCost    int                   `json:"actualQueryCost"`
			ThrottleStatus     shopifyThrottleStatus `json:"throttleStatus"`
		} `json:"cost"`
	} `json:"extensions"`
}

// callShopifyGraphQL POSTs one GraphQL request via the shared safe-HTTP
// helper (doConnectorRequest, engines/connector_http.go) and returns the
// parsed envelope. Logs the throttle status every call - see the rate
// limiting note above.
func callShopifyGraphQL(ctx context.Context, shopDomain, accessToken, query string, variables map[string]interface{}) (*shopifyGraphQLResponse, error) {
	reqBody, err := json.Marshal(shopifyGraphQLRequest{Query: query, Variables: variables})
	if err != nil {
		return nil, err
	}
	url := shopifyGraphQLURL(shopDomain)
	headers := map[string]string{
		"Content-Type":           "application/json",
		"X-Shopify-Access-Token": accessToken,
	}
	status, respBody, err := doConnectorRequest(ctx, 20*time.Second, http.MethodPost, url, headers, reqBody)
	if err != nil {
		return nil, fmt.Errorf("shopify request failed: %v", err)
	}
	if status < 200 || status >= 300 {
		return nil, fmt.Errorf("shopify returned HTTP %d: %s", status, string(respBody))
	}

	var parsed shopifyGraphQLResponse
	if err := json.Unmarshal(respBody, &parsed); err != nil {
		return nil, fmt.Errorf("failed to parse shopify response: %v", err)
	}
	if len(parsed.Errors) > 0 {
		return nil, fmt.Errorf("shopify GraphQL error: %s", parsed.Errors[0].Message)
	}
	ts := parsed.Extensions.Cost.ThrottleStatus
	log.Printf("[SHOPIFY] query cost %d, throttle available %.0f/%.0f", parsed.Extensions.Cost.ActualQueryCost, ts.CurrentlyAvailable, ts.MaximumAvailable)
	return &parsed, nil
}

var shopifyProductSetMutation = strings.Join([]string{
	"mutation productSet($input: ProductSetInput!) {",
	"  productSet(input: $input, synchronous: true) {",
	"    product { id }",
	"    userErrors { field message }",
	"  }",
	"}",
}, "\n")

func (shopifyConnector) PublishProduct(ctx context.Context, cred map[string]string, payload ChannelProductPayload) (string, error) {
	shopDomain := cred["shop_domain"]
	accessToken := cred["access_token"]
	if shopDomain == "" || accessToken == "" {
		return "", fmt.Errorf("shopify credential missing shop_domain/access_token, configure it via POST /api/v1/pim/channels/{code}/credentials")
	}

	metafields := []map[string]interface{}{}
	vendor, productType, tags := "", "", ""
	for target, value := range payload.Attributes {
		switch strings.ToLower(target) {
		case "vendor":
			vendor = value
		case "product_type", "producttype":
			productType = value
		case "tags":
			tags = value
		default:
			metafields = append(metafields, map[string]interface{}{
				"namespace": "custom",
				"key":       target,
				"value":     value,
				"type":      "single_line_text_field",
			})
		}
	}

	input := map[string]interface{}{
		"title":           payload.Title,
		"descriptionHtml": payload.Description,
	}
	if vendor != "" {
		input["vendor"] = vendor
	}
	if productType != "" {
		input["productType"] = productType
	}
	if tags != "" {
		input["tags"] = strings.Split(tags, ",")
	}
	if len(metafields) > 0 {
		input["metafields"] = metafields
	}

	resp, err := callShopifyGraphQL(ctx, shopDomain, accessToken, shopifyProductSetMutation, map[string]interface{}{"input": input})
	if err != nil {
		return "", err
	}

	var result struct {
		ProductSet struct {
			Product struct {
				ID string `json:"id"`
			} `json:"product"`
			UserErrors []struct {
				Field   []string `json:"field"`
				Message string   `json:"message"`
			} `json:"userErrors"`
		} `json:"productSet"`
	}
	if err := json.Unmarshal(resp.Data, &result); err != nil {
		return "", fmt.Errorf("failed to parse productSet response: %v", err)
	}
	if len(result.ProductSet.UserErrors) > 0 {
		return "", fmt.Errorf("shopify rejected the product: %s", result.ProductSet.UserErrors[0].Message)
	}
	externalID := result.ProductSet.Product.ID
	if externalID == "" {
		return "", fmt.Errorf("shopify did not return a product id")
	}

	if len(payload.Images) > 0 {
		if err := uploadShopifyMedia(ctx, shopDomain, accessToken, externalID, payload.Images); err != nil {
			// Media failure does not fail the whole publish - the product
			// itself was created successfully, which is the primary
			// outcome; log so it is visible rather than silently dropped.
			log.Printf("[SHOPIFY] product %s created but media upload failed: %v", externalID, err)
		}
	}

	return externalID, nil
}

var shopifyStagedUploadsCreateMutation = strings.Join([]string{
	"mutation stagedUploadsCreate($input: [StagedUploadInput!]!) {",
	"  stagedUploadsCreate(input: $input) {",
	"    stagedTargets { url resourceUrl parameters { name value } }",
	"    userErrors { field message }",
	"  }",
	"}",
}, "\n")

var shopifyProductCreateMediaMutation = strings.Join([]string{
	"mutation productCreateMedia($productId: ID!, $media: [CreateMediaInput!]!) {",
	"  productCreateMedia(productId: $productId, media: $media) {",
	"    media { alt }",
	"    mediaUserErrors { field message }",
	"  }",
	"}",
}, "\n")

type shopifyStagedTargetParam struct {
	Name  string
	Value string
}

// uploadShopifyMedia implements the real Shopify 3-step upload flow (it
// does not accept inline binary data in a GraphQL request): (1) ask
// Shopify for a pre-signed upload target per file, (2) PUT the raw bytes
// to that target, (3) attach the now-staged resource to the product.
func uploadShopifyMedia(ctx context.Context, shopDomain, accessToken, productID string, images []ChannelImage) error {
	stagedInputs := make([]map[string]interface{}, len(images))
	for i, img := range images {
		stagedInputs[i] = map[string]interface{}{
			"filename":   img.Filename,
			"mimeType":   img.MIMEType,
			"httpMethod": "POST",
			"resource":   "IMAGE",
			"fileSize":   fmt.Sprintf("%d", len(img.Bytes)),
		}
	}
	resp, err := callShopifyGraphQL(ctx, shopDomain, accessToken, shopifyStagedUploadsCreateMutation, map[string]interface{}{"input": stagedInputs})
	if err != nil {
		return fmt.Errorf("stagedUploadsCreate failed: %v", err)
	}
	var staged struct {
		StagedUploadsCreate struct {
			StagedTargets []struct {
				URL         string                     `json:"url"`
				ResourceURL string                     `json:"resourceUrl"`
				Parameters  []shopifyStagedTargetParam `json:"parameters"`
			} `json:"stagedTargets"`
			UserErrors []struct{ Message string } `json:"userErrors"`
		} `json:"stagedUploadsCreate"`
	}
	if err := json.Unmarshal(resp.Data, &staged); err != nil {
		return fmt.Errorf("failed to parse stagedUploadsCreate response: %v", err)
	}
	if len(staged.StagedUploadsCreate.UserErrors) > 0 {
		return fmt.Errorf("stagedUploadsCreate rejected: %s", staged.StagedUploadsCreate.UserErrors[0].Message)
	}
	targets := staged.StagedUploadsCreate.StagedTargets
	if len(targets) != len(images) {
		return fmt.Errorf("expected %d staged targets, got %d", len(images), len(targets))
	}

	mediaInputs := make([]map[string]interface{}, 0, len(images))
	for i, img := range images {
		target := targets[i]
		if err := putStagedUpload(ctx, target.URL, target.Parameters, img); err != nil {
			log.Printf("[SHOPIFY] staged upload failed for %s: %v", img.Filename, err)
			continue
		}
		mediaInputs = append(mediaInputs, map[string]interface{}{
			"originalSource":   target.ResourceURL,
			"mediaContentType": "IMAGE",
		})
	}
	if len(mediaInputs) == 0 {
		return fmt.Errorf("no images successfully staged")
	}

	_, err = callShopifyGraphQL(ctx, shopDomain, accessToken, shopifyProductCreateMediaMutation, map[string]interface{}{
		"productId": productID,
		"media":     mediaInputs,
	})
	return err
}

// putStagedUpload performs the multipart POST the Shopify staged-upload
// target expects: the exact parameters Shopify returned, in order, plus
// the file bytes as the final field.
func putStagedUpload(ctx context.Context, uploadURL string, parameters []shopifyStagedTargetParam, img ChannelImage) error {
	var body strings.Builder
	writer := multipart.NewWriter(&body)
	for _, p := range parameters {
		if err := writer.WriteField(p.Name, p.Value); err != nil {
			return err
		}
	}
	header := textproto.MIMEHeader{}
	header.Set("Content-Disposition", fmt.Sprintf("form-data; name=\"file\"; filename=\"%s\"", img.Filename))
	header.Set("Content-Type", img.MIMEType)
	part, err := writer.CreatePart(header)
	if err != nil {
		return err
	}
	if _, err := part.Write(img.Bytes); err != nil {
		return err
	}
	if err := writer.Close(); err != nil {
		return err
	}

	status, respBody, err := doConnectorRequest(ctx, 30*time.Second, http.MethodPost, uploadURL, map[string]string{"Content-Type": writer.FormDataContentType()}, []byte(body.String()))
	if err != nil {
		return err
	}
	if status < 200 || status >= 300 {
		return fmt.Errorf("staged upload returned HTTP %d: %s", status, string(respBody))
	}
	return nil
}
