---
doc_id: DOC-PRODUCT-RESEARCH-REC-WMS-D365
title: Recording blueprint - Dynamics 365 Advanced Warehouse Management, gap-checked
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

# Blueprint + gap check: Microsoft Dynamics 365 SCM - Advanced Warehouse Management (recording `dyn_erp`)

> Stage 146 (2026-10-31): learned from the user's meeting recordings (`Downloads\Telegram Desktop\Dynamics Loyalty Management Demo 1`). Citations `(key mm:ss)` point into those videos; the machine transcripts and screen OCR were working files and are not kept. Gap rows were checked against the working tree on 2026-10-31. The plan built from these files is `docs/product/recordings-gap-plan-2026-10-31.md`.

Citation format: `(dyn mm:ss)` = transcript the transcript; `(frame mm:ss)` = screen frame / OCR block `t0hMMmSSs`. Our-ERP evidence is `file:line` in this repo.

## 1. Sources

- **Recording:** "Microsoft Dynamics - Discussion 1 - ERP", Google Meet, 06 Jul 2023, 80 min. Deck "D365 Finance & Operations - Warehouse Management.pptx" (9 slides, PwC) followed by a live demo of Dynamics 365 Finance & Operations (cloud, `*.axcloud.dynamics.com`, Contoso demo company **USMF**, site 2, warehouse **24**) plus the **Warehouse Management mobile app** in a desktop window (frame 00:00, 09:45, 12:15).
- **Presenters:** Somnath Basu (PwC, functional consultant, demo); Ashish Jain (PwC, account lead: industry framing and the e-commerce narrative). **Client:** Kushal's (fashion jewellery); Vishal Nayak, Vijay and colleagues asked the questions (frame 10:30).
- **Scope actually shown:** warehouse layout, location types, bin naming, location profile, fixed locations, handheld PO receipt with serials, put-away work, physical on-hand, sales order release, pick work, packing station with containers, shipment confirm, packing list, and the quality setup (quality associations). Slide 7 "Warehouse Maintenance" was **never presented**. It shows only as a thumbnail (frame 01:00). The promised "simpler, non-advanced alternative" was announced (dyn 00:08-00:28) but **never demonstrated**.
- **Demo data:** the client's own items: bangles 146068/146285/150358/158930 and earrings 140634/152574/155681/161144. They carried sell/purchase prices, and one item was flagged "Manual Serial No require" (frame 11:00).

## 2. Blueprint (what the market does, as shown)

### 2.1 Warehouse model and physical layout (generic)

**Purpose:** model the physical warehouse so that every unit's position is known "from receiving to dispatch, end to end" (dyn 04:09).

- **Layout slide** (frame 01:00): Inbound has the Receiving Location with an embedded Quality check area and a Return area. Storage has Aisles 01-03 of racks, Pallet 1-4 bulk floor positions and SCRAP. Outbound has the Dispatch Location.
- **Everything is a location** (dyn 02:27-02:47): receiving, storage and dispatch are all locations. They differ by **attributes/rules** ("whenever I receive it should always come to the receiving location; dispatch goes to the dispatch location", dyn 02:47-03:00).
- **Location types slide** (frame 02:30): Warehouse has the types Receiving, Storage, Packing, Staging and Dispatch. Storage is subdivided into **Handling unit, Bulk and Defective**.
  - Handling unit is the fast-moving area "where you move inventory very quickly" (dyn 04:42-04:55).
  - Bulk is bought in bulk and kept on top shelves or pallets, not handled daily (dyn 04:57-05:07). Defective holds damaged or QC-failed stock (dyn 03:25-03:29, 04:30-04:36).
  - A special zone such as a freezing zone was mentioned and explicitly said not to apply to jewellery (dyn 05:17-05:22). The presenter called the types "completely configured, just one example" (dyn 05:34).
- **Outbound staging** (dyn 03:32-03:51): stock is picked to a packing location, packed, kept in a staging location, then dispatched to the delivery schedule.

**Bin hierarchy and naming** (slide 5, frame 05:45; dyn 05:42-08:19):

- The structure is Warehouse, then Aisle (A), Rack (1, 2), Shelf (1, 2), Bin. Bin codes are `A-01-01-01`, `A-01-01-02`, `A-01-02-01` ... `A-02-02-01`, in the form aisle-rack-shelf-bin.
- **Zone sits above aisle** (dyn 06:26-06:49). A zone holds aisles, but the zone is **not part of the bin code**. Rule of thumb: "keep the aisle number unique across the warehouse" so a bin code is unique even when zones exist (dyn 07:17-07:30). Zones are still defined and stock can be tracked by zone (dyn 07:30-07:36, 08:13).
- Physically, stock is stored either on the floor or on racks, and the smallest unit is the bin (dyn 05:49-06:09).

### 2.2 Location setup screens (generic)

**Locations** (`WHSLocation`, frame 44:30 and 47:15): a grid with the columns **Warehouse, Location, Check digits, Location profile ID, Zone ID, Additional Zone 1/2/3, Work policy**, plus the option "Generate check digits for location".
- Actions: **Generate check digit, On-hand list, Transactions, Location setup wizard, Create inventory movement work**.
- Example: location `FL-001` in warehouse 24 has profile `FLOOR` and zone `FLOOR`.

**Location profile** (`WHSLocationProfile`, frame 44:45; dyn 45:25-47:09). Profile `FLOOR` / "Floor" carries the following settings:

| Field | Value shown | Meaning given in demo |
|---|---|---|
| Location format | FLOOR | the code pattern locations of this profile follow |
| Location type | FLOOR | the location type (receiving / storage / pack / ...) |
| Dock management profile ID | blank | - |
| Use license plate tracking | Yes | LP required at locations of this profile |
| Allow negative inventory | No | - |
| **Allow mixed items** | Yes | **No = once a bin holds one item (SKU/variant), putting a different item there gives an error: "cannot put this item because it is already occupied by a different item"** (dyn 45:38-46:40) |
| Enable location product dimension sp(ecific) | No | - |
| Allow mixed inventory statuses | Yes | - |
| **Allow mixed inventory batches** | Yes | No = two batches of the same item cannot share a bin ("mostly in pharma", dyn 46:44-47:09) |
| Allow cycle counting | Yes | - |
| Generate check digit | No | - |
| Override rules for batch days | No | - |
| Allowed container type group | blank | - |
| Enable item in location | No | - |
| Enable location activity date and time | No | - |
| Enable location status | No | - |
| Enable license plate positioning / Display mobile device LP position | No / No | - |
| FastTabs | "Allowed product dimension mixing", "Dimensions" | - |

**Fixed locations** (`WHSInventFixedLocation`, frame 48:00; dyn 47:30-48:42):
- Two tabs, **Products** and **Product variants**, with the grid Item number, Product name, Site, Warehouse, Location. In the demo data, `A0001` is fixed to `CP-001` and `L0100` to `06A07R1S1B` and `06A07R2S1B`.
- Rule: "whenever I put anything of this item/variant it should always go to this location; the handheld will by default show me this location for put-away" (dyn 48:06-48:42).
- The presenter offered this as **the** answer to the client's "one combination, one bin" ask (dyn 45:13-45:23).

