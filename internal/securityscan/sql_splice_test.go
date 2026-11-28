package securityscan

import (
	"fmt"
	"sort"
	"strings"
	"testing"
)

// Item 322.1's reviewed list: every file where a query writes something into
// a quoted SQL literal instead of passing it as $n, with how many places and
// why each is safe. The count is part of the entry, as in the no-bypass
// inventory: a second splice next to a reviewed one fails the build.
//
// The way off this list is to pass the value as a parameter. Add an entry only
// when what is written is fixed in code (a constant, a key from a closed set),
// and say where it comes from.
var reviewedSQLSplices = map[string]struct {
	Count  int
	Reason string
}{
	"engines/approval_reminders.go":                {1, "step.stamp is approval_reminded_at or approval_escalated_at, from the loop's own table"},
	"engines/asset_depreciation.go":                {2, "the account constant assetAccAccumulated (1410)"},
	"engines/budgeting.go":                         {1, "amountField is total_amount or invoice_amount, literals at both callers"},
	"engines/connector_disconnect.go":              {1, "k is one of the job's payload keys, a list in code"},
	"engines/credit_control.go":                    {2, "the constants AcceptanceRejected and HoldCredit"},
	"engines/crm_analytics.go":                     {2, "r.field is customer_id or customer, from the reassign table above it"},
	"engines/crm_campaigns.go":                     {2, "a loaded time zone name and a generated run id, both with every quote stripped"},
	"engines/crm_core.go":                          {1, "a loaded time zone name with every quote stripped"},
	"engines/crm_reports.go":                       {1, "f is consent_sms, consent_whatsapp or consent_email, literals"},
	"engines/crm_segments.go":                      {2, "crmConsentTick returns one of three constant field names, or empty (refused)"},
	"engines/currency_fx.go":                       {1, "class.DocType comes from the revaluation table in code"},
	"engines/dashboard_sales.go":                   {3, "array, field and origDoctype are literals at every call"},
	"engines/deferred_prepaid.go":                  {2, "linkedIDField is sales_invoice_id or expense_account_code, literals"},
	"engines/finance_cash_position.go":             {1, "amountField is total_amount or invoice_amount, literals at both callers"},
	"engines/inventory_stock_requests.go":          {1, "field is to_warehouse or from_warehouse, set just above"},
	"engines/item_attribute_map.go":                {1, "field is product_type or category, set just above"},
	"engines/location_cost_center.go":              {1, "keys come from locationRefKeys and locationParentKeys, lists in code"},
	"engines/loyalty_tiering.go":                   {1, "the constant loyaltyExpiryReferenceDoctype"},
	"engines/marketing_consent.go":                 {1, "consentField returns one of two constant field names, else the SQL is FALSE"},
	"engines/master_duplicates.go":                 {1, "field lists are literals at both callers"},
	"engines/moving_cost.go":                       {4, "the serial status constants SerialInStock and SerialAllocated"},
	"engines/oms_console.go":                       {1, "spec.doctype comes from the related-section table in the same function"},
	"engines/oms_exception_queues.go":              {1, "the hold constant HoldPaymentPending"},
	"engines/pack_invoice.go":                      {1, "field is customer_name or customer_reference, literals"},
	"engines/pim_item_merge.go":                    {6, "to_regclass on the checked schema and a table from a literal list"},
	"engines/pos_sale_reconciliation.go":           {1, "the account constant accountRoundOff (5280)"},
	"engines/posting_groups.go":                    {3, "base is an account constant at every caller; event comes from controlKinds"},
	"engines/posting_groups_returns.go":            {1, "the account constant accountRoundOff (5280)"},
	"engines/purchasing_conveniences.go":           {1, "step.stamp is approval_reminded_at or approval_escalated_at, from the loop's own table"},
	"engines/quality_lot_genealogy.go":             {2, "col is input or output, literals"},
	"engines/report_catalog.go":                    {2, "catSQLField strips every quote from a field a catalog source names in code"},
	"engines/report_catalog_controls.go":           {1, "storeField is location at both callers"},
	"engines/report_catalog_sales.go":              {1, "the stored-value event constant SVEventRedeem"},
	"engines/report_register_builder.go":           {2, "register spec fields from code, with every quote stripped"},
	"engines/saas.go":                              {1, "table comes from the literal list of template tables"},
	"engines/security_detection_abuse.go":          {1, "doctype is a literal at every caller"},
	"engines/service_entry.go":                     {1, "field is service_entry_id or po_id, literals"},
	"engines/setup_readiness.go":                   {2, "f is a literal at every call"},
	"engines/shipping_package.go":                  {1, "field is fulfillment_task_id or order_id, set just above"},
	"engines/sticker_document_source.go":           {1, "fields come from ScopeConstraints, defined in code"},
	"engines/stock_location_txn_defaults.go":       {1, "field is stock_to or stock_from, literals"},
	"engines/supplier_portal_orders.go":            {1, "doctype is a literal at every caller"},
	"engines/tenant_portable_export.go":            {1, "saasTextArray of the constant terminal status list"},
	"engines/threshold_splitting_report.go":        {2, "field is a literal, or base_ plus a name from documentAmountFields"},
	"engines/vendor_invoice.go":                    {1, "field is grn_id or po_id, literals"},
	"engines/worker_sweep.go":                      {1, "s is a schema name checked against sweepSchemaRe just above"},
	"internal/server/document_read_rule.go":        {1, "the field comes from ScopeConstraints, defined in code"},
	"internal/server/handlers_core_doc_engine.go":  {6, "scope fields come from code, supplierScopeField is a constant, and a filter key must match safeFilterKeyRe and be a declared field"},
	"internal/server/handlers_core_list_search.go": {4, "field lists are package constants, and doctype must be a key of masterSearchFields"},
	"internal/testfault/testfault.go":              {1, "the constant Marker, in a test-only trigger"},
}

