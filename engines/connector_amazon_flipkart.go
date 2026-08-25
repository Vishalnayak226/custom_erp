package engines

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

var (
	amazonLWAURL    = "https://api.amazon.com/auth/o2/token"
	flipkartBaseURL = "https://api.flipkart.net"
)

func init() {
	registerOmnichannelConnector(amazonSPConnector{})
	registerOmnichannelConnector(flipkartConnector{})
}

type amazonSPConnector struct{}

func (amazonSPConnector) Descriptor() ConnectorDescriptor {
	return ConnectorDescriptor{Code: "Amazon", Kind: "marketplace", PullOrders: true, PushInventory: true, PublishCatalogue: true, PushStatus: true}
}
func (amazonSPConnector) RateLimit() (int, time.Duration) { return 5, time.Second }
func (amazonSPConnector) MapError(err error) string       { return DefaultConnectorErrorCode(err) }
func (amazonSPConnector) ValidateCredentials(c map[string]string) error {
	for _, k := range []string{"seller_id", "marketplace_id"} {
		if c[k] == "" {
			return fmt.Errorf("Amazon credential missing %s", k)
		}
	}
	if c["access_token"] == "" && (c["refresh_token"] == "" || c["lwa_client_id"] == "" || c["lwa_client_secret"] == "") {
		return fmt.Errorf("Amazon credential needs access_token or refresh_token plus LWA client credentials")
	}
	return nil
}
func amazonBase(c map[string]string) string {
	if c["base_url"] != "" {
		return strings.TrimRight(c["base_url"], "/")
	}
	return "https://sellingpartnerapi-eu.amazon.com"
}
func amazonToken(ctx context.Context, c map[string]string) (string, error) {
	if c["access_token"] != "" {
		return c["access_token"], nil
	}
	form := url.Values{"grant_type": {"refresh_token"}, "refresh_token": {c["refresh_token"]}, "client_id": {c["lwa_client_id"]}, "client_secret": {c["lwa_client_secret"]}}.Encode()
	status, body, err := doConnectorRequest(ctx, 20*time.Second, http.MethodPost, amazonLWAURL, map[string]string{"Content-Type": "application/x-www-form-urlencoded"}, []byte(form), "channel:amazon-auth")
	if err != nil {
		return "", err
	}
	if status < 200 || status >= 300 {
		return "", fmt.Errorf("Amazon LWA returned HTTP %d: %s", status, body)
	}
	var response struct {
		AccessToken string `json:"access_token"`
	}
	if err := json.Unmarshal(body, &response); err != nil {
		return "", err
	}
	if response.AccessToken == "" {
		return "", fmt.Errorf("Amazon LWA returned no access token")
	}
	return response.AccessToken, nil
}
func amazonHeaders(token string) map[string]string {
	return map[string]string{"x-amz-access-token": token, "Accept": "application/json"}
}

