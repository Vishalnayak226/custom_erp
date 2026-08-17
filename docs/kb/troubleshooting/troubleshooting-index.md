---
title: Troubleshooting by symptom
section: Troubleshooting
order: 2
summary: Start from what you saw, not from a code - each symptom below names the codes it produces and what actually fixes it.
audience: everyone
last_verified: 2026-08-17
screens: [pos, oms, inventory, purchase-orders, grn, sales-invoices, marketplace, transfers]
---

# Troubleshooting by symptom

The [error code reference](error-code-reference.md) answers "what does this code
mean". This page answers the question people actually arrive with: "this
happened, now what".

If you have a code in front of you, search for it - the search box at the top of
the sidebar finds it faster than scrolling. If you do not, find your symptom
below.

> [!TIP]
> Before anything else, read the **detail** line of the dialog. It names the
> specific field, record or value that failed, and it is almost always the line
> that tells you what to fix. See [Reading an error code](error-codes.md).

## Signing in

| What you saw | Usually | Codes | Do this |
|---|---|---|---|
| Rejected after several tries | A rate limit, not a lockout | `SEC-0280` | Wait a minute and try again |
| "Account locked" | A genuine lock after repeated failures | `USERAC-0022` | An administrator unlocks it |
| Code from the authenticator refused | Wrong code, or clock drift on the phone | `USERAC-0025` | Retry with a fresh code; if it persists, use a recovery code |
| "No role is assigned to this user" | The account was created but never given a role | `USERAC-0026` | An administrator assigns a role |
| Signed out mid-task | Session expired, or your role changed while signed in | `GLOBAL-0009` | Sign in again. The server re-checks your account on every request, so a role change takes effect immediately |
| "This browser origin is not allowed" | You are on an address that is not the approved one | `SEC-0284` | Use the URL your administrator gave you |

## Being refused an action

| What you saw | Usually | Codes | Do this |
|---|---|---|---|
| "You do not have permission" | Your role | `GLOBAL-0011` | Ask an administrator. Nothing lets you grant it to yourself |
| "You do not have access to this store" | Role is fine, store scope is not | `USERAC-0027` | Administrator adds the location to your access |
| "The same user cannot create and approve" | Self-approval, which is never allowed | `USERAC-0029` | A second person approves it |
| "This action is not allowed for the current document status" | The document has moved past that action | `GLOBAL-0019` | Check the status. Shipped, Delivered, Closed and Cancelled close most actions |
| Approve button missing | You raised the document, or it is not in an approvable state | - | Both are the rules working |
| "This approval action is already completed" | Someone decided it while your screen was open | `APPROV-0158` | Refresh |
| "No approver is configured" | An approval rule points at nobody | `APPROV-0157` | Administrator fixes the rule |

## Saving a record

| What you saw | Usually | Codes | Do this |
|---|---|---|---|
| "Required value is missing" | A mandatory field | `GLOBAL-0001` | The detail line names the field |
| "Invalid format" | A field with a shape - GSTIN, PAN, IFSC, phone, email, PIN | `GLOBAL-0002` | The placeholder shows the expected shape |
| "A record with the same … already exists" | A unique key collision | `GLOBAL-0003`, `MASTER-0040`, `MASTER-0053` | Use a unique value, or open the existing record |
| "This record was updated by another user" | Two people editing at once | `GLOBAL-0006` | Refresh and reapply your change |
| "Cannot be deleted because it is already used" | Referenced by transactions | `GLOBAL-0017` | Deactivate rather than delete |
| "The selected record is inactive" | A link pointing at a deactivated master | `GLOBAL-0018`, `MASTER-0054` | Reactivate it, or pick an active one |
| Record not found after selecting it | Someone deleted it, or a link is stale | `GLOBAL-0004` | Refresh and reselect |

## Items and tax

