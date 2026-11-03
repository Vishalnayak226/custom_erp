---
doc_id: DOC-PRODUCT-RESEARCH-REC-WMS-INFOR-PWC
title: Recording blueprint - Infor LN warehousing and the PwC warehouse process demos, gap-checked
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

# Blueprint + gap check: Infor LN Warehousing (`infor_wms`) and PwC's D365 warehouse process demo (`pwc_wms1`, `pwc_wms2`)

> Stage 146 (2026-10-31): learned from the user's meeting recordings (`Downloads\Telegram Desktop\Dynamics Loyalty Management Demo 1`). Citations `(key mm:ss)` point into those videos; the machine transcripts and screen OCR were working files and are not kept. Gap rows were checked against the working tree on 2026-10-31. The plan built from these files is `docs/product/recordings-gap-plan-2026-10-31.md`.

Citations: `(inf mm:ss)` = the transcript; `(pwc1 mm:ss)` / `(pwc2 mm:ss)` = the transcript / the transcript; `-f` = a screen frame / OCR block at that time (e.g. `(inf-f 17:00)`). Frame times match transcript times. "wms_d365 #n" = row n of the gap table in `blueprint/wms_d365.md` (the earlier D365 recording, `dyn_erp`). Our-ERP evidence is `file:line` in this repo.

## 1. Sources

- **"Microsoft Teams - Infor Wms"** (57 min). The Teams call is dated 26 Jun 2023 (taskbar clock in every frame). The product is **Infor LN Cloud Edition** (Infor CloudSuite with the Infor OS / Ming.le portal and an "MSL_10.7" bookmark), demonstrating LN's **built-in Warehousing module**. It is **not** the separate Infor WMS product:
  - the warehouse's "WMS Controlled" flag is off (inf-f 29:15);
  - "WMS Interface" in the menu exists to connect an external WMS (inf-f 44:30);
  - the closing line was "these are all part of standard ERP itself, you don't need to buy another WMS" (inf 56:54).

  Sanjay Singh (Infor partner) presented, and an account lead ran the questions. Also present: Nitesh Sharma, Harish Babu, Monali (guest) and Manoj Kapoor. The client was Vishal Nayak for Kushal's. The demo data was Infor's Dutch glass-maker demo (sites Leerdam / Amsterdam / Marinha Grande; warehouses LRFG01, LRRM01, AMFG01).
  - **Only master data, dashboards and blocking screens were shown.** The account lead asked for a PO receipt into a located warehouse, a location-to-location transfer and lot-wise inventory (inf 09:44-10:37). Those were never demonstrated, and neither was any handheld (RF) screen. "Factory Track", Infor's shop-floor/warehouse data-capture app, shows only as an icon in the app menu (inf-f 00:45).
- **"Pwc Process Wms Demo 1 / 2"**. One Google Meet session ("Warehouse Demo", 28 Aug 2023) split into two files of 21 + 31 min; demo 2 starts mid-sentence.
  - **Product:** Microsoft **Dynamics 365 Finance & Operations**, the same Contoso USMF demo as wms_d365 (warehouse 62 to 24), with the Warehouse Management app run in a browser ("web device", pwc1 00:30).
  - **People:** Anjul Bhalla (PwC) presented. A second PwC consultant answered the work-assignment and pricing questions. Ashish Jain, Somnath Basu, Amit Gabber, Abhay Pandey and Vaibhav Raghav Khatri (PwC) were also present. Client side: Vishal Nayak, Harish Babu, Supriya Verma and Monali Alaspure. A sheet "Timeline of Kushals Warehouse P..." was open in the client's browser.
  - **Context:** this was the follow-up to wms_d365 (06 Jul 2023). At the client's request it walks their own flow end to end instead of isolated steps (pwc2 03:53-04:20): PO, handheld receipt, put-away, GRN, invoice and price, PO amendment, store transfer order, transfer pick work.

## 2. Blueprint

### A. Infor LN Warehousing

#### A.1 Platform, homepages, alerts and approvals (generic)
- **Single sign-on.** Infor OS links many apps under one login: LN, ION Desk, Supplier Exchange, Document Management, Birst analytics, ION API and Factory Track (inf 00:10-00:59; inf-f 00:45).
- **Configurable homepages** per role: "LN Inventory Manager", "Sales Manager", Purchase Manager, Finance (inf 01:01-03:17).
  - A context selector picks company, site and one or more warehouses, and can be changed on the fly (inf 01:14-01:43).
  - Standard widgets can be added freely: Inventory Value, Days of Inventory, Potential Stock Shortages, Inventory Aging, expected inventory and supply excess (inf 02:07-02:32; inf-f 02:30), plus Net Sales Invoiced on the Sales Manager page (inf-f 04:45).
- **Alerts and tasks** (inf 03:21-04:35; inf-f 05:45): an inbox with the tabs Alerts, Tasks, Posts, Notifications and Pending Requests.
  - *Alerts* are exception notices to named people, for example "a very high-value purchase order has been raised".
  - *Tasks* are approval-workflow steps, such as three levels of senior management, shown in each approver's own task window.

#### A.2 The three dashboards (generic; the partner's answer to "how will category managers plan stores", inf 40:17-42:08)
1. **Warehouse Manager Dashboard** (inf 04:51-06:30; inf-f 06:15, 41:15-42:00).
   - Filters: Site; Ship Code (Warehouse / Business Partner / Project / Work Center); Date range.
   - Options: Include Planned Orders, Include Finished Orders, Show Figures per Order.
   - **Inbound Open** tiles: Open Order Lines, Lines Overdue, ASN Lines, Received Lines, To be Inspected, To be Put Away.
   - **Outbound Open** tiles: Open Order Lines, Lines Overdue, To be Inspected, To be Picked, Open Shipment Lines, PI Loads/Shipments.
   - Order grid: Order Origin (Transfer (Manual), Project (Manual), Purchase, Sales, Sales Return, Maintenance Sales, JSC Production...), Order, Order Type, Transaction Type (Receipt / Issue / Transfer), Order Date, Order Group, **Blocked**, **Rush Order**.
   - "You can directly complete your activities from here" (inf 06:22).
2. **Warehouse 360** (inf 06:30-07:24; inf-f 06:45-07:30). Pick one warehouse to see:
   - details: WMS Controlled, Handling units in use, counts of Zones / Locations / Items / Stock Points / PI transactions, % Occupied;
   - inventory: Items Inventory, Stock Point Inventory, Consignment Inventory, Quarantine Inventory, Inventory Transactions, Planned Transactions;
   - Cycle Count Orders, Adjustment Orders, **Generate Cycle Counting**, Handling Units, Blocking.
   - The items-inventory grid shows **On Hand, Blocked, Allocated, On Order, Economic Stock** (inf-f 07:00).
3. **Inventory 360 / "Item, Lot and Serial 360"** (inf 07:24-09:04; inf-f 07:45-10:45). Pick one item to see:
   - flags: Location Controlled, Serialized, Lot Controlled;
   - warehouse and site inventory, **Item Lot Inventory** (lot, warehouse, on hand, blocked, allocated, on order, lot date; inf-f 08:45), Item Serial Inventory, Consignment, Quarantine, **Warehouse Inventory Inspections**, Project-pegged inventory;
   - analysis: ABC, Slow-Moving, Economic Order Quantity;
   - planning: Purchase / Warehouse (transfer) / Production order advice, Item Order Plan, Demand Forecast, Generate Orders (TPOP);
   - Item Issue by Period, Inventory Commitments, Inventory Blocking and Valuation.
   - Projects were waved off for the client: "they are mostly into trading ... multiple showrooms" (inf 09:44-10:13).

#### A.3 Warehouse master (generic)
- **Warehouse list:** each warehouse is either location-controlled or not. "Not location controlled means locations are not defined" (inf 11:01-11:13). Warehouse types Normal and Shop Floor (inf-f 06:45).
- **Warehouse form** (inf-f 29:15). Header: Site, Enterprise unit, Use Site Settings.
  - Tabs: General, Relationships, **Locations**, Lead Times, Handling Units, Labels, Kanban, Shipments.
  - General: Warehouse Type, Address, Operational and Financial company, Warehouse Valuation Group (MAUC), Calendar / working hours.
  - Switches: **Locations** (on), **WMS Controlled** (off), Zero Receipt Allowed, Confirm Receipt as Final, Confirm on Scan, Include in Enterprise Planning, **Block During Warehouse Cycle Counting**, **Process Inventory Variances Automatically**, **QM Overrules Warehouse Inbound Order Type**.
- **Locations tab: default locations per warehouse** (inf 29:00-31:34):
  - **Receiving** location: "default location for all items of this warehouse" when a PO is received;
  - **Inspection** location: if good the stock goes back to the good bulk location, if faulty to quarantine;
  - **Quarantine** location: rejections and temporary holds, from which the stock is purchase-returned or written off;
  - **Staging** location: outbound staging before shipping;
  - default location type for inbound advice (Bulk).

