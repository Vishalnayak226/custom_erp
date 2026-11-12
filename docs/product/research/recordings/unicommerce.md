---
doc_id: DOC-PRODUCT-RESEARCH-REC-UNICOMMERCE
title: Recording blueprint - Unicommerce and the D365 DOM boundary, gap-checked
type: reference
status: draft
owner: product-owner
approvers: [product-owner]
audience: [product, engineering]
applies_to: docs/micro_checklist.md Stage 146
authority: source
confidentiality: internal
last_verified: 2026-10-31
review_by: 2026-12-31
supersedes: none
superseded_by: none
---

# Unicommerce (Uniware) plus the D365 DOM / Unicommerce boundary: blueprint and gap check

> Stage 146 (2026-10-31): learned from the user's meeting recordings (`Downloads\Telegram Desktop\Dynamics Loyalty Management Demo 1`). Citations `(key mm:ss)` point into those videos; the machine transcripts and screen OCR were working files and are not kept. Gap rows were checked against the working tree on 2026-10-31. The plan built from these files is `docs/product/recordings-gap-plan-2026-10-31.md`.

## 1. Sources

- **uc1** "Manoj Unicommerce Learning-1" (35 min, 19 Dec 2022), **uc3** "-3" (42 min, 20 Dec 2022, Meet title "Inbound - session") and **uc6** "-6" (13 min, late Dec 2022). Internal Kushal's training. Manoj Kumar K (Kushal's) presents to Harish Babu, Vishal Nayak, Monali, Shriram and others. The product is Unicommerce **Uniware**. uc1 and uc3 run on the staging tenant `stagingkushals.unicommerce.com`, facility **BLR E-Commerce**. uc6 runs on production `kushals.unicommerce.com` with "All Facilities". The audio is Tamil/English, machine-translated and noisy, so screen names and columns below come from the OCR and frames. uc1 covers purchase and replenishment, uc3 covers inbound (GRN, labels, QC, put-away), uc6 covers channel listings and the product master. **No recording shows sale orders, picklists, packing, invoicing, manifests or returns being processed.** Those appear only as menu names and put-away types (uc6 11:10 "Sales maybe we can do it tomorrow").
- **loy1 20:00-43:00**: Microsoft Dynamics 365 Commerce demo (Oct 2023, sandbox `cvtuat...dynamics.com`, company CRET) by Muthu (partner) for Kushal's team. It covers Kushal's channel topology, D365 Distributed Order Management (DOM) and how D365 would sit beside Unicommerce.
- Client context: Kushal's Fashion Jewellery. One e-com warehouse (BLR E-Commerce), BLR Warehouse and about 78 stores. Unicommerce facilities include stores (CHN T Nagar, BLR Jaynagar: facility drop-down at uc3 08:30). Today's POS/back office is GoFrugal. The PIM is Unbxd (its tabs are open in uc3/uc6). Shopify shows Unicommerce as a sales channel/app (uc6 12:45).

## 2. Blueprint

### 2.1 Navigation (what the product has)

- **Purchase** menu (uc1 10:30, 21:45): PURCHASE ORDERS (Purchase Orders, Search ASN) · REPLENISHMENT PLANNING (Add to Cart, Purchase Cart, Back Orders, Reorders, Reorder Config) · VENDOR MANAGEMENT (Vendors, Vendor Catalog).
- **Inbound** menu (uc1 10:00; uc3 32:15): PUTAWAY (Putaway, Awaiting Putaway, Search PO, Item Details) · GOODS RECEIPT (GRNs, Gate Entry, Create Label, Quality Check, Create PO Labels) · TRANSITION (Add Inventory).
- **Products** menu (uc6 11:00): Products, Categories, Facility ItemTypes, Listings · Materials: TRANSFERS (Gatepass) · INVENTORY (Channel Inventory, Inventory Snapshot, Search Product) · CONFIGURATIONS (Tax Classes, Discount Group Items) · CYCLE COUNT (Cycle Counts).
- **Settings** (uc6 00:30): CHANNELS (Channels, Channel Return Facility Mapping) · USERS (Roles, Datatable Views, API) · LAYOUTS (Package Types, Search Shelf, Create Shelf) · FACILITIES (Allocation Methods, Billing Parties, Business Lines, General Settings, Custom Fields, Email Templates) · SHIPPING PROVIDER (Shipping Providers, Serviceability, Shipping Rules) · OTHERS (Export Job Type, Import Job Type, Templates, Edit Templates, Scripts, Reconciliations, Pick Buckets). These were shown as a menu only and not opened.
- Top bar: facility switcher (BLR E-Commerce / BLR Warehouse / CHN T Nagar / BLR Jaynagar, uc3 08:30), a document-type search box ("Sale Order"), import (upload) and export (download) icons, and notifications.
- **Import framework** (`/tasks/import`, uc1 00:15, 09:15): choose an Import Type, then "Import Options" (Create New ...), "Download CSV format", and a column table (Column Name, Description, Required). **Export framework** (`/tasks/export`, uc3 12:00, 21:45): choose an export type, set filters (name contains, SKU, status, created-date range, inventory type), then "Send an Email on Job Completion" and CREATE EXPORT.

### 2.2 Purchase orders (uc1, uc3 33:30)

- **Purpose:** buy from a vendor into a facility, with approval.
- **Screen** `Purchase Orders / #<PO>` (uc1 00:45, 25:00) has tabs PO Items · GRN · Activities(n).
  - Lines: Item Name, Uniware SKU, Vendor SKU, Ordered Units, Pending, Unit Price, Unit Discount, Total.
  - PO Details panel: PO No., Vendor Name, Vendor Code, Status (APPROVED / COMPLETE / WAITING FOR APPROVAL), Reject Reason, Type (MANUAL / CART), From Party (the billing party, "Kushals Retail Pvt Ltd"), **Amended From** (link to the earlier PO, e.g. P00007 at uc3 10:45), Created Date/By, **Expiry Date**, **Delivery Date**, Agreement.
  - Payment Summary: Subtotal, Total Discount, Total Tax, Logistic Charges, Estimated TCS, Total Order Amount.
  - Also on the screen: Attachments, Comments, Billing/Shipping Address, and client custom fields (Additional_Cost_Inclusive_of_Taxes, Channel Name, VendorInvoiceNumber).
- **Actions** (uc1 00:22-00:29; uc3 33:30 buttons): Create, Send for approval, Approve, **Print**, **Clone**, **Close**, **Amend**, **Resend Approval Email** (approval reminder), **Send Delivery Reminder**.
- **Status tabs** on the list (uc3 32:15): APPROVED · WAITING FOR APPROVAL · CART, plus CREATE PO. Columns: SKU Units Ordered / Received, amount, dates.
- **Bulk import** "Multiple Facility Approved Purchase Order" (uc1 00:15-00:30). Columns: Vendor Code, Facility Code, Agreement Name / Start / End / Text, Product Code, Quantity, Unit Price, Discount, Logistic Charges, Expiry Date, Delivery Date, Purchase Order Code, Additional cost, Channel Name, VendorInvoiceNumber.
- **Rules:**
  - Each PO needs approval. An e-mail says "PO created, waiting for approval" (uc1 24:43-25:01).
  - Anything beyond the standard options needs a ticket to Unicommerce (uc1 00:00-00:10).
  - Shortage is handled by amending the PO down to what came. Excess is either kept, by amending the PO up and taking a second vendor invoice, or sent back on a gate pass. "There is no debit note / credit note concept" (uc3 17:27-19:49).

