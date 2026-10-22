package engines

import (
	"custom_erp/db"
	"errors"
	"strings"
	"testing"

	"golang.org/x/crypto/bcrypt"
)

// storeApp102Tenant is a throwaway client with three stores, two items and
// four people: a store manager and a cashier at Andheri (the cashier may also
// work at Bandra), an admin, and a cashier at Colaba only.
func storeApp102Tenant(t *testing.T) (tenantID, schema string) {
	t.Helper()
	db.InitDB(testConnStr())
	tenantID, schema = uniqueLifecycleTenant(t)
	t.Cleanup(func() { dropLifecycleTenant(tenantID, schema) })
	if _, err := ProvisionTenantSchema(tenantID, schema, "0.1.0-test"); err != nil {
		t.Fatalf("ProvisionTenantSchema: %v", err)
	}
	pw := func(p string) string {
		h, _ := bcrypt.GenerateFromPassword([]byte(p), bcrypt.MinCost)
		return string(h)
	}
	if _, err := db.DB.Exec(`INSERT INTO ` + schema + `.documents (doctype, id, data, status, created_by) VALUES
		('Location', 'ST-A', '{"name":"Andheri","type":"Store"}', 'Active', 'admin'),
		('Location', 'ST-B', '{"name":"Bandra","type":"Store"}', 'Active', 'admin'),
		('Location', 'ST-C', '{"name":"Colaba","type":"Store"}', 'Active', 'admin'),
		('Location', 'WH-1', '{"name":"Central warehouse","type":"Warehouse"}', 'Active', 'admin'),
		('Item', 'SA2-TEE', '{"code":"SA2-TEE","name":"Linen tee","barcode":"8900000102001","sale_price":500,"mrp":799}', 'Active', 'admin'),
		('Item', 'SA2-CAP', '{"code":"SA2-CAP","name":"Cotton cap","barcode":"8900000102002","sale_price":200,"mrp":299}', 'Active', 'admin'),
		('Salesman', 'SM-1', '{"name":"Ravi"}', 'Active', 'admin')`); err != nil {
		t.Fatalf("seed: %v", err)
	}
	if _, err := db.DB.Exec(`INSERT INTO `+schema+`.users (id, username, password_hash, role, status, location_code, credential_version, store_access, stores) VALUES
		('u-mgr', 'mgr102', $1, 'Store Manager', 'Active', 'ST-A', 1, 'Home', '{}'),
		('u-cash', 'cash102', $2, 'Cashier', 'Active', 'ST-A', 1, 'Selected', '{ST-B}'),
		('u-adm', 'adm102', $3, 'Admin', 'Active', 'ST-A', 1, 'All', '{}'),
		('u-col', 'col102', $4, 'Cashier', 'Active', 'ST-C', 1, 'Home', '{}')`,
		pw("pw-mgr"), pw("pw-cash"), pw("pw-adm"), pw("pw-col")); err != nil {
		t.Fatalf("seed users: %v", err)
	}
	for _, u := range []string{"u-mgr", "u-cash", "u-adm", "u-col"} {
		InvalidateLiveUserState(tenantID, u)
	}
	return tenantID, schema
}

