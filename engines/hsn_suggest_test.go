package engines

import (
	"custom_erp/db"
	"fmt"
	"strings"
	"testing"
)

// Stage 66 - HSN suggestions. The ranking tests are pure (master list only);
// TestSuggestHSNForTenant runs against its own throwaway tenant schema, the
// same isolation industry_lock_test.go uses, because it switches industry.

func TestHSNMasterIsWellFormed(t *testing.T) {
	master, index, err := hsnMaster()
	if err != nil {
		t.Fatalf("hsnMaster: %v", err)
	}
	if len(master) < 250 || len(index) != len(master) {
		t.Fatalf("master list has %d entries (%d indexed) - expected the full list", len(master), len(index))
	}
	// GST 2.0 rates plus the precious-metal / diamond special rates.
	allowed := map[float64]bool{0: true, 0.25: true, 1.5: true, 3: true, 5: true, 18: true, 40: true}
	for _, e := range master {
		if e.Description == "" || len(e.Keywords) == 0 || len(e.Industries) == 0 {
			t.Errorf("%s: needs a description, keywords and an industry", e.HSN)
		}
		for _, r := range e.Rates {
			if !allowed[r.Rate] {
				t.Errorf("%s: rate %v is not a GST rate", e.HSN, r.Rate)
			}
		}
		if len(e.Rates) > 1 {
			for _, r := range e.Rates {
				if r.Note == "" {
					t.Errorf("%s: a code with several rates must say when each applies", e.HSN)
				}
			}
		}
		for _, ind := range e.Industries {
			if _, ok := hsnIndustryNames[ind]; !ok {
				t.Errorf("%s: unknown industry %q", e.HSN, ind)
			}
		}
		for _, w := range append(append([]string{}, e.Keywords...), e.Related...) {
			if w == "" || w != hsnText(w) {
				t.Errorf("%s: word %q was not normalized", e.HSN, w)
			}
		}
	}
	if hsnMasterDescription("711719") == "" || hsnMasterDescription("99999999") != "" {
		t.Errorf("hsnMasterDescription does not look codes up")
	}
}

func TestHSNWordsMatching(t *testing.T) {
	w := newHSNWords("Aluminum T-Shirts, 2 Bangles & Panties")
	for _, term := range []string{"aluminium", "t shirt", "bangle", "panty", "2"} {
		if !w.has(term) {
			t.Errorf("%q should be found", term)
		}
	}
	for _, term := range []string{"alu", "shirt sleeve", "bang", "ring"} {
		if w.has(term) {
			t.Errorf("%q should not be found", term)
		}
	}
}

// topMasterHSN ranks the master list exactly as SuggestHSN does for a name.
func topMasterHSN(t *testing.T, name, industry string) string {
	t.Helper()
	master, _, err := hsnMaster()
	if err != nil {
		t.Fatal(err)
	}
	w := newHSNWords(name)
	best, bestScore := "", 0
	for i := range master {
		s, _ := scoreHSNEntry(&master[i], w)
		if s > 0 && industry != "" && master[i].inIndustry(industry) {
			s++
		}
		if s > bestScore {
			best, bestScore = master[i].HSN, s
		}
	}
	return best
}

