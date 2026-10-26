package engines

import (
	"context"
	"custom_erp/db"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"strconv"
	"strings"
	"time"
)

// Stage 101.2.1 - the segment builder (the blueprint's M09), and 101.2.3
// static lists. A segment is a tree of AND / OR groups (any group may be
// NOT) of rules over the customer facts crmFactsCTE computes - profile,
// spend, visits, last purchase, tier, lifecycle, RFM, birthday month,
// consent, store and category / brand bought or not bought in the last N
// days - with day windows counted in the client's time zone. Saving a
// changed definition makes a new version; a campaign uses one exact version
// and a snapshot of who was in it when it went out.
//
// The blueprint's examples hold by construction: spend and averages come
// from bills (a bill with three lines is one visit), an average ignores a
// customer with no bills (100, blank and 200 average 150), and someone who
// never bought is "Never bought", not the lowest spender.

// CRMSegmentRule is one condition, or a nested group when Group is set.
type CRMSegmentRule struct {
	Field    string           `json:"field,omitempty"`
	Cmp      string           `json:"cmp,omitempty"`
	Value    interface{}      `json:"value,omitempty"`
	Value2   interface{}      `json:"value2,omitempty"`
	Days     int              `json:"days,omitempty"`
	Category string           `json:"category,omitempty"`
	Brand    string           `json:"brand,omitempty"`
	Item     string           `json:"item,omitempty"`
	Store    string           `json:"store,omitempty"`
	Group    *CRMSegmentGroup `json:"group,omitempty"`
}

// CRMSegmentGroup is AND / OR over rules, optionally negated.
type CRMSegmentGroup struct {
	Op    string           `json:"op"` // and, or
	Not   bool             `json:"not,omitempty"`
	Rules []CRMSegmentRule `json:"rules"`
}

// CRMSegmentFields describes every field the builder offers.
var CRMSegmentFields = []struct {
	Field, Label, Kind string
	Options            []string `json:",omitempty"`
}{
	{"spend", "Total spend (net of returns)", "number", nil},
	{"visits", "Number of bills", "number", nil},
	{"avg_bill", "Average bill", "number", nil},
	{"returns", "Number of returns", "number", nil},
	{"points", "Points they can use", "number", nil},
	{"days_since", "Days since last bill", "number", nil},
	{"customer_days", "Days since the customer was added", "number", nil},
	{"lifecycle", "Lifecycle", "select", CRMLifecycleStates},
	{"rfm", "RFM segment", "select", []string{"Champions", "Loyal Customers", "Potential Loyalists", "At Risk", "Lost"}},
	{"tier", "Loyalty tier", "text", nil},
	{"gender", "Gender", "select", []string{"Female", "Male", "Other"}},
	{"city", "City", "text", nil},
	{"state", "State", "text", nil},
	{"tags", "Has tag", "text", nil},
	{"preferred_store", "Preferred store", "text", nil},
	{"last_store", "Last bought at store", "text", nil},
	{"birthday_month", "Birthday month (1-12, 0 = this month)", "number", nil},
	{"anniversary_month", "Anniversary month (1-12, 0 = this month)", "number", nil},
	{"consent", "Agrees to offers by", "select", crmChannels},
	{"has_contact", "Has", "select", []string{"phone", "email"}},
	{"bought", "Bought in the last N days (optionally a category, brand, item or store)", "bought", nil},
	{"not_bought", "Did not buy in the last N days (optionally a category, brand, item or store)", "bought", nil},
	{"in_list", "Is on static list", "list", nil},
}

func crmSegmentFieldKind(f string) string {
	for _, d := range CRMSegmentFields {
		if d.Field == f {
			return d.Kind
		}
	}
	return ""
}

type crmSQLBuilder struct {
	schema string
	zone   string
	args   []interface{}
	start  int // first placeholder number
}

func (b *crmSQLBuilder) arg(v interface{}) string {
	b.args = append(b.args, v)
	return fmt.Sprintf("$%d", b.start+len(b.args)-1)
}