func (amazonSPConnector) PullOrders(ctx context.Context, c map[string]string, req ConnectorPullRequest) (ConnectorOrderPage, error) {
	token, err := amazonToken(ctx, c)
	if err != nil {
		return ConnectorOrderPage{}, err
	}
	endpoint, _ := url.Parse(amazonBase(c) + "/orders/2026-01-01/orders")
	q := endpoint.Query()
	q.Set("marketplaceIds", c["marketplace_id"])
	q.Set("includedData", "RECIPIENT,BUYER")
	if req.Limit > 0 {
		q.Set("maxResultsPerPage", strconv.Itoa(req.Limit))
	}
	if !req.UpdatedAfter.IsZero() {
		q.Set("lastUpdatedAfter", req.UpdatedAfter.UTC().Format(time.RFC3339))
	}
	if req.Cursor != "" {
		q.Set("paginationToken", req.Cursor)
	}
	endpoint.RawQuery = q.Encode()
	var response struct {
		Orders []struct {
			OrderID     string `json:"orderId"`
			OrderStatus string `json:"orderStatus"`
			Buyer       struct {
				Name string `json:"buyerName"`
			} `json:"buyer"`
			Recipient struct {
				DeliveryAddress struct {
					Name         string `json:"name"`
					AddressLine1 string `json:"addressLine1"`
					AddressLine2 string `json:"addressLine2"`
					City         string `json:"city"`
					State        string `json:"stateOrRegion"`
					PostalCode   string `json:"postalCode"`
					Country      string `json:"countryCode"`
					Phone        string `json:"phone"`
				} `json:"deliveryAddress"`
			} `json:"recipient"`
			Items []struct {
				Quantity int `json:"quantityOrdered"`
				Product  struct {
					SellerSKU string `json:"sellerSku"`
					Price     struct {
						UnitPrice struct {
							Amount string `json:"amount"`
						} `json:"unitPrice"`
					} `json:"price"`
				} `json:"product"`
			} `json:"orderItems"`
		} `json:"orders"`
		Pagination struct {
			NextToken string `json:"nextToken"`
		} `json:"pagination"`
	}
	if err := connectorJSONRequest(ctx, "Amazon", http.MethodGet, endpoint.String(), amazonHeaders(token), nil, &response); err != nil {
		return ConnectorOrderPage{}, err
	}
	page := ConnectorOrderPage{NextCursor: response.Pagination.NextToken}
	for _, row := range response.Orders {
		// lastUpdatedAfter surfaces every status transition, including a
		// buyer cancellation - Amazon does not filter those out of this
		// feed. Skip anything not actually fulfillable so a cancelled or
		// not-yet-payment-confirmed order can't be imported as Confirmed
		// and picked/shipped for a sale that will never be paid.
		switch strings.ToLower(row.OrderStatus) {
		case "canceled", "cancelled", "pending", "pendingavailability", "unfulfillable":
			continue
		}
		address := row.Recipient.DeliveryAddress
		o := ConnectorOrder{ChannelOrderID: row.OrderID, CustomerName: row.Buyer.Name, CustomerPhone: address.Phone, ShippingState: address.State, ShippingAddress: strings.TrimSpace(strings.Join([]string{address.Name, address.AddressLine1, address.AddressLine2, address.City, address.State, address.PostalCode, address.Country}, " ")), PaymentStatus: "Confirmed"}
		for _, line := range row.Items {
			amount, _ := strconv.ParseFloat(line.Product.Price.UnitPrice.Amount, 64)
			o.Lines = append(o.Lines, ConnectorOrderLine{ChannelSKU: line.Product.SellerSKU, Quantity: line.Quantity, UnitPrice: amount})
		}
		page.Orders = append(page.Orders, o)
	}
	return page, nil
}
func (amazonSPConnector) PushInventory(ctx context.Context, c map[string]string, updates []ConnectorInventoryUpdate) error {
	token, err := amazonToken(ctx, c)
	if err != nil {
		return err
	}
	for _, u := range updates {
		endpoint := fmt.Sprintf("%s/listings/2021-08-01/items/%s/%s?marketplaceIds=%s", amazonBase(c), url.PathEscape(c["seller_id"]), url.PathEscape(u.ChannelSKU), url.QueryEscape(c["marketplace_id"]))
		payload := map[string]interface{}{"productType": "PRODUCT", "patches": []map[string]interface{}{{"op": "merge", "path": "/attributes/fulfillment_availability", "value": []map[string]interface{}{{"fulfillment_channel_code": "DEFAULT", "quantity": u.Quantity}}}}}
		if err := connectorJSONRequest(ctx, "Amazon", http.MethodPatch, endpoint, amazonHeaders(token), payload, nil); err != nil {
			return err
		}
	}
	return nil
}
func (amazonSPConnector) PushOrderStatus(ctx context.Context, c map[string]string, u ConnectorStatusUpdate) error {
	token, err := amazonToken(ctx, c)
	if err != nil {
		return err
	}
	endpoint := amazonBase(c) + "/orders/v0/orders/" + url.PathEscape(u.ChannelOrderID) + "/shipmentConfirmation"
	if len(u.Items) == 0 {
		return fmt.Errorf("Amazon shipment confirmation requires order item ids and quantities")
	}
	items := make([]map[string]interface{}, 0, len(u.Items))
	for _, item := range u.Items {
		if item.OrderItemID == "" || item.Quantity <= 0 {
			return fmt.Errorf("Amazon shipment confirmation item requires order_item_id and positive quantity")
		}
		items = append(items, map[string]interface{}{"orderItemId": item.OrderItemID, "quantity": item.Quantity})
	}
	payload := map[string]interface{}{"marketplaceId": c["marketplace_id"], "packageDetail": map[string]interface{}{"packageReferenceId": "1", "carrierCode": u.Carrier, "trackingNumber": u.TrackingNumber, "shipDate": time.Now().UTC().Format(time.RFC3339), "orderItems": items}}
	return connectorJSONRequest(ctx, "Amazon", http.MethodPost, endpoint, amazonHeaders(token), payload, nil)
}
func (amazonSPConnector) PublishProduct(ctx context.Context, c map[string]string, p ChannelProductPayload) (string, error) {
	token, err := amazonToken(ctx, c)
	if err != nil {
		return "", err
	}
	productType := c["product_type"]
	if productType == "" {
		productType = "PRODUCT"
	}
	attributes := map[string]interface{}{}
	for k, v := range p.Attributes {
		attributes[k] = []map[string]string{{"value": v, "marketplace_id": c["marketplace_id"]}}
	}
	endpoint := fmt.Sprintf("%s/listings/2021-08-01/items/%s/%s?marketplaceIds=%s", amazonBase(c), url.PathEscape(c["seller_id"]), url.PathEscape(p.ItemCode), url.QueryEscape(c["marketplace_id"]))
	var response struct {
		SKU string `json:"sku"`
	}
	if err := connectorJSONRequest(ctx, "Amazon", http.MethodPut, endpoint, amazonHeaders(token), map[string]interface{}{"productType": productType, "requirements": "LISTING", "attributes": attributes}, &response); err != nil {
		return "", err
	}
	if response.SKU == "" {
		response.SKU = p.ItemCode
	}
	return response.SKU, nil
}

