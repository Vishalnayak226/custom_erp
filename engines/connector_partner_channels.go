package engines

import (
	"context"
	"encoding/base64"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// partnerRESTConnector covers partner-only marketplace/quick-commerce APIs
// whose contracts are not public. The credential names the four negotiated
// HTTPS paths; payloads use the SDK's normalized JSON schema. This keeps each
// private onboarding a configuration exercise while still failing closed if
// a required contract path was never supplied.
type partnerRESTConnector struct{ descriptor ConnectorDescriptor }

func init() {
	for _, code := range []string{"Myntra", "Meesho", "Ajio", "Nykaa"} {
		registerOmnichannelConnector(partnerRESTConnector{ConnectorDescriptor{Code: code, Kind: "marketplace", PullOrders: true, PushInventory: true, PublishCatalogue: true, PushStatus: true, PrivateContract: true}})
	}
	for _, code := range []string{"Blinkit", "Zepto", "SwiggyInstamart"} {
		registerOmnichannelConnector(partnerRESTConnector{ConnectorDescriptor{Code: code, Kind: "quick_commerce", PullOrders: true, PushInventory: true, PublishCatalogue: true, PushStatus: true, RequiresLocation: true, NoSplit: true, PrivateContract: true}})
	}
	registerOmnichannelConnector(wooCommerceConnector{})
}

func (c partnerRESTConnector) Descriptor() ConnectorDescriptor { return c.descriptor }
func (c partnerRESTConnector) RateLimit() (int, time.Duration) { return 30, time.Minute }
func (c partnerRESTConnector) MapError(err error) string       { return DefaultConnectorErrorCode(err) }

func (c partnerRESTConnector) ValidateCredentials(cred map[string]string) error {
	if cred["base_url"] == "" || cred["access_token"] == "" {
		return fmt.Errorf("%s credential requires base_url and access_token", c.descriptor.Code)
	}
	for _, field := range []string{"orders_path", "inventory_path", "catalogue_path", "status_path"} {
		if cred[field] == "" {
			return fmt.Errorf("%s private contract requires %s", c.descriptor.Code, field)
		}
	}
	return nil
}

func partnerHeaders(cred map[string]string) map[string]string {
	return map[string]string{"Authorization": "Bearer " + cred["access_token"], "Accept": "application/json"}
}
func partnerEndpoint(cred map[string]string, key string) string {
	return strings.TrimRight(cred["base_url"], "/") + "/" + strings.TrimLeft(cred[key], "/")
}

func (c partnerRESTConnector) PullOrders(ctx context.Context, cred map[string]string, req ConnectorPullRequest) (ConnectorOrderPage, error) {
	endpoint, err := url.Parse(partnerEndpoint(cred, "orders_path"))
	if err != nil {
		return ConnectorOrderPage{}, err
	}
	q := endpoint.Query()
	if !req.UpdatedAfter.IsZero() {
		q.Set("updated_after", req.UpdatedAfter.UTC().Format(time.RFC3339))
	}
	if req.Cursor != "" {
		q.Set("cursor", req.Cursor)
	}
	if req.Limit > 0 {
		q.Set("limit", strconv.Itoa(req.Limit))
	}
	endpoint.RawQuery = q.Encode()
	var page ConnectorOrderPage
	err = connectorJSONRequest(ctx, c.descriptor.Code, http.MethodGet, endpoint.String(), partnerHeaders(cred), nil, &page)
	return page, err
}

func (c partnerRESTConnector) PushInventory(ctx context.Context, cred map[string]string, updates []ConnectorInventoryUpdate) error {
	return connectorJSONRequest(ctx, c.descriptor.Code, http.MethodPost, partnerEndpoint(cred, "inventory_path"), partnerHeaders(cred), map[string]interface{}{"updates": updates}, nil)
}
func (c partnerRESTConnector) PushOrderStatus(ctx context.Context, cred map[string]string, update ConnectorStatusUpdate) error {
	return connectorJSONRequest(ctx, c.descriptor.Code, http.MethodPost, partnerEndpoint(cred, "status_path"), partnerHeaders(cred), update, nil)
}
func (c partnerRESTConnector) PublishProduct(ctx context.Context, cred map[string]string, payload ChannelProductPayload) (string, error) {
	var response struct {
		ID string `json:"id"`
	}
	err := connectorJSONRequest(ctx, c.descriptor.Code, http.MethodPost, partnerEndpoint(cred, "catalogue_path"), partnerHeaders(cred), payload, &response)
	return response.ID, err
}

type wooCommerceConnector struct{}

func (wooCommerceConnector) Descriptor() ConnectorDescriptor {
	return ConnectorDescriptor{Code: "WooCommerce", Kind: "webstore", PullOrders: true, PushInventory: true, PublishCatalogue: true, PushStatus: true}
}
func (wooCommerceConnector) RateLimit() (int, time.Duration) { return 100, time.Minute }
func (wooCommerceConnector) MapError(err error) string       { return DefaultConnectorErrorCode(err) }
func (wooCommerceConnector) ValidateCredentials(cred map[string]string) error {
	if cred["base_url"] == "" || cred["consumer_key"] == "" || cred["consumer_secret"] == "" {
		return fmt.Errorf("WooCommerce credential requires base_url, consumer_key and consumer_secret")
	}
	return nil
}
func wooBase(cred map[string]string) string {
	return strings.TrimRight(cred["base_url"], "/") + "/wp-json/wc/v3"
}
func wooHeaders(cred map[string]string) map[string]string {
	return map[string]string{"Authorization": "Basic " + base64.StdEncoding.EncodeToString([]byte(cred["consumer_key"]+":"+cred["consumer_secret"])), "Accept": "application/json"}
}

func (wooCommerceConnector) PullOrders(ctx context.Context, cred map[string]string, req ConnectorPullRequest) (ConnectorOrderPage, error) {
	endpoint, _ := url.Parse(wooBase(cred) + "/orders")
	q := endpoint.Query()
	q.Set("status", "processing")
	q.Set("per_page", "100")
	if !req.UpdatedAfter.IsZero() {
		q.Set("modified_after", req.UpdatedAfter.UTC().Format(time.RFC3339))
	}
	if req.Cursor != "" {
		q.Set("page", req.Cursor)
	}
	endpoint.RawQuery = q.Encode()
	var rows []struct {
		ID            int64  `json:"id"`
		Status        string `json:"status"`
		PaymentMethod string `json:"payment_method"`
		Billing       struct {
			FirstName string `json:"first_name"`
			LastName  string `json:"last_name"`
			Phone     string `json:"phone"`
		} `json:"billing"`
		Shipping struct {
			FirstName string `json:"first_name"`
			LastName  string `json:"last_name"`
			Address1  string `json:"address_1"`
			Address2  string `json:"address_2"`
			City      string `json:"city"`
			State     string `json:"state"`
			Postcode  string `json:"postcode"`
			Country   string `json:"country"`
		} `json:"shipping"`
		LineItems []struct {
			SKU      string  `json:"sku"`
			Quantity int     `json:"quantity"`
			Price    float64 `json:"price"`
		} `json:"line_items"`
	}
	if err := connectorJSONRequest(ctx, "WooCommerce", http.MethodGet, endpoint.String(), wooHeaders(cred), nil, &rows); err != nil {
		return ConnectorOrderPage{}, err
	}
	page := ConnectorOrderPage{Orders: make([]ConnectorOrder, 0, len(rows))}
	for _, row := range rows {
		o := ConnectorOrder{ChannelOrderID: strconv.FormatInt(row.ID, 10), CustomerName: strings.TrimSpace(row.Shipping.FirstName + " " + row.Shipping.LastName), CustomerPhone: row.Billing.Phone, ShippingState: row.Shipping.State, ShippingAddress: strings.TrimSpace(strings.Join([]string{row.Shipping.Address1, row.Shipping.Address2, row.Shipping.City, row.Shipping.State, row.Shipping.Postcode, row.Shipping.Country}, " ")), PaymentStatus: "Confirmed"}
		for _, line := range row.LineItems {
			o.Lines = append(o.Lines, ConnectorOrderLine{ChannelSKU: line.SKU, Quantity: line.Quantity, UnitPrice: line.Price})
		}
		page.Orders = append(page.Orders, o)
	}
	if len(rows) == 100 {
		n := 2
		if req.Cursor != "" {
			n, _ = strconv.Atoi(req.Cursor)
			n++
		}
		page.NextCursor = strconv.Itoa(n)
	}
	return page, nil
}

func (wooCommerceConnector) PushInventory(ctx context.Context, cred map[string]string, updates []ConnectorInventoryUpdate) error {
	for _, update := range updates {
		if update.ProductID == "" {
			return fmt.Errorf("WooCommerce mapping for %s has no external_product_id", update.ChannelSKU)
		}
		if err := connectorJSONRequest(ctx, "WooCommerce", http.MethodPut, wooBase(cred)+"/products/"+url.PathEscape(update.ProductID), wooHeaders(cred), map[string]interface{}{"manage_stock": true, "stock_quantity": update.Quantity}, nil); err != nil {
			return err
		}
	}
	return nil
}
func (wooCommerceConnector) PushOrderStatus(ctx context.Context, cred map[string]string, update ConnectorStatusUpdate) error {
	status := strings.ToLower(strings.ReplaceAll(update.Status, " ", "-"))
	return connectorJSONRequest(ctx, "WooCommerce", http.MethodPut, wooBase(cred)+"/orders/"+url.PathEscape(update.ChannelOrderID), wooHeaders(cred), map[string]string{"status": status}, nil)
}
func (wooCommerceConnector) PublishProduct(ctx context.Context, cred map[string]string, payload ChannelProductPayload) (string, error) {
	request := map[string]interface{}{"name": payload.Title, "sku": payload.ItemCode, "description": payload.Description, "status": "draft"}
	for k, v := range payload.Attributes {
		request[k] = v
	}
	var response struct {
		ID int64 `json:"id"`
	}
	if err := connectorJSONRequest(ctx, "WooCommerce", http.MethodPost, wooBase(cred)+"/products", wooHeaders(cred), request, &response); err != nil {
		return "", err
	}
	return strconv.FormatInt(response.ID, 10), nil
}
