package engines

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"
)

// Stage 99.1 (user, 2026-10-19: "Shopify Integration"; D1-D6 approved
// 2026-10-21): Shopify as a full connector under the Stage 35.6 SDK -
// credential check naming what is missing, order pull, stock push per
// location, fulfilment / cancellation / RTO back to Shopify, webhook
// registration, publishing a design with its variants, linking existing
// products, reading stock levels and payouts. GraphQL Admin API only
// (shopifyAPIVersion); every call goes through callShopifyGraphQL, so the
// rate limiter, egress rules and error codes are the existing ones. The
// orchestration (what is sent when, the links kept) is shopify_sync.go.
//
// D1: the ERP owns products, prices and stock; Shopify owns online orders
// and their payments. D2: a custom app's admin token per client.

type shopifySDKConnector struct{ shopifyConnector }

func init() {
	// Replaces the Stage 35.6 legacy wrapper for Shopify (registered from
	// connector_zz_legacy_sdk.go before this stage).
	registerOmnichannelConnector(shopifySDKConnector{})
}

func (shopifySDKConnector) Descriptor() ConnectorDescriptor {
	return ConnectorDescriptor{Code: "Shopify", Kind: "webstore", PullOrders: true, PushInventory: true, PublishCatalogue: true, PushStatus: true}
}

func (shopifySDKConnector) ValidateCredentials(cred map[string]string) error {
	for _, field := range []string{"shop_domain", "access_token"} {
		if strings.TrimSpace(cred[field]) == "" {
			return fmt.Errorf("Shopify credential missing %s", field)
		}
	}
	return nil
}

func (shopifySDKConnector) MapError(err error) string {
	var v *ValidationError
	if errors.As(err, &v) && v.Code != "" {
		return v.Code
	}
	return DefaultConnectorErrorCode(err)
}

func shopifyGQL(ctx context.Context, cred map[string]string, query string, vars map[string]interface{}, out interface{}) error {
	resp, err := callShopifyGraphQL(ctx, cred["shop_domain"], cred["access_token"], query, vars)
	if err != nil {
		return err
	}
	if out == nil {
		return nil
	}
	if err := json.Unmarshal(resp.Data, out); err != nil {
		return fmt.Errorf("shopify returned an unexpected answer: %v", err)
	}
	return nil
}

type shopifyUserError struct {
	Field   []string `json:"field"`
	Message string   `json:"message"`
}

func shopifyUserErrors(errs []shopifyUserError, what string) error {
	if len(errs) == 0 {
		return nil
	}
	msgs := make([]string, len(errs))
	for i, e := range errs {
		msgs[i] = e.Message
	}
	msg := fmt.Sprintf("shopify refused %s: %s", what, strings.Join(msgs, "; "))
	return &ValidationError{Code: classifyConnectorError(msg), Message: msg}
}

type shopifyMoney struct {
	ShopMoney struct {
		Amount string `json:"amount"`
	} `json:"shopMoney"`
}

func (m shopifyMoney) f() float64 {
	v, _ := strconv.ParseFloat(m.ShopMoney.Amount, 64)
	return v
}

// --- 99.1.2 the credential check -----------------------------------------------

// ShopifyRequiredScopes are the custom app's access the connector uses.
// Fulfilment needs one of the fulfilment-order scopes (shopifyFulfilScopes).
var ShopifyRequiredScopes = []string{"read_products", "write_products", "read_inventory", "write_inventory", "read_locations",
	"read_orders", "write_orders", "read_customers"}

var shopifyFulfilScopes = []string{"write_merchant_managed_fulfillment_orders", "write_assigned_fulfillment_orders", "write_third_party_fulfillment_orders", "write_fulfillments"}

// ShopifyLocation is one Shopify location.
type ShopifyLocation struct {
	ID     string `json:"id"`
	Name   string `json:"name"`
	Active bool   `json:"active"`
}

// ShopifyCheck is what Test connection shows.
type ShopifyCheck struct {
	Shop          string            `json:"shop"`
	Domain        string            `json:"domain"`
	Currency      string            `json:"currency"`
	Locations     []ShopifyLocation `json:"locations"`
	Scopes        []string          `json:"scopes"`
	MissingScopes []string          `json:"missing_scopes"`
	Payouts       bool              `json:"payouts"` // read_shopify_payments_payouts granted
}

const shopifyCheckQuery = `query { shop { name myshopifyDomain currencyCode }
  currentAppInstallation { accessScopes { handle } }
  locations(first: 50) { edges { node { id name isActive } } } }`

// CheckShopify tests the token, lists the shop's locations and names any
// access the app is missing.
func (shopifySDKConnector) CheckShopify(ctx context.Context, cred map[string]string) (ShopifyCheck, error) {
	var out struct {
		Shop struct {
			Name     string `json:"name"`
			Domain   string `json:"myshopifyDomain"`
			Currency string `json:"currencyCode"`
		} `json:"shop"`
		App struct {
			Scopes []struct {
				Handle string `json:"handle"`
			} `json:"accessScopes"`
		} `json:"currentAppInstallation"`
		Locations struct {
			Edges []struct {
				Node struct {
					ID     string `json:"id"`
					Name   string `json:"name"`
					Active bool   `json:"isActive"`
				} `json:"node"`
			} `json:"edges"`
		} `json:"locations"`
	}
	if err := shopifyGQL(ctx, cred, shopifyCheckQuery, nil, &out); err != nil {
		return ShopifyCheck{}, err
	}
	c := ShopifyCheck{Shop: out.Shop.Name, Domain: out.Shop.Domain, Currency: out.Shop.Currency, Locations: []ShopifyLocation{}, MissingScopes: []string{}}
	has := map[string]bool{}
	for _, s := range out.App.Scopes {
		c.Scopes = append(c.Scopes, s.Handle)
		has[s.Handle] = true
		if strings.HasPrefix(s.Handle, "write_") {
			has["read_"+strings.TrimPrefix(s.Handle, "write_")] = true // a write scope implies its read
		}
	}
	for _, s := range ShopifyRequiredScopes {
		if !has[s] {
			c.MissingScopes = append(c.MissingScopes, s)
		}
	}
	fulfil := false
	for _, s := range shopifyFulfilScopes {
		fulfil = fulfil || has[s]
	}
	if !fulfil {
		c.MissingScopes = append(c.MissingScopes, "write_merchant_managed_fulfillment_orders")
	}
	c.Payouts = has["read_shopify_payments_payouts"]
	for _, e := range out.Locations.Edges {
		c.Locations = append(c.Locations, ShopifyLocation{ID: e.Node.ID, Name: e.Node.Name, Active: e.Node.Active})
	}
	return c, nil
}

// --- orders: one shape for the pull and the webhooks --------------------------------

// ShopifyAddress is a Shopify address, flattened.
type ShopifyAddress struct {
	Name, Address1, Address2, City, Province, ProvinceCode, Zip, Country, Phone string
}

func (a ShopifyAddress) Line() string {
	parts := []string{}
	for _, p := range []string{a.Address1, a.Address2, a.City, a.Province, a.Zip, a.Country} {
		if strings.TrimSpace(p) != "" {
			parts = append(parts, strings.TrimSpace(p))
		}
	}
	return strings.Join(parts, ", ")
}

