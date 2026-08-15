package engines

import (
	"custom_erp/db"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
)

// Stage 35.4.1: ShippingPackage - the object between a completed pack task and
// the courier booking.
//
// Why this needs to exist at all. Until now the chain ran FulfillmentTask ->
// LogisticsBooking, which quietly assumes one task ships as exactly one parcel.
// That assumption breaks the moment a packer needs two boxes for one task (an
// oversize item, a courier weight cap, a fragile line packed separately), and
// it leaves nothing to hang an invoice off: 26.12.3 deferred invoice-from-pack
// precisely because "the pack" was not a thing you could point at.
//
// The lifecycle is deliberately short - Draft -> Invoiced -> Shipped, with
// Cancelled as the exit - and only Draft is mutable. Once an invoice exists the
// package's contents are frozen, because at that point a tax document has been
// raised against them and editing the box silently makes the invoice wrong.
// That freeze is the whole basis of 35.4.3's ordering rule.

// sqlExecer is the small slice of *sql.DB / *sql.Tx this file needs, so a
// package can be written either standalone or inside a caller's transaction
// without two copies of the same INSERT. Local to this file on purpose - the
// repo has no such abstraction today and does not need a general one.
type sqlExecer interface {
	Exec(query string, args ...interface{}) (sql.Result, error)
}

// dbExecer routes a standalone write at the pooled connection. A bare
// db.DB would satisfy sqlExecer directly, but naming it keeps the two call
// styles visibly symmetric at every call site.
type dbExecer struct{}

func (dbExecer) Exec(query string, args ...interface{}) (sql.Result, error) {
	return db.DB.Exec(query, args...)
}

// ShippingPackageLine is one SKU and quantity inside a package. Deliberately
// not a SalesOrderLine reference: after a split, one order line's quantity can
// legitimately live across two packages, so the package holds quantities, and
// SalesOrderLine remains the single per-line object for order-level questions.
type ShippingPackageLine struct {
	SKU string `json:"sku"`
	Qty int    `json:"qty"`
}

// ShippingPackage is a package loaded for mutation. data holds every other
// top-level field untouched, the same shape pickPackTask uses, so a save never
// clobbers a field this file does not know about.
type ShippingPackage struct {
	ID             string                `json:"id"`
	TaskID         string                `json:"fulfillment_task_id"`
	OrderID        string                `json:"order_id"`
	LocationCode   string                `json:"location_code"`
	Items          []ShippingPackageLine `json:"items"`
	WeightKg       float64               `json:"weight_kg"`
	LengthCm       float64               `json:"length_cm"`
	WidthCm        float64               `json:"width_cm"`
	HeightCm       float64               `json:"height_cm"`
	PackageType    string                `json:"package_type"`
	SalesInvoiceID string                `json:"sales_invoice_id"`
	SplitFrom      string                `json:"split_from"`
	Status         string                `json:"status"`

	data map[string]interface{}
}

// shippingPackageMutableStatus is the single answer to "can this package still
// be changed". Kept as one function rather than repeated string comparisons so
// the four mutating entry points below cannot drift apart on it.
func shippingPackageMutableStatus(status string) error {
	if status == "Draft" {
		return nil
	}
	return fmt.Errorf("shipping package is %s - only a Draft package can be modified (an invoice has already been raised against its contents)", status)
}

func packageLinesFromAny(raw interface{}) []ShippingPackageLine {
	arr, ok := raw.([]interface{})
	if !ok {
		return nil
	}
	var out []ShippingPackageLine
	for _, ri := range arr {
		m, ok := ri.(map[string]interface{})
		if !ok {
			continue
		}
		sku, _ := m["sku"].(string)
		if sku == "" {
			continue
		}
		out = append(out, ShippingPackageLine{SKU: sku, Qty: int(numFromInterface(m["qty"]))})
	}
	return out
}

func packageLinesToAny(lines []ShippingPackageLine) []map[string]interface{} {
	out := make([]map[string]interface{}, len(lines))
	for i, l := range lines {
		out[i] = map[string]interface{}{"sku": l.SKU, "qty": l.Qty}
	}
	return out
}

