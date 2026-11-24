package main

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

// The journeys. Each one is the path a person takes, over the real HTTP API,
// as the role that person has. Every journey ends with reconciliation.

func fy(t time.Time) string {
	y := t.Year()
	if t.Month() < time.April {
		y--
	}
	return fmt.Sprintf("%d-%02d", y, (y+1)%100)
}

func j2s(v interface{}) string { b, _ := json.Marshal(v); return string(b) }

func approve(s *StepCtx, who, doctype, id string) {
	sub := s.Call(who, "POST", "/api/v1/approval/submit", map[string]string{"doctype": doctype, "document_id": id})
	s.Check(sub.Status < 300, "submit %s %s: %d %s", doctype, id, sub.Status, trunc(string(sub.Body), 160))
}

func decide(s *StepCtx, who, doctype, id, decision string) *Resp {
	r := s.Call(who, "POST", "/api/v1/approval/decide", map[string]string{"doctype": doctype, "document_id": id, "decision": decision, "comment": "journeylab"})
	s.Check(r.Status < 300, "%s %s %s: %d %s", decision, doctype, id, r.Status, trunc(string(r.Body), 160))
	return r
}

func (r *Runner) finish(j *Journey) JourneyRecord {
	if r.Cfg.InjectFault {
		_ = r.InjectFault()
	}
	return j.Finish(j.Reconcile())
}

// receiveStock raises, approves and receives a purchase order for qty units at
// the warehouse. It is the first half of procure-to-pay and the precondition
// for every journey that needs stock, so it is steps, not a hidden helper.
func (j *Journey) receiveStock(qty int, label string) (poID, grnID string) {
	j.Step(label+"raise purchase order", "jl-ap", func(s *StepCtx) error {
		rate := 200
		r := s.Call("jl-ap", "POST", "/api/v1/doc/PurchaseOrder", map[string]interface{}{
			"vendor_id": vendorID, "vendor": vendorID, "location": locWH, "target_warehouse": locWH, "status": "Draft",
			"items": []map[string]interface{}{{"sku": skuMain, "qty": qty, "rate": rate}}, "total_amount": qty * rate, "code": "x", "po_number": "x"})
		poID = jstr(r.JSON, "id")
		s.Check(r.Status < 300 && poID != "", "purchase order created: %d %s", r.Status, trunc(string(r.Body), 160))
		return nil
	})
	j.Step(label+"submit and approve purchase order", "jl-ap", func(s *StepCtx) error {
		approve(s, "jl-ap", "PurchaseOrder", poID)
		decide(s, "admin", "PurchaseOrder", poID, "Approved")
		return nil
	})
	j.Step(label+"receive goods (GRN)", "jl-ap", func(s *StepCtx) error {
		before := j.R.onHand(skuMain, locWH)
		r := s.Call("jl-ap", "POST", "/api/v1/doc/GRN", map[string]interface{}{
			"po_id": poID, "location": locWH, "status": "Pending",
			"received_items": j2s([]map[string]interface{}{{"sku": skuMain, "qty": qty, "accepted_qty": qty}})})
		grnID = jstr(r.JSON, "id")
		s.Check(r.Status < 300 && grnID != "", "GRN created: %d %s", r.Status, trunc(string(r.Body), 160))
		now := j.R.onHand(skuMain, locWH) - before
		s.Check(int(now) == qty, "stock at %s rose by %d (got %.0f)", locWH, qty, now)
		return nil
	})
	return poID, grnID
}

