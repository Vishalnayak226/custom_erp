package engines

import (
	"database/sql"
	"fmt"
	"strings"

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

// Stage 106.5: a document's own From / To, picked at the top of its screen,
// wins over the setting for that document only - and only where the
// setting's row may be changed. "" keeps the setting.
func stockOverride(txn string, from bool, override []string) string {
	rule := stockDefaultRules[txn]
	if len(override) == 0 || (from && !rule.FromEditable) || (!from && !rule.ToEditable) {
		return ""
	}
	return strings.ToUpper(strings.TrimSpace(override[0]))
}

// docStockField is a document's stock_from / stock_to (106.5), "" if none.
func docStockField(tx *sql.Tx, schema, docID, field string) string {
	var v sql.NullString
	if docID != "" {
		_ = tx.QueryRow(fmt.Sprintf(`SELECT data->>'%s' FROM %s.documents WHERE id = $1 LIMIT 1`, field, schema), docID).Scan(&v)
	}
	return v.String
}

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
// lines (negative qty) from Main at store; override is the document's own
// From (106.5).
func drawFromDefaultFromTx(tx *sql.Tx, schema, txn, store string, items []interface{}, override ...string) error {
	from := stockDefaultFrom(schema, txn)
	if o := stockOverride(txn, true, override); o != "" {
		from = o
	}
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

// StockSide is one side (From or To) of a transaction as its screen shows it
// at the top (106.5): the location it uses now, and the choices when the
// setting's row may be changed and the store has more than one that fits.
type StockSide struct {
	Code     string          `json:"code"`
	Name     string          `json:"name"`
	Editable bool            `json:"editable"`
	Options  []StockLocation `json:"options,omitempty"`
	Note     string          `json:"note,omitempty"`
}

// StockTxnHeader is a transaction's From and To at one store; a side the
// transaction has no use for is nil.
type StockTxnHeader struct {
	Txn  string     `json:"txn"`
	From *StockSide `json:"from,omitempty"`
	To   *StockSide `json:"to,omitempty"`
}

// StockTxnHeaderFor reads txn's From / To for store: the setting where the
// store uses that location, Main where it does not (what the engines do).
func StockTxnHeaderFor(tenantID, txn, store string) (StockTxnHeader, error) {
	h := StockTxnHeader{Txn: txn}
	rule, ok := stockDefaultRules[txn]
	if !ok {
		return h, stockLocationError("STKLOC-0862", fmt.Sprintf("%q is not a stock transaction.", txn))
	}
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return h, err
	}
	enabled, err := storeStockLocations(schema, store)
	if err != nil {
		return h, err
	}
	all, err := listStockLocations(db.DB, schema)
	if err != nil {
		return h, err
	}
	name := func(code string) string {
		for _, l := range all {
			if l.Code == code {
				return l.Name
			}
		}
		return code
	}
	side := func(code string, editable bool) *StockSide {
		if code == "" {
			return nil
		}
		var opts []StockLocation
		for _, l := range enabled {
			if l.Active && l.Movable && (txn != "POS Sale" || l.Code == "MAIN" || l.Sellable) {
				opts = append(opts, l)
			}
		}
		s := &StockSide{Code: code, Name: name(code)}
		used := false
		for _, l := range opts {
			used = used || l.Code == code
		}
		if editable && !used { // a location this store does not use: the engines take Main
			s.Code, s.Name = "MAIN", name("MAIN")
		}
		if editable && len(opts) > 1 {
			s.Editable, s.Options = true, opts
		}
		return s
	}
	var from, to string
	_ = db.DB.QueryRow(fmt.Sprintf(`SELECT COALESCE(from_location, ''), COALESCE(to_location, '') FROM %s.stock_location_defaults WHERE txn_type = $1`, schema), txn).Scan(&from, &to)
	h.From, h.To = side(from, rule.FromEditable), side(to, rule.ToEditable)
	if txn == "Transfer Out" && h.To != nil {
		h.To.Note = "the goods are on the road until the other store receives them"
	}
	return h, nil
}

// SetDocumentStockField stamps a document's own stock_from / stock_to (106.5)
// where the choice is made after the document exists (a return's QC).
func SetDocumentStockField(tenantID, docID, field, code string) error {
	if field != "stock_from" && field != "stock_to" {
		return fmt.Errorf("%s is not a stock location field", field)
	}
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return err
	}
	_, err = db.DB.Exec(fmt.Sprintf(`UPDATE %s.documents SET data = jsonb_set(data, $1, to_jsonb($2::text)) WHERE id = $3`, schema),
		"{"+field+"}", strings.ToUpper(strings.TrimSpace(code)), docID)
	return err
}
