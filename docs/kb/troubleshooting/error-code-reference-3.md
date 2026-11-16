---
title: Error code reference (part 3)
section: Troubleshooting
order: 22
summary: Error codes for PIM to SaaS / Multi-Tenant, continued from the error code reference.
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
lists every area and explains how to read a code. It holds **279** codes, from
PIM to SaaS / Multi-Tenant.

## Contents

- [PIM](#pim) - 57 codes
- [PIM / Product Publishing](#pim-product-publishing) - 8 codes
- [POS / Billing](#pos-billing) - 49 codes
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
- [Quality](#quality) - 8 codes
- [Quality & Maintenance](#quality-maintenance) - 12 codes
- [RFQ](#rfq) - 6 codes
- [Reports](#reports) - 3 codes
- [Reports & Exports](#reports-exports) - 8 codes
- [SaaS / Multi-Tenant](#saas-multi-tenant) - 8 codes

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
| `PIM-9362` | Saving a declaration pack with an unknown pack, no reviewer or evidence, or a last day before the effective date | This declaration pack can't be saved. | Pick a shipped pack, name the qualified reviewer and the evidence, and a last day on or after the effective date. | Medium | 422 |
| `PIM-9363` | Saving an item or item name with an item alias longer than 14 characters | The item alias can be 14 characters at most. | Shorten it, or clear it to have it made from the name. | Low | 422 |

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
| `POSOFF-0878` | Saving a store or tender with a UPI ID that is not name@provider, or a UPI ID on a non-UPI tender | This UPI ID can't be used. | Type the UPI ID as name@provider (for example store@ybl), or leave it blank. Only a UPI tender takes one. | Medium | 422 |
| `POSOFF-0879` | B2B bill without the buyer's legal name, address, city or a 6-digit PIN | This B2B bill needs the buyer's name and address. | Fill the buyer's legal name, address, city and PIN in the B2B panel, or clear the GSTIN for a B2C bill. | Medium | 422 |
| `POSOFF-0880` | B2B bill without the buyer's PO number or date, or a PO date after today | This B2B bill needs the buyer's PO number and date. | Type the buyer's PO number and date (not after today). An admin can make the PO optional in Settings -> Point of Sale. | Medium | 422 |
| `POSOFF-0881` | B2B bill marked Shipped without a full ship-to address | Shipped goods need the ship-to address. | Fill the ship-to address, city, PIN and state, or choose Collected if the buyer takes the goods at the counter. | Medium | 422 |
| `POSOFF-0882` | Offer saved without what its type needs, or with days, times, slabs or limits that do not read | This offer is not complete. | Fill the field the message names. Slabs read 2000=200; 5000=10%, days Mon-Fri or Weekend, times 18:30. | Medium | 422 |
| `POSOFF-0883` | Sales History or the till's bill search given a date, kind or source that does not read | This search could not be read. | Pick the dates from the calendar and search again. | Low | 422 |
| `POSOFF-1301` | Bill cannot be cancelled (not found, not today's, partly returned, nothing left, or no reason) | This bill can't be cancelled. | Only today's bill, with nothing returned yet, can be cancelled - give the reason. For an older bill, or one already partly returned, take a return instead. | Medium | 422 |
| `POSOFF-1303` | Item not yet released (Draft, Pending Approval or Blocked) scanned at the till | This item can't be sold yet. | It is waiting for its release approval - ask a manager to approve it under Approvals, or sell another item. | Medium | 422 |
| `POSOFF-1304` | Item listed under the store's Items not sold here | This store does not sell this item. | The store's record lists it under Items not sold here - sell it from another store, or ask an administrator to change the list. | Medium | 422 |
| `POSOFF-1309` | Store credit cannot pay (no customer, none or not enough, more than due, above the manager limit, daily uses reached) | Store credit can't pay this. | Store credit is the bill's own customer's: take their number, use no more than they have and the bill needs. Above the limit a store manager switches in. | Medium | 422 |
| `POSOFF-1361` | Offline sale validated with a line not scanned, or scanned with a code that is not that item's barcode | The scanned barcodes don't match. | Every line must have been scanned with one of the item's own barcodes. Check the goods with the cashier; if it can't be put right, reject the sale with the reason. | High | 422 |
| `POSOFF-1362` | Offline sale cannot be decided (already validated or rejected, rung up by the person deciding, or a rejection with no reason) | This offline sale can't be decided. | Only a Pending sale is validated or rejected, by a manager other than the cashier who rang it up; a rejection needs the reason. | Medium | 422 |
| `POSOFF-1363` | Duplicate tender - the same card / UPI / cheque reference and amount was taken at this store minutes ago | This payment was already taken on an earlier bill. | Check the earlier bill. If the customer really paid twice with the same reference, mark the tender as a second payment and try again. | High | 409 |
| `POSOFF-1371` | Till series refused (not a store, all 99 tills taken) or an offline bill number refused at sync (not this store's till series, malformed, or already on a bill) | This offline bill number can't be used. | Check the number on the customer's receipt against the till that rang it up. If it is on another bill already, reject the sale with the reason and re-bill it online; a till gets its series when its screen opens online. | High | 422 |
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
| `QUALIT-1766` | Instrument calibration interval missing or out of range | This instrument cannot be saved. | Give a calibration interval of 1 to 3650 days. | Medium | 422 |
| `QUALIT-1767` | Calibration record refused (unknown or retired instrument, date missing or in the future, result not Pass or Fail, or a recorded calibration edited) | This calibration cannot be saved. | Pick an instrument in use, the date it was calibrated (not later than today) and Pass or Fail. A recorded calibration cannot be changed. | Medium | 422 |

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

