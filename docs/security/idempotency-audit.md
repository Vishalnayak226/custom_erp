---
doc_id: DOC-IDEMPOTENCY01
title: Idempotency audit of state-changing API commands
type: reference
status: draft
owner: security-owner
approvers: [documentation-maintainer, security-owner]
audience: [maintainers, security-owner]
applies_to: source release 0.19.0 and later
authority: source
confidentiality: internal
last_verified: 2026-10-29
review_by: 2027-01-27
supersedes: none
superseded_by: none
---

# Idempotency audit of state-changing API commands

**Stage 49.4.7.** Generated from `internal/server/routes.go` and the handler
sources by `IdempotencyAuditMarkdown` (internal/server/idempotency_audit.go).
Do not edit by hand: `TestStage4947IdempotencyAuditIsCurrent` fails when this file
is stale. Regenerate with
`ERP_WRITE_IDEMPOTENCY_AUDIT=1 go test ./internal/server -run TestStage4947IdempotencyAuditIsCurrent`.

Every POST / PUT / PATCH / DELETE route under `/api` is one row. **Key** says
whether a retry of the same command is recognised: a command key (`ClaimCommand`,
Stage 47.3 - the claim is remembered 7 days), the public API's required
`Idempotency-Key` header (Stage 38.2), or a job key (`EnqueueJob` - a repeated
key returns the same job). **n.a.** marks a POST that asks a question (a
search, a preview, a validation, an export) and changes nothing a replay could
double. **Touches** is read from the route's name: money, stock, or only a
document. A money- or stock-moving command with no key is a finding.

What a finding means here: the command has no *explicit* replay key. Most of
these are still safe to retry once, because the record's own state gate refuses
the second posting (an invoice already Posted, a claim already Paid, a period
already Closed answers a refusal, not a second entry - the 132.17 action-only
statuses and `expected_version` on record saves), and a command that creates a
new document each time (a journal voucher, an adjustment) is the case a key
exists for. Each finding is therefore either given a key through
`engines.ClaimCommand` (as checkout, returns and offline sales are) or justified
in the tracker item that closes it; the list below is the open work.

Routes audited: **533**; keyed: **8**; findings (money / stock without a key): **148**.

## Findings

A retry of any of these can repeat its effect. Each is either given a key through the existing command-claim helper, or justified here.