// J2 procure to pay.
func (r *Runner) JourneyP2P() JourneyRecord {
	j := r.NewJourney("J2", "Procure to pay")
	j.StockBase(skuMain, locWH)
	const qty = 50
	po, grn := j.receiveStock(qty, "")
	j.Expect(skuMain, locWH, qty, "goods received")
	var wrongInv, goodInv string
	j.Step("vendor invoice at a wrong rate", "jl-ap", func(s *StepCtx) error {
		r := s.Call("jl-ap", "POST", "/api/v1/doc/VendorInvoice", map[string]interface{}{
			"invoice_number": fmt.Sprintf("JLINV-W-%d", time.Now().UnixNano()), "vendor_id": vendorID, "po_id": po, "grn_id": grn,
			"invoice_amount": 10500, "financial_year": fy(time.Now()), "status": "Draft",
			"bill_lines": []map[string]interface{}{{"sku": skuMain, "qty": qty, "rate": 200}}})
		wrongInv = jstr(r.JSON, "id")
		s.Check(r.Status < 300 && wrongInv != "", "invoice created: %d %s", r.Status, trunc(string(r.Body), 160))
		return nil
	})
	j.Step("three-way match holds the wrong rate", "jl-ap", func(s *StepCtx) error {
		m := s.Call("jl-ap", "POST", "/api/v1/procurement/vendor-invoice/match", map[string]string{"invoice_id": wrongInv, "po_id": po, "grn_id": grn})
		s.Check(m.Status < 300, "match call: %d %s", m.Status, trunc(string(m.Body), 160))
		pay := s.Call("jl-ap", "POST", "/api/v1/procurement/vendor-invoice/pay", map[string]string{"invoice_id": wrongInv}, 400, 409, 422)
		s.Check(pay.Status >= 400, "payment of a held invoice must be refused (got %d)", pay.Status)
		return nil
	})
	j.Step("vendor invoice at the received rate", "jl-ap", func(s *StepCtx) error {
		r := s.Call("jl-ap", "POST", "/api/v1/doc/VendorInvoice", map[string]interface{}{
			"invoice_number": fmt.Sprintf("JLINV-%d", time.Now().UnixNano()), "vendor_id": vendorID, "po_id": po, "grn_id": grn,
			"invoice_amount": 10000, "financial_year": fy(time.Now()), "status": "Draft",
			"bill_lines": []map[string]interface{}{{"sku": skuMain, "qty": qty, "rate": 190.48}}})
		goodInv = jstr(r.JSON, "id")
		s.Check(r.Status < 300 && goodInv != "", "invoice created: %d %s", r.Status, trunc(string(r.Body), 160))
		return nil
	})
	j.Step("match and pay", "jl-ap", func(s *StepCtx) error {
		m := s.Call("jl-ap", "POST", "/api/v1/procurement/vendor-invoice/match", map[string]string{"invoice_id": goodInv, "po_id": po, "grn_id": grn})
		s.Check(m.Status < 300, "match: %d %s", m.Status, trunc(string(m.Body), 160))
		p := s.Call("jl-ap", "POST", "/api/v1/procurement/vendor-invoice/pay", map[string]string{"invoice_id": goodInv})
		s.Check(p.Status < 300, "pay: %d %s", p.Status, trunc(string(p.Body), 200))
		g := s.SetupCall("jl-ap", "GET", "/api/v1/doc/VendorInvoice/"+goodInv, nil)
		s.Check(jstr(g.JSON, "status") == "Paid", "invoice status Paid (got %q)", jstr(g.JSON, "status"))
		return nil
	})
	return r.finish(j)
}

