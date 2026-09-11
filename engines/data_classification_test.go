package engines

import "testing"

// A category named in sensitive_fields.go's rule table but missing from this
// registry would mean 49.6.1 silently fell out of sync with 47.1.3 the next
// time someone adds a sensitive field with a new category. This walks every
// rule actually in use, not just the six named constants, so it catches a
// typo'd or ad-hoc category string too.
func TestSensitiveFieldCategoriesAreAllClassified(t *testing.T) {
	seen := map[string]bool{}
	for doctype, rules := range sensitiveFields {
		for field, rule := range rules {
			seen[rule.Category] = true
			if _, ok := ClassifyCategory(rule.Category); !ok {
				t.Errorf("%s.%s uses category %q with no entry in dataClassifications", doctype, field, rule.Category)
			}
		}
	}
	if len(seen) == 0 {
		t.Fatal("expected sensitive_fields.go to declare at least one category - test setup looks broken")
	}
}

func TestEveryDataFlowClassificationIsComplete(t *testing.T) {
	for _, category := range AllDataFlowCategories() {
		c, _ := ClassifyCategory(category)
		if c.Tier == "" {
			t.Errorf("%s: Tier must not be empty", category)
		}
		if c.Purpose == "" {
			t.Errorf("%s: Purpose must not be empty", category)
		}
		if c.Deletion == "" {
			t.Errorf("%s: Deletion must not be empty - even an open question must be recorded, not silently absent", category)
		}
		if c.RetentionTrigger == "" {
			t.Errorf("%s: RetentionTrigger must not be empty", category)
		}
	}
}

func TestClassificationTiersMatchExpectedSensitivity(t *testing.T) {
	cases := []struct {
		category string
		tier     DataTier
		privacy  PrivacyCategory
	}{
		{SensitiveCategoryPayroll, TierRestricted, PrivacySensitiveFinancial},
		{SensitiveCategoryBank, TierRestricted, PrivacySensitiveFinancial},
		{SensitiveCategorySecret, TierRestricted, PrivacyAuthentication},
		{SensitiveCategoryPersonal, TierConfidential, PrivacyPersonal},
		{SensitiveCategoryCost, TierConfidential, ""},
		{DataFlowSigningKey, TierRestricted, ""},
		{DataFlowAuditEvidence, TierRestricted, PrivacyAudit},
	}
	for _, tc := range cases {
		c, ok := ClassifyCategory(tc.category)
		if !ok {
			t.Fatalf("%s: expected a classification entry", tc.category)
		}
		if c.Tier != tc.tier {
			t.Errorf("%s: tier = %q, want %q", tc.category, c.Tier, tc.tier)
		}
		if c.Privacy != tc.privacy {
			t.Errorf("%s: privacy = %q, want %q", tc.category, c.Privacy, tc.privacy)
		}
	}
}

func TestClassifyDoctypeFieldResolvesThroughSensitiveFields(t *testing.T) {
	c, ok := ClassifyDoctypeField("Payslip", "gross_pay")
	if !ok {
		t.Fatal("expected Payslip.gross_pay to resolve to a classification")
	}
	if c.Tier != TierRestricted {
		t.Errorf("Payslip.gross_pay tier = %q, want restricted", c.Tier)
	}

	if _, ok := ClassifyDoctypeField("Customer", "phone"); ok {
		t.Error("Customer.phone is deliberately uncovered by sensitive_fields.go and must not resolve to a classification")
	}
	if _, ok := ClassifyDoctypeField("NoSuchDoctype", "whatever"); ok {
		t.Error("an unknown doctype must not resolve to a classification")
	}
}
