package server

import (
	"bytes"
	"encoding/csv"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"custom_erp/db"
	"custom_erp/engines"
)

// Stage 142.3: Sales from one Excel file - Financial Accounting → Sales
// Invoice → Upload from Excel. A client fills one workbook (Customers and
// Sales sheets; the template is built live with the client's stores and the
// states in drop-downs) and one upload makes the customers it needs and one
// Sales Invoice per Invoice Ref + customer, each line found by its barcode
// with its own discount (₹ per piece, or a %), posted as the uploader where
// their role may post. Preview says what each row will do and changes
// nothing; Apply runs in the background and is kept as an ImportJob run
// (engines.BulkSalesDoctype) with its file.
//
// Every write replays the API call its own screen makes (swCaller, Stage
// 128): the customer save, the invoice save (numbering, GST, permissions and
// audit are the screens' own) and the finance post. Re-uploading the same
// file is safe: an invoice is found again by customer + Invoice Ref (stamped
// on it as upload_ref). The Bulk Purchase Upload (bulk_purchase.go, Stage
// 129) is the model; its date and whole-number readers are reused.

const (
	bsColRef      = "invoice ref"
	bsColCustomer = "customer"
	bsColStore    = "store"
	bsColDate     = "invoice date"
	bsColBarcode  = "barcode"
	bsColQty      = "qty"
	bsColRate     = "rate"
	bsColDisc     = "discount"
)

func bsHeaderWords() map[string][]string {
	return map[string][]string{
		"customers": {"Customer Name", "Phone"},
		"sales":     {"Invoice Ref", "Barcode"},
	}
}

// ---------------------------------------------------------------- input

type bsCustomerIn struct {
	Row                             int
	Name, Phone, GSTIN, State, City string
}

type bsLine struct {
	Row                                     int
	Ref, Customer, Store, DateText, Barcode string
	Qty                                     int
	Rate                                    float64 // per piece, GST included; 0 = the item's own
	DiscText                                string
	DiscPct, DiscEach                       float64
	Date                                    string // ISO
	err                                     string

	customerID, customerCode, storeCode string
	sku, name, hsn                      string
	gstRate, mrp                        float64
	discount                            float64 // the line's ₹ off
	inv                                 *bsInvoice
	out                                 *bsLineOut
}

// ---------------------------------------------------------------- output

type bsRowOut struct {
	Row     int    `json:"row,omitempty"`
	Key     string `json:"key"`
	Action  string `json:"action"` // new | existing | error | note
	Message string `json:"message,omitempty"`
	Result  string `json:"result,omitempty"` // done | failed
}

type bsLineOut struct {
	Row      int     `json:"row"`
	Barcode  string  `json:"barcode"`
	SKU      string  `json:"sku,omitempty"`
	Name     string  `json:"name,omitempty"`
	Qty      int     `json:"qty"`
	Rate     float64 `json:"rate"`
	Discount float64 `json:"discount"`
	Amount   float64 `json:"amount"`
	Action   string  `json:"action"` // ok | error
	Message  string  `json:"message,omitempty"`
}

type bsInvoiceOut struct {
	Ref           string       `json:"ref"`
	Customer      string       `json:"customer"`
	Store         string       `json:"store"`
	Date          string       `json:"date"`
	Action        string       `json:"action"` // new | existing | error
	InvoiceID     string       `json:"invoice_id,omitempty"`
	InvoiceNumber string       `json:"invoice_number,omitempty"`
	Status        string       `json:"status,omitempty"`
	Qty           int          `json:"qty"`
	Discount      float64      `json:"discount"`
	Taxable       float64      `json:"taxable"`
	Tax           float64      `json:"tax"`
	Amount        float64      `json:"amount"`
	Lines         []*bsLineOut `json:"lines"`
	Message       string       `json:"message,omitempty"`
	Result        string       `json:"result,omitempty"` // done | failed
}

type bsTotals struct {
	NewCustomers     int     `json:"new_customers"`
	NewInvoices      int     `json:"new_invoices"`
	ExistingInvoices int     `json:"existing_invoices"`
	Lines            int     `json:"lines"`
	Qty              int     `json:"qty"`
	Discount         float64 `json:"discount"`
	Amount           float64 `json:"amount"`
	Errors           int     `json:"errors"`
	Done             int     `json:"done"`
	Failed           int     `json:"failed"`
	Posted           int     `json:"posted"`
	Drafts           int     `json:"drafts"`
}

type bsResult struct {
	Customers []*bsRowOut     `json:"customers"`
	Invoices  []*bsInvoiceOut `json:"invoices"`
	Totals    bsTotals        `json:"totals"`
	CanPost   bool            `json:"can_post"`
}

// ---------------------------------------------------------------- read

