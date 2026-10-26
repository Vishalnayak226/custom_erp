# ERP shortcuts: plan (Stage 139, 2026-10-26)

User, 2026-10-26: *"We have to build erp shorcuts. Think and propose easy and best practice to follow. & Need report number as wel & Tell what else can be done for trasaction and access transaction to make user's life easy"*.

**What exists today:** the top-bar search already suggests screens and record types (`buildGlobalSearchIndex`, `public/app.js`), every screen has a deep link and Back/Forward works (Stage 64.6), a few screens have their own keys (PO entry: Ctrl+D add design, Alt+R reset). **Missing:** a key to reach the search, reports and documents in that search, short codes, report numbers, one standard key set, recents, favourites.

**The idea in one line:** one box (Ctrl+K) that opens anything - a screen by name, a transaction by short code, a report by number, a document by its number - plus one small key set that works the same on every screen.

## Decisions (to confirm)

| # | Decision | Recommended | Not chosen, and why |
|---|---|---|---|
| D1 | Code style | Short letters people can guess: `SO`, `PO`, `GRN`, `SI` | SAP-style `VA01` - has to be memorised, means nothing to a store user |
| D2 | Code grammar | `SO` = list · `SO+` = new · `SO 42` or the full number = open that document | Separate codes per action (create / change / display) - 3x the codes |
| D3 | Which screens get a code | Transactions and the masters used daily (~40 codes); everything else is found by name | A code for every screen (~200) - nobody learns them |
| D4 | Report number | `R` + 4 digits, first digit = area (below). Same number for every client, never reused | Per-client numbers - support can't say "run R3004" |
| D5 | Keys | Browser-safe set (Ctrl / Alt), no F-keys; Mac uses Cmd | Tally F-keys - the browser owns F5, F6, F11, F12 |
| D6 | Enter moves to the next field | On, in transaction entry only (Tally habit) | Everywhere - breaks notes boxes and pickers |
| D7 | Favourites / recents | Favourites saved per user on the server (follow them to any PC); recents in the browser | Both in the browser - lost on a new PC |
| D8 | Extras (139.6) | Build a-d; e-h only if you want them | - |

**Report number areas (D4):**

| Range | Area (catalog modules) |
|---|---|
| R1xxx | Sales & POS |
| R2xxx | Procurement & GRN |
| R3xxx | Inventory, Stock Transfer, External Movement, Inventory Loss & Audit |
| R4xxx | WMS |
| R5xxx | Accounting, GST & Compliance, Expense Management |
| R6xxx | CRM, E-commerce & Integration, OMS |
| R7xxx | PIM, Manufacturing |
| R8xxx | HRMS |
| R9xxx | MIS & Management, Master Data & Audit |

Numbered in catalog order inside each area (Sales Register = R1001). The older fixed reports get numbers in the same ranges. The 38 catalog reports not built yet get their number now, so nothing moves when they land.

**Sample codes (D1-D3)** - full list goes in `docs/specs/shortcuts/codes.tsv`:

| Area | Codes |
|---|---|
| Sales | `SO` Sales Order · `SI` Sales Invoice · `SR` Sales Return · `CN` Credit Note · `POS` Store billing |
| Buying | `RFQ` · `PO` Purchase Order · `GRN` · `PR` Purchase Return · `DN` Debit Note |
| Stock | `TO` Transfer Order · `SA` Stock Adjustment |
| Money | `PAY` Payment · `RCT` Receipt · `EXP` Expense Claim |
| Making / HR | `PRO` Production Order · `PSL` Payslip |
| Masters | `CUS` Customer · `VEN` Vendor · `ITM` Item |
| Go to | `HOME` · `INB` Approvals inbox · `RPT` Reports |

## Order of work

| Order | Item | Size | Depends on |
|---|---|---|---|
| 1 | 139.1 Command bar | M | - |
| 2 | 139.2 Report numbers | M | - |
| 3 | 139.3 Short codes | S | 139.1 |
| 4 | 139.4 Standard keys | M | 139.1 |
| 5 | 139.5 Recents and favourites | S | 139.1 |
| 6 | 139.6 Easier transactions (chosen extras) | M | D8 |
| 7 | 139.7 Guides, shortcut card, live check | S | all |

## Phase A - Find anything in one box (139.1-139.3)

**Why:** today a user clicks module → flyout → screen → search → row. The box makes it type-and-Enter.

**Your actions:** confirm D1-D4.

