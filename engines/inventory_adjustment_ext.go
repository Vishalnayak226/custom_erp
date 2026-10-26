package engines

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"custom_erp/db"
)

// Stage 130.14 (74.2.3b, "build all" 2026-10-22): the rest of the stock
// adjustment - lines by bin / lot / serial with their before and after
// quantity, a preview (PreviewInventoryAdjustment), the post refusing a
// snapshot that no longer holds, a linked reversing adjustment, a damaged unit
// still owned moved to Damaged without a write-off, and the register, the loss
// dashboard and the repeated-gain flag.

func absInt(n int) int {
	if n < 0 {
		return -n
	}
	return n
}

// onHandAt is the store's on-hand quantity of sku.
func onHandAt(schema, sku, location string) int {
	var n int
	_ = db.DB.QueryRow(fmt.Sprintf(`SELECT on_hand FROM %s.inventory_availability WHERE sku = $1 AND location_code = $2`, schema), sku, location).Scan(&n)
	return n
}

// damagedStock is the store's Damaged quantity of sku.
func damagedStock(schema, sku, location string) int {
	var n int
	_ = db.DB.QueryRow(fmt.Sprintf(`SELECT COALESCE(damaged, 0) FROM %s.inventory_availability WHERE sku = $1 AND location_code = $2`, schema), sku, location).Scan(&n)
	return n
}

// binGoodStock is the sellable quantity of sku in a bin.
func binGoodStock(schema, bin, sku string) int {
	var n int
	_ = db.DB.QueryRow(fmt.Sprintf(`SELECT COALESCE(SUM(qty), 0) FROM %s.bin_stock WHERE bin_code = $1 AND sku = $2 AND condition = 'Good'`, schema), bin, sku).Scan(&n)
	return n
}

// checkAdjustmentSnapshotTx refuses to post when an item's on-hand quantity is
// no longer what the adjustment was worked out from (its before_qty), under
// the stock row's lock. A line written before 130.14 has no snapshot.
func checkAdjustmentSnapshotTx(tx *sql.Tx, schema, location string, lines []map[string]interface{}) error {
	checked := map[string]bool{}
	for _, l := range lines {
		sku := strField(l, "sku")
		if _, has := l["before_qty"]; !has || checked[sku] {
			continue
		}
		checked[sku] = true
		var now int
		if err := tx.QueryRow(fmt.Sprintf(`SELECT on_hand FROM %s.inventory_availability WHERE sku = $1 AND location_code = $2 FOR UPDATE`, schema), sku, location).Scan(&now); err != nil && err != sql.ErrNoRows {
			return err
		}
		if was := int(numFromInterface(l["before_qty"])); now != was {
			return &ValidationError{Code: "INVENT-1312", SubFor: sku, Message: fmt.Sprintf("%s at %s was %d when this adjustment was made and is %d now - check the count, then post with the current stock", sku, location, was, now)}
		}
	}
	return nil
}

// RefreshAdjustmentSnapshot takes each line's before / after quantity again
// from the stock on hand now - a manager's "post with the current stock" for
// an adjustment the stock moved under while it waited.
func RefreshAdjustmentSnapshot(tenantID, id, userID string) error {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return err
	}
	data, status, _, err := fetchDocument(tenantID, InventoryAdjustmentDoctype, id)
	if err != nil {
		return fmt.Errorf("stock adjustment %s not found", id)
	}
	if status != "Draft" && status != "Approved" {
		return fmt.Errorf("stock adjustment %s is %s - only one waiting to post is refreshed", id, status)
	}
	location := strField(data, "location")
	lines := jsonLines(data["items"])
	for _, l := range lines {
		before := onHandAt(schema, strField(l, "sku"), location)
		l["before_qty"] = before
		if strField(l, "condition") != "" {
			l["after_qty"] = before
		} else {
			l["after_qty"] = before + int(numFromInterface(l["qty"]))
		}
	}
	encoded, _ := json.Marshal(lines)
	LogAuditEvent(tenantID, userID, "REFRESH_INVENTORY_ADJUSTMENT", "SUCCESS", fmt.Sprintf("%s: before quantities taken again from current stock", id))
	return mergeDocData(schema, InventoryAdjustmentDoctype, id, map[string]interface{}{"items": string(encoded), "snapshot_refreshed_by": userID})
}