// bsRead reads the two sheets. A missing Customers sheet is empty; the
// Sales sheet is required. dateOrder is the client's date order (bpDate).
func bsRead(file []byte, dateOrder string) ([]*bsCustomerIn, []*bsLine, error) {
	tables, _, err := swParse(file, bsHeaderWords())
	if err != nil {
		return nil, nil, err
	}
	st := tables["sales"]
	if st == nil || !st.has("Invoice Ref") {
		return nil, nil, errors.New("this file has no Sales sheet with an Invoice Ref column - download the Sales template and fill that in")
	}
	var customers []*bsCustomerIn
	if t := tables["customers"]; t != nil {
		for _, r := range t.rows {
			c := &bsCustomerIn{Row: r.Num, Name: t.get(r, "Customer Name", "Customer"), Phone: t.get(r, "Phone"),
				GSTIN: strings.ToUpper(t.get(r, "GSTIN")), State: t.get(r, "State"), City: t.get(r, "City")}
			if c.Name == "" && c.Phone == "" && c.GSTIN == "" && c.State == "" && c.City == "" {
				continue
			}
			customers = append(customers, c)
		}
	}
	var lines []*bsLine
	for _, r := range st.rows {
		l := &bsLine{Row: r.Num, Ref: st.get(r, bsColRef), Customer: st.get(r, bsColCustomer), Store: st.get(r, bsColStore),
			DateText: st.get(r, bsColDate), Barcode: st.get(r, bsColBarcode), DiscText: st.get(r, bsColDisc)}
		qtyText, rateText := st.get(r, bsColQty), st.get(r, bsColRate)
		if l.Ref == "" && l.Customer == "" && l.Store == "" && l.DateText == "" && l.Barcode == "" && qtyText == "" && rateText == "" && l.DiscText == "" {
			continue
		}
		var miss, probs []string
		for _, f := range [][2]string{{"Invoice Ref", l.Ref}, {"Customer", l.Customer}, {"Store", l.Store}, {"Invoice Date", l.DateText}, {"Barcode", l.Barcode}, {"Qty", qtyText}} {
			if f[1] == "" {
				miss = append(miss, f[0])
			}
		}
		if len(miss) > 0 {
			probs = append(probs, "missing: "+strings.Join(miss, ", "))
		}
		if qtyText != "" {
			if q, err := bpWhole(qtyText, "Qty"); err != nil || q <= 0 {
				probs = append(probs, fmt.Sprintf("Qty must be a whole number above 0 (%q)", qtyText))
			} else {
				l.Qty = q
			}
		}
		if rateText != "" {
			if f, err := swNumber(rateText); err != nil || f <= 0 {
				probs = append(probs, fmt.Sprintf("Rate must be an amount above 0 (%q) - or leave it blank for the item's own price", rateText))
			} else {
				l.Rate = f
			}
		}
		if l.DateText != "" {
			if d, err := bpDate(l.DateText, dateOrder); err != nil {
				probs = append(probs, err.Error())
			} else {
				l.Date = d
			}
		}
		if s := strings.TrimSpace(l.DiscText); s != "" {
			if strings.HasSuffix(s, "%") {
				if f, err := swNumber(s); err != nil || f < 0 || f > 100 {
					probs = append(probs, fmt.Sprintf("Discount %q is not a percentage between 0%% and 100%%", s))
				} else {
					l.DiscPct = f
				}
			} else if f, err := swNumber(s); err != nil || f < 0 {
				probs = append(probs, fmt.Sprintf("Discount must be an amount per piece (12.50) or a percentage (10%%), not %q", s))
			} else {
				l.DiscEach = f
			}
		}
		l.err = strings.Join(probs, "; ")
		lines = append(lines, l)
	}
	return customers, lines, nil
}

// ---------------------------------------------------------------- plan + apply

type bsInvoice struct {
	ref, customer                       string
	customerID, customerCode, custState string
	customerGSTIN                       string
	store, storeState                   string
	date                                string
	lines                               []*bsLine
	out                                 *bsInvoiceOut
	pendingCustomer                     bool // made by this upload (preview)
}

type bsRun struct {
	sw       *swRun
	apply    bool
	res      *bsResult
	canPost  bool
	progress func(step string, done, total int)

	customers  map[string]string        // normalized name -> id ("" while only planned)
	custRows   map[string]*bsCustomerIn // normalized name -> its Customers row
	custErr    map[string]string
	perms      map[string]bool
	storeState map[string]string
}

func newBSRun(c *swCaller, apply bool) (*bsRun, error) {
	sw, err := newSWRun(c, false, apply)
	if err != nil {
		return nil, err
	}
	canPost, _, _ := checkRouteCapability("POST /api/v1/finance/sales-invoice/{id}/post", c.role)
	return &bsRun{sw: sw, apply: apply, canPost: canPost, res: &bsResult{Customers: []*bsRowOut{}, Invoices: []*bsInvoiceOut{}, CanPost: canPost},
		customers: map[string]string{}, custRows: map[string]*bsCustomerIn{}, custErr: map[string]string{}, perms: map[string]bool{}, storeState: map[string]string{}}, nil
}

func (b *bsRun) write(pattern, method, target string, pv map[string]string, body interface{}, h http.HandlerFunc) (map[string]interface{}, error) {
	status, out := b.sw.c.call(pattern, method, target, pv, body, h)
	if status < 200 || status >= 300 {
		return nil, errors.New(swMessage(status, out))
	}
	m := map[string]interface{}{}
	_ = json.Unmarshal(out, &m)
	return m, nil
}

func (b *bsRun) saveDoc(doctype string, body map[string]interface{}) (string, error) {
	m, err := b.write("/api/v1/doc/{doctype}", http.MethodPost, "/api/v1/doc/"+url.PathEscape(doctype), map[string]string{"doctype": doctype}, body, handleGenericDoc)
	if err != nil {
		return "", err
	}
	id := swText(m["id"])
	if id == "" {
		id = swText(m["code"])
	}
	if id == "" {
		return "", fmt.Errorf("the %s was saved but its id did not come back", doctype)
	}
	return id, nil
}

func (b *bsRun) mayCreate(doctype string) error {
	k := doctype + "|create"
	ok, seen := b.perms[k]
	if !seen {
		ok, _ = checkPermission(b.sw.c.tenantID, b.sw.c.role, doctype, "create")
		b.perms[k] = ok
	}
	if !ok {
		return fmt.Errorf("your role may not create %s records - ask an admin, or have them made first", swDoctypeLabel(doctype))
	}
	return nil
}

func (b *bsRun) oneMatch(doctype, name string) (string, error) {
	idx := b.sw.index(doctype)
	hits := idx.byText[swNorm(name)]
	if len(hits) > 1 {
		return "", fmt.Errorf("%d %s records are called %q", len(hits), swDoctypeLabel(doctype), name)
	}
	if len(hits) == 1 {
		return idx.recs[hits[0]].ID, nil
	}
	return "", nil
}

