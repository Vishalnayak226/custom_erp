---
doc_id: DOC-78F8FF850A-05
title: User Guide: Labels and printing documents
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

# User Guide: Labels and printing documents

Part 5 of 9 of the [User Guide](user-guide.md). Previous: [Receiving goods and moving stock](user-guide-receiving-and-transfers.md). Next: [Master data](user-guide-master-data.md).

## 7A. Printing Barcode Stickers and Labels

**Inventory → Sticker Printing.** This is where item labels come from — the small barcode tags that go on the product itself. The screen is the print run itself. What a label looks like (**sticker templates**) and which template each item's label uses (**template rules**, §7A.5) are on **Setup → Printing → Templates** with every other template — the **Templates** button at the top of Sticker Printing goes there.

**Day to day, in two clicks:**

| To | Click |
| --- | --- |
| Print a GRN's stickers | **+ Print Stickers**, pick the GRN, then **Print** in the preview |
| Print a row again | **Print history** → **Reprint** on the row, then **Print** in the preview |
| Move the print sideways on the tags | **Printer preset** → **Line up**, **Print**, then tap the letter of the best row (§7A.6) |

> **Before your first print run you need one Printer record.** Setup → Printing → **Printer**, Active, one record per physical label printer. Without one, the Printer dropdown on this screen is empty and printing stops with "Select a printer first." (On this screen you always pick the printer yourself, so **Default For** = `Sticker` is not required here — it is what makes the *other* print screens one-click.) Getting labels to come out *silently* — straight to a **TSC (TSPL)** or **Zebra (ZPL)** thermal printer with no browser dialog — is a separate one-time per-PC setup covered in **[qz-printing-setup.md](qz-printing-setup.md)**; the Printer record needs its **Printer Language** (TSPL or ZPL), **Printer DPI** and **OS Printer Name** filled in. Until that is done, printing still works, it just opens the normal browser print dialog (as roll-sized pages or an A4 sticker sheet — §7A.2, *Label tab*).

### 7A.1 Print the labels for a GRN or a Transfer Order

This is the normal way to print. You don't type SKUs — you point at the document the goods arrived on, and the system reads its own lines.

1. Go to **Sticker Printing**. The **Print** tab opens by default, on the **Print history** — every run, who printed it, from which document, and when.
2. Click **+ Print Stickers**. In the **Print from Transaction** panel, pick **Goods Receipt (GRN)** or **Transfer Order** in the **Module** dropdown - or any other transaction your company has that carries items (purchase order, sales order, purchase return, sales invoice, return, shipping package, ASN, vendor quote, RFQ...; a new one appears in the list by itself). You see only the transactions you may open.
3. **The quick way: pick the receipt, press Print.** The latest documents drop down by themselves under **Document**; pick one and its stickers' preview opens straight away - every line, its own quantity, on this computer's printer. Press **Print** there and they print. To change copies, a line's template or the printer first, **Close** the preview: the lines stay below (steps 4-6). GRN steps' **Print stickers** opens the same preview for that receipt.

   Or click **Document** to see the latest ones, or start typing the number — the list narrows as you type; pick the one you want. You can also type just the end of the number (for example `00048`) and click **Load**: when only one document ends that way it loads; when several do, or none, a message says so. The list shows your store's documents (all stores for an Admin), newest first, without cancelled ones - and it works for anyone who may print stickers, including a Store Manager or Cashier who cannot open the receipt itself. Hover a row for its date, store and status.
4. The line list appears, one row per SKU (or per SKU-and-lot, if a SKU arrived on more than one lot):
   - **Category** — what that item's Item Master says.
   - **Template** — the label layout that line will print with, and in brackets the rule that picked it: *item*, *item type*, *product type*, *category* or *default* (§7A.5), or **Standard label (no rule)** when no rule applies. To print a line on a different layout **this time only**, pick it in that line's dropdown — **Standard label** or any Active template. **Template for every line**, above the table, sets every line at once; choose **As per template rules** to go back. Nothing is saved to the item — the next print follows the rules again. To keep a pick for good, press **Keep for *(product type)*** beside it: that template becomes the product type's rule (the same setting as **Template rules → Product type**, where you can change it later), and every item of that type prints with it from then on — unless the item or its item type names a template of its own. When the chosen **Printer** has a roll set in its preset (§7A.6), a line whose template does not fit that roll shows ***roll* (printer preset)** and prints with it, and the dropdowns offer only templates that fit the roll.
   - **Batch/Lot** — shown only when at least one line on the document carries a lot number.
   - **Copies** — pre-filled with that line's own quantity. Overtype it if you want a different number.
5. Check the **Printer** in the panel just below the line list, and give a **Reprint reason** if this is a re-run of labels already printed once (optional, but it is kept in the print history against the run): pick one from the list beside the box when your company has reprint reasons (Setup → Reason Codes), and type a note if you like. When your company makes reasons compulsory (**Settings → Reasons must be picked from the reason codes**), a typed note alone is refused - pick from the list. The printer is already chosen for you: the one last used on this computer, or the first time, the printer set as **Default For: Sticker** for your store (Setup → Printing → Printer), or the only printer there is. Pick another for this print if you need to; the one you pick is remembered for the next print. If the label printer is off, out of labels or not answering, a box says the stickers did not print: **Retry** once it is fixed (it carries on from the labels that had not gone out), or **Print with the browser instead**.
6. Then either:
   - untick any rows you don't want (the box in the header ticks or clears them all) and click **Print Selected** — the ticked lines in one run; or
   - click **Print** on a single row — just that one line.

Three things it does for you:

