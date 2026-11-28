package engines

// Items 324.1 and 324.4 (ask 324): stock requests.
//
// A location (store or warehouse) asks another location for stock. The
// request is a transfer order raised by the location that receives it and
// sent for approval: no second document type. One request asking several
// sources becomes one transfer per source, all carrying the same request
// number. The source's Accept is the approval (approval rules, maker-checker
// and the audit trail unchanged); only someone at the source, or with full
// access, may accept or reject. The source may accept less: a line is cut,
// a line at 0 is cancelled, and what was asked stays beside it. Pack,
// dispatch and receive are the transfer's own steps.
//
// Auto requests: a location switched on ("Auto Stock Requests" = Yes) gets
// one request a day, at the client's hour, for what its reorder rules say it
// is short, skipping items already on an open transfer to it.

import (
	"context"
	"custom_erp/db"
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"math"
	"strings"
	"time"

	"github.com/lib/pq"
)

// StockRequestAutoHourSetting is the hour (client's time) of the daily auto run.
const StockRequestAutoHourSetting = "inventory.stock_request_auto_hour"

func init() {
	RegisterSetting(SettingDefinition{
		Key: StockRequestAutoHourSetting, Module: "Warehouse",
		Label: "Auto stock requests: hour of day", Type: SettingTypeInt, Default: "6",
		Min: settingBound(0), Max: settingBound(23),
		Description: "Locations with Auto Stock Requests = Yes ask for what their reorder rules say is short once a day at this hour (0-23, your business time).",
	})
}

func stockRequestRefusal(msg string) error {
	return &ValidationError{Code: "STOCKT-0494", Message: msg}
}

// StockRequestInput is a new request from one location.
type StockRequestInput struct {
	Location   string               `json:"location"`
	ReasonCode string               `json:"reason_code"`
	Reason     string               `json:"reason"`
	Note       string               `json:"note"`
	NeededBy   string               `json:"needed_by"`
	Lines      []StockRequestLineIn `json:"lines"`
	Origin     string               `json:"-"`
}

// StockRequestTransfer is one transfer a request became.
type StockRequestTransfer struct {
	ID     string  `json:"id"`
	From   string  `json:"from"`
	Status string  `json:"status"`
	Lines  int     `json:"lines"`
	Units  float64 `json:"units"`
	Note   string  `json:"note,omitempty"`
}

// StockRequestResult is what a create made, and what no source could cover.
type StockRequestResult struct {
	RequestNo string                 `json:"request_no"`
	Location  string                 `json:"location"`
	Transfers []StockRequestTransfer `json:"transfers"`
	Unplaced  []StockRequestLineIn   `json:"unplaced,omitempty"`
}

// mayActFor says whether a user may act for a location: full access, no
// location of their own (head office), or that very location.
func mayActFor(locs requestLocations, role, userLocation, location string) bool {
	if HasFullAccess(role) || strings.TrimSpace(userLocation) == "" {
		return true
	}
	return locs.canon(userLocation) == locs.canon(location)
}

