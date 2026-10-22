package engines

import (
	"crypto/sha256"
	"custom_erp/db"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/lib/pq"
)

// Stage 102.3 - the store app's stock work: low stock (102.1.3's alert lands
// here), receive by scanning (102.3.2), count by scanning (102.3.3) and labels
// asked from the phone (102.3.4).
//
// None of these is a second way to move stock. The phone counts and checks;
// what it saves goes through the engine the desktop uses:
//   - a goods receipt check is the receipt's Verify step (VerifyGRNStep, via
//     POST /api/v1/grn-steps/{id}/verify), a transfer-in is
//     ReceiveTransferOrderLines (POST /api/v1/transfer/receive) - this file only
//     reads the document into a scan list;
//   - a spot count becomes ordinary CycleCountLines reconciled by
//     ReconcileCycleCountDetailed (zero variance posts, any other goes for
//     approval); a full count records its numbers on the Physical Inventory's
//     own lines, which the desktop reconciles and closes as before;
//   - labels become a StoreLabelRequest, a transaction with item lines, which
//     the store's computer loads in Sticker Printing > Print from Transaction
//     and prints through its own printer routing (98.4) - a phone has no QZ
//     Tray / PrintBridge of its own, and this keeps one print path.

// storeAppScan is what one scanned code is.
type storeAppScan struct {
	SKU, Name, BatchNo string
}

// storeAppResolveCode reads a scan the way "scan an item" does: a GRN batch
// number first, then code / barcode / id (ResolveItemBySKU).
func storeAppResolveCode(tenantID, code string) (storeAppScan, error) {
	code = strings.TrimSpace(code)
	if code == "" {
		return storeAppScan{}, ErrItemNotFound
	}
	var out storeAppScan
	lookup := code
	if batches, err := GetBatchAgeing(tenantID, code, 0, false); err == nil && len(batches) > 0 && strings.EqualFold(batches[0].BatchNo, code) {
		out.BatchNo, lookup = batches[0].BatchNo, batches[0].Sku
	}
	item, err := ResolveItemBySKU(tenantID, lookup)
	if err != nil {
		return storeAppScan{}, err
	}
	out.SKU = firstNonEmpty(strField(item.Data, "code"), item.ID)
	out.Name = firstNonEmpty(strField(item.Data, "item_name"), strField(item.Data, "name"), out.SKU)
	return out, nil
}

// storeAppItemInfo is an item's name and the codes a scan of it may carry.
type storeAppItemInfo struct {
	Name  string
	Codes []string
}

func storeAppItemInfos(schema string, skus []string) map[string]storeAppItemInfo {
	out := map[string]storeAppItemInfo{}
	if len(skus) == 0 {
		return out
	}
	rows, err := db.DB.Query(fmt.Sprintf(`
		SELECT id, COALESCE(data->>'code', ''), COALESCE(data->>'barcode', ''), COALESCE(NULLIF(data->>'item_name', ''), data->>'name', '')
		FROM %s.documents WHERE doctype = 'Item' AND deleted_at IS NULL AND (id = ANY($1) OR data->>'code' = ANY($1))`, schema), pq.Array(skus))
	if err != nil {
		return out
	}
	defer rows.Close()
	for rows.Next() {
		var id, code, barcode, name string
		if rows.Scan(&id, &code, &barcode, &name) != nil {
			continue
		}
		info := storeAppItemInfo{Name: name}
		for _, c := range []string{code, barcode, id} {
			if c = strings.TrimSpace(c); c != "" {
				info.Codes = append(info.Codes, c)
			}
		}
		key := firstNonEmpty(code, id)
		out[key] = info
		out[id] = info
	}
	return out
}