- **A GRN prints against accepted quantity only.** If 100 arrived, 3 were rejected and 2 damaged, you get 95 labels — not 100. A line with nothing accepted doesn't appear at all, since there is nothing sellable to label.
- **Lots stay separate.** If the same SKU came in on two different batch/lot numbers, it stays as two rows and a **Batch/Lot** column appears so you can tell them apart. Each row is selected, counted and printed on its own, and each label carries its own lot and expiry.
- **The batch is on the label.** A GRN line's batch number (§6.3) prints on the built-in label as a second, smaller barcode under the SKU's, with the number written beneath it — so the day the piece arrived can be read off the tag, and scanning that barcode in **Batch Lookup & Ageing** finds this exact batch. A label printed without a document has no batch, and that barcode is simply left off. On your own templates, drag **Batch / Lot** from the Studio's **Data** panel onto the label (§7A.2).
- **Mixed categories come off grouped.** A GRN holding earrings and necklaces prints all the earring labels, then all the necklace labels, so the strip can be torn into one stack per category.

If the document loads but says *"That document has no stickerable lines"*, the lines were all rejected/damaged, or (for a transfer) the quantities are zero.

**Where the labels go.** With the print agent — **PrintBridge** (or QZ Tray) — running on this computer and a thermal printer chosen, labels print silently, dot-sharp, at the label's own size. That needs three things once: **PrintBridge** on this computer (the first print offers **Install** - see §7A.6); a **Printer** record (**Setup → Printing → Printer**) with **Printer Language** = TSPL (most 4-inch label printers, e.g. Y468BT) or ZPL (Zebra), the Windows printer name in **OS Printer Name** (use **Detect Printers**), and the **Printer DPI** (203 for most); and that printer picked on the print page. The wrong language prints nothing: a TSPL printer sent ZPL feeds blank tags. Otherwise the browser's print dialog opens, and **Cancel** there prints nothing.

**See it before it prints.** Every **Print** on this page — **Print Selected**, a row's **Print**, **Print Stickers** — first draws the print exactly as the printer will get it. Labels show the way the tag reads, and grey dashes mark each label's edge (the dashes don't print). The caption names the template and its size. It also says how many labels of a multi-label print (a "× 3" template, say) are left blank, and whether the print comes out turned 180°. **Print** in the preview prints it; **Close** prints nothing and adds nothing to the print history. With **Print on scan** on, each scan still prints straight away (that is what the box is for), and **Print Stickers** shows whatever is typed in the scan box, as that scan would print. The page it sends is the label's own size, but the Windows driver decides the paper: if the tag lands small or turned in the middle of a bigger label, set the driver's paper size to the roll (Printer preferences) or, better, use PrintBridge. An item with no barcode of its own prints its SKU as the barcode. **Printer setup on this computer (QZ Tray) and channel label PDFs**, collapsed at the bottom of the print page, holds the one-time setup — QZ status, **Detect Printers**, **Test Print** — and printing a marketplace label or invoice PDF exactly as the channel issued it. Its **Print** first shows a ZPL or text label file's contents, or for a PDF a link that opens it in a new browser tab; **Print** in that window prints it.

**A code for every piece (146.3.2).** For an item whose item type has **Code for every piece received** on, at a store the administrator listed, posting the goods receipt gives each accepted piece its own code from the **Piece code** series (`PC00000001`...). Rejected pieces get none. Type or scan codes into the line's serial numbers first if you have them - the system adds only the missing ones, and fewer codes than pieces is fine. The codes are kept on the receipt line, so printing stickers again prints the same codes and never makes new ones: one label per piece, the piece code as its barcode. Posting the same receipt twice does not mint again. On **RF**, **Piece codes** gives new codes for an item and a number of pieces. **Receive pieces** (146.3.2b) does it while you receive, on a goods receipt received in steps: scan the receipt or pick it from the list, pick a line, then scan each piece's code. Every scan is saved at once and the line shows how many pieces have a code. **Mint the rest** gives the missing pieces new codes; **Undo last** takes the last code off. A code already on another line of the receipt, or already in stock, is refused. Once the line is posted its codes cannot be changed here. Scan a piece code at the till and that piece sells at its own cost (§6.15).

### 7A.2 Design your labels (Templates and the Sticker Studio)

Out of the box, every SKU prints the **Standard label** — 50 × 25 mm with the name, the variant's values (for example **Silver / 7**; left off for an item with no variant), the SKU's barcode, SKU and HSN, and the batch barcode. To get any other look (a jewellery tail tag with weight and a QR code, a smaller earring tag, a 2-up retail roll), design a **template** in the **Sticker Studio**. You do this yourself; it needs no developer.

**Setup → Printing → Templates**, under **Stickers and barcodes**, shows every sticker template as a card with a live preview of the label, its size, DPI, how many elements it has and the categories it covers. **Edit** (or clicking the preview) opens it in the studio; **Duplicate** opens a copy to start a variant from. With more than six templates a **Filter templates…** box appears. The first card is always the **Standard label** (badge **Built-in**). It cannot be edited, but its **Duplicate** opens a copy of it in the studio, named *Standard label copy* — rename it, change what you like, **Save**, and it is a template of your own.

#### Start a template

1. **Setup → Printing → Templates** → *Stickers and barcodes* → **New template**. The studio opens full-screen. (**New template**, **Duplicate** and **Save** show only for a role allowed to make templates; anyone else can still open a design, preview it and **Test print**.)
2. **Start a new label** asks for the size of the roll you print on. Pick one of the standard sizes — **Jewellery tags** (tail tags 92 × 12, 95 × 12, 100 × 15, 100 × 14.3, small tag 70 × 10), **Retail** (38 × 25, 50 × 25, 50 × 38, 75 × 50), **Multi-up rolls** (2-up 38 × 25, 3-up 32 × 25), **Shipping** (100 × 50, 4 × 6 in), **Ready designs** (a jewellery **Square 2-up** on a 54.5 mm roll, a **Ring tag 97.6 × 11.9** and four **Seznik** designs for the Seznik 100 × 14.3 jewellery tag, the Ring layout on the taller tag (barcode panel, text panel, then the tail) — pick the one that matches how your roll feeds: **Seznik horizontal × 1** (one tag lying across the roll), **Seznik horizontal × 3** (three lying tags stacked down the roll in one print), **Seznik vertical × 1** (one tag standing along the roll, read top to bottom) and **Seznik vertical × 3** (three standing tags side by side — the Seznik roll with three tags across) — each already laid out with the barcode, name, colour, design, polish, weight, Combination ID, size and MRP; retype the sideways "Brand name" with your shop's name. The same designs are **Built-in** cards on the **Sticker templates** tab. Pick one straight from the template list on **Print** or in a template rule (§7A.5) and it is saved as your own template on the spot; **Duplicate** one to change it first) — or **Use the standard label (50 × 25)** to start from the Standard label (name, variant, the SKU's barcode, SKU and HSN, and the batch barcode) and drag it into your own arrangement, or **Blank 50 × 25**. Any size can be changed later.
3. Type a name in **Name this template** at the top left (for example `Bridal silver tag`). It is the only thing you must fill in to save.

#### Put things on the label

The **Insert** rail on the left adds elements to the middle of the label:

| Button | What it adds |
|---|---|
| **Text** | Your own wording. Type plain text, or mix in item data with `{field}` tokens — `W: {weight} gm`, `MRP ₹{mrp}`, `{metal_type} {purity_karat}`. **Insert field…** under the text box drops a token in for you. |
| **Data** | Opens the **Data fields** drawer: every field on the label (**Item alias** — the item's short name, 14 characters at most, set on its item name; **Item name (full)**; **SKU**, **Barcode value**, **HSN code**, **Category**, **Batch / Lot**, **Expiry date**, **Mfg date**, **Qty**, **Source document no.**) and **every field on the Item record** — weight, purity, size, MRP, sale price, and any field your business added, including ones added later. Search it, then click a field to add it, or drag it onto the exact spot on the label. **Drop a field onto a barcode or QR code** and that code switches to it — drop **Batch / Lot** onto a barcode to make the batch scannable. Each row shows the value it would print (Batch / Lot shows a sample in today's batch format). **Variant** prints just the variant's values — an item saved as `metal_type:Silver;size:7` prints **Silver / 7**. |
| **Barcode** | A barcode of the item's barcode value (or any other field, or custom text). **Code 128** takes any text; **EAN-13** is the 13-digit retail code (a value that isn't a valid EAN-13 prints as Code 128 instead, and the studio says so). |
| **QR** | A QR code of any field or custom text — for example a product link `https://yourshop.in/p/{sku}`. |
| **Line**, **Box** | Dividers and borders. A box can be **solid filled**. |
| **Logo** | Upload a small PNG/JPG/SVG (up to 400 KB). Thermal printers print black or nothing, so tune **Ink threshold** with **Printer view** on. |

