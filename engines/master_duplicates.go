package engines

import (
	"custom_erp/db"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/lib/pq"
)

// Stage 47.13.5: duplicate suppliers and items, and which record of each
// group should be the golden one. Customers already have theirs
// (crm_merge.go, Stage 101.1.3); this is the same idea for the two masters a
// migration most often doubles up.
//
// Matching - normalised before comparing, so spacing, case and punctuation
// never hide a duplicate:
//   - Supplier: the same GSTIN; the same PAN under GSTINs of different states
//     (one business, two registrations - often right, worth a look); the same
//     phone (last 10 digits); the same email; the same name once case,
//     punctuation and company-form words (Pvt, Ltd, LLP...) are gone.
//   - Item: the same barcode; the same GTIN; the same name with the same HSN.
//
// Golden-record rules - deliberately NOT "the latest write wins", which is
// how a careless re-import silently becomes the truth. The record kept is,
// in order:
//  1. the one the business already runs on - most transactions pointing at
//     it (supplier: documents naming it as vendor; item: stock rows and
//     ledger lines), because moving history is the expensive, risky part;
//  2. then the one that passes the hard checks (supplier: a GSTIN whose check
//     digit is right; item: a barcode or GTIN on it);
//  3. then the most complete one (most filled fields);
//  4. then the one created first - the original, not the copy;
//  5. then the lowest id, so the answer never depends on row order.
//
// The report only proposes. Merging suppliers or items - moving references
// reversibly, with an audit trail - is not built here.

// MasterDuplicateRow is one record of one duplicate group.
type MasterDuplicateRow struct {
	Group        int
	ID           string
	Name         string
	Reasons      []string
	Golden       bool
	GoldenRule   string
	References   int64
	Completeness int
	Valid        bool
	CreatedAt    time.Time
}

type masterCandidate struct {
	id, name string
	keys     map[string]string // reason -> normalised key
	refs     int64
	filled   int
	valid    bool
	created  time.Time
}

var (
	dupNonAlnum   = regexp.MustCompile(`[^a-z0-9]+`)
	dupFormsWords = map[string]bool{"pvt": true, "private": true, "ltd": true, "limited": true, "llp": true, "inc": true,
		"co": true, "company": true, "the": true, "and": true, "m/s": true, "ms": true, "corp": true, "corporation": true}
)

// normalizeMasterName lower-cases, drops punctuation and company-form words,
// and joins what is left: "M/s. Sharma Textiles Pvt. Ltd." -> "sharma textiles".
func normalizeMasterName(s string) string {
	lower := strings.NewReplacer("&", " and ", "m/s", " ").Replace(strings.ToLower(s))
	words := strings.Fields(dupNonAlnum.ReplaceAllString(lower, " "))
	kept := words[:0]
	for _, w := range words {
		if !dupFormsWords[w] {
			kept = append(kept, w)
		}
	}
	return strings.Join(kept, " ")
}

// phoneDigitsKey is a phone's last ten digits - enough to match +91 98765
// 43210 with 098765-43210.
func phoneDigitsKey(s string) string {
	var b strings.Builder
	for _, r := range s {
		if r >= '0' && r <= '9' {
			b.WriteRune(r)
		}
	}
	d := b.String()
	if len(d) < 8 {
		return ""
	}
	if len(d) > 10 {
		d = d[len(d)-10:]
	}
	return d
}

func normalizeCodeKey(s string) string {
	return strings.ToUpper(strings.Join(strings.Fields(s), ""))
}

// FindDuplicateSuppliers groups suppliers that look like one business.
func FindDuplicateSuppliers(tenantID string) ([]MasterDuplicateRow, error) {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return nil, err
	}
	cands, err := loadMasterCandidates(schema, "Vendor",
		[]string{"name", "gstin", "contact_phone", "contact_email"})
	if err != nil {
		return nil, err
	}
	for _, c := range cands {
		raw := c.keys
		c.keys = map[string]string{}
		g := normalizeCodeKey(raw["gstin"])
		if g != "" {
			c.keys["Same GSTIN"] = g
			c.valid = gstinPattern.MatchString(g) && ValidGSTINChecksum(g)
			if len(g) == 15 {
				c.keys["Same PAN"] = g[2:12]
			}
		}
		if p := phoneDigitsKey(raw["contact_phone"]); p != "" {
			c.keys["Same phone"] = p
		}
		if e := strings.ToLower(strings.TrimSpace(raw["contact_email"])); strings.Contains(e, "@") {
			c.keys["Same email"] = e
		}
		if n := normalizeMasterName(raw["name"]); n != "" {
			c.keys["Same name"] = n
		}
	}
	if err := countMasterReferences(schema, cands, `SELECT data->>'vendor', COUNT(*) FROM %s.documents
		WHERE deleted_at IS NULL AND doctype <> 'Vendor' AND data->>'vendor' = ANY($1) GROUP BY 1`); err != nil {
		return nil, err
	}
	return groupMasterDuplicates(cands, "a GSTIN with a valid check digit"), nil
}

