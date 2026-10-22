---
doc_id: DOC-PO-SETUP-001
title: Setting Up an Organisation for Purchase Orders
type: procedure
status: draft
owner: documentation-maintainer
approvers: [documentation-maintainer, documentation-maintainer]
audience: [administrators]
applies_to: source documentation; scoped release acceptance required
authority: transition-copy
confidentiality: internal
last_verified: 2026-10-19
review_by: 2026-11-19
supersedes: none
superseded_by: none
verification_scope: compiled from USER_GUIDE §6/§13, ADMIN_GUIDE §B.2-B.3, ADMIN_SOP §B; not yet walked live end to end
---

# Setting Up an Organisation for Purchase Orders

This is the order in which to set up an organisation so that its first purchase order goes through without a stop: raised, approved, sent to the vendor, received, priced, posted to stock and paid.

**Who it is for:** the organisation's administrator (Super Admin). Most steps are on **Settings** or **Setup** screens that only an administrator can change.

**How to use it:** work top to bottom. Each step uses what the steps before it created, so doing them out of order is what causes most blocked POs. Tick each box as you finish it. **Settings → Configuration** shows the same order at the top of the page (**Setup path**), marking each master **Done**, **Next**, **To do** or **Waiting**. Use it to see where you are.

**All at once, from one Excel file:** steps 1–14 can also be filled into the **Setup Workbook** and uploaded in one go, with a preview first and as many corrections as needed afterwards (**Settings → Configuration → Setup Workbook**, [Admin Guide §B.3.19](ADMIN_GUIDE.md)). This page still says what each sheet sets up and why.

**Time:** about two hours for one company with a few stores, a handful of vendors and items. Most of that time is gathering the data in step 0.

**Finding a Setup list:** the **Setup** menu groups lists by module. Type the list's name (*Legal Entity*, *Reason Code*, *MRP Rule*...) into its filter box at the top to go straight to it.

---

## Step 0 — Gather this before you start

- [ ] **Your company's registration:** each GSTIN you hold (one per state you are registered in), with its registered name and address.
- [ ] **Your places:** every store, warehouse and head office with its address, state and PIN code, and a 2–4 letter short code for each (*BDR*, *JPN*, *HO*). The short code prints inside every PO number.
- [ ] **Your teams:** the departments people work in (Purchase, Accounts, Stores, Sales...).
- [ ] **Your vendors:** name, GSTIN (or at least the state), email, phone, address, bank account and IFSC; and the TDS sections you deduct under, if any.
- [ ] **What you buy:** categories (*Antique*, *Cotton*), product types (*Necklace*, *Kurta*), the HSN code and GST rate of each, and the attributes you buy in (*Colour*, *Size*, *Purity*...) with their values.
- [ ] **Who does what:** who raises POs, who approves them (and up to what amount), who receives goods, who checks prices and sets MRP, who pays vendors.

---

## Step 1 — Choose the industry (do this first)

**Settings → Configuration → Industry** (first entry on the left).

- [ ] Click your industry (Jewelry, Clothing, Food & Beverage...), then **Set industry to …** and confirm.

What it does: adds that industry's fields to Item, replaces the screen labels with the industry's own, and loads its common **HSN codes** with their GST rates into **Setup → HSN Code**. Nothing that already exists is changed.

> **It locks once set.** Changing it later needs a written reason. Choose it before you create items, so the item fields and HSN suggestions are right from the first one.

---

## Step 2 — Legal Entity (your GSTIN)

**Setup → Legal Entity → New.**

- [ ] Create **one Legal Entity per GSTIN**. Twenty stores in one state = one legal entity.
- [ ] Fill in the **GSTIN** (required, checked for shape and check digit), registered name, address and **State**.

Why it matters for POs: the PO compares **this entity's state** with the **vendor's state** to choose **CGST + SGST** (same state) or **IGST** (different states). The entity's GSTIN and address also print on the PO.

---

