package engines

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"

	"custom_erp/db"

	"github.com/lib/pq"
)

// Ask 215 (215.12, 215.13): one list of what was sold and returned - store
// bills, online orders' bills and returns - searched by any field a cashier
// might have in hand: the bill or cart number, the online order number (ours
// or the channel's), the AWB, the OMS bill number, the customer's name or
// mobile in any format, or a B2B buyer's GSTIN. POS > Sales History lists it,
// and the till's Return tab finds the bill to return through the same query,
// so an online bill can be returned at any store with no OMS step: its
// SalesInvoice id is what the return quotes (salesInvoiceForOrder).

// SalesHistoryFilter narrows the list. Dates are business days, YYYY-MM-DD.
type SalesHistoryFilter struct {
	Stores []string // nil: every store
	From   string
	To     string
	Q      string
	Kind   string // "", "Sale" or "Return"
	Source string // "", "Store" or "Online"
	Limit  int    // 1-200, default 50
	Offset int
	// NoTotals skips the totals: the till's find-a-bill shows none.
	NoTotals bool
	// TotalsOnly skips the rows. The screen asks for its rows and then for
	// the totals, so adding up a big book never holds the list (297.4).
	TotalsOnly bool
}

// SalesHistoryRow is one bill or return.
type SalesHistoryRow struct {
	Kind           string  `json:"kind"`   // Sale | Return
	Source         string  `json:"source"` // Store | Online
	ID             string  `json:"id"`     // cart, invoice or return id
	Number         string  `json:"number"` // bill, invoice or RMA number
	Store          string  `json:"store"`
	Date           string  `json:"date"` // business-day local time
	Customer       string  `json:"customer"`
	Phone          string  `json:"phone"`
	GSTIN          string  `json:"gstin,omitempty"`
	Total          float64 `json:"total"`
	ItemDiscount   float64 `json:"item_discount"`
	OfferDiscount  float64 `json:"offer_discount"`
	BillDiscount   float64 `json:"bill_discount"`
	Status         string  `json:"status"`
	Returned       bool    `json:"returned,omitempty"`   // a sale with a return raised against it
	AgainstID      string  `json:"against_id,omitempty"` // a return's original bill
	Against        string  `json:"against,omitempty"`
	OrderID        string  `json:"order_id,omitempty"`
	OrderNo        string  `json:"order_no,omitempty"`
	ChannelOrderID string  `json:"channel_order_id,omitempty"`
	Channel        string  `json:"channel,omitempty"`
	AWB            string  `json:"awb,omitempty"`
}

// SalesHistory is a page of rows and the totals of everything the filter
// matched, the three discounts apart (215.5).
type SalesHistory struct {
	Rows          []SalesHistoryRow `json:"rows"`
	More          bool              `json:"more"`
	Sales         int               `json:"sales"`
	SalesTotal    float64           `json:"sales_total"`
	Returns       int               `json:"returns"`
	ReturnsTotal  float64           `json:"returns_total"`
	ItemDiscount  float64           `json:"item_discount"`
	OfferDiscount float64           `json:"offer_discount"`
	BillDiscount  float64           `json:"bill_discount"`
	// TotalsSkipped: too many bills matched to add up (297.4); the totals
	// above are zero and the screen asks for fewer days.
	TotalsSkipped bool `json:"totals_skipped,omitempty"`
	// SearchScope "number": store bills were matched by bill number, cart
	// id, GSTIN or full mobile only (297.4, a big book).
	SearchScope string `json:"search_scope,omitempty"`
}

