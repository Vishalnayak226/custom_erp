package engines

import (
	"custom_erp/db"
	"database/sql"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"unicode/utf8"

	"github.com/lib/pq"
)

// Item 201.1 (user, 2026-11-15): "Need a Item Alias name that will be
// printed. Make Item Alias Auto generated from Item Name. Can be overwritten.
// Make a smart logic. Necklace Antique will be Necklace Antq. Earring CZ will
// be same Earring CZ. Managalsutra Zircon will be Mangsutra Zirc. It will be
// char limit 14."
//
// item_alias sits on the Item Name master and on each Item. Left blank it is
// made from the name (MakeItemAlias); a typed one is kept. An item takes its
// item name's alias. Stickers print it where a template prints the item name.

const (
	SettingItemAliasShortForms = "inventory.item_alias_short_forms"
	// ItemAliasMaxLen is the most characters an alias holds (the user's limit).
	ItemAliasMaxLen = 14
	// DefaultItemAliasShortForms are the user's own examples, plus the
	// spellings their item names use.
	DefaultItemAliasShortForms = "Antique=Antq, Zircon=Zirc, Mangalsutra=Mangsutra, Mangalsuthra=Mangsutra, Managalsutra=Mangsutra"
)

// ItemAliasShortForms reads the editable short-forms list: "Word=Short"
// pairs separated by commas, semicolons or new lines, one word each, any case.
func ItemAliasShortForms(tenantID string) map[string]string {
	return parseItemAliasShortForms(GetSettingString(tenantID, SettingItemAliasShortForms))
}

func itemAliasShortFormsForSchema(schema string) map[string]string {
	return parseItemAliasShortForms(GetSettingStringForSchema(schema, SettingItemAliasShortForms))
}

func parseItemAliasShortForms(s string) map[string]string {
	out := map[string]string{}
	for _, pair := range strings.FieldsFunc(s, func(r rune) bool { return r == ',' || r == ';' || r == '\n' || r == '\r' }) {
		kv := strings.SplitN(pair, "=", 2)
		if len(kv) != 2 {
			continue
		}
		from, to := strings.TrimSpace(kv[0]), strings.TrimSpace(kv[1])
		if from == "" || to == "" || strings.ContainsAny(from, " \t") {
			continue
		}
		out[strings.ToLower(from)] = to
	}
	return out
}

// MakeItemAlias is name in at most ItemAliasMaxLen characters. lead is the
// part to shorten first (the category: "Antique" in "Necklace Antique"), ""
// when unknown. In order, until it fits:
//  1. a name that fits is kept as it is ("Earring CZ");
//  2. words with a short form are swapped for it - the lead's words first,
//     then the rest, longest word first ("Necklace Antique" -> "Necklace Antq");
//  3. vowels after a word's first letter go, from the end of the word, the
//     same word order ("Kundan Necklace" -> "Kundn Necklace");
//  4. the rest is cut.
func MakeItemAlias(name, lead string, short map[string]string) string {
	words := strings.Fields(name)
	join := func() string { return strings.Join(words, " ") }
	fits := func() bool { return utf8.RuneCountInString(join()) <= ItemAliasMaxLen }
	if fits() {
		return join()
	}
	inLead := map[string]bool{}
	for _, w := range strings.Fields(lead) {
		inLead[strings.ToLower(w)] = true
	}
	// The order words are shortened in: the lead's first, then longest
	// first, the later word first on a tie.
	order := func() []int {
		idx := make([]int, len(words))
		for i := range idx {
			idx[i] = i
		}
		sort.SliceStable(idx, func(a, b int) bool {
			wa, wb := words[idx[a]], words[idx[b]]
			la, lb := inLead[strings.ToLower(wa)], inLead[strings.ToLower(wb)]
			if la != lb {
				return la
			}
			if na, nb := utf8.RuneCountInString(wa), utf8.RuneCountInString(wb); na != nb {
				return na > nb
			}
			return idx[a] > idx[b]
		})
		return idx
	}
	for _, i := range order() {
		if fits() {
			break
		}
		if s, ok := short[strings.ToLower(words[i])]; ok {
			words[i] = s
		}
	}
	for _, i := range order() {
		for !fits() {
			trimmed, ok := dropLastInnerVowel(words[i])
			if !ok {
				break
			}
			words[i] = trimmed
		}
		if fits() {
			break
		}
	}
	if fits() {
		return join()
	}
	r := []rune(join())
	return strings.TrimSpace(string(r[:ItemAliasMaxLen]))
}

