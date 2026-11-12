package engines

import (
	"custom_erp/db"
	"database/sql"
	"encoding/json"
	"fmt"
	"math"
	"strings"
)

// Stage 148.36: custom-order configurator (module custom_orders, off by default).
//
// A made-to-order piece is priced from the day's metal rate:
//
//	price = weight x the day's rate for the metal and purity
//	      + the making charge of the chosen making type
//	      + the setting and stone lines the rules add
//
// Every part is shown, and the saved quote keeps the rate used, so a later rate
// never reprices it. The metal rate, the choices and the rules are small masters
// (doctypes MetalRate, CustomChoice, CustomOrderRule). With no rate for today the
// order is refused. Saving creates the Item (the caller saves it through the
// normal item save, so its ID is the next Item number, never built from the
// choices) and a Draft sales order for a person to submit; no stock is reserved.
//
// The jewellery fields already on the Item (metal_type, purity_karat, stone_type,
// size, net_weight) are reused when the tenant's Item form has them.

const (
	ModuleCustomOrders = "custom_orders"

	MetalRateDoctype        = "MetalRate"
	CustomChoiceDoctype     = "CustomChoice"
	CustomOrderRuleDoctype  = "CustomOrderRule"
	CustomOrderQuoteDoctype = "CustomOrderQuote"

	coKindMetal  = "Metal"
	coKindPurity = "Purity"
	coKindStone  = "Stone"
	coKindSize   = "Size"
	coKindMaking = "Making type"

	coBasisFlat     = "Flat"
	coBasisPerGram  = "Per gram"
	coBasisPerStone = "Per stone"
	coBasisPercent  = "% of metal"

	coOffCode      = "SALESP-0760"
	coNoRateCode   = "SALESP-0761"
	coChoiceCode   = "SALESP-0762"
	coWeightCode   = "SALESP-0763"
	coMakingCode   = "SALESP-0764"
	coTaxCode      = "SALESP-0765"
	coCustomerCode = "SALESP-0766"
	coItemCode     = "SALESP-0767"
)

// moduleGatedDoctypes hide a whole doctype until its module is on (the module's
// own gate refuses the generic routes; this keeps the menu clean as well).
var moduleGatedDoctypes = map[string]string{
	MetalRateDoctype: ModuleCustomOrders, CustomChoiceDoctype: ModuleCustomOrders,
	CustomOrderRuleDoctype: ModuleCustomOrders, CustomOrderQuoteDoctype: ModuleCustomOrders,
}

func coErr(code, msg string) error { return &ValidationError{Code: code, Message: msg} }

// CustomOrderInput is what the configurator sends.
type CustomOrderInput struct {
	Metal      string  `json:"metal"`
	Purity     string  `json:"purity"`
	NetWeight  float64 `json:"net_weight"`
	Stone      string  `json:"stone"`
	StoneCount int     `json:"stone_count"`
	Size       string  `json:"size"`
	MakingType string  `json:"making_type"`
}

// CustomOrderLine is one part of the price.
type CustomOrderLine struct {
	Part   string  `json:"part"`   // Metal, Making, Setting, Stone
	Label  string  `json:"label"`  // what the person reads
	Amount float64 `json:"amount"` // rounded to 2 places
}

// CustomOrderPrice is the quote: the rate used, each part and the total.
type CustomOrderPrice struct {
	Input       CustomOrderInput  `json:"input"`
	RateDate    string            `json:"rate_date"`
	RatePerGram float64           `json:"rate_per_gram"`
	Lines       []CustomOrderLine `json:"lines"`
	MetalAmount float64           `json:"metal_amount"`
	MakingAmt   float64           `json:"making_amount"`
	ExtraAmount float64           `json:"extra_amount"`
	Total       float64           `json:"total"`
	Description string            `json:"description"`
	HSN         string            `json:"hsn_code"`
	GSTRate     float64           `json:"gst_rate"`
	// GSTAmount is the GST inside Total (the total is GST-inclusive, like every price).
	GSTAmount float64 `json:"gst_amount"`
}

func coRound(v float64) float64 { return math.Round(v*100) / 100 }

// coDocs reads the live documents of one doctype.
func coDocs(schema, doctype string) ([]map[string]interface{}, error) {
	rows, err := db.DB.Query(fmt.Sprintf(`SELECT data FROM %s.documents WHERE doctype = $1 AND deleted_at IS NULL ORDER BY created_at, id`, schema), doctype)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []map[string]interface{}
	for rows.Next() {
		var raw []byte
		if err := rows.Scan(&raw); err != nil {
			return nil, err
		}
		m := map[string]interface{}{}
		if json.Unmarshal(raw, &m) == nil {
			out = append(out, m)
		}
	}
	return out, rows.Err()
}