// Ask 297.4 (user, 2026-11-26: "PO, grn, each and every transaction after
// that also erp should not hang"). On the 1-crore scratch run, 30 lakh bills,
// this list took 3.4 s for a week, and with no dates, or with a bill number or
// mobile typed and no date, ran past three minutes. What changed:
//
//  1. Rows and totals are two queries. The totals were window sums over every
//     matched row, so even the first page read all of them.
//  2. Once a book has over BigDoctypeRows bills, totals are added up only when
//     the planner expects at most salesHistoryTotalsCap matches. Otherwise
//     TotalsSkipped is set and the screen asks for fewer days. Totals that
//     run past half the search's time are skipped the same way.
//  3. On such a book, typed text finds a store bill by its bill number, cart
//     id or GSTIN, or by its customer's full mobile, through indexes
//     (migrations_stage297_4_bill_search_indexes.sql, and the customer and
//     store indexes already there). A name or part of a number no longer
//     finds store bills (SearchScope "number"); online bills and returns are
//     searched as before.
//  4. The search stops after salesHistoryDeadline with a message saying what
//     to narrow, and its query stops in the database when the caller leaves.
//
// salesHistoryTotalsCap, salesHistoryDeadline and salesHistoryIsBig are vars
// only so a test can lower, shorten and force them.
var salesHistoryTotalsCap int64 = 200_000

var salesHistoryDeadline = 20 * time.Second

var salesHistoryIsBig = func(schema string) bool {
	return DoctypeRows(schema, "POSCart") > BigDoctypeRows
}

var nonDigits = regexp.MustCompile(`\D`)

// historyLocalTime shows a row's time on the business clock.
func historyLocalTime(alias string) string {
	return fmt.Sprintf(`to_char((%s.created_at AT TIME ZONE current_setting('TimeZone')) AT TIME ZONE public.erp_business_tz(), 'YYYY-MM-DD HH24:MI')`, alias)
}

// SearchSalesHistory reads the list. Online bills and returns are found at
// any store once something is typed - a customer brings an online order to
// whichever store is nearest - and otherwise only at the given stores.
func SearchSalesHistory(parent context.Context, tenantID string, f SalesHistoryFilter) (*SalesHistory, error) {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return nil, err
	}
	for _, d := range []string{f.From, f.To} {
		if d == "" {
			continue
		}
		if _, err := time.Parse("2006-01-02", d); err != nil {
			return nil, &ValidationError{Code: "POSOFF-0883", Message: fmt.Sprintf("%q is not a date (YYYY-MM-DD).", d)}
		}
	}
	if f.Kind != "" && f.Kind != "Sale" && f.Kind != "Return" {
		return nil, &ValidationError{Code: "POSOFF-0883", Message: "Kind must be Sale or Return."}
	}
	if f.Source != "" && f.Source != "Store" && f.Source != "Online" {
		return nil, &ValidationError{Code: "POSOFF-0883", Message: "Source must be Store or Online."}
	}
	if f.Limit <= 0 || f.Limit > 200 {
		f.Limit = 50
	}
	if f.Offset < 0 {
		f.Offset = 0
	}
	q := strings.TrimSpace(f.Q)
	digits := nonDigits.ReplaceAllString(q, "")
	mobile := "" // q is a whole mobile number: its national form
	if p := NormalizePhone(q, TenantPhoneCountry(tenantID)); p.Valid {
		digits, mobile = p.National, p.National
	}
	if len(digits) < 6 {
		digits = "" // too short to be a mobile; the text match still applies
	}
	ctx, cancel := context.WithTimeout(parent, salesHistoryDeadline)
	defer cancel()
	out, err := salesHistoryQuery(ctx, schema, tenantID, f, q, digits, mobile)
	if err != nil && parent.Err() == nil && errors.Is(ctx.Err(), context.DeadlineExceeded) {
		return nil, &ValidationError{Code: "POSOFF-0884", Message: "Pick fewer days or one store, or type the full bill number or mobile."}
	}
	return out, err
}

