package engines

import (
	"custom_erp/db"
	"encoding/json"
	"fmt"
	"io"
	"sort"
	"strconv"
	"strings"
)

// Stage 146.3.12 (D23): one allocation upload makes one transfer per store.
// The file has store, sku and qty columns; each store gets one Draft
// TransferOrder from the chosen source, all tagged with the run ID. A run is
// all or nothing - one bad row makes no Draft - and a run ID is used once.
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

func allocColumn(header []string, names ...string) int {
	for i, h := range header {
		h = strings.ToLower(strings.TrimSpace(strings.TrimPrefix(h, string(rune(0xFEFF)))))
		for _, n := range names {
			if h == n {
				return i
			}
		}
	}
	return -1
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
	rows, err := readCSVRecords(tenantID, r)
	if err != nil {
		return nil, err
	}
	if len(rows) < 2 {
		return nil, allocError("STOCKT-0488", "the file has no rows")
	}
	ci := allocColumn(rows[0], "store", "location", "to_warehouse", "to")
	ck := allocColumn(rows[0], "sku", "item", "item_code")
	cq := allocColumn(rows[0], "qty", "quantity")
	if ci < 0 || ck < 0 || cq < 0 {
		return nil, allocError("STOCKT-0488", "the file needs store, sku and qty columns")
	}
	res := &AllocationResult{RunID: req.RunID, DryRun: dryRun, Stores: []AllocationStore{}, Shortfalls: []AllocationShortfall{}, Errors: []RowValidationError{}, Created: []string{}}
	perStore := map[string]map[string]int{}
	for i, rec := range rows[1:] {
		n := i + 2
		get := func(c int) string {
			if c < len(rec) {
				return strings.TrimSpace(rec[c])
			}
			return ""
		}
		store, sku := get(ci), get(ck)
		if store == "" && sku == "" && get(cq) == "" {
			continue
		}
		qty, qerr := strconv.Atoi(get(cq))
		switch {
		case store == "" || !allocDocExists(schema, "Location", store):
			res.Errors = append(res.Errors, RowValidationError{RowNumber: n, Message: fmt.Sprintf("store %q is not a location", store)})
		case store == req.FromWarehouse:
			res.Errors = append(res.Errors, RowValidationError{RowNumber: n, Message: "store is the source"})
		case sku == "" || !allocDocExists(schema, "Item", sku):
			res.Errors = append(res.Errors, RowValidationError{RowNumber: n, Message: fmt.Sprintf("SKU %q is not an item", sku)})
		case qerr != nil || qty <= 0:
			res.Errors = append(res.Errors, RowValidationError{RowNumber: n, Message: "qty must be a whole number above 0"})
		default:
			if perStore[store] == nil {
				perStore[store] = map[string]int{}
			}
			perStore[store][sku] += qty
		}
	}
	if len(perStore) == 0 && len(res.Errors) == 0 {
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
	if len(res.Errors) > 0 {
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
	LogAuditEvent(tenantID, userID, "TRANSFER_ALLOCATION_RUN", "SUCCESS", fmt.Sprintf("Run %s made %d draft transfer(s) from %s", req.RunID, len(res.Created), req.FromWarehouse))
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
