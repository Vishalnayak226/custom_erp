package server

import (
	"context"
	_ "embed"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"custom_erp/db"
	"custom_erp/engines"
)

// Run is the server's real entrypoint (called from cmd/server/main.go) - DB
// init, background worker startup, every route registration, and
// http.ListenAndServe. The route table just wires a path to a handler
// defined in one of this package's other handlers_*.go files.

func Run() {
	// Initialize database connection
	db.InitDB(db.ConnStringFromEnv())

	// Stage 30.2.2: say so when the binary carries migrations this database
	// has never had applied. Deliberately a warning and not an automatic
	// apply - schema changes against a live database are an operator
	// decision, and two instances starting at once must not both try. Run
	// `erp-server -migrate` to close the gap.
	if pending, err := db.PendingMigrations(); err == nil && len(pending) > 0 {
		log.Printf("WARNING: %d database migration(s) have not been applied to this database (oldest: %s). Run `erp-server -migrate` to apply them.", len(pending), pending[0])
	}

	// 20.6: warn (or, in production, refuse to start) if the connected
	// database isn't UTF8-encoded - found via messy-data stress testing,
	// see db.EnforceUTF8Encoding's own comment for why this matters.
	if err := db.EnforceUTF8Encoding(); err != nil {
		log.Fatalf("Refusing to start: %v", err)
	}

	// 24.27: refuse to start in production if the seed admin credential
	// from db/migration.sql is still active - a fresh dev/test install is
	// unaffected (this only fires the hard-stop when ENV=production).
	if err := engines.EnforceNoDefaultAdminCredentialInProduction(); err != nil {
		log.Fatalf("Refusing to start: %v", err)
	}

	// 24.15: one cancellable context threaded into every background worker,
	// canceled from the SIGINT/SIGTERM handler at the bottom of this
	// function so a process stop tells them to finish their current tick
	// and exit instead of just being killed mid-flight.
	workerCtx, cancelWorkers := context.WithCancel(context.Background())

	// Start Outbox background poller (Scale and Omnichannel integration queue)
	engines.StartOutboxWorker(workerCtx, 5*time.Second)

	// Start PIM Publish Queue background poller (Stage 15.2 - stub connector,
	// see engines/pim_publish.go's file header for the real-connector caveat)
	engines.StartPublishQueueWorker(workerCtx, 10*time.Second)
	// Stage 35.6: order pull + ATS inventory sync for every Active Channel.
	// One worker owns both directions so a connector cannot race two cursors.
	engines.StartChannelSyncWorker(workerCtx, 5*time.Minute)

	// Start Magento Open Source order-change poller (Stage 16.4) - the
	// substitute for native webhooks Magento Open Source does not have.
	// Adobe Commerce Cloud channels use real webhooks instead and are
	// skipped by this poller (see engines/connector_magento.go).
	engines.StartMagentoPollWorker(workerCtx, 60*time.Second)

	// Start Patch/Bug-Intake background worker (Stage 14.13-14.16). Never
	// mutates tenant/business state - see engines/patchintake.go's file
	// header for why that's true by construction, not just by convention.
	engines.StartPatchIntakeWorker(workerCtx, 24*time.Hour)

	// Start Ops Alert Monitor (Stage 17.10) - polls every minute, alerts if
	// a tenant schema logs 20+ system_error_logs rows within a 5-minute
	// window (a single PANIC alerts immediately instead, from inside
	// engines.LogSystemError). No-ops until OPS_ALERT_WEBHOOK_URL is set -
	// see engines/alerting.go.
	engines.StartAlertMonitor(workerCtx, 1*time.Minute, 5*time.Minute, 20)

	// Start Backup Freshness Monitor (Stage 43.2) - hourly check that a
	// nightly backup actually exists and is under 36h old. 36 rather than 24
	// so a single late or slow run does not page anyone; two consecutive
	// missed nights always does, which is the signal hypercare_plan.md's
	// rollback trigger is written against. Disables itself where there is no
	// backup directory (every dev box), and no-ops in delivery until
	// OPS_ALERT_WEBHOOK_URL is set - see engines/alerting.go.
	engines.StartBackupFreshnessMonitor(workerCtx, 1*time.Hour, 36*time.Hour)

	// Start Stage 9.1 Integration Workers (Unicommerce, Pine Labs, CleverTap)
	engines.StartUnicommerceWorker(workerCtx, 30*time.Second)
	engines.StartPineLabsReconciliationWorker(workerCtx, 5*time.Minute)
	engines.StartCleverTapWorker(workerCtx, 30*time.Second)

	// Start Report Export Worker (Stage 20.37) - polls for Pending
	// ReportExportJob documents and runs them in the background.
	engines.StartReportExportWorker(workerCtx, 10*time.Second)

	// Start Scheduled Report Worker (Stage 26.10.4) - daily-granularity scan
	// for Active ScheduledReport documents whose next_run_date has arrived.
	engines.StartScheduledReportWorker(workerCtx, 1*time.Hour)

	// Start Wave Auto-Creation Worker (Stage 42.4.1, open decision 5) -
	// hourly scan for Active WaveTemplates whose run_daily_at has arrived
	// today, reusing the same ticker/schema-fanout shape as the scheduled
	// report worker above rather than a second timer mechanism.
	engines.StartWaveAutoCreationWorker(workerCtx, 1*time.Hour)

	// Start Recurring Journal Worker (Stage 26.6.4) - daily-granularity scan
	// for Recurring Template JournalVouchers whose next_run_date has arrived.
	engines.StartRecurringJournalWorker(workerCtx, 1*time.Hour)

	// Start PIM Import Schedule Worker (Stage 36.3.1) - hourly scan for
	// Active Drop-Directory PIMImportSchedules whose next_run_date has
	// arrived, reusing the same ticker/schema-fanout shape as the scheduled
	// report and wave-auto-creation workers above.
	engines.StartPIMImportScheduleWorker(workerCtx, 1*time.Hour)

	// Start PIM Export Schedule Worker (Stage 36.4.2) - same ticker/
	// schema-fanout shape, delivering an export template's output via the
	// existing outbox instead of scanning a directory.
	engines.StartPIMExportScheduleWorker(workerCtx, 1*time.Hour)

	// Start Loyalty Expiry Worker (Stage 26.7.3) - daily-granularity sweep
	// of lapsed loyalty point lots.
	engines.StartLoyaltyExpiryWorker(workerCtx, 1*time.Hour)

	// Start Dunning Worker (Stage 37.4.4) - daily-granularity scan of
	// overdue SalesInvoices, dispatching Reminder/Escalation notifications
	// through the existing outbox.
	engines.StartDunningWorker(workerCtx, 1*time.Hour)

	// Start Amortization Worker (Stage 37.6.1/37.6.2) - monthly-granularity
	// recognition of deferred revenue and prepaid expense schedules.
	engines.StartAmortizationWorker(workerCtx, 1*time.Hour)

	// Start Recurring Billing Worker (Stage 37.6.3) - the
	// StartRecurringJournalWorker precedent, spawning a Draft SalesInvoice
	// per due RecurringSalesContract.
	engines.StartRecurringBillingWorker(workerCtx, 1*time.Hour)

	// Start Campaign Worker (Stage 26.7.4) - daily-granularity scan for
	// Active campaigns whose birthday/lapsed-customer trigger newly
	// matches a customer.
	engines.StartCampaignWorker(workerCtx, 1*time.Hour)

	// Start Competitor Undercut Worker (Stage 34.3) - hourly scan for SKUs
	// where a newly-recorded CompetitorPrice observation sits more than
	// market.undercut_threshold_pct below our last transacted price. Reads
	// only; alerts through the existing DispatchNotification path. No-ops for
	// any tenant that leaves the threshold at its 0 default.
	engines.StartCompetitorUndercutWorker(workerCtx, 1*time.Hour)

	// Start Reservation Sweeper (Stage 35.3.7) - releases expired cart holds
	// and reservations left behind by lines that are no longer reserved. It
	// never expires a live order's reservation on time alone; see the file
	// header in engines/reservation_sweeper.go for why that distinction is the
	// whole safety argument. Skips any tenant whose database has not had the
	// Stage 35.3.7 migration applied yet.
	engines.StartReservationSweeper(workerCtx, 5*time.Minute)

	// Start Public API Runtime Sweeper (Stage 38.3/38.5/38.9) - hourly deletion
	// of expired idempotency keys and traffic-log rows past their retention.
	// Skips any tenant whose database has not had the Stage 38.3 migration
	// applied yet, so it is safe to ship ahead of the migration.
	engines.StartPublicAPIRuntimeSweeper(workerCtx, 1*time.Hour)

	// Authentication API
	http.HandleFunc("POST /api/v1/login", apiMiddleware(handleLogin))

	// Version (Stage 14.6) - public, same tier as /login, so a client/ops
	// tool can check what build is running without authenticating first.
	http.HandleFunc("GET /api/v1/version", apiMiddleware(handleVersion))
	// Health check (24.14) - for a load balancer/process supervisor to poll;
	// same public tier as /version, no bearer token required.
	http.HandleFunc("GET /api/v1/health", apiMiddleware(handleHealth))
	// Stage 44: the gate behind Caddy's on_demand_tls "ask" directive - Caddy
	// calls this before requesting a certificate for a hostname it has not
	// seen, and issues only on a 2xx.
	//
	// Registered without apiMiddleware on purpose: Caddy calls it over
	// 127.0.0.1 mid-TLS-handshake with no bearer token to present, and
	// putting it in a rate-limit bucket would mean a burst of handshakes
	// could block certificate issuance for a legitimate tenant. It is not
	// under /api/v1 so deploy/Caddyfile can block /internal/* wholesale on
	// the public listener without pattern-matching a single API path.
	http.HandleFunc("GET /internal/tls-ask", handleTLSAsk)
	http.HandleFunc("POST /api/v1/auth/mfa/enroll", apiMiddleware(handleMFAEnroll))
	http.HandleFunc("POST /api/v1/auth/mfa/activate", apiMiddleware(handleMFAActivate))
	http.HandleFunc("POST /api/v1/auth/mfa/verify", apiMiddleware(handleMFAVerify))
	// Password reset (24.28) - both public (a locked-out user has no bearer
	// token to present), rate-limited in the same tight "login" bucket as
	// /login itself (see rateLimitCategory).
	http.HandleFunc("POST /api/v1/auth/forgot-password", apiMiddleware(handleForgotPassword))
	http.HandleFunc("POST /api/v1/auth/reset-password", apiMiddleware(handleResetPassword))

	// Self-service User Profile (Stage 21)
	http.HandleFunc("GET /api/v1/me", apiMiddleware(handleGetProfile))
	http.HandleFunc("PUT /api/v1/me", apiMiddleware(handleUpdateProfile))
	http.HandleFunc("POST /api/v1/me/change-password", apiMiddleware(handleChangePassword))
	http.HandleFunc("GET /api/v1/me/permissions", apiMiddleware(handleMyPermissions))
	// Self-service module entitlements (Stage 27) - any authenticated user
	// can read which products/modules are enabled for their OWN tenant, the
	// same way handleMyPermissions already does for role permissions. The
	// existing GET /api/v1/admin/tenant/module-entitlements stays HR/Admin-
	// only and untouched; this is the read a regular Cashier/Store-Manager
	// session needs so the frontend can filter its own nav by entitlement.
	http.HandleFunc("GET /api/v1/me/modules", apiMiddleware(handleMyModules))

	// Setup guidance + localization (Stage 41). Both are self-service reads
	// on the same tier as /me/permissions and /me/modules: every screen needs
	// them to render correctly for the signed-in user, so restricting them to
	// an admin would just mean a Cashier's screens render wrong.
	http.HandleFunc("GET /api/v1/setup/status", apiMiddleware(handleSetupStatus))
	http.HandleFunc("GET /api/v1/localization", apiMiddleware(handleGetLocalization))

	// MFA recovery and device migration (Stage 32.5). Unlike the three
	// /auth/mfa/* endpoints above - which run on short-lived purpose tokens
	// mid-login - these take a normal session token, because they are things a
	// signed-in user does about their own account. Between them they close the
	// lockout hole: recovery codes to get in without the authenticator, and a
	// re-enroll pair to move the authenticator to a new phone (which the
	// login-time /auth/mfa/enroll cannot do, being reachable only by accounts
	// that are not yet enrolled).
	http.HandleFunc("GET /api/v1/me/mfa/recovery-codes", apiMiddleware(handleMFARecoveryStatus))
	http.HandleFunc("POST /api/v1/me/mfa/recovery-codes/regenerate", apiMiddleware(handleMFARegenerateRecoveryCodes))
	http.HandleFunc("POST /api/v1/me/mfa/reenroll", apiMiddleware(handleMFAReenrollStart))
	http.HandleFunc("POST /api/v1/me/mfa/reenroll/confirm", apiMiddleware(handleMFAReenrollConfirm))
	http.HandleFunc("POST /api/v1/me/mfa/reenroll/cancel", apiMiddleware(handleMFAReenrollCancel))

	// Admin user/role management (Stage 21 QA fix) - the Users/Roles sidebar
	// items had never had a backend at all. HR/Admin-only, enforced in each handler.
	http.HandleFunc("GET /api/v1/admin/users", apiMiddleware(handleListUsers))
	http.HandleFunc("POST /api/v1/admin/users", apiMiddleware(handleCreateUser))
	http.HandleFunc("POST /api/v1/admin/users/status", apiMiddleware(handleSetUserStatus))
	http.HandleFunc("POST /api/v1/admin/users/location", apiMiddleware(handleSetUserLocation))
	// 32.5: an admin-side escape hatch for a colleague who lost both their
	// phone and their recovery codes. Puts the account back into the
	// enrollment state rather than disabling MFA, so the next login still has
	// to set up an authenticator.
	http.HandleFunc("POST /api/v1/admin/users/reset-mfa", apiMiddleware(handleAdminResetUserMFA))
	// 26.4.10: links a Supplier login to the Vendor it speaks for. Without
	// it a supplier account cannot be finished from inside the app.
	http.HandleFunc("POST /api/v1/admin/users/supplier", apiMiddleware(handleSetUserSupplier))
	http.HandleFunc("GET /api/v1/admin/roles", apiMiddleware(handleListRoles))
	http.HandleFunc("GET /api/v1/admin/role-permissions", apiMiddleware(handleRolePermissions))
	http.HandleFunc("POST /api/v1/admin/role-permissions", apiMiddleware(handleRolePermissions))

	// Stage 28.1: module-by-module admin configuration (system_settings).
	http.HandleFunc("GET /api/v1/admin/settings", apiMiddleware(handleGetSettings))
	http.HandleFunc("PUT /api/v1/admin/settings", apiMiddleware(handleUpdateSettings))
	http.HandleFunc("GET /api/v1/ops/deployment-status", apiMiddleware(handleDeploymentStatus))
	http.HandleFunc("GET /api/v1/ops/backup-status", apiMiddleware(handleBackupStatus))

	// Generic DocType CRUD APIs (Go 1.22 enhanced routing)
	http.HandleFunc("/api/v1/doc/{doctype}", apiMiddleware(handleGenericDoc))
	http.HandleFunc("/api/v1/doc/{doctype}/{id}", apiMiddleware(handleGenericDoc))
	http.HandleFunc("POST /api/v1/doc/{doctype}/{id}/reactivate", apiMiddleware(handleReactivateMasterDocument))

	// Availability & Reservation APIs
	http.HandleFunc("GET /api/v1/availability", apiMiddleware(handleGetAvailability))
	http.HandleFunc("POST /api/v1/reserve", apiMiddleware(handleCreateReservation))

	// Stage 26.12.1: Order Engine - create (validate/reserve chain), the
	// Hold engine's manual place/release actions, and the stage-gated
	// cancellation matrix. moduleGate("oms",...) since this is squarely OMS,
	// same convention as the other oms-gated routes below.
	http.HandleFunc("POST /api/v1/orders", apiMiddleware(moduleGate("oms", handleCreateSalesOrder)))
	http.HandleFunc("POST /api/v1/orders/{id}/hold", apiMiddleware(moduleGate("oms", handlePlaceOrderHold)))
	http.HandleFunc("POST /api/v1/orders/{id}/release-hold", apiMiddleware(moduleGate("oms", handleReleaseOrderHold)))
	http.HandleFunc("POST /api/v1/orders/{id}/cancel", apiMiddleware(moduleGate("oms", handleCancelOrder)))

	// Stage 35.2: the OMS Console. Read endpoints for the faceted list, the
	// one-call order detail, the report-backed tiles and global search, plus
	// bulk actions and saved views. The console's per-order actions reuse the
	// three routes above and 35.3's mutation routes below - no duplicate
	// action API.
	http.HandleFunc("GET /api/v1/oms/orders", apiMiddleware(moduleGate("oms", handleOMSOrderList)))
	http.HandleFunc("GET /api/v1/oms/orders/search", apiMiddleware(moduleGate("oms", handleOMSOrderSearch)))
	http.HandleFunc("GET /api/v1/oms/orders/{id}", apiMiddleware(moduleGate("oms", handleOMSOrderDetail)))
	http.HandleFunc("GET /api/v1/oms/tiles", apiMiddleware(moduleGate("oms", handleOMSConsoleTiles)))
	http.HandleFunc("POST /api/v1/oms/orders/bulk", apiMiddleware(moduleGate("oms", handleOMSBulkAction)))
	http.HandleFunc("GET /api/v1/oms/views", apiMiddleware(moduleGate("oms", handleOMSSavedViews)))
	http.HandleFunc("POST /api/v1/oms/views", apiMiddleware(moduleGate("oms", handleOMSSavedViews)))
	http.HandleFunc("DELETE /api/v1/oms/views/{id}", apiMiddleware(moduleGate("oms", handleOMSDeleteSavedView)))

	// Stage 35.3: order-mutation surface parity with Uniware - item-level
	// hold, order edit, switch facility, priority and split. Every one is
	// gated by the same StatusTransitionRule master (26.12.9) the
	// cancellation matrix uses, so the rules stay configurable per tenant.
	http.HandleFunc("POST /api/v1/orders/{id}/edit", apiMiddleware(moduleGate("oms", handleEditOrder)))
	http.HandleFunc("POST /api/v1/orders/{id}/switch-facility", apiMiddleware(moduleGate("oms", handleSwitchOrderFacility)))
	http.HandleFunc("POST /api/v1/orders/{id}/priority", apiMiddleware(moduleGate("oms", handleSetOrderPriority)))
	http.HandleFunc("POST /api/v1/orders/{id}/split", apiMiddleware(moduleGate("oms", handleSplitOrder)))
	http.HandleFunc("POST /api/v1/order-lines/{lineId}/hold", apiMiddleware(moduleGate("oms", handleHoldOrderLine)))
	http.HandleFunc("POST /api/v1/order-lines/{lineId}/release-hold", apiMiddleware(moduleGate("oms", handleReleaseOrderLineHold)))
	http.HandleFunc("GET /api/v1/oms/pick-queue", apiMiddleware(moduleGate("oms", handlePickQueue)))

	// Stage 35.4: the outbound document chain - shipping package, invoice
	// from pack, gate pass, credit note. The invoice route is the one that
	// closes 26.12.3's deferred invoice-from-pack item; the label and
	// manifest ordering rule it enables is enforced inside the existing
	// shipment engine, not here, so it applies to every caller.
	http.HandleFunc("POST /api/v1/oms/shipping-packages", apiMiddleware(moduleGate("oms", handleCreateShippingPackage)))
	http.HandleFunc("GET /api/v1/oms/shipping-packages", apiMiddleware(moduleGate("oms", handleListShippingPackages)))
	http.HandleFunc("POST /api/v1/oms/shipping-packages/{id}/update", apiMiddleware(moduleGate("oms", handleUpdateShippingPackage)))
	http.HandleFunc("POST /api/v1/oms/shipping-packages/{id}/split", apiMiddleware(moduleGate("oms", handleSplitShippingPackage)))
	http.HandleFunc("POST /api/v1/oms/shipping-packages/{id}/cancel", apiMiddleware(moduleGate("oms", handleCancelShippingPackage)))
	http.HandleFunc("POST /api/v1/oms/shipping-packages/{id}/invoice", apiMiddleware(moduleGate("oms", handleInvoiceShippingPackage)))
	http.HandleFunc("POST /api/v1/oms/gate-passes", apiMiddleware(moduleGate("oms", handleCreateGatePass)))
	http.HandleFunc("GET /api/v1/oms/gate-passes", apiMiddleware(moduleGate("oms", handleSearchGatePasses)))
	http.HandleFunc("POST /api/v1/oms/gate-passes/{id}/update", apiMiddleware(moduleGate("oms", handleUpdateGatePass)))
	http.HandleFunc("POST /api/v1/oms/gate-passes/{id}/issue", apiMiddleware(moduleGate("oms", handleGatePassTransition("issue"))))
	http.HandleFunc("POST /api/v1/oms/gate-passes/{id}/complete", apiMiddleware(moduleGate("oms", handleGatePassTransition("complete"))))
	http.HandleFunc("POST /api/v1/oms/gate-passes/{id}/discard", apiMiddleware(moduleGate("oms", handleGatePassTransition("discard"))))
	http.HandleFunc("POST /api/v1/orders/{id}/credit-notes", apiMiddleware(moduleGate("oms", handleOrderCreditNotes)))

	// Stage 26.12.5: Returns/RTO/QC/Refund - a request/approval-gated
	// workflow distinct from the pre-existing instant POST
	// /api/v1/fulfillment/return (ProcessReturnAnywhere, the POS in-store
	// path, unchanged). moduleGate("oms",...) since ReturnRequest/
	// RefundRequest are OMS doctypes, same convention as the Order Engine
	// routes above.
	http.HandleFunc("POST /api/v1/returns", apiMiddleware(moduleGate("oms", handleCreateReturnRequest)))
	http.HandleFunc("POST /api/v1/returns/{id}/approve", apiMiddleware(moduleGate("oms", handleApproveReturnRequest)))
	http.HandleFunc("POST /api/v1/returns/{id}/reject", apiMiddleware(moduleGate("oms", handleRejectReturnRequest)))
	http.HandleFunc("POST /api/v1/returns/{id}/receive", apiMiddleware(moduleGate("oms", handleReceiveReturnRequest)))
	http.HandleFunc("POST /api/v1/returns/{id}/qc", apiMiddleware(moduleGate("oms", handleApplyReturnQC)))
	// Stage 35.9.1: courier reverse pickup for a Customer Return.
	http.HandleFunc("POST /api/v1/returns/{id}/reverse-pickup", apiMiddleware(moduleGate("oms", handleScheduleReturnReversePickup)))
	http.HandleFunc("POST /api/v1/refunds/{id}/approve", apiMiddleware(moduleGate("oms", handleApproveRefundRequest)))
	http.HandleFunc("POST /api/v1/refunds/{id}/reject", apiMiddleware(moduleGate("oms", handleRejectRefundRequest)))
	http.HandleFunc("POST /api/v1/refunds/{id}/process", apiMiddleware(moduleGate("oms", handleProcessRefundRequest)))

	// Stage 35.8: Settlement/payment reconciliation. Import itself reuses
	// POST /api/v1/import/MarketplaceSettlementLine (registered generically
	// above, no dedicated route needed) - these are the matching/dispute/
	// write-off actions layered on top of it.
	http.HandleFunc("POST /api/v1/oms/settlements/reconcile", apiMiddleware(moduleGate("oms", handleReconcileMarketplaceSettlements)))
	http.HandleFunc("POST /api/v1/oms/settlements/{id}/dispute", apiMiddleware(moduleGate("oms", handleRaiseSettlementDispute)))
	http.HandleFunc("POST /api/v1/oms/settlements/{id}/resolve", apiMiddleware(moduleGate("oms", handleResolveSettlementDispute)))
	http.HandleFunc("POST /api/v1/oms/settlements/{id}/write-off", apiMiddleware(moduleGate("oms", handleWriteOffSettlementVariance)))

	// WMS Maturity (Stage 20 Track B.2): putaway, bin-grouped pick lists,
	// transfer-order pack/box-mapping, cycle-count reconciliation.
	// moduleGate("wms",...) added Stage 27 (Modular Product Packaging) - these
	// were previously registered role-open with no module-entitlement check
	// at all, which was fine while WMS was just a feature of one indivisible
	// product but stopped being safe once WMS became its own sellable
	// product (see engines/modules.go's ProductPackages) - a tenant that
	// never bought WMS must not be able to reach these.
	http.HandleFunc("POST /api/v1/wms/putaway", apiMiddleware(moduleGate("wms", handlePutaway)))
	http.HandleFunc("POST /api/v1/wms/hold/place", apiMiddleware(moduleGate("wms", handlePlaceHold)))
	http.HandleFunc("GET /api/v1/wms/pick-list", apiMiddleware(moduleGate("wms", handlePickList)))
	http.HandleFunc("POST /api/v1/wms/condition-transition", apiMiddleware(moduleGate("wms", handleBinConditionTransition)))
	http.HandleFunc("POST /api/v1/wms/transfer/pack", apiMiddleware(moduleGate("wms", handlePackTransferOrder)))
	// Stage 26.12.3: FulfillmentTask Pick/Pack scan endpoints - same module
	// gate as the pick-list/putaway/condition-transition floor-ops routes above.
	http.HandleFunc("POST /api/v1/wms/pick-scan", apiMiddleware(moduleGate("wms", handlePickScan)))
	http.HandleFunc("POST /api/v1/wms/pack-scan", apiMiddleware(moduleGate("wms", handlePackScan)))
	http.HandleFunc("POST /api/v1/wms/short-pick", apiMiddleware(moduleGate("wms", handleShortPick)))
	// Stage 42.4.6: this route now runs through handlePackCompleteValidated
	// (handlers_wms_outbound.go), which defers to the same, unchanged
	// CompletePackTask after an additive pre-pack checklist.
	http.HandleFunc("POST /api/v1/wms/pack-complete", apiMiddleware(moduleGate("wms", handlePackCompleteValidated)))
	http.HandleFunc("POST /api/v1/wms/cycle-count/reconcile", apiMiddleware(moduleGate("wms", handleReconcileCycleCount)))

	// Stage 26.5 (WMS Enterprise Maturity Sprint): cross-dock staging, LPN/
	// carton/pallet grouping, bin-to-bin replenishment, wave/batch picking,
	// cartonization suggestions, the ABC cycle-count planner, and the
	// blind-recount + variance-reason cycle-count workflow. Same
	// moduleGate("wms",...) as every other floor-ops route above.
	http.HandleFunc("POST /api/v1/wms/cross-dock/check", apiMiddleware(moduleGate("wms", handleCrossDockCheck)))
	http.HandleFunc("POST /api/v1/wms/cross-dock/putaway", apiMiddleware(moduleGate("wms", handleCrossDockPutaway)))
	http.HandleFunc("POST /api/v1/wms/cross-dock/planned-putaway", apiMiddleware(moduleGate("wms", handlePlannedCrossDockPutaway)))
	http.HandleFunc("POST /api/v1/wms/lpn/assign", apiMiddleware(moduleGate("wms", handleLPNAssign)))
	http.HandleFunc("GET /api/v1/wms/lpn/contents", apiMiddleware(moduleGate("wms", handleLPNContents)))
	http.HandleFunc("GET /api/v1/wms/bin-replenishment/suggestions", apiMiddleware(moduleGate("wms", handleBinReplenishmentSuggestions)))
	http.HandleFunc("POST /api/v1/wms/bin-replenishment/execute", apiMiddleware(moduleGate("wms", handleBinReplenishmentExecute)))
	http.HandleFunc("POST /api/v1/wms/wave/assign", apiMiddleware(moduleGate("wms", handleWaveAssign)))
	http.HandleFunc("GET /api/v1/wms/wave/pick-list", apiMiddleware(moduleGate("wms", handleWavePickList)))
	// 26.5.16 (P2, go-ahead 2026-07-27): robotics/conveyor/scale inbound integration
	http.HandleFunc("POST /api/v1/wms/robotics/event", apiMiddleware(moduleGate("wms", handleRoboticsEvent)))
	http.HandleFunc("POST /api/v1/wms/cartonization/suggest", apiMiddleware(moduleGate("wms", handleCartonizationSuggest)))
	http.HandleFunc("GET /api/v1/wms/cycle-count/abc-plan", apiMiddleware(moduleGate("wms", handleABCCycleCountPlan)))
	http.HandleFunc("POST /api/v1/wms/cycle-count/recount/request", apiMiddleware(moduleGate("wms", handleRequestRecount)))
	http.HandleFunc("POST /api/v1/wms/cycle-count/recount/submit", apiMiddleware(moduleGate("wms", handleSubmitRecountValue)))
	http.HandleFunc("POST /api/v1/wms/cycle-count/variance-reason", apiMiddleware(moduleGate("wms", handleSetCycleCountVarianceReason)))
	http.HandleFunc("POST /api/v1/wms/cycle-count/post-adjustment", apiMiddleware(moduleGate("wms", handleRetryCycleCountPost)))
	// Stage 42.1 (traceability foundation): batch/lot floor actions. Same
	// moduleGate("wms", ...) as every other floor-ops route above. The three
	// read paths (near-expiry watchlist, batch stock inquiry, batch movement
	// history) are ReportDefinitions served by the generic report endpoint, so
	// they deliberately have no routes of their own.
	http.HandleFunc("POST /api/v1/wms/batch/putaway", apiMiddleware(moduleGate("wms", handleBatchPutaway)))
	http.HandleFunc("POST /api/v1/wms/batch/consume", apiMiddleware(moduleGate("wms", handleBatchConsume)))
	http.HandleFunc("POST /api/v1/wms/batch/status", apiMiddleware(moduleGate("wms", handleBatchStatus)))
	http.HandleFunc("POST /api/v1/wms/batch/expiry-sweep", apiMiddleware(moduleGate("wms", handleBatchExpirySweep)))
	http.HandleFunc("GET /api/v1/wms/batch/allocation-preview", apiMiddleware(moduleGate("wms", handleBatchAllocationPreview)))
	// 42.1.8 - the serial analogues. Same "reports own the read paths" split:
	// serial-inquiry and serial-movement-history are ReportDefinitions, not
	// routes here.
	http.HandleFunc("POST /api/v1/wms/serial/putaway", apiMiddleware(moduleGate("wms", handleSerialPutaway)))
	http.HandleFunc("POST /api/v1/wms/serial/status", apiMiddleware(moduleGate("wms", handleSerialStatus)))
	// 42.2 - the warehouse task spine. handleNextTask is the RF/mobile
	// dispatch call; handleTransitionTask is the one lifecycle route every
	// future floor action (start/complete/except/cancel) funnels through.
	http.HandleFunc("POST /api/v1/wms/tasks/next", apiMiddleware(moduleGate("wms", handleNextTask)))
	http.HandleFunc("POST /api/v1/wms/tasks/transition", apiMiddleware(moduleGate("wms", handleTransitionTask)))
	// 42.2.7 - directed putaway suggestion; 42.2.10 - the warehouse cockpit.
	http.HandleFunc("GET /api/v1/wms/putaway/suggest-bin", apiMiddleware(moduleGate("wms", handleSuggestPutawayBin)))
	http.HandleFunc("GET /api/v1/wms/cockpit", apiMiddleware(moduleGate("wms", handleWarehouseCockpit)))

	// Stage 42.4 - outbound depth. Same moduleGate("wms", ...) as every other
	// floor-ops route above.
	http.HandleFunc("POST /api/v1/wms/wave/create", apiMiddleware(moduleGate("wms", handleWaveCreate)))
	http.HandleFunc("POST /api/v1/wms/wave/template/run", apiMiddleware(moduleGate("wms", handleWaveTemplateRun)))
	http.HandleFunc("POST /api/v1/wms/wave/transition", apiMiddleware(moduleGate("wms", handleWaveTransition)))
	http.HandleFunc("GET /api/v1/wms/wave/monitor", apiMiddleware(moduleGate("wms", handleWaveMonitor)))
	http.HandleFunc("POST /api/v1/wms/sortation/provision-slots", apiMiddleware(moduleGate("wms", handleSortSlotProvision)))
	http.HandleFunc("POST /api/v1/wms/sortation/assign", apiMiddleware(moduleGate("wms", handleSortSlotAssign)))
	http.HandleFunc("POST /api/v1/wms/sortation/confirm", apiMiddleware(moduleGate("wms", handleSortSlotConfirm)))
	http.HandleFunc("POST /api/v1/wms/sortation/clear", apiMiddleware(moduleGate("wms", handleSortSlotClear)))
	http.HandleFunc("GET /api/v1/wms/sortation/slots", apiMiddleware(moduleGate("wms", handleSortSlotList)))
	http.HandleFunc("POST /api/v1/wms/cartonization/suggest-v2", apiMiddleware(moduleGate("wms", handleCartonizationSuggestV2)))
	http.HandleFunc("GET /api/v1/wms/packing-validation", apiMiddleware(moduleGate("wms", handlePackingValidation)))
	http.HandleFunc("GET /api/v1/wms/pack-template/resolve", apiMiddleware(moduleGate("wms", handlePackTemplateResolve)))
	http.HandleFunc("POST /api/v1/wms/lpn/deconsolidate", apiMiddleware(moduleGate("wms", handleDeconsolidateLPN)))
	http.HandleFunc("POST /api/v1/wms/loading/create", apiMiddleware(moduleGate("wms", handleLoadingTaskCreate)))
	http.HandleFunc("POST /api/v1/wms/loading/scan", apiMiddleware(moduleGate("wms", handleLoadingScan)))
	http.HandleFunc("POST /api/v1/wms/loading/complete", apiMiddleware(moduleGate("wms", handleLoadingComplete)))
	http.HandleFunc("POST /api/v1/wms/loading/depart", apiMiddleware(moduleGate("wms", handleLoadingDepart)))
	http.HandleFunc("GET /api/v1/wms/loading/bol", apiMiddleware(moduleGate("wms", handleBillOfLading)))
	http.HandleFunc("POST /api/v1/wms/vas/create", apiMiddleware(moduleGate("wms", handleVASTaskCreate)))
	http.HandleFunc("POST /api/v1/wms/vas/complete", apiMiddleware(moduleGate("wms", handleVASTaskComplete)))

	// Stage 42.5 - inventory control depth: physical inventory, the
	// CycleClass-aware cycle-count plan, demand-driven/wave-triggered/
	// dynamic pick-face replenishment, and facility hierarchy/copy. Same
	// moduleGate("wms", ...) as every other floor-ops route above. Slotting
	// v2 and the two facility-inventory inquiries add no routes here - see
	// handlers_wms_inventory_depth.go's own comment: they're
	// ReportDefinitions served by the generic report endpoint.
	http.HandleFunc("POST /api/v1/wms/physical-inventory/start", apiMiddleware(moduleGate("wms", handlePhysicalInventoryStart)))
	http.HandleFunc("POST /api/v1/wms/physical-inventory/submit-count", apiMiddleware(moduleGate("wms", handlePhysicalInventorySubmitCount)))
	http.HandleFunc("POST /api/v1/wms/physical-inventory/reconcile", apiMiddleware(moduleGate("wms", handlePhysicalInventoryReconcile)))
	http.HandleFunc("POST /api/v1/wms/physical-inventory/close", apiMiddleware(moduleGate("wms", handlePhysicalInventoryClose)))
	http.HandleFunc("POST /api/v1/wms/physical-inventory/cancel", apiMiddleware(moduleGate("wms", handlePhysicalInventoryCancel)))
	http.HandleFunc("GET /api/v1/wms/cycle-count/plan", apiMiddleware(moduleGate("wms", handleCycleCountPlan)))
	http.HandleFunc("GET /api/v1/wms/bin-replenishment/demand-driven", apiMiddleware(moduleGate("wms", handleDemandDrivenReplenishmentSuggestions)))
	http.HandleFunc("GET /api/v1/wms/bin-replenishment/wave", apiMiddleware(moduleGate("wms", handleWaveReplenishmentSuggestions)))
	http.HandleFunc("GET /api/v1/wms/bin-replenishment/dynamic-pickface", apiMiddleware(moduleGate("wms", handleDynamicPickFaceSuggestions)))
	http.HandleFunc("POST /api/v1/wms/bin-replenishment/dynamic-pickface/apply", apiMiddleware(moduleGate("wms", handleApplyDynamicPickFaceMinMax)))
	http.HandleFunc("GET /api/v1/wms/facility/children", apiMiddleware(moduleGate("wms", handleFacilityChildren)))
	http.HandleFunc("GET /api/v1/wms/facility/descendants", apiMiddleware(moduleGate("wms", handleFacilityDescendants)))
	http.HandleFunc("POST /api/v1/wms/facility/copy", apiMiddleware(moduleGate("wms", handleFacilityCopy)))
	// 42.5.5 - multi-owner stock segregation. Same manual assign/consume shape
	// as the batch breakdown's /wms/batch/putaway and /wms/batch/consume.
	http.HandleFunc("POST /api/v1/wms/owner-stock/assign", apiMiddleware(moduleGate("wms", handleOwnerStockAssign)))
	http.HandleFunc("POST /api/v1/wms/owner-stock/consume", apiMiddleware(moduleGate("wms", handleOwnerStockConsume)))

	// Stage 42.6 - engineered labour planning and 3PL billing. The associated
	// masters stay on the generic document API; these are the calculation and
	// system-write actions that must not be editable like ordinary documents.
	http.HandleFunc("GET /api/v1/wms/labor/plan", apiMiddleware(moduleGate("wms", handleLaborPlan)))
	http.HandleFunc("GET /api/v1/wms/billing/charges", apiMiddleware(moduleGate("wms", handleCapturedCharges)))
	http.HandleFunc("POST /api/v1/wms/billing/charges", apiMiddleware(moduleGate("wms", handleCapturedCharges)))
	http.HandleFunc("POST /api/v1/wms/billing/invoices", apiMiddleware(moduleGate("wms", handleCapturedChargeInvoice)))
	http.HandleFunc("POST /api/v1/wms/billing/storage/snapshot", apiMiddleware(moduleGate("wms", handleStorageBalanceSnapshot)))
	http.HandleFunc("GET /api/v1/wms/billing/storage", apiMiddleware(moduleGate("wms", handleStorageBillingV2)))
	http.HandleFunc("POST /api/v1/wms/billing/storage/capture", apiMiddleware(moduleGate("wms", handleCaptureStorageCharges)))

	// Checkout & Finance APIs
	http.HandleFunc("POST /api/v1/checkout", apiMiddleware(handleCheckout))
	http.HandleFunc("POST /api/v1/pos/session/open", apiMiddleware(handlePOSSessionOpen))
	http.HandleFunc("POST /api/v1/pos/session/close", apiMiddleware(handlePOSSessionClose))
	http.HandleFunc("GET /api/v1/pos/session/current", apiMiddleware(handlePOSSessionCurrent))
	http.HandleFunc("POST /api/v1/pos/offline-heartbeat", apiMiddleware(handlePOSOfflineHeartbeat))
	// Stage 30.7: read-only offer preview for the POS cart. Checkout
	// re-evaluates server-side regardless, so this never sets the price.
	http.HandleFunc("POST /api/v1/pos/offers/preview", apiMiddleware(handlePOSOffersPreview))
	http.HandleFunc("GET /api/v1/finance/trial-balance", apiMiddleware(handleTrialBalance))
	http.HandleFunc("GET /api/v1/finance/periods", apiMiddleware(handleAccountingPeriods))
	http.HandleFunc("POST /api/v1/finance/periods", apiMiddleware(handleAccountingPeriods))
	http.HandleFunc("POST /api/v1/finance/periods/{id}/close", apiMiddleware(handleCloseAccountingPeriod))
	http.HandleFunc("GET /api/v1/finance/periods/{id}/close-checklist", apiMiddleware(handlePeriodCloseChecklist))

	// Finance Maturity (Stage 20 Track B.3): bank reconciliation, payment
	// proposals/batch, TDS-aware vendor payment, debit/credit notes,
	// SalesInvoice post/settle (the AR side Receivables Ageing reads).
	http.HandleFunc("POST /api/v1/finance/bank-reconcile", apiMiddleware(handleBankReconcile))
	http.HandleFunc("POST /api/v1/finance/payment-proposal", apiMiddleware(handlePaymentProposal))
	http.HandleFunc("POST /api/v1/finance/payment-proposal/{id}/execute", apiMiddleware(handleExecutePaymentProposal))
	// Stage 26.6.5: bank-file generation + duplicate-UTR check for an
	// Executed proposal.
	http.HandleFunc("GET /api/v1/finance/payment-proposal/{id}/payment-file", apiMiddleware(handleGeneratePaymentFile))
	http.HandleFunc("POST /api/v1/finance/payment-proposal/{id}/record-utr", apiMiddleware(handleRecordPaymentUTR))
	http.HandleFunc("GET /api/v1/finance/payment-proposal/{id}/utrs", apiMiddleware(handleListPaymentUTRs))
	http.HandleFunc("POST /api/v1/procurement/vendor-invoice/pay-with-tds", apiMiddleware(handlePayVendorInvoiceWithTDS))
	http.HandleFunc("POST /api/v1/finance/debit-note/{id}/post", apiMiddleware(handlePostDebitNote))
	http.HandleFunc("POST /api/v1/finance/credit-note/{id}/post", apiMiddleware(handlePostCreditNote))
	http.HandleFunc("POST /api/v1/finance/sales-invoice/{id}/post", apiMiddleware(handlePostSalesInvoice))
	http.HandleFunc("POST /api/v1/finance/sales-invoice/{id}/settle", apiMiddleware(handleSettleSalesInvoice))

	// Stage 37.1.4/37.1.5 - FX revaluation and the presentation-currency trial
	// balance. GET previews the revaluation and POST commits it; the split is
	// the verb, not a flag, so a preview can never post. The three 37.1.5
	// reports need no route - they are registered ReportDefinitions and the
	// generic report catalog already serves and exports them.
	http.HandleFunc("GET /api/v1/finance/fx-revaluation", apiMiddleware(handleFXRevaluation))
	http.HandleFunc("POST /api/v1/finance/fx-revaluation", apiMiddleware(handleFXRevaluation))
	http.HandleFunc("GET /api/v1/finance/trial-balance/presentation", apiMiddleware(handleTrialBalancePresentationCurrency))

	// Journal Voucher (Stage 26.6.4) - manual GL entries, reversal,
	// recurring templates. Submit-for-approval/approve/reject reuse the
	// generic /api/v1/approval/submit|decide endpoints below (doctype
	// "JournalVoucher") - no new endpoint needed for that part.
	http.HandleFunc("POST /api/v1/finance/journal-voucher", apiMiddleware(handleCreateJournalVoucher))
	http.HandleFunc("POST /api/v1/finance/journal-voucher/{id}/reverse", apiMiddleware(handleReverseJournalVoucher))
	http.HandleFunc("POST /api/v1/finance/journal-voucher/{id}/retry-post", apiMiddleware(handleRetryPostJournalVoucher))
	http.HandleFunc("POST /api/v1/finance/journal-voucher/recurring", apiMiddleware(handleCreateRecurringJournalTemplate))

	// Multi-entity & intercompany (Stage 37.2.2). Submit-for-approval/
	// approve/reject reuse the generic /api/v1/approval/submit|decide
	// endpoints below (doctype "IntercompanyTransaction") - no new endpoint
	// needed for that part. The three new reports (entity-trial-balance,
	// intercompany-reconciliation, consolidated-trial-balance) need no
	// route either - they are registered ReportDefinitions served by the
	// generic report catalog, the same posture 37.1.5 already took.
	http.HandleFunc("POST /api/v1/finance/intercompany-transaction", apiMiddleware(handleCreateIntercompanyTransaction))
	http.HandleFunc("POST /api/v1/finance/intercompany-transaction/{id}/retry-post", apiMiddleware(handleRetryPostIntercompanyTransaction))

	// Costing & valuation (Stage 37.3.2). The inventory-valuation report
	// needs no route - it is a registered ReportDefinition served by the
	// generic report catalog.
	http.HandleFunc("POST /api/v1/finance/landed-cost-voucher", apiMiddleware(handleCreateLandedCostVoucher))
	http.HandleFunc("POST /api/v1/finance/landed-cost-voucher/{id}/apply", apiMiddleware(handleApplyLandedCostVoucher))

	// Approval / Workflow Engine (maker-checker)
	http.HandleFunc("POST /api/v1/approval/submit", apiMiddleware(handleSubmitApproval))
	http.HandleFunc("POST /api/v1/approval/decide", apiMiddleware(handleDecideApproval))
	http.HandleFunc("POST /api/v1/approval/bulk-decide", apiMiddleware(handleBulkDecideApproval))
	http.HandleFunc("GET /api/v1/approval/log", apiMiddleware(handleApprovalLog))
	http.HandleFunc("GET /api/v1/approval/pending", apiMiddleware(handleListPendingApprovals))
	http.HandleFunc("/api/v1/approval/rules", apiMiddleware(handleApprovalRules))

	// GST / Tax Engine
	http.HandleFunc("POST /api/v1/gst/calculate", apiMiddleware(handleCalculateGST))

	// Report Catalog (Stage 14.1: module-gated - "reports")
	http.HandleFunc("GET /api/v1/reports/current-stock", apiMiddleware(moduleGate("reports", handleCurrentStockReport)))
	http.HandleFunc("GET /api/v1/reports/sales-register", apiMiddleware(moduleGate("reports", handleSalesRegisterReport)))
	http.HandleFunc("GET /api/v1/reports/vendor-ledger", apiMiddleware(moduleGate("reports", handleVendorLedgerReport)))
	http.HandleFunc("GET /api/v1/reports/payables-ageing", apiMiddleware(moduleGate("reports", handlePayablesAgeingReport)))
	http.HandleFunc("GET /api/v1/reports/receivables-ageing", apiMiddleware(moduleGate("reports", handleReceivablesAgeingReport)))
	http.HandleFunc("GET /api/v1/reports/gst-return-summary", apiMiddleware(moduleGate("reports", handleGSTReturnSummary)))

	// Report Engine framework (Stage 20 Track B.4): generic catalog/run/
	// drill-down/async-export, on top of the same reports above and new
	// catalog additions (engines/report_definitions.go).
	http.HandleFunc("GET /api/v1/reports/catalog", apiMiddleware(moduleGate("reports", handleReportCatalog)))
	http.HandleFunc("GET /api/v1/reports/run/{id}", apiMiddleware(moduleGate("reports", handleRunReport)))
	http.HandleFunc("GET /api/v1/reports/drilldown/{id}", apiMiddleware(moduleGate("reports", handleReportDrillDown)))
	http.HandleFunc("POST /api/v1/reports/export", apiMiddleware(moduleGate("reports", handleCreateReportExport)))
	http.HandleFunc("GET /api/v1/reports/export/{id}", apiMiddleware(moduleGate("reports", handleGetReportExport)))

	// RFQ / Vendor Quote / Quote Comparison (Stage 14.1: module-gated - "rfq")
	http.HandleFunc("GET /api/v1/rfq/quotes", apiMiddleware(moduleGate("rfq", handleGetVendorQuotesForRFQ)))
	http.HandleFunc("POST /api/v1/rfq/select-quote", apiMiddleware(moduleGate("rfq", handleSelectWinningQuote)))

	// Sticker / Barcode Printing (Stage 14.1: module-gated - "stickers")
	http.HandleFunc("POST /api/v1/stickers/print", apiMiddleware(moduleGate("stickers", handlePrintStickers)))
	http.HandleFunc("GET /api/v1/stickers/history", apiMiddleware(moduleGate("stickers", handlePrintHistory)))

	// QZ Tray silent printing (Stage 31.1). Shares the "stickers" module key
	// with the routes above because the Printer Master these all resolve
	// against is already mapped to it - a new key would 403 every existing
	// tenant until their plan was edited.
	http.HandleFunc("GET /api/v1/print/qz/certificate", apiMiddleware(moduleGate("stickers", handleQZCertificate)))
	http.HandleFunc("POST /api/v1/print/qz/sign", apiMiddleware(moduleGate("stickers", handleQZSign)))
	http.HandleFunc("GET /api/v1/print/qz/printers", apiMiddleware(moduleGate("stickers", handleQZPrinters)))
	http.HandleFunc("POST /api/v1/print/qz/payload", apiMiddleware(moduleGate("stickers", handleQZPrintPayload)))
	http.HandleFunc("GET /api/v1/print/qz/log", apiMiddleware(moduleGate("stickers", handleQZPrintLog)))
	http.HandleFunc("POST /api/v1/print/qz/log", apiMiddleware(moduleGate("stickers", handleQZPrintLog)))

	// HR Foundation (Stage 14.1: module-gated - "hr")
	http.HandleFunc("GET /api/v1/hr/payroll-export", apiMiddleware(moduleGate("hr", handlePayrollExport)))

	// HR/Payroll Maturity Sprint (Stage 26.8, module-gated - "hr")
	http.HandleFunc("GET /api/v1/hr/salary-components", apiMiddleware(moduleGate("hr", handleSalaryComponentsPreview)))
	http.HandleFunc("POST /api/v1/hr/run-payroll", apiMiddleware(moduleGate("hr", handleRunPayroll)))
	http.HandleFunc("POST /api/v1/hr/post-payslip", apiMiddleware(moduleGate("hr", handlePostPayslip)))
	http.HandleFunc("POST /api/v1/hr/disburse-loan", apiMiddleware(moduleGate("hr", handleDisburseEmployeeLoan)))
	http.HandleFunc("GET /api/v1/hr/my-employee", apiMiddleware(moduleGate("hr", handleMyEmployeeRecord)))

	// Fixed Asset Management (Stage 14.1: module-gated - "assets")
	http.HandleFunc("GET /api/v1/assets/register", apiMiddleware(moduleGate("assets", handleAssetRegister)))
	http.HandleFunc("POST /api/v1/assets/capitalize", apiMiddleware(moduleGate("assets", handleCapitalizeAsset)))
	http.HandleFunc("POST /api/v1/assets/transfer", apiMiddleware(moduleGate("assets", handleTransferAsset)))
	http.HandleFunc("POST /api/v1/assets/dispose", apiMiddleware(moduleGate("assets", handleDisposeAsset)))

	// Expense Management (Stage 14.1: module-gated - "expenses")
	http.HandleFunc("POST /api/v1/expenses/verify", apiMiddleware(moduleGate("expenses", handleVerifyExpenseClaim)))
	http.HandleFunc("POST /api/v1/expenses/pay", apiMiddleware(moduleGate("expenses", handlePayExpenseClaim)))

	// CRM / Loyalty (Stage 14.1: module-gated - "crm_loyalty")
	http.HandleFunc("POST /api/v1/loyalty/redeem", apiMiddleware(moduleGate("crm_loyalty", handleRedeemLoyaltyPoints)))
	http.HandleFunc("GET /api/v1/loyalty/ledger", apiMiddleware(moduleGate("crm_loyalty", handleLoyaltyLedger)))
	// Stage 26.7.2/26.7.3: voucher redemption (create/list/bulk-issue reuse
	// the generic doctype/CSV-import endpoints - Voucher is a flat-schema
	// Master) and the loyalty tier rules admin config.
	http.HandleFunc("POST /api/v1/crm/voucher/validate", apiMiddleware(moduleGate("crm_loyalty", handleValidateVoucher)))
	http.HandleFunc("POST /api/v1/crm/voucher/redeem", apiMiddleware(moduleGate("crm_loyalty", handleRedeemVoucher)))
	http.HandleFunc("/api/v1/crm/loyalty-tier-rules", apiMiddleware(moduleGate("crm_loyalty", handleLoyaltyTierRules)))
	// Stage 26.7.5: OTP + fraud/staff-restriction gated redemption - an
	// opt-in alternative to the immediate /loyalty/redeem above.
	http.HandleFunc("POST /api/v1/crm/loyalty-redemption/initiate", apiMiddleware(moduleGate("crm_loyalty", handleInitiateSecureLoyaltyRedemption)))
	http.HandleFunc("POST /api/v1/crm/loyalty-redemption/verify", apiMiddleware(moduleGate("crm_loyalty", handleVerifySecureLoyaltyRedemption)))
	// 26.7.9/26.7.11 (P2, go-ahead 2026-07-27): householding/merge + inbound CleverTap segment sync
	http.HandleFunc("POST /api/v1/crm/customer/merge", apiMiddleware(moduleGate("crm_loyalty", handleMergeCustomers)))
	http.HandleFunc("POST /api/v1/integrations/clevertap/segment-sync", apiMiddleware(moduleGate("crm_loyalty", handleCleverTapSegmentSync)))

	// Manufacturing (Stage 14.1: module-gated - "manufacturing")
	http.HandleFunc("POST /api/v1/manufacturing/issue-material", apiMiddleware(moduleGate("manufacturing", handleIssueProductionMaterial)))
	http.HandleFunc("POST /api/v1/manufacturing/complete", apiMiddleware(moduleGate("manufacturing", handleCompleteProductionOrder)))

	// Manufacturing/MRP Maturity Sprint (Stage 26.9, module-gated - "manufacturing")
	http.HandleFunc("POST /api/v1/manufacturing/partial-complete", apiMiddleware(moduleGate("manufacturing", handlePartialCompleteProductionOrder)))
	http.HandleFunc("POST /api/v1/manufacturing/scrap", apiMiddleware(moduleGate("manufacturing", handlePostProductionScrap)))
	http.HandleFunc("POST /api/v1/manufacturing/rework", apiMiddleware(moduleGate("manufacturing", handleSendProductionToRework)))
	http.HandleFunc("POST /api/v1/manufacturing/confirm-operation", apiMiddleware(moduleGate("manufacturing", handleConfirmProductionOperation)))
	http.HandleFunc("POST /api/v1/manufacturing/acknowledge-bom-variance", apiMiddleware(moduleGate("manufacturing", handleAcknowledgeBOMVariance)))
	http.HandleFunc("POST /api/v1/manufacturing/record-actual-cost", apiMiddleware(moduleGate("manufacturing", handleRecordActualProductionCost)))
	http.HandleFunc("GET /api/v1/manufacturing/mrp-suggestions", apiMiddleware(moduleGate("manufacturing", handleMRPSuggestions)))
	http.HandleFunc("GET /api/v1/manufacturing/active-bom", apiMiddleware(moduleGate("manufacturing", handleActiveBOMForItem)))
	// 26.9.10/26.9.11 (P2, go-ahead 2026-07-27): capacity scheduling + subcontracting
	http.HandleFunc("GET /api/v1/manufacturing/production-schedule", apiMiddleware(moduleGate("manufacturing", handleGetProductionSchedule)))
	http.HandleFunc("POST /api/v1/manufacturing/subcontract-order/send", apiMiddleware(moduleGate("manufacturing", handleSendSubcontractOrder)))
	http.HandleFunc("POST /api/v1/manufacturing/subcontract-order/receive", apiMiddleware(moduleGate("manufacturing", handleReceiveSubcontractOrder)))

	// PIM Foundation MVP (Stage 15: module-gated - "pim")
	// Dashboard (Stage 16.5a) reads the existing PIM snapshot/queue state;
	// it is a fixed PIM route, so module gating happens at registration.
	http.HandleFunc("GET /api/v1/pim/dashboard", apiMiddleware(moduleGate("pim", handlePIMDashboard)))
	// Bulk edit (Stage 16.5b) is deliberately a PIM-only endpoint; its
	// handler additionally applies the target doctype's normal update RBAC.
	http.HandleFunc("POST /api/v1/pim/bulk-edit", apiMiddleware(moduleGate("pim", handlePIMBulkEdit)))
	http.HandleFunc("GET /api/v1/pim/reports/{name}", apiMiddleware(moduleGate("pim", handlePIMReport)))
	http.HandleFunc("GET /api/v1/pim/workbench", apiMiddleware(moduleGate("pim", handlePIMWorkbench)))
	// Stage 36.1: resolve a saved static/dynamic Product Group on demand.
	http.HandleFunc("GET /api/v1/pim/product-groups/{id}/members", apiMiddleware(moduleGate("pim", handlePIMProductGroupMembers)))
	http.HandleFunc("GET /api/v1/pim/product-groups/{id}/export.csv", apiMiddleware(moduleGate("pim", handlePIMProductGroupExport)))

	// Stage 36.2 - the PIM task & workflow engine. Authoring a template or a
	// workflow definition stays on the generic /api/v1/doc/{doctype} API; what
	// is here is only what the generic API cannot express - the task state
	// machine, template instantiation against a group, and the run lifecycle.
	http.HandleFunc("GET /api/v1/pim/tasks", apiMiddleware(moduleGate("pim", handlePIMTasks)))
	http.HandleFunc("POST /api/v1/pim/tasks", apiMiddleware(moduleGate("pim", handlePIMTasks)))
	http.HandleFunc("POST /api/v1/pim/tasks/bulk", apiMiddleware(moduleGate("pim", handlePIMTaskBulk)))
	http.HandleFunc("POST /api/v1/pim/tasks/{id}/{action}", apiMiddleware(moduleGate("pim", handlePIMTaskAction)))
	http.HandleFunc("GET /api/v1/pim/assignable-users", apiMiddleware(moduleGate("pim", handlePIMAssignableUsers)))
	http.HandleFunc("GET /api/v1/pim/task-templates", apiMiddleware(moduleGate("pim", handlePIMTaskTemplates)))
	http.HandleFunc("POST /api/v1/pim/task-templates/{code}/instantiate", apiMiddleware(moduleGate("pim", handlePIMTaskTemplateInstantiate)))
	http.HandleFunc("GET /api/v1/pim/workflows", apiMiddleware(moduleGate("pim", handlePIMWorkflows)))
	http.HandleFunc("POST /api/v1/pim/workflows/{code}/start", apiMiddleware(moduleGate("pim", handlePIMWorkflowStart)))
	http.HandleFunc("GET /api/v1/pim/workflow-runs", apiMiddleware(moduleGate("pim", handlePIMWorkflowRuns)))
	http.HandleFunc("POST /api/v1/pim/workflow-runs/bulk", apiMiddleware(moduleGate("pim", handlePIMWorkflowRunBulk)))
	http.HandleFunc("POST /api/v1/pim/workflow-runs/{id}/action", apiMiddleware(moduleGate("pim", handlePIMWorkflowRunAction)))
	http.HandleFunc("GET /api/v1/pim/completeness/{itemCode}", apiMiddleware(moduleGate("pim", handlePIMCompleteness)))
	// Declarative data transformation rules (Stage 36.5) - the vocabulary a
	// Channel Field Map or (Stage 36.3) Import Template step editor offers.
	http.HandleFunc("GET /api/v1/pim/transform-rules", apiMiddleware(moduleGate("pim", handlePIMTransformRules)))
	// Content assist (Stage 26.4.11) - local/offline draft generation from the
	// item's own data. Suggest-only: writes no ProductContent.
	http.HandleFunc("GET /api/v1/pim/content-assist/{itemCode}", apiMiddleware(moduleGate("pim", handlePIMContentAssist)))
	// Media Library (Stage 15.2, versioning/alt-text/expiry/thumbnails Stage 26.4.4)
	http.HandleFunc("POST /api/v1/pim/media/upload", apiMiddleware(moduleGate("pim", handlePIMMediaUpload)))
	http.HandleFunc("GET /api/v1/pim/media/{id}/file", apiMiddleware(moduleGate("pim", handlePIMMediaFile)))
	http.HandleFunc("GET /api/v1/pim/media/{id}/thumbnail", apiMiddleware(moduleGate("pim", handlePIMMediaThumbnail)))
	http.HandleFunc("POST /api/v1/pim/media/{id}/metadata", apiMiddleware(moduleGate("pim", handlePIMMediaUpdateMetadata)))
	http.HandleFunc("GET /api/v1/pim/media", apiMiddleware(moduleGate("pim", handlePIMMediaList)))
	http.HandleFunc("POST /api/v1/pim/media/{id}/deactivate", apiMiddleware(moduleGate("pim", handlePIMMediaDeactivate)))
	// DAM depth (Stage 36.6): on-demand transform renditions, bulk zip
	// up/down with filename auto-association, and the catalog-wide search
	// the Media Library browse tab reads.
	http.HandleFunc("GET /api/v1/pim/media/transform-presets", apiMiddleware(moduleGate("pim", handlePIMMediaTransformPresets)))
	http.HandleFunc("GET /api/v1/pim/media/{id}/transform/{preset}", apiMiddleware(moduleGate("pim", handlePIMMediaTransform)))
	http.HandleFunc("POST /api/v1/pim/media/bulk-upload", apiMiddleware(moduleGate("pim", handlePIMMediaBulkUpload)))
	http.HandleFunc("GET /api/v1/pim/media/bulk-download", apiMiddleware(moduleGate("pim", handlePIMMediaBulkDownload)))
	http.HandleFunc("GET /api/v1/pim/media/search", apiMiddleware(moduleGate("pim", handlePIMMediaSearch)))
	// Channel Publishing (Stage 15.2; validation packs + diff preview Stage 26.4.7)
	http.HandleFunc("POST /api/v1/pim/publish", apiMiddleware(moduleGate("pim", handlePIMPublish)))
	http.HandleFunc("GET /api/v1/pim/publish-preview", apiMiddleware(moduleGate("pim", handlePIMPublishPreview)))
	http.HandleFunc("GET /api/v1/pim/publish/{jobID}", apiMiddleware(moduleGate("pim", handlePIMPublishJobStatus)))
	http.HandleFunc("GET /api/v1/pim/publish-log", apiMiddleware(moduleGate("pim", handlePIMPublishLog)))
	// Import/Export (Stage 15.2)
	http.HandleFunc("POST /api/v1/pim/import/{doctype}/preview", apiMiddleware(moduleGate("pim", handlePIMImportPreview)))
	http.HandleFunc("GET /api/v1/pim/import-jobs/{id}/errors.csv", apiMiddleware(moduleGate("pim", handlePIMImportJobErrors)))
	// Import depth (Stage 36.3): reusable column-mapping templates, driven
	// either by a scheduled directory scan or an inbound webhook token.
	http.HandleFunc("GET /api/v1/pim/import-templates", apiMiddleware(moduleGate("pim", handlePIMImportTemplates)))
	http.HandleFunc("GET /api/v1/pim/import-templates/{id}/preview-mapping", apiMiddleware(moduleGate("pim", handlePIMImportTemplatePreviewMapping)))
	http.HandleFunc("POST /api/v1/pim/import-templates/{id}/preview", apiMiddleware(moduleGate("pim", handlePIMImportTemplateRun(true))))
	http.HandleFunc("POST /api/v1/pim/import-templates/{id}/import", apiMiddleware(moduleGate("pim", handlePIMImportTemplateRun(false))))
	http.HandleFunc("GET /api/v1/pim/import-schedules", apiMiddleware(moduleGate("pim", handlePIMImportSchedules)))
	http.HandleFunc("POST /api/v1/pim/import-schedules/{id}/rotate-hook-token", apiMiddleware(moduleGate("pim", handlePIMImportScheduleRotateHookToken)))
	// Public: listed in middleware.go's publicRoutes. An external system
	// authenticates with X-Hook-Token alone (verified inside the handler),
	// never a session - see handlePIMImportHook's own comment.
	http.HandleFunc("POST /api/v1/pim/import/hook", apiMiddleware(moduleGate("pim", handlePIMImportHook)))
	// Export & syndication depth (Stage 36.4): template create/list/edit use
	// the generic document API like every PIM master; running one is the one
	// action that needs logic the generic endpoint doesn't have.
	http.HandleFunc("POST /api/v1/pim/export-templates/{id}/run", apiMiddleware(moduleGate("pim", handlePIMExportTemplateRun)))
	http.HandleFunc("POST /api/v1/pim/catalogs/{id}/rotate-share-token", apiMiddleware(moduleGate("pim", handlePIMCatalogRotateShareToken)))
	// Public: listed in middleware.go's publicRoutes. A partner holds only
	// the share token (as ?token=, verified inside the handler), never a
	// session - see handlePIMCatalogShare's own comment.
	http.HandleFunc("GET /api/v1/pim/catalog-share", apiMiddleware(moduleGate("pim", handlePIMCatalogShare)))
	// Enrichment & quality (Stage 36.7): related products, UPC/EAN generation.
	http.HandleFunc("GET /api/v1/pim/content-assist-shapes", apiMiddleware(moduleGate("pim", handlePIMContentAssistShapes)))
	http.HandleFunc("GET /api/v1/pim/related-products/{itemCode}", apiMiddleware(moduleGate("pim", handlePIMRelatedProducts)))
	http.HandleFunc("POST /api/v1/pim/barcode/generate", apiMiddleware(moduleGate("pim", handlePIMGenerateBarcode)))
	http.HandleFunc("POST /api/v1/pim/translations/seed", apiMiddleware(moduleGate("pim", handlePIMSeedTranslations)))
	// Bulk channel download (36.4.5): pull selected items' live channel
	// state back for a connector that supports it (see engines.ChannelReader).
	http.HandleFunc("POST /api/v1/pim/channels/{code}/pull-state", apiMiddleware(moduleGate("pim", handlePIMChannelPullState)))
	// Real Channel Connector Framework (Stage 16.1) - write-only credential
	// endpoint, HR/Admin only; there is deliberately no matching GET.
	http.HandleFunc("POST /api/v1/pim/channels/{code}/credentials", apiMiddleware(moduleGate("pim", handleSaveChannelCredential)))
	// Taxonomy versioning (Stage 26.4.3) - reads the existing audit_logs trail.
	http.HandleFunc("GET /api/v1/pim/taxonomy-history/{doctype}/{id}", apiMiddleware(moduleGate("pim", handlePIMTaxonomyHistory)))
	// Search/discovery feed export (Stage 26.4.9)
	http.HandleFunc("GET /api/v1/pim/search-feed.csv", apiMiddleware(moduleGate("pim", handlePIMSearchFeedExport)))
	// Content workflow: version history + rollback (Stage 26.4.6)
	http.HandleFunc("GET /api/v1/pim/content/{id}/versions", apiMiddleware(moduleGate("pim", handlePIMContentVersions)))
	http.HandleFunc("POST /api/v1/pim/content/{id}/rollback", apiMiddleware(moduleGate("pim", handlePIMContentRollback)))
	// moduleGate("oms",...) added Stage 27 - this webhook had no gate of any
	// kind before (not even the older feature-flag system).
	http.HandleFunc("POST /api/v1/integration/bigcommerce/webhook/{channelCode}", apiMiddleware(moduleGate("oms", handleBigCommerceWebhook)))

	// Shopify Integration Webhook APIs. moduleGate("oms",...) (Stage 27) now
	// stacks alongside the pre-existing featureGate("oms_integration",...) -
	// the module gate answers "did this tenant buy the OMS product," the
	// feature flag keeps answering its own narrower "is this specific
	// integration configured" question (see middleware.go's moduleGate/
	// featureGate doc comments for why both stay).
	http.HandleFunc("POST /api/v1/integration/shopify/product/map", apiMiddleware(moduleGate("oms", featureGate("oms_integration", handleShopifyProductMap))))
	http.HandleFunc("POST /api/v1/integration/shopify/order", apiMiddleware(moduleGate("oms", featureGate("oms_integration", handleShopifyOrderWebhook))))

	// Store Fulfillment & Returns APIs. moduleGate("wms",...) (Stage 27) now
	// stacks alongside the pre-existing featureGate("wms_integration",...),
	// same reasoning as the Shopify routes above.
	http.HandleFunc("POST /api/v1/fulfillment/task/transition", apiMiddleware(moduleGate("wms", featureGate("wms_integration", handleFulfillmentTaskTransition))))
	http.HandleFunc("POST /api/v1/fulfillment/return", apiMiddleware(moduleGate("wms", featureGate("wms_integration", handleFulfillmentReturn))))

	// Transfer-order lifecycle (Stage 17.6)
	http.HandleFunc("POST /api/v1/transfer/dispatch", apiMiddleware(moduleGate("inventory", handleDispatchTransferOrder)))
	http.HandleFunc("POST /api/v1/transfer/receive", apiMiddleware(moduleGate("inventory", handleReceiveTransferOrder)))

	// Purchase requisition conversion (Stage 17.7)
	http.HandleFunc("POST /api/v1/procurement/convert-requisition", apiMiddleware(moduleGate("procurement", handleConvertRequisition)))

	// Purchase Order lines: live pricing preview, the printed vendor copy, and
	// dispatch to the vendor (Stage 40.1). All three keep HSN/GST resolution
	// and the inter-state decision server-side - see handlers_purchase_order.go.
	http.HandleFunc("POST /api/v1/procurement/purchase-order/preview", apiMiddleware(moduleGate("procurement", handlePreviewPurchaseOrder)))
	http.HandleFunc("GET /api/v1/procurement/purchase-order/{id}/print", apiMiddleware(moduleGate("procurement", handlePrintPurchaseOrder)))
	http.HandleFunc("POST /api/v1/procurement/purchase-order/{id}/send", apiMiddleware(moduleGate("procurement", handleSendPurchaseOrder)))

	// Vendor invoice 3-way match + payment (Stage 17.8)
	http.HandleFunc("POST /api/v1/procurement/vendor-invoice/match", apiMiddleware(moduleGate("procurement", handleMatchVendorInvoice)))
	http.HandleFunc("POST /api/v1/procurement/vendor-invoice/pay", apiMiddleware(moduleGate("procurement", handlePayVendorInvoice)))

	// Administration Scale Test APIs
	http.HandleFunc("POST /api/v1/admin/scale-test", apiMiddleware(handleScaleTest))

	// Marketplace & Logistics Integration APIs. moduleGate("oms",...) (Stage
	// 27) stacks alongside the pre-existing featureGate("oms_integration",...).
	http.HandleFunc("POST /api/v1/marketplace/settlement/reconcile", apiMiddleware(moduleGate("oms", featureGate("oms_integration", handleMarketplaceReconcile))))
	http.HandleFunc("POST /api/v1/marketplace/logistics/book", apiMiddleware(moduleGate("oms", featureGate("oms_integration", handleLogisticsBook))))
	// Stage 26.12.4 (Courier/Shipment/Manifest): serviceability preview,
	// manifest generation/handover, tracking sync, RTO, and label printing -
	// same moduleGate/featureGate pair as the pre-existing logistics/book
	// route above, since this is the same Shipment engine.
	http.HandleFunc("GET /api/v1/marketplace/logistics/serviceability", apiMiddleware(moduleGate("oms", featureGate("oms_integration", handleCourierServiceability))))
	http.HandleFunc("POST /api/v1/marketplace/logistics/manifest", apiMiddleware(moduleGate("oms", featureGate("oms_integration", handleGenerateManifest))))
	http.HandleFunc("POST /api/v1/marketplace/logistics/manifest/handover", apiMiddleware(moduleGate("oms", featureGate("oms_integration", handleHandoverManifest))))
	http.HandleFunc("POST /api/v1/marketplace/logistics/tracking", apiMiddleware(moduleGate("oms", featureGate("oms_integration", handleShipmentTracking))))
	http.HandleFunc("POST /api/v1/marketplace/logistics/rto", apiMiddleware(moduleGate("oms", featureGate("oms_integration", handleShipmentRTO))))
	http.HandleFunc("GET /api/v1/marketplace/logistics/label", apiMiddleware(moduleGate("oms", featureGate("oms_integration", handleShippingLabel))))
	// Stage 35.5: encrypted courier credentials, real provider AWB/pickup/
	// cancellation calls, rate shopping, a vector Code128 PDF label, signed
	// tracking intake and NDR resolution. The provider is path-scoped so a
	// new adapter adds no new route family.
	http.HandleFunc("POST /api/v1/marketplace/couriers/{provider}/credentials", apiMiddleware(moduleGate("oms", featureGate("oms_integration", handleCourierCredentials))))
	http.HandleFunc("GET /api/v1/marketplace/couriers/{provider}/credentials", apiMiddleware(moduleGate("oms", featureGate("oms_integration", handleCourierCredentials))))
	http.HandleFunc("POST /api/v1/marketplace/couriers/{provider}/awb", apiMiddleware(moduleGate("oms", featureGate("oms_integration", handleCourierAllocateAWB))))
	http.HandleFunc("POST /api/v1/marketplace/couriers/{provider}/pickup", apiMiddleware(moduleGate("oms", featureGate("oms_integration", handleCourierPickup))))
	http.HandleFunc("POST /api/v1/marketplace/couriers/{provider}/cancel", apiMiddleware(moduleGate("oms", featureGate("oms_integration", handleCourierCancel))))
	http.HandleFunc("GET /api/v1/marketplace/couriers/rates", apiMiddleware(moduleGate("oms", featureGate("oms_integration", handleCourierRates))))
	http.HandleFunc("POST /api/v1/integration/courier/{provider}/tracking", apiMiddleware(moduleGate("oms", featureGate("oms_integration", handleCourierTrackingWebhook))))
	http.HandleFunc("POST /api/v1/marketplace/ndr/{id}/resolve", apiMiddleware(moduleGate("oms", featureGate("oms_integration", handleNDRResolve))))
	http.HandleFunc("GET /api/v1/marketplace/logistics/label.pdf", apiMiddleware(moduleGate("oms", featureGate("oms_integration", handleShippingLabelPDF))))
	// Stage 35.6: the full connector SDK and its operational surface.
	http.HandleFunc("GET /api/v1/marketplace/connectors", apiMiddleware(moduleGate("oms", featureGate("oms_integration", handleConnectorDescriptors))))
	http.HandleFunc("GET /api/v1/marketplace/channels/{channel}/credentials", apiMiddleware(moduleGate("oms", featureGate("oms_integration", handleChannelConnectorCredentials))))
	http.HandleFunc("POST /api/v1/marketplace/channels/{channel}/credentials", apiMiddleware(moduleGate("oms", featureGate("oms_integration", handleChannelConnectorCredentials))))
	http.HandleFunc("POST /api/v1/marketplace/channels/{channel}/pull-orders", apiMiddleware(moduleGate("oms", featureGate("oms_integration", handleChannelOrderPull))))
	http.HandleFunc("POST /api/v1/marketplace/channels/{channel}/sync-inventory", apiMiddleware(moduleGate("oms", featureGate("oms_integration", handleChannelInventorySync))))
	http.HandleFunc("POST /api/v1/marketplace/channels/{channel}/push-status", apiMiddleware(moduleGate("oms", featureGate("oms_integration", handleChannelStatusPush))))
	http.HandleFunc("GET /api/v1/marketplace/sku-mappings", apiMiddleware(moduleGate("oms", featureGate("oms_integration", handleChannelSKUMappings))))
	http.HandleFunc("POST /api/v1/marketplace/sku-mappings", apiMiddleware(moduleGate("oms", featureGate("oms_integration", handleChannelSKUMappings))))
	http.HandleFunc("GET /api/v1/marketplace/sku-exceptions", apiMiddleware(moduleGate("oms", featureGate("oms_integration", handleChannelSKUExceptions))))
	http.HandleFunc("GET /api/v1/marketplace/connectors/health", apiMiddleware(moduleGate("oms", featureGate("oms_integration", handleConnectorHealth))))
	// Stage 35.7: virtual-bundle availability and stocked-kit warehouse moves.
	http.HandleFunc("GET /api/v1/oms/bundles/{sku}/availability", apiMiddleware(moduleGate("oms", handleBundleAvailability)))
	http.HandleFunc("POST /api/v1/wms/bundles/{operation}", apiMiddleware(moduleGate("wms", handleBundleAssembly)))

	// Optimization & Advanced Forecasting APIs (gated by the "advanced_forecasting" flag)
	http.HandleFunc("GET /api/v1/optimization/replenishment-suggestions", apiMiddleware(featureGate("advanced_forecasting", handleReplenishmentSuggestions)))
	http.HandleFunc("GET /api/v1/optimization/sla-breaches", apiMiddleware(featureGate("advanced_forecasting", handleSLABreaches)))
	http.HandleFunc("POST /api/v1/optimization/forecast", apiMiddleware(featureGate("advanced_forecasting", handleDemandForecast)))

	// Stage 9.1: Unicommerce Integration APIs. moduleGate("oms",...) added
	// Stage 27 - this entire surface had no gate of any kind before.
	http.HandleFunc("POST /api/v1/unicommerce/credentials", apiMiddleware(moduleGate("oms", handleUnicommerceCredentials)))
	http.HandleFunc("GET /api/v1/unicommerce/credentials", apiMiddleware(moduleGate("oms", handleGetUnicommerceCredentials)))
	http.HandleFunc("POST /api/v1/unicommerce/order", apiMiddleware(moduleGate("oms", handleUnicommerceOrder)))
	http.HandleFunc("GET /api/v1/unicommerce/orders", apiMiddleware(moduleGate("oms", handleListUnicommerceOrders)))
	http.HandleFunc("GET /api/v1/unicommerce/inventory-syncs", apiMiddleware(moduleGate("oms", handleListUnicommerceInventorySyncs)))

	// Stage 9.1: Pine Labs Plutus Integration APIs
	http.HandleFunc("POST /api/v1/pinelabs/credentials", apiMiddleware(handlePineLabsCredentials))
	http.HandleFunc("GET /api/v1/pinelabs/credentials", apiMiddleware(handleGetPineLabsCredentials))
	http.HandleFunc("POST /api/v1/pinelabs/transaction", apiMiddleware(handlePineLabsTransaction))
	http.HandleFunc("POST /api/v1/pinelabs/reconcile", apiMiddleware(handlePineLabsReconcile))
	http.HandleFunc("GET /api/v1/pinelabs/transactions", apiMiddleware(handleListPineLabsTransactions))

	// Stage 9.1: CleverTap Integration APIs
	http.HandleFunc("POST /api/v1/clevertap/credentials", apiMiddleware(handleCleverTapCredentials))
	http.HandleFunc("GET /api/v1/clevertap/credentials", apiMiddleware(handleGetCleverTapCredentials))
	http.HandleFunc("GET /api/v1/clevertap/logs", apiMiddleware(handleListCleverTapLogs))

	// Integration Logs and Retry APIs
	http.HandleFunc("GET /api/v1/integration/logs", apiMiddleware(handleGetIntegrationLogs))
	http.HandleFunc("POST /api/v1/integration/retry", apiMiddleware(handleRetryIntegrationEvent))

	// Tenant Provisioning and SaaS Control APIs
	http.HandleFunc("POST /api/v1/admin/tenant/provision", apiMiddleware(handleProvisionTenant))
	http.HandleFunc("POST /api/v1/admin/tenant/feature-flag", apiMiddleware(handleSetFeatureFlag))
	// Stage 26.1.4: tenant registry list, for the entitlement admin screen's tenant picker.
	http.HandleFunc("GET /api/v1/admin/tenants", apiMiddleware(handleListTenants))
	// Stage 26.1.5: tenant usage/health dashboard (live concurrency usage + configured limits, per tenant).
	http.HandleFunc("GET /api/v1/admin/tenant-usage", apiMiddleware(handleTenantUsage))

	// Module Registry / Per-Tenant Module Entitlements (Stage 14.1)
	http.HandleFunc("GET /api/v1/admin/modules", apiMiddleware(handleListModules))
	http.HandleFunc("GET /api/v1/admin/tenant/module-entitlements", apiMiddleware(handleGetModuleEntitlements))
	http.HandleFunc("POST /api/v1/admin/tenant/module-entitlement", apiMiddleware(handleSetModuleEntitlement))
	// Product Package catalog + "apply a plan to a tenant" (Stage 26.1.4,
	// reuses Stage 27's engines.ProductPackages/ApplyPackageSelection)
	http.HandleFunc("GET /api/v1/admin/packages", apiMiddleware(handleListProductPackages))
	http.HandleFunc("POST /api/v1/admin/tenant/package", apiMiddleware(handleSetTenantPackage))
	// Stage 44.11: the write path for a tenant's own hostname. Stage 44
	// shipped public.tenants.host_slug with no way to set it short of raw SQL
	// against production, which is not an operator workflow.
	http.HandleFunc("POST /api/v1/admin/tenant/host-slug", apiMiddleware(handleSetTenantHostSlug))

	// Per-Tenant Version Record (Stage 14.6)
	http.HandleFunc("GET /api/v1/admin/tenant/version", apiMiddleware(handleGetTenantVersion))

	// Patch/Bug-Intake Proposals (Stage 14.13-14.16) - a triage queue and
	// decision audit trail, not an auto-executor; see engines/patchintake.go.
	http.HandleFunc("GET /api/v1/admin/patch/proposals", apiMiddleware(handleListPatchProposals))
	http.HandleFunc("POST /api/v1/admin/patch/approve", apiMiddleware(handleApprovePatchProposal))
	http.HandleFunc("POST /api/v1/admin/patch/reject", apiMiddleware(handleRejectPatchProposal))

	// 3rd-Party Extension Isolation (Stage 14.17-14.20) - out-of-process
	// webhook hooks + scoped tokens; see engines/extensions.go.
	http.HandleFunc("POST /api/v1/admin/extension/hooks", apiMiddleware(handleCreateExtensionHook))
	http.HandleFunc("GET /api/v1/admin/extension/hooks", apiMiddleware(handleListExtensionHooks))
	http.HandleFunc("DELETE /api/v1/admin/extension/hooks/{id}", apiMiddleware(handleDeleteExtensionHook))
	http.HandleFunc("GET /api/v1/admin/extension/hooks/{id}/log", apiMiddleware(handleGetExtensionHookLog))
	http.HandleFunc("POST /api/v1/admin/extension/token", apiMiddleware(handleIssueExtensionToken))

	// Stage 38.2: durable public-API credentials. Administration uses a
	// normal human Super Admin session; the generated key is never accepted
	// by apiMiddleware and therefore cannot become a user session by accident.
	http.HandleFunc("POST /api/v1/admin/api-credentials", apiMiddleware(handleIssueAPICredential))
	http.HandleFunc("GET /api/v1/admin/api-credentials", apiMiddleware(handleListAPICredentials))
	http.HandleFunc("POST /api/v1/admin/api-credentials/{id}/rotate", apiMiddleware(handleRotateAPICredential))
	http.HandleFunc("DELETE /api/v1/admin/api-credentials/{id}", apiMiddleware(handleRevokeAPICredential))
	// Stage 38.3/38.9 administration: per-credential budgets and the traffic
	// log. Both stay behind the existing Super Admin human-session gate - an
	// integration key can never read or change its own limits.
	http.HandleFunc("PUT /api/v1/admin/api-credentials/{id}/limits", apiMiddleware(handleSetAPICredentialLimits))
	http.HandleFunc("GET /api/v1/admin/api-credentials/{id}/traffic", apiMiddleware(handleListAPICredentialTraffic))
	http.HandleFunc("GET /api/v1/admin/api-traffic", apiMiddleware(handleListAPICredentialTraffic))
	// Stage 38.8: the OpenAPI document, generated from the public route table
	// on every request so it can never be stale.
	http.HandleFunc("GET /api/v1/admin/public-api/openapi.json", apiMiddleware(handlePublicAPIOpenAPISpec))

	// Stage 39.4/39.6: the Knowledge Center. The generated content is embedded
	// in the binary and has no copy under public/, so these handlers are the
	// only way to read an article - "authenticated by default" is a property of
	// where the content lives, not a check that could be forgotten.
	http.HandleFunc("GET /api/v1/help/index", apiMiddleware(handleHelpIndex))
	http.HandleFunc("GET /api/v1/help/search-index", apiMiddleware(handleHelpSearchIndex))
	http.HandleFunc("GET /api/v1/help/article/{slug}", apiMiddleware(handleHelpArticle))
	// The unauthenticated subset: only articles marked `public: true`.
	http.HandleFunc("GET /api/v1/help/public/index", apiMiddleware(handleHelpPublicIndex))
	http.HandleFunc("GET /api/v1/help/public/search-index", apiMiddleware(handleHelpPublicSearchIndex))
	http.HandleFunc("GET /api/v1/help/public/article", apiMiddleware(handleHelpPublicArticle))

	// Stage 38.1: the curated public API. These are the ONLY routes an
	// integration key can reach - every one of them names its required scope
	// explicitly, and publicAPIMiddleware panics at registration for a route
	// that does not, so "public by accident" is not a reachable state.
	//
	// Read-only by design in this first slice. Writes wait until each specific
	// mutation has been curated on its own terms; the idempotency spine (38.5)
	// is already in place under this middleware for when they land.
	registerPublicAPIV1Routes()

	// DocType Metadata APIs
	http.HandleFunc("GET /api/v1/doc/{doctype}/meta", apiMiddleware(handleGetDocTypeMeta))
	// Field-format specs (Stage 40.2). Read once at boot by the frontend so
	// the input hints, placeholders and keystroke filtering are driven by the
	// same declarations the server validates against - one list, not two.
	http.HandleFunc("GET /api/v1/meta/field-formats", apiMiddleware(handleFieldFormats))
	http.HandleFunc("GET /api/v1/meta/doctypes", apiMiddleware(handleGetDocTypes))
	http.HandleFunc("POST /api/v1/meta/doctypes", apiMiddleware(handleSaveDocType))
	http.HandleFunc("POST /api/v1/meta/{doctype}/fields", apiMiddleware(handleSaveFieldDefinition))
	http.HandleFunc("PUT /api/v1/meta/{doctype}/fields/{id}", apiMiddleware(handleUpdateFieldDefinition))
	http.HandleFunc("DELETE /api/v1/meta/{doctype}/fields/{id}", apiMiddleware(handleDeleteFieldDefinition))

	// Core Foundation APIs
	http.HandleFunc("/api/v1/labels", apiMiddleware(handleLabels))
	http.HandleFunc("/api/v1/sequence", apiMiddleware(handleSequence))
	http.HandleFunc("/api/v1/prefix", apiMiddleware(handlePrefix))
	http.HandleFunc("/api/v1/logs/audit", apiMiddleware(handleAuditLogs))
	http.HandleFunc("GET /api/v1/admin/audit-logs/verify", apiMiddleware(handleVerifyAuditLogChain))

	// Industry Configuration & Preset Profiler
	http.HandleFunc("GET /api/v1/admin/industries", apiMiddleware(handleGetIndustries))
	http.HandleFunc("POST /api/v1/admin/industry", apiMiddleware(handleSwitchIndustry))

	// Bulk CSV Import
	http.HandleFunc("POST /api/v1/import/{doctype}", apiMiddleware(handleBulkImport))
	http.HandleFunc("GET /api/v1/import/{doctype}/template", apiMiddleware(handleGetImportTemplate))
	http.HandleFunc("/api/v1/logs/system", apiMiddleware(handleSystemLogs))
	// 24.16: the deliberate-panic test endpoint is dev/test tooling only -
	// registered only outside production, matching the ENV-driven pattern
	// environments.json/promote.ps1 (Stage 14) already established.
	if os.Getenv("ENV") != "production" {
		http.HandleFunc("/api/v1/debug/panic", apiMiddleware(handleDebugPanic))
	}

	// Product-namespace SPA fallback (Stage 27): the frontend is one static
	// SPA with no server-rendered pages, so a direct request to a product
	// URL like /wms or /pims would otherwise 404 against the plain
	// http.FileServer below (no file by that name exists in public/). This
	// serves the same index.html for every known product prefix so a client
	// can be handed (or bookmark) a URL like xyzerp.com/wms directly - the
	// frontend then reads location.pathname on boot to decide which
	// product's nav to show (see public/app.js's boot sequence). Driven
	// entirely by engines.ProductPackages, so a future product added there
	// gets a working URL with no route-table edit needed here. This is
	// purely a navigation convenience - actual access control is still
	// enforced server-side by moduleGate on every API route, never by the
	// URL itself.
	spaShell := func(w http.ResponseWriter, r *http.Request) { http.ServeFile(w, r, "./public/index.html") }
	for _, pkg := range engines.ProductPackages {
		prefix := pkg.URLPrefix
		http.HandleFunc("GET "+prefix, spaShell)
		http.HandleFunc("GET "+prefix+"/{rest...}", spaShell)
	}
	// Stage 39.4: /help and /help/<slug> are the Knowledge Center's own URLs,
	// served by the same SPA shell so an article link can be shared, bookmarked
	// or opened cold. The shell then fetches the article through the
	// authenticated help API - the URL carries no content by itself.
	http.HandleFunc("GET /help", spaShell)
	http.HandleFunc("GET /help/{rest...}", spaShell)

	// Serve Static Files
	fs := http.FileServer(http.Dir("./public"))
	http.Handle("/", fs)

	// Stage 14.9: PORT is what lets dev/test/live (and any throwaway
	// verification instance) run the exact same binary side by side on one
	// machine. Defaults to 8080 so every existing deployment/doc/script that
	// assumes the old hardcoded port keeps working unchanged.
	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}

	// Stage 28.4: HOST binds the listener to a specific interface. Empty (the
	// default) keeps the historical all-interfaces bind (":"+port); behind Caddy
	// (deploy/Caddyfile) set HOST=127.0.0.1 so only the local reverse proxy can
	// reach the Go process and nothing hits it directly from the network.
	host := os.Getenv("HOST")

	// 24.13: explicit timeouts instead of the bare ListenAndServe's
	// unbounded defaults - a slow/stalled client can no longer hold a
	// connection (and the goroutine/DB-conn behind it) open indefinitely.
	// WriteTimeout is generous enough to not clip legitimate long-running
	// report/export requests (Stage 20 Track B.4's async export exists
	// precisely for the ones that would otherwise be too slow for this).
	// Stage 40.4: compression and static caching wrap the whole mux, outside
	// securityHeaders, so every response - static asset and API alike - gets
	// them on every access path (the SSH tunnel straight to this port as much
	// as the Caddy-proxied one). Order matters: staticAssetCache sets headers
	// before the handler runs, compressResponses must see the Content-Type the
	// handler sets, and securityHeaders stays innermost so its headers are on
	// the response either way.
	//
	// Stage 44.10: tenantHostGate sits inside securityHeaders and outside the
	// mux. Inside, so its 404 still carries the security headers; outside the
	// mux, so it covers the SPA shell and the static files too - a browser
	// asks for those first, and they never pass through apiMiddleware. It is
	// inert unless TENANT_BASE_DOMAIN is set.
	srv := &http.Server{
		Addr:         host + ":" + port,
		Handler:      staticAssetCache(compressResponses(securityHeaders(tenantHostGate(http.DefaultServeMux)))),
		ReadTimeout:  30 * time.Second,
		WriteTimeout: 60 * time.Second,
		IdleTimeout:  120 * time.Second,
	}

	// 24.15: graceful shutdown - SIGINT/SIGTERM stops accepting new
	// connections, lets in-flight requests finish (bounded by the context
	// timeout below), and tells every background worker to stop, instead of
	// a process kill dropping everything mid-flight with no warning.
	shutdownComplete := make(chan struct{})
	go func() {
		sigCh := make(chan os.Signal, 1)
		signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
		sig := <-sigCh
		log.Printf("Received %v, shutting down gracefully...", sig)

		cancelWorkers()

		shutdownCtx, cancelShutdown := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancelShutdown()
		if err := srv.Shutdown(shutdownCtx); err != nil {
			log.Printf("Graceful shutdown did not complete cleanly: %v", err)
		}
		close(shutdownComplete)
	}()

	log.Printf("Starting ERP Server on http://localhost:%s\n", port)
	if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		log.Fatalf("Server failed to start: %v", err)
	}
	<-shutdownComplete
	log.Println("Server stopped.")
}

// REST HANDLERS
