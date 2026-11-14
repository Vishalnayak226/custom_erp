package main

import (
	"encoding/json"
	"fmt"
	"math"
	"net/url"
	"reflect"
	"time"
)

// E8 is the run sheet's E2E-08: each economic command of the BRD matrix is run
// once with a command key, then sent again with the same key, as a client does
// after a timeout. The second answer must be the first one replayed, and the
// counts the command moves (documents, ledger rows, stock, liability, the key
// table) must be the same before and after the replay. It is not part of the
// seven release-gate journeys; run it with -journeys E8.

// keyed sends a request twice with one command key and checks the replay.
// counts reads the figures the command moves; they must not change on the
// second send.
func (s *StepCtx) keyed(role, method, path, key string, body interface{}, counts func() string) *Resp {
	opt := CallOpts{Headers: map[string]string{"Idempotency-Key": key}}
	first, err := s.J.R.Cl.Do(role, method, path, body, opt)
	if err != nil {
		s.errs = append(s.errs, err.Error())
		return &Resp{}
	}
	if !s.Check(first.Status < 300, "first %s: %d %s", path, first.Status, trunc(string(first.Body), 200)) {
		return first
	}
	s.Check(first.Header.Get("Idempotent-Replayed") == "", "the first send was marked replayed")
	after := counts()
	again, err := s.J.R.Cl.Do(role, method, path, body, opt)
	if err != nil {
		s.errs = append(s.errs, err.Error())
		return first
	}
	s.Check(again.Status == first.Status, "replay status %d, first %d: %s", again.Status, first.Status, trunc(string(again.Body), 200))
	s.Check(again.Header.Get("Idempotent-Replayed") == "true", "replay of %s is not marked Idempotent-Replayed", path)
	s.Check(sameJSON(again.Body, first.Body), "replay of %s answered differently", path)
	if now := counts(); now != after {
		s.errs = append(s.errs, fmt.Sprintf("check failed: the replay of %s moved the counts: %s -> %s", path, after, now))
	}
	return first
}

func (r *Runner) countOf(q string, args ...interface{}) int {
	var n int
	_ = r.DB.QueryRow(fmt.Sprintf(q, r.Cfg.Schema), args...).Scan(&n)
	return n
}

func (r *Runner) strOf(q string, args ...interface{}) string {
	var v string
	_ = r.DB.QueryRow(fmt.Sprintf(q, r.Cfg.Schema), args...).Scan(&v)
	return v
}

func (r *Runner) docCount(doctype string) int {
	return r.countOf(`SELECT COUNT(*) FROM %s.documents WHERE doctype = $1 AND deleted_at IS NULL`, doctype)
}

