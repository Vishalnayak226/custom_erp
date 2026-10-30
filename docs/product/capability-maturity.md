---
doc_id: DOC-PRODUCT-CAPABILITY-MATURITY
title: Capability maturity
type: generated
status: active
owner: product-owner
authority: projection
confidentiality: internal
last_verified: 2026-09-10
---

# Capability maturity

<!-- GENERATED FILE - DO NOT EDIT BY HAND.
     Source: `engines.CapabilityMaturityRegistry()` (`engines/capability_maturity.go`)
     Regenerate: `pwsh docs/update-docs.ps1 -Group Content` -->

> **Generated 2026-09-10.** This page is produced from `engines.CapabilityMaturityRegistry()` (`engines/capability_maturity.go`), for this source revision.
> It is not release or tenant assurance. Hand edits are lost on the next run - change the source instead.

Maturity is per configuration, not per module: the online till and the offline till are both the `sales` module and are not equally proven. **Experimental** - synthetic data only. **Preview** - built and tested, known limits, pilot use. **Production** - journey, control, documentation, restore and UAT evidence, with a live run on a client on record. **Certified** - external validation with an expiry (none yet). A module with no entry, a Certified entry past its expiry, or a Production entry without dated evidence fails `TestStage47175EveryModuleHasAMaturityEntry`. The Control Panel carries the same list (`capability_maturity`).

| Level | Capabilities |
|---|---|
| Experimental | 2 |
| Preview | 16 |
| Production | 13 |
| Certified | 0 |

## Production

