# Report catalog coverage

<!-- GENERATED FILE - DO NOT EDIT BY HAND.
     Source: engines.CatalogCoverage() (engines/report_catalog*.go) and docs/specs/reports/catalog_*.tsv
     Regenerate: go run ./cmd/gencatalog -coverage docs/product/report-catalog-coverage.md -->

Stage 75.3: the user's two report catalogs (290 reports). **252 are built** - registered under their catalog id, with the catalog's columns in the catalog's order (held to `docs/specs/reports/catalog_columns.tsv` by `TestReportColumnsMatchCatalogSpec`). **38 wait for a module** that does not exist yet. Of the built reports' 2637 columns, 2574 are filled from ERP data; the rest are in the report, blank, because the ERP does not record them (listed per report below).

Reports open under **Reports → <catalog module>** (Sales & POS, Inventory, Procurement & GRN...), or search the report list by name.

## By module

| Module | Reports | Built | Waiting |
|---|---:|---:|---:|
| Sales & POS | 22 | 22 | 0 |
| Inventory | 21 | 19 | 2 |
| Inventory Loss & Audit | 12 | 0 | 12 |
| Procurement & GRN | 29 | 27 | 2 |
| Stock Transfer | 9 | 9 | 0 |
| External Movement | 17 | 0 | 17 |
| GST & Compliance | 9 | 8 | 1 |
| E-commerce & Integration | 12 | 12 | 0 |
| Master Data & Audit | 11 | 11 | 0 |
| MIS & Management | 10 | 10 | 0 |
| OMS | 13 | 13 | 0 |
| PIM / PIMS | 16 | 16 | 0 |
| Accounting | 26 | 26 | 0 |
| Expense Management | 11 | 11 | 0 |
| CRM | 13 | 10 | 3 |
| HRMS | 16 | 15 | 1 |
| Manufacturing | 16 | 16 | 0 |
| WMS | 27 | 27 | 0 |

## Sales & POS

| Report | Id | Status | Columns filled | Blank (not held by the ERP) |
|---|---|---|---:|---|
| Sales Register | `cat-sales-register` | Built | 21 / 21 |  |
| Sales Itemwise Detail | `cat-sales-itemwise-detail` | Built | 27 / 28 | Packing Charges |
| Sales Return Register | `cat-sales-return-register` | Built | 16 / 16 |  |
| Sales Return Itemwise Detail | `cat-sales-return-itemwise-detail` | Built | 24 / 26 | Item Discount, Packing Charges |
| Net Sales Summary | `cat-net-sales-summary` | Built | 13 / 13 |  |
| Sales Tender Summary | `cat-sales-tender-summary` | Built | 7 / 7 |  |
| Sales Tender Detail | `cat-sales-tender-detail` | Built | 11 / 11 |  |
| Salesman Sales Summary | `cat-salesman-sales-summary` | Built | 12 / 12 |  |
| Sales Counter Summary | `cat-sales-counter-summary` | Built | 10 / 10 |  |
| Sales Discount Analysis | `cat-sales-discount-analysis` | Built | 23 / 23 |  |
| Sales Margin Analysis | `cat-sales-margin-analysis` | Built | 18 / 18 |  |
| Sales Bill Edit Audit Report | `cat-sales-bill-edit-audit-report` | Built | 13 / 13 |  |
| Cancelled / Void Bill Register | `cat-cancelled-void-bill-register` | Built | 14 / 14 |  |
| Customer Advance Summary | `cat-customer-advance-summary` | Built | 13 / 14 | Sales Invoice No |
| Customer Advance Itemwise Detail | `cat-customer-advance-itemwise-detail` | Built | 21 / 24 | Sales Invoice No, POS Bill Status, POS Invoice No |
| Customer Advance Utilization | `cat-customer-advance-utilization` | Built | 12 / 12 |  |
| Credit Note Register | `cat-credit-note-register` | Built | 12 / 12 |  |
| Gift Card / Voucher Register | `cat-gift-card-voucher-register` | Built | 12 / 12 |  |
| B2B Sales Register | `cat-b2b-sales-register` | Built | 17 / 17 |  |
| Export / International Sales Register | `cat-export-international-sales-register` | Built | 15 / 15 |  |
| Sales Design Image Register | `cat-sales-design-image-register` | Built | 10 / 10 |  |
| Sales Variant Image Register | `cat-sales-variant-image-register` | Built | 11 / 11 |  |

## Inventory