func salesHistoryQuery(ctx context.Context, schema, tenantID string, f SalesHistoryFilter, q, digits, mobile string) (*SalesHistory, error) {
	var stores interface{} // NULL: every store
	if f.Stores != nil {
		stores = pq.Array(f.Stores)
	}
	// $1 stores, $2 from, $3 to, $4 typed text, $5 its LIKE, $6 mobile digits,
	// $7 kind, $8 source; on a big book with text typed, $9 the customers with
	// that mobile; then, in the rows query only, the limit+1 and the offset.
	args := []interface{}{stores, f.From, f.To, q, "%" + strings.ToLower(q) + "%", digits, f.Kind, f.Source}
	inRange := func(a string) string {
		return fmt.Sprintf(`($2 = '' OR %[1]s.created_at >= public.erp_day_start(NULLIF($2, '')::date)) AND ($3 = '' OR %[1]s.created_at < public.erp_day_start(NULLIF($3, '')::date + 1))`, a)
	}
	paid := fmt.Sprintf(`c.doctype = 'POSCart' AND c.status = 'Paid' AND c.deleted_at IS NULL AND %s AND ($1::text[] IS NULL OR c.data->>'location' = ANY($1))`, inRange("c"))
	// carts is the store bills the list can show. In the rows query, limit
	// keeps only the newest (offset + page) of them when nothing is typed -
	// all the page can use, as every bill then shows. Without it the page read
	// and sorted every bill in the dates. A big book's customer part (below)
	// is bounded the same way. The totals query passes "".
	carts := func(limit string) string {
		if limit != "" && q == "" {
			return fmt.Sprintf(`SELECT c.* FROM %s.documents c WHERE %s ORDER BY c.created_at DESC, c.id %s`, schema, paid, limit)
		}
		return fmt.Sprintf(`SELECT c.* FROM %s.documents c WHERE %s`, schema, paid)
	}
	out := &SalesHistory{Rows: []SalesHistoryRow{}}
	big := salesHistoryIsBig(schema)
	if big && q != "" {
		// (3): the bills the text names, each part through its own index, so
		// the planner never walks every bill. A customer's bills come newest
		// first, only as many as the page needs: one customer can have lakhs.
		out.SearchScope = "number"
		customers := []string{}
		if mobile != "" {
			var err error
			if customers, err = salesHistoryCustomers(ctx, schema, mobile); err != nil {
				return nil, err
			}
		}
		args = append(args, pq.Array(customers))
		named := `(c.id IN ($4::text, upper($4::text)) OR c.data->>'bill_number' IN ($4::text, upper($4::text)) OR c.data->'b2b'->>'gstin' = upper($4::text))`
		namedOrEmpty := `(c.id IN ($4::text, upper($4::text)) OR COALESCE(c.data->>'bill_number', '') IN ($4::text, upper($4::text)) OR COALESCE(c.data->'b2b'->>'gstin', '') = upper($4::text))`
		carts = func(limit string) string {
			return fmt.Sprintf(`SELECT c.* FROM %[1]s.documents c WHERE %[2]s AND %[3]s
		UNION ALL
		SELECT c.* FROM unnest($9::text[]) k(cid) CROSS JOIN LATERAL (
			SELECT c.* FROM %[1]s.documents c WHERE %[2]s AND c.data->>'customer_id' = k.cid AND NOT %[4]s
			ORDER BY c.created_at DESC, c.id %[5]s) c`, schema, paid, named, namedOrEmpty, limit)
		}
	}
	// The online invoices and the returns are bounded the same way when
	// nothing is typed. A return's source is known only after its joins, so
	// with a source picked its part stays whole.
	newest := func(base, a, limit string, ok bool) string {
		if limit != "" && q == "" && ok {
			return fmt.Sprintf(`%s ORDER BY %[2]s.created_at DESC, %[2]s.id %s`, base, a, limit)
		}
		return base
	}
	invoices := func(limit string) string {
		return newest(fmt.Sprintf(`SELECT i.* FROM %s.documents i WHERE i.doctype = 'SalesInvoice' AND i.deleted_at IS NULL AND i.status NOT IN ('Draft', 'Cancelled')
		AND COALESCE(i.data->>'sales_order_id', '') <> '' AND %s
		AND ($1::text[] IS NULL OR $4 <> '' OR i.data->>'location' = ANY($1))`, schema, inRange("i")), "i", limit, true)
	}
	returns := func(limit string) string {
		return newest(fmt.Sprintf(`SELECT r.* FROM %s.documents r WHERE r.doctype = 'ReturnRequest' AND r.deleted_at IS NULL AND %s
		AND ($1::text[] IS NULL OR $4 <> '' OR r.data->>'return_location' = ANY($1) OR r.data->>'original_store' = ANY($1))`, schema, inRange("r")), "r", limit, f.Source == "")
	}
	num := crmNumSQL
	// matched is every row the search finds; limit as for carts.
	matched := func(limit string) string {
		return fmt.Sprintf(`WITH h AS (
	SELECT 'Sale' AS kind, 'Store' AS source, c.id, COALESCE(NULLIF(c.data->>'bill_number', ''), c.id) AS number,
		COALESCE(c.data->>'location', '') AS store, c.created_at AS ts, %[2]s AS shown,
		COALESCE(NULLIF(c.data->'b2b'->>'legal_name', ''), cu.data->>'name', '') AS customer,
		CASE WHEN COALESCE(cu.data->>'walk_in', '') = 'Yes' THEN '' ELSE COALESCE(cu.data->>'phone', '') END AS phone,
		COALESCE(cu.data->>'phone_country', '') AS phone_cc,
		COALESCE(c.data->'b2b'->>'gstin', '') AS gstin,
		COALESCE(%[5]s, 0) AS total, COALESCE(%[6]s, 0) AS item_disc, COALESCE(%[7]s, 0) AS offer_disc, COALESCE(%[8]s, 0) AS bill_disc,
		c.status::text AS status, '' AS against_id, '' AS against,
		'' AS order_id, '' AS order_no, '' AS channel_order_id, '' AS channel, '' AS awb
	FROM (%[11]s) c LEFT JOIN %[1]s.documents cu ON cu.doctype = 'Customer' AND cu.id = c.data->>'customer_id'
	UNION ALL
	SELECT 'Sale', 'Online', i.id, COALESCE(NULLIF(i.data->>'invoice_number', ''), i.id),
		COALESCE(i.data->>'location', ''), i.created_at, %[3]s,
		COALESCE(NULLIF(o.data->>'customer_name', ''), i.data->>'customer', ''), COALESCE(o.data->>'customer_phone', ''),
		COALESCE(o.data->>'customer_country', ''),
		COALESCE(i.data->>'customer_gstin', ''),
		COALESCE(%[9]s, 0), 0, 0, 0,
		i.status::text, '', '',
		COALESCE(i.data->>'sales_order_id', ''), COALESCE(NULLIF(o.data->>'code', ''), o.id, ''), COALESCE(o.data->>'channel_order_id', ''),
		COALESCE(o.data->>'channel', ''), COALESCE(bk.awb, '')
	FROM (%[12]s) i
	LEFT JOIN %[1]s.documents o ON o.doctype = 'SalesOrder' AND o.id = i.data->>'sales_order_id'
	LEFT JOIN LATERAL (SELECT COALESCE(NULLIF(b.data->>'provider_awb', ''), NULLIF(b.data->>'awb_number', ''), b.data->>'tracking_number') AS awb
		FROM %[1]s.documents b WHERE b.doctype = 'LogisticsBooking' AND b.deleted_at IS NULL AND b.data->>'order_id' = i.data->>'sales_order_id'
		ORDER BY (b.data->>'shipping_package_id' = i.data->>'shipping_package_id') DESC NULLS LAST, b.created_at DESC LIMIT 1) bk ON true
	UNION ALL
	SELECT 'Return', CASE WHEN so.id IS NOT NULL THEN 'Online' ELSE 'Store' END, r.id,
		COALESCE(NULLIF(r.data->>'rma_no', ''), NULLIF(r.data->>'code', ''), r.id),
		COALESCE(r.data->>'return_location', ''), r.created_at, %[4]s,
		COALESCE(NULLIF(oc.data->'b2b'->>'legal_name', ''), ocu.data->>'name', NULLIF(so.data->>'customer_name', ''), oc.data->>'customer', ''),
		CASE WHEN COALESCE(ocu.data->>'walk_in', '') = 'Yes' THEN '' ELSE COALESCE(ocu.data->>'phone', so.data->>'customer_phone', '') END,
		CASE WHEN ocu.data->>'phone' IS NOT NULL THEN COALESCE(ocu.data->>'phone_country', '') ELSE COALESCE(so.data->>'customer_country', '') END,
		COALESCE(oc.data->'b2b'->>'gstin', oc.data->>'customer_gstin', ''),
		COALESCE(NULLIF(%[10]s, 0), (SELECT SUM(COALESCE(%[13]s, 0) * COALESCE(%[14]s, 0)) FROM jsonb_array_elements(%[15]s) it), 0), 0, 0, 0,
		r.status::text, COALESCE(r.data->>'original_order_id', ''),
		COALESCE(NULLIF(oc.data->>'bill_number', ''), NULLIF(oc.data->>'invoice_number', ''), r.data->>'original_order_id', ''),
		COALESCE(so.id, ''), COALESCE(NULLIF(so.data->>'code', ''), so.id, ''), COALESCE(so.data->>'channel_order_id', ''), COALESCE(so.data->>'channel', ''), ''
	FROM (%[16]s) r
	LEFT JOIN %[1]s.documents oc ON oc.id = r.data->>'original_order_id' AND oc.doctype IN ('POSCart', 'SalesInvoice', 'SalesOrder')
	LEFT JOIN %[1]s.documents ocu ON ocu.doctype = 'Customer' AND ocu.id = oc.data->>'customer_id'
	LEFT JOIN %[1]s.documents so ON so.doctype = 'SalesOrder' AND so.id = CASE WHEN oc.doctype = 'SalesOrder' THEN oc.id ELSE oc.data->>'sales_order_id' END
), m AS (
	SELECT * FROM h
	WHERE ($7 = '' OR kind = $7) AND ($8 = '' OR source = $8)
		AND ($4 = '' OR lower(concat_ws(' ', id, number, customer, phone, gstin, against_id, against, order_id, order_no, channel_order_id, awb)) LIKE $5
			OR ($6 <> '' AND regexp_replace(phone, '\D', '', 'g') LIKE '%%' || $6))
)`,
			schema, historyLocalTime("c"), historyLocalTime("i"), historyLocalTime("r"),
			num("c.data->>'amount_paid'"), num("c.data->>'item_discount'"), num("c.data->>'offer_discount'"), num("c.data->>'bill_discount'"),
			num("i.data->>'total_amount'"), num("r.data->>'total_refund_eligible'"),
			carts(limit), invoices(limit), num("it->>'original_unit_price'"), num("it->>'qty'"), crmItemsSQL("r"), returns(limit))
	}

	if !f.TotalsOnly {
		if err := salesHistoryPage(ctx, out, tenantID, schema, matched, args, f); err != nil {
			return nil, err
		}
	}
	if f.NoTotals {
		return out, nil
	}

	// (2) The totals of everything matched, the three discounts apart (215.5).
	all := matched("")
	if big {
		n, err := PlannerRows(ctx, all+"\nSELECT 1 FROM m", args...)
		if err != nil {
			return nil, err
		}
		if n > salesHistoryTotalsCap {
			out.TotalsSkipped = true
			return out, nil
		}
	}
	// The totals never cost the page: adding up gets half the search's time,
	// and past it the rows show without totals.
	tctx, cancel := context.WithTimeout(ctx, salesHistoryDeadline/2)
	defer cancel()
	var t SalesHistory
	err := db.DB.QueryRowContext(tctx, all+`
SELECT COUNT(*) FILTER (WHERE kind = 'Sale'), COALESCE(SUM(total) FILTER (WHERE kind = 'Sale'), 0),
	COUNT(*) FILTER (WHERE kind = 'Return'), COALESCE(SUM(total) FILTER (WHERE kind = 'Return'), 0),
	COALESCE(SUM(item_disc), 0), COALESCE(SUM(offer_disc), 0), COALESCE(SUM(bill_disc), 0)
FROM m`, args...).Scan(&t.Sales, &t.SalesTotal, &t.Returns, &t.ReturnsTotal, &t.ItemDiscount, &t.OfferDiscount, &t.BillDiscount)
	if err != nil && errors.Is(tctx.Err(), context.DeadlineExceeded) {
		out.TotalsSkipped = true
		return out, nil
	}
	if err != nil {
		return nil, err
	}
	out.Sales, out.SalesTotal, out.Returns, out.ReturnsTotal = t.Sales, t.SalesTotal, t.Returns, t.ReturnsTotal
	out.ItemDiscount, out.OfferDiscount, out.BillDiscount = t.ItemDiscount, t.OfferDiscount, t.BillDiscount
	return out, nil
}

