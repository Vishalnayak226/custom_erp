package engines

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"

	"custom_erp/db"
)

// Product merge. Two items that are really one product (a duplicate code) are
// joined: the duplicate's stock and average cost move to the item that stays,
// Draft documents are re-pointed, and the duplicate's code, barcode and
// receipt-date codes keep resolving to the item that stays (ResolveItemBySKU
// follows merged_into). The duplicate is kept as an Inactive record, never
// deleted, so its history still reads. The whole move is one economic
// command: one transaction, replay safe, and a refusal changes nothing.
//
// It is the same inventory account either side, so there is no GL entry.

const itemMergeCommand = "item.merge"

// itemMergeLineDoctypes are the documents that carry an `items` line list.
var itemMergeLineDoctypes = []string{"DeliveryChallan", "InventoryAdjustment", "MaterialIssue", "MaterialReturn",
	"MRPChange", "OnboardingChecklist", "POSInvoice", "PriceListVersion", "PurchaseOrder", "PurchaseRequisition",
	"ReturnRequest", "SalesInvoice", "SalesQuotation", "ShippingPackage", "StoreLabelRequest", "TransferOrder"}

// itemMergeDoneStatuses are the end states: a document there no longer needs
// the item, so it does not block a merge.
var itemMergeDoneStatuses = map[string]bool{"posted": true, "completed": true, "closed": true, "cancelled": true,
	"canceled": true, "rejected": true, "paid": true, "fulfilled": true, "delivered": true, "received": true,
	"void": true, "voided": true, "returned": true, "shipped": true, "invoiced": true, "expired": true,
	"superseded": true, "applied": true, "failed": true, "settled": true}

// itemMergeSameFields must match between the two items: a merge never changes
// how an item is taxed, counted or tracked.
var itemMergeSameFields = []string{"uom", "gst_rate", "hsn_code", "tax_treatment", "tracking_mode", "stock_tracking"}

// ItemMergePreview is what the merge would do, and why it cannot when it cannot.
type ItemMergePreview struct {
	Keep        string             `json:"keep"`
	Duplicate   string             `json:"duplicate"`
	Refusals    []string           `json:"refusals"`
	StockMoves  []ItemMergeStock   `json:"stock_moves"`
	OpenDocs    []string           `json:"open_documents"`
	DraftDocs   []string           `json:"draft_documents"`
	Aliases     []string           `json:"aliases"`
	DuplicateID string             `json:"-"`
	KeepID      string             `json:"-"`
	MoveQty     map[string]float64 `json:"-"`
}

// ItemMergeStock is the stock of the duplicate at one location.
type ItemMergeStock struct {
	Location string  `json:"location"`
	Qty      float64 `json:"qty"`
}

func mergeFail(format string, a ...interface{}) error {
	return &ValidationError{Code: "MASTER-1400", Message: fmt.Sprintf(format, a...)}
}

// MayMergeItems is the role gate: full access, or a role allowed to delete Items.
func MayMergeItems(tenantID, role string) bool {
	if HasFullAccess(role) {
		return true
	}
	ok, err := RoleMayOnDoctype(tenantID, role, "Item", "delete")
	return err == nil && ok
}

func mergeCodeOf(r *ResolvedItem) string { return firstNonEmpty(strField(r.Data, "code"), r.ID) }

// itemsRefersTo reports whether any line of the document names the item.
func mergeLinesRefer(items interface{}, codes map[string]bool) bool {
	for _, l := range jsonLines(items) {
		if codes[strField(l, "sku")] || codes[strField(l, "item_code")] {
			return true
		}
	}
	return false
}

