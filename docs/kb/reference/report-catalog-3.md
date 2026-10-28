---
title: Report catalog (part 3)
section: Reference
order: 22
summary: Reports in Procurement & GRN to WMS, continued from the report catalog.
audience: store manager, finance, category manager, admin
last_verified: 2026-09-10
owner: documentation-maintainer
status: active
topic_type: reference
module: platform
task: Report catalog (part 3)
prerequisites: Authorized access to the relevant ERP task
applies_to: source registries; not release acceptance
screens: [reports]
---

<!-- GENERATED ARTICLE - DO NOT EDIT BY HAND.
     Regenerate: pwsh docs/update-docs.ps1 -Group Content -->

# Report catalog (part 3)

This page continues the [report catalog](report-catalog.md), which lists
every category and explains how to run a report. It holds **118** reports, from
Procurement & GRN to WMS.

## Contents

- [Procurement & GRN](#procurement-grn) - 27 reports
- [Sales](#sales) - 10 reports
- [Sales & POS](#sales-pos) - 22 reports
- [Service](#service) - 1 reports
- [Stock Transfer](#stock-transfer) - 9 reports
- [WMS](#wms) - 49 reports

## Procurement & GRN

### R2010 Cancelled PO Register

**Report id:** `cat-cancelled-po-register`

**Parameters:**

- **From** (`from`) - date, optional
- **To** (`to`) - date, optional
- **Store** (`location`) - text, optional
- **Status (optional)** (`status`) - select, optional, one of: ,Draft,Pending Approval,Approved,Partially Received,Received,Closed,Rejected,Cancelled

**Columns:** PO Date · PO No · Vendor Name · PO Gross Value · Cancelled Qty · Reason · User Name · Approved By · Status

**Drill-down:** no.

### R2011 Expired PO Report

**Report id:** `cat-expired-po-report`

**Parameters:**

- **From** (`from`) - date, optional
- **To** (`to`) - date, optional
- **Store** (`location`) - text, optional
- **Status (optional)** (`status`) - select, optional, one of: ,Draft,Pending Approval,Approved,Partially Received,Received,Closed,Rejected,Cancelled

**Columns:** PO Date · PO No · Vendor Name · Store Name · PO Qty · Received Qty · Pending Qty · PO Status · Remarks

**Drill-down:** no.

### R2024 GRN Barcode Generation Register

**Report id:** `cat-grn-barcode-generation-register`

**Parameters:**

- **From** (`from`) - date, optional
- **To** (`to`) - date, optional
- **Store** (`location`) - text, optional

**Columns:** GRN Date · GRN No · PO No · Vendor Name · Barcode · SKU · Design No · Combination ID · MRP · Purchase Price *(sensitive)* · Created By

**Drill-down:** no.

### R2023 GRN Cancelled Register

**Report id:** `cat-grn-cancelled-register`

**Parameters:**

- **From** (`from`) - date, optional
- **To** (`to`) - date, optional
- **Store** (`location`) - text, optional
- **Status (optional)** (`status`) - select, optional, one of: ,Draft,Pending Approval,Received,Approved,Posted,Cancelled

**Columns:** GRN Date · GRN No · PO No · Vendor Name · Received Qty · Reason · User ID · User Name · Approved By · Status

**Drill-down:** no.

### R2021 GRN Itemwise Detail

**Report id:** `cat-grn-itemwise-detail`

**Parameters:**

- **From** (`from`) - date, optional
- **To** (`to`) - date, optional
- **Store** (`location`) - text, optional
- **Status (optional)** (`status`) - select, optional, one of: ,Draft,Pending Approval,Received,Approved,Posted,Cancelled

**Columns:** GRN Date · GRN No · PO No · Vendor Code · Vendor Name · Vendor Invoice No · Item Code · Item Name · SKU · Barcode · Design No · Combination ID · Category Name · Received Qty · Accepted Qty · Rejected Qty · PO Rate · Invoice Rate · Silver Rate / Gram *(sensitive)* · Making Charge / Gram *(sensitive)* · Silver + Making Cost / Gram *(sensitive)* · Purchase Price *(sensitive)* · MRP · User Name · GRN Status

**Drill-down:** no.

### R2022 GRN PO / Invoice Variance

**Report id:** `cat-grn-po-invoice-variance`

**Parameters:**

- **From** (`from`) - date, optional
- **To** (`to`) - date, optional
- **Store** (`location`) - text, optional
- **Status (optional)** (`status`) - select, optional, one of: ,Draft,Pending Approval,Received,Approved,Posted,Cancelled

**Columns:** GRN Date · GRN No · PO No · Vendor Invoice No · Vendor Name · Item Code · Item Name · PO Qty · Received Qty · Invoice Qty · PO Rate · Invoice Rate · Rate Variance · Amount Variance · Debit Note No · Credit Note No (Vendor)

**Drill-down:** no.

### R2025 GRN QC / Rejection Exception

**Report id:** `cat-grn-qc-rejection-exception`

**Parameters:**

- **From** (`from`) - date, optional
- **To** (`to`) - date, optional
- **Store** (`location`) - text, optional
- **Status (optional)** (`status`) - select, optional, one of: ,Draft,Pending Approval,Received,Approved,Posted,Cancelled

**Columns:** GRN Date · GRN No · PO No · Vendor Name · Item Code · Item Name · SKU · Barcode · Design No · Combination ID · Category Name · Received Qty · Accepted Qty · Rejected Qty · Reason Code · Reason · Status · Created By

**Drill-down:** no.

### R2020 GRN Register

**Report id:** `cat-grn-register`

**Parameters:**

- **From** (`from`) - date, optional
- **To** (`to`) - date, optional
- **Store** (`location`) - text, optional
- **Status (optional)** (`status`) - select, optional, one of: ,Draft,Pending Approval,Received,Approved,Posted,Cancelled

**Columns:** GRN Date · GRN No · PO No · Vendor Code · Vendor Name · Vendor Invoice No · Store Code · Store Name · Received Qty · Accepted Qty · Rejected Qty · GRN Status · Created By

**Drill-down:** no.

### R2027 GRN vs Vendor Invoice Reconciliation

**Report id:** `cat-grn-vs-vendor-invoice-reconciliation`

**Parameters:**

- **From** (`from`) - date, optional
- **To** (`to`) - date, optional
- **Store** (`location`) - text, optional
- **Status (optional)** (`status`) - select, optional, one of: ,Draft,Pending Approval,Received,Approved,Posted,Cancelled

**Columns:** GRN Date · GRN No · PO No · Vendor Invoice No · Vendor Name · Item Code · Item Name · Received Qty · Invoice Qty · Purchase Price *(sensitive)* · Invoice Rate · Amount Variance · Status

**Drill-down:** no.

### R2009 PO Amendment History

**Report id:** `cat-po-amendment-history`

**Parameters:**

- **From** (`from`) - date, optional
- **To** (`to`) - date, optional

**Columns:** PO No · PO Date · Amendment No · Amendment Reason · Field Changed · Old Value · New Value · User Name · Edited At · Approved By

**Drill-down:** no.

### R2008 PO Approval Pending Report

**Report id:** `cat-po-approval-pending-report`

**Parameters:**

- **From** (`from`) - date, optional
- **To** (`to`) - date, optional
- **Store** (`location`) - text, optional
- **Status (optional)** (`status`) - select, optional, one of: ,Draft,Pending Approval,Approved,Partially Received,Received,Closed,Rejected,Cancelled

**Columns:** PO Date · PO No · Vendor Name · PO Gross Value · Approval Status · Created By · Approved By · Remarks

**Drill-down:** no.

### R2012 PO vs GRN Status

**Report id:** `cat-po-vs-grn-status`

**Parameters:**

- **From** (`from`) - date, optional
- **To** (`to`) - date, optional
- **Store** (`location`) - text, optional
- **Status (optional)** (`status`) - select, optional, one of: ,Draft,Pending Approval,Approved,Partially Received,Received,Closed,Rejected,Cancelled

**Columns:** PO No · PO Date · Vendor Name · Item Code · Item Name · Design No · Combination ID · PO Qty · Received Qty · Pending Qty · GRN No · GRN Date · PO Status

**Drill-down:** no.

### R2005 Pending / Open PO Report

**Report id:** `cat-pending-open-po-report`

**Parameters:**

- **From** (`from`) - date, optional
- **To** (`to`) - date, optional
- **Store** (`location`) - text, optional
- **Status (optional)** (`status`) - select, optional, one of: ,Draft,Pending Approval,Approved,Partially Received,Received,Closed,Rejected,Cancelled

**Columns:** PO Date · PO No · Vendor Code · Vendor Name · Store Code · Store Name · Item Code · Item Name · Design No · Combination ID · PO Qty · Received Qty · Pending Qty · PO Status

**Drill-down:** no.

### R2026 Pending GRN / Open Receipt

**Report id:** `cat-pending-grn-open-receipt`

**Parameters:**

- **From** (`from`) - date, optional
- **To** (`to`) - date, optional
- **Store** (`location`) - text, optional
- **Status (optional)** (`status`) - select, optional, one of: ,Draft,Pending Approval,Approved,Partially Received,Received,Closed,Rejected,Cancelled

**Columns:** Receipt Note Date · Receipt Note No · PO No · Vendor Name · Item Code · Item Name · PO Qty · Received Qty · Pending Qty · Status

**Drill-down:** no.

### R2006 Pending PO with Image

**Report id:** `cat-pending-po-with-image`

**Parameters:**

- **From** (`from`) - date, optional
- **To** (`to`) - date, optional
- **Store** (`location`) - text, optional
- **Status (optional)** (`status`) - select, optional, one of: ,Draft,Pending Approval,Approved,Partially Received,Received,Closed,Rejected,Cancelled

**Columns:** PO Date · PO No · Vendor Name · Design No · Item Name · Category Name · PO Qty · Received Qty · Pending Qty · Image URL · PO Status

**Drill-down:** no.

### R2007 Pending PO with Variant Image

**Report id:** `cat-pending-po-with-variant-image`

**Parameters:**

- **From** (`from`) - date, optional
- **To** (`to`) - date, optional
- **Store** (`location`) - text, optional
- **Status (optional)** (`status`) - select, optional, one of: ,Draft,Pending Approval,Approved,Partially Received,Received,Closed,Rejected,Cancelled

**Columns:** PO Date · PO No · Vendor Name · Design No · Combination ID · SKU · Item Name · PO Qty · Received Qty · Pending Qty · Image URL · PO Status

**Drill-down:** no.

### R2015 Purchase Itemwise Detail

**Report id:** `cat-purchase-itemwise-detail`

**Parameters:**

- **From** (`from`) - date, optional
- **To** (`to`) - date, optional
- **Store** (`location`) - text, optional
- **Status (optional)** (`status`) - select, optional, one of: ,Draft,Pending Approval,Received,Approved,Posted,Cancelled

**Columns:** GRN Date · GRN No · Vendor Code · Vendor Name · Vendor Invoice No · Item Code · Item Name · SKU · Barcode · Design No · Combination ID · Category Name · Received Qty · Accepted Qty · Rejected Qty · Purchase Price *(sensitive)* · Taxable Amount · GST % · GST Amount · Net Amount

**Drill-down:** no.

### R2004 Purchase Order Itemwise Detail

**Report id:** `cat-purchase-order-itemwise-detail`

**Parameters:**

- **From** (`from`) - date, optional
- **To** (`to`) - date, optional
- **Store** (`location`) - text, optional
- **Status (optional)** (`status`) - select, optional, one of: ,Draft,Pending Approval,Approved,Partially Received,Received,Closed,Rejected,Cancelled

**Columns:** PO Date · PO No · Vendor Code · Vendor Name · Vendor GSTIN · Store Code · Store Name · Item Code · Item Name · SKU · Barcode · Design No · Combination ID · Category Name · HSN Code · PO Qty · PO Rate · PO Basic Value · GST % · PO GST Amount · PO Gross Value · Received Qty · Pending Qty · PO Status

**Drill-down:** no.

### R2003 Purchase Order Register

**Report id:** `cat-purchase-order-register`

**Parameters:**

- **From** (`from`) - date, optional
- **To** (`to`) - date, optional
- **Store** (`location`) - text, optional
- **Status (optional)** (`status`) - select, optional, one of: ,Draft,Pending Approval,Approved,Partially Received,Received,Closed,Rejected,Cancelled

**Columns:** PO Date · PO No · Vendor Code · Vendor Name · Vendor GSTIN · Store Code · Store Name · PO Status · PO Qty · PO Basic Value · PO GST Amount · PO Gross Value · Received Qty · Pending Qty · Created By · Approved By

**Drill-down:** no.

### R2014 Purchase Register

**Report id:** `cat-purchase-register`

**Parameters:**

- **From** (`from`) - date, optional
- **To** (`to`) - date, optional
- **Store** (`location`) - text, optional
- **Status (optional)** (`status`) - select, optional, one of: ,Draft,Pending Approval,Received,Approved,Posted,Cancelled

**Columns:** GRN Date · GRN No · Vendor Code · Vendor Name · Vendor GSTIN · Vendor Invoice No · Vendor Invoice Date · Taxable Amount · CGST Amount · SGST Amount · IGST Amount · Net Amount · Status

**Drill-down:** no.

### R2001 Purchase Requisition Register

**Report id:** `cat-purchase-requisition-register`

**Parameters:**

- **From** (`from`) - date, optional
- **To** (`to`) - date, optional

**Columns:** Purchase Requisition No · Document Date · Store Code · Store Name · Item Code · Item Name · Category Name · Qty · Reason · Approval Status · Created By · Approved By

**Drill-down:** no.

### R2017 Purchase Return Itemwise Detail

**Report id:** `cat-purchase-return-itemwise-detail`

**Parameters:**

- **From** (`from`) - date, optional
- **To** (`to`) - date, optional
- **Store** (`location`) - text, optional

**Columns:** Document Date · Document No · Return From Outlet · Transaction Source Location · Transaction Destination Location · Vendor Code · Vendor Name · Item Code · Item Name · SKU · Barcode · Design No · Combination ID · Category Name · Qty · GRN No · Purchase Price *(sensitive)* · Taxable Amount · GST Amount · Net Amount · Reason

**Drill-down:** no.

### R2016 Purchase Return Register

**Report id:** `cat-purchase-return-register`

**Parameters:**

- **From** (`from`) - date, optional
- **To** (`to`) - date, optional
- **Store** (`location`) - text, optional

**Columns:** Document Date · Document No · Return From Outlet · Transaction Source Location · Transaction Destination Location · Vendor Code · Vendor Name · Qty · Taxable Amount · GST Amount · Net Amount · Debit Note No · Status

**Drill-down:** no.

### R2002 RFQ / Quotation Comparison

**Report id:** `cat-rfq-quotation-comparison`

**Parameters:**

- **From** (`from`) - date, optional
- **To** (`to`) - date, optional

**Columns:** RFQ No · Document Date · Item Code · Item Name · Vendor Code · Vendor Name · Quotation No · PO Qty · PO Rate · Landed Cost *(sensitive)* · GST % · PO Gross Value · Status

**Drill-down:** no.

### R2019 Vendor Credit Note Register

**Report id:** `cat-vendor-credit-note-register`

**Parameters:**

- **From** (`from`) - date, optional
- **To** (`to`) - date, optional
- **Store** (`location`) - text, optional

**Columns:** Document Date · Credit Note No (Vendor) · Vendor Code · Vendor Name · GRN No · Vendor Invoice No · Taxable Amount · GST Amount · Net Amount · Reason · Status

**Drill-down:** no.

### R2018 Vendor Debit Note Register

**Report id:** `cat-vendor-debit-note-register`

**Parameters:**

- **From** (`from`) - date, optional
- **To** (`to`) - date, optional
- **Store** (`location`) - text, optional

**Columns:** Document Date · Debit Note No · Vendor Code · Vendor Name · GRN No · Vendor Invoice No · Taxable Amount · GST Amount · Net Amount · Reason · Status

**Drill-down:** no.

### R2013 Vendor Performance Summary

**Report id:** `cat-vendor-performance-summary`

**Parameters:**

- **From** (`from`) - date, optional
- **To** (`to`) - date, optional
- **Store** (`location`) - text, optional
- **Status (optional)** (`status`) - select, optional, one of: ,Draft,Pending Approval,Approved,Partially Received,Received,Closed,Rejected,Cancelled

**Columns:** From Date · To Date · Vendor Code · Vendor Name · PO Qty · Received Qty · Rejected Qty · Pending Qty · Amount Variance · Status

**Drill-down:** no.

## Sales

### R1026 Competitor Price Gap

**Report id:** `competitor-price-gap`

**Parameters:**

- **Platform (optional)** (`platform`) - select, optional, one of: Amazon,Flipkart,Myntra,Meesho,Ajio,Nykaa,Tata Cliq,JioMart,eBay,Own Website,Other
- **Our SKU (optional)** (`sku`) - text, optional
- **Observed on or after (optional)** (`since`) - date, optional
- **We are (optional)** (`position`) - select, optional, one of: Above,At,Below,No price on file

**Columns:** Our SKU · Item · Our Price · Our Price From · Best Competitor Price · Platform · Gap (₹) · Gap (%) · We Are · Observed On · Observations · Source

**Drill-down:** no.

### R1028 Customer Advance Register

**Report id:** `advance-register`

**Parameters:**

- **Store code (blank = all)** (`store`) - text, optional
- **Open only** (`open`) - select, optional, one of: ,Yes

**Columns:** Receipt voucher · Taken · Store · Customer · For · Expected · Taken · Used · Refunded · Balance · Status · Paid by

**Drill-down:** no.

### R1024 Customer Ledger

**Report id:** `customer-ledger`

**Parameters:**

- **Customer (optional)** (`customer`) - text, optional

**Columns:** Invoice ID · Customer · Invoice Number · Total Amount *(sensitive)* · Status · Date

**Drill-down:** no.

### R1032 Customer advances outstanding (ageing)

**Report id:** `advance-outstanding-ageing`

**Parameters:** none - it runs as soon as you pick it.

**Columns:** Receipt voucher · Customer · Store · Taken · Expected · Balance · Age (days) · Age

**Drill-down:** no.

### R1025 Gift With Purchase Register

**Report id:** `gwp-issue-register`

**Parameters:**

- **From** (`from`) - date, optional
- **To** (`to`) - date, optional

**Columns:** Date · Bill · Store · Customer · Gift · Qty · Offer

**Drill-down:** no.

### R1031 Post-Sale Edit History

**Report id:** `post-sale-edit-history`

**Parameters:**

- **From** (`from_date`) - date, optional
- **To** (`to_date`) - date, optional
- **Document type** (`document_type`) - select, optional, one of: ,Sales Invoice,Sales Order,Till bill
- **Number** (`document_id`) - text, optional

**Columns:** When · Document · Number · Status then · Field · Before · After · Changed by · Reason

**Drill-down:** no.

### R1027 Sales Register

**Report id:** `sales-register`

**Parameters:** none - it runs as soon as you pick it.

**Columns:** Cart Number · Location · Payment Mode · Status · Sale Total *(sensitive)* · Date · Data Issue

**Drill-down:** no.

### R1023 Sales by Customer Reference

**Report id:** `sales-by-customer-reference`

**Parameters:**

- **From** (`from_date`) - date, optional
- **To** (`to_date`) - date, optional
- **Customer PO / Ref** (`customer_reference`) - text, optional
- **Customer** (`customer`) - text, optional

**Columns:** Customer PO / Ref · Document · Number · Date · Customer · From order · Amount · Status

**Drill-down:** no.

### R1030 Salesman-wise Sales

**Report id:** `salesman-sales`

**Parameters:**

- **Store code (blank = all)** (`store`) - text, optional
- **From** (`from`) - date, optional
- **To** (`to`) - date, optional

**Columns:** Salesman · Bills · Sales

**Drill-down:** no.

### R1029 Tender-wise Sales

**Report id:** `tender-sales`

**Parameters:**

- **Store code (blank = all)** (`store`) - text, optional
- **From** (`from`) - date, optional
- **To** (`to`) - date, optional

**Columns:** Tender · Amount

**Drill-down:** no.

## Sales & POS

### R1019 B2B Sales Register

**Report id:** `cat-b2b-sales-register`

**Parameters:**

- **From** (`from`) - date, optional
- **To** (`to`) - date, optional
- **Store** (`location`) - text, optional
- **Status (optional)** (`status`) - select, optional, one of: ,Approved,Posted,Paid,Overdue

**Columns:** Legal Entity · Store Code · Store Name · Sales Invoice Date · Sales Invoice No · GST Bill No · Customer ID · Customer Name · Customer GSTIN · PO Number · Place of Supply · Taxable Amount · CGST Amount · SGST Amount · IGST Amount · Bill Net Amount · Status

**Drill-down:** no.

### R1013 Cancelled / Void Bill Register

**Report id:** `cat-cancelled-void-bill-register`

**Parameters:**

- **From** (`from`) - date, optional
- **To** (`to`) - date, optional
- **Store** (`location`) - text, optional

**Columns:** Legal Entity · Store Code · Store Name · Sales Invoice Date · Sales Invoice No · Customer ID · Customer Name · Bill Net Amount · Reason · User ID · User Name · Approved By · Approved At · Status

**Drill-down:** no.

### R1017 Credit Note Register

**Report id:** `cat-credit-note-register`

**Parameters:**

- **From** (`from`) - date, optional
- **To** (`to`) - date, optional
- **Store** (`location`) - text, optional
- **Status (optional)** (`status`) - select, optional, one of: ,Draft,Approved,Posted,Applied,Cancelled

**Columns:** Legal Entity · Store Code · Store Name · Document Date · Credit Note No · Original Bill No · Customer ID · Customer Name · Credit Note Amount · Status · Sales Invoice No · Created By

**Drill-down:** no.

### R1015 Customer Advance Itemwise Detail

**Report id:** `cat-customer-advance-itemwise-detail`

**Parameters:**

- **From** (`from`) - date, optional
- **To** (`to`) - date, optional
- **Store** (`location`) - text, optional

**Columns:** Legal Entity · Store Code · Store Name · Advance Date · Advance No · Customer ID · Customer Name · Customer Mobile · Item Code · Item Name · SKU · Barcode · Design No · Combination ID · Category Name · Qty · MRP · Advance Amount · Advance Adjusted Amount · Advance Balance Amount · Advance Status · Sales Invoice No · POS Bill Status · POS Invoice No

**Drill-down:** no.

### R1014 Customer Advance Summary

**Report id:** `cat-customer-advance-summary`

**Parameters:**

- **From** (`from`) - date, optional
- **To** (`to`) - date, optional
- **Store** (`location`) - text, optional

**Columns:** Legal Entity · Store Code · Store Name · Advance Date · Advance No · Customer ID · Customer Name · Customer Mobile · Advance Amount · Advance Adjusted Amount · Advance Balance Amount · Advance Status · Sales Invoice No · Created By

**Drill-down:** no.

### R1016 Customer Advance Utilization

**Report id:** `cat-customer-advance-utilization`

**Parameters:**

- **From** (`from`) - date, optional
- **To** (`to`) - date, optional
- **Store** (`location`) - text, optional

**Columns:** Legal Entity · Store Code · Store Name · Advance No · Customer ID · Customer Name · Sales Invoice Date · Sales Invoice No · Advance Adjusted Amount · Advance Balance Amount · Status · Created By

**Drill-down:** no.

### R1020 Export / International Sales Register

**Report id:** `cat-export-international-sales-register`

**Parameters:**

- **From** (`from`) - date, optional
- **To** (`to`) - date, optional
- **Store** (`location`) - text, optional
- **Status (optional)** (`status`) - select, optional, one of: ,Approved,Posted,Paid,Overdue

**Columns:** Legal Entity · Store Code · Store Name · Sales Invoice Date · Sales Invoice No · Customer ID · Customer Name · Sales Channel · Place of Supply · Tax Type · Gross Amount · Taxable Amount · IGST Amount · Bill Net Amount · Status

**Drill-down:** no.

### R1018 Gift Card / Voucher Register

**Report id:** `cat-gift-card-voucher-register`

**Parameters:**

- **From** (`from`) - date, optional
- **To** (`to`) - date, optional
- **Store** (`location`) - text, optional

**Columns:** Legal Entity · Store Code · Store Name · Document Date · Gift Card No · Customer ID · Customer Name · Gift Card Amount · Sales Invoice No · Tender Type · Status · Created By

**Drill-down:** no.

### R1005 Net Sales Summary

**Report id:** `cat-net-sales-summary`

**Parameters:**

- **From** (`from`) - date, optional
- **To** (`to`) - date, optional
- **Store** (`location`) - text, optional

**Columns:** Document Date · Store Code · Store Name · Sales Channel · Bill Count · Sold Qty · Return Qty · Net Sales Qty · Sales Amount · Sales Return Amount · Net Sales Amount · Discount Amount · GST Amount

**Drill-down:** no.

### R1012 Sales Bill Edit Audit Report

**Report id:** `cat-sales-bill-edit-audit-report`

**Parameters:**

- **From** (`from`) - date, optional
- **To** (`to`) - date, optional
- **Store** (`location`) - text, optional

**Columns:** Legal Entity · Store Code · Store Name · Sales Invoice Date · Sales Invoice No · Audit ID · Edit Action · Field Changed · Old Value · New Value · Edit Reason · Edited By · Edited At

**Drill-down:** no.

### R1009 Sales Counter Summary

**Report id:** `cat-sales-counter-summary`

**Parameters:**

- **From** (`from`) - date, optional
- **To** (`to`) - date, optional
- **Store** (`location`) - text, optional

**Columns:** From Date · To Date · Store Code · Store Name · Counter · Bill Count · Sold Qty · Return Qty · Net Sales Amount · Tender Amount

**Drill-down:** no.

### R1021 Sales Design Image Register

**Report id:** `cat-sales-design-image-register`

**Parameters:**

- **From** (`from`) - date, optional
- **To** (`to`) - date, optional
- **Store** (`location`) - text, optional

**Columns:** Legal Entity · Store Code · Store Name · Sales Invoice Date · Sales Invoice No · Design No · Item Name · Category Name · Sold Qty · Image URL

**Drill-down:** no.

### R1010 Sales Discount Analysis

**Report id:** `cat-sales-discount-analysis`

**Parameters:**

- **From** (`from`) - date, optional
- **To** (`to`) - date, optional
- **Store** (`location`) - text, optional

**Columns:** Legal Entity · Store Code · Store Name · Sales Invoice Date · Sales Invoice No · Customer ID · Customer Name · Item Code · Item Name · SKU · Barcode · Design No · Combination ID · Category Name · MRP · Gross Value · Customer Redeem Points · Item Discount · Bill Discount · Discount Amount · Item Taxable Amount · GST Amount · Item Net Amount

**Drill-down:** no.

### R1002 Sales Itemwise Detail

**Report id:** `cat-sales-itemwise-detail`

**Parameters:**

- **From** (`from`) - date, optional
- **To** (`to`) - date, optional
- **Store** (`location`) - text, optional

**Columns:** Legal Entity · Store Code · Store Name · Sales Invoice Date · Sales Invoice No · GST Bill No · Customer ID · Customer Name · Sales Channel · Item Code · Item Name · SKU · Barcode · Design No · Combination ID · Category Name · Sold Qty · MRP · Gross Value · Customer Redeem Points · Item Discount · Bill Discount · Packing Charges · Item Taxable Amount · GST % · GST Amount · Item Net Amount · Salesperson Name

**Drill-down:** no.

### R1011 Sales Margin Analysis

**Report id:** `cat-sales-margin-analysis`

**Parameters:**

- **From** (`from`) - date, optional
- **To** (`to`) - date, optional
- **Store** (`location`) - text, optional

**Columns:** Legal Entity · Store Code · Store Name · Sales Invoice Date · Sales Invoice No · Item Code · Item Name · SKU · Barcode · Design No · Combination ID · Category Name · Sold Qty · Item Net Amount · Purchase Price *(sensitive)* · Landed Cost *(sensitive)* · Margin Amount *(sensitive)* · Margin % *(sensitive)*

**Drill-down:** no.

### R1001 Sales Register

**Report id:** `cat-sales-register`

**Parameters:**

- **From** (`from`) - date, optional
- **To** (`to`) - date, optional
- **Store** (`location`) - text, optional
- **Status (optional)** (`status`) - select, optional, one of: ,Approved,Posted,Paid,Overdue

**Columns:** Legal Entity · Store Code · Store Name · Sales Invoice Date · Sales Invoice No · GST Bill No · Customer ID · Customer Name · Customer Mobile · Sales Channel · Customer GSTIN · PO Number · Bill Count · Gross Amount · Discount Amount · Taxable Amount · GST Amount · Bill Net Amount · Tender Type · Salesperson Name · Status

**Drill-down:** no.

### R1004 Sales Return Itemwise Detail

**Report id:** `cat-sales-return-itemwise-detail`

**Parameters:**

- **From** (`from`) - date, optional
- **To** (`to`) - date, optional
- **Store** (`location`) - text, optional

**Columns:** Legal Entity · Store Code · Store Name · Sales Return Date · Sales Return No · Original Bill No · Customer ID · Customer Name · Item Code · Item Name · SKU · Barcode · Design No · Combination ID · Category Name · Return Qty · MRP · Gross Value · Customer Redeem Points · Item Discount · Packing Charges · Item Taxable Amount · GST % · GST Amount · Item Net Amount · Reason

**Drill-down:** no.

### R1003 Sales Return Register

**Report id:** `cat-sales-return-register`

**Parameters:**

- **From** (`from`) - date, optional
- **To** (`to`) - date, optional
- **Store** (`location`) - text, optional

**Columns:** Legal Entity · Store Code · Store Name · Sales Return Date · Sales Return No · GST Bill No · Original Bill No · Customer ID · Customer Name · Sales Channel · Return Qty · Taxable Amount · GST Amount · Net Amount · Credit Note No · Status

**Drill-down:** no.

### R1007 Sales Tender Detail

**Report id:** `cat-sales-tender-detail`

**Parameters:**

- **From** (`from`) - date, optional
- **To** (`to`) - date, optional
- **Store** (`location`) - text, optional

**Columns:** Legal Entity · Store Code · Store Name · Sales Invoice Date · Sales Invoice No · Customer Name · Tender Type · Tender Amount · Reference No · Bill Net Amount · Status

**Drill-down:** no.

### R1006 Sales Tender Summary

**Report id:** `cat-sales-tender-summary`

**Parameters:**

- **From** (`from`) - date, optional
- **To** (`to`) - date, optional
- **Store** (`location`) - text, optional

**Columns:** Document Date · Store Code · Store Name · Tender Type · Bill Count · Tender Amount · Net Sales Amount

**Drill-down:** no.

### R1022 Sales Variant Image Register

**Report id:** `cat-sales-variant-image-register`

**Parameters:**

- **From** (`from`) - date, optional
- **To** (`to`) - date, optional
- **Store** (`location`) - text, optional

**Columns:** Legal Entity · Store Code · Store Name · Sales Invoice Date · Sales Invoice No · Design No · Combination ID · SKU · Item Name · Sold Qty · Image URL

**Drill-down:** no.

### R1008 Salesman Sales Summary

**Report id:** `cat-salesman-sales-summary`

**Parameters:**

- **From** (`from`) - date, optional
- **To** (`to`) - date, optional
- **Store** (`location`) - text, optional

**Columns:** From Date · To Date · Store Code · Store Name · Salesperson ID · Salesperson Name · Bill Count · Sold Qty · Net Sales Amount · ABV · UPT · Discount Amount

**Drill-down:** no.

## Service

### R6072 Service SLA Breaches

**Report id:** `service-sla-breaches`

**Parameters:** none - it runs as soon as you pick it.

**Columns:** Ticket · Customer · Status · Breach Type · Respond By · Resolve By

**Drill-down:** no.

## Stock Transfer

### R3042 Transfer Approval / Cancellation Audit

**Report id:** `cat-transfer-approval-cancellation-audit`

**Parameters:**

- **From** (`from`) - date, optional
- **To** (`to`) - date, optional
- **Store** (`location`) - text, optional

**Columns:** Transfer Out No · Document Date · From Store · To Store · Edit Action · Reason · User ID · User Name · Approved By · Approved At · Status

**Drill-down:** no.

### R3039 Transfer Barcode Traceability

**Report id:** `cat-transfer-barcode-traceability`

**Parameters:**

- **From** (`from`) - date, optional
- **To** (`to`) - date, optional
- **Store** (`location`) - text, optional
- **Status (optional)** (`status`) - select, optional, one of: ,Draft,Pending Approval,Approved,Dispatched,In Transit,Partially Received,Received,Closed,Cancelled

**Columns:** Barcode · SKU · Design No · Combination ID · Transfer Out No · Transfer Out Date · Transfer In No · Transfer In Date · From Store · To Store · Transfer Status

**Drill-down:** no.

### R3040 Transfer GST / E-Way Control

**Report id:** `cat-transfer-gst-e-way-control`

**Parameters:**

- **From** (`from`) - date, optional
- **To** (`to`) - date, optional
- **Store** (`location`) - text, optional
- **Status (optional)** (`status`) - select, optional, one of: ,Draft,Pending Approval,Approved,Dispatched,In Transit,Partially Received,Received,Closed,Cancelled

**Columns:** Transfer Out Date · Transfer Out No · From Store · To Store · Taxable Amount · GST Amount · IRN · IRN Status · E-Way Bill No · E-Way Status · Status

**Drill-down:** no.

### R3037 Transfer In Itemwise Detail

**Report id:** `cat-transfer-in-itemwise-detail`

**Parameters:**

- **From** (`from`) - date, optional
- **To** (`to`) - date, optional
- **Store** (`location`) - text, optional

**Columns:** Transfer In Date · Transfer In No · Transfer Out No · From Store · To Store · Item Code · Item Name · SKU · Barcode · Design No · Combination ID · Category Name · Qty · Receipt Date · Transfer Status · Reason · Remarks

**Drill-down:** no.

### R3036 Transfer In Register

**Report id:** `cat-transfer-in-register`

**Parameters:**

- **From** (`from`) - date, optional
- **To** (`to`) - date, optional
- **Store** (`location`) - text, optional

**Columns:** Transfer In Date · Transfer In No · Transfer Out No · From Store · To Store · Qty · Receipt Date · Transfer Status · Created By

**Drill-down:** no.

### R3038 Transfer In-Transit Aging

**Report id:** `cat-transfer-in-transit-aging`

**Parameters:**

- **From** (`from`) - date, optional
- **To** (`to`) - date, optional
- **Store** (`location`) - text, optional
- **Status (optional)** (`status`) - select, optional, one of: ,Draft,Pending Approval,Approved,Dispatched,In Transit,Partially Received,Received,Closed,Cancelled

**Columns:** Transfer Out Date · Transfer Out No · From Store · To Store · Dispatch Date · In-Transit Days · Qty · In-Transit Qty · Transfer Status

**Drill-down:** no.

### R3035 Transfer Out Itemwise Detail

**Report id:** `cat-transfer-out-itemwise-detail`

**Parameters:**

- **From** (`from`) - date, optional
- **To** (`to`) - date, optional
- **Store** (`location`) - text, optional
- **Status (optional)** (`status`) - select, optional, one of: ,Draft,Pending Approval,Approved,Dispatched,In Transit,Partially Received,Received,Closed,Cancelled

**Columns:** Transfer Out Date · Transfer Out No · From Store · To Store · Item Code · Item Name · SKU · Barcode · Design No · Combination ID · Category Name · Qty · Transfer Status · Dispatch Date · E-Way Bill No · IRN

**Drill-down:** no.

### R3034 Transfer Out Register

**Report id:** `cat-transfer-out-register`

**Parameters:**

- **From** (`from`) - date, optional
- **To** (`to`) - date, optional
- **Store** (`location`) - text, optional
- **Status (optional)** (`status`) - select, optional, one of: ,Draft,Pending Approval,Approved,Dispatched,In Transit,Partially Received,Received,Closed,Cancelled

**Columns:** Transfer Out Date · Transfer Out No · From Store · To Store · Qty · Transfer Status · Dispatch Date · E-Way Bill No · IRN · Created By

**Drill-down:** no.

### R3041 Transfer Shortage / Excess Exception

**Report id:** `cat-transfer-shortage-excess-exception`

**Parameters:**

- **From** (`from`) - date, optional
- **To** (`to`) - date, optional
- **Store** (`location`) - text, optional

**Columns:** Transfer Out No · Transfer In No · From Store · To Store · Item Code · Item Name · SKU · Barcode · Design No · Combination ID · Category Name · Qty · Received Qty · Difference Qty · Reason · Remarks · Status

**Drill-down:** no.

## WMS

### R4033 3PL Storage & Handling Billing

**Report id:** `3pl-storage-billing`

**Parameters:**

- **Owner (optional)** (`owner_id`) - text, optional
- **Period Start** (`start`) - date, **required**
- **Period End** (`end`) - date, **required**

**Columns:** Owner · Location · Current Units (EA) · Days · Billing UOM · Billing Units · Storage Charge *(sensitive)* · Handling Tasks · Handling Charge *(sensitive)* · Total *(sensitive)*

**Drill-down:** no.

### R4043 3PL Storage Billing v2

**Report id:** `storage-billing-v2`

**Parameters:**

- **Owner (optional)** (`owner_id`) - text, optional
- **Period Start** (`start`) - date, **required**
- **Period End** (`end`) - date, **required**

**Columns:** Rate · Owner · Location · Item · Snapshot Days · Average Units · Net *(sensitive)* · Tax *(sensitive)* · Total *(sensitive)*

**Drill-down:** no.

### R4024 Appointment Calendar Report

**Report id:** `cat-appointment-calendar-report`

**Parameters:**

- **From** (`from`) - date, optional
- **To** (`to`) - date, optional
- **Store** (`location`) - text, optional
- **Status (optional)** (`status`) - select, optional, one of: ,Scheduled,Checked In,At Door,Completed,No Show,Cancelled

**Columns:** Document Date · Appointment No · Vendor Name · Dock Door · Vehicle No · Qty · Status

**Drill-down:** no.

### R4032 Batch Movement History (Recall)

**Report id:** `batch-movement-history`

**Parameters:**

- **Batch / Lot** (`batch_no`) - text, **required**
- **Item Code (optional)** (`sku`) - text, optional

**Columns:** When · Item · Batch / Lot · Qty · Movement · Document · Location · From Bin · To Bin · By

**Drill-down:** no.

### R4030 Batch Near-Expiry Watchlist

**Report id:** `batch-near-expiry`

**Parameters:**

- **Within (days, default 30)** (`within_days`) - text, optional

**Columns:** Item · Batch / Lot · Expires · Days Left · Batch Status · Qty On Hand · Locations

**Drill-down:** no.

### R4031 Batch Stock Inquiry

**Report id:** `batch-stock-inquiry`

**Parameters:**

- **Item Code (optional)** (`sku`) - text, optional
- **Location (optional)** (`location`) - text, optional
- **Batch / Lot (optional)** (`batch_no`) - text, optional

**Columns:** Item · Batch / Lot · Location · Bin · Condition · Qty · Expires · Days Left · Batch Status

**Drill-down:** no.

### R4006 Bin Inventory Report

**Report id:** `cat-bin-inventory-report`

**Parameters:**

- **Store** (`location`) - text, optional

**Columns:** As Of Date · Bin Code · Bin Condition · SKU · Barcode · Available Qty · Blocked Qty · LPN · Stock Age Days

**Drill-down:** no.

### R4012 Bin Replenishment Report

**Report id:** `cat-bin-replenishment-report`

**Parameters:**

- **From** (`from`) - date, optional
- **To** (`to`) - date, optional
- **Store** (`location`) - text, optional

**Columns:** Document Date · Document No · SKU · From Location · To Location · Qty · Status · RF User · Task Duration Min

**Drill-down:** no.

### R4020 Bin Stock Aging

**Report id:** `cat-bin-stock-aging`

**Parameters:**

- **Store** (`location`) - text, optional

**Columns:** As Of Date · Bin Code · SKU · Item Name · Available Qty · Stock Age Days · Age Bucket · MRP Value

**Drill-down:** no.

### R4034 Bin Stock Reconciliation

**Report id:** `bin-stock-reconciliation`

**Parameters:**

- **Item Code (optional)** (`sku`) - text, optional
- **Location (optional)** (`location`) - text, optional
- **Show** (`mismatches_only`) - select, optional, one of: Mismatches only,All rows

**Columns:** Location · Item · On Hand · In Bins · Picked, Not Dispatched · Difference · Status

**Drill-down:** no.

### R4010 Bin Utilization / Condition Report

**Report id:** `cat-bin-utilization-condition-report`

**Parameters:**

- **Store** (`location`) - text, optional
- **Status (optional)** (`status`) - select, optional, one of: ,Active,Inactive,Blocked

**Columns:** As Of Date · Bin Code · Bin Condition · Available Qty · Blocked Qty · Status · Remarks

**Drill-down:** no.

### R4049 Clean-Location Consolidation Suggestions

**Report id:** `consolidation-suggestions`

**Parameters:**

- **Location** (`location_code`) - text, **required**

**Columns:** SKU · Action · From Bin · To Bin · Qty · Reason

**Drill-down:** no.

### R4035 Cross-Facility Inventory Inquiry

**Report id:** `cross-facility-stock`

**Parameters:**

- **SKU** (`sku`) - text, **required**

**Columns:** SKU · Location · On Hand · Available · In Transit

**Drill-down:** no.

### R4018 Cycle Count Register

**Report id:** `cat-cycle-count-register`

**Parameters:**

- **From** (`from`) - date, optional
- **To** (`to`) - date, optional
- **Store** (`location`) - text, optional

**Columns:** Document Date · Cycle Count No · Bin Code · SKU · System Qty · Physical Qty · Difference Qty · Status · User Name

**Drill-down:** no.

### R4019 Cycle Count Variance Report

**Report id:** `cat-cycle-count-variance-report`

**Parameters:**

- **From** (`from`) - date, optional
- **To** (`to`) - date, optional
- **Store** (`location`) - text, optional

**Columns:** Document Date · Cycle Count No · Bin Code · SKU · Barcode · System Qty · Physical Qty · Difference Qty · Reason · Approval Status

**Drill-down:** no.

### R4017 Dispatch / Shipment Register

**Report id:** `cat-dispatch-shipment-register`

**Parameters:**

- **From** (`from`) - date, optional
- **To** (`to`) - date, optional
- **Store** (`location`) - text, optional

**Columns:** Document Date · Order ID · Shipment ID · Dispatch Qty · Tracking Number · Shipping Provider · Vehicle No · Dock Door · Status

**Drill-down:** no.

### R4023 Dock Door Utilization

**Report id:** `cat-dock-door-utilization`

**Parameters:**

- **From** (`from`) - date, optional
- **To** (`to`) - date, optional
- **Store** (`location`) - text, optional
- **Status (optional)** (`status`) - select, optional, one of: ,Scheduled,Checked In,At Door,Completed,No Show,Cancelled

**Columns:** Document Date · Dock Door · Appointment No · Vendor Name · Vehicle No · SLA Due At · Status

**Drill-down:** no.

### R4036 Facility Roll-Up Inventory Inquiry

**Report id:** `facility-rollup`

**Parameters:**

- **Facility (Location code)** (`facility_code`) - text, **required**

**Columns:** SKU · On Hand · Available · In Transit · Locations

**Drill-down:** no.

### R4001 Inbound / ASN Register

**Report id:** `cat-inbound-asn-register`

**Parameters:**

- **From** (`from`) - date, optional
- **To** (`to`) - date, optional
- **Store** (`location`) - text, optional

**Columns:** Document Date · Document No · Vendor Code · Vendor Name · Appointment No · Dock Door · Qty · Status

**Drill-down:** no.

### R4003 Inbound QC Exception

**Report id:** `cat-inbound-qc-exception`

**Parameters:**

- **From** (`from`) - date, optional
- **To** (`to`) - date, optional
- **Store** (`location`) - text, optional

**Columns:** GRN Date · GRN No · SKU · Barcode · Received Qty · Rejected Qty · QC Result · Reason · Status

**Drill-down:** no.

### R4011 LPN / Carton / Pallet Register

**Report id:** `cat-lpn-carton-pallet-register`

**Parameters:**

- **From** (`from`) - date, optional
- **To** (`to`) - date, optional

**Columns:** Document Date · LPN · Carton No · Pallet No · Location Name · Qty · Status · Created By

**Drill-down:** no.

### R4046 Labor Productivity

**Report id:** `labor-productivity`

**Parameters:**

- **From (optional)** (`start`) - date, optional
- **To (optional)** (`end`) - date, optional

**Columns:** User · Task Type · Tasks · Tasks/Hour · First Task · Last Task

**Drill-down:** no.

### R4042 Labour Cost

**Report id:** `labor-cost`

**Parameters:**

- **From (optional)** (`start`) - date, optional
- **To (optional)** (`end`) - date, optional

**Columns:** Location · Tasks · Quantity · Standard Hours · Labour Cost *(sensitive)*

**Drill-down:** no.

### R4037 Labour Enterprise Productivity

**Report id:** `labor-enterprise-productivity`

**Parameters:**

- **From (optional)** (`start`) - date, optional
- **To (optional)** (`end`) - date, optional

**Columns:** Scope · Tasks · Quantity · Standard Hours · Labour Cost *(sensitive)*

**Drill-down:** no.

### R4038 Labour Facility Performance

**Report id:** `labor-facility-performance`

**Parameters:**

- **From (optional)** (`start`) - date, optional
- **To (optional)** (`end`) - date, optional

**Columns:** Location · Tasks · Quantity · Standard Hours · Labour Cost *(sensitive)*

**Drill-down:** no.

### R4041 Labour Standards Audit

**Report id:** `labor-standards-audit`

**Parameters:** none - it runs as soon as you pick it.

**Columns:** Standard · Operation · Element Seconds · Travel Seconds · Allowance Seconds · Total Seconds

**Drill-down:** no.

### R4039 Labour Task Productivity

**Report id:** `labor-task-productivity`

**Parameters:**

- **From (optional)** (`start`) - date, optional
- **To (optional)** (`end`) - date, optional

**Columns:** Task Type · Tasks · Quantity · Standard Hours · Labour Cost *(sensitive)*

**Drill-down:** no.

### R4040 Labour User Performance

**Report id:** `labor-user-performance`

**Parameters:**

- **From (optional)** (`start`) - date, optional
- **To (optional)** (`end`) - date, optional

**Columns:** User · Tasks · Quantity · Standard Hours · Labour Cost *(sensitive)*

**Drill-down:** no.

### R4007 Location Movement Register

**Report id:** `cat-location-movement-register`

**Parameters:**

- **From** (`from`) - date, optional
- **To** (`to`) - date, optional
- **Store** (`location`) - text, optional

**Columns:** Document No · Document Date · Warehouse · SKU · Barcode · From Location · To Location · Qty · Status · Created By

**Drill-down:** no.

### R4021 Lot / Serial / Barcode Traceability

**Report id:** `cat-lot-serial-barcode-traceability`

**Parameters:**

- **From** (`from`) - date, optional
- **To** (`to`) - date, optional
- **Store** (`location`) - text, optional

**Columns:** Barcode · SKU · LPN · Bin Code · Movement Date · Movement Type · Source Document No · RF User · Status

**Drill-down:** no.

### R4044 Owner Stock Exceptions (3PL)

**Report id:** `owner-stock-exceptions`

**Parameters:** none - it runs as soon as you pick it.

**Columns:** Exception · Location · Bin · Item · Condition · Bin Qty · Owner Qty · What to do

**Drill-down:** no.

### R4045 Owner Stock Inquiry (3PL)

**Report id:** `owner-stock-inquiry`

**Parameters:**

- **Item Code (optional)** (`sku`) - text, optional
- **Location (optional)** (`location`) - text, optional
- **Owner (optional)** (`owner_id`) - text, optional

**Columns:** Owner · Item · Bin · Location · Condition · Qty

**Drill-down:** no.

### R4016 Packing Report

**Report id:** `cat-packing-report`

**Parameters:**

- **From** (`from`) - date, optional
- **To** (`to`) - date, optional
- **Store** (`location`) - text, optional

**Columns:** Document Date · Order ID · Shipment ID · Carton No · LPN · Picked Qty · Packed Qty · User Name · Status

**Drill-down:** no.

### R4013 Pick List Report

**Report id:** `cat-pick-list-report`

**Parameters:**

- **From** (`from`) - date, optional
- **To** (`to`) - date, optional
- **Store** (`location`) - text, optional

**Columns:** Document Date · Pick List No · Order ID · Shipment ID · SKU · Bin Code · Qty · Picked Qty · RF User · Status

**Drill-down:** no.

### R4015 Picking Exception Report

**Report id:** `cat-picking-exception-report`

**Parameters:**

- **From** (`from`) - date, optional
- **To** (`to`) - date, optional
- **Store** (`location`) - text, optional

**Columns:** Document Date · Pick List No · Order ID · SKU · Bin Code · Qty · Picked Qty · Variance Qty · Reason · Status

**Drill-down:** no.

### R4005 Putaway Completion Report

**Report id:** `cat-putaway-completion-report`

**Parameters:**

- **From** (`from`) - date, optional
- **To** (`to`) - date, optional
- **Store** (`location`) - text, optional

**Columns:** Document Date · Document No · SKU · Barcode · LPN · Bin Code · Qty · RF User · Task Duration Min · Putaway Status

**Drill-down:** no.

### R4004 Putaway Pending Report

**Report id:** `cat-putaway-pending-report`

**Parameters:**

- **From** (`from`) - date, optional
- **To** (`to`) - date, optional
- **Store** (`location`) - text, optional

**Columns:** Document Date · Document No · SKU · Barcode · LPN · From Location · To Location · Putaway Status · SLA Age Hours

**Drill-down:** no.

### R4022 RF / Mobile Picking Productivity

**Report id:** `cat-rf-mobile-picking-productivity`

**Parameters:**

- **From** (`from`) - date, optional
- **To** (`to`) - date, optional
- **Store** (`location`) - text, optional

**Columns:** From Date · To Date · RF User · Pick List No · Picked Qty · Task Duration Min · Achievement % · Status

**Drill-down:** no.

### R4002 Receiving / GRN Detail

**Report id:** `cat-receiving-grn-detail`

**Parameters:**

- **From** (`from`) - date, optional
- **To** (`to`) - date, optional
- **Store** (`location`) - text, optional

**Columns:** GRN Date · GRN No · Vendor Name · SKU · Barcode · LPN · Received Qty · Accepted Qty · Rejected Qty · Dock Door · Status

**Drill-down:** no.

### R4029 Serial Movement History

**Report id:** `serial-movement-history`

**Parameters:**

- **Serial Number** (`serial_no`) - text, **required**
- **Item Code (optional)** (`sku`) - text, optional

**Columns:** When · Item · Serial Number · Movement · Document · Location · From · To · By

**Drill-down:** no.

### R4028 Serial Number Inquiry

**Report id:** `serial-inquiry`

**Parameters:**

- **Item Code (optional)** (`sku`) - text, optional
- **Serial Number (optional)** (`serial_no`) - text, optional
- **Status (optional)** (`status`) - select, optional, one of: InStock,Allocated,Shipped,Returned,Scrapped

**Columns:** Item · Serial Number · Batch / Lot · Status · Current Bin · Location · Reserved For · Supplier

**Drill-down:** no.

### R4047 Slotting / Re-Slotting Suggestions

**Report id:** `slotting-suggestions`

**Parameters:**

- **Location** (`location_code`) - text, **required**

**Columns:** SKU · Velocity Tier · Daily Velocity · Suggested Action · From Bin · To Bin · Qty · Reason

**Drill-down:** no.

### R4048 Unslotting Suggestions (discontinued items)

**Report id:** `unslotting-suggestions`

**Parameters:**

- **Location** (`location_code`) - text, **required**

**Columns:** SKU · Action · From Bin · To Bin · Qty · Reason

**Drill-down:** no.

### R4008 WMS Stock Transfer Execution Report

**Report id:** `cat-wms-stock-transfer-execution-report`

**Parameters:**

- **From** (`from`) - date, optional
- **To** (`to`) - date, optional
- **Store** (`location`) - text, optional

**Columns:** Transfer Out No · Transfer Date · From Location · To Location · SKU · Barcode · Transfer Qty · Picked Qty · Packed Qty · Dispatched Qty · Status

**Drill-down:** no.

### R4009 Warehouse Cockpit KPI Summary

**Report id:** `cat-warehouse-cockpit-kpi-summary`

**Parameters:**

- **Store** (`location`) - text, optional

**Columns:** As Of Date · Warehouse · Pending Inbound Qty · Pending Putaway Qty · Pending Pick Qty · Pending Pack Qty · Pending Dispatch Qty · Exception Count

**Drill-down:** no.

### R4027 Warehouse Inventory Accuracy Summary

**Report id:** `cat-warehouse-inventory-accuracy-summary`

**Parameters:**

- **From** (`from`) - date, optional
- **To** (`to`) - date, optional
- **Store** (`location`) - text, optional

**Columns:** From Date · To Date · Location Name · Bin Code · System Qty · Physical Qty · Difference Qty · Achievement %

**Drill-down:** no.

### R4026 Warehouse SLA / Throughput Summary

**Report id:** `cat-warehouse-sla-throughput-summary`

**Parameters:**

- **From** (`from`) - date, optional
- **To** (`to`) - date, optional
- **Store** (`location`) - text, optional

**Columns:** Document Date · Document Type · Qty · Task Duration Min · SLA Age Hours · Achievement % · Status

**Drill-down:** no.

### R4014 Wave / Batch Picking Report

**Report id:** `cat-wave-batch-picking-report`

**Parameters:**

- **From** (`from`) - date, optional
- **To** (`to`) - date, optional
- **Store** (`location`) - text, optional

**Columns:** Document Date · Wave No · Order ID · Shipment ID · SKU · Qty · Picked Qty · RF User · Task Duration Min · Status

**Drill-down:** no.

### R4025 Yard / Trailer Board Report

**Report id:** `cat-yard-trailer-board-report`

**Parameters:**

- **From** (`from`) - date, optional
- **To** (`to`) - date, optional
- **Store** (`location`) - text, optional

**Columns:** Document Date · Vehicle No · Appointment No · Dock Door · Yard Status · SLA Age Hours · Status

**Drill-down:** no.