// applyAdjustmentLinesTx does what the stock ledger does not: a condition move
// between sellable and Damaged (still owned, no value), each line's bin, and
// its serial numbers.
func applyAdjustmentLinesTx(tx *sql.Tx, schema, location, number string, lines []map[string]interface{}) error {
	for _, l := range lines {
		sku, bin := strField(l, "sku"), strField(l, "bin")
		qty := int(numFromInterface(l["qty"]))
		if strField(l, "condition") != "" {
			into := -qty // > 0: sellable into Damaged; < 0: back to sellable
			if _, err := tx.Exec(fmt.Sprintf(`UPDATE %s.inventory_availability SET available = available - $3, damaged = COALESCE(damaged, 0) + $3, updated_at = CURRENT_TIMESTAMP
				WHERE sku = $1 AND location_code = $2`, schema), sku, location, into); err != nil {
				return err
			}
			if bin != "" {
				from, to := "Good", "Damaged"
				if into < 0 {
					from, to, into = "Damaged", "Good", -into
				}
				if err := moveBinConditionTx(tx, schema, bin, sku, location, from, to, into); err != nil {
					return err
				}
			}
		} else if bin != "" {
			res, err := tx.Exec(fmt.Sprintf(`UPDATE %s.bin_stock SET qty = qty + $3, updated_at = CURRENT_TIMESTAMP
				WHERE bin_code = $1 AND sku = $2 AND condition = 'Good' AND qty + $3 >= 0`, schema), bin, sku, qty)
			if err != nil {
				return err
			}
			if n, _ := res.RowsAffected(); n == 0 {
				if qty < 0 {
					return &ValidationError{Code: "INVENT-0101", SubFor: bin, Message: fmt.Sprintf("bin %s no longer holds %d of %s", bin, -qty, sku)}
				}
				if _, err := tx.Exec(fmt.Sprintf(`INSERT INTO %s.bin_stock (bin_code, sku, location_code, qty, condition) VALUES ($1, $2, $3, $4, 'Good')`, schema), bin, sku, location, qty); err != nil {
					return err
				}
			}
		}
		for _, sn := range stringList(l["serials"]) {
			docID, _, err := lockSerialTx(tx, schema, sku, sn)
			if err != nil {
				return &ValidationError{Code: "INVENT-0101", SubFor: sn, Message: fmt.Sprintf("serial %s of %s is not known", sn, sku)}
			}
			status, fields := SerialInStock, map[string]interface{}{"location_code": location, "notes": "Adjusted in on " + number}
			if qty < 0 && strField(l, "condition") == "" {
				status, fields = SerialScrapped, map[string]interface{}{"reserved_for": "", "notes": "Adjusted out on " + number}
			}
			if err := patchSerialTx(tx, schema, docID, status, fields); err != nil {
				return err
			}
		}
	}
	return nil
}

// moveBinConditionTx moves qty of sku in a bin from one condition to another.
func moveBinConditionTx(tx *sql.Tx, schema, bin, sku, location, from, to string, qty int) error {
	res, err := tx.Exec(fmt.Sprintf(`UPDATE %s.bin_stock SET qty = qty - $4, updated_at = CURRENT_TIMESTAMP
		WHERE bin_code = $1 AND sku = $2 AND condition = $3 AND qty >= $4`, schema), bin, sku, from, qty)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return &ValidationError{Code: "INVENT-0101", SubFor: bin, Message: fmt.Sprintf("bin %s holds fewer than %d %s units of %s", bin, qty, from, sku)}
	}
	res, err = tx.Exec(fmt.Sprintf(`UPDATE %s.bin_stock SET qty = qty + $4, updated_at = CURRENT_TIMESTAMP WHERE bin_code = $1 AND sku = $2 AND condition = $3`, schema), bin, sku, to, qty)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		_, err = tx.Exec(fmt.Sprintf(`INSERT INTO %s.bin_stock (bin_code, sku, location_code, qty, condition) VALUES ($1, $2, $5, $4, $3)`, schema), bin, sku, to, qty, location)
	}
	return err
}

// stringList reads a JSON array of strings.
func stringList(v interface{}) []string {
	var out []string
	if arr, ok := v.([]interface{}); ok {
		for _, x := range arr {
			if s := strings.TrimSpace(fmt.Sprintf("%v", x)); s != "" {
				out = append(out, s)
			}
		}
	}
	return out
}