- **139.1 Command bar.** Ctrl+K (or `/`) focuses the existing top-bar search - the same box, not a second one. It now also finds reports (name and number), actions ("New Sales Order") and documents by number. Empty box shows recents and favourites. Each result shows its code or number on the right, so codes are learned by using the box.
- **139.2 Report numbers.** One locked list `docs/specs/reports/report_numbers.tsv`; a test fails if a number is missing, repeated or moved. The number shows in the Reports menu, on the report's title line, in the command bar, and in export file names and print headers (`R1001-Sales-Register-2026-10-26.xlsx`).
- **139.3 Short codes.** Fixed list `docs/specs/shortcuts/codes.tsv`; a test fails if a transaction record type has no code or two share one. `SO 42` opens the document numbered 42 in the user's store and year; if several match, the box lists them. A pasted full number or a scanned document barcode opens it directly.

**Done when:** Ctrl+K → `R1001` → Enter opens Sales Register; `PO+` opens a new PO; `GRN 3` opens GRN 3; a user only sees what they are allowed to open.

## Phase B - Hands stay on the keyboard (139.4)

**Your actions:** confirm D5-D6.

- **139.4 One key set, every screen.** Shown by pressing `?`, and in each button's tooltip ("Save · Ctrl+S").

| Where | Key | Does |
|---|---|---|
| Anywhere | Ctrl+K or `/` | Command bar |
| Anywhere | `?` | Key list |
| Anywhere | Esc | Close dialog / menu |
| List | ↑ ↓, Enter | Move, open |
| List | Alt+N | New |
| Form | Ctrl+S | Save |
| Form | Ctrl+Enter | Save and submit |
| Form | Ctrl+P | Print the document (ERP print, not the browser's) |
| Form | Enter | Next field (transaction entry only, D6) |
| Lines | Ctrl+D / Ctrl+Delete | Add line / remove line (PO's keys, made standard) |

Never taken: F-keys, Ctrl+N / T / W, Alt+F / E / D (the browser keeps them). POS keeps its own keys.

**Done when:** a GRN can be made start to finish without the mouse.

## Phase C - My things first (139.5)

**Your actions:** confirm D7.

- **139.5 Recents and favourites.** Last 10 screens, reports and documents in the empty command bar. A ☆ on any screen or report pins it; pinned items head the command bar and a ★ menu in the top bar. Home is unchanged (Stage 88.1: Home shows transactions only). Storage: one additive column or table for favourites.

## Phase D - Fewer steps per transaction (139.6, pick from the list)

**Your actions:** pick which (D8).

| # | Extra | What the user gets | Recommended |
|---|---|---|---|
| a | Next step after save | "Saved PO 42 · Print · New · Make GRN" - no hunting for the next screen | Yes |
| b | Copy a document | Copy last week's PO for this vendor, change qty, save | Yes |
| c | Remember my last values | New form starts with the user's last store, warehouse, payment mode | Yes |
| d | Scan to open | Scan the barcode on a printed document → it opens | Yes |
| e | Bulk actions on lists | Tick many → approve / print / export | If wanted |
| f | Saved list views | My filters and columns kept per list | If wanted |
| g | Draft kept on tab close | Half-filled form comes back | If wanted |
| h | Counts in the command bar | "Approvals 4", "GRN pending 2" | If wanted |

Each one is checked against what already exists before building (some screens may have parts of it).

## Phase E - Documented and checked (139.7)

- **139.7** USER_GUIDE "Shortcuts" section; a one-page printable shortcut card (codes, report numbers, keys) generated from the two spec files, so it never goes stale; Help screen links it. Live check with Guide on a scratch instance.

## Not adopted

| Idea | Why not |
|---|---|
| SAP numeric codes (`VA01`) | Cryptic; letters are guessable |
| Tally F-keys in the browser | Browser owns several F-keys; a partial set confuses |
| Two-key chords (`g` then `s`) | Hard to discover; the command bar covers it |
| User-made codes | Same codes for every client keep training, guides and support simple. Later, if asked |
| A second search box or palette | The existing top-bar search becomes it |

## Guardrails

- One box, one index: the command bar extends `buildGlobalSearchIndex`; picking a screen still clicks the real sidebar item, so permissions and routing stay in one place.
- Cold core: `app.js` gets only a tiny key listener; the rest lives in a lazy `public/shortcuts.js`. Measured with `cmd/releasebudget`.
- Keys never fire while typing in a field (Ctrl / Alt combos excepted), while a dialog is open (`.modal-overlay.open`), inside POS, or inside a typeahead (shadow DOM, `composedPath`).
- Codes and numbers live in one spec file each, locked by tests; guides and the shortcut card are generated from them.
- No new dependency; the favourites storage is an additive migration.