## Step 3 — Locations (stores, warehouses, head office)

**Setup → Location → New Location.** Create the **head office first**, so the others can name it as their parent.

For each location:

- [ ] **Store Name**: what every screen and print shows (*Bandra Store*).
- [ ] **Type**: Store, Warehouse or HO. **Sellable** follows it (a Store sells, a Warehouse or HO does not).
- [ ] **Short Code**: 2–4 capital letters (*BDR*). It is suggested from the name; check it. It prints inside every document number (`PO/BDR/26-27/00001`). Use only capital letters and digits: a `_`, `.` or small letters makes the number invalid for GST.
- [ ] **Legal Entity**: required. Pick the entity from step 2.
- [ ] **Address, City, State, PIN code**. These print on the PO as the billing and shipping address.
- [ ] Leave **Cost Center** on *New: <name>*. One is created for the location when you save.

Check: open **Settings → Prefix Configs**, then **Edit** on the PO series. Every location should appear under **Store-wise code and suffix** with a sensible **Next number**.

---

## Step 4 — Departments

**Setup → Department → New.**

- [ ] Create one per team: *Purchase, Accounts, Stores, Sales, HR*... (usually 5–8). These are teams, not stores.

Why: every user must have a department (step 13 will not save a user without one), and postings carry the department of the person who raised them.

---

## Step 5 — HSN codes and units

**Setup → HSN Code** and **Setup → UOM**.

- [ ] Check the HSN codes the industry loaded. Correct any **Default GST Rate** that is out of date: the rate on your list is what items and item names pick up, and it wins over the system's suggestion.
- [ ] Add any HSN code you buy that is missing (4, 6 or 8 digits).
- [ ] Check the units you buy in exist (*PCS*, *KG*, *MTR*...).

Why: every item on a PO needs an **HSN code and GST rate**. A line whose item has none turns red and the PO will not save.

---

## Step 6 — Number series (decide before the first PO)

**Settings → Prefix Configs**, **Procurement** group.

Numbers are never typed. Each document draws the next number from its series when it is saved. A change applies only to the **next** document; issued numbers are never renumbered. So settle the format before the first PO.

- [ ] Review these series: **Purchase order (PO)**, **Goods receipt (GRN)**, **Purchase return (PRT)**, **Debit note (DN)**, **Vendor credit note (VCN)**, and **PR / RFQ / Vendor Quote** if you will use requisitions or quotes.
- [ ] For each, set **Prefix**, **Separator**, **Number length**, **Restart numbering** (every financial year / every month / never), **Year in the number** (*26-27*, *2627*, *26*...) and **Where the year goes** (before the number or at the end). Check the **Next number looks like** preview.
- [ ] **Store in the number**: **Yes** gives each store its own counter and prints its short code. **No** gives one shared series.
- [ ] Keep every number **16 characters or fewer** (GST rule). The form refuses a longer one and names the store whose code makes it too long.

**Master codes** (Vendor `V00001`, Item / Combination ID `I00001`, Design `D00001`, Category, Product Type...) are asked **once**, on the first record of each kind: the New form shows *"… code format (asked once)"* with a suggestion. Accept it or change it then.

---

## Step 7 — Organisation settings

**Settings → Configuration.** Use **Find a setting** at the top to jump to any of these. Changes take effect immediately; **Save Changes** per module.

### Procurement

