---
doc_id: DOC-PRODUCT-POS-B2B-OFFERS-PLAN
title: POS B2B bills, discounts, offers and online bills plan
type: reference
status: in-review
owner: product-owner
approvers: [product-owner]
audience: [product, engineering]
applies_to: docs/micro_checklist.md items 215.1-215.19
authority: proposed-design
confidentiality: internal
last_verified: 2026-11-15
review_by: 2027-02-13
supersedes: none
superseded_by: none
---

# POS B2B bills, discounts, offers and online bills: plan (ask 215, 2026-11-15)

The owner asked for a B2B bill on the POS till, the Indian GST and income-tax rules applied in full,
three separate discounts, a complete offer module with bulk upload, clean phone numbers from the
OMS, online orders and returns converted to POS bills, a find-anything bill search and a Sales
History page. The exact words are in the tracker's Asks log (ask 215).

## What already exists

| Area | Built before this ask |
|---|---|
| Buyer tax ids | GSTIN (with check digit) and PAN on the Customer; PAN filled from the GSTIN (85.C1/85.C2) |
| B2B print | A till bill to a Customer with a GSTIN prints as a B2B tax invoice; GSTR-1 files it under B2B (85.D2) |
| High-value rules | Address from ₹50,000 to an unregistered buyer, PAN above ₹2,00,000 or Form 97, cash of ₹2,00,000+ refused (85.C4, `HighValuePolicy`) |
| Offers | Percentage off, flat off, Buy X Get Y free (cheapest free), any N for ₹P, gift with purchase, bonus points; over a bill, item, category, product group or SKU list; coupons, customer tier, tag, stores, dates, priority, stacking (`engines/pos_offers.go`) |
| Discounts | Item: the approval-gated price override. Bill: the manual % or ₹ discount. Both exist, but the bill discount is added into the offer share, so reports cannot tell them apart |
| Online bills | OMS bill at pack (its number is the GST invoice number), Sales Invoice at manifest handover (88.3.2/88.3.3) |
| Returns at the till | Find by bill number, mobile or date (85.E4) |

## Decisions

Taken 2026-11-15. The ask-timeout took the recommended option for D1, D2, D5 and D6 after five minutes with no answer; the owner can still change them.

| # | Decision | Answer |
|---|---|---|
| D1 | Required B2B fields | GSTIN, legal name, address line + city + PIN (state from the GSTIN), buyer PO number + PO date. Ship-to only when the goods are shipped. Optional: trade name, email, contact. A setting makes the PO optional |
| D2 | PAN threshold | The law: name, address and state from ₹50,000 (unregistered buyer); PAN above ₹2,00,000 or Form 97. The Settings value can lower PAN to ₹50,000 as a house rule |
| D3 | Cash limit | ₹2,00,000+ in cash refused per bill and across the same customer's cash bills that day (s.269ST covers "one event or occasion") |
| D4 | Place of supply at the till | Store's state when the buyer collects at the counter (IGST Act s.10(1)(a)); the buyer's GSTIN state when shipped on the buyer's order (s.10(1)(b), bill-to-ship-to), with the ship-to address printed. IGST when that differs from the store's state |
| D5 | Online order to POS bill | At manifest handover, a POS bill in the fulfilling store with the OMS bill's number, lines and tax, in place of the Sales Invoice. Stock already left at pack, so the POS bill does not move it again. Per-client setting, off until switched on |
| D6 | Discount order | Item (cashier's line cut) → offers → bill (cashier's whole-bill cut), as today. Each stored and reported apart |
| D7 | Clean phone | One normaliser for all (`NormalizePhone`): India as 10 digits, abroad as + country code. The order keeps the phone as received too |

## Order of work

| Phase | Items | Why first |
|---|---|---|
| 1 Till: B2B, name and mobile, high-value | 215.1-215.4, 215.18 | Daily use at every store; B2CL is a filing error today |
| 2 Three discounts | 215.5, 215.6, 215.11 | Every report after this reads the split |
| 3 Offers | 215.7-215.10 | Builds on the split |
| 4 Find and history | 215.12-215.14 | Needed before online bills land in POS |
| 5 Online bills | 215.15-215.17 | Biggest change to an approved flow; last, behind a setting |
| 6 IRN for till B2B | 215.19 | Waits on GSP credentials (26.2.3) for a live check |

## Phase 1 - Till: B2B bill, name and mobile, high-value rules

- **215.1** Mobile and Name are two boxes. Mobile looks up; Name fills from the customer or names a new one.
- **215.2** A GSTIN box. A valid GSTIN turns the bill B2B and opens the buyer panel (D1). A known GSTIN fills it from the Customer; a new one creates the Customer at Complete Sale.
- **215.3** The server checks D1 at checkout and sets place of supply and IGST (D4). The tax invoice prints PO number and date, ship-to, place of supply and reverse charge No.
- **215.4** The till shows which rule applies as the total grows, and the cash check counts the day (D2, D3).
- **215.18** GSTR-1 B2CL limit: ₹1 lakh from August 2024, ₹2.5 lakh before.

Done when: a B2B bill can't be completed without D1's fields; an out-of-state ship-to charges IGST and files under the buyer's state; tests cover each rule.

