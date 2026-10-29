package engines

import (
	"crypto/sha256"
	"custom_erp/db"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/rand/v2"
	"os"
	"regexp"
	"sort"
	"strings"
	"time"
)

// Stage 47.9.1: deterministic synthetic tenants for the performance
// laboratory. One seed always produces the same tenant - the same items,
// stock, sales, returns, ledger and audit trail - so a slow query or a
// growth figure measured on it can be measured again after a change.
//
// Every row is written through the engine a real tenant uses, so the data
// obeys the same invariants: masters through the CSV import core
// (runDocDataImport - validation, numbering, master id = code), stock
// through PostInventoryLedgerWithVoucher (no negative stock), bins through
// PutawayToBin and RecordOwnerStock (one owner per warehouse), sales through
// ResolvePOSQuote + FinalizePOSCheckout (server prices, GST, stock, GL in one
// transaction), partial returns through the ReturnRequest chain
// (CreateReturnRequestCommand ... ProcessRefundRequest, two people), the
// audit trail through LogAuditEvent and month-end finance through
// PostDoubleEntry (balanced, idempotency-keyed).
//
// The skew a real tenant has, and a uniform generator hides: one hot SKU
// takes a fixed share of sale lines, a few very large bills, a share of bills
// partly returned, most WMS bins empty, two or more stock owners, a long
// audit history on the hot SKU and a burst of postings at each month end.
//
// Safety: it refuses ENV=production, refuses while external side effects are
// on, and refuses a database whose name does not look like a scratch one
// (CheckSyntheticTarget). The tenant itself is flagged is_sandbox, so a server
// later started on the same database still sends nothing for it.

// SynthShape is one named tenant size. Bills is the size the name refers to:
// "10k" is ten thousand POS bills, with masters and stock scaled to suit.
type SynthShape struct {
	Name             string
	Items            int
	Stores           int
	Warehouses       int
	BinsPerWarehouse int
	StockedBinPct    int // the share of bins that ever receive stock; the rest stay empty
	Vendors          int
	Customers        int
	Bills            int
	MaxLinesPerBill  int
	HotSKUPct        int // share of ordinary sale lines that take the hot SKU
	LargeOrders      int // bills with LargeOrderLines lines each
	LargeOrderLines  int
	ReturnPct        int // share of bills partly returned (one unit of one line)
	CustomerBillPct  int // share of bills with a known customer (loyalty earns)
	AuditTrail       int // audit events on the hot SKU - the long history
	Owners           int // stock owners, one per warehouse (47.5.1)
	Months           int // month ends, each with a burst of finance postings
	MonthEndEntries  int // postings per month end
}

var synthShapes = []SynthShape{
	{Name: "small", Items: 40, Stores: 2, Warehouses: 2, BinsPerWarehouse: 30, StockedBinPct: 20, Vendors: 8, Customers: 30,
		Bills: 60, MaxLinesPerBill: 4, HotSKUPct: 40, LargeOrders: 2, LargeOrderLines: 25, ReturnPct: 10, CustomerBillPct: 30,
		AuditTrail: 150, Owners: 2, Months: 3, MonthEndEntries: 6},
	{Name: "10k", Items: 2000, Stores: 10, Warehouses: 3, BinsPerWarehouse: 2000, StockedBinPct: 15, Vendors: 200, Customers: 5000,
		Bills: 10_000, MaxLinesPerBill: 5, HotSKUPct: 25, LargeOrders: 20, LargeOrderLines: 120, ReturnPct: 6, CustomerBillPct: 40,
		AuditTrail: 5_000, Owners: 3, Months: 12, MonthEndEntries: 40},
	{Name: "100k", Items: 10_000, Stores: 40, Warehouses: 4, BinsPerWarehouse: 10_000, StockedBinPct: 12, Vendors: 1_000, Customers: 40_000,
		Bills: 100_000, MaxLinesPerBill: 5, HotSKUPct: 20, LargeOrders: 100, LargeOrderLines: 300, ReturnPct: 6, CustomerBillPct: 40,
		AuditTrail: 50_000, Owners: 4, Months: 24, MonthEndEntries: 120},
	{Name: "1m", Items: 50_000, Stores: 150, Warehouses: 6, BinsPerWarehouse: 40_000, StockedBinPct: 10, Vendors: 5_000, Customers: 300_000,
		Bills: 1_000_000, MaxLinesPerBill: 5, HotSKUPct: 15, LargeOrders: 500, LargeOrderLines: 500, ReturnPct: 5, CustomerBillPct: 40,
		AuditTrail: 500_000, Owners: 6, Months: 36, MonthEndEntries: 400},
}