func (b *bsRun) step(name string, done, total int) {
	if b.progress != nil {
		b.progress(name, done, total)
	}
}

// bsDigits keeps a phone's digits, so "+91 98765-43210" matches "9876543210".
func bsDigits(s string) string {
	var sb strings.Builder
	for _, r := range s {
		if r >= '0' && r <= '9' {
			sb.WriteRune(r)
		}
	}
	d := sb.String()
	if len(d) > 10 {
		d = d[len(d)-10:]
	}
	return d
}

// bsDocField reads one field of a saved record (the index is read once per
// run, so a record this run just made is not in it).
func bsDocField(schema, doctype, id, field string) string {
	var v string
	_ = db.DB.QueryRow(fmt.Sprintf(`SELECT COALESCE(data->>$3, '') FROM %s.documents WHERE doctype = $1 AND id = $2 AND deleted_at IS NULL`, schema), doctype, id, field).Scan(&v)
	return v
}

// process plans (and, when applying, does) the whole upload.
func (b *bsRun) process(customers []*bsCustomerIn, lines []*bsLine) *bsResult {
	b.step("Customers", 0, 2)
	b.doCustomers(customers, lines)
	invs := b.groupInvoices(lines)
	b.step("Sales", 1, 2)
	b.doInvoices(invs)
	// A store with no State bills within its own state, as the New Sales
	// Invoice screen does - say so, since the tax split then rests on it.
	for _, inv := range invs {
		if inv.out.Action == "new" && inv.storeState == "" {
			note := fmt.Sprintf("store %s has no State (on it or its legal entity) - billed CGST + SGST; give it a State for IGST across states", inv.store)
			inv.out.Message = strings.TrimPrefix(inv.out.Message+"; "+note, "; ")
		}
	}
	b.totals(invs)
	return b.res
}

// doCustomers: the Customers sheet. Only the customers the Sales rows use
// are made; a customer already in Wholeops (GSTIN, phone or name) is found.
func (b *bsRun) doCustomers(rows []*bsCustomerIn, lines []*bsLine) {
	used := map[string]bool{}
	for _, l := range lines {
		used[swNorm(l.Customer)] = true
	}
	seen := map[string]int{}
	for _, c := range rows {
		out := &bsRowOut{Row: c.Row, Key: c.Name}
		b.res.Customers = append(b.res.Customers, out)
		k := swNorm(c.Name)
		if c.Name != "" && !used[k] {
			out.Action, out.Message = "note", "no Sales row uses this customer - left out"
			continue
		}
		fail := func(msg string) { out.Action, out.Message = "error", msg }
		if c.Name == "" {
			fail("missing: Customer Name")
			continue
		}
		stateCode := engines.StateCodeFromName(c.State)
		switch {
		case c.State != "" && stateCode == "":
			fail(fmt.Sprintf("%q is not a state of India - pick it from the list", c.State))
			continue
		case c.GSTIN != "" && len(c.GSTIN) != 15:
			fail("GSTIN must be 15 characters - or leave it blank")
			continue
		case c.GSTIN != "" && stateCode != "" && engines.StateCodeFromGSTIN(c.GSTIN) != stateCode:
			fail(fmt.Sprintf("GSTIN starts with %s but %s is %s", c.GSTIN[:2], c.State, stateCode))
			continue
		}
		if first, dup := seen[k]; dup {
			fail(fmt.Sprintf("listed twice (row %d)", first))
			continue
		}
		seen[k] = c.Row
		b.custRows[k] = c
		id, err := b.findCustomer(c.Name, c.Phone, c.GSTIN)
		if err != nil {
			fail(err.Error() + " - use the customer's code")
			continue
		}
		if id != "" {
			out.Action = "existing"
			b.customers[k] = id
			continue
		}
		out.Action = "new"
		b.customers[k] = ""
		if !b.apply {
			if err := b.mayCreate("Customer"); err != nil {
				out.Action, out.Message = "error", err.Error()
				delete(b.customers, k)
				b.custErr[k] = fmt.Sprintf("customer %s: %v", c.Name, err)
			}
			continue
		}
		body := map[string]interface{}{"name": c.Name, "status": "Active"}
		for f, val := range map[string]string{"phone": c.Phone, "gstin": c.GSTIN, "state": c.State, "state_code": stateCode, "city": c.City} {
			if val != "" {
				body[f] = val
			}
		}
		if c.GSTIN != "" && stateCode == "" {
			body["state_code"] = engines.StateCodeFromGSTIN(c.GSTIN)
		}
		id, err = b.saveDoc("Customer", body)
		if err != nil {
			out.Result, out.Message = "failed", err.Error()
			delete(b.customers, k)
			b.custErr[k] = fmt.Sprintf("customer %s could not be made: %v", c.Name, err)
			continue
		}
		out.Result = "done"
		b.customers[k] = id
	}
}

// findCustomer: by GSTIN, then the phone's digits, then the name.
func (b *bsRun) findCustomer(name, phone, gstin string) (string, error) {
	if gstin != "" {
		if id, err := b.oneMatch("Customer", gstin); err != nil || id != "" {
			return id, err
		}
	}
	if d := bsDigits(phone); len(d) >= 10 {
		idx := b.sw.index("Customer")
		var hits []string
		for _, rec := range idx.recs {
			if bsDigits(swText(rec.Data["phone"])) == d {
				hits = append(hits, rec.ID)
			}
		}
		if len(hits) == 1 {
			return hits[0], nil
		}
		if len(hits) > 1 {
			return "", fmt.Errorf("%d customers have the phone %s", len(hits), phone)
		}
	}
	return b.oneMatch("Customer", name)
}

