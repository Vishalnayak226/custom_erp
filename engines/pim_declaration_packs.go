package engines

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"custom_erp/db"

	"github.com/lib/pq"
)

// Declaration packs (47.13.6, ask 185). A pack is data: the declaration fields a
// category's products must carry (legal metrology, e-commerce). The packs ship in
// pim_declaration_packs.json; none applies until someone turns it on for a
// category with a DeclarationPack record, which names the reviewer and the
// evidence, an effective date and an optional last day, and goes through the
// approval engine. Whether a pack applies to a category is a legal call, so the
// shipped packaged-goods list is an example that stays off until a qualified
// reviewer turns it on.
//
// While an Approved record is in date, an item of that category (or a child
// category) is not ready to publish to the record's channel - every channel when
// none is named - until each required field has a value (CheckPublishReadiness),
// and the Declaration gaps report lists what is missing. Approving a record adds
// the pack's attributes the client does not have yet.

const DeclarationPackDoctype = "DeclarationPack"

//go:embed pim_declaration_packs.json
var declarationPacksJSON []byte

// DeclarationField is one field of a pack: an attribute (Code) or an Item field.
type DeclarationField struct {
	Code      string   `json:"code,omitempty"`
	ItemField string   `json:"item_field,omitempty"`
	Label     string   `json:"label"`
	Values    []string `json:"values,omitempty"`
	Required  bool     `json:"required"`
}

// DeclarationPackDef is one shipped pack.
type DeclarationPackDef struct {
	ID      string             `json:"id"`
	Label   string             `json:"label"`
	Version string             `json:"version"`
	Note    string             `json:"note"`
	Fields  []DeclarationField `json:"fields"`
}

var declarationPacks = func() map[string]DeclarationPackDef {
	var file struct {
		Packs []DeclarationPackDef `json:"packs"`
	}
	if err := json.Unmarshal(declarationPacksJSON, &file); err != nil {
		panic("pim_declaration_packs.json: " + err.Error())
	}
	out := map[string]DeclarationPackDef{}
	for _, p := range file.Packs {
		out[p.ID] = p
	}
	return out
}()

