# Report Catalog

<!-- GENERATED FILE - DO NOT EDIT BY HAND.
     Source: `engines`' report registry (`report_definitions.go`)
     Regenerate: `pwsh docs/update-docs.ps1 -Group Content` -->

> **Generated 2026-09-10.** This page is produced from `engines`' report registry (`report_definitions.go`), for this source revision.
> It is not release or tenant assurance. Hand edits are lost on the next run - change the source instead.

Every report reachable from **Sales & Marketplace -> Reports**. There are **430**
across **26** categories.

Pick one from the catalog list on that screen, fill in any parameter marked
**required**, and run it. Where a report supports **drill-down**, clicking a
figure opens the individual transactions behind it. **Export in Background**
queues a CSV for any report large enough to time out a normal request.

Some columns are masked for roles other than Super Admin and Store Manager - those
are marked *sensitive* below, and render as `•••` rather than being dropped, so the
table has the same shape whoever runs it.

## Contents

- [Accounting](#accounting) - 26 reports
- [Admin](#admin) - 7 reports
- [Assets](#assets) - 1 reports
- [BI](#bi) - 1 reports
- [CRM](#crm) - 36 reports
- [E-commerce & Integration](#e-commerce--integration) - 12 reports
- [Exceptions](#exceptions) - 6 reports
- [Expense Management](#expense-management) - 11 reports
- [Finance](#finance) - 39 reports
- [GST & Compliance](#gst--compliance) - 8 reports
- [HR](#hr) - 2 reports
- [HRMS](#hrms) - 15 reports
- [Inventory](#inventory) - 42 reports
- [MIS & Management](#mis--management) - 10 reports
- [Manufacturing](#manufacturing) - 21 reports
- [Master Data & Audit](#master-data--audit) - 11 reports
- [OMS](#oms) - 25 reports
- [PIM](#pim) - 4 reports
- [PIM / PIMS](#pim--pims) - 16 reports
- [Procurement](#procurement) - 18 reports
- [Procurement & GRN](#procurement--grn) - 27 reports
- [Sales](#sales) - 11 reports
- [Sales & POS](#sales--pos) - 22 reports
- [Service](#service) - 1 reports
- [Stock Transfer](#stock-transfer) - 9 reports
- [WMS](#wms) - 49 reports

---

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

### Duplicate items

**Report id:** `duplicate-items`

**Parameters:** none - it runs as soon as you pick it.

**Columns:** Group · Code · Name · Why it matches · Proposal · Kept because · Transactions · Filled fields · Created

**Drill-down:** no.

### Duplicate suppliers

**Report id:** `duplicate-suppliers`

**Parameters:** none - it runs as soon as you pick it.

**Columns:** Group · Code · Name · Why it matches · Proposal · Kept because · Transactions · Filled fields · Created

**Drill-down:** no.

### Failed prints and fallbacks

**Report id:** `print-failures`

**Parameters:**

- **From** (`from`) - date, optional
- **To** (`to`) - date, optional

**Columns:** When · Print · Document · Printer · Outcome · Detail · By

**Drill-down:** no.

### Knowledge Center Feedback

**Report id:** `help-article-feedback`

**Parameters:** none - it runs as soon as you pick it.

**Columns:** Article Slug · Marked Helpful · Marked Not Helpful · Total Responses · Helpful % · Last Feedback

**Drill-down:** no.

### Missing regulatory print data

**Report id:** `missing-regulatory-print-data`

**Parameters:** none - it runs as soon as you pick it.

**Columns:** Record · Code · Name · Missing · Why it matters

**Drill-down:** no.

### Print and reprint audit

**Report id:** `print-reprint-audit`

**Parameters:**

- **From** (`from`) - date, optional
- **To** (`to`) - date, optional

**Columns:** Document · Record · Prints · Reprints · Last reprint reason · Last by · Approved reprints · Anomaly

**Drill-down:** no.

### Print template coverage

**Report id:** `print-template-coverage`

**Parameters:** none - it runs as soon as you pick it.

**Columns:** Print · Record · Templates · Default template · Profiles · Copies · Printed (30 days) · Changes awaiting approval

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

### CRM Customer Summary

**Report id:** `crm-customer-summary`

**Parameters:** none - it runs as soon as you pick it.

**Columns:** Lifecycle · Customers · Spend (net of returns) *(sensitive)* · Average spend *(sensitive)* · Offers by SMS · Offers by WhatsApp · Offers by email

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

### Campaign Summary

**Report id:** `crm-campaign-summary`

**Parameters:**

- **From** (`from_date`) - date, optional
- **To** (`to_date`) - date, optional

**Columns:** Campaign · Run · Channel · Started · Status · Matched · Hold-out · Not sent · Sent · Delivered · Failed · Bills credited · Sales credited *(sensitive)* · Cost *(sensitive)*

**Drill-down:** no.

### Clienteling Tasks

**Report id:** `crm-clienteling`

**Parameters:**

- **From** (`from_date`) - date, optional
- **To** (`to_date`) - date, optional

**Columns:** Store · Owner · Kind · Open · Overdue · Done · Appointments

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

### Consent Audit

**Report id:** `crm-consent-audit`

**Parameters:**

- **From** (`from_date`) - date, optional
- **To** (`to_date`) - date, optional

**Columns:** Recorded · Customer · Name · Channel · Purpose · Action · Source · Recorded by · Notice · Evidence

**Drill-down:** no.

### Coupon / Voucher Usage Report

**Report id:** `cat-coupon-voucher-usage-report`

**Parameters:**

- **From** (`from`) - date, optional
- **To** (`to`) - date, optional
- **Store** (`location`) - text, optional

**Columns:** Document Date · Customer ID · Customer Name · Coupon Code · Campaign · Sales Invoice No · Discount Amount · Net Amount · Status

**Drill-down:** no.

### Coupon Use

**Report id:** `crm-coupon-use`

**Parameters:**

- **From** (`from_date`) - date, optional
- **To** (`to_date`) - date, optional

**Columns:** Issued · Code · Customer · Offer · From · Expires · Used · Status

**Drill-down:** no.

### Credited Sales

**Report id:** `crm-credited-sales`

**Parameters:**

- **From** (`from_date`) - date, optional
- **To** (`to_date`) - date, optional

**Columns:** Bill date · Bill · Store · Customer · Credited to · Message sent · Amount *(sensitive)*

**Drill-down:** no.

### Customer 360

**Report id:** `customer-360`

**Parameters:**

- **Customer** (`customer_id`) - link, **required**, one of: Customer

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

### Customer affinity and next best offer

**Report id:** `customer-affinity`

**Parameters:** none - it runs as soon as you pick it.

**Columns:** Customer · Name · Bills · Spend *(sensitive)* · Favourite category · Share of spend % · Last bill · Next best offer · Offer item

**Drill-down:** no.

### Customer pulse

**Report id:** `crm-pulse`

**Parameters:** none - it runs as soon as you pick it.

**Columns:** Measure · Value · How it is counted

**Drill-down:** no.

### Customers Sharing a Mobile Number

**Report id:** `customer-duplicate-phones`

**Parameters:** none - it runs as soon as you pick it.

**Columns:** Mobile *(sensitive)* · Customers · Customer codes and names

**Drill-down:** no.

### Feedback and NPS

**Report id:** `crm-feedback`

**Parameters:**

- **From** (`from_date`) - date, optional
- **To** (`to_date`) - date, optional

**Columns:** Store · Asked · Answers · Promoters · Detractors · NPS · Tickets opened

**Drill-down:** no.

### Journey Performance

**Report id:** `crm-journey-performance`

**Parameters:**

- **From** (`from_date`) - date, optional
- **To** (`to_date`) - date, optional

**Columns:** Journey · On · Entered · In it now · Finished · Reached the goal · Messages sent

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

### Message Cost

**Report id:** `crm-message-cost`

**Parameters:**

- **From** (`from_date`) - date, optional
- **To** (`to_date`) - date, optional

**Columns:** Month · Channel · Messages · SMS parts · Estimated *(sensitive)* · Charged (reported) *(sensitive)*

**Drill-down:** no.

### Message Detail

**Report id:** `crm-message-detail`

**Parameters:**

- **From** (`from_date`) - date, optional
- **To** (`to_date`) - date, optional
- **Status** (`status`) - select, optional, one of: ,Queued,Sending,Accepted,Delivered,Read,Failed,Unknown,Suppressed,Expired,Cancelled,HeldOut,Simulated

**Columns:** Queued · From · Customer · Name · Channel · Purpose · Status · Reason · To · SMS parts · Cost *(sensitive)*

**Drill-down:** no.

### RFM Customer Segmentation

**Report id:** `rfm-segmentation`

**Parameters:** none - it runs as soon as you pick it.

**Columns:** Customer · Recency (days) · Frequency · Monetary *(sensitive)* · R · F · M · Segment

**Drill-down:** no.

### RFM and Lifecycle

**Report id:** `crm-rfm-lifecycle`

**Parameters:**

- **Lifecycle** (`lifecycle`) - select, optional, one of: ,New,Active repeat,Reactivated,At risk,Lapsed,Never bought

**Columns:** Customer · Name · Lifecycle · RFM · Bills · Spend *(sensitive)* · Days since last bill

**Drill-down:** no.

### Repeat Purchase Report

**Report id:** `cat-repeat-purchase-report`

**Parameters:** none - it runs as soon as you pick it.

**Columns:** Customer ID · Customer Name · Customer Mobile · Bill Count · Net Sales Amount · Last Sale Date · Segment

**Drill-down:** no.

### Sales Pipeline

**Report id:** `crm-pipeline`

**Parameters:**

- **Deals** (`status`) - select, optional, one of: Open,Won,Lost,All

**Columns:** Deal · Customer · Owner · Stage · Value *(sensitive)* · Chance % · Weighted *(sensitive)* · Expected close · Status · Sales order

**Drill-down:** no.

### Segment Members

**Report id:** `crm-segment-members`

**Parameters:**

- **Segment** (`segment_id`) - segment, **required**

**Columns:** Customer · Name · Phone · Email · Lifecycle · RFM · Bills · Spend *(sensitive)* · Last bill

**Drill-down:** no.

### Service SLA

**Report id:** `crm-service-sla`

**Parameters:**

- **From** (`from_date`) - date, optional
- **To** (`to_date`) - date, optional

**Columns:** Ticket · Opened · Customer · Store · State · Respond by · Response late · Resolve by · Resolution late

**Drill-down:** no.

### Suggested segments

**Report id:** `suggested-segments`

**Parameters:** none - it runs as soon as you pick it.

**Columns:** Segment · Customers today · Build it with · Why

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

### Customer receipts

**Report id:** `customer-receipts`

**Parameters:**

- **From** (`from`) - date, optional
- **To** (`to`) - date, optional
- **Customer** (`customer`) - text, optional

**Columns:** Date · Receipt · Customer · Store · Mode · Reference · Amount · TDS · To invoices · Invoices · On account · Advance no.

**Drill-down:** no.

### Day Book

**Report id:** `day-book`

**Parameters:**

- **From** (`from`) - date, optional
- **To** (`to`) - date, optional

**Columns:** Date · Voucher type · Voucher · Account · Account name · Debit *(sensitive)* · Credit *(sensitive)* · By

**Drill-down:** no.

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

**Columns:** Card · Card ID · Profile · Status · Loaded · Used / cancelled · Outstanding · Expires · Store issued

**Drill-down:** no.

### IRN Portal Manual Upload Report

**Report id:** `einvoice-manual-upload`

**Parameters:**

- **From** (`from`) - date, optional
- **To** (`to`) - date, optional
- **GSTIN** (`gstin`) - text, optional

**Columns:** Date · Type · Number · Seller GSTIN · IRN state · Why · IRN (when entered)

**Drill-down:** no.

### Input credit past its deadline

**Report id:** `itc-past-deadline`

**Parameters:** none - it runs as soon as you pick it.

**Columns:** Document · Type · Vendor · Invoice date · Deadline

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

### Receivables vs Ledger (by invoice)

**Report id:** `receivables-vs-ledger`

**Parameters:**

- **Include rows that agree** (`all`) - select, optional, one of: No,Yes

**Columns:** Source · Document · Customer · Status · Owed per invoice *(sensitive)* · Owed per ledger (1300) *(sensitive)* · Difference *(sensitive)* · Ledger postings · Owner · What it suggests

**Drill-down:** no.

### Refunds by method

**Report id:** `refund-methods`

**Parameters:**

- **From** (`from`) - date, optional
- **To** (`to`) - date, optional

**Columns:** Refund method · Refunds · Amount

**Drill-down:** no.

### Statutory Audit Export (Full GL)

**Report id:** `statutory-gl-export`

**Parameters:**

- **Period Start** (`start`) - date, **required**
- **Period End** (`end`) - date, **required**

**Columns:** Posting ID · Account Code · Account Name · Type · Document Type · Document ID · Debit *(sensitive)* · Credit *(sensitive)* · Date · Posted by · Source command

**Drill-down:** no.

### Store credit by customer

**Report id:** `store-credit-liability`

**Parameters:** none - it runs as soon as you pick it.

**Columns:** Customer · Credited · Used · Balance · Store Credit Liability (GL)

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

### Consumption by cost centre

**Report id:** `consumption-by-cost-centre`

**Parameters:**

- **From** (`from`) - date, optional
- **To** (`to`) - date, optional

**Columns:** Cost centre · Department · Issued · Returned · Net consumption

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

### Delivery challan register

**Report id:** `delivery-challan-register`

**Parameters:**

- **From** (`from`) - date, optional
- **To** (`to`) - date, optional

**Columns:** Date · Challan · Purpose · Store · Party · Item · Lot · Issued · Back good · Back damaged · Back rejected · Written off · Still out · Expected back · Status · E-way bill

**Drill-down:** no.

### Demand Forecast (Trend)

**Report id:** `demand-forecast`

**Parameters:**

- **SKU** (`sku`) - text, **required**
- **Location** (`location_code`) - text, **required**
- **Forecast Days** (`forecast_days`) - text, **required**

**Columns:** SKU · Location · Forecasted Qty

**Drill-down:** no.

### E-way bill register

**Report id:** `ewaybill-register`

**Parameters:**

- **From** (`from`) - date, optional
- **To** (`to`) - date, optional

**Columns:** Date · Document · Type · From · To · Value · E-way bill · EWB no · Valid until · Vehicle · Why

**Drill-down:** no.

### Goods In Receiving

**Report id:** `grn-in-receiving`

**Parameters:**

- **Store code (blank = all)** (`store`) - text, optional

**Columns:** Store · SKU · In receiving · Receipts · Received since

**Drill-down:** no.

### Goods out on challan, by party

**Report id:** `challan-outstanding-returnables`

**Parameters:** none - it runs as soon as you pick it.

**Columns:** Party · Challans · Still out · Value · Not due · Overdue up to 30 days · 31-60 days · Over 60 days

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

### Material issue and return register

**Report id:** `material-issue-register`

**Parameters:**

- **From** (`from`) - date, optional
- **To** (`to`) - date, optional

**Columns:** Date · Document · Type · Store · Department · Cost centre · Item · Qty · Value · Status

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

**Columns:** Item · Location · On hand (book) · Ledger total · Difference · Ledger lines · In own locations · Set aside (book) · What it suggests

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

**Report id:** `cat-stock-ledger`

**Parameters:**

- **From** (`from`) - date, optional
- **To** (`to`) - date, optional
- **Store** (`location`) - text, optional

**Columns:** Movement Date · Store Code · Store Name · Location Code · Location Name · Item Code · Item Name · SKU · Barcode · Design No · Combination ID · Category Name · Movement Type · Qty · Source Document Type · Source Document No · Stock Status · Created By

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

### Stock adjustment register

**Report id:** `inventory-adjustment-register`

**Parameters:**

- **From** (`from`) - date, optional
- **To** (`to`) - date, optional

**Columns:** Date · Adjustment · Store · Item · Qty · Before · After · Reason · Value · Status · Flag

**Drill-down:** no.

### Stock by Location

**Report id:** `stock-by-location`

**Parameters:**

- **Store code (blank = all)** (`store`) - text, optional

**Columns:** Store · SKU · Main · Inward · In-Transit · QC Hold · Damage · On Hold · Purchase Return · Own locations · Sellable

**Drill-down:** no.

### Stock losses by reason and store

**Report id:** `inventory-loss-by-reason`

**Parameters:**

- **From** (`from`) - date, optional
- **To** (`to`) - date, optional

**Columns:** Store · Reason · Lines · Units lost · Value

**Drill-down:** no.

### Transfer shortages by reason

**Report id:** `transfer-shortage-by-reason`

**Parameters:**

- **From** (`from`) - date, optional
- **To** (`to`) - date, optional

**Columns:** Reason · Transfers · Damaged · Lost · Returned to source · Excess

**Drill-down:** no.

### Transfers in transit too long

**Report id:** `transfers-in-transit-long`

**Parameters:** none - it runs as soon as you pick it.

**Columns:** Transfer · From · To · Dispatched · Days in transit

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

### Job work cost, yield and wastage

**Report id:** `job-work-yield`

**Parameters:** none - it runs as soon as you pick it.

**Columns:** Order · Job worker · Sent · Came back as · Consumed · Output accepted · Output rejected · Yield % (of expected) · Scrap / loss % · Material value · Processing charge · Cost per output

**Drill-down:** no.

### Job work outstanding (qty and weight)

**Report id:** `job-work-outstanding`

**Parameters:** none - it runs as soon as you pick it.

**Columns:** Order · Job worker · Line · Item · Sent · Accepted · Rejected · Scrap / loss · Loss awaiting approval · Still out · Net weight out · Fine weight out · Sent · Due back · Days overdue · Statutory return by

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

### Goods receipt discrepancies by vendor

**Report id:** `grn-discrepancy-by-vendor`

**Parameters:**

- **From** (`from`) - date, optional
- **To** (`to`) - date, optional

**Columns:** Vendor · Receipts · Received · Accepted · Rejected · Damaged · Short of the invoice · Invoice difference · Rejected %

**Drill-down:** no.

### Goods receipts not billed

**Report id:** `grn-not-billed`

**Parameters:** none - it runs as soon as you pick it.

**Columns:** Goods receipt · Vendor · Posted · Days · Vendor invoice no.

**Drill-down:** no.

### Goods received not invoiced (GRNI)

**Report id:** `grni`

**Parameters:**

- **From** (`from`) - date, optional
- **To** (`to`) - date, optional

**Columns:** Goods receipt · Date · Vendor · Store · Accepted value · Invoiced · Not invoiced · Price difference · Vendor invoices · AP hold

**Drill-down:** no.

### Invoice vs GRN Differences

**Report id:** `grn-invoice-differences`

**Parameters:**

- **From** (`from`) - date, optional
- **To** (`to`) - date, optional
- **Vendor (blank = all)** (`vendor`) - text, optional

**Columns:** Month · Vendor · Receipts · Invoiced *(sensitive)* · Received and verified *(sensitive)* · Difference *(sensitive)* · Debit notes *(sensitive)* · Credit notes *(sensitive)* · Notes

**Drill-down:** no.

### Late and early receipts

**Report id:** `grn-receipt-timing`

**Parameters:** none - it runs as soon as you pick it.

**Columns:** Goods receipt · PO · Vendor · Expected · Received · Timing

**Drill-down:** no.

### Open Purchase Commitments

**Report id:** `po-commitments`

**Parameters:** none - it runs as soon as you pick it.

**Columns:** Age Bucket · PO Count · Committed Amount *(sensitive)*

**Drill-down:** yes - a row's **View Details** opens the transactions behind it.

### PO expected delivery and terms

**Report id:** `po-delivery-and-terms`

**Parameters:**

- **From** (`from`) - date, optional
- **To** (`to`) - date, optional

**Columns:** PO · Vendor · Store · Status · Expected delivery · Days late · Payment term · Delivery terms · Freight paid by · Quote valid until · Amendments · Value

**Drill-down:** no.

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

### Vendor document and transport register

**Report id:** `vendor-document-transport-register`

**Parameters:**

- **From** (`from`) - date, optional
- **To** (`to`) - date, optional

**Columns:** Goods receipt · Received · Vendor · Store · Invoice no · Invoice date · Challan no · Challan date · LR / AWB · Vehicle · Transporter · Packages

**Drill-down:** no.


## Procurement & GRN

### Cancelled PO Register

**Report id:** `cat-cancelled-po-register`

**Parameters:**

- **From** (`from`) - date, optional
- **To** (`to`) - date, optional
- **Store** (`location`) - text, optional
- **Status (optional)** (`status`) - select, optional, one of: ,Draft,Pending Approval,Approved,Partially Received,Received,Closed,Rejected,Cancelled

**Columns:** PO Date · PO No · Vendor Name · PO Gross Value · Cancelled Qty · Reason · User Name · Approved By · Status

**Drill-down:** no.

### Expired PO Report

**Report id:** `cat-expired-po-report`

**Parameters:**

- **From** (`from`) - date, optional
- **To** (`to`) - date, optional
- **Store** (`location`) - text, optional
- **Status (optional)** (`status`) - select, optional, one of: ,Draft,Pending Approval,Approved,Partially Received,Received,Closed,Rejected,Cancelled

**Columns:** PO Date · PO No · Vendor Name · Store Name · PO Qty · Received Qty · Pending Qty · PO Status · Remarks

**Drill-down:** no.

### GRN Barcode Generation Register

**Report id:** `cat-grn-barcode-generation-register`

**Parameters:**

- **From** (`from`) - date, optional
- **To** (`to`) - date, optional
- **Store** (`location`) - text, optional

**Columns:** GRN Date · GRN No · PO No · Vendor Name · Barcode · SKU · Design No · Combination ID · MRP · Purchase Price *(sensitive)* · Created By

**Drill-down:** no.

### GRN Cancelled Register

**Report id:** `cat-grn-cancelled-register`

**Parameters:**

- **From** (`from`) - date, optional
- **To** (`to`) - date, optional
- **Store** (`location`) - text, optional
- **Status (optional)** (`status`) - select, optional, one of: ,Draft,Pending Approval,Received,Approved,Posted,Cancelled

**Columns:** GRN Date · GRN No · PO No · Vendor Name · Received Qty · Reason · User ID · User Name · Approved By · Status

**Drill-down:** no.

### GRN Itemwise Detail

**Report id:** `cat-grn-itemwise-detail`

**Parameters:**

- **From** (`from`) - date, optional
- **To** (`to`) - date, optional
- **Store** (`location`) - text, optional
- **Status (optional)** (`status`) - select, optional, one of: ,Draft,Pending Approval,Received,Approved,Posted,Cancelled

**Columns:** GRN Date · GRN No · PO No · Vendor Code · Vendor Name · Vendor Invoice No · Item Code · Item Name · SKU · Barcode · Design No · Combination ID · Category Name · Received Qty · Accepted Qty · Rejected Qty · PO Rate · Invoice Rate · Silver Rate / Gram *(sensitive)* · Making Charge / Gram *(sensitive)* · Silver + Making Cost / Gram *(sensitive)* · Purchase Price *(sensitive)* · MRP · User Name · GRN Status

**Drill-down:** no.

### GRN PO / Invoice Variance

**Report id:** `cat-grn-po-invoice-variance`

**Parameters:**

- **From** (`from`) - date, optional
- **To** (`to`) - date, optional
- **Store** (`location`) - text, optional
- **Status (optional)** (`status`) - select, optional, one of: ,Draft,Pending Approval,Received,Approved,Posted,Cancelled

**Columns:** GRN Date · GRN No · PO No · Vendor Invoice No · Vendor Name · Item Code · Item Name · PO Qty · Received Qty · Invoice Qty · PO Rate · Invoice Rate · Rate Variance · Amount Variance · Debit Note No · Credit Note No (Vendor)

**Drill-down:** no.

### GRN QC / Rejection Exception

**Report id:** `cat-grn-qc-rejection-exception`

**Parameters:**

- **From** (`from`) - date, optional
- **To** (`to`) - date, optional
- **Store** (`location`) - text, optional
- **Status (optional)** (`status`) - select, optional, one of: ,Draft,Pending Approval,Received,Approved,Posted,Cancelled

**Columns:** GRN Date · GRN No · PO No · Vendor Name · Item Code · Item Name · SKU · Barcode · Design No · Combination ID · Category Name · Received Qty · Accepted Qty · Rejected Qty · Reason Code · Reason · Status · Created By

**Drill-down:** no.

### GRN Register

**Report id:** `cat-grn-register`

**Parameters:**

- **From** (`from`) - date, optional
- **To** (`to`) - date, optional
- **Store** (`location`) - text, optional
- **Status (optional)** (`status`) - select, optional, one of: ,Draft,Pending Approval,Received,Approved,Posted,Cancelled

**Columns:** GRN Date · GRN No · PO No · Vendor Code · Vendor Name · Vendor Invoice No · Store Code · Store Name · Received Qty · Accepted Qty · Rejected Qty · GRN Status · Created By

**Drill-down:** no.

### GRN vs Vendor Invoice Reconciliation

**Report id:** `cat-grn-vs-vendor-invoice-reconciliation`

**Parameters:**

- **From** (`from`) - date, optional
- **To** (`to`) - date, optional
- **Store** (`location`) - text, optional
- **Status (optional)** (`status`) - select, optional, one of: ,Draft,Pending Approval,Received,Approved,Posted,Cancelled

**Columns:** GRN Date · GRN No · PO No · Vendor Invoice No · Vendor Name · Item Code · Item Name · Received Qty · Invoice Qty · Purchase Price *(sensitive)* · Invoice Rate · Amount Variance · Status

**Drill-down:** no.

### PO Amendment History

**Report id:** `cat-po-amendment-history`

**Parameters:**

- **From** (`from`) - date, optional
- **To** (`to`) - date, optional

**Columns:** PO No · PO Date · Amendment No · Amendment Reason · Field Changed · Old Value · New Value · User Name · Edited At · Approved By

**Drill-down:** no.

### PO Approval Pending Report

**Report id:** `cat-po-approval-pending-report`

**Parameters:**

- **From** (`from`) - date, optional
- **To** (`to`) - date, optional
- **Store** (`location`) - text, optional
- **Status (optional)** (`status`) - select, optional, one of: ,Draft,Pending Approval,Approved,Partially Received,Received,Closed,Rejected,Cancelled

**Columns:** PO Date · PO No · Vendor Name · PO Gross Value · Approval Status · Created By · Approved By · Remarks

**Drill-down:** no.

### PO vs GRN Status

**Report id:** `cat-po-vs-grn-status`

**Parameters:**

- **From** (`from`) - date, optional
- **To** (`to`) - date, optional
- **Store** (`location`) - text, optional
- **Status (optional)** (`status`) - select, optional, one of: ,Draft,Pending Approval,Approved,Partially Received,Received,Closed,Rejected,Cancelled

**Columns:** PO No · PO Date · Vendor Name · Item Code · Item Name · Design No · Combination ID · PO Qty · Received Qty · Pending Qty · GRN No · GRN Date · PO Status

**Drill-down:** no.

### Pending / Open PO Report

**Report id:** `cat-pending-open-po-report`

**Parameters:**

- **From** (`from`) - date, optional
- **To** (`to`) - date, optional
- **Store** (`location`) - text, optional
- **Status (optional)** (`status`) - select, optional, one of: ,Draft,Pending Approval,Approved,Partially Received,Received,Closed,Rejected,Cancelled

**Columns:** PO Date · PO No · Vendor Code · Vendor Name · Store Code · Store Name · Item Code · Item Name · Design No · Combination ID · PO Qty · Received Qty · Pending Qty · PO Status

**Drill-down:** no.

### Pending GRN / Open Receipt

**Report id:** `cat-pending-grn-open-receipt`

**Parameters:**

- **From** (`from`) - date, optional
- **To** (`to`) - date, optional
- **Store** (`location`) - text, optional
- **Status (optional)** (`status`) - select, optional, one of: ,Draft,Pending Approval,Approved,Partially Received,Received,Closed,Rejected,Cancelled

**Columns:** Receipt Note Date · Receipt Note No · PO No · Vendor Name · Item Code · Item Name · PO Qty · Received Qty · Pending Qty · Status

**Drill-down:** no.

### Pending PO with Image

**Report id:** `cat-pending-po-with-image`

**Parameters:**

- **From** (`from`) - date, optional
- **To** (`to`) - date, optional
- **Store** (`location`) - text, optional
- **Status (optional)** (`status`) - select, optional, one of: ,Draft,Pending Approval,Approved,Partially Received,Received,Closed,Rejected,Cancelled

**Columns:** PO Date · PO No · Vendor Name · Design No · Item Name · Category Name · PO Qty · Received Qty · Pending Qty · Image URL · PO Status

**Drill-down:** no.

### Pending PO with Variant Image

**Report id:** `cat-pending-po-with-variant-image`

**Parameters:**

- **From** (`from`) - date, optional
- **To** (`to`) - date, optional
- **Store** (`location`) - text, optional
- **Status (optional)** (`status`) - select, optional, one of: ,Draft,Pending Approval,Approved,Partially Received,Received,Closed,Rejected,Cancelled

**Columns:** PO Date · PO No · Vendor Name · Design No · Combination ID · SKU · Item Name · PO Qty · Received Qty · Pending Qty · Image URL · PO Status

**Drill-down:** no.

### Purchase Itemwise Detail

**Report id:** `cat-purchase-itemwise-detail`

**Parameters:**

- **From** (`from`) - date, optional
- **To** (`to`) - date, optional
- **Store** (`location`) - text, optional
- **Status (optional)** (`status`) - select, optional, one of: ,Draft,Pending Approval,Received,Approved,Posted,Cancelled

**Columns:** GRN Date · GRN No · Vendor Code · Vendor Name · Vendor Invoice No · Item Code · Item Name · SKU · Barcode · Design No · Combination ID · Category Name · Received Qty · Accepted Qty · Rejected Qty · Purchase Price *(sensitive)* · Taxable Amount · GST % · GST Amount · Net Amount

**Drill-down:** no.

### Purchase Order Itemwise Detail

**Report id:** `cat-purchase-order-itemwise-detail`

**Parameters:**

- **From** (`from`) - date, optional
- **To** (`to`) - date, optional
- **Store** (`location`) - text, optional
- **Status (optional)** (`status`) - select, optional, one of: ,Draft,Pending Approval,Approved,Partially Received,Received,Closed,Rejected,Cancelled

**Columns:** PO Date · PO No · Vendor Code · Vendor Name · Vendor GSTIN · Store Code · Store Name · Item Code · Item Name · SKU · Barcode · Design No · Combination ID · Category Name · HSN Code · PO Qty · PO Rate · PO Basic Value · GST % · PO GST Amount · PO Gross Value · Received Qty · Pending Qty · PO Status

**Drill-down:** no.

### Purchase Order Register

**Report id:** `cat-purchase-order-register`

**Parameters:**

- **From** (`from`) - date, optional
- **To** (`to`) - date, optional
- **Store** (`location`) - text, optional
- **Status (optional)** (`status`) - select, optional, one of: ,Draft,Pending Approval,Approved,Partially Received,Received,Closed,Rejected,Cancelled

**Columns:** PO Date · PO No · Vendor Code · Vendor Name · Vendor GSTIN · Store Code · Store Name · PO Status · PO Qty · PO Basic Value · PO GST Amount · PO Gross Value · Received Qty · Pending Qty · Created By · Approved By

**Drill-down:** no.

### Purchase Register

**Report id:** `cat-purchase-register`

**Parameters:**

- **From** (`from`) - date, optional
- **To** (`to`) - date, optional
- **Store** (`location`) - text, optional
- **Status (optional)** (`status`) - select, optional, one of: ,Draft,Pending Approval,Received,Approved,Posted,Cancelled

**Columns:** GRN Date · GRN No · Vendor Code · Vendor Name · Vendor GSTIN · Vendor Invoice No · Vendor Invoice Date · Taxable Amount · CGST Amount · SGST Amount · IGST Amount · Net Amount · Status

**Drill-down:** no.

### Purchase Requisition Register

**Report id:** `cat-purchase-requisition-register`

**Parameters:**

- **From** (`from`) - date, optional
- **To** (`to`) - date, optional

**Columns:** Purchase Requisition No · Document Date · Store Code · Store Name · Item Code · Item Name · Category Name · Qty · Reason · Approval Status · Created By · Approved By

**Drill-down:** no.

### Purchase Return Itemwise Detail

**Report id:** `cat-purchase-return-itemwise-detail`

**Parameters:**

- **From** (`from`) - date, optional
- **To** (`to`) - date, optional
- **Store** (`location`) - text, optional

**Columns:** Document Date · Document No · Return From Outlet · Transaction Source Location · Transaction Destination Location · Vendor Code · Vendor Name · Item Code · Item Name · SKU · Barcode · Design No · Combination ID · Category Name · Qty · GRN No · Purchase Price *(sensitive)* · Taxable Amount · GST Amount · Net Amount · Reason

**Drill-down:** no.

### Purchase Return Register

**Report id:** `cat-purchase-return-register`

**Parameters:**

- **From** (`from`) - date, optional
- **To** (`to`) - date, optional
- **Store** (`location`) - text, optional

**Columns:** Document Date · Document No · Return From Outlet · Transaction Source Location · Transaction Destination Location · Vendor Code · Vendor Name · Qty · Taxable Amount · GST Amount · Net Amount · Debit Note No · Status

**Drill-down:** no.

### RFQ / Quotation Comparison

**Report id:** `cat-rfq-quotation-comparison`

**Parameters:**

- **From** (`from`) - date, optional
- **To** (`to`) - date, optional

**Columns:** RFQ No · Document Date · Item Code · Item Name · Vendor Code · Vendor Name · Quotation No · PO Qty · PO Rate · Landed Cost *(sensitive)* · GST % · PO Gross Value · Status

**Drill-down:** no.

### Vendor Credit Note Register

**Report id:** `cat-vendor-credit-note-register`

**Parameters:**

- **From** (`from`) - date, optional
- **To** (`to`) - date, optional
- **Store** (`location`) - text, optional

**Columns:** Document Date · Credit Note No (Vendor) · Vendor Code · Vendor Name · GRN No · Vendor Invoice No · Taxable Amount · GST Amount · Net Amount · Reason · Status

**Drill-down:** no.

### Vendor Debit Note Register

**Report id:** `cat-vendor-debit-note-register`

**Parameters:**

- **From** (`from`) - date, optional
- **To** (`to`) - date, optional
- **Store** (`location`) - text, optional

**Columns:** Document Date · Debit Note No · Vendor Code · Vendor Name · GRN No · Vendor Invoice No · Taxable Amount · GST Amount · Net Amount · Reason · Status

**Drill-down:** no.

### Vendor Performance Summary

**Report id:** `cat-vendor-performance-summary`

**Parameters:**

- **From** (`from`) - date, optional
- **To** (`to`) - date, optional
- **Store** (`location`) - text, optional
- **Status (optional)** (`status`) - select, optional, one of: ,Draft,Pending Approval,Approved,Partially Received,Received,Closed,Rejected,Cancelled

**Columns:** From Date · To Date · Vendor Code · Vendor Name · PO Qty · Received Qty · Rejected Qty · Pending Qty · Amount Variance · Status

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

### Customer Advance Register

**Report id:** `advance-register`

**Parameters:**

- **Store code (blank = all)** (`store`) - text, optional
- **Open only** (`open`) - select, optional, one of: ,Yes

**Columns:** Receipt voucher · Taken · Store · Customer · For · Expected · Taken · Used · Refunded · Balance · Status · Paid by

**Drill-down:** no.

### Customer Ledger

**Report id:** `customer-ledger`

**Parameters:**

- **Customer (optional)** (`customer`) - text, optional

**Columns:** Invoice ID · Customer · Invoice Number · Total Amount *(sensitive)* · Status · Date

**Drill-down:** no.

### Customer advances outstanding (ageing)

**Report id:** `advance-outstanding-ageing`

**Parameters:** none - it runs as soon as you pick it.

**Columns:** Receipt voucher · Customer · Store · Taken · Expected · Balance · Age (days) · Age

**Drill-down:** no.

### Gift With Purchase Register

**Report id:** `gwp-issue-register`

**Parameters:**

- **From** (`from`) - date, optional
- **To** (`to`) - date, optional

**Columns:** Date · Bill · Store · Customer · Gift · Qty · Offer

**Drill-down:** no.

### Post-Sale Edit History

**Report id:** `post-sale-edit-history`

**Parameters:**

- **From** (`from_date`) - date, optional
- **To** (`to_date`) - date, optional
- **Document type** (`document_type`) - select, optional, one of: ,Sales Invoice,Sales Order,Till bill
- **Number** (`document_id`) - text, optional

**Columns:** When · Document · Number · Status then · Field · Before · After · Changed by · Reason

**Drill-down:** no.

### Sales Register

**Report id:** `sales-register`

**Parameters:** none - it runs as soon as you pick it.

**Columns:** Cart Number · Location · Payment Mode · Status · Sale Total *(sensitive)* · Date · Data Issue

**Drill-down:** no.

### Sales by Customer Reference

**Report id:** `sales-by-customer-reference`

**Parameters:**

- **From** (`from_date`) - date, optional
- **To** (`to_date`) - date, optional
- **Customer PO / Ref** (`customer_reference`) - text, optional
- **Customer** (`customer`) - text, optional

**Columns:** Customer PO / Ref · Document · Number · Date · Customer · From order · Amount · Status

**Drill-down:** no.

### Salesman-wise Sales

**Report id:** `salesman-sales`

**Parameters:**

- **Store code (blank = all)** (`store`) - text, optional
- **From** (`from`) - date, optional
- **To** (`to`) - date, optional

**Columns:** Salesman · Bills · Sales

**Drill-down:** no.

### Tender-wise Sales

**Report id:** `tender-sales`

**Parameters:**

- **Store code (blank = all)** (`store`) - text, optional
- **From** (`from`) - date, optional
- **To** (`to`) - date, optional

**Columns:** Tender · Amount

**Drill-down:** no.

### Z-report (till close)

**Report id:** `z-report`

**Parameters:**

- **From** (`from`) - date, optional
- **To** (`to`) - date, optional
- **Store** (`store`) - text, optional

**Columns:** Closed · Store · Till · Cashier · Bills · First bill · Last bill · Opening float · Cash sales · Cash refunds · Advances in · Advances refunded · Paid in · Paid out · Safe drops · Expected cash · Counted cash · Variance · Tenders · Card declared · Card difference · UPI declared · UPI difference · Status

**Drill-down:** no.


## Sales & POS

### B2B Sales Register

**Report id:** `cat-b2b-sales-register`

**Parameters:**

- **From** (`from`) - date, optional
- **To** (`to`) - date, optional
- **Store** (`location`) - text, optional
- **Status (optional)** (`status`) - select, optional, one of: ,Approved,Posted,Paid,Overdue

**Columns:** Legal Entity · Store Code · Store Name · Sales Invoice Date · Sales Invoice No · GST Bill No · Customer ID · Customer Name · Customer GSTIN · PO Number · Place of Supply · Taxable Amount · CGST Amount · SGST Amount · IGST Amount · Bill Net Amount · Status

**Drill-down:** no.

### Cancelled / Void Bill Register

**Report id:** `cat-cancelled-void-bill-register`

**Parameters:**

- **From** (`from`) - date, optional
- **To** (`to`) - date, optional
- **Store** (`location`) - text, optional

**Columns:** Legal Entity · Store Code · Store Name · Sales Invoice Date · Sales Invoice No · Customer ID · Customer Name · Bill Net Amount · Reason · User ID · User Name · Approved By · Approved At · Status

**Drill-down:** no.

### Credit Note Register

**Report id:** `cat-credit-note-register`

**Parameters:**

- **From** (`from`) - date, optional
- **To** (`to`) - date, optional
- **Store** (`location`) - text, optional
- **Status (optional)** (`status`) - select, optional, one of: ,Draft,Approved,Posted,Applied,Cancelled

**Columns:** Legal Entity · Store Code · Store Name · Document Date · Credit Note No · Original Bill No · Customer ID · Customer Name · Credit Note Amount · Status · Sales Invoice No · Created By

**Drill-down:** no.

### Customer Advance Itemwise Detail

**Report id:** `cat-customer-advance-itemwise-detail`

**Parameters:**

- **From** (`from`) - date, optional
- **To** (`to`) - date, optional
- **Store** (`location`) - text, optional

**Columns:** Legal Entity · Store Code · Store Name · Advance Date · Advance No · Customer ID · Customer Name · Customer Mobile · Item Code · Item Name · SKU · Barcode · Design No · Combination ID · Category Name · Qty · MRP · Advance Amount · Advance Adjusted Amount · Advance Balance Amount · Advance Status · Sales Invoice No · POS Bill Status · POS Invoice No

**Drill-down:** no.

### Customer Advance Summary

**Report id:** `cat-customer-advance-summary`

**Parameters:**

- **From** (`from`) - date, optional
- **To** (`to`) - date, optional
- **Store** (`location`) - text, optional

**Columns:** Legal Entity · Store Code · Store Name · Advance Date · Advance No · Customer ID · Customer Name · Customer Mobile · Advance Amount · Advance Adjusted Amount · Advance Balance Amount · Advance Status · Sales Invoice No · Created By

**Drill-down:** no.

### Customer Advance Utilization

**Report id:** `cat-customer-advance-utilization`

**Parameters:**

- **From** (`from`) - date, optional
- **To** (`to`) - date, optional
- **Store** (`location`) - text, optional

**Columns:** Legal Entity · Store Code · Store Name · Advance No · Customer ID · Customer Name · Sales Invoice Date · Sales Invoice No · Advance Adjusted Amount · Advance Balance Amount · Status · Created By

**Drill-down:** no.

### Export / International Sales Register

**Report id:** `cat-export-international-sales-register`

**Parameters:**

- **From** (`from`) - date, optional
- **To** (`to`) - date, optional
- **Store** (`location`) - text, optional
- **Status (optional)** (`status`) - select, optional, one of: ,Approved,Posted,Paid,Overdue

**Columns:** Legal Entity · Store Code · Store Name · Sales Invoice Date · Sales Invoice No · Customer ID · Customer Name · Sales Channel · Place of Supply · Tax Type · Gross Amount · Taxable Amount · IGST Amount · Bill Net Amount · Status

**Drill-down:** no.

### Gift Card / Voucher Register

**Report id:** `cat-gift-card-voucher-register`

**Parameters:**

- **From** (`from`) - date, optional
- **To** (`to`) - date, optional
- **Store** (`location`) - text, optional

**Columns:** Legal Entity · Store Code · Store Name · Document Date · Gift Card No · Customer ID · Customer Name · Gift Card Amount · Sales Invoice No · Tender Type · Status · Created By

**Drill-down:** no.

### Net Sales Summary

**Report id:** `cat-net-sales-summary`

**Parameters:**

- **From** (`from`) - date, optional
- **To** (`to`) - date, optional
- **Store** (`location`) - text, optional

**Columns:** Document Date · Store Code · Store Name · Sales Channel · Bill Count · Sold Qty · Return Qty · Net Sales Qty · Sales Amount · Sales Return Amount · Net Sales Amount · Discount Amount · GST Amount

**Drill-down:** no.

### Sales Bill Edit Audit Report

**Report id:** `cat-sales-bill-edit-audit-report`

**Parameters:**

- **From** (`from`) - date, optional
- **To** (`to`) - date, optional
- **Store** (`location`) - text, optional

**Columns:** Legal Entity · Store Code · Store Name · Sales Invoice Date · Sales Invoice No · Audit ID · Edit Action · Field Changed · Old Value · New Value · Edit Reason · Edited By · Edited At

**Drill-down:** no.

### Sales Counter Summary

**Report id:** `cat-sales-counter-summary`

**Parameters:**

- **From** (`from`) - date, optional
- **To** (`to`) - date, optional
- **Store** (`location`) - text, optional

**Columns:** From Date · To Date · Store Code · Store Name · Counter · Bill Count · Sold Qty · Return Qty · Net Sales Amount · Tender Amount

**Drill-down:** no.

### Sales Design Image Register

**Report id:** `cat-sales-design-image-register`

**Parameters:**

- **From** (`from`) - date, optional
- **To** (`to`) - date, optional
- **Store** (`location`) - text, optional

**Columns:** Legal Entity · Store Code · Store Name · Sales Invoice Date · Sales Invoice No · Design No · Item Name · Category Name · Sold Qty · Image URL

**Drill-down:** no.

### Sales Discount Analysis

**Report id:** `cat-sales-discount-analysis`

**Parameters:**

- **From** (`from`) - date, optional
- **To** (`to`) - date, optional
- **Store** (`location`) - text, optional

**Columns:** Legal Entity · Store Code · Store Name · Sales Invoice Date · Sales Invoice No · Customer ID · Customer Name · Item Code · Item Name · SKU · Barcode · Design No · Combination ID · Category Name · MRP · Gross Value · Customer Redeem Points · Item Discount · Bill Discount · Discount Amount · Item Taxable Amount · GST Amount · Item Net Amount

**Drill-down:** no.

### Sales Itemwise Detail

**Report id:** `cat-sales-itemwise-detail`

**Parameters:**

- **From** (`from`) - date, optional
- **To** (`to`) - date, optional
- **Store** (`location`) - text, optional

**Columns:** Legal Entity · Store Code · Store Name · Sales Invoice Date · Sales Invoice No · GST Bill No · Customer ID · Customer Name · Sales Channel · Item Code · Item Name · SKU · Barcode · Design No · Combination ID · Category Name · Sold Qty · MRP · Gross Value · Customer Redeem Points · Item Discount · Bill Discount · Packing Charges · Item Taxable Amount · GST % · GST Amount · Item Net Amount · Salesperson Name

**Drill-down:** no.

### Sales Margin Analysis

**Report id:** `cat-sales-margin-analysis`

**Parameters:**

- **From** (`from`) - date, optional
- **To** (`to`) - date, optional
- **Store** (`location`) - text, optional

**Columns:** Legal Entity · Store Code · Store Name · Sales Invoice Date · Sales Invoice No · Item Code · Item Name · SKU · Barcode · Design No · Combination ID · Category Name · Sold Qty · Item Net Amount · Purchase Price *(sensitive)* · Landed Cost *(sensitive)* · Margin Amount *(sensitive)* · Margin % *(sensitive)*

**Drill-down:** no.

### Sales Register

**Report id:** `cat-sales-register`

**Parameters:**

- **From** (`from`) - date, optional
- **To** (`to`) - date, optional
- **Store** (`location`) - text, optional
- **Status (optional)** (`status`) - select, optional, one of: ,Approved,Posted,Paid,Overdue

**Columns:** Legal Entity · Store Code · Store Name · Sales Invoice Date · Sales Invoice No · GST Bill No · Customer ID · Customer Name · Customer Mobile · Sales Channel · Customer GSTIN · PO Number · Bill Count · Gross Amount · Discount Amount · Taxable Amount · GST Amount · Bill Net Amount · Tender Type · Salesperson Name · Status

**Drill-down:** no.

### Sales Return Itemwise Detail

**Report id:** `cat-sales-return-itemwise-detail`

**Parameters:**

- **From** (`from`) - date, optional
- **To** (`to`) - date, optional
- **Store** (`location`) - text, optional

**Columns:** Legal Entity · Store Code · Store Name · Sales Return Date · Sales Return No · Original Bill No · Customer ID · Customer Name · Item Code · Item Name · SKU · Barcode · Design No · Combination ID · Category Name · Return Qty · MRP · Gross Value · Customer Redeem Points · Item Discount · Packing Charges · Item Taxable Amount · GST % · GST Amount · Item Net Amount · Reason

**Drill-down:** no.

### Sales Return Register

**Report id:** `cat-sales-return-register`

**Parameters:**

- **From** (`from`) - date, optional
- **To** (`to`) - date, optional
- **Store** (`location`) - text, optional

**Columns:** Legal Entity · Store Code · Store Name · Sales Return Date · Sales Return No · GST Bill No · Original Bill No · Customer ID · Customer Name · Sales Channel · Return Qty · Taxable Amount · GST Amount · Net Amount · Credit Note No · Status

**Drill-down:** no.

### Sales Tender Detail

**Report id:** `cat-sales-tender-detail`

**Parameters:**

- **From** (`from`) - date, optional
- **To** (`to`) - date, optional
- **Store** (`location`) - text, optional

**Columns:** Legal Entity · Store Code · Store Name · Sales Invoice Date · Sales Invoice No · Customer Name · Tender Type · Tender Amount · Reference No · Bill Net Amount · Status

**Drill-down:** no.

### Sales Tender Summary

**Report id:** `cat-sales-tender-summary`

**Parameters:**

- **From** (`from`) - date, optional
- **To** (`to`) - date, optional
- **Store** (`location`) - text, optional

**Columns:** Document Date · Store Code · Store Name · Tender Type · Bill Count · Tender Amount · Net Sales Amount

**Drill-down:** no.

### Sales Variant Image Register

**Report id:** `cat-sales-variant-image-register`

**Parameters:**

- **From** (`from`) - date, optional
- **To** (`to`) - date, optional
- **Store** (`location`) - text, optional

**Columns:** Legal Entity · Store Code · Store Name · Sales Invoice Date · Sales Invoice No · Design No · Combination ID · SKU · Item Name · Sold Qty · Image URL

**Drill-down:** no.

### Salesman Sales Summary

**Report id:** `cat-salesman-sales-summary`

**Parameters:**

- **From** (`from`) - date, optional
- **To** (`to`) - date, optional
- **Store** (`location`) - text, optional

**Columns:** From Date · To Date · Store Code · Store Name · Salesperson ID · Salesperson Name · Bill Count · Sold Qty · Net Sales Amount · ABV · UPT · Discount Amount

**Drill-down:** no.


## Service

### Service SLA Breaches

**Report id:** `service-sla-breaches`

**Parameters:** none - it runs as soon as you pick it.

**Columns:** Ticket · Customer · Status · Breach Type · Respond By · Resolve By

**Drill-down:** no.


## Stock Transfer

### Transfer Approval / Cancellation Audit

**Report id:** `cat-transfer-approval-cancellation-audit`

**Parameters:**

- **From** (`from`) - date, optional
- **To** (`to`) - date, optional
- **Store** (`location`) - text, optional

**Columns:** Transfer Out No · Document Date · From Store · To Store · Edit Action · Reason · User ID · User Name · Approved By · Approved At · Status

**Drill-down:** no.

### Transfer Barcode Traceability

**Report id:** `cat-transfer-barcode-traceability`

**Parameters:**

- **From** (`from`) - date, optional
- **To** (`to`) - date, optional
- **Store** (`location`) - text, optional
- **Status (optional)** (`status`) - select, optional, one of: ,Draft,Pending Approval,Approved,Dispatched,In Transit,Partially Received,Received,Closed,Cancelled

**Columns:** Barcode · SKU · Design No · Combination ID · Transfer Out No · Transfer Out Date · Transfer In No · Transfer In Date · From Store · To Store · Transfer Status

**Drill-down:** no.

### Transfer GST / E-Way Control

**Report id:** `cat-transfer-gst-e-way-control`

**Parameters:**

- **From** (`from`) - date, optional
- **To** (`to`) - date, optional
- **Store** (`location`) - text, optional
- **Status (optional)** (`status`) - select, optional, one of: ,Draft,Pending Approval,Approved,Dispatched,In Transit,Partially Received,Received,Closed,Cancelled

**Columns:** Transfer Out Date · Transfer Out No · From Store · To Store · Taxable Amount · GST Amount · IRN · IRN Status · E-Way Bill No · E-Way Status · Status

**Drill-down:** no.

### Transfer In Itemwise Detail

**Report id:** `cat-transfer-in-itemwise-detail`

**Parameters:**

- **From** (`from`) - date, optional
- **To** (`to`) - date, optional
- **Store** (`location`) - text, optional

**Columns:** Transfer In Date · Transfer In No · Transfer Out No · From Store · To Store · Item Code · Item Name · SKU · Barcode · Design No · Combination ID · Category Name · Qty · Receipt Date · Transfer Status · Reason · Remarks

**Drill-down:** no.

### Transfer In Register

**Report id:** `cat-transfer-in-register`

**Parameters:**

- **From** (`from`) - date, optional
- **To** (`to`) - date, optional
- **Store** (`location`) - text, optional

**Columns:** Transfer In Date · Transfer In No · Transfer Out No · From Store · To Store · Qty · Receipt Date · Transfer Status · Created By

**Drill-down:** no.

### Transfer In-Transit Aging

**Report id:** `cat-transfer-in-transit-aging`

**Parameters:**

- **From** (`from`) - date, optional
- **To** (`to`) - date, optional
- **Store** (`location`) - text, optional
- **Status (optional)** (`status`) - select, optional, one of: ,Draft,Pending Approval,Approved,Dispatched,In Transit,Partially Received,Received,Closed,Cancelled

**Columns:** Transfer Out Date · Transfer Out No · From Store · To Store · Dispatch Date · In-Transit Days · Qty · In-Transit Qty · Transfer Status

**Drill-down:** no.

### Transfer Out Itemwise Detail

**Report id:** `cat-transfer-out-itemwise-detail`

**Parameters:**

- **From** (`from`) - date, optional
- **To** (`to`) - date, optional
- **Store** (`location`) - text, optional
- **Status (optional)** (`status`) - select, optional, one of: ,Draft,Pending Approval,Approved,Dispatched,In Transit,Partially Received,Received,Closed,Cancelled

**Columns:** Transfer Out Date · Transfer Out No · From Store · To Store · Item Code · Item Name · SKU · Barcode · Design No · Combination ID · Category Name · Qty · Transfer Status · Dispatch Date · E-Way Bill No · IRN

**Drill-down:** no.

### Transfer Out Register

**Report id:** `cat-transfer-out-register`

**Parameters:**

- **From** (`from`) - date, optional
- **To** (`to`) - date, optional
- **Store** (`location`) - text, optional
- **Status (optional)** (`status`) - select, optional, one of: ,Draft,Pending Approval,Approved,Dispatched,In Transit,Partially Received,Received,Closed,Cancelled

**Columns:** Transfer Out Date · Transfer Out No · From Store · To Store · Qty · Transfer Status · Dispatch Date · E-Way Bill No · IRN · Created By

**Drill-down:** no.

### Transfer Shortage / Excess Exception

**Report id:** `cat-transfer-shortage-excess-exception`

**Parameters:**

- **From** (`from`) - date, optional
- **To** (`to`) - date, optional
- **Store** (`location`) - text, optional

**Columns:** Transfer Out No · Transfer In No · From Store · To Store · Item Code · Item Name · SKU · Barcode · Design No · Combination ID · Category Name · Qty · Received Qty · Difference Qty · Reason · Remarks · Status

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

### Appointment Calendar Report

**Report id:** `cat-appointment-calendar-report`

**Parameters:**

- **From** (`from`) - date, optional
- **To** (`to`) - date, optional
- **Store** (`location`) - text, optional
- **Status (optional)** (`status`) - select, optional, one of: ,Scheduled,Checked In,At Door,Completed,No Show,Cancelled

**Columns:** Document Date · Appointment No · Vendor Name · Dock Door · Vehicle No · Qty · Status

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

### Bin Inventory Report

**Report id:** `cat-bin-inventory-report`

**Parameters:**

- **Store** (`location`) - text, optional

**Columns:** As Of Date · Bin Code · Bin Condition · SKU · Barcode · Available Qty · Blocked Qty · LPN · Stock Age Days

**Drill-down:** no.

### Bin Replenishment Report

**Report id:** `cat-bin-replenishment-report`

**Parameters:**

- **From** (`from`) - date, optional
- **To** (`to`) - date, optional
- **Store** (`location`) - text, optional

**Columns:** Document Date · Document No · SKU · From Location · To Location · Qty · Status · RF User · Task Duration Min

**Drill-down:** no.

### Bin Stock Aging

**Report id:** `cat-bin-stock-aging`

**Parameters:**

- **Store** (`location`) - text, optional

**Columns:** As Of Date · Bin Code · SKU · Item Name · Available Qty · Stock Age Days · Age Bucket · MRP Value

**Drill-down:** no.

### Bin Stock Reconciliation

**Report id:** `bin-stock-reconciliation`

**Parameters:**

- **Item Code (optional)** (`sku`) - text, optional
- **Location (optional)** (`location`) - text, optional
- **Show** (`mismatches_only`) - select, optional, one of: Mismatches only,All rows

**Columns:** Location · Item · On Hand · In Bins · Picked, Not Dispatched · Difference · Status

**Drill-down:** no.

### Bin Utilization / Condition Report

**Report id:** `cat-bin-utilization-condition-report`

**Parameters:**

- **Store** (`location`) - text, optional
- **Status (optional)** (`status`) - select, optional, one of: ,Active,Inactive,Blocked

**Columns:** As Of Date · Bin Code · Bin Condition · Available Qty · Blocked Qty · Status · Remarks

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

### Cycle Count Register

**Report id:** `cat-cycle-count-register`

**Parameters:**

- **From** (`from`) - date, optional
- **To** (`to`) - date, optional
- **Store** (`location`) - text, optional

**Columns:** Document Date · Cycle Count No · Bin Code · SKU · System Qty · Physical Qty · Difference Qty · Status · User Name

**Drill-down:** no.

### Cycle Count Variance Report

**Report id:** `cat-cycle-count-variance-report`

**Parameters:**

- **From** (`from`) - date, optional
- **To** (`to`) - date, optional
- **Store** (`location`) - text, optional

**Columns:** Document Date · Cycle Count No · Bin Code · SKU · Barcode · System Qty · Physical Qty · Difference Qty · Reason · Approval Status

**Drill-down:** no.

### Dispatch / Shipment Register

**Report id:** `cat-dispatch-shipment-register`

**Parameters:**

- **From** (`from`) - date, optional
- **To** (`to`) - date, optional
- **Store** (`location`) - text, optional

**Columns:** Document Date · Order ID · Shipment ID · Dispatch Qty · Tracking Number · Shipping Provider · Vehicle No · Dock Door · Status

**Drill-down:** no.

### Dock Door Utilization

**Report id:** `cat-dock-door-utilization`

**Parameters:**

- **From** (`from`) - date, optional
- **To** (`to`) - date, optional
- **Store** (`location`) - text, optional
- **Status (optional)** (`status`) - select, optional, one of: ,Scheduled,Checked In,At Door,Completed,No Show,Cancelled

**Columns:** Document Date · Dock Door · Appointment No · Vendor Name · Vehicle No · SLA Due At · Status

**Drill-down:** no.

### Facility Roll-Up Inventory Inquiry

**Report id:** `facility-rollup`

**Parameters:**

- **Facility (Location code)** (`facility_code`) - text, **required**

**Columns:** SKU · On Hand · Available · In Transit · Locations

**Drill-down:** no.

### Inbound / ASN Register

**Report id:** `cat-inbound-asn-register`

**Parameters:**

- **From** (`from`) - date, optional
- **To** (`to`) - date, optional
- **Store** (`location`) - text, optional

**Columns:** Document Date · Document No · Vendor Code · Vendor Name · Appointment No · Dock Door · Qty · Status

**Drill-down:** no.

### Inbound QC Exception

**Report id:** `cat-inbound-qc-exception`

**Parameters:**

- **From** (`from`) - date, optional
- **To** (`to`) - date, optional
- **Store** (`location`) - text, optional

**Columns:** GRN Date · GRN No · SKU · Barcode · Received Qty · Rejected Qty · QC Result · Reason · Status

**Drill-down:** no.

### LPN / Carton / Pallet Register

**Report id:** `cat-lpn-carton-pallet-register`

**Parameters:**

- **From** (`from`) - date, optional
- **To** (`to`) - date, optional

**Columns:** Document Date · LPN · Carton No · Pallet No · Location Name · Qty · Status · Created By

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

### Location Movement Register

**Report id:** `cat-location-movement-register`

**Parameters:**

- **From** (`from`) - date, optional
- **To** (`to`) - date, optional
- **Store** (`location`) - text, optional

**Columns:** Document No · Document Date · Warehouse · SKU · Barcode · From Location · To Location · Qty · Status · Created By

**Drill-down:** no.

### Lot / Serial / Barcode Traceability

**Report id:** `cat-lot-serial-barcode-traceability`

**Parameters:**

- **From** (`from`) - date, optional
- **To** (`to`) - date, optional
- **Store** (`location`) - text, optional

**Columns:** Barcode · SKU · LPN · Bin Code · Movement Date · Movement Type · Source Document No · RF User · Status

**Drill-down:** no.

### Owner Stock Exceptions (3PL)

**Report id:** `owner-stock-exceptions`

**Parameters:** none - it runs as soon as you pick it.

**Columns:** Exception · Location · Bin · Item · Condition · Bin Qty · Owner Qty · What to do

**Drill-down:** no.

### Owner Stock Inquiry (3PL)

**Report id:** `owner-stock-inquiry`

**Parameters:**

- **Item Code (optional)** (`sku`) - text, optional
- **Location (optional)** (`location`) - text, optional
- **Owner (optional)** (`owner_id`) - text, optional

**Columns:** Owner · Item · Bin · Location · Condition · Qty

**Drill-down:** no.

### Packing Report

**Report id:** `cat-packing-report`

**Parameters:**

- **From** (`from`) - date, optional
- **To** (`to`) - date, optional
- **Store** (`location`) - text, optional

**Columns:** Document Date · Order ID · Shipment ID · Carton No · LPN · Picked Qty · Packed Qty · User Name · Status

**Drill-down:** no.

### Pick List Report

**Report id:** `cat-pick-list-report`

**Parameters:**

- **From** (`from`) - date, optional
- **To** (`to`) - date, optional
- **Store** (`location`) - text, optional

**Columns:** Document Date · Pick List No · Order ID · Shipment ID · SKU · Bin Code · Qty · Picked Qty · RF User · Status

**Drill-down:** no.

### Picking Exception Report

**Report id:** `cat-picking-exception-report`

**Parameters:**

- **From** (`from`) - date, optional
- **To** (`to`) - date, optional
- **Store** (`location`) - text, optional

**Columns:** Document Date · Pick List No · Order ID · SKU · Bin Code · Qty · Picked Qty · Variance Qty · Reason · Status

**Drill-down:** no.

### Putaway Completion Report

**Report id:** `cat-putaway-completion-report`

**Parameters:**

- **From** (`from`) - date, optional
- **To** (`to`) - date, optional
- **Store** (`location`) - text, optional

**Columns:** Document Date · Document No · SKU · Barcode · LPN · Bin Code · Qty · RF User · Task Duration Min · Putaway Status

**Drill-down:** no.

### Putaway Pending Report

**Report id:** `cat-putaway-pending-report`

**Parameters:**

- **From** (`from`) - date, optional
- **To** (`to`) - date, optional
- **Store** (`location`) - text, optional

**Columns:** Document Date · Document No · SKU · Barcode · LPN · From Location · To Location · Putaway Status · SLA Age Hours

**Drill-down:** no.

### RF / Mobile Picking Productivity

**Report id:** `cat-rf-mobile-picking-productivity`

**Parameters:**

- **From** (`from`) - date, optional
- **To** (`to`) - date, optional
- **Store** (`location`) - text, optional

**Columns:** From Date · To Date · RF User · Pick List No · Picked Qty · Task Duration Min · Achievement % · Status

**Drill-down:** no.

### Receiving / GRN Detail

**Report id:** `cat-receiving-grn-detail`

**Parameters:**

- **From** (`from`) - date, optional
- **To** (`to`) - date, optional
- **Store** (`location`) - text, optional

**Columns:** GRN Date · GRN No · Vendor Name · SKU · Barcode · LPN · Received Qty · Accepted Qty · Rejected Qty · Dock Door · Status

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

### WMS Stock Transfer Execution Report

**Report id:** `cat-wms-stock-transfer-execution-report`

**Parameters:**

- **From** (`from`) - date, optional
- **To** (`to`) - date, optional
- **Store** (`location`) - text, optional

**Columns:** Transfer Out No · Transfer Date · From Location · To Location · SKU · Barcode · Transfer Qty · Picked Qty · Packed Qty · Dispatched Qty · Status

**Drill-down:** no.

### Warehouse Cockpit KPI Summary

**Report id:** `cat-warehouse-cockpit-kpi-summary`

**Parameters:**

- **Store** (`location`) - text, optional

**Columns:** As Of Date · Warehouse · Pending Inbound Qty · Pending Putaway Qty · Pending Pick Qty · Pending Pack Qty · Pending Dispatch Qty · Exception Count

**Drill-down:** no.

### Warehouse Inventory Accuracy Summary

**Report id:** `cat-warehouse-inventory-accuracy-summary`

**Parameters:**

- **From** (`from`) - date, optional
- **To** (`to`) - date, optional
- **Store** (`location`) - text, optional

**Columns:** From Date · To Date · Location Name · Bin Code · System Qty · Physical Qty · Difference Qty · Achievement %

**Drill-down:** no.

### Warehouse SLA / Throughput Summary

**Report id:** `cat-warehouse-sla-throughput-summary`

**Parameters:**

- **From** (`from`) - date, optional
- **To** (`to`) - date, optional
- **Store** (`location`) - text, optional

**Columns:** Document Date · Document Type · Qty · Task Duration Min · SLA Age Hours · Achievement % · Status

**Drill-down:** no.

### Wave / Batch Picking Report

**Report id:** `cat-wave-batch-picking-report`

**Parameters:**

- **From** (`from`) - date, optional
- **To** (`to`) - date, optional
- **Store** (`location`) - text, optional

**Columns:** Document Date · Wave No · Order ID · Shipment ID · SKU · Qty · Picked Qty · RF User · Task Duration Min · Status

**Drill-down:** no.

### Yard / Trailer Board Report

**Report id:** `cat-yard-trailer-board-report`

**Parameters:**

- **From** (`from`) - date, optional
- **To** (`to`) - date, optional
- **Store** (`location`) - text, optional

**Columns:** Document Date · Vehicle No · Appointment No · Dock Door · Yard Status · SLA Age Hours · Status

**Drill-down:** no.