// ShopifyTaxLine is one tax line (CGST / SGST / IGST... as Shopify titles it).
type ShopifyTaxLine struct {
	Title  string  `json:"title"`
	Rate   float64 `json:"rate"`
	Amount float64 `json:"amount"`
}

// ShopifyOrderLine is one line of a Shopify order.
type ShopifyOrderLine struct {
	LineID    string           `json:"line_id"`
	SKU       string           `json:"sku"`
	Title     string           `json:"title"`
	Qty       int              `json:"qty"`
	UnitPrice float64          `json:"unit_price"` // before discounts
	Discount  float64          `json:"discount"`   // the whole line's
	Tax       []ShopifyTaxLine `json:"tax"`
}

// ShopifyOrder is a Shopify order as the ERP keeps it (99.5.1).
type ShopifyOrder struct {
	ID          string // numeric id (what the webhooks carry), the import key
	GID         string // gid://shopify/Order/...
	Number      string // "#1001"
	Email       string
	Phone       string
	CustomerID  string
	FirstName   string
	LastName    string
	CreatedAt   time.Time
	UpdatedAt   time.Time
	CancelledAt *time.Time
	Financial   string // paid, pending, refunded...
	Fulfillment string
	Gateways    []string
	Currency    string
	Discount    float64 // order-level (all) discounts
	Shipping    float64
	Tax         []ShopifyTaxLine
	Total       float64
	Ship        ShopifyAddress
	Bill        ShopifyAddress
	Lines       []ShopifyOrderLine
	Tags        []string
	Note        string
	// CheckoutToken is the web-shop cart the order came from (Stage 101's
	// cart journey ends on it).
	CheckoutToken string
	// DiscountCodes are the codes typed at the checkout; one that is an ERP
	// voucher takes a use of it (Stage 101.11.2).
	DiscountCodes []string
}

// COD is a cash-on-delivery order.
func (o ShopifyOrder) COD() bool {
	for _, g := range o.Gateways {
		if strings.Contains(strings.ToLower(g), "cash on delivery") || strings.EqualFold(g, "cod") {
			return true
		}
	}
	return false
}

func shopifyGIDNumber(gid string) string {
	if i := strings.LastIndex(gid, "/"); i >= 0 {
		return gid[i+1:]
	}
	return gid
}

const shopifyOrderFields = `id legacyResourceId name email phone createdAt updatedAt cancelledAt displayFinancialStatus displayFulfillmentStatus
  paymentGatewayNames currencyCode tags note discountCodes
  totalDiscountsSet { shopMoney { amount } } totalShippingPriceSet { shopMoney { amount } } totalPriceSet { shopMoney { amount } }
  taxLines { title rate priceSet { shopMoney { amount } } }
  customer { id firstName lastName email phone }
  shippingAddress { name address1 address2 city province provinceCode zip country phone }
  billingAddress { name address1 address2 city province provinceCode zip country phone }
  lineItems(first: 100) { edges { node { id sku title quantity
    originalUnitPriceSet { shopMoney { amount } } totalDiscountSet { shopMoney { amount } }
    taxLines { title rate priceSet { shopMoney { amount } } } } } }`

type shopifyGQLAddress struct {
	Name         string `json:"name"`
	Address1     string `json:"address1"`
	Address2     string `json:"address2"`
	City         string `json:"city"`
	Province     string `json:"province"`
	ProvinceCode string `json:"provinceCode"`
	Zip          string `json:"zip"`
	Country      string `json:"country"`
	Phone        string `json:"phone"`
}

func (a *shopifyGQLAddress) addr() ShopifyAddress {
	if a == nil {
		return ShopifyAddress{}
	}
	return ShopifyAddress{Name: a.Name, Address1: a.Address1, Address2: a.Address2, City: a.City, Province: a.Province, ProvinceCode: a.ProvinceCode, Zip: a.Zip, Country: a.Country, Phone: a.Phone}
}

type shopifyGQLTax struct {
	Title    string       `json:"title"`
	Rate     float64      `json:"rate"`
	PriceSet shopifyMoney `json:"priceSet"`
}

func gqlTaxes(in []shopifyGQLTax) []ShopifyTaxLine {
	out := make([]ShopifyTaxLine, 0, len(in))
	for _, t := range in {
		out = append(out, ShopifyTaxLine{Title: t.Title, Rate: t.Rate, Amount: t.PriceSet.f()})
	}
	return out
}

type shopifyGQLOrder struct {
	ID          string          `json:"id"`
	Legacy      string          `json:"legacyResourceId"`
	Name        string          `json:"name"`
	Email       string          `json:"email"`
	Phone       string          `json:"phone"`
	CreatedAt   time.Time       `json:"createdAt"`
	UpdatedAt   time.Time       `json:"updatedAt"`
	CancelledAt *time.Time      `json:"cancelledAt"`
	Financial   string          `json:"displayFinancialStatus"`
	Fulfillment string          `json:"displayFulfillmentStatus"`
	Gateways    []string        `json:"paymentGatewayNames"`
	Currency    string          `json:"currencyCode"`
	Tags        []string        `json:"tags"`
	Note        string          `json:"note"`
	Codes       []string        `json:"discountCodes"`
	Discounts   shopifyMoney    `json:"totalDiscountsSet"`
	Shipping    shopifyMoney    `json:"totalShippingPriceSet"`
	Total       shopifyMoney    `json:"totalPriceSet"`
	TaxLines    []shopifyGQLTax `json:"taxLines"`
	Customer    *struct {
		ID        string `json:"id"`
		FirstName string `json:"firstName"`
		LastName  string `json:"lastName"`
		Email     string `json:"email"`
		Phone     string `json:"phone"`
	} `json:"customer"`
	Ship      *shopifyGQLAddress `json:"shippingAddress"`
	Bill      *shopifyGQLAddress `json:"billingAddress"`
	LineItems struct {
		Edges []struct {
			Node struct {
				ID       string          `json:"id"`
				SKU      string          `json:"sku"`
				Title    string          `json:"title"`
				Quantity int             `json:"quantity"`
				Price    shopifyMoney    `json:"originalUnitPriceSet"`
				Discount shopifyMoney    `json:"totalDiscountSet"`
				TaxLines []shopifyGQLTax `json:"taxLines"`
			} `json:"node"`
		} `json:"edges"`
	} `json:"lineItems"`
}

func (g shopifyGQLOrder) order() ShopifyOrder {
	o := ShopifyOrder{ID: firstNonEmpty(g.Legacy, shopifyGIDNumber(g.ID)), GID: g.ID, Number: g.Name, Email: g.Email, Phone: g.Phone,
		CreatedAt: g.CreatedAt, UpdatedAt: g.UpdatedAt, CancelledAt: g.CancelledAt, Financial: strings.ToLower(g.Financial),
		Fulfillment: strings.ToLower(g.Fulfillment), Gateways: g.Gateways, Currency: g.Currency, Discount: g.Discounts.f(),
		Shipping: g.Shipping.f(), Total: g.Total.f(), Tax: gqlTaxes(g.TaxLines), Ship: g.Ship.addr(), Bill: g.Bill.addr(), Tags: g.Tags, Note: g.Note, DiscountCodes: g.Codes}
	if g.Customer != nil {
		o.CustomerID, o.FirstName, o.LastName = shopifyGIDNumber(g.Customer.ID), g.Customer.FirstName, g.Customer.LastName
		o.Email, o.Phone = firstNonEmpty(o.Email, g.Customer.Email), firstNonEmpty(o.Phone, g.Customer.Phone)
	}
	for _, e := range g.LineItems.Edges {
		n := e.Node
		o.Lines = append(o.Lines, ShopifyOrderLine{LineID: shopifyGIDNumber(n.ID), SKU: n.SKU, Title: n.Title, Qty: n.Quantity,
			UnitPrice: n.Price.f(), Discount: n.Discount.f(), Tax: gqlTaxes(n.TaxLines)})
	}
	return o
}

