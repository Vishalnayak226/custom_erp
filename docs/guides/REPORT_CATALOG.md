# Report Catalog

<!-- GENERATED FILE - DO NOT EDIT BY HAND.
     Source: `engines`' report registry (`report_definitions.go`)
     Regenerate: `go run ./cmd/gendocs` -->

> **Generated 2026-08-09.** This page is produced from `engines`' report registry (`report_definitions.go`), so it cannot drift from
> the running system. Hand edits are lost on the next run - change the source instead.

Every report reachable from **Sales & Marketplace -> Reports**. There are **47**
across **12** categories.

Pick one from the catalog list on that screen, fill in any parameter marked
**required**, and run it. Where a report supports **drill-down**, clicking a
figure opens the individual transactions behind it. **Export in Background**
queues a CSV for any report large enough to time out a normal request.

Some columns are masked for roles other than Super Admin and Store Manager - those
are marked *sensitive* below, and render as `•••` rather than being dropped, so the
table has the same shape whoever runs it.

## Contents

- [Assets](#assets) - 1 reports
- [BI](#bi) - 1 reports
- [CRM](#crm) - 7 reports
- [Exceptions](#exceptions) - 3 reports
- [Finance](#finance) - 12 reports
- [HR](#hr) - 1 reports
- [Inventory](#inventory) - 2 reports
- [Manufacturing](#manufacturing) - 2 reports
- [OMS](#oms) - 9 reports
- [Procurement](#procurement) - 3 reports
- [Sales](#sales) - 3 reports
- [WMS](#wms) - 3 reports

---

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

**Columns:** Account Code · Account Name · Type · Amount *(sensitive)*

**Drill-down:** no.

### Bank Book

**Report id:** `bank-book`

**Parameters:**

- **Bank Account** (`bank_account`) - text, **required**

**Columns:** Date · Document Type · Document ID · Debit *(sensitive)* · Credit *(sensitive)* · Running Balance *(sensitive)*

**Drill-down:** no.

### Cash Book

**Report id:** `cash-book`

**Parameters:** none - it runs as soon as you pick it.

**Columns:** Date · Document Type · Document ID · Debit *(sensitive)* · Credit *(sensitive)* · Running Balance *(sensitive)*

**Drill-down:** no.

### Cash Flow Statement

**Report id:** `cash-flow-statement`

**Parameters:**

- **From** (`start`) - date, **required**
- **To** (`end`) - date, **required**

**Columns:** Activity · Cash In *(sensitive)* · Cash Out *(sensitive)* · Net Change *(sensitive)*

**Drill-down:** no.

### Cost Center P&L

**Report id:** `cost-center-pl`

**Parameters:**

- **From** (`start`) - date, **required**
- **To** (`end`) - date, **required**

**Columns:** Cost Center · Type · Amount *(sensitive)*

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

### Payables Ageing

**Report id:** `payables-ageing`

**Parameters:** none - it runs as soon as you pick it.

**Columns:** Age Bucket · PO Count · Outstanding Amount *(sensitive)*

**Drill-down:** yes - a row's **View Details** opens the transactions behind it.

### Profit & Loss

**Report id:** `profit-and-loss`

**Parameters:**

- **From** (`start`) - date, **required**
- **To** (`end`) - date, **required**

**Columns:** Account Code · Account Name · Type · Amount *(sensitive)*

**Drill-down:** no.

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

### Stock Mismatch

**Report id:** `stock-mismatch`

**Parameters:** none - it runs as soon as you pick it.

**Columns:** SKU · Location · On Hand · Available · Reserved · Safety Stock · Blocked · QC Hold · Damaged · Channel Buffer · ATS (negative = over-committed)

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


## WMS

### 3PL Storage & Handling Billing

**Report id:** `3pl-storage-billing`

**Parameters:**

- **Owner (optional)** (`owner_id`) - text, optional
- **Period Start** (`start`) - date, **required**
- **Period End** (`end`) - date, **required**

**Columns:** Owner · Location · Current Units · Days · Storage Charge *(sensitive)* · Handling Tasks · Handling Charge *(sensitive)* · Total *(sensitive)*

**Drill-down:** no.

### Labor Productivity

**Report id:** `labor-productivity`

**Parameters:**

- **From (optional)** (`start`) - date, optional
- **To (optional)** (`end`) - date, optional

**Columns:** User · Task Type · Tasks · Tasks/Hour · First Task · Last Task

**Drill-down:** no.

### Slotting / Re-Slotting Suggestions

**Report id:** `slotting-suggestions`

**Parameters:**

- **Location** (`location_code`) - text, **required**

**Columns:** SKU · Velocity Tier · Daily Velocity · Suggested Action · From Bin · To Bin · Qty · Reason

**Drill-down:** no.

