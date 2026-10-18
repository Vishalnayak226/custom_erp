package engines

import (
	"custom_erp/db"
	"encoding/json"
	"errors"
	"sort"
	"testing"
)

// TestProcurementDayLimits covers the day settings of 2026-10-17:
// procurement.po_auto_cancel_days (from approval; nothing received ->
// cancelled, partly received -> the rest short-closed) and
// procurement.purchase_return_window_days (accepted stock only).
func TestProcurementDayLimits(t *testing.T) {
	db.InitDB(testConnStr())
	tenantID, schema := uniqueLifecycleTenant(t)
	defer dropLifecycleTenant(tenantID, schema)
	if _, err := ProvisionTenantSchema(tenantID, schema, "0.1.0-test"); err != nil {
		t.Fatalf("ProvisionTenantSchema: %v", err)
	}
	js := func(v interface{}) string { b, _ := json.Marshal(v); return string(b) }
	// insert backdates created_at by ageDays.
	insert := func(id, doctype, status string, ageDays int, data map[string]interface{}) {
		t.Helper()
		raw, _ := json.Marshal(data)
		if _, err := db.DB.Exec("INSERT INTO "+schema+".documents (id, doctype, data, status, created_by, created_at) VALUES ($1, $2, $3, $4, 'system', LOCALTIMESTAMP - make_interval(days => $5))",
			id, doctype, raw, status, ageDays); err != nil {
			t.Fatalf("insert %s: %v", id, err)
		}
	}
	approved := func(po string, ageDays int) {
		t.Helper()
		if _, err := db.DB.Exec("INSERT INTO "+schema+".approval_log (doctype, document_id, action, actor_user_id, actor_role, amount, comment, created_at) VALUES ('PurchaseOrder', $1, 'Approved', 'manager1', 'Store Manager', 0, '', LOCALTIMESTAMP - make_interval(days => $2))",
			po, ageDays); err != nil {
			t.Fatalf("approval %s: %v", po, err)
		}
	}
	set := func(key, value string) {
		t.Helper()
		if err := SetSetting(tenantID, key, value, "test"); err != nil {
			t.Fatalf("SetSetting %s: %v", key, err)
		}
	}
	status := func(id string) (string, map[string]interface{}) {
		t.Helper()
		data, st, err := fetchDocData(tenantID, "PurchaseOrder", id)
		if err != nil {
			t.Fatalf("fetch %s: %v", id, err)
		}
		return st, data
	}
	const sku, vendor, loc = "DL-SHIRT", "DL-V", "DL-LOC"
	insert(sku, "Item", "Active", 0, map[string]interface{}{"code": sku, "name": "Day-limit shirt", "hsn_code": "6205", "gst_rate": 5.0, "tax_treatment": "Taxable"})
	insert(vendor, "Vendor", "Active", 0, map[string]interface{}{"code": vendor, "name": "Day-limit Vendor"})
	po := func(id, st string, ageDays int) {
		insert(id, "PurchaseOrder", st, ageDays, map[string]interface{}{"code": id, "vendor": vendor, "location": loc, "gst_mode": GSTModeExclusive,
			"items": js([]map[string]interface{}{{"sku": sku, "qty": 10, "rate": 100.0}})})
	}
	grn := func(id, poID string, ageDays int, qty, rejected int) {
		line := map[string]interface{}{"sku": sku, "qty": qty, "accepted_qty": qty - rejected}
		if rejected > 0 {
			line["rejected_qty"], line["rejection_reason"] = rejected, "torn"
		}
		insert(id, "GRN", "Posted", ageDays, map[string]interface{}{"code": id, "po_id": poID, "vendor": vendor, "location": loc, "received_items": js([]map[string]interface{}{line})})
	}

	po("DL-PO-NONE", "Approved", 20)
	approved("DL-PO-NONE", 15) // approved 15 days ago, nothing received -> cancelled
	po("DL-PO-HALF", "Approved", 20)
	approved("DL-PO-HALF", 15) // 4 of 10 received -> 6 short-closed
	grn("DL-GRN-HALF", "DL-PO-HALF", 5, 4, 0)
	po("DL-PO-RECENT", "Approved", 20)
	approved("DL-PO-RECENT", 3)        // created long ago, approved 3 days ago -> kept
	po("DL-PO-NORULE", "Approved", 20) // no approval row: counts from creation -> cancelled
	po("DL-PO-DRAFT", "Draft", 30)     // never auto-cancelled
	po("DL-PO-FULL", "Approved", 20)
	approved("DL-PO-FULL", 15) // fully received -> left Approved
	grn("DL-GRN-FULL", "DL-PO-FULL", 5, 10, 0)

	// Off by default.
	if res, err := RunPOAutoCancel(tenantID); err != nil || len(res.Cancelled)+len(res.ShortClosed) != 0 {
		t.Fatalf("setting 0 must do nothing: %v %+v", err, res)
	}

	set("procurement.po_auto_cancel_days", "10")
	res, err := RunPOAutoCancel(tenantID)
	if err != nil || len(res.Errors) != 0 {
		t.Fatalf("run: %v %+v", err, res)
	}
	sort.Strings(res.Cancelled)
	if js(res.Cancelled) != `["DL-PO-NONE","DL-PO-NORULE"]` || js(res.ShortClosed) != `["DL-PO-HALF"]` {
		t.Fatalf("cancelled %v, short-closed %v", res.Cancelled, res.ShortClosed)
	}
	if st, data := status("DL-PO-NONE"); st != "Cancelled" || data["auto_cancelled"] != true || str(data, "cancel_reason") == "" {
		t.Fatalf("DL-PO-NONE: %s %v", st, data)
	}
	var verr *ValidationError
	if _, err := MarkPurchaseOrderSent(tenantID, "DL-PO-NONE", "test"); !errors.As(err, &verr) || verr.Code != "PURCHA-0621" {
		t.Fatalf("sending the auto-cancelled PO: want PURCHA-0621, got %v", err)
	}
	rec, _ := PurchaseOrderReconciliation(tenantID, "DL-PO-HALF")
	if rec.Status != "Closed" || !rec.ShortClosed || rec.Lines[0].Accepted != 4 || rec.Lines[0].ShortClosed != 6 || rec.Lines[0].Open != 0 {
		t.Fatalf("DL-PO-HALF after the run: %+v", rec)
	}
	if _, data := status("DL-PO-HALF"); str(data, "short_close_reason_code") != poAutoCancelReason {
		t.Fatalf("DL-PO-HALF reason %q", str(data, "short_close_reason_code"))
	}
	for id, want := range map[string]string{"DL-PO-RECENT": "Approved", "DL-PO-DRAFT": "Draft", "DL-PO-FULL": "Approved"} {
		if st, _ := status(id); st != want {
			t.Fatalf("%s is %s, want %s", id, st, want)
		}
	}
	// A closed PO takes no receipt; a second run changes nothing.
	if err := validateGRNRules(tenantID, "", map[string]interface{}{"po_id": "DL-PO-HALF", "received_items": js([]map[string]interface{}{{"sku": sku, "qty": 1, "accepted_qty": 1}})}); !errors.As(err, &verr) || verr.Code != "PURCHA-0489" {
		t.Fatalf("receipt on the auto-closed PO: %v", err)
	}
	if res, err := RunPOAutoCancel(tenantID); err != nil || len(res.Cancelled)+len(res.ShortClosed)+len(res.Errors) != 0 {
		t.Fatalf("second run: %v %+v", err, res)
	}

	// Purchase return window: accepted stock only, counted from the receipt.
	po("DL-PO-RET", "Approved", 60)
	grn("DL-GRN-OLD", "DL-PO-RET", 40, 6, 2) // 4 accepted, 2 rejected, 40 days ago
	grn("DL-GRN-NEW", "DL-PO-RET", 5, 4, 0)
	returnOf := func(grnID, bucket string) error {
		t.Helper()
		ctx, err := GetPurchaseReturnable(tenantID, grnID, "")
		if err != nil {
			t.Fatalf("returnable %s: %v", grnID, err)
		}
		_, _, err = resolvePurchaseReturnLines(ctx, []PurchaseReturnLine{{SKU: sku, StockBucket: bucket, Qty: 1}})
		return err
	}
	if err := returnOf("DL-GRN-OLD", PurchaseReturnBucketAccepted); err != nil {
		t.Fatalf("no window set: %v", err)
	}
	set("procurement.purchase_return_window_days", "30")
	if err := returnOf("DL-GRN-OLD", PurchaseReturnBucketAccepted); !errors.As(err, &verr) || verr.Code != "PURCHA-0620" {
		t.Fatalf("accepted goods 40 days after receipt with a 30-day window: want PURCHA-0620, got %v", err)
	}
	if err := returnOf("DL-GRN-OLD", PurchaseReturnBucketRejected); err != nil {
		t.Fatalf("rejected goods are always returnable: %v", err)
	}
	if err := returnOf("DL-GRN-NEW", PurchaseReturnBucketAccepted); err != nil {
		t.Fatalf("5 days after receipt: %v", err)
	}
}
