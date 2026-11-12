---
doc_id: DOC-PRODUCT-RESEARCH-REC-PIM-UNBXD-SAASTIFY
title: Recording blueprint - Unbxd and Saastify PIM, gap-checked
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

# PIM blueprint: Saastify (first meeting) + Unbxd (training), gap-checked against our ERP

> Stage 146 (2026-10-31): learned from the user's meeting recordings (`Downloads\Telegram Desktop\Dynamics Loyalty Management Demo 1`). Citations `(key mm:ss)` point into those videos; the machine transcripts and screen OCR were working files and are not kept. Gap rows were checked against the working tree on 2026-10-31. The plan built from these files is `docs/product/recordings-gap-plan-2026-10-31.md`.

Citations: `(sas mm:ss)` is the Saastify transcript, `(unb mm:ss)` is the Unbxd transcript, and `f` marks a screen frame (`sas f14:15` is frame t0h14m15s). File:line references point into the ERP repo.

## 1. Sources

- **"Saastify 1st meeting - PIMS" (52 min).** A sales and onboarding call. Siva (architect) demos **SaaStify Edge Suite** to Kushal's Fashion Jewellery: Vishal, Asha, and Velu from the catalogue team. Karthik (solutions engineer) and Priya (customer success) also attend. Most screens come from a demo account with apparel and fabric data and from two other clients' template lists (Leemboodi; a saree seller). The second half covers Kushal's move from Unbxd: a data dump, the GoFrugal template, S3 and Google Drive feeds, and image naming.
- **"Unbxd Training PIMS" (57 min).** An internal Kushal's handover. Vishal trains the catalogue and marketplace staff (Saurav, Rashmita) on **Unbxd PIM** with Kushal's live tenant: 94,189 products and 141 attributes. At 39:30 Sourav takes over to debug failed Amazon listings in Seller Central. Everything here is Kushal's real configuration: Shopify and Shopify International stores, the Amazon SP-API app, and Myntra by Excel.
- **Client context.** GoFrugal is the item master and POS. Unicommerce holds inventory and orders, so a PIM needs no inventory (sas 39:24-39:58). Shopify is the storefront. Amazon is half set up (98% mapped, stuck on errors, unb 26:06-26:22). Myntra is fed by Excel upload.

## 2. Blueprint

### 2A. Unbxd PIM, as Kushal's runs it

**Navigation (unb f00:00, f30:00):** OVERVIEW · IMPORTS · PRODUCTS (Products / Certified Products) · EXPORTS (Exports List, Export Templates, Scheduled Exports) · DAM · MANAGE (Attributes, Tasks, Workflows, Product Groups) · APP STORE.

**A1. Overview dashboard (unb f00:00).**
- Tiles: Products 94,189 (29,157 parents, 65,455 variants), 141 Attributes, 2,391 Categories, 240,801 Assets.
- Attribute Distribution by data type: Text 49, Number 21, Boolean 0, Date 0, Image 15, Link 2, Tree/List 51, Text Area 3.
- Core Attribute Coverage gauges, each "% of products mapped" with a View link: Product Name 99.33%, **Product Image 0.00%**, Category 99.34%, Parent ID 99.32%.
- Start Import / Start Export shortcuts.

**A2. Attributes ("properties") - Manage → Attributes (unb 02:53-04:30, 38:00; f03:30-f06:45).**
- An attribute is one column header of the Excel. Create it with a name and data type: Text, Number, Date, Link, Boolean, File, Image, Tree/List (dropdown), Text Area (unb 03:32-03:45, f03:45).
- Flags: "Allow attribute to be searched", "Is MultiValued". Tree-list values are maintained in place; Amazon's clasp types are Ball, S-Hook, Slide Lock, Barrel, Box, Buckle, Snap, Hook and Eye, Fishhook, Lobster (f04:15).
- **Permissions for Roles** per attribute, and Delete Attribute (f06:45).
- Attribute groups, with attributes left ungrouped where none applies. Bulk Upload Attributes (f03:45).
- Kushal's groups are team-shaped (f05:00, f14:30): "01 Shopify Manual Entry by Catalog Team (16)", "02 … by Stylist Team", "04 Shopify Visual", "05 … Description (Content Team)", "Shopify Default Inherited Values", Pricing, Specifications, Basic.
- Jewellery attributes seen: Stone, Tags (multi), Tikka Weight/Length, Product Weight (gms), Product Width (cm), Catalog Analyst, Earring Length (cms), Earring Style, Earring Weight, Earring closure type, Idol Type, CAT-NAME (batch), Shopify options, MF_discount, Compare-at price, MRP.
- **Channel-only attributes** are created with a channel prefix, for example "Clasp type necklace (Amazon)", "Clasp type bangles (Amazon)", and Amazon image 1-5 (f03:30, f09:00). *Client-specific.*

**A3. Products, parent/variant (unb 11:03-14:46, f10:00-f15:00).**
- Design number = **parent**; Combination ID = **variant**. An item with no combination is "solo". The grid toggles between "Parents and Solo" and "Variants and Solo" views. In the parent view a design is one line; in the variant view it is its four SKUs (unb 11:20-12:05).
- Parent-level data: description, product type, tags, metafields, catalogue attributes. These are **entered once for all combinations** (unb 12:30-14:46).
- Variant-level data: colour/polish, price, MRP. Kushal's prices all variants of a design the same today. Silver used to vary by weight (₹1,020 / 1,080 / 1,060), "everyone else will have a different price" (unb 13:28-13:55).
- Grid: columns DESIGNNO, productType, Combination ID, Item Name, MRP, image1-4. Column sets come from attribute groups ("04 Shopify Visual", "Data Entry"). Page size is 50, max 100 (unb 19:01). Values are editable inline in the grid (unb 17:48-18:08).
- Product page (unb f14:30): Certified-at and Updated-at timestamps; Parent badge; **"Pending updates to certify (Go to last certified product)"**; category path ("Manual Entry Fields (Shopify Catalog) > Ear Rings"); attribute-group navigator; More Actions.

**A4. Product groups (unb 15:05-16:25, f15:30-f16:45).**
- Kushal's works in **catalogue batches** ("CAT-240"). The catalogue team filters attribute CAT-NAME *equals* CAT-240 → "Save as dynamic group" → group by parent → data entry.
- Filter rows are attribute + operator + value, with "Add another Set". Groups seen include CAT-468…CAT-473, VDAY2025, Sandbox export list, STS Disc data, MOD-137/138, "Price change 18/01".

**A5. Certify (QC freeze) (unb 00:27-01:02, 16:25-18:08, f17:15-f20:30).**
1. Select products (the page selection is 50) and press Certify.
2. Create a new **certified product group** or pick an existing one ("CAT-240 Amazon"), then Certify all.
3. Certified products are read-only, "it is QC done, ready to export". The working copy stays editable. A later edit shows "pending updates to certify" until it is re-certified.
4. **All exports read from Certified Products** (the export source column, unb f36:15).

