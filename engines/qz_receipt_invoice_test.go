package engines

import (
	"custom_erp/db"
	"encoding/json"
	"strings"
	"testing"
)

// Stage 31.1.9 - one-click print for POS receipts and sales invoices.
//
// The pure formatting helpers are tested without a database because they are
// where a receipt can go wrong silently: a column count that is one too wide
// wraps every line, and a total clipped by a long SKU name is a customer
// dispute rather than a visible failure.

func TestESCPOSRowKeepsTheAmountAndClipsTheDescription(t *testing.T) {
	const cols = 32

	got := escposRow("SKU-1 x2", "240.00", cols)
	if !strings.HasSuffix(got, "\n") {
		t.Fatalf("row must end in a newline, got %q", got)
	}
	line := strings.TrimSuffix(got, "\n")
	if len([]rune(line)) != cols {
		t.Fatalf("row is %d columns wide, want %d: %q", len([]rune(line)), cols, line)
	}
	if !strings.HasSuffix(line, "240.00") {
		t.Fatalf("amount must sit flush right, got %q", line)
	}

	// A product name longer than the roll must cost characters off the
	// description, never off the money - a receipt is checked on its numbers.
	long := escposRow(strings.Repeat("LONG-ITEM-NAME", 5), "1234.50", cols)
	longLine := strings.TrimSuffix(long, "\n")
	if len([]rune(longLine)) != cols {
		t.Fatalf("over-long row is %d columns, want %d: %q", len([]rune(longLine)), cols, longLine)
	}
	if !strings.HasSuffix(longLine, "1234.50") {
		t.Fatalf("amount was clipped by a long description: %q", longLine)
	}

	// An amount that fills the whole roll on its own must not make the
	// padding arithmetic go negative and panic.
	if r := escposRow("x", strings.Repeat("9", cols+10), cols); r == "" {
		t.Fatal("an over-long amount produced no row")
	}
}

func TestESCPOSColumnsDefaultsToTheStandardRoll(t *testing.T) {
	// Unset means an 80mm retail roll, which is what a POS receipt printer
	// almost always is; a 58mm unit has to say so on its Printer record.
	if c := escposColumns(""); c != 42 {
		t.Fatalf("unset width gave %d columns, want 42", c)
	}
	if c := escposColumns("80"); c != 42 {
		t.Fatalf("80mm gave %d columns, want 42", c)
	}
	if c := escposColumns("58"); c != 32 {
		t.Fatalf("58mm gave %d columns, want 32", c)
	}
}

func TestOnlyESCPOSGetsRawReceiptCommands(t *testing.T) {
	// isRawLanguage is deliberately not the test used for receipts: ZPL and
	// TSPL are label languages with no continuous feed, so a receipt sent as
	// raw commands to a ZPL unit comes out blank.
	if !isESCPOS("ESC-POS") || !isESCPOS(" esc-pos ") {
		t.Fatal("ESC-POS not recognised")
	}
	for _, lang := range []string{"ZPL", "TSPL", "PDF", "HTML", ""} {
		if isESCPOS(lang) {
			t.Fatalf("%q must not be treated as ESC-POS", lang)
		}
	}
}

// seedPrintableDoc inserts one document straight into the tenant schema. It
// deliberately does not go through the doctype engine: these tests are about
// how a stored document prints, and DocType validation rules changing should
// not turn into a failure here.
func seedPrintableDoc(t *testing.T, schema, doctype, id string, data map[string]interface{}, status string) {
	t.Helper()
	payload, err := json.Marshal(data)
	if err != nil {
		t.Fatalf("marshal %s: %v", doctype, err)
	}
	// created_by is FK'd to users; 'system' is the seeded account the rest of
	// the suite uses for the same reason.
	if _, err := db.DB.Exec(
		"INSERT INTO "+schema+".documents (id, doctype, data, status, created_by) VALUES ($1, $2, $3, $4, 'system')",
		id, doctype, payload, status); err != nil {
		t.Fatalf("seed %s %s: %v", doctype, id, err)
	}
}

