---
doc_id: DOC-PRODUCT-RESEARCH-REC-LOYALTY-D365
title: Recording blueprint - Dynamics 365 Commerce loyalty, gap-checked
type: reference
status: draft
owner: product-owner
approvers: [product-owner]
audience: [product, engineering]
applies_to: docs/micro_checklist.md Stage 146
authority: source
confidentiality: internal
last_verified: 2026-10-31
review_by: 2026-12-31
supersedes: none
superseded_by: none
---

# Loyalty: Microsoft Dynamics 365 Commerce blueprint and gap check

> Stage 146 (2026-10-31): learned from the user's meeting recordings (`Downloads\Telegram Desktop\Dynamics Loyalty Management Demo 1`). Citations `(key mm:ss)` point into those videos; the machine transcripts and screen OCR were working files and are not kept. Gap rows were checked against the working tree on 2026-10-31. The plan built from these files is `docs/product/recordings-gap-plan-2026-10-31.md`.

## 1. Sources

- **loy1**, "Dynamics Loyalty Management Demo 1" (54 min, 26-10-2023). Presenter: Muthu (CodeValue Technologies, the D365 partner), on the "cvtuat" sandbox, legal entity **CRET**. Screens: programme "CRET IND Rewards", scheme **CRET_LS** ("CRET Loyalty Schemes"), and the Store Commerce POS. The demo data is grocery (Fresh Food, Dry Food, Beverages) and US Contoso/Fabrikam stores, not jewellery. Kushal's attendees: Harish Babu, Monali, Vishal Nair, Vishal. The second half (20-43 min) covers DOM, Unicommerce and inventory sync, not loyalty.
- **loy2**, "Dynamics Loyalty Management Demo 2" (55 min, 31-10-2023). Presenter: Kaustav Chakraborty (PwC), on the "pwcecommerce" sandbox, company **inmf**, with apparel data (Lee, Wrangler, Manyavar, AceTurtle stores). The recording starts part-way through the meeting (the Teams clock reads 26:03 at 00:00). From 38 min, Sumit Mishra (PwC, CRM lead) pitches D365 Marketing and Customer Insights (CDP).
- Product: D365 Finance & Operations, **Retail and Commerce > Customers > Loyalty**, plus Store Commerce POS. Client context: Kushal's Fashion Jewellery has 78 stores, sells online through Shopify and marketplaces via Unicommerce, and bills at the stores with GoFrugal. **Today it gives points to offline customers only** (loy2 34:22).

## 2. Blueprint

### 2.1 Menu and object model
- Menu (loy1 20:15, 43:15, 46:00 frames): Retail and Commerce > Customers > Loyalty > **Loyalty cards, Loyalty reward points, Loyalty programs, Loyalty schemes, Date intervals, Resolve conflict cards, Affiliations, Activity types / Other activity type**.
- Hierarchy: **Programme** (holds tiers) → **Scheme** (belongs to one programme; holds the earning rules, redemption rules, retail channels and excluded affiliations; has its own dates) → **Reward point** IDs (the "currency" a rule earns or burns, for example CERT, CERT REDE, "Total spent") → **Loyalty card** (belongs to a customer; enrolled in one or more programmes).
- Generic D365. Nothing in it is specific to jewellery.

### 2.2 Loyalty programmes and tiers
- Screen *Loyalty programs* (frames loy1 06:00, 43:30). Fields: Name, Description, **Combine related loyalty cards** (toggle). Toolbar: Translations, Price groups, Card transactions.
- **Program tiers** grid: Level (1.00/2.00/3.00), Tier (Silver/Gold/Platinum), Description, **Date interval** (PrevYear / NextYear). The date interval is how long a tier, once reached, stays valid. Tiers have their own *Price groups* and *Translations*.
- **Tier rules** grid: Reward point (for example CERT, or a "Total spent" point), **Minimum increase** (500.0000), **Evaluation date interval** (PrevYear, PrevMonth).
- Rule examples. 500 points in the previous year gives Silver, 1,000 gives Gold, 1,500 gives Platinum (loy1 06:10-06:32). Another programme used Total spent ≥ 500 over the previous month, with each tier valid for the next year (frame loy1 43:30).
- **Tier evaluation is a batch job.** It is scheduled (for example every 15 days or monthly), finds members who crossed a threshold and moves them to the next tier (loy1 43:28-43:55). The card also has a manual **Update card tiers** button (frame loy1 45:30).
- Tier history per card: Loyalty program | Program tier | Start date | End date | **Active tiers** check. Example: Silver from 10/26/2022 to Never, Platinum 10/26/2022-6/2/2024, Gold 6/2/2023-6/2/2024 (frame loy1 45:30; loy2 09:12-10:09: "enrolled 1 Jan, 500 points by 1 Feb, upgraded to Gold").
- **Several programmes can run side by side.** A different programme, with its own tiers and rules, can serve new stores. A customer can hold several cards across programmes and accumulate on each (loy2 06:25-07:41).

