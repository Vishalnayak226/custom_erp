package engines

import (
	"custom_erp/db"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"
)

// Stage 73.2.18 - the planner approves supply proposals.
//
// Reorder suggestions (GetReplenishmentSuggestionsConfigured) and MRP
// suggestions (GetMRPSuggestions, with its raw-material shortfalls) were
// read-only lists. Generating proposals keeps them as SupplyProposal records
// (one open proposal per source, item and location - regenerating refreshes
// it rather than adding a duplicate); the planner then approves a set of
// them as one action - a Draft purchase order to one vendor, a Draft stock
// transfer from one location, or a Draft production order per item - or
// rejects them with a reason. A proposal converts once, and the document it
// became and the proposals it came from point at each other.

// Supply proposal states and actions.
const (
	ProposalOpen      = "Proposed"
	ProposalConverted = "Converted"
	ProposalRejected  = "Rejected"

	ProposalPurchase   = "Purchase"
	ProposalTransfer   = "Transfer"
	ProposalProduction = "Production"
)

// SupplyProposal is one proposal, flattened.
type SupplyProposal struct {
	ID            string  `json:"id"`
	Source        string  `json:"source"` // Reorder / MRP / MRP component
	Sku           string  `json:"sku"`
	ItemName      string  `json:"item_name,omitempty"`
	Location      string  `json:"location"`
	Available     float64 `json:"available"`
	ReorderPoint  float64 `json:"reorder_point"`
	DailyVelocity float64 `json:"daily_velocity,omitempty"`
	SuggestedQty  float64 `json:"suggested_qty"`
	ApprovedQty   float64 `json:"approved_qty,omitempty"`
	BOMID         string  `json:"bom_id,omitempty"`
	ForItem       string  `json:"for_item,omitempty"`
	Action        string  `json:"action"`
	Status        string  `json:"status"`
	TargetDoctype string  `json:"target_doctype,omitempty"`
	TargetID      string  `json:"target_id,omitempty"`
	DecidedBy     string  `json:"decided_by,omitempty"`
	DecidedAt     string  `json:"decided_at,omitempty"`
	Reason        string  `json:"reason,omitempty"`
	GeneratedAt   string  `json:"generated_at,omitempty"`
}

func proposalFromData(id, status string, d map[string]interface{}) SupplyProposal {
	return SupplyProposal{ID: id, Status: status, Source: str(d, "source"), Sku: str(d, "sku"), ItemName: str(d, "item_name"),
		Location: str(d, "location"), Available: numFromInterface(d["available"]), ReorderPoint: numFromInterface(d["reorder_point"]),
		DailyVelocity: numFromInterface(d["daily_velocity"]), SuggestedQty: numFromInterface(d["suggested_qty"]),
		ApprovedQty: numFromInterface(d["approved_qty"]), BOMID: str(d, "bom_id"), ForItem: str(d, "for_item"),
		Action: str(d, "action"), TargetDoctype: str(d, "target_doctype"), TargetID: str(d, "target_id"),
		DecidedBy: str(d, "decided_by"), DecidedAt: str(d, "decided_at"), Reason: str(d, "reason"), GeneratedAt: str(d, "generated_at")}
}

func proposalErr(format string, a ...interface{}) error {
	return &ValidationError{Code: "PLAN-0557", Message: fmt.Sprintf(format, a...)}
}

// GenerateSupplyProposalsResult counts what a run did.
type GenerateSupplyProposalsResult struct {
	Created   int `json:"created"`
	Refreshed int `json:"refreshed"`
}

