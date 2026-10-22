package engines

import (
	"custom_erp/db"
	"errors"
	"fmt"
	"strings"
	"time"
)

// Stage 101.1.1 - one customer page: the profile (birthday, anniversary,
// gender, sizes, preferred store and salesman, tags), what they have bought
// (visits, spend net of returns, average bill, last store, points, lifecycle,
// RFM), consent per channel and purpose, notes, and where each imported field
// came from. 101.1.4 masks phone and email for roles without contact access,
// here and wherever a Customer is read (MaskCustomerContacts).

// CRMCustomerSummary is what the customer has done with the business.
type CRMCustomerSummary struct {
	Visits          int      `json:"visits"`
	Spend           float64  `json:"spend"`
	Returns         int      `json:"returns"`
	AvgBill         *float64 `json:"avg_bill"`
	FirstBill       string   `json:"first_bill,omitempty"`
	LastBill        string   `json:"last_bill,omitempty"`
	DaysSince       *int     `json:"days_since,omitempty"`
	LastStore       string   `json:"last_store,omitempty"`
	Points          int      `json:"points"`
	PointsPending   int      `json:"points_pending"`
	Lifecycle       string   `json:"lifecycle"`
	RFM             string   `json:"rfm,omitempty"`
	OpenTickets     int      `json:"open_tickets"`
	LiveJourneys    int      `json:"live_journeys"`
	QueuedMessages  int      `json:"queued_messages"`
	OpenTasks       int      `json:"open_tasks"`
	NextAppointment string   `json:"next_appointment,omitempty"`
	ReferralCode    string   `json:"referral_code,omitempty"`
}

// CRMCustomerPage is the whole page.
type CRMCustomerPage struct {
	ID             string                 `json:"id"`
	Status         string                 `json:"status"`
	Customer       map[string]interface{} `json:"customer"`
	Masked         bool                   `json:"masked"`
	Summary        CRMCustomerSummary     `json:"summary"`
	Consent        []CRMConsentState      `json:"consent"`
	ProfileSources map[string]interface{} `json:"profile_sources"`
	Notes          []CRMCustomerNote      `json:"notes"`
	Wishlist       []CRMWishItem          `json:"wishlist"`
	MergedInto     string                 `json:"merged_into,omitempty"`
}

// CRMCustomerNote is one note on the customer page.
type CRMCustomerNote struct {
	ID        int64     `json:"id"`
	Note      string    `json:"note"`
	CreatedBy string    `json:"created_by"`
	CreatedAt time.Time `json:"created_at"`
}