**Location directives** were narrated, not opened (dyn 38:04-39:02). This is the rule set behind the system-suggested put location: "for this item or this type of item, keep it in this bin / this zone / this state of location". Examples given:
- an unusually heavy or bulky item always goes to the floor;
- a category always goes to the handling unit, never to bulk.

The suggestion appears on the worker's put screen.

**Work policy** is a column on Locations. It was not discussed.

### 2.3 Item, tracking and barcode prerequisites (D365 generic; jewellery-specific comments marked)

- **Serial control** (dyn 11:04-12:01): "for jewellery the serial number is very important, each individual piece tracked". Every demo item was serial-active, so serials are required on every purchase. There are **two modes**:
  - **auto-generated from a number sequence** at receipt (registration shows `Create serial numbers`; the auto serial `000108` appears in frame 33:00);
  - **manual capture** one by one at receipt (item 152574: the handheld asked "scan serial number", and `20000` was typed, frame 33:45).
- For a serial item the receipt qty is forced to 1 per scan (dyn 24:43-25:05).
- **Item = design, variants = SKU** (dyn 28:03-32:12). The item code is the design number. Under it are three product dimensions, **style, colour and size** (configuration is also available), and each combination gets its own variant number (the SKU). Example: 3 styles x 2 colours = 6 variants. A **barcode can sit at item level or at variant level** (dyn 30:25).
  - Jewellery note: the client said their current ERP uses the *batch code as barcode*, while the OMS and Shopify use the *combination ID*. They asked to work at combination (SKU) level, and PwC confirmed barcodes at SKU level (dyn 26:09-32:16).
  - PwC also said a jewellery peer keeps its barcode at SKU level (dyn 31:16-31:22).
- **Barcodes** (dyn 23:45-24:16, 32:18-32:33): 1D, 2D and QR are supported, and barcodes must be uploaded or defined beforehand. A barcode may differ from the item number and either can be scanned. The system can generate barcodes or import them, supports several symbologies, and prints labels. **GS1** 2D barcodes were mentioned as the current direction (dyn 25:46-26:04).
- Released product (frame 16:45, 47:45): actions Print shelf labels, Print product labels, Product dimension groups, View/Create trade agreements, Approved vendors.

### 2.4 Mobile warehouse app (generic)

- Microsoft's **Warehouse Management** app is part of the product, and its layout adapts to the device ("any handheld will work", dyn 09:32-10:01).
- **Warehouse workers** get a user ID and password, and every handheld action is recorded against that worker (dyn 10:06-10:22). Login screens appear at frame 09:45 and 10:00.
- The **main menu** (frame 22:45, 49:30) offers Inbound, Outbound, Inventory, Production, Quality, Change Warehouse, Settings, About and Log off.
- The **Work List** (frame 37:30, 59:45) shows a scan box and one card per work item. A card shows Work ID, Order number, Work status, Start location, Final put location and Created date-time.
  - Sort is "Default"; the filter is "All" or a **work class**. Classes listed: Sales orders, Purchase orders, Finished goods put away, Transfer issue, Transfer receipt, Inventory movement, Canceled work, Cycle counting, Replenishment and Return orders (dyn 59:24-59:46).
  - The intent is a paperless instruction to the worker instead of a printed report (dyn 37:13-37:28).
- **Screens are configurable**, not hard-coded (dyn 49:11-49:25).
- **Work priority** (dyn 59:54-61:31; Work grid frame 55:00 shows `Work priority 50`): rank the work so that sales-shipment picking always comes to the top, above put-away. The number is just a sequence (10, 20, 30 ...), and a higher value sorts higher.

### 2.5 Inbound: PO receipt, registration, put-away, GRN (generic, with jewellery data)

Slide 6 (frame 08:45) lists PO receiving and put-away: "Registration of items through handheld device, Palletization of inventory, Put away in storage locations, Put away using handheld based on system suggestions, Manual put away decisions".

1. **Create the PO** (frame 15:00-22:15). The header warehouse defaults the receiving warehouse. To split one PO across warehouses, set the **warehouse per line** instead of in the header (dyn 14:07-14:43). The demo PO was 00000202 to "1004 - Indian Vendor": 146068 x 4 and 152574 x 4. Price comes from the trade agreement (see 2.11).
2. **Confirm the PO** (dyn 21:56-22:09). A confirmed PO is ready to receive. An **approval workflow** before receipt "is possible, not configured here" (dyn 22:12-22:33).
3. **Handheld: Inbound, then Purchase receipt** (frame 23:30-24:45):
   - Scan or enter the PO number; the screen shows the default **receiving location `RECV`**.
   - Scan the item (or its barcode) and enter qty (pre-set to 1 for serial items).
   - Enter or scan the serial, or let it auto-generate.
   - "Work completed" is shown (dyn 23:00-33:41).
4. **Registration in the back office** (frame 33:00, 34:00; `Registration` form):
   - Header: Reference = Purchase order, Number, Ordered, Remainder, Inventory unit.
   - Transaction lines: Receipt status (Ordered / **Registered**), Site, Warehouse, **Location RECV**, **Serial number**, **Inventory status = Available**, **License plate** `000USMF-0000000680`, Quantity.
   - Actions: Add registration line, Create serial numbers, Split, Reset batch disposition code, Batch details, Confirm registration.
5. **License plate (LP)** (dyn 33:53-35:21):
   - An LP is a pallet, tray or pouch identifier. It can be **predefined** (pre-labelled trays) or **system-generated**, and an LP label can be printed. It is **not mandatory**.
   - Client answer: "we keep it in the bin, inside a pouch" (dyn 34:27-34:36).
   - At pick time the handheld asked to "scan the license plate" only because this demo profile used LP tracking. It "can be omitted for you" (dyn 62:00-62:20).
6. **Physical on-hand inventory** (`WHSInventPhysicalOnhand`, frame 35:45-37:00):
   - Columns: Item, Product name, Site, Warehouse, **Location**, **Serial number**, **License plate**, Physical inventory, Unit, CW (catch-weight) quantity. Auto refresh is available.
   - **Display dimensions** dialog (frame 36:45): Product dimensions (Configuration, Size, Color, Style), Storage dimensions (Site, Warehouse, **Location**, **License plate**, **Inventory status**), Tracking dimensions (**Batch number**, **Serial number**, Owner). The item can be seen "at design level or variant level" (dyn 36:31-36:42).
   - The unit shows at `RECV` straight after receipt.
7. **Put-away work** is created automatically on receipt (dyn 37:01-39:12).
   - The work card shows Start location RECV and Final location **FL-001**, a location chosen by the **location directive** (or the fixed location).
   - The worker opens the card, which shows "Pick: item, description, location RECV, qty 1", picks, and is told **"Deliver to put location: FL-001"** (frame 39:30, 40:15).
   - **Available actions: Split Put, Override Loc, Cancel** (frame 40:15). The worker may accept the suggestion, split the quantity across locations, or override to any other location (dyn 39:45-40:22).
   - Completing the work removes the card. On-hand moves RECV to the storage location **in real time** (dyn 40:36-41:52).