// J1 first sale: stock the shop from the warehouse, then sell at the till.
func (r *Runner) JourneyFirstSale() JourneyRecord {
	j := r.NewJourney("J1", "First sale")
	const move, sell = 20, 2
	if r.onHand(skuMain, locWH) < move {
		j.receiveStock(50, "stock the warehouse: ")
	}
	j.StockBase(skuMain, locWH)
	j.StockBase(skuMain, locStore)
	var to string
	j.Step("transfer order to the shop", "jl-sm", func(s *StepCtx) error {
		resp := s.Call("jl-sm", "POST", "/api/v1/doc/TransferOrder", map[string]interface{}{
			"transfer_number": "x", "from_warehouse": locWH, "to_warehouse": locStore, "status": "Draft",
			"items": j2s([]map[string]interface{}{{"sku": skuMain, "qty": move}})})
		to = jstr(resp.JSON, "id")
		s.Check(resp.Status < 300 && to != "", "transfer order: %d %s", resp.Status, trunc(string(resp.Body), 160))
		sub := s.Call("jl-sm", "POST", "/api/v1/transfer/submit", map[string]string{"transfer_order_id": to})
		s.Check(sub.Status < 300, "submit transfer: %d %s", sub.Status, trunc(string(sub.Body), 160))
		return nil
	})
	j.Step("approve and dispatch the transfer", "jl-sm", func(s *StepCtx) error {
		decide(s, "admin", "TransferOrder", to, "Approved")
		d := s.Call("jl-sm", "POST", "/api/v1/transfer/dispatch", map[string]string{"transfer_order_id": to})
		s.Check(d.Status < 300, "dispatch: %d %s", d.Status, trunc(string(d.Body), 160))
		return nil
	})
	j.Step("shop receives the transfer", "jl-sms", func(s *StepCtx) error {
		rc := s.Call("jl-sms", "POST", "/api/v1/transfer/receive", map[string]interface{}{"transfer_order_id": to, "received_items": []map[string]interface{}{{"sku": skuMain, "qty": move}}})
		s.Check(rc.Status < 300, "receive: %d %s", rc.Status, trunc(string(rc.Body), 160))
		return nil
	})
	var sid string
	j.Step("open the till", "jl-cash", func(s *StepCtx) error {
		sid = r.openTill(s, "jl-cash")
		s.Check(sid != "", "till session id")
		return nil
	})
	j.Step("quote and checkout", "jl-cash", func(s *StepCtx) error {
		r.sell(s, "jl-cash", sell)
		return nil
	})
	j.Step("close the till", "jl-cash", func(s *StepCtx) error {
		sum := s.SetupCall("jl-cash", "GET", "/api/v1/pos/session/summary?session_id="+sid, nil)
		s.Check(sum.Status < 300, "session summary: %d", sum.Status)
		cl := s.Call("jl-cash", "POST", "/api/v1/pos/session/close", map[string]interface{}{"session_id": sid, "counted_cash": 500 * sell, "declared_card": 0, "declared_upi": 0})
		s.Check(cl.Status < 300, "close: %d %s", cl.Status, trunc(string(cl.Body), 200))
		s.Check(jnum(cl.JSON, "variance") == 0, "till variance is zero (got %v)", jnum(cl.JSON, "variance"))
		return nil
	})
	j.Expect(skuMain, locWH, -move, "transferred to the shop")
	j.Expect(skuMain, locStore, move-sell, "received less the sale")
	return r.finish(j)
}

func (r *Runner) openTill(s *StepCtx, who string) string {
	o := s.Call(who, "POST", "/api/v1/pos/session/open", map[string]interface{}{"location": locStore, "opening_cash": 0})
	sid := firstNonEmpty(jstr(o.JSON, "session_id"), jstr(o.JSON, "id"))
	if sid == "" {
		cur := s.SetupCall(who, "GET", "/api/v1/pos/session/current?location="+locStore, nil)
		sid = firstNonEmpty(jstr(cur.JSON, "session_id"), jstr(cur.JSON, "id"))
	}
	return sid
}

func firstNonEmpty(a ...string) string {
	for _, x := range a {
		if x != "" {
			return x
		}
	}
	return ""
}

// sell quotes and checks out qty units for cash. It returns the bill number.
func (r *Runner) sell(s *StepCtx, who string, qty int) string {
	cart := fmt.Sprintf("JLC-%d", time.Now().UnixNano())
	items := []map[string]interface{}{{"sku": skuMain, "qty": qty}}
	q := s.Call(who, "POST", "/api/v1/pos/quote", map[string]interface{}{"cart_number": cart, "location": locStore, "items": items})
	s.Check(q.Status < 300, "quote: %d %s", q.Status, trunc(string(q.Body), 160))
	c := s.Call(who, "POST", "/api/v1/checkout", map[string]interface{}{"cart_number": cart, "location": locStore, "payment_mode": "Cash", "amount_paid": 500 * qty, "items": items})
	s.Check(c.Status < 300 && jstr(c.JSON, "status") == "completed", "checkout completed: %d %s", c.Status, trunc(string(c.Body), 200))
	bill := jstr(c.JSON, "bill_number")
	s.Check(bill != "", "bill number issued")
	return bill
}