// DeclarationPackList returns the shipped packs, by id.
func DeclarationPackList() []DeclarationPackDef {
	out := make([]DeclarationPackDef, 0, len(declarationPacks))
	for _, p := range declarationPacks {
		out = append(out, p)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

// ValidateDeclarationPack is the save check: a shipped pack, a reviewer, the
// evidence, and a last day no earlier than the effective date.
func ValidateDeclarationPack(data map[string]interface{}) error {
	pack := strings.TrimSpace(strField(data, "pack"))
	if _, ok := declarationPacks[pack]; !ok {
		ids := make([]string, 0, len(declarationPacks))
		for id := range declarationPacks {
			ids = append(ids, id)
		}
		sort.Strings(ids)
		return &ValidationError{Code: "PIM-9362", SubFor: "pack", Message: fmt.Sprintf("unknown pack %q - the packs are %s", pack, strings.Join(ids, ", "))}
	}
	if strings.TrimSpace(strField(data, "reviewer")) == "" || strings.TrimSpace(strField(data, "evidence")) == "" {
		return &ValidationError{Code: "PIM-9362", SubFor: "reviewer", Message: "name the qualified reviewer and the evidence they checked"}
	}
	from, to := strings.TrimSpace(strField(data, "effective_from")), strings.TrimSpace(strField(data, "effective_to"))
	if from != "" && to != "" && len(from) >= 10 && len(to) >= 10 && to[:10] < from[:10] {
		return &ValidationError{Code: "PIM-9362", SubFor: "effective_to", Message: "the last day is before the effective date"}
	}
	return nil
}

// declarationAssignment is one Approved, in-date DeclarationPack record.
type declarationAssignment struct {
	ID, Pack, Category, Channel string
}

// activeDeclarationAssignments reads the Approved records in date today.
func activeDeclarationAssignments(schema string) ([]declarationAssignment, error) {
	today := BusinessToday()
	rows, err := db.DB.Query(fmt.Sprintf(`
		SELECT id, COALESCE(data->>'pack',''), COALESCE(data->>'category',''), COALESCE(data->>'channel','')
		FROM %s.documents
		WHERE doctype = 'DeclarationPack' AND status = 'Approved' AND deleted_at IS NULL
		  AND LEFT(COALESCE(data->>'effective_from',''), 10) <= $1
		  AND (COALESCE(data->>'effective_to','') = '' OR LEFT(data->>'effective_to', 10) >= $1)
		ORDER BY id`, schema), today)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []declarationAssignment
	for rows.Next() {
		var a declarationAssignment
		if err := rows.Scan(&a.ID, &a.Pack, &a.Category, &a.Channel); err != nil {
			return nil, err
		}
		if _, ok := declarationPacks[a.Pack]; ok {
			out = append(out, a)
		}
	}
	return out, rows.Err()
}

// categoryTree is the client's categories: parent by id, id by lower-case name.
type categoryTree struct {
	parent map[string]string
	byName map[string]string
	known  map[string]bool
}

func loadCategoryTree(schema string) (*categoryTree, error) {
	t := &categoryTree{parent: map[string]string{}, byName: map[string]string{}, known: map[string]bool{}}
	rows, err := db.DB.Query(fmt.Sprintf(`SELECT id, COALESCE(data->>'parent',''), lower(COALESCE(data->>'name','')) FROM %s.documents WHERE doctype = 'Category' AND deleted_at IS NULL ORDER BY id`, schema))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var id, parent, name string
		if err := rows.Scan(&id, &parent, &name); err != nil {
			return nil, err
		}
		t.known[id] = true
		if parent != "" {
			t.parent[id] = parent
		}
		if _, dup := t.byName[name]; name != "" && !dup {
			t.byName[name] = id
		}
	}
	return t, rows.Err()
}

// resolve turns an item's category (an id or a name) into the category id.
func (t *categoryTree) resolve(category string) string {
	if t.known[category] {
		return category
	}
	if id, ok := t.byName[strings.ToLower(strings.TrimSpace(category))]; ok {
		return id
	}
	return category
}

// within reports whether category is root or sits under it. A cycle stops the walk.
func (t *categoryTree) within(category, root string) bool {
	seen := map[string]bool{}
	for c := t.resolve(category); c != "" && !seen[c] && len(seen) < 32; c = t.parent[c] {
		if c == t.resolve(root) {
			return true
		}
		seen[c] = true
	}
	return false
}

// declarationGap is one item missing required fields of one pack.
type declarationGap struct {
	Pack    DeclarationPackDef
	Record  string
	Channel string
	Missing []string
}

// itemDeclarationGaps lists, for one item, every in-date pack that applies to
// its category on this channel ("" = any record, whatever its channel) and the
// required fields it still lacks.
func itemDeclarationGaps(schema, itemCode string, item map[string]interface{}, channel string, active []declarationAssignment, tree *categoryTree) ([]declarationGap, error) {
	category := strField(item, "category")
	if category == "" {
		return nil, nil
	}
	var gaps []declarationGap
	var filled map[string]bool
	for _, a := range active {
		if channel != "" && a.Channel != "" && a.Channel != channel {
			continue
		}
		if !tree.within(category, a.Category) {
			continue
		}
		if filled == nil {
			var err error
			if filled, err = filledAttributes(schema, itemCode); err != nil {
				return nil, err
			}
		}
		pack := declarationPacks[a.Pack]
		var missing []string
		for _, f := range pack.Fields {
			if !f.Required {
				continue
			}
			if f.ItemField != "" {
				if v := strings.TrimSpace(fmt.Sprintf("%v", orBlank(item[f.ItemField]))); v == "" || v == "0" {
					missing = append(missing, f.Label)
				}
			} else if !filled[f.Code] {
				missing = append(missing, f.Label)
			}
		}
		if len(missing) > 0 {
			gaps = append(gaps, declarationGap{Pack: pack, Record: a.ID, Channel: a.Channel, Missing: missing})
		}
	}
	return gaps, nil
}

// filledAttributes is the attributes the item has a value for, by attribute id
// and by the attribute's code (a client's own attribute may carry the pack's
// code under another id).
func filledAttributes(schema, itemCode string) (map[string]bool, error) {
	rows, err := db.DB.Query(fmt.Sprintf(`SELECT DISTINCT v.data->>'attribute', upper(COALESCE(d.data->>'code',''))
		FROM %s.documents v
		LEFT JOIN %s.documents d ON d.doctype = 'ProductAttributeDef' AND d.id = v.data->>'attribute' AND d.deleted_at IS NULL
		WHERE v.doctype = 'ProductAttributeValue' AND v.deleted_at IS NULL AND v.data->>'item' = $1
		  AND btrim(COALESCE(v.data->>'value','')) <> ''`, schema, schema), itemCode)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]bool{}
	for rows.Next() {
		var a, code string
		if err := rows.Scan(&a, &code); err != nil {
			return nil, err
		}
		out[a] = true
		if code != "" {
			out[code] = true
		}
	}
	return out, rows.Err()
}

