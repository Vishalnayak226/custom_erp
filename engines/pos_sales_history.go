package engines

import (
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
}

var nonDigits = regexp.MustCompile(`\D`)

// historyLocalTime shows a row's time on the business clock.
func historyLocalTime(alias string) string {
	return fmt.Sprintf(`to_char((%s.created_at AT TIME ZONE current_setting('TimeZone')) AT TIME ZONE public.erp_business_tz(), 'YYYY-MM-DD HH24:MI')`, alias)
}

// SearchSalesHistory reads the list. Online bills and returns are found at
// any store once something is typed - a customer brings an online order to
// whichever store is nearest - and otherwise only at the given stores.
func SearchSalesHistory(tenantID string, f SalesHistoryFilter) (*SalesHistory, error) {
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
	if p := NormalizePhone(q, TenantPhoneCountry(tenantID)); p.Valid {
		digits = p.National
	}
	if len(digits) < 6 {
		digits = "" // too short to be a mobile; the text match still applies
	}
	var stores interface{} // NULL: every store
	if f.Stores != nil {
		stores = pq.Array(f.Stores)
	}
	// $1 stores, $2 from, $3 to, $4 typed text, $5 its LIKE, $6 mobile digits,
	// $7 kind, $8 source, $9 limit+1, $10 offset.
	inRange := func(a string) string {
		return fmt.Sprintf(`($2 = '' OR %[1]s.created_at >= public.erp_day_start(NULLIF($2, '')::date)) AND ($3 = '' OR %[1]s.created_at < public.erp_day_start(NULLIF($3, '')::date + 1))`, a)
	}
	num := crmNumSQL
	sqlq := fmt.Sprintf(`WITH h AS (
	SELECT 'Sale' AS kind, 'Store' AS source, c.id, COALESCE(NULLIF(c.data->>'bill_number', ''), c.id) AS number,
		COALESCE(c.data->>'location', '') AS store, c.created_at AS ts, %[2]s AS shown,
		COALESCE(NULLIF(c.data->'b2b'->>'legal_name', ''), cu.data->>'name', '') AS customer,
		CASE WHEN COALESCE(cu.data->>'walk_in', '') = 'Yes' THEN '' ELSE COALESCE(cu.data->>'phone', '') END AS phone,
		COALESCE(cu.data->>'phone_country', '') AS phone_cc,
		COALESCE(c.data->'b2b'->>'gstin', '') AS gstin,
		COALESCE(%[5]s, 0) AS total, COALESCE(%[6]s, 0) AS item_disc, COALESCE(%[7]s, 0) AS offer_disc, COALESCE(%[8]s, 0) AS bill_disc,
		c.status::text AS status, '' AS against_id, '' AS against,
		'' AS order_id, '' AS order_no, '' AS channel_order_id, '' AS channel, '' AS awb
	FROM %[1]s.documents c LEFT JOIN %[1]s.documents cu ON cu.doctype = 'Customer' AND cu.id = c.data->>'customer_id'
	WHERE c.doctype = 'POSCart' AND c.status = 'Paid' AND c.deleted_at IS NULL AND %[11]s
		AND ($1::text[] IS NULL OR c.data->>'location' = ANY($1))
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
	FROM %[1]s.documents i
	LEFT JOIN %[1]s.documents o ON o.doctype = 'SalesOrder' AND o.id = i.data->>'sales_order_id'
	LEFT JOIN LATERAL (SELECT COALESCE(NULLIF(b.data->>'provider_awb', ''), NULLIF(b.data->>'awb_number', ''), b.data->>'tracking_number') AS awb
		FROM %[1]s.documents b WHERE b.doctype = 'LogisticsBooking' AND b.deleted_at IS NULL AND b.data->>'order_id' = i.data->>'sales_order_id'
		ORDER BY (b.data->>'shipping_package_id' = i.data->>'shipping_package_id') DESC NULLS LAST, b.created_at DESC LIMIT 1) bk ON true
	WHERE i.doctype = 'SalesInvoice' AND i.deleted_at IS NULL AND i.status NOT IN ('Draft', 'Cancelled')
		AND COALESCE(i.data->>'sales_order_id', '') <> '' AND %[12]s
		AND ($1::text[] IS NULL OR $4 <> '' OR i.data->>'location' = ANY($1))
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
	FROM %[1]s.documents r
	LEFT JOIN %[1]s.documents oc ON oc.id = r.data->>'original_order_id' AND oc.doctype IN ('POSCart', 'SalesInvoice', 'SalesOrder')
	LEFT JOIN %[1]s.documents ocu ON ocu.doctype = 'Customer' AND ocu.id = oc.data->>'customer_id'
	LEFT JOIN %[1]s.documents so ON so.doctype = 'SalesOrder' AND so.id = CASE WHEN oc.doctype = 'SalesOrder' THEN oc.id ELSE oc.data->>'sales_order_id' END
	WHERE r.doctype = 'ReturnRequest' AND r.deleted_at IS NULL AND %[16]s
		AND ($1::text[] IS NULL OR $4 <> '' OR r.data->>'return_location' = ANY($1) OR r.data->>'original_store' = ANY($1))
), m AS (
	SELECT * FROM h
	WHERE ($7 = '' OR kind = $7) AND ($8 = '' OR source = $8)
		AND ($4 = '' OR lower(concat_ws(' ', id, number, customer, phone, gstin, against_id, against, order_id, order_no, channel_order_id, awb)) LIKE $5
			OR ($6 <> '' AND regexp_replace(phone, '\D', '', 'g') LIKE '%%' || $6))
)
SELECT kind, source, id, number, store, shown, customer, phone, phone_cc, gstin, total, item_disc, offer_disc, bill_disc, status, against_id, against,
	order_id, order_no, channel_order_id, channel, awb,
	kind = 'Sale' AND EXISTS (SELECT 1 FROM %[1]s.documents x WHERE x.doctype = 'ReturnRequest' AND x.deleted_at IS NULL AND x.data->>'original_order_id' = m.id),
	COUNT(*) FILTER (WHERE kind = 'Sale') OVER (), COALESCE(SUM(total) FILTER (WHERE kind = 'Sale') OVER (), 0),
	COUNT(*) FILTER (WHERE kind = 'Return') OVER (), COALESCE(SUM(total) FILTER (WHERE kind = 'Return') OVER (), 0),
	COALESCE(SUM(item_disc) OVER (), 0), COALESCE(SUM(offer_disc) OVER (), 0), COALESCE(SUM(bill_disc) OVER (), 0)
FROM m ORDER BY ts DESC, id LIMIT $9 OFFSET $10`,
		schema, historyLocalTime("c"), historyLocalTime("i"), historyLocalTime("r"),
		num("c.data->>'amount_paid'"), num("c.data->>'item_discount'"), num("c.data->>'offer_discount'"), num("c.data->>'bill_discount'"),
		num("i.data->>'total_amount'"), num("r.data->>'total_refund_eligible'"),
		inRange("c"), inRange("i"), num("it->>'original_unit_price'"), num("it->>'qty'"), crmItemsSQL("r"), inRange("r"))
	rows, err := db.DB.Query(sqlq, stores, f.From, f.To, q, "%"+strings.ToLower(q)+"%", digits, f.Kind, f.Source, f.Limit+1, f.Offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := &SalesHistory{Rows: []SalesHistoryRow{}}
	for rows.Next() {
		var x SalesHistoryRow
		var phoneCC string
		if err := rows.Scan(&x.Kind, &x.Source, &x.ID, &x.Number, &x.Store, &x.Date, &x.Customer, &x.Phone, &phoneCC, &x.GSTIN,
			&x.Total, &x.ItemDiscount, &x.OfferDiscount, &x.BillDiscount, &x.Status, &x.AgainstID, &x.Against,
			&x.OrderID, &x.OrderNo, &x.ChannelOrderID, &x.Channel, &x.AWB, &x.Returned,
			&out.Sales, &out.SalesTotal, &out.Returns, &out.ReturnsTotal, &out.ItemDiscount, &out.OfferDiscount, &out.BillDiscount); err != nil {
			return nil, err
		}
		// 215.15: a number from abroad shows with + and its code, as on the bill.
		x.Phone = BillPhone(tenantID, x.Phone, phoneCC)
		out.Rows = append(out.Rows, x)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if len(out.Rows) > f.Limit {
		out.Rows, out.More = out.Rows[:f.Limit], true
	}
	return out, nil
}
