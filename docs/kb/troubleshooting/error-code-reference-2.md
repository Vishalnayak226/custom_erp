---
title: Error code reference (part 2)
section: Troubleshooting
order: 21
summary: Error codes for POS / Offline & Cash Drawer to WMS / Logistics, continued from the error code reference.
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
lists every area and explains how to read a code. It holds **133** codes, from
POS / Offline & Cash Drawer to WMS / Logistics.

## Contents

- [POS / Offline & Cash Drawer](#pos-offline-cash-drawer) - 8 codes
- [Patch / Bug Governance](#patch-bug-governance) - 3 codes
- [Payments](#payments) - 7 codes
- [Planning](#planning) - 1 codes
- [Procurement / RFQ](#procurement-rfq) - 6 codes
- [Purchase Order](#purchase-order) - 14 codes
- [Purchase Return](#purchase-return) - 1 codes
- [Purchase Return / RTV](#purchase-return-rtv) - 5 codes
- [Quality & Maintenance](#quality-maintenance) - 6 codes
- [RFQ](#rfq) - 6 codes
- [Reports & Exports](#reports-exports) - 8 codes
- [SaaS / Multi-Tenant](#saas-multi-tenant) - 8 codes
- [Sales / POS](#sales-pos) - 9 codes
- [Sales Return](#sales-return) - 5 codes
- [Security / Rate Limit](#security-rate-limit) - 5 codes
- [Stock Transfer](#stock-transfer) - 10 codes
- [Tax / GST / E-Invoice](#tax-gst-e-invoice) - 15 codes
- [User Access & Security](#user-access-security) - 9 codes
- [Vendor Invoice / AP](#vendor-invoice-ap) - 4 codes
- [WMS / Logistics](#wms-logistics) - 3 codes

## POS / Offline & Cash Drawer

| Code | When it happens | What you see | What to do | Severity | HTTP |
|---|---|---|---|---|---|
| `POSOFF-0237` | POS profile missing | POS profile is not configured for this store/register. | Configure POS profile before billing. | Critical | 422 |
| `POSOFF-0238` | Cash opening required | Cash opening is required before billing. Please open the shift drawer. | Open cash drawer session. | High | 422 |
| `POSOFF-0239` | Cash closing pending | Cash closing is pending for this shift. Please close the drawer before logout. | Close cash drawer and enter counted cash. | High | 200 |
| `POSOFF-0240` | Drawer variance exceeds tolerance | Cash variance exceeds allowed tolerance. Manager approval is required. | Enter reason and request approval. | High | 422 |
| `POSOFF-0241` | Offline invoice sync conflict | Offline bill could not be synced because server data changed. Please review the conflict. | Review conflict and choose server/client action. | High | 409 |
| `POSOFF-0242` | Duplicate offline invoice UUID | This offline bill was already synced. Duplicate submission was ignored. | No action required unless bill is missing. | Medium | 200 |
| `POSOFF-0243` | Payment terminal not mapped | Payment terminal is not mapped to this POS register. | Map terminal in POS settings. | High | 422 |
| `POSOFF-0244` | Payment callback mismatch | Payment confirmation does not match bill amount or reference. Please reconcile manually. | Hold bill and reconcile payment. | Critical | 422 |

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

## Procurement / RFQ

| Code | When it happens | What you see | What to do | Severity | HTTP |
|---|---|---|---|---|---|
| `PROCUR-0078` | RFQ vendor missing | Please select at least one vendor before releasing the RFQ. | Correct transaction or send for approval. | High | 422 |
| `PROCUR-0079` | RFQ item missing | Please add at least one item before releasing the RFQ. | Correct transaction or send for approval. | High | 422 |
| `PROCUR-0080` | Quotation upload missing | Vendor quotation is required before approval. | Correct transaction or send for approval. | High | 422 |
| `PROCUR-0081` | Quotation cycle exceeded | Maximum quotation revision cycle has been reached. Further revision is not allowed. | Correct transaction or send for approval. | High | 422 |
| `RFQ-0250` | Budget code missing | Budget code is required before requisition submission. | Select budget/cost center. | High | 422 |
| `RFQ-0251` | PR already converted | This requisition is already converted to RFQ/PO. Further changes are not allowed. | Create amendment/change request. | High | 409 |

## Purchase Order

| Code | When it happens | What you see | What to do | Severity | HTTP |
|---|---|---|---|---|---|
| `PO-0252` | PO amendment blocked after full GRN | PO cannot be amended after full receipt. Create an adjustment or return process. | Use approved adjustment/return process. | High | 409 |
| `PURCHA-0082` | PO approval pending | PO cannot be released until approval is completed. | Correct transaction or send for approval. | High | 422 |
| `PURCHA-0083` | PO amount exceeds approval limit | PO amount exceeds your approval limit. Please send it for higher approval. | Correct transaction or send for approval. | High | 403 |
| `PURCHA-0084` | PO closed | This PO is closed. No further changes are allowed. | Correct transaction or send for approval. | High | 409 |
| `PURCHA-0085` | PO amendment reason missing | Please enter amendment reason before changing the PO. | Correct transaction or send for approval. | High | 422 |
| `PURCHA-0086` | PO amendment approval pending | PO amendment cannot be released until approval is completed. | Correct transaction or send for approval. | High | 422 |
| `PURCHA-0087` | PO quantity exceeded in GRN | Received quantity cannot exceed open PO quantity unless excess receipt is approved. | Correct transaction or send for approval. | High | 422 |
| `PURCHA-0088` | GRN item not in PO | This item is not part of the selected PO. Please verify the item. | Correct transaction or send for approval. | High | 422 |
| `PURCHA-0179` | PO released | PO released successfully. | No action required. | Low | 200 |
| `PURCHA-0320` | Deleting a purchase order that goods have been received against | This purchase order cannot be deleted - goods have already been received against it. | Cancel the goods receipt first (or return the goods with a Purchase Return); the purchase order can then be deleted. | Medium | 422 |
| `PURCHA-0487` | Short-close without a valid reason code | Choose why the rest of this order is being closed. | Pick an active reason code of category Cancellation (Masters > Reason Codes). | Medium | 422 |
| `PURCHA-0488` | Short-close not possible in the PO's state | This purchase order cannot be short-closed now. | Only an Approved order with units still open can be short-closed. | Medium | 422 |
| `PURCHA-0489` | Receipt or amendment on a closed (short-closed) PO | This purchase order is closed. | Raise a new purchase order for anything still needed. | High | 422 |
| `PURCHA-0621` | Sending a cancelled (e.g. auto-cancelled) purchase order to the vendor | This purchase order is cancelled. | Raise a new purchase order for anything still needed. | Medium | 422 |

## Purchase Return

| Code | When it happens | What you see | What to do | Severity | HTTP |
|---|---|---|---|---|---|
| `PURCHA-0620` | Returning accepted goods after the purchase return window | The return window for this goods receipt has passed. | Accepted goods can be returned only within the days set in Configuration > Procurement > Purchase return window. Rejected and damaged goods can still be returned. | Medium | 422 |

## Purchase Return / RTV

| Code | When it happens | What you see | What to do | Severity | HTTP |
|---|---|---|---|---|---|
| `PURCHA-0116` | RTV reason missing | Return to Vendor reason is required. | Correct RTV details. | Medium | 422 |
| `PURCHA-0117` | RTV quantity exceeds rejected | RTV quantity cannot exceed rejected/returnable quantity. | Correct RTV details. | Medium | 422 |
| `PURCHA-0118` | RTV without GRN | RTV must be created against a valid GRN or accepted returnable stock. | Correct RTV details. | Medium | 422 |
| `PURCHA-0119` | RTV already created | RTV is already created for the selected rejected quantity. | Correct RTV details. | Medium | 409 |
| `PURCHA-0120` | Credit note pending | Vendor credit note is pending for this RTV. | Correct RTV details. | Medium | 422 |

## Quality & Maintenance

| Code | When it happens | What you see | What to do | Severity | HTTP |
|---|---|---|---|---|---|
| `QUALIT-0561` | Inspection source, item, location or quantity missing or unknown | This inspection is missing what it inspects. | Choose Production, Incoming or Outgoing, the order or GRN it is for, the item (one received on that GRN), the location and the quantity inspected. | Medium | 422 |
| `QUALIT-0562` | Inspection readings incomplete or plan unusable | The readings do not complete this inspection. | Record a reading marked Pass or Fail for every parameter of the plan (the plan must be active and for this item), or at least one reading without a plan. | Medium | 422 |
| `QUALIT-0563` | Inspection disposition not allowed | This decision cannot be recorded on this inspection. | Accept a passed inspection; hold, rework or concede a failed one; production inspections go to Submit for approval. Holding stock needs a Quality hold code and enough stock at the location. | Medium | 422 |
| `QUALIT-0564` | Concession without a note or by the inspector | A concession needs a reason and a second person. | Write why the stock can be used, and have someone other than the inspector record the concession. | Medium | 422 |
| `QUALIT-0565` | Non-conformance step out of order or missing owner/evidence | This non-conformance step cannot be recorded yet. | Investigate first, then plan the action with an owner, then close it with the evidence the action was done. | Medium | 422 |
| `QUALIT-0566` | Maintenance order step out of order or completion incomplete | This maintenance step cannot be recorded yet. | Schedule a draft order, start a scheduled one, and complete it only with notes, every checklist task ticked and parts with quantities. | Medium | 422 |

## RFQ

| Code | When it happens | What you see | What to do | Severity | HTTP |
|---|---|---|---|---|---|
| `RFQ-0551` | RFQ line incomplete or no lines | An RFQ line is incomplete. | Give each line an item or a description, a unit and a quantity above zero. | Medium | 422 |
| `RFQ-0552` | RFQ not open for this change | This RFQ is not open for that. | Lines change only on a Draft; vendors, quotes and responses only while it is Draft or Sent. | Medium | 422 |
| `RFQ-0553` | Vendor not active, not invited, or already quoted | That vendor cannot be used here. | Choose an active vendor; a vendor that already quoted cannot decline. | Medium | 422 |
| `RFQ-0554` | Quote line or terms invalid | This quote cannot be recorded as entered. | Price at least one line; give the unit conversion when the vendor quotes in another unit, a validity date that is not over, and no negative amounts. | Medium | 422 |
| `RFQ-0555` | RFQ already awarded, or quote not awardable | This RFQ has already been awarded, or that quote cannot be. | Open the purchase order created by the award; a second award is refused. | Medium | 422 |
| `RFQ-0556` | Awarded quote cannot become a purchase order | The quote cannot become a purchase order yet. | Pick an item on every quoted RFQ line, use whole quantities, and award a quote that is still valid. | Medium | 422 |

## Reports & Exports

| Code | When it happens | What you see | What to do | Severity | HTTP |
|---|---|---|---|---|---|
| `REPORT-0160` | Report no data | No data found for the selected filters. Please change filters and try again. | Correct details or retry. Contact admin if repeated. | Medium | 422 |
| `REPORT-0161` | Export limit exceeded | Export limit exceeded. Please narrow the filters and try again. | Correct details or retry. Contact admin if repeated. | Medium | 422 |
| `REPORT-0162` | Report generation failed | Report could not be generated. Please try again or contact support. | Correct details or retry. Contact admin if repeated. | Medium | 503 |
| `REPORT-0184` | Export started | Export is being prepared. You can download it once ready. | Wait for download link. | Low | 200 |
| `REPORT-0285` | Export job failed | Export job failed. Please review filters or contact support. | Narrow filters or retry export. | High | 503 |
| `REPORT-0286` | Export download link expired | Download link has expired. Please generate the export again. | Generate export again. | Low | 200 |
| `REPORT-0287` | Sensitive column masked | Some sensitive fields are masked based on your access level. | Request additional access if required. | Low | 200 |
| `REPORT-0288` | Report query timeout | Report is taking too long. Please reduce filters or run as background export. | Narrow filters or start background export. | High | 503 |

## SaaS / Multi-Tenant

| Code | When it happens | What you see | What to do | Severity | HTTP |
|---|---|---|---|---|---|
| `SAAS-0188` | Tenant not resolved | Company workspace could not be identified. Please sign in again or contact support. | Sign in again; admin must verify tenant mapping. | Critical | 422 |
| `SAAS-0189` | Cross-tenant access attempt | You cannot access data outside your company workspace. | Use your assigned workspace or contact admin. | Critical | 403 |
| `SAAS-0190` | Tenant provisioning failed | Tenant setup could not be completed. Please review the setup log and retry. | Retry after fixing the setup issue. | Critical | 503 |
| `SAAS-0191` | Module disabled for tenant | This module is not enabled for your company plan. Please contact administrator. | Ask admin to enable the module if required. | High | 403 |
| `SAAS-0192` | Feature flag disabled | This feature is not enabled for your company yet. | Contact admin or use the enabled process. | Medium | 200 |
| `SAAS-0193` | Subscription limit exceeded | Your plan limit has been reached. Please upgrade or disable unused records. | Upgrade plan or reduce usage. | High | 422 |
| `SAAS-0194` | Industry switch blocked after transactions | Industry profile cannot be changed after transactions are posted. | Create a new tenant or archive data before switching. | High | 409 |
| `SAAS-0195` | Tenant version mismatch | Tenant schema version does not match the running application version. Please run migration or rollback. | Contact tech admin to migrate or rollback. | High | 200 |

## Sales / POS

| Code | When it happens | What you see | What to do | Severity | HTTP |
|---|---|---|---|---|---|
| `SALESP-0122` | MRP exceeded | Selling price cannot exceed MRP. | Correct details or request approval. | High | 422 |
| `SALESP-0123` | Discount exceeds limit | Discount exceeds your allowed limit. Approval is required. | Correct details or request approval. | High | 422 |
| `SALESP-0124` | Coupon invalid | Coupon is invalid, expired, or not applicable for this bill. | Correct details or request approval. | High | 422 |
| `SALESP-0125` | Payment split mismatch | Payment total must match invoice total. | Correct details or request approval. | High | 422 |
| `SALESP-0126` | Card payment reference missing | Payment reference is required for card/UPI transaction. | Correct details or request approval. | High | 422 |
| `SALESP-0127` | Cash limit exceeded | Cash amount exceeds allowed transaction limit. | Correct details or request approval. | High | 422 |
| `SALESP-0128` | Bill cancellation blocked | Bill cannot be cancelled after the allowed cancellation period. | Correct details or request approval. | High | 409 |
| `SALESP-0183` | Bill saved | Bill generated successfully. | No action required. | Low | 200 |
| `SALESP-0616` | Change to an issued sale without a reason | Say why this issued sale is being changed. | Enter a reason for the change and save again. The change, who made it and the reason are kept in the Post-Sale Edit History report. | Medium | 422 |

## Sales Return

| Code | When it happens | What you see | What to do | Severity | HTTP |
|---|---|---|---|---|---|
| `SALESR-0129` | Sales return period exceeded | Sales return is not allowed after the configured return period. | Correct details or request approval. | High | 422 |
| `SALESR-0130` | Return quantity exceeds sold quantity | Return quantity cannot exceed sold quantity. | Correct details or request approval. | High | 422 |
| `SALESR-0131` | Original bill required for return | Original bill reference is required for sales return. | Correct details or request approval. | High | 422 |
| `SALESR-0132` | Refund exceeds return value | Refund amount cannot exceed approved return value. | Correct details or request approval. | High | 422 |
| `SALESR-0617` | Return or refund approved by the person who raised the return | Another person must approve this. | Ask a colleague or manager to approve it. The business has turned on second-person approval of returns and refunds. | Medium | 422 |

## Security / Rate Limit

| Code | When it happens | What you see | What to do | Severity | HTTP |
|---|---|---|---|---|---|
| `SEC-0280` | Rate limit exceeded | Too many requests. Please wait and try again. | Wait and retry later. | High | 429 |
| `SEC-0281` | Payload size exceeded | Request is too large. Please reduce file/data size and try again. | Reduce payload size. | High | 422 |
| `SEC-0282` | Suspicious activity blocked | Suspicious activity detected. Action was blocked for security review. | Contact administrator. | Critical | 409 |
| `SEC-0283` | Token purpose invalid | This verification token is not valid for this action. Please restart the process. | Restart login/authorized process. | High | 401 |
| `SEC-0284` | Origin not allowed | This browser origin is not allowed to access the ERP API. | Use approved ERP URL. | Critical | 422 |

## Stock Transfer

| Code | When it happens | What you see | What to do | Severity | HTTP |
|---|---|---|---|---|---|
| `STOCKT-0111` | Transfer source destination same | Source and destination location cannot be the same. | Select valid stock/location or request approval. | High | 422 |
| `STOCKT-0112` | Transfer quantity exceeds available | Transfer quantity cannot exceed available stock. | Select valid stock/location or request approval. | High | 422 |
| `STOCKT-0113` | Transfer receipt pending | Stock transfer cannot be closed until receipt is completed. | Select valid stock/location or request approval. | High | 422 |
| `STOCKT-0481` | Direct transfer approval not allowed (D04) | This transfer cannot be approved directly. | Submit it for approval - someone other than its creator approves it. | Medium | 422 |
| `STOCKT-0482` | Batch-tracked transfer line cannot be traced to a lot | The lots for this item could not be found. | Name the batch on the line, or put the stock away against its lot first. | Medium | 422 |
| `STOCKT-0483` | Serial numbers on a transfer do not match | The serial numbers do not match this transfer. | Scan one serial number per unit; each must be in stock at the sending store, or in transit on this transfer. | Medium | 422 |
| `STOCKT-0484` | Transfer discrepancy, return or cancel without a valid reason code | Choose a reason for this. | Pick an active reason code (Masters > Reason Codes: Transfer Discrepancy, or Cancellation to cancel). | Medium | 422 |
| `STOCKT-0485` | Transfer receipt or return exceeds what is in transit | That is more than is still in transit on this transfer. | Enter at most the quantity still in transit; record extra units as excess with a reason. | Medium | 422 |
| `STOCKT-0486` | Transfer cannot be cancelled or returned in its current status | This transfer cannot be changed that way now. | Cancel only before dispatch; after dispatch, return what is still in transit to the source. | Medium | 422 |
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

## WMS / Logistics

| Code | When it happens | What you see | What to do | Severity | HTTP |
|---|---|---|---|---|---|
| `WMSLOG-0137` | Shipment partner missing | Logistics partner is required before dispatch. | Correct details or request approval. | High | 422 |
| `WMSLOG-0138` | AWB generation failed | AWB generation failed. Please review courier response and try again. | Correct details or request approval. | High | 503 |
| `WMSLOG-0139` | Delivery status update failed | Delivery status could not be updated. Please retry or check integration. | Correct details or request approval. | High | 503 |