// SynthShapeNamed returns the named shape.
func SynthShapeNamed(name string) (SynthShape, bool) {
	for _, s := range synthShapes {
		if s.Name == name {
			return s, true
		}
	}
	return SynthShape{}, false
}

// SynthShapeNames lists the shapes, smallest first.
func SynthShapeNames() []string {
	out := make([]string, len(synthShapes))
	for i, s := range synthShapes {
		out[i] = s.Name
	}
	return out
}

// SynthOptions selects what GenerateSyntheticTenant builds.
type SynthOptions struct {
	Shape    string
	Seed     uint64
	TenantID string    // "" = synth-<shape>-<seed>
	Log      io.Writer // progress lines; nil = quiet
	// Measure records, per business transaction, which database transactions
	// it ran (47.18.2) so SynthResult.Footprint can report bytes per sale,
	// return, receipt and posting. It costs one cheap query per transaction.
	Measure bool
	// Custom replaces the named shape (tests, one-off experiments). Its Name
	// is used as given.
	Custom *SynthShape
}

// SynthResult is what was built: stable counts and a checksum over the
// business content (never over generated ids or timestamps), so two runs
// with one seed can be compared exactly.
type SynthResult struct {
	TenantID  string
	Schema    string
	Shape     SynthShape
	Seed      uint64
	HotSKU    string
	Counts    map[string]int64
	Checksum  string
	Elapsed   time.Duration
	Footprint []SynthFootprint `json:",omitempty"`
}

const (
	synthUser       = "synth-loader"
	synthCashier    = "synth-cashier"
	synthManager    = "synth-manager"
	synthSupervisor = "synth-supervisor"
	synthLegalCode  = "SYN-LE"
	// synthBaseDate anchors every dated posting the generator chooses itself,
	// so the ledger is the same whatever day it runs. POS bills and returns
	// carry the server's own date, as they would on a live till.
	synthBaseDate = "2026-04-01"
)

// synthScratchDB is what a scratch database's name looks like.
var synthScratchDB = regexp.MustCompile(`(?i)(test|scratch|synth|bench|perf|sandbox|tmp)`)

// CheckSyntheticTarget refuses anywhere a synthetic tenant must not be
// written: production, a run that could reach the outside world, and a
// database whose name does not say it is a scratch one. ERP_SYNTH_ALLOW_DB
// names one database on purpose, for a scratch copy with a plain name.
func CheckSyntheticTarget() error {
	if IsProductionEnv() {
		return errors.New("refused: ENV=production - synthetic tenants are for scratch databases only")
	}
	if ExternalSideEffectsEnabled() {
		return errors.New("refused: external side effects are on - run with ERP_DISABLE_EXTERNAL_SIDE_EFFECTS=1")
	}
	var name string
	if err := db.DB.QueryRow(`SELECT current_database()`).Scan(&name); err != nil {
		return err
	}
	if allow := strings.TrimSpace(os.Getenv("ERP_SYNTH_ALLOW_DB")); allow != "" && allow == name {
		return nil
	}
	if synthScratchDB.MatchString(name) {
		return nil
	}
	return fmt.Errorf("refused: database %q does not look like a scratch database - use a name with test/scratch/synth/bench/perf in it, or set ERP_SYNTH_ALLOW_DB=%s to name this one on purpose", name, name)
}

// synthGen carries one run's state.
type synthGen struct {
	opts     SynthOptions
	shape    SynthShape
	rng      *rand.Rand
	tenantID string
	schema   string
	stores   []string
	whs      []string
	items    []synthItem
	hot      string
	log      io.Writer
	meter    *synthMeter
}

type synthItem struct {
	code  string
	price float64
	cost  int64 // paise
}

