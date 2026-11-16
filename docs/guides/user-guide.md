---
doc_id: DOC-78F8FF850A
title: User Guide
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

# User Guide

> **Applies to:** Stage 30.8 · **Last verified against the running app:** 2026-08-01
> Every step below was walked through against a live server on the date shown. If something here doesn't match what you see, the document is what's wrong — please report it.

**Welcome!** This guide explains how to use the ERP system. It's written in plain language for anyone using the system for the first time — no computer or accounting background needed. If a word might be unfamiliar, it's explained the first time it's used, and there's a glossary at the end.

*Need literal click-by-click steps for a screen not walked through in depth below? See **[user-sop.md](user-sop.md)** — the same plain-language style, one section per screen, covering every module.*

*Prefer to watch? The [editable process-video library](../sop-video/readme.md) currently has a
complete Purchase Requisition training chapter, including Department setup and a saved-number
check. Other video chapters are explicitly pending; use this written guide for those tasks.*

This guide is split into 9 parts so each one opens quickly. The sections of each part are listed below.

## How do I…?

Jump straight to the thing you're trying to do.

| I want to… | Go to |
|---|---|
| Log in for the first time | [§2](user-guide-getting-started.md#2-logging-in) |
| Watch a step-by-step Purchase Requisition example | [Process-video library](../sop-video/readme.md) |
| Find a screen I can't see in the menu | [§3](user-guide-getting-started.md#3-finding-your-way-around) |
| Open any screen, report or document fast (Ctrl+K, codes like `PO`, report numbers like `R1001`) | [§3A](user-guide-getting-started.md#3a-keyboard-shortcuts-and-quick-codes) |
| See every keyboard shortcut, or print a shortcut card | [§3A](user-guide-getting-started.md#3a-keyboard-shortcuts-and-quick-codes) (press **?** on any screen) |
| Copy a record or a purchase order instead of typing it again | [§3A.6](user-guide-getting-started.md#3a6-less-typing) |
| Approve, reject, submit, export or delete many rows at once | [§3A.6](user-guide-getting-started.md#3a6-less-typing) |
| **Ring up a sale** | [§4](user-guide-selling-and-stock.md#4-making-a-sale-pos--billing) — read the prerequisites box first |
| Open or close the till for a shift | [§4.0](user-guide-selling-and-stock.md#40-open-the-till-for-the-shift) |
| Find a product when I don't have its barcode | [§4.1](user-guide-selling-and-stock.md#41-ring-it-up) step 1 |
| Attach a customer to a sale, or add one at the counter | [§4.1](user-guide-selling-and-stock.md#41-ring-it-up) step 5 |
| Work out a customer's change from cash | [§4.1](user-guide-selling-and-stock.md#41-ring-it-up) step 8 |
| Understand why the till froze after a sale, and start the next one | [§4.1a](user-guide-selling-and-stock.md#41a-the-sale-is-complete--the-till-freezes) |
| Apply a coupon, or understand why an offer didn't appear | [§4.2](user-guide-selling-and-stock.md#42-offers-and-coupons-at-the-till) |
| Print a receipt, or make it print without a dialog | [§4.3](user-guide-selling-and-stock.md#43-printing-the-receipt) |
| Change which shop this till sells from | [§4.5](user-guide-selling-and-stock.md#45-reset-terminal--starting-the-till-again) |
| Understand why my shop isn't in the till's Store list | [§4.0](user-guide-selling-and-stock.md#40-open-the-till-for-the-shift) step 2 |
| Fix a screen showing old or wrong data (Refresh vs Reset) | [§12.3](user-guide-store-app-and-help.md#123-when-a-screen-shows-something-that-cant-be-right--refresh-vs-reset) |
| Print barcode stickers for everything on a GRN or a transfer | [§7A.1](user-guide-labels-and-printing.md#7a1-print-the-labels-for-a-grn-or-a-transfer-order) |
| Make one product category's label look different | [§7A.2](user-guide-labels-and-printing.md#7a2-design-your-labels-templates-and-the-sticker-studio), then [§7A.5](user-guide-labels-and-printing.md#7a5-which-template-each-item-uses-template-rules) |
| Change the standard label (copy it into my own template) | [§7A.2](user-guide-labels-and-printing.md#7a2-design-your-labels-templates-and-the-sticker-studio) (**Duplicate** on the **Standard label** card) |
| Choose which template an item, item type or category prints with | [§7A.5](user-guide-labels-and-printing.md#7a5-which-template-each-item-uses-template-rules) |
| Print one GRN line on a different label layout, just this once | [§7A.1](user-guide-labels-and-printing.md#7a1-print-the-labels-for-a-grn-or-a-transfer-order) (the line's **Template** dropdown) |
| Design a jewellery tag with weight, size, MRP and a QR code | [§7A.2](user-guide-labels-and-printing.md#7a2-design-your-labels-templates-and-the-sticker-studio) |
| Print 2 or 3 labels across a multi-up roll, or on an A4 sticker sheet | [§7A.2](user-guide-labels-and-printing.md#7a2-design-your-labels-templates-and-the-sticker-studio) (*Label tab*) |
| Fix labels that print off-centre, too light, or upside down | [§7A.2](user-guide-labels-and-printing.md#7a2-design-your-labels-templates-and-the-sticker-studio) (*Label tab → Printer tuning*, then **Test print**) |
| Re-print a single tag that fell off | [§7A.3](user-guide-labels-and-printing.md#7a3-print-labels-without-a-document-manual--re-print) |
| See who printed which labels, and when | [§7A.4](user-guide-labels-and-printing.md#7a4-what-was-printed-and-by-whom) |
| Scan a sticker and see which day it came in and how old it is | [§6.3](user-guide-purchase-orders.md#63-batches-and-expiry-dates-for-items-that-need-them) (*Batch Lookup & Ageing*) |
| List stock older than 90 days, oldest first | [§6.3](user-guide-purchase-orders.md#63-batches-and-expiry-dates-for-items-that-need-them) (*Batch Lookup & Ageing → Older than*) |
| See how much stock is 0-30, 31-60, 61-90, 91-180 or over 180 days old | [§6.3](user-guide-purchase-orders.md#63-batches-and-expiry-dates-for-items-that-need-them) (*Stock Ageing by GRN batch*) |
| Make the batch number scannable on my own label design | [§7A.2](user-guide-labels-and-printing.md#7a2-design-your-labels-templates-and-the-sticker-studio) (drag **Batch / Lot** onto a barcode) |
| Print a shipping label or a sales invoice, or make labels print with no dialog | **[qz-printing-setup.md](qz-printing-setup.md)** — "Day-to-day use" |
| Take a return | [USER_SOP §3.3](user-sop.md) |
| Check how much stock I have | [§5](user-guide-selling-and-stock.md#5-checking-stock) |
| **Order stock from a supplier** | [§6](user-guide-purchase-orders.md#6-ordering-more-stock-purchase-order) |
| Add items and prices to a purchase order | [§6](user-guide-purchase-orders.md#6-ordering-more-stock-purchase-order), step 3 |
| Print a purchase order and send it to the vendor | [§6](user-guide-purchase-orders.md#6-ordering-more-stock-purchase-order), step 7 |
| Create a new design and its colour / size combinations from the PO | [§6.9](user-guide-purchase-orders.md#69-choosing-an-item-names-attributes-in-the-po-designs-and-combinations) |
| Receive several POs of one vendor on one GRN | [§6.10](user-guide-receiving-and-transfers.md#610-receiving-goods-in-steps-receive-verify-price-check-mrp-pay) |
| Record the vendor's invoice price when it differs from the PO (debit / credit note) | [§6.10](user-guide-receiving-and-transfers.md#610-receiving-goods-in-steps-receive-verify-price-check-mrp-pay) |
| See what a sale's cost comes from, and what a late charge does to it | [§6.15](user-guide-receiving-and-transfers.md#615-what-stock-costs-and-what-a-sale-books) |
| Send the vendor a PO in Excel with item pictures | [§7B.1](user-guide-labels-and-printing.md#7b1-printing-a-document) |
| Print a GRN, transfer challan, debit or credit note | [§7B.1](user-guide-labels-and-printing.md#7b1-printing-a-document) |
| Change how a PO / GRN / transfer / note prints (my own template) | [§7B.2](user-guide-labels-and-printing.md#7b2-designing-a-print-template-setup--printing--templates) |
| Use a different print layout for one vendor or one product range | [§7B.2](user-guide-labels-and-printing.md#7b2-designing-a-print-template-setup--printing--templates), step 6 |
| Understand inclusive vs exclusive GST, or why a PO says IGST | [§6.2](user-guide-purchase-orders.md#62-gst-on-a-purchase-order-inclusive-vs-exclusive-and-inter-state) |
| Record stock that has arrived | [§6](user-guide-purchase-orders.md#6-ordering-more-stock-purchase-order), step 9 |
| Complete a missing barcode when receiving stock | [§6.4](user-guide-purchase-orders.md#64-completing-missing-barcodes-at-receipt) — existing/imported Items are covered automatically |
| Understand where document numbers come from | [§6.1](user-guide-purchase-orders.md#61-where-document-numbers-come-from) |
| Sell a gift or free-with-item product, or use backflush materials in production | [§6.8](user-guide-purchase-orders.md#68-item-types-gifts-free-items-accessories-and-backflush-materials) |
| Move stock between locations | [§7](user-guide-receiving-and-transfers.md#7-moving-stock-between-locations-stock-movement) |
| Add a vendor, item, brand, location… | [§8](user-guide-master-data.md#8-managing-master-data-vendors-locations-brands-and-similar-lists) |
| Correct a record I got wrong | [§8](user-guide-master-data.md#8-managing-master-data-vendors-locations-brands-and-similar-lists) — use the row's **Edit** icon |
| Create a missing vendor/item mid-way through a document, then carry on | [§8.1.1](user-guide-master-data.md#811-you-are-brought-back-to-where-you-came-from) |
| **Set up jewellery Designs and their SKUs (Combination IDs)** | [§8](user-guide-master-data.md#8-managing-master-data-vendors-locations-brands-and-similar-lists)d |
| Understand the extra jewellery fields on an Item | [§8](user-guide-master-data.md#8-managing-master-data-vendors-locations-brands-and-similar-lists)d.1 |
| **Record what competitors charge, and see where I sit against them** | [§8](user-guide-master-data.md#8-managing-master-data-vendors-locations-brands-and-similar-lists)a, then the Competitor Price Gap report in [§9](user-guide-reports-and-orders.md#9-running-a-report) |
| Run a report / find the right report | [§9](user-guide-reports-and-orders.md#9-running-a-report) and **[report-catalog.md](report-catalog.md)** |
| **Place a customer order by hand (phone / walk-in / replacement)** | [§9A.1](user-guide-reports-and-orders.md#9a1-placing-a-manual-order) |
| Check whether a marketplace or Unicommerce order reached the ERP | [§9A.2](user-guide-reports-and-orders.md#9a2-finding-an-order-and-seeing-that-it-reached-the-erp) |
| Process an order through to shipment and invoice | [§9A.3](user-guide-reports-and-orders.md#9a3-processing-an-order-through-to-invoice) and [§9A.4](user-guide-reports-and-orders.md#9a4-the-full-order-to-cash-walkthrough) |
| Understand why a field says my GSTIN, email or phone number is wrong | [§8](user-guide-master-data.md#8-managing-master-data-vendors-locations-brands-and-similar-lists)e |
| Pick a state, fill an address, or see why a new record's code is V00001 | [§8](user-guide-master-data.md#8-managing-master-data-vendors-locations-brands-and-similar-lists)f |
| Approve or reject something | [§10](user-guide-finance-crm-and-approvals.md#10-approvals) |
| Approve or reject from my phone | [§10A.4](user-guide-store-app-and-help.md#10a4-approvals) |
| Scan an item on my phone for its price, stock at other stores or batch age | [§10A.5](user-guide-store-app-and-help.md#10a5-scan-an-item) |
| Install the Store app on a phone | [§10A.1](user-guide-store-app-and-help.md#10a1-install-it-once-per-phone) |
| Sign in on the store's shared phone with a PIN | [§10A.2](user-guide-store-app-and-help.md#10a2-sign-in), [§10A.14](user-guide-store-app-and-help.md#10a14-settings-your-pin-and-this-phone) |
| See today's sales for my store on my phone | [§10A.7](user-guide-store-app-and-help.md#10a7-today-store-managers) |
| Check a delivery or a transfer by scanning it on my phone | [§10A.8](user-guide-store-app-and-help.md#10a8-receive-by-scanning) |
| Count stock on my phone, even with no connection | [§10A.9](user-guide-store-app-and-help.md#10a9-count-by-scanning-and-ask-for-stock) |
| Print an item's labels from my phone | [§10A.10](user-guide-store-app-and-help.md#10a10-print-labels-from-the-phone) |
| Build a basket on my phone for the till | [§10A.11](user-guide-store-app-and-help.md#10a11-basket-for-the-till) |
| Order a size from another store for home delivery | [§10A.12](user-guide-store-app-and-help.md#10a12-order-for-home-delivery) |
| Get an alert on my phone for a new approval or low stock | [§10A.13](user-guide-store-app-and-help.md#10a13-alerts) |
| Post an accrual, correction or opening balance (journal voucher) | [§9B.1](user-guide-finance-crm-and-approvals.md#9b1-raising-a-journal-voucher-accruals-corrections-opening-balances) |
| **Log a customer's service call and work it to closure** | [§9C](user-guide-finance-crm-and-approvals.md#9c-service--maintenance-contracts-and-service-tickets) |
| Record a customer's maintenance contract (AMC) | [§9C](user-guide-finance-crm-and-approvals.md#9c-service--maintenance-contracts-and-service-tickets) |
| **See everything about one customer, and record their consent** | [§9D.1](user-guide-finance-crm-and-approvals.md#9d1-the-customer-page), [§9D.2](user-guide-finance-crm-and-approvals.md#9d2-consent-who-may-be-messaged) |
| Build a segment of customers | [§9D.3](user-guide-finance-crm-and-approvals.md#9d3-segments-and-uploaded-lists) |
| **Send a campaign, get it approved, and see what it brought in** | [§9D.5](user-guide-finance-crm-and-approvals.md#9d5-campaigns) |
| Switch on birthday, welcome, win-back and other automatic messages | [§9D.6](user-guide-finance-crm-and-approvals.md#9d6-journeys--messages-that-send-themselves) |
| Find out why a message was not sent | [§9D.7](user-guide-finance-crm-and-approvals.md#9d7-messages--what-was-sent-and-why-not) |
| Answer WhatsApp replies; work a complaint ticket | [§9D.8](user-guide-finance-crm-and-approvals.md#9d8-inbox-whatsapp-replies), [§9D.9](user-guide-finance-crm-and-approvals.md#9d9-tickets) |
| Ask customers for feedback after a bill; record a referral | [§9D.10](user-guide-finance-crm-and-approvals.md#9d10-feedback-and-referrals) |
| Book a customer appointment; ask a store to keep an item | [§9D.11](user-guide-finance-crm-and-approvals.md#9d11-worklist-tasks-appointments-holds) |
| Track business-buyer leads and deals to a Sales Order | [§9D.12](user-guide-finance-crm-and-approvals.md#9d12-pipeline--business-buyers) |
| Serve a customer from my phone; see my tasks | [§10A.15](user-guide-store-app-and-help.md#10a15-serve-a-customer), [§10A.16](user-guide-store-app-and-help.md#10a16-my-tasks) |
| Move or dispose of a fixed asset | [§6.7](user-guide-purchase-orders.md#67-buying-equipment-for-the-business-fixed-assets) step 5 |
| Change my own password or auto-logout | [§11](user-guide-store-app-and-help.md#11-your-profile-password-and-session) |
| Find out which version I am on, to report a problem | [§11.1](user-guide-store-app-and-help.md#111-which-version-you-are-on) |
| Learn what a screen is for, why it matters and how to use it | [§11.2](user-guide-store-app-and-help.md#112-help-on-any-screen) |
| **Understand an error message or code** | [§12](user-guide-store-app-and-help.md#12-if-you-get-logged-out-or-see-an-error) and **[error-codes.md](error-codes.md)** |
| **Set the whole thing up from scratch and make my first sale** | [§13](user-guide-store-app-and-help.md#13-open-a-shop-and-make-your-first-sale--the-full-worked-example) — the full worked example |
| Know what my role is allowed to do | **[PERMISSION_MATRIX.md](../archive/legacy/permission-matrix.md)** |

## Parts

1. **[Getting started](user-guide-getting-started.md)**
   - [1. What is this system?](user-guide-getting-started.md#1-what-is-this-system)
   - [2. Logging In](user-guide-getting-started.md#2-logging-in)
      - [If your role uses MFA: your recovery codes](user-guide-getting-started.md#if-your-role-uses-mfa-your-recovery-codes)
      - [Remember this device (fewer codes on your own computer)](user-guide-getting-started.md#remember-this-device-fewer-codes-on-your-own-computer)
      - [Moving your authenticator to a new phone](user-guide-getting-started.md#moving-your-authenticator-to-a-new-phone)
   - [3. Finding Your Way Around](user-guide-getting-started.md#3-finding-your-way-around)
      - [Home, your landing screen](user-guide-getting-started.md#home-your-landing-screen)
   - [3A. Keyboard Shortcuts and Quick Codes](user-guide-getting-started.md#3a-keyboard-shortcuts-and-quick-codes)
      - [3A.1 The search box: Ctrl+K](user-guide-getting-started.md#3a1-the-search-box-ctrlk)
      - [3A.2 Keys](user-guide-getting-started.md#3a2-keys)
      - [3A.3 Codes](user-guide-getting-started.md#3a3-codes)
      - [3A.4 Report numbers](user-guide-getting-started.md#3a4-report-numbers)
      - [3A.5 Favourites, recent and after a save](user-guide-getting-started.md#3a5-favourites-recent-and-after-a-save)
      - [3A.6 Less typing](user-guide-getting-started.md#3a6-less-typing)
2. **[Selling and checking stock](user-guide-selling-and-stock.md)**
   - [4. Making a Sale (POS / Billing)](user-guide-selling-and-stock.md#4-making-a-sale-pos--billing)
      - [4.0 Open the till for the shift](user-guide-selling-and-stock.md#40-open-the-till-for-the-shift)
      - [4.1 Ring it up](user-guide-selling-and-stock.md#41-ring-it-up)
      - [4.1a The sale is complete — the till freezes](user-guide-selling-and-stock.md#41a-the-sale-is-complete--the-till-freezes)
      - [4.2 Offers and coupons at the till](user-guide-selling-and-stock.md#42-offers-and-coupons-at-the-till)
      - [4.3 Printing the receipt](user-guide-selling-and-stock.md#43-printing-the-receipt)
      - [4.4 If prices changed while you were ringing up](user-guide-selling-and-stock.md#44-if-prices-changed-while-you-were-ringing-up)
      - [4.5 Reset Terminal — starting the till again](user-guide-selling-and-stock.md#45-reset-terminal--starting-the-till-again)
      - [4.6 Packaging, free gifts, gift cards — and items the till won't sell](user-guide-selling-and-stock.md#46-packaging-free-gifts-gift-cards--and-items-the-till-wont-sell)
      - [4.7 Returns — what the customer gets back](user-guide-selling-and-stock.md#47-returns--what-the-customer-gets-back)
      - [4.8 A customer's points — the Loyalty screen (store managers)](user-guide-selling-and-stock.md#48-a-customers-points--the-loyalty-screen-store-managers)
      - [4.9 Customer advances — the Advance tab](user-guide-selling-and-stock.md#49-customer-advances--the-advance-tab)
      - [4.10 Large bills — address, PAN and the cash limit](user-guide-selling-and-stock.md#410-large-bills--address-pan-and-the-cash-limit)
      - [4.10a B2B bill — the buyer's GSTIN, PO and ship-to](user-guide-selling-and-stock.md#410a-b2b-bill--the-buyers-gstin-po-and-ship-to)
      - [4.11 Many lines at once — Upload lines](user-guide-selling-and-stock.md#411-many-lines-at-once--upload-lines)
      - [4.12 Offline Sales — validating sales rung up offline (store managers)](user-guide-selling-and-stock.md#412-offline-sales--validating-sales-rung-up-offline-store-managers)
      - [4.13 Sales History — every sale and return](user-guide-selling-and-stock.md#413-sales-history--every-sale-and-return)
   - [5. Checking Stock](user-guide-selling-and-stock.md#5-checking-stock)
      - [5.1 Choosing a wave on Mobile Picking](user-guide-selling-and-stock.md#51-choosing-a-wave-on-mobile-picking)
      - [5.2 How a pick list holds stock in the bins](user-guide-selling-and-stock.md#52-how-a-pick-list-holds-stock-in-the-bins)
      - [5.1 Stock locations inside a store — what sells and what doesn't](user-guide-selling-and-stock.md#51-stock-locations-inside-a-store--what-sells-and-what-doesnt)
      - [5.3 Inventory Dashboard](user-guide-selling-and-stock.md#53-inventory-dashboard)
      - [5.4 Inventory Ledger](user-guide-selling-and-stock.md#54-inventory-ledger)
      - [5.5 Stock Adjustment](user-guide-selling-and-stock.md#55-stock-adjustment)
      - [5.6 MRP Change](user-guide-selling-and-stock.md#56-mrp-change)
      - [5.7 Material Issue (store items for a department's own use)](user-guide-selling-and-stock.md#57-material-issue-store-items-for-a-departments-own-use)
      - [5.8 Delivery Challans (goods out without a sale)](user-guide-selling-and-stock.md#58-delivery-challans-goods-out-without-a-sale)
3. **[Purchase orders](user-guide-purchase-orders.md)**
   - [6. Ordering More Stock (Purchase Order)](user-guide-purchase-orders.md#6-ordering-more-stock-purchase-order)
      - [6.1 Where document numbers come from](user-guide-purchase-orders.md#61-where-document-numbers-come-from)
      - [6.2 GST on a purchase order: inclusive vs exclusive, and inter-state](user-guide-purchase-orders.md#62-gst-on-a-purchase-order-inclusive-vs-exclusive-and-inter-state)
      - [6.2a Vendor price agreements, discounts and free quantity (Stages 148.6, 148.7)](user-guide-purchase-orders.md#62a-vendor-price-agreements-discounts-and-free-quantity-stages-1486-1487)
      - [6.2b Charge codes: freight that goes into item cost (Stage 148.8)](user-guide-purchase-orders.md#62b-charge-codes-freight-that-goes-into-item-cost-stage-1488)
      - [6.3 Batches and expiry dates (for items that need them)](user-guide-purchase-orders.md#63-batches-and-expiry-dates-for-items-that-need-them)
      - [6.4 Asking vendors for quotes (RFQ)](user-guide-purchase-orders.md#64-asking-vendors-for-quotes-rfq)
      - [6.5 Sending goods back to a supplier](user-guide-purchase-orders.md#65-sending-goods-back-to-a-supplier)
      - [6.6 Paying the supplier's bill (Vendor Invoice and the three-way match)](user-guide-purchase-orders.md#66-paying-the-suppliers-bill-vendor-invoice-and-the-three-way-match)
      - [6.7 Buying equipment for the business (Fixed Assets)](user-guide-purchase-orders.md#67-buying-equipment-for-the-business-fixed-assets)
      - [6.8 Item types: gifts, free items, accessories and backflush materials](user-guide-purchase-orders.md#68-item-types-gifts-free-items-accessories-and-backflush-materials)
      - [6.9 Choosing an item name's attributes in the PO (designs and combinations)](user-guide-purchase-orders.md#69-choosing-an-item-names-attributes-in-the-po-designs-and-combinations)
4. **[Receiving goods and moving stock](user-guide-receiving-and-transfers.md)**
      - [6.10 Receiving goods in steps: receive, verify, price check, MRP, pay](user-guide-receiving-and-transfers.md#610-receiving-goods-in-steps-receive-verify-price-check-mrp-pay)
      - [6.11 Adding photos to designs and Combination IDs (after the PO)](user-guide-receiving-and-transfers.md#611-adding-photos-to-designs-and-combination-ids-after-the-po)
      - [6.12 Ordering and receiving from one Excel file (Upload from Excel)](user-guide-receiving-and-transfers.md#612-ordering-and-receiving-from-one-excel-file-upload-from-excel)
      - [6.13 Delivery dates, reminders and day limits](user-guide-receiving-and-transfers.md#613-delivery-dates-reminders-and-day-limits)
      - [6.14 A PO's commercial terms, and a receipt's challan and transport](user-guide-receiving-and-transfers.md#614-a-pos-commercial-terms-and-a-receipts-challan-and-transport)
      - [6.15 What stock costs, and what a sale books](user-guide-receiving-and-transfers.md#615-what-stock-costs-and-what-a-sale-books)
   - [7. Moving Stock Between Locations (Stock Movement)](user-guide-receiving-and-transfers.md#7-moving-stock-between-locations-stock-movement)
      - [7.1 Moving stock between bins in the same location (Location Movement)](user-guide-receiving-and-transfers.md#71-moving-stock-between-bins-in-the-same-location-location-movement)
      - [7.2 A warehouse shared by several clients (3PL owners)](user-guide-receiving-and-transfers.md#72-a-warehouse-shared-by-several-clients-3pl-owners)
      - [7.3 Receiving a delivery: gate, door, RF receiving and putaway (Stage 55.7)](user-guide-receiving-and-transfers.md#73-receiving-a-delivery-gate-door-rf-receiving-and-putaway-stage-557)
      - [7.3a A draft order from an AI assistant](user-guide-receiving-and-transfers.md#73a-a-draft-order-from-an-ai-assistant)
      - [7.4 Shipping orders: wave, pick, pack and load (Stage 55.8)](user-guide-receiving-and-transfers.md#74-shipping-orders-wave-pick-pack-and-load-stage-558)
      - [7.4a Gate entry, and the packing list per box (Stage 146)](user-guide-receiving-and-transfers.md#74a-gate-entry-and-the-packing-list-per-box-stage-146)
      - [7.5 Warehouse labour and billing (Stage 55.8)](user-guide-receiving-and-transfers.md#75-warehouse-labour-and-billing-stage-558)
      - [7.6 The warehouse (WMS) screens — where to start](user-guide-receiving-and-transfers.md#76-the-warehouse-wms-screens--where-to-start)
      - [7.7 Transfers: purpose, short stock, waves and an allocation upload (Stage 146.3)](user-guide-receiving-and-transfers.md#77-transfers-purpose-short-stock-waves-and-an-allocation-upload-stage-1463)
5. **[Labels and printing documents](user-guide-labels-and-printing.md)**
   - [7A. Printing Barcode Stickers and Labels](user-guide-labels-and-printing.md#7a-printing-barcode-stickers-and-labels)
      - [7A.1 Print the labels for a GRN or a Transfer Order](user-guide-labels-and-printing.md#7a1-print-the-labels-for-a-grn-or-a-transfer-order)
      - [7A.2 Design your labels (Templates and the Sticker Studio)](user-guide-labels-and-printing.md#7a2-design-your-labels-templates-and-the-sticker-studio)
      - [7A.3 Print labels without a document (manual / re-print)](user-guide-labels-and-printing.md#7a3-print-labels-without-a-document-manual--re-print)
      - [7A.4 What was printed, and by whom](user-guide-labels-and-printing.md#7a4-what-was-printed-and-by-whom)
      - [7A.5 Which template each item uses (Template rules)](user-guide-labels-and-printing.md#7a5-which-template-each-item-uses-template-rules)
      - [7A.6 Set a label printer up once (Printer preset)](user-guide-labels-and-printing.md#7a6-set-a-label-printer-up-once-printer-preset)
   - [7B. Printing POs, GRNs, Transfers and Notes — and Designing Their Layout](user-guide-labels-and-printing.md#7b-printing-pos-grns-transfers-and-notes--and-designing-their-layout)
      - [7B.1 Printing a document](user-guide-labels-and-printing.md#7b1-printing-a-document)
      - [7B.2 Designing a print template (Setup → Printing → Templates)](user-guide-labels-and-printing.md#7b2-designing-a-print-template-setup--printing--templates)
      - [7B.3 Which printer prints what, on each computer](user-guide-labels-and-printing.md#7b3-which-printer-prints-what-on-each-computer)
      - [7B.4 Reprint a document](user-guide-labels-and-printing.md#7b4-reprint-a-document)
6. **[Master data](user-guide-master-data.md)**
   - [8. Managing Master Data (Vendors, Locations, Brands, and Similar Lists)](user-guide-master-data.md#8-managing-master-data-vendors-locations-brands-and-similar-lists)
      - [8.1 The system tells you what is missing — read the hints](user-guide-master-data.md#81-the-system-tells-you-what-is-missing--read-the-hints)
      - [8.1a How pickers search](user-guide-master-data.md#81a-how-pickers-search)
      - [8.2 Phone numbers](user-guide-master-data.md#82-phone-numbers)
      - [8.3 Price lists (Price List Version)](user-guide-master-data.md#83-price-lists-price-list-version)
      - [8a. Tracking What Competitors Charge](user-guide-master-data.md#8a-tracking-what-competitors-charge)
      - [8a. Setting up PIM in order](user-guide-master-data.md#8a-setting-up-pim-in-order)
      - [8b. Grouping Products for PIM Work](user-guide-master-data.md#8b-grouping-products-for-pim-work)
      - [8b-1. Marketplace templates, feeds and design values](user-guide-master-data.md#8b-1-marketplace-templates-feeds-and-design-values)
      - [8b-2. Attribute types, related items and the small PIM helpers](user-guide-master-data.md#8b-2-attribute-types-related-items-and-the-small-pim-helpers)
      - [8c. Handing out PIM work: tasks and workflows](user-guide-master-data.md#8c-handing-out-pim-work-tasks-and-workflows)
      - [8d. Jewellery items: Designs, Combinations (SKUs) and the extra attribute fields](user-guide-master-data.md#8d-jewellery-items-designs-combinations-skus-and-the-extra-attribute-fields)
      - [8e. Fields that check what you typed (GSTIN, email, phone, PAN, IFSC, PIN code)](user-guide-master-data.md#8e-fields-that-check-what-you-typed-gstin-email-phone-pan-ifsc-pin-code)
      - [8f. States, addresses and record codes](user-guide-master-data.md#8f-states-addresses-and-record-codes)
      - [8g. Creating an item: what kind it is, its item name, and the rest is filled in](user-guide-master-data.md#8g-creating-an-item-what-kind-it-is-its-item-name-and-the-rest-is-filled-in)
      - [8h. Data quality queue and import order (Stage 170.41, 170.42)](user-guide-master-data.md#8h-data-quality-queue-and-import-order-stage-17041-17042)
      - [8i. Data Healer: one-time fixes after an update (ask 207)](user-guide-master-data.md#8i-data-healer-one-time-fixes-after-an-update-ask-207)
7. **[Reports and customer orders](user-guide-reports-and-orders.md)**
   - [9. Running a Report](user-guide-reports-and-orders.md#9-running-a-report)
      - [9.1 Your report catalog](user-guide-reports-and-orders.md#91-your-report-catalog)
      - [9.1a Merge duplicates, order-to-cash mismatches, setup history (Stages 47.13.5, 49.5.5, 47.12.4)](user-guide-reports-and-orders.md#91a-merge-duplicates-order-to-cash-mismatches-setup-history-stages-47135-4955-47124)
      - [9.2 Custom dashboards — your own charts and numbers](user-guide-reports-and-orders.md#92-custom-dashboards--your-own-charts-and-numbers)
   - [9A. Orders (OMS) — taking and processing customer orders](user-guide-reports-and-orders.md#9a-orders-oms--taking-and-processing-customer-orders)
      - [9A.1 Placing a manual order](user-guide-reports-and-orders.md#9a1-placing-a-manual-order)
      - [9A.2 Finding an order, and seeing that it reached the ERP](user-guide-reports-and-orders.md#9a2-finding-an-order-and-seeing-that-it-reached-the-erp)
      - [9A.3 Processing an order through to invoice](user-guide-reports-and-orders.md#9a3-processing-an-order-through-to-invoice)
      - [9A.4 The full order-to-cash walkthrough](user-guide-reports-and-orders.md#9a4-the-full-order-to-cash-walkthrough)
      - [9A.5 Invoicing a customer in a foreign currency](user-guide-reports-and-orders.md#9a5-invoicing-a-customer-in-a-foreign-currency)
      - [9A.6 Stuck work, and an order's whole story](user-guide-reports-and-orders.md#9a6-stuck-work-and-an-orders-whole-story)
      - [9A.7 Changing an order: lines, cancelling after release, reship and replacement](user-guide-reports-and-orders.md#9a7-changing-an-order-lines-cancelling-after-release-reship-and-replacement)
      - [9A.8 Pending Orders — accept or reject a channel order](user-guide-reports-and-orders.md#9a8-pending-orders--accept-or-reject-a-channel-order)
      - [9A.9 Pick](user-guide-reports-and-orders.md#9a9-pick)
      - [9A.10 Pack — and the two bills](user-guide-reports-and-orders.md#9a10-pack--and-the-two-bills)
      - [9A.11 Ship](user-guide-reports-and-orders.md#9a11-ship)
      - [9A.12 Settlements](user-guide-reports-and-orders.md#9a12-settlements)
      - [9A.12a Freight — carrier claims and the carrier's invoice](user-guide-reports-and-orders.md#9a12a-freight--carrier-claims-and-the-carriers-invoice)
      - [9A.13 OMS settings](user-guide-reports-and-orders.md#9a13-oms-settings)
      - [9A.14 Orders from your Shopify shop (Stage 99)](user-guide-reports-and-orders.md#9a14-orders-from-your-shopify-shop-stage-99)
8. **[Finance, service, CRM, people and approvals](user-guide-finance-crm-and-approvals.md)**
   - [9B. Finance work to review](user-guide-finance-crm-and-approvals.md#9b-finance-work-to-review)
      - [9B.2 Closing a month (Accounting Periods)](user-guide-finance-crm-and-approvals.md#9b2-closing-a-month-accounting-periods)
      - [9B.3 Reconciling a bank account](user-guide-finance-crm-and-approvals.md#9b3-reconciling-a-bank-account)
      - [9B.3a Raising a sales invoice (Stage 130.16)](user-guide-finance-crm-and-approvals.md#9b3a-raising-a-sales-invoice-stage-13016)
      - [9B.3a-1 Custom jewellery order (Stage 148.36, module Custom orders)](user-guide-finance-crm-and-approvals.md#9b3a-1-custom-jewellery-order-stage-14836-module-custom-orders)
      - [9B.3b Many sales invoices from one Excel file (Upload from Excel)](user-guide-finance-crm-and-approvals.md#9b3b-many-sales-invoices-from-one-excel-file-upload-from-excel)
      - [9B.4 Receiving and paying in parts, and credit notes against an invoice](user-guide-finance-crm-and-approvals.md#9b4-receiving-and-paying-in-parts-and-credit-notes-against-an-invoice)
      - [9B.1a Setting off what a customer owes against what you owe them (148.23)](user-guide-finance-crm-and-approvals.md#9b1a-setting-off-what-a-customer-owes-against-what-you-owe-them-14823)
      - [9B.1 Raising a journal voucher (accruals, corrections, opening balances)](user-guide-finance-crm-and-approvals.md#9b1-raising-a-journal-voucher-accruals-corrections-opening-balances)
      - [9B.5 E-invoices (IRN): the workbench](user-guide-finance-crm-and-approvals.md#9b5-e-invoices-irn-the-workbench)
      - [9B.6 E-way bills (Stage 130.16)](user-guide-finance-crm-and-approvals.md#9b6-e-way-bills-stage-13016)
   - [9C. Service — maintenance contracts and service tickets](user-guide-finance-crm-and-approvals.md#9c-service--maintenance-contracts-and-service-tickets)
   - [9D. CRM — customers, consent, campaigns, journeys, service and the sales pipeline](user-guide-finance-crm-and-approvals.md#9d-crm--customers-consent-campaigns-journeys-service-and-the-sales-pipeline)
      - [9D.1 The customer page](user-guide-finance-crm-and-approvals.md#9d1-the-customer-page)
      - [9D.2 Consent: who may be messaged](user-guide-finance-crm-and-approvals.md#9d2-consent-who-may-be-messaged)
      - [9D.3 Segments and uploaded lists](user-guide-finance-crm-and-approvals.md#9d3-segments-and-uploaded-lists)
      - [9D.4 Message templates](user-guide-finance-crm-and-approvals.md#9d4-message-templates)
      - [9D.5 Campaigns](user-guide-finance-crm-and-approvals.md#9d5-campaigns)
      - [9D.6 Journeys — messages that send themselves](user-guide-finance-crm-and-approvals.md#9d6-journeys--messages-that-send-themselves)
      - [9D.7 Messages — what was sent and why not](user-guide-finance-crm-and-approvals.md#9d7-messages--what-was-sent-and-why-not)
      - [9D.8 Inbox (WhatsApp replies)](user-guide-finance-crm-and-approvals.md#9d8-inbox-whatsapp-replies)
      - [9D.9 Tickets](user-guide-finance-crm-and-approvals.md#9d9-tickets)
      - [9D.10 Feedback and referrals](user-guide-finance-crm-and-approvals.md#9d10-feedback-and-referrals)
      - [9D.11 Worklist: tasks, appointments, holds](user-guide-finance-crm-and-approvals.md#9d11-worklist-tasks-appointments-holds)
      - [9D.12 Pipeline — business buyers](user-guide-finance-crm-and-approvals.md#9d12-pipeline--business-buyers)
      - [9D.13 CRM reports and dashboard](user-guide-finance-crm-and-approvals.md#9d13-crm-reports-and-dashboard)
      - [9D.14 Campaigns made before this version](user-guide-finance-crm-and-approvals.md#9d14-campaigns-made-before-this-version)
   - [9E. People — HR, leave, payroll and expense claims](user-guide-finance-crm-and-approvals.md#9e-people--hr-leave-payroll-and-expense-claims)
   - [9F. Manufacturing and supply planning](user-guide-finance-crm-and-approvals.md#9f-manufacturing-and-supply-planning)
      - [9F.1 Supply proposals — what to buy, move or make (Stage 73.2.18)](user-guide-finance-crm-and-approvals.md#9f1-supply-proposals--what-to-buy-move-or-make-stage-73218)
      - [9F.2 BOMs and production orders (Stage 55.12)](user-guide-finance-crm-and-approvals.md#9f2-boms-and-production-orders-stage-5512)
      - [9F.2a Job work (Stage 130.16)](user-guide-finance-crm-and-approvals.md#9f2a-job-work-stage-13016)
      - [9F.3 Quality inspections, certificates, non-conformance and maintenance (Stage 55.13)](user-guide-finance-crm-and-approvals.md#9f3-quality-inspections-certificates-non-conformance-and-maintenance-stage-5513)
      - [9F.3a Lots held for inspection, and stock that has sat too long (148.33, 146.3.11)](user-guide-finance-crm-and-approvals.md#9f3a-lots-held-for-inspection-and-stock-that-has-sat-too-long-14833-146311)
   - [10. Approvals](user-guide-finance-crm-and-approvals.md#10-approvals)
9. **[Store app, profile and help](user-guide-store-app-and-help.md)**
   - [10A. The Store app on your phone](user-guide-store-app-and-help.md#10a-the-store-app-on-your-phone)
      - [10A.1 Install it (once per phone)](user-guide-store-app-and-help.md#10a1-install-it-once-per-phone)
      - [10A.2 Sign in](user-guide-store-app-and-help.md#10a2-sign-in)
      - [10A.3 Your store](user-guide-store-app-and-help.md#10a3-your-store)
      - [10A.4 Approvals](user-guide-store-app-and-help.md#10a4-approvals)
      - [10A.5 Scan an item](user-guide-store-app-and-help.md#10a5-scan-an-item)
      - [10A.6 No connection](user-guide-store-app-and-help.md#10a6-no-connection)
      - [10A.7 Today (store managers)](user-guide-store-app-and-help.md#10a7-today-store-managers)
      - [10A.8 Receive by scanning](user-guide-store-app-and-help.md#10a8-receive-by-scanning)
      - [10A.9 Count by scanning, and ask for stock](user-guide-store-app-and-help.md#10a9-count-by-scanning-and-ask-for-stock)
      - [10A.10 Print labels from the phone](user-guide-store-app-and-help.md#10a10-print-labels-from-the-phone)
      - [10A.11 Basket for the till](user-guide-store-app-and-help.md#10a11-basket-for-the-till)
      - [10A.12 Order for home delivery](user-guide-store-app-and-help.md#10a12-order-for-home-delivery)
      - [10A.13 Alerts](user-guide-store-app-and-help.md#10a13-alerts)
      - [10A.14 Settings: your PIN and this phone](user-guide-store-app-and-help.md#10a14-settings-your-pin-and-this-phone)
      - [10A.15 Serve a customer](user-guide-store-app-and-help.md#10a15-serve-a-customer)
      - [10A.16 My tasks](user-guide-store-app-and-help.md#10a16-my-tasks)
      - [10A.17 Inspect](user-guide-store-app-and-help.md#10a17-inspect)
   - [11. Your Profile, Password, and Session](user-guide-store-app-and-help.md#11-your-profile-password-and-session)
      - [11.1 Which version you are on](user-guide-store-app-and-help.md#111-which-version-you-are-on)
      - [11.2 Help on any screen](user-guide-store-app-and-help.md#112-help-on-any-screen)
   - [12. If You Get Logged Out or See an Error](user-guide-store-app-and-help.md#12-if-you-get-logged-out-or-see-an-error)
      - [12.1 How to read an error message](user-guide-store-app-and-help.md#121-how-to-read-an-error-message)
      - [12.2 The errors people hit most](user-guide-store-app-and-help.md#122-the-errors-people-hit-most)
      - [12.3 When a screen shows something that can't be right — Refresh vs Reset](user-guide-store-app-and-help.md#123-when-a-screen-shows-something-that-cant-be-right--refresh-vs-reset)
   - [13. Open a Shop and Make Your First Sale — the full worked example](user-guide-store-app-and-help.md#13-open-a-shop-and-make-your-first-sale--the-full-worked-example)
      - [Step 1 — Create the place you trade from](user-guide-store-app-and-help.md#step-1--create-the-place-you-trade-from)
      - [Step 2 — Create the supplier you buy from](user-guide-store-app-and-help.md#step-2--create-the-supplier-you-buy-from)
      - [Step 3 — Create something to sell](user-guide-store-app-and-help.md#step-3--create-something-to-sell)
      - [Step 4 — Order some stock](user-guide-store-app-and-help.md#step-4--order-some-stock)
      - [Step 5 — Approve it (as the *other* user)](user-guide-store-app-and-help.md#step-5--approve-it-as-the-other-user)
      - [Step 6 — Receive the stock](user-guide-store-app-and-help.md#step-6--receive-the-stock)
      - [Step 7 — Open the till](user-guide-store-app-and-help.md#step-7--open-the-till)
      - [Step 8 — Sell something](user-guide-store-app-and-help.md#step-8--sell-something)
      - [Step 9 — Check that everything moved on its own](user-guide-store-app-and-help.md#step-9--check-that-everything-moved-on-its-own)
      - [Step 10 — Close the till](user-guide-store-app-and-help.md#step-10--close-the-till)
   - [14. Glossary](user-guide-store-app-and-help.md#14-glossary)
   - [15. Run an import through staging](user-guide-store-app-and-help.md#15-run-an-import-through-staging)
