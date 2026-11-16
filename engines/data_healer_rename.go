package engines

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	"custom_erp/db"

	"github.com/lib/pq"
)

// Item 201.4, the Data Healer's second tool (owner, 2026-11-15: "If prev one
// any changes, I will be do in database level with superadmin access. Make
// that part also dead easy for me to change anything."): change an item's
// name on past documents, Super Admin only, instead of SQL.
//
// Most transaction lines hold only the item code and show the Item's name of
// today, so renaming the Item already renames them. The old name lives on in
// two places, and this changes both:
//   - lines that copied the name (a delivery challan, a material issue, a
//     stock adjustment, an RFQ line...): a line object carrying an item code
//     whose item_name, name or description is the old name;
//   - the frozen copy of an issued document's first print (130.16), which
//     every reprint shows: its lines' item_name, description ("name · variant")
//     and item name, with the copy's checksum made again.
//
// Only transaction record types (doctype_meta), only the created dates picked,
// only after a preview, and only on YES. A document filed with GST - an IRN
// or an e-way bill number - is listed and never changed. Each changed document
// gets one audit row (PAST_NAME_CHANGE) naming the old name, the new name and
// exactly which lines and print revisions changed, which is enough to put it
// back; the document's version goes up and its edit reason says why. The
// names match whole, ignoring case. Running the same change again finds
// nothing left to change.

const (
	pastRenameMaxDocs  = 20000 // one Apply changes at most this many documents
	pastRenameListMax  = 200   // the preview lists at most this many
	pastRenameMaxTypes = 100
	pastRenameMaxName  = 200
)

// PastRenameRequest is the screen's form. Apply also needs Confirm "YES".
type PastRenameRequest struct {
	Old      string   `json:"old"`
	New      string   `json:"new"`
	From     string   `json:"from"` // created on or after, YYYY-MM-DD (business day)
	To       string   `json:"to"`
	Doctypes []string `json:"doctypes"` // empty: every transaction type
	Confirm  string   `json:"confirm"`
}

// PastRenameType is one record type in the preview.
type PastRenameType struct {
	Doctype   string `json:"doctype"`
	Label     string `json:"label"`
	Documents int    `json:"documents"`
	Lines     int    `json:"lines"`
	Prints    int    `json:"prints"`
	Filed     int    `json:"filed"`
}

// PastRenameDoc is one document the change finds.
type PastRenameDoc struct {
	Doctype string `json:"doctype"`
	ID      string `json:"id"`
	Date    string `json:"date"`
	Lines   int    `json:"lines"`
	Prints  int    `json:"prints"`
	Filed   bool   `json:"filed"`
}

// PastRenamePreview is what Apply would change. Documents and Filed count
// documents; Docs lists the first pastRenameListMax of them, filed ones too.
type PastRenamePreview struct {
	Types     []PastRenameType `json:"types"`
	Docs      []PastRenameDoc  `json:"docs"`
	Documents int              `json:"documents"`
	Lines     int              `json:"lines"`
	Prints    int              `json:"prints"`
	Filed     int              `json:"filed"`
}

// PastRenameResult is what Apply changed.
type PastRenameResult struct {
	Documents int `json:"documents"`
	Lines     int `json:"lines"`
	Prints    int `json:"prints"`
	Filed     int `json:"filed"`
}

// pastRenameDocChange is one document's planned change.
type pastRenameDocChange struct {
	doc       PastRenameDoc
	dataPatch map[string]interface{} // changed top-level keys only
	linePaths []string
	prints    []pastRenamePrintChange
}

type pastRenamePrintChange struct {
	txn      string
	revision int
	lines    interface{}
	checksum string
	paths    []string
}

type pastRenameSnap struct {
	txn, doctype string
	revision     int
	payload      string
}

func pastRenameErr(code, msg string) error {
	return &ValidationError{Code: code, Message: msg}
}