| Route | Handler | Touches |
|---|---|---|
| `ANY /api/v1/loyalty/adjustments` | `handleLoyaltyAdjustments` | stock |
| `POST /api/v1/assets/depreciation/run` | `handleRunAssetDepreciation` | money |
| `POST /api/v1/assets/transfer` | `handleTransferAsset` | stock |
| `POST /api/v1/crm/loyalty-redemption/initiate` | `handleInitiateSecureLoyaltyRedemption` | money |
| `POST /api/v1/crm/voucher-holds/{action}` | `handleCRMVoucherHold` | money |
| `POST /api/v1/crm/voucher/redeem` | `handleRedeemVoucher` | money |
| `POST /api/v1/einvoice/documents/{id}/{action}` | `handleEInvoiceDocumentAction` | money |
| `POST /api/v1/einvoice/profiles` | `handleSaveEInvoiceProfile` | money |
| `POST /api/v1/einvoice/run` | `handleRunEInvoiceWorker` | money |
| `POST /api/v1/expenses/pay` | `handlePayExpenseClaim` | money |
| `POST /api/v1/finance/bank-reconcile` | `handleBankReconcile` | money |
| `POST /api/v1/finance/bank-reconciliation/lines/{id}/{action}` | `handleBankLineAction` | money |
| `POST /api/v1/finance/bank-reconciliation/rule-match` | `handleBankRuleMatch` | money |
| `POST /api/v1/finance/credit-note/{id}/post` | `handlePostCreditNote` | money, stock |
| `POST /api/v1/finance/debit-note/{id}/post` | `handlePostDebitNote` | stock |
| `POST /api/v1/finance/fx-revaluation` | `handleFXRevaluation` | money |
| `POST /api/v1/finance/journal-voucher` | `handleCreateJournalVoucher` | money |
| `POST /api/v1/finance/journal-voucher/recurring` | `handleCreateRecurringJournalTemplate` | money |
| `POST /api/v1/finance/journal-voucher/{id}/retry-post` | `handleRetryPostJournalVoucher` | money |
| `POST /api/v1/finance/journal-voucher/{id}/reverse` | `handleReverseJournalVoucher` | money |
| `POST /api/v1/finance/landed-cost-voucher` | `handleCreateLandedCostVoucher` | money |
| `POST /api/v1/finance/landed-cost-voucher/{id}/apply` | `handleApplyLandedCostVoucher` | money |
| `POST /api/v1/finance/payment-proposal` | `handlePaymentProposal` | money |
| `POST /api/v1/finance/payment-proposal/{id}/execute` | `handleExecutePaymentProposal` | money |
| `POST /api/v1/finance/payment-proposal/{id}/record-utr` | `handleRecordPaymentUTR` | money |
| `POST /api/v1/finance/period-reopen/{request}/apply` | `handlePeriodReopenApply` | money |
| `POST /api/v1/finance/periods` | `handleAccountingPeriods` | money |
| `POST /api/v1/finance/periods/{id}/close` | `handleCloseAccountingPeriod` | money |
| `POST /api/v1/finance/periods/{id}/close-tasks/{task}/{action}` | `handlePeriodCloseTaskAction` | money |
| `POST /api/v1/finance/periods/{id}/reopen-request` | `handlePeriodReopenRequest` | money |
| `POST /api/v1/finance/receipts` | `handleCustomerReceipts` | money, stock |
| `POST /api/v1/finance/sales-invoice/{id}/cancel` | `handleCancelSalesInvoice` | money |
| `POST /api/v1/finance/sales-invoice/{id}/post` | `handlePostSalesInvoice` | money, stock |
| `POST /api/v1/finance/sales-invoice/{id}/settle` | `handleSettleSalesInvoice` | money |
| `POST /api/v1/finance/vendor-credit-note/{id}/post` | `handlePostVendorCreditNote` | money, stock |
| `POST /api/v1/fulfillment/return` | `handleFulfillmentReturn` | stock |
| `POST /api/v1/fulfillment/task/transition` | `handleFulfillmentTaskTransition` | stock |
| `POST /api/v1/grn-steps/{id}/correct` | `handleGRNStepsCorrect` | stock |
| `POST /api/v1/grn-steps/{id}/pay` | `handleGRNStepsPay` | money, stock |
| `POST /api/v1/grn-steps/{id}/post-all` | `handleGRNStepsPostAll` | stock |
| `POST /api/v1/grn-steps/{id}/post/{child}` | `handleGRNStepsPostChild` | stock |
| `POST /api/v1/grn-steps/{id}/price` | `handleGRNStepsPrice` | money, stock |
| `POST /api/v1/grn-steps/{id}/receive` | `handleGRNStepsReceive` | stock |
| `POST /api/v1/gst/ewaybills/{doctype}/{id}/{action}` | `handleEWBAction` | money |
| `POST /api/v1/hr/disburse-loan` | `handleDisburseEmployeeLoan` | money |
| `POST /api/v1/hr/post-payslip` | `handlePostPayslip` | stock |
| `POST /api/v1/hr/run-payroll` | `handleRunPayroll` | money |
| `POST /api/v1/inventory/adjustments` | `handleCreateInventoryAdjustment` | stock |
| `POST /api/v1/inventory/adjustments/{id}/post` | `handlePostInventoryAdjustment` | stock |
| `POST /api/v1/inventory/adjustments/{id}/reverse` | `handleReverseInventoryAdjustment` | stock |
| `POST /api/v1/inventory/challans` | `handleCreateDeliveryChallan` | stock |
| `POST /api/v1/inventory/challans/{id}/cancel` | `handleCancelDeliveryChallan` | stock |
| `POST /api/v1/inventory/challans/{id}/dispatch` | `handleDispatchDeliveryChallan` | stock |
| `POST /api/v1/inventory/challans/{id}/return` | `handleReturnDeliveryChallan` | stock |
| `POST /api/v1/inventory/challans/{id}/write-off` | `handleRequestChallanWriteOff` | money, stock |
| `POST /api/v1/inventory/consumption` | `handlePostConsumptionEntry` | stock |
| `POST /api/v1/inventory/material-requests` | `handleCreateMaterialRequest` | stock |
| `POST /api/v1/inventory/material-requests/{id}/issue` | `handleIssueMaterial` | stock |
| `POST /api/v1/inventory/material-requests/{id}/return` | `handleReturnMaterial` | stock |
| `POST /api/v1/inventory/mrp-changes` | `handleCreateMRPChange` | stock |
| `POST /api/v1/inventory/mrp-changes/{id}/cancel` | `handleCancelMRPChange` | stock |
| `POST /api/v1/loyalty/redeem` | `handleRedeemLoyaltyPoints` | money |
| `POST /api/v1/manufacturing/complete` | `handleCompleteProductionOrder` | stock |
| `POST /api/v1/manufacturing/confirm-operation` | `handleConfirmProductionOperation` | stock |
| `POST /api/v1/manufacturing/issue-material` | `handleIssueProductionMaterial` | stock |
| `POST /api/v1/manufacturing/scrap` | `handlePostProductionScrap` | stock |
| `POST /api/v1/manufacturing/subcontract-order/charge` | `handleJobWorkCharge` | money, stock |
| `POST /api/v1/manufacturing/subcontract-order/receive` | `handleReceiveSubcontractOrder` | stock |
| `POST /api/v1/manufacturing/subcontract-order/receive-lines` | `handleReceiveJobWorkLines` | stock |
| `POST /api/v1/manufacturing/subcontract-order/send` | `handleSendSubcontractOrder` | stock |
| `POST /api/v1/marketplace/channels/{channel}/sync-inventory` | `handleChannelInventorySync` | stock |
| `POST /api/v1/marketplace/couriers/{provider}/pickup` | `handleCourierPickup` | stock |
| `POST /api/v1/marketplace/logistics/manifest` | `handleGenerateManifest` | stock |
| `POST /api/v1/marketplace/logistics/manifest/handover` | `handleHandoverManifest` | stock |
| `POST /api/v1/marketplace/settlement/reconcile` | `handleMarketplaceReconcile` | money |
| `POST /api/v1/oms/gate-passes/{id}/complete` | `handleGatePassTransition` | stock |
| `POST /api/v1/oms/gate-passes/{id}/issue` | `handleGatePassTransition` | stock |
| `POST /api/v1/oms/settlements/reconcile` | `handleReconcileMarketplaceSettlements` | money |
| `POST /api/v1/oms/settlements/{id}/dispute` | `handleRaiseSettlementDispute` | money |
| `POST /api/v1/oms/settlements/{id}/write-off` | `handleWriteOffSettlementVariance` | money |
| `POST /api/v1/oms/shipping-packages/{id}/invoice` | `handleInvoiceShippingPackage` | money |
| `POST /api/v1/orders/{id}/credit-notes` | `handleOrderCreditNotes` | money |
| `POST /api/v1/pos/advances` | `handlePOSAdvances` | money |
| `POST /api/v1/pos/gift-card/balance` | `handleGiftCardBalance` | money |
| `POST /api/v1/pos/gift-card/issue` | `handleIssueGiftCard` | money, stock |
| `POST /api/v1/pos/offline-sales/{id}/reject` | `handleRejectOfflineSale` | money, stock |
| `POST /api/v1/pos/payment/confirm` | `handlePOSPaymentConfirm` | money |
| `POST /api/v1/pos/payment/void` | `handlePOSPaymentVoid` | money |
| `POST /api/v1/pos/price-override` | `handlePOSPriceOverride` | money |
| `POST /api/v1/pos/session/cash-movement` | `handlePOSCashMovement` | money |
| `POST /api/v1/pos/session/close` | `handlePOSSessionClose` | money |
| `POST /api/v1/procurement/purchase-returns/{id}/post` | `handlePostPurchaseReturn` | stock |
| `POST /api/v1/procurement/vendor-invoice/pay` | `handlePayVendorInvoice` | money |
| `POST /api/v1/procurement/vendor-invoice/pay-with-tds` | `handlePayVendorInvoiceWithTDS` | money |
| `POST /api/v1/quality/maintenance-order/{id}/complete` | `handleCompleteMaintenanceOrder` | stock |
| `POST /api/v1/quality/ncr/{id}/close` | `handleCloseNonConformanceReport` | money |
| `POST /api/v1/refunds/{id}/approve` | `handleApproveRefundRequest` | money |
| `POST /api/v1/refunds/{id}/process` | `handleProcessRefundRequest` | money |
| `POST /api/v1/refunds/{id}/reject` | `handleRejectRefundRequest` | money |
| `POST /api/v1/reserve` | `handleCreateReservation` | stock |
| `POST /api/v1/returns/{id}/approve` | `handleApproveReturnRequest` | stock |
| `POST /api/v1/returns/{id}/qc` | `handleApplyReturnQC` | stock |
| `POST /api/v1/returns/{id}/receive` | `handleReceiveReturnRequest` | stock |
| `POST /api/v1/returns/{id}/reject` | `handleRejectReturnRequest` | stock |
| `POST /api/v1/returns/{id}/reverse-pickup` | `handleScheduleReturnReversePickup` | stock |
| `POST /api/v1/service/ticket/{id}/close` | `handleCloseServiceTicket` | money |
| `POST /api/v1/stock-locations` | `handleStockLocations` | stock |
| `POST /api/v1/stock-locations/defaults` | `handleStockLocationDefault` | stock |
| `POST /api/v1/stock-locations/move` | `handleMoveStock` | stock |
| `POST /api/v1/stock-locations/stores` | `handleStoreStockLocations` | stock |
| `POST /api/v1/transfer/approve-direct` | `handleApproveTransferOrderDirect` | stock |
| `POST /api/v1/transfer/cancel` | `handleCancelTransferOrder` | stock |
| `POST /api/v1/transfer/dispatch` | `handleDispatchTransferOrder` | stock |
| `POST /api/v1/transfer/receive` | `handleReceiveTransferOrder` | stock |
| `POST /api/v1/transfer/return` | `handleReturnTransferToSource` | stock |
| `POST /api/v1/transfer/submit` | `handleSubmitTransferOrder` | stock |
| `POST /api/v1/wms/batch/consume` | `handleBatchConsume` | stock |
| `POST /api/v1/wms/batch/putaway` | `handleBatchPutaway` | stock |
| `POST /api/v1/wms/billing/charges` | `handleCapturedCharges` | money |
| `POST /api/v1/wms/billing/invoices` | `handleCapturedChargeInvoice` | money |
| `POST /api/v1/wms/bin-move` | `handleBinMove` | stock |
| `POST /api/v1/wms/bin-replenishment/dynamic-pickface/apply` | `handleApplyDynamicPickFaceMinMax` | stock |
| `POST /api/v1/wms/bin-replenishment/execute` | `handleBinReplenishmentExecute` | stock |
| `POST /api/v1/wms/cross-dock/putaway` | `handleCrossDockPutaway` | stock |
| `POST /api/v1/wms/cycle-count/post-adjustment` | `handleRetryCycleCountPost` | stock |
| `POST /api/v1/wms/cycle-count/reconcile` | `handleReconcileCycleCount` | stock |
| `POST /api/v1/wms/cycle-count/recount/request` | `handleRequestRecount` | stock |
| `POST /api/v1/wms/cycle-count/recount/submit` | `handleSubmitRecountValue` | stock |
| `POST /api/v1/wms/cycle-count/variance-reason` | `handleSetCycleCountVarianceReason` | stock |
| `POST /api/v1/wms/loading/complete` | `handleLoadingComplete` | stock |
| `POST /api/v1/wms/lpn/assign` | `handleLPNAssign` | stock |
| `POST /api/v1/wms/lpn/deconsolidate` | `handleDeconsolidateLPN` | stock |
| `POST /api/v1/wms/owner-stock/consume` | `handleOwnerStockConsume` | stock |
| `POST /api/v1/wms/owner-stock/correct` | `handleCorrectOwnerStock` | stock |
| `POST /api/v1/wms/pack-complete` | `handlePackCompleteValidated` | stock |
| `POST /api/v1/wms/pack-scan` | `handlePackScan` | stock |
| `POST /api/v1/wms/physical-inventory/close` | `handlePhysicalInventoryClose` | money |
| `POST /api/v1/wms/pick-scan` | `handlePickScan` | stock |
| `POST /api/v1/wms/putaway` | `handlePutaway` | stock |
| `POST /api/v1/wms/serial/putaway` | `handleSerialPutaway` | stock |
| `POST /api/v1/wms/transfer/pack` | `handlePackTransferOrder` | stock |
| `POST /api/v1/wms/vas/complete` | `handleVASTaskComplete` | stock |
| `POST /api/v1/wms/wave/assign` | `handleWaveAssign` | stock |
| `POST /api/v1/wms/wave/create` | `handleWaveCreate` | stock |
| `POST /api/v1/wms/wave/from-selection` | `handleWaveFromSelection` | stock |
| `POST /api/v1/wms/wave/pick-confirm` | `handleWavePickConfirm` | stock |
| `POST /api/v1/wms/wave/template/run` | `handleWaveTemplateRun` | stock |
| `POST /api/v1/wms/wave/transition` | `handleWaveTransition` | stock |

## Every state-changing route

