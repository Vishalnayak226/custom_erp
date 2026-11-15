package engines

import (
	"bytes"
	"custom_erp/db"
	"database/sql"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"
)

// Ask 170.35: GSTR-1 amendments. Marking a month filed (MarkGSTR1Filed) keeps
// what its return told the portal, document by document, in gstr1_filed_rows.
// A later month's file reads each filed document of the amendment window again
// when it, or its customer, changed after filing. One that now reads
// differently is reported in the amendment table of the table it was filed in
// (b2ba, b2cla, expa, cdnra, cdnura); a change to a small B2C sale restates
// that month's B2CS row in b2csa. A change the portal has no amendment for (a
// cancelled document, a move between tables, the nil table) is a warning.
// Marking the later month records the amendment, so it is reported once.
// Months marked before this build kept no rows: their documents are not compared.

// gstr1Row is one document as a filed return reported it.
type gstr1Row struct {
	Sec   string         `json:"sec"`
	Note  bool           `json:"note,omitempty"`
	Num   string         `json:"num"`
	Date  string         `json:"date"`
	Val   int64          `json:"val"`
	Pos   string         `json:"pos"`
	Ctin  string         `json:"ctin,omitempty"`
	Exp   string         `json:"exp,omitempty"`
	Inter bool           `json:"inter,omitempty"`
	Items []gstr1RowItem `json:"items"`
}

type gstr1RowItem struct {
	Rate  float64 `json:"rt"`
	Nil   string  `json:"nil,omitempty"`
	Txval int64   `json:"txval"`
	I     int64   `json:"i"`
	C     int64   `json:"c"`
	S     int64   `json:"s"`
}

// listed is the table the document is reported in: its section, or "nil"
// when every line is nil / exempt.
func (d gstr1Doc) listed() string {
	if len(d.taxed()) == 0 {
		return "nil"
	}
	return d.section()
}

// row is the document as filed: its lines summed by rate and treatment.
func (d gstr1Doc) row() gstr1Row {
	r := gstr1Row{Sec: d.listed(), Note: d.note, Num: d.num, Date: d.date, Val: d.val, Pos: d.pos, Ctin: d.ctin, Exp: d.export, Inter: d.interstate, Items: []gstr1RowItem{}}
	by := map[string]int{}
	for _, it := range d.items {
		k := fmt.Sprintf("%s|%g", it.nilKind, it.rate)
		n, ok := by[k]
		if !ok {
			n = len(r.Items)
			by[k] = n
			r.Items = append(r.Items, gstr1RowItem{Rate: it.rate, Nil: it.nilKind})
		}
		r.Items[n].Txval += it.txval
		r.Items[n].I += it.i
		r.Items[n].C += it.c
		r.Items[n].S += it.s
	}
	sort.Slice(r.Items, func(a, b int) bool {
		if r.Items[a].Nil != r.Items[b].Nil {
			return r.Items[a].Nil < r.Items[b].Nil
		}
		return r.Items[a].Rate < r.Items[b].Rate
	})
	return r
}

func (r gstr1Row) same(o gstr1Row) bool {
	a, _ := json.Marshal(r)
	b, _ := json.Marshal(o)
	return bytes.Equal(a, b)
}

func (r gstr1Row) b2csType() string {
	if r.Inter {
		return "INTER"
	}
	return "INTRA"
}

// gstr1Changed is a filed document this month's file reports again.
type gstr1Changed struct {
	doc   gstr1Doc
	month time.Time // the month it was filed for
	gone  bool      // cancelled or deleted: its filed row is dropped
	key   string
	gstin string
}

// gstr1AmendSet is the amendment tables of one GSTIN's file.
type gstr1AmendSet struct {
	changed                  []gstr1Changed
	fresh                    []gstr1Doc // a B2CS sale that now has a GSTIN: listed this month
	b2ba, b2cla, expa, cdnra map[string][]map[string]interface{}
	cdnura                   []map[string]interface{}
	b2csa                    []map[string]interface{}
}

