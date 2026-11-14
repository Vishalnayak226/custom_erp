package engines

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	"custom_erp/db"

	"github.com/lib/pq"
)

// Assortment / ranging: which items a store or a channel carries. A store
// that has at least one Active Assortment row is "ranged": an item outside
// its range is warned or refused on a new purchase order for the store, a
// transfer into it and a sale there, as inventory.assortment_enforce says
// (off / warn / block, default off). A store with no rows is never touched.
// A row names an item or a whole category and may carry a date window; a row
// outside its window does not admit anything but still marks the store ranged.

const (
	AssortmentSettingKey = "inventory.assortment_enforce"
	AssortmentOff        = "off"
	AssortmentWarn       = "warn"
	AssortmentBlock      = "block"

	ItemExceptionOutsideRange = "Outside assortment"
)

// Error codes, PIM-9240 to PIM-9249.
const (
	assortmentCodePO       = "PIM-9240"
	assortmentCodeTransfer = "PIM-9241"
	assortmentCodeSale     = "PIM-9242"
	assortmentCodeChannel  = "PIM-9243"
	assortmentCodeShape    = "PIM-9244"
	assortmentCodeBulk     = "PIM-9245"
)

// AssortmentMode is the setting, normalised; anything unknown is off.
func AssortmentMode(tenantID string) string {
	switch strings.ToLower(strings.TrimSpace(GetSettingString(tenantID, AssortmentSettingKey))) {
	case AssortmentWarn:
		return AssortmentWarn
	case AssortmentBlock:
		return AssortmentBlock
	}
	return AssortmentOff
}

type assortmentRow struct {
	ID       string
	Store    string
	Channel  string
	Item     string
	Category string
	From, To string
}

// loadAssortmentRows reads the Active rows of one store (matched by Location id
// or code) or one channel.
func loadAssortmentRows(tenantID, store, channel string) ([]assortmentRow, error) {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return nil, err
	}
	rs, err := db.DB.Query(fmt.Sprintf(`SELECT id, COALESCE(data->>'store',''), COALESCE(data->>'channel',''), COALESCE(data->>'item',''),
		COALESCE(data->>'category',''), COALESCE(data->>'valid_from',''), COALESCE(data->>'valid_to','')
		FROM %s.documents WHERE doctype = 'Assortment' AND deleted_at IS NULL
		AND COALESCE(NULLIF(data->>'status',''),'Active') = 'Active'
		AND ((COALESCE(data->>'store','') <> '' AND lower(data->>'store') = ANY($1)) OR ($2 <> '' AND lower(COALESCE(data->>'channel','')) = lower($2)))`, schema),
		pq.Array(storeKeys(tenantID, store)), channel)
	if err != nil {
		return nil, err
	}
	defer rs.Close()
	var out []assortmentRow
	for rs.Next() {
		var r assortmentRow
		if err := rs.Scan(&r.ID, &r.Store, &r.Channel, &r.Item, &r.Category, &r.From, &r.To); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rs.Err()
}

