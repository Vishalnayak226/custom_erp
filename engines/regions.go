package engines

import (
	"sort"
	"strings"
)

// States and other first-level regions per country (Stage 76.1).
//
// Every state field in the product was a free-text box, so one vendor said
// "Karnataka", the next "KA", the next "karnataka " - and the place-of-supply
// engine had to guess which was which before it could pick IGST or CGST+SGST.
// A state is now chosen from the tenant's home country's list, stored by its
// name, and its short code (KA) and tax code (29) are derived from it wherever
// a setup needs them - never typed.
//
// One table per country, in code like the phone table (engines/phone.go):
// adding a country is one entry here and nothing else. A country with no
// entry keeps a free-text state box rather than an empty dropdown.

// Region is one state / union territory / province.
type Region struct {
	// Name is what is stored in a document's state field.
	Name string `json:"name"`
	// Short is the ISO 3166-2 subdivision code without the country prefix
	// ("KA" for IN-KA). Filled into a setup's state_code field.
	Short string `json:"short"`
	// TaxCode is the code the country's tax registration carries, where it
	// has one: India's two-digit GST state code ("29"), the first two
	// characters of every GSTIN registered there.
	TaxCode string `json:"tax_code,omitempty"`
	// Legacy marks a code that is still recognised on old registrations but
	// is no longer a choice: India's 25 (Daman & Diu, merged into 26 in 2020),
	// 28 (pre-2014 Andhra Pradesh) and 99 (Centre Jurisdiction).
	Legacy bool `json:"-"`
	// Aliases are other spellings in real use that resolve to this region.
	Aliases []string `json:"-"`
}

// regionsByCountry is the table. India's names follow the GST portal; the
// short codes are ISO 3166-2:IN as amended in 2023 (CG, OD, TS, UK - the
// older CT, OR, TG, UT are accepted as aliases). Telangana's vehicle plates
// moved to TG in 2024, but ISO still lists TS, and ISO is what this follows.
var regionsByCountry = map[string][]Region{
	"IN": {
		{Name: "Jammu and Kashmir", Short: "JK", TaxCode: "01", Aliases: []string{"Jammu & Kashmir", "J&K"}},
		{Name: "Himachal Pradesh", Short: "HP", TaxCode: "02"},
		{Name: "Punjab", Short: "PB", TaxCode: "03"},
		{Name: "Chandigarh", Short: "CH", TaxCode: "04"},
		{Name: "Uttarakhand", Short: "UK", TaxCode: "05", Aliases: []string{"Uttaranchal", "UT"}},
		{Name: "Haryana", Short: "HR", TaxCode: "06"},
		{Name: "Delhi", Short: "DL", TaxCode: "07", Aliases: []string{"New Delhi", "NCT of Delhi"}},
		{Name: "Rajasthan", Short: "RJ", TaxCode: "08"},
		{Name: "Uttar Pradesh", Short: "UP", TaxCode: "09"},
		{Name: "Bihar", Short: "BR", TaxCode: "10"},
		{Name: "Sikkim", Short: "SK", TaxCode: "11"},
		{Name: "Arunachal Pradesh", Short: "AR", TaxCode: "12"},
		{Name: "Nagaland", Short: "NL", TaxCode: "13"},
		{Name: "Manipur", Short: "MN", TaxCode: "14"},
		{Name: "Mizoram", Short: "MZ", TaxCode: "15"},
		{Name: "Tripura", Short: "TR", TaxCode: "16"},
		{Name: "Meghalaya", Short: "ML", TaxCode: "17"},
		{Name: "Assam", Short: "AS", TaxCode: "18"},
		{Name: "West Bengal", Short: "WB", TaxCode: "19"},
		{Name: "Jharkhand", Short: "JH", TaxCode: "20"},
		{Name: "Odisha", Short: "OD", TaxCode: "21", Aliases: []string{"Orissa", "OR"}},
		{Name: "Chhattisgarh", Short: "CG", TaxCode: "22", Aliases: []string{"Chattisgarh", "CT"}},
		{Name: "Madhya Pradesh", Short: "MP", TaxCode: "23"},
		{Name: "Gujarat", Short: "GJ", TaxCode: "24"},
		{Name: "Daman and Diu", Short: "DD", TaxCode: "25", Legacy: true, Aliases: []string{"Daman & Diu"}},
		{Name: "Dadra and Nagar Haveli and Daman and Diu", Short: "DH", TaxCode: "26",
			Aliases: []string{"Dadra & Nagar Haveli and Daman & Diu", "Dadra and Nagar Haveli", "Dadra & Nagar Haveli", "DN"}},
		{Name: "Maharashtra", Short: "MH", TaxCode: "27"},
		{Name: "Andhra Pradesh (Old)", TaxCode: "28", Legacy: true},
		{Name: "Karnataka", Short: "KA", TaxCode: "29"},
		{Name: "Goa", Short: "GA", TaxCode: "30"},
		{Name: "Lakshadweep", Short: "LD", TaxCode: "31"},
		{Name: "Kerala", Short: "KL", TaxCode: "32"},
		{Name: "Tamil Nadu", Short: "TN", TaxCode: "33"},
		{Name: "Puducherry", Short: "PY", TaxCode: "34", Aliases: []string{"Pondicherry"}},
		{Name: "Andaman and Nicobar Islands", Short: "AN", TaxCode: "35", Aliases: []string{"Andaman & Nicobar Islands", "Andaman and Nicobar"}},
		{Name: "Telangana", Short: "TS", TaxCode: "36", Aliases: []string{"TG"}},
		{Name: "Andhra Pradesh", Short: "AP", TaxCode: "37"},
		{Name: "Ladakh", Short: "LA", TaxCode: "38"},
		{Name: "Other Territory", Short: "OT", TaxCode: "97"},
		{Name: "Centre Jurisdiction", TaxCode: "99", Legacy: true},
	},
}