func loadShippingPackage(schema, packageID string) (*ShippingPackage, error) {
	var status, dataStr string
	err := db.DB.QueryRow(fmt.Sprintf(
		`SELECT status, data FROM %s.documents WHERE doctype = 'ShippingPackage' AND id = $1 AND deleted_at IS NULL`, schema),
		packageID).Scan(&status, &dataStr)
	if err == sql.ErrNoRows {
		return nil, fmt.Errorf("shipping package %s not found", packageID)
	} else if err != nil {
		return nil, err
	}
	var data map[string]interface{}
	if err := json.Unmarshal([]byte(dataStr), &data); err != nil {
		return nil, err
	}
	p := &ShippingPackage{ID: packageID, Status: status, data: data}
	p.TaskID, _ = data["fulfillment_task_id"].(string)
	p.OrderID, _ = data["order_id"].(string)
	p.LocationCode, _ = data["location_code"].(string)
	p.PackageType, _ = data["package_type"].(string)
	p.SalesInvoiceID, _ = data["sales_invoice_id"].(string)
	p.SplitFrom, _ = data["split_from"].(string)
	p.WeightKg = numFromInterface(data["weight_kg"])
	p.LengthCm = numFromInterface(data["length_cm"])
	p.WidthCm = numFromInterface(data["width_cm"])
	p.HeightCm = numFromInterface(data["height_cm"])
	p.Items = packageLinesFromAny(data["items"])
	return p, nil
}

// save writes the package back through the same execer the caller is already
// using, so a split can write both packages in one transaction.
func (p *ShippingPackage) save(ex sqlExecer, schema string) error {
	if p.data == nil {
		p.data = map[string]interface{}{}
	}
	p.data["code"] = p.ID
	p.data["fulfillment_task_id"] = p.TaskID
	p.data["order_id"] = p.OrderID
	p.data["location_code"] = p.LocationCode
	p.data["items"] = packageLinesToAny(p.Items)
	p.data["weight_kg"] = p.WeightKg
	p.data["length_cm"] = p.LengthCm
	p.data["width_cm"] = p.WidthCm
	p.data["height_cm"] = p.HeightCm
	p.data["package_type"] = p.PackageType
	p.data["sales_invoice_id"] = p.SalesInvoiceID
	p.data["split_from"] = p.SplitFrom
	p.data["status"] = p.Status
	marshaled, err := json.Marshal(p.data)
	if err != nil {
		return err
	}
	_, err = ex.Exec(fmt.Sprintf(
		`UPDATE %s.documents SET data = $1, status = $2, updated_at = CURRENT_TIMESTAMP WHERE doctype = 'ShippingPackage' AND id = $3`, schema),
		marshaled, p.Status, p.ID)
	return err
}

func (p *ShippingPackage) insert(ex sqlExecer, schema string) error {
	if p.data == nil {
		p.data = map[string]interface{}{}
	}
	p.data["code"] = p.ID
	p.data["fulfillment_task_id"] = p.TaskID
	p.data["order_id"] = p.OrderID
	p.data["location_code"] = p.LocationCode
	p.data["items"] = packageLinesToAny(p.Items)
	p.data["weight_kg"] = p.WeightKg
	p.data["length_cm"] = p.LengthCm
	p.data["width_cm"] = p.WidthCm
	p.data["height_cm"] = p.HeightCm
	p.data["package_type"] = p.PackageType
	p.data["sales_invoice_id"] = p.SalesInvoiceID
	p.data["split_from"] = p.SplitFrom
	p.data["status"] = p.Status
	marshaled, err := json.Marshal(p.data)
	if err != nil {
		return err
	}
	_, err = ex.Exec(fmt.Sprintf(
		`INSERT INTO %s.documents (id, doctype, data, status, created_by) VALUES ($1, 'ShippingPackage', $2, $3, 'system')`, schema),
		p.ID, marshaled, p.Status)
	return err
}

