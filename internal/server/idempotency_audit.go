package server

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
)

// Stage 49.4.7 - the per-command idempotency audit.
//
// docs/security/idempotency-audit.md is generated from two things this
// package already owns: the route table (RouteAccessTable, read from
// routes.go) and the handler sources. For every state-changing /api route it
// says whether the command carries an idempotency key, what the replay window
// is, and whether the command moves money, stock or only a document. A
// money- or stock-moving command with no key is a finding and is listed at
// the top. TestStage4947IdempotencyAuditIsCurrent regenerates the file and
// fails when the committed copy is stale, so a new POST route is audited the
// day it is registered.

// IdempotencyAuditRow is one state-changing route's audit line.
type IdempotencyAuditRow struct {
	Route   string
	Handler string
	Key     string // yes (command claim) / yes (header) / yes (job key) / no / n.a.
	Window  string
	Touches string // money / stock / money, stock / document
}

var (
	idempotencyKeyedSource = regexp.MustCompile(`(?i)idempotenc|ClaimCommand\(|RequestKey`)
	idempotencyJobSource   = regexp.MustCompile(`\bEnqueueJob(?:Context)?\(`)
	idempotencyParam       = regexp.MustCompile(`\{[^}]*\}`)
)

// The three vocabularies below are matched against a route's path SEGMENTS
// (parameters removed), by prefix, so "pack" reads /pack and /packing but not
// /package, and "count" reads /count but not /cycle-count's command.

// idempotencyQuestionSegments: a POST whose last segment is one of these asks
// a question and changes nothing a replay could double.
var idempotencyQuestionSegments = []string{"search", "preview", "validate", "check", "lookup", "query", "find", "export", "print", "sign",
	"login", "logout", "verify", "refresh", "report", "count", "simulate", "dry-run", "test", "calc", "estimate", "suggest", "resolve",
	"render", "download", "forecast", "whoami", "csp-report", "heartbeat", "explain", "compare", "diff", "evaluate", "questions", "ask",
	"analy", "inspect", "ping", "echo", "mfa", "password", "token", "quote", "classify", "detect", "scan", "trace", "match", "plan", "what-if"}

// idempotencyMoneySegments: the command moves money (a posting, a payment, a
// settlement, a payroll run).
var idempotencyMoneySegments = []string{"payment", "payments", "checkout", "settle", "settlement", "settlements", "invoice", "invoices",
	"sales-invoice", "vendor-invoice", "journal-voucher", "refund", "refunds", "credit-note", "vendor-credit-note", "payroll", "run-payroll",
	"disburse-loan", "gl", "voucher", "vouchers", "receipt", "receipts", "pay", "tender", "cash", "cash-movements", "advance", "advances",
	"tds", "einvoice", "ewaybills", "bank-reconcile", "bank-reconciliation", "loan", "deposit", "petty-cash", "payout", "write-off",
	"depreciation", "fx-revaluation", "landed-cost-voucher", "payment-proposal", "redeem", "loyalty-redemption", "wallet", "gift-card",
	"record-utr", "offline-sales", "expenses", "credit", "charge", "commission", "price", "discount", "coupon", "period-reopen", "periods", "close"}

// idempotencyStockSegments: the command moves stock (a receipt, an issue, a
// transfer, an adjustment, a pick / pack / ship step).
var idempotencyStockSegments = []string{"grn", "grn-steps", "stock", "stock-moves", "transfer", "transfers", "transfer-orders", "pick", "picks",
	"picking", "pack", "packing", "ship", "shipments", "dispatch", "receive", "receive-lines", "return", "returns", "adjustments", "cycle-count",
	"cycle-counts", "bin", "bins", "putaway", "manifest", "consume", "consumption", "issue-material", "issue", "production", "production-order",
	"asn", "waves", "wave", "lpn", "challans", "bundles", "assemble", "disassemble", "material-requests", "scrap", "quarantine", "reserve",
	"allocate", "fulfillment", "deliver", "delivery", "replenish", "handover", "pickup", "sync-inventory", "kits", "checkout", "offline-sales",
	"job-work", "subcontract-order", "backflush", "complete", "confirm-operation", "receipt", "receipts", "mrp-changes", "correct", "post", "post-all"}