// FindDuplicateItems groups items that look like one product.
func FindDuplicateItems(tenantID string) ([]MasterDuplicateRow, error) {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return nil, err
	}
	cands, err := loadMasterCandidates(schema, "Item", []string{"name", "barcode", "gtin", "hsn_code"})
	if err != nil {
		return nil, err
	}
	for _, c := range cands {
		raw := c.keys
		c.keys = map[string]string{}
		if b := normalizeCodeKey(raw["barcode"]); b != "" {
			c.keys["Same barcode"] = b
		}
		if g := normalizeCodeKey(raw["gtin"]); g != "" {
			c.keys["Same GTIN"] = g
		}
		c.valid = c.keys["Same barcode"] != "" || c.keys["Same GTIN"] != ""
		if n := normalizeMasterName(raw["name"]); n != "" {
			c.keys["Same name and HSN"] = n + "|" + normalizeCodeKey(raw["hsn_code"])
		}
	}
	if err := countMasterReferences(schema, cands, `SELECT sku, COUNT(*) FROM (
		SELECT sku FROM %[1]s.inventory_availability WHERE sku = ANY($1)
		UNION ALL SELECT data->>'item_id' FROM %[1]s.documents WHERE doctype = 'StockLedgerEntry' AND data->>'item_id' = ANY($1)) r
		GROUP BY 1`); err != nil {
		return nil, err
	}
	return groupMasterDuplicates(cands, "a barcode or GTIN"), nil
}

// loadMasterCandidates reads a master's live records with the fields matched
// on (kept in keys until the caller normalises them).
func loadMasterCandidates(schema, doctype string, fields []string) ([]*masterCandidate, error) {
	cols := make([]string, len(fields))
	for i, f := range fields {
		cols[i] = fmt.Sprintf("COALESCE(data->>'%s', '')", f)
	}
	rows, err := db.DB.Query(fmt.Sprintf(`SELECT id, created_at,
		(SELECT COUNT(*) FROM jsonb_each_text(data) e WHERE TRIM(e.value) <> ''), %s
		FROM %s.documents WHERE doctype = $1 AND deleted_at IS NULL AND status NOT IN ('Merged', 'Cancelled')
		ORDER BY id`, strings.Join(cols, ", "), schema), doctype)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*masterCandidate
	for rows.Next() {
		c := &masterCandidate{keys: map[string]string{}}
		vals := make([]string, len(fields))
		dest := []interface{}{&c.id, &c.created, &c.filled}
		for i := range vals {
			dest = append(dest, &vals[i])
		}
		if err := rows.Scan(dest...); err != nil {
			return nil, err
		}
		for i, f := range fields {
			c.keys[f] = vals[i]
		}
		c.name = vals[0]
		out = append(out, c)
	}
	return out, rows.Err()
}

// countMasterReferences fills refs for the candidates that ended up in a
// group-able key, with one query over all of them.
func countMasterReferences(schema string, cands []*masterCandidate, query string) error {
	ids := make([]string, 0, len(cands))
	byID := map[string]*masterCandidate{}
	for _, c := range cands {
		ids = append(ids, c.id)
		byID[c.id] = c
	}
	if len(ids) == 0 {
		return nil
	}
	rows, err := db.DB.Query(fmt.Sprintf(query, schema), pq.Array(ids))
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var id string
		var n int64
		if err := rows.Scan(&id, &n); err != nil {
			return err
		}
		if c := byID[id]; c != nil {
			c.refs = n
		}
	}
	return rows.Err()
}

