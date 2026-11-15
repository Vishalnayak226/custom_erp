package engines

import (
	"database/sql"
	"fmt"
	"math"
	"os"
	"strings"
	"time"

	"custom_erp/db"
)

// 50.12 / BLD-026 (ask 193): the platform's own tax invoice for a captured
// subscription payment.
//
// Official sources this follows (CBIC pages only, no mirror), re-read 2026-11-15:
//   - Rule 46, Central Goods and Services Tax Rules, 2017 (tax invoice):
//     https://taxinformation.cbic.gov.in/content/html/tax_repository/gst/rules/cgst_rules/active/chapter6/rule46_v1.00.html
//     (b) a consecutive serial number of at most sixteen characters, unique
//     for a financial year, hyphen and slash allowed; (g) the HSN code of the
//     goods or services (the rule says HSN; SAC is the services
//     classification, so the setting is called SAC); (h) description of the
//     goods or services; supplier and recipient particulars, taxable value,
//     tax rate and amount, place of supply with the State name. (q) the
//     signature of the supplier is not required on an electronic invoice
//     issued under the Information Technology Act, 2000, and this invoice is
//     electronic, so it carries no signature.
//   - Section 12(2), Integrated Goods and Services Tax Act, 2017 (place of
//     supply of services): (a) to a registered person, the location of that
//     person; (b) to anyone else, the location of the recipient where the
//     address on record exists, and the location of the supplier in other
//     cases. https://taxinformation.cbic.gov.in/content/html/tax_repository/gst/acts/2017_IGST_Act/active/chapterv/section12_v1.00.html
//   - Section 8 IGST Act: a supply is inter-State when the supplier's
//     location and the place of supply are in different States.
//
// The SAC and the 18% rate are not decided here: the SAC is a setting the
// owner fills in and the CA confirms both (docs/operations/ca-confirmation-pack.md).

// SettingSaaSInvoiceSAC is the SAC printed on the subscription invoice. It has
// no default on purpose: no code is assumed to be right.
const SettingSaaSInvoiceSAC = "saas.invoice_sac"

func init() {
	RegisterSetting(SettingDefinition{
		Key: SettingSaaSInvoiceSAC, Module: "Platform billing",
		Label: "Subscription invoice SAC", Type: SettingTypeString, Default: "",
		Description: "The SAC printed on the platform's subscription invoice, set on the billing client. Blank holds invoices unnumbered until it is set.",
	})
}

// saasParty is one side of the invoice.
type saasParty struct {
	Name, Address, GSTIN, State string
}

// saasInvoiceCalc is a computed invoice before it is written.
type saasInvoiceCalc struct {
	Supplier, Recipient       saasParty
	SAC                       string
	PosCode, PosName          string
	Gross, Taxable            int64
	CGST, SGST, IGST, GSTPaid int64
	Pending                   bool
	Note                      string
}

// saasSupplier reads the platform's own details from the environment.
func saasSupplier() saasParty {
	g := strings.ToUpper(strings.TrimSpace(os.Getenv("SAAS_SUPPLIER_GSTIN")))
	return saasParty{
		Name:    strings.TrimSpace(os.Getenv("SAAS_SUPPLIER_NAME")),
		Address: strings.TrimSpace(os.Getenv("SAAS_SUPPLIER_ADDRESS")),
		GSTIN:   g,
		State:   StateCodeFromGSTIN(g),
	}
}