| What you saw | Usually | Codes | Do this |
|---|---|---|---|
| Item will not save | Missing HSN | `MASTER-0042` | Enter the HSN code. It is required whatever the tax rate |
| "Invalid HSN code" | Wrong length | `MASTER-0043` | 4, 6 or 8 digits, as configured |
| "Tax category is required" | No tax classification | `MASTER-0044` | Set GST Rate, or Tax Treatment for genuinely untaxed goods |
| "Unit of Measure is required" | No UOM | `MASTER-0045` | Set it on the item |
| "MRP cannot be lower than selling price" | Prices the wrong way round | `MASTER-0047` | Correct the price |
| "Invalid GSTIN" | Not 15 characters, or malformed | `MASTER-0049` | Check the number with the vendor |
| "PAN in GSTIN does not match the vendor PAN" | One of the two is wrong | `MASTER-0050` | Verify both against the vendor's documents |
| "Invalid mobile number" | Does not fit the tenant's country rules | `MASTER-0051` | See [Country codes and phone number rules](country-phone-rules.md) |

## Tax on a transaction

| What you saw | Usually | Codes | Do this |
|---|---|---|---|
| "GSTIN is required" | A party without a registration | `TAXGST-0067` | Fill it in on the vendor or customer |
| "GSTIN state code does not match the selected state" | The first two digits disagree with the state field | `TAXGST-0068` | Fix whichever is wrong - the GSTIN is the authority |
| "Place of Supply is required" | Cannot decide the tax without it | `TAXGST-0069` | Set it on the document |
| "GST rate is missing for one or more items" | An item with no tax configuration | `TAXGST-0070` | Fix the Item, not the transaction |
| "Tax type does not match the place of supply" | CGST/SGST where IGST belongs, or the reverse | `TAXGST-0071` | Verify billing state against supply state |
| "Taxable value does not match item value" | Discounts or charges applied inconsistently | `TAXGST-0075` | Recalculate the lines |
| "GST return data is locked" | The period is closed | `TAXGST-0077` | Corrections go in the current period, as notes |

## At the till

| What you saw | Usually | Codes | Do this |
|---|---|---|---|
| "Cash opening is required before billing" | No open session at this location | `POSOFF-0238` | Open a session, entering the cash physically present |
| "POS profile is not configured" | This register was never set up | `POSOFF-0237` | Administrator creates the POS profile |
| "Cash closing is pending for this shift" | A previous shift was never closed | `POSOFF-0239` | Close it, entering the counted cash |
| "Cash variance exceeds allowed tolerance" | The count is too far from expected | `POSOFF-0240` | Enter a reason; a manager approves it |
| "Payment terminal is not mapped" | Card machine not linked to this register | `POSOFF-0243` | Administrator maps it |
| Offline sale did not appear | It synced with a conflict, or was a duplicate | `POSOFF-0241`, `POSOFF-0242` | Review **POS » Offline Sync Review** |
| Sale sits as Pending Approval | A large manual discount | - | A manager decides it. Nothing is charged, and no receipt prints, until then |

## Stock

| What you saw | Usually | Codes | Do this |
|---|---|---|---|
| "Insufficient available stock" | Nothing free to sell here | `INVENT-0101` | Check the goods receipt was posted, then that putaway happened |
| "This stock is reserved for another order" | Committed elsewhere | `INVENT-0114` | Read available-to-sell, not the raw count |
| "Blocked stock cannot be issued or sold" | A bin condition | `INVENT-0104` | Release it on **Bin Conditions**, if it should be sellable |
| "This batch is expired" | Exactly that | `INVENT-0106` | Pick a different batch; expired stock cannot transact |
| "Storage location/bin is required" | Stock with no place | `INVENT-0107` | Put it away first |
| "Selected barcode is not available" / "already consumed" | Scanning something already used | `INVENT-0102`, `INVENT-0103` | Scan the right unit |
| "Adjustment reason is required" | Posting a count without an explanation | `INVENT-0109` | Enter the reason - it is the only record of why |
| "Adjustment exceeds tolerance" | A large variance | `INVENT-0110`, `INVENT-0185` | Submit for approval rather than splitting it up |
| "Source and destination cannot be the same" | Transfer to itself | `STOCKT-0111` | Pick a different destination |
| "Stock transfer cannot be closed until receipt is completed" | The far end never confirmed | `STOCKT-0113` | Confirm arrival at the receiving location |