### 2.3 Replenishment: reorder config, reorders, purchase cart, back orders (uc1 00:47-31:20)

- **Reorder Configuration** form (uc1 01:15, 14:30): SKU, Threshold Quantity, Minimum Reorder Quantity, Maximum Reorder Quantity, Status (Enabled). Helper text: "The suggested reorder quantity will be the deficit amount from threshold... If you want to continue with Fixed Qty, input the same in both Minimum & Maximum."
  - List view (10:45, 15:00): Item Name, SKU Code, Facility, Reorder Threshold, Min Reorder Qty, Max Reorder Qty, Enabled, Last Updated At, Edit. The config is **per SKU per facility**.
- **Bulk import** "Reorder Configuration" (09:15) with columns Item SKU Code, Reorder Threshold, Minimum Reorder Quantity, Maximum Reorder Quantity, Enabled. Rules printed in the template:
  - The reorder triggers when **Inventory + Open Purchase + Putaway Pending** goes below the threshold.
  - Min ≥ 1. Max ≤ 10,000.
  - 1/true = enabled.
- **Business rule, worked example** (01:06-08:52): threshold (base stock) 1,000, min 250, max 500. Stock falls to 150, so the deficit is 850, which is above the max, so the system raises 500. At 999 the deficit is 1, which is below the min, so it raises 250, because a vendor will not supply one piece. The max caps capital tied up in high-value designs ("I do not want to lock my capital"). Any remainder is raised on a later run.
- **Live demo** (11:18-16:03): SKU with 11 in stock, threshold 12, min 5, max 20. The deficit is 1, so a reorder of 5 appears automatically. The presenter says each stock event re-runs it (23:27-23:54).
- **Reorders** screen (17:30, 24:15) has ENABLED / DISABLED / ALL tabs. Columns: SKU Code, Item Name, Category, Vendor (with the vendor's price, "Test vendor (150)"), Available Quantity, Quantity Deficit, Quantity in Cart, **Item Bucket Size**, Quantity To Add.
  - The planner may lower the quantity (5 → 3 "I don't have that much money"). Zero cannot be added to the cart. An un-carted remainder keeps reappearing until it is raised, or until open supply covers it (19:32-21:35, 25:35-26:49).
- **Add Items To Cart** (27:00): manual buying by browsing the vendor catalogue. Columns: Product Code, Vendor SKU, Category, Vendor, Quantity in Cart, Item Bucket Size, Quantity To Add, Unit Price, Product Status.
- **Purchase Cart** (21:45, 28:15): Product Code, Product Name, Category, Vendor, Quantity, Unit Price, Total.
  - Lines can be selected and turned into a PO, or removed. Lines can come from several vendors.
  - Create Purchase Order makes the PO with the vendor and price already known (type **CART**). Status is Waiting for approval, then Approved (24:20-25:01).
- **Back Orders - Waiting For Inventory** (29:30): Product Code, Product Name, Category, Vendor ("No Vendor for this item type"), Quantity Waiting, Quantity in Cart, Quantity To Raise, Unit Price, Product Status.
  - Meaning: sales are waiting with no stock (29:27-31:20).
  - They arise when the picker finds fewer pieces than the system holds. The picker invoices what is there, a stock adjustment zeroes the rest, and the waiting orders become back orders (32:12-33:08). A cycle count finding can do the same.
- **Advance Shipping Notices** (33:30): ASN No, Purchase Order, Vendor, Status, ASN Created, Expected Delivery, Quantity.
  - Only useful with the Uniware vendor panel or an integrated vendor panel. The vendor states the delivery date and slot.
  - Presenter: "Amazon / Cloudtail refuse goods without an ASN" (33:23-34:38).
- **Trainer's framing** (28:33-29:12): the cart and reorder flow is "super power for a startup". For an established company like Kushal's "this may not mean much because they will be having better systems". **This is generic Uniware, not a Kushal's-specific setup.**

### 2.4 Inbound: gate entry, GRN, labels, QC, put-away (uc3)

**Gate Entry** (`/inflow/gatePassEntry`, uc3 37:43-38:38)
- The security desk records an inward parcel before the GRN: Code (IGP0001), Username, Party, Number of Parcels, Transporter, Tracking Number, Reference, Vehicle Number, Created, material description and an attachment (a scan of the documents).
- The GRN team reads this list to see which receipts are pending.

**Search PO** (`/inflow/searchActivePO`, 32:39-33:44)
- Lists only approved POs (Status, Approve Time) so a GRN clerk can receive without the Purchase Orders screen. Giving that screen would also grant create and edit rights. **This is a role-separation rule.**

**GRN** (PO > GRN tab, 00:00-10:21)
- List columns: GRN #, Vendor Invoice #, Vendor Invoice Date, ASN, Transportation And Other Charges, GRN Status, Received Units / Amount, Rejected Units / Amount, Created By / On, plus the custom field Manufacturing_Date.
- Line columns: Item SKU, Vendor SKU, Batch Code, Status, Received, Rejected, Pending, Price Info, Additional Cost, Mfg Date, Shelf Life, Exp Date.
- Create GRN form (01:00): Copy From GRN (optional), Vendor Invoice Number, Vendor Invoice Date, Manufacturing_Date, Transportation And Other Charges (optional).
- **Rule:** the invoice date cannot be before the PO date ("GRN cannot be done before the purchase order", 02:09-02:27).
- Lines are added by "Scan SKU To Add" or by typing. The quantity is typed. Unit price defaults from the master and can be edited. Additional cost can be added (02:38-03:15).
- Many PO lines are received over several GRNs. Pending stays open (00:04-00:41).
- **Jewellery-specific** (03:28-06:11): Kushal's vendors are small, send no barcodes and often hand-write invoices. The clerk therefore types quantities, and labels are created and printed at the GRN. Big garment vendors pre-label instead.

**Item labels and item-level traceability** (04:39-09:53, 35:23-36:00)
- After quantities are entered, "Print labels" generates **one unique item code per piece** (20 pieces give 20 codes 1044-1063) and downloads a label file. Its columns are ItemCode, ItemSku, ItemName, ItemCategory, VendorSku, ItemMRP, ItemCreated, ItemSize, ItemStyle, ItemColor (06:30).
- This is because **item-level traceability is switched on for the BLR E-Commerce facility**. A facility on SKU-level traceability prints the SKU code instead (08:18-08:56).
- Why piece codes: aging per piece, FIFO, cycle count, bin-to-bin moves, and knowing which shelf a scanned piece came from out of about 2.5 lakh pieces (09:01-09:37).
- Labels must exist before QC.
- Reprinting is done from the GRN. "Create PO Labels" would mint a new sequence, so it should not be used for reprints (35:23-36:00, 39:39-40:14).
- **Create PO Labels** (`/inflow/poLabels/create`, 40:00): Purchase Order Code, Manufacturing Date, Label Quantity → Create Labels. It pre-prints codes before the GRN for vendors who label at source.
- Create Label / Quality Check (`/inflow/grn/createLabels`, `/inflow/poLabel/qc`, 38:40-39:35) were opened, but the presenter said he did not know how they work.
- **Rule** (uc1 14:03-14:47): stock with item-level traceability cannot be reduced by a stock adjustment. It leaves only by a sale or by "liquidation" on a non-returnable gate pass.

**GRN status flow** (10:00-11:19)
- CREATED → (Complete GRN) → **QC_PENDING** → (QC) → **QC_COMPLETE** → (put-away) → **COMPLETE**. The GRN status is the lowest status among its lines (26:40).

**Quality check** (10:21-11:19; frame 10:45)
- Per GRN line: "Scan Rejected Items: Item code" plus "Rejection Reason: Select Reasons" → Save. Then tick the lines and press "Quality Check Complete" ("select at least one row").
- **Rules** (14:15-16:07):
  - Until QC rejects it, a received piece counts as GOOD inventory, even while QC is pending.
  - A rejected piece becomes BAD inventory.
  - **QC-pending pieces cannot be put away.**
- An e-mailed **QC_REJECTION report** follows (11:42): P.O. No., Invoice No., GRN No., Report generated by, Report date, Total qty in GRN, Total qty QC rejected, Defective SKU, Category, QC Checked, QC Rejected, Comments.
- Rejected pieces go back to the vendor on a gate pass. The system generates the GST document for the return to vendor (RTV) when the vendor is in another state (19:55-20:29).

**Item Barcodes export** (11:26-16:30)
- Filters: date range. With no range it covers "the last 15 months". Unicommerce keeps piece codes for 15 months (12:04-13:15).
- Columns: Item Code, Created On, Updated On, Item Status (QC_PENDING / QC_COMPLETE / ...), GRN Number, Shelf Code, Category, Item Type Name, Brand, Colour, Size, SKU code, Vendor SKU code, Expires On, Unit price without / with tax, Custom field values, **Days in warehouse**, Inventory type (GOOD_INVENTORY), **Myntra SKU Code, Myntra Style ID**, Facility Id.

**Put-away** (21:55-32:03; frames 22:15, 22:45-23:45, 41:45)
- Create Putaway → choose a **Putaway Type**. Seen types: GRN item, GATEPASS_ITEM, INSPECTED_NOT_BAD_ITEM, INSPECTED_NOT_GOOD_ITEM, SHELF_TRANSFER, PICKLIST_ITEM, RECEIVED_RETURNS. The transcript adds return item, transfer item, courier return, customer return and reverse pickup.
- Steps:
  1. Give the GRN number.
  2. Tick the QC-complete lines (QC-pending lines have no checkbox) → Add To Putaway.
  3. Create Putaway List.
  4. Per line, Edit Shelf / "+ Assign New Shelf" (Bin-1, A01, M05).
  5. Complete Putaway.
- QC-rejected pieces go to a bad-inventory shelf.
- A putaway can be discarded before its list is created (31:20-31:34). List statuses include DISCARDED (41:45).
- **Rule:** **stock enters inventory (and so the channels) only at put-away, not at GRN.** "Put away means going live" (16:07, 24:38-25:20).
- Shelves must already exist in the shelf master (27:21-27:49).
- The presenter could not split 19 pieces across two shelves and was unsure how (28:44-31:09).

**Awaiting Putaway** (32:13-32:38, frame 32:30)
- Tabs PUTAWAY PENDING · PUTBACK PENDING. Status values GRN GATEPASS, CUSTOMER RETURN, STOCK-TRANSFER, COURIER RETURN, RETURN-AWAITED. Columns: Username, To Party, Reference, Created At.
- One queue of everything that still has to be shelved, whatever its source.

**Other inbound screens**
- **Item Details** (`/inflow/scanItem`, 37:20-37:41): scan an item code to see product code, vendor SKU, GRN and good/bad status.
- **Add Inventory** (`/procure/createInventory`, 40:16-42:18): pick SKU, vendor, shelf, quantity and shelf life → CREATE ITEMS (mints piece codes) → ADD TO INVENTORY, or DISCARD ALL. Stock "injection" also only lands through this put-away-like step.
- **Activities** tab on PO/GRN: who did what (31:57-32:03).

### 2.5 Inventory screen (uc1 11:00-14:00; uc3 24:45)

- Tabs: GOOD INVENTORY · BAD INVENTORY · ALL.
- Columns: Sku Code, Item Name, Shelf (DEFAULT, Bin-1, A02, M05), **Shelf Attributes** ("Inventory Allocation: true · Inventory Sync: true"), Batch, Batch Status, Type (Good / QC Rejected), Total (Stock on hand), **Available (ATP)**, **Blocked (Committed)**, **Not Found**, Size, Color, Brand, Updated.
- **Rule:** a shelf can be excluded from order allocation and/or from channel inventory sync.

### 2.6 Channels: listings, linking, sync (uc6 00:00-10:05)

- **Listings** (`/channel/productMapping`) has tabs LINKED · UNLINKED · SYNC ENABLED · SYNC DISABLED. Columns: Channel, Product Name on Channel, SKU Summary (Seller SKU on Channel, Channel Product Id), Synced From (Catalog), Created, Action (Link).
- **Process:**
  1. The seller lists products on a marketplace with the marketplace's template, giving the client's own SKU as "vendor SKU" (02:19-05:29). The marketplace issues its own codes. Each of Amazon, Myntra and Shopify has its own.
  2. About 30-60 minutes later a Uniware job fetches the new listings, as a delta (only new ones), into **Unlinked** (00:00-00:53).
  3. The user links each channel product to a Uniware SKU, one by one ("Product Link" dialog: Item Type, channel product id, LINK, 01:45) or in bulk (07:31).
  4. Orders for that channel product then resolve to the SKU (02:00, 07:21-07:31).
  5. A newly linked listing starts **Sync Enabled**. Disabling it stops **inventory** sync to that listing only. **Order sync cannot be stopped per listing, only per channel** (07:41-08:54).
  6. Deleting a link sends the listing back to Unlinked (09:01-09:59).
- **Linked tab** (09:30-09:45): ListingStatus ACTIVE / INACTIVE, Selling Price, Price Update History, Disabled. Bulk actions: SYNC INVENTORY, EDIT INVENTORY, DELETE, SYNC PRICES, MATCH COMPETITIVE PRICES.
- **Rule:** Uniware is the origin of channel inventory. Channels show what Uniware pushes (08:14-08:57).

### 2.7 Product master (uc6 10:15-12:00)

- `/products`, 40,882 products, tabs ENABLED / DISABLED / ALL. Columns: Item Name, Sku Code, Category (all "Default" in the demo), Color, Brand, HSN Code (7117). ADD PRODUCT or bulk upload. Disabling a product moves it to Disabled.
- The fields named are name, SKU code, category, colour, brand, HSN, size and weight (10:38-10:47).
- Shopify admin (uc6 12:30-13:00, context): the Shopify product carries metafields Design No., Colour, Polish, Base Metal, Stone, Earring Weight, plus tags such as HSN:7117 and Festive. Unicommerce is listed under "Sales channels and apps". Shopify's own analytics show sell-through rate, days of inventory remaining and ABC grade by inventory value.

### 2.8 D365 Distributed Order Management and the Unicommerce boundary (loy1 20:00-43:00)

**Kushal's channel topology** (20:33-22:20)
- Four or five online channels. The online store (Shopify) and Myntra ship from the central warehouse. Amazon, AJIO and Flipkart orders can ship from any of about 78 stores, the one nearest the customer by PIN code.

**D365 design as demoed**
- Each online channel becomes a "store" with no stock of its own (Kushal Shopify = store KS000617 with a virtual warehouse, 29:07-30:15). It is mapped to the central warehouse, where its orders land by default (22:21-23:01).
- **DOM** allocates the other channels' orders by customer PIN code against store PIN code and stock. If the nearest store has no stock it goes to the next nearest (23:04-25:40).
- DOM screens:
  - Fulfillment Plans: plan id, order, variant, size / colour / style, site, quantity (23:45).
  - Summary: Exception lines, Orders exceptions, Scheduled jobs (active / past), Unprocessed lines, Rejected order, Open lines (23:30).
  - The sales order carries "DOM Status: Not processed" and "Order fulfillment policy" (39:45).
- **DOM rules** (32:30 frame, Manage rules): Fulfillment location priority, **Maximum distance**, **Maximum orders** (per store), **Minimum inventory**, **Offline fulfillment location**, **Partial orders** (order / line). Each rule has a "Hard constraint" Yes/No and a Disabled flag. Setup also has DOM parameters, Cost configuration and Fulfillment profiles.
- The fulfilling store sees the work at its POS under "Picking and receiving" as **Transfer out**. It ships and closes it, and the **packing slip records who delivered** (32:48-35:48).
- Loyalty points belong to the **channel where the order started**, not the fulfilling store. Shopify buyers earn nothing because that store is left out of the scheme (26:44-30:07). This is covered in `blueprint/loyalty.md`.

**Kushal's answer: keep Unicommerce** (36:29-37:16)
- "We are using Unicommerce as the order management system, so happy with it, we don't want to replace that". The marketplaces are connected to it and the proximity rules live there. The partner agrees to "remove DOM" and integrate D365 with Unicommerce.

**Current flow** (37:55-38:28)
- The order is processed in Unicommerce and the bill is closed there. It then appears in **GoFrugal** (POS/back office) as a normal bill, marked as coming from Unicommerce. GoFrugal is to be replaced and Unicommerce is not.

**Proposed integration** (38:33-40:09)
1. Unicommerce creates the sales order in D365, labelled with its source (Unicommerce / Shopify).
2. The order says **which facility fulfils it**. Unicommerce's decision is used and DOM is not.
3. D365 creates a **transfer order** to that store automatically. It shows at the store POS as Transfer out.
4. When the store ships, D365 **books the invoice automatically**. The partner calls this a custom automation, not standard.

**Inventory latency** (40:09-42:51)
- Today there is about a **1-hour lag** between store sales and online inventory, because a scheduler moves it.
- In D365, online orders are reserved immediately. Store (offline) sales are pulled to HQ every 5-15 minutes ("Product availability" batch job, Distribution schedule screen at 41:15).
- **Kushal's hard requirement:** the same piece must never be sold twice online. "We are bound to the customer to fulfil if we accept."

### 2.9 What the recordings imply about the integration boundary (ERP beside Unicommerce)

| Flow | Direction implied | Evidence |
|---|---|---|
| Item master / catalogue | ERP or PIM → Unicommerce product master. Today it is a bulk upload into Uniware. Channel listing and linking stay inside Unicommerce | uc6 10:15-10:50, 00:00-09:59 |
| Store stock | ERP / POS store stock → Unicommerce facilities (stores are UC facilities), frequently. Today the lag is 1 hour, which is the pain point | uc3 08:30; loy1 40:09-40:30 |
| Channel stock | Unicommerce → marketplaces and Shopify | uc6 08:14-08:57 |
| Orders | Channels → Unicommerce → ERP, with the **fulfilling facility chosen by Unicommerce**, labelled by source channel | loy1 36:40-39:50 |
| Fulfilment at a store | ERP store task (D365 used a transfer order). The store ships | loy1 39:22-40:04 |
| Invoice | When Unicommerce closes the bill, the ERP must carry it as the sale, flagged as from Unicommerce, once (GoFrugal today). D365 proposed booking it automatically on shipment | loy1 37:55-38:28, 39:34-40:09 |
| Returns | Unicommerce receives customer and courier returns (put-away types) → the ERP needs the stock and credit effect | uc3 22:15, 32:30 |
| Purchasing / GRN for the e-com facility | Run in Unicommerce in the training (staging). Who owns it in production is not stated. If the ERP is the stock master, a second GRN in UC would double-count | uc1, uc3 |
| Settlements | **Not shown** in any recording (only "Reconciliations" as a settings menu label) | uc6 00:30 |
| Loyalty | ERP (D365) by originating channel | loy1 26:44-28:02 |

## 3. Gap table

Our ERP = Have / Partial / Missing / Planned (id). The evidence is file:line in the repo unless marked otherwise.

| # | Capability | Vendor reference | Our ERP | Evidence | Note |
|---|---|---|---|---|---|
| 1 | PO create → submit → approve / reject with reason | uc1 00:22, 24:43-25:01 | Have | Maker-checker `engines/approval.go`; approval-gated PO save `public/view-procurement.js:1997-2009` | - |
| 2 | Amend an approved PO, re-approval, link to the earlier version | uc1 00:22; uc3 10:45 "Amended From" | Have (different) | Edit approved PO → Pending Approval with mandatory reason (PURCHA-0085) `public/view-procurement.js:1997-2006`, composer `:2052-2087` | Same PO number with a version and reason, not a new PO with an "amended from" link. |
| 3 | Clone a PO | uc3 33:30 | Have | Copy `public/view-procurement.js:395, 2083-2091` | - |
| 4 | Close a PO's open balance | uc3 33:30 | Have | `ShortClosePurchaseOrder` with reason `engines/po_receipt_policy.go:265-268`; auto-cancel after N days `engines/po_auto_cancel.go:33-34` | - |
| 5 | Print / send PO to vendor | uc1 00:22 | Have | `BuildPurchaseOrderPrint` `engines/purchase_order.go:305`, `MarkPurchaseOrderSent` `:398` | - |
| 6 | Delivery reminder to vendor | uc3 33:30 | Have | "PO Delivery Reminder" N days before due, once per PO `engines/day_limits.go:54-55, 222` | Automatic, better than a button. |
| 7 | Approval reminder (resend approval e-mail) | uc3 33:30 | Partial | Approval alert pushed **once** `engines/web_push.go:51-57`; grep `reminder\|escalat` in `engines/approval*.go`, `notifications.go` = none | No repeat or escalation for a pending approval. |
| 8 | PO expiry date | uc1 00:45 | Have (different) | Tenant-wide `procurement.po_auto_cancel_days` `engines/po_auto_cancel.go:13-34` | Per-PO date not needed. |
| 9 | PO charges, attachments, comments, activity log | uc1 00:45 | Have | `PurchaseOrder.freight_amount` (doctype fields); `public/attachments-panel.js`; audit log | - |
| 10 | Bulk import of approved POs across facilities | uc1 00:15-00:30 | Have | Bulk Purchase Upload (vendors, items, POs approved, GRNs) `internal/server/bulk_purchase.go:23-39` | Exceeds the demo. |
| 11 | Vendor catalogue: vendor SKU, vendor price, bucket size per SKU | uc1 27:00, 17:30 | Partial / Planned (148.6) | Design-level default vendor `('ProductFamily','vendor')` in migrations; grep `vendor_sku\|preferred_vendor\|moq\|order_multiple\|bucket_size` = none in engines/db (only a Myntra pack column header `engines/pim_channel_packs.go:71`) | Vendor price agreements are 148.6. Vendor SKU and order multiple are not tracked. |
| 12 | Reorder config per SKU × facility: threshold, min, max, enabled | uc1 01:15, 10:45 | Partial | `ReorderPointConfig` = item, location, lead_time_days, safety_stock_qty, status `db/migrations/migrations_stage37_10_planning_depth.sql:27-32` | No min / max reorder qty or order multiple. |
| 13 | Reorder trigger counts **open PO + putaway-pending** stock | uc1 09:15 template text | Missing | Suggestions read only `inventory_availability.available` `engines/planning.go:117-147`, `engines/optimization.go:81-114`; `GenerateSupplyProposals` does no netting `engines/supply_proposals.go:80-168` | **Correctness risk:** after a proposal converts to a PO, the next run proposes the same qty again until the GRN posts. |
| 14 | Suggested qty = deficit clamped to [min, max]; fixed qty when min = max | uc1 01:06-08:52 | Missing | `SuggestedQty = reorderPoint - available` `engines/optimization.go:108-114`, `engines/planning.go:139-144` | - |
| 15 | Bulk import of reorder config | uc1 09:15 | Have | `ReorderPointConfig` is a doctype → `BulkImportCSV` `engines/import.go:132` | Gains columns when 12 is extended. |
| 16 | Reorders list, planner edits qty, remainder re-suggested | uc1 17:30-21:35 | Have (different) | SupplyProposal list, one open proposal refreshed per source/item/location `engines/supply_proposals.go:139-150`; `ApproveProposalsInput.Quantities` `:199-206` | - |
| 17 | Re-run automatically on stock events | uc1 23:27-23:54 | Partial | On demand only `internal/server/handlers_supply_proposals.go:31` | No schedule or after-GRN refresh. |
| 18 | Purchase cart → PO per vendor with vendor price known | uc1 21:45-25:01 | Partial | Approve proposals → one Draft PO to a **planner-chosen** vendor, rates at item cost `engines/supply_proposals.go:215-220, 241-248` | No vendor defaulting or per-vendor split. |
| 19 | Manual "add to cart" while browsing the vendor catalogue | uc1 27:00-28:30 | Have (different) | PO composer add-row by design / combination / barcode (147.2) | §5 not adopted as a cart. |
| 20 | Back orders: SKUs with sales waiting for stock, qty waiting / in cart / to raise | uc1 29:30 | Partial / Planned (73.4.2) | Orders on hold ALLOCATION_FAILED `engines/orders.go:45, 291-293` listed per order (142.1 On hold table); per-SKU pegging `engines/planning.go:185` | No SKU-level back-order list feeding purchase. "backorder / preorder" is 73.4.2. |
| 21 | ASN: expected delivery per PO | uc1 33:30 | Have (internal) / Planned (73.4.1, 148.18) | ASN doctype (asn_number, po_id, vendor, expected_date, expected_items, carrier, tracking) `db/migrations/migration.sql`; InboundWork `engines/wms_inbound_work.go:46-102` | Vendor-submitted ASN = supplier portal 73.4.1 / vendor link 148.18. |
| 22 | Gate entry register before GRN (party, parcels, transporter, tracking, vehicle, reference, attachment) | uc3 37:43-38:38 | Partial | GRN carries transporter / vehicle_no / lr_awb_no / package_count `db/migrations/migrations_stage130_16_2_po_terms_grn_logistics.sql`; YardCheckIn is trailer-oriented (trailer_no, dock_door) | No inward-parcel list the GRN desk works from. |
| 23 | GRN clerk sees approved POs only (role separation) | uc3 32:39-33:44 | Have | Open POs = Approved `engines/grn_steps.go:1952`, route `internal/server/routes.go:1319`; per-step roles `grn.steps.*_roles` settings | - |
| 24 | GRN header: vendor invoice no / date, ASN, transport charges, mfg / expiry | uc3 00:15, 01:00 | Have | GRN fields vendor_invoice_no/date, asn_id, invoice amounts (migrations); batch mfg/expiry (Stage 42) | - |
| 25 | Vendor invoice date not before the PO date | uc3 02:09-02:27 | Missing | Only backdate / forward day limits `engines/day_limits.go:60-63, 106`; grep for PO-date comparison = none | §4 item 10. |
| 26 | Line price defaults from master, editable at receipt | uc3 03:02-03:15 | Have | po_rate / invoice_rate / grn_price at Verify `engines/grn_steps.go:28-36` | - |
| 27 | Shortage, excess, mismatch handling | uc3 16:55-19:49 | Have (better) | Excess keep / return with approval `engines/grn_steps.go:36-37`; rejected / short policy D05 `engines/po_receipt_policy.go:12-23`; Purchase Return + debit note (BRAIN Procurement, `db/migrations/migrations_stage57_15_purchase_return.sql`) | UC has no debit / credit note. |
| 28 | QC at receipt: rejected qty + reason; QC pending → complete | uc3 10:00-11:19 | Have (qty-level) | rejected_qty / damaged_qty / rejection_reason `engines/grn_steps.go:184-187, 609-621`; rejected → qc_hold pool `engines/wms_receiving.go:13-40` | - |
| 29 | QC by scanning the individual rejected piece | uc3 10:45 | Partial | Serial register `engines/serial_tracking.go:132`; QC is per line qty | Follows from 32. |
| 30 | QC rejection report / e-mail | uc3 11:42 | Partial | Reports `cat-grn-qc-rejection-exception`, `cat-inbound-qc-exception` (report catalog); no automatic mail | Small. |
| 31 | Return rejects to vendor with a GST document | uc3 19:55-20:29 | Have | Purchase Return against GRN, stock out, auto Debit Note (`engines/purchase_return.go:101`) | - |
| 32 | **Piece-level item codes minted at GRN** (item-level traceability per facility) | uc3 04:39-09:53 | Partial | Barcode per SKU or per receipt date `engines/barcode_policy.go:12-33`; serials must be **typed**, one per accepted unit `engines/serial_tracking.go:420-430` | No auto-generated per-piece code. §4 item 4. |
| 33 | Print labels at GRN, reprint without a new sequence | uc3 04:39-05:00, 35:23 | Have | GRN "Print stickers" per posted part `public/grn-steps.js:382, 587-592`; `cat-barcode-reprint-label-history` | - |
| 34 | PO labels before receipt (vendor pre-labels) | uc3 39:39-40:14 | Partial | SKU barcode issued at SKU creation by default `engines/barcode_policy.go:24-27`, so stickers can be printed any time | No "stickers for this PO" action. |
| 35 | Put-away per shelf, split across shelves, from any source | uc3 21:55-31:34 | Have | PutawayToBin per qty (`blueprint/wms_d365.md` #11, `engines/wms.go`); derived waiting list `engines/wms_inbound_work.go`; `PutawaySerial` `engines/serial_tracking.go:230` | UC's demo could not split. Ours can. |
| 36 | Stock not sellable until put away; QC pending excluded | uc3 16:07, 24:38-25:20 | Have (configurable) | GRN lands in its default location (e.g. Inward pool, not sellable) `engines/inventory.go:210-215`; buckets `engines/stock_locations.go:19-55`; QC hold outside available `engines/inventory.go:474-491` | Client chooses. |
| 37 | Awaiting put-away / put-back pending queue by source | uc3 32:13-32:38 | Partial | Waiting-for-put-away list covers every source (`public/view-warehouse.js:373`, per wms_d365 #10); a cancel after picking goes to the "Channel changes" queue `engines/oms_exception_queues.go:35-37` | No put-back task for picked-then-cancelled pieces. |
| 38 | Shelf master | uc3 27:21-27:49 | Have | Bin master with zone / aisle / rack (wms_d365 #1, `db/migrations/migrations_stage20b_wms_maturity.sql:11-19`) | - |
| 39 | Item details by scanning a code | uc3 37:20-37:41 | Have | `GetSerial` `engines/serial_tracking.go:96`; `engines/item_lookup.go`; `cat-lot-serial-barcode-traceability` | - |
| 40 | Add inventory (stock injection) | uc3 40:16-42:18 | Have | Stock adjustment by bin / lot / serial with preview `engines/inventory_adjustment_ext.go:13-18` | - |
| 41 | Liquidation / goods out on a non-returnable gate pass | uc1 14:32-14:47 | Have | Delivery Challan returnable / non-returnable `engines/delivery_challan.go:14-32` | - |
| 42 | Per-piece register export (status, GRN, shelf, days in warehouse, channel style IDs) | uc3 11:58-16:30 | Partial | `cat-grn-barcode-generation-register`, `cat-stock-aging`, `cat-bin-stock-aging`, `cat-lot-serial-barcode-traceability`; channel ids in `channel_product_mapping.external_product_id` | Per-piece only once 32 exists. |
| 43 | Activity log per PO / GRN | uc3 31:57 | Have | GRN step log header `engines/grn_steps.go:434`; `LogAuditEvent` | - |
| 44 | Inventory buckets: on hand, ATP, committed, good / bad | uc1 11:15 | Have | `computeATS` `engines/inventory.go:483-491`; buckets `engines/stock_locations.go:39-55` | 7-bucket ATP exceeds UC. |
| 45 | **"Not found"**: pieces missing at pick held out of ATP and counted | uc1 11:15 col, 32:12-33:08 | Missing | `ShortPickLine` releases the reservation only `engines/fulfillment_pickpack.go:352-410`; grep short-pick → count / hold in `warehouse_task.go`, `wms_physical_inventory.go`, `wms_holds.go` = none | §4 item 5. |
| 46 | Shelf attributes: allocation on/off, channel sync on/off | uc1 11:00 | Have (different) | Client stock locations with sellable flag, carve-out of available `engines/stock_locations.go:19-55` | - |
| 47 | Cycle counts | uc6 11:00 menu | Have | `engines/wms.go:505, 723` (wms_d365 #26) | - |
| 48 | Facilities incl. stores in one system, facility switch, OMS facility code | uc3 08:30 | Have | `Location.facility_code` `db/migrations/migrations_stage60_2_store_identity.sql:32`; `ResolveFacilityCode` `engines/channel_orders.go:102-105, 243-255` | - |
| 49 | Product master: SKU, name, category, colour, brand, HSN, size, weight; enable / disable; bulk upload | uc6 10:15-10:50 | Have | Item / PIM (BRAIN "Product Information (PIM)"); `BulkImportCSV` | - |
| 50 | Pull channel listings (delta) into an Unlinked queue | uc6 00:00-00:53 | Partial | Shopify lists variants and auto-links by SKU / barcode, unmatched only **counted** `engines/shopify_sync.go:549-583`; SDK has no listing pull `engines/connector_sdk.go:79-87`; unmapped SKUs surface only from orders `engines/channel_sync.go:142` | - |
| 51 | Link / unlink channel product ↔ SKU, singly and in bulk | uc6 01:02-02:08, 07:31 | Partial | One by one from SKU exceptions `public/view-oms.js:371-386` → `UpsertChannelSKUMapping` `engines/channel_sync.go:107`; writers of `channel_product_mapping` include no import | No bulk mapping upload, no unlink screen. |
| 52 | Per-listing inventory sync on/off | uc6 07:41-08:54 | Missing | `SyncChannelInventory` pushes every mapping `engines/channel_sync.go:306-321`; Shopify toggles are channel-wide `engines/shopify_sync.go:53, 70`; grep `sync_enabled\|no_sync\|exclude_from_sync` = none | - |
| 53 | Inventory push to channels, delta, buffer | uc6 08:14 | Have | Changed-only push `engines/shopify_sync.go:660-700`; channel buffer `engines/channel_sync.go:21, 269-272`; worker every 5 min `internal/server/routes.go:81` | - |
| 54 | Price push + price update history | uc6 09:45 | Have (Shopify) | `PushShopifyPrices`, each send logged `engines/shopify_sync.go:586` | Marketplaces: no price push in SDK. |
| 55 | Match competitive prices | uc6 09:45 | Partial | Competitor gap report `engines/market_intelligence.go:124`; harvesting 34.4 open, 34.5 declined | §5. |
| 56 | Channel return facility mapping | uc6 00:30 (menu only) | Partial | `ReturnRequest.return_location` chosen per return `engines/document_numbering.go:73` | No per-channel default store. Small. |
| 57 | Shipping providers, serviceability, shipping rules | uc6 00:30 (menu only) | Have | `CheckCourierServiceability` `engines/marketplace.go:42`; AWB / pickup / rate shop `engines/courier.go:152, 206, 337` | - |
| 58 | Import / export jobs with templates, filters, e-mail on completion | uc1 09:15; uc3 12:00 | Partial | `BulkImportCSV` `engines/import.go:132`; ImportJob runs `public/view-import-jobs.js`; e-mail on completion not found | Small. |
| 59 | Online channel as a stockless store; its orders default to the central warehouse | loy1 22:21-23:01, 29:07-30:15 | Have | Channel location → `PreferredLocation` `engines/channel_sync.go:245`, `engines/shopify_sync.go:895`; "Channel Location" plan `engines/orders.go:262-279` | - |
| 60 | Nearest store with stock by PIN code, else next nearest | loy1 23:04-25:40 | Have (proxy) | Nearest Pincode among stocked locations `engines/sourcing.go:129-184, 261-301` | Distance = numeric PIN difference, not geography. |
| 61 | DOM rules: location priority, max distance, max orders per store, min inventory, offline location, partial orders, hard / soft | loy1 32:30 | Partial | AllocationRule {channel, priority, strategy}: Manual / Highest ATS / Nearest Pincode / Lowest Workload / Oldest Stock / Split `engines/sourcing.go:368-450`; NoSplit per channel `engines/channel_sync.go:20`; per-SKU-location safety stock `engines/inventory.go:483-484`; candidates = every location with stock `engines/sourcing.go:129-131`; grep `ship_from_store\|online_fulfil\|fulfil*_enabled` = none | Missing: eligible stores per channel, store online on/off, daily cap, max distance. |
| 62 | DOM exceptions; re-route when a store rejects | loy1 23:30 | Have | Hold ALLOCATION_FAILED `engines/orders.go:291-293` + On hold table (142.1); Rejected task re-reserves at next node `engines/fulfillment.go:271, 322` | - |
| 63 | Store fulfils an online order (POS "Transfer out") | loy1 32:48-35:48, 39:22 | Have (different) | Order allocated straight to the store's pick task `engines/fulfillment.go:66-90`; scan pick / pack `engines/fulfillment_pickpack.go:227-429`; OMS Pick, Pack, Ship | No transfer order needed. Not inside the till. |
| 64 | Packing slip shows the fulfilling store; invoice booked on shipment | loy1 33:14-35:48, 39:34-40:09 | Have | OMS bill at pack, ERP Sales Invoice posted at hand-over under the OMS number `engines/oms_bill.go:9-24` | - |
| 65 | Loyalty by originating channel | loy1 26:44-30:07 | Missing | See `blueprint/loyalty.md` row 14 | Owned by the loyalty blueprint. |
| 66 | Online orders reserved at once; store sales reach stock with no scheduler lag | loy1 40:09-42:51 | Have | Row-locked `CreateReservation` `engines/inventory.go` (after :491); POS posts to the same stock; channel push ≤ 5 min `internal/server/routes.go:81` | Only for channels we connect directly. |
| 67 | Store-wise availability on the web shop | loy1 28:24-29:00 | Have (Shopify) | Stock per mapped Shopify location `engines/shopify_sync.go:641-660` | - |
| 68 | Unicommerce credentials per account, expiry, disconnect | - | Have | `engines/unicommerce.go:18-48, 223-241`; routes `internal/server/routes.go:1437-1438` | - |
| 69 | **Orders UC → ERP** as SalesOrders, idempotent | loy1 38:33-39:10 | Partial | `POST /api/v1/unicommerce/order` `internal/server/routes.go:1439`, body = channel_order_id, store_code, sku / qty / unit_price only `internal/server/handlers_integrations_admin.go:79-87`; customer "Unicommerce order X", address "Store X 000000" `engines/channel_orders.go:191-197` | No customer, address, tax, source channel or invoice data. |
| 70 | **Honour UC's chosen facility** | loy1 39:04-39:50 | Missing | Intake passes no `PreferredLocation` (`engines/channel_orders.go:191-197`), so the ERP re-sources by its own rules (`engines/orders.go:280`) although the engine supports a channel location (`:262-279`) and facility codes | One-line wiring plus the payload field. |
| 71 | **Inventory ERP → UC** per facility | loy1 40:09-42:51 | Missing (in practice) | `SyncUnicommerceInventory` has no caller (dead code, 103.4 `docs/micro_checklist.md:3667`); the worker only logs "Would push" and marks Dispatched `engines/unicommerce.go:320-334` | ADMIN_GUIDE says it sends (`docs/guides/admin-guide.md:277-279`). |
| 72 | Order status / cancel sync both ways | - | Missing | Only `unicommerce.order.imported`, stubbed `engines/unicommerce.go:335-340` | - |
| 73 | **Invoice UC → ERP** under UC's number, counted once | loy1 37:55-38:28 | Partial | Manual: Sales from Excel with Invoice Ref (142.3, `internal/server/bulk_sales.go`); the own-OMS rule exists `engines/oms_bill.go:9-24` | No automatic intake. |
| 74 | Returns / RTO UC → ERP | uc3 22:15, 32:30 | Missing | grep return in `engines/unicommerce.go` = none | - |
| 75 | Catalogue ERP → UC product master | uc6 10:15-10:50 | Missing | UC is not an `OmnichannelConnector` (no `PublishProduct`) `engines/connector_sdk.go:79-87`; `engines/connector.go:33-45` | - |
| 76 | ERP vs UC inventory reconciliation | - | Partial | Reports `cat-erp-vs-unicommerce-inventory-reconciliation-*` `engines/report_catalog_mis_admin.go:854-867, 1102-1103` read `unicommerce_inventory_sync`, which no live path writes | Always empty today. |
| 77 | Marketplace settlements | not shown | Have (generic) | `engines/settlement_reconciliation.go` | Not in the recordings. |

## 4. Missing / partial pieces, ranked

**1. A real Unicommerce connector for "ERP beside Unicommerce"** (rows 69-76). **Size L.**
- **What it is.** The ERP becomes the stock and finance master and Unicommerce stays the channel OMS.
- **Rules:**
  - Store / warehouse sellable stock (`computeATS`) is pushed per UC facility (`Location.facility_code`). The push is a delta, triggered by any stock change and at most every few minutes. It is never negative and the channel buffer is taken off.
  - A UC order arrives with its UC code, source channel, customer, address, line prices / tax and **facility**. It is reserved at that facility ("Channel Location" plan) and is **never re-sourced** by ERP rules. If that facility lacks stock, the order is held and UC is told.
  - Status changes go back to UC (picked / packed / shipped / cancelled). A UC cancellation before picking releases the reservation. After picking it goes to the existing Channel changes queue.
  - When UC invoices, the ERP posts a Sales Invoice under **UC's invoice number** (mirror of the 88.3 OMS-bill rule), so GST counts the sale once.
  - UC customer returns and RTO create the return and credit note and put the stock into the QC pool.
  - Optionally the ERP pushes item master changes to UC's product master.
- **Why it matters.** Kushal's said outright that they keep Unicommerce (loy1 36:32-37:07). The 1-hour stock lag and double-selling are their stated pain (40:09-42:07). Today the admin screen and the guide say "order push and inventory sync", but nothing is sent.
- **How it fits.** Register UC as an `OmnichannelConnector` (`engines/connector_sdk.go`) so it gets `PushInventory` / `PullOrders` / `PushOrderStatus`, the outbox retries, health, credentials and the 5-minute worker that Shopify already has. Intake goes through `ImportChannelSalesOrder` with `PreferredLocation`. The invoice follows `oms_bill.go`'s "external number, ERP invoice posted" pattern. Retire the stub worker and the dead `SyncUnicommerceInventory` (103.4). Migrations are additive (UC order code / invoice no. columns).
- **Do now, independent of the build:** correct `docs/guides/admin-guide.md:277-279` and the Integrations blurb (`public/view-admin.js:2033`), which claim it sends.
- **[needs decision D1]** Topology: beside UC, or replace UC with our OMS? **Recommended:** build "beside" (the client's stated wish). Our own OMS stays for clients without UC.
- **[D2]** Who issues the customer's GST invoice for UC orders? **Recommended:** UC, with the ERP books under UC's number. A store-fulfilled order is never invoiced twice.
- **[D3]** Who runs e-com purchasing / GRN? **Recommended:** the ERP, with UC fed by the stock push, so there is one GRN, one cost and no double count.
- **Unknown:** UC's actual API was not shown. Read the UC API docs before scoping.

**2. Reorder that nets open supply, with min / max / multiple** (rows 12-14, 17). **Size M.**
- **Rules:**
  - Position = available + inward / QC-pending + approved-not-received PO qty + in-transit transfers, less waiting back orders.
  - Trigger when position < threshold. Suggestion = threshold − position, raised to the min, capped at the max, rounded up to the order multiple. Min = max gives a fixed quantity.
  - A lowered approval leaves the remainder to reappear on the next run.
  - Runs nightly, after each GRN post, and on demand.
- **Why it matters.** Today the next run re-proposes quantities already on order (row 13), which is a real over-buying risk. The max cap is how a jewellery buyer limits capital in high-value designs (uc1 07:17-07:39). The min reflects small vendors who will not make one piece.
- **How it fits.** Additive columns on `ReorderPointConfig` (threshold, min_reorder_qty, max_reorder_qty, order_multiple), still CSV-importable. A sibling of `GetReplenishmentSuggestionsConfigured` (the file's own sibling convention). `GenerateSupplyProposals` stays the output.

**3. Vendor catalogue → one-click PO per vendor** (rows 11, 18). **Size M.**
- **Rules:**
  - Each item (or design) has a default vendor, vendor SKU, vendor price and pack multiple.
  - Approving proposals groups them by default vendor into one Draft PO each, at the vendor price, with the vendor SKU printed on the PO.
  - A proposal with no vendor is listed as "no vendor" (as UC does).
- **Why it matters.** Kushal's vendors are small artisans, and buyers think in the vendor's design code.
- **How it fits.** Fold into 148.6 (vendor price agreements on `PriceListVersion`). Add vendor SKU and pack multiple there rather than a new master. `ApproveSupplyProposals` takes "by default vendor".

**4. Piece-level item codes at GRN (opt-in)** (rows 32, 29, 42). **Size M-L.**
- **Rules:**
  - For an opted-in store or item type, posting a GRN mints one code per accepted piece from a number series and registers each as a serial.
  - Stickers print the piece code. QC rejects by scanning a piece. Put-away, pick and sale scan the piece. Aging and "days in warehouse" are per piece.
  - Reprint never re-mints.
  - Stock with piece codes leaves only by sale, transfer, challan or an approved adjustment naming the pieces.
- **Why it matters.** Kushal's e-com warehouse runs item-level traceability (uc3 08:27-09:37) for aging, FIFO and finding a piece among about 2.5 lakh. Unlabelled vendor goods make GRN-time labelling the norm (03:28-06:11).
- **How it fits.** It extends `serial_tracking.go` (register, PutawaySerial, lifecycle), `barcode_policy.go` (a third mode, "per piece") and the sticker engine. No new doctype.
- **[needs decision D4]** Which stores / items? **Recommended:** per item type with a store switch, default off. Turn it on for the e-com warehouse and 92.5 silver / high-value types.

**5. Short pick → "Not found" hold and a count task** (row 45). **Size S-M.**
- **Rules:**
  - A short pick moves the missing quantity from the bin into a not-found hold, which is out of ATP and so cannot be re-promised.
  - It raises a cycle-count task for that bin and SKU and re-sources or holds the order.
  - The count either restores the stock or writes it off with reason and approval.
- **Why it matters.** Small jewellery pieces get misplaced. UC's back-order story (uc1 32:12-33:08) is exactly this case.
- **How it fits.** `ShortPickLine` plus the existing `hold_qty` / hold codes (`wms_holds.go`), `WarehouseTask` count work, and cycle count posting.

**6. Back-order list that feeds buying** (row 20). **Size S. Planned (73.4.2)**, so build it inside that item.
- **Rules:** per SKU and location, show qty waiting (orders on ALLOCATION_FAILED), qty already on PO, and qty to raise. "Raise" creates supply proposals.
- **How it fits.** Uses pegging (`planning.go:185`), `GenerateSupplyProposals`, and a `ReportDefinition` for the list.

**7. Ship-from-store constraints in sourcing** (row 61). **Size M.**
- **Rules:**
  - A store "fulfils online orders" switch, which can be paused for a day.
  - Eligible locations per channel (Myntra / Shopify from the warehouse; Amazon / AJIO / Flipkart from stores).
  - A max orders per store per day.
  - Max distance in km, from a PIN code → lat / long master instead of the numeric PIN difference.
  - A location priority (warehouse first).
  - Each rule can be hard or soft.
- **How it fits.** Extra AllocationRule fields and a filter inside `qualifyingLocations` (`engines/sourcing.go:129`), which is the one choke point.
- **[needs decision D5]** While UC is in place UC routes, so only item 1's "honour facility" is needed. **Recommended:** build this only when a client routes marketplaces through our OMS.

**8. Inbound gate-entry register** (row 22). **Size S.**
- **Rules:**
  - The gate records each inward parcel or vehicle: party / vendor, PO if known, parcels, transporter, LR / tracking, vehicle, reference and a photo.
  - The GRN is started from the entry, and its fields carry over to the GRN.
  - A list shows entries with no GRN after N hours.
- **How it fits.** A small doctype beside the GRN, reusing the GRN logistics fields and attachments. A `ReportDefinition` gives the "awaiting GRN" list.

**9. Marketplace listings: pull, bulk link, per-listing sync switch** (rows 50-52). **Size M.**
- **Rules:**
  - Pull new channel listings (delta) into an Unlinked queue and auto-link by SKU or barcode.
  - The rest are linked by hand or by CSV. Unlinking sends a listing back.
  - A per-listing "stock sync off" stops pushes for that listing only. Orders still import.
- **How it fits.** An optional `ListListings` capability on the connector SDK (Shopify already has `ListVariants`), `BulkImportCSV` for mappings, and a `sync_enabled` column on `channel_product_mapping` (additive).
- Only needed for channels the ERP connects directly, not through UC.

**10. Small items, each Size S.**
- (a) **Approval reminder:** re-alert after N hours, then escalate to the approver's manager (row 7). Extends the `web_push.go` alert worker.
- (b) **Vendor invoice date earlier than the PO date:** warn and flag as an after-the-fact PO in a report (row 25). **Recommended:** warn rather than block. The approval trail is what matters.
- (c) **"Stickers for this PO" action** for vendor pre-labelling (row 34).
- (d) **Notify on GRN with rejections**, using the existing notification templates (row 30).
- (e) **Default return store per channel** (row 56).
- (f) **E-mail when a long export / import job finishes** (row 58).

## 5. Not worth adopting

- **"Add to Cart / Purchase Cart" shopping metaphor.** Our supply-proposal approval does the same in one step. A second cart layer adds clicks, and the trainer himself called it a small-company tool (uc1 28:33-29:12).
- **"No debit / credit note; amend the PO and send a gate pass" for discrepancies.** Our GRN steps (excess keep / return with approval, D05 short policy, purchase return with debit note) are stronger and GST-correct.
- **15-month retention of piece codes.** A vendor storage limit, not a business rule.
- **Separate put-away documents per source type** (PUTAWAY_GRN_ITEM, _GATEPASS_ITEM, _RECEIVED_RETURNS ...). Our single derived "waiting for put-away" list covers every source with no extra document.
- **A hard 10,000 cap on max reorder qty.** It is arbitrary.
- **"Match competitive prices" auto-repricing** for MRP-led jewellery. It also depends on scraping that 34.5 / 34.6 decided against.
- **D365's transfer order per online order to drive store fulfilment** (loy1 39:22-39:34). Our order allocates straight to the store's pick task, so a transfer adds a document and a GST question with no benefit.
- **DOM as a scheduled batch job.** We allocate synchronously at intake, which is what Kushal's "never double-sell" needs.
- **Free-form "Scripts" in settings.** It conflicts with the lightweight / solid principle. Hooks belong in the extension platform.
- **Online channels modelled as fake stores with virtual warehouses** (loy1 29:07-30:15). Our channel config already carries a location and channel identity.

## 6. Uncertainties

- Whether Kushal's runs Uniware purchasing / GRN in production or only in this staging training is not stated. Row 1 decision D3 depends on it.
- None of the UC API shapes (orders, inventory, invoices, returns) were shown. Item 1 needs UC's API documentation.
- Sale-order, picklist, packing, invoicing, manifest and returns screens of Uniware were **not** demonstrated in these three recordings, so they are not blueprinted here. The ERP's own OMS covers them (`docs/guides/user-guide.md:2171` maps UC's vocabulary onto it).
- The uc3 frames show "Create Label" and "Quality Check" (stand-alone) screens whose purpose the presenter did not know. They are not blueprinted.
