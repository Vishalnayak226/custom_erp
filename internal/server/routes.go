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
	connStr := os.Getenv("DATABASE_URL")
	if connStr == "" {
		connStr = "postgres://postgres@localhost:5435/custom_erp?sslmode=disable"
	}
	db.InitDB(connStr)

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

	// Start Stage 9.1 Integration Workers (Unicommerce, Pine Labs, CleverTap)
	engines.StartUnicommerceWorker(workerCtx, 30*time.Second)
	engines.StartPineLabsReconciliationWorker(workerCtx, 5*time.Minute)
	engines.StartCleverTapWorker(workerCtx, 30*time.Second)

	// Start Report Export Worker (Stage 20.37) - polls for Pending
	// ReportExportJob documents and runs them in the background.
	engines.StartReportExportWorker(workerCtx, 10*time.Second)

	// Authentication API
	http.HandleFunc("POST /api/v1/login", apiMiddleware(handleLogin))

	// Version (Stage 14.6) - public, same tier as /login, so a client/ops
	// tool can check what build is running without authenticating first.
	http.HandleFunc("GET /api/v1/version", apiMiddleware(handleVersion))
	// Health check (24.14) - for a load balancer/process supervisor to poll;
	// same public tier as /version, no bearer token required.
	http.HandleFunc("GET /api/v1/health", apiMiddleware(handleHealth))
	http.HandleFunc("POST /api/v1/auth/mfa/enroll", apiMiddleware(handleMFAEnroll))
	http.HandleFunc("POST /api/v1/auth/mfa/activate", apiMiddleware(handleMFAActivate))
	http.HandleFunc("POST /api/v1/auth/mfa/verify", apiMiddleware(handleMFAVerify))

	// Self-service User Profile (Stage 21)
	http.HandleFunc("GET /api/v1/me", apiMiddleware(handleGetProfile))
	http.HandleFunc("PUT /api/v1/me", apiMiddleware(handleUpdateProfile))
	http.HandleFunc("POST /api/v1/me/change-password", apiMiddleware(handleChangePassword))

	// Admin user/role management (Stage 21 QA fix) - the Users/Roles sidebar
	// items had never had a backend at all. HR/Admin-only, enforced in each handler.
	http.HandleFunc("GET /api/v1/admin/users", apiMiddleware(handleListUsers))
	http.HandleFunc("POST /api/v1/admin/users", apiMiddleware(handleCreateUser))
	http.HandleFunc("POST /api/v1/admin/users/status", apiMiddleware(handleSetUserStatus))
	http.HandleFunc("POST /api/v1/admin/users/location", apiMiddleware(handleSetUserLocation))
	http.HandleFunc("GET /api/v1/admin/roles", apiMiddleware(handleListRoles))
	http.HandleFunc("GET /api/v1/admin/role-permissions", apiMiddleware(handleRolePermissions))
	http.HandleFunc("POST /api/v1/admin/role-permissions", apiMiddleware(handleRolePermissions))

	// Generic DocType CRUD APIs (Go 1.22 enhanced routing)
	http.HandleFunc("/api/v1/doc/{doctype}", apiMiddleware(handleGenericDoc))
	http.HandleFunc("/api/v1/doc/{doctype}/{id}", apiMiddleware(handleGenericDoc))
	http.HandleFunc("POST /api/v1/doc/{doctype}/{id}/reactivate", apiMiddleware(handleReactivateMasterDocument))

	// Availability & Reservation APIs
	http.HandleFunc("GET /api/v1/availability", apiMiddleware(handleGetAvailability))
	http.HandleFunc("POST /api/v1/reserve", apiMiddleware(handleCreateReservation))

	// WMS Maturity (Stage 20 Track B.2): putaway, bin-grouped pick lists,
	// transfer-order pack/box-mapping, cycle-count reconciliation
	http.HandleFunc("POST /api/v1/wms/putaway", apiMiddleware(handlePutaway))
	http.HandleFunc("GET /api/v1/wms/pick-list", apiMiddleware(handlePickList))
	http.HandleFunc("POST /api/v1/wms/condition-transition", apiMiddleware(handleBinConditionTransition))
	http.HandleFunc("POST /api/v1/wms/transfer/pack", apiMiddleware(handlePackTransferOrder))
	http.HandleFunc("POST /api/v1/wms/cycle-count/reconcile", apiMiddleware(handleReconcileCycleCount))

	// Checkout & Finance APIs
	http.HandleFunc("POST /api/v1/checkout", apiMiddleware(handleCheckout))
	http.HandleFunc("POST /api/v1/pos/session/open", apiMiddleware(handlePOSSessionOpen))
	http.HandleFunc("POST /api/v1/pos/session/close", apiMiddleware(handlePOSSessionClose))
	http.HandleFunc("GET /api/v1/pos/session/current", apiMiddleware(handlePOSSessionCurrent))
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
	http.HandleFunc("POST /api/v1/procurement/vendor-invoice/pay-with-tds", apiMiddleware(handlePayVendorInvoiceWithTDS))
	http.HandleFunc("POST /api/v1/finance/debit-note/{id}/post", apiMiddleware(handlePostDebitNote))
	http.HandleFunc("POST /api/v1/finance/credit-note/{id}/post", apiMiddleware(handlePostCreditNote))
	http.HandleFunc("POST /api/v1/finance/sales-invoice/{id}/post", apiMiddleware(handlePostSalesInvoice))
	http.HandleFunc("POST /api/v1/finance/sales-invoice/{id}/settle", apiMiddleware(handleSettleSalesInvoice))

	// Approval / Workflow Engine (maker-checker)
	http.HandleFunc("POST /api/v1/approval/submit", apiMiddleware(handleSubmitApproval))
	http.HandleFunc("POST /api/v1/approval/decide", apiMiddleware(handleDecideApproval))
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

	// HR Foundation (Stage 14.1: module-gated - "hr")
	http.HandleFunc("GET /api/v1/hr/payroll-export", apiMiddleware(moduleGate("hr", handlePayrollExport)))

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

	// Manufacturing (Stage 14.1: module-gated - "manufacturing")
	http.HandleFunc("POST /api/v1/manufacturing/issue-material", apiMiddleware(moduleGate("manufacturing", handleIssueProductionMaterial)))
	http.HandleFunc("POST /api/v1/manufacturing/complete", apiMiddleware(moduleGate("manufacturing", handleCompleteProductionOrder)))

	// PIM Foundation MVP (Stage 15: module-gated - "pim")
	// Dashboard (Stage 16.5a) reads the existing PIM snapshot/queue state;
	// it is a fixed PIM route, so module gating happens at registration.
	http.HandleFunc("GET /api/v1/pim/dashboard", apiMiddleware(moduleGate("pim", handlePIMDashboard)))
	// Bulk edit (Stage 16.5b) is deliberately a PIM-only endpoint; its
	// handler additionally applies the target doctype's normal update RBAC.
	http.HandleFunc("POST /api/v1/pim/bulk-edit", apiMiddleware(moduleGate("pim", handlePIMBulkEdit)))
	http.HandleFunc("GET /api/v1/pim/reports/{name}", apiMiddleware(moduleGate("pim", handlePIMReport)))
	http.HandleFunc("GET /api/v1/pim/workbench", apiMiddleware(moduleGate("pim", handlePIMWorkbench)))
	http.HandleFunc("GET /api/v1/pim/completeness/{itemCode}", apiMiddleware(moduleGate("pim", handlePIMCompleteness)))
	// Media Library (Stage 15.2)
	http.HandleFunc("POST /api/v1/pim/media/upload", apiMiddleware(moduleGate("pim", handlePIMMediaUpload)))
	http.HandleFunc("GET /api/v1/pim/media/{id}/file", apiMiddleware(moduleGate("pim", handlePIMMediaFile)))
	http.HandleFunc("GET /api/v1/pim/media", apiMiddleware(moduleGate("pim", handlePIMMediaList)))
	http.HandleFunc("POST /api/v1/pim/media/{id}/deactivate", apiMiddleware(moduleGate("pim", handlePIMMediaDeactivate)))
	// Channel Publishing (Stage 15.2)
	http.HandleFunc("POST /api/v1/pim/publish", apiMiddleware(moduleGate("pim", handlePIMPublish)))
	http.HandleFunc("GET /api/v1/pim/publish/{jobID}", apiMiddleware(moduleGate("pim", handlePIMPublishJobStatus)))
	http.HandleFunc("GET /api/v1/pim/publish-log", apiMiddleware(moduleGate("pim", handlePIMPublishLog)))
	// Import/Export (Stage 15.2)
	http.HandleFunc("POST /api/v1/pim/import/{doctype}/preview", apiMiddleware(moduleGate("pim", handlePIMImportPreview)))
	http.HandleFunc("GET /api/v1/pim/import-jobs/{id}/errors.csv", apiMiddleware(moduleGate("pim", handlePIMImportJobErrors)))
	// Real Channel Connector Framework (Stage 16.1) - write-only credential
	// endpoint, HR/Admin only; there is deliberately no matching GET.
	http.HandleFunc("POST /api/v1/pim/channels/{code}/credentials", apiMiddleware(moduleGate("pim", handleSaveChannelCredential)))
	http.HandleFunc("POST /api/v1/integration/bigcommerce/webhook/{channelCode}", apiMiddleware(handleBigCommerceWebhook))

	// Shopify Integration Webhook APIs (gated by the "oms_integration" flag)
	http.HandleFunc("POST /api/v1/integration/shopify/product/map", apiMiddleware(featureGate("oms_integration", handleShopifyProductMap)))
	http.HandleFunc("POST /api/v1/integration/shopify/order", apiMiddleware(featureGate("oms_integration", handleShopifyOrderWebhook)))

	// Store Fulfillment & Returns APIs (gated by the "wms_integration" flag)
	http.HandleFunc("POST /api/v1/fulfillment/task/transition", apiMiddleware(featureGate("wms_integration", handleFulfillmentTaskTransition)))
	http.HandleFunc("POST /api/v1/fulfillment/return", apiMiddleware(featureGate("wms_integration", handleFulfillmentReturn)))

	// Transfer-order lifecycle (Stage 17.6)
	http.HandleFunc("POST /api/v1/transfer/dispatch", apiMiddleware(moduleGate("inventory", handleDispatchTransferOrder)))
	http.HandleFunc("POST /api/v1/transfer/receive", apiMiddleware(moduleGate("inventory", handleReceiveTransferOrder)))

	// Purchase requisition conversion (Stage 17.7)
	http.HandleFunc("POST /api/v1/procurement/convert-requisition", apiMiddleware(moduleGate("procurement", handleConvertRequisition)))

	// Vendor invoice 3-way match + payment (Stage 17.8)
	http.HandleFunc("POST /api/v1/procurement/vendor-invoice/match", apiMiddleware(moduleGate("procurement", handleMatchVendorInvoice)))
	http.HandleFunc("POST /api/v1/procurement/vendor-invoice/pay", apiMiddleware(moduleGate("procurement", handlePayVendorInvoice)))

	// Administration Scale Test APIs
	http.HandleFunc("POST /api/v1/admin/scale-test", apiMiddleware(handleScaleTest))

	// Marketplace & Logistics Integration APIs (gated by the "oms_integration" flag)
	http.HandleFunc("POST /api/v1/marketplace/settlement/reconcile", apiMiddleware(featureGate("oms_integration", handleMarketplaceReconcile)))
	http.HandleFunc("POST /api/v1/marketplace/logistics/book", apiMiddleware(featureGate("oms_integration", handleLogisticsBook)))

	// Optimization & Advanced Forecasting APIs (gated by the "advanced_forecasting" flag)
	http.HandleFunc("GET /api/v1/optimization/replenishment-suggestions", apiMiddleware(featureGate("advanced_forecasting", handleReplenishmentSuggestions)))
	http.HandleFunc("GET /api/v1/optimization/sla-breaches", apiMiddleware(featureGate("advanced_forecasting", handleSLABreaches)))
	http.HandleFunc("POST /api/v1/optimization/forecast", apiMiddleware(featureGate("advanced_forecasting", handleDemandForecast)))

	// Stage 9.1: Unicommerce Integration APIs
	http.HandleFunc("POST /api/v1/unicommerce/credentials", apiMiddleware(handleUnicommerceCredentials))
	http.HandleFunc("GET /api/v1/unicommerce/credentials", apiMiddleware(handleGetUnicommerceCredentials))
	http.HandleFunc("POST /api/v1/unicommerce/order", apiMiddleware(handleUnicommerceOrder))
	http.HandleFunc("GET /api/v1/unicommerce/orders", apiMiddleware(handleListUnicommerceOrders))
	http.HandleFunc("GET /api/v1/unicommerce/inventory-syncs", apiMiddleware(handleListUnicommerceInventorySyncs))

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

	// Module Registry / Per-Tenant Module Entitlements (Stage 14.1)
	http.HandleFunc("GET /api/v1/admin/modules", apiMiddleware(handleListModules))
	http.HandleFunc("GET /api/v1/admin/tenant/module-entitlements", apiMiddleware(handleGetModuleEntitlements))
	http.HandleFunc("POST /api/v1/admin/tenant/module-entitlement", apiMiddleware(handleSetModuleEntitlement))

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

	// DocType Metadata APIs
	http.HandleFunc("GET /api/v1/doc/{doctype}/meta", apiMiddleware(handleGetDocTypeMeta))
	http.HandleFunc("GET /api/v1/meta/doctypes", apiMiddleware(handleGetDocTypes))
	http.HandleFunc("POST /api/v1/meta/doctypes", apiMiddleware(handleSaveDocType))
	http.HandleFunc("POST /api/v1/meta/{doctype}/fields", apiMiddleware(handleSaveFieldDefinition))
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

	// 24.13: explicit timeouts instead of the bare ListenAndServe's
	// unbounded defaults - a slow/stalled client can no longer hold a
	// connection (and the goroutine/DB-conn behind it) open indefinitely.
	// WriteTimeout is generous enough to not clip legitimate long-running
	// report/export requests (Stage 20 Track B.4's async export exists
	// precisely for the ones that would otherwise be too slow for this).
	srv := &http.Server{
		Addr:         ":" + port,
		Handler:      securityHeaders(http.DefaultServeMux),
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