## Buying and receiving

| What you saw | Usually | Codes | Do this |
|---|---|---|---|
| "PO cannot be released until approval is completed" | Still pending | `PURCHA-0082` | Someone else approves it |
| "PO amount exceeds your approval limit" | Above your threshold | `PURCHA-0083` | Send to higher approval |
| "This PO is closed" | Already completed | `PURCHA-0084` | Raise a new PO |
| "Please enter amendment reason" | Changing an approved PO | `PURCHA-0085` | Write the reason; the amendment then needs approval again |
| "Received quantity cannot exceed open PO quantity" | Over-receipt | `PURCHA-0087` | Correct the quantity, or get the excess approved |
| "This item is not part of the selected PO" | Wrong PO, or an unordered item | `PURCHA-0088` | Verify the item against the order |
| "GRN cannot be cancelled" | Downstream documents exist | `GRN-0253` | Reverse rather than cancel |
| "Stock is under QC hold" | Received but not released | `GRN-0255` | Complete the QC decision |
| Stock did not go up after receiving | The receipt was never posted | - | **Post Receipt** is the step that creates stock |

## Orders

| What you saw | Usually | Codes | Do this |
|---|---|---|---|
| Manual order refused | Not enough stock to reserve | `INVENT-0101` | Receive the stock. A manual order goes through the same engine a channel order does, on purpose |
| "Order cannot be confirmed because stock is not allocated" | Allocation has not run or failed | `ORDERM-0135` | Check the **Allocation pending** tile on Order Management |
| "Order is already dispatched" | Too late to change it | `ORDERM-0136` | The goods have physically moved; use a return |
| Order stuck On Hold | A hold rule fired | - | Open it, read the reason, fix the cause, **Release hold** |
| Channel order missing | It never arrived | - | Check the **Source** column for the channel's own order id. If it is not there, it did not sync |

## Integrations and channels

| What you saw | Usually | Codes | Do this |
|---|---|---|---|
| "Channel credentials are missing" | Never configured, or cleared | `CONN-0224` | Administrator enters them |
| "Channel is rate limiting requests" | Publishing too fast | `CONN-0225` | It retries automatically; reduce volume if it persists |
| "Channel field mapping is missing" | A required field is unmapped | `CONN-0227` | Complete the mapping - it names the field |
| "SKU already exists on the selected channel" | Listing collision | `CONN-0228` | Map to the existing listing, or change the SKU |
| "Integration event is pending longer than expected" | A stuck outbox event | `INT-0218` | Administrator reviews the queue |
| "Integration retry limit reached" | Repeated failure | `INT-0219` | Fix the root cause before retrying, or the same failure repeats |
| "This request was already processed" | An idempotency key replay | `INT-0222` | No action needed - this is the duplicate protection working |
| "Webhook verification failed" | Wrong secret, or a forged request | `INT-0220` | Check the secret with the source platform |

## Reports and files

| What you saw | Usually | Codes | Do this |
|---|---|---|---|
| "From Date cannot be later than To Date" | Reversed range | `GLOBAL-0012` | Swap them |
| "The selected date is outside the allowed posting period" | A closed period | `GLOBAL-0013` | Post in an open period, or ask for the period to be opened |
| "The file size exceeds the allowed limit" | Attachment too big | `GLOBAL-0007` | Upload something smaller |
| "This file type is not supported" | Wrong format | `GLOBAL-0008` | Bulk imports take CSV |
| A large report times out | Too much data for one request | - | Use **Export in Background** |

## When none of the above fits

1. **Read the detail line again.** It names the specific thing.
2. **Search the code** in the Knowledge Center.
3. **Check the HTTP status** in the [error code
   reference](error-code-reference.md). A status of 500 or above is an internal
   failure and nothing you did.
4. **Quote the correlation id** when you ask for help. Every error dialog shows
   one, and it identifies that single request in the server's logs - the
   difference between "something failed yesterday" and a line an administrator
   can read.