// GetCRMCustomerPage assembles the customer page for role.
func GetCRMCustomerPage(tenantID, role, customerID string) (*CRMCustomerPage, error) {
	schema, err := crmSchema(tenantID)
	if err != nil {
		return nil, err
	}
	c, err := crmLoadCustomer(schema, customerID)
	if errors.Is(err, ErrCRMNotFound) {
		return nil, crmNotFound("customer", customerID)
	}
	if err != nil {
		return nil, err
	}
	page := &CRMCustomerPage{ID: c.ID, Status: c.Status, Customer: c.Data, Notes: []CRMCustomerNote{}, Wishlist: []CRMWishItem{},
		ProfileSources: map[string]interface{}{}, MergedInto: strField(c.Data, "merged_into")}
	if m, ok := c.Data["profile_sources"].(map[string]interface{}); ok {
		page.ProfileSources = m
	}
	delete(page.Customer, "profile_sources")
	// The page shows the same fields the record screen would: payroll-style
	// restrictions (date of birth for a Cashier) apply here too.
	if filtered, err := FilterFieldsForRole(tenantID, role, "Customer", page.Customer); err == nil {
		page.Customer = filtered
	}
	page.Masked = !RoleSeesContacts(tenantID, role)

	if f, err := crmCustomerFacts(schema, c.ID); err == nil {
		s := &page.Summary
		s.Visits, s.Spend, s.Returns, s.LastStore, s.Points, s.PointsPending, s.Lifecycle, s.RFM =
			f.Visits, round2(f.Spend), f.Returns, f.LastStore, f.Points, f.PointsPending, f.Lifecycle, f.RFM
		if f.AvgBill.Valid {
			v := round2(f.AvgBill.Float64)
			s.AvgBill = &v
		}
		if f.FirstAt.Valid {
			s.FirstBill = f.FirstAt.Time.Format("2006-01-02")
		}
		if f.LastAt.Valid {
			s.LastBill = f.LastAt.Time.Format("2006-01-02")
		}
		if f.DaysSince.Valid {
			d := int(f.DaysSince.Int64)
			s.DaysSince = &d
		}
	} else if !errors.Is(err, ErrCRMNotFound) {
		return nil, err
	} else {
		page.Summary.Lifecycle = "Never bought"
	}
	counts := []struct {
		dst   *int
		query string
	}{
		{&page.Summary.OpenTickets, `SELECT COUNT(*) FROM %s.documents WHERE doctype = 'ServiceTicket' AND data->>'customer' = $1 AND status NOT IN ('Resolved', 'Closed', 'Cancelled') AND deleted_at IS NULL`},
		{&page.Summary.LiveJourneys, `SELECT COUNT(*) FROM %s.crm_journey_runs WHERE customer_id = $1 AND state IN ('Active', 'Waiting')`},
		{&page.Summary.QueuedMessages, `SELECT COUNT(*) FROM %s.crm_sends WHERE customer_id = $1 AND status IN ('Queued', 'Deferred')`},
		{&page.Summary.OpenTasks, `SELECT COUNT(*) FROM %s.crm_tasks WHERE customer_id = $1 AND status = 'Open'`},
	}
	for _, q := range counts {
		_ = db.DB.QueryRow(fmt.Sprintf(q.query, schema), c.ID).Scan(q.dst)
	}
	var next time.Time
	if err := db.DB.QueryRow(fmt.Sprintf(`SELECT slot_start FROM %s.crm_appointments WHERE customer_id = $1 AND status = 'Booked' AND slot_start >= LOCALTIMESTAMP ORDER BY slot_start LIMIT 1`, schema), c.ID).Scan(&next); err == nil {
		page.Summary.NextAppointment = next.Format("2006-01-02 15:04")
	}
	_ = db.DB.QueryRow(fmt.Sprintf(`SELECT code FROM %s.crm_referral_codes WHERE customer_id = $1`, schema), c.ID).Scan(&page.Summary.ReferralCode)

	if page.Consent, err = crmConsentStates(schema, c); err != nil {
		return nil, err
	}
	if page.Notes, err = listCRMCustomerNotes(schema, c.ID, 20); err != nil {
		return nil, err
	}
	if page.Wishlist, err = crmWishlistFor(schema, c.ID); err != nil {
		return nil, err
	}
	MaskCustomerContacts(tenantID, role, page.Customer)
	return page, nil
}

// CRMCustomerRow is one row of the customer list.
type CRMCustomerRow struct {
	ID        string   `json:"id"`
	Code      string   `json:"code"`
	Name      string   `json:"name"`
	Phone     string   `json:"phone"`
	Email     string   `json:"email"`
	City      string   `json:"city,omitempty"`
	Tier      string   `json:"tier,omitempty"`
	Visits    int      `json:"visits"`
	Spend     float64  `json:"spend"`
	LastBill  string   `json:"last_bill,omitempty"`
	Lifecycle string   `json:"lifecycle"`
	Tags      []string `json:"tags,omitempty"`
}

// CRMCustomerList is one page of customers.
type CRMCustomerList struct {
	Rows     []CRMCustomerRow `json:"rows"`
	Total    int              `json:"total"`
	Page     int              `json:"page"`
	PageSize int              `json:"page_size"`
	Masked   bool             `json:"masked"`
}

