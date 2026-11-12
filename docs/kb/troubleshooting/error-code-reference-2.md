---
title: Error code reference (part 2)
section: Troubleshooting
order: 21
summary: Error codes for HR & Payroll to Procurement / RFQ, continued from the error code reference.
audience: everyone
last_verified: 2026-09-10
owner: documentation-maintainer
status: active
topic_type: reference
module: platform
task: Error code reference (part 2)
prerequisites: Authorized access to the relevant ERP task
applies_to: source registries; not release acceptance
---

<!-- GENERATED ARTICLE - DO NOT EDIT BY HAND.
     Regenerate: pwsh docs/update-docs.ps1 -Group Content -->

# Error code reference (part 2)

This page continues the [error code reference](error-code-reference.md), which
lists every area and explains how to read a code. It holds **279** codes, from
HR & Payroll to Procurement / RFQ.

## Contents

- [HR & Payroll](#hr-payroll) - 2 codes
- [HR / Payroll](#hr-payroll) - 15 codes
- [Integration / API](#integration-api) - 14 codes
- [Integrations](#integrations) - 1 codes
- [Inventory](#inventory) - 23 codes
- [Inventory / Warehouse](#inventory-warehouse) - 29 codes
- [Item Master](#item-master) - 2 codes
- [Manufacturing / Production](#manufacturing-production) - 16 codes
- [Master Data](#master-data) - 43 codes
- [Mobile App / Device](#mobile-app-device) - 7 codes
- [Notifications](#notifications) - 6 codes
- [Observability / Incident](#observability-incident) - 3 codes
- [Omnichannel / OMS](#omnichannel-oms) - 5 codes
- [Order Management](#order-management) - 10 codes
- [PIM](#pim) - 27 codes
- [PIM / Product Publishing](#pim-product-publishing) - 8 codes
- [POS / Billing](#pos-billing) - 42 codes
- [POS / Offline & Cash Drawer](#pos-offline-cash-drawer) - 8 codes
- [Patch / Bug Governance](#patch-bug-governance) - 3 codes
- [Payments](#payments) - 7 codes
- [Planning](#planning) - 1 codes
- [Procurement](#procurement) - 1 codes
- [Procurement / RFQ](#procurement-rfq) - 6 codes

## HR & Payroll

| Code | When it happens | What you see | What to do | Severity | HTTP |
|---|---|---|---|---|---|
| `HRPAYR-1322` | Payslip deductions are more than its gross pay | This payslip's deductions are more than its gross pay. | Check the salary structure, the loan EMI and the statutory rates, then run payroll for the period again. | Medium | 422 |
| `HRPAYR-1323` | Payroll run again for a period whose payslip is already posted | This employee's payslip for the period is already posted. | Open the posted payslip; a correction is made as an adjustment in the next period. | Medium | 422 |

## HR / Payroll

| Code | When it happens | What you see | What to do | Severity | HTTP |
|---|---|---|---|---|---|
| `HR-0267` | Inactive employee login blocked | Employee is inactive. ERP access is blocked. Please contact HR/Admin. | Contact HR/Admin. | High | 409 |
| `HR-0268` | Attendance device/location mismatch | Attendance location does not match assigned work location. Manager review is required. | Submit regularization request. | Medium | 200 |
| `HR-0269` | Shift not assigned | Shift is not assigned for this employee/date. Please configure shift before attendance. | Assign shift and retry. | High | 422 |
| `HR-0531` | Onboarding or offboarding checklist not found | That onboarding or offboarding checklist was not found. | Refresh HR -> Onboarding/Offboarding and open the checklist again. | Low | 404 |
| `HR-0532` | Checklist steps are not a readable step list | This checklist's steps are not in a form the step list can read. | Open the checklist and choose "Use as steps" to turn the text into one step per line. | Low | 422 |
| `HR-0533` | Checklist step does not exist | That step is not on this checklist. | Refresh the checklist - someone may have changed its steps - and tick the step again. | Low | 422 |
| `HR-0600` | Incentive changed since the payslip was made | The incentive changed since this payslip was made. | Run payroll for the employee again, then post. | Medium | 422 |
| `HRPAYR-0149` | Employee code duplicate | Employee code already exists. Please use a unique employee code. | Correct HR details. | Medium | 409 |
| `HRPAYR-0150` | Attendance missing | Attendance is missing for one or more days. Please regularize before payroll. | Correct HR details. | Medium | 422 |
| `HRPAYR-0151` | Leave balance insufficient | Insufficient leave balance for this leave request. | Correct HR details. | Medium | 422 |
| `HRPAYR-0152` | Leave overlap | Leave request overlaps with an existing leave entry. | Correct HR details. | Medium | 422 |
| `HRPAYR-0153` | Payroll period locked | Payroll period is locked. Changes are not allowed. | Correct HR details. | Medium | 409 |
| `HRPAYR-0154` | Salary component missing | Salary component configuration is missing for this employee. | Correct HR details. | Medium | 422 |
| `HRPAYR-0155` | Bank details missing | Employee bank details are required for salary payment. | Correct HR details. | Medium | 422 |
| `HRPAYR-0156` | Exit date before joining date | Exit date cannot be earlier than joining date. | Correct HR details. | Medium | 422 |

## Integration / API

| Code | When it happens | What you see | What to do | Severity | HTTP |
|---|---|---|---|---|---|
| `INT-0218` | Outbox event stuck | Integration event is pending longer than expected. Please review the queue. | Retry or move to dead-letter queue. | High | 200 |
| `INT-0219` | Retry limit reached | Integration retry limit reached. Please review the failed payload and take action. | Review payload, fix root cause, and retry. | High | 422 |
| `INT-0220` | Webhook signature invalid | Webhook verification failed. The request was rejected. | Check webhook secret and source platform. | Critical | 422 |
| `INT-0221` | Webhook timestamp expired | Webhook request expired and was rejected. | Ask source system to resend current event. | High | 422 |
| `INT-0222` | Duplicate idempotency key | This request was already processed. Duplicate action was ignored. | No action needed unless data is incorrect. | Medium | 200 |
| `INT-0223` | Credential decryption failed | Connector credentials could not be read. Please re-enter credentials. | Re-save credentials or rotate key correctly. | Critical | 503 |
| `INTEGR-0167` | Integration timeout | External service did not respond in time. Please retry. | Correct details or retry. Contact admin if repeated. | High | 503 |
| `INTEGR-0168` | Integration authentication failed | External service authentication failed. Please contact administrator. | Correct details or retry. Contact admin if repeated. | High | 401 |
| `INTEGR-0169` | Invalid API response | Invalid response received from external service. Please retry or contact support. | Correct details or retry. Contact admin if repeated. | High | 422 |
| `INTEGR-4718` | Courier booking not confirmed - the courier may already have it and cannot be asked | This courier booking is not confirmed - the courier may already have it. | Find the reference below on the courier's portal, then enter its AWB, or mark it not booked to try again. | High | 409 |
| `INTEGR-4719` | Courier booked again for a fulfillment task that already has a live booking | This parcel is already booked with a courier. | Use the booking it has, or cancel that booking before booking again. | Medium | 409 |
| `INTEGR-4720` | Courier pickup or cancellation not confirmed - the courier may already have done it and cannot be asked | This courier pickup or cancellation is not confirmed - the courier may already have done it. | Check the booking on the courier's portal, then resolve it here as done or not done to try again. | High | 409 |
| `INTEGR-4800` | Listings pulled from a channel whose connector cannot read its listings | This channel cannot send its product list to the ERP. | Link its SKUs one by one on the SKU mapping list. | Medium | 422 |
| `INTEGR-4801` | Stock switch set for a listing that has not been pulled | That listing is not on the list. | Pull the channel's listings first. | Medium | 422 |

## Integrations

| Code | When it happens | What you see | What to do | Severity | HTTP |
|---|---|---|---|---|---|
| `CONN-0994` | Shopify channel set-up incomplete or not valid | The Shopify connection is not ready for this. | Open the channel's Shopify page: save the shop address (yourstore.myshopify.com), the admin API token and the webhook secret, test the connection, and map each Shopify location to a store. | Medium | 422 |

## Inventory

| Code | When it happens | What you see | What to do | Severity | HTTP |
|---|---|---|---|---|---|
| `INVENT-1312` | Stock adjustment's snapshot is stale - on-hand changed since it was made | The stock changed since this adjustment was made. | Check the count again. A store manager can post it with the current stock (Post with current stock), or make a new adjustment. | Medium | 409 |
| `INVENT-1313` | Stock adjustment cannot be reversed (not posted, already reversed, itself a reversal, or no reason) | This adjustment can't be reversed. | Only a posted adjustment that is not a reversal and not already reversed can be reversed, with a reason. | Low | 422 |
| `INVENT-1314` | Material issue / return refused (no store, department or purpose; non-stock or backflush item; not approved; short stock; more back than issued) | This material request can't go ahead. | Name the store, department and purpose; issue stock items only (not backflushed ones); issue once approved; return no more than was issued. | Medium | 422 |
| `INVENT-1318` | Delivery challan refused (no store, purpose or party; non-stock item; no return date; short stock; not approved; more back than out; serial not out; write-off with no approver) | This delivery challan can't go ahead. | Name the store, purpose, party and return date; send stock items only; dispatch once approved; take back no more than is out, by the serials that went. | Medium | 422 |
| `INVENT-1325` | Piece hold placed while the setting is off | Holding a piece for a customer is switched off. | Ask an administrator to switch it on in Settings. | Low | 422 |
| `INVENT-1326` | Held piece sold, picked or allocated for someone else | This piece is held for another customer. | Pick another piece, or release the hold first. | Medium | 422 |
| `INVENT-1327` | Piece or customer cannot be held | This piece cannot be held. | Pick a customer and a piece that is in stock and not taken by an order or transfer. | Low | 422 |
| `INVENT-1328` | Piece already held | This piece is already held. | Release the hold first. | Low | 409 |
| `INVENT-1329` | Hold ends in the past | The hold must end today or later. | Pick a later day, or leave it empty. | Low | 422 |
| `INVENT-1330` | Release of a piece that is not held | This piece is not held. | Refresh the list. | Low | 422 |
| `INVENT-1400` | Purchase unit without its factor, or a factor without a unit (148.35) | Enter the purchase unit and how many stock units are in one. | Fill both boxes, or clear both. | Medium | 422 |
| `INVENT-1401` | Stock unit changed after the item has moved (148.35) | This item has stock movements, so its stock unit cannot change. | Keep the stock unit; make a new item if the unit really changed. | Medium | 422 |
| `INVENT-1402` | Purchase unit is the stock unit (148.35) | The purchase unit is the stock unit. | Leave the purchase unit blank, or use a different unit. | Medium | 422 |
| `PURCHA-5040` | Correct attributes on something that is not a batch or serial, or one that does not exist | That batch or serial was not found. | Pick a batch or serial from the list. | Medium | 422 |
| `PURCHA-5041` | Correct a batch or serial attribute without a reason | Say why the attributes are being corrected. | Type the reason, for example hallmark re-test. | Medium | 422 |
| `PURCHA-5042` | Correct an attribute that is not an item field, or a weight that is not a number | That value cannot be saved. | Use the fields shown and numbers that are not negative. | Medium | 422 |
| `PURCHA-5043` | Correct attributes with every value the same as today | Nothing was changed. | Change at least one value. | Medium | 422 |
| `STKLOC-0860` | Stock location name or code missing, duplicate or badly formed | The stock location's name or code is not right. | Give it a name no other location uses, and a code of 2-30 capital letters, digits, '-' or '_'. | Medium | 422 |
| `STKLOC-0861` | Stock location switched off or taken from a store while it still holds stock | This location still holds stock. | Move the stock out with Stock -> Move Stock first, then switch the location off. | High | 422 |
| `STKLOC-0862` | Transaction default location not allowed for that transaction | That location cannot be this transaction's default. | Pick Main, Inward, QC Hold, Damage, Purchase Return or one of your own locations, and two different ones for from and to. | Medium | 422 |
| `STKLOC-0863` | Stock move incomplete or through a location that only transfers / holds change | The stock move is not complete. | Choose the item, a quantity above zero and two different locations the store uses (In-Transit and On Hold change only through transfers and hold codes). | Medium | 422 |
| `STKLOC-0864` | Stock move larger than the free stock in the from location | There is not enough free stock in that location. | Move no more than the location holds (stock promised to an order is not free). | High | 422 |
| `STKLOC-0865` | Stock move record written through the generic record API | Stock moves are made from Stock -> Move Stock. | Use Move Stock, which moves the stock and records it in one step. | Medium | 422 |

## Inventory / Warehouse

| Code | When it happens | What you see | What to do | Severity | HTTP |
|---|---|---|---|---|---|
| `INV-0256` | FEFO violation | The stock that expires first, or the oldest stock, must be issued first. | Pick the batch the screen suggests. | High | 422 |
| `INV-0257` | Bin capacity exceeded | Selected bin does not have enough capacity. Please choose another bin. | Select another bin or split stock. | Medium | 422 |
| `INV-0258` | Cycle count freeze active | Stock is frozen for counting. Sale/transfer is blocked until count is completed. | Complete or release count session. | High | 200 |
| `INVENT-0101` | Insufficient stock | Insufficient available stock for this item at the selected location. | Lower the quantity to what is available, or choose a location that has the stock. | High | 422 |
| `INVENT-0102` | Stock not available for barcode | Selected barcode is not available for transaction. | Scan the barcode of a unit that is in stock here. | High | 422 |
| `INVENT-0103` | Barcode already consumed | This barcode is already consumed or transferred. Please scan a valid barcode. | Scan the barcode on another unit. If this unit is in your hand, give it to a supervisor to check. | High | 409 |
| `INVENT-0104` | Blocked stock selected | Blocked stock cannot be issued or sold until it is released. | Use stock that is not blocked, or ask a supervisor to release the block first. | High | 409 |
| `INVENT-0105` | Negative stock not allowed | Stock of this item at this location cannot go below zero. | Lower the quantity, or receive or move the stock in first. | High | 422 |
| `INVENT-0106` | Batch expired | This batch is expired. Transaction is not allowed. | Pick a batch that has not expired, and set the expired stock aside. | High | 422 |
| `INVENT-0107` | Location/bin missing | This transaction needs a storage location or bin. | Choose the bin, then save again. | High | 422 |
| `INVENT-0108` | Stock count already in progress | A stock count is already running for this item at this location. | Wait for the count to finish, or ask the person running it to close it. | High | 409 |
| `INVENT-0109` | Inventory adjustment reason missing | Adjustment reason is required before posting inventory adjustment. | Choose a reason for the adjustment, then post again. | High | 422 |
| `INVENT-0110` | Adjustment exceeds tolerance | This adjustment is larger than allowed without approval. | Submit it for approval. It posts once a manager approves it. | High | 422 |
| `INVENT-0114` | Reserved stock blocked | This stock is reserved for another order and cannot be used. | Use stock that is not reserved, or ask a supervisor to release the reservation. | High | 409 |
| `INVENT-0115` | Serial/batch mismatch | The scanned barcode is for a different item. | Scan the barcode of the item on the line, or change the line to the scanned item. | High | 422 |
| `INVENT-0185` | Approval needed | This adjustment requires approval because it exceeds tolerance. | Submit for approval. | Medium | 200 |
| `INVENT-0421` | Non-stock item on a stock transaction (ITM-002) | This item keeps no stock, so it cannot be transferred, counted or consumed. | Remove it - services and non-stock expense items never hold store stock. | Medium | 422 |
| `INVENT-0422` | Consumption entry incomplete or invalid | This consumption entry cannot be posted. | Choose the store, then for each item the counted closing quantity (periodic) or the quantity used (manual issue) - neither can exceed the stock on hand. | Medium | 422 |
| `INVENT-0491` | Stock, a bin or an order for another owner at a warehouse dedicated to one owner | This warehouse holds one client's stock only. | Use that client's own warehouse for this stock or order, or switch Settings > Warehouse > Stock ownership model to several owners per warehouse. | High | 422 |
| `INVENT-0492` | Order has no owner at a location holding several owners' stock | Say which client (owner) this order is for. | Set the order's owner (WMS > Fulfillment > Order owner) and release it again. | High | 422 |
| `INVENT-0493` | Order owner changed after release | This order's owner can no longer change. | An order keeps its owner once released to the warehouse; cancel and re-enter it to change whose stock fills it. | Medium | 422 |
| `INVENT-0494` | Owner attribution would exceed or orphan bin stock | This would credit an owner with stock the bin does not hold, or leave stock without an owner. | Move the stock out or record whose it is first; owners can never hold more than the bin. | Medium | 422 |
| `INVENT-0495` | Stock moved into or out of a bin that belongs to another owner | That bin belongs to another client. | Use a bin of the stock's own owner, or an unowned bin. | Medium | 422 |
| `INVENT-0496` | Owner not given where several owners' stock is involved | Say whose stock this is. | Choose the owner (client) - this location or bin holds stock of more than one. | Medium | 422 |
| `INVENT-0497` | Trailer cannot be spotted at that door | That door cannot take this trailer. | Choose an active door with no trailer at it, or check the other trailer out first. | Medium | 422 |
| `INVENT-0498` | Trailer check-in duplicated or appointment not open | This trailer cannot be checked in. | Enter the trailer number; a trailer already in the yard must be checked out first, and only a Scheduled appointment can be checked in. | Medium | 422 |
| `INVENT-0499` | Tasks cannot go into that wave | These tasks cannot go into a wave. | Choose tasks at the wave's location that are still Pending or Picking and in no other wave, and a wave that is Planned, Released or In Progress. | Medium | 422 |
| `INVENT-0500` | Billing review without an owner or a valid period | Choose whose charges and which period to review. | Pick the owner (client), a start date and an end date on or after it. | Low | 422 |
| `INVENT-0971` | Putaway refused: bin missing, inactive, blocked or full, over capacity, hazmat zone, or not enough unbinned stock | This putaway cannot be made. | Read the reason: scan a bin that exists and is Active and Available, put away no more than the location's unbinned stock, or choose a bin with room (or a hazmat zone for a hazmat item). | Medium | 422 |

## Item Master

| Code | When it happens | What you see | What to do | Severity | HTTP |
|---|---|---|---|---|---|
| `MASTER-1302` | Item release approval: no Item approval rule, or an item waiting for release edited to another status | This item waits for its approval. | Add an Item rule under Settings > Approval Rules if there is none; an item in Draft, Pending Approval or Blocked is released only by its approval. | Medium | 422 |
| `MASTER-1306` | Scheduled tax change incomplete or malformed (no date, no rate or HSN, rate 0, HSN not 4/6/8 digits) | The scheduled tax change is not complete. | Give the date it applies from and the new GST rate (more than 0), the new HSN code (4, 6 or 8 digits), or both - or clear all three. | Low | 422 |

## Manufacturing / Production

| Code | When it happens | What you see | What to do | Severity | HTTP |
|---|---|---|---|---|---|
| `MANUFA-0140` | BOM missing | BOM is not maintained for this finished good. | Correct production details or request approval. | Medium | 422 |
| `MANUFA-0141` | BOM inactive | Selected BOM is inactive. Please select an active BOM. | Correct production details or request approval. | Medium | 422 |
| `MANUFA-0142` | Routing missing | Routing/work center is not configured for this production order. | Correct production details or request approval. | Medium | 422 |
| `MANUFA-0143` | Raw material shortage | Raw material stock is insufficient for production issue. | Correct production details or request approval. | Medium | 422 |
| `MANUFA-0144` | Production order already released | Production order is already released. This change is not allowed. | Correct production details or request approval. | Medium | 409 |
| `MANUFA-0145` | Yield exceeds planned quantity | Yield quantity cannot exceed planned quantity without approval. | Correct production details or request approval. | Medium | 422 |
| `MANUFA-0146` | Scrap reason missing | Scrap reason is required before posting scrap quantity. | Correct production details or request approval. | Medium | 422 |
| `MANUFA-0147` | Operation sequence invalid | Operation cannot be confirmed before previous operation is completed. | Correct production details or request approval. | Medium | 422 |
| `MANUFA-0148` | Quality approval pending | Production receipt cannot be posted until quality approval is completed. | Correct production details or request approval. | Medium | 422 |
| `MFG-0276` | BOM version changed after release | BOM changed after production release. Please re-plan or approve variance. | Cancel/re-plan or approve variance. | High | 422 |
| `MFG-0277` | Work center capacity exceeded | Work center capacity is exceeded for the selected schedule. | Reschedule or approve overtime. | Medium | 200 |
| `MFG-0278` | QC result failed | Quality inspection failed. Finished goods cannot be released to available stock. | Route to rework/scrap or approve exception. | High | 503 |
| `MFG-0279` | Production cost variance exceeds tolerance | Production cost variance exceeds configured tolerance. Approval is required. | Review variance and approve/post adjustment. | High | 200 |
| `MFG-4962` | Save or import sets, changes or clears a production order's BOM-change acceptance | A BOM change is accepted only with Accept BOM change. | Save the order without that field. To finish an order whose BOM changed, use Complete and give the reason when asked. | Medium | 422 |
| `MFG-4963` | BOM change accepted without a reason | Give the reason for accepting the BOM change. | Type why the order may finish on the changed BOM, then accept again. | Low | 422 |
| `MFG-4964` | BOM change accepted where there is none (BOM as issued, order completed or cancelled) | There is no BOM change to accept on this order. | Complete the order as it is. | Low | 422 |

## Master Data

| Code | When it happens | What you see | What to do | Severity | HTTP |
|---|---|---|---|---|---|
| `MASTER-0040` | Item code duplicate | Item code already exists. Please use a unique item code. | Correct master data or request approval. | Medium | 409 |
| `MASTER-0041` | Item mandatory attributes missing | Please enter all mandatory item attributes before saving. | Correct master data or request approval. | Medium | 422 |
| `MASTER-0042` | HSN missing | HSN code is required for this item. Please enter a valid HSN code. | Correct master data or request approval. | Medium | 422 |
| `MASTER-0043` | Invalid HSN length | Invalid HSN code. Please enter a valid 4, 6, or 8 digit HSN code as configured. | Correct master data or request approval. | Medium | 422 |
| `MASTER-0044` | Tax category missing | Tax category is required for this item. | Correct master data or request approval. | Medium | 422 |
| `MASTER-0045` | UOM missing | Unit of Measure is required for this item. | Correct master data or request approval. | Medium | 422 |
| `MASTER-0046` | UOM conversion missing | UOM conversion is not configured for this item. | Correct master data or request approval. | Medium | 422 |
| `MASTER-0047` | MRP lower than selling price rule | MRP cannot be lower than selling price. Please correct the price. | Correct master data or request approval. | Medium | 422 |
| `MASTER-0048` | Cost price higher than allowed variance | Cost price exceeds the allowed variance limit. Approval is required. | Correct master data or request approval. | Medium | 422 |
| `MASTER-0049` | Vendor GSTIN invalid | Invalid GSTIN. Please enter a valid 15-character GSTIN. | Correct master data or request approval. | Medium | 422 |
| `MASTER-0050` | Vendor PAN mismatch | PAN in GSTIN does not match the vendor PAN. Please verify vendor details. | Correct master data or request approval. | Medium | 422 |
| `MASTER-0051` | Customer mobile invalid | Invalid mobile number. Please enter a valid mobile number. | Correct master data or request approval. | Medium | 422 |
| `MASTER-0052` | Bank account invalid | Invalid bank account details. Please verify account number and IFSC. | Correct master data or request approval. | Medium | 422 |
| `MASTER-0053` | Duplicate barcode | Barcode already exists. Please use a unique barcode. | Correct master data or request approval. | Medium | 409 |
| `MASTER-0054` | Inactive item transaction | Inactive item cannot be used in transactions. | Correct master data or request approval. | Medium | 422 |
| `MASTER-0263` | Vendor bank change pending approval | Vendor bank details are pending approval and cannot be used for payment. | Approve bank change before payment. | Critical | 422 |
| `MASTER-0312` | Store Short Code already used by another store | Another store already uses this Short Code. | Give each store its own Short Code - it is printed inside document numbers. | High | 422 |
| `MASTER-0313` | Store Facility Code already used by another store | Another store already uses this Facility Code. | Give each store its own Facility Code - OMS orders find their store by it. | High | 422 |
| `MASTER-0315` | Free-with-item product has no parent item | Choose the item this one is given free with. | Set "Free with item" to the main product - it cannot be the item itself. | Medium | 422 |
| `MASTER-0401` | Valuated stock without Inventory asset accounting (ITM-005) | Valuated stock has to be an inventory asset. | Set Accounting treatment to Inventory asset, or Valuation to Non-valuated. | High | 422 |
| `MASTER-0402` | Stock tracking and valuation do not fit together (ITM-006) | An item that keeps no stock cannot carry stock value. | Not tracked items use Valuation = Not applicable; tracked items are Valuated or Non-valuated. | High | 422 |
| `MASTER-0403` | Fixed asset accounting on an item that is not a Fixed Asset (ITM-007) | Only a Fixed Asset item can use Fixed asset accounting. | Set Commercial class to Fixed Asset, or choose another accounting treatment. | High | 422 |
| `MASTER-0404` | Stored-value item set up as merchandise, or merchandise set up as stored value (ITM-008) | A gift card or voucher item is stored value, not merchandise. | Use Commercial class Stored Value, Sellability Stored-value issue only and Accounting Stored value liability together (the Gift Card item type has them). | High | 422 |
| `MASTER-0405` | Automatic or periodic consumption on an item with no stock (ITM-009) | Only a stock-tracked item can be consumed from a store. | Set Stock tracking to Tracked, or choose a stock-tracked item (for example of type Packaging). | High | 422 |
| `MASTER-0406` | Expense account not in the chart of accounts | That expense account is not in the chart of accounts. | Enter an account code from Finance -> Chart of Accounts (for example 5160 Packaging Consumption), or leave it blank to use the default. | Medium | 422 |
| `MASTER-0407` | Stock tracking, valuation or accounting changed after stock history (ITM-018) | This item already has stock, so how it is stocked, valued and accounted can only be corrected by an admin. | Tell your admin. An admin uses Correct treatment on the item (or item type): it shows what happens to the stock and the books before anything changes. | High | 422 |
| `MASTER-0408` | Backflush rule incomplete or invalid | This backflush rule is not complete. | Say what it applies to (an item, Name:Value attribute, category, item type or all items), the item consumed, a whole quantity of 1 or more, and dates in order. | Medium | 422 |
| `MASTER-0472` | Vendor GSTIN already used by another vendor | Another vendor already has this GSTIN. | Use the existing vendor - two records for one supplier is how a bill gets paid twice. If it is a different branch, correct the GSTIN. | High | 422 |
| `MASTER-0890` | Item field setting not valid | That item field setting cannot be saved. | Asked at the GRN must be No, Optional or Required; only a typed field can be made a list, and only once. | Low | 422 |
| `MASTER-0891` | GRN line missing a field the organisation requires at receipt | A line on this GRN is missing something that must be entered at receipt. | Fill the column named in the message (for example the weight or purity) on that line, then post again. | Medium | 422 |
| `MASTER-0892` | Item treatment correction attempted by someone who is not an admin | Only an admin can correct how an item with stock is stocked, valued or accounted. | Tell your admin what needs to change and why. | Medium | 403 |
| `MASTER-0893` | Item treatment correction not confirmed - reason missing, code not typed, or nothing to correct | The correction was not confirmed. | Give the reason and type the item code exactly as shown, then confirm. A change that does not touch stock tracking, valuation or accounting is saved on the form instead. | Low | 422 |
| `MASTER-0894` | Item treatment correction cannot be made as it stands | This correction cannot be made yet. | Do what the message lists first - release reserved or held stock, enter a value per unit, add the account, or create a new item where the change is not possible. | Medium | 422 |
| `MASTER-0895` | Stock or cost changed between the impact preview and the confirmation | The stock or cost changed since you reviewed the impact. | Review the impact again and confirm the new figures. | Low | 409 |
| `MASTER-0896` | MRP rule that cannot suggest an MRP | That MRP rule cannot be saved. | Choose its method and fill what it needs: a multiplier above 0; price bands written as 500=3, 2000=2.5, above=2; or a rate per gram above 0. | Low | 422 |
| `MASTER-0931` | Department missing, not on the Department list, or Inactive | Pick the person's department from the list. | Choose an Active department. If it is not there yet, add it under Setup > Department first. | Low | 422 |
| `MASTER-0932` | Department changed on a user who is linked to an employee | This user's department comes from their employee record. | Change the department on the employee (HR > Employees); the user follows it. | Low | 422 |
| `MASTER-0975` | A reason code missing, inactive, of another action or outside its dates | Choose a reason from the list. | Pick one of the reasons offered. If none fits, ask an administrator to add one under Setup > Reason Codes for this action. | Low | 422 |
| `MASTER-0990` | Old master codes renumbered without a backup from the last 24 hours | Renumbering old codes needs a backup from the last 24 hours. | Wait for the nightly backup (02:00) or ask the operator to run one now, then renumber again. Nothing was changed. | Medium | 422 |
| `MASTER-0991` | Item codes renumbered without acknowledging that stickers and channel SKUs keep the old code | Confirm you have read the warning about stickers and sales channels first. | Tick the box under the warning, then renumber. Nothing was changed. | Low | 422 |
| `MASTER-0992` | Store code typed on the Edit form is not valid, or the store was not found | This store code cannot be used. | Use 1 to 30 capital letters or digits, with - or _ between them - no spaces or /. Nothing was changed. | Low | 422 |
| `MASTER-0993` | Store code typed on the Edit form is already another record's code | Another record already has this code. | Choose a code no other store or record uses. Nothing was changed. | Medium | 422 |
| `MASTER-1321` | Duplicate master record, or a second Active strategy / template / rule for the same scope | A record like this already exists. | Use the existing record, change these values, or make that record inactive first. | Medium | 409 |

## Mobile App / Device

| Code | When it happens | What you see | What to do | Severity | HTTP |
|---|---|---|---|---|---|
| `DEVICE-0298` | Printer not configured | Printer is not configured for this location. Please contact administrator. | Pick another printer, or ask an administrator to set one up for this location. | High | 422 |
| `DEVICE-0299` | Print job failed | Sticker print job failed. Please check printer connection and retry. | Check the printer is on, connected and loaded, then print again. | Medium | 503 |
| `DEVICE-0300` | Barcode symbology not configured | Barcode format is not configured. Please contact administrator. | Ask an administrator to set up the barcode template. | High | 422 |
| `DEVICE-0301` | Device clock mismatch | Device time differs from server time. Sync may be blocked. | Correct device time. | Medium | 200 |
| `MOBILE-0175` | Device not registered | This device is not registered. Please contact administrator. | Ask an administrator to register this device, then sign in again. | Medium | 422 |
| `MOBILE-0176` | Offline sync conflict | Offline data has conflicts with latest server data. Please review and sync again. | Check each record the sync names, correct it, then sync again. | Medium | 409 |
| `MOBILE-0177` | Scanner input invalid | The scanned code could not be read as a barcode or QR code. | Scan again, or type the code in. | Medium | 422 |

## Notifications

| Code | When it happens | What you see | What to do | Severity | HTTP |
|---|---|---|---|---|---|
| `NOTIFI-0170` | Notification not sent | Notification could not be sent. Please verify template and recipient details. | Correct details or retry. Contact admin if repeated. | Medium | 422 |
| `NOTIFI-0171` | Email recipient missing | Recipient email address is required. | Correct details or retry. Contact admin if repeated. | Medium | 422 |
| `NOTIFI-0172` | SMS template missing | SMS template is not configured. Please contact administrator. | Correct details or retry. Contact admin if repeated. | Medium | 422 |
| `NOTIFI-0461` | Delivery receipt status not recognised | The delivery status must be delivered or failed. | Send status "delivered" or "failed" for this notification. | Low | 422 |
| `NOTIFI-0462` | Delivery receipt for an unknown notification | No notification has this delivery id. | Use the delivery_id the notification webhook carried. | Low | 404 |
| `NOTIFI-0463` | Delivery receipt conflicts with a final or never-sent notification | This notification's delivery state can no longer change. | A delivered notification is final, and one that was never sent (skipped) has nothing to confirm. | Low | 409 |

## Observability / Incident

| Code | When it happens | What you see | What to do | Severity | HTTP |
|---|---|---|---|---|---|
| `OBS-0215` | Alert webhook missing | Incident alert channel is not configured. Please set the operations webhook URL. | Configure alert webhook and contacts. | Critical | 422 |
| `OBS-0216` | Incident not acknowledged | Incident is not acknowledged within SLA. Escalation is required. | Acknowledge or escalate incident. | High | 200 |
| `OBS-0217` | Correlation ID missing | Error could not be traced because correlation ID is missing. | Capture request trace and retry. | High | 422 |

## Omnichannel / OMS

| Code | When it happens | What you see | What to do | Severity | HTTP |
|---|---|---|---|---|---|
| `OMNI-0245` | ATS mismatch detected | Available-to-sell stock mismatch detected. Please run inventory reconciliation. | Run sync reconciliation. | High | 422 |
| `OMNI-0246` | Reservation expired | Stock reservation expired. Please reallocate the order. | Reallocate order or reserve again. | Medium | 200 |
| `OMNI-0247` | No fulfillment node available | No store or warehouse has enough available stock to fulfill this order. | Backorder, split order, or cancel as per policy. | High | 422 |
| `OMNI-0248` | Pickup window expired | Customer pickup window has expired. Please release or extend reservation. | Extend pickup window or release stock. | Medium | 200 |
| `OMNI-0249` | Return tax jurisdiction mismatch | Return location tax treatment differs from original sale. Please follow configured return process. | Use central return flow or approved exception. | High | 422 |

## Order Management

| Code | When it happens | What you see | What to do | Severity | HTTP |
|---|---|---|---|---|---|
| `ORDERM-0135` | Order stock not allocated | Order cannot be confirmed because stock is not allocated. | Correct details or request approval. | High | 422 |
| `ORDERM-0136` | Order already dispatched | Order is already dispatched. Changes are not allowed. | Correct details or request approval. | High | 409 |
| `ORDERM-0464` | Unknown OMS exception queue requested | There is no such order queue. | Choose one of the queues listed on the Order Management screen. | Low | 404 |
| `ORDERM-0465` | Cancel or line edit after goods were packed | Part of this order is already packed, so it can no longer be cancelled or changed. | Let it ship and raise a return, or mark the shipment RTO if it has not left. | Medium | 409 |
| `ORDERM-0466` | Line edit after the pick cutoff, after invoicing or in a closed status | This order's lines can no longer change. | Lines change only until the warehouse starts picking and before invoicing. After that, cancel and re-order, or use a return. | Medium | 409 |
| `ORDERM-0467` | Line change request not valid | The line change could not be applied. | Check the item, quantity (at least 1), price and reason, and that the order keeps at least one line. | Low | 422 |
| `ORDERM-0468` | Replacement or reship order not valid | The replacement or reship could not be raised. | Choose lines of a shipped order, within the quantity that shipped and was not already sent again, with a reason. | Low | 422 |
| `ORDERM-0469` | Not enough stock for a line change | There is not enough free stock for this quantity. | Lower the quantity, or move the order to a location with stock (Switch facility) and try again. | Medium | 409 |
| `ORDERM-0970` | Order re-allocated to a store without enough of its owner's own stock | That store does not hold enough of this order owner's own stock. | Pick a store that holds the owner's stock, or leave the store blank so the system sources it on the owner's stock. | Medium | 422 |
| `ORDERM-0980` | Credit hold released or replaced by hand | This order is on Credit hold, so it cannot be released or held again by hand. | Approve its release request in the approval inbox. | Low | 409 |

## PIM

| Code | When it happens | What you see | What to do | Severity | HTTP |
|---|---|---|---|---|---|
| `PIM-0321` | Adding an attribute value from the PO that cannot be added | That value could not be added to the product attribute. | Type one value without commas, for an attribute chosen from a list; typed attributes take their value on the design itself. | Low | 422 |
| `PIM-0690` | Deleting a product attribute that was set up from the attribute library | This attribute was set up for your business and cannot be deleted. | Switch it off instead: open it and set Status to Inactive. Its values can still be removed one by one. | Low | 422 |
| `PIM-0691` | Creating a new product attribute from a purchase order when the business's attributes are set up for it | New attributes are not created from a purchase order here - your product attributes are set up for your business. | Pick one of the attributes shown, or ask your administrator to add one under Setup -> Item Attribute. | Low | 422 |
| `PIM-0950` | Saving or testing image drive settings with a field missing or malformed | The image drive settings are not complete. | Fill in the field named in the message: the endpoint (https://...), the bucket name, the access key ID and the secret access key, as your storage provider shows them. | Low | 422 |
| `PIM-0951` | The client's image drive could not be reached, or refused the keys, bucket or region | The image drive did not accept the request. | Read the reason in the message, correct the endpoint, region, bucket or keys in Settings > Configuration > Integrations > Image drive, and press Test connection. | Medium | 422 |
| `PIM-0952` | Opening an image stored on the image drive after the drive's settings were removed | This image is stored on the image drive, which is not set up. | Ask your administrator to set the image drive up again in Settings > Configuration > Integrations > Image drive. | Medium | 422 |
| `PIM-0953` | Loading a marketplace template file that cannot be read or has no columns | The template file could not be read. | Upload the category file as downloaded from the marketplace (.xlsx) and check it has a header row. | Medium | 422 |
| `PIM-0954` | Saving or loading a marketplace template that is not valid | This marketplace template is not valid. | Fix the field named in the message and save again. | Medium | 422 |
| `PIM-0955` | Using a marketplace template that does not exist | This marketplace template was not found. | Load the category file again, or choose another template. | Medium | 422 |
| `PIM-0956` | Saving a feed with a missing or unusable setting | This feed cannot be saved as it is. | Fix the field named in the message: template, product group, format, destination or recipient. | Medium | 422 |
| `PIM-0957` | Using Ready only on a feed whose template has no channel | Ready only needs a template that belongs to a channel. | Choose a template made for a channel, or turn Ready only off. | Medium | 422 |
| `PIM-0958` | Running a feed that does not exist or is not active | This feed was not found or is not active. | Open Feeds, make the feed Active, and run it again. | Medium | 422 |
| `PIM-0959` | Setting design values with no product group or design chosen | Choose a product group or a design first. | Pick a product group or at least one design, then set the value. | Medium | 422 |
| `PIM-0960` | Setting a design value for an attribute that does not exist | This attribute was not found. | Choose an attribute from the list. | Medium | 422 |
| `PIM-0961` | Setting a design value on a design that is not an item | This design was not found. | Choose a design from the grid; it must be an existing product. | Medium | 422 |
| `PIM-0962` | Setting design values on more designs than one bulk set allows | Too many designs for one bulk set. | Choose a smaller product group, or ask your administrator to raise the bulk edit limit. | Medium | 422 |
| `PIM-0970` | Adding or removing tags in bulk with no action or no tag | Choose add or remove, and enter at least one tag. | Pick the action and type the tags, separated by commas. | Low | 422 |
| `PIM-0971` | Saving a dynamic product group with an attribute condition that does not fit its attribute | One of the conditions cannot be saved. | Use an operator that fits the attribute (for example between for a number, after for a date) and a value of that type. | Low | 422 |
| `PIM-0972` | Making a checklist task with no attribute, an unknown attribute, or opening one on a task without a checklist | This checklist cannot be used. | Choose at least one attribute that exists. | Low | 422 |
| `PIM-0973` | Making a checklist task for something that is not a product or a product group | Choose a product or a product group for this checklist. | Pick the product or product group the attributes are to be filled for. | Low | 422 |
| `PIM-0980` | Attribute value that does not fit its attribute's type | That value does not fit this attribute. | Use Yes or No, pick from the attribute's list, or give a web address starting http:// or https://, as the attribute's type asks. | Medium | 422 |
| `PIM-0981` | Product related to itself | A product cannot be related to itself. | Choose a different related item. | Low | 422 |
| `PIM-0982` | Same product relation entered twice | This relation already exists. | Keep the existing relation, or choose another type or item. | Low | 422 |
| `PIM-1000` | Product sheet without an id column | The sheet needs an id column. | Add a column named id holding each item code, then upload again. | Medium | 422 |
| `PIM-9150` | Attribute stewardship request that is incomplete or names an unknown attribute, user or channel | This stewardship entry is not complete. | Fix the field named, then save the request again. | Medium | 422 |
| `PIM-9151` | Attribute value written from a source other than the attribute's system of record | This value comes from another system. | Write it from the system of record named, or ask for the stewardship entry to change. | Medium | 422 |
| `PIM-9152` | Image order for a channel that names an unknown channel or an image not on the item | This image order cannot be saved. | Choose a channel and only images of this item. | Medium | 422 |

## PIM / Product Publishing

| Code | When it happens | What you see | What to do | Severity | HTTP |
|---|---|---|---|---|---|
| `PIM-0229` | Product family missing | Product family is required before enrichment and publishing. | Select a product family. | High | 422 |
| `PIM-0230` | Product completeness failed | Product is not complete for {channel}/{locale}. Please complete missing fields. | Fill missing fields shown in checklist. | High | 503 |
| `PIM-0231` | Content approval pending | Product content is pending approval and cannot be published yet. | Wait for approval or request review. | High | 200 |
| `PIM-0232` | Primary image missing | Primary product image is required before publishing. | Upload and assign a primary image. | High | 422 |
| `PIM-0233` | Media hash duplicate | This media file already exists and has been linked to the product. | No action required. | Low | 200 |
| `PIM-0234` | Field edit permission denied | You cannot edit this product field. Please contact administrator. | Request access or ask authorized user. | High | 403 |
| `PIM-0235` | Bulk edit partially failed | Some products could not be updated. Please download the error file. | Download error file and correct failed rows. | Medium | 200 |
| `PIM-0236` | Product already queued or published | This product already has an active publish job. Current status: {status}. | Review existing publish job. | Medium | 200 |

## POS / Billing

| Code | When it happens | What you see | What to do | Severity | HTTP |
|---|---|---|---|---|---|
| `POSOFF-0245` | POS session or sale attempted at a location that is not a selling location | This location is not a selling location, so a till cannot sell from it. | Choose the store you are selling from, or ask an administrator to set Sellable = Yes on this Location. | High | 422 |
| `POSOFF-0314` | Free-with-item product billed without its parent item | This item is free only with its parent item, which is not on the bill in the same quantity. | Add the parent item to the bill, or remove the free item. | Medium | 422 |
| `POSOFF-0411` | Non-sellable item put on a bill (ITM-003) | This item is not sold at the till. | Remove it. Packaging is taken out of stock automatically with the sale - use Packaging to change the box. | Medium | 422 |
| `POSOFF-0412` | Promotional item without an eligible promotion (ITM-004) | This promotional gift has not been earned by this bill. | Give it only when the till shows it as eligible (a Gift With Purchase offer), in that quantity. | Medium | 422 |
| `POSOFF-0413` | Stored-value item scanned as merchandise | A gift card is issued, not sold as a bill line. | Use Gift Card -> Issue on the POS screen. | Medium | 422 |
| `POSOFF-0414` | Packaging override reason is required (ITM-011) | Say why the packaging was changed. | Open Packaging and type a reason next to the changed choice. | Low | 422 |
| `POSOFF-0415` | Packaging choice not allowed by its rule | That packaging choice is not allowed here. | Pick one of the options Packaging offers for this line, or keep the default. | Low | 422 |
| `POSOFF-0416` | Insufficient component stock for automatic consumption (ITM-012) | The store is short of a component this sale uses. | Choose another option under Packaging, or receive the stock. Rules set to Allow or Warn let the sale go ahead and list the shortfall under Item Exceptions. | Medium | 422 |
| `POSOFF-0431` | Gift card number not found or not valid | This gift card number is not recognised. | Check the number on the card and scan or type it again. | Medium | 422 |
| `POSOFF-0432` | Gift card number already active or has history (ITM-014) | This gift card has already been issued. | Use a new, unissued card - or Add value, if this card allows reloading. | High | 422 |
| `POSOFF-0433` | Gift card balance cannot become negative (ITM-015) | The gift card does not have that much balance. | Check the balance and take the rest by another payment mode. | High | 422 |
| `POSOFF-0434` | Gift card not active (not issued, blocked, expired, cancelled) | This gift card cannot be used right now. | Check its status under Gift Card -> Balance; a blocked card is released by a manager. | High | 422 |
| `POSOFF-0435` | Gift card amount outside the card's rules | That amount is not allowed for this gift card. | Use one of the card's denominations, or an amount between its minimum and maximum. | Medium | 422 |
| `POSOFF-0436` | Gift card event already processed with different details (ITM-016) | This gift card action was already processed. | Check the card's history before trying again - do not issue or redeem twice. | High | 409 |
| `POSOFF-0604` | Split payment adds up to more than the bill | The split payment is more than the bill. | Open More -> Split payment and lower one of the parts; the Payment Mode takes whatever is left. | Medium | 422 |
| `POSOFF-0605` | Split payment names a payment mode the till does not take | A split payment can only be Cash, Card or UPI. | Use Cash, Card or UPI for each part; pay by gift card or points with their own buttons. | Low | 422 |
| `POSOFF-0606` | Refund cannot go back onto a blocked or cancelled gift card | Part of this refund goes back to a gift card that is blocked or cancelled. | A manager releases the card (Gift Card -> Release a blocked card), then the refund is processed again. | High | 422 |
| `POSOFF-0607` | Cash movement not valid (type, amount, reason, or more than the drawer holds) | This cash movement can't be recorded. | Choose Paid In, Paid Out or Safe Drop, an amount the drawer can cover, and a Cash Movement reason code. | Medium | 422 |
| `POSOFF-0608` | Cash movement or takeover on a session that is not the cashier's open one | There is no open session here for you to use. | Open your own session, or take over the till from the cashier who has it open. | Medium | 422 |
| `POSOFF-0609` | Bill cannot be held (empty, already held, or already being paid) | This bill can't be held. | Add items before holding a bill. A bill that is already held, or already being paid, stays as it is. | Low | 422 |
| `POSOFF-0610` | Held bill already resumed, discarded, or held at another store | That bill is no longer held here. | Refresh the list of held bills - another till may have resumed it. | Low | 409 |
| `POSOFF-0611` | Exchange cannot start (no bill, store or items, or nothing refundable coming back) | This exchange can't be started. | Look up the original bill, choose what is coming back and its condition - Missing, Wrong Item and Rejected items carry no credit. | Medium | 422 |
| `POSOFF-0612` | Exchange credit used, missing, at another store, or offline | This exchange credit can't be used here. | An exchange credit is used once, at the store and online. Check it under Sales & Marketplace -> Returns, or start the exchange again. | High | 422 |
| `POSOFF-0613` | Voucher can no longer be used when the sale completes (used up, expired, another customer's) | The voucher on this bill can't be used any more. | Remove the voucher code from the Coupon box, or ask the customer for another; the bill is priced again without it. | Medium | 422 |
| `POSOFF-0866` | High-value bill without the buyer's details | This bill needs the buyer's details. | Take the customer's mobile number and fill in their address (line 1, state and city or PIN) in Customer details, then complete the sale. | High | 422 |
| `POSOFF-0867` | Bill above the PAN limit without PAN or Form 97 | This bill needs the buyer's PAN. | Add the PAN in Customer details, or tick Form 97 when the buyer has no PAN and has signed the declaration. | High | 422 |
| `POSOFF-0868` | Cash on one bill at or above the legal cash limit | This much cash cannot be taken on one bill. | Take part of the payment by card or UPI (Split payment) so the cash stays under the limit. | High | 422 |
| `POSOFF-0871` | Customer advance missing its customer, amount or a payment mode the till takes | The advance is not complete. | Take the customer's mobile number (not Walk-in), the amount, and Cash, Card or UPI. | Medium | 422 |
| `POSOFF-0872` | Advance not found, used twice on a bill, larger than the bill, or used offline | This advance cannot be used on this bill. | Use the customer's own open advance, once, for no more than the bill - online at the till. | Medium | 422 |
| `POSOFF-0873` | Advance used on another customer's bill | An advance can only be used by the customer who paid it. | Put the customer who paid the advance on the bill, or take another payment. | High | 422 |
| `POSOFF-0874` | Advance refund without a reason or larger than what is left | The advance refund is not complete. | Say why it is refunded, and refund no more than is left on the advance. | Medium | 422 |
| `POSOFF-0875` | Bill paid by a tender the store does not take | This store does not take that tender. | Pick one of the store's tenders, or add it in Setup -> Masters -> POS Tender (and the store to its Stores). | Medium | 422 |
| `POSOFF-0876` | Bill without the salesman the store asks for, or with someone not its salesman | The bill needs one of the store's salesmen. | Pick the salesman on the till. Add or reactivate them in Setup -> Masters -> Salesman. | Medium | 422 |
| `POSOFF-0877` | Bill discount above the reason limit without a reason | This discount needs a reason. | Say why the discount is given; above an approval slab a manager approves it too. | Medium | 422 |
| `POSOFF-1301` | Bill cannot be cancelled (not found, not today's, partly returned, nothing left, or no reason) | This bill can't be cancelled. | Only today's bill, with nothing returned yet, can be cancelled - give the reason. For an older bill, or one already partly returned, take a return instead. | Medium | 422 |
| `POSOFF-1303` | Item not yet released (Draft, Pending Approval or Blocked) scanned at the till | This item can't be sold yet. | It is waiting for its release approval - ask a manager to approve it under Approvals, or sell another item. | Medium | 422 |
| `POSOFF-1304` | Item listed under the store's Items not sold here | This store does not sell this item. | The store's record lists it under Items not sold here - sell it from another store, or ask an administrator to change the list. | Medium | 422 |
| `POSOFF-1309` | Store credit cannot pay (no customer, none or not enough, more than due, above the manager limit, daily uses reached) | Store credit can't pay this. | Store credit is the bill's own customer's: take their number, use no more than they have and the bill needs. Above the limit a store manager switches in. | Medium | 422 |
| `POSOFF-1361` | Offline sale validated with a line not scanned, or scanned with a code that is not that item's barcode | The scanned barcodes don't match. | Every line must have been scanned with one of the item's own barcodes. Check the goods with the cashier; if it can't be put right, reject the sale with the reason. | High | 422 |
| `POSOFF-1362` | Offline sale cannot be decided (already validated or rejected, rung up by the person deciding, or a rejection with no reason) | This offline sale can't be decided. | Only a Pending sale is validated or rejected, by a manager other than the cashier who rang it up; a rejection needs the reason. | Medium | 422 |
| `POSOFF-1363` | Duplicate tender - the same card / UPI / cheque reference and amount was taken at this store minutes ago | This payment was already taken on an earlier bill. | Check the earlier bill. If the customer really paid twice with the same reference, mark the tender as a second payment and try again. | High | 409 |
| `STORE-0854` | Store switch while the user's till session is open at the current store | Your till session is still open at this store. | Close the session (POS -> More... -> Close Session), then switch store. | High | 422 |

## POS / Offline & Cash Drawer

| Code | When it happens | What you see | What to do | Severity | HTTP |
|---|---|---|---|---|---|
| `POSOFF-0237` | POS profile missing | This till has no POS profile yet. | Ask an administrator to set up the POS profile for this store and till, then open the till again. | Critical | 422 |
| `POSOFF-0238` | Cash opening required | Cash opening is required before billing. Please open the shift drawer. | Open the till session and enter the opening cash, then bill. | High | 422 |
| `POSOFF-0239` | Cash closing pending | Cash closing is pending for this shift. Please close the drawer before logout. | Close cash drawer and enter counted cash. | High | 200 |
| `POSOFF-0240` | Drawer variance exceeds tolerance | Cash variance exceeds allowed tolerance. Manager approval is required. | Count the cash again. If the difference stands, enter a reason and ask a manager to approve it. | High | 422 |
| `POSOFF-0241` | Offline invoice sync conflict | Offline bill could not be synced because server data changed. Please review the conflict. | Open the bill's conflict, choose which version to keep, then sync again. | High | 409 |
| `POSOFF-0242` | Duplicate offline invoice UUID | This offline bill was already synced. Duplicate submission was ignored. | No action required unless bill is missing. | Medium | 200 |
| `POSOFF-0243` | Payment terminal not mapped | This till has no card machine linked to it. | Take the payment another way, and ask an administrator to link the card machine in POS settings. | High | 422 |
| `POSOFF-0244` | Payment callback mismatch | Payment confirmation does not match bill amount or reference. Please reconcile manually. | Hold the bill and check the payment on the card machine or UPI app before finishing it. | Critical | 422 |

## Patch / Bug Governance

| Code | When it happens | What you see | What to do | Severity | HTTP |
|---|---|---|---|---|---|
| `PATCH-0292` | Patch proposal pending approval | A system patch proposal is waiting for review. Please approve or reject it. | Review and decide patch proposal. | Medium | 200 |
| `PATCH-0293` | Patch decision already completed | This patch decision is already completed. Please refresh. | Refresh page. | Medium | 409 |
| `PATCH-0294` | Bug reopened after verification | Bug has reopened after verification. Root cause review is required. | Assign owner and add regression test. | High | 200 |

## Payments

| Code | When it happens | What you see | What to do | Severity | HTTP |
|---|---|---|---|---|---|
| `PAY-0262` | Payment proposal approval pending | Payment cannot be released until approval is completed. | Send for approval or wait for approval. | Critical | 422 |
| `PAYMEN-0094` | Duplicate vendor invoice | Vendor invoice number already exists for this vendor. | Correct transaction or send for approval. | High | 409 |
| `PAYMEN-0097` | Payment exceeds payable | Payment amount cannot exceed open payable amount. | Correct transaction or send for approval. | High | 422 |
| `PAYMEN-0098` | UTR missing | UTR number is required to record payment. | Correct transaction or send for approval. | High | 422 |
| `PAYMEN-0099` | Duplicate UTR | UTR number already exists. Please verify payment details. | Correct transaction or send for approval. | High | 409 |
| `PAYMEN-0100` | TDS deduction missing | TDS deduction details are required for this payment as per configuration. | Correct transaction or send for approval. | High | 422 |
| `PAYMEN-0182` | Payment recorded | Payment recorded successfully. | No action required. | Low | 200 |

## Planning

| Code | When it happens | What you see | What to do | Severity | HTTP |
|---|---|---|---|---|---|
| `PLAN-0557` | Supply proposal cannot be generated, approved or rejected as asked | These proposals cannot be processed as asked. | Choose open proposals of one location, the action and what it needs (vendor, source location, an item with a BOM), and a reason to reject. | Medium | 422 |

## Procurement

| Code | When it happens | What you see | What to do | Severity | HTTP |
|---|---|---|---|---|---|
| `PURCHA-5100` | Save a PO routing rule whose approver or escalation role does not exist | That is not a role here. | Pick a role from the list. | Medium | 422 |

## Procurement / RFQ

| Code | When it happens | What you see | What to do | Severity | HTTP |
|---|---|---|---|---|---|
| `PROCUR-0078` | RFQ vendor missing | Please select at least one vendor before releasing the RFQ. | Correct transaction or send for approval. | High | 422 |
| `PROCUR-0079` | RFQ item missing | Please add at least one item before releasing the RFQ. | Correct transaction or send for approval. | High | 422 |
| `PROCUR-0080` | Quotation upload missing | Vendor quotation is required before approval. | Correct transaction or send for approval. | High | 422 |
| `PROCUR-0081` | Quotation cycle exceeded | Maximum quotation revision cycle has been reached. Further revision is not allowed. | Correct transaction or send for approval. | High | 422 |
| `RFQ-0250` | Budget code missing | Budget code is required before requisition submission. | Select budget/cost center. | High | 422 |
| `RFQ-0251` | PR already converted | This requisition is already converted to RFQ/PO. Further changes are not allowed. | Create amendment/change request. | High | 409 |