// storeAppStoreFor checks the store is one this person may work at.
func storeAppStoreFor(tenantID, userID, store string) (string, error) {
	store = strings.TrimSpace(store)
	if store == "" {
		return "", &ValidationError{Code: "GLOBAL-0002", SubFor: "store", Message: "choose your store first"}
	}
	access, err := UserStoreAccess(tenantID, userID)
	if err != nil {
		return "", err
	}
	if !access.Allows(store) {
		return "", &ValidationError{Code: "GLOBAL-0002", SubFor: "store", Message: fmt.Sprintf("you cannot work at %s", store)}
	}
	return store, nil
}

// ---------------------------------------------------------------- low stock

// StoreAppLowItem is one item running low at the store.
type StoreAppLowItem struct {
	SKU    string  `json:"sku"`
	Name   string  `json:"name"`
	Free   float64 `json:"free"`
	Safety float64 `json:"safety"`
}

// StoreAppLowStock is the store's low stock by the Inventory Dashboard's rule.
func StoreAppLowStock(tenantID, userID, store string) ([]StoreAppLowItem, error) {
	store, err := storeAppStoreFor(tenantID, userID, store)
	if err != nil {
		return nil, err
	}
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return nil, err
	}
	low, err := inventoryLowStock(schema, store, 200)
	if err != nil {
		return nil, err
	}
	var skus []string
	for _, r := range low {
		skus = append(skus, fmt.Sprint(r["sku"]))
	}
	infos := storeAppItemInfos(schema, skus)
	out := []StoreAppLowItem{}
	for _, r := range low {
		sku := fmt.Sprint(r["sku"])
		out = append(out, StoreAppLowItem{SKU: sku, Name: firstNonEmpty(infos[sku].Name, sku), Free: numFromInterface(r["free"]), Safety: numFromInterface(r["safety"])})
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Free < out[j].Free })
	return out, nil
}

// ---------------------------------------------------------------- receive

// StoreAppReceiveDoc is one delivery waiting at the store.
type StoreAppReceiveDoc struct {
	Kind    string  `json:"kind"` // grn | transfer
	ID      string  `json:"id"`
	Number  string  `json:"number"`
	From    string  `json:"from"`
	Status  string  `json:"status"`
	Lines   int     `json:"lines"`
	Units   float64 `json:"units"`
	Created string  `json:"created"`
}

// StoreAppReceiveList lists what is waiting to be checked at the store: goods
// receipts at Verify, and transfers on their way here.
func StoreAppReceiveList(tenantID, userID, store string) ([]StoreAppReceiveDoc, error) {
	store, err := storeAppStoreFor(tenantID, userID, store)
	if err != nil {
		return nil, err
	}
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return nil, err
	}
	out := []StoreAppReceiveDoc{}
	inbox, err := ListGRNInbox(tenantID)
	if err != nil {
		return nil, err
	}
	for _, g := range inbox {
		if g.Tab != "verify" || g.Location != store {
			continue
		}
		out = append(out, StoreAppReceiveDoc{Kind: "grn", ID: g.ID, Number: g.Code, From: firstNonEmpty(g.VendorName, g.Vendor),
			Status: "To check", Created: g.CreatedAt})
	}
	rows, err := db.DB.Query(fmt.Sprintf(`
		SELECT id, COALESCE(NULLIF(data->>'transfer_number', ''), id), COALESCE(data->>'from_warehouse', ''), status, data, to_char(created_at, 'YYYY-MM-DD')
		FROM %s.documents WHERE doctype = 'TransferOrder' AND deleted_at IS NULL AND data->>'to_warehouse' = $1
		  AND status IN ('Dispatched', 'In Transit', 'Partially Received')
		ORDER BY created_at LIMIT 100`, schema), store)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var d StoreAppReceiveDoc
		var dataStr string
		if err := rows.Scan(&d.ID, &d.Number, &d.From, &d.Status, &dataStr, &d.Created); err != nil {
			return nil, err
		}
		var data map[string]interface{}
		_ = json.Unmarshal([]byte(dataStr), &data)
		if bal, err := loadTransferBalances(data); err == nil {
			for _, b := range bal {
				if open := b.Open(); open > 0 {
					d.Lines++
					d.Units += float64(open)
				}
			}
		}
		d.Kind = "transfer"
		d.From = storeAppStoreName(schema, d.From)
		out = append(out, d)
	}
	return out, rows.Err()
}