func (a *gstr1AmendSet) write(file map[string]interface{}) {
	gstr1Grouped(file, "b2ba", "ctin", "inv", a.b2ba)
	gstr1Grouped(file, "b2cla", "pos", "inv", a.b2cla)
	if len(a.b2csa) > 0 {
		file["b2csa"] = a.b2csa
	}
	gstr1Grouped(file, "cdnra", "ctin", "nt", a.cdnra)
	if len(a.cdnura) > 0 {
		file["cdnura"] = a.cdnura
	}
	gstr1Grouped(file, "expa", "exp_typ", "inv", a.expa)
}

// add lists the document in the amendment table of its own table, naming the
// number and date it was filed under.
func (a *gstr1AmendSet) add(d gstr1Doc, old gstr1Row) {
	e := d.entry()
	if d.note {
		e["ont_num"], e["ont_dt"] = old.Num, old.Date
	} else {
		e["oinum"], e["oidt"] = old.Num, old.Date
	}
	switch d.section() {
	case "b2b":
		a.b2ba[d.ctin] = append(a.b2ba[d.ctin], e)
	case "b2cl":
		a.b2cla[d.pos] = append(a.b2cla[d.pos], e)
	case "exp":
		a.expa[d.export] = append(a.expa[d.export], e)
	case "cdnr":
		a.cdnra[d.ctin] = append(a.cdnra[d.ctin], e)
	case "cdnur":
		a.cdnura = append(a.cdnura, e)
	}
}

// gstr1AmendFrom is the first month an amendment can still reach: the start
// of the previous financial year.
func gstr1AmendFrom(month time.Time) time.Time {
	y := month.Year() - 1
	if month.Month() < time.April {
		y--
	}
	return time.Date(y, time.April, 1, 0, 0, 0, 0, time.UTC)
}

func gstr1RowsKept(schema string) bool {
	var ok bool
	_ = db.DB.QueryRow(`SELECT to_regclass($1) IS NOT NULL`, schema+".gstr1_filed_rows").Scan(&ok)
	return ok
}

type gstr1Bucket struct {
	omon    time.Time
	ty, pos string
}