// declarationPublishFailures is what CheckPublishReadiness adds: one line per
// pack the item still lacks fields of on this channel.
func declarationPublishFailures(tenantID, itemCode, channel string, item map[string]interface{}) ([]string, error) {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return nil, err
	}
	active, err := activeDeclarationAssignments(schema)
	if err != nil || len(active) == 0 {
		return nil, err
	}
	tree, err := loadCategoryTree(schema)
	if err != nil {
		return nil, err
	}
	gaps, err := itemDeclarationGaps(schema, itemCode, item, channel, active, tree)
	if err != nil {
		return nil, err
	}
	var out []string
	for _, g := range gaps {
		out = append(out, fmt.Sprintf("declarations (%s): %s", g.Pack.Label, strings.Join(g.Missing, ", ")))
	}
	return out, nil
}

// ensureDeclarationPackAttributes adds the pack's attributes the client does not
// have yet, when its DeclarationPack record is approved. An attribute the client
// already has under the same code is left as it is.
func ensureDeclarationPackAttributes(tenantID, docID string, data map[string]interface{}) {
	pack, ok := declarationPacks[strField(data, "pack")]
	if !ok {
		return
	}
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return
	}
	var made []string
	for _, f := range pack.Fields {
		if f.Code == "" {
			continue
		}
		attr := map[string]interface{}{"code": f.Code, "label": f.Label, "value_type": "Data", "status": "Active", "used_for": "Product information"}
		if len(f.Values) > 0 {
			attr["value_type"], attr["value_options"] = "Select", strings.Join(f.Values, ",")
		}
		raw, _ := json.Marshal(attr)
		r, err := db.DB.Exec(fmt.Sprintf(`INSERT INTO %s.documents (id, doctype, data, status, created_by)
			SELECT $1::text, 'ProductAttributeDef', $2, 'Active', 'system'
			WHERE NOT EXISTS (SELECT 1 FROM %s.documents WHERE doctype = 'ProductAttributeDef' AND deleted_at IS NULL
			                    AND (id = $1::text OR upper(COALESCE(data->>'code','')) = $1::text))
			ON CONFLICT DO NOTHING`, schema, schema), f.Code, raw)
		if err != nil {
			LogSystemError(tenantID, "", "ERROR", "ensureDeclarationPackAttributes", fmt.Sprintf("%s %s: %v", docID, f.Code, err), "")
			continue
		}
		if n, _ := r.RowsAffected(); n > 0 {
			made = append(made, f.Code)
		}
	}
	added := "none"
	if len(made) > 0 {
		added = strings.Join(made, ", ")
	}
	LogAuditEvent(tenantID, "system", "PIM", "DECLARATION_PACK_APPROVED", fmt.Sprintf("%s: pack %s on category %s; attributes added: %s",
		docID, pack.ID, strField(data, "category"), added))
}

