package main

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"custom_erp/internal/server"
)

// Stage 49.5.1: docs/security/command-catalog.md - every command of the
// families the item names, with what the code says about each one and what
// only a person can say.
//
// Read from code (never hand-written): the routes of a family and their
// actor (route_capabilities.go: level, capability, role allowlist, module
// gate), the audit rows the handler and the engine calls under it write
// (LogAuditEvent literals, two calls deep), the idempotency mechanism
// (ClaimCommand / RunEconomicCommand / a client idempotency_key), whether an
// approval rule or a status-transition rule can gate it, and whether the
// middleware logs it as a bulk export.
//
// Hand-kept (commandNotes): the authoritative inputs the server computes and
// the client may not set, the state preconditions and the invariant each
// command proves - prose no parser can read. TestStage4951 fails the build
// when a route of a family has no note or a note names a route that no
// longer exists, so the catalogue cannot silently fall behind the registry.
const commandCatalogPath = "docs/security/command-catalog.md"

type commandFamily struct {
	Name  string
	Match *regexp.Regexp
}

// commandFamilies in matching order: a route belongs to the first family
// whose pattern matches. Only mutating methods are commands.
var commandFamilies = []commandFamily{
	{"Export", regexp.MustCompile(`/export`)},
	{"Credential / config change", regexp.MustCompile(`credentials|/admin/settings$|/admin/tenant/package|/admin/security/|/shopify/\{channel\}/settings`)},
	{"Loyalty", regexp.MustCompile(`loyalty`)},
	{"Tender", regexp.MustCompile(`/pos/payment/`)},
	{"Quote / discount / override", regexp.MustCompile(`/pos/(quote|offers/preview|price-override)|/rfq/`)},
	{"Return / refund / exchange", regexp.MustCompile(`/returns|/refunds|/return$|purchase-returns|/pos/exchange`)},
	{"Sale", regexp.MustCompile(`^POST /api/v1/(checkout$|pos/(held-bills|bills|offline-sales|advances|gift-card/issue|session))`)},
	{"Allocation / pick / ship", regexp.MustCompile(`/wms/(wave|pick-scan|short-pick|pack-scan|pack-complete|transfer/pack|bin-replenishment)|/oms/shipping-packages|/logistics/manifest|/couriers/\{provider\}/pickup|/dispatch$`)},
	{"Adjustment / count", regexp.MustCompile(`/inventory/adjustments|/cycle-count|/physical-inventory|/store-app/counts`)},
	{"Vendor / payment", regexp.MustCompile(`/vendor-invoice/|/payment-proposal|/vendor-credit-note`)},
	{"Journal / period", regexp.MustCompile(`/finance/(journal-voucher|periods|period-reopen)`)},
	{"Payroll", regexp.MustCompile(`payroll`)},
	{"Master merge", regexp.MustCompile(`/merge`)},
}

var mutatingMethod = regexp.MustCompile(`^(POST|PUT|PATCH|DELETE) `)

// commandFamilyOf names the family a registered pattern belongs to, or "".
func commandFamilyOf(pattern string) string {
	if !mutatingMethod.MatchString(pattern) {
		return ""
	}
	for _, f := range commandFamilies {
		if f.Match.MatchString(pattern) {
			return f.Name
		}
	}
	return ""
}

// commandNote is the hand-kept part of one command's row.
type commandNote struct {
	Inputs        string // authoritative inputs: what the server computes, the client may not set
	Preconditions string // state the command needs
	Invariants    string // what it proves, from the engine's own checks
	Idempotency   string // only when the code reads nothing
	Audit         string // only when the code reads nothing
}

const (
	noIdempotency = "none - a repeat is a second command"
	docRowOnly    = "none beyond the document row"
)

