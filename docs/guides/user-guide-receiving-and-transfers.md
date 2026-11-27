---
doc_id: DOC-78F8FF850A-04
title: User Guide: Receiving goods and moving stock
type: procedure
status: draft
owner: documentation-maintainer
approvers: [documentation-maintainer, documentation-maintainer]
audience: [maintainers, documentation-maintainer]
applies_to: source documentation; scoped release acceptance required
authority: transition-copy
confidentiality: internal
last_verified: 2026-09-09
review_by: 2026-10-09
supersedes: none
superseded_by: none
verification_scope: metadata and lifecycle classification; domain acceptance pending
---

# User Guide: Receiving goods and moving stock

Part 4 of 9 of the [User Guide](user-guide.md). Previous: [Purchase orders](user-guide-purchase-orders.md). Next: [Labels and printing documents](user-guide-labels-and-printing.md).

*Continues [6. Ordering More Stock (Purchase Order)](user-guide-purchase-orders.md#6-ordering-more-stock-purchase-order).*

### 6.10 Receiving goods in steps: receive, verify, price check, MRP, pay

A goods receipt (GRN) is worked in five steps, each on its own page with one main button and a **Back**. Nothing enters stock until step 4, so a mistake at any earlier step is fixed by going back and changing it. Each step can be done by a different person (your administrator decides who - Admin Guide §B.3.6.1); in a small shop one person does them all.

**① Receive - what arrived and what the invoice says**

1. **Procurement → Goods Receipt**, click **+ New Goods Receipt** (or **Receive goods** on an approved PO's row, which opens it with that PO already loaded).
2. Pick the **Vendor** first. Their approved POs with anything still open appear underneath, each with its date, items, open quantity and value. Tick the ones this delivery is for and click **Load ticked POs**. You can still type a PO number and click **Add PO & Load Items**, or **Load Items from ASN**. Only one vendor's POs go on one receipt; each PO shows as a chip (× removes it with its lines). Once a PO is on the receipt the vendor is **fixed**, even if you remove that PO again, until **Reset** in the top bar. Each line shows its number (#), **Product**, **Design**, **Combination ID** and its **Colour / Size...** values; a PO's lines show the same columns.
3. On each line, type what really arrived in **Received** - it starts at the PO's open quantity. Scanning an item that is already on the receipt (into the **SKU** box, with no quantity typed) counts one more unit, with a short beep.
   Goods set aside at the door go in **Rejected** or **Damaged** on the line; a **Reason** box opens under the number and must be filled before saving. The grey columns are worked out for you: **Accepted** (received minus rejected and damaged), **Short** (still owed on the PO) and **Excess** (more than the PO asked for - keep or return is decided at Verify).
4. If the bill differs from what arrived, change **Invoice Qty** and **Invoice Rate** (the price per unit on the bill - **incl. GST** when the PO's prices include GST, as the column heading says, otherwise before GST; §6.2). They start at the received quantity and the PO price, so only lines where the bill differs need touching. **Value** is invoice rate × invoice qty, so the lines add up to **Invoice value** in the bar at the top. When the invoice bills a different quantity from what you accepted, the line shows the gap (*+10 vs accepted*) and a warning appears above the lines.
5. Fill in the vendor's **Invoice No**, **Invoice Date** and **Invoice Total (incl. GST)** - all three are required. The same invoice number of the same vendor cannot be received twice.
6. Click **Save receipt**. The receipt is saved as *Received* - **no stock moves** - and its step page opens.

If your administrator turned on **blind count**, the receiving page does not show the PO quantity: you count what is there.

On every step page each line shows the product's **name**, then its design, Combination ID and batch, then its Colour / Size... values; the vendor (and, on Price check, the vendor of the last purchase) is shown by name.

**② Verify - what is really in hand, at what price**

7. For each line, **GRN Qty** (what is really in hand) starts at the received quantity and **GRN Price** at the invoice price. Change them after checking the goods and calling the vendor. Each line shows its difference from the invoice and from the PO.
8. Enter **Rejected** and **Damaged** quantities and choose the reason from the list (an optional note goes beside it); they go back to the vendor on a Purchase Return (§6.5). Attach the vendor's invoice copy under **Vendor's invoice copy** at the bottom of the receipt while it is being worked.
9. Goods **beyond the PO** (more than ordered, or an item not on the PO): choose **Keep** (it goes for approval - say why in the box underneath) or **Return to vendor** (added to rejected). A small excess within the vendor's tolerance is accepted without asking.
10. The bar at the top compares **Invoice total**, the **invoice lines** and **Received and verified** (GRN qty × GRN price + GST), and tells you the note the difference will raise: a **Debit note** when the vendor billed more, a **Credit note** when they billed less.
11. Click **Verify**. If the bill's total and the items on it do not add up (more than ₹1 apart), a yellow box **The bill does not add up** says by how much and which box is probably wrong - the total you typed, or one line's invoice qty or price. Verify takes you back to **Receive** with those boxes highlighted (or click **Fix it on Receive**); correct them against the vendor's bill and click **Save and verify again**. Verify goes ahead once they match.

**Post all.** When everything matches - received = invoiced = PO quantity, invoice price = PO price, every price the same as last time and every item already has an MRP - a **Post all** button runs steps 2-4 in one go. When a receipt posts, the message says how many units are available to sell and how many rejected or damaged units were set aside, so it matches Stock on Hand.

**③ Price check - against the last purchase price**

12. Each line shows its **previous price** (the last time this item was received, from any vendor, with the vendor and date) and the change in %. Click **Check prices**. The lines split into up to three **child GRNs**:
    - **/A New item** - never bought before;
    - **/B Same price** - same as last time;
    - **/C Price changed** - higher (▲) or lower (▼).
13. Rename a child if you like, or move an item to another child with its drop-down; **Check prices again** puts everything back where the prices say. Click **Save and go to MRP**.

**④ MRP and posting - one child at a time**

14. Each child is a card. Every line shows its cost, the **current MRP** (filled in when the item has one), a **suggested MRP** (from the item's product type's **MRP rule**, or the default rule, named under the figure; with no rule, cost plus your markup and GST - always rounded to a price ending such as ₹x99) and the margin. An MRP typed on the PO is already in the **MRP** box - keep it or change it. Type the **MRP** or click **Use suggested**. An MRP below cost shows in red, below the current MRP in amber.
15. Click **Save and post** on a child. That child's stock goes in **at the GRN price**, its batch carries this MRP, barcodes are issued, and the accounts are posted. Post the *Same price* child first and those goods are on the shelf today while you decide the new prices.
16. **Print stickers** appears on a posted child: it opens Sticker Printing with this receipt loaded and only that child's lines ticked. Each sticker prints the MRP of its own lot. Once the whole receipt is posted it opens on **Pay**; **MRP and post** in the steps still opens (read-only) for each child's **Print stickers** and **Put away**, and **Print stickers** beside **Print** at the top labels every posted line at once.
17. When the last child posts, the receipt is **Posted**. The debit or credit note (invoice total against what you received and verified) is raised, and the **vendor bill** is made from what you typed at Receive - nothing to key in again.

A child holding goods **kept beyond the PO** waits until the approver decides it (**My work → Approvals**). If it is rejected, the excess becomes *Return to vendor*. Post the other children meanwhile.

**⑤ Pay (optional)**

18. **Due** is the invoice total less a debit note, plus a credit note, less what is already paid. Enter the **Amount** (part payments are fine), **Paid by** (Bank, UPI, Cheque or Cash), the **UTR / cheque no** (for cash any reference, or leave it blank) and the **date**, then **Pay**. Tick **Withhold TDS** when the vendor has a TDS section and you are paying the whole bill. What the receipt pays in all is exactly what you received and verified, to the paisa: the bill is not rounded to the rupee, and the bill and the receipt record each payment together or not at all.
19. Or click **Leave it in Payables** - the bill waits in Finance → Payables and payment proposals as usual. The receipt shows *Due*, *Part paid* or *Paid*.

**Correcting a posted receipt.** On step 5, **Correct** lets you change a line's GRN qty, GRN price or MRP. **Show the effect** lists what it will do (stock, value and the note it raises) before anything changes; **Apply the correction** asks why and posts only the difference. It refuses only what cannot be undone: stock that is already sold (raise a Purchase Return for what is left), or a bill already paid beyond the new amount.

**The list.** Goods Receipt opens on tabs - **To verify**, **To price**, **Ready to pay**, **Done** - each with its count and the age of its oldest receipt. A bill for goods that arrived before their PO is not counted in **Ready to pay**; it waits on its own **Waiting for PO** tab (shown only when one waits) until the PO is raised. **Open** takes you to that receipt's current step. Every change at every step is kept (**Changes** at the bottom of a receipt).

**Reports** (Reports → Procurement): *GRN Register - every line, every step*, *Invoice vs GRN Differences*, *Purchase Price Changes*, *Goods Receipts Waiting*, *GRN Corrections Log*, *PO vs GRN Price Variance*, *GRN Payments*; and under Inventory, *Goods In Receiving* (arrived, not yet posted - not sellable).

**What it means for the money.** Stock is valued at the **GRN price**. The vendor bill is the vendor's invoice total; the note brings what you pay to what you received and verified. Receipts posted before this way of working keep the earlier basis: stock at the invoice price, with a note for the difference from the PO price. A bill that differs from the invoice keyed at **Receive** moves your stock cost by that difference (§6.15).

### 6.11 Adding photos to designs and Combination IDs (after the PO)

Once a PO is saved, add the product photos for what it orders: images for each **design** and for each **Combination ID** (its colour / size version).

1. On **Purchase Orders**, click **Images** on the PO's row. It is there on every PO with lines except a cancelled one — draft, approved, sent or closed.
2. The page lists each **design** on the PO with the **combinations** ordered under it, then any line whose item has no design. The chip at the top says where the photos are kept: **your image drive** (your business's own storage, set up by your administrator — Admin Guide §B.3.0) or **the ERP server**.
3. **A few photos:** click **+ Add images** under a design or a combination and choose one or more photos — JPG, PNG, WebP or GIF, up to 10 MB each.
4. **A folder of photos:** drag them onto the box at the top, or click **Choose files** there. Each file goes to the design or combination it is **named after**:
   - its number — `D00006.jpg` for a design, `I00042.jpg` for a combination; an older long number with each `/` written as `-` or `_` — `Design-HO-26-27-000006.jpg`, `Design_HO_26-27_000006-Red-S.jpg`;
   - or, for an older long number, only the part after the last `/` — `000006.jpg` for the design, `000006-Red-S.jpg` for its Red / S combination — as long as only one design on this PO ends that way;
   - anything after the number is allowed, so more photos of one combination can be `I00042-2.jpg` or `I00042 back.jpg`. Capitals, spaces, `-` and `_` make no difference.
5. The box under it lists what happened: each file and where it went, and in red any file that matched nothing, with the reason. Pick a design or combination in **Add it to…** next to that file to send it there — no file is dropped.
6. The **first** photo of a design or combination is its **main** photo (marked **Main**); the others are extra photos. **Make main** on another one swaps them — the old main stays as an extra. **Remove** takes a photo off (it stays in the image history; nothing is deleted). Click a photo to see it full size; click it or press **Esc** to close.
7. The same photos show on the **Design** (**Setup → Product Family**) and **Item** forms, in their **Images** section, where you can add, make main and remove them too. Those changes are saved straight away — there is no need to press **Save** on the form. PIM, publishing to sales channels, the item picture on printed documents and the Vendor PO in Excel use these same photos.

**Who can.** Anyone who can create purchase orders can add a PO's photos. On the Design and Item forms, anyone who can edit that record.

**Uploading the same photo twice** keeps it once. A photo that shows *This image could not be loaded* is on the image drive and the drive did not answer — tell your administrator (Admin Guide §B.3.0, **Image drive**).

### 6.12 Ordering and receiving from one Excel file (Upload from Excel)

Many purchase orders — and the goods receipts of the bills that came with them — can be made in one go from one Excel file, instead of typing each PO and each receipt.

1. On **Purchase Orders**, click **Upload from Excel**, then pick a template. Each one is made from your own records when you download it, so its drop-downs list your stores, vendors, product types, categories, HSN codes and attribute values:
   - **PO** — new designs and repeat ones; one row per combination.
   - **PO + GRN** — the same, for goods that have already arrived: every row also needs the **Vendor Invoice No** and **Invoice Date**.
   - **Reorder** — only what already exists: a **Combination ID** (or the **Design No** of a design with one combination) and a **Qty**.
   - **Reorder + GRN** — a reorder whose goods have already arrived.

   Open it in Excel; its **Start Here** sheet says how to fill it. Columns marked * are required.
2. **Vendors** sheet (PO templates): only a vendor that is not in Wholeops yet — its **Vendor Name** and **State** (from the list); City, GSTIN, phone and email if you have them. A vendor already in Wholeops is found by its GSTIN or name and left as it is. If a vendor or **Store** already in Wholeops has no state, its PO shows a Problem (*the vendor has no state*) - choose the state, then upload again.
3. **Purchase** sheet, one row per combination:
   - **PO Ref** — your name for the order; rows with the same PO Ref and vendor become **one PO**. **Vendor** and **Store** from the lists.
   - **PO** templates: **Product Type**, **Category** and **HSN Code** (pick, or type a new one - it is made for you; **GST %** only for a new HSN code), then a column for each attribute (Colour, Size…), and **Qty**, **Purchase Rate** (per piece, **including GST**) and **MRP**. A new attribute value is added to its attribute; a new attribute is a new column with its name as the header. **Design No** is optional: the same label on several rows makes them one design (`1, 1, 1`, `EI001`… only within this file). Wholeops makes each design and combination from the row's attributes - **a design with the same item name, vendor and attributes is used again, not made twice**. When an administrator has turned off **Bulk PO upload makes new designs**, a row that would need a new design or combination does not load.
   - **Reorder** templates: **Combination ID** or **Design No**, **Store** and **Qty**. **Vendor**, **Purchase Rate** and **MRP** left blank come from the design. A code that does not exist does not load (*does not exist*); nothing new is made.
   - **Photo** (PO templates, optional) — the file name of the row's photo (several: `a.jpg, b.jpg`); the photos are sent with the file (step 5).
4. **+ GRN** templates: **Vendor Invoice No** and **Invoice Date** on every row (the date as an Excel date or in your **Date order** - §3 "Dates in a file"), and **Received Qty** only when it differs from Qty. The rows of one vendor and invoice become **one goods receipt** at the PO's store. Then allocate it to stores from Transfers (§7.7, *From a goods receipt*). A file made from the older template, with its **Bills** sheet, still uploads.
5. Back on **Upload from Excel**: **Choose file**; if the file names photos, also **Choose photos** (or **Choose a photo folder**) — JPG, PNG, WebP or GIF, up to 10 MB each — then **Preview**. Nothing changes yet. A photo the file names but that was not chosen is marked ✗. The preview lists the vendors, the setup records it will make (categories, product types, item names, attribute values), each PO with its rows — *New design*, *New combination* or *Already there* — and each bill. Anything with a **Problem** is left out; fix it in the file and preview again.
6. Click **Make … POs and receive … bills** and confirm. Each PO is saved (GST included), sent for approval and approved as you where your approval rules allow you to approve it; then each bill is received, verified, price-checked and posted with the MRPs in the file — stock goes up and the barcodes are issued.
7. The result shows each PO's number and status and each bill's goods receipt. **Print stickers** on a received bill opens Sticker Printing with that receipt loaded.
8. Then the photos are added — keep the page open until **Photos** shows the count. Each photo goes to its row's **Combination ID**; the first photo of a design also goes to the **design** (the first one is the main photo). They are stored where the PO's **Images** page keeps them (your image drive, or the server — §6.11) — on the image drive each is renamed after its number (`D00010.jpg`, then `D00010-2.jpg`; Admin Guide, **Image drive**) — and show there, on the Design and Item forms and in PIM. Photos forgotten the first time: choose them and upload the **same file** again — the PO and bills are found, and only the photos are added (the same photo twice is kept once).

**Rows to fix.** A row with a problem does not load; the rest do. After the upload, **Rows to fix** (also on the upload in **Past uploads**) downloads those rows on the same sheets, each with its **Reason** and the same drop-downs. Correct them and upload that file. A row left out of a PO that was made comes back with a new **PO Ref** (`<ref>-2`), so it makes a second PO; a row that was ordered but not received keeps its PO Ref, so it is received on its PO.

**Uploading the same file again is safe.** A PO already made from it (same vendor and PO Ref) is found and not made again, and a bill already received is not received twice. So when goods arrive later, fill the bill into the same file and upload it again. New rows for a PO Ref that is already made are refused — give them a new PO Ref.

**Waiting.** A PO you may not approve waits in **Approvals**, and its bills wait with it: approve it, then upload the same file again. A bill that stopped at a step (for example *the bill's total and the items on it do not add up*) stays at that step: fix it in the file and upload again, or finish it on the goods receipt.

**Bigger bills.** One goods receipt holds about 4,000 lines; the preview says when a bill does not fit, and such a bill goes in as two goods receipts.

**Who can.** Anyone who can create purchase orders. Each part is checked as if it were done on its own screen: making vendors, designs and item names, approving, and receiving goods. The preview says when your role may not do one of them.

**Past uploads** lists every upload with its result; **Details** shows it again, **Download file** gives back the file that was uploaded and **Rows to fix** gives the rows that did not load.

### 6.13 Delivery dates, reminders and day limits

A PO can carry an **Expected delivery** date (beside the GST treatment box on the PO). Your administrator decides what the business does with it; each of these is off until they set it:

- **A reminder before the date** - a set number of days before Expected delivery, the *PO Delivery Reminder* goes out once (to the vendor or your team, by whatever channel the business set up).
- **Late or early receipts** - a goods receipt made well after (or before) the PO's Expected delivery is marked *Late by N days* / *Early by N days*; **Reports → Procurement → Late and early receipts** lists them.
- **How far back or ahead a date may go** - a vendor invoice date on a goods receipt or vendor bill, and a PO's Expected delivery, can be refused when outside the window (`PURCHA-1308`). An administrator can enter such a date.
- **Goods receipts not billed** - **Reports → Procurement → Goods receipts not billed** lists receipts posted that long ago with no vendor bill yet.
- **Requisitions and quotes** - a purchase requisition that never became a PO closes as *Expired* after the set days; a vendor quote with no *Valid until* is valid for the set days.
- **Transfers in transit** - a transfer dispatched that long ago and not received is flagged and listed in **Reports → Inventory → Transfers in transit too long**.
- **Input tax credit deadline** - a vendor invoice of one financial year entered after 30 November of the next is marked *time-barred* (its GST credit cannot be claimed) and listed in **Reports → Finance → Input credit past its deadline**.

### 6.14 A PO's commercial terms, and a receipt's challan and transport

**On the PO** (Stage 130.16): under the header, open **Commercial terms** and fill in the **Payment term** (a list - *Net 30 days*, *Advance before dispatch*... - kept under Setup as Payment Terms), the **Delivery terms** (Door delivery, Ex-works, FOB, CIF...), who pays the **Freight**, the **Vendor contact** and the date the vendor's **Quote** is **valid until**. They print on the PO, and the payment term sets the due date of the vendor bill made from its goods receipt (invoice date + the term's net days). If your company requires them (an administrator's setting), a PO cannot go for approval - or be approved - without the payment term, delivery terms, Expected delivery and freight, nor after its quote's date (`PURCHA-1316`); a draft never needs them. Once approved, the terms are kept as they were approved; changing them on an approved PO asks for the **amendment reason** and records what changed, before and after.

**Vendor hold, default terms and changes that wait (Stages 148.9, 148.10).** On a vendor, **Hold** can be *No new PO*, *No payment* or *Both*: a held vendor cannot be put on a new purchase order (`VENDOR-0190`; orders already approved can still be received) and a vendor held from payment cannot be paid by any route - Pay, part payment, a payment run (`VENDOR-0191`). The vendor's **Payment term** is used when the PO has none; a term can count from the **End of month** and carry an early-payment discount (**Discount days** and **Discount %**): the payment run takes the discount when it pays inside the window, off the goods value, and pays the rest. A vendor marked **MSME** is never due later than 45 days after the goods were accepted. If your administrator lists vendor fields for approval (bank account, IFSC, GSTIN, payment term, credit limit, group by default), changing one on an existing vendor saves the rest and files a **Vendor Change** with the old and new value; the vendor keeps its old value, payments to it pause (`VENDOR-0192`), and the change is applied when someone else approves it in **Approvals** (the same person only if self-approval is allowed). A new vendor is never held.

**Vendor advances and volume rebates (Stages 148.15, 148.17).** **Vendor Advances** (Procurement) pays a vendor before the bill: **Give advance** (vendor, optional purchase order, amount) posts to **1810 Vendor Advances** against the bank, and a vendor held from payment cannot be given one. Press **Apply** on the advance and pick from that vendor's Matched bills, or **Advance** on a Matched bill and pick from that vendor's open advances; an advance with a purchase order only lists bills on that order. The bill is paid from the advance (the part-paid history shows an *Advance* line), several bills can use one advance, and the rest stays open (`FIN-0922` if you ask for more). A refusal names the bill by the number on the vendor's invoice, the vendor by name and the advance by its amount and date - never by an internal id (`FIN-0923`, `FIN-0924`). **Refund** returns the open amount to the bank. **Cancel** shows only until the advance is used. Advances and rebate claims are made only by these buttons, not typed or imported (`FIN-0926`, `PURCHA-5025`). No TDS is withheld on an advance, and a bill that has an advance applied cannot then be paid with TDS; deduct TDS on the bill instead. **Volume rebates** appear only when your administrator has turned on *Volume rebates from vendors*. Put a target and up to two tiers on the vendor agreement (**Rebate on** Gross or Net), then **Vendor Rebate Claims > Calculate** for a period: one claim per vendor and period. **Send** it for approval; on approval the rebate is accrued (**1820**), and **Settle** raises the debit note and clears it. A re-run changes a Calculated claim, never an approved one.

**Memo stock (Stage 148.16).** When your administrator turns on *Memo stock* under Modules, **New goods receipt** has a **Memo stock** field: set it to *Yes* for goods a vendor leaves with you and you pay for only when they sell (the vendor is required, `PURCHA-5052`), and type the vendor's memo or challan number as the invoice number. The receipt adds the units to stock at no value: nothing posts to the accounts and the stock value and average cost leave them out. It raises no vendor bill, so once posted it goes to **Done**, not **Ready to pay**; **Pay** and **Correct** do not apply to it, and a bill or landed cost keyed against it is refused (`PURCHA-5055`); to change one, return the units and receive again. When a sale (till or invoice) takes those units, oldest first, one vendor bill per vendor is raised for what sold, at the vendor agreement's rate or else the rate on the receipt, and the cost of that sale is booked at that rate. When the vendor has a **GSTIN**, the bill splits that amount into the taxable value and input GST at the item's purchase GST rate (the memo rate is the price **before** GST, as on a Purchase Order): CGST and SGST when the vendor's state matches the store's, IGST otherwise, and the GST goes to the same input-credit account as any vendor bill, so GST reports include it; a vendor with no GSTIN is billed the plain amount as before (Stage 148.16c). Pay the bill as any vendor bill. Returning unsold memo units to the vendor raises no debit note; units already sold cannot be returned this way (`PURCHA-5051`), and a sale that took memo stock cannot be cancelled (`PURCHA-5053`) - take the goods back with a return. Memo units written off or short in a count never reach a vendor bill: the Memo stock report shows them as **Lost**, and you settle those with the vendor by hand. Items tracked by serial or batch cannot be received as memo stock (`PURCHA-5054`); receive them as ordinary stock. The **Memo stock** report (Procurement) lists each receipt with sold, returned and open units. With the module off nothing changes; memo stock still held keeps its handling.
**One invoice per customer per period, and drop-ship (Stages 148.31, 148.32; Wholesale module).** Both appear only when your administrator has turned **Wholesale** on. On a customer set **Bill to** (another customer who receives the balance and the invoice), **Summary invoice**, and optionally **Group by place of supply** and **Group by customer PO**. On **Sales Invoices** press **Summary invoices**, pick the period and press **Show groups**: each group is bill-to, currency, place of supply and customer PO, with the reason it is one invoice and how GST follows the ship-to (the supply is inter-state when the ship-to state differs from your store's). A group that cannot be one invoice (ship-to states differ without grouping by place of supply) is shown and left out, never split quietly. Choose the groups and press **Create invoices**: each posts like any invoice, against the bill-to's credit limit, and carries one delivery challan per ship-to address. On an order line, write instalments as qty@date, e.g. 40@2026-12-01. For drop-ship, mark the item **Drop-ship** and give it a default vendor. An order is all drop-ship items or none. Saving it raises a Draft purchase order to the vendor with the customer's address and its GST; no stock is reserved, picked or received. Changing a quantity, removing a line or moving the delivery date follows the Draft purchase order, GST included (Stage 148.52); once it is past Draft the change is refused (`SALESP-0717`) and the vendor is asked instead. On **Purchase Orders** press **Drop-ship shipments**, tick what the vendor shipped (or **Confirm all ready**): the line ships, the order invoice follows, and the cost posts as the vendor's bill, once per shipped line (Dr 5100 cost, Dr 1500 input GST, Cr 2100 the vendor's total). A line already shipped or cancelled is refused (`SALESP-0719`). Cancelling an order cancels its Draft purchase order; once that is past Draft the cancel is refused (`SALESP-0717`) until the purchase order is cancelled. A customer on a summary invoice is not billed per package (`SALESP-0713`); a summary invoice made again after a cancel takes the next id (INV-SUM-…-2). Cancelling an order inside a summary invoice: a Draft invoice is voided whole and its other orders can be invoiced again; a posted one gets a Draft credit note for that order's share, with its tax (Stage 148.54). A cancelled order's credit note is for the invoice's exact amount, paise included (Stage 148.58). A line whose purchase order is not approved yet cannot be confirmed (`PURCHA-5106`).

**Vendor advances and volume rebates (Stages 148.15, 148.17).** **Vendor Advances** (Procurement) pays a vendor before the bill: **Give advance** (vendor, optional purchase order, amount) posts to **1810 Vendor Advances** against the bank, and a vendor held from payment cannot be given one. Press **Apply** on the advance and pick from that vendor's Matched bills, or **Advance** on a Matched bill and pick from that vendor's open advances; an advance with a purchase order only lists bills on that order. The bill is paid from the advance (the part-paid history shows an *Advance* line), several bills can use one advance, and the rest stays open (`FIN-0922` if you ask for more). **Refund** returns the open amount to the bank. **Cancel** shows only until the advance is used. Advances and rebate claims are made only by these buttons, not typed or imported (`FIN-0926`, `PURCHA-5025`). No TDS is withheld on an advance, and a bill that has an advance applied cannot then be paid with TDS; deduct TDS on the bill instead. **Volume rebates** appear only when your administrator has turned on *Volume rebates from vendors*. Put a target and up to two tiers on the vendor agreement (**Rebate on** Gross or Net), then **Vendor Rebate Claims > Calculate** for a period: one claim per vendor and period. **Send** it for approval; on approval the rebate is accrued (**1820**), and **Settle** raises the debit note and clears it. A re-run changes a Calculated claim, never an approved one.

**On the goods receipt**: open **Supplier documents and transport** and type the vendor's **challan no / date** (kept apart from the invoice), the **LR / AWB** number, **vehicle**, **transporter** and how many **packages** came. They can be corrected at **Back to Receive** until the receipt posts, and print on the GRN. A goods receipt refuses POs in two currencies (`GOODSR-1317`) - receive each currency on its own - and an invoice number already booked for that vendor, on another receipt or as a vendor bill of its own (`GOODSR-0941`).

**Reports → Procurement**: **PO expected delivery and terms** (each PO's date, days late, terms and amendments), **Goods receipt discrepancies by vendor** (received, accepted, rejected, damaged, short of the invoice, rejected %), **Vendor document and transport register** (invoice, challan, LR / AWB, vehicle, transporter, packages per receipt) and **Goods received not invoiced (GRNI)** (each receipt's accepted value, what its vendor bills have cleared, what is still not invoiced, the price difference and any bill on AP hold).

### 6.15 What stock costs, and what a sale books

Nothing here needs setting up. The system works out each item's cost by itself.

**One rate per item, across all stores: the value of the stock in hand ÷ the units in hand.** Nothing that has already been sold counts.

- **A receipt** blends in. 10 in hand at ₹100, 10 more received at ₹200: the rate is ₹150.
- **A sale** takes units out at the rate and leaves the rate as it is.
- **Nothing in hand** (or less than nothing): the next receipt's price is the rate. 1,000 at ₹100 received and sold, then 10 at ₹200 received: the next sale costs ₹200.
- **A store transfer** changes nothing. It is the same stock in another place.
- **A count or stock adjustment** posts at the current rate, never at the last price, and leaves the rate as it is.
- **Units rejected or damaged on a receipt** were never given a value, so they stay out of the rate until they leave.

**Pieces and batches carry their own cost.** For an item tracked by serial or batch, each piece or batch remembers what it was received at.

- **At the till**, scan the piece code or the batch sticker. The line shows it, and the sale books that piece's or that batch's own cost.
- **On a sales invoice** (§9B.3a), type it into **Piece codes** or **Batch** under the item.
- A piece that is not in stock, or a code that belongs to another item, is refused (`FIN-0850`).
- **At Pick or Pack**, scan the piece code (§9A.9). Dispatch books that piece's own cost. If the piece was sold in between, dispatch is refused (`FIN-0850`); **Reject** the task to send it on.
- **What you make** carries its own cost too. Give the piece codes or the lot when you complete a production order (§9F.2) or receive job work (§9F.2a); its cost is the material that went in. Left blank, it goes into the rate as before.
- A sale that only scans the item's own barcode names no piece, and books the rate.
- One line holds one batch. To sell two batches of an item on one bill, ring up the second on its own sale.
- A held bill comes back without its piece codes. Scan them again after you resume it.

**What comes back goes back at what the sale booked.**

- A **customer return** taken back as sellable, a **cancelled bill** and a **cancelled invoice** put back the cost the sale booked, not today's rate. The rate moves to match.
- A **credit note** moves no stock, unless you fill in **Piece returned** and **Back to store**. Then that piece goes back into that store at what the invoice booked (Dr 1200 Inventory / Cr 5100 Cost of Goods Sold). A piece the invoice did not sell, or one already back, is refused (`FIN-0851`). Several pieces or plain units come back through **Returns** instead.
- A sale made before this rule has no booked cost on record, so it comes back at today's rate.
- A service or a non-valuated item booked no cost, so it comes back at none. A free gift comes back at what it cost on the day of the sale.
- A **bundle** comes back with no value: its parts' cost stays in Cost of Goods Sold.

**A purchase return** takes its own value out, and the rate of what stays moves to match.

**A bill or a late charge that differs from the receipt** is split when it is matched or applied, with no month-end step:

- The units **still in hand** take their share, and the rate moves.
- The units **already sold** post their share to **5195 COGS Variance**.

For example: 10 received at ₹100, 6 sold, then ₹100 of freight on a **Landed Cost Voucher**. ₹40 goes on the 4 in hand, so the rate is ₹110, and ₹60 goes to COGS Variance. Tracked pieces take the charge on the pieces still here only.

A bill difference under ₹1 is left alone. So is a bill in another currency, whose rupee value is settled at payment, and a bill with no receipt behind it.

- **A bill on hold** books nothing until it is released. It books once: when a new **Match** clears it, or when **Override & Pay** pays it. For example: 10 received at ₹100, 6 sold, a bill for ₹1,500 held, then paid by override. ₹200 goes on the 4 in hand, so the rate is ₹150, and ₹300 goes to COGS Variance.
- **A second bill on one receipt** books its own part. A bill with **Bill Lines** answers for the units it lists. A bill without them answers for what the earlier bills left.
- **A receipt done in steps** books only what the bill differs from the invoice keyed at **Receive**. The debit / credit note has settled the rest (§6.10).
- **A deleted bill** takes back what it booked, split by what is in hand and what has been sold by then.

**Where to see it.** **Reports → Inventory Valuation** shows each item's value, and **Rate From** says where its rate came from: **Average in hand**, **Own cost**, **Standard cost** or **No cost yet**.

## 7. Moving Stock Between Locations (Stock Movement)

1. Open **Inventory → Stock Movement**. It lists every transfer between stores (its document is still the *Stock Transfer*); **+ New Stock Transfer** starts one.
2. The screen opens on the list of transfers, the last changed first. Click **+ New Transfer**, fill in the From and To warehouse/location, then add one or more line items (SKU + quantity) using the **Add Line** button — a transfer needs at least one line before it can be created. The bar at the top counts the lines and units. The Transfer Number is filled in for you when you save (§6.1).
3. Click **Create Transfer** (top right). It starts as a **Draft**, and you're back on the list with it on top. Adding the same item twice adds to its one line.
   If you work at only some stores, you can create a transfer only when its From or To store is yours, dispatch it only from its From store and receive it only at its To store. Otherwise you see "not one of your stores" (`STORE-0855`); ask an administrator to add the store to your login.
4. **Get it approved.** A transfer is approved by someone other than the person who made it:
   - **Send for approval** puts it in **Pending Approval**. A Store Manager (or an administrator) opens Stock Movement (or **Approvals**) and clicks **Approve** — or **Reject**, with a reason. The person who created it may decide it too unless your administrator set **Who can approve their own documents** to *Nobody* (then they get "you cannot approve or reject a document you submitted").
   - **Approve now** is for small transfers. If your business has set a **transfer direct-approval limit** (Admin Guide, Settings → Inventory) and the transfer is worth less than it at item cost, a Store Manager of the sending or receiving store approves it alone. Otherwise the button tells you why (limit switched off, value at or above the limit, or an item with no cost on record) and offers to send it for approval instead.
5. Click **Dispatch** to move the stock out of the source location (it sits "in transit" until received). If an item is tracked:
   - **Serial-tracked** — scan one serial number per unit (one per line). Each must be in stock at the sending store; it is reserved to this transfer until it arrives.
   - **Batch-tracked** — leave the lots box blank and the lots with the earliest expiry are taken from the store's bins (expired, blocked and already-reserved lots are skipped). Or name them, as `LOT-A:3, LOT-B:2`. A store with no bins has to name the lot.
   The dispatch is refused, with the item named, if the lots or serial numbers do not cover the quantity.
6. **Receive it — in one go or delivery by delivery.** Click **Receive**. For each item enter what arrived: **Good**, **Damaged**, **Lost** and **Extra** (more than was sent). A batch item has a **Lot** choice (default: oldest lot first); a serial item lists the serial numbers in transit — move any damaged or missing ones to the **Damaged** / **Lost** boxes.
   - **Counted the delivery into a file?** Click **Upload received quantities** at the top of the Receive form and pick a file with **Barcode** and **Qty** (**Download template** gives one). Each item in the file gets that quantity in **Good** — anything above the quantity in transit goes to **Extra** — and every item not in the file goes to 0, so it stays in transit (or is lost, if you untick the box below). An item that is not on this transfer, or is received by serial number, is listed back and not filled in.
   - Damaged, lost and extra units need a **reason** (reason codes of type *Transfer Discrepancy*; if there are none yet, **Add a reason code** opens Masters → Reason Codes). A code marked *(note needed)* wants a short note typed with it.
   - With **Units not entered here stay in transit** ticked (the default), anything you did not enter waits for the next delivery and the transfer shows **Partially Received**. Untick it to close the transfer now: what is not entered is recorded as lost, with the reason.
   - Damaged units are counted in the store's stock but kept out of sellable stock. Lost units leave the books. Extra units are added to the receiving store.
   - Pressing **Record Receipt** twice (or a retry after a dropped connection) records the delivery once.
7. **Send it back.** While anything is still in transit, **Return to source** puts it back into the sending store's stock (choose the quantity, or the serial numbers, and a reason). Put returned lots away again there. A transfer whose goods all went back shows **Returned**.
8. **Cancel** is on a transfer until it is dispatched (Draft, Pending Approval, Approved, Packed) and needs a reason code of type *Cancellation*. Nothing has moved, so nothing is reversed. Once dispatched, use **Return to source** instead.
   If your business holds approved transfers (Admin Guide, Settings → Warehouse), an Approved transfer keeps its stock out of sale at the sending store until it is dispatched or cancelled; lowering a quantity frees the difference, and a hold older than the stale age shows under **Stale Transfer Holds** on **WMS → Home**.
9. **Details** shows, per item and per lot, how many were sent, received, damaged, lost, returned, extra and are still in transit; each serial number's state; the value at cost and how it was approved; and every receipt and return with who and when.
10. **Print it.** From **Approved** onwards the row has **Print**. The print window (§7B) opens on **Transfer Out** — the dispatch challan that travels with the goods — or, once anything is received, on **Transfer In** — the receipt, with sent and received quantities and the reason for any short line; **Print as** in the window switches between them. A transfer between two stores of the **same GSTIN** prints as a delivery challan with no tax; between two **different GSTINs** it is a taxable stock transfer and the GST is shown (IGST across states).

Received batch or serial stock arrives at the store level, like a goods receipt: put it away into a bin and attach its lot there (WMS → Putaway, then RF Lot & Serial) before it can be picked from a bin.

### 7.1 Moving stock between bins in the same location (Location Movement)

Use this to re-slot stock inside one warehouse or store, for example moving a fast seller to a bin near the packing table. The stock stays at the same location, so nothing changes in what you can sell. Only where it sits changes.

1. Go to **WMS → Storage & Control → Location Movement**. It opens on the history of moves — when, location, item, from bin, to bin, quantity, batch and reason, newest first, 50 a page; the search box above it finds any move by item, bin or batch.
2. Click **+ New Movement** and pick the **Location** by name. A table lists every bin that holds stock there, with the item and quantity.
3. Click **Move** on the row you want to move from.
4. Enter the **Quantity**, pick the **To Bin** (only bins at this location are offered), and optionally a **Reason**. If the bin holds more than one batch of the item, choose the **Batch** too.
5. Click **Move Stock**. You're back on the history with the move on top. **+ New Movement** again keeps the same location, with the bin quantities already updated, for the next move.

The move is refused, with the reason shown, if the destination bin is inactive, Blocked, Full or being counted, would go over its capacity, is in a zone that cannot take that item, or is at a different location. It is also refused for stock an open pick task is holding (§5.2): the message says how many units can be moved. Every move is recorded in the stock ledger with both bins.

### 7.2 A warehouse shared by several clients (3PL owners)

If your business stores goods for other businesses (you are a 3PL), each client is an **owner**. Your administrator chooses whether a warehouse may hold one owner or several (Admin Guide B.3.11). With several owners, each client's stock is kept apart all the way through:

1. **Receiving and putaway.** On **WMS → Putaway**, fill in **Owner** with the client whose goods you are putting away. A bin that belongs to another client refuses them; in a warehouse that holds several clients' stock, putaway without an owner is refused and asks whose stock it is. (A goods receipt can carry the owner too, in its **Owner** field.)
2. **Orders.** Every order is for one owner. Before an order is released to the warehouse, set its owner on **OMS → Pending Orders**: **Owner** on the order's row (shown where WMS is in use), type the owner, **OK**. An order with no owner is given the warehouse's own owner; at a warehouse holding several clients' stock it is refused at release, with a message saying so. Once released, the order keeps its owner. Its pick task carries that owner.
3. **Picking.** A pick list (single order or wave) only ever offers the owner's own units. If that owner has too few in the bins, the pick list shows a shortfall - it never borrows another client's stock. Stock is also sourced only where the owner's own stock covers the order.
4. **Moving and damage.** On **Location Movement** and **Bin Conditions**, fill in **Owner** when the bin holds more than one client's stock; the client's units move (or change condition) and stay theirs. A client's units cannot go into another client's bin.
5. **Counting.** A count line can carry the owner; the posted difference records it.
6. **Billing.** Storage billing counts each owner's own stock.
7. **When something doesn't add up.** **Reports → WMS → Owner Stock Exceptions** lists bins where the owners are credited with more than the bin holds, another client's units in an owned bin, several owners in a one-owner warehouse, and pick tasks without an owner at a shared warehouse - each with what to do. A Store Manager corrects an owner's quantity after counting it (Admin Guide B.3.11).

### 7.3 Receiving a delivery: gate, door, RF receiving and putaway (Stage 55.7)

A delivery runs **Appointment → Yard → RF Receiving → Putaway**, and each screen offers the next piece of work instead of asking you to remember a number.

**Warehouse setup (140.12).** A bin can be saved with a zone that does not exist yet - the zone is made on the Zone master with it (for anyone who may create zones). **Put away** from the waiting list sends the goods receipt the units came in on, so in a 3PL warehouse the owner is taken from it. A sellable customer return at a location with bins is given a suggested put-away bin.

1. **Book it.** **WMS → Appointment Calendar → + New Appointment**: door, Inbound, carrier, trailer, the **ASN it brings** (its PO comes with it), date and time. No doors yet? The calendar's **Set up dock doors** opens the Dock Door list and brings you back. A delivery that will not come: open it under **Setup → Appointment** and set **Status** to *Cancelled* or *No show* (a checked-in one can only be *Cancelled*). *Checked in*, *In progress* and *Completed* are set by the Yard Board and receiving, never by hand (49.5.3).
2. **At the gate.** **WMS → Yard Board → + Check In Trailer**. Pick the **Booked appointment** — trailer and carrier fill in — and **Check In**. The appointment becomes *Checked in*. A trailer already in the yard cannot be checked in again until it is checked out.
3. **To a door.** **Assign to Door** lists every active door: *Free* or *Occupied* (by which trailer) and what is booked on it today. **Spot here** on a free door; an occupied door is refused. The board's **Delivery** column shows the ASN the trailer brought. After each action the board reloads in place: the old rows stay on screen, dimmed, until the new ones replace them (the **Warehouse Cockpit** and the OMS order list do the same, 50.13).
4. **Receive.** At the door, the row's **Receive** opens **RF Receiving** with the ASN loaded (or open RF Receiving, choose the **Receiving Location**, and pick from **Expected deliveries** — trailers at a door first). Then, one carton at a time: **scan the item** → the screen asks **How many?** with what is still expected filled in → **Enter** (or **Confirm**). An item not on the ASN is refused (*set the carton aside, nothing was received*) and offers **Get a supervisor**, which asks a supervisor on their own device; counting more than is still expected is refused; scanning an item already received in full adds nothing. **Post Receipt** books the goods receipt. If some units have not come, it says so: the ASN stays open for the rest, and the receipt that completes it marks the ASN *Received* and the appointment *Completed* — receiving it again is refused.
5. **Put away.** **Put it away now** opens **WMS → Putaway** at the same location. **Waiting for putaway** lists every item received there but not yet in a bin (on hand, in bins, waiting). **Put away** on a row fills the item and quantity and suggests a bin if a putaway strategy is set up; then it is keyboard / scanner only: scan the **bin** → Enter → item → Enter → quantity → Enter. The row shrinks as stock goes into bins. A bin of another location, a blocked or full bin, or more than is waiting is refused with the reason. A location without bins says so, with **Set up bins**.

**My next task on the handheld (146.3.3).** When the administrator has turned on the work queue, a posted goods receipt leaves one put-away task per received item, and a released wave leaves a pick task per bin. On the **RF** screen, **My next task** shows what you already hold, otherwise the top of the open list: pick first, then replenish, put-away, move, count. **Start** locks it to you, so nobody else can take it. **Done** finishes it (a put-away asks for the **Bin**; a pick scans its units). **Split** with a quantity cuts that many off into a new task for someone else; the two add up to the original. Type a document number under **Document** and **Filter** to see only that receipt's or order's tasks. Putting stock away on the desktop **Putaway** screen closes the matching task by itself, and cancelling the receipt or the order cancels its open tasks. With the queue off the RF screen is unchanged.

**More tasks, rules and blocks (146.3.3, 146.3.7, 146.3.14, 146.3.15).** With the queue on, the list also holds **Replenish** (a pick bin running low), **Move** (a requested bin-to-bin move) and **Count** (one per bin of a started Physical Inventory); finishing a Replenish or Move moves the stock. **Suggest Bin** follows home bins, then the **Put-away Rule** for the item, design or category, then the normal strategy; the reason says which. A bin or zone blocked *No receipts* is skipped and refuses put-away; one blocked *No picks* is not picked from. If the administrator turned on split put-away, a quantity too big for one bin is shown as "Split: A x 3, B x 9", and a bin near full shows a warning.

**Cross-dock** (send it straight out instead of shelving) is under **Cross-dock instead of shelving (advanced)** on the Putaway screen.

**Home bins and one-item bins (Stage 146.3).** If a bin is set up for the item, **Suggest Bin** shows it first as "home bin X (priority 1)", and says why when it skips one (Full, Blocked, no room). A bin set to *Single item* refuses a second item with a message naming both; put it in another bin. Your admin sets these up (Admin Guide B.3.22).

### 7.3a A draft order from an AI assistant

When your company uses an AI assistant, it can leave a **Draft** sales order that is marked as made by an assistant. It holds no stock and nothing is posted. Open **OMS → Home**: the **Assistant drafts** panel lists the open drafts at your stores (it is not there when there are none, when the assistant feature is off, or if you may not make orders). Check the customer, store, lines and total, then press **Submit** on the row and confirm. The submit makes the real order the usual way, under your name, and closes the draft. You can only see and submit a draft for a store you may use. An assistant can leave at most 20 open drafts, with a customer name of at most 200 and an address of at most 500 letters.

### 7.4 Shipping orders: wave, pick, pack and load (Stage 55.8)

1. **Make a wave.** **WMS → Wave / Batch Picking**, choose the **Location**. *Orders ready to wave* lists every released order there that is not in a wave yet (Pending or Picking): tick the ones to pick together (or the box in the heading for all), choose **Into** — *A new wave* or an open one — and click **Put N selected into the wave**. An order at another location, already in a wave or past picking is not added, and the screen says which and why.
2. **Release it.** *Open waves* shows each wave's status and how many of its orders are to pick, picking, packed and dispatched. A new wave is *Planned* — **Release** it when the floor should start. Then **Pick list** shows the walking-order list and, per order, what could be allocated and what is short; **Pick on a phone** opens Mobile Picking with that wave.
3. **Pick on a phone.** One line at a time: item, bin, batch and quantity, with *N confirmed · M to go*. **Confirm & Next** (or say "next" / "confirm"). The line moves on only once it is saved: if the confirm is refused (the line was picked on another phone, say), the reason shows and the line stays open. After the last line the screen lists the orders that are complete and the ones that are **short** (which item, how many). **Mark N order(s) packed** packs the complete ones. A short order is left as it is — finish it on **OMS → Pick** when the stock is there, or **Reject** it there to send it to another location. Picking and packing move no stock; stock leaves when the order is dispatched.
4. **Load.** **WMS → Loading** lists the loads open at the doors (door, trailer, manifest, cartons scanned) — **Open** one to carry on, or open a new load with the door, trailer and manifest picked from their lists. The load shows the packages *still to load* on its manifest. Scan each package. If fewer cartons are scanned than expected, **Complete Load** asks first and records what was actually loaded. Then record the pallet exchange and **Depart**, and print the Bill of Lading.

When a wave's orders are all packed, dispatched or rejected, **Complete** it on Wave / Batch Picking.

### 7.4a Gate entry, and the packing list per box (Stage 146)

- **Gate entry (Procurement > Gate Entry).** When a truck arrives, add a gate entry: vendor, PO, parcels, transporter, LR no, vehicle and a photo of the challan. On the list, **Start GRN** opens Goods Receipt with the vendor, PO, LR, vehicle, transporter and parcels filled in; the entry is marked as received against that receipt. Entries are limited to your location. The report **Gate entries with no GRN after N hours** (default 24) lists trucks still waiting.
- **Packing list.** Once a package is closed (not Draft), open it and press **Print** for the packing list: package no, weights, dimensions and contents. A Draft package says to close it first.
- **Stickers for a PO.** On an Approved or Closed purchase order press **Stickers**; Sticker Printing opens on that PO's lines (or pick *Purchase Order* under Print from Transaction).
- **Invoice dated before its PO.** Saving a vendor invoice dated before its PO shows a warning; the report **Vendor invoices dated before their PO** lists them.

### 7.5 Warehouse labour and billing (Stage 55.8)

**WMS → Labour plan** and **WMS → Labour standards** are two pages (288.2).

- **Labour plan.** Pick the dates and **Plan**: open warehouse tasks per department turned into hours and a headcount (planned hours ÷ the hours one worker has in the period, rounded up). *No standard* counts tasks whose task type has no labour standard — they are not in the hours, so set one up.
- **Labour standards.** Each standard with its operation, elements, travel and allowances. Enter a quantity and **Preview** to see the working: each work element (fixed seconds + seconds per unit × quantity), travel (per task + per unit × quantity), allowance (a % of work + travel, plus fixed seconds), the total in seconds and minutes, and the cost at the operation's hourly rate — with *matches the planning engine* confirming it is the same figure the plan uses.
- **+ New labour standard** sets one up in one go: the operation (pick an existing one or name a new one with its task type, department and cost per hour), the work elements, optional travel and allowance, and the standard's code and name. Every field shows its unit. If one step cannot be saved, the screen says which; the steps before it are kept.

**WMS → Warehouse Billing.** Enter the **Owner** (client) and the period and **Review**: what has been captured, what is not yet invoiced and the storage still to capture. Every captured charge shows its working (quantity × rate, markup, discount, minimum charge, tax, total); every storage rate shows average units × rate per day × the days that have a balance snapshot. **Check before invoicing** lists what deserves a look: storage days without a snapshot, charges billed at the minimum, and charges captured before their rate was recorded. **Capture storage charges for the period** and **Raise invoice** do what they say (each asks first; an invoice is posted like any credit sale). Captured charges and snapshots are written by the system — nobody types them in. What to charge is your business decision; the screen only applies the charge codes, rate groups and contracts you set up (Admin Guide B.3.16).

### 7.6 The warehouse (WMS) screens — where to start

Every warehouse screen is under **WMS** (Home, then the **Inbound**, **Storage & Control** and **Outbound** groups), and the **?** (Help) button on each one opens the Knowledge Center article **Warehouse (WMS) - what each screen is for**: the one-time setup order, receiving, keeping stock right, shipping, and a one-line summary of every menu entry. In short:

- **Set up once, in this order:** Location → Zones (optional) → **Bin** (Bin Code, Location, Status; capacity and Bin Status are enforced) → each Item's **Batch, serial and expiry** section → **Dock Doors** → **Hold Codes** (Setup → WMS → Hold Code).
- **Receiving:** ASN → **Appointment Calendar** → **Yard Board** (check in, assign to a door) → **RF Receiving** or **Goods Receipt** → **Putaway** (the list shows stock not yet in a bin) → for batch-tracked items, **RF Lot & Serial → Put a lot away**.
- **Keeping stock right:** **Location Movement** and **Bin Replenishment** move stock between bins; **Bin Conditions** marks stock Damaged / QC-Hold / RTV; **Place Hold** stops a quantity being sold (release it through **Hold Release Requests**); **Cycle Count** corrects it.
- **Shipping:** releasing a sales order creates its tasks → **Wave / Batch Picking** (or **Mobile Picking** on a phone) → **Loading** → handover on **OMS → Manifests** (§9A.11), which dispatches the tasks and takes the stock out.

Moving stock between bins never changes how much you have at the location; a Goods Receipt, a transfer, a sale and an approved cycle count do. A posted Goods Receipt has a **Put away** button that opens Putaway on its own items; held stock is never picked; Mobile Picking keeps each confirmed line on the server, so a reload or another phone carries on; Sortation sets up its slots and takes a whole wave; a bin's cycle count sets the bin; a receipt meets its cross-dock plans; and a departing load checks its trailer out of the yard. The Help article *Warehouse (WMS) - what each screen is for* has the details.

### 7.7 Transfers: purpose, short stock, waves and an allocation upload (Stage 146.3)

- **Purpose.** The New Transfer page has a **Purpose**: Replenishment (the default), New store or Launch allocation. It decides how a short transfer is handled (below).
- **When the source has less than the transfer asks (off until an administrator turns on *Transfer fill policy*).** **Dispatch** checks what is there first. A *Replenishment* transfer opens **Ship what is there**: it lists what will go, you pick a **reason** for the short lines, and the transfer is sent short; the short lines are recorded on the transfer, not lost. A *New store* or *Launch allocation* transfer is **Complete only**: it will not go until the whole quantity is there (`STOCKT-0490`). A minimum fill % set on the transfer or on the receiving store refuses a dispatch that would ship less (`STOCKT-0489`). A transfer's own policy beats the store's, and the store's beats the purpose. With the setting off, a short transfer is refused as before.
- **Waves.** When an administrator turns on *Transfers can join waves*, an **Approved** or **Packed** transfer can be added to a wave at its source location. (There is no button for this yet: it is the wave assign call with `transfer_ids`, see the Admin Guide.) Releasing the wave reserves bins for the transfer along with the orders, the picks reach the pickers' work queue with the transfer named as their source, and **Dispatch** takes what was reserved. A transfer added after the wave was released is reserved the next time you generate the pick list. Cancelling the transfer frees its bins (`STOCKT-0491` if the transfer cannot join).
- **Allocation upload (Stock Movement → Allocation).** One file, one transfer per store. Give a **Run ID**, choose **From** (each store reads as its name with its code, for example *Head Office (HQ)*), pick a **Purpose**, and click **Choose file** to pick an Excel or CSV file in one of two shapes: rows of **store, sku, qty** (rows for the same store and SKU are added together), or one row per SKU with a column per store headed by the store's code, the quantity in each cell (a blank or 0 cell sends nothing). **Preview** checks every row - an unknown store or SKU, a quantity that is not a whole number above 0, or the source itself as a store is listed against its row number - and shows what the source cannot cover; it makes nothing. **Make Drafts** creates one Draft transfer per store from the good rows; the rows with a problem are left out, and **Rows to fix** downloads them with a **Reason** - correct them and upload that file under a new Run ID (a Run ID can be used once, `STOCKT-0487`; a file where no row is good makes nothing, `STOCKT-0488`). Then **Submit all** sends the Drafts for approval, and a second person uses **Approve all** - the same maker-checker rules as any transfer; a transfer that cannot be approved is listed with its reason, and the rest go through (`STOCKT-0492` when nothing is waiting).
- **From a goods receipt.** Type a posted goods receipt's number in **From a goods receipt** and click **Download sheet**: an Excel sheet with each SKU received (accepted quantity, design and name) and a column for every other store. **From** is set to the receipt's store and the Run ID to its number. Fill the store columns and upload the sheet as above. A receipt that is not posted yet, or has nothing accepted, says so.
- **Reason codes marked High (74.1.1).** Receive a transfer with damaged, lost or short units under a reason code whose severity is **High**, and the receipt is not posted: it waits as a **Transfer Loss Hold** in **Pending Approval**, and the whole receipt (good units too) waits with it. Stock moves only when an approver approves it; rejected, nothing is posted.
