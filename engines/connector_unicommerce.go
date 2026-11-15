package engines

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"
)

// Item 146.4.1 - Unicommerce (Uniware) connector. Written against the public
// API documentation at documentation.unicommerce.com (OAuth, Search Sale Order,
// Get Sale Order, Adjust Inventory (Multiple), Get Invoice Details), read
// 2026-11-15. Our OMS stays the order system; this serves clients who run
// Unicommerce. Uniware itself picks, packs, invoices and dispatches, so status
// is read (UnicommerceOrderStatus) rather than pushed, and the invoice is the
// one Uniware issued (invoice.displayCode). Nothing here has met a live Uniware
// tenant: it needs the client's own Uniware login (an admin user with access
// to the facilities).

func init() { registerOmnichannelConnector(unicommerceConnector{}) }

type unicommerceConnector struct{}

func (unicommerceConnector) Descriptor() ConnectorDescriptor {
	return ConnectorDescriptor{Code: "Unicommerce", Kind: "marketplace", PullOrders: true, PushInventory: true, RequiresLocation: true}
}
func (unicommerceConnector) RateLimit() (int, time.Duration) { return 20, time.Minute }
func (unicommerceConnector) MapError(err error) string       { return DefaultConnectorErrorCode(err) }

func (unicommerceConnector) ValidateCredentials(c map[string]string) error {
	if c["base_url"] == "" && c["tenant"] == "" {
		return fmt.Errorf("Unicommerce credential needs tenant (the part before .unicommerce.com)")
	}
	if c["access_token"] == "" && (c["username"] == "" || c["password"] == "") {
		return fmt.Errorf("Unicommerce credential needs username and password of a Uniware admin user, or an access_token")
	}
	if len(unicommerceFacilities(c)) == 0 {
		return fmt.Errorf("Unicommerce credential needs facility_code (one or more Uniware facility codes, comma separated)")
	}
	return nil
}

func unicommerceFirst(vals ...string) string {
	for _, v := range vals {
		if v != "" {
			return v
		}
	}
	return ""
}

func unicommerceBase(c map[string]string) string {
	if c["base_url"] != "" {
		return strings.TrimRight(c["base_url"], "/")
	}
	return "https://" + strings.TrimSpace(c["tenant"]) + ".unicommerce.com"
}

func unicommerceFacilities(c map[string]string) []string {
	var out []string
	for _, f := range strings.Split(c["facility_code"], ",") {
		if f = strings.TrimSpace(f); f != "" {
			out = append(out, f)
		}
	}
	return out
}

// unicommerceToken uses the documented password grant:
// GET /oauth/token?grant_type=password&client_id=...&username=...&password=...
func unicommerceToken(ctx context.Context, c map[string]string) (string, error) {
	if c["access_token"] != "" {
		return c["access_token"], nil
	}
	q := url.Values{
		"grant_type": {"password"},
		"client_id":  {unicommerceFirst(c["client_id"], "my-trusted-client")},
		"username":   {c["username"]},
		"password":   {c["password"]},
	}
	var resp struct {
		AccessToken string `json:"access_token"`
	}
	if err := connectorJSONRequest(ctx, "Unicommerce", http.MethodGet, unicommerceBase(c)+"/oauth/token?"+q.Encode(), nil, nil, &resp); err != nil {
		return "", err
	}
	if resp.AccessToken == "" {
		return "", fmt.Errorf("Unicommerce returned no access token")
	}
	return resp.AccessToken, nil
}

type unicommerceEnvelope struct {
	Successful bool   `json:"successful"`
	Message    string `json:"message"`
	Errors     []struct {
		Code        string `json:"code"`
		Description string `json:"description"`
		Message     string `json:"message"`
	} `json:"errors"`
}

// refusal turns a 200 response with successful=false into an error: Uniware
// reports rejections inside the body, not by HTTP status.
func (e unicommerceEnvelope) refusal(what string) error {
	if e.Successful {
		return nil
	}
	detail := e.Message
	if len(e.Errors) > 0 {
		detail = strings.TrimSpace(e.Errors[0].Code + " " + unicommerceFirst(e.Errors[0].Description, e.Errors[0].Message))
	}
	return fmt.Errorf("Unicommerce %s was refused: %s", what, detail)
}