### 2.3 Loyalty schemes: general
- Screen *Loyalty schemes* (frames loy1 06:45, 10:00). Header fields: **Scheme ID** (CRET_LS), **Description**, **Loyalty program**. Below it are four FastTabs: **Earning rules**, **Redemption rules**, **Retail channels**, **Excluded affiliations**.
- **Many schemes can run at once**, each with its own start and end dates (loy1 06:42-06:51). Dates switch rules on and off. Example: end the old Silver scheme on 30 Nov and start a new one on 1 Dec, so no points are earned during the November sale (loy2 00:00-00:46).

### 2.4 Earning rules
- Grid columns (frames loy1 07:00, 10:00, 17:30; loy2 00:45): **Program tier**, **Affiliation**, **Activity type**, **Category**, **Product**, **Product variant**, **Activity amount/quantity**, **Activity currency**, **Reward point ID**, **Reward point type** (Amount), **Reward points**, **Start date**, **End date**.
- Activity types (loy1 07:24-07:57): **Purchase products by amount**, **Purchase products by quantity**, **Sales transaction count**, plus user-defined **Other activity** types (09:05-09:43).
- Scope comes from the product category hierarchy: all products, a category, a product or a variant (loy1 08:00-08:30; loy2 01:30 "Product variant").
- Rates are expressed as "N reward points per X currency". Examples: 1 point per ₹1 base, 2 points per ₹1 on Fresh Food (loy1 08:34-09:04); 1 point per ₹100 (loy2 00:45).
- **Base and category rules stack.** "Anything a customer buys gets 1 point, and fresh food gets 2" (loy1 08:47-09:04).
- **Tier-specific rates.** Silver earns 1 point per ₹100 and Gold 2. Within one tier, category A can earn 2 and category B 3 (loy2 01:06-02:40). D365 warns that tier-based earning lines may not be supported when "Commerce Data Exchange backward compatibility" is on (frame loy2 01:15).
- **Date-bound promotional earning.** For Diwali, 1-15 Nov, a slow-moving product earns 100 points per unit bought, set up with "purchase by quantity" on that product. The rule is set once and the system does the rest (loy1 16:49-18:31).
- **Channel-specific earning.** Physical store 627 earns 10 points per ₹100 and the online store earns 20, set up as two schemes with different retail channels (loy1 11:02-12:10).
- **Other activities.** "Submit product review" earns 100 points (D365 Commerce has product reviews built in). The activity list is user-defined: referral, birthday and so on, and the system adds the points automatically (loy1 09:05-09:43, 46:06-46:32). Other activity type master: ID, Name, Description, **Ignore vesting** (frame loy1 46:15).
- Validation messages seen: "Field 'Reward points' must be filled in" (loy1 10:45); "The earn rewards line is missing the activity amount currency" (loy2 08:00).

### 2.5 Redemption rules
- Grid columns (frames loy1 18:45, 20:00; loy2 03:00-04:45): **Program tier**, **Affiliation**, **Reward point ID** (CERT REDE), **Reward point type** (Amount), **Reward points**, **Redemption type**, **Category**, **Product**, **Product variant**, **Amount or quantity**, **Currency**, **Start date**, **End date**.
- Two redemption types:
  - **Payment by amount.** Points pay as a **tender**: the loyalty card is a payment method. Example: 1 point = ₹5 on one category for 1-15 Nov (loy1 18:33-19:13).
  - **Loyalty points discount.** Points become a **discount line** on the sale. Example: 10 points = ₹1 discount on ALL categories (loy1 16:10-16:48; frame 16:30).
- **Rule: a scheme cannot have a discount line and a tender line with overlapping effective periods.** The error reads "You cannot create a discount line and a tender line that have an overlapping effective period. Change the effective..." (frame loy1 20:00). So at any date a scheme redeems one way only.
- The rate is free-form: 1 point = 25 paise, or 100 points = ₹10 (loy2 02:51-03:51).
- **Redemption can be limited to categories, products or variants**, for example to keep newly launched products out of points redemption (loy2 04:05-04:33). Redemption can also be limited by tier, channel and date.
- The presenter summed up the two uses of loyalty (loy1 19:13-19:49): (1) members get special prices and discounts, through price groups or affiliations; (2) members pay with points, the card working as a tender.

### 2.6 Retail channels and excluded affiliations (scheme scope)
- *Retail channels* grid: Name, Operating unit number. Lines are added by picking organisation hierarchy nodes ("Retail Stores by Business Unit / Region / Type") (frames loy1 12:15-15:30; loy2 05:15).
- Rules on scope:
  - Only the channels listed earn or redeem under the scheme.
  - Run the programme in 18 of 20 stores. A new store is added to the scheme and starts at once, including any campaign already running (loy2 04:42-06:25).
  - A subset of the 78 stores can have its own scheme (loy1 14:59-16:02).
  - An online store (Shopify, the website) is a channel too. If it is left off the scheme, online buyers earn nothing (loy1 29:07-30:07; loy2 37:00-37:30).
