package engines

import (
	"custom_erp/db"
	"fmt"
	"strings"
	"time"

	"github.com/lib/pq"
)

// Stage 102.2.1 - the store manager's Today: what the stores this person runs
// have sold so far today. Read straight from the paid bills (POSCart, Paid) the
// till posts, with the same spend figure the CRM and loyalty use
// (posCartSpendSQL), for the stores the person may work at only (85).
//
// No sales-target record exists in the ERP yet, so the yardstick is the same
// weekday last week up to the same time of day; Target stays empty until a
// target record exists (TargetNote says so). Stage 100's datasets do not exist
// either: StoreAppTodayFor is written as a plain dataset function (stores and
// day in, figures out) so Stage 100 can register it as one without change.
// Days are the database's own calendar days, as every sales report here.

// StoreAppTodayItem is one of the day's best sellers.
type StoreAppTodayItem struct {
	SKU    string  `json:"sku"`
	Name   string  `json:"name"`
	Qty    float64 `json:"qty"`
	Amount float64 `json:"amount"`
}

// StoreAppTodaySalesman is one salesman's day.
type StoreAppTodaySalesman struct {
	Name   string  `json:"name"`
	Bills  int     `json:"bills"`
	Amount float64 `json:"amount"`
}

// StoreAppTodayFigures is one period's totals.
type StoreAppTodayFigures struct {
	Sales        float64 `json:"sales"`
	Bills        int     `json:"bills"`
	Units        float64 `json:"units"`
	AvgBill      float64 `json:"avg_bill"`
	ItemsPerBill float64 `json:"items_per_bill"`
}

// StoreAppToday is the Today screen.
type StoreAppToday struct {
	Stores     []string                `json:"stores"`
	Label      string                  `json:"label"`
	Date       string                  `json:"date"`
	AsOf       string                  `json:"as_of"`
	Currency   string                  `json:"currency"`
	Today      StoreAppTodayFigures    `json:"today"`
	LastWeek   StoreAppTodayFigures    `json:"last_week"`
	ChangePct  *float64                `json:"change_pct,omitempty"`
	Target     *float64                `json:"target,omitempty"`
	TargetNote string                  `json:"target_note,omitempty"`
	TopItems   []StoreAppTodayItem     `json:"top_items"`
	Salesmen   []StoreAppTodaySalesman `json:"salesmen"`
	Returns    int                     `json:"returns"`
	Refunds    float64                 `json:"refunds"`
}

// StoreAppTodayForUser picks the stores: the working store, or every store the
// person may work at when store is "all".
func StoreAppTodayForUser(tenantID, userID, workingStore, store string) (*StoreAppToday, error) {
	access, err := UserStoreAccess(tenantID, userID)
	if err != nil {
		return nil, err
	}
	options, err := StoresFor(tenantID, access)
	if err != nil {
		return nil, err
	}
	var stores []string
	label := ""
	store = strings.TrimSpace(store)
	switch {
	case strings.EqualFold(store, "all"):
		for _, o := range options {
			stores = append(stores, o.Code)
		}
		label = "All your stores"
	default:
		if store == "" {
			store = workingStore
		}
		if !access.Allows(store) {
			return nil, &ValidationError{Code: "GLOBAL-0002", SubFor: "store", Message: fmt.Sprintf("you cannot see %s's sales", store)}
		}
		stores = []string{store}
		for _, o := range options {
			if o.Code == store {
				label = o.Name
			}
		}
		if label == "" {
			label = store
		}
	}
	if len(stores) == 0 {
		return nil, &ValidationError{Code: "GLOBAL-0002", SubFor: "store", Message: "no store is set up for you"}
	}
	out, err := StoreAppTodayFor(tenantID, stores, time.Time{})
	if err != nil {
		return nil, err
	}
	out.Label = label
	return out, nil
}