// D4: a phone registered by a manager, then each person's own PIN on it.
func TestStoreAppSharedPhonePIN(t *testing.T) {
	tenantID, schema := storeApp102Tenant(t)

	if _, _, err := RegisterStoreAppDevice(tenantID, "u-cash", "cash102", "Cashier", "ST-A", "Counter phone"); err == nil {
		t.Fatal("a cashier registered a phone")
	}
	if _, _, err := RegisterStoreAppDevice(tenantID, "u-mgr", "mgr102", "Store Manager", "ST-C", "x"); err == nil {
		t.Fatal("a manager registered a phone to a store they cannot work at")
	}
	phone, token, err := RegisterStoreAppDevice(tenantID, "u-mgr", "mgr102", "Store Manager", "ST-A", "Counter phone")
	if err != nil || token == "" || phone.Store != "ST-A" || phone.StoreName != "Andheri" {
		t.Fatalf("register = %+v %q %v", phone, token, err)
	}
	var stored string
	_ = db.DB.QueryRow(`SELECT token_hash FROM `+schema+`.store_app_devices WHERE id = $1`, phone.ID).Scan(&stored)
	if stored == token || stored != storeAppTokenHash(token) {
		t.Fatal("the phone's token is stored in the clear, or not hashed")
	}

	if err := SetStoreAppPIN(tenantID, "u-adm", "Admin", "pw-adm", "4826"); err == nil {
		t.Fatal("an admin set a PIN")
	}
	for _, weak := range []string{"1111", "1234", "9876", "12a4", "123"} {
		if err := SetStoreAppPIN(tenantID, "u-cash", "Cashier", "pw-cash", weak); err == nil {
			t.Fatalf("PIN %q was accepted", weak)
		}
	}
	if err := SetStoreAppPIN(tenantID, "u-cash", "Cashier", "wrong", "4826"); err == nil {
		t.Fatal("a PIN was set without the right password")
	}
	if err := SetStoreAppPIN(tenantID, "u-cash", "Cashier", "pw-cash", "4826"); err != nil {
		t.Fatal(err)
	}

	if _, err := StoreAppPINSignIn(tenantID, "not-a-phone", "cash102", "4826"); !errors.Is(err, ErrStoreAppDeviceUnknown) {
		t.Fatalf("unknown phone = %v", err)
	}
	if _, err := StoreAppPINSignIn(tenantID, token, "cash102", "4827"); !errors.Is(err, ErrStoreAppPINSignIn) {
		t.Fatalf("wrong PIN = %v", err)
	}
	if _, err := StoreAppPINSignIn(tenantID, token, "nobody", "4826"); !errors.Is(err, ErrStoreAppPINSignIn) {
		t.Fatalf("unknown name = %v", err)
	}
	s, err := StoreAppPINSignIn(tenantID, token, "cash102", "4826")
	if err != nil || s.Store != "ST-A" || s.IdleMinutes != 5 {
		t.Fatalf("sign in = %+v, %v", s, err)
	}
	claims, err := ParseToken(s.Token)
	if err != nil || claims["dev"] != phone.ID || claims["loc"] != "ST-A" || claims["id"] != "u-cash" {
		t.Fatalf("token claims = %v, %v", claims, err)
	}
	if !StoreAppDeviceActive(tenantID, phone.ID) {
		t.Fatal("a registered phone reads as removed")
	}

	// An admin with a PIN row put in by hand still cannot use it.
	h, _ := bcrypt.GenerateFromPassword([]byte("5937"), bcrypt.MinCost)
	_, _ = db.DB.Exec(`INSERT INTO `+schema+`.store_app_pins (user_id, pin_hash) VALUES ('u-adm', $1)`, string(h))
	if _, err := StoreAppPINSignIn(tenantID, token, "adm102", "5937"); !errors.Is(err, ErrStoreAppPINSignIn) {
		t.Fatalf("admin PIN sign-in = %v", err)
	}

	// A person who cannot work at the phone's store.
	_, _ = db.DB.Exec(`INSERT INTO `+schema+`.store_app_pins (user_id, pin_hash) VALUES ('u-col', $1)`, string(h))
	if _, err := StoreAppPINSignIn(tenantID, token, "col102", "5937"); !errors.Is(err, ErrStoreAppPINSignIn) {
		t.Fatalf("other store's cashier = %v", err)
	}

	// Five wrong PINs stop PIN sign-in, even with the right PIN next.
	for i := 0; i < storeAppPinMaxFails; i++ {
		_, _ = StoreAppPINSignIn(tenantID, token, "cash102", "0000")
	}
	if _, err := StoreAppPINSignIn(tenantID, token, "cash102", "4826"); !errors.Is(err, ErrStoreAppPINSignIn) {
		t.Fatalf("locked PIN = %v", err)
	}
	var locked bool
	_ = db.DB.QueryRow(`SELECT locked_until > NOW() + interval '14 minutes' FROM ` + schema + `.store_app_pins WHERE user_id = 'u-cash'`).Scan(&locked)
	if !locked {
		t.Fatal("five wrong PINs did not lock for 15 minutes")
	}
	_, _ = db.DB.Exec(`UPDATE ` + schema + `.store_app_pins SET locked_until = NOW() - interval '1 minute' WHERE user_id = 'u-cash'`)
	if _, err := StoreAppPINSignIn(tenantID, token, "cash102", "4826"); err != nil {
		t.Fatalf("after the lock = %v", err)
	}

	// A password-locked account cannot use its PIN either.
	_, _ = db.DB.Exec(`UPDATE ` + schema + `.users SET locked_until = NOW() + interval '10 minutes' WHERE id = 'u-cash'`)
	if _, err := StoreAppPINSignIn(tenantID, token, "cash102", "4826"); !errors.Is(err, ErrStoreAppPINSignIn) {
		t.Fatalf("password-locked account = %v", err)
	}
	_, _ = db.DB.Exec(`UPDATE ` + schema + `.users SET locked_until = NULL WHERE id = 'u-cash'`)

	list, err := ListStoreAppDevices(tenantID, "u-mgr", "Store Manager")
	if err != nil || len(list) != 1 || list[0].LastUser != "cash102" {
		t.Fatalf("list = %+v, %v", list, err)
	}
	if _, err := ListStoreAppDevices(tenantID, "u-cash", "Cashier"); err == nil {
		t.Fatal("a cashier listed the store phones")
	}
	if err := RemoveStoreAppDevice(tenantID, "u-col", "col102", "Store Manager", phone.ID); err == nil {
		t.Fatal("a manager of another store removed this phone")
	}
	if err := RemoveStoreAppDevice(tenantID, "u-mgr", "mgr102", "Store Manager", phone.ID); err != nil {
		t.Fatal(err)
	}
	if StoreAppDeviceActive(tenantID, phone.ID) {
		t.Fatal("a removed phone is still active - its sessions would go on")
	}
	if _, err := StoreAppPINSignIn(tenantID, token, "cash102", "4826"); !errors.Is(err, ErrStoreAppDeviceUnknown) {
		t.Fatalf("removed phone = %v", err)
	}
}