// gstr1Amendments compares one GSTIN's filed documents before month with what
// they read now.
func gstr1Amendments(c *gstr1Ctx, gstin string, month time.Time) (*gstr1AmendSet, []string, error) {
	a := &gstr1AmendSet{b2ba: map[string][]map[string]interface{}{}, b2cla: map[string][]map[string]interface{}{},
		expa: map[string][]map[string]interface{}{}, cdnra: map[string][]map[string]interface{}{}}
	var warnings []string
	if !gstr1RowsKept(c.schema) {
		return a, warnings, nil
	}
	rows, err := db.DB.Query(fmt.Sprintf(`
		WITH cust AS (
			SELECT id, COALESCE(data->>'code', '') AS code, updated_at FROM %[1]s.documents
			 WHERE doctype = 'Customer' AND updated_at > (SELECT MIN(filed_at) FROM %[1]s.gstr1_filed_rows
			                                              WHERE gstin = $1 AND month_start >= $2 AND month_start < $3))
		SELECT r.doctype, r.doc_id, r.month_start, r.row, COALESCE(d.status, ''), COALESCE(d.data, '{}'::jsonb),
		       d.id IS NOT NULL AND d.deleted_at IS NULL
		  FROM %[1]s.gstr1_filed_rows r
		  LEFT JOIN %[1]s.documents d ON d.id = r.doc_id AND d.doctype = r.doctype
		 WHERE r.gstin = $1 AND r.month_start >= $2 AND r.month_start < $3
		   AND (d.id IS NULL OR d.deleted_at IS NOT NULL OR d.updated_at > r.filed_at
		        OR (r.party <> '' AND EXISTS (SELECT 1 FROM cust WHERE (cust.id = r.party OR cust.code = r.party) AND cust.updated_at > r.filed_at)))
		 ORDER BY r.month_start, r.doc_key`, c.schema), gstin, gstr1AmendFrom(month), month)
	if err != nil {
		return nil, nil, err
	}
	type cand struct {
		doctype, id, status string
		month               time.Time
		old                 gstr1Row
		data                map[string]interface{}
		live                bool
	}
	var cands []cand
	for rows.Next() {
		var cd cand
		var oldRaw, dataRaw []byte
		if err := rows.Scan(&cd.doctype, &cd.id, &cd.month, &oldRaw, &cd.status, &dataRaw, &cd.live); err != nil {
			rows.Close()
			return nil, nil, err
		}
		_ = json.Unmarshal(oldRaw, &cd.old)
		_ = json.Unmarshal(dataRaw, &cd.data)
		cands = append(cands, cd)
	}
	rows.Close()

	touched := map[gstr1Bucket]bool{}
	newB2CS := map[string]*gstr1Row{} // key -> its B2CS row now (nil: no longer one)
	for _, cd := range cands {
		key := cd.doctype + ":" + cd.id
		label := fmt.Sprintf("%s %s (filed for %s)", gstr1KindLabel(cd.doctype), cd.old.Num, cd.month.Format("01-2006"))
		if !cd.live || cd.status == "Cancelled" {
			warnings = append(warnings, label+" was cancelled or deleted after filing: amend it to nil on the portal")
			a.changed = append(a.changed, gstr1Changed{month: cd.month, gone: true, key: key, gstin: gstin})
			continue
		}
		day, _ := time.Parse("02-01-2006", cd.old.Date)
		var d gstr1Doc
		var err error
		switch cd.doctype {
		case "POSCart":
			d, err = gstr1TillBill(c, cd.id, cd.data, day)
		case "ReturnRequest":
			d, _, err = gstr1TillReturn(c, cd.id, cd.data, day)
		default:
			d, err = gstr1CanonicalDoc(c, cd.doctype, cd.id, cd.data)
		}
		if err != nil {
			warnings = append(warnings, label+" could not be read again: "+err.Error())
			continue
		}
		if d.gstin != gstin {
			warnings = append(warnings, label+" is now issued under another GSTIN: amend it on the portal by hand")
			continue
		}
		nr := d.row()
		if nr.same(cd.old) {
			continue
		}
		oldSec, newSec := cd.old.Sec, nr.Sec
		switch {
		case oldSec == "b2cs":
			touched[gstr1Bucket{cd.month, cd.old.b2csType(), cd.old.Pos}] = true
			if newSec == "b2cs" {
				touched[gstr1Bucket{cd.month, nr.b2csType(), nr.Pos}] = true
				newB2CS[key] = &nr
			} else {
				newB2CS[key] = nil
				if newSec != "nil" {
					a.fresh = append(a.fresh, d)
					warnings = append(warnings, fmt.Sprintf("%s was filed as B2C: listed in %s this month and taken out of B2C", label, strings.ToUpper(newSec)))
				}
			}
		case oldSec == newSec && oldSec != "nil":
			a.add(d, cd.old)
		case oldSec == "nil" && newSec == "nil":
			warnings = append(warnings, label+": its nil / exempt value changed after filing; adjust this month's nil table on the portal")
		default:
			warnings = append(warnings, fmt.Sprintf("%s moved from %s to %s after filing: amend it on the portal by hand", label, strings.ToUpper(oldSec), strings.ToUpper(newSec)))
		}
		a.changed = append(a.changed, gstr1Changed{doc: d, month: cd.month, key: key, gstin: gstin})
	}
	if len(touched) > 0 {
		if err := a.restateB2CS(c, gstin, touched, newB2CS); err != nil {
			return nil, nil, err
		}
	}
	return a, warnings, nil
}

func gstr1KindLabel(doctype string) string {
	switch doctype {
	case "POSCart":
		return "Till bill"
	case "ReturnRequest":
		return "Till credit note"
	case "CreditNote":
		return "Credit note"
	}
	return "Invoice"
}