func crmNumber(v interface{}) (float64, bool) {
	switch t := v.(type) {
	case float64:
		return t, !math.IsNaN(t) && !math.IsInf(t, 0)
	case int:
		return float64(t), true
	case string:
		f, err := strconv.ParseFloat(strings.TrimSpace(strings.ReplaceAll(t, ",", "")), 64)
		return f, err == nil
	}
	return 0, false
}

var crmNumericCmp = map[string]string{">": ">", ">=": ">=", "<": "<", "<=": "<=", "=": "=", "!=": "<>"}

// compileGroup turns a group into a SQL predicate over cf (the facts alias).
func (b *crmSQLBuilder) compileGroup(g *CRMSegmentGroup, depth int) (string, error) {
	if depth > 4 {
		return "", crmErr(CRMCodeSegment, "groups can be nested at most 4 deep")
	}
	if len(g.Rules) == 0 {
		return "TRUE", nil
	}
	if len(g.Rules) > 40 {
		return "", crmErr(CRMCodeSegment, "a group can hold at most 40 rules")
	}
	joiner := " AND "
	switch strings.ToLower(strings.TrimSpace(g.Op)) {
	case "or":
		joiner = " OR "
	case "and", "":
	default:
		return "", crmErr(CRMCodeSegment, "a group is AND or OR")
	}
	parts := make([]string, 0, len(g.Rules))
	for i := range g.Rules {
		p, err := b.compileRule(&g.Rules[i], depth)
		if err != nil {
			return "", err
		}
		parts = append(parts, "("+p+")")
	}
	out := "(" + strings.Join(parts, joiner) + ")"
	if g.Not {
		out = "NOT COALESCE(" + out + ", FALSE)"
	}
	return out, nil
}