// commandNotes is keyed by the exact registered pattern.
var commandNotes = map[string]commandNote{
	// --- Sale ---------------------------------------------------------------
	"POST /api/v1/checkout": {
		Inputs:        "prices, offers, tax split, bill number, GL and stock postings, session; the client sends the cart number, tenders and an idempotency key",
		Preconditions: "an open till session; cart quoted; tenders cover the total; offline sales only on the till's day",
		Invariants:    "one bill per cart, all-or-nothing: stock, COGS, revenue, tax and tender post in one transaction or not at all; a replay returns the first result"},
	"POST /api/v1/pos/held-bills": {
		Inputs: "hold number, time, holder", Preconditions: "an open session; a quoted cart",
		Invariants: "a held bill reserves nothing and posts nothing until it is resumed through checkout"},
	"POST /api/v1/pos/held-bills/{id}/{action}": {
		Inputs: "the resumed cart's prices are re-quoted", Preconditions: "the held bill exists in this store and is still held",
		Invariants: "resume or discard once; a resumed hold cannot be resumed again"},
	"POST /api/v1/pos/bills/{id}/cancel": {
		Inputs: "the reversal's own number, reason stamp, GL and stock reversal", Preconditions: "a posted bill of this store, within the cancel window, reason given",
		Invariants: "the reversal mirrors the original exactly - stock back, GL reversed, tender refunded - and a bill cancels once"},
	"POST /api/v1/pos/offline-sales/{id}/validate": {
		Inputs: "server prices and tax are recomputed from the till's record; the bill number is the server's", Preconditions: "an offline sale uploaded by this till for its own session day",
		Invariants: "an offline sale is validated once per idempotency key; a different payload under the same key is refused"},
	"POST /api/v1/pos/offline-sales/{id}/reject": {
		Inputs: "rejection stamp and reason", Preconditions: "an uploaded, not yet validated offline sale",
		Invariants: "a rejected offline sale never posts; held offline cash stays in the drawer count"},
	"POST /api/v1/pos/advances": {
		Inputs: "advance number, GL posting (cash / clearing vs customer advance)", Preconditions: "an open session; a customer",
		Invariants: "the advance balance equals receipts minus applications minus refunds, never negative"},
	"POST /api/v1/pos/advances/refund": {
		Inputs: "GL reversal and the refund's tender row", Preconditions: "an advance with balance >= the refund",
		Invariants: "a refund cannot exceed the advance's remaining balance"},
	"POST /api/v1/pos/gift-card/issue": {
		Inputs: "card number, liability posting", Preconditions: "an open session; tender received",
		Invariants: "card balance = issued - redeemed; issuing posts the liability once"},
	"POST /api/v1/pos/session/open": {
		Inputs: "session id, opening time, opening float from the drawer", Preconditions: "no open session for this till and user; the user holds the till",
		Invariants: "one open session per till"},
	"POST /api/v1/pos/session/close": {
		Inputs: "expected cash from the session's own tenders and cash movements; variance", Preconditions: "an open session; counted cash given",
		Invariants: "expected cash is computed server-side; a variance over tolerance goes to approval, never silently accepted"},
	"POST /api/v1/pos/session/cash-movement": {
		Inputs: "movement stamp and running drawer balance", Preconditions: "an open session; reason given",
		Invariants: "the drawer balance never goes below zero on a payout"},
	"POST /api/v1/pos/session/takeover": {
		Inputs: "handover stamp, the counted drawer at takeover", Preconditions: "an open session held by another user; the taker holds the till",
		Invariants: "a session has one holder at a time; the takeover is written to the session's record"},

	// --- Quote / discount / override ----------------------------------------
	"POST /api/v1/pos/quote": {
		Inputs: "every price, offer, discount cap and tax line is computed; the client sends items, quantities and the customer", Preconditions: "sellable items of this store",
		Invariants: "a manual discount above the role's cap needs an approval or a manager override; totals are recomputed at checkout, never trusted from the quote"},
	"POST /api/v1/pos/offers/preview": {
		Inputs: "offer evaluation", Preconditions: "a quoted cart", Invariants: "a preview changes nothing; it is the same evaluation checkout will run",
		Idempotency: "read-only preview", Audit: "none (preview)"},
	"POST /api/v1/pos/price-override": {
		Inputs: "override stamp, approver, original price kept", Preconditions: "a manager's credential or approval; reason given",
		Invariants: "the original price and the overrider are kept on the line; an override is counted by the till-reversal detection"},
	"POST /api/v1/rfq/save": {
		Inputs: "RFQ number, status Draft", Preconditions: "items and vendors exist", Invariants: "a sent RFQ's lines do not change through save"},
	"POST /api/v1/rfq/{id}/invite": {
		Inputs: "invitation stamp per vendor", Preconditions: "a Draft or Open RFQ", Invariants: "a vendor is invited once per RFQ"},
	"POST /api/v1/rfq/{id}/quote": {
		Inputs: "quote stamp, the quoting vendor from the session's supplier link", Preconditions: "an invited vendor; the RFQ is open",
		Invariants: "a supplier session can quote only its own vendor's lines"},
	"POST /api/v1/rfq/select-quote": {
		Inputs: "selection stamp", Preconditions: "quotes received", Invariants: "one selected quote per line"},
	"POST /api/v1/rfq/{id}/award": {
		Inputs: "award stamp, the resulting PO's number and prices from the quote", Preconditions: "a selected quote",
		Invariants: "the PO carries the awarded quote's prices, not the request's"},
	"POST /api/v1/rfq/{id}/decline": {
		Inputs: "decline stamp", Preconditions: "an invited vendor", Invariants: "a declined vendor cannot quote"},

	// --- Tender -------------------------------------------------------------
	"POST /api/v1/pos/payment/confirm": {
		Inputs: "payment state, terminal reference, settlement row", Preconditions: "a payment in a state the state machine allows to confirm (CanTransitionPaymentState)",
		Invariants: "a tender confirms once; the confirmed amount equals the requested amount"},
	"POST /api/v1/pos/payment/void": {
		Inputs: "void stamp", Preconditions: "a payment not yet settled on a bill", Invariants: "a voided tender never counts toward a bill"},

	// --- Loyalty ------------------------------------------------------------
	"POST /api/v1/loyalty/redeem": {
		Inputs: "points value, liability release, redemption number", Preconditions: "a customer with enough points; a bill to apply to",
		Invariants: "points redeemed never exceed the balance; the liability release equals the points' value"},
	"POST /api/v1/pos/loyalty/redemption/start": {
		Inputs: "one-time code, expiry", Preconditions: "a customer with points; an open cart", Invariants: "one open redemption per cart"},
	"POST /api/v1/pos/loyalty/redemption/verify": {
		Inputs: "verification result", Preconditions: "a started redemption, code unexpired", Invariants: "a code verifies once; a wrong code does not consume points"},
	"POST /api/v1/crm/loyalty-redemption/initiate": {
		Inputs: "one-time code, expiry", Preconditions: "a customer with points", Invariants: "one open redemption per customer"},
	"POST /api/v1/crm/loyalty-redemption/verify": {
		Inputs: "verification result, points deducted", Preconditions: "an initiated redemption, code unexpired", Invariants: "points deducted once, never over the balance"},

	// --- Return / refund / exchange ------------------------------------------
	"POST /api/v1/returns": {
		Inputs: "return number, refund value from the original bill's lines, idempotency key (default type:order:booking)", Preconditions: "an original order or bill; quantities within what was sold minus already returned",
		Invariants: "one return request per key; a repeat returns the first result; quantities never exceed the original"},
	"POST /api/v1/returns/{id}/approve": {
		Inputs: "approval stamp, approver", Preconditions: "a Requested return; the approver is not the requester where SoD applies", Invariants: "approval once; a returned request is not approved twice"},
	"POST /api/v1/returns/{id}/reject": {
		Inputs: "rejection stamp", Preconditions: "a Requested return", Invariants: "a rejected return releases nothing and refunds nothing"},
	"POST /api/v1/returns/{id}/receive": {
		Inputs: "receipt stamp, received quantities", Preconditions: "an Approved return", Invariants: "received never exceeds approved"},
	"POST /api/v1/returns/{id}/qc": {
		Inputs: "QC disposition per line, stock posting to Sellable / Damaged", Preconditions: "a Received return", Invariants: "stock comes back exactly once, in the disposition's state"},
	"POST /api/v1/returns/{id}/reverse-pickup": {
		Inputs: "pickup booking reference", Preconditions: "an Approved return", Invariants: "one pickup per return"},
	"POST /api/v1/refunds/{id}/approve": {
		Inputs: "approval stamp", Preconditions: "a Pending refund; approver is not the requester", Invariants: "a refund is approved once"},
	"POST /api/v1/refunds/{id}/process": {
		Inputs: "refund posting (GL, tender), processing stamp", Preconditions: "an Approved refund", Invariants: "a refund processes once; its amount equals the approved amount"},
	"POST /api/v1/refunds/{id}/reject": {
		Inputs: "rejection stamp", Preconditions: "a Pending refund", Invariants: "a rejected refund pays nothing"},
	"POST /api/v1/fulfillment/return": {
		Inputs: "return number, stock posting", Preconditions: "a shipped order", Invariants: "returned quantity never exceeds shipped"},
	"POST /api/v1/pos/exchange/start": {
		Inputs: "exchange cart with the return credit", Preconditions: "an original bill of this store", Invariants: "the credit equals the returned lines' value; one exchange per returned line"},
	"POST /api/v1/inventory/challans/{id}/return": {
		Inputs: "return stamp, stock back at the issuing location", Preconditions: "a dispatched challan", Invariants: "returned never exceeds dispatched"},
	"POST /api/v1/inventory/material-requests/{id}/return": {
		Inputs: "return stamp, stock back", Preconditions: "an issued material request", Invariants: "returned never exceeds issued"},
	"POST /api/v1/transfer/return": {
		Inputs: "return stamp, in-transit reversal", Preconditions: "a dispatched transfer not yet received", Invariants: "in-transit stock returns to the source once"},
	"POST /api/v1/procurement/purchase-returns/{id}/post": {
		Inputs: "debit note number, GL and stock posting", Preconditions: "a Draft purchase return against a received GRN", Invariants: "returned never exceeds received; the debit note equals the returned value; posts once"},

	// --- Allocation / pick / ship --------------------------------------------
	"POST /api/v1/wms/wave/create": {
		Inputs: "wave number, task list", Preconditions: "allocated orders", Invariants: "an order is in one wave at a time"},
	"POST /api/v1/wms/wave/from-selection": {
		Inputs: "wave number", Preconditions: "selected allocated orders", Invariants: "an order is in one wave at a time"},
	"POST /api/v1/wms/wave/template/run": {
		Inputs: "wave number from the template's selection", Preconditions: "a template", Invariants: "an order is in one wave at a time"},
	"POST /api/v1/wms/wave/assign": {
		Inputs: "assignment stamp", Preconditions: "a wave not yet complete", Invariants: "one picker per wave task"},
	"POST /api/v1/wms/wave/transition": {
		Inputs: "status stamp", Preconditions: "the transition the wave status machine allows", Invariants: "a wave's status moves forward only"},
	"POST /api/v1/wms/wave/pick-confirm": {
		Inputs: "picked quantity per task, bin stock movement", Preconditions: "an assigned wave task", Invariants: "picked never exceeds the task's quantity; bin stock moves once"},
	"POST /api/v1/wms/pick-scan": {
		Inputs: "pick progress, bin stock movement", Preconditions: "a Pending or InProgress fulfilment task; the scanned SKU is on it", Invariants: "picked never exceeds allocated; a scan on a completed task is refused"},
	"POST /api/v1/wms/short-pick": {
		Inputs: "shortage stamp, reallocation", Preconditions: "a task in picking", Invariants: "short quantity never exceeds remaining; the shortage is recorded, not silently dropped"},
	"POST /api/v1/wms/pack-scan": {
		Inputs: "pack progress", Preconditions: "a picked task", Invariants: "packed never exceeds picked"},
	"POST /api/v1/wms/pack-complete": {
		Inputs: "package number, status Packed", Preconditions: "every picked line packed", Invariants: "a task completes packing once"},
	"POST /api/v1/wms/transfer/pack": {
		Inputs: "package number", Preconditions: "a transfer picked", Invariants: "packed never exceeds picked"},
	"POST /api/v1/wms/bin-replenishment/execute": {
		Inputs: "bin movements", Preconditions: "a replenishment plan", Invariants: "bin stock moves once per plan line; totals on hand unchanged"},
	"POST /api/v1/wms/bin-replenishment/dynamic-pickface/apply": {
		Inputs: "pick-face assignment", Preconditions: "a computed suggestion", Invariants: "one pick face per SKU per zone"},
	"POST /api/v1/oms/shipping-packages": {
		Inputs: "package number, weight class", Preconditions: "a packed task", Invariants: "a package belongs to one order"},
	"POST /api/v1/oms/shipping-packages/{id}/update": {
		Inputs: "update stamp", Preconditions: "a package not yet shipped", Invariants: "a shipped package's contents do not change"},
	"POST /api/v1/oms/shipping-packages/{id}/split": {
		Inputs: "new package numbers", Preconditions: "a package not yet shipped", Invariants: "the parts sum to the original"},
	"POST /api/v1/oms/shipping-packages/{id}/invoice": {
		Inputs: "OMS bill number, tax split by place of supply, GL and stock posting", Preconditions: "a Packed package; the status rule allows Invoiced (ValidateStatusTransition)", Invariants: "one bill per package; the bill posts once"},
	"POST /api/v1/oms/shipping-packages/{id}/cancel": {
		Inputs: "cancel stamp, reservation release, reversal of the bill if invoiced", Preconditions: "the status rule allows Cancelled; reason code", Invariants: "stock reserved by the package is released exactly once"},
	"POST /api/v1/marketplace/logistics/manifest": {
		Inputs: "manifest number", Preconditions: "booked packages of one courier", Invariants: "a package is on one manifest"},
	"POST /api/v1/marketplace/logistics/manifest/handover": {
		Inputs: "handover stamp, the sales invoice posted from the package, stock out", Preconditions: "a manifest with booked packages", Invariants: "handover posts each package's invoice once; on hand falls by the shipped quantity"},
	"POST /api/v1/marketplace/couriers/{provider}/pickup": {
		Inputs: "pickup reference from the courier", Preconditions: "a manifest; courier credentials", Invariants: "one pickup request per manifest"},
	"POST /api/v1/transfer/dispatch": {
		Inputs: "dispatch stamp, in-transit posting", Preconditions: "an approved transfer; stock available at source", Invariants: "dispatched never exceeds requested; source stock falls and in-transit rises by the same quantity"},
	"POST /api/v1/inventory/challans/{id}/dispatch": {
		Inputs: "challan number, stock out at the issuing location", Preconditions: "a Draft delivery challan with stock available", Invariants: "dispatched never exceeds available; a challan dispatches once"},

	// --- Adjustment / count --------------------------------------------------
	"POST /api/v1/inventory/adjustments": {
		Inputs: "adjustment number, status Draft, valuation at current cost", Preconditions: "items and location exist; reason code", Invariants: "a Draft adjustment moves no stock"},
	"POST /api/v1/inventory/adjustments/preview": {
		Inputs: "projected stock and value", Preconditions: "lines given", Invariants: "a preview changes nothing", Idempotency: "read-only preview", Audit: "none (preview)"},
	"POST /api/v1/inventory/adjustments/{id}/post": {
		Inputs: "stock movement, GL posting at cost, posting stamp", Preconditions: "a Draft adjustment; approval where a rule matches; stock never below zero unless allowed", Invariants: "the stock movement and the GL entry post together once (RunEconomicCommand); the GL value equals quantity x cost"},
	"POST /api/v1/inventory/adjustments/{id}/reverse": {
		Inputs: "the reversal's own number and postings", Preconditions: "a Posted adjustment", Invariants: "a reversal mirrors the original and happens once"},
	"POST /api/v1/wms/cycle-count/variance-reason": {
		Inputs: "reason stamp", Preconditions: "a counted task with a variance", Invariants: "a variance over tolerance needs a reason before it can post"},
	"POST /api/v1/wms/cycle-count/recount/request": {
		Inputs: "recount task", Preconditions: "a counted task", Invariants: "one open recount per task"},
	"POST /api/v1/wms/cycle-count/recount/submit": {
		Inputs: "recount result", Preconditions: "an open recount", Invariants: "the recount replaces the count, never adds to it"},
	"POST /api/v1/wms/cycle-count/reconcile": {
		Inputs: "variance per bin", Preconditions: "all counts submitted", Invariants: "variance = counted - system, per bin"},
	"POST /api/v1/wms/cycle-count/post-adjustment": {
		Inputs: "adjustment number, stock and GL posting", Preconditions: "a reconciled count; reasons on variances", Invariants: "the adjustment equals the reconciled variance and posts once"},
	"POST /api/v1/wms/physical-inventory/start": {
		Inputs: "count id, freeze stamp", Preconditions: "no open count for the location", Invariants: "one open physical inventory per location"},
	"POST /api/v1/wms/physical-inventory/submit-count": {
		Inputs: "count stamp", Preconditions: "an open count", Invariants: "a bin is counted once per sheet"},
	"POST /api/v1/wms/physical-inventory/reconcile": {
		Inputs: "variance per bin", Preconditions: "every sheet submitted", Invariants: "variance = counted - system"},
	"POST /api/v1/wms/physical-inventory/close": {
		Inputs: "adjustment postings, close stamp", Preconditions: "a reconciled count", Invariants: "closing posts the variance once; a closed count cannot reopen"},
	"POST /api/v1/wms/physical-inventory/cancel": {
		Inputs: "cancel stamp", Preconditions: "an open count", Invariants: "a cancelled count posts nothing"},
	"POST /api/v1/store-app/counts": {
		Inputs: "count stamp, store from the session", Preconditions: "the user's own store", Invariants: "a store count is a record, not a posting; it posts only through the adjustment"},

	// --- Vendor / payment ----------------------------------------------------
	"POST /api/v1/procurement/vendor-invoice/match": {
		Inputs: "match result, MismatchHold status", Preconditions: "a vendor invoice and its GRN / PO", Invariants: "a bill matches one GRN; a second bill for the same goods is held, not paid"},
	"POST /api/v1/procurement/vendor-invoice/pay": {
		Inputs: "payment number, GL posting (payable vs bank), paid amount on the bill", Preconditions: "a matched, unpaid bill; approval where a rule matches; the vendor's bank not changed since (vendor_bank_then_paid detection)", Invariants: "paid never exceeds the invoice; a bill is paid once; posted money is frozen after"},
	"POST /api/v1/procurement/vendor-invoice/pay-with-tds": {
		Inputs: "TDS amount from the section rate, net payment, two GL legs", Preconditions: "as pay; a TDS section", Invariants: "TDS + net = invoice; the TDS liability posts with the payment"},
	"POST /api/v1/finance/payment-proposal": {
		Inputs: "proposal number, selected bills by due date", Preconditions: "open payables", Invariants: "a bill is in one open proposal"},
	"POST /api/v1/finance/payment-proposal/{id}/execute": {
		Inputs: "payment numbers, GL postings", Preconditions: "an Approved proposal; approver is not the preparer", Invariants: "every bill in the proposal is paid once, for its proposed amount"},
	"POST /api/v1/finance/payment-proposal/{id}/record-utr": {
		Inputs: "UTR stamp", Preconditions: "an executed proposal", Invariants: "one UTR per payment"},
	"POST /api/v1/finance/vendor-credit-note/{id}/post": {
		Inputs: "GL posting, posting stamp", Preconditions: "a Draft vendor credit note against a bill", Invariants: "the note never exceeds the bill's open amount; posts once; posted money is frozen after"},

	// --- Journal / period ----------------------------------------------------
	"POST /api/v1/finance/journal-voucher": {
		Inputs: "voucher number, posting stamp, period check", Preconditions: "an open accounting period; approval where a rule matches", Invariants: "debits equal credits; a voucher posts once; posted lines are frozen after"},
	"POST /api/v1/finance/journal-voucher/recurring": {
		Inputs: "schedule and the generated vouchers' numbers", Preconditions: "a balanced template", Invariants: "each occurrence posts once, in its own period"},
	"POST /api/v1/finance/journal-voucher/{id}/retry-post": {
		Inputs: "posting stamp", Preconditions: "a voucher whose posting failed", Invariants: "a retry posts once; a posted voucher is not posted again"},
	"POST /api/v1/finance/journal-voucher/{id}/reverse": {
		Inputs: "the reversal voucher's number and lines", Preconditions: "a Posted voucher; an open period", Invariants: "the reversal mirrors the original; a voucher reverses once"},
	"POST /api/v1/finance/periods": {
		Inputs: "period id", Preconditions: "no overlap with an existing period", Invariants: "periods do not overlap"},
	"POST /api/v1/finance/periods/{id}/close": {
		Inputs: "close stamp, closing balances", Preconditions: "every close task done; no unposted document in the period", Invariants: "a closed period accepts no posting dated in it"},
	"POST /api/v1/finance/periods/{id}/close-tasks/{task}/{action}": {
		Inputs: "task stamp", Preconditions: "an open period", Invariants: "a task is done once; the close needs every task"},
	"POST /api/v1/finance/periods/{id}/reopen-request": {
		Inputs: "request stamp", Preconditions: "a closed period; reason", Invariants: "a reopen needs a second person (period_reopened detection)"},
	"POST /api/v1/finance/period-reopen/{request}/apply": {
		Inputs: "reopen stamp", Preconditions: "an approved request; approver is not the requester", Invariants: "reopening is recorded; the period closes again through the same tasks"},

	// --- Payroll -------------------------------------------------------------
	"POST /api/v1/hr/run-payroll": {
		Inputs: "every payslip's gross, PF, ESI, PT, TDS, loan EMI, net and the GL posting", Preconditions: "attendance and salary structures for the month; approval where a rule matches", Invariants: "net = gross - statutory - deductions to the paise; a month runs once per employee; each payslip's GL balances"},

	// --- Master merge --------------------------------------------------------
	"POST /api/v1/crm/customer/merge": {
		Inputs: "survivor id, re-pointed documents, merge record", Preconditions: "two customers; the survivor named", Invariants: "every document of the merged customer points at the survivor; balances and points are added, not lost"},
	"POST /api/v1/crm/merges": {
		Inputs: "merge request record", Preconditions: "two customers", Invariants: "a merge is applied once per request"},

	// --- Credential / config change ------------------------------------------
	"PUT /api/v1/admin/settings": {
		Inputs: "each key validated against the settings registry; each change stamped", Preconditions: "an administrator", Invariants: "only registered keys change; every key = value is audited (config_change_burst detection)"},
	"POST /api/v1/admin/tenant/package": {
		Inputs: "module entitlements from the package", Preconditions: "an administrator", Invariants: "core modules stay on; dependents follow their prerequisite"},
	"POST /api/v1/admin/api-credentials": {
		Inputs: "credential id, the secret shown once, scopes", Preconditions: "an administrator", Invariants: "the secret is stored hashed; scopes never exceed the issuer's"},
	"POST /api/v1/integrations/credentials/disconnect": {
		Inputs: "the connector kind and its code; who disconnected and when", Preconditions: "an administrator; a saved credential for that connector",
		Invariants: "the credential is deleted and its pending / leased jobs cancelled; mappings and settings stay; disconnecting nothing is refused"},
	"POST /api/v1/admin/api-credentials/{id}/rotate": {
		Inputs: "new secret shown once", Preconditions: "an existing credential", Invariants: "the old secret stops working"},
	"PUT /api/v1/admin/api-credentials/{id}/limits": {
		Inputs: "rate limits", Preconditions: "an existing credential", Invariants: "limits never exceed the platform ceiling"},
	"DELETE /api/v1/admin/api-credentials/{id}": {
		Inputs: "revocation stamp", Preconditions: "an existing credential", Invariants: "a revoked credential never authenticates again"},
	"POST /api/v1/admin/security/freeze-exports": {
		Inputs: "the freeze setting and its stamp", Preconditions: "an administrator", Invariants: "while frozen every bulk export is refused at the middleware"},
	"POST /api/v1/admin/security/sign-out-all": {
		Inputs: "session revocation stamp", Preconditions: "an administrator", Invariants: "every session of the client is dead after"},
	"POST /api/v1/admin/security/sign-out-user": {
		Inputs: "session revocation stamp", Preconditions: "an administrator; a user", Invariants: "every session of that user is dead after"},
	"POST /api/v1/admin/security/detections/run": {
		Inputs: "detection sweep", Preconditions: "an administrator", Invariants: "a sweep writes one SECURITY_ALERT per fired rule, deduplicated per window",
		Idempotency: "dedup per rule and subject within the rule's window"},
	"POST /api/v1/shopify/{channel}/credentials": {
		Inputs: "the secret stored encrypted, write-only", Preconditions: "a channel of this client", Invariants: "a stored secret is never returned"},
	"POST /api/v1/shopify/{channel}/settings": {
		Inputs: "settings stamp", Preconditions: "a channel of this client", Invariants: "sync settings change only through this route"},
	"POST /api/v1/marketplace/channels/{channel}/credentials": {
		Inputs: "the secret stored encrypted, write-only", Preconditions: "a channel of this client", Invariants: "a stored secret is never returned"},
	"POST /api/v1/marketplace/couriers/{provider}/credentials": {
		Inputs: "the secret stored encrypted, write-only", Preconditions: "a provider the ERP knows", Invariants: "a stored secret is never returned"},
	"POST /api/v1/pim/channels/{code}/credentials": {
		Inputs: "the secret stored encrypted, write-only", Preconditions: "a PIM channel", Invariants: "a stored secret is never returned"},
	"POST /api/v1/pinelabs/credentials": {
		Inputs: "the secret stored encrypted, write-only", Preconditions: "an administrator", Invariants: "a stored secret is never returned"},
	"POST /api/v1/clevertap/credentials": {
		Inputs: "the secret stored encrypted, write-only", Preconditions: "an administrator", Invariants: "a stored secret is never returned"},
	"POST /api/v1/unicommerce/credentials": {
		Inputs: "the secret stored encrypted, write-only", Preconditions: "an administrator", Invariants: "a stored secret is never returned"},

	// --- Export --------------------------------------------------------------
	"POST /api/v1/reports/export": {
		Inputs: "export id, the job's row cap from the requesting client, masking by role", Preconditions: "a registered report; exports not frozen; the role may read the report", Invariants: "rows and totals in the file equal the on-screen report; sensitive columns are masked by the same policy"},
	"POST /api/v1/reports/export/{id}/cancel": {
		Inputs: "cancel stamp", Preconditions: "the caller's own running export", Invariants: "a cancelled export delivers nothing", Idempotency: "cancel is idempotent", Audit: "none (not an export)"},
	"POST /api/v1/dashboards/export": {
		Inputs: "the dataset computed server-side, masked by role", Preconditions: "exports not frozen", Invariants: "the file holds what the dashboard shows for this role"},
	"POST /api/v1/pim/export-templates/{id}/run": {
		Inputs: "the feed's columns from the template's field maps", Preconditions: "a template; exports not frozen", Invariants: "every column comes through a template field or a field map; a field appears in one column"},
}

