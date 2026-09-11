# Data dictionary

<!-- GENERATED FILE - DO NOT EDIT BY HAND.
     Source: `docs/data/registry-snapshot.json`, `business-definitions.json`, sensitive-field and report registries
     Regenerate: `pwsh docs/update-docs.ps1 -Group Content` -->

> **Generated 2026-09-10.** This page is produced from `docs/data/registry-snapshot.json`, `business-definitions.json`, sensitive-field and report registries, for this source revision.
> It is not release or tenant assurance. Hand edits are lost on the next run - change the source instead.

## Scope and provenance

Metadata captured **2026-09-09**, environment **development**, schema **tenant_default**. Snapshot SHA-256: `d8efb92702e82cf5fd61cb9b57b2e5c9ed107ae116352deacc2f20b0a0cb3393`. This is a development structure snapshot, not a claim that any deployed tenant has this schema.

[Machine-readable dictionary](dictionary.json) contains every captured field, type, required flag, relationship, physical column/key, sensitive-field policy and report definition. [Capture procedure](../dictionary-workflow.md) explains refresh and review. Field labels describe the configured form; source validation and database constraints remain authoritative. No customer records or default values are exported.

## Stewardship and interpretation

Owner: **data-owner**. Business definitions are **draft**, review due **2026-10-09**.

- Key policy: Physical primary and unique keys are listed in registry.keys. JSON business identifiers and numbering rules are additional application controls; do not infer global uniqueness from a field label.
- Tenant scope: The captured schema is tenant_default in development. Tenant, entity, location and owner authorization must be checked by the service; copying a document identifier does not grant access.
- Retention: No universal duration is approved. Apply the signed jurisdiction, document-class and customer retention schedule and legal holds before purge; preserve required audit and transactional records.
- Classification: The sensitive-field registry identifies known protected fields. Every other field needs process-owner classification before export; absence from that registry is not a public-data designation.

## Document types

