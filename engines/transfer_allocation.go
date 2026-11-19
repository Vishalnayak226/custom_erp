package engines

import (
	"bytes"
	"custom_erp/db"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"sort"
	"strconv"
	"strings"
)

// Stage 146.3.12 (D23): one allocation upload makes one transfer per store.
// The file has store, sku and qty columns; each store gets one Draft
// TransferOrder from the chosen source, all tagged with the run ID. A run ID
// is used once.
// Ask 237.7 changes D23: a run applies partially - the good rows make the
// Drafts and the bad ones come back with a reason to fix and upload again
// under a new run ID. The file may be .xlsx as well as CSV, and wide: a SKU
// column and a column per store, as the goods receipt sheet
// (GetGRNAllocationSheet) gives it.
// The Drafts then go through the ordinary transfer approval: "submit" sends
// them for approval as the maker, "approve" bulk-decides them as the checker,
// so the usual maker-checker rules (who may approve, self-approval) still apply.

// AllocationRequest is the upload's parameters.
type AllocationRequest struct {
	RunID         string `json:"run_id"`
	FromWarehouse string `json:"from_warehouse"`
	Purpose       string `json:"purpose"`
}

// AllocationStore is one store's share of the run.
type AllocationStore struct {
	Store string `json:"store"`
	Lines int    `json:"lines"`
	Units int    `json:"units"`
}

// AllocationShortfall is a SKU the source cannot cover. Informational: the
// transfers still go through the normal fill policy at dispatch.
type AllocationShortfall struct {
	Sku       string `json:"sku"`
	Wanted    int    `json:"wanted"`
	Available int    `json:"available"`
	Short     int    `json:"short"`
}

// AllocationResult is what a dry run shows and a real run did.
type AllocationResult struct {
	RunID      string                `json:"run_id"`
	DryRun     bool                  `json:"dry_run"`
	Stores     []AllocationStore     `json:"stores"`
	Shortfalls []AllocationShortfall `json:"shortfalls"`
	Errors     []RowValidationError  `json:"errors"`
	Created    []string              `json:"created"`
	// FixHeader and FixRows (ask 237.7): the rows that were left out, as the
	// file had them plus a Reason; the screen downloads them to fix and
	// upload again under a new run ID.
	FixHeader []string   `json:"fix_header,omitempty"`
	FixRows   [][]string `json:"fix_rows,omitempty"`
}

// AllocationRunTransfer is one transfer of a run.
type AllocationRunTransfer struct {
	ID     string `json:"id"`
	Store  string `json:"store"`
	Status string `json:"status"`
	Lines  int    `json:"lines"`
}

func allocError(code, format string, args ...interface{}) error {
	return &ValidationError{Code: code, Message: fmt.Sprintf(format, args...)}
}

// allocHead is a header as the reader compares it: lower case, without a
// required-field "*" or a "(...)" note.
func allocHead(h string) string {
	h = strings.ToLower(strings.TrimSpace(strings.TrimPrefix(h, string(rune(0xFEFF)))))
	h = strings.TrimSpace(strings.TrimSuffix(h, "*"))
	if i := strings.Index(h, "("); i > 0 {
		h = strings.TrimSpace(h[:i])
	}
	return h
}

func allocColumn(header []string, names ...string) int {
	for i, h := range header {
		h = allocHead(h)
		for _, n := range names {
			if h == n {
				return i
			}
		}
	}
	return -1
}

var (
	allocSKUNames   = []string{"sku", "item", "item_code", "item code", "combination id", "combination"}
	allocStoreNames = []string{"store", "location", "to_warehouse", "to"}
	allocQtyNames   = []string{"qty", "quantity"}
	// allocInfoCols are the wide sheet's columns that say what a row is; they
	// are read past, never as a store (ask 237.7).
	allocInfoCols = map[string]bool{"design no": true, "design": true, "design number": true, "item name": true, "name": true,
		"description": true, "received": true, "received qty": true, "accepted qty": true, "total": true, "colour": true,
		"color": true, "size": true, "mrp": true, "rate": true, "variant": true, "reason": true, "check": true, "notes": true, "remarks": true}
)

// allocMaxFile caps an allocation upload.
const allocMaxFile = 8 << 20