// --- code facts -----------------------------------------------------------

type routeFacts struct {
	Handler          string
	Module           string
	AuditRows        []string
	Idempotency      []string
	ApprovalGated    bool
	StatusTransition bool
}

type codeFacts struct {
	available bool
	routes    map[string]routeFacts
}

type funcIndex struct {
	server  map[string]*ast.FuncDecl
	engines map[string]*ast.FuncDecl
}

var (
	handleFuncLine = regexp.MustCompile(`http\.HandleFunc\("([^"]+)",(.*)$`)
	handlerIdent   = regexp.MustCompile(`\b(handle[A-Za-z0-9_]+)\b`)
	moduleGateOne  = regexp.MustCompile(`moduleGate\("([a-z_]+)"`)
	moduleGateAny  = regexp.MustCompile(`moduleGateAny\(\[\]string\{([^}]*)\}`)
)

// readCommandFacts parses routes.go and the two packages' function bodies.
// A source tree without them (a fixture root) yields no facts, not an error.
func readCommandFacts(root string) codeFacts {
	routesSrc, err := os.ReadFile(filepath.Join(root, "internal", "server", "routes.go"))
	if err != nil {
		return codeFacts{}
	}
	idx := funcIndex{server: parseFuncs(filepath.Join(root, "internal", "server")), engines: parseFuncs(filepath.Join(root, "engines"))}
	facts := codeFacts{available: true, routes: map[string]routeFacts{}}
	for _, line := range strings.Split(strings.ReplaceAll(string(routesSrc), "\r\n", "\n"), "\n") {
		m := handleFuncLine.FindStringSubmatch(line)
		if m == nil {
			continue
		}
		pattern, rest := m[1], m[2]
		rf := routeFacts{}
		if names := handlerIdent.FindAllString(rest, -1); len(names) > 0 {
			rf.Handler = names[len(names)-1]
		}
		if g := moduleGateOne.FindStringSubmatch(rest); g != nil {
			rf.Module = g[1]
		} else if g := moduleGateAny.FindStringSubmatch(rest); g != nil {
			rf.Module = strings.ReplaceAll(strings.ReplaceAll(g[1], "\"", ""), " ", "")
		}
		if rf.Handler != "" {
			c := &factCollector{idx: idx, visited: map[string]bool{}}
			c.collect("server", rf.Handler, 0)
			rf.AuditRows, rf.Idempotency = sortedSet(c.audit), sortedSet(c.idem)
			rf.ApprovalGated, rf.StatusTransition = c.approval, c.transition
		}
		facts.routes[pattern] = rf
	}
	return facts
}