// saasRecipient reads the client's party details from its first active Legal
// Entity, falling back to the client's name.
func saasRecipient(tenantID string) saasParty {
	var p saasParty
	_ = db.DB.QueryRow(`SELECT name FROM public.tenants WHERE tenant_id = $1`, tenantID).Scan(&p.Name)
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return p
	}
	var name, gstin, state, addr, city, pin sql.NullString
	err = db.DB.QueryRow(fmt.Sprintf(`SELECT data->>'name', data->>'gstin', data->>'state', data->>'address', data->>'city', data->>'pincode'
		  FROM %s.documents WHERE doctype = 'LegalEntity' AND deleted_at IS NULL AND status = 'Active'
		 ORDER BY created_at, id LIMIT 1`, schema)).Scan(&name, &gstin, &state, &addr, &city, &pin)
	if err != nil {
		return p
	}
	if strings.TrimSpace(name.String) != "" {
		p.Name = strings.TrimSpace(name.String)
	}
	p.GSTIN = strings.ToUpper(strings.TrimSpace(gstin.String))
	parts := []string{}
	for _, s := range []string{addr.String, city.String, state.String, pin.String} {
		if s = strings.TrimSpace(s); s != "" {
			parts = append(parts, s)
		}
	}
	p.Address = strings.Join(parts, ", ")
	if c := StateCodeFromGSTIN(p.GSTIN); c != "" {
		p.State = c
	} else {
		p.State = StateCodeFromName(state.String)
	}
	return p
}

// saasPlaceOfSupply applies IGST Act s.12(2): a registered recipient's
// location, else the address on record, else the supplier's location.
func saasPlaceOfSupply(supplier, recipient saasParty) string {
	if c := StateCodeFromGSTIN(recipient.GSTIN); c != "" {
		return c
	}
	if recipient.State != "" {
		return recipient.State
	}
	return supplier.State
}

func paiseOf(rupees float64) int64 { return int64(math.Round(rupees * 100)) }

// computeSaaSInvoice works out one invoice. The gross is what Razorpay
// captured and is GST-inclusive; the tax is split by the Sales Invoice's own
// helper (CalculateGST), so there is one GST engine. Taxable value is the
// gross less the tax, so the total is exactly the money received.
func computeSaaSInvoice(tenantID string, gross int64) saasInvoiceCalc {
	c := saasInvoiceCalc{Supplier: saasSupplier(), Recipient: saasRecipient(tenantID), Gross: gross}
	c.SAC = strings.TrimSpace(GetSettingString(saasBillingTenant(), SettingSaaSInvoiceSAC))
	first := (gross*100 + (100+saasGSTRate)/2) / (100 + saasGSTRate)
	reasons := []string{}
	switch {
	case c.Supplier.GSTIN == "":
		reasons = append(reasons, "supplier GSTIN not set")
	case c.Supplier.State == "":
		reasons = append(reasons, "supplier GSTIN is not valid")
	}
	if c.SAC == "" {
		reasons = append(reasons, "SAC not set")
	}
	if len(reasons) > 0 {
		c.Pending, c.Note = true, strings.Join(reasons, "; ")
		c.Taxable, c.GSTPaid = first, gross-first
		return c
	}
	c.PosCode = saasPlaceOfSupply(c.Supplier, c.Recipient)
	c.PosName = indianStateNames[c.PosCode]
	bd, err := CalculateGST(float64(first)/100, saasGSTRate, c.PosCode != c.Supplier.State)
	if err != nil {
		c.Pending, c.Note = true, err.Error()
		c.Taxable, c.GSTPaid = first, gross-first
		return c
	}
	c.GSTPaid = paiseOf(bd.TotalTax)
	c.Taxable = gross - c.GSTPaid
	c.CGST, c.IGST = paiseOf(bd.CGST), paiseOf(bd.IGST)
	c.SGST = c.GSTPaid - c.CGST - c.IGST
	return c
}

// nextSaaSInvoiceNo takes the next serial of the financial year. It is at most
// 16 characters (Rule 46), such as SA/26-27/000123.
func nextSaaSInvoiceNo(tx *sql.Tx, now time.Time) (no, fy string, err error) {
	fy = indianFiscalYear(now)
	var n int64
	if err = tx.QueryRow(`INSERT INTO public.saas_invoice_counters (fiscal_year, last_no) VALUES ($1, 1)
		ON CONFLICT (fiscal_year) DO UPDATE SET last_no = public.saas_invoice_counters.last_no + 1 RETURNING last_no`, fy).Scan(&n); err != nil {
		return "", "", err
	}
	return fmt.Sprintf("SA/%s/%06d", fy[2:], n), fy, nil
}

