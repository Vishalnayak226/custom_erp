package engines

import (
	"custom_erp/db"
	"encoding/json"
	"strings"
	"sync"
	"testing"
)

// BLD-031 - providers, public API and extensions: local failure campaigns for
// bad signatures, timeouts, uncertain/ambiguous results, retried/duplicate/
// out-of-order events, old client versions and hostile extension capability
// requests (docs/product/erp-build-checklist.md). This file covers the
// engines-package half: a genuine channel-webhook duplicate-delivery race
// (found and fixed here), an extension-hook SSRF gap (found and fixed here),
// and Pine Labs' previously entirely untested payment-reconciliation contract
// (uncertain/ambiguous provider results). The inbound-webhook-signature half
// lives in internal/server (verifyShopifyWebhookSignature has its own
// coverage there - it needs the real HTTP handler, not just the engine).
//
// Reused rather than rebuilt: Stage 38.4's outbound-webhook SSRF/timeout/
// idempotent-redispatch coverage (webhook_test.go), the extension-hook
// signature/timeout/non-2xx coverage (extensions_test.go), the connector
// circuit breaker/rate limiter (connector_test.go), and the public API
// idempotency/quota/scope primitives (public_api_runtime_test.go,
// public_api_credentials_test.go) - none of that is duplicated here.
//
// "Old client versions" is deliberately not covered by a new test here: this
// codebase has never shipped a second public API version, and BLD-020
// already proved the honest scope - a clean 404 for an unsupported version,
// not a fabricated compatibility scenario - so there is no additional
// versioned-contract surface for BLD-031 to add. Stated, not hidden.

// --- Retried/duplicate/out-of-order events: the channel-order webhook race ---