#### A.4 Locations (bins) - the core of the demo (generic + client Q&A)
**Purpose:** "One warehouse may have multiple locations, and within the location there can be rows, levels and bins; transfer can happen location to location or bin to bin" (inf 11:44-12:10).

**Location list** (inf-f 16:30, 26:15):
- Locations A001, A002, B001, B002, C001, C002, D001, INSPECTION, PICKING and QUARANTINE.
- Columns: Location, Search Key, **Location Type** (Bulk / Inspection / Pick / Quarantine), Storage Zone, **Fixed Location**, **Location Occupied**, **Location Full**, **Infinite Capacity**.

**Location form** (inf-f 11:30), with tabs Locations / Transactions / Sequences:
- Search Key, Location Type, Address, Storage Zone, **Owner**, **Row / Level / Bin** (three fields), Remark, Text.
- *Characteristics*:
  - **Multi-Item Locations**: off means "one location is dedicated to a specific item only" (inf 13:20-13:38).
  - **Multi-Lot by Item**: "even if there is only one item, do I allow multiple lots of that item in that location"; off means one lot per bin. When a second batch arrives, the system will not put it in the earlier bin (inf 13:41-15:46).
  - Infinite Capacity, and **Location Occupied / Location Full**, which the system sets from inventory transactions (inf 12:43-13:06).
- *Fixed Location*: For Specific Items, **Allow Other Items**.

**Jewellery-specific exchange on multi-lot** (inf 14:01-15:30):
- The client: one design number has several combination IDs, all bought in lots from several vendors.
- The partner: "some precious jewellery businesses buy the same necklace design from five vendors but want **specific bins for the individual vendors** of the same item. Then they turn Multi-Lot by Item off."

**Capacity** (inf-f 16:45-17:00; inf 15:58-18:11, 22:50-25:42):
- Each of **Units / Weight (kg) / Floor Space (cm2) / Volume (cm3)** has Maximum, Available, Expected Occupation Increase and Expected Occupation Decrease. Dimensions are L / W / H (cm). Actions: New, Recalculate.
- **When Exceeding Capacity: Warning at [100] % and Block** (checkbox).
- Worked example (inf 17:09-17:46). A bin holds at most 100, already holds 98, and 5 arrive:
  - **if multi-lot is allowed, put 2 here and 3 into another bin**;
  - if not, **put all 5 into another bin**.
  - Bins are never created automatically. "System will only search for a free bin ... and suggest you put it there" (inf 17:52-18:09).
- Client's question: "one bin can hold three necklaces and one belt - is it product-level?" (inf 18:11-18:28). The answer:
  - capacity on the location, combined with the item's physical size (weight / material / size on the item master, inf-f 26:00; inf 25:34-26:01);
  - on an item-dedicated location, the system "guides you that this is going to be full, move on to the next location" (inf 23:22-23:40).
- Client concern: "our necklaces have no size; will bin management fail without size?" Answer: "depends on the mapping ... part of the project" (inf 26:48-28:01).

**Transactions tab** (inf-f 16:00, 43:00): **Blocked for All Transactions**, or separately **Inbound, Outbound, Transfer (Receipt), Transfer (Issue), Assembly**, each with a Blocked flag and a **Last Status Change** date-time (inf 28:14-28:30).

**Sequences tab:** "sequence of picking list ... storage list and picking list sequencing and priorities can be defined" (inf 28:34-28:48).

**Bulk load** (inf 26:05-26:47): masters are created one by one in the app. At go-live the partner supplies Excel formats and uploads them. After that, new bins / locations / items are maintained by hand.

#### A.5 Fixed locations and replenishment (generic)
- **Fixed Locations** (inf-f 19:30, 25:00): Warehouse, Location, Item, description and Storage Zone. In the demo, C002 holds LR-PK-005 "Tape (roll 25mtr)".
  - "One item can be linked with multiple locations, or one location with multiple items" (inf 19:47-20:05).
  - Further right are **Owner** (location reserved for certain suppliers) and **Inbound Priority**: "first priority to this location, then the next" (inf 20:29-21:10).
  - When the earlier location is full or would exceed its maximum, "the system will automatically pick up another location" (inf 20:05-20:18).
  - The master data drives the suggested bin at inbound (inf 21:14-21:39).
- **Bin minimum / replenishment quantity**: "a bin for a specific item where you can keep 100, but it should not come below 10 because it's a very high-moving item; below 10, the replenishment planning for that bin happens" (inf 21:44-22:14). The menu entry is **Replenishment Location Matrix** (inf-f 11:15).
- Other warehouse references (menu only, inf-f 11:15): Dock Locations, Storage Conditions, WMS Interface Parameters, WMS Reconciliation, Project Pegged Inventory, Financial Warehouses.

#### A.6 Item data by warehouse (generic; one jewellery-relevant setting)
**List** (inf-f 31:45): Item, Status, Use Item Ordering Data by Site, **Inventory Valuation Method per item x warehouse** (MAUC / FIFO / Standard Cost, inf 31:58-32:26), Order System (Planned / Manual), Order Method (Lot for Lot), Supply System.

**General tab** (inf-f 32:30; inf 32:28-36:02):
- Item Warehouse Status, **default Storage Zone for this item in this warehouse**, Default Location Type for Inbound Advice, Package Definition, Order Costs, Inventory Carrying Costs per Year.
- **Inventory Inspection** with **Frequency for Inventory Inspection (days)**. The partner's pitch was "probably very important for you":
  - stock still unsold after N days should be cross-checked "whether the finish or the other things are still good";
  - "on the 180 days the system automatically will generate the inspection order".
  - The client's version: "if we have more than six months of stock, we need to recheck" (inf 33:04-34:36).
  - Reports promised: the standard inventory ageing report, and a report of "which inventory is going to come for inspection in the next 7 or 10 days" (inf 34:43-35:07).
- **Ownership**: Registration Level, Issue Priority (Owned Inventory First), Ownership for Return to Warehouse (Company Owned), Usage at Warehouse Transfer, which is the consignment logic.
- **Settings**:
  - **Exclude from Cycle Counting**, for very low-value items (inf 35:17-35:52);
  - process receipt variances automatically (inf 35:52-36:02);
  - **QM overrules warehouse** (inf 36:02-37:07). Without the quality module, the warehouse manager decides at receipt "this much approved, this much rejected". With it, per item: "for these items the warehouse manager can approve or reject; for certain items quality management is mandated", and the warehouse manager cannot bypass it.

**Replenishment tab** (inf-f 37:45-38:00):
- Order System, Order Method, Order Quantity Increment, Minimum / Maximum / Fixed Order Quantity, **Economic Order Quantity (Calculate EOQ)**, Maximum Inventory, **Reorder Point, Safety Stock**, Season Pattern Safety Stock.
- Actions: Generate Order Advice (SIC), Generate Orders (Kanban / TPOP / OCB), Purchase / Warehouse / Production Order Advice, Print Replenishment List.
- The values can be overridden per site / warehouse (inf 37:15-38:21). The account lead said "Kanban is not important for them, but time-phased order planning (TPOP) would be extremely good" (inf 38:35-38:41).

**Supply tab** (inf-f 38:30-38:45; inf 38:43-40:05): Supply System, **Supply from Warehouse** (Supply Company, Supply Warehouse) or Buy-from / Ship-from Business Partner, Generate Order Advice, Combine Order Advice, Automatically Confirm Advice.
- The account lead's framing for Kushal's: "if I consider all your stores as individual warehouses, their supply-from warehouse is your main warehouse; whatever inventory depletes at stores, the demand comes on the main warehouse and is consolidated; once the PO is received there, you supply the stores". It works across companies too.

#### A.7 Blocking (generic; client's own ask is jewellery-specific)
- **Menu** (inf-f 44:00-45:15):
  - *Inventory Blocking*: Zone/Location Blocking, **Stock Point Blocking**, **Lot Blocking**, Handling Unit Blocking.
  - *Transaction Blocking*: Location / Zone / Lot / Stock Point Blocking by Transaction.
- **Stock Point Blocking form** (inf-f 44:30): Warehouse, Location, Item; tabs **Blocking / Partial Blocking**; columns Lot, Inventory Date, **Reason for Blocking**, **Non-Conforming Material Report**.
- **Location Blocking by Transaction grid** (inf-f 44:00): per location, Inbound / Outbound / Transfer (Receipt) / Transfer (Issue) / Assembly / All, each blocked or not. It lets you block many locations from one screen (inf 44:07-44:26).
- **Rules** (inf 42:37-44:49):
  - shelf-life-expired stock shows as blocked, "until that item has been released by the warehouse manager or some authorised person, you cannot use it";
  - **releasing a block needs an authorised person** ("four types of options are available").
- **Client's actual blocking need** (inf 44:53-45:27): "we have fixed a certain quantity for some store; this store should get this minimum quantity; we should not generate the pick list, should not start picking (until then)". The partner reframed it as "the order has to be shipped complete" and moved to shipping constraints (A.8).