func unicommerceHeaders(token, facility string) map[string]string {
	h := map[string]string{"Authorization": "bearer " + token, "Accept": "application/json"}
	if facility != "" {
		h["Facility"] = facility
	}
	return h
}

// PullOrders searches sale orders updated since the last pull at the
// configured facilities, then reads each one for its items and address. The
// cursor is the search offset (displayStart).
func (u unicommerceConnector) PullOrders(ctx context.Context, c map[string]string, req ConnectorPullRequest) (ConnectorOrderPage, error) {
	token, err := unicommerceToken(ctx, c)
	if err != nil {
		return ConnectorOrderPage{}, err
	}
	limit := req.Limit
	if limit <= 0 || limit > 50 {
		limit = 50
	}
	start, _ := strconv.Atoi(req.Cursor)
	body := map[string]interface{}{
		"dateType":      "UPDATED",
		"facilityCodes": unicommerceFacilities(c),
		"searchOptions": map[string]interface{}{"displayStart": start, "displayLength": limit, "getCount": true},
	}
	if !req.UpdatedAfter.IsZero() {
		body["fromDate"] = req.UpdatedAfter.UTC().Format("2006-01-02T15:04:05.000Z")
		body["toDate"] = time.Now().UTC().Format("2006-01-02T15:04:05.000Z")
	}
	var found struct {
		unicommerceEnvelope
		TotalRecords int `json:"totalRecords"`
		Elements     []struct {
			Code   string `json:"code"`
			Status string `json:"status"`
		} `json:"elements"`
	}
	if err := connectorJSONRequest(ctx, "Unicommerce", http.MethodPost, unicommerceBase(c)+"/services/rest/v1/oms/saleOrder/search", unicommerceHeaders(token, ""), body, &found); err != nil {
		return ConnectorOrderPage{}, err
	}
	if err := found.refusal("order search"); err != nil {
		return ConnectorOrderPage{}, err
	}
	page := ConnectorOrderPage{}
	if len(found.Elements) > 0 && start+len(found.Elements) < found.TotalRecords {
		page.NextCursor = strconv.Itoa(start + len(found.Elements))
	}
	for _, el := range found.Elements {
		// A cancelled order never ships, one still PENDING_VERIFICATION is not
		// confirmed by Uniware, and a COMPLETE one has already shipped.
		switch strings.ToUpper(el.Status) {
		case "CANCELLED", "PENDING_VERIFICATION", "COMPLETE":
			continue
		}
		order, err := u.readOrder(ctx, c, token, el.Code)
		if err != nil {
			return ConnectorOrderPage{}, err
		}
		if len(order.Lines) > 0 {
			page.Orders = append(page.Orders, order)
		}
	}
	return page, nil
}

type unicommerceSaleOrder struct {
	unicommerceEnvelope
	SaleOrderDTO struct {
		Code      string `json:"code"`
		Status    string `json:"status"`
		COD       bool   `json:"cod"`
		Addresses []struct {
			Name         string `json:"name"`
			AddressLine1 string `json:"addressLine1"`
			AddressLine2 string `json:"addressLine2"`
			City         string `json:"city"`
			State        string `json:"state"`
			Pincode      string `json:"pincode"`
			Phone        string `json:"phone"`
		} `json:"addresses"`
		SaleOrderItems []struct {
			Code                string          `json:"code"`
			ItemSku             string          `json:"itemSku"`
			ShippingPackageCode string          `json:"shippingPackageCode"`
			FacilityCode        string          `json:"facilityCode"`
			SellingPrice        json.RawMessage `json:"sellingPrice"`
			StatusCode          string          `json:"statusCode"`
		} `json:"saleOrderItems"`
	} `json:"saleOrderDTO"`
}

func unicommerceGetOrder(ctx context.Context, c map[string]string, token, code string) (unicommerceSaleOrder, error) {
	var resp unicommerceSaleOrder
	body := map[string]interface{}{"code": code, "facilityCodes": unicommerceFacilities(c)}
	if err := connectorJSONRequest(ctx, "Unicommerce", http.MethodPost, unicommerceBase(c)+"/services/rest/v1/oms/saleorder/get", unicommerceHeaders(token, ""), body, &resp); err != nil {
		return resp, err
	}
	return resp, resp.refusal("order read")
}

