package engines

import (
	"custom_erp/db"
	"fmt"
	"sort"
	"strings"
	"testing"
)

// Stage 49.6.2: every personal or personal-looking doctype field has a row in
// the field-purpose register, and every row names a field that still exists.
// The field list is the database's own doctype_fields - what the generic form
// renders and the import accepts - so a migration that adds a field is caught
// whichever file it lives in.
func TestStage4962EveryPersonalFieldHasARegisteredPurpose(t *testing.T) {
	db.InitDB(testConnStr())
	schema, err := db.GetTenantSchema("default")
	if err != nil {
		t.Fatal(err)
	}
	rows, err := db.DB.Query(fmt.Sprintf(`SELECT DISTINCT doctype_name, fieldname FROM %s.doctype_fields`, schema))
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	present := map[string]bool{}
	var missing []string
	for rows.Next() {
		var dt, f string
		if err := rows.Scan(&dt, &f); err != nil {
			t.Fatal(err)
		}
		present[dt+"."+f] = true
		if !IsPersonalDataField(dt, f) {
			continue
		}
		if _, ok := LookupPersonalDataField(dt, f); !ok {
			missing = append(missing, dt+"."+f)
		}
	}
	if len(present) < 1000 {
		t.Fatalf("only %d doctype fields in %s - the database is not migrated, so this proves nothing", len(present), schema)
	}
	var stale []string
	for dt, fields := range personalDataRegister {
		for f := range fields {
			if !present[dt+"."+f] {
				stale = append(stale, dt+"."+f)
			}
		}
	}
	sort.Strings(missing)
	sort.Strings(stale)
	if len(missing) > 0 {
		t.Errorf("personal-looking fields with no purpose in engines/personal_data_register.go (add a row: why it is collected, on what basis, how long, who reads it - or BasisNotPersonal if it is business data):\n  %s", strings.Join(missing, "\n  "))
	}
	if len(stale) > 0 {
		t.Errorf("register rows for fields no doctype declares any more - remove them:\n  %s", strings.Join(stale, "\n  "))
	}
	for _, q := range PersonalDataReviewQuestions() {
		t.Logf("minimization question for the privacy owner: %s", q)
	}
}

// Every row is complete and uses the fixed vocabularies; a not-personal row is
// genuinely business data; and the register agrees with sensitive_fields.go
// about the personal fields that file already restricts.
func TestStage4962RegisterRowsAreComplete(t *testing.T) {
	for dt, fields := range personalDataRegister {
		for f, row := range fields {
			name := dt + "." + f
			if strings.TrimSpace(row.Purpose) == "" || strings.TrimSpace(row.Retention) == "" || strings.TrimSpace(row.Readers) == "" {
				t.Errorf("%s: purpose, retention and readers are all required", name)
			}
			if !personalDataKinds[row.Kind] {
				t.Errorf("%s: kind %q is not one of the register's kinds", name, row.Kind)
			}
			if !personalDataBases[row.LegalBasis] {
				t.Errorf("%s: legal basis %q is not one of the register's bases", name, row.LegalBasis)
			}
			if (row.LegalBasis == BasisNotPersonal) != (row.Kind == "business") {
				t.Errorf("%s: a business row and a not-personal basis go together (kind %q, basis %q)", name, row.Kind, row.LegalBasis)
			}
			if row.LegalBasis == BasisNotPersonal && personDoctypes[dt] {
				t.Errorf("%s: a field of a person doctype is personal data", name)
			}
			if !IsPersonalDataField(dt, f) {
				t.Errorf("%s: neither a person doctype field nor a personal-looking name - it does not belong in the register", name)
			}
		}
	}
	for dt, fields := range sensitiveFields {
		for f, rule := range fields {
			if rule.Category != SensitiveCategoryPersonal && rule.Category != SensitiveCategoryBank && !(dt == "Employee") {
				continue
			}
			if row, ok := LookupPersonalDataField(dt, f); ok && row.LegalBasis != BasisNotPersonal && !strings.Contains(row.Readers, "sensitive_fields.go") {
				t.Errorf("%s.%s is restricted by sensitive_fields.go but the register's readers do not say so: %q", dt, f, row.Readers)
			}
		}
	}

	// The pattern is the gate: it must catch the shapes it exists for and
	// leave ordinary business fields alone.
	for _, f := range []string{"phone", "customer_phone", "contact_email", "shipping_address", "date_of_birth", "pan", "aadhaar_number", "bank_account_no", "driver_name", "vehicle_no", "consent_sms", "ip_address"} {
		if !personalDataFieldPattern.MatchString(f) {
			t.Errorf("the personal-data pattern misses %q", f)
		}
	}
	for _, f := range []string{"item_name", "location", "span", "company", "quantity", "template_name", "shipment_id", "description"} {
		if personalDataFieldPattern.MatchString(f) {
			t.Errorf("the personal-data pattern matches the business field %q", f)
		}
	}
}
