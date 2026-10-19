package engines

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	"custom_erp/db"

	"github.com/lib/pq"
)

// Stage 85.B (user, 2026-10-18): "location storewise ... Saleable, Non
// saleable. I should have that control. It is not wms depenednt ... WMS has
// to adopt and know that from erp."
//
// Stock inside a store sits in the buckets inventory_availability already
// has; this is the master that names them, says which sell, and says which
// each store uses (db/migrations_stage85_2_stock_locations.sql has the full
// map). Two kinds of bucket:
//
//   pools outside `available` - inward, in_transit, qc_hold, damaged, rtv.
//     Every writer (returns QC, goods-receipt QC, transfer-in damage) adds to
//     on_hand and the pool, never to available.
//   carve-outs inside `available` - hold_qty (hold codes) and blocked, which
//     is the sum of the client's own locations (stock_location_balance).
//
// So the store's Main stock is available - blocked - hold_qty, and what can
// be sold is Main plus any pool the client has marked sellable (QC Hold and
// Damage may be; the others are by their nature not). computeATS is that
// formula, for every caller, from atsPolicyFor.

// Built-in buckets and what the client may change about them.
var stockBuckets = map[string]struct {
	pool             bool   // outside available (else a carve-out of it)
	sellableEditable bool   // the client decides whether it sells
	movable          bool   // Move Stock may take stock in / out
	wmsCondition     string // the WMS bin condition it stands for
	notPhysical      bool   // its stock is not in the store (Stage 107)
	physicalEditable bool   // the client decides whether it is in the store
}{
	"available":  {pool: false, movable: true, wmsCondition: "Good"},
	"inward":     {pool: true, movable: true},
	"in_transit": {pool: true, notPhysical: true},
	"qc_hold":    {pool: true, sellableEditable: true, movable: true, wmsCondition: "QC-Hold"},
	"damaged":    {pool: true, sellableEditable: true, movable: true, wmsCondition: "Damaged"},
	"hold_qty":   {pool: false},
	"rtv":        {pool: true, movable: true, wmsCondition: "RTV"},
	"custom":     {pool: false, movable: true, physicalEditable: true},
}

// StockLocation is one row of the master.
type StockLocation struct {
	Code             string `json:"code"`
	Name             string `json:"name"`
	Bucket           string `json:"bucket"`
	Sellable         bool   `json:"sellable"`
	AppliesTo        string `json:"applies_to"`
	DefaultForNew    bool   `json:"default_for_new"`
	Active           bool   `json:"active"`
	DisplayOrder     int    `json:"display_order"`
	Description      string `json:"description"`
	SellableEditable bool   `json:"sellable_editable"`
	Movable          bool   `json:"movable"`
	WMSCondition     string `json:"wms_condition"`
	// Stage 107: stock here is not physically in the store (In-Transit; a
	// client location such as "At repair" may be marked so too).
	NotPhysical      bool `json:"not_physical"`
	PhysicalEditable bool `json:"physical_editable"`
}

func (l *StockLocation) fill() {
	b := stockBuckets[l.Bucket]
	l.SellableEditable, l.Movable, l.WMSCondition = b.sellableEditable, b.movable, b.wmsCondition
	l.PhysicalEditable = b.physicalEditable
	if !b.physicalEditable {
		l.NotPhysical = b.notPhysical
	}
}

// ATSPolicy says which pools outside `available` count as sellable.
type ATSPolicy struct {
	QCHoldSellable  bool
	DamagedSellable bool
}

type atsPolicyEntry struct {
	policy ATSPolicy
	at     time.Time
}

var (
	atsPolicyMu    sync.RWMutex
	atsPolicyCache = map[string]atsPolicyEntry{}
)

const atsPolicyTTL = 30 * time.Second

// atsPolicyFor is the tenant's sellable policy, cached briefly. A schema the
// Stage 85 migration has not reached sells Main only - today's behaviour.
func atsPolicyFor(schema string) ATSPolicy {
	atsPolicyMu.RLock()
	e, ok := atsPolicyCache[schema]
	atsPolicyMu.RUnlock()
	if ok && time.Since(e.at) < atsPolicyTTL {
		return e.policy
	}
	var p ATSPolicy
	if rows, err := db.DB.Query(fmt.Sprintf(
		`SELECT bucket, sellable FROM %s.stock_locations WHERE active AND bucket IN ('qc_hold', 'damaged')`, schema)); err == nil {
		for rows.Next() {
			var bucket string
			var sellable bool
			if rows.Scan(&bucket, &sellable) == nil {
				switch bucket {
				case "qc_hold":
					p.QCHoldSellable = sellable
				case "damaged":
					p.DamagedSellable = sellable
				}
			}
		}
		rows.Close()
	}
	atsPolicyMu.Lock()
	atsPolicyCache[schema] = atsPolicyEntry{policy: p, at: time.Now()}
	atsPolicyMu.Unlock()
	return p
}

