---
title: Report catalog
section: Reference
order: 20
summary: Every report you can run, what it asks for, what it returns and whether you can drill into it.
audience: store manager, finance, category manager, admin
last_verified: 2026-09-10
owner: documentation-maintainer
status: active
topic_type: reference
module: platform
task: Report catalog
prerequisites: Authorized access to the relevant ERP task
applies_to: source registries; not release acceptance
screens: [reports]
---

<!-- GENERATED ARTICLE - DO NOT EDIT BY HAND.
     Regenerate: pwsh docs/update-docs.ps1 -Group Content -->

# Report catalog

**382** reports across **26** categories, all reachable from
**Sales & Marketplace » Reports**.

Pick one from the catalog list on that screen, fill in any parameter marked
**required**, and run it. Where a report supports drill-down, clicking a figure
opens the individual transactions behind it. **Export in Background** queues a
CSV for any report large enough to time out a normal request.

Columns marked *sensitive* are masked as `•••` for roles other than Super
Admin and Store Manager. They are masked rather than dropped, so the table has
the same shape whoever runs it and a screenshot from one person lines up with a
screenshot from another.

The list runs over **3** pages. The contents below cover every category and
open the right page.

## Contents

- [Accounting](#accounting) - 26 reports
- [Admin](#admin) - 1 reports
- [Assets](#assets) - 1 reports
- [BI](#bi) - 1 reports
- [CRM](#crm) - 19 reports
- [E-commerce & Integration](#e-commerce-integration) - 12 reports
- [Exceptions](#exceptions) - 6 reports
- [Expense Management](#expense-management) - 11 reports
- [Finance](#finance) - 33 reports
- [GST & Compliance](#gst-compliance) - 8 reports
- [HR](#hr) - 2 reports
- [HRMS](#hrms) - 15 reports
- [Inventory](report-catalog-2.md#inventory) - 33 reports
- [MIS & Management](report-catalog-2.md#mis-management) - 10 reports
- [Manufacturing](report-catalog-2.md#manufacturing) - 19 reports
- [Master Data & Audit](report-catalog-2.md#master-data-audit) - 11 reports
- [OMS](report-catalog-2.md#oms) - 25 reports
- [PIM](report-catalog-2.md#pim) - 4 reports
- [PIM / PIMS](report-catalog-2.md#pim-pims) - 16 reports
- [Procurement](report-catalog-2.md#procurement) - 12 reports
- [Procurement & GRN](report-catalog-3.md#procurement-grn) - 27 reports
- [Sales](report-catalog-3.md#sales) - 9 reports
- [Sales & POS](report-catalog-3.md#sales-pos) - 22 reports
- [Service](report-catalog-3.md#service) - 1 reports
- [Stock Transfer](report-catalog-3.md#stock-transfer) - 9 reports
- [WMS](report-catalog-3.md#wms) - 49 reports

## Accounting

### AP Aging

**Report id:** `cat-ap-aging`

**Parameters:**

- **Store** (`location`) - text, optional

**Columns:** Vendor Code · Vendor Name · Vendor Invoice No · Posting Date · Due Date · Outstanding Amount · Aging Bucket · Status

**Drill-down:** no.

### AR Aging

**Report id:** `cat-ar-aging`

**Parameters:**

- **Store** (`location`) - text, optional
- **Status (optional)** (`status`) - select, optional, one of: ,Approved,Posted,Partially Paid,Paid,Overdue

**Columns:** Customer ID · Customer Name · Sales Invoice No · Posting Date · Due Date · Outstanding Amount · Aging Bucket · Status

**Drill-down:** no.

### Accounting Audit Trail

**Report id:** `cat-accounting-audit-trail`

**Parameters:**

- **From** (`from`) - date, optional
- **To** (`to`) - date, optional

**Columns:** Audit ID · Posting Date · Voucher No · Document Type · Document No · Edit Action · Field Changed · Old Value · New Value · User Name · Edited At

**Drill-down:** no.

### Balance Sheet

**Report id:** `cat-balance-sheet`

**Parameters:**

- **From** (`from`) - date, optional
- **To** (`to`) - date, optional

**Columns:** As Of Date · GL Account · Balance

**Drill-down:** no.

### Bank Reconciliation Report

**Report id:** `cat-bank-reconciliation-report`

**Parameters:**

- **From** (`from`) - date, optional
- **To** (`to`) - date, optional

**Columns:** Posting Date · Bank Account · Bank Statement Ref · Voucher No · Reference No · Debit Amount · Credit Amount · Reconciliation Status · Remarks

**Drill-down:** no.

### Budget vs Actual

**Report id:** `cat-budget-vs-actual`

**Parameters:** none - it runs as soon as you pick it.

**Columns:** From Date · To Date · Department · Cost Center *(sensitive)* · GL Account · Target Amount · Net Amount · Amount Variance · Achievement %

**Drill-down:** no.

### Cash Flow Statement

**Report id:** `cat-cash-flow-statement`

**Parameters:**

- **From** (`from`) - date, optional
- **To** (`to`) - date, optional
- **Store** (`location`) - text, optional

**Columns:** From Date · To Date · GL Account · Debit Amount · Credit Amount · Net Amount

**Drill-down:** no.

### Cost Center P&L

**Report id:** `cat-cost-center-p-and-l`

**Parameters:**

- **From** (`from`) - date, optional
- **To** (`to`) - date, optional
- **Store** (`location`) - text, optional

**Columns:** From Date · To Date · Department · Cost Center *(sensitive)* · GL Account · Debit Amount · Credit Amount · Net Amount

**Drill-down:** no.

### Customer Ledger

**Report id:** `cat-customer-ledger`

**Parameters:**

- **From** (`from`) - date, optional
- **To** (`to`) - date, optional
- **Store** (`location`) - text, optional

**Columns:** Posting Date · Customer ID · Customer Name · Document Type · Document No · Debit Amount · Credit Amount · Balance

**Drill-down:** no.

### Debit / Credit Note Register

**Report id:** `cat-debit-credit-note-register`

**Parameters:**

- **From** (`from`) - date, optional
- **To** (`to`) - date, optional
- **Store** (`location`) - text, optional

**Columns:** Document Date · Document Type · Document No · Vendor Code · Customer ID · Taxable Amount · GST Amount · Net Amount · Reason · Status

**Drill-down:** no.

### Depreciation Schedule

**Report id:** `cat-depreciation-schedule`

**Parameters:**

- **From** (`from`) - date, optional
- **To** (`to`) - date, optional
- **Store** (`location`) - text, optional
- **Status (optional)** (`status`) - select, optional, one of: ,Draft,Active,Capitalised,Disposed,Retired

**Columns:** From Date · To Date · Item Code · Item Name · Purchase Price *(sensitive)* · Debit Amount · Balance · Status

**Drill-down:** no.

### Finance Approval Pending Report

**Report id:** `cat-finance-approval-pending-report`

**Parameters:**

- **Store** (`location`) - text, optional

**Columns:** Document Type · Document No · Document Date · Vendor Name · Amount · Approval Level · Pending With · Ageing Days · Status

**Drill-down:** no.

### Fixed Asset Register

**Report id:** `cat-fixed-asset-register`

**Parameters:**

- **From** (`from`) - date, optional
- **To** (`to`) - date, optional
- **Store** (`location`) - text, optional
- **Status (optional)** (`status`) - select, optional, one of: ,Draft,Active,Capitalised,Disposed,Retired

**Columns:** Document Date · Document No · Item Code · Item Name · Store Code · Cost Center *(sensitive)* · Purchase Price *(sensitive)* · Status · Created At

**Drill-down:** no.

### General Ledger

**Report id:** `cat-general-ledger`

**Parameters:**

- **From** (`from`) - date, optional
- **To** (`to`) - date, optional
- **Store** (`location`) - text, optional

**Columns:** Posting Date · Voucher No · Document Type · Document No · GL Account · Cost Center *(sensitive)* · Debit Amount · Credit Amount · Balance · Remarks

**Drill-down:** no.

### Intercompany Reconciliation

**Report id:** `cat-intercompany-reconciliation`

**Parameters:**

- **From** (`from`) - date, optional
- **To** (`to`) - date, optional

**Columns:** Posting Date · Legal Entity · Document Type · Document No · Debit Amount · Credit Amount · Balance · Reconciliation Status

**Drill-down:** no.

### Journal Voucher Register

**Report id:** `cat-journal-voucher-register`

**Parameters:**

- **From** (`from`) - date, optional
- **To** (`to`) - date, optional
- **Status (optional)** (`status`) - select, optional, one of: ,Draft,Pending Approval,Approved,Posted,Reversed,Rejected

**Columns:** Posting Date · Voucher No · GL Account · Cost Center *(sensitive)* · Debit Amount · Credit Amount · Remarks · Created By · Approved By

**Drill-down:** no.

### Payment Proposal Register

**Report id:** `cat-payment-proposal-register`

**Parameters:**

- **From** (`from`) - date, optional
- **To** (`to`) - date, optional
- **Status (optional)** (`status`) - select, optional, one of: ,Draft,Pending Approval,Approved,Paid,Rejected

**Columns:** Payment Proposal No · Posting Date · Vendor Code · Vendor Name · Proposed Payment Amount · Status · Approved By · Approval Date

**Drill-down:** no.

### Payment Register

**Report id:** `cat-payment-register`

**Parameters:**

- **From** (`from`) - date, optional
- **To** (`to`) - date, optional
- **Store** (`location`) - text, optional

**Columns:** Payment Date · Voucher No · Vendor Code · Vendor Name · Bank Account · Net Amount · Reference No · Payment Status

**Drill-down:** no.

### Period Close Checklist

**Report id:** `cat-period-close-checklist`

**Parameters:**

- **From** (`from`) - date, optional
- **To** (`to`) - date, optional

**Columns:** From Date · To Date · Legal Entity · Document Type · Status · User Name · Approved By · Approved At · Remarks

**Drill-down:** no.

### Profit & Loss Statement

**Report id:** `cat-profit-and-loss-statement`

**Parameters:**

- **From** (`from`) - date, optional
- **To** (`to`) - date, optional
- **Store** (`location`) - text, optional

**Columns:** From Date · To Date · GL Account · Department · Cost Center *(sensitive)* · Debit Amount · Credit Amount · Net Amount

**Drill-down:** no.

### Receipt Register

**Report id:** `cat-receipt-register`

**Parameters:**

- **From** (`from`) - date, optional
- **To** (`to`) - date, optional
- **Store** (`location`) - text, optional

**Columns:** Payment Date · Voucher No · Customer ID · Customer Name · Bank Account · Net Amount · Reference No · Status

**Drill-down:** no.

### Sales Invoice Accounting Register

**Report id:** `cat-sales-invoice-accounting-register`

**Parameters:**

- **From** (`from`) - date, optional
- **To** (`to`) - date, optional
- **Store** (`location`) - text, optional
- **Status (optional)** (`status`) - select, optional, one of: ,Approved,Posted,Partially Paid,Paid,Overdue

**Columns:** Posting Date · Sales Invoice No · Customer ID · Customer Name · Customer GSTIN · Taxable Amount · GST Amount · Net Amount · Outstanding Amount · Status

**Drill-down:** no.

### TDS Register

**Report id:** `cat-tds-register`

**Parameters:**

- **From** (`from`) - date, optional
- **To** (`to`) - date, optional
- **Store** (`location`) - text, optional

**Columns:** Posting Date · Vendor Code · Vendor Name · Document No · GL Account · Taxable Amount · GST % · Net Amount · Status

**Drill-down:** no.

### Trial Balance

**Report id:** `cat-trial-balance`

**Parameters:**

- **From** (`from`) - date, optional
- **To** (`to`) - date, optional

**Columns:** From Date · To Date · GL Account · Opening Stock Qty · Debit Amount · Credit Amount · Balance

**Drill-down:** no.

### Vendor Invoice Register

**Report id:** `cat-vendor-invoice-register`

**Parameters:**

- **From** (`from`) - date, optional
- **To** (`to`) - date, optional
- **Store** (`location`) - text, optional
- **Status (optional)** (`status`) - select, optional, one of: ,Draft,Pending Approval,Approved,Partially Paid,Paid,Cancelled

**Columns:** Posting Date · Vendor Invoice No · Vendor Code · Vendor Name · Vendor GSTIN · Taxable Amount · GST Amount · Net Amount · Due Date · Payment Status

**Drill-down:** no.

### Vendor Ledger

**Report id:** `cat-vendor-ledger`

**Parameters:**

- **From** (`from`) - date, optional
- **To** (`to`) - date, optional
- **Store** (`location`) - text, optional

**Columns:** Posting Date · Vendor Code · Vendor Name · Document Type · Document No · Debit Amount · Credit Amount · Balance

**Drill-down:** no.

## Admin

### Knowledge Center Feedback

**Report id:** `help-article-feedback`

**Parameters:** none - it runs as soon as you pick it.

**Columns:** Article Slug · Marked Helpful · Marked Not Helpful · Total Responses · Helpful % · Last Feedback

**Drill-down:** no.

## Assets

### Asset Register

**Report id:** `asset-register`

**Parameters:** none - it runs as soon as you pick it.

**Columns:** Asset ID · Code · Category · Cost *(sensitive)* · Location · Custodian · Status · Accumulated Depreciation *(sensitive)* · Net Block *(sensitive)*

**Drill-down:** no.

## BI

### Report Performance

**Report id:** `report-performance`

**Parameters:**

- **From (optional)** (`start`) - date, optional
- **To (optional)** (`end`) - date, optional

**Columns:** Report · Runs · Avg Duration (ms) · Max Duration (ms) · Avg Rows · Last Run

**Drill-down:** no.

## CRM

### Birthday / Anniversary Campaign List

**Report id:** `cat-birthday-anniversary-campaign-list`

**Parameters:** none - it runs as soon as you pick it.

**Columns:** Customer ID · Customer Name · Customer Mobile · Campaign · Segment · Last Sale Date · Loyalty Points · Status

**Drill-down:** no.

### Campaign Performance

**Report id:** `cat-campaign-performance`

**Parameters:**

- **From** (`from`) - date, optional
- **To** (`to`) - date, optional
- **Store** (`location`) - text, optional

**Columns:** From Date · To Date · Campaign · Channel · Bill Count · Net Sales Amount · Discount Amount · Achievement %

**Drill-down:** no.

### Campaign ROI

**Report id:** `campaign-roi`

**Parameters:** none - it runs as soon as you pick it.

**Columns:** Campaign ID · Campaign · Customers Targeted · Attributed Revenue *(sensitive)* · Cost *(sensitive)* · ROI *(sensitive)*

**Drill-down:** no.

### Cohort Retention

**Report id:** `cohort-retention`

**Parameters:** none - it runs as soon as you pick it.

**Columns:** Cohort Month · Cohort Size · Month Offset · Retained

**Drill-down:** no.

### Complaint / Service Case Register

**Report id:** `cat-complaint-service-case-register`

**Parameters:**

- **From** (`from`) - date, optional
- **To** (`to`) - date, optional
- **Status (optional)** (`status`) - select, optional, one of: ,Open,In Progress,Resolved,Closed,Cancelled

**Columns:** Case No · Created At · Customer ID · Customer Name · Customer Mobile · Store Name · Case Status · Reason · User Name · Remarks

**Drill-down:** no.

### Coupon / Voucher Usage Report

**Report id:** `cat-coupon-voucher-usage-report`

**Parameters:**

- **From** (`from`) - date, optional
- **To** (`to`) - date, optional
- **Store** (`location`) - text, optional

**Columns:** Document Date · Customer ID · Customer Name · Coupon Code · Campaign · Sales Invoice No · Discount Amount · Net Amount · Status

**Drill-down:** no.

### Customer 360

**Report id:** `customer-360`

**Parameters:**

- **Customer ID** (`customer_id`) - text, **required**

**Columns:** Customer · Loyalty Balance *(sensitive)* · POS Purchases · POS Spend *(sensitive)* · Last Purchase · Invoices · Invoice Total *(sensitive)*

**Drill-down:** no.

### Customer 360 Summary

**Report id:** `cat-customer-360-summary`

**Parameters:**

- **From** (`from`) - date, optional
- **To** (`to`) - date, optional

**Columns:** Customer ID · Customer Name · Customer Mobile · Segment · Loyalty Points · Last Sale Date · Net Sales Amount · Bill Count · Feedback Score · Status

**Drill-down:** no.

### Customer Inactivity / Churn Report

**Report id:** `cat-customer-inactivity-churn-report`

**Parameters:** none - it runs as soon as you pick it.

**Columns:** Customer ID · Customer Name · Customer Mobile · Last Sale Date · Bill Count · Net Sales Amount · Segment · Status

**Drill-down:** no.

### Customer Lifetime Value

**Report id:** `customer-lifetime-value`

**Parameters:**

- **Churn Threshold Days (default 90)** (`churn_days`) - text, optional

**Columns:** Customer · Lifetime Value *(sensitive)* · Orders · First Order · Last Order · Churned?

**Drill-down:** no.

### Customer Merge / Mobile Change Audit

**Report id:** `cat-customer-merge-mobile-change-audit`

**Parameters:**

- **From** (`from`) - date, optional
- **To** (`to`) - date, optional

**Columns:** Audit ID · Customer ID · Customer Name · Field Changed · Old Value · New Value · Edit Reason · Edited By · Edited At

**Drill-down:** no.

### Customer Segmentation Report

**Report id:** `cat-customer-segmentation-report`

**Parameters:** none - it runs as soon as you pick it.

**Columns:** Customer ID · Customer Name · Customer Mobile · Segment · RFM Score · Bill Count · Net Sales Amount · Last Sale Date

**Drill-down:** no.

### Customers Sharing a Mobile Number

**Report id:** `customer-duplicate-phones`

**Parameters:** none - it runs as soon as you pick it.

**Columns:** Mobile *(sensitive)* · Customers · Customer codes and names

**Drill-down:** no.

### Loyalty Ledger Summary

**Report id:** `loyalty-summary`

**Parameters:** none - it runs as soon as you pick it.

**Columns:** Customer · Total Earned · Total Redeemed · Current Balance *(sensitive)* · Transactions

**Drill-down:** no.

### Loyalty Points Ledger

**Report id:** `cat-loyalty-points-ledger`

**Parameters:**

- **From** (`from`) - date, optional
- **To** (`to`) - date, optional
- **Store** (`location`) - text, optional

**Columns:** Document Date · Customer ID · Customer Name · Sales Invoice No · Sales Return No · Loyalty Points · Edit Action · Balance

**Drill-down:** no.

### Loyalty Points Liability

**Report id:** `points-liability`

**Parameters:** none - it runs as soon as you pick it.

**Columns:** Outstanding Points · Liability Value *(sensitive)* · Customers

**Drill-down:** no.

### Loyalty Points Movement

**Report id:** `loyalty-points-movement`

**Parameters:**

- **From** (`from_date`) - date, optional
- **To** (`to_date`) - date, optional

**Columns:** Movement · Points · Value *(sensitive)*

**Drill-down:** no.

### RFM Customer Segmentation

**Report id:** `rfm-segmentation`

**Parameters:** none - it runs as soon as you pick it.

**Columns:** Customer · Recency (days) · Frequency · Monetary *(sensitive)* · R · F · M · Segment

**Drill-down:** no.

### Repeat Purchase Report

**Report id:** `cat-repeat-purchase-report`

**Parameters:** none - it runs as soon as you pick it.

**Columns:** Customer ID · Customer Name · Customer Mobile · Bill Count · Net Sales Amount · Last Sale Date · Segment

**Drill-down:** no.

## E-commerce & Integration

### E-commerce Sales Order Item Detail

**Report id:** `cat-e-commerce-sales-order-item-detail`

**Parameters:**

- **From** (`from`) - date, optional
- **To** (`to`) - date, optional
- **Store** (`location`) - text, optional

**Columns:** Order Date · Order ID · Shipment ID · Invoice Code · Sales Channel · Outlet ID · Outlet Name · Channel SKU · SKU · Item Name · Qty · Line Status · Invoice Status · UC Status · POS Bill Status · POS Invoice No · Package Amount · Tracking Number · Shipping Provider

**Drill-down:** no.

### E-commerce Sales Order Summary

**Report id:** `cat-e-commerce-sales-order-summary`

**Parameters:**

- **From** (`from`) - date, optional
- **To** (`to`) - date, optional
- **Store** (`location`) - text, optional

**Columns:** Order Date · Order ID · Shipment ID · Shipment Sequence · Invoice Code · Invoice Date · Manifest Code · Sales Channel · Outlet ID · Outlet Name · UC Status · Invoice Status · POS Bill Status · POS Invoice No · Qty · Package Amount · Tracking Number · Shipping Provider

**Drill-down:** no.

### ERP vs Unicommerce Inventory Reconciliation - Itemwise

**Report id:** `cat-erp-vs-unicommerce-inventory-reconciliation-itemwise`

**Parameters:**

- **Store** (`location`) - text, optional

**Columns:** As Of Date · ERP Sync Time · Outlet ID · Outlet Name · UC Facility Code · SKU · Barcode · ERP Inventory Qty · Unicommerce Inventory Qty · Variance Qty · Variance Status · Mapping Status

**Drill-down:** no.

### ERP vs Unicommerce Inventory Reconciliation - Summary

**Report id:** `cat-erp-vs-unicommerce-inventory-reconciliation-summary`

**Parameters:**

- **Store** (`location`) - text, optional

**Columns:** As Of Date · Latest ERP Sync Time · SKU · Item Name · ERP Inventory Qty · Unicommerce Inventory Qty · Variance Qty · Mismatch Outlet Count · Variance Status · Mapping Status

**Drill-down:** no.

### Integration Retry / Failure Audit

**Report id:** `cat-integration-retry-failure-audit`

**Parameters:**

- **From** (`from`) - date, optional
- **To** (`to`) - date, optional

**Columns:** Created At · Source Document Type · Source Document No · Integration Status · Error Code · Error Message · Retry Count · Last Retry At · User Name

**Drill-down:** no.

### Inventory Sync Log

**Report id:** `cat-inventory-sync-log`

**Parameters:**

- **Store** (`location`) - text, optional

**Columns:** ERP Sync Time · Outlet ID · Outlet Name · UC Facility Code · SKU · ERP Inventory Qty · Unicommerce Inventory Qty · Integration Status · Error Code · Error Message · Retry Count

**Drill-down:** no.

### Marketplace Order Status Summary

**Report id:** `cat-marketplace-order-status-summary`

**Parameters:**

- **From** (`from`) - date, optional
- **To** (`to`) - date, optional
- **Store** (`location`) - text, optional

**Columns:** From Date · To Date · Sales Channel · UC Status · POS Bill Status · Invoice Status · Bill Count · Qty · Net Amount

**Drill-down:** no.

### Online Return Register

**Report id:** `cat-online-return-register`

**Parameters:**

- **From** (`from`) - date, optional
- **To** (`to`) - date, optional
- **Store** (`location`) - text, optional

**Columns:** Document Date · Sales Return No · Original Bill No · Order ID · Shipment ID · Sales Channel · Outlet Name · SKU · Barcode · Return Qty · Net Amount · Status

**Drill-down:** no.

### Order Sync Log

**Report id:** `cat-order-sync-log`

**Parameters:**

- **From** (`from`) - date, optional
- **To** (`to`) - date, optional
- **Store** (`location`) - text, optional

**Columns:** Created At · Order ID · Shipment ID · Sales Channel · Outlet Name · Integration Status · Error Code · Error Message · Retry Count · Last Retry At

**Drill-down:** no.

### Outlet / Facility Mapping Summary

**Report id:** `cat-outlet-facility-mapping-summary`

**Parameters:**

- **Store** (`location`) - text, optional

**Columns:** Outlet ID · Outlet Name · UC Facility Code · Mapping Status · Status · Created By

**Drill-down:** no.

### Pending Sales Order Stock

**Report id:** `cat-pending-sales-order-stock`

**Parameters:**

- **From** (`from`) - date, optional
- **To** (`to`) - date, optional
- **Store** (`location`) - text, optional

**Columns:** Order Date · Order ID · Shipment ID · Outlet Name · SKU · Item Name · Qty · Available Qty · UC Status · POS Bill Status · Status

**Drill-down:** no.

### Sales Order Integration Error

**Report id:** `cat-sales-order-integration-error`

**Parameters:**

- **From** (`from`) - date, optional
- **To** (`to`) - date, optional

**Columns:** Order Date · Order ID · Shipment ID · Sales Channel · Integration Status · Error Code · Error Message · Retry Count · Last Retry At

**Drill-down:** no.

## Exceptions

### Failed Syncs

**Report id:** `exception-failed-syncs`

**Parameters:** none - it runs as soon as you pick it.

**Columns:** Event ID · Event · Attempts · Queued

**Drill-down:** no.

### Inactive Items With Stock

**Report id:** `inactive-items-with-stock`

**Parameters:** none - it runs as soon as you pick it.

**Columns:** SKU · Item · Status · Store · On hand

**Drill-down:** no.

### Item Exceptions

**Report id:** `item-exceptions`

**Parameters:**

- **Status** (`status`) - select, optional, one of: ,Open,Logged,Resolved

**Columns:** Date · Type · Source · SKU · Store · Code · What happened · Qty · Status

**Drill-down:** no.

### Item Master Completeness

**Report id:** `item-master-completeness`

**Parameters:** none - it runs as soon as you pick it.

**Columns:** SKU · Item · Item Type · Missing

**Drill-down:** no.

### Negative Stock Flags

**Report id:** `exception-negative-stock`

**Parameters:** none - it runs as soon as you pick it.

**Columns:** Flag ID · SKU · Location · Cart · Shortfall · Resulting Available · Flagged

**Drill-down:** no.

### Stale Approvals

**Report id:** `exception-stale-approvals`

**Parameters:**

- **Threshold Hours (default 24)** (`threshold_hours`) - text, optional

**Columns:** Doctype · Document · Submitted · Hours Pending

**Drill-down:** no.

## Expense Management

### Accounting Export Report

**Report id:** `cat-accounting-export-report`

**Parameters:**

- **From** (`from`) - date, optional
- **To** (`to`) - date, optional
- **Store** (`location`) - text, optional

**Columns:** Claim No · Posting Date · Employee ID · Employee Name · Expense Category · GL Account · Cost Center *(sensitive)* · Eligible Amount · GST Amount · Status

**Drill-down:** no.

### Approved Pending Payment

**Report id:** `cat-approved-pending-payment`

**Parameters:**

- **From** (`from`) - date, optional
- **To** (`to`) - date, optional
- **Store** (`location`) - text, optional
- **Status (optional)** (`status`) - select, optional, one of: ,Draft,Submitted,Verified,Paid,Rejected

**Columns:** Claim No · Expense Date · Employee ID · Employee Name · Department · Eligible Amount · Approval Status · Payment Status · SLA Age Hours

**Drill-down:** no.

### Claims Pending Approval

**Report id:** `cat-claims-pending-approval`

**Parameters:**

- **From** (`from`) - date, optional
- **To** (`to`) - date, optional
- **Store** (`location`) - text, optional
- **Status (optional)** (`status`) - select, optional, one of: ,Draft,Submitted,Verified,Paid,Rejected

**Columns:** Claim No · Expense Date · Employee ID · Employee Name · Department · Expense Category · Claim Amount · Approval Status · SLA Age Hours · Manager Name

**Drill-down:** no.

### Department / Category / Month Summary

**Report id:** `cat-department-category-month-summary`

**Parameters:**

- **From** (`from`) - date, optional
- **To** (`to`) - date, optional
- **Store** (`location`) - text, optional
- **Status (optional)** (`status`) - select, optional, one of: ,Draft,Submitted,Verified,Paid,Rejected

**Columns:** From Date · To Date · Department · Expense Category · Claim Amount · Eligible Amount · Headcount

**Drill-down:** no.

### Employee Expense Summary

**Report id:** `cat-employee-expense-summary`

**Parameters:**

- **From** (`from`) - date, optional
- **To** (`to`) - date, optional
- **Store** (`location`) - text, optional
- **Status (optional)** (`status`) - select, optional, one of: ,Draft,Submitted,Verified,Paid,Rejected

**Columns:** From Date · To Date · Employee ID · Employee Name · Department · Claim Amount · Eligible Amount · Payment Status

**Drill-down:** no.

### Expense Advance Settlement

**Report id:** `cat-expense-advance-settlement`

**Parameters:**

- **From** (`from`) - date, optional
- **To** (`to`) - date, optional
- **Store** (`location`) - text, optional
- **Status (optional)** (`status`) - select, optional, one of: ,Draft,Submitted,Verified,Paid,Rejected

**Columns:** Document Date · Employee ID · Employee Name · Advance No · Advance Amount · Advance Adjusted Amount · Advance Balance Amount · Status

**Drill-down:** no.

### Expense Aging / SLA Report

**Report id:** `cat-expense-aging-sla-report`

**Parameters:**

- **From** (`from`) - date, optional
- **To** (`to`) - date, optional
- **Store** (`location`) - text, optional
- **Status (optional)** (`status`) - select, optional, one of: ,Draft,Submitted,Verified,Paid,Rejected

**Columns:** Claim No · Expense Date · Employee ID · Department · Claim Amount · Approval Status · Payment Status · SLA Due At · SLA Age Hours · Status

**Drill-down:** no.

### Expense Claim Register

**Report id:** `cat-expense-claim-register`

**Parameters:**

- **From** (`from`) - date, optional
- **To** (`to`) - date, optional
- **Store** (`location`) - text, optional
- **Status (optional)** (`status`) - select, optional, one of: ,Draft,Submitted,Verified,Paid,Rejected

**Columns:** Claim No · Expense Date · Employee ID · Employee Name · Department · Expense Category · Claim Amount · Eligible Amount · Approval Status · Payment Status · Status

**Drill-down:** no.

### Paid Claims Register

**Report id:** `cat-paid-claims-register`

**Parameters:**

- **From** (`from`) - date, optional
- **To** (`to`) - date, optional
- **Store** (`location`) - text, optional
- **Status (optional)** (`status`) - select, optional, one of: ,Draft,Submitted,Verified,Paid,Rejected

**Columns:** Claim No · Expense Date · Employee ID · Employee Name · Eligible Amount · Payment Date · UTR No · Payment Status

**Drill-down:** no.

### Policy Violation / Exception Report

**Report id:** `cat-policy-violation-exception-report`

**Parameters:**

- **From** (`from`) - date, optional
- **To** (`to`) - date, optional
- **Store** (`location`) - text, optional
- **Status (optional)** (`status`) - select, optional, one of: ,Draft,Submitted,Verified,Paid,Rejected

**Columns:** Claim No · Employee ID · Employee Name · Expense Category · Claim Amount · Eligible Amount · Policy Exception · Approval Status · Status

**Drill-down:** no.

### Rejected / Returned Claims

**Report id:** `cat-rejected-returned-claims`

**Parameters:**

- **From** (`from`) - date, optional
- **To** (`to`) - date, optional
- **Store** (`location`) - text, optional
- **Status (optional)** (`status`) - select, optional, one of: ,Draft,Submitted,Verified,Paid,Rejected

**Columns:** Claim No · Expense Date · Employee ID · Employee Name · Claim Amount · Approval Status · Reason · Remarks · User Name

**Drill-down:** no.

## Finance

### Balance Sheet

**Report id:** `balance-sheet`

**Parameters:**

- **As Of** (`as_of`) - date, **required**
- **Cost Center (optional)** (`cost_center`) - text, optional
- **Department (optional)** (`department`) - text, optional
- **Entity (optional)** (`entity`) - text, optional

**Columns:** Account Code · Account Name · Type · Amount *(sensitive)*

**Drill-down:** yes - a row's **View Details** opens the transactions behind it.

### Bank Book

**Report id:** `bank-book`

**Parameters:**

- **Bank Account** (`bank_account`) - text, **required**

**Columns:** Date · Document Type · Document ID · Debit *(sensitive)* · Credit *(sensitive)* · Running Balance *(sensitive)*

**Drill-down:** no.

### Budget vs Actual

**Report id:** `budget-variance`

**Parameters:**

- **From** (`start`) - date, **required**
- **To** (`end`) - date, **required**

**Columns:** Cost Center · Account · Planned *(sensitive)* · Actual *(sensitive)* · Variance *(sensitive)*

**Drill-down:** no.

### Cash Book

**Report id:** `cash-book`

**Parameters:** none - it runs as soon as you pick it.

**Columns:** Date · Document Type · Document ID · Debit *(sensitive)* · Credit *(sensitive)* · Running Balance *(sensitive)*

**Drill-down:** no.

### Cash Flow Forecast

**Report id:** `cash-flow-forecast`

**Parameters:**

- **As Of** (`as_of`) - date, **required**
- **Horizon (days)** (`horizon_days`) - text, optional

**Columns:** Date · Expected Inflow *(sensitive)* · Expected Outflow *(sensitive)* · Projected Balance *(sensitive)*

**Drill-down:** no.

### Cash Flow Statement

**Report id:** `cash-flow-statement`

**Parameters:**

- **From** (`start`) - date, **required**
- **To** (`end`) - date, **required**

**Columns:** Activity · Cash In *(sensitive)* · Cash Out *(sensitive)* · Net Change *(sensitive)*

**Drill-down:** no.

### Consolidated Trial Balance

**Report id:** `consolidated-trial-balance`

**Parameters:**

- **As Of** (`as_of`) - date, **required**

**Columns:** Account · Account Name · Type · Debit (pre-elimination) *(sensitive)* · Credit (pre-elimination) *(sensitive)* · Debit (consolidated) *(sensitive)* · Credit (consolidated) *(sensitive)*

**Drill-down:** no.

### Cost Center P&L

**Report id:** `cost-center-pl`

**Parameters:**

- **From** (`start`) - date, **required**
- **To** (`end`) - date, **required**

**Columns:** Cost Center · Name · Type · Amount *(sensitive)*

**Drill-down:** yes - a row's **View Details** opens the transactions behind it.

### Deferred Revenue Roll-Forward

**Report id:** `deferred-revenue-roll-forward`

**Parameters:** none - it runs as soon as you pick it.

**Columns:** Schedule · Invoice · Total *(sensitive)* · Recognized Months · Term Months · Recognized *(sensitive)* · Remaining *(sensitive)* · Status

**Drill-down:** no.

### Department P&L

**Report id:** `department-pl`

**Parameters:**

- **From** (`start`) - date, **required**
- **To** (`end`) - date, **required**

**Columns:** Department · Name · Type · Amount *(sensitive)*

**Drill-down:** yes - a row's **View Details** opens the transactions behind it.

### Dunning Queue

**Report id:** `dunning-queue`

**Parameters:** none - it runs as soon as you pick it.

**Columns:** Invoice · Customer · Due Date · Days Overdue · Amount *(sensitive)* · Tier · Last Notified

**Drill-down:** no.

### E-Invoice / IRN Status Register

**Report id:** `einvoice-irn-register`

**Parameters:**

- **From** (`from`) - date, optional
- **To** (`to`) - date, optional
- **GSTIN** (`gstin`) - text, optional
- **State** (`state`) - select, optional, one of: ,NEEDS ACTION,QUEUED,UNKNOWN,REJECTED,ACKNOWLEDGED,CANCELLED

**Columns:** Date · Type · Number · Seller GSTIN · IRN state · IRN · Ack No · Ack Date · Made by · Cancelled

**Drill-down:** no.

### E-Invoice Error / Retry Report

**Report id:** `einvoice-errors`

**Parameters:**

- **From** (`from`) - date, optional
- **To** (`to`) - date, optional
- **GSTIN** (`gstin`) - text, optional

**Columns:** Date · Type · Number · Seller GSTIN · IRN state · Attempts · Next try · Last error / what to fix

**Drill-down:** no.

### Entity Trial Balance

**Report id:** `entity-trial-balance`

**Parameters:**

- **From** (`start`) - date, **required**
- **To** (`end`) - date, **required**

**Columns:** Entity · Account · Account Name · Type · Debit *(sensitive)* · Credit *(sensitive)*

**Drill-down:** no.

### FX Gain/Loss Register

**Report id:** `fx-gain-loss-register`

**Parameters:**

- **From (YYYY-MM-DD, default 3 months back)** (`from_date`) - text, optional
- **To (YYYY-MM-DD, default today)** (`to_date`) - text, optional
- **Kind (realised / unrealised, blank for both)** (`kind`) - text, optional

**Columns:** Posted · Kind · Account · Account Name · Document Type · Document · Currency · Rate · Gain · Loss · Net

**Drill-down:** no.

### GL Drill-down (by Account)

**Report id:** `gl-drilldown`

**Parameters:**

- **Account Code** (`account_code`) - text, **required**

**Columns:** Document Type · Document ID · Debit *(sensitive)* · Credit *(sensitive)* · Running Balance *(sensitive)* · Date

**Drill-down:** no.

### GL Posting Review (by poster)

**Report id:** `gl-posting-review`

**Parameters:**

- **From** (`start`) - date, **required**
- **To** (`end`) - date, **required**

**Columns:** Posted by · Source commands · GL lines · Documents · Debit *(sensitive)* · Credit *(sensitive)* · First posted · Last posted

**Drill-down:** yes - a row's **View Details** opens the transactions behind it.

### GST Return Summary

**Report id:** `gst-return-summary`

**Parameters:**

- **From** (`start`) - date, **required**
- **To** (`end`) - date, **required**

**Columns:** From · To · Taxable Value *(sensitive)* · Output CGST *(sensitive)* · Output SGST *(sensitive)* · Output IGST *(sensitive)* · Total Tax Liability *(sensitive)* · Input Tax Credit *(sensitive)* · Net GST Payable *(sensitive)* · Exempt Value *(sensitive)* · Nil-Rated Value *(sensitive)* · Zero-Rated Value *(sensitive)* · Total Non-Taxable *(sensitive)* · Transactions

**Drill-down:** yes - a row's **View Details** opens the transactions behind it.

### General Journal (who posted what)

**Report id:** `gl-journal`

**Parameters:**

- **From** (`start`) - date, **required**
- **To** (`end`) - date, **required**
- **Document type (optional)** (`document_type`) - text, optional
- **Posted by (username, service:job, or "before attribution")** (`posted_by`) - text, optional
- **Attribution** (`attribution`) - select, optional, one of: All,Attributed,Before attribution

**Columns:** Posted at · Document Type · Document · Account · Account Name · Debit *(sensitive)* · Credit *(sensitive)* · Posted by · Source command

**Drill-down:** no.

### Gift Card Ledger

**Report id:** `stored-value-ledger`

**Parameters:**

- **Last 4 digits** (`card`) - text, optional
- **From** (`from`) - date, optional
- **To** (`to`) - date, optional

**Columns:** When · Card · # · Event · Amount · Balance after · Source · Store · User · Reason

**Drill-down:** no.

### Gift Card Liability

**Report id:** `stored-value-liability`

**Parameters:**

- **Status** (`status`) - select, optional, one of: ,Inactive,Active,Partially redeemed,Fully redeemed,Blocked,Cancelled

**Columns:** Card · Profile · Status · Loaded · Used / cancelled · Outstanding · Expires · Store issued

**Drill-down:** no.

### IRN Portal Manual Upload Report

**Report id:** `einvoice-manual-upload`

**Parameters:**

- **From** (`from`) - date, optional
- **To** (`to`) - date, optional
- **GSTIN** (`gstin`) - text, optional

**Columns:** Date · Type · Number · Seller GSTIN · IRN state · Why · IRN (when entered)

**Drill-down:** no.

### Intercompany Reconciliation

**Report id:** `intercompany-reconciliation`

**Parameters:**

- **As Of** (`as_of`) - date, **required**

**Columns:** From Entity · To Entity · IC Ledger Amount *(sensitive)* · From Entity 1700 Balance *(sensitive)* · To Entity 2500 Balance *(sensitive)* · Variance *(sensitive)* · In Balance

**Drill-down:** no.

### Open FX Exposure

**Report id:** `fx-open-item-exposure`

**Parameters:**

- **As Of (YYYY-MM-DD, default today)** (`as_of`) - text, optional
- **Rate Type (Closing/Spot/Average)** (`rate_type`) - text, optional

**Columns:** Document Type · Document · Currency · Amount (txn) · Booked Rate · Booked (functional) · Carrying · Rate Now · Revalued · Unrealised Movement · Last Revalued · Note

**Drill-down:** no.

### POS Sale Reconciliation

**Report id:** `pos-sale-reconciliation`

**Parameters:**

- **From** (`from_date`) - date, optional
- **To** (`to_date`) - date, optional

**Columns:** Sale · Status · Payment State · Qty Sold · Qty in Stock Ledger · Cart Total *(sensitive)* · Revenue Posted *(sensitive)* · COGS Posted *(sensitive)* · Tax Posted *(sensitive)* · Tender Posted *(sensitive)* · Points Burned · Event Published · Verdict · Detail

**Drill-down:** no.

### Payables Ageing

**Report id:** `payables-ageing`

**Parameters:** none - it runs as soon as you pick it.

**Columns:** Age Bucket · Open Items · Outstanding Amount *(sensitive)*

**Drill-down:** yes - a row's **View Details** opens the transactions behind it.

### Prepaid Expense Roll-Forward

**Report id:** `prepaid-expense-roll-forward`

**Parameters:** none - it runs as soon as you pick it.

**Columns:** Schedule · Expense Account · Total *(sensitive)* · Recognized Months · Term Months · Recognized *(sensitive)* · Remaining *(sensitive)* · Status

**Drill-down:** no.

### Profit & Loss

**Report id:** `profit-and-loss`

**Parameters:**

- **From** (`start`) - date, **required**
- **To** (`end`) - date, **required**
- **Cost Center (optional)** (`cost_center`) - text, optional
- **Department (optional)** (`department`) - text, optional
- **Entity (optional)** (`entity`) - text, optional

**Columns:** Account Code · Account Name · Type · Amount *(sensitive)*

**Drill-down:** yes - a row's **View Details** opens the transactions behind it.

### Project P&L (Job Costing)

**Report id:** `project-pl`

**Parameters:**

- **From** (`start`) - date, **required**
- **To** (`end`) - date, **required**

**Columns:** Project · Type · Amount *(sensitive)*

**Drill-down:** yes - a row's **View Details** opens the transactions behind it.

### Receivables Ageing

**Report id:** `receivables-ageing`

**Parameters:** none - it runs as soon as you pick it.

**Columns:** Age Bucket · Invoice Count · Outstanding Amount *(sensitive)*

**Drill-down:** yes - a row's **View Details** opens the transactions behind it.

### Statutory Audit Export (Full GL)

**Report id:** `statutory-gl-export`

**Parameters:**

- **Period Start** (`start`) - date, **required**
- **Period End** (`end`) - date, **required**

**Columns:** Posting ID · Account Code · Account Name · Type · Document Type · Document ID · Debit *(sensitive)* · Credit *(sensitive)* · Date · Posted by · Source command

**Drill-down:** no.

### Tax Ledger

**Report id:** `tax-ledger`

**Parameters:**

- **From (optional)** (`start`) - date, optional
- **To (optional)** (`end`) - date, optional

**Columns:** Account Code · Account Name · Document Type · Document ID · Debit *(sensitive)* · Credit *(sensitive)* · Date

**Drill-down:** no.

### Trial Balance in Presentation Currency

**Report id:** `trial-balance-presentation-currency`

**Parameters:**

- **As Of (YYYY-MM-DD, default today)** (`as_of`) - text, optional
- **Present In (ISO code, e.g. USD)** (`presentation_currency`) - text, optional
- **Rate Type (Closing/Spot/Average)** (`rate_type`) - text, optional

**Columns:** Account · Account Name · Type · Balance (functional) · Balance (presentation) · Currency · Rate · Rate Type

**Drill-down:** no.

## GST & Compliance

### E-Invoice / IRN Status Register

**Report id:** `cat-e-invoice-irn-status-register`

**Parameters:**

- **From** (`from`) - date, optional
- **To** (`to`) - date, optional
- **Store** (`location`) - text, optional

**Columns:** Document Date · Document Type · Document No · GSTIN · IRN · Ack No · Ack Date · IRN Status · IRN Error · Status

**Drill-down:** no.

### E-Invoice Error / Retry Report

**Report id:** `cat-e-invoice-error-retry-report`

**Parameters:**

- **From** (`from`) - date, optional
- **To** (`to`) - date, optional
- **Store** (`location`) - text, optional

**Columns:** Document Date · Document No · IRN Status · IRN Error · Retry Count · Last Retry At · User Name · Status

**Drill-down:** no.

### GST Bill Number / Document Series Audit

**Report id:** `cat-gst-bill-number-document-series-audit`

**Parameters:**

- **From** (`from`) - date, optional
- **To** (`to`) - date, optional
- **Store** (`location`) - text, optional

**Columns:** Document Date · Document Type · Document No · GST Bill No · Original Bill No · GSTIN · Status · Created By

**Drill-down:** no.

### GST Document Tax Detail

**Report id:** `cat-gst-document-tax-detail`

**Parameters:**

- **From** (`from`) - date, optional
- **To** (`to`) - date, optional
- **Store** (`location`) - text, optional

**Columns:** Document Date · Document Type · Document No · GSTIN · Customer GSTIN · HSN Code · Place of Supply · Tax Type · GST % · Taxable Amount · CGST Amount · SGST Amount · IGST Amount · Net Amount

**Drill-down:** no.

### GSTR-1 B2B / B2C Summary

**Report id:** `cat-gstr-1-b2b-b2c-summary`

**Parameters:**

- **From** (`from`) - date, optional
- **To** (`to`) - date, optional
- **Store** (`location`) - text, optional

**Columns:** From Date · To Date · GSTIN · Supply Type · Place of Supply · GST % · Taxable Amount · CGST Amount · SGST Amount · IGST Amount · Net Amount

**Drill-down:** no.

### GSTR-1 HSN Summary

**Report id:** `cat-gstr-1-hsn-summary`

**Parameters:**

- **From** (`from`) - date, optional
- **To** (`to`) - date, optional
- **Store** (`location`) - text, optional

**Columns:** From Date · To Date · GSTIN · HSN Code · GST % · Qty · Taxable Amount · CGST Amount · SGST Amount · IGST Amount · Net Amount

**Drill-down:** no.

### HSN Tax Summary

**Report id:** `cat-hsn-tax-summary`

**Parameters:**

- **From** (`from`) - date, optional
- **To** (`to`) - date, optional
- **Store** (`location`) - text, optional

**Columns:** From Date · To Date · HSN Code · GST % · Qty · Taxable Amount · GST Amount · Net Amount

**Drill-down:** no.

### IRN Portal Manual Upload Report

**Report id:** `cat-irn-portal-manual-upload-report`

**Parameters:**

- **From** (`from`) - date, optional
- **To** (`to`) - date, optional
- **Store** (`location`) - text, optional

**Columns:** Document Date · Document Type · Document No · GSTIN · Customer GSTIN · Place of Supply · Taxable Amount · CGST Amount · SGST Amount · IGST Amount · Net Amount · IRN Status · IRN Error

**Drill-down:** no.

## HR

### Attendance Summary

**Report id:** `attendance-summary`

**Parameters:**

- **From (optional)** (`start`) - date, optional
- **To (optional)** (`end`) - date, optional

**Columns:** Employee ID · Employee · Department · Present · Absent · Late · Leave · Holiday · Weekly Off · Total Marked Days · Attendance %

**Drill-down:** no.

### Payroll Reconciliation (gross to bank to GL)

**Report id:** `payroll-reconciliation`

**Parameters:**

- **Pay periods ending from** (`start`) - date, **required**
- **Pay periods ending to** (`end`) - date, **required**

**Columns:** Step · What is compared · Payslips · Source total *(sensitive)* · Compared with *(sensitive)* · Difference *(sensitive)* · Status · Note

**Drill-down:** yes - a row's **View Details** opens the transactions behind it.

## HRMS

### Attendance Register

**Report id:** `cat-attendance-register`

**Parameters:**

- **From** (`from`) - date, optional
- **To** (`to`) - date, optional
- **Store** (`location`) - text, optional
- **Status (optional)** (`status`) - select, optional, one of: ,Present,Absent,Half Day,Late,On Leave

**Columns:** Attendance Date · Employee ID · Employee Name · Department · Store Name · Shift · Check In · Check Out · Attendance Status

**Drill-down:** no.

### Attrition Report

**Report id:** `cat-attrition-report`

**Parameters:**

- **Store** (`location`) - text, optional
- **Status (optional)** (`status`) - select, optional, one of: ,Active,Inactive,Exited,On Notice

**Columns:** Document Date · Employee ID · Employee Name · Department · Store Name · Reason · Status

**Drill-down:** no.

### Employee Loan / Advance Register

**Report id:** `cat-employee-loan-advance-register`

**Parameters:**

- **From** (`from`) - date, optional
- **To** (`to`) - date, optional
- **Store** (`location`) - text, optional

**Columns:** Document Date · Employee ID · Employee Name · Advance No · Advance Amount · Advance Adjusted Amount · Advance Balance Amount · Status

**Drill-down:** no.

### Employee Master Register

**Report id:** `cat-employee-master-register`

**Parameters:**

- **From** (`from`) - date, optional
- **To** (`to`) - date, optional
- **Store** (`location`) - text, optional
- **Status (optional)** (`status`) - select, optional, one of: ,Active,Inactive,Exited,On Notice

**Columns:** Employee ID · Employee Name · Department · Store Code · Store Name · Manager Name · Status · Created At

**Drill-down:** no.

### Employee Role / Access Mapping

**Report id:** `cat-employee-role-access-mapping`

**Parameters:**

- **Store** (`location`) - text, optional
- **Status (optional)** (`status`) - select, optional, one of: ,Active,Inactive,Exited,On Notice

**Columns:** Employee ID · Employee Name · Department · Store Code · Store Name · User ID · User Name · Status · Approved By

**Drill-down:** no.

### Headcount Summary

**Report id:** `cat-headcount-summary`

**Parameters:**

- **Store** (`location`) - text, optional
- **Status (optional)** (`status`) - select, optional, one of: ,Active,Inactive,Exited,On Notice

**Columns:** As Of Date · Department · Store Code · Store Name · Headcount · Status

**Drill-down:** no.

### Late / Overtime Report

**Report id:** `cat-late-overtime-report`

**Parameters:**

- **From** (`from`) - date, optional
- **To** (`to`) - date, optional
- **Store** (`location`) - text, optional
- **Status (optional)** (`status`) - select, optional, one of: ,Present,Absent,Half Day,Late,On Leave

**Columns:** Attendance Date · Employee ID · Employee Name · Shift · Check In · Check Out · Task Duration Min · Attendance Status · Reason

**Drill-down:** no.

### Leave Register

**Report id:** `cat-leave-register`

**Parameters:**

- **From** (`from`) - date, optional
- **To** (`to`) - date, optional
- **Store** (`location`) - text, optional
- **Status (optional)** (`status`) - select, optional, one of: ,Draft,Pending Approval,Approved,Rejected,Cancelled

**Columns:** Document Date · Employee ID · Employee Name · Leave Type · Leave Days · From Date · To Date · Approval Status · Manager Name · Reason

**Drill-down:** no.

### Onboarding Checklist

**Report id:** `cat-onboarding-checklist`

**Parameters:**

- **From** (`from`) - date, optional
- **To** (`to`) - date, optional
- **Store** (`location`) - text, optional

**Columns:** Employee ID · Employee Name · Department · Document Type · Status · User Name · Approved By · Remarks

**Drill-down:** no.

### Payroll Register

**Report id:** `cat-payroll-register`

**Parameters:**

- **From** (`from`) - date, optional
- **To** (`to`) - date, optional
- **Store** (`location`) - text, optional

**Columns:** Payroll Period · Employee ID · Employee Name · Department · Gross Salary · Deductions · Net Pay · Payment Status

**Drill-down:** no.

### Performance Review Summary

**Report id:** `cat-performance-review-summary`

**Parameters:**

- **From** (`from`) - date, optional
- **To** (`to`) - date, optional
- **Store** (`location`) - text, optional

**Columns:** From Date · To Date · Employee ID · Employee Name · Department · Manager Name · Achievement % · Status · Remarks

**Drill-down:** no.

### Salary Component Detail

**Report id:** `cat-salary-component-detail`

**Parameters:**

- **From** (`from`) - date, optional
- **To** (`to`) - date, optional
- **Store** (`location`) - text, optional

**Columns:** Payroll Period · Employee ID · Employee Name · Document Type · Gross Salary · Deductions · Net Pay

**Drill-down:** no.

### Shift / Roster Report

**Report id:** `cat-shift-roster-report`

**Parameters:**

- **From** (`from`) - date, optional
- **To** (`to`) - date, optional
- **Store** (`location`) - text, optional

**Columns:** Document Date · Employee ID · Employee Name · Store Name · Department · Shift · Status

**Drill-down:** no.

### Statutory Deduction Summary

**Report id:** `cat-statutory-deduction-summary`

**Parameters:**

- **From** (`from`) - date, optional
- **To** (`to`) - date, optional
- **Store** (`location`) - text, optional

**Columns:** Payroll Period · Department · Document Type · Deductions · Status

**Drill-down:** no.

### Training / Certification Register

**Report id:** `cat-training-certification-register`

**Parameters:**

- **From** (`from`) - date, optional
- **To** (`to`) - date, optional
- **Store** (`location`) - text, optional

**Columns:** Document Date · Employee ID · Employee Name · Department · Document Type · Status · Remarks

**Drill-down:** no.