func TestHSNMasterRanking(t *testing.T) {
	cases := []struct{ name, industry, want string }{
		// The user's own examples: zircon is not silver, silver is.
		{"Zircon Haar", "JEWELRY", "711719"},
		{"Silver Haar", "JEWELRY", "711311"},
		{"Silver Zircon Ring", "JEWELRY", "711311"},
		{"925 Sterling Silver Ring", "", "711311"},
		{"German Silver Payal", "", "711719"},
		{"Gold Plated Jhumka", "", "711719"},
		{"AD Necklace Set", "", "711719"},
		{"22K Gold Chain", "", "711319"},
		{"Silver Pooja Thali", "", "711411"},
		{"Silver Coin 10g", "", "711890"},
		{"Glass Bangles", "", "711790"},
		// Cloth / fabric / garments.
		{"Cotton Fabric", "", "5208"},
		{"Georgette Saree", "", "5407"},
		{"Banarasi Silk Saree", "", "5007"},
		{"Men Cotton Shirt", "", "620520"},
		{"Cotton T-Shirt", "", "610910"},
		{"Polo T-Shirt", "", "610510"},
		{"Denim Jeans", "", "620342"},
		{"Salwar Suit", "", "621142"},
		{"Women Kurta", "", "620630"},
		{"Leather Shoes", "", "640399"},
		// Other industries.
		{"Basmati Rice 5kg", "", "1006"},
		{"Mustard Oil 1L", "", "1514"},
		{"Cumin Seeds", "", "0909"},
		{"Tomato Seeds", "AGRICULTURE", "1209"},
		{"Engine Oil 20W40", "", "271019"},
		{"Piston Ring", "", "840991"},
		{"Car Battery", "", "850710"},
		{"TMT Bar 12mm", "", "721420"},
		{"Aluminum Sheet", "", "7606"},
		{"Copper Wire", "", "7408"},
		{"Copper Bottle", "", "741810"},
		{"Steel Kadai", "", "7323"},
		{"PVC Pipe 1 inch", "", "391723"},
		{"Toughened Glass", "", "7007"},
		{"Paracetamol Tablet 500mg", "", "300490"},
		{"Android Tablet", "", "847130"},
		{"Surgical Gloves", "", "401511"},
		{"Solar Panel 540W", "", "854143"},
		// The industry decides between two readings of one word.
		{"Muffler", "CLOTHING", "621420"},
		{"Muffler", "AUTO", "870892"},
	}
	for _, c := range cases {
		if got := topMasterHSN(t, c.name, c.industry); got != c.want {
			t.Errorf("%q (%s): top suggestion %s, want %s", c.name, c.industry, got, c.want)
		}
	}
}