**If an item has no value for a field, that element is left off the label entirely** — an item with no weight never shows a stray "W: gm". A text that combines several fields still prints whichever of them are present.

#### Arrange it

- **Drag** an element to move it; drag one of its eight **handles** to resize it (the size shows under it as you go). Pink **guide lines** appear when an edge or centre lines up with the label or another element, and it snaps there; with no guide in reach it snaps to a 0.5 mm grid. Hold **Alt** while dragging to place freely, **Shift** to move in one direction only.
- **Select several**: Shift/Ctrl-click, or drag a box around them on an empty part of the canvas. The **align and distribute** buttons then appear in the bottom dock.
- **The quick bar** above the label acts on the selection: **Rotate 90°**, **Duplicate**, **Bring to front**, **Send to back**, **Lock**, **Delete**.
- **Keyboard**: arrow keys nudge 0.1 mm (Shift + arrow: 1 mm), **R** rotates, **Ctrl+D** duplicates, **Ctrl+C / Ctrl+V** copy and paste, **Del** deletes, **Ctrl+Z / Ctrl+Shift+Z** undo and redo, **Ctrl+S** saves, **Esc** deselects (pressed again, it leaves the studio). The **Keys** button at the bottom of the rail lists them all.
- **The bottom dock**: zoom out / in, the zoom percentage (click it to fit the label to the screen again), **1:1** (actual size), **Grid**, **Snap**, and **Printer view**, which shows the label exactly as the printer will burn it, dot by dot, at the template's DPI.
- **Rulers** along the top and left are in millimetres and highlight the selection; the line at the bottom shows the cursor position in mm.

#### Fine-tune in the inspector (right-hand panel)

**Element tab** — for the selected element:

- **Content**: what it **Prints** (a field, or **Custom text / combine fields…**), and for a field, text to put **Before** and **After** it (`W:` … ` gm`), **Decimals** (or *As stored*), **1,23,456 grouping** for amounts.
- **Type**: **Font** (or the template default), **Size** in mm (with the point size beside it), **B** bold, **I** italic, **AA** uppercase, left / centre / right, top / middle / bottom, and what to do with **Long values** — **Shrink to fit** (best for item names), **Wrap** onto more lines, or **Cut off**.
- **Barcode / QR**: symbology, **Print the value under the bars**, **Keep a quiet zone** (leave it on unless space is very tight), QR **error correction** L / M / Q / H, and a **scan check** — green "Scannable" with the bar width in printer dots, or an amber warning when bars would be under 2 dots wide (make it wider or shorten the value).
- **Placement**: exact **X / Y / W / H** in mm, **Rotate** 0° / 90° / 180° / 270° (rotating sideways keeps the element on the label), **White on black**, and one-click **Centre ↔**, **Centre ↕**, **Full width**.
- The element's name at the top can be renamed; the lock and eye icons lock its position or hide it (hidden elements don't print).

**Label tab** — the template itself:

- **Size**: a **Standard size** list (changing it scales the existing layout to the new size), or exact **Width / Height**, and **Printer DPI** (203 / 300 / 600 — match your printer; it drives the scan checks and Printer view).
- **Roll**: **Direction** — **Horizontal** prints the design as you see it; **Vertical** prints it turned a quarter, so a tag that stands along the roll reads top to bottom (you keep designing it as it reads). Labels **Across** the roll (1 for a single-label roll, 2 or 3 for multi-up) and **Down** (how many labels one print stacks, e.g. 3 lying tags in one go), the **Gap ↔** between labels across and **Gap ↕** between labels down and between prints, and **Roll width** when the liner is wider than the labels. **One print** under them shows the print exactly as it comes off the roll. On a horizontal multi-up design the other labels show faded beside the one you're designing.
- **Printer tuning**: **Shift → / Shift ↓** to correct a print that lands off-centre, **Darkness** (0–15) and **Speed**, and **Rotate the print 180°** if labels come out upside down. Blank darkness and speed keep the printer's own settings.
- **Text**: the template's default **Font** (Arial, Arial Narrow, Segoe UI, Verdana, Tahoma, Trebuchet MS, Georgia, Times New Roman, Courier New, Consolas).
- **Office printer / PDF**: how labels lay out when they go through the browser print dialog — **Roll - a page per row** (a thermal printer driven by its Windows driver) or **A4 sticker sheet** with margins and gaps; the studio tells you how many labels fit per page.
- **Which items use it**: **Item categories** (comma-separated, matched against the Item's **Category** — spelling must match, capitals and spaces don't matter; a typo simply won't match and those items fall to the next rule), **Default for categories with no template of their own**, **Status** (an **Inactive** template is ignored — the safe way to retire a layout), and the **Template code** (made from the name when you first save; fixed after that).

**Layers tab** — every element, front-most first, with forward / backward, lock and hide on each row. Click a row to select it.

#### Check it with a real item, test, save

- **Preview with** (top bar): search an item and the label redraws with that item's real data — the quickest way to catch a name that's too long or a missing weight. The **×** goes back to sample data.
- The status line at the bottom right says **✓ Ready to print**, or warns about anything that runs off the label or won't scan.
- **Test print** prints one label (one row on a multi-up roll) of the current design on the printer you choose — nothing is written to the print history. Use it to set **Shift**, **Darkness** and **Rotate 180°** before the real run.
- **Test prints show first too (Stage 143.4).** Choose the printer and click **Preview**: the label shows as that printer will print it, with its shift. **Print** there prints it; **Close** prints nothing. **Test alignment** on the sticker page, the template studio's **Test print**, **Test Print** under **Print Setup** and **Print** on the **?** key list work the same way: the print shows first, with one **Print**.
- **Save** (or **Ctrl+S**). An orange dot beside the name means there are unsaved changes; leaving the studio with unsaved changes asks first.

What you see in the studio is what prints — the canvas, Printer view, the print dialog and the thermal printer all use the same drawing code.

> **The default template.** Tick **Default for categories with no template of their own** on one template to make it the catch-all. Leave it unticked everywhere and unmatched items use the Standard label instead. Only one template should have it — the **Template rules** tab (§7A.5) sets it on one and clears it from the rest for you.

### 7A.3 Print labels without a document (manual / re-print)

For one-offs — a tag that fell off, a single item being re-tagged — click **+ Print Stickers** and use the panel below **Print from Transaction**:

1. Check the **Printer** and **Copies per SKU**, and enter a **Reprint Reason** if it's a re-print. Both open as you last left them on this computer (the printer as in §7A.1 step 5); change them for any one print and the new values are kept for the next.
2. Scan or type into **Scan or Enter SKU** and press Enter. With **Print on scan** ticked (the default, remembered on this computer), each scan prints that item's sticker at once - **Copies per SKU** copies - and the box is ready for the next scan. With a goods receipt or transfer loaded above and the item on it, the sticker carries that line's lot.
3. To collect several items first, untick **Print on scan**: each scan (or **Add**) lists the SKU underneath with an **x** to remove it; then click **Print Stickers**. When it's done you're back on the Print history with the run on top.

The template rules (§7A.5) apply here exactly as they do to a document print. Picking a different template for one print is on the document print only (§7A.1).

**Bin labels.** Open the **Bin** list, tick the bins (or tick none for every bin on the page) and click **Print labels**. Pick the **Template** and **Copies**, press **Print**, check the preview and press **Print** again. **A4 sheet** prints them on plain paper instead. The **Bin Generator** has the same **Print labels** button.

### 7A.4 What was printed, and by whom

The table at the bottom of the **Print** tab is the print history: SKU, barcode, printer, who printed it, how many copies, the **Source** document, the **Lot**, the reprint reason, and when. Every print run — document-driven or manual — lands here, so a queried re-print can always be traced back to a person and a time. A run started from a GRN or Transfer Order also records which document it came from and which template it used. If that line carried a batch/lot, the run records the lot too. So when one SKU arrived on two lots in the same GRN and each lot's stickers were printed separately, the history shows two rows, one per lot (for example `LOT-JUL` ×3 and `LOT-AUG` ×5). A manual SKU scan has no lot, so that column stays blank for it, as it does for runs printed before this column existed.

**To print a row again**, click **Reprint** at the end of it, then **Print** in the preview that opens. That is all: the preview has that row's stickers on its printer, with its copies.

- A row printed from a document loads that document with only that SKU and lot ticked. If the line is no longer on the document, no preview opens and a message says so.
- A row printed by scanning puts the SKU in the scan list (with **Print on scan** off for this print).
- **Reprint reason** is optional. To record one, click **Close** in the preview, type it (or pick a code), and click **Print Selected** (or **Print Stickers**).
- To change the copies or the template first, **Close** the preview, change them, and print. The template follows today's template rules, so a reprint looks like a fresh print.

### 7A.5 Which template each item uses (Template rules)

**Setup → Printing → Templates → Stickers and barcodes → Template rules.** An item's label uses the **first rule that applies**, checked top to bottom:

| # | Rule | Where it is set |
| --- | --- | --- |
| 1 | **Item** — the item's own template | **Sticker template** on the Item form (or a `sticker_template` column in an Item import). The rules screen shows how many items use each template this way. |
| 2 | **Item type** — every item of that type | The **Item type** table on this screen (also **Sticker template** on the Item Type record). |
| 3 | **Product type** — every item of that product type (Ring, Necklace...) | The **Product type** table on this screen (also **Sticker template** on the Product Type record). |
| 4 | **Category** — items whose **Category** matches (capitals don't matter) | The **Category** table on this screen. To map a category that is not listed, type it in **Another category** and pick its template. |
| 5 | **Default** — everything no rule above picked | The **Default** dropdown on this screen. |
| 6 | **Standard label** — the built-in label | Nothing to set. **Duplicate the standard label** opens a copy of it in the studio. |

1. Open **Setup → Printing → Templates** and scroll to **Template rules** (under *Stickers and barcodes*).
2. Pick a template in any dropdown. It saves the moment you pick it, and a message confirms it (for example *"Category Rings saved."*).
3. **Next rule** (or **Standard label (built-in)** for the default) takes that rule away again.

Good to know:

- Each category belongs to one template. Mapping a category here takes it off any other template, so two templates never fight over it. It is the same list as **Item categories** in the studio's **Label** tab.
- The **Default** dropdown keeps the default tick on exactly one template.
- An **Inactive** template is skipped wherever it is named, so a rule pointing at it falls through to the next rule.
- The lists offer your **Active** templates and the built-in designs (Square 2-up, Ring tag, the four Seznik designs), marked *(built-in)*. Picking a built-in design saves it once as a template of your own (a message says so) and uses that; from then on the list shows your copy instead. With no templates and no built-in designs left to pick, every label prints the Standard label.
- To print a few lines of one document on a different layout without changing any rule, pick the template on that line of the document print (§7A.1).

### 7A.6 Set a label printer up once (Printer preset)

Set it once per printer. Every computer that prints to that printer uses it, and nothing asks at print time.

1. **Sticker Printing → + Print Stickers**, pick the **Printer**, and click **Printer preset** beside it (it reads **Roll: *name*** once a roll is set).
2. **Roll loaded** — the template for the roll in the printer, for example *Seznik vertical × 3*. From then on every sticker on this printer prints on that roll: an item whose template rule picks a design of another size prints with the roll's template instead, so a print can no longer cross the tags; a design of the same roll size (a Square and a Ring design, say) still prints as itself. **None** leaves it to the template rules (§7A.5).
3. Load the roll with the tags in the middle of the printer, snug against its guides, and click **Calibrate roll**: the printer feeds a few labels and measures the label and the gap. Do it again after changing to a different roll. (A printer set up as **PDF** goes through its Windows driver and cannot be calibrated from here; use the printer's own label calibration from its manual.)
4. **Line up** - put the print in the middle of the tags, no measuring:
   1. Click **Line up**, then **Print** in the preview. Five rows of tags come out, lettered **A** to **E**. Each tag has a box just inside its edge. Each row is moved 0.5 mm further right than the one before; **C** is the shift you have now. The rows are drawn on the **Roll loaded**; with **None**, on the template ticked **Default** (or the only label-roll template). The preview names it under each row: check it is your roll before you print.
   2. Keep the Printer preset open. Look at the rows and find the one whose boxes sit evenly inside the tags.
   3. Tap that letter under **Best row**. Its shift is saved at once, and a message says which.
   4. If **A** or **E** was still not even, tap it and click **Line up** again: the next strip starts from there.
5. **Test alignment** prints one row with a box, a cross through the middle and the shift in use. Use it to check up and down too: if the box sits nearer the top or bottom of the tags, change **Shift down** (mm; a negative number moves up) and test again. The test uses the numbers on screen, saved or not.
6. **Darkness** (0-15) and **Speed**: leave blank to keep each template's own; set them when every tag is too faint or smudged.
7. **Save** (Line up has already saved its shift). The preset is kept on the Printer record (**Setup → Printing → Printer** shows the same fields), so it holds for every computer and every template. Only roles that may change printers can save.

**When a tag comes out wrong:**

| You see | Do this |
| --- | --- |
| Text cut off at one side of every tag | **Line up** (step 4). |
| Text cut off at the top or bottom of every tag | **Test alignment**, then change **Shift down** (step 5). |
| The far side of the last tag never prints, whatever the shift | The roll sits off-centre under the print head. Slide it so the tags are in the middle of the printer, then **Line up** again. |
| A blank tag or a blank row before the stickers | Update PrintBridge on that computer (**Download PrintBridge**, below), then print again. |
| Faint or smudged print | Set **Darkness** (higher is darker) or a lower **Speed**. |
| Each print starts part-way down a tag | **Calibrate roll** (step 3). |

**PrintBridge on each computer - installed once, for good.** The first print on a Windows computer without PrintBridge shows *Install PrintBridge*. Click **Install**, open the downloaded file (Windows may first say *Windows protected your PC* - **More info → Run anyway**), and the print goes out by itself as soon as PrintBridge starts; the installer asks nothing. **Not now** prints through the browser and is not asked again in that browser - **Download PrintBridge** (Sticker Printing → + Print Stickers → *Printer setup on this computer*) is the same installer. New versions replace themselves (PrintBridge 0.3.0 and later): the panel says *updating to ... by itself*, and only a PrintBridge older than 0.3.0 needs **Download PrintBridge** once more. The computer keeps its printer choices after the browser's data is cleared. It starts with Windows; if someone quits it from the tray, it is back at the next restart, and the ERP starts it again at the next click before that (the browser may ask once *Open PrintBridge?* - tick **Always allow**). If it cannot be started, the print says so in about 10 seconds: **Retry**, or print through the browser. Opening PrintBridge again while it runs only says *PrintBridge is already running* - click **OK**; nothing is wrong. Opened at any other address (an office IP, your own domain), PrintBridge asks once on that computer *Allow printing?* - say **Yes** only for your ERP. The panel says whether PrintBridge is running and which version.

## 7B. Printing POs, GRNs, Transfers and Notes — and Designing Their Layout

### 7B.1 Printing a document

**Print** on any of these opens the print window: a Purchase Order, Goods Receipt, **Purchase Return** (Procurement → Purchase Return list), Stock Transfer (Transfer Out or In), a **Sales Invoice** (**Financial Accounting → Sales Invoices**), a debit / credit note — vendor or **customer** (**Financial Accounting → Debit Notes**, **Vendor Credit Notes** or **Customer Credit Notes**, or the GRN's **Price Notes** column), or a **Sales Return** (the Returns workbench).
If your administrator has set a **print profile** for a document (Stage 127.4), Print gives the whole set in one go - for example three copies of an invoice labelled *Original for Recipient*, *Duplicate for Transporter* and *Triplicate for Supplier* - with the profile's template and footer line. The preview shows every copy.

**Every other transaction prints too** (Stage 127.3). Open any saved transaction from its list - a journal voucher, RFQ, vendor quote, landed cost voucher, warehouse task, production order, ASN and so on - and click **Print** beside its title. A transaction with no print of its own prints in a standard layout: its title and number, date and status, your store and the vendor or customer, every filled field in the order the form shows them (a linked record by its name), its lines, and the total of each quantity and amount column. A transaction added to the ERP later prints the same way, with nothing to set up. Silent printing treats these as the kind **Other Documents** (§7B.3). Some print in their standard form: a **journal voucher** with each account by its name and debit / credit in rupees, a **payslip** as earnings and deductions, a **job work order** as the goods sent and what comes back (its statutory challan is its delivery challan, §5.8), a **vendor invoice** as the **Purchase Bill** with its payments, and a **loading task** as the **Bill of Lading** with its cartons (WMS → Loading Dock → **Print Bill of Lading** opens this same print window).

**More standard prints** (Stage 130.17): a **payment proposal** prints as the **Payment Advice** (each bill it pays), a **production order** as the **Material Issue Slip** (each BOM component for the order quantity, with a column to sign), a **material issue** (§5.7) as its slip (asked / issued / returned), a **physical inventory** as the **Count Sheet** (bins and items with an empty *Counted* box - no system quantity, so the count is blind), a **purchase requisition** and an **RFQ** in their own layouts (the RFQ with a *Your rate* column for the vendor). On **Financial Accounting → Sales Invoice** and **Vendor Invoices**, open an invoice's **Payments** and click **Voucher** beside any receipt or payment for its **Receipt Voucher** / **Payment Voucher**; **Statement** at the top asks for a customer (or vendor) code and prints their **Statement of Account** - every invoice and receipt (bill and payment) in date order with the running and closing balance.

- **Print as** — for a PO: **Purchase Order** or **Vendor PO** (the vendor's copy, with item pictures); for a GRN: **Goods Receipt Note** or **Vendor Copy**; for a transfer: **Transfer Out** or **Transfer In**.
- **Paper** — A4, A5, Letter, and for a till sale or sales invoice the **80 mm roll (3 inch)** and **58 mm roll (2 inch)**. It starts on the paper your administrator's print profile sets for this document, this kind of customer (B2B with a GSTIN, or B2C) and you; otherwise on the template's own paper. Pick another paper and the best template drawn for it is chosen for you (Stage 213.1).
- **Template** — the best-matching designed template is chosen for you (marked "best match"); pick another, or **Standard layout (built-in)**. Picking a template sets the paper it is drawn for. A till sale or sales invoice also offers **Roll bill (built-in)**, which prints on the roll paper; printing a roll layout goes to the till's receipt printer (§4.3).
- **Generating store** — the store the print is issued from. It starts as the document's own store; pick another and its name, address, state and GSTIN go in the store block. Once a document has been printed, every later print (and its CSV / Excel) is that first copy, store included, so the box is locked and reads "(as first printed)".
- The **preview** shows every page exactly as it will print.
- **Printer** (with PrintBridge running) — where this print goes on this computer: its printer for this kind of document, or the **Receipt** printer when the paper is a roll. Pick another of this computer's printers to send just this print there; **Change** sets the printer this computer uses for the kind from now on (Stage 213.1).
- **Print** — on a computer with **QZ Tray**, it goes straight to that computer's printer for this kind of document, with no dialog (§7B.3); the window shows which printer, with **Change**. Without QZ Tray your browser's print dialog opens. Once a computer has a printer for a kind, a print of that kind never opens the browser's dialog by itself: if PrintBridge is not running or the printer does not answer, a box says *<kind> did not print* and why, with **Retry**, **Print with the browser instead** and **Cancel**. When the cause is the setup instead (no printer chosen for this computer, or a Printer record with no **OS Printer Name**), the box says exactly what to fix and where; fix it, then **Retry**.
- **PDF** downloads what the preview shows, page for page, named after the document (for example *Purchase-Order-PO-HO-26-27-00045.pdf*). It is a picture of each page: it looks exactly like the print, but its text cannot be selected or copied.
- **CSV** and **Excel** download the lines. At the top: the document, number, date, status, store, the vendor or customer, and linked numbers (the PO on a GRN, the order on a sales invoice). Then one row per line: **Design ID**, **Combination ID / SKU**, **Barcode**, item name, variant, one column per attribute — named as your Item Attribute list names it, so a renamed attribute shows its new name (§8) — **Brand**, HSN, UOM, batch, the quantities the document has (ordered, received, accepted, shipped, short…), **MRP**, then prices: a **PO** and a **GRN** show cost (cost price, taxable value, GST, amount); a **sales invoice** its selling price; transfers, returns and notes **MRP only**. A column no line fills is left out, and a **Total** row closes the list.
- **Excel with pictures** (a PO) — the Vendor PO as a spreadsheet with each item's picture.
- **Send** — emails the PDF to the other party: the vendor, the receiving store or the customer, their email filled in from their record — a store's is its **Store Email** (Setup → Core → Location), which is also the Reply-To on everything that store sends (§6, step 7).
- **Design templates** opens the designer (§7B.2) on this transaction, previewing this document.

Every print carries **your brand name** (your administrator sets it; otherwise your company's legal name) — even on a template that doesn't place it. Long item lists continue onto further pages with the table heading repeated; totals, the tax split, amount in words and the signature go on the last page.

**What the Standard layout prints.** Every document has the same frame: brand, title, number, date, **financial year** and **status** at the top; the store and the other party in two boxes; a few reference lines under them (only the ones that have a value — a blank one is left off rather than printed empty); the items table; totals; three **signature lines**; and a small **audit line** at the foot — who created the document and when, who approved it and when (from the approval log), and who printed it and when.

| Print | Items table | Totals | Signatures |
| --- | --- | --- | --- |
| **Purchase Order** / **Vendor PO** | Ln, Item / Barcode, Description / Variant, HSN, UOM, Qty, Unit Price, Tax %, Line Total (Vendor PO adds the picture) | Subtotal, CGST + SGST or IGST, PO Total | Prepared By · Approved By · Supplier Acceptance |
| **Goods Receipt Note** | Barcode / Item, Design / Variant, PO Qty, Received, Accepted, Rejected, Damaged, QC Status, Variance Reason | Total PO qty, received, accepted, rejected (quantities only — no prices) | Received By · QC / Verification · Posted By / Approver |
| **Vendor Copy** (of a GRN) | Item / Barcode, Description, Ordered, Delivered, Accepted, Rejected, Damaged, Remarks | Quantities only | Vendor / Driver · Received By · Security / Gate |
| **Purchase Return** | Barcode / Item, Description, Received Qty (on the GRN), Return Qty, UOM, Rate, Tax %, Return Value, Reason | Taxable return value, tax adjustment, total | Prepared / Packed By · Approved By · Vendor / Transporter |
| **Transfer Out** | Barcode / Combination, Description, HSN, UOM, Requested Qty, Shipped Qty, Value / Unit, Value | Total pieces, value, GST when the stores have different GSTINs | Picked / Packed By · Dispatched By · Transporter / Handover |
| **Transfer In** | Barcode / Combination, Description, Shipped Qty, Received Qty, Short, Reason | Total shipped, received, short | Received By · Verified By · Posted / Approved By |
| **Debit Note** / **Credit Note** (vendor) | Item, HSN, Description / Reason, Original Taxable, Debit (Credit) Taxable, CGST, SGST, IGST, Total | Taxable debited (credited), tax, total | Prepared By · Approved By · Authorised Signatory |
| **Credit Note** (customer) | Description / Reason, Credit Amount | Total credit note | Prepared By · Approved By · Authorised Signatory |
| **Sales Return** | Barcode, Item / Description, Original Qty, Return Qty, MRP, Original Disc., Tax, Refund Value, Condition | Return subtotal, tax reversal, net refund / credit | Processed By · Verified / Approved By · Customer Acknowledgement |
| **Sales Invoice** | Ln, Item / Barcode, Description, HSN, Qty, Rate (incl. GST), Discount, Taxable, GST %, Amount; the IRN, Ack and signed QR once e-invoiced | Taxable value, CGST + SGST or IGST, invoice total | Prepared By · Customer's Signature · Authorised Signatory |

Good to know:

- **The Vendor Copy is the same GRN, not a new document** — same number. It is what you hand the vendor or driver as proof of what arrived: no prices, values or tax at all (whatever template prints it), with the note that it is not an acceptance of the invoice, payment or final QC.
- **The GRN print shows quantities, not prices.** The PO and invoice rates are still there to add as columns in a template of your own (§7B.2), and the price differences print on the debit / credit notes.
- **Transfer In prints against what was shipped.** A line received short shows the shortfall and the reason typed at receipt, and the header says *Received with shortage*.
- **Sales Return** splits the refund back into taxable value and GST at the item's rate (shop prices include GST).
- **A Draft sales invoice prints as a proforma** — titled *Proforma Invoice (Draft - not a tax invoice)* — so it can never pass for a posted bill. Once posted it prints as the **Tax Invoice**.
- A designed template of your own still wins over the Standard layout: pick **Standard layout (built-in)** in **Template** to see this one.

**Other stores' documents.** If you work in one store, Print, PDF, Send and Reprint work for that store's documents only; a document of another store answers "not found", as opening it does. Importing a file is the same: a row that updates another store's record is refused and the error names the store. Posted documents cannot be changed by an import file either - use the reversal.

### 7B.2 Designing a print template (Setup → Printing → Templates)

**Setup → Printing → Templates is the one page for every template**, module by module: **ERP documents** (this section), **Stickers and barcodes** (§7A) and **OMS** (the OMS bill, the pick list and the courier label — §9A.10). The buttons at the top jump to each.

Under **ERP documents** the page lists each transaction — Purchase Order, Vendor PO, Goods Receipt Note, Vendor Copy, Purchase Return, Transfer Out, Transfer In, Debit Note, Credit Note (to vendor), Credit Note (to customer), Sales Return, Sales Invoice — with its templates and the **Standard layout**. **Customise** starts from the standard layout; **+ New template** does the same for the transaction you pick; **Design** edits, **Duplicate** copies, **Make default** sets the one used when nothing more specific fits, **Delete** removes one.

The designer works like the Sticker Studio:

1. **Fields — drag onto the page.** The left list has everything the document carries: document fields (number, date, PO numbers, vendor invoice no…), your **brand**, the **generating store**, the **vendor / other store** and the **ship-to** (name, legal name, address, city, PIN, state, state code, GSTIN, phone, email), and **totals** (taxable, CGST, SGST, IGST, round off, grand total, amount in words). Search it, then drag a field to where you want it — or double-click to add it.
2. **Items table columns.** Line fields (quantity, rates, HSN, GST, batch, PO…) and **every Item master field** (and **Item image**) are listed under *Items table*. Drag one **onto the table** to add it as a column. In the right panel, rename columns, set their relative width, alignment (L / C / R), move them, or remove them; set the table's font size, picture size and header shade. **+ line under…** prints another field under a column's own, one per line (the roll bill shows the barcode and HSN under the item name this way); **×** on a line removes it, and a blank or zero value prints nothing. Rows that do not fit carry on to the next page with the column headings repeated; later pages start at the top margin and pages before the last run to the bottom margin.
3. **Insert** adds a Text box (type text and insert `{fields}`), a Field, a Store / Vendor / Ship-to block (tick which lines to show), the Items table, the **Tax summary** (one row per GST rate — CGST + SGST within a state, IGST across states, chosen automatically), **Totals** (edit, reorder or add rows), a **Logo / image** (PNG or JPG up to 400 KB), a Line, a Box, the Page number or a **QR code** (pick the value it encodes — for a credit note, **E-invoice: IRN signed QR**; it prints only once the value exists, and shows as a dashed box while you design). The standard **Credit Note** layout already places the e-invoice QR with the IRN, Ack No and Ack Date beside it, and "IRN PENDING" there while the IRN is on its way; a note that is not e-invoiced prints none of it.
4. **Place it.** Drag to move (it snaps to the margins, the centre and other elements; hold **Alt** to place freely), drag the handles to resize, or type exact **X / Y / W / H** in mm. Where it sits decides the page: anything **above the items table** (the header) prints on the **first page only**, anything **below it** (tax summary, totals, amount in words, terms, signatures) on the **last page only** — **Prints on** shows this and cannot be changed for them. For the **page number** and anything **beside the table**, **Prints on** decides **first page**, **every page** or **last page**. Arrows nudge 0.5 mm (Shift: 5 mm); **Delete** removes; **Ctrl+D** duplicates; **Ctrl+Z / Ctrl+Y** undo / redo.
5. **Preview with** a real document number to see it filled in; **Test print** shows the pages as they will print, and **Print** there prints them. Click an empty part of the page to see the template settings: page size (A4 / A5 / Letter, or **80 mm roll (3 inch)** / **58 mm roll (2 inch)** receipt paper), orientation, margin and font. On a roll the page is as long as the bill: the items table grows with its lines and everything under it moves down. Choosing a roll size for a till sale or sales invoice offers to lay the page out as the 3-inch bill (Undo brings your old layout back).
6. **When to use it.** Leave it for any vendor and product, or limit it: **Only for vendor**, **Only for designs / families**, **Only for categories** (comma-separated). When printing, the most specific template that fits is offered first — vendor, then design, then category, then the default.
7. **Save** (Ctrl+S).

### 7B.3 Which printer prints what, on each computer

Every computer that prints silently needs **QZ Tray** installed once (your administrator does it — [qz-printing-setup.md](qz-printing-setup.md)). After that, each computer remembers **its own printer for each kind of print**: Purchase Order, Goods Receipt, Transfer, Sales Invoice, Returns and Notes, Receipt (the till bill), Sticker, Shipping Label, Pick List and **Other Documents** (every other transaction - see below). The choice is kept on the server under the computer's name, so clearing the browser does not lose it.

1. **The first time** a kind is printed on a computer, the printer is chosen for you when the choice is clear — A4 / A5 documents go to the computer's **default printer** (unless it is a label or receipt printer - TSC, TVS, Zebra, Gprinter, Y468BT and the like - then to its one ordinary printer), bills to its one **receipt printer**, stickers and shipping labels to its one **label printer** — and a note at the top of the screen says which. Only when two printers could fit does it ask, once: *Printer for Goods Receipt — which printer prints Goods Receipt on this computer?*
2. **Every later print** of that kind on that computer goes straight to that printer. It never asks again.
3. **To change it**, open any print of that kind: the print window shows *Purchase Order prints on HP LaserJet · Change*. **Change** asks which printer, and that becomes the new choice.
4. **To see every computer**, open **Setup → Printing → Printer Assignment**: one row per computer and kind of print, with its printer and store. Set a row to **Inactive** and that computer chooses again at its next print.

A computer with no choice yet for a kind uses its store's default — a **Printer** record (**Setup → Printing → Printer**) at that store whose **Default For** is that kind — before it chooses for itself; one store never prints on another store's printer. A Printer record made by a computer's first choice takes the printer's name as it appears on that computer. A sticker printer whose name says Zebra (or a Zebra model such as ZD220 or GK420) is set to **ZPL**; every other label printer (TSC, TVS, Xprinter, Gprinter, 4-inch printers such as the Y468BT) is set to **TSPL**. If the first stickers come out blank, the language is wrong for that printer: change **Printer Language** on the record. **Inventory → Sticker Printing** still lets you pick the printer for a print run.

### 7B.4 Reprint a document

**Setup → Printing → Reprint** lists every document printed - newest first: when, which document and number, the store, how (**Print**, **PDF**, **Send** or **Reprint**), who, and a reprint's reason. You see only the documents of the modules your company has and that your role may open.

1. Narrow the list with **Document** (one kind) and **Number or text** (a number, a name or who printed it), then **Search**.
2. Click **Reprint** on a row. The print window opens titled **Reprint**, on the paper and template your print profile sets today (a till bill on the roll unless the profile says otherwise); switch **Paper**, **Template** or **Printer** if this copy needs to go elsewhere - the content stays as first printed. Click **Print**, type **why it is being printed again** (it cannot be left blank), and it prints - silently on a computer set up for it (§7B.3). The page's printed line reads *Printed: <you> - REPRINT*, and the list gains a **Reprint** row with your reason.
3. A document printed before this list began, or never printed: pick its **Document**, type its number and click **Print this number**.

**A reprint is the original again (Stage 130.16).** The first print of an issued document - approved, posted, sent; not a draft or one waiting for approval - is kept exactly as it printed: party, store, GSTIN, prices and tax. Every later print of it, from anywhere (the document's own Print, this list, the till's Print Receipt), prints that kept copy and is marked *REPRINT*, even if a vendor's address or a price has changed since. *Printed by / at* come from the server. An amended document (its amendment reason changes) prints as its next **revision**, an original again. A cancelled document prints as it is now. Each row in the list also shows the revision, the copies of the run (a three-copy set - Original / Duplicate / Triplicate - is one run, not three reprints), how it went out - *Sent to browser* (the browser's print) or *Submitted* (PrintBridge); nothing claims *Printed*, because neither can confirm paper - and who approved a reprint.

**When reprints are controlled** (an administrator's setting - Admin Guide, *Issued output and reprint control*): printing a document again asks **why**, needs the *Document Reprint* permission, and after the free reprints (or for the document types the company names) asks for approval first: give the reason and a **Reprint request** goes to the approver; once it is approved, **Print** again - one approval, one reprint. The till behaves the same; it never falls back to an unchecked receipt.

Barcode labels and stickers are reprinted from **Sticker Printing → Print history**, with the **Reprint** button on the row (§7A.4).