8. **Product receipt (GRN)** (dyn 50:01-50:46; frame 50:30, 51:15): this is the "final inventory and financial impact".
   - Choose quantity = **Registered quantity**; the lines fill with what the handheld registered (qty 1.00 each, deliver remainder 3.00).
   - Enter a product receipt number and post. The PO menu also shows **Quality management: Non conformances, Quality orders**.
9. **Client's ask during inbound** (jewellery-specific, dyn 43:22-45:07):
   - "We open the box, check, enter on the handheld this PO and this product received, and store into the bin."
   - "**One bin should have one combination, not more**", and "**bin alignment must happen at the inward stage itself**", because they receive many combinations in one supply.
   - PwC's answer combined two settings: profile "Allow mixed items = No", plus fixed locations per product/variant (dyn 45:13-48:42).

### 2.6 Quality control and quarantine (generic)

- **Narrated flow (Ashish, dyn 70:07-71:52):**
  - **gate entry**: the transporter delivers boxes into the premises (first step);
  - **inbound**: open boxes and check;
  - QC;
  - storage, in **fixed bins** ("I decide this item is in this bin"), **variable bins** or **flexible bins**;
  - picking for one or many sales orders;
  - packing location, with a different process for e-commerce (X), store (Y) or other (Z);
  - manifest printed on the box.
- Client asked for **segregation, QA/QC and in-house storing stages** and for "an item journey through the stages" to be visualised (dyn 69:02-69:21, 76:06-76:32). PwC answered that journeys are designed per item class during implementation (gold differs from artificial jewellery), then SOPs follow (dyn 76:51-77:39).
- **Workflow, notifications and alerts** are three separate concepts (dyn 72:44-73:06). Workflow is the maker-checker approval. A notification goes to a person or a set of people. Alerts are notification-only.
- **Quality associations** (`InventTestAssociationTable`, frame 73:30, 74:15; dyn 73:13-74:24):
  - Reference type: Purchase or Production.
  - Item code: **Table** (one item), **Group** or **All**; Item `M0008`.
  - **Conditions**: Site, Account code (All / one vendor), Account selection, Applicable warehouse type, Resource / Resource code.
  - **Process**: **Event type = Product receipt**, **Execution = Before / After**, Show info = Yes.
  - FastTabs: **Quality order process**, **Specifications**.
  - Rule: with Event = Product receipt and Execution = **Before**, the system **refuses the product receipt (GRN) of that item until it passes the quality order** ("system will flag an error that this item should go through quality check", dyn 73:38-74:18).
- **Quality setup menu** (frame 70:00, 74:30-74:45):
  - Inventory management, Setup, Quality control: **Test instruments, Tests, Test variables, Test groups, Quality associations, Quality groups, Item quality groups, Item sampling, Workers responsible for quality**.
  - Inventory management, Setup, Quality management: **Diagnostic types, Quality charges, Operations, Problem types, Quarantine zones**.
  - Inquiries and periodic tasks: Quality orders, Quality check results, Corrections.
  - Warehouse management, Setup, Work: **Quality check template**. Test instructions were also mentioned (dyn 74:30-74:40).
- **QC fail disposition** (dyn 75:39-76:06): move stock to a **quarantine location** and from there send it back to the vendor, or (in production) back to the floor for rework. A workflow can be built for quality incidents and returns to vendor (dyn 75:11-75:33).

### 2.7 Warehouse maintenance (only partly shown)

- **Slide 7, "Warehouse Maintenance"**, is visible only as a thumbnail (frame 01:00): three boxes, legible as roughly inventory movement, cycle counting and adjustment. It was **not presented**.
- What the recording does show:
  - Locations action **Create inventory movement work** (frame 44:30);
  - the mobile work classes Inventory movement, Cycle counting, Replenishment, Transfer issue/receipt and Return orders (frame 59:45);
  - the menu entries Cycle counting, Replenishment and Cycle count sheet report (frame 63:00).

### 2.8 Outbound: release, wave, pick, pack, ship (generic, with e-commerce narrative)

Slide 8 (frame 43:30) shows the outbound chain:
1. Sales / transfer order creation.
2. Release to warehouse to start picking.
3. Picking based on system-suggested bins, or a manual decision.
4. The picked stock is placed in the packing location.
5. Stock is packed into a container and a shipping label is printed.
6. The container is put away from packing to dispatch.
7. The shipment is released for dispatch.

1. **Sales order** (frame 51:30-53:00):
   - Create SO 000913 for customer IN-CUST (Kolkata), warehouse 24, item 152574 qty 1.
   - The price comes from the trade agreement, which can be customer-specific, generic or location-specific (dyn 52:08-52:23). Confirm the SO.
   - SO list columns include **Release status** (Released).
   - SO menus: Pick and pack (Generate picking list, Picking list registration, Post packing slip), **Load planning workbench**, **Outbound load planning workbench**, Shipment details, Load details.
2. **Loads / load planning workbench** (dyn 53:08-54:29):
   - A **load combines several outbound orders**, for example 3-4 sales orders of one customer, released to the warehouse together. Picking then happens "based on that combination, no isolated activity".
   - The **same applies to transfer orders**: transfer orders for one store or a group of stores are combined into a load and released together.
3. **Release to warehouse** (dyn 54:47-55:09; frame 63:00 toast "batch job **Execute Wave** USMF-000000064 completed"): release runs the wave and **generates work**. The work for SO 000913 has two lines (frame 55:00):
   - **Pick** from `FL-001`;
   - **Put** to `PACK`.

   The Work header carries Work priority 50, Locked by, Work ID USMF-000144, Work status Open, Site, Warehouse, **Work pool ID**, **Target license plate ID** and **Work order type = Sales orders**. Tabs: Overview, General, **Blocking reasons**. Line actions: Transactions, **User actions**, Custom data, **Cancel work line**, Display dimensions, **Quality check results**.
4. **Handheld Sales picking** (dyn 61:36-62:38; frame 62:30): the card shows "pick 1 of 152574 from FL-001, put to PACK". Scan the **license plate** (demo profile only), then confirm "Deliver to put location: PACK, inventory status Available". The system's pick location can be **overridden** to pick elsewhere (dyn 55:43-55:49).
5. **Packing station** (`WHSPack`, frame 63:15-65:30; dyn 62:39-65:24):
   - **Select packing station**: Worker, Site, Warehouse, Location `PACK`, **Packing profile ID** `WH24WorkCreation`.
   - Pack form sections:
     - *Packing information*: License plate or shipment, Shipment ID, Total containers, Container group license plate ID.
     - *Packing station*: Warehouse, Location, Packing profile ID, **Container packing policy** `WH24CloseWithWork`.
     - *Customer information*.
     - *Open containers*: Container ID, Container type, Container packing policy, Container group LP, Total quantity, **Gross weight**, Total items, **Container manifest requirements met**, **Tracking number**.
     - *Item packing*: Identifier, Quantity.
     - *Open lines*: Item, Product name, Qty, Packed qty, Remaining, Unit, **Tote ID**, **Zone ID**.
   - Toolbar: New container, Close container, Containers for shipment, Pack, Containers at station, **Manifest**, Shipment details, Display dimensions.
   - **New container** (frame 63:45): the system generates the Container ID `CONT-000000055`, and the **Container type** is picked from a user-defined list (`Box-Small`; different box sizes per shipment, dyn 63:46-64:06). The container ID prints on the packing slip.
   - Scan or add items one by one into the open container (dyn 64:36-64:42).
   - **Close container** (frame 64:45) records Closed by worker and **Gross weight**, with a **"Get system weight"** button to read a **weighing scale**. The demo entered 1 kg and the client remarked "should be grams" (dyn 64:47-65:20).