func TestSuggestHSNForTenant(t *testing.T) {
	db.InitDB(testConnStr())
	tenantID, schemaName := uniqueLifecycleTenant(t)
	defer dropLifecycleTenant(tenantID, schemaName)
	if _, err := ProvisionTenantSchema(tenantID, schemaName, "0.1.0-test"); err != nil {
		t.Fatalf("ProvisionTenantSchema: %v", err)
	}

	// Choosing the industry loads its codes into the HSN list.
	if err := SwitchIndustryProfile(tenantID, "../public/profiles/jewelry.json", "alice", false, ""); err != nil {
		t.Fatalf("SwitchIndustryProfile: %v", err)
	}
	var desc, rate string
	if err := db.DB.QueryRow(fmt.Sprintf(`SELECT data->>'description', COALESCE(data->>'gst_rate','') FROM %s.documents WHERE id = 'HSN-711719' AND doctype = 'HSNCode'`, schemaName)).Scan(&desc, &rate); err != nil {
		t.Fatalf("industry switch did not add HSN 711719 to the HSN list: %v", err)
	}
	if desc == "" || rate != "3" {
		t.Errorf("seeded HSN 711719 = %q at %q%%, want the master description at 3%%", desc, rate)
	}

	// No name yet: the industry's codes, from the client's own list now.
	res, err := SuggestHSN(tenantID, HSNSuggestQuery{HistoryDoctypes: []string{"Item"}})
	if err != nil {
		t.Fatalf("SuggestHSN: %v", err)
	}
	if res.Industry != "JEWELRY" || len(res.Suggestions) == 0 || !res.Suggestions[0].InCatalogue {
		t.Fatalf("empty name should list the jewellery codes on the HSN list, got industry %q, %+v", res.Industry, res.Suggestions)
	}

	// The master list's reading of the name...
	res, err = SuggestHSN(tenantID, HSNSuggestQuery{Name: "Zircon Haar", HistoryDoctypes: []string{"Item"}})
	if err != nil {
		t.Fatalf("SuggestHSN: %v", err)
	}
	if len(res.Suggestions) == 0 || res.Suggestions[0].HSN != "711719" || !strings.HasPrefix(res.Suggestions[0].Group, "Suggested for") {
		t.Fatalf("Zircon Haar: want 711719 first under Suggested, got %+v", res.Suggestions)
	}
	if r := res.Suggestions[0].GSTRate; r == nil || *r != 3 {
		t.Errorf("711719 should carry its 3%% rate, got %v", r)
	}

	// ...gives way to how this business already classifies such items.
	if _, err := db.DB.Exec(fmt.Sprintf(`INSERT INTO %s.documents (id, doctype, data, status, created_by)
		VALUES ('ITEM-HSN66-T1', 'Item', '{"name":"Zircon Haar Green","hsn_code":"7113 19","gst_rate":3}', 'Active', 'test')`, schemaName)); err != nil {
		t.Fatalf("insert item: %v", err)
	}
	res, err = SuggestHSN(tenantID, HSNSuggestQuery{Name: "Zircon Haar", HistoryDoctypes: []string{"Item"}})
	if err != nil {
		t.Fatalf("SuggestHSN: %v", err)
	}
	if len(res.Suggestions) == 0 || res.Suggestions[0].HSN != "711319" || !strings.Contains(res.Suggestions[0].Reason, "Zircon Haar Green") {
		t.Fatalf("an existing item with the same words should lead, got %+v", res.Suggestions)
	}
	// Without the right to read Items, their names are not quoted.
	res, _ = SuggestHSN(tenantID, HSNSuggestQuery{Name: "Zircon Haar"})
	if len(res.Suggestions) == 0 || res.Suggestions[0].HSN != "711719" {
		t.Errorf("history must not be used without HistoryDoctypes, got %+v", res.Suggestions)
	}

	// Typing narrows: digits by code, words by description.
	res, _ = SuggestHSN(tenantID, HSNSuggestQuery{Name: "Zircon Haar", Query: "7113"})
	for _, s := range res.Suggestions {
		if !strings.HasPrefix(s.HSN, "7113") {
			t.Errorf("query 7113 returned %s", s.HSN)
		}
	}
	res, _ = SuggestHSN(tenantID, HSNSuggestQuery{Query: "bedsheet"})
	if len(res.Suggestions) == 0 || res.Suggestions[0].HSN != "630221" {
		t.Errorf("query bedsheet should find 630221 from the master list, got %+v", res.Suggestions)
	}

	// One code's rates - how the GST box follows the HSN box.
	res, _ = SuggestHSN(tenantID, HSNSuggestQuery{HSN: "6205 20"})
	if len(res.Suggestions) != 1 || len(res.Suggestions[0].Rates) != 2 || res.Suggestions[0].GSTRate != nil {
		t.Fatalf("620520 should offer its two price-banded rates and fill none, got %+v", res.Suggestions)
	}
	res, _ = SuggestHSN(tenantID, HSNSuggestQuery{HSN: "711319", HistoryDoctypes: []string{"Item"}})
	if len(res.Suggestions) != 1 || res.Suggestions[0].GSTRate == nil || *res.Suggestions[0].GSTRate != 3 {
		t.Errorf("711319 should carry 3%%, got %+v", res.Suggestions)
	}
	// A rate the client set on its own HSN list wins, and is one of the
	// choices shown - labelled as theirs - even when the master has others.
	if _, err := db.DB.Exec(fmt.Sprintf(`INSERT INTO %s.documents (id, doctype, data, status, created_by)
		VALUES ('HSN-620520', 'HSNCode', '{"code":"HSN-620520","hsn":"620520","description":"","gst_rate":12,"status":"Active"}', 'Active', 'test')`, schemaName)); err != nil {
		t.Fatalf("insert HSN code: %v", err)
	}
	res, _ = SuggestHSN(tenantID, HSNSuggestQuery{HSN: "620520"})
	if len(res.Suggestions) != 1 || res.Suggestions[0].GSTRate == nil || *res.Suggestions[0].GSTRate != 12 ||
		len(res.Suggestions[0].Rates) != 3 || res.Suggestions[0].Rates[0].Note != "Set on your HSN list" {
		t.Errorf("620520 at 12%% on the HSN list should fill 12 and list it first, got %+v", res.Suggestions)
	}
}