// CreateStockRequest raises a request: one transfer per source, each sent
// for approval. Lines without a source are placed by the sourcing engine;
// what no source can spare comes back as Unplaced.
func CreateStockRequest(tenantID, userID, role, userLocation string, in StockRequestInput) (*StockRequestResult, error) {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return nil, err
	}
	locs, err := loadRequestLocations(schema)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(in.Location) == "" {
		in.Location = userLocation
	}
	req := locs.get(in.Location)
	if req == nil {
		return nil, stockRequestRefusal("choose the location that needs the stock")
	}
	if !req.active() {
		return nil, stockRequestRefusal(fmt.Sprintf("%s is not active", req.label()))
	}
	if !mayActFor(locs, role, userLocation, req.ID) {
		return nil, stockRequestRefusal("you can ask for stock only for your own location")
	}
	// A built-in reason is a fixed list too, so it passes even where the
	// client requires reason codes; typed text alone does not.
	code, text := "", strings.TrimSpace(in.Reason)
	if strings.TrimSpace(in.ReasonCode) != "" || !isBuiltInStockRequestReason(text) {
		code, text, err = ReasonForException(tenantID, StockRequestReasonCategory, in.ReasonCode, in.Reason)
		if err != nil {
			return nil, err
		}
	}
	if text == "" {
		return nil, stockRequestRefusal("give a reason - pick one or type it")
	}
	neededBy := strings.TrimSpace(in.NeededBy)
	if neededBy != "" {
		if _, err := time.Parse("2006-01-02", neededBy); err != nil {
			return nil, stockRequestRefusal("needed-by date must be YYYY-MM-DD")
		}
	}
	lines := mergeRequestLines(in.Lines)
	var asked []StockRequestLineIn
	for _, l := range lines {
		if l.Qty >= 1 {
			asked = append(asked, l)
		}
	}
	if len(asked) == 0 {
		return nil, stockRequestRefusal("add at least one item with a quantity")
	}
	for _, l := range asked {
		if l.From == "" {
			continue
		}
		src := locs.get(l.From)
		if src == nil || !src.active() {
			return nil, stockRequestRefusal(fmt.Sprintf("source %q is not an active location", l.From))
		}
		if src.ID == req.ID {
			return nil, stockRequestRefusal(fmt.Sprintf("%s cannot come from the location asking for it", l.SKU))
		}
	}

	plan, err := SuggestRequestSources(tenantID, req.ID, asked)
	if err != nil {
		return nil, err
	}
	res := &StockRequestResult{Location: req.ID, Transfers: []StockRequestTransfer{}}
	type group struct {
		from  string
		items []map[string]interface{}
		units float64
	}
	var groups []*group
	byFrom := map[string]*group{}
	for _, l := range plan.Lines {
		for _, p := range l.Plan {
			g := byFrom[p.From]
			if g == nil {
				g = &group{from: p.From}
				byFrom[p.From] = g
				groups = append(groups, g)
			}
			q := int(p.Qty)
			g.items = append(g.items, map[string]interface{}{"sku": l.SKU, "qty": q, "requested_qty": q})
			g.units += p.Qty
		}
		if l.Short > 0 {
			res.Unplaced = append(res.Unplaced, StockRequestLineIn{SKU: l.SKU, Qty: l.Short})
		}
	}
	if len(groups) == 0 {
		return res, stockRequestRefusal("no other location can spare these items now - name a source on each line to ask anyway")
	}

	origin := in.Origin
	if origin == "" {
		origin = "Store"
	}
	today := BusinessToday()
	type built struct {
		g       *group
		payload map[string]interface{}
		draw    *NumberDraw
	}
	var all []built
	written := false
	defer func() {
		if !written {
			for _, b := range all {
				b.draw.Release()
			}
		}
	}()
	requestNo := ""
	for _, g := range groups {
		itemsJSON, _ := json.Marshal(g.items)
		payload := map[string]interface{}{
			"from_warehouse": g.from, "to_warehouse": req.ID, "items": string(itemsJSON), "status": "Draft",
			"request_origin": origin, "request_reason_code": code, "request_reason": text,
			"request_note": strings.TrimSpace(in.Note), "needed_by": neededBy,
			"requested_by": userID, "requested_on": today,
		}
		draw, err := DrawDocumentNumber(tenantID, g.from, "TransferOrder", true, payload)
		if err != nil {
			return nil, err
		}
		all = append(all, built{g: g, payload: payload, draw: draw})
		id, _ := payload["id"].(string)
		if id == "" {
			n, err := GenerateSequence(tenantID, "TO", g.from, documentFinancialYear(time.Now()))
			if err != nil {
				return nil, err
			}
			payload["id"], payload["transfer_number"] = n, n
		}
		if requestNo == "" {
			requestNo, _ = payload["id"].(string)
		}
		payload["request_no"] = requestNo
		if err := ValidateDocument(tenantID, "TransferOrder", payload); err != nil {
			return nil, err
		}
		if err := ValidateTransactionalRules(tenantID, "TransferOrder", "", "", nil, payload); err != nil {
			return nil, err
		}
	}

	tx, err := db.DB.Begin()
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	for _, b := range all {
		data, _ := json.Marshal(b.payload)
		if _, err := tx.Exec(fmt.Sprintf(`
			INSERT INTO %s.documents (id, doctype, data, status, created_by, version)
			VALUES ($1, 'TransferOrder', $2, 'Draft', $3, 1)`, schema), b.payload["id"], data, userID); err != nil {
			if dup := DuplicateKeyRefusal(tenantID, "TransferOrder", err); dup != nil {
				return nil, dup
			}
			return nil, err
		}
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	written = true
	res.RequestNo = requestNo

	for _, b := range all {
		id, _ := b.payload["id"].(string)
		InvokeAfterSaveHooksAsync(tenantID, userID, "TransferOrder", id, b.payload)
		t := StockRequestTransfer{ID: id, From: b.g.from, Status: "Draft", Lines: len(b.g.items), Units: b.g.units}
		if err := SubmitTransferOrderForApproval(tenantID, id, userID, role); err != nil {
			// No approval rule for transfers (or another refusal): the request
			// stays a Draft the source can still accept directly.
			t.Note = "waiting at the source as a draft: " + err.Error()
		} else {
			t.Status = "Pending Approval"
		}
		res.Transfers = append(res.Transfers, t)
	}
	LogAuditEvent(tenantID, userID, "STOCK_REQUEST_CREATE", "SUCCESS",
		fmt.Sprintf("Stock request %s for %s (%s): %d transfer(s), reason %q", requestNo, req.ID, origin, len(res.Transfers), text))
	return res, nil
}

