# Silent Printing Setup (QZ Tray)

One-click printing of shipping labels, invoices and stickers straight to a
named printer — no browser print dialog, no choosing the printer each time.

This is the same mechanism marketplace seller panels (Myntra Mdirect and
others) use for their "Print Shipping Label" buttons. Setting it up once for
the ERP also fixes those panels, because they share the QZ Tray install and
the same Chrome permissions — see [Making the marketplace panels work
too](#making-the-marketplace-panels-work-too).

---

## What you need to know first

QZ Tray is a small program that runs on the **packing PC** (not the server).
The browser talks to it over a local connection, and it talks to the printer.
So it must be installed on every PC that prints, and it must be running.

If QZ Tray is not running, nothing breaks — the ERP silently falls back to the
normal browser print dialog. You just lose the one-click part.

---

## Part 1 — On each packing PC (once per PC)

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
go run ./cmd/qzcert -o override.crt
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

### 1. Find out what your printers are really called

Go to **Sticker Printing**. At the top is a **Print Setup (QZ Tray)** panel.

- It should say *"Connected to QZ Tray — printing is silent on this PC."*
  If it doesn't, QZ Tray isn't running — go back to Part 1.
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

Go to **Masters → Printer** and create one record per printer:

| Field | What to put |
|---|---|
| **Printer Code** | Short code, e.g. `LABEL-01` |
| **Printer Name** | Friendly name operators see, e.g. `Packing Bench Label Printer` |
| **OS Printer Name** | The exact name you copied from Detect Printers |
| **Default For** | `Shipping Label`, `Invoice`, `Sticker`, `Receipt` or `General` |
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

**"Default For" is what makes printing one-click.** Once a printer is the
default for Shipping Label, every label goes there without anyone choosing.

### 3. Test it

Back on **Sticker Printing → Print Setup**, click **Test Print**. A test label
should come out with no dialog.

---

## Day-to-day use

| To print | Do this |
|---|---|
| **Barcode stickers** | Sticker Printing → add SKUs → **Print Stickers** |
| **A marketplace label or invoice** (Myntra, or any channel PDF) | Sticker Printing → Print Setup → choose the file → **Print** |
| **An ERP shipping label** | Print from the logistics booking |

Marketplace PDFs are printed **exactly as the channel issued them** — the file
is passed to the printer untouched, so carrier barcodes are never re-rendered
or resized in a way that could stop them scanning.

Every job is recorded, so a disputed reprint can be traced to an operator and a
time.

---

## Making the marketplace panels work too

Parts 1.1, 1.2, 1.3 and 1.4 above are exactly what Myntra's support steps ask
for, so once a PC is set up for the ERP it is also set up for Mdirect and
similar panels. The only per-panel step left is entering the printer names in
that panel's own "Configure Printer" screen — use the same names you copied
from **Detect Printers**.

Two things worth knowing:

- The `override.crt` in Part 1.3 only makes **this ERP's** prints silent.
  Marketplace panels use their own certificates and will keep prompting unless
  they ship one; that is normal and not something we control.
- If a panel's print button does nothing, its Chrome permission is almost
  always the cause — repeat Part 1.4 for that panel's address (for QZ's own
  loopback the address to allow is `https://localhost:8181`).

---

## Troubleshooting

**"QZ Tray is not running or not reachable"**
QZ Tray isn't started, or Chrome is blocking the local connection. Check the
tray icon first, then Part 1.4.

**Printing still shows an "Allow" prompt every time**
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
configured as `ZPL` prints garbage or nothing. Also check Label Width/Height
are set for 4x6 labels.

**It works on one PC but not another**
Setup is per-PC. Walk through Part 1 again on the failing machine — usually
QZ Tray isn't installed, isn't running, or is missing `override.crt`.

---

## For administrators

- **Where the key lives.** By default the server generates an RSA-2048 keypair
  on first use and stores it outside the repo, under the OS per-user config
  directory (`%APPDATA%\custom_erp\` on Windows). To manage it yourself, set
  `QZ_PRIVATE_KEY_PATH` and `QZ_CERTIFICATE_PATH` to your own PEM files.
- **Rotating the certificate** invalidates every PC's `override.crt`. Re-export
  and redistribute, or prints start prompting again.
- **The signing endpoint** only ever signs a single SHA-256 digest, and only
  for an authenticated user, so it cannot be used to sign arbitrary content.
- **Print history** is in `print_job_log`; barcode stickers additionally keep
  their existing `sticker_print_log` history on the Sticker Printing screen.