// pastRenameClean checks the form and returns the names trimmed and the
// dates parsed (zero when not given).
func pastRenameClean(req *PastRenameRequest) (from, to time.Time, err error) {
	req.Old, req.New = strings.TrimSpace(req.Old), strings.TrimSpace(req.New)
	if req.Old == "" || req.New == "" {
		return from, to, pastRenameErr("MASTER-1705", "Type the old name and the new one.")
	}
	if req.Old == req.New {
		return from, to, pastRenameErr("MASTER-1705", "The new name is the same as the old one.")
	}
	if utf8.RuneCountInString(req.Old) > pastRenameMaxName || utf8.RuneCountInString(req.New) > pastRenameMaxName {
		return from, to, pastRenameErr("MASTER-1705", fmt.Sprintf("Names are %d characters at most.", pastRenameMaxName))
	}
	parse := func(s string) (time.Time, error) {
		if s = strings.TrimSpace(s); s == "" {
			return time.Time{}, nil
		}
		return time.ParseInLocation("2006-01-02", s, BusinessLocation())
	}
	if from, err = parse(req.From); err != nil {
		return from, to, pastRenameErr("MASTER-1706", "The From date is not a date.")
	}
	if to, err = parse(req.To); err != nil {
		return from, to, pastRenameErr("MASTER-1706", "The To date is not a date.")
	}
	if !from.IsZero() && !to.IsZero() && to.Before(from) {
		return from, to, pastRenameErr("MASTER-1706", "The To date is before the From date.")
	}
	if len(req.Doctypes) > pastRenameMaxTypes {
		return from, to, pastRenameErr("MASTER-1705", "Too many record types.")
	}
	return from, to, nil
}

// pastRenameNeedle is the part of the old name every match must contain as
// stored text: lower case, and the longest run without a quote or backslash
// (JSON escapes those, twice inside a line list kept as a string).
func pastRenameNeedle(old string) string {
	best := ""
	for _, part := range strings.FieldsFunc(strings.ToLower(old), func(r rune) bool { return r == '"' || r == '\\' }) {
		if len(part) > len(best) {
			best = part
		}
	}
	return best
}

func pastRenameTableExists(q healQuerier, schema, table string) bool {
	var ok bool
	_ = q.QueryRow(`SELECT to_regclass($1) IS NOT NULL`, schema+"."+table).Scan(&ok)
	return ok
}

