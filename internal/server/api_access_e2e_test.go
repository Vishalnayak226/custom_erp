package server

import (
	"custom_erp/db"
	"custom_erp/engines"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"
)

// 205.14-205.16, 205.21 (ask 226): the API access build end to end. A key runs
// each listed action's happy path, a report comes back as CSV, a change made
// through the records API, an action or a module's own route queues its
// webhook event, and Fix widens a key so the refused call then works.

const t226Schema = "tenant_default"

func t226Seed(t *testing.T, id, doctype, status string, data map[string]interface{}) {
	t.Helper()
	b, _ := json.Marshal(data)
	db.DB.Exec(`DELETE FROM `+t226Schema+`.documents WHERE id = $1`, id)
	if _, err := db.DB.Exec(`INSERT INTO `+t226Schema+`.documents (id, doctype, data, status, created_by) VALUES ($1, $2, $3, $4, 'system')`, id, doctype, b, status); err != nil {
		t.Fatalf("seed %s: %v", id, err)
	}
}

// t226Outbox waits up to five seconds for the event the change queued: the
// publish runs in the background.
func t226Outbox(eventName, docID string) bool {
	deadline := time.Now().Add(5 * time.Second)
	for {
		var n int
		db.DB.QueryRow(`SELECT COUNT(*) FROM `+t226Schema+`.integration_event_outbox WHERE event_name = $1 AND payload->>'id' = $2`, eventName, docID).Scan(&n)
		if n > 0 {
			return true
		}
		if time.Now().After(deadline) {
			return false
		}
		time.Sleep(100 * time.Millisecond)
	}
}

func t226Status(id string) string {
	var s string
	db.DB.QueryRow(`SELECT status FROM `+t226Schema+`.documents WHERE id = $1`, id).Scan(&s)
	return s
}

