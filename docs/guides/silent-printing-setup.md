---
doc_id: DOC-C42FE7B40B
title: Silent Printing Setup (PrintBridge)
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

# Silent Printing Setup (PrintBridge)

One-click printing of shipping labels, invoices and stickers straight to a
named printer — no browser print dialog, no choosing the printer each time.

The ERP prints through **PrintBridge**, its own print agent. Marketplace seller
panels (Myntra Mdirect and others) print through a different program, QZ Tray,
which PrintBridge does not serve. Only a computer that also prints from such a
panel needs QZ Tray — see [Making the marketplace panels work
too](#making-the-marketplace-panels-work-too).

---

## Install PrintBridge (each computer that prints)

PrintBridge is the ERP's own print agent. It speaks the same protocol as QZ
Tray, needs no Java, and one installer serves every Wholeops address. Each
computer installs it once; later versions replace themselves. On each computer
that prints:

1. Print anything. On a computer without PrintBridge the ERP shows *Install
   PrintBridge* - click **Install**. (Or open **Sticker Printing → + Print
   Stickers → Printer setup on this computer** and click **Download
   PrintBridge**.)
2. Open the downloaded file. It asks nothing; if Windows says *Windows
   protected your PC*, click **More info → Run anyway**.
3. A printer icon appears near the clock and the waiting print goes out. PrintBridge starts with Windows; if
   it was closed from the tray, the ERP starts it again at your next click (the
   browser may ask once whether to open *printbridge-agent* - tick **Always allow** and click **Open**; the
   page takes no clicks until that question is answered).

Then do Part 2 (the Printer records) and set each label printer's preset -
roll, alignment, darkness (User Guide §7A.6). Part 1 below is only for a
computer that also prints from a marketplace panel.

---

## What you need to know first

PrintBridge is a small program that runs on each **computer that prints** (not
the server). The browser talks to it over a local connection, and it talks to
the printer. So it must be installed on every computer that prints.

If PrintBridge is not running, nothing breaks — the ERP starts it again at your
next click (step 3 above).

---

## Part 1 — QZ Tray, only for marketplace panels (once per PC)

Skip this part unless the computer also prints from a marketplace seller panel
(Myntra Mdirect and others). Those panels need QZ Tray; the ERP does not.

### 1. Install Java

QZ Tray needs Java. Install the latest release from
[adoptium.net](https://adoptium.net) (or oracle.com/java). Any current version
is fine.

> Newer QZ Tray installers bundle their own Java. If yours did, skip this step.

### 2. Install QZ Tray

Download from [qz.io/download](https://qz.io/download) and install it.

After installing, **start it**. You should see a small QZ icon in the system
tray (bottom-right, next to the clock). If you don't, launch "QZ Tray" from the
Start menu.

> Set it to start automatically with Windows, or an operator will hit a print
> failure every Monday morning. QZ Tray's tray-icon menu has this option.

### 3. Make printing silent (skip the "Allow" prompt)

Out of the box QZ shows an **"Allow this site to print?"** dialog. To remove it,
each PC needs to trust this server's print certificate.

On the server, export the certificate once:

```
go run ./cmd/printcert -o override.crt
```

Then on each packing PC, copy that file into QZ Tray's install folder as
`override.crt`:

| OS      | Location                                                  |
|---------|-----------------------------------------------------------|
| Windows | `C:\Program Files\QZ Tray\override.crt`                    |
| macOS   | `/Applications/QZ Tray.app/Contents/Resources/override.crt` |
| Linux   | `/opt/qz-tray/override.crt`                                |

**Restart QZ Tray.** Prints no longer prompt.

> `override.crt` is a public certificate. It is safe to put on a shared drive
> or push with your device management tool. The private key never leaves the
> server.

If you skip this step everything still works — the operator just clicks
"Allow" once per session (ticking *Remember this decision* makes it stick).

### 4. Let Chrome reach the local connection

Recent Chrome versions block a website from contacting software on your own
machine until you permit it. If printing does nothing and the browser console
mentions a blocked or failed connection, do this:

1. Click the icon at the **left of the address bar** → **Site settings**
2. Set **"Apps on device"** (or *Local network access*) to **Allow**

If that option isn't shown:

1. Chrome **Settings → Privacy and security → Site settings**
2. **Additional permissions → Local network**
3. Choose **"Sites can ask to access other devices on your local network"**
4. Under **Allowed to access other devices on your local network**, click
   **Add** and enter your ERP address (for example `https://erp.example.com`,
   or `http://localhost:8080` if you run it locally)
5. Reload the ERP tab

---

## Part 2 — In the ERP (once for the whole company)

**Most stores can skip this part.** Each PC picks its own printer for each kind
of print — Purchase Order, Goods Receipt, Transfer, Sales Invoice, Returns and
Notes, Receipt, Sticker, Shipping Label — the first time it prints that kind:
by itself when the PC has one obvious printer (its default printer for A4 / A5
documents, its one receipt printer for bills, its one label printer for
stickers and labels), otherwise by asking once. The choice is kept on the
server under the PC's name, so it is never asked again and survives a browser
reset; **Setup → Printing → Printer Assignment** lists every PC's choices, and
the print window's **Change** changes one (USER_GUIDE §7B.3). Rolling out to
many stores is therefore just installing PrintBridge on each PC.

Do Part 2 when you want a **store-wide default** — a Printer record with a
**Location** and a **Default For**, used by any PC at that store that has not
chosen its own yet — or to set label sizes and DPI for a sticker printer.

### 1. Find out what your printers are really called

Go to **Sticker Printing → + Print Stickers** and open **Printer setup on this
computer (PrintBridge)** at the bottom.

- It should say *"PrintBridge … is running - printing is silent on this
  computer."* If it doesn't, install PrintBridge (top of this guide).
- Click **Detect Printers**. You get the exact names this PC knows, e.g.

  ```
  ZDesigner ZD220-203dpi ZPL     (system default)
  HP LaserJet M404
  Microsoft Print to PDF
  ```

Copy the name you want **exactly**, including spacing and capitals. This is the
step people most often get wrong — "Zebra ZD220" will not match
`ZDesigner ZD220-203dpi ZPL`.

### 2. Create a Printer record per physical printer

Go to **Setup → Printing → Printer** and create one record per printer:

| Field | What to put |
|---|---|
| **Printer Code** | Short code, e.g. `LABEL-01` |
| **Printer Name** | Friendly name operators see, e.g. `Packing Bench Label Printer` |
| **OS Printer Name** | The exact name you copied from Detect Printers |
| **Location** | The store the printer is at — a print uses a printer at its own store before one anywhere else |
| **Default For** | `Purchase Order`, `Goods Receipt`, `Transfer`, `Sales Invoice` (or the older `Invoice`), `Returns and Notes`, `Receipt`, `Sticker`, `Shipping Label` or `General` |
| **Printer Language** | See the table below |
| **Label Width / Height (mm)** | For 4x6 labels: `101.6` x `152.4` |
| **Printer DPI** | Usually `203` for thermal, `300` for higher-end |
| **Status** | `Active` |

**Which Printer Language?**

| Your printer | Choose | Why |
|---|---|---|
| Zebra / TVS / thermal that accepts raw commands | `ZPL` | Sharpest, fastest, prints a real scannable barcode |
| TSC-type thermal | `TSPL` | Same, different command set |
| Thermal receipt printer | `ESC-POS` | Receipt command set |
| Thermal driven by its Windows driver | `PDF` | Simpler, slightly slower |
| Ordinary office laser / inkjet | `PDF` | For A4 invoices |

Not sure? Start with `PDF` — it works through the normal Windows driver on
anything. Switch to `ZPL` later if labels look soft or print slowly.

When a PC picks its own sticker printer (its first sticker print), the record
it makes fills this in by name: `ZPL` for a Zebra, `TSPL` for TSC / TVS /
Xprinter / Gprinter / Y468 printers, and `PDF` for any other printer, such
as the Seznik LD0801, which takes neither.

**"Default For" is the store-wide default.** Once a printer at a store is the
default for Shipping Label, every label printed at that store goes there - on
any PC there that has not chosen its own label printer.

### 3. Test it

Back on **Sticker Printing → Print Setup**, click **Test Print**. It shows the
test page and the printer it goes to; click **Print**. A test label should come
out with no dialog.

---

## Day-to-day use

| To print | Do this | Uses the printer that is Default For |
|---|---|---|
| **Barcode stickers (manual)** | Sticker Printing → Print tab → **+ Print Stickers** → add SKUs → **Print Stickers** | `Sticker` |
| **Barcode stickers (from a GRN/Transfer Order)** | Sticker Printing → Print tab → **Print from Transaction** → pick the document → **Print Selected** (or **Print** on one line) | `Sticker` |
| **A marketplace label or invoice** (Myntra, or any channel PDF) | Sticker Printing → Print Setup → choose the file → **Print** | `Shipping Label` |
| **An ERP shipping label** | Marketplace → Logistics Bookings → **Print Label** on the row | `Shipping Label` |
| **A POS receipt** | Point of Sale → complete the sale → answer **Yes** to "Print receipt?" | `Receipt` |
| **A sales invoice** | Sales Invoice → **Print** on the row | `Invoice` |
| **A purchase order** | Purchase Orders → **Print** on the row | `Purchase Order` |

Marketplace PDFs are printed **exactly as the channel issued them** — the file
is passed to the printer untouched, so carrier barcodes are never re-rendered
or resized in a way that could stop them scanning.

An ERP shipping label sent to a `ZPL`/`TSPL` printer carries a **real scannable
Code 128 barcode** of the AWB, not just the digits. On a `PDF` printer the same
label is laid out as a page and printed through the Windows driver.

**Nothing here stops working if PrintBridge is not running.** Each of these falls
back to what it did before: the shipping label opens on screen, the receipt and
the invoice go to the normal browser print dialog. You lose the one-click part,
not the ability to print.

**A note on receipts.** A receipt prints only once the sale is **complete**. A
sale still waiting on a manager's discount approval will not print one — the
money has not been collected yet. Reprinting later is safe: the receipt is
rebuilt from the recorded sale, so it always shows what was actually rung up.
If you use a narrow **58mm** till roll, set **Label Width (mm)** to `58` on
that Printer record, or lines will be laid out for an 80mm roll and wrap.

**A note on invoices.** A **Draft** invoice will print, so you can hand a
customer a proforma — but it comes out marked `DRAFT` so it cannot be mistaken
for a posted tax invoice. Post the invoice first if you need the real thing.

Every job is recorded, so a disputed reprint can be traced to an operator and a
time.

---

## Category-based sticker templates

By default every SKU prints the same simple label (name, barcode, SKU). If
different product categories should look different — a small earring tag
next to a bigger necklace tag, say — design a template per category instead.

### 1. Design a template

1. Go to **Sticker Printing → Templates → New template**. The **Sticker
   Studio** opens full-screen and asks for the roll size (jewellery tail
   tags, retail, multi-up and shipping sizes are listed).
2. Name it, then add elements from the **Insert** rail: **Text** (with
   `{field}` tokens such as `W: {weight} gm`), **Data** (any label or Item
   field, including weight, purity, size and MRP), **Barcode** (Code 128 /
   EAN-13), **QR**, **Line**, **Box** and **Logo**. Drag, resize, rotate and
   align them; the right-hand inspector sets fonts, sizes and exact mm
   positions.
3. On the inspector's **Label** tab, set the **Printer DPI**, labels
   **Across** the roll, the gaps, and under **Which items use it** the
   **Item categories** (comma-separated, matched against the Item's free-text
   **Category**, not case-sensitive) and, on one template only, **Default
   for categories with no template of their own**.
4. **Test print** one label, adjust **Printer tuning** (shift, darkness,
   rotate 180°) if it lands off-centre, then **Save**.

Leave **Status** on **Active** — an **Inactive** template is skipped on every
print run, which is how you retire a layout without deleting it. The full
walkthrough of every control: [user-guide.md](user-guide.md) §7A.2;
administrator notes (permissions, rendering, DPI, storage limits):
[admin-guide.md](admin-guide.md) §B.3.4.

The studio canvas is a true preview: the canvas, its **Printer view**, the
browser print dialog and the thermal printer are all drawn by the same code.
For silent printing the Printer record's **Printer Language** is
**TSPL** (TSC and most Indian desktop label printers), **ZPL** (Zebra) or
**PDF** (any other label printer, through its own Windows driver). With TSPL
or ZPL each label is sent as a bitmap at the printer's DPI; with PDF the same
drawing goes to the driver as a page. Either way any font and logo prints
exactly as designed.

### 2. Print from a GRN or Transfer Order

1. Go to **Sticker Printing → Print → Print from Transaction**.
2. Pick **Goods Receipt (GRN)** or **Transfer Order** from the **Module**
   dropdown, then search for the document by its number in the field next
   to it.
3. The line list shows every stickerable line — its SKU, name, lot (when
   the document carries one), category,
   which template it will use and the rule that picked it (or "Standard
   label" when no rule applies; the dropdown picks another template for
   that print only), and how many copies (defaulting to the accepted/
   transfer quantity — edit the number if you want a different count).
4. Either untick the lines you don't want and click **Print Selected**, or
   click **Print** on a single line to print just that one.

A GRN prints only against **accepted** quantity — a rejected or fully
damaged line does not get a sticker, since there is nothing sellable to
label. If a document mixes categories, each line prints with its own
category's template, and the whole run comes off the printer grouped by
category so it can be torn into separate stacks.

---

## Making the marketplace panels work too

Parts 1.1, 1.2, 1.3 and 1.4 above are exactly what Myntra's support steps ask
for. After them, the only per-panel step left is entering the printer names in
that panel's own "Configure Printer" screen — use the same names you copied
from **Detect Printers**.

Things worth knowing:

- PrintBridge prints only for the ERP. A marketplace panel needs QZ Tray.
- On one computer both want the same local ports; whichever starts second
  takes the next one (8282 instead of 8181). Running both on one computer has
  not been tried with a real marketplace panel yet.
- The `override.crt` in Part 1.3 only stops QZ Tray prompting for **this
  ERP's** prints. Marketplace panels use their own certificates and will keep
  prompting unless they ship one; that is normal and not something we control.
- If a panel's print button does nothing, its Chrome permission is almost
  always the cause — repeat Part 1.4 for that panel's address (for QZ's own
  loopback the address to allow is `https://localhost:8181`).

---

## Troubleshooting

**"PrintBridge is not running or not reachable on this computer"**
PrintBridge isn't installed or started, or the browser is blocking the local
connection. Check for the printer icon near the clock first, then the browser
permission in Part 1.4.

**"The printer "…" is off or unplugged"**
From PrintBridge 0.3.3, each print first checks with Windows that the printer
is there. Before that, Windows took the job for a printer that was off or
unplugged, held it, and printed it the next time the printer was plugged in.
Switch the printer on or plug in its USB cable, then click **Retry**. Nothing
waits in the Windows queue.

**"… is set to Use Printer Offline in Windows"** or **"… is paused in Windows"**
Open Windows **Settings → Bluetooth & devices → Printers & scanners**, pick
the printer, **Open print queue**, and untick **Use Printer Offline** or
**Pause Printing** in its **Printer** menu. Then click **Retry**.

A printer that is out of paper or jammed still takes the job: Windows clears
that state only when a job goes through, and the label prints once the roll is
loaded.

**A marketplace panel shows an "Allow" prompt every time (QZ Tray)**
`override.crt` is missing, in the wrong folder, or QZ Tray wasn't restarted
after it was added.

**"Printer … has no OS printer name set"**
That Printer record's **OS Printer Name** is blank. Use Detect Printers and
paste the exact name.

**"No active printer is set as the default for …"**
No Active Printer has **Default For** set to that document type. Set it on one
record.

**Nothing prints, no error**
The OS Printer Name doesn't match a real printer. Names change when a printer
is reinstalled or moved to a different port — re-run Detect Printers and
compare character by character.

**Labels come out blank, tiny, or clipped**
The Printer Language is probably wrong. A `PDF` payload sent to a printer
configured as `ZPL` prints garbage or nothing. A label printer that takes
neither `ZPL` nor `TSPL` (the Seznik LD0801 is one) prints nothing in those
languages: set it to `PDF`. Also check Label Width/Height are set for 4x6
labels.

**It works on one PC but not another**
Setup is per-PC. On the failing machine, open **Printer setup on this computer**
— usually PrintBridge isn't installed or isn't running there.

---

## For administrators

- **Where the key lives.** By default the server generates an RSA-2048 keypair
  on first use and stores it outside the repo, under the OS per-user config
  directory (`%APPDATA%\custom_erp\` on Windows), as
  `print_agent_private_key.pem` and `print_agent_certificate.pem`. A key made
  before ask 263 (`qz_private_key.pem`) is still used. To manage it yourself,
  set `PRINT_AGENT_PRIVATE_KEY_PATH` and `PRINT_AGENT_CERTIFICATE_PATH` to your
  own PEM files; the names used before ask 263, `QZ_PRIVATE_KEY_PATH` and
  `QZ_CERTIFICATE_PATH`, are still read.
- **Printers saved before ask 263** keep their OS printer name in an old field
  until an admin runs the **Printer names** heal (Settings → Data Healer).
  Printing works meanwhile; only the Printer form waits for the heal.
- **Rotating the certificate** invalidates every PC's `override.crt`. Re-export
  and redistribute, or prints start prompting again.
- **The signing endpoint** only ever signs a single SHA-256 digest, and only
  for an authenticated user, so it cannot be used to sign arbitrary content.
- **Print history** is in `print_job_log`; barcode stickers additionally keep
  their existing `sticker_print_log` history on the Sticker Printing screen —
  a print made from a GRN/Transfer Order also records which document and
  which template it used, alongside every other row.
- **Who can design templates.** `StickerTemplate` follows the same
  permission shape as the other print/config masters: HR/Admin has full
  control, Store Manager can create/edit (not delete), Cashier is read-only.
