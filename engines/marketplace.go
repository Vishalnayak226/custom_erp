package engines

import (
	"custom_erp/db"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
)

// Stage 26.12.4 (Courier/Shipment/Manifest): extends the pre-existing bare
// LogisticsBooking record (carrier/tracking/charge, hardcoded status
// "Shipped" at creation - the single largest orchestration gap versus the
// blueprint, per docs/specs/oms_master_blueprint_reference.md §7) into a
// real Shipment engine - serviceability check, AWB assignment, manifest
// grouping, a handover cascade, tracking sync, and RTO detection. Per the
// checklist item's own scope note this is the internal AWB/manifest engine
// only; a real courier API is a separate, later, credentials-gated item,
// the same "code-complete, credentials pending" pattern as the Shopify/
// BigCommerce/Magento/Unicommerce channel connectors.

// CourierOption is one serviceable courier candidate returned by
// CheckCourierServiceability, sorted by priority ascending (lower runs
// first) - the design note (§12) calls this out explicitly as
// "serviceability-then-priority", not a cost/SLA scoring engine.
type CourierOption struct {
	Courier  string `json:"courier"`
	Priority int    `json:"priority"`
}

// CheckCourierServiceability is the Shipment engine's serviceability gate -
// it queries the new CourierServiceArea master (a stand-in "Courier
// Provider connector" config table) for every Active row whose
// pincode_prefix is a prefix of destinationPincode (or blank, meaning
// "services everywhere"). A pincode-prefix match is a distance/coverage
// proxy, not a real geo/zone lookup - the same documented caveat Stage
// 26.12.2's Nearest-Pincode allocation strategy already carries. An empty
// result means nothing is serviceable for that pincode.
func CheckCourierServiceability(tenantID, destinationPincode string) ([]CourierOption, error) {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return nil, err
	}
	rows, err := db.DB.Query(fmt.Sprintf(`
		SELECT data->>'courier', COALESCE((data->>'priority')::int, 999)
		FROM %s.documents
		WHERE doctype = 'CourierServiceArea' AND status = 'Active' AND deleted_at IS NULL
		  AND (COALESCE(data->>'pincode_prefix', '') = '' OR $1 LIKE (data->>'pincode_prefix') || '%%')
		ORDER BY COALESCE((data->>'priority')::int, 999), data->>'courier'`, schema), destinationPincode)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var options []CourierOption
	for rows.Next() {
		var o CourierOption
		if err := rows.Scan(&o.Courier, &o.Priority); err != nil {
			return nil, err
		}
		options = append(options, o)
	}
	return options, rows.Err()
}

// anyCourierServiceAreaConfigured reports whether the tenant has configured
// any CourierServiceArea rows at all, so CreateLogisticsBooking can stay
// usable before any admin config exists - the same "don't gate a feature on
// setup that hasn't happened yet" precedent isCancellationBlocked
// (engines/orders.go) and ResolveAllocationPlan (engines/sourcing.go) both
// already use for their own config masters.
func anyCourierServiceAreaConfigured(schema string) (bool, error) {
	var exists bool
	err := db.DB.QueryRow(fmt.Sprintf(
		`SELECT EXISTS(SELECT 1 FROM %s.documents WHERE doctype = 'CourierServiceArea' AND status = 'Active' AND deleted_at IS NULL)`, schema)).
		Scan(&exists)
	return exists, err
}