| Capability | Module | Evidence | Limits | Verified |
|---|---|---|---|---|
| Core · stores, users, roles, masters (`core.masters`) | `core` | docs/qa/guide-org-cycle-2026-10-27.md §2 (BA setup on app); engines/role_templates.go; docs/guides/ADMIN_GUIDE.md §A; docs/operations/restore_drill_log.md | - | 2026-10-28 |
| CRM · customer reports, segments, templates (`crm_loyalty.segments_reports`) | `crm_loyalty` | docs/qa/guide-org-cycle-2026-10-27.md §6 (ten CRM reports, segment, explain view on app); docs/guides/USER_GUIDE.md CRM | the customer ledger shows credit invoices only (140.12.7) | 2026-10-28 |
| Finance · invoices, receipts, credit notes, GST and ageing reports (`finance.reconciling_reports`) | `finance` | docs/qa/guide-org-cycle-2026-10-27.md §7 (sales register, AR ageing, GSTR-1, trial balance tie on app); report control totals (engines/control_totals.go); docs/guides/USER_GUIDE.md Finance | a credit invoice does not move stock (140.12.4); receipts default to cash (140.12.5); prod days are UTC (132.19) | 2026-10-28 |
| HR · attendance, leave, loan, payroll (`hr.payroll`) | `hr` | docs/qa/guide-org-cycle-2026-10-27.md §8 (payslips posted on app, PF, PT, TDS, ESI to the paise, GL balances); engines/sensitive_fields.go (payroll read policy); docs/guides/USER_GUIDE.md HR | - | 2026-10-28 |
| OMS · website order to doorstep (internal courier) (`oms.order_to_doorstep`) | `oms` | docs/qa/guide-org-cycle-2026-10-27.md §4 (accept, allocate, pick, pack, bill, manifest, handover on app); handover posts the sales invoice (engines/oms_bill.go); docs/guides/USER_GUIDE.md OMS | a wave-picked order cannot be packed or billed (140.12.15) | 2026-10-28 |
| OMS · sales return (approve, receive, QC, refund) (`oms.sales_return`) | `oms` | docs/qa/guide-org-cycle-2026-10-27.md §4 (RR on app, refund processed, on hand back); engines/returns_atomic.go (one transaction, idempotent); docs/guides/USER_GUIDE.md Returns | the Returns screen's own check says no original bill for an OMS order (140.12.16); the returned unit waits for put-away | 2026-10-28 |
| PIM · attributes, channel export templates (`pim.attributes_templates`) | `pim` | docs/qa/guide-org-cycle-2026-10-27.md §5 (Shopify, Amazon, Myntra feeds checked header by header on app); docs/guides/USER_GUIDE.md PIM | a feed exports every item, CSV only (140.12.24); one field in two columns needs a field map (140.12.23) | 2026-10-28 |
| Procurement · PO, GRN, purchase return, bill match and pay (`procurement.po_to_pay`) | `procurement` | docs/qa/guide-org-cycle-2026-10-27.md §8 (PO, GRN, return with debit note, bill matched and paid, duplicate held on app); engines/grn_steps_post.go; docs/guides/USER_GUIDE.md Procurement | a purchase return takes the HO number series for a store GRN (140.12.8) | 2026-10-28 |
| Quality · incoming inspection, hold, NCR (`quality.inspection_ncr`) | `quality` | docs/qa/guide-org-cycle-2026-10-27.md §8 (failed inspection → held stock → NCR closed on app); docs/guides/USER_GUIDE.md Quality | - | 2026-10-28 |
| Reports · the catalog (`reports.catalog`) | `reports` | docs/qa/guide-org-cycle-2026-10-27.md §7 (all 421 reports ran on app, slowest 330 ms); docs/product/report-catalog-coverage.md; TestReportColumnsMatchCatalogSpec | a Cashier can read the trial balance through the report (140.12.10) | 2026-10-28 |
| POS · online till (cash, UPI, advance) (`sales.online_till`) | `sales` | docs/qa/guide-org-cycle-2026-10-27.md §3 (bills on app, 61-184 ms); till close variance → approval (engines/pos_session.go); docs/guides/USER_GUIDE.md POS; docs/operations/restore_drill_log.md; user approved the run 2026-10-28 | advances are left out of the close's expected cash (140.12.1); a close variance does not reach the GL (140.12.11) | 2026-10-28 |
| Service · tickets (`service.tickets`) | `service` | docs/qa/guide-org-cycle-2026-10-27.md §8 (ticket assigned → resolved → closed on app; resolving a draft refused); docs/guides/USER_GUIDE.md Service | - | 2026-10-28 |
| WMS · rack allocation, pick by bin, pack, put-away (`wms.rack_pick_pack`) | `wms` | docs/qa/guide-org-cycle-2026-10-27.md §4, §8 (put-away, bin move, over-put-away refused on app); engines/wms_inbound_work.go; docs/guides/USER_GUIDE.md WMS | after picking, units show as waiting for put-away (140.12.19) | 2026-10-28 |

## Preview

