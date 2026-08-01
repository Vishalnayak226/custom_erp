package engines

import (
	"custom_erp/db"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
)

// ErrItemNotFound is returned by ResolveItemBySKU when nothing matches the
// given SKU on any of its three accepted identifiers. Callers that treat a
// missing Item as a business outcome rather than a failure (sticker printing,
// channel-order SKU mapping) test for it with errors.Is instead of
// string-matching the message.
var ErrItemNotFound = errors.New("item not found")

// ResolvedItem is one Item master row resolved from a user-facing SKU string.
// MatchedOn names which identifier actually matched ("code"/"barcode"/"id") -
// useful in error messages and tests, since the same string can legitimately
// be any of the three.
type ResolvedItem struct {
	ID        string
	Status    string
	Data      map[string]interface{}
	MatchedOn string
}

// ResolveItemBySKU is the shared Item lookup for every place a human-entered
// or scanned SKU has to become a real Item record - POS checkout, PurchaseOrder
// GST resolution, sticker printing, channel-order SKU mapping.
//
// Stage 30.1.1: these call sites each used to pick one identifier and match
// only that. GetItemGSTInfo matched `id`, which is a generated UUID for any
// item created through the UI, so the Code and the Barcode a cashier actually
// has in front of them both failed checkout with "item not found" - and the
// POS field's own typeahead fills in `code` (attachTypeahead's default
// valueFields, public/app.js), making the failure certain rather than
// occasional. validateOrderChain had the mirror-image bug, matching only
// `code`.
//
// Match order is code -> barcode -> id, resolved in one query so the priority
// is deterministic when two different items collide on one string (e.g. one
// item's barcode equals another's code): the Code the user typed wins, and
// only then the barcode, and only then the raw internal id. Soft-deleted items
// (Stage 17.1) never match.
func ResolveItemBySKU(tenantID, sku string) (*ResolvedItem, error) {
	if sku == "" {
		return nil, ErrItemNotFound
	}
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return nil, err
	}

	var (
		id        string
		status    string
		dataStr   string
		matchedOn string
	)
	err = db.DB.QueryRow(fmt.Sprintf(`
		SELECT id, status, data,
		       CASE WHEN data->>'code' = $1 THEN 'code'
		            WHEN data->>'barcode' = $1 THEN 'barcode'
		            ELSE 'id' END AS matched_on
		FROM %s.documents
		WHERE doctype = 'Item' AND deleted_at IS NULL
		  AND (data->>'code' = $1 OR data->>'barcode' = $1 OR id = $1)
		ORDER BY CASE WHEN data->>'code' = $1 THEN 0
		              WHEN data->>'barcode' = $1 THEN 1
		              ELSE 2 END
		LIMIT 1`, schema), sku).Scan(&id, &status, &dataStr, &matchedOn)
	if err == sql.ErrNoRows {
		return nil, ErrItemNotFound
	}
	if err != nil {
		return nil, err
	}

	item := &ResolvedItem{ID: id, Status: status, MatchedOn: matchedOn, Data: map[string]interface{}{}}
	if err := json.Unmarshal([]byte(dataStr), &item.Data); err != nil {
		return nil, fmt.Errorf("item '%s' has unreadable data: %v", sku, err)
	}
	return item, nil
}
