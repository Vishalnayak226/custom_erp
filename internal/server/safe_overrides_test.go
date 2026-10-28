package server

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"testing"
)

// Stage 49.5.6 - safe overrides. "No boolean 'force' field creates a general
// bypass": every request field, document key or function flag whose name
// says it lifts a control (force, override, bypass, skip, ignore, unsafe,
// allow_negative) is listed here with the capability that gates it, the
// reason it captures and the audit row it leaves - or why it is not a
// control at all. A new one fails this test until it is reviewed and listed;
// an entry whose code is gone fails it too, so the list stays the truth.

type overrideEntry struct {
	// A control override: all three must be named.
	Gate, Reason, Audit string
	// Or: why this name is not a control override.
	NotAControl string
	// Or: a known gap waiting for a product decision (49.5.6 is not done
	// while any remain) - the test passes and lists them.
	Open string
}

// Keys are "file|kind|name": kind json (a request/response field), key (a
// document or payload key read or written by name) or bool (a bool struct
// field or parameter).
var reviewedOverrides = func() map[string]overrideEntry {
	industry := overrideEntry{Gate: "admin.industry capability (requireAdminCapability) on POST switch-industry",
		Reason: "override_reason required once the profile is locked", Audit: "SWITCH_INDUSTRY row marked [OVERRIDE ... reason]; industry_lock.override_count"}
	priceOverride := overrideEntry{Gate: "pos.price_override capability; past the limit it waits for approval (POSPriceOverride, maker-checker)",
		Reason: "reason or reason code required (ReasonForException)", Audit: "POS_PRICE_OVERRIDE row; till_reversal_burst detection"}
	payOverride := overrideEntry{Gate: "routed to the approval role (approval_log Submitted); the requester cannot approve their own",
		Reason: "override_reason required", Audit: "PAY_VENDOR_INVOICE_OVERRIDE_SUBMITTED / _FINALIZED rows"}
	backflushRule := overrideEntry{NotAControl: "a Backflush Rule master setting (72.4): whether the till may swap a packaging component - the gate itself, enforced by POSOFF-0415"}
	audience := overrideEntry{NotAControl: "a campaign audience filter (leave out customers who already bought), not a control"}
	// Stage 49.5.6a: offline_synced still lifts the stock floor, the packaging-stock
	// block, the salesman check and the high-value cash check - but only for a sale
	// a manager validated; the claim itself only holds the sale (pos_offline_sales).
	offline := overrideEntry{Gate: "pos.offline_validate capability on POS > Offline Sales: a checkout claiming offline_synced is only held, and posts when a manager other than its cashier validates it (handleValidateOfflineSale replays it through handleCheckout)",
		Reason: "every line scanned with one of that item's ERP barcodes (POSOFF-1361); a rejection needs its reason (POSOFF-1362)",
		Audit:  "POS_OFFLINE_SALE_VALIDATED / POS_OFFLINE_SALE_REJECTED rows; offline_sale_burst detection over every held sale; a POSOfflineSyncVariance record per shortfall"}
	interstate := overrideEntry{NotAControl: "no longer an override (49.5.6b): the supply type is always derived from the vendor's and the store's states, a vendor with no state is refused, and ApplyPlaceOfSupply stores this legacy column false whatever a client sends"}
	return map[string]overrideEntry{
		"internal/server/handlers_core_doc_engine.go|json|override":            industry,
		"internal/server/handlers_core_doc_engine.go|json|override_reason":     industry,
		"internal/server/handlers_core_doc_engine.go|bool|Override":            industry,
		"engines/doctype.go|bool|isOverride":                                   industry,
		"engines/doctype.go|json|override_count":                               {NotAControl: "output: how many times the industry lock was overridden"},
		"engines/doctype.go|json|doctype_overrides":                            {NotAControl: "an industry profile's doctype additions (configuration file), not a control"},
		"internal/server/handlers_access_governance.go|json|doctype_overrides": {NotAControl: "output: a role template's per-doctype access levels"},

		"engines/pos_quote.go|json|override_id":      priceOverride,
		"engines/pos_quote.go|json|override_price":   priceOverride,
		"engines/sale_allocation.go|key|override_id": priceOverride,

		"engines/vendor_invoice.go|key|override_from_status":          payOverride,
		"engines/vendor_invoice.go|key|payment_override_reason":       payOverride,
		"internal/server/handlers_operations.go|json|override_reason": payOverride,

		"engines/negative_stock_policy.go|key|allow_negative": {Gate: "the item's Product Type says Allow negative stock = Yes; the mark is a server type no JSON body can produce (49.5.6)",
			Reason: "the product type's own setting (made-to-order or weighed goods)", Audit: "a POSOfflineSyncVariance record per shortfall; negative_stock_burst detection"},
		"engines/inventory.go|bool|allowNegative": {Gate: "server callers only: false everywhere except the POS sale (offline_synced, see its open entry) and a Backflush Rule's lenient lines",
			Reason: "the rule's Short stock setting, or the offline claim", Audit: "a POSOfflineSyncVariance record per shortfall; negative_stock_burst detection"},
		"engines/pos_checkout.go|bool|OfflineSynced":                     offline,
		"internal/server/handlers_pim_pos_finance.go|bool|OfflineSynced": offline,
		"engines/backflush_rules.go|bool|offlineSynced":                  offline,

		"engines/backflush_rules.go|json|override_allowed":                     backflushRule,
		"engines/backflush_rules.go|key|override_allowed":                      backflushRule,
		"engines/backflush_rules.go|bool|OverrideAllowed":                      backflushRule,
		"internal/server/handlers_pim_pos_finance.go|json|packaging_overrides": {NotAControl: "the cashier's pick among the components a Backflush Rule offers; refused unless the rule allows a change (POSOFF-0415), with the rule's reason when it asks for one"},

		"engines/orders.go|bool|SkipPending": {Gate: "oms.default on POST /api/v1/orders - the people who may accept at the Pending gate anyway; channel imports never set it",
			Reason: "the order was keyed in by staff, not imported (88.3.1 D12)", Audit: "ORDER_ACCEPTED row naming who accepted"},
		"engines/returns.go|bool|skipEligibility": {Gate: "set only by a No Receipt return exception (47.4.5), capability-gated, then second-person approval of the return",
			Reason: "exception reason and authorised_by required (validateReturnException)", Audit: "exception type and reason on the ReturnRequest; the approval log"},

		"engines/gst_place_of_supply.go|key|interstate_override": interstate,

		"engines/crm_campaigns.go|json|skip_if_bought": audience,
		"engines/crm_campaigns.go|key|skip_if_bought":  audience,
		"engines/crm_campaigns.go|bool|SkipIfBought":   audience,
		"engines/crm_send_gate.go|key|skip_if_bought":  audience,
		"engines/crm_send_gate.go|bool|SkipIfBought":   audience,

		"engines/number_series.go|json|store_overrides":                     {NotAControl: "a number series' per-store prefix (configuration), not a control"},
		"internal/server/handlers_core_doc_engine.go|json|store_overrides":  {NotAControl: "a number series' per-store prefix (configuration), not a control"},
		"internal/server/handlers_procurement_pim2.go|json|copies_override": {NotAControl: "how many copies to print"},
		"internal/server/handlers_qz_print.go|json|copies_override":         {NotAControl: "how many copies to print"},
		"engines/shopify_sync.go|bool|force":                                {NotAControl: "send every price instead of only the changed ones"},
		"engines/transaction_controls.go|bool|force":                        {NotAControl: "run the reconciliation checks now instead of at most once a day"},
	}
}()