func (b *crmSQLBuilder) compileRule(r *CRMSegmentRule, depth int) (string, error) {
	if r.Group != nil {
		return b.compileGroup(r.Group, depth+1)
	}
	kind := crmSegmentFieldKind(r.Field)
	if kind == "" {
		return "", crmErr(CRMCodeSegment, "%q is not a field the builder knows", r.Field)
	}
	switch r.Field {
	case "spend", "visits", "avg_bill", "returns", "points", "days_since", "customer_days":
		col := map[string]string{"customer_days": "(LOCALTIMESTAMP::date - public.erp_local_date(cf.created_at))"}[r.Field]
		if col == "" {
			col = "cf." + r.Field
		}
		if r.Cmp == "between" {
			lo, ok1 := crmNumber(r.Value)
			hi, ok2 := crmNumber(r.Value2)
			if !ok1 || !ok2 {
				return "", crmErr(CRMCodeSegment, "%s between needs two numbers", r.Field)
			}
			return fmt.Sprintf("%s BETWEEN %s AND %s", col, b.arg(lo), b.arg(hi)), nil
		}
		op, ok := crmNumericCmp[r.Cmp]
		n, okN := crmNumber(r.Value)
		if !ok || !okN {
			return "", crmErr(CRMCodeSegment, "%s needs a comparison (>, >=, <, <=, =, !=, between) and a number", r.Field)
		}
		return fmt.Sprintf("COALESCE(%s %s %s, FALSE)", col, op, b.arg(n)), nil
	case "lifecycle", "rfm", "gender", "tier", "city", "state", "preferred_store", "last_store":
		v := crmString(r.Value)
		if v == "" {
			return "", crmErr(CRMCodeSegment, "%s needs a value", r.Field)
		}
		expr := fmt.Sprintf("LOWER(cf.%s) = LOWER(%s)", r.Field, b.arg(v))
		if r.Cmp == "!=" {
			expr = "NOT (" + expr + ")"
		}
		return expr, nil
	case "tags":
		v := crmString(r.Value)
		if v == "" {
			return "", crmErr(CRMCodeSegment, "choose a tag")
		}
		return fmt.Sprintf("(',' || LOWER(regexp_replace(cf.tags, '\\s*,\\s*', ',', 'g')) || ',') LIKE '%%,' || LOWER(%s) || ',%%'", b.arg(strings.TrimSpace(v))), nil
	case "birthday_month", "anniversary_month":
		col := "cf.dob"
		if r.Field == "anniversary_month" {
			col = "cf.anniversary"
		}
		m, ok := crmNumber(r.Value)
		if !ok || m < 0 || m > 12 {
			return "", crmErr(CRMCodeSegment, "a month is 1-12 (0 = this month)")
		}
		if m == 0 {
			return fmt.Sprintf("EXTRACT(MONTH FROM %s) = EXTRACT(MONTH FROM %s)", col, crmLocalTodaySQL(b.zone)), nil
		}
		return fmt.Sprintf("EXTRACT(MONTH FROM %s) = %s", col, b.arg(int(m))), nil
	case "consent":
		tick := crmConsentTick(crmChannel(crmString(r.Value)))
		if tick == "" {
			return "", crmErr(CRMCodeSegment, "choose SMS, WhatsApp or Email")
		}
		return fmt.Sprintf("LOWER(COALESCE(cf.data->>'%s', '')) IN ('true', '1', 'yes', 'on')", tick), nil
	case "has_contact":
		if crmString(r.Value) == "email" {
			return "cf.email LIKE '%_@_%'", nil
		}
		return "length(regexp_replace(cf.phone, '[^0-9]', '', 'g')) >= 8", nil
	case "bought", "not_bought":
		cond := []string{"b.doctype = 'POSCart'", "b.status = 'Paid'", "b.deleted_at IS NULL", "b.data->>'customer_id' = cf.id"}
		if r.Days > 0 {
			cond = append(cond, fmt.Sprintf("%s::date > %s - %s::int", crmDocLocalSQL("b.created_at", b.zone), crmLocalTodaySQL(b.zone), b.arg(r.Days)))
		}
		if s := strings.TrimSpace(r.Store); s != "" {
			cond = append(cond, fmt.Sprintf("b.data->>'location' = %s", b.arg(s)))
		}
		var line []string
		if v := strings.TrimSpace(r.Category); v != "" {
			line = append(line, fmt.Sprintf("LOWER(i.data->>'category') = LOWER(%s)", b.arg(v)))
		}
		if v := strings.TrimSpace(r.Brand); v != "" {
			line = append(line, fmt.Sprintf("LOWER(i.data->>'brand') = LOWER(%s)", b.arg(v)))
		}
		if v := strings.TrimSpace(r.Item); v != "" {
			line = append(line, fmt.Sprintf("(li->>'sku' = %[1]s OR i.data->>'item_name' = %[1]s OR i.data->>'name' = %[1]s)", b.arg(v)))
		}
		if len(line) > 0 {
			cond = append(cond, fmt.Sprintf(`EXISTS (SELECT 1 FROM jsonb_array_elements(%s) li JOIN %s.documents i ON i.doctype = 'Item' AND i.deleted_at IS NULL
				AND i.data->>'code' = li->>'sku' WHERE %s)`, crmItemsSQL("b"), b.schema, strings.Join(line, " AND ")))
		}
		exists := fmt.Sprintf("EXISTS (SELECT 1 FROM %s.documents b WHERE %s)", b.schema, strings.Join(cond, " AND "))
		if r.Field == "not_bought" {
			return "NOT " + exists, nil
		}
		return exists, nil
	case "in_list":
		id := crmString(r.Value)
		if id == "" {
			return "", crmErr(CRMCodeSegment, "choose a list")
		}
		return fmt.Sprintf(`EXISTS (SELECT 1 FROM %[1]s.crm_static_list_members m JOIN %[1]s.crm_static_lists l ON l.id = m.list_id
			WHERE m.list_id = %[2]s AND m.customer_id = cf.id AND (l.expires_on IS NULL OR l.expires_on >= %[3]s))`, b.schema, b.arg(id), crmLocalTodaySQL(b.zone)), nil
	}
	return "", crmErr(CRMCodeSegment, "%q is not supported", r.Field)
}

// crmCompileSegment compiles a definition; start is the first $n number.
func crmCompileSegment(schema string, g *CRMSegmentGroup, start int) (string, []interface{}, error) {
	b := &crmSQLBuilder{schema: schema, zone: crmZoneName(schema), start: start}
	pred, err := b.compileGroup(g, 0)
	return pred, b.args, err
}