// restateB2CS is the b2csa rows: each touched month / supply type / state
// restated, rate by rate, from what was filed with the changed sales as they
// read now.
func (a *gstr1AmendSet) restateB2CS(c *gstr1Ctx, gstin string, touched map[gstr1Bucket]bool, newRows map[string]*gstr1Row) error {
	var months []time.Time
	seen := map[time.Time]bool{}
	for b := range touched {
		if !seen[b.omon] {
			seen[b.omon] = true
			months = append(months, b.omon)
		}
	}
	sort.Slice(months, func(i, j int) bool { return months[i].Before(months[j]) })
	type agg map[gstr1Bucket]map[float64]*gstr1RowItem
	addTo := func(m agg, b gstr1Bucket, r gstr1Row) {
		if !touched[b] {
			return
		}
		sign := int64(1)
		if r.Note {
			sign = -1
		}
		if m[b] == nil {
			m[b] = map[float64]*gstr1RowItem{}
		}
		for _, it := range r.Items {
			if it.Nil != "" {
				continue
			}
			g := m[b][it.Rate]
			if g == nil {
				g = &gstr1RowItem{Rate: it.Rate}
				m[b][it.Rate] = g
			}
			g.Txval += sign * it.Txval
			g.I += sign * it.I
			g.C += sign * it.C
			g.S += sign * it.S
		}
	}
	for _, m := range months {
		rows, err := db.DB.Query(fmt.Sprintf(`SELECT doc_key, row FROM %s.gstr1_filed_rows WHERE gstin = $1 AND month_start = $2 AND section = 'b2cs'`, c.schema), gstin, m)
		if err != nil {
			return err
		}
		before, after := agg{}, agg{}
		for rows.Next() {
			var key string
			var raw []byte
			if err := rows.Scan(&key, &raw); err != nil {
				rows.Close()
				return err
			}
			var r gstr1Row
			_ = json.Unmarshal(raw, &r)
			b := gstr1Bucket{m, r.b2csType(), r.Pos}
			addTo(before, b, r)
			if nr, ok := newRows[key]; ok {
				if nr != nil {
					addTo(after, gstr1Bucket{m, nr.b2csType(), nr.Pos}, *nr)
				}
			} else {
				addTo(after, b, r)
			}
		}
		rows.Close()
		var buckets []gstr1Bucket
		for b := range touched {
			if b.omon.Equal(m) {
				buckets = append(buckets, b)
			}
		}
		sort.Slice(buckets, func(i, j int) bool {
			if buckets[i].pos != buckets[j].pos {
				return buckets[i].pos < buckets[j].pos
			}
			return buckets[i].ty < buckets[j].ty
		})
		for _, b := range buckets {
			var rates []float64
			for r := range before[b] {
				rates = append(rates, r)
			}
			for r := range after[b] {
				if before[b][r] == nil {
					rates = append(rates, r)
				}
			}
			sort.Float64s(rates)
			itms := []map[string]interface{}{}
			for _, rt := range rates {
				g := after[b][rt]
				if g == nil {
					g = &gstr1RowItem{Rate: rt}
				}
				det := map[string]interface{}{"rt": rt, "txval": PaiseToRupees(g.Txval), "csamt": 0.0}
				if b.ty == "INTER" {
					det["iamt"] = PaiseToRupees(g.I)
				} else {
					det["camt"], det["samt"] = PaiseToRupees(g.C), PaiseToRupees(g.S)
				}
				itms = append(itms, det)
			}
			a.b2csa = append(a.b2csa, map[string]interface{}{"omon": m.Format("012006"), "sply_ty": b.ty, "pos": b.pos, "typ": "OE", "itms": itms})
		}
	}
	return nil
}

// gstr1Snapshot is what marking a month filed keeps: every GSTIN's documents
// of the month, and the filed documents that month's file amended.
type gstr1Snapshot struct {
	own, amended []map[string]interface{}
	gone         [][2]string // gstin, doc_key
}