// GenerateSyntheticTenant provisions a new sandbox tenant and fills it.
func GenerateSyntheticTenant(opts SynthOptions) (*SynthResult, error) {
	start := time.Now()
	if err := CheckSyntheticTarget(); err != nil {
		return nil, err
	}
	shape, ok := SynthShapeNamed(opts.Shape)
	if opts.Custom != nil {
		shape, ok = *opts.Custom, true
	}
	if !ok {
		return nil, fmt.Errorf("unknown shape %q (have %s)", opts.Shape, strings.Join(SynthShapeNames(), ", "))
	}
	if err := shape.check(); err != nil {
		return nil, err
	}
	tenantID := strings.TrimSpace(opts.TenantID)
	if tenantID == "" {
		tenantID = fmt.Sprintf("synth-%s-%d", shape.Name, opts.Seed)
	}
	schema := "tenant_" + strings.ToLower(strings.NewReplacer("-", "_", ".", "_").Replace(tenantID))
	if !validSQLIdentifier(schema) {
		return nil, fmt.Errorf("%q does not make a usable schema name", tenantID)
	}
	var exists bool
	if err := db.DB.QueryRow(`SELECT EXISTS (SELECT 1 FROM public.tenants WHERE tenant_id = $1)`, tenantID).Scan(&exists); err != nil {
		return nil, err
	}
	if exists {
		return nil, fmt.Errorf("tenant %s already exists - a synthetic tenant is built once; delete it first or pick another seed", tenantID)
	}

	g := &synthGen{opts: opts, shape: shape, tenantID: tenantID, schema: schema, log: opts.Log,
		rng: rand.New(rand.NewPCG(opts.Seed, 0x4709_0001))}
	if opts.Measure {
		g.meter = &synthMeter{}
	}
	if _, err := ProvisionTenantSchema(tenantID, schema, ""); err != nil {
		return nil, fmt.Errorf("provision: %w", err)
	}
	if _, err := db.DB.Exec(`UPDATE public.tenants SET name = $2, is_sandbox = TRUE,
		sandbox_expires_at = CURRENT_TIMESTAMP + INTERVAL '30 days' WHERE tenant_id = $1`,
		tenantID, fmt.Sprintf("Synthetic %s (seed %d)", shape.Name, opts.Seed)); err != nil {
		return nil, fmt.Errorf("flag sandbox: %w", err)
	}

	steps := []struct {
		name string
		run  func() error
	}{
		{"masters", g.masters},
		{"opening stock", g.openingStock},
		{"warehouse bins and owners", g.ownerStock},
		{"sales and partial returns", g.sales},
		{"audit history", g.auditHistory},
		{"month-end finance", g.monthEnds},
	}
	for _, s := range steps {
		t := time.Now()
		if err := s.run(); err != nil {
			return nil, fmt.Errorf("%s: %w", s.name, err)
		}
		g.logf("%-28s %s", s.name, time.Since(t).Round(time.Millisecond))
	}

	res := &SynthResult{TenantID: tenantID, Schema: schema, Shape: shape, Seed: opts.Seed, HotSKU: g.hot}
	var err error
	if res.Counts, res.Checksum, err = synthDigest(schema); err != nil {
		return nil, err
	}
	if g.meter != nil {
		if res.Footprint, err = g.meter.footprint(schema); err != nil {
			return nil, err
		}
	}
	res.Elapsed = time.Since(start)
	return res, nil
}

func (s SynthShape) check() error {
	switch {
	case s.Items < 2 || s.Stores < 1 || s.Bills < 1 || s.MaxLinesPerBill < 1:
		return errors.New("a shape needs at least 2 items, 1 store, 1 bill and 1 line per bill")
	case s.LargeOrders > 0 && s.LargeOrderLines > s.Items:
		return fmt.Errorf("a large order of %d lines needs at least that many items (have %d)", s.LargeOrderLines, s.Items)
	case s.Owners > s.Warehouses:
		return fmt.Errorf("%d owners need %d warehouses - one owner per warehouse", s.Owners, s.Owners)
	case s.HotSKUPct < 0 || s.HotSKUPct > 100 || s.ReturnPct < 0 || s.ReturnPct > 100 || s.StockedBinPct < 0 || s.StockedBinPct > 100:
		return errors.New("percentages run 0-100")
	}
	return nil
}

func (g *synthGen) logf(format string, args ...interface{}) {
	if g.log != nil {
		fmt.Fprintf(g.log, format+"\n", args...)
	}
}