// J3 order to cash.
func (r *Runner) JourneyO2C() JourneyRecord {
	j := r.NewJourney("J3", "Order to cash")
	const qty = 3
	if r.onHand(skuMain, locWH) < qty+5 {
		j.receiveStock(50, "stock the warehouse: ")
	}
	j.StockBase(skuMain, locWH)
	var order, task, pkg, bill, booking, manifest, sinv string
	j.Step("create the order", "admin", func(s *StepCtx) error {
		o := s.Call("admin", "POST", "/api/v1/orders", map[string]interface{}{
			"channel": "Manual", "manual": true, "customer_name": "JL Customer", "customer_phone": "9000000001",
			"shipping_address": "1 Test Road", "shipping_city": "Bengaluru", "shipping_state": "Karnataka", "shipping_pincode": "560001",
			"payment_status": "Confirmed", "lines": []map[string]interface{}{{"sku": skuMain, "qty": qty, "unit_price": 500}}})
		order = jstr(o.JSON, "order_id")
		s.Check(o.Status < 300 && order != "", "order: %d %s", o.Status, trunc(string(o.Body), 160))
		return nil
	})
	j.Step("release to fulfilment", "admin", func(s *StepCtx) error {
		rl := s.Call("admin", "POST", "/api/v1/orders/"+order+"/release-to-fulfillment", map[string]interface{}{})
		if ids, ok := rl.JSON["task_ids"].([]interface{}); ok && len(ids) > 0 {
			task, _ = ids[0].(string)
		}
		s.Check(task != "", "fulfilment task: %d %s", rl.Status, trunc(string(rl.Body), 160))
		return nil
	})
	j.Step("pick and pack", "jl-wh", func(s *StepCtx) error {
		for i := 0; i < qty; i++ {
			p := s.Call("jl-wh", "POST", "/api/v1/wms/pick-scan", map[string]string{"task_id": task, "scan": skuMain})
			s.Check(p.Status < 300, "pick scan %d: %d %s", i+1, p.Status, trunc(string(p.Body), 160))
			k := s.Call("jl-wh", "POST", "/api/v1/wms/pack-scan", map[string]string{"task_id": task, "scan": skuMain})
			s.Check(k.Status < 300, "pack scan %d: %d %s", i+1, k.Status, trunc(string(k.Body), 160))
		}
		pc := s.Call("jl-wh", "POST", "/api/v1/wms/pack-complete", map[string]interface{}{"task_id": task, "weight_kg": 1.2, "documents_confirmed": true})
		s.Check(pc.Status < 300, "pack complete: %d %s", pc.Status, trunc(string(pc.Body), 200))
		return nil
	})
	j.Step("shipping package and invoice", "jl-wh", func(s *StepCtx) error {
		pk := s.Call("jl-wh", "POST", "/api/v1/oms/shipping-packages", map[string]string{"fulfillment_task_id": task})
		pkg = jstr(pk.JSON, "package_id")
		s.Check(pk.Status < 300 && pkg != "", "package: %d %s", pk.Status, trunc(string(pk.Body), 160))
		in := s.Call("jl-wh", "POST", "/api/v1/oms/shipping-packages/"+pkg+"/invoice", map[string]interface{}{})
		bill = jstr(in.JSON, "bill_id")
		s.Check(in.Status < 300 && bill != "", "invoice: %d %s", in.Status, trunc(string(in.Body), 160))
		return nil
	})
	j.Step("book courier, print label", "jl-wh", func(s *StepCtx) error {
		b := s.Call("jl-wh", "POST", "/api/v1/marketplace/logistics/book", map[string]string{"order_id": order, "fulfillment_task_id": task, "carrier": "JLC", "destination_pincode": "560001"})
		booking = jstr(b.JSON, "booking_id")
		s.Check(b.Status < 300 && booking != "", "book: %d %s", b.Status, trunc(string(b.Body), 160))
		l := s.SetupCall("jl-wh", "GET", "/api/v1/marketplace/logistics/label?booking_id="+booking, nil)
		s.Check(l.Status == 200, "label prints: %d", l.Status)
		return nil
	})
	j.Step("manifest and hand over", "jl-wh", func(s *StepCtx) error {
		m := s.Call("jl-wh", "POST", "/api/v1/marketplace/logistics/manifest", map[string]string{"courier": "JLC", "location_code": locWH})
		manifest = jstr(m.JSON, "manifest_id")
		s.Check(m.Status < 300 && manifest != "", "manifest: %d %s", m.Status, trunc(string(m.Body), 200))
		h := s.Call("jl-wh", "POST", "/api/v1/marketplace/logistics/manifest/handover", map[string]string{"manifest_id": manifest})
		s.Check(h.Status < 300, "handover: %d %s", h.Status, trunc(string(h.Body), 160))
		pk := s.SetupCall("jl-wh", "GET", "/api/v1/doc/ShippingPackage/"+pkg, nil)
		sinv = jstr(pk.JSON, "sales_invoice_id")
		s.Check(sinv != "", "ERP sales invoice raised at handover")
		return nil
	})
	j.Step("receive the customer payment", "jl-acct", func(s *StepCtx) error {
		g := s.SetupCall("admin", "GET", "/api/v1/doc/SalesInvoice/"+sinv, nil)
		total := jnum(g.JSON, "total_amount")
		s.Check(total > 0, "invoice total (got %v)", total)
		half := float64(int(total / 2))
		a := s.Call("jl-acct", "POST", "/api/v1/finance/sales-invoice/"+sinv+"/settle", map[string]interface{}{"reference": fmt.Sprintf("JLR-%d-1", time.Now().UnixNano()), "amount": half})
		s.Check(a.Status < 300 && jnum(a.JSON, "outstanding_amount") > 0, "part receipt leaves a balance: %d %s", a.Status, trunc(string(a.Body), 160))
		b := s.Call("jl-acct", "POST", "/api/v1/finance/sales-invoice/"+sinv+"/settle", map[string]interface{}{"reference": fmt.Sprintf("JLR-%d-2", time.Now().UnixNano()), "amount": total - half})
		s.Check(b.Status < 300 && jnum(b.JSON, "outstanding_amount") == 0 && jstr(b.JSON, "status") == "Paid", "full receipt clears it: %d %s", b.Status, trunc(string(b.Body), 160))
		return nil
	})
	j.Expect(skuMain, locWH, -qty, "shipped to the customer")
	return r.finish(j)
}