func unicommerceNumber(raw json.RawMessage) float64 {
	var f float64
	if json.Unmarshal(raw, &f) == nil {
		return f
	}
	var s string
	if json.Unmarshal(raw, &s) == nil {
		f, _ = strconv.ParseFloat(s, 64)
	}
	return f
}

// readOrder turns one Uniware sale order into the SDK order. Uniware lists one
// sale order item per unit, so units of one SKU at one price are summed; the
// order lands at the facility of its first live item (the ERP does not split a
// Uniware order that Uniware routed to several facilities).
func (unicommerceConnector) readOrder(ctx context.Context, c map[string]string, token, code string) (ConnectorOrder, error) {
	resp, err := unicommerceGetOrder(ctx, c, token, code)
	if err != nil {
		return ConnectorOrder{}, err
	}
	d := resp.SaleOrderDTO
	o := ConnectorOrder{ChannelOrderID: unicommerceFirst(d.Code, code), PaymentStatus: "Confirmed"}
	if d.COD {
		o.PaymentStatus = "COD"
	}
	if len(d.Addresses) > 0 {
		a := d.Addresses[0]
		o.CustomerName, o.CustomerPhone, o.ShippingState = a.Name, a.Phone, a.State
		o.ShippingAddress = strings.Join(strings.Fields(strings.Join([]string{a.AddressLine1, a.AddressLine2, a.City, a.State, a.Pincode}, " ")), " ")
	}
	type key struct {
		sku   string
		price float64
	}
	counts := map[key]int{}
	var order []key
	for _, it := range d.SaleOrderItems {
		if strings.EqualFold(it.StatusCode, "CANCELLED") || it.ItemSku == "" {
			continue
		}
		if o.LocationCode == "" {
			o.LocationCode = it.FacilityCode
		}
		k := key{it.ItemSku, unicommerceNumber(it.SellingPrice)}
		if counts[k] == 0 {
			order = append(order, k)
		}
		counts[k]++
	}
	for _, k := range order {
		o.Lines = append(o.Lines, ConnectorOrderLine{ChannelSKU: k.sku, Quantity: counts[k], UnitPrice: k.price})
	}
	// Once Uniware has invoiced a package, the ERP books the sale under that
	// number. Not invoiced yet (or unreadable) just leaves it empty for now.
	for _, it := range d.SaleOrderItems {
		if it.ShippingPackageCode == "" || strings.EqualFold(it.StatusCode, "CANCELLED") {
			continue
		}
		if code, display, err := unicommerceInvoice(ctx, c, token, it.FacilityCode, it.ShippingPackageCode); err == nil && (code != "" || display != "") {
			o.MarketplaceInvoiceNo = unicommerceFirst(display, code)
			break
		}
	}
	return o, nil
}

// PushInventory sets the stock of each SKU at its facility with the documented
// bulk adjustment (REPLACE on one shelf). One call per facility, because the
// API wants the facility code in the Facility header.
func (unicommerceConnector) PushInventory(ctx context.Context, c map[string]string, updates []ConnectorInventoryUpdate) error {
	if c["shelf_code"] == "" {
		return fmt.Errorf("Unicommerce stock push needs shelf_code (the Uniware shelf the stock sits on)")
	}
	facilities := unicommerceFacilities(c)
	if len(facilities) == 0 {
		return fmt.Errorf("Unicommerce stock push needs facility_code")
	}
	token, err := unicommerceToken(ctx, c)
	if err != nil {
		return err
	}
	byFacility := map[string][]map[string]interface{}{}
	for _, up := range updates {
		facility := unicommerceFirst(up.LocationCode, facilities[0])
		byFacility[facility] = append(byFacility[facility], map[string]interface{}{
			"itemSKU": up.ChannelSKU, "quantity": up.Quantity, "shelfCode": c["shelf_code"],
			"inventoryType": "GOOD_INVENTORY", "adjustmentType": "REPLACE", "facilityCode": facility,
			"remarks": "Stock from ERP",
		})
	}
	names := make([]string, 0, len(byFacility))
	for f := range byFacility {
		names = append(names, f)
	}
	sort.Strings(names)
	for _, f := range names {
		var resp struct {
			unicommerceEnvelope
		}
		payload := map[string]interface{}{"inventoryAdjustments": byFacility[f], "forceAllocate": false}
		if err := connectorJSONRequest(ctx, "Unicommerce", http.MethodPost, unicommerceBase(c)+"/services/rest/v1/inventory/adjust/bulk", unicommerceHeaders(token, f), payload, &resp); err != nil {
			return err
		}
		if err := resp.refusal("stock update at " + f); err != nil {
			return err
		}
	}
	return nil
}