// CreateShippingPackageFromTask turns a completed pack task into one package
// holding everything that was actually packed.
//
// Idempotent by design, and the reason matters: the pack screen's "done" button
// is exactly the kind of control an operator double-taps, and this runs at the
// end of a physical process nobody wants to repeat. A second call returns the
// package the first one made rather than producing a duplicate parcel record
// that would then be invoiced twice.
//
// packed_qty, not qty, is what goes in the box - a short-picked line ships
// short, and invoicing the ordered quantity for a parcel that does not contain
// it is the single most expensive mistake available in this part of the chain.
func CreateShippingPackageFromTask(tenantID, taskID, userID string) (string, error) {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return "", err
	}
	if strings.TrimSpace(taskID) == "" {
		return "", fmt.Errorf("fulfillment_task_id is required")
	}

	task, err := loadPickPackTask(schema, taskID)
	if err != nil {
		return "", err
	}
	// Packed is the gate. Dispatched is allowed too, so a task that shipped
	// through the legacy path before this stage can still be given the package
	// its invoice needs, rather than being permanently unrepresentable.
	if task.status != "Packed" && task.status != "Dispatched" {
		return "", fmt.Errorf("fulfillment task %s is %s - complete packing before creating a shipping package", taskID, task.status)
	}

	if existing, err := firstLivePackageForTask(schema, taskID); err != nil {
		return "", err
	} else if existing != "" {
		return existing, nil
	}

	var items []ShippingPackageLine
	for _, it := range task.items {
		if it.PackedQty > 0 {
			items = append(items, ShippingPackageLine{SKU: it.SKU, Qty: it.PackedQty})
		}
	}
	if len(items) == 0 {
		return "", fmt.Errorf("fulfillment task %s has nothing packed - a package with no contents would invoice to zero", taskID)
	}

	orderID, _ := task.data["order_id"].(string)
	locationCode, _ := task.data["location_code"].(string)

	p := &ShippingPackage{
		ID:           NewDocID("PKG"),
		TaskID:       taskID,
		OrderID:      orderID,
		LocationCode: locationCode,
		Items:        items,
		Status:       "Draft",
	}
	if err := p.insert(dbExecer{}, schema); err != nil {
		return "", err
	}
	LogAuditEvent(tenantID, userID, "CREATE_SHIPPING_PACKAGE", "SUCCESS",
		fmt.Sprintf("Created shipping package %s for task %s (order %s, %d lines)", p.ID, taskID, orderID, len(items)))
	return p.ID, nil
}

// firstLivePackageForTask returns the oldest non-Cancelled package for a task,
// which is what makes creation idempotent. A cancelled package is deliberately
// not a match: cancelling one is how an operator says "repack this".
func firstLivePackageForTask(schema, taskID string) (string, error) {
	var id string
	err := db.DB.QueryRow(fmt.Sprintf(
		`SELECT id FROM %s.documents
		  WHERE doctype = 'ShippingPackage' AND deleted_at IS NULL
		    AND data->>'fulfillment_task_id' = $1 AND status <> 'Cancelled'
		  ORDER BY created_at, id LIMIT 1`, schema), taskID).Scan(&id)
	if err == sql.ErrNoRows {
		return "", nil
	}
	return id, err
}

// ShippingPackageUpdate carries the mutable physical attributes. Pointers so an
// omitted field means "leave it alone" - the same partial-edit convention
// handleEditOrder already uses, and the right one here because the weigh
// station and the dimensioner are two different steps.
type ShippingPackageUpdate struct {
	WeightKg    *float64 `json:"weight_kg"`
	LengthCm    *float64 `json:"length_cm"`
	WidthCm     *float64 `json:"width_cm"`
	HeightCm    *float64 `json:"height_cm"`
	PackageType *string  `json:"package_type"`
}