// salesHistoryPage reads one page into out: newest first, one row past it to
// tell whether there is more. matched(limit) is the search's WITH part.
func salesHistoryPage(ctx context.Context, out *SalesHistory, tenantID, schema string, matched func(string) string, args []interface{}, f SalesHistoryFilter) error {
	lim, off := len(args)+1, len(args)+2
	sqlq := matched(fmt.Sprintf("LIMIT $%d::bigint + $%d::bigint", lim, off)) + fmt.Sprintf(`
SELECT kind, source, id, number, store, shown, customer, phone, phone_cc, gstin, total, item_disc, offer_disc, bill_disc, status, against_id, against,
	order_id, order_no, channel_order_id, channel, awb,
	kind = 'Sale' AND EXISTS (SELECT 1 FROM %s.documents x WHERE x.doctype = 'ReturnRequest' AND x.deleted_at IS NULL AND x.data->>'original_order_id' = m.id)
FROM m ORDER BY ts DESC, id LIMIT $%d OFFSET $%d`, schema, lim, off)
	rows, err := db.DB.QueryContext(ctx, sqlq, append(append([]interface{}{}, args...), f.Limit+1, f.Offset)...)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var x SalesHistoryRow
		var phoneCC string
		if err := rows.Scan(&x.Kind, &x.Source, &x.ID, &x.Number, &x.Store, &x.Date, &x.Customer, &x.Phone, &phoneCC, &x.GSTIN,
			&x.Total, &x.ItemDiscount, &x.OfferDiscount, &x.BillDiscount, &x.Status, &x.AgainstID, &x.Against,
			&x.OrderID, &x.OrderNo, &x.ChannelOrderID, &x.Channel, &x.AWB, &x.Returned); err != nil {
			return err
		}
		// 215.15: a number from abroad shows with + and its code, as on the bill.
		x.Phone = BillPhone(tenantID, x.Phone, phoneCC)
		out.Rows = append(out.Rows, x)
	}
	if err := rows.Err(); err != nil {
		return err
	}
	rows.Close()
	if len(out.Rows) > f.Limit {
		out.Rows, out.More = out.Rows[:f.Limit], true
	}
	return nil
}

// salesHistoryCustomers is the customers with this mobile (the national
// form, as the master stores it), walk-in records apart as in the list's own
// match. At most 50: a mobile is one customer, or a few after a mix-up.
func salesHistoryCustomers(ctx context.Context, schema, mobile string) ([]string, error) {
	rows, err := db.DB.QueryContext(ctx, fmt.Sprintf(`SELECT id FROM %s.documents
		WHERE doctype = 'Customer' AND deleted_at IS NULL AND data->>'phone' = $1 AND COALESCE(data->>'walk_in', '') <> 'Yes'
		ORDER BY id LIMIT 50`, schema), mobile)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	ids := []string{}
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}
