---
title: Error code reference (part 2)
section: Troubleshooting
order: 21
summary: Error codes for Goods Receipt to PIM, continued from the error code reference.
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
lists every area and explains how to read a code. It holds **305** codes, from
Goods Receipt to PIM.

## Contents

- [Goods Receipt](#goods-receipt) - 22 codes
- [Goods Receipt / GRN](#goods-receipt-grn) - 8 codes
- [HR & Payroll](#hr-payroll) - 15 codes
- [HR / Payroll](#hr-payroll) - 32 codes
- [Integration / API](#integration-api) - 14 codes
- [Integrations](#integrations) - 1 codes
- [Inventory](#inventory) - 23 codes
- [Inventory / Warehouse](#inventory-warehouse) - 29 codes
- [Item Master](#item-master) - 4 codes
- [Manufacturing](#manufacturing) - 7 codes
- [Manufacturing / Production](#manufacturing-production) - 16 codes
- [Master Data](#master-data) - 46 codes
- [Mobile App / Device](#mobile-app-device) - 7 codes
- [Notifications](#notifications) - 6 codes
- [Observability / Incident](#observability-incident) - 3 codes
- [Omnichannel / OMS](#omnichannel-oms) - 5 codes
- [Order Management](#order-management) - 12 codes
- [PIM](#pim) - 55 codes

## Goods Receipt

| Code | When it happens | What you see | What to do | Severity | HTTP |
|---|---|---|---|---|---|
| `ASN-0271` | ASN line names an item that is not on its purchase order | An item on this ASN is not on its purchase order. | Remove the line, or correct the item code to one the purchase order has; amend the PO first if the vendor is sending something new. | Medium | 422 |
| `GOODSR-0096` | Damaged quantity requires a reason | Enter a damage reason for each damaged receipt line. | Record the observed damage and submit the receipt again. | High | 422 |
| `GOODSR-0097` | Rejected and damaged quantities exceed received quantity | Rejected plus damaged quantity cannot exceed received quantity. | Check the received, rejected and damaged counts; correct the line before submitting. | High | 422 |
| `GOODSR-0318` | One goods receipt given POs of different vendors | A goods receipt can only take purchase orders of the same vendor. | Remove the PO of the other vendor and receive it on its own goods receipt. | High | 422 |
| `GOODSR-0319` | Invoice rate on a receipt line is negative | The invoice rate cannot be negative. | Enter the per-unit price before GST exactly as the vendor's invoice shows it, or leave it blank to use the PO price. | Medium | 422 |
| `GOODSR-0558` | Receipt against an ASN already received or cancelled | This delivery has already been received. | Open the existing goods receipt for this ASN; a new receipt needs a new ASN or none. | Medium | 422 |
| `GOODSR-0940` | Receipt in steps saved without the vendor invoice number, date or total | Enter the vendor invoice number, date and total. | Copy them from the vendor's invoice: the number, the date and the total including GST. | Low | 422 |
| `GOODSR-0941` | The same vendor invoice received a second time | This vendor invoice is already received. | Open the goods receipt named in the message instead of receiving it again. If the vendor really sent two invoices with one number, ask them for the right number. | Medium | 422 |
| `GOODSR-0942` | Invoice total does not agree with the invoice lines by more than Rs 1 | The bill's total and the items on it do not add up. | Fix the highlighted box on Receive - the total or the line it names - then click Save and verify again. | Low | 422 |
| `GOODSR-0943` | Goods beyond the PO or not on it, with no Keep or Return chosen | Choose Keep or Return for the goods beyond the PO. | For each line named, pick Keep (sent for approval) or Return to vendor (goes back on a purchase return). | Low | 422 |
| `GOODSR-0944` | A goods receipt step done out of order, on a posted receipt, or on a one-step receipt | This step cannot be done on this goods receipt now. | Follow the steps in order: Receive, Verify, Price check, MRP and post. After posting, use Correct. | Low | 422 |
| `GOODSR-0945` | A line posted without an MRP | Enter the MRP for every line. | Type the MRP printed on the goods, or click Use suggested, then post again. | Low | 422 |
| `GOODSR-0946` | Posting goods kept beyond the PO before the approval | Keeping the goods beyond the PO is waiting for approval. | Post the other children meanwhile. The approver decides it under My work > Approvals; or go back to Verify and choose Return. | Low | 422 |
| `GOODSR-0947` | A goods receipt step done by a role not listed for it | Your role does not do this step. | Ask someone whose role does it, or an admin to add your role under Configuration > Procurement (Goods receipt: who may ...). | Medium | 403 |
| `GOODSR-0950` | Paying the vendor from a goods receipt with a wrong amount, mode or reference, or nothing due | The payment cannot be made as entered. | Pick Cash, Bank, UPI or Cheque, enter the UTR or cheque number, and pay no more than is due. | Low | 422 |
| `GOODSR-0951` | Post all used when the receipt does not match everywhere | Post all needs everything to match. | Go through the steps one by one: the message lists what differs. | Low | 422 |
| `GOODSR-0952` | A negative or invalid quantity or price on a goods receipt step | Quantities and prices must be zero or more. | Correct the number on the line named. | Low | 422 |
| `GOODSR-1305` | Item listed under the receiving store's Items not stocked here | This store does not stock this item. | Receive it at a store that stocks it, or ask an administrator to change the store's Items not stocked here. | Medium | 422 |
| `GOODSR-1317` | One goods receipt for POs in different currencies | These purchase orders can't be received together. | Receive the POs of each currency on their own goods receipt. | Medium | 422 |
| `GOODSR-1318` | Packing list of a package that is still open | This package is still open, so its packing list can't be printed yet. | Close the package, then print its packing list. | Low | 422 |
| `GOODSR-1330` | Vendor bill paid while a lot of its receipt is still being inspected (148.33) | This bill is for goods still being inspected. | Pay it after the lot is released; pending lots are listed under Inspection lots. | Medium | 422 |
| `PURCHA-5131` | Packs convert to a part-piece on an item that does not allow it (148.35) | That is not a whole number of pieces. | Receive whole packs, or allow part-pieces on the item. | Medium | 422 |

## Goods Receipt / GRN

| Code | When it happens | What you see | What to do | Severity | HTTP |
|---|---|---|---|---|---|
| `GOODSR-0089` | GRN accepted quantity invalid | Accepted quantity cannot be greater than received quantity. | Lower the accepted quantity to no more than was received, then save again. | High | 422 |
| `GOODSR-0090` | Rejected quantity reason missing | Rejection reason is required for rejected quantity. | Choose a rejection reason on each line with a rejected quantity, then save again. | High | 422 |
| `GOODSR-0091` | Barcode generation failed | Barcode could not be generated. Please try again or contact support. | Try again. If it fails again, tell support and quote the reference shown. | High | 503 |
| `GOODSR-0095` | Invoice without GRN | Vendor invoice cannot be posted because GRN is not completed. | Finish and post the goods receipt first, then post the vendor invoice. | High | 422 |
| `GOODSR-0180` | GRN posted | GRN posted successfully. | No action required. | Low | 200 |
| `GRN-0253` | GRN cancellation blocked by downstream invoice | GRN cannot be cancelled because vendor invoice or stock movement exists. | Cancel the documents made from this receipt first (its vendor invoice or stock movement), then cancel the receipt. | Critical | 409 |
| `GRN-0254` | Accepted quantity without barcode | Accepted quantity must have generated barcodes before stock posting. | Generate the barcodes for the accepted quantity, then post again. | High | 422 |
| `GRN-0255` | QC hold pending | Stock is under QC hold and cannot be sold or transferred. | Complete QC approval/rejection. | High | 200 |

## HR & Payroll

| Code | When it happens | What you see | What to do | Severity | HTTP |
|---|---|---|---|---|---|
| `HR-1740` | Appraisal calibrated without a reason | Give the reason for the calibrated rating. | Type why the rating changes, then save. | Medium | 422 |
| `HR-1741` | Calibration by someone who is not HR or the cycle owner, or a calibration field edited directly | Only HR or the cycle owner can calibrate a rating. | Ask HR or the cycle owner to calibrate it with the Calibrate action. | Medium | 422 |
| `HR-1742` | Calibrated rating outside 1 to 5 | The calibrated rating must be between 1 and 5. | Enter a rating from 1 to 5. | Medium | 422 |
| `HR-1743` | Appraisal not found, not approved, or its cycle is closed | Only an approved appraisal in an open cycle can be calibrated. | Approve the appraisal or reopen the cycle first. | Medium | 422 |
| `HRPAYR-1322` | Payslip deductions are more than its gross pay | This payslip's deductions are more than its gross pay. | Check the salary structure, the loan EMI and the statutory rates, then run payroll for the period again. | Medium | 422 |
| `HRPAYR-1323` | Payroll run again for a period whose payslip is already posted | This employee's payslip for the period is already posted. | Open the posted payslip; a correction is made as an adjustment in the next period. | Medium | 422 |
| `HRPAYR-1740` | Slab tax asked for but no tax slabs exist for the regime and year | No tax slabs are set for this regime and financial year. | Add the slabs under Tax Slabs, or turn off tax by slab. | Medium | 422 |
| `HRPAYR-1741` | Form 24Q or ECR asked for with a bad year, quarter, annexure or month | Check the financial year, quarter, annexure or month. | Use a year like 2025-26, a quarter Q1 to Q4, annexure I or II, a month like 2026-04. | Medium | 422 |
| `HRPAYR-1742` | PF ECR asked for a month with no posted payslip carrying PF | No posted payslip with PF ends in that month. | Post the month's payslips first. | Medium | 422 |
| `HRPAYR-1743` | Benefit enrolment ends before it starts | The end date is before the start date. | Correct the dates. | Medium | 422 |
| `HRPAYR-1744` | Benefit plan with a negative amount or a percent above 100 | The benefit amount or percent is not valid. | Enter an amount of zero or more, and a percent up to 100. | Medium | 422 |
| `HRPAYR-1745` | Tax slab with a bad rate or limits | The tax slab is not valid. | Rate must be 0 to 100 and the upper limit above the lower. | Medium | 422 |
| `HRPAYR-1746` | Salary revision with no amount, a negative amount or a bad date | Enter the effective date and at least one new amount. | Fill the date and the amounts, none negative. | Medium | 422 |
| `HRPAYR-1747` | Salary revision for an employee with no active salary structure | This employee has no active salary structure to revise. | Set up the salary structure first. | Medium | 422 |
| `HRPAYR-1748` | Arrears asked for a salary revision that is missing or not approved | Arrears are listed only for an approved salary revision. | Approve the revision first. | Medium | 422 |

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
| `HR-1700` | Position reports to itself or in a loop | That reporting line goes in a circle. | Pick a different reports-to position. | Medium | 422 |
| `HR-1701` | Employee manager is the employee or makes a reporting loop | That manager would report to this person. | Pick a different manager. | Medium | 422 |
| `HR-1702` | Applicant stage not valid, or changed after Hired or Rejected | That stage change is not allowed. | Pick one of Applied, Screening, Interview, Offer or Rejected. A closed applicant stays as it is. | Medium | 422 |
| `HR-1703` | Applicant set to Hired by hand | An applicant becomes Hired when the offer is accepted. | Send the offer and use Accept on it. | Medium | 422 |
| `HR-1704` | Job offer not found or already decided | This offer cannot be accepted. | Open the offer from the list. Only a Draft or Sent offer can be accepted. | Medium | 422 |
| `HR-1705` | Offer accepted while the applicant is not at the Offer stage | The applicant is not at the Offer stage. | Move the applicant to Offer, then accept. | Medium | 422 |
| `HR-1706` | Offer accepted on a job opening with no place left | Every place on this opening is filled. | Raise the number of openings or pick another opening. | Medium | 422 |
| `HR-1710` | Login made from an onboarding checklist that is not complete | Finish every onboarding step first. | Tick the remaining steps on the checklist, then create the login. | Medium | 422 |
| `HR-1711` | Login made for an employee who already has one | This employee already has a login. | Open the user on the Users screen instead. | Medium | 422 |
| `HR-1712` | Login made with a role the client does not have | Pick a valid role for the login. | Choose one of the roles on the Roles screen. Super Admin cannot be given. | Medium | 422 |
| `HR-1713` | Login made for an employee with no email | The employee needs an email. | Add the email to the employee or type it in the form. | Medium | 422 |
| `HR-1720` | Final pay posted while the exit checklist is open (exit clearance setting on) | The exit checklist is still open. | Finish every step of the offboarding checklist, then post the payslip. | Medium | 422 |
| `HR-1730` | Manager decided the leave of someone outside their team (team scope setting on) | This leave is not from your team. | Ask the person's own manager or HR to decide it. | Medium | 403 |
| `HR-1731` | Holiday calendar weekly off day is not a weekday name | A weekly off must be a day name, like Sunday. | Type day names separated by commas. | Medium | 422 |
| `HR-1732` | Holiday calendar date not valid | A holiday date is not valid. | Give each holiday date as YYYY-MM-DD. | Medium | 422 |
| `HR-1733` | Leave applied only on holidays and weekly offs | Every day of this leave is already a day off. | Pick dates that include a working day. | Medium | 422 |
| `HR-1734` | People analytics grouped by something other than department, location or position | Group by department, location or position. | Pick one of the three groups. | Medium | 422 |
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
| `MASTER-1400` | Item merge refused (same item, unit / tax / tracking differ, variants, not Active, open documents, stock not movable) | These items cannot be merged. | Read the reasons listed, fix them (for example finish the open documents) and run the merge again. | Medium | 422 |
| `MASTER-1401` | Item merge attempted without the master-data role | Merging items needs a master-data administrator. | Ask an administrator to merge the items. | Medium | 403 |

## Manufacturing

| Code | When it happens | What you see | What to do | Severity | HTTP |
|---|---|---|---|---|---|
| `MFG-1740` | Operation confirmation would pass the work order quantity | That is more than the order quantity. | Confirm no more than what is left on the operation. | Medium | 422 |
| `MFG-1741` | Operation confirmation with a negative quantity or time | Quantities and minutes cannot be negative. | Enter zero or more. | Low | 422 |
| `MFG-1742` | Operation confirmed for an employee that does not exist | That employee was not found. | Pick an employee from the list, or leave it blank to confirm as yourself. | Low | 422 |
| `MFG-1743` | By-product cost shares on a BOM are negative or add up to more than 100% | The by-product cost shares are over 100%. | Edit the BOM so the shares add up to 100% or less. | Medium | 422 |
| `MFG-1744` | Rework quantity would pass the order quantity | That is more than the order quantity. | Send no more to rework than the order made. | Medium | 422 |
| `MFG-1760` | Lot trace asked for with no lot number | Enter a lot number. | Type the lot number and run the trace again. | Medium | 422 |
| `MFG-1761` | Lot trace direction not Forward, Backward or Both | Pick a trace direction. | Choose Forward, Backward or Both. | Medium | 422 |

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
| `MASTER-1700` | Data-quality problem ignored without a reason | Say why this is ignored. | Add a short reason. | Medium | 422 |
| `MASTER-1701` | Data-quality status not Open, Fixed or Ignored | Status must be Open, Fixed or Ignored. | Pick one of the three. | Medium | 422 |
| `MASTER-1702` | Data-quality problem not found | That problem was not found. | Refresh the list. | Medium | 404 |

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
| `ORDERM-0470` | Payment move not valid for the order's payment state | This payment change cannot be made on the order now. | Check the payment state and amount: a refund or chargeback needs a paid order and at most what was paid; a chargeback needs a reason. | Low | 422 |
| `ORDERM-0471` | Carrier claim step not valid | This claim step cannot be made now. | A claim is filed with the carrier's reference, settled for at most what was claimed, and rejected with a note; only one claim can be open per booking. | Low | 422 |
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
| `PIM-9200` | New purchase order line for a Phase-out or End of life item | This item is being phased out. | Buy the substitute item named in the message, or move the item back to Active on its Item screen. | Medium | 422 |
| `PIM-9201` | Item lifecycle boxes invalid (item is its own substitute, launch date not a date) | The lifecycle details are not valid. | Pick a different substitute item, and give the launch date as YYYY-MM-DD. | Low | 422 |
| `PIM-9202` | End of life item sold while the refuse setting is on | This item is End of life. | Sell the substitute item named in the message, or ask an administrator to turn the refuse setting off (Configuration > Product Information). | Medium | 422 |
| `PIM-9203` | Launch item sold before its launch date while the refuse setting is on | This item has not launched yet. | Wait for the launch date, or change the item's launch date or stage on its Item screen. | Medium | 422 |
| `PIM-9210` | Scheduled item change not allowed (field not on the list, value not valid, date missing or in the past) | That change cannot be scheduled. | Pick selling price, MRP, purchase price, a tax or HSN field or an attribute, give a valid new value, and a date that is today or later. | Low | 422 |
| `PIM-9211` | Two scheduled changes to the same item field on the same day | That field already has a change on that day. | Cancel the other scheduled change, or pick a different date. | Low | 422 |
| `PIM-9212` | Scheduled item change edited or cancelled after it was applied or already cancelled | This change is no longer scheduled. | Only a Scheduled change can be cancelled. Schedule a new change if needed. | Low | 422 |
| `PIM-9240` | New purchase order carrying items outside the store range | These items are outside the store range. | Remove them from the lines, or add them to the store range. | Medium | 422 |
| `PIM-9241` | Transfer into a store carrying items outside its range | These items are outside the store range. | Remove them from the lines, or add them to the store range. | Medium | 422 |
| `PIM-9242` | Sale at a store of an item outside its range | This item is not sold at this store. | Add it to the store range, or sell it elsewhere. | Medium | 422 |
| `PIM-9243` | Channel order line outside the channel range | These items are outside the channel range. | Add them to the channel range, or drop the lines. | Medium | 422 |
| `PIM-9244` | Range row that is incomplete or inconsistent | This range row is not complete. | Name a store or a channel, one item or category, and valid dates. | Medium | 422 |
| `PIM-9245` | Bulk range add with no target or no item or category | Choose where to add and what to add. | Pick a store or channel, and a category or items. | Medium | 422 |
| `PIM-9250` | Markdown plan that is incomplete or inconsistent | This markdown plan is not complete. | Fix the field named, then save again. | Medium | 422 |
| `PIM-9260` | Listing drift asked for with no channel | Choose a channel. | Pick a channel, then open the report again. | Medium | 422 |
| `PIM-9300` | Blank value for a category-required attribute | This attribute is required for the item's category. | Enter a value, or ask an admin to change the category rule. | Medium | 422 |
| `PIM-9301` | Item saved without its category-required attribute values | The item's category needs more attribute values. | Add the values named in the message, then save again. | Medium | 422 |
| `PIM-9302` | Category attribute rule with an unknown rule | The rule must be Required, Optional or Hidden. | Pick one of the three. | Medium | 422 |
| `PIM-9303` | Category attribute rule default value refused | That default value does not fit. | Use a value that fits the attribute, or leave it blank. A hidden attribute has none. | Medium | 422 |
| `PIM-9304` | Second rule for one category and attribute | This category already has a rule for that attribute. | Edit the existing rule instead. | Medium | 422 |
| `PIM-9305` | Number with a unit typed with text | Type the number only. | The unit is added for you. Remove the letters. | Medium | 422 |
| `PIM-9306` | Regulated value changed where it cannot wait for approval | A regulated value changes on the item page. | Open the item and change it there; it waits for approval. | Medium | 422 |
| `PIM-9340` | Myntra market-price collection started while the client switched it off | Collecting Myntra prices is switched off. | Ask an admin to turn it on in Settings, Platform. | Medium | 422 |
| `PIM-9341` | Myntra run with a missing, too long or too many keywords, a bad category or a bad day | This run cannot start. | Fix the field named, then try again. | Medium | 422 |
| `PIM-9342` | Myntra run started while another is waiting or running | A Myntra run is already going. | Wait for it to finish, then start the next one. | Medium | 409 |
| `PIM-9343` | Re-parse of a day with no saved Myntra pages | No saved Myntra pages for that day. | Choose a day that has a run, or start a run first. | Medium | 404 |
| `PIM-9360` | Copying photos to the image drive while the drive is not switched on | The image drive is not switched on. | Switch it on under Settings, Image drive, then copy the photos. | Medium | 422 |
| `PIM-9361` | Starting a photo copy to the image drive while one is already running | A copy is already running. | Wait for it to finish, then start it again if photos are left. | Low | 409 |