// CreateLogisticsBooking books a shipment for orderID, running the
// serviceability -> AWB assignment step the blueprint calls for. A blank
// carrier auto-selects the top-priority serviceable courier for
// destinationPincode; an explicitly-given carrier is still required to be
// serviceable, unless the tenant hasn't configured any CourierServiceArea
// rows yet. Status starts at "AWB Assigned", not the old hardcoded
// "Shipped" - nothing has actually shipped at booking time (§7's own
// documented problem). The AWB number is a locally-generated placeholder -
// a real courier API call is deliberately out of scope here (see this
// item's own "internal engine first, credentials-gated courier API later"
// note), the same "prints text, not a real integration" limitation
// engines/stickers.go already documents for barcode labels. fulfillmentTaskID
// is optional - a booking with no task link stays outside manifest grouping
// and the SalesOrder closure cascade below, a documented boundary, not an
// oversight.
// Stage 35.4.3 added the optional trailing shippingPackageID. It is variadic
// so all six existing call sites needed no change, the same reason
// PostingOptions took variadic fields in 37.1.2. When it is omitted the
// booking still links itself to the package its fulfillment task produced, if
// there is one - otherwise the ordering guard would only apply to callers who
// happened to know about packages, which is precisely the set of callers that
// does not need guarding.
func CreateLogisticsBooking(tenantID, orderID, fulfillmentTaskID, carrier, trackingNumber, destinationPincode string, shippingCharge int, shippingPackageID ...string) (string, error) {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return "", err
	}
	if orderID == "" {
		return "", errors.New("order_id is required")
	}

	packageID := ""
	if len(shippingPackageID) > 0 {
		packageID = strings.TrimSpace(shippingPackageID[0])
	}
	if packageID == "" && fulfillmentTaskID != "" {
		if auto, errP := firstLivePackageForTask(schema, fulfillmentTaskID); errP == nil {
			packageID = auto
		}
	}
	if packageID != "" {
		p, errP := loadShippingPackage(schema, packageID)
		if errP != nil {
			return "", errP
		}
		if p.Status == "Cancelled" {
			return "", fmt.Errorf("shipping package %s is cancelled and cannot be booked", packageID)
		}
	}

	configured, err := anyCourierServiceAreaConfigured(schema)
	if err != nil {
		return "", err
	}

	resolvedCarrier := carrier
	if resolvedCarrier == "" {
		if !configured {
			return "", errors.New("carrier is required (no courier service areas configured for auto-selection)")
		}
		options, errS := CheckCourierServiceability(tenantID, destinationPincode)
		if errS != nil {
			return "", errS
		}
		if len(options) == 0 {
			return "", fmt.Errorf("no serviceable courier configured for destination pincode %q", destinationPincode)
		}
		resolvedCarrier = options[0].Courier
	} else if configured {
		options, errS := CheckCourierServiceability(tenantID, destinationPincode)
		if errS != nil {
			return "", errS
		}
		found := false
		for _, o := range options {
			if o.Courier == resolvedCarrier {
				found = true
				break
			}
		}
		if !found {
			return "", fmt.Errorf("courier %q does not service pincode %q", resolvedCarrier, destinationPincode)
		}
	}

	bookingID := NewDocID("LOG")
	awb := NewDocIDCompact("AWB")
	if trackingNumber == "" {
		trackingNumber = awb
	}
	docData := map[string]interface{}{
		"code":                bookingID,
		"order_id":            orderID,
		"fulfillment_task_id": fulfillmentTaskID,
		"carrier":             resolvedCarrier,
		"tracking_number":     trackingNumber,
		"destination_pincode": destinationPincode,
		"awb_number":          awb,
		"manifest_id":         "",
		"shipping_package_id": packageID,
		"shipping_charge":     shippingCharge,
		"status":              "AWB Assigned",
	}

	marshaled, err := json.Marshal(docData)
	if err != nil {
		return "", err
	}

	query := fmt.Sprintf(`
		INSERT INTO %s.documents (id, doctype, data, status, created_by)
		VALUES ($1, 'LogisticsBooking', $2, 'AWB Assigned', 'system')`, schema)
	_, err = db.DB.Exec(query, bookingID, marshaled)
	return bookingID, err
}