func invalidateATSPolicy(schema string) {
	atsPolicyMu.Lock()
	delete(atsPolicyCache, schema)
	atsPolicyMu.Unlock()
}

// ListStockLocations is the whole master, in display order.
func ListStockLocations(tenantID string) ([]StockLocation, error) {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return nil, err
	}
	return listStockLocations(db.DB, schema)
}

type queryer interface {
	Query(query string, args ...interface{}) (*sql.Rows, error)
}

func listStockLocations(q queryer, schema string) ([]StockLocation, error) {
	rows, err := q.Query(fmt.Sprintf(`
		SELECT code, name, bucket, sellable, applies_to, default_for_new, active, display_order, description, not_physical
		  FROM %s.stock_locations ORDER BY display_order, name`, schema))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []StockLocation{}
	for rows.Next() {
		var l StockLocation
		if err := rows.Scan(&l.Code, &l.Name, &l.Bucket, &l.Sellable, &l.AppliesTo, &l.DefaultForNew, &l.Active, &l.DisplayOrder, &l.Description, &l.NotPhysical); err != nil {
			return nil, err
		}
		l.fill()
		out = append(out, l)
	}
	return out, rows.Err()
}

func stockLocationError(code, msg string) error { return &ValidationError{Code: code, Message: msg} }

var stockLocationCodePattern = regexp.MustCompile(`^[A-Z0-9][A-Z0-9_-]{1,29}$`)

// bucketHasStock reports whether any store holds stock in a location.
func bucketHasStock(schema string, l StockLocation, store string) (bool, error) {
	var n int
	var err error
	if l.Bucket == "custom" {
		q := fmt.Sprintf(`SELECT COALESCE(SUM(qty), 0) FROM %s.stock_location_balance WHERE stock_location = $1`, schema)
		args := []interface{}{l.Code}
		if store != "" {
			q += ` AND location_code = $2`
			args = append(args, store)
		}
		err = db.DB.QueryRow(q, args...).Scan(&n)
	} else if l.Bucket != "available" {
		q := fmt.Sprintf(`SELECT COALESCE(SUM(%s), 0) FROM %s.inventory_availability`, pq.QuoteIdentifier(l.Bucket), schema)
		args := []interface{}{}
		if store != "" {
			q += ` WHERE location_code = $1`
			args = append(args, store)
		}
		err = db.DB.QueryRow(q, args...).Scan(&n)
	}
	return n > 0, err
}

// notPhysicalCustomQty is one item's stock at a store held in the client's
// own locations marked not physically in the store (Stage 107). It is inside
// on_hand (a carve-out of available) but not on the shelf. In-Transit needs no
// such step - it is already outside on_hand.
func notPhysicalCustomQty(schema, sku, store string) int {
	var n int
	_ = db.DB.QueryRow(fmt.Sprintf(`
		SELECT COALESCE(SUM(b.qty), 0) FROM %[1]s.stock_location_balance b
		  JOIN %[1]s.stock_locations l ON l.code = b.stock_location
		 WHERE b.sku = $1 AND b.location_code = $2 AND l.bucket = 'custom' AND l.not_physical`, schema),
		sku, store).Scan(&n)
	return n
}

