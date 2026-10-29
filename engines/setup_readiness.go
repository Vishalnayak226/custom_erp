package engines

import (
	"custom_erp/db"
	"fmt"
	"sort"
	"strings"
)

// Stage 47.12.4 - readiness is not row count.
//
// GetSetupPath (setup_path.go) marks a step Done when its masters have an
// active record. That is the guided setup's progress, and it stays. But "one
// legal entity exists" is not "the business can issue a lawful invoice": the
// entity may have no GSTIN, a GSTIN whose check digit is wrong, a state that
// disagrees with the GSTIN, a store linked to no entity, or an invoice series
// that prints 18 characters. So each step also carries the checks it was held
// to, their outcome, the state they add up to and how serious the worst
// failure is - recomputed from live data on every call, like the rest of the
// path.
//
// The states are the item's: Draft (nothing entered yet), Invalid (entered
// but a blocking check fails), Awaiting Approval (a record still waits for
// its checker), Ready, and Waived (an optional step the client left - the
// standard behaviour applies, as Stage 91.3 decided).

// SetupValidatorVersion names the rule set a step was judged by. Bump it
// whenever a check is added, removed or changes what it accepts, so a
// readiness answer kept as evidence says which rules produced it.
const SetupValidatorVersion = "47.12.4-v1"

// Readiness states.
const (
	SetupStateDraft            = "Draft"
	SetupStateInvalid          = "Invalid"
	SetupStateAwaitingApproval = "Awaiting Approval"
	SetupStateReady            = "Ready"
	SetupStateWaived           = "Waived"
)

// Check severities: a Blocking failure keeps the step from Ready; a Warning
// is shown but does not.
const (
	SetupSeverityBlocking = "Blocking"
	SetupSeverityWarning  = "Warning"
)

// SetupCheck is one validator's answer for one step. A passed check is the
// step's evidence; a failed one names what is wrong in Detail.
type SetupCheck struct {
	ID       string `json:"id"`
	Label    string `json:"label"`
	Severity string `json:"severity"`
	Passed   bool   `json:"passed"`
	Detail   string `json:"detail,omitempty"`
}

// setupLiveStatus is "a record that counts": not switched off and not still
// waiting for its checker. %s is the table alias with its dot, or "".
const setupLiveStatus = `COALESCE(%sstatus, 'Active') NOT IN ('Inactive', 'Cancelled', 'Rejected', 'Archived', 'Pending Approval', 'Draft')`

func setupLive(alias string) string { return fmt.Sprintf(setupLiveStatus, alias) }

// setupNamed lists up to five offending records, then how many more.
func setupNamed(codes []string) string {
	sort.Strings(codes)
	if len(codes) <= 5 {
		return strings.Join(codes, ", ")
	}
	return fmt.Sprintf("%s and %d more", strings.Join(codes[:5], ", "), len(codes)-5)
}

// setupCheck passes when nothing is failing.
func setupCheck(id, label, severity string, failing []string) SetupCheck {
	c := SetupCheck{ID: id, Label: label, Severity: severity, Passed: len(failing) == 0}
	if !c.Passed {
		c.Detail = setupNamed(failing)
	}
	return c
}

// setupExists is a blocking "there is at least one" check.
func setupExists(id, label string, n int, what string) SetupCheck {
	c := SetupCheck{ID: id, Label: label, Severity: SetupSeverityBlocking, Passed: n > 0}
	if n == 0 {
		c.Detail = "none yet: add a " + what
	}
	return c
}

// setupDoctypeNames is what a check calls each master.
var setupDoctypeNames = map[string]string{
	"LegalEntity": "legal entity", "Location": "store or warehouse", "Department": "department",
	"CostCenter": "cost centre", "HSNCode": "HSN code", "UOM": "unit", "ItemType": "item type",
	"Category": "category", "Item": "item", "Vendor": "vendor", "Customer": "customer",
	"PrintTemplate": "print format",
}