// allocRows reads the upload: a CSV file, or (ask 237.7) an .xlsx workbook -
// its first sheet with a SKU column in its first six rows. A row whose first
// cell starts with "#" is a hint and is skipped. nums are each row's own row
// number in the file, for the errors.
func allocRows(tenantID string, r io.Reader) (rows [][]string, nums []int, err error) {
	data, err := io.ReadAll(io.LimitReader(r, allocMaxFile+1))
	if err != nil {
		return nil, nil, err
	}
	if len(data) > allocMaxFile {
		return nil, nil, allocError("STOCKT-0488", "the file is over %d MB", allocMaxFile>>20)
	}
	hint := func(rec []string) bool { return len(rec) > 0 && strings.HasPrefix(strings.TrimSpace(rec[0]), "#") }
	if bytes.HasPrefix(data, []byte("PK")) {
		if err := CheckUpload(UploadWorkbook, data); err != nil {
			return nil, nil, err
		}
		sheets, err := ReadXLSX(data)
		if err != nil {
			return nil, nil, err
		}
		for _, s := range sheets {
			for h := 0; h < len(s.Rows) && h < 6; h++ {
				if allocColumn(s.Rows[h], allocSKUNames...) < 0 {
					continue
				}
				rows, nums = append(rows, s.Rows[h]), append(nums, s.RowNumbers[h])
				for j := h + 1; j < len(s.Rows); j++ {
					if !hint(s.Rows[j]) {
						rows, nums = append(rows, s.Rows[j]), append(nums, s.RowNumbers[j])
					}
				}
				return rows, nums, nil
			}
		}
		return nil, nil, allocError("STOCKT-0488", "no sheet of the workbook has a SKU column")
	}
	recs, err := readCSVRecords(tenantID, bytes.NewReader(data))
	if err != nil {
		return nil, nil, err
	}
	for i, rec := range recs {
		if i == 0 || !hint(rec) {
			rows, nums = append(rows, rec), append(nums, i+1)
		}
	}
	return rows, nums, nil
}

func allocRunExists(schema, runID string) (bool, error) {
	var n int
	err := db.DB.QueryRow(fmt.Sprintf(`SELECT COUNT(*) FROM %s.documents WHERE doctype = 'TransferOrder' AND data->>'allocation_run_id' = $1`, schema), runID).Scan(&n)
	return n > 0, err
}

func allocDocExists(schema, doctype, key string) bool {
	var n int
	_ = db.DB.QueryRow(fmt.Sprintf(`SELECT COUNT(*) FROM %s.documents WHERE doctype = $1 AND deleted_at IS NULL
		AND (data->>'code' = $2 OR data->>'sku' = $2 OR id = $2)`, schema), doctype, key).Scan(&n)
	return n > 0
}

func atsInt(v interface{}) int {
	switch x := v.(type) {
	case int:
		return x
	case int64:
		return int(x)
	case float64:
		return int(x)
	}
	return 0
}