func parseFuncs(dir string) map[string]*ast.FuncDecl {
	out := map[string]*ast.FuncDecl{}
	files, _ := filepath.Glob(filepath.Join(dir, "*.go"))
	fset := token.NewFileSet()
	for _, f := range files {
		if strings.HasSuffix(f, "_test.go") {
			continue
		}
		file, err := parser.ParseFile(fset, f, nil, 0)
		if err != nil {
			continue
		}
		for _, d := range file.Decls {
			if fd, ok := d.(*ast.FuncDecl); ok && fd.Recv == nil && fd.Body != nil {
				out[fd.Name.Name] = fd
			}
		}
	}
	return out
}

// factCollector walks a handler and the functions it calls, two levels
// deep, for the calls that are facts about the command.
type factCollector struct {
	idx        funcIndex
	visited    map[string]bool
	audit      map[string]bool
	idem       map[string]bool
	approval   bool
	transition bool
}

const factDepth = 2

func (c *factCollector) collect(pkg, name string, depth int) {
	key := pkg + "." + name
	if c.visited[key] || depth > factDepth {
		return
	}
	c.visited[key] = true
	var fd *ast.FuncDecl
	if pkg == "server" {
		fd = c.idx.server[name]
	} else {
		fd = c.idx.engines[name]
	}
	if fd == nil {
		return
	}
	ast.Inspect(fd.Body, func(n ast.Node) bool {
		switch v := n.(type) {
		case *ast.Field:
			if depth == 0 && v.Tag != nil && strings.Contains(v.Tag.Value, `json:"idempotency_key"`) {
				c.note(&c.idem, "client idempotency_key")
			}
		case *ast.CallExpr:
			callee, calleePkg := "", pkg
			switch fn := v.Fun.(type) {
			case *ast.Ident:
				callee = fn.Name
			case *ast.SelectorExpr:
				if x, ok := fn.X.(*ast.Ident); ok && x.Name == "engines" {
					callee, calleePkg = fn.Sel.Name, "engines"
				}
			}
			if callee == "" {
				return true
			}
			c.fact(callee, v)
			c.collect(calleePkg, callee, depth+1)
		}
		return true
	})
}