// customerFor resolves a Sales row's customer: the Customers sheet first,
// then the customers already in Wholeops. pending: made by this upload.
func (b *bsRun) customerFor(name string) (id string, pending bool, err error) {
	k := swNorm(name)
	if id, ok := b.customers[k]; ok {
		return id, id == "", nil
	}
	if msg := b.custErr[k]; msg != "" {
		return "", false, errors.New(msg)
	}
	id, err = b.oneMatch("Customer", name)
	if err != nil {
		return "", false, err
	}
	if id == "" {
		return "", false, fmt.Errorf("customer %q is not on the Customers sheet or in Wholeops - add it on the Customers sheet", name)
	}
	return id, false, nil
}

// customerState is the GST state the customer is billed in: GSTIN first,
// then the record's state code, then its state name.
func (b *bsRun) customerState(id, name string) (gstin, state string) {
	var data map[string]interface{}
	if id != "" {
		idx := b.sw.index("Customer")
		if n, ok := idx.byID[strings.ToLower(id)]; ok {
			data = idx.recs[n].Data
		}
	}
	if data == nil {
		if row := b.custRows[swNorm(name)]; row != nil {
			data = map[string]interface{}{"gstin": row.GSTIN, "state": row.State}
		} else if id != "" {
			data = map[string]interface{}{"gstin": bsDocField(b.sw.schema, "Customer", id, "gstin"),
				"state_code": bsDocField(b.sw.schema, "Customer", id, "state_code"), "state": bsDocField(b.sw.schema, "Customer", id, "state")}
		}
	}
	gstin = strings.ToUpper(swText(data["gstin"]))
	if code := engines.StateCodeFromGSTIN(gstin); code != "" {
		return gstin, code
	}
	if code := swText(data["state_code"]); len(code) == 2 {
		return gstin, code
	}
	return gstin, engines.StateCodeFromName(swText(data["state"]))
}

func (b *bsRun) storeCode(name string) (string, error) {
	idx := b.sw.index("Location")
	if n, ok := idx.byID[strings.ToLower(name)]; ok {
		return firstNonEmptyStr(swText(idx.recs[n].Data["code"]), idx.recs[n].ID), nil
	}
	hits := idx.byText[swNorm(name)]
	if len(hits) > 1 {
		return "", fmt.Errorf("%d stores are called %q - use the store's code", len(hits), name)
	}
	if len(hits) == 0 {
		return "", fmt.Errorf("store %q is not set up - pick it from the list", name)
	}
	return firstNonEmptyStr(swText(idx.recs[hits[0]].Data["code"]), idx.recs[hits[0]].ID), nil
}

func (b *bsRun) stateOfStore(code string) string {
	if s, ok := b.storeState[code]; ok {
		return s
	}
	s, _ := engines.LocationStateCode(b.sw.c.tenantID, code)
	b.storeState[code] = s
	return s
}

func bsRound(f float64) float64 { return math.Round(f*100) / 100 }

// groupInvoices turns Sales rows into invoices: the same Invoice Ref and
// customer is one invoice. Each row's customer, store and item are resolved
// here; a row with a problem marks its whole invoice as one (an invoice with
// a line missing would be the wrong bill).
func (b *bsRun) groupInvoices(lines []*bsLine) []*bsInvoice {
	byKey := map[string]*bsInvoice{}
	var invs []*bsInvoice
	for _, l := range lines {
		l.out = &bsLineOut{Row: l.Row, Barcode: l.Barcode, Qty: l.Qty, Rate: l.Rate, Action: "ok"}
		key := swNorm(l.Customer) + "|" + swNorm(l.Ref)
		inv := byKey[key]
		if inv == nil {
			inv = &bsInvoice{ref: l.Ref, customer: l.Customer, store: l.Store, date: l.Date}
			if l.Ref == "" || l.Customer == "" {
				inv.ref = fmt.Sprintf("row %d", l.Row)
			}
			inv.out = &bsInvoiceOut{Ref: inv.ref, Customer: l.Customer, Store: l.Store, Date: l.Date, Action: "new", Lines: []*bsLineOut{}}
			byKey[key] = inv
			invs = append(invs, inv)
			b.res.Invoices = append(b.res.Invoices, inv.out)
		}
		inv.lines = append(inv.lines, l)
		inv.out.Lines = append(inv.out.Lines, l.out)
		l.inv = inv
		probs := []string{}
		if l.err != "" {
			probs = append(probs, l.err)
		}
		if l.err == "" {
			if !strings.EqualFold(l.Store, inv.store) {
				probs = append(probs, fmt.Sprintf("Store %q differs from row %d of the same Invoice Ref (%q)", l.Store, inv.lines[0].Row, inv.store))
			}
			if l.Date != inv.date {
				probs = append(probs, fmt.Sprintf("Invoice Date differs from row %d of the same Invoice Ref", inv.lines[0].Row))
			}
			if err := b.resolveLine(l); err != nil {
				probs = append(probs, err.Error())
			}
		}
		if len(probs) > 0 {
			l.out.Action, l.out.Message = "error", strings.Join(probs, "; ")
			inv.out.Action = "error"
			continue
		}
		l.out.SKU, l.out.Name, l.out.Rate, l.out.Discount = l.sku, l.name, l.Rate, l.discount
		l.out.Amount = bsRound(float64(l.Qty)*l.Rate - l.discount)
	}
	for _, inv := range invs {
		if inv.out.Action == "error" {
			n := 0
			for _, l := range inv.lines {
				if l.out.Action == "error" {
					n++
				}
			}
			inv.out.Message = fmt.Sprintf("%d of %d rows have a problem - fix them and upload again", n, len(inv.lines))
			continue
		}
		first := inv.lines[0]
		inv.customerID, inv.customerCode, inv.store = first.customerID, first.customerCode, first.storeCode
		inv.pendingCustomer = inv.customerID == ""
		inv.customerGSTIN, inv.custState = b.customerState(inv.customerID, inv.customer)
		inv.storeState = b.stateOfStore(inv.store)
		inv.out.Store = inv.store
		for _, l := range inv.lines {
			inv.out.Qty += l.Qty
			inv.out.Discount = bsRound(inv.out.Discount + l.discount)
			inv.out.Amount = bsRound(inv.out.Amount + l.out.Amount)
		}
	}
	return invs
}