#### A.8 Outbound: shipping constraints, item-customer data, one outbound flow (generic + client Q&A)
- **Items - Sales, Shipping** (inf-f 46:00-46:30): Logistic Company, Site, Warehouse, **Shipping Constraint** = None / **Ship Line Complete** / **Ship Set Complete** / **Ship Line & Cancel** / Not Applicable.
  - The Ordering section has Minimum Rate for Commitment, Minimum Order Quantity and Conformance Reporting, plus "Release to Warehousing".
  - "If the line has to be shipped complete, you cannot do the partial shipping" (inf 46:17-46:42).
- **Item - Sales Business Partner** (inf-f 47:15, 49:45). The same constraint applies per item x customer, "complete for one customer, partial allowed for another" (inf 46:50-47:21). It also carries:
  - sold-to and ship-to BP, effective and expiry dates;
  - **Blocked**: "this particular customer cannot be sold this particular item" (inf 47:29-47:40);
  - delivery schedules and EDI (automotive-style schedule fields).
- **Bin re-arrangement** (inf 47:54-48:50). Client: "after picking, I need to rearrange bins; fast moving should come forward and low moving at the back. Manual button, or an automatic report at the end of the day?" Answer: location-to-location and bin-to-bin transfers exist; standard reports can be put on a **job schedule**, and new reports can be built.
- **Store transfers and online orders from one warehouse** (inf 49:05-51:41; inf-f 52:00-52:45).
  - Client: "we cater to store dispatch from the warehouse and store to store. The plan is to **merge the main warehouse and the e-com warehouse into one platform**, so picking happens for store transfers and online dispatch. How will there be no conflict? Two pickings or one?"
  - Answer: every shipment is linked to its **origin order**, which can be Transfer Outbound or Sales Outbound. **Both can be picked from the same bin**: "one bin has 100 necklaces, 20 go against a transfer outbound and 40 against a sales outbound". Alternatively, sales and transfers use separate bins, "part of configuration ... during the project".
  - Reference customer: "Jiva is using the same model" (inf 51:05-51:16).
  - Warehousing Orders screen: Order Origin, Transaction Type (Issue / Receipt), Order Type (Sales Shipment 210 / Sales Return 200), Ship-from warehouse. Actions: Warehouse Order Reclassification, Handling Unit Transfer. Further order origins: Maintenance Sales, Maintenance Work, Customer Claim, Supplier Claim, JSC/ASC Production, Transfer (Manual).

#### A.9 Integration answers (client-specific)
- **Batch code vs combination ID** (inf 52:35-54:32).
  - The client's ERP bills and transfers on the **batch code**; Unicommerce runs on the **combination ID**.
  - The partner's answer: integration is decided in the project, with Unicommerce kept and its data flowing to the ERP (inf 53:59-54:20). "Is it possible to scan the batch code and the SKU will move?" got "of course yes" (inf 54:24-54:32).
- **POS locations** (inf 54:34-56:04): the client's POS has main / in-transit / inventory / waste / damage locations, and the WMS should update the POS location through an API. Answer: yes, through the Infor ION API exchange.

### B. PwC process demo on D365 (pwc1, pwc2)

#### B.1 Mobile app (generic; overlaps wms_d365 2.4)
- **Main menu:** Inbound, Outbound, **Work List**, Inventory, Change Warehouse, About, Log off (pwc2-f 25:45; pwc1 00:42-01:00).
- **Inbound menu:** **Purchase Receive, Purchase Put-away, Transfer receive, Transfer receive LP, RMA Receive, RMA and Putaway** (pwc2-f 02:15; pwc1 01:01-01:11).
- Receiving screen: "Purchase order item receiving" with Cancel / Exception (pwc1-f 01:45).
- Pick screen: Work, Purchase order: Pick, Loc RECV, Target LP, Item, Qty 5 pcs, with **OK / Cancel / Short pick** (pwc1-f 16:30).

#### B.2 Design vs combination, barcode and batch (client-specific; the most important discussion)
1. **Level** (pwc1 01:47-03:46; pwc2 05:55-06:09). PwC's demo item was configured at **design** level. The client: "this tweak we have to do, our entire warehouse workflow will flow with combination; we don't want the design number". PwC: if the barcode is at combination level, every handheld operation runs at combination level.
2. **Barcode** (pwc1 03:51-05:39).
   - The barcode is internal and generated after receipt.
   - PwC: the system can generate a unique barcode per combination from design ID plus size / colour / configuration, then print and paste it.
   - The client's current barcode carries **colour, polish, size and receipt date**, so "batch code will change" with each receipt date (pwc1 05:22-05:43).
3. **Why the client uses batch codes** (pwc1 07:16-08:50):
   - the ERP bills and transfers by batch code, while Unicommerce works by combination ID;
   - the batch lets them **trace back and find the ageing**, and after ageing "sell the product at a certain discount or **re-polish** the product, because the polish fades";
   - there is no expiry. "If I scan a product, how will I know its ageing? How will it backtrack the batch code against that combination? That is the biggest challenge" (pwc1 08:06-08:21).
4. **PwC's answer** (pwc1 09:16-11:27):
   - keep item + combination barcodes and track the batch as a separate dimension;
   - assign the batch number **automatically at receipt from a number sequence defined on the item**, optionally a different sequence per category;
   - shelf life only if operations must be restricted (e.g. no selling expired stock). For ageing alone, a batch number is enough.
5. **Master data** (pwc1 12:10-14:12).
   - The client: "if we keep our current process we end up with the same system". Today, **creating the PO creates the design number and combination IDs**, because "the PO person is not aware of the design combination".
   - The client asked what the market does. PwC: other clients have a master-data team; PwC gives recommendations in the BRD and takes confirmation.

#### B.3 Inbound: handheld receipt, put-away work, GRN (generic, shown end to end)
1. **PO** 00000177: vendor 1001, warehouse 62, item A0001, qty 10 at 12.00 (pwc1 00:07-00:27).
2. **Handheld Purchase Receive** (pwc1 01:16-15:20): enter the PO, scan or enter the product (this picks up the PO line), and receive 5. The default receive location is RECV. "Work completed".
3. **Put-away work.** Receiving creates a work order "which will update that this is my incoming material ... now where I'm going to place it" (pwc1 15:12-15:37).
   - In the Work List the system has **automatically suggested the bin** (bulk). The operator "will only have to place the product to that particular bin" (pwc1 15:52-16:43).
   - On OK the work status goes **Open to Closed** and stock moves RECV to bulk (pwc1 16:51-17:59; pwc1-f 17:45-20:30). Work 00000177 has work class Purchase, lines Pick RECV and Put bulk, and Blocking reasons.
4. **Product receipt (GRN)** (pwc2 02:57-05:54).
   - The warehouse user hands the documents over, and the GRN is posted from registered quantity: 5 received, 5 remaining.
   - The **vendor invoice number is used as the product receipt number** (INV00102).
   - The journal has Preview/Print, Vouchers, **Correct**, **Cancel** and History (pwc2-f 05:15-05:45).
5. **Performance per worker** (pwc1 18:44-19:01): "if we have multiple persons, track how many put-aways each did per day".

#### B.4 Work distribution (generic, with the client's choice)
- Receipts, transfer receipts and released sales or transfer orders all create **work**, "an instruction given to the warehouse workers" (pwc1 19:27-19:56).
- **Two approaches** (pwc1 20:07-20:30; pwc2 00:00-02:55):
  - **pool**: workers take work themselves. The first to start a work locks it ("Anjul cannot see the work I have already taken"), and **one work cannot be shared by several workers**;
  - **assignment**: a supervisor assigns work. **Split work** breaks a big work into pieces, and **Change user** assigns a work to a worker, who then sees only their own work after logging in on the handheld.
  - Work distribution is manual in the base product.
- **The client chose the pool:** "we want the first one" (pwc1 20:30). "The product will come to the warehouse; the pickers will randomly pick a box and start scanning with their login, and it should start recording" (pwc2 01:16-01:41).
- The work list can be **filtered by work ID, purchase order or transfer order** (pwc2 02:27-02:55).

#### B.5 Vendor invoice, price change, three-way match (generic + client policy)
- **Price is changed on the invoice, not the GRN** (pwc2 06:11-06:23). The invoice line showed qty 5 x 12 = 60, and the price was changed to 64 (pwc2 07:22-08:06).
- **Three-way matching with tolerance** (pwc2 06:28-06:50; pwc2-f 07:30):
  - with a tolerance, a price change within it posts;
  - with zero tolerance, a changed invoice price cannot post;
  - the demo showed "Match status: Not performed" because matching was off.
  - The client wants **5-10%** (pwc2 06:50-06:59).
- **Invoice screen:** Match product receipts, Update match status, Default from product-receipt quantity, On hold, budget check, related PO / product receipt / purchase agreement.

