---
doc_id: DOC-C44AC5B356
title: Admin Guide
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

# Admin Guide

A complete, standalone operator manual — written so a person can pick up this system with **zero AI assistance**, starting from a bare Windows machine, and get it running, keep it running, and grow with it. It's organized in layers: start at §1 if you've never touched this system before; skip ahead if you already know the basics and need a specific procedure.

This guide reuses, rather than duplicates, the deeper operational docs that already exist — it's the table of contents and the walkthrough that ties them together. Where a topic has its own detailed doc, this guide says so and points there.

*Need literal click-by-click steps for an admin screen or a maker-checker/approval workflow? See **[admin-sop.md](admin-sop.md)** — same operator-manual voice, one section per screen and per workflow.*

This guide is split into 6 parts so each one opens quickly. The sections of each part are listed below.

## Parts

1. **[Foundation, users and settings](admin-guide-foundation-users-and-settings.md)**
   - [Part A — Foundation (for someone who has never seen this system)](admin-guide-foundation-users-and-settings.md#part-a--foundation-for-someone-who-has-never-seen-this-system)
      - [A.1 What is this, in one paragraph?](admin-guide-foundation-users-and-settings.md#a1-what-is-this-in-one-paragraph)
      - [A.2 What you need before you start](admin-guide-foundation-users-and-settings.md#a2-what-you-need-before-you-start)
      - [A.3 Starting the system for the first time](admin-guide-foundation-users-and-settings.md#a3-starting-the-system-for-the-first-time)
      - [A.4 Stopping the system](admin-guide-foundation-users-and-settings.md#a4-stopping-the-system)
      - [A.5 The single most important safety rule](admin-guide-foundation-users-and-settings.md#a5-the-single-most-important-safety-rule)
   - [Part B — Day-to-Day Operation](admin-guide-foundation-users-and-settings.md#part-b--day-to-day-operation)
      - [B.1 The `manage.ps1` command reference](admin-guide-foundation-users-and-settings.md#b1-the-manageps1-command-reference)
      - [B.2 User and Role Management](admin-guide-foundation-users-and-settings.md#b2-user-and-role-management)
      - [B.3 Configuring the System (no code required)](admin-guide-foundation-users-and-settings.md#b3-configuring-the-system-no-code-required)
         - [B.3.0 Configuration — every operational setting, in one place](admin-guide-foundation-users-and-settings.md#b30-configuration--every-operational-setting-in-one-place)
2. **[Settings, numbering and batches](admin-guide-settings-numbering-and-batches.md)**
         - [B.3.1 Status Transition Rules — how "strict" works](admin-guide-settings-numbering-and-batches.md#b31-status-transition-rules--how-strict-works)
         - [B.3.2 Prefix Configurations — the number series behind every transaction](admin-guide-settings-numbering-and-batches.md#b32-prefix-configurations--the-number-series-behind-every-transaction)
         - [B.3.3 Turning on batch / lot traceability (Stage 42.1)](admin-guide-settings-numbering-and-batches.md#b33-turning-on-batch--lot-traceability-stage-421)
3. **[Items, printing and stock](admin-guide-items-printing-and-stock.md)**
         - [B.3.4 Sticker templates, template rules and the Sticker Studio (Stages 52, 58, 70)](admin-guide-items-printing-and-stock.md#b34-sticker-templates-template-rules-and-the-sticker-studio-stages-52-58-70)
         - [B.3.5 Sending events to another system (Webhook Subscriptions)](admin-guide-items-printing-and-stock.md#b35-sending-events-to-another-system-webhook-subscriptions)
         - [B.3.5a Who can delete a purchase order (Stage 65.2)](admin-guide-items-printing-and-stock.md#b35a-who-can-delete-a-purchase-order-stage-652)
         - [B.3.5b Item names, categories and product types (Stage 65.3)](admin-guide-items-printing-and-stock.md#b35b-item-names-categories-and-product-types-stage-653)
         - [B.3.5c The item master, asked once — defaults, item fields, attributes per type, correcting a treatment (Stage 89)](admin-guide-items-printing-and-stock.md#b35c-the-item-master-asked-once--defaults-item-fields-attributes-per-type-correcting-a-treatment-stage-89)
         - [B.3.6 Designs from the PO, multi-PO goods receipts and price-difference notes (Stage 62)](admin-guide-items-printing-and-stock.md#b36-designs-from-the-po-multi-po-goods-receipts-and-price-difference-notes-stage-62)
         - [B.3.7 Transaction print templates (Stages 63, 71)](admin-guide-items-printing-and-stock.md#b37-transaction-print-templates-stages-63-71)
         - [B.3.8 Item & Material Master — profiles, packaging backflush at the till, gifts with purchase, gift cards (Stage 72)](admin-guide-items-printing-and-stock.md#b38-item--material-master--profiles-packaging-backflush-at-the-till-gifts-with-purchase-gift-cards-stage-72)
         - [B.3.9 Stock transfers — approval, reason codes and tracked stock (Stage 73.1.7)](admin-guide-items-printing-and-stock.md#b39-stock-transfers--approval-reason-codes-and-tracked-stock-stage-7317)
         - [B.3.10 Cycle count recount triggers (Stage 73.1.8)](admin-guide-items-printing-and-stock.md#b310-cycle-count-recount-triggers-stage-7318)
         - [B.3.11 Warehouses for several 3PL owners (Stage 59)](admin-guide-items-printing-and-stock.md#b311-warehouses-for-several-3pl-owners-stage-59)
4. **[Finance, integrations and go-live](admin-guide-finance-integrations-and-go-live.md)**
         - [B.3.12 Finance controls — who posted, period close and reopen, bank reconciliation, control totals (Stage 73, 55.10–55.20)](admin-guide-finance-integrations-and-go-live.md#b312-finance-controls--who-posted-period-close-and-reopen-bank-reconciliation-control-totals-stage-73-55105520)
         - [B.3.12 RFQ to purchase order (Stages 73.2.1-73.2.3, decision D06)](admin-guide-finance-integrations-and-go-live.md#b312-rfq-to-purchase-order-stages-7321-7323-decision-d06)
         - [B.3.13 Supply proposals (Stage 73.2.18)](admin-guide-finance-integrations-and-go-live.md#b313-supply-proposals-stage-73218)
         - [B.3.13a Vendor price agreements and PO discounts (Stages 148.6, 148.7)](admin-guide-finance-integrations-and-go-live.md#b313a-vendor-price-agreements-and-po-discounts-stages-1486-1487)
         - [B.3.13b Charge codes (Stage 148.8)](admin-guide-finance-integrations-and-go-live.md#b313b-charge-codes-stage-1488)
         - [B.3.14 Quality and maintenance (Stage 55.13)](admin-guide-finance-integrations-and-go-live.md#b314-quality-and-maintenance-stage-5513)
         - [B.3.16 Outbound, labour and billing (Stage 55.8)](admin-guide-finance-integrations-and-go-live.md#b316-outbound-labour-and-billing-stage-558)
         - [B.3.15 Inbound: yard, RF receiving and putaway work (Stage 55.7)](admin-guide-finance-integrations-and-go-live.md#b315-inbound-yard-rf-receiving-and-putaway-work-stage-557)
         - [B.3.17 E-invoice (IRN) — GSTIN set-up, the queue, cancellation and the outage path (Stage 75.2)](admin-guide-finance-integrations-and-go-live.md#b317-e-invoice-irn--gstin-set-up-the-queue-cancellation-and-the-outage-path-stage-752)
         - [B.3.18 OMS steps, the two bills, and the Inventory documents (Stage 88)](admin-guide-finance-integrations-and-go-live.md#b318-oms-steps-the-two-bills-and-the-inventory-documents-stage-88)
         - [B.3.19 Setup Workbook — a whole organisation's setup from one Excel file (Stage 128)](admin-guide-finance-integrations-and-go-live.md#b319-setup-workbook--a-whole-organisations-setup-from-one-excel-file-stage-128)
         - [B.3.19a Go-live: scope, structure, finance start, cutover and the gate (Stages 47.12 and 49.17.3)](admin-guide-finance-integrations-and-go-live.md#b319a-go-live-scope-structure-finance-start-cutover-and-the-gate-stages-4712-and-49173)
         - [B.3.20 Shopify — products, prices and stock out; orders, payments and refunds in (Stage 99)](admin-guide-finance-integrations-and-go-live.md#b320-shopify--products-prices-and-stock-out-orders-payments-and-refunds-in-stage-99)
         - [B.3.21 CRM — senders, consent, contact rules and approvals (Stage 101)](admin-guide-finance-integrations-and-go-live.md#b321-crm--senders-consent-contact-rules-and-approvals-stage-101)
         - [B.3.22 Bin rules, the bin generator and store shipping limits (Stage 146.3 and 146.4)](admin-guide-finance-integrations-and-go-live.md#b322-bin-rules-the-bin-generator-and-store-shipping-limits-stage-1463-and-1464)
         - [B.3.23 Credit control on the order (Stage 148.3)](admin-guide-finance-integrations-and-go-live.md#b323-credit-control-on-the-order-stage-1483)
         - [B.3.24 Journal dimension rules and templates (Stage 148.20 and 148.21)](admin-guide-finance-integrations-and-go-live.md#b324-journal-dimension-rules-and-templates-stage-14820-and-14821)
         - [B.3.23a Brand-restricted dealers (Stage 148.43)](admin-guide-finance-integrations-and-go-live.md#b323a-brand-restricted-dealers-stage-14843)
      - [B.4 Where to Look When Something Seems Wrong](admin-guide-finance-integrations-and-go-live.md#b4-where-to-look-when-something-seems-wrong)
5. **[Security events and operations](admin-guide-security-and-operations.md)**
      - [B.4a Settings → Monitoring → Security Events (Stage 49.11-49.13)](admin-guide-security-and-operations.md#b4a-settings--monitoring--security-events-stage-4911-4913)
   - [Part C — Operator / Platform-Team Level](admin-guide-security-and-operations.md#part-c--operator--platform-team-level)
      - [C.1 Environment Layout](admin-guide-security-and-operations.md#c1-environment-layout)
      - [C.2 Deployment Pipeline](admin-guide-security-and-operations.md#c2-deployment-pipeline)
      - [C.3 Backup and Restore](admin-guide-security-and-operations.md#c3-backup-and-restore)
      - [C.4 Incident Response and Alerting](admin-guide-security-and-operations.md#c4-incident-response-and-alerting)
      - [C.5 Connector / Integration Verification](admin-guide-security-and-operations.md#c5-connector--integration-verification)
      - [C.6 Creating and Removing a Tenant (`tenantctl`)](admin-guide-security-and-operations.md#c6-creating-and-removing-a-tenant-tenantctl)
      - [C.7 Data Console and cross-client control — platform operators only (Stage 60.6)](admin-guide-security-and-operations.md#c7-data-console-and-cross-client-control--platform-operators-only-stage-606)
      - [C.8 Control Panel — every client at a glance, pause and free trial (Stage 65.5, 65.6)](admin-guide-security-and-operations.md#c8-control-panel--every-client-at-a-glance-pause-and-free-trial-stage-655-656)
      - [C.9 A client's product attributes, from the industry library (Stage 81)](admin-guide-security-and-operations.md#c9-a-clients-product-attributes-from-the-industry-library-stage-81)
      - [Install a new host, and the sandbox rehearsal](admin-guide-security-and-operations.md#install-a-new-host-and-the-sandbox-rehearsal)
6. **[Developer reference and glossary](admin-guide-developer-and-glossary.md)**
   - [Part D — Developer / CTO Level](admin-guide-developer-and-glossary.md#part-d--developer--cto-level)
      - [D.1 Deployment (`promote.ps1`)](admin-guide-developer-and-glossary.md#d1-deployment-promoteps1)
      - [D.2 Building and Testing](admin-guide-developer-and-glossary.md#d2-building-and-testing)
      - [D.3 Project Layout](admin-guide-developer-and-glossary.md#d3-project-layout)
      - [D.4 Architecture Reference](admin-guide-developer-and-glossary.md#d4-architecture-reference)
      - [D.5 Security Posture](admin-guide-developer-and-glossary.md#d5-security-posture)
      - [D.6 Extending the System](admin-guide-developer-and-glossary.md#d6-extending-the-system)
      - [D.7 Governance Model](admin-guide-developer-and-glossary.md#d7-governance-model)
   - [Glossary (technical → plain language, both directions)](admin-guide-developer-and-glossary.md#glossary-technical--plain-language-both-directions)
   - [Two-factor for sensitive actions (Stage 49.2.3)](admin-guide-developer-and-glossary.md#two-factor-for-sensitive-actions-stage-4923)
   - [Staged imports: approval rule and retention (Stage 47.13.1)](admin-guide-developer-and-glossary.md#staged-imports-approval-rule-and-retention-stage-47131)