// JourneyCommandReplay is E2E-08.
func (r *Runner) JourneyCommandReplay() JourneyRecord {
	j := r.NewJourney("E8", "The eleven commands, each run once and replayed")
	stamp := fmt.Sprint(time.Now().UnixNano())
	key := func(name string) string { return "e8-" + name + "-" + stamp }
	j.StockBase(skuMain, locWH)
	j.receiveStock(40, "stock: ")

	ledger := func() string {
		return fmt.Sprintf("gl %d, stock %.0f/%.0f, keys %d", r.countOf(`SELECT COUNT(*) FROM %s.gl_postings`), r.onHand(skuMain, locWH), r.onHand(skuMain, locStore),
			r.countOf(`SELECT COUNT(*) FROM %s.command_idempotency`))
	}

	var challanID, mrID string
	j.Step("inventory adjustment post", "admin", func(s *StepCtx) error {
		counts := func() string { return ledger() + fmt.Sprintf(", adjustments %d", r.docCount("InventoryAdjustment")) }
		before := r.onHand(skuMain, locWH)
		resp := s.keyed("admin", "POST", "/api/v1/inventory/adjustments", key("adj"), map[string]interface{}{
			"location": locWH, "note": "E2E-08", "loss_class": "Shrinkage",
			"lines": []map[string]interface{}{{"sku": skuMain, "qty": -1, "reason_code": "IA-SHRINK", "note": "E2E-08"}}}, counts)
		s.Check(jstr(resp.JSON, "id") != "", "adjustment id")
		s.Check(r.onHand(skuMain, locWH) == before-1, "stock moved once: %.0f -> %.0f", before, r.onHand(skuMain, locWH))
		return nil
	})
	j.Step("challan create and dispatch", "admin", func(s *StepCtx) error {
		counts := func() string { return ledger() + fmt.Sprintf(", challans %d", r.docCount("DeliveryChallan")) }
		c := s.keyed("admin", "POST", "/api/v1/inventory/challans", key("chc"), map[string]interface{}{
			"location": locWH, "purpose": "Non-returnable", "party_name": "E8 party", "lines": []map[string]interface{}{{"sku": skuMain, "qty": 2}}}, counts)
		challanID = jstr(c.JSON, "id")
		if s.Check(challanID != "", "challan id") {
			s.keyed("admin", "POST", "/api/v1/inventory/challans/"+challanID+"/dispatch", key("chd"), map[string]interface{}{}, counts)
		}
		return nil
	})
	j.Step("material request and issue", "admin", func(s *StepCtx) error {
		counts := func() string { return ledger() + fmt.Sprintf(", material issues %d", r.docCount("MaterialIssue")) }
		m := s.keyed("admin", "POST", "/api/v1/inventory/material-requests", key("mrq"), map[string]interface{}{
			"location": locWH, "department": "JL-OPS", "purpose": "E2E-08", "lines": []map[string]interface{}{{"sku": skuMain, "qty": 1}}}, counts)
		mrID = jstr(m.JSON, "id")
		if s.Check(mrID != "", "material request id") {
			before := r.onHand(skuMain, locWH)
			s.keyed("admin", "POST", "/api/v1/inventory/material-requests/"+mrID+"/issue", key("mri"), map[string]interface{}{}, counts)
			s.Check(r.onHand(skuMain, locWH) == before-1, "issue moved stock once: %.0f -> %.0f", before, r.onHand(skuMain, locWH))
		}
		return nil
	})
	var poID string
	j.Step("purchase order send", "jl-ap", func(s *StepCtx) error {
		rate := 200
		p := s.Call("jl-ap", "POST", "/api/v1/doc/PurchaseOrder", map[string]interface{}{
			"vendor_id": vendorID, "vendor": vendorID, "location": locWH, "target_warehouse": locWH, "status": "Draft",
			"items": []map[string]interface{}{{"sku": skuMain, "qty": 5, "rate": rate}}, "total_amount": 5 * rate, "code": "x", "po_number": "x"})
		poID = jstr(p.JSON, "id")
		if !s.Check(poID != "", "purchase order created: %d %s", p.Status, trunc(string(p.Body), 160)) {
			return nil
		}
		approve(s, "jl-ap", "PurchaseOrder", poID)
		decide(s, "admin", "PurchaseOrder", poID, "Approved")
		counts := func() string {
			return ledger() + fmt.Sprintf(", sent %d", r.countOf(`SELECT COUNT(*) FROM %s.documents WHERE doctype = 'PurchaseOrder' AND id = $1 AND data->>'sent_at' IS NOT NULL`, poID))
		}
		s.keyed("jl-ap", "POST", "/api/v1/procurement/purchase-order/"+url.PathEscape(poID)+"/send", key("pos"), map[string]interface{}{}, counts)
		return nil
	})
	var customer string
	j.Step("advance collected at the till", "jl-cash", func(s *StepCtx) error {
		s.SetupCall("jl-cash", "POST", "/api/v1/pos/session/open", map[string]interface{}{"location": locStore, "opening_cash": 0}, 200, 201, 422) // a till left open by an earlier run is fine
		cu := s.SetupCall("admin", "POST", "/api/v1/doc/Customer", map[string]interface{}{"name": "E8 customer " + stamp, "phone": "9" + stamp[len(stamp)-9:], "status": "Active"})
		customer = jstr(cu.JSON, "id")
		if !s.Check(customer != "", "customer: %d %s", cu.Status, trunc(string(cu.Body), 160)) {
			return nil
		}
		counts := func() string {
			return ledger() + fmt.Sprintf(", advances %d", r.countOf(`SELECT COUNT(*) FROM %s.customer_advances`))
		}
		s.keyed("jl-cash", "POST", "/api/v1/pos/advances", key("adv"), map[string]interface{}{
			"location": locStore, "customer_id": customer, "amount": 1000, "payment_mode": "Cash", "purpose": "E2E-08"}, counts)
		return nil
	})
	j.Step("gift card issued at the till", "jl-cash", func(s *StepCtx) error {
		reg := s.SetupCall("admin", "POST", "/api/v1/stored-value/cards", map[string]interface{}{"profile": "Standard Gift Card", "generate": 1})
		nums, _ := reg.JSON["numbers"].([]interface{})
		if !s.Check(len(nums) == 1, "card registered: %d %s", reg.Status, trunc(string(reg.Body), 160)) {
			return nil
		}
		card, _ := nums[0].(string)
		counts := func() string {
			return ledger() + fmt.Sprintf(", stored value %d", r.countOf(`SELECT COUNT(*) FROM %s.stored_value_ledger`))
		}
		s.keyed("jl-cash", "POST", "/api/v1/pos/gift-card/issue", key("gc"), map[string]interface{}{
			"card_number": card, "idempotency_key": key("gc"), "amount": 500, "payment_mode": "Cash", "location": locStore}, counts)
		return nil
	})
	// 74.U.2: a goods receipt in steps, posted with Post all and paid from the
	// receipt. The opening receipt above was at the same rate and the same
	// (inclusive) GST mode, so every price is Same and the quick path is open.
	j.Step("goods receipt post all and pay", "admin", func(s *StepCtx) error {
		// One piece: 200 inclusive is 190.476, booked at 190.48 with no paise
		// for the bill to settle. Five pieces would leave 0.02 (item 173.1).
		const qty, rate = 1, 200
		p := s.Call("jl-ap", "POST", "/api/v1/doc/PurchaseOrder", map[string]interface{}{
			"vendor_id": vendorID, "vendor": vendorID, "location": locWH, "target_warehouse": locWH, "status": "Draft",
			"items": []map[string]interface{}{{"sku": skuMain, "qty": qty, "rate": rate}}, "total_amount": qty * rate, "code": "x", "po_number": "x"})
		po := jstr(p.JSON, "id")
		if !s.Check(po != "", "purchase order for the receipt: %d %s", p.Status, trunc(string(p.Body), 160)) {
			return nil
		}
		approve(s, "jl-ap", "PurchaseOrder", po)
		decide(s, "admin", "PurchaseOrder", po, "Approved")
		// The invoice rate is before GST: 200 with 5% in it is 190.48; the
		// invoice is the lines' total.
		net := math.Round(rate/1.05*100) / 100
		g := s.SetupCall("admin", "POST", "/api/v1/doc/GRN", map[string]interface{}{
			"flow": "steps", "po_ids": po, "vendor": vendorID, "location": locWH,
			"vendor_invoice_no": "E8-INV-" + stamp, "vendor_invoice_date": time.Now().Format("2006-01-02"), "vendor_invoice_amount": math.Round(qty*net*1.05*100) / 100,
			"received_items": j2s([]map[string]interface{}{{"sku": skuMain, "qty": qty, "invoice_rate": net}})})
		grn := jstr(g.JSON, "id")
		if !s.Check(grn != "", "goods receipt in steps: %d %s", g.Status, trunc(string(g.Body), 200)) {
			return nil
		}
		v := s.SetupCall("admin", "GET", "/api/v1/grn-steps/"+url.PathEscape(grn), nil)
		if !s.Check(v.JSON["quick_path"] == true, "the quick path is shut: %v", v.JSON["quick_path_reasons"]) {
			return nil
		}
		counts := func() string {
			return ledger() + fmt.Sprintf(", bills %d, receipt %s", r.docCount("VendorInvoice"),
				r.strOf(`SELECT COALESCE(data->>'grn_step','') || '/' || COALESCE(data->>'grn_payments','') FROM %s.documents WHERE id = $1`, grn))
		}
		before := r.onHand(skuMain, locWH)
		s.keyed("admin", "POST", "/api/v1/grn-steps/"+url.PathEscape(grn)+"/post-all", key("grnpost"), map[string]interface{}{}, counts)
		s.Check(r.onHand(skuMain, locWH) == before+qty, "the receipt moved stock once: %.0f -> %.0f", before, r.onHand(skuMain, locWH))
		s.keyed("admin", "POST", "/api/v1/grn-steps/"+url.PathEscape(grn)+"/pay", key("grnpay"), map[string]interface{}{
			"mode": "Bank", "reference": "E8PAY-" + stamp}, counts)
		return nil
	})
	// 74.U.2: a reprint request on the purchase order printed once. Reprints
	// need an approver, so one rule is set first (a rule left by an earlier
	// run is fine).
	j.Step("reprint request", "admin", func(s *StepCtx) error {
		if poID == "" {
			return nil
		}
		s.SetupCall("admin", "POST", "/api/v1/approval/rules", map[string]interface{}{"doctype": "ReprintRequest", "min_amount": 0, "required_role": "Store Manager"}, 200, 201, 409, 422)
		s.SetupCall("admin", "POST", "/api/v1/print/document/PurchaseOrder/"+url.PathEscape(poID)+"/log", map[string]interface{}{"action": "Print", "copies": 1})
		counts := func() string {
			return ledger() + fmt.Sprintf(", reprint requests %d", r.docCount("ReprintRequest"))
		}
		rq := s.keyed("admin", "POST", "/api/v1/print/document/PurchaseOrder/"+url.PathEscape(poID)+"/reprint-request", key("rpr"),
			map[string]interface{}{"reason": "E2E-08 torn copy"}, counts)
		s.Check(jstr(rq.JSON, "id") != "", "reprint request id")
		return nil
	})
	return r.finish(j)
}

// sameJSON compares two answers by content: a replay is written from the
// kept document, so its keys may come in another order than the first answer.
func sameJSON(a, b []byte) bool {
	var x, y interface{}
	if json.Unmarshal(a, &x) != nil || json.Unmarshal(b, &y) != nil {
		return string(a) == string(b)
	}
	return reflect.DeepEqual(x, y)
}