// CRMSegment is a saved segment and its current definition.
type CRMSegment struct {
	ID             string          `json:"id"`
	Name           string          `json:"name"`
	Description    string          `json:"description,omitempty"`
	CurrentVersion int             `json:"current_version"`
	Status         string          `json:"status"`
	Definition     CRMSegmentGroup `json:"definition"`
	UpdatedAt      time.Time       `json:"updated_at"`
	CreatedBy      string          `json:"created_by,omitempty"`
}

// SaveCRMSegment creates (id "") or saves a segment; a changed definition is
// a new version, an unchanged one keeps the version.
func SaveCRMSegment(tenantID, id, name, description string, def CRMSegmentGroup, actor string) (*CRMSegment, error) {
	schema, err := crmSchema(tenantID)
	if err != nil {
		return nil, err
	}
	name = strings.TrimSpace(name)
	if name == "" {
		return nil, crmErr(CRMCodeSegment, "name the segment")
	}
	if _, _, err := crmCompileSegment(schema, &def, 1); err != nil {
		return nil, err
	}
	defJSON := crmJSON(def)
	tx, err := db.DB.Begin()
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	if id == "" {
		id = NewDocID("SEG")
		if _, err := tx.Exec(fmt.Sprintf(`INSERT INTO %s.crm_segments (id, name, description, current_version, created_by) VALUES ($1, $2, NULLIF($3, ''), 1, $4)`, schema),
			id, name, strings.TrimSpace(description), actor); err != nil {
			return nil, err
		}
		if _, err := tx.Exec(fmt.Sprintf(`INSERT INTO %s.crm_segment_versions (segment_id, version, definition, created_by) VALUES ($1, 1, $2, $3)`, schema), id, defJSON, actor); err != nil {
			return nil, err
		}
	} else {
		var cur int
		var curDef []byte
		err := tx.QueryRow(fmt.Sprintf(`SELECT s.current_version, v.definition FROM %s.crm_segments s JOIN %s.crm_segment_versions v ON v.segment_id = s.id AND v.version = s.current_version
			WHERE s.id = $1 FOR UPDATE OF s`, schema, schema), id).Scan(&cur, &curDef)
		if errors.Is(err, sql.ErrNoRows) {
			return nil, crmNotFound("segment", id)
		}
		if err != nil {
			return nil, err
		}
		var old CRMSegmentGroup
		_ = json.Unmarshal(curDef, &old)
		if string(crmJSON(old)) != string(defJSON) {
			cur++
			if _, err := tx.Exec(fmt.Sprintf(`INSERT INTO %s.crm_segment_versions (segment_id, version, definition, created_by) VALUES ($1, $2, $3, $4)`, schema), id, cur, defJSON, actor); err != nil {
				return nil, err
			}
		}
		if _, err := tx.Exec(fmt.Sprintf(`UPDATE %s.crm_segments SET name = $2, description = NULLIF($3, ''), current_version = $4, updated_at = NOW() WHERE id = $1`, schema),
			id, name, strings.TrimSpace(description), cur); err != nil {
			return nil, err
		}
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return GetCRMSegment(tenantID, id, 0)
}

// GetCRMSegment returns a segment at a version (0 = current).
func GetCRMSegment(tenantID, id string, version int) (*CRMSegment, error) {
	schema, err := crmSchema(tenantID)
	if err != nil {
		return nil, err
	}
	return crmGetSegment(schema, id, version)
}

func crmGetSegment(schema, id string, version int) (*CRMSegment, error) {
	var s CRMSegment
	var def []byte
	err := db.DB.QueryRow(fmt.Sprintf(`SELECT s.id, s.name, COALESCE(s.description, ''), s.current_version, s.status, s.updated_at, COALESCE(s.created_by, ''), v.definition
		FROM %s.crm_segments s JOIN %s.crm_segment_versions v ON v.segment_id = s.id AND v.version = CASE WHEN $2 > 0 THEN $2 ELSE s.current_version END
		WHERE s.id = $1`, schema, schema), id, version).Scan(&s.ID, &s.Name, &s.Description, &s.CurrentVersion, &s.Status, &s.UpdatedAt, &s.CreatedBy, &def)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, crmNotFound("segment", id)
	}
	if err != nil {
		return nil, err
	}
	_ = json.Unmarshal(def, &s.Definition)
	return &s, nil
}

