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

Every refusal in this application carries a code. There are **826** of them,
across **78** areas. This page is produced from the running catalog, so it
cannot describe a code the application does not have, or miss one it does.

Search is usually faster than scrolling: type the code into the search box
at the top of the sidebar. If you have not met a code yet and want to know how
to read the dialog it appears in, start with
[Reading an error code](error-codes.md).

> [!NOTE]
> A code with an HTTP status of 500 or above is an internal failure, not
> something you did. The detail line is deliberately withheld in that case -
> give your administrator the correlation id shown in the dialog instead.

The list runs over **3** pages. The contents below cover every area and
open the right page.

## Contents

- [Admin / Configuration](#admin-configuration) - 21 codes
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
- [CRM / Pipeline](#crm-pipeline) - 3 codes
- [CRM / Privacy](#crm-privacy) - 1 codes
- [CRM / Referrals](#crm-referrals) - 1 codes
- [CRM / Segments](#crm-segments) - 1 codes
- [CRM / Senders](#crm-senders) - 1 codes
- [CRM / Service](#crm-service) - 1 codes
- [CRM / Templates](#crm-templates) - 1 codes
- [CRM / Vouchers](#crm-vouchers) - 1 codes
- [Channel Connectors](#channel-connectors) - 5 codes
- [Customer / CRM](#customer-crm) - 15 codes
- [Data Import / Excel Upload](#data-import-excel-upload) - 16 codes
- [Deployment / Release](#deployment-release) - 7 codes
- [DocType / Dynamic Metadata](#doctype-dynamic-metadata) - 8 codes
- [Expense Management](#expense-management) - 8 codes
- [Extension Hooks / Customization](#extension-hooks-customization) - 4 codes
- [Finance & Accounting](#finance-accounting) - 96 codes
- [Fixed Assets](#fixed-assets) - 9 codes
- [GST](#gst) - 3 codes
- [Global](#global) - 22 codes
- [Global / Common](#global-common) - 25 codes
- [Goods Receipt](#goods-receipt) - 22 codes
- [Goods Receipt / GRN](#goods-receipt-grn) - 8 codes
- [HR & Payroll](error-code-reference-2.md#hr-payroll) - 2 codes
- [HR / Payroll](error-code-reference-2.md#hr-payroll) - 15 codes
- [Integration / API](error-code-reference-2.md#integration-api) - 14 codes
- [Integrations](error-code-reference-2.md#integrations) - 1 codes
- [Inventory](error-code-reference-2.md#inventory) - 23 codes
- [Inventory / Warehouse](error-code-reference-2.md#inventory-warehouse) - 29 codes
- [Item Master](error-code-reference-2.md#item-master) - 2 codes
- [Manufacturing / Production](error-code-reference-2.md#manufacturing-production) - 16 codes
- [Master Data](error-code-reference-2.md#master-data) - 43 codes
- [Mobile App / Device](error-code-reference-2.md#mobile-app-device) - 7 codes
- [Notifications](error-code-reference-2.md#notifications) - 6 codes
- [Observability / Incident](error-code-reference-2.md#observability-incident) - 3 codes
- [Omnichannel / OMS](error-code-reference-2.md#omnichannel-oms) - 5 codes
- [Order Management](error-code-reference-2.md#order-management) - 10 codes
- [PIM](error-code-reference-2.md#pim) - 27 codes
- [PIM / Product Publishing](error-code-reference-2.md#pim-product-publishing) - 8 codes
- [POS / Billing](error-code-reference-2.md#pos-billing) - 42 codes
- [POS / Offline & Cash Drawer](error-code-reference-2.md#pos-offline-cash-drawer) - 8 codes
- [Patch / Bug Governance](error-code-reference-2.md#patch-bug-governance) - 3 codes
- [Payments](error-code-reference-2.md#payments) - 7 codes
- [Planning](error-code-reference-2.md#planning) - 1 codes
- [Procurement](error-code-reference-2.md#procurement) - 1 codes
- [Procurement / RFQ](error-code-reference-2.md#procurement-rfq) - 6 codes
- [Purchase Order](error-code-reference-3.md#purchase-order) - 73 codes
- [Purchase Requisition](error-code-reference-3.md#purchase-requisition) - 6 codes
- [Purchase Return](error-code-reference-3.md#purchase-return) - 1 codes
- [Purchase Return / RTV](error-code-reference-3.md#purchase-return-rtv) - 5 codes
- [Quality & Maintenance](error-code-reference-3.md#quality-maintenance) - 12 codes
- [RFQ](error-code-reference-3.md#rfq) - 6 codes
- [Reports](error-code-reference-3.md#reports) - 3 codes
- [Reports & Exports](error-code-reference-3.md#reports-exports) - 8 codes
- [SaaS / Multi-Tenant](error-code-reference-3.md#saas-multi-tenant) - 8 codes
- [Sales / POS](error-code-reference-3.md#sales-pos) - 40 codes
- [Sales Return](error-code-reference-3.md#sales-return) - 9 codes
- [Security / AI assistant access](error-code-reference-3.md#security-ai-assistant-access) - 10 codes
- [Security / Rate Limit](error-code-reference-3.md#security-rate-limit) - 6 codes
- [Stock Transfer](error-code-reference-3.md#stock-transfer) - 17 codes
- [Tax / GST / E-Invoice](error-code-reference-3.md#tax-gst-e-invoice) - 15 codes
- [User Access & Security](error-code-reference-3.md#user-access-security) - 9 codes
- [Vendor Invoice / AP](error-code-reference-3.md#vendor-invoice-ap) - 7 codes
- [WMS](error-code-reference-3.md#wms) - 5 codes
- [WMS / Logistics](error-code-reference-3.md#wms-logistics) - 3 codes

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
| `GLOBAL-9140` | Landing screen set with no role | Choose the role. | Pick a role on the Roles screen. | Low | 422 |
| `GLOBAL-9141` | Landing screen not one of the offered screens | That screen cannot be a landing. | Choose one of the screens in the list. | Low | 422 |
| `PLATFORM-0316` | Data console edit refused for a protected table or column | This value cannot be changed from the data console. | Keys, hidden (secret or personal) values, audit tables and the ledger are protected. Change the record through its own screen instead. | Medium | 422 |
| `PLATFORM-0317` | Cross-client action (data console, client list, plans, sandboxes) by someone who is not a platform operator | Only a platform operator can see or change other clients. | Ask the server administrator to add your client and username to ERP_PLATFORM_OPERATORS. | High | 403 |
| `PLATFORM-0318` | Scale test run on a client that is not a sandbox | The scale test runs only on a sandbox client. | Run it signed in to a sandbox client (a platform operator creates one with POST /api/v1/admin/sandbox-tenants). The test posts sales, which must never reach a real client's books. | High | 422 |
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
| `CRMPLT-1130` | Expired quotation converted to an order | This quotation has expired, so it cannot become an order. | Revise the quotation with a new valid-until date and get it approved. | Low | 409 |

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
| `CUSTOM-0621` | Points transfer not valid (switched off, same customer, no points, unknown customer or transfer) | This points transfer can't be made. | Check the setting is on, choose two different customers and enter the points. | Low | 422 |
| `CUSTOM-0622` | Transfer larger than the customer's usable points | That is more points than can be sent. | Send at most the usable points - pending, expired and used points cannot be sent. | Medium | 422 |
| `CUSTOM-0623` | Transfer over the customer's daily limit | This customer has reached today's limit for sending points. | Send fewer points, or try again tomorrow. | Medium | 422 |
| `CUSTOM-0624` | Transfer code wrong, expired, used or for another transfer | That code didn't work. | Ask the customer for the latest code, or start the transfer again. | Medium | 422 |
| `CUSTOM-0690` | Loyalty scheme overlaps another for a store and dates | Another scheme already covers these stores on these dates. | Change the dates or the stores, or switch the other scheme off. | Medium | 422 |
| `CUSTOM-0691` | Customer is blocked from loyalty points | This customer is blocked from using points. | Take the sale without points, or lift the block on the customer. | Medium | 422 |
| `CUSTOM-0692` | Loyalty scheme or block not valid (no reason, end before start, or spend per point below 1) | Check the highlighted loyalty field. | Give a reason for the block, end the scheme after it starts, and set spend per point to 1 or more. | Low | 422 |

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
| `DATAIM-0480` | File larger than the staged-import cap | The file is too large. | Split it into files under 5 MB and stage each. | Low | 422 |
| `DATAIM-0481` | Import batch not found, or not at the step asked | That import batch cannot do that now. | Open the batch and use the step it is at: check, preview, approve, commit. | Low | 422 |
| `DATAIM-0482` | Import rules changed since the batch was checked | The import rules changed since this batch was checked. | Check the batch again, then preview and approve it. | Low | 422 |
| `DATAIM-0483` | Commit asked of a batch that is not approved | This batch is not approved yet. | Approve it first - if approval is set up, someone else decides it. | Low | 422 |
| `DATAIM-0484` | Commit asked while another commit of the batch runs | This batch is being committed right now. | Wait for it to finish, then open the batch for the result. | Low | 422 |
| `DATAIM-0485` | Approve asked of a batch with no valid rows | There are no valid rows to import. | Fix the rows listed in the preview and stage the file again. | Low | 422 |
| `DATAIM-0486` | File encoding cannot be read | The file could not be read as text. | In Excel use Save As, CSV UTF-8, and stage it again. | Low | 422 |
| `DATAIM-0487` | Stated column total does not tie to the file | The file does not tie to the total you stated. | Check the file against the stated total, fix the rows and stage it again. | Medium | 422 |

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
| `EXT-0292` | Hook API version not served | This hook was written for a hook version this ERP does not serve. | Register it against the current hook version named below. | Medium | 422 |

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
| `FIN-0850` | A sale names a piece or batch it cannot sell | This piece or batch cannot be sold here. Check the code and the store. | Scan the piece or batch again, or sell it from the store that holds it. | Medium | 422 |
| `FIN-0851` | A credit note names a piece it cannot bring back | This piece cannot be brought back on this note. | Check that the piece was sold on this invoice and has not already come back. | Medium | 422 |
| `FIN-0860` | Order accepted over the customer's credit limit - put on Credit hold | This order takes the customer over their credit limit, so it is on hold. | Ask a Store Manager to approve the release request in the approval inbox, or cancel the order. | Medium | 422 |
| `FIN-0861` | Sales invoice posting refused - a stop customer over the credit limit | This customer is over their credit limit and set to stop, so the invoice cannot be posted. | Collect payment or raise the customer's credit limit, then post again. | Medium | 422 |
| `FIN-0870` | Posting to a blocked account | That account is blocked, so nothing was posted. | Unblock the account in the Chart of Accounts, or point the posting rule at another account. | Medium | 422 |
| `FIN-0871` | Posting to an account that is not in the chart | That account is not in the chart of accounts, so nothing was posted. | Add the account in the Chart of Accounts, or correct the posting rule that names it. | Medium | 422 |
| `FIN-0872` | Manual journal line on a no-manual-posting control account | That account is a control account and takes no manual journal. | Post through the account's own document, or ask an accountant to lift the flag in the Chart of Accounts. | Medium | 422 |
| `FIN-0873` | Chart of accounts change request is not valid | That account change cannot be saved. | Fix the field named in the message and save again. | Low | 422 |
| `FIN-0890` | Charge code account missing, blocked or not in the chart | That account cannot take this charge. | Pick an unblocked account from the Chart of Accounts. | Medium | 422 |
| `FIN-0891` | Charge code account of the wrong type | That account is the wrong type for this charge. | Pick an account of the type the message names. | Medium | 422 |
| `FIN-0892` | Buying charge code without a payee or into-cost answer | This charge code is missing a setting. | Fill in Paid to and Into item cost. | Medium | 422 |
| `FIN-0893` | Charge code GST rate outside 0 to 100 | This GST rate is not allowed. | Enter a rate from 0 to 100. | Medium | 422 |
| `FIN-0894` | Charge entry saved by hand | Charge entries are written by the system. | Post the receipt or book the bill instead. | Medium | 422 |
| `FIN-0895` | Charge entry that cannot be billed | This charge cannot be billed. | Pick a charge that is awaiting a bill. | Medium | 422 |
| `FIN-0896` | Charge bill with a field missing or not valid | This bill is not complete. | Fill in the vendor, bill number, date and amount. | Medium | 422 |
| `FIN-0897` | Charge entry already on a different bill | This charge is already on another bill. | Open that bill, or pick another charge. | Medium | 422 |
| `FIN-0898` | Charge bill from a vendor other than the charge's carrier | This bill is from a different vendor than the charge. | Pick the carrier named on the charge. | Medium | 422 |
| `FIN-0899` | Charge bill number already used for this vendor | That bill number is already used. | Check the number on the bill. | Medium | 422 |
| `FIN-0910` | Vendor bill deleted with a payment or approval on it | This bill has a payment or an approval on it. | Reverse the payment or decide the approval first. | Medium | 422 |
| `FIN-0911` | Production or job work output given a piece code already in use | That piece code is already in use. | Scan or type another code. | Medium | 422 |
| `FIN-0912` | Production or job work output named on an item that keeps no codes or lots | This item keeps no piece codes or lots. | Leave them blank, or set the item's tracking first. | Medium | 422 |
| `FIN-0920` | Vendor advance with a missing vendor, amount or date | The advance needs a vendor, a positive amount and a valid date. | Pick the vendor, enter the amount in rupees and paise, and check the date. | Medium | 422 |
| `FIN-0921` | Vendor advance against a purchase order that is not the vendor's or is closed | This purchase order cannot take an advance. | Choose an open purchase order of the same vendor, or leave the order blank. | Medium | 422 |
| `FIN-0922` | Applying or refunding more than the advance still holds | That is more than is left on the advance. | Use the open amount shown on the advance, or less. | Medium | 422 |
| `FIN-0923` | Applying an advance to a bill that cannot take it | This bill cannot take the advance. | Apply it to a Matched rupee bill of the same vendor (and the same purchase order, if the advance names one). | Medium | 422 |
| `FIN-0924` | Cancelling an advance that was already applied or refunded | This advance has been used, so it cannot be cancelled. | Refund what is still open; the applied part stays on its bill. | Medium | 422 |
| `FIN-0925` | Applying or refunding an advance with nothing open | Nothing is open on this advance. | Check the advance's status; a refunded, fully applied or cancelled advance has nothing left. | Medium | 422 |
| `FIN-0926` | Vendor advance saved by hand | Vendor advances are made by Give advance. | Use Give advance, Apply, Refund or Cancel on Vendor Advances. | Medium | 422 |
| `FIN-0930` | Bills without a PO switched off | Bills without a PO are switched off. | Ask an admin to turn on Bills without a PO in Settings. | Medium | 422 |
| `FIN-0931` | Expense bill vendor, number, date or lines not usable | This bill can't be entered. | Fix what the message names. | Medium | 422 |
| `FIN-0932` | Expense bill line account, amount, rate or credit not usable | A line on this bill can't be booked. | Fix the line the message names. | Medium | 422 |
| `FIN-0933` | Expense bill typed in or changed by a plain save | Bills without a PO are entered on their own page. | Use New on Bills without a PO. | Medium | 422 |
| `FIN-0934` | Expense bill link or cancel refused | That can't be done to this bill. | Fix what the message names. | Medium | 422 |
| `FIN-0935` | Expense bill not booked yet | This bill is not booked yet. | Wait for its approval. | Medium | 422 |
| `FIN-0950` | Journal line missing a dimension its account requires | A journal line is missing a required dimension. | Fill the cost centre, department, store, employee or tag the message names, or ask finance to exempt the account. | Medium | 422 |
| `FIN-0951` | Journal line dimension suspended, unknown or too long | A journal line uses a dimension that is not usable. | Pick an active cost centre, department, store or employee; a suspended one stays on old entries only. | Medium | 422 |
| `FIN-0952` | Line dimensions on a voucher in another currency | Line dimensions are not available in another currency. | Set the dimensions on the whole voucher, or enter it in the home currency. | Medium | 422 |
| `FIN-0953` | Journal template whose lines do not balance or split to 100 | The template lines do not add up. | Make debits equal credits, or each side of a percent split total 100. | Medium | 422 |
| `FIN-0954` | Journal dimension rule with no dimension or a reversed account range | The dimension rule is incomplete. | Name the dimension for a required rule, and give the account range from low to high. | Medium | 422 |
| `FIN-0970` | GSTR-1 filed month: sales posting refused | This month is locked: its GSTR-1 is filed. | Correct it with a credit or debit note, or ask an admin to unmark the month. | Medium | 422 |
| `FIN-0971` | Sales posting in a period On Hold | This period is on hold for sales. | Release the period, or date the sale in an open period. | Medium | 422 |
| `FIN-0972` | GSTR-1 mark, unmark or period hold refused | That cannot be done to this month or period. | Check the month has ended, is marked, and give a reason to unmark. | Medium | 422 |
| `FIN-0973` | Customer-vendor pair refused | These two cannot be paired. | Pick one customer and one vendor that are not already in an active pair. | Medium | 422 |
| `FIN-0974` | Set-off voucher input refused | The set-off is not complete. | Pick the pair, an amount, a clearing account and valid dates. | Medium | 422 |
| `FIN-0975` | Set-off clearing account missing or unusable | No clearing account is set for set-off. | An admin picks it in Settings under Finance. | Medium | 422 |
| `FIN-0976` | Set-off more than a balance | That is more than one side owes. | Use the balances shown for the pair, or less. | Medium | 422 |
| `FIN-0977` | Set-off reversal refused | This set-off cannot be reversed now. | Reverse a posted set-off once, on or after its own date. | Medium | 422 |
| `FIN-0978` | Set-off voucher saved by hand as posted | Set-offs are posted by approval. | Submit the draft for approval. | Medium | 422 |
| `FIN-0979` | Invoice line names an account that is not an income account | That is not an income account. | Pick a Revenue account from the chart. | Medium | 422 |
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
| `TAXGST-1330` | HSN value bands not usable | The value bands can't be saved. | Write them as limit:rate, lowest first, with only the last one open, like 1000:5,2500:12,:18. | Medium | 422 |
| `TAXGST-1331` | GST credit value not usable | The GST credit must be Eligible or Blocked. | Pick Eligible or Blocked. | Medium | 422 |

## Global

| Code | When it happens | What you see | What to do | Severity | HTTP |
|---|---|---|---|---|---|
| `GLOBAL-0303` | Request method not allowed | This request method is not supported here. | Use GET to read and POST, PUT or DELETE to change data, as the API documentation shows. | Low | 405 |
| `GLOBAL-0304` | Request content type or encoding not accepted | The request body is in a format this address does not accept. | Send JSON (Content-Type: application/json), or a file upload as multipart/form-data, without compression. | Low | 415 |
| `GLOBAL-0305` | Request body too large | The request is too large to accept. | Send less data at once - split a large file or list into smaller parts. | Low | 413 |
| `GLOBAL-0306` | Request headers too large | The request carries too many or too large headers. | Sign out and back in; if this persists, contact support - a browser extension or proxy may be adding headers. | Low | 431 |
| `GLOBAL-0307` | Malformed, ambiguous or over-nested JSON request | The request data could not be accepted. | Check the request is one valid JSON value, with no field repeated in the same object. | Low | 400 |
| `GLOBAL-0308` | Request exceeded its time limit | This took longer than the time a single request is allowed, so it was stopped. | Send less at once - split a large import file into smaller files, or narrow a report's filters and use background export. | Medium | 504 |
| `GLOBAL-0309` | Field holds an invisible control or direction character | A field holds an invisible character that cannot be saved. | Retype the field named below, or paste it as plain text. | Low | 422 |
| `GLOBAL-0310` | Too many report runs still running | Too many reports are still running for this business, so this one did not start. | Try again in a minute. For a large report, use Export - it runs in the background. | Medium | 503 |
| `GLOBAL-0311` | Identifier mixes look-alike alphabets | A code or identifier mixes letters from different alphabets. | Retype the field named below in one alphabet - a look-alike letter (for example a Cyrillic letter inside a Latin code) was pasted in. | Low | 422 |
| `GLOBAL-1315` | Reprint refused (no Document Reprint permission, no reason, approval needed or already used, no approver set) | This document can't be printed again yet. | Give the reason; ask a manager for the Document Reprint permission or send a Reprint Request and print once it is approved. | Medium | 422 |
| `GLOBAL-1319` | Print refused: a tax document from a store with no GSTIN, or a template version that cannot be decided (not waiting, own change, not found) | This can't be printed or changed yet. | Add the GSTIN to the store's legal entity (Setup > Legal Entities); a template change is approved by someone other than the person who saved it. | Medium | 422 |
| `GLOBAL-4971` | Same command key still being processed | This is still being processed. | Wait a moment and check whether it went through before trying again - do not enter it again. | Medium | 409 |
| `GLOBAL-4972` | Command key reused for a different request | This request key was already used for a different request. | Send a new key for a new request; resend the same request to get the first answer. | Medium | 409 |
| `GLOBAL-9110` | Go-live scope answers not usable | The scope answers are not usable. | Check the answers marked. | Medium | 422 |
| `GLOBAL-9111` | Evidence on a go-live check that is unknown or read from live data | This check cannot take evidence. | Fix the data, or waive the check. | Medium | 422 |
| `GLOBAL-9112` | Go-live evidence with no note or reference | Add a note or a reference. | Say what proves the check. | Medium | 422 |
| `GLOBAL-9113` | Waiver without a reason or expiry, or on a check that is not open | The waiver needs a reason and a date. | Give a reason and an expiry within a year. | Medium | 422 |
| `GLOBAL-9114` | Sign-off while a Critical or Blocking check is open | Some checks are still open. | Close them, or waive them with a reason. | Medium | 422 |
| `GLOBAL-9115` | Sign-off by the person who waived a Critical check | Another person must sign. | Ask someone else to sign off. | Medium | 422 |
| `GLOBAL-9116` | Scope decision on a record that cannot be set out of scope | That cannot be set out of scope. | Pick an existing entity, location or cost centre, and give a reason. | Medium | 422 |
| `GLOBAL-9117` | Reconciliation accepted while the books do not tie | The books do not tie yet. | Fix the difference first. | Medium | 422 |
| `GLOBAL-9118` | Sign-off of a pack already signed (or signed a moment ago by someone else) | Already signed. | Nothing to do; reopen a check if it must be signed again. | Medium | 422 |

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