// J4 warehouse execution: put-away, wave, pick, pack.
func (r *Runner) JourneyWarehouse() JourneyRecord {
	j := r.NewJourney("J4", "Warehouse execution")
	const put, qty = 10, 2
	if r.waiting() < put {
		j.receiveStock(50, "stock the warehouse: ")
	}
	j.StockBase(skuMain, locWH)
	var order, task, wave string
	var binBefore float64
	j.Step("put away into a rack", "jl-wh", func(s *StepCtx) error {
		binBefore = r.binQty()
		w := s.SetupCall("jl-wh", "GET", "/api/v1/wms/inbound-work?location="+locWH, nil)
		s.Check(w.Status < 300, "inbound work list: %d", w.Status)
		p := s.Call("jl-wh", "POST", "/api/v1/wms/putaway", map[string]interface{}{"bin_code": "JL-R1", "sku": skuMain, "qty": put})
		s.Check(p.Status < 300, "put away: %d %s", p.Status, trunc(string(p.Body), 160))
		s.Check(r.binQty() == binBefore+put, "rack holds %d more (got %v)", put, r.binQty()-binBefore)
		return nil
	})
	j.Step("create and release an order", "admin", func(s *StepCtx) error {
		o := s.Call("admin", "POST", "/api/v1/orders", map[string]interface{}{
			"channel": "Manual", "manual": true, "customer_name": "JL Customer 2", "customer_phone": "9000000002",
			"shipping_address": "2 Test Road", "shipping_city": "Bengaluru", "shipping_state": "Karnataka", "shipping_pincode": "560001",
			"payment_status": "Confirmed", "lines": []map[string]interface{}{{"sku": skuMain, "qty": qty, "unit_price": 500}}})
		order = jstr(o.JSON, "order_id")
		rl := s.Call("admin", "POST", "/api/v1/orders/"+order+"/release-to-fulfillment", map[string]interface{}{})
		if ids, ok := rl.JSON["task_ids"].([]interface{}); ok && len(ids) > 0 {
			task, _ = ids[0].(string)
		}
		s.Check(order != "" && task != "", "order %q task %q: %d %s", order, task, rl.Status, trunc(string(rl.Body), 160))
		return nil
	})
	j.Step("build and release a wave", "jl-wh", func(s *StepCtx) error {
		w := s.Call("jl-wh", "POST", "/api/v1/wms/wave/from-selection", map[string]interface{}{"location": locWH, "wave_id": "", "task_ids": []string{task}})
		wave = jstr(w.JSON, "wave_id")
		s.Check(w.Status < 300 && wave != "", "wave: %d %s", w.Status, trunc(string(w.Body), 160))
		t := s.Call("jl-wh", "POST", "/api/v1/wms/wave/transition", map[string]string{"wave_id": wave, "status": "Released"})
		s.Check(t.Status < 300, "release wave: %d %s", t.Status, trunc(string(t.Body), 160))
		return nil
	})
	j.Step("pick the wave", "jl-wh", func(s *StepCtx) error {
		pl := s.SetupCall("jl-wh", "GET", "/api/v1/wms/wave/pick-list?wave_id="+wave, nil)
		lines, _ := pl.JSON["pick_lines"].([]interface{})
		s.Check(len(lines) > 0, "pick list has lines: %d %s", pl.Status, trunc(string(pl.Body), 160))
		for _, l := range lines {
			m, _ := l.(map[string]interface{})
			key := fmt.Sprintf("%s|%s|%s", jstr(m, "sku"), jstr(m, "bin_code"), jstr(m, "batch_no"))
			c := s.Call("jl-wh", "POST", "/api/v1/wms/wave/pick-confirm", map[string]interface{}{"wave_id": wave, "line_key": key, "qty": jnum(m, "pick_qty")})
			s.Check(c.Status < 300, "confirm pick %s: %d %s", key, c.Status, trunc(string(c.Body), 160))
		}
		s.Check(r.binQty() == binBefore+put-qty, "rack fell by the picked quantity (rack now %v, expected %v)", r.binQty(), binBefore+put-qty)
		return nil
	})
	j.Step("pack and make the shipping package", "jl-wh", func(s *StepCtx) error {
		for i := 0; i < qty; i++ {
			k := s.Call("jl-wh", "POST", "/api/v1/wms/pack-scan", map[string]string{"task_id": task, "scan": skuMain})
			s.Check(k.Status < 300, "pack scan: %d %s", k.Status, trunc(string(k.Body), 160))
		}
		pc := s.Call("jl-wh", "POST", "/api/v1/wms/pack-complete", map[string]interface{}{"task_id": task, "weight_kg": 1, "documents_confirmed": true})
		s.Check(pc.Status < 300, "pack complete: %d %s", pc.Status, trunc(string(pc.Body), 160))
		pk := s.Call("jl-wh", "POST", "/api/v1/oms/shipping-packages", map[string]string{"fulfillment_task_id": task})
		s.Check(pk.Status < 300 && jstr(pk.JSON, "package_id") != "", "package: %d %s", pk.Status, trunc(string(pk.Body), 160))
		return nil
	})
	return r.finish(j)
}