// TestBLD031ConcurrentDuplicateChannelWebhookDeliveryIsIdempotent proves the
// property this session's channel_orders.go fix (acquireChannelOrderLock)
// exists for: a real e-commerce/payment channel redelivers webhooks at least
// once (a timeout on the first response looks identical to a lost request
// from the channel's side), so the identical channel_order_id arriving
// concurrently is the normal case, not an edge case. Before the fix, the
// mapping lookup and CreateSalesOrder's own duplicate check were both plain
// SELECTs with no lock between them, so N concurrent deliveries could each
// observe "not found" and each create a real SalesOrder - doubling reserved
// stock and revenue for what was, on the channel's side, one single order.
//
// Verified by a guarded revert-test-restore cycle against the live fix
// (see project_ledger.md/BLD-031 entry): with acquireChannelOrderLock
// removed, this test reliably produced 2+ distinct SalesOrder documents for
// one channel_order_id across repeated runs; restored, it is green every
// time. It is not a probabilistic flake-prone test by design - `concurrency`
// goroutines are all released from a single closed channel so they issue
// their first DB statement within microseconds of each other, and the
// unprotected code path's race window spans many sequential round trips
// (SKU-mapping lookups, validation, allocation, the document insert).
func TestBLD031ConcurrentDuplicateChannelWebhookDeliveryIsIdempotent(t *testing.T) {
	db.InitDB(testConnStr())
	const tenantID = "default"
	const channel = "BLD031ConcurrentChannel"
	sku := NewDocID("BLD031SKU")
	channelOrderID := NewDocID("BLD031CHORD")
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		t.Fatal(err)
	}

	cleanup := func() {
		db.DB.Exec("DELETE FROM "+schema+".channel_order_mapping WHERE channel = $1 AND channel_order_id = $2", channel, channelOrderID)
		db.DB.Exec("DELETE FROM "+schema+".documents WHERE doctype = 'SalesOrderLine' AND data->>'order_id' IN (SELECT id FROM "+schema+".documents WHERE doctype = 'SalesOrder' AND data->>'channel_order_id' = $1)", channelOrderID)
		db.DB.Exec("DELETE FROM "+schema+".documents WHERE doctype = 'SalesOrder' AND data->>'channel_order_id' = $1", channelOrderID)
		db.DB.Exec("DELETE FROM "+schema+".inventory_availability WHERE sku = $1", sku)
		db.DB.Exec("DELETE FROM "+schema+".documents WHERE id = $1", "ITEM-"+sku)
	}
	cleanup()
	t.Cleanup(cleanup)

	item, _ := json.Marshal(map[string]interface{}{"code": sku, "name": "BLD-031 concurrency test item"})
	if _, err := db.DB.Exec("INSERT INTO "+schema+".documents (id, doctype, data, status, created_by) VALUES ($1, 'Item', $2, 'Active', 'system')", "ITEM-"+sku, item); err != nil {
		t.Fatalf("seed item: %v", err)
	}
	if _, err := db.DB.Exec("INSERT INTO "+schema+".inventory_availability (sku, location_code, on_hand, available) VALUES ($1, 'HO', 1000, 1000)", sku); err != nil {
		t.Fatalf("seed inventory: %v", err)
	}

	input := ChannelOrderInput{
		Channel:         channel,
		ChannelOrderID:  channelOrderID,
		CustomerName:    "BLD-031 Duplicate Delivery Customer",
		ShippingAddress: "1 Duplicate Delivery Road, Bengaluru 560001",
		PaymentStatus:   "Confirmed",
		Lines:           []SalesOrderLineInput{{SKU: sku, Qty: 2, UnitPrice: 55}},
	}

	const concurrency = 20
	var wg sync.WaitGroup
	orderIDs := make([]string, concurrency)
	errs := make([]error, concurrency)
	start := make(chan struct{})
	for i := 0; i < concurrency; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			<-start
			orderIDs[i], errs[i] = ImportChannelSalesOrder(tenantID, input)
		}(i)
	}
	close(start)
	wg.Wait()

	for i, callErr := range errs {
		if callErr != nil {
			t.Fatalf("goroutine %d: ImportChannelSalesOrder errored under concurrent duplicate delivery (want every retry to succeed): %v", i, callErr)
		}
	}
	first := orderIDs[0]
	for i, id := range orderIDs {
		if id != first {
			t.Fatalf("counterexample: concurrent duplicate delivery of the identical channel_order_id returned different order ids (goroutine 0 = %q, goroutine %d = %q) - two SalesOrder documents for one real order, doubling its economics", first, i, id)
		}
	}

	var count int
	if err := db.DB.QueryRow("SELECT count(*) FROM "+schema+".documents WHERE doctype = 'SalesOrder' AND data->>'channel_order_id' = $1", channelOrderID).Scan(&count); err != nil {
		t.Fatalf("count SalesOrder documents: %v", err)
	}
	if count != 1 {
		t.Fatalf("counterexample: %d SalesOrder documents exist for one channel_order_id after %d concurrent duplicate deliveries, want exactly 1", count, concurrency)
	}

	var mappingCount int
	if err := db.DB.QueryRow("SELECT count(*) FROM "+schema+".channel_order_mapping WHERE channel = $1 AND channel_order_id = $2", channel, channelOrderID).Scan(&mappingCount); err != nil {
		t.Fatalf("count channel_order_mapping rows: %v", err)
	}
	if mappingCount != 1 {
		t.Fatalf("channel_order_mapping rows = %d, want exactly 1", mappingCount)
	}
}

// --- Hostile extension capability requests: the extension-hook SSRF gap ---

// TestBLD031ExtensionHookTargetURLRejectsPrivateResolvedHTTPS proves the
// registration-time half of the fix: validateHookTargetURL's https branch
// used to return nil unconditionally with no resolution check at all (unlike
// webhook.go's validateWebhookURL for Stage 38.4's outbound webhooks, which
// has always done this). A hostile or compromised extension could register
// an ordinary-looking https hostname that resolves to an internal service or
// the cloud metadata endpoint. These cases use literal IP addresses as the
// hostname - Go's resolver returns a literal IP without any real DNS query,
// so this is fully deterministic and needs no network access.
func TestBLD031ExtensionHookTargetURLRejectsPrivateResolvedHTTPS(t *testing.T) {
	cases := []struct {
		url     string
		wantErr bool
		note    string
	}{
		{"https://127.0.0.1/hook", true, "loopback"},
		{"https://10.0.0.5/hook", true, "private range"},
		{"https://169.254.169.254/hook", true, "cloud metadata / link-local"},
		{"https://[::1]/hook", true, "IPv6 loopback"},
		{"https://8.8.8.8/hook", false, "genuine public address"},
	}
	for _, c := range cases {
		err := validateHookTargetURL(c.url)
		if c.wantErr && err == nil {
			t.Errorf("validateHookTargetURL(%q) [%s]: expected an error, got nil", c.url, c.note)
		}
		if !c.wantErr && err != nil {
			t.Errorf("validateHookTargetURL(%q) [%s]: expected no error, got %v", c.url, c.note, err)
		}
	}
}