// shopifyRESTMoney reads a webhook's money ("12.50" or 12.5).
type shopifyRESTMoney float64

func (m *shopifyRESTMoney) UnmarshalJSON(b []byte) error {
	s := strings.Trim(string(b), `"`)
	if s == "" || s == "null" {
		*m = 0
		return nil
	}
	v, err := strconv.ParseFloat(s, 64)
	*m = shopifyRESTMoney(v)
	return err
}

type shopifyRESTAddress struct {
	Name         string `json:"name"`
	FirstName    string `json:"first_name"`
	LastName     string `json:"last_name"`
	Address1     string `json:"address1"`
	Address2     string `json:"address2"`
	City         string `json:"city"`
	Province     string `json:"province"`
	ProvinceCode string `json:"province_code"`
	Zip          string `json:"zip"`
	Country      string `json:"country"`
	Phone        string `json:"phone"`
}

func (a *shopifyRESTAddress) addr() ShopifyAddress {
	if a == nil {
		return ShopifyAddress{}
	}
	return ShopifyAddress{Name: firstNonEmpty(a.Name, strings.TrimSpace(a.FirstName+" "+a.LastName)), Address1: a.Address1, Address2: a.Address2,
		City: a.City, Province: a.Province, ProvinceCode: a.ProvinceCode, Zip: a.Zip, Country: a.Country, Phone: a.Phone}
}

type shopifyRESTTax struct {
	Title string           `json:"title"`
	Rate  float64          `json:"rate"`
	Price shopifyRESTMoney `json:"price"`
}

func restTaxes(in []shopifyRESTTax) []ShopifyTaxLine {
	out := make([]ShopifyTaxLine, 0, len(in))
	for _, t := range in {
		out = append(out, ShopifyTaxLine{Title: t.Title, Rate: t.Rate, Amount: float64(t.Price)})
	}
	return out
}

// ParseShopifyOrderWebhook reads an orders/* webhook body (REST JSON).
func ParseShopifyOrderWebhook(body []byte) (ShopifyOrder, error) {
	var w struct {
		ID             json.Number      `json:"id"`
		AdminGID       string           `json:"admin_graphql_api_id"`
		Name           string           `json:"name"`
		Email          string           `json:"email"`
		Phone          string           `json:"phone"`
		CreatedAt      time.Time        `json:"created_at"`
		UpdatedAt      time.Time        `json:"updated_at"`
		CancelledAt    *time.Time       `json:"cancelled_at"`
		Financial      string           `json:"financial_status"`
		Fulfillment    string           `json:"fulfillment_status"`
		Gateways       []string         `json:"payment_gateway_names"`
		Currency       string           `json:"currency"`
		Tags           string           `json:"tags"`
		CheckoutToken  string           `json:"checkout_token"`
		Note           string           `json:"note"`
		TotalDiscounts shopifyRESTMoney `json:"total_discounts"`
		TotalPrice     shopifyRESTMoney `json:"total_price"`
		TaxLines       []shopifyRESTTax `json:"tax_lines"`
		Customer       *struct {
			ID        json.Number `json:"id"`
			FirstName string      `json:"first_name"`
			LastName  string      `json:"last_name"`
			Email     string      `json:"email"`
			Phone     string      `json:"phone"`
		} `json:"customer"`
		Ship          *shopifyRESTAddress `json:"shipping_address"`
		Bill          *shopifyRESTAddress `json:"billing_address"`
		ShippingLines []struct {
			Price shopifyRESTMoney `json:"price"`
		} `json:"shipping_lines"`
		LineItems []struct {
			ID            json.Number      `json:"id"`
			SKU           string           `json:"sku"`
			Title         string           `json:"title"`
			Quantity      int              `json:"quantity"`
			Price         shopifyRESTMoney `json:"price"`
			TotalDiscount shopifyRESTMoney `json:"total_discount"`
			TaxLines      []shopifyRESTTax `json:"tax_lines"`
		} `json:"line_items"`
		DiscountCodes []struct {
			Code string `json:"code"`
		} `json:"discount_codes"`
	}
	if err := json.Unmarshal(body, &w); err != nil {
		return ShopifyOrder{}, fmt.Errorf("not a Shopify order: %v", err)
	}
	if w.ID.String() == "" {
		return ShopifyOrder{}, fmt.Errorf("the Shopify order has no id")
	}
	o := ShopifyOrder{ID: w.ID.String(), GID: firstNonEmpty(w.AdminGID, "gid://shopify/Order/"+w.ID.String()), Number: w.Name, Email: w.Email, Phone: w.Phone,
		CreatedAt: w.CreatedAt, UpdatedAt: w.UpdatedAt, CancelledAt: w.CancelledAt, Financial: strings.ToLower(w.Financial), Fulfillment: strings.ToLower(w.Fulfillment),
		Gateways: w.Gateways, Currency: w.Currency, Discount: float64(w.TotalDiscounts), Total: float64(w.TotalPrice), Tax: restTaxes(w.TaxLines),
		Ship: w.Ship.addr(), Bill: w.Bill.addr(), Note: w.Note, CheckoutToken: w.CheckoutToken}
	for _, d := range w.DiscountCodes {
		if d.Code != "" {
			o.DiscountCodes = append(o.DiscountCodes, d.Code)
		}
	}
	for _, t := range strings.Split(w.Tags, ",") {
		if t = strings.TrimSpace(t); t != "" {
			o.Tags = append(o.Tags, t)
		}
	}
	for _, s := range w.ShippingLines {
		o.Shipping += float64(s.Price)
	}
	if w.Customer != nil {
		o.CustomerID, o.FirstName, o.LastName = w.Customer.ID.String(), w.Customer.FirstName, w.Customer.LastName
		o.Email, o.Phone = firstNonEmpty(o.Email, w.Customer.Email), firstNonEmpty(o.Phone, w.Customer.Phone)
	}
	for _, l := range w.LineItems {
		o.Lines = append(o.Lines, ShopifyOrderLine{LineID: l.ID.String(), SKU: l.SKU, Title: l.Title, Qty: l.Quantity, UnitPrice: float64(l.Price),
			Discount: float64(l.TotalDiscount), Tax: restTaxes(l.TaxLines)})
	}
	return o, nil
}

const shopifyOrdersQuery = `query orders($first: Int!, $after: String, $query: String) {
  orders(first: $first, after: $after, query: $query, sortKey: UPDATED_AT) {
    pageInfo { hasNextPage endCursor }
    edges { node { ` + shopifyOrderFields + ` } } } }`

