package engines

import (
	"custom_erp/db"
	"encoding/json"
	"fmt"
	"math"
	"sort"
	"time"
)

// Decisions from customer data (140.12.27). CRM numbers were not on the main
// dashboard and the customer reports stopped at five fixed RFM buckets. Three
// reports, read from the till's paid bills of the last year (one pass):
//
//   - crm-pulse: the numbers an owner asks first - active customers, new
//     this month, repeat rate, average bill, lapsed high spenders - and a
//     tile on the main dashboard;
//   - customer-affinity: each customer's favourite category and its share
//     of their spend, and a next best offer - the category's best seller
//     they have not bought yet;
//   - suggested-segments: segments worth building, with how many customers
//     are in each today and the rule to build it with in CRM > Segments.

func init() {
	RegisterReport(ReportDefinition{ID: "crm-pulse", Label: "Customer pulse", Category: "CRM", StoreKey: "-",
		Columns: []ReportColumn{{Key: "metric", Label: "Measure"}, {Key: "value", Label: "Value"}, {Key: "note", Label: "How it is counted"}},
		Run: func(tenantID string, _ map[string]string) ([]map[string]interface{}, error) {
			return crmPulse(tenantID, time.Now())
		}})
	RegisterReport(ReportDefinition{ID: "customer-affinity", Label: "Customer affinity and next best offer", Category: "CRM", StoreKey: "-",
		Columns: []ReportColumn{{Key: "customer_id", Label: "Customer"}, {Key: "name", Label: "Name"}, {Key: "bills", Label: "Bills"},
			{Key: "spend", Label: "Spend", Sensitive: true}, {Key: "top_category", Label: "Favourite category"}, {Key: "share", Label: "Share of spend %"},
			{Key: "last_bill", Label: "Last bill"}, {Key: "next_offer", Label: "Next best offer"}, {Key: "next_offer_name", Label: "Offer item"}},
		Run: func(tenantID string, _ map[string]string) ([]map[string]interface{}, error) {
			return crmAffinity(tenantID, time.Now())
		}})
	RegisterReport(ReportDefinition{ID: "suggested-segments", Label: "Suggested segments", Category: "CRM", StoreKey: "-",
		Columns: []ReportColumn{{Key: "segment", Label: "Segment"}, {Key: "members", Label: "Customers today"}, {Key: "rule", Label: "Build it with"}, {Key: "why", Label: "Why"}},
		Run: func(tenantID string, _ map[string]string) ([]map[string]interface{}, error) {
			return crmSuggestedSegments(tenantID, time.Now())
		}})
}

type crmCustomerYear struct {
	id, name        string
	bills           []time.Time
	amounts         []float64
	spend           float64
	byCategory      map[string]float64
	bought          map[string]bool
	first, lastBill time.Time
}

type crmYear struct {
	customers   map[string]*crmCustomerYear
	categoryOf  map[string]string
	nameOf      map[string]string
	bestSellers map[string][]string // category -> SKUs by quantity sold, best first
}