var (
	overrideSnake = regexp.MustCompile(`^[a-z][a-z0-9_]*$`)
	overrideWord  = regexp.MustCompile(`(^|_)(force|forced|bypass|override|overrides|skip|ignore|unsafe|allow_negative)(_|$)`)
	overrideFlag  = regexp.MustCompile(`(?i)^(is)?(force|bypass|override|skip|ignore|unsafe|allownegative|offlinesynced)`)
)

// overrideNames walks one Go file and returns "kind|name" for every
// override-shaped JSON tag, string key or bool field / parameter in it.
func overrideNames(t *testing.T, fset *token.FileSet, path string) []string {
	f, err := parser.ParseFile(fset, path, nil, 0)
	if err != nil {
		t.Fatalf("parse %s: %v", path, err)
	}
	seen := map[string]bool{}
	add := func(kind, name string) { seen[kind+"|"+name] = true }
	snakeKey := func(e ast.Expr) (string, bool) {
		lit, ok := e.(*ast.BasicLit)
		if !ok || lit.Kind != token.STRING {
			return "", false
		}
		s, err := strconv.Unquote(lit.Value)
		if err != nil || !overrideSnake.MatchString(s) || !overrideWord.MatchString(s) {
			return "", false
		}
		return s, true
	}
	ast.Inspect(f, func(n ast.Node) bool {
		switch x := n.(type) {
		case *ast.Field: // struct fields, and function parameters and results
			if x.Tag != nil {
				tag, _ := strconv.Unquote(x.Tag.Value)
				name := strings.Split(reflect.StructTag(tag).Get("json"), ",")[0]
				if overrideSnake.MatchString(name) && overrideWord.MatchString(name) {
					add("json", name)
				}
			}
			if id, ok := x.Type.(*ast.Ident); ok && id.Name == "bool" {
				for _, nm := range x.Names {
					if overrideFlag.MatchString(nm.Name) {
						add("bool", nm.Name)
					}
				}
			}
		case *ast.IndexExpr:
			if s, ok := snakeKey(x.Index); ok {
				add("key", s)
			}
		case *ast.CallExpr:
			for _, a := range x.Args {
				if s, ok := snakeKey(a); ok {
					add("key", s)
				}
			}
		}
		return true
	})
	out := make([]string, 0, len(seen))
	for k := range seen {
		out = append(out, k)
	}
	return out
}

