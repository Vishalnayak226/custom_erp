package engines

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"custom_erp/db"

	"github.com/lib/pq"
)

// 215.10 (user, 2026-11-15: "All should be bulk uploaded"): an offer's item
// lists come from a CSV of rows - offer, list, code - rather than thousands of
// codes typed into one cell of the offers CSV. Each list the file names is
// replaced whole by the file's codes. The offers are then saved through the
// CSV import itself (merge into the stored offer, field rights, validateOffer),
// so a list reaches an offer exactly as the form or the offers CSV puts it.
//
// An offer with any bad row is left as it was: half a list is a different
// offer than the one the admin meant.

// OfferListChange is one list an upload replaces.
type OfferListChange struct {
	OfferID string `json:"offer_id"`
	Offer   string `json:"offer"`
	List    string `json:"list"`
	Before  int    `json:"before"`
	Codes   int    `json:"codes"`
}

// OfferListImportResult is the preview, or what was applied.
type OfferListImportResult struct {
	DryRun     bool                 `json:"dry_run"`
	TotalRows  int                  `json:"total_rows"`
	FailedRows int                  `json:"failed_rows"`
	Changes    []OfferListChange    `json:"changes"`
	Errors     []RowValidationError `json:"errors"`
}

const (
	offerListScope   = "Applies to"
	offerListGet     = "Get"
	offerListExclude = "Exclude"
)

var offerComboListName = regexp.MustCompile(`^combo\s*(?:list\s*)?(\d{1,2})$`)

// offerListKind reads a list cell: Applies to, Get, Exclude or Combo N.
func offerListKind(v string) (name string, combo int, ok bool) {
	k := strings.Join(strings.Fields(strings.ToLower(v)), " ")
	switch k {
	case "applies to", "applies", "scope", "items":
		return offerListScope, 0, true
	case "get", "get items", "get list":
		return offerListGet, 0, true
	case "exclude", "excluded", "exclude list":
		return offerListExclude, 0, true
	}
	if m := offerComboListName.FindStringSubmatch(k); m != nil {
		if n, _ := strconv.Atoi(m[1]); n >= 1 && n <= 20 {
			return fmt.Sprintf("Combo %d", n), n, true
		}
	}
	return "", 0, false
}

type offerListTarget struct {
	id, name, offerType, scope string
	data                       map[string]interface{}
	rows                       []int // file rows naming it
	bad                        bool
	lists                      map[string][]string // list name -> codes in file order
	seen                       map[string]map[string]bool
	combos                     map[int]bool
}