| Report | Id | Status | Columns filled | Blank (not held by the ERP) |
|---|---|---|---:|---|
| Current Stock Summary | `cat-current-stock-summary` | Built | 11 / 11 |  |
| Current Stock Itemwise Detail | `cat-current-stock-itemwise-detail` | Built | 18 / 18 |  |
| Current Stock Barcode Detail | `cat-current-stock-barcode-detail` | Built | 19 / 19 |  |
| Stock Details - Period Wise | `cat-stock-details-period-wise` | Built | 33 / 33 |  |
| Stock Ledger | `cat-stock-ledger` | Built | 18 / 18 |  |
| Stock Ledger - Category Summary | `cat-stock-ledger-category-summary` | Built | 12 / 12 |  |
| Stock Aging | `cat-stock-aging` | Built | 18 / 18 |  |
| Stock Valuation Summary | `cat-stock-valuation-summary` | Built | 10 / 10 |  |
| Barcode-wise Cost & MRP Report | `cat-barcode-wise-cost-and-mrp-report` | Built | 22 / 22 |  |
| Stock Movement History | `cat-stock-movement-history` | Built | 12 / 12 |  |
| Inventory by Location Summary | `cat-inventory-by-location-summary` | Built | 8 / 8 |  |
| Inventory Matrix Summary | `cat-inventory-matrix-summary` | Built | 8 / 8 |  |
| Current Stock with Image | `cat-current-stock-with-image` | Built | 12 / 12 |  |
| Bulk Stock Upload Register | `cat-bulk-stock-upload-register` | Waits: The ERP has no bulk stock upload | - | - |
| Bulk Stock Upload Item Detail | `cat-bulk-stock-upload-item-detail` | Waits: The ERP has no bulk stock upload | - | - |
| Physical Stock Count Register | `cat-physical-stock-count-register` | Built | 11 / 11 |  |
| Physical Stock Variance Report | `cat-physical-stock-variance-report` | Built | 19 / 19 |  |
| Negative Stock Exception | `cat-negative-stock-exception` | Built | 10 / 10 |  |
| MRP Change Register | `cat-mrp-change-register` | Built | 13 / 13 |  |
| Barcode Reprint / Label History | `cat-barcode-reprint-label-history` | Built | 11 / 11 |  |
| Unique Design Inventory Summary | `cat-unique-design-inventory-summary` | Built | 7 / 7 |  |

## Inventory Loss & Audit

| Report | Id | Status | Columns filled | Blank (not held by the ERP) |
|---|---|---|---:|---|
| Wastage Entry Register | `cat-wastage-entry-register` | Waits: Wastage documents do not exist yet - Stage 74 (74.5.1) | - | - |
| Wastage Itemwise Detail | `cat-wastage-itemwise-detail` | Waits: Wastage documents do not exist yet - Stage 74 (74.5.1) | - | - |
| Damage Entry Register | `cat-damage-entry-register` | Waits: Damage documents do not exist yet - Stage 74 (74.5.1) | - | - |
| Damage Itemwise Detail | `cat-damage-itemwise-detail` | Waits: Damage documents do not exist yet - Stage 74 (74.5.1) | - | - |
| Wastage Reason Analysis | `cat-wastage-reason-analysis` | Waits: Wastage documents do not exist yet - Stage 74 (74.5.1) | - | - |
| Wastage Value Impact | `cat-wastage-value-impact` | Waits: Wastage documents do not exist yet - Stage 74 (74.5.1) | - | - |
| Damage Reason Analysis | `cat-damage-reason-analysis` | Waits: Damage documents do not exist yet - Stage 74 (74.5.1) | - | - |
| Damage Value Impact | `cat-damage-value-impact` | Waits: Damage documents do not exist yet - Stage 74 (74.5.1) | - | - |
| Stock Adjustment Register | `cat-stock-adjustment-register` | Waits: The Stock Adjustment document does not exist yet - 88.5.4 | - | - |
| Stock Adjustment Itemwise Detail | `cat-stock-adjustment-itemwise-detail` | Waits: The Stock Adjustment document does not exist yet - 88.5.4 | - | - |
| Stock Adjustment Approval Audit | `cat-stock-adjustment-approval-audit` | Waits: The Stock Adjustment document does not exist yet - 88.5.4 | - | - |
| Stock Adjustment Reason Analysis | `cat-stock-adjustment-reason-analysis` | Waits: The Stock Adjustment document does not exist yet - 88.5.4 | - | - |

## Procurement & GRN