**A6. Export templates and mapping (unb 22:52-26:22, 47:31-55:00; f23:30-f26:30, f48:15-f55:00).**
- One template per marketplace **category file**. The real Amazon category template (.xlsm from the category manager) is uploaded, and its columns become the template's attributes (unb 23:31-24:29). Template details: name, description, file URL, Gist ID, meta info (f24:30).
- Kushal's Amazon template has tabs per Amazon product type: Necklaces, Earrings, Toe Rings, Bracelets, Bangles, Anklet, Brooches & Pin, Mangalsutra, Hair Clip, Kamarband, Bajuband, Maang Tikka, Finger Rings (f26:30, f54:00). *Jewellery-specific.*
- Coverage header: Toe Rings is "Total 79/198 mapped 39% · Mandatory 19/28 67% · Optional 60/170 35%"; Necklaces is 81/190, 42% (f25:00, f48:45).
- Per column: "exports as" target name, PIM source attribute, Data Type, **Mandatory for all**, **Is Multivalued**.
- **Validation Rules**, scoped by product type ("Edit Rules for feed_product_type = ring"), hold **Accepted Values**: inches/centimeters/millimeters; Yes/No; battery chemistry list (f25:00).
- **Transformation**, either a tool view (Attribute → Operation **Set** → value "Adult"; age_range_description = Adult, batteries_required = No) or a **code view** (Python `def transformer(data)`). Post-transformation, Preview product, history (f25:15-f25:30).
- Real transformations: colour normalisation by chained replaces (AMT/GREEN→Amethyst-Green, B.SAPHIRE→Sapphire, BLACK/WHT→Black-White, CHAMP→Champagne, NA→Gold; f25:30). Value translation Jhumka→Jhumki, Hanging→"Hanging one" (unb 06:41-08:45). The **parent_child** column is computed: Parent if the item has no combination ID, else Child (f54:15).
- **Rule placement (unb 08:57-10:58).** Write the rule on the *import* side when the derived value should be stored and visible in PIM. Write it on the *export* side when it should only shape that channel's file.
- **Channel-specific manual override (unb 05:18-08:45).** When Amazon needs a value PIM cannot derive ("star"), create an Amazon-only dropdown attribute. The export rule is "if the Amazon attribute has a value push it, else push the transformed generic value".
- **Images (unb 47:31-52:40).** Amazon main_image_url must be a **white-background** image, so a separate "Amazon image 1" is uploaded. other_image_url1… map to PIM image1…image4, Thumbnail image link, and so on. A fallback rule applies if Amazon image 1 is missing. Images are stored per variant (or parent), picked from device or from a DAM **set**: a bulk-uploaded batch searched by SKU. Stored URLs look like `…/183394_437984_M.jpg` (design_combination_M) (f53:30).

**A7. Exports (unb 18:43-22:10, 36:00-37:15; f21:45-f22:30, f36:15-f36:30, f45:45-f46:45).**
1. Choose the source (a certified group, or All Certified Products) and switch to parent view. Use "Export all products", not the page selection.
2. Name the export. Pick a method: **Email**, **SFTP** or **Channel**. The channel apps are "Amazon Installed by Unbxd" (SP-API), Kushals Shopify, Shopify GraphQL, "installed from unbxd", Sandbox, Shopify International (+Sandbox) (f30:30).
3. Pick a template, a format (CSV / Excel / both) and a file-name option. Add recipients, comma-separated.
4. Choose **Export with / without readiness check**. Readiness is a filter; the team uses "without" (unb 21:13-21:25).
5. The Exports list (13,972 rows) shows name, source, destination, template, exported-on, status (Started / In Progress / Exported), and Success / Failed / Total. Actions are Re-export, Product stats and Download CSV. Email arrives in 2-3 minutes.
6. Export detail lists failed products with the raw error (Shopify images.json connection timeouts; a Slack webhook DNS failure) (f36:30).
- **Never use the Shopify channel directly.** A wrong template "caused a lot of chaos" (unb 36:46-37:15). Myntra has no API: export Excel and upload it on Myntra (unb 02:46-02:53).

**A8. Workflows (automation) (unb 28:49-36:39, f29:00-f34:15).**
- List shows Active 4 / Paused 4 / Completed 1, last ran (FAILED), Run report. Examples: "Bulk delete tags 40% off" (and 10/20/50/60%), "Shopify March Sale Export Template", "international website export", and one Myntra workflow per category (Saree Accessories, Head Jewellery, Bangle, Anklet, Necklace and Chains).
- Configure: trigger **Time**, repeats every N minutes, starting 00:00 GMT, ends Never. Steps are nodes: **Filter Certified Products** (attribute conditions, group by parent) → **Export** (Email / SFTP / Channel + template + extra parameters). Start Workflow or Run manually (f30:00, f30:30, f32:00).
- Practice: the CAT value changes daily, so the workflows are run **manually** after editing the filter. The aim is to push one new CAT to all 11 category templates in one go. Failures are checked an hour later (unb 35:04-36:39).

**A9. Tasks (unb 26:34-28:49, f26:45-f28:30).**
1. Pick a task template (or select attributes by hand). Example: "catalog shoot", assignee role Photographer, attribute image1, type Image, group 04 Shopify Visual.
2. Select products by filter (CAT-NAME), **max 500 per manual task**, group by parent.
3. Select the attributes to fill (12 selected).
4. Enter name and instructions, pick assignees from users, then Save & Assign. **The assignee gets an email.**
- Task list columns: Task, Assigned By, Members / Roles, created date, **Task Type (Readiness Task / PIM Task)**, **Template Used** (International Shopify Template / Default Master Shopify Template / attributes selected manually). Names seen: "2 missing vals", "Wrong Image Association", "Tag Update".

**A10. Marketplace facts learnt (Amazon India, jewellery) (unb 39:30-55:00).** *Client-specific but reusable.*
- external_product_id: they **zero-padded** internal codes to 13 digits. That is wrong; leave it blank or use a GTIN exemption (unb 43:12-44:01).
- Setting **metal type** makes metal ID, metal type and metal stamp mandatory. That is a conditional requirement (unb 44:38-45:21).
- Amazon variation (parent/child) is only possible for themes Amazon supports (colour, style, material, stone shape…). Polish variation is not, so those SKUs are listed individually. The variation type cannot change after the parent SKU exists (unb 52:47-53:24, f43:00).

### 2B. SaaStify Edge Suite (as demoed)

**Navigation (sas f00:00, f01:30, f42:30):** PRODUCTS & INVENTORY (Enrichment, Products, Categories, Collections, Price Lists, Digital Assets, Templates, Vendors, Brand Portals, Product Merge) · SALES & ORDERS · SUPPORT & TICKETS · REPORTS & ANALYTICS (Analytics, Job History, Reports, Automation) · SETTINGS (Account, Support, Integrations, Users). A "Jobs" counter sits in the footer.

**B1. Product list (sas 12:53-14:29; f00:00, f13:15, f14:15).**
- Search box: "Paste from Excel, comma/newline/tab separated values - auto-converted": a **multi-SKU paste search**.
- Actions: Add Product, Import, Export, Enrich with AI.
- Columns: SKU, Category, Priority, Priority Notes, Enrichment status (Pending), Updated, and a **Stores column**. The Stores column has icons for every channel the product is live on, each a one-click link to that channel's admin or front-end page. One client has 10+ channels including 5 Shopify accounts (sas 12:59-13:57).
- **"Select a template…"** switches the grid to that template's columns plus a **Completeness** column. Example: "ETSY Channel Export (CHANNEL)", 22/27 = 81%, with sellerSku, description, price, quantity, condition, warehouse (sas 14:10-14:26, f14:15). "Create View" saves a column view.

