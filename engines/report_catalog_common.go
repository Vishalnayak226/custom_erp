package engines

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"
)

// The catalog columns most reports share (Stage 75.3), worked out from the
// fields a source names - so a report only adds what is its own.

func catStr(v interface{}) string {
	switch t := v.(type) {
	case nil:
		return ""
	case string:
		return strings.TrimSpace(t)
	case float64:
		return strconv.FormatFloat(t, 'f', -1, 64)
	case bool:
		if t {
			return "Yes"
		}
		return "No"
	case json.Number:
		return t.String()
	}
	return strings.TrimSpace(fmt.Sprint(v))
}

// catDay is a stored date or timestamp as YYYY-MM-DD.
func catDay(v interface{}) string {
	switch t := v.(type) {
	case time.Time:
		if t.IsZero() {
			return ""
		}
		return t.Format("2006-01-02")
	}
	s := catStr(v)
	if len(s) >= 10 && s[4] == '-' {
		return s[:10]
	}
	return s
}

func catStamp(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.Format("2006-01-02 15:04")
}

// --- row accessors ---------------------------------------------------------

func (r *catRow) hv(f string) interface{} {
	switch f {
	case "":
		return nil
	case "id":
		return r.ID
	case "status":
		return r.Status
	case "created_at":
		return catStamp(r.Created)
	case "created_by":
		return r.CreatedBy
	}
	return r.H[f]
}
func (r *catRow) hs(f string) string  { return catStr(r.hv(f)) }
func (r *catRow) hn(f string) float64 { return catNum(r.hv(f)) }

// lv is a line field, or the document's own on a one-row-per-document source.
func (r *catRow) lv(f string) interface{} {
	if f == "" {
		return nil
	}
	if r.L == nil {
		return r.hv(f)
	}
	return r.L[f]
}
func (r *catRow) ls(f string) string  { return catStr(r.lv(f)) }
func (r *catRow) ln(f string) float64 { return catNum(r.lv(f)) }

func (r *catRow) master(doctype, id string) map[string]interface{} {
	if r.lk == nil {
		return nil
	}
	return r.lk.get(doctype, id)
}

// nameOf is a master's name for an id, or the id when it is not a record.
func (r *catRow) nameOf(doctype, id string) string {
	if m := r.master(doctype, id); m != nil {
		if n := catStr(m["name"]); n != "" {
			return n
		}
	}
	return id
}

func (r *catRow) storeID() string {
	if r.src == nil || r.src.StoreField == "-" {
		return ""
	}
	if r.src.StoreOf != nil {
		return r.src.StoreOf(r)
	}
	return r.hs(r.src.storeField())
}
func (r *catRow) store() map[string]interface{} { return r.master("Location", r.storeID()) }
func (r *catRow) legal() map[string]interface{} {
	return r.master("LegalEntity", catStr(r.store()["legal_entity"]))
}
func (r *catRow) sku() string {
	if r.src == nil {
		return ""
	}
	return r.ls(r.src.SKU)
}
func (r *catRow) item() map[string]interface{} { return r.master("Item", r.sku()) }
func (r *catRow) family() map[string]interface{} {
	return r.master("ProductFamily", catStr(r.item()["family"]))
}
func (r *catRow) customer() map[string]interface{} {
	if r.src == nil {
		return nil
	}
	return r.master("Customer", r.hs(r.src.Customer))
}
func (r *catRow) vendor() map[string]interface{} {
	if r.src == nil {
		return nil
	}
	return r.master("Vendor", r.hs(r.src.Vendor))
}

