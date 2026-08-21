package engines

import (
	"custom_erp/db"
	"database/sql"
	"fmt"
	"strings"
	"time"
)

// Stage 38.1 - the curated public API's read model.
//
// These readers exist so a public response is a DELIBERATE projection, not
// whatever happens to be in a document's JSON body today. The internal
// /api/v1/doc/{doctype} endpoint returns every stored field, including ones
// added last week for an internal screen; publishing that shape would make
// every future internal field an accidental public contract. Each struct below
// is the contract, and adding a field to it is a decision.
//
// The compatibility rules these obey are written down in
// docs/specs/public_api_v1.md: fields are never removed or renamed inside v1,
// timestamps are RFC3339 UTC, and identifiers are opaque strings.

// PublicPage is the shared envelope for every list response, so a client writes
// one pagination loop rather than one per endpoint.
type PublicPage struct {
	Data    interface{} `json:"data"`
	Limit   int         `json:"limit"`
	Offset  int         `json:"offset"`
	Count   int         `json:"count"`
	HasMore bool        `json:"has_more"`
}

const (
	publicAPIDefaultPageSize = 50
	publicAPIMaxPageSize     = 200
)

// NormalizePublicPaging clamps caller-supplied paging. A public endpoint must
// never accept an unbounded limit: one integration asking for everything is the
// cheapest possible way to take the database down for everyone else.
func NormalizePublicPaging(limit, offset int) (int, int) {
	if limit <= 0 {
		limit = publicAPIDefaultPageSize
	}
	if limit > publicAPIMaxPageSize {
		limit = publicAPIMaxPageSize
	}
	if offset < 0 {
		offset = 0
	}
	return limit, offset
}

// PublicItem is the curated product identity. Deliberately no cost price, no
// margin, no supplier and no internal status notes - an integration key is
// typically held by a storefront or a marketplace connector, and none of those
// need to know what we paid.
type PublicItem struct {
	Code      string  `json:"code"`
	Name      string  `json:"name"`
	Status    string  `json:"status"`
	Family    string  `json:"family,omitempty"`
	Category  string  `json:"category,omitempty"`
	Brand     string  `json:"brand,omitempty"`
	UOM       string  `json:"uom,omitempty"`
	HSNCode   string  `json:"hsn_code,omitempty"`
	Barcode   string  `json:"barcode,omitempty"`
	MRP       float64 `json:"mrp,omitempty"`
	UpdatedAt string  `json:"updated_at,omitempty"`
}

const publicItemSelect = `id,
	COALESCE(data->>'name',''), status,
	COALESCE(data->>'family',''), COALESCE(data->>'category',''), COALESCE(data->>'brand',''),
	COALESCE(data->>'uom',''), COALESCE(data->>'hsn_code',''), COALESCE(data->>'barcode',''),
	COALESCE(NULLIF(data->>'mrp','')::numeric, 0), updated_at`

func scanPublicItem(scanner interface{ Scan(...interface{}) error }) (PublicItem, error) {
	var item PublicItem
	var updatedAt sql.NullTime
	err := scanner.Scan(&item.Code, &item.Name, &item.Status, &item.Family, &item.Category,
		&item.Brand, &item.UOM, &item.HSNCode, &item.Barcode, &item.MRP, &updatedAt)
	if err != nil {
		return PublicItem{}, err
	}
	if updatedAt.Valid {
		item.UpdatedAt = updatedAt.Time.UTC().Format(time.RFC3339)
	}
	return item, nil
}