func setupDoctypeName(dt string) string {
	if n, ok := setupDoctypeNames[dt]; ok {
		return n
	}
	return dt
}

// setupReadiness runs the checks of one GetSetupPath call. The first query
// error is kept and returned; later queries are skipped.
type setupReadiness struct {
	schema, country string
	err             error
}

// codes runs a query returning one string per row; %[1]s is the schema.
func (r *setupReadiness) codes(query string, args ...interface{}) []string {
	out := []string{}
	if r.err != nil {
		return out
	}
	rows, err := db.DB.Query(fmt.Sprintf(query, r.schema), args...)
	if err != nil {
		r.err = err
		return out
	}
	defer rows.Close()
	for rows.Next() {
		var c string
		if err := rows.Scan(&c); err != nil {
			r.err = err
			return out
		}
		out = append(out, c)
	}
	if err := rows.Err(); err != nil {
		r.err = err
	}
	return out
}

// count runs a COUNT query; %[1]s is the schema.
func (r *setupReadiness) count(query string, args ...interface{}) int {
	if r.err != nil {
		return 0
	}
	var n int
	if err := db.DB.QueryRow(fmt.Sprintf(query, r.schema), args...).Scan(&n); err != nil {
		r.err = err
	}
	return n
}

// pending counts a step's records still waiting for their checker.
func (r *setupReadiness) pending(doctypes []string) int {
	n := 0
	for _, dt := range doctypes {
		n += r.count(`SELECT COUNT(*) FROM %[1]s.documents WHERE doctype = $1 AND deleted_at IS NULL AND status = 'Pending Approval'`, dt)
	}
	return n
}

// setupFieldSep joins a row's fields (chr(31), the unit separator - never
// typed into an address).
const setupFieldSep = "\x1f"

// legalEntityChecks: every active entity has a valid tax registration for
// the home country, a state that agrees with it, and the address an invoice
// (and an e-invoice) prints.
func (r *setupReadiness) legalEntityChecks() []SetupCheck {
	rows := r.codes(`SELECT concat_ws(chr(31), COALESCE(NULLIF(data->>'code', ''), id), COALESCE(data->>'gstin', ''),
			COALESCE(data->>'state', ''), COALESCE(data->>'state_code', ''), COALESCE(data->>'address', ''), COALESCE(data->>'pincode', ''))
		FROM %[1]s.documents WHERE doctype = 'LegalEntity' AND deleted_at IS NULL AND ` + setupLive(""))
	india := r.country == "" || r.country == "IN"
	format := TaxIDFormat(r.country)
	var noGSTIN, badGSTIN, badState, noAddress []string
	for _, row := range rows {
		f := strings.SplitN(row, setupFieldSep, 6)
		for len(f) < 6 {
			f = append(f, "")
		}
		code, gstin, state, stateCode, address, pincode := f[0], strings.ToUpper(strings.TrimSpace(f[1])), f[2], f[3], f[4], f[5]
		switch {
		case gstin == "":
			noGSTIN = append(noGSTIN, code)
		case ValidateFieldFormat(format, format.Label, gstin) != nil:
			badGSTIN = append(badGSTIN, code)
		}
		if india {
			// The entity's state decides CGST+SGST or IGST on every invoice:
			// it must be set, and agree with the GSTIN's first two digits.
			st := StateCodeFromName(stateCode)
			if st == "" {
				st = StateCodeFromName(state)
			}
			if fromGSTIN := StateCodeFromGSTIN(gstin); st == "" || (fromGSTIN != "" && fromGSTIN != st) {
				badState = append(badState, code)
			}
		} else if strings.TrimSpace(state) == "" && strings.TrimSpace(stateCode) == "" {
			badState = append(badState, code)
		}
		if strings.TrimSpace(address) == "" || strings.TrimSpace(pincode) == "" {
			noAddress = append(noAddress, code)
		}
	}
	return []SetupCheck{
		setupExists("entity-active", "At least one active legal entity", len(rows), "legal entity"),
		setupCheck("entity-gstin", "Every legal entity has its "+format.Label, SetupSeverityBlocking, noGSTIN),
		setupCheck("entity-gstin-valid", "Every "+format.Label+" is a valid number", SetupSeverityBlocking, badGSTIN),
		setupCheck("entity-state", "Every legal entity's state is set and matches its "+format.Label, SetupSeverityBlocking, badState),
		setupCheck("entity-address", "Every legal entity has the address and PIN code invoices print", SetupSeverityWarning, noAddress),
	}
}

