package engines

import (
	"encoding/json"
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/lib/pq"
)

// The item-alias heal (207.4): Item Names and Items saved before item 201
// have no item_alias. Each gets the one a save would give it today
// (autoItemAlias): an Item Name's is made from its name, an Item takes its
// item name's. Item Names are filled first, so their items take the alias
// just written - typed on the screen or proposed. Lookups are loaded once
// (item names, designs, categories) so a client with thousands of items
// heals in a few statements, not one query per item.

const healItemAlias = "item-alias"

func itemAliasHeal() healDef {
	return healDef{
		ID: healItemAlias, Item: "207.4", Title: "Item aliases", FieldLabel: "Item Alias",
		Doctypes: []string{"ItemName", "Item"}, MaxLen: ItemAliasMaxLen,
		count:    countItemAliasHeal,
		records:  itemAliasHealRecords,
		check:    checkItemAliasTyped,
		apply:    applyItemAliasHeal,
	}
}

const itemAliasBlank = `doctype = $1 AND deleted_at IS NULL AND COALESCE(TRIM(data->>'item_alias'), '') = ''`

func countItemAliasHeal(q healQuerier, schema string) (int, error) {
	var n int
	err := q.QueryRow(fmt.Sprintf(`SELECT
		(SELECT COUNT(*) FROM %[1]s.documents WHERE `+strings.Replace(itemAliasBlank, "$1", "'ItemName'", 1)+`) +
		(SELECT COUNT(*) FROM %[1]s.documents WHERE `+strings.Replace(itemAliasBlank, "$1", "'Item'", 1)+`)`, schema)).Scan(&n)
	return n, err
}

func checkItemAliasTyped(r HealRecord, typed string) error {
	if n := utf8.RuneCountInString(typed); n > ItemAliasMaxLen {
		return &ValidationError{Code: "PIM-9363", SubFor: "item_alias",
			Message: fmt.Sprintf("Item Alias \"%s\" for %s has %d characters; keep it to %d.", typed, r.ID, n, ItemAliasMaxLen)}
	}
	return nil
}

// aliasLookups mirrors autoItemAlias with everything it reads loaded once.
type aliasLookups struct {
	short       map[string]string
	itemNames   map[string]map[string]interface{} // by id, then by code
	designNames map[string]string                 // design id or code -> its item_name
	categories  map[string]string                 // category id or code -> name
}

func loadAliasLookups(q healQuerier, schema string) (*aliasLookups, error) {
	l := &aliasLookups{short: itemAliasShortFormsForSchema(schema), itemNames: map[string]map[string]interface{}{},
		designNames: map[string]string{}, categories: map[string]string{}}
	rows, err := q.Query(fmt.Sprintf(`SELECT id, data::text FROM %s.documents WHERE doctype = 'ItemName' AND deleted_at IS NULL ORDER BY id`, schema))
	if err != nil {
		return nil, err
	}
	byCode := map[string]map[string]interface{}{}
	for rows.Next() {
		var id, raw string
		if err := rows.Scan(&id, &raw); err != nil {
			rows.Close()
			return nil, err
		}
		var d map[string]interface{}
		if json.Unmarshal([]byte(raw), &d) != nil {
			continue
		}
		l.itemNames[id] = d
		if c := strField(d, "code"); c != "" {
			if _, seen := byCode[c]; !seen {
				byCode[c] = d
			}
		}
	}
	rows.Close()
	for c, d := range byCode {
		if _, isID := l.itemNames[c]; !isID {
			l.itemNames[c] = d
		}
	}
	if err := loadRefMap(q, schema, "ProductFamily", "item_name", l.designNames); err != nil {
		return nil, err
	}
	if err := loadRefMap(q, schema, "Category", "name", l.categories); err != nil {
		return nil, err
	}
	return l, nil
}

// loadRefMap maps each live record's id, then its code, to one data field.
func loadRefMap(q healQuerier, schema, doctype, field string, out map[string]string) error {
	rows, err := q.Query(fmt.Sprintf(`SELECT id, COALESCE(data->>'code', ''), TRIM(COALESCE(data->>$2, ''))
		FROM %s.documents WHERE doctype = $1 AND deleted_at IS NULL ORDER BY id`, schema), doctype, field)
	if err != nil {
		return err
	}
	defer rows.Close()
	codes := map[string]string{}
	for rows.Next() {
		var id, code, v string
		if err := rows.Scan(&id, &code, &v); err != nil {
			return err
		}
		out[id] = v
		if code != "" {
			if _, seen := codes[code]; !seen {
				codes[code] = v
			}
		}
	}
	for c, v := range codes {
		if _, isID := out[c]; !isID {
			out[c] = v
		}
	}
	return rows.Err()
}

// category is masterDisplayName for a Category: its name, else the ref itself.
func (l *aliasLookups) category(ref string) string {
	ref = strings.TrimSpace(ref)
	if n := l.categories[ref]; n != "" {
		return n
	}
	return ref
}

