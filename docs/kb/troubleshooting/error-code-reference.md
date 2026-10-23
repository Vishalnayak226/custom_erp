---
title: Error code reference
section: Troubleshooting
order: 20
summary: Every code the application can show you, what causes it and what to do about it.
audience: everyone
last_verified: 2026-09-10
owner: documentation-maintainer
status: active
topic_type: reference
module: platform
task: Error code reference
prerequisites: Authorized access to the relevant ERP task
applies_to: source registries; not release acceptance
---

<!-- GENERATED ARTICLE - DO NOT EDIT BY HAND.
     Regenerate: pwsh docs/update-docs.ps1 -Group Content -->

# Error code reference

Every refusal in this application carries a code. There are **562** of them,
across **74** areas. This page is produced from the running catalog, so it
cannot describe a code the application does not have, or miss one it does.

Search is usually faster than scrolling: type the code into the search box
at the top of the sidebar. If you have not met a code yet and want to know how
to read the dialog it appears in, start with
[Reading an error code](error-codes.md).

> [!NOTE]
> A code with an HTTP status of 500 or above is an internal failure, not
> something you did. The detail line is deliberately withheld in that case -
> give your administrator the correlation id shown in the dialog instead.

The list runs over **2** pages. The contents below cover every area and
open the right page.

## Contents