| Report | Id | Status | Columns filled | Blank (not held by the ERP) |
|---|---|---|---:|---|
| Purchase Requisition Register | `cat-purchase-requisition-register` | Built | 12 / 12 |  |
| RFQ / Quotation Comparison | `cat-rfq-quotation-comparison` | Built | 13 / 13 |  |
| Purchase Order Register | `cat-purchase-order-register` | Built | 16 / 16 |  |
| Purchase Order Itemwise Detail | `cat-purchase-order-itemwise-detail` | Built | 24 / 24 |  |
| Pending / Open PO Report | `cat-pending-open-po-report` | Built | 14 / 14 |  |
| Pending PO with Image | `cat-pending-po-with-image` | Built | 11 / 11 |  |
| Pending PO with Variant Image | `cat-pending-po-with-variant-image` | Built | 12 / 12 |  |
| PO Approval Pending Report | `cat-po-approval-pending-report` | Built | 8 / 8 |  |
| PO Amendment History | `cat-po-amendment-history` | Built | 9 / 10 | Approved By |
| Cancelled PO Register | `cat-cancelled-po-register` | Built | 9 / 9 |  |
| Expired PO Report | `cat-expired-po-report` | Built | 9 / 9 |  |
| PO vs GRN Status | `cat-po-vs-grn-status` | Built | 13 / 13 |  |
| Vendor Performance Summary | `cat-vendor-performance-summary` | Built | 10 / 10 |  |
| Purchase Register | `cat-purchase-register` | Built | 13 / 13 |  |
| Purchase Itemwise Detail | `cat-purchase-itemwise-detail` | Built | 20 / 20 |  |
| Purchase Return Register | `cat-purchase-return-register` | Built | 13 / 13 |  |
| Purchase Return Itemwise Detail | `cat-purchase-return-itemwise-detail` | Built | 21 / 21 |  |
| Vendor Debit Note Register | `cat-vendor-debit-note-register` | Built | 11 / 11 |  |
| Vendor Credit Note Register | `cat-vendor-credit-note-register` | Built | 11 / 11 |  |
| GRN Register | `cat-grn-register` | Built | 13 / 13 |  |
| GRN Itemwise Detail | `cat-grn-itemwise-detail` | Built | 23 / 25 | Silver Rate / Gram, Making Charge / Gram |
| GRN PO / Invoice Variance | `cat-grn-po-invoice-variance` | Built | 16 / 16 |  |
| GRN Cancelled Register | `cat-grn-cancelled-register` | Built | 10 / 10 |  |
| GRN Barcode Generation Register | `cat-grn-barcode-generation-register` | Built | 11 / 11 |  |
| GRN QC / Rejection Exception | `cat-grn-qc-rejection-exception` | Built | 18 / 18 |  |
| Pending GRN / Open Receipt | `cat-pending-grn-open-receipt` | Built | 10 / 10 |  |
| GRN vs Vendor Invoice Reconciliation | `cat-grn-vs-vendor-invoice-reconciliation` | Built | 13 / 13 |  |
| Receipt Note Register | `cat-receipt-note-register` | Waits: Receipt notes do not exist yet - Stage 74 (74.5.1) | - | - |
| Receipt Note Itemwise Detail | `cat-receipt-note-itemwise-detail` | Waits: Receipt notes do not exist yet - Stage 74 (74.5.1) | - | - |

## Stock Transfer

| Report | Id | Status | Columns filled | Blank (not held by the ERP) |
|---|---|---|---:|---|
| Transfer Out Register | `cat-transfer-out-register` | Built | 8 / 10 | E-Way Bill No, IRN |
| Transfer Out Itemwise Detail | `cat-transfer-out-itemwise-detail` | Built | 14 / 16 | E-Way Bill No, IRN |
| Transfer In Register | `cat-transfer-in-register` | Built | 9 / 9 |  |
| Transfer In Itemwise Detail | `cat-transfer-in-itemwise-detail` | Built | 17 / 17 |  |
| Transfer In-Transit Aging | `cat-transfer-in-transit-aging` | Built | 9 / 9 |  |
| Transfer Barcode Traceability | `cat-transfer-barcode-traceability` | Built | 11 / 11 |  |
| Transfer GST / E-Way Control | `cat-transfer-gst-e-way-control` | Built | 7 / 11 | IRN, IRN Status, E-Way Bill No, E-Way Status |
| Transfer Shortage / Excess Exception | `cat-transfer-shortage-excess-exception` | Built | 17 / 17 |  |
| Transfer Approval / Cancellation Audit | `cat-transfer-approval-cancellation-audit` | Built | 11 / 11 |  |

## External Movement