// StoreAppReceiveLine is one line to scan against.
type StoreAppReceiveLine struct {
	Key       string   `json:"key"` // GRN line_no, or the transfer's SKU
	LineNo    int      `json:"line_no,omitempty"`
	SKU       string   `json:"sku"`
	Name      string   `json:"name"`
	Expected  float64  `json:"expected"`
	POOpenQty *float64 `json:"po_open_qty,omitempty"`
	Codes     []string `json:"codes"`
	Tracking  string   `json:"tracking,omitempty"`
}

// StoreAppReceiveDetail is a delivery as a scan list.
type StoreAppReceiveDetail struct {
	Kind        string                `json:"kind"`
	ID          string                `json:"id"`
	Number      string                `json:"number"`
	From        string                `json:"from"`
	Lines       []StoreAppReceiveLine `json:"lines"`
	CanSave     bool                  `json:"can_save"`
	CannotSave  string                `json:"cannot_save,omitempty"`
	ReasonCodes []ReasonCodeOption    `json:"reason_codes,omitempty"`
}

// StoreAppReceiveDocument reads one delivery into a scan list.
func StoreAppReceiveDocument(tenantID, userID, role, store, kind, id string) (*StoreAppReceiveDetail, error) {
	store, err := storeAppStoreFor(tenantID, userID, store)
	if err != nil {
		return nil, err
	}
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return nil, err
	}
	out := &StoreAppReceiveDetail{Kind: kind, ID: id, Lines: []StoreAppReceiveLine{}}
	switch kind {
	case "grn":
		v, err := GetGRNStepView(tenantID, id)
		if err != nil {
			return nil, err
		}
		if v.Location != store {
			return nil, &ValidationError{Code: "GLOBAL-0002", Message: fmt.Sprintf("goods receipt %s is for %s, not this store", v.Code, v.Location)}
		}
		out.Number, out.From = v.Code, firstNonEmpty(v.VendorName, v.Vendor)
		var skus []string
		for _, l := range v.Lines {
			skus = append(skus, l.SKU)
		}
		infos := storeAppItemInfos(schema, skus)
		for _, l := range v.Lines {
			codes := infos[l.SKU].Codes
			if len(codes) == 0 {
				codes = []string{l.SKU}
			}
			out.Lines = append(out.Lines, StoreAppReceiveLine{Key: strconv.Itoa(l.LineNo), LineNo: l.LineNo, SKU: l.SKU,
				Name: firstNonEmpty(l.ItemName, infos[l.SKU].Name, l.SKU), Expected: l.ReceivedQty, POOpenQty: l.POOpenQty, Codes: codes})
		}
		switch {
		case v.Step != GRNStepReceived:
			out.CannotSave = "This goods receipt has already been checked."
		case !GRNStepAllowed(tenantID, role, "verify"):
			out.CannotSave = "You can count it, but saving the check needs someone allowed to verify goods receipts."
		default:
			out.CanSave = true
		}
	case "transfer":
		data, status, err := fetchDocData(tenantID, "TransferOrder", id)
		if err != nil {
			return nil, err
		}
		if strField(data, "to_warehouse") != store {
			return nil, &ValidationError{Code: "GLOBAL-0002", Message: fmt.Sprintf("transfer %s is going to %s, not this store", id, strField(data, "to_warehouse"))}
		}
		out.Number, out.From = firstNonEmpty(strField(data, "transfer_number"), id), storeAppStoreName(schema, strField(data, "from_warehouse"))
		bal, err := loadTransferBalances(data)
		if err != nil {
			return nil, err
		}
		var skus []string
		for _, b := range bal {
			skus = append(skus, b.Sku)
		}
		infos := storeAppItemInfos(schema, skus)
		tracked := ""
		for _, b := range bal {
			open := b.Open()
			if open <= 0 {
				continue
			}
			codes := infos[b.Sku].Codes
			if len(codes) == 0 {
				codes = []string{b.Sku}
			}
			line := StoreAppReceiveLine{Key: b.Sku, SKU: b.Sku, Name: firstNonEmpty(infos[b.Sku].Name, b.Sku), Expected: float64(open), Codes: codes}
			if tr, err := GetItemTracking(tenantID, b.Sku); err == nil && (tr.TracksSerial() || (tr.TracksBatch() && len(b.Lots) > 1)) {
				line.Tracking = tr.Mode
				tracked = b.Sku
			}
			out.Lines = append(out.Lines, line)
		}
		switch {
		case status != "Dispatched" && status != "In Transit" && status != "Partially Received":
			out.CannotSave = fmt.Sprintf("This transfer is %s - nothing is on its way.", strings.ToLower(status))
		case tracked != "":
			out.CannotSave = fmt.Sprintf("%s is tracked by serial number or by more than one lot - receive this transfer on the computer (Stock Movement).", tracked)
		default:
			out.CanSave = true
		}
		out.ReasonCodes, _ = ListReasonCodes(tenantID, TransferDiscrepancyReasonCategory)
	default:
		return nil, &ValidationError{Code: "GLOBAL-0002", SubFor: "kind", Message: "kind must be grn or transfer"}
	}
	return out, nil
}