// PullShopifyOrders is one page of orders updated after since (99.5.2's
// 15-minute pull).
func (shopifySDKConnector) PullShopifyOrders(ctx context.Context, cred map[string]string, since time.Time, cursor string) ([]ShopifyOrder, string, error) {
	vars := map[string]interface{}{"first": 50, "query": fmt.Sprintf("updated_at:>'%s'", since.UTC().Format(time.RFC3339))}
	if cursor != "" {
		vars["after"] = cursor
	}
	var out struct {
		Orders struct {
			PageInfo struct {
				HasNext bool   `json:"hasNextPage"`
				End     string `json:"endCursor"`
			} `json:"pageInfo"`
			Edges []struct {
				Node shopifyGQLOrder `json:"node"`
			} `json:"edges"`
		} `json:"orders"`
	}
	if err := shopifyGQL(ctx, cred, shopifyOrdersQuery, vars, &out); err != nil {
		return nil, "", err
	}
	orders := make([]ShopifyOrder, 0, len(out.Orders.Edges))
	for _, e := range out.Orders.Edges {
		orders = append(orders, e.Node.order())
	}
	next := ""
	if out.Orders.PageInfo.HasNext {
		next = out.Orders.PageInfo.End
	}
	return orders, next, nil
}

// PullOrders is the SDK's generic form (the scheduler uses shopify_sync.go's
// richer import; this keeps the SDK contract complete).
func (c shopifySDKConnector) PullOrders(ctx context.Context, cred map[string]string, req ConnectorPullRequest) (ConnectorOrderPage, error) {
	orders, next, err := c.PullShopifyOrders(ctx, cred, req.UpdatedAfter, req.Cursor)
	if err != nil {
		return ConnectorOrderPage{}, err
	}
	page := ConnectorOrderPage{NextCursor: next}
	for _, o := range orders {
		co := ConnectorOrder{ChannelOrderID: o.ID, CustomerName: strings.TrimSpace(o.FirstName + " " + o.LastName), CustomerPhone: o.Phone,
			ShippingAddress: o.Ship.Line(), ShippingState: o.Ship.Province, PaymentStatus: shopifyPaymentStatus(o)}
		for _, l := range o.Lines {
			co.Lines = append(co.Lines, ConnectorOrderLine{ChannelSKU: l.SKU, Quantity: l.Qty, UnitPrice: l.UnitPrice})
		}
		page.Orders = append(page.Orders, co)
	}
	return page, nil
}

// shopifyPaymentStatus is the ERP's payment status for a Shopify order: COD
// is collected on delivery; paid is confirmed.
func shopifyPaymentStatus(o ShopifyOrder) string {
	switch {
	case o.COD():
		return "COD"
	case o.Financial == "paid" || o.Financial == "partially_paid" || o.Financial == "partially_refunded":
		return "Confirmed"
	case o.Financial == "refunded" || o.Financial == "voided":
		return "Refunded"
	}
	return "Pending"
}

// --- stock -----------------------------------------------------------------------

const shopifyInventorySetMutation = `mutation set($input: InventorySetQuantitiesInput!) {
  inventorySetQuantities(input: $input) { userErrors { field message } } }`

// PushInventory sets "available" quantities: ProductID is the variant's
// inventory item gid, LocationCode the Shopify location gid (shopify_sync.go
// builds them from the links and the location map). 250 per call, grouped.
func (shopifySDKConnector) PushInventory(ctx context.Context, cred map[string]string, updates []ConnectorInventoryUpdate) error {
	for start := 0; start < len(updates); start += 250 {
		end := start + 250
		if end > len(updates) {
			end = len(updates)
		}
		qs := make([]map[string]interface{}, 0, end-start)
		for _, u := range updates[start:end] {
			if u.ProductID == "" || u.LocationCode == "" {
				return fmt.Errorf("Shopify stock for %s needs its inventory item and location", u.ChannelSKU)
			}
			q := u.Quantity
			if q < 0 {
				q = 0
			}
			qs = append(qs, map[string]interface{}{"inventoryItemId": u.ProductID, "locationId": u.LocationCode, "quantity": q})
		}
		var out struct {
			Set struct {
				UserErrors []shopifyUserError `json:"userErrors"`
			} `json:"inventorySetQuantities"`
		}
		input := map[string]interface{}{"name": "available", "reason": "correction", "ignoreCompareQuantity": true, "quantities": qs}
		if err := shopifyGQL(ctx, cred, shopifyInventorySetMutation, map[string]interface{}{"input": input}, &out); err != nil {
			return err
		}
		if err := shopifyUserErrors(out.Set.UserErrors, "the stock"); err != nil {
			return err
		}
	}
	return nil
}

// ShopifyStockLevel is one variant's available quantity at one location.
type ShopifyStockLevel struct {
	InventoryItemID string `json:"inventory_item_id"`
	LocationID      string `json:"location_id"`
	Available       int    `json:"available"`
}

const shopifyLevelsQuery = `query levels($ids: [ID!]!) { nodes(ids: $ids) { ... on InventoryItem { id
  inventoryLevels(first: 50) { edges { node { location { id } quantities(names: ["available"]) { name quantity } } } } } } }`

// ReadStockLevels reads what Shopify shows as available for inventory items
// (99.4.2's mismatch report).
func (shopifySDKConnector) ReadStockLevels(ctx context.Context, cred map[string]string, itemIDs []string) ([]ShopifyStockLevel, error) {
	out := []ShopifyStockLevel{}
	for start := 0; start < len(itemIDs); start += 100 {
		end := start + 100
		if end > len(itemIDs) {
			end = len(itemIDs)
		}
		var res struct {
			Nodes []*struct {
				ID     string `json:"id"`
				Levels struct {
					Edges []struct {
						Node struct {
							Location struct {
								ID string `json:"id"`
							} `json:"location"`
							Quantities []struct {
								Name     string `json:"name"`
								Quantity int    `json:"quantity"`
							} `json:"quantities"`
						} `json:"node"`
					} `json:"edges"`
				} `json:"inventoryLevels"`
			} `json:"nodes"`
		}
		if err := shopifyGQL(ctx, cred, shopifyLevelsQuery, map[string]interface{}{"ids": itemIDs[start:end]}, &res); err != nil {
			return nil, err
		}
		for _, n := range res.Nodes {
			if n == nil {
				continue
			}
			for _, e := range n.Levels.Edges {
				lvl := ShopifyStockLevel{InventoryItemID: n.ID, LocationID: e.Node.Location.ID}
				for _, q := range e.Node.Quantities {
					if q.Name == "available" {
						lvl.Available = q.Quantity
					}
				}
				out = append(out, lvl)
			}
		}
	}
	return out, nil
}

// --- 99.6 status back to Shopify -----------------------------------------------------

const shopifyFulfilmentOrdersQuery = `query fo($id: ID!) { order(id: $id) { fulfillmentOrders(first: 20) { edges { node { id status
  lineItems(first: 100) { edges { node { id remainingQuantity lineItem { id sku } } } } } } } } }`

const shopifyFulfilmentCreateMutation = `mutation fulfil($fulfillment: FulfillmentInput!) {
  fulfillmentCreate(fulfillment: $fulfillment) { fulfillment { id status } userErrors { field message } } }`

