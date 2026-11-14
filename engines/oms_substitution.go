package engines

// Stage 73.4.2.5: swap an unshipped order line for its item's set substitute
// (Item -> Substitute item) in one step. It is a line edit - remove the line,
// add the substitute at the same quantity and price - so EditOrderLines does
// the stock, the status gates, the payment recheck and the history, exactly
// as when someone makes the same two changes by hand.

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"

	"custom_erp/db"
)

// LineSubstitute is the item set as a line's substitute, or "".
func LineSubstitute(tenantID, sku string) string {
	item, err := ResolveItemBySKU(tenantID, sku)
	if err != nil || item == nil {
		return ""
	}
	sub, _ := item.Data["substitute_item"].(string)
	return strings.TrimSpace(sub)
}

// SubstituteOrderLine replaces one line with its item's substitute.
func SubstituteOrderLine(tenantID, orderID, lineID, userID string) (map[string]interface{}, error) {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return nil, err
	}
	var raw []byte
	err = db.DB.QueryRow(fmt.Sprintf(`SELECT data FROM %s.documents
		WHERE doctype = 'SalesOrderLine' AND id = $1 AND data->>'order_id' = $2 AND deleted_at IS NULL`, schema), lineID, orderID).Scan(&raw)
	if err == sql.ErrNoRows {
		return nil, &ValidationError{Code: "GLOBAL-0004", Message: fmt.Sprintf("line %s is not on order %s", lineID, orderID)}
	} else if err != nil {
		return nil, err
	}
	var line map[string]interface{}
	if err := json.Unmarshal(raw, &line); err != nil {
		return nil, err
	}
	sku := strField(line, "sku")
	sub := LineSubstitute(tenantID, sku)
	if sub == "" {
		return nil, &ValidationError{Code: "ORDERM-0467", SubFor: "lines", Message: fmt.Sprintf("%s has no substitute item set", sku)}
	}
	subItem, err := ResolveItemBySKU(tenantID, sub)
	if err != nil {
		return nil, &ValidationError{Code: "ORDERM-0467", SubFor: "lines", Message: fmt.Sprintf("substitute %s is not an item", sub)}
	}
	subSKU, _ := subItem.Data["code"].(string)
	if subSKU == "" {
		subSKU = subItem.ID
	}
	qty := 0
	switch v := line["qty"].(type) {
	case float64:
		qty = int(v)
	case int:
		qty = v
	}
	price, _ := line["unit_price"].(float64)
	return EditOrderLines(tenantID, orderID, []OrderLineChange{
		{LineID: lineID, Remove: true},
		{SKU: subSKU, Qty: qty, UnitPrice: &price},
	}, fmt.Sprintf("Substitute: %s for %s", subSKU, sku), userID)
}