| Report | Id | Status | Columns filled | Blank (not held by the ERP) |
|---|---|---|---:|---|
| Delivery Note Register | `cat-delivery-note-register` | Waits: Delivery challan documents do not exist yet - Stage 74 (74.5.1) | - | - |
| Delivery Note Itemwise Detail | `cat-delivery-note-itemwise-detail` | Waits: Delivery challan documents do not exist yet - Stage 74 (74.5.1) | - | - |
| Delivery Note Return Register | `cat-delivery-note-return-register` | Waits: Delivery challan documents do not exist yet - Stage 74 (74.5.1) | - | - |
| Delivery Note Return Item Detail | `cat-delivery-note-return-item-detail` | Waits: Delivery challan documents do not exist yet - Stage 74 (74.5.1) | - | - |
| Delivery Note Pending Return Aging | `cat-delivery-note-pending-return-aging` | Waits: Delivery challan documents do not exist yet - Stage 74 (74.5.1) | - | - |
| Job Work Outward Register | `cat-job-work-outward-register` | Waits: Job work outward / inward documents do not exist yet - Stage 74 (74.5.1) | - | - |
| Job Work Outward Item Detail | `cat-job-work-outward-item-detail` | Waits: Job work outward / inward documents do not exist yet - Stage 74 (74.5.1) | - | - |
| Job Work Inward Register | `cat-job-work-inward-register` | Waits: Job work outward / inward documents do not exist yet - Stage 74 (74.5.1) | - | - |
| Job Work Inward Item Detail | `cat-job-work-inward-item-detail` | Waits: Job work outward / inward documents do not exist yet - Stage 74 (74.5.1) | - | - |
| Job Work Pending Return Aging | `cat-job-work-pending-return-aging` | Waits: Job work outward / inward documents do not exist yet - Stage 74 (74.5.1) | - | - |
| Job Work Barcode Traceability | `cat-job-work-barcode-traceability` | Waits: Job work outward / inward documents do not exist yet - Stage 74 (74.5.1) | - | - |
| Job Work Shortage / Excess Exception | `cat-job-work-shortage-excess-exception` | Waits: Job work outward / inward documents do not exist yet - Stage 74 (74.5.1) | - | - |
| Material Issue Register | `cat-material-issue-register` | Waits: Material issue documents do not exist yet - Stage 74 (74.5.1) | - | - |
| Material Issue Item Detail | `cat-material-issue-item-detail` | Waits: Material issue documents do not exist yet - Stage 74 (74.5.1) | - | - |
| Material Receipt Register | `cat-material-receipt-register` | Waits: Material receipt documents do not exist yet - Stage 74 (74.5.1) | - | - |
| Material Receipt Item Detail | `cat-material-receipt-item-detail` | Waits: Material receipt documents do not exist yet - Stage 74 (74.5.1) | - | - |
| MI-MR Reclassification Traceability | `cat-mi-mr-reclassification-traceability` | Waits: Material issue / receipt documents do not exist yet - Stage 74 (74.5.1) | - | - |

## GST & Compliance

| Report | Id | Status | Columns filled | Blank (not held by the ERP) |
|---|---|---|---:|---|
| GSTR-1 HSN Summary | `cat-gstr-1-hsn-summary` | Built | 11 / 11 |  |
| GSTR-1 B2B / B2C Summary | `cat-gstr-1-b2b-b2c-summary` | Built | 11 / 11 |  |
| IRN Portal Manual Upload Report | `cat-irn-portal-manual-upload-report` | Built | 13 / 13 |  |
| E-Invoice / IRN Status Register | `cat-e-invoice-irn-status-register` | Built | 10 / 10 |  |
| E-Invoice Error / Retry Report | `cat-e-invoice-error-retry-report` | Built | 8 / 8 |  |
| E-Way Bill Register | `cat-e-way-bill-register` | Waits: The ERP does not issue e-way bills (75.2 covers e-invoices only) | - | - |
| GST Document Tax Detail | `cat-gst-document-tax-detail` | Built | 14 / 14 |  |
| HSN Tax Summary | `cat-hsn-tax-summary` | Built | 8 / 8 |  |
| GST Bill Number / Document Series Audit | `cat-gst-bill-number-document-series-audit` | Built | 8 / 8 |  |

## E-commerce & Integration