// approval is the document's maker-checker trail (approval_log): who
// approved it and when, and the last action taken - loaded once per doctype.
func (r *catRow) approval() [3]string {
	if r.src == nil || r.src.Doctype == "" || r.lk == nil {
		return [3]string{}
	}
	key := "approvals:" + r.src.Doctype
	m, ok := r.lk.extra[key].(map[string][3]string)
	if !ok {
		m = map[string][3]string{}
		rows, err := dbQuery(fmt.Sprintf(`SELECT document_id, action, COALESCE(actor_user_id, ''), to_char(created_at, 'YYYY-MM-DD HH24:MI')
			FROM %s.approval_log WHERE doctype = $1 ORDER BY created_at`, r.lk.schema), r.src.Doctype)
		if err == nil {
			for rows.Next() {
				var id, action, actor, at string
				if rows.Scan(&id, &action, &actor, &at) == nil {
					v := m[id]
					if action == "Approved" {
						v[0], v[1] = actor, at
					}
					v[2] = action
					m[id] = v
				}
			}
			rows.Close()
		}
		r.lk.extra[key] = m
	}
	return m[r.ID]
}

// date is the row's business date.
func (r *catRow) date() string {
	if r.src != nil && r.src.DateField != "" {
		if d := catDay(r.hv(r.src.DateField)); d != "" {
			return d
		}
	}
	return catDay(r.Created)
}

// number is the document's own number, else its id.
func (r *catRow) number() string {
	if r.src != nil && r.src.Number != "" {
		if n := r.hs(r.src.Number); n != "" {
			return n
		}
	}
	return firstNonEmpty(r.hs("code"), r.ID)
}

// variant is one of the item's variant values (Colour, Size, Polish...).
func (r *catRow) variant(names ...string) string {
	vals, _ := r.item()["variant_option_values"].(map[string]interface{})
	for k, v := range vals {
		for _, n := range names {
			if strings.EqualFold(strings.TrimSpace(k), n) {
				return catStr(v)
			}
		}
	}
	return ""
}

// --- column constructors for wiring ----------------------------------------

func catH(f string) catFn  { return func(r *catRow) interface{} { return r.hv(f) } }
func catHN(f string) catFn { return func(r *catRow) interface{} { return r.hn(f) } }
func catHD(f string) catFn { return func(r *catRow) interface{} { return catDay(r.hv(f)) } }
func catL(f string) catFn  { return func(r *catRow) interface{} { return r.lv(f) } }
func catLN(f string) catFn { return func(r *catRow) interface{} { return r.ln(f) } }
func catConst(v interface{}) catFn {
	return func(*catRow) interface{} { return v }
}

// catMasterOf shows a master's field for the id a document field holds.
func catMasterOf(doctype, field, prop string) catFn {
	return func(r *catRow) interface{} {
		id := r.hs(field)
		if m := r.master(doctype, id); m != nil {
			if v := catStr(m[prop]); v != "" {
				return v
			}
		}
		if prop == "name" || prop == "code" {
			return id
		}
		return nil
	}
}

// catStoreOf / catStoreCodeOf: a store's name / code for the id a field holds.
func catStoreOf(field string) catFn     { return catMasterOf("Location", field, "name") }
func catStoreCodeOf(field string) catFn { return catMasterOf("Location", field, "code") }

// catSumLines / catCountLines read the document's lines.
func catSumLines(lines, field string) catFn {
	return func(r *catRow) interface{} {
		total := 0.0
		for _, l := range catLineList(r.H[lines]) {
			total += catNum(l[field])
		}
		return total
	}
}
func catCountLines(lines string) catFn {
	return func(r *catRow) interface{} { return float64(len(catLineList(r.H[lines]))) }
}

// --- the shared columns ----------------------------------------------------

func catItemProp(prop string) catFn {
	return func(r *catRow) interface{} { return r.item()[prop] }
}

var catStoreCode catFn = func(r *catRow) interface{} {
	id := r.storeID()
	if m := r.store(); m != nil {
		return firstNonEmpty(catStr(m["code"]), id)
	}
	return id
}
var catStoreName catFn = func(r *catRow) interface{} { return r.nameOf("Location", r.storeID()) }