// crmLoadYear reads a year of paid till bills of known customers.
func crmLoadYear(tenantID string, now time.Time) (*crmYear, error) {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return nil, err
	}
	y := &crmYear{customers: map[string]*crmCustomerYear{}, categoryOf: map[string]string{}, nameOf: map[string]string{}, bestSellers: map[string][]string{}}
	items, err := db.DB.Query(fmt.Sprintf(`SELECT id, COALESCE(data->>'category', ''), COALESCE(data->>'name', data->>'item_name', id) FROM %s.documents WHERE doctype = 'Item' AND deleted_at IS NULL`, schema))
	if err != nil {
		return nil, err
	}
	for items.Next() {
		var sku, cat, name string
		if items.Scan(&sku, &cat, &name) == nil {
			y.categoryOf[sku], y.nameOf[sku] = cat, name
		}
	}
	items.Close()
	rows, err := db.DB.Query(fmt.Sprintf(`SELECT b.data->>'customer_id', COALESCE(c.data->>'name', b.data->>'customer_id'), b.created_at,
		COALESCE((b.data->>'amount_paid')::numeric, 0), COALESCE(b.data->'items', '[]'::jsonb)
		FROM %s.documents b JOIN %s.documents c ON c.doctype = 'Customer' AND c.id = b.data->>'customer_id' AND c.deleted_at IS NULL
		WHERE b.doctype = 'POSCart' AND b.status = 'Paid' AND b.deleted_at IS NULL AND b.created_at >= $1 ORDER BY b.created_at`, schema, schema), now.AddDate(-1, 0, 0))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	sold := map[string]map[string]float64{}
	for rows.Next() {
		var id, name string
		var at time.Time
		var amount float64
		var raw []byte
		if err := rows.Scan(&id, &name, &at, &amount, &raw); err != nil {
			return nil, err
		}
		c := y.customers[id]
		if c == nil {
			c = &crmCustomerYear{id: id, name: name, byCategory: map[string]float64{}, bought: map[string]bool{}, first: at}
			y.customers[id] = c
		}
		c.bills = append(c.bills, at)
		c.amounts = append(c.amounts, amount)
		c.spend += amount
		c.lastBill = at
		var lines []struct {
			SKU       string  `json:"sku"`
			Qty       float64 `json:"qty"`
			SalePrice float64 `json:"sale_price"`
		}
		_ = json.Unmarshal(raw, &lines)
		for _, l := range lines {
			cat := y.categoryOf[l.SKU]
			c.byCategory[cat] += l.Qty * l.SalePrice
			c.bought[l.SKU] = true
			if sold[cat] == nil {
				sold[cat] = map[string]float64{}
			}
			sold[cat][l.SKU] += l.Qty
		}
	}
	for cat, bySKU := range sold {
		skus := make([]string, 0, len(bySKU))
		for s := range bySKU {
			skus = append(skus, s)
		}
		sort.Slice(skus, func(i, j int) bool {
			if bySKU[skus[i]] != bySKU[skus[j]] {
				return bySKU[skus[i]] > bySKU[skus[j]]
			}
			return skus[i] < skus[j]
		})
		y.bestSellers[cat] = skus
	}
	return y, rows.Err()
}

// topSpendCut is the spend at which a customer is in the top fifth.
func (y *crmYear) topSpendCut() float64 {
	spends := make([]float64, 0, len(y.customers))
	for _, c := range y.customers {
		spends = append(spends, c.spend)
	}
	if len(spends) == 0 {
		return math.Inf(1)
	}
	sort.Float64s(spends)
	return spends[int(float64(len(spends))*0.8)]
}

func (c *crmCustomerYear) favourite() (string, float64) {
	best, bestV := "", 0.0
	for cat, v := range c.byCategory {
		if cat != "" && (v > bestV || (v == bestV && cat < best)) {
			best, bestV = cat, v
		}
	}
	if c.spend <= 0 {
		return best, 0
	}
	return best, math.Round(bestV/c.spend*1000) / 10
}

func crmPulse(tenantID string, now time.Time) ([]map[string]interface{}, error) {
	y, err := crmLoadYear(tenantID, now)
	if err != nil {
		return nil, err
	}
	d90 := now.AddDate(0, 0, -90)
	monthStart := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, now.Location())
	cut := y.topSpendCut()
	var active, repeat, newThisMonth, lapsedHigh, bills90 int
	var spend90 float64
	for _, c := range y.customers {
		n := 0
		for _, at := range c.bills {
			if at.After(d90) {
				n++
			}
		}
		if n > 0 {
			active++
			bills90 += n
			if n >= 2 {
				repeat++
			}
		}
		if !c.first.Before(monthStart) {
			newThisMonth++
		}
		if c.lastBill.Before(d90) && c.spend >= cut {
			lapsedHigh++
		}
		for i, at := range c.bills {
			if at.After(d90) {
				spend90 += c.amounts[i]
			}
		}
	}
	pct := 0.0
	if active > 0 {
		pct = math.Round(float64(repeat)/float64(active)*1000) / 10
	}
	avg := 0.0
	if bills90 > 0 {
		avg = math.Round(spend90/float64(bills90)*100) / 100
	}
	return []map[string]interface{}{
		{"metric": "Active customers", "value": active, "note": "bought in the last 90 days"},
		{"metric": "New this month", "value": newThisMonth, "note": "first bill this month"},
		{"metric": "Repeat rate %", "value": pct, "note": "of the active, 2 or more bills in 90 days"},
		{"metric": "Average bill", "value": avg, "note": "known customers, last 90 days"},
		{"metric": "Lapsed high spenders", "value": lapsedHigh, "note": "top fifth by a year's spend, no bill in 90 days"},
	}, nil
}