| Capability | Module | Evidence | Limits | Verified |
|---|---|---|---|---|
| Assets · register, depreciation, disposal (`assets.lifecycle`) | `assets` | engines/asset_lifecycle.go; engines/asset_depreciation.go; docs/qa/guide-org-cycle-2026-10-27.md §8 (local copy, same day) | Guide runs it on the local copy only | 2026-10-28 |
| CRM · WhatsApp / SMS campaign send (`crm_loyalty.campaign_send`) | `crm_loyalty` | docs/qa/guide-org-cycle-2026-10-27.md §6 (sent locally with a test sender: matched 1, sent 1, delivered 1) | no WhatsApp, SMS or email sender on app (140.12.26); WhatsApp needs a Meta-approved template name | 2026-10-28 |
| Expenses · claims and approval (`expenses.claims`) | `expenses` | engines/approval.go (maker-checker); docs/guides/USER_GUIDE.md Expenses | no live run on a client recorded | - |
| Finance · bank reconciliation (`finance.bank_reconciliation`) | `finance` | engines/bank_reconciliation.go; docs/qa/guide-org-cycle-2026-10-27.md §8 (local copy, same day) | Guide runs it on the local copy only | 2026-10-28 |
| Finance · accounting periods, close, reopen (`finance.periods_close`) | `finance` | engines/accounting_periods.go; period_reopened detection; bld020_period_boundary_test.go | no live close on a client recorded | 2026-10-28 |
| Payments · Pine Labs terminal (`integrations.pinelabs`) | `integrations` | POST /api/v1/pinelabs/credentials; engines/partial_settlement.go | reconciliation on minn reports a missing pinelabs_transactions table (prod finding, open) | - |
| Integrations · Shopify orders and inventory (`integrations.shopify`) | `integrations` | engines/channel_credentials.go; POST /api/v1/shopify/{channel}/credentials; integration tests against recorded responses | no live store connected on a client | - |
| Inventory · transfers, adjustments, counts (`inventory.transfers_adjustments`) | `inventory` | engines/inventory.go; inventory.adjustment.post on the idempotency spine (engines/command_spine.go); stock_adjustment_burst detection | local runs only; the stock-vs-GL control total ties except the 132.22 gap fixed in 0.19.0 | 2026-10-28 |
| Manufacturing · BOM, production order, completion (`manufacturing.production`) | `manufacturing` | docs/qa/guide-org-cycle-2026-10-27.md §8 (local copy); engines/job_work.go | not run on app until 132.22 (finished goods at no cost) is released; each run widened the stock-vs-GL gap | 2026-10-28 |
| Master data · setup workbook (preview, apply, re-apply) (`master_data.setup_workbook`) | `master_data` | engines/setup_path.go; Stage 128 (one Excel per client); docs/guides/ADMIN_GUIDE.md Setup | applied by Guide on minn and app for setup; no business-cycle run rests on it | 2026-10-19 |
| PIM · publish to Shopify (`pim.channel_publish`) | `pim` | release 0.19.0 (PIM publishes to Shopify linked); engines/marketplace.go | channels on app are switched off with no credentials; nothing published live | - |
| RFQ · request, vendor quotes, selection (`rfq.quotes`) | `rfq` | POST /api/v1/rfq/{id}/quote, /rfq/select-quote (route_capabilities.go); docs/guides/USER_GUIDE.md RFQ | no live run on a client recorded | - |
| POS · bill return, exchange, cancel (`sales.bill_return_exchange`) | `sales` | internal/server/handlers_pos_held.go; till_reversal_burst detection; docs/qa/guide-org-cycle-2026-10-27.md §3 | a return of a credit-invoice line was valued at 0 on 0.18.0 (140.12.3) | 2026-10-28 |
| POS · offline till (`sales.offline_till`) | `sales` | release 0.19.0 (offline sales on the till's day, held offline cash in the drawer); internal/server/handlers_pos_offline_sales.go; offline_sale_burst detection (49.5.4) | no live run on a client recorded; validated on the server from the till's own record | 2026-10-28 |
| Stickers · barcode and label printing (`stickers.labels`) | `stickers` | engines/print_documents.go; PrintBridge (docs/guides/ADMIN_GUIDE.md Printing) | PrintBridge trusts one address; an agent built for another URL reports QZ not reachable | - |
| WMS · wave picking (`wms.wave_picking`) | `wms` | engines/wms.go; docs/qa/guide-org-cycle-2026-10-27.md §4 (local only) | a wave-picked order cannot be packed or billed (140.12.15) | 2026-10-28 |

## Experimental

| Capability | Module | Evidence | Limits | Verified |
|---|---|---|---|---|
| Courier · Delhivery, Shiprocket (API) (`integrations.courier_api`) | `integrations` | engines/marketplace.go (provider contract); bld031_provider_extension_contract_test.go (mocks) | no courier account; the live run used the internal courier only (docs/qa/guide-org-cycle-2026-10-27.md §2) | - |
| E-invoice · IRP (IRN) (`integrations.einvoice_irp`) | `integrations` | docs/product/einvoice-iris-irp-plan-2026-10-17.md; engines validateEInvoiceLock (75.2.7) | no IRP sandbox or GSP account connected; IRN fields are locked once present, never issued live | - |