| Report | Id | Status | Columns filled | Blank (not held by the ERP) |
|---|---|---|---:|---|
| E-commerce Sales Order Summary | `cat-e-commerce-sales-order-summary` | Built | 17 / 18 | POS Invoice No |
| E-commerce Sales Order Item Detail | `cat-e-commerce-sales-order-item-detail` | Built | 18 / 19 | POS Invoice No |
| Pending Sales Order Stock | `cat-pending-sales-order-stock` | Built | 11 / 11 |  |
| Sales Order Integration Error | `cat-sales-order-integration-error` | Built | 8 / 9 | Shipment ID |
| ERP vs Unicommerce Inventory Reconciliation - Itemwise | `cat-erp-vs-unicommerce-inventory-reconciliation-itemwise` | Built | 12 / 12 |  |
| ERP vs Unicommerce Inventory Reconciliation - Summary | `cat-erp-vs-unicommerce-inventory-reconciliation-summary` | Built | 10 / 10 |  |
| Inventory Sync Log | `cat-inventory-sync-log` | Built | 8 / 11 | Error Code, Error Message, Retry Count |
| Order Sync Log | `cat-order-sync-log` | Built | 8 / 10 | Error Code, Error Message |
| Marketplace Order Status Summary | `cat-marketplace-order-status-summary` | Built | 8 / 9 | POS Bill Status |
| Online Return Register | `cat-online-return-register` | Built | 12 / 12 |  |
| Integration Retry / Failure Audit | `cat-integration-retry-failure-audit` | Built | 9 / 9 |  |
| Outlet / Facility Mapping Summary | `cat-outlet-facility-mapping-summary` | Built | 6 / 6 |  |

## Master Data & Audit

| Report | Id | Status | Columns filled | Blank (not held by the ERP) |
|---|---|---|---:|---|
| Item Master Register | `cat-item-master-register` | Built | 19 / 19 |  |
| Store / Location Master Register | `cat-store-location-master-register` | Built | 10 / 10 |  |
| Vendor Master Register | `cat-vendor-master-register` | Built | 6 / 6 |  |
| Master Data Change Audit | `cat-master-data-change-audit` | Built | 8 / 9 | Edit Reason |
| User Access Matrix Report | `cat-user-access-matrix-report` | Built | 6 / 7 | Approved By |
| Role Permission Audit | `cat-role-permission-audit` | Built | 5 / 6 | Approved By |
| Approval History Report | `cat-approval-history-report` | Built | 7 / 7 |  |
| User Activity Log | `cat-user-activity-log` | Built | 7 / 8 | IP Address |
| Report Download Audit | `cat-report-download-audit` | Built | 7 / 8 | IP Address |
| Backdated Entry Report | `cat-backdated-entry-report` | Built | 9 / 9 |  |
| Transaction Cancellation Audit | `cat-transaction-cancellation-audit` | Built | 10 / 10 |  |

## MIS & Management

| Report | Id | Status | Columns filled | Blank (not held by the ERP) |
|---|---|---|---:|---|
| Daily Business Summary | `cat-daily-business-summary` | Built | 12 / 12 |  |
| Sales vs Target | `cat-sales-vs-target` | Built | 6 / 8 | Target Amount, Achievement % |
| Store Performance / Ranking | `cat-store-performance-ranking` | Built | 11 / 11 |  |
| Category Performance Summary | `cat-category-performance-summary` | Built | 10 / 10 |  |
| Gross Margin Summary | `cat-gross-margin-summary` | Built | 8 / 8 |  |
| Fast / Slow Moving Stock | `cat-fast-slow-moving-stock` | Built | 11 / 11 |  |
| Stock Cover Report | `cat-stock-cover-report` | Built | 8 / 8 |  |
| Ageing Inventory Value | `cat-ageing-inventory-value` | Built | 7 / 7 |  |
| Inventory Turnover Summary | `cat-inventory-turnover-summary` | Built | 8 / 8 |  |
| Management Stock & Sales Summary | `cat-management-stock-and-sales-summary` | Built | 10 / 10 |  |

## OMS

| Report | Id | Status | Columns filled | Blank (not held by the ERP) |
|---|---|---|---:|---|
| Order Register | `cat-order-register` | Built | 11 / 12 | Discount Amount |
| Order Line Detail | `cat-order-line-detail` | Built | 12 / 12 |  |
| Order Allocation / Reservation Report | `cat-order-allocation-reservation-report` | Built | 10 / 10 |  |
| Fulfilment Status Report | `cat-fulfilment-status-report` | Built | 10 / 11 | Promise Date |
| Split Shipment Report | `cat-split-shipment-report` | Built | 9 / 9 |  |
| Pending Order / SLA Aging | `cat-pending-order-sla-aging` | Built | 9 / 9 |  |
| Order Cancellation Report | `cat-order-cancellation-report` | Built | 9 / 9 |  |
| Return / RTO Register | `cat-return-rto-register` | Built | 10 / 10 |  |
| Refund Status Report | `cat-refund-status-report` | Built | 8 / 8 |  |
| Order to Invoice Reconciliation | `cat-order-to-invoice-reconciliation` | Built | 8 / 10 | POS Invoice No, POS Bill Status |
| ATP / Promise Inventory Report | `cat-atp-promise-inventory-report` | Built | 8 / 8 |  |
| Channel Order Reconciliation | `cat-channel-order-reconciliation` | Built | 7 / 8 | Error Code |
| OMS Integration Error / Retry | `cat-oms-integration-error-retry` | Built | 8 / 9 | Shipment ID |