func crmAffinity(tenantID string, now time.Time) ([]map[string]interface{}, error) {
	y, err := crmLoadYear(tenantID, now)
	if err != nil {
		return nil, err
	}
	list := make([]*crmCustomerYear, 0, len(y.customers))
	for _, c := range y.customers {
		list = append(list, c)
	}
	sort.Slice(list, func(i, j int) bool {
		if list[i].spend != list[j].spend {
			return list[i].spend > list[j].spend
		}
		return list[i].id < list[j].id
	})
	out := []map[string]interface{}{}
	for i, c := range list {
		if i >= 1000 {
			break
		}
		cat, share := c.favourite()
		offer := ""
		for _, sku := range y.bestSellers[cat] {
			if !c.bought[sku] {
				offer = sku
				break
			}
		}
		out = append(out, map[string]interface{}{"customer_id": c.id, "name": c.name, "bills": len(c.bills), "spend": math.Round(c.spend*100) / 100,
			"top_category": cat, "share": share, "last_bill": c.lastBill.Format("2006-01-02"), "next_offer": offer, "next_offer_name": y.nameOf[offer]})
	}
	return out, nil
}

func crmSuggestedSegments(tenantID string, now time.Time) ([]map[string]interface{}, error) {
	y, err := crmLoadYear(tenantID, now)
	if err != nil {
		return nil, err
	}
	d90, d60 := now.AddDate(0, 0, -90), now.AddDate(0, 0, -60)
	cut := y.topSpendCut()
	var lapsedHigh, oneTime, regulars int
	lovers := map[string]int{}
	for _, c := range y.customers {
		if c.lastBill.Before(d90) && c.spend >= cut {
			lapsedHigh++
		}
		if len(c.bills) == 1 && c.lastBill.Before(d60) {
			oneTime++
		}
		n := 0
		for _, at := range c.bills {
			if at.After(d90) {
				n++
			}
		}
		if n >= 4 {
			regulars++
		}
		if cat, share := c.favourite(); cat != "" && share >= 50 {
			lovers[cat]++
		}
	}
	out := []map[string]interface{}{
		{"segment": "Lapsed high spenders", "members": lapsedHigh, "rule": fmt.Sprintf("spend in 365 days >= %.0f and days since last bill > 90", cut), "why": "win back the customers worth most"},
		{"segment": "One-time buyers", "members": oneTime, "rule": "bills = 1 and days since last bill > 60", "why": "a second visit is the hardest"},
		{"segment": "Regulars", "members": regulars, "rule": "bills in 90 days >= 4", "why": "reward and keep them"},
	}
	cats := make([]string, 0, len(lovers))
	for cat := range lovers {
		cats = append(cats, cat)
	}
	sort.Slice(cats, func(i, j int) bool {
		if lovers[cats[i]] != lovers[cats[j]] {
			return lovers[cats[i]] > lovers[cats[j]]
		}
		return cats[i] < cats[j]
	})
	for i, cat := range cats {
		if i >= 5 {
			break
		}
		out = append(out, map[string]interface{}{"segment": cat + " lovers", "members": lovers[cat],
			"rule": fmt.Sprintf("half or more of spend in category %s", cat), "why": "new arrivals and offers in their category"})
	}
	return out, nil
}