func TestStage4956EveryOverrideIsReviewed(t *testing.T) {
	root, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	fset := token.NewFileSet()
	found := map[string]bool{}
	for _, dir := range []string{"engines", filepath.Join("internal", "server")} {
		files, _ := filepath.Glob(filepath.Join(root, dir, "*.go"))
		for _, path := range files {
			base := filepath.Base(path)
			if strings.HasSuffix(base, "_test.go") || strings.HasSuffix(base, "_generated.go") {
				continue
			}
			if _, err := os.Stat(path); err != nil {
				continue
			}
			rel := filepath.ToSlash(filepath.Join(dir, base))
			for _, n := range overrideNames(t, fset, path) {
				found[rel+"|"+n] = true
			}
		}
	}

	var unlisted, open []string
	for k := range found {
		e, ok := reviewedOverrides[k]
		switch {
		case !ok:
			unlisted = append(unlisted, k)
		case e.Open != "":
			open = append(open, k+" - "+e.Open)
		}
	}
	sort.Strings(unlisted)
	sort.Strings(open)
	for _, k := range unlisted {
		t.Errorf("unreviewed override %s: add it to reviewedOverrides with its capability check, reason and audit row - or say why it is not a control", k)
	}
	var stale []string
	for k, e := range reviewedOverrides {
		if !found[k] {
			stale = append(stale, k)
		}
		if e.NotAControl == "" && e.Open == "" && (e.Gate == "" || e.Reason == "" || e.Audit == "") {
			t.Errorf("override %s must name its gate, reason and audit (or NotAControl / Open)", k)
		}
	}
	sort.Strings(stale)
	for _, k := range stale {
		t.Errorf("reviewedOverrides lists %s but the code no longer has it - remove the entry", k)
	}
	for _, o := range open {
		t.Logf("open (needs a decision): %s", o)
	}
	t.Logf("%d override-shaped names reviewed, %d open", len(found), len(open))
}

// The scanner itself: a new force flag, skip key or bypass parameter is seen.
func TestStage4956ScannerSeesANewOverride(t *testing.T) {
	path := filepath.Join(t.TempDir(), "x.go")
	src := "package x\n\ntype req struct {\n\tForce bool `json:\"force\"`\n\tName string `json:\"name\"`\n}\n\n" +
		"func h(payload map[string]interface{}, bypassLimits bool, skipped bool) {\n\t_ = payload[\"skip_approval\"]\n\t_ = payload[\"skipped_rows\"]\n}\n"
	if err := os.WriteFile(path, []byte(src), 0o600); err != nil {
		t.Fatal(err)
	}
	got := map[string]bool{}
	for _, n := range overrideNames(t, token.NewFileSet(), path) {
		got[n] = true
	}
	for _, want := range []string{"json|force", "bool|Force", "bool|bypassLimits", "key|skip_approval"} {
		if !got[want] {
			t.Errorf("the scanner missed %s (found %v)", want, got)
		}
	}
	for _, not := range []string{"json|name", "key|skipped_rows"} {
		if got[not] {
			t.Errorf("the scanner flagged %s, which is not an override", not)
		}
	}
}