// fetchLogisticsBooking loads one LogisticsBooking's raw fields, mirroring
// engines/orders.go's fetchSalesOrder pattern for the same reason: several
// functions below each need a different subset, and none of it needs to be
// exported as its own getter.
func fetchLogisticsBooking(tenantID, bookingID string) (schema string, data map[string]interface{}, status string, err error) {
	schema, err = db.GetTenantSchema(tenantID)
	if err != nil {
		return "", nil, "", err
	}
	var dataBytes []byte
	err = db.DB.QueryRow(fmt.Sprintf(
		`SELECT data, status FROM %s.documents WHERE doctype = 'LogisticsBooking' AND id = $1 AND deleted_at IS NULL`, schema),
		bookingID).Scan(&dataBytes, &status)
	if err != nil {
		return "", nil, "", fmt.Errorf("logistics booking %s not found: %v", bookingID, err)
	}
	if err := json.Unmarshal(dataBytes, &data); err != nil {
		return "", nil, "", err
	}
	return schema, data, status, nil
}

// GenerateShippingLabel returns a plain-text shipping label for a booking -
// the same "prints text, not a real scannable symbology/PDF" scope
// limitation engines/stickers.go already documents for barcode printing,
// not a new gap introduced here. (35.5.3 is the item that replaces this with
// a real Code128/QR PDF.)
//
// Stage 35.4.3 enforces Uniware's hard rule here: no label before the invoice.
// A label is what authorises a parcel to move, and a parcel that moves without
// a tax document is a compliance problem, not a workflow inconvenience - which
// is why this refuses rather than warns.
//
// The rule applies only to a booking that has a shipping package. A booking
// made before 35.4.1, or a manual one with no package, has no invoice to wait
// for and is labelled exactly as it was - the same backward-compatibility
// boundary CreateLogisticsBooking already draws around task-less bookings.
func GenerateShippingLabel(tenantID, bookingID string) (string, error) {
	schema, data, _, err := fetchLogisticsBooking(tenantID, bookingID)
	if err != nil {
		return "", err
	}
	packageID, _ := data["shipping_package_id"].(string)
	if packageID != "" {
		p, errP := loadShippingPackage(schema, packageID)
		if errP != nil {
			return "", errP
		}
		if p.Status == "Draft" {
			return "", fmt.Errorf("cannot print a label for booking %s: shipping package %s has not been invoiced yet - generate the invoice first", bookingID, packageID)
		}
		if p.Status == "Cancelled" {
			return "", fmt.Errorf("cannot print a label for booking %s: shipping package %s is cancelled", bookingID, packageID)
		}
	}

	carrier, _ := data["carrier"].(string)
	awb, _ := data["awb_number"].(string)
	tracking, _ := data["tracking_number"].(string)
	orderID, _ := data["order_id"].(string)
	pincode, _ := data["destination_pincode"].(string)

	// Stamp the print so the manifest step can tell a labelled parcel from an
	// unlabelled one. Recorded on the booking rather than inferred from the
	// package, because "was a label printed" and "was an invoice raised" are
	// genuinely different facts and the manifest guard needs the first.
	//
	// Write-once, deliberately. This is reached by a GET, so it has to be safe
	// to repeat: the guard only asks whether a label was ever printed, and
	// re-printing a damaged label must not rewrite when the first one went out.
	// The WHERE clause makes the reprint a no-op rather than an update.
	if _, err := db.DB.Exec(fmt.Sprintf(
		`UPDATE %s.documents SET data = jsonb_set(data, '{label_generated_at}', to_jsonb($1::text)), updated_at = CURRENT_TIMESTAMP
		  WHERE id = $2 AND doctype = 'LogisticsBooking'
		    AND COALESCE(data->>'label_generated_at', '') = ''`, schema),
		time.Now().UTC().Format(time.RFC3339), bookingID); err != nil {
		return "", err
	}

	return fmt.Sprintf("SHIP TO PIN: %s\nCARRIER: %s\nAWB: %s\nTRACKING: %s\nORDER: %s\nBOOKING: %s",
		pincode, carrier, awb, tracking, orderID, bookingID), nil
}