// itemNameOf is itemNameRecordOf: the item's own item name, else its design's.
func (l *aliasLookups) itemNameOf(item map[string]interface{}) map[string]interface{} {
	ref := strings.TrimSpace(strField(item, "item_name"))
	if ref == "" {
		design := firstNonEmpty(strings.TrimSpace(strField(item, "family")), strings.TrimSpace(strField(item, "parent_product_code")))
		if design == "" {
			return nil
		}
		ref = l.designNames[design]
	}
	if ref == "" {
		return nil
	}
	return l.itemNames[ref]
}

// alias is autoItemAlias for a record.
func (l *aliasLookups) alias(doctype string, rec map[string]interface{}) string {
	if doctype == "Item" {
		if in := l.itemNameOf(rec); in != nil {
			if a := strings.TrimSpace(strField(in, "item_alias")); a != "" {
				return a
			}
			return MakeItemAlias(strField(in, "name"), l.category(strField(in, "category")), l.short)
		}
	}
	return MakeItemAlias(strField(rec, "name"), l.category(strField(rec, "category")), l.short)
}

// blankAliasRecords is every live record of doctype with no alias, in id
// order, at most limit (0: all).
func blankAliasRecords(q healQuerier, schema, doctype string, limit int) ([]string, []map[string]interface{}, error) {
	query := fmt.Sprintf(`SELECT id, data::text FROM %s.documents WHERE `+itemAliasBlank+` ORDER BY id`, schema)
	if limit > 0 {
		query += fmt.Sprintf(" LIMIT %d", limit)
	}
	rows, err := q.Query(query, doctype)
	if err != nil {
		return nil, nil, err
	}
	defer rows.Close()
	var ids []string
	var recs []map[string]interface{}
	for rows.Next() {
		var id, raw string
		if err := rows.Scan(&id, &raw); err != nil {
			return nil, nil, err
		}
		var d map[string]interface{}
		if json.Unmarshal([]byte(raw), &d) != nil {
			d = map[string]interface{}{}
		}
		ids, recs = append(ids, id), append(recs, d)
	}
	return ids, recs, rows.Err()
}

func itemAliasHealLabel(rec map[string]interface{}, id string) string {
	name := strings.TrimSpace(strField(rec, "name"))
	code := strings.TrimSpace(strField(rec, "code"))
	switch {
	case name != "" && code != "" && code != name:
		return code + " · " + name
	case name != "":
		return name
	case code != "":
		return code
	}
	return id
}

func itemAliasHealRecords(q healQuerier, schema string, limit int) ([]HealRecord, error) {
	l, err := loadAliasLookups(q, schema)
	if err != nil {
		return nil, err
	}
	var out []HealRecord
	for _, dt := range []string{"ItemName", "Item"} {
		left := limit - len(out)
		if left <= 0 {
			break
		}
		ids, recs, err := blankAliasRecords(q, schema, dt, left)
		if err != nil {
			return nil, err
		}
		for i, id := range ids {
			out = append(out, HealRecord{Doctype: dt, ID: id, Label: itemAliasHealLabel(recs[i], id), Proposed: l.alias(dt, recs[i])})
		}
	}
	return out, nil
}

func applyItemAliasHeal(q healQuerier, schema string, typed map[string]string) (fixed, typedUsed int, err error) {
	l, err := loadAliasLookups(q, schema)
	if err != nil {
		return 0, 0, err
	}
	for _, dt := range []string{"ItemName", "Item"} {
		ids, recs, err := blankAliasRecords(q, schema, dt, 0)
		if err != nil {
			return fixed, typedUsed, err
		}
		var setIDs, setAliases []string
		for i, id := range ids {
			a := typed[dt+"/"+id]
			if a != "" {
				typedUsed++
			} else {
				a = l.alias(dt, recs[i])
			}
			if a == "" {
				continue
			}
			setIDs, setAliases = append(setIDs, id), append(setAliases, a)
			if in := l.itemNames[id]; dt == "ItemName" && in != nil {
				// Its items take the alias just written.
				in["item_alias"] = a
			}
		}
		if len(setIDs) == 0 {
			continue
		}
		res, err := q.Exec(fmt.Sprintf(`UPDATE %s.documents d
			SET data = jsonb_set(d.data, '{item_alias}', to_jsonb(u.alias)), updated_at = CURRENT_TIMESTAMP, version = d.version + 1
			FROM unnest($2::text[], $3::text[]) AS u(id, alias)
			WHERE d.id = u.id AND d.doctype = $1 AND d.deleted_at IS NULL AND COALESCE(TRIM(d.data->>'item_alias'), '') = ''`, schema),
			dt, pq.Array(setIDs), pq.Array(setAliases))
		if err != nil {
			return fixed, typedUsed, err
		}
		n, _ := res.RowsAffected()
		fixed += int(n)
	}
	return fixed, typedUsed, nil
}
