package engines

import (
	"custom_erp/db"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

// Stage 148.3 (D4): credit control on the order.
//
// A customer with a credit limit has an exposure: what they still owe on
// posted invoices, plus what they have ordered and not yet been invoiced for,
// plus the order in hand. An order accepted over the limit goes On Hold with
// the Credit reason and a CreditHoldRelease in the approval inbox; approving
// it releases the order, rejecting it leaves the hold. At invoice post only a
// customer flagged "stop" is refused; the others get a warning on the
// invoice. A customer with no limit, and a one-time customer, are never held.

const (
	// HoldCredit is the hold code of an order held over its customer's credit limit.
	HoldCredit = "CREDIT"

	creditReleaseDoctype = "CreditHoldRelease"
	creditApproverRole   = "Store Manager"
)

// creditCustomer is the part of a Customer that credit control reads.
type creditCustomer struct {
	ID          string
	Name        string
	Limit       float64
	Stop        bool
	OneTime     bool
	PaymentTerm string
}

func creditCustomerFrom(id string, data map[string]interface{}) *creditCustomer {
	limit, _ := parityNumber(data["credit_limit"])
	return &creditCustomer{ID: id, Name: strField(data, "name"), Limit: limit, Stop: strings.EqualFold(strField(data, "credit_stop"), "Yes"),
		OneTime: strings.EqualFold(strField(data, "one_time"), "Yes"), PaymentTerm: strField(data, "payment_term")}
}

// loadCreditCustomerByID reads one customer; nil when there is none.
func loadCreditCustomerByID(schema, id string) *creditCustomer {
	if id == "" {
		return nil
	}
	var raw []byte
	if err := db.DB.QueryRow(fmt.Sprintf(`SELECT data FROM %s.documents WHERE doctype = 'Customer' AND id = $1 AND deleted_at IS NULL`, schema), id).Scan(&raw); err != nil {
		return nil
	}
	var data map[string]interface{}
	if json.Unmarshal(raw, &data) != nil {
		return nil
	}
	return creditCustomerFrom(id, data)
}

// loadCreditCustomerByName is the lookup an invoice without a customer link
// has always used (CheckCustomerCreditLimit): the typed value is a name or an id.
func loadCreditCustomerByName(schema, name string) *creditCustomer {
	if name == "" {
		return nil
	}
	var id string
	var raw []byte
	if err := db.DB.QueryRow(fmt.Sprintf(
		`SELECT id, data FROM %s.documents WHERE doctype = 'Customer' AND (data->>'name' = $1 OR id = $1) AND deleted_at IS NULL ORDER BY (id = $1) DESC, created_at LIMIT 1`, schema),
		name).Scan(&id, &raw); err != nil {
		return nil
	}
	var data map[string]interface{}
	if json.Unmarshal(raw, &data) != nil {
		return nil
	}
	return creditCustomerFrom(id, data)
}

// ResolveCustomerLink is the Customer id a typed name stands for: the one
// customer with exactly that name. A name that matches none, or several,
// returns "" - a link is never guessed.
func ResolveCustomerLink(tenantID, name string) string {
	name = strings.TrimSpace(name)
	if name == "" {
		return ""
	}
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return ""
	}
	rows, err := db.DB.Query(fmt.Sprintf(`SELECT id FROM %s.documents WHERE doctype = 'Customer' AND deleted_at IS NULL AND data->>'name' = $1 LIMIT 2`, schema), name)
	if err != nil {
		return ""
	}
	defer rows.Close()
	var ids []string
	for rows.Next() {
		var id string
		if rows.Scan(&id) == nil {
			ids = append(ids, id)
		}
	}
	if len(ids) != 1 {
		return ""
	}
	return ids[0]
}

// CreditExposure is what a customer owes or has on order, against the limit.
type CreditExposure struct {
	Receivable float64 `json:"receivable"`
	OpenOrders float64 `json:"open_orders"`
	ThisOrder  float64 `json:"this_order"`
	Total      float64 `json:"total"`
	Limit      float64 `json:"limit"`
	OneTime    bool    `json:"one_time"`
}