func idempotencySegments(path string) []string {
	clean := idempotencyParam.ReplaceAllString(strings.ToLower(path), "")
	var out []string
	for _, s := range strings.Split(clean, "/") {
		if s != "" {
			out = append(out, s)
		}
	}
	return out
}

func idempotencySegmentIn(segments []string, vocabulary []string) bool {
	for _, seg := range segments {
		for _, word := range vocabulary {
			if seg == word || strings.HasPrefix(seg, word+"-") || strings.HasPrefix(seg, word+".") || (len(word) >= 5 && strings.HasPrefix(seg, word)) {
				return true
			}
		}
	}
	return false
}

// handlerBodies maps every top-level function in dir's non-test Go files to
// its source text, so a handler's own body can be inspected for the key.
func handlerBodies(dir string) (map[string]string, error) {
	fset := token.NewFileSet()
	files, err := filepath.Glob(filepath.Join(dir, "*.go"))
	if err != nil {
		return nil, err
	}
	bodies := map[string]string{}
	for _, path := range files {
		if strings.HasSuffix(path, "_test.go") {
			continue
		}
		src, err := os.ReadFile(path)
		if err != nil {
			return nil, err
		}
		f, err := parser.ParseFile(fset, path, src, 0)
		if err != nil {
			return nil, err
		}
		for _, decl := range f.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if !ok || fn.Body == nil || fn.Recv != nil {
				continue
			}
			start, end := fset.Position(fn.Body.Pos()).Offset, fset.Position(fn.Body.End()).Offset
			bodies[fn.Name.Name] = string(src[start:end])
		}
	}
	return bodies, nil
}

// IdempotencyAuditRows classifies every state-changing /api route.
func IdempotencyAuditRows(routesGo, handlerDir string) ([]IdempotencyAuditRow, error) {
	routes, err := RouteAccessTable(routesGo)
	if err != nil {
		return nil, err
	}
	bodies, err := handlerBodies(handlerDir)
	if err != nil {
		return nil, err
	}
	// Local same-package calls are followed one hop, so a handler that hands
	// the key to a helper in this package is still read as keyed.
	callRe := regexp.MustCompile(`\b([a-zA-Z_][a-zA-Z0-9_]*)\(`)
	sourceOf := func(handler string) string {
		body, ok := bodies[handler]
		if !ok {
			return ""
		}
		var sb strings.Builder
		sb.WriteString(body)
		for _, m := range callRe.FindAllStringSubmatch(body, -1) {
			if callee, ok := bodies[m[1]]; ok && m[1] != handler {
				sb.WriteString(callee)
			}
		}
		return sb.String()
	}
	var rows []IdempotencyAuditRow
	for _, rt := range routes {
		switch rt.Method {
		case "POST", "PUT", "PATCH", "DELETE", "ANY": // ANY: the handler switches on the method itself (the generic document API)
		default:
			continue
		}
		row := IdempotencyAuditRow{Route: rt.Method + " " + rt.Path, Handler: rt.Handler}
		src := sourceOf(rt.Handler)
		segments := idempotencySegments(rt.Path)
		last := []string{}
		if len(segments) > 0 {
			last = segments[len(segments)-1:]
		}
		switch {
		case strings.HasPrefix(rt.Path, "/api/public/"):
			row.Key, row.Window = "yes (Idempotency-Key header, required)", "public API retention (BeginIdempotentRequest)"
		case idempotencyKeyedSource.MatchString(src):
			row.Key, row.Window = "yes (command key)", "7 days (command claim)"
			if !strings.Contains(src, "ClaimCommand(") {
				row.Window = "per engine (see handler)"
			}
		case idempotencyJobSource.MatchString(src):
			row.Key, row.Window = "yes (job key)", "job lifetime (async_jobs)"
		case rt.Method == "POST" && idempotencySegmentIn(last, idempotencyQuestionSegments):
			row.Key, row.Window = "n.a. (a question, not a command)", "-"
		default:
			row.Key, row.Window = "no", "-"
		}
		money, stock := idempotencySegmentIn(segments, idempotencyMoneySegments), idempotencySegmentIn(segments, idempotencyStockSegments)
		switch {
		case money && stock:
			row.Touches = "money, stock"
		case money:
			row.Touches = "money"
		case stock:
			row.Touches = "stock"
		default:
			row.Touches = "document"
		}
		rows = append(rows, row)
	}
	sort.Slice(rows, func(i, j int) bool { return rows[i].Route < rows[j].Route })
	return rows, nil
}

