---
title: Error code reference (part 4)
section: Troubleshooting
order: 23
summary: Error codes for Sales / POS to WMS / Logistics, continued from the error code reference.
audience: everyone
last_verified: 2026-09-10
owner: documentation-maintainer
status: active
topic_type: reference
module: platform
task: Error code reference (part 4)
prerequisites: Authorized access to the relevant ERP task
applies_to: source registries; not release acceptance
---

<!-- GENERATED ARTICLE - DO NOT EDIT BY HAND.
     Regenerate: pwsh docs/update-docs.ps1 -Group Content -->

# Error code reference (part 4)

This page continues the [error code reference](error-code-reference.md), which
lists every area and explains how to read a code. It holds **140** codes, from
Sales / POS to WMS / Logistics.

## Contents

- [Sales / POS](#sales-pos) - 40 codes
- [Sales Return](#sales-return) - 9 codes
- [Security](#security) - 9 codes
- [Security / AI assistant access](#security-ai-assistant-access) - 10 codes
- [Security / Rate Limit](#security-rate-limit) - 6 codes
- [Stock Transfer](#stock-transfer) - 17 codes
- [Tax / GST / E-Invoice](#tax-gst-e-invoice) - 15 codes
- [User Access & Security](#user-access-security) - 9 codes
- [Vendor Invoice / AP](#vendor-invoice-ap) - 17 codes
- [WMS](#wms) - 5 codes
- [WMS / Logistics](#wms-logistics) - 3 codes

## Sales / POS

| Code | When it happens | What you see | What to do | Severity | HTTP |
|---|---|---|---|---|---|
| `SALESP-0122` | MRP exceeded | Selling price cannot exceed MRP. | Lower the price to the MRP or below. | High | 422 |
| `SALESP-0123` | Discount exceeds limit | Discount exceeds your allowed limit. Approval is required. | Lower the discount, or ask a manager to approve it. | High | 422 |
| `SALESP-0124` | Coupon invalid | Coupon is invalid, expired, or not applicable for this bill. | Remove the coupon, or check its dates and conditions. | High | 422 |
| `SALESP-0125` | Payment split mismatch | Payment total must match invoice total. | Change the payments so they add up to the bill total. | High | 422 |
| `SALESP-0126` | Card payment reference missing | A card or UPI payment needs its reference number. | Type the reference from the card slip or the UPI app. | High | 422 |
| `SALESP-0127` | Cash limit exceeded | Cash amount exceeds allowed transaction limit. | Take part of the payment by card or UPI so the cash stays under the limit. | High | 422 |
| `SALESP-0128` | Bill cancellation blocked | Bill cannot be cancelled after the allowed cancellation period. | Take a return against the bill instead. | High | 409 |
| `SALESP-0183` | Bill saved | Bill generated successfully. | No action required. | Low | 200 |
| `SALESP-0616` | Change to an issued sale without a reason | Say why this issued sale is being changed. | Enter a reason for the change and save again. The change, who made it and the reason are kept in the Post-Sale Edit History report. | Medium | 422 |
| `SALESP-0620` | Customer price-list row not valid | A price-list row is not valid. | Name an item, a design or a category, give a price above 0, and keep the bands in order. | Low | 422 |
| `SALESP-0621` | Margin hold released by the generic release | This order is on Margin hold, so it cannot be released or held again by hand. | A Store Manager releases it with a reason. | Low | 409 |
| `SALESP-0622` | Margin hold release by a role that may not | Only a Store Manager or an administrator can release a Margin hold. | Ask a Store Manager to release the order. | Low | 403 |
| `SALESP-0623` | Order line below the floor without a reason | A line is below the lowest rate. | Give a reason for the line, or raise the rate. | Low | 422 |
| `SALESP-0624` | Invoice or quote line below the floor without a reason | A line is below the lowest rate. | Give a reason for the line, or raise the rate. | Low | 422 |
| `SALESP-0625` | Price endings on an approved or vendor list | Only a draft customer price list can be changed. | Make a new version of the list. | Low | 409 |
| `SALESP-0626` | Price ending or release reason not valid | Check the ending and the reason. | Use 0, 9, 49, 99 or none, and say why a low rate is accepted. | Low | 422 |
| `SALESP-0710` | Wholesale used while the module is off | Wholesale is off for this company. | Ask an administrator to switch Wholesale on. | Medium | 422 |
| `SALESP-0711` | Bill-to customer is itself, missing, or billed elsewhere | This bill-to customer cannot be used. | Pick another customer, or leave bill-to blank. | Medium | 422 |
| `SALESP-0712` | A summary invoice group cannot be one invoice | This group cannot be one invoice. | Change the grouping on the customer, or invoice the orders one by one. | Medium | 422 |
| `SALESP-0713` | Nothing chosen or nothing to invoice | Nothing to invoice here. | Pick a period that has shipped orders, then choose the groups. | Medium | 422 |
| `SALESP-0714` | Instalment dates on a line do not add up | The instalments do not match the line. | Write each as qty; together they may not pass the line quantity. | Medium | 422 |
| `SALESP-0715` | Drop-ship item without a default vendor | A drop-ship item needs a default vendor. | Set the default vendor on the item. | Medium | 422 |
| `SALESP-0716` | Drop-ship and stock lines on one order | An order is all drop-ship or none. | Put the drop-ship items on their own order. | Medium | 422 |
| `SALESP-0717` | Change refused: the drop-ship purchase order is past Draft | The purchase order is past Draft. | Ask the vendor to change it. | Medium | 409 |
| `SALESP-0718` | A new line on a drop-ship order | A drop-ship order takes no new line. | Use a new order. | Medium | 422 |
| `SALESP-0719` | Drop-ship line already shipped or not a drop-ship line | A chosen line is already shipped. | Refresh the list and choose again. | Medium | 409 |
| `SALESP-0740` | Incentive split is not 100 percent | The salesman split does not add up to 100%. | Change the split so the shares add up to 100% and post again. | Medium | 422 |
| `SALESP-0741` | Incentive salesman not on file | That salesman is not on file. | Pick a salesman from the list. | Medium | 422 |
| `SALESP-0742` | Sales incentives are off | Sales incentives are off. | Ask an admin to turn on Sales incentives. | Medium | 403 |
| `SALESP-0743` | Login is not a salesman | This login is not linked to a salesman. | Ask HR to link your login to your salesman profile. | Medium | 422 |
| `SALESP-0760` | Custom orders used while the module is off | Custom orders are off. | Ask an admin to turn the Custom orders module on. | Low | 403 |
| `SALESP-0761` | No metal rate for today | No rate for today. | Add today's rate for this metal and purity under Metal Rate, then try again. | Medium | 422 |
| `SALESP-0762` | A choice that is not in the masters | Pick a choice from the list. | Pick from the list, or add the choice under Custom choices. | Medium | 422 |
| `SALESP-0763` | Custom order without a weight | Enter the weight. | Enter the weight in grams. | Medium | 422 |
| `SALESP-0764` | Making type without a making charge | This making type has no charge. | Set the making charge on the choice under Custom choices. | Medium | 422 |
| `SALESP-0765` | Metal without HSN or GST | The metal has no HSN or GST. | Set the HSN and GST on the metal under Custom choices. | Medium | 422 |
| `SALESP-0766` | Custom order without customer or address | Enter the customer and address. | Fill both and save again. | Medium | 422 |
| `SALESP-0767` | The item for a custom order was not saved | The item was not saved. | Fix what the message says and try again. No order was made. | Medium | 422 |
| `SALESP-0768` | A custom-order quote typed in by hand | Quotes are made with New order. | Use New order on the Custom order quotes list. | Medium | 422 |
| `SALESR-1400` | Order line for a brand outside the dealer's allowed list | This dealer cannot buy that brand. | Remove the line, or add the brand to the dealer's allowed brands on the customer. | Medium | 422 |

## Sales Return

| Code | When it happens | What you see | What to do | Severity | HTTP |
|---|---|---|---|---|---|
| `SALESR-0129` | Sales return period exceeded | Sales return is not allowed after the configured return period. | Tell the customer the return period has ended, or ask a manager whether an exception can be made. | High | 422 |
| `SALESR-0130` | Return quantity exceeds sold quantity | Return quantity cannot exceed sold quantity. | Lower the return quantity to no more than was sold, less anything already returned. | High | 422 |
| `SALESR-0131` | Original bill required for return | Original bill reference is required for sales return. | Scan or type the original bill number. Without the bill, a manager handles the return. | High | 422 |
| `SALESR-0132` | Refund exceeds return value | Refund amount cannot exceed approved return value. | Lower the refund to the approved return value. | High | 422 |
| `SALESR-0617` | Return or refund approved by the person who raised the return | Another person must approve this. | Ask a colleague or manager to approve it. The business has turned on second-person approval of returns and refunds. | Medium | 422 |
| `SALESR-1307` | Whole purchase returned without its free gift (Gift With Purchase) | The free gift comes back with the purchase. | Everything paid for on this bill is coming back - add the free gift to the return, or keep part of the purchase. An administrator can let customers keep gifts (Configuration > Point of Sale). | Medium | 422 |
| `SALESR-1310` | Store credit chosen for a refund of a bill with no customer | Store credit needs a customer. | This bill was a walk-in sale - refund it the way it was paid. | Low | 422 |
| `SALESR-1315` | A return raised without a reason | A return needs a reason. | Pick a reason or type one. | Low | 422 |
| `SALESR-1316` | Credit note on stock items before the return is received | This credit note needs a received return. | Receive and check in the return first, then post the note. | Low | 422 |

## Security

| Code | When it happens | What you see | What to do | Severity | HTTP |
|---|---|---|---|---|---|
| `SEC-1700` | Retention rule not valid (record type, unit, action or field missing) | The retention rule is not valid. | Fill the record type, a keep period, and the field for a clear-field rule. | Medium | 422 |
| `SEC-1701` | Retention below the statutory floor (audit address 180 days) | That record must be kept longer. | Use the minimum shown in the message or more. | Medium | 422 |
| `SEC-1705` | Erasure of an employee or vendor refused: legal hold | This record is under legal hold. | Lift the hold first, or leave it as it is. | Medium | 422 |
| `SEC-1706` | Erasure of an employee refused: still active | The employee is still active. | Set the exit date and status first. | Medium | 422 |
| `SEC-1707` | Erasure of an employee refused: inside the payroll retention window | Payroll records must be kept longer. | Try again after the date in the message. | Medium | 422 |
| `SEC-1708` | Erasure of a vendor refused: open purchase documents | The vendor has open documents. | Finish or cancel them, then try again. | Medium | 422 |
| `SEC-1709` | Privacy request subject not found or not an employee or vendor | That record was not found. | Check the record and try again. | Medium | 422 |
| `SEC-1710` | Masked copy target is not a sandbox, or is a production client name | A copy can only go into a sandbox client. | Create a new sandbox for the copy. | Medium | 422 |
| `SEC-1711` | Masked copy source missing, unknown or the same as the target | Pick a different client to copy from. | Choose the client to copy. | Medium | 422 |

## Security / AI assistant access

| Code | When it happens | What you see | What to do | Severity | HTTP |
|---|---|---|---|---|---|
| `SEC-0300` | Assistant call by a user who is disabled or gone | This assistant can no longer act. | Ask an administrator to turn the user back on, or make a new assistant client for another user. | High | 403 |
| `SEC-0301` | Assistant asked for a store its user may not work at | This user cannot see that store. | Ask for a store the user works at, or leave the store out to get all of theirs. | High | 403 |
| `SEC-0302` | Assistant client not tied to a user | This client is not set up as an assistant. | Make the client under Admin, AI assistants, so it acts as one named user. | High | 403 |
| `SEC-0303` | Assistant draft order not valid | The draft order is not complete. | Send at least one line with a known item and a quantity above zero. | Medium | 422 |
| `SEC-0304` | Assistant client not valid at issue | The assistant client cannot be made. | Pick an active user and only the read and draft choices. | Medium | 422 |
| `SEC-0305` | Order not found for the assistant | No such order. | Check the order number. Only orders at the user's stores are shown. | Low | 404 |
| `SEC-0306` | Submitting something that is not an open assistant draft | This is not an open assistant draft. | Pick a draft made by an assistant that is still a draft. | Medium | 422 |
| `SEC-0307` | Report not found for the assistant | No such report. | Ask for a report from the list. | Low | 404 |
| `SEC-0308` | Report refused for the assistant's role | This user's role cannot run that report. | Pick a report the list shows for this user. | High | 403 |
| `SEC-0309` | Assistant tied to a user who has full access | This assistant cannot act as an Admin. | Make a new assistant for a user with a narrower role. | High | 403 |

## Security / Rate Limit

| Code | When it happens | What you see | What to do | Severity | HTTP |
|---|---|---|---|---|---|
| `SEC-0280` | Rate limit exceeded | Too many requests. Please wait and try again. | Wait and retry later. | High | 429 |
| `SEC-0281` | Payload size exceeded | Request is too large. Please reduce file/data size and try again. | Reduce payload size. | High | 422 |
| `SEC-0282` | Suspicious activity blocked | Suspicious activity detected. Action was blocked for security review. | Contact administrator. | Critical | 409 |
| `SEC-0283` | Token purpose invalid | This verification token is not valid for this action. Please restart the process. | Restart login/authorized process. | High | 401 |
| `SEC-0284` | Origin not allowed | This browser origin is not allowed to access the ERP API. | Use approved ERP URL. | Critical | 422 |
| `SEC-0290` | A second active API access for a purpose another already holds | Another active API access already has this purpose. | Rotate that access instead, revoke it first, or give this one a different purpose. | Medium | 409 |

## Stock Transfer

| Code | When it happens | What you see | What to do | Severity | HTTP |
|---|---|---|---|---|---|
| `STOCKT-0111` | Transfer source destination same | Source and destination location cannot be the same. | Choose a different location to send the stock to. | High | 422 |
| `STOCKT-0112` | Transfer quantity exceeds available | Transfer quantity cannot exceed available stock. | Lower the quantity to what the sending location has. | High | 422 |
| `STOCKT-0113` | Transfer receipt pending | Stock transfer cannot be closed until receipt is completed. | Receive the transfer at the receiving store first, then close it. | High | 422 |
| `STOCKT-0481` | Direct transfer approval not allowed (D04) | This transfer cannot be approved directly. | Submit it for approval - someone other than its creator approves it. | Medium | 422 |
| `STOCKT-0482` | Batch-tracked transfer line cannot be traced to a lot | The lots for this item could not be found. | Name the batch on the line, or put the stock away against its lot first. | Medium | 422 |
| `STOCKT-0483` | Serial numbers on a transfer do not match | The serial numbers do not match this transfer. | Scan one serial number per unit; each must be in stock at the sending store, or in transit on this transfer. | Medium | 422 |
| `STOCKT-0484` | Transfer discrepancy, return or cancel without a valid reason code | Choose a reason for this. | Pick an active reason code (Masters > Reason Codes: Transfer Discrepancy, or Cancellation to cancel). | Medium | 422 |
| `STOCKT-0485` | Transfer receipt or return exceeds what is in transit | That is more than is still in transit on this transfer. | Enter at most the quantity still in transit; record extra units as excess with a reason. | Medium | 422 |
| `STOCKT-0486` | Transfer cannot be cancelled or returned in its current status | This transfer cannot be changed that way now. | Cancel only before dispatch; after dispatch, return what is still in transit to the source. | Medium | 422 |
| `STOCKT-0487` | Allocation run ID already used | This allocation run ID was already used. | Use a new run ID, or open the existing run to see its transfers. | Medium | 422 |
| `STOCKT-0488` | Allocation file or parameters invalid | The allocation file cannot be used. | Give a run ID, a source store and a file with store, sku and qty columns; fix the rows listed. | Medium | 422 |
| `STOCKT-0489` | Source cannot fill the minimum share of the transfer | The source cannot fill enough of this transfer to ship it. | Wait for stock, lower the minimum fill, or cancel the transfer. | Medium | 422 |
| `STOCKT-0490` | Complete-fill transfer waits for stock | This transfer waits until every line can be filled. | Receive or free the short stock, then dispatch again; the lines that are short are listed. | Medium | 422 |
| `STOCKT-0491` | Transfer cannot join the wave | This transfer cannot be added to that wave. | Use an Approved or Packed transfer from the wave's location, into a wave that is not closed, with transfer waves turned on. | Medium | 422 |
| `STOCKT-0492` | Allocation run not found or nothing to act on | That allocation run has nothing to do. | Check the run ID; submit applies to Drafts, approve to transfers waiting for approval. | Medium | 422 |
| `STOCKT-0493` | Fill policy value not recognised | That fill policy is not valid. | Choose Exact, Ship what is there or Complete. | Medium | 422 |
| `TRN-0259` | Transfer receiving mismatch | Received quantity does not match dispatched quantity. Shortage/damage reason is required. | Enter shortage/damage reason and submit exception. | High | 200 |

## Tax / GST / E-Invoice

| Code | When it happens | What you see | What to do | Severity | HTTP |
|---|---|---|---|---|---|
| `TAX-0264` | IRN cancellation window expired | IRN cannot be cancelled because the allowed cancellation window has expired. | Create credit note/reversal as per process. | Critical | 422 |
| `TAX-0265` | Vehicle or transporter details missing | Vehicle/transporter details are required before e-way bill generation. | Enter transport details. | High | 422 |
| `TAX-0266` | GST return already filed | GST return is already filed for this period. Changes are blocked. | Use correction in next period or approved reversal. | Critical | 409 |
| `TAXGST-0067` | GSTIN missing | GSTIN is required for this GST transaction. | Correct GST details or retry API. | High | 422 |
| `TAXGST-0068` | Invalid GSTIN state code | GSTIN state code does not match the selected state. Please verify GSTIN and state. | Correct GST details or retry API. | High | 422 |
| `TAXGST-0069` | Place of supply missing | Place of Supply is required for GST calculation. | Correct GST details or retry API. | High | 422 |
| `TAXGST-0070` | Tax rate missing | GST rate is missing for one or more items. Please check tax configuration. | Correct GST details or retry API. | High | 422 |
| `TAXGST-0071` | CGST/SGST vs IGST mismatch | Tax type does not match the place of supply. Please verify billing state and supply state. | Correct GST details or retry API. | High | 422 |
| `TAXGST-0072` | E-invoice IRN generation failed | IRN generation failed. Please review the error details and try again. | Correct GST details or retry API. | High | 503 |
| `TAXGST-0073` | E-invoice already generated | IRN is already generated for this invoice. Duplicate IRN generation is not allowed. | Correct GST details or retry API. | High | 409 |
| `TAXGST-0074` | E-way bill required | E-way bill is required for this transaction before dispatch. | Correct GST details or retry API. | High | 422 |
| `TAXGST-0075` | Taxable value mismatch | Taxable value does not match item value after discount and charges. Please recalculate. | Correct GST details or retry API. | High | 422 |
| `TAXGST-0076` | GST round off mismatch | GST total has a rounding difference beyond allowed tolerance. | Correct GST details or retry API. | High | 422 |
| `TAXGST-0077` | GSTR data locked | GST return data is locked for the selected period. Changes are not allowed. | Correct GST details or retry API. | High | 409 |
| `TAXGST-0186` | IRN generated | IRN generated successfully. | No action required. | Low | 200 |

## User Access & Security

| Code | When it happens | What you see | What to do | Severity | HTTP |
|---|---|---|---|---|---|
| `USERAC-0021` | Invalid login | Invalid username or password. Please check and try again. | Contact admin or correct input. | High | 401 |
| `USERAC-0022` | Account locked | Your account is locked due to multiple failed login attempts. Please contact your administrator. | Contact admin or correct input. | High | 409 |
| `USERAC-0023` | Password expired | Your password has expired. Please reset your password to continue. | Contact admin or correct input. | High | 422 |
| `USERAC-0024` | Weak password | Password does not meet the required policy. Please use the required length, complexity, and special characters. | Contact admin or correct input. | High | 422 |
| `USERAC-0025` | MFA failed | Verification failed. Please enter the correct verification code. | Contact admin or correct input. | High | 401 |
| `USERAC-0026` | Role not mapped | No role is assigned to this user. Please contact your administrator. | Contact admin or correct input. | High | 422 |
| `USERAC-0027` | Store access missing | You do not have access to this store. Please contact your administrator. | Contact admin or correct input. | High | 403 |
| `USERAC-0028` | Company/state access missing | You do not have access to this company or state. Please contact your administrator. | Contact admin or correct input. | High | 403 |
| `USERAC-0029` | Maker-checker violation | The same user cannot create and approve this transaction. | Contact admin or correct input. | High | 403 |

## Vendor Invoice / AP

| Code | When it happens | What you see | What to do | Severity | HTTP |
|---|---|---|---|---|---|
| `VENDOR-0092` | Invoice amount mismatch | Invoice amount does not match PO/GRN value within allowed tolerance. | Correct transaction or send for approval. | High | 422 |
| `VENDOR-0093` | Invoice quantity mismatch | Invoice quantity does not match accepted GRN quantity. | Correct transaction or send for approval. | High | 422 |
| `VENDOR-0096` | Three-way match failed | Three-way matching failed between PO, GRN, and Invoice. Please review differences. | Correct transaction or send for approval. | High | 503 |
| `VENDOR-0181` | Invoice posted | Vendor invoice posted successfully. | No action required. | Low | 200 |
| `VENDOR-0190` | Purchase order for a vendor held from new orders | This vendor is on hold for new purchase orders. | Take the hold off the vendor, or choose another vendor. | Medium | 422 |
| `VENDOR-0191` | Payment to a vendor held from payment | This vendor is on hold for payment. | Take the hold off the vendor, then pay again. | Medium | 422 |
| `VENDOR-0192` | Payment to a vendor with a change waiting for approval | A change to this vendor is waiting for approval. | Ask an approver to decide the change in Approvals; payments resume then. | Medium | 422 |
| `VENDOR-0300` | Purchase order for a vendor not yet approved | This vendor is not approved yet. | Approve the vendor first, or choose another vendor. | Medium | 422 |
| `VENDOR-0301` | Purchase order line for a vendor not on the approved vendor list | This vendor is not on the approved list for that item. | Pick one of the approved vendors, or add this vendor to the approved vendor list. | Medium | 422 |
| `VENDOR-0302` | Vendor onboarding request that cannot be filed | This vendor cannot be sent for a decision. | Check the vendor status and that vendor onboarding is switched on. | Medium | 422 |
| `VENDOR-0303` | Vendor approval requested with the documents checklist incomplete | The documents checklist is not complete. | Tick GSTIN, PAN and bank proof on the vendor, then ask again. | Medium | 422 |
| `VENDOR-0304` | Vendor onboarding decision already waiting | A decision for this vendor is already waiting. | Wait for the approver, or open Approvals. | Medium | 422 |
| `VENDOR-0305` | Approved vendor entry without exactly one of item or category | Give an item or a category, not both. | Fill one of the two fields. | Medium | 422 |
| `VENDOR-0306` | Approved vendor entry already listed | This vendor is already listed there. | Open the existing entry instead. | Medium | 422 |
| `VENDOR-0310` | Supplier notice for an order that is not open | A notice can only be sent for an open order. | Pick an order that is still open. | Medium | 422 |
| `VENDOR-0311` | Supplier notice quantity above what is still open | That is more than the order still has open. | Lower the quantity to the open amount. | Medium | 422 |
| `VENDOR-0312` | Supplier notice with a bad line or date | Check the lines and the date. | Give each line an item and a quantity, and the date as YYYY-MM-DD. | Medium | 422 |

## WMS

| Code | When it happens | What you see | What to do | Severity | HTTP |
|---|---|---|---|---|---|
| `WMS-1311` | Pick confirmation or wave sortation without a wave / line / station, or a wave with no open orders | Choose the wave first. | Pick the wave (and the sort station) - a wave with no open orders has nothing to sort. | Low | 422 |
| `WMS-1312` | Bin or zone block with an unknown kind or no reason | Say why it is blocked. | Choose No receipts or No picks and give a short reason - or clear the block. | Low | 422 |
| `WMS-1313` | Stock put into, moved into or received into a bin or zone blocked for receipts | This bin is closed to receipts. | Choose another bin, or ask a store manager to lift the block on the bin or its zone. | Medium | 422 |
| `WMS-1314` | Put-away rule naming more than one of item, design and category, no zone or bin, or something that does not exist | This put-away rule is not complete. | Name one of item, design or category (or none for a location default) and the zone or bin the stock goes to. | Low | 422 |
| `WMS-1315` | Bin move request refused (work queue off, bins or item missing, same bin, or not enough stock in the from bin) | This move can't be requested. | Check the two bins, the item and the quantity; the work queue must be on (Configuration > Warehouse). | Low | 422 |

## WMS / Logistics

| Code | When it happens | What you see | What to do | Severity | HTTP |
|---|---|---|---|---|---|
| `WMSLOG-0137` | Shipment partner missing | Logistics partner is required before dispatch. | Choose the courier for the shipment, then dispatch. | High | 422 |
| `WMSLOG-0138` | AWB generation failed | AWB generation failed. Please review courier response and try again. | Read the courier's reason below, correct the shipment details, then try again. | High | 503 |
| `WMSLOG-0139` | Delivery status update failed | Delivery status could not be updated. Please retry or check integration. | Try again in a few minutes. If it keeps failing, tell your administrator the courier link may be down. | High | 503 |