// SaveStockLocation creates a client location or changes one. A built-in
// location keeps its bucket; only QC Hold and Damage take a Sellable choice;
// Main always sells and is always on.
func SaveStockLocation(tenantID string, in StockLocation, actor string) error {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return err
	}
	in.Code = strings.ToUpper(strings.TrimSpace(in.Code))
	in.Name = strings.Join(strings.Fields(in.Name), " ")
	if in.Name == "" || len(in.Name) > 80 {
		return stockLocationError("STKLOC-0860", "Give the location a name (up to 80 characters).")
	}
	switch in.AppliesTo {
	case "Store", "Warehouse", "Both":
	default:
		in.AppliesTo = "Both"
	}
	all, err := ListStockLocations(tenantID)
	if err != nil {
		return err
	}
	var existing *StockLocation
	for i := range all {
		if all[i].Code == in.Code {
			existing = &all[i]
		} else if strings.EqualFold(all[i].Name, in.Name) {
			return stockLocationError("STKLOC-0860", fmt.Sprintf("%s already uses the name %q.", all[i].Code, in.Name))
		}
	}
	if existing == nil {
		if !stockLocationCodePattern.MatchString(in.Code) {
			return stockLocationError("STKLOC-0860", "The code is 2-30 capital letters, digits, '-' or '_' (e.g. ALTERATION).")
		}
		// A new location is always the client's own: the built-in buckets
		// each have their one location already.
		in.Bucket, in.Sellable = "custom", false
		in.Active = true
		if in.DisplayOrder == 0 {
			in.DisplayOrder = 100
		}
	} else {
		in.Bucket = existing.Bucket
		b := stockBuckets[in.Bucket]
		if !b.physicalEditable {
			in.NotPhysical = b.notPhysical
		}
		if !b.sellableEditable {
			in.Sellable = existing.Sellable
		}
		if in.Bucket == "available" {
			in.Sellable, in.Active = true, true
		}
		if existing.Active && !in.Active {
			if has, err := bucketHasStock(schema, *existing, ""); err != nil {
				return err
			} else if has {
				return stockLocationError("STKLOC-0861", fmt.Sprintf("%s still holds stock. Move it out (Stock -> Move Stock) before switching the location off.", existing.Name))
			}
		}
	}
	if _, err := db.DB.Exec(fmt.Sprintf(`
		INSERT INTO %s.stock_locations (code, name, bucket, sellable, applies_to, default_for_new, active, display_order, description, updated_by, updated_at, not_physical)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12)
		ON CONFLICT (code) DO UPDATE SET name = EXCLUDED.name, sellable = EXCLUDED.sellable, applies_to = EXCLUDED.applies_to,
		  default_for_new = EXCLUDED.default_for_new, active = EXCLUDED.active, display_order = EXCLUDED.display_order,
		  description = EXCLUDED.description, updated_by = EXCLUDED.updated_by, updated_at = EXCLUDED.updated_at,
		  not_physical = EXCLUDED.not_physical`, schema),
		in.Code, in.Name, in.Bucket, in.Sellable, in.AppliesTo, in.DefaultForNew, in.Active, in.DisplayOrder,
		strings.TrimSpace(in.Description), actor, time.Now(), in.NotPhysical); err != nil {
		return err
	}
	invalidateATSPolicy(schema)
	LogAuditEvent(tenantID, actor, "INVENTORY", "STOCK_LOCATION_SAVED", fmt.Sprintf("Stock location %s (%s): sellable=%v not_physical=%v active=%v", in.Code, in.Name, in.Sellable, in.NotPhysical, in.Active))
	return nil
}

// storeKind is Store or Warehouse for the "applies to" filter (HO counts as a
// warehouse: it holds stock, it does not sell over a counter).
func storeKind(schema, store string) string {
	var t string
	_ = db.DB.QueryRow(fmt.Sprintf(`SELECT COALESCE(data->>'type', '') FROM %s.documents WHERE doctype = 'Location' AND id = $1`, schema), store).Scan(&t)
	if t == "Store" {
		return "Store"
	}
	return "Warehouse"
}

// StoreStockLocations is the locations a store uses: its own list when one
// is saved, otherwise every location marked for new stores. Main is always
// there; a location that is off, or does not apply to this kind of store, is
// not.
func StoreStockLocations(tenantID, store string) ([]StockLocation, error) {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return nil, err
	}
	return storeStockLocations(schema, store)
}

func storeStockLocations(schema, store string) ([]StockLocation, error) {
	all, err := listStockLocations(db.DB, schema)
	if err != nil {
		return nil, err
	}
	chosen := map[string]bool{}
	rows, err := db.DB.Query(fmt.Sprintf(`SELECT stock_location FROM %s.store_stock_locations WHERE location_code = $1`, schema), store)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var c string
		if rows.Scan(&c) == nil {
			chosen[c] = true
		}
	}
	rows.Close()
	kind := storeKind(schema, store)
	out := []StockLocation{}
	for _, l := range all {
		if !l.Active || (l.AppliesTo != "Both" && l.AppliesTo != kind && l.Bucket != "available") {
			continue
		}
		if l.Bucket == "available" || (len(chosen) == 0 && l.DefaultForNew) || chosen[l.Code] {
			out = append(out, l)
		}
	}
	return out, nil
}