| Route | Handler | Key | Replay window | Touches |
|---|---|---|---|---|
| `ANY /api/v1/approval/rules` | `handleApprovalRules` | no | - | document |
| `ANY /api/v1/crm/appointments` | `handleCRMAppointments` | no | - | document |
| `ANY /api/v1/crm/appointments/{id}/{action}` | `handleCRMAppointments` | no | - | document |
| `ANY /api/v1/crm/campaign-runs/{id}/{action}` | `handleCRMCampaignRun` | no | - | document |
| `ANY /api/v1/crm/campaigns` | `handleCRMCampaigns` | no | - | document |
| `ANY /api/v1/crm/campaigns/{id}` | `handleCRMCampaigns` | no | - | document |
| `ANY /api/v1/crm/campaigns/{id}/{part}` | `handleCRMCampaigns` | no | - | document |
| `ANY /api/v1/crm/conversations` | `handleCRMConversations` | no | - | document |
| `ANY /api/v1/crm/conversations/{id}` | `handleCRMConversations` | no | - | document |
| `ANY /api/v1/crm/conversations/{id}/{part}` | `handleCRMConversations` | no | - | document |
| `ANY /api/v1/crm/customers/{id}` | `handleCRMCustomer` | no | - | document |
| `ANY /api/v1/crm/customers/{id}/{part}` | `handleCRMCustomer` | no | - | document |
| `ANY /api/v1/crm/holds` | `handleCRMHolds` | no | - | document |
| `ANY /api/v1/crm/holds/{id}/{action}` | `handleCRMHolds` | no | - | document |
| `ANY /api/v1/crm/hooks/email` | `handleCRMHook` | no | - | document |
| `ANY /api/v1/crm/hooks/sms` | `handleCRMHook` | no | - | document |
| `ANY /api/v1/crm/hooks/webhook` | `handleCRMHook` | no | - | document |
| `ANY /api/v1/crm/hooks/whatsapp` | `handleCRMHookWhatsApp` | no | - | document |
| `ANY /api/v1/crm/journeys` | `handleCRMJourneys` | no | - | document |
| `ANY /api/v1/crm/journeys/{id}` | `handleCRMJourneys` | no | - | document |
| `ANY /api/v1/crm/journeys/{id}/{part}` | `handleCRMJourneys` | no | - | document |
| `ANY /api/v1/crm/leads` | `handleCRMLeads` | no | - | document |
| `ANY /api/v1/crm/leads/{id}` | `handleCRMLeads` | no | - | document |
| `ANY /api/v1/crm/leads/{id}/{part}` | `handleCRMLeads` | no | - | document |
| `ANY /api/v1/crm/lifecycle` | `handleCRMLifecycle` | no | - | document |
| `ANY /api/v1/crm/lists` | `handleCRMLists` | no | - | document |
| `ANY /api/v1/crm/loyalty-tier-rules` | `handleLoyaltyTierRules` | no | - | document |
| `ANY /api/v1/crm/messages` | `handleCRMMessages` | no | - | document |
| `ANY /api/v1/crm/messages/{id}/{part}` | `handleCRMMessages` | no | - | document |
| `ANY /api/v1/crm/opportunities` | `handleCRMOpportunities` | no | - | document |
| `ANY /api/v1/crm/opportunities/{id}` | `handleCRMOpportunities` | no | - | document |
| `ANY /api/v1/crm/opportunities/{id}/{part}` | `handleCRMOpportunities` | no | - | document |
| `ANY /api/v1/crm/pipeline/{part}` | `handleCRMPipelineConfig` | no | - | document |
| `ANY /api/v1/crm/public/feedback` | `handleCRMPublicFeedback` | no | - | document |
| `ANY /api/v1/crm/public/preferences` | `handleCRMPublicPreferences` | no | - | document |
| `ANY /api/v1/crm/referrals` | `handleCRMReferrals` | no | - | document |
| `ANY /api/v1/crm/referrals/{id}/{part}` | `handleCRMReferrals` | no | - | document |
| `ANY /api/v1/crm/segments` | `handleCRMSegments` | no | - | document |
| `ANY /api/v1/crm/segments/{id}` | `handleCRMSegments` | no | - | document |
| `ANY /api/v1/crm/senders` | `handleCRMSenders` | no | - | document |
| `ANY /api/v1/crm/senders/{id}` | `handleCRMSenders` | no | - | document |
| `ANY /api/v1/crm/stores/{store}/slots` | `handleCRMStoreSlots` | no | - | document |
| `ANY /api/v1/crm/surveys` | `handleCRMSurveys` | no | - | document |
| `ANY /api/v1/crm/surveys/{id}` | `handleCRMSurveys` | no | - | document |
| `ANY /api/v1/crm/surveys/{id}/{part}` | `handleCRMSurveys` | no | - | document |
| `ANY /api/v1/crm/tasks` | `handleCRMTasks` | no | - | document |
| `ANY /api/v1/crm/tasks/{id}` | `handleCRMTasks` | no | - | document |
| `ANY /api/v1/crm/tasks/{id}/{action}` | `handleCRMTasks` | no | - | document |
| `ANY /api/v1/crm/templates` | `handleCRMTemplates` | no | - | document |
| `ANY /api/v1/crm/templates/{id}` | `handleCRMTemplates` | no | - | document |
| `ANY /api/v1/crm/templates/{id}/{part}` | `handleCRMTemplates` | no | - | document |
| `ANY /api/v1/crm/tickets` | `handleCRMTickets` | no | - | document |
| `ANY /api/v1/crm/tickets/{id}` | `handleCRMTickets` | no | - | document |
| `ANY /api/v1/crm/tickets/{id}/{part}` | `handleCRMTickets` | no | - | document |
| `ANY /api/v1/debug/panic` | `handleDebugPanic` | no | - | document |
| `ANY /api/v1/doc/{doctype}` | `handleGenericDoc` | no | - | document |
| `ANY /api/v1/doc/{doctype}/{id}` | `handleGenericDoc` | no | - | document |
| `ANY /api/v1/labels` | `handleLabels` | no | - | document |
| `ANY /api/v1/logs/audit` | `handleAuditLogs` | no | - | document |
| `ANY /api/v1/logs/system` | `handleSystemLogs` | no | - | document |
| `ANY /api/v1/loyalty/adjustments` | `handleLoyaltyAdjustments` | no | - | stock |
| `ANY /api/v1/prefix` | `handlePrefix` | no | - | document |
| `ANY /api/v1/sequence` | `handleSequence` | no | - | document |
| `DELETE /api/v1/admin/api-credentials/{id}` | `handleRevokeAPICredential` | no | - | document |
| `DELETE /api/v1/admin/branding/logo` | `handleBrandingLogoAdmin` | no | - | document |
| `DELETE /api/v1/admin/extension/hooks/{id}` | `handleDeleteExtensionHook` | no | - | document |
| `DELETE /api/v1/admin/sandbox-tenants/{id}` | `handleDeleteSandboxTenant` | no | - | document |
| `DELETE /api/v1/attachments/file/{attachmentID}` | `handleRemoveAttachment` | no | - | document |
| `DELETE /api/v1/dashboards/custom/{id}` | `handleCustomDashboard` | no | - | document |
| `DELETE /api/v1/dashboards/layouts/{id}` | `handleDeleteDashboardLayout` | no | - | document |
| `DELETE /api/v1/meta/{doctype}/fields/{id}` | `handleDeleteFieldDefinition` | no | - | document |
| `DELETE /api/v1/oms/views/{id}` | `handleOMSDeleteSavedView` | no | - | document |
| `POST /api/v1/admin/api-credentials` | `handleIssueAPICredential` | no | - | document |
| `POST /api/v1/admin/api-credentials/{id}/rotate` | `handleRotateAPICredential` | no | - | document |
| `POST /api/v1/admin/audit-logs/archive` | `handleRunAuditArchive` | no | - | document |
| `POST /api/v1/admin/audit-logs/archives/{id}/restore` | `handleRestoreAuditArchive` | no | - | document |
| `POST /api/v1/admin/audit-logs/checkpoint` | `handleWriteAuditCheckpoint` | n.a. (a question, not a command) | - | document |
| `POST /api/v1/admin/branding/logo` | `handleBrandingLogoAdmin` | no | - | document |
| `POST /api/v1/admin/extension/hooks` | `handleCreateExtensionHook` | no | - | document |
| `POST /api/v1/admin/extension/token` | `handleIssueExtensionToken` | n.a. (a question, not a command) | - | document |
| `POST /api/v1/admin/image-drive` | `handleSaveImageDrive` | no | - | document |
| `POST /api/v1/admin/image-drive/test` | `handleTestImageDrive` | n.a. (a question, not a command) | - | document |
| `POST /api/v1/admin/industry` | `handleSwitchIndustry` | no | - | document |
| `POST /api/v1/admin/patch/approve` | `handleApprovePatchProposal` | no | - | document |
| `POST /api/v1/admin/patch/reject` | `handleRejectPatchProposal` | no | - | document |
| `POST /api/v1/admin/privacy/legal-hold` | `handleSetSubjectLegalHold` | no | - | document |
| `POST /api/v1/admin/privacy/requests` | `handleCreateDataSubjectRequest` | no | - | document |
| `POST /api/v1/admin/privacy/requests/{id}/decide` | `handleDecideDataSubjectRequest` | no | - | document |
| `POST /api/v1/admin/privacy/requests/{id}/execute` | `handleExecuteDataSubjectRequest` | no | - | document |
| `POST /api/v1/admin/role-permissions` | `handleRolePermissions` | no | - | document |
| `POST /api/v1/admin/role-templates/apply` | `handleApplyRoleTemplates` | no | - | document |
| `POST /api/v1/admin/role-templates/revert` | `handleRevertRoleTemplates` | no | - | document |
| `POST /api/v1/admin/roles` | `handleCreateRole` | no | - | document |
| `POST /api/v1/admin/roles/stores` | `handleRoleStores` | no | - | document |
| `POST /api/v1/admin/sandbox-tenants` | `handleProvisionSandboxTenant` | no | - | document |
| `POST /api/v1/admin/sandbox-tenants/{id}/reset` | `handleResetSandboxTenant` | no | - | document |
| `POST /api/v1/admin/scale-test` | `handleScaleTest` | no | - | document |
| `POST /api/v1/admin/security/detections/run` | `handleRunSecurityDetections` | no | - | document |
| `POST /api/v1/admin/security/freeze-exports` | `handleFreezeExports` | no | - | document |
| `POST /api/v1/admin/security/sign-out-all` | `handleSignOutAllUsers` | n.a. (a question, not a command) | - | document |
| `POST /api/v1/admin/security/sign-out-user` | `handleSignOutUserEverywhere` | n.a. (a question, not a command) | - | document |
| `POST /api/v1/admin/setup-workbook/apply` | `handleSetupWorkbookApply` | no | - | document |
| `POST /api/v1/admin/setup-workbook/preview` | `handleSetupWorkbookPreview` | n.a. (a question, not a command) | - | document |
| `POST /api/v1/admin/tenant/feature-flag` | `handleSetFeatureFlag` | no | - | document |
| `POST /api/v1/admin/tenant/host-slug` | `handleSetTenantHostSlug` | no | - | document |
| `POST /api/v1/admin/tenant/module-entitlement` | `handleSetModuleEntitlement` | no | - | document |
| `POST /api/v1/admin/tenant/package` | `handleSetTenantPackage` | no | - | document |
| `POST /api/v1/admin/tenant/provision` | `handleProvisionTenant` | no | - | document |
| `POST /api/v1/admin/users` | `handleCreateUser` | no | - | document |
| `POST /api/v1/admin/users/location` | `handleSetUserLocation` | no | - | document |
| `POST /api/v1/admin/users/reset-mfa` | `handleAdminResetUserMFA` | no | - | document |
| `POST /api/v1/admin/users/reset-password` | `handleRequestPasswordReset` | no | - | document |
| `POST /api/v1/admin/users/status` | `handleSetUserStatus` | no | - | document |
| `POST /api/v1/admin/users/stores` | `handleUserStores` | no | - | document |
| `POST /api/v1/admin/users/supplier` | `handleSetUserSupplier` | no | - | document |
| `POST /api/v1/approval/bulk-decide` | `handleBulkDecideApproval` | no | - | document |
| `POST /api/v1/approval/decide` | `handleDecideApproval` | no | - | document |
| `POST /api/v1/approval/submit` | `handleSubmitApproval` | no | - | document |
| `POST /api/v1/assets/capitalize` | `handleCapitalizeAsset` | no | - | document |
| `POST /api/v1/assets/depreciation/run` | `handleRunAssetDepreciation` | no | - | money |
| `POST /api/v1/assets/dispose` | `handleDisposeAsset` | no | - | document |
| `POST /api/v1/assets/transfer` | `handleTransferAsset` | no | - | stock |
| `POST /api/v1/attachments/file/{attachmentID}/remove` | `handleRemoveAttachment` | no | - | document |
| `POST /api/v1/attachments/{doctype}/{id}` | `handleAddAttachment` | no | - | document |
| `POST /api/v1/auth/forgot-password` | `handleForgotPassword` | no | - | document |
| `POST /api/v1/auth/mfa/activate` | `handleMFAActivate` | no | - | document |
| `POST /api/v1/auth/mfa/enroll` | `handleMFAEnroll` | no | - | document |
| `POST /api/v1/auth/mfa/verify` | `handleMFAVerify` | n.a. (a question, not a command) | - | document |
| `POST /api/v1/auth/operator-handoff` | `handleOperatorHandoff` | no | - | document |
| `POST /api/v1/auth/reset-password` | `handleResetPassword` | no | - | document |
| `POST /api/v1/checkout` | `handleCheckout` | yes (command key) | 7 days (command claim) | money, stock |
| `POST /api/v1/clevertap/credentials` | `handleCleverTapCredentials` | no | - | document |
| `POST /api/v1/crm/customer/merge` | `handleMergeCustomers` | no | - | document |
| `POST /api/v1/crm/loyalty-redemption/initiate` | `handleInitiateSecureLoyaltyRedemption` | no | - | money |
| `POST /api/v1/crm/loyalty-redemption/verify` | `handleVerifySecureLoyaltyRedemption` | n.a. (a question, not a command) | - | money |
| `POST /api/v1/crm/merges` | `handleCRMMerge` | no | - | document |
| `POST /api/v1/crm/public/unsubscribe` | `handleCRMPublicUnsubscribe` | no | - | document |
| `POST /api/v1/crm/senders/{id}/test` | `handleCRMSenderTest` | n.a. (a question, not a command) | - | document |
| `POST /api/v1/crm/voucher-holds/{action}` | `handleCRMVoucherHold` | no | - | money |
| `POST /api/v1/crm/voucher/redeem` | `handleRedeemVoucher` | no | - | money |
| `POST /api/v1/crm/voucher/validate` | `handleValidateVoucher` | n.a. (a question, not a command) | - | money |
| `POST /api/v1/dashboards/custom` | `handleCustomDashboards` | no | - | document |
| `POST /api/v1/dashboards/custom/{id}/copy` | `handleCopyCustomDashboard` | no | - | document |
| `POST /api/v1/dashboards/custom/{id}/digest` | `handleScheduleDashboardDigest` | no | - | document |
| `POST /api/v1/dashboards/export` | `handleDashboardExport` | n.a. (a question, not a command) | - | document |
| `POST /api/v1/dashboards/layouts` | `handleDashboardLayouts` | no | - | document |
| `POST /api/v1/dashboards/query` | `handleDashboardQuery` | n.a. (a question, not a command) | - | document |
| `POST /api/v1/doc/{doctype}/{id}/reactivate` | `handleReactivateMasterDocument` | no | - | document |
| `POST /api/v1/einvoice/documents/{id}/{action}` | `handleEInvoiceDocumentAction` | no | - | money |
| `POST /api/v1/einvoice/profiles` | `handleSaveEInvoiceProfile` | no | - | money |
| `POST /api/v1/einvoice/profiles/{gstin}/test` | `handleTestEInvoiceProfile` | n.a. (a question, not a command) | - | money |
| `POST /api/v1/einvoice/run` | `handleRunEInvoiceWorker` | no | - | money |
| `POST /api/v1/expenses/pay` | `handlePayExpenseClaim` | no | - | money |
| `POST /api/v1/expenses/verify` | `handleVerifyExpenseClaim` | n.a. (a question, not a command) | - | money |
| `POST /api/v1/finance/acquirer-settlements/match` | `handleMatchAcquirerSettlements` | n.a. (a question, not a command) | - | document |
| `POST /api/v1/finance/bank-reconcile` | `handleBankReconcile` | no | - | money |
| `POST /api/v1/finance/bank-reconciliation/lines/{id}/{action}` | `handleBankLineAction` | no | - | money |
| `POST /api/v1/finance/bank-reconciliation/rule-match` | `handleBankRuleMatch` | no | - | money |
| `POST /api/v1/finance/credit-note/{id}/post` | `handlePostCreditNote` | no | - | money, stock |
| `POST /api/v1/finance/debit-note/{id}/post` | `handlePostDebitNote` | no | - | stock |
| `POST /api/v1/finance/fx-revaluation` | `handleFXRevaluation` | no | - | money |
| `POST /api/v1/finance/intercompany-transaction` | `handleCreateIntercompanyTransaction` | no | - | document |
| `POST /api/v1/finance/intercompany-transaction/{id}/retry-post` | `handleRetryPostIntercompanyTransaction` | no | - | document |
| `POST /api/v1/finance/journal-voucher` | `handleCreateJournalVoucher` | no | - | money |
| `POST /api/v1/finance/journal-voucher/recurring` | `handleCreateRecurringJournalTemplate` | no | - | money |
| `POST /api/v1/finance/journal-voucher/{id}/retry-post` | `handleRetryPostJournalVoucher` | no | - | money |
| `POST /api/v1/finance/journal-voucher/{id}/reverse` | `handleReverseJournalVoucher` | no | - | money |
| `POST /api/v1/finance/landed-cost-voucher` | `handleCreateLandedCostVoucher` | no | - | money |
| `POST /api/v1/finance/landed-cost-voucher/{id}/apply` | `handleApplyLandedCostVoucher` | no | - | money |
| `POST /api/v1/finance/payment-proposal` | `handlePaymentProposal` | no | - | money |
| `POST /api/v1/finance/payment-proposal/{id}/execute` | `handleExecutePaymentProposal` | no | - | money |
| `POST /api/v1/finance/payment-proposal/{id}/record-utr` | `handleRecordPaymentUTR` | no | - | money |
| `POST /api/v1/finance/period-reopen/{request}/apply` | `handlePeriodReopenApply` | no | - | money |
| `POST /api/v1/finance/periods` | `handleAccountingPeriods` | no | - | money |
| `POST /api/v1/finance/periods/{id}/close` | `handleCloseAccountingPeriod` | no | - | money |
| `POST /api/v1/finance/periods/{id}/close-tasks/{task}/{action}` | `handlePeriodCloseTaskAction` | no | - | money |
| `POST /api/v1/finance/periods/{id}/reopen-request` | `handlePeriodReopenRequest` | no | - | money |
| `POST /api/v1/finance/receipts` | `handleCustomerReceipts` | no | - | money, stock |
| `POST /api/v1/finance/sales-invoice/{id}/cancel` | `handleCancelSalesInvoice` | no | - | money |
| `POST /api/v1/finance/sales-invoice/{id}/post` | `handlePostSalesInvoice` | no | - | money, stock |
| `POST /api/v1/finance/sales-invoice/{id}/settle` | `handleSettleSalesInvoice` | no | - | money |
| `POST /api/v1/finance/vendor-credit-note/{id}/post` | `handlePostVendorCreditNote` | no | - | money, stock |
| `POST /api/v1/fulfillment/return` | `handleFulfillmentReturn` | no | - | stock |
| `POST /api/v1/fulfillment/task/transition` | `handleFulfillmentTaskTransition` | no | - | stock |
| `POST /api/v1/grn-steps/{id}/correct` | `handleGRNStepsCorrect` | no | - | stock |
| `POST /api/v1/grn-steps/{id}/pay` | `handleGRNStepsPay` | no | - | money, stock |
| `POST /api/v1/grn-steps/{id}/post-all` | `handleGRNStepsPostAll` | no | - | stock |
| `POST /api/v1/grn-steps/{id}/post/{child}` | `handleGRNStepsPostChild` | no | - | stock |
| `POST /api/v1/grn-steps/{id}/price` | `handleGRNStepsPrice` | no | - | money, stock |
| `POST /api/v1/grn-steps/{id}/receive` | `handleGRNStepsReceive` | no | - | stock |
| `POST /api/v1/grn-steps/{id}/verify` | `handleGRNStepsVerify` | n.a. (a question, not a command) | - | stock |
| `POST /api/v1/gst/calculate` | `handleCalculateGST` | no | - | document |
| `POST /api/v1/gst/ewaybills/{doctype}/{id}/{action}` | `handleEWBAction` | no | - | money |
| `POST /api/v1/hr/disburse-loan` | `handleDisburseEmployeeLoan` | no | - | money |
| `POST /api/v1/hr/onboarding/{id}/steps` | `handleOnboardingStep` | no | - | document |
| `POST /api/v1/hr/post-payslip` | `handlePostPayslip` | no | - | stock |
| `POST /api/v1/hr/run-payroll` | `handleRunPayroll` | no | - | money |
| `POST /api/v1/images` | `handleUploadOwnerImage` | no | - | document |
| `POST /api/v1/images/main` | `handleOwnerImageAction` | no | - | document |
| `POST /api/v1/images/remove` | `handleOwnerImageAction` | no | - | document |
| `POST /api/v1/import-jobs/{id}/resume` | `handleResumeImportJob` | no | - | document |
| `POST /api/v1/import/{doctype}` | `handleBulkImport` | no | - | document |
| `POST /api/v1/integration/bigcommerce/webhook/{channelCode}` | `handleBigCommerceWebhook` | no | - | document |
| `POST /api/v1/integration/courier/{provider}/tracking` | `handleCourierTrackingWebhook` | no | - | document |
| `POST /api/v1/integration/retry` | `handleRetryIntegrationEvent` | no | - | document |
| `POST /api/v1/integration/shopify/order` | `handleShopifyOrderWebhook` | no | - | document |
| `POST /api/v1/integration/shopify/product/map` | `handleShopifyProductMap` | no | - | document |
| `POST /api/v1/integration/shopify/webhook` | `handleShopifyWebhook` | no | - | document |
| `POST /api/v1/integrations/clevertap/segment-sync` | `handleCleverTapSegmentSync` | no | - | document |
| `POST /api/v1/integrations/credentials/disconnect` | `handleConnectorDisconnect` | no | - | document |
| `POST /api/v1/inventory/adjustments` | `handleCreateInventoryAdjustment` | no | - | stock |
| `POST /api/v1/inventory/adjustments/preview` | `handlePreviewInventoryAdjustment` | n.a. (a question, not a command) | - | stock |
| `POST /api/v1/inventory/adjustments/{id}/post` | `handlePostInventoryAdjustment` | no | - | stock |
| `POST /api/v1/inventory/adjustments/{id}/reverse` | `handleReverseInventoryAdjustment` | no | - | stock |
| `POST /api/v1/inventory/attribute-value` | `handleAddItemAttributeValue` | no | - | document |
| `POST /api/v1/inventory/challans` | `handleCreateDeliveryChallan` | no | - | stock |
| `POST /api/v1/inventory/challans/{id}/cancel` | `handleCancelDeliveryChallan` | no | - | stock |
| `POST /api/v1/inventory/challans/{id}/dispatch` | `handleDispatchDeliveryChallan` | no | - | stock |
| `POST /api/v1/inventory/challans/{id}/return` | `handleReturnDeliveryChallan` | no | - | stock |
| `POST /api/v1/inventory/challans/{id}/write-off` | `handleRequestChallanWriteOff` | no | - | money, stock |
| `POST /api/v1/inventory/consumption` | `handlePostConsumptionEntry` | no | - | stock |
| `POST /api/v1/inventory/item-setup/fields/list` | `handleCreateItemFieldList` | no | - | document |
| `POST /api/v1/inventory/material-requests` | `handleCreateMaterialRequest` | no | - | stock |
| `POST /api/v1/inventory/material-requests/{id}/issue` | `handleIssueMaterial` | no | - | stock |
| `POST /api/v1/inventory/material-requests/{id}/return` | `handleReturnMaterial` | no | - | stock |
| `POST /api/v1/inventory/mrp-changes` | `handleCreateMRPChange` | no | - | stock |
| `POST /api/v1/inventory/mrp-changes/{id}/cancel` | `handleCancelMRPChange` | no | - | stock |
| `POST /api/v1/inventory/treatment-correction/apply` | `handleTreatmentCorrectionApply` | no | - | document |
| `POST /api/v1/inventory/treatment-correction/preview` | `handleTreatmentCorrectionPreview` | n.a. (a question, not a command) | - | document |
| `POST /api/v1/jobs/{id}/cancel` | `handleCancelJob` | no | - | document |
| `POST /api/v1/jobs/{id}/retry` | `handleRetryJob` | no | - | document |
| `POST /api/v1/lines/resolve` | `handleResolveLineCodes` | n.a. (a question, not a command) | - | document |
| `POST /api/v1/login` | `handleLogin` | n.a. (a question, not a command) | - | document |
| `POST /api/v1/loyalty/redeem` | `handleRedeemLoyaltyPoints` | no | - | money |
| `POST /api/v1/manufacturing/acknowledge-bom-variance` | `handleAcknowledgeBOMVariance` | no | - | document |
| `POST /api/v1/manufacturing/complete` | `handleCompleteProductionOrder` | no | - | stock |
| `POST /api/v1/manufacturing/confirm-operation` | `handleConfirmProductionOperation` | no | - | stock |
| `POST /api/v1/manufacturing/issue-material` | `handleIssueProductionMaterial` | no | - | stock |
| `POST /api/v1/manufacturing/partial-complete` | `handlePartialCompleteProductionOrder` | no | - | document |
| `POST /api/v1/manufacturing/record-actual-cost` | `handleRecordActualProductionCost` | no | - | document |
| `POST /api/v1/manufacturing/rework` | `handleSendProductionToRework` | no | - | document |
| `POST /api/v1/manufacturing/scrap` | `handlePostProductionScrap` | no | - | stock |
| `POST /api/v1/manufacturing/subcontract-order/charge` | `handleJobWorkCharge` | no | - | money, stock |
| `POST /api/v1/manufacturing/subcontract-order/receive` | `handleReceiveSubcontractOrder` | no | - | stock |
| `POST /api/v1/manufacturing/subcontract-order/receive-lines` | `handleReceiveJobWorkLines` | no | - | stock |
| `POST /api/v1/manufacturing/subcontract-order/send` | `handleSendSubcontractOrder` | no | - | stock |
| `POST /api/v1/marketplace/channels/{channel}/credentials` | `handleChannelConnectorCredentials` | no | - | document |
| `POST /api/v1/marketplace/channels/{channel}/pull-orders` | `handleChannelOrderPull` | no | - | document |
| `POST /api/v1/marketplace/channels/{channel}/push-status` | `handleChannelStatusPush` | no | - | document |
| `POST /api/v1/marketplace/channels/{channel}/sync-inventory` | `handleChannelInventorySync` | no | - | stock |
| `POST /api/v1/marketplace/couriers/{provider}/awb` | `handleCourierAllocateAWB` | no | - | document |
| `POST /api/v1/marketplace/couriers/{provider}/cancel` | `handleCourierCancel` | no | - | document |
| `POST /api/v1/marketplace/couriers/{provider}/credentials` | `handleCourierCredentials` | no | - | document |
| `POST /api/v1/marketplace/couriers/{provider}/pickup` | `handleCourierPickup` | no | - | stock |
| `POST /api/v1/marketplace/couriers/{provider}/resolve` | `handleCourierResolveBooking` | n.a. (a question, not a command) | - | document |
| `POST /api/v1/marketplace/logistics/book` | `handleLogisticsBook` | no | - | document |
| `POST /api/v1/marketplace/logistics/manifest` | `handleGenerateManifest` | no | - | stock |
| `POST /api/v1/marketplace/logistics/manifest/handover` | `handleHandoverManifest` | no | - | stock |
| `POST /api/v1/marketplace/logistics/rto` | `handleShipmentRTO` | no | - | document |
| `POST /api/v1/marketplace/logistics/tracking` | `handleShipmentTracking` | no | - | document |
| `POST /api/v1/marketplace/ndr/{id}/resolve` | `handleNDRResolve` | n.a. (a question, not a command) | - | document |
| `POST /api/v1/marketplace/settlement/reconcile` | `handleMarketplaceReconcile` | no | - | money |
| `POST /api/v1/marketplace/sku-mappings` | `handleChannelSKUMappings` | no | - | document |
| `POST /api/v1/master-codes/Location/code` | `handleLocationCodeChange` | no | - | document |
| `POST /api/v1/master-codes/{doctype}/clean` | `handleMasterCodeClean` | no | - | document |
| `POST /api/v1/me/change-password` | `handleChangePassword` | no | - | document |
| `POST /api/v1/me/mfa/devices/forget` | `handleForgetMyMFADevices` | no | - | document |
| `POST /api/v1/me/mfa/recovery-codes/regenerate` | `handleMFARegenerateRecoveryCodes` | no | - | document |
| `POST /api/v1/me/mfa/reenroll` | `handleMFAReenrollStart` | no | - | document |
| `POST /api/v1/me/mfa/reenroll/cancel` | `handleMFAReenrollCancel` | no | - | document |
| `POST /api/v1/me/mfa/reenroll/confirm` | `handleMFAReenrollConfirm` | no | - | document |
| `POST /api/v1/me/working-store` | `handleSwitchWorkingStore` | no | - | document |
| `POST /api/v1/meta/doctypes` | `handleSaveDocType` | no | - | document |
| `POST /api/v1/meta/{doctype}/fields` | `handleSaveFieldDefinition` | no | - | document |
| `POST /api/v1/oms/gate-passes` | `handleCreateGatePass` | no | - | document |
| `POST /api/v1/oms/gate-passes/{id}/complete` | `handleGatePassTransition` | no | - | stock |
| `POST /api/v1/oms/gate-passes/{id}/discard` | `handleGatePassTransition` | no | - | document |
| `POST /api/v1/oms/gate-passes/{id}/issue` | `handleGatePassTransition` | no | - | stock |
| `POST /api/v1/oms/gate-passes/{id}/update` | `handleUpdateGatePass` | no | - | document |
| `POST /api/v1/oms/orders/bulk` | `handleOMSBulkAction` | no | - | document |
| `POST /api/v1/oms/orders/{id}/channel-change-done` | `handleChannelChangeDone` | no | - | document |
| `POST /api/v1/oms/settlements/reconcile` | `handleReconcileMarketplaceSettlements` | no | - | money |
| `POST /api/v1/oms/settlements/{id}/dispute` | `handleRaiseSettlementDispute` | no | - | money |
| `POST /api/v1/oms/settlements/{id}/resolve` | `handleResolveSettlementDispute` | n.a. (a question, not a command) | - | money |
| `POST /api/v1/oms/settlements/{id}/write-off` | `handleWriteOffSettlementVariance` | no | - | money |
| `POST /api/v1/oms/shipping-packages` | `handleCreateShippingPackage` | no | - | document |
| `POST /api/v1/oms/shipping-packages/{id}/cancel` | `handleCancelShippingPackage` | no | - | document |
| `POST /api/v1/oms/shipping-packages/{id}/invoice` | `handleInvoiceShippingPackage` | no | - | money |
| `POST /api/v1/oms/shipping-packages/{id}/split` | `handleSplitShippingPackage` | no | - | document |
| `POST /api/v1/oms/shipping-packages/{id}/update` | `handleUpdateShippingPackage` | no | - | document |
| `POST /api/v1/oms/views` | `handleOMSSavedViews` | no | - | document |
| `POST /api/v1/optimization/forecast` | `handleDemandForecast` | n.a. (a question, not a command) | - | document |
| `POST /api/v1/order-lines/{lineId}/hold` | `handleHoldOrderLine` | no | - | document |
| `POST /api/v1/order-lines/{lineId}/release-hold` | `handleReleaseOrderLineHold` | no | - | document |
| `POST /api/v1/orders` | `handleCreateSalesOrder` | no | - | document |
| `POST /api/v1/orders/{id}/cancel` | `handleCancelOrder` | no | - | document |
| `POST /api/v1/orders/{id}/credit-notes` | `handleOrderCreditNotes` | no | - | money |
| `POST /api/v1/orders/{id}/edit` | `handleEditOrder` | no | - | document |
| `POST /api/v1/orders/{id}/hold` | `handlePlaceOrderHold` | no | - | document |
| `POST /api/v1/orders/{id}/lines` | `handleEditOrderLines` | no | - | document |
| `POST /api/v1/orders/{id}/priority` | `handleSetOrderPriority` | no | - | document |
| `POST /api/v1/orders/{id}/release-hold` | `handleReleaseOrderHold` | no | - | document |
| `POST /api/v1/orders/{id}/release-to-fulfillment` | `handleReleaseOrderToFulfillment` | no | - | document |
| `POST /api/v1/orders/{id}/replacement` | `handleCreateReplacementOrder` | yes (command key) | per engine (see handler) | document |
| `POST /api/v1/orders/{id}/split` | `handleSplitOrder` | no | - | document |
| `POST /api/v1/orders/{id}/switch-facility` | `handleSwitchOrderFacility` | no | - | document |
| `POST /api/v1/pim/barcode/generate` | `handlePIMGenerateBarcode` | no | - | document |
| `POST /api/v1/pim/bulk-edit` | `handlePIMBulkEdit` | no | - | document |
| `POST /api/v1/pim/catalogs/{id}/rotate-share-token` | `handlePIMCatalogRotateShareToken` | no | - | document |
| `POST /api/v1/pim/channels/{code}/apply-pack` | `handlePIMApplyChannelPack` | no | - | document |
| `POST /api/v1/pim/channels/{code}/credentials` | `handleSaveChannelCredential` | no | - | document |
| `POST /api/v1/pim/channels/{code}/pull-state` | `handlePIMChannelPullState` | no | - | document |
| `POST /api/v1/pim/content/{id}/rollback` | `handlePIMContentRollback` | no | - | document |
| `POST /api/v1/pim/export-templates/{id}/run` | `handlePIMExportTemplateRun` | no | - | document |
| `POST /api/v1/pim/import-schedules/{id}/rotate-hook-token` | `handlePIMImportScheduleRotateHookToken` | no | - | document |
| `POST /api/v1/pim/import-templates/{id}/import` | `handlePIMImportTemplateRun` | no | - | document |
| `POST /api/v1/pim/import-templates/{id}/preview` | `handlePIMImportTemplateRun` | n.a. (a question, not a command) | - | document |
| `POST /api/v1/pim/import/hook` | `handlePIMImportHook` | no | - | document |
| `POST /api/v1/pim/import/{doctype}/preview` | `handlePIMImportPreview` | n.a. (a question, not a command) | - | document |
| `POST /api/v1/pim/media/bulk-upload` | `handlePIMMediaBulkUpload` | no | - | document |
| `POST /api/v1/pim/media/upload` | `handlePIMMediaUpload` | no | - | document |
| `POST /api/v1/pim/media/{id}/deactivate` | `handlePIMMediaDeactivate` | no | - | document |
| `POST /api/v1/pim/media/{id}/metadata` | `handlePIMMediaUpdateMetadata` | no | - | document |
| `POST /api/v1/pim/publish` | `handlePIMPublish` | no | - | document |
| `POST /api/v1/pim/task-templates/{code}/instantiate` | `handlePIMTaskTemplateInstantiate` | no | - | document |
| `POST /api/v1/pim/tasks` | `handlePIMTasks` | no | - | document |
| `POST /api/v1/pim/tasks/bulk` | `handlePIMTaskBulk` | no | - | document |
| `POST /api/v1/pim/tasks/{id}/{action}` | `handlePIMTaskAction` | no | - | document |
| `POST /api/v1/pim/translations/seed` | `handlePIMSeedTranslations` | no | - | document |
| `POST /api/v1/pim/workflow-runs/bulk` | `handlePIMWorkflowRunBulk` | no | - | document |
| `POST /api/v1/pim/workflow-runs/{id}/action` | `handlePIMWorkflowRunAction` | no | - | document |
| `POST /api/v1/pim/workflows/{code}/start` | `handlePIMWorkflowStart` | no | - | document |
| `POST /api/v1/pinelabs/credentials` | `handlePineLabsCredentials` | no | - | document |
| `POST /api/v1/pinelabs/reconcile` | `handlePineLabsReconcile` | no | - | document |
| `POST /api/v1/pinelabs/transaction` | `handlePineLabsTransaction` | no | - | document |
| `POST /api/v1/planning/proposals/approve` | `handleApproveSupplyProposals` | no | - | document |
| `POST /api/v1/planning/proposals/generate` | `handleGenerateSupplyProposals` | no | - | document |
| `POST /api/v1/planning/proposals/reject` | `handleRejectSupplyProposals` | no | - | document |
| `POST /api/v1/platform/tenants/{tenant}/attribute-library` | `handleApplyAttributeLibrary` | no | - | document |
| `POST /api/v1/platform/tenants/{tenant}/open` | `handleControlPanelOpenTenant` | no | - | document |
| `POST /api/v1/platform/tenants/{tenant}/records/{doctype}/{id}` | `handlePlatformUpdateRecord` | no | - | document |
| `POST /api/v1/platform/tenants/{tenant}/status` | `handleControlPanelTenantStatus` | no | - | document |
| `POST /api/v1/platform/tenants/{tenant}/tables/{table}/row` | `handlePlatformUpdateRow` | no | - | document |
| `POST /api/v1/platform/tenants/{tenant}/trial` | `handleControlPanelTenantTrial` | no | - | document |
| `POST /api/v1/pos/advances` | `handlePOSAdvances` | no | - | money |
| `POST /api/v1/pos/advances/refund` | `handlePOSAdvanceRefund` | yes (command key) | per engine (see handler) | money |
| `POST /api/v1/pos/bills/{id}/cancel` | `handleCancelPOSBill` | no | - | document |
| `POST /api/v1/pos/exchange/start` | `handleStartPOSExchange` | no | - | document |
| `POST /api/v1/pos/gift-card/balance` | `handleGiftCardBalance` | no | - | money |
| `POST /api/v1/pos/gift-card/issue` | `handleIssueGiftCard` | no | - | money, stock |
| `POST /api/v1/pos/held-bills` | `handleHoldPOSBill` | no | - | document |
| `POST /api/v1/pos/held-bills/{id}/{action}` | `handleHeldPOSBillAction` | no | - | document |
| `POST /api/v1/pos/loyalty/redemption/start` | `handleStartPOSRedemption` | no | - | document |
| `POST /api/v1/pos/loyalty/redemption/verify` | `handleVerifyPOSRedemption` | n.a. (a question, not a command) | - | document |
| `POST /api/v1/pos/offers/preview` | `handlePOSOffersPreview` | n.a. (a question, not a command) | - | document |
| `POST /api/v1/pos/offline-heartbeat` | `handlePOSOfflineHeartbeat` | no | - | document |
| `POST /api/v1/pos/offline-sales/{id}/reject` | `handleRejectOfflineSale` | no | - | money, stock |
| `POST /api/v1/pos/offline-sales/{id}/validate` | `handleValidateOfflineSale` | n.a. (a question, not a command) | - | money, stock |
| `POST /api/v1/pos/payment/confirm` | `handlePOSPaymentConfirm` | no | - | money |
| `POST /api/v1/pos/payment/void` | `handlePOSPaymentVoid` | no | - | money |
| `POST /api/v1/pos/price-override` | `handlePOSPriceOverride` | no | - | money |
| `POST /api/v1/pos/quote` | `handlePOSQuote` | n.a. (a question, not a command) | - | document |
| `POST /api/v1/pos/session/cash-movement` | `handlePOSCashMovement` | no | - | money |
| `POST /api/v1/pos/session/close` | `handlePOSSessionClose` | no | - | money |
| `POST /api/v1/pos/session/open` | `handlePOSSessionOpen` | no | - | document |
| `POST /api/v1/pos/session/takeover` | `handlePOSSessionTakeover` | no | - | document |
| `POST /api/v1/pos/walk-in-customer` | `handlePOSWalkInCustomer` | no | - | document |
| `POST /api/v1/print/document/{txn}/{id}/log` | `handlePrintDocumentLog` | no | - | document |
| `POST /api/v1/print/document/{txn}/{id}/reprint-request` | `handleRequestReprint` | no | - | document |
| `POST /api/v1/print/document/{txn}/{id}/send` | `handlePrintDocumentSend` | no | - | document |
| `POST /api/v1/print/qz/assignments` | `handleQZPrinterChoices` | no | - | document |
| `POST /api/v1/print/qz/log` | `handleQZPrintLog` | no | - | document |
| `POST /api/v1/print/qz/payload` | `handleQZPrintPayload` | yes (command key) | per engine (see handler) | document |
| `POST /api/v1/print/qz/sign` | `handleQZSign` | n.a. (a question, not a command) | - | document |
| `POST /api/v1/print/templates/{id}/versions/{version}` | `handlePrintTemplateVersionAction` | no | - | document |
| `POST /api/v1/procurement/bulk-upload/apply` | `handleBulkPurchaseApply` | no | - | document |
| `POST /api/v1/procurement/bulk-upload/preview` | `handleBulkPurchasePreview` | n.a. (a question, not a command) | - | document |
| `POST /api/v1/procurement/convert-requisition` | `handleConvertRequisition` | no | - | document |
| `POST /api/v1/procurement/design-attribute` | `handleAddDesignAttributeValue` | no | - | document |
| `POST /api/v1/procurement/design/match` | `handleMatchDesign` | n.a. (a question, not a command) | - | document |
| `POST /api/v1/procurement/purchase-order/preview` | `handlePreviewPurchaseOrder` | n.a. (a question, not a command) | - | document |
| `POST /api/v1/procurement/purchase-order/{id}/images` | `handleUploadPOImage` | no | - | document |
| `POST /api/v1/procurement/purchase-order/{id}/images/main` | `handlePOImageMain` | no | - | document |
| `POST /api/v1/procurement/purchase-order/{id}/images/remove` | `handlePOImageRemove` | no | - | document |
| `POST /api/v1/procurement/purchase-order/{id}/send` | `handleSendPurchaseOrder` | no | - | document |
| `POST /api/v1/procurement/purchase-order/{id}/short-close` | `handlePOShortClose` | no | - | document |
| `POST /api/v1/procurement/purchase-returns/{id}/post` | `handlePostPurchaseReturn` | no | - | stock |
| `POST /api/v1/procurement/vendor-invoice/match` | `handleMatchVendorInvoice` | n.a. (a question, not a command) | - | money |
| `POST /api/v1/procurement/vendor-invoice/pay` | `handlePayVendorInvoice` | no | - | money |
| `POST /api/v1/procurement/vendor-invoice/pay-with-tds` | `handlePayVendorInvoiceWithTDS` | no | - | money |
| `POST /api/v1/quality/coa` | `handleCreateCertificateOfAnalysis` | no | - | document |
| `POST /api/v1/quality/coa/{id}/reject` | `handleRejectCertificateOfAnalysis` | no | - | document |
| `POST /api/v1/quality/coa/{id}/release` | `handleReleaseCertificateOfAnalysis` | no | - | document |
| `POST /api/v1/quality/inspection` | `handleRecordQualityInspection` | n.a. (a question, not a command) | - | document |
| `POST /api/v1/quality/inspection/{id}/dispose` | `handleDisposeQualityInspection` | no | - | document |
| `POST /api/v1/quality/maintenance-order` | `handleCreateMaintenanceOrder` | no | - | document |
| `POST /api/v1/quality/maintenance-order/{id}/cancel` | `handleCancelMaintenanceOrder` | no | - | document |
| `POST /api/v1/quality/maintenance-order/{id}/complete` | `handleCompleteMaintenanceOrder` | no | - | stock |
| `POST /api/v1/quality/maintenance-order/{id}/schedule` | `handleScheduleMaintenanceOrder` | no | - | document |
| `POST /api/v1/quality/maintenance-order/{id}/start` | `handleStartMaintenanceOrder` | no | - | document |
| `POST /api/v1/quality/ncr` | `handleCreateNonConformanceReport` | no | - | document |
| `POST /api/v1/quality/ncr/{id}/close` | `handleCloseNonConformanceReport` | no | - | money |
| `POST /api/v1/quality/ncr/{id}/investigate` | `handleInvestigateNonConformanceReport` | no | - | document |
| `POST /api/v1/quality/ncr/{id}/plan-corrective-action` | `handlePlanCorrectiveAction` | n.a. (a question, not a command) | - | document |
| `POST /api/v1/refunds/{id}/approve` | `handleApproveRefundRequest` | no | - | money |
| `POST /api/v1/refunds/{id}/process` | `handleProcessRefundRequest` | no | - | money |
| `POST /api/v1/refunds/{id}/reject` | `handleRejectRefundRequest` | no | - | money |
| `POST /api/v1/reports/export` | `handleCreateReportExport` | n.a. (a question, not a command) | - | document |
| `POST /api/v1/reports/export/{id}/cancel` | `handleCancelReportExport` | no | - | document |
| `POST /api/v1/reports/schedules/{kind}/{id}/{action}` | `handleReportScheduleAction` | yes (command key) | per engine (see handler) | document |
| `POST /api/v1/reserve` | `handleCreateReservation` | no | - | stock |
| `POST /api/v1/returns` | `handleCreateReturnRequest` | yes (command key) | per engine (see handler) | stock |
| `POST /api/v1/returns/{id}/approve` | `handleApproveReturnRequest` | no | - | stock |
| `POST /api/v1/returns/{id}/qc` | `handleApplyReturnQC` | no | - | stock |
| `POST /api/v1/returns/{id}/receive` | `handleReceiveReturnRequest` | no | - | stock |
| `POST /api/v1/returns/{id}/reject` | `handleRejectReturnRequest` | no | - | stock |
| `POST /api/v1/returns/{id}/reverse-pickup` | `handleScheduleReturnReversePickup` | no | - | stock |
| `POST /api/v1/rfq/save` | `handleSaveRFQ` | no | - | document |
| `POST /api/v1/rfq/select-quote` | `handleSelectWinningQuote` | no | - | document |
| `POST /api/v1/rfq/{id}/award` | `handleAwardRFQ` | no | - | document |
| `POST /api/v1/rfq/{id}/decline` | `handleDeclineRFQ` | no | - | document |
| `POST /api/v1/rfq/{id}/invite` | `handleInviteRFQVendors` | no | - | document |
| `POST /api/v1/rfq/{id}/quote` | `handleRecordVendorQuote` | n.a. (a question, not a command) | - | document |
| `POST /api/v1/sales/bulk-upload/apply` | `handleBulkSalesApply` | no | - | document |
| `POST /api/v1/sales/bulk-upload/preview` | `handleBulkSalesPreview` | n.a. (a question, not a command) | - | document |
| `POST /api/v1/security/csp-report` | `handleCSPReport` | n.a. (a question, not a command) | - | document |
| `POST /api/v1/service/ticket` | `handleCreateServiceTicket` | no | - | document |
| `POST /api/v1/service/ticket/{id}/assign` | `handleAssignServiceTicket` | no | - | document |
| `POST /api/v1/service/ticket/{id}/cancel` | `handleCancelServiceTicket` | no | - | document |
| `POST /api/v1/service/ticket/{id}/close` | `handleCloseServiceTicket` | no | - | money |
| `POST /api/v1/service/ticket/{id}/resolve` | `handleResolveServiceTicket` | n.a. (a question, not a command) | - | document |
| `POST /api/v1/service/ticket/{id}/start` | `handleStartServiceTicket` | no | - | document |
| `POST /api/v1/shopify/{channel}/credentials` | `handleShopifyCredentials` | no | - | document |
| `POST /api/v1/shopify/{channel}/do/{action}` | `handleShopifyAction` | no | - | document |
| `POST /api/v1/shopify/{channel}/settings` | `handleShopifySettings` | no | - | document |
| `POST /api/v1/stickers/print` | `handlePrintStickers` | yes (command key) | per engine (see handler) | document |
| `POST /api/v1/stock-locations` | `handleStockLocations` | no | - | stock |
| `POST /api/v1/stock-locations/defaults` | `handleStockLocationDefault` | no | - | stock |
| `POST /api/v1/stock-locations/move` | `handleMoveStock` | no | - | stock |
| `POST /api/v1/stock-locations/stores` | `handleStoreStockLocations` | no | - | stock |
| `POST /api/v1/store-app/counts` | `handleStoreAppCounts` | n.a. (a question, not a command) | - | document |
| `POST /api/v1/store-app/device/check` | `handleStoreAppDeviceCheck` | n.a. (a question, not a command) | - | document |
| `POST /api/v1/store-app/devices` | `handleStoreAppDevices` | no | - | document |
| `POST /api/v1/store-app/devices/{id}/remove` | `handleStoreAppDeviceRemove` | no | - | document |
| `POST /api/v1/store-app/labels` | `handleStoreAppLabels` | no | - | document |
| `POST /api/v1/store-app/orders` | `handleStoreAppOrder` | no | - | document |
| `POST /api/v1/store-app/pin` | `handleStoreAppPIN` | no | - | document |
| `POST /api/v1/store-app/pin/login` | `handleStoreAppPINLogin` | n.a. (a question, not a command) | - | document |
| `POST /api/v1/store-app/pin/remove` | `handleStoreAppPINRemove` | no | - | document |
| `POST /api/v1/store-app/push` | `handleStoreAppPush` | no | - | document |
| `POST /api/v1/store-app/push/off` | `handleStoreAppPushOff` | no | - | document |
| `POST /api/v1/stored-value/cards` | `handleRegisterGiftCards` | no | - | document |
| `POST /api/v1/stored-value/cards/action` | `handleGiftCardAction` | no | - | document |
| `POST /api/v1/transfer/approve-direct` | `handleApproveTransferOrderDirect` | no | - | stock |
| `POST /api/v1/transfer/cancel` | `handleCancelTransferOrder` | no | - | stock |
| `POST /api/v1/transfer/dispatch` | `handleDispatchTransferOrder` | no | - | stock |
| `POST /api/v1/transfer/receive` | `handleReceiveTransferOrder` | no | - | stock |
| `POST /api/v1/transfer/return` | `handleReturnTransferToSource` | no | - | stock |
| `POST /api/v1/transfer/submit` | `handleSubmitTransferOrder` | no | - | stock |
| `POST /api/v1/unicommerce/credentials` | `handleUnicommerceCredentials` | no | - | document |
| `POST /api/v1/unicommerce/order` | `handleUnicommerceOrder` | no | - | document |
| `POST /api/v1/wms/batch/consume` | `handleBatchConsume` | no | - | stock |
| `POST /api/v1/wms/batch/expiry-sweep` | `handleBatchExpirySweep` | no | - | document |
| `POST /api/v1/wms/batch/putaway` | `handleBatchPutaway` | no | - | stock |
| `POST /api/v1/wms/batch/status` | `handleBatchStatus` | no | - | document |
| `POST /api/v1/wms/billing/charges` | `handleCapturedCharges` | no | - | money |
| `POST /api/v1/wms/billing/invoices` | `handleCapturedChargeInvoice` | no | - | money |
| `POST /api/v1/wms/billing/storage/capture` | `handleCaptureStorageCharges` | no | - | document |
| `POST /api/v1/wms/billing/storage/snapshot` | `handleStorageBalanceSnapshot` | no | - | document |
| `POST /api/v1/wms/bin-move` | `handleBinMove` | no | - | stock |
| `POST /api/v1/wms/bin-replenishment/dynamic-pickface/apply` | `handleApplyDynamicPickFaceMinMax` | no | - | stock |
| `POST /api/v1/wms/bin-replenishment/execute` | `handleBinReplenishmentExecute` | no | - | stock |
| `POST /api/v1/wms/bundles/{operation}` | `handleBundleAssembly` | yes (command key) | per engine (see handler) | stock |
| `POST /api/v1/wms/cartonization/suggest` | `handleCartonizationSuggest` | n.a. (a question, not a command) | - | document |
| `POST /api/v1/wms/cartonization/suggest-v2` | `handleCartonizationSuggestV2` | n.a. (a question, not a command) | - | document |
| `POST /api/v1/wms/condition-transition` | `handleBinConditionTransition` | no | - | document |
| `POST /api/v1/wms/cross-dock/check` | `handleCrossDockCheck` | n.a. (a question, not a command) | - | document |
| `POST /api/v1/wms/cross-dock/planned-putaway` | `handlePlannedCrossDockPutaway` | no | - | document |
| `POST /api/v1/wms/cross-dock/putaway` | `handleCrossDockPutaway` | no | - | stock |
| `POST /api/v1/wms/cycle-count/post-adjustment` | `handleRetryCycleCountPost` | no | - | stock |
| `POST /api/v1/wms/cycle-count/reconcile` | `handleReconcileCycleCount` | no | - | stock |
| `POST /api/v1/wms/cycle-count/recount/request` | `handleRequestRecount` | no | - | stock |
| `POST /api/v1/wms/cycle-count/recount/submit` | `handleSubmitRecountValue` | no | - | stock |
| `POST /api/v1/wms/cycle-count/variance-reason` | `handleSetCycleCountVarianceReason` | no | - | stock |
| `POST /api/v1/wms/facility/copy` | `handleFacilityCopy` | no | - | document |
| `POST /api/v1/wms/hold/place` | `handlePlaceHold` | no | - | document |
| `POST /api/v1/wms/loading/complete` | `handleLoadingComplete` | no | - | stock |
| `POST /api/v1/wms/loading/create` | `handleLoadingTaskCreate` | no | - | document |
| `POST /api/v1/wms/loading/depart` | `handleLoadingDepart` | no | - | document |
| `POST /api/v1/wms/loading/scan` | `handleLoadingScan` | n.a. (a question, not a command) | - | document |
| `POST /api/v1/wms/lpn/assign` | `handleLPNAssign` | no | - | stock |
| `POST /api/v1/wms/lpn/deconsolidate` | `handleDeconsolidateLPN` | no | - | stock |
| `POST /api/v1/wms/order-owner` | `handleSetOrderOwner` | no | - | document |
| `POST /api/v1/wms/owner-stock/assign` | `handleOwnerStockAssign` | no | - | document |
| `POST /api/v1/wms/owner-stock/consume` | `handleOwnerStockConsume` | no | - | stock |
| `POST /api/v1/wms/owner-stock/correct` | `handleCorrectOwnerStock` | no | - | stock |
| `POST /api/v1/wms/owner/assign-warehouse` | `handleWarehouseOwnerAssign` | no | - | document |
| `POST /api/v1/wms/pack-complete` | `handlePackCompleteValidated` | no | - | stock |
| `POST /api/v1/wms/pack-scan` | `handlePackScan` | no | - | stock |
| `POST /api/v1/wms/physical-inventory/cancel` | `handlePhysicalInventoryCancel` | no | - | document |
| `POST /api/v1/wms/physical-inventory/close` | `handlePhysicalInventoryClose` | no | - | money |
| `POST /api/v1/wms/physical-inventory/reconcile` | `handlePhysicalInventoryReconcile` | no | - | document |
| `POST /api/v1/wms/physical-inventory/start` | `handlePhysicalInventoryStart` | no | - | document |
| `POST /api/v1/wms/physical-inventory/submit-count` | `handlePhysicalInventorySubmitCount` | no | - | document |
| `POST /api/v1/wms/pick-scan` | `handlePickScan` | no | - | stock |
| `POST /api/v1/wms/putaway` | `handlePutaway` | no | - | stock |
| `POST /api/v1/wms/robotics/event` | `handleRoboticsEvent` | no | - | document |
| `POST /api/v1/wms/serial/putaway` | `handleSerialPutaway` | no | - | stock |
| `POST /api/v1/wms/serial/status` | `handleSerialStatus` | no | - | document |
| `POST /api/v1/wms/short-pick` | `handleShortPick` | no | - | document |
| `POST /api/v1/wms/sortation/assign` | `handleSortSlotAssign` | no | - | document |
| `POST /api/v1/wms/sortation/assign-wave` | `handleSortSlotAssignWave` | no | - | document |
| `POST /api/v1/wms/sortation/clear` | `handleSortSlotClear` | no | - | document |
| `POST /api/v1/wms/sortation/confirm` | `handleSortSlotConfirm` | no | - | document |
| `POST /api/v1/wms/sortation/provision-slots` | `handleSortSlotProvision` | no | - | document |
| `POST /api/v1/wms/tasks/next` | `handleNextTask` | no | - | document |
| `POST /api/v1/wms/tasks/transition` | `handleTransitionTask` | no | - | document |
| `POST /api/v1/wms/transfer/pack` | `handlePackTransferOrder` | no | - | stock |
| `POST /api/v1/wms/vas/complete` | `handleVASTaskComplete` | no | - | stock |
| `POST /api/v1/wms/vas/create` | `handleVASTaskCreate` | no | - | document |
| `POST /api/v1/wms/wave/assign` | `handleWaveAssign` | no | - | stock |
| `POST /api/v1/wms/wave/create` | `handleWaveCreate` | no | - | stock |
| `POST /api/v1/wms/wave/from-selection` | `handleWaveFromSelection` | no | - | stock |
| `POST /api/v1/wms/wave/pick-confirm` | `handleWavePickConfirm` | no | - | stock |
| `POST /api/v1/wms/wave/template/run` | `handleWaveTemplateRun` | no | - | stock |
| `POST /api/v1/wms/wave/transition` | `handleWaveTransition` | no | - | stock |
| `POST /api/v1/wms/yard/check-in` | `handleYardCheckIn` | n.a. (a question, not a command) | - | document |
| `POST /api/v1/wms/yard/{id}/door` | `handleYardAssignDoor` | no | - | document |
| `PUT /api/v1/admin/api-credentials/{id}/limits` | `handleSetAPICredentialLimits` | no | - | document |
| `PUT /api/v1/admin/settings` | `handleUpdateSettings` | no | - | document |
| `PUT /api/v1/inventory/item-setup/fields` | `handleSaveItemFieldRules` | no | - | document |
| `PUT /api/v1/me` | `handleUpdateProfile` | no | - | document |
| `PUT /api/v1/meta/{doctype}/fields/{id}` | `handleUpdateFieldDefinition` | no | - | document |
