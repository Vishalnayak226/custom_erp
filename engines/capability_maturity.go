package engines

import (
	"fmt"
	"sort"
	"strings"
	"time"
)

// Stage 47.17.5: capability maturity, per configuration rather than per
// module. A module is a licence boundary (modules.go); a capability is one
// way of using it - the online till and the offline till are both "sales",
// and are not equally proven. Each entry says what level the capability is
// at, the evidence behind that level, the limits known at Preview, and, for
// Certified, when the external validation expires.
//
// The levels, from the tracker's own definition:
//
//	Experimental - exercised against synthetic data only (mocks, no live account)
//	Preview      - built and tested; known limits; pilot use
//	Production   - journey, control, documentation, restore and UAT evidence,
//	               including a live run on a client (app / minn) on record
//	Certified    - external validation with an expiry (none yet)
//
// Filled from what docs/micro_checklist.md and docs/qa record as verified -
// Production only where a live run on app / minn is written down, Preview by
// default, Certified nowhere. TestStage47175 keeps every module covered and
// no Certified entry past its expiry; cmd/gendocs projects the registry into
// docs/product/capability-maturity.md; the Control Panel carries it as
// `capability_maturity`.
const (
	MaturityExperimental = "Experimental"
	MaturityPreview      = "Preview"
	MaturityProduction   = "Production"
	MaturityCertified    = "Certified"
)

// CapabilityMaturity is one capability's standing.
type CapabilityMaturity struct {
	Key    string `json:"key"`    // stable id, module.configuration
	Module string `json:"module"` // module_key in public.modules
	Title  string `json:"title"`  // "POS · online till"
	Level  string `json:"level"`
	// Evidence: short pointers - a journey record, a control, a test, a guide
	// section, a restore drill, a UAT sign-off. Never prose.
	Evidence []string `json:"evidence"`
	// Limits known at this level; what a pilot must know before relying on it.
	Limits string `json:"limits,omitempty"`
	// VerifiedOn is the date of the evidence the level rests on.
	VerifiedOn string `json:"verified_on,omitempty"`
	// CertifiedUntil is the external validation's expiry, Certified only.
	CertifiedUntil string `json:"certified_until,omitempty"`
}

// orgCycle is the live organisation-cycle run on app.wholeops.in (release
// 0.18.0, 2026-10-27/28, user-approved writes), the one record that puts a
// capability at Production today.
const (
	orgCycle     = "docs/qa/guide-org-cycle-2026-10-27.md"
	orgCycleDate = "2026-10-28"
	userGuide    = "docs/guides/USER_GUIDE.md"
	adminGuide   = "docs/guides/ADMIN_GUIDE.md"
	restoreDrill = "docs/operations/restore_drill_log.md"
)