func gstr1SnapRow(d gstr1Doc, month time.Time) map[string]interface{} {
	doctype, id, _ := strings.Cut(d.key, ":")
	return map[string]interface{}{"gstin": d.gstin, "doc_key": d.key, "doctype": doctype, "doc_id": id, "party": d.party,
		"month_start": month.Format("2006-01-02"), "section": d.listed(), "row": d.row()}
}

// takeGSTR1Snapshot reads the month as BuildGSTR1 does, for every GSTIN. Nil
// when the tenant has no gstr1_filed_rows table yet.
func takeGSTR1Snapshot(tenantID, month string) (*gstr1Snapshot, error) {
	c, err := newGSTR1Ctx(tenantID)
	if err != nil {
		return nil, err
	}
	if !gstr1RowsKept(c.schema) {
		return nil, nil
	}
	first, last, err := gstr1Month(month)
	if err != nil {
		return nil, err
	}
	docs, _, err := gstr1MonthDocs(c, first, last)
	if err != nil {
		return nil, err
	}
	s := &gstr1Snapshot{}
	gstins := map[string]bool{}
	for _, d := range docs {
		if d.cancelled || !gstinRe.MatchString(d.gstin) {
			continue
		}
		gstins[d.gstin] = true
		s.own = append(s.own, gstr1SnapRow(d, first))
	}
	rows, err := db.DB.Query(fmt.Sprintf(`SELECT DISTINCT gstin FROM %s.gstr1_filed_rows`, c.schema))
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var g string
		if rows.Scan(&g) == nil {
			gstins[g] = true
		}
	}
	rows.Close()
	for _, g := range gstr1SortedKeys(gstins) {
		a, _, err := gstr1Amendments(c, g, first)
		if err != nil {
			return nil, err
		}
		for _, ch := range a.changed {
			if ch.gone {
				s.gone = append(s.gone, [2]string{ch.gstin, ch.key})
			} else {
				s.amended = append(s.amended, gstr1SnapRow(ch.doc, ch.month))
			}
		}
	}
	return s, nil
}

// save writes the snapshot inside the transaction that marks the month.
func (s *gstr1Snapshot) save(tx *sql.Tx, schema, month string) error {
	if s == nil {
		return nil
	}
	put := func(rows []map[string]interface{}, guard string) error {
		if len(rows) == 0 {
			return nil
		}
		b, err := json.Marshal(rows)
		if err != nil {
			return err
		}
		_, err = tx.Exec(fmt.Sprintf(`
			INSERT INTO %[1]s.gstr1_filed_rows (gstin, doc_key, doctype, doc_id, party, month_start, section, row, filed_month)
			SELECT x.gstin, x.doc_key, x.doctype, x.doc_id, COALESCE(x.party, ''), x.month_start, x.section, x.row, ($2 || '-01')::date
			  FROM jsonb_to_recordset($1::jsonb) AS x(gstin text, doc_key text, doctype text, doc_id text, party text, month_start date, section text, row jsonb)
			ON CONFLICT (gstin, doc_key) DO UPDATE SET section = EXCLUDED.section, row = EXCLUDED.row, party = EXCLUDED.party,
				filed_month = EXCLUDED.filed_month, filed_at = CURRENT_TIMESTAMP %[2]s`, schema, guard), string(b), month)
		return err
	}
	// Marking a month again (after an unmark) refreshes its own rows, never a
	// row a later month's file has since amended.
	if err := put(s.own, fmt.Sprintf("WHERE %s.gstr1_filed_rows.filed_month = EXCLUDED.filed_month", schema)); err != nil {
		return err
	}
	if err := put(s.amended, ""); err != nil {
		return err
	}
	for _, g := range s.gone {
		if _, err := tx.Exec(fmt.Sprintf(`DELETE FROM %s.gstr1_filed_rows WHERE gstin = $1 AND doc_key = $2`, schema), g[0], g[1]); err != nil {
			return err
		}
	}
	return nil
}