// The day so far against the same day last week, own stores only.
func TestStoreAppToday(t *testing.T) {
	tenantID, schema := storeApp102Tenant(t)
	seed := func(id, loc, ago string, amount float64, salesman string, items string) {
		t.Helper()
		if _, err := db.DB.Exec(`INSERT INTO `+schema+`.documents (doctype, id, data, status, created_by, created_at)
			VALUES ('POSCart', $1, jsonb_build_object('location', $2::text, 'amount_paid', $3::numeric, 'salesman_id', $4::text, 'items', $5::jsonb), 'Paid', 'u-cash', LOCALTIMESTAMP - $6::interval)`,
			id, loc, amount, salesman, items, ago); err != nil {
			t.Fatalf("seed %s: %v", id, err)
		}
	}
	seed("C102-1", "ST-A", "1 second", 1000, "SM-1", `[{"sku":"SA2-TEE","qty":2,"sale_price":500}]`)
	seed("C102-2", "ST-A", "2 seconds", 200, "", `[{"sku":"SA2-CAP","qty":1,"sale_price":200}]`)
	seed("C102-3", "ST-B", "1 second", 9999, "SM-1", `[{"sku":"SA2-TEE","qty":9,"sale_price":1111}]`)
	seed("C102-4", "ST-A", "7 days 3 seconds", 600, "SM-1", `[{"sku":"SA2-TEE","qty":1,"sale_price":600}]`)
	seed("C102-5", "ST-A", "6 days 23 hours", 5000, "SM-1", `[{"sku":"SA2-TEE","qty":5,"sale_price":1000}]`) // later in that day than now: not counted

	got, err := StoreAppTodayForUser(tenantID, "u-mgr", "ST-A", "")
	if err != nil {
		t.Fatal(err)
	}
	if got.Today.Sales != 1200 || got.Today.Bills != 2 || got.Today.Units != 3 || got.Today.AvgBill != 600 || got.Today.ItemsPerBill != 1.5 {
		t.Fatalf("today = %+v", got.Today)
	}
	if got.LastWeek.Sales != 600 || got.LastWeek.Bills != 1 || got.ChangePct == nil || *got.ChangePct != 100 {
		t.Fatalf("last week = %+v change %v", got.LastWeek, got.ChangePct)
	}
	if len(got.TopItems) != 2 || got.TopItems[0].SKU != "SA2-TEE" || got.TopItems[0].Name != "Linen tee" || got.TopItems[0].Amount != 1000 {
		t.Fatalf("top items = %+v", got.TopItems)
	}
	if len(got.Salesmen) != 2 || got.Salesmen[0].Name != "Ravi" || got.Salesmen[0].Amount != 1000 || got.Salesmen[1].Name != "No salesman" {
		t.Fatalf("salesmen = %+v", got.Salesmen)
	}
	if got.Target != nil || got.TargetNote == "" || got.Label != "Andheri" {
		t.Fatalf("target / label = %v %q %q", got.Target, got.TargetNote, got.Label)
	}
	if _, err := StoreAppTodayForUser(tenantID, "u-mgr", "ST-A", "ST-B"); err == nil {
		t.Fatal("a manager of Andheri saw Bandra")
	}
	all, err := StoreAppTodayForUser(tenantID, "u-cash", "ST-A", "all")
	if err != nil || all.Today.Sales != 11199 || all.Today.Bills != 3 {
		t.Fatalf("all stores = %+v, %v", all, err)
	}
}