// locationChecks: every active store or warehouse belongs to an active
// legal entity (whose GSTIN its bills carry - buyerStateCode's join) and has
// a cost centre.
func (r *setupReadiness) locationChecks() []SetupCheck {
	active := r.count(`SELECT COUNT(*) FROM %[1]s.documents WHERE doctype = 'Location' AND deleted_at IS NULL AND ` + setupLive(""))
	orphan := r.codes(`SELECT COALESCE(NULLIF(l.data->>'code', ''), l.id) FROM %[1]s.documents l
		WHERE l.doctype = 'Location' AND l.deleted_at IS NULL AND ` + setupLive("l.") + `
		  AND NOT EXISTS (SELECT 1 FROM %[1]s.documents e WHERE e.doctype = 'LegalEntity' AND e.deleted_at IS NULL
		      AND ` + setupLive("e.") + ` AND COALESCE(l.data->>'legal_entity', '') <> ''
		      AND (e.id = l.data->>'legal_entity' OR e.data->>'code' = l.data->>'legal_entity'))`)
	noCC := r.codes(`SELECT COALESCE(NULLIF(data->>'code', ''), id) FROM %[1]s.documents
		WHERE doctype = 'Location' AND deleted_at IS NULL AND ` + setupLive("") + ` AND COALESCE(data->>'cost_center', '') = ''`)
	return []SetupCheck{
		setupExists("locations-active", "At least one active store or warehouse", active, "store or warehouse"),
		setupCheck("locations-entity", "Every store and warehouse belongs to an active legal entity", SetupSeverityBlocking, orphan),
		setupCheck("locations-cost-centre", "Every store and warehouse has a cost centre", SetupSeverityWarning, noCC),
	}
}

// taxChecks: HSN codes exist and are the 4, 6 or 8 digits an Item accepts,
// and no active legal entity lacks its GSTIN (the 91.3 rule).
func (r *setupReadiness) taxChecks(activeHSN int) []SetupCheck {
	var badHSN []string
	for _, row := range r.codes(`SELECT COALESCE(NULLIF(data->>'code', ''), id) || '|' || COALESCE(NULLIF(data->>'hsn', ''), data->>'hsn_code', '')
		FROM %[1]s.documents WHERE doctype = 'HSNCode' AND deleted_at IS NULL AND ` + setupLive("")) {
		code, hsn, _ := strings.Cut(row, "|")
		if !validHSN(NormalizeHSN(hsn)) {
			badHSN = append(badHSN, code)
		}
	}
	noGSTIN := r.codes(`SELECT COALESCE(NULLIF(data->>'code', ''), id) FROM %[1]s.documents
		WHERE doctype = 'LegalEntity' AND deleted_at IS NULL AND ` + setupLive("") + ` AND COALESCE(data->>'gstin', '') = ''`)
	return []SetupCheck{
		setupExists("tax-hsn", "At least one HSN code", activeHSN, "HSN code"),
		setupCheck("tax-entity-gstin", "No active legal entity is without its GSTIN", SetupSeverityBlocking, noGSTIN),
		setupCheck("tax-hsn-shape", "Every HSN code is 4, 6 or 8 digits", SetupSeverityBlocking, badHSN),
	}
}

// gstSeriesDocTypes are the series whose numbers are GST tax-document
// numbers (document_numbering.go): sales invoice, credit note, debit note
// and the OMS bill.
var gstSeriesDocTypes = []string{"SI", "CN", "DN", "OB"}