// dropLastInnerVowel removes the last a/e/i/o/u after a word's first letter,
// keeping at least three letters.
func dropLastInnerVowel(w string) (string, bool) {
	r := []rune(w)
	if len(r) <= 3 {
		return w, false
	}
	for i := len(r) - 1; i >= 1; i-- {
		if strings.ContainsRune("aeiouAEIOU", r[i]) {
			return string(append(r[:i:i], r[i+1:]...)), true
		}
	}
	return w, false
}

// itemNameAliasOf is the alias an Item Name record prints: its own, else one
// made from its name with its category shortened first.
func itemNameAliasOf(schema string, rec map[string]interface{}, short map[string]string) string {
	if a := strings.TrimSpace(strField(rec, "item_alias")); a != "" {
		return a
	}
	return MakeItemAlias(strField(rec, "name"), masterDisplayName(schema, "Category", strField(rec, "category")), short)
}

// itemNameRecordOf is the Item Name an item hangs off - its own item_name, else
// its design's - or nil.
func itemNameRecordOf(schema string, item map[string]interface{}) map[string]interface{} {
	ref := strings.TrimSpace(strField(item, "item_name"))
	if ref == "" {
		design := firstNonEmpty(strings.TrimSpace(strField(item, "family")), strings.TrimSpace(strField(item, "parent_product_code")))
		if design == "" {
			return nil
		}
		var v sql.NullString
		if db.DB.QueryRow(fmt.Sprintf(`SELECT data->>'item_name' FROM %s.documents
			WHERE doctype = 'ProductFamily' AND deleted_at IS NULL AND (id = $1 OR data->>'code' = $1) LIMIT 1`, schema), design).Scan(&v) != nil {
			return nil
		}
		ref = strings.TrimSpace(v.String)
	}
	if ref == "" {
		return nil
	}
	return storedDocData(schema, "ItemName", ref)
}

// storedDocData is a live record's data by id or code, or nil.
func storedDocData(schema, doctype, ref string) map[string]interface{} {
	var raw string
	if db.DB.QueryRow(fmt.Sprintf(`SELECT data::text FROM %s.documents
		WHERE doctype = $1 AND deleted_at IS NULL AND (id = $2 OR data->>'code' = $2) LIMIT 1`, schema), doctype, ref).Scan(&raw) != nil {
		return nil
	}
	var d map[string]interface{}
	if json.Unmarshal([]byte(raw), &d) != nil {
		return nil
	}
	return d
}

// autoItemAlias is the alias a record gets when none is typed: an Item Name's
// is made from its name; an Item takes its item name's, or else one made from
// its own name.
func autoItemAlias(schema, doctype string, rec map[string]interface{}, short map[string]string) string {
	if doctype == "ItemName" {
		return MakeItemAlias(strField(rec, "name"), masterDisplayName(schema, "Category", strField(rec, "category")), short)
	}
	if in := itemNameRecordOf(schema, rec); in != nil {
		return itemNameAliasOf(schema, in, short)
	}
	return MakeItemAlias(strField(rec, "name"), masterDisplayName(schema, "Category", strField(rec, "category")), short)
}

// ItemAliasForItem is what a sticker prints for an item: its alias, else the
// one it would get (so records saved before 201.1 print a short name too).
func ItemAliasForItem(schema string, item map[string]interface{}) string {
	if a := strings.TrimSpace(strField(item, "item_alias")); a != "" {
		return a
	}
	return autoItemAlias(schema, "Item", item, itemAliasShortFormsForSchema(schema))
}

// PreviewItemName is the name and alias an Item Name form gets for a category
// and type (either may be blank), in the org's order.
func PreviewItemName(tenantID, category, productType string) (string, string, error) {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return "", "", err
	}
	cat := masterDisplayName(schema, "Category", category)
	name := composeItemName(ItemNameOrder(tenantID), cat, masterDisplayName(schema, "ProductType", productType))
	return name, MakeItemAlias(name, cat, ItemAliasShortForms(tenantID)), nil
}

// PreviewItemAlias is the alias a name gets, its category (id, code or name,
// may be blank) shortened first.
func PreviewItemAlias(tenantID, name, category string) (string, error) {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return "", err
	}
	return MakeItemAlias(name, masterDisplayName(schema, "Category", category), ItemAliasShortForms(tenantID)), nil
}