// loadStockRequest reads a request transfer and checks the actor is at its
// source.
func loadStockRequest(tenantID, id, role, userLocation string) (map[string]interface{}, string, error) {
	data, status, _, err := fetchDocument(tenantID, "TransferOrder", id)
	if err != nil {
		return nil, "", stockRequestRefusal("that request was not found")
	}
	if s, _ := data["request_no"].(string); s == "" {
		return nil, "", stockRequestRefusal("that transfer is not a stock request")
	}
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return nil, "", err
	}
	locs, err := loadRequestLocations(schema)
	if err != nil {
		return nil, "", err
	}
	from, _ := data["from_warehouse"].(string)
	if !mayActFor(locs, role, userLocation, from) {
		return nil, "", stockRequestRefusal("only the location asked to send the stock can accept or reject this request")
	}
	return data, status, nil
}

// AcceptStockRequest approves a request at its source. lines, when given,
// accept less: each names an item and the quantity the source will send
// (0 cancels the line). Accepting more than was asked is refused.
func AcceptStockRequest(tenantID, id, userID, role, userLocation string, lines []StockRequestLineIn) error {
	data, status, err := loadStockRequest(tenantID, id, role, userLocation)
	if err != nil {
		return err
	}
	if status != "Draft" && status != "Pending Approval" {
		return stockRequestRefusal(fmt.Sprintf("this request is %s - only a new request can be accepted", status))
	}
	itemsStr, _ := data["items"].(string)
	current, err := parseTransferItems(itemsStr)
	if err != nil {
		return err
	}
	have := map[string]int{}
	for _, l := range current {
		have[l.Sku] += l.Qty
	}
	cut := map[string]int{}
	for _, l := range lines {
		sku := strings.TrimSpace(l.SKU)
		q, ok := have[sku]
		if !ok {
			return stockRequestRefusal(fmt.Sprintf("%s is not on this request", sku))
		}
		n := int(math.Round(l.Qty))
		if n < 0 || n > q {
			return stockRequestRefusal(fmt.Sprintf("%s: send between 0 and %d", sku, q))
		}
		if n < q {
			cut[sku] = n
		}
	}
	if len(cut) > 0 {
		left := 0
		for sku, q := range have {
			if n, ok := cut[sku]; ok {
				left += n
			} else {
				left += q
			}
		}
		if left == 0 {
			return stockRequestRefusal("nothing left to send - reject the request instead")
		}
	}
	comment := "Stock request accepted"
	if len(cut) > 0 {
		comment = "Stock request accepted with less"
	}
	if status == "Pending Approval" {
		err = DecideApproval(tenantID, "TransferOrder", id, userID, role, userLocation, "Approved", comment)
	} else {
		_, err = ApproveTransferOrderDirect(tenantID, id, userID, role, userLocation)
	}
	if err != nil {
		return err
	}
	if len(cut) > 0 {
		if err := cutStockRequestLines(tenantID, id, userID, cut); err != nil {
			return fmt.Errorf("accepted, but the quantities were not changed: %w", err)
		}
	}
	LogAuditEvent(tenantID, userID, "STOCK_REQUEST_ACCEPT", "SUCCESS", fmt.Sprintf("Transfer %s: %s", id, comment))
	return nil
}

