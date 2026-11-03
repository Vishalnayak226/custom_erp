# Every page its own submenu entry - plan (Stage 155, 2026-11-03)

User, 2026-11-03 (screenshot: Financial Accounting ▸ Finance / GL with the strip *Work to review · Trial Balance · Chart of Accounts · Accounting Periods*): *"I want all the one pager in submenus. Ex- I found in Finance / GL. Scan the pages Guide and prepare the plan."*

A "one pager" is a screen that puts several pages behind a tab strip. OMS (Stage 88), Inventory, Manufacturing, PIM (88.1.6), Reports (88.1) and HRM (151) already have one sidebar entry per page. **Seven screens still have a tab strip.** They were found by scanning the code (every `*_TABS`, `data-*-tab` and `txnTabs` strip) and cross-checked against Guide's saved map of app (`guide/atlas/app-super-admin.json`, learned 2026-10-23). No live Guide run was needed. Guide's local `walk` is the check after the build.

## Decisions

| # | Question | Recommended | Other option |
|---|---|---|---|
| D1 | POS **Sale · Return · Advance** | **Keep inside the till.** It is one till with one cart, and Stage 152 is making POS rigid. A sidebar jump would rebuild the till. | Three entries that open the till in that mode |
| D2 | Finance **Work to review** cards | **Keep as the first Finance entry.** Cut each card to its title, per the short-UI-text rule. | Drop it, since every card's page is now in the submenu |
| D3 | The Financial Accounting flyout grows to 15 entries | **Group them under headings**, as HRM does: General ledger · Payables · Receivables · Bank | One flat list |
| D4 | Settings ▸ Configuration module pills | **Keep.** It is one settings page with its own module picker; about 20 modules would flood Settings. | One entry per module |
| D5 | Status filters (GRN *To verify…Done*, E-Invoice *Needs action…*, E-way bill *Required…*) and List / New panes | **Keep.** They filter or page one list; they are not separate pages. The user's own "list first, New as its own page" rule. | - |

## Order of work

| Order | Item | Screen | New entries |
|---|---|---|---|
| 1 | 155.1 | Finance / GL | 4 |
| 2 | 155.2 | Debit / Credit Notes | 3 |
| 3 | 155.3 | Activity Log | 5 |
| 4 | 155.4 | Warehouse Cockpit | 3 |
| 5 | 155.5 | Stock Locations | 3 |
| 6 | 155.6 | CRM | +5 (13 total) |
| 7 | 155.7 | Loyalty | 4 |
| 8 | 155.8 | Guides, KB, Guide walk | - |

## How each split is built (same as Stage 151)

Each tab becomes a sidebar entry `data-go="<view>:<page>" data-like="<first entry>"`; it shows only to a role that can open it. The screen reads the page from the route, highlights its own entry and drops the strip; its title is the entry's name. Save, filter and refresh stay on that page. The old `#/view/<view>` link still opens the first page. Each entry can be found with Ctrl+K and starred (152.1). This is front end only: no schema, API or dependency change.

## Phases

**155.1 Finance / GL** (`view-finance.js`). *Why:* the user's example.
- Entries: **Work to review** (landing, `finance`) · **Trial Balance** · **Chart of Accounts** · **Accounting Periods** (`finance:trial-balance|chart-of-accounts|periods`).
- The "Period close" card opens `finance:periods`.
- Flyout (D3):
  - *General ledger:* Work to review · Trial Balance · Chart of Accounts · Accounting Periods · Journal Vouchers
  - *Payables:* Vendor Invoice · Payment Proposals · Debit Notes · Vendor Credit Notes
  - *Receivables:* Sales Invoice · Customer Credit Notes · E-Invoice (IRN) · E-Invoice Settings
  - *Bank:* Bank Reconciliation
  - Approvals stays cross-module.
- *Done when:* each entry opens its page directly, there is no strip, and F5 stays on the page.

**155.2 Debit / Credit Notes** (`view-finance.js`, `fn-screen` strip). Entries: **Debit Notes** · **Vendor Credit Notes** · **Customer Credit Notes**. Each keeps its own count, list and New. *Done when:* the Payables / Receivables entries open each note type's list.

**155.3 Activity Log** (`view-admin.js`, `log-hub-tab` strip). Under Settings ▸ Monitoring: **Activity Log** · **System Errors** · **Integration Payloads** · **Async Jobs** · **Security Events** · System Status. *Done when:* each entry opens its log with its own filters.

**155.4 Warehouse Cockpit** (`view-warehouse.js` + `view-wms-labour.js`). WMS flyout top: **Warehouse Cockpit** · **Labour** · **Warehouse Billing**. *Done when:* Labour and Billing open without passing through the cockpit.

**155.5 Stock Locations** (`view-stock-locations.js`). Inventory flyout: **Stock Locations** · **Locations by Store** · **Locations by Transaction**, beside Move Stock. *Done when:* each opens its own pane, and + New Location shows only on Stock Locations.

**155.6 CRM** (`view-crm.js`). The sidebar already has 8 CRM entries, but every CRM page still draws the 13-tab strip. Drop the strip and add the missing **Templates** · **Messages** · **Tickets** · **Feedback** · **Settings** (Settings only for `can_manage`). Each keeps its feature gate (messaging / service). Overview tiles still jump to their page. *Done when:* no CRM page has a strip, and all 13 pages are in the flyout.

**155.7 Loyalty** (`view-loyalty.js`). Under a *Loyalty* heading in CRM: **Customer Points** · **Tiers** · **Points Movement** · **Vouchers & Campaigns**. The Vouchers master list stays; the loyalty page is the usage view. *Done when:* each opens directly, and adjustments and tier saves stay on their page.

**155.8 Guides and the check.**
- In USER_GUIDE, ADMIN_GUIDE and `docs/kb`, each "open X → the Y tab" step becomes "Module ▸ Y".
- Guide local `walk` (admin, manager1, cashier1) plus `guidecheck` on the changed sections.
- `node --check`, and the frontend / module-manifest Go tests.
- Cold core stays under budget.
- *Done when:* no screen in the inventory above draws a strip, and Guide finds no dead entry.

## Kept as they are

| Screen | Why |
|---|---|
| POS Sale · Return · Advance | D1 |
| GRN steps, E-Invoice, E-way bill status strips | D5: filters of one list |
| List / New / History panes (every `txnPagedSection` screen, Roles' Access) | D5: the user's own page shape |
| Configuration module pills | D4 |
| Sticker designer Element · Label · Layers | A side panel inside the designer, not a page |
| OMS, Inventory, Manufacturing, PIM, Reports, HRM | Already one entry per page |

## Guardrails

- **Stage 152 (session erp-cc, Building) is changing the same sidebar** (`index.html`, `app.js`: favourites, the menu surviving a refresh). Build after it lands, or keep the hunks to the menu blocks above and re-read them just before each edit.
- Vanilla JS and `index.html` only. Routing rides the existing `data-go` listener (no new listener in the cold core). No new dependency.
- Nothing ships until the user says release / deploy.