type flipkartConnector struct{}

func (flipkartConnector) Descriptor() ConnectorDescriptor {
	return ConnectorDescriptor{Code: "Flipkart", Kind: "marketplace", PullOrders: true, PushInventory: true, PublishCatalogue: true, PushStatus: true, RequiresLocation: true}
}
func (flipkartConnector) RateLimit() (int, time.Duration) { return 20, time.Minute }
func (flipkartConnector) MapError(err error) string       { return DefaultConnectorErrorCode(err) }
func (flipkartConnector) ValidateCredentials(c map[string]string) error {
	if c["access_token"] == "" && (c["app_id"] == "" || c["app_secret"] == "") {
		return fmt.Errorf("Flipkart credential needs access_token or app_id/app_secret")
	}
	return nil
}
func flipkartBase(c map[string]string) string {
	if c["base_url"] != "" {
		return strings.TrimRight(c["base_url"], "/")
	}
	return flipkartBaseURL
}
func flipkartToken(ctx context.Context, c map[string]string) (string, error) {
	if c["access_token"] != "" {
		return c["access_token"], nil
	}
	endpoint := flipkartBase(c) + "/oauth-service/oauth/token?grant_type=client_credentials&scope=Seller_Api"
	auth := base64.StdEncoding.EncodeToString([]byte(c["app_id"] + ":" + c["app_secret"]))
	status, body, err := doConnectorRequest(ctx, 20*time.Second, http.MethodGet, endpoint, map[string]string{"Authorization": "Basic " + auth}, nil, "channel:flipkart-auth")
	if err != nil {
		return "", err
	}
	if status < 200 || status >= 300 {
		return "", fmt.Errorf("Flipkart OAuth returned HTTP %d: %s", status, body)
	}
	var response struct {
		AccessToken string `json:"access_token"`
	}
	if err := json.Unmarshal(body, &response); err != nil {
		return "", err
	}
	if response.AccessToken == "" {
		return "", fmt.Errorf("Flipkart OAuth returned no access token")
	}
	return response.AccessToken, nil
}
func flipkartHeaders(token string) map[string]string {
	return map[string]string{"Authorization": "Bearer " + token, "Accept": "application/json"}
}
func (flipkartConnector) PullOrders(ctx context.Context, c map[string]string, req ConnectorPullRequest) (ConnectorOrderPage, error) {
	token, err := flipkartToken(ctx, c)
	if err != nil {
		return ConnectorOrderPage{}, err
	}
	payload := map[string]interface{}{"filter": map[string]interface{}{"type": "preDispatch", "states": []string{"APPROVED", "PACKING_IN_PROGRESS", "PACKED", "READY_TO_DISPATCH"}}, "pagination": map[string]interface{}{"pageSize": 20}, "sort": map[string]string{"field": "orderDate", "order": "asc"}}
	if c["location_id"] != "" {
		payload["filter"].(map[string]interface{})["locationId"] = c["location_id"]
	}
	endpoint := flipkartBase(c) + "/sellers/v3/shipments/filter/"
	if req.Cursor != "" {
		endpoint = req.Cursor
	}
	var response struct {
		NextPageURL string `json:"nextPageUrl"`
		Shipments   []struct {
			ShipmentID string `json:"shipmentId"`
			LocationID string `json:"locationId"`
			OrderItems []struct {
				OrderItemID     string `json:"orderItemId"`
				SellerSKU       string `json:"sku"`
				Quantity        int    `json:"quantity"`
				PriceComponents struct {
					SellingPrice float64 `json:"sellingPrice"`
				} `json:"priceComponents"`
			} `json:"orderItems"`
		} `json:"shipments"`
	}
	if err := connectorJSONRequest(ctx, "Flipkart", http.MethodPost, endpoint, flipkartHeaders(token), payload, &response); err != nil {
		return ConnectorOrderPage{}, err
	}
	page := ConnectorOrderPage{NextCursor: response.NextPageURL}
	for _, s := range response.Shipments {
		o := ConnectorOrder{ChannelOrderID: s.ShipmentID, CustomerName: "Flipkart order " + s.ShipmentID, ShippingAddress: "Flipkart protected address", PaymentStatus: "Confirmed", LocationCode: s.LocationID}
		for _, line := range s.OrderItems {
			o.Lines = append(o.Lines, ConnectorOrderLine{ChannelSKU: line.SellerSKU, Quantity: line.Quantity, UnitPrice: line.PriceComponents.SellingPrice})
		}
		page.Orders = append(page.Orders, o)
	}
	return page, nil
}
func (flipkartConnector) PushInventory(ctx context.Context, c map[string]string, updates []ConnectorInventoryUpdate) error {
	token, err := flipkartToken(ctx, c)
	if err != nil {
		return err
	}
	for start := 0; start < len(updates); start += 10 {
		end := start + 10
		if end > len(updates) {
			end = len(updates)
		}
		payload := map[string]interface{}{}
		for _, u := range updates[start:end] {
			location := u.LocationCode
			if location == "" {
				location = c["location_id"]
			}
			if u.ProductID == "" {
				return fmt.Errorf("Flipkart mapping for %s has no product_id", u.ChannelSKU)
			}
			payload[u.ChannelSKU] = map[string]interface{}{"product_id": u.ProductID, "locations": []map[string]interface{}{{"id": location, "inventory": u.Quantity}}}
		}
		if err := connectorJSONRequest(ctx, "Flipkart", http.MethodPost, flipkartBase(c)+"/sellers/listings/v3/update/inventory", flipkartHeaders(token), payload, nil); err != nil {
			return err
		}
	}
	return nil
}
func (flipkartConnector) PushOrderStatus(ctx context.Context, c map[string]string, u ConnectorStatusUpdate) error {
	token, err := flipkartToken(ctx, c)
	if err != nil {
		return err
	}
	return connectorJSONRequest(ctx, "Flipkart", http.MethodPost, flipkartBase(c)+"/sellers/v3/shipments/dispatch", flipkartHeaders(token), map[string]interface{}{"shipmentIds": []string{u.ChannelOrderID}}, nil)
}
func (flipkartConnector) PublishProduct(ctx context.Context, c map[string]string, p ChannelProductPayload) (string, error) {
	token, err := flipkartToken(ctx, c)
	if err != nil {
		return "", err
	}
	payload := map[string]interface{}{p.ItemCode: map[string]interface{}{"product_id": c["product_id"], "price": map[string]interface{}{}, "attributes": p.Attributes}}
	if err := connectorJSONRequest(ctx, "Flipkart", http.MethodPost, flipkartBase(c)+"/sellers/listings/v3", flipkartHeaders(token), payload, nil); err != nil {
		return "", err
	}
	return p.ItemCode, nil
}