// TestBLD031ExtensionHookDeliveryRevalidatesTargetBeforeEveryCall proves the
// delivery-time half: a hook row planted directly (bypassing
// RegisterExtensionHook's own check, exactly as a stale registration from
// before this fix - or DNS changing after a legitimate registration - would
// look) must still be refused at the moment of delivery, not merely at
// creation. Before this fix there was no re-check at all for extension
// hooks, so this would have dialed the private address.
func TestBLD031ExtensionHookDeliveryRevalidatesTargetBeforeEveryCall(t *testing.T) {
	db.InitDB(testConnStr())
	tenantID := "default"
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		t.Fatalf("schema: %v", err)
	}
	const doctype = "TEST_BLD031_EXT_SSRF"
	cleanupExtensionHooks(schema, doctype)
	defer cleanupExtensionHooks(schema, doctype)

	// Planted directly - RegisterExtensionHook would refuse this URL outright,
	// which is exactly why the re-check at delivery time is the real guarantee.
	seedExtensionHook(t, schema, "document.before_save", doctype, "https://169.254.169.254/hook", "secret", 3000)

	err = InvokeBeforeSaveHooks(tenantID, doctype, "DOC-BLD031-SSRF", map[string]interface{}{})
	if err == nil {
		t.Fatal("expected a hook whose target resolves to a private/link-local address to be refused at delivery time, got no error")
	}
	if !strings.Contains(err.Error(), "non-public address") {
		t.Fatalf("expected the SSRF refusal reason in the error, got: %v", err)
	}
}

// --- Uncertain/ambiguous results: Pine Labs payment-terminal reconciliation ---
//
// This provider integration (engines/pinelabs.go) had zero test coverage
// before this file - a real gap for the one payment-terminal integration in
// this codebase, since "the terminal says a payment happened but the ERP's
// own cart never reaches Paid" is the textbook ambiguous-payment-result
// scenario BLD-031 asks for a local failure campaign against.

func bld031SeedPineLabsTerminal(t *testing.T, tenantID, terminalID string) {
	t.Helper()
	if err := SavePineLabsCredential(tenantID, terminalID, "test-api-key", "TEST-MERCHANT", "https://pinelabs.test.invalid"); err != nil {
		t.Fatalf("seed pinelabs credential: %v", err)
	}
}

func bld031CleanupPineLabs(schema, terminalID string, transactionIDs ...string) {
	for _, id := range transactionIDs {
		db.DB.Exec("DELETE FROM "+schema+".pinelabs_transactions WHERE transaction_id = $1", id)
	}
	db.DB.Exec("DELETE FROM "+schema+".pinelabs_credentials WHERE terminal_id = $1", terminalID)
}

// TestBLD031PineLabsRejectsUnmappedTerminal is the "bad/unknown credential"
// case: a terminal_id nobody configured must be refused (POSOFF-0243), not
// silently recorded as a real payment against an unmapped device.
func TestBLD031PineLabsRejectsUnmappedTerminal(t *testing.T) {
	db.InitDB(testConnStr())
	tenantID := "default"
	terminalID := NewDocID("BLD031UNMAPPEDTERM")
	txnID := NewDocID("BLD031UNMAPPEDTXN")
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		t.Fatal(err)
	}
	defer bld031CleanupPineLabs(schema, terminalID, txnID)

	err = RecordPineLabsTransaction(tenantID, txnID, terminalID, "SOME-CART", 500, "Card")
	if err == nil {
		t.Fatal("expected an unmapped terminal_id to be refused")
	}
	verr, ok := err.(*ValidationError)
	if !ok || verr.Code != "POSOFF-0243" {
		t.Fatalf("expected a POSOFF-0243 ValidationError, got %v (%T)", err, err)
	}
}