// pastRenameScan finds every document the change touches, under a row lock
// when lock is set (Apply), and works out each one's change.
func pastRenameScan(q healQuerier, schema string, req PastRenameRequest, from, to time.Time, lock bool) ([]pastRenameDocChange, error) {
	needle := pastRenameNeedle(req.Old)
	hasSnaps := pastRenameTableExists(q, schema, "print_snapshots")
	filed := []string{}
	if pastRenameTableExists(q, schema, "irp_documents") {
		filed = append(filed, fmt.Sprintf(`EXISTS (SELECT 1 FROM %s.irp_documents i WHERE i.source_doctype = d.doctype AND i.source_id = d.id AND COALESCE(i.irn, '') <> '')`, schema))
	}
	if pastRenameTableExists(q, schema, "ewb_documents") {
		filed = append(filed, fmt.Sprintf(`EXISTS (SELECT 1 FROM %s.ewb_documents e WHERE e.doctype = d.doctype AND e.doc_id = d.id AND COALESCE(e.ewb_no, '') <> '')`, schema))
	}
	filedExpr := "false"
	if len(filed) > 0 {
		filedExpr = strings.Join(filed, " OR ")
	}
	match := `strpos(lower(d.data::text), $1) > 0`
	if hasSnaps {
		match = fmt.Sprintf(`(%s OR EXISTS (SELECT 1 FROM %s.print_snapshots s WHERE s.doc_id = d.id AND strpos(lower(s.payload::text), $1) > 0))`, match, schema)
	}
	day := `(d.created_at::timestamptz AT TIME ZONE public.erp_business_tz())::date`
	args := []interface{}{needle, pq.Array(req.Doctypes)}
	where := []string{`d.deleted_at IS NULL`, `(COALESCE(cardinality($2::text[]), 0) = 0 OR d.doctype = ANY($2::text[]))`, match}
	if !from.IsZero() {
		args = append(args, from.Format("2006-01-02"))
		where = append(where, fmt.Sprintf(`%s >= $%d::date`, day, len(args)))
	}
	if !to.IsZero() {
		args = append(args, to.Format("2006-01-02"))
		where = append(where, fmt.Sprintf(`%s <= $%d::date`, day, len(args)))
	}
	query := fmt.Sprintf(`SELECT d.id, d.doctype, %s::text, d.data::text, (%s)
		FROM %s.documents d JOIN %s.doctype_meta m ON m.name = d.doctype AND m.document_type = 'Transaction'
		WHERE %s ORDER BY d.created_at, d.id`, day, filedExpr, schema, schema, strings.Join(where, " AND "))
	if lock {
		query += ` FOR UPDATE OF d`
	}
	rows, err := q.Query(query, args...)
	if err != nil {
		return nil, err
	}
	type found struct {
		doc  PastRenameDoc
		data string
	}
	var docs []found
	ids := []string{}
	for rows.Next() {
		var f found
		if err := rows.Scan(&f.doc.ID, &f.doc.Doctype, &f.doc.Date, &f.data, &f.doc.Filed); err != nil {
			rows.Close()
			return nil, err
		}
		docs = append(docs, f)
		ids = append(ids, f.doc.ID)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}
	snaps := map[string][]pastRenameSnap{}
	if hasSnaps && len(ids) > 0 {
		srows, err := q.Query(fmt.Sprintf(`SELECT s.txn, s.doc_id, COALESCE(s.doctype, ''), s.revision, s.payload::text
			FROM %s.print_snapshots s WHERE s.doc_id = ANY($1) AND strpos(lower(s.payload::text), $2) > 0
			ORDER BY s.doc_id, s.txn, s.revision`, schema), pq.Array(ids), needle)
		if err != nil {
			return nil, err
		}
		for srows.Next() {
			var s pastRenameSnap
			var docID string
			if err := srows.Scan(&s.txn, &docID, &s.doctype, &s.revision, &s.payload); err != nil {
				srows.Close()
				return nil, err
			}
			snaps[docID] = append(snaps[docID], s)
		}
		srows.Close()
		if err := srows.Err(); err != nil {
			return nil, err
		}
	}
	out := []pastRenameDocChange{}
	for _, f := range docs {
		c := pastRenameDocChange{doc: f.doc}
		c.dataPatch, c.linePaths = pastRenameData(f.data, req.Old, req.New)
		for _, s := range snaps[f.doc.ID] {
			if s.doctype != "" && s.doctype != f.doc.Doctype {
				continue
			}
			if pc, ok := pastRenamePrint(s, req.Old, req.New); ok {
				c.prints = append(c.prints, pc)
			}
		}
		c.doc.Lines, c.doc.Prints = len(c.linePaths), len(c.prints)
		if c.doc.Lines+c.doc.Prints > 0 {
			out = append(out, c)
		}
	}
	return out, nil
}

func pastRenameDecode(raw string, v interface{}) error {
	dec := json.NewDecoder(strings.NewReader(raw))
	dec.UseNumber()
	return dec.Decode(v)
}

// pastRenameValue is the new value of one name field, if it changes. A
// description may carry the variant after the name ("Name · Silver / 7").
func pastRenameValue(v interface{}, old, new string, withVariant bool) (string, bool) {
	s, ok := v.(string)
	if !ok {
		return "", false
	}
	t := strings.TrimSpace(s)
	if strings.EqualFold(t, old) {
		return new, true
	}
	if i := strings.Index(t, " · "); withVariant && i > 0 && strings.EqualFold(strings.TrimSpace(t[:i]), old) {
		return new + t[i:], true
	}
	return "", false
}

// pastRenameItemLine is a line about an item: it carries an item code.
func pastRenameItemLine(l map[string]interface{}) bool {
	for _, k := range []string{"sku", "item_code", "item"} {
		if s, ok := l[k].(string); ok && strings.TrimSpace(s) != "" {
			return true
		}
	}
	return false
}

// pastRenameFields changes the name fields of one item line in place and
// returns the keys it changed.
func pastRenameFields(l map[string]interface{}, old, new string) []string {
	var changed []string
	for _, k := range []string{"item_name", "name", "description"} {
		if nv, ok := pastRenameValue(l[k], old, new, k == "description"); ok {
			l[k] = nv
			changed = append(changed, k)
		}
	}
	return changed
}