// IdempotencyAuditMarkdown renders the audit document. It carries no date,
// so the output is a pure function of the sources and the staleness test can
// compare bytes.
func IdempotencyAuditMarkdown(routesGo, handlerDir string) (string, error) {
	rows, err := IdempotencyAuditRows(routesGo, handlerDir)
	if err != nil {
		return "", err
	}
	keyed, findings := 0, []IdempotencyAuditRow{}
	for _, row := range rows {
		if strings.HasPrefix(row.Key, "yes") {
			keyed++
		}
		if row.Key == "no" && row.Touches != "document" {
			findings = append(findings, row)
		}
	}
	var b strings.Builder
	b.WriteString(`---
doc_id: DOC-IDEMPOTENCY01
title: Idempotency audit of state-changing API commands
type: reference
status: draft
owner: security-owner
approvers: [documentation-maintainer, security-owner]
audience: [maintainers, security-owner]
applies_to: source release 0.19.0 and later
authority: source
confidentiality: internal
last_verified: 2026-10-29
review_by: 2027-01-27
supersedes: none
superseded_by: none
---

# Idempotency audit of state-changing API commands

**Stage 49.4.7.** Generated from ` + "`internal/server/routes.go`" + ` and the handler
sources by ` + "`IdempotencyAuditMarkdown`" + ` (internal/server/idempotency_audit.go).
Do not edit by hand: ` + "`TestStage4947IdempotencyAuditIsCurrent`" + ` fails when this file
is stale. Regenerate with
` + "`ERP_WRITE_IDEMPOTENCY_AUDIT=1 go test ./internal/server -run TestStage4947IdempotencyAuditIsCurrent`" + `.

Every POST / PUT / PATCH / DELETE route under ` + "`/api`" + ` is one row. **Key** says
whether a retry of the same command is recognised: a command key (` + "`ClaimCommand`" + `,
Stage 47.3 - the claim is remembered 7 days), the public API's required
` + "`Idempotency-Key`" + ` header (Stage 38.2), or a job key (` + "`EnqueueJob`" + ` - a repeated
key returns the same job). **n.a.** marks a POST that asks a question (a
search, a preview, a validation, an export) and changes nothing a replay could
double. **Touches** is read from the route's name: money, stock, or only a
document. A money- or stock-moving command with no key is a finding.

What a finding means here: the command has no *explicit* replay key. Most of
these are still safe to retry once, because the record's own state gate refuses
the second posting (an invoice already Posted, a claim already Paid, a period
already Closed answers a refusal, not a second entry - the 132.17 action-only
statuses and ` + "`expected_version`" + ` on record saves), and a command that creates a
new document each time (a journal voucher, an adjustment) is the case a key
exists for. Each finding is therefore either given a key through
` + "`engines.ClaimCommand`" + ` (as checkout, returns and offline sales are) or justified
in the tracker item that closes it; the list below is the open work.

`)
	fmt.Fprintf(&b, "Routes audited: **%d**; keyed: **%d**; findings (money / stock without a key): **%d**.\n\n", len(rows), keyed, len(findings))
	b.WriteString("## Findings\n\n")
	if len(findings) == 0 {
		b.WriteString("None.\n\n")
	} else {
		b.WriteString("A retry of any of these can repeat its effect. Each is either given a key through the existing command-claim helper, or justified here.\n\n| Route | Handler | Touches |\n|---|---|---|\n")
		for _, row := range findings {
			fmt.Fprintf(&b, "| `%s` | `%s` | %s |\n", row.Route, row.Handler, row.Touches)
		}
		b.WriteString("\n")
	}
	b.WriteString("## Every state-changing route\n\n| Route | Handler | Key | Replay window | Touches |\n|---|---|---|---|---|\n")
	for _, row := range rows {
		fmt.Fprintf(&b, "| `%s` | `%s` | %s | %s | %s |\n", row.Route, row.Handler, row.Key, row.Window, row.Touches)
	}
	return b.String(), nil
}