// TestBLD031PineLabsRejectsDuplicateTransactionID is the straightforward
// retry case: the same terminal transaction_id arriving twice sequentially
// (a cashier re-entering a response code, or a retried callback) must not be
// recorded twice.
func TestBLD031PineLabsRejectsDuplicateTransactionID(t *testing.T) {
	db.InitDB(testConnStr())
	tenantID := "default"
	terminalID := NewDocID("BLD031DUPTERM")
	txnID := NewDocID("BLD031DUPTXN")
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		t.Fatal(err)
	}
	bld031SeedPineLabsTerminal(t, tenantID, terminalID)
	defer bld031CleanupPineLabs(schema, terminalID, txnID)

	if err := RecordPineLabsTransaction(tenantID, txnID, terminalID, "CART-A", 250, "Card"); err != nil {
		t.Fatalf("first record: %v", err)
	}
	err = RecordPineLabsTransaction(tenantID, txnID, terminalID, "CART-A", 250, "Card")
	if err == nil || !strings.Contains(err.Error(), "TRANSACTION_ALREADY_RECORDED") {
		t.Fatalf("expected TRANSACTION_ALREADY_RECORDED on replay, got %v", err)
	}
	var count int
	if err := db.DB.QueryRow("SELECT count(*) FROM "+schema+".pinelabs_transactions WHERE transaction_id = $1", txnID).Scan(&count); err != nil {
		t.Fatalf("count: %v", err)
	}
	if count != 1 {
		t.Fatalf("expected exactly 1 stored transaction after a sequential replay, got %d", count)
	}
}

// TestBLD031PineLabsConcurrentDuplicateTransactionIDNeverDoubleRecords is the
// genuinely concurrent version of the same replay: RecordPineLabsTransaction
// does an existence-check-then-insert with no application-level lock, which
// is a TOCTOU race in principle. This proves the DB's own UNIQUE constraint
// on transaction_id (db/migration.sql section 35) is the real backstop that
// makes it safe anyway - exactly one caller may ever succeed, whichever
// order they interleave in, and the table never ends up with two rows for
// one terminal transaction.
func TestBLD031PineLabsConcurrentDuplicateTransactionIDNeverDoubleRecords(t *testing.T) {
	db.InitDB(testConnStr())
	tenantID := "default"
	terminalID := NewDocID("BLD031RACETERM")
	txnID := NewDocID("BLD031RACETXN")
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		t.Fatal(err)
	}
	bld031SeedPineLabsTerminal(t, tenantID, terminalID)
	defer bld031CleanupPineLabs(schema, terminalID, txnID)

	const concurrency = 10
	var wg sync.WaitGroup
	errs := make([]error, concurrency)
	start := make(chan struct{})
	for i := 0; i < concurrency; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			<-start
			errs[i] = RecordPineLabsTransaction(tenantID, txnID, terminalID, "CART-RACE", 999, "Card")
		}(i)
	}
	close(start)
	wg.Wait()

	successes := 0
	for _, callErr := range errs {
		if callErr == nil {
			successes++
		}
	}
	if successes != 1 {
		t.Fatalf("counterexample: %d of %d concurrent identical transaction_id recordings succeeded, want exactly 1", successes, concurrency)
	}
	var count int
	if err := db.DB.QueryRow("SELECT count(*) FROM "+schema+".pinelabs_transactions WHERE transaction_id = $1", txnID).Scan(&count); err != nil {
		t.Fatalf("count: %v", err)
	}
	if count != 1 {
		t.Fatalf("counterexample: %d rows stored for one transaction_id after %d concurrent duplicate recordings, want exactly 1", count, concurrency)
	}
}