var capabilityMaturity = []CapabilityMaturity{
	// --- Production: live run on app on record ---------------------------
	{Key: "core.masters", Module: "core", Title: "Core · stores, users, roles, masters", Level: MaturityProduction,
		Evidence:   []string{orgCycle + " §2 (BA setup on app)", "engines/role_templates.go", adminGuide + " §A", restoreDrill},
		VerifiedOn: orgCycleDate},
	{Key: "sales.online_till", Module: "sales", Title: "POS · online till (cash, UPI, advance)", Level: MaturityProduction,
		Evidence:   []string{orgCycle + " §3 (bills on app, 61-184 ms)", "till close variance → approval (engines/pos_session.go)", userGuide + " POS", restoreDrill, "user approved the run 2026-10-28"},
		Limits:     "advances are left out of the close's expected cash (140.12.1); a close variance does not reach the GL (140.12.11)",
		VerifiedOn: orgCycleDate},
	{Key: "oms.order_to_doorstep", Module: "oms", Title: "OMS · website order to doorstep (internal courier)", Level: MaturityProduction,
		Evidence:   []string{orgCycle + " §4 (accept, allocate, pick, pack, bill, manifest, handover on app)", "handover posts the sales invoice (engines/oms_bill.go)", userGuide + " OMS"},
		Limits:     "a wave-picked order cannot be packed or billed (140.12.15)",
		VerifiedOn: orgCycleDate},
	{Key: "oms.sales_return", Module: "oms", Title: "OMS · sales return (approve, receive, QC, refund)", Level: MaturityProduction,
		Evidence:   []string{orgCycle + " §4 (RR on app, refund processed, on hand back)", "engines/returns_atomic.go (one transaction, idempotent)", userGuide + " Returns"},
		Limits:     "the Returns screen's own check says no original bill for an OMS order (140.12.16); the returned unit waits for put-away",
		VerifiedOn: orgCycleDate},
	{Key: "wms.rack_pick_pack", Module: "wms", Title: "WMS · rack allocation, pick by bin, pack, put-away", Level: MaturityProduction,
		Evidence:   []string{orgCycle + " §4, §8 (put-away, bin move, over-put-away refused on app)", "engines/wms_inbound_work.go", userGuide + " WMS"},
		Limits:     "after picking, units show as waiting for put-away (140.12.19)",
		VerifiedOn: orgCycleDate},
	{Key: "finance.reconciling_reports", Module: "finance", Title: "Finance · invoices, receipts, credit notes, GST and ageing reports", Level: MaturityProduction,
		Evidence:   []string{orgCycle + " §7 (sales register, AR ageing, GSTR-1, trial balance tie on app)", "report control totals (engines/control_totals.go)", userGuide + " Finance"},
		Limits:     "a credit invoice does not move stock (140.12.4); receipts default to cash (140.12.5); prod days are UTC (132.19)",
		VerifiedOn: orgCycleDate},
	{Key: "reports.catalog", Module: "reports", Title: "Reports · the catalog", Level: MaturityProduction,
		Evidence:   []string{orgCycle + " §7 (all 421 reports ran on app, slowest 330 ms)", "docs/product/report-catalog-coverage.md", "TestReportColumnsMatchCatalogSpec"},
		Limits:     "a Cashier can read the trial balance through the report (140.12.10)",
		VerifiedOn: orgCycleDate},
	{Key: "crm_loyalty.segments_reports", Module: "crm_loyalty", Title: "CRM · customer reports, segments, templates", Level: MaturityProduction,
		Evidence:   []string{orgCycle + " §6 (ten CRM reports, segment, explain view on app)", userGuide + " CRM"},
		Limits:     "the customer ledger shows credit invoices only (140.12.7)",
		VerifiedOn: orgCycleDate},
	{Key: "pim.attributes_templates", Module: "pim", Title: "PIM · attributes, channel export templates", Level: MaturityProduction,
		Evidence:   []string{orgCycle + " §5 (Shopify, Amazon, Myntra feeds checked header by header on app)", userGuide + " PIM"},
		Limits:     "a feed exports every item, CSV only (140.12.24); one field in two columns needs a field map (140.12.23)",
		VerifiedOn: orgCycleDate},
	{Key: "hr.payroll", Module: "hr", Title: "HR · attendance, leave, loan, payroll", Level: MaturityProduction,
		Evidence:   []string{orgCycle + " §8 (payslips posted on app, PF, PT, TDS, ESI to the paise, GL balances)", "engines/sensitive_fields.go (payroll read policy)", userGuide + " HR"},
		VerifiedOn: orgCycleDate},
	{Key: "procurement.po_to_pay", Module: "procurement", Title: "Procurement · PO, GRN, purchase return, bill match and pay", Level: MaturityProduction,
		Evidence:   []string{orgCycle + " §8 (PO, GRN, return with debit note, bill matched and paid, duplicate held on app)", "engines/grn_steps_post.go", userGuide + " Procurement"},
		Limits:     "a purchase return takes the HO number series for a store GRN (140.12.8)",
		VerifiedOn: orgCycleDate},
	{Key: "quality.inspection_ncr", Module: "quality", Title: "Quality · incoming inspection, hold, NCR", Level: MaturityProduction,
		Evidence:   []string{orgCycle + " §8 (failed inspection → held stock → NCR closed on app)", userGuide + " Quality"},
		VerifiedOn: orgCycleDate},
	{Key: "service.tickets", Module: "service", Title: "Service · tickets", Level: MaturityProduction,
		Evidence:   []string{orgCycle + " §8 (ticket assigned → resolved → closed on app; resolving a draft refused)", userGuide + " Service"},
		VerifiedOn: orgCycleDate},

	// --- Preview: built and tested, no live run on record, limits known ---
	{Key: "sales.offline_till", Module: "sales", Title: "POS · offline till", Level: MaturityPreview,
		Evidence:   []string{"release 0.19.0 (offline sales on the till's day, held offline cash in the drawer)", "internal/server/handlers_pos_offline_sales.go", "offline_sale_burst detection (49.5.4)"},
		Limits:     "no live run on a client recorded; validated on the server from the till's own record",
		VerifiedOn: "2026-10-28"},
	{Key: "sales.bill_return_exchange", Module: "sales", Title: "POS · bill return, exchange, cancel", Level: MaturityPreview,
		Evidence:   []string{"internal/server/handlers_pos_held.go", "till_reversal_burst detection", orgCycle + " §3"},
		Limits:     "a return of a credit-invoice line was valued at 0 on 0.18.0 (140.12.3)",
		VerifiedOn: orgCycleDate},
	{Key: "master_data.setup_workbook", Module: "master_data", Title: "Master data · setup workbook (preview, apply, re-apply)", Level: MaturityPreview,
		Evidence:   []string{"engines/setup_path.go", "Stage 128 (one Excel per client)", adminGuide + " Setup"},
		Limits:     "applied by Guide on minn and app for setup; no business-cycle run rests on it",
		VerifiedOn: "2026-10-19"},
	{Key: "inventory.transfers_adjustments", Module: "inventory", Title: "Inventory · transfers, adjustments, counts", Level: MaturityPreview,
		Evidence:   []string{"engines/inventory.go", "inventory.adjustment.post on the idempotency spine (engines/command_spine.go)", "stock_adjustment_burst detection"},
		Limits:     "local runs only; the stock-vs-GL control total ties except the 132.22 gap fixed in 0.19.0",
		VerifiedOn: orgCycleDate},
	{Key: "wms.wave_picking", Module: "wms", Title: "WMS · wave picking", Level: MaturityPreview,
		Evidence:   []string{"engines/wms.go", orgCycle + " §4 (local only)"},
		Limits:     "a wave-picked order cannot be packed or billed (140.12.15)",
		VerifiedOn: orgCycleDate},
	{Key: "finance.bank_reconciliation", Module: "finance", Title: "Finance · bank reconciliation", Level: MaturityPreview,
		Evidence:   []string{"engines/bank_reconciliation.go", orgCycle + " §8 (local copy, same day)"},
		Limits:     "Guide runs it on the local copy only",
		VerifiedOn: orgCycleDate},
	{Key: "finance.periods_close", Module: "finance", Title: "Finance · accounting periods, close, reopen", Level: MaturityPreview,
		Evidence:   []string{"engines/accounting_periods.go", "period_reopened detection", "bld020_period_boundary_test.go"},
		Limits:     "no live close on a client recorded",
		VerifiedOn: "2026-10-28"},
	{Key: "manufacturing.production", Module: "manufacturing", Title: "Manufacturing · BOM, production order, completion", Level: MaturityPreview,
		Evidence:   []string{orgCycle + " §8 (local copy)", "engines/job_work.go"},
		Limits:     "not run on app until 132.22 (finished goods at no cost) is released; each run widened the stock-vs-GL gap",
		VerifiedOn: orgCycleDate},
	{Key: "assets.lifecycle", Module: "assets", Title: "Assets · register, depreciation, disposal", Level: MaturityPreview,
		Evidence:   []string{"engines/asset_lifecycle.go", "engines/asset_depreciation.go", orgCycle + " §8 (local copy, same day)"},
		Limits:     "Guide runs it on the local copy only",
		VerifiedOn: orgCycleDate},
	{Key: "expenses.claims", Module: "expenses", Title: "Expenses · claims and approval", Level: MaturityPreview,
		Evidence: []string{"engines/approval.go (maker-checker)", userGuide + " Expenses"},
		Limits:   "no live run on a client recorded"},
	{Key: "rfq.quotes", Module: "rfq", Title: "RFQ · request, vendor quotes, selection", Level: MaturityPreview,
		Evidence: []string{"POST /api/v1/rfq/{id}/quote, /rfq/select-quote (route_capabilities.go)", userGuide + " RFQ"},
		Limits:   "no live run on a client recorded"},
	{Key: "stickers.labels", Module: "stickers", Title: "Stickers · barcode and label printing", Level: MaturityPreview,
		Evidence: []string{"engines/print_documents.go", "PrintBridge (docs/guides/ADMIN_GUIDE.md Printing)"},
		Limits:   "PrintBridge trusts one address; an agent built for another URL reports QZ not reachable"},
	{Key: "crm_loyalty.campaign_send", Module: "crm_loyalty", Title: "CRM · WhatsApp / SMS campaign send", Level: MaturityPreview,
		Evidence:   []string{orgCycle + " §6 (sent locally with a test sender: matched 1, sent 1, delivered 1)"},
		Limits:     "no WhatsApp, SMS or email sender on app (140.12.26); WhatsApp needs a Meta-approved template name",
		VerifiedOn: orgCycleDate},
	{Key: "pim.channel_publish", Module: "pim", Title: "PIM · publish to Shopify", Level: MaturityPreview,
		Evidence: []string{"release 0.19.0 (PIM publishes to Shopify linked)", "engines/marketplace.go"},
		Limits:   "channels on app are switched off with no credentials; nothing published live"},
	{Key: "integrations.shopify", Module: "integrations", Title: "Integrations · Shopify orders and inventory", Level: MaturityPreview,
		Evidence: []string{"engines/channel_credentials.go", "POST /api/v1/shopify/{channel}/credentials", "integration tests against recorded responses"},
		Limits:   "no live store connected on a client"},
	{Key: "integrations.pinelabs", Module: "integrations", Title: "Payments · Pine Labs terminal", Level: MaturityPreview,
		Evidence: []string{"POST /api/v1/pinelabs/credentials", "engines/partial_settlement.go"},
		Limits:   "reconciliation on minn reports a missing pinelabs_transactions table (prod finding, open)"},

	// --- Experimental: synthetic data only, no live account ---------------
	{Key: "integrations.courier_api", Module: "integrations", Title: "Courier · Delhivery, Shiprocket (API)", Level: MaturityExperimental,
		Evidence: []string{"engines/marketplace.go (provider contract)", "bld031_provider_extension_contract_test.go (mocks)"},
		Limits:   "no courier account; the live run used the internal courier only (" + orgCycle + " §2)"},
	{Key: "integrations.einvoice_irp", Module: "integrations", Title: "E-invoice · IRP (IRN)", Level: MaturityExperimental,
		Evidence: []string{"docs/product/einvoice-iris-irp-plan-2026-10-17.md", "engines validateEInvoiceLock (75.2.7)"},
		Limits:   "no IRP sandbox or GSP account connected; IRN fields are locked once present, never issued live"},
}