func TestAPIAccessE2E(t *testing.T) {
	f := newAPIAccessFixture(t)
	omsMod, _ := engines.ModuleForDoctype("default", "SalesOrder")
	if omsMod == "" {
		t.Skip("no module for SalesOrder")
	}

	rec := f.do(http.MethodPost, "/api/v1/api-access/keys", f.adminToken, map[string]interface{}{
		"name": "t226 e2e key", "modules": map[string]string{f.module: "write", "oms": "write", "inventory": "write"},
		"expires": "30d", "deliver": "show",
	}, nil)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create key: %d %s", rec.Code, rec.Body.String())
	}
	var created struct {
		Credential struct {
			ID string `json:"id"`
		} `json:"credential"`
		APIKey string `json:"api_key"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &created)
	credID, key := created.Credential.ID, created.APIKey
	t.Cleanup(func() {
		db.DB.Exec(`DELETE FROM `+t226Schema+`.documents WHERE doctype = 'WebhookSubscription' AND data->>'api_credential_id' = $1`, credID)
		db.DB.Exec(`DELETE FROM `+t226Schema+`.api_credentials WHERE id = $1`, credID)
	})
	keyPath := "/api/v1/api-access/keys/" + credID

	// A key subscription for every event the key's modules make, so document
	// events are published at all.
	if rec := f.do(http.MethodPost, keyPath+"/webhooks", f.adminToken, map[string]interface{}{"url": "https://example.com/t226"}, nil); rec.Code != http.StatusCreated {
		t.Fatalf("subscribe: %d %s", rec.Code, rec.Body.String())
	}

	seq := 0
	act := func(module, action string, body map[string]interface{}) *httptest.ResponseRecorder {
		seq++
		return f.do(http.MethodPost, "/api/public/v1/actions/"+module+"/"+action, key, body,
			map[string]string{"Idempotency-Key": fmt.Sprintf("t226-act-%d-%d", time.Now().UnixNano(), seq)})
	}

	// Order fixtures: an item with stock at HO and the two reason codes.
	const sku = "T226-SKU"
	orders := []string{}
	t.Cleanup(func() {
		for _, o := range orders {
			db.DB.Exec(`DELETE FROM `+t226Schema+`.gl_postings WHERE document_id IN (SELECT id FROM `+t226Schema+`.documents WHERE doctype = 'FulfillmentTask' AND data->>'order_id' = $1)`, o)
			db.DB.Exec(`DELETE FROM `+t226Schema+`.documents WHERE id = $1 OR data->>'order_id' = $1 OR data->>'sales_order_id' = $1`, o)
			db.DB.Exec(`DELETE FROM `+t226Schema+`.integration_event_outbox WHERE payload->>'id' = $1`, o)
		}
		db.DB.Exec(`DELETE FROM `+t226Schema+`.documents WHERE doctype = 'StockLedgerEntry' AND data->>'item_id' = $1`, sku)
		db.DB.Exec(`DELETE FROM `+t226Schema+`.inventory_reservation WHERE sku = $1`, sku)
		db.DB.Exec(`DELETE FROM `+t226Schema+`.inventory_availability WHERE sku = $1`, sku)
		db.DB.Exec(`DELETE FROM ` + t226Schema + `.documents WHERE id IN ('T226-SKU','T226-RC-HOLD','T226-RC-CANCEL')`)
	})
	db.DB.Exec(`DELETE FROM `+t226Schema+`.inventory_availability WHERE sku = $1`, sku)
	t226Seed(t, sku, "Item", "Active", map[string]interface{}{"code": sku, "name": "T226 Item", "barcode": sku, "status": "Active", "standard_cost": 40, "hsn_code": "71131910", "gst_rate": 3})
	t226Seed(t, "T226-RC-HOLD", "ReasonCode", "Active", map[string]interface{}{"code": "T226-HOLD", "description": "T226 review", "category": "Hold", "status": "Active"})
	t226Seed(t, "T226-RC-CANCEL", "ReasonCode", "Active", map[string]interface{}{"code": "T226-CANCEL", "description": "T226 cancel", "category": "Cancellation", "status": "Active"})
	if _, err := db.DB.Exec(`INSERT INTO `+t226Schema+`.inventory_availability (sku, location_code, on_hand, available) VALUES ($1, 'HO', 100, 100)`, sku); err != nil {
		t.Fatalf("seed stock: %v", err)
	}
	newOrder := func(ref string) string {
		t.Helper()
		id, err := engines.CreateSalesOrder("default", engines.SalesOrderInput{Channel: "T226", ChannelOrderID: ref + "-" + fmt.Sprint(time.Now().UnixNano()),
			CustomerName: "T226 Customer", ShippingAddress: "T226 12 MG Road, Bengaluru 560001", PaymentStatus: "Confirmed", PreferredLocation: "HO",
			Lines: []engines.SalesOrderLineInput{{SKU: sku, Qty: 1, UnitPrice: 100}}})
		if err != nil {
			t.Fatalf("order %s: %v", ref, err)
		}
		orders = append(orders, id)
		if s := t226Status(id); s != "Reserved" {
			t.Fatalf("order %s is %q, want Reserved", ref, s)
		}
		return id
	}

	t.Run("order actions: hold, release hold, release to fulfilment", func(t *testing.T) {
		id := newOrder("A")
		if rec := act("oms", "order-hold", map[string]interface{}{"id": id, "reason_code": "T226-RC-HOLD"}); rec.Code != http.StatusOK {
			t.Fatalf("hold: %d %s", rec.Code, rec.Body.String())
		}
		if s := t226Status(id); s != "On Hold" {
			t.Fatalf("after hold: %q", s)
		}
		if rec := act("oms", "order-release-hold", map[string]interface{}{"id": id}); rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "Reserved") {
			t.Fatalf("release hold: %d %s", rec.Code, rec.Body.String())
		}
		rec := act("oms", "order-release-to-fulfillment", map[string]interface{}{"id": id})
		if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"task_ids"`) {
			t.Fatalf("release to fulfilment: %d %s", rec.Code, rec.Body.String())
		}
		if s := t226Status(id); s != engines.OrderStatusReleased {
			t.Errorf("after release to fulfilment: %q", s)
		}
		// Each status move is an event too.
		if !t226Outbox(omsMod+".SalesOrder.updated", id) {
			t.Errorf("no %s.SalesOrder.updated event for actions on %s", omsMod, id)
		}
	})

	t.Run("an action's cancel queues the cancelled event", func(t *testing.T) {
		id := newOrder("B")
		if rec := act("oms", "order-cancel", map[string]interface{}{"id": id, "reason_code": "T226-RC-CANCEL"}); rec.Code != http.StatusOK {
			t.Fatalf("cancel: %d %s", rec.Code, rec.Body.String())
		}
		if s := t226Status(id); s != "Cancelled" {
			t.Fatalf("after cancel: %q", s)
		}
		if !t226Outbox(omsMod+".SalesOrder.cancelled", id) {
			t.Errorf("no %s.SalesOrder.cancelled event for a cancel through the Actions API", omsMod)
		}
	})

	t.Run("a cancel on the module's own route queues the cancelled event", func(t *testing.T) {
		id := newOrder("C")
		if rec := f.do(http.MethodPost, "/api/v1/orders/"+url.PathEscape(id)+"/cancel", f.adminToken, map[string]string{"reason_code": "T226-RC-CANCEL"}, nil); rec.Code != http.StatusOK {
			t.Fatalf("screen cancel: %d %s", rec.Code, rec.Body.String())
		}
		if !t226Outbox(omsMod+".SalesOrder.cancelled", id) {
			t.Errorf("no %s.SalesOrder.cancelled event for a cancel on /api/v1/orders/{id}/cancel", omsMod)
		}
	})

	t.Run("piece hold: place with the documented body, then release", func(t *testing.T) {
		const serial, item, cust = "T226-SN1", "T226-RING", "T226-CUST"
		prior := engines.GetSettingBool("default", engines.SettingPieceHolds)
		if err := engines.SetSetting("default", engines.SettingPieceHolds, "true", "t226"); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() {
			engines.SetSetting("default", engines.SettingPieceHolds, fmt.Sprint(prior), "t226")
			db.DB.Exec(`DELETE FROM ` + t226Schema + `.documents WHERE id IN ('T226-SN1','T226-CUST')`)
		})
		t226Seed(t, cust, "Customer", "Active", map[string]interface{}{"name": "T226 Customer", "mobile": "9922600001"})
		t226Seed(t, serial, "SerialNumber", "InStock", map[string]interface{}{"serial_no": serial, "item": item, "location_code": "HO", "status": "InStock", "reserved_for": ""})

		// The body the actions list documents must be enough to place a hold.
		var listed struct {
			Data []struct {
				Module string `json:"module"`
				Action string `json:"action"`
				Body   string `json:"body"`
			} `json:"data"`
		}
		rec := f.do(http.MethodGet, "/api/public/v1/actions", key, nil, nil)
		_ = json.Unmarshal(rec.Body.Bytes(), &listed)
		var doc string
		for _, a := range listed.Data {
			if a.Module == "inventory" && a.Action == "piece-hold-place" {
				doc = a.Body
			}
		}
		if !strings.Contains(doc, `"customer_id"`) {
			t.Errorf("piece-hold-place documents %s; the hold needs customer_id", doc)
		}

		until := time.Now().In(engines.BusinessLocation()).AddDate(0, 0, 1).Format("2006-01-02")
		rec = act("inventory", "piece-hold-place", map[string]interface{}{"sku": item, "serial_no": serial, "customer_id": cust, "until": until, "note": "t226"})
		if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), cust) {
			t.Fatalf("place: %d %s", rec.Code, rec.Body.String())
		}
		rec = act("inventory", "piece-hold-release", map[string]interface{}{"sku": item, "serial_no": serial, "reason": "t226 done"})
		if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "released") {
			t.Fatalf("release: %d %s", rec.Code, rec.Body.String())
		}
	})

	t.Run("records API: create and submit queue their events", func(t *testing.T) {
		const vendor = "T226-VENDOR"
		storeHasState(t, "HO")
		t226Seed(t, vendor, "Vendor", "Active", map[string]interface{}{"code": vendor, "name": "T226 Vendor", "state": "Maharashtra"})
		t.Cleanup(func() {
			db.DB.Exec(`DELETE FROM `+t226Schema+`.integration_event_outbox WHERE payload->>'id' IN (SELECT id FROM `+t226Schema+`.documents WHERE doctype='PurchaseOrder' AND data->>'vendor_id'=$1)`, vendor)
			db.DB.Exec(`DELETE FROM `+t226Schema+`.approval_log WHERE document_id IN (SELECT id FROM `+t226Schema+`.documents WHERE doctype='PurchaseOrder' AND data->>'vendor_id'=$1)`, vendor)
			db.DB.Exec(`DELETE FROM `+t226Schema+`.documents WHERE id = $1 OR (doctype='PurchaseOrder' AND data->>'vendor_id'=$1)`, vendor)
		})
		create := func(n int) string {
			t.Helper()
			po := map[string]interface{}{"vendor": vendor, "vendor_id": vendor, "target_warehouse": "HO", "location": "HO",
				"items": `[{"sku":"T226-SKU","qty":1,"rate":10}]`, "gst_mode": engines.GSTModeExclusive, "interstate": false, "total_amount": 10, "status": "Draft"}
			rec := f.do(http.MethodPost, "/api/public/v1/records/PurchaseOrder", key, po, map[string]string{"Idempotency-Key": fmt.Sprintf("t226-po-%d-%d", n, time.Now().UnixNano())})
			var out struct {
				ID string `json:"id"`
			}
			_ = json.Unmarshal(rec.Body.Bytes(), &out)
			if rec.Code >= 300 || out.ID == "" {
				t.Fatalf("create PO: %d %s", rec.Code, rec.Body.String())
			}
			return out.ID
		}
		first := create(1)
		if !t226Outbox(f.module+".PurchaseOrder.created", first) {
			t.Errorf("no %s.PurchaseOrder.created event", f.module)
		}
		rec := f.do(http.MethodPost, "/api/public/v1/records/PurchaseOrder/"+url.PathEscape(first)+"/submit", key, map[string]string{},
			map[string]string{"Idempotency-Key": fmt.Sprintf("t226-submit-%d", time.Now().UnixNano())})
		if rec.Code != http.StatusOK {
			t.Fatalf("submit: %d %s", rec.Code, rec.Body.String())
		}
		if !t226Outbox(f.module+".PurchaseOrder.submitted", first) {
			t.Errorf("no %s.PurchaseOrder.submitted event", f.module)
		}
		// A purchase order has no Cancelled status, so the cancel is refused as on
		// the screen - and a refused change sends no event. (The cancelled event
		// itself is checked above on an order, through both routes.)
		second := create(2)
		rec = f.do(http.MethodPost, "/api/public/v1/records/PurchaseOrder/"+url.PathEscape(second)+"/cancel", key, map[string]string{},
			map[string]string{"Idempotency-Key": fmt.Sprintf("t226-cancel-%d", time.Now().UnixNano())})
		if rec.Code != http.StatusUnprocessableEntity {
			t.Fatalf("records cancel of a PO: %d %s, want 422", rec.Code, rec.Body.String())
		}
		var n int
		db.DB.QueryRow(`SELECT count(*) FROM `+t226Schema+`.integration_event_outbox WHERE event_name = $1 AND payload->>'id' = $2`, f.module+".PurchaseOrder.cancelled", second).Scan(&n)
		if n != 0 || t226Status(second) != "Draft" {
			t.Errorf("a refused cancel moved the PO or sent an event: %d events, status %q", n, t226Status(second))
		}
	})

	t.Run("a report runs as JSON and as CSV with the column labels", func(t *testing.T) {
		rec := f.do(http.MethodGet, "/api/public/v1/reports", key, nil, nil)
		var list struct {
			Data []struct {
				ID     string `json:"id"`
				Params []struct {
					Required bool `json:"required"`
				} `json:"params"`
			} `json:"data"`
		}
		if rec.Code != http.StatusOK || json.Unmarshal(rec.Body.Bytes(), &list) != nil || len(list.Data) == 0 {
			t.Fatalf("reports list: %d %s", rec.Code, rec.Body.String())
		}
		tried := 0
		for _, r := range list.Data {
			needs := false
			for _, p := range r.Params {
				needs = needs || p.Required
			}
			if needs || tried >= 15 {
				continue
			}
			tried++
			js := f.do(http.MethodGet, "/api/public/v1/reports/"+r.ID+"?format=json&page_size=5", key, nil, nil)
			if js.Code != http.StatusOK {
				t.Logf("report %s: %d %.200s", r.ID, js.Code, js.Body.String())
				continue
			}
			var body struct {
				Columns []engines.ReportColumn   `json:"columns"`
				Rows    []map[string]interface{} `json:"rows"`
			}
			if err := json.Unmarshal(js.Body.Bytes(), &body); err != nil || len(body.Columns) == 0 {
				t.Fatalf("report %s JSON: %v %s", r.ID, err, js.Body.String())
			}
			cs := f.do(http.MethodGet, "/api/public/v1/reports/"+r.ID+"?format=csv&page_size=5", key, nil, nil)
			if cs.Code != http.StatusOK || !strings.HasPrefix(cs.Header().Get("Content-Type"), "text/csv") ||
				!strings.Contains(cs.Header().Get("Content-Disposition"), r.ID+".csv") {
				t.Fatalf("report %s CSV: %d %v %s", r.ID, cs.Code, cs.Header(), cs.Body.String())
			}
			records, err := csv.NewReader(strings.NewReader(cs.Body.String())).ReadAll()
			if err != nil || len(records) != len(body.Rows)+1 {
				t.Fatalf("report %s CSV: %d records for %d rows (%v)", r.ID, len(records), len(body.Rows), err)
			}
			for i, c := range body.Columns {
				if records[0][i] != c.Label {
					t.Errorf("report %s CSV header %d = %q, want %q", r.ID, i, records[0][i], c.Label)
				}
			}
			return
		}
		t.Fatalf("no report without required filters ran (tried %d)", tried)
	})

	t.Run("Fix widens the key so the refused call works, audited", func(t *testing.T) {
		otherMod, _ := engines.ModuleForDoctype("default", f.other)
		if rec := f.do(http.MethodGet, "/api/public/v1/records/"+f.other+"?limit=1", key, nil, nil); rec.Code != http.StatusForbidden || apiAccessErrCode(rec) != engines.APIKeyModuleNotOnKey {
			t.Fatalf("before fix: %d %s", rec.Code, rec.Body.String())
		}
		if rec := f.do(http.MethodPost, keyPath+"/fix", f.adminToken, map[string]string{"module": otherMod, "level": "read"}, nil); rec.Code != http.StatusOK {
			t.Fatalf("fix: %d %s", rec.Code, rec.Body.String())
		}
		if rec := f.do(http.MethodGet, "/api/public/v1/records/"+f.other+"?limit=1", key, nil, nil); rec.Code != http.StatusOK {
			t.Fatalf("after fix: %d %s", rec.Code, rec.Body.String())
		}
		var audited int
		db.DB.QueryRow(`SELECT COUNT(*) FROM `+t226Schema+`.audit_logs WHERE action = 'API_KEY_FIXED' AND details LIKE $1`, "%credential="+credID+"%").Scan(&audited)
		if audited != 1 {
			t.Errorf("API_KEY_FIXED audit rows = %d, want 1", audited)
		}
	})
}

// Every pattern a key call re-enters as its owner must be a capability entry:
// a missing one fails closed, so the call is a 403 for every key (ask 226 found
// three that lacked their method).
func TestAsKeyOwnerPatternsAreCapabilities(t *testing.T) {
	patterns := []string{recordsDocPattern, recordsDocIDPattern}
	for _, a := range publicActions {
		patterns = append(patterns, a.Pattern)
	}
	files, _ := filepath.Glob("handlers_public_*.go")
	re := regexp.MustCompile(`asKeyOwner\(w, r, "([^"]+)"\)`)
	for _, f := range files {
		src, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		for _, m := range re.FindAllStringSubmatch(string(src), -1) {
			patterns = append(patterns, m[1])
		}
	}
	if len(patterns) < 10 {
		t.Fatalf("only %d patterns found", len(patterns))
	}
	for _, p := range patterns {
		if _, ok := routeCapabilities[p]; !ok {
			t.Errorf("asKeyOwner pattern %q has no capability entry", p)
		}
	}
}