// GenerateManifest groups every 'AWB Assigned' LogisticsBooking for one
// courier+location pair into a new Manifest, per the design note's (§12)
// "manifest generation groups already-AWB-assigned shipments by courier+
// location". Location comes from each booking's linked FulfillmentTask
// (fulfillment_task_id) - a manual/legacy booking with no task link has no
// location to group by and is never picked up by this query, the same
// documented boundary CreateLogisticsBooking's own comment draws.
func GenerateManifest(tenantID, courier, locationCode string) (manifestID string, shipmentCount int, err error) {
	if courier == "" || locationCode == "" {
		return "", 0, errors.New("courier and location_code are required")
	}
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return "", 0, err
	}

	// Stage 35.4.3, the manifest half of the ordering rule: a booking that has
	// a shipping package must also have had its label printed before it can be
	// manifested. Expressed as a WHERE clause rather than a post-filter so an
	// unlabelled parcel is never grouped and then silently dropped - it simply
	// is not part of this manifest, and stays available for the next one once
	// its label is printed.
	//
	// The NULL-package branch is what keeps every pre-35.4 booking manifesting
	// exactly as it did.
	rows, err := db.DB.Query(fmt.Sprintf(`
		SELECT lb.id
		FROM %s.documents lb
		JOIN %s.documents ft ON ft.doctype = 'FulfillmentTask' AND ft.id = lb.data->>'fulfillment_task_id'
		WHERE lb.doctype = 'LogisticsBooking' AND lb.status = 'AWB Assigned'
		  AND lb.data->>'carrier' = $1 AND ft.data->>'location_code' = $2
		  AND lb.deleted_at IS NULL AND ft.deleted_at IS NULL
		  AND (COALESCE(lb.data->>'shipping_package_id', '') = ''
		       OR COALESCE(lb.data->>'label_generated_at', '') <> '')`, schema, schema), courier, locationCode)
	if err != nil {
		return "", 0, err
	}
	var bookingIDs []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return "", 0, err
		}
		bookingIDs = append(bookingIDs, id)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return "", 0, err
	}
	if len(bookingIDs) == 0 {
		// Distinguish "nothing to ship" from "everything is waiting on a
		// label", because the two call for completely different operator
		// action and the old single message sent them looking in the wrong
		// place.
		var awaiting int
		_ = db.DB.QueryRow(fmt.Sprintf(`
			SELECT COUNT(*)
			FROM %s.documents lb
			JOIN %s.documents ft ON ft.doctype = 'FulfillmentTask' AND ft.id = lb.data->>'fulfillment_task_id'
			WHERE lb.doctype = 'LogisticsBooking' AND lb.status = 'AWB Assigned'
			  AND lb.data->>'carrier' = $1 AND ft.data->>'location_code' = $2
			  AND lb.deleted_at IS NULL AND ft.deleted_at IS NULL`, schema, schema), courier, locationCode).Scan(&awaiting)
		if awaiting > 0 {
			return "", 0, fmt.Errorf("no manifestable shipments for courier %q at location %q - %d are AWB-assigned but still awaiting an invoice and label", courier, locationCode, awaiting)
		}
		return "", 0, fmt.Errorf("no AWB-assigned shipments found for courier %q at location %q", courier, locationCode)
	}

	manifestID = NewDocID("MAN")
	manifestDoc := map[string]interface{}{
		"code":           manifestID,
		"courier":        courier,
		"location_code":  locationCode,
		"shipment_count": len(bookingIDs),
		"status":         "Open",
	}
	marshaled, err := json.Marshal(manifestDoc)
	if err != nil {
		return "", 0, err
	}

	tx, err := db.DB.Begin()
	if err != nil {
		return "", 0, err
	}
	defer tx.Rollback()
	if err := db.SetSearchPath(tx, schema); err != nil {
		return "", 0, err
	}
	if _, err := tx.Exec(fmt.Sprintf(
		`INSERT INTO %s.documents (id, doctype, data, status, created_by) VALUES ($1, 'Manifest', $2, 'Open', 'system')`, schema),
		manifestID, marshaled); err != nil {
		return "", 0, err
	}
	for _, bID := range bookingIDs {
		if _, err := tx.Exec(fmt.Sprintf(
			`UPDATE %s.documents SET data = jsonb_set(data, '{manifest_id}', to_jsonb($1::text)), status = 'Manifested', updated_at = CURRENT_TIMESTAMP WHERE id = $2 AND doctype = 'LogisticsBooking'`, schema),
			manifestID, bID); err != nil {
			return "", 0, err
		}
	}
	if err := tx.Commit(); err != nil {
		return "", 0, err
	}
	return manifestID, len(bookingIDs), nil
}