// SearchCRMCustomers lists customers, newest first, or those matching q by
// name, code, city, tag, email or phone. A masked role may still find a
// customer by typing the whole phone number - the results show it masked.
func SearchCRMCustomers(tenantID, role, q, lifecycle string, page, pageSize int) (*CRMCustomerList, error) {
	schema, err := crmSchema(tenantID)
	if err != nil {
		return nil, err
	}
	if page < 1 {
		page = 1
	}
	if pageSize < 1 || pageSize > 200 {
		pageSize = 50
	}
	th := crmLifecycleThresholds(schema)
	where := []string{"TRUE"}
	args := []interface{}{}
	q = strings.TrimSpace(q)
	if q != "" {
		args = append(args, "%"+strings.ToLower(q)+"%")
		n := len(args)
		digits := strings.Map(func(r rune) rune {
			if r >= '0' && r <= '9' {
				return r
			}
			return -1
		}, q)
		cond := fmt.Sprintf(`(LOWER(name) LIKE $%[1]d OR LOWER(COALESCE(data->>'code', '')) LIKE $%[1]d OR LOWER(city) LIKE $%[1]d OR LOWER(tags) LIKE $%[1]d`, n)
		if RoleSeesContacts(tenantID, role) {
			cond += fmt.Sprintf(` OR LOWER(email) LIKE $%d`, n)
			if len(digits) >= 4 {
				args = append(args, "%"+digits+"%")
				cond += fmt.Sprintf(` OR regexp_replace(phone, '[^0-9]', '', 'g') LIKE $%d`, len(args))
			}
		} else if len(digits) >= 8 {
			// The whole number only: a masked role cannot fish for a phone
			// by its digits.
			args = append(args, digits)
			cond += fmt.Sprintf(` OR regexp_replace(phone, '[^0-9]', '', 'g') = $%d OR right(regexp_replace(phone, '[^0-9]', '', 'g'), 10) = right($%d, 10)`, len(args), len(args))
		} else if strings.Contains(q, "@") {
			args = append(args, strings.ToLower(q))
			cond += fmt.Sprintf(` OR LOWER(email) = $%d`, len(args))
		}
		where = append(where, cond+")")
	}
	if lifecycle = strings.TrimSpace(lifecycle); lifecycle != "" {
		args = append(args, lifecycle)
		where = append(where, fmt.Sprintf("lifecycle = $%d", len(args)))
	}
	base := crmFactsCTE(schema, th, false)
	var total int
	if err := db.DB.QueryRow(base+` SELECT COUNT(*) FROM cf WHERE `+strings.Join(where, " AND "), args...).Scan(&total); err != nil {
		return nil, err
	}
	args = append(args, pageSize, (page-1)*pageSize)
	rows, err := db.DB.Query(base+fmt.Sprintf(` SELECT id, COALESCE(data->>'code', ''), name, phone, email, city, tier, visits, spend, last_at, lifecycle, tags
		FROM cf WHERE %s ORDER BY last_at DESC NULLS LAST, created_at DESC LIMIT $%d OFFSET $%d`, strings.Join(where, " AND "), len(args)-1, len(args)), args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := &CRMCustomerList{Rows: []CRMCustomerRow{}, Total: total, Page: page, PageSize: pageSize, Masked: !RoleSeesContacts(tenantID, role)}
	for rows.Next() {
		var r CRMCustomerRow
		var tags string
		var last *time.Time
		if err := rows.Scan(&r.ID, &r.Code, &r.Name, &r.Phone, &r.Email, &r.City, &r.Tier, &r.Visits, &r.Spend, &last, &r.Lifecycle, &tags); err != nil {
			return nil, err
		}
		r.Spend = round2(r.Spend)
		if last != nil {
			r.LastBill = last.Format("2006-01-02")
		}
		r.Tags = crmSplitTags(tags)
		if out.Masked {
			r.Phone, r.Email = crmMaskPhone(r.Phone), crmMaskEmail(r.Email)
		}
		out.Rows = append(out.Rows, r)
	}
	return out, rows.Err()
}

func crmSplitTags(s string) []string {
	var out []string
	for _, t := range strings.Split(s, ",") {
		if t = strings.TrimSpace(t); t != "" {
			out = append(out, t)
		}
	}
	return out
}

// AddCRMCustomerNote adds a note to the customer page.
func AddCRMCustomerNote(tenantID, customerID, note, actor string) (*CRMCustomerNote, error) {
	note = strings.TrimSpace(note)
	if note == "" {
		return nil, crmErr(CRMCodeCustomer, "write the note first")
	}
	if len([]rune(note)) > 2000 {
		return nil, crmErr(CRMCodeCustomer, "a note can be at most 2,000 characters")
	}
	schema, err := crmSchema(tenantID)
	if err != nil {
		return nil, err
	}
	c, err := crmLoadCustomer(schema, customerID)
	if errors.Is(err, ErrCRMNotFound) {
		return nil, crmNotFound("customer", customerID)
	}
	if err != nil {
		return nil, err
	}
	if crmCustomerErased(c) {
		return nil, crmErr(CRMCodeErased, "this customer was erased")
	}
	n := &CRMCustomerNote{Note: note, CreatedBy: actor}
	err = db.DB.QueryRow(fmt.Sprintf(`INSERT INTO %s.crm_customer_notes (customer_id, note, created_by) VALUES ($1, $2, $3) RETURNING id, created_at`, schema),
		c.ID, note, actor).Scan(&n.ID, &n.CreatedAt)
	return n, err
}

func listCRMCustomerNotes(schema, customerID string, limit int) ([]CRMCustomerNote, error) {
	rows, err := db.DB.Query(fmt.Sprintf(`SELECT id, note, COALESCE(created_by, ''), created_at FROM %s.crm_customer_notes WHERE customer_id = $1 ORDER BY id DESC LIMIT $2`, schema), customerID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []CRMCustomerNote{}
	for rows.Next() {
		var n CRMCustomerNote
		if err := rows.Scan(&n.ID, &n.Note, &n.CreatedBy, &n.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, n)
	}
	return out, rows.Err()
}

// maskContactColumns masks customer phone and email columns in a report for a
// role without contact access (101.1.4), the same rule the customer screens
// and the record API apply, so an export cannot show what a screen hides.
func maskContactColumns(tenantID string, columns []ReportColumn, rows []map[string]interface{}, role string) []map[string]interface{} {
	var phones, emails []string
	for _, c := range columns {
		switch c.Key {
		case "phone", "customer_phone", "mobile":
			phones = append(phones, c.Key)
		case "email", "customer_email":
			emails = append(emails, c.Key)
		}
	}
	if len(phones)+len(emails) == 0 || RoleSeesContacts(tenantID, role) {
		return rows
	}
	for _, row := range rows {
		for _, k := range phones {
			if v, ok := row[k].(string); ok && v != "" {
				row[k] = crmMaskPhone(v)
			}
		}
		for _, k := range emails {
			if v, ok := row[k].(string); ok && v != "" {
				row[k] = crmMaskEmail(v)
			}
		}
	}
	return rows
}

// crmRestoreMaskedContacts keeps the stored phone and email when a role that
// sees them masked saves the record unchanged - otherwise saving the form
// would write "98******10" over the real number.
func crmRestoreMaskedContacts(tenantID, role string, prior, payload map[string]interface{}) {
	if prior == nil || RoleSeesContacts(tenantID, role) {
		return
	}
	if p := strField(prior, "phone"); p != "" && strField(payload, "phone") == crmMaskPhone(p) {
		payload["phone"] = p
	}
	if e := strField(prior, "email"); e != "" && strField(payload, "email") == crmMaskEmail(e) {
		payload["email"] = e
	}
}
