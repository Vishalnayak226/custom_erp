package engines

import (
	"custom_erp/db"
	"errors"
	"fmt"
	"strings"
	"time"
)

// Stage 101.12 - store clienteling (the blueprint's M31): a worklist of
// custom-order, arrival, alteration and follow-up tasks; customer
// appointments with slot capacity; wish lists and hold requests.

// CRMWishItem is one item a customer wishes for.
type CRMWishItem struct {
	ID        int64     `json:"id"`
	ItemCode  string    `json:"item_code"`
	ItemName  string    `json:"item_name,omitempty"`
	Note      string    `json:"note,omitempty"`
	Store     string    `json:"store,omitempty"`
	CreatedBy string    `json:"created_by,omitempty"`
	CreatedAt time.Time `json:"created_at"`
}

func crmWishlistFor(schema, customerID string) ([]CRMWishItem, error) {
	rows, err := db.DB.Query(fmt.Sprintf(`SELECT w.id, w.item_code, COALESCE(i.data->>'name', i.data->>'item_name', ''), COALESCE(w.note, ''), COALESCE(w.store, ''),
		COALESCE(w.created_by, ''), w.created_at
		FROM %s.crm_wishlist w LEFT JOIN %s.documents i ON i.doctype = 'Item' AND i.data->>'code' = w.item_code AND i.deleted_at IS NULL
		WHERE w.customer_id = $1 AND w.removed_at IS NULL ORDER BY w.id DESC LIMIT 50`, schema, schema), customerID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []CRMWishItem{}
	for rows.Next() {
		var w CRMWishItem
		if err := rows.Scan(&w.ID, &w.ItemCode, &w.ItemName, &w.Note, &w.Store, &w.CreatedBy, &w.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, w)
	}
	return out, rows.Err()
}

// AddCRMWish adds an item to the customer's wish list (once).
func AddCRMWish(tenantID, customerID, itemCode, note, store, actor string) (*CRMWishItem, error) {
	schema, err := crmSchema(tenantID)
	if err != nil {
		return nil, err
	}
	c, err := crmLoadCustomer(schema, customerID)
	if errors.Is(err, ErrCRMNotFound) {
		return nil, crmNotFound("customer", customerID)
	}
	if err != nil {
		return nil, err
	}
	item, err := ResolveItemBySKU(tenantID, strings.TrimSpace(itemCode))
	if err != nil {
		return nil, crmErr(CRMCodeHold, "no item %q - scan or type its code", itemCode)
	}
	code := strField(item.Data, "code")
	if _, err := db.DB.Exec(fmt.Sprintf(`INSERT INTO %s.crm_wishlist (customer_id, item_code, note, store, created_by) VALUES ($1, $2, NULLIF($3, ''), NULLIF($4, ''), $5)
		ON CONFLICT (customer_id, item_code) WHERE removed_at IS NULL DO NOTHING`, schema), c.ID, code, strings.TrimSpace(note), store, actor); err != nil {
		return nil, err
	}
	list, err := crmWishlistFor(schema, c.ID)
	if err != nil {
		return nil, err
	}
	for i := range list {
		if list[i].ItemCode == code {
			return &list[i], nil
		}
	}
	return nil, crmErr(CRMCodeHold, "the item was not added")
}

// RemoveCRMWish takes an item off the wish list.
func RemoveCRMWish(tenantID, customerID string, id int64) error {
	schema, err := crmSchema(tenantID)
	if err != nil {
		return err
	}
	_, err = db.DB.Exec(fmt.Sprintf(`UPDATE %s.crm_wishlist SET removed_at = NOW() WHERE id = $1 AND customer_id = $2 AND removed_at IS NULL`, schema), id, customerID)
	return err
}