// RunStoreAllocation reads the file and, unless dryRun, makes the Drafts.
func RunStoreAllocation(tenantID string, req AllocationRequest, r io.Reader, userID string, dryRun bool) (*AllocationResult, error) {
	req.RunID = strings.TrimSpace(req.RunID)
	req.FromWarehouse = strings.TrimSpace(req.FromWarehouse)
	if req.RunID == "" {
		return nil, allocError("STOCKT-0488", "give the run a name (run ID)")
	}
	if req.FromWarehouse == "" {
		return nil, allocError("STOCKT-0488", "choose the source location")
	}
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return nil, err
	}
	if used, err := allocRunExists(schema, req.RunID); err != nil {
		return nil, err
	} else if used {
		return nil, allocError("STOCKT-0487", "run %s was already uploaded - use a new run ID", req.RunID)
	}
	if !allocDocExists(schema, "Location", req.FromWarehouse) {
		return nil, allocError("STOCKT-0488", "source %s is not a location", req.FromWarehouse)
	}
	rows, nums, err := allocRows(tenantID, r)
	if err != nil {
		return nil, err
	}
	if len(rows) < 2 {
		return nil, allocError("STOCKT-0488", "the file has no rows")
	}
	head := rows[0]
	ci := allocColumn(head, allocStoreNames...)
	ck := allocColumn(head, allocSKUNames...)
	cq := allocColumn(head, allocQtyNames...)
	// Ask 237.7: the wide sheet - a SKU column and a column per store, named
	// by its location code - is the one the goods receipt sheet gives.
	wide := ck >= 0 && ci < 0 && cq < 0
	var storeCols []int
	if wide {
		var unknown []string
		for c, h := range head {
			if c == ck || allocHead(h) == "" || allocInfoCols[allocHead(h)] {
				continue
			}
			if allocDocExists(schema, "Location", strings.TrimSpace(h)) {
				storeCols = append(storeCols, c)
			} else {
				unknown = append(unknown, strings.TrimSpace(h))
			}
		}
		if len(unknown) > 0 {
			return nil, allocError("STOCKT-0488", "column %s is not a location - head each store column with its location code", strings.Join(unknown, ", "))
		}
		if len(storeCols) == 0 {
			return nil, allocError("STOCKT-0488", "the file has a SKU column but no store column")
		}
	} else if ci < 0 || ck < 0 || cq < 0 {
		return nil, allocError("STOCKT-0488", "the file needs store, sku and qty columns, or a SKU column and a column per store")
	}
	res := &AllocationResult{RunID: req.RunID, DryRun: dryRun, Stores: []AllocationStore{}, Shortfalls: []AllocationShortfall{}, Errors: []RowValidationError{}, Created: []string{}}
	// 237.7: a row that cannot be made is left out; the rest go on. Each one
	// comes back as a row of the rows-to-fix file, with its reason.
	reasonCol := allocColumn(head, "reason")
	fixHead := append([]string{}, head...)
	if len(fixHead) > 0 {
		fixHead[0] = strings.TrimPrefix(fixHead[0], string(rune(0xFEFF))) // a CSV's byte-order mark
	}
	if reasonCol < 0 {
		reasonCol = len(fixHead)
		fixHead = append(fixHead, "Reason")
	}
	leaveOut := func(n int, rec []string, reason string) {
		res.Errors = append(res.Errors, RowValidationError{RowNumber: n, Message: reason})
		fix := make([]string, len(fixHead))
		copy(fix, rec)
		fix[reasonCol] = reason
		res.FixRows = append(res.FixRows, fix)
	}
	perStore := map[string]map[string]int{}
	add := func(store, sku string, qty int) {
		if perStore[store] == nil {
			perStore[store] = map[string]int{}
		}
		perStore[store][sku] += qty
	}
	for i, rec := range rows[1:] {
		n := nums[i+1]
		get := func(c int) string {
			if c >= 0 && c < len(rec) {
				return strings.TrimSpace(rec[c])
			}
			return ""
		}
		if wide {
			sku, filled := get(ck), false
			for _, c := range storeCols {
				filled = filled || get(c) != ""
			}
			if !filled {
				continue // a received line not sent anywhere
			}
			if sku == "" || !allocDocExists(schema, "Item", sku) {
				leaveOut(n, rec, fmt.Sprintf("SKU %q is not an item", sku))
				continue
			}
			var probs []string
			bad := append([]string{}, rec...)
			for _, c := range storeCols {
				store, v := strings.TrimSpace(head[c]), get(c)
				if v == "" || v == "0" {
					continue
				}
				qty, qerr := strconv.Atoi(v)
				switch {
				case store == req.FromWarehouse:
					probs = append(probs, store+": store is the source")
				case qerr != nil || qty <= 0:
					probs = append(probs, store+": qty must be a whole number above 0")
				default:
					add(store, sku, qty)
					if c < len(bad) {
						bad[c] = "" // made: the rows-to-fix file keeps only what was not
					}
				}
			}
			if len(probs) > 0 {
				leaveOut(n, bad, strings.Join(probs, "; "))
			}
			continue
		}
		store, sku := get(ci), get(ck)
		if store == "" && sku == "" && get(cq) == "" {
			continue
		}
		qty, qerr := strconv.Atoi(get(cq))
		switch {
		case store == "" || !allocDocExists(schema, "Location", store):
			leaveOut(n, rec, fmt.Sprintf("store %q is not a location", store))
		case store == req.FromWarehouse:
			leaveOut(n, rec, "store is the source")
		case sku == "" || !allocDocExists(schema, "Item", sku):
			leaveOut(n, rec, fmt.Sprintf("SKU %q is not an item", sku))
		case qerr != nil || qty <= 0:
			leaveOut(n, rec, "qty must be a whole number above 0")
		default:
			add(store, sku, qty)
		}
	}
	if len(res.FixRows) > 0 {
		res.FixHeader = fixHead
	}
	if len(perStore) == 0 && len(res.Errors) == 0 {
		if wide {
			return nil, allocError("STOCKT-0488", "no store has a qty - fill the store columns")
		}
		return nil, allocError("STOCKT-0488", "the file has no rows")
	}
	stores := make([]string, 0, len(perStore))
	wanted := map[string]int{}
	for s, m := range perStore {
		stores = append(stores, s)
		u := 0
		for sku, q := range m {
			u += q
			wanted[sku] += q
		}
		res.Stores = append(res.Stores, AllocationStore{Store: s, Lines: len(m), Units: u})
	}
	sort.Strings(stores)
	sort.Slice(res.Stores, func(i, j int) bool { return res.Stores[i].Store < res.Stores[j].Store })
	skus := make([]string, 0, len(wanted))
	for s := range wanted {
		skus = append(skus, s)
	}
	sort.Strings(skus)
	for _, sku := range skus {
		have := 0
		if m, err := GetAvailableToSell(tenantID, sku, req.FromWarehouse); err == nil && m != nil {
			have = atsInt(m["ats"])
		}
		if wanted[sku] > have {
			res.Shortfalls = append(res.Shortfalls, AllocationShortfall{Sku: sku, Wanted: wanted[sku], Available: have, Short: wanted[sku] - have})
		}
	}
	if dryRun {
		return res, nil
	}
	if len(perStore) == 0 {
		return res, allocError("STOCKT-0488", "%d row(s) have errors - nothing was made", len(res.Errors))
	}
	type draft struct {
		id   string
		data map[string]interface{}
	}
	var drafts []draft
	for _, store := range stores {
		m := perStore[store]
		ks := make([]string, 0, len(m))
		for k := range m {
			ks = append(ks, k)
		}
		sort.Strings(ks)
		lines := make([]map[string]interface{}, 0, len(ks))
		for _, k := range ks {
			lines = append(lines, map[string]interface{}{"sku": k, "qty": m[k]})
		}
		itemsJSON, _ := json.Marshal(lines)
		id := NewDocID("TO")
		data := map[string]interface{}{
			"transfer_number": id, "from_warehouse": req.FromWarehouse, "to_warehouse": store,
			"items": string(itemsJSON), "status": "Draft", "allocation_run_id": req.RunID,
		}
		if p := strings.TrimSpace(req.Purpose); p != "" {
			data["purpose"] = p
		}
		if err := ValidateDocument(tenantID, "TransferOrder", data); err != nil {
			return nil, err
		}
		drafts = append(drafts, draft{id, data})
	}
	tx, err := db.DB.Begin()
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	for _, d := range drafts {
		raw, _ := json.Marshal(d.data)
		if _, err := tx.Exec(fmt.Sprintf(`INSERT INTO %s.documents (id, doctype, status, created_by, data) VALUES ($1, 'TransferOrder', 'Draft', $2, $3)`, schema), d.id, userID, raw); err != nil {
			return nil, err
		}
		res.Created = append(res.Created, d.id)
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	LogAuditEvent(tenantID, userID, "TRANSFER_ALLOCATION_RUN", "SUCCESS", fmt.Sprintf("Run %s made %d draft transfer(s) from %s; %d row(s) left out to fix",
		req.RunID, len(res.Created), req.FromWarehouse, len(res.FixRows)))
	return res, nil
}

