package server

import (
	"bytes"
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

// Stage 129: the Bulk Purchase Upload. A client fills one Excel file
// (Vendors, Purchase and Bills sheets; ask 237 builds the template per
// client, bulk_purchase_template.go)
// and one upload makes what it needs - vendors, categories, product types,
// HSN codes, item names, attribute values, designs and combinations - then
// the purchase orders (one per vendor + PO Ref), approved as the uploader
// where the approval rules allow, and receives every bill on the Bills sheet
// through the goods-receipt steps with the sheet's MRP. Preview says what
// each row will do and changes nothing; Apply runs in the background and is
// kept as an ImportJob run (engines.BulkPurchaseDoctype) with its file.
//
// Every write replays the API call its own screen makes (swCaller, Stage
// 128): the record saves the PO's Add design makes, the PO save, approval
// submit / decide and each goods-receipt step, so numbering, GST, place of
// supply, permissions and audit are the screens' own. Re-uploading the same
// file is safe: a PO is found again by vendor + PO Ref (stamped on it as
// upload_ref) and a bill by vendor + invoice no. User Guide §6.12; plan
// docs/product/bulk-purchase-upload-plan-2026-10-22.md.

// Purchase sheet columns read by name. Any other column whose header is not
// one of these is a product attribute (Colour, Polish, Size...).
const (
	bpColRef      = "po ref"
	bpColVendor   = "vendor"
	bpColStore    = "store"
	bpColDesign   = "design no"
	bpColType     = "product type"
	bpColCategory = "category"
	bpColHSN      = "hsn code"
	bpColGST      = "gst %"
	bpColQty      = "qty"
	bpColRate     = "purchase rate"
	bpColMRP      = "mrp"
	bpColInvoice  = "vendor invoice no"
	bpColRecv     = "received qty"
	bpColPhoto    = "photo"
	// 237.2: the PO + GRN template carries the invoice date on every row
	// (no Bills sheet). 237.5: the reorder template names a combination.
	bpColInvDate = "invoice date"
	bpColCombo   = "combination id"
)

var bpNotAttributes = map[string]bool{
	"check": true, bpColRef: true, bpColVendor: true, bpColStore: true, bpColDesign: true, bpColType: true,
	bpColCategory: true, bpColHSN: true, bpColGST: true, bpColQty: true, bpColRate: true, bpColMRP: true,
	"amount": true, bpColInvoice: true, bpColRecv: true, "received amount": true, bpColPhoto: true,
	bpColInvDate: true, bpColCombo: true, "reason": true,
}

// bpGRNLineChars: what one received line takes in the goods receipt's
// line field once the receipt has filled it in (measured: ~330).
const bpGRNLineChars = 360

// bpNotReceivable: PO statuses goods are not received against.
var bpNotReceivable = map[string]bool{"": true, "Draft": true, "Pending Approval": true, "Rejected": true, "Cancelled": true, "Closed": true}

// ---------------------------------------------------------------- input

type bpVendorIn struct {
	Row                                    int
	Name, State, City, GSTIN, Phone, Email string
}

type bpAttrCol struct {
	Key, Label string // Key: bpAttrKey(Label)
}

type bpLine struct {
	Row                                                  int
	Ref, Vendor, Store, Design, PType, Category, HSNText string
	GSTText                                              string
	Attrs                                                map[string]string // attribute key -> value
	Qty                                                  int
	Rate, MRP                                            float64
	Invoice                                              string
	Photos                                               []string // the client's photo file names
	Recv                                                 int
	HasRecv                                              bool
	err                                                  string
	// 237.5: Combo is a reorder row's Combination ID; reorder rows name what
	// exists and never make a design. 237.2: InvDate (ISO) is set on every
	// row of the PO + GRN template (invMode).
	Combo, InvDate   string
	reorder, invMode bool
	// 237.6: dropped - the row is left out to fix; the rest of its PO goes on.
	dropped bool

	vendorID, storeID string
	hsn               string
	inm               *bpItemName
	design            *bpDesign
	codes             map[string]string // attribute code -> value
	sku               string
	po                *bpPO
	out               *bpLineOut
}

type bpBillIn struct {
	Row                   int
	Invoice, Vendor, Date string
	Total                 float64
	err                   string
	// fromRows: made from the PO + GRN template's rows (237.2); its total
	// is its rows' total.
	fromRows bool
}

// ---------------------------------------------------------------- output

// bpRowOut is one row of the Vendors sheet, or one master the upload makes.
type bpRowOut struct {
	Row     int    `json:"row,omitempty"`
	Key     string `json:"key"`
	Action  string `json:"action"` // new | existing | error | note
	Message string `json:"message,omitempty"`
	Result  string `json:"result,omitempty"` // done | failed
}

type bpLineOut struct {
	Row     int     `json:"row"`
	Design  string  `json:"design"`                // the client's label, as typed
	Code    string  `json:"design_code,omitempty"` // the design's own number in Wholeops
	Variant string  `json:"variant,omitempty"`
	Action  string  `json:"action"` // new design | new combination | existing | error
	SKU     string  `json:"sku,omitempty"`
	Qty     int     `json:"qty"`
	Rate    float64 `json:"rate"`
	MRP     float64 `json:"mrp"`
	Bill    string  `json:"bill,omitempty"`
	// Photos: the client's photo file names; the screen uploads the files it
	// was given to this row's Combination ID (and the first to its design).
	Photos  []string `json:"photos,omitempty"`
	Message string   `json:"message,omitempty"`
}

type bpPOOut struct {
	Ref      string       `json:"ref"`
	Vendor   string       `json:"vendor"`
	Store    string       `json:"store"`
	Action   string       `json:"action"` // new | existing | error
	POID     string       `json:"po_id,omitempty"`
	PONumber string       `json:"po_number,omitempty"`
	Status   string       `json:"status,omitempty"`
	Qty      int          `json:"qty"`
	Amount   float64      `json:"amount"`
	Lines    []*bpLineOut `json:"lines"`
	Message  string       `json:"message,omitempty"`
	Result   string       `json:"result,omitempty"` // done | failed | waiting
}

type bpBillOut struct {
	Row     int     `json:"row"`
	Invoice string  `json:"invoice"`
	Vendor  string  `json:"vendor"`
	Date    string  `json:"date"`
	Total   float64 `json:"total"`
	RowsSum float64 `json:"rows_total"`
	Lines   int     `json:"lines"`
	Qty     int     `json:"qty"`
	Action  string  `json:"action"` // new | existing | error | waiting
	GRNID   string  `json:"grn_id,omitempty"`
	GRNCode string  `json:"grn_code,omitempty"`
	Step    string  `json:"step,omitempty"`
	Message string  `json:"message,omitempty"`
	Result  string  `json:"result,omitempty"` // done | failed | waiting
	// SKUs received, for Print stickers on the screen.
	SKUs     []string `json:"skus,omitempty"`
	vendorID string
	lines    []*bpLine
	fromRows bool // the PO + GRN template's bill: its problems are its rows'
}

type bpTotals struct {
	NewVendors  int     `json:"new_vendors"`
	NewMasters  int     `json:"new_masters"`
	NewPOs      int     `json:"new_pos"`
	ExistingPOs int     `json:"existing_pos"`
	Lines       int     `json:"lines"`
	Qty         int     `json:"qty"`
	Amount      float64 `json:"amount"`
	Bills       int     `json:"bills"`
	Errors      int     `json:"errors"`
	Done        int     `json:"done"`
	Failed      int     `json:"failed"`
	Waiting     int     `json:"waiting"`
	RowsToFix   int     `json:"rows_to_fix"` // rows in the "rows to fix" file (237.6)
}

type bpResult struct {
	Vendors []*bpRowOut  `json:"vendors"`
	Masters []*bpRowOut  `json:"masters"`
	POs     []*bpPOOut   `json:"pos"`
	Bills   []*bpBillOut `json:"bills"`
	Totals  bpTotals     `json:"totals"`
	// Fix: 237.6 - the rows that did not load, with why; the run's "rows to
	// fix" workbook is the file's own rows for these plus a Reason column.
	Fix []bpFixRow `json:"fix_rows,omitempty"`
}

// bpFixRow is one sheet row that did not load. NewRef is the PO Ref the row
// must carry when uploaded again, set when its PO was made without it.
type bpFixRow struct {
	Sheet  string `json:"sheet"` // vendors | purchase | bills
	Row    int    `json:"row"`
	Reason string `json:"reason"`
	NewRef string `json:"new_ref,omitempty"`
}

// ---------------------------------------------------------------- reading

func bpHeaderWords() map[string][]string {
	return map[string][]string{
		"vendors":  {"Vendor Name", "State"},
		"purchase": {"PO Ref", "Design No"},
		"bills":    {"Vendor Invoice No", "Invoice Total"},
	}
}

// bpAttrKey matches an attribute by its name: case, spacing and the
// British / American spelling of colour do not matter.
func bpAttrKey(s string) string {
	return strings.ReplaceAll(swNorm(s), "colour", "color")
}

// bpCleanHead is a header as a person reads it: no trailing * or note.
func bpCleanHead(s string) string {
	s = strings.TrimSpace(strings.TrimSuffix(strings.TrimSpace(s), "*"))
	if i := strings.Index(s, "("); i > 0 {
		s = s[:i]
	}
	return strings.TrimSpace(s)
}

// bpHSNDigits reads "7117 - Imitation jewellery", "7117 19" or "711719".
func bpHSNDigits(s string) string {
	if i := strings.Index(s, "-"); i > 0 {
		s = s[:i]
	}
	var b strings.Builder
	for _, r := range s {
		switch {
		case r >= '0' && r <= '9':
			b.WriteRune(r)
		case r == ' ' || r == '.':
		default:
			return b.String()
		}
	}
	return b.String()
}

// bpPhotoNames reads a Photo cell: one or more file names, separated by
// commas, semicolons or new lines; a path is cut to its file name.
func bpPhotoNames(s string) []string {
	var out []string
	for _, f := range strings.FieldsFunc(s, func(r rune) bool { return r == ',' || r == ';' || r == '\n' || r == '\r' }) {
		f = strings.TrimSpace(f)
		if i := strings.LastIndexAny(f, `/\`); i >= 0 {
			f = f[i+1:]
		}
		if f != "" {
			out = append(out, f)
		}
	}
	return out
}

// bpDate reads a date cell: an Excel date number or a typed date, read in
// the client's date order (47.14.2a: engines.ParseDateInOrder, as imports).
func bpDate(s, order string) (string, error) {
	s = strings.TrimSpace(s)
	if f, err := strconv.ParseFloat(s, 64); err == nil && f > 20000 && f < 80000 {
		return time.Date(1899, 12, 30, 0, 0, 0, 0, time.UTC).AddDate(0, 0, int(f)).Format("2006-01-02"), nil
	}
	if iso, err := engines.ParseDateInOrder(s, order); err == nil && len(iso) >= 10 {
		return iso[:10], nil
	}
	example := map[string]string{engines.DateOrderMDY: "10-20-2026", engines.DateOrderYMD: "2026-10-20"}[order]
	if example == "" {
		example = "20-10-2026"
	}
	return "", fmt.Errorf("%q is not a date - type it like %s", s, example)
}

func bpWhole(s, what string) (int, error) {
	f, err := swNumber(s)
	if err != nil || f < 0 || f != math.Trunc(f) || f > 1e6 {
		return 0, fmt.Errorf("%s must be a whole number (%q)", what, s)
	}
	return int(f), nil
}

// bpRead reads the three sheets. A sheet that is missing is empty; the
// Purchase sheet is required. dateOrder is the client's date order (bpDate).
func bpRead(file []byte, dateOrder string) ([]*bpVendorIn, []*bpLine, []*bpBillIn, []bpAttrCol, error) {
	tables, _, err := swParse(file, bpHeaderWords())
	if err != nil {
		return nil, nil, nil, nil, err
	}
	pt := tables["purchase"]
	if pt == nil || !pt.has("PO Ref") {
		return nil, nil, nil, nil, errors.New("this file has no Purchase sheet with a PO Ref column - download the Bulk Purchase template and fill that in")
	}
	var vendors []*bpVendorIn
	if t := tables["vendors"]; t != nil {
		for _, r := range t.rows {
			vendors = append(vendors, &bpVendorIn{Row: r.Num, Name: t.get(r, "Vendor Name", "Vendor"), State: t.get(r, "State"),
				City: t.get(r, "City"), GSTIN: strings.ToUpper(t.get(r, "GSTIN")), Phone: t.get(r, "Phone"), Email: t.get(r, "Email")})
		}
	}
	// 237.5: the reorder template names a Combination ID and has no Product
	// Type; its rows only order what exists. 237.2: the PO + GRN template
	// has an Invoice Date column, and every row carries its bill.
	reorder := pt.has(bpColCombo) && !pt.has(bpColType)
	invMode := pt.has(bpColInvDate)
	// Attribute columns, in sheet order.
	type colAt struct {
		key string
		at  int
	}
	var ac []colAt
	for k, c := range pt.cols {
		if !bpNotAttributes[k] && !reorder {
			ac = append(ac, colAt{k, c})
		}
	}
	sort.Slice(ac, func(i, j int) bool { return ac[i].at < ac[j].at })
	var attrs []bpAttrCol
	for _, c := range ac {
		attrs = append(attrs, bpAttrCol{Key: bpAttrKey(c.key), Label: bpCleanHead(pt.heads[c.key])})
	}
	var lines []*bpLine
	for _, r := range pt.rows {
		l := &bpLine{Row: r.Num, Ref: pt.get(r, bpColRef), Vendor: pt.get(r, bpColVendor), Store: pt.get(r, bpColStore),
			Design: pt.get(r, bpColDesign), PType: pt.get(r, bpColType), Category: pt.get(r, bpColCategory),
			HSNText: pt.get(r, bpColHSN), GSTText: pt.get(r, bpColGST), Invoice: pt.get(r, bpColInvoice), Attrs: map[string]string{},
			Photos: bpPhotoNames(pt.get(r, bpColPhoto)), Combo: pt.get(r, bpColCombo), reorder: reorder, invMode: invMode}
		for _, c := range ac {
			if c.at < len(r.Cells) {
				if v := strings.TrimSpace(r.Cells[c.at]); v != "" {
					l.Attrs[bpAttrKey(c.key)] = v
				}
			}
		}
		invDate := pt.get(r, bpColInvDate)
		filled := l.Ref != "" || l.Vendor != "" || l.Store != "" || l.Design != "" || l.PType != "" || l.Category != "" ||
			l.HSNText != "" || l.Invoice != "" || len(l.Attrs) > 0 || pt.get(r, bpColQty) != "" || pt.get(r, bpColRate) != "" ||
			pt.get(r, bpColMRP) != "" || pt.get(r, bpColRecv) != "" || l.Combo != "" || invDate != ""
		if !filled {
			continue // only automatic columns (Check, GST %, Amount) on this row
		}
		need := [][2]string{{"PO Ref", l.Ref}, {"Vendor", l.Vendor}, {"Store", l.Store},
			{"Product Type", l.PType}, {"Category", l.Category}, {"HSN Code", l.HSNText}, {"Qty", pt.get(r, bpColQty)},
			{"Purchase Rate", pt.get(r, bpColRate)}, {"MRP", pt.get(r, bpColMRP)}}
		if reorder {
			// Vendor, rate and MRP left blank come from the design (237.5).
			need = [][2]string{{"PO Ref", l.Ref}, {"Store", l.Store}, {"Qty", pt.get(r, bpColQty)}, {"Combination ID or Design No", l.Combo + l.Design}}
		}
		if invMode {
			need = append(need, [2]string{"Vendor Invoice No", l.Invoice}, [2]string{"Invoice Date", invDate})
		}
		var miss []string
		for _, f := range need {
			if strings.TrimSpace(f[1]) == "" {
				miss = append(miss, f[0])
			}
		}
		var probs []string
		if len(miss) > 0 {
			probs = append(probs, "missing: "+strings.Join(miss, ", "))
		}
		if s := pt.get(r, bpColQty); s != "" {
			if q, err := bpWhole(s, "Qty"); err != nil || q <= 0 {
				probs = append(probs, fmt.Sprintf("Qty must be a whole number above 0 (%q)", s))
			} else {
				l.Qty = q
			}
		}
		for _, m := range []struct {
			col, label string
			into       *float64
		}{{bpColRate, "Purchase Rate", &l.Rate}, {bpColMRP, "MRP", &l.MRP}} {
			if s := pt.get(r, m.col); s != "" {
				if f, err := swNumber(s); err != nil || f <= 0 {
					probs = append(probs, fmt.Sprintf("%s must be an amount above 0 (%q)", m.label, s))
				} else {
					*m.into = f
				}
			}
		}
		if l.MRP > 0 && l.Rate > 0 && l.MRP < l.Rate {
			probs = append(probs, "MRP is below the Purchase Rate")
		}
		if l.HSNText != "" {
			l.hsn = bpHSNDigits(l.HSNText)
			if n := len(l.hsn); n != 4 && n != 6 && n != 8 {
				probs = append(probs, fmt.Sprintf("HSN Code %q is not a 4, 6 or 8 digit HSN - pick it from the list", l.HSNText))
			}
		}
		if s := pt.get(r, bpColRecv); s != "" {
			if q, err := bpWhole(s, "Received Qty"); err != nil {
				probs = append(probs, err.Error())
			} else {
				l.Recv, l.HasRecv = q, true
			}
			if l.Invoice == "" {
				probs = append(probs, "Received Qty needs the Vendor Invoice No")
			}
		}
		if invMode && invDate != "" {
			if d, err := bpDate(invDate, dateOrder); err != nil {
				probs = append(probs, "Invoice Date "+err.Error())
			} else {
				l.InvDate = d
			}
		}
		l.err = strings.Join(probs, "; ")
		lines = append(lines, l)
	}
	var bills []*bpBillIn
	if t := tables["bills"]; t != nil && !invMode {
		for _, r := range t.rows {
			b := &bpBillIn{Row: r.Num, Invoice: t.get(r, "Vendor Invoice No"), Vendor: t.get(r, "Vendor")}
			date, total := t.get(r, "Invoice Date"), t.get(r, "Invoice Total")
			if b.Invoice == "" && b.Vendor == "" && date == "" && total == "" {
				continue
			}
			var probs []string
			var miss []string
			for _, f := range [][2]string{{"Vendor Invoice No", b.Invoice}, {"Vendor", b.Vendor}, {"Invoice Date", date}, {"Invoice Total", total}} {
				if f[1] == "" {
					miss = append(miss, f[0])
				}
			}
			if len(miss) > 0 {
				probs = append(probs, "missing: "+strings.Join(miss, ", "))
			}
			if date != "" {
				if d, err := bpDate(date, dateOrder); err != nil {
					probs = append(probs, err.Error())
				} else {
					b.Date = d
				}
			}
			if total != "" {
				if f, err := swNumber(total); err != nil || f <= 0 {
					probs = append(probs, fmt.Sprintf("Invoice Total must be an amount above 0 (%q)", total))
				} else {
					b.Total = f
				}
			}
			b.err = strings.Join(probs, "; ")
			bills = append(bills, b)
		}
	}
	return vendors, lines, bills, attrs, nil
}

// ---------------------------------------------------------------- plan + apply

type bpItemName struct {
	ID, Name, CategoryID, CategoryName, TypeID, HSN string
	GST                                             float64
	New                                             bool
	err                                             string
}

type bpDesign struct {
	key, designNo, vendorID, id, name string
	inm                               *bpItemName
	exists                            bool
	// detail is shared with the design pool (237.3): a combination one group
	// of rows makes is there for the next group that joins the design.
	detail *engines.DesignDetail
	lines  []*bpLine
	skus   map[string]string // combination key -> SKU
	err    string
}

// bpPoolDesign is a design a group of rows may join (237.3): one already in
// Wholeops, or one this upload made.
type bpPoolDesign struct {
	id     string
	attrs  map[string]string // design attributes, lower case
	detail *engines.DesignDetail
	// label: the file's Design No that took the design in this upload. Rows
	// under another label are another design (the client said so).
	label string
}

type bpPO struct {
	key, vkey, ref, vendorID, storeID string
	lines                             []*bpLine // the rows that go on
	out                               *bpPOOut
	existingLines                     []bpPOItem // a PO this file already made: its lines, in order
	failed                            bool
	dropped                           int    // rows left out to fix (237.6)
	newRef                            string // the PO Ref those rows take when uploaded again
}

type bpPOItem struct {
	sku string
	qty int
}

type bpAttrInfo struct {
	code, label, valueType string
	options                []string
	offered, exists        bool
}

type bpRun struct {
	sw        *swRun
	apply     bool
	res       *bpResult
	vendors   map[string]string // bpAttrKey(name) -> vendor id ("" = to be made)
	vendorNm  map[string]string // vendor id -> name
	vendorErr map[string]string // bpAttrKey(name) -> why it could not be made
	perms     map[string]bool   // doctype|action -> allowed (preview)
	cats      map[string]string // norm name -> id
	types     map[string]string
	hsns      map[string]string // digits -> HSNCode id
	names     map[string]*bpItemName
	attrs     map[*bpItemName]map[string]*bpAttrInfo // attribute key -> info, per item name
	newAttrs  map[string]*bpAttrInfo                 // attributes this upload makes, by key
	newRows   map[string]*bpRowOut
	told      map[string]bool // attribute value already listed as new
	designs   map[string]*bpDesign
	progress  func(step string, done, total int)
	// 237.3 / 237.4 / 237.6
	allowNew bool                       // procurement.bulk_po_new_designs
	pools    map[string][]*bpPoolDesign // item name + vendor -> designs rows may join
	madeSKU  map[string]string          // combination made by this upload -> its row action
	newSeq   int                        // preview stand-ins for combinations not made yet
	poKeys   map[string]bool            // vendor + PO Ref in this file, and refs handed out
	billOf   map[*bpLine]*bpBillOut
	shapes   map[string]*engines.DesignDetail // design -> detail, for a PO already made
}

const bpNewDesignsSetting = "procurement.bulk_po_new_designs"

func newBPRun(c *swCaller, apply bool) (*bpRun, error) {
	sw, err := newSWRun(c, false, apply)
	if err != nil {
		return nil, err
	}
	return &bpRun{sw: sw, apply: apply, res: &bpResult{Vendors: []*bpRowOut{}, Masters: []*bpRowOut{}, POs: []*bpPOOut{}, Bills: []*bpBillOut{}},
		vendors: map[string]string{}, vendorNm: map[string]string{}, vendorErr: map[string]string{}, perms: map[string]bool{}, cats: map[string]string{}, types: map[string]string{},
		hsns: map[string]string{}, names: map[string]*bpItemName{}, attrs: map[*bpItemName]map[string]*bpAttrInfo{}, newAttrs: map[string]*bpAttrInfo{}, newRows: map[string]*bpRowOut{}, told: map[string]bool{}, designs: map[string]*bpDesign{},
		allowNew: engines.GetSettingBool(c.tenantID, bpNewDesignsSetting), pools: map[string][]*bpPoolDesign{}, madeSKU: map[string]string{},
		poKeys: map[string]bool{}, billOf: map[*bpLine]*bpBillOut{}, shapes: map[string]*engines.DesignDetail{}}, nil
}

// write replays one API call and returns its JSON answer.
func (b *bpRun) write(pattern, method, target string, pv map[string]string, body interface{}, h http.HandlerFunc) (map[string]interface{}, error) {
	status, out := b.sw.c.call(pattern, method, target, pv, body, h)
	if status < 200 || status >= 300 {
		return nil, errors.New(swMessage(status, out))
	}
	m := map[string]interface{}{}
	_ = json.Unmarshal(out, &m)
	return m, nil
}

func (b *bpRun) saveDoc(doctype string, body map[string]interface{}) (string, error) {
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

// mayCreate tells the preview, before anything is written, that the
// uploader's role may not make a record the upload needs (Apply would be
// refused by that record's own save).
func (b *bpRun) mayCreate(doctype string) error {
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

func (b *bpRun) master(key, action, msg string) *bpRowOut {
	r := &bpRowOut{Key: key, Action: action, Message: msg}
	b.res.Masters = append(b.res.Masters, r)
	return r
}

// result marks a row done or failed.
func bpDone(r *bpRowOut, err error) {
	if err != nil {
		r.Result, r.Message = "failed", err.Error()
	} else {
		r.Result = "done"
	}
}

// oneMatch is the single record of doctype named name (by name, code...).
func (b *bpRun) oneMatch(doctype, name string) (string, error) {
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

func (b *bpRun) step(name string, done, total int) {
	if b.progress != nil {
		b.progress(name, done, total)
	}
}

// process plans (and, when applying, does) the whole upload.
func (b *bpRun) process(vendors []*bpVendorIn, lines []*bpLine, bills []*bpBillIn, attrCols []bpAttrCol) *bpResult {
	b.step("Vendors", 0, 5)
	b.doVendors(vendors, lines, bills)
	pos := b.groupPOs(lines)
	// 237.2: the PO + GRN template has no Bills sheet - each vendor +
	// invoice on its rows is one bill.
	for _, l := range lines {
		if l.invMode {
			bills = bpBillsFromRows(lines)
			break
		}
	}
	b.step("Item names", 1, 5)
	b.doMasters(pos)
	b.matchExisting(pos)
	b.doAttributes(pos, attrCols)
	b.step("Designs", 2, 5)
	b.doDesigns(pos, attrCols)
	b.step("Purchase orders", 3, 5)
	b.doPOs(pos)
	for _, po := range pos {
		if po.dropped > 0 && !po.failed {
			note := fmt.Sprintf("%d row(s) left out to fix", po.dropped)
			if po.out.Message != "" {
				note = po.out.Message + "; " + note
			}
			po.out.Message = note
		}
	}
	b.step("Goods receipts", 4, 5)
	b.doBills(bills, lines)
	b.fixRows(lines)
	b.step("", 5, 5)
	b.totals()
	return b.res
}

// bpBillsFromRows: one bill per vendor + Vendor Invoice No on the PO + GRN
// template's rows, dated by its rows (237.2).
func bpBillsFromRows(lines []*bpLine) []*bpBillIn {
	var out []*bpBillIn
	by := map[string]*bpBillIn{}
	first := map[*bpBillIn]int{}
	for _, l := range lines {
		if l.Invoice == "" || l.Vendor == "" {
			continue
		}
		k := bpAttrKey(l.Vendor) + "|" + swNorm(l.Invoice)
		bi := by[k]
		if bi == nil {
			bi = &bpBillIn{Row: l.Row, Invoice: l.Invoice, Vendor: l.Vendor, Date: l.InvDate, fromRows: true}
			by[k], first[bi] = bi, l.Row
			out = append(out, bi)
			continue
		}
		if bi.Date == "" {
			bi.Date, first[bi] = l.InvDate, l.Row
		} else if l.InvDate != "" && l.InvDate != bi.Date && bi.err == "" {
			bi.err = fmt.Sprintf("row %d has Invoice Date %s and row %d has %s - one bill, one date", first[bi], bi.Date, l.Row, l.InvDate)
		}
	}
	return out
}

// fixRows lists each sheet row that did not fully load, with why - the
// "rows to fix" file (237.6). A row left out of a PO that was made (or will
// be, in the preview) takes a new PO Ref: its own is used now. A row that is
// ordered but not received keeps its PO Ref, so the same row again finds its
// PO and receives it.
func (b *bpRun) fixRows(lines []*bpLine) {
	seen := map[string]bool{}
	add := func(sheet string, row int, reason, ref string) {
		k := sheet + "|" + strconv.Itoa(row)
		if row <= 0 || seen[k] {
			return
		}
		seen[k] = true
		b.res.Fix = append(b.res.Fix, bpFixRow{Sheet: sheet, Row: row, Reason: reason, NewRef: ref})
	}
	for _, v := range b.res.Vendors {
		if v.Action == "error" || v.Result == "failed" {
			add("vendors", v.Row, v.Message, "")
		}
	}
	badBill := func(o *bpBillOut) bool {
		return o.Action == "error" || o.Action == "waiting" || o.Result == "failed" || o.Result == "waiting"
	}
	for _, l := range lines {
		po := l.po
		if po == nil || l.out == nil {
			continue
		}
		switch {
		case l.dropped:
			ref := ""
			if po.out.POID != "" || (!b.apply && !po.failed) {
				ref = b.nextRef(po)
			}
			add("purchase", l.Row, l.out.Message, ref)
		case po.failed:
			add("purchase", l.Row, fmt.Sprintf("PO Ref %s: %s", po.ref, po.out.Message), "")
		case l.Invoice != "":
			bill := b.billOf[l]
			switch {
			case bill == nil:
				add("purchase", l.Row, firstNonEmptyStr(l.out.Message, "its bill is not received"), "")
			case badBill(bill):
				add("purchase", l.Row, fmt.Sprintf("ordered on PO Ref %s; bill %s not received: %s", po.ref, l.Invoice, bill.Message), "")
			}
		}
	}
	for _, bill := range b.res.Bills {
		if badBill(bill) && !bill.fromRows {
			add("bills", bill.Row, bill.Message, "")
		}
	}
}

// nextRef: the PO Ref a PO's left-out rows take when uploaded again -
// "<ref>-2", "-3"... one this file and the vendor's POs do not use yet.
func (b *bpRun) nextRef(po *bpPO) string {
	if po.newRef != "" {
		return po.newRef
	}
	for n := 2; n < 1000; n++ {
		ref := fmt.Sprintf("%s-%d", po.ref, n)
		k := po.vkey + "|" + swNorm(ref)
		if b.poKeys[k] {
			continue
		}
		if po.vendorID != "" {
			if id, _, _, _, _ := bpFindPO(b.sw.schema, po.vendorID, ref); id != "" {
				continue
			}
		}
		b.poKeys[k], po.newRef = true, ref
		return ref
	}
	return ""
}

// --- vendors

func (b *bpRun) doVendors(rows []*bpVendorIn, lines []*bpLine, bills []*bpBillIn) {
	// Only the vendors the purchase rows and bills use are made, so sample
	// rows left on the Vendors sheet never become vendors.
	used := map[string]bool{}
	for _, l := range lines {
		used[bpAttrKey(l.Vendor)] = true
	}
	for _, bi := range bills {
		used[bpAttrKey(bi.Vendor)] = true
	}
	seen := map[string]int{}
	for _, v := range rows {
		out := &bpRowOut{Row: v.Row, Key: v.Name}
		b.res.Vendors = append(b.res.Vendors, out)
		if v.Name != "" && !used[bpAttrKey(v.Name)] {
			out.Action, out.Message = "note", "no Purchase row uses this vendor - left out"
			continue
		}
		fail := func(msg string) { out.Action, out.Message = "error", msg }
		code := engines.StateCodeFromName(v.State)
		switch {
		case v.Name == "" || v.State == "":
			miss := []string{}
			if v.Name == "" {
				miss = append(miss, "Vendor Name")
			}
			if v.State == "" {
				miss = append(miss, "State")
			}
			fail("missing: " + strings.Join(miss, ", "))
			continue
		case code == "":
			fail(fmt.Sprintf("%q is not a state of India - pick it from the list", v.State))
			continue
		case v.GSTIN != "" && len(v.GSTIN) != 15:
			fail("GSTIN must be 15 characters - or leave it blank")
			continue
		case v.GSTIN != "" && engines.StateCodeFromGSTIN(v.GSTIN) != code:
			fail(fmt.Sprintf("GSTIN starts with %s but %s is %s", v.GSTIN[:2], v.State, code))
			continue
		}
		k := bpAttrKey(v.Name)
		if first, dup := seen[k]; dup {
			fail(fmt.Sprintf("listed twice (row %d)", first))
			continue
		}
		seen[k] = v.Row
		id := ""
		var err error
		if v.GSTIN != "" {
			id, err = b.oneMatch("Vendor", v.GSTIN)
		}
		if id == "" && err == nil {
			id, err = b.oneMatch("Vendor", v.Name)
		}
		if err != nil {
			fail(err.Error() + " - use the vendor's code")
			continue
		}
		if id != "" {
			out.Action = "existing"
			b.vendors[k], b.vendorNm[id] = id, b.sw.display("Vendor", id)
			continue
		}
		out.Action = "new"
		b.vendors[k] = ""
		if !b.apply {
			if err := b.mayCreate("Vendor"); err != nil {
				out.Action, out.Message = "error", err.Error()
				delete(b.vendors, k)
				b.vendorErr[k] = fmt.Sprintf("vendor %s: %v", v.Name, err)
			}
			continue
		}
		body := map[string]interface{}{"name": v.Name, "state": v.State, "status": "Active"}
		for f, val := range map[string]string{"gstin": v.GSTIN, "city": v.City, "contact_phone": v.Phone, "contact_email": v.Email} {
			if val != "" {
				body[f] = val
			}
		}
		id, err = b.saveDoc("Vendor", body)
		bpDone(out, err)
		if err == nil {
			b.vendors[k], b.vendorNm[id] = id, v.Name
		} else {
			delete(b.vendors, k)
			b.vendorErr[k] = fmt.Sprintf("vendor %s could not be made: %v", v.Name, err)
		}
	}
}

// vendorFor resolves a Purchase / Bills sheet vendor: the Vendors sheet
// first, then the vendors already in Wholeops. pending: made by this upload.
func (b *bpRun) vendorFor(name string) (id string, pending bool, err error) {
	if id, ok := b.vendors[bpAttrKey(name)]; ok {
		return id, id == "", nil
	}
	if msg := b.vendorErr[bpAttrKey(name)]; msg != "" {
		return "", false, errors.New(msg)
	}
	id, err = b.oneMatch("Vendor", name)
	if err != nil {
		return "", false, err
	}
	if id == "" {
		return "", false, fmt.Errorf("vendor %q is not on the Vendors sheet or in Wholeops - add it on the Vendors sheet", name)
	}
	b.vendorNm[id] = b.sw.display("Vendor", id)
	return id, false, nil
}

// --- purchase rows -> POs

func (b *bpRun) groupPOs(lines []*bpLine) []*bpPO {
	var pos []*bpPO
	byKey := map[string]*bpPO{}
	designShape := map[string]*bpLine{} // vendor + design -> first row
	nameHSN := map[string]*bpLine{}     // type + category -> first row
	for _, l := range lines {
		reFam := ""
		if l.reorder && l.err == "" {
			reFam, l.err = b.reorderRow(l)
		}
		// The vendor is found even on a row with a problem, so the row joins
		// its PO's group and the rows to fix carry the right PO Ref (237.6).
		if l.vendorID == "" && l.Vendor != "" {
			if id, pending, err := b.vendorFor(l.Vendor); err != nil {
				if l.err == "" {
					l.err = err.Error()
				}
			} else if !pending {
				l.vendorID = id
			}
		}
		vkey := "n:" + bpAttrKey(l.Vendor)
		if l.vendorID != "" {
			vkey = "id:" + l.vendorID
		}
		if l.err == "" {
			id, pending, err := b.sw.resolveLink("Location", l.Store)
			switch {
			case err != nil:
				l.err = fmt.Sprintf("store %q is not in Wholeops - stores are set up first (Setup Workbook or Setup > Location)", l.Store)
			case pending:
				l.err = fmt.Sprintf("store %q is not in Wholeops yet", l.Store)
			default:
				l.storeID = id
			}
		}
		if l.err == "" && !l.reorder {
			dk := vkey + "|" + swNorm(l.Design)
			if f, ok := designShape[dk]; l.Design != "" && ok && (swNorm(f.PType) != swNorm(l.PType) || swNorm(f.Category) != swNorm(l.Category)) {
				l.err = fmt.Sprintf("design %s is %s %s on row %d - one design has one Product Type and Category", l.Design, f.Category, f.PType, f.Row)
			} else if !ok && l.Design != "" {
				designShape[dk] = l
			}
			nk := swNorm(l.PType) + "|" + swNorm(l.Category)
			if f, ok := nameHSN[nk]; ok && f.hsn != l.hsn {
				l.err = fmt.Sprintf("%s + %s has HSN %s on row %d - one Product Type + Category has one HSN", l.PType, l.Category, f.hsn, f.Row)
			} else if !ok {
				nameHSN[nk] = l
			}
		}
		key := vkey + "|" + swNorm(l.Ref)
		po := byKey[key]
		if po == nil {
			po = &bpPO{key: key, vkey: vkey, ref: l.Ref, vendorID: l.vendorID, storeID: l.storeID,
				out: &bpPOOut{Ref: l.Ref, Vendor: l.Vendor, Store: l.Store, Lines: []*bpLineOut{}}}
			byKey[key] = po
			b.poKeys[key] = true
			pos = append(pos, po)
		}
		if l.err == "" && po.storeID != "" && l.storeID != po.storeID {
			l.err = fmt.Sprintf("PO Ref %s has another store on an earlier row - one PO, one store", l.Ref)
		}
		if po.storeID == "" {
			po.storeID = l.storeID
		}
		if po.vendorID == "" {
			po.vendorID = l.vendorID
		}
		l.po = po
		po.lines = append(po.lines, l)
		l.out = &bpLineOut{Row: l.Row, Design: firstNonEmptyStr(l.Design, l.Combo), Qty: l.Qty, Rate: l.Rate, MRP: l.MRP, Bill: l.Invoice, Photos: l.Photos, Action: "error"}
		if l.reorder && l.err == "" {
			l.out.Action, l.out.SKU, l.out.Code = "existing", l.sku, reFam
		}
		po.out.Lines = append(po.out.Lines, l.out)
	}
	// A PO already made from an earlier upload of this file.
	for _, po := range pos {
		if po.vendorID == "" {
			continue
		}
		id, number, status, items, err := bpFindPO(b.sw.schema, po.vendorID, po.ref)
		if err != nil || id == "" {
			continue
		}
		po.out.Action, po.out.POID, po.out.PONumber, po.out.Status = "existing", id, number, status
		po.existingLines = items
	}
	// 237.6: a row with a problem is left out to fix; the PO is made from
	// its other rows. A PO with no good row is not made.
	for _, po := range pos {
		var good []*bpLine
		for _, l := range po.lines {
			if l.err != "" {
				l.dropped = true
				l.out.Message = l.err
				po.dropped++
			} else {
				good = append(good, l)
			}
		}
		po.lines = good
		po.resum()
		if len(good) == 0 {
			po.failed = true
			po.out.Action = "error"
			po.out.Message = "no row of this PO can be made - fix its rows"
		} else if po.out.Action == "" {
			po.out.Action = "new"
		}
		b.res.POs = append(b.res.POs, po.out)
	}
	return pos
}

// resum recounts a PO's qty and amount from the rows that go on.
func (po *bpPO) resum() {
	po.out.Qty, po.out.Amount = 0, 0
	for _, l := range po.lines {
		po.out.Qty += l.Qty
		po.out.Amount += float64(l.Qty) * l.Rate
	}
	po.out.Amount = math.Round(po.out.Amount*100) / 100
}

// dropLine leaves one row out to fix (237.6); its PO goes on without it,
// and is not made at all when no row is left.
func (b *bpRun) dropLine(l *bpLine, msg string) {
	if l.dropped {
		return
	}
	l.dropped = true
	l.out.Action, l.out.Message, l.out.SKU = "error", msg, ""
	po := l.po
	keep := make([]*bpLine, 0, len(po.lines))
	for _, x := range po.lines {
		if x != l {
			keep = append(keep, x)
		}
	}
	po.lines = keep
	po.dropped++
	po.resum()
	if len(keep) == 0 {
		b.failPO(po, "no row of this PO can be made - fix its rows")
	}
}

// reorderRow resolves a reorder template row (237.5): its Combination ID,
// or a design with one combination, must exist; a blank vendor, rate or
// MRP comes from the combination and its design. Nothing is ever made.
// It returns the combination's design.
func (b *bpRun) reorderRow(l *bpLine) (string, string) {
	schema := b.sw.schema
	fam := ""
	var mrp, rate float64
	if l.Combo != "" {
		id, code, family, m, c := bpFindItem(schema, l.Combo)
		if id == "" {
			return "", fmt.Sprintf("Combination ID %s does not exist in Wholeops", l.Combo)
		}
		l.sku, fam, mrp, rate = firstNonEmptyStr(code, id), family, m, c
		if l.Design != "" {
			did, _ := bpFindDesignCode(schema, l.Design)
			if did == "" {
				return "", fmt.Sprintf("design %s does not exist in Wholeops", l.Design)
			}
			if famID, _ := bpFindDesignCode(schema, fam); !strings.EqualFold(famID, did) {
				return "", fmt.Sprintf("Combination ID %s is not of design %s - give one of them, or both of the same design", l.Combo, l.Design)
			}
		}
	} else {
		did, _ := bpFindDesignCode(schema, l.Design)
		if did == "" {
			return "", fmt.Sprintf("design %s does not exist in Wholeops - give its Wholeops design number or the Combination ID", l.Design)
		}
		det, err := engines.GetDesign(b.sw.c.tenantID, did)
		if err != nil {
			return "", err.Error()
		}
		if n := len(det.Combinations); n != 1 {
			return "", fmt.Sprintf("design %s has %d combinations - give the Combination ID", l.Design, n)
		}
		l.sku, fam = det.Combinations[0].SKU, det.Code
		_, _, _, mrp, rate = bpFindItem(schema, l.sku)
	}
	famID, vendor, dmrp, drate := bpDesignRow(schema, fam)
	if l.Vendor == "" {
		if vendor == "" {
			return famID, "its design has no vendor - fill the Vendor"
		}
		l.vendorID = vendor
		l.Vendor = b.sw.display("Vendor", vendor)
		b.vendorNm[vendor] = l.Vendor
	}
	if l.Rate == 0 {
		l.Rate = firstAbove0(rate, drate)
	}
	if l.MRP == 0 {
		l.MRP = firstAbove0(mrp, dmrp)
	}
	switch {
	case l.Rate == 0:
		return famID, "the combination has no purchase rate - fill the Purchase Rate"
	case l.MRP == 0:
		return famID, "the combination has no MRP - fill the MRP"
	case l.MRP < l.Rate:
		return famID, "MRP is below the Purchase Rate"
	}
	return famID, ""
}

func firstAbove0(vals ...float64) float64 {
	for _, v := range vals {
		if v > 0 {
			return v
		}
	}
	return 0
}

// bpFindItem finds a combination by its Combination ID (code or id).
func bpFindItem(schema, ref string) (id, code, family string, mrp, cost float64) {
	var m, c string
	_ = db.DB.QueryRow(fmt.Sprintf(`SELECT id, COALESCE(data->>'code', ''), COALESCE(NULLIF(data->>'family', ''), data->>'parent_product_code', ''),
		COALESCE(data->>'mrp', ''), COALESCE(data->>'standard_cost', '') FROM %s.documents
		WHERE doctype = 'Item' AND deleted_at IS NULL AND (lower(id) = lower($1) OR lower(data->>'code') = lower($1))
		ORDER BY (id = $1) DESC LIMIT 1`, schema), strings.TrimSpace(ref)).Scan(&id, &code, &family, &m, &c)
	mrp, _ = swNumber(m)
	cost, _ = swNumber(c)
	return id, code, family, mrp, cost
}

// bpDesignRow reads a design's vendor and prices (code or id).
func bpDesignRow(schema, ref string) (id, vendor string, mrp, rate float64) {
	var m, r string
	_ = db.DB.QueryRow(fmt.Sprintf(`SELECT id, COALESCE(data->>'vendor', ''), COALESCE(data->>'mrp', ''), COALESCE(data->>'purchase_rate', '')
		FROM %s.documents WHERE doctype = 'ProductFamily' AND deleted_at IS NULL AND (lower(id) = lower($1) OR lower(data->>'code') = lower($1))
		LIMIT 1`, schema), strings.TrimSpace(ref)).Scan(&id, &vendor, &m, &r)
	mrp, _ = swNumber(m)
	rate, _ = swNumber(r)
	return id, vendor, mrp, rate
}

func bpFindPO(schema, vendorID, ref string) (id, number, status string, items []bpPOItem, err error) {
	var raw string
	err = db.DB.QueryRow(fmt.Sprintf(`SELECT id, COALESCE(data->>'po_number', data->>'code', id), COALESCE(NULLIF(status, ''), data->>'status', ''), COALESCE(data->>'items', '')
		FROM %s.documents WHERE doctype = 'PurchaseOrder' AND deleted_at IS NULL
		  AND lower(data->>'upload_ref') = lower($1) AND (data->>'vendor' = $2 OR data->>'vendor_id' = $2)
		  AND COALESCE(NULLIF(status, ''), data->>'status', '') NOT IN ('Cancelled', 'Rejected')
		ORDER BY created_at DESC LIMIT 1`, schema), ref, vendorID).Scan(&id, &number, &status, &raw)
	if err != nil {
		return "", "", "", nil, err
	}
	var lines []map[string]interface{}
	_ = json.Unmarshal([]byte(raw), &lines)
	for _, l := range lines {
		q, _ := swNumber(swText(l["qty"]))
		items = append(items, bpPOItem{sku: swText(l["sku"]), qty: int(q)})
	}
	return id, number, status, items, nil
}

func bpPOStatus(schema, id string) (number, status string) {
	_ = db.DB.QueryRow(fmt.Sprintf(`SELECT COALESCE(data->>'po_number', data->>'code', id), COALESCE(NULLIF(status, ''), data->>'status', '')
		FROM %s.documents WHERE id = $1`, schema), id).Scan(&number, &status)
	return number, status
}

// usable: the rows that go on (their PO has no problem).
func bpUsable(pos []*bpPO) []*bpLine {
	var out []*bpLine
	for _, po := range pos {
		if po.failed {
			continue
		}
		out = append(out, po.lines...)
	}
	return out
}

// failPO stops a PO: nothing more of it is made.
func (b *bpRun) failPO(po *bpPO, msg string) {
	if po.failed {
		return
	}
	po.failed = true
	po.out.Result, po.out.Message = "failed", msg
	if !b.apply {
		po.out.Action, po.out.Result = "error", ""
	}
}

// --- categories, product types, HSN codes, item names

func (b *bpRun) doMasters(pos []*bpPO) {
	for _, l := range bpUsable(pos) {
		if l.po.failed || l.dropped || l.reorder {
			continue
		}
		inm, err := b.itemName(l)
		if err != nil {
			b.dropLine(l, err.Error())
			continue
		}
		l.inm = inm
	}
}

func (b *bpRun) simpleMaster(doctype, label, name string, cache map[string]string) (string, error) {
	k := swNorm(name)
	if id, ok := cache[k]; ok {
		return id, nil
	}
	id, err := b.oneMatch(doctype, name)
	if err != nil {
		return "", err
	}
	if id == "" {
		if !b.apply {
			if err := b.mayCreate(doctype); err != nil {
				return "", err
			}
		}
		row := b.master(label+": "+name, "new", "")
		if b.apply {
			id, err = b.saveDoc(doctype, map[string]interface{}{"name": name, "status": "Active"})
			bpDone(row, err)
			if err != nil {
				return "", fmt.Errorf("%s %s could not be made: %v", label, name, err)
			}
		}
	}
	cache[k] = id
	return id, nil
}

func (b *bpRun) hsnCode(l *bpLine) (string, float64, error) {
	if id, ok := b.hsns[l.hsn]; ok {
		return id, b.hsnRate(id, l), nil
	}
	id, err := b.oneMatch("HSNCode", l.hsn)
	if err != nil {
		return "", 0, err
	}
	gst := b.hsnRate(id, l)
	if gst < 0 {
		return "", 0, fmt.Errorf("HSN %s is new and its GST %% is blank - pick the HSN from the list", l.hsn)
	}
	if id == "" {
		if !b.apply {
			if err := b.mayCreate("HSNCode"); err != nil {
				return "", 0, err
			}
		}
		row := b.master(fmt.Sprintf("HSN code: %s (GST %s%%)", l.hsn, swNumText(gst)), "new", "")
		if b.apply {
			desc := strings.TrimSpace(strings.TrimPrefix(strings.TrimPrefix(l.HSNText, l.hsn), "-"))
			body := map[string]interface{}{"hsn": l.hsn, "gst_rate": gst, "status": "Active"}
			if desc != "" && desc != l.HSNText {
				body["description"] = desc
			}
			id, err = b.saveDoc("HSNCode", body)
			bpDone(row, err)
			if err != nil {
				return "", 0, fmt.Errorf("HSN %s could not be made: %v", l.hsn, err)
			}
		}
	}
	b.hsns[l.hsn] = id
	return id, gst, nil
}

// hsnRate: an existing HSN code's own GST rate, else the row's GST % (the
// template fills it from its fixed HSN list); -1 when neither is known.
func (b *bpRun) hsnRate(id string, l *bpLine) float64 {
	if id != "" {
		idx := b.sw.index("HSNCode")
		if n, ok := idx.byID[strings.ToLower(id)]; ok {
			if v, ok := idx.recs[n].Data["gst_rate"]; ok && v != nil {
				if f, err := swNumber(swText(v)); err == nil {
					return f
				}
			}
		}
	}
	if f, err := swNumber(l.GSTText); err == nil && f >= 0 {
		return f
	}
	return -1
}

func (b *bpRun) itemName(l *bpLine) (*bpItemName, error) {
	nk := swNorm(l.PType) + "|" + swNorm(l.Category)
	if inm, ok := b.names[nk]; ok {
		if inm.err != "" {
			return nil, errors.New(inm.err)
		}
		return inm, nil
	}
	inm := &bpItemName{CategoryName: l.Category}
	b.names[nk] = inm
	fail := func(err error) (*bpItemName, error) { inm.err = err.Error(); return nil, err }
	var err error
	if inm.CategoryID, err = b.simpleMaster("Category", "Category", l.Category, b.cats); err != nil {
		return fail(err)
	}
	if inm.TypeID, err = b.simpleMaster("ProductType", "Product type", l.PType, b.types); err != nil {
		return fail(err)
	}
	if _, inm.GST, err = b.hsnCode(l); err != nil {
		return fail(err)
	}
	inm.HSN = l.hsn
	if inm.CategoryID != "" && inm.TypeID != "" {
		idx := b.sw.index("ItemName")
		for _, r := range idx.recs {
			if strings.EqualFold(swText(r.Data["category"]), inm.CategoryID) && strings.EqualFold(swText(r.Data["product_type"]), inm.TypeID) {
				inm.ID, inm.Name = r.ID, swText(r.Data["name"])
				if have := bpHSNDigits(swText(r.Data["hsn_code"])); have != "" && have != l.hsn {
					b.master("Item name: "+inm.Name, "note", fmt.Sprintf("has HSN %s in Wholeops; the file says %s - Wholeops keeps %s (change it under Setup > Item Name)", have, l.hsn, have))
					inm.HSN = have
				}
				if f, err := swNumber(swText(r.Data["gst_rate"])); err == nil {
					inm.GST = f
				}
				return inm, nil
			}
		}
	}
	inm.New, inm.Name = true, engines.ItemNameForSchema(b.sw.schema, l.Category, l.PType) // 199.1: the org's order
	if !b.apply {
		if err := b.mayCreate("ItemName"); err != nil {
			return fail(err)
		}
	}
	row := b.master(fmt.Sprintf("Item name: %s + %s (HSN %s, GST %s%%)", l.Category, l.PType, l.hsn, swNumText(inm.GST)), "new", "")
	if !b.apply {
		return inm, nil
	}
	if inm.ID, err = b.saveDoc("ItemName", map[string]interface{}{"category": inm.CategoryID, "product_type": inm.TypeID,
		"hsn_code": l.hsn, "gst_rate": inm.GST, "status": "Active"}); err != nil {
		bpDone(row, err)
		return fail(fmt.Errorf("item name %s %s could not be made: %v", l.Category, l.PType, err))
	}
	bpDone(row, nil)
	delete(b.sw.docs, "ItemName")
	if n := b.sw.display("ItemName", inm.ID); n != "" && n != inm.ID {
		inm.Name = n
	}
	return inm, nil
}

// --- attributes: each column is one of the org's item attributes; a new
// value (or a new attribute) is added the way the PO's design panel adds it.

func (b *bpRun) doAttributes(pos []*bpPO, cols []bpAttrCol) {
	all, _ := engines.GetDesignMatrix(b.sw.c.tenantID)
	labelOf := map[string]string{}
	for _, c := range cols {
		labelOf[c.Key] = c.Label
	}
	byName := map[*bpItemName][]*bpLine{}
	var order []*bpItemName
	for _, l := range bpUsable(pos) {
		if l.po.failed || l.dropped || l.inm == nil {
			continue
		}
		if _, ok := byName[l.inm]; !ok {
			order = append(order, l.inm)
		}
		byName[l.inm] = append(byName[l.inm], l)
	}
	for _, inm := range order {
		info := b.attrsFor(inm, all)
		// values used per attribute, in row order
		used := map[string][]string{}
		var keys []string
		for _, l := range byName[inm] {
			for k, v := range l.Attrs {
				if _, ok := used[k]; !ok {
					keys = append(keys, k)
				}
				dup := false
				for _, u := range used[k] {
					dup = dup || strings.EqualFold(u, v)
				}
				if !dup {
					used[k] = append(used[k], v)
				}
			}
		}
		sort.Strings(keys)
		for _, k := range keys {
			a := info[k]
			label := labelOf[k]
			switch {
			case a == nil && b.newAttrs[k] != nil:
				a = b.newAttrs[k] // made for an earlier item name of this upload
				info[k] = a
			case a == nil:
				// No attribute by that name: the first value makes it.
				a = &bpAttrInfo{code: "", label: label, valueType: "Select", offered: true, exists: true}
				info[k], b.newAttrs[k] = a, a
				b.newRows[k] = b.master("Attribute: "+label, "new", "")
			case !a.offered:
				msg := fmt.Sprintf("%s is not asked in the PO for %s - turn on In the PO for %s under Configuration > Item fields (or add it to the product type's attributes)", a.label, inm.Name, a.label)
				for _, l := range byName[inm] {
					if l.Attrs[k] != "" {
						b.dropLine(l, msg)
					}
				}
				continue
			}
			if a.valueType != "Select" {
				continue // typed once per design
			}
			var add []string
			for _, v := range used[k] {
				if bpOption(a.options, v) != "" {
					continue
				}
				// Only for a row still going on: a row left out to fix
				// (another attribute's problem) adds nothing.
				live := false
				for _, l := range byName[inm] {
					live = live || (!l.dropped && strings.EqualFold(l.Attrs[k], v))
				}
				if live {
					add = append(add, v)
				}
			}
			if len(add) == 0 {
				continue
			}
			row := b.newRows[k]
			if row != nil {
				row.Key = fmt.Sprintf("Attribute: %s (%s)", a.label, strings.Join(append(append([]string{}, a.options...), add...), ", "))
			} else {
				var fresh []string
				for _, v := range add {
					if t := a.code + "|" + strings.ToLower(v); !b.told[t] {
						b.told[t] = true
						fresh = append(fresh, v)
					}
				}
				if len(fresh) > 0 {
					row = b.master(fmt.Sprintf("%s: new value %s", a.label, strings.Join(fresh, ", ")), "new", "")
				}
			}
			if !b.apply {
				if a.code == "" {
					a.code = strings.ToUpper(strings.ReplaceAll(label, " ", "_"))
				}
				a.options = append(a.options, add...)
				continue
			}
			var lastErr error
			for _, v := range add {
				m, err := b.write("POST /api/v1/procurement/design-attribute", http.MethodPost, "/api/v1/procurement/design-attribute", nil,
					map[string]string{"code": a.code, "label": a.label, "value": v, "item_name": inm.ID},
					moduleGate("procurement", handleAddDesignAttributeValue))
				if err != nil {
					lastErr = err
					for _, l := range byName[inm] {
						if strings.EqualFold(l.Attrs[k], v) {
							b.dropLine(l, fmt.Sprintf("%s %s could not be added: %v", a.label, v, err))
						}
					}
					continue
				}
				a.code = swText(m["code"])
				a.options = append(a.options, v)
			}
			if row != nil && row.Result != "failed" {
				bpDone(row, lastErr)
			}
		}
		// Each row's values by attribute code, in the attribute's own casing.
		for _, l := range byName[inm] {
			l.codes = map[string]string{}
			for k, v := range l.Attrs {
				if a := info[k]; a != nil && a.code != "" {
					if o := bpOption(a.options, v); o != "" {
						v = o
					}
					l.codes[a.code] = v
				}
			}
		}
	}
}

func bpOption(options []string, v string) string {
	for _, o := range options {
		if strings.EqualFold(o, v) {
			return o
		}
	}
	return ""
}

// attrsFor: the attributes an item name's PO asks (offered), and every
// other attribute the org has (exists, not offered), by attribute key.
func (b *bpRun) attrsFor(inm *bpItemName, all engines.DesignMatrix) map[string]*bpAttrInfo {
	if m, ok := b.attrs[inm]; ok {
		return m
	}
	m := map[string]*bpAttrInfo{}
	for _, a := range all.Attributes {
		info := &bpAttrInfo{code: a.Code, label: a.Label, valueType: a.ValueType, options: a.Options, exists: true}
		m[bpAttrKey(a.Label)] = info
		if _, dup := m[bpAttrKey(a.Code)]; !dup {
			m[bpAttrKey(a.Code)] = info
		}
	}
	offered, err := engines.GetDesignMatrixFor(b.sw.c.tenantID, inm.ID)
	if err == nil {
		for _, a := range offered.Attributes {
			for _, k := range []string{bpAttrKey(a.Label), bpAttrKey(a.Code)} {
				if info := m[k]; info != nil {
					info.offered = true
					if a.ValueType == "Select" {
						info.options = a.Options
					}
				}
			}
		}
	}
	b.attrs[inm] = m
	return m
}

// --- designs and combinations, as the PO's Add design makes them

// matchExisting: the rows of a PO this file already made are its lines, so
// no design is looked up or made again. Each row takes the unused line of
// the same item and qty - the same place first. A row that is no line of the
// PO is left out to fix under a new PO Ref (237.6), the rest go on.
func (b *bpRun) matchExisting(pos []*bpPO) {
	var all engines.DesignMatrix
	loaded := false
	type exLine struct {
		sku, shape string
		ids        map[string]bool
		qty        int
		used       bool
	}
	for _, po := range pos {
		if po.failed || po.out.Action != "existing" {
			continue
		}
		if !loaded {
			all, _ = engines.GetDesignMatrix(b.sw.c.tenantID)
			loaded = true
		}
		var lines []*exLine
		for _, e := range po.existingLines {
			shape, ids := b.itemShape(e.sku)
			lines = append(lines, &exLine{sku: e.sku, shape: shape, ids: ids, qty: e.qty})
		}
		for i, l := range append([]*bpLine{}, po.lines...) {
			shape := ""
			if !l.reorder {
				shape = b.rowShape(l, all)
			}
			match := func(x *exLine) bool {
				if x.used || x.qty != l.Qty {
					return false
				}
				if l.reorder {
					return x.ids[strings.ToLower(l.sku)]
				}
				return shape != "" && x.shape == shape
			}
			var hit *exLine
			if i < len(lines) && match(lines[i]) {
				hit = lines[i]
			} else {
				for _, x := range lines {
					if match(x) {
						hit = x
						break
					}
				}
			}
			if hit == nil {
				b.dropLine(l, fmt.Sprintf("not on %s (same item and qty) - the rows of a PO Ref already made cannot change; give this row a new PO Ref",
					firstNonEmptyStr(po.out.PONumber, po.ref)))
				continue
			}
			hit.used = true
			l.sku = hit.sku
			l.out.Action, l.out.SKU = "existing", hit.sku
			l.out.Code = bpItemDesign(b.sw.schema, hit.sku) // for photos added on a later upload
		}
	}
}

// bpShape: an item name and its attribute values, as one comparable key.
func bpShape(itemName string, vals map[string]string) string {
	var parts []string
	for _, k := range bpSortedKeys(vals) {
		if v := vals[k]; v != "" {
			parts = append(parts, strings.ToLower(k)+":"+strings.ToLower(v))
		}
	}
	return strings.ToLower(itemName) + "|" + strings.Join(parts, ";")
}

// itemShape: a PO line's item as bpShape, and the ids it is known by.
func (b *bpRun) itemShape(sku string) (string, map[string]bool) {
	id, code, fam, _, _ := bpFindItem(b.sw.schema, sku)
	ids := map[string]bool{strings.ToLower(sku): true}
	for _, s := range []string{id, code} {
		if s != "" {
			ids[strings.ToLower(s)] = true
		}
	}
	if fam == "" {
		return "", ids
	}
	famID, itemName := bpFindDesignCode(b.sw.schema, fam)
	det := b.designDetail(famID)
	if famID == "" || det == nil {
		return "", ids
	}
	vals := bpLower(det.Attributes)
	for _, c := range det.Combinations {
		if ids[strings.ToLower(c.SKU)] {
			for k, v := range bpLower(c.Options) {
				vals[k] = v
			}
		}
	}
	return bpShape(itemName, vals), ids
}

// rowShape: a Purchase row as bpShape, by the attribute codes its columns
// are; "" when a column is no attribute yet (so it is on no PO line).
func (b *bpRun) rowShape(l *bpLine, all engines.DesignMatrix) string {
	if l.inm == nil || l.inm.ID == "" {
		return ""
	}
	info := b.attrsFor(l.inm, all)
	vals := map[string]string{}
	for k, v := range l.Attrs {
		a := info[k]
		if a == nil || a.code == "" {
			return ""
		}
		vals[a.code] = v
	}
	return bpShape(l.inm.ID, vals)
}

func (b *bpRun) doDesigns(pos []*bpPO, cols []bpAttrCol) {
	var order []*bpDesign
	for _, l := range bpUsable(pos) {
		if l.po.failed || l.dropped || l.inm == nil || l.reorder || l.po.out.Action == "existing" {
			continue
		}
		// The Design No only groups this file's rows (1, 1, 1 = one design);
		// Wholeops numbers every design itself. A blank one is a design of
		// its own (the user, 2026-10-22).
		dkey := swNorm(l.Design)
		if l.Design == "" {
			dkey = "#row" + strconv.Itoa(l.Row)
		}
		key := l.po.vendorID + "|" + bpAttrKey(l.Vendor) + "|" + l.inm.CategoryName + "|" + l.inm.ID + "|" + dkey
		d := b.designs[key]
		if d == nil {
			d = &bpDesign{key: key, designNo: l.Design, vendorID: l.po.vendorID, inm: l.inm, skus: map[string]string{}}
			b.designs[key] = d
			order = append(order, d)
		}
		d.lines = append(d.lines, l)
		l.design = d
	}
	for i, d := range order {
		if i%20 == 0 {
			b.step("Designs", 2, 5)
		}
		if err := b.design(d, cols); err != nil {
			msg := err.Error()
			if d.designNo != "" {
				msg = "design " + d.designNo + ": " + msg
			}
			// Rows the design already gave a combination go on.
			for _, l := range d.lines {
				if l.sku == "" || !b.apply {
					b.dropLine(l, msg)
				}
			}
		}
	}
	// One item once per PO (237.3 can make two rows the same item).
	for _, po := range pos {
		if po.failed || po.out.Action == "existing" {
			continue
		}
		seen := map[string]*bpLine{}
		for _, l := range append([]*bpLine{}, po.lines...) {
			if l.sku == "" {
				continue
			}
			if f, ok := seen[strings.ToLower(l.sku)]; ok {
				b.dropLine(l, fmt.Sprintf("same item as row %d on PO Ref %s - add the qty to one row", f.Row, po.ref))
				continue
			}
			seen[strings.ToLower(l.sku)] = l
		}
	}
	// What each row will be, for the preview and the result.
	for _, po := range pos {
		for _, l := range po.lines {
			if l.design == nil || l.out == nil {
				continue
			}
			var vals []string
			for _, c := range cols {
				if v := l.Attrs[c.Key]; v != "" {
					vals = append(vals, v)
				}
			}
			l.out.Variant = strings.Join(vals, " / ")
			l.out.Code = l.design.id
		}
	}
}

func bpSortedKeys(m map[string]string) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

func bpKey(vals map[string]string, axes []string) string {
	parts := make([]string, 0, len(axes))
	for _, a := range axes {
		if v := vals[a]; v != "" {
			parts = append(parts, a+":"+v)
		}
	}
	return strings.Join(parts, ";")
}

func bpLower(m map[string]string) map[string]string {
	out := map[string]string{}
	for k, v := range m {
		if v != "" {
			out[strings.ToLower(k)] = strings.ToLower(v)
		}
	}
	return out
}

func (b *bpRun) design(d *bpDesign, cols []bpAttrCol) error {
	info := b.attrsFor(d.inm, engines.DesignMatrix{})
	isSelect := func(code string) bool {
		for _, a := range info {
			if a.code == code {
				return a.valueType == "Select"
			}
		}
		return true
	}
	label := func(code string) string {
		for _, a := range info {
			if a.code == code && a.label != "" {
				return a.label
			}
		}
		return code
	}
	// The design panel's rule: an attribute is on every row or on none, and
	// a typed attribute has one value for the design.
	codes := map[string]bool{}
	for _, l := range d.lines {
		for c := range l.codes {
			codes[c] = true
		}
	}
	var sorted []string
	for c := range codes {
		sorted = append(sorted, c)
	}
	sort.Strings(sorted)
	distinct := map[string]map[string]string{}
	for _, c := range sorted {
		distinct[c] = map[string]string{}
		filled := 0
		for _, l := range d.lines {
			if v := l.codes[c]; v != "" {
				filled++
				distinct[c][strings.ToLower(v)] = v
			}
		}
		if filled != len(d.lines) {
			return fmt.Errorf("%s is filled on some of its rows and empty on others - fill it on every row of the design, or on none", label(c))
		}
		if !isSelect(c) && len(distinct[c]) > 1 {
			return fmt.Errorf("%s is typed, not picked from a list, so it must be the same on every row of the design", label(c))
		}
	}

	// A reorder: the Design No is a design Wholeops already has, by its own
	// number (D00010). The client's own labels never match across files.
	if d.designNo != "" {
		if id, itemName := bpFindDesignCode(b.sw.schema, d.designNo); id != "" {
			if d.inm.ID == "" || !strings.EqualFold(itemName, d.inm.ID) {
				return fmt.Errorf("%s is a Wholeops design of %s - to reorder it use its Product Type and Category, or give a new design another label",
					d.designNo, b.sw.display("ItemName", itemName))
			}
			if det := b.designDetail(id); det != nil {
				d.exists, d.id, d.name, d.detail = true, det.Code, det.Name, det
			}
		}
	}
	if !d.exists {
		b.reuse(d, sorted, distinct)
	}
	if d.exists {
		// Axes over the existing combinations and the new rows together.
		all := []map[string]string{}
		for _, c := range d.detail.Combinations {
			merged := map[string]string{}
			for k, v := range d.detail.Attributes {
				merged[k] = v
			}
			for k, v := range c.Options {
				if v != "" {
					merged[k] = v
				}
			}
			all = append(all, merged)
		}
		for _, l := range d.lines {
			all = append(all, l.codes)
		}
		vals := map[string]map[string]bool{}
		canon := map[string]string{}
		for _, m := range all {
			for k, v := range m {
				if v == "" || !isSelect(k) {
					continue
				}
				lk := strings.ToLower(k)
				if vals[lk] == nil {
					vals[lk], canon[lk] = map[string]bool{}, k
				}
				vals[lk][strings.ToLower(v)] = true
			}
		}
		var axes []string
		for lk, vs := range vals {
			if len(vs) > 1 {
				axes = append(axes, canon[lk])
			}
		}
		sort.Strings(axes)
		for _, l := range d.lines {
			if l.po.failed || l.dropped {
				continue
			}
			key := bpKey(l.codes, axes)
			if sku, ok := d.skus[key]; ok {
				b.takeSKU(l, sku)
				continue
			}
			want := map[string]string{}
			for _, a := range axes {
				want[strings.ToLower(a)] = strings.ToLower(l.codes[a])
			}
			for _, c := range d.detail.Combinations {
				merged := bpLower(d.detail.Attributes)
				for k, v := range bpLower(c.Options) {
					merged[k] = v
				}
				match := true
				for k, v := range want {
					match = match && merged[k] == v
				}
				if match {
					d.skus[key] = c.SKU
					break
				}
			}
			if sku, ok := d.skus[key]; ok {
				b.takeSKU(l, sku)
				continue
			}
			if !b.allowNew {
				b.dropLine(l, "a new combination is needed and new designs from an upload are turned off (Settings › Procurement)")
				continue
			}
			var sku string
			if !b.apply {
				if err := b.mayCreate("Item"); err != nil {
					return err
				}
				b.newSeq++
				sku = fmt.Sprintf("#new%d", b.newSeq)
			} else {
				var err error
				if sku, err = b.newItem(d, l, key, axes); err != nil {
					return err
				}
			}
			b.madeSKU[sku] = "new combination"
			d.skus[key] = sku
			d.detail.Combinations = append(d.detail.Combinations, engines.DesignCombination{SKU: sku, Options: bpPick(l.codes, axes)})
			b.takeSKU(l, sku)
		}
		return nil
	}
	if !b.allowNew {
		return errors.New("a new design is needed and new designs from an upload are turned off (Settings › Procurement)")
	}

	// A new design: one value on every row = a design attribute, several =
	// an axis whose values make the combinations.
	var axes []string
	designAttrs := map[string]string{}
	for _, c := range sorted {
		if len(distinct[c]) > 1 {
			axes = append(axes, c)
			continue
		}
		for _, v := range distinct[c] {
			designAttrs[c] = v
		}
	}
	// Named after its item name, as a design with no typed number is on the
	// PO; its number comes from the Design series.
	d.name = d.inm.Name
	if !b.apply {
		for _, dt := range []string{"ProductFamily", "Item"} {
			if err := b.mayCreate(dt); err != nil {
				return err
			}
		}
	}
	var live []*bpLine
	for _, l := range d.lines {
		if !l.po.failed && !l.dropped {
			live = append(live, l)
		}
	}
	if len(live) == 0 {
		return nil
	}
	d.lines = live
	// In the pool, so a later group of the same item name and vendor joins
	// it instead of making the same design again (237.3).
	det := &engines.DesignDetail{Name: d.name, Attributes: designAttrs}
	register := func(id string) {
		det.Code, d.detail = id, det
		b.pools[b.poolKey(d)] = append(b.pool(d), &bpPoolDesign{id: id, attrs: bpLower(designAttrs), detail: det, label: swNorm(d.designNo)})
		if id != "" {
			b.shapes[id] = det
		}
	}
	// combo: a combination made (or, in the preview, to be made) for a row.
	combo := func(l *bpLine, key, sku string) {
		d.skus[key], b.madeSKU[sku] = sku, "new design"
		det.Combinations = append(det.Combinations, engines.DesignCombination{SKU: sku, Options: bpPick(l.codes, axes)})
	}
	if !b.apply {
		register("")
		for _, l := range d.lines {
			key := bpKey(l.codes, axes)
			if _, ok := d.skus[key]; !ok {
				b.newSeq++
				combo(l, key, fmt.Sprintf("#new%d", b.newSeq))
			}
			b.takeSKU(l, d.skus[key])
		}
		return nil
	}
	attrString := bpKey(designAttrs, sorted)
	body := map[string]interface{}{
		"name": d.name, "status": "Active", "item_name": d.inm.ID,
		"hsn_code": d.inm.HSN, "gst_rate": d.inm.GST, "mrp": d.lines[0].MRP, "purchase_rate": d.lines[0].Rate,
	}
	if d.vendorID != "" {
		body["vendor"] = d.vendorID
	}
	if attrString != "" {
		body["design_attributes"] = attrString
		body["description"] = "Design attributes: " + attrString
	}
	id, err := b.saveDoc("ProductFamily", body)
	if err != nil {
		return fmt.Errorf("could not be made: %v", err)
	}
	d.id, d.exists = id, true
	register(id)
	for _, l := range d.lines {
		key := bpKey(l.codes, axes)
		if _, ok := d.skus[key]; !ok {
			sku, err := b.newItem(d, l, key, axes)
			if err != nil {
				return err
			}
			combo(l, key, sku)
		}
		b.takeSKU(l, d.skus[key])
	}
	return nil
}

// takeSKU puts a row on a combination. A preview stand-in (#new1) is no
// Combination ID, so the row shows none.
func (b *bpRun) takeSKU(l *bpLine, sku string) {
	l.sku = sku
	l.out.Action = firstNonEmptyStr(b.madeSKU[sku], "existing")
	l.out.SKU = sku
	if strings.HasPrefix(sku, "#new") {
		l.out.SKU = ""
	}
}

func bpPick(codes map[string]string, axes []string) map[string]string {
	out := map[string]string{}
	for _, a := range axes {
		if v := codes[a]; v != "" {
			out[a] = v
		}
	}
	return out
}

// designDetail: a design and its combinations, read once per upload.
func (b *bpRun) designDetail(id string) *engines.DesignDetail {
	if det := b.shapes[id]; det != nil {
		return det
	}
	d, err := engines.GetDesign(b.sw.c.tenantID, id)
	if err != nil {
		return nil
	}
	b.shapes[id] = &d
	return &d
}

func (b *bpRun) poolKey(d *bpDesign) string {
	vk := d.vendorID
	if vk == "" && len(d.lines) > 0 {
		vk = "n:" + bpAttrKey(d.lines[0].Vendor)
	}
	ik := d.inm.ID
	if ik == "" {
		ik = "n:" + strings.ToLower(d.inm.Name)
	}
	return ik + "|" + vk
}

// pool: the designs a group of rows may join - the item name's active
// designs of the vendor (or of no vendor), oldest first, then the ones this
// upload made. Their combinations are read only when a design fits.
func (b *bpRun) pool(d *bpDesign) []*bpPoolDesign {
	key := b.poolKey(d)
	if p, ok := b.pools[key]; ok {
		return p
	}
	var out []*bpPoolDesign
	if d.inm.ID != "" {
		rows, err := db.DB.Query(fmt.Sprintf(`SELECT id, COALESCE(data->>'design_attributes', '') FROM %s.documents
			WHERE doctype = 'ProductFamily' AND deleted_at IS NULL AND COALESCE(status, 'Active') = 'Active'
			AND lower(COALESCE(data->>'item_name', '')) = lower($1) AND COALESCE(data->>'vendor', '') IN ('', $2)
			ORDER BY created_at, id`, b.sw.schema), d.inm.ID, d.vendorID)
		if err == nil {
			for rows.Next() {
				var id, attrs string
				if rows.Scan(&id, &attrs) == nil {
					out = append(out, &bpPoolDesign{id: id, attrs: bpLower(bpPairs(attrs))})
				}
			}
			rows.Close()
		}
	}
	b.pools[key] = out
	return out
}

// bpPairs reads "colour:Red;metal:Brass", as a design's attributes are kept.
func bpPairs(s string) map[string]string {
	out := map[string]string{}
	for _, part := range strings.Split(s, ";") {
		if k, v, ok := strings.Cut(part, ":"); ok && strings.TrimSpace(k) != "" && strings.TrimSpace(v) != "" {
			out[strings.TrimSpace(k)] = strings.TrimSpace(v)
		}
	}
	return out
}

// reuse (237.3, "if same do not create new"): a design of the same item
// name and vendor whose own attributes every row carries, and whose
// attributes are the rows' own, takes the rows - as combinations it has, or
// new ones of it. Of several, the one that has most of the rows already,
// then the oldest. A Design No the file gave another design keeps them
// apart (the client said they differ).
func (b *bpRun) reuse(d *bpDesign, sorted []string, distinct map[string]map[string]string) {
	one := map[string]string{} // attribute -> its one value on every row
	all := map[string]bool{}
	for _, c := range sorted {
		lc := strings.ToLower(c)
		all[lc] = true
		if len(distinct[c]) == 1 {
			for lv := range distinct[c] {
				one[lc] = lv
			}
		}
	}
	label := swNorm(d.designNo)
	var best *bpPoolDesign
	bestN := -1
	for _, p := range b.pool(d) {
		if label != "" && p.label != "" && p.label != label {
			continue
		}
		fits := true
		for k, v := range p.attrs {
			fits = fits && one[k] == v
		}
		if !fits {
			continue
		}
		if p.detail == nil {
			p.detail = b.designDetail(p.id)
			if p.detail == nil {
				continue
			}
		}
		keys := map[string]bool{}
		for k := range p.attrs {
			keys[k] = true
		}
		for _, c := range p.detail.Combinations {
			for k, v := range c.Options {
				if v != "" {
					keys[strings.ToLower(k)] = true
				}
			}
		}
		if len(keys) != len(all) {
			continue
		}
		for k := range keys {
			fits = fits && all[k]
		}
		if !fits {
			continue
		}
		n := 0
		for _, l := range d.lines {
			if bpComboOf(p.detail, l.codes) != "" {
				n++
			}
		}
		if n > bestN {
			best, bestN = p, n
		}
	}
	if best == nil {
		return
	}
	if label != "" {
		best.label = label
	}
	d.exists, d.id, d.name, d.detail = true, best.id, firstNonEmptyStr(best.detail.Name, d.inm.Name), best.detail
}

// bpComboOf: the combination of a design that is exactly these values.
func bpComboOf(det *engines.DesignDetail, codes map[string]string) string {
	want := bpLower(codes)
	for _, c := range det.Combinations {
		have := bpLower(det.Attributes)
		for k, v := range bpLower(c.Options) {
			have[k] = v
		}
		if len(have) != len(want) {
			continue
		}
		same := true
		for k, v := range want {
			same = same && have[k] == v
		}
		if same {
			return c.SKU
		}
	}
	return ""
}

// newItem makes one combination of a design, as the PO's Add design does.
func (b *bpRun) newItem(d *bpDesign, l *bpLine, key string, axes []string) (string, error) {
	// 199.1 (user, 2026-11-15: "Item name should be Category and Type"): a
	// combination is named by its item name; its values (colour, size, ...)
	// are its attributes, not part of the name.
	var vals []string
	for _, a := range bpSortedKeys(l.codes) {
		if v := l.codes[a]; v != "" {
			vals = append(vals, v)
		}
	}
	name := firstNonEmptyStr(d.inm.Name, d.name)
	body := map[string]interface{}{
		"name": name, "family": d.id, "parent_product_code": d.id, "category": d.inm.CategoryName,
		"hsn_code": d.inm.HSN, "gst_rate": d.inm.GST, "mrp": l.MRP, "standard_cost": l.Rate,
	}
	if key != "" {
		body["variant_option_values"] = key
	}
	sku, err := b.saveDoc("Item", body)
	if err != nil {
		return "", fmt.Errorf("combination %s could not be made: %v", strings.Join(vals, " / "), err)
	}
	return sku, nil
}

// bpItemDesign is the design a Combination ID belongs to.
func bpItemDesign(schema, sku string) string {
	var family string
	_ = db.DB.QueryRow(fmt.Sprintf(`SELECT COALESCE(NULLIF(data->>'family', ''), data->>'parent_product_code', '') FROM %s.documents
		WHERE doctype = 'Item' AND deleted_at IS NULL AND (id = $1 OR data->>'code' = $1) LIMIT 1`, schema), sku).Scan(&family)
	return family
}

// bpFindDesignCode finds a design by its number in Wholeops (code or id).
func bpFindDesignCode(schema, code string) (id, itemName string) {
	_ = db.DB.QueryRow(fmt.Sprintf(`SELECT id, COALESCE(data->>'item_name', '') FROM %s.documents
		WHERE doctype = 'ProductFamily' AND deleted_at IS NULL AND (lower(id) = lower($1) OR lower(data->>'code') = lower($1))
		LIMIT 1`, schema), code).Scan(&id, &itemName)
	return id, itemName
}

// --- purchase orders

func (b *bpRun) doPOs(pos []*bpPO) {
	for i, po := range pos {
		b.step(fmt.Sprintf("Purchase orders (%d of %d)", i+1, len(pos)), 3, 5)
		if po.failed {
			continue
		}
		if !b.apply {
			if po.out.Action == "new" {
				// 49.5.6b / 49.5.6e: Apply would refuse a PO for a vendor or a
				// store with no state.
				if err := engines.RequirePOVendorState(b.sw.c.tenantID, map[string]interface{}{"vendor": po.vendorID}); err != nil {
					b.failPO(po, "the vendor has no state - add its State on the vendor in Wholeops, then upload again")
				} else if err := engines.RequirePOStoreState(b.sw.c.tenantID, map[string]interface{}{"location": po.storeID}); err != nil {
					b.failPO(po, "the store has no state - add its State in Settings › Stores, then upload again")
				}
			}
			continue
		}
		if po.out.Action == "new" {
			items := make([]map[string]interface{}, 0, len(po.lines))
			for _, l := range po.lines {
				items = append(items, map[string]interface{}{"sku": l.sku, "qty": l.Qty, "rate": l.Rate, "mrp": l.MRP})
			}
			raw, _ := json.Marshal(items)
			id, err := b.saveDoc("PurchaseOrder", map[string]interface{}{
				"vendor": po.vendorID, "vendor_id": po.vendorID, "location": po.storeID, "target_warehouse": po.storeID,
				"items": string(raw), "gst_mode": "Inclusive", "total_amount": 0, "status": "Draft", "upload_ref": po.ref,
			})
			if err != nil {
				b.failPO(po, "the PO could not be saved: "+err.Error())
				continue
			}
			po.out.POID = id
		}
		po.out.PONumber, po.out.Status = bpPOStatus(b.sw.schema, po.out.POID)
		po.out.Message = ""
		if po.out.Status == "Draft" || po.out.Status == "Rejected" {
			if _, err := b.write("POST /api/v1/approval/submit", http.MethodPost, "/api/v1/approval/submit", nil,
				map[string]string{"doctype": "PurchaseOrder", "document_id": po.out.POID}, handleSubmitApproval); err != nil {
				po.out.Message = "saved as Draft, not sent for approval: " + err.Error()
			}
		}
		if _, st := bpPOStatus(b.sw.schema, po.out.POID); st == "Pending Approval" {
			if _, err := b.write("POST /api/v1/approval/decide", http.MethodPost, "/api/v1/approval/decide", nil,
				map[string]string{"doctype": "PurchaseOrder", "document_id": po.out.POID, "decision": "Approved", "comment": "Bulk purchase upload"},
				handleDecideApproval); err != nil {
				po.out.Message = "waiting for approval: " + err.Error()
			}
		}
		po.out.PONumber, po.out.Status = bpPOStatus(b.sw.schema, po.out.POID)
		switch {
		case !bpNotReceivable[po.out.Status]:
			po.out.Result = "done"
		case po.out.Status == "Pending Approval" && po.out.Message == "":
			po.out.Result, po.out.Message = "waiting", "waiting for the next approval"
		default:
			po.out.Result = "waiting"
		}
	}
}

// --- bills -> goods receipts

func (b *bpRun) doBills(bills []*bpBillIn, lines []*bpLine) {
	byBill := map[string][]*bpLine{}
	listed := map[string]bool{}
	for _, l := range lines {
		if l.Invoice != "" {
			k := bpAttrKey(l.Vendor) + "|" + swNorm(l.Invoice)
			byBill[k] = append(byBill[k], l)
		}
	}
	billed := map[string]bool{}
	for _, bi := range bills {
		billed[bpAttrKey(bi.Vendor)+"|"+swNorm(bi.Invoice)] = true
	}
	for k, rows := range byBill {
		if billed[k] {
			continue
		}
		for _, l := range rows {
			if l.out != nil && l.out.Message == "" {
				l.out.Message = fmt.Sprintf("bill %s is not on the Bills sheet - this row is ordered, not received", l.Invoice)
			}
		}
	}
	for i, bi := range bills {
		b.step(fmt.Sprintf("Goods receipts (%d of %d)", i+1, len(bills)), 4, 5)
		k := bpAttrKey(bi.Vendor) + "|" + swNorm(bi.Invoice)
		out := &bpBillOut{Row: bi.Row, Invoice: bi.Invoice, Vendor: bi.Vendor, Date: bi.Date, Total: bi.Total, Action: "new", fromRows: bi.fromRows}
		b.res.Bills = append(b.res.Bills, out)
		fail := func(msg string) { out.Action, out.Message = "error", msg }
		for _, l := range byBill[k] {
			if b.billOf[l] == nil {
				b.billOf[l] = out
			}
		}
		if bi.err != "" {
			fail(bi.err)
			continue
		}
		if listed[k] {
			fail("this bill is listed twice")
			continue
		}
		listed[k] = true
		rows := byBill[k]
		if len(rows) == 0 {
			fail("no Purchase row has this bill no for this vendor")
			continue
		}
		out.lines = rows
		for _, l := range rows {
			q := l.Qty
			if l.HasRecv {
				q = l.Recv
			}
			out.RowsSum += float64(q) * l.Rate
			if q > 0 {
				out.Lines++
				out.Qty += q
			}
		}
		out.RowsSum = math.Round(out.RowsSum*100) / 100
		if bi.fromRows {
			// The PO + GRN template has no bill total: the rows are the bill.
			bi.Total, out.Total = out.RowsSum, out.RowsSum
		}
		if !bi.fromRows && math.Abs(out.RowsSum-bi.Total) > 1 {
			fail(fmt.Sprintf("the bill total %s and its rows (%s) differ by %s - fix the total or the rows", swNumText(bi.Total), swNumText(out.RowsSum), swNumText(math.Round((bi.Total-out.RowsSum)*100)/100)))
			continue
		}
		if out.Lines == 0 {
			fail("every row of this bill has Received Qty 0 - nothing to receive")
			continue
		}
		// One goods receipt keeps its lines in one field (its own limit since
		// Stage 130.1, else the org's "Default maximum field length"); a
		// received line takes about bpGRNLineChars of it.
		if limit := engines.FieldMaxLength(b.sw.c.tenantID, "GRN", "received_items"); limit > 0 && out.Lines*bpGRNLineChars > limit {
			fail(fmt.Sprintf("this bill has %d lines; one goods receipt holds about %d (%d characters) - split the bill into two goods receipts",
				out.Lines, limit/bpGRNLineChars, limit))
			continue
		}
		var blocked []string
		store := ""
		for _, l := range rows {
			switch {
			case l.dropped:
				// A bill is received whole or not at all (237.2): held until
				// its row is fixed and the file uploaded again.
				blocked = append(blocked, fmt.Sprintf("row %d is to fix", l.Row))
			case l.po == nil || l.po.failed:
				blocked = append(blocked, fmt.Sprintf("row %d's PO Ref %s has a problem", l.Row, l.Ref))
			case store != "" && l.po.storeID != store:
				blocked = append(blocked, "its rows are for two stores - one bill, one store")
			}
			if l.po != nil && store == "" {
				store = l.po.storeID
			}
		}
		if len(blocked) > 0 {
			fail(strings.Join(bpUniq(blocked), "; "))
			continue
		}
		out.vendorID = rows[0].po.vendorID
		if out.vendorID != "" {
			if id := bpFindGRN(b.sw.schema, out.vendorID, bi.Invoice); id != "" {
				out.Action, out.GRNID = "existing", id
				if v, err := engines.GetGRNStepView(b.sw.c.tenantID, id); err == nil {
					out.GRNCode, out.Step = v.Code, v.Step
				}
			}
		}
		if !b.apply {
			if out.Action == "new" {
				if err := b.mayCreate("GRN"); err != nil {
					fail(err.Error())
					continue
				}
			}
			if out.Action == "existing" && out.Step == engines.GRNStepPosted {
				out.Message = "already received and posted"
			} else {
				out.Message = "received once its PO is approved"
			}
			continue
		}
		var notReady []string
		for _, l := range rows {
			if bpNotReceivable[l.po.out.Status] {
				notReady = append(notReady, fmt.Sprintf("%s is %s", firstNonEmptyStr(l.po.out.PONumber, l.Ref), firstNonEmptyStr(l.po.out.Status, "not saved")))
			}
		}
		if len(notReady) > 0 {
			out.Action, out.Result = "waiting", "waiting"
			out.Message = strings.Join(bpUniq(notReady), "; ") + " - approve it, then upload the same file again"
			continue
		}
		b.receive(out, bi, store)
	}
}

func firstNonEmptyStr(vals ...string) string {
	for _, v := range vals {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}

func bpUniq(in []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, s := range in {
		if !seen[s] {
			seen[s] = true
			out = append(out, s)
		}
	}
	return out
}

func bpFindGRN(schema, vendorID, invoice string) string {
	var id string
	_ = db.DB.QueryRow(fmt.Sprintf(`SELECT id FROM %s.documents WHERE doctype = 'GRN' AND deleted_at IS NULL
		  AND data->>'vendor' = $1 AND lower(data->>'vendor_invoice_no') = lower($2)
		  AND COALESCE(NULLIF(status, ''), data->>'status', '') <> 'Cancelled'
		ORDER BY created_at DESC LIMIT 1`, schema), vendorID, invoice).Scan(&id)
	return id
}

func (b *bpRun) grnStep(out *bpBillOut, step string, body interface{}, h http.HandlerFunc, pattern string, pv map[string]string) (*engines.GRNStepView, error) {
	target := "/api/v1/grn-steps/" + url.PathEscape(out.GRNID) + "/" + step
	if c := pv["child"]; c != "" {
		target += "/" + url.PathEscape(c)
	}
	if pv == nil {
		pv = map[string]string{}
	}
	pv["id"] = out.GRNID
	status, raw := b.sw.c.call(pattern, http.MethodPost, target, pv, body, moduleGate("procurement", h))
	if status < 200 || status >= 300 {
		return nil, errors.New(swMessage(status, raw))
	}
	var v engines.GRNStepView
	if err := json.Unmarshal(raw, &v); err != nil {
		return nil, err
	}
	out.Step, out.GRNCode = v.Step, firstNonEmptyStr(v.Code, out.GRNCode)
	return &v, nil
}

// receive takes one bill through the goods-receipt steps: Receive, Verify,
// Price check, then MRP and post for every child. A bill met again (the
// file uploaded twice) carries on from the step it stopped at.
func (b *bpRun) receive(out *bpBillOut, bi *bpBillIn, store string) {
	failed := func(where string, err error) {
		out.Result, out.Message = "failed", where+": "+err.Error()
	}
	type grnLine struct {
		l   *bpLine
		qty int
	}
	var gl []grnLine
	for _, l := range out.lines {
		q := l.Qty
		if l.HasRecv {
			q = l.Recv
		}
		if q > 0 {
			gl = append(gl, grnLine{l, q})
		}
	}
	mrp := map[string]float64{}
	for i, g := range gl {
		mrp[strconv.Itoa(i+1)] = g.l.MRP
		out.SKUs = append(out.SKUs, g.l.sku)
	}
	if out.Action == "new" {
		var poIDs []string
		seen := map[string]bool{}
		items := make([]map[string]interface{}, 0, len(gl))
		for _, g := range gl {
			if !seen[g.l.po.out.POID] {
				seen[g.l.po.out.POID] = true
				poIDs = append(poIDs, g.l.po.out.POID)
			}
			// The receipt fills in the rest (accepted qty, PO rate, GST...).
			items = append(items, map[string]interface{}{"po_id": g.l.po.out.POID, "sku": g.l.sku, "ordered_qty": g.l.Qty, "qty": g.qty, "mrp": g.l.MRP})
		}
		raw, _ := json.Marshal(items)
		id, err := b.saveDoc("GRN", map[string]interface{}{
			"flow": "steps", "po_id": poIDs[0], "po_ids": strings.Join(poIDs, ","), "location": store,
			"vendor_invoice_no": bi.Invoice, "vendor_invoice_date": bi.Date, "vendor_invoice_amount": bi.Total, "received_items": string(raw),
		})
		if err != nil {
			failed("Receive", err)
			return
		}
		out.GRNID = id
		out.Step = engines.GRNStepReceived
	} else if out.Step == engines.GRNStepPosted {
		out.Result, out.Message = "done", "already received and posted"
		return
	}
	v, err := engines.GetGRNStepView(b.sw.c.tenantID, out.GRNID)
	if err != nil {
		failed("Receive", err)
		return
	}
	out.GRNCode, out.Step = v.Code, v.Step
	// A bill corrected in the file after an earlier upload stopped it.
	if out.Action == "existing" && v.Step != engines.GRNStepPosted && (math.Abs(v.InvoiceTotal-bi.Total) >= 0.005 || v.InvoiceDate != bi.Date) {
		if v, err = b.grnStep(out, "receive", map[string]interface{}{"vendor_invoice_date": bi.Date, "vendor_invoice_amount": bi.Total,
			"reason": "Bulk purchase upload: bill corrected in the file"}, handleGRNStepsReceive, "POST /api/v1/grn-steps/{id}/receive", nil); err != nil {
			failed("Receive", err)
			return
		}
	}
	if v.Step == engines.GRNStepReceived {
		if v, err = b.grnStep(out, "verify", map[string]interface{}{"reason": "Bulk purchase upload"}, handleGRNStepsVerify, "POST /api/v1/grn-steps/{id}/verify", nil); err != nil {
			failed("Verify", err)
			return
		}
	}
	if v.Step == engines.GRNStepVerified {
		if v, err = b.grnStep(out, "price", map[string]interface{}{"reason": "Bulk purchase upload"}, handleGRNStepsPrice, "POST /api/v1/grn-steps/{id}/price", nil); err != nil {
			failed("Price check", err)
			return
		}
	}
	for _, c := range v.Children {
		if c.Status == engines.GRNStepPosted {
			continue
		}
		if v, err = b.grnStep(out, "post", map[string]interface{}{"mrp": mrp, "reason": "Bulk purchase upload"}, handleGRNStepsPostChild,
			"POST /api/v1/grn-steps/{id}/post/{child}", map[string]string{"child": c.Key}); err != nil {
			failed("Post "+firstNonEmptyStr(c.Name, c.Key), err)
			return
		}
	}
	if v.ExcessPending {
		out.Result, out.Message = "waiting", "more than ordered was received: the extra waits for approval on the goods receipt"
		return
	}
	out.Result = "done"
}

// --- totals

func (b *bpRun) totals() {
	t := &b.res.Totals
	for _, v := range b.res.Vendors {
		switch {
		case v.Action == "error":
			t.Errors++
		case v.Action == "new" && v.Result != "failed":
			t.NewVendors++
		}
		if v.Result == "done" {
			t.Done++
		} else if v.Result == "failed" {
			t.Failed++
		}
	}
	for _, m := range b.res.Masters {
		if m.Action == "new" && m.Result != "failed" {
			t.NewMasters++
		}
		if m.Result == "failed" {
			t.Failed++
		}
	}
	for _, po := range b.res.POs {
		switch po.Action {
		case "error":
			t.Errors++
		case "new":
			t.NewPOs++
		case "existing":
			t.ExistingPOs++
		}
		switch po.Result {
		case "done":
			t.Done++
		case "failed":
			t.Failed++
		case "waiting":
			t.Waiting++
		}
		t.Lines += len(po.Lines)
		t.Qty += po.Qty
		t.Amount = math.Round((t.Amount+po.Amount)*100) / 100
		if po.Action != "error" {
			for _, l := range po.Lines {
				if l.Action == "error" {
					t.Errors++ // a row left out of a PO that goes on
				}
			}
		}
	}
	t.RowsToFix = len(b.res.Fix)
	for _, bo := range b.res.Bills {
		t.Bills++
		if bo.Action == "error" {
			t.Errors++
		}
		switch bo.Result {
		case "done":
			t.Done++
		case "failed":
			t.Failed++
		case "waiting":
			t.Waiting++
		}
	}
}

// ---------------------------------------------------------------- handlers

// bpAllowed: the upload makes purchase orders, so it needs that right; each
// replayed call then checks its own (vendors, items, goods receipts...).
func bpAllowed(w http.ResponseWriter, r *http.Request, action string) bool {
	return requireDocPermission(w, r, "PurchaseOrder", action, "You do not have permission to work on purchase orders")
}

func bpPrepare(w http.ResponseWriter, r *http.Request, apply bool) (*bpRun, []byte, string, func() *bpResult, bool) {
	if !bpAllowed(w, r, "create") {
		return nil, nil, "", nil, false
	}
	file, name, ok := swReadUpload(w, r)
	if !ok {
		return nil, nil, "", nil, false
	}
	vendors, lines, bills, attrs, err := bpRead(file, engines.ClientDateOrder(r.Header.Get("Resolved-Tenant-ID")))
	if err != nil {
		writeAPIErrorGeneric(w, r, http.StatusUnprocessableEntity, err.Error())
		return nil, nil, "", nil, false
	}
	if len(lines) == 0 {
		writeAPIErrorGeneric(w, r, http.StatusUnprocessableEntity, "The Purchase sheet has no rows to upload.")
		return nil, nil, "", nil, false
	}
	run, err := newBPRun(newSWCaller(r), apply)
	if err != nil {
		writeAPIErrorGeneric(w, r, http.StatusInternalServerError, err.Error())
		return nil, nil, "", nil, false
	}
	return run, file, name, func() *bpResult { return run.process(vendors, lines, bills, attrs) }, true
}

// handleBulkPurchasePreview reads an uploaded file and returns what each row
// would do. Nothing is changed.
func handleBulkPurchasePreview(w http.ResponseWriter, r *http.Request) {
	_, _, name, process, ok := bpPrepare(w, r, false)
	if !ok {
		return
	}
	res := process()
	_ = json.NewEncoder(w).Encode(map[string]interface{}{"file_name": name, "result": res})
}

// handleBulkPurchaseApply starts applying an uploaded file and returns the
// run's id at once; the screen follows it on the run endpoint.
func handleBulkPurchaseApply(w http.ResponseWriter, r *http.Request) {
	run, file, name, process, ok := bpPrepare(w, r, true)
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
	runID, err := engines.CreateWorkbookRun(c.tenantID, engines.BulkPurchaseDoctype, c.username, name, file)
	if err != nil {
		lock.Unlock()
		writeAPIErrorGeneric(w, r, http.StatusInternalServerError, err.Error())
		return
	}
	run.progress = func(step string, done, total int) {
		_ = engines.UpdateWorkbookRun(c.tenantID, engines.BulkPurchaseDoctype, runID, map[string]interface{}{
			"progress_done": done, "progress_total": total, "progress_sheet": step,
		})
	}
	go func() {
		defer lock.Unlock()
		var res *bpResult
		func() {
			defer func() {
				if p := recover(); p != nil {
					_ = engines.UpdateWorkbookRun(c.tenantID, engines.BulkPurchaseDoctype, runID, map[string]interface{}{"status": "Failed", "message": fmt.Sprint("internal error: ", p)})
					res = nil
				}
			}()
			res = process()
		}()
		if res == nil {
			return
		}
		t := res.Totals
		errorCSV := bpErrorCSV(res)
		status := "Completed"
		if t.Failed > 0 || t.Errors > 0 || t.Waiting > 0 || t.RowsToFix > 0 {
			status = "Completed with errors"
		}
		_ = engines.UpdateWorkbookRun(c.tenantID, engines.BulkPurchaseDoctype, runID, map[string]interface{}{
			"status": status, "result": res, "totals": t,
			"total_rows": t.Lines + t.Bills, "success_rows": t.Done, "failed_rows": t.Failed + t.Errors,
			"error_csv": errorCSV, "finished_at": time.Now().UTC().Format(time.RFC3339),
		})
		engines.LogAuditEvent(c.tenantID, c.username, "BULK_PURCHASE", "APPLIED",
			fmt.Sprintf("%s: %d new POs, %d bills, %d done, %d failed, %d waiting, %d with errors (run %s)", name, t.NewPOs, t.Bills, t.Done, t.Failed, t.Waiting, t.Errors, runID))
	}()
	_ = json.NewEncoder(w).Encode(map[string]string{"run_id": runID, "status": "Running"})
}

func handleBulkPurchaseRuns(w http.ResponseWriter, r *http.Request) {
	if !bpAllowed(w, r, "read") {
		return
	}
	runs, err := engines.ListWorkbookRuns(r.Header.Get("Resolved-Tenant-ID"), engines.BulkPurchaseDoctype, 50)
	if err != nil {
		writeAPIErrorGeneric(w, r, http.StatusInternalServerError, err.Error())
		return
	}
	for _, run := range runs {
		delete(run, "result")
		delete(run, "error_csv")
	}
	_ = json.NewEncoder(w).Encode(runs)
}

func handleBulkPurchaseRun(w http.ResponseWriter, r *http.Request) {
	if !bpAllowed(w, r, "read") {
		return
	}
	run, err := engines.GetWorkbookRun(r.Header.Get("Resolved-Tenant-ID"), engines.BulkPurchaseDoctype, r.PathValue("id"))
	if err != nil {
		writeAPIErrorGeneric(w, r, http.StatusNotFound, err.Error())
		return
	}
	_ = json.NewEncoder(w).Encode(run)
}

func handleBulkPurchaseRunFile(w http.ResponseWriter, r *http.Request) {
	if !bpAllowed(w, r, "read") {
		return
	}
	name, file, err := engines.GetWorkbookRunFile(r.Header.Get("Resolved-Tenant-ID"), engines.BulkPurchaseDoctype, r.PathValue("id"))
	if err != nil {
		writeAPIErrorGeneric(w, r, http.StatusNotFound, err.Error())
		return
	}
	swWriteXLSX(w, name, file)
}

// bpErrorCSV is the failed-rows file of a run. The keys come from the
// uploaded workbook, so every cell is made safe to open in a spreadsheet.
func bpErrorCSV(res *bpResult) string {
	var buf bytes.Buffer
	cw := engines.NewSafeCSVWriter(&buf)
	_ = cw.Write([]string{"sheet", "row_number", "key", "message"})
	for _, v := range res.Vendors {
		if v.Action == "error" || v.Result == "failed" {
			_ = cw.Write([]string{"Vendors", strconv.Itoa(v.Row), v.Key, v.Message})
		}
	}
	for _, po := range res.POs {
		bad := po.Action == "error" || po.Result == "failed"
		if bad {
			_ = cw.Write([]string{"Purchase", "", po.Ref, po.Message})
		}
		for _, l := range po.Lines {
			if l.Message != "" && (bad || l.Action == "error") {
				_ = cw.Write([]string{"Purchase", strconv.Itoa(l.Row), l.Design, l.Message})
			}
		}
	}
	for _, bo := range res.Bills {
		if bo.Action == "error" || bo.Result == "failed" || bo.Result == "waiting" {
			_ = cw.Write([]string{"Bills", strconv.Itoa(bo.Row), bo.Invoice, bo.Message})
		}
	}
	cw.Flush()
	return buf.String()
}