// ListCRMSegments lists segments.
func ListCRMSegments(tenantID string) ([]CRMSegment, error) {
	schema, err := crmSchema(tenantID)
	if err != nil {
		return nil, err
	}
	rows, err := db.DB.Query(fmt.Sprintf(`SELECT id FROM %s.crm_segments WHERE status = 'Active' ORDER BY updated_at DESC`, schema))
	if err != nil {
		return nil, err
	}
	var ids []string
	for rows.Next() {
		var id string
		if rows.Scan(&id) == nil {
			ids = append(ids, id)
		}
	}
	rows.Close()
	out := []CRMSegment{}
	for _, id := range ids {
		s, err := crmGetSegment(schema, id, 0)
		if err != nil {
			return nil, err
		}
		out = append(out, *s)
	}
	return out, nil
}

// ArchiveCRMSegment hides a segment; its versions and snapshots stay.
func ArchiveCRMSegment(tenantID, id string) error {
	schema, err := crmSchema(tenantID)
	if err != nil {
		return err
	}
	_, err = db.DB.Exec(fmt.Sprintf(`UPDATE %s.crm_segments SET status = 'Archived', updated_at = NOW() WHERE id = $1`, schema), id)
	return err
}

// CRMSegmentCount is the live count and its waterfall for one channel.
type CRMSegmentCount struct {
	Matched   int                 `json:"matched"`
	Channel   string              `json:"channel,omitempty"`
	NoConsent int                 `json:"no_consent"`
	NoContact int                 `json:"no_contact"`
	Final     int                 `json:"final"`
	Lifecycle []CRMLifecycleCount `json:"lifecycle"`
	Sample    []CRMCustomerRow    `json:"sample"`
	TimeZone  string              `json:"time_zone"`
	CountedAt time.Time           `json:"counted_at"`
}

// CountCRMSegment counts a definition (saved or not): who matches, and for a
// channel how many of them lack consent or a contact - the waterfall a
// campaign shows before it is sent.
func CountCRMSegment(tenantID, role string, def CRMSegmentGroup, channel string) (*CRMSegmentCount, error) {
	schema, err := crmSchema(tenantID)
	if err != nil {
		return nil, err
	}
	pred, args, err := crmCompileSegment(schema, &def, 1)
	if err != nil {
		return nil, err
	}
	th := crmLifecycleThresholds(schema)
	out := &CRMSegmentCount{Channel: crmChannel(channel), TimeZone: crmZoneName(schema), CountedAt: time.Now(), Sample: []CRMCustomerRow{}}
	base := crmFactsCTE(schema, th, false)
	consent, contact := "TRUE", "TRUE"
	if out.Channel != "" {
		consent = fmt.Sprintf("LOWER(COALESCE(data->>'%s', '')) IN ('true', '1', 'yes', 'on')", crmConsentTick(out.Channel))
		if out.Channel == CRMChannelEmail {
			contact = "email LIKE '%_@_%'"
		} else {
			contact = "length(regexp_replace(phone, '[^0-9]', '', 'g')) >= 8"
		}
	}
	err = db.DB.QueryRow(base+fmt.Sprintf(`, m AS (SELECT * FROM cf WHERE %s)
		SELECT COUNT(*), COUNT(*) FILTER (WHERE NOT (%s)), COUNT(*) FILTER (WHERE (%s) AND NOT (%s)) FROM m`, pred, consent, consent, contact), args...).
		Scan(&out.Matched, &out.NoConsent, &out.NoContact)
	if err != nil {
		return nil, err
	}
	out.Final = out.Matched - out.NoConsent - out.NoContact
	rows, err := db.DB.Query(base+fmt.Sprintf(` SELECT lifecycle, COUNT(*) FROM cf WHERE %s GROUP BY lifecycle`, pred), args...)
	if err != nil {
		return nil, err
	}
	counts := map[string]int{}
	for rows.Next() {
		var s string
		var n int
		if rows.Scan(&s, &n) == nil {
			counts[s] = n
		}
	}
	rows.Close()
	for _, s := range CRMLifecycleStates {
		out.Lifecycle = append(out.Lifecycle, CRMLifecycleCount{State: s, Count: counts[s]})
	}
	rows, err = db.DB.Query(base+fmt.Sprintf(` SELECT id FROM cf WHERE %s ORDER BY spend DESC, id LIMIT 10`, pred), args...)
	if err != nil {
		return nil, err
	}
	var ids []string
	for rows.Next() {
		var id string
		if rows.Scan(&id) == nil {
			ids = append(ids, id)
		}
	}
	rows.Close()
	masked := !RoleSeesContacts(tenantID, role)
	for _, id := range ids {
		if r, err := crmCustomerRow(schema, id, masked); err == nil {
			out.Sample = append(out.Sample, r)
		}
	}
	return out, nil
}

