---
title: Report catalog (part 2)
section: Reference
order: 21
summary: Reports in Inventory to Procurement, continued from the report catalog.
audience: store manager, finance, category manager, admin
last_verified: 2026-09-10
owner: documentation-maintainer
status: active
topic_type: reference
module: platform
task: Report catalog (part 2)
prerequisites: Authorized access to the relevant ERP task
applies_to: source registries; not release acceptance
screens: [reports]
---

<!-- GENERATED ARTICLE - DO NOT EDIT BY HAND.
     Regenerate: pwsh docs/update-docs.ps1 -Group Content -->

# Report catalog (part 2)

This page continues the [report catalog](report-catalog.md), which lists
every category and explains how to run a report. It holds **130** reports, from
Inventory to Procurement.

## Contents

- [Inventory](#inventory) - 33 reports
- [MIS & Management](#mis-management) - 10 reports
- [Manufacturing](#manufacturing) - 19 reports
- [Master Data & Audit](#master-data-audit) - 11 reports
- [OMS](#oms) - 25 reports
- [PIM](#pim) - 4 reports
- [PIM / PIMS](#pim-pims) - 16 reports
- [Procurement](#procurement) - 12 reports

## Inventory

### Backflush Consumption

**Report id:** `backflush-consumption`

**Parameters:**

- **From** (`from`) - date, optional
- **To** (`to`) - date, optional
- **Store** (`store`) - text, optional

**Columns:** Date · Bill · Store · Sold item · Group · Consumed · Qty · Default · Changed · Reason · Rule (version)

**Drill-down:** no.

### Barcode Reprint / Label History

**Report id:** `cat-barcode-reprint-label-history`

**Parameters:**

- **From** (`from`) - date, optional
- **To** (`to`) - date, optional
- **Store** (`location`) - text, optional

**Columns:** Document Date · Barcode · SKU · Design No · Combination ID · Store Code · Store Name · Reason · User ID · User Name · Created At

**Drill-down:** no.

### Barcode-wise Cost & MRP Report

**Report id:** `cat-barcode-wise-cost-and-mrp-report`

**Parameters:**

- **Store** (`location`) - text, optional

**Columns:** As Of Date · Store Code · Store Name · Location Name · Item Code · Item Name · SKU · Barcode · Design No · Combination ID · Category Name · Vendor Name · PO No · GRN No · Purchase Price *(sensitive)* · Landed Cost *(sensitive)* · MRP · Cost Value *(sensitive)* · MRP Value · Margin Amount *(sensitive)* · Margin % *(sensitive)* · Stock Status

**Drill-down:** no.

### Batch Lookup & Ageing

**Report id:** `batch-ageing`

**Parameters:**

- **Scan batch, barcode or SKU** (`scan`) - text, optional
- **Older than (days)** (`min_age_days`) - text, optional
- **Show** (`show`) - select, optional, one of: All batches,Only with stock left

**Columns:** Batch · Item · Received On · Age (days) · Age Band · GRN · Supplier · Received Qty · Est. Left (FIFO) · Expires · Batch Status

**Drill-down:** no.

### Current Stock

**Report id:** `current-stock`

**Parameters:** none - it runs as soon as you pick it.

**Columns:** SKU · Location · On Hand · Available · Committed · Reserved · Safety Stock

**Drill-down:** no.

### Current Stock Barcode Detail

**Report id:** `cat-current-stock-barcode-detail`

**Parameters:**

- **Store** (`location`) - text, optional

**Columns:** As Of Date · Store Code · Store Name · Location Code · Location Name · Item Code · Item Name · SKU · Barcode · Design No · Combination ID · Category Name · EAN Code · MRP · Purchase Price *(sensitive)* · Stock Status · Rack · Shelf · Box

**Drill-down:** no.

### Current Stock Itemwise Detail

**Report id:** `cat-current-stock-itemwise-detail`

**Parameters:**

- **Store** (`location`) - text, optional

**Columns:** As Of Date · Store Code · Store Name · Location Code · Location Name · Item Code · Item Name · SKU · Barcode · Design No · Combination ID · Category Name · Available Qty · Blocked Qty · In-Transit Qty · MRP · MRP Value · Stock Status

**Drill-down:** no.

### Current Stock Summary

**Report id:** `cat-current-stock-summary`

**Parameters:**

- **Store** (`location`) - text, optional

**Columns:** As Of Date · Store Code · Store Name · Location Code · Location Name · Category Name · SKU · Available Qty · Blocked Qty · In-Transit Qty · MRP Value

**Drill-down:** no.

### Current Stock with Image

**Report id:** `cat-current-stock-with-image`

**Parameters:**

- **Store** (`location`) - text, optional

**Columns:** As Of Date · Store Code · Store Name · Location Name · Design No · Combination ID · SKU · Item Name · Category Name · Available Qty · MRP · Image URL

**Drill-down:** no.

### Demand Forecast (Trend)

**Report id:** `demand-forecast`

**Parameters:**

- **SKU** (`sku`) - text, **required**
- **Location** (`location_code`) - text, **required**
- **Forecast Days** (`forecast_days`) - text, **required**

**Columns:** SKU · Location · Forecasted Qty

**Drill-down:** no.

### Goods In Receiving

**Report id:** `grn-in-receiving`

**Parameters:**

- **Store code (blank = all)** (`store`) - text, optional

**Columns:** Store · SKU · In receiving · Receipts · Received since

**Drill-down:** no.

### Inventory Matrix Summary

**Report id:** `cat-inventory-matrix-summary`

**Parameters:**

- **Store** (`location`) - text, optional

**Columns:** As Of Date · Store Code · Store Name · Category Name · Design No · Unique Design Count · Available Qty · MRP Value

**Drill-down:** no.

### Inventory Valuation

**Report id:** `inventory-valuation`

**Parameters:** none - it runs as soon as you pick it.

**Columns:** SKU · Qty On Hand · Unit Cost (Avg) *(sensitive)* · Total Value *(sensitive)* · Costed

**Drill-down:** no.

### Inventory by Location Summary

**Report id:** `cat-inventory-by-location-summary`

**Parameters:**

- **Store** (`location`) - text, optional

**Columns:** As Of Date · Store Code · Store Name · Location Code · Location Name · Closing Stock Qty · Unique Design Count · MRP Value

**Drill-down:** no.

### Item Classification Register

**Report id:** `item-classification-register`

**Parameters:**

- **Item Type** (`item_type`) - text, optional
- **Sellability** (`sellability`) - select, optional, one of: ,Normal sale,Promo only,Not sellable,Stored-value issue only

**Columns:** SKU · Item · Item Type · Class · Stock · Valuation · Accounting · Sellability · Bill · Capture · Consumption · Item overrides · Status

**Drill-down:** no.

### MRP Change Register

**Report id:** `cat-mrp-change-register`

**Parameters:**

- **From** (`from`) - date, optional
- **To** (`to`) - date, optional

**Columns:** Document Date · Document No · Store Code · Store Name · Design No · Combination ID · Barcode · MRP · New Value · Reason · Status · Created By · Approved By

**Drill-down:** no.

### Negative Stock Exception

**Report id:** `cat-negative-stock-exception`

**Parameters:**

- **Store** (`location`) - text, optional

**Columns:** As Of Date · Store Code · Store Name · Location Name · SKU · Barcode · Item Name · Closing Stock Qty · Reason · Source Document No

**Drill-down:** no.

### Non-Valuated Stock

**Report id:** `non-valuated-stock`

**Parameters:** none - it runs as soon as you pick it.

**Columns:** SKU · Item · Item Type · Store · On hand · Available

**Drill-down:** no.

### Pegged Demand & Supply

**Report id:** `pegged-demand-supply`

**Parameters:**

- **SKU** (`sku`) - text, **required**
- **Location (for on-hand supply)** (`location_code`) - text, optional

**Columns:** Kind · Type · Reference · Qty · Status

**Drill-down:** no.

### Physical Stock Count Register

**Report id:** `cat-physical-stock-count-register`

**Parameters:**

- **From** (`from`) - date, optional
- **To** (`to`) - date, optional
- **Store** (`location`) - text, optional
- **Status (optional)** (`status`) - select, optional, one of: ,Open,Counting,Review,Closed,Posted

**Columns:** Document Date · Count Reference · Store Code · Store Name · Location Name · System Qty · Physical Qty · Difference Qty · Status · Created By · Approved By

**Drill-down:** no.

### Physical Stock Variance Report

**Report id:** `cat-physical-stock-variance-report`

**Parameters:**

- **From** (`from`) - date, optional
- **To** (`to`) - date, optional
- **Store** (`location`) - text, optional

**Columns:** Document Date · Count Reference · Store Code · Store Name · Location Name · Item Code · Item Name · SKU · Barcode · Design No · Combination ID · Category Name · System Qty · Physical Qty · Difference Qty · Reason · Purchase Price *(sensitive)* · MRP · Approval Status

**Drill-down:** no.

### Reorder Suggestions (Configured)

**Report id:** `reorder-suggestions`

**Parameters:**

- **Location** (`location_code`) - text, **required**
- **Default Lead Time (days)** (`default_lead_time_days`) - text, **required**
- **Default Safety Stock** (`default_safety_stock`) - text, **required**

**Columns:** SKU · Location · Available · Daily Velocity · Reorder Point · Suggested Qty · Safety Stock · Lead Time (days)

**Drill-down:** no.

### Stock Ageing (by GRN batch)

**Report id:** `stock-ageing`

**Parameters:**

- **Scan barcode or SKU (optional)** (`scan`) - text, optional

**Columns:** Item · On Hand · 0-30 days · 31-60 days · 61-90 days · 91-180 days · Over 180 days · Before batch numbers · Oldest Batch Held · Oldest Received On

**Drill-down:** no.

### Stock Aging

**Report id:** `cat-stock-aging`

**Parameters:**

- **Store** (`location`) - text, optional

**Columns:** As Of Date · Store Code · Store Name · Location Name · Item Code · Item Name · SKU · Barcode · Design No · Combination ID · Category Name · Available Qty · MRP · Stock Age Days · Age Bucket · Last GRN Date · Last Sale Date · MRP Value

**Drill-down:** no.

### Stock Book vs Ledger (drift finder)

**Report id:** `stock-book-vs-ledger`

**Parameters:**

- **Item Code (optional)** (`sku`) - text, optional
- **Location (optional)** (`location`) - text, optional
- **Include rows that agree** (`all`) - select, optional, one of: No,Yes

**Columns:** Item · Location · On hand (book) · Ledger total · Difference · Ledger lines · What it suggests

**Drill-down:** no.

### Stock Details - Period Wise

**Report id:** `cat-stock-details-period-wise`

**Parameters:**

- **From** (`from`) - date, optional
- **To** (`to`) - date, optional
- **Store** (`location`) - text, optional

**Columns:** From Date · To Date · Store Code · Store Name · Location Code · Location Name · EAN Code · Barcode · Item Name · Design No · Style · Color · Polish · Size · Brand · Configured Attribute 1 · Configured Attribute 2 · Category Name · Transaction HSN Code · GST % · Item Type · Rack · Shelf · Box · Opening Stock Qty · Inward Qty · Outward Qty · Closing Stock Qty · Stock Value at Purchase Price *(sensitive)* · Stock Value at Landed Cost *(sensitive)* · Stock Value at Selling Price · Stock Value at MRP · Stock Value Excl. Tax

**Drill-down:** no.

### Stock Ledger

**Report id:** `stock-ledger`

**Parameters:**

- **SKU (optional)** (`sku`) - text, optional
- **Location (optional)** (`location_code`) - text, optional
- **Voucher Type (optional)** (`voucher_type`) - text, optional
- **From (optional)** (`start`) - date, optional
- **To (optional)** (`end`) - date, optional

**Columns:** Date · SKU · Location · Qty Delta · Running Balance · Voucher Type · Voucher ID · From Location/Bin · To Location/Bin · From Condition · To Condition · User

**Drill-down:** no.

### Stock Ledger

**Report id:** `cat-stock-ledger`

**Parameters:**

- **From** (`from`) - date, optional
- **To** (`to`) - date, optional
- **Store** (`location`) - text, optional

**Columns:** Movement Date · Store Code · Store Name · Location Code · Location Name · Item Code · Item Name · SKU · Barcode · Design No · Combination ID · Category Name · Movement Type · Qty · Source Document Type · Source Document No · Stock Status · Created By

**Drill-down:** no.

### Stock Ledger - Category Summary

**Report id:** `cat-stock-ledger-category-summary`

**Parameters:**

- **From** (`from`) - date, optional
- **To** (`to`) - date, optional
- **Store** (`location`) - text, optional

**Columns:** From Date · To Date · Category Code · Category Name · Store Code · Store Name · Location Code · Location Name · Opening Stock Qty · Inward Qty · Outward Qty · Closing Stock Qty

**Drill-down:** no.

### Stock Movement History

**Report id:** `cat-stock-movement-history`

**Parameters:**

- **From** (`from`) - date, optional
- **To** (`to`) - date, optional
- **Store** (`location`) - text, optional

**Columns:** Movement Date · Barcode · SKU · Design No · Combination ID · From Location · To Location · Movement Type · Qty · Source Document Type · Source Document No · Created By

**Drill-down:** no.

### Stock Valuation Summary

**Report id:** `cat-stock-valuation-summary`

**Parameters:**

- **Store** (`location`) - text, optional

**Columns:** As Of Date · Store Code · Store Name · Location Name · Category Name · Closing Stock Qty · Stock Value at Purchase Price *(sensitive)* · Stock Value at Landed Cost *(sensitive)* · Stock Value at MRP · Stock Value Excl. Tax

**Drill-down:** no.

### Stock by Location

**Report id:** `stock-by-location`

**Parameters:**

- **Store code (blank = all)** (`store`) - text, optional

**Columns:** Store · SKU · Main · Inward · In-Transit · QC Hold · Damage · On Hold · Purchase Return · Own locations · Sellable

**Drill-down:** no.

### Unique Design Inventory Summary

**Report id:** `cat-unique-design-inventory-summary`

**Parameters:**

- **Store** (`location`) - text, optional

**Columns:** As Of Date · Store Code · Store Name · Category Name · Unique Design Count · Available Qty · MRP Value

**Drill-down:** no.

## MIS & Management

### Ageing Inventory Value

**Report id:** `cat-ageing-inventory-value`

**Parameters:**

- **From** (`from`) - date, optional
- **To** (`to`) - date, optional
- **Store** (`location`) - text, optional

**Columns:** As Of Date · Store Name · Category Name · Age Bucket · Closing Stock Qty · Stock Value at Purchase Price *(sensitive)* · Stock Value at MRP

**Drill-down:** no.

### Category Performance Summary

**Report id:** `cat-category-performance-summary`

**Parameters:**

- **From** (`from`) - date, optional
- **To** (`to`) - date, optional
- **Store** (`location`) - text, optional

**Columns:** From Date · To Date · Category Name · Sold Qty · Net Sales Amount · Margin Amount *(sensitive)* · Margin % *(sensitive)* · Closing Stock Qty · MRP Value · Sell Through %

**Drill-down:** no.

### Daily Business Summary

**Report id:** `cat-daily-business-summary`

**Parameters:**

- **From** (`from`) - date, optional
- **To** (`to`) - date, optional
- **Store** (`location`) - text, optional

**Columns:** Document Date · Store Code · Store Name · Bill Count · Sold Qty · Return Qty · Net Sales Amount · ABV · UPT · Discount Amount · Closing Stock Qty · MRP Value

**Drill-down:** no.

### Fast / Slow Moving Stock

**Report id:** `cat-fast-slow-moving-stock`

**Parameters:**

- **From** (`from`) - date, optional
- **To** (`to`) - date, optional
- **Store** (`location`) - text, optional

**Columns:** As Of Date · SKU · Design No · Item Name · Category Name · Available Qty · Sold Qty · Stock Age Days · Last Sale Date · MRP Value · Status

**Drill-down:** no.

### Gross Margin Summary

**Report id:** `cat-gross-margin-summary`

**Parameters:**

- **From** (`from`) - date, optional
- **To** (`to`) - date, optional
- **Store** (`location`) - text, optional

**Columns:** From Date · To Date · Store Name · Category Name · Net Sales Amount · Cost Value *(sensitive)* · Margin Amount *(sensitive)* · Margin % *(sensitive)*

**Drill-down:** no.

### Inventory Turnover Summary

**Report id:** `cat-inventory-turnover-summary`

**Parameters:**

- **From** (`from`) - date, optional
- **To** (`to`) - date, optional
- **Store** (`location`) - text, optional

**Columns:** From Date · To Date · Store Name · Category Name · Sold Qty · Closing Stock Qty · Inventory Turnover · Sell Through %

**Drill-down:** no.

### Management Stock & Sales Summary

**Report id:** `cat-management-stock-and-sales-summary`

**Parameters:**

- **From** (`from`) - date, optional
- **To** (`to`) - date, optional
- **Store** (`location`) - text, optional

**Columns:** From Date · To Date · Store Name · Category Name · Net Sales Amount · Sold Qty · Closing Stock Qty · MRP Value · Margin % *(sensitive)* · Stock Cover Days

**Drill-down:** no.

### Sales vs Target

**Report id:** `cat-sales-vs-target`

**Parameters:**

- **From** (`from`) - date, optional
- **To** (`to`) - date, optional
- **Store** (`location`) - text, optional

**Columns:** From Date · To Date · Store Code · Store Name · Target Amount · Net Sales Amount · Achievement % · Rank

**Drill-down:** no.

### Stock Cover Report

**Report id:** `cat-stock-cover-report`

**Parameters:**

- **From** (`from`) - date, optional
- **To** (`to`) - date, optional
- **Store** (`location`) - text, optional

**Columns:** As Of Date · Store Name · Category Name · SKU · Available Qty · Sold Qty · Stock Cover Days · Status

**Drill-down:** no.

### Store Performance / Ranking

**Report id:** `cat-store-performance-ranking`

**Parameters:**

- **From** (`from`) - date, optional
- **To** (`to`) - date, optional
- **Store** (`location`) - text, optional

**Columns:** From Date · To Date · Store Code · Store Name · Bill Count · Sold Qty · Net Sales Amount · ABV · UPT · Margin % *(sensitive)* · Rank

**Drill-down:** no.

## Manufacturing

### BOM Costing Report

**Report id:** `cat-bom-costing-report`

**Parameters:**

- **From** (`from`) - date, optional
- **To** (`to`) - date, optional

**Columns:** BOM Code · Parent Item · Component Item · Component Qty · Purchase Price *(sensitive)* · Standard Cost *(sensitive)* · Cost Value *(sensitive)* · Status

**Drill-down:** no.

### BOM Register

**Report id:** `cat-bom-register`

**Parameters:**

- **From** (`from`) - date, optional
- **To** (`to`) - date, optional

**Columns:** BOM Code · Parent Item · Component Item · Component Qty · UOM · Status · Created By

**Drill-down:** no.

### Batch / Lot Traceability

**Report id:** `cat-batch-lot-traceability`

**Parameters:**

- **From** (`from`) - date, optional
- **To** (`to`) - date, optional
- **Store** (`location`) - text, optional

**Columns:** Document Date · Production Order No · Parent Item · Component Item · Barcode · Source Document No · Movement Type · Qty · Status

**Drill-down:** no.

### MRP Suggestions

**Report id:** `cat-mrp-suggestions`

**Parameters:**

- **From** (`from`) - date, optional
- **To** (`to`) - date, optional
- **Store** (`location`) - text, optional
- **Status (optional)** (`status`) - select, optional, one of: ,Proposed,Approved,Rejected,Converted

**Columns:** As Of Date · Component Item · Available Qty · Reserved Qty · MRP Suggestion Qty · Promise Date · Status

**Drill-down:** no.

### Material Issue / Consumption Report

**Report id:** `cat-material-issue-consumption-report`

**Parameters:**

- **From** (`from`) - date, optional
- **To** (`to`) - date, optional
- **Store** (`location`) - text, optional

**Columns:** Document Date · Production Order No · Component Item · Component Qty · Qty · From Location · Work Center · Actual Cost *(sensitive)* · Status

**Drill-down:** no.

### Material Usage Variance

**Report id:** `cat-material-usage-variance`

**Parameters:**

- **From** (`from`) - date, optional
- **To** (`to`) - date, optional
- **Store** (`location`) - text, optional

**Columns:** Production Order No · Component Item · Component Qty · Qty · Variance Qty · Standard Cost *(sensitive)* · Actual Cost *(sensitive)* · Cost Variance *(sensitive)*

**Drill-down:** no.

### Production Capacity Schedule

**Report id:** `production-capacity-schedule`

**Parameters:** none - it runs as soon as you pick it.

**Columns:** Production Order · Operation Seq · Work Center · Needed Minutes · Finite Date · Infinite Date · Overflow (past due date)

**Drill-down:** no.

### Production Cost Variance

**Report id:** `production_cost_variance`

**Parameters:** none - it runs as soon as you pick it.

**Columns:** Production Order · BOM · Quantity · Standard Cost (Total) · Actual Cost · Variance · Variance % · Flag

**Drill-down:** no.

### Production Order Register

**Report id:** `cat-production-order-register`

**Parameters:**

- **From** (`from`) - date, optional
- **To** (`to`) - date, optional
- **Store** (`location`) - text, optional
- **Status (optional)** (`status`) - select, optional, one of: ,Draft,Released,In Progress,Completed,Closed,Cancelled

**Columns:** Production Order No · Document Date · Parent Item · BOM Code · Planned Qty · Completed Qty · WIP Qty · Work Center · Routing · Status

**Drill-down:** no.

### Production Order Status

**Report id:** `production-order-status`

**Parameters:** none - it runs as soon as you pick it.

**Columns:** Order ID · BOM · Finished Item · Qty · Status · Date

**Drill-down:** no.

### Production Receipt / Completion

**Report id:** `cat-production-receipt-completion`

**Parameters:**

- **From** (`from`) - date, optional
- **To** (`to`) - date, optional
- **Store** (`location`) - text, optional
- **Status (optional)** (`status`) - select, optional, one of: ,Draft,Released,In Progress,Completed,Closed,Cancelled

**Columns:** Document Date · Production Order No · Parent Item · Completed Qty · Scrap Qty · Yield % · To Location · QC Result · Status

**Drill-down:** no.

### Production Schedule

**Report id:** `cat-production-schedule`

**Parameters:**

- **From** (`from`) - date, optional
- **To** (`to`) - date, optional
- **Store** (`location`) - text, optional
- **Status (optional)** (`status`) - select, optional, one of: ,Draft,Released,In Progress,Completed,Closed,Cancelled

**Columns:** Document Date · Production Order No · Parent Item · Work Center · Planned Qty · Promise Date · Status

**Drill-down:** no.

### Quality Inspection Report

**Report id:** `cat-quality-inspection-report`

**Parameters:**

- **From** (`from`) - date, optional
- **To** (`to`) - date, optional
- **Store** (`location`) - text, optional
- **Status (optional)** (`status`) - select, optional, one of: ,Draft,Released,Rejected,Open,Closed

**Columns:** Document Date · Production Order No · Parent Item · Work Center · QC Result · Rejected Qty · Reason · User Name · Status

**Drill-down:** no.

### Rework / Rejection Report

**Report id:** `cat-rework-rejection-report`

**Parameters:**

- **From** (`from`) - date, optional
- **To** (`to`) - date, optional
- **Store** (`location`) - text, optional
- **Status (optional)** (`status`) - select, optional, one of: ,Draft,Released,Rejected,Open,Closed

**Columns:** Document Date · Production Order No · Parent Item · Rejected Qty · Qty · Reason · QC Result · Status

**Drill-down:** no.

### Standard vs Actual Cost

**Report id:** `cat-standard-vs-actual-cost`

**Parameters:**

- **From** (`from`) - date, optional
- **To** (`to`) - date, optional
- **Store** (`location`) - text, optional
- **Status (optional)** (`status`) - select, optional, one of: ,Draft,Released,In Progress,Completed,Closed,Cancelled

**Columns:** Production Order No · Parent Item · Completed Qty · Standard Cost *(sensitive)* · Actual Cost *(sensitive)* · Cost Variance *(sensitive)* · Margin % *(sensitive)*

**Drill-down:** no.

### Subcontracting / Job Work Report

**Report id:** `cat-subcontracting-job-work-report`

**Parameters:**

- **From** (`from`) - date, optional
- **To** (`to`) - date, optional
- **Store** (`location`) - text, optional

**Columns:** Document Date · Job Work No · Vendor Code · Vendor Name · Component Item · Qty · Expected Return Date · Work Result · Status

**Drill-down:** no.

### Work Center Capacity / Load

**Report id:** `cat-work-center-capacity-load`

**Parameters:**

- **From** (`from`) - date, optional
- **To** (`to`) - date, optional
- **Store** (`location`) - text, optional

**Columns:** From Date · To Date · Work Center · Planned Qty · Completed Qty · WIP Qty · Achievement %

**Drill-down:** no.

### Work In Process Report

**Report id:** `cat-work-in-process-report`

**Parameters:**

- **From** (`from`) - date, optional
- **To** (`to`) - date, optional
- **Store** (`location`) - text, optional
- **Status (optional)** (`status`) - select, optional, one of: ,Draft,Released,In Progress,Completed,Closed,Cancelled

**Columns:** As Of Date · Production Order No · Parent Item · Work Center · Routing · Planned Qty · Completed Qty · WIP Qty · Actual Cost *(sensitive)* · Status

**Drill-down:** no.

### Yield / Scrap Analysis

**Report id:** `cat-yield-scrap-analysis`

**Parameters:**

- **From** (`from`) - date, optional
- **To** (`to`) - date, optional
- **Store** (`location`) - text, optional
- **Status (optional)** (`status`) - select, optional, one of: ,Draft,Released,In Progress,Completed,Closed,Cancelled

**Columns:** From Date · To Date · Production Order No · Parent Item · Planned Qty · Completed Qty · Scrap Qty · Yield % · Reason

**Drill-down:** no.

## Master Data & Audit

### Approval History Report

**Report id:** `cat-approval-history-report`

**Parameters:**

- **From** (`from`) - date, optional
- **To** (`to`) - date, optional
- **Store** (`location`) - text, optional

**Columns:** Document Type · Document No · Approval Status · Approved By · Approved At · Reason · Remarks

**Drill-down:** no.

### Backdated Entry Report

**Report id:** `cat-backdated-entry-report`

**Parameters:**

- **From** (`from`) - date, optional
- **To** (`to`) - date, optional
- **Store** (`location`) - text, optional
- **Status (optional)** (`status`) - select, optional, one of: ,Pending,Approved,Rejected

**Columns:** Document Type · Document No · Document Date · Created At · Store Name · User ID · User Name · Reason · Status

**Drill-down:** no.

### Item Master Register

**Report id:** `cat-item-master-register`

**Parameters:**

- **From** (`from`) - date, optional
- **To** (`to`) - date, optional
- **Status (optional)** (`status`) - select, optional, one of: ,Active,Inactive,Draft

**Columns:** Item Code · Item Name · SKU · Barcode · EAN Code · Design No · Combination ID · Category Name · Subcategory · Brand · Style · Color · Polish · Size · HSN Code · GST % · MRP · Item Type · Status

**Drill-down:** no.

### Master Data Change Audit

**Report id:** `cat-master-data-change-audit`

**Parameters:**

- **From** (`from`) - date, optional
- **To** (`to`) - date, optional

**Columns:** Audit ID · Document Type · Document No · Field Changed · Old Value · New Value · Edit Reason · Edited By · Edited At

**Drill-down:** no.

### Report Download Audit

**Report id:** `cat-report-download-audit`

**Parameters:**

- **From** (`from`) - date, optional
- **To** (`to`) - date, optional

**Columns:** Report Name · From Date · To Date · User ID · User Name · Export Format · Download At · IP Address

**Drill-down:** no.

### Role Permission Audit

**Report id:** `cat-role-permission-audit`

**Parameters:** none - it runs as soon as you pick it.

**Columns:** Document Date · Document No · Status · Created By · Approved By · Remarks

**Drill-down:** no.

### Store / Location Master Register

**Report id:** `cat-store-location-master-register`

**Parameters:**

- **From** (`from`) - date, optional
- **To** (`to`) - date, optional
- **Store** (`location`) - text, optional
- **Status (optional)** (`status`) - select, optional, one of: ,Active,Inactive

**Columns:** Legal Entity · Store Code · Store Name · Outlet ID · GSTIN · Location Code · Location Name · Status · Created By · Created At

**Drill-down:** no.

### Transaction Cancellation Audit

**Report id:** `cat-transaction-cancellation-audit`

**Parameters:**

- **From** (`from`) - date, optional
- **To** (`to`) - date, optional
- **Store** (`location`) - text, optional

**Columns:** Document Type · Document No · Document Date · Store Name · Edit Action · Reason · User ID · User Name · Approved By · Approved At

**Drill-down:** no.

### User Access Matrix Report

**Report id:** `cat-user-access-matrix-report`

**Parameters:**

- **Store** (`location`) - text, optional

**Columns:** User ID · User Name · Status · Store Code · Store Name · Created At · Approved By

**Drill-down:** no.

### User Activity Log

**Report id:** `cat-user-activity-log`

**Parameters:**

- **From** (`from`) - date, optional
- **To** (`to`) - date, optional

**Columns:** Created At · User ID · User Name · Edit Action · Document Type · Document No · IP Address · Remarks

**Drill-down:** no.

### Vendor Master Register

**Report id:** `cat-vendor-master-register`

**Parameters:**

- **From** (`from`) - date, optional
- **To** (`to`) - date, optional
- **Status (optional)** (`status`) - select, optional, one of: ,Active,Inactive

**Columns:** Vendor Code · Vendor Name · Vendor GSTIN · Status · Created By · Created At

**Drill-down:** no.

## OMS

### ATP / Promise Inventory Report

**Report id:** `cat-atp-promise-inventory-report`

**Parameters:**

- **Store** (`location`) - text, optional

**Columns:** As Of Date · SKU · Item Name · Allocated Location · Available Qty · Reserved Qty · ATP Qty · Promise Date

**Drill-down:** no.

### Allocation Pending

**Report id:** `allocation-pending`

**Parameters:** none - it runs as soon as you pick it.

**Columns:** Order · Customer · Channel · Total Amount *(sensitive)* · Created At

**Drill-down:** no.

### Channel Order Reconciliation

**Report id:** `cat-channel-order-reconciliation`

**Parameters:**

- **From** (`from`) - date, optional
- **To** (`to`) - date, optional
- **Store** (`location`) - text, optional

**Columns:** Order ID · Channel · Order Status · UC Status · Net Amount · Integration Status · Error Code · Error Message

**Drill-down:** no.

### Courier Performance

**Report id:** `courier-performance`

**Parameters:** none - it runs as soon as you pick it.

**Columns:** Carrier · Total Shipments · Delivered · RTO · RTO Rate % · Avg Handover->Delivered (hrs)

**Drill-down:** no.

### Fulfilment Status Report

**Report id:** `cat-fulfilment-status-report`

**Parameters:**

- **From** (`from`) - date, optional
- **To** (`to`) - date, optional
- **Store** (`location`) - text, optional

**Columns:** Order ID · Shipment ID · Shipment Sequence · Allocated Location · Fulfilment Status · Tracking Number · Shipping Provider · Promise Date · SLA Due At · SLA Age Hours · Status

**Drill-down:** no.

### OMS Exception Queue

**Report id:** `oms-exception-queue`

**Parameters:** none - it runs as soon as you pick it.

**Columns:** Event ID · Event · Status · Attempts · Queued At · Last Error

**Drill-down:** no.

### OMS Integration Error / Retry

**Report id:** `cat-oms-integration-error-retry`

**Parameters:**

- **From** (`from`) - date, optional
- **To** (`to`) - date, optional

**Columns:** Created At · Order ID · Shipment ID · Channel · Integration Status · Error Code · Error Message · Retry Count · Last Retry At

**Drill-down:** no.

### OMS Reconciliation Variance

**Report id:** `oms-reconciliation-variance`

**Parameters:** none - it runs as soon as you pick it.

**Columns:** Order · Order Status · Shipment/Booking · Booking Status · Variance

**Drill-down:** no.

### Order Aging

**Report id:** `order-aging`

**Parameters:** none - it runs as soon as you pick it.

**Columns:** Age Bucket · Order Count · Total Amount *(sensitive)*

**Drill-down:** yes - a row's **View Details** opens the transactions behind it.

### Order Allocation / Reservation Report

**Report id:** `cat-order-allocation-reservation-report`

**Parameters:**

- **From** (`from`) - date, optional
- **To** (`to`) - date, optional
- **Store** (`location`) - text, optional

**Columns:** Order ID · Shipment ID · SKU · Qty · Allocated Location · Available Qty · Reserved Qty · Allocation Status · Promise Date · Status

**Drill-down:** no.

### Order Cancellation Report

**Report id:** `cat-order-cancellation-report`

**Parameters:**

- **From** (`from`) - date, optional
- **To** (`to`) - date, optional
- **Store** (`location`) - text, optional

**Columns:** Order Date · Order ID · Channel · SKU · Qty · Net Amount · Cancellation Reason · User Name · Status

**Drill-down:** no.

### Order Line Detail

**Report id:** `cat-order-line-detail`

**Parameters:**

- **From** (`from`) - date, optional
- **To** (`to`) - date, optional
- **Store** (`location`) - text, optional

**Columns:** Order Date · Order ID · Channel · Channel SKU · SKU · Item Name · Qty · MRP · Discount Amount · Net Amount · Line Status · Order Status

**Drill-down:** no.

### Order Register

**Report id:** `cat-order-register`

**Parameters:**

- **From** (`from`) - date, optional
- **To** (`to`) - date, optional
- **Store** (`location`) - text, optional

**Columns:** Order Date · Order ID · Channel · Customer ID · Customer Name · Customer Mobile · Order Status · Gross Amount · Discount Amount · Net Amount · Payment Status · Status

**Drill-down:** no.

### Order to Invoice Reconciliation

**Report id:** `cat-order-to-invoice-reconciliation`

**Parameters:**

- **From** (`from`) - date, optional
- **To** (`to`) - date, optional
- **Store** (`location`) - text, optional

**Columns:** Order ID · Shipment ID · Invoice Code · POS Invoice No · Order Status · Invoice Status · POS Bill Status · Package Amount · Net Amount · Variance Qty

**Drill-down:** no.

### Orphaned Channel Orders (pre-35.1 intake)

**Report id:** `orphaned-channel-orders`

**Parameters:** none - it runs as soon as you pick it.

**Columns:** Mapping Table · Channel · Channel Order ID · Legacy Order ID · Last Written

**Drill-down:** no.

### Pending Order / SLA Aging

**Report id:** `cat-pending-order-sla-aging`

**Parameters:**

- **From** (`from`) - date, optional
- **To** (`to`) - date, optional
- **Store** (`location`) - text, optional

**Columns:** Order Date · Order ID · Shipment ID · Channel · Order Status · Fulfilment Status · SLA Due At · SLA Age Hours · Status

**Drill-down:** no.

### Refund Status Report

**Report id:** `cat-refund-status-report`

**Parameters:**

- **From** (`from`) - date, optional
- **To** (`to`) - date, optional
- **Store** (`location`) - text, optional

**Columns:** Document Date · Order ID · Shipment ID · Customer Name · Refund Amount · Refund Status · Reference No · Created At

**Drill-down:** no.

### Reserved Stock

**Report id:** `reserved-stock`

**Parameters:** none - it runs as soon as you pick it.

**Columns:** SKU · Location · Reserved · Committed · On Hand · Available

**Drill-down:** no.

### Return / RTO Register

**Report id:** `cat-return-rto-register`

**Parameters:**

- **From** (`from`) - date, optional
- **To** (`to`) - date, optional
- **Store** (`location`) - text, optional

**Columns:** Document Date · Order ID · Shipment ID · Tracking Number · SKU · Qty · Return Status · Reason · Refund Status · Refund Amount

**Drill-down:** no.

### Return Aging

**Report id:** `return-aging`

**Parameters:** none - it runs as soon as you pick it.

**Columns:** Age Bucket · Return Request Count

**Drill-down:** yes - a row's **View Details** opens the transactions behind it.

### SLA Breach

**Report id:** `sla-breach`

**Parameters:**

- **Threshold (minutes, default 120)** (`threshold_minutes`) - text, optional

**Columns:** Task ID · Order · Location · Status · Minutes Elapsed · Threshold (mins)

**Drill-down:** no.

### Settlement Variance Queue

**Report id:** `oms-settlement-variance`

**Parameters:** none - it runs as soon as you pick it.

**Columns:** Settlement Line · Channel · Channel Order ID · Order · Batch · Gross Amount *(sensitive)* · Expected (Invoiced) *(sensitive)* · Variance *(sensitive)* · Status · Settlement Date

**Drill-down:** no.

### Split Shipment Report

**Report id:** `cat-split-shipment-report`

**Parameters:**

- **From** (`from`) - date, optional
- **To** (`to`) - date, optional
- **Store** (`location`) - text, optional

**Columns:** Order ID · Shipment ID · Shipment Sequence · Allocated Location · Qty · Package Amount · Invoice Code · Tracking Number · Fulfilment Status

**Drill-down:** no.

### Stock Mismatch

**Report id:** `stock-mismatch`

**Parameters:** none - it runs as soon as you pick it.

**Columns:** SKU · Location · On Hand · Available · Reserved · Safety Stock · Blocked · QC Hold · Damaged · Channel Buffer · ATS (negative = over-committed)

**Drill-down:** no.

### Unsettled Marketplace Orders

**Report id:** `oms-unsettled-settlements`

**Parameters:** none - it runs as soon as you pick it.

**Columns:** Order · Channel · Order Status · Days Since Ship · Overdue

**Drill-down:** no.

## PIM

### PIM Overdue Tasks

**Report id:** `pim-task-overdue`

**Parameters:**

- **Assignee (blank = everyone)** (`assignee`) - text, optional

**Columns:** Task · Title · Item · Assignee · Due · Days Overdue · Priority · Status

**Drill-down:** no.

### PIM Product Group Readiness

**Report id:** `pim-product-group-readiness`

**Parameters:**

- **Product Group ID or code** (`group_id`) - text, **required**

**Columns:** Item · Name · Family · Status · Completeness % · Missing Fields

**Drill-down:** no.

### PIM Stalled Workflow Runs

**Report id:** `pim-workflow-stalled`

**Parameters:** none - it runs as soon as you pick it.

**Columns:** Run · Workflow · Item · Stage · Status · Why It Is Waiting · Age (days) · Open Tasks

**Drill-down:** no.

### PIM Task Workload by Assignee

**Report id:** `pim-task-workload`

**Parameters:** none - it runs as soon as you pick it.

**Columns:** Assignee · Open · In Progress · Blocked · Overdue · Due This Week

**Drill-down:** no.

## PIM / PIMS

### Attribute Definition Report

**Report id:** `cat-attribute-definition-report`

**Parameters:**

- **From** (`from`) - date, optional
- **To** (`to`) - date, optional

**Columns:** Attribute Name · Attribute Value · Product Family · Family Attribute Required · Status · Created By

**Drill-down:** no.

### Attribute Group Report

**Report id:** `cat-attribute-group-report`

**Parameters:**

- **From** (`from`) - date, optional
- **To** (`to`) - date, optional

**Columns:** Attribute Group · Attribute Name · Sequence No · Mandatory Flag · Status

**Drill-down:** no.

### Category / Attribute Mapping Report

**Report id:** `cat-category-attribute-mapping-report`

**Parameters:**

- **From** (`from`) - date, optional
- **To** (`to`) - date, optional

**Columns:** Product Family · Category Name · Channel · Attribute Name · Attribute Value · Mapping Status · Status

**Drill-down:** no.

### Channel Publication Error Report

**Report id:** `cat-channel-publication-error-report`

**Parameters:** none - it runs as soon as you pick it.

**Columns:** SKU · Channel SKU · Channel · Publication Status · Publication Error · Retry Count · Last Retry At

**Drill-down:** no.

### Channel Publication Status

**Report id:** `cat-channel-publication-status`

**Parameters:** none - it runs as soon as you pick it.

**Columns:** SKU · Channel SKU · Channel · Publication Status · Publication Error · Created At · Status

**Drill-down:** no.

### Channel Readiness Report

**Report id:** `cat-channel-readiness-report`

**Parameters:** none - it runs as soon as you pick it.

**Columns:** SKU · Channel · Completeness % · Media Count · Channel Readiness · Publication Status · Publication Error

**Drill-down:** no.

### Duplicate SKU / Barcode Exception

**Report id:** `cat-duplicate-sku-barcode-exception`

**Parameters:** none - it runs as soon as you pick it.

**Columns:** SKU · Barcode · EAN Code · Design No · Combination ID · Item Name · Reason · Status

**Drill-down:** no.

### Family Attribute Mapping Report

**Report id:** `cat-family-attribute-mapping-report`

**Parameters:**

- **From** (`from`) - date, optional
- **To** (`to`) - date, optional

**Columns:** Product Family · Attribute Group · Attribute Name · Mandatory Flag · Status

**Drill-down:** no.

### Media / Image Coverage Report

**Report id:** `cat-media-image-coverage-report`

**Parameters:**

- **From** (`from`) - date, optional
- **To** (`to`) - date, optional
- **Status (optional)** (`status`) - select, optional, one of: ,Active,Inactive,Draft

**Columns:** SKU · Design No · Combination ID · Item Name · Media Count · Image URL · Channel Readiness · Status

**Drill-down:** no.

### Missing Attribute Report

**Report id:** `cat-missing-attribute-report`

**Parameters:** none - it runs as soon as you pick it.

**Columns:** Product Family · SKU · Item Name · Attribute Name · Family Attribute Required · Channel · Publication Status

**Drill-down:** no.

### PIM Workbench Queue

**Report id:** `cat-pim-workbench-queue`

**Parameters:**

- **From** (`from`) - date, optional
- **To** (`to`) - date, optional
- **Status (optional)** (`status`) - select, optional, one of: ,Open,In Progress,Blocked,Done,Cancelled

**Columns:** SKU · Item Name · Product Family · Status · Assigned To · Created At · Updated At · Ageing Days

**Drill-down:** no.

### Price / Inventory Publish Status

**Report id:** `cat-price-inventory-publish-status`

**Parameters:** none - it runs as soon as you pick it.

**Columns:** SKU · Channel SKU · Channel · MRP · Available Qty · ERP Sync Time · Publication Status · Publication Error

**Drill-down:** no.

### Product Change Audit

**Report id:** `cat-product-change-audit`

**Parameters:**

- **From** (`from`) - date, optional
- **To** (`to`) - date, optional

**Columns:** Audit ID · SKU · Field Changed · Old Value · New Value · Edit Reason · Edited By · Edited At

**Drill-down:** no.

### Product Completeness Report

**Report id:** `cat-product-completeness-report`

**Parameters:**

- **From** (`from`) - date, optional
- **To** (`to`) - date, optional
- **Status (optional)** (`status`) - select, optional, one of: ,Active,Inactive,Draft

**Columns:** Product Family · SKU · Item Name · Completeness % · Missing Attribute Count · Media Count · Channel Readiness · Status

**Drill-down:** no.

### Product Family / Variant Matrix

**Report id:** `cat-product-family-variant-matrix`

**Parameters:**

- **From** (`from`) - date, optional
- **To** (`to`) - date, optional
- **Status (optional)** (`status`) - select, optional, one of: ,Active,Inactive,Draft

**Columns:** Product Family · Design No · Combination ID · SKU · Item Name · Style · Color · Polish · Size · Status

**Drill-down:** no.

### Product Master Report

**Report id:** `cat-product-master-report`

**Parameters:**

- **From** (`from`) - date, optional
- **To** (`to`) - date, optional
- **Status (optional)** (`status`) - select, optional, one of: ,Active,Inactive,Draft

**Columns:** Product Family · Item Code · Item Name · SKU · Design No · Combination ID · Category Name · Brand · Style · Color · Polish · Size · HSN Code · MRP · Status

**Drill-down:** no.

## Procurement

### GRN Corrections Log

**Report id:** `grn-corrections`

**Parameters:**

- **From** (`from`) - date, optional
- **To** (`to`) - date, optional

**Columns:** When · GRN · Line · SKU · Step · Field · Was · Now · Why · By

**Drill-down:** no.

### GRN Payments

**Report id:** `grn-payments`

**Parameters:**

- **Vendor (blank = all)** (`vendor`) - text, optional
- **Due only** (`due_only`) - select, optional, one of: ,Yes

**Columns:** GRN · Vendor · Invoice No · Invoiced *(sensitive)* · We owe *(sensitive)* · Paid *(sensitive)* · Due *(sensitive)* · Status · Last paid · Paid by

**Drill-down:** no.

### GRN Register

**Report id:** `grn-register`

**Parameters:** none - it runs as soon as you pick it.

**Columns:** GRN ID · PO Reference · Item Lines · Status · Date

**Drill-down:** no.

### GRN Register - every line, every step

**Report id:** `grn-line-register`

**Parameters:**

- **From** (`from`) - date, optional
- **To** (`to`) - date, optional
- **Vendor (blank = all)** (`vendor`) - text, optional

**Columns:** GRN · Child · Store · Vendor · Invoice No · PO · SKU · PO Qty · Received · Invoice Qty · GRN Qty · Rejected · Damaged · PO Price *(sensitive)* · Invoice Price *(sensitive)* · GRN Price *(sensitive)* · GST % · Old MRP · MRP · Batch · Step · Received · Posted

**Drill-down:** no.

### Goods Receipts Waiting

**Report id:** `grn-waiting`

**Parameters:** none - it runs as soon as you pick it.

**Columns:** GRN · Vendor · Invoice No · Waiting for · Age (days) · Invoice Total *(sensitive)* · Excess awaiting approval

**Drill-down:** no.

### Invoice vs GRN Differences

**Report id:** `grn-invoice-differences`

**Parameters:**

- **From** (`from`) - date, optional
- **To** (`to`) - date, optional
- **Vendor (blank = all)** (`vendor`) - text, optional

**Columns:** Month · Vendor · Receipts · Invoiced *(sensitive)* · Received and verified *(sensitive)* · Difference *(sensitive)* · Debit notes *(sensitive)* · Credit notes *(sensitive)* · Notes

**Drill-down:** no.

### Open Purchase Commitments

**Report id:** `po-commitments`

**Parameters:** none - it runs as soon as you pick it.

**Columns:** Age Bucket · PO Count · Committed Amount *(sensitive)*

**Drill-down:** yes - a row's **View Details** opens the transactions behind it.

### PO vs GRN Price Variance

**Report id:** `grn-po-variance`

**Parameters:**

- **From** (`from`) - date, optional
- **To** (`to`) - date, optional
- **Vendor (blank = all)** (`vendor`) - text, optional

**Columns:** Received · Vendor · PO · GRN · SKU · Qty · PO Price *(sensitive)* · GRN Price *(sensitive)* · Variance / unit *(sensitive)* · Variance % · Variance value *(sensitive)*

**Drill-down:** no.

### Purchase Price Changes

**Report id:** `grn-price-changes`

**Parameters:**

- **From** (`from`) - date, optional
- **To** (`to`) - date, optional
- **Vendor (blank = all)** (`vendor`) - text, optional

**Columns:** Received · SKU · Vendor · GRN · Previous Price *(sensitive)* · Previous Vendor · New Price *(sensitive)* · Change % · Old MRP · New MRP

**Drill-down:** no.

### RFQ Comparison Export

**Report id:** `rfq-comparison`

**Parameters:**

- **RFQ ID** (`rfq_id`) - text, **required**

**Columns:** Quote ID · Vendor · Quoted Price *(sensitive)* · Status

**Drill-down:** no.

### Rejections and Returns by Reason

**Report id:** `rejections-by-reason`

**Parameters:**

- **From** (`from`) - date, optional
- **To** (`to`) - date, optional
- **Vendor (blank = all)** (`vendor`) - text, optional

**Columns:** What · Reason code · Reason · Vendor · Documents · Qty

**Drill-down:** no.

### Vendor Ledger

**Report id:** `vendor-ledger`

**Parameters:**

- **Vendor (optional)** (`vendor_id`) - text, optional

**Columns:** PO ID · Vendor · PO Number · Total Amount *(sensitive)* · Status · Date

**Drill-down:** no.