const shopifyOrderCancelMutation = `mutation cancel($orderId: ID!, $reason: OrderCancelReason!, $refund: Boolean!, $restock: Boolean!, $notify: Boolean) {
  orderCancel(orderId: $orderId, reason: $reason, refund: $refund, restock: $restock, notifyCustomer: $notify) {
    orderCancelUserErrors { field message } } }`

const shopifyTagsAddMutation = `mutation tags($id: ID!, $tags: [String!]!) { tagsAdd(id: $id, tags: $tags) { userErrors { field message } } }`

func shopifyOrderGID(id string) string {
	if strings.HasPrefix(id, "gid://") {
		return id
	}
	return "gid://shopify/Order/" + id
}

// PushOrderStatus sends the ERP's step to Shopify: Shipped / Dispatched
// creates the fulfilment with carrier and tracking (a part shipment when
// Items name lines and quantities - by Shopify line id or SKU); Cancelled /
// Rejected cancels the order (stock back, no automatic refund); RTO tags it.
func (shopifySDKConnector) PushOrderStatus(ctx context.Context, cred map[string]string, u ConnectorStatusUpdate) error {
	orderGID := shopifyOrderGID(u.ChannelOrderID)
	switch strings.ToLower(strings.TrimSpace(u.Status)) {
	case "shipped", "dispatched", "fulfilled", "handed over":
		var fo struct {
			Order *struct {
				FOs struct {
					Edges []struct {
						Node struct {
							ID     string `json:"id"`
							Status string `json:"status"`
							Lines  struct {
								Edges []struct {
									Node struct {
										ID        string `json:"id"`
										Remaining int    `json:"remainingQuantity"`
										LineItem  struct {
											ID  string `json:"id"`
											SKU string `json:"sku"`
										} `json:"lineItem"`
									} `json:"node"`
								} `json:"edges"`
							} `json:"lineItems"`
						} `json:"node"`
					} `json:"edges"`
				} `json:"fulfillmentOrders"`
			} `json:"order"`
		}
		if err := shopifyGQL(ctx, cred, shopifyFulfilmentOrdersQuery, map[string]interface{}{"id": orderGID}, &fo); err != nil {
			return err
		}
		if fo.Order == nil {
			return fmt.Errorf("Shopify order %s was not found", u.ChannelOrderID)
		}
		want := map[string]int{} // line id or sku -> qty; empty = everything left
		for _, it := range u.Items {
			want[strings.ToLower(it.OrderItemID)] += it.Quantity
		}
		groups := []map[string]interface{}{}
		for _, e := range fo.Order.FOs.Edges {
			if st := strings.ToUpper(e.Node.Status); st != "OPEN" && st != "IN_PROGRESS" {
				continue
			}
			lines := []map[string]interface{}{}
			for _, le := range e.Node.Lines.Edges {
				n := le.Node
				qty := n.Remaining
				if len(want) > 0 {
					key := strings.ToLower(shopifyGIDNumber(n.LineItem.ID))
					q, ok := want[key]
					if !ok {
						key = strings.ToLower(n.LineItem.SKU)
						q, ok = want[key]
					}
					if !ok {
						continue
					}
					if q < qty {
						qty = q
					}
					want[key] -= qty
				}
				if qty > 0 {
					lines = append(lines, map[string]interface{}{"id": n.ID, "quantity": qty})
				}
			}
			if len(lines) > 0 {
				groups = append(groups, map[string]interface{}{"fulfillmentOrderId": e.Node.ID, "fulfillmentOrderLineItems": lines})
			}
		}
		if len(groups) == 0 {
			return nil // already fulfilled in Shopify
		}
		f := map[string]interface{}{"lineItemsByFulfillmentOrder": groups, "notifyCustomer": true}
		if u.TrackingNumber != "" || u.Carrier != "" {
			f["trackingInfo"] = map[string]interface{}{"number": u.TrackingNumber, "company": u.Carrier}
		}
		var out struct {
			Create struct {
				UserErrors []shopifyUserError `json:"userErrors"`
			} `json:"fulfillmentCreate"`
		}
		if err := shopifyGQL(ctx, cred, shopifyFulfilmentCreateMutation, map[string]interface{}{"fulfillment": f}, &out); err != nil {
			return err
		}
		return shopifyUserErrors(out.Create.UserErrors, "the fulfilment")
	case "cancelled", "canceled", OrderDecisionRejected:
		var out struct {
			Cancel struct {
				Errors []shopifyUserError `json:"orderCancelUserErrors"`
			} `json:"orderCancel"`
		}
		if err := shopifyGQL(ctx, cred, shopifyOrderCancelMutation, map[string]interface{}{"orderId": orderGID, "reason": "OTHER", "refund": false, "restock": true, "notify": true}, &out); err != nil {
			return err
		}
		return shopifyUserErrors(out.Cancel.Errors, "the cancellation")
	case "rto", "returned to origin":
		return shopifyAddTags(ctx, cred, orderGID, []string{"RTO"})
	case strings.ToLower(OrderDecisionAccepted):
		return shopifyAddTags(ctx, cred, orderGID, []string{"ERP accepted"})
	}
	return nil // a step Shopify has no word for
}

func shopifyAddTags(ctx context.Context, cred map[string]string, gid string, tags []string) error {
	var out struct {
		Add struct {
			UserErrors []shopifyUserError `json:"userErrors"`
		} `json:"tagsAdd"`
	}
	if err := shopifyGQL(ctx, cred, shopifyTagsAddMutation, map[string]interface{}{"id": gid, "tags": tags}, &out); err != nil {
		return err
	}
	return shopifyUserErrors(out.Add.UserErrors, "the tag")
}

// --- 99.2.2 webhooks --------------------------------------------------------------

// ShopifyWebhookTopics are the topics the ERP listens to.
var ShopifyWebhookTopics = []string{"ORDERS_CREATE", "ORDERS_UPDATED", "ORDERS_CANCELLED", "REFUNDS_CREATE", "FULFILLMENTS_CREATE", "CHECKOUTS_CREATE", "CHECKOUTS_UPDATE", "APP_UNINSTALLED"}

const shopifyWebhooksQuery = `query { webhookSubscriptions(first: 100) { edges { node { id topic endpoint { __typename ... on WebhookHttpEndpoint { callbackUrl } } } } } }`

const shopifyWebhookCreateMutation = `mutation create($topic: WebhookSubscriptionTopic!, $sub: WebhookSubscriptionInput!) {
  webhookSubscriptionCreate(topic: $topic, webhookSubscription: $sub) { webhookSubscription { id } userErrors { field message } } }`

const shopifyWebhookDeleteMutation = `mutation del($id: ID!) { webhookSubscriptionDelete(id: $id) { userErrors { field message } } }`