func TestBuildReceiptPayloadFromTheStoredSale(t *testing.T) {
	db.InitDB(testConnStr())
	tenantID := "default"
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		t.Fatalf("schema: %v", err)
	}

	const paidCart = "TEST-QZ-RECEIPT-PAID"
	const openCart = "TEST-QZ-RECEIPT-OPEN"
	cleanup := func() {
		db.DB.Exec("DELETE FROM "+schema+".documents WHERE id IN ($1, $2)", paidCart, openCart)
	}
	cleanup()
	defer cleanup()

	cart := map[string]interface{}{
		"location":     "TEST-LOC",
		"payment_mode": "Cash",
		"items": []map[string]interface{}{
			{"sku": "SKU-A", "qty": 2, "sale_price": 150},
			{"sku": "SKU-B", "qty": 1, "sale_price": 200},
		},
		"offer_discount": 50,
		"applied_offers": []map[string]interface{}{{"name": "Festive 10%"}},
	}
	seedPrintableDoc(t, schema, "POSCart", paidCart, cart, "Paid")

	escpos := QZPrinter{Language: "ESC-POS", WidthMM: "80"}

	t.Run("an unpaid cart cannot produce a receipt", func(t *testing.T) {
		// A receipt is what a customer walks out with. Printing one for a
		// Pending Approval cart would be a receipt for money not collected.
		seedPrintableDoc(t, schema, "POSCart", openCart, cart, "Pending Approval")
		_, err := BuildReceiptPayload(tenantID, openCart, escpos)
		if err == nil {
			t.Fatal("an unpaid cart printed a receipt")
		}
		var ve *ValidationError
		if !asValidationError(err, &ve) || ve.Code != "GLOBAL-0019" {
			t.Fatalf("want a GLOBAL-0019 ValidationError, got %#v", err)
		}
	})

	t.Run("a missing cart is a coded not-found, not a raw SQL error", func(t *testing.T) {
		_, err := BuildReceiptPayload(tenantID, "TEST-QZ-NO-SUCH-CART", escpos)
		var ve *ValidationError
		if !asValidationError(err, &ve) || ve.Code != "GLOBAL-0004" {
			t.Fatalf("want a GLOBAL-0004 ValidationError, got %#v", err)
		}
	})

	t.Run("the ESC-POS receipt totals what was actually collected", func(t *testing.T) {
		p, err := BuildReceiptPayload(tenantID, paidCart, escpos)
		if err != nil {
			t.Fatalf("build: %v", err)
		}
		if len(p.Items) != 1 || p.Items[0].Type != "raw" || p.Items[0].Format != "command" {
			t.Fatalf("ESC-POS printer must get a raw command item, got %+v", p.Items)
		}
		body := p.Items[0].Data

		// 2 x 150 + 1 x 200 = 500, less the Stage 30.7 offer discount of 50.
		if !strings.Contains(body, "500.00") {
			t.Fatalf("subtotal 500.00 missing from receipt:\n%q", body)
		}
		if !strings.Contains(body, "450.00") {
			t.Fatalf("amount due 450.00 missing from receipt - the offer discount was dropped:\n%q", body)
		}
		if !strings.Contains(body, "Festive 10%") {
			t.Fatalf("the applied offer is not shown on the receipt:\n%q", body)
		}
		if !strings.HasPrefix(body, escInit) {
			t.Fatal("receipt does not start with ESC @ - the printer keeps whatever state the last job left")
		}
		if !strings.HasSuffix(body, escCut) {
			t.Fatal("receipt does not end with a cut - the next sale prints onto the same strip")
		}
		for _, line := range strings.Split(strings.TrimSuffix(body, escCut), "\n") {
			if strings.Contains(line, "\x1b") || strings.Contains(line, "\x1d") {
				continue // a control sequence makes the visible width unmeasurable
			}
			if len([]rune(line)) > 42 {
				t.Fatalf("line exceeds the 80mm roll and will wrap: %q", line)
			}
		}
	})

	t.Run("a non-ESC-POS printer gets rasterisable HTML, not commands", func(t *testing.T) {
		for _, lang := range []string{"PDF", "ZPL", ""} {
			p, err := BuildReceiptPayload(tenantID, paidCart, QZPrinter{Language: lang})
			if err != nil {
				t.Fatalf("build for %q: %v", lang, err)
			}
			if p.Items[0].Type != "pixel" || p.Items[0].Format != "html" {
				t.Fatalf("%q printer got %+v, want a pixel/html item", lang, p.Items[0])
			}
			if !strings.Contains(p.Items[0].Data, "450.00") {
				t.Fatalf("%q receipt is missing the amount due", lang)
			}
		}
	})
}

func TestBuildInvoicePayloadMarksAnUnpostedInvoice(t *testing.T) {
	db.InitDB(testConnStr())
	tenantID := "default"
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		t.Fatalf("schema: %v", err)
	}

	const draftID = "TEST-QZ-INV-DRAFT"
	const paidID = "TEST-QZ-INV-PAID"
	cleanup := func() {
		db.DB.Exec("DELETE FROM "+schema+".documents WHERE id IN ($1, $2)", draftID, paidID)
	}
	cleanup()
	defer cleanup()

	invoice := map[string]interface{}{
		"invoice_number": "SINV-0001",
		"customer":       "Acme Retail",
		"location":       "TEST-LOC",
		"total_amount":   12500,
	}
	seedPrintableDoc(t, schema, "SalesInvoice", draftID, invoice, "Draft")
	seedPrintableDoc(t, schema, "SalesInvoice", paidID, invoice, "Paid")

	// A Draft invoice is a legitimate proforma to hand a customer, so it
	// prints - but it must never be mistakable for a posted one.
	draft, err := BuildInvoicePayload(tenantID, draftID, QZPrinter{Language: "PDF"})
	if err != nil {
		t.Fatalf("draft: %v", err)
	}
	if !strings.Contains(draft.Items[0].Data, "DRAFT") {
		t.Fatalf("a Draft invoice printed with no DRAFT marking:\n%s", draft.Items[0].Data)
	}

	paid, err := BuildInvoicePayload(tenantID, paidID, QZPrinter{Language: "PDF"})
	if err != nil {
		t.Fatalf("paid: %v", err)
	}
	if strings.Contains(paid.Items[0].Data, "DRAFT") {
		t.Fatal("a Paid invoice was marked DRAFT")
	}
	for _, want := range []string{"SINV-0001", "Acme Retail", "12500.00"} {
		if !strings.Contains(paid.Items[0].Data, want) {
			t.Fatalf("invoice is missing %q:\n%s", want, paid.Items[0].Data)
		}
	}

	// A till printer is a legitimate invoice printer for a small counter.
	escpos, err := BuildInvoicePayload(tenantID, paidID, QZPrinter{Language: "ESC-POS", WidthMM: "58"})
	if err != nil {
		t.Fatalf("escpos: %v", err)
	}
	if escpos.Items[0].Type != "raw" || !strings.HasSuffix(escpos.Items[0].Data, escCut) {
		t.Fatalf("ESC-POS invoice must be a raw command stream ending in a cut, got %+v", escpos.Items[0])
	}
}

// asValidationError is errors.As without the import, kept local to this file
// so the assertion reads the same at each call site.
func asValidationError(err error, target **ValidationError) bool {
	ve, ok := err.(*ValidationError)
	if ok {
		*target = ve
	}
	return ok
}