// CRMWhyMatched is, rule by rule, whether one customer passes.
type CRMWhyMatched struct {
	Matched bool              `json:"matched"`
	Rules   []CRMWhyRuleState `json:"rules"`
}

type CRMWhyRuleState struct {
	Depth   int    `json:"depth"`
	Text    string `json:"text"`
	Matched bool   `json:"matched"`
}

// ExplainCRMSegment says why a customer is (or is not) in a segment.
func ExplainCRMSegment(tenantID string, def CRMSegmentGroup, customerID string) (*CRMWhyMatched, error) {
	schema, err := crmSchema(tenantID)
	if err != nil {
		return nil, err
	}
	c, err := crmLoadCustomer(schema, customerID)
	if err != nil {
		return nil, crmNotFound("customer", customerID)
	}
	type item struct {
		depth int
		text  string
		pred  string
	}
	b := &crmSQLBuilder{schema: schema, zone: crmZoneName(schema), start: 2}
	var items []item
	var walk func(g *CRMSegmentGroup, depth int) error
	walk = func(g *CRMSegmentGroup, depth int) error {
		pred, err := b.compileGroup(g, depth)
		if err != nil {
			return err
		}
		op := strings.ToUpper(firstNonEmpty(g.Op, "and"))
		label := map[bool]string{true: "NOT ", false: ""}[g.Not] + map[string]string{"AND": "All of", "OR": "Any of"}[op]
		items = append(items, item{depth, label, pred})
		for i := range g.Rules {
			r := &g.Rules[i]
			if r.Group != nil {
				if err := walk(r.Group, depth+1); err != nil {
					return err
				}
				continue
			}
			p, err := b.compileRule(r, depth)
			if err != nil {
				return err
			}
			items = append(items, item{depth + 1, crmRuleText(r), p})
		}
		return nil
	}
	if err := walk(&def, 0); err != nil {
		return nil, err
	}
	cols := make([]string, len(items))
	for i, it := range items {
		cols[i] = "COALESCE((" + it.pred + "), FALSE)"
	}
	th := crmLifecycleThresholds(schema)
	args := append([]interface{}{c.ID}, b.args...)
	vals := make([]bool, len(items))
	dest := make([]interface{}, len(items))
	for i := range vals {
		dest[i] = &vals[i]
	}
	err = db.DB.QueryRow(crmFactsCTE(schema, th, false)+` SELECT `+strings.Join(cols, ", ")+` FROM cf WHERE id = $1`, args...).Scan(dest...)
	if errors.Is(err, sql.ErrNoRows) {
		return &CRMWhyMatched{Rules: []CRMWhyRuleState{{Text: "This customer is not counted in segments (merged, erased or a store's walk-in record)"}}}, nil
	}
	if err != nil {
		return nil, err
	}
	out := &CRMWhyMatched{Matched: vals[0]}
	for i, it := range items {
		out.Rules = append(out.Rules, CRMWhyRuleState{Depth: it.depth, Text: it.text, Matched: vals[i]})
	}
	return out, nil
}