// CapabilityMaturityRegistry returns the registry, sorted by module then key.
func CapabilityMaturityRegistry() []CapabilityMaturity {
	out := make([]CapabilityMaturity, len(capabilityMaturity))
	copy(out, capabilityMaturity)
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Module != out[j].Module {
			return out[i].Module < out[j].Module
		}
		return out[i].Key < out[j].Key
	})
	return out
}

// MaturityLevels is the ladder, lowest first.
var MaturityLevels = []string{MaturityExperimental, MaturityPreview, MaturityProduction, MaturityCertified}

// CapabilityMaturityProblems is what the registry test refuses: a module
// with no entry, an entry on an unknown module, an unknown level, a
// Production entry with no evidence, a Certified entry with no expiry or
// past it. modules is the catalog (ListModules); nil skips the module checks.
func CapabilityMaturityProblems(entries []CapabilityMaturity, modules []string, now time.Time) []string {
	var problems []string
	covered := map[string]bool{}
	seen := map[string]bool{}
	known := map[string]bool{}
	for _, m := range modules {
		known[m] = true
	}
	levels := map[string]bool{}
	for _, l := range MaturityLevels {
		levels[l] = true
	}
	for _, e := range entries {
		if seen[e.Key] {
			problems = append(problems, e.Key+": duplicate key")
		}
		seen[e.Key] = true
		covered[e.Module] = true
		if modules != nil && !known[e.Module] {
			problems = append(problems, e.Key+": module "+e.Module+" is not in the module catalog")
		}
		if !levels[e.Level] {
			problems = append(problems, e.Key+": unknown level "+e.Level)
		}
		if e.Level == MaturityProduction || e.Level == MaturityCertified {
			if len(e.Evidence) == 0 || e.VerifiedOn == "" {
				problems = append(problems, e.Key+": "+e.Level+" needs evidence and a verified_on date")
			}
		}
		if e.Level == MaturityCertified {
			until, err := time.Parse("2006-01-02", e.CertifiedUntil)
			switch {
			case err != nil:
				problems = append(problems, e.Key+": Certified needs certified_until (YYYY-MM-DD)")
			case !until.After(now):
				problems = append(problems, fmt.Sprintf("%s: certification expired %s", e.Key, e.CertifiedUntil))
			}
		} else if e.CertifiedUntil != "" {
			problems = append(problems, e.Key+": certified_until on a "+e.Level+" entry")
		}
		if strings.TrimSpace(e.Title) == "" || strings.TrimSpace(e.Key) == "" {
			problems = append(problems, e.Key+": blank key or title")
		}
	}
	for _, m := range modules {
		if !covered[m] {
			problems = append(problems, "module "+m+" has no capability maturity entry")
		}
	}
	return problems
}
