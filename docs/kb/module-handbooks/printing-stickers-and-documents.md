---
doc_id: KB-PRINT-001
title: Printing - stickers and document templates
type: procedure
status: draft
owner: documentation-maintainer
approvers: [qa-owner, documentation-maintainer]
audience: store manager, warehouse operator, category manager, admin
applies_to: source release 0.1.0; configured tenant permissions
authority: canonical
confidentiality: internal
last_verified: 2026-10-19
review_by: 2026-11-18
supersedes: none
superseded_by: none
section: Module Handbooks
order: 64
summary: The difference between a sticker template and a document print template, where each is made, which one prints, and how a document is printed, saved as PDF or Excel, sent, and routed to each computer's own printer.
screens: [stickers, reprint, doctype:printtemplate, doctype:stickertemplate, doctype:printer, doctype:printerassignment]
topic_type: how-to
module: inventory
task: Print item stickers and set up how documents print
prerequisites: One Active Printer record for sticker printing
failure_behavior: If a print comes out wrong, preview the template with a real document number before changing the rules.
---

# Printing - stickers and document templates

The system prints two different things, and each has its own kind of template.

| | Sticker template | Document print template |
|---|---|---|
| What it prints | The small barcode label that goes on the product | A transaction on paper: Purchase Order, Vendor PO, Goods Receipt, Vendor Copy, Purchase Return, Transfer Out, Transfer In, Debit Note, Credit Note, Sales Return, Sales Invoice |
| Where you make it | **Setup → Printing → Templates → Stickers and barcodes** | **Setup → Printing → Templates → ERP documents** (and **OMS** for the OMS bill and the pick list) |
| Which one prints | The first rule that fits: the item's own, its item type's, its category's, then the default | The most specific that fits: for the vendor, then for the design, then for the category, then the transaction's default |
| If you make none | The built-in standard label | The built-in **Standard layout** |

**Setup → Printing → Templates is the one page for every template**, module by module: ERP documents, stickers and barcodes (with the template rules), and OMS (the OMS bill, the pick list, and the built-in courier label). Sticker Printing's **Templates** button goes there too.

## Print stickers

1. Set up one **Printer** first (**Setup → Printing → Printer**, Active). Without one, printing stops with *Select a printer first*.
2. Open **Inventory → Sticker Printing** and **+ Print Stickers**. Load the lines from a Goods Receipt or a Transfer Order, or add items by SKU.
3. Check the copies and the **Template** of each line. The template comes from the rules below; you can pick another one for this print only.
4. Choose the printer and print.

## Choose which sticker each item uses

**Template rules** (Setup → Printing → Templates, under Stickers and barcodes) decides it, checked top to bottom: the **Sticker template** on the item itself, then the item type's, then the category's, then the **Default**. If none applies, the standard label prints. **Duplicate the standard label** gives you a copy to change.

## Set up how a document prints

1. Open **Setup → Printing → Templates** (ERP documents, or OMS for the OMS bill and the pick list).
2. Each transaction shows its templates and its **Standard layout**. Press **Customise** to start from the standard layout, or pick a transaction under **New template for** and press **+ New template**.
3. In the designer, drag fields onto the page and columns onto the items table, then **Save**. Use **Preview with** and a real document number to check it.
4. To use a template only for one vendor, design or category, fill **Only for vendor**, **Only for designs / families** or **Only for categories**. **Make default** sets the one used when nothing more specific fits.

**Design**, **Duplicate** and **Delete** on a template card do what they say. After deleting, documents print with the next best template, or the standard layout.

## Print, save or send a document

**Print** on a PO, GRN, transfer, sales invoice, return or note opens the print window. Besides **Print** it has:

- **PDF** - the pages exactly as previewed, as a file named after the document.
- **CSV** and **Excel** - the lines: design, combination / SKU, barcode, item, each attribute, brand, quantities and MRP, with cost on a PO or GRN and the selling price on a sales invoice (transfers, returns and notes carry MRP only).
- **Send** - emails the PDF to the vendor, the receiving store or the customer. **Download PDF** there saves it to send yourself.

## Print a document again

**Setup → Printing → Reprint** lists every document printed - when, which document and number, the store, how it went out, who, and a reprint's reason - only for the modules your company has and the documents your role may open. **Reprint** on a row prints it again after you say why; **Print this number** prints a document that was never printed. Once a document has been issued and printed, every later print is that same copy marked *REPRINT*, even if a price or an address has changed since. If your company controls reprints, a reprint also needs the *Document Reprint* permission, and after the allowed number of reprints a manager approves a **Reprint Request** first.

## The right printer on every computer

With QZ Tray on a computer, **Print** goes straight to that computer's printer for the kind of document - no dialog. The first time a kind prints there, the printer is chosen for you when it is clear (the default printer for A4 / A5, the receipt printer for bills, the label printer for stickers and labels), or you are asked once. It is never asked again; **Change** in the print window changes it, and **Setup → Printing → Printer Assignment** lists every computer's choices.
