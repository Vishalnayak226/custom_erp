package engines

import (
	"custom_erp/db"
	"encoding/json"
	"strings"
	"testing"
)

// Item 273.3: each PO line shows this vendor's last issued rate for the item
// and a cheaper vendor of the last 180 days. Only issued POs count, the PO
// being edited is not its own last rate, rates compare after discounts and in
// this PO's GST mode, a blocked vendor is never suggested, and a rise above
// procurement.last_rate_alert_pct is flagged.
func TestPOLinesShowTheLastRatePaid(t *testing.T) {
	db.InitDB(testConnStr())
	tenantID, schema := uniqueLifecycleTenant(t)
	defer dropLifecycleTenant(tenantID, schema)
	if _, err := ProvisionTenantSchema(tenantID, schema, "0.1.0-test"); err != nil {
		t.Fatalf("ProvisionTenantSchema: %v", err)
	}
	insert := func(id, doctype, status string, daysAgo int, data map[string]interface{}) {
		t.Helper()
		raw, _ := json.Marshal(data)
		if _, err := db.DB.Exec("INSERT INTO "+schema+".documents (id, doctype, data, status, created_by, created_at) VALUES ($1, $2, $3, $4, 'system', now() - make_interval(days => $5))",
			id, doctype, raw, status, daysAgo); err != nil {
			t.Fatalf("insert %s: %v", id, err)
		}
	}
	po := func(id, status, vendor, date, mode string, daysAgo int, lines ...map[string]interface{}) {
		raw, _ := json.Marshal(lines)
		insert(id, "PurchaseOrder", status, daysAgo, map[string]interface{}{"po_number": id, "vendor": vendor,
			"order_date": date, "gst_mode": mode, "items": string(raw)})
	}
	line := func(sku string, qty int, rate float64) map[string]interface{} {
		return map[string]interface{}{"sku": sku, "qty": qty, "rate": rate}
	}
	insert("LR-V1", "Vendor", "Active", 0, map[string]interface{}{"code": "LR-V1", "name": "Vendor One"})
	insert("LR-V2", "Vendor", "Active", 0, map[string]interface{}{"code": "LR-V2", "name": "Vendor Two"})
	insert("LR-V3", "Vendor", "Blocked", 0, map[string]interface{}{"code": "LR-V3", "name": "Vendor Three"})

	discounted := line("LR-TEE", 10, 100)
	discounted["net_value"] = 950.0 // 5% off: one unit cost 95
	po("PO-1", "Approved", "LR-V1", "2026-05-01", GSTModeExclusive, 200, line("LR-TEE", 10, 90))
	po("PO-2", "Received", "LR-V1", "2026-10-01", GSTModeExclusive, 50, discounted)
	po("PO-3", "Draft", "LR-V1", "2026-11-20", GSTModeExclusive, 2, line("LR-TEE", 10, 50))               // not issued
	po("PO-4", "Approved", "LR-V2", "2026-10-20", GSTModeExclusive, 30, line("LR-TEE", 5, 92))            // cheaper vendor
	po("PO-5", "Approved", "LR-V3", "2026-10-25", GSTModeExclusive, 25, line("LR-TEE", 5, 80))            // blocked vendor
	po("PO-6", "Closed", "LR-V2", "2025-09-01", GSTModeExclusive, 400, line("LR-TEE", 5, 70))             // over a year old
	po("PO-7", "Approved", "LR-V1", "2026-10-10", GSTModeInclusive, 40, line("LR-CAP", 5, 105))           // 100 before 5% GST
	po("PO-8", "Approved", "LR-V1", "2026-11-25", GSTModeExclusive, 1, line("LR-TEE", 10, 60))            // the PO being edited
	po("PO-9", "Partially Received", "LR-V2", "2026-06-01", GSTModeExclusive, 190, line("LR-CAP", 5, 50)) // cheaper, but over 180 days

	preview := func(mode, exclude string, lines ...POLinePreview) POPreview {
		t.Helper()
		pv := POPreview{GSTMode: mode, Lines: lines}
		if err := StampPOLastRates(tenantID, map[string]interface{}{"vendor": "LR-V1", "last_rate_exclude": exclude}, &pv); err != nil {
			t.Fatalf("StampPOLastRates: %v", err)
		}
		return pv
	}
	pl := func(sku string, qty int, rate float64) POLinePreview {
		return POLinePreview{POLineInput: POLineInput{SKU: sku, Qty: qty, Rate: rate}, GSTRate: 5}
	}

	t.Run("the PO being edited is not its own last rate", func(t *testing.T) {
		if got := preview(GSTModeExclusive, "", pl("LR-TEE", 10, 105)).Lines[0]; got.LastRatePO != "PO-8" {
			t.Fatalf("without an exclusion the newest issued PO is the last rate, got %q", got.LastRatePO)
		}
		got := preview(GSTModeExclusive, "PO-8", pl("LR-TEE", 10, 105)).Lines[0]
		if got.LastRatePO != "PO-2" || got.LastRate != 95 || got.LastRateDate != "2026-10-01" {
			t.Fatalf("last rate = %v on %q (%s), want 95 after discount on PO-2 (2026-10-01); a Draft never counts", got.LastRate, got.LastRatePO, got.LastRateDate)
		}
		if got.LastRateUpPct != 10.5 || !got.LastRateAlert {
			t.Fatalf("105 against 95: up %v alert %v, want 10.5 and red above the default 5%%", got.LastRateUpPct, got.LastRateAlert)
		}
		if !strings.Contains(got.LastRateNote, "Last ₹95 · PO-2, 1 Oct 2026") || !strings.Contains(got.LastRateNote, "now +10.5%") {
			t.Fatalf("note = %q", got.LastRateNote)
		}
		if got.BetterVendor != "Vendor Two" || got.BetterRate != 92 || !strings.Contains(got.BetterRateNote, "Cheaper: Vendor Two ₹92") {
			t.Fatalf("cheaper = %q %v (%q), want Vendor Two at 92; a blocked vendor is never suggested", got.BetterVendor, got.BetterRate, got.BetterRateNote)
		}
	})

	t.Run("an inclusive PO is compared after taking the GST out", func(t *testing.T) {
		got := preview(GSTModeExclusive, "PO-8", pl("LR-CAP", 5, 100)).Lines[0]
		if got.LastRate != 100 || got.LastRateUpPct != 0 || got.LastRateAlert || strings.Contains(got.LastRateNote, "now") {
			t.Fatalf("CAP: last %v up %v alert %v note %q, want 100 with no change", got.LastRate, got.LastRateUpPct, got.LastRateAlert, got.LastRateNote)
		}
		if got.BetterVendor != "" {
			t.Fatalf("a rate over 180 days old is not a cheaper vendor, got %q", got.BetterVendor)
		}
		in := preview(GSTModeInclusive, "PO-8", pl("LR-TEE", 10, 99.75)).Lines[0]
		if in.LastRate != 99.75 || in.LastRateUpPct != 0 {
			t.Fatalf("an exclusive 95 on an inclusive PO reads 99.75, got %v (up %v)", in.LastRate, in.LastRateUpPct)
		}
	})

	t.Run("no history, no price and a higher threshold", func(t *testing.T) {
		got := preview(GSTModeExclusive, "PO-8", pl("LR-NEW", 1, 10), pl("LR-TEE", 10, 0)).Lines
		if got[0].LastRateNote != "" || got[0].BetterRateNote != "" {
			t.Fatalf("an item never bought shows nothing, got %q / %q", got[0].LastRateNote, got[0].BetterRateNote)
		}
		if got[1].LastRate != 95 || got[1].LastRateAlert || strings.Contains(got[1].LastRateNote, "now") || got[1].BetterRate != 92 {
			t.Fatalf("no rate typed yet: %+v, want the last rate and the cheaper vendor, no change shown", got[1])
		}
		if err := SetSetting(tenantID, SettingLastRateAlertPct, "20", "test"); err != nil {
			t.Fatalf("SetSetting: %v", err)
		}
		if up := preview(GSTModeExclusive, "PO-8", pl("LR-TEE", 10, 105)).Lines[0]; up.LastRateAlert || up.LastRateUpPct != 10.5 {
			t.Fatalf("10.5%% under a 20%% threshold: alert %v", up.LastRateAlert)
		}
	})
}