// GenerateSupplyProposals turns the current reorder ("reorder") or MRP
// ("mrp") suggestions for a location into open proposals.
func GenerateSupplyProposals(tenantID, userID, location, source string, leadTimeDays, safetyStock int) (GenerateSupplyProposalsResult, error) {
	var res GenerateSupplyProposalsResult
	location = strings.TrimSpace(location)
	if location == "" {
		return res, proposalErr("choose the location to plan for")
	}
	if loc, err := ResolveLocationReference(tenantID, location); err == nil {
		location = loc
	}
	type cand struct {
		source, sku, bom, forItem, action string
		available, reorderPoint, velocity float64
		qty                               float64
	}
	var cands []cand
	switch source {
	case "reorder":
		sugg, err := GetReplenishmentSuggestionsConfigured(tenantID, location, leadTimeDays, safetyStock)
		if err != nil {
			return res, err
		}
		for _, s := range sugg {
			if s.SuggestedQty > 0 {
				cands = append(cands, cand{source: "Reorder", sku: s.SKU, action: ProposalPurchase, available: float64(s.Available),
					reorderPoint: float64(s.ReorderPoint), velocity: s.DailyVelocity, qty: float64(s.SuggestedQty)})
			}
		}
	case "mrp":
		sugg, err := GetMRPSuggestions(tenantID, location, leadTimeDays, safetyStock)
		if err != nil {
			return res, err
		}
		for _, s := range sugg {
			if s.SuggestedProductionQty > 0 {
				cands = append(cands, cand{source: "MRP", sku: s.ParentItem, bom: s.BOMID, action: ProposalProduction,
					available: float64(s.Available), reorderPoint: float64(s.ReorderPoint), qty: float64(s.SuggestedProductionQty)})
			}
			for _, r := range s.RawMaterialShortfalls {
				if r.ShortfallQty > 0 {
					cands = append(cands, cand{source: "MRP component", sku: r.Sku, forItem: s.ParentItem, action: ProposalPurchase,
						available: r.Available, reorderPoint: r.Required, qty: r.ShortfallQty})
				}
			}
		}
	default:
		return res, proposalErr("source must be reorder or mrp")
	}
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return res, err
	}
	now := time.Now().UTC().Format(time.RFC3339)
	for _, c := range cands {
		name := c.sku
		if item, _, err := fetchDocData(tenantID, "Item", c.sku); err == nil {
			name = firstNonEmpty(str(item, "name"), c.sku)
		}
		fields := map[string]interface{}{
			"source": c.source, "sku": c.sku, "item_name": name, "location": location, "available": c.available,
			"reorder_point": c.reorderPoint, "daily_velocity": round2(c.velocity), "suggested_qty": round2(c.qty),
			"bom_id": c.bom, "for_item": c.forItem, "generated_at": now,
		}
		patch, _ := json.Marshal(fields)
		r, err := db.DB.Exec(fmt.Sprintf(`
			UPDATE %s.documents SET data = data || $1::jsonb, updated_at = CURRENT_TIMESTAMP
			 WHERE doctype = 'SupplyProposal' AND status = 'Proposed' AND deleted_at IS NULL
			   AND data->>'source' = $2 AND data->>'sku' = $3 AND data->>'location' = $4`, schema),
			patch, c.source, c.sku, location)
		if err != nil {
			return res, err
		}
		if n, _ := r.RowsAffected(); n > 0 {
			res.Refreshed++
			continue
		}
		id := NewDocIDCompact("SP")
		fields["code"], fields["action"], fields["status"] = id, c.action, ProposalOpen
		b, _ := json.Marshal(fields)
		if _, err := db.DB.Exec(fmt.Sprintf(
			`INSERT INTO %s.documents (id, doctype, data, status, created_by) VALUES ($1, 'SupplyProposal', $2, 'Proposed', $3)`, schema),
			id, b, userID); err != nil {
			return res, err
		}
		res.Created++
	}
	LogAuditEvent(tenantID, userID, "SUPPLY_PROPOSALS_GENERATED", "SUCCESS",
		fmt.Sprintf("%s proposals at %s: %d new, %d refreshed", source, location, res.Created, res.Refreshed))
	return res, nil
}

// ListSupplyProposals lists proposals, newest first; blank filters mean all.
func ListSupplyProposals(tenantID, location, status string) ([]SupplyProposal, error) {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return nil, err
	}
	rows, err := db.DB.Query(fmt.Sprintf(`
		SELECT id, COALESCE(status, ''), data FROM %s.documents
		 WHERE doctype = 'SupplyProposal' AND deleted_at IS NULL
		   AND ($1 = '' OR data->>'location' = $1) AND ($2 = '' OR status = $2)
		 ORDER BY created_at DESC, id LIMIT 500`, schema), location, status)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []SupplyProposal{}
	for rows.Next() {
		var id, st, raw string
		if err := rows.Scan(&id, &st, &raw); err != nil {
			return nil, err
		}
		var d map[string]interface{}
		_ = json.Unmarshal([]byte(raw), &d)
		out = append(out, proposalFromData(id, st, d))
	}
	return out, rows.Err()
}

// ApproveProposalsInput is one approval: which proposals, into what.
type ApproveProposalsInput struct {
	IDs          []string           `json:"ids"`
	Action       string             `json:"action"`
	Vendor       string             `json:"vendor"`
	FromLocation string             `json:"from_location"`
	Quantities   map[string]float64 `json:"quantities"`
	Note         string             `json:"note"`
}

// ApproveProposalsResult names the documents created.
type ApproveProposalsResult struct {
	Action    string   `json:"action"`
	Documents []string `json:"documents"`
	Doctype   string   `json:"doctype"`
}

