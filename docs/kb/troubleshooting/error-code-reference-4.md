---
title: Error code reference (part 4)
section: Troubleshooting
order: 23
summary: Error codes for User Access & Security to WMS / Logistics, continued from the error code reference.
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
lists every area and explains how to read a code. It holds **34** codes, from
User Access & Security to WMS / Logistics.

## Contents

- [User Access & Security](#user-access-security) - 9 codes
- [Vendor Invoice / AP](#vendor-invoice-ap) - 17 codes
- [WMS](#wms) - 5 codes
- [WMS / Logistics](#wms-logistics) - 3 codes

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