## PIM / PIMS

| Report | Id | Status | Columns filled | Blank (not held by the ERP) |
|---|---|---|---:|---|
| Product Master Report | `cat-product-master-report` | Built | 15 / 15 |  |
| Product Completeness Report | `cat-product-completeness-report` | Built | 8 / 8 |  |
| PIM Workbench Queue | `cat-pim-workbench-queue` | Built | 8 / 8 |  |
| Missing Attribute Report | `cat-missing-attribute-report` | Built | 6 / 7 | Channel |
| Product Family / Variant Matrix | `cat-product-family-variant-matrix` | Built | 10 / 10 |  |
| Attribute Definition Report | `cat-attribute-definition-report` | Built | 6 / 6 |  |
| Attribute Group Report | `cat-attribute-group-report` | Built | 5 / 5 |  |
| Family Attribute Mapping Report | `cat-family-attribute-mapping-report` | Built | 5 / 5 |  |
| Media / Image Coverage Report | `cat-media-image-coverage-report` | Built | 8 / 8 |  |
| Channel Readiness Report | `cat-channel-readiness-report` | Built | 7 / 7 |  |
| Channel Publication Status | `cat-channel-publication-status` | Built | 7 / 7 |  |
| Channel Publication Error Report | `cat-channel-publication-error-report` | Built | 7 / 7 |  |
| Category / Attribute Mapping Report | `cat-category-attribute-mapping-report` | Built | 6 / 7 | Product Family |
| Duplicate SKU / Barcode Exception | `cat-duplicate-sku-barcode-exception` | Built | 8 / 8 |  |
| Product Change Audit | `cat-product-change-audit` | Built | 7 / 8 | Edit Reason |
| Price / Inventory Publish Status | `cat-price-inventory-publish-status` | Built | 8 / 8 |  |

## Accounting

| Report | Id | Status | Columns filled | Blank (not held by the ERP) |
|---|---|---|---:|---|
| General Ledger | `cat-general-ledger` | Built | 10 / 10 |  |
| Trial Balance | `cat-trial-balance` | Built | 7 / 7 |  |
| Profit & Loss Statement | `cat-profit-and-loss-statement` | Built | 8 / 8 |  |
| Balance Sheet | `cat-balance-sheet` | Built | 3 / 3 |  |
| Cash Flow Statement | `cat-cash-flow-statement` | Built | 6 / 6 |  |
| AP Aging | `cat-ap-aging` | Built | 8 / 8 |  |
| AR Aging | `cat-ar-aging` | Built | 8 / 8 |  |
| Vendor Ledger | `cat-vendor-ledger` | Built | 8 / 8 |  |
| Customer Ledger | `cat-customer-ledger` | Built | 8 / 8 |  |
| Vendor Invoice Register | `cat-vendor-invoice-register` | Built | 10 / 10 |  |
| Payment Proposal Register | `cat-payment-proposal-register` | Built | 8 / 8 |  |
| Finance Approval Pending Report | `cat-finance-approval-pending-report` | Built | 9 / 9 |  |
| Sales Invoice Accounting Register | `cat-sales-invoice-accounting-register` | Built | 10 / 10 |  |
| Payment Register | `cat-payment-register` | Built | 8 / 8 |  |
| Receipt Register | `cat-receipt-register` | Built | 8 / 8 |  |
| Journal Voucher Register | `cat-journal-voucher-register` | Built | 9 / 9 |  |
| Debit / Credit Note Register | `cat-debit-credit-note-register` | Built | 10 / 10 |  |
| Bank Reconciliation Report | `cat-bank-reconciliation-report` | Built | 9 / 9 |  |
| TDS Register | `cat-tds-register` | Built | 9 / 9 |  |
| Fixed Asset Register | `cat-fixed-asset-register` | Built | 9 / 9 |  |
| Depreciation Schedule | `cat-depreciation-schedule` | Built | 8 / 8 |  |
| Cost Center P&L | `cat-cost-center-p-and-l` | Built | 8 / 8 |  |
| Budget vs Actual | `cat-budget-vs-actual` | Built | 9 / 9 |  |
| Intercompany Reconciliation | `cat-intercompany-reconciliation` | Built | 8 / 8 |  |
| Accounting Audit Trail | `cat-accounting-audit-trail` | Built | 11 / 11 |  |
| Period Close Checklist | `cat-period-close-checklist` | Built | 9 / 9 |  |

## Expense Management