// receivableFor is the customer's posted, unpaid invoices: those linked to
// them, and those that carry only their typed name.
func receivableFor(schema string, c *creditCustomer) (float64, error) {
	var total float64
	err := db.DB.QueryRow(fmt.Sprintf(
		`SELECT COALESCE(SUM(%s), 0) FROM %s.documents
		  WHERE doctype = 'SalesInvoice' AND status = 'Approved' AND deleted_at IS NULL
		    AND (data->>'customer_id' = $1 OR (COALESCE(data->>'customer_id', '') = '' AND data->>'customer' = $2))`,
		salesInvoiceOutstandingSQL, schema), c.ID, c.Name).Scan(&total)
	return total, err
}

// openOrdersFor is the value of the customer's other live orders that no
// posted invoice covers yet and that are not already paid for.
func openOrdersFor(schema string, c *creditCustomer, exceptOrderID string) (float64, error) {
	var total float64
	err := db.DB.QueryRow(fmt.Sprintf(
		`SELECT COALESCE(SUM(COALESCE((o.data->>'total_amount')::numeric, 0)), 0) FROM %[1]s.documents o
		  WHERE o.doctype = 'SalesOrder' AND o.deleted_at IS NULL AND o.id <> $3
		    AND (o.data->>'customer_id' = $1 OR (COALESCE(o.data->>'customer_id', '') = '' AND o.data->>'customer_name' = $2))
		    AND COALESCE(o.data->>'order_status', '') NOT IN ('Cancelled', 'Closed', 'Draft')
		    AND COALESCE(o.data->>'acceptance_status', '') <> '%[2]s'
		    AND COALESCE(o.data->>'payment_status', '') <> 'Confirmed'
		    AND NOT (COALESCE(o.data->>'order_status', '') = 'On Hold' AND COALESCE(o.data->>'hold_reason', '') = '%[3]s')
		    AND NOT EXISTS (SELECT 1 FROM %[1]s.documents i WHERE i.doctype = 'SalesInvoice' AND i.deleted_at IS NULL
		                       AND i.status IN ('Approved', 'Paid') AND i.data->>'sales_order_id' = o.id)`,
		schema, AcceptanceRejected, HoldCredit), c.ID, c.Name, exceptOrderID).Scan(&total)
	return total, err
}

// CustomerCreditExposure is the customer's exposure with thisOrder (rupees,
// already zero for a prepaid order) added. A one-time customer is outside
// credit control altogether: all zeros.
func CustomerCreditExposure(tenantID, customerID, exceptOrderID string, thisOrder float64) (CreditExposure, error) {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return CreditExposure{}, err
	}
	c := loadCreditCustomerByID(schema, customerID)
	if c == nil {
		return CreditExposure{}, nil
	}
	return creditExposureOf(schema, c, exceptOrderID, thisOrder)
}

func creditExposureOf(schema string, c *creditCustomer, exceptOrderID string, thisOrder float64) (CreditExposure, error) {
	out := CreditExposure{Limit: c.Limit, OneTime: c.OneTime}
	if c.OneTime {
		return out, nil
	}
	var err error
	if out.Receivable, err = receivableFor(schema, c); err != nil {
		return out, err
	}
	if out.OpenOrders, err = openOrdersFor(schema, c, exceptOrderID); err != nil {
		return out, err
	}
	out.ThisOrder = thisOrder
	out.Total = round2(out.Receivable + out.OpenOrders + out.ThisOrder)
	return out, nil
}