func crmRuleText(r *CRMSegmentRule) string {
	label := r.Field
	for _, f := range CRMSegmentFields {
		if f.Field == r.Field {
			label = f.Label
		}
	}
	switch r.Field {
	case "bought", "not_bought":
		parts := []string{map[bool]string{true: "Bought", false: "Did not buy"}[r.Field == "bought"]}
		for _, kv := range [][2]string{{"category", r.Category}, {"brand", r.Brand}, {"item", r.Item}, {"at store", r.Store}} {
			if kv[1] != "" {
				parts = append(parts, kv[0]+" "+kv[1])
			}
		}
		if r.Days > 0 {
			parts = append(parts, fmt.Sprintf("in the last %d days", r.Days))
		} else {
			parts = append(parts, "ever")
		}
		return strings.Join(parts, " ")
	case "consent", "has_contact", "in_list":
		return label + " " + crmString(r.Value)
	}
	if r.Cmp == "between" {
		return fmt.Sprintf("%s between %s and %s", label, crmString(r.Value), crmString(r.Value2))
	}
	return strings.TrimSpace(fmt.Sprintf("%s %s %s", label, firstNonEmpty(r.Cmp, "="), crmString(r.Value)))
}

// crmSnapshotSegment freezes who is in a segment version now (inside the
// caller's transaction) and returns the snapshot id and its size.
func crmSnapshotSegment(tx *sql.Tx, schema, segmentID string, version int, purpose, refID string) (string, int, error) {
	seg, err := crmGetSegment(schema, segmentID, version)
	if err != nil {
		return "", 0, err
	}
	pred, args, err := crmCompileSegment(schema, &seg.Definition, 2)
	if err != nil {
		return "", 0, err
	}
	id := NewDocID("SNAP")
	if _, err := tx.Exec(fmt.Sprintf(`INSERT INTO %s.crm_segment_snapshots (id, segment_id, version, purpose, ref_id) VALUES ($1, $2, $3, $4, $5)`, schema),
		id, segmentID, seg.CurrentVersion, purpose, refID); err != nil {
		return "", 0, err
	}
	if version > 0 {
		if _, err := tx.Exec(fmt.Sprintf(`UPDATE %s.crm_segment_snapshots SET version = $2 WHERE id = $1`, schema), id, version); err != nil {
			return "", 0, err
		}
	}
	th := crmLifecycleThresholds(schema)
	res, err := tx.Exec(crmFactsCTE(schema, th, false)+fmt.Sprintf(` INSERT INTO %s.crm_segment_snapshot_members (snapshot_id, customer_id) SELECT $1, id FROM cf WHERE %s`, schema, pred),
		append([]interface{}{id}, args...)...)
	if err != nil {
		return "", 0, err
	}
	n, _ := res.RowsAffected()
	if _, err := tx.Exec(fmt.Sprintf(`UPDATE %s.crm_segment_snapshots SET member_count = $2 WHERE id = $1`, schema), id, n); err != nil {
		return "", 0, err
	}
	return id, int(n), nil
}

// --- static lists (101.2.3) ---------------------------------------------------------

// CRMStaticList is an uploaded phone list.
type CRMStaticList struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	Source    string    `json:"source"`
	ExpiresOn string    `json:"expires_on,omitempty"`
	Rows      int       `json:"rows"`
	Matched   int       `json:"matched"`
	Expired   bool      `json:"expired"`
	CreatedBy string    `json:"created_by,omitempty"`
	CreatedAt time.Time `json:"created_at"`
}

// CRMStaticListUpload is what an upload did.
type CRMStaticListUpload struct {
	List   *CRMStaticList       `json:"list"`
	Errors []RowValidationError `json:"errors"`
}