const saasInvoiceInsert = `INSERT INTO public.saas_invoices
	(invoice_no, tenant_id, razorpay_payment_id, period_start, period_end, gross_paise, taxable_paise, gst_paise, gst_rate,
	 status, note, fiscal_year, invoice_date, supplier_name, supplier_address, supplier_gstin, recipient_name, recipient_address, recipient_gstin,
	 pos_state_code, pos_state_name, sac, cgst_paise, sgst_paise, igst_paise, description)
	VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18,$19,$20,$21,$22,$23,$24,$25,$26)`

// issueSaaSInvoiceTx writes the platform's tax invoice for one captured
// payment. The payment id is unique, so a replay writes nothing. When the
// supplier GSTIN or the SAC is not set the payment is recorded as a pending
// invoice with no tax invoice number; IssuePendingSaaSInvoices numbers it once
// they are set.
func issueSaaSInvoiceTx(tx *sql.Tx, tenantID, paymentID string, grossPaise int64, ps, pe *time.Time) (string, bool, error) {
	var existing string
	err := tx.QueryRow(`SELECT invoice_no FROM public.saas_invoices WHERE razorpay_payment_id = $1`, paymentID).Scan(&existing)
	if err == nil {
		return existing, false, nil
	}
	if err != sql.ErrNoRows {
		return "", false, err
	}
	c := computeSaaSInvoice(tenantID, grossPaise)
	now := saasNow()
	no, fy, status := "PENDING-"+paymentID, "", "pending"
	var date interface{}
	if !c.Pending {
		if no, fy, err = nextSaaSInvoiceNo(tx, now); err != nil {
			return "", false, err
		}
		status, date = "issued", now.In(istZone).Format("2006-01-02")
	}
	if _, err := tx.Exec(saasInvoiceInsert, no, tenantID, paymentID, ps, pe, grossPaise, c.Taxable, c.GSTPaid, saasGSTRate,
		status, c.Note, fy, date, c.Supplier.Name, c.Supplier.Address, c.Supplier.GSTIN, c.Recipient.Name, c.Recipient.Address, c.Recipient.GSTIN,
		c.PosCode, c.PosName, c.SAC, c.CGST, c.SGST, c.IGST, saasInvoiceDescription(tx, tenantID, ps, pe)); err != nil {
		return "", false, err
	}
	return no, true, nil
}

// IssuePendingSaaSInvoices numbers the pending invoices that the settings now
// allow, oldest first, and returns how many it numbered.
func IssuePendingSaaSInvoices() (int, error) {
	rows, err := db.DB.Query(`SELECT razorpay_payment_id FROM public.saas_invoices WHERE status = 'pending' ORDER BY created_at, invoice_no`)
	if err != nil {
		return 0, err
	}
	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return 0, err
		}
		ids = append(ids, id)
	}
	rows.Close()
	done := 0
	for _, id := range ids {
		ok, err := issueOnePendingSaaSInvoice(id)
		if err != nil {
			return done, err
		}
		if ok {
			done++
		}
	}
	return done, nil
}