// StoreAppTodayFor is the dataset: stores and a moment in, the day's figures
// up to that moment (and the same span a week earlier) out. A zero moment is
// "now" by the database's own clock - the clock the bills were stamped with -
// never this server's, which can sit in another time zone.
func StoreAppTodayFor(tenantID string, stores []string, at time.Time) (*StoreAppToday, error) {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return nil, err
	}
	if at.IsZero() {
		if err := db.DB.QueryRow(`SELECT LOCALTIMESTAMP`).Scan(&at); err != nil {
			return nil, err
		}
	}
	dayStart := time.Date(at.Year(), at.Month(), at.Day(), 0, 0, 0, 0, at.Location())
	out := &StoreAppToday{
		Stores: stores, Date: dayStart.Format("2006-01-02"), AsOf: at.Format("15:04"),
		Currency: FunctionalCurrency(tenantID), TopItems: []StoreAppTodayItem{}, Salesmen: []StoreAppTodaySalesman{},
		TargetNote: "No sales target is set up, so today is compared with the same day last week up to this time.",
	}
	stamp := func(t time.Time) string { return t.Format("2006-01-02 15:04:05") }
	figures := func(from, to time.Time) (StoreAppTodayFigures, error) {
		var f StoreAppTodayFigures
		err := db.DB.QueryRow(fmt.Sprintf(`
			SELECT COALESCE(SUM(`+posCartSpendSQL+`), 0), COUNT(*),
			       COALESCE(SUM((SELECT COALESCE(SUM((i->>'qty')::numeric), 0) FROM jsonb_array_elements(COALESCE(data->'items', '[]'::jsonb)) i)), 0)
			FROM %s.documents
			WHERE doctype = 'POSCart' AND status = 'Paid' AND deleted_at IS NULL AND data->>'location' = ANY($1)
			  AND created_at >= $2::timestamp AND created_at < $3::timestamp`, schema), pq.Array(stores), stamp(from), stamp(to)).
			Scan(&f.Sales, &f.Bills, &f.Units)
		if err != nil {
			return f, err
		}
		f.Sales, f.Units = round2(f.Sales), round2(f.Units)
		if f.Bills > 0 {
			f.AvgBill = round2(f.Sales / float64(f.Bills))
			f.ItemsPerBill = round2(f.Units / float64(f.Bills))
		}
		return f, nil
	}
	if out.Today, err = figures(dayStart, at); err != nil {
		return nil, err
	}
	if out.LastWeek, err = figures(dayStart.AddDate(0, 0, -7), at.AddDate(0, 0, -7)); err != nil {
		return nil, err
	}
	if out.LastWeek.Sales > 0 {
		pct := round2((out.Today.Sales - out.LastWeek.Sales) / out.LastWeek.Sales * 100)
		out.ChangePct = &pct
	}

	rows, err := db.DB.Query(fmt.Sprintf(`
		SELECT i->>'sku', MAX(COALESCE(NULLIF(i->>'name', ''), NULLIF(it.data->>'item_name', ''), it.data->>'name', i->>'sku')),
		       SUM(COALESCE((i->>'qty')::numeric, 0)), SUM(COALESCE((i->>'qty')::numeric, 0) * COALESCE((i->>'sale_price')::numeric, 0))
		FROM %[1]s.documents c CROSS JOIN LATERAL jsonb_array_elements(COALESCE(c.data->'items', '[]'::jsonb)) i
		LEFT JOIN %[1]s.documents it ON it.doctype = 'Item' AND it.id = i->>'sku'
		WHERE c.doctype = 'POSCart' AND c.status = 'Paid' AND c.deleted_at IS NULL AND c.data->>'location' = ANY($1)
		  AND c.created_at >= $2::timestamp AND c.created_at < $3::timestamp AND COALESCE(i->>'sku', '') <> ''
		GROUP BY i->>'sku' ORDER BY 4 DESC, 3 DESC LIMIT 5`, schema), pq.Array(stores), stamp(dayStart), stamp(at))
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var it StoreAppTodayItem
		if err := rows.Scan(&it.SKU, &it.Name, &it.Qty, &it.Amount); err != nil {
			rows.Close()
			return nil, err
		}
		it.Qty, it.Amount = round2(it.Qty), round2(it.Amount)
		out.TopItems = append(out.TopItems, it)
	}
	rows.Close()

	rows, err = db.DB.Query(fmt.Sprintf(`
		SELECT COALESCE(NULLIF(s.data->>'name', ''), NULLIF(c.data->>'salesman_id', ''), 'No salesman'), COUNT(*),
		       COALESCE(SUM(`+strings.ReplaceAll(posCartSpendSQL, "data->", "c.data->")+`), 0)
		FROM %[1]s.documents c LEFT JOIN %[1]s.documents s ON s.doctype = 'Salesman' AND s.id = c.data->>'salesman_id'
		WHERE c.doctype = 'POSCart' AND c.status = 'Paid' AND c.deleted_at IS NULL AND c.data->>'location' = ANY($1)
		  AND c.created_at >= $2::timestamp AND c.created_at < $3::timestamp
		GROUP BY 1 ORDER BY 3 DESC LIMIT 10`, schema), pq.Array(stores), stamp(dayStart), stamp(at))
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var s StoreAppTodaySalesman
		if err := rows.Scan(&s.Name, &s.Bills, &s.Amount); err != nil {
			rows.Close()
			return nil, err
		}
		s.Amount = round2(s.Amount)
		out.Salesmen = append(out.Salesmen, s)
	}
	rows.Close()

	if err := db.DB.QueryRow(fmt.Sprintf(`
		SELECT COUNT(*), COALESCE(SUM(COALESCE((r.data->>'total_refund_eligible')::numeric, 0)), 0)
		FROM %[1]s.documents r JOIN %[1]s.documents c ON c.id = r.data->>'original_order_id' AND c.doctype = 'POSCart'
		WHERE r.doctype = 'ReturnRequest' AND r.deleted_at IS NULL AND COALESCE(r.status, '') NOT IN ('Rejected', 'Cancelled')
		  AND c.data->>'location' = ANY($1) AND r.created_at >= $2::timestamp AND r.created_at < $3::timestamp`, schema),
		pq.Array(stores), stamp(dayStart), stamp(at)).Scan(&out.Returns, &out.Refunds); err != nil {
		return nil, err
	}
	out.Refunds = round2(out.Refunds)
	return out, nil
}