// StoreLocationMatrix is every store's own list (nil = uses the defaults),
// for the Stores tab.
func StoreLocationMatrix(tenantID string) (map[string][]string, error) {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return nil, err
	}
	rows, err := db.DB.Query(fmt.Sprintf(`SELECT location_code, stock_location FROM %s.store_stock_locations ORDER BY 1, 2`, schema))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string][]string{}
	for rows.Next() {
		var store, code string
		if err := rows.Scan(&store, &code); err != nil {
			return nil, err
		}
		out[store] = append(out[store], code)
	}
	return out, rows.Err()
}

// SetStoreStockLocations saves which locations a store uses. A location that
// still holds stock at the store cannot be taken away from it.
func SetStoreStockLocations(tenantID, store string, codes []string, actor string) error {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return err
	}
	store = strings.TrimSpace(store)
	if _, err := cleanStoreList(schema, []string{store}); err != nil {
		return err
	}
	all, err := listStockLocations(db.DB, schema)
	if err != nil {
		return err
	}
	byCode := map[string]StockLocation{}
	for _, l := range all {
		byCode[l.Code] = l
	}
	want := map[string]bool{}
	for _, c := range codes {
		c = strings.ToUpper(strings.TrimSpace(c))
		l, ok := byCode[c]
		if !ok {
			return stockLocationError("STKLOC-0860", fmt.Sprintf("%s is not a stock location.", c))
		}
		if l.Bucket != "available" {
			want[c] = true
		}
	}
	current, err := storeStockLocations(schema, store)
	if err != nil {
		return err
	}
	for _, l := range current {
		if l.Bucket == "available" || want[l.Code] {
			continue
		}
		if has, err := bucketHasStock(schema, l, store); err != nil {
			return err
		} else if has {
			return stockLocationError("STKLOC-0861", fmt.Sprintf("%s still holds stock at this store. Move it out before taking the location away.", l.Name))
		}
	}
	tx, err := db.DB.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.Exec(fmt.Sprintf(`DELETE FROM %s.store_stock_locations WHERE location_code = $1`, schema), store); err != nil {
		return err
	}
	// Main is written too, so a store whose list is "Main only" has a row
	// and does not fall back to the defaults.
	want["MAIN"] = true
	for c := range want {
		if _, err := tx.Exec(fmt.Sprintf(`INSERT INTO %s.store_stock_locations (location_code, stock_location) VALUES ($1, $2)`, schema), store, c); err != nil {
			return err
		}
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	LogAuditEvent(tenantID, actor, "INVENTORY", "STORE_STOCK_LOCATIONS_SET", fmt.Sprintf("Store %s uses %v", store, stockLocationCodes(want)))
	return nil
}

func stockLocationCodes(m map[string]bool) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// StockLocationDefault is one transaction's from / to, and which of the two
// the client may change. The rest are how the transaction works (a sale
// always sells from what is sellable; returns go where QC decides).
type StockLocationDefault struct {
	TxnType      string `json:"txn_type"`
	From         string `json:"from_location"`
	To           string `json:"to_location"`
	FromEditable bool   `json:"from_editable"`
	ToEditable   bool   `json:"to_editable"`
	Note         string `json:"note"`
}

var stockDefaultRules = map[string]StockLocationDefault{
	// Stage 106.4: POS Sale / Transfer Out / Purchase Return From and Sales
	// Return To editable (stock_location_txn_defaults.go); Transfer Out To
	// stays In-Transit, which is how the other store receives it.
	"POS Sale":        {FromEditable: true, Note: "Where a sale takes stock first (a location that sells), then Main and any other location marked sellable."},
	"Goods Receipt":   {ToEditable: true, Note: "Where received goods land. Choose Inward if goods are checked before they go on sale. Lines the receipt rejects still go to QC Hold / Damage."},
	"Transfer Out":    {FromEditable: true, Note: "Where dispatch takes stock first, then Main. To is always In-Transit: the goods are on the road until the other store receives them."},
	"Transfer In":     {ToEditable: true, Note: "Where goods from another store land when received."},
	"Sales Return":    {ToEditable: true, Note: "Where a return checked Good lands. Damaged and QC Hold results still go to Damage and QC Hold."},
	"Purchase Return": {FromEditable: true, Note: "Where accepted goods going back to a vendor are taken from first, then Main. Rejected / damaged lines leave their own location."},
	"Damage":          {FromEditable: true, ToEditable: true, Note: "The usual move when stock is found damaged."},
	"Stock Move":      {FromEditable: true, ToEditable: true, Note: "What Move Stock starts with."},
}