// ApproveSupplyProposals converts open proposals - all at one location - in
// one transaction: Purchase makes one Draft PO to the vendor (rates at item
// cost, for the buyer to confirm), Transfer one Draft transfer from the
// source location, Production one Draft production order per proposal on
// its BOM.
func ApproveSupplyProposals(tenantID, userID, storeCode string, in ApproveProposalsInput) (*ApproveProposalsResult, error) {
	if len(in.IDs) == 0 {
		return nil, proposalErr("choose at least one proposal")
	}
	switch in.Action {
	case ProposalPurchase, ProposalTransfer, ProposalProduction:
	default:
		return nil, proposalErr("approve as Purchase, Transfer or Production")
	}
	if strings.TrimSpace(storeCode) == "" {
		storeCode = "HQ"
	}
	ids := append([]string(nil), in.IDs...)
	sort.Strings(ids)
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return nil, err
	}
	// Read first (outside the lock) to know how many numbers to take; the
	// locked re-read below is what decides.
	fy := documentFinancialYear(time.Now())
	var vendorName string
	if in.Action == ProposalPurchase {
		var ok bool
		if vendorName, ok = vendorNameOf(tenantID, in.Vendor); !ok || strings.TrimSpace(in.Vendor) == "" {
			return nil, proposalErr("choose an active vendor for the purchase order")
		}
		// 49.5.6b: before a number is taken - not for a vendor with no state.
		if err := RequirePOVendorState(tenantID, map[string]interface{}{"vendor": in.Vendor}); err != nil {
			return nil, err
		}
	}
	if in.Action == ProposalTransfer {
		if strings.TrimSpace(in.FromLocation) == "" {
			return nil, proposalErr("choose the location the stock comes from")
		}
		if loc, err := ResolveLocationReference(tenantID, in.FromLocation); err == nil {
			in.FromLocation = loc
		} else {
			return nil, proposalErr("from location: %v", err)
		}
	}
	var numbers []string
	need := 1
	if in.Action == ProposalProduction {
		need = len(ids)
	}
	series := map[string]string{ProposalPurchase: "PO", ProposalTransfer: "TO", ProposalProduction: "PRO"}[in.Action]
	for i := 0; i < need; i++ {
		n, err := GenerateSequence(tenantID, series, storeCode, fy)
		if err != nil {
			return nil, err
		}
		numbers = append(numbers, n)
	}

	tx, err := db.DB.Begin()
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	type prop struct {
		id   string
		data map[string]interface{}
		qty  float64
	}
	var props []prop
	location := ""
	for _, id := range ids {
		d, st, err := lockDocTx(tx, schema, "SupplyProposal", id)
		if err != nil {
			return nil, err
		}
		if st != ProposalOpen {
			return nil, proposalErr("proposal %s is already %s%s", id, st, func() string {
				if t := str(d, "target_id"); t != "" {
					return " (" + t + ")"
				}
				return ""
			}())
		}
		if location == "" {
			location = str(d, "location")
		} else if str(d, "location") != location {
			return nil, proposalErr("approve one location's proposals at a time (%s and %s)", location, str(d, "location"))
		}
		qty := numFromInterface(d["suggested_qty"])
		if q, ok := in.Quantities[id]; ok {
			qty = q
		}
		if !(qty > 0) {
			return nil, proposalErr("proposal %s needs a quantity above zero", id)
		}
		if in.Action != ProposalProduction && qty != float64(int(qty)) {
			qty = float64(int(qty) + 1) // whole units on a PO / transfer line, rounded up
		}
		props = append(props, prop{id: id, data: d, qty: qty})
	}
	if in.Action == ProposalTransfer && in.FromLocation == location {
		return nil, proposalErr("the stock cannot come from %s itself", location)
	}

	res := &ApproveProposalsResult{Action: in.Action}
	now := time.Now().UTC().Format(time.RFC3339)
	insert := func(id, doctype string, data map[string]interface{}) error {
		b, _ := json.Marshal(data)
		_, err := tx.Exec(fmt.Sprintf(
			`INSERT INTO %s.documents (id, doctype, data, status, created_by) VALUES ($1, $2, $3, 'Draft', $4)`, schema), id, doctype, b, userID)
		return err
	}
	target := map[string]string{} // proposal -> document
	switch in.Action {
	case ProposalPurchase:
		type poLine struct {
			SKU  string  `json:"sku"`
			Qty  int     `json:"qty"`
			Rate float64 `json:"rate"`
		}
		var items []poLine
		total := 0.0
		for _, p := range props {
			rate := 0.0
			if c, ok := ResolveQuoteUnitCostPaise(tenantID, str(p.data, "sku")); ok {
				rate = PaiseToRupees(c)
			}
			items = append(items, poLine{SKU: str(p.data, "sku"), Qty: int(p.qty), Rate: rate})
			total += rate * p.qty
			target[p.id] = numbers[0]
		}
		itemsJSON, _ := json.Marshal(items)
		po := map[string]interface{}{
			"code": numbers[0], "po_number": numbers[0], "vendor": in.Vendor, "vendor_id": in.Vendor, "location": location,
			"target_warehouse": location, "items": string(itemsJSON), "total_amount": round2(total), "gst_mode": GSTModeExclusive,
			"source_proposal_ids": strings.Join(ids, ","), "status": "Draft",
		}
		ApplyPlaceOfSupply(tenantID, po) // 49.5.6b: the supply type is derived
		if err := insert(numbers[0], "PurchaseOrder", po); err != nil {
			return nil, err
		}
		res.Doctype, res.Documents = "PurchaseOrder", []string{numbers[0]}
	case ProposalTransfer:
		var items []transferLine
		for _, p := range props {
			items = append(items, transferLine{Sku: str(p.data, "sku"), Qty: int(p.qty)})
			target[p.id] = numbers[0]
		}
		itemsJSON, _ := json.Marshal(items)
		if err := insert(numbers[0], "TransferOrder", map[string]interface{}{
			"transfer_number": numbers[0], "from_warehouse": in.FromLocation, "to_warehouse": location,
			"items": string(itemsJSON), "source_proposal_ids": strings.Join(ids, ","), "status": "Draft",
		}); err != nil {
			return nil, err
		}
		res.Doctype, res.Documents = "TransferOrder", []string{numbers[0]}
	case ProposalProduction:
		for i, p := range props {
			bom := str(p.data, "bom_id")
			if bom == "" {
				if b, err := GetActiveBOMForItem(tenantID, str(p.data, "sku"), ""); err == nil {
					bom = b
				}
			}
			if bom == "" {
				return nil, proposalErr("%s has no active BOM - it can be purchased or transferred, not produced", str(p.data, "sku"))
			}
			if err := insert(numbers[i], "ProductionOrder", map[string]interface{}{
				"code": numbers[i], "bom_id": bom, "quantity": round2(p.qty), "location": location,
				"source_proposal_ids": p.id, "status": "Draft",
			}); err != nil {
				return nil, err
			}
			target[p.id] = numbers[i]
			res.Documents = append(res.Documents, numbers[i])
		}
		res.Doctype = "ProductionOrder"
	}
	for _, p := range props {
		p.data["action"], p.data["approved_qty"] = in.Action, p.qty
		p.data["target_doctype"], p.data["target_id"] = res.Doctype, target[p.id]
		p.data["decided_by"], p.data["decided_at"], p.data["reason"] = userID, now, strings.TrimSpace(in.Note)
		if in.Action == ProposalPurchase {
			p.data["vendor"], p.data["vendor_name"] = in.Vendor, vendorName
		}
		if in.Action == ProposalTransfer {
			p.data["from_location"] = in.FromLocation
		}
		if err := writeDocTx(tx, schema, "SupplyProposal", p.id, p.data, ProposalConverted); err != nil {
			return nil, err
		}
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	LogAuditEvent(tenantID, userID, "SUPPLY_PROPOSALS_APPROVED", "SUCCESS",
		fmt.Sprintf("%d proposal(s) at %s approved as %s -> %s %v", len(props), location, in.Action, res.Doctype, res.Documents))
	return res, nil
}

// RejectSupplyProposals closes open proposals with a reason.
func RejectSupplyProposals(tenantID, userID, reason string, ids []string) error {
	if strings.TrimSpace(reason) == "" {
		return proposalErr("say why the proposal is rejected")
	}
	if len(ids) == 0 {
		return proposalErr("choose at least one proposal")
	}
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return err
	}
	tx, err := db.DB.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	sorted := append([]string(nil), ids...)
	sort.Strings(sorted)
	now := time.Now().UTC().Format(time.RFC3339)
	for _, id := range sorted {
		d, st, err := lockDocTx(tx, schema, "SupplyProposal", id)
		if err != nil {
			return err
		}
		if st != ProposalOpen {
			return proposalErr("proposal %s is already %s", id, st)
		}
		d["decided_by"], d["decided_at"], d["reason"] = userID, now, strings.TrimSpace(reason)
		if err := writeDocTx(tx, schema, "SupplyProposal", id, d, ProposalRejected); err != nil {
			return err
		}
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	LogAuditEvent(tenantID, userID, "SUPPLY_PROPOSALS_REJECTED", "SUCCESS", fmt.Sprintf("%v rejected: %s", sorted, reason))
	return nil
}