func TestNoUnreviewedSQLSplice(t *testing.T) {
	hits, err := ScanSQLSplices(repoRoot)
	if err != nil {
		t.Fatal(err)
	}
	byFile := map[string][]SQLSplice{}
	for _, h := range hits {
		byFile[h.File] = append(byFile[h.File], h)
	}
	where := func(file string) string {
		var b strings.Builder
		for _, h := range byFile[file] {
			fmt.Fprintf(&b, "\n             %s:%d %s: %s", h.File, h.Line, h.Func, h.Excerpt)
		}
		return b.String()
	}
	var problems []string
	for file, n := range CountSQLSplicesByFile(hits) {
		entry, ok := reviewedSQLSplices[file]
		switch {
		case !ok:
			problems = append(problems, fmt.Sprintf("  NEW      %s: %d place(s) write into a quoted SQL literal - pass the value as $n, or list the file with why it is safe%s", file, n, where(file)))
		case n > entry.Count:
			problems = append(problems, fmt.Sprintf("  INCREASE %s: %d place(s), %d reviewed - review the new one on its own%s", file, n, entry.Count, where(file)))
		case n < entry.Count:
			problems = append(problems, fmt.Sprintf("  STALE    %s: %d place(s), %d reviewed - lower the count", file, n, entry.Count))
		}
	}
	for file := range reviewedSQLSplices {
		if len(byFile[file]) == 0 {
			problems = append(problems, fmt.Sprintf("  GONE     %s is reviewed but has no splice any more - remove the entry", file))
		}
	}
	if len(problems) > 0 {
		sort.Strings(problems)
		t.Fatalf("SQL splice review (322.1) does not match the tree:\n%s", strings.Join(problems, "\n"))
	}
}