// importRows writes master rows through the CSV import core, in chunks so a
// large shape never holds one huge result.
func (g *synthGen) importRows(doctype string, rows []map[string]interface{}) error {
	const chunk = 2000
	for at := 0; at < len(rows); at += chunk {
		end := min(at+chunk, len(rows))
		part := rows[at:end]
		var res *ImportResult
		err := g.measure("master:"+doctype, len(part), func() error {
			var err error
			res, err = runDocDataImport(g.tenantID, doctype, synthUser, "", false, part, make([]string, len(part)))
			return err
		})
		if err != nil {
			return fmt.Errorf("%s: %w", doctype, err)
		}
		if res.FailedRows > 0 {
			return fmt.Errorf("%s: %d row(s) refused, first: row %d %s", doctype, res.FailedRows, res.Errors[0].RowNumber, res.Errors[0].Message)
		}
	}
	return nil
}

// synthGSTIN builds a GSTIN that passes the real shape and check-digit rules.
func synthGSTIN(stateCode string, n int) string {
	pan := fmt.Sprintf("AAB%c%c%04d%c", 'C', 'A'+rune(n%26), n%10000, 'A'+rune((n/26)%26))
	base := stateCode + pan + "1Z"
	for _, c := range gstinChars {
		if g := base + string(c); ValidGSTINChecksum(g) {
			return g
		}
	}
	return base + "0"
}

func (g *synthGen) masters() error {
	s := g.shape
	if err := g.importRows("LegalEntity", []map[string]interface{}{{
		"code": synthLegalCode, "name": "Synthetic Retail Pvt Ltd", "gstin": synthGSTIN("29", 0), "status": "Active",
		"state": "Karnataka", "city": "Bengaluru", "pincode": "560001",
	}}); err != nil {
		return err
	}
	var locs []map[string]interface{}
	for i := 1; i <= s.Stores; i++ {
		code := fmt.Sprintf("SYN-S%03d", i)
		g.stores = append(g.stores, code)
		locs = append(locs, map[string]interface{}{"code": code, "name": fmt.Sprintf("Synthetic store %d", i), "type": "Store",
			"legal_entity": synthLegalCode, "status": "Active", "sellable": "Yes", "state": "Karnataka", "city": "Bengaluru", "pincode": "560001"})
	}
	for i := 1; i <= s.Warehouses; i++ {
		code := fmt.Sprintf("SYN-W%02d", i)
		g.whs = append(g.whs, code)
		locs = append(locs, map[string]interface{}{"code": code, "name": fmt.Sprintf("Synthetic warehouse %d", i), "type": "Warehouse",
			"legal_entity": synthLegalCode, "status": "Active", "sellable": "No", "state": "Karnataka", "city": "Bengaluru", "pincode": "560002"})
	}
	if err := g.importRows("Location", locs); err != nil {
		return err
	}
	var bins []map[string]interface{}
	for _, wh := range g.whs {
		for b := 1; b <= s.BinsPerWarehouse; b++ {
			bins = append(bins, map[string]interface{}{"bin_code": fmt.Sprintf("%s-B%05d", wh, b), "location": wh,
				"zone": fmt.Sprintf("Z%d", 1+(b-1)/500), "status": "Active", "bin_type": "Reserve"})
		}
	}
	if err := g.importRows("Bin", bins); err != nil {
		return err
	}
	items := make([]map[string]interface{}, 0, s.Items)
	for i := 1; i <= s.Items; i++ {
		price := float64(200 + 10*g.rng.IntN(480)) // 200 - 4990
		cost := int64(price*100) * int64(40+g.rng.IntN(21)) / 100
		code := fmt.Sprintf("SYN-I%06d", i)
		g.items = append(g.items, synthItem{code: code, price: price, cost: cost})
		items = append(items, map[string]interface{}{"code": code, "name": fmt.Sprintf("Synthetic item %d", i),
			"barcode": fmt.Sprintf("29%011d", i), "hsn_code": "6109", "gst_rate": 5, "tax_treatment": "Taxable",
			"sale_price": price, "mrp": price, "standard_cost": float64(cost) / 100, "category": fmt.Sprintf("Category %d", 1+i%12)})
	}
	g.hot = g.items[0].code
	if err := g.importRows("Item", items); err != nil {
		return err
	}
	var vendors []map[string]interface{}
	for i := 1; i <= s.Vendors; i++ {
		vendors = append(vendors, map[string]interface{}{"code": fmt.Sprintf("SYN-V%05d", i), "name": fmt.Sprintf("Synthetic supplier %d", i),
			"gstin": synthGSTIN("29", i), "state": "Karnataka", "status": "Active", "contact_email": fmt.Sprintf("supplier%d@example.invalid", i)})
	}
	if err := g.importRows("Vendor", vendors); err != nil {
		return err
	}
	var customers []map[string]interface{}
	for i := 1; i <= s.Customers; i++ {
		customers = append(customers, map[string]interface{}{"code": fmt.Sprintf("SYN-C%07d", i), "name": fmt.Sprintf("Synthetic customer %d", i),
			"phone": fmt.Sprintf("9%09d", 100000000+i), "status": "Active"})
	}
	return g.importRows("Customer", customers)
}