**B2. Product detail tabs (sas f01:30-f05:30).** Product Completeness · Category Based Attributes · AI Enrichment · Channel View · DAM · Variants (21) · Pricing · Relationships · Addons · Master Data.
- **Channel View Management** (f01:30): store selector plus template. Shows Field Mapping 100%, Data Completeness 33%, and 18 total / 18 mapped / 6 with data. Column groups: Identity, Json Data, Advanced, Classification, SEO & Marketing, Content. Values are edited per column. Actions: Refresh, Open Template, Remove, Clear All, AI Generate, Channel Export.
- **Category Based Attributes:** "Fill in template-based attributes for each Category associated with this product" (f03:15). Necklace, bracelet, bangles and kundan each carry different attributes (sas 03:08-03:19).
- **Variants:** Shopify-style options (option name such as Plain Colour / Casual Sizes, and values). The variant grid has SKU, price, sale price, cost, inventory, UPC, EAN, MPN and barcode; Configure Options (sas 03:38-04:01, f02:00).
- **Pricing:** price lists per country at product or variant level (India, Nepal, Bhutan, Sri Lanka, Middle East/Dubai) (sas 04:05-04:28).
- **Relationships:** types Recommended (PDP), Alternative, Associated, Related, Upsell, Cross-sell, Bundle, Accessory, plus "Create Relationship Type". Added by search or **bulk upload**. Optional ML models (fast-selling, new, bought-together) generate a file that is uploaded. Pushed as **Shopify metafields** that the storefront renders (sas 04:31-07:29, f05:30).
- **Add-ons:** product-specific attach items that raise order value, for example earring back screws in a pack of 3 (sas 05:03-08:38).
- **Master Data:** dimensions, weight and unit; case length/width/height/weight; product status; enrich status; priority (99) and notes; lists of warehouses / suppliers / dropshippers; source; store URL; channel status (f05:15).

**B3. Collections (sas 00:17-01:09, f00:45-f01:00).**
- Title and Handle (URL-friendly). Type is Manual or Automated.
- Automated collections use conditions with "products must match **all / any**". Fields: Product Name, SKU, Price, Category, Type, Brand, Tag, metafields.
- Also: collection image (upload / URL / from DAM), description, SEO meta title and description, sort order, channels, and collection completeness.

**B4. Standard schema (sas 11:31-12:37).**
- A predefined global schema of about 100 attributes (product IDs, SKU, UPC, barcode, name, type, tags, description, short description). It is mapped once and reused for Shopify, Amazon, Walmart, eBay, Etsy, Flipkart, Myntra and Ajio.
- Anything beyond it goes in **customer-managed fields** (key-value pairs).

**B5. Templates (sas 11:15-11:26, 13:59-14:29, 42:05-45:00; f16:45-f17:00, f21:00-f21:30, f42:15-f45:00).**
- Template types: **FEED** (import feeds), **CHANNEL** (marketplace export), CUSTOM, **CATEGORY**, CATALOG, GLOBAL, **TASK**, ORDER.
- Formats: SIMPLE, AMAZON, MYNTRA, MEESHO, JIOMART, FLIPKART. Attribute counts per template range from 3 to 202.
- Templates seen: purpose-built update feeds ("UPDATE PRIORITY LEVEL ONLY", "PRICE UPDATE", "UPDATE ITEM UPC", "CHANGE STATUS ITEM OOS", "Missing Image for SKUs across all stores"); per-marketplace-per-category exports (Amazon Saree 152 attributes, Amazon Top & Shirt 194, Myntra KBD, Flipkart co-ord, JioMart/Meesho Saree); order feed / shipment feed / packing slip.
- Template editor (f42:30, f44:15):
  - Metrics: Total / Mapped / Data Type / Validation / Transformation %, "14 Issues", **Analyze Template**, **AI Map**, Schema Map, Add New Attribute.
  - Per row: source, data type, **Mandatory**, **Multi-valued**, Transformation steps, Validation Rules, Description.
- Transformations: Join (delimiter only between existing values, empties skipped), Amazon HTML, Replace, Replace Regex, Prefix, Suffix, Split, Uppercase, Lowercase, Strip left/right (sas 42:05-42:37, f42:30). Validation Rules are **Enum** lists ("Bronze… +19 more"; "Made in the USA or Imported") (f44:15-f45:00).
- An Excel output keeps the original marketplace template's dropdowns (sas 43:24-43:37).

**B6. Import (sas 08:46-12:07, 41:23-41:40; f08:45).**
- **File Import** (named job) or **Channel Import** (pull from Shopify and other connected channels).
- Entities: products, categories, price lists, collections, relationships, variations, inventory. Each import runs through a FEED template that does mapping + transformations + validation.
- **Kushal's pain:** in Unbxd they upload the same file three times ("step 1, step 2, step 3 import") (sas 09:07-09:36, 40:44-40:56).
- Proposal: GoFrugal exports to S3 and Saastify picks it up **nightly at 3-4 am** and **merges** it with existing data. Unmatched columns become customer-managed fields. Remove junk or duplicate columns first; send parents + variants in one file (sas 10:18-10:34, 35:51-36:00, 41:23-41:40).

**B7. Digital assets (sas 15:16-16:38, 44:02-47:48; f15:30-f16:15, f45:15, f47:00-f47:45).**
- About 90k assets in folders, searchable by name or extension. Images, documents and videos: size charts, warranty, usage guidelines. Linked to products (one asset can be linked to more products).
- **Import Digital Assets** wizard (f45:15): select files → import configuration (optional template, associate with products, the image-name field to match on). File-name conventions:
  - many per SKU, `SKU_1`, `SKU_2`;
  - single per SKU;
  - ZIP, single folder, SKU as name;
  - ZIP, single folder, many per SKU;
  - ZIP, one folder per SKU;
  - SKU names for parent and variant.
- Kushal's agencies name images SKU_1/_2/_3 and drop them in a folder; they must "auto-map, no need to arrange again" (sas 44:02-45:22). Underscore or hyphen is chosen at upload.
- **Automation:** a Google Drive listener ingests new files automatically, built in n8n (sas 46:44-47:48). AWS S3 is said to "not have listeners".
- **Kushal's Shopify problem:** Shopify replaces image URLs on every GraphQL update. They want **a fixed image URL per combination** for other channels such as Unicommerce (sas 32:10-34:15, 38:59-39:06).

**B8. AI (paid per use) (sas 15:53-21:31; f16:15, f17:45-f20:45).**
- Imagine with AI: lifestyle and model images, own-model replication, AI avatars. Inputs: template, scene prompt, primary/secondary image, quality, format, background, compression.
- Catalog Enrichment from product URL / existing product / image / part number covers titles, specs, SEO and media.
- **Brand Guidelines** (tone, ideology, ICP, audience, pain points, value propositions, preferred and banned terms, brand name, tagline, logo) "steer every enrichment and generation".
- Prices: ₹25-30 per content item; images ₹80-100 (720p) to ₹200 (4K); a 10-second video ₹1,000-1,200. Retries: 3 for content and images, 2 for video. A bring-your-own Gemini key is possible, with a margin.

