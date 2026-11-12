# Error Code Reference

<!-- GENERATED FILE - DO NOT EDIT BY HAND.
     Source: `internal/server`'s error catalog (`error_catalog_generated.go`)
     Regenerate: `pwsh docs/update-docs.ps1 -Group Content` -->

> **Generated 2026-09-10.** This page is produced from `internal/server`'s error catalog (`error_catalog_generated.go`), for this source revision.
> It is not release or tenant assurance. Hand edits are lost on the next run - change the source instead.

Every error dialog in the app shows a code like `GLOBAL-0001`. Look it up here.

There are **826** codes. Each row says what the user is shown, what to do about
it, and how serious it is.

**How to read an error dialog** - it has up to three lines: the *headline* (the
catalog's User Message, below), the *detail* (the specific field or value that
failed, written by the engine that rejected it), and the *action* (the User
Action column, below). The detail line is usually the one that tells you what to
fix. See [USER_GUIDE](user-guide.md) §12.

## Contents

- [Admin / Configuration](#admin--configuration) - 21 codes
- [Approvals / Workflow](#approvals--workflow) - 4 codes
- [Audit & Compliance](#audit--compliance) - 2 codes
- [Backup / DR](#backup--dr) - 4 codes
- [CRM](#crm) - 1 codes
- [CRM / Appointments](#crm--appointments) - 1 codes
- [CRM / Campaigns](#crm--campaigns) - 1 codes
- [CRM / Clienteling](#crm--clienteling) - 2 codes
- [CRM / Customer links](#crm--customer-links) - 1 codes
- [CRM / Customers](#crm--customers) - 5 codes
- [CRM / Feedback](#crm--feedback) - 1 codes
- [CRM / Journeys](#crm--journeys) - 1 codes
- [CRM / Lists](#crm--lists) - 1 codes
- [CRM / Messages](#crm--messages) - 1 codes
- [CRM / Pipeline](#crm--pipeline) - 3 codes
- [CRM / Privacy](#crm--privacy) - 1 codes
- [CRM / Referrals](#crm--referrals) - 1 codes
- [CRM / Segments](#crm--segments) - 1 codes
- [CRM / Senders](#crm--senders) - 1 codes
- [CRM / Service](#crm--service) - 1 codes
- [CRM / Templates](#crm--templates) - 1 codes
- [CRM / Vouchers](#crm--vouchers) - 1 codes
- [Channel Connectors](#channel-connectors) - 5 codes
- [Customer / CRM](#customer--crm) - 15 codes
- [Data Import / Excel Upload](#data-import--excel-upload) - 16 codes
- [Deployment / Release](#deployment--release) - 7 codes
- [DocType / Dynamic Metadata](#doctype--dynamic-metadata) - 8 codes
- [Expense Management](#expense-management) - 8 codes
- [Extension Hooks / Customization](#extension-hooks--customization) - 4 codes
- [Finance & Accounting](#finance--accounting) - 96 codes
- [Fixed Assets](#fixed-assets) - 9 codes
- [GST](#gst) - 3 codes
- [Global](#global) - 22 codes
- [Global / Common](#global--common) - 25 codes
- [Goods Receipt](#goods-receipt) - 22 codes
- [Goods Receipt / GRN](#goods-receipt--grn) - 8 codes
- [HR & Payroll](#hr--payroll) - 2 codes
- [HR / Payroll](#hr--payroll) - 15 codes
- [Integration / API](#integration--api) - 14 codes
- [Integrations](#integrations) - 1 codes
- [Inventory](#inventory) - 23 codes
- [Inventory / Warehouse](#inventory--warehouse) - 29 codes
- [Item Master](#item-master) - 2 codes
- [Manufacturing / Production](#manufacturing--production) - 16 codes
- [Master Data](#master-data) - 43 codes
- [Mobile App / Device](#mobile-app--device) - 7 codes
- [Notifications](#notifications) - 6 codes
- [Observability / Incident](#observability--incident) - 3 codes
- [Omnichannel / OMS](#omnichannel--oms) - 5 codes
- [Order Management](#order-management) - 10 codes
- [PIM](#pim) - 27 codes
- [PIM / Product Publishing](#pim--product-publishing) - 8 codes
- [POS / Billing](#pos--billing) - 42 codes
- [POS / Offline & Cash Drawer](#pos--offline--cash-drawer) - 8 codes
- [Patch / Bug Governance](#patch--bug-governance) - 3 codes
- [Payments](#payments) - 7 codes
- [Planning](#planning) - 1 codes
- [Procurement](#procurement) - 1 codes
- [Procurement / RFQ](#procurement--rfq) - 6 codes
- [Purchase Order](#purchase-order) - 73 codes
- [Purchase Requisition](#purchase-requisition) - 6 codes
- [Purchase Return](#purchase-return) - 1 codes
- [Purchase Return / RTV](#purchase-return--rtv) - 5 codes
- [Quality & Maintenance](#quality--maintenance) - 12 codes
- [RFQ](#rfq) - 6 codes
- [Reports](#reports) - 3 codes
- [Reports & Exports](#reports--exports) - 8 codes
- [SaaS / Multi-Tenant](#saas--multi-tenant) - 8 codes
- [Sales / POS](#sales--pos) - 40 codes
- [Sales Return](#sales-return) - 9 codes
- [Security / AI assistant access](#security--ai-assistant-access) - 10 codes
- [Security / Rate Limit](#security--rate-limit) - 6 codes
- [Stock Transfer](#stock-transfer) - 17 codes
- [Tax / GST / E-Invoice](#tax--gst--e-invoice) - 15 codes
- [User Access & Security](#user-access--security) - 9 codes
- [Vendor Invoice / AP](#vendor-invoice--ap) - 7 codes
- [WMS](#wms) - 5 codes
- [WMS / Logistics](#wms--logistics) - 3 codes

---

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
| `PURCHA-0490` | A PO line without a purchase price or quantity (Stage 127.7) | Every line of a purchase order needs its purchase price and quantity. | Enter the purchase price (and quantity) on each line named, then save again. | Medium | 422 |
| `PURCHA-0621` | Sending a cancelled (e.g. auto-cancelled) purchase order to the vendor | This purchase order is cancelled. | Raise a new purchase order for anything still needed. | Medium | 422 |
| `PURCHA-1308` | Typed date further back or ahead than the client's day limit (vendor invoice date, PO expected delivery) | That date is outside the allowed window. | Check the date against the bill. An administrator can enter a date outside the window (Configuration > Procurement & GRN sets it). | Medium | 422 |
| `PURCHA-1316` | PO sent for approval without its commercial terms, or after its quote's validity | This purchase order can't go for approval yet. | Fill in the payment term, delivery terms, expected delivery date and who pays the freight; renew or correct an expired quote date. | Medium | 422 |
| `PURCHA-4961` | Purchase order for a vendor with no state (save, import, submit, RFQ award, supply proposal, bulk upload) | This vendor has no state, so the GST split can't be worked out. | Open the vendor, choose its State (or enter its GSTIN) and save, then save the purchase order again. | Medium | 422 |
| `PURCHA-4965` | Purchase order billed to a store with no state (save, import, submit, RFQ award, supply proposal, bulk upload) | This store has no state, so the GST split can't be worked out. | Open the store, choose its State (or enter the GSTIN on its legal entity) and save, then save the purchase order again. | Medium | 422 |
| `PURCHA-4966` | Goods receipt started from a gate entry that is unknown, already used or cancelled | This gate entry can't start a goods receipt. | Pick an open gate entry, or leave the gate entry blank. | Medium | 422 |
| `PURCHA-4967` | Goods receipt for a different vendor than its gate entry | The vendor does not match the gate entry. | Use the vendor on the gate entry, or pick the right gate entry. | Medium | 422 |
| `PURCHA-4968` | Vendor invoice dated before its purchase order (warning on save) | This invoice is dated before its purchase order. | Check the invoice date and the PO; the invoice is listed in Vendor invoices dated before their PO. | Low | 200 |
| `PURCHA-4970` | Vendor agreement with qty breaks that overlap | These qty breaks overlap. | Change the Qty from / Qty to so each quantity falls in one break. | Medium | 422 |
| `PURCHA-4971` | Vendor agreement with a gap between qty breaks | There is a gap between these qty breaks. | Start the next break right after the previous one ends. | Medium | 422 |
| `PURCHA-4972` | Vendor agreement row with no item, design or category | This row needs an item, design or category. | Pick what the row applies to, or leave a sales price list without a vendor. | Medium | 422 |
| `PURCHA-4973` | Vendor agreement or PO line discount outside 0 to 100, or a negative amount | This discount is not allowed. | Enter a percent from 0 to 100, or an amount that is not negative. | Medium | 422 |
| `PURCHA-4974` | Vendor agreement buy N get M with a missing or negative number, or a pack that is not a whole number | Buy, Get and Pack of need whole numbers above zero. | Fill both Buy and Get, or neither. | Medium | 422 |
| `PURCHA-4975` | Order discount over 100 percent, negative, or above the order value | This order discount is not allowed. | Enter a percent from 0 to 100, or an amount that is less than the order value. | Medium | 422 |
| `PURCHA-4980` | Charge row or rule with an unknown or inactive charge code | That charge code cannot be used. | Pick an active charge code. | Medium | 422 |
| `PURCHA-4981` | Charge row or rule with a selling-only code | That charge code is for selling only. | Pick a code used for buying. | Medium | 422 |
| `PURCHA-4982` | Charge row with no amount, or naming a SKU not on the document | This charge row is not valid. | Enter an amount above nought and SKUs on this document. | Medium | 422 |
| `PURCHA-4983` | Transfer charge on a code paid to the vendor | A transfer charge must go to a third party. | Pick a code paid to a third party. | Medium | 422 |
| `PURCHA-4984` | Charge rule rate not valid | This rule's rate is not allowed. | A percent is above 0 and up to 100; other methods are above 0. | Medium | 422 |
| `PURCHA-4985` | Receipt on a stopped PO line | This line is stopped; nothing more can be received on it. | Ask the buyer to reopen it, or receive it on its own. | Medium | 422 |
| `PURCHA-4986` | Part delivery of a full-delivery-only PO line | This line must be delivered in full. | Receive the whole line, or ask the buyer to clear full delivery only. | Medium | 422 |
| `PURCHA-4987` | PO line control values not valid | The line control values are not valid. | Keep percentages between 0 and 100 and instalments adding up to the line quantity. | Medium | 422 |
| `PURCHA-4988` | Stop or close of a PO line without a reason or nothing open | A reason is needed, or nothing is open on this line. | Pick a Cancellation reason code on a line that still has quantity open. | Low | 422 |
| `PURCHA-4989` | Cancel transfer lines without a reason or after dispatch | Lines can only be cancelled before dispatch, with a reason. | Pick a Cancellation reason code; after dispatch use Return to source. | Medium | 422 |
| `PURCHA-4990` | Transfer excess beyond the item over-delivery tolerance | More arrived than the item's over-delivery tolerance allows. | Receive up to the tolerance, or ask for the item's tolerance to be changed. | Medium | 422 |
| `PURCHA-4991` | Vendor challan no. already used this year (warning) | This vendor challan no. was already used this year. | Check the challan no.; the earlier receipt is named in the warning. | Low | 200 |
| `PURCHA-5020` | Volume rebate action while the setting is off | Volume rebates are off. | Turn on Volume rebates from vendors in Configuration, under Procurement. | Medium | 422 |
| `PURCHA-5021` | Rebate period or vendor not usable | Enter a period (from, to) and a known vendor. | Use dates as YYYY-MM-DD with from not after to, or leave the vendor blank for all. | Medium | 422 |
| `PURCHA-5022` | Rebate claim step taken from the wrong status | This claim is not ready for that step. | A Calculated claim goes for approval; an Approved claim is settled. | Medium | 422 |
| `PURCHA-5023` | Submitting a rebate claim with no rebate | This claim has nothing to claim. | The vendor did not reach the first tier in this period. Recalculate after more bills, or leave it. | Medium | 422 |
| `PURCHA-5024` | Rebate tiers on a vendor agreement are not usable | The rebate target on the agreement is not set up correctly. | Tier 1 needs a target and a percentage; tier 2 needs a higher target and a higher percentage; the basis is Gross or Net. | Medium | 422 |
| `PURCHA-5025` | Rebate claim saved by hand | Rebate claims are made by Calculate. | Use Calculate, Send or Settle on Vendor Rebate Claims. | Medium | 422 |
| `PURCHA-5030` | Goods received before a PO without a reason code (148.38) | Choose why these goods came before a PO. | Pick a reason under Received before PO, or link the PO this receipt is for. | Medium | 422 |
| `PURCHA-5031` | Goods received before a PO without a vendor (148.38) | Choose the vendor for this receipt. | A receipt with no PO needs its vendor; pick it and save again. | Medium | 422 |
| `PURCHA-5032` | Vendor bill paid against goods whose retro PO is not approved (148.38) | This bill is for goods received before a PO. | Pay it after the retro PO is approved; it is listed under Goods received before PO. | Medium | 422 |
| `PURCHA-5033` | Retro PO names a receipt that is not one received before a PO (148.38) | That receipt was not received before a PO. | Pick a receipt marked Received before PO. | Medium | 422 |
| `PURCHA-5034` | Retro PO for a vendor other than the receipt's (148.38) | This PO's vendor differs from the receipt's. | Use the vendor the goods were received from. | Medium | 422 |
| `PURCHA-5035` | Receipt already has a retro PO (148.38) | This receipt already has a retro PO. | Open that PO, or reject it first. | Medium | 422 |
| `PURCHA-5050` | A receipt marked Memo stock while the module is off, or with a bad value | Memo stock is off. | An admin turns on Memo stock under Modules, or leave the field blank. | Medium | 422 |
| `PURCHA-5051` | Returning memo units that were already sold | Part of this memo stock is already sold. | Return only the units still unsold; sold units are on a vendor bill. | Medium | 422 |
| `PURCHA-5052` | A memo receipt with no vendor | Pick the vendor who owns the memo stock. | Enter the vendor on the receipt, then save. | Medium | 422 |
| `PURCHA-5053` | Cancelling a sale that took memo stock | This sale took memo stock, so it cannot be cancelled. | Take the goods back with a return instead. | Medium | 422 |
| `PURCHA-5054` | Memo receipt of a serial or batch item | A serial or batch tracked item cannot be received as memo stock. | Receive it as ordinary stock, or clear Memo stock on the receipt. | Medium | 422 |
| `PURCHA-5055` | A bill, payment, correction or landed cost on a memo receipt | A memo receipt has no bill of its own: its sales raise the bills. | Pay the bill a sale raised. To change the receipt, return the units and receive again. | Medium | 422 |
| `PURCHA-5060` | Vendor link used while procurement.vendor_link is off (148.18) | The vendor link is not switched on. | Ask an administrator to turn it on in Settings. | Medium | 422 |
| `PURCHA-5061` | Vendor link is wrong, expired or revoked (148.18) | This link is not valid or has expired. | Ask the buyer for a new link. | Medium | 422 |
| `PURCHA-5062` | Vendor link for a purchase order that is not open (148.18) | This purchase order is not open. | Check the order status. | Medium | 422 |
| `PURCHA-5063` | Vendor proposal with no change, a bad quantity or a bad date (148.18) | That change cannot be sent. | Give a quantity above zero or a date that is not past. | Medium | 422 |
| `PURCHA-5064` | Vendor change already dealt with or not found (148.18) | That change was already dealt with. | Refresh the list. | Low | 422 |
| `PURCHA-5065` | Vendor change cannot be applied to the line (148.18) | This change cannot be applied to the line. | Change the line on the order, or reject the change. | Medium | 422 |
| `PURCHA-5066` | Vendor invoice file refused: type, size, count or detail (148.18) | That file cannot be accepted. | Send a PDF, PNG or JPG under 2 MB. | Medium | 422 |
| `PURCHA-5067` | Vendor file not found or marked with an unknown status (148.18) | That file was not found. | Refresh the list. | Low | 422 |
| `PURCHA-5068` | Revoke a vendor link when none is live (148.18) | This order has no live link. | Make a link first. | Low | 422 |
| `PURCHA-5105` | Goods receipt against a drop-ship purchase order | This is a drop-ship order: no goods receipt. | The vendor ships to the customer. Confirm shipment instead. | Medium | 409 |
| `PURCHA-5106` | Vendor confirmation before the purchase order is approved | The purchase order is not approved yet. | Approve it, then confirm the shipment. | Medium | 409 |
| `PURCHA-5107` | Drop-ship vendor bill or lines could not be made | The vendor bill could not be made. | Check the purchase order and try again. | Medium | 422 |
| `PURCHA-5130` | Pack quantity on an item with no purchase unit (148.35) | This item has no purchase unit. | Set a purchase unit on the item, or key pieces. | Medium | 422 |
| `PURCHA-5132` | Packs convert to a part-piece on a PO (148.35) | A PO takes whole pieces. | Order whole pieces; receive the part-pack on the goods receipt. | Medium | 422 |
| `PURCHA-5133` | Pack quantity or pack price not a valid number (148.35) | Pack quantity or price is not valid. | Enter a pack quantity above zero and a price of zero or more. | Medium | 422 |

## Purchase Requisition

| Code | When it happens | What you see | What to do | Severity | HTTP |
|---|---|---|---|---|---|
| `PURCHA-5010` | Requisition line with no item, an unknown item, a quantity not above zero or not whole, no store, an unknown store or an unusable vendor | This request line is not complete. | Choose an item, a whole quantity above zero and a store; leave the vendor empty or choose an active one. | Medium | 422 |
| `PURCHA-5011` | Requisition with no item lines and no description, quantity or amount | Add item lines, or fill description, quantity and amount. | Add at least one item line, or complete the three header fields. | Medium | 422 |
| `PURCHA-5012` | Edit of a requisition line already on a purchase order | This line is already on a purchase order. | Change the purchase order, or cancel it to free the line. | Medium | 422 |
| `PURCHA-5013` | Combine with no open request line for the vendor | No open request lines for this vendor. | Check the vendor on the lines, or whether another person already combined them. | Low | 422 |
| `PURCHA-5014` | Combine with no vendor, or an unknown or unusable vendor or store | Choose a vendor to combine for. | Choose an active vendor, and a store that exists if you limit it to one. | Low | 422 |
| `PURCHA-5016` | One-shot purchase order conversion of a requisition that has item lines | This request has item lines. | Use Combine open requests for the vendor. | Low | 422 |

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
| `QUALIT-0600` | Inspection result lines do not total the lot or are not whole quantities | The result lines do not match the lot. | Enter whole quantities that add up to the lot, each marked Pass or Fail. | Medium | 422 |
| `QUALIT-0601` | Inspection lot not found or already decided | That lot cannot take a result. | Open the pending lots and pick one that is still waiting. | Low | 422 |
| `QUALIT-0602` | Inspection by rule or stock age inspection is switched off | This check is off. | An administrator turns it on under Configuration > Quality. | Low | 422 |
| `QUALIT-0603` | Inspection lot or stock age inspection typed in by hand | This record is made by the system. | Lots come from a receipt; age inspections come from the daily check. | Low | 422 |
| `QUALIT-0604` | Mandatory check waived, waived without a reason, or age outcome incomplete | That decision cannot be recorded. | A mandatory check cannot be waived; give a reason, and for scrap the lines and reason code, for re-polish the job-work. | Medium | 422 |
| `QUALIT-0605` | Inspection rule saved without what it needs (days or a category or item) | That rule is incomplete. | Give the days between checks and a category or item. | Low | 422 |

## RFQ

| Code | When it happens | What you see | What to do | Severity | HTTP |
|---|---|---|---|---|---|
| `RFQ-0551` | RFQ line incomplete or no lines | An RFQ line is incomplete. | Give each line an item or a description, a unit and a quantity above zero. | Medium | 422 |
| `RFQ-0552` | RFQ not open for this change | This RFQ is not open for that. | Lines change only on a Draft; vendors, quotes and responses only while it is Draft or Sent. | Medium | 422 |
| `RFQ-0553` | Vendor not active, not invited, or already quoted | That vendor cannot be used here. | Choose an active vendor; a vendor that already quoted cannot decline. | Medium | 422 |
| `RFQ-0554` | Quote line or terms invalid | This quote cannot be recorded as entered. | Price at least one line; give the unit conversion when the vendor quotes in another unit, a validity date that is not over, and no negative amounts. | Medium | 422 |
| `RFQ-0555` | RFQ already awarded, or quote not awardable | This RFQ has already been awarded, or that quote cannot be. | Open the purchase order created by the award; a second award is refused. | Medium | 422 |
| `RFQ-0556` | Awarded quote cannot become a purchase order | The quote cannot become a purchase order yet. | Pick an item on every quoted RFQ line, use whole quantities, and award a quote that is still valid. | Medium | 422 |

## Reports

| Code | When it happens | What you see | What to do | Severity | HTTP |
|---|---|---|---|---|---|
| `REPORT-0987` | Report filter names a store outside the user's store access | You can only see reports for your own stores. | Choose one of your stores in the store filter, or leave it blank to see all of yours. An administrator widens store access on the User or Role screen. | Medium | 403 |
| `REPORT-0988` | Dashboard widget asks a dataset for a field, total or filter it does not have, or one the role may not read | This widget's data cannot be shown. | Edit the widget and pick its fields again from the lists - group by a name or a date, total a number. If your role may not read the record type, ask an administrator. | Low | 422 |
| `REPORT-0989` | Dashboard not valid, not found, or changed by someone other than its maker | This dashboard cannot be saved. | Give it a name and at least one widget. To change a dashboard someone else made, use Copy and change your copy. | Low | 422 |

## Reports & Exports

| Code | When it happens | What you see | What to do | Severity | HTTP |
|---|---|---|---|---|---|
| `REPORT-0160` | Report no data | No data found for the selected filters. Please change filters and try again. | Correct details or retry. Contact admin if repeated. | Medium | 422 |
| `REPORT-0161` | Export limit exceeded | This report is too large to show. | Narrow the filters, then run it again. | Medium | 422 |
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

---

*A code with an HTTP 5xx status is an internal failure. The detail line is deliberately withheld from the response in that case - give your administrator the correlation ID shown in the dialog instead.*