// resolveLine finds the row's customer, store and item and prices it.
func (b *bsRun) resolveLine(l *bsLine) error {
	id, _, err := b.customerFor(l.Customer)
	if err != nil {
		return err
	}
	l.customerID = id
	if id != "" {
		idx := b.sw.index("Customer")
		if n, ok := idx.byID[strings.ToLower(id)]; ok {
			l.customerCode = firstNonEmptyStr(swText(idx.recs[n].Data["code"]), id)
		} else {
			l.customerCode = firstNonEmptyStr(bsDocField(b.sw.schema, "Customer", id, "code"), id)
		}
	}
	if l.storeCode, err = b.storeCode(l.Store); err != nil {
		return err
	}
	item, err := engines.ResolveItemBySKU(b.sw.c.tenantID, l.Barcode)
	if err != nil {
		return fmt.Errorf("no item has the barcode or code %q", l.Barcode)
	}
	if item.Status != "" && item.Status != "Active" {
		return fmt.Errorf("item %s (%s) is %s, not Active", swText(item.Data["code"]), l.Barcode, item.Status)
	}
	l.sku = firstNonEmptyStr(swText(item.Data["code"]), item.ID)
	l.name = swText(item.Data["name"])
	l.hsn = swText(item.Data["hsn_code"])
	l.gstRate, _ = strconv.ParseFloat(swText(item.Data["gst_rate"]), 64)
	l.mrp, _ = strconv.ParseFloat(swText(item.Data["mrp"]), 64)
	if l.Rate == 0 {
		sale, _ := strconv.ParseFloat(swText(item.Data["sale_price"]), 64)
		l.Rate = firstPositiveFloat(sale, l.mrp)
		if l.Rate == 0 {
			return fmt.Errorf("item %s has no sale price or MRP - type the Rate", l.sku)
		}
	}
	gross := float64(l.Qty) * l.Rate
	if l.DiscPct > 0 {
		l.discount = bsRound(gross * l.DiscPct / 100)
	} else {
		l.discount = bsRound(float64(l.Qty) * l.DiscEach)
	}
	if l.discount > gross {
		return fmt.Errorf("the discount (%.2f) is more than the line (%.2f)", l.discount, gross)
	}
	return nil
}

func firstPositiveFloat(vals ...float64) float64 {
	for _, v := range vals {
		if v > 0 {
			return v
		}
	}
	return 0
}

// bsFindInvoice: the invoice an earlier upload of this file made.
func bsFindInvoice(schema, customerCode, ref string) (id, number, status string) {
	_ = db.DB.QueryRow(fmt.Sprintf(`SELECT id, COALESCE(data->>'invoice_number', ''), COALESCE(status, data->>'status', '')
		FROM %s.documents WHERE doctype = 'SalesInvoice' AND deleted_at IS NULL
		  AND data->>'upload_ref' = $1 AND data->>'customer' = $2 AND COALESCE(status, data->>'status', '') <> 'Cancelled'
		ORDER BY created_at LIMIT 1`, schema), ref, customerCode).Scan(&id, &number, &status)
	return id, number, status
}

// doInvoices saves and posts each invoice (or says what it would do).
func (b *bsRun) doInvoices(invs []*bsInvoice) {
	for i, inv := range invs {
		b.step("Sales", i, len(invs))
		out := inv.out
		if out.Action == "error" {
			continue
		}
		if inv.customerCode != "" {
			if id, number, status := bsFindInvoice(b.sw.schema, inv.customerCode, inv.ref); id != "" {
				out.Action, out.InvoiceID, out.InvoiceNumber, out.Status = "existing", id, number, status
				switch {
				case status != "Draft":
					out.Message = "made and posted by an earlier upload"
					out.Result = "done"
				case !b.canPost:
					out.Message = "made by an earlier upload; still a Draft - someone who may post invoices posts it from the list"
					out.Result = "done"
				case !b.apply:
					out.Message = "made by an earlier upload; still a Draft - it will be posted"
				default:
					b.post(inv)
				}
				continue
			}
		}
		if !b.apply {
			if err := b.mayCreate("SalesInvoice"); err != nil {
				out.Action, out.Message = "error", err.Error()
				continue
			}
			switch {
			case inv.pendingCustomer && b.canPost:
				out.Message = "its customer is made first; saved and posted"
			case inv.pendingCustomer:
				out.Message = "its customer is made first; saved as a Draft (your role may not post invoices)"
			case !b.canPost:
				out.Message = "saved as a Draft - your role may not post invoices; post it from the list"
			}
			continue
		}
		if inv.pendingCustomer {
			// The customer was planned but not made (its row failed).
			id, _, err := b.customerFor(inv.customer)
			if err == nil && id == "" {
				err = fmt.Errorf("customer %q could not be made", inv.customer)
			}
			if err != nil {
				out.Result, out.Message = "failed", err.Error()
				continue
			}
			inv.customerID = id
			inv.customerCode = firstNonEmptyStr(bsDocField(b.sw.schema, "Customer", id, "code"), id)
			inv.customerGSTIN, inv.custState = b.customerState(id, inv.customer)
		}
		id, err := b.saveDoc("SalesInvoice", b.invoiceBody(inv))
		if err != nil {
			out.Result, out.Message = "failed", err.Error()
			continue
		}
		out.InvoiceID, out.Status = id, "Draft"
		out.InvoiceNumber = bsDocField(b.sw.schema, "SalesInvoice", id, "invoice_number")
		if !b.canPost {
			out.Result, out.Message = "done", "saved as a Draft - your role may not post invoices; post it from the list"
			continue
		}
		b.post(inv)
	}
}