| Report | Id | Status | Columns filled | Blank (not held by the ERP) |
|---|---|---|---:|---|
| Expense Claim Register | `cat-expense-claim-register` | Built | 11 / 11 |  |
| Claims Pending Approval | `cat-claims-pending-approval` | Built | 10 / 10 |  |
| Approved Pending Payment | `cat-approved-pending-payment` | Built | 9 / 9 |  |
| Paid Claims Register | `cat-paid-claims-register` | Built | 7 / 8 | UTR No |
| Rejected / Returned Claims | `cat-rejected-returned-claims` | Built | 9 / 9 |  |
| Expense Aging / SLA Report | `cat-expense-aging-sla-report` | Built | 10 / 10 |  |
| Department / Category / Month Summary | `cat-department-category-month-summary` | Built | 7 / 7 |  |
| Employee Expense Summary | `cat-employee-expense-summary` | Built | 8 / 8 |  |
| Policy Violation / Exception Report | `cat-policy-violation-exception-report` | Built | 9 / 9 |  |
| Expense Advance Settlement | `cat-expense-advance-settlement` | Built | 8 / 8 |  |
| Accounting Export Report | `cat-accounting-export-report` | Built | 10 / 10 |  |

## CRM

| Report | Id | Status | Columns filled | Blank (not held by the ERP) |
|---|---|---|---:|---|
| Customer 360 Summary | `cat-customer-360-summary` | Built | 9 / 10 | Feedback Score |
| Lead Register | `cat-lead-register` | Waits: The ERP keeps no leads | - | - |
| Opportunity Pipeline | `cat-opportunity-pipeline` | Waits: The ERP keeps no opportunities | - | - |
| Campaign Performance | `cat-campaign-performance` | Built | 7 / 8 | Achievement % |
| Customer Segmentation Report | `cat-customer-segmentation-report` | Built | 8 / 8 |  |
| Repeat Purchase Report | `cat-repeat-purchase-report` | Built | 7 / 7 |  |
| Loyalty Points Ledger | `cat-loyalty-points-ledger` | Built | 8 / 8 |  |
| Coupon / Voucher Usage Report | `cat-coupon-voucher-usage-report` | Built | 9 / 9 |  |
| Birthday / Anniversary Campaign List | `cat-birthday-anniversary-campaign-list` | Built | 8 / 8 |  |
| Customer Feedback / NPS Report | `cat-customer-feedback-nps-report` | Waits: The ERP keeps no customer feedback or NPS responses | - | - |
| Complaint / Service Case Register | `cat-complaint-service-case-register` | Built | 10 / 10 |  |
| Customer Merge / Mobile Change Audit | `cat-customer-merge-mobile-change-audit` | Built | 8 / 9 | Edit Reason |
| Customer Inactivity / Churn Report | `cat-customer-inactivity-churn-report` | Built | 8 / 8 |  |

## HRMS

| Report | Id | Status | Columns filled | Blank (not held by the ERP) |
|---|---|---|---:|---|
| Employee Master Register | `cat-employee-master-register` | Built | 8 / 8 |  |
| Attendance Register | `cat-attendance-register` | Built | 7 / 9 | Check In, Check Out |
| Late / Overtime Report | `cat-late-overtime-report` | Built | 6 / 9 | Check In, Check Out, Task Duration Min |
| Leave Register | `cat-leave-register` | Built | 9 / 10 | Reason |
| Shift / Roster Report | `cat-shift-roster-report` | Built | 7 / 7 |  |
| Payroll Register | `cat-payroll-register` | Built | 8 / 8 |  |
| Salary Component Detail | `cat-salary-component-detail` | Built | 7 / 7 |  |
| Statutory Deduction Summary | `cat-statutory-deduction-summary` | Built | 5 / 5 |  |
| Employee Loan / Advance Register | `cat-employee-loan-advance-register` | Built | 8 / 8 |  |
| Headcount Summary | `cat-headcount-summary` | Built | 6 / 6 |  |
| Attrition Report | `cat-attrition-report` | Built | 6 / 7 | Reason |
| Recruitment Pipeline | `cat-recruitment-pipeline` | Waits: The ERP keeps no recruitment records | - | - |
| Onboarding Checklist | `cat-onboarding-checklist` | Built | 7 / 8 | Approved By |
| Performance Review Summary | `cat-performance-review-summary` | Built | 9 / 9 |  |
| Training / Certification Register | `cat-training-certification-register` | Built | 7 / 7 |  |
| Employee Role / Access Mapping | `cat-employee-role-access-mapping` | Built | 8 / 9 | Approved By |

## Manufacturing

