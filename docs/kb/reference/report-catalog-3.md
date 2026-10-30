---
title: Report catalog (part 3)
section: Reference
order: 22
summary: Reports in Procurement to Stock Transfer, continued from the report catalog.
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
every category and explains how to run a report. It holds **88** reports, from
Procurement to Stock Transfer.

## Contents

- [Procurement](#procurement) - 18 reports
- [Procurement & GRN](#procurement-grn) - 27 reports
- [Sales](#sales) - 11 reports
- [Sales & POS](#sales-pos) - 22 reports
- [Service](#service) - 1 reports
- [Stock Transfer](#stock-transfer) - 9 reports

## Procurement

### R2036 GRN Corrections Log

**Report id:** `grn-corrections`

**Parameters:**

- **From** (`from`) - date, optional
- **To** (`to`) - date, optional

**Columns:** When · GRN · Line · SKU · Step · Field · Was · Now · Why · By

**Drill-down:** no.

### R2038 GRN Payments

**Report id:** `grn-payments`

**Parameters:**

- **Vendor (blank = all)** (`vendor`) - text, optional
- **Due only** (`due_only`) - select, optional, one of: ,Yes

**Columns:** GRN · Vendor · Invoice No · Invoiced *(sensitive)* · We owe *(sensitive)* · Paid *(sensitive)* · Due *(sensitive)* · Status · Last paid · Paid by

**Drill-down:** no.

### R2046 GRN Register

**Report id:** `grn-register`

**Parameters:** none - it runs as soon as you pick it.

**Columns:** GRN ID · PO Reference · Item Lines · Status · Date

**Drill-down:** no.

### R2032 GRN Register - every line, every step

**Report id:** `grn-line-register`

**Parameters:**

- **From** (`from`) - date, optional
- **To** (`to`) - date, optional
- **Vendor (blank = all)** (`vendor`) - text, optional

**Columns:** GRN · Child · Store · Vendor · Invoice No · PO · SKU · PO Qty · Received · Invoice Qty · GRN Qty · Rejected · Damaged · PO Price *(sensitive)* · Invoice Price *(sensitive)* · GRN Price *(sensitive)* · GST % · Old MRP · MRP · Batch · Step · Received · Posted

**Drill-down:** no.

### R2035 Goods Receipts Waiting

**Report id:** `grn-waiting`

**Parameters:** none - it runs as soon as you pick it.

**Columns:** GRN · Vendor · Invoice No · Waiting for · Age (days) · Invoice Total *(sensitive)* · Excess awaiting approval

**Drill-down:** no.

### R2040 Goods receipt discrepancies by vendor

**Report id:** `grn-discrepancy-by-vendor`

**Parameters:**

- **From** (`from`) - date, optional
- **To** (`to`) - date, optional

**Columns:** Vendor · Receipts · Received · Accepted · Rejected · Damaged · Short of the invoice · Invoice difference · Rejected %

**Drill-down:** no.

### R2031 Goods receipts not billed

**Report id:** `grn-not-billed`

**Parameters:** none - it runs as soon as you pick it.

**Columns:** Goods receipt · Vendor · Posted · Days · Vendor invoice no.

**Drill-down:** no.

### R2042 Goods received not invoiced (GRNI)

**Report id:** `grni`

**Parameters:**

- **From** (`from`) - date, optional
- **To** (`to`) - date, optional

**Columns:** Goods receipt · Date · Vendor · Store · Accepted value · Invoiced · Not invoiced · Price difference · Vendor invoices · AP hold

**Drill-down:** no.

### R2033 Invoice vs GRN Differences

**Report id:** `grn-invoice-differences`

**Parameters:**

- **From** (`from`) - date, optional
- **To** (`to`) - date, optional
- **Vendor (blank = all)** (`vendor`) - text, optional

**Columns:** Month · Vendor · Receipts · Invoiced *(sensitive)* · Received and verified *(sensitive)* · Difference *(sensitive)* · Debit notes *(sensitive)* · Credit notes *(sensitive)* · Notes

**Drill-down:** no.

### R2030 Late and early receipts

**Report id:** `grn-receipt-timing`

**Parameters:** none - it runs as soon as you pick it.

**Columns:** Goods receipt · PO · Vendor · Expected · Received · Timing

**Drill-down:** no.

### R2045 Open Purchase Commitments

**Report id:** `po-commitments`

**Parameters:** none - it runs as soon as you pick it.

**Columns:** Age Bucket · PO Count · Committed Amount *(sensitive)*

**Drill-down:** yes - a row's **View Details** opens the transactions behind it.

### R2039 PO expected delivery and terms

**Report id:** `po-delivery-and-terms`

**Parameters:**

- **From** (`from`) - date, optional
- **To** (`to`) - date, optional

**Columns:** PO · Vendor · Store · Status · Expected delivery · Days late · Payment term · Delivery terms · Freight paid by · Quote valid until · Amendments · Value

**Drill-down:** no.

### R2037 PO vs GRN Price Variance

**Report id:** `grn-po-variance`

**Parameters:**

- **From** (`from`) - date, optional
- **To** (`to`) - date, optional
- **Vendor (blank = all)** (`vendor`) - text, optional

**Columns:** Received · Vendor · PO · GRN · SKU · Qty · PO Price *(sensitive)* · GRN Price *(sensitive)* · Variance / unit *(sensitive)* · Variance % · Variance value *(sensitive)*

**Drill-down:** no.

### R2034 Purchase Price Changes

**Report id:** `grn-price-changes`

**Parameters:**

- **From** (`from`) - date, optional
- **To** (`to`) - date, optional
- **Vendor (blank = all)** (`vendor`) - text, optional

**Columns:** Received · SKU · Vendor · GRN · Previous Price *(sensitive)* · Previous Vendor · New Price *(sensitive)* · Change % · Old MRP · New MRP

**Drill-down:** no.

### R2047 RFQ Comparison Export

**Report id:** `rfq-comparison`

**Parameters:**

- **RFQ ID** (`rfq_id`) - text, **required**

**Columns:** Quote ID · Vendor · Quoted Price *(sensitive)* · Status

**Drill-down:** no.

### R2043 Rejections and Returns by Reason

**Report id:** `rejections-by-reason`

**Parameters:**

- **From** (`from`) - date, optional
- **To** (`to`) - date, optional
- **Vendor (blank = all)** (`vendor`) - text, optional

**Columns:** What · Reason code · Reason · Vendor · Documents · Qty

**Drill-down:** no.

### R2044 Vendor Ledger

**Report id:** `vendor-ledger`

**Parameters:**

- **Vendor (optional)** (`vendor_id`) - text, optional

**Columns:** PO ID · Vendor · PO Number · Total Amount *(sensitive)* · Status · Date

**Drill-down:** no.

### R2041 Vendor document and transport register

**Report id:** `vendor-document-transport-register`

**Parameters:**

- **From** (`from`) - date, optional
- **To** (`to`) - date, optional

**Columns:** Goods receipt · Received · Vendor · Store · Invoice no · Invoice date · Challan no · Challan date · LR / AWB · Vehicle · Transporter · Packages

**Drill-down:** no.

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

### R1033 Z-report (till close)

**Report id:** `z-report`

**Parameters:**

- **From** (`from`) - date, optional
- **To** (`to`) - date, optional
- **Store** (`store`) - text, optional

**Columns:** Closed · Store · Till · Cashier · Bills · First bill · Last bill · Opening float · Cash sales · Cash refunds · Advances in · Advances refunded · Paid in · Paid out · Safe drops · Expected cash · Counted cash · Variance · Tenders · Card declared · Card difference · UPI declared · UPI difference · Status

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

