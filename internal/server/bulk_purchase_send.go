package server

// Ask 320.1: one upload takes a bill's stock all the way to the store that
// sells it. With "Send to" chosen at Apply, each received bill gets a stock
// transfer from the store that received it: approved - or sent for approval
// where the business's rule says so - sent out, and received there. One
// transfer per bill, found again by its goods receipt, so the same file again
// makes no second one and finishes a transfer that waited for approval.

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"custom_erp/db"
	"custom_erp/engines"
)

// bpSendTo reads Apply's "Send to" store: its id and name, or "" to keep the
// stock where it was received. ok is false once an error has been written.
func bpSendTo(w http.ResponseWriter, r *http.Request, run *bpRun) bool {
	v := strings.TrimSpace(r.FormValue("send_to"))
	if v == "" {
		return true
	}
	id, pending, err := run.sw.resolveLink("Location", v)
	if err != nil || pending || id == "" {
		writeAPIErrorGeneric(w, r, http.StatusUnprocessableEntity, fmt.Sprintf("Send to: no store called %q.", v))
		return false
	}
	if ok, _ := checkPermission(run.sw.c.tenantID, run.sw.c.role, "TransferOrder", "create"); !ok {
		writeAPIErrorGeneric(w, r, http.StatusForbidden, "Your role may not make stock transfers - choose Keep there, or ask an admin.")
		return false
	}
	run.sendTo = id
	run.res.SendTo = run.sw.display("Location", id)
	return true
}

// bpTransferFor is the transfer an upload made for a goods receipt, if any.
func bpTransferFor(schema, grnID string) (id, number, status string) {
	_ = db.DB.QueryRow(fmt.Sprintf(`SELECT id, COALESCE(data->>'transfer_number', ''), COALESCE(NULLIF(status, ''), data->>'status', '')
		FROM %s.documents WHERE doctype = 'TransferOrder' AND deleted_at IS NULL AND data->>'source_grn' = $1
		ORDER BY created_at DESC LIMIT 1`, schema), grnID).Scan(&id, &number, &status)
	return id, number, status
}

func bpTransferStatus(schema, id string) string {
	var status string
	_ = db.DB.QueryRow(fmt.Sprintf(`SELECT COALESCE(NULLIF(status, ''), data->>'status', '') FROM %s.documents
		WHERE doctype = 'TransferOrder' AND id = $1`, schema), id).Scan(&status)
	return status
}

// sendLines is what a bill brought in, per item, as far as the store still
// holds it: an item sold or moved since is not sent again.
func (b *bpRun) sendLines(out *bpBillOut, from string) []map[string]interface{} {
	qty := map[string]int{}
	var order []string
	for _, l := range out.lines {
		q := l.Qty
		if l.HasRecv {
			q = l.Recv
		}
		if q <= 0 || l.sku == "" {
			continue
		}
		if _, seen := qty[l.sku]; !seen {
			order = append(order, l.sku)
		}
		qty[l.sku] += q
	}
	items := []map[string]interface{}{}
	for _, sku := range order {
		var avail int
		_ = db.DB.QueryRow(fmt.Sprintf(`SELECT GREATEST(available - blocked - hold_qty, 0) FROM %s.inventory_availability
			WHERE sku = $1 AND location_code = $2`, b.sw.schema), sku, from).Scan(&avail)
		n := qty[sku]
		if avail < n {
			n = avail
		}
		if n > 0 {
			items = append(items, map[string]interface{}{"sku": sku, "qty": n})
		}
	}
	return items
}

// sendOn takes a received bill's stock from the store that received it to
// Send to, carrying on from the step an earlier upload stopped at.
func (b *bpRun) sendOn(out *bpBillOut, from string) {
	schema := b.sw.schema
	id, number, status := bpTransferFor(schema, out.GRNID)
	if id == "" {
		items := b.sendLines(out, from)
		if len(items) == 0 {
			out.Message = bpJoin(out.Message, "nothing of it is left at the store to send")
			return
		}
		raw, _ := json.Marshal(items)
		var err error
		if id, err = b.saveDoc("TransferOrder", map[string]interface{}{
			"from_warehouse": from, "to_warehouse": b.sendTo, "items": string(raw), "status": "Draft",
			"source_grn": out.GRNID, "remarks": "Bulk PO upload: goods receipt " + firstNonEmptyStr(out.GRNCode, out.GRNID),
		}); err != nil {
			out.TOResult, out.Message = "failed", bpJoin(out.Message, "transfer not made: "+err.Error())
			return
		}
		_, number, status = bpTransferFor(schema, out.GRNID)
	}
	out.TOID, out.TONumber = id, firstNonEmptyStr(number, id)
	call := func(path string, h http.HandlerFunc, body map[string]interface{}) error {
		body["transfer_order_id"] = id
		_, err := b.write("POST "+path, http.MethodPost, path, nil, body, moduleGate("inventory", h))
		status = bpTransferStatus(schema, id)
		return err
	}
	failed := func(step string, err error) {
		out.TOStatus, out.TOResult = status, "failed"
		out.Message = bpJoin(out.Message, fmt.Sprintf("transfer %s, %s: %s", out.TONumber, step, err))
	}
	if status == "Draft" {
		// The business's rule decides: approved here where a transfer of
		// this value may be, else sent for approval.
		if err := call("/api/v1/transfer/approve-direct", handleApproveTransferOrderDirect, map[string]interface{}{}); err != nil && status == "Draft" {
			if err := call("/api/v1/transfer/submit", handleSubmitTransferOrder, map[string]interface{}{}); err != nil {
				failed("send for approval", err)
				return
			}
		}
	}
	if status == "Approved" || status == "Packed" {
		if err := call("/api/v1/transfer/dispatch", handleDispatchTransferOrder, map[string]interface{}{}); err != nil {
			failed("send out", err)
			return
		}
	}
	if status == "Dispatched" || status == "Partially Received" {
		lines, _, err := engines.TransferOpenLines(b.sw.c.tenantID, id)
		if err == nil {
			err = call("/api/v1/transfer/receive", handleReceiveTransferOrder, map[string]interface{}{"received_items": lines})
		}
		if err != nil {
			failed("receive", err)
			return
		}
	}
	out.TOStatus = status
	switch status {
	case "Received":
		out.TOResult = "done"
	case "Pending Approval":
		out.TOResult = "waiting"
		out.Message = bpJoin(out.Message, fmt.Sprintf("transfer %s waits for approval - approve it, then upload the same file again", out.TONumber))
	default:
		out.Message = bpJoin(out.Message, fmt.Sprintf("transfer %s is %s - nothing more sent", out.TONumber, status))
	}
}