// numberingChecks: the tax documents' series exist and are on, and every
// number they print at every store keeps the home country's cap - in India
// at most 16 characters of GST's characters (CGST Rule 46(b)). A series
// saved before that rule keeps printing what it printed (SaveNumberSeries
// lets an unchanged one save), so this is where it shows.
func (r *setupReadiness) numberingChecks(series []NumberSeries) []SetupCheck {
	byType := map[string]NumberSeries{}
	for _, s := range series {
		byType[s.DocType] = s
	}
	maxLen, gstRules := seriesNumberRules(r.country)
	var stores []seriesStore
	if r.err == nil {
		stores, r.err = loadSeriesStores(r.schema)
	}
	var missing, tooLong []string
	for _, dt := range gstSeriesDocTypes {
		s, ok := byType[dt]
		if !ok || !s.ActiveStatus {
			missing = append(missing, dt)
			continue
		}
		for _, n := range seriesSampleNumbers(s, s.StoreOverrides, stores) {
			if len(n.Number) > maxLen || (gstRules && !gstNumberPattern.MatchString(n.Number)) {
				tooLong = append(tooLong, dt+" prints "+n.Number)
				break
			}
		}
	}
	label := fmt.Sprintf("Every tax document number is at most %d characters", maxLen)
	if gstRules {
		label += " of capital letters, digits, / and -"
	}
	return []SetupCheck{
		setupExists("numbering-series", "Document numbering is set up", len(series), "number series"),
		setupCheck("numbering-tax-series", "Sales invoice, credit note, debit note and OMS bill series exist and are on", SetupSeverityBlocking, missing),
		setupCheck("numbering-tax-length", label, SetupSeverityBlocking, tooLong),
	}
}

// itemChecks: an active item without a valid HSN code cannot go on a GST
// invoice. A warning, since the item form already demands the code and only
// imported or older records can lack it.
func (r *setupReadiness) itemChecks(activeItems int) []SetupCheck {
	var bad []string
	for _, row := range r.codes(`SELECT COALESCE(NULLIF(data->>'code', ''), id) || '|' || COALESCE(data->>'hsn_code', '')
		FROM %[1]s.documents WHERE doctype = 'Item' AND deleted_at IS NULL AND ` + setupLive("")) {
		code, hsn, _ := strings.Cut(row, "|")
		if !validHSN(NormalizeHSN(hsn)) {
			bad = append(bad, code)
		}
	}
	return []SetupCheck{
		setupExists("items-active", "At least one active item", activeItems, "item"),
		setupCheck("items-hsn", "Every item has a 4, 6 or 8 digit HSN code", SetupSeverityWarning, bad),
	}
}

// countChecks is the plain "each master has an active record" rule the path
// always had, as checks.
func countChecks(step SetupPathStep, active map[string]int) []SetupCheck {
	out := []SetupCheck{}
	for _, dt := range step.Doctypes {
		name := setupDoctypeName(dt)
		out = append(out, setupExists("has-"+strings.ToLower(dt), "At least one active "+name, active[dt], name))
	}
	return out
}

// setupStepState folds a step's checks into its state and the worst failing
// severity ("" when nothing fails).
func setupStepState(step SetupPathStep, pending int) (state, severity string) {
	blocking, warning := false, false
	for _, c := range step.Checks {
		switch {
		case c.Passed:
		case c.Severity == SetupSeverityBlocking:
			blocking = true
		default:
			warning = true
		}
	}
	if blocking {
		severity = SetupSeverityBlocking
	} else if warning {
		severity = SetupSeverityWarning
	}
	switch {
	case step.Optional && step.Count == 0:
		return SetupStateWaived, ""
	case pending > 0:
		return SetupStateAwaitingApproval, severity
	case step.Count == 0:
		return SetupStateDraft, severity
	case blocking:
		return SetupStateInvalid, severity
	}
	return SetupStateReady, severity
}