// enforceCreditAtAccept is the gate AcceptPendingOrder runs before an order is
// released to pick. Over the limit it holds the order, files the release
// request and returns FIN-0860. An order already released by approval passes.
func enforceCreditAtAccept(tenantID, schema, orderID string, order map[string]interface{}, userID string) error {
	if strField(order, "credit_approved_by") != "" {
		return nil
	}
	id := strField(order, "customer_id")
	if id == "" {
		if id = ResolveCustomerLink(tenantID, strField(order, "customer_name")); id != "" {
			_ = mergeSalesOrderData(schema, orderID, map[string]interface{}{"customer_id": id})
		}
	}
	c := loadCreditCustomerByID(schema, id)
	if c == nil || c.Limit <= 0 || c.OneTime {
		return nil
	}
	value := numFromInterface(order["total_amount"])
	if strField(order, "payment_status") == "Confirmed" {
		value = 0 // paid up front: it adds nothing to what they owe
	}
	exp, err := creditExposureOf(schema, c, orderID, value)
	if err != nil {
		return err
	}
	if exp.Total <= c.Limit {
		return nil
	}
	// The owner is whoever has to act: the approver, not the person the hold stopped.
	now := time.Now().UTC().Format(time.RFC3339)
	if err := mergeSalesOrderData(schema, orderID, map[string]interface{}{
		"order_status": "On Hold", "hold_reason": HoldCredit, "hold_owner": creditApproverRole, "hold_since": now,
	}); err != nil {
		return err
	}
	if _, err := db.DB.Exec(fmt.Sprintf(`UPDATE %s.documents SET status = 'On Hold' WHERE doctype = 'SalesOrder' AND id = $1`, schema), orderID); err != nil {
		return err
	}
	requestID, err := fileCreditRelease(tenantID, schema, orderID, c, value, exp, userID)
	if err != nil {
		return err
	}
	LogAuditEvent(tenantID, userID, "ORDER_CREDIT_HOLD", "SUCCESS", fmt.Sprintf("order=%s customer=%s exposure=%.2f limit=%.2f request=%s", orderID, c.ID, exp.Total, c.Limit, requestID))
	return &ValidationError{Code: "FIN-0860", SubFor: "SalesOrder",
		Message: fmt.Sprintf("%s would owe %.2f with this order, over their credit limit of %.2f - the order is on Credit hold until %s approves it (%s)", firstNonEmpty(c.Name, c.ID), exp.Total, c.Limit, creditApproverRole, requestID)}
}

// fileCreditRelease puts one release request in the approval inbox for the
// order; a request already waiting is reused.
func fileCreditRelease(tenantID, schema, orderID string, c *creditCustomer, value float64, exp CreditExposure, userID string) (string, error) {
	var existing string
	err := db.DB.QueryRow(fmt.Sprintf(`SELECT id FROM %s.documents WHERE doctype = $1 AND status IN ('Draft', 'Pending Approval') AND deleted_at IS NULL AND data->>'order_id' = $2 LIMIT 1`, schema),
		creditReleaseDoctype, orderID).Scan(&existing)
	if err == nil {
		return existing, nil
	}
	if err != sql.ErrNoRows {
		return "", err
	}
	id := NewDocID("CHR")
	raw, _ := json.Marshal(map[string]interface{}{"code": id, "order_id": orderID, "party": firstNonEmpty(c.Name, c.ID), "customer_id": c.ID,
		"order_value": value, "exposure": exp.Total, "credit_limit": c.Limit, "status": "Draft"})
	if _, err := db.DB.Exec(fmt.Sprintf(`INSERT INTO %s.documents (id, doctype, data, status, created_by) VALUES ($1, $2, $3, 'Draft', 'system')`, schema), id, creditReleaseDoctype, raw); err != nil {
		return "", err
	}
	requester := strings.TrimSpace(userID)
	if requester == "" {
		requester = "system"
	}
	if err := SubmitForApproval(tenantID, creditReleaseDoctype, id, requester, "Order desk"); err != nil {
		return "", err
	}
	_ = mergeSalesOrderData(schema, orderID, map[string]interface{}{"credit_release_id": id})
	return id, nil
}

// releaseCreditHold lifts a Credit hold once its release is approved and sends
// the order on to Pick, as the accept that was stopped would have.
func releaseCreditHold(tenantID, orderID, approver string) error {
	schema, order, err := fetchSalesOrder(tenantID, orderID)
	if err != nil {
		return err
	}
	if strField(order, "order_status") != "On Hold" || strField(order, "hold_reason") != HoldCredit {
		return nil // already released or cancelled some other way
	}
	if err := mergeSalesOrderData(schema, orderID, map[string]interface{}{
		"order_status": "Reserved", "hold_reason": "", "hold_owner": "", "hold_since": "",
		"credit_approved_by": approver, "credit_approved_at": time.Now().UTC().Format(time.RFC3339),
	}); err != nil {
		return err
	}
	if _, err := db.DB.Exec(fmt.Sprintf(`UPDATE %s.documents SET status = 'Reserved' WHERE doctype = 'SalesOrder' AND id = $1`, schema), orderID); err != nil {
		return err
	}
	_, err = AcceptPendingOrder(tenantID, orderID, approver)
	return err
}