6. **Container closed, so work is created** (toast "Work created for container CONT-000000055", frame 65:30). On the handheld, the work list shows a new card PACK to **BAYDOOR**. **Packed Container Picking** then reads "Deliver to put location BAYDOOR, container CONT-000000055" (frame 66:00-66:15; dyn 65:30-66:19). "Bay door" is the dispatch location.
7. **Shipment details** (`WHSShipmentDetails`, frame 66:30-68:00):
   - Shipment ID USMF-000058, **Shipment status Loaded, then Shipped**, **Load ID** USMF-000109, **Wave ID** USMF-000000064, Direction Outbound, Volume, Weight.
   - Dates: Shipping confirmation date-time, Drop-off date-time, Shipment arrival date-time, Drop-off date, Pick-up date. Also Storage dimensions, Wave labels printed, Shipment consolidation policy.
   - Actions: **Confirm shipment, Consolidate shipments, Transfer shipment to existing load / to new load**, Change work location, Compatibility check.
   - Generate: **Bill of lading, Packing slip, Send ASN**. Work: Work details, **Skip non-mandatory work**. Related: Quality orders, Wave labels, Multimodal dangerous goods, Shipper's declaration, Carriage of Merchandise by Road, Bill of lading.
   - Print: **Load list report, Pick list, Packing list**, NAFTA certificate of origin, Certificate of origin, Commercial invoice.
8. **Confirm shipment and packing slip** (dyn 66:26-67:41): inventory leaves the system, and the packing slip is handed to the courier.
   - **Packing list report** (frame 67:00): Package number (= container ID), gross and net weight each, **height, width, length**, quantity, and "clearly state contents of each package" (item and description).
   - Courier service details can be captured (dyn 67:49-67:50).
   - An **SSCC label** can also be printed, designed with the client's logo and fields (dyn 68:06-68:42).
9. **E-commerce narrative** (Ashish, dyn 56:44-58:42, client-relevant):
   - Each logistics provider (LSP) comes to a location, for example "XYZ courier assigned Malad".
   - Select all sales orders for that LSP or area and release them together.
   - Pickers go to the (fixed) bins the system suggests and bring the stock to packing.
   - Packing puts each order's envelope into a box per LSP, and a **manifest** sticker on the box lists the orders for that courier. The shipping label is the manifest handed to the courier.

### 2.9 Warehouse management module map (as shown, not demonstrated, frame 63:00)

- **Workspaces**: Outbound work planning, Outbound work monitoring, Warehouse implementation tasks.
- **Loads**, **Release to warehouse**, **Shipments**, **Outbound waves**.
- **Work**: All work, Work details, Work line details, Work inventory transactions, Deferred put processing tasks, **Work exceptions log**, Work creation history log, Temporary work history, Closed work report, Work report, Planned work details.
- **Packing and containerization**: Containers, Pack, Container contents report, Containerization history, Outbound sorting position assignments.
- **Cycle counting**, **Replenishment**.
- **Inquiries and reports**: On-hand by location, Physical on-hand inventory, **License plate labels**, Work audit data capture, Bill of lading, **Mixed license plate receiving**, Quality check results, Loads report, Load list report, Wave labels, Wave label history, Cycle count sheet report, **Planned cross docking**.
- **Mobile device logs**: Mobile device activity log, Work user sessions, Warehouse app events.
- **Warehouse monitoring reports**: Labor by work transaction type, Labor by user, **Warehouse utilization**, Fixed locations, **Outbound workload visualization**.
- **Pick list reports**, **Warehouse performance analysis**, **Hazardous material shipping documentation**.

### 2.10 Non-advanced alternative and implementation approach (what was said)

- PwC opened by saying D365 also offers a simpler, non-advanced warehouse option that a client can start with and move to advanced WMS later (dyn 00:08-00:28). **It was not shown.**
- Ashish: "WMS should be phase two". The ERP takes 4-5 months and WMS needs 1-2 months more, so the ERP is not delayed by WMS (dyn 76:39-77:52). WMS is "policy governed rather than customisation driven" (dyn 70:00).
- Next step agreed: the client shares use cases and stages. PwC then runs a persona-by-persona demo of 1-2 hours (dyn 79:26-79:49).

### 2.11 Other D365 ERP areas shown

- **Navigation** (dyn 12:37-13:26; frame 12:15-13:15): role dashboards, workspaces, modules menu and a "search for a page" box. The client's logo can be applied.
- **Trade agreements: purchase and sales prices** (dyn 15:32-21:41; frame 17:00-20:30):
  - Prices default onto PO and SO lines from agreements.
  - Fields: **Relation** (Price purch. / Price sales / line discount), **Account code** (All / **Table** = one vendor or customer / Group), Item, **variant dimensions (size, colour, style, configuration)**, Currency INR, **From/To date**, **quantity breaks** (up to 10, 10-100 ...), Price unit, Price charges, Discount % 1 and 2, **Lead time**, Disregard lead time, Include generic currency.
  - Maintained through **price adjustment journals** that are **posted** (Trade agreement journals PDJ-00022/24/35: Price adjustment, Sales price adjustment, Sales discount adjustment).
  - "View trade agreements" on the released product.
  - The vendor + item combination carries its own price (dyn 20:32-20:44).
  - **Sales: store price groups.** Link a group of stores to a price group so each store picks up its own price (dyn 20:44-21:28).
- **Costing method** (dyn 18:25-18:53) is set per **item model group** (standard cost, weighted average ...). Changing it later needs inventory and book closing.
- **PO confirm**, optional **approval workflow**, pro-forma confirmation and "Send for confirmation" (frame 15:00).
- **Product receipt** (GRN) posting, covered in 2.5 step 8.
- **Sales order** create and confirm with agreement prices, covered in 2.8 step 1.
- **Released products**: shelf and product label printing, approved vendors, product dimension groups (frame 16:45, 47:45).

## 3. Gap table

Legend for **Our ERP**: Have / Partial / Missing / Planned (id).