**B9. Reports, admin, support (sas 21:43-24:03; f22:45-f24:15).**
- Consolidated **job history** of imports, exports and jobs. Catalogue analytics (completeness, price distribution) as custom widgets.
- Users: invite; per-user permissions per module (Templates, Products & Inventory, Sales & Orders, Reports, Tickets, Settings); **which templates a user sees**; manage vs read-only. "You can only assign permissions you have."
- Tickets: Issue / Feature / Onboarding / Bespoke. Statuses: Pending, In Progress, Waiting for Support, Waiting for Customer, Resolved, Closed, Escalated, Approved. Projects; Slack integration.

### 2C. The catalogue lifecycle both tools serve (Kushal's process)

1. The item master is born in GoFrugal: design no., combination ID, MRP, barcode.
2. Products are imported into PIM: Unbxd in three passes; Saastify proposes one merged nightly feed.
3. The catalogue team (Raju + 4) opens the day's **CAT batch** as a dynamic group (CAT-NAME = CAT-240) and enters parent-level attributes once per design by attribute group: catalogue team, stylist team, visual (images), content team (unb 15:05-16:25, 37:24-37:57).
4. The photographer task fills image1… Amazon-specific dropdowns are filled by whoever owns Amazon; ownership is unclear today (unb 55:03-56:01).
5. QC → **Certify** into a certified group.
6. Export runs per channel template: Shopify through the app; Amazon by SP-API app per category template; Myntra by Excel email per category workflow.
7. Exports are watched for failures, fixed, and re-exported.

## 3. Gap table

Our ERP column: **Have / Partial / Missing / Planned (id) / Not adopt**.