// GetAllocationRun lists a run's transfers.
func GetAllocationRun(tenantID, runID string) ([]AllocationRunTransfer, error) {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return nil, err
	}
	rows, err := db.DB.Query(fmt.Sprintf(`SELECT id, COALESCE(data->>'to_warehouse',''), status, COALESCE(data->>'items','') FROM %s.documents
		WHERE doctype = 'TransferOrder' AND data->>'allocation_run_id' = $1 AND deleted_at IS NULL ORDER BY created_at, id`, schema), runID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []AllocationRunTransfer{}
	for rows.Next() {
		var t AllocationRunTransfer
		var items string
		if err := rows.Scan(&t.ID, &t.Store, &t.Status, &items); err != nil {
			return nil, err
		}
		if ls, perr := parseTransferItems(items); perr == nil {
			t.Lines = len(ls)
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

// DecideAllocationRun acts on a whole run. "submit" sends its Drafts for
// approval; "approve" approves its Pending Approval transfers. Failures are
// per transfer and reported; nothing to act on is STOCKT-0492.
func DecideAllocationRun(tenantID, runID, action, userID, role, location string) (ok []string, failed map[string]string, err error) {
	var want string
	switch action {
	case "submit":
		want = "Draft"
	case "approve":
		want = "Pending Approval"
	default:
		return nil, nil, allocError("STOCKT-0488", "action must be submit or approve")
	}
	run, err := GetAllocationRun(tenantID, runID)
	if err != nil {
		return nil, nil, err
	}
	var ids []string
	for _, t := range run {
		if t.Status == want {
			ids = append(ids, t.ID)
		}
	}
	if len(ids) == 0 {
		return nil, nil, allocError("STOCKT-0492", "run %s has no %s transfers to %s", runID, want, action)
	}
	if action == "approve" {
		return BulkDecideApproval(tenantID, "TransferOrder", ids, userID, role, location, "Approved", "")
	}
	failed = map[string]string{}
	for _, id := range ids {
		if e := SubmitTransferOrderForApproval(tenantID, id, userID, role); e != nil {
			failed[id] = e.Error()
			continue
		}
		ok = append(ok, id)
	}
	return ok, failed, nil
}

// GRNAllocationLine is one received SKU of a goods receipt.
type GRNAllocationLine struct {
	SKU    string `json:"sku"`
	Design string `json:"design"`
	Name   string `json:"name"`
	Qty    int    `json:"qty"`
}

// GRNAllocationStore is a location the goods can go to.
type GRNAllocationStore struct {
	Code string `json:"code"`
	Name string `json:"name"`
}

// GRNAllocationSheet is what the allocation sheet for a goods receipt lists
// (ask 237.7): each SKU it put into stock with the accepted qty, and every
// other active location as a store column. Location is the receipt's own
// location, the transfers' source.
type GRNAllocationSheet struct {
	Code     string               `json:"code"`
	Location string               `json:"location"`
	Lines    []GRNAllocationLine  `json:"lines"`
	Stores   []GRNAllocationStore `json:"stores"`
}

// grnStockIn reports whether a goods receipt has put its stock in: a one-step
// receipt once approved, a step receipt once posted.
func grnStockIn(data map[string]interface{}, status string) bool {
	switch strings.ToLower(status) {
	case "approved", "posted", "completed", "closed":
		return true
	}
	return strField(data, "grn_step") == GRNStepPosted
}

// GetGRNAllocationSheet reads a posted goods receipt (by ID or number) for
// its allocation sheet.
func GetGRNAllocationSheet(tenantID, grn string) (*GRNAllocationSheet, error) {
	grn = strings.TrimSpace(grn)
	if grn == "" {
		return nil, allocError("STOCKT-0488", "choose the goods receipt")
	}
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return nil, err
	}
	data, status, err := fetchDocData(tenantID, "GRN", grn)
	if err == sql.ErrNoRows {
		return nil, allocError("STOCKT-0488", "goods receipt %s not found", grn)
	} else if err != nil {
		return nil, err
	}
	out := &GRNAllocationSheet{Code: firstNonEmpty(strField(data, "code"), grn), Location: strField(data, "location"),
		Lines: []GRNAllocationLine{}, Stores: []GRNAllocationStore{}}
	if ld, _, err := fetchDocData(tenantID, "Location", out.Location); err == nil {
		out.Location = firstNonEmpty(strField(ld, "code"), out.Location) // the code: the screen's From list holds codes
	}
	if !grnStockIn(data, status) {
		return nil, allocError("STOCKT-0488", "goods receipt %s is not posted yet - its stock is not in %s", out.Code, out.Location)
	}
	at := map[string]int{}
	for _, l := range grnLineMaps(data) {
		sku := strings.TrimSpace(strField(l, "sku"))
		q := int(math.Round(grnLineAcceptedQty(l)))
		if sku == "" || q <= 0 {
			continue
		}
		if i, ok := at[sku]; ok {
			out.Lines[i].Qty += q
			continue
		}
		line := GRNAllocationLine{SKU: sku, Qty: q}
		if item, _, err := fetchDocData(tenantID, "Item", sku); err == nil {
			line.Name = strField(item, "name")
			if fam := strField(item, "family"); fam != "" {
				line.Design = fam
				if fd, _, err := fetchDocData(tenantID, "ProductFamily", fam); err == nil {
					line.Design = firstNonEmpty(strField(fd, "code"), fam)
				}
			}
		}
		at[sku] = len(out.Lines)
		out.Lines = append(out.Lines, line)
	}
	if len(out.Lines) == 0 {
		return nil, allocError("STOCKT-0488", "goods receipt %s has no accepted stock to send", out.Code)
	}
	rows, err := db.DB.Query(fmt.Sprintf(`SELECT COALESCE(NULLIF(data->>'code', ''), id), COALESCE(data->>'name', '') FROM %s.documents
		WHERE doctype = 'Location' AND deleted_at IS NULL
		  AND lower(COALESCE(status, '')) NOT IN ('inactive', 'disabled', 'archived', 'closed', 'cancelled')
		  AND COALESCE(data->>'is_active', 'true') <> 'false'
		ORDER BY 1`, schema))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var s GRNAllocationStore
		if err := rows.Scan(&s.Code, &s.Name); err != nil {
			return nil, err
		}
		if s.Code != out.Location {
			out.Stores = append(out.Stores, s)
		}
	}
	return out, rows.Err()
}