// ListStockLocationDefaults is every transaction's from / to.
func ListStockLocationDefaults(tenantID string) ([]StockLocationDefault, error) {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return nil, err
	}
	rows, err := db.DB.Query(fmt.Sprintf(`SELECT txn_type, from_location, to_location FROM %s.stock_location_defaults ORDER BY txn_type`, schema))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []StockLocationDefault{}
	for rows.Next() {
		var d StockLocationDefault
		if err := rows.Scan(&d.TxnType, &d.From, &d.To); err != nil {
			return nil, err
		}
		rule := stockDefaultRules[d.TxnType]
		d.FromEditable, d.ToEditable, d.Note = rule.FromEditable, rule.ToEditable, rule.Note
		out = append(out, d)
	}
	return out, rows.Err()
}

func stockDefaultTo(schema, txn string) string {
	var to string
	_ = db.DB.QueryRow(fmt.Sprintf(`SELECT to_location FROM %s.stock_location_defaults WHERE txn_type = $1`, schema), txn).Scan(&to)
	return to
}

// SetStockLocationDefault changes a transaction's from / to where it may be.
func SetStockLocationDefault(tenantID, txn, from, to, actor string) error {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return err
	}
	rule, ok := stockDefaultRules[txn]
	if !ok {
		return stockLocationError("STKLOC-0862", fmt.Sprintf("%q is not a stock transaction.", txn))
	}
	all, err := listStockLocations(db.DB, schema)
	if err != nil {
		return err
	}
	byCode := map[string]StockLocation{}
	for _, l := range all {
		byCode[l.Code] = l
	}
	var cur StockLocationDefault
	_ = db.DB.QueryRow(fmt.Sprintf(`SELECT from_location, to_location FROM %s.stock_location_defaults WHERE txn_type = $1`, schema), txn).Scan(&cur.From, &cur.To)
	check := func(code string, editable bool, current string) (string, error) {
		code = strings.ToUpper(strings.TrimSpace(code))
		if !editable {
			return current, nil
		}
		l, ok := byCode[code]
		if !ok || !l.Active || !l.Movable {
			return "", stockLocationError("STKLOC-0862", fmt.Sprintf("%s cannot be used here - pick Main, Inward, QC Hold, Damage, Purchase Return or one of your own locations.", code))
		}
		return code, nil
	}
	if from, err = check(from, rule.FromEditable, cur.From); err != nil {
		return err
	}
	if to, err = check(to, rule.ToEditable, cur.To); err != nil {
		return err
	}
	if txn == "POS Sale" && from != "MAIN" && !byCode[from].Sellable {
		return stockLocationError("STKLOC-0862", fmt.Sprintf("%s does not sell - a sale can only take stock from a location marked Sells.", byCode[from].Name))
	}
	if rule.FromEditable && rule.ToEditable && from == to {
		return stockLocationError("STKLOC-0862", "From and To must be different locations.")
	}
	if _, err := db.DB.Exec(fmt.Sprintf(`
		INSERT INTO %s.stock_location_defaults (txn_type, from_location, to_location, updated_by, updated_at) VALUES ($1, $2, $3, $4, $5)
		ON CONFLICT (txn_type) DO UPDATE SET from_location = EXCLUDED.from_location, to_location = EXCLUDED.to_location,
		  updated_by = EXCLUDED.updated_by, updated_at = EXCLUDED.updated_at`, schema), txn, from, to, actor, time.Now()); err != nil {
		return err
	}
	LogAuditEvent(tenantID, actor, "INVENTORY", "STOCK_LOCATION_DEFAULT_SET", fmt.Sprintf("%s: from %q to %q", txn, from, to))
	return nil
}

// LocationQty is one location's stock of one item at one store.
type LocationQty struct {
	Code     string `json:"code"`
	Name     string `json:"name"`
	Qty      int    `json:"qty"`
	Sellable bool   `json:"sellable"`
	Enabled  bool   `json:"enabled"`
}

type bucketRow struct {
	available, blocked, hold, inward, inTransit, qc, damaged, rtv, reserved int
}

