package engines

import (
	"custom_erp/db"
	"fmt"
)

// Stage 41: the data behind the product's setup guidance.
//
// The problem this solves: a user standing in Purchase Orders who needs a
// Vendor that does not exist yet has no way to learn that from the screen
// they are on. The picker is simply empty. Stage 30.5.1 fixed that for the
// one case where a list renders and comes back with zero rows, but a screen
// can only say "no vendors exist" *after* it has queried vendors - which is
// too late for a banner at the top of the page, and impossible for a screen
// that has not queried them at all.
//
// So: one cheap query returns, for every Master record type in the tenant,
// how many records it actually has. The frontend holds that map and can
// answer "is X set up?" instantly, anywhere, without a per-screen round trip.
//
// Deliberately NOT a hand-written list of "important" masters. Every Master
// doctype is included, derived from doctype_meta, so a record type added by a
// migration or through the Database Schema Design screen is covered the day
// it exists with no code change here - the same register-once posture the
// report and settings registries use.

// SetupStatusEntry is one Master record type and how populated it is.
type SetupStatusEntry struct {
	Doctype string `json:"doctype"`
	Module  string `json:"module"`
	// Count is live, non-deleted records. Zero is what "not set up yet" means.
	Count int `json:"count"`
	// Active is the subset that is not Inactive/Cancelled/Rejected. A tenant
	// whose only two Vendors are both Inactive is, for the purpose of picking
	// one on a Purchase Order, not set up - and a hint that said "2 vendors
	// exist" while the picker stayed empty would be worse than no hint.
	Active int `json:"active"`
}

// GetSetupStatus returns the record counts for every Master doctype in one
// query. Called once per session by the frontend (and refreshed after a
// master is created), so it is a page-load-cost read, not a per-keystroke one.
func GetSetupStatus(tenantID string) ([]SetupStatusEntry, error) {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return nil, err
	}

	// LEFT JOIN against a grouped subquery rather than a correlated count per
	// doctype: one pass over documents for the whole answer, and a Master with
	// no rows at all still appears (with 0) instead of dropping out - which is
	// precisely the row the caller most needs.
	//
	// The status exclusion list is the union of the "switched off" statuses
	// this codebase uses across its masters; a doctype using none of them
	// simply has Active == Count.
	rows, err := db.DB.Query(fmt.Sprintf(`
		SELECT m.name,
		       COALESCE(m.module, ''),
		       COALESCE(c.total, 0),
		       COALESCE(c.active, 0)
		FROM %s.doctype_meta m
		LEFT JOIN (
			SELECT doctype,
			       COUNT(*) AS total,
			       COUNT(*) FILTER (WHERE COALESCE(status, '') NOT IN ('Inactive', 'Cancelled', 'Rejected', 'Archived')) AS active
			FROM %s.documents
			WHERE deleted_at IS NULL
			GROUP BY doctype
		) c ON c.doctype = m.name
		WHERE COALESCE(m.document_type, 'Master') = 'Master'
		ORDER BY m.name`, schema, schema))
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []SetupStatusEntry{}
	for rows.Next() {
		var e SetupStatusEntry
		if err := rows.Scan(&e.Doctype, &e.Module, &e.Count, &e.Active); err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

// LocalizationInfo is what the frontend needs to apply the tenant's country
// rules in the browser: which country is configured, its phone shape, and the
// full list so a Configuration screen can offer alternatives.
//
// Returned to any authenticated user (not just an admin) because every screen
// with a phone field needs the rule to set an input's maxlength and inline
// hint - the same reasoning that makes GET /api/v1/me/permissions and
// /me/modules readable by a plain Cashier session.
type LocalizationInfo struct {
	Country   string             `json:"country"`
	Rule      CountryPhoneRule   `json:"rule"`
	Countries []CountryPhoneRule `json:"countries"`
	// PhoneFieldTokens lets the frontend recognise a phone field by the same
	// rule the server uses, instead of keeping a second hardcoded list that
	// would drift the first time a token is added on one side only.
	PhoneFieldTokens []string `json:"phone_field_tokens"`
}

// GetLocalizationInfo resolves the tenant's configured country into everything
// a client needs to enforce the same rules the server will.
func GetLocalizationInfo(tenantID string) LocalizationInfo {
	iso := TenantPhoneCountry(tenantID)
	return LocalizationInfo{
		Country:          iso,
		Rule:             PhoneRuleFor(iso),
		Countries:        PhoneCountries(),
		PhoneFieldTokens: append([]string(nil), phoneFieldTokens...),
	}
}