// storeKeys is every name a store goes by (what was typed, its Location id and
// its code), lower-cased, so a row written with the code matches a PO written
// with the id.
func storeKeys(tenantID, store string) []string {
	store = strings.TrimSpace(store)
	if store == "" {
		return []string{}
	}
	keys := map[string]bool{strings.ToLower(store): true}
	if schema, err := db.GetTenantSchema(tenantID); err == nil {
		var id, code string
		if db.DB.QueryRow(fmt.Sprintf(`SELECT id, COALESCE(data->>'code','') FROM %s.documents
			WHERE doctype = 'Location' AND deleted_at IS NULL AND (id = $1 OR data->>'code' = $1) LIMIT 1`, schema), store).Scan(&id, &code) == nil {
			keys[strings.ToLower(id)] = true
			if code != "" {
				keys[strings.ToLower(code)] = true
			}
		}
	}
	out := make([]string, 0, len(keys))
	for k := range keys {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func (r assortmentRow) inWindow(day string) bool {
	return (r.From == "" || r.From <= day) && (r.To == "" || r.To >= day)
}

func (r assortmentRow) admits(sku string, item map[string]interface{}) bool {
	if r.Item != "" && (strings.EqualFold(r.Item, sku) || strings.EqualFold(r.Item, strField(item, "code"))) {
		return true
	}
	return r.Category != "" && strings.EqualFold(r.Category, strField(item, "category"))
}

// itemsOutsideRange returns the SKUs the store's (or channel's) range does not
// admit today. ranged is false when it has no Active rows: then nothing is
// outside.
func itemsOutsideRange(tenantID, store, channel string, skus []string) (outside []string, ranged bool, err error) {
	rows, err := loadAssortmentRows(tenantID, store, channel)
	if err != nil || len(rows) == 0 {
		return nil, false, err
	}
	day := BusinessToday()
	seen := map[string]bool{}
	for _, sku := range skus {
		sku = strings.TrimSpace(sku)
		if sku == "" || seen[sku] {
			continue
		}
		seen[sku] = true
		var item map[string]interface{}
		if r, rerr := ResolveItemBySKU(tenantID, sku); rerr == nil {
			item = r.Data
		}
		in := false
		for _, r := range rows {
			if r.inWindow(day) && r.admits(sku, item) {
				in = true
				break
			}
		}
		if !in {
			outside = append(outside, sku)
		}
	}
	return outside, true, nil
}

// assortmentMessage is the sentence a refusal or a warning carries.
func assortmentMessage(target, verb string, outside []string) string {
	shown := outside
	more := ""
	if len(shown) > 5 {
		shown, more = shown[:5], fmt.Sprintf(" and %d more", len(outside)-5)
	}
	return fmt.Sprintf("%s is outside the range of %s: %s%s", strings.Join(shown, ", "), target, verb, more)
}

// assortmentDocTargets is the store(s) a PO or a transfer is for and the line
// SKUs, or none when this save does not add or change lines.
func assortmentDocTargets(doctype string, priorData, payload map[string]interface{}) (stores, skus []string) {
	raw, _ := payload["items"].(string)
	if raw == "" {
		return nil, nil
	}
	if priorData != nil && raw == strField(priorData, "items") {
		return nil, nil // lines untouched: an approval or a status move is not re-judged
	}
	switch doctype {
	case "PurchaseOrder":
		lines, err := parsePOLineMaps(raw)
		if err != nil {
			return nil, nil
		}
		for _, l := range lines {
			skus = append(skus, strField(l, "sku"))
		}
		stores = []string{strField(payload, "location"), strField(payload, "target_warehouse")}
	case "TransferOrder":
		lines, err := parseTransferItems(raw)
		if err != nil {
			return nil, nil
		}
		for _, l := range lines {
			skus = append(skus, l.Sku)
		}
		stores = []string{strField(payload, "to_warehouse")}
	}
	return stores, skus
}

// AssortmentCheckDocument judges a purchase order or transfer save. mode is
// the setting; msg is "" when nothing is outside.
func AssortmentCheckDocument(tenantID, doctype string, priorData, payload map[string]interface{}) (mode, msg string, err error) {
	if doctype != "PurchaseOrder" && doctype != "TransferOrder" {
		return AssortmentOff, "", nil
	}
	if mode = AssortmentMode(tenantID); mode == AssortmentOff {
		return mode, "", nil
	}
	stores, skus := assortmentDocTargets(doctype, priorData, payload)
	if len(skus) == 0 {
		return mode, "", nil
	}
	done := map[string]bool{}
	for _, store := range stores {
		if store = strings.TrimSpace(store); store == "" || done[strings.ToLower(store)] {
			continue
		}
		done[strings.ToLower(store)] = true
		outside, _, qerr := itemsOutsideRange(tenantID, store, "", skus)
		if qerr != nil {
			return mode, "", qerr
		}
		if len(outside) > 0 {
			verb := "a new purchase order for it cannot carry them"
			if doctype == "TransferOrder" {
				verb = "a transfer into it cannot carry them"
			}
			return mode, assortmentMessage(store, verb, outside), nil
		}
	}
	return mode, "", nil
}

// validateAssortmentRange is the save check: it refuses in Block mode only.
func validateAssortmentRange(tenantID, doctype string, priorData, payload map[string]interface{}) error {
	mode, msg, err := AssortmentCheckDocument(tenantID, doctype, priorData, payload)
	if err != nil || mode != AssortmentBlock || msg == "" {
		return err
	}
	code := assortmentCodePO
	if doctype == "TransferOrder" {
		code = assortmentCodeTransfer
	}
	return &ValidationError{Code: code, SubFor: "items", Message: msg + " - add them to its range, or remove them from the lines"}
}

// AssortmentWarnDocument is the warning for the save response in Warn mode;
// it also leaves a Logged item exception so the miss can be reviewed.
func AssortmentWarnDocument(tenantID, doctype, docID string, priorData, payload map[string]interface{}) string {
	mode, msg, err := AssortmentCheckDocument(tenantID, doctype, priorData, payload)
	if err != nil || mode != AssortmentWarn || msg == "" {
		return ""
	}
	code := assortmentCodePO
	if doctype == "TransferOrder" {
		code = assortmentCodeTransfer
	}
	RecordItemException(tenantID, ItemExceptionInput{ProcessType: ItemExceptionOutsideRange, SourceDoctype: doctype, SourceID: docID,
		Code: code, Message: msg, Qty: 1, Status: ItemExceptionStatusLogged,
		DedupKey: "assortment|" + doctype + "|" + docID + "|" + msg})
	return msg
}

// assortmentSaleGate is the till's check for one line: Block refuses, Warn
// leaves a Logged exception, Off and an un-ranged store do nothing.
func assortmentSaleGate(tenantID, cartNumber, location, sku string, item map[string]interface{}) error {
	mode := AssortmentMode(tenantID)
	if mode == AssortmentOff || strings.TrimSpace(location) == "" {
		return nil
	}
	outside, _, err := itemsOutsideRange(tenantID, location, "", []string{sku})
	if err != nil || len(outside) == 0 {
		return nil // a failed lookup never stops a sale
	}
	msg := fmt.Sprintf("%s is outside the range of %s", posLineLabel(item, sku), location)
	logPOSPolicyAttempt(tenantID, cartNumber, location, sku, ItemExceptionOutsideRange, assortmentCodeSale, msg)
	if mode == AssortmentBlock {
		return &ValidationError{Code: assortmentCodeSale, SubFor: sku, Message: msg + " - it is not sold here"}
	}
	return nil
}

// assortmentChannelGate is the same check for a channel order's lines.
func assortmentChannelGate(tenantID, channel string, skus []string) error {
	mode := AssortmentMode(tenantID)
	if mode == AssortmentOff || strings.TrimSpace(channel) == "" {
		return nil
	}
	outside, _, err := itemsOutsideRange(tenantID, "", channel, skus)
	if err != nil || len(outside) == 0 {
		return nil
	}
	msg := assortmentMessage("channel "+channel, "it does not carry them", outside)
	if mode == AssortmentBlock {
		return &ValidationError{Code: assortmentCodeChannel, SubFor: "items", Message: msg}
	}
	RecordItemException(tenantID, ItemExceptionInput{ProcessType: ItemExceptionOutsideRange, SourceDoctype: "SalesOrder", SourceID: channel,
		Code: assortmentCodeChannel, Message: msg, Qty: 1, Status: ItemExceptionStatusLogged, DedupKey: "assortment-ch|" + channel + "|" + msg})
	return nil
}

// ValidateAssortmentRecord is the save check of an Assortment row itself.
func ValidateAssortmentRecord(payload map[string]interface{}) error {
	store, channel := strField(payload, "store"), strField(payload, "channel")
	if (store == "") == (channel == "") {
		return &ValidationError{Code: assortmentCodeShape, SubFor: "store", Message: "name a store or a channel, not both and not neither"}
	}
	item, category := strField(payload, "item"), strField(payload, "category")
	if item == "" && category == "" {
		return &ValidationError{Code: assortmentCodeShape, SubFor: "item", Message: "name an item or a category"}
	}
	if item != "" && category != "" {
		return &ValidationError{Code: assortmentCodeShape, SubFor: "item", Message: "name an item or a category, not both"}
	}
	from, to := strField(payload, "valid_from"), strField(payload, "valid_to")
	for _, d := range []string{from, to} {
		if d != "" {
			if _, err := time.Parse("2006-01-02", d); err != nil {
				return &ValidationError{Code: assortmentCodeShape, SubFor: "valid_from", Message: "dates are written YYYY-MM-DD"}
			}
		}
	}
	if from != "" && to != "" && to < from {
		return &ValidationError{Code: assortmentCodeShape, SubFor: "valid_to", Message: "the end date is before the start date"}
	}
	return nil
}

// BulkAssortmentInput adds one category (or a list of items) to many stores or
// channels in one go.
type BulkAssortmentInput struct {
	Stores    []string `json:"stores"`
	Channels  []string `json:"channels"`
	Category  string   `json:"category"`
	Items     []string `json:"items"`
	ValidFrom string   `json:"valid_from"`
	ValidTo   string   `json:"valid_to"`
}

// BulkAssortmentResult says what a bulk add did.
type BulkAssortmentResult struct {
	Added   int `json:"added"`
	Skipped int `json:"skipped"`
}

// BulkAddAssortment checks every row first (nothing is written if one is bad),
// skips a row that already exists, and writes the rest in one transaction.
func BulkAddAssortment(tenantID, actor string, in BulkAssortmentInput) (BulkAssortmentResult, error) {
	var res BulkAssortmentResult
	targets := map[string]string{}
	for _, s := range in.Stores {
		if s = strings.TrimSpace(s); s != "" {
			targets[s] = "store"
		}
	}
	for _, c := range in.Channels {
		if c = strings.TrimSpace(c); c != "" {
			targets[c] = "channel"
		}
	}
	cat := strings.TrimSpace(in.Category)
	var items []string
	for _, i := range in.Items {
		if i = strings.TrimSpace(i); i != "" {
			items = append(items, i)
		}
	}
	if len(targets) == 0 || (cat == "") == (len(items) == 0) {
		return res, &ValidationError{Code: assortmentCodeBulk, Message: "pick at least one store or channel, and one category or a list of items"}
	}
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return res, err
	}
	names := make([]string, 0, len(targets))
	for t := range targets {
		names = append(names, t)
	}
	sort.Strings(names)
	what := items
	if cat != "" {
		what = []string{cat}
	}
	tx, err := db.DB.Begin()
	if err != nil {
		return res, err
	}
	defer tx.Rollback()
	for _, target := range names {
		for _, w := range what {
			payload := map[string]interface{}{targets[target]: target, "status": "Active", "valid_from": in.ValidFrom, "valid_to": in.ValidTo}
			if cat != "" {
				payload["category"] = w
			} else {
				payload["item"] = w
			}
			if err := ValidateAssortmentRecord(payload); err != nil {
				return BulkAssortmentResult{}, err
			}
			var exists bool
			if err := tx.QueryRow(fmt.Sprintf(`SELECT EXISTS (SELECT 1 FROM %s.documents WHERE doctype = 'Assortment' AND deleted_at IS NULL
				AND COALESCE(NULLIF(data->>'status',''),'Active') = 'Active' AND lower(COALESCE(data->>$1,'')) = lower($2)
				AND lower(COALESCE(data->>'item','')) = lower($3) AND lower(COALESCE(data->>'category','')) = lower($4)
				AND COALESCE(data->>'valid_from','') = $5 AND COALESCE(data->>'valid_to','') = $6)`, schema),
				targets[target], target, strField(payload, "item"), strField(payload, "category"), in.ValidFrom, in.ValidTo).Scan(&exists); err != nil {
				return BulkAssortmentResult{}, err
			}
			if exists {
				res.Skipped++
				continue
			}
			data, _ := json.Marshal(payload)
			if _, err := tx.Exec(fmt.Sprintf(`INSERT INTO %s.documents (id, doctype, data, status, created_by) VALUES ($1,'Assortment',$2,'Active',$3)`, schema),
				NewDocIDCompact("ASRT"), string(data), actor); err != nil {
				return BulkAssortmentResult{}, err
			}
			res.Added++
		}
	}
	if err := tx.Commit(); err != nil {
		return BulkAssortmentResult{}, err
	}
	LogAuditEvent(tenantID, actor, "PIM", "ASSORTMENT_BULK_ADDED", fmt.Sprintf("%d added, %d already there", res.Added, res.Skipped))
	return res, nil
}

// --- Range by store report --------------------------------------------------

func init() {
	RegisterReport(ReportDefinition{
		ID: "range-by-store", Label: "Range by store", Category: "PIM", ModuleKey: "pim",
		Columns: []ReportColumn{
			{Key: "store", Label: "Store or channel"},
			{Key: "item", Label: "Item"},
			{Key: "name", Label: "Name"},
			{Key: "via", Label: "In range by"},
			{Key: "valid_from", Label: "From"},
			{Key: "valid_to", Label: "To"},
		},
		Params:   []ReportParam{{Key: "store", Label: "Store or channel", Type: "text"}},
		StoreKey: "store",
		Run:      runRangeByStoreReport,
	})
}

// runRangeByStoreReport lists, for each ranged store or channel, the items it
// carries today (an item row, or every item of a category row).
func runRangeByStoreReport(tenantID string, params map[string]string) ([]map[string]interface{}, error) {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return nil, err
	}
	want := strings.ToLower(strings.TrimSpace(params["store"]))
	rs, err := db.DB.Query(fmt.Sprintf(`SELECT COALESCE(NULLIF(data->>'store',''), data->>'channel', ''), COALESCE(data->>'item',''), COALESCE(data->>'category',''),
		COALESCE(data->>'valid_from',''), COALESCE(data->>'valid_to','')
		FROM %s.documents WHERE doctype = 'Assortment' AND deleted_at IS NULL AND COALESCE(NULLIF(data->>'status',''),'Active') = 'Active'
		ORDER BY 1, 3, 2 LIMIT 2000`, schema))
	if err != nil {
		return nil, err
	}
	type entry struct{ target, item, category, from, to string }
	var entries []entry
	for rs.Next() {
		var e entry
		if err := rs.Scan(&e.target, &e.item, &e.category, &e.from, &e.to); err != nil {
			rs.Close()
			return nil, err
		}
		if want == "" || strings.ToLower(e.target) == want {
			entries = append(entries, e)
		}
	}
	rs.Close()
	day := BusinessToday()
	rows := []map[string]interface{}{}
	for _, e := range entries {
		if !(assortmentRow{From: e.from, To: e.to}).inWindow(day) {
			continue
		}
		if e.item != "" {
			name := ""
			if r, rerr := ResolveItemBySKU(tenantID, e.item); rerr == nil {
				name = strField(r.Data, "name")
			}
			rows = append(rows, map[string]interface{}{"store": e.target, "item": e.item, "name": name, "via": "Item", "valid_from": e.from, "valid_to": e.to})
			continue
		}
		ir, err := db.DB.Query(fmt.Sprintf(`SELECT id, COALESCE(data->>'name','') FROM %s.documents WHERE doctype = 'Item' AND deleted_at IS NULL
			AND lower(COALESCE(data->>'category','')) = lower($1) ORDER BY id LIMIT 1000`, schema), e.category)
		if err != nil {
			return nil, err
		}
		for ir.Next() {
			var id, name string
			if err := ir.Scan(&id, &name); err != nil {
				ir.Close()
				return nil, err
			}
			rows = append(rows, map[string]interface{}{"store": e.target, "item": id, "name": name, "via": "Category " + e.category, "valid_from": e.from, "valid_to": e.to})
		}
		ir.Close()
	}
	return rows, nil
}