func (b bucketRow) of(bucket string) int {
	switch bucket {
	case "available":
		return b.available - b.blocked - b.hold
	case "inward":
		return b.inward
	case "in_transit":
		return b.inTransit
	case "qc_hold":
		return b.qc
	case "damaged":
		return b.damaged
	case "hold_qty":
		return b.hold
	case "rtv":
		return b.rtv
	}
	return 0
}

func readBuckets(q interface {
	QueryRow(string, ...interface{}) *sql.Row
}, schema, sku, store string, lock bool) (bucketRow, bool, error) {
	var b bucketRow
	query := fmt.Sprintf(`SELECT available, blocked, hold_qty, inward, in_transit, qc_hold, damaged, rtv, reserved
		FROM %s.inventory_availability WHERE sku = $1 AND location_code = $2`, schema)
	if lock {
		query += " FOR UPDATE"
	}
	err := q.QueryRow(query, sku, store).Scan(&b.available, &b.blocked, &b.hold, &b.inward, &b.inTransit, &b.qc, &b.damaged, &b.rtv, &b.reserved)
	if errors.Is(err, sql.ErrNoRows) {
		return b, false, nil
	}
	return b, err == nil, err
}

// StockLocationBalances is one item's stock at one store, location by
// location: every location the store uses, plus any other that still holds
// some (stock is never hidden because a location was switched off).
func StockLocationBalances(tenantID, sku, store string) ([]LocationQty, error) {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return nil, err
	}
	all, err := listStockLocations(db.DB, schema)
	if err != nil {
		return nil, err
	}
	enabled, err := storeStockLocations(schema, store)
	if err != nil {
		return nil, err
	}
	on := map[string]bool{}
	for _, l := range enabled {
		on[l.Code] = true
	}
	b, _, err := readBuckets(db.DB, schema, sku, store, false)
	if err != nil {
		return nil, err
	}
	custom := map[string]int{}
	if rows, err := db.DB.Query(fmt.Sprintf(`SELECT stock_location, qty FROM %s.stock_location_balance WHERE sku = $1 AND location_code = $2`, schema), sku, store); err == nil {
		for rows.Next() {
			var c string
			var n int
			if rows.Scan(&c, &n) == nil {
				custom[c] = n
			}
		}
		rows.Close()
	}
	out := []LocationQty{}
	for _, l := range all {
		qty := b.of(l.Bucket)
		if l.Bucket == "custom" {
			qty = custom[l.Code]
		}
		if !on[l.Code] && qty == 0 {
			continue
		}
		out = append(out, LocationQty{Code: l.Code, Name: l.Name, Qty: qty, Sellable: l.Sellable && l.Active, Enabled: on[l.Code]})
	}
	return out, nil
}

// StockMoveRequest is one Move Stock line.
type StockMoveRequest struct {
	Store  string `json:"store"`
	SKU    string `json:"sku"`
	Qty    int    `json:"qty"`
	From   string `json:"from_location"`
	To     string `json:"to_location"`
	Reason string `json:"reason"`
}