| # | Capability | Vendor reference | Our ERP | Evidence | Note |
|---|---|---|---|---|---|
| 1 | Location hierarchy warehouse > zone > aisle > rack > (shelf) > bin; pick walk sorted by it | slide 5; dyn 05:42-08:19 | Partial | Bin fields zone/aisle/rack/capacity `db/migrations_stage20b_wms_maturity.sql:11-19`; Zone master `db/migrations_stage42_2_5_zone.sql`; pick list sorted zone>aisle>rack>bin `engines/wms.go:317-324` | **No shelf/level or position field.** The bin code is free text with no format. |
| 2 | Location types / roles (receiving, handling unit vs bulk, defective, packing, staging, dispatch, QC, scrap) | slide 4; dyn 02:27-05:34 | Have (different model) | `Bin.bin_type` PickFace/Reserve (= handling unit/bulk) `db/migrations_stage26_5_wms_enterprise.sql:102`; `Zone.zone_type` Storage/Staging/Dock/PickFace/Reserve/QC/Other (zone migration); condition Good/Damaged/QC-Hold/RTV `engines/wms.go:370` | Receiving, pack and dispatch are **states**, not stock-holding bins (see §5). |
| 3 | Location profile: allow/deny **mixed items** per location | frame 44:45; dyn 45:25-46:40 | Missing | `PutawayToBin` checks status, op-state, hazmat and capacity only (`engines/wms.go:60-113`); grep `allow_mixed\|mixed_sku\|single_sku\|mixed_item` finds none in code or SQL | The client's explicit ask: "one bin = one combination". It appears only as a phrase in draft plan `docs/product/wholeops-outsider-plan-2026-10-23.md:294` (M08), not as a tracker item. |
| 4 | Location profile: allow mixed batches / mixed inventory statuses | frame 44:45; dyn 46:44-47:09 | Missing | `bin_stock` PK (bin, sku, condition) lets any mix coexist (`db/migrations_stage20b_wms_maturity.sql:33-41`) | Low value for jewellery (§5). |
| 5 | Location operational status / counting block / capacity | profile flags; frame 44:45 | Have | `bin_status` Available/Blocked/Full/Counting + max qty/weight/volume (`engines/wms.go:68-70,155-180`; `db/migrations_stage42_2_6_bin_capacity.sql:25`) | Exceeds the demo. |
| 6 | Check digits on locations | frame 44:30 | Missing | grep `check_digit` hits only GTIN/stored-value code (`engines/pim_barcode.go:77`) | Not worth adopting (§5). |
| 7 | Location setup wizard (bulk-generate bins from a format) | frame 44:30 | Partial | Generic CSV import of any master `engines/import.go:132`; clone zones and bins between facilities `engines/wms_facility.go:165`; grep `GenerateBins\|bin_range\|wizard` = none | No range or format generator. |
| 8 | **Fixed location per product / per variant** (default put bin) | frame 48:00; dyn 47:30-48:42 | Partial | `BinReplenishmentRule` binds sku to bin with min/max (pick face) `db/...26_5_wms_enterprise.sql`, `engines/wms_putaway_ext.go:450-456`. `SuggestPutawayBin` candidates ignore it (`engines/wms_putaway_directed.go:155-163`). grep `default_bin\|home_bin\|fixed_location` = none | The SKU-to-bin link exists but does not drive put-away or receiving. |
| 9 | Location directives (rule by item/category gives zone or location type for put) | dyn 38:04-39:02 | Partial | `PutawayStrategy` per location with criteria velocity/zone_sequence/capacity/hazmat_temp/batch_consolidation `engines/wms_putaway_directed.go:24-30,116-126` | No item, category or design scoping ("this category always to bulk / floor"). |
| 10 | System-suggested put location shown to the worker; override allowed | frame 39:30-40:15 | Have | "Waiting for put-away" list auto-calls Suggest Bin `public/view-warehouse.js:373-387`; manual bin always allowed `engines/wms_putaway_directed.go:10-18` | The suggestion-versus-actual divergence is not logged. |
| 11 | Split put (part qty to different bins) | frame 40:15 | Have | `PutawayToBin` takes any qty ≤ unbinned and can be called per bin (`engines/wms.go:27,106`) | - |
| 12 | Handheld warehouse app with per-worker login and every action stamped by user | dyn 09:32-10:22 | Have | RF task shell `public/view-rf-traceability.js:4-29`; RF Receiving `public/view-warehouse.js:972`; Mobile Picking; user on every write, e.g. `engines/wms.go:137` | Physical scanner certification is open: **Planned (47.6.6, 73.B.11)**. |
| 13 | **Work list / next work by priority across work types** (sales picks above put-away) | frame 37:30, 55:00, 59:45; dyn 59:54-61:31 | Partial | `WarehouseTask` with priority, queue and assignment `engines/warehouse_task.go:38-81`; `GetNextTask` priority then ageing `engines/warehouse_task.go:379,456-466`; route `internal/server/routes.go:668` | **No screen calls `/wms/tasks/next`** (grep public/ = none). Putaway and pick tasks are logged as Completed after the fact (only VAS creates Pending, `engines/wms_vas.go:40`). There is no default priority per task type. |
| 14 | Work automatically generated on receipt (put) and on release (pick, put to pack) | dyn 37:01-39:12, 54:47-55:20 | Partial | Derived put-away list "on hand minus binned" `engines/wms_inbound_work.go` (header 1-15); wave pick list `engines/wms_picking.go:99` | Equivalent outcome, but not as queued, assignable work items (see #13). |
| 15 | PO receipt on handheld: scan PO, scan item, qty, serial | frame 23:30-34:00 | Partial | RF Receiving against ASN/PO, qty-confirm only; "batch/serial ... stay on the desktop GRN Workbench" `public/view-warehouse.js:972-980` | Serial capture is desktop-only. |
| 16 | **Serial control: auto-generate serials from a number sequence at receipt**, or manual | dyn 11:18-12:01; frame 33:00 | Partial | Manual serials, exactly one per accepted unit `engines/serial_tracking.go:426`, `public/view-procurement.js:3137`; serial register and lifecycle `engines/serial_tracking.go:132,306` | **No auto-generation**: grep `auto.?serial\|serial series\|generate serial` = none. |
| 17 | License plates (optional; system or predefined numbers; LP labels; scan LP at pick) | dyn 33:53-35:32, 62:00-62:20 | Partial | LPN as a breakdown of bin stock `engines/wms_putaway_ext.go:346,410`; deconsolidate `engines/wms_deconsolidation.go:33`; LPN column in bin report `engines/report_catalog_mfg_wms.go:624` | No LP at receipt, no LP number series or labels, no "move whole LP" scan. |
| 18 | Inventory status on stock (Available / blocked) | frame 33:00, 62:30 | Have | `bin_stock.condition` + `TransitionBinStockCondition` `engines/wms.go:370`; qc_hold/damaged buckets `engines/wms_receiving.go:33-36` | - |
| 19 | Physical on-hand by location / LP / serial / status / variant | frame 35:45-37:00 | Have | Bin stock report `engines/report_catalog_mfg_wms.go:490`; serial inquiry `engines/serial_tracking.go:486`; bin reconciliation `engines/wms_bin_allocation.go:441` | - |
| 20 | Product receipt (GRN) posts the registered qty; partial receipts | dyn 50:01-50:46 | Have | GRN steps Receive > Verify > Price > MRP/post `engines/grn_steps.go:1-30`; `PostGRNReceiptPart` `engines/wms_receiving.go:51` | - |
| 21 | **Quality association: item/group/all + vendor, event Product receipt, Before/After, blocks the receipt until QC passes** | frame 73:30-74:15; dyn 73:13-74:24 | Missing | Inspections are recorded manually against a GRN/SO/production source `engines/quality_inspection.go:1-25,114`. `InspectionPlan` is per item with a sample size `engines/quality_maintenance.go:27-29`. grep `qc_required\|inspection_required\|quality association` in receiving/GRN = none | The rejected split at GRN goes to qc_hold (`engines/wms_receiving.go:33`), but nothing *requires* an inspection. |
| 22 | Quality setup: tests, variables, instruments, test groups, item sampling, workers responsible | frame 70:00, 74:45 | Partial | `InspectionPlan` with test parameters JSON + sample size (`InspectionPlan` doctype fields); readings pass/fail `engines/quality_inspection.go:96-114` | No instruments, test-variable outcome lists or responsible-worker register. |
| 23 | QC fail goes to quarantine, then return to vendor or rework; non-conformance | dyn 75:39-76:06 | Have | Disposition hold / rework / concession with a second person `engines/quality_inspection.go:252`; NCR+CAPA `:347-379`; `QuarantineBatch` `engines/quality_maintenance.go:125`; holds `engines/wms_holds.go:24,114`; purchase return `engines/purchase_return.go` | - |
| 24 | Gate entry before inbound | dyn 70:37-70:51 | Have (trailer-oriented) | `CheckInTrailer` / InboundAppointment `engines/wms_inbound_work.go:241`; outbound gate pass `engines/gate_pass.go:43` | For courier parcels this may be heavier than needed. |
| 25 | Stage journey (gate > inbound > QC > storage) visible per receipt | dyn 69:02-69:21, 76:06-76:32 | Partial | InboundWork "every expected ASN with how far it has got" `engines/wms_inbound_work.go:1-15` | No QC stage in that chain; no per-GRN timeline. |
| 26 | Warehouse maintenance: inventory movement, cycle counting, replenishment, adjustment | slide 7 thumb; frame 44:30, 59:45 | Have | `MoveBinStock` `engines/wms_bin_move.go:47`; cycle count reconcile/post `engines/wms.go:505,723`; ABC plan `engines/wms_pack_count.go:162`; bin replenishment `engines/wms_putaway_ext.go:517,582` | - |
| 27 | Release to warehouse > wave > pick list | dyn 52:52-55:20 | Have | `CreateWave` `engines/wms_wave.go:43`; WaveTemplate auto-create `:232`; wave pick list `engines/wms_picking.go:99` | - |
| 28 | **Load planning: combine orders by customer / route / carrier, incl. transfer orders to stores** | dyn 53:08-54:29 | Partial | WaveTemplate matches location/channel/priority only ("carrier/order_type are not matched") `engines/wms_wave.go:225-240`; Manifest = courier+location after pack (`Manifest` doctype); LoadingTask `engines/wms_loading.go:46` | Transfers have their own pack/dispatch (`engines/transfer_orders.go:60`) and **never join a wave**. Bins are taken only for batch/serial-tracked transfer lines (`engines/transfer_orders.go:400-456`). |
| 29 | Pick from the suggested bin, with override to another location | dyn 55:41-55:49 | Partial | Bin reservation per task `engines/wms_bin_allocation.go:181-247`; short pick with reason `engines/fulfillment_pickpack.go:360` | Bin scan-confirm on OMS pick is **Planned (140.12.20, built, not verified)**. No explicit "pick from other bin" action found. |
| 30 | Picked stock to a packing location | frame 55:00 | Have (state) | Picked units counted as `in_pick` `engines/wms.go:96-105` | Not a PACK bin (§5). |
| 31 | Packing station with profile; multiple containers per order; container type; container ID | frame 63:15-64:15 | Have | `PackStation`/`PackTemplate`/`PackingValidationTemplate` `engines/wms_pack_station.go:1-15,127,180`; `ShippingPackage` create/split `engines/shipping_package.go:214,352`; cartonization `engines/wms_pack_count.go:53` | - |
| 32 | Close container with gross weight | frame 64:45 | Have | `UpdateShippingPackage` weight/L/W/H/type `engines/shipping_package.go:289-298`; pack weight check `engines/wms_pack_station.go:127` | - |
| 33 | **Weighing-scale integration ("Get system weight")** | frame 64:45; dyn 65:01-65:12 | Missing | grep `weighing\|scale_weight\|navigator.serial\|WeighScale` = none | Weight is typed by hand. |
| 34 | Container closed > move to dispatch / bay door > load | frame 65:30-66:15 | Have | OutboundWork "invoiced packages not yet on a trailer" `engines/wms_outbound_work.go:1-15`; `ScanCartonToTrailer` `engines/wms_loading.go:110`; pre-ship gate `engines/wms_preship.go:45` | - |
| 35 | Shipment confirm (Loaded > Shipped), courier details | frame 66:30-68:00 | Have | Loading Loaded/Departed `engines/wms_loading.go:185,218`; courier AWB / pickup / tracking `engines/courier.go:152,206,398` | - |
| 36 | Bill of lading, load list, pick list prints | frame 68:00 | Have | BOL print shape `engines/print_shapes.go:34`; Pick List print `engines/print_documents.go:80` | - |
| 37 | **Packing list per package** (package no., gross/net weight, L/W/H, contents) | frame 67:00 | Partial | BOL lists package + order + weight only `engines/print_shapes.go:34-35`; 4x6 shipping label `engines/shipping_label_pdf.go`; grep "packing list\|package contents" = none | No contents-per-box document for the courier or store. |
| 38 | Courier manifest per pickup (e-commerce narrative) | dyn 56:44-58:42 | Have | `Manifest` (courier, location, count, Open/Handed Over); gate pass per vehicle `engines/gate_pass.go:1-15` | - |
| 39 | SSCC / GS1-128 shipping label | dyn 68:18-68:42 | Missing | Code 128 set B only; GS1-128/SSCC explicitly deferred `engines/code128.go:13-16` | §5: only when a partner requires it. |
| 40 | Bin / location labels and LP labels | menu frame 63:00 (License plate labels) | Missing | Code128 encoder exists `engines/code128.go`; grep `bin label\|lpn label\|location label` = none; 140.12.20 assumes "a scanned bin label" (`docs/micro_checklist.md:4442`) | Needed before bin scan-confirm works on the floor. |
| 41 | Labour by user / by work type, warehouse utilisation, work audit | menu frame 63:00 | Have | `GetLaborProductivity` `engines/wms_productivity.go:66`; reports `cat-bin-utilization-condition-report`, `cat-rf-mobile-picking-productivity`, `cat-dock-door-utilization` `engines/report_catalog_mfg_wms.go:798-827` | - |
| 42 | Cross-docking (planned) | menu frame 63:00 | Have | `CrossDockPutaway`/`PlannedCrossDockPutaway` `engines/wms_putaway_ext.go:131,228`; match on receipt `engines/wms_gaps.go:236` | - |
| 43 | Outbound sorting positions | menu frame 63:00 | Have | `AssignWaveToSortSlots` `engines/wms_gaps.go:159` | - |
| 44 | Configurable mobile screens / menus | dyn 49:11-49:25 | Missing | RF screens are code (`public/view-rf-traceability.js:33-54`) | §5. |
| 45 | Item = design, variants (style/colour/size) = SKU; barcode at SKU (or design) level; label print; GS1 GTIN | dyn 26:09-32:33 | Have | Design = ProductFamily, combination = Item `engines/po_design.go:1-8`; EAN/GTIN check digit and generation `engines/pim_barcode.go:77,107` | - |
| 46 | Workflow approval on PO; notifications; alerts | dyn 22:12-22:33, 72:44-73:06 | Have | PO amount tiers `db/migration.sql:590`; maker-checker engine `engines/approval.go` | - |
| 47 | PO warehouse per line (one PO received into several warehouses) | dyn 14:07-14:43 | Missing | `PurchaseOrder.location` / `target_warehouse` are header fields (doctype fields) | §5: receive centrally, then transfer or cross-dock. |
| 48 | Purchase trade agreements (vendor x item x variant, date range, qty breaks, lead time, posted journal) | frame 17:00-20:30 | Planned (73.4.1) | `docs/micro_checklist.md:1917` "blanket / rate contracts" | The PO rate is typed by hand (`engines/purchase_order.go:24-27`). |
| 49 | Sales price agreements: effective-dated, customer-specific | dyn 20:44-21:28, 52:08-52:23 | Have | `PriceListVersion` effective-dated with approval; customer contract list > default list `engines/pos_quote.go:28-60` | - |
| 50 | Store-group price groups | dyn 20:44-21:28 | Missing | Price sources are override / customer contract / tenant default / item price / MRP only `engines/pos_quote.go:41-60` | Low value under MRP retail (§5). |
| 51 | Costing method per item group, locked after use | dyn 18:25-18:53 | Missing (by design) | One company-wide moving average `engines/costing.go:23` | §5. |

## 4. Missing / partial pieces, ranked

**1. One-SKU bins and a home bin per combination, set at inward** (Gap #3, #8, part of #9). Size **S-M**.
- **What:** a bin (or a zone of bins) can be marked "single item". Each combination (SKU) can have a home bin that receiving and put-away go to by default.
- **Rules:**
  - (a) A bin marked single-item refuses a put-away or bin move of SKU B while it holds any qty of SKU A, with a clear message naming both.
  - (b) A SKU's home bin is the first suggestion at put-away, and the GRN / RF Receiving line shows it ("bin alignment at inward").
  - (c) If the home bin is full, Blocked or Counting, fall back to the existing strategy and say why.
  - (d) A bin left empty after a single-SKU rule is free for any SKU again, unless it is that SKU's home bin.
- **Why it matters:** the client asked for exactly this (dyn 43:42-45:07). Fashion jewellery is small, high-count and visually similar, and one combination per bin is what makes a picker's "bin + barcode" check foolproof.
- **Fit:**
  - Additive field `Bin.allow_mixed_items` (Yes/No, default Yes), optionally defaulted from `Zone`.
  - Enforce once inside `PutawayToBin` (`engines/wms.go:27`) and `MoveBinStock` (`engines/wms_bin_move.go:47`), the two choke points that write `bin_stock`. `TransitionBinStockCondition` keeps the same SKU and so needs no check.
  - Home bin: reuse `BinReplenishmentRule` (sku+bin already) with a new `trigger_type` value or an `is_home` flag. Do not add a new doctype.
  - Add a `fixed_location` token to `putawayCriteriaWeights` (`engines/wms_putaway_directed.go:24`) that ranks the home bin first.
  - Mass setup through `BulkImportCSV`.
- **Decision needed:** set "single item" per Bin, per Zone or per bin_type? *Recommended:* per Zone, with a Bin override, defaulting Yes = mixed allowed, so nothing changes for existing tenants.

**2. Auto-generated serial / tag numbers at receipt, captured on the handheld** (Gap #16, #15). Size **S-M**.
- **What:** for serial-tracked items, Receive offers "Generate serials" from a number series (per item category or tenant) instead of typing one per piece. RF Receiving can also scan or generate serials, not only desktop GRN.
- **Rules:**
  - Generated serials are unique tenant-wide and never reused, even after cancel.
  - Exactly one serial per accepted unit; the existing rule `engines/serial_tracking.go:426` stays the gate.
  - Print a tag label per serial in the same step.
  - Manual and generated serials can mix on one line.
- **Why it matters:** PwC led with "for jewellery the serial number is very important" (dyn 11:04). Typing a serial for every piece of a 200-piece receipt is the bottleneck.
- **Fit:**
  - Number series via `engines/document_numbering.go`; an additive `serial_series` setting.
  - Generation inside the GRN line builder (`public/view-procurement.js:3137`) and RF Receiving (`public/view-warehouse.js:972`).
  - Labels through the sticker engine / QZ path.
- **Decision needed:** which items are serial-tracked at all. *Recommended:* only high-value categories (92.5 silver, gold-plated premium); fashion lines stay SKU-barcode only. The client's current practice is SKU / combination barcodes (dyn 26:09-26:44).

**3. Quality-on-receipt rule (quality association)** (Gap #21, #25). Size **M**.
- **What:** a rule that says "receipts of item / category / vendor X need an inspection".
- **Rules:**
  - (a) A match on GRN posting auto-creates a QualityInspection (source = GRN) with the plan's sample size.
  - (b) **Before** mode: the matched qty posts to `qc_hold`, not `available`, until the inspection passes. Our GRN never refuses the receipt; it holds it, which is safer than D365's "cannot post".
  - (c) A failed inspection uses the existing dispositions (hold / rework / concession with a second person) and raises an NCR.
  - (d) **After** mode: stock is available immediately and the inspection is a follow-up.
  - (e) Released qty moves `qc_hold` to `available` once, idempotently.
- **Why it matters:** the client named segregation and QA/QC as stages before storage (dyn 69:02-71:34). Today QC depends on someone remembering to record an inspection.
- **Fit:**
  - Extend `InspectionPlan` additively with `applies_to` (Item / Category / Vendor / All), `trigger` (GRN) and `mode` (Before / After). Resolve it inside `PostGRNReceiptPart` (`engines/wms_receiving.go:51`), the single receipt choke point.
  - The hold release reuses the existing hold / approval path (`engines/wms_holds.go:114`).
  - Add a "QC" stage to the InboundWork chain.
- **Decision needed:** in Before mode, hold the whole received qty or only the sample? *Recommended:* the whole line qty for vendors on probation; sample-only otherwise (a setting on the rule).

**4. A real work queue on the handheld: generated work, priority by type, next task** (Gap #13, #14). Size **M-L**.
- **What:** receipts create Pending Putaway tasks (with the suggested or home bin), waves create Pick tasks, and the worker's handheld shows "My next task" ordered by priority.
- **Rules:**
  - A default priority per task type is configurable. *Recommended order:* Pick (sales) > Replenish > Putaway > Move > Count.
  - The existing completion functions close the Pending task they fulfil, instead of logging a new Completed one.
  - One task goes to one worker at a time; SKIP LOCKED already exists.
  - Cancelling the source document cancels its open tasks.
- **Why it matters:** without it, sales picks wait behind put-away (dyn 60:14-60:31), and there is no paperless instruction to the worker.
- **Fit:**
  - Engines exist: `CreateWarehouseTask`, `GetNextTask`, `TaskDispatchStrategy` (`engines/warehouse_task.go:86,379,456`); route `POST /api/v1/wms/tasks/next` (`internal/server/routes.go:668`). Missing: emission at the receipt and wave choke points, plus one RF "My work" screen in `view-rf-traceability.js`.
  - Add a per-type priority map to `TaskDispatchStrategy`.
- **Decision needed:** the default type order above. Also, should a manual desktop put-away still be allowed without a task? *Recommended:* yes, and it closes the matching task.

**5. Bin code structure, bulk bin generator and bin labels** (Gap #1, #7, #40). Size **S-M**.
- **What:** Bin gains `shelf` (level) and `position`. A generator creates bins for Aisle A-B x Rack 01-10 x Shelf 01-05 x Bin 01-04 using a code format (default `A-01-01-01`). Bin labels print as Code 128 with the human code beside them.
- **Rules:**
  - Generated codes must be unique per location; a re-run skips existing codes (idempotent).
  - The aisle is unique per warehouse even across zones (PwC's rule, dyn 07:17-07:30).
  - Labels print from the bin list or the generator result.
- **Why it matters:** a warehouse is set up once with hundreds of bins, and scan-to-confirm (140.12.20) needs a printed label on every bin.
- **Fit:**
  - Additive `doctype_fields` rows.
  - A generator action alongside `CopyFacilityConfig` (`engines/wms_facility.go:165`) that writes through the normal master save (`validateBinMasterRules`).
  - Labels via the `engines/code128.go` / sticker / QZ path.
  - Pick-list sort adds shelf (`engines/wms.go:317-324`).

**6. Load / route consolidation, including store transfers** (Gap #28). Size **M**.
- **What:** WaveTemplate and the Manifest flow can group by courier, route or customer. Warehouse-to-store transfer orders can be picked in the same wave or bin-picking flow.
- **Rules:**
  - A transfer line picks from bins for every item, not only batch/serial items. Today plain SKUs are not taken off bins on transfer dispatch: "Untracked lines behave exactly as before" (`engines/transfer_orders.go:146`), and the early return is at `:400-402`. This needs a check against `BinStockReconciliation` (`engines/wms_bin_allocation.go:441`) on a live tenant.
  - One wave can mix SOs and TOs for one route.
  - A wave cannot release stock already reserved elsewhere.
- **Why it matters:** the client runs stores and e-commerce out of one DC. PwC's load narrative covered both sales and transfer loads (dyn 53:41-54:29).
- **Fit:** `WaveTemplate` criteria (additive fields) and `GenerateWavePickList`; transfer dispatch reusing `ConsumeTaskBinStockTx`-style bin consumption.
- **Decision needed:** should transfers join waves, or get their own pick list per TO? *Recommended:* first make plain-SKU transfers consume bins (a correctness gap), then add TO waves.

**7. Packing list per package** (Gap #37). Size **S**.
- **What:** a printable per-box document showing package no., gross/net weight, L/W/H, and lines (SKU, name, qty), plus a total sheet per order or manifest.
- **Rules:** printable only once the package is closed (Draft package contents are mutable); reprints are audited like other prints.
- **Why it matters:** stores receiving transfer boxes and couriers both need "what is in this box", and D365 printed exactly that (frame 67:00).
- **Fit:** a new `printShapes` entry for `ShippingPackage` (`engines/print_shapes.go:25-35`) reading `ShippingPackage` lines.

**8. Weighing-scale capture at pack** (Gap #33). Size **M** (hardware).
- **What:** a "Read weight" button at pack fills gross weight in grams from a connected scale.
- **Rules:**
  - Manual entry stays possible, with a reason.
  - The read value is stored with its source (scale / manual).
  - A tolerance check against the expected weight (pack template) warns or blocks.
- **Why it matters:** courier charges are weight-based, and the client asked for grams (dyn 65:16-65:20).
- **Fit:** the local PrintBridge agent (open item 31.1.9) is the natural bridge to a USB/serial scale. No browser dependency.
- **Decision needed:** scale model and whether PrintBridge ships first. *Recommended:* defer until PrintBridge is live (31.1.9).

**9. Item / category-scoped put-away directives** (Gap #9). Size **S**.
- **What:** a PutawayStrategy can be scoped to an item category or a design as well as a location ("bangles to zone B handling-unit bins").
- **Rules:** the most specific strategy wins (item > category > location > everywhere), the same specificity order PackTemplate already uses (`engines/wms_pack_station.go:180`).
- **Fit:** additive `PutawayStrategy.category` / `item` fields in `resolvePutawayCriteria` (`engines/wms_putaway_directed.go:116`), plus a `zone_type` / `bin_type` preference token.

**10. License plates at receipt** (Gap #17). Size **S-M**, optional.
- **What:** assign an LPN (tray or pouch) during receipt or put-away, from a number series, with an LPN label. "Move LPN" moves all of its contents in one scan.
- **Rules:** an LPN lives in one bin at a time, and moving it moves every line.
- **Fit:** `AssignToLPN` / `GetLPNContents` (`engines/wms_putaway_ext.go:346,410`) plus a number series and label.
- *Recommended:* only if the client uses physical trays. They said pouches go loose into bins (dyn 34:27-34:36).

## 5. Not worth adopting

- **Stock-holding RECV / PACK / BAYDOOR locations, with "work" moves between them**: our ERP models these as states (unbinned, `in_pick`, ShippingPackage, LoadingTask). It shows the same "where is it now" answer without extra transactions or scans.
- **Location check digits**: a printed bin barcode plus scan-confirm (140.12.20) prevents wrong-bin puts more simply.
- **Mixed-batch and mixed-inventory-status rules per location**: pharma-oriented, as the presenter said himself (dyn 46:57-47:04). Jewellery is serial or SKU-tracked, and our `condition` is already a dimension within the bin.
- **Configurable mobile menu / step designer**: a no-code screen builder is a framework in its own right. Our RF screens are hand-built vanilla JS by rule.
- **SSCC / GS1-128 container labels, NAFTA / certificate of origin, hazardous-material and multimodal shipping documents**: no domestic jewellery need. SSCC only if a marketplace or B2B partner mandates it (already deferred, `engines/code128.go:13-16`).
- **License plate positioning, dock management profiles, container packing policies and "container manifest requirements"**: pallet / truck-scale features with no fit for pouches and courier boxes.
- **PO warehouse per line**: for a single DC plus stores, receive centrally and transfer or cross-dock (`engines/wms_putaway_ext.go:131`).
- **Per-item-group costing method**: the company-wide moving average is a deliberate design (`engines/costing.go:23`). Per-group methods add closing complexity that a retailer does not need.
- **Store-group price groups**: with MRP-printed jewellery, prices are the same in every store. The customer contract / default list (`engines/pos_quote.go`) covers B2B. Revisit only if store-specific pricing becomes a business rule.
- **Wave labels / wave label history**: a pallet-wave artefact. Our manifest and package label already identify the parcel.