func coSame(a, b string) bool { return strings.EqualFold(strings.TrimSpace(a), strings.TrimSpace(b)) }

func coNum(m map[string]interface{}, k string) float64 {
	switch v := m[k].(type) {
	case float64:
		return v
	case string:
		var f float64
		fmt.Sscanf(strings.TrimSpace(v), "%g", &f)
		return f
	}
	return 0
}

// CustomOrderChoices lists the active choices by kind, for the configurator.
func CustomOrderChoices(tenantID string) (map[string][]map[string]interface{}, error) {
	if err := RequireModules(tenantID, ModuleCustomOrders); err != nil {
		return nil, coErr(coOffCode, "Custom orders are off.")
	}
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return nil, err
	}
	docs, err := coDocs(schema, CustomChoiceDoctype)
	if err != nil {
		return nil, err
	}
	out := map[string][]map[string]interface{}{}
	for _, d := range docs {
		if strField(d, "status") == "Inactive" {
			continue
		}
		k := strField(d, "kind")
		out[k] = append(out[k], map[string]interface{}{"value": strField(d, "value")})
	}
	return out, nil
}

// coTodayAndRate finds today's rate (in the client's zone) for a metal and
// purity. The latest save of the day wins.
func coTodayAndRate(schema, metal, purity string) (string, float64, bool, error) {
	var today string
	if err := db.DB.QueryRow(fmt.Sprintf(`SELECT to_char(%s, 'YYYY-MM-DD')`, crmLocalTodaySQL(crmZoneName(schema)))).Scan(&today); err != nil {
		return "", 0, false, err
	}
	var raw sql.NullString
	err := db.DB.QueryRow(fmt.Sprintf(`SELECT data->>'rate_per_gram' FROM %s.documents
		WHERE doctype = 'MetalRate' AND deleted_at IS NULL AND data->>'rate_date' = $1
		  AND lower(btrim(data->>'metal')) = lower(btrim($2)) AND lower(btrim(data->>'purity')) = lower(btrim($3))
		ORDER BY created_at DESC, id DESC LIMIT 1`, schema), today, metal, purity).Scan(&raw)
	if err == sql.ErrNoRows {
		return today, 0, false, nil
	}
	if err != nil {
		return today, 0, false, err
	}
	var rate float64
	fmt.Sscanf(raw.String, "%g", &rate)
	return today, rate, rate > 0, nil
}

