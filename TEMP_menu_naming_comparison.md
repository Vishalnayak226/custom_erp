# ERP Menu/Module Naming — Comparison (temp working doc, delete once decided)

Built 2026-07-23 to help decide final menu/module names for your ERP before turning it into a SaaS product.
**Bottom line up front: none of the words below are legally "owned" by any vendor.** They're generic
functional/accounting terms used industry-wide. The only naming risk is using an actual **brand name**
(SAP, S/4HANA, Fiori, NetSuite, SuitePeople, Dynamics 365, CloudSuite, Odoo) as *your own product/module name*
implying affiliation — not using the generic function words themselves. See the risk column at the end of each table.

Assumption: I read "next erp" as **Oracle NetSuite** (phonetically closest well-known cloud ERP). If you meant a
different specific product, tell me and I'll swap that column.

---

## Table 1 — Top-level module names

| Function area | SAP (S/4HANA & Business One) | Microsoft (Dynamics 365 Business Central) | Oracle Fusion Cloud ERP | Oracle NetSuite | Infor CloudSuite | Odoo | ERPNext (Frappe) | **Your ERP — current** |
|---|---|---|---|---|---|---|---|---|
| Financial accounting | Financials / FI-CO | Finance | Financials | Financial (under Transactions) | Financials | Accounting/Invoicing | Accounts | **Accounting** (flyout label; menu id `module-finance`) |
| Sales / order-to-cash | Sales & Distribution (SD) | Sales | Order Management | Customers | Customer Experience / CPQ | Sales | Selling | **Sales & Marketplace** (Fulfillment + Marketplace) |
| Procurement / purchasing | Materials Mgmt (MM) — buying side | Purchasing | Procurement | Vendors | Supply Mgmt | Purchase | Buying | **Buying** |
| Inventory / stock / warehouse | Materials Mgmt (MM) / WM / EWM | Warehouse | SCM → Inventory Mgmt | Inventory | Supply Chain Execution | Inventory | Stock | **Stock** |
| Manufacturing / production | Production Planning (PP) | Manufacturing | Manufacturing | Manufacturing | Manufacturing / Production | Manufacturing (MRP) | Manufacturing | **Manufacturing** |
| Point of sale | (separate SAP POS product) | Retail/POS add-on | Retail module | SuiteCommerce/POS | Infor POS | Point of Sale | POS | **Point of Sale** |
| HR / people / assets | Human Capital Mgmt (HCM) | Human Resources | HCM | SuitePeople (HR) | HCM/GHR | HR | HR | **HR & Assets** |
| CRM | SAP CRM (separate) / embedded | Sales (Relationship Mgmt) | CX / Sales Cloud | Customers (CRM) | CRM | CRM | CRM | *(not a separate module yet)* |
| Product/master data | Material Master | Item Master | Product Info Mgmt | Items | Product Data Mgmt | Product/Website | Item | **PIM** |
| Admin / config | IMG / Customizing | Setup & Extensions | Setup and Maintenance | Setup | Infor OS admin | Settings | Settings | **Settings** |
| Reports | SAP Analytics / Reports | Reports | OTBI / Reports | Reports | Birst/Reports | Reporting | Report | **Reports** |

**Risk read:** every cell above is a generic function word except the *vendor names in the column headers themselves*
(SAP, Dynamics 365, Fusion, NetSuite, CloudSuite, Odoo, ERPNext) — never use those as your product name. The cell
contents (Sales, Buying, Stock, HR, Settings, etc.) are safe to reuse verbatim; that's exactly why five different
vendors already converge on nearly the same words.

---

## Table 2 — Common transaction/document names

| Concept | SAP | Microsoft BC | Oracle Fusion | NetSuite | Infor | Odoo | ERPNext | Your ERP — current |
|---|---|---|---|---|---|---|---|---|
| Customer sales order | Sales Order | Sales Order | Sales Order | Sales Order | Sales Order | Sales Order | Sales Order | *(via Fulfillment)* |
| Purchase order | Purchase Order | Purchase Order | Purchase Order | Purchase Order | Purchase Order | Purchase Order | Purchase Order | Purchase Orders |
| Goods receipt | Goods Receipt (MIGO) | Posted Purchase Receipt | Receiving | Item Receipt | Receiving | Receipt | Purchase Receipt | *(GRN — check `engines/`)* |
| Vendor/supplier record | Vendor Master / Business Partner | Vendor | Supplier | Vendor | Supplier | Vendor | Supplier | Vendors |
| Customer record | Customer Master / Business Partner | Customer | Customer | Customer | Customer | Customer | Customer | *(under Fulfillment/Marketplace)* |
| Chart of accounts | Chart of Accounts | Chart of Accounts | Chart of Accounts | Chart of Accounts | Chart of Accounts | Chart of Accounts | Chart of Accounts | Finance / GL |
| Invoice (AR) | Billing Document | Sales Invoice | Receivables Invoice | Invoice | Invoice | Customer Invoice | Sales Invoice | Sales Invoices |
| Invoice (AP) | Vendor Invoice / MIRO | Purchase Invoice | Payables Invoice | Vendor Bill | Invoice | Vendor Bill | Purchase Invoice | Vendor Invoices |
| Bill of materials | BOM | Production BOM | BOM | Assembly/BOM | BOM | BOM (MRP) | BOM | *(check Manufacturing screens)* |
| Stock transfer | Stock Transport Order | Transfer Order | Transfer Order | Transfer Order | Transfer | Internal Transfer | Stock Entry | Transfers |
| Bin/location | Storage Bin | Bin | Locator/Subinventory | Bin | Location | Location | Warehouse/Bin | Bin Master |

**Risk read:** identical story — "Sales Order," "Purchase Order," "Chart of Accounts," "BOM," "Invoice" are universal
accounting-industry terms, not brand-specific IP. Nothing here needs to be renamed for legal safety.

---

## What genuinely would create risk (avoid these, not the generic words)

1. Naming your SaaS product/company something confusingly close to a real vendor (e.g. "SAP One," "Odoo Cloud," "NetSuite Lite").
2. Copying a vendor's logo, color scheme + layout closely enough that a screenshot looks like a rebadge of their product (trade dress).
3. Literally copying their source code (not a concern here — this codebase is original).
4. Claiming certification/partnership/affiliation with SAP/Oracle/Microsoft/Odoo you don't have (e.g. "SAP-certified" when it isn't).

## Suggested next step

Pick your final module names straight from Table 1's "Your ERP — current" column or mix-and-match from the other
columns (all safe) — e.g. decide between "Buying" vs "Procurement" vs "Purchasing" for that module, purely on which
reads clearest to your target users, not on legal grounds. Once you've decided, this file can be deleted — it's not
part of the tracked docs set (`micro_checklist.md` / `project_ledger.md` / `ai_handover.md`).