// receive puts stock into a location with its cost and the GL entry a
// receipt carries (Inventory against the GRN suspense account).
func (g *synthGen) receive(loc, voucher string, qty map[string]int) error {
	return g.receiveCosted(loc, voucher, qty, true)
}

// receiveCosted is receive; withCost false leaves the moving-average cost to
// the caller (opening stock records it once per item for every store).
func (g *synthGen) receiveCosted(loc, voucher string, qty map[string]int, withCost bool) error {
	return g.measure("receipt", 1, func() error {
		skus := make([]string, 0, len(qty))
		for sku := range qty {
			skus = append(skus, sku)
		}
		sort.Strings(skus)
		lines := make([]interface{}, 0, len(skus))
		var value int64
		for _, sku := range skus {
			lines = append(lines, map[string]interface{}{"sku": sku, "qty": qty[sku]})
		}
		if _, err := PostInventoryLedgerWithVoucher(g.tenantID, loc, lines, false, "StockReceipt", voucher, synthUser); err != nil {
			return err
		}
		for _, sku := range skus {
			cost := g.costOf(sku)
			if withCost {
				if err := RecordStockReceiptCost(g.tenantID, sku, float64(qty[sku]), cost); err != nil {
					return err
				}
			}
			value += cost * int64(qty[sku])
		}
		if value == 0 {
			return nil
		}
		return PostDoubleEntry(g.tenantID, "StockReceipt", voucher, map[string]int64{"1200": value}, map[string]int64{"2100": value},
			synthBaseDate, "synth:"+voucher)
	})
}

func (g *synthGen) costOf(sku string) int64 {
	var n int
	_, _ = fmt.Sscanf(sku, "SYN-I%06d", &n)
	if n >= 1 && n <= len(g.items) {
		return g.items[n-1].cost
	}
	return 0
}

// openingStock stocks every store for the bills to come, from the shape's
// own expected demand with headroom; a till that still runs short is
// replenished (a second receipt) rather than allowed negative.
func (g *synthGen) openingStock() error {
	s := g.shape
	avgLines := float64(s.MaxLinesPerBill+1) / 2
	perStoreLines := float64(s.Bills)/float64(s.Stores)*avgLines + float64(s.LargeOrders*s.LargeOrderLines)/float64(s.Stores)
	hotQty := int(perStoreLines*float64(s.HotSKUPct)/100*2*2) + 10
	otherQty := int(perStoreLines*float64(100-s.HotSKUPct)/100*2*2/float64(s.Items-1)) + 10
	qty := make(map[string]int, len(g.items))
	for i, it := range g.items {
		if i == 0 {
			qty[it.code] = hotQty
		} else {
			qty[it.code] = otherQty
		}
	}
	for _, store := range g.stores {
		if err := g.receiveCosted(store, "SYN-OPEN-"+store, qty, false); err != nil {
			return err
		}
	}
	// Every store received at the same unit cost, so one moving-average entry
	// per item for the whole quantity is the same result as one per store.
	for _, it := range g.items {
		if err := RecordStockReceiptCost(g.tenantID, it.code, float64(qty[it.code]*len(g.stores)), it.cost); err != nil {
			return err
		}
	}
	return nil
}