func (c *factCollector) note(set *map[string]bool, s string) {
	if *set == nil {
		*set = map[string]bool{}
	}
	(*set)[s] = true
}

func (c *factCollector) fact(callee string, call *ast.CallExpr) {
	switch callee {
	case "LogAuditEvent", "LogAuditEventCtx":
		off := 2
		if callee == "LogAuditEventCtx" {
			off = 3
		}
		if len(call.Args) > off+1 {
			action, status := literalOr(call.Args[off], "(dynamic)"), literalOr(call.Args[off+1], "(dynamic)")
			if action == "(dynamic)" && status == "Error" {
				// writeAPIError: a refused request whose catalog entry is
				// AuditRequired is logged under its own error code.
				c.note(&c.audit, "refusal: <error code> / Error (AuditRequired entries)")
				return
			}
			c.note(&c.audit, action+" / "+status)
		}
	case "ClaimCommand":
		if len(call.Args) > 1 {
			c.note(&c.idem, "ClaimCommand("+literalOr(call.Args[1], "dynamic")+")")
		}
	case "RunEconomicCommand":
		name := "dynamic"
		for _, a := range call.Args {
			if cl, ok := a.(*ast.CompositeLit); ok {
				for _, e := range cl.Elts {
					if kv, ok := e.(*ast.KeyValueExpr); ok {
						if k, ok := kv.Key.(*ast.Ident); ok && k.Name == "Command" {
							name = literalOr(kv.Value, "dynamic")
						}
					}
				}
			}
		}
		c.note(&c.idem, "RunEconomicCommand("+name+")")
	case "BeginIdempotentRequest":
		c.note(&c.idem, "Idempotency-Key header (public API)")
	case "IsApprovalGated", "SubmitForApproval", "RequireApprovalIfGated":
		c.approval = true
	case "ValidateStatusTransition":
		c.transition = true
	}
}