| Setting | Default | What to choose |
|---|---|---|
| **Purchase Order price GST treatment** | Inclusive | **Inclusive** if your vendors quote prices with GST in them, **Exclusive** if they quote before GST. Each PO can still switch. |
| **Days a PO or GRN stays editable** | 0 (no limit) | A number of days if you want old documents frozen. |
| **Purchase Order auto-cancel** | 0 (never) | e.g. 30. An Approved PO not received in that many days is cancelled (or short-closed if part-received). |
| **Vendor-invoice 3-way match tolerance %** | 2 | How far a vendor bill may differ from the accepted goods and still match. |
| **Purchase return window** | 0 (no limit) | Days after a GRN that accepted stock may still go back. |
| **Design number when a PO adds an item name's design** | Always new | **Always new** for fast fashion. **Inherit from existing** reuses a design of the same name and attributes from the same vendor. |
| **Goods receipt: same price within** | 0 | Rupees within which a price counts as "same as last time" at the price-check step. |
| **Goods receipt: blind count** | Off | On = the receiving clerk does not see the PO quantity and counts what is there. |
| **Goods receipt: suggested MRP markup** | 100 % | Used for the suggested MRP when no MRP rule applies (step 9). |
| **Goods receipt: suggested MRP ending** | ₹x99 | ₹x99, ₹x49/x99, ₹x9, round to ₹10, or none. |
| **Goods receipt: who may verify / price check / set MRP and post / pay / correct** | blank (everyone who can update GRNs) | Role names, comma-separated, if different people do different steps. Leave blank in a small shop. |

### Inventory

| Setting | Default | What to choose |
|---|---|---|
| **Traceability for new items** | None | Batch / Serial for items you must trace (food, pharma, high-value). |
| **Shelf life / minimum shelf life on receipt** | 0 | Days, for goods with an expiry. |
| **Generate barcodes at** | SKU creation | **SKU creation** (each item gets its barcode when saved) or **Goods receipt** (the first time it is received). |
| **New barcode for each receipt date** | No | Yes only if you want each day's stock on its own barcode. |
| **GRN batch numbers** | One per SKU on each GRN | **One per GRN line** for jewellery, where each piece has its own weight. |
| **Item name order** | Category + Type | *Antique Necklace* or *Necklace Antique*. |
| **Attributes follow the item's type** | Off | On if you want a necklace PO line to offer only necklace attributes and values (map them in step 9). |

### Security and Platform

| Setting | Default | What to choose |
|---|---|---|
| **Security → Who can approve their own documents** | Anyone who can approve it | Keep it if one person buys and approves. **Nobody** for strict maker-checker. You then need a second user with the approving role (step 13). |
| **Platform → Reasons must be picked from the reason codes** | Off | On if every rejected / damaged / returned line must carry a coded reason (step 11). |

---

## Step 8 — Item fields: what the PO and the GRN ask

**Settings → Configuration → Item fields.** One table of every item field and attribute.

- [ ] **In the PO**: tick the attributes the buyer picks on a PO line (*Colour*, *Size*, *Fabric*...). Each becomes a column on the PO and its print.
- [ ] **At the GRN**: *No / Optional / Required* for values typed when goods arrive (*Gross Weight*, *Net Weight*, *Purity*...). *Required* stops a receipt line posting without it. The value goes on the batch and prints on the stickers.
- [ ] **Values from**: link a typed field to an Item Attribute so it becomes a drop-down.
- [ ] **Hidden**: hide fields you never use.

---

## Step 9 — Item masters: what you buy

Create these in this order. Each one uses the one before it.