// TestBLD031PineLabsReconciliationReportsAmbiguousResultsHonestly is the core
// "uncertain/ambiguous result" campaign: a terminal transaction whose
// corresponding POSCart is missing, or exists but never reached Paid, is an
// ambiguous outcome (did the sale actually complete?) that must be reported
// as unreconciled with a named reason - never silently marked reconciled,
// and never fabricated as a completed sale.
func TestBLD031PineLabsReconciliationReportsAmbiguousResultsHonestly(t *testing.T) {
	db.InitDB(testConnStr())
	tenantID := "default"
	terminalID := NewDocID("BLD031RECONTERM")
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		t.Fatal(err)
	}
	bld031SeedPineLabsTerminal(t, tenantID, terminalID)

	missingCartTxn := NewDocID("BLD031RECONMISSING")
	pendingCartTxn := NewDocID("BLD031RECONPENDING")
	paidCartTxn := NewDocID("BLD031RECONPAID")
	pendingCart := NewDocID("BLD031RECONCART-PENDING")
	paidCart := NewDocID("BLD031RECONCART-PAID")

	defer func() {
		bld031CleanupPineLabs(schema, terminalID, missingCartTxn, pendingCartTxn, paidCartTxn)
		db.DB.Exec("DELETE FROM "+schema+".documents WHERE id IN ($1, $2) AND doctype = 'POSCart'", pendingCart, paidCart)
	}()

	seedCart := func(id, status string) {
		data, _ := json.Marshal(map[string]interface{}{"cart_number": id})
		if _, err := db.DB.Exec("INSERT INTO "+schema+".documents (id, doctype, data, status, created_by) VALUES ($1, 'POSCart', $2, $3, 'system')", id, data, status); err != nil {
			t.Fatalf("seed POSCart %s: %v", id, err)
		}
	}
	seedCart(pendingCart, "Pending")
	seedCart(paidCart, "Paid")

	// A terminal transaction referencing a cart that was never created at all
	// - the classic "did the terminal receipt even correspond to a real sale"
	// ambiguity.
	if err := RecordPineLabsTransaction(tenantID, missingCartTxn, terminalID, "BLD031-CART-NEVER-EXISTED", 100, "Card"); err != nil {
		t.Fatalf("record missing-cart txn: %v", err)
	}
	// A terminal transaction referencing a cart that exists but never reached
	// Paid - the payment may have been taken while the ERP-side checkout
	// stalled or failed.
	if err := RecordPineLabsTransaction(tenantID, pendingCartTxn, terminalID, pendingCart, 200, "Card"); err != nil {
		t.Fatalf("record pending-cart txn: %v", err)
	}
	// The unambiguous, genuinely-completed case.
	if err := RecordPineLabsTransaction(tenantID, paidCartTxn, terminalID, paidCart, 300, "Card"); err != nil {
		t.Fatalf("record paid-cart txn: %v", err)
	}

	result, err := ReconcilePineLabsTransactions(tenantID)
	if err != nil {
		t.Fatalf("ReconcilePineLabsTransactions: %v", err)
	}
	reconciled, _ := result["reconciled"].(int)
	failed, _ := result["failed"].(int)
	if reconciled != 1 {
		t.Fatalf("counterexample: reconciled = %d, want exactly 1 (only the genuinely Paid cart)", reconciled)
	}
	if failed != 2 {
		t.Fatalf("counterexample: failed = %d, want exactly 2 (missing cart + non-Paid cart), got result=%#v", failed, result)
	}
	errMessages, _ := result["errors"].([]string)
	joined := strings.Join(errMessages, " | ")
	if !strings.Contains(joined, missingCartTxn) {
		t.Fatalf("expected the missing-cart transaction to be named in the failure list: %v", errMessages)
	}
	if !strings.Contains(joined, pendingCartTxn) {
		t.Fatalf("expected the non-Paid-cart transaction to be named in the failure list: %v", errMessages)
	}

	txns, err := ListPineLabsTransactions(tenantID)
	if err != nil {
		t.Fatalf("ListPineLabsTransactions: %v", err)
	}
	reconciledFlag := map[string]bool{}
	for _, txn := range txns {
		id, _ := txn["transaction_id"].(string)
		flag, _ := txn["reconciled"].(bool)
		reconciledFlag[id] = flag
	}
	if reconciledFlag[missingCartTxn] {
		t.Fatal("counterexample: the missing-cart transaction was marked reconciled")
	}
	if reconciledFlag[pendingCartTxn] {
		t.Fatal("counterexample: the non-Paid-cart transaction was marked reconciled")
	}
	if !reconciledFlag[paidCartTxn] {
		t.Fatal("the genuinely Paid-cart transaction was not marked reconciled")
	}

	// A second reconciliation pass must not re-report or double-count the two
	// still-unreconciled rows as new failures - they stay pending, honestly,
	// until whatever real-world resolution actually happens (a manual match,
	// a cart status changing, or an operator write-off), not because the
	// reconciler pretends they succeeded.
	result2, err := ReconcilePineLabsTransactions(tenantID)
	if err != nil {
		t.Fatalf("second ReconcilePineLabsTransactions: %v", err)
	}
	failed2, _ := result2["failed"].(int)
	if failed2 != 2 {
		t.Fatalf("second pass failed = %d, want the same 2 still-unreconciled rows reported again (not silently dropped or fabricated as success)", failed2)
	}
}