// RegisterWebhooks points every topic at callback: subscriptions already on
// it are replaced ("Register again"), others' are left alone.
func (shopifySDKConnector) RegisterWebhooks(ctx context.Context, cred map[string]string, callback string) ([]string, error) {
	var list struct {
		Subs struct {
			Edges []struct {
				Node struct {
					ID       string `json:"id"`
					Topic    string `json:"topic"`
					Endpoint struct {
						CallbackURL string `json:"callbackUrl"`
					} `json:"endpoint"`
				} `json:"node"`
			} `json:"edges"`
		} `json:"webhookSubscriptions"`
	}
	if err := shopifyGQL(ctx, cred, shopifyWebhooksQuery, nil, &list); err != nil {
		return nil, err
	}
	base := strings.SplitN(callback, "?", 2)[0]
	for _, e := range list.Subs.Edges {
		if strings.SplitN(e.Node.Endpoint.CallbackURL, "?", 2)[0] == base {
			if err := shopifyGQL(ctx, cred, shopifyWebhookDeleteMutation, map[string]interface{}{"id": e.Node.ID}, nil); err != nil {
				return nil, err
			}
		}
	}
	done := []string{}
	for _, topic := range ShopifyWebhookTopics {
		var out struct {
			Create struct {
				UserErrors []shopifyUserError `json:"userErrors"`
			} `json:"webhookSubscriptionCreate"`
		}
		if err := shopifyGQL(ctx, cred, shopifyWebhookCreateMutation, map[string]interface{}{"topic": topic,
			"sub": map[string]interface{}{"callbackUrl": callback, "format": "JSON"}}, &out); err != nil {
			return done, err
		}
		if err := shopifyUserErrors(out.Create.UserErrors, "the "+topic+" webhook"); err != nil {
			return done, err
		}
		done = append(done, topic)
	}
	return done, nil
}

// --- 99.3 products ----------------------------------------------------------------

// ShopifyVariantInput is one ERP item as a Shopify variant.
type ShopifyVariantInput struct {
	SKU        string            `json:"sku"`
	Barcode    string            `json:"barcode"`
	Price      float64           `json:"price"`
	CompareAt  float64           `json:"compare_at"` // the MRP, shown struck through
	WeightGram float64           `json:"weight_g"`
	Options    map[string]string `json:"options"` // option name -> value (Colour -> Red)
	VariantGID string            `json:"variant_gid,omitempty"`
}

// ShopifyProductInput is one ERP design (or a single item) as a product.
type ShopifyProductInput struct {
	ProductGID  string // set to update the linked product, never a duplicate
	Title       string
	Description string
	Vendor      string // the brand
	ProductType string
	HSN         string
	Archived    bool
	OptionOrder []string
	Variants    []ShopifyVariantInput
}

// ShopifyVariantLink is a variant as Shopify keeps it.
type ShopifyVariantLink struct {
	SKU             string `json:"sku"`
	Barcode         string `json:"barcode"`
	ProductGID      string `json:"product_gid"`
	VariantGID      string `json:"variant_gid"`
	InventoryItemID string `json:"inventory_item_id"`
	Price           float64
	CompareAt       float64
}

const shopifyProductSetVariantsMutation = `mutation productSet($input: ProductSetInput!) {
  productSet(input: $input, synchronous: true) {
    product { id variants(first: 100) { edges { node { id sku barcode price compareAtPrice inventoryItem { id } } } } }
    userErrors { field message } } }`

func shopifyMoneyString(v float64) string { return strconv.FormatFloat(round2(v), 'f', 2, 64) }

// PublishShopifyProduct creates or updates a product with its options and
// variants (productSet: what is sent is what Shopify keeps - a variant left
// out is removed), and returns each variant's link.
func (shopifySDKConnector) PublishShopifyProduct(ctx context.Context, cred map[string]string, p ShopifyProductInput) (string, []ShopifyVariantLink, error) {
	input := map[string]interface{}{"title": p.Title, "descriptionHtml": p.Description, "status": "ACTIVE"}
	if p.Archived {
		input["status"] = "ARCHIVED"
	}
	if p.ProductGID != "" {
		input["id"] = p.ProductGID
	}
	if p.Vendor != "" {
		input["vendor"] = p.Vendor
	}
	if p.ProductType != "" {
		input["productType"] = p.ProductType
	}
	if p.HSN != "" {
		input["metafields"] = []map[string]interface{}{{"namespace": "custom", "key": "hsn_code", "type": "single_line_text_field", "value": p.HSN}}
	}
	options := p.OptionOrder
	if len(options) > 0 {
		opts := []map[string]interface{}{}
		for _, name := range options {
			seen := map[string]bool{}
			vals := []map[string]interface{}{}
			for _, v := range p.Variants {
				if val := v.Options[name]; val != "" && !seen[val] {
					seen[val] = true
					vals = append(vals, map[string]interface{}{"name": val})
				}
			}
			if len(vals) > 0 {
				opts = append(opts, map[string]interface{}{"name": name, "values": vals})
			}
		}
		input["productOptions"] = opts
	} else {
		input["productOptions"] = []map[string]interface{}{{"name": "Title", "values": []map[string]interface{}{{"name": "Default Title"}}}}
	}
	vars := []map[string]interface{}{}
	for _, v := range p.Variants {
		ov := []map[string]interface{}{}
		if len(options) == 0 {
			ov = append(ov, map[string]interface{}{"optionName": "Title", "name": "Default Title"})
		}
		for _, name := range options {
			if val := v.Options[name]; val != "" {
				ov = append(ov, map[string]interface{}{"optionName": name, "name": val})
			}
		}
		vi := map[string]interface{}{"optionValues": ov, "sku": v.SKU, "price": shopifyMoneyString(v.Price),
			"inventoryItem": map[string]interface{}{"tracked": true}}
		if v.VariantGID != "" {
			vi["id"] = v.VariantGID
		}
		if v.Barcode != "" {
			vi["barcode"] = v.Barcode
		}
		if v.CompareAt > v.Price {
			vi["compareAtPrice"] = shopifyMoneyString(v.CompareAt)
		}
		if v.WeightGram > 0 {
			vi["inventoryItem"] = map[string]interface{}{"tracked": true, "measurement": map[string]interface{}{"weight": map[string]interface{}{"value": v.WeightGram, "unit": "GRAMS"}}}
		}
		vars = append(vars, vi)
	}
	input["variants"] = vars
	var out struct {
		Set struct {
			Product *struct {
				ID       string `json:"id"`
				Variants struct {
					Edges []struct {
						Node struct {
							ID        string `json:"id"`
							SKU       string `json:"sku"`
							Barcode   string `json:"barcode"`
							Price     string `json:"price"`
							CompareAt string `json:"compareAtPrice"`
							Item      struct {
								ID string `json:"id"`
							} `json:"inventoryItem"`
						} `json:"node"`
					} `json:"edges"`
				} `json:"variants"`
			} `json:"product"`
			UserErrors []shopifyUserError `json:"userErrors"`
		} `json:"productSet"`
	}
	if err := shopifyGQL(ctx, cred, shopifyProductSetVariantsMutation, map[string]interface{}{"input": input}, &out); err != nil {
		return "", nil, err
	}
	if err := shopifyUserErrors(out.Set.UserErrors, "the product"); err != nil {
		return "", nil, err
	}
	if out.Set.Product == nil {
		return "", nil, fmt.Errorf("shopify did not return the product")
	}
	links := []ShopifyVariantLink{}
	for _, e := range out.Set.Product.Variants.Edges {
		n := e.Node
		price, _ := strconv.ParseFloat(n.Price, 64)
		cmp, _ := strconv.ParseFloat(n.CompareAt, 64)
		links = append(links, ShopifyVariantLink{SKU: n.SKU, Barcode: n.Barcode, ProductGID: out.Set.Product.ID, VariantGID: n.ID, InventoryItemID: n.Item.ID, Price: price, CompareAt: cmp})
	}
	return out.Set.Product.ID, links, nil
}