1. - [ ] **Category** (*Setup → Category*): just a name (*Antique*, *Cotton*). Optional: its own **MRP markup %**.
2. - [ ] **Product Type** (*Setup → Product Type*): just a name (*Necklace*, *Kurta*).
3. - [ ] **Item Attribute** (*Setup → Item Attribute*): *Colour*, *Size*... with their values. Value Type **Select**.
4. - [ ] **Attributes per type** (only if *Attributes follow the item's type* is on): open each Product Type, tick its attributes under **Attributes for this type** and click the values it may use. **Suggest from my items** does this from existing items.
5. - [ ] **MRP Rule** (*Setup → MRP Rule*, optional): how the MRP is suggested at the GRN: a **Multiplier** (price × 3), **Purchase price bands** (`500=3, 2000=2.5, above=2`) or **Weight + purchase price**. Pick a rule on each **Product Type** (field **MRP rule**), or tick one rule **Default for product types with no rule**. Without any rule the suggestion is cost × (1 + markup) × (1 + GST).
6. - [ ] **Item Name** (*Setup → Item Name*): a Category + a Product Type + **HSN code** + **GST rate**. This is what the buyer types on a PO line, and every design and combination created from it inherits its HSN and GST.

**Items themselves are optional before the first PO.** A buyer can type an item name on the PO, enter a design number, pick the colours and sizes, and the PO creates the design and every combination. Create items in advance (*Setup → Item*, or **Bulk Import**) only for what you already stock. Every item needs an **HSN Code** and **GST Rate** (or a **Tax Treatment** of Exempt / Nil-Rated / Zero-Rated).

**Many POs at once:** *Purchase Orders → Upload from Excel* makes the vendors, item names, designs and POs from one Excel file — and the goods receipts of the bills in it (User Guide §6.12). Only the stores must be set up first.

---

## Step 10 — Vendors

**Procurement → Vendors → New Vendor.**

- [ ] **Vendor Name** (required).
- [ ] **GSTIN**, or at least **State**. Without either, the PO cannot tell CGST + SGST from IGST and shows an amber banner.
- [ ] **Contact Email**: **Send to Vendor** emails the PO here. With none, the buyer downloads the PDF instead.
- [ ] **Address, City, PIN**: print on the PO.
- [ ] **Bank Account Number** and **Bank IFSC**: for paying the vendor.
- [ ] **TDS** (only if you deduct it): the section is not set on the vendor. Create your sections and rates once under *Setup → TDS Section*; whoever pays picks one with **Withhold TDS** / **Pay w/ TDS**.

**Over-delivery tolerance** (optional): *Setup → Receipt Validation Rule*. A rule with no vendor is the default for everyone; a rule per vendor overrides it. **Over-receipt tolerance %** is how much extra is accepted without asking. **Allow unexpected items** decides whether items not on the PO may be received. Anything beyond it is decided at the GRN's Verify step (keep it, which needs an approval, or return it).

---

## Step 11 — Reason codes

**Setup → Reason Code → New.** Add a few for each category. The screens offer them from a list.

| Category | Used when |
|---|---|
| **Cancellation** | **Short-closing** a PO the vendor will not complete. Short-close cannot be done without one. |
| **Goods Receipt Rejection** | Units rejected at the GRN. |
| **Goods Receipt Damage** | Units damaged at the GRN. |
| **Purchase Return** | Sending goods back to the vendor. |

- [ ] At least two codes in each of the four categories (e.g. *Vendor cannot supply*, *Wrong item*, *Quality fail*, *Broken in transit*).

---

## Step 12 — Roles: what each person may do

**Settings → Roles.** Super Admin always has full access. Every other role sees **nothing** until you tick it.

- [ ] Create a role per job (for example *Purchase*, *Store Keeper*, *Accounts Payable*), or use the shipped *Store Manager*.
- [ ] Open the role and tick on its access page:

| The person... | Needs |
|---|---|
| raises POs | **Procurement** module: Read, Create, Update (covers PO, GRN, Vendor, Vendor Invoice, Requisition, RFQ). |
| creates new designs from the PO | **Create** on *Item Name*, *Category*, *Product Type*; **Update** on *Item Attribute* (to add a value), **Create** on it (to add an attribute). |
| deletes POs | *Purchase Order → Delete* (off for Store Manager as shipped). |
| receives goods | **Create** on *Goods Receipt*; **Update** on it for the steps after Receive. |
| pays the vendor from the GRN | **Update** on *Vendor Invoice*. |

- [ ] **Stores** button on the role: **All stores**, **Selected stores** or **Home store only** (the default). A buyer at head office who raises POs for every store needs **All stores**.
- [ ] **Save access.**

---

## Step 13 — Users

**Settings → Users.**

- [ ] For each person: **Username**, **Password**, **Email**, **Role** (step 12), **Home store** (step 3) and **Department** (step 4) → **Create User**.
- [ ] **Stores** on the user's row, if this person needs more or fewer stores than their role gives.
- [ ] If step 7 set **Who can approve their own documents** to *Nobody* or *Admins only*, create **at least two** accounts: one who raises the PO and one who holds the approving role (step 14).

---

## Step 14 — Approval rules: who approves which PO

**Settings → Approval Rules.** Every PO is submitted for approval, and its amount decides which role must approve it.

As shipped, record type **PurchaseOrder**:

| Min | Max | Approved by |
|---|---|---|
| 0 | 49,999 | Store Manager |
| 50,000 | (no limit) | Super Admin |

- [ ] Change the bands and roles to match your business, using the roles from step 12. **Save Rule** per band.
- [ ] **Leave no gap**: if one band ends at 49,999 the next starts at 50,000. A PO whose amount falls in no band cannot be submitted (*"has no approval rule configured"*).
- [ ] Make sure someone actually **holds** each role you name. A PO routed to a role no user has sits in Approvals for ever.
- [ ] Check **GRNExcessApproval** (goods kept beyond the PO; shipped: any amount → Store Manager) and **PurchaseRequisition** if you use requisitions.
- [ ] Use **Who approves what** below the table: pick *PurchaseOrder*, type an amount, and it shows which role it goes to and marks any gap.

**One person running everything?** Keep *Who can approve their own documents* on **Anyone who can approve it** and name that person's role on every band. They raise, then approve their own PO in **Approvals**. Each such decision is marked *[decided by the submitter]* in the history.

---

## Step 15 — Printing and sending the PO

- [ ] **Store addresses and GSTINs** (steps 2–3) and **vendor details** (step 10) are what the standard PO print shows. Check them now.
- [ ] **Print templates** (*Setup → Printing → Print Template*, optional): the **Standard layout** works without setup. Design your own only if you want a different look. The **Vendor PO** copy never shows MRP.
- [ ] **Email**: **Send email** needs the server's mail account (`SMTP_HOST`, `SMTP_PORT`, `SMTP_FROM`, `SMTP_USER`, `SMTP_PASSWORD`), which the server operator sets, not a screen. Without it, **Send email** downloads the PDF instead and says so.
- [ ] **Product photos** (optional): **Settings → Configuration → Integrations → Image drive** to keep design and combination photos in your own S3-compatible bucket. Without it they are kept on the ERP server.
- [ ] **Label printing** (optional): sticker templates and printers if you print barcode stickers at the GRN (*Setup → Printing*; see [Printing setup](QZ_PRINTING_SETUP.md)).

---

## Step 16 — Prove it with one small PO

Raise one PO for a small amount and take it all the way through. If every check below passes, the setup is done.

| # | Do | Where | Check |
|---|---|---|---|
| 1 | Click **+ New Purchase Order**, pick the vendor, the billing store and one or two items or an item name with a design | Procurement → Purchase Order | The **supply-type banner** says CGST + SGST or IGST correctly (not amber). HSN and GST fill in on every line; none is red. |
| 2 | **Create Draft** | – | The PO number reads the way you set it in step 6 (`PO/BDR/26-27/00001`). |
| 3 | **Submit for Approval** on its row | PO list | Status moves to **Pending Approval**. |
| 4 | **Approve**, as the approving user | Approvals | Status **Approved**; **Receive goods** appears on the row. |
| 5 | **Send to Vendor** → **Download PDF** (or **Send email**) | PO list | The PDF shows your entity, both addresses and GSTINs, HSN, the tax split and the total. Row says *Sent to vendor*. |
| 6 | **Receive goods**: type the vendor's **Invoice No, Date and Total**, the quantities, any GRN-time fields → **Save receipt** | Goods Receipt | Saved as *Received*. No stock yet. |
| 7 | **Verify** | GRN steps | No *"The bill does not add up"* box (or fix the invoice total). |
| 8 | **Check prices** → **Save and go to MRP** | GRN steps | Lines split into *New item / Same price / Price changed*. |
| 9 | Type or **Use suggested** MRP → **Save and post** each child | GRN steps | The suggested MRP names your MRP rule. |
| 10 | Check stock | Inventory → Stock on Hand | Quantity went up at the receiving store. |
| 11 | **Print stickers** (optional) | posted child | Stickers show the item, barcode and MRP. |
| 12 | **Pay** or **Leave it in Payables** | GRN step 5 | The vendor bill was made from the invoice you typed. |
| 13 | Check the vendor | Reports → Procurement → Vendor Ledger | The bill (and payment) is against the vendor. |

The full walk of each screen is in the [User Guide](USER_GUIDE.md), §6 (PO) and §6.10 (receiving in steps).

---

## When something blocks you

| You see | Why | Fix |
|---|---|---|
| Amber banner: *... has no state — choose it* | The vendor, or the billing store and its Legal Entity, have no GSTIN or State | Choose the state in the banner and click **Save to vendor** / **Save to store**; it is saved on that record for every later PO. Or add it to the vendor (step 10) or the legal entity (step 2). |
| A PO line turns red | That item has no HSN code or GST rate | Fix the item (or its item name) in Setup. |
| *enter the purchase price* | A line has no purchase price | Every PO line needs one. |
| *has no approval rule configured* | The PO amount falls in a gap between bands | Close the gap (step 14). |
| PO sits in Approvals and nobody can approve it | The band names a role nobody holds, or the raiser cannot approve their own | Give someone that role, or change the band or the self-approval setting (step 7). |
| `APPROV-0840` *You cannot approve or reject a document you submitted* | *Who can approve their own documents* is *Nobody* / *Admins only* | Approve as another user, or change the setting. |
| `ADMINC-0311` on Prefix Configs | A number would be over 16 characters, or two stores print the same code | Shorter prefix, year as *2627* or *26*, fewer digits, or a shorter store code / **Code in number**. |
| `ADMINC-0030` | That series is **Inactive** | Set it Active (step 6). |
| `MASTER-0312` on a location | Short Code already used by another store | Pick another. |
| `GOODSR-0940` / `GOODSR-0941` | Vendor invoice number, date or total missing / that invoice number already received | Type all three / check it is not a duplicate bill. |
| `GOODSR-0942` *The bill does not add up* | Invoice total ≠ invoice lines + GST (by more than ₹1) | Correct the total or the line's invoice qty / rate on Receive. |
| `GOODSR-0947` | The user's role is not allowed to do that GRN step | Add the role to that step's setting (step 7), or leave the setting blank. |
| `MASTER-0891` | A field set *Required at the GRN* is empty | Fill it on the line (step 8 decides which). |
| No reason codes to pick at Short-close | No **Cancellation** reason codes | Add them (step 11). |
| **Delete** not shown on a PO | The role lacks *Purchase Order → Delete* | Tick it (step 12). |
| `PURCHA-0320` on Delete | Goods were already received on it | Cancel the receipt first, or **Short-close** instead. |
| **Send email** downloads a PDF instead | No mail account on the server | Ask the server operator to set it (step 15), or send the PDF yourself. |
| A user cannot see a store in the switcher | Their role or user **Stores** do not include it | Step 12 / 13. |

---

## Where to read more

- [User Guide](USER_GUIDE.md) §6: the PO and GRN screens step by step (§6.2 GST, §6.9 designs from the PO, §6.10 receiving in steps).
- [User Guide](USER_GUIDE.md) §13: the full first-sale walkthrough, from an empty system to the till.
- [Admin Guide](ADMIN_GUIDE.md) §B.3: every setting above in detail (§B.3.2 number series, §B.3.5b–c item master, §B.3.6–B.3.6.1 goods receipt).
- [Admin SOP](ADMIN_SOP.md) §B.9–B.10: approval rules and approving your own documents.
- [Printing setup](QZ_PRINTING_SETUP.md): sticker and document printers.