// ownerStock gives each warehouse its own owner and puts stock into a share
// of its bins; every other bin stays empty, as most of a real warehouse's do.
func (g *synthGen) ownerStock() error {
	s := g.shape
	stocked := s.BinsPerWarehouse * s.StockedBinPct / 100
	for w, wh := range g.whs {
		if w >= s.Owners {
			break
		}
		owner := fmt.Sprintf("SYN-OWNER-%02d", w+1)
		type placement struct {
			bin, sku string
			qty      int
		}
		var plan []placement
		qty := map[string]int{}
		for b := 0; b < stocked; b++ {
			// Bins are picked across the warehouse, not packed at the front.
			bin := fmt.Sprintf("%s-B%05d", wh, 1+(b*s.BinsPerWarehouse)/max(stocked, 1))
			sku := g.items[1+g.rng.IntN(len(g.items)-1)].code
			q := 5 + g.rng.IntN(46)
			plan = append(plan, placement{bin, sku, q})
			qty[sku] += q
		}
		if len(plan) == 0 {
			continue
		}
		if err := g.receive(wh, "SYN-OPEN-"+wh, qty); err != nil {
			return err
		}
		for _, p := range plan {
			if err := g.measure("putaway", 1, func() error {
				if err := PutawayToBin(g.tenantID, p.bin, p.sku, p.qty, synthUser); err != nil {
					return err
				}
				return RecordOwnerStock(g.tenantID, p.bin, p.sku, owner, "Good", p.qty, synthUser)
			}); err != nil {
				return fmt.Errorf("bin %s: %w", p.bin, err)
			}
		}
	}
	return nil
}

// sales rings up every bill through the real quote and checkout, and partly
// returns a share of them through the real return chain.
func (g *synthGen) sales() error {
	s := g.shape
	modes := []string{"Cash", "Card", "UPI"}
	// Large orders are spread through the run rather than bunched at one end.
	large := map[int]bool{}
	for k := 0; k < s.LargeOrders; k++ {
		large[(k*s.Bills)/max(s.LargeOrders, 1)] = true
	}
	for b := 0; b < s.Bills; b++ {
		cart := fmt.Sprintf("SYN-CART-%07d", b+1)
		store := g.stores[g.rng.IntN(len(g.stores))]
		mode := modes[g.rng.IntN(len(modes))]
		customer := ""
		if s.Customers > 0 && g.rng.IntN(100) < s.CustomerBillPct {
			customer = fmt.Sprintf("SYN-C%07d", 1+g.rng.IntN(s.Customers))
		}
		lines := g.billLines(large[b])
		wantReturn := !large[b] && g.rng.IntN(100) < s.ReturnPct
		if err := g.measure("sale", 1, func() error { return g.ringUp(cart, store, mode, customer, lines) }); err != nil {
			return fmt.Errorf("bill %s: %w", cart, err)
		}
		if wantReturn && (len(lines) > 1 || lines[0].Qty > 1) {
			if err := g.measure("return", 1, func() error { return g.partialReturn(cart, store, lines[0].Sku) }); err != nil {
				return fmt.Errorf("return of %s: %w", cart, err)
			}
		}
		if (b+1)%1000 == 0 {
			g.logf("  %d / %d bills", b+1, s.Bills)
		}
	}
	return nil
}

// billLines picks one bill's lines: the hot SKU takes HotSKUPct of ordinary
// lines; a large order is LargeOrderLines distinct SKUs.
func (g *synthGen) billLines(isLarge bool) []QuoteLineRequest {
	s := g.shape
	n := 1 + g.rng.IntN(s.MaxLinesPerBill)
	if isLarge {
		n = s.LargeOrderLines
	}
	seen := map[string]int{}
	var lines []QuoteLineRequest
	for len(lines) < n {
		sku := g.items[1+g.rng.IntN(len(g.items)-1)].code
		if !isLarge && g.rng.IntN(100) < s.HotSKUPct {
			sku = g.hot
		}
		qty := 1 + g.rng.IntN(3)
		if at, dup := seen[sku]; dup {
			if !isLarge {
				lines[at].Qty += qty
				n-- // a repeat pick is more of the same line
			}
			continue
		}
		seen[sku] = len(lines)
		lines = append(lines, QuoteLineRequest{Sku: sku, Qty: qty})
	}
	return lines
}