- [Admin / Configuration](#admin-configuration) - 18 codes
- [Approvals / Workflow](#approvals-workflow) - 4 codes
- [Audit & Compliance](#audit-compliance) - 2 codes
- [Backup / DR](#backup-dr) - 4 codes
- [CRM](#crm) - 1 codes
- [CRM / Appointments](#crm-appointments) - 1 codes
- [CRM / Campaigns](#crm-campaigns) - 1 codes
- [CRM / Clienteling](#crm-clienteling) - 2 codes
- [CRM / Customer links](#crm-customer-links) - 1 codes
- [CRM / Customers](#crm-customers) - 5 codes
- [CRM / Feedback](#crm-feedback) - 1 codes
- [CRM / Journeys](#crm-journeys) - 1 codes
- [CRM / Lists](#crm-lists) - 1 codes
- [CRM / Messages](#crm-messages) - 1 codes
- [CRM / Pipeline](#crm-pipeline) - 2 codes
- [CRM / Privacy](#crm-privacy) - 1 codes
- [CRM / Referrals](#crm-referrals) - 1 codes
- [CRM / Segments](#crm-segments) - 1 codes
- [CRM / Senders](#crm-senders) - 1 codes
- [CRM / Service](#crm-service) - 1 codes
- [CRM / Templates](#crm-templates) - 1 codes
- [CRM / Vouchers](#crm-vouchers) - 1 codes
- [Channel Connectors](#channel-connectors) - 5 codes
- [Customer / CRM](#customer-crm) - 8 codes
- [Data Import / Excel Upload](#data-import-excel-upload) - 8 codes
- [Deployment / Release](#deployment-release) - 7 codes
- [DocType / Dynamic Metadata](#doctype-dynamic-metadata) - 8 codes
- [Expense Management](#expense-management) - 8 codes
- [Extension Hooks / Customization](#extension-hooks-customization) - 3 codes
- [Finance & Accounting](#finance-accounting) - 47 codes
- [Fixed Assets](#fixed-assets) - 9 codes
- [GST](#gst) - 1 codes
- [Global](#global) - 8 codes
- [Global / Common](#global-common) - 25 codes
- [Goods Receipt](#goods-receipt) - 19 codes
- [Goods Receipt / GRN](#goods-receipt-grn) - 8 codes
- [HR / Payroll](#hr-payroll) - 14 codes
- [Integration / API](#integration-api) - 9 codes
- [Integrations](#integrations) - 1 codes
- [Inventory](#inventory) - 10 codes
- [Inventory / Warehouse](#inventory-warehouse) - 29 codes
- [Item Master](#item-master) - 2 codes
- [Manufacturing / Production](#manufacturing-production) - 13 codes
- [Master Data](error-code-reference-2.md#master-data) - 43 codes
- [Mobile App / Device](error-code-reference-2.md#mobile-app-device) - 7 codes
- [Notifications](error-code-reference-2.md#notifications) - 6 codes
- [Observability / Incident](error-code-reference-2.md#observability-incident) - 3 codes
- [Omnichannel / OMS](error-code-reference-2.md#omnichannel-oms) - 5 codes
- [Order Management](error-code-reference-2.md#order-management) - 9 codes
- [PIM](error-code-reference-2.md#pim) - 6 codes
- [PIM / Product Publishing](error-code-reference-2.md#pim-product-publishing) - 8 codes
- [POS / Billing](error-code-reference-2.md#pos-billing) - 39 codes
- [POS / Offline & Cash Drawer](error-code-reference-2.md#pos-offline-cash-drawer) - 8 codes
- [Patch / Bug Governance](error-code-reference-2.md#patch-bug-governance) - 3 codes
- [Payments](error-code-reference-2.md#payments) - 7 codes
- [Planning](error-code-reference-2.md#planning) - 1 codes
- [Procurement / RFQ](error-code-reference-2.md#procurement-rfq) - 6 codes
- [Purchase Order](error-code-reference-2.md#purchase-order) - 17 codes
- [Purchase Return](error-code-reference-2.md#purchase-return) - 1 codes
- [Purchase Return / RTV](error-code-reference-2.md#purchase-return-rtv) - 5 codes
- [Quality & Maintenance](error-code-reference-2.md#quality-maintenance) - 6 codes
- [RFQ](error-code-reference-2.md#rfq) - 6 codes
- [Reports](error-code-reference-2.md#reports) - 3 codes
- [Reports & Exports](error-code-reference-2.md#reports-exports) - 8 codes
- [SaaS / Multi-Tenant](error-code-reference-2.md#saas-multi-tenant) - 8 codes
- [Sales / POS](error-code-reference-2.md#sales-pos) - 9 codes
- [Sales Return](error-code-reference-2.md#sales-return) - 7 codes
- [Security / Rate Limit](error-code-reference-2.md#security-rate-limit) - 5 codes
- [Stock Transfer](error-code-reference-2.md#stock-transfer) - 10 codes
- [Tax / GST / E-Invoice](error-code-reference-2.md#tax-gst-e-invoice) - 15 codes
- [User Access & Security](error-code-reference-2.md#user-access-security) - 9 codes
- [Vendor Invoice / AP](error-code-reference-2.md#vendor-invoice-ap) - 4 codes
- [WMS](error-code-reference-2.md#wms) - 1 codes
- [WMS / Logistics](error-code-reference-2.md#wms-logistics) - 3 codes

## Admin / Configuration

| Code | When it happens | What you see | What to do | Severity | HTTP |
|---|---|---|---|---|---|
| `ADMINC-0030` | Missing number series | Number series is not configured for this document type. Please contact administrator. | Contact admin to configure. | High | 422 |
| `ADMINC-0031` | Number series exhausted | Number series is exhausted for this document type. Please contact administrator. | Contact admin to configure. | High | 422 |
| `ADMINC-0032` | Approval workflow missing | Approval workflow is not configured for this transaction. Please contact administrator. | Contact admin to configure. | High | 422 |
| `ADMINC-0033` | Posting period closed | Posting period is closed for the selected date. Please select an open period or request approval. | Contact admin to configure. | High | 409 |
| `ADMINC-0034` | Tax configuration missing | Tax configuration is missing for this transaction. Please contact administrator. | Contact admin to configure. | High | 422 |
| `ADMINC-0035` | GL mapping missing | Accounting GL mapping is missing. Please contact Finance/Admin. | Contact admin to configure. | High | 422 |
| `ADMINC-0036` | Store mapping missing | Store mapping is missing or inactive. Please contact administrator. | Contact admin to configure. | High | 422 |
| `ADMINC-0037` | Warehouse mapping missing | Warehouse mapping is missing or inactive. Please contact administrator. | Contact admin to configure. | High | 422 |
| `ADMINC-0038` | Payment mode configuration missing | Payment mode configuration is missing. Please contact administrator. | Contact admin to configure. | High | 422 |
| `ADMINC-0039` | Template configuration missing | Document template is not configured. Please contact administrator. | Contact admin to configure. | High | 422 |
| `ADMINC-0310` | Number series prefix already used by another series | This prefix is already used by another number series. | Choose a prefix no other transaction uses - two series with one prefix could print the same number. | High | 422 |
| `ADMINC-0311` | Number series settings invalid | The number series settings are not valid. | Use capital letters, digits, '/' and '-' in prefix and suffix (no spaces, up to 20), a separator of / or - (or none), a length of 1 to 12 digits, and keep the whole number to 16 characters. | Medium | 422 |
| `PLATFORM-0316` | Data console edit refused for a protected table or column | This value cannot be changed from the data console. | Keys, hidden (secret or personal) values and audit tables are protected. Change the record through its own screen instead. | Medium | 422 |
| `PLATFORM-0317` | Cross-client action (data console, client list, plans, sandboxes) by someone who is not a platform operator | Only a platform operator can see or change other clients. | Ask the server administrator to add your client and username to ERP_PLATFORM_OPERATORS. | High | 403 |
| `STORE-0850` | Working at, or switching to, a store the user has not been given | You have not been given this store. | Ask an administrator to add the store in Users -> Stores, or on your role in Roles -> Stores. | High | 422 |
| `STORE-0851` | Store access setting incomplete or not one of the offered choices | The store access setting is not complete. | Choose All stores, Selected stores (and tick at least one) or Home store only. | Medium | 422 |
| `STORE-0852` | Store access names a store that is not a Location | One of the stores is not in Setup -> Location. | Pick stores from the list; create the store in Setup -> Location first if it is new. | Medium | 422 |
| `STORE-0853` | Attempt to narrow Super Admin's store access | Super Admin always works at every store. | Give the person a different role if they should work at fewer stores. | Low | 422 |

## Approvals / Workflow

| Code | When it happens | What you see | What to do | Severity | HTTP |
|---|---|---|---|---|---|
| `APPROV-0157` | Approver not found | No approver is configured for this transaction. Please contact administrator. | Correct details or retry. Contact admin if repeated. | High | 404 |
| `APPROV-0158` | Approval already completed | This approval action is already completed. Please refresh the page. | Correct details or retry. Contact admin if repeated. | High | 409 |
| `APPROV-0159` | Reject reason missing | Rejection reason is required. | Correct details or retry. Contact admin if repeated. | High | 422 |
| `APPROV-0840` | Approver is the person who submitted the document | You cannot approve or reject a document you submitted. | Ask another approver with the required role to decide it, or ask an admin to change Who can approve their own documents (Settings > Configuration > Security). | Medium | 422 |

## Audit & Compliance

| Code | When it happens | What you see | What to do | Severity | HTTP |
|---|---|---|---|---|---|
| `AUDITC-0173` | Audit log missing reason | Reason/comment is required for audit-sensitive changes. | Correct details or retry. Contact admin if repeated. | High | 422 |
| `AUDITC-0174` | Backdated change blocked | Backdated change is not allowed for this transaction. | Correct details or retry. Contact admin if repeated. | High | 409 |

## Backup / DR

| Code | When it happens | What you see | What to do | Severity | HTTP |
|---|---|---|---|---|---|
| `DR-0211` | Backup failed | Database backup failed. Please review the backup log immediately. | Retry backup and investigate storage/DB issue. | Critical | 503 |
| `DR-0212` | Restore checksum mismatch | Backup restore failed because checksum verification did not match. | Use another backup and investigate corruption. | Critical | 422 |
| `DR-0213` | Restore drill overdue | Restore drill is overdue. Please complete and record a recovery test. | Schedule and complete restore drill. | High | 200 |
| `DR-0214` | RPO breach detected | Recovery-point target is breached. Recent backup coverage is insufficient. | Run backup and investigate missed schedule. | Critical | 422 |

## CRM

| Code | When it happens | What you see | What to do | Severity | HTTP |
|---|---|---|---|---|---|
| `CRMPLT-1119` | CRM record not found | That record was not found. | Refresh the list; it may have been removed or merged. | Low | 404 |

## CRM / Appointments

| Code | When it happens | What you see | What to do | Severity | HTTP |
|---|---|---|---|---|---|
| `CRMPLT-1109` | Appointment slot full, closed or not valid | That appointment slot can't be booked. | Choose another slot from the list. | Low | 409 |

## CRM / Campaigns

| Code | When it happens | What you see | What to do | Severity | HTTP |
|---|---|---|---|---|---|
| `CRMPLT-1106` | Campaign not ready for this action (no segment, template or sender; not a draft; already sending) | The campaign can't do that yet. | Fix what the detail lists, then submit it for approval again. | Medium | 422 |

## CRM / Clienteling

| Code | When it happens | What you see | What to do | Severity | HTTP |
|---|---|---|---|---|---|
| `CRMPLT-1116` | Wish list or hold request not valid (unknown item, no store, not confirmed) | This hold can't be done. | Scan or type the item, choose the store, and tell the customer only after the store confirms the hold. | Low | 422 |
| `CRMPLT-1120` | Task action not valid (no title, already done, no outcome) | The task can't be saved. | Give the task a title; to finish it, say how it went. | Low | 422 |

## CRM / Customer links

| Code | When it happens | What you see | What to do | Severity | HTTP |
|---|---|---|---|---|---|
| `CRMPLT-1108` | Preference, feedback or callback link not valid or expired | This link is not valid or has expired. | Use the link in your latest message from us. | Low | 404 |

## CRM / Customers

| Code | When it happens | What you see | What to do | Severity | HTTP |
|---|---|---|---|---|---|
| `CRMPLT-1101` | Customer page action not valid | This can't be saved on the customer. | Check the detail below, correct it and save again. | Low | 422 |
| `CRMPLT-1102` | Consent action not valid (channel, purpose, source, or an import re-granting a withdrawal) | This consent can't be recorded. | Choose the channel and purpose and say where the consent came from. Only the customer can opt back in after saying no. | Medium | 422 |
| `CRMPLT-1112` | Customer merge not valid (same customer, already merged, erased, already waiting) | These customers can't be merged. | Choose two different, active customers. A merge already waiting is under Approvals. | Medium | 422 |
| `CUSTOM-0869` | Customer GSTIN malformed or failing its check digit | The GSTIN is not valid. | Type the 15-character GSTIN exactly as it is on the customer's GST certificate. | Medium | 422 |
| `CUSTOM-0870` | Customer PAN malformed | The PAN is not valid. | Type the 10-character PAN (5 letters, 4 digits, 1 letter) as it is on the PAN card. | Medium | 422 |

## CRM / Feedback

| Code | When it happens | What you see | What to do | Severity | HTTP |
|---|---|---|---|---|---|
| `CRMPLT-1121` | Survey or feedback answer not valid | The feedback can't be saved. | Choose a score in the range shown. | Low | 422 |

## CRM / Journeys

| Code | When it happens | What you see | What to do | Severity | HTTP |
|---|---|---|---|---|---|
| `CRMPLT-1107` | Journey not valid (trigger, steps, branches or not published) | The journey isn't complete. | Fix the step the detail names, then publish it. | Low | 422 |

## CRM / Lists

| Code | When it happens | What you see | What to do | Severity | HTTP |
|---|---|---|---|---|---|
| `CRMPLT-1115` | Static list upload not valid (no phone column, no source, bad expiry) | The list can't be uploaded. | Upload a CSV with a phone column, say where the numbers came from, and give the expiry as YYYY-MM-DD. | Low | 422 |

## CRM / Messages

| Code | When it happens | What you see | What to do | Severity | HTTP |
|---|---|---|---|---|---|
| `CRMPLT-1113` | Message not sent or not valid (no text, no template, refused by the send rules) | This message can't be sent. | Read the reason in the detail. Outside WhatsApp's 24-hour reply window, choose a template. | Low | 422 |

## CRM / Pipeline

| Code | When it happens | What you see | What to do | Severity | HTTP |
|---|---|---|---|---|---|
| `CRMPLT-1111` | Lead or opportunity rule not met (stage requirement, approved quotation to win, reason to lose) | The deal can't move there yet. | Do what the detail says the stage needs (for example, have the quotation approved), then try again. | Low | 422 |
| `CRMPLT-1122` | Sales quotation not valid (no lines, unknown item, not a draft) | The quotation can't be saved. | Add at least one line with an item, a quantity and a rate. | Low | 422 |

## CRM / Privacy

| Code | When it happens | What you see | What to do | Severity | HTTP |
|---|---|---|---|---|---|
| `CRMPLT-1118` | Phone or email belonged to an erased customer | This customer asked to be erased. | Their phone number and email cannot be added again. If they come back and ask to be a customer again, ask an administrator. | High | 409 |

## CRM / Referrals

| Code | When it happens | What you see | What to do | Severity | HTTP |
|---|---|---|---|---|---|
| `CRMPLT-1117` | Referral not valid (unknown code, existing customer, already referred) | This referral can't be recorded. | Check the referral code. A referral is for a new customer, recorded once. | Low | 422 |

## CRM / Segments

| Code | When it happens | What you see | What to do | Severity | HTTP |
|---|---|---|---|---|---|
| `CRMPLT-1103` | Segment rule or lifecycle thresholds not valid | The segment's rules are not complete. | Finish the rule the detail names (a field, a comparison and a value) and count again. | Low | 422 |

## CRM / Senders

| Code | When it happens | What you see | What to do | Severity | HTTP |
|---|---|---|---|---|---|
| `CRMPLT-1105` | Sender settings not valid or the provider could not be asked | The sender isn't set up correctly. | Fill in what the detail names under CRM > Settings > Senders (URLs must be https and public). | Medium | 422 |

## CRM / Service

| Code | When it happens | What you see | What to do | Severity | HTTP |
|---|---|---|---|---|---|
| `CRMPLT-1114` | Ticket action not valid in its state | The ticket can't do that in its current state. | Check the ticket's state; resolve needs a note of what was done. | Low | 422 |

## CRM / Templates

| Code | When it happens | What you see | What to do | Severity | HTTP |
|---|---|---|---|---|---|
| `CRMPLT-1104` | Message template not valid (unknown placeholder, missing text or subject, channel change) | The template can't be saved. | Use only the placeholders listed beside the text, and give an email a subject. | Low | 422 |

## CRM / Vouchers

| Code | When it happens | What you see | What to do | Severity | HTTP |
|---|---|---|---|---|---|
| `CRMPLT-1110` | Single-use voucher held by another checkout, or not usable | This voucher code can't be used here right now. | A single-use code can be taken by one checkout at a time. Finish or cancel the other checkout, or use another code. | Medium | 409 |

## Channel Connectors

| Code | When it happens | What you see | What to do | Severity | HTTP |
|---|---|---|---|---|---|
| `CONN-0224` | Live connector credentials missing | Channel credentials are missing. Please configure credentials before publishing. | Configure channel credentials. | High | 422 |
| `CONN-0225` | Channel API rate limited | Channel is rate limiting requests. Publishing will retry automatically. | Wait or reduce publish volume. | Medium | 200 |
| `CONN-0226` | Channel publish failed | Product could not be published to {channel}. Please review the error details. | Fix product/channel issue and retry. | High | 503 |
| `CONN-0227` | Channel field mapping missing | Channel field mapping is missing for required field {field name}. | Complete channel field mapping. | High | 422 |
| `CONN-0228` | Duplicate SKU on channel | SKU already exists on the selected channel. Please map to existing listing or change SKU. | Map to existing listing or correct SKU. | High | 409 |

## Customer / CRM

| Code | When it happens | What you see | What to do | Severity | HTTP |
|---|---|---|---|---|---|
| `CUSTOM-0121` | Customer required for B2B invoice | Customer GST details are required for B2B invoice. | Correct details or request approval. | High | 422 |
| `CUSTOM-0133` | Customer duplicate mobile | A customer with this mobile number already exists. | Correct details or request approval. | High | 409 |
| `CUSTOM-0134` | Loyalty points insufficient | Insufficient loyalty points for redemption. | Correct details or request approval. | High | 422 |
| `CUSTOM-0601` | Loyalty redemption daily limit reached | This customer has already redeemed points as many times as allowed today. | Take this payment another way. The limit resets tomorrow (Settings -> Security: Max loyalty redemptions per customer per day). | Medium | 422 |
| `CUSTOM-0602` | Loyalty redemption needs the customer's one-time code | The customer has to confirm this redemption with the code sent to them. | Choose Redeem Points again to send the code, and enter the code the customer reads back. | High | 422 |
| `CUSTOM-0603` | Loyalty redemption above the approval value without an approved request | Redeeming this many points needs a manager's approval. | Raise the request from Redeem Points with a reason, ask a manager to approve it under Approvals, then complete the sale. | High | 422 |
| `CUSTOM-0614` | Loyalty adjustment not valid (no customer, zero points, or no Loyalty Adjustment reason) | This points adjustment can't be made. | Choose the customer, type the points (+ to credit, - to debit) and pick a Loyalty Adjustment reason. | Low | 422 |
| `CUSTOM-0615` | Loyalty debit adjustment larger than the balance | That debit is more than the customer's points. | Debit at most the customer's current balance. | Medium | 422 |

## Data Import / Excel Upload

| Code | When it happens | What you see | What to do | Severity | HTTP |
|---|---|---|---|---|---|
| `DATAIM-0163` | Excel template invalid | Invalid upload template. Please download and use the latest template. | Correct details or retry. Contact admin if repeated. | Medium | 422 |
| `DATAIM-0164` | Excel mandatory column missing | Mandatory column is missing in the uploaded file. Please correct the file and upload again. | Correct details or retry. Contact admin if repeated. | Medium | 422 |
| `DATAIM-0165` | Excel row validation failed | Some rows have validation errors. Please download the error file and correct them. | Correct details or retry. Contact admin if repeated. | Medium | 503 |
| `DATAIM-0166` | Duplicate rows in upload | Duplicate rows found in uploaded file. Please remove duplicates and upload again. | Correct details or retry. Contact admin if repeated. | Medium | 409 |
| `DATAIM-0187` | Partial upload | File uploaded with validation errors. Please download the error file and correct failed rows. | Download error file. | Medium | 200 |
| `DATAIM-0189` | Uploaded file exceeds the maximum import row count | This file has too many rows to import in one go. | Split the file into smaller batches and import them one at a time. | Medium | 422 |
| `DATAIM-0470` | Resume asked of an import job recorded before failed rows were kept | This import job cannot be resumed. | It was recorded before failed rows were kept - correct the original file and import it again. | Low | 422 |
| `DATAIM-0471` | Resume file does not match the job's open failed rows | These rows cannot resume this import. | Use the job's Failed rows download: keep its original_row column, fix only those rows, and send each row once. | Low | 422 |

## Deployment / Release

| Code | When it happens | What you see | What to do | Severity | HTTP |
|---|---|---|---|---|---|
| `DEPLOY-0204` | Promotion blocked due to dirty tree | Release promotion is blocked because source changes are not committed or reviewed. | Commit/review changes before promotion. | High | 409 |
| `DEPLOY-0205` | Build or tests failed | Release build failed. Please fix build, test, or vulnerability errors before promotion. | Fix failed checks and rerun. | Critical | 503 |
| `DEPLOY-0206` | Migration failed during release | Release migration failed. The environment was not promoted. | Review migration error and rollback if needed. | Critical | 503 |
| `DEPLOY-0207` | Rollback failed | Rollback could not be completed. Manual intervention is required. | Escalate to release owner immediately. | Critical | 503 |
| `DEPLOY-0208` | Health check failed after deployment | Deployment health check failed. The environment is not ready. | Rollback or fix service health. | Critical | 503 |
| `DEPLOY-0209` | Required environment variable missing | System configuration is incomplete. Please contact technical support. | Set missing configuration and restart. | Critical | 422 |
| `DEPLOY-0210` | Production sourcemap/debug artifact detected | Production build contains debug artifacts. Release is blocked until removed. | Remove debug artifact and rebuild. | High | 200 |

## DocType / Dynamic Metadata

| Code | When it happens | What you see | What to do | Severity | HTTP |
|---|---|---|---|---|---|
| `META-0196` | DocType not registered | This form is not configured. Please contact administrator. | Configure DocType or remove menu link. | High | 422 |
| `META-0197` | Unsupported field type | This field type is not supported. Please contact administrator. | Fix DocType field configuration. | High | 422 |
| `META-0198` | Linked master record missing | Selected {linked record} does not exist or is inactive. | Select a valid master value. | Medium | 422 |
| `META-0199` | Free-text value requires master selection | Please select a valid value from the list. Free text is not allowed for this field. | Select an existing master or create it first. | High | 422 |
| `META-0200` | Metadata cache stale | Form configuration has changed. Please refresh the page. | Refresh page and retry. | Medium | 200 |
| `META-0201` | Custom field name collision | This custom field conflicts with an existing field. Please use another field name. | Rename the custom field. | High | 422 |
| `META-0202` | Label replacement conflict | This label replacement conflicts with another configured term. Please review before saving. | Correct label mapping. | Medium | 200 |
| `META-0203` | Schema migration pending | Database migration is pending. Please complete migration before using this feature. | Run migration or rollback release. | Critical | 422 |

## Expense Management

| Code | When it happens | What you see | What to do | Severity | HTTP |
|---|---|---|---|---|---|
| `EXP-0273` | Expense policy violation | Expense violates company policy. Please correct or request exception approval. | Correct claim or request exception. | High | 422 |
| `EXP-0274` | Receipt attachment required | Receipt attachment is required for this expense claim. | Attach receipt and resubmit. | High | 422 |
| `EXP-0275` | Employee advance overdue | Employee advance is overdue for settlement. Please settle or escalate. | Submit settlement or escalate. | Medium | 200 |
| `EXP-0541` | Attachment is not an accepted file type | This file cannot be attached. | Attach a photo or scan of the receipt - JPEG, PNG, WebP or PDF. | Low | 422 |
| `EXP-0542` | Attachment larger than 5 MB | This file is too large to attach. | Take the photo at a lower resolution, or scan at 150-200 dpi, and attach it again. | Low | 413 |
| `EXP-0543` | Record already holds the most attachments allowed | This record already has the most files it can hold. | Remove a file you no longer need, or put several receipts into one PDF. | Low | 422 |
| `EXP-0544` | Same file attached twice to one record | This exact file is already attached. | Nothing to do if it is the same receipt; attach a different file if you meant another one. | Low | 409 |
| `EXP-0545` | Attachments changed after the record left Draft or Rejected | Files can no longer be added or removed on this record. | Receipts can be changed only while the claim is Draft or Rejected; ask a Super Admin if a wrong file must be removed. | Medium | 409 |

## Extension Hooks / Customization

| Code | When it happens | What you see | What to do | Severity | HTTP |
|---|---|---|---|---|---|
| `EXT-0289` | Extension hook rejected transaction | Custom validation rejected this transaction: {reason}. | Correct data or contact extension owner. | High | 422 |
| `EXT-0290` | Extension hook timeout | Custom validation service did not respond in time. Please retry or contact support. | Retry later or contact admin. | High | 503 |
| `EXT-0291` | Extension token scope violation | Extension token is not authorized for this document type or action. | Use correct scoped token. | Critical | 401 |

## Finance & Accounting

| Code | When it happens | What you see | What to do | Severity | HTTP |
|---|---|---|---|---|---|
| `EINV-0980` | E-invoice GSTIN set-up not valid | This e-invoice set-up cannot be saved. | Read the reason: a valid GSTIN, an available provider, turnover band, document and supply types, a start date, and credentials before switching it on. | Medium | 422 |
| `EINV-0981` | E-invoice document number already used under the GSTIN this year | This document number already has an e-invoice under this GSTIN. | A number is never reused, even after its IRN is cancelled - issue the document under a new number from its series. | High | 422 |
| `EINV-0982` | E-invoice action not allowed in its state | This e-invoice cannot do that now. | Fix the listed problems and press Validate first, or open the record to see what it is waiting for. | Medium | 422 |
| `EINV-0983` | Changing a tax field of a document that has an IRN | This document has an IRN - its tax details cannot change. | Cancel the IRN within 24 hours (E-Invoice workbench, needs approval), or correct it with a credit / debit note. | High | 422 |
| `EINV-0984` | IRN cancellation not allowed | This IRN cannot be cancelled this way. | Only the tax role (Accountant) cancels an acknowledged IRN, with a reason code and a reason, within 24 hours; after that issue a credit note. | Medium | 422 |
| `EINV-0985` | Portal IRN entry not valid | This portal IRN cannot be entered. | The tax role enters the 64-character IRN, Ack No, Ack Date, the signed QR text and the reason, for a document that has no IRN yet. | Medium | 422 |
| `EINV-0986` | E-invoiced sale sent to silent printing | This invoice carries its IRN and QR, so it prints from the browser. | No action: the browser print opens with the IRN, Ack and the IRP's signed QR. | Low | 422 |
| `FIN-0021` | Exchange rate not usable (same currency on both sides, zero or negative, or none on file for the date) | The exchange rate can't be used. | Pick two different currencies and a rate greater than zero; if the rate is missing, add it under Setup -> Exchange Rate for that date, or type the rate on the document. | Medium | 422 |
| `FIN-0260` | Financial period locked | Financial period is locked. Posting is not allowed. | Select open period or request reopening. | Critical | 409 |
| `FIN-0261` | Bank statement mismatch | Bank statement amount/reference does not match system payment. Please reconcile manually. | Review and manually match/unmatch. | High | 200 |
| `FIN-0501` | Period close attempted with close tasks not signed off | This period cannot close yet - some close tasks are not signed off. | Open the period's close tasks; each owner signs theirs off with evidence, then close. | High | 409 |
| `FIN-0502` | Accounting period, close task or reopen request not found | That accounting period, close task or reopen request was not found. | Refresh Accounting Periods and open the period again. | Low | 404 |
| `FIN-0503` | Close task changed while the period is closed | Close tasks can only change while the period is open. | Request a reopen of the period; once it is approved the tasks open for the next close round. | Medium | 409 |
| `FIN-0504` | Close or reconciliation control action by a role other than Accountant or Super Admin | Only an Accountant or a Super Admin can do this. | Ask your Accountant (Finance Head) or a Super Admin. | High | 403 |
| `FIN-0505` | Close task owner is not an active user | The owner must be an active user. | Choose the username of the person who will sign this task off. | Low | 422 |
| `FIN-0506` | Close task already signed off, or not signed off | This close task is not in a state that allows that. | Refresh the close tasks; undo a sign-off before changing it. | Low | 409 |
| `FIN-0507` | Sign-off evidence or reopen reason missing or too short | Write the evidence or reason - it is what the decision rests on. | Say what you checked and what it showed, or why the period must reopen (at least 10 characters). | Medium | 422 |
| `FIN-0508` | Close task signed off by someone other than its owner | Only the task's owner can sign it off. | Ask the owner to sign it, or ask an Accountant to reassign the task to you. | High | 403 |
| `FIN-0509` | Period reopen not allowed (not closed, or a request already pending) | A reopen cannot be requested for this period now. | Only a closed period reopens, one request at a time - check Approvals for the pending one. | Medium | 409 |
| `FIN-0510` | Bank account or statement line not found | That bank account or statement line was not found. | Refresh Bank Reconciliation and choose the bank account again. | Low | 404 |
| `FIN-0511` | Reconciliation change on a statement line dated in a closed period | This statement line is in a closed period, so its reconciliation is frozen. | Request a reopen of that period if the reconciliation must change. | High | 409 |
| `FIN-0512` | Manual bank match not valid (entry on another account, wrong side, already matched) | Those book entries cannot be matched to this line. | Choose unmatched entries on this bank's GL account, on the side the statement line implies. | Medium | 422 |
| `FIN-0513` | Bank match amounts do not agree | The amounts do not agree, so the match was not made. | Post the bank charge or correction for the difference first (Journal Voucher), then match. | Medium | 422 |
| `FIN-0514` | Bank rule match settings out of range | The rule settings are out of range. | Use a tolerance of 0 to 10,000 and a date window of 0 to 31 days. | Low | 422 |
| `FIN-0515` | Bank exception missing owner or reason, or not in the queue | The exception needs an owner and a reason. | Name an active user as owner and say what is wrong with the line. | Low | 422 |
| `FIN-0516` | Marketplace or card/UPI payout cannot be matched to this bank line | That payout cannot be matched to this bank line. | Match a payout to a Credit line, after the settlement file is reconciled, from the right clearing account. | Medium | 422 |
| `FIN-0517` | Receipt or payment amount not valid - more than the outstanding balance, not positive, or nothing outstanding | That amount cannot be settled against this invoice. | Enter an amount above zero and no more than the outstanding balance shown on the invoice, or leave it as offered to settle the whole balance. | Medium | 422 |
| `FIN-0518` | Part-payment of a foreign-currency invoice or bill | A foreign-currency invoice can only be settled in full. | Settle or pay the whole amount in one go - the realised exchange difference is booked on the full settlement. | Medium | 422 |
| `FIN-0519` | Settlement reference already used for a different amount on this invoice | That reference is already used for a different amount on this invoice. | Refresh the invoice to see the payment already recorded; a new payment needs a new reference. | Medium | 409 |
| `FIN-0520` | Settlement reference has characters or a length that is not allowed | The payment reference is not valid. | Use up to 64 letters, digits, dots, dashes, underscores or slashes. | Low | 422 |
| `FIN-0521` | TDS payment on a vendor bill that is already part-paid | TDS is withheld on a whole bill, and this bill is already part-paid. | Pay the remaining balance with Pay (without TDS), and record the TDS separately with your accountant. | Medium | 409 |
| `FIN-0522` | Credit note names a foreign-currency sales invoice that is still open | This credit note cannot be applied to a foreign-currency invoice. | Settle that invoice in full, or remove the invoice from the note and post it as a refund. | Medium | 422 |
| `FIN-0841` | Pay with TDS on a bill in a foreign currency | TDS cannot be worked out on a bill in a foreign currency. | Pay the bill with Pay, which converts at the payment rate; record any withholding on a foreign payment separately. | Medium | 422 |
| `FIN-0842` | Matching a vendor bill that is paid, part-paid or awaiting approval | This bill can no longer be matched. | A bill is matched once, before it is paid. Check its payments under Supplier bills; a wrong bill is corrected with a debit or credit note. | Medium | 422 |
| `FIN-0844` | Changing the amounts of a document that has posted to the accounts | The amounts of a posted document cannot be changed. | Correct it with a credit or debit note, or reverse it; other details of the document can still be edited. | High | 422 |
| `FINANC-0055` | Document not balanced | Accounting entry is not balanced. Debit and credit totals must match. | Correct finance details or request approval. | High | 422 |
| `FINANC-0056` | GL account inactive | Selected GL account is inactive. Please select an active GL account. | Correct finance details or request approval. | High | 422 |
| `FINANC-0057` | Cost center missing | Cost center is required for this transaction. | Correct finance details or request approval. | High | 422 |
| `FINANC-0058` | Profit center missing | Profit center is required for this transaction. | Correct finance details or request approval. | High | 422 |
| `FINANC-0059` | Currency exchange rate missing | Exchange rate is missing for the selected currency and date. | Correct finance details or request approval. | High | 422 |
| `FINANC-0060` | Posting blocked | Posting is blocked because required accounting configuration is missing. | Correct finance details or request approval. | High | 409 |
| `FINANC-0061` | Reversal date invalid | Reversal date must be within an open posting period. | Correct finance details or request approval. | High | 422 |
| `FINANC-0062` | Duplicate journal reference | A journal with the same reference already exists. | Correct finance details or request approval. | High | 409 |
| `FINANC-0063` | Budget exceeded | Transaction exceeds the available budget. Approval is required. | Correct finance details or request approval. | High | 422 |
| `FINANC-0064` | TDS section missing | TDS section is required for this vendor/payment. | Correct finance details or request approval. | High | 422 |
| `FINANC-0065` | TDS amount invalid | TDS amount cannot exceed payable amount. | Correct finance details or request approval. | High | 422 |
| `FINANC-0066` | Advance adjustment exceeds balance | Advance adjustment amount exceeds available advance balance. | Correct finance details or request approval. | High | 422 |

## Fixed Assets

| Code | When it happens | What you see | What to do | Severity | HTTP |
|---|---|---|---|---|---|
| `ASSET-0270` | Asset not capitalized | Asset must be capitalized before depreciation, transfer, or disposal. | Capitalize asset first. | High | 422 |
| `ASSET-0271` | Depreciation calculation missing | Depreciation cannot be calculated because useful life or capitalization date is missing. | Update asset financial details. | High | 422 |
| `ASSET-0272` | Asset physical verification overdue | Asset physical verification is overdue. Please complete verification. | Complete verification or mark exception. | Medium | 200 |
| `ASSET-0273` | A Fixed Asset item was put on a sale, order or invoice | This item is a fixed asset, not stock for sale, so it cannot be sold, ordered or invoiced. | Remove it from the sale. Fixed assets are managed under HRM -> Fixed Assets. | High | 422 |
| `ASSET-0536` | Depreciation run by a role that does not own period close | Only an Accountant or a Super Admin can run depreciation. | Ask an Accountant to choose Run depreciation, or wait for the monthly run. | Medium | 403 |
| `ASSET-0537` | Disposal proceeds below zero or not a number | The proceeds must be zero or more. | Enter what was received for the asset, or 0 if nothing was. | Low | 422 |
| `ASSET-0538` | Disposal proceeds received into an account that is not cash or a bank account | Proceeds can only be received into Cash/Bank or a bank account. | Choose 1100 Cash/Bank or one of the bank accounts listed under Received into. | Low | 422 |
| `ASSET-0539` | Depreciation due before a disposal could not be posted | The depreciation due on this asset could not be posted, so it was not disposed. | Check System Errors for the reason (for example a missing GL account), fix it, and dispose again. | High | 422 |
| `ASSET-0843` | Asset set to Capitalised or Disposed on the record form | An asset is capitalised or disposed with its own button. | Use Capitalise or Dispose on HRM → Fixed Assets - they post the ledger entry; the record form cannot. | High | 422 |

## GST

| Code | When it happens | What you see | What to do | Severity | HTTP |
|---|---|---|---|---|---|
| `TAXGST-1320` | E-way bill refused (not needed, missing GSTIN / PIN / state / HSN / distance / vehicle, no provider, outside the extend or cancel window, already generated) | This e-way bill can't go ahead. | Fix what the message names; until a provider is connected, make the bill on the e-way bill portal and use Record from portal. | Medium | 422 |

## Global

| Code | When it happens | What you see | What to do | Severity | HTTP |
|---|---|---|---|---|---|
| `GLOBAL-0303` | Request method not allowed | This request method is not supported here. | Use GET to read and POST, PUT or DELETE to change data, as the API documentation shows. | Low | 405 |
| `GLOBAL-0304` | Request content type or encoding not accepted | The request body is in a format this address does not accept. | Send JSON (Content-Type: application/json), or a file upload as multipart/form-data, without compression. | Low | 415 |
| `GLOBAL-0305` | Request body too large | The request is too large to accept. | Send less data at once - split a large file or list into smaller parts. | Low | 413 |
| `GLOBAL-0306` | Request headers too large | The request carries too many or too large headers. | Sign out and back in; if this persists, contact support - a browser extension or proxy may be adding headers. | Low | 431 |
| `GLOBAL-0307` | Malformed, ambiguous or over-nested JSON request | The request data could not be accepted. | Check the request is one valid JSON value, with no field repeated in the same object. | Low | 400 |
| `GLOBAL-0308` | Request exceeded its time limit | This took longer than the time a single request is allowed, so it was stopped. | Send less at once - split a large import file into smaller files, or narrow a report's filters and use background export. | Medium | 504 |
| `GLOBAL-1315` | Reprint refused (no Document Reprint permission, no reason, approval needed or already used, no approver set) | This document can't be printed again yet. | Give the reason; ask a manager for the Document Reprint permission or send a Reprint Request and print once it is approved. | Medium | 422 |
| `GLOBAL-1319` | Print refused: a tax document from a store with no GSTIN, or a template version that cannot be decided (not waiting, own change, not found) | This can't be printed or changed yet. | Add the GSTIN to the store's legal entity (Setup > Legal Entities); a template change is approved by someone other than the person who saved it. | Medium | 422 |

## Global / Common

| Code | When it happens | What you see | What to do | Severity | HTTP |
|---|---|---|---|---|---|
| `GLOBAL-0001` | Mandatory value missing | Required value is missing. Please enter {field name} to continue. | Enter the missing value. | Medium | 422 |
| `GLOBAL-0002` | Invalid format | Invalid format for {field name}. Please enter a valid value. | Correct the field format. | Medium | 422 |
| `GLOBAL-0003` | Duplicate record | A record with the same {unique key} already exists. Please check and try again. | Use a unique value or open the existing record. | Medium | 409 |
| `GLOBAL-0004` | Record not found | The selected record was not found or may have been deleted. Please refresh and try again. | Refresh the page and select the record again. | Medium | 404 |
| `GLOBAL-0005` | Unsaved changes | You have unsaved changes. Save or discard the changes before leaving this page. | Save or discard changes. | Medium | 422 |
| `GLOBAL-0006` | Concurrent update | This record was updated by another user. Please refresh to view the latest data. | Refresh and reapply changes if required. | Medium | 409 |
| `GLOBAL-0007` | Attachment too large | The file size exceeds the allowed limit. Please upload a smaller file. | Upload a smaller file. | Medium | 422 |
| `GLOBAL-0008` | Unsupported file type | This file type is not supported. Please upload an allowed file format. | Upload an allowed file. | Medium | 422 |
| `GLOBAL-0009` | Session expired | Your session has expired. Please sign in again to continue. | Sign in again. | Medium | 401 |
| `GLOBAL-0010` | System unavailable | The system is temporarily unavailable. Please try again later or contact support. | Retry later or contact support. | Medium | 503 |
| `GLOBAL-0011` | Permission denied | You do not have permission to perform this action. Please contact your administrator. | Contact admin for access. | Medium | 403 |
| `GLOBAL-0012` | Invalid date range | From Date cannot be later than To Date. Please select a valid date range. | Correct the date range. | Medium | 422 |
| `GLOBAL-0013` | Past/future date blocked | The selected date is outside the allowed posting period. Please select a valid date. | Select valid date or request period opening. | Medium | 409 |
| `GLOBAL-0014` | Negative value not allowed | Negative values are not allowed for this field. Please enter zero or a positive value. | Enter valid value. | Medium | 422 |
| `GLOBAL-0015` | Decimal precision exceeded | The value exceeds the allowed decimal precision. Please enter a valid amount or quantity. | Adjust decimal value. | Medium | 422 |
| `GLOBAL-0016` | Save failed | The record could not be saved. Please review highlighted errors and try again. | Fix highlighted errors and save. | Medium | 503 |
| `GLOBAL-0017` | Delete blocked by reference | This record cannot be deleted because it is already used in transactions. You may deactivate it instead. | Deactivate record if needed. | Medium | 409 |
| `GLOBAL-0018` | Inactive record used | The selected record is inactive. Please select an active record. | Select active record. | Medium | 422 |
| `GLOBAL-0019` | Invalid status transition | This action is not allowed for the current document status. | Check document status and choose allowed action. | Medium | 422 |
| `GLOBAL-0020` | Cancellation reason missing | Please enter a cancellation reason to continue. | Enter reason. | Medium | 422 |
| `GLOBAL-0178` | Confirm delete | This action cannot be undone. Do you want to continue? | Review and confirm. | Medium | 200 |
| `GLOBAL-0295` | Autosuggest no matching master | No matching master record found. Create a new master or change search text. | Create master or change search term. | Medium | 200 |
| `GLOBAL-0296` | Invalid copied grid data | Copied data contains invalid cells. Please correct highlighted rows. | Correct highlighted grid cells. | Medium | 422 |
| `GLOBAL-0297` | Required lookup inactive | Selected lookup value is inactive. Please choose an active value. | Select active master value. | Medium | 422 |
| `GLOBAL-0302` | Unexpected server error | An unexpected error occurred. Please try again, or contact support with the reference ID below if it persists. | Retry the action. If it keeps happening, contact support with the correlation ID shown. | Critical | 500 |

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

## Goods Receipt / GRN

| Code | When it happens | What you see | What to do | Severity | HTTP |
|---|---|---|---|---|---|
| `GOODSR-0089` | GRN accepted quantity invalid | Accepted quantity cannot be greater than received quantity. | Correct transaction or send for approval. | High | 422 |
| `GOODSR-0090` | Rejected quantity reason missing | Rejection reason is required for rejected quantity. | Correct transaction or send for approval. | High | 422 |
| `GOODSR-0091` | Barcode generation failed | Barcode could not be generated. Please try again or contact support. | Correct transaction or send for approval. | High | 503 |
| `GOODSR-0095` | Invoice without GRN | Vendor invoice cannot be posted because GRN is not completed. | Correct transaction or send for approval. | High | 422 |
| `GOODSR-0180` | GRN posted | GRN posted successfully. | No action required. | Low | 200 |
| `GRN-0253` | GRN cancellation blocked by downstream invoice | GRN cannot be cancelled because vendor invoice or stock movement exists. | Cancel downstream document first or create reversal. | Critical | 409 |
| `GRN-0254` | Accepted quantity without barcode | Accepted quantity must have generated barcodes before stock posting. | Regenerate barcode or hold posting. | High | 422 |
| `GRN-0255` | QC hold pending | Stock is under QC hold and cannot be sold or transferred. | Complete QC approval/rejection. | High | 200 |

## HR / Payroll

| Code | When it happens | What you see | What to do | Severity | HTTP |
|---|---|---|---|---|---|
| `HR-0267` | Inactive employee login blocked | Employee is inactive. ERP access is blocked. Please contact HR/Admin. | Contact HR/Admin. | High | 409 |
| `HR-0268` | Attendance device/location mismatch | Attendance location does not match assigned work location. Manager review is required. | Submit regularization request. | Medium | 200 |
| `HR-0269` | Shift not assigned | Shift is not assigned for this employee/date. Please configure shift before attendance. | Assign shift and retry. | High | 422 |
| `HR-0531` | Onboarding or offboarding checklist not found | That onboarding or offboarding checklist was not found. | Refresh HR -> Onboarding/Offboarding and open the checklist again. | Low | 404 |
| `HR-0532` | Checklist steps are not a readable step list | This checklist's steps are not in a form the step list can read. | Open the checklist and choose "Use as steps" to turn the text into one step per line. | Low | 422 |
| `HR-0533` | Checklist step does not exist | That step is not on this checklist. | Refresh the checklist - someone may have changed its steps - and tick the step again. | Low | 422 |
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
| `STKLOC-0860` | Stock location name or code missing, duplicate or badly formed | The stock location's name or code is not right. | Give it a name no other location uses, and a code of 2-30 capital letters, digits, '-' or '_'. | Medium | 422 |
| `STKLOC-0861` | Stock location switched off or taken from a store while it still holds stock | This location still holds stock. | Move the stock out with Stock -> Move Stock first, then switch the location off. | High | 422 |
| `STKLOC-0862` | Transaction default location not allowed for that transaction | That location cannot be this transaction's default. | Pick Main, Inward, QC Hold, Damage, Purchase Return or one of your own locations, and two different ones for from and to. | Medium | 422 |
| `STKLOC-0863` | Stock move incomplete or through a location that only transfers / holds change | The stock move is not complete. | Choose the item, a quantity above zero and two different locations the store uses (In-Transit and On Hold change only through transfers and hold codes). | Medium | 422 |
| `STKLOC-0864` | Stock move larger than the free stock in the from location | There is not enough free stock in that location. | Move no more than the location holds (stock promised to an order is not free). | High | 422 |
| `STKLOC-0865` | Stock move record written through the generic record API | Stock moves are made from Stock -> Move Stock. | Use Move Stock, which moves the stock and records it in one step. | Medium | 422 |

## Inventory / Warehouse

| Code | When it happens | What you see | What to do | Severity | HTTP |
|---|---|---|---|---|---|
| `INV-0256` | FEFO violation | Older/earlier-expiry stock must be issued first as per configured rule. | Pick suggested batch/lot. | High | 422 |
| `INV-0257` | Bin capacity exceeded | Selected bin does not have enough capacity. Please choose another bin. | Select another bin or split stock. | Medium | 422 |
| `INV-0258` | Cycle count freeze active | Stock is frozen for counting. Sale/transfer is blocked until count is completed. | Complete or release count session. | High | 200 |
| `INVENT-0101` | Insufficient stock | Insufficient available stock for this item at the selected location. | Select valid stock/location or request approval. | High | 422 |
| `INVENT-0102` | Stock not available for barcode | Selected barcode is not available for transaction. | Select valid stock/location or request approval. | High | 422 |
| `INVENT-0103` | Barcode already consumed | This barcode is already consumed or transferred. Please scan a valid barcode. | Select valid stock/location or request approval. | High | 409 |
| `INVENT-0104` | Blocked stock selected | Blocked stock cannot be issued or sold until it is released. | Select valid stock/location or request approval. | High | 409 |
| `INVENT-0105` | Negative stock not allowed | Stock cannot go negative for this item/location. | Select valid stock/location or request approval. | High | 422 |
| `INVENT-0106` | Batch expired | This batch is expired. Transaction is not allowed. | Select valid stock/location or request approval. | High | 422 |
| `INVENT-0107` | Location/bin missing | Storage location/bin is required for this transaction. | Select valid stock/location or request approval. | High | 422 |
| `INVENT-0108` | Stock count already in progress | Stock count is already in progress for this item/location. | Select valid stock/location or request approval. | High | 409 |
| `INVENT-0109` | Inventory adjustment reason missing | Adjustment reason is required before posting inventory adjustment. | Select valid stock/location or request approval. | High | 422 |
| `INVENT-0110` | Adjustment exceeds tolerance | Adjustment quantity/value exceeds tolerance. Approval is required. | Select valid stock/location or request approval. | High | 422 |
| `INVENT-0114` | Reserved stock blocked | This stock is reserved for another order and cannot be used. | Select valid stock/location or request approval. | High | 409 |
| `INVENT-0115` | Serial/batch mismatch | Scanned barcode does not match selected item/design/combination. | Select valid stock/location or request approval. | High | 422 |
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

