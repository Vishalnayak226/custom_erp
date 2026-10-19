package engines

import (
	"database/sql"
	"fmt"

	"custom_erp/db"
)

// Stage 106.4 (user, 2026-10-19: "Everything should be editable" - Stock
// Locations > Transactions). The rows that were fixed text now choose a real
// location, and the engines move stock by them:
//
//   POS Sale From, Purchase Return From (accepted stock), Transfer Out From -
//     the outbound transaction takes the client's chosen location first: as
//     much as that location holds of the line is moved into Main inside the
//     same database transaction, then the engine takes the line from Main
//     exactly as before. A location holding none, or one the store does not
//     use, changes nothing, so a transaction is never refused over this.
//   Sales Return To - where a return QC-checked Good lands (applyReceiptDefaultTx,
//     the same step Goods Receipt and Transfer In use). Damaged / QC Hold
//     results still go to Damage / QC Hold.
//   Transfer Out To stays In-Transit: that is how the other store receives it.

func stockDefaultFrom(schema, txn string) string {
	var from string
	_ = db.DB.QueryRow(fmt.Sprintf(`SELECT from_location FROM %s.stock_location_defaults WHERE txn_type = $1`, schema), txn).Scan(&from)
	return from
}

// storeLocationFor is code as a location the store uses and stock can move
// through, or nil.
func storeLocationFor(schema, store, code string) *StockLocation {
	enabled, err := storeStockLocations(schema, store)
	if err != nil {
		return nil
	}
	for i := range enabled {
		if enabled[i].Code == code && enabled[i].Movable && enabled[i].Active {
			return &enabled[i]
		}
	}
	return nil
}

// drawFromDefaultFromTx runs just before an outbound transaction posts its
// lines (negative qty) from Main at store.
func drawFromDefaultFromTx(tx *sql.Tx, schema, txn, store string, items []interface{}) error {
	from := stockDefaultFrom(schema, txn)
	if from == "" || from == "MAIN" {
		return nil
	}
	src := storeLocationFor(schema, store, from)
	if src == nil || src.Bucket == "available" || (txn == "POS Sale" && !src.Sellable) {
		return nil
	}
	main := StockLocation{Code: "MAIN", Bucket: "available"}
	for _, it := range items {
		m, ok := it.(map[string]interface{})
		if !ok {
			continue
		}
		sku := stockItemSKU(it)
		want := -int(numFromInterface(m["qty"]))
		if sku == "" || want <= 0 {
			continue
		}
		have, err := locationQtyTx(tx, schema, sku, store, *src)
		if err != nil {
			return err
		}
		if n := minInt(have, want); n > 0 {
			if err := applyBucketDeltaTx(tx, schema, sku, store, *src, -n); err != nil {
				return err
			}
			if err := applyBucketDeltaTx(tx, schema, sku, store, main, n); err != nil {
				return err
			}
		}
	}
	return nil
}

// locationQtyTx is how much of sku a non-Main location holds at store, locked.
func locationQtyTx(tx *sql.Tx, schema, sku, store string, l StockLocation) (int, error) {
	if l.Bucket == "custom" {
		var have int
		err := tx.QueryRow(fmt.Sprintf(`SELECT COALESCE(qty, 0) FROM %s.stock_location_balance WHERE sku = $1 AND location_code = $2 AND stock_location = $3 FOR UPDATE`, schema),
			sku, store, l.Code).Scan(&have)
		if err == sql.ErrNoRows {
			return 0, nil
		}
		return have, err
	}
	b, found, err := readBuckets(tx, schema, sku, store, true)
	if err != nil || !found {
		return 0, err
	}
	return b.of(l.Bucket), nil
}