func (r *Runner) waiting() float64 {
	resp := r.quiet("jl-wh", "GET", "/api/v1/wms/inbound-work?location="+locWH, nil)
	rows, _ := resp.JSON["putaway"].([]interface{})
	for _, x := range rows {
		m, _ := x.(map[string]interface{})
		if jstr(m, "sku") == skuMain {
			return jnum(m, "waiting")
		}
	}
	return 0
}

func (r *Runner) binQty() float64 {
	resp := r.quiet("jl-wh", "GET", "/api/v1/wms/bin-contents?location_code="+locWH, nil)
	t := 0.0
	for _, x := range resp.Arr {
		m, _ := x.(map[string]interface{})
		if jstr(m, "sku") == skuMain && jstr(m, "bin_code") == "JL-R1" {
			t += jnum(m, "qty")
		}
	}
	return t
}

// J5 period close.
func (r *Runner) JourneyPeriodClose() JourneyRecord {
	j := r.NewJourney("J5", "Period close")
	var pid string
	j.Step("open an accounting period", "jl-acct", func(s *StepCtx) error {
		// A past month that does not overlap one already made, looking back from two months ago.
		now := time.Now()
		for back := 2; back < 40 && pid == ""; back++ {
			first := time.Date(now.Year(), now.Month()-time.Month(back), 1, 0, 0, 0, 0, time.UTC)
			last := first.AddDate(0, 1, -1)
			resp, err := r.Cl.Do("jl-acct", "POST", "/api/v1/finance/periods", map[string]string{"period_name": "JL " + first.Format("2006-01"), "start_date": first.Format("2006-01-02"), "end_date": last.Format("2006-01-02")}, CallOpts{Expect: []int{400, 409, 422}})
			if err == nil && resp.Status < 300 {
				pid = jstr(resp.JSON, "id")
			}
		}
		s.Check(pid != "", "a free month to close was found")
		return nil
	})
	j.Step("close refused before sign-off", "jl-acct", func(s *StepCtx) error {
		c := s.Call("jl-acct", "POST", "/api/v1/finance/periods/"+pid+"/close", map[string]interface{}{}, 409, 422)
		s.Check(c.Status >= 400, "close must be refused while tasks are unsigned (got %d)", c.Status)
		return nil
	})
	j.Step("clear the approval queue", "admin", func(s *StepCtx) error {
		pend := s.SetupCall("admin", "GET", "/api/v1/approval/pending", nil)
		n := 0
		for _, x := range pend.Arr {
			m, _ := x.(map[string]interface{})
			id := firstNonEmpty(jstr(m, "id"), jstr(m, "document_id"))
			if id != "" {
				decide(s, "admin", jstr(m, "doctype"), id, "Rejected")
				n++
			}
		}
		j.Rec.Evidence = append(j.Rec.Evidence, fmt.Sprintf("approval queue cleared: %d document(s) rejected (scratch database)", n))
		return nil
	})
	j.Step("sign off the close tasks", "jl-acct", func(s *StepCtx) error {
		cb := s.SetupCall("jl-acct", "GET", "/api/v1/finance/periods/"+pid+"/close-board", nil)
		tasks, _ := cb.JSON["tasks"].([]interface{})
		s.Check(len(tasks) > 0, "close board has tasks: %d", cb.Status)
		for _, t := range tasks {
			m, _ := t.(map[string]interface{})
			key := jstr(m, "key")
			a := s.Call("jl-acct", "POST", "/api/v1/finance/periods/"+pid+"/close-tasks/"+key+"/assign", map[string]string{"owner": "jl-acct"})
			s.Check(a.Status < 300, "assign %s: %d %s", key, a.Status, trunc(string(a.Body), 160))
			g := s.Call("jl-acct", "POST", "/api/v1/finance/periods/"+pid+"/close-tasks/"+key+"/sign", map[string]string{"status": "Done", "evidence": "journeylab tie-out", "evidence_ref": "journeylab"})
			s.Check(g.Status < 300, "sign %s: %d %s", key, g.Status, trunc(string(g.Body), 160))
		}
		return nil
	})
	j.Step("close the period", "jl-acct", func(s *StepCtx) error {
		ck := s.SetupCall("jl-acct", "GET", "/api/v1/finance/periods/"+pid+"/close-checklist", nil)
		// J2 leaves one vendor invoice in mismatch hold on purpose (the wrong-rate invoice);
		// any other failing check is a real problem. Since 162.12 a close that is not
		// ready needs a typed reason (FIN-0507), so that one expected hold gets one (47.17.2:
		// release 0.28.0's first GitHub run failed J5 here).
		body := map[string]interface{}{}
		if f := failedChecks(ck.JSON); ck.JSON["ready_to_close"] != true && !onlyMismatchHold(f) {
			s.Check(false, "checklist not ready to close: %s", f)
		} else if ck.JSON["ready_to_close"] != true {
			body["reason"] = "journeylab J5: J2's wrong-rate vendor invoice stays in mismatch hold on purpose"
			j.Rec.Evidence = append(j.Rec.Evidence, "closed with a reason, as expected from J2: "+f)
		}
		c := s.Call("jl-acct", "POST", "/api/v1/finance/periods/"+pid+"/close", body)
		s.Check(c.Status < 300, "close: %d %s", c.Status, trunc(string(c.Body), 200))
		return nil
	})
	j.Step("trial balance and period status", "jl-acct", func(s *StepCtx) error {
		tb := s.SetupCall("jl-acct", "GET", "/api/v1/finance/trial-balance?as_of="+time.Now().Format("2006-01-02"), nil)
		s.Check(tb.JSON["balanced"] == true, "trial balance balanced: %d %s", tb.Status, trunc(string(tb.Body), 120))
		ps := s.SetupCall("jl-acct", "GET", "/api/v1/finance/periods", nil)
		st := ""
		for _, x := range ps.Arr {
			m, _ := x.(map[string]interface{})
			if jstr(m, "id") == pid {
				st = jstr(m, "status")
			}
		}
		s.Check(st == "Closed", "period status Closed (got %q)", st)
		return nil
	})
	return r.finish(j)
}