func (b *bsRun) post(inv *bsInvoice) {
	out := inv.out
	_, err := b.write("POST /api/v1/finance/sales-invoice/{id}/post", http.MethodPost,
		"/api/v1/finance/sales-invoice/"+url.PathEscape(out.InvoiceID)+"/post", map[string]string{"id": out.InvoiceID}, nil, handlePostSalesInvoice)
	if err != nil {
		out.Result, out.Message = "failed", "saved as a Draft, not posted: "+err.Error()
		return
	}
	out.Result, out.Status = "done", "Approved"
	if out.Message == "" {
		out.Message = "posted"
	}
}

// invoiceBody is what the New Sales Invoice screen saves (view-sales-invoice.js),
// with each line's discount and the barcode it was found by.
func (b *bsRun) invoiceBody(inv *bsInvoice) map[string]interface{} {
	interstate := inv.custState != "" && inv.storeState != "" && inv.custState != inv.storeState
	items := make([]map[string]interface{}, 0, len(inv.lines))
	var taxable, tax, total float64
	for _, l := range inv.lines {
		net := bsRound(float64(l.Qty)*l.Rate - l.discount)
		lineTaxable := bsRound(net / (1 + l.gstRate/100))
		lineTax := bsRound(net - lineTaxable)
		taxable += lineTaxable
		tax += lineTax
		total += net
		it := map[string]interface{}{"sku": l.sku, "item_name": l.name, "hsn_code": l.hsn, "barcode": l.Barcode, "qty": l.Qty,
			"rate": l.Rate, "discount": l.discount, "gst_rate": l.gstRate, "taxable": lineTaxable, "tax": lineTax, "amount": net}
		if l.mrp > 0 {
			it["mrp"] = l.mrp
		}
		items = append(items, it)
	}
	taxable, tax, total = bsRound(taxable), bsRound(tax), bsRound(total)
	raw, _ := json.Marshal(items)
	body := map[string]interface{}{
		"customer": inv.customerCode, "location": inv.store, "invoice_date": inv.date,
		"customer_reference": inv.ref, "upload_ref": inv.ref, "source": "Upload from Excel",
		"interstate": interstate, "items": string(raw),
		"taxable_amount": taxable, "total_tax": tax, "total_amount": total, "status": "Draft",
	}
	if inv.customerGSTIN != "" {
		body["customer_gstin"] = inv.customerGSTIN
	}
	if pos := firstNonEmptyStr(inv.custState, inv.storeState); pos != "" {
		body["place_of_supply"] = pos
	}
	if interstate {
		body["cgst"], body["sgst"], body["igst"] = 0.0, 0.0, tax
	} else {
		half := bsRound(tax / 2)
		body["cgst"], body["sgst"], body["igst"] = half, bsRound(tax-half), 0.0
	}
	return body
}

func (b *bsRun) totals(invs []*bsInvoice) {
	t := &b.res.Totals
	for _, c := range b.res.Customers {
		switch {
		case c.Action == "error" || c.Result == "failed":
			t.Errors++
		case c.Action == "new":
			t.NewCustomers++
		}
	}
	for _, inv := range invs {
		out := inv.out
		switch out.Action {
		case "error":
			t.Errors++
			continue
		case "existing":
			t.ExistingInvoices++
		default:
			t.NewInvoices++
		}
		t.Lines += len(inv.lines)
		t.Qty += out.Qty
		t.Discount = bsRound(t.Discount + out.Discount)
		t.Amount = bsRound(t.Amount + out.Amount)
		switch out.Result {
		case "done":
			t.Done++
			if out.Status == "Draft" {
				t.Drafts++
			} else {
				t.Posted++
			}
		case "failed":
			t.Failed++
		}
	}
}

// ---------------------------------------------------------------- handlers

func bsAllowed(w http.ResponseWriter, r *http.Request, action string) bool {
	return requireDocPermission(w, r, "SalesInvoice", action, "You do not have permission to work on sales invoices")
}

func bsPrepare(w http.ResponseWriter, r *http.Request, apply bool) (*bsRun, []byte, string, func() *bsResult, bool) {
	if !bsAllowed(w, r, "create") {
		return nil, nil, "", nil, false
	}
	file, name, ok := swReadUpload(w, r)
	if !ok {
		return nil, nil, "", nil, false
	}
	customers, lines, err := bsRead(file, engines.ClientDateOrder(r.Header.Get("Resolved-Tenant-ID")))
	if err != nil {
		writeAPIErrorGeneric(w, r, http.StatusUnprocessableEntity, err.Error())
		return nil, nil, "", nil, false
	}
	if len(lines) == 0 {
		writeAPIErrorGeneric(w, r, http.StatusUnprocessableEntity, "The Sales sheet has no rows to upload.")
		return nil, nil, "", nil, false
	}
	run, err := newBSRun(newSWCaller(r), apply)
	if err != nil {
		writeAPIErrorGeneric(w, r, http.StatusInternalServerError, err.Error())
		return nil, nil, "", nil, false
	}
	return run, file, name, func() *bsResult { return run.process(customers, lines) }, true
}