// ListPublicItems returns one page of sellable products. Cancelled and
// soft-deleted records are excluded: a public catalog read that returns
// withdrawn products is a bug in every consumer that trusts it.
func ListPublicItems(tenantID, updatedSince string, limit, offset int) (*PublicPage, error) {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return nil, err
	}
	limit, offset = NormalizePublicPaging(limit, offset)
	clauses := []string{"doctype = 'Item'", "deleted_at IS NULL", "status <> 'Cancelled'"}
	args := []interface{}{}
	if strings.TrimSpace(updatedSince) != "" {
		parsed, parseErr := time.Parse(time.RFC3339, strings.TrimSpace(updatedSince))
		if parseErr != nil {
			return nil, &ValidationError{Code: "GLOBAL-0002", SubFor: "updated_since", Message: "updated_since must be an RFC3339 timestamp, for example 2026-08-01T00:00:00Z"}
		}
		args = append(args, parsed.UTC())
		clauses = append(clauses, fmt.Sprintf("updated_at >= $%d", len(args)))
	}
	// One extra row is fetched purely to answer has_more without a COUNT(*)
	// over the whole table on every page.
	args = append(args, limit+1, offset)
	rows, err := db.DB.Query(fmt.Sprintf(`SELECT %s FROM %s.documents WHERE %s
		ORDER BY id LIMIT $%d OFFSET $%d`, publicItemSelect, schema, strings.Join(clauses, " AND "), len(args)-1, len(args)), args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []PublicItem{}
	for rows.Next() {
		item, scanErr := scanPublicItem(rows)
		if scanErr != nil {
			return nil, scanErr
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	hasMore := len(items) > limit
	if hasMore {
		items = items[:limit]
	}
	return &PublicPage{Data: items, Limit: limit, Offset: offset, Count: len(items), HasMore: hasMore}, nil
}

// ErrPublicNotFound is returned for any resource a public caller asked for and
// is allowed to ask for, but which does not exist.
var ErrPublicNotFound = &ValidationError{Code: "GLOBAL-0004", Message: "resource not found"}

func GetPublicItem(tenantID, code string) (*PublicItem, error) {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return nil, err
	}
	row := db.DB.QueryRow(fmt.Sprintf(`SELECT %s FROM %s.documents
		WHERE doctype = 'Item' AND id = $1 AND deleted_at IS NULL AND status <> 'Cancelled'`, publicItemSelect, schema), strings.TrimSpace(code))
	item, err := scanPublicItem(row)
	if err == sql.ErrNoRows {
		return nil, ErrPublicNotFound
	}
	if err != nil {
		return nil, err
	}
	return &item, nil
}

// PublicInventoryLevel reports availability only. The internal breakdown
// (safety stock, QC hold, damaged, channel buffer) is deliberately not
// published: those are operational decisions, and exposing them tells an
// outside party how much stock we are holding back and why.
type PublicInventoryLevel struct {
	SKU          string `json:"sku"`
	LocationCode string `json:"location_code"`
	Available    int    `json:"available_to_sell"`
	AsOf         string `json:"as_of"`
}

// ListPublicInventory returns availability for one SKU across locations, or for
// one SKU at one location. A SKU filter is mandatory: an unbounded "give me
// every level everywhere" read is a stock-position export, not an availability
// lookup, and it is not part of this surface.
func ListPublicInventory(tenantID, sku, locationCode string) ([]PublicInventoryLevel, error) {
	sku = strings.TrimSpace(sku)
	if sku == "" {
		return nil, &ValidationError{Code: "GLOBAL-0001", SubFor: "sku", Message: "a sku is required"}
	}
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return nil, err
	}
	args := []interface{}{sku}
	locationClause := ""
	if strings.TrimSpace(locationCode) != "" {
		args = append(args, strings.TrimSpace(locationCode))
		locationClause = " AND location_code = $2"
	}
	rows, err := db.DB.Query(fmt.Sprintf(`SELECT sku, location_code, available, reserved,
		safety_stock, blocked, qc_hold, damaged, channel_buffer, hold_qty, updated_at
		FROM %s.inventory_availability WHERE sku = $1%s ORDER BY location_code`, schema, locationClause), args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []PublicInventoryLevel{}
	for rows.Next() {
		var level PublicInventoryLevel
		var available, reserved, safetyStock, blocked, qcHold, damaged, channelBuffer, held int
		var updatedAt sql.NullTime
		if err := rows.Scan(&level.SKU, &level.LocationCode, &available, &reserved, &safetyStock,
			&blocked, &qcHold, &damaged, &channelBuffer, &held, &updatedAt); err != nil {
			return nil, err
		}
		// The same computeATS choke point every internal caller uses, so a
		// public read can never disagree with what the order path will accept.
		level.Available = computeATS(available, reserved, safetyStock, blocked, qcHold, damaged, channelBuffer, held)
		if level.Available < 0 {
			level.Available = 0
		}
		if updatedAt.Valid {
			level.AsOf = updatedAt.Time.UTC().Format(time.RFC3339)
		}
		out = append(out, level)
	}
	return out, rows.Err()
}

// PublicOrderStatus is the order-tracking projection: enough for a storefront
// to tell a customer where their order is, and nothing else. No pricing, no
// margin, no customer PII beyond what the caller already supplied to place the
// order.
type PublicOrderStatus struct {
	OrderID        string                  `json:"order_id"`
	ChannelOrderID string                  `json:"channel_order_id,omitempty"`
	Channel        string                  `json:"channel,omitempty"`
	Status         string                  `json:"status"`
	HoldReason     string                  `json:"hold_reason,omitempty"`
	PlacedAt       string                  `json:"placed_at,omitempty"`
	UpdatedAt      string                  `json:"updated_at,omitempty"`
	Lines          []PublicOrderStatusLine `json:"lines"`
	Shipments      []PublicOrderShipment   `json:"shipments"`
}

type PublicOrderStatusLine struct {
	SKU      string  `json:"sku"`
	Quantity float64 `json:"quantity"`
	Status   string  `json:"status,omitempty"`
}

type PublicOrderShipment struct {
	TrackingNumber string `json:"tracking_number,omitempty"`
	Carrier        string `json:"carrier,omitempty"`
	Status         string `json:"status,omitempty"`
	DispatchedAt   string `json:"dispatched_at,omitempty"`
}

// GetPublicOrderStatus resolves an order by its own id or by the channel order
// id the integration knows it as. A marketplace connector holds its own
// identifier, not ours, so requiring our id would make the endpoint useless to
// the caller most likely to need it.
func GetPublicOrderStatus(tenantID, identifier string) (*PublicOrderStatus, error) {
	identifier = strings.TrimSpace(identifier)
	if identifier == "" {
		return nil, ErrPublicNotFound
	}
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return nil, err
	}
	var status PublicOrderStatus
	var createdAt, updatedAt sql.NullTime
	err = db.DB.QueryRow(fmt.Sprintf(`SELECT id, status,
		COALESCE(data->>'channel_order_id',''), COALESCE(data->>'channel',''),
		COALESCE(data->>'hold_reason',''), created_at, updated_at
		FROM %s.documents
		WHERE doctype = 'SalesOrder' AND deleted_at IS NULL
		  AND (id = $1 OR data->>'channel_order_id' = $1)
		ORDER BY CASE WHEN id = $1 THEN 0 ELSE 1 END, created_at DESC LIMIT 1`, schema), identifier).
		Scan(&status.OrderID, &status.Status, &status.ChannelOrderID, &status.Channel,
			&status.HoldReason, &createdAt, &updatedAt)
	if err == sql.ErrNoRows {
		return nil, ErrPublicNotFound
	}
	if err != nil {
		return nil, err
	}
	if createdAt.Valid {
		status.PlacedAt = createdAt.Time.UTC().Format(time.RFC3339)
	}
	if updatedAt.Valid {
		status.UpdatedAt = updatedAt.Time.UTC().Format(time.RFC3339)
	}

	status.Lines = []PublicOrderStatusLine{}
	lineRows, err := db.DB.Query(fmt.Sprintf(`SELECT COALESCE(data->>'sku',''),
		COALESCE(NULLIF(data->>'qty','')::numeric, 0), COALESCE(NULLIF(data->>'line_status',''), status)
		FROM %s.documents WHERE doctype = 'SalesOrderLine' AND deleted_at IS NULL
		  AND data->>'order_id' = $1 ORDER BY id`, schema), status.OrderID)
	if err != nil {
		return nil, err
	}
	defer lineRows.Close()
	for lineRows.Next() {
		var line PublicOrderStatusLine
		if err := lineRows.Scan(&line.SKU, &line.Quantity, &line.Status); err != nil {
			return nil, err
		}
		status.Lines = append(status.Lines, line)
	}
	if err := lineRows.Err(); err != nil {
		return nil, err
	}

	status.Shipments = []PublicOrderShipment{}
	shipmentRows, err := db.DB.Query(fmt.Sprintf(`SELECT COALESCE(data->>'tracking_number',''),
		COALESCE(data->>'carrier',''), status, updated_at
		FROM %s.documents WHERE doctype = 'LogisticsBooking' AND deleted_at IS NULL
		  AND data->>'order_id' = $1 ORDER BY created_at`, schema), status.OrderID)
	if err != nil {
		return nil, err
	}
	defer shipmentRows.Close()
	for shipmentRows.Next() {
		var shipment PublicOrderShipment
		var dispatchedAt sql.NullTime
		if err := shipmentRows.Scan(&shipment.TrackingNumber, &shipment.Carrier, &shipment.Status, &dispatchedAt); err != nil {
			return nil, err
		}
		if dispatchedAt.Valid {
			shipment.DispatchedAt = dispatchedAt.Time.UTC().Format(time.RFC3339)
		}
		status.Shipments = append(status.Shipments, shipment)
	}
	return &status, shipmentRows.Err()
}