func literalOr(e ast.Expr, fallback string) string {
	if lit, ok := e.(*ast.BasicLit); ok && lit.Kind == token.STRING {
		return strings.Trim(lit.Value, "`\"")
	}
	return fallback
}

func sortedSet(m map[string]bool) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// --- the page ---------------------------------------------------------------

// commandCatalogRoutes groups the registered mutating routes by family.
func commandCatalogRoutes() map[string][]string {
	out := map[string][]string{}
	for _, p := range server.RegisteredRoutePatterns() {
		if f := commandFamilyOf(p); f != "" {
			out[f] = append(out[f], p)
		}
	}
	return out
}

// commandCatalogProblems is what TestStage4951 refuses: a family route with
// no note, a note for a route that is not registered.
func commandCatalogProblems() []string {
	var problems []string
	registered := map[string]bool{}
	for _, routes := range commandCatalogRoutes() {
		for _, p := range routes {
			registered[p] = true
			if _, ok := commandNotes[p]; !ok {
				problems = append(problems, "no catalogue note for "+p)
			}
		}
	}
	for p := range commandNotes {
		if !registered[p] {
			problems = append(problems, "note for a route that is not a registered command of any family: "+p)
		}
	}
	sort.Strings(problems)
	return problems
}

func commandCatalogFiles(root, stamp string) map[string][]byte {
	return map[string][]byte{commandCatalogPath: []byte(commandCatalogDoc(stamp, readCommandFacts(root)))}
}