| Report | Id | Status | Columns filled | Blank (not held by the ERP) |
|---|---|---|---:|---|
| BOM Register | `cat-bom-register` | Built | 7 / 7 |  |
| BOM Costing Report | `cat-bom-costing-report` | Built | 8 / 8 |  |
| Production Order Register | `cat-production-order-register` | Built | 10 / 10 |  |
| Work In Process Report | `cat-work-in-process-report` | Built | 10 / 10 |  |
| Material Issue / Consumption Report | `cat-material-issue-consumption-report` | Built | 9 / 9 |  |
| Material Usage Variance | `cat-material-usage-variance` | Built | 8 / 8 |  |
| Production Receipt / Completion | `cat-production-receipt-completion` | Built | 9 / 9 |  |
| Yield / Scrap Analysis | `cat-yield-scrap-analysis` | Built | 9 / 9 |  |
| Quality Inspection Report | `cat-quality-inspection-report` | Built | 9 / 9 |  |
| Rework / Rejection Report | `cat-rework-rejection-report` | Built | 8 / 8 |  |
| Production Schedule | `cat-production-schedule` | Built | 7 / 7 |  |
| Work Center Capacity / Load | `cat-work-center-capacity-load` | Built | 7 / 7 |  |
| MRP Suggestions | `cat-mrp-suggestions` | Built | 6 / 7 | Reserved Qty |
| Subcontracting / Job Work Report | `cat-subcontracting-job-work-report` | Built | 9 / 9 |  |
| Batch / Lot Traceability | `cat-batch-lot-traceability` | Built | 9 / 9 |  |
| Standard vs Actual Cost | `cat-standard-vs-actual-cost` | Built | 7 / 7 |  |

## WMS

| Report | Id | Status | Columns filled | Blank (not held by the ERP) |
|---|---|---|---:|---|
| Inbound / ASN Register | `cat-inbound-asn-register` | Built | 8 / 8 |  |
| Receiving / GRN Detail | `cat-receiving-grn-detail` | Built | 10 / 11 | LPN |
| Inbound QC Exception | `cat-inbound-qc-exception` | Built | 9 / 9 |  |
| Putaway Pending Report | `cat-putaway-pending-report` | Built | 9 / 9 |  |
| Putaway Completion Report | `cat-putaway-completion-report` | Built | 10 / 10 |  |
| Bin Inventory Report | `cat-bin-inventory-report` | Built | 9 / 9 |  |
| Location Movement Register | `cat-location-movement-register` | Built | 10 / 10 |  |
| WMS Stock Transfer Execution Report | `cat-wms-stock-transfer-execution-report` | Built | 11 / 11 |  |
| Warehouse Cockpit KPI Summary | `cat-warehouse-cockpit-kpi-summary` | Built | 8 / 8 |  |
| Bin Utilization / Condition Report | `cat-bin-utilization-condition-report` | Built | 7 / 7 |  |
| LPN / Carton / Pallet Register | `cat-lpn-carton-pallet-register` | Built | 8 / 8 |  |
| Bin Replenishment Report | `cat-bin-replenishment-report` | Built | 9 / 9 |  |
| Pick List Report | `cat-pick-list-report` | Built | 10 / 10 |  |
| Wave / Batch Picking Report | `cat-wave-batch-picking-report` | Built | 10 / 10 |  |
| Picking Exception Report | `cat-picking-exception-report` | Built | 10 / 10 |  |
| Packing Report | `cat-packing-report` | Built | 8 / 9 | LPN |
| Dispatch / Shipment Register | `cat-dispatch-shipment-register` | Built | 9 / 9 |  |
| Cycle Count Register | `cat-cycle-count-register` | Built | 9 / 9 |  |
| Cycle Count Variance Report | `cat-cycle-count-variance-report` | Built | 10 / 10 |  |
| Bin Stock Aging | `cat-bin-stock-aging` | Built | 8 / 8 |  |
| Lot / Serial / Barcode Traceability | `cat-lot-serial-barcode-traceability` | Built | 8 / 9 | LPN |
| RF / Mobile Picking Productivity | `cat-rf-mobile-picking-productivity` | Built | 7 / 8 | Achievement % |
| Dock Door Utilization | `cat-dock-door-utilization` | Built | 7 / 7 |  |
| Appointment Calendar Report | `cat-appointment-calendar-report` | Built | 7 / 7 |  |
| Yard / Trailer Board Report | `cat-yard-trailer-board-report` | Built | 7 / 7 |  |
| Warehouse SLA / Throughput Summary | `cat-warehouse-sla-throughput-summary` | Built | 5 / 7 | SLA Age Hours, Achievement % |
| Warehouse Inventory Accuracy Summary | `cat-warehouse-inventory-accuracy-summary` | Built | 8 / 8 |  |