// handleBulkSalesPreview reads an uploaded file and returns what each row
// would do. Nothing is changed.
func handleBulkSalesPreview(w http.ResponseWriter, r *http.Request) {
	_, _, name, process, ok := bsPrepare(w, r, false)
	if !ok {
		return
	}
	res := process()
	_ = json.NewEncoder(w).Encode(map[string]interface{}{"file_name": name, "result": res})
}

// handleBulkSalesApply starts applying an uploaded file and returns the run's
// id at once; the screen follows it on the run endpoint.
func handleBulkSalesApply(w http.ResponseWriter, r *http.Request) {
	run, file, name, process, ok := bsPrepare(w, r, true)
	if !ok {
		return
	}
	c := run.sw.c
	lockAny, _ := swApplying.LoadOrStore(c.tenantID, &sync.Mutex{})
	lock := lockAny.(*sync.Mutex)
	if !lock.TryLock() {
		writeAPIErrorGeneric(w, r, http.StatusConflict, "Another upload is being applied to this organisation - wait for it to finish.")
		return
	}
	runID, err := engines.CreateWorkbookRun(c.tenantID, engines.BulkSalesDoctype, c.username, name, file)
	if err != nil {
		lock.Unlock()
		writeAPIErrorGeneric(w, r, http.StatusInternalServerError, err.Error())
		return
	}
	run.progress = func(step string, done, total int) {
		_ = engines.UpdateWorkbookRun(c.tenantID, engines.BulkSalesDoctype, runID, map[string]interface{}{
			"progress_done": done, "progress_total": total, "progress_sheet": step,
		})
	}
	go func() {
		defer lock.Unlock()
		var res *bsResult
		func() {
			defer func() {
				if p := recover(); p != nil {
					_ = engines.UpdateWorkbookRun(c.tenantID, engines.BulkSalesDoctype, runID, map[string]interface{}{"status": "Failed", "message": fmt.Sprint("internal error: ", p)})
					res = nil
				}
			}()
			res = process()
		}()
		if res == nil {
			return
		}
		t := res.Totals
		var buf bytes.Buffer
		cw := csv.NewWriter(&buf)
		_ = cw.Write([]string{"sheet", "row_number", "key", "message"})
		for _, c := range res.Customers {
			if c.Action == "error" || c.Result == "failed" {
				_ = cw.Write([]string{"Customers", strconv.Itoa(c.Row), c.Key, c.Message})
			}
		}
		for _, inv := range res.Invoices {
			if inv.Action == "error" || inv.Result == "failed" {
				_ = cw.Write([]string{"Sales", "", inv.Ref, inv.Message})
				for _, l := range inv.Lines {
					if l.Message != "" {
						_ = cw.Write([]string{"Sales", strconv.Itoa(l.Row), l.Barcode, l.Message})
					}
				}
			}
		}
		cw.Flush()
		status := "Completed"
		if t.Failed > 0 || t.Errors > 0 {
			status = "Completed with errors"
		}
		_ = engines.UpdateWorkbookRun(c.tenantID, engines.BulkSalesDoctype, runID, map[string]interface{}{
			"status": status, "result": res, "totals": t,
			"total_rows": t.Lines, "success_rows": t.Done, "failed_rows": t.Failed + t.Errors,
			"error_csv": buf.String(), "finished_at": time.Now().UTC().Format(time.RFC3339),
		})
		engines.LogAuditEvent(c.tenantID, c.username, "BULK_SALES", "APPLIED",
			fmt.Sprintf("%s: %d new invoices, %d posted, %d drafts, %d failed, %d with errors (run %s)", name, t.NewInvoices, t.Posted, t.Drafts, t.Failed, t.Errors, runID))
	}()
	_ = json.NewEncoder(w).Encode(map[string]string{"run_id": runID, "status": "Running"})
}

func handleBulkSalesRuns(w http.ResponseWriter, r *http.Request) {
	if !bsAllowed(w, r, "read") {
		return
	}
	runs, err := engines.ListWorkbookRuns(r.Header.Get("Resolved-Tenant-ID"), engines.BulkSalesDoctype, 50)
	if err != nil {
		writeAPIErrorGeneric(w, r, http.StatusInternalServerError, err.Error())
		return
	}
	if runs == nil {
		runs = []map[string]interface{}{}
	}
	_ = json.NewEncoder(w).Encode(runs)
}

func handleBulkSalesRun(w http.ResponseWriter, r *http.Request) {
	if !bsAllowed(w, r, "read") {
		return
	}
	run, err := engines.GetWorkbookRun(r.Header.Get("Resolved-Tenant-ID"), engines.BulkSalesDoctype, r.PathValue("id"))
	if err != nil {
		writeAPIErrorGeneric(w, r, http.StatusNotFound, err.Error())
		return
	}
	_ = json.NewEncoder(w).Encode(run)
}

func handleBulkSalesRunFile(w http.ResponseWriter, r *http.Request) {
	if !bsAllowed(w, r, "read") {
		return
	}
	name, file, err := engines.GetWorkbookRunFile(r.Header.Get("Resolved-Tenant-ID"), engines.BulkSalesDoctype, r.PathValue("id"))
	if err != nil {
		writeAPIErrorGeneric(w, r, http.StatusNotFound, err.Error())
		return
	}
	swWriteXLSX(w, name, file)
}

// handleBulkSalesTemplate builds the blank template for this organisation:
// Start Here, Customers, Sales and Example, with the client's stores and the
// states in drop-downs (engines.WriteXLSXBook, no file on disk).
func handleBulkSalesTemplate(w http.ResponseWriter, r *http.Request) {
	if !bsAllowed(w, r, "read") {
		return
	}
	run, err := newSWRun(newSWCaller(r), false, false)
	if err != nil {
		writeAPIErrorGeneric(w, r, http.StatusInternalServerError, err.Error())
		return
	}
	file, err := bsTemplate(run)
	if err != nil {
		writeAPIErrorGeneric(w, r, http.StatusInternalServerError, err.Error())
		return
	}
	swWriteXLSX(w, "sales-upload-template.xlsx", file)
}