// HandoverManifest is the Shipment engine's handover cascade - the design
// note's own concrete split-shipment-aware order-closure rule (§12):
// shipment -> Handed Over, its fulfillment task -> Dispatched (reusing the
// existing TransitionTaskStatus rather than duplicating its stock-deduction
// logic - the CLAUDE.md choke-point principle), and the parent SalesOrder ->
// Shipped only once every FulfillmentTask under that order has reached
// Dispatched (otherwise Partially Fulfilled). A booking's order_id is only
// treated as a SalesOrder reference when a SalesOrder with that code
// actually exists - a manual/legacy booking (or one from the still-
// POSCart-based channel-webhook path 26.12.1 deliberately left unrewired)
// has no order to cascade into and is simply handed over.
func HandoverManifest(tenantID, manifestID, userID string) error {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return err
	}
	var manifestStatus string
	if err := db.DB.QueryRow(fmt.Sprintf(
		`SELECT status FROM %s.documents WHERE doctype = 'Manifest' AND id = $1 AND deleted_at IS NULL`, schema), manifestID).
		Scan(&manifestStatus); err != nil {
		return fmt.Errorf("manifest %s not found: %v", manifestID, err)
	}
	if manifestStatus != "Open" {
		return fmt.Errorf("manifest %s is not Open (currently %s)", manifestID, manifestStatus)
	}

	rows, err := db.DB.Query(fmt.Sprintf(
		`SELECT id, COALESCE(data->>'order_id', ''), COALESCE(data->>'fulfillment_task_id', ''), COALESCE(data->>'shipping_package_id', '') FROM %s.documents
		 WHERE doctype = 'LogisticsBooking' AND data->>'manifest_id' = $1 AND deleted_at IS NULL`, schema), manifestID)
	if err != nil {
		return err
	}
	type bookingRow struct{ id, orderID, taskID, packageID string }
	var bookings []bookingRow
	for rows.Next() {
		var b bookingRow
		if err := rows.Scan(&b.id, &b.orderID, &b.taskID, &b.packageID); err != nil {
			rows.Close()
			return err
		}
		bookings = append(bookings, b)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return err
	}
	if len(bookings) == 0 {
		return fmt.Errorf("manifest %s has no shipments", manifestID)
	}

	affectedOrders := map[string]bool{}
	for _, b := range bookings {
		if _, err := db.DB.Exec(fmt.Sprintf(
			`UPDATE %s.documents SET data = jsonb_set(data, '{status}', '"Handed Over"'), status = 'Handed Over', updated_at = CURRENT_TIMESTAMP WHERE id = $1 AND doctype = 'LogisticsBooking'`, schema),
			b.id); err != nil {
			return err
		}
		if b.taskID != "" {
			var taskStatus string
			_ = db.DB.QueryRow(fmt.Sprintf(
				`SELECT status FROM %s.documents WHERE doctype = 'FulfillmentTask' AND id = $1`, schema), b.taskID).Scan(&taskStatus)
			if taskStatus != "Dispatched" {
				if err := TransitionTaskStatus(tenantID, b.taskID, "Dispatched"); err != nil {
					return fmt.Errorf("booking %s: failed to dispatch fulfillment task %s: %v", b.id, b.taskID, err)
				}
			}
		}
		// Close the package lifecycle at the same moment the parcel physically
		// leaves. Doing it here rather than in a separate call is what stops
		// "Invoiced" from becoming a permanent resting state that quietly
		// disagrees with the booking beside it.
		if b.packageID != "" {
			if p, errP := loadShippingPackage(schema, b.packageID); errP == nil && p.Status == "Invoiced" {
				p.Status = "Shipped"
				if errS := p.save(dbExecer{}, schema); errS != nil {
					return fmt.Errorf("booking %s: failed to mark shipping package %s shipped: %v", b.id, b.packageID, errS)
				}
			}
		}
		if b.orderID != "" {
			affectedOrders[b.orderID] = true
		}
	}

	if _, err := db.DB.Exec(fmt.Sprintf(
		`UPDATE %s.documents SET data = jsonb_set(data, '{status}', '"Handed Over"'), status = 'Handed Over', updated_at = CURRENT_TIMESTAMP WHERE id = $1 AND doctype = 'Manifest'`, schema),
		manifestID); err != nil {
		return err
	}

	for orderID := range affectedOrders {
		if err := evaluateOrderShipmentClosure(tenantID, schema, orderID, userID); err != nil {
			return fmt.Errorf("order %s closure check: %v", orderID, err)
		}
	}

	LogAuditEvent(tenantID, userID, "SHIPMENT_HANDOVER", "SUCCESS", fmt.Sprintf("Manifest %s handed over (%d shipments)", manifestID, len(bookings)))
	return nil
}

