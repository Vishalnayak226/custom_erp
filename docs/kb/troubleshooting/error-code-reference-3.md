---
title: Error code reference (part 3)
section: Troubleshooting
order: 22
summary: Error codes for PIM / Product Publishing to Tax / GST / E-Invoice, continued from the error code reference.
audience: everyone
last_verified: 2026-09-10
owner: documentation-maintainer
status: active
topic_type: reference
module: platform
task: Error code reference (part 3)
prerequisites: Authorized access to the relevant ERP task
applies_to: source registries; not release acceptance
---

<!-- GENERATED ARTICLE - DO NOT EDIT BY HAND.
     Regenerate: pwsh docs/update-docs.ps1 -Group Content -->

# Error code reference (part 3)

This page continues the [error code reference](error-code-reference.md), which
lists every area and explains how to read a code. It holds **319** codes, from
PIM / Product Publishing to Tax / GST / E-Invoice.

## Contents

- [PIM / Product Publishing](#pim-product-publishing) - 8 codes
- [POS / Billing](#pos-billing) - 42 codes
- [POS / Offline & Cash Drawer](#pos-offline-cash-drawer) - 8 codes
- [Patch / Bug Governance](#patch-bug-governance) - 3 codes
- [Payments](#payments) - 7 codes
- [Planning](#planning) - 1 codes
- [Procurement](#procurement) - 1 codes
- [Procurement / RFQ](#procurement-rfq) - 6 codes
- [Purchase Order](#purchase-order) - 82 codes
- [Purchase Requisition](#purchase-requisition) - 6 codes
- [Purchase Return](#purchase-return) - 1 codes
- [Purchase Return / RTV](#purchase-return-rtv) - 5 codes
- [Quality](#quality) - 6 codes
- [Quality & Maintenance](#quality-maintenance) - 12 codes
- [RFQ](#rfq) - 6 codes
- [Reports](#reports) - 3 codes
- [Reports & Exports](#reports-exports) - 8 codes
- [SaaS / Multi-Tenant](#saas-multi-tenant) - 8 codes
- [Sales / POS](#sales-pos) - 40 codes
- [Sales Return](#sales-return) - 9 codes
- [Security](#security) - 9 codes
- [Security / AI assistant access](#security-ai-assistant-access) - 10 codes
- [Security / Rate Limit](#security-rate-limit) - 6 codes
- [Stock Transfer](#stock-transfer) - 17 codes
- [Tax / GST / E-Invoice](#tax-gst-e-invoice) - 15 codes

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
| `PURCHA-6100` | Purchase order names a contract that is not Active, is for another vendor or store, or is outside its dates | This contract can't be used for this order. | Pick an Active contract for the same vendor and store, and date the order inside its validity. | Medium | 422 |
| `PURCHA-6101` | Purchase order line not on the contract, or above its rate | A line is not on the contract or is above its rate. | Use only items on the contract, at the contract rate. | Medium | 422 |
| `PURCHA-6102` | Release would pass the contract's value or quantity cap | This release is over the contract cap. | Reduce the quantity to what is left on the contract, or ask for the cap to be raised on a new contract. | Medium | 422 |
| `PURCHA-6103` | Contract saved with bad terms, or changed after it went Active | This contract can't be saved. | Fix the dates, cap and lines; an Active contract keeps its terms - close it and raise a new one. | Medium | 422 |
| `PURCHA-6104` | Contract activated or closed from the wrong status or by a role that cannot approve its value | This contract can't be activated or closed. | Ask the person who approves purchase orders of this value, and check the contract's status. | Medium | 422 |
| `PURCHA-6120` | Goods receipt against a service order, or a purchase order's type changed after approval | A service order is not received as goods. | Record the work on a service entry sheet. Raise a new order to change its type. | Medium | 422 |
| `PURCHA-6121` | Service entry sheet with no lines, an item not on the order, or more than is still open; or an accepted sheet edited | This service entry sheet can't be saved. | Use items on the approved service order and no more than is still open. | Medium | 422 |
| `PURCHA-6122` | Service entry sheet accepted twice, past the open quantity, or by a role that cannot approve its value | This sheet can't be accepted. | Ask the person who approves purchase orders of this value; check the sheet is Draft and within the order. | Medium | 422 |
| `PURCHA-6123` | Bill for a service order without a service entry sheet | A service bill needs its service entry sheet. | Pick the accepted service entry sheet on the bill, then match it. | Medium | 422 |

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

## Quality

| Code | When it happens | What you see | What to do | Severity | HTTP |
|---|---|---|---|---|---|
| `QUALIT-1760` | AQL not one of the table values | Pick an AQL from the list. | Choose one of the AQL values on the inspection plan. | Medium | 422 |
| `QUALIT-1761` | Lot size missing or not a whole number for AQL sampling | Enter the lot size as a whole number. | Type the number of units in the lot. | Medium | 422 |
| `QUALIT-1762` | Inspection against an AQL plan with no defects count | Enter the defects found. | Count the defects in the sample and enter the number. | Medium | 422 |
| `QUALIT-1763` | SCAR missing vendor, issue or reply-by date, or its vendor or non-conformance not found | The SCAR is not complete. | Give the vendor, the issue and the reply-by date. | Medium | 422 |
| `QUALIT-1764` | SCAR opened for a non-conformance that already has an open SCAR | That non-conformance already has an open SCAR. | Close the open SCAR first. | Medium | 422 |
| `QUALIT-1765` | SCAR step out of order, with no text, or on an unknown SCAR | That SCAR step cannot be recorded. | Record root cause, then action, then verification. | Medium | 422 |

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