// cutStockRequestLines lowers lines of an approved request; the asked
// quantity stays in requested_qty and a line at 0 is cancelled.
func cutStockRequestLines(tenantID, id, userID string, cut map[string]int) error {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return err
	}
	tx, err := db.DB.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	data, status, err := lockTransferOrderTx(tx, schema, id)
	if err != nil {
		return err
	}
	if status != "Approved" {
		return stockRequestRefusal(fmt.Sprintf("this request is %s", status))
	}
	var items []map[string]interface{}
	itemsStr, _ := data["items"].(string)
	if err := json.Unmarshal([]byte(itemsStr), &items); err != nil {
		return err
	}
	for _, it := range items {
		sku, _ := it["sku"].(string)
		n, ok := cut[sku]
		if !ok {
			continue
		}
		if c, _ := it["cancelled"].(bool); c {
			continue
		}
		if _, set := it["requested_qty"]; !set {
			it["requested_qty"] = it["qty"]
		}
		it["qty"] = n
		if n == 0 {
			it["cancelled"] = true
			it["cancel_reason"] = "Not sent by the source"
		}
	}
	itemsJSON, _ := json.Marshal(items)
	data["items"] = string(itemsJSON)
	data["accepted_less"] = true
	b, _ := json.Marshal(data)
	if _, err := tx.Exec(fmt.Sprintf(`UPDATE %s.documents SET data = $1, updated_at = CURRENT_TIMESTAMP, updated_by = $3
		WHERE doctype = 'TransferOrder' AND id = $2`, schema), b, id, userID); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	_, _ = stampTransferValue(tenantID, id)
	SyncTransferReservation(tenantID, id)
	return nil
}

// RejectStockRequest turns a request down at its source, with a reason.
func RejectStockRequest(tenantID, id, userID, role, userLocation, reason string) error {
	reason = strings.TrimSpace(reason)
	if reason == "" {
		return &ValidationError{Code: "APPROV-0159", Message: "say why the request is rejected"}
	}
	_, status, err := loadStockRequest(tenantID, id, role, userLocation)
	if err != nil {
		return err
	}
	switch status {
	case "Pending Approval":
		if err := DecideApproval(tenantID, "TransferOrder", id, userID, role, userLocation, "Rejected", reason); err != nil {
			return err
		}
	case "Draft":
		if err := rejectDraftStockRequest(tenantID, id, userID, role, reason); err != nil {
			return err
		}
	default:
		return stockRequestRefusal(fmt.Sprintf("this request is %s - only a new request can be rejected", status))
	}
	schema, err := db.GetTenantSchema(tenantID)
	if err == nil {
		patch, _ := json.Marshal(map[string]interface{}{"rejected_reason": reason, "rejected_by": userID})
		_, _ = db.DB.Exec(fmt.Sprintf(`UPDATE %s.documents SET data = data || $1::jsonb WHERE doctype = 'TransferOrder' AND id = $2`, schema), patch, id)
	}
	LogAuditEvent(tenantID, userID, "STOCK_REQUEST_REJECT", "SUCCESS", fmt.Sprintf("Transfer %s: %s", id, reason))
	return nil
}