func issueOnePendingSaaSInvoice(paymentID string) (bool, error) {
	tx, err := db.DB.Begin()
	if err != nil {
		return false, err
	}
	defer tx.Rollback()
	var old, tenantID string
	var gross int64
	err = tx.QueryRow(`SELECT invoice_no, tenant_id, gross_paise FROM public.saas_invoices WHERE razorpay_payment_id = $1 AND status = 'pending' FOR UPDATE`, paymentID).Scan(&old, &tenantID, &gross)
	if err == sql.ErrNoRows {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	c := computeSaaSInvoice(tenantID, gross)
	if c.Pending {
		if _, err := tx.Exec(`UPDATE public.saas_invoices SET note = $1 WHERE invoice_no = $2`, c.Note, old); err != nil {
			return false, err
		}
		return false, tx.Commit()
	}
	now := saasNow()
	no, fy, err := nextSaaSInvoiceNo(tx, now)
	if err != nil {
		return false, err
	}
	if _, err := tx.Exec(`UPDATE public.saas_invoices SET invoice_no = $1, status = 'issued', note = '', fiscal_year = $2, invoice_date = $3,
		taxable_paise = $4, gst_paise = $5, supplier_name = $6, supplier_address = $7, supplier_gstin = $8,
		recipient_name = $9, recipient_address = $10, recipient_gstin = $11, pos_state_code = $12, pos_state_name = $13, sac = $14,
		cgst_paise = $15, sgst_paise = $16, igst_paise = $17 WHERE invoice_no = $18`,
		no, fy, now.In(istZone).Format("2006-01-02"), c.Taxable, c.GSTPaid, c.Supplier.Name, c.Supplier.Address, c.Supplier.GSTIN,
		c.Recipient.Name, c.Recipient.Address, c.Recipient.GSTIN, c.PosCode, c.PosName, c.SAC, c.CGST, c.SGST, c.IGST, old); err != nil {
		return false, err
	}
	return true, tx.Commit()
}

// SaaSInvoice is one invoice as the client sees it.
type SaaSInvoice struct {
	InvoiceNo   string  `json:"invoice_no"`
	Status      string  `json:"status"`
	Note        string  `json:"note,omitempty"`
	Date        string  `json:"date,omitempty"`
	PeriodStart *string `json:"period_start,omitempty"`
	PeriodEnd   *string `json:"period_end,omitempty"`
	PlaceState  string  `json:"place_of_supply,omitempty"`
	SAC         string  `json:"sac,omitempty"`
	Description string  `json:"description,omitempty"`
	Taxable     int64   `json:"taxable_paise"`
	CGST        int64   `json:"cgst_paise"`
	SGST        int64   `json:"sgst_paise"`
	IGST        int64   `json:"igst_paise"`
	Gross       int64   `json:"gross_paise"`
	GSTRate     float64 `json:"gst_rate"`
}

// ListSaaSInvoices returns a client's invoices, newest first.
func ListSaaSInvoices(tenantID string, limit int) ([]SaaSInvoice, error) {
	if limit <= 0 || limit > 200 {
		limit = 100
	}
	rows, err := db.DB.Query(`SELECT invoice_no, status, note, COALESCE(to_char(invoice_date,'YYYY-MM-DD'),''),
		to_char(period_start AT TIME ZONE 'Asia/Kolkata','YYYY-MM-DD'), to_char(period_end AT TIME ZONE 'Asia/Kolkata','YYYY-MM-DD'),
		pos_state_code, pos_state_name, sac, description, taxable_paise, cgst_paise, sgst_paise, igst_paise, gross_paise, gst_rate
		FROM public.saas_invoices WHERE tenant_id = $1 ORDER BY created_at DESC, invoice_no DESC LIMIT $2`, tenantID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []SaaSInvoice{}
	for rows.Next() {
		var v SaaSInvoice
		var code, name string
		if err := rows.Scan(&v.InvoiceNo, &v.Status, &v.Note, &v.Date, &v.PeriodStart, &v.PeriodEnd, &code, &name, &v.SAC, &v.Description,
			&v.Taxable, &v.CGST, &v.SGST, &v.IGST, &v.Gross, &v.GSTRate); err != nil {
			return nil, err
		}
		if code != "" {
			v.PlaceState = fmt.Sprintf("%s (%s)", name, code)
		}
		out = append(out, v)
	}
	return out, rows.Err()
}

// saasInvoiceDescription is the Rule 46(h) description of the service, such as
// "Software subscription - Growth, 2026-11-01 to 2026-12-01".
func saasInvoiceDescription(tx *sql.Tx, tenantID string, ps, pe *time.Time) string {
	var plan string
	_ = tx.QueryRow(`SELECT COALESCE(NULLIF(p.display_name,''), p.plan_key) FROM public.saas_subscriptions s
		JOIN public.saas_plans p ON p.plan_key = s.plan_key WHERE s.tenant_id = $1`, tenantID).Scan(&plan)
	d := "Software subscription"
	if plan != "" {
		d += " - " + plan
	}
	if ps != nil && pe != nil {
		d += ", " + ps.In(istZone).Format("2006-01-02") + " to " + pe.In(istZone).Format("2006-01-02")
	}
	return d
}