// A spot count becomes cycle-count lines reconciled the usual way; a resend
// changes nothing. A full count adds each phone's figure to the Physical
// Inventory's own line.
func TestStoreAppCounts(t *testing.T) {
	tenantID, schema := storeApp102Tenant(t)
	if _, err := db.DB.Exec(`INSERT INTO ` + schema + `.inventory_availability (sku, location_code, on_hand, available) VALUES ('SA2-TEE', 'ST-A', 4, 4), ('SA2-CAP', 'ST-A', 2, 2)`); err != nil {
		t.Fatal(err)
	}
	in := StoreAppCountInput{SessionKey: "phone-count-1", Mode: "spot", Lines: []StoreAppCountLine{{Code: "8900000102001", Qty: 3}, {Code: "SA2-TEE", Qty: 1}, {Code: "NO-SUCH", Qty: 2}}}
	res, err := StoreAppSubmitCount(tenantID, "u-mgr", "mgr102", "Store Manager", "ST-A", in)
	if err != nil {
		t.Fatal(err)
	}
	if res.Recorded != 1 || res.Posted != 1 || len(res.Unknown) != 1 || res.Unknown[0] != "NO-SUCH" {
		t.Fatalf("spot count = %+v", res)
	}
	var counted float64
	var status string
	if err := db.DB.QueryRow(`SELECT (data->>'counted_qty')::numeric, status FROM `+schema+`.documents WHERE doctype = 'CycleCountLine' AND data->>'count_session' = $1`, res.Session).Scan(&counted, &status); err != nil || counted != 4 || status != "Posted" {
		t.Fatalf("line = %v %s %v", counted, status, err)
	}
	again, err := StoreAppSubmitCount(tenantID, "u-mgr", "mgr102", "Store Manager", "ST-A", in)
	if err != nil || again.Recorded != 0 || again.Session != res.Session {
		t.Fatalf("resend = %+v, %v", again, err)
	}
	var n int
	_ = db.DB.QueryRow(`SELECT COUNT(*) FROM `+schema+`.documents WHERE doctype = 'CycleCountLine' AND data->>'count_session' = $1`, res.Session).Scan(&n)
	if n != 1 {
		t.Fatalf("resend made %d lines", n)
	}
	if _, err := StoreAppSubmitCount(tenantID, "u-col", "col102", "Cashier", "ST-A", in); err == nil {
		t.Fatal("a Colaba cashier counted Andheri")
	}

	// Full count: one PI with a line for the tee, counted 1 on the computer.
	if _, err := db.DB.Exec(`INSERT INTO ` + schema + `.documents (doctype, id, data, status, created_by) VALUES
		('PhysicalInventory', 'PI-102', '{"location":"ST-A","status":"Counting"}', 'Counting', 'u-mgr'),
		('CycleCountLine', 'PI-102-L1', '{"count_session":"PI-102","physical_inventory":"PI-102","location":"ST-A","sku":"SA2-TEE","status":"Draft","counted_qty":1}', 'Draft', 'u-mgr')`); err != nil {
		t.Fatal(err)
	}
	sessions, err := StoreAppCountSessions(tenantID, "u-mgr", "ST-A")
	if err != nil || len(sessions) != 1 || sessions[0].ID != "PI-102" || sessions[0].Lines != 1 {
		t.Fatalf("sessions = %+v, %v", sessions, err)
	}
	full := func(key string, lines ...StoreAppCountLine) *StoreAppCountResult {
		t.Helper()
		r, err := StoreAppSubmitCount(tenantID, "u-mgr", "mgr102", "Store Manager", "ST-A", StoreAppCountInput{SessionKey: key, Mode: "full", PhysicalInventory: "PI-102", Lines: lines})
		if err != nil {
			t.Fatal(err)
		}
		return r
	}
	qtyOf := func(sku string) float64 {
		t.Helper()
		var q float64
		_ = db.DB.QueryRow(`SELECT (data->>'counted_qty')::numeric FROM `+schema+`.documents WHERE doctype = 'CycleCountLine' AND data->>'physical_inventory' = 'PI-102' AND data->>'sku' = $1`, sku).Scan(&q)
		return q
	}
	full("phone-a", StoreAppCountLine{Code: "SA2-TEE", Qty: 2})
	full("phone-b", StoreAppCountLine{Code: "SA2-TEE", Qty: 3}, StoreAppCountLine{Code: "SA2-CAP", Qty: 1})
	if q := qtyOf("SA2-TEE"); q != 6 { // 1 typed on the computer + 2 + 3
		t.Fatalf("tee after two phones = %v", q)
	}
	if r := full("phone-a", StoreAppCountLine{Code: "SA2-TEE", Qty: 2}); r.Recorded != 0 {
		t.Fatalf("a resend changed the count: %+v", r)
	}
	full("phone-a", StoreAppCountLine{Code: "SA2-TEE", Qty: 4}) // phone A recounted its shelf
	if q := qtyOf("SA2-TEE"); q != 8 {
		t.Fatalf("tee after phone A's recount = %v", q)
	}
	if q := qtyOf("SA2-CAP"); q != 1 {
		t.Fatalf("cap, not on the list, = %v", q)
	}
}

