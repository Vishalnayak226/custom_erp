# Permission Matrix

<!-- GENERATED FILE - DO NOT EDIT BY HAND.
     Source: the tenant's own `role_permissions` table
     Regenerate: `go run ./cmd/gendocs -db "postgres://..."` -->

> **Generated 2026-08-01.** This page is produced from the tenant's own `role_permissions` table, so it cannot drift from
> the running system. Hand edits are lost on the next run - change the source instead.

What each role may do with each record type, read from the default tenant's
grants: **106 record types across 3 roles.**

**HR/Admin is not listed** - it always has full access to everything and needs no
grant rows. A role with **no row at all** for a record type has **no access to
it**: this system fails closed, so a missing grant is a denial, never a default
allow.

Legend: **R** read - **C** create - **U** update - **D** delete - `-` none

An administrator changes any of this on **Settings -> Roles** (ADMIN_SOP §A.2).
Since Stage 30.5.7 the app also *hides* what a role cannot do - no **New** or
**Bulk Import** button without create, no row **Edit**/**Delete** icons without
update/delete - so this table also predicts what each role actually sees.

| Record type | Cashier | HR/Admin | Store Manager |
|---|---|---|---|
| **ASN** | - | R C U D | R C U |
| **AllocationRule** | - | R C U D | R |
| **Appraisal** | - | R C U D | R C U |
| **AppraisalCycle** | - | R C U D | R |
| **Asset** | - | R C U D | R |
| **Attendance** | - | R C U D | R C U |
| **BOM** | - | R C U D | R |
| **BackdatedPostingRequest** | - | R C U D | R C U |
| **BankAccount** | - | R C U D | R |
| **BankStatementLine** | - | R C U D | R C |
| **Bin** | - | R C U D | R C U |
| **BinReplenishmentRule** | - | R C U D | R C U |
| **Brand** | R | R C U D | - |
| **Campaign** | - | R C U D | R C U |
| **CartonType** | - | R C U D | R C U |
| **Channel** | - | R C U D | R |
| **ChannelCategoryMap** | - | R C U D | R |
| **ChannelFieldMap** | - | R C U D | R |
| **ChannelValidationRule** | - | R C U D | R |
| **Color** | R | R C U D | - |
| **CostCenter** | - | R C U D | R |
| **CourierServiceArea** | - | R C U D | R |
| **CreditNote** | - | R C U D | R C |
| **Customer** | R | R C U D | R |
| **CycleCountLine** | - | R C | R C |
| **DebitNote** | - | R C U D | R C |
| **Department** | - | R C U D | R |
| **Employee** | - | R C U D | R |
| **EmployeeLoan** | R | R C U D | R C |
| **ExpenseClaim** | R C U | R C U D | R C U |
| **FulfillmentTask** | R C U | R C U D | - |
| **GLPost** | - | R C | - |
| **GRN** | - | R C U D | - |
| **Grievance** | R C U | R C U D | R C U |
| **ImportJob** | - | R C U D | R C |
| **Item** | R | R C U D | R |
| **JournalVoucher** | - | R C U D | R C U |
| **LPN** | - | R C U D | R C U |
| **Leave** | R C | R C U D | R C U |
| **LegalEntity** | - | R C U D | R |
| **Location** | - | R C U D | R |
| **LogisticsBooking** | R C U | R C U D | - |
| **LoyaltyRedemptionRequest** | - | R C U D | R C U |
| **Manifest** | R | R C U D | R C U |
| **MarketplaceSettlement** | R C U | R C U D | - |
| **NotificationChannelConfig** | - | R C U D | R |
| **NotificationLog** | - | R | R |
| **NotificationTemplate** | - | R C U D | R |
| **Offer** | R | R C U D | R C U |
| **OnboardingChecklist** | - | R C U D | R C U |
| **PIMProductProfile** | - | R | R |
| **POSCart** | R C U | R C U D | - |
| **POSInvoice** | R C | R C U D | - |
| **POSOfflineQueueGap** | - | R U | R |
| **POSOfflineSyncVariance** | - | R U | R |
| **POSProfile** | - | R C U D | R C U |
| **POSSession** | R | R | R |
| **PaymentProposal** | - | R | R |
| **Payslip** | R | R C U D | R |
| **Printer** | R | R C U D | R |
| **ProductAttributeDef** | - | R C U D | R |
| **ProductAttributeGroup** | - | R C U D | R |
| **ProductAttributeValue** | - | R C U D | R C U |
| **ProductContent** | - | R C U D | R C U |
| **ProductFamily** | - | R C U D | R |
| **ProductFamilyAttribute** | - | R C U D | R |
| **ProductMedia** | - | R C U D | R C U |
| **ProductionOrder** | - | R C U D | R C U |
| **PurchaseOrder** | - | R C U D | R U |
| **PurchaseRequisition** | - | R C U D | R C U |
| **PurchaseRequisitionDescription** | - | R C U D | R |
| **QualityInspection** | - | R C U D | R C U |
| **RFQ** | - | R C U D | R C U |
| **ReasonCode** | - | R C U D | R |
| **RefundRequest** | R | R C U D | R C U |
| **ReportColumnProfile** | R C U D | R C U D | R C U D |
| **ReportExportJob** | R | R | R |
| **ReportFilterPreset** | R C U D | R C U D | R C U D |
| **ReportRunLog** | - | R | - |
| **ReturnRequest** | R C | R C U D | R C U |
| **RoboticsIntegrationCredential** | - | R C U D | - |
| **Routing** | - | R C U D | R C U |
| **SalaryStructure** | - | R C U D | R |
| **SalesInvoice** | - | R C U D | R C U |
| **SalesOrder** | R C | R C U D | R C U |
| **SalesOrderLine** | R C | R C U D | R C U |
| **SalesReturn** | R C | R C U D | - |
| **ScheduledReport** | - | R C U D | R C U |
| **Shift** | - | R C U D | R C U |
| **ShiftAssignment** | R | R C U D | R C U |
| **StatusTransitionRule** | - | R C U D | R |
| **StockLedgerEntry** | R | R C | - |
| **StorageBillingRate** | - | R C U D | R |
| **Stores** | - | R C U D | R |
| **Style** | R | R C U D | - |
| **SubcontractOrder** | - | R C U D | R C U |
| **TDSSection** | - | R C U D | R |
| **TaskCompletionLog** | - | R | R |
| **TrainingProgram** | - | R C U D | R |
| **TrainingRecord** | - | R C U D | R C U |
| **TransferOrder** | - | R C U D | - |
| **Vendor** | - | R C U D | R |
| **VendorInvoice** | - | R C U D | R C U |
| **VendorQuote** | - | R C U D | R C U |
| **Voucher** | - | R C U D | R C U |
| **WorkCenter** | - | R C U D | R C U |
