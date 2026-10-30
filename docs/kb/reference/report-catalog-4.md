---
title: Report catalog (part 4)
section: Reference
order: 23
summary: Reports in WMS to WMS, continued from the report catalog.
audience: store manager, finance, category manager, admin
last_verified: 2026-09-10
owner: documentation-maintainer
status: active
topic_type: reference
module: platform
task: Report catalog (part 4)
prerequisites: Authorized access to the relevant ERP task
applies_to: source registries; not release acceptance
screens: [reports]
---

<!-- GENERATED ARTICLE - DO NOT EDIT BY HAND.
     Regenerate: pwsh docs/update-docs.ps1 -Group Content -->

# Report catalog (part 4)

This page continues the [report catalog](report-catalog.md), which lists
every category and explains how to run a report. It holds **49** reports, from
WMS to WMS.

## Contents

- [WMS](#wms) - 49 reports

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