// applyBucketDeltaTx changes ONE location's quantity at a store by qty
// (negative = out, positive = in), touching only what that location is made
// of, so a move is always "from -q" plus "to +q" and every sum stays right:
// Main is available (less the carve-outs); a pool is its own column; a
// client location is its balance, inside available, held back by blocked.
// The caller has the row locked and has checked there is enough.
func applyBucketDeltaTx(tx *sql.Tx, schema, sku, store string, l StockLocation, qty int) error {
	switch {
	case l.Bucket == "available":
		_, err := tx.Exec(fmt.Sprintf(`UPDATE %s.inventory_availability SET available = available + $1, updated_at = CURRENT_TIMESTAMP WHERE sku = $2 AND location_code = $3`, schema), qty, sku, store)
		return err
	case l.Bucket == "custom" && qty < 0:
		// Stage 106.4: taking stock out is an UPDATE - an INSERT ... ON
		// CONFLICT checks qty >= 0 on the proposed (negative) row before it
		// merges, so stock could never leave a client's own location.
		res, err := tx.Exec(fmt.Sprintf(`UPDATE %s.stock_location_balance SET qty = qty + $1, updated_at = CURRENT_TIMESTAMP WHERE sku = $2 AND location_code = $3 AND stock_location = $4`, schema),
			qty, sku, store, l.Code)
		if err != nil {
			return err
		}
		if n, _ := res.RowsAffected(); n == 0 {
			return fmt.Errorf("%s holds none of %s at %s", l.Name, sku, store)
		}
		_, err = tx.Exec(fmt.Sprintf(`UPDATE %s.inventory_availability SET available = available + $1, blocked = blocked + $1, updated_at = CURRENT_TIMESTAMP WHERE sku = $2 AND location_code = $3`, schema), qty, sku, store)
		return err
	case l.Bucket == "custom":
		if _, err := tx.Exec(fmt.Sprintf(`
			INSERT INTO %s.stock_location_balance (sku, location_code, stock_location, qty) VALUES ($1, $2, $3, $4)
			ON CONFLICT (sku, location_code, stock_location) DO UPDATE SET qty = %s.stock_location_balance.qty + EXCLUDED.qty, updated_at = CURRENT_TIMESTAMP`, schema, schema),
			sku, store, l.Code, qty); err != nil {
			return err
		}
		// A client location's stock is on hand and inside available; blocked
		// is what keeps it from being sold, so Main does not change.
		_, err := tx.Exec(fmt.Sprintf(`UPDATE %s.inventory_availability SET available = available + $1, blocked = blocked + $1, updated_at = CURRENT_TIMESTAMP WHERE sku = $2 AND location_code = $3`, schema), qty, sku, store)
		return err
	case stockBuckets[l.Bucket].pool:
		col := pq.QuoteIdentifier(l.Bucket)
		_, err := tx.Exec(fmt.Sprintf(`UPDATE %s.inventory_availability SET %s = %s + $1, updated_at = CURRENT_TIMESTAMP WHERE sku = $2 AND location_code = $3`, schema, col, col), qty, sku, store)
		return err
	}
	return fmt.Errorf("stock cannot be moved through %s", l.Name)
}

// MoveStock moves stock between two locations of one store, in one
// transaction, and records it as a StockMove.
func MoveStock(tenantID string, req StockMoveRequest, userID, username string) (string, error) {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return "", err
	}
	req.Store, req.SKU = strings.TrimSpace(req.Store), strings.TrimSpace(req.SKU)
	req.From, req.To = strings.ToUpper(strings.TrimSpace(req.From)), strings.ToUpper(strings.TrimSpace(req.To))
	if req.Store == "" || req.SKU == "" {
		return "", stockLocationError("STKLOC-0863", "Choose the store and the item.")
	}
	if req.Qty <= 0 {
		return "", stockLocationError("STKLOC-0863", "The quantity must be more than zero.")
	}
	if req.From == "" || req.To == "" || req.From == req.To {
		return "", stockLocationError("STKLOC-0863", "Choose two different locations.")
	}
	all, err := listStockLocations(db.DB, schema)
	if err != nil {
		return "", err
	}
	var from, to *StockLocation
	for i := range all {
		if all[i].Code == req.From {
			from = &all[i]
		}
		if all[i].Code == req.To {
			to = &all[i]
		}
	}
	if from == nil || to == nil || !from.Movable || !to.Movable {
		return "", stockLocationError("STKLOC-0863", "In-Transit and On Hold change only through transfers and hold codes. Pick another location.")
	}
	enabled, err := storeStockLocations(schema, req.Store)
	if err != nil {
		return "", err
	}
	on := map[string]bool{}
	for _, l := range enabled {
		on[l.Code] = true
	}
	if !on[to.Code] {
		return "", stockLocationError("STKLOC-0863", fmt.Sprintf("%s is not used at this store. Add it under Stock Locations -> Stores first.", to.Name))
	}

	number, err := GenerateSequence(tenantID, "SM", req.Store, documentFinancialYear(time.Now()))
	if err != nil {
		return "", err
	}
	tx, err := db.DB.Begin()
	if err != nil {
		return "", err
	}
	defer tx.Rollback()
	b, found, err := readBuckets(tx, schema, req.SKU, req.Store, true)
	if err != nil {
		return "", err
	}
	have := 0
	if found {
		switch from.Bucket {
		case "available":
			// Stock promised to an order is not free to move.
			have = b.of("available") - b.reserved
		case "custom":
			_ = tx.QueryRow(fmt.Sprintf(`SELECT COALESCE(qty, 0) FROM %s.stock_location_balance WHERE sku = $1 AND location_code = $2 AND stock_location = $3 FOR UPDATE`, schema),
				req.SKU, req.Store, from.Code).Scan(&have)
		default:
			have = b.of(from.Bucket)
		}
	}
	if have < req.Qty {
		return "", stockLocationError("STKLOC-0864", fmt.Sprintf("%s has %d of this item free at this store; %d cannot be moved.", from.Name, maxInt(have, 0), req.Qty))
	}
	if err := applyBucketDeltaTx(tx, schema, req.SKU, req.Store, *from, -req.Qty); err != nil {
		return "", err
	}
	if err := applyBucketDeltaTx(tx, schema, req.SKU, req.Store, *to, req.Qty); err != nil {
		return "", err
	}
	var itemName string
	_ = tx.QueryRow(fmt.Sprintf(`SELECT COALESCE(data->>'name', '') FROM %s.documents WHERE doctype = 'Item' AND (id = $1 OR data->>'code' = $1) AND deleted_at IS NULL LIMIT 1`, schema), req.SKU).Scan(&itemName)
	record, _ := json.Marshal(map[string]interface{}{
		"id": number, "location": req.Store, "sku": req.SKU, "item_name": itemName, "qty": req.Qty,
		"from_location": from.Code, "to_location": to.Code, "reason": strings.TrimSpace(req.Reason), "moved_by": username,
	})
	if _, err := tx.Exec(fmt.Sprintf(`INSERT INTO %s.documents (id, doctype, data, status, created_by) VALUES ($1, 'StockMove', $2, 'Posted', $3)`, schema),
		number, record, userID); err != nil {
		return "", err
	}
	if err := tx.Commit(); err != nil {
		return "", err
	}
	LogAuditEvent(tenantID, username, "INVENTORY", "STOCK_MOVED", fmt.Sprintf("%s: %d x %s at %s from %s to %s", number, req.Qty, req.SKU, req.Store, from.Code, to.Code))
	return number, nil
}