// Labels from the phone become a transaction the desktop's Print from
// Transaction loads, and read Printed once the computer prints them.
func TestStoreAppLabelRequest(t *testing.T) {
	tenantID, schema := storeApp102Tenant(t)
	if _, err := StoreAppRequestLabels(tenantID, "u-cash", "cash102", "ST-A", []StoreAppLabelLine{{Code: "NOPE", Copies: 1}}, ""); err == nil {
		t.Fatal("an unknown code was accepted")
	}
	if _, err := StoreAppRequestLabels(tenantID, "u-cash", "cash102", "ST-A", []StoreAppLabelLine{{Code: "SA2-TEE", Copies: storeAppMaxLabels + 1}}, ""); err == nil {
		t.Fatal("too many labels were accepted")
	}
	req, err := StoreAppRequestLabels(tenantID, "u-cash", "cash102", "ST-A", []StoreAppLabelLine{{Code: "8900000102001", Copies: 3}, {Code: "SA2-CAP", Copies: 0}}, "rack 4")
	if err != nil || req.Labels != 3 || len(req.Items) != 1 || req.Items[0]["sku"] != "SA2-TEE" {
		t.Fatalf("request = %+v, %v", req, err)
	}
	if !DoctypeIsTransaction(tenantID, "StoreLabelRequest") {
		t.Fatal("StoreLabelRequest is not a transaction - Print from Transaction will not list it")
	}
	lines, err := ResolveDocumentStickerLines(tenantID, "StoreLabelRequest", req.ID)
	if err != nil || len(lines) != 1 || lines[0].SKU != "SA2-TEE" || lines[0].Qty != 3 {
		t.Fatalf("desktop sticker lines = %+v, %v", lines, err)
	}
	list, err := StoreAppLabelRequests(tenantID, "u-cash", "ST-A")
	if err != nil || len(list) != 1 || list[0].Status != "Requested" || list[0].Labels != 3 {
		t.Fatalf("list = %+v, %v", list, err)
	}
	if _, err := db.DB.Exec(`INSERT INTO `+schema+`.sticker_print_log (sku, barcode, printer_code, printed_by, copies, source_doctype, source_doc_id) VALUES ('SA2-TEE', '8900000102001', 'P1', 'counter-pc', 3, 'StoreLabelRequest', $1)`, req.ID); err != nil {
		t.Fatal(err)
	}
	list, _ = StoreAppLabelRequests(tenantID, "u-cash", "ST-A")
	var status string
	_ = db.DB.QueryRow(`SELECT status FROM `+schema+`.documents WHERE id = $1`, req.ID).Scan(&status)
	if list[0].Status != "Printed" || list[0].PrintedBy != "counter-pc" || status != "Printed" {
		t.Fatalf("after print = %+v (doc %s)", list[0], status)
	}
	if other, _ := StoreAppLabelRequests(tenantID, "u-cash", "ST-B"); len(other) != 0 {
		t.Fatal("Bandra sees Andheri's labels")
	}
}