// ImportOfferItemLists previews (dryRun) or applies an item-list CSV.
func ImportOfferItemLists(ctx context.Context, tenantID, userID, role string, r io.Reader, dryRun bool) (*OfferListImportResult, error) {
	records, err := readCSVRecordsContext(ctx, tenantID, r)
	if err != nil {
		return nil, err
	}
	if len(records) < 2 {
		return nil, &ValidationError{Code: "DATAIM-0163", Message: "CSV is empty or missing data rows"}
	}
	col := map[string]int{}
	for i, h := range records[0] {
		switch strings.Join(strings.Fields(strings.ToLower(strings.TrimPrefix(h, string(rune(0xFEFF))))), " ") {
		case "offer", "offer id", "offer_id", "offer name":
			col["offer"] = i
		case "list", "list name":
			col["list"] = i
		case "code", "sku", "design", "item", "item code":
			col["code"] = i
		}
	}
	var missing []string
	for _, k := range []string{"offer", "list", "code"} {
		if _, ok := col[k]; !ok {
			missing = append(missing, k)
		}
	}
	if len(missing) > 0 {
		return nil, &ValidationError{Code: "DATAIM-0164", Message: "uploaded file is missing mandatory column(s): " + strings.Join(missing, ", ")}
	}
	cell := func(rec []string, k string) string {
		if i := col[k]; i < len(rec) {
			return strings.TrimSpace(rec[i])
		}
		return ""
	}

	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return nil, err
	}
	offers, byName, err := loadOfferListTargets(schema)
	if err != nil {
		return nil, err
	}

	res := &OfferListImportResult{DryRun: dryRun, TotalRows: len(records) - 1, Changes: []OfferListChange{}, Errors: []RowValidationError{}}
	failed := map[int]bool{}
	rowErr := func(row int, msg string) {
		if !failed[row] {
			failed[row] = true
			res.FailedRows++
		}
		if msg != "" && len(res.Errors) < maxImportErrorsRecorded {
			res.Errors = append(res.Errors, RowValidationError{RowNumber: row, Message: msg})
		}
	}

	type pending struct {
		row  int
		t    *offerListTarget
		list string
		code string
	}
	var rows []pending
	var codes []string
	for i, rec := range records[1:] {
		row := i + 2
		ref, listCell, code := cell(rec, "offer"), cell(rec, "list"), cell(rec, "code")
		if ref == "" && listCell == "" && code == "" {
			res.TotalRows--
			continue
		}
		t := offers[ref]
		if t == nil {
			switch hits := byName[strings.ToLower(ref)]; {
			case ref == "":
				rowErr(row, "Offer is blank.")
				continue
			case len(hits) > 1:
				rowErr(row, fmt.Sprintf("More than one offer is named %q: use its id.", ref))
				continue
			case len(hits) == 0:
				rowErr(row, fmt.Sprintf("No offer %q.", ref))
				continue
			default:
				t = hits[0]
			}
		}
		t.rows = append(t.rows, row)
		list, combo, ok := offerListKind(listCell)
		if !ok {
			rowErr(row, "List must be Applies to, Get, Exclude or Combo 1, Combo 2 ...")
			t.bad = true
			continue
		}
		if code == "" {
			rowErr(row, "Code is blank.")
			t.bad = true
			continue
		}
		switch {
		case list == offerListScope && t.scope != offerScopeSKUList && t.scope != offerScopeDesignList:
			rowErr(row, fmt.Sprintf("%s applies to %s: set Applies To to SKU List or Design List first.", t.name, orDash(t.scope)))
			t.bad = true
			continue
		case list == offerListGet && t.offerType != offerTypeBuyAGetB:
			rowErr(row, fmt.Sprintf("%s is %s: a Get list is for Buy A Get B.", t.name, orDash(t.offerType)))
			t.bad = true
			continue
		case combo > 0 && t.offerType != offerTypeCombo:
			rowErr(row, fmt.Sprintf("%s is %s: combo lists are for Combo.", t.name, orDash(t.offerType)))
			t.bad = true
			continue
		}
		if combo > 0 {
			t.combos[combo] = true
		}
		rows = append(rows, pending{row: row, t: t, list: list, code: code})
		codes = append(codes, strings.ToUpper(code))
	}

	items, designs, err := offerListCodeIndex(schema, codes)
	if err != nil {
		return nil, err
	}
	for _, p := range rows {
		up := strings.ToUpper(p.code)
		stored := p.code
		if it, isItem := items[up]; isItem {
			stored = it
		} else if !designs[up] {
			rowErr(p.row, fmt.Sprintf("No item or design %q.", p.code))
			p.t.bad = true
			continue
		} else if p.list == offerListScope && p.t.scope == offerScopeSKUList {
			rowErr(p.row, fmt.Sprintf("%q is a design: use Design List, or list its SKUs.", p.code))
			p.t.bad = true
			continue
		}
		seen := p.t.seen[p.list]
		if seen == nil {
			seen = map[string]bool{}
			p.t.seen[p.list] = seen
		}
		if key := strings.ToUpper(stored); !seen[key] {
			seen[key] = true
			p.t.lists[p.list] = append(p.t.lists[p.list], stored)
		}
	}

	// One update row per offer with nothing wrong, through the CSV import.
	var targets []*offerListTarget
	for _, t := range offers {
		if len(t.rows) == 0 {
			continue
		}
		if t.bad {
			skipOfferListTarget(t, rowErr, "")
			continue
		}
		targets = append(targets, t)
	}
	sort.Slice(targets, func(i, j int) bool { return targets[i].rows[0] < targets[j].rows[0] })
	docRows := make([]map[string]interface{}, 0, len(targets))
	changes := make([][]OfferListChange, 0, len(targets))
	for _, t := range targets {
		row := map[string]interface{}{"id": t.id}
		var ch []OfferListChange
		for _, l := range []struct{ name, field string }{{offerListScope, "scope_value"}, {offerListGet, "get_list"}, {offerListExclude, "exclude_list"}} {
			if c, ok := t.lists[l.name]; ok {
				row[l.field] = strings.Join(c, ", ")
				ch = append(ch, OfferListChange{OfferID: t.id, Offer: t.name, List: l.name, Before: len(splitSKUList(strField(t.data, l.field))), Codes: len(c)})
			}
		}
		if len(t.combos) > 0 {
			var ns []int
			for n := range t.combos {
				ns = append(ns, n)
			}
			sort.Ints(ns)
			before := splitOfferComboLists(strField(t.data, "combo_lists"))
			var lists []string
			for _, n := range ns {
				name := fmt.Sprintf("Combo %d", n)
				c := t.lists[name]
				was := 0
				if n <= len(before) {
					was = len(splitSKUList(before[n-1]))
				}
				lists = append(lists, strings.Join(c, ", "))
				ch = append(ch, OfferListChange{OfferID: t.id, Offer: t.name, List: name, Before: was, Codes: len(c)})
			}
			row["combo_lists"] = strings.Join(lists, "\n")
		}
		docRows = append(docRows, row)
		changes = append(changes, ch)
	}
	if len(docRows) > 0 {
		saved, err := runDocDataImportContext(ctx, tenantID, "Offer", userID, role, dryRun, docRows, make([]string, len(docRows)))
		if err != nil {
			return nil, err
		}
		refused := map[int]string{}
		for _, e := range saved.Errors {
			refused[e.RowNumber-2] = e.Message
		}
		if saved.FailedRows > len(saved.Errors) { // errors past the recorded cap
			for i := range docRows {
				if _, ok := refused[i]; !ok && !offerListHas(saved.UpdatedIDs, targets[i].id) {
					refused[i] = "not saved"
				}
			}
		}
		for i, t := range targets {
			if msg, ok := refused[i]; ok {
				skipOfferListTarget(t, rowErr, msg)
				continue
			}
			res.Changes = append(res.Changes, changes[i]...)
		}
	}
	return res, nil
}