| Document type | Module | Kind | Fields | Business meaning |
|---|---|---|---:|---|
| AllocationRule | OMS | Master | 6 | Configured ERP document; validate the field labels and owning process with the implementation data steward before import. |
| AllocationStrategy | Inventory | Master | 5 | Stock, storage and movement records managed by the inventory process owner. |
| Appointment | Inventory | Transaction | 11 | Stock, storage and movement records managed by the inventory process owner. |
| Appraisal | HR | Transaction | 7 | Employee and workforce records managed by the HR process owner. |
| AppraisalCycle | HR | Master | 5 | Employee and workforce records managed by the HR process owner. |
| ASN | Inbound | Transaction | 10 | Configured ERP document; validate the field labels and owning process with the implementation data steward before import. |
| Asset | Finance | Transaction | 13 | Accounting and settlement records managed by the finance process owner. |
| Attendance | HR | Transaction | 5 | Employee and workforce records managed by the HR process owner. |
| BackdatedPostingRequest | Finance | Transaction | 5 | Accounting and settlement records managed by the finance process owner. |
| BankAccount | Finance | Master | 6 | Accounting and settlement records managed by the finance process owner. |
| BankStatementLine | Finance | Transaction | 7 | Accounting and settlement records managed by the finance process owner. |
| Batch | Master Data | Master | 13 | Configured ERP document; validate the field labels and owning process with the implementation data steward before import. |
| Bin | Inventory | Master | 12 | Stock, storage and movement records managed by the inventory process owner. |
| BinReplenishmentRule | Inventory | Master | 6 | Stock, storage and movement records managed by the inventory process owner. |
| BOM | Manufacturing | Master | 10 | Configured ERP document; validate the field labels and owning process with the implementation data steward before import. |
| Brand | Master Data | Master | 6 | Configured ERP document; validate the field labels and owning process with the implementation data steward before import. |
| Budget | Finance | Master | 7 | Accounting and settlement records managed by the finance process owner. |
| BundleAssembly | OMS | Transaction | 11 | Configured ERP document; validate the field labels and owning process with the implementation data steward before import. |
| Campaign | CRM | Master | 6 | Configured ERP document; validate the field labels and owning process with the implementation data steward before import. |
| CapturedCharge | Inventory | Master | 14 | Stock, storage and movement records managed by the inventory process owner. |
| CartonType | Inventory | Master | 6 | Stock, storage and movement records managed by the inventory process owner. |
| CertificateOfAnalysis | Quality | Transaction | 7 | Configured ERP document; validate the field labels and owning process with the implementation data steward before import. |
| Channel | PIM | Master | 13 | Configured ERP document; validate the field labels and owning process with the implementation data steward before import. |
| ChannelCategoryMap | PIM | Master | 4 | Configured ERP document; validate the field labels and owning process with the implementation data steward before import. |
| ChannelFieldMap | PIM | Master | 6 | Configured ERP document; validate the field labels and owning process with the implementation data steward before import. |
| ChannelSKUException | OMS | Transaction | 8 | Configured ERP document; validate the field labels and owning process with the implementation data steward before import. |
| ChannelSyncRun | OMS | Transaction | 9 | Configured ERP document; validate the field labels and owning process with the implementation data steward before import. |
| ChannelValidationRule | PIM | Master | 5 | Configured ERP document; validate the field labels and owning process with the implementation data steward before import. |
| ChargeCode | Inventory | Master | 14 | Stock, storage and movement records managed by the inventory process owner. |
| ChargeContract | Inventory | Master | 10 | Stock, storage and movement records managed by the inventory process owner. |
| ChargeGroup | Inventory | Master | 3 | Stock, storage and movement records managed by the inventory process owner. |
| Color | Master Data | Master | 6 | Configured ERP document; validate the field labels and owning process with the implementation data steward before import. |
| CompetitorPrice | PIM | Master | 12 | Configured ERP document; validate the field labels and owning process with the implementation data steward before import. |
| ContentAssistLog | PIM | Transaction | 10 | Configured ERP document; validate the field labels and owning process with the implementation data steward before import. |
| CostCenter | Core | Master | 3 | Configured ERP document; validate the field labels and owning process with the implementation data steward before import. |
| CourierServiceArea | OMS | Master | 5 | Configured ERP document; validate the field labels and owning process with the implementation data steward before import. |
| CourierTrackingEvent | OMS | Transaction | 6 | Configured ERP document; validate the field labels and owning process with the implementation data steward before import. |
| CreditNote | Finance | Transaction | 8 | Accounting and settlement records managed by the finance process owner. |
| CrossDockPlan | Inventory | Transaction | 8 | Stock, storage and movement records managed by the inventory process owner. |
| Currency | Finance | Master | 5 | Accounting and settlement records managed by the finance process owner. |
| Customer | Sales | Master | 13 | Party receiving goods or services; access and consent must follow the approved customer process. |
| CycleClass | Inventory | Master | 6 | Stock, storage and movement records managed by the inventory process owner. |
| CycleCountLine | Inventory | Transaction | 12 | Stock, storage and movement records managed by the inventory process owner. |
| DashboardDigest | Reports | Master | 9 | Configured ERP document; validate the field labels and owning process with the implementation data steward before import. |
| DashboardLayout | Reports | Master | 5 | Configured ERP document; validate the field labels and owning process with the implementation data steward before import. |
| DebitNote | Finance | Transaction | 6 | Accounting and settlement records managed by the finance process owner. |
| DeferredRevenueSchedule | Finance | Transaction | 7 | Accounting and settlement records managed by the finance process owner. |
| Department | Core | Master | 3 | Configured ERP document; validate the field labels and owning process with the implementation data steward before import. |
| DockDoor | Inventory | Master | 8 | Stock, storage and movement records managed by the inventory process owner. |
| Employee | HR | Master | 12 | Employee and workforce records managed by the HR process owner. |
| EmployeeLoan | HR | Transaction | 7 | Employee and workforce records managed by the HR process owner. |
| ExchangeRate | Finance | Master | 10 | Accounting and settlement records managed by the finance process owner. |
| ExpenseClaim | Finance | Transaction | 14 | Accounting and settlement records managed by the finance process owner. |
| FloorAssistRequest | Inventory | Transaction | 4 | Stock, storage and movement records managed by the inventory process owner. |
| FulfillmentTask | Inventory | Transaction | 6 | Stock, storage and movement records managed by the inventory process owner. |
| GatePass | OMS | Transaction | 11 | Configured ERP document; validate the field labels and owning process with the implementation data steward before import. |
| GLPost | Finance | Transaction | 5 | Accounting and settlement records managed by the finance process owner. |
| Grievance | HR | Transaction | 6 | Employee and workforce records managed by the HR process owner. |
| GRN | Procurement | Transaction | 6 | Supplier purchasing records managed by the procurement process owner. |
| HelpArticleFeedback | Core | Transaction | 3 | Configured ERP document; validate the field labels and owning process with the implementation data steward before import. |
| Hold | Inventory | Transaction | 7 | Stock, storage and movement records managed by the inventory process owner. |
| HoldCode | Inventory | Master | 4 | Stock, storage and movement records managed by the inventory process owner. |
| HoldReleaseRequest | Inventory | Transaction | 3 | Stock, storage and movement records managed by the inventory process owner. |
| ImportJob | PIM | Transaction | 7 | Configured ERP document; validate the field labels and owning process with the implementation data steward before import. |
| InspectionPlan | Quality | Master | 5 | Configured ERP document; validate the field labels and owning process with the implementation data steward before import. |
| IntercompanyTransaction | Finance | Transaction | 9 | Accounting and settlement records managed by the finance process owner. |
| Item | Inventory | Master | 24 | Product master identifying a sellable, purchasable or stocked SKU; operational settings control tracking and validation. |
| JournalVoucher | Finance | Transaction | 13 | Accounting and settlement records managed by the finance process owner. |
| LaborAllowance | Inventory | Master | 5 | Stock, storage and movement records managed by the inventory process owner. |
| LaborElement | Inventory | Master | 6 | Stock, storage and movement records managed by the inventory process owner. |
| LaborOperation | Inventory | Master | 6 | Stock, storage and movement records managed by the inventory process owner. |
| LaborStandard | Inventory | Master | 7 | Stock, storage and movement records managed by the inventory process owner. |
| LandedCostVoucher | Finance | Transaction | 4 | Accounting and settlement records managed by the finance process owner. |
| Leave | HR | Transaction | 7 | Employee and workforce records managed by the HR process owner. |
| LegalEntity | Core | Master | 5 | Configured ERP document; validate the field labels and owning process with the implementation data steward before import. |
| LoadingTask | Inventory | Transaction | 11 | Stock, storage and movement records managed by the inventory process owner. |
| Location | Core | Master | 13 | Configured ERP document; validate the field labels and owning process with the implementation data steward before import. |
| LogisticsBooking | Inventory | Transaction | 20 | Stock, storage and movement records managed by the inventory process owner. |
| LottableConstraint | Master Data | Master | 6 | Configured ERP document; validate the field labels and owning process with the implementation data steward before import. |
| LoyaltyRedemptionRequest | CRM | Transaction | 5 | Configured ERP document; validate the field labels and owning process with the implementation data steward before import. |
| LPN | Inventory | Master | 4 | Stock, storage and movement records managed by the inventory process owner. |
| MaintenanceOrder | Quality | Transaction | 7 | Configured ERP document; validate the field labels and owning process with the implementation data steward before import. |
| MaintenanceSchedule | Quality | Master | 6 | Configured ERP document; validate the field labels and owning process with the implementation data steward before import. |
| Manifest | OMS | Transaction | 5 | Configured ERP document; validate the field labels and owning process with the implementation data steward before import. |
| MarketplaceSettlement | Finance | Transaction | 7 | Accounting and settlement records managed by the finance process owner. |
| MarketplaceSettlementLine | Finance | Transaction | 12 | Accounting and settlement records managed by the finance process owner. |
| Model | Master Data | Master | 5 | Configured ERP document; validate the field labels and owning process with the implementation data steward before import. |
| NDRCase | OMS | Transaction | 7 | Configured ERP document; validate the field labels and owning process with the implementation data steward before import. |
| NonConformanceReport | Quality | Transaction | 7 | Configured ERP document; validate the field labels and owning process with the implementation data steward before import. |
| NotificationChannelConfig | OMS | Master | 4 | Configured ERP document; validate the field labels and owning process with the implementation data steward before import. |
| NotificationLog | OMS | Transaction | 7 | Configured ERP document; validate the field labels and owning process with the implementation data steward before import. |
| NotificationTemplate | OMS | Master | 6 | Configured ERP document; validate the field labels and owning process with the implementation data steward before import. |
| Offer | POS | Master | 20 | Configured ERP document; validate the field labels and owning process with the implementation data steward before import. |
| OMSSavedView | OMS | Transaction | 4 | Configured ERP document; validate the field labels and owning process with the implementation data steward before import. |
| OnboardingChecklist | HR | Transaction | 6 | Employee and workforce records managed by the HR process owner. |
| PackingValidationTemplate | Inventory | Master | 7 | Stock, storage and movement records managed by the inventory process owner. |
| PackStation | Inventory | Master | 5 | Stock, storage and movement records managed by the inventory process owner. |
| PackTemplate | Inventory | Master | 9 | Stock, storage and movement records managed by the inventory process owner. |
| PaymentProposal | Finance | Transaction | 4 | Accounting and settlement records managed by the finance process owner. |
| Payslip | HR | Transaction | 12 | Employee and workforce records managed by the HR process owner. |
| PhysicalInventory | Inventory | Transaction | 8 | Stock, storage and movement records managed by the inventory process owner. |
| PIMCatalog | PIM | Master | 7 | Configured ERP document; validate the field labels and owning process with the implementation data steward before import. |
| PIMExportSchedule | PIM | Master | 10 | Configured ERP document; validate the field labels and owning process with the implementation data steward before import. |
| PIMExportTemplate | PIM | Master | 8 | Configured ERP document; validate the field labels and owning process with the implementation data steward before import. |
| PIMImportSchedule | PIM | Master | 11 | Configured ERP document; validate the field labels and owning process with the implementation data steward before import. |
| PIMImportTemplate | PIM | Master | 6 | Configured ERP document; validate the field labels and owning process with the implementation data steward before import. |
| PIMProductGroup | PIM | Master | 10 | Configured ERP document; validate the field labels and owning process with the implementation data steward before import. |
| PIMProductProfile | PIM | Transaction | 6 | Configured ERP document; validate the field labels and owning process with the implementation data steward before import. |
| PIMTask | PIM | Transaction | 18 | Configured ERP document; validate the field labels and owning process with the implementation data steward before import. |
| PIMTaskTemplate | PIM | Master | 10 | Configured ERP document; validate the field labels and owning process with the implementation data steward before import. |
| PIMTransformRule | PIM | Master | 5 | Configured ERP document; validate the field labels and owning process with the implementation data steward before import. |
| PIMWorkflowDefinition | PIM | Master | 5 | Configured ERP document; validate the field labels and owning process with the implementation data steward before import. |
| PIMWorkflowRun | PIM | Transaction | 10 | Configured ERP document; validate the field labels and owning process with the implementation data steward before import. |
| POSCart | Sales | Transaction | 7 | Customer demand and selling records managed by the sales process owner. |
| POSCostingGap | Sales | Transaction | 5 | Customer demand and selling records managed by the sales process owner. |
| POSInvoice | POS | Transaction | 5 | Configured ERP document; validate the field labels and owning process with the implementation data steward before import. |
| POSOfflineQueueGap | Sales | Transaction | 6 | Customer demand and selling records managed by the sales process owner. |
| POSOfflineSyncVariance | Sales | Transaction | 6 | Customer demand and selling records managed by the sales process owner. |
| POSPriceOverride | Sales | Transaction | 11 | Customer demand and selling records managed by the sales process owner. |
| POSProfile | Sales | Master | 6 | Customer demand and selling records managed by the sales process owner. |
| POSSession | Sales | Transaction | 8 | Customer demand and selling records managed by the sales process owner. |
| PrepaidExpenseSchedule | Finance | Transaction | 8 | Accounting and settlement records managed by the finance process owner. |
| PreShipValidationRule | Inventory | Master | 6 | Stock, storage and movement records managed by the inventory process owner. |
| PriceListVersion | Sales | Master | 7 | Customer demand and selling records managed by the sales process owner. |
| Printer | Inventory | Master | 10 | Stock, storage and movement records managed by the inventory process owner. |
| ProductAttributeDef | PIM | Master | 6 | Configured ERP document; validate the field labels and owning process with the implementation data steward before import. |
| ProductAttributeGroup | PIM | Master | 4 | Configured ERP document; validate the field labels and owning process with the implementation data steward before import. |
| ProductAttributeValue | PIM | Transaction | 7 | Configured ERP document; validate the field labels and owning process with the implementation data steward before import. |
| ProductBundle | OMS | Master | 8 | Configured ERP document; validate the field labels and owning process with the implementation data steward before import. |
| ProductContent | PIM | Transaction | 11 | Configured ERP document; validate the field labels and owning process with the implementation data steward before import. |
| ProductFamily | PIM | Master | 4 | Configured ERP document; validate the field labels and owning process with the implementation data steward before import. |
| ProductFamilyAttribute | PIM | Master | 5 | Configured ERP document; validate the field labels and owning process with the implementation data steward before import. |
| ProductionOrder | Manufacturing | Transaction | 10 | Configured ERP document; validate the field labels and owning process with the implementation data steward before import. |
| ProductMedia | PIM | Transaction | 15 | Configured ERP document; validate the field labels and owning process with the implementation data steward before import. |
| Project | Finance | Master | 4 | Accounting and settlement records managed by the finance process owner. |
| PurchaseOrder | Procurement | Transaction | 16 | Supplier purchasing records managed by the procurement process owner. |
| PurchaseRequisition | Procurement | Transaction | 6 | Supplier purchasing records managed by the procurement process owner. |
| PurchaseRequisitionDescription | Procurement | Master | 3 | Supplier purchasing records managed by the procurement process owner. |
| PutawayStrategy | Inventory | Master | 5 | Stock, storage and movement records managed by the inventory process owner. |
| QualityInspection | Manufacturing | Transaction | 5 | Configured ERP document; validate the field labels and owning process with the implementation data steward before import. |
| RateGroup | Inventory | Master | 7 | Stock, storage and movement records managed by the inventory process owner. |
| ReasonCode | OMS | Master | 6 | Configured ERP document; validate the field labels and owning process with the implementation data steward before import. |
| ReceiptValidationRule | Inventory | Master | 4 | Stock, storage and movement records managed by the inventory process owner. |
| RecurringSalesContract | Sales | Master | 7 | Customer demand and selling records managed by the sales process owner. |
| RefundRequest | OMS | Transaction | 8 | Configured ERP document; validate the field labels and owning process with the implementation data steward before import. |
| ReorderPointConfig | Inventory | Master | 6 | Stock, storage and movement records managed by the inventory process owner. |
| ReportColumnProfile | Reports | Master | 6 | Configured ERP document; validate the field labels and owning process with the implementation data steward before import. |
| ReportExportJob | Reports | Transaction | 6 | Configured ERP document; validate the field labels and owning process with the implementation data steward before import. |
| ReportFilterPreset | Reports | Master | 5 | Configured ERP document; validate the field labels and owning process with the implementation data steward before import. |
| ReportRunLog | Reports | Transaction | 4 | Configured ERP document; validate the field labels and owning process with the implementation data steward before import. |
| ReturnRequest | OMS | Transaction | 11 | Configured ERP document; validate the field labels and owning process with the implementation data steward before import. |
| RFQ | Procurement | Transaction | 5 | Supplier purchasing records managed by the procurement process owner. |
| RoboticsIntegrationCredential | Inventory | Master | 3 | Stock, storage and movement records managed by the inventory process owner. |
| Routing | Manufacturing | Master | 4 | Configured ERP document; validate the field labels and owning process with the implementation data steward before import. |
| SalaryStructure | HR | Master | 10 | Employee and workforce records managed by the HR process owner. |
| SalesInvoice | Sales | Transaction | 19 | Customer demand and selling records managed by the sales process owner. |
| SalesOrder | OMS | Transaction | 14 | Configured ERP document; validate the field labels and owning process with the implementation data steward before import. |
| SalesOrderLine | OMS | Transaction | 14 | Configured ERP document; validate the field labels and owning process with the implementation data steward before import. |
| SalesReturn | Sales | Transaction | 3 | Customer demand and selling records managed by the sales process owner. |
| ScheduledReport | Reports | Master | 10 | Configured ERP document; validate the field labels and owning process with the implementation data steward before import. |
| SerialNumber | Master Data | Master | 10 | Configured ERP document; validate the field labels and owning process with the implementation data steward before import. |
| ServiceContract | Service | Master | 9 | Configured ERP document; validate the field labels and owning process with the implementation data steward before import. |
| ServiceTicket | Service | Transaction | 12 | Configured ERP document; validate the field labels and owning process with the implementation data steward before import. |
| Shift | HR | Master | 7 | Employee and workforce records managed by the HR process owner. |
| ShiftAssignment | HR | Transaction | 6 | Employee and workforce records managed by the HR process owner. |
| ShippingPackage | OMS | Transaction | 15 | Configured ERP document; validate the field labels and owning process with the implementation data steward before import. |
| Size | Master Data | Master | 2 | Configured ERP document; validate the field labels and owning process with the implementation data steward before import. |
| SortSlot | Inventory | Transaction | 8 | Stock, storage and movement records managed by the inventory process owner. |
| SortStation | Inventory | Master | 5 | Stock, storage and movement records managed by the inventory process owner. |
| StatusTransitionRule | OMS | Master | 7 | Configured ERP document; validate the field labels and owning process with the implementation data steward before import. |
| StockLedgerEntry | Inventory | Transaction | 15 | Stock, storage and movement records managed by the inventory process owner. |
| StorageBalanceSnapshot | Inventory | Master | 7 | Stock, storage and movement records managed by the inventory process owner. |
| StorageBillingRate | Inventory | Master | 11 | Stock, storage and movement records managed by the inventory process owner. |
| Style | Master Data | Master | 9 | Configured ERP document; validate the field labels and owning process with the implementation data steward before import. |
| SubcontractOrder | Manufacturing | Transaction | 11 | Configured ERP document; validate the field labels and owning process with the implementation data steward before import. |
| SupplierSubmission | PIM | Transaction | 13 | Configured ERP document; validate the field labels and owning process with the implementation data steward before import. |
| TaskCompletionLog | Inventory | Transaction | 5 | Stock, storage and movement records managed by the inventory process owner. |
| TaskDispatchStrategy | Inventory | Master | 5 | Stock, storage and movement records managed by the inventory process owner. |
| TDSSection | Finance | Master | 5 | Accounting and settlement records managed by the finance process owner. |
| Trailer | Inventory | Master | 5 | Stock, storage and movement records managed by the inventory process owner. |
| TrainingProgram | HR | Master | 4 | Employee and workforce records managed by the HR process owner. |
| TrainingRecord | HR | Transaction | 6 | Employee and workforce records managed by the HR process owner. |
| TransferOrder | Inventory | Transaction | 5 | Stock, storage and movement records managed by the inventory process owner. |
| TravelSection | Inventory | Master | 5 | Stock, storage and movement records managed by the inventory process owner. |
| UOM | Master Data | Master | 3 | Configured ERP document; validate the field labels and owning process with the implementation data steward before import. |
| UOMConversion | Master Data | Master | 5 | Configured ERP document; validate the field labels and owning process with the implementation data steward before import. |
| UserWorkSchedule | Inventory | Master | 6 | Stock, storage and movement records managed by the inventory process owner. |
| Vendor | Procurement | Master | 10 | Party supplying goods or services; procurement maintains identity and commercial references. |
| VendorInvoice | Procurement | Transaction | 10 | Supplier purchasing records managed by the procurement process owner. |
| VendorQuote | Procurement | Transaction | 6 | Supplier purchasing records managed by the procurement process owner. |
| Voucher | CRM | Master | 7 | Configured ERP document; validate the field labels and owning process with the implementation data steward before import. |
| WarehouseTask | Inventory | Transaction | 20 | Stock, storage and movement records managed by the inventory process owner. |
| Wave | Inventory | Transaction | 7 | Stock, storage and movement records managed by the inventory process owner. |
| WaveTemplate | Inventory | Master | 10 | Stock, storage and movement records managed by the inventory process owner. |
| WebhookSubscription | Integrations | Master | 6 | Configured ERP document; validate the field labels and owning process with the implementation data steward before import. |
| WeeklySchedule | Inventory | Master | 6 | Stock, storage and movement records managed by the inventory process owner. |
| WorkCenter | Manufacturing | Master | 5 | Configured ERP document; validate the field labels and owning process with the implementation data steward before import. |
| YardCheckIn | Inventory | Transaction | 9 | Stock, storage and movement records managed by the inventory process owner. |
| Zone | Inventory | Master | 8 | Stock, storage and movement records managed by the inventory process owner. |

## Completeness

199 document types, 1542 configured fields, 449 physical columns and 98 key-column memberships were captured. Link targets describe configured relations; not every JSON document relation is a physical foreign key. Unlisted sensitive fields are **unclassified**, not automatically public. Review business definitions and retention before customer use. Report labels, parameters and columns come from the source report registry in the JSON projection.