const shopifyVariantPricesMutation = `mutation prices($productId: ID!, $variants: [ProductVariantsBulkInput!]!) {
  productVariantsBulkUpdate(productId: $productId, variants: $variants) { userErrors { field message } } }`

// PushVariantPrices sends prices for one product's variants (99.3.3).
func (shopifySDKConnector) PushVariantPrices(ctx context.Context, cred map[string]string, productGID string, variants []ShopifyVariantLink) error {
	vs := make([]map[string]interface{}, 0, len(variants))
	for _, v := range variants {
		vi := map[string]interface{}{"id": v.VariantGID, "price": shopifyMoneyString(v.Price)}
		if v.CompareAt > v.Price {
			vi["compareAtPrice"] = shopifyMoneyString(v.CompareAt)
		} else {
			vi["compareAtPrice"] = nil
		}
		vs = append(vs, vi)
	}
	var out struct {
		Upd struct {
			UserErrors []shopifyUserError `json:"userErrors"`
		} `json:"productVariantsBulkUpdate"`
	}
	if err := shopifyGQL(ctx, cred, shopifyVariantPricesMutation, map[string]interface{}{"productId": productGID, "variants": vs}, &out); err != nil {
		return err
	}
	return shopifyUserErrors(out.Upd.UserErrors, "the prices")
}

const shopifyVariantsPageQuery = `query variants($after: String) { productVariants(first: 250, after: $after) {
  pageInfo { hasNextPage endCursor } edges { node { id sku barcode price compareAtPrice product { id } inventoryItem { id } } } } }`

// ListVariants reads every variant in the shop (99.3.2's "Link existing
// Shopify products" matches them by SKU or barcode). At most 40 pages.
func (shopifySDKConnector) ListVariants(ctx context.Context, cred map[string]string) ([]ShopifyVariantLink, error) {
	out := []ShopifyVariantLink{}
	after := ""
	for page := 0; page < 40; page++ {
		vars := map[string]interface{}{}
		if after != "" {
			vars["after"] = after
		}
		var res struct {
			V struct {
				PageInfo struct {
					HasNext bool   `json:"hasNextPage"`
					End     string `json:"endCursor"`
				} `json:"pageInfo"`
				Edges []struct {
					Node struct {
						ID        string `json:"id"`
						SKU       string `json:"sku"`
						Barcode   string `json:"barcode"`
						Price     string `json:"price"`
						CompareAt string `json:"compareAtPrice"`
						Product   struct {
							ID string `json:"id"`
						} `json:"product"`
						Item struct {
							ID string `json:"id"`
						} `json:"inventoryItem"`
					} `json:"node"`
				} `json:"edges"`
			} `json:"productVariants"`
		}
		if err := shopifyGQL(ctx, cred, shopifyVariantsPageQuery, vars, &res); err != nil {
			return nil, err
		}
		for _, e := range res.V.Edges {
			n := e.Node
			price, _ := strconv.ParseFloat(n.Price, 64)
			cmp, _ := strconv.ParseFloat(n.CompareAt, 64)
			out = append(out, ShopifyVariantLink{SKU: n.SKU, Barcode: n.Barcode, ProductGID: n.Product.ID, VariantGID: n.ID, InventoryItemID: n.Item.ID, Price: price, CompareAt: cmp})
		}
		if !res.V.PageInfo.HasNext {
			break
		}
		after = res.V.PageInfo.End
	}
	return out, nil
}

// --- 99.7 payouts -------------------------------------------------------------------

// ShopifyPayout is one Shopify Payments payout and the orders in it.
type ShopifyPayout struct {
	ID       string              `json:"id"`
	IssuedAt time.Time           `json:"issued_at"`
	Status   string              `json:"status"`
	Net      float64             `json:"net"`
	Lines    []ShopifyPayoutLine `json:"lines"`
}

// ShopifyPayoutLine is one balance transaction: an order's charge or refund,
// with Shopify's fee.
type ShopifyPayoutLine struct {
	Type    string  `json:"type"`
	OrderID string  `json:"order_id"` // numeric
	Order   string  `json:"order"`    // "#1001"
	Amount  float64 `json:"amount"`
	Fee     float64 `json:"fee"`
	Net     float64 `json:"net"`
}

const shopifyPayoutsQuery = `query payouts($after: String) { shopifyPaymentsAccount { payouts(first: 20, after: $after, reverse: true) {
  pageInfo { hasNextPage endCursor } edges { node { id legacyResourceId issuedAt status net { amount } } } } } }`

const shopifyPayoutLinesQuery = `query lines($q: String!, $after: String) { shopifyPaymentsAccount { balanceTransactions(first: 100, after: $after, query: $q) {
  pageInfo { hasNextPage endCursor } edges { node { type amount { amount } fee { amount } net { amount } associatedOrder { id name } } } } } }`

type shopifyMoneyV2 struct {
	Amount string `json:"amount"`
}

func (m shopifyMoneyV2) f() float64 {
	v, _ := strconv.ParseFloat(m.Amount, 64)
	return v
}

// ReadPayouts reads payouts issued since (newest first, at most 60) with
// their transactions.
func (shopifySDKConnector) ReadPayouts(ctx context.Context, cred map[string]string, since time.Time) ([]ShopifyPayout, error) {
	out := []ShopifyPayout{}
	after := ""
	for page := 0; page < 3; page++ {
		vars := map[string]interface{}{}
		if after != "" {
			vars["after"] = after
		}
		var res struct {
			Acct *struct {
				Payouts struct {
					PageInfo struct {
						HasNext bool   `json:"hasNextPage"`
						End     string `json:"endCursor"`
					} `json:"pageInfo"`
					Edges []struct {
						Node struct {
							ID       string         `json:"id"`
							Legacy   string         `json:"legacyResourceId"`
							IssuedAt time.Time      `json:"issuedAt"`
							Status   string         `json:"status"`
							Net      shopifyMoneyV2 `json:"net"`
						} `json:"node"`
					} `json:"edges"`
				} `json:"payouts"`
			} `json:"shopifyPaymentsAccount"`
		}
		if err := shopifyGQL(ctx, cred, shopifyPayoutsQuery, vars, &res); err != nil {
			return nil, err
		}
		if res.Acct == nil {
			return out, nil // the shop does not use Shopify Payments
		}
		stop := false
		for _, e := range res.Acct.Payouts.Edges {
			n := e.Node
			if !since.IsZero() && n.IssuedAt.Before(since) {
				stop = true
				continue
			}
			out = append(out, ShopifyPayout{ID: firstNonEmpty(n.Legacy, shopifyGIDNumber(n.ID)), IssuedAt: n.IssuedAt, Status: strings.ToLower(n.Status), Net: n.Net.f()})
		}
		if stop || !res.Acct.Payouts.PageInfo.HasNext {
			break
		}
		after = res.Acct.Payouts.PageInfo.End
	}
	for i := range out {
		after := ""
		for page := 0; page < 10; page++ {
			vars := map[string]interface{}{"q": "payout_id:" + out[i].ID}
			if after != "" {
				vars["after"] = after
			}
			var res struct {
				Acct *struct {
					Tx struct {
						PageInfo struct {
							HasNext bool   `json:"hasNextPage"`
							End     string `json:"endCursor"`
						} `json:"pageInfo"`
						Edges []struct {
							Node struct {
								Type   string         `json:"type"`
								Amount shopifyMoneyV2 `json:"amount"`
								Fee    shopifyMoneyV2 `json:"fee"`
								Net    shopifyMoneyV2 `json:"net"`
								Order  *struct {
									ID   string `json:"id"`
									Name string `json:"name"`
								} `json:"associatedOrder"`
							} `json:"node"`
						} `json:"edges"`
					} `json:"balanceTransactions"`
				} `json:"shopifyPaymentsAccount"`
			}
			if err := shopifyGQL(ctx, cred, shopifyPayoutLinesQuery, vars, &res); err != nil {
				return nil, err
			}
			if res.Acct == nil {
				break
			}
			for _, e := range res.Acct.Tx.Edges {
				n := e.Node
				l := ShopifyPayoutLine{Type: strings.ToLower(n.Type), Amount: n.Amount.f(), Fee: n.Fee.f(), Net: n.Net.f()}
				if n.Order != nil {
					l.OrderID, l.Order = shopifyGIDNumber(n.Order.ID), n.Order.Name
				}
				out[i].Lines = append(out[i].Lines, l)
			}
			if !res.Acct.Tx.PageInfo.HasNext {
				break
			}
			after = res.Acct.Tx.PageInfo.End
		}
	}
	sort.SliceStable(out, func(a, b int) bool { return out[a].IssuedAt.Before(out[b].IssuedAt) })
	return out, nil
}