// UploadCRMStaticList reads a CSV with a phone column (the import's own CSV
// reader and row-error shape; a list row is not a record, so BulkImportCSV's
// document writes do not apply), keeps where it came from and when it lapses,
// and matches each number to a customer. A list never grants consent: its
// members are still checked at send time.
func UploadCRMStaticList(tenantID, name, source, expiresOn string, r io.Reader, actor string) (*CRMStaticListUpload, error) {
	name, source = strings.TrimSpace(name), strings.TrimSpace(source)
	if name == "" || source == "" {
		return nil, crmErr(CRMCodeList, "name the list and say where the numbers came from")
	}
	var expires interface{}
	if e := strings.TrimSpace(expiresOn); e != "" {
		t, err := time.Parse("2006-01-02", e)
		if err != nil {
			return nil, crmErr(CRMCodeList, "the expiry date is not a date (YYYY-MM-DD)")
		}
		expires = t.Format("2006-01-02")
	}
	records, err := readCSVRecordsContext(context.Background(), tenantID, r)
	if err != nil {
		return nil, crmErr(CRMCodeList, "%v", err)
	}
	if len(records) < 2 {
		return nil, crmErr(CRMCodeList, "the file needs a header row and at least one number")
	}
	col := -1
	for i, h := range records[0] {
		h = strings.ToLower(strings.TrimSpace(strings.TrimPrefix(h, string(rune(0xFEFF)))))
		if h == "phone" || h == "mobile" || h == "phone number" || h == "mobile number" {
			col = i
			break
		}
	}
	if col < 0 {
		return nil, crmErr(CRMCodeList, "no column named phone (or mobile)")
	}
	schema, err := crmSchema(tenantID)
	if err != nil {
		return nil, err
	}
	country := TenantPhoneCountry(tenantID)
	tx, err := db.DB.Begin()
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	id := NewDocID("LIST")
	if _, err := tx.Exec(fmt.Sprintf(`INSERT INTO %s.crm_static_lists (id, name, source, expires_on, created_by) VALUES ($1, $2, $3, $4, $5)`, schema),
		id, name, source, expires, actor); err != nil {
		return nil, err
	}
	out := &CRMStaticListUpload{Errors: []RowValidationError{}}
	rowsOK := 0
	for i, rec := range records[1:] {
		rowNo := i + 2
		if col >= len(rec) || strings.TrimSpace(rec[col]) == "" {
			continue
		}
		p := NormalizePhone(rec[col], country)
		if !p.Valid {
			if len(out.Errors) < 1000 {
				out.Errors = append(out.Errors, RowValidationError{RowNumber: rowNo, Message: firstNonEmpty(p.Reason, "not a phone number")})
			}
			continue
		}
		var cust sql.NullString
		_ = tx.QueryRow(fmt.Sprintf(`SELECT id FROM %s.documents WHERE doctype = 'Customer' AND deleted_at IS NULL AND status <> 'Merged'
			AND COALESCE(data->>'anonymized_at', '') = '' AND data->>'phone' = $1 ORDER BY created_at LIMIT 1`, schema), p.National).Scan(&cust)
		if _, err := tx.Exec(fmt.Sprintf(`INSERT INTO %s.crm_static_list_members (list_id, phone, customer_id) VALUES ($1, $2, $3) ON CONFLICT DO NOTHING`, schema),
			id, p.E164, cust); err != nil {
			return nil, err
		}
		rowsOK++
	}
	if _, err := tx.Exec(fmt.Sprintf(`UPDATE %s.crm_static_lists l SET row_count = (SELECT COUNT(*) FROM %s.crm_static_list_members WHERE list_id = l.id),
		matched_count = (SELECT COUNT(*) FROM %s.crm_static_list_members WHERE list_id = l.id AND customer_id IS NOT NULL) WHERE id = $1`, schema, schema, schema), id); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	lists, err := ListCRMStaticLists(tenantID)
	if err != nil {
		return nil, err
	}
	for i := range lists {
		if lists[i].ID == id {
			out.List = &lists[i]
		}
	}
	return out, nil
}

// ListCRMStaticLists lists uploaded lists.
func ListCRMStaticLists(tenantID string) ([]CRMStaticList, error) {
	schema, err := crmSchema(tenantID)
	if err != nil {
		return nil, err
	}
	rows, err := db.DB.Query(fmt.Sprintf(`SELECT id, name, source, COALESCE(to_char(expires_on, 'YYYY-MM-DD'), ''), row_count, matched_count,
		(expires_on IS NOT NULL AND expires_on < public.erp_today()), COALESCE(created_by, ''), created_at FROM %s.crm_static_lists ORDER BY created_at DESC`, schema))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []CRMStaticList{}
	for rows.Next() {
		var l CRMStaticList
		if err := rows.Scan(&l.ID, &l.Name, &l.Source, &l.ExpiresOn, &l.Rows, &l.Matched, &l.Expired, &l.CreatedBy, &l.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, l)
	}
	return out, rows.Err()
}