// A scanner that finds nothing passes the list test, so it is checked on
// shapes it must see and shapes it must leave alone.
func TestSQLSpliceScannerSeesQuotedHoles(t *testing.T) {
	cases := map[string]int{
		"fmt.Sprintf(`SELECT * FROM %s.documents WHERE code = '%s'`, schema, code)": 1,
		"fmt.Sprintf(\"UPDATE t SET a = 1 WHERE id = '%v'\", id)":                   1,
		"fmt.Sprintf(`SELECT 1 WHERE status IN ('%s', '%s')`, a, b)":                2,
		"fmt.Sprintf(`UPDATE d SET data = jsonb_set(data, '{%s}', $1)`, key)":       1,
		"fmt.Sprintf(`SELECT data->>'%[1]s' FROM d`, key)":                          1,
		"fmt.Sprintf(`SELECT 1 FROM d WHERE n = '%q'`, n)":                          1,
		"fmt.Sprintf(`SELECT 1 FROM d WHERE n = '%c'`, n)":                          1,
		"fmt.Sprintf(\"SELECT 1 \"+\"WHERE code = '%s'\", code)":                    1,
		"fmt.Fprintf(&b, ` AND code = '%s'`, code)":                                 1,
		"\"SELECT * FROM d WHERE code = '\" + code + \"'\"":                         1,
		"\"DELETE FROM d WHERE id = '\" + id + \"' AND x = 1\"":                     1,
		"\" AND name = '\" + strings.TrimSpace(name) + \"'\"":                       1,
	}
	for expr, want := range cases {
		src := "package p\n\nfunc f() { _ = " + expr + " }\n"
		hits, err := sqlSplicesInSource("p.go", []byte(src))
		if err != nil {
			t.Fatalf("%s: %v", expr, err)
		}
		if len(hits) != want {
			t.Errorf("%s: %d hit(s), want %d", expr, len(hits), want)
		}
	}
}

func TestSQLSpliceScannerLeavesSafeShapesAlone(t *testing.T) {
	for _, expr := range []string{
		"fmt.Sprintf(`SELECT * FROM %s.documents WHERE code = $1`, schema)",           // schema, value as $1
		"fmt.Sprintf(`SELECT * FROM d WHERE name ILIKE '%%' || $1 || '%%'`)",          // %% is text
		"fmt.Sprintf(`SELECT 'it''s' AS a, %s FROM d WHERE status = 'Open'`, col)",    // '' is an escaped quote
		"fmt.Sprintf(`SELECT 1 FROM d WHERE id IN (%s)`, holders)",                    // unquoted: review, not scan
		"fmt.Sprintf(\"field '%s' is missing\", name)",                                // not SQL
		"fmt.Sprintf(\"updated_at:>'%s'\", since)",                                    // a Shopify query, not SQL
		"\"SELECT * FROM \" + schema + \".documents WHERE status = 'Open'\"",          // hole outside quotes
		"fmt.Sprintf(`SELECT * FROM \"%s\".documents WHERE status = 'Open'`, schema)", // identifier quotes
		"\"'\" + name + \"'\"", // no SQL text at all
		"fmt.Sprintf(\"SELECT 1 -- the wave's tasks\\nFROM %s.documents WHERE a = $1\", schema)", // apostrophe in a comment
		"fmt.Sprintf(\"SELECT 1 /* it's */ FROM %s.documents\", schema)",                         // apostrophe in a block comment
		"fmt.Sprintf(\"SELECT 1 FROM d -- '%s'\\nWHERE a = $1\", note)",                          // a hole in a comment
		"fmt.Sprintf(`DELETE FROM d WHERE at < now() - INTERVAL '%d days'`, days)",               // a number prints no quote
		"fmt.Sprintf(`SELECT 1 FROM d WHERE n = '%05d' AND r = '%.2f'`, n, r)",                   // nor does a padded one
	} {
		src := "package p\n\nfunc f() { _ = " + expr + " }\n"
		hits, err := sqlSplicesInSource("p.go", []byte(src))
		if err != nil {
			t.Fatalf("%s: %v", expr, err)
		}
		if len(hits) != 0 {
			t.Errorf("%s: %d hit(s), want none", expr, len(hits))
		}
	}
}