// groupMasterDuplicates joins candidates that share any key (union-find),
// names each pair's reasons and picks each group's golden record.
func groupMasterDuplicates(cands []*masterCandidate, validRule string) []MasterDuplicateRow {
	parent := make([]int, len(cands))
	for i := range parent {
		parent[i] = i
	}
	var find func(int) int
	find = func(i int) int {
		for parent[i] != i {
			parent[i] = parent[parent[i]]
			i = parent[i]
		}
		return i
	}
	reasons := make([]map[string]bool, len(cands))
	firstByKey := map[string]int{}
	reasonNames := map[string]bool{}
	for i, c := range cands {
		reasons[i] = map[string]bool{}
		for reason, key := range c.keys {
			reasonNames[reason] = true
			k := reason + "\x00" + key
			if j, ok := firstByKey[k]; ok {
				parent[find(i)] = find(j)
				reasons[i][reason], reasons[j][reason] = true, true
			} else {
				firstByKey[k] = i
			}
		}
	}
	groups := map[int][]int{}
	for i := range cands {
		if len(reasons[i]) > 0 {
			root := find(i)
			groups[root] = append(groups[root], i)
		}
	}
	roots := make([]int, 0, len(groups))
	for r := range groups {
		roots = append(roots, r)
	}
	// Biggest groups first, then by the lowest id in each - a stable order.
	sort.Slice(roots, func(a, b int) bool {
		ga, gb := groups[roots[a]], groups[roots[b]]
		if len(ga) != len(gb) {
			return len(ga) > len(gb)
		}
		return cands[ga[0]].id < cands[gb[0]].id
	})
	var out []MasterDuplicateRow
	for n, r := range roots {
		members := groups[r]
		sort.SliceStable(members, func(a, b int) bool { return goldenBefore(cands[members[a]], cands[members[b]]) })
		golden := cands[members[0]]
		runner := cands[members[1]]
		rule := goldenRule(golden, runner, validRule)
		for k, i := range members {
			c := cands[i]
			var rs []string
			for reason := range reasons[i] {
				if reason == "Same PAN" && reasons[i]["Same GSTIN"] && samePANOnlySameGSTIN(cands, members, i) {
					continue
				}
				rs = append(rs, reason)
			}
			sort.Strings(rs)
			row := MasterDuplicateRow{Group: n + 1, ID: c.id, Name: c.name, Reasons: rs, Golden: k == 0,
				References: c.refs, Completeness: c.filled, Valid: c.valid, CreatedAt: c.created}
			if k == 0 {
				row.GoldenRule = rule
			}
			out = append(out, row)
		}
	}
	return out
}

// samePANOnlySameGSTIN is true when every other member sharing i's PAN also
// shares its GSTIN - "Same PAN" says nothing new then, so it is not shown.
func samePANOnlySameGSTIN(cands []*masterCandidate, members []int, i int) bool {
	me := cands[i].keys
	for _, j := range members {
		if j == i {
			continue
		}
		o := cands[j].keys
		if o["Same PAN"] == me["Same PAN"] && o["Same GSTIN"] != me["Same GSTIN"] {
			return false
		}
	}
	return true
}

// goldenBefore orders a group by the golden-record rules above.
func goldenBefore(a, b *masterCandidate) bool {
	switch {
	case a.refs != b.refs:
		return a.refs > b.refs
	case a.valid != b.valid:
		return a.valid
	case a.filled != b.filled:
		return a.filled > b.filled
	case !a.created.Equal(b.created):
		return a.created.Before(b.created)
	}
	return a.id < b.id
}

// goldenRule names the rule that decided between the kept record and the
// next one.
func goldenRule(g, next *masterCandidate, validRule string) string {
	switch {
	case g.refs != next.refs:
		return fmt.Sprintf("Most transactions (%d against %d)", g.refs, next.refs)
	case g.valid != next.valid:
		return "Has " + validRule
	case g.filled != next.filled:
		return fmt.Sprintf("Most complete (%d fields against %d)", g.filled, next.filled)
	case !g.created.Equal(next.created):
		return "Created first"
	}
	return "Lowest id"
}

func masterDuplicateReportRows(rows []MasterDuplicateRow) []map[string]interface{} {
	out := make([]map[string]interface{}, 0, len(rows))
	for _, r := range rows {
		keep := "Merge into the kept record"
		if r.Golden {
			keep = "Keep"
		}
		out = append(out, map[string]interface{}{
			"group": r.Group, "id": r.ID, "name": r.Name, "reasons": strings.Join(r.Reasons, ", "),
			"proposal": keep, "rule": r.GoldenRule, "references": r.References, "completeness": r.Completeness,
			"created": r.CreatedAt.Format("2006-01-02"),
		})
	}
	return out
}

func init() {
	cols := []ReportColumn{{Key: "group", Label: "Group"}, {Key: "id", Label: "Code"}, {Key: "name", Label: "Name"},
		{Key: "reasons", Label: "Why it matches"}, {Key: "proposal", Label: "Proposal"}, {Key: "rule", Label: "Kept because"},
		{Key: "references", Label: "Transactions"}, {Key: "completeness", Label: "Filled fields"}, {Key: "created", Label: "Created"}}
	RegisterReport(ReportDefinition{ID: "duplicate-suppliers", Label: "Duplicate suppliers", Category: "Admin", Columns: cols, StoreKey: "-",
		Run: func(tenantID string, _ map[string]string) ([]map[string]interface{}, error) {
			rows, err := FindDuplicateSuppliers(tenantID)
			if err != nil {
				return nil, err
			}
			return masterDuplicateReportRows(rows), nil
		}})
	RegisterReport(ReportDefinition{ID: "duplicate-items", Label: "Duplicate items", Category: "Admin", Columns: cols, StoreKey: "-",
		Run: func(tenantID string, _ map[string]string) ([]map[string]interface{}, error) {
			rows, err := FindDuplicateItems(tenantID)
			if err != nil {
				return nil, err
			}
			return masterDuplicateReportRows(rows), nil
		}})
}