var catCommon = map[string]catFn{
	// Store and company
	"Store Code": catStoreCode, "Location Code": catStoreCode, "Outlet ID": catStoreCode,
	"Store Name": catStoreName, "Location Name": catStoreName, "Outlet Name": catStoreName, "Warehouse": catStoreName,
	"Legal Entity": func(r *catRow) interface{} { return r.legal()["name"] },
	"GSTIN": func(r *catRow) interface{} {
		return firstNonEmpty(catStr(r.store()["gstin"]), catStr(r.legal()["gstin"]))
	},
	"From Store": func(r *catRow) interface{} {
		if r.src.FromStore == "" {
			return nil
		}
		return r.nameOf("Location", r.hs(r.src.FromStore))
	},
	"To Store": func(r *catRow) interface{} {
		if r.src.ToStore == "" {
			return nil
		}
		return r.nameOf("Location", r.hs(r.src.ToStore))
	},
	// The document
	"Document Date": func(r *catRow) interface{} { return r.date() },
	"Posting Date":  func(r *catRow) interface{} { return r.date() },
	"Document No":   func(r *catRow) interface{} { return r.number() },
	"Document Type": func(r *catRow) interface{} { return firstNonEmpty(r.src.Label, r.src.Doctype) },
	"Status":        func(r *catRow) interface{} { return r.Status },
	"Created By":    func(r *catRow) interface{} { return r.CreatedBy },
	"Created At":    func(r *catRow) interface{} { return catStamp(r.Created) },
	"User Name":     func(r *catRow) interface{} { return r.CreatedBy },
	"User ID":       func(r *catRow) interface{} { return r.CreatedBy },
	"Approved By": func(r *catRow) interface{} {
		if v := r.hs("approved_by"); v != "" {
			return v
		}
		return r.approval()[0]
	},
	"Approved At": func(r *catRow) interface{} {
		if v := r.hs("approved_at"); v != "" {
			return v
		}
		return r.approval()[1]
	},
	"Approval Status": func(r *catRow) interface{} { return firstNonEmpty(r.approval()[2], r.Status) },
	"Remarks": func(r *catRow) interface{} {
		return firstNonEmpty(r.hs(r.src.Remarks), r.hs("remarks"), r.hs("notes"))
	},
	"Reason": func(r *catRow) interface{} {
		if r.src.Reason != "" {
			return r.ls(r.src.Reason)
		}
		return r.hs("reason")
	},
	"Reason Code": func(r *catRow) interface{} { return firstNonEmpty(r.hs("reason_code"), r.ls("reason_code")) },
	"From Date":   func(r *catRow) interface{} { return r.p["from"] },
	"To Date":     func(r *catRow) interface{} { return r.p["to"] },
	"As Of Date": func(r *catRow) interface{} {
		return firstNonEmpty(r.p["to"], BusinessToday())
	},
	// The item
	"SKU":            func(r *catRow) interface{} { return r.sku() },
	"Combination ID": func(r *catRow) interface{} { return r.sku() },
	"Item Code": func(r *catRow) interface{} {
		if r.sku() == "" {
			return nil
		}
		return firstNonEmpty(catStr(r.item()["code"]), r.sku())
	},
	"Item Name": func(r *catRow) interface{} {
		return firstNonEmpty(catStr(r.item()["name"]), r.ls("item_name"), r.ls("description"))
	},
	"Barcode":  func(r *catRow) interface{} { return firstNonEmpty(r.ls("barcode"), catStr(r.item()["barcode"])) },
	"EAN Code": catItemProp("gtin"),
	"Design No": func(r *catRow) interface{} {
		if f := r.family(); f != nil {
			return firstNonEmpty(catStr(f["design_number"]), catStr(f["code"]))
		}
		return r.item()["family"]
	},
	"Product Family": func(r *catRow) interface{} { return r.nameOf("ProductFamily", catStr(r.item()["family"])) },
	"Category Name":  func(r *catRow) interface{} { return r.nameOf("Category", catStr(r.item()["category"])) },
	"Brand":          func(r *catRow) interface{} { return r.nameOf("Brand", catStr(r.item()["brand"])) },
	"Item Type":      catItemProp("item_type"),
	"HSN Code": func(r *catRow) interface{} {
		return firstNonEmpty(r.ls("hsn_code"), catStr(r.item()["hsn_code"]))
	},
	"GST %": func(r *catRow) interface{} {
		if v := r.ln("gst_rate"); v != 0 {
			return v
		}
		return r.item()["gst_rate"]
	},
	"MRP": func(r *catRow) interface{} {
		if r.src.MRP != "" {
			if v := r.ln(r.src.MRP); v != 0 {
				return v
			}
		}
		return r.item()["mrp"]
	},
	"Purchase Price": func(r *catRow) interface{} {
		if r.src.Rate != "" {
			if v := r.ln(r.src.Rate); v != 0 {
				return v
			}
		}
		return r.item()["standard_cost"]
	},
	"Style": func(r *catRow) interface{} { return r.variant("Style") },
	"Color": func(r *catRow) interface{} { return r.variant("Color", "Colour") },
	"Size":  func(r *catRow) interface{} { return r.variant("Size") },
	"Polish": func(r *catRow) interface{} {
		return r.variant("Polish", "Finish", "Plating")
	},
	"UOM": catItemProp("uom"),
	"Qty": func(r *catRow) interface{} {
		if r.src.Qty == "" {
			return nil
		}
		return r.ln(r.src.Qty)
	},
	// The party
	"Customer ID": func(r *catRow) interface{} { return r.hs(r.src.Customer) },
	"Customer Name": func(r *catRow) interface{} {
		return firstNonEmpty(catStr(r.customer()["name"]), r.hs(r.src.CustomerName))
	},
	"Customer Mobile": func(r *catRow) interface{} {
		return firstNonEmpty(catStr(r.customer()["phone"]), r.hs(r.src.CustomerPhone))
	},
	"Customer GSTIN": func(r *catRow) interface{} { return r.customer()["gstin"] },
	"Vendor Code": func(r *catRow) interface{} {
		if v := r.vendor(); v != nil {
			return firstNonEmpty(catStr(v["code"]), r.hs(r.src.Vendor))
		}
		return r.hs(r.src.Vendor)
	},
	"Vendor Name":  func(r *catRow) interface{} { return r.nameOf("Vendor", r.hs(r.src.Vendor)) },
	"Vendor GSTIN": func(r *catRow) interface{} { return r.vendor()["gstin"] },
	// Money on the document
	"Net Amount": func(r *catRow) interface{} {
		if r.src.LineAmount != "" && r.L != nil {
			return r.ln(r.src.LineAmount)
		}
		if r.src.Amount == "" {
			return nil
		}
		return r.hn(r.src.Amount)
	},
	"Taxable Amount": func(r *catRow) interface{} {
		if r.src.Taxable == "" {
			return nil
		}
		return r.hn(r.src.Taxable)
	},
	"GST Amount": func(r *catRow) interface{} {
		if r.src.Tax == "" {
			return nil
		}
		return r.hn(r.src.Tax)
	},
}

// catSince is how long ago a stored timestamp was. Documents keep their
// times as the database session's wall clock (timestamp without time zone),
// so the comparison is against the database's own LOCALTIMESTAMP, read once
// per run - never the Go process clock, which may sit in another zone.
func (r *catRow) since(t time.Time) time.Duration {
	if t.IsZero() || r.lk == nil {
		return 0
	}
	now, ok := r.lk.extra["dbnow"].(time.Time)
	if !ok {
		now = time.Now()
		if rows, err := dbQuery(`SELECT LOCALTIMESTAMP`); err == nil {
			if rows.Next() {
				_ = rows.Scan(&now)
			}
			rows.Close()
		}
		r.lk.extra["dbnow"] = now
	}
	return now.Sub(t)
}

// catNotHeld marks a catalog column the ERP does not hold: the column stays
// in the report, blank, and report-catalog-coverage.md lists it.
var catNotHeld catFn = func(*catRow) interface{} { return nil }

// catPost marks a column a summary works out after folding (Post / PostAll:
// a ratio, a rank, a running balance) - filled, not "not held".
var catPost catFn = func(*catRow) interface{} { return nil }