// J6 migration: a small synthetic file, through the real import.
func (r *Runner) JourneyMigration() JourneyRecord {
	j := r.NewJourney("J6", "Migration (item master import)")
	tag := time.Now().Unix() % 100000000
	hdr := "code,name,hsn_code,gst_rate,tax_treatment,sale_price,mrp,standard_cost\n"
	good := hdr + fmt.Sprintf("JLM%d-1,Mig item 1,6109,5,Taxable,100,120,60\nJLM%d-2,Mig item 2,6109,5,Taxable,200,240,120\nJLM%d-3,Mig item 3,6109,5,Taxable,300,360,180\n", tag, tag, tag)
	bad := hdr + fmt.Sprintf("JLM%d-4,Mig item 4,6109,5,Taxable,100,120,60\nJLM%d-5,Mig item 5,6109,5,Taxable,not-a-number,240,120\n", tag, tag)
	imp := func(s *StepCtx, name, csv string, expect ...int) *Resp {
		resp, err := r.Cl.Do("admin", "POST", "/api/v1/import/Item", nil, CallOpts{Expect: expect, Multipart: &multipartBody{Field: "file", Filename: name, Content: []byte(csv)}})
		if err != nil {
			s.errs = append(s.errs, err.Error())
			return &Resp{}
		}
		return resp
	}
	j.Step("download the import template", "admin", func(s *StepCtx) error {
		t := s.SetupCall("admin", "GET", "/api/v1/import/Item/template", nil)
		s.Check(t.Status == 200 && strings.Contains(string(t.Body), "code"), "template: %d", t.Status)
		return nil
	})
	j.Step("import three items", "admin", func(s *StepCtx) error {
		resp := imp(s, "items.csv", good)
		ct, _ := resp.JSON["control_totals"].(map[string]interface{})
		s.Check(resp.Status < 300, "import: %d %s", resp.Status, trunc(string(resp.Body), 240))
		s.Check(jnum(ct, "rows_read") == 3 && jnum(ct, "rows_accepted") == 3 && jnum(ct, "rows_rejected") == 0, "all three rows read and accepted: %s", trunc(j2s(ct), 200))
		s.Check(ct["balanced"] == true, "control totals balance")
		s.Check(jnum(ct, "created") == 3, "three created")
		return nil
	})
	j.Step("import the same file again", "admin", func(s *StepCtx) error {
		resp := imp(s, "items.csv", good)
		ct, _ := resp.JSON["control_totals"].(map[string]interface{})
		s.Check(resp.Status < 300 && jnum(ct, "created") == 0, "a re-run creates nothing new: %d %s", resp.Status, trunc(j2s(ct), 200))
		return nil
	})
	j.Step("import a file with one bad row", "admin", func(s *StepCtx) error {
		resp := imp(s, "items-bad.csv", bad, 400, 422)
		ct, _ := resp.JSON["control_totals"].(map[string]interface{})
		rej := jnum(ct, "rows_rejected")
		if rej == 0 {
			rej = jnum(resp.JSON, "failed_rows")
		}
		s.Check(rej == 1, "exactly one row rejected (status %d): %s", resp.Status, trunc(string(resp.Body), 240))
		return nil
	})
	j.Step("the imported items exist", "admin", func(s *StepCtx) error {
		g := s.SetupCall("admin", "GET", fmt.Sprintf("/api/v1/doc/Item?code=JLM%d-2&limit=1", tag), nil)
		s.Check(g.Status == 200 && len(g.Arr) == 1, "imported item is readable: %d", g.Status)
		return nil
	})
	return r.finish(j)
}

// failedChecks names the close-checklist checks that did not pass, so a red
// run says why instead of dumping the whole checklist.
func failedChecks(m map[string]interface{}) string {
	var out []string
	cs, _ := m["checks"].([]interface{})
	for _, c := range cs {
		cm, _ := c.(map[string]interface{})
		if p, _ := cm["passed"].(bool); !p {
			out = append(out, fmt.Sprintf("%s (%s)", jstr(cm, "name"), jstr(cm, "detail")))
		}
	}
	if len(out) == 0 {
		return "no failing check listed"
	}
	return strings.Join(out, "; ")
}

func onlyMismatchHold(f string) bool {
	return strings.Contains(f, "mismatch hold") && !strings.Contains(f, "; ")
}