// UpdateShippingPackage sets weight/dimensions/type on a Draft package.
func UpdateShippingPackage(tenantID, packageID string, up ShippingPackageUpdate, userID string) error {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return err
	}
	p, err := loadShippingPackage(schema, packageID)
	if err != nil {
		return err
	}
	if err := shippingPackageMutableStatus(p.Status); err != nil {
		return err
	}
	// Negative weight or dimension is not a business decision anyone can make;
	// it is a bad payload, and it would go on to price the shipment.
	for label, v := range map[string]*float64{"weight_kg": up.WeightKg, "length_cm": up.LengthCm, "width_cm": up.WidthCm, "height_cm": up.HeightCm} {
		if v != nil && *v < 0 {
			return fmt.Errorf("%s cannot be negative", label)
		}
	}
	if up.WeightKg != nil {
		p.WeightKg = *up.WeightKg
	}
	if up.LengthCm != nil {
		p.LengthCm = *up.LengthCm
	}
	if up.WidthCm != nil {
		p.WidthCm = *up.WidthCm
	}
	if up.HeightCm != nil {
		p.HeightCm = *up.HeightCm
	}
	if up.PackageType != nil {
		p.PackageType = strings.TrimSpace(*up.PackageType)
	}
	if err := p.save(dbExecer{}, schema); err != nil {
		return err
	}
	LogAuditEvent(tenantID, userID, "UPDATE_SHIPPING_PACKAGE", "SUCCESS", fmt.Sprintf("Updated shipping package %s", packageID))
	return nil
}

// SplitShippingPackage moves part of a Draft package's contents into a new
// Draft package, and is the reason ShippingPackage exists as a document rather
// than a few extra columns on the booking.
//
// Two refusals worth stating. Moving *everything* is refused: that is not a
// split, it is a rename, and it would leave an empty package that still looks
// shippable. Moving more than a line holds is refused rather than clamped,
// because a clamp would silently produce two packages whose contents no longer
// add up to what was packed - and nothing downstream would ever notice.
//
// Both writes happen in one transaction. A split that half-succeeded would
// double-count or lose stock in exactly the window an operator is least likely
// to look.
func SplitShippingPackage(tenantID, packageID string, move []ShippingPackageLine, userID string) (string, error) {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return "", err
	}
	if len(move) == 0 {
		return "", fmt.Errorf("nothing to split - specify at least one sku and qty to move")
	}
	source, err := loadShippingPackage(schema, packageID)
	if err != nil {
		return "", err
	}
	if err := shippingPackageMutableStatus(source.Status); err != nil {
		return "", err
	}

	// Collapse duplicate SKUs in the request first. Two rows for the same SKU
	// would otherwise each be validated against the full remaining quantity and
	// both pass, letting the split move more than the package holds.
	wanted := map[string]int{}
	for _, m := range move {
		sku := strings.TrimSpace(m.SKU)
		if sku == "" {
			return "", fmt.Errorf("every split line needs a sku")
		}
		if m.Qty <= 0 {
			return "", fmt.Errorf("split quantity for %s must be positive", sku)
		}
		wanted[sku] += m.Qty
	}

	remaining := make([]ShippingPackageLine, 0, len(source.Items))
	moved := make([]ShippingPackageLine, 0, len(wanted))
	held := map[string]int{}
	for _, it := range source.Items {
		held[it.SKU] += it.Qty
	}
	for sku, qty := range wanted {
		if held[sku] == 0 {
			return "", fmt.Errorf("sku %s is not in package %s", sku, packageID)
		}
		if qty > held[sku] {
			return "", fmt.Errorf("cannot move %d of %s - package %s holds only %d", qty, sku, packageID, held[sku])
		}
	}
	totalHeld, totalMoved := 0, 0
	for _, it := range source.Items {
		totalHeld += it.Qty
		take := wanted[it.SKU]
		if take > it.Qty {
			take = it.Qty
		}
		wanted[it.SKU] -= take
		totalMoved += take
		if take > 0 {
			moved = append(moved, ShippingPackageLine{SKU: it.SKU, Qty: take})
		}
		if it.Qty-take > 0 {
			remaining = append(remaining, ShippingPackageLine{SKU: it.SKU, Qty: it.Qty - take})
		}
	}
	if totalMoved >= totalHeld {
		return "", fmt.Errorf("cannot move every unit out of package %s - that would leave an empty package; cancel it instead", packageID)
	}

	target := &ShippingPackage{
		ID:           NewDocID("PKG"),
		TaskID:       source.TaskID,
		OrderID:      source.OrderID,
		LocationCode: source.LocationCode,
		Items:        moved,
		PackageType:  source.PackageType,
		SplitFrom:    source.ID,
		Status:       "Draft",
	}
	source.Items = remaining
	// Weight and dimensions describe the original box and are now wrong for
	// both halves. Clearing them is honest; carrying them over would price two
	// shipments off one measurement.
	source.WeightKg, source.LengthCm, source.WidthCm, source.HeightCm = 0, 0, 0, 0

	tx, err := db.DB.Begin()
	if err != nil {
		return "", err
	}
	defer tx.Rollback()
	if err := db.SetSearchPath(tx, schema); err != nil {
		return "", err
	}
	if err := target.insert(tx, schema); err != nil {
		return "", err
	}
	if err := source.save(tx, schema); err != nil {
		return "", err
	}
	if err := tx.Commit(); err != nil {
		return "", err
	}
	LogAuditEvent(tenantID, userID, "SPLIT_SHIPPING_PACKAGE", "SUCCESS",
		fmt.Sprintf("Split %d units out of package %s into %s", totalMoved, source.ID, target.ID))
	return target.ID, nil
}