// PreviewItemMerge works out the merge without changing anything.
func PreviewItemMerge(tenantID, keepRef, dupRef string) (*ItemMergePreview, error) {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return nil, err
	}
	keep, errK := resolveItemDirect(tenantID, keepRef)
	dup, errD := resolveItemDirect(tenantID, dupRef)
	if errK != nil {
		return nil, mergeFail("item to keep %q was not found", keepRef)
	}
	if errD != nil {
		return nil, mergeFail("item to merge %q was not found", dupRef)
	}
	p := &ItemMergePreview{Keep: mergeCodeOf(keep), Duplicate: mergeCodeOf(dup), KeepID: keep.ID, DuplicateID: dup.ID,
		Refusals: []string{}, StockMoves: []ItemMergeStock{}, OpenDocs: []string{}, DraftDocs: []string{}, Aliases: []string{}, MoveQty: map[string]float64{}}
	refuse := func(f string, a ...interface{}) { p.Refusals = append(p.Refusals, fmt.Sprintf(f, a...)) }

	if keep.ID == dup.ID {
		refuse("pick two different items")
		return p, nil
	}
	if strField(dup.Data, "merged_into") != "" {
		refuse("%s was already merged into %s", p.Duplicate, strField(dup.Data, "merged_into"))
	}
	if strField(keep.Data, "merged_into") != "" {
		refuse("%s was merged into %s - keep that one instead", p.Keep, strField(keep.Data, "merged_into"))
	}
	if !strings.EqualFold(keep.Status, "Active") {
		refuse("%s is %s - the item you keep must be Active", p.Keep, keep.Status)
	}
	for _, f := range itemMergeSameFields {
		a, b := strings.TrimSpace(fmt.Sprint(keep.Data[f])), strings.TrimSpace(fmt.Sprint(dup.Data[f]))
		if keep.Data[f] == nil {
			a = ""
		}
		if dup.Data[f] == nil {
			b = ""
		}
		if !strings.EqualFold(a, b) {
			refuse("%s differs (%s has %q, %s has %q)", f, p.Keep, a, p.Duplicate, b)
		}
	}
	// Variants: a merge does not reshape a variant family.
	var kids int
	for _, it := range []*ResolvedItem{keep, dup} {
		if strField(it.Data, "parent_product_code") != "" {
			refuse("%s is a variant - merge variants one family at a time is not supported", mergeCodeOf(it))
		}
		_ = db.DB.QueryRow(fmt.Sprintf(`SELECT count(*) FROM %s.documents WHERE doctype='Item' AND deleted_at IS NULL AND data->>'parent_product_code' IN ($1,$2)`, schema),
			mergeCodeOf(it), it.ID).Scan(&kids)
		if kids > 0 {
			refuse("%s has variants - merge its variants first", mergeCodeOf(it))
		}
	}

	codes := map[string]bool{p.Duplicate: true, dup.ID: true}
	for _, d := range itemMergeLineDoctypes {
		rows, err := db.DB.Query(fmt.Sprintf(`SELECT id, status, data FROM %s.documents WHERE doctype = $1 AND deleted_at IS NULL
			AND (data::text LIKE '%%' || $2 || '%%' OR data::text LIKE '%%' || $3 || '%%')`, schema), d, p.Duplicate, dup.ID)
		if err != nil {
			return nil, err
		}
		for rows.Next() {
			var id, status, raw string
			if rows.Scan(&id, &status, &raw) != nil {
				continue
			}
			var data map[string]interface{}
			if json.Unmarshal([]byte(raw), &data) != nil || !mergeLinesRefer(data["items"], codes) {
				continue
			}
			label := d + " " + firstNonEmpty(strField(data, "code"), id)
			switch {
			case strings.EqualFold(status, "Draft"):
				p.DraftDocs = append(p.DraftDocs, d+"|"+id)
			case itemMergeDoneStatuses[strings.ToLower(status)]:
			default:
				p.OpenDocs = append(p.OpenDocs, fmt.Sprintf("%s (%s)", label, status))
			}
		}
		rows.Close()
	}
	if len(p.OpenDocs) > 0 {
		refuse("%s is on %d open document(s): %s - finish or cancel them first", p.Duplicate, len(p.OpenDocs), strings.Join(firstN(p.OpenDocs, 5), ", "))
	}

	// Stock: only plain on-hand moves. Anything set aside, in a bin, a lot or a
	// reservation is somebody's decision and is not guessed at.
	srows, err := db.DB.Query(fmt.Sprintf(`SELECT location_code, COALESCE(on_hand,0),
		COALESCE(reserved,0)+COALESCE(committed,0)+COALESCE(blocked,0)+COALESCE(qc_hold,0)+COALESCE(damaged,0)+COALESCE(hold_qty,0)+
		COALESCE(in_transit,0)+COALESCE(inward,0)+COALESCE(rtv,0)+COALESCE(with_party,0)
		FROM %s.inventory_availability WHERE sku = $1`, schema), p.Duplicate)
	if err != nil {
		return nil, err
	}
	for srows.Next() {
		var loc string
		var onHand, aside float64
		if srows.Scan(&loc, &onHand, &aside) != nil {
			continue
		}
		if aside != 0 {
			refuse("%s has stock set aside at %s (reserved, held, in transit or damaged) - clear it first", p.Duplicate, loc)
		}
		if onHand != float64(int64(onHand)) {
			refuse("%s has a part-unit quantity at %s", p.Duplicate, loc)
		}
		if onHand < 0 {
			refuse("%s is negative at %s - correct it first", p.Duplicate, loc)
		}
		if onHand > 0 {
			p.StockMoves = append(p.StockMoves, ItemMergeStock{Location: loc, Qty: onHand})
			p.MoveQty[loc] = onHand
		}
	}
	srows.Close()
	for _, t := range []string{"bin_stock", "bin_stock_batch", "bin_stock_lpn", "bin_stock_owner", "bin_allocation", "inventory_reservation", "memo_stock_lots"} {
		var n int
		if db.DB.QueryRow(fmt.Sprintf(`SELECT CASE WHEN to_regclass('%s.%s') IS NULL THEN 0 ELSE 1 END`, schema, t)).Scan(&n) == nil && n == 1 {
			var held int
			if db.DB.QueryRow(fmt.Sprintf(`SELECT count(*) FROM %s.%s WHERE sku = $1`, schema, t), p.Duplicate).Scan(&held) == nil && held > 0 {
				refuse("%s still has records in %s - clear them first", p.Duplicate, strings.ReplaceAll(t, "_", " "))
			}
		}
	}
	// Channel links that the keep already has cannot be joined.
	for _, t := range []string{"channel_product_mapping", "shopify_links"} {
		var clash int
		var exists int
		if db.DB.QueryRow(fmt.Sprintf(`SELECT CASE WHEN to_regclass('%s.%s') IS NULL THEN 0 ELSE 1 END`, schema, t)).Scan(&exists) == nil && exists == 1 {
			if db.DB.QueryRow(fmt.Sprintf(`SELECT count(*) FROM %s.%s a JOIN %s.%s b ON b.channel = a.channel WHERE a.sku = $1 AND b.sku = $2`, schema, t, schema, t),
				p.Duplicate, p.Keep).Scan(&clash) == nil && clash > 0 {
				refuse("both items are already linked on the same channel (%s) - unlink one first", strings.ReplaceAll(t, "_", " "))
			}
		}
	}
	p.Aliases = []string{p.Duplicate}
	if b := strField(dup.Data, "barcode"); b != "" {
		p.Aliases = append(p.Aliases, b)
	}
	return p, nil
}