// ---------------------------------------------------------------- count

// StoreAppCountLine is one counted code: whatever was scanned, and how many.
type StoreAppCountLine struct {
	Code string  `json:"code"`
	Qty  float64 `json:"qty"`
}

// StoreAppCountInput is one phone's count, sent when online. SessionKey is the
// phone's own id for the count: sending it again changes nothing.
type StoreAppCountInput struct {
	SessionKey        string              `json:"session_key"`
	Mode              string              `json:"mode"` // spot | full
	PhysicalInventory string              `json:"physical_inventory"`
	Lines             []StoreAppCountLine `json:"lines"`
}

// StoreAppCountResult says what the count became.
type StoreAppCountResult struct {
	Session         string   `json:"session"`
	Recorded        int      `json:"recorded"`
	Unknown         []string `json:"unknown"`
	Posted          int      `json:"posted"`
	PendingApproval int      `json:"pending_approval"`
	Recounts        int      `json:"recounts"`
	Message         string   `json:"message"`
}

// StoreAppCountSession is an open full count at the store.
type StoreAppCountSession struct {
	ID      string `json:"id"`
	Zone    string `json:"zone,omitempty"`
	Started string `json:"started"`
	Lines   int    `json:"lines"`
	Counted int    `json:"counted"`
}

// StoreAppCountSessions lists the store's open full counts (Physical
// Inventory, started on the computer).
func StoreAppCountSessions(tenantID, userID, store string) ([]StoreAppCountSession, error) {
	store, err := storeAppStoreFor(tenantID, userID, store)
	if err != nil {
		return nil, err
	}
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return nil, err
	}
	rows, err := db.DB.Query(fmt.Sprintf(`
		SELECT p.id, COALESCE(p.data->>'zone', ''), to_char(p.created_at, 'YYYY-MM-DD HH24:MI'),
		       (SELECT COUNT(*) FROM %[1]s.documents l WHERE l.doctype = 'CycleCountLine' AND l.data->>'physical_inventory' = p.id),
		       (SELECT COUNT(*) FROM %[1]s.documents l WHERE l.doctype = 'CycleCountLine' AND l.data->>'physical_inventory' = p.id AND COALESCE(l.data->>'counted_qty', '') <> '')
		FROM %[1]s.documents p WHERE p.doctype = 'PhysicalInventory' AND p.status = 'Counting' AND p.data->>'location' = $1
		ORDER BY p.created_at`, schema), store)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []StoreAppCountSession{}
	for rows.Next() {
		var s StoreAppCountSession
		if err := rows.Scan(&s.ID, &s.Zone, &s.Started, &s.Lines, &s.Counted); err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	return out, rows.Err()
}