// PushOrderStatus: Uniware owns pick, pack, invoice and dispatch, so there is
// nothing to push back; status is read with UnicommerceOrderStatus.
func (unicommerceConnector) PushOrderStatus(context.Context, map[string]string, ConnectorStatusUpdate) error {
	return ErrOrderDecisionPushUnsupported
}

func (unicommerceConnector) PublishProduct(context.Context, map[string]string, ChannelProductPayload) (string, error) {
	return "", fmt.Errorf("Unicommerce items are created in Uniware; the ERP only maps its SKUs")
}

// UnicommerceOrderState is what Uniware says about one order: its status and,
// per shipping package, the invoice Uniware issued (the number the ERP books).
type UnicommerceOrderState struct {
	Code     string                   `json:"code"`
	Status   string                   `json:"status"`
	Packages []UnicommercePackageInfo `json:"packages"`
}

// UnicommercePackageInfo is one shipping package of an order.
type UnicommercePackageInfo struct {
	ShippingPackageCode string `json:"shipping_package_code"`
	FacilityCode        string `json:"facility_code"`
	InvoiceCode         string `json:"invoice_code"`
	InvoiceDisplayCode  string `json:"invoice_display_code"`
	ItemStatus          string `json:"item_status"`
}

// UnicommerceOrderStatus reads the order, then the invoice of each shipping
// package (Get Invoice Details), so the ERP can book the sale under
// Unicommerce's own invoice number.
func UnicommerceOrderStatus(ctx context.Context, c map[string]string, code string) (UnicommerceOrderState, error) {
	token, err := unicommerceToken(ctx, c)
	if err != nil {
		return UnicommerceOrderState{}, err
	}
	resp, err := unicommerceGetOrder(ctx, c, token, code)
	if err != nil {
		return UnicommerceOrderState{}, err
	}
	state := UnicommerceOrderState{Code: resp.SaleOrderDTO.Code, Status: resp.SaleOrderDTO.Status}
	seen := map[string]bool{}
	for _, it := range resp.SaleOrderDTO.SaleOrderItems {
		if it.ShippingPackageCode == "" || seen[it.ShippingPackageCode] {
			continue
		}
		seen[it.ShippingPackageCode] = true
		info := UnicommercePackageInfo{ShippingPackageCode: it.ShippingPackageCode, FacilityCode: it.FacilityCode, ItemStatus: it.StatusCode}
		code, display, err := unicommerceInvoice(ctx, c, token, it.FacilityCode, it.ShippingPackageCode)
		if err != nil {
			return state, err
		}
		info.InvoiceCode, info.InvoiceDisplayCode = code, display
		state.Packages = append(state.Packages, info)
	}
	return state, nil
}

// unicommerceInvoice reads one package's invoice (Get Invoice Details). A
// refusal, such as "not invoiced yet", gives empty codes and no error.
func unicommerceInvoice(ctx context.Context, c map[string]string, token, facility, pkg string) (code, display string, err error) {
	var inv struct {
		unicommerceEnvelope
		Invoice struct {
			Code        string `json:"code"`
			DisplayCode string `json:"displayCode"`
		} `json:"invoice"`
	}
	if err = connectorJSONRequest(ctx, "Unicommerce", http.MethodPost, unicommerceBase(c)+"/services/rest/v1/invoice/details/get",
		unicommerceHeaders(token, facility), map[string]interface{}{"shippingPackageCode": pkg}, &inv); err != nil {
		return "", "", err
	}
	if inv.refusal("invoice read") != nil {
		return "", "", nil
	}
	return inv.Invoice.Code, inv.Invoice.DisplayCode, nil
}