// CancelShippingPackage voids a package. Reason-coded through the same
// ReasonCode master every other cancellation in this repo uses, and refused
// once the package has shipped - at that point the parcel is physically gone
// and a credit note, not a cancellation, is the correct instrument.
func CancelShippingPackage(tenantID, packageID, reasonCode, userID string) error {
	if err := requireActiveReasonCode(tenantID, reasonCode, "Cancellation"); err != nil {
		return err
	}
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return err
	}
	p, err := loadShippingPackage(schema, packageID)
	if err != nil {
		return err
	}
	if p.Status == "Shipped" {
		return fmt.Errorf("shipping package %s has already shipped and cannot be cancelled - raise a credit note instead", packageID)
	}
	if p.Status == "Cancelled" {
		return nil
	}
	if err := ValidateStatusTransition(tenantID, "ShippingPackage", p.Status, "Cancelled", map[string]interface{}{"reason_code": reasonCode}); err != nil {
		return err
	}
	p.Status = "Cancelled"
	p.data["cancel_reason"] = reasonCode
	if err := p.save(dbExecer{}, schema); err != nil {
		return err
	}
	LogAuditEvent(tenantID, userID, "CANCEL_SHIPPING_PACKAGE", "SUCCESS", fmt.Sprintf("Cancelled shipping package %s (%s)", packageID, reasonCode))
	return nil
}

// ListShippingPackages returns every live package for a task or an order.
// Exactly one of the two filters must be given - an unfiltered list of every
// package a tenant has ever produced is not a question anyone asks, and
// answering it would be an unbounded read.
func ListShippingPackages(tenantID, taskID, orderID string) ([]ShippingPackage, error) {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return nil, err
	}
	taskID, orderID = strings.TrimSpace(taskID), strings.TrimSpace(orderID)
	if (taskID == "") == (orderID == "") {
		return nil, fmt.Errorf("specify exactly one of fulfillment_task_id or order_id")
	}
	field, value := "fulfillment_task_id", taskID
	if taskID == "" {
		field, value = "order_id", orderID
	}
	rows, err := db.DB.Query(fmt.Sprintf(
		`SELECT id FROM %s.documents
		  WHERE doctype = 'ShippingPackage' AND deleted_at IS NULL AND data->>'%s' = $1
		  ORDER BY created_at, id`, schema, field), value)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	out := make([]ShippingPackage, 0, len(ids))
	for _, id := range ids {
		p, err := loadShippingPackage(schema, id)
		if err != nil {
			return nil, err
		}
		out = append(out, *p)
	}
	return out, nil
}