// regionIndex resolves any accepted spelling of a region - name, alias, short
// code or tax code - to its entry, per country. Built once from the table so
// the two cannot drift.
var regionIndex = func() map[string]map[string]Region {
	out := make(map[string]map[string]Region, len(regionsByCountry))
	for country, regions := range regionsByCountry {
		idx := map[string]Region{}
		for _, r := range regions {
			keys := append([]string{r.Name, r.Short, r.TaxCode}, r.Aliases...)
			for _, k := range keys {
				if n := normalizeStateName(k); n != "" {
					if _, taken := idx[n]; !taken {
						idx[n] = r
					}
				}
			}
		}
		out[country] = idx
	}
	return out
}()

// CountryRegions returns the regions a user may choose for a country, sorted
// by name, or nil when the product has no list for it (the state stays a
// free-text box there).
func CountryRegions(country string) []Region {
	all := regionsByCountry[strings.ToUpper(strings.TrimSpace(country))]
	if len(all) == 0 {
		return nil
	}
	out := make([]Region, 0, len(all))
	for _, r := range all {
		if !r.Legacy {
			out = append(out, r)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

// HasRegionList reports whether a country has a state list.
func HasRegionList(country string) bool {
	return len(regionsByCountry[strings.ToUpper(strings.TrimSpace(country))]) > 0
}

// ResolveRegion finds the region a typed or stored value names, accepting the
// name in any case/spacing, an alias, the short code or the tax code. Legacy
// entries resolve too, so an old registration is still recognised.
func ResolveRegion(country, value string) (Region, bool) {
	n := normalizeStateName(value)
	if n == "" {
		return Region{}, false
	}
	r, ok := regionIndex[strings.ToUpper(strings.TrimSpace(country))][n]
	return r, ok
}

// RegionFromTaxCode finds a region by its tax code ("29" -> Karnataka).
func RegionFromTaxCode(country, code string) (Region, bool) {
	code = strings.TrimSpace(code)
	for _, r := range regionsByCountry[strings.ToUpper(strings.TrimSpace(country))] {
		if r.TaxCode != "" && r.TaxCode == code {
			return r, true
		}
	}
	return Region{}, false
}
