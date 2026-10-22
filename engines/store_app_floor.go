package engines

import (
	"custom_erp/db"
	"encoding/json"
	"fmt"
	"strings"
)

// Stage 102.4.3 - order from another store. A size this store does not have
// is ordered on the phone from another store or the warehouse and delivered to
// the customer's home. It is an ordinary OMS order made by the Order Engine's
// own creation path (CreateSalesOrder - the only one since Stage 35.1): the
// same SKU / address / payment checks, the same sourcing and reservation, and
// it shows on Pending Orders, Pick, Pack and Ship like any other. Keyed in by
// staff, it skips Pending (88.3.1 D12). The phone's request key is the order's
// channel reference, so a retried tap returns the order already made.
//
// 102.4.2 (basket to the till) needs no code here: the phone's basket is a held
// bill (73.2.5, POST /api/v1/pos/held-bills) that the till resumes.

// StoreAppOrderChannel is the OMS channel these orders carry.
const StoreAppOrderChannel = "Store app"

// StoreAppOrderLine is one item ordered.
type StoreAppOrderLine struct {
	Code string `json:"code"`
	Qty  int    `json:"qty"`
}

// StoreAppOrderInput is the phone's order form.
type StoreAppOrderInput struct {
	RequestKey    string              `json:"request_key"`
	CustomerName  string              `json:"customer_name"`
	CustomerPhone string              `json:"customer_phone"`
	Address       string              `json:"address"`
	AddressLine2  string              `json:"address_line2"`
	City          string              `json:"city"`
	State         string              `json:"state"`
	Pincode       string              `json:"pincode"`
	Payment       string              `json:"payment"` // paid | cod
	FromStore     string              `json:"from_store"`
	Lines         []StoreAppOrderLine `json:"lines"`
	Note          string              `json:"note"`
}

// StoreAppOrderResult is the order made (or found, on a retry).
type StoreAppOrderResult struct {
	OrderID    string  `json:"order_id"`
	Existing   bool    `json:"existing"`
	Status     string  `json:"status"`
	HoldReason string  `json:"hold_reason,omitempty"`
	FromStore  string  `json:"from_store,omitempty"`
	Total      float64 `json:"total"`
	Message    string  `json:"message"`
}