func firstN(s []string, n int) []string {
	if len(s) > n {
		return s[:n]
	}
	return s
}

// resolveItemDirect finds an Item by code, barcode or id without following a merge.
func resolveItemDirect(tenantID, ref string) (*ResolvedItem, error) {
	return resolveItemBySKUHops(tenantID, ref, -1)
}

// MergeItems joins the duplicate into the item that stays. It is the only
// writer of merged_into. role must pass MayMergeItems.
func MergeItems(tenantID, keepRef, dupRef, userID, role string) (map[string]interface{}, error) {
	if !MayMergeItems(tenantID, role) {
		return nil, &ValidationError{Code: "MASTER-1401", Message: "only an admin or a master-data role can merge products"}
	}
	pre, err := PreviewItemMerge(tenantID, keepRef, dupRef)
	if err != nil {
		return nil, err
	}
	if len(pre.Refusals) > 0 {
		return nil, mergeFail("cannot merge: %s", strings.Join(pre.Refusals, "; "))
	}
	result, err := RunEconomicCommand(tenantID, EconomicCommand{Command: itemMergeCommand, Doctype: "Item", DocID: pre.DuplicateID,
		IdempotencyKey: itemMergeCommand + ":" + pre.DuplicateID + ":" + pre.KeepID, Payload: map[string]string{"keep": pre.KeepID, "duplicate": pre.DuplicateID}},
		func(tx *sql.Tx, schema string, dupDoc map[string]interface{}) (map[string]interface{}, error) {
			// Re-check under the lock: a merge or stock change that landed
			// while this waited must not slip through.
			if strField(dupDoc, "merged_into") != "" {
				return nil, mergeFail("%s was already merged into %s", pre.Duplicate, strField(dupDoc, "merged_into"))
			}
			moves := []interface{}{}
			for loc, n := range pre.MoveQty {
				var onHand float64
				if err := tx.QueryRow(fmt.Sprintf(`SELECT COALESCE(on_hand,0) FROM %s.inventory_availability WHERE sku=$1 AND location_code=$2 FOR UPDATE`, schema), pre.Duplicate, loc).Scan(&onHand); err != nil {
					return nil, err
				}
				if onHand != n {
					return nil, &ValidationError{Code: "GLOBAL-0006", Message: fmt.Sprintf("stock of %s at %s changed while merging - try again", pre.Duplicate, loc)}
				}
				moves = append(moves, loc)
			}
			var movedUnits float64
			for _, loc := range moves {
				l := loc.(string)
				n := int(pre.MoveQty[l])
				_, outLines, err := PostInventoryLedgerWithVoucherTx(tx, schema, l, []interface{}{map[string]interface{}{"sku": pre.Duplicate, "qty": float64(-n)}}, false)
				if err != nil {
					return nil, err
				}
				_, inLines, err := PostInventoryLedgerWithVoucherTx(tx, schema, l, []interface{}{map[string]interface{}{"sku": pre.Keep, "qty": float64(n)}}, false)
				if err != nil {
					return nil, err
				}
				voucher := "MERGE-" + pre.Duplicate + "-" + pre.Keep
				if err := WriteStockLedgerLinesTx(tx, schema, l, "ItemMerge", voucher, userID, "", append(outLines, inLines...)); err != nil {
					return nil, err
				}
				movedUnits += float64(n)
			}
			// Cost: the duplicate's units arrive at the duplicate's own average.
			if movedUnits > 0 {
				_, rate, err := lockItemCostTx(tx, schema, pre.Duplicate)
				if err != nil {
					return nil, err
				}
				if err := blendIntoCostTx(tx, schema, pre.Keep, movedUnits, int64(movedUnits)*rate, movedUnits, true); err != nil {
					return nil, err
				}
			}
			// Draft documents now name the item that stays.
			repointed := 0
			for _, ref := range pre.DraftDocs {
				parts := strings.SplitN(ref, "|", 2)
				n, err := mergeRepointDraftTx(tx, schema, parts[0], parts[1], pre)
				if err != nil {
					return nil, err
				}
				repointed += n
			}
			// Receipt-date codes and channel links follow the item.
			if _, err := tx.Exec(fmt.Sprintf(`UPDATE %s.documents SET data = data || jsonb_build_object('item', $2::text, 'item_code', $3::text), updated_at = CURRENT_TIMESTAMP
				WHERE doctype = 'ItemBarcode' AND deleted_at IS NULL AND data->>'item' = $1`, schema), pre.DuplicateID, pre.KeepID, pre.Keep); err != nil {
				return nil, err
			}
			for _, t := range []string{"channel_listings", "channel_product_mapping", "shopify_links"} {
				var exists int
				if tx.QueryRow(fmt.Sprintf(`SELECT CASE WHEN to_regclass('%s.%s') IS NULL THEN 0 ELSE 1 END`, schema, t)).Scan(&exists) == nil && exists == 1 {
					if _, err := tx.Exec(fmt.Sprintf(`UPDATE %s.%s SET sku = $2 WHERE sku = $1`, schema, t), pre.Duplicate, pre.Keep); err != nil {
						return nil, err
					}
				}
			}
			// The item that stays remembers the old codes; the duplicate
			// becomes an Inactive record that points at it.
			if _, err := tx.Exec(fmt.Sprintf(`UPDATE %s.documents SET data = data || jsonb_build_object('alias_codes',
				trim(both ',' from COALESCE(data->>'alias_codes','') || ',' || $2::text)), version = version + 1, updated_at = CURRENT_TIMESTAMP
				WHERE doctype = 'Item' AND id = $1`, schema), pre.KeepID, strings.Join(pre.Aliases, ",")); err != nil {
				return nil, err
			}
			if _, err := tx.Exec(fmt.Sprintf(`UPDATE %s.documents SET status = 'Inactive', data = data || jsonb_build_object('status','Inactive','merged_into', $2::text),
				version = version + 1, updated_at = CURRENT_TIMESTAMP WHERE doctype = 'Item' AND id = $1`, schema), pre.DuplicateID, pre.Keep); err != nil {
				return nil, err
			}
			return map[string]interface{}{"merged": true, "keep": pre.Keep, "duplicate": pre.Duplicate, "units_moved": movedUnits, "drafts_repointed": repointed}, nil
		})
	if err != nil {
		LogAuditEvent(tenantID, userID, "item.merge", "Failure", fmt.Sprintf("%s into %s: %v", pre.Duplicate, pre.Keep, err))
		return nil, err
	}
	LogAuditEvent(tenantID, userID, "item.merge", "Success", fmt.Sprintf("%s into %s: %v units moved, %v draft line set(s) re-pointed", pre.Duplicate, pre.Keep, result["units_moved"], result["drafts_repointed"]))
	return result, nil
}