func init() {
	RegisterApprovalDecisionHook(creditReleaseDoctype, false, func(tenantID, docID, decision, actor string) {
		data, _, _, err := fetchDocument(tenantID, creditReleaseDoctype, docID)
		if err != nil {
			return
		}
		orderID := strField(data, "order_id")
		if decision != "Approved" {
			LogAuditEvent(tenantID, actor, "ORDER_CREDIT_HOLD_REJECTED", "SUCCESS", fmt.Sprintf("order=%s request=%s", orderID, docID))
			return
		}
		if err := releaseCreditHold(tenantID, orderID, actor); err != nil {
			LogSystemError(tenantID, "", "ERROR", "credit_control", fmt.Sprintf("credit release %s approved but order %s not released: %v", docID, orderID, err), "")
			return
		}
		LogAuditEvent(tenantID, actor, "ORDER_CREDIT_HOLD_RELEASED", "SUCCESS", fmt.Sprintf("order=%s request=%s", orderID, docID))
	})
}

// checkInvoiceCredit is the credit check at invoice post. The customer is the
// invoice's link, else the typed name as before. Over the limit, only a
// customer flagged "stop" is refused (FIN-0861); anyone else's invoice gets a
// credit_warning and an audit line. No limit, or a one-time customer: no check.
func checkInvoiceCredit(tenantID, invoiceID string, data map[string]interface{}, amount float64) error {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return err
	}
	c := loadCreditCustomerByID(schema, strField(data, "customer_id"))
	if c == nil {
		c = loadCreditCustomerByName(schema, strField(data, "customer"))
	}
	if c == nil || c.Limit <= 0 || c.OneTime {
		return nil
	}
	outstanding, err := receivableFor(schema, c)
	if err != nil {
		return err
	}
	if outstanding+amount <= c.Limit {
		return nil
	}
	msg := fmt.Sprintf("this invoice takes %s's outstanding receivable to %.2f, over their credit limit of %.2f", firstNonEmpty(c.Name, c.ID), outstanding+amount, c.Limit)
	if c.Stop {
		return &ValidationError{Code: "FIN-0861", SubFor: "Customer", Message: "posting stopped: " + msg}
	}
	data["credit_warning"] = msg
	LogAuditEvent(tenantID, "system", "CREDIT_LIMIT_WARNING", "SUCCESS", fmt.Sprintf("invoice=%s %s", invoiceID, msg))
	return nil
}

// customerPaymentTerm is the payment term a customer's documents default to.
func customerPaymentTerm(schema, customerID string) string {
	if c := loadCreditCustomerByID(schema, customerID); c != nil {
		return c.PaymentTerm
	}
	return ""
}

// paymentTermDueDate is a day's due date under a payment term: the day plus
// the term's net days ("" when the term or the day is unknown).
func paymentTermDueDate(tenantID, term, day string) string {
	if term == "" || day == "" {
		return ""
	}
	t, _, err := fetchDocData(tenantID, "PaymentTerm", term)
	if err != nil || t == nil {
		return ""
	}
	d, err := time.ParseInLocation("2006-01-02", strings.TrimSpace(day), time.Local)
	if err != nil {
		return ""
	}
	if strField(t, "end_of_month") == "Yes" { // Stage 148.9: counted from the month's last day
		d = time.Date(d.Year(), d.Month()+1, 0, 0, 0, 0, 0, time.Local)
	}
	return d.AddDate(0, 0, int(numFromInterface(t["net_days"]))).Format("2006-01-02")
}

// defaultInvoiceDueDate fills a SalesInvoice's blank due date from its
// customer's payment terms (Stage 148.3), on every way a draft is saved. A
// typed date always wins; no terms, or no known customer, leaves it blank.
func defaultInvoiceDueDate(tenantID string, data map[string]interface{}) {
	if strings.TrimSpace(strField(data, "due_date")) != "" {
		return
	}
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return
	}
	term := strings.TrimSpace(strField(data, "payment_term"))
	if term == "" {
		customer := strings.TrimSpace(strField(data, "customer"))
		for _, id := range []string{strField(data, "customer_id"), customer, ResolveCustomerLink(tenantID, customer)} {
			if term = customerPaymentTerm(schema, id); term != "" {
				break
			}
		}
	}
	if term == "" {
		return
	}
	if due := paymentTermDueDate(tenantID, term, firstNonEmpty(strField(data, "invoice_date"), BusinessToday())); due != "" {
		data["due_date"] = due
		data["payment_term"] = term
	}
}