## Phase 2 - Three discounts

- **215.5** Each line stores `item_discount`, `offer_discount` and `bill_discount`; the bill stores their totals. The bill discount no longer hides inside the offer share. GST is still on the net line value.
- **215.6** The line discount dialog takes a %, an amount off or a new price; the approval slab and reason stay.
- **215.11** A discount report by bill, item and offer, with the three in their own columns.

Done when: one bill with all three discounts shows each one separately on the bill, in the stored sale and in the report, and they add up to the bill's total discount.

## Phase 3 - Offers

**215.7** types (all on any scope: bill, item, category, product group, SKU list):

| Type | Example | Status |
|---|---|---|
| Percentage off | 10% off on a list of designs | built |
| Flat off | ₹200 off the bill above ₹2,000 | built |
| Buy X Get Y free | Buy 1 get 1 on a list (cheapest free) | built |
| Buy X Get Y at % off | Buy 2, the 3rd at 50% | new |
| Buy from A, get from B | Buy any shirt, get a tie free | new |
| Any N for ₹P | Any 3 for ₹999 | built |
| Combo | One shirt + one trouser for ₹1,999 | new |
| Fixed price | Every item in the list at ₹499 | new |
| Spend slabs | ₹2,000+: 10%, ₹5,000+: 15% | new |
| Quantity slabs | 2 pcs: 10%, 3+ pcs: 20% | new |
| Gift with purchase | Free bag above ₹3,000 | built |
| Bonus points | Double points this weekend | built |

- **215.8** Conditions: days of the week, hours, an exclude list, uses per customer and in total.
- **215.9** The Offer screen shows only the chosen type's fields.
- **215.10** Bulk upload: an offers CSV and an offer-items CSV (offer code, list Buy/Get/Combo-A/Combo-B/Exclude, SKU), preview then apply, errors by row.

Done when: every type has a test with a worked bill, and a CSV of each type uploads and applies at the till.

## Phase 4 - Find any bill, Sales History

- **215.13** One search across store and online bills: bill number, online order number, marketplace order id, AWB, OMS bill number, mobile, GSTIN.
- **215.12** POS › Sales History: sales and returns, newest first, search, store and channel filters; a row opens the bill (with its three discounts), reprints it or starts a return.
- **215.14** Return an online bill at the till with no return from the channel.

## Phase 5 - Online bills

- **215.15** The order keeps `phone_raw` and the cleaned `phone`.
- **215.16** D5, behind a per-client setting.
- **215.17** A channel return becomes a POS return against that bill.

Done when: an order goes pack → handover → one POS bill (no Sales Invoice) and a return of it, from the channel or from the till, gives one credit note; stock moved once each way.

## Phase 6 - IRN for B2B till bills

- **215.19** A B2B till bill joins the IRN queue when the client is above the e-invoice limit (₹5 crore). The live check waits on the GSP credentials (26.2.3).

## Not adopted

| Idea | Why not |
|---|---|
| Bank or card offers in the ERP | The card terminal (Pine Labs) runs bank offers and EMI itself; doing it twice risks a double discount |
| GSTIN lookup from the GST portal | Needs a GSP account; the check digit and the state code are checked offline. Revisit with 26.2.3 |
| PAN from ₹50,000 by default | Not the law (D2); kept as a house-rule setting |

## Research sources

- CGST Rule 46, tax invoice particulars: [CBIC](https://taxinformation.cbic.gov.in/content/html/tax_repository/gst/rules/cgst_rules/active/chapter6/rule46_v1.00.html), [Tally](https://tallysolutions.com/gst/gst-invoice-format-mandatory-fields-rules-and-examples-for-businesses/)
- B2CL at ₹1 lakh from 1 Aug 2024 (Notification 12/2024-CT): [Tally help](https://help.tallysolutions.com/gstr-1/b2c-large-invoices-5a-5b), [Taxscan](https://www.taxscan.in/gstn-issues-advisory-on-reporting-supplies-to-unregistered-dealers-in-gstr-1-gstr-5/433979)
- E-invoice from ₹5 crore, 6-digit HSN above ₹5 crore: [Tally](https://tallysolutions.com/accounting/e-invoicing-rules-in-india/), [GimBooks](https://www.gimbooks.com/blog/5-crore-e-invoice-turnover-rule-2026)
- Promotion types in other systems: [Ginesys buy-get](https://www.ginesys.in/blog/buyget-promotion-now-available-memo-level-promotion), [Oracle Retail offers](https://docs.oracle.com/en/industries/retail/retail-customer-engagement/20.4/ceugg/creating-offers.htm), [Vori offer types](https://help.vori.com/promotions/defining-offer-types)
- B2B order fields (PO number, company location): [Shopify B2B draft orders](https://help.shopify.com/en/manual/b2b/draft-orders)

## Guardrails

- Server decides: B2B fields, offers and discount splits are worked out on the server; the till only shows them.
- Additive only: new JSON fields on the cart and new Offer fields through a new migration; old bills read as before.
- No change to a client's data at release; online-to-POS billing stays off until a client switches it on.
- Guides updated with each phase (constitution Article 2).