// A transfer on its way to the store reads as a scan list.
func TestStoreAppReceiveTransfer(t *testing.T) {
	tenantID, schema := storeApp102Tenant(t)
	if _, err := db.DB.Exec(`INSERT INTO ` + schema + `.documents (doctype, id, data, status, created_by) VALUES
		('TransferOrder', 'TO-102', jsonb_build_object('transfer_number', 'TR/102', 'from_warehouse', 'WH-1', 'to_warehouse', 'ST-A',
		   'transfer_balance', '[{"sku":"SA2-TEE","dispatched":5,"received":2},{"sku":"SA2-CAP","dispatched":1,"received":1}]'), 'Partially Received', 'u-mgr'),
		('TransferOrder', 'TO-102B', jsonb_build_object('from_warehouse', 'WH-1', 'to_warehouse', 'ST-B', 'transfer_balance', '[{"sku":"SA2-TEE","dispatched":1}]'), 'Dispatched', 'u-mgr')`); err != nil {
		t.Fatal(err)
	}
	docs, err := StoreAppReceiveList(tenantID, "u-mgr", "ST-A")
	if err != nil {
		t.Fatal(err)
	}
	if len(docs) != 1 || docs[0].ID != "TO-102" || docs[0].Units != 3 || docs[0].Lines != 1 || docs[0].From != "Central warehouse" {
		t.Fatalf("list = %+v", docs)
	}
	d, err := StoreAppReceiveDocument(tenantID, "u-mgr", "Store Manager", "ST-A", "transfer", "TO-102")
	if err != nil || !d.CanSave || len(d.Lines) != 1 || d.Lines[0].Expected != 3 {
		t.Fatalf("detail = %+v, %v", d, err)
	}
	codes := strings.Join(d.Lines[0].Codes, ",")
	if !strings.Contains(codes, "8900000102001") || !strings.Contains(codes, "SA2-TEE") {
		t.Fatalf("codes = %s", codes)
	}
	if _, err := StoreAppReceiveDocument(tenantID, "u-mgr", "Store Manager", "ST-A", "transfer", "TO-102B"); err == nil {
		t.Fatal("Andheri read Bandra's transfer")
	}
}

// A size from another store, booked for home delivery as an OMS order.
func TestStoreAppOrderFromAnotherStore(t *testing.T) {
	tenantID, schema := storeApp102Tenant(t)
	if _, err := db.DB.Exec(`INSERT INTO ` + schema + `.inventory_availability (sku, location_code, on_hand, available) VALUES ('SA2-TEE', 'ST-B', 5, 5)`); err != nil {
		t.Fatal(err)
	}
	in := StoreAppOrderInput{RequestKey: "ord-1", CustomerName: "Asha", CustomerPhone: "9876543210", Address: "12 Hill Road", City: "Mumbai",
		State: "Maharashtra", Pincode: "400050", Payment: "cod", FromStore: "ST-B", Lines: []StoreAppOrderLine{{Code: "8900000102001", Qty: 1}}, Note: "size M"}
	bad := in
	bad.Pincode = ""
	if _, err := StoreAppCreateOrder(tenantID, "u-cash", "cash102", "ST-A", bad); err == nil {
		t.Fatal("an order without a PIN code was accepted")
	}
	res, err := StoreAppCreateOrder(tenantID, "u-cash", "cash102", "ST-A", in)
	if err != nil {
		t.Fatal(err)
	}
	if res.Existing || res.OrderID == "" || res.Total != 500 {
		t.Fatalf("order = %+v", res)
	}
	data, _, err := fetchDocData(tenantID, "SalesOrder", res.OrderID)
	if err != nil || data["channel"] != StoreAppOrderChannel || data["booked_at_store"] != "ST-A" || data["booked_by"] != "cash102" || data["payment_status"] != "COD" {
		t.Fatalf("order doc = %v, %v", data, err)
	}
	if res.HoldReason == "" && res.FromStore != "ST-B" {
		t.Fatalf("sourced from %q, want ST-B (%+v)", res.FromStore, res)
	}
	again, err := StoreAppCreateOrder(tenantID, "u-cash", "cash102", "ST-A", in)
	if err != nil || !again.Existing || again.OrderID != res.OrderID {
		t.Fatalf("retry = %+v, %v", again, err)
	}
}