const shopifyAssignedLocationQuery = `query loc($id: ID!) { order(id: $id) { fulfillmentOrders(first: 5) { edges { node { assignedLocation { location { id } } } } } } }`

// AssignedLocation is the Shopify location Shopify chose to fulfil an order
// from (D5: the ERP store mapped to it ships it), "" if none.
func (shopifySDKConnector) AssignedLocation(ctx context.Context, cred map[string]string, orderID string) (string, error) {
	var out struct {
		Order *struct {
			FOs struct {
				Edges []struct {
					Node struct {
						Assigned struct {
							Location *struct {
								ID string `json:"id"`
							} `json:"location"`
						} `json:"assignedLocation"`
					} `json:"node"`
				} `json:"edges"`
			} `json:"fulfillmentOrders"`
		} `json:"order"`
	}
	if err := shopifyGQL(ctx, cred, shopifyAssignedLocationQuery, map[string]interface{}{"id": shopifyOrderGID(orderID)}, &out); err != nil {
		return "", err
	}
	if out.Order != nil {
		for _, e := range out.Order.FOs.Edges {
			if e.Node.Assigned.Location != nil && e.Node.Assigned.Location.ID != "" {
				return e.Node.Assigned.Location.ID, nil
			}
		}
	}
	return "", nil
}

const shopifyCreateMediaIDsMutation = `mutation media($productId: ID!, $media: [CreateMediaInput!]!) {
  productCreateMedia(productId: $productId, media: $media) { media { id } mediaUserErrors { field message } } }`

// UploadProductImages stages images and attaches them to a product,
// returning the new media ids in the images' order ("" for one that failed).
func (shopifySDKConnector) UploadProductImages(ctx context.Context, cred map[string]string, productGID string, images []ChannelImage) ([]string, error) {
	ids := make([]string, len(images))
	if len(images) == 0 {
		return ids, nil
	}
	staged := make([]map[string]interface{}, len(images))
	for i, img := range images {
		staged[i] = map[string]interface{}{"filename": img.Filename, "mimeType": img.MIMEType, "httpMethod": "POST", "resource": "IMAGE", "fileSize": strconv.Itoa(len(img.Bytes))}
	}
	var st struct {
		Create struct {
			Targets []struct {
				URL         string                     `json:"url"`
				ResourceURL string                     `json:"resourceUrl"`
				Parameters  []shopifyStagedTargetParam `json:"parameters"`
			} `json:"stagedTargets"`
			UserErrors []shopifyUserError `json:"userErrors"`
		} `json:"stagedUploadsCreate"`
	}
	if err := shopifyGQL(ctx, cred, shopifyStagedUploadsCreateMutation, map[string]interface{}{"input": staged}, &st); err != nil {
		return ids, err
	}
	if err := shopifyUserErrors(st.Create.UserErrors, "the picture upload"); err != nil {
		return ids, err
	}
	if len(st.Create.Targets) != len(images) {
		return ids, fmt.Errorf("shopify staged %d of %d pictures", len(st.Create.Targets), len(images))
	}
	media := []map[string]interface{}{}
	order := []int{}
	for i, img := range images {
		t := st.Create.Targets[i]
		if err := putStagedUpload(ctx, t.URL, t.Parameters, img); err != nil {
			continue
		}
		media = append(media, map[string]interface{}{"originalSource": t.ResourceURL, "mediaContentType": "IMAGE", "alt": img.Filename})
		order = append(order, i)
	}
	if len(media) == 0 {
		return ids, fmt.Errorf("no picture could be uploaded")
	}
	var out struct {
		Create struct {
			Media []struct {
				ID string `json:"id"`
			} `json:"media"`
			Errors []shopifyUserError `json:"mediaUserErrors"`
		} `json:"productCreateMedia"`
	}
	if err := shopifyGQL(ctx, cred, shopifyCreateMediaIDsMutation, map[string]interface{}{"productId": productGID, "media": media}, &out); err != nil {
		return ids, err
	}
	for k, m := range out.Create.Media {
		if k < len(order) {
			ids[order[k]] = m.ID
		}
	}
	return ids, shopifyUserErrors(out.Create.Errors, "a picture")
}

const shopifyVariantMediaMutation = `mutation vm($productId: ID!, $variants: [ProductVariantsBulkInput!]!) {
  productVariantsBulkUpdate(productId: $productId, variants: $variants) { userErrors { field message } } }`

// AttachVariantImages gives variants their own picture (variant gid ->
// media id). Shopify may still be processing a new picture; a refusal is
// returned for the caller to note, never fatal to the publish.
func (shopifySDKConnector) AttachVariantImages(ctx context.Context, cred map[string]string, productGID string, media map[string]string) error {
	vs := []map[string]interface{}{}
	for variant, mediaID := range media {
		if variant != "" && mediaID != "" {
			vs = append(vs, map[string]interface{}{"id": variant, "mediaId": mediaID})
		}
	}
	if len(vs) == 0 {
		return nil
	}
	var out struct {
		Upd struct {
			UserErrors []shopifyUserError `json:"userErrors"`
		} `json:"productVariantsBulkUpdate"`
	}
	if err := shopifyGQL(ctx, cred, shopifyVariantMediaMutation, map[string]interface{}{"productId": productGID, "variants": vs}, &out); err != nil {
		return err
	}
	return shopifyUserErrors(out.Upd.UserErrors, "the variant pictures")
}