| # | Capability | Vendor reference | Our ERP | Evidence | Note |
|---|---|---|---|---|---|
| 1 | Attribute definitions (code, label, type, options, group, unit) | unb 02:53-04:30; sas 11:31 | Have | `db/migrations/migration.sql:961-965`; group `migrations_stage26_4_pim_maturity.sql:26`; unit/used_for `migrations_stage81_1_attribute_library.sql:35-37` | Types only Data/Number/Select/Date |
| 2 | More attribute types: Boolean, multi-valued, long text, link/URL, image/file, tree list | unb f03:45, f04:15; sas f42:30 | Partial | value_type options `'Data,Number,Select,Date'` (`migration.sql:963`). Grep `multi_value\|multivalue\|is_multi\|MultiSelect` in engines/db finds nothing | Tags and stones are multi-valued in jewellery |
| 3 | Attribute value checked against its definition (type, allowed values) on save | unb f25:00 (accepted values) | Missing (verify) | `ProductAttributeValue.value` is `Data` (`migration.sql:983`). No ProductAttributeValue validator in `engines/` (grep finds only reads: `pim.go:168`, `pim_related.go:59`, `pim_reports.go:43`). UI saves free text (`public/view-pim.js:1786-1818`) | Data-quality hole |
| 4 | Attribute groups | unb f14:30 | Have | `migrations_stage26_4_pim_maturity.sql:14-27` | |
| 5 | Per-attribute (group) edit permission by role or team; searchable flag | unb f06:45; sas 22:40 | Partial / Planned (47.13.4) | Field-level RBAC exists for doctype fields (`engines/field_permissions.go:60-66,189`), but attributes are rows, so it does not reach them. Stewardship per attribute is open `micro_checklist.md:423` | |
| 6 | Bulk upload of attribute definitions; industry attribute library | unb f03:45 | Have | Generic CSV import on ProductAttributeDef; library of ~670 attributes `engines/attribute_library.go:10-21`, `attribute_library_apply.go` | |
| 7 | Category-specific attributes (category templates) | sas 03:08, 14:52-15:16 | Have | `engines/item_attribute_map.go:13-24` (attribute map by ProductType / Category); family mandatory attributes `pim.go:97` | |
| 8 | Category hierarchy (multi-level tree) | unb f00:00 (2,391), f14:30; sas 14:55 | Partial | Category is flat: code/name/status/attribute_map/mrp_markup (`migrations_stage65_3_item_names.sql:42-44`, `stage89_1:36`, `stage94_2:11`) + Product Type; channel category via `ChannelCategoryMap` (`migration.sql:1095-1098`) | Marketplace nodes are covered by the category map |
| 9 | Standard core schema plus free custom attributes | sas 11:31-12:37 | Have | Item core fields + ProductAttributeDef + library | |
| 10 | Parent/variant (design / combination) with variant options | unb 11:03-12:05; sas 03:38 | Have | `engines/po_design.go:12-14`; variant uniqueness `pim.go:619-690`; Shopify option order `shopify_sync.go:285` | |
| 11 | Enter an attribute once at design level; combinations inherit unless overridden | unb 12:30-14:46 | Partial | `ResolveAttributeValue` resolves item → locale/channel only, with no parent fallback (`pim.go:160-179`). ERP-level `design_attributes` exist on the design (`po_design.go:150,226`) but not for PIM attributes | Catalogue team works at parent level |
| 12 | Completeness score (core + family mandatory + approved content) | sas 02:59-03:08; unb f00:00 | Have | `engines/pim.go:285-375` | |
| 13 | Channel-scoped completeness | sas 14:10-14:26 | Have | `pim.go:341-358` (ChannelFieldMap mandatory=Yes) | |
| 14 | Mandatory/validation per marketplace **category / product type** | unb f25:00, f26:30 | Missing | ChannelFieldMap fields: code, channel, source_field, target_field, mandatory, transform_rule only (`migration.sql:1104-1109`, `migrations_stage36_5_transform_rules.sql:60`); no category scope | Amazon jewellery: 13 product types |
| 15 | Channel view of a product (that channel's columns, fill %, edit in place) | sas f01:30 | Partial | Readiness, "Not ready for…" and publish preview exist (`view-pim.js:1602,1658`; `pim_publish.go:38,580`). No edit-in-channel-layout view | |
| 16 | Workbench queue with "what to fix next" | — | Have | `pim.go:421` ListWorkbench; USER_GUIDE §8a (`docs/guides/user-guide.md:1802`) | |
| 17 | Enrichment status lifecycle | sas f05:15 | Have | `migration.sql:1065` (Draft…Published, Publish Failed, Archived) | |
| 18 | Marketing content per language (title, short/long, SEO, tags), approval-gated, versions and rollback | unb f14:30 (05 Content Team) | Have | `migration.sql:993-1001,1022-1023`; `engines/pim_content_versions.go:29,80` | |
| 19 | **Certify:** sign off the whole product (attributes + media + content) for channel export; "changed since sign-off" flag; feeds read the signed-off copy | unb 00:27-01:02, 16:25-18:08, f14:30 | Partial | Only ProductContent is approval-gated and snapshotted. Attributes and media are not; feeds read live data (`pim_export_template.go:279-323`). Payload hash exists (`pim_publish.go:205`) | Needs decision |
| 20 | Tasks: assignee/role, due, priority, comments, templates, My Work inbox, bulk actions | unb 26:34-28:49 | Have | `engines/pim_tasks.go:13-41`; `migrations_stage36_2_pim_tasks.sql:44-137` | |
| 21 | Task carries **the attributes to fill** for a product set (≤500), a focused entry grid, email to assignee, auto-close when filled | unb f27:15-f28:30 | Partial | PIMTask has no attribute-list field (`migrations_stage36_2_pim_tasks.sql:115-137`). Scope "Attribute Set" is never used (grep finds only the option). No email (grep `notif` in `pim_tasks.go`) | |
| 22 | Readiness task raised from a readiness report row | unb f26:45 | Have | 36.2.6 (closed Stage 36, `docs/archive/legacy/parity-master-plan.md:196`) | |
| 23 | Enrichment workflow stages with entry/exit conditions | — | Have | `engines/pim_workflow.go:111-216` | |
| 24 | Dynamic product group by **attribute = value** (CAT batch), category, several conditions | unb 15:05-16:25, f16:30 | Partial | Filters are only family / status / completeness_below / missing_attribute (`pim_product_groups.go:65-80,190-213`) | Kushal's daily unit of work |
| 25 | Static product groups | unb f17:30 | Have | `pim_product_groups.go:96-120` | |
| 26 | Bulk edit one field across a selection or group | sas f13:15 | Have | `engines/pim_bulk.go:37`; route `routes.go:1162` | Doctype fields only |
| 27 | Bulk **set an attribute value** (and add/remove a tag token) across a group | unb f29:00 (bulk delete tags) | Missing | Bulk edit takes `doctype` + `field` (`internal/server/handlers_procurement_pim2.go:232-265`); attribute values are separate rows | |
| 28 | Grid data entry: many products × one attribute group | unb 17:48, f14:30 | Missing | The PIM panel saves one attribute at a time (`view-pim.js:1786-1818`); no grid editor found | |
| 29 | Multi-SKU paste search | sas f00:00 | Missing | Grep `paste.*sku\|several skus\|codes separated` in `public/`, `engines/`, `internal/server/` finds only a comment (`app.js:555`) | Small |
| 30 | Related products (auto) | sas 05:40 | Have | `engines/pim_related.go:8-28` (same family + shared attributes) | |
| 31 | **Typed relationships** (similar, alternative, complete-the-look, upsell, cross-sell, accessory, bundle, add-on), manual + CSV, pushed as Shopify metafields | sas 04:31-08:38, f05:30 | Missing | Grep `upsell\|cross.sell\|ProductRelation\|related_product\|addon` in engines/db finds nothing | |
| 32 | Price lists per country/currency; per store | sas 04:05-04:28 | Have | `migrations_stage37_6_deferred_prepaid_recurring_pricelist.sql:91-99`; Shopify store price list `shopify_sync.go:484` | |
| 33 | Collections (manual / automated rules) | sas 00:17-01:09 | Missing → Not adopt | Grep `collection` in engines/view-pim: no PIM hits | See §5 |
| 34 | CSV import of any record with preview, dry-run, row errors | sas 08:46 | Have | `BulkImportCSV`; `/api/v1/pim/import/{doctype}/preview` (`routes.go:1213-1214`) | |
| 35 | Import templates (supplier header → field, per-column transform, default) | sas 11:15-11:26 | Have | `engines/pim_import_template.go:12-25` | |
| 36 | Scheduled / drop-folder / webhook imports | sas 10:18-10:34 | Have | `engines/pim_import_schedule.go:19-23` | |
| 37 | **One wide product sheet** (item fields + PIM attributes + content in one row) imported in one pass; merge, no step1/2/3 | sas 09:07-09:36, 40:44-40:56 | Partial / Planned (47.13.1) | A template targets one record type (`pim_import_template.go:58`); attribute values import only in long form (one row per item × attribute). Staging pipeline is open `micro_checklist.md:420` | Kushal's named pain |
| 38 | Variant-aware import (parents + variants in one file) | sas 41:23-41:40 | Have | 36.3.5 `pim_import_template.go:223-236`; family template `:373-395` | |
| 39 | Channel catalogue import (pull products from Shopify to seed PIM) | sas 08:46-08:55, f08:45 | Partial | Link by SKU `shopify_sync.go:549`; per-item read-back `pim_publish.go:677`; no catalogue pull that creates items | Onboarding only |
| 40 | Transform vocabulary: trim, case, prefix, suffix, truncate, default, find-replace, number/date format | sas 42:05-42:37 | Have | `engines/pim_transform.go:56-155` | |
| 41 | **Value map** (lookup table), **join** of several attributes, **set constant**, **pad** (EAN), regex replace, strip/convert HTML, split, conditional fallback | unb f25:15-f25:30, 06:41-08:45; sas f42:30 | Partial | None of these is in the 10-function vocabulary (`pim_transform.go:56-155`). Concat, split, regex-extract, lookup-map and unit-convert were in the 36.5.1 plan (`parity-master-plan.md:216`) but were not built | Chained literal replaces are what broke Unbxd's colour script |
| 42 | `default_if_empty` on a channel field map | — | **Bug** | `engines/connector.go:201` applies the rule only when `val != ""`, so a blank value never reaches `default_if_empty` on export | A constant column cannot be expressed |
| 43 | Channel-only override value (Amazon-only entry wins over the generic value) | unb 05:18-08:45 | Have | ProductAttributeValue.channel override (`migrations_stage26_4_pim_maturity.sql:30-31`); resolved in payload `connector.go:199` | The override is also passed through the field's transform |
| 44 | Rules on import side (stored) vs export side (channel only) | unb 08:57-10:58 | Have | Import-template rule `pim_import_template.go:12-16`; field-map rule `connector.go:201-211` | |
| 45 | Code-script transformations | unb f25:30 | Not adopt | 36.5.4 explicitly rejected (`parity-master-plan.md:219`) | |
| 46 | Export templates (columns, order, headers, headerless, parent-only) | sas 13:59 | Have | `migrations_stage36_4_export_syndication.sql:59-69`; `pim_export_template.go:239-366` | |
| 47 | Ready-made channel layouts (Shopify, Amazon, Myntra) | unb 22:52 | Have (shallow) | `engines/pim_channel_packs.go:40-85`: 11-14 columns each, apparel-oriented (fabric, sleeve) | No jewellery columns |
| 48 | **Load the marketplace's own template file** (xlsx/xlsm) → columns, mandatory/optional, accepted values, per product type | unb 23:31-24:29; sas 43:24 | Missing | Grep `xlsm` finds nothing; an XLSX reader exists to build on (`engines/xlsx_reader.go:15-36`) | |
| 49 | Template mapping coverage (mapped X/Y, mandatory/optional) and "Analyze template" issues | unb f25:00; sas f42:30 | Missing | Only a per-item `channel-mapping-gap` report (`pim_reports.go:39-53`) | |
| 50 | Per-column validation: accepted values, length, type, **conditional mandatory** (metal type ⇒ metal stamp) | unb f25:00, 44:38-45:21; sas f44:15 | Partial | ChannelValidationRule types: Min Images / Max Title Length / Required Tag only (`migrations_stage26_4_pim_maturity.sql:91`; `pim_publish.go:180-196`) | |
| 51 | Feed by family/category, CSV/XLSX, skip-and-list | unb 19:31 | Have | 140.12.24 `micro_checklist.md:4448`; `pim_export_template.go:253-366` | |
| 52 | Feed by **product group** / signed-off group / **only ready items** | unb f22:30 (readiness), f36:15 | Missing | `PIMExportFilter` = Family, Category, Format (`pim_export_template.go:255-259`) | |
| 53 | Several image columns (main + other 1…8) and a per-channel image slot (white-background main for Amazon) | unb 47:31-52:40 | Partial | Feed `image_url` = one main image only (`pim_export_template.go:377-381`); media roles exist (`migration.sql:1074`) | |
| 54 | Stable public image URL per image for marketplaces/Unicommerce | sas 32:10-34:15 | Planned / needs decision (140.12.24) | `micro_checklist.md:4414,4448`: image_url fills only from public http(s) paths, and a signed public media route is still undecided | |
| 55 | Marketplace parent/child rows (parentage, variation theme) | unb 52:47-55:00, f54:15 | Partial | Feed variant_mode is All Rows / Parent Only (`migrations_stage36_4:66-67`); no parent row or parentage value; Amazon push sends no variation (`connector_amazon_flipkart.go:190-214`) | |
| 56 | Scheduled export delivery | unb A8; sas 10:18 | Partial | `PIMExportSchedule` Daily/Weekly/Monthly runs the **whole** template with no filter (`pim_export_schedule.go:125-130`). Email is simulated: outbox log only, no SMTP (`pim_export_schedule.go:13-20`, `outbox.go:185-200`). Webhook goes via WebhookSubscription (`outbox.go:175-183`) | |
| 57 | Export job history: counts, failed list with reasons, download, re-export | unb f36:15-f36:30; sas 21:43 | Partial | Per-item publish log (`pim_publish.go:310`, `payload_snapshot`/`error_code` `migrations_stage26_4:101-102`); a feed is a one-off download with no run record. Import jobs are recorded (`migration.sql:1112-1119`) | |
| 58 | Export by email / SFTP | unb 19:38-20:07 | Partial / Not adopt (SFTP) | Email simulated (#56); SFTP deliberately out (`migrations_stage36_3_import_depth.sql:56-57`) | |
| 59 | Automation recipe: filter → export, scheduled or manual, last-run status | unb 28:49-36:39, f29:00-f30:30 | Partial | #56 + #24; no filter step | |
| 60 | Shopify publish: design as product + variants, price/compare-at, stock, linked, multi-store | unb 36:46; sas 12:53 | Have | `shopify_sync.go:372-548`; `pim_publish.go:526-556`; stores `shopify_sync.go:163` | Not live on app (`capability-maturity.md:62`) |
| 61 | **Shopify publish carries PIM enrichment:** approved title/description/SEO, tags, product type, attribute metafields | unb f13:30 (metafields, tags), f36:15 | **Missing** | `publishShopifyOne` sends design name, family description, brand and HSN (`shopify_sync.go:447-462`); `ProductType` is never set; the PIM payload is built only for the log (`pim_publish.go:526-531`) | Biggest Shopify gap |
| 62 | Image changes after first publish reach Shopify | sas 32:10-33:04 | Partial | "Pictures go up with the first publish only" (`shopify_sync.go:523`) | |
| 63 | Amazon SP-API listing push | unb f45:45-f46:45 | Partial | PUT listings with one `product_type` per credential (`connector_amazon_flipkart.go:190-214`); no per-category type, no variation | |
| 64 | Myntra by spreadsheet | unb 02:46 | Have | XLSX feed 140.12.24 | |
| 65 | Product shows the channels it is live on, with deep links | sas 12:59-13:57 | Partial | `shopify_links` holds product_gid per SKU (`shopify_sync.go:212-223`); no link surfaced in the PIM list (not verified in UI) | |
| 66 | Channel read-back / diff | — | Have | `pim_publish.go:677-732` | |
| 67 | Media library: roles, versions, alt text, expiry, checksum dedup, tags/search | sas 15:16; unb 51:15 | Have | `migration.sql:1072-1082`; `migrations_stage26_4:44-48`; `engines/pim_dam.go:15-19`; `/pim/media/search` | |
| 68 | Renditions / resize presets | sas f16:15 | Have | `pim_dam.go:35-40` (small/medium/large/square; JPEG/PNG only) | |
| 69 | Bulk image upload with **file-name conventions** (SKU_1/SKU-1 → position, folder per SKU, design vs combination) | sas 44:02-45:22, f45:15 | Partial | PIM zip accepts only `ITEMCODE__role.ext` (`pim_dam.go:177-200`); the PO Images page matches design/combination numbers with suffixes (`owner_images.go:345-353`). No position → sort_order | |
| 70 | Watched folder: images dropped on the client drive are ingested | sas 46:44-47:48 | Missing | Import schedules scan CSV drop dirs only (`pim_import_schedule.go:19-23`); the image drive is the write target only (`image_drive.go:23-35`) | |
| 71 | Client-owned image storage (S3-compatible) | sas 33:04 | Have | `engines/image_drive.go:23-35` (Stage 95) | |
| 72 | One asset linked to many products; DAM folders | sas 15:46 | Partial | ProductMedia links one `item` (`migration.sql:1073`); duplicates collapse by checksum | Low value |
| 73 | AI image/video generation, AI enrichment credits, brand-guideline LLM steering | sas 15:53-21:31 | Not adopt | Local-only decision `engines/pim_content_assist.go:12-27`; D8 `parity-master-plan.md:450,463` | |
| 74 | Deterministic content assist (marketplace title, bullets, meta) | sas f17:45 | Have | `pim_content_assist.go`; 36.7.1 shape=marketplace | |
| 75 | Brand vocabulary rules (preferred/banned terms) checked before approval | sas f18:30 | Missing | Grep `banned\|preferred term` finds nothing | Optional, small |
| 76 | PIM dashboard counters | unb f00:00 | Have | `pim_reports.go:17-26,100` | |
| 77 | Core-attribute coverage gauges, attribute type distribution, per-attribute fill rate | unb f00:00 | Partial | Quality reports `pim_reports.go:39-53`; catalogue reports `report_catalog_oms_pim_crm.go:1032-1045` (completeness, missing attribute, media coverage, channel readiness, change audit). No per-attribute fill-% | |
| 78 | Job history across imports + exports | sas 21:43-21:52 | Partial | ImportJob only (#57) | |
| 79 | Users and per-module permissions | sas 22:40-22:56 | Have | role_permissions + `field_permissions.go` | Per-template visibility missing (low) |
| 80 | Duplicate detection / product merge | sas nav "Product Merge" | Planned (47.13.5) | Duplicate-items report built (`micro_checklist.md:424`); merge not built | |
| 81 | GTIN/EAN generation and check digit | unb 43:12-44:01 | Have | 36.7.4 (`micro_checklist_closed_stages.md:752`) | Internal 020-029 GTINs must not go to Amazon as external_product_id |
| 82 | Tickets / projects inside PIM | sas 22:59-24:03 | Not adopt | — | |

## 4. Missing / partial pieces, ranked

Sizes: S ≤1 session, M 1-3, L 3+.

**1. Shopify publish carries PIM enrichment (#61, #62).** Size M.
- **What:** the linked Shopify publish sends the approved PIM content and attributes, not just the design name and brand.
- **Rules:**
  - Only **Approved** ProductContent for the channel's locale is sent: title, description HTML, SEO title/description, tags.
  - A field map whose target is `custom.<key>` (or `<namespace>.<key>`) becomes a product metafield, typed from the attribute type.
  - Product type comes from Category or Product Type.
  - A design-level value goes on the product; a combination-level value goes on the variant.
  - Images re-sync when the checksum set of the owner's active images changes: add new, remove retired, never duplicate.
  - An unapproved change never publishes.
- **Why:** Shopify is Kushal's storefront. Unbxd pushes metafields and tags today (unb f13:30), and Shopify smart collections run on tags and type.
- **Fit:** `publishShopifyOne` (`shopify_sync.go:442`) consumes the `BuildChannelPayload` it already builds and discards (`pim_publish.go:532`). Add tags/SEO/metafields to `ShopifyProductInput` (`connector_shopify_sdk.go:919`).
- **Decision:** metafield naming convention. Recommended: field-map target `custom.<attribute_code>`, with metafield definitions created on first publish.

**2. Channel templates from the marketplace's own file, per category, with validation (#14, #47-#50, #55).** Size L.
- **What:**
  - Upload Amazon's or Myntra's category template (XLSX). It creates an export template per product type, with its columns, mandatory/optional flag and accepted-values list.
  - Show mapping coverage (mandatory X/Y, optional X/Y).
  - Validate each item per column before export and readiness.
- **Rules:**
  - Mandatory, accepted-values and max-length rules are scoped to channel + channel category.
  - Conditional mandatory: "if column A filled then B, C required".
  - A value outside the accepted list fails that item, which is skipped and listed with column and reason. It never blanks silently.
  - Coverage counts only active columns.
  - Parentage rows: optional parent row per design with a variation theme column, only when every combination differs on a supported axis; otherwise list combinations standalone.
- **Why:** Amazon jewellery is 13 product types × 170-198 columns, and Kushal's has been stuck at "98%" for a month on exactly these rules (unb 26:06-26:22, 43:00-45:33).
- **Fit:**
  - `ReadXLSX` (`engines/xlsx_reader.go:36`).
  - Additive fields on `ChannelFieldMap` (`channel_category`, `allowed_values`, `max_length`, `required_if`).
  - Extend `ChannelValidationRule` rule types.
  - Checks run in `RunPIMExport`'s skip list (`pim_export_template.go:307-310`) and `CheckPublishReadiness` (`pim_publish.go:38`).
  - Extend the channel packs (`pim_channel_packs.go`) with jewellery columns.
- **Decision:** whether templates are per channel+category (recommended) or one wide template with category-scoped rules.

**3. Transform vocabulary completion and the default bug (#41, #42).** Size S-M.
- **What:** value map, join, set-constant, left-pad, RE2 regex replace, strip HTML, split-take-Nth, and "first non-empty of A, B".
- **Rules:**
  - A value map is an exact-match table, optionally case-insensitive, with an unmatched mode (pass through / blank / fail item).
  - Join adds the delimiter only between non-empty parts.
  - A constant needs no source value.
  - Fix `connector.go:201` so `default_if_empty` and constants run on blank values.
- **Why:** colour codes (AMT GREEN → Amethyst Green), Jhumka → Jhumki, composed titles, age_range = Adult. Unbxd's chained `replace()` calls corrupt substrings ("BLACK" inside "BLACK/PEARL").
- **Fit:** new functions in `pimTransformFunctions` (`pim_transform.go:56`); a `PIMValueMap` master (JSONTable from→to) referenced by the `value_map` step; `ChannelFieldMap.extra_sources` (additive) for join and fallback. RE2 is linear-time, so no ReDoS concern.
- **Decision:** value maps as their own record (recommended, reusable across channels) vs inline in the rule.

**4. One wide product sheet import (#37).** Size M.
- **What:** one row per design or combination. Columns are Item fields + PIM attributes (by code or label) + content (title/description per language). The header mapping is saved once as an import template. One preview, one commit, updates by SKU or design.
- **Rules:**
  - Unknown headers are listed, never silently dropped.
  - An attribute value is checked against its definition (#3).
  - Blank cells do not erase unless "blank clears" is ticked.
  - Content arrives as Draft and goes through approval.
- **Why:** this removes the step-1/2/3 imports of the same file (sas 09:07-09:36). It is also how agency and supplier sheets arrive.
- **Fit:** `PIMImportTemplate` gains target "Product sheet" and fans out inside the same `importBatch` transaction (`pim_import_template.go:18-25`); `BulkImportCSV` core. It aligns with **Planned 47.13.1** staging and must not fork it.

**5. Design-level entry, grid entry, bulk attribute set (#11, #27, #28).** Size M.
- **What:**
  - An attribute value set on the design applies to all its combinations unless a combination has its own.
  - A grid for a product group × an attribute group, edited and saved as one batch.
  - "Set attribute X = v" and "add/remove tag t" across a group.
- **Rules:**
  - Resolution order: combination+channel → combination → design+channel → design.
  - Bulk actions are bounded by `pim.max_bulk_edit_documents` and validated per row; all-or-nothing.
  - The audit log shows one entry per changed value.
- **Why:** Kushal's enters jewellery attributes once per design for 4+ combinations, by team column set (unb 14:27-14:46, f14:30).
- **Fit:** `ResolveAttributeValue` fallback (`pim.go:160`); `ResolvePIMBulkTargetIDs` (`pim_product_groups.go:292`); grid in `view-pim.js` Workbench.

**6. Feed runs: filtered, recorded, delivered (#52, #56-#59).** Size M.
- **What:** an export recipe is template + product group + "only items ready for this channel" + format + destination (download / real email / webhook / channel API), run now or on schedule. Every run stores rows, skipped items with reasons, the file and re-run.
- **Rules:**
  - The readiness filter is on by default.
  - A run with skips is "Exported with skips", not "Delivered".
  - Email uses the real SMTP sender, and attachments respect size limits.
  - A schedule runs its recipe only.
- **Why:** this replaces Unbxd's 11 Myntra workflows and the hour-later failure checks (unb 35:04-36:39).
- **Fit:**
  - `PIMExportSchedule` gains `product_group` and `ready_only`.
  - A new `PIMExportRun` record in the ImportJob shape.
  - Real mail through `engines/document_mail.go:62` instead of the outbox log (`outbox.go:185`).
  - `PIMExportFilter` gains a group (`pim_export_template.go:255`).

**7. Dynamic groups by attribute value (#24).** Size S.
- **What:** filter rows of attribute / field + operator (equals, contains, empty, not empty, in list) + value, ANDed; plus category and product type.
- **Rules:** a typed operator allowlist (no free SQL), as now.
- **Why:** "CAT-NAME = CAT-240" is the catalogue team's daily batch (unb 15:05-16:25).
- **Fit:** `productGroupFilters` / `matchesDynamicProductGroup` (`pim_product_groups.go:65-213`). A JSONTable of conditions, additive.

**8. Channel sign-off (certify) (#19).** Size M.
- **What:** sign off a product group for a channel. The system snapshots each item's channel payload; the product shows "changed since sign-off"; feeds and publish can be set to send only signed-off items, or the signed-off snapshot.
- **Rules:**
  - Sign-off is a maker-checker approval: the preparer cannot sign off their own.
  - Any change to content, attributes or media flips the item to "changed since sign-off" by payload hash.
  - The signed-off snapshot is never editable.
- **Why:** this is Kushal's QC step before marketplaces (unb 16:25-18:08).
- **Fit:** approval engine; `computePublishPayloadHash` (`pim_publish.go:205`); `pim_publish_log.payload_snapshot` (`migrations_stage26_4:101`).
- **Decision:** the vendor model keeps a second editable copy. Recommended: snapshot + hash only. A second editable copy doubles the data and the confusion.

**9. Image slots per channel and stable public URLs (#53, #54, #69, #70).** Size M.
- **What:**
  - Ordered image positions per design or combination (1…n from the SKU_1/SKU-1 file suffix).
  - A per-channel "main image" choice (white-background shot for Amazon).
  - Feed columns `image_url_1…8`.
  - A stable public URL per image.
  - A watched folder on the client drive.
- **Rules:**
  - Position comes from the file-name suffix; re-uploading the same position replaces the image and keeps the old one inactive.
  - The URL never changes for the life of the image.
  - Feeds take only Active images.
- **Why:** Amazon/Myntra feeds and Unicommerce need image URLs. Shopify's changing URLs are Kushal's stated pain (sas 32:10-34:15).
- **Fit:** `ProductMedia.sort_order` + `media_role`; a `channel` field on ProductMedia (additive); `owner_images.go` name matching; `image_drive.go`.
- **Decision (already open, 140.12.24):** recommended a per-image unguessable token URL served read-only by the ERP (same digest-token pattern as `pim_catalog.go:15-25`), or the client drive's public base URL when the bucket is public.

**10. Tasks with an attribute checklist (#21).** Size S-M.
- **What:** a task template lists attributes. The task covers a product set; the assignee opens a grid of just those attributes × products; the task auto-completes when all are filled; the assignee gets an email.
- **Rules:** the exit check reuses `attribute_present`; max products per task is a setting (vendor: 500).
- **Fit:** `PIMTaskTemplate.attributes` (additive); `pim_workflow.go:149` condition; reuse the grid from #5.

**11. Attribute types and value validation (#2, #3).** Size S-M.
- **What:** types Yes/No, Multi-select, Long text, URL. Every value is checked on save against its definition.
- **Rules:**
  - Number parses; Date is ISO.
  - Select/Multi-select values must be in the options (case-insensitive match, stored canonical).
  - Multi-select is stored comma-joined in option order.
  - Invalid values are refused with the attribute named.
- **Why:** tags, multiple stones and polish need multi-values; free-text values break marketplace enums.
- **Fit:** a ProductAttributeValue validator at the `ValidateDocument` choke point (`engines/doctype.go:411`). Additive value_type options.

**12. Typed product relationships (#31).** Size M.
- **What:** relationships of type Similar, Complete the look (set: necklace + earrings), Alternative, Upsell, Cross-sell, Accessory / Add-on (earring backs), Bundle. Manual and CSV.
- **Rules:**
  - Directional unless marked mutual; no self-links; only Active items.
  - Published to Shopify as product-reference list metafields (depends on #1).
- **Fit:** a new `ProductRelation` doctype (CSV through `BulkImportCSV`); `pim_related.go` suggestions feed it.

**Smaller items (each S):**
- Per-attribute fill-rate and core-coverage report (#77), via `RegisterReport`.
- Multi-SKU paste search on the Item and PIM lists (#29).
- "Live on" channel links from `shopify_links` (#65).
- Per-category Amazon `product_type` in the SP-API push (#63, follows #2).
- Brand vocabulary check on content submit (#75, optional; banned terms block, preferred terms suggest).
- Attribute-group edit permissions per role (#5). This is **Planned 47.13.4**; build there, not separately.

## 5. Not worth adopting

- **AI generation: content, lifestyle/model images, video, credits, brand-guideline prompting, bring-your-own Gemini key** (sas 15:53-21:31). This conflicts with the 26.4.11 local-only decision (D8). The deterministic content assist already covers marketplace titles and bullets.
- **Python/JS transformation scripts** (unb f25:30). Rejected in 36.5.4. #3's closed vocabulary covers every script shown in the recording.
- **"AI Map" auto-mapping** (sas f42:30). An exact or normalised header-name match when loading a template (#2) gets most of the value with no model.
- **Collections management** (sas 00:17-01:09). Shopify already runs automated collections from tags, type and vendor. Pushing those (#1) is enough; a second collection editor duplicates Shopify.
- **Ticketing and projects inside PIM** (sas 22:59-24:03). Not PIM.
- **Google Drive listeners / n8n flows** (sas 46:44-47:48). Use the client's S3 drive + #9's watched folder. No third-party automation runtime.
- **SFTP export/import** (unb 19:38). Deliberately out (`migrations_stage36_3_import_depth.sql:56-57`). Drop directory + webhook instead.
- **Nightly GoFrugal→S3→PIM sync and inventory in PIM** (sas 10:18-10:34, 39:24). Our PIM sits on the ERP item master, so there is nothing to sync.
- **ML relationship recommendations** (sas 06:47-07:29). A bespoke paid service. Manual + CSV + `pim_related.go` suggestions suffice.
- **Several app variants per store** (Shopify, Shopify GraphQL, "installed from Unbxd", sandbox; unb f30:30). One channel per store; the multiplicity is what caused the "wrong template chaos" (unb 36:46).
- **"Without readiness" as the default export, and page-size-capped "export all" workarounds** (unb 19:01-21:25). Our readiness gate stays on, and feeds are not page-bound.
- **Zero-padding internal codes into EAN** (unb 43:45). Wrong practice. Internal-range GTINs (020-029) must be blocked from `external_product_id`.

## 6. Unsure / to verify

- **#3** (no server-side check of attribute values): concluded from the absence of any ProductAttributeValue-specific code. Confirm with a quick test before building.
- **Parent Only feed mode with design-based items** (`pim_export_template.go:196-235`) assumes a variant's parent is an Item. For Stage 87 combinations the parent is a ProductFamily (design). A "Parent Only" feed may drop those combinations entirely. Unverified; worth a test.
- **#65:** whether any screen already links to the Shopify product was not checked in the UI.
- **Saastify menu items not demoed:** Brand Portals, Vendors, Product Merge, Sales & Orders. The blueprint only names them.
- **Unbxd "readiness check":** described only as "a filter" (unb 21:19-21:25). The exact rule set is unknown; Task Type "Readiness Task" suggests a template-based completeness check.
- **Transcript garbling:** names (Shambhavi = Unbxd POC; Raju/Naveen/Shashikala = catalogue team) and the "Velu/Velo" speaker are approximate. The ₹ prices are as spoken.