func commandCatalogDoc(stamp string, facts codeFacts) string {
	var b strings.Builder
	b.WriteString("---\n" +
		"doc_id: SEC-CMD-001\n" +
		"title: Command catalog\n" +
		"type: generated\n" +
		"status: active\n" +
		"owner: security-owner\n" +
		"authority: projection\n" +
		"confidentiality: internal\n" +
		"last_verified: " + stamp + "\n" +
		"---\n\n")
	b.WriteString(generatedHeader("Command catalog", stamp,
		"`internal/server/routes.go` and `route_capabilities.go`, the handlers and engines they call (`go/ast`), and the hand-kept notes in `cmd/gendocs/command_catalog.go`",
		"`pwsh docs/update-docs.ps1 -Group Content`"))
	b.WriteString("Stage 49.5.1. Every command of the families below, one row each. " +
		"**Actor** is the route registry's access level, capability and role allowlist, plus the module gate. " +
		"**Authoritative inputs** are what the server computes and the client may not set. " +
		"**Preconditions** are the state the command needs; *approval rule may gate it* and *status transition rule* are read from the handler. " +
		"**Invariants** are what the command proves, from the engine's own checks (hand-kept, reviewed with the engine). " +
		"**Idempotency** and **Audit row** are read from the handler and the engine calls under it, two calls deep: `ClaimCommand(name)` is the command idempotency spine (`engines/command_idempotency.go`), " +
		"`RunEconomicCommand(name)` the all-or-nothing posting spine, *client idempotency_key* a key the request carries; `DATA_EXPORT / SUCCESS` is written by `apiMiddleware` for every bulk export. " +
		"`docs/security/idempotency-audit.md` is the yes / no view of the same routes.\n\n")
	if !facts.available {
		b.WriteString("> Code facts were not available to this run (no source tree); actor, idempotency and audit columns show the registry and the notes only.\n\n")
	}
	families := commandCatalogRoutes()
	total := 0
	for _, routes := range families {
		total += len(routes)
	}
	fmt.Fprintf(&b, "| Family | Commands |\n|---|---|\n")
	for _, f := range commandFamilies {
		fmt.Fprintf(&b, "| %s | %d |\n", f.Name, len(families[f.Name]))
	}
	fmt.Fprintf(&b, "| **Total** | **%d** |\n\n", total)

	for _, f := range commandFamilies {
		routes := families[f.Name]
		if len(routes) == 0 {
			continue
		}
		fmt.Fprintf(&b, "## %s\n\n| Command | Actor | Authoritative inputs | Preconditions | Invariants | Idempotency | Audit row |\n|---|---|---|---|---|---|---|\n", f.Name)
		for _, p := range routes {
			note := commandNotes[p]
			rf := facts.routes[p]
			fmt.Fprintf(&b, "| `%s` | %s | %s | %s | %s | %s | %s |\n",
				p, mdCell(actorCell(p, rf)), mdCell(orDash(note.Inputs)), mdCell(preconditionCell(note, rf)),
				mdCell(orDash(note.Invariants)), mdCell(idempotencyCell(note, rf)), mdCell(auditCell(p, note, rf)))
		}
		b.WriteString("\n")
	}
	return b.String()
}