// pastRenameData works out a document's own change: the line lists (kept as
// an array or as a JSON string, the same way back) and a header naming one
// item. It returns only the top-level keys that change.
func pastRenameData(raw, old, new string) (map[string]interface{}, []string) {
	var data map[string]interface{}
	if pastRenameDecode(raw, &data) != nil {
		return nil, nil
	}
	patch := map[string]interface{}{}
	var paths []string
	keys := make([]string, 0, len(data))
	for k := range data {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, key := range keys {
		var list []interface{}
		asString := false
		switch v := data[key].(type) {
		case []interface{}:
			list = v
		case string:
			if t := strings.TrimSpace(v); strings.HasPrefix(t, "[") && pastRenameDecode(t, &list) == nil {
				asString = true
			}
		}
		changedHere := false
		for i, el := range list {
			l, ok := el.(map[string]interface{})
			if !ok || !pastRenameItemLine(l) {
				continue
			}
			for _, f := range pastRenameFields(l, old, new) {
				paths = append(paths, fmt.Sprintf("%s[%d].%s", key, i, f))
				changedHere = true
			}
		}
		if !changedHere {
			continue
		}
		if asString {
			b, _ := json.Marshal(list)
			patch[key] = string(b)
		} else {
			patch[key] = list
		}
	}
	if pastRenameItemLine(data) {
		if nv, ok := pastRenameValue(data["item_name"], old, new, false); ok {
			patch["item_name"] = nv
			paths = append(paths, "item_name")
		}
	}
	return patch, paths
}

// pastRenamePrint works out a frozen print's change: its item lines' names,
// and the copy's checksum made again over what it now says.
func pastRenamePrint(s pastRenameSnap, old, new string) (pastRenamePrintChange, bool) {
	pc := pastRenamePrintChange{txn: s.txn, revision: s.revision}
	var payload map[string]interface{}
	if pastRenameDecode(s.payload, &payload) != nil {
		return pc, false
	}
	lines, _ := payload["lines"].([]interface{})
	for i, el := range lines {
		l, ok := el.(map[string]interface{})
		if !ok || !pastRenameItemLine(l) {
			continue
		}
		for _, f := range pastRenameFields(l, old, new) {
			pc.paths = append(pc.paths, fmt.Sprintf("lines[%d].%s", i, f))
		}
		if item, ok := l["item"].(map[string]interface{}); ok {
			if nv, ok := pastRenameValue(item["name"], old, new, false); ok {
				item["name"] = nv
				pc.paths = append(pc.paths, fmt.Sprintf("lines[%d].item.name", i))
			}
		}
	}
	if len(pc.paths) == 0 {
		return pc, false
	}
	pc.lines = lines
	raw, err := json.Marshal(payload)
	if err != nil {
		return pc, false
	}
	var doc PrintDocument
	if json.Unmarshal(raw, &doc) != nil {
		return pc, false
	}
	pc.checksum = printChecksum(doc)
	return pc, true
}

// PastRenamePreviewFor is what changing req.Old to req.New would change.
func PastRenamePreviewFor(tenantID string, req PastRenameRequest) (PastRenamePreview, error) {
	out := PastRenamePreview{Types: []PastRenameType{}, Docs: []PastRenameDoc{}}
	from, to, err := pastRenameClean(&req)
	if err != nil {
		return out, err
	}
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return out, err
	}
	changes, err := pastRenameScan(db.DB, schema, req, from, to, false)
	if err != nil {
		return out, err
	}
	byType := map[string]*PastRenameType{}
	order := []string{}
	for _, c := range changes {
		t := byType[c.doc.Doctype]
		if t == nil {
			t = &PastRenameType{Doctype: c.doc.Doctype, Label: humanizeDoctype(c.doc.Doctype)}
			byType[c.doc.Doctype] = t
			order = append(order, c.doc.Doctype)
		}
		if c.doc.Filed {
			t.Filed++
			out.Filed++
		} else {
			t.Documents++
			t.Lines += c.doc.Lines
			t.Prints += c.doc.Prints
			out.Documents++
			out.Lines += c.doc.Lines
			out.Prints += c.doc.Prints
		}
		if len(out.Docs) < pastRenameListMax {
			out.Docs = append(out.Docs, c.doc)
		}
	}
	sort.Strings(order)
	for _, d := range order {
		out.Types = append(out.Types, *byType[d])
	}
	return out, nil
}