func storeAppShortHash(parts ...string) string {
	sum := sha256.Sum256([]byte(strings.Join(parts, "\x00")))
	return strings.ToUpper(hex.EncodeToString(sum[:6]))
}

// StoreAppSubmitCount records a phone's count. Codes are read on the server
// (the phone may have counted offline, unable to look them up); a code that
// is no item comes back in Unknown and is not counted.
func StoreAppSubmitCount(tenantID, userID, username, role, store string, in StoreAppCountInput) (*StoreAppCountResult, error) {
	store, err := storeAppStoreFor(tenantID, userID, store)
	if err != nil {
		return nil, err
	}
	key := strings.TrimSpace(in.SessionKey)
	if key == "" || len(key) > 80 {
		return nil, &ValidationError{Code: "GLOBAL-0002", SubFor: "session_key", Message: "the count has no id - update the app and count again"}
	}
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return nil, err
	}
	counted := map[string]float64{}
	var order []string
	out := &StoreAppCountResult{Unknown: []string{}}
	for _, l := range in.Lines {
		if l.Qty < 0 {
			return nil, &ValidationError{Code: "GLOBAL-0002", SubFor: "qty", Message: fmt.Sprintf("%s has a count below zero", l.Code)}
		}
		scan, err := storeAppResolveCode(tenantID, l.Code)
		if err != nil {
			out.Unknown = append(out.Unknown, strings.TrimSpace(l.Code))
			continue
		}
		if _, seen := counted[scan.SKU]; !seen {
			order = append(order, scan.SKU)
		}
		counted[scan.SKU] += l.Qty
	}
	by := firstNonEmpty(username, userID)
	now := time.Now().Format("2006-01-02 15:04")

	switch in.Mode {
	case "spot", "":
		out.Session = "SPOT-" + storeAppShortHash(store, key)
		for _, sku := range order {
			line, _ := json.Marshal(map[string]interface{}{
				"count_session": out.Session, "location": store, "sku": sku, "counted_qty": counted[sku],
				"status": "Draft", "counted_by": by, "counted_at": now, "source": "Store app",
			})
			res, err := db.DB.Exec(fmt.Sprintf(`INSERT INTO %s.documents (id, doctype, data, status, created_by) VALUES ($1, 'CycleCountLine', $2, 'Draft', $3) ON CONFLICT (id) DO NOTHING`, schema),
				"CCL-"+out.Session+"-"+storeAppShortHash(sku), line, userID)
			if err != nil {
				return nil, err
			}
			if n, _ := res.RowsAffected(); n > 0 {
				out.Recorded++
			}
		}
		rec, err := ReconcileCycleCountDetailed(tenantID, out.Session, userID, role)
		if err != nil {
			return nil, err
		}
		out.Posted, out.PendingApproval, out.Recounts = rec.Posted, rec.PendingApproval, rec.RecountRequested
		switch {
		case len(order) == 0:
			out.Message = "Nothing on this count is an item, so nothing was counted."
		case out.Recorded == 0 && rec.Posted == 0 && rec.PendingApproval == 0:
			out.Message = "This count was already sent."
		default:
			out.Message = fmt.Sprintf("%d item(s) counted: %d matched the stock, %d went for approval.", len(order), rec.Posted, rec.PendingApproval)
			if out.Recounts > 0 {
				out.Message += fmt.Sprintf(" %d need a recount first.", out.Recounts)
			}
		}
	case "full":
		pi := strings.TrimSpace(in.PhysicalInventory)
		var piStatus, piLoc string
		if err := db.DB.QueryRow(fmt.Sprintf(`SELECT status, COALESCE(data->>'location', '') FROM %s.documents WHERE doctype = 'PhysicalInventory' AND id = $1`, schema), pi).Scan(&piStatus, &piLoc); err != nil {
			return nil, &ValidationError{Code: "GLOBAL-0004", SubFor: "physical_inventory", Message: "that full count was not found"}
		}
		if piLoc != store || piStatus != "Counting" {
			return nil, &ValidationError{Code: "GLOBAL-0002", SubFor: "physical_inventory", Message: fmt.Sprintf("that full count is %s at %s - it does not take counts here now", strings.ToLower(piStatus), piLoc)}
		}
		out.Session = pi
		for _, sku := range order {
			changed, err := storeAppRecordFullCount(schema, pi, store, sku, key, counted[sku], userID, by, now)
			if err != nil {
				return nil, err
			}
			if changed {
				out.Recorded++
			}
		}
		out.Message = fmt.Sprintf("%d item(s) recorded on the full count. The count is reconciled and closed on the computer.", out.Recorded)
		if out.Recorded == 0 && len(order) > 0 {
			out.Message = "This count was already sent."
		}
	default:
		return nil, &ValidationError{Code: "GLOBAL-0002", SubFor: "mode", Message: "mode must be spot or full"}
	}
	LogAuditEvent(tenantID, by, "STORE_APP_COUNT", "RECORDED", fmt.Sprintf("%s count %s at %s: %d item(s), %d unknown code(s)", firstNonEmpty(in.Mode, "spot"), out.Session, store, len(order), len(out.Unknown)))
	return out, nil
}