// PrepareItemAlias fills and checks item_alias on an Item Name or an Item -
// the record as it will be saved (an update already merged with its stored
// keys), id "" for a new one. Blank: made from the name. Unchanged from an
// alias that was made automatically: made again, so it follows a new name or
// item name. Typed: kept, at most ItemAliasMaxLen characters.
func PrepareItemAlias(tenantID, doctype, id string, payload map[string]interface{}) error {
	if doctype != "ItemName" && doctype != "Item" {
		return nil
	}
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return err
	}
	short := ItemAliasShortForms(tenantID)
	typed := strings.TrimSpace(strField(payload, "item_alias"))
	if typed != "" && id != "" {
		if stored := storedDocData(schema, doctype, id); stored != nil &&
			typed == strings.TrimSpace(strField(stored, "item_alias")) && typed == autoItemAlias(schema, doctype, stored, short) {
			typed = ""
		}
	}
	if typed == "" {
		payload["item_alias"] = autoItemAlias(schema, doctype, payload, short)
		return nil
	}
	if n := utf8.RuneCountInString(typed); n > ItemAliasMaxLen {
		return &ValidationError{Code: "PIM-9363", SubFor: "item_alias",
			Message: fmt.Sprintf("Item Alias \"%s\" has %d characters; keep it to %d.", typed, n, ItemAliasMaxLen)}
	}
	payload["item_alias"] = typed
	return nil
}

type queryExecer interface {
	Query(query string, args ...interface{}) (*sql.Rows, error)
	Exec(query string, args ...interface{}) (sql.Result, error)
}

// followItemAlias gives every item under an item name (refs: its id and code,
// and its designs' ids and codes) that stores the old alias the new one. A
// typed alias that differs is left alone; a blank one already prints the
// item name's alias, so it needs no write.
func followItemAlias(q queryExecer, schema string, refs []string, oldAlias, newAlias string) (int64, error) {
	if oldAlias == "" || oldAlias == newAlias || len(refs) == 0 {
		return 0, nil
	}
	res, err := q.Exec(fmt.Sprintf(`UPDATE %s.documents
		SET data = jsonb_set(data, '{item_alias}', to_jsonb($2::text)), updated_at = CURRENT_TIMESTAMP, version = version + 1
		WHERE doctype = 'Item' AND deleted_at IS NULL
		  AND (data->>'item_name' = ANY($1) OR data->>'family' = ANY($1) OR data->>'parent_product_code' = ANY($1))
		  AND data->>'item_alias' = $3`, schema), pq.Array(refs), newAlias, oldAlias)
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}

// itemNameRefs is an item name's id and code plus its designs' ids and codes:
// every value an item names it by.
func itemNameRefs(q queryExecer, schema, id, code string) ([]string, error) {
	refs := []string{id}
	if code != "" && code != id {
		refs = append(refs, code)
	}
	rows, err := q.Query(fmt.Sprintf(`SELECT id, COALESCE(data->>'code', '') FROM %s.documents
		WHERE doctype = 'ProductFamily' AND deleted_at IS NULL AND data->>'item_name' = ANY($1)`, schema), pq.Array(refs))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var designs []string
	for rows.Next() {
		var did, dcode string
		if err := rows.Scan(&did, &dcode); err != nil {
			return nil, err
		}
		designs = append(designs, did)
		if dcode != "" {
			designs = append(designs, dcode)
		}
	}
	return append(refs, designs...), rows.Err()
}

// FollowItemNameAlias runs after an Item Name is saved: when its alias
// changed, the items that printed the old one print the new one. Items
// with an alias typed by hand keep it.
func FollowItemNameAlias(tenantID, id string, prior, saved map[string]interface{}) {
	if prior == nil {
		return
	}
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return
	}
	short := ItemAliasShortForms(tenantID)
	oldAlias, newAlias := itemNameAliasOf(schema, prior, short), itemNameAliasOf(schema, saved, short)
	if oldAlias == newAlias {
		return
	}
	refs, err := itemNameRefs(db.DB, schema, id, firstNonEmpty(strField(saved, "code"), strField(prior, "code")))
	if err != nil {
		return
	}
	if n, err := followItemAlias(db.DB, schema, refs, oldAlias, newAlias); err == nil && n > 0 {
		LogAuditEvent(tenantID, "", "ITEM_ALIAS_FOLLOWED", "SUCCESS", fmt.Sprintf("item name %s: %q -> %q on %d items", id, oldAlias, newAlias, n))
	}
}