#### B.6 PO price source and PO amendment with approval (generic + client policy)
- **Price sources** (pwc2 08:32-10:09): a vendor **trade agreement / rate contract** ("item A at 100 rupees for this duration") or the **last purchase price**.
- **Client's problem** (pwc2 10:20-12:11). Vendors raise the price for the same design or combination (metal, an added polish), often discovered only at invoice or at GRN.
  - Today the client **creates a new combination for the new price**, so "the store has the same combination with a different price ... a very big issue".
- **PwC's recommendation** (pwc2 12:18-15:19):
  - freeze the price at PO;
  - if the vendor changes it, **amend the PO**, not the invoice;
  - only variable charges (freight, insurance) should change at invoice;
  - "the amended price should go through a different (approval) process" (pwc2 14:28-14:36).
- **Approval on the move** (pwc2 15:21-18:21):
  - the director approves but travels, so "the link can go to mobile"?;
  - PwC: approval by **mobile app or e-mail**, but the D365 mobile app has a **separate licensing policy** (pwc2 16:24-16:51);
  - the client's current workflow tool already e-mails a link that opens the approval (pwc2 17:43-18:10).
- **PO amendment demo** (pwc2 19:00-23:17; pwc2-f 22:15-23:00):
  - re-open the confirmed PO and change the unit price from 12 to 112;
  - without workflow the status returns to Approved. With workflow it goes back to Draft and must be sent for approval;
  - **every confirmation is versioned**: 00000177-1 at 120.00 and 00000177-2 at 1,120.00, "the system keeps track of what changes were done".

#### B.7 Store transfers (client-specific flow on generic D365)
- **Client's flow** (pwc2 23:28-24:42): PO, then GRN, then a **pick list per store**. Of 300 received designs, store 1 gets 50 and store 2 others, with some returned and some new.
- **PwC:** first create a **transfer order per store**, with designs on the lines. Bulk entry is possible (pwc2 24:42-25:16).
- **Client's allocation tool** (pwc2 26:25-28:12):
  - a back-end "separation" tool reads all warehouse and store inventory and runs overnight, computing how much goes to each store;
  - it should **create the transfer orders in Dynamics** via API or an Excel template, "clubbing all designs for store 24 into one transfer order";
  - PwC had no bulk-upload template ready.
- **TO 000009**, warehouse 62 to 24 (pwc2-f 03:45-04:00, 24:15-24:45, 29:00-29:45):
  - list columns: Transfer status (Created / Shipped / Received), From and To warehouse, Ship date, Receipt date;
  - lines: A0001 and A0002, qty 2 each, with Ship now / Receive now, Transit shipment lot ID, Receive lot ID and **Reserve items**;
  - **Reservation form**: Reserved physical, Reserved ordered, On order, Detailed availability by dimension, **Reserve lot**, **Lock reservations**.
- **Release to warehouse creates work USMF-000055**: Work order type **Transfer issue**, priority 50, **Load ID**.
  - Lines: **Pick A0001 from bulk-001 and A0002 from bulk-002, Put 4 to Baydoor**, work class TransfOut.
  - On the handheld the worker filters the open work list by "Transfer issue" and sees Work ID, Order number, Start location bulk-001 and Final put location Baydoor (pwc2 28:24-30:28).

#### B.8 Inventory management menu (shown only, pwc2-f 24:00)
- **Reports:** Inventory picked not delivered, On-hand reports, **Inventory aging**, **Items in quarantine**, Shelf advice, **Warehouse load utilization**, **Workload capacity**, Inventory value, Location label, ABC classification (by location), Counting statistics, Packing material weight / fee, Batch disposition history.
- **Journals and orders:** Counting, Tag counting, Transfer, Inventory ownership change, Planned transfer orders.
- **Periodic tasks:** Release sales order picking, **Release transfer order picking**, **Inventory blocking**.

### C. How Infor, D365 and PwC's process view compare (where the client had to choose)