// storeAppRecordFullCount adds one phone's count of an item to the Physical
// Inventory line. Each phone's figure is kept under its count id, so phones
// counting different shelves add up, a resend replaces only its own figure,
// and a figure typed on the computer (counted_qty without phone figures) is
// kept underneath. An item the full count did not list gets a line of its own.
func storeAppRecordFullCount(schema, pi, store, sku, key string, qty float64, userID, by, now string) (bool, error) {
	tx, err := db.DB.Begin()
	if err != nil {
		return false, err
	}
	defer tx.Rollback()
	var id, status, dataStr string
	err = tx.QueryRow(fmt.Sprintf(`SELECT id, status, data FROM %s.documents WHERE doctype = 'CycleCountLine' AND data->>'physical_inventory' = $1 AND data->>'sku' = $2 ORDER BY created_at LIMIT 1 FOR UPDATE`, schema), pi, sku).
		Scan(&id, &status, &dataStr)
	data := map[string]interface{}{}
	if err == sql.ErrNoRows {
		id = "CCL-" + pi + "-X-" + storeAppShortHash(sku)
		status = "Draft"
		data = map[string]interface{}{"count_session": pi, "physical_inventory": pi, "location": store, "sku": sku, "status": "Draft", "not_on_list": true}
		raw, _ := json.Marshal(data)
		if _, err := tx.Exec(fmt.Sprintf(`INSERT INTO %s.documents (id, doctype, data, status, created_by) VALUES ($1, 'CycleCountLine', $2, 'Draft', $3) ON CONFLICT (id) DO NOTHING`, schema), id, raw, userID); err != nil {
			return false, err
		}
	} else if err != nil {
		return false, err
	} else if err := json.Unmarshal([]byte(dataStr), &data); err != nil {
		return false, err
	}
	if status != "Draft" {
		return false, &ValidationError{Code: "GLOBAL-0002", Message: fmt.Sprintf("%s is already %s on this full count - count it again on the computer if needed", sku, strings.ToLower(status))}
	}
	phones, _ := data["store_app_counts"].(map[string]interface{})
	if phones == nil {
		phones = map[string]interface{}{}
	}
	prevSum := 0.0
	for _, v := range phones {
		prevSum += numFromInterface(v)
	}
	base := 0.0
	if raw, ok := data["counted_qty"]; ok && raw != nil && raw != "" {
		base = numFromInterface(raw) - prevSum
		if base < 0 {
			base = 0
		}
	}
	if old, ok := phones[key]; ok && numFromInterface(old) == qty {
		return false, nil
	}
	phones[key] = qty
	newSum := 0.0
	for _, v := range phones {
		newSum += numFromInterface(v)
	}
	data["store_app_counts"] = phones
	data["counted_qty"] = base + newSum
	data["counted_by"], data["counted_at"] = by, now
	raw, _ := json.Marshal(data)
	if _, err := tx.Exec(fmt.Sprintf(`UPDATE %s.documents SET data = $2, updated_at = CURRENT_TIMESTAMP WHERE id = $1`, schema), id, raw); err != nil {
		return false, err
	}
	return true, tx.Commit()
}