- **Points belong to the channel where the order started, not the store that fulfilled it.** An online order fulfilled by a physical store still earns, or does not earn, under the online channel's scheme (loy1 26:44-28:02; loy2 35:07-35:54). This answers Kushal's question about omni-channel orders billed at store POS.
- *Excluded affiliations*: customer affiliations (groups) whose members never earn under the scheme (frame loy1 15:00).
- Store hierarchies (Class A/B/C stores or marketplaces) let schemes, discounts and catalogues be set by level (loy1 01:25-02:06).

### 2.7 Loyalty cards
- Screen *Loyalty cards* (frames loy1 44:45-45:30; loy2 08:30-11:15). Toolbar: **Card adjustments, Replace card, Transfer points, Card transactions**.
- Card fields: **Card number** (for example 5500106), **Card type** ("As card tender" on every card seen), **Customer name** (can be blank: unassigned cards exist, for example 5500105), **Replacement card number**, **Enrollment source** (the store, for example "Fashion Store"), **Effective** date, **Blocked date**. Sections: Loyalty programs (with tier history, 2.2), **Reward point summary** (available, expired and used points, loy2 10:21-10:39).
- **Card numbers** are generated from a number sequence ("any number of letters you define", loy2 09:07) or imported from an external system (loy1 45:44-45:54).
- **Enrolment at POS:** select the customer and issue a loyalty card. Alternatively, configure every registered customer to get a card automatically (loy2 08:34-09:07; loy1 04:37-04:44).
- **Replace a lost card** (loy1 44:54-45:01). **Activate or deactivate** a card; a deactivated card cannot be used (loy1 45:20-45:33).
- **Transfer points** between cards, so friends or family can pool points for a redemption. Dialog: Source loyalty card number, Destination loyalty card number, Points available to transfer (loy1 45:01-45:17; frame 45:00).
- A card works across the organisation, in every channel and store; one number is enough (loy1 45:33-45:42).
- *Card transactions*: ID, Item number, Company, Name (channel), Register number, Operator ID, **Entry type** (Earn, Adjust seen), Loyalty program, Tier, Customer account (frames loy1 44:00; loy2 10:45). Points are earned **per sales line** (item rows 9999, FG001) and per programme. Each row links back to the retail transaction, which carries a LOYALTY > Card number field (loy1 44:02-44:40; frame 44:30).
- Reports: card transactions, a loyalty report of all card details (loy2 11:03-11:14), and Power BI through BYOD or Azure Data Lake (loy1 44:40-44:48, 49:06-49:29).

### 2.8 Points lifecycle
- **Vesting.** Points can be held for the return window. With a 7-day return policy, points show on the card but cannot be redeemed for 7 days. The alternative is to reverse points on return; both options exist (loy2 18:40-19:44). Other activity types can **ignore vesting** (2.4).
- **Expiry.** The POS shows "Expiring within 30 days" (frame loy2 14:15). The card summary shows expired points (loy2 10:27).
- **Adjustments.** *Card adjustments* button; card transactions show an "Adjust" entry type (frame loy1 44:00).
- **Points are decimal.** The POS showed 50.38 points (frame loy2 14:15).