// PriceCustomOrder prices a configuration from today's rate. It writes nothing.
func PriceCustomOrder(tenantID string, in CustomOrderInput) (*CustomOrderPrice, error) {
	if err := RequireModules(tenantID, ModuleCustomOrders); err != nil {
		return nil, coErr(coOffCode, "Custom orders are off.")
	}
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return nil, err
	}
	in.Metal, in.Purity, in.Stone = strings.TrimSpace(in.Metal), strings.TrimSpace(in.Purity), strings.TrimSpace(in.Stone)
	in.Size, in.MakingType = strings.TrimSpace(in.Size), strings.TrimSpace(in.MakingType)
	if in.NetWeight <= 0 || in.StoneCount < 0 {
		return nil, coErr(coWeightCode, "Enter the weight.")
	}
	choices, err := coDocs(schema, CustomChoiceDoctype)
	if err != nil {
		return nil, err
	}
	pick := func(kind, value string, required bool) (map[string]interface{}, error) {
		if value == "" {
			if required {
				return nil, coErr(coChoiceCode, fmt.Sprintf("Pick the %s.", strings.ToLower(kind)))
			}
			return nil, nil
		}
		for _, c := range choices {
			if strField(c, "kind") == kind && coSame(strField(c, "value"), value) && strField(c, "status") != "Inactive" {
				return c, nil
			}
		}
		return nil, coErr(coChoiceCode, fmt.Sprintf("%s is not a %s choice.", value, strings.ToLower(kind)))
	}
	metal, err := pick(coKindMetal, in.Metal, true)
	if err != nil {
		return nil, err
	}
	if _, err = pick(coKindPurity, in.Purity, true); err != nil {
		return nil, err
	}
	if _, err = pick(coKindStone, in.Stone, false); err != nil {
		return nil, err
	}
	if _, err = pick(coKindSize, in.Size, false); err != nil {
		return nil, err
	}
	making, err := pick(coKindMaking, in.MakingType, true)
	if err != nil {
		return nil, err
	}
	if in.Stone != "" && in.StoneCount <= 0 {
		in.StoneCount = 1
	}

	today, rate, found, err := coTodayAndRate(schema, in.Metal, in.Purity)
	if err != nil {
		return nil, err
	}
	if !found {
		return nil, coErr(coNoRateCode, fmt.Sprintf("No %s %s rate for today.", in.Purity, in.Metal))
	}

	p := &CustomOrderPrice{Input: in, RateDate: today, RatePerGram: rate}
	p.MetalAmount = coRound(in.NetWeight * rate)
	p.Lines = append(p.Lines, CustomOrderLine{Part: "Metal", Label: fmt.Sprintf("%g g x %.2f", in.NetWeight, rate), Amount: p.MetalAmount})

	basis, value := strField(making, "making_basis"), coNum(making, "making_value")
	if basis == "" {
		return nil, coErr(coMakingCode, fmt.Sprintf("%s has no making charge.", in.MakingType))
	}
	p.MakingAmt = coCharge(basis, value, in.NetWeight, in.StoneCount, p.MetalAmount)
	p.Lines = append(p.Lines, CustomOrderLine{Part: "Making", Label: in.MakingType, Amount: p.MakingAmt})

	rules, err := coDocs(schema, CustomOrderRuleDoctype)
	if err != nil {
		return nil, err
	}
	selected := map[string]string{coKindMetal: in.Metal, coKindPurity: in.Purity, coKindStone: in.Stone, coKindSize: in.Size, coKindMaking: in.MakingType}
	for _, r := range rules {
		if strField(r, "status") == "Inactive" || selected[strField(r, "applies_kind")] == "" || !coSame(selected[strField(r, "applies_kind")], strField(r, "choice")) {
			continue
		}
		amt := coCharge(strField(r, "basis"), coNum(r, "amount"), in.NetWeight, in.StoneCount, p.MetalAmount)
		part := "Setting"
		if strField(r, "line_type") == "Stone" {
			part = "Stone"
		}
		p.ExtraAmount += amt
		p.Lines = append(p.Lines, CustomOrderLine{Part: part, Label: strField(r, "label"), Amount: amt})
	}
	p.ExtraAmount = coRound(p.ExtraAmount)
	p.Total = coRound(p.MetalAmount + p.MakingAmt + p.ExtraAmount)
	p.HSN, p.GSTRate = strField(metal, "hsn_code"), coNum(metal, "gst_rate")
	if p.GSTRate > 0 {
		p.GSTAmount = coRound(p.Total - p.Total/(1+p.GSTRate/100))
	}

	desc := []string{"Custom " + in.Purity + " " + in.Metal}
	if in.Size != "" {
		desc = append(desc, "size "+in.Size)
	}
	if in.Stone != "" {
		desc = append(desc, fmt.Sprintf("%s x%d", in.Stone, in.StoneCount))
	}
	p.Description = strings.Join(desc, ", ")
	return p, nil
}

// coCharge is one charge by its basis.
func coCharge(basis string, amount, weight float64, stones int, metalAmount float64) float64 {
	switch basis {
	case coBasisPerGram:
		return coRound(amount * weight)
	case coBasisPerStone:
		return coRound(amount * float64(stones))
	case coBasisPercent:
		return coRound(metalAmount * amount / 100)
	}
	return coRound(amount)
}

// CustomOrderCustomer is who the draft order is for.
type CustomOrderCustomer struct {
	Name    string `json:"customer_name"`
	Phone   string `json:"customer_phone"`
	Address string `json:"shipping_address"`
}

// CheckCustomOrderReady refuses, before an item is saved, what would leave an
// item without an order: no customer, no address, no tax set on the metal.
func CheckCustomOrderReady(p *CustomOrderPrice, c CustomOrderCustomer) error {
	if strings.TrimSpace(c.Name) == "" || strings.TrimSpace(c.Address) == "" {
		return coErr(coCustomerCode, "Enter the customer and address.")
	}
	if strings.TrimSpace(p.HSN) == "" || p.GSTRate < 0 {
		return coErr(coTaxCode, fmt.Sprintf("%s has no HSN or GST.", p.Input.Metal))
	}
	return nil
}

// CustomOrderItemBody is the body of the normal item save. No code is sent, so
// the Item series gives the next number.
func CustomOrderItemBody(tenantID string, p *CustomOrderPrice) map[string]interface{} {
	body := map[string]interface{}{
		"name": p.Description, "hsn_code": p.HSN, "gst_rate": p.GSTRate,
		"sale_price": p.Total, "weight": p.Input.NetWeight, "product_type": "Custom jewellery",
	}
	if meta, err := GetDocTypeMeta(tenantID, "Item"); err == nil {
		have := map[string]bool{}
		for _, f := range meta {
			have[f.Fieldname] = true
		}
		for k, v := range map[string]string{"metal_type": p.Input.Metal, "purity_karat": p.Input.Purity, "stone_type": p.Input.Stone, "size": p.Input.Size} {
			if v != "" && have[k] {
				body[k] = v
			}
		}
		if have["net_weight"] {
			body["net_weight"] = p.Input.NetWeight
		}
	}
	return body
}