// rejectDraftStockRequest rejects a request that could not be sent for
// approval (no approval rule): the same status and approval-log row a
// decision writes.
func rejectDraftStockRequest(tenantID, id, userID, role, reason string) error {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return err
	}
	tx, err := db.DB.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	data, status, err := lockTransferOrderTx(tx, schema, id)
	if err != nil {
		return err
	}
	if status != "Draft" {
		return stockRequestRefusal(fmt.Sprintf("this request is %s", status))
	}
	data["status"] = "Rejected"
	b, _ := json.Marshal(data)
	if _, err := tx.Exec(fmt.Sprintf(`UPDATE %s.documents SET data = $1, status = 'Rejected', updated_at = CURRENT_TIMESTAMP, updated_by = $3
		WHERE doctype = 'TransferOrder' AND id = $2`, schema), b, id, userID); err != nil {
		return err
	}
	if _, err := tx.Exec(fmt.Sprintf(`
		INSERT INTO %s.approval_log (doctype, document_id, action, actor_user_id, actor_role, amount, comment)
		VALUES ('TransferOrder', $1, 'Rejected', $2, $3, 0, $4)`, schema), id, userID, role, reason); err != nil {
		return err
	}
	return tx.Commit()
}

// StockRequestRowLine is one line of a request in a list.
type StockRequestRowLine struct {
	SKU       string `json:"sku"`
	Name      string `json:"name"`
	Qty       int    `json:"qty"`
	Requested int    `json:"requested"`
	Cancelled bool   `json:"cancelled,omitempty"`
}

// StockRequestRow is one request transfer in a list.
type StockRequestRow struct {
	ID             string                `json:"id"`
	RequestNo      string                `json:"request_no"`
	From           string                `json:"from"`
	FromName       string                `json:"from_name"`
	To             string                `json:"to"`
	ToName         string                `json:"to_name"`
	Status         string                `json:"status"`
	Origin         string                `json:"origin"`
	Reason         string                `json:"reason"`
	Note           string                `json:"note,omitempty"`
	NeededBy       string                `json:"needed_by,omitempty"`
	RequestedBy    string                `json:"requested_by"`
	RequestedOn    string                `json:"requested_on"`
	RejectedReason string                `json:"rejected_reason,omitempty"`
	AcceptedLess   bool                  `json:"accepted_less,omitempty"`
	Units          int                   `json:"units"`
	Lines          []StockRequestRowLine `json:"lines"`
}

var stockRequestOpenStatuses = []string{"Draft", "Pending Approval", "Approved", "Packed", "Dispatched", "Partially Received"}