// StoreAppCreateOrder books a home-delivery order from the phone.
func StoreAppCreateOrder(tenantID, userID, username, store string, in StoreAppOrderInput) (*StoreAppOrderResult, error) {
	store, err := storeAppStoreFor(tenantID, userID, store)
	if err != nil {
		return nil, err
	}
	key := strings.TrimSpace(in.RequestKey)
	if key == "" || len(key) > 80 {
		return nil, &ValidationError{Code: "GLOBAL-0002", SubFor: "request_key", Message: "the order has no id - update the app and try again"}
	}
	name, phone := strings.TrimSpace(in.CustomerName), strings.TrimSpace(in.CustomerPhone)
	if name == "" || phone == "" {
		return nil, &ValidationError{Code: "GLOBAL-0002", SubFor: "customer_name", Message: "the customer's name and phone number are needed for delivery"}
	}
	addr, pin := strings.TrimSpace(in.Address), strings.TrimSpace(in.Pincode)
	if addr == "" || !pincodeShapeRe.MatchString(pin) {
		return nil, &ValidationError{Code: "GLOBAL-0002", SubFor: "pincode", Message: "the delivery address and its PIN code are needed"}
	}
	payment := "COD"
	switch strings.ToLower(strings.TrimSpace(in.Payment)) {
	case "paid":
		payment = "Confirmed"
	case "cod", "":
	default:
		return nil, &ValidationError{Code: "GLOBAL-0002", SubFor: "payment", Message: "payment must be paid or cod"}
	}
	ref := store + ":" + key
	if id, _ := FindSalesOrderByChannelRef(tenantID, StoreAppOrderChannel, ref); id != "" {
		return storeAppOrderResult(tenantID, id, true)
	}

	var lines []SalesOrderLineInput
	for _, l := range in.Lines {
		if l.Qty <= 0 {
			continue
		}
		scan, err := storeAppResolveCode(tenantID, l.Code)
		if err != nil {
			return nil, &ValidationError{Code: "GLOBAL-0002", SubFor: "code", Message: fmt.Sprintf("%s is not an item", l.Code)}
		}
		item, err := ResolveItemBySKU(tenantID, scan.SKU)
		if err != nil {
			return nil, err
		}
		price, _ := storeAppReferencePrice(tenantID, item.Data, scan.SKU)
		lines = append(lines, SalesOrderLineInput{SKU: scan.SKU, Qty: l.Qty, UnitPrice: price})
	}
	if len(lines) == 0 {
		return nil, &ValidationError{Code: "GLOBAL-0002", SubFor: "lines", Message: "add the item to order"}
	}
	from := strings.TrimSpace(in.FromStore)
	if from == store {
		from = ""
	}
	by := firstNonEmpty(username, userID)
	orderID, err := CreateSalesOrder(tenantID, SalesOrderInput{
		Channel: StoreAppOrderChannel, ChannelOrderID: ref, CustomerName: name, CustomerPhone: phone,
		ShippingAddress: addr, ShippingLine2: strings.TrimSpace(in.AddressLine2), ShippingCity: strings.TrimSpace(in.City),
		ShippingState: strings.TrimSpace(in.State), ShippingPincode: pin, PaymentStatus: payment,
		PreferredLocation: from, Lines: lines, SkipPending: true, AcceptedBy: by,
	})
	if err != nil {
		return nil, err
	}
	note := strings.TrimSpace(in.Note)
	if len(note) > 300 {
		note = note[:300]
	}
	if schema, err := db.GetTenantSchema(tenantID); err == nil {
		patch, _ := json.Marshal(map[string]interface{}{"booked_at_store": store, "booked_by": by, "store_app_note": note})
		_, _ = db.DB.Exec(fmt.Sprintf(`UPDATE %s.documents SET data = data || $2::jsonb WHERE doctype = 'SalesOrder' AND id = $1`, schema), orderID, patch)
	}
	LogAuditEvent(tenantID, by, "STORE_APP_ORDER", "CREATED", fmt.Sprintf("Order %s booked at %s for home delivery (%d line(s))", orderID, store, len(lines)))
	return storeAppOrderResult(tenantID, orderID, false)
}

func storeAppOrderResult(tenantID, orderID string, existing bool) (*StoreAppOrderResult, error) {
	data, _, err := fetchDocData(tenantID, "SalesOrder", orderID)
	if err != nil {
		return nil, err
	}
	out := &StoreAppOrderResult{OrderID: orderID, Existing: existing, Status: strField(data, "order_status"), HoldReason: strField(data, "hold_reason"),
		Total: round2(numFromInterface(data["total_amount"]))}
	if schema, err := db.GetTenantSchema(tenantID); err == nil {
		_ = db.DB.QueryRow(fmt.Sprintf(`SELECT COALESCE(MIN(NULLIF(data->>'location_code', '')), '') FROM %s.documents WHERE doctype = 'SalesOrderLine' AND data->>'order_id' = $1`, schema), orderID).Scan(&out.FromStore)
	}
	switch {
	case existing:
		out.Message = fmt.Sprintf("Order %s was already made for this request.", orderID)
	case out.HoldReason == HoldAllocationFailed:
		out.Message = fmt.Sprintf("Order %s is made but on hold: no store or warehouse has all of it. The order desk will source it.", orderID)
	case out.HoldReason != "":
		out.Message = fmt.Sprintf("Order %s is made but on hold (%s). The order desk will sort it out.", orderID, strings.ToLower(strings.ReplaceAll(out.HoldReason, "_", " ")))
	default:
		out.Message = fmt.Sprintf("Order %s is booked for home delivery.", orderID)
	}
	return out, nil
}
