package engines

import (
	"custom_erp/db"
	"encoding/json"
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
		err := PostInventoryLedger(tenantID, location, items)
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
	})

	// 5. Test Double-Entry journal booking and POS cart checkouts
	t.Run("FinanceDoubleEntryAndPOS", func(t *testing.T) {
		// Clean postings for test consistency
		_, _ = db.DB.Exec("DELETE FROM " + schema + ".gl_postings")

		// 1. Post balanced double-entry
		debits := map[string]int{"1100": 1000}
		credits := map[string]int{"4100": 1000}
		err := PostDoubleEntry(tenantID, "TestVoucher", "V-001", debits, credits)
		if err != nil {
			t.Fatalf("Failed to post balanced journal entry: %v", err)
		}

		// 2. Expect failure on unbalanced entries
		badDebits := map[string]int{"1100": 1000}
		badCredits := map[string]int{"4100": 800}
		err = PostDoubleEntry(tenantID, "TestVoucher", "V-002", badDebits, badCredits)
		if err == nil {
			t.Errorf("Expected error when posting unbalanced journal entries, but got none")
		}

		// 3. Test trial balance retrieval
		tb, err := GetTrialBalance(tenantID)
		if err != nil {
			t.Fatalf("Failed to fetch trial balance: %v", err)
		}

		if tb["balanced"].(bool) == false || tb["total_debits"].(int) != 1000 || tb["total_credits"].(int) != 1000 {
			t.Errorf("Trial balance mismatch: %+v", tb)
		}

		// 4. Test automated sales booking
		err = PostSalesFinanceBooking(tenantID, "CRT-TEST-99", 5000, 3000)
		if err != nil {
			t.Fatalf("Failed to post automated sales bookings: %v", err)
		}

		tbPost, _ := GetTrialBalance(tenantID)
		if tbPost["total_debits"].(int) != 9000 || tbPost["total_credits"].(int) != 9000 {
			t.Errorf("Expected total trial balance debits/credits of 9000 (1000 test + 5000 sale + 3000 COGS), got: %+v", tbPost)
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

		// 3. Test Return Anywhere: Return items originally from WH02 to WH01
		returnItems := []interface{}{
			map[string]interface{}{
				"sku":        "BAR12345",
				"qty":        5,
				"sale_price": 5000.0,
				"cost_price": 3000.0,
			},
		}

		err = ProcessReturnAnywhere(tenantID, "WH01", "ORD-WEB-111", returnItems)
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
		bookingID, err := CreateLogisticsBooking(tenantID, "ORD-WEB-111", "FedEx", "TRK123456", 250)
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
		adminPassword, err := ProvisionTenantSchema(newTenant, newSchema)
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
		err := db.DB.QueryRow("INSERT INTO "+schema+".integration_event_outbox (event_name, payload, status, attempts) VALUES ('test.event', '{}', 'Failed', 3) RETURNING id").Scan(&eventID)
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
}