// StoreAppSource is a place the store may ask for stock from.
type StoreAppSource struct {
	Code string `json:"code"`
	Name string `json:"name"`
	Type string `json:"type,omitempty"`
}

// StoreAppStockSources lists the warehouses and other stores a transfer could
// come from, warehouses first.
func StoreAppStockSources(tenantID, store string) ([]StoreAppSource, error) {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return nil, err
	}
	rows, err := db.DB.Query(fmt.Sprintf(`
		SELECT id, COALESCE(NULLIF(data->>'name', ''), id), COALESCE(data->>'type', COALESCE(data->>'location_type', ''))
		FROM %s.documents WHERE doctype = 'Location' AND deleted_at IS NULL AND status = 'Active' AND id <> $1
		ORDER BY CASE WHEN LOWER(COALESCE(data->>'type', data->>'location_type', '')) LIKE '%%warehouse%%' THEN 0 ELSE 1 END, 2 LIMIT 200`, schema), store)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []StoreAppSource{}
	for rows.Next() {
		var s StoreAppSource
		if err := rows.Scan(&s.Code, &s.Name, &s.Type); err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	return out, rows.Err()
}

// ---------------------------------------------------------------- labels

// StoreAppLabelLine is one label asked for.
type StoreAppLabelLine struct {
	Code    string `json:"code"`
	Copies  int    `json:"copies"`
	BatchNo string `json:"batch_no,omitempty"`
}

// StoreAppLabelRequest is labels asked from the phone, for the store's computer.
type StoreAppLabelRequest struct {
	ID          string                   `json:"id"`
	Store       string                   `json:"store"`
	RequestedBy string                   `json:"requested_by"`
	Items       []map[string]interface{} `json:"items"`
	Labels      int                      `json:"labels"`
	Note        string                   `json:"note,omitempty"`
	Status      string                   `json:"status"`
	PrintedBy   string                   `json:"printed_by,omitempty"`
	PrintedAt   string                   `json:"printed_at,omitempty"`
	Created     string                   `json:"created"`
}

const storeAppMaxLabels = 500