// PastRenameApply makes the change: one transaction under a per-client lock,
// so two Super Admins pressing Apply together change each document once.
func PastRenameApply(tenantID string, req PastRenameRequest, actor string) (PastRenameResult, error) {
	var res PastRenameResult
	from, to, err := pastRenameClean(&req)
	if err != nil {
		return res, err
	}
	if strings.TrimSpace(req.Confirm) != "YES" {
		return res, pastRenameErr("MASTER-1707", "Type YES to change past documents.")
	}
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return res, err
	}
	tx, err := db.DB.Begin()
	if err != nil {
		return res, err
	}
	defer tx.Rollback()
	if _, err := tx.Exec(`SELECT pg_advisory_xact_lock(hashtext($1))`, "past-rename|"+schema); err != nil {
		return res, err
	}
	changes, err := pastRenameScan(tx, schema, req, from, to, true)
	if err != nil {
		return res, err
	}
	todo := 0
	for _, c := range changes {
		if !c.doc.Filed {
			todo++
		}
	}
	if todo > pastRenameMaxDocs {
		return res, pastRenameErr("MASTER-1708", fmt.Sprintf("%d documents: more than %d at once. Narrow the dates.", todo, pastRenameMaxDocs))
	}
	reason := fmt.Sprintf("Name changed on past documents: %s -> %s", req.Old, req.New)
	audits := []string{}
	for _, c := range changes {
		if c.doc.Filed {
			res.Filed++
			continue
		}
		if len(c.dataPatch) > 0 {
			patch, err := json.Marshal(c.dataPatch)
			if err != nil {
				return res, err
			}
			if _, err := tx.Exec(fmt.Sprintf(`UPDATE %s.documents SET data = data || $1::jsonb, version = COALESCE(version, 1) + 1,
				updated_by = $2, edit_reason = $3, updated_at = CURRENT_TIMESTAMP WHERE id = $4 AND doctype = $5`, schema),
				string(patch), actor, reason, c.doc.ID, c.doc.Doctype); err != nil {
				return res, err
			}
		}
		printPaths := []string{}
		for _, p := range c.prints {
			lines, err := json.Marshal(p.lines)
			if err != nil {
				return res, err
			}
			if _, err := tx.Exec(fmt.Sprintf(`UPDATE %s.print_snapshots SET payload = payload || jsonb_build_object('lines', $1::jsonb), checksum = $2
				WHERE txn = $3 AND doc_id = $4 AND revision = $5`, schema), string(lines), p.checksum, p.txn, c.doc.ID, p.revision); err != nil {
				return res, err
			}
			printPaths = append(printPaths, fmt.Sprintf("%s#%d:%s", p.txn, p.revision, strings.Join(p.paths, ",")))
		}
		res.Documents++
		res.Lines += c.doc.Lines
		res.Prints += c.doc.Prints
		audits = append(audits, fmt.Sprintf("doctype=%s id=%s old=%q new=%q lines=%s prints=%s",
			c.doc.Doctype, c.doc.ID, req.Old, req.New, strings.Join(c.linePaths, ","), strings.Join(printPaths, " ")))
	}
	if err := tx.Commit(); err != nil {
		return res, err
	}
	for _, a := range audits {
		LogAuditEvent(tenantID, actor, "PAST_NAME_CHANGE", "Success", a)
	}
	LogAuditEvent(tenantID, actor, "PAST_NAME_CHANGE_RUN", "Success", fmt.Sprintf("old=%q new=%q from=%s to=%s types=%s documents=%d lines=%d prints=%d filed_skipped=%d",
		req.Old, req.New, req.From, req.To, strings.Join(req.Doctypes, ","), res.Documents, res.Lines, res.Prints, res.Filed))
	return res, nil
}