// ListStockRequests lists requests a location made (side "mine") or was
// asked to send (side "incoming"). A user tied to a location sees only that
// location's; full access or head office may name any location, or none.
// state: "open", "closed" or "" for all.
func ListStockRequests(tenantID, role, userLocation, side, location, state string) ([]StockRequestRow, error) {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return nil, err
	}
	locs, err := loadRequestLocations(schema)
	if err != nil {
		return nil, err
	}
	if !HasFullAccess(role) && strings.TrimSpace(userLocation) != "" {
		location = userLocation
	}
	field := "to_warehouse"
	if side == "incoming" {
		field = "from_warehouse"
	}
	where := []string{"doctype = 'TransferOrder'", "deleted_at IS NULL", "COALESCE(data->>'request_no', '') <> ''"}
	args := []interface{}{}
	if strings.TrimSpace(location) != "" {
		l := locs.get(location)
		refs := []string{location}
		if l != nil {
			refs = []string{l.ID}
			if l.Code != "" && l.Code != l.ID {
				refs = append(refs, l.Code)
			}
		}
		args = append(args, pq.Array(refs))
		where = append(where, fmt.Sprintf("data->>'%s' = ANY($%d)", field, len(args)))
	}
	switch state {
	case "open":
		args = append(args, pq.Array(stockRequestOpenStatuses))
		where = append(where, fmt.Sprintf("status = ANY($%d)", len(args)))
	case "closed":
		args = append(args, pq.Array(stockRequestOpenStatuses))
		where = append(where, fmt.Sprintf("NOT (status = ANY($%d))", len(args)))
	}
	rows, err := db.DB.Query(fmt.Sprintf(`SELECT id, status, data FROM %s.documents WHERE %s
		ORDER BY created_at DESC, id DESC LIMIT 500`, schema, strings.Join(where, " AND ")), args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []StockRequestRow{}
	skuSet := map[string]bool{}
	for rows.Next() {
		var id, status string
		var raw []byte
		if err := rows.Scan(&id, &status, &raw); err != nil {
			return nil, err
		}
		var d map[string]interface{}
		if json.Unmarshal(raw, &d) != nil {
			continue
		}
		s := func(k string) string { v, _ := d[k].(string); return v }
		r := StockRequestRow{ID: id, RequestNo: s("request_no"), From: s("from_warehouse"), To: s("to_warehouse"), Status: status,
			Origin: s("request_origin"), Reason: s("request_reason"), Note: s("request_note"), NeededBy: s("needed_by"),
			RequestedBy: s("requested_by"), RequestedOn: s("requested_on"), RejectedReason: s("rejected_reason"), Lines: []StockRequestRowLine{}}
		r.AcceptedLess, _ = d["accepted_less"].(bool)
		if l := locs.get(r.From); l != nil {
			r.FromName = l.label()
		}
		if l := locs.get(r.To); l != nil {
			r.ToName = l.label()
		}
		var items []map[string]interface{}
		_ = json.Unmarshal([]byte(s("items")), &items)
		for _, it := range items {
			ln := StockRequestRowLine{SKU: fmt.Sprint(it["sku"]), Qty: int(numFromInterface(it["qty"]))}
			ln.Requested = ln.Qty
			if v, ok := it["requested_qty"]; ok {
				ln.Requested = int(numFromInterface(v))
			}
			ln.Cancelled, _ = it["cancelled"].(bool)
			if !ln.Cancelled {
				r.Units += ln.Qty
			}
			skuSet[ln.SKU] = true
			r.Lines = append(r.Lines, ln)
		}
		out = append(out, r)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	skus := make([]string, 0, len(skuSet))
	for k := range skuSet {
		skus = append(skus, k)
	}
	names := itemNames(schema, skus)
	for i := range out {
		for j := range out[i].Lines {
			out[i].Lines[j].Name = names[out[i].Lines[j].SKU]
		}
	}
	return out, nil
}

// StockRequestReasonList is what the reason picker offers.
type StockRequestReasonList struct {
	BuiltIn       []string           `json:"built_in"`
	Codes         []ReasonCodeOption `json:"codes"`
	CodesRequired bool               `json:"codes_required"`
}

// ListStockRequestReasons returns the built-in reasons and the client's own
// Stock Request reason codes.
func ListStockRequestReasons(tenantID string) (StockRequestReasonList, error) {
	codes, err := ListReasonCodes(tenantID, StockRequestReasonCategory)
	if codes == nil {
		codes = []ReasonCodeOption{}
	}
	return StockRequestReasonList{BuiltIn: StockRequestReasons, Codes: codes, CodesRequired: ReasonCodesRequired(tenantID)}, err
}

// autoRequestLines is what a location's reorder rules say it is short of,
// less items already on an open transfer to it.
func autoRequestLines(tenantID, schema string, loc *requestLocation) ([]StockRequestLineIn, error) {
	sugg, err := GetReplenishmentSuggestionsConfigured(tenantID, loc.ID, stockRequestDefaultLead, stockRequestDefaultSafety)
	if err != nil {
		return nil, err
	}
	refs := []string{loc.ID}
	if loc.Code != "" && loc.Code != loc.ID {
		refs = append(refs, loc.Code)
	}
	open := map[string]bool{}
	rows, err := db.DB.Query(fmt.Sprintf(`
		SELECT COALESCE(data->>'items', '') FROM %s.documents
		 WHERE doctype = 'TransferOrder' AND deleted_at IS NULL
		   AND status IN ('Draft', 'Pending Approval', 'Approved', 'Packed')
		   AND data->>'to_warehouse' = ANY($1)`, schema), pq.Array(refs))
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var items string
		if rows.Scan(&items) != nil {
			continue
		}
		if lines, err := parseTransferItems(items); err == nil {
			for _, l := range lines {
				open[l.Sku] = true
			}
		}
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}
	var out []StockRequestLineIn
	for _, s := range sugg {
		if s.SuggestedQty > 0 && !open[s.SKU] {
			out = append(out, StockRequestLineIn{SKU: s.SKU, Qty: float64(s.SuggestedQty)})
		}
	}
	return out, nil
}