// StoreAppRequestLabels files labels for the store's computer to print.
func StoreAppRequestLabels(tenantID, userID, username, store string, lines []StoreAppLabelLine, note string) (*StoreAppLabelRequest, error) {
	store, err := storeAppStoreFor(tenantID, userID, store)
	if err != nil {
		return nil, err
	}
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return nil, err
	}
	var items []map[string]interface{}
	total := 0
	for _, l := range lines {
		if l.Copies <= 0 {
			continue
		}
		scan, err := storeAppResolveCode(tenantID, l.Code)
		if err != nil {
			return nil, &ValidationError{Code: "GLOBAL-0002", SubFor: "code", Message: fmt.Sprintf("%s is not an item", l.Code)}
		}
		batch := firstNonEmpty(strings.TrimSpace(l.BatchNo), scan.BatchNo)
		items = append(items, map[string]interface{}{"sku": scan.SKU, "name": scan.Name, "qty": l.Copies, "batch_no": batch})
		total += l.Copies
	}
	if len(items) == 0 {
		return nil, &ValidationError{Code: "GLOBAL-0002", SubFor: "lines", Message: "say which item and how many labels"}
	}
	if total > storeAppMaxLabels {
		return nil, &ValidationError{Code: "GLOBAL-0002", SubFor: "lines", Message: fmt.Sprintf("at most %d labels at a time - print a whole delivery from its goods receipt on the computer", storeAppMaxLabels)}
	}
	by := firstNonEmpty(username, userID)
	id := "LBL-" + store + "-" + strings.ToUpper(strconv.FormatInt(time.Now().UnixMilli(), 36))
	note = strings.TrimSpace(note)
	if len(note) > 200 {
		note = note[:200]
	}
	data, _ := json.Marshal(map[string]interface{}{
		"code": id, "location": store, "requested_by": by, "items": items, "line_count": len(items), "note": note, "status": "Requested",
	})
	if _, err := db.DB.Exec(fmt.Sprintf(`INSERT INTO %s.documents (id, doctype, data, status, created_by) VALUES ($1, 'StoreLabelRequest', $2, 'Requested', $3)`, schema), id, data, userID); err != nil {
		return nil, err
	}
	LogAuditEvent(tenantID, by, "STORE_APP_LABELS", "REQUESTED", fmt.Sprintf("%d label(s) asked for at %s (%s)", total, store, id))
	return &StoreAppLabelRequest{ID: id, Store: store, RequestedBy: by, Items: items, Labels: total, Note: note, Status: "Requested", Created: time.Now().Format("2006-01-02 15:04")}, nil
}

// StoreAppLabelRequests lists the store's recent label requests. Printed is
// read from the sticker print log - the computer's print records the request
// as its source - and the request's status follows it.
func StoreAppLabelRequests(tenantID, userID, store string) ([]StoreAppLabelRequest, error) {
	store, err := storeAppStoreFor(tenantID, userID, store)
	if err != nil {
		return nil, err
	}
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return nil, err
	}
	rows, err := db.DB.Query(fmt.Sprintf(`
		SELECT d.id, d.data, d.status, to_char(d.created_at, 'YYYY-MM-DD HH24:MI'),
		       COALESCE(p.printed_by, ''), COALESCE(to_char(p.printed_at, 'YYYY-MM-DD HH24:MI'), '')
		FROM %[1]s.documents d
		LEFT JOIN LATERAL (SELECT printed_by, printed_at FROM %[1]s.sticker_print_log l
		                   WHERE l.source_doctype = 'StoreLabelRequest' AND l.source_doc_id = d.id ORDER BY printed_at DESC LIMIT 1) p ON TRUE
		WHERE d.doctype = 'StoreLabelRequest' AND d.deleted_at IS NULL AND d.data->>'location' = $1
		ORDER BY d.created_at DESC LIMIT 30`, schema), store)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []StoreAppLabelRequest{}
	var printedNow []string
	for rows.Next() {
		var r StoreAppLabelRequest
		var dataStr string
		if err := rows.Scan(&r.ID, &dataStr, &r.Status, &r.Created, &r.PrintedBy, &r.PrintedAt); err != nil {
			return nil, err
		}
		var data map[string]interface{}
		_ = json.Unmarshal([]byte(dataStr), &data)
		r.Store, r.RequestedBy, r.Note = store, strField(data, "requested_by"), strField(data, "note")
		r.Items = genericLineList(data["items"])
		for _, it := range r.Items {
			r.Labels += int(numFromInterface(it["qty"]))
		}
		if r.PrintedAt != "" && r.Status == "Requested" {
			r.Status = "Printed"
			printedNow = append(printedNow, r.ID)
		}
		out = append(out, r)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if len(printedNow) > 0 {
		_, _ = db.DB.Exec(fmt.Sprintf(`UPDATE %s.documents SET status = 'Printed', data = data || '{"status":"Printed"}'::jsonb, updated_at = CURRENT_TIMESTAMP
			WHERE doctype = 'StoreLabelRequest' AND id = ANY($1) AND status = 'Requested'`, schema), pq.Array(printedNow))
	}
	return out, nil
}