// applyReceiptDefaultTx puts received stock where the transaction's default
// "to" location says (Goods Receipt, Transfer In), inside the receipt's own
// transaction. It runs after the receipt has posted to Main, so Main - or a
// location the store does not use - means nothing to do: a receipt is never
// refused over this.
func applyReceiptDefaultTx(tx *sql.Tx, schema, txn, store string, lines []PostedStockLine) error {
	to := stockDefaultTo(schema, txn)
	if to == "" || to == "MAIN" {
		return nil
	}
	enabled, err := storeStockLocations(schema, store)
	if err != nil {
		return nil
	}
	var target *StockLocation
	for i := range enabled {
		if enabled[i].Code == to && enabled[i].Movable {
			target = &enabled[i]
		}
	}
	if target == nil || target.Bucket == "available" {
		return nil
	}
	main := StockLocation{Code: "MAIN", Bucket: "available"}
	for _, l := range lines {
		if l.Qty <= 0 {
			continue
		}
		if err := applyBucketDeltaTx(tx, schema, l.SKU, store, main, -l.Qty); err != nil {
			return err
		}
		if err := applyBucketDeltaTx(tx, schema, l.SKU, store, *target, l.Qty); err != nil {
			return err
		}
	}
	return nil
}

// drawFromSellablePoolsTx covers a sale's shortfall in Main from the pools
// the client has marked sellable (QC Hold, then Damage), so a figure the till
// offered as sellable can actually be sold. Returns how much it covered.
func drawFromSellablePoolsTx(tx *sql.Tx, schema, sku, store string, shortfall int) (int, error) {
	p := atsPolicyFor(schema)
	if shortfall <= 0 || (!p.QCHoldSellable && !p.DamagedSellable) {
		return 0, nil
	}
	b, found, err := readBuckets(tx, schema, sku, store, true)
	if err != nil || !found {
		return 0, err
	}
	covered := 0
	for _, pool := range []struct {
		col  string
		on   bool
		have int
	}{{"qc_hold", p.QCHoldSellable, b.qc}, {"damaged", p.DamagedSellable, b.damaged}} {
		if !pool.on || covered >= shortfall || pool.have <= 0 {
			continue
		}
		take := shortfall - covered
		if take > pool.have {
			take = pool.have
		}
		// The units leave the pool and, for the sale's own decrement that
		// follows, count as Main for the moment it takes.
		if _, err := tx.Exec(fmt.Sprintf(`UPDATE %s.inventory_availability SET %s = %s - $1, available = available + $1 WHERE sku = $2 AND location_code = $3`,
			schema, pq.QuoteIdentifier(pool.col), pq.QuoteIdentifier(pool.col)), take, sku, store); err != nil {
			return covered, err
		}
		covered += take
	}
	return covered, nil
}