| Topic | Infor LN (Jun 2023) | D365 (wms_d365, Jul 2023) | PwC process view (Aug 2023) |
|---|---|---|---|
| What was shown | Masters, dashboards, blocking, no transaction and no handheld | Full mobile WMS: receive, put-away, pick, pack, ship | The client's own flow end to end on D365 |
| One SKU per bin | **Per location** "Multi-Item Locations" off, and a fixed location with "Allow Other Items" | **Per location profile** "Allow mixed items = No", plus fixed locations | (not revisited) |
| One lot per bin | Per location "Multi-Lot by Item", with a jewellery reason (vendor-specific bins) | Per profile "Allow mixed batches", which the presenter called pharma-only | Batch from a number sequence, used for ageing |
| Home bins | Several fixed locations per item, ranked by **inbound priority**, overflow to the next | One fixed location per product or variant | System-suggested bin in the put-away work |
| Capacity | Units / weight / floor / volume with **warn %** and block; split-or-move overflow rule | Not shown | Not shown |
| Blocking | By **transaction direction** (inbound / outbound / transfer receipt / transfer issue), by zone / location / lot / stock point / HU, with partial qty and reason, released by an authorised person | Inventory status and work blocking reasons | Inventory blocking (menu only) |
| QC on receipt | Warehouse inspection, or **QM mandatory per item** ("QM overrules warehouse") | Quality associations Before / After on GRN | (not revisited) |
| Ageing | **Re-inspection every N days** per item x warehouse, plus ageing reports | Not shown | Batch = receipt lot for ageing, used for discount or re-polish |
| Store replenishment | Supply-from-warehouse relationship, then warehouse order advice | Loads combining TOs | One TO per store created from the client's **allocation tool** (API / Excel) |
| Transfers and e-com from one DC | One "warehousing order" engine; same or separate bins by configuration | Waves / loads for SO and TO | TO release creates pick work to the bay door |
| Complete shipment | **Shipping constraint** per item and per item x customer | Not shown | Not shown |
| Price changes | Not shown | Trade agreements | Freeze at PO; **amend the PO with approval**; 3-way match 5-10% tolerance |
| Work assignment | Not shown | Work priority | **Pool with lock-on-start** (client's choice); split / change user optional |
| Licensing | "Part of standard ERP, no separate WMS" (inf 56:54) | Advanced WMS is a phase 2 (wms_d365 2.10) | Mobile approvals need a separate licence (pwc2 16:40) |

Both vendors deferred every client-specific question to "mapping during the project" (inf 27:16-28:01, 50:46-50:58, 53:42-53:55; pwc1 11:56-12:04, 13:44-14:12). Neither committed to an answer for the combination-ID-versus-batch-code problem. Our ERP has already answered it (gap #43-44).

## 3. Gap table

Legend: **Our ERP** = Have / Partial / Missing / Planned (id). A row already analysed in wms_d365 says "see wms_d365 #n" and records only what these recordings change.

| # | Capability | Vendor reference | Our ERP | Evidence | Note |
|---|---|---|---|---|---|
| 1 | Role homepages with configurable widgets and a company / site / warehouse context; SSO across apps | inf 00:10-03:17 | Have | Role dashboards with savable layouts and digests `engines/dashboard.go:1-12` | SSO is moot: one application. |
| 2 | Exception alerts to named people (e.g. a high-value PO raised) | inf 03:21-04:08 | Partial | Web-push alerts for inbox approvals and low stock `engines/web_push.go:52,218`; amount-slab approval routing `engines/approval.go` | No general "notify X when Y" rule: grep `AlertRule\|alert_rule\|business alert\|threshold alert` finds none in code or tracker. |
| 3 | Approval tasks in each approver's own inbox | inf 04:09-04:35 | Have | One task inbox `engines/task_inbox.go:1-15`, `ListTaskInbox` `:60`; route `approvals` → `view-inbox.js` (`public/app.js:393`) | - |
| 4 | Warehouse manager dashboard: inbound / outbound open lines, overdue, to inspect, to put away, to pick, loads; rush and blocked orders | inf 04:51-06:30; inf-f 06:15 | Have | Warehouse Cockpit `public/view-warehouse.js:1659-1672`, `GetWarehouseCockpit` `engines/warehouse_cockpit.go:91` (open tasks by type and age, exceptions, waves, inbound due today, throughput, bin utilisation) | Overdue lines per order origin are not one tile. |
| 5 | Warehouse 360: one warehouse's zones, locations, occupancy, quarantine / consignment stock, count and adjustment orders | inf 06:30-07:24; inf-f 06:45-07:30 | Partial | Cockpit per location + bin utilisation `engines/warehouse_cockpit.go:58-62`; Bin Utilization report `engines/report_catalog_mfg_wms.go:798` | No single per-warehouse page with drill-downs. |
| 6 | Item / Lot / Serial 360: one SKU across warehouses (on hand, blocked, allocated, on order); lot, serial, quarantine views; transactions; planning advices | inf 07:24-09:04; inf-f 07:45-10:45 | Partial | Inventory list `public/view-reports.js:423`; network ATS per SKU `engines/availability_network.go:23`; serial inquiry `engines/serial_tracking.go:486`; batch ledger `engines/traceability.go` | All of it exists, but on separate screens. There is no one per-SKU page. |
| 7 | Warehouse parameters: location-controlled, block during cycle count, auto-process variances, confirm receipt as final, QM overrules | inf-f 29:15 | Have (different shape) | Bins optional per location (`engines/wms_bin_allocation.go:36-37`); bin `Counting` state `engines/wms.go:68`; count variances post or go to approval `engines/wms.go:505` | GRN steps fix the receipt behaviour, so the extra switches are not needed. |
| 8 | Default receiving / inspection / quarantine / staging locations per warehouse; inspection then good bulk or quarantine; quarantine then return or scrap | inf 29:00-31:34 | Have (state model) | see wms_d365 #2, #23 | - |
| 9 | Location record: type (Bulk / Pick / Inspection / Quarantine), storage zone, owner, row / level / bin | inf-f 11:30; inf 11:01-12:13 | Partial | see wms_d365 #1, #2; owner breakdown `bin_stock_owner` `engines/wms_bin_allocation.go:31-32` | Infor confirms the missing **level** (shelf) field from wms_d365 #1. |
| 10 | "Multi-Item Locations" off: a bin dedicated to one item | inf-f 11:30; inf 13:20-13:38 | Missing | see wms_d365 #3 | Infor sets it **per location**, which supports wms_d365's recommendation of per-zone with a bin override. |
| 11 | "Multi-Lot by Item" off: one lot per bin, e.g. vendor-specific bins for the same design | inf 13:41-15:46 | Missing | see wms_d365 #4 | **New business reason:** a jewellery peer keeps vendor-specific bins. It moves from "pharma only" to "optional, ask the client". |
| 12 | Bin capacity in units / weight / volume, with the item's physical size | inf-f 16:45-17:00; inf 15:58-25:42 | Have | see wms_d365 #5; capacity reads Item `weight` / `volume` `engines/wms.go:155-170` | Floor space (cm2) is not modelled and not needed. |
| 13 | Capacity **warning at X %** before the hard block | inf-f 17:00 | Partial | `enforceBinCapacity` only refuses `engines/wms.go:155`; grep `warn_pct\|warning_pct\|capacity_warn\|warn_at` hits only manufacturing | - |
| 14 | Overflow rule: fill the bin's remainder and put the rest in the next free bin (or all of it elsewhere under one-lot) | inf 17:09-18:11 | Partial | `SuggestPutawayBin` returns "the single best bin" and drops bins without room `engines/wms_putaway_directed.go:43-47,150-155`; manual split put (wms_d365 #11) | No split suggestion. |
| 15 | Location Occupied / Full kept by the system | inf-f 11:30; inf 12:43-13:06 | Have | Occupancy computed in the cockpit and Bin Utilization report (`engines/warehouse_cockpit.go:58`, `report_catalog_mfg_wms.go:798`) | `Full` itself is a manual operational state. |
| 16 | **Block a location by transaction direction** (inbound / outbound / transfer receipt / transfer issue / all), with last status change | inf-f 16:00, 44:00; inf 28:14-28:30, 42:37-44:26 | Partial | `bin_status` Blocked / Full / Counting refuses put-away (`engines/wms.go:68`), bin move-in (`engines/wms_bin_move.go:131`) **and** picks (`engines/traceability.go:845,889,960`) alike; grep `block_inbound\|inbound_blocked\|block_outbound\|blocked_for` finds none | No inbound-only / outbound-only block. |
| 17 | Picking / storage sequence and priority per location | inf 28:34-28:48 | Partial | Pick list sorted zone > aisle > rack > bin (see wms_d365 #1); grep `pick_sequence\|walk_sequence\|picking_sequence` finds none | No explicit walk-sequence number. |
| 18 | Fixed locations, item to location many-to-many, with **inbound priority** among several bins, owner, and "Allow Other Items" | inf-f 19:30, 11:30; inf 19:47-21:39 | Partial | see wms_d365 #8 | Infor adds several ranked home bins per SKU, and the switch "home bin, but others may use spare room". |
| 19 | Minimum / replenishment qty per fixed (pick-face) bin | inf 21:39-22:14 | Have | BinReplenishmentRule (see wms_d365 #8, #26) | - |
| 20 | Item physical properties (weight, size, material, EAN) | inf-f 26:00 | Have | Item weight / volume used by capacity `engines/wms.go:160-163`; EAN `engines/pim_barcode.go:77,107` | - |
| 21 | Masters bulk-loaded from Excel at go-live, maintained one by one afterwards | inf 26:05-26:47 | Have | `BulkImportCSV` `engines/import.go:132`; client Setup Workbook (Stage 128) | Ours also bulk-loads after go-live. |
| 22 | Item data by warehouse: default storage zone and location type for inbound | inf-f 32:30 | Partial | see wms_d365 #9 | - |
| 23 | **Re-inspection by stock age**: frequency in days per item x warehouse; an inspection order is raised automatically when stock age passes it; a "due in the next 7 / 10 days" list | inf-f 32:30; inf 33:04-35:07 | Missing | grep `inspection_frequency\|reinspect\|re_inspect\|reinspection\|aging_inspection\|repolish` finds none. `InspectionPlan` has no age trigger (`engines/quality_maintenance.go:27-29`). The only interval scheduler spawns MaintenanceOrders (`engines/quality_maintenance.go:443-472`). | Client: ">6 months stock needs rechecking" (inf 34:04-34:14); the polish fades (pwc1 08:32-08:50). **New piece 3.** |
| 24 | Stock ageing report (unsold over N days) | inf 34:43-35:02; pwc2-f 24:00 | Have | `cat-stock-aging`, one row per SKU / location / barcode with Last GRN Date and Last Sale Date (`engines/report_catalog_inventory.go:668`); `cat-bin-stock-aging` (`engines/report_catalog_mfg_wms.go:818`); `cat-ageing-inventory-value` | - |
| 25 | Exclude an item from cycle counting | inf 35:17-35:52 | Partial | ABC plan with per-tier intervals `engines/wms_pack_count.go:162`; grep `exclude_from_cycle\|cycle_count_exclude\|skip_cycle` finds none | - |
| 26 | Receipt and count variances processed automatically or approved | inf 35:52-36:02 | Have | Count reconcile posts or sends to approval `engines/wms.go:505` | - |
| 27 | **QM overrules warehouse**, per item: the warehouse manager accepts / rejects at receipt, or QC is mandatory and cannot be bypassed | inf 36:02-37:07 | Missing | see wms_d365 #21 | Adds a per-item "who decides" switch to wms_d365 piece 3. |
| 28 | Ordering data per item x warehouse: min / max / fixed qty, increment, EOQ, max inventory, reorder point, safety stock | inf-f 37:45-38:00 | Partial | `ReorderPointConfig` per SKU x location `engines/planning.go:73-112`; `GetReplenishmentSuggestionsConfigured` `:112`; grep `EOQ\|economic_order` finds none | EOQ / order-cost theory is not adopted (section 5). |
| 29 | Supply-from-warehouse (store fed by the DC), then a warehouse (transfer) order advice | inf-f 38:30; inf 38:43-40:05 | Have | `GenerateSupplyProposals` `engines/supply_proposals.go:80`. Approval converts to a Draft PO or a **Draft transfer from another location** (`:217-267`; tracker 73.2.18 `[x]`) | The source is chosen at conversion, not from a standing "supply-from" relationship. |
| 30 | Valuation method per item x warehouse | inf 31:58-32:26 | Missing (by design) | see wms_d365 #51 | - |
| 31 | Ownership / consignment (owned stock issued first, ownership on return) | inf-f 32:30 | Partial | Owner breakdown of bin stock `engines/wms_bin_allocation.go:31-32`; `consignment` appears only in challans and e-way bill | No vendor sale-or-return stock, and no client ask for it. |
| 32 | Blocking of stock point / lot / zone / HU, with **partial qty**, **reason**, NCR link, release only by an authorised person | inf-f 44:00-45:15; inf 42:37-44:49 | Have (except direction, #16) | `PlaceHold` on SKU / location / batch with qty and hold code `engines/wms_holds.go:24`; release only through an Approved HoldReleaseRequest (`:1-6,114`); batch Quarantined / Blocked `engines/traceability.go:405`; NCR `engines/quality_inspection.go` | - |
| 33 | Expired stock blocked until released | inf 43:12-43:42 | Have | FEFO skips expired / blocked lots `engines/traceability.go:921-934`; `SweepExpiredBatches` `:1154` | Not used for fashion jewellery. |
| 34 | **Shipping constraint** per item and per item x customer: none / ship line complete / ship set complete / ship line & cancel | inf-f 46:30; inf 45:28-47:21 | Partial | OMS AllocationRule: one node covering every line, or "Split Shipment" (`engines/sourcing.go:101-112`). A TO dispatches exactly its approved qty, all or nothing (`engines/transfer_orders.go:151-154,243-245`). grep `ship_complete\|ship_set\|partial_shipment\|allow_partial` finds none | No per-order fill policy. A TO cannot "dispatch what is there and cancel the rest". Nothing checks fill before picking. **New piece 5.** |
| 35 | Customer x item block ("this customer cannot buy this item") | inf 47:29-47:40 | Missing | grep `customer_item_block\|blocked_items\|restricted_items\|do_not_sell\|not_for_sale` finds none | Not adopted (section 5). |
| 36 | Bin-to-bin moves; re-slotting fast movers forward; a scheduled report | inf 47:54-48:50 | Have | `MoveBinStock` `engines/wms_bin_move.go:47`; `GetSlottingSuggestions` (ABC tier to PickFace) `engines/wms_slotting.go:64`, unslotting `:236`; report schedules `engines/report_schedule_view.go:70` | - |
| 37 | **One outbound flow for store transfers and online orders**, from the same or separate bins | inf 49:05-51:41; inf-f 52:00-52:45 | Partial | see wms_d365 #28 | **Stronger reason:** the client plans to merge the main and e-com warehouses. The bin overstatement is verified in 3.1. |
| 38 | Scan the batch / receipt-date code and resolve it to the combination (SKU) | inf 52:35-54:32; pwc1 08:06-08:21 | Have | Per-receipt-date barcodes recorded in the ItemBarcode register resolve to the SKU (`engines/barcode_policy.go:11-33`; `ResolveItemBySKU` `engines/item_lookup.go:47`; stickers `ReceiptBarcodeFor` `barcode_policy.go:228`) | This is the client's "biggest challenge", already built (Stage 57.7). |
| 39 | Tell the POS when stock changes location (main / in-transit / damage / waste) | inf 54:34-56:04 | Have (native) | POS lives in the same ERP; client stock locations with per-transaction defaults `engines/stock_location_txn_defaults.go:15-24` | No API needed. |
| 40 | Stock **allocated** to open outbound orders, **including transfer orders** | inf-f 07:00 ("Inventory Allocated"); pwc2-f 29:00 (TO "Reserve items / Lock reservations") | Partial | Sales reservations `engines/inventory.go:573`, `engines/order_changes.go:157`. **TO approval reserves nothing.** `ApproveTransferOrderDirect` (`engines/transfer_lifecycle.go:1065`) writes no `inventory_availability`. `computeATS` takes no pending-transfer input (`engines/inventory.go:483`). Availability is first checked at dispatch (`engines/transfer_orders.go:234-245`). Tracker grep "reserve ... transfer" finds none. | An approved store transfer's units stay sellable online until dispatch. **New piece 2.** |
| 41 | Handheld menus: purchase receive, put-away, **transfer receive (+LP)**, **RMA receive and put-away**, work list, change warehouse | pwc2-f 02:15, 25:45; pwc1 00:42-01:11 | Partial | see wms_d365 #12, #15. RF Receiving is ASN-only (`public/view-warehouse.js:972`); the RF shell has put-away / issue of a lot (`public/view-rf-traceability.js:31-60`) | Transfer receipts and return receipts are desktop-only. |
| 42 | Handheld receipt at combination level (barcode at SKU) | pwc1 01:47-03:46; pwc2 05:55-06:09 | Have | see wms_d365 #45 | The client insisted the whole WMS flow runs at combination level. |
| 43 | Barcode built from design + dimensions; printed after receipt; the client's code includes the receipt date | pwc1 03:51-05:43 | Have | Barcode issued at SKU create or at GRN, optionally fresh per receipt date (`engines/barcode_policy.go:11-33`); sticker printing | - |
| 44 | Batch number auto-assigned at receipt from a number series per item or category, for ageing, discount or re-polish | pwc1 05:43-11:27 | Partial | No batch series (grep `auto_batch\|batch_series\|GenerateBatch\|next_batch` and tracker grep "batch number series" find none). The need itself (age by arrival, scan resolves to SKU) is met by #38 + #24. Re-polish via job work `engines/job_work.go:1-12` | A series is not adopted (section 5). |
| 45 | Design and combinations created at PO time, or by a master-data team | pwc1 12:36-14:12 | Have (both) | Designs + combinations created from inside the PO with numbering and a duplicate-combination check (`engines/po_design.go:1-8`); the Item master on its own | A process choice for the client, not a gap. |
| 46 | Receipt creates put-away work with a suggested bin, which then closes; registered qty becomes the GRN | pwc1 14:48-17:59; pwc2 02:57-05:54 | Partial | see wms_d365 #10, #14, #20 | - |
| 47 | Put-aways per worker per day | pwc1 18:44-19:01 | Have | see wms_d365 #41 | - |
| 48 | Work pool: first worker to start **locks** the work; supervisor **assign / change user**; **split work**; workers see only their own work | pwc1 19:27-20:52; pwc2 00:00-02:55 | Partial | see wms_d365 #13. Claim with lock exists (`GetNextTask` FOR UPDATE SKIP LOCKED + assign, `engines/warehouse_task.go:379-420`); reassign through a transition with `assignedTo` (`:241,284`). Split: grep `split` in `warehouse_task.go` finds only string splits. | **The client chose the open pool** (pwc1 20:30; pwc2 01:16-01:41). |
| 49 | Short pick on the handheld | pwc1-f 16:30 | Have | see wms_d365 #29 | - |
| 50 | Partial GRN (5 of 10) under the vendor invoice number; correct / cancel a posted receipt | pwc2 04:43-05:54; pwc2-f 05:45 | Have | see wms_d365 #20; `CorrectGRNStep` `engines/grn_steps_post.go:912`; posted GRN cancellable (release 0.20.0) | - |
| 51 | Three-way match with % tolerance; zero tolerance blocks a changed invoice price | pwc2 06:28-07:48 | Have | `Match3Way` / `MatchVendorInvoice` `engines/vendor_invoice.go:76,103`; setting `procurement.vendor_invoice_tolerance_percent` (default 2%) `engines/settings_definitions.go:223-224` | The client's 5-10% is a setting value. |
| 52 | PO price defaulted from a vendor rate contract or the last purchase price | pwc2 08:32-10:09 | Planned (73.4.1) | see wms_d365 #48; grep `last_purchase_price\|LastPurchasePrice\|last rate` finds nothing in PO code (`engines/purchase_order.go:24-27`: the rate is typed) | Last-price default should be part of 73.4.1. |
| 53 | **PO amendment** after approval: price change needs re-approval; version history | pwc2 13:58-15:19, 19:00-23:17 | Have | Editing an Approved PO needs `amendment_reason` (`engines/transactional_validation.go:760-764`) and resets it to pending (tracker `docs/micro_checklist.md:2143`); term amendment log `engines/po_terms.go:1-19`; report `cat-po-amendment-history` | - |
| 54 | A vendor price change must not create a new combination (SKU) | pwc2 10:20-12:11 | Have (rule) | Cost is the PO line rate into a moving average (`engines/costing.go:23`); duplicate-combination check on design creation (`engines/po_design.go:1-8`) | A process rule for the client's rollout, not a build. |
| 55 | Approvals on the move (mobile / e-mail link) | pwc2 15:21-18:21 | Partial | Web push for inbox approvals `engines/web_push.go:52,218`; responsive inbox. `approval.go` sends no notification; grep `approval_link\|approve_token\|email_approval` finds none | E-mail one-click is not adopted (section 5). |
| 56 | **Allocation file / API produces one TO per store** (the client's overnight allocation tool) | pwc2 26:25-28:24 | Partial | Line upload into **one** TO `public/view-transfers.js:120-129` (template `stock-transfer`); generic `BulkImportCSV` is header-level. grep `store_allocation\|AllocateToStores\|allocation_plan\|transfer suggestion` in code, tracker and research finds none | **New piece 4.** |
| 57 | TO lifecycle: created, shipped, received; ship now / receive now; in transit | pwc2-f 03:45-04:00, 24:15-24:45 | Have | Dispatch / receive with lots, serials and partial receipts (`engines/transfer_orders.go:147`, `engines/transfer_lifecycle.go`; tracker 73.1.7 `[x]`) | - |
| 58 | Reserve a TO line (a specific lot, lock reservations) | pwc2-f 29:00 | Missing | see #40 | **New piece 2.** |
| 59 | Release a TO to the warehouse, giving pick work from bulk bins to the bay door with a load ID; worker filters work by "Transfer issue" | pwc2 28:24-30:28; pwc2-f 29:30-29:45 | Partial | see wms_d365 #28. TOs never get a pick task, and plain SKUs never leave their bins (3.1) | - |
| 60 | Inventory management inquiries (ageing, quarantine items, ABC by location, counting statistics, blocking, location label, packing-material fees) | pwc2-f 24:00 | Have mostly | Ageing #24; holds / qc_hold `engines/wms_holds.go`; ABC plan `engines/wms_pack_count.go:162`; cycle count | Location labels missing (see wms_d365 #40). |

### 3.1 Open question from wms_d365: do plain (untracked) SKUs on a transfer dispatch leave their bins?

**Conclusion: no. Bin stock is left overstated, and nothing else corrects it except a cycle count.** I checked every path:

- **Dispatch:**
  - `DispatchTransferOrderWith` (`engines/transfer_orders.go:155`) takes each line through `dispatchTrackedLineTx` (`:247`). For an item that is neither serial- nor batch-tracked, that function returns at `:400-402` ("An untracked line passes through unchanged", `:344`).
  - The caller then updates only `inventory_availability`: on_hand and available down, in_transit up (`:251-256`).
  - `takeFromBinTx` runs only for serial units that carry a `current_bin` (`:373-376`) and for batch lots (`:431`, `:456`).
- **Packing:** `PackTransferOrder` (`:60-133`) reads no bin.
- **Pick tasks:** bin reservation and consumption (`ReserveTaskBinStockTx` `engines/wms_bin_allocation.go:181`, `ConsumeTaskBinStockTx` `:247`) exist only for **FulfillmentTask** documents (model comment `:13-37`; live statuses `:52`). No TransferOrder ever creates a FulfillmentTask: grep `TransferOrder` in `fulfillment*.go` and `wms_bin_allocation.go` finds none. The only WMS reference to TOs is cross-dock matching on the *inbound* side (`engines/wms_putaway_ext.go:85-107`).
- **Reconciliation:** `BinStockReconciliation` (`engines/wms_bin_allocation.go:434-441`) will show the SKU as "Bins over-state stock", but its own comment says that is "reported, not corrected ... a count settles it". The only fix is a cycle count reconcile (`engines/wms.go:505`) or a manual bin adjustment.
- **Knock-on effects** (from reading the code, not run live):
  - **Put-away refusal.** After N units of a plain SKU leave on a transfer, the next N units received at that DC **cannot be put away**. `PutawayToBin` refuses when `already binned + qty > on_hand` ("putaway qty exceeds unassigned on-hand stock", `engines/wms.go:90-108`). Until someone counts, the warehouse sees fresh stock it cannot bin.
  - **Phantom bin stock in pick allocation.** Sales pick allocation reserves from `bin_stock` (`ReserveTaskBinStockTx`), so a picker can be sent to a bin for units that already left on a transfer, which ends as a short pick.
- **Tracker:** no tracker item covers it. Grep "untracked / plain SKU ... bin" and "transfer ... off bins" find only 73.1.7, which handled tracked lines.

This is a correctness bug. It matters now because the client wants store transfers and online orders picked from one set of bins (inf 49:05-51:41). It is ranked first in section 4.

## 4. New missing / partial pieces, ranked (relative to wms_d365.md)

**1. Every transfer line leaves its bins on dispatch, not just tracked lines** (gap #37, #59, section 3.1). Size **S**.
- **Status:** carried over from wms_d365 piece 6, rule (a). It is verified now, so it becomes its own item.
- **Rules:**
  - (a) At a source location that has bin stock for the SKU, an untracked line takes `qty` off bins in the same transaction and under the same per-(location, SKU) lock as tracked lines.
  - (b) Order: the bins named on the dispatch (when a pick list was used), otherwise pick-walk order (zone > aisle > rack > bin). Skip Blocked / Counting bins and units reserved for a live FulfillmentTask.
  - (c) If the bins hold less than `qty`, take the rest from unbinned stock. That is allowed, because the location's own available check already passed at `:243`.
  - (d) A receive-back or return of a dispatched TO puts nothing back into bins. The units arrive unbinned and are put away normally.
  - (e) A one-off data fix runs `BinStockReconciliation` per tenant after release and lists the over-stated SKUs for a count. It changes no data silently.
- **Why it matters:** a merged DC dispatches store transfers daily. Without this, bins drift further every day, put-away of new receipts is refused, and online picks go to empty bins.
- **Fit:**
  - In the untracked branch of `dispatchTrackedLineTx` (`engines/transfer_orders.go:400`), reuse `takeFromBinTx` (`engines/wms_bin_allocation.go:337`) and the existing pick-order query.
  - No migration is needed.
  - Add a regression test beside `TestTransferLifecycleStage7317`.

**2. Approved transfer orders reserve their stock at the source** (gap #40, #58). Size **S-M**.
- **What:** once a TO is Approved (or released for picking, see the decision), its quantities are held at the source. They stop counting as sellable for online, POS and other transfers. Dispatch consumes the hold; cancel or edit releases it.
- **Rules:**
  - (a) Approve: `reserved += qty` per line at `from_warehouse`, refused when the location's ATS cannot cover it. The message names the SKUs short, with an option to approve the lines that fit and drop the rest (ties into piece 5).
  - (b) Dispatch: `reserved -= approved qty` in the same transaction that moves the stock to in_transit.
  - (c) Cancel, reject, or an amendment that lowers qty: release the difference, once and idempotently.
  - (d) A reservation older than N days with no dispatch shows on the Warehouse Cockpit exceptions. It is never released silently.
  - (e) Batch / serial lines reserve the named or FEFO lots, the same way `allocateFEFO` already skips reserved lots.
- **Why it matters:** the client asked directly: "how will there be no conflict between store transfer picking and online dispatch?" (inf 49:58-50:04). D365 reserves TO lines (pwc2-f 29:00), and Infor shows "Allocated" per item (inf-f 07:00). Today an approved store transfer can be sold online first, and the TO then fails at dispatch with STOCKT-0112.
- **Fit:**
  - `inventory_availability.reserved` already exists and `computeATS` already subtracts it (`engines/inventory.go:483,573`).
  - Hook approval in `ApproveTransferOrderDirect` / the approval decision (`engines/transfer_lifecycle.go:1065`, `engines/approval.go`), dispatch in `transfer_orders.go:251`, and cancel in `transfer_lifecycle.go`.
  - No new table. An additive `reserved_qty` per line in the TO JSON records what was held.
- **Decision needed:** reserve at approval, or only at "release to pick"? *Recommended:* at approval. Our TOs have no separate release step, and approval is already the point where a store is promised the goods.

**3. Re-inspection by stock age (polish check)** (gap #23). Size **M**.
- **What:** a rule "stock of category / item X that has sat N days since receipt must be re-inspected". A daily job raises QualityInspection documents for the aged quantity per location, and a "due in the next 7 days" list shows what is coming.
- **Rules:**
  - (a) Age is measured from the receipt date of the unit's lot or receipt-date barcode where known (Stage 57.7). Otherwise it falls back to the SKU's last GRN date at that location (the `cat-stock-aging` logic).
  - (b) One open inspection per SKU x location x age cohort. Reruns are idempotent.
  - (c) Outcomes reuse the existing dispositions:
    - Pass sets `last_inspected_at`, so the next one is due at last inspected + N;
    - **Re-polish** sends the goods out on a job-work order (`engines/job_work.go`) and returns them as the same SKU;
    - **Markdown** hands off to pricing (out of WMS scope; only flagged);
    - Scrap / RTV via the existing hold and NCR flow.
  - (d) Optionally place a hold (`PlaceHold`) on the aged qty while the inspection is open, decided per rule.
  - (e) Default is off, so nothing changes for existing tenants.
- **Why it matters:** fashion-jewellery plating and polish degrade on the shelf. The client raised it unprompted twice: ">6 months stock must be rechecked" (inf 34:04-34:14) and "polish will remove ... re-polish or discount" (pwc1 07:38-08:50). Infor had this as a standard field.
- **Fit:**
  - Extend `InspectionPlan` additively with `trigger = Stock age`, `age_days` and `applies_to` (Category / Item). This reuses wms_d365 piece 3's `applies_to` field, so build the two together.
  - The scheduler copies the preventive-maintenance pattern (`engines/quality_maintenance.go:443-472`) inside the job runner.
  - The "due soon" list is a `ReportDefinition` / `catWire` report on the `cat-stock-aging` source.
- **Decision needed:**
  - Is the rule per category or per item? *Recommended:* per category with an item override.
  - Should an open inspection block selling? *Recommended:* no hold by default (store stock keeps selling), hold optional per rule.

**4. One allocation upload or API call gives one transfer order per store** (gap #56). Size **S-M**.
- **What:** a file (or API body) with rows `to_location, sku, qty` (plus an optional `from_location`). One Draft TO is created per destination store, with all of its lines. This is the "clubbing all designs for store 24 into one transfer order" the client's overnight allocation tool needs (pwc2 26:25-28:12).
- **Rules:**
  - (a) A dry run first, with per-row errors (unknown SKU or store, qty ≤ 0, same from and to) before anything is created.
  - (b) An **allocation run ID** on every TO it creates. Re-uploading the same run is refused or replayed, never duplicated.
  - (c) The TOs enter the normal approval. A bulk approve on the run approves them together, and piece 2 then reserves the stock per TO, reporting shortfalls per store.
  - (d) A source with less ATS than the run's total for a SKU is flagged in the dry run. The upload is not silently cut.
- **Why it matters:** this is the client's daily store-distribution process. Keying 50-store x 300-design allocations into one TO at a time is not workable.
- **Fit:**
  - `BulkImportCSV`'s dry-run / result shape (`engines/import.go:132`) with a TO-specific grouping step.
  - The existing per-TO line upload template `stock-transfer` (`public/view-transfers.js:120`).
  - Lineage modelled on `source_proposal_ids` (tracker 73.2.18).
  - An API route through `apiMiddleware`, with errors through `writeAPIError`.
- **Decision needed:** should the upload also approve? *Recommended:* no. Create Drafts, then use one bulk-approve action, so maker-checker stays intact.

**5. Fill policy for transfers: ship complete, or ship what is there and close the rest** (gap #34). Size **S-M**.
- **What:** a per-TO policy, defaulted from the destination store:
  - **Complete:** nothing is picked or dispatched until every line can be filled in full. This is the client's "the store should get its minimum quantity before we generate the pick list", inf 44:53-45:21.
  - **Line & cancel:** dispatch the lines or quantities available and close the remainder with a reason.
- **Rules:**
  - (a) The policy is checked at approval / reservation (piece 2), not only at dispatch. A Complete TO that cannot be filled stays approved but un-reserved, flagged "waiting for stock".
  - (b) Line & cancel records the cancelled remainder as a TO balance with a reason code, the same way transfer discrepancies are coded today.
  - (c) An optional minimum-fill % per store: below it the TO waits, above it it ships.
- **Why it matters:** today a TO dispatches exactly its approved qty or fails (`engines/transfer_orders.go:151-154,243-245`). Short stock means editing and re-approving the TO. Infor offered four shipping constraints for this.
- **Fit:** an additive `fill_policy` field on TransferOrder and Location; checks in the approval and dispatch choke points above; a ReasonCode category for cancelled remainders.
- **Decision needed:** the default policy. *Recommended:* "Line & cancel" for replenishment TOs and "Complete" for new-store / launch allocations, chosen per TO with a store default.

**6. Direction-specific bin and zone blocks** (gap #16). Size **S**.
- **What:** besides Blocked, a bin or zone can be **Inbound blocked** (no put-away or move-in; picks continue) or **Outbound blocked** (no picks or move-out; put-away continues). Each carries a reason and the user and time of the last change.
- **Rules:**
  - (a) Inbound blocked is checked in `PutawayToBin` (`engines/wms.go:68`) and `MoveBinStock` (`engines/wms_bin_move.go:131`).
  - (b) Outbound blocked is checked in the pick allocation filters (`engines/traceability.go:845,889,960` and the FulfillmentTask allocation in `wms_bin_allocation.go`) and in move-out.
  - (c) A zone block applies to its bins unless the bin is stricter.
  - (d) Placing a block is immediate. Lifting an Outbound block on a bin with an open NCR goes through the existing hold-release approval.
- **Why it matters:**
  - Re-slotting and bin decommissioning: drain a bin without new stock landing in it, which pairs with `GetUnslottingSuggestions` (`engines/wms_slotting.go:236`).
  - Investigations: freeze picks from a suspect bin while receipts carry on.
- **Fit:** extend `bin_status` values additively (Zone gets the same field), using the same two choke points plus the allocation query. No new doctype.

**7. Put-away overflow split and a capacity warning** (gap #13, #14). Size **S**.
- **What:** when the best bin has less room than the quantity, the suggestion becomes "put 2 in A-01-02, 3 in A-01-03" (Infor's rule, inf 17:09-17:46). Bins also warn at a set % of capacity before the hard block.
- **Rules:**
  - (a) The suggestion fills the highest-ranked bin's headroom first, then the next, never exceeding capacity.
  - (b) Where the SKU's bin is one-lot or one-item (wms_d365 piece 1), the split never mixes; the whole qty goes to a bin that fits.
  - (c) The warning % is a per-bin or per-zone field (default blank, meaning no warning) shown on put-away and on the cockpit's bin utilisation.
- **Fit:** `SuggestPutawayBin` returns a list (`engines/wms_putaway_directed.go:48`), reusing the headroom computed in `loadPutawayCandidates` (`:150-155`). The warning goes in `enforceBinCapacity` (`engines/wms.go:155`) as a non-blocking message.

**8. Per-item cycle-count exclusion** (gap #25). Size **S**, low priority.
- **What:** an Item (or category) flag "exclude from cycle count". The ABC plan and count generation skip it; full physical inventory still counts it.
- **Fit:** an additive Item field, filtered in `GetABCCycleCountPlan` (`engines/wms_pack_count.go:162`).
- *Recommended:* build it only if the client names low-value items (packaging, display props) they do not want counted.

### 4.1 What these recordings change in wms_d365's ranked pieces

- **wms_d365 piece 1 (one-SKU bins + home bin):** Infor adds three refinements worth adopting:
  - **several home bins per SKU with an inbound priority** order (fill the first, overflow to the next);
  - an **"Allow other items"** switch on a home bin (spare room usable by other SKUs);
  - the single-item switch set **per bin**, which confirms "per zone with a bin override".
  - Also consider a **one-lot / one-vendor bin** option, given Infor's vendor-specific-bin example (inf 14:53-15:46). *Recommended:* make it the same switch's third value ("Single item, single lot"), not a separate field.
- **wms_d365 piece 2 (auto serials):** PwC steered towards **batches**, not serials, for this client, and the client's real need is ageing by arrival. Per-receipt-date barcodes already give that (gap #38, #43). *Recommended:* keep auto-serials only for genuinely serial-tracked high-value lines, and drop any batch-number-series work (section 5).
- **wms_d365 piece 3 (quality on receipt):** add Infor's per-item **"QC mandatory / warehouse may decide"** switch (inf 36:02-37:07). Build `InspectionPlan.applies_to` once for both this and new piece 3 (re-inspection by age).
- **wms_d365 piece 4 (work queue):** the client chose the **open pool with lock-on-start** (pwc1 20:30; pwc2 01:16-01:41). Default to that. Supervisor assign / change user stays optional. **Split work** is new: a task for qty N can be split into two tasks that add up to N, each claimable separately. The work list needs filters by source document (PO / TO / order), as shown in pwc2 02:27-02:55.
- **wms_d365 piece 6 (load / transfer consolidation):** the client's own words now support it ("merge main and e-com warehouse ... picking for store transfer and online dispatch", inf 49:05-49:29). Its rule (a) has become new piece 1. The reservation and fill policy (new pieces 2 and 5) are prerequisites for mixing SOs and TOs in one wave.
- **wms_d365 piece 5 (bin code / labels):** Infor's location record has row / **level** / bin plus a picking **sequence** per location. Add a `pick_sequence` integer to the generator and the pick sort, defaulting to code order.

## 5. Not worth adopting

- **Infor OS single sign-on / app launcher, ION API hub:** our ERP is one application with POS, OMS and WMS inside it, so there is nothing to federate. POS locations are native (gap #39).
- **Project-pegged inventory, Kanban, TPOP / SIC order advice, EOQ with order and carrying costs, season-pattern safety stock:** manufacturing and planning theory. The client was confirmed as trading-only (inf 09:44-10:13). Reorder points and supply proposals (`engines/planning.go`, `engines/supply_proposals.go`) cover retail replenishment.
- **Valuation method per item x warehouse:** see wms_d365 section 5. The moving average is deliberate (`engines/costing.go:23`).
- **Floor-space (cm2) capacity and handling-unit / packaging definitions per item-warehouse:** pallet and floor storage. Jewellery goes in bins and pouches.
- **Shelf life / expiry for fashion jewellery:** PwC itself said to use it only if operations must be restricted (pwc1 10:49-11:27). Ageing by arrival plus re-inspection (new piece 3) is the right tool.
- **Batch numbers from a number series per item or category** (PwC's suggestion): the client's batch code exists only to carry the arrival date and drive ageing. Per-receipt-date barcodes (`engines/barcode_policy.go`) already do that and still resolve to the combination, without adding a lot ledger that every sale, transfer and count must then honour.
- **Customer x item sale blocks and item x customer shipping constraints:** a B2B / EDI construct (Infor's screen also carries EDI schedules and CUM models). Retail and online orders do not need it. Revisit only if franchise / wholesale customers get restricted assortments.
- **One-click approval from an e-mail link:** a bearer link that approves money is a phishing and replay surface. Web push to the store app plus the responsive inbox (`engines/web_push.go`, `engines/task_inbox.go`) already lets a travelling director approve after logging in. A plain notification e-mail *without* an approve action would be fine if ever asked.
- **Separate licence-gated mobile approval app** (D365 caveat, pwc2 16:40): our approvals are the same web app.
- **Warehouse-level switches "Zero receipt allowed", "Confirm receipt as final", "Confirm on scan":** our GRN steps (Receive > Verify > Price > Post) already fix this behaviour. Switches would add states without a business ask.
- **Vendor consignment ownership on stock (owned-first issue priority):** no sale-or-return ask from the client. Owner isolation for 3PL already exists if needed (`engines/wms_bin_allocation.go:31-32`).