func (g *synthGen) ringUp(cart, store, mode, customer string, lines []QuoteLineRequest) error {
	quote, err := ResolvePOSQuote(g.tenantID, QuoteRequest{CartNumber: cart, Location: store, CustomerID: customer, Lines: lines})
	if err != nil {
		return fmt.Errorf("quote: %w", err)
	}
	payload, err := json.Marshal(map[string]interface{}{
		"cart_number": cart, "location": store, "payment_mode": mode, "customer_id": customer, "amount_paid": quote.AmountDue,
		"items": POSCartItemsFromQuote(quote), "gst_breakdown": quote.GSTBreakdown, "applied_offers": quote.AppliedOffers,
		"offer_discount": quote.OfferDiscount, "quote_version": quote.Version, "quote_id": quote.QuoteID,
		"reference_subtotal": quote.ReferenceSubtotal, "cashier": synthCashier, "offline_synced": false,
	})
	if err != nil {
		return err
	}
	if _, err := db.DB.Exec(fmt.Sprintf(`INSERT INTO %s.documents (id, doctype, data, status, created_by)
		VALUES ($1, 'POSCart', $2, 'Processing', $3)`, g.schema), cart, payload, synthCashier); err != nil {
		return err
	}
	for attempt := 0; ; attempt++ {
		_, _, err = FinalizePOSCheckout(g.tenantID, cart, "")
		var short *InsufficientStockError
		if err == nil || attempt > 0 || !errors.As(err, &short) {
			return err
		}
		// The till ran short: a replenishment receipt, then the same bill again.
		if rerr := g.receive(store, fmt.Sprintf("SYN-REPL-%s-%s", store, cart), map[string]int{short.SKU: 2*short.Requested + 20}); rerr != nil {
			return rerr
		}
	}
}

func (g *synthGen) partialReturn(cart, store, sku string) error {
	cmd, err := CreateReturnRequestCommand(g.tenantID, "Customer Return", store, cart, "", synthCashier, "synth:"+cart, "",
		[]ReturnItemInput{{SKU: sku, Qty: 1}}, nil)
	if err != nil {
		return err
	}
	if err := ApproveReturnRequest(g.tenantID, cmd.ReturnRequestID, synthManager); err != nil {
		return err
	}
	if err := ReceiveReturnRequest(g.tenantID, cmd.ReturnRequestID, synthManager); err != nil {
		return err
	}
	_, refundID, err := ApplyReturnQC(g.tenantID, cmd.ReturnRequestID, map[string]string{sku: "Sellable"}, synthManager)
	if err != nil {
		return err
	}
	if refundID == "" {
		return nil
	}
	if err := ApproveRefundRequest(g.tenantID, refundID, synthSupervisor); err != nil {
		return err
	}
	return ProcessRefundRequest(g.tenantID, refundID, synthManager, RefundMethodOriginalPayment)
}

// auditHistory writes the hot SKU's long history - price changes - through
// the real (signed) audit writer.
func (g *synthGen) auditHistory() error {
	price := int(g.items[0].price)
	for i := 0; i < g.shape.AuditTrail; i++ {
		next := max(100, price+10*(g.rng.IntN(21)-10))
		_ = g.measure("audit", 1, func() error {
			LogAuditEvent(g.tenantID, "synth-merchandiser", "ITEM_PRICE_CHANGE", "SUCCESS",
				fmt.Sprintf("Item %s sale price %d -> %d", g.hot, price, next))
			return nil
		})
		price = next
	}
	return nil
}

// monthEnds posts each month end's close burst - depreciation, salary
// accrual, prepaid amortisation - on the last day of each month from the
// base date on.
func (g *synthGen) monthEnds() error {
	base, _ := time.Parse("2006-01-02", synthBaseDate)
	kinds := []struct{ dr, cr string }{{"5310", "1410"}, {"5500", "2400"}, {"5400", "1800"}}
	for m := 0; m < g.shape.Months; m++ {
		date := base.AddDate(0, m+1, -1).Format("2006-01-02")
		for k := 0; k < g.shape.MonthEndEntries; k++ {
			kind := kinds[k%len(kinds)]
			amount := int64(1000+g.rng.IntN(99_000)) * 100
			doc := fmt.Sprintf("SYN-ME-%s-%03d", date, k+1)
			if err := g.measure("month_end_posting", 1, func() error {
				return PostDoubleEntry(g.tenantID, "MonthEndClose", doc, map[string]int64{kind.dr: amount}, map[string]int64{kind.cr: amount}, date, "synth:"+doc)
			}); err != nil {
				return err
			}
		}
	}
	return nil
}