// DeclarationGapRow is one row of the Declaration gaps report.
type DeclarationGapRow struct {
	ItemCode string `json:"item_code"`
	ItemName string `json:"item_name"`
	Category string `json:"category"`
	Pack     string `json:"pack"`
	Channel  string `json:"channel"`
	Missing  string `json:"missing"`
	Record   string `json:"record"`
}

// ListDeclarationGaps lists every item that lacks a required field of a pack
// in date for its category (category narrows it to one category's subtree).
func ListDeclarationGaps(tenantID, category string) ([]DeclarationGapRow, error) {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return nil, err
	}
	out := []DeclarationGapRow{}
	active, err := activeDeclarationAssignments(schema)
	if err != nil || len(active) == 0 {
		return out, err
	}
	tree, err := loadCategoryTree(schema)
	if err != nil {
		return nil, err
	}
	// Candidate categories: every category under a pack's own one.
	var ids, names []string
	for c := range tree.known {
		for _, a := range active {
			if tree.within(c, a.Category) && (category == "" || tree.within(c, category)) {
				ids = append(ids, c)
				break
			}
		}
	}
	for name, id := range tree.byName {
		for _, c := range ids {
			if c == id {
				names = append(names, name)
				break
			}
		}
	}
	if len(ids) == 0 {
		return out, nil
	}
	rows, err := db.DB.Query(fmt.Sprintf(`SELECT id, data FROM %s.documents
		WHERE doctype = 'Item' AND deleted_at IS NULL
		  AND (data->>'category' = ANY($1) OR lower(COALESCE(data->>'category','')) = ANY($2))
		ORDER BY id LIMIT 20000`, schema), pq.Array(ids), pq.Array(names))
	if err != nil {
		return nil, err
	}
	type itemRow struct {
		id   string
		data map[string]interface{}
	}
	var items []itemRow
	for rows.Next() {
		var id, raw string
		if err := rows.Scan(&id, &raw); err != nil {
			rows.Close()
			return nil, err
		}
		var d map[string]interface{}
		if json.Unmarshal([]byte(raw), &d) == nil {
			items = append(items, itemRow{id, d})
		}
	}
	rows.Close()
	for _, it := range items {
		gaps, err := itemDeclarationGaps(schema, it.id, it.data, "", active, tree)
		if err != nil {
			return nil, err
		}
		for _, g := range gaps {
			channel := g.Channel
			if channel == "" {
				channel = "Every channel"
			}
			out = append(out, DeclarationGapRow{ItemCode: it.id, ItemName: strField(it.data, "name"), Category: strField(it.data, "category"),
				Pack: g.Pack.Label, Channel: channel, Missing: strings.Join(g.Missing, ", "), Record: g.Record})
		}
	}
	return out, nil
}

func init() {
	RegisterReport(ReportDefinition{ID: "declaration-gaps", Label: "Declaration gaps", Category: "PIM",
		Columns: []ReportColumn{
			{Key: "item_code", Label: "Item"}, {Key: "item_name", Label: "Name"}, {Key: "category", Label: "Category"},
			{Key: "pack", Label: "Pack"}, {Key: "channel", Label: "Channel"}, {Key: "missing", Label: "Missing"},
			{Key: "record", Label: "Turned on by"},
		},
		Params: []ReportParam{{Key: "category", Label: "Category", Type: "text"}},
		Run: func(tenantID string, params map[string]string) ([]map[string]interface{}, error) {
			rows, err := ListDeclarationGaps(tenantID, params["category"])
			if err != nil {
				return nil, err
			}
			return structsToRows(rows)
		}})
}