// evaluateOrderShipmentClosure implements the design note's (§12)
// split-shipment-aware SalesOrder closure rule: Shipped only once every
// FulfillmentTask under orderID has reached Dispatched, otherwise Partially
// Fulfilled once at least one has. A no-op if no SalesOrder with that code
// exists (a manual/legacy booking's order_id isn't necessarily a
// SalesOrder) or if the order is already in a terminal-ish status.
func evaluateOrderShipmentClosure(tenantID, schema, orderID, userID string) error {
	var orderStatus string
	err := db.DB.QueryRow(fmt.Sprintf(
		`SELECT status FROM %s.documents WHERE doctype = 'SalesOrder' AND id = $1 AND deleted_at IS NULL`, schema), orderID).
		Scan(&orderStatus)
	if err == sql.ErrNoRows {
		return nil
	} else if err != nil {
		return err
	}
	if orderStatus == "Shipped" || orderStatus == "Delivered" || orderStatus == "Cancelled" || orderStatus == "Closed" {
		return nil
	}

	rows, err := db.DB.Query(fmt.Sprintf(
		`SELECT status FROM %s.documents WHERE doctype = 'FulfillmentTask' AND data->>'order_id' = $1 AND deleted_at IS NULL`, schema), orderID)
	if err != nil {
		return err
	}
	total, dispatched := 0, 0
	for rows.Next() {
		var st string
		if err := rows.Scan(&st); err != nil {
			rows.Close()
			return err
		}
		total++
		if st == "Dispatched" {
			dispatched++
		}
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return err
	}
	if total == 0 {
		return nil
	}

	newStatus := "Partially Fulfilled"
	if dispatched == total {
		newStatus = "Shipped"
	}
	if newStatus == orderStatus {
		return nil
	}
	_, err = db.DB.Exec(fmt.Sprintf(
		`UPDATE %s.documents SET data = jsonb_set(data, '{order_status}', to_jsonb($1::text)), status = $1, updated_at = CURRENT_TIMESTAMP WHERE id = $2 AND doctype = 'SalesOrder'`, schema),
		newStatus, orderID)
	if err != nil {
		return err
	}
	if newStatus == "Shipped" {
		if _, err := CreateSalesInvoiceFromOrder(tenantID, orderID, userID); err != nil {
			return fmt.Errorf("create draft invoice: %v", err)
		}
		DispatchNotification(tenantID, "Order Shipped", orderID, map[string]string{"order_status": newStatus})
	}
	return nil
}

