---
doc_id: DOC-PRODUCT-STAGE101-CRM-PLATFORM-PLAN
title: Stage 101 plan - the CRM platform blueprint mapped onto the ERP
type: reference
status: draft
owner: product-owner
approvers: [product-owner]
audience: [product, engineering]
applies_to: docs/micro_checklist.md Stage 101
authority: proposed-design
confidentiality: internal
last_verified: 2026-10-19
review_by: 2026-11-19
supersedes: none
superseded_by: none
---

# Stage 101 plan: the CRM platform

**As of 2026-10-19. Not yet approved** - the decisions in section 1 marked *Needs decision* come first. Status is tracked as **Stage 101** in `docs/micro_checklist.md`, with the same item IDs.

**Input:** the user's *CRM Platform Business and Product Requirements* (v1.0, 19 October 2026, 70 pages; kept at `Downloads\MyBusiness\IT Solution\ERP\Docs\PDF\CRM_Platform_BRD_PRD_Master_Blueprint.pdf`). It specifies 38 modules (M01-M38) in three phases (P0 foundation, P1 engagement and retail, P2 maturity), shared rules (INV01-INV10), an 11-step send-eligibility table, state machines, a report catalog, 8 reference journeys (J01-J08), a cross-module acceptance pack and a decision register (D01-D12). It sharpens Stage 101, which was opened from the user's *"CRM Moule better/best one"* the same morning; it does not replace it.

**How the blueprint fits this ERP.** It was written for a separate CRM product run by an 8-10 person team on its own stack. Here the CRM sits inside the ERP, which already owns bills, returns, stock, loyalty and vouchers. So the CRM reads those directly instead of copying them into a "canonical commerce" layer, and every module is built in the existing Go server and Postgres, on the job runner and outbox already used for notifications. The blueprint's rules are kept; its stack and its separate-platform plumbing are not (section 4).