### 2.9 POS experience
- Customer search, then add the customer to the transaction (loy2 13:40-14:10, 19:44-20:21).
- **Loyalty details panel** (frame loy2 14:15): Total available balance (Points 50.38), then per reward point **Available as at <date>** and **Expiring within 30 days**, and an **Add to sale** button.
- Customer card (loy1 46:46-48:13; loy2 15:25-16:13): customer since, number of loyalty cards ("1 card, Card tender 5500106, Enrolment date"), recent purchases, order history, total spent, wish lists, recommended products, **affiliations** (government staff, Kushal's staff, students, retired officers), customer attributes.
- New customer at POS (frames loy1 03:45, 04:45): customer type (Person or Organisation), first and last name, primary email, receipt email, phone, **Email opt-in** and **Phone marketing opt-in**.
- **Cross-store data.** The customer and transactions live centrally in F&O. A purchase at Jayanagar can be returned at Indiranagar once the upload job has run, which takes 3-4 minutes and can be set as low as 1 minute (loy2 16:43-18:33). An online purchase can be returned in a store.

### 2.10 Integration, rules raised by the client, and customisations
- **APIs for earn, redeem and customer balance**, so a third-party POS or e-commerce site can use D365 loyalty (loy2 12:22-13:30).
- **Who earns on an online order:** the registered customer who placed it (logged in by phone or email), whatever the billing or shipping phone (loy2 29:04-30:07).
- **Customers without a phone** (email only) can be allowed or blocked. The vendor recommended making the phone mandatory, so redemption can use an SMS OTP; an email OTP is also possible (loy2 31:35-33:16).
- Stated as **not standard, a small customisation**:
  - Phone validation: 10 digits, digits only, no "911" prefix, no 1234-style numbers (loy2 26:33-28:54).
  - Limiting points by pin code. Refusing a card by country (loy2 20:30-24:00).
  - Sending SMS or WhatsApp. Email goes through Power Automate, for birthday or postal-code campaigns (loy1 50:07-53:06; loy2 24:32-25:09).
- **Customer Insights / Dynamics Marketing:** a CDP with 1,000+ connectors, AI segments, journeys and abandoned-cart follow-up. It is a separate licence (loy1 48:23-49:29; loy2 38:00-53:30). It was pitched only, not shown in use.

## 3. Gap table

| # | Capability | Vendor reference | Our ERP | Evidence | Note |
|---|---|---|---|---|---|
| 1 | Named loyalty programme(s); several side by side | loy1 05:36; loy2 06:25 | Missing | grep `loyalty_program`, `LoyaltyProgram`, `loyalty_scheme` in engines/internal/db/public: 0 hits; all rules are tenant-wide settings (`engines/settings_definitions.go:20-66`) | One programme per client by construction |
| 2 | Tiers (levels, names) | loy1 06:00 frame | Have | `engines/loyalty_tiering.go:22-31`; Loyalty screen Tiers tab `public/view-loyalty.js:77-79`; tracker 26.7.3, 73.3.1 [x] | Tier also has benefits text and grace days |
| 3 | Tier rule: threshold over an evaluation period | loy1 06:00, 43:30 frames | Have | Spend net of offers and refunds over `loyalty.tier_window_days` (`loyalty_tiering.go:239-252`; setting `settings_definitions.go:38-43`) | Ours is spend-based, not points-based. Spend is what a jeweller wants |
| 4 | Tier validity period after qualifying (NextYear) | loy1 43:30 frame | Partial | Grace days keep a tier after spend drops (`loyalty_tiering.go:204-217`) | No fixed "kept until the end of next year" |
| 5 | Scheduled tier evaluation job, plus a manual "Update card tiers" | loy1 43:28-43:55; frame 45:30 | Partial | The only caller of `RecomputeLoyaltyTier` is the earn path (`engines/loyalty.go:276-279`); no daily job (`registerCRMDaily` has only journeys, referrals, retention) | **Finding:** a lapsed customer is never downgraded until they buy again. Their comeback bill earns at the stale tier's multiplier (`loyalty.go:251` runs before the recompute at 276), and tier-only offers apply to it (`pos_offers.go:177,210`). ADMIN_GUIDE.md:248 implies ageing spend lowers the tier |
| 6 | Tier history per member (start/end of each tier) | loy1 45:30; loy2 09:12 | Partial | Only the current `loyalty_tier` and `loyalty_tier_below_since` (`loyalty_tiering.go:195-226`); no tier-change event (grep `tier` in `crm_timeline.go`, `crm_events.go`: none) | Visible only in the generic record audit |
| 7 | Several schemes with start/end dates per programme | loy1 06:42; loy2 00:00 | Missing | Earn and burn rates are single settings (`settings_definitions.go:26-31, 345-350`); no dated rule rows | Rate changes apply instantly and cannot be scheduled |
| 8 | Earn by amount (points per currency) | loy1 08:34; loy2 00:45 | Have (coarse) | `loyalty.rupees_per_point`, integer ≥1 (`loyalty.go:243-247`) | Floor per bill. Cannot express "2 points per ₹1" except through a tier multiplier |
| 9 | Earn by quantity (fixed points per unit) | loy1 16:49-18:31 | Missing | `EarnLoyaltyPoints(tenantID, customerID, netSaleAmount, ref)` takes only an amount (`loyalty.go:237`) | Needs line-level earning |
| 10 | Earn by transaction count (points per bill) | loy1 07:33 | Missing | Same signature as #9 | |
| 11 | Earn rules scoped by category, product or variant (bonus on top of base) | loy1 08:00-09:04; loy2 02:15 | Missing | Same as #9; grep `earn_rule`/`EarnRule`: 0 hits | The biggest functional gap |
| 12 | Tier-specific earn rate | loy2 01:06-02:10 | Have | `earn_multiplier` per tier (`loyalty_tiering.go:135-150`, applied `loyalty.go:251`) | Tier × category combinations missing (#11) |
| 13 | Date-bound promotional earning (festival bonus) | loy1 16:49-18:31 | Missing | No dated earn rules (#7, #11) | |
| 14 | Earning and redemption scoped to stores/channels; a new store joins at once | loy1 11:02-16:02; loy2 04:42-06:25 | Missing | Earn has no location (`loyalty.go:237`); the ledger has no store column (`db/migration.sql:860-868`, later ALTERs add only expires_at, pending_until, cancelled_at) | Every store earns at the same rate |
| 15 | Online channel orders earn by originating channel | loy1 26:44-28:02; loy2 35:07 | Missing (matches today's policy) | `EarnLoyaltyPoints` is called only from `pos_checkout.go:535`; `channel_orders.go` and `oms_bill.go` have no loyalty code (OMS bills are Sales Invoices, `oms_bill.go:1-12`) | Kushal's gives no points online today (loy2 34:22), so ours already matches. Omni-channel orders cannot earn by accident |
| 16 | Excluded affiliations (for example staff do not earn) | frame loy1 15:00 | Missing | No affiliation concept (grep `affiliation`: 0). Customer tags exist for segments only (`crm_segments.go` "tags") | |
| 17 | Other-activity earning (review, birthday, referral) | loy1 09:05, 46:06 | Partial | Referral reward points: Have (`crm_loyalty_depth.go:373-415`, tracker 101.11.3 [x]). Birthday and anniversary journeys send messages only (`crm_journeys_ready.go:53-57`, journey steps wait/check/send/task/end, `crm_journeys.go:232-267`). Manual credit with reason: `loyalty_admin.go:109` | No automatic birthday or anniversary points |
| 18 | Redemption as tender (payment by amount) | loy1 18:33, 19:13 | Have | `TenderLoyaltyPoints` (`sale_allocation.go:28`). Sale posts full revenue Cr 4100 and points Dr 5250 (`finance.go:604-607`). Burn inside the sale's transaction (`pos_checkout.go:308-350`) | GST is on the full value, as for any tender |
| 19 | Redemption as "loyalty points discount" (reduces price and taxable value) | loy1 16:10-16:48; frame 16:30 | Missing | Only the tender path exists (#18) | Needs a tax decision (section 4) |
| 20 | One redemption mode per scheme per period (no overlapping discount and tender lines) | frame loy1 20:00 | N/A today | Only one mode exists | Becomes a validation rule if #19 is built |
| 21 | Free-form redemption rate (10 pts = ₹1; 1 pt = 25 paise) | loy1 16:42; loy2 03:15 | Partial | `loyalty.redemption_value_per_point` is an integer ≥ ₹1 (`settings_definitions.go:345-350`) | Values below ₹1 per point cannot be set |
| 22 | Redemption limited to categories/products/variants (exclude new launches) | loy2 04:05-04:33 | Missing | `quoteLoyalty` caps on the whole bill total (`pos_quote.go:628-661`) | |
| 23 | Redemption rules by tier, channel or date | loy1 18:59-19:22; loy2 03:00 | Missing | One rate for everyone (`loyalty.go:20-22`) | |
| 24 | Redemption security: OTP, approval, limits | loy2 31:35-33:16 (OTP suggested) | Have (exceeds the demo) | Daily cap, OTP and manager approval at one choke point (`loyalty_redemption_security.go:108-157`); tracker 26.7.5, 73.1.4 [x] | |
| 25 | Enrolment: issue a card at POS or auto-enrol | loy2 08:34; loy1 04:37 | Have (implicit) | Every identified customer earns; walk-in excluded (`loyalty.go:237-242`); phone-first customer (85 D1) | No explicit opt-in or terms acceptance |
| 26 | Card numbers (sequence, import), card types | loy1 44:45, 45:44; loy2 09:07 | Missing (by design) | grep `loyalty_card`, `LoyaltyCard`: 0. `card_number` hits are gift cards and stored value only | Phone is the member ID (CRM plan D5) |
| 27 | Block or deactivate a member (blocked date) | loy1 45:20; frame 45:30 | Missing | No block check in earn (`loyalty.go:237-242`) or burn (`loyalty.go:82-102`); grep block/suspend in loyalty files: none | |
| 28 | Replace lost card | loy1 44:54 | N/A | Phone-keyed. Changing the phone is the equivalent | |
| 29 | Transfer points between members | loy1 45:01-45:17; frame 45:00 | Missing | grep `transfer_points`, `TransferPoints`, `points_transfer`: 0. Trackers: no item | |
| 30 | Combine duplicate members' points ("resolve conflict cards") | menu, loy1 20:15 | Have | `MergeCustomers` reassigns the ledger (`crm_analytics.go:59, 100`) | |
| 31 | One membership valid in every store and channel; real-time balance | loy1 45:33; loy2 16:43 | Have | Central ledger, `GetLoyaltyBalance` (`loyalty.go:123-135`) | Real time, not a 3-4 minute sync |
| 32 | Multiple cards and programmes per customer, accumulated separately | loy2 06:25-07:41 | Missing | #1 | Not recommended (section 5) |
| 33 | Point expiry | loy2 10:27, frame 14:15 | Have | `loyalty.point_expiry_days`; hourly sweep (`loyalty_tiering.go:287-369`, `routes.go:162`) | Expiry is an approximation, not FIFO by lot |
| 34 | Vesting: points held for the return window | loy2 18:40-19:44 | Have | `loyalty.pending_points` (`crm_loyalty_depth.go:34-49`); tracker 101.11.1 [x] | |
| 35 | Points reversed on return | loy2 18:40 | Have | `loyalty_returns.go:15-22` (D01-D03); tracker 73.1.2 [x] | Pro-rata. Points paid with come back as points |
| 36 | Manual point adjustment | frame loy1 44:00 ("Adjust") | Have | `loyalty_admin.go:109-160`, approval-gated with reason codes; tracker 73.2.11 [x] | |
| 37 | Decimal points | frame loy2 14:15 (50.38) | Missing | `points INT` (`db/migration.sql:864`) | Low value |
| 38 | POS balance and points value | frame loy2 14:15 | Have | `TillCustomerContext` (`pos_customer_context.go:43-60`) | |
| 39 | POS "expiring within 30 days" and pending points | frame loy2 14:15 | Partial | Not in the till context (`pos_customer_context.go:43-60`). Pending shows on the CRM customer page (`crm_customer.go:28-29`). A 7-days-before expiry journey exists (`crm_journeys_ready.go:53-57`) | |
| 40 | POS customer card: since, recent purchases, order history, spend | loy1 46:46; loy2 15:25 | Have | `pos_customer_context.go:53-59` (spend, recent bills, returns, vouchers, tier and next tier) | Wish lists and recommendations not relevant |
| 41 | Member transaction history with channel, register, operator, item, tier | frames loy1 44:00; loy2 10:45 | Partial | Loyalty screen history shows When/What/Points/From/Expires (`public/view-loyalty.js:67`); the catalog report has the store (`report_catalog_oms_pim_crm.go:955`) | No register or cashier, no line-level item |
| 42 | Reward point summary (available, expired, used) | loy2 10:21 | Partial | Loyalty Points Movement report (`loyalty_admin.go:261-358`) gives tenant-level earned, redeemed, expired and closing figures | Not per customer at a glance |
| 43 | Loyalty reports | loy2 11:03 | Have | `loyalty-summary` (`report_definitions.go:209`), `cat-loyalty-points-ledger` (`report_catalog_spec_generated.go:455`), movement report, dashboard dataset (`dashboard_datasets.go:254`) | |
| 44 | Member-only or tier-only discounts (price groups on tiers) | loy1 00:55-01:23 | Have | Offer `customer_tier` (`pos_offers.go:118, 210`; ADMIN_GUIDE.md:531) | "Any member" equals any identified customer only if a 0-threshold tier exists |
| 45 | Affiliations (staff, government, students) driving discounts | loy1 47:40-48:06 | Partial | Customer tags exist (segments `crm_segments.go` "tags"); an Offer cannot target a tag (`pos_offers.go:102-134`) | |
| 46 | Discounts and schemes by store hierarchy (Class A/B/C) | loy1 01:25-02:06 | Missing | The Offer has no location field (`db/migrations_stage30_7_pos_offers.sql` field list) | Pricing domain. Noted because store-scoped schemes need the same field |
| 47 | Earn, redeem and balance APIs for third-party channels | loy2 12:22-13:30 | Partial | `POST /api/v1/loyalty/redeem` and `GET /api/v1/loyalty/ledger` (balance) (`internal/server/routes.go:1053-1054`) | No earn API for external orders |
| 48 | Phone validation (10 digits, valid start) | loy2 26:33 (customisation in D365) | Have | Country table, IN 10 digits (`engines/phone.go:162`); one phone, one customer (85 D1) | |
| 49 | Marketing opt-ins captured at customer creation | frame loy1 04:45 | Have | Consent ledger per channel and purpose (101.8, `engines/crm_consent.go`) | |
| 50 | Points-earned SMS or WhatsApp after a bill | loy2 24:32 (integration in D365) | Partial | Bill-paid journeys exist (`crm_journeys_ready.go:34`); template variables have no "points earned / balance" (`crm_templates.go:30-60`) | |
| 51 | Birthday or postal-code campaigns, feedback link after a bill | loy1 50:07-53:06 | Have (city/state, not pin code) | Birthday campaign `campaign.go:13`; segments city/state/birthday month (`crm_segments.go:55-100`); feedback after bill `crm_feedback.go:1-7` | |
| 52 | Loyalty liability accounting | (not shown) | Have (beyond the demo) | `loyalty_admin.go:22-30, 54-70`; tracker 73.2.12 [x] | |
| 53 | CDP / Customer Insights / AI segments | loy2 38:00-53:30 | Not adopted | CRM plan §4 (AI, SDK funnels not adopted); RFM, CLV and cohorts exist (`crm_analytics.go`) | |

## 4. Missing and partial pieces, ranked

**1. Bonus-points rules (earn rules scoped by product, category, tier, date and store).** Size L. *Gap rows 9-13 and 17.*
- **What it is:** configurable rules that give extra points on top of the base rate: per ₹ on a category, product group or SKU list; a fixed number of points per unit; or a fixed number per bill. Each rule has a validity window, an optional tier and an optional store list.
- **Business rules:**
  - Base points always apply. Bonus rules add to them.
  - When several bonus rules match one line, the highest-priority rule wins. A rule may be marked stackable, as offers already are.
  - The earn base is the money paid on the line (net of offer and points, same as today), so returns can reverse each line pro rata (D01).
  - A rule's points follow pending and expiry like base points.
  - A rule outside its window, or for another tier or store, gives nothing.
  - The till previews "you will earn N points".
- **Why it matters for a jeweller:** festival double points (Diwali, Akshaya Tritiya), pushing slow-moving designs, launching a bridal or new collection, per-unit points on accessories. The loy1 Diwali example is exactly this.
- **How it fits:**
  - Add an **Offer type "Bonus Points"** to the existing Offer doctype and evaluator (`engines/pos_offers.go`). "Gift With Purchase" (line 41-43) is the precedent for a non-discount offer type, and the evaluator already handles scope (Bill/Item/Category/Product Group/SKU List), `customer_tier`, `valid_from`/`valid_to`, `min_bill_amount`, `min_qty`, `priority` and `stackable`.
  - Add `bonus_points_per_100`, `points_per_unit` and `points_per_bill` fields to the Offer doctype (additive `doctype_fields` migration).
  - `EarnLoyaltyPoints` takes the evaluated bonus lines and writes one ledger row per rule, `reference_doctype='Offer'`.
- **Decision:** whether bonus rules belong on the Offer screen or on a separate "Points rule" list. **Recommended: on Offer.** It is one place for promotions, and one evaluator is already trusted server-side.

**2. Store and channel scope for loyalty, and the store on every ledger row.** Size M. *Gap rows 7, 14, 46.*
- **What it is:** decide in which stores points are earned and redeemed. Allow a different base rate for a group of stores (new stores, franchise stores, airport kiosks), and have a new store pick up the client's default.
- **Business rules:**
  - A sale earns under the rule for **its own store**, and for a channel order the store or channel where the order started, never the fulfilling store (the D365 rule, and Kushal's own question).
  - The rate in force is the one valid on the bill date.
  - Every ledger row records the store.
- **How it fits:**
  - Additive `location` column on `loyalty_point_ledger` (`ADD COLUMN IF NOT EXISTS`), filled by the till earn and burn.
  - A small dated **Loyalty Scheme** master (generic doctype): valid from/to, stores (blank = all), spend per point, value per point.
  - Validation: no two schemes for the same store with overlapping dates, the same idea as D365's overlap rule.
  - Add an optional `locations` field to Offer for #1.
- **Decision:** a dated Loyalty Scheme master, or keep tenant settings plus a store exclusion list? **Recommended: the dated master.** It also gives scheduled rate changes (D365's "new scheme from 1 Dec", loy2 00:00).

**3. Scheduled tier evaluation, tier history, and a stale-tier fix.** Size S. *Gap rows 4-6.*
- **What it is:** re-evaluate every member's tier on a schedule, not only when they buy. Add a "Re-evaluate now" action on the Loyalty screen, and keep tier changes as dated history.
- **Business rules:**
  - A daily job recomputes tiers for customers who hold a tier or spent in the window. Grace days still apply.
  - On a bill, re-evaluate the tier **before** the earn multiplier and tier-only offers are applied.
  - Every change writes a CRM timeline event ("Silver → Gold on 2026-11-02"). This can also trigger a "tier upgraded" journey message.
- **Why it matters:** with `tier_window_days` set (for example 365), the admin guide promises that ageing spend lowers a tier (ADMIN_GUIDE.md:248). Today that only happens at the next purchase, and that purchase is still priced at the old tier (`loyalty.go:251` runs before 276; `pos_offers.go:177`).
- **How it fits:** `registerCRMDaily("tier evaluation", ...)` (the pattern in `crm_send_gate.go:198`), calling `RecomputeLoyaltyTier`, plus a CRM event type. No new table: tier history can come from events.
- **Decision:** none needed. This is a correctness fix to documented behaviour.

**4. Redemption eligibility by product or category.** Size S/M. *Gap row 22.*
- **What it is:** some items cannot be paid with points (new launches, gold or silver coins, gift cards, MRP-locked items).
- **Business rules:**
  - The most a bill can take in points is capped at the net value of its **eligible** lines.
  - The Loyalty Points tender is allocated only across eligible lines.
  - A return of an ineligible line never gives points back.
- **How it fits:** an item flag or a PIM Product Group "not redeemable with points" (Product Groups already resolve live in `pos_offers.go`). `quoteLoyalty` (`pos_quote.go:628`) caps by eligible lines. `sale_allocation.go` restricts the `TenderLoyaltyPoints` split.

**5. Finer redemption rate.** Size S. *Gap row 21.*
- **What it is:** let a point be worth less than ₹1, for example 10 points = ₹1 or 1 point = 25 paise.
- **Business rules:** points stay whole numbers. The value is held in paise, and redemption rounds down to whole paise.
- **How it fits:** a new setting `loyalty.redemption_paise_per_point`, read by `loyaltyPointValuePaise` (`loyalty_admin.go:46-52`), which is already the single place that turns points into paise. The old rupee setting stays as the default.
- **Decision:** whether to allow decimal points (gap row 37). **Recommended: no.** Whole points are easier at the till and in SMS.

**6. Block a member, and exclude groups (affiliations) from earning.** Size S. *Gap rows 16, 27, 45.*
- **Business rules:**
  - A blocked customer neither earns nor redeems; the balance is kept.
  - Customers carrying an excluded tag (for example "Staff") earn nothing.
  - Blocking needs a reason and is audited.
- **Why it matters:** staff purchases at staff discount, resellers buying in bulk, suspected fraud.
- **How it fits:** a `loyalty_blocked` field plus reason on Customer, and a setting "Customer tags that do not earn points". Check both at the two choke points, `EarnLoyaltyPoints` (`loyalty.go:237`) and `RedeemLoyaltyPointsTx` (`loyalty.go:82`).
- Letting Offers target a customer tag (gap row 45) is the same small change on `offerRule`.

**7. Transfer points between customers.** Size S/M. *Gap row 29.*
- **What it is:** move available points from one customer to another, for family gifting before a big jewellery purchase.
- **Business rules:**
  - Only usable points move (not pending, not negative).
  - The source customer confirms with an OTP.
  - A manager approves at or above a value, through the approval engine (a new `LoyaltyPointTransfer` doctype with approval rule slabs, like `LoyaltyPointAdjustment`).
  - The transfer writes a Burn on the source and an Earn on the target. The target keeps the source lot's expiry, so expiry cannot be extended.
  - A daily or monthly cap applies.
- **Decision:** CRM plan D5 rejected several people sharing one phone (pooling), not transfers. **Recommended: allow approved transfers, never shared balances.**

**8. Automatic points for non-sale activities (birthday, anniversary, profile completion).** Size S. *Gap row 17.*
- **Business rules:**
  - A fixed number of points, once per customer per occasion per year.
  - The points ignore pending (D365's "ignore vesting") and expire like other points.
- **How it fits:** a journey step type "give points" in `crm_journeys.go` (steps today: wait, check, send, task, end). It posts a ledger row with `reference_doctype='CRMJourney'`, keyed for idempotency. Date triggers for birthday and anniversary already exist.

**9. Till and history visibility.** Size S. *Gap rows 39, 41, 42, 50.*
- Add **expiring within 30 days** and **pending** to `TillCustomerContext`.
- Add **store and cashier** columns to the Loyalty screen history, by joining the referenced POSCart or using the new ledger `location`.
- Add a per-customer available / pending / expired / used summary.
- Add template variables `points.earned` and `points.balance`, so the bill-paid journey can send "you earned 120 points, balance 860".

**10. Redemption as a price discount instead of a tender.** Size M. **Needs a product and tax decision.** *Gap rows 19-20.*
- Today points are a tender: revenue and GST are on the full price, and the points' value is booked to 5250 or 2630.
- A discount mode would lower the line price, and with it the taxable value, on the invoice.
- If built, enforce D365's rule: one redemption mode in force per scheme at any date.
- **Recommended: keep the tender (today's behaviour) unless Kushal's tax advisor says points must reduce the taxable value.** Then add it as a per-scheme option (#2).

**11. Points on online orders, plus an earn API.** Size M. **Needs a decision.** *Gap rows 15, 47.*
- **Business rules:** points belong to the channel where the order started. An order fulfilled from a store earns under the online channel's rule. Earning waits until the order is delivered and past its return window.
- **Recommended: not now.** Kushal's gives no points online today (loy2 34:22), and the ERP already guarantees that store-fulfilled OMS orders do not earn. Build it only when the client wants online points. Then use the same earn path keyed on the channel order's channel, via the Sales Invoice made from the OMS bill.

## 5. Not worth adopting

- **Physical loyalty cards** (card numbers, number sequences, imported card ranges, card types, replace card, unassigned cards). The phone is the member ID (85 D1, CRM plan D5). A second identifier would have to be kept in step with it, for no gain at a jewellery counter.
- **Several programmes per customer with separate cards, "combine related loyalty cards", "resolve conflict cards".** A dated, store-scoped scheme (#2) covers new-store and regional needs without parallel balances. Duplicate customers are already merged (`MergeCustomers`).
- **Multiple reward-point currencies per scheme** (separate earn and redeem point IDs such as CERT and CERT REDE, ranking points). Spend-based tiering already does the job of a ranking point, and one point currency keeps the till and SMS simple.
- **Points-based tier qualification.** Spend net of refunds is the stronger signal for a jeweller and is already built.
- **Product-review points.** There are no product reviews and no shopper app or tracked website (CRM plan §4).
- **Translations of programme and tier names.** These clients run in one language; the tier name is free text.
- **Pin-code-based points or country-based card refusal.** The vendor called both non-standard and the client did not ask for them.
- **D365 Marketing / Customer Insights CDP, AI segments, Power BI through BYOD.** Already ruled out (CRM plan §4: AI and SDK funnels). Segments, RFM, CLV, cohorts and journeys exist in the ERP.
- **The 3-4 minute data-sync and upload-job tuning.** The ERP is a single central database, so balances are real time.