// validShipmentTrackingProgress lists which current statuses a tracking-sync
// event may advance from, per target status - In-Transit can be re-reported
// (idempotent), Delivered can follow either Handed Over (skipping an
// In-Transit scan, which real couriers sometimes do) or In-Transit.
var validShipmentTrackingProgress = map[string][]string{
	"In-Transit": {"Handed Over", "In-Transit"},
	"Delivered":  {"In-Transit", "Handed Over"},
}

// RecordDeliveryEvent is the Shipment engine's tracking-sync step -
// progresses a booking through In-Transit -> Delivered. A real courier
// webhook/polling integration is out of scope here (same internal-only
// note as the rest of this item); this is the ingestion point a future
// connector would call into.
func RecordDeliveryEvent(tenantID, bookingID, newStatus, userID string) error {
	allowedFrom, ok := validShipmentTrackingProgress[newStatus]
	if !ok {
		return fmt.Errorf("status %q is not a valid tracking update (must be In-Transit or Delivered)", newStatus)
	}
	schema, bookingData, currentStatus, err := fetchLogisticsBooking(tenantID, bookingID)
	if err != nil {
		return err
	}
	allowed := false
	for _, s := range allowedFrom {
		if s == currentStatus {
			allowed = true
			break
		}
	}
	if !allowed {
		return fmt.Errorf("booking %s cannot move to %q from its current status %q", bookingID, newStatus, currentStatus)
	}
	if _, err := db.DB.Exec(fmt.Sprintf(
		`UPDATE %s.documents SET data = jsonb_set(data, '{status}', to_jsonb($1::text)), status = $1, updated_at = CURRENT_TIMESTAMP WHERE id = $2 AND doctype = 'LogisticsBooking'`, schema),
		newStatus, bookingID); err != nil {
		return err
	}
	LogAuditEvent(tenantID, userID, "SHIPMENT_TRACKING_UPDATE", "SUCCESS", fmt.Sprintf("Booking %s -> %s", bookingID, newStatus))
	if newStatus == "Delivered" {
		if orderID, _ := bookingData["order_id"].(string); orderID != "" {
			DispatchNotification(tenantID, "Order Delivered", orderID, map[string]string{"booking_id": bookingID, "tracking_number": fmt.Sprint(bookingData["tracking_number"])})
		}
	}
	return nil
}

// RecordRTO captures the Shipment engine's own RTO detection event - a
// courier reporting non-delivery. Stock/refund handling for an RTO'd
// shipment is Stage 26.12.5's scope (Returns/RTO/QC/Refund, not yet built);
// this function's job ends at the shipment-status/reason capture the
// blueprint's "RTO detection" step calls for.
func RecordRTO(tenantID, bookingID, reason, userID string) error {
	if reason == "" {
		return errors.New("an RTO reason is required")
	}
	schema, _, currentStatus, err := fetchLogisticsBooking(tenantID, bookingID)
	if err != nil {
		return err
	}
	if currentStatus == "RTO" || currentStatus == "Delivered" {
		return fmt.Errorf("booking %s cannot be marked RTO from its current status %q", bookingID, currentStatus)
	}
	if _, err := db.DB.Exec(fmt.Sprintf(
		`UPDATE %s.documents SET data = jsonb_set(jsonb_set(data, '{status}', '"RTO"'), '{rto_reason}', to_jsonb($1::text)), status = 'RTO', updated_at = CURRENT_TIMESTAMP WHERE id = $2 AND doctype = 'LogisticsBooking'`, schema),
		reason, bookingID); err != nil {
		return err
	}
	LogAuditEvent(tenantID, userID, "SHIPMENT_RTO", "SUCCESS", fmt.Sprintf("Booking %s marked RTO: %s", bookingID, reason))
	return nil
}

