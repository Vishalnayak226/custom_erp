---
title: Report catalog
section: Reference
order: 20
summary: Every report you can run, what it asks for, what it returns and whether you can drill into it.
audience: store manager, finance, category manager, admin
last_verified: 2026-09-03
screens: [reports]
---

<!-- GENERATED ARTICLE - DO NOT EDIT BY HAND.
     Regenerate: go run ./cmd/gendocs && go run ./cmd/genkb -->

# Report catalog

**91** reports across **15** categories, all reachable from
**Sales & Marketplace » Reports**.

Pick one from the catalog list on that screen, fill in any parameter marked
**required**, and run it. Where a report supports drill-down, clicking a figure
opens the individual transactions behind it. **Export in Background** queues a
CSV for any report large enough to time out a normal request.

Columns marked *sensitive* are masked as `•••` for roles other than Super
Admin and Store Manager. They are masked rather than dropped, so the table has
the same shape whoever runs it and a screenshot from one person lines up with a
screenshot from another.

## Contents

- [Admin](#admin) - 1 reports
- [Assets](#assets) - 1 reports
- [BI](#bi) - 1 reports
- [CRM](#crm) - 7 reports
- [Exceptions](#exceptions) - 3 reports
- [Finance](#finance) - 25 reports
- [HR](#hr) - 1 reports
- [Inventory](#inventory) - 6 reports
- [Manufacturing](#manufacturing) - 3 reports
- [OMS](#oms) - 12 reports
- [PIM](#pim) - 4 reports
- [Procurement](#procurement) - 3 reports
- [Sales](#sales) - 3 reports
- [Service](#service) - 1 reports
- [WMS](#wms) - 20 reports

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

### Customer 360

**Report id:** `customer-360`

**Parameters:**

- **Customer ID** (`customer_id`) - text, **required**

**Columns:** Customer · Loyalty Balance *(sensitive)* · POS Purchases · POS Spend *(sensitive)* · Last Purchase · Invoices · Invoice Total *(sensitive)*

**Drill-down:** no.

### Customer Lifetime Value

**Report id:** `customer-lifetime-value`

**Parameters:**

- **Churn Threshold Days (default 90)** (`churn_days`) - text, optional

**Columns:** Customer · Lifetime Value *(sensitive)* · Orders · First Order · Last Order · Churned?

**Drill-down:** no.

### Loyalty Ledger Summary

**Report id:** `loyalty-summary`

**Parameters:** none - it runs as soon as you pick it.

**Columns:** Customer · Total Earned · Total Redeemed · Current Balance *(sensitive)* · Transactions

**Drill-down:** no.

### Loyalty Points Liability

**Report id:** `points-liability`

**Parameters:** none - it runs as soon as you pick it.

**Columns:** Outstanding Points · Liability Value *(sensitive)* · Customers

**Drill-down:** no.

### RFM Customer Segmentation

**Report id:** `rfm-segmentation`

**Parameters:** none - it runs as soon as you pick it.

**Columns:** Customer · Recency (days) · Frequency · Monetary *(sensitive)* · R · F · M · Segment

**Drill-down:** no.

## Exceptions

### Failed Syncs

**Report id:** `exception-failed-syncs`

**Parameters:** none - it runs as soon as you pick it.

**Columns:** Event ID · Event · Attempts · Queued

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

**Columns:** Cost Center · Type · Amount *(sensitive)*

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

**Columns:** Department · Type · Amount *(sensitive)*

**Drill-down:** yes - a row's **View Details** opens the transactions behind it.

### Dunning Queue

**Report id:** `dunning-queue`

**Parameters:** none - it runs as soon as you pick it.

**Columns:** Invoice · Customer · Due Date · Days Overdue · Amount *(sensitive)* · Tier · Last Notified

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

### GST Return Summary

**Report id:** `gst-return-summary`

**Parameters:**

- **From** (`start`) - date, **required**
- **To** (`end`) - date, **required**

**Columns:** From · To · Taxable Value *(sensitive)* · Output CGST *(sensitive)* · Output SGST *(sensitive)* · Output IGST *(sensitive)* · Total Tax Liability *(sensitive)* · Exempt Value *(sensitive)* · Nil-Rated Value *(sensitive)* · Zero-Rated Value *(sensitive)* · Total Non-Taxable *(sensitive)* · Transactions

**Drill-down:** yes - a row's **View Details** opens the transactions behind it.

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

### Payables Ageing

**Report id:** `payables-ageing`

**Parameters:** none - it runs as soon as you pick it.

**Columns:** Age Bucket · PO Count · Outstanding Amount *(sensitive)*

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

**Columns:** Posting ID · Account Code · Account Name · Type · Document Type · Document ID · Debit *(sensitive)* · Credit *(sensitive)* · Date

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

## HR

### Attendance Summary

**Report id:** `attendance-summary`

**Parameters:**

- **From (optional)** (`start`) - date, optional
- **To (optional)** (`end`) - date, optional

**Columns:** Employee ID · Employee · Department · Present · Absent · Late · Leave · Holiday · Weekly Off · Total Marked Days · Attendance %

**Drill-down:** no.

## Inventory

### Current Stock

**Report id:** `current-stock`

**Parameters:** none - it runs as soon as you pick it.

**Columns:** SKU · Location · On Hand · Available · Committed · Reserved · Safety Stock

**Drill-down:** no.

### Demand Forecast (Trend)

**Report id:** `demand-forecast`

**Parameters:**

- **SKU** (`sku`) - text, **required**
- **Location** (`location_code`) - text, **required**
- **Forecast Days** (`forecast_days`) - text, **required**

**Columns:** SKU · Location · Forecasted Qty

**Drill-down:** no.

### Inventory Valuation

**Report id:** `inventory-valuation`

**Parameters:** none - it runs as soon as you pick it.

**Columns:** SKU · Qty On Hand · Unit Cost (Avg) *(sensitive)* · Total Value *(sensitive)* · Costed

**Drill-down:** no.

### Pegged Demand & Supply

**Report id:** `pegged-demand-supply`

**Parameters:**

- **SKU** (`sku`) - text, **required**
- **Location (for on-hand supply)** (`location_code`) - text, optional

**Columns:** Kind · Type · Reference · Qty · Status

**Drill-down:** no.

### Reorder Suggestions (Configured)

**Report id:** `reorder-suggestions`

**Parameters:**

- **Location** (`location_code`) - text, **required**
- **Default Lead Time (days)** (`default_lead_time_days`) - text, **required**
- **Default Safety Stock** (`default_safety_stock`) - text, **required**

**Columns:** SKU · Location · Available · Daily Velocity · Reorder Point · Suggested Qty · Safety Stock · Lead Time (days)

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

## Manufacturing

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

### Production Order Status

**Report id:** `production-order-status`

**Parameters:** none - it runs as soon as you pick it.

**Columns:** Order ID · BOM · Finished Item · Qty · Status · Date

**Drill-down:** no.

## OMS

### Allocation Pending

**Report id:** `allocation-pending`

**Parameters:** none - it runs as soon as you pick it.

**Columns:** Order · Customer · Channel · Total Amount *(sensitive)* · Created At

**Drill-down:** no.

### Courier Performance

**Report id:** `courier-performance`

**Parameters:** none - it runs as soon as you pick it.

**Columns:** Carrier · Total Shipments · Delivered · RTO · RTO Rate % · Avg Handover->Delivered (hrs)

**Drill-down:** no.

### OMS Exception Queue

**Report id:** `oms-exception-queue`

**Parameters:** none - it runs as soon as you pick it.

**Columns:** Event ID · Event · Status · Attempts · Queued At · Last Error

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

### Orphaned Channel Orders (pre-35.1 intake)

**Report id:** `orphaned-channel-orders`

**Parameters:** none - it runs as soon as you pick it.

**Columns:** Mapping Table · Channel · Channel Order ID · Legacy Order ID · Last Written

**Drill-down:** no.

### Reserved Stock

**Report id:** `reserved-stock`

**Parameters:** none - it runs as soon as you pick it.

**Columns:** SKU · Location · Reserved · Committed · On Hand · Available

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

## Procurement

### GRN Register

**Report id:** `grn-register`

**Parameters:** none - it runs as soon as you pick it.

**Columns:** GRN ID · PO Reference · Item Lines · Status · Date

**Drill-down:** no.

### RFQ Comparison Export

**Report id:** `rfq-comparison`

**Parameters:**

- **RFQ ID** (`rfq_id`) - text, **required**

**Columns:** Quote ID · Vendor · Quoted Price *(sensitive)* · Status

**Drill-down:** no.

### Vendor Ledger

**Report id:** `vendor-ledger`

**Parameters:**

- **Vendor (optional)** (`vendor_id`) - text, optional

**Columns:** PO ID · Vendor · PO Number · Total Amount *(sensitive)* · Status · Date

**Drill-down:** no.

## Sales

### Competitor Price Gap

**Report id:** `competitor-price-gap`

**Parameters:**

- **Platform (optional)** (`platform`) - select, optional, one of: Amazon,Flipkart,Myntra,Meesho,Ajio,Nykaa,Tata Cliq,JioMart,eBay,Own Website,Other
- **Our SKU (optional)** (`sku`) - text, optional
- **Observed on or after (optional)** (`since`) - date, optional
- **We are (optional)** (`position`) - select, optional, one of: Above,At,Below,No price on file

**Columns:** Our SKU · Item · Our Price · Our Price From · Best Competitor Price · Platform · Gap (₹) · Gap (%) · We Are · Observed On · Observations · Source

**Drill-down:** no.

### Customer Ledger

**Report id:** `customer-ledger`

**Parameters:**

- **Customer (optional)** (`customer`) - text, optional

**Columns:** Invoice ID · Customer · Invoice Number · Total Amount *(sensitive)* · Status · Date

**Drill-down:** no.

### Sales Register

**Report id:** `sales-register`

**Parameters:** none - it runs as soon as you pick it.

**Columns:** Cart Number · Location · Payment Mode · Status · Sale Total *(sensitive)* · Date · Data Issue

**Drill-down:** no.

## Service

### Service SLA Breaches

**Report id:** `service-sla-breaches`

**Parameters:** none - it runs as soon as you pick it.

**Columns:** Ticket · Customer · Status · Breach Type · Respond By · Resolve By

**Drill-down:** no.

## WMS

### 3PL Storage & Handling Billing

**Report id:** `3pl-storage-billing`

**Parameters:**

- **Owner (optional)** (`owner_id`) - text, optional
- **Period Start** (`start`) - date, **required**
- **Period End** (`end`) - date, **required**

**Columns:** Owner · Location · Current Units (EA) · Days · Billing UOM · Billing Units · Storage Charge *(sensitive)* · Handling Tasks · Handling Charge *(sensitive)* · Total *(sensitive)*

**Drill-down:** no.

### 3PL Storage Billing v2

**Report id:** `storage-billing-v2`

**Parameters:**

- **Owner (optional)** (`owner_id`) - text, optional
- **Period Start** (`start`) - date, **required**
- **Period End** (`end`) - date, **required**

**Columns:** Rate · Owner · Location · Item · Snapshot Days · Average Units · Net *(sensitive)* · Tax *(sensitive)* · Total *(sensitive)*

**Drill-down:** no.

### Batch Movement History (Recall)

**Report id:** `batch-movement-history`

**Parameters:**

- **Batch / Lot** (`batch_no`) - text, **required**
- **Item Code (optional)** (`sku`) - text, optional

**Columns:** When · Item · Batch / Lot · Qty · Movement · Document · Location · From Bin · To Bin · By

**Drill-down:** no.

### Batch Near-Expiry Watchlist

**Report id:** `batch-near-expiry`

**Parameters:**

- **Within (days, default 30)** (`within_days`) - text, optional

**Columns:** Item · Batch / Lot · Expires · Days Left · Batch Status · Qty On Hand · Locations

**Drill-down:** no.

### Batch Stock Inquiry

**Report id:** `batch-stock-inquiry`

**Parameters:**

- **Item Code (optional)** (`sku`) - text, optional
- **Location (optional)** (`location`) - text, optional
- **Batch / Lot (optional)** (`batch_no`) - text, optional

**Columns:** Item · Batch / Lot · Location · Bin · Condition · Qty · Expires · Days Left · Batch Status

**Drill-down:** no.

### Clean-Location Consolidation Suggestions

**Report id:** `consolidation-suggestions`

**Parameters:**

- **Location** (`location_code`) - text, **required**

**Columns:** SKU · Action · From Bin · To Bin · Qty · Reason

**Drill-down:** no.

### Cross-Facility Inventory Inquiry

**Report id:** `cross-facility-stock`

**Parameters:**

- **SKU** (`sku`) - text, **required**

**Columns:** SKU · Location · On Hand · Available · In Transit

**Drill-down:** no.

### Facility Roll-Up Inventory Inquiry

**Report id:** `facility-rollup`

**Parameters:**

- **Facility (Location code)** (`facility_code`) - text, **required**

**Columns:** SKU · On Hand · Available · In Transit · Locations

**Drill-down:** no.

### Labor Productivity

**Report id:** `labor-productivity`

**Parameters:**

- **From (optional)** (`start`) - date, optional
- **To (optional)** (`end`) - date, optional

**Columns:** User · Task Type · Tasks · Tasks/Hour · First Task · Last Task

**Drill-down:** no.

### Labour Cost

**Report id:** `labor-cost`

**Parameters:**

- **From (optional)** (`start`) - date, optional
- **To (optional)** (`end`) - date, optional

**Columns:** Location · Tasks · Quantity · Standard Hours · Labour Cost *(sensitive)*

**Drill-down:** no.

### Labour Enterprise Productivity

**Report id:** `labor-enterprise-productivity`

**Parameters:**

- **From (optional)** (`start`) - date, optional
- **To (optional)** (`end`) - date, optional

**Columns:** Scope · Tasks · Quantity · Standard Hours · Labour Cost *(sensitive)*

**Drill-down:** no.

### Labour Facility Performance

**Report id:** `labor-facility-performance`

**Parameters:**

- **From (optional)** (`start`) - date, optional
- **To (optional)** (`end`) - date, optional

**Columns:** Location · Tasks · Quantity · Standard Hours · Labour Cost *(sensitive)*

**Drill-down:** no.

### Labour Standards Audit

**Report id:** `labor-standards-audit`

**Parameters:** none - it runs as soon as you pick it.

**Columns:** Standard · Operation · Element Seconds · Travel Seconds · Allowance Seconds · Total Seconds

**Drill-down:** no.

### Labour Task Productivity

**Report id:** `labor-task-productivity`

**Parameters:**

- **From (optional)** (`start`) - date, optional
- **To (optional)** (`end`) - date, optional

**Columns:** Task Type · Tasks · Quantity · Standard Hours · Labour Cost *(sensitive)*

**Drill-down:** no.

### Labour User Performance

**Report id:** `labor-user-performance`

**Parameters:**

- **From (optional)** (`start`) - date, optional
- **To (optional)** (`end`) - date, optional

**Columns:** User · Tasks · Quantity · Standard Hours · Labour Cost *(sensitive)*

**Drill-down:** no.

### Owner Stock Inquiry (3PL)

**Report id:** `owner-stock-inquiry`

**Parameters:**

- **Item Code (optional)** (`sku`) - text, optional
- **Location (optional)** (`location`) - text, optional
- **Owner (optional)** (`owner_id`) - text, optional

**Columns:** Owner · Item · Bin · Location · Condition · Qty

**Drill-down:** no.

### Serial Movement History

**Report id:** `serial-movement-history`

**Parameters:**

- **Serial Number** (`serial_no`) - text, **required**
- **Item Code (optional)** (`sku`) - text, optional

**Columns:** When · Item · Serial Number · Movement · Document · Location · From · To · By

**Drill-down:** no.

### Serial Number Inquiry

**Report id:** `serial-inquiry`

**Parameters:**

- **Item Code (optional)** (`sku`) - text, optional
- **Serial Number (optional)** (`serial_no`) - text, optional
- **Status (optional)** (`status`) - select, optional, one of: InStock,Allocated,Shipped,Returned,Scrapped

**Columns:** Item · Serial Number · Batch / Lot · Status · Current Bin · Location · Reserved For · Supplier

**Drill-down:** no.

### Slotting / Re-Slotting Suggestions

**Report id:** `slotting-suggestions`

**Parameters:**

- **Location** (`location_code`) - text, **required**

**Columns:** SKU · Velocity Tier · Daily Velocity · Suggested Action · From Bin · To Bin · Qty · Reason

**Drill-down:** no.

### Unslotting Suggestions (discontinued items)

**Report id:** `unslotting-suggestions`

**Parameters:**

- **Location** (`location_code`) - text, **required**

**Columns:** SKU · Action · From Bin · To Bin · Qty · Reason

**Drill-down:** no.

