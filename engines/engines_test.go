package engines

import (
	"custom_erp/db"
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func TestEngines(t *testing.T) {
	// Initialize connection for testing
	connStr := "postgres://postgres@localhost:5435/custom_erp?sslmode=disable"
	db.InitDB(connStr)

	tenantID := "default"
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		t.Fatalf("Failed to get tenant schema: %v", err)
	}

	// 1. Test Prefix Configuration and Sequence Generation
	t.Run("GenerateSequence", func(t *testing.T) {
		docType := "TEST_DOC"
		store := "TEST_STORE"
		fy := "26-27"

		// Clear pre-existing
		db.DB.Exec("DELETE FROM "+schema+".prefix_configs WHERE doc_type = $1", docType)
		db.DB.Exec("DELETE FROM "+schema+".sequence_counters WHERE doc_type = $1", docType)

		// Insert test config
		_, err := db.DB.Exec(`
			INSERT INTO `+schema+`.prefix_configs (doc_type, prefix, separator, padding_width, reset_frequency) 
			VALUES ($1, $2, $3, $4, $5)`, docType, "TST", "-", 4, "ANNUAL")
		if err != nil {
			t.Fatalf("Failed to insert test prefix config: %v", err)
		}

		// Generate 1st code
		code1, err := GenerateSequence(tenantID, docType, store, fy)
		if err != nil {
			t.Fatalf("Failed to generate 1st sequence: %v", err)
		}
		expected1 := "TST-TEST_STORE-26-27-0001"
		if code1 != expected1 {
			t.Errorf("Expected 1st sequence %q, got %q", expected1, code1)
		}

		// Generate 2nd code
		code2, err := GenerateSequence(tenantID, docType, store, fy)
		if err != nil {
			t.Fatalf("Failed to generate 2nd sequence: %v", err)
		}
		expected2 := "TST-TEST_STORE-26-27-0002"
		if code2 != expected2 {
			t.Errorf("Expected 2nd sequence %q, got %q", expected2, code2)
		}
	})

	// 2. Test Dynamic Translation Labels
	t.Run("DynamicLabels", func(t *testing.T) {
		orig := "TestOriginalTranslationKey"
		cust := "TestCustomTranslationVal"

		// Cleanup
		_ = DeleteLabel(tenantID, orig)

		// Save Label
		err := SaveLabel(tenantID, orig, cust)
		if err != nil {
			t.Fatalf("Failed to save label: %v", err)
		}

		// Get Labels
		labels, err := GetLabels(tenantID)
		if err != nil {
			t.Fatalf("Failed to retrieve labels: %v", err)
		}

		val, exists := labels[orig]
		if !exists {
			t.Errorf("Expected label key %q to exist", orig)
		}
		if val != cust {
			t.Errorf("Expected label val %q, got %q", cust, val)
		}

		// Delete Label
		err = DeleteLabel(tenantID, orig)
		if err != nil {
			t.Fatalf("Failed to delete label: %v", err)
		}

		labels2, err := GetLabels(tenantID)
		if err != nil {
			t.Fatalf("Failed to retrieve labels: %v", err)
		}
		_, exists2 := labels2[orig]
		if exists2 {
			t.Errorf("Expected label key %q to be deleted", orig)
		}
	})

	// 3. Test DocType metadata validations and JWT token signatures
	t.Run("DocTypeValidationAndAuth", func(t *testing.T) {
		// 26.0.2: earlier Agriculture industry-profile testing against this
		// same shared dev DB left a stray Brand.fefo_enabled doctype_fields
		// row behind (mandatory:true, per public/profiles/agriculture.json)
		// that was never part of Brand's own baseline schema (absent from
		// db/migration.sql). Reset it before asserting, same
		// clear-fixture-before-asserting convention as GenerateSequence
		// above, instead of leaving this subtest hostage to whatever
		// industry-profile testing last touched the "default" tenant.
		_, _ = db.DB.Exec("DELETE FROM " + schema + ".doctype_fields WHERE doctype_name = 'Brand' AND fieldname = 'fefo_enabled'")

		// Valid brand payload
		validDoc := map[string]interface{}{
			"code":   "BRD99",
			"name":   "Test Brand Name",
			"status": "Active",
		}
		err := ValidateDocument(tenantID, "Brand", validDoc)
		if err != nil {
			t.Errorf("Expected valid Brand payload to pass validation, got error: %v", err)
		}

		// Invalid brand payload (missing mandatory name)
		invalidDoc := map[string]interface{}{
			"code":   "BRD99",
			"status": "Active",
		}
		err = ValidateDocument(tenantID, "Brand", invalidDoc)
		if err == nil {
			t.Errorf("Expected Brand payload missing name to fail validation, but it passed")
		}

		// Invalid brand payload (incorrect select option status)
		badOptionDoc := map[string]interface{}{
			"code":   "BRD99",
			"name":   "Test Brand Name",
			"status": "InvalidOptionStatus",
		}
		err = ValidateDocument(tenantID, "Brand", badOptionDoc)
		if err == nil {
			t.Errorf("Expected Brand payload with bad status option to fail validation, but it passed")
		}

		// Test JWT signed token signature verification
		token := SignToken("admin", "admin", "HR/Admin", "default", "HO")
		claims, err := ParseToken(token)
		if err != nil {
			t.Fatalf("Failed to parse signed token: %v", err)
		}

		if claims["id"] != "admin" || claims["role"] != "HR/Admin" || claims["tenant"] != "default" || claims["loc"] != "HO" {
			t.Errorf("Extracted token claims do not match signed values: %v", claims)
		}
	})

	// 4. Test Omnichannel, WMS & OMS Scale Foundation
	t.Run("OmnichannelAndWMS", func(t *testing.T) {
		sku := "SKU-TEST-99"
		location := "WH01"

		// Clear previous availability
		_, _ = db.DB.Exec("DELETE FROM "+schema+".inventory_availability WHERE sku = $1", sku)
		_, _ = db.DB.Exec("DELETE FROM "+schema+".inventory_reservation WHERE sku = $1", sku)

		// Post a GRN transaction received items mock
		items := []interface{}{
			map[string]interface{}{"sku": sku, "qty": 15.0},
		}
		_, err := PostInventoryLedger(tenantID, location, items, false)
		if err != nil {
			t.Fatalf("Failed to post inventory ledger from GRN callback: %v", err)
		}

		// Verify availability levels unreserved
		atsRes, err := GetAvailableToSell(tenantID, sku, location)
		if err != nil {
			t.Fatalf("Failed to fetch available to sell stock: %v", err)
		}

		if atsRes["on_hand"].(int) != 15 || atsRes["available"].(int) != 15 || atsRes["ats"].(int) != 15 {
			t.Errorf("Expected stock quantities (15), got: %v", atsRes)
		}

		// Create a temporary reservation
		resID, err := CreateReservation(tenantID, sku, location, 5, "Online", 300)
		if err != nil {
			t.Fatalf("Failed to create temporary reservation: %v", err)
		}

		if resID == "" {
			t.Errorf("Expected reservation ID, got empty string")
		}

		// Verify ATS reduction after reservation locks
		atsResPost, err := GetAvailableToSell(tenantID, sku, location)
		if err != nil {
			t.Fatalf("Failed to fetch available to sell stock post reservation: %v", err)
		}

		if atsResPost["reserved"].(int) != 5 || atsResPost["ats"].(int) != 10 {
			t.Errorf("Expected ATS to reduce to 10 (reserved 5), got: %v", atsResPost)
		}

		// 26.12.6: the 7-term ATP formula's held-back buckets (Blocked/QC
		// Hold/Damaged/Channel Buffer) must actually reduce ATS, not just
		// exist as inert columns - set all four directly (no writer engine
		// function exists yet, that's a later 26.12 item's job) and confirm
		// both GetAvailableToSell and FindBestFulfillmentNode's sourcing
		// comparison read them consistently via the shared computeATS helper.
		_, err = db.DB.Exec("UPDATE "+schema+".inventory_availability SET blocked = 1, qc_hold = 1, damaged = 1, channel_buffer = 1 WHERE sku = $1 AND location_code = $2", sku, location)
		if err != nil {
			t.Fatalf("Failed to set inventory buckets: %v", err)
		}

		atsResBuckets, err := GetAvailableToSell(tenantID, sku, location)
		if err != nil {
			t.Fatalf("Failed to fetch available to sell stock post bucket update: %v", err)
		}
		if atsResBuckets["ats"].(int) != 6 {
			t.Errorf("Expected ATS to reduce to 6 (10 - 1 - 1 - 1 - 1 across the 4 new buckets), got: %v", atsResBuckets)
		}

		bestNode, err := FindBestFulfillmentNode(tenantID, []map[string]interface{}{{"sku": sku, "qty": 7}})
		if err != nil {
			t.Fatalf("Failed to resolve fulfillment node: %v", err)
		}
		if bestNode != "HO" {
			t.Errorf("Expected sourcing to fall back to HO once ATS (6) can't cover the requested qty (7) at %s, got node: %s", location, bestNode)
		}
	})

	// 26.12.1: Order Engine - validate/reserve chain, Hold engine, and the
	// stage-gated cancellation matrix.
	t.Run("OrderEngine", func(t *testing.T) {
		sku := "SKU-ORD-TEST-1"
		location := "WH01"

		// Clean fixtures from any prior run (order documents themselves are
		// cleaned via each creation's own deferred cleanupOrder below).
		_, _ = db.DB.Exec("DELETE FROM "+schema+".documents WHERE doctype = 'Item' AND data->>'code' = $1", sku)
		_, _ = db.DB.Exec("DELETE FROM " + schema + ".documents WHERE doctype = 'ReasonCode' AND id IN ('RC-TEST-CANCEL','RC-TEST-HOLD')")
		_, _ = db.DB.Exec("DELETE FROM " + schema + ".documents WHERE doctype = 'StatusTransitionRule' AND id = 'STR-TEST-1'")
		_, _ = db.DB.Exec("DELETE FROM "+schema+".inventory_availability WHERE sku = $1", sku)

		// Fixtures: an Active Item (SKU-mapping target), a Cancellation
		// reason code, and a Hold reason code (Stage 26.12.9's ReasonCode
		// master - mandatory-reason-code enforcement needs a real row).
		itemData, _ := json.Marshal(map[string]interface{}{"code": sku, "name": "Order Engine Test Item", "barcode": sku})
		if _, err := db.DB.Exec("INSERT INTO "+schema+".documents (id, doctype, data, status, created_by) VALUES ($1, 'Item', $2, 'Active', 'system')", "ITEM-"+sku, itemData); err != nil {
			t.Fatalf("Failed to seed test Item: %v", err)
		}
		cancelReasonData, _ := json.Marshal(map[string]interface{}{"code": "RC-CANCEL", "description": "Customer requested cancellation", "category": "Cancellation", "status": "Active"})
		if _, err := db.DB.Exec("INSERT INTO "+schema+".documents (id, doctype, data, status, created_by) VALUES ($1, 'ReasonCode', $2, 'Active', 'system')", "RC-TEST-CANCEL", cancelReasonData); err != nil {
			t.Fatalf("Failed to seed test Cancellation ReasonCode: %v", err)
		}
		holdReasonData, _ := json.Marshal(map[string]interface{}{"code": "RC-HOLD", "description": "Risk review", "category": "Hold", "status": "Active"})
		if _, err := db.DB.Exec("INSERT INTO "+schema+".documents (id, doctype, data, status, created_by) VALUES ($1, 'ReasonCode', $2, 'Active', 'system')", "RC-TEST-HOLD", holdReasonData); err != nil {
			t.Fatalf("Failed to seed test Hold ReasonCode: %v", err)
		}
		if _, err := db.DB.Exec("INSERT INTO "+schema+".inventory_availability (sku, location_code, on_hand, available) VALUES ($1, $2, 100, 100)", sku, location); err != nil {
			t.Fatalf("Failed to seed test inventory: %v", err)
		}

		cleanupOrder := func(orderID string) {
			_, _ = db.DB.Exec("DELETE FROM "+schema+".documents WHERE doctype = 'SalesOrderLine' AND data->>'order_id' = $1", orderID)
			_, _ = db.DB.Exec("DELETE FROM "+schema+".documents WHERE doctype = 'SalesOrder' AND id = $1", orderID)
		}

		// 1. Happy path: valid SKU + address-with-pincode + confirmed payment
		// reserves immediately.
		orderID, err := CreateSalesOrder(tenantID, "TestChannel", "CHORD-1", "Test Customer", "ORDTEST 12 MG Road, Bengaluru 560001", "Confirmed", []SalesOrderLineInput{{SKU: sku, Qty: 5, UnitPrice: 100}})
		if err != nil {
			t.Fatalf("CreateSalesOrder (happy path) failed: %v", err)
		}
		defer cleanupOrder(orderID)

		var orderStatus string
		if err := db.DB.QueryRow("SELECT status FROM "+schema+".documents WHERE id = $1", orderID).Scan(&orderStatus); err != nil {
			t.Fatalf("Failed to read created order status: %v", err)
		}
		if orderStatus != "Reserved" {
			t.Errorf("Expected happy-path order to be Reserved, got %q", orderStatus)
		}
		ats, err := GetAvailableToSell(tenantID, sku, location)
		if err != nil {
			t.Fatalf("Failed to read ATS after order creation: %v", err)
		}
		if ats["reserved"].(int) != 5 {
			t.Errorf("Expected 5 reserved after order creation, got: %v", ats["reserved"])
		}

		// 2. Idempotent replay: same channel+channel_order_id returns the
		// same order instead of creating a duplicate.
		replayID, err := CreateSalesOrder(tenantID, "TestChannel", "CHORD-1", "Test Customer", "ORDTEST 12 MG Road, Bengaluru 560001", "Confirmed", []SalesOrderLineInput{{SKU: sku, Qty: 5, UnitPrice: 100}})
		if err != nil {
			t.Fatalf("CreateSalesOrder (replay) failed: %v", err)
		}
		if replayID != orderID {
			t.Errorf("Expected idempotent replay to return the same order id %q, got %q", orderID, replayID)
		}

		// 3. Address validation failure -> On Hold with ADDR_INVALID,
		// reservation NOT created (stock still shows 0 additional reserved
		// for this second order).
		holdOrderID, err := CreateSalesOrder(tenantID, "TestChannel", "CHORD-2", "Test Customer", "no pincode here", "Confirmed", []SalesOrderLineInput{{SKU: sku, Qty: 3, UnitPrice: 100}})
		if err != nil {
			t.Fatalf("CreateSalesOrder (bad address) failed: %v", err)
		}
		defer cleanupOrder(holdOrderID)

		var holdStatus, holdReason string
		if err := db.DB.QueryRow("SELECT status, data->>'hold_reason' FROM "+schema+".documents WHERE id = $1", holdOrderID).Scan(&holdStatus, &holdReason); err != nil {
			t.Fatalf("Failed to read held order: %v", err)
		}
		if holdStatus != "On Hold" || holdReason != HoldAddressInvalid {
			t.Errorf("Expected On Hold / %s for a missing pincode, got status=%q reason=%q", HoldAddressInvalid, holdStatus, holdReason)
		}

		// 4. Release the hold after fixing the address directly (no amend-
		// order function exists yet - out of this item's scope) - re-running
		// the same validate chain should now pass and reserve stock.
		fixedAddr, _ := json.Marshal("ORDTEST fixed address 560002")
		if _, err := db.DB.Exec("UPDATE "+schema+".documents SET data = jsonb_set(data, '{shipping_address}', $1) WHERE id = $2", fixedAddr, holdOrderID); err != nil {
			t.Fatalf("Failed to patch order address: %v", err)
		}
		if err := ReleaseOrderHold(tenantID, holdOrderID); err != nil {
			t.Fatalf("ReleaseOrderHold failed after fixing the address: %v", err)
		}
		var releasedStatus string
		if err := db.DB.QueryRow("SELECT status FROM "+schema+".documents WHERE id = $1", holdOrderID).Scan(&releasedStatus); err != nil {
			t.Fatalf("Failed to read released order: %v", err)
		}
		if releasedStatus != "Reserved" {
			t.Errorf("Expected order to be Reserved after a successful hold release, got %q", releasedStatus)
		}

		// 5. Unconfirmed payment -> On Hold with PAYMENT_PENDING.
		paymentHoldID, err := CreateSalesOrder(tenantID, "TestChannel", "CHORD-3", "Test Customer", "ORDTEST 5 Park St 560003", "Pending", []SalesOrderLineInput{{SKU: sku, Qty: 2, UnitPrice: 100}})
		if err != nil {
			t.Fatalf("CreateSalesOrder (unconfirmed payment) failed: %v", err)
		}
		defer cleanupOrder(paymentHoldID)
		var paymentHoldReason string
		if err := db.DB.QueryRow("SELECT data->>'hold_reason' FROM "+schema+".documents WHERE id = $1", paymentHoldID).Scan(&paymentHoldReason); err != nil {
			t.Fatalf("Failed to read payment-hold order: %v", err)
		}
		if paymentHoldReason != HoldPaymentPending {
			t.Errorf("Expected hold_reason %s for unconfirmed payment, got %q", HoldPaymentPending, paymentHoldReason)
		}

		// 6. Cancellation requires a mandatory, category-matched reason code.
		if err := CancelOrder(tenantID, orderID, ""); err == nil {
			t.Errorf("Expected CancelOrder to reject a missing reason code")
		}
		if err := CancelOrder(tenantID, orderID, "RC-TEST-HOLD"); err == nil {
			t.Errorf("Expected CancelOrder to reject a Hold-category reason code")
		}

		// 7. Cancelling a Reserved order releases its reservation.
		if err := CancelOrder(tenantID, orderID, "RC-TEST-CANCEL"); err != nil {
			t.Fatalf("CancelOrder (valid) failed: %v", err)
		}
		var cancelledStatus string
		if err := db.DB.QueryRow("SELECT status FROM "+schema+".documents WHERE id = $1", orderID).Scan(&cancelledStatus); err != nil {
			t.Fatalf("Failed to read cancelled order: %v", err)
		}
		if cancelledStatus != "Cancelled" {
			t.Errorf("Expected order to be Cancelled, got %q", cancelledStatus)
		}
		atsAfterCancel, err := GetAvailableToSell(tenantID, sku, location)
		if err != nil {
			t.Fatalf("Failed to read ATS after cancellation: %v", err)
		}
		// 5 (original order) released; the fixed-address order (holdOrderID)
		// still holds its own 3 reserved.
		if atsAfterCancel["reserved"].(int) != 3 {
			t.Errorf("Expected reserved to drop back to 3 after cancelling the 5-qty order, got: %v", atsAfterCancel["reserved"])
		}

		// 8. Stage-gated cancellation matrix: blocked by the hardcoded
		// default once Shipped, then explicitly allowed once a
		// StatusTransitionRule (Stage 26.12.9) override exists.
		if _, err := db.DB.Exec("UPDATE "+schema+".documents SET status = 'Shipped', data = jsonb_set(data, '{order_status}', '\"Shipped\"') WHERE id = $1", holdOrderID); err != nil {
			t.Fatalf("Failed to force order to Shipped: %v", err)
		}
		if err := CancelOrder(tenantID, holdOrderID, "RC-TEST-CANCEL"); err == nil {
			t.Errorf("Expected CancelOrder to be blocked for a Shipped order by the default matrix")
		}

		ruleData, _ := json.Marshal(map[string]interface{}{"code": "STR-TEST", "entity": "Order", "from_status": "Shipped", "to_status": "Cancelled", "allowed": "Yes", "requires_reason_code": "Yes", "status": "Active"})
		if _, err := db.DB.Exec("INSERT INTO "+schema+".documents (id, doctype, data, status, created_by) VALUES ($1, 'StatusTransitionRule', $2, 'Active', 'system')", "STR-TEST-1", ruleData); err != nil {
			t.Fatalf("Failed to seed StatusTransitionRule override: %v", err)
		}
		if err := CancelOrder(tenantID, holdOrderID, "RC-TEST-CANCEL"); err != nil {
			t.Errorf("Expected CancelOrder to succeed once a StatusTransitionRule explicitly allows Shipped->Cancelled, got: %v", err)
		}

		// Cleanup
		_, _ = db.DB.Exec("DELETE FROM " + schema + ".documents WHERE doctype = 'StatusTransitionRule' AND id = 'STR-TEST-1'")
		_, _ = db.DB.Exec("DELETE FROM " + schema + ".documents WHERE doctype = 'ReasonCode' AND id IN ('RC-TEST-CANCEL','RC-TEST-HOLD')")
		_, _ = db.DB.Exec("DELETE FROM " + schema + ".documents WHERE doctype = 'Item' AND id = 'ITEM-" + sku + "'")
		_, _ = db.DB.Exec("DELETE FROM "+schema+".inventory_availability WHERE sku = $1", sku)
		_, _ = db.DB.Exec("DELETE FROM "+schema+".inventory_reservation WHERE sku = $1", sku)
	})

	// 4.5 (Stage 26.12.2). Allocation/Sourcing Engine: configurable
	// strategies over the AllocationRule master (Stage 26.12.9), tried in
	// priority order, plus the allocation-exception hold path when nothing
	// configured can produce a plan.
	t.Run("AllocationSourcing", func(t *testing.T) {
		cleanupAlloc := func() {
			_, _ = db.DB.Exec("DELETE FROM " + schema + ".documents WHERE doctype = 'AllocationRule' AND id LIKE 'AR-TEST-%'")
			_, _ = db.DB.Exec("DELETE FROM " + schema + ".documents WHERE doctype = 'Location' AND id LIKE 'ALC-TEST-%'")
			_, _ = db.DB.Exec("DELETE FROM " + schema + ".documents WHERE doctype = 'FulfillmentTask' AND id LIKE 'TSK-ALLOC-TEST-%'")
			_, _ = db.DB.Exec("DELETE FROM " + schema + ".documents WHERE doctype = 'Item' AND id LIKE 'ITEM-SKU-ALLOC-%'")
			_, _ = db.DB.Exec("DELETE FROM " + schema + ".inventory_availability WHERE location_code LIKE 'ALC-TEST-%'")
		}
		cleanupAlloc()
		defer cleanupAlloc()

		seedLocation := func(id, pincode string) {
			data, _ := json.Marshal(map[string]interface{}{"code": id, "name": id, "type": "Warehouse", "status": "Active", "pincode": pincode})
			if _, err := db.DB.Exec("INSERT INTO "+schema+".documents (id, doctype, data, status, created_by) VALUES ($1, 'Location', $2, 'Active', 'system')", id, data); err != nil {
				t.Fatalf("Failed to seed Location %s: %v", id, err)
			}
		}
		seedRule := func(id, strategy string, priority int, channel string) {
			data, _ := json.Marshal(map[string]interface{}{"code": id, "rule_name": id, "strategy": strategy, "priority": priority, "channel": channel, "status": "Active"})
			if _, err := db.DB.Exec("INSERT INTO "+schema+".documents (id, doctype, data, status, created_by) VALUES ($1, 'AllocationRule', $2, 'Active', 'system')", id, data); err != nil {
				t.Fatalf("Failed to seed AllocationRule %s: %v", id, err)
			}
		}

		// 1. Nearest Pincode: two qualifying locations, order address
		// pincode closer to one of them.
		seedLocation("ALC-TEST-NEAR-A", "560001")
		seedLocation("ALC-TEST-NEAR-B", "560099")
		skuNear := "SKU-ALLOC-NEAR"
		_, _ = db.DB.Exec("INSERT INTO "+schema+".inventory_availability (sku, location_code, on_hand, available) VALUES ($1, $2, 50, 50)", skuNear, "ALC-TEST-NEAR-A")
		_, _ = db.DB.Exec("INSERT INTO "+schema+".inventory_availability (sku, location_code, on_hand, available) VALUES ($1, $2, 50, 50)", skuNear, "ALC-TEST-NEAR-B")
		seedRule("AR-TEST-NEAR", "Nearest Pincode", 1, "")
		plan, err := ResolveAllocationPlan(tenantID, "", "12 Some Street 560005", []SalesOrderLineInput{{SKU: skuNear, Qty: 5, UnitPrice: 10}})
		if err != nil {
			t.Fatalf("ResolveAllocationPlan (nearest pincode) failed: %v", err)
		}
		if plan == nil || plan.LineLocations[0] != "ALC-TEST-NEAR-A" {
			t.Errorf("Expected Nearest Pincode to pick ALC-TEST-NEAR-A (560001, closer to 560005 than 560099), got: %+v", plan)
		}
		_, _ = db.DB.Exec("DELETE FROM " + schema + ".documents WHERE doctype = 'AllocationRule' AND id = 'AR-TEST-NEAR'")

		// 2. Lowest Workload: two qualifying locations, one busier with open
		// FulfillmentTask rows.
		seedLocation("ALC-TEST-WKLD-A", "")
		seedLocation("ALC-TEST-WKLD-B", "")
		skuWkld := "SKU-ALLOC-WKLD"
		_, _ = db.DB.Exec("INSERT INTO "+schema+".inventory_availability (sku, location_code, on_hand, available) VALUES ($1, $2, 50, 50)", skuWkld, "ALC-TEST-WKLD-A")
		_, _ = db.DB.Exec("INSERT INTO "+schema+".inventory_availability (sku, location_code, on_hand, available) VALUES ($1, $2, 50, 50)", skuWkld, "ALC-TEST-WKLD-B")
		for _, taskID := range []string{"TSK-ALLOC-TEST-1", "TSK-ALLOC-TEST-2", "TSK-ALLOC-TEST-3"} {
			taskData, _ := json.Marshal(map[string]interface{}{"code": taskID, "location_code": "ALC-TEST-WKLD-A", "status": "Pending"})
			if _, err := db.DB.Exec("INSERT INTO "+schema+".documents (id, doctype, data, status, created_by) VALUES ($1, 'FulfillmentTask', $2, 'Pending', 'system')", taskID, taskData); err != nil {
				t.Fatalf("Failed to seed FulfillmentTask %s: %v", taskID, err)
			}
		}
		seedRule("AR-TEST-WKLD", "Lowest Workload", 1, "")
		plan, err = ResolveAllocationPlan(tenantID, "", "1 Road 560000", []SalesOrderLineInput{{SKU: skuWkld, Qty: 5, UnitPrice: 10}})
		if err != nil {
			t.Fatalf("ResolveAllocationPlan (lowest workload) failed: %v", err)
		}
		if plan == nil || plan.LineLocations[0] != "ALC-TEST-WKLD-B" {
			t.Errorf("Expected Lowest Workload to pick ALC-TEST-WKLD-B (0 open tasks vs A's 3), got: %+v", plan)
		}
		_, _ = db.DB.Exec("DELETE FROM " + schema + ".documents WHERE doctype = 'AllocationRule' AND id = 'AR-TEST-WKLD'")

		// 3. Oldest Stock: two qualifying locations, one touched longer ago
		// (the updated_at proxy - see singleLocationOldestStock's own
		// comment for why).
		seedLocation("ALC-TEST-OLD-A", "")
		seedLocation("ALC-TEST-OLD-B", "")
		skuOld := "SKU-ALLOC-OLD"
		_, _ = db.DB.Exec("INSERT INTO "+schema+".inventory_availability (sku, location_code, on_hand, available) VALUES ($1, $2, 50, 50)", skuOld, "ALC-TEST-OLD-A")
		_, _ = db.DB.Exec("INSERT INTO "+schema+".inventory_availability (sku, location_code, on_hand, available) VALUES ($1, $2, 50, 50)", skuOld, "ALC-TEST-OLD-B")
		_, _ = db.DB.Exec("UPDATE "+schema+".inventory_availability SET updated_at = CURRENT_TIMESTAMP - INTERVAL '30 days' WHERE sku = $1 AND location_code = $2", skuOld, "ALC-TEST-OLD-A")
		seedRule("AR-TEST-OLD", "Oldest Stock", 1, "")
		plan, err = ResolveAllocationPlan(tenantID, "", "1 Road 560000", []SalesOrderLineInput{{SKU: skuOld, Qty: 5, UnitPrice: 10}})
		if err != nil {
			t.Fatalf("ResolveAllocationPlan (oldest stock) failed: %v", err)
		}
		if plan == nil || plan.LineLocations[0] != "ALC-TEST-OLD-A" {
			t.Errorf("Expected Oldest Stock to pick ALC-TEST-OLD-A (touched 30 days ago vs B's just now), got: %+v", plan)
		}
		_, _ = db.DB.Exec("DELETE FROM " + schema + ".documents WHERE doctype = 'AllocationRule' AND id = 'AR-TEST-OLD'")

		// 4. Split Shipment, plus priority fallthrough in the same scenario:
		// a higher-priority Highest ATS rule can't find one location
		// stocking both SKUs, so ResolveAllocationPlan falls through to the
		// lower-priority Split Shipment rule, which succeeds by putting each
		// line at its own qualifying location.
		seedLocation("ALC-TEST-SPLIT-X", "")
		seedLocation("ALC-TEST-SPLIT-Y", "")
		skuSplitA := "SKU-ALLOC-SPLIT-A"
		skuSplitB := "SKU-ALLOC-SPLIT-B"
		_, _ = db.DB.Exec("INSERT INTO "+schema+".inventory_availability (sku, location_code, on_hand, available) VALUES ($1, $2, 10, 10)", skuSplitA, "ALC-TEST-SPLIT-X")
		_, _ = db.DB.Exec("INSERT INTO "+schema+".inventory_availability (sku, location_code, on_hand, available) VALUES ($1, $2, 10, 10)", skuSplitB, "ALC-TEST-SPLIT-Y")
		seedRule("AR-TEST-SPLIT-1", "Highest ATS", 1, "")
		seedRule("AR-TEST-SPLIT-2", "Split Shipment", 2, "")
		plan, err = ResolveAllocationPlan(tenantID, "", "1 Road 560000", []SalesOrderLineInput{{SKU: skuSplitA, Qty: 5, UnitPrice: 10}, {SKU: skuSplitB, Qty: 5, UnitPrice: 10}})
		if err != nil {
			t.Fatalf("ResolveAllocationPlan (split shipment) failed: %v", err)
		}
		if plan == nil || !plan.Split || plan.LineLocations[0] != "ALC-TEST-SPLIT-X" || plan.LineLocations[1] != "ALC-TEST-SPLIT-Y" {
			t.Errorf("Expected a split plan (X for line 0, Y for line 1), got: %+v", plan)
		}
		if plan != nil && plan.Strategy != "Split Shipment" {
			t.Errorf("Expected the Split Shipment rule to be the one that produced the plan (Highest ATS should have failed to find one location stocking both SKUs), got strategy %q", plan.Strategy)
		}
		_, _ = db.DB.Exec("DELETE FROM " + schema + ".documents WHERE doctype = 'AllocationRule' AND id IN ('AR-TEST-SPLIT-1', 'AR-TEST-SPLIT-2')")

		// 5. Allocation exception: a Manual rule always fails to produce a
		// plan - CreateSalesOrder places the order On Hold with
		// HoldAllocationFailed even though stock genuinely exists (proves
		// this is a routing decision, not a disguised stock shortage).
		skuManual := "SKU-ALLOC-MANUAL"
		itemData, _ := json.Marshal(map[string]interface{}{"code": skuManual, "name": "Manual Alloc Test Item", "barcode": skuManual})
		if _, err := db.DB.Exec("INSERT INTO "+schema+".documents (id, doctype, data, status, created_by) VALUES ($1, 'Item', $2, 'Active', 'system')", "ITEM-"+skuManual, itemData); err != nil {
			t.Fatalf("Failed to seed Item %s: %v", skuManual, err)
		}
		_, _ = db.DB.Exec("INSERT INTO "+schema+".inventory_availability (sku, location_code, on_hand, available) VALUES ($1, $2, 50, 50)", skuManual, "ALC-TEST-NEAR-A")
		seedRule("AR-TEST-MANUAL", "Manual", 1, "ManualTestChannel")
		manualOrderID, err := CreateSalesOrder(tenantID, "ManualTestChannel", "CHORD-MANUAL-1", "Test Customer", "1 Road 560000", "Confirmed", []SalesOrderLineInput{{SKU: skuManual, Qty: 1, UnitPrice: 10}})
		if err != nil {
			t.Fatalf("CreateSalesOrder (manual allocation) failed: %v", err)
		}
		defer func() {
			_, _ = db.DB.Exec("DELETE FROM "+schema+".documents WHERE doctype = 'SalesOrderLine' AND data->>'order_id' = $1", manualOrderID)
			_, _ = db.DB.Exec("DELETE FROM "+schema+".documents WHERE doctype = 'SalesOrder' AND id = $1", manualOrderID)
		}()
		var manualStatus, manualHoldReason string
		if err := db.DB.QueryRow("SELECT status, data->>'hold_reason' FROM "+schema+".documents WHERE id = $1", manualOrderID).Scan(&manualStatus, &manualHoldReason); err != nil {
			t.Fatalf("Failed to read manual-allocation order: %v", err)
		}
		if manualStatus != "On Hold" || manualHoldReason != HoldAllocationFailed {
			t.Errorf("Expected On Hold / %s for a Manual allocation rule, got status=%q reason=%q", HoldAllocationFailed, manualStatus, manualHoldReason)
		}
	})

	// 5. Test Double-Entry journal booking and POS cart checkouts
	t.Run("FinanceDoubleEntryAndPOS", func(t *testing.T) {
		// Clean postings for test consistency
		_, _ = db.DB.Exec("DELETE FROM " + schema + ".gl_postings")

		// 1. Post balanced double-entry
		debits := map[string]int{"1100": 1000}
		credits := map[string]int{"4100": 1000}
		err := PostDoubleEntry(tenantID, "TestVoucher", "V-001", debits, credits, "", "")
		if err != nil {
			t.Fatalf("Failed to post balanced journal entry: %v", err)
		}

		// 2. Expect failure on unbalanced entries
		badDebits := map[string]int{"1100": 1000}
		badCredits := map[string]int{"4100": 800}
		err = PostDoubleEntry(tenantID, "TestVoucher", "V-002", badDebits, badCredits, "", "")
		if err == nil {
			t.Errorf("Expected error when posting unbalanced journal entries, but got none")
		}

		// 24.5: a repeat call with the same postingKey (simulating a client
		// retry after a dropped response) must not double-post.
		idemDebits := map[string]int{"1100": 500}
		idemCredits := map[string]int{"4100": 500}
		if err := PostDoubleEntry(tenantID, "TestVoucher", "V-003", idemDebits, idemCredits, "", "TestVoucher:V-003:TEST"); err != nil {
			t.Fatalf("first idempotent post failed: %v", err)
		}
		if err := PostDoubleEntry(tenantID, "TestVoucher", "V-003", idemDebits, idemCredits, "", "TestVoucher:V-003:TEST"); err != nil {
			t.Fatalf("second (retried) idempotent post should be a silent no-op, not an error: %v", err)
		}
		var idemCount int
		_ = db.DB.QueryRow("SELECT COUNT(*) FROM " + schema + ".gl_postings WHERE idempotency_key = 'TestVoucher:V-003:TEST'").Scan(&idemCount)
		if idemCount != 2 {
			t.Fatalf("expected exactly 2 gl_postings rows (1 debit + 1 credit) from the FIRST call only, got %d", idemCount)
		}

		// 3. Test trial balance retrieval. 26.0.2: expected totals are 1500,
		// not 1000 - V-001 (1000/1000) plus V-003's idempotency-test posting
		// just above (500/500, posted exactly once) both land in gl_postings
		// before this check runs. V-003 was added in Stage 24.5 without
		// updating this pre-existing assertion, which is the actual root
		// cause of the "expects 9000, gets 9500" regression this repo kept
		// re-discovering - a stale test assertion, not shared-DB fixture
		// debris or a broken posting engine (confirmed: gl_postings is
		// wiped unconditionally for this schema at the top of this subtest,
		// so nothing external can be contaminating it by this point).
		tb, err := GetTrialBalance(tenantID)
		if err != nil {
			t.Fatalf("Failed to fetch trial balance: %v", err)
		}

		if tb["balanced"].(bool) == false || tb["total_debits"].(int) != 1500 || tb["total_credits"].(int) != 1500 {
			t.Errorf("Trial balance mismatch: %+v", tb)
		}

		// 4. Test automated sales booking
		err = PostSalesFinanceBooking(tenantID, "CRT-TEST-99", 5000, 3000, "Cash")
		if err != nil {
			t.Fatalf("Failed to post automated sales bookings: %v", err)
		}

		tbPost, _ := GetTrialBalance(tenantID)
		if tbPost["total_debits"].(int) != 9500 || tbPost["total_credits"].(int) != 9500 {
			t.Errorf("Expected total trial balance debits/credits of 9500 (1000 V-001 + 500 V-003 + 5000 sale + 3000 COGS), got: %+v", tbPost)
		}
	})

	// 6. Test Shopify Channel Sync and Sourcing Routing
	t.Run("ShopifySyncAndSourcingRouting", func(t *testing.T) {
		// Clean mappings
		_, _ = db.DB.Exec("DELETE FROM " + schema + ".channel_product_mapping")
		_, _ = db.DB.Exec("DELETE FROM " + schema + ".channel_order_mapping")

		// 1. Configure channel product map
		err := MapChannelProduct(tenantID, "Shopify", "BAR12345", "SHOPIFY-GOLD-01")
		if err != nil {
			t.Fatalf("Failed to configure channel product mapping: %v", err)
		}

		// 2. Set up availability at WH01 and WH02
		_, _ = db.DB.Exec("DELETE FROM "+schema+".inventory_availability WHERE sku = $1", "BAR12345")
		_, _ = db.DB.Exec("INSERT INTO "+schema+".inventory_availability (sku, location_code, on_hand, available) VALUES ($1, $2, $3, $3)", "BAR12345", "WH01", 40)
		_, _ = db.DB.Exec("INSERT INTO "+schema+".inventory_availability (sku, location_code, on_hand, available) VALUES ($1, $2, $3, $3)", "BAR12345", "WH02", 80)

		// Verify sourcing routes to WH02 (which has 80 available) rather than WH01 (which has 40)
		orderItems := []map[string]interface{}{
			{"sku": "BAR12345", "qty": 5},
		}
		loc, err := FindBestFulfillmentNode(tenantID, orderItems)
		if err != nil {
			t.Fatalf("Failed to find best fulfillment node: %v", err)
		}
		if loc != "WH02" {
			t.Errorf("Expected order to route to WH02 (higher stock 80), but routed to: %s", loc)
		}

		// 3. Import Channel Order (validates mapping translation, reservation, and idempotency)
		orderID, err := ImportChannelOrder(tenantID, "Shopify", "WEB-9988", []map[string]interface{}{
			{"sku": "SHOPIFY-GOLD-01", "qty": 10},
		})
		if err != nil {
			t.Fatalf("Failed to import channel order: %v", err)
		}
		if orderID != "ORD-Shopify-WEB-9988" {
			t.Errorf("Expected imported order ID ORD-Shopify-WEB-9988, got: %s", orderID)
		}

		// 4. Expect idempotency block on duplicate imports
		_, err = ImportChannelOrder(tenantID, "Shopify", "WEB-9988", []map[string]interface{}{
			{"sku": "SHOPIFY-GOLD-01", "qty": 10},
		})
		if err == nil || err.Error() != "ORDER_ALREADY_IMPORTED" {
			t.Errorf("Expected ORDER_ALREADY_IMPORTED error for duplicate order ID, got: %v", err)
		}
	})

	// 7. Test Store Fulfillment Picking Tasks and Return Anywhere
	t.Run("StoreFulfillmentAndReturnAnywhere", func(t *testing.T) {
		// Clean and prepare inventory
		_, _ = db.DB.Exec("DELETE FROM " + schema + ".inventory_availability WHERE sku = 'BAR12345'")
		_, _ = db.DB.Exec("INSERT INTO " + schema + ".inventory_availability (sku, location_code, on_hand, available, reserved) VALUES ('BAR12345', 'WH01', 50, 50, 0)")
		_, _ = db.DB.Exec("INSERT INTO " + schema + ".inventory_availability (sku, location_code, on_hand, available, reserved) VALUES ('BAR12345', 'WH02', 100, 100, 10)")

		// 1. Create a fulfillment picking task for WH01
		taskItems := []interface{}{
			map[string]interface{}{"sku": "BAR12345", "qty": 10},
		}
		taskID, err := CreateFulfillmentTasks(tenantID, "ORD-WEB-111", "WH01", taskItems)
		if err != nil {
			t.Fatalf("Failed to create fulfillment task: %v", err)
		}

		// Set reservation manually to simulate ordering
		_, _ = db.DB.Exec("UPDATE " + schema + ".inventory_availability SET reserved = 10 WHERE sku = 'BAR12345' AND location_code = 'WH01'")

		// 2. Reject task at WH01 -> Expect system to re-route to WH02 (which has 100 units available)
		err = TransitionTaskStatus(tenantID, taskID, "Rejected")
		if err != nil {
			t.Fatalf("Failed to transition task status to Rejected: %v", err)
		}

		// Verify WH01 reserved count is released back to 0
		var wh01Reserved int
		_ = db.DB.QueryRow("SELECT reserved FROM " + schema + ".inventory_availability WHERE sku = 'BAR12345' AND location_code = 'WH01'").Scan(&wh01Reserved)
		if wh01Reserved != 0 {
			t.Errorf("Expected WH01 reserved count to be released to 0, got: %d", wh01Reserved)
		}

		// Verify WH02 reserved count increased (original 10 + new 10 = 20)
		var wh02Reserved int
		_ = db.DB.QueryRow("SELECT reserved FROM " + schema + ".inventory_availability WHERE sku = 'BAR12345' AND location_code = 'WH02'").Scan(&wh02Reserved)
		if wh02Reserved != 20 {
			t.Errorf("Expected WH02 reserved count to rise to 20, got: %d", wh02Reserved)
		}

		// 3. Test Return Anywhere: Return items originally from WH02 to WH01.
		// ProcessReturnAnywhere's SALESR-0129/0130/0131 checks (Stage 25
		// Batch 3) now require a real original sale to validate the return
		// against - seed the Paid POSCart "ORD-WEB-111" refers to as its
		// original bill (10 units sold, 5 being returned here).
		_, _ = db.DB.Exec("DELETE FROM "+schema+".documents WHERE doctype = 'POSCart' AND id = $1", "ORD-WEB-111")
		_, err = db.DB.Exec("INSERT INTO "+schema+".documents (id, doctype, data, status, created_by) VALUES ($1, 'POSCart', $2, 'Paid', 'system')",
			"ORD-WEB-111", `{"items":[{"sku":"BAR12345","qty":10}]}`)
		if err != nil {
			t.Fatalf("Failed to seed original POSCart for Return Anywhere test: %v", err)
		}

		returnItems := []interface{}{
			map[string]interface{}{
				"sku":        "BAR12345",
				"qty":        5,
				"sale_price": 5000.0,
				"cost_price": 3000.0,
			},
		}

		_, err = ProcessReturnAnywhere(tenantID, "WH01", "ORD-WEB-111", returnItems)
		if err != nil {
			t.Fatalf("Failed to process Return Anywhere: %v", err)
		}

		// Verify stock at WH01 increased by 5 (original 50 + returned 5 = 55)
		var wh01OnHand int
		_ = db.DB.QueryRow("SELECT on_hand FROM " + schema + ".inventory_availability WHERE sku = 'BAR12345' AND location_code = 'WH01'").Scan(&wh01OnHand)
		if wh01OnHand != 55 {
			t.Errorf("Expected WH01 stock to rise to 55, got: %d", wh01OnHand)
		}
	})

	// 7.5 (Stage 26.12.3). Fulfillment Pick/Pack: scan-first validation,
	// short-pick reason capture, and blocked pack completion on any
	// exact-qty mismatch.
	t.Run("PickPack", func(t *testing.T) {
		cleanupPP := func() {
			_, _ = db.DB.Exec("DELETE FROM " + schema + ".documents WHERE doctype = 'FulfillmentTask' AND id LIKE 'TSK-PP-TEST%'")
			_, _ = db.DB.Exec("DELETE FROM " + schema + ".documents WHERE doctype = 'Item' AND id LIKE 'ITEM-SKU-PP-%'")
			_, _ = db.DB.Exec("DELETE FROM " + schema + ".documents WHERE doctype = 'ReasonCode' AND id = 'RC-PP-SHORT'")
		}
		cleanupPP()
		defer cleanupPP()

		skuA, skuB := "SKU-PP-A", "SKU-PP-B"
		itemAData, _ := json.Marshal(map[string]interface{}{"code": skuA, "name": "Pick Pack Item A", "barcode": "BC-PP-A", "status": "Active"})
		if _, err := db.DB.Exec("INSERT INTO "+schema+".documents (id, doctype, data, status, created_by) VALUES ($1, 'Item', $2, 'Active', 'system')", "ITEM-"+skuA, itemAData); err != nil {
			t.Fatalf("Failed to seed Item A: %v", err)
		}
		// Item B has no distinct barcode - scans against its own SKU/code
		// exercise resolveScanToItem's code-fallback path.
		itemBData, _ := json.Marshal(map[string]interface{}{"code": skuB, "name": "Pick Pack Item B", "status": "Active"})
		if _, err := db.DB.Exec("INSERT INTO "+schema+".documents (id, doctype, data, status, created_by) VALUES ($1, 'Item', $2, 'Active', 'system')", "ITEM-"+skuB, itemBData); err != nil {
			t.Fatalf("Failed to seed Item B: %v", err)
		}
		reasonData, _ := json.Marshal(map[string]interface{}{"code": "RC-SHORT", "description": "Out of stock at bin", "category": "Short Pick", "status": "Active"})
		if _, err := db.DB.Exec("INSERT INTO "+schema+".documents (id, doctype, data, status, created_by) VALUES ($1, 'ReasonCode', $2, 'Active', 'system')", "RC-PP-SHORT", reasonData); err != nil {
			t.Fatalf("Failed to seed Short Pick ReasonCode: %v", err)
		}

		taskID, err := CreateFulfillmentTasks(tenantID, "ORD-PP-TEST", "WH01", []interface{}{
			map[string]interface{}{"sku": skuA, "qty": 5},
			map[string]interface{}{"sku": skuB, "qty": 3},
		})
		if err != nil {
			t.Fatalf("Failed to create pick/pack test task: %v", err)
		}
		// CreateFulfillmentTasks mints its own TSK-<nanos> id, outside the
		// TSK-PP-TEST% cleanup pattern - track and remove it explicitly too.
		defer func() {
			_, _ = db.DB.Exec("DELETE FROM "+schema+".documents WHERE doctype = 'FulfillmentTask' AND id = $1", taskID)
		}()

		// 1. An unknown scan matches no item at all.
		if _, _, err := ScanPickItem(tenantID, taskID, "NO-SUCH-BARCODE"); err == nil {
			t.Errorf("Expected an unknown scan to be rejected")
		}

		// 2. A real product's barcode that isn't part of this task is
		// rejected naming that product, not a bare "invalid scan".
		otherData, _ := json.Marshal(map[string]interface{}{"code": "SKU-PP-OTHER", "name": "Unrelated Product", "barcode": "BC-PP-OTHER", "status": "Active"})
		if _, err := db.DB.Exec("INSERT INTO "+schema+".documents (id, doctype, data, status, created_by) VALUES ($1, 'Item', $2, 'Active', 'system')", "ITEM-SKU-PP-OTHER", otherData); err != nil {
			t.Fatalf("Failed to seed unrelated Item: %v", err)
		}
		defer func() {
			_, _ = db.DB.Exec("DELETE FROM " + schema + ".documents WHERE id = 'ITEM-SKU-PP-OTHER'")
		}()
		_, _, err = ScanPickItem(tenantID, taskID, "BC-PP-OTHER")
		if err == nil || !strings.Contains(err.Error(), "Unrelated Product") {
			t.Errorf("Expected a not-part-of-this-task scan to name the actual product, got: %v", err)
		}

		// 3. Pick 3 of A's 5 (by barcode).
		for i := 0; i < 3; i++ {
			sku, picked, err := ScanPickItem(tenantID, taskID, "BC-PP-A")
			if err != nil {
				t.Fatalf("ScanPickItem A (%d) failed: %v", i, err)
			}
			if sku != skuA || picked != i+1 {
				t.Errorf("Expected picked_qty %d for %s, got sku=%s picked=%d", i+1, skuA, sku, picked)
			}
		}
		var taskStatus string
		if err := db.DB.QueryRow("SELECT status FROM "+schema+".documents WHERE id = $1", taskID).Scan(&taskStatus); err != nil {
			t.Fatalf("Failed to read task status: %v", err)
		}
		if taskStatus != "Picking" {
			t.Errorf("Expected task to be Picking after the first pick scan, got %q", taskStatus)
		}

		// 4. Completing pack now must fail: A is short 2 unresolved units and
		// B hasn't been picked at all - the "unresolved pick shortfall"
		// invariant, not the packed-vs-picked one.
		if err := CompletePackTask(tenantID, taskID); err == nil || !strings.Contains(err.Error(), "unresolved pick shortfall") {
			t.Errorf("Expected CompletePackTask to block on an unresolved pick shortfall, got: %v", err)
		}

		// 5. Short-pick A's remaining 2 in one action - requires a mandatory,
		// category-matched reason code.
		if err := ShortPickLine(tenantID, taskID, skuA, ""); err == nil {
			t.Errorf("Expected ShortPickLine to reject a missing reason code")
		}
		if err := ShortPickLine(tenantID, taskID, skuA, "RC-PP-SHORT"); err != nil {
			t.Fatalf("ShortPickLine failed: %v", err)
		}
		// A duplicate scan against A is now rejected - it's fully
		// picked-or-short-picked (3 picked + 2 short == 5).
		if _, _, err := ScanPickItem(tenantID, taskID, "BC-PP-A"); err == nil || !strings.Contains(err.Error(), "duplicate scan") {
			t.Errorf("Expected a scan against a fully picked+short-picked line to be rejected as a duplicate, got: %v", err)
		}

		// 6. Pick all 3 of B (by SKU/code, no distinct barcode).
		for i := 0; i < 3; i++ {
			if _, _, err := ScanPickItem(tenantID, taskID, skuB); err != nil {
				t.Fatalf("ScanPickItem B (%d) failed: %v", i, err)
			}
		}

		// 7. Completing pack now must still fail: picking is fully resolved
		// for both lines, but nothing has been packed yet.
		if err := CompletePackTask(tenantID, taskID); err == nil || !strings.Contains(err.Error(), "picked_qty=3 but packed_qty=0") {
			t.Errorf("Expected CompletePackTask to block on packed_qty=0 for B (picked_qty=3), got: %v", err)
		}

		// 8. Pack A's 3 picked units and B's 3 picked units. Packing ahead of
		// picking is structurally impossible - a 4th pack scan for A must
		// fail since packed_qty would equal picked_qty after the 3rd.
		for i := 0; i < 3; i++ {
			if _, _, err := ScanPackItem(tenantID, taskID, "BC-PP-A"); err != nil {
				t.Fatalf("ScanPackItem A (%d) failed: %v", i, err)
			}
		}
		if _, _, err := ScanPackItem(tenantID, taskID, "BC-PP-A"); err == nil {
			t.Errorf("Expected a 4th pack scan for A (picked_qty=3) to be rejected - packed can never exceed picked")
		}
		for i := 0; i < 3; i++ {
			if _, _, err := ScanPackItem(tenantID, taskID, skuB); err != nil {
				t.Fatalf("ScanPackItem B (%d) failed: %v", i, err)
			}
		}

		// 9. Now pack completion succeeds.
		if err := CompletePackTask(tenantID, taskID); err != nil {
			t.Fatalf("CompletePackTask failed once every line matched exactly: %v", err)
		}
		if err := db.DB.QueryRow("SELECT status FROM "+schema+".documents WHERE id = $1", taskID).Scan(&taskStatus); err != nil {
			t.Fatalf("Failed to read task status after completion: %v", err)
		}
		if taskStatus != "Packed" {
			t.Errorf("Expected task to be Packed, got %q", taskStatus)
		}

		// 10. A Packed task can no longer be scanned into.
		if _, _, err := ScanPickItem(tenantID, taskID, "BC-PP-A"); err == nil {
			t.Errorf("Expected a scan against a Packed task to be rejected")
		}
	})

	// 8. Test Scale Simulation Concurrency (Phase 5)
	t.Run("ScaleSimulationConcurrency", func(t *testing.T) {
		// Seed 100 stores for fast test scale execution (running 50 transactions with 5 parallel workers)
		err := SeedScaleTestData(tenantID, 100, "BAR-SCALE", 500)
		if err != nil {
			t.Fatalf("Failed to seed scale test data: %v", err)
		}

		report, err := RunScaleSimulation(tenantID, 5, 50, "BAR-SCALE", 100)
		if err != nil {
			t.Fatalf("Failed to execute scale simulation: %v", err)
		}

		if report["success_count"].(int) != 50 {
			t.Errorf("Expected 50 successful simulation transactions, got: %+v", report)
		}

		// Verify GL Trial Balance remains balanced post simulation
		tb, err := GetTrialBalance(tenantID)
		if err != nil {
			t.Fatalf("Failed to query trial balance post-simulation: %v", err)
		}

		if tb["balanced"].(bool) == false {
			t.Errorf("GL Trial balance became unbalanced after concurrent simulation run: %+v", tb)
		}
	})

	// 9. Test Marketplace OMS Settlements and Logistics Bookings (Phase 6)
	t.Run("MarketplaceOMSAndLogistics", func(t *testing.T) {
		// Clean and prepare postings
		_, _ = db.DB.Exec("DELETE FROM " + schema + ".gl_postings")

		// 1. Test Logistics Booking creation
		bookingID, err := CreateLogisticsBooking(tenantID, "ORD-WEB-111", "", "FedEx", "TRK123456", "", 250)
		if err != nil {
			t.Fatalf("Failed to create logistics booking: %v", err)
		}
		if bookingID == "" {
			t.Errorf("Expected booking ID returned, got empty string")
		}

		// 2. Seed Accounts Receivable balance (debit 1300, credit 4100)
		err = SeedReceivableBalance(tenantID, 10000, "ORD-WEB-111")
		if err != nil {
			t.Fatalf("Failed to seed receivable balance: %v", err)
		}

		// 3. Test payout settlement reconciliation (10000 sale, 1500 commission, 8500 net payout)
		err = ProcessMarketplaceSettlement(tenantID, "Shopify", "SETT-SH-01", 10000, 1500, 8500, []string{"ORD-WEB-111"})
		if err != nil {
			t.Fatalf("Failed to process marketplace settlement: %v", err)
		}

		// 4. Assert GL Balances
		tb, err := GetTrialBalance(tenantID)
		if err != nil {
			t.Fatalf("Failed to fetch trial balance: %v", err)
		}

		if tb["balanced"].(bool) == false {
			t.Errorf("GL Trial balance became unbalanced after settlement: %+v", tb)
		}

		// Marshal and unmarshal to check specific balances
		balancesBytes, _ := json.Marshal(tb["balances"])
		var testBal []struct {
			Code   string `json:"account_code"`
			Debit  int    `json:"debit"`
			Credit int    `json:"credit"`
		}
		_ = json.Unmarshal(balancesBytes, &testBal)

		foundAR := false
		foundComm := false
		for _, b := range testBal {
			if b.Code == "1300" {
				foundAR = true
				if b.Debit != 10000 || b.Credit != 10000 {
					t.Errorf("Accounts Receivable expected debit 10000, credit 10000, got: debit %d, credit %d", b.Debit, b.Credit)
				}
			}
			if b.Code == "5200" {
				foundComm = true
				if b.Debit != 1500 {
					t.Errorf("Marketplace Commission expected debit 1500, got: %d", b.Debit)
				}
			}
		}

		if !foundAR || !foundComm {
			t.Errorf("Expected AR (1300) and Commission (5200) balances to be present, but weren't: %+v", testBal)
		}
	})

	// 9b. Test the Shipment/Manifest Engine (Stage 26.12.4): courier
	// serviceability, AWB-assignment booking, manifest grouping, and the
	// handover cascade's split-shipment-aware SalesOrder closure rule.
	t.Run("ShipmentManifestEngine", func(t *testing.T) {
		sku := "SKU-SHIP-TEST-1"
		locA, locB := "WH01", "WH02"

		// Clean fixtures from any prior run.
		_, _ = db.DB.Exec("DELETE FROM "+schema+".documents WHERE doctype = 'Item' AND data->>'code' = $1", sku)
		_, _ = db.DB.Exec("DELETE FROM " + schema + ".documents WHERE doctype = 'CourierServiceArea' AND id IN ('CSA-TEST-FEDEX','CSA-TEST-BLUEDART')")
		_, _ = db.DB.Exec("DELETE FROM "+schema+".inventory_availability WHERE sku = $1", sku)

		itemData, _ := json.Marshal(map[string]interface{}{"code": sku, "name": "Shipment Engine Test Item", "barcode": sku})
		if _, err := db.DB.Exec("INSERT INTO "+schema+".documents (id, doctype, data, status, created_by) VALUES ($1, 'Item', $2, 'Active', 'system')", "ITEM-"+sku, itemData); err != nil {
			t.Fatalf("Failed to seed test Item: %v", err)
		}
		defer db.DB.Exec("DELETE FROM "+schema+".documents WHERE doctype = 'Item' AND data->>'code' = $1", sku)

		fedexData, _ := json.Marshal(map[string]interface{}{"code": "CSA-FEDEX", "courier": "FedEx", "pincode_prefix": "560", "priority": 1, "status": "Active"})
		if _, err := db.DB.Exec("INSERT INTO "+schema+".documents (id, doctype, data, status, created_by) VALUES ($1, 'CourierServiceArea', $2, 'Active', 'system')", "CSA-TEST-FEDEX", fedexData); err != nil {
			t.Fatalf("Failed to seed FedEx CourierServiceArea: %v", err)
		}
		blueDartData, _ := json.Marshal(map[string]interface{}{"code": "CSA-BLUEDART", "courier": "BlueDart", "pincode_prefix": "", "priority": 2, "status": "Active"})
		if _, err := db.DB.Exec("INSERT INTO "+schema+".documents (id, doctype, data, status, created_by) VALUES ($1, 'CourierServiceArea', $2, 'Active', 'system')", "CSA-TEST-BLUEDART", blueDartData); err != nil {
			t.Fatalf("Failed to seed BlueDart CourierServiceArea: %v", err)
		}
		defer db.DB.Exec("DELETE FROM " + schema + ".documents WHERE doctype = 'CourierServiceArea' AND id IN ('CSA-TEST-FEDEX','CSA-TEST-BLUEDART')")

		if _, err := db.DB.Exec("INSERT INTO "+schema+".inventory_availability (sku, location_code, on_hand, available) VALUES ($1, $2, 50, 50)", sku, locA); err != nil {
			t.Fatalf("Failed to seed inventory at %s: %v", locA, err)
		}
		if _, err := db.DB.Exec("INSERT INTO "+schema+".inventory_availability (sku, location_code, on_hand, available) VALUES ($1, $2, 50, 50)", sku, locB); err != nil {
			t.Fatalf("Failed to seed inventory at %s: %v", locB, err)
		}
		defer db.DB.Exec("DELETE FROM "+schema+".inventory_availability WHERE sku = $1", sku)

		// 1. Serviceability: a 560xxx pincode gets both couriers, FedEx
		// first (lower priority number wins); a pincode FedEx doesn't cover
		// falls back to BlueDart's blank-prefix "services everywhere" row.
		options, err := CheckCourierServiceability(tenantID, "560001")
		if err != nil {
			t.Fatalf("CheckCourierServiceability failed: %v", err)
		}
		if len(options) != 2 || options[0].Courier != "FedEx" || options[1].Courier != "BlueDart" {
			t.Errorf("Expected [FedEx, BlueDart] for a 560xxx pincode, got: %+v", options)
		}
		fallbackOptions, err := CheckCourierServiceability(tenantID, "999999")
		if err != nil {
			t.Fatalf("CheckCourierServiceability (fallback) failed: %v", err)
		}
		if len(fallbackOptions) != 1 || fallbackOptions[0].Courier != "BlueDart" {
			t.Errorf("Expected only BlueDart to cover a non-560 pincode, got: %+v", fallbackOptions)
		}

		// 2. Auto-select: a blank carrier with a serviceable pincode picks
		// the top-priority courier automatically.
		autoBookingID, err := CreateLogisticsBooking(tenantID, "ORD-SHIP-AUTO", "", "", "", "560001", 100)
		if err != nil {
			t.Fatalf("CreateLogisticsBooking (auto-select) failed: %v", err)
		}
		defer db.DB.Exec("DELETE FROM "+schema+".documents WHERE id = $1", autoBookingID)
		var autoCarrier, autoStatus string
		if err := db.DB.QueryRow("SELECT data->>'carrier', status FROM "+schema+".documents WHERE id = $1", autoBookingID).Scan(&autoCarrier, &autoStatus); err != nil {
			t.Fatalf("Failed to read auto-selected booking: %v", err)
		}
		if autoCarrier != "FedEx" || autoStatus != "AWB Assigned" {
			t.Errorf("Expected auto-selected booking to be FedEx/AWB Assigned, got carrier=%q status=%q", autoCarrier, autoStatus)
		}

		// 3. An explicit carrier not serviceable for the given pincode is
		// rejected.
		if _, err := CreateLogisticsBooking(tenantID, "ORD-SHIP-BAD", "", "FedEx", "", "999999", 100); err == nil {
			t.Errorf("Expected CreateLogisticsBooking to reject FedEx for a pincode it doesn't service")
		}

		// 4. Split-shipment order closure: one SalesOrder, two
		// FulfillmentTasks at different locations. Handing over only the
		// first location's manifest should leave the order Partially
		// Fulfilled; handing over the second should flip it to Shipped.
		orderID, err := CreateSalesOrder(tenantID, "TestChannel", "CHORD-SHIP-1", "Ship Test Customer", "ORDTEST 1 Ship St 560001", "Confirmed", []SalesOrderLineInput{{SKU: sku, Qty: 4, UnitPrice: 50}})
		if err != nil {
			t.Fatalf("CreateSalesOrder failed: %v", err)
		}
		defer func() {
			_, _ = db.DB.Exec("DELETE FROM "+schema+".documents WHERE doctype = 'SalesOrderLine' AND data->>'order_id' = $1", orderID)
			_, _ = db.DB.Exec("DELETE FROM "+schema+".documents WHERE doctype = 'SalesOrder' AND id = $1", orderID)
		}()

		taskAID, err := CreateFulfillmentTasks(tenantID, orderID, locA, []interface{}{map[string]interface{}{"sku": sku, "qty": 2}})
		if err != nil {
			t.Fatalf("Failed to create FulfillmentTask A: %v", err)
		}
		defer db.DB.Exec("DELETE FROM "+schema+".documents WHERE id = $1", taskAID)
		taskBID, err := CreateFulfillmentTasks(tenantID, orderID, locB, []interface{}{map[string]interface{}{"sku": sku, "qty": 2}})
		if err != nil {
			t.Fatalf("Failed to create FulfillmentTask B: %v", err)
		}
		defer db.DB.Exec("DELETE FROM "+schema+".documents WHERE id = $1", taskBID)

		bookingAID, err := CreateLogisticsBooking(tenantID, orderID, taskAID, "FedEx", "", "560001", 50)
		if err != nil {
			t.Fatalf("Failed to book shipment A: %v", err)
		}
		defer db.DB.Exec("DELETE FROM "+schema+".documents WHERE id = $1", bookingAID)
		bookingBID, err := CreateLogisticsBooking(tenantID, orderID, taskBID, "FedEx", "", "560001", 50)
		if err != nil {
			t.Fatalf("Failed to book shipment B: %v", err)
		}
		defer db.DB.Exec("DELETE FROM "+schema+".documents WHERE id = $1", bookingBID)

		manifestAID, countA, err := GenerateManifest(tenantID, "FedEx", locA)
		if err != nil {
			t.Fatalf("GenerateManifest(A) failed: %v", err)
		}
		if countA != 1 {
			t.Errorf("Expected manifest A to group exactly 1 shipment, got %d", countA)
		}
		defer db.DB.Exec("DELETE FROM "+schema+".documents WHERE id = $1", manifestAID)

		if err := HandoverManifest(tenantID, manifestAID, "test-user"); err != nil {
			t.Fatalf("HandoverManifest(A) failed: %v", err)
		}
		var orderStatusAfterA string
		if err := db.DB.QueryRow("SELECT status FROM "+schema+".documents WHERE id = $1", orderID).Scan(&orderStatusAfterA); err != nil {
			t.Fatalf("Failed to read order status after manifest A handover: %v", err)
		}
		if orderStatusAfterA != "Partially Fulfilled" {
			t.Errorf("Expected order to be Partially Fulfilled after only location A shipped, got %q", orderStatusAfterA)
		}
		var taskAStatus string
		if err := db.DB.QueryRow("SELECT status FROM "+schema+".documents WHERE id = $1", taskAID).Scan(&taskAStatus); err != nil {
			t.Fatalf("Failed to read task A status: %v", err)
		}
		if taskAStatus != "Dispatched" {
			t.Errorf("Expected FulfillmentTask A to be Dispatched after handover, got %q", taskAStatus)
		}

		manifestBID, countB, err := GenerateManifest(tenantID, "FedEx", locB)
		if err != nil {
			t.Fatalf("GenerateManifest(B) failed: %v", err)
		}
		if countB != 1 {
			t.Errorf("Expected manifest B to group exactly 1 shipment, got %d", countB)
		}
		defer db.DB.Exec("DELETE FROM "+schema+".documents WHERE id = $1", manifestBID)

		if err := HandoverManifest(tenantID, manifestBID, "test-user"); err != nil {
			t.Fatalf("HandoverManifest(B) failed: %v", err)
		}
		var orderStatusAfterB string
		if err := db.DB.QueryRow("SELECT status FROM "+schema+".documents WHERE id = $1", orderID).Scan(&orderStatusAfterB); err != nil {
			t.Fatalf("Failed to read order status after manifest B handover: %v", err)
		}
		if orderStatusAfterB != "Shipped" {
			t.Errorf("Expected order to flip to Shipped once every fulfillment task is dispatched, got %q", orderStatusAfterB)
		}

		// 5. Re-handing-over an already-handed-over manifest is refused.
		if err := HandoverManifest(tenantID, manifestAID, "test-user"); err == nil {
			t.Errorf("Expected HandoverManifest to refuse a manifest that's already Handed Over")
		}

		// 6. Tracking sync: Handed Over -> In-Transit -> Delivered; a
		// booking that hasn't been handed over yet can't jump straight to
		// In-Transit (there is none left un-handed-over here, so this
		// exercises the same booking twice in sequence instead).
		if err := RecordDeliveryEvent(tenantID, bookingAID, "In-Transit", "test-user"); err != nil {
			t.Errorf("RecordDeliveryEvent(In-Transit) on a Handed-Over booking failed: %v", err)
		}
		if err := RecordDeliveryEvent(tenantID, bookingAID, "Delivered", "test-user"); err != nil {
			t.Errorf("RecordDeliveryEvent(Delivered) failed: %v", err)
		}
		if err := RecordDeliveryEvent(tenantID, autoBookingID, "Delivered", "test-user"); err == nil {
			t.Errorf("Expected RecordDeliveryEvent(Delivered) to refuse a booking that was never Handed Over")
		}

		// 7. RTO: a Handed-Over booking can be marked RTO with a reason; a
		// Delivered booking cannot.
		if err := RecordRTO(tenantID, bookingBID, "Customer refused delivery", "test-user"); err != nil {
			t.Errorf("RecordRTO on a Handed-Over booking failed: %v", err)
		}
		if err := RecordRTO(tenantID, bookingAID, "too late", "test-user"); err == nil {
			t.Errorf("Expected RecordRTO to refuse a Delivered booking")
		}

		// 8. Shipping label includes the AWB and carrier.
		label, err := GenerateShippingLabel(tenantID, bookingBID)
		if err != nil {
			t.Fatalf("GenerateShippingLabel failed: %v", err)
		}
		if !strings.Contains(label, "FedEx") {
			t.Errorf("Expected shipping label to mention the carrier, got: %s", label)
		}
	})

	// 10. Test Advanced Optimization and Forecasting (Phase 7)
	t.Run("AdvancedOptimizationAndForecasting", func(t *testing.T) {
		// Clean and prepare
		_, _ = db.DB.Exec("DELETE FROM " + schema + ".documents WHERE doctype = 'POSCart'")

		// 1. Post a checkout to establish sales velocity (30 items sold).
		// Status is 'Paid' to match what handleCheckout actually writes in production
		// (engines/optimization.go's CalculateSalesVelocity now matches this too).
		cartID := "CRT-OPT-01"
		cartDoc := map[string]interface{}{
			"cart_number": cartID,
			"location":    "WH01",
			"status":      "Paid",
			"items": []map[string]interface{}{
				{"sku": "BAR12345", "qty": 30},
			},
		}
		cartBytes, _ := json.Marshal(cartDoc)
		_, err := db.DB.Exec("INSERT INTO "+schema+".documents (id, doctype, data, status, created_by) VALUES ($1, 'POSCart', $2, 'Paid', 'system')", cartID, cartBytes)
		if err != nil {
			t.Fatalf("Failed to insert mock POSCart: %v", err)
		}

		// Calculate sales velocity over past 30 days
		velocity, err := CalculateSalesVelocity(tenantID, "WH01", "BAR12345", 30)
		if err != nil {
			t.Fatalf("Failed to calculate sales velocity: %v", err)
		}
		if velocity != 1.0 { // 30 units sold / 30 days = 1.0 unit/day
			t.Errorf("Expected sales velocity to be 1.0, got: %f", velocity)
		}

		// 2. Test Forecasting (30 days ahead forecast should project 30 units)
		forecast, err := ForecastDemand(tenantID, "WH01", "BAR12345", 30)
		if err != nil {
			t.Fatalf("Failed to project forecast: %v", err)
		}
		if forecast != 30.0 {
			t.Errorf("Expected forecast to be 30.0, got: %f", forecast)
		}

		// 3. Test Replenishment Suggestion
		// available stock at WH01 for BAR12345 is 55 (from previous Return Anywhere test!)
		// LeadTimeDays = 7, SafetyStock = 10 -> ReorderPoint = (1.0 * 7) + 10 = 17.
		// Since available 55 >= reorder point 17, there should be NO replenishment suggested!
		suggestions, err := GetReplenishmentSuggestions(tenantID, "WH01", 7, 10)
		if err != nil {
			t.Fatalf("Failed to compute replenishment suggestions: %v", err)
		}
		for _, s := range suggestions {
			if s.SKU == "BAR12345" {
				t.Errorf("Expected no replenishment suggestions for BAR12345, but got one: %+v", s)
			}
		}

		// Increase LeadTimeDays to 60 -> ReorderPoint = (1.0 * 60) + 10 = 70.
		// Since available 55 < reorder point 70, it should suggest reordering 15 units!
		suggestions, err = GetReplenishmentSuggestions(tenantID, "WH01", 60, 10)
		if err != nil {
			t.Fatalf("Failed to compute replenishment suggestions with high lead time: %v", err)
		}
		found := false
		for _, s := range suggestions {
			if s.SKU == "BAR12345" {
				found = true
				if s.SuggestedQty != 15 {
					t.Errorf("Expected suggested replenishment quantity to be 15, got: %d", s.SuggestedQty)
				}
			}
		}
		if !found {
			t.Errorf("Expected to find replenishment suggestion for BAR12345, but did not")
		}

		// 4. Test SLA Breach Scanner
		// Clean up old task to prevent unique constraints violations
		taskID := "TASK-SLA-01"
		_, _ = db.DB.Exec("DELETE FROM "+schema+".documents WHERE id = $1", taskID)
		taskDoc := map[string]interface{}{
			"order_id":      "ORD-WEB-SLA",
			"location_code": "WH01",
			"status":        "Pending",
		}
		taskBytes, _ := json.Marshal(taskDoc)
		threeHoursAgo := time.Now().UTC().Add(-3 * time.Hour)
		_, err = db.DB.Exec("INSERT INTO "+schema+".documents (id, doctype, data, status, created_by, created_at) VALUES ($1, 'FulfillmentTask', $2, 'Pending', 'system', $3)", taskID, taskBytes, threeHoursAgo)
		if err != nil {
			t.Fatalf("Failed to insert mock FulfillmentTask: %v", err)
		}

		// Check SLA breaches with 120 minutes (2 hours) threshold
		breaches, err := GetSLABreaches(tenantID, 120.0)
		if err != nil {
			t.Fatalf("Failed to get SLA breaches: %v", err)
		}
		foundBreach := false
		for _, b := range breaches {
			if b.TaskID == taskID {
				foundBreach = true
				if b.MinutesElapsed < 179 || b.MinutesElapsed > 181 {
					t.Errorf("Expected minutes elapsed to be around 180, got: %f", b.MinutesElapsed)
				}
			}
		}
		if !foundBreach {
			t.Errorf("Expected to find SLA breach for task %s, but did not", taskID)
		}
	})

	// 11. Test SaaS Provisioning & Feature Flags (Stage 12.1)
	t.Run("SaaSProvisioningAndFeatureFlags", func(t *testing.T) {
		newTenant := "tenant_new"
		newSchema := "tenant_new_schema"

		// Clean up schema if leftover
		_, _ = db.DB.Exec("DROP SCHEMA IF EXISTS " + newSchema + " CASCADE")
		_, _ = db.DB.Exec("DELETE FROM public.tenants WHERE id = $1", newTenant)

		// Provision new tenant
		adminPassword, err := ProvisionTenantSchema(newTenant, newSchema, "0.1.0-test")
		if err != nil {
			t.Fatalf("Failed to provision new tenant schema: %v", err)
		}
		if adminPassword == "" {
			t.Errorf("Expected a generated admin password, got empty string")
		}

		// The new tenant's admin password hash must differ from tenant_default's - each tenant gets a unique credential
		var tenantDefaultHash, newTenantHash string
		_ = db.DB.QueryRow("SELECT password_hash FROM tenant_default.users WHERE id = 'admin'").Scan(&tenantDefaultHash)
		_ = db.DB.QueryRow("SELECT password_hash FROM " + newSchema + ".users WHERE id = 'admin'").Scan(&newTenantHash)
		if tenantDefaultHash == newTenantHash {
			t.Errorf("Expected the new tenant's admin password hash to differ from tenant_default's, but they matched")
		}

		// The new tenant should only have the one generated admin user, not tenant_default's cashier1/manager1/system
		var userCount int
		_ = db.DB.QueryRow("SELECT COUNT(*) FROM " + newSchema + ".users").Scan(&userCount)
		if userCount != 1 {
			t.Errorf("Expected exactly 1 seeded user (admin) in the new tenant, got %d", userCount)
		}

		// Verify default feature flags are seeded
		enabled, err := IsFeatureEnabled(newTenant, "wms_integration")
		if err != nil {
			t.Fatalf("Failed to check feature flag: %v", err)
		}
		if !enabled {
			t.Errorf("Expected wms_integration feature flag to be enabled by default")
		}

		// Toggle feature flag and verify
		err = SetFeatureFlag(newTenant, "wms_integration", false)
		if err != nil {
			t.Fatalf("Failed to update feature flag: %v", err)
		}
		enabled, _ = IsFeatureEnabled(newTenant, "wms_integration")
		if enabled {
			t.Errorf("Expected wms_integration feature flag to be disabled post toggle")
		}
	})

	// 12. Test Integration Logs & Outbox Retries (Stage 9.2)
	t.Run("IntegrationLogsAndOutboxRetries", func(t *testing.T) {
		var eventID string
		err := db.DB.QueryRow("INSERT INTO " + schema + ".integration_event_outbox (event_name, payload, status, attempts) VALUES ('test.event', '{}', 'Failed', 3) RETURNING id").Scan(&eventID)
		if err != nil {
			t.Fatalf("Failed to insert mock outbox event: %v", err)
		}

		// Query logs
		logs, err := GetIntegrationLogs(tenantID)
		if err != nil {
			t.Fatalf("Failed to query integration logs: %v", err)
		}
		found := false
		for _, l := range logs {
			if l["id"] == eventID {
				found = true
				if l["status"] != "Failed" || l["attempts"] != 3 {
					t.Errorf("Expected failed outbox event returned in logs, got: %+v", l)
				}
			}
		}
		if !found {
			t.Errorf("Expected to find mock failed event in integration logs, but did not")
		}

		// Trigger retry
		err = RetryIntegrationEvent(tenantID, eventID)
		if err != nil {
			t.Fatalf("Failed to trigger retry for event: %v", err)
		}

		// Verify status reset to Pending
		var status string
		var attempts int
		_ = db.DB.QueryRow("SELECT status, attempts FROM "+schema+".integration_event_outbox WHERE id = $1", eventID).Scan(&status, &attempts)
		if status != "Pending" || attempts != 0 {
			t.Errorf("Expected outbox event reset to Pending and 0 attempts, got: status %s, attempts %d", status, attempts)
		}
	})

	t.Run("GSTCalculation", func(t *testing.T) {
		// Intra-state: 18% splits evenly into CGST 9% + SGST 9%, no IGST.
		intra, err := CalculateGST(1000, 18, false)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if intra.CGST != 90 || intra.SGST != 90 || intra.IGST != 0 {
			t.Errorf("intra-state 1000@18%%: expected CGST=90 SGST=90 IGST=0, got CGST=%v SGST=%v IGST=%v", intra.CGST, intra.SGST, intra.IGST)
		}
		if intra.TotalTax != 180 || intra.TotalAmount != 1180 {
			t.Errorf("intra-state 1000@18%%: expected TotalTax=180 TotalAmount=1180, got TotalTax=%v TotalAmount=%v", intra.TotalTax, intra.TotalAmount)
		}

		// Inter-state: full rate as IGST, no CGST/SGST split.
		inter, err := CalculateGST(1000, 18, true)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if inter.IGST != 180 || inter.CGST != 0 || inter.SGST != 0 {
			t.Errorf("inter-state 1000@18%%: expected IGST=180 CGST=0 SGST=0, got IGST=%v CGST=%v SGST=%v", inter.IGST, inter.CGST, inter.SGST)
		}

		// Negative inputs are rejected rather than silently producing a
		// nonsensical negative tax figure.
		if _, err := CalculateGST(-100, 18, false); err == nil {
			t.Errorf("expected an error for negative taxable_amount, got none")
		}
		if _, err := CalculateGST(1000, -5, false); err == nil {
			t.Errorf("expected an error for negative gst_rate, got none")
		}
	})

	// Stage 26.12.5 (Returns/RTO/QC/Refund), 26.12.7 (exception/
	// reconciliation reports), 26.12.8 (OMS report catalog), 26.12.10
	// (notification dispatch log-only fallback paths).
	t.Run("ReturnsRTOQCRefund", func(t *testing.T) {
		sku := "SKU-RET-TEST-1"
		location := "WHRET01"
		cartID := "POS-RET-TEST-1"
		rtoSKU := "SKU-RTO-TEST-1"
		rtoLocation := "WHRTO01"
		mismatchSKU := "SKU-MISMATCH-TEST-1"

		var returnIDs, refundIDs []string
		cleanup := func() {
			for _, id := range returnIDs {
				_, _ = db.DB.Exec("DELETE FROM "+schema+".documents WHERE doctype = 'ReturnRequest' AND id = $1", id)
			}
			for _, id := range refundIDs {
				_, _ = db.DB.Exec("DELETE FROM "+schema+".documents WHERE doctype = 'RefundRequest' AND id = $1", id)
			}
			_, _ = db.DB.Exec("DELETE FROM "+schema+".documents WHERE doctype = 'POSCart' AND id = $1", cartID)
			_, _ = db.DB.Exec("DELETE FROM " + schema + ".documents WHERE doctype = 'ReasonCode' AND id = 'RC-TEST-RETURN'")
			_, _ = db.DB.Exec("DELETE FROM "+schema+".inventory_availability WHERE sku IN ($1, $2, $3)", sku, rtoSKU, mismatchSKU)
			_, _ = db.DB.Exec("DELETE FROM "+schema+".documents WHERE doctype = 'SalesOrderLine' AND data->>'sku' = $1", rtoSKU)
			_, _ = db.DB.Exec("DELETE FROM "+schema+".documents WHERE doctype = 'SalesOrder' AND id LIKE 'SO-%' AND data->>'channel_order_id' = 'CHORD-RTO-TEST-1'")
			_, _ = db.DB.Exec("DELETE FROM "+schema+".documents WHERE doctype = 'LogisticsBooking' AND data->>'destination_pincode' = '560099'")
			_, _ = db.DB.Exec("DELETE FROM " + schema + ".documents WHERE doctype = 'Item' AND id = 'ITEM-" + rtoSKU + "'")
			_, _ = db.DB.Exec("DELETE FROM " + schema + ".documents WHERE doctype = 'SalesOrder' AND id = 'SO-ALLOC-TEST-1'")
			_, _ = db.DB.Exec("DELETE FROM " + schema + ".documents WHERE doctype = 'FulfillmentTask' AND id = 'TSK-SLA-TEST-1'")
			_, _ = db.DB.Exec("DELETE FROM " + schema + ".integration_event_outbox WHERE event_name = 'oms.test.exception'")
			_, _ = db.DB.Exec("DELETE FROM " + schema + ".documents WHERE doctype = 'NotificationTemplate' AND id = 'NT-TEST-1'")
			_, _ = db.DB.Exec("DELETE FROM " + schema + ".documents WHERE doctype = 'NotificationLog' AND data->>'template_id' = 'NT-TEST-1'")
			_, _ = db.DB.Exec("DELETE FROM "+schema+".documents WHERE doctype = 'NotificationLog' AND data->>'order_id' = $1", cartID)
		}
		cleanup()
		defer cleanup()

		reasonData, _ := json.Marshal(map[string]interface{}{"code": "RC-RETURN", "description": "Customer changed mind", "category": "Return", "status": "Active"})
		if _, err := db.DB.Exec("INSERT INTO "+schema+".documents (id, doctype, data, status, created_by) VALUES ($1, 'ReasonCode', $2, 'Active', 'system')", "RC-TEST-RETURN", reasonData); err != nil {
			t.Fatalf("Failed to seed Return ReasonCode: %v", err)
		}
		// Original sale: 6 units sold at 250 (cost 150) - enough headroom to
		// exercise the happy path, a rejection, an over-limit attempt, and
		// both the Missing and Damaged disposition buckets against the same
		// SKU without exhausting the returnable-quantity pool prematurely.
		cartData, _ := json.Marshal(map[string]interface{}{
			"items": []map[string]interface{}{{"sku": sku, "qty": 6, "sale_price": 250.0, "cost_price": 150.0}},
		})
		if _, err := db.DB.Exec("INSERT INTO "+schema+".documents (id, doctype, data, status, created_by) VALUES ($1, 'POSCart', $2, 'Paid', 'system')", cartID, cartData); err != nil {
			t.Fatalf("Failed to seed POSCart: %v", err)
		}

		// 1. Happy path: request -> approve -> receive -> QC (Sellable) ->
		// refund created from the ORIGINAL sale price (this API never even
		// accepts a return-time price) -> approve -> process.
		sellableID, err := CreateReturnRequest(tenantID, "Customer Return", location, cartID, "", "tester", []ReturnItemInput{{SKU: sku, Qty: 3}})
		if err != nil {
			t.Fatalf("CreateReturnRequest (happy path) failed: %v", err)
		}
		returnIDs = append(returnIDs, sellableID)

		var noTemplateStatus string
		if err := db.DB.QueryRow("SELECT data->>'dispatch_status' FROM "+schema+".documents WHERE doctype = 'NotificationLog' AND data->>'order_id' = $1 AND data->>'event' = 'Return Requested' ORDER BY created_at DESC LIMIT 1", cartID).Scan(&noTemplateStatus); err != nil {
			t.Errorf("Expected a NotificationLog row for Return Requested with no template configured: %v", err)
		} else if noTemplateStatus != "Skipped-NoTemplate" {
			t.Errorf("Expected dispatch_status Skipped-NoTemplate, got %q", noTemplateStatus)
		}

		if err := ApproveReturnRequest(tenantID, sellableID, "approver1"); err != nil {
			t.Fatalf("ApproveReturnRequest failed: %v", err)
		}
		if err := ReceiveReturnRequest(tenantID, sellableID, "warehouse1"); err != nil {
			t.Fatalf("ReceiveReturnRequest failed: %v", err)
		}
		refundTotal, refundRequestID, err := ApplyReturnQC(tenantID, sellableID, map[string]string{sku: "Sellable"}, "qc1")
		if err != nil {
			t.Fatalf("ApplyReturnQC (Sellable) failed: %v", err)
		}
		if refundTotal != 750 {
			t.Errorf("Expected refund total 750 (3 * original sale price 250), got %v", refundTotal)
		}
		if refundRequestID == "" {
			t.Fatalf("Expected a RefundRequest to be created for a refund-eligible QC result")
		}
		refundIDs = append(refundIDs, refundRequestID)

		ats, err := GetAvailableToSell(tenantID, sku, location)
		if err != nil {
			t.Fatalf("Failed to read ATS after Sellable QC: %v", err)
		}
		if ats["available"].(int) != 3 || ats["on_hand"].(int) != 3 {
			t.Errorf("Expected 3 available/on_hand after a Sellable QC receipt, got: %v", ats)
		}

		if err := ApproveRefundRequest(tenantID, refundRequestID, "approver1"); err != nil {
			t.Fatalf("ApproveRefundRequest failed: %v", err)
		}
		if err := ProcessRefundRequest(tenantID, refundRequestID, "finance1", "Original Payment Method"); err != nil {
			t.Fatalf("ProcessRefundRequest failed: %v", err)
		}
		var finalReturnStatus, finalRefundStatus string
		_ = db.DB.QueryRow("SELECT status FROM "+schema+".documents WHERE id = $1", sellableID).Scan(&finalReturnStatus)
		_ = db.DB.QueryRow("SELECT status FROM "+schema+".documents WHERE id = $1", refundRequestID).Scan(&finalRefundStatus)
		if finalReturnStatus != "Closed" || finalRefundStatus != "Processed" {
			t.Errorf("Expected ReturnRequest Closed / RefundRequest Processed, got %q / %q", finalReturnStatus, finalRefundStatus)
		}

		// 2. Reject path: mandatory category-matched reason code; a rejected
		// request does not count against the returnable-quantity pool.
		rejectID, err := CreateReturnRequest(tenantID, "Customer Return", location, cartID, "", "tester", []ReturnItemInput{{SKU: sku, Qty: 1}})
		if err != nil {
			t.Fatalf("CreateReturnRequest (to be rejected) failed: %v", err)
		}
		returnIDs = append(returnIDs, rejectID)
		if err := RejectReturnRequest(tenantID, rejectID, "", "rejector1"); err == nil {
			t.Errorf("Expected RejectReturnRequest to reject a missing reason code")
		}
		if err := RejectReturnRequest(tenantID, rejectID, "RC-TEST-RETURN", "rejector1"); err != nil {
			t.Fatalf("RejectReturnRequest failed: %v", err)
		}

		// 3. Over-quantity (SALESR-0130): 6 sold, 3 already returned, 1
		// rejected (excluded) -> 3 remain returnable; requesting 4 must fail.
		if _, err := CreateReturnRequest(tenantID, "Customer Return", location, cartID, "", "tester", []ReturnItemInput{{SKU: sku, Qty: 4}}); err == nil {
			t.Errorf("Expected SALESR-0130 over-quantity rejection (only 3 of 6 remain returnable)")
		}

		// 4. Missing disposition: no stock received, no refund, and the
		// request closes immediately since nothing is refund-eligible.
		// Checked as an "open" (not Closed/Rejected) request just before QC,
		// for the Return Aging report below.
		missingID, err := CreateReturnRequest(tenantID, "Customer Return", location, cartID, "", "tester", []ReturnItemInput{{SKU: sku, Qty: 1}})
		if err != nil {
			t.Fatalf("CreateReturnRequest (Missing) failed: %v", err)
		}
		returnIDs = append(returnIDs, missingID)
		if err := ApproveReturnRequest(tenantID, missingID, "approver1"); err != nil {
			t.Fatalf("ApproveReturnRequest (Missing) failed: %v", err)
		}
		if err := ReceiveReturnRequest(tenantID, missingID, "warehouse1"); err != nil {
			t.Fatalf("ReceiveReturnRequest (Missing) failed: %v", err)
		}

		_, returnAgingRows, _, err := RunReport(tenantID, "return-aging", "HR/Admin", nil)
		if err != nil {
			t.Fatalf("RunReport return-aging failed: %v", err)
		}
		openCount := 0.0
		for _, row := range returnAgingRows {
			// RunReport round-trips every report through structsToRows'
			// JSON marshal/unmarshal, so numeric fields decode as float64,
			// never int - PayablesAgeingBucket's Count int becomes a JSON
			// number then an untyped float64 on the way back out.
			if c, ok := row["count"].(float64); ok {
				openCount += c
			}
		}
		if openCount < 1 {
			t.Errorf("Expected Return Aging to count at least 1 open request (Received, pre-QC), got total %v across buckets: %v", openCount, returnAgingRows)
		}

		missingRefund, missingRefundID, err := ApplyReturnQC(tenantID, missingID, map[string]string{sku: "Missing"}, "qc1")
		if err != nil {
			t.Fatalf("ApplyReturnQC (Missing) failed: %v", err)
		}
		if missingRefund != 0 || missingRefundID != "" {
			t.Errorf("Expected a Missing-disposition line to be refund-ineligible (0, no RefundRequest), got refund=%v id=%q", missingRefund, missingRefundID)
		}
		var closedAfterMissing string
		_ = db.DB.QueryRow("SELECT status FROM "+schema+".documents WHERE id = $1", missingID).Scan(&closedAfterMissing)
		if closedAfterMissing != "Closed" {
			t.Errorf("Expected a return request with nothing refund-eligible to close immediately, got %q", closedAfterMissing)
		}

		// 5. Damaged disposition: stock received into the 'damaged' bucket
		// (not 'available'), still refund-eligible in full.
		damagedID, err := CreateReturnRequest(tenantID, "Customer Return", location, cartID, "", "tester", []ReturnItemInput{{SKU: sku, Qty: 2}})
		if err != nil {
			t.Fatalf("CreateReturnRequest (Damaged) failed: %v", err)
		}
		returnIDs = append(returnIDs, damagedID)
		if err := ApproveReturnRequest(tenantID, damagedID, "approver1"); err != nil {
			t.Fatalf("ApproveReturnRequest (Damaged) failed: %v", err)
		}
		if err := ReceiveReturnRequest(tenantID, damagedID, "warehouse1"); err != nil {
			t.Fatalf("ReceiveReturnRequest (Damaged) failed: %v", err)
		}
		damagedRefund, damagedRefundID, err := ApplyReturnQC(tenantID, damagedID, map[string]string{sku: "Damaged"}, "qc1")
		if err != nil {
			t.Fatalf("ApplyReturnQC (Damaged) failed: %v", err)
		}
		if damagedRefund != 500 {
			t.Errorf("Expected refund total 500 (2 * original sale price 250) for a Damaged-but-refund-eligible line, got %v", damagedRefund)
		}
		refundIDs = append(refundIDs, damagedRefundID)
		atsAfterDamaged, err := GetAvailableToSell(tenantID, sku, location)
		if err != nil {
			t.Fatalf("Failed to read ATS after Damaged QC: %v", err)
		}
		if atsAfterDamaged["damaged"].(int) != 2 || atsAfterDamaged["available"].(int) != 3 || atsAfterDamaged["on_hand"].(int) != 5 {
			t.Errorf("Expected damaged=2, available unchanged at 3, on_hand=5 (3 Sellable + 2 Damaged, Missing contributed 0), got: %v", atsAfterDamaged)
		}

		// 6. Stock Mismatch report: a SKU/location where reserved exceeds
		// available (negative ATS) must be flagged.
		if _, err := db.DB.Exec("INSERT INTO "+schema+".inventory_availability (sku, location_code, on_hand, available, reserved) VALUES ($1, $2, 5, 5, 8)", mismatchSKU, location); err != nil {
			t.Fatalf("Failed to seed stock-mismatch fixture: %v", err)
		}
		_, mismatchRows, _, err := RunReport(tenantID, "stock-mismatch", "HR/Admin", nil)
		if err != nil {
			t.Fatalf("RunReport stock-mismatch failed: %v", err)
		}
		foundMismatch := false
		for _, row := range mismatchRows {
			if row["sku"] == mismatchSKU {
				foundMismatch = true
				if ats, ok := row["ats"].(int); !ok || ats >= 0 {
					t.Errorf("Expected a negative ats for the mismatch fixture, got: %v", row["ats"])
				}
			}
		}
		if !foundMismatch {
			t.Errorf("Expected stock-mismatch report to flag SKU %s", mismatchSKU)
		}

		// 7. RTO path: a real SalesOrder + LogisticsBooking, RecordRTO'd
		// (Stage 26.12.4), feeds a request_type='RTO' ReturnRequest priced
		// from the SalesOrderLine's own unit_price.
		itemData, _ := json.Marshal(map[string]interface{}{"code": rtoSKU, "name": "RTO Test Item", "barcode": rtoSKU})
		if _, err := db.DB.Exec("INSERT INTO "+schema+".documents (id, doctype, data, status, created_by) VALUES ($1, 'Item', $2, 'Active', 'system')", "ITEM-"+rtoSKU, itemData); err != nil {
			t.Fatalf("Failed to seed RTO test Item: %v", err)
		}
		if _, err := db.DB.Exec("INSERT INTO "+schema+".inventory_availability (sku, location_code, on_hand, available) VALUES ($1, $2, 50, 50)", rtoSKU, rtoLocation); err != nil {
			t.Fatalf("Failed to seed RTO test inventory: %v", err)
		}
		rtoOrderID, err := CreateSalesOrder(tenantID, "TestChannel", "CHORD-RTO-TEST-1", "RTO Customer", "RTO ADDR 99 MG Road 560099", "Confirmed", []SalesOrderLineInput{{SKU: rtoSKU, Qty: 2, UnitPrice: 300}})
		if err != nil {
			t.Fatalf("CreateSalesOrder (RTO fixture) failed: %v", err)
		}
		defer func() {
			_, _ = db.DB.Exec("DELETE FROM "+schema+".documents WHERE doctype = 'SalesOrderLine' AND data->>'order_id' = $1", rtoOrderID)
			_, _ = db.DB.Exec("DELETE FROM "+schema+".documents WHERE doctype = 'SalesOrder' AND id = $1", rtoOrderID)
		}()

		bookingID, err := CreateLogisticsBooking(tenantID, rtoOrderID, "", "TestCourier", "", "560099", 40)
		if err != nil {
			t.Fatalf("CreateLogisticsBooking (RTO fixture) failed: %v", err)
		}
		defer func() {
			_, _ = db.DB.Exec("DELETE FROM "+schema+".documents WHERE doctype = 'LogisticsBooking' AND id = $1", bookingID)
		}()

		if _, err := CreateReturnRequest(tenantID, "RTO", rtoLocation, "", bookingID, "system", []ReturnItemInput{{SKU: rtoSKU, Qty: 2}}); err == nil {
			t.Errorf("Expected an RTO return request to be rejected before the booking is actually marked RTO")
		}
		if err := RecordRTO(tenantID, bookingID, "Customer refused delivery", "system"); err != nil {
			t.Fatalf("RecordRTO failed: %v", err)
		}

		rtoReturnID, err := CreateReturnRequest(tenantID, "RTO", rtoLocation, "", bookingID, "system", []ReturnItemInput{{SKU: rtoSKU, Qty: 2}})
		if err != nil {
			t.Fatalf("CreateReturnRequest (RTO) failed: %v", err)
		}
		returnIDs = append(returnIDs, rtoReturnID)

		replayID, err := CreateReturnRequest(tenantID, "RTO", rtoLocation, "", bookingID, "system", []ReturnItemInput{{SKU: rtoSKU, Qty: 2}})
		if err != nil {
			t.Fatalf("CreateReturnRequest (RTO replay) failed: %v", err)
		}
		if replayID != rtoReturnID {
			t.Errorf("Expected an RTO return request replay for the same booking to be idempotent, got a new id %q vs original %q", replayID, rtoReturnID)
		}

		var rtoOriginalOrderID string
		var rtoOriginalPrice float64
		if err := db.DB.QueryRow(
			"SELECT data->>'original_order_id', (data->'items'->0->>'original_unit_price')::numeric FROM "+schema+".documents WHERE id = $1",
			rtoReturnID).Scan(&rtoOriginalOrderID, &rtoOriginalPrice); err != nil {
			t.Fatalf("Failed to read back RTO return request: %v", err)
		}
		if rtoOriginalOrderID != rtoOrderID {
			t.Errorf("Expected the RTO return request's original_order_id to auto-resolve from the booking to %q, got %q", rtoOrderID, rtoOriginalOrderID)
		}
		if rtoOriginalPrice != 300 {
			t.Errorf("Expected the RTO return line's original_unit_price to resolve from SalesOrderLine.unit_price (300), got %v", rtoOriginalPrice)
		}

		if err := ApproveReturnRequest(tenantID, rtoReturnID, "approver1"); err != nil {
			t.Fatalf("ApproveReturnRequest (RTO) failed: %v", err)
		}
		if err := ReceiveReturnRequest(tenantID, rtoReturnID, "warehouse1"); err != nil {
			t.Fatalf("ReceiveReturnRequest (RTO) failed: %v", err)
		}
		rtoRefund, rtoRefundID, err := ApplyReturnQC(tenantID, rtoReturnID, map[string]string{rtoSKU: "Sellable"}, "qc1")
		if err != nil {
			t.Fatalf("ApplyReturnQC (RTO) failed: %v", err)
		}
		if rtoRefund != 600 {
			t.Errorf("Expected RTO refund total 600 (2 * SalesOrderLine unit_price 300), got %v", rtoRefund)
		}
		refundIDs = append(refundIDs, rtoRefundID)
		if err := ApproveRefundRequest(tenantID, rtoRefundID, "approver1"); err != nil {
			t.Fatalf("ApproveRefundRequest (RTO) failed: %v", err)
		}
		if err := ProcessRefundRequest(tenantID, rtoRefundID, "finance1", "Original Payment Method"); err != nil {
			t.Fatalf("ProcessRefundRequest (RTO) failed: %v", err)
		}

		// 8. Allocation Pending report - directly seeded, since driving a
		// real Manual AllocationRule end-to-end is exercised by the
		// AllocationSourcing subtest already; this just proves the report
		// query itself.
		allocData, _ := json.Marshal(map[string]interface{}{
			"code": "SO-ALLOC-TEST-1", "customer_name": "Alloc Test Customer", "channel": "TestChannel",
			"order_status": "On Hold", "hold_reason": HoldAllocationFailed, "total_amount": 999.0,
		})
		if _, err := db.DB.Exec("INSERT INTO "+schema+".documents (id, doctype, data, status, created_by) VALUES ($1, 'SalesOrder', $2, 'On Hold', 'system')", "SO-ALLOC-TEST-1", allocData); err != nil {
			t.Fatalf("Failed to seed Allocation Pending fixture: %v", err)
		}
		_, allocRows, _, err := RunReport(tenantID, "allocation-pending", "HR/Admin", nil)
		if err != nil {
			t.Fatalf("RunReport allocation-pending failed: %v", err)
		}
		foundAlloc := false
		for _, row := range allocRows {
			if row["order_id"] == "SO-ALLOC-TEST-1" {
				foundAlloc = true
			}
		}
		if !foundAlloc {
			t.Errorf("Expected allocation-pending report to include the seeded On-Hold/ALLOCATION_FAILED order")
		}

		// 9. Order Aging - the RTO SalesOrder is still Reserved (open,
		// counts) regardless of the reconciliation-variance edit below.
		_, orderAgingRows, _, err := RunReport(tenantID, "order-aging", "HR/Admin", nil)
		if err != nil {
			t.Fatalf("RunReport order-aging failed: %v", err)
		}
		orderAgingTotal := 0.0
		for _, row := range orderAgingRows {
			if c, ok := row["count"].(float64); ok {
				orderAgingTotal += c
			}
		}
		if orderAgingTotal < 1 {
			t.Errorf("Expected Order Aging to count at least 1 open order, got total %v: %v", orderAgingTotal, orderAgingRows)
		}

		// 10. Reconciliation Variance: force the RTO order to 'Shipped' -
		// its only booking is RTO, not Handed Over/Delivered, so this must
		// surface as a variance.
		if _, err := db.DB.Exec("UPDATE "+schema+".documents SET status = 'Shipped', data = jsonb_set(data, '{order_status}', '\"Shipped\"') WHERE id = $1", rtoOrderID); err != nil {
			t.Fatalf("Failed to force RTO order to Shipped: %v", err)
		}
		_, varianceRows, _, err := RunReport(tenantID, "oms-reconciliation-variance", "HR/Admin", nil)
		if err != nil {
			t.Fatalf("RunReport oms-reconciliation-variance failed: %v", err)
		}
		foundVariance := false
		for _, row := range varianceRows {
			if row["order_id"] == rtoOrderID {
				foundVariance = true
			}
		}
		if !foundVariance {
			t.Errorf("Expected oms-reconciliation-variance to flag order %s (Shipped with only an RTO booking)", rtoOrderID)
		}

		// 11. Reserved Stock report - the RTO order's reservation was never
		// released (no fulfillment/dispatch step ran against it in this
		// test), so it should still show up.
		_, reservedRows, _, err := RunReport(tenantID, "reserved-stock", "HR/Admin", nil)
		if err != nil {
			t.Fatalf("RunReport reserved-stock failed: %v", err)
		}
		foundReserved := false
		for _, row := range reservedRows {
			if row["sku"] == rtoSKU && row["location_code"] == rtoLocation {
				foundReserved = true
			}
		}
		if !foundReserved {
			t.Errorf("Expected reserved-stock report to include %s at %s", rtoSKU, rtoLocation)
		}

		// 12. Courier Performance report.
		_, courierRows, _, err := RunReport(tenantID, "courier-performance", "HR/Admin", nil)
		if err != nil {
			t.Fatalf("RunReport courier-performance failed: %v", err)
		}
		foundCourier := false
		for _, row := range courierRows {
			if row["carrier"] == "TestCourier" {
				foundCourier = true
				if row["rto"].(int) < 1 {
					t.Errorf("Expected TestCourier's rto count to be at least 1, got: %v", row["rto"])
				}
			}
		}
		if !foundCourier {
			t.Errorf("Expected courier-performance report to include carrier TestCourier")
		}

		// 13. SLA Breach report - a backdated Pending FulfillmentTask.
		slaTaskID, err := CreateFulfillmentTasks(tenantID, "ORD-SLA-TEST", rtoLocation, []interface{}{
			map[string]interface{}{"sku": rtoSKU, "qty": 1},
		})
		if err != nil {
			t.Fatalf("CreateFulfillmentTasks (SLA fixture) failed: %v", err)
		}
		defer func() {
			_, _ = db.DB.Exec("DELETE FROM "+schema+".documents WHERE doctype = 'FulfillmentTask' AND id = $1", slaTaskID)
		}()
		// Backdated by 25 hours, not just past the 60-minute threshold - this
		// dev DB's Postgres session clock reads several hours off from Go's
		// time.Now() (a pre-existing environment quirk in how naive
		// `timestamp without time zone` columns round-trip through lib/pq,
		// not something this change introduces), so a small backdate can
		// get swallowed by that skew; 25 hours safely dominates it.
		if _, err := db.DB.Exec("UPDATE "+schema+".documents SET created_at = CURRENT_TIMESTAMP - INTERVAL '25 hours' WHERE id = $1", slaTaskID); err != nil {
			t.Fatalf("Failed to backdate SLA fixture task: %v", err)
		}
		_, slaRows, _, err := RunReport(tenantID, "sla-breach", "HR/Admin", map[string]string{"threshold_minutes": "60"})
		if err != nil {
			t.Fatalf("RunReport sla-breach failed: %v", err)
		}
		foundSLA := false
		for _, row := range slaRows {
			if row["task_id"] == slaTaskID {
				foundSLA = true
			}
		}
		if !foundSLA {
			t.Errorf("Expected sla-breach report to flag the backdated task %s; got rows: %#v", slaTaskID, slaRows)
		}

		// 14. OMS Exception Queue report - a Failed outbox event.
		var exceptionEventID string
		payload, _ := json.Marshal(map[string]interface{}{"test": true})
		if err := db.DB.QueryRow(
			"INSERT INTO "+schema+".integration_event_outbox (event_name, payload, status, attempts) VALUES ($1, $2, 'Failed', 5) RETURNING id",
			"oms.test.exception", payload).Scan(&exceptionEventID); err != nil {
			t.Fatalf("Failed to seed exception-queue fixture: %v", err)
		}
		_, exceptionRows, _, err := RunReport(tenantID, "oms-exception-queue", "HR/Admin", nil)
		if err != nil {
			t.Fatalf("RunReport oms-exception-queue failed: %v", err)
		}
		foundException := false
		for _, row := range exceptionRows {
			if row["event_id"] == exceptionEventID {
				foundException = true
			}
		}
		if !foundException {
			t.Errorf("Expected oms-exception-queue report to include the seeded Failed event %s", exceptionEventID)
		}

		// 15. Notification dispatch: Skipped-NoConfig path (an Active
		// template exists for the event, but no NotificationChannelConfig
		// is configured for its channel).
		tmplData, _ := json.Marshal(map[string]interface{}{
			"code": "NT-TEST-1", "event": "Order Cancelled", "channel": "Email",
			"subject": "", "body_template": "Order {{order_id}} was cancelled", "status": "Active",
		})
		if _, err := db.DB.Exec("INSERT INTO "+schema+".documents (id, doctype, data, status, created_by) VALUES ($1, 'NotificationTemplate', $2, 'Active', 'system')", "NT-TEST-1", tmplData); err != nil {
			t.Fatalf("Failed to seed NotificationTemplate: %v", err)
		}
		DispatchNotification(tenantID, "Order Cancelled", "SO-NOTIFY-TEST-1", nil)
		var noConfigStatus string
		if err := db.DB.QueryRow("SELECT data->>'dispatch_status' FROM "+schema+".documents WHERE doctype = 'NotificationLog' AND data->>'template_id' = 'NT-TEST-1' ORDER BY created_at DESC LIMIT 1").Scan(&noConfigStatus); err != nil {
			t.Fatalf("Expected a NotificationLog row for the Skipped-NoConfig path: %v", err)
		}
		if noConfigStatus != "Skipped-NoConfig" {
			t.Errorf("Expected dispatch_status Skipped-NoConfig (template exists, channel unconfigured), got %q", noConfigStatus)
		}
	})
}