// mergeRepointDraftTx rewrites the item lines of one Draft document, keeping
// the stored shape (a JSON text or a native array).
func mergeRepointDraftTx(tx *sql.Tx, schema, doctype, id string, pre *ItemMergePreview) (int, error) {
	var raw string
	if err := tx.QueryRow(fmt.Sprintf(`SELECT data::text FROM %s.documents WHERE doctype=$1 AND id=$2 FOR UPDATE`, schema), doctype, id).Scan(&raw); err != nil {
		return 0, err
	}
	var data map[string]interface{}
	if err := json.Unmarshal([]byte(raw), &data); err != nil {
		return 0, err
	}
	lines := jsonLines(data["items"])
	changed := 0
	for _, l := range lines {
		for _, k := range []string{"sku", "item_code"} {
			if v := strField(l, k); v == pre.Duplicate || v == pre.DuplicateID {
				l[k] = pre.Keep
				changed++
			}
		}
	}
	if changed == 0 {
		return 0, nil
	}
	var stored interface{} = lines
	if _, isText := data["items"].(string); isText {
		b, _ := json.Marshal(lines)
		stored = string(b)
	}
	patch, _ := json.Marshal(map[string]interface{}{"items": stored})
	_, err := tx.Exec(fmt.Sprintf(`UPDATE %s.documents SET data = data || $3::jsonb, updated_at = CURRENT_TIMESTAMP WHERE doctype=$1 AND id=$2`, schema), doctype, id, string(patch))
	return changed, err
}