// AutoRunResult is one location's outcome of an auto run.
type AutoRunResult struct {
	Location string              `json:"location"`
	Name     string              `json:"name"`
	Short    int                 `json:"short"`
	Request  *StockRequestResult `json:"request,omitempty"`
	Message  string              `json:"message,omitempty"`
}

// RunAutoStockRequests asks for what a location's reorder rules say it is
// short. With a location named (the Run now button) it runs for that one
// whether or not its switch is on; with none (the daily worker) it runs for
// every active location switched on that has not had an auto request today.
func RunAutoStockRequests(tenantID, location, userID, role, userLocation string) ([]AutoRunResult, error) {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return nil, err
	}
	locs, err := loadRequestLocations(schema)
	if err != nil {
		return nil, err
	}
	var targets []*requestLocation
	if strings.TrimSpace(location) != "" {
		l := locs.get(location)
		if l == nil || !l.active() {
			return nil, stockRequestRefusal("choose an active location")
		}
		if !mayActFor(locs, role, userLocation, l.ID) {
			return nil, stockRequestRefusal("you can ask for stock only for your own location")
		}
		targets = append(targets, l)
	} else {
		for _, l := range locs.byID {
			if l.Auto && l.active() && !autoRequestedToday(schema, l) {
				targets = append(targets, l)
			}
		}
	}
	out := []AutoRunResult{}
	for _, l := range targets {
		r := AutoRunResult{Location: l.ID, Name: l.label()}
		lines, err := autoRequestLines(tenantID, schema, l)
		if err != nil {
			r.Message = err.Error()
			out = append(out, r)
			continue
		}
		r.Short = len(lines)
		if len(lines) == 0 {
			r.Message = "nothing short"
			out = append(out, r)
			continue
		}
		res, err := CreateStockRequest(tenantID, userID, role, userLocation, StockRequestInput{
			Location: l.ID, Reason: "Replenishment", Lines: lines, Origin: "Auto",
		})
		if err != nil {
			r.Message = err.Error()
		} else {
			r.Request = res
		}
		out = append(out, r)
	}
	return out, nil
}

func autoRequestedToday(schema string, l *requestLocation) bool {
	refs := []string{l.ID}
	if l.Code != "" && l.Code != l.ID {
		refs = append(refs, l.Code)
	}
	var n int
	err := db.DB.QueryRow(fmt.Sprintf(`
		SELECT COUNT(*) FROM %s.documents
		 WHERE doctype = 'TransferOrder' AND deleted_at IS NULL AND data->>'request_origin' = 'Auto'
		   AND data->>'requested_on' = $1 AND data->>'to_warehouse' = ANY($2)`, schema), BusinessToday(), pq.Array(refs)).Scan(&n)
	return (err != nil && err != sql.ErrNoRows) || n > 0
}

// StartStockRequestAutoWorker runs the daily auto requests: every interval
// it checks each client whose hour (StockRequestAutoHourSetting, business
// time) it is.
func StartStockRequestAutoWorker(ctx context.Context, interval time.Duration) {
	ticker := time.NewTicker(interval)
	go func() {
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				if !IsSchedulerLeader() || db.DB == nil {
					continue
				}
				schemas, err := sweepTenantSchemas("inventory")
				if err != nil {
					log.Printf("[STOCK-REQUEST] could not list tenant schemas: %v", err)
					continue
				}
				hour := time.Now().In(BusinessLocation()).Hour()
				for _, schema := range schemas {
					tenantID, err := tenantIDForSchema(schema)
					if err != nil {
						continue
					}
					if GetSettingInt(tenantID, StockRequestAutoHourSetting) != hour {
						continue
					}
					res, err := RunAutoStockRequests(tenantID, "", "system", "System", "")
					if err != nil {
						log.Printf("[STOCK-REQUEST] %s: %v", schema, err)
						continue
					}
					for _, r := range res {
						if r.Request != nil && len(r.Request.Transfers) > 0 {
							log.Printf("[STOCK-REQUEST] %s: auto request %s for %s", schema, r.Request.RequestNo, r.Location)
						}
					}
				}
			}
		}
	}()
}