// synthDigest counts what a tenant holds and hashes its business content:
// stock by SKU and location, bins and owners, the GL by account, documents
// and audit actions by kind. Generated ids, timestamps and signatures are
// left out, so one seed always hashes the same.
func synthDigest(schema string) (map[string]int64, string, error) {
	counts := map[string]int64{}
	h := sha256.New()
	section := func(name, query string) error {
		rows, err := db.DB.Query(query)
		if err != nil {
			return fmt.Errorf("%s: %w", name, err)
		}
		defer rows.Close()
		fmt.Fprintf(h, "[%s]\n", name)
		var n int64
		for rows.Next() {
			var line string
			if err := rows.Scan(&line); err != nil {
				return err
			}
			fmt.Fprintln(h, line)
			n++
		}
		counts[name] = n
		return rows.Err()
	}
	queries := []struct{ name, q string }{
		{"stock", `SELECT sku||'|'||location_code||'|'||on_hand||'|'||available FROM %s.inventory_availability ORDER BY 1`},
		{"bin_stock", `SELECT bin_code||'|'||sku||'|'||condition||'|'||qty FROM %s.bin_stock ORDER BY 1`},
		{"bin_stock_owner", `SELECT bin_code||'|'||sku||'|'||owner_id||'|'||qty FROM %s.bin_stock_owner ORDER BY 1`},
		{"gl_accounts_used", `SELECT account_code||'|'||SUM(debit)||'|'||SUM(credit)||'|'||COUNT(*) FROM %s.gl_postings GROUP BY account_code ORDER BY 1`},
		{"doctypes", `SELECT doctype||'|'||status||'|'||COUNT(*) FROM %s.documents GROUP BY doctype, status ORDER BY 1`},
		{"audit_actions", `SELECT action||'|'||COUNT(*) FROM %s.audit_logs GROUP BY action ORDER BY 1`},
	}
	for _, q := range queries {
		if err := section(q.name, fmt.Sprintf(q.q, schema)); err != nil {
			return nil, "", err
		}
	}
	for _, t := range []struct{ key, q string }{
		{"documents", `SELECT COUNT(*) FROM %s.documents`},
		{"gl_postings", `SELECT COUNT(*) FROM %s.gl_postings`},
		{"audit_logs", `SELECT COUNT(*) FROM %s.audit_logs`},
		{"bills", `SELECT COUNT(*) FROM %s.documents WHERE doctype = 'POSCart' AND status = 'Paid'`},
		{"returns", `SELECT COUNT(*) FROM %s.documents WHERE doctype = 'ReturnRequest'`},
		{"bins", `SELECT COUNT(*) FROM %s.documents WHERE doctype = 'Bin'`},
		{"items", `SELECT COUNT(*) FROM %s.documents WHERE doctype = 'Item'`},
	} {
		var n int64
		if err := db.DB.QueryRow(fmt.Sprintf(t.q, schema)).Scan(&n); err != nil {
			return nil, "", err
		}
		counts[t.key] = n
	}
	return counts, hex.EncodeToString(h.Sum(nil)), nil
}

// DeleteSyntheticTenant drops a tenant GenerateSyntheticTenant made. It
// refuses anything that is not a sandbox, so it cannot reach a real client.
func DeleteSyntheticTenant(tenantID string) error {
	var schema string
	var sandbox bool
	if err := db.DB.QueryRow(`SELECT schema_name, COALESCE(is_sandbox, FALSE) FROM public.tenants WHERE tenant_id = $1`, tenantID).
		Scan(&schema, &sandbox); err != nil {
		return err
	}
	if !sandbox || !strings.HasPrefix(schema, "tenant_") {
		return fmt.Errorf("%s is not a synthetic sandbox tenant", tenantID)
	}
	if _, err := db.DB.Exec(`DROP SCHEMA IF EXISTS ` + schema + ` CASCADE`); err != nil {
		return err
	}
	_, _ = db.DB.Exec(`DELETE FROM public.tenant_lifecycle_events WHERE tenant_id = $1`, tenantID)
	_, err := db.DB.Exec(`DELETE FROM public.tenants WHERE tenant_id = $1`, tenantID)
	InvalidateTenantLifecycleCache(tenantID)
	return err
}