// ReverseInventoryAdjustment raises the linked reversing adjustment of a
// posted one: every line the other way, same bins / lots / serials, through
// the approval rules like any adjustment. A posted adjustment is never
// deleted (74.1.5) and is reversed once.
func ReverseInventoryAdjustment(tenantID, id, reason, userID, role string) (string, string, error) {
	data, status, _, err := fetchDocument(tenantID, InventoryAdjustmentDoctype, id)
	if err != nil {
		return "", "", fmt.Errorf("stock adjustment %s not found", id)
	}
	number := firstNonEmpty(strField(data, "code"), id)
	switch {
	case strings.TrimSpace(reason) == "":
		return "", "", &ValidationError{Code: "INVENT-1313", SubFor: "Reason", Message: "say why the adjustment is reversed"}
	case status != "Posted":
		return "", "", &ValidationError{Code: "INVENT-1313", SubFor: number, Message: fmt.Sprintf("%s is %s - only a posted adjustment is reversed", number, status)}
	case strField(data, "reversed_by") != "":
		return "", "", &ValidationError{Code: "INVENT-1313", SubFor: number, Message: fmt.Sprintf("%s is already reversed by %s", number, strField(data, "reversed_by"))}
	case strField(data, "reversal_of") != "":
		return "", "", &ValidationError{Code: "INVENT-1313", SubFor: number, Message: fmt.Sprintf("%s is itself a reversal - make a new adjustment instead", number)}
	}
	in := InventoryAdjustmentInput{Location: strField(data, "location"), Note: fmt.Sprintf("Reverses %s: %s", number, strings.TrimSpace(reason)),
		LossClass: strField(data, "loss_class"), ReversalOf: id}
	for _, l := range jsonLines(data["items"]) {
		in.Lines = append(in.Lines, InventoryAdjustmentLine{SKU: strField(l, "sku"), Qty: -int(numFromInterface(l["qty"])), ReasonCode: strField(l, "reason_code"),
			Note: "Reverses " + number, Bin: strField(l, "bin"), BatchNo: strField(l, "batch_no"), Serials: stringList(l["serials"]), Condition: strField(l, "condition")})
	}
	newID, newStatus, err := CreateInventoryAdjustment(tenantID, in, userID, role)
	if newID != "" {
		if schema, serr := db.GetTenantSchema(tenantID); serr == nil {
			_ = mergeDocData(schema, InventoryAdjustmentDoctype, id, map[string]interface{}{"reversed_by": newID, "reversal_reason": strings.TrimSpace(reason)})
		}
		LogAuditEvent(tenantID, userID, "REVERSE_INVENTORY_ADJUSTMENT", "SUCCESS", fmt.Sprintf("%s reversed by %s (%s)", number, newID, newStatus))
	}
	return newID, newStatus, err
}

func init() {
	RegisterReport(ReportDefinition{ID: "inventory-adjustment-register", Label: "Stock adjustment register", Category: "Inventory",
		Params: []ReportParam{{Key: "from", Label: "From", Type: "date"}, {Key: "to", Label: "To", Type: "date"}},
		Columns: []ReportColumn{{Key: "date", Label: "Date"}, {Key: "number", Label: "Adjustment"}, {Key: "location", Label: "Store"}, {Key: "sku", Label: "Item"},
			{Key: "qty", Label: "Qty"}, {Key: "before", Label: "Before"}, {Key: "after", Label: "After"}, {Key: "reason", Label: "Reason"}, {Key: "value", Label: "Value"},
			{Key: "status", Label: "Status"}, {Key: "flag", Label: "Flag"}},
		Run: inventoryAdjustmentRegister})
	RegisterReport(ReportDefinition{ID: "inventory-loss-by-reason", Label: "Stock losses by reason and store", Category: "Inventory",
		Params:  []ReportParam{{Key: "from", Label: "From", Type: "date"}, {Key: "to", Label: "To", Type: "date"}},
		Columns: []ReportColumn{{Key: "location", Label: "Store"}, {Key: "reason", Label: "Reason"}, {Key: "lines", Label: "Lines"}, {Key: "qty", Label: "Units lost"}, {Key: "value", Label: "Value"}},
		Run:     inventoryLossByReason})
}

