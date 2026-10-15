---
doc_id: DOC-SOP-VIDEO-SCRIPTS-20261005
title: ERP SOP video voice and on-screen scripts
type: reference
status: draft
owner: product-owner
approvers: [operations-owner, finance-owner]
audience: [trainers, operators, administrators]
applies_to: 2026-10-05 modular training edition
authority: proposed-policy
confidentiality: internal
last_verified: 2026-10-10
review_by: 2026-11-05
supersedes: none
superseded_by: none
---

# Human narration and caption script

Use a normal speaking pace. Leave a beat after each save so a viewer can read
the generated number. These are prompts for a human narrator, not text-to-speech
instructions. The first edition is intentionally silent with readable captions;
record optional human narration as a separate per-segment audio file in a video
editor when an approved speaker is available.

## Department setup

“Before anyone raises a request, the responsible department needs to exist.
Open Department under Setup. Create a clear, recognizable name, save it, and
check it appears in the list. The system assigns its code; don't invent one.”

## Vendor setup

“Before a purchase order—or a receipt against that order—confirm the supplier.
Open Vendor under Setup. Enter the legal/trading name and the business fields
your organization requires. Save and find the record again. This training
supplier is synthetic; a real supplier's tax and bank details need approval.”

## Item setup

“Confirm the item before ordering or receiving it. Use the name people recognize,
then check the barcode, HSN/tax setup, traceability and cost against approved
master data. This is training data, not tax advice. Save and read it back.”

## Location check

“Every PO and receipt needs the correct destination. Find the receiving
warehouse by name and confirm it is active and linked to the right entity.
If it is missing, an administrator must set it up before the transaction.”

## Requisition create

“Now raise the request. Describe what is needed in ordinary words, enter the
quantity and estimate, and select the responsible department. Save as Draft.
Read back the generated requisition number and the quantity before handing it
to the buyer.”

## PO create

“Choose the supplier, billing location and delivery warehouse by their names.
Add the item, quantity and agreed rate. Check the displayed totals, then create
the Draft PO. The system gives it a number. Draft is not permission to receive.”

## PO approval

“The buyer submits the PO. A different authorized approver opens Approvals,
checks the supplier, destination, lines and amount, and records a decision.
The buyer must not approve their own order.”

## GRN post

“Start from the approved PO. Load its remaining lines, check the physical
quantity and condition, then post the receipt. Read back the GRN number and
check the stock view at the receiving warehouse. A partial or rejected receipt
needs its own exception lesson; do not silently treat damaged stock as saleable.”

## Role setup

“Access is given by role, not person by person. Create a role named for the job,
tick only the module that job needs, and save. Everyone given this role sees that
module and nothing else.”

## User setup

“Now the person. Enter their username and name, choose the role you just made,
and save. They sign in and see only their module. Give a new password through the
reset flow, never by sharing one.”

## Bin and zone setup

“A bin is a numbered shelf position inside a warehouse. Create it by name and
code at its warehouse. If its zone does not exist yet, create the zone from the
same form — you do not have to leave and come back.”

## Request for quotation

“Before ordering, ask suppliers for prices. Describe what you need, invite the
vendor, record each quote as it comes in, then choose the winning price. The
choice is recorded with the RFQ, so the order that follows can be traced to it.”

## Stock transfer

“Stock moves from the warehouse to the shop on a transfer order. Choose both
locations by name, add the item and quantity, and send it. The shop receives it
on the same order — until then the stock is in transit, not lost.”

## Open the till

“Choose the shop by name, then count the opening cash — the float — and open
the till. No sale can be rung up until the till is open.”

## Cash sale

“Search the item by name or scan it. Check the quantity and price in the cart,
take payment, and complete the sale. The receipt shows exactly what was charged
and collected.”

## Close the till

“At the end of the shift, count the cash in the drawer and enter it. The system
compares it with what it expects and shows any difference. Close the till; the
day's summary is saved for the store manager.”

## Supplier bill

“Record the supplier's bill against the order and receipt it relates to, then
match it. If the quantity or price disagrees with what was received, the match
holds the bill and shows why. A matched bill is paid, and the GST on it becomes
input credit.”

## Phone order

“A customer orders by phone. Enter them by name, add the items, choose cash on
delivery or prepaid, and save. The order gets a number the customer can quote.”

## Release to the warehouse

“Check the order and release it. Only a released order reaches the warehouse's
queue; until then it can still be changed.”

## Pick, pack and dispatch

“The warehouse picks the items, packs them and dispatches. Stock leaves the
books at dispatch, and the order shows as shipped. A short pick ships what was
found and the rest goes back to be reserved.”

## Post the order's invoice

“The invoice bills what was actually shipped, with its GST. Post it, then record
the payment against it so the customer's balance is cleared.”

## Raise a journal voucher

“For an adjustment no other screen makes — a month-end accrual — raise a journal
voucher. Enter the narration, then each debit and credit line by account name.
Debits must equal credits. Save as Draft and submit it for approval.”

## Approve a journal voucher

“A controller, not the person who raised it, checks the lines and approves.
Only then is it posted to the accounts. A posted voucher is corrected by
reversing it, never by editing it.”

## Customer setup

“Confirm the customer exists before billing or refunding them. Enter the name,
save, and find it in the list. The code is generated.”

## Debit note

“When a supplier owes you money back — a rebate, a shortfall — raise a debit
note against them and the order it relates to. Save it as Draft, then post it.
Posting reduces what you owe that supplier.”

## Credit note