// CustomOrderResult is what saving gives back.
type CustomOrderResult struct {
	QuoteID     string            `json:"quote_id"`
	ItemID      string            `json:"item_id"`
	OrderID     string            `json:"order_id"`
	OrderNumber string            `json:"order_number"`
	Price       *CustomOrderPrice `json:"price"`
}

// SaveCustomOrder writes the quote (the rate used and each part) and the Draft
// sales order for an item the caller has just saved. The order reserves no stock
// and waits for a person to submit it.
func SaveCustomOrder(tenantID, userID string, p *CustomOrderPrice, itemID string, c CustomOrderCustomer) (*CustomOrderResult, error) {
	if err := RequireModules(tenantID, ModuleCustomOrders); err != nil {
		return nil, coErr(coOffCode, "Custom orders are off.")
	}
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return nil, err
	}
	orderID := NewDocID("SO")
	orderDoc := map[string]interface{}{
		"code": orderID, "channel": "Custom order", "customer_name": strings.TrimSpace(c.Name),
		"shipping_address": strings.TrimSpace(c.Address), "payment_status": "Pending",
		"order_status": "Draft", "hold_reason": "", "hold_owner": "", "total_amount": p.Total,
	}
	if ph := strings.TrimSpace(c.Phone); ph != "" {
		n := NormalizeTenantPhone(tenantID, ph)
		orderDoc["customer_phone"] = n.National
		if n.CountryISO2 != "" {
			orderDoc["customer_country"] = n.CountryISO2
		}
	}
	if cid := ResolveCustomerLink(tenantID, strings.TrimSpace(c.Name)); cid != "" {
		orderDoc["customer_id"] = cid
	}
	issueNumberInto(orderDoc, tenantID, "SalesOrder", orderID, "", "")
	lineID := orderID + "-L1"
	lineDoc := map[string]interface{}{
		"code": lineID, "order_id": orderID, "sku": itemID, "qty": 1, "unit_price": p.Total, "line_status": "Pending",
	}
	quoteID := NewDocID("CQ")
	quoteDoc := map[string]interface{}{
		"code": quoteID, "customer_name": strings.TrimSpace(c.Name), "description": p.Description,
		"net_weight": p.Input.NetWeight, "rate_date": p.RateDate, "rate_per_gram": p.RatePerGram,
		"metal_amount": p.MetalAmount, "making_amount": p.MakingAmt, "extra_amount": p.ExtraAmount, "total": p.Total,
		"item_id": itemID, "order_id": orderID, "status": "Ordered", "input": p.Input, "lines": p.Lines,
	}
	tx, err := db.DB.Begin()
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	for _, d := range []struct {
		id, doctype, status string
		data                map[string]interface{}
	}{
		{orderID, "SalesOrder", "Draft", orderDoc}, {lineID, "SalesOrderLine", "Pending", lineDoc}, {quoteID, CustomOrderQuoteDoctype, "Ordered", quoteDoc},
	} {
		raw, err := json.Marshal(d.data)
		if err != nil {
			return nil, err
		}
		if _, err := tx.Exec(fmt.Sprintf(`INSERT INTO %s.documents (id, doctype, data, status, created_by) VALUES ($1, $2, $3, $4, $5)`, schema),
			d.id, d.doctype, raw, d.status, userID); err != nil {
			return nil, err
		}
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return &CustomOrderResult{QuoteID: quoteID, ItemID: itemID, OrderID: orderID, OrderNumber: DocumentNumber(tenantID, "SalesOrder", orderID), Price: p}, nil
}

// DiscardCustomOrderItem removes the Item a custom order made a moment ago when
// the order itself could not be saved, so no item is left without an order. It
// touches only an Item of the custom type that no order line uses.
func DiscardCustomOrderItem(tenantID, itemID string) {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil || strings.TrimSpace(itemID) == "" {
		return
	}
	_, _ = db.DB.Exec(fmt.Sprintf(`UPDATE %[1]s.documents SET deleted_at = NOW(), updated_at = NOW()
		WHERE doctype = 'Item' AND id = $1 AND deleted_at IS NULL AND data->>'product_type' = 'Custom jewellery'
		  AND NOT EXISTS (SELECT 1 FROM %[1]s.documents l WHERE l.doctype = 'SalesOrderLine' AND l.data->>'sku' = $1 AND l.deleted_at IS NULL)`, schema), itemID)
}
