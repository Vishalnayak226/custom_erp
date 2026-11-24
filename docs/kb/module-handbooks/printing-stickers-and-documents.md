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
screen_sections: [stickers=print-stickers,reprint=print-a-document-again]
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

> [!WHAT]
> Sticker Printing prints the small barcode labels that go on products, from a
> Goods Receipt, a Transfer Order or items added by SKU.

> [!WHY]
> Each line carries its own copies and **Template**, so the right label prints
> for the right item. Without an active **Printer**, printing stops with
> *Select a printer first*.

> [!USECASE]
> A Goods Receipt of 3 designs reaches the Surat store. You open **Inventory →
> Sticker Printing**, choose **+ Print Stickers**, load the lines from that
> receipt, check the copies and print.

> [!HOW]

1. Set up one **Printer** first (**Setup → Printing → Printer**, Active). Without one, printing stops with *Select a printer first*.
2. Open **Inventory → Sticker Printing** and **+ Print Stickers**. Load the lines from a Goods Receipt or a Transfer Order, or add items by SKU.
3. Check the copies and the **Template** of each line. The template comes from the rules below; you can pick another one for this print only.
4. Choose the printer and print.

## Choose which sticker each item uses

**Template rules** (Setup → Printing → Templates, under Stickers and barcodes) decides it, checked top to bottom: the **Sticker template** on the item itself, then the item type's, then the category's, then the **Default**. If none applies, the standard label prints. **Duplicate the standard label** gives you a copy to change.

## Set up how a document prints

> [!WHAT]
> A document print template sets how a transaction such as a Purchase Order or
> Sales Invoice looks on paper.

> [!WHY]
> You can give one vendor, design or category its own layout. The most
> specific template that fits is used, and the **Standard layout** applies
> when none does.

> [!USECASE]
> One vendor needs its name and a custom column on every Purchase Order. You
> press **Customise**, design the page, preview it with a real document number
> and fill **Only for vendor**.

> [!HOW]

1. Open **Setup → Printing → Templates** (ERP documents, or OMS for the OMS bill and the pick list).
2. Each transaction shows its templates and its **Standard layout**. Press **Customise** to start from the standard layout, or pick a transaction under **New template for** and press **+ New template**.
3. In the designer, drag fields onto the page and columns onto the items table, then **Save**. Use **Preview with** and a real document number to check it.
4. To use a template only for one vendor, design or category, fill **Only for vendor**, **Only for designs / families** or **Only for categories**. **Make default** sets the one used when nothing more specific fits.

**Design**, **Duplicate** and **Delete** on a template card do what they say. After deleting, documents print with the next best template, or the standard layout.

## Print, save or send a document

**Print** on a PO, GRN, transfer, sales invoice, return or note opens the print window. It starts on the paper and template a **Print Profile** sets (**Setup → Printing → Print Profile**: by document, B2B / B2C, user, store, legal entity, channel). For this one print you can switch:

- **Paper** - A4, A5, Letter, or the 80 mm / 58 mm roll for a till bill or sales invoice. The best template for that paper is picked for you.
- **Template** - any template for the document, or the built-in standard layout or roll bill. Picking one sets its paper.
- **Printer** - with PrintBridge running, this computer's printer for the kind (the receipt printer on a roll). Pick another of its printers for this print only; **Change** sets the computer's printer for the kind.

Besides **Print** it has:

- **PDF** - the pages exactly as previewed, as a file named after the document.
- **CSV** and **Excel** - the lines: design, combination / SKU, barcode, item, each attribute, brand, quantities and MRP, with cost on a PO or GRN and the selling price on a sales invoice (transfers, returns and notes carry MRP only).
- **Send** - emails the PDF to the vendor, the receiving store or the customer. **Download PDF** there saves it to send yourself.

## Print a document again

> [!WHAT]
> Reprint lists every document that was printed and lets you print one again
> after you say why.

> [!WHY]
> A reprint is always the same issued copy marked *REPRINT*, even if a price
> or address has changed since. If your company controls reprints, a manager
> approves a **Reprint Request** after the allowed number.

> [!USECASE]
> A customer at Indore loses a Sales Invoice copy. You open **Setup → Printing
> → Reprint**, find the row, choose **Reprint** and enter the reason.

> [!HOW]

1. Open **Setup → Printing → Reprint** and find the document.
2. Click **Reprint** on its row, switch the paper, template or printer if this copy goes elsewhere, and say why. The copy prints marked *REPRINT*, with the content as first printed.
3. For a document that was never printed, click **Print this number**.

**Setup → Printing → Reprint** lists every document printed - when, which document and number, the store, how it went out, who, and a reprint's reason - only for the modules your company has and the documents your role may open. **Reprint** on a row prints it again after you say why; **Print this number** prints a document that was never printed. Once a document has been issued and printed, every later print is that same copy marked *REPRINT*, even if a price or an address has changed since. If your company controls reprints, a reprint also needs the *Document Reprint* permission, and after the allowed number of reprints a manager approves a **Reprint Request** first.

## The right printer on every computer

With PrintBridge on a computer, **Print** goes straight to that computer's printer for the kind of document - no dialog. The first time a kind prints there, the printer is chosen for you when it is clear (the default printer for A4 / A5, the receipt printer for bills, the label printer for stickers and labels), or you are asked once. It is never asked again; **Change** in the print window changes it, and **Setup → Printing → Printer Assignment** lists every computer's choices.