// The alert worker: the first look only takes note; a new approval for the
// person, and an item newly low at their store, each queue one push - once.
func TestWebPushAlertWorker(t *testing.T) {
	tenantID, schema := storeApp102Tenant(t)
	_, _ = db.DB.Exec(`DELETE FROM ` + schema + `.approval_rules WHERE doctype = 'PurchaseRequisition'`)
	if _, err := UpsertApprovalRule(tenantID, "PurchaseRequisition", 0, nil, RoleStoreManager, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := db.DB.Exec(`INSERT INTO ` + schema + `.inventory_availability (sku, location_code, on_hand, available) VALUES ('SA2-TEE', 'ST-A', 90, 90), ('SA2-CAP', 'ST-A', 0, 0)`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.DB.Exec(`INSERT INTO ` + schema + `.web_push_subscriptions (user_id, endpoint, p256dh, auth, store) VALUES ('u-mgr', 'https://fcm.googleapis.com/fcm/send/test-102', 'k', 'a', 'ST-A')`); err != nil {
		t.Fatal(err)
	}
	jobs := func() []string {
		t.Helper()
		rows, err := db.DB.Query(`SELECT payload->>'body' FROM ` + schema + `.async_jobs WHERE job_type = 'web_push_delivery' ORDER BY created_at`)
		if err != nil {
			t.Fatal(err)
		}
		defer rows.Close()
		var out []string
		for rows.Next() {
			var b string
			_ = rows.Scan(&b)
			out = append(out, b)
		}
		return out
	}
	if low, _ := inventoryLowStock(schema, "ST-A", 0); len(low) != 1 || low[0]["sku"] != "SA2-CAP" {
		t.Fatalf("low stock = %v - an item with 90 free is not low", low)
	}
	if err := runWebPushAlerts(schema); err != nil {
		t.Fatal(err)
	}
	if j := jobs(); len(j) != 0 {
		t.Fatalf("first look pushed %v", j)
	}
	if _, err := db.DB.Exec(`INSERT INTO ` + schema + `.documents (doctype, id, data, status, created_by) VALUES
		('PurchaseRequisition', 'PR-102-1', '{"code":"PR/102/1","location":"ST-A","total_amount":100}', 'Pending Approval', 'u-cash')`); err != nil {
		t.Fatal(err)
	}
	_, _ = db.DB.Exec(`UPDATE ` + schema + `.inventory_availability SET on_hand = 0, available = 0 WHERE sku = 'SA2-TEE' AND location_code = 'ST-A'`)
	if err := runWebPushAlerts(schema); err != nil {
		t.Fatal(err)
	}
	j := jobs()
	if len(j) != 2 {
		t.Fatalf("pushes = %v", j)
	}
	joined := strings.Join(j, " | ")
	if !strings.Contains(joined, "PR/102/1 is waiting for your approval") || !strings.Contains(joined, "Linen tee has run out at Andheri") {
		t.Fatalf("pushes = %v", j)
	}
	if err := runWebPushAlerts(schema); err != nil {
		t.Fatal(err)
	}
	if again := jobs(); len(again) != 2 {
		t.Fatalf("a second look pushed again: %v", again)
	}
}