func orDash(s string) string {
	if strings.TrimSpace(s) == "" {
		return "-"
	}
	return s
}

func actorCell(pattern string, rf routeFacts) string {
	level, capability, ok := server.RouteClassificationFor(pattern)
	if !ok {
		return "unclassified"
	}
	s := level + " · " + capability
	if roles, restricted := server.CapabilityRoleAllowlistFor(capability); restricted {
		s += " (" + strings.Join(roles, ", ") + ")"
	}
	if rf.Module != "" {
		s += "; module " + rf.Module
	}
	return s
}

func preconditionCell(note commandNote, rf routeFacts) string {
	parts := []string{}
	if strings.TrimSpace(note.Preconditions) != "" {
		parts = append(parts, note.Preconditions)
	}
	if rf.ApprovalGated {
		parts = append(parts, "*approval rule may gate it*")
	}
	if rf.StatusTransition {
		parts = append(parts, "*status transition rule*")
	}
	return orDash(strings.Join(parts, "; "))
}

func idempotencyCell(note commandNote, rf routeFacts) string {
	if len(rf.Idempotency) > 0 {
		return strings.Join(rf.Idempotency, "; ")
	}
	if strings.TrimSpace(note.Idempotency) != "" {
		return note.Idempotency
	}
	return noIdempotency
}

func auditCell(pattern string, note commandNote, rf routeFacts) string {
	parts := append([]string(nil), rf.AuditRows...)
	if server.IsBulkExportPattern(pattern) {
		parts = append(parts, "DATA_EXPORT / SUCCESS (apiMiddleware)")
	}
	if rf.ApprovalGated {
		parts = append(parts, "approval_log (approval engine)")
	}
	if len(parts) == 0 {
		if strings.TrimSpace(note.Audit) != "" {
			return note.Audit
		}
		return docRowOnly
	}
	return strings.Join(parts, "; ")
}