“When you owe a customer money back, raise a credit note for them by name, with
the reason. Post it. A posted note cannot be edited; a mistake is put right with
a new note.”

## Second bin

“Add a second bin in the same warehouse — the place stock will move to.”

## Putaway

“Received stock waits in the receiving area until it is put away. Choose the
item and the bin it goes to, and confirm. The bin now holds it.”

## Bin-to-bin move

“To re-slot stock inside one warehouse, choose the bin it is in, the item and
quantity, and the bin it goes to. The move is refused, with the reason, if the
destination cannot take it. Every move is in the stock ledger with both bins.”

## Product group

“Group the products a promotion will cover. Name the group and add the items
by name. One group can be used by many offers.”

## Coupon offer

“Create the offer: what it gives — ten percent — which product group it applies
to, the coupon code customers will quote, and the dates it runs. Save it active.”

## Coupon at the till

“At the till, ring up an item from the group and enter the customer's coupon.
The discount appears as its own line, and the receipt shows it.”

## Purchase return

“Choose the goods receipt the damaged goods arrived on. The lines show what can
still be returned — never more than arrived. Enter the quantity and reason, save,
then post. Stock leaves and a debit note is raised against the supplier
automatically.”

## Add an employee

“Enter the employee's name, department, location and joining date, and save.
They can now have a salary, leave and attendance.”

## Salary structure

“Give the employee their salary structure: the earnings and deductions each
month. Payroll reads it; nobody types salaries month by month.”

## Leave

“The employee applies for leave with the dates and type. Their manager approves
it in Approvals. Approved leave is counted in payroll.”

## Attendance

“Mark the day's attendance for the employee. Payroll uses it with leave to work
out the days paid.”

## Payroll

“Run payroll for the month and location. Check each payslip, then post. Posting
books the salary cost and what is owed to staff in the accounts.”

## Expense claim

“An employee claims back money they spent. Enter who, where, the date, the
category, the amount and the GST on the bill, and the purpose. Save and submit
it; it goes to that location's manager.”

## Approve an expense claim

“The manager checks who claimed, what for and how much, then approves. The claim
moves on to finance.”

## Pay an expense claim

“Finance checks the claim against the receipt and verifies it, then marks it
paid when the money is sent. The expense and any GST credit are posted.”

## Register a fixed asset

“Record equipment the business will use for years: its category, cost, useful
life, location and the person responsible. Save, then capitalise it. Its cost
moves to fixed assets and depreciation starts.”

## Transfer an asset

“When the asset moves, transfer it: choose the new location by name and who is
responsible now, then confirm. The register shows where it is.”

## Finished product

“Set up the product you will make, like any other item — name, unit, tax. It
needs its own record before it can have a bill of materials.”

## Bill of materials

“List what one unit is made from: each raw material and how much. Save it. Every
production order of this product uses it.”

## Production order

“Order a quantity to be made. Issue the raw materials to it, then receive the
finished goods. Raw material stock goes down and finished stock goes up.”

## Quality inspection

“Inspect the batch: the inspection number, what was checked, how many passed
and failed. Save and submit it for approval.”

## Approve an inspection

“A store manager approves the result in Approvals. Only an approved inspection
counts.”

## Product family

“A family is a kind of product that shares the same information — gift hampers,
say. Create it by name.”

## Attribute

“Define a piece of information products carry — pack size — with a clear label.”

## Required attribute

“Link the attribute to the family and make it mandatory for completeness. Every
product in the family must now have it.”

## Product in its family

“Edit the product and choose its family. From now on it is scored against what
that family requires.”

## Workbench

“Open the Workbench. The product shows its completeness and what is missing.
Fill each missing value, then the selling content — title, short and long
description. Submit it for approval; only approved content counts as complete.”

## Service contract

“Record the customer's maintenance contract: the customer, the asset it covers,
the dates and the number of visits included. The contract counts the visits used
as tickets close.”

## Log a service ticket

“When the customer calls, log a ticket: number, customer, asset, what is wrong,
priority and the dates you must respond and resolve by. Link the contract. Save,
then assign it to the technician by username.”

## Work a ticket to closure

“Each step is one button on the ticket's row. Start when work begins. Resolve,
saying what was done — a ticket cannot be resolved without a note. Close it once
the customer confirms; one visit is used from the contract.”

## Customer date of birth

“A birthday campaign can only greet customers whose date of birth is on file.
Edit the customer and add it. Only the day and month are used.”

## Birthday campaign

“Name the campaign, choose the Birthday trigger, and write the message — the
customer-name placeholder is replaced with each customer's name. Enter what it
costs and save it active. Every hour the system finds today's birthdays and
sends the message once.”

## Campaign return

“In Reports, open the Report Catalog and run Campaign ROI. It shows how many
customers the campaign reached, what they spent since, the cost, and the return
on it.”

## GST position

“Before filing, open Reports and the GST Return Summary for the period. It shows
GST collected on sales, GST paid on purchases that can be claimed back, and the
net to pay. A negative net means credit to carry forward.”

## Catalog report and export

“Every other report is in the Report Catalog. Choose one — the Stock Ledger, for
example — and run it. Export in Background builds the file while you work;
download the CSV when it is ready.”

## Label printer

“Record the label printer once: a recognisable name, where it sits, Active, used
for Stickers, and its language, DPI and label size. Until silent printing is set
up, labels open in the normal print dialog.”

## Labels for a goods receipt

“Print labels from the receipt itself. Choose Goods Receipt, pick the document,
and its lines load — one row per item, copies equal to the accepted quantity.
Choose the printer and Print Selected. The run is logged against the receipt.”