// skipOfferListTarget marks every row of an offer left as it was, with one
// message at its first row.
func skipOfferListTarget(t *offerListTarget, rowErr func(int, string), why string) {
	msg := t.name + " left as it was: fix its rows and upload again."
	if why != "" {
		msg = t.name + " left as it was: " + why
	}
	rowErr(t.rows[0], msg)
	for _, row := range t.rows[1:] {
		rowErr(row, "")
	}
}

func offerListHas(list []string, v string) bool {
	for _, s := range list {
		if s == v {
			return true
		}
	}
	return false
}

// loadOfferListTargets reads every offer, by id and by lower-case name.
func loadOfferListTargets(schema string) (map[string]*offerListTarget, map[string][]*offerListTarget, error) {
	rows, err := db.DB.Query(fmt.Sprintf(`SELECT id, data FROM %s.documents WHERE doctype = 'Offer' AND deleted_at IS NULL`, schema))
	if err != nil {
		return nil, nil, err
	}
	defer rows.Close()
	byID := map[string]*offerListTarget{}
	byName := map[string][]*offerListTarget{}
	for rows.Next() {
		var id string
		var raw []byte
		if err := rows.Scan(&id, &raw); err != nil {
			return nil, nil, err
		}
		d := map[string]interface{}{}
		_ = json.Unmarshal(raw, &d)
		t := &offerListTarget{
			id: id, name: strings.TrimSpace(strField(d, "name")), data: d,
			offerType: strings.TrimSpace(strField(d, "offer_type")), scope: strings.TrimSpace(strField(d, "scope")),
			lists: map[string][]string{}, seen: map[string]map[string]bool{}, combos: map[int]bool{},
		}
		if t.name == "" {
			t.name = id
		}
		byID[id] = t
		byName[strings.ToLower(t.name)] = append(byName[strings.ToLower(t.name)], t)
	}
	return byID, byName, rows.Err()
}

// offerListCodeIndex finds the file's codes among items (by code, id or
// barcode, answered with the item's code) and designs (by id, code or design
// number). One read of each per upload.
func offerListCodeIndex(schema string, codes []string) (map[string]string, map[string]bool, error) {
	items := map[string]string{}
	designs := map[string]bool{}
	if len(codes) == 0 {
		return items, designs, nil
	}
	rows, err := db.DB.Query(fmt.Sprintf(`
		SELECT id, COALESCE(data->>'code', ''), COALESCE(data->>'barcode', '')
		  FROM %s.documents
		 WHERE doctype = 'Item' AND deleted_at IS NULL
		   AND (upper(data->>'code') = ANY($1) OR upper(id) = ANY($1) OR upper(data->>'barcode') = ANY($1))`, schema), pq.Array(codes))
	if err != nil {
		return nil, nil, err
	}
	for rows.Next() {
		var id, code, barcode string
		if err := rows.Scan(&id, &code, &barcode); err != nil {
			rows.Close()
			return nil, nil, err
		}
		canon := code
		if canon == "" {
			canon = id
		}
		for _, k := range []string{id, code, barcode} {
			if k = strings.ToUpper(strings.TrimSpace(k)); k != "" {
				if _, taken := items[k]; !taken || k == strings.ToUpper(code) {
					items[k] = canon
				}
			}
		}
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, nil, err
	}
	frows, err := db.DB.Query(fmt.Sprintf(`
		SELECT id, COALESCE(data->>'code', ''), COALESCE(data->>'design_number', '')
		  FROM %s.documents
		 WHERE doctype = 'ProductFamily' AND deleted_at IS NULL
		   AND (upper(id) = ANY($1) OR upper(data->>'code') = ANY($1) OR upper(data->>'design_number') = ANY($1))`, schema), pq.Array(codes))
	if err != nil {
		return nil, nil, err
	}
	defer frows.Close()
	for frows.Next() {
		var id, code, dn string
		if err := frows.Scan(&id, &code, &dn); err != nil {
			return nil, nil, err
		}
		for _, k := range []string{id, code, dn} {
			if k = strings.ToUpper(strings.TrimSpace(k)); k != "" {
				designs[k] = true
			}
		}
	}
	return items, designs, frows.Err()
}