// adjustmentLineRows are the posted adjustment lines of a period.
func adjustmentLineRows(tenantID string, p map[string]string, each func(at time.Time, data map[string]interface{}, status string, l map[string]interface{})) error {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return err
	}
	rows, err := db.DB.Query(fmt.Sprintf(`SELECT created_at, data, status FROM %s.documents WHERE doctype = $1 AND deleted_at IS NULL
		AND ($2 = '' OR created_at >= public.erp_day_start($2::date)) AND ($3 = '' OR created_at < public.erp_day_start($3::date + 1)) ORDER BY created_at DESC LIMIT 5000`, schema),
		InventoryAdjustmentDoctype, strings.TrimSpace(p["from"]), strings.TrimSpace(p["to"]))
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var at time.Time
		var raw, status string
		if err := rows.Scan(&at, &raw, &status); err != nil {
			return err
		}
		var data map[string]interface{}
		if json.Unmarshal([]byte(raw), &data) != nil {
			continue
		}
		for _, l := range jsonLines(data["items"]) {
			each(at, data, status, l)
		}
	}
	return rows.Err()
}

// inventoryAdjustmentRegister (R-TX-05) lists every adjustment line, and flags
// an item found (a gain) three or more times in 90 days - stock appearing out
// of nowhere again and again is worth a look.
func inventoryAdjustmentRegister(tenantID string, p map[string]string) ([]map[string]interface{}, error) {
	gains := map[string]int{}
	since := time.Now().AddDate(0, 0, -90)
	if err := adjustmentLineRows(tenantID, map[string]string{"from": since.Format("2006-01-02")}, func(at time.Time, data map[string]interface{}, status string, l map[string]interface{}) {
		if status == "Posted" && numFromInterface(l["qty"]) > 0 && strField(l, "condition") == "" && strField(data, "reversal_of") == "" {
			gains[strField(data, "location")+"|"+strField(l, "sku")]++
		}
	}); err != nil {
		return nil, err
	}
	out := []map[string]interface{}{}
	err := adjustmentLineRows(tenantID, p, func(at time.Time, data map[string]interface{}, status string, l map[string]interface{}) {
		flag := ""
		if n := gains[strField(data, "location")+"|"+strField(l, "sku")]; n >= 3 && numFromInterface(l["qty"]) > 0 {
			flag = fmt.Sprintf("Repeated gain (%d in 90 days)", n)
		}
		if r := strField(data, "reversal_of"); r != "" {
			flag = strings.TrimSpace(flag + " Reverses " + r)
		} else if r := strField(data, "reversed_by"); r != "" {
			flag = strings.TrimSpace(flag + " Reversed by " + r)
		}
		if strField(l, "condition") != "" {
			flag = strings.TrimSpace(flag + " To " + strField(l, "condition"))
		}
		out = append(out, map[string]interface{}{"date": at.Format("2006-01-02"), "number": firstNonEmpty(strField(data, "code"), ""), "location": strField(data, "location"),
			"sku": strField(l, "sku"), "qty": numFromInterface(l["qty"]), "before": l["before_qty"], "after": l["after_qty"], "reason": firstNonEmpty(strField(l, "reason"), strField(l, "reason_code")),
			"value": numFromInterface(l["value"]), "status": status, "flag": flag})
	})
	return out, err
}

// inventoryLossByReason is the loss dashboard: posted losses (not condition
// moves, not reversals) by store and reason.
func inventoryLossByReason(tenantID string, p map[string]string) ([]map[string]interface{}, error) {
	type agg struct {
		lines, qty int
		value      float64
	}
	sums := map[[2]string]*agg{}
	var order [][2]string
	err := adjustmentLineRows(tenantID, p, func(at time.Time, data map[string]interface{}, status string, l map[string]interface{}) {
		if status != "Posted" || numFromInterface(l["qty"]) >= 0 || strField(l, "condition") != "" || strField(data, "reversal_of") != "" {
			return
		}
		k := [2]string{strField(data, "location"), firstNonEmpty(strField(l, "reason"), strField(l, "reason_code"))}
		a := sums[k]
		if a == nil {
			a = &agg{}
			sums[k] = a
			order = append(order, k)
		}
		a.lines++
		a.qty += -int(numFromInterface(l["qty"]))
		a.value = round2(a.value + numFromInterface(l["value"]))
	})
	if err != nil {
		return nil, err
	}
	out := []map[string]interface{}{}
	for _, k := range order {
		a := sums[k]
		out = append(out, map[string]interface{}{"location": k[0], "reason": k[1], "lines": a.lines, "qty": a.qty, "value": a.value})
	}
	return out, nil
}