// ProcessMarketplaceSettlement processes settlements, reconciles orders, and posts accounting journals
func ProcessMarketplaceSettlement(tenantID string, channel string, settlementID string, totalSale int, commission int, netPayout int, orderIDs []string) error {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return err
	}

	// 1. Math validation
	if totalSale-commission != netPayout {
		return fmt.Errorf("invalid payout math: total sale (%d) minus commission (%d) must equal net payout (%d)", totalSale, commission, netPayout)
	}

	tx, err := db.DB.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	if err := db.SetSearchPath(tx, schema); err != nil {
		return err
	}

	// 2. Reconcile matched orders
	for _, orderID := range orderIDs {
		// Fetch the document and transition its status to 'Settled'
		var docBytes []byte
		errFetch := tx.QueryRow(fmt.Sprintf(`
			SELECT data FROM %s.documents 
			WHERE id = $1 AND doctype = 'POSCart'`, schema), orderID).Scan(&docBytes)
		if errFetch == nil {
			var orderDoc map[string]interface{}
			if errJson := json.Unmarshal(docBytes, &orderDoc); errJson == nil {
				orderDoc["status"] = "Settled"
				updatedBytes, _ := json.Marshal(orderDoc)
				_, _ = tx.Exec(fmt.Sprintf(`
					UPDATE %s.documents 
					SET data = $1, status = 'Settled', updated_at = CURRENT_TIMESTAMP 
					WHERE id = $2 AND doctype = 'POSCart'`, schema), updatedBytes, orderID)
			}
		}
	}

	// 3. Commit order reconciliation updates
	err = tx.Commit()
	if err != nil {
		return err
	}

	// 4. Post balanced GL accounting entries
	// Debit: Cash/Bank (1100) -> netPayout
	// Debit: Commission Expense (5200) -> commission
	// Credit: Accounts Receivable (1300) -> totalSale
	debits := map[string]int{
		"1100": netPayout,
		"5200": commission,
	}
	credits := map[string]int{
		"1300": totalSale,
	}

	err = PostDoubleEntry(tenantID, "MarketplaceSettlement", settlementID, PaiseMap(debits), PaiseMap(credits), "", fmt.Sprintf("MarketplaceSettlement:%s:RECONCILE", settlementID))
	if err != nil {
		return fmt.Errorf("failed to write settlement GL postings: %v", err)
	}

	// 5. Save the MarketplaceSettlement document
	settlementDoc := map[string]interface{}{
		"code":        settlementID,
		"channel":     channel,
		"payout_date": time.Now().Format("2006-01-02"),
		"total_sale":  totalSale,
		"commission":  commission,
		"net_payout":  netPayout,
		"status":      "Reconciled",
		"orders":      orderIDs,
	}
	settlementBytes, err := json.Marshal(settlementDoc)
	if err != nil {
		return err
	}

	query := fmt.Sprintf(`
		INSERT INTO %s.documents (id, doctype, data, status, created_by) 
		VALUES ($1, 'MarketplaceSettlement', $2, 'Reconciled', 'system') 
		ON CONFLICT (id) DO UPDATE SET 
			data = EXCLUDED.data, 
			status = EXCLUDED.status, 
			updated_at = CURRENT_TIMESTAMP`, schema)
	_, err = db.DB.Exec(query, settlementID, settlementBytes)
	return err
}

// SeedReceivableBalance seeds Accounts Receivable for test transactions
func SeedReceivableBalance(tenantID string, amount int, documentID string) error {
	// To credit Accounts Receivable in payout, we must first debit it (debit Receivable 1300, credit Revenue 4100)
	debits := map[string]int{"1300": amount}
	credits := map[string]int{"4100": amount}
	return PostDoubleEntry(tenantID, "POSCart", documentID, PaiseMap(debits), PaiseMap(credits), "", "")
}