**What already exists** (one line each; detail in Stage 101's opening): phone-first customers with merge (85, 26.7.9); Customer 360 and RFM reports (26.7.1, 26.7.7); lifetime value and cohorts (26.7.10); loyalty ledger with tiers, expiry, reversal on returns, approved adjustments and secure redemption (26.7, 73.2.11-73.3.1); Offers, Vouchers and gift cards (30.7, 73.2.10, 72); Campaigns with birthday / lapsed triggers and a per-channel consent check (26.7.4, 55.17a); notification templates with a real delivery state through a webhook (26.12.10, 73.2.13); Service Tickets and Contracts; privacy requests with erasure for Customer (49.6.8); outbound webhooks (38.4); the CleverTap link (9.1, 26.7.11); the task inbox (73.3.3).

## 1. Decisions

D1-D4 were asked when Stage 101 was opened; D5-D13 come from the blueprint's decision register and its rules where they change what is built.

| # | Decision | Recommended | Status |
|---|---|---|---|
| D1 | Which CRM first | The retail CRM (shoppers) first; the business-buyer pipeline last. The blueprint puts sales CRM in its last phase too (M29-M30 are P2) | Needs decision |
| D2 | Real sending and which providers | Email through the store mail account already used for documents (98.3) and **one** DLT-registered SMS provider for the pilot (the blueprint's D05); WhatsApp Business Cloud API next. Plain HTTPS / SMTP, no provider library; the webhook stays for clients who already use one. **Needs the user's accounts, the DLT entity / header IDs and approved templates** | Needs decision |
| D3 | The bar for "best" | Answered by the blueprint: its research covers CleverTap, Braze, Segment, HubSpot and Customer.io and six open-source CRMs (its Part 6). The WhatsApp-first, bill-linked habits of Indian retail CRMs (Capillary, Reelo) are already in 101.4 and 101.5 | Settled (blueprint) |
| D4 | Store staff | Store staff work their own customers from the store app (102.4.1) and the clienteling worklist (101.12) | Needs decision |
| D5 | One phone, one customer | **Kept** (the user's rule, 85 D1). The blueprint allows several people on one family phone; not adopted. Duplicates are joined by a reviewed merge (101.1.3) | Settled (85 D1) |
| D6 | What consent is scoped to | Per client, per channel (SMS, WhatsApp, email) and per purpose (marketing or service). Per brand only when a client runs more than one brand | Needs decision |
| D7 | Customers with no recorded consent | Not reachable for marketing - already the rule since 55.17a; the blueprint's D04 says the same | Settled (55.17a) |
| D8 | Contact limits | Per-client settings with the blueprint's defaults: at most 2 marketing messages in 24 hours and 5 in 7 days across channels, 4 hours apart, none between 21:00 and 09:00 customer time. A client may tighten them; loosening needs an approval | Needs decision |
| D9 | How journeys are edited | A step list (trigger, wait, check, send, task, end) with yes / no branches - quicker to build and to read. A drag-and-drop canvas later if asked | Needs decision |
| D10 | Which campaign gets credit for a bill | Stores have no clicks, so: the last marketing message the customer got in the 7 days before the bill gets the credit, once per bill; a return reduces it. Real lift comes from an optional hold-out group (101.3.5) | Needs decision |
| D11 | Points on a bill that may be returned | Today points are earned at once and reversed on return. Recommended: keep that as the default; offer "pending until the return window ends" as a per-programme setting (the blueprint's D10) | Needs decision |
| D12 | How long CRM records are kept | Messages and send decisions 24 months; consent evidence for as long as the customer exists; an erased customer leaves only a hashed suppression entry so an import cannot bring them back (the blueprint's D06) | Needs decision |
| D13 | Who approves a campaign | Campaigns go through the existing approval engine. The blueprint wants a second person always (M02-R05); Stage 92 lets a client allow self-approval. Recommended: campaigns follow the client's Stage 92 setting, with a campaign-only override to force a second approver | Needs decision |

## 2. Order of work

The blueprint's first safe slice is: find a customer, record consent properly, read their bills and returns, build a simple segment, send one approved email / SMS campaign and reconcile the result. Nothing automatic (journeys, scoring, AI) goes live before that works.

| Step | Phase | Blueprint modules | Waits on |
|---|---|---|---|
| 1 | 101.0 Decisions | D01-D12 | The user |
| 2 | 101.1 One customer page and identity | M05, M06 | - |
| 3 | 101.8 Consent ledger, preferences, erasure | M07, M35 | D6, D12 |
| 4 | 101.2 Segments and lifecycle | M09, M10 | 101.1 |
| 5 | 101.9 The send gate (one place every message passes) | M14, M37 budgets, M36 alerts | 101.8, D8 |
| 6 | 101.3 Templates, senders, campaigns, results | M11, M12, M15, M16, M22 basic | 101.9, D2, D13 - **pilot gate** |
| 7 | 101.4 Automatic journeys | M13, J01-J08 | Pilot passed, D9 |
| 8 | 101.5 Feedback, service and the WhatsApp inbox | M17, M32, M33 | 101.3 WhatsApp sender |
| 9 | 101.11 Loyalty and offers depth | M27, M28 | D11 |
| 10 | 101.12 Store clienteling and appointments | M31 | D4, Stage 102 |
| 11 | 101.3.5 Hold-outs and lift | M22 advanced, M23 | Enough sending volume |
| 12 | 101.6 Business-buyer pipeline | M29, M30 | D1 |
| 13 | 101.7 Reports, guides, acceptance pack | M24, report catalog, acceptance pack | Each phase adds its own; closes the stage |

## 3. Phases

### 101.0 Decisions
**Why:** the blueprint itself says unresolved identity, consent, money and provider decisions block the matching production feature.
**Your actions:** answer D1, D2, D4, D6, D8-D13; give the SMS provider account, DLT entity ID, sender header and the first approved templates (D2).
**Items:** 101.0.2 decisions answered · 101.0.3 benchmark (done by the blueprint) · 101.0.4 blueprint recorded and planned (this file).
**Done when:** every *Needs decision* row above has an answer recorded in Stage 101.

### 101.1 One customer page and identity (M05, M06)
**Why:** everything else - segments, store staff, service - works from this page.
**Items:**
- 101.1.1 **Profile** - birthday, anniversary, gender, sizes, preferred store and salesman, tags, notes, consent per channel and purpose; imported fields show their source and last update.
- 101.1.2 **One timeline** - bills, returns, Shopify orders (Stage 99), points, vouchers, messages with their send decision, tickets and notes, newest first by when it happened, each opening its document; paged.
- 101.1.3 **Duplicates and merge** - a "possible duplicates" list (same email, same name and birthday, CleverTap ID); merge shows what moves - bills, points, consent, active journeys - and goes through the approval engine; the merged record keeps pointing to the survivor (26.7.9) and the survivor gets at most one copy of any pending message.
- 101.1.4 **Masking by role** - phone and email masked for roles that do not need them, the same way in search, the page, exports and the API (M06-R03), using the existing sensitive-fields rules.
**Done when:** a store manager opens a customer by phone, sees the full timeline, and a role without contact access sees masked numbers everywhere, including an export.

### 101.8 Consent ledger, preferences, erasure (M07, M35)
**Why:** "a reachable phone is not permission". Today consent is two ticks on the Customer; the blueprint needs who agreed, when, how and to which notice, kept forever, and a withdrawal that stops queued messages.
**Items:**
- 101.8.1 **Consent ledger** - every grant or withdrawal is a new row (customer, channel, purpose, granted / denied / withdrawn, source, who recorded it, evidence, notice version, when); the Customer ticks become the latest-row summary; today's ticks are carried in as "legacy" rows. Order of strength: legal block or erasure → global opt-out → channel opt-out → topic → consent. No row means not reachable.
- 101.8.2 **Preference page and unsubscribe** - every marketing message carries a short token link (no customer ID in it) to a page for opting out per channel; one-click unsubscribe for email; STOP replies on SMS / WhatsApp handled from the provider callback; repeat requests are harmless; opting back in needs a fresh action by the customer, never an import.
- 101.8.3 **Erasure that sticks** - an approved erasure (49.6.8) first cancels the customer's queued messages and journeys, then anonymises; a hashed phone / email suppression row stops a later import or Shopify sync from re-creating them.
**Done when:** a withdrawal made while 1,000 messages are queued stops every one not yet handed to the provider, and an import with "opt-in = yes" for that customer does not restore consent.

### 101.2 Segments and lifecycle (M09, M10)
**Items:**
- 101.2.1 **Segment builder** - groups of AND / OR / NOT; rules on profile fields, spend, visits, last purchase, category / brand bought, store, tier, birthday month, consent, and "bought / did not buy in the last N days"; windows in the client's time zone; a live count with a waterfall (matched → no consent → no contact → final); "why is this customer in it"; saved versions - a campaign uses one exact version and a snapshot of who was in it.
- 101.2.2 **RFM and lifecycle** - reuses today's RFM scoring; adds named states that do not overlap (new, active repeat, at risk, lapsed, reactivated, never bought), versioned thresholds, recomputed after returns and merges.
- 101.2.3 **Static lists** - upload a list of phones through `BulkImportCSV`; each list keeps where it came from and an expiry, and still passes the consent check at send time.
**Done when:** the blueprint's examples hold: average spend over 100, blank and 200 is 150; a bill with three lines counts once; a customer who never bought sits in "never bought", not "lowest spend".

### 101.9 The send gate (M14, M37 budgets, M36)
**Why:** the one shared choke point every marketing, journey and store-staff message passes through, so no channel or screen can skip a rule. Built on `engines/notifications.go` and its 73.2.13 delivery jobs.
**Items:**
- 101.9.1 **Send decision** - one send record per campaign run × customer × channel (a duplicate is refused by a unique key); the blueprint's 11 checks run in order right before handing to the provider - stopped campaign, expired / duplicate, erased customer, consent now, hold-out, no valid contact or template, goal already met, quiet hours, limits, budget - and a suppressed message records its reason (`NO_CONSENT`, `CAP_REACHED`, `QUIET_HOURS` ...).
- 101.9.2 **Limits, quiet hours and budget** - D8's limits held atomically (two workers cannot both take the last slot); a quiet-hours delay past the campaign's end expires the message instead of sending late; a campaign budget is reserved from an estimate (SMS counted in billable parts) and checked against the provider's actual charge later.
- 101.9.3 **Unknown outcomes and the stop button** - a provider timeout after sending is "unknown", resolved from the provider's status or callback, never resent blindly; pause and stop per campaign run.
- 101.9.4 **Test safety** - a mock provider (accept, reject, timeout, late callback) for tests; test sends and non-production servers reach only an allowlist of numbers and emails.
- 101.9.5 **Alerts** - stuck journeys, unknown outcomes, provider failures and budget overruns raise the existing ops alerts.
**Done when:** the acceptance cases E04 (withdrawal while queued), M14-A01 (cap race), M14-A02 (timeout then callback) and M14-A03 (quiet hours past expiry) pass as Go tests.

### 101.3 Templates, senders, campaigns, results (M11, M12, M15, M16, M22)
**Items:**
- 101.3.4 **Templates** (built first) - versioned per channel and language; variables from the customer, the bill or the item; a missing required value suppresses the message (`TEMPLATE_DATA_MISSING`) rather than showing a blank price; preview with sample customers; SMS part counter; DLT template ID on SMS templates; WhatsApp templates synced with their approval state.
- 101.3.1 **Senders** (D2) - email (verified sender, SPF / DKIM shown, unsubscribe header, bounce and complaint suppress the address), SMS (DLT entity / header / template IDs checked before sending, STOP), WhatsApp Cloud API (templates, signed callbacks); credentials encrypted per client; delivery states queued → accepted → delivered / failed / unknown.
- 101.3.2 **Campaign run** - segment version × template version × channel × schedule × budget × goal; approval through the approval engine (D13), and any edit after approval needs a new approval; a pre-send waterfall (matched, hold-out, no consent, no contact, final, estimated cost); a unique voucher per customer when the campaign gives one; pause and stop.
- 101.3.3 **Campaign results** - matched, sent, accepted, delivered, read where the channel tells us, vouchers redeemed, bills credited under D10, cost; numbers the channel does not report stay blank, not zero.
- 101.3.5 **Hold-outs and lift** (later, step 11) - a fixed random hold-out per campaign, kept the same on rerun; results compare everyone assigned, not just those reached; small samples read "not enough data yet".
**Pilot gate:** one campaign to a small real segment by email and SMS, with its send decisions, provider receipts, cost and credited sales reconciled by hand. Journeys do not start until this passes.

### 101.4 Automatic journeys (M13, J01-J08)
**Items:**
- 101.4.2 **Journey engine** (built first) - steps from D9: start on an event, a segment or a date; wait; wait for an event; check; send (through 101.9); create a task; end. One live run per customer per journey (per cart for cart journeys); re-entry cool-down; ends early when the goal happens (for example a purchase); waits survive a server restart without sending twice; a published journey is frozen - edits make a new version and running customers finish the old one.
- 101.4.1 **Ready-made journeys** - welcome and first purchase, thank-you after the first bill, birthday, anniversary, points about to expire, not seen for N days (win-back), feedback after a bill, product arrived for a customer's order, and Shopify abandoned cart once Stage 99 sends cart events. Each with its template, timing and optional voucher; on / off per client.
**Done when:** M13-A01 (restart during a 24-hour wait, no duplicate) and the blueprint's cart-recovery fixture (debounce, stale-data hold, same-cart purchase ends the run) pass as tests.

### 101.5 Feedback, service and the WhatsApp inbox (M17, M32, M33)
**Items:**
- 101.5.1 **Feedback after a bill** - an NPS or rating link by WhatsApp / SMS, one per bill with a cool-down; NPS = % promoters (9-10) − % detractors (0-6) with the response count shown; a changed question makes a new survey version; a low score opens one Service Ticket for the store manager.
- 101.5.2 **Tickets tied to the customer** - customer, bill and store on each ticket; states new → open → waiting on customer / waiting internally → resolved → closed; first-response and resolution SLA on business hours; private notes never sent to the customer; on the timeline.
- 101.5.3 **WhatsApp inbox** - customer replies land in one inbox linked to the customer and ticket; a person can take over from the automatic replies and hand back; replying inside WhatsApp's service window; replies never count as marketing consent.

### 101.11 Loyalty and offers depth (M27, M28)
**Items:**
- 101.11.1 **Pending points** (D11) - optional "pending until the return window ends" per programme.
- 101.11.2 **One-use codes everywhere** - a single-use voucher cannot be taken twice by the till and Shopify at the same moment (reserve, then capture); codes are unguessable; a cancelled bill's code follows a stated reinstate rule.
- 101.11.3 **Referrals** - refer-a-friend codes; the reward is released only after the friend's first bill passes the return window; self-referral flagged.
**Done when:** two simultaneous redemptions of 80 points from 100 give one success (M27-A01) and two simultaneous uses of one code give one (M28-A02).

### 101.12 Store clienteling and appointments (M31)
**Items:**
- 101.12.1 **Store worklist** - tasks for custom orders, product arrivals, alterations and follow-ups, with a due time, an owner and an outcome, in the task inbox and the store app; a staff member's WhatsApp to a customer goes through the send gate.
- 101.12.2 **Customer appointments** - a visit, trial or fitting at a store with slot capacity (two bookings cannot take one slot), reminders, and cancellation that stops the reminders. Separate from the warehouse dock Appointment (42.3.2).
- 101.12.3 **Wish list and hold requests** - a customer's wished items; a hold asks Inventory, and nothing tells the customer "it is ready" until the hold is confirmed.

### 101.6 Business-buyer pipeline (M29, M30)
**Items:** 101.6.1 Leads (source, owner, status; assignment rules with a queue for unassigned; converting twice gives one result) · 101.6.2 Opportunities on a board (stages with entry rules, value, chance, expected close; won needs an approved quotation; lost needs a reason; history never overwritten; a won opportunity raises one Sales Order even if retried) · 101.6.3 Follow-ups in the task inbox · 101.6.4 Forecast (weighted pipeline, committed, booked orders kept apart; last month's snapshot stays as it was).

### 101.7 Reports, guides, acceptance pack (M24)
**Items:**
- 101.7.1 **CRM dashboard and reports** - as `ReportDefinition` reports and a Stage 100 dashboard, taken from the blueprint's catalog: customer summary, consent audit, segment members, campaign summary, message detail, journey performance, credited sales, RFM and lifecycle, coupon use, service SLA, clienteling tasks, feedback, message cost, pipeline. Each states its time zone, date basis and refresh time.
- 101.7.2 **Guides and walk-through** - USER_GUIDE CRM chapter rewritten; ADMIN_GUIDE senders, DLT, limits and consent; KB; a campaign sent to test numbers end to end.
- 101.7.3 **Acceptance pack** - the blueprint's acceptance cases that apply here become Go tests: other-client access refused, duplicate events, shared-device not applicable (D5), withdrawal while queued, cap race, provider timeout, restart during a wait, redemption race, erasure then re-import.

## 4. Not adopted

| Blueprint item | Why not here |
|---|---|
| Its stack: TypeScript / React, Temporal, Redis, ClickHouse, a separate queue | First principle: no new framework or dependency. Go, Postgres, the existing job runner and outbox carry the same rules |
| A canonical commerce layer copying orders into the CRM (M08-R01) | The ERP is the authority for bills, returns and stock; the CRM reads them. Shopify orders arrive as ERP orders (Stage 99) |
| Separate organisation / brand / environment model (M01) | Client schemas already isolate data. Only the allowlist for test sends is adopted (101.9.4) |
| Several people on one phone (M05-R02) | The user's rule: one phone, one customer (85 D1) |
| Web and app tracking SDKs, push, in-app messages, inbox, remote configuration (M04-R05, M18, M19, M25) | No shopper app or tracked website exists; Shopify events come through Stage 99. Revisit if a shopper app is built |
| Behavioural funnels and path analysis (M21) | Needs those SDK events. Cohort retention already exists |
| Advertising audiences, RCS (M20-R03/R04, M16-R05) | No demand yet; outbound webhooks (38.4) cover custom actions |
| AI recommendations, predictions and drafting (M26) | The blueprint itself says not before data is reliable. Best-sellers by category can come later as a plain report |
| Custom-object designer and SCIM (M03-R03-R05, M02-R04) | The doctype machinery already handles fields; no identity-provider demand |
| SaaS billing for messages, extension sandboxes (M37-R03-R05, M38-R05) | The Control Panel covers clients; only message cost tracking is adopted |
| Sequential / bandit experiments (M23-R05) | Fixed hold-outs first, as the blueprint advises |
| Its team, sprint plan and INR 1.59 crore cost model (sections 24-27) | Built by stage in this repo; the phase order above follows its sequence |
| Reusing Dittofeed, Mautic, Twenty, Frappe CRM, Chatwoot or Novu code (its D12) | Patterns only; no new dependency, and several are AGPL / GPL |

## 5. Guardrails

- Every marketing message, from any screen or journey, goes through the send gate (101.9). No second sending path.
- Consent is checked when the message is about to go, not when the campaign was made.
- Money stays in the ERP's existing amount fields; points stay separate from money; bills and returns are never edited from the CRM.
- Approved campaign, template, segment and journey versions are never changed in place.
- A suppressed or held-out customer is a recorded outcome with a reason, never silently missing.
- Schema changes are additive migrations; new screens are lazy view modules (cold-core budget).
- Test sends and non-production servers reach only allowlisted numbers and emails.