func bsStoreOptions(run *swRun) []string {
	idx := run.index("Location")
	var codes []string
	n := 0
	for _, rec := range idx.recs {
		if swText(rec.Data["status"]) == "Inactive" {
			continue
		}
		if code := swText(rec.Data["code"]); code != "" {
			codes = append(codes, code)
			n += len(code) + 1
		}
	}
	sort.Strings(codes)
	if n > 250 { // Excel's in-cell list is 255 characters; a longer list gets no drop-down
		return nil
	}
	return codes
}

func bsTemplate(run *swRun) ([]byte, error) {
	start := engines.XLSXSheet{Name: "Start Here", ColWidths: []float64{18, 110}, Rows: [][]engines.XLSXCell{
		{swBold("Sales - Upload from Excel")},
		{swT("Downloaded"), swT(time.Now().Format("2006-01-02 15:04"))},
		{},
		{swBold("How it works")},
		{swT("1"), swT("Sales sheet: one row per item sold - the Invoice Ref (your name for the bill), the Customer, the Store it is sold from, the Invoice Date, the item's Barcode and Qty. Rows with the same Invoice Ref and Customer become one Sales Invoice.")},
		{swT("2"), swT("Rate is the price per piece including GST. Leave it blank to bill the item's own sale price (or MRP). Discount is what comes off: an amount per piece (12.50) or a percentage (10%). It is taken before GST and printed on the invoice.")},
		{swT("3"), swT("Customers sheet: a customer not yet in Wholeops - name, phone, GSTIN and State. A customer already there is found by GSTIN, phone or name and left as it is. GST splits CGST + SGST or IGST from the store's state and the customer's (GSTIN first).")},
		{swT("4"), swT("Upload it on Financial Accounting → Sales Invoice → Upload from Excel. Preview says what every row will do and changes nothing. Apply makes the customers, saves each invoice and posts it as you where your role may post invoices; otherwise it is left as a Draft to post from the list.")},
		{swT("5"), swT("Uploading the same file again is safe: an invoice already made (same customer and Invoice Ref) is found, not made twice. Fix the rows marked as problems and upload again.")},
		{},
		{swBold("Dates")},
		{swT(""), swT("Type a date as a date in Excel, or the way your organisation writes dates (day first unless your administrator changed it). The grey row under each header says what the column takes; it is skipped on upload.")},
	}}
	stateOpts := engines.IndianStateNames()
	customers := swTableSheet([]swCol{
		{Head: "Customer Name *", Hint: "as on the Sales sheet", Width: 30},
		{Head: "Phone", Hint: "10 digits; finds an existing customer", Width: 16},
		{Head: "GSTIN", Hint: "15 characters, for a registered buyer", Width: 20},
		{Head: "State", Hint: "pick from the list - decides CGST + SGST or IGST", Width: 22, Options: stateOpts},
		{Head: "City", Hint: "optional", Width: 18},
	}, nil)
	customers.Name = "Customers"
	sales := swTableSheet([]swCol{
		{Head: "Invoice Ref *", Hint: "your name for the bill; same ref + customer = one invoice", Width: 16},
		{Head: "Customer *", Hint: "name, phone or code - on the Customers sheet or already in Wholeops", Width: 26},
		{Head: "Store *", Hint: "the store it is sold from", Width: 14, Options: bsStoreOptions(run)},
		{Head: "Invoice Date *", Hint: "an Excel date, or typed in your date order", Width: 14},
		{Head: "Barcode *", Hint: "the item's barcode or code", Width: 18},
		{Head: "Qty *", Hint: "whole number", Width: 8},
		{Head: "Rate (₹, incl. GST)", Hint: "per piece; blank = the item's sale price / MRP", Width: 18},
		{Head: "Discount", Hint: "per piece (12.50) or a percentage (10%)", Width: 14},
	}, nil)
	sales.Name = "Sales"
	example := engines.XLSXSheet{Name: "Example", ColWidths: []float64{16, 26, 14, 14, 18, 8, 18, 14}, Rows: [][]engines.XLSXCell{
		{swBold("Sales sheet, filled in - two invoices, one with a line discount")},
		{swHeadC("Invoice Ref *"), swHeadC("Customer *"), swHeadC("Store *"), swHeadC("Invoice Date *"), swHeadC("Barcode *"), swHeadC("Qty *"), swHeadC("Rate (₹, incl. GST)"), swHeadC("Discount")},
		{swT("B-1001"), swT("Sharma Traders"), swT("HO"), swT("20-10-2026"), swT("8901234567890"), swNum(2), swNum(1250), swT("10%")},
		{swT("B-1001"), swT("Sharma Traders"), swT("HO"), swT("20-10-2026"), swT("8901234567891"), swNum(1), swT(""), swT("")},
		{swT("B-1002"), swT("Anita Rao"), swT("HO"), swT("21-10-2026"), swT("8901234567890"), swNum(3), swNum(1200), swNum(50)},
		{},
		{swBold("Customers sheet, filled in - only the customers the Sales rows name and that are not in Wholeops yet")},
		{swHeadC("Customer Name *"), swHeadC("Phone"), swHeadC("GSTIN"), swHeadC("State"), swHeadC("City")},
		{swT("Sharma Traders"), swT("9876543210"), swT("29ABCDE1234F1Z5"), swT("Karnataka"), swT("Bengaluru")},
		{swT("Anita Rao"), swT("9812345678"), swT(""), swT("Karnataka"), swT("Mysuru")},
	}}
	return engines.WriteXLSXBook([]engines.XLSXSheet{start, customers, sales, example})
}
