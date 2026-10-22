package engines

import (
	"custom_erp/db"
	"fmt"
	"strings"
)

// Stage 101.4 - what journeys react to. A crm_events row is written where the
// thing happens (a bill is paid, a customer is created, a return is
// refunded, an order arrives for a customer) and the journey worker reads it.
// dedupe_key makes a repeated event - a retried checkout, a webhook delivered
// twice - one event (the blueprint's duplicate-event acceptance case).

const (
	CRMEventCustomerCreated = "customer_created"
	CRMEventBillPaid        = "bill_paid"
	CRMEventReturn          = "return_completed"
	CRMEventOrderPlaced     = "order_placed"
	CRMEventProductArrived  = "product_arrived"
	CRMEventFeedbackLow     = "feedback_low"
	CRMEventCartAbandoned   = "cart_abandoned"
	CRMEventCartPurchased   = "cart_purchased"
	CRMEventMessageReply    = "message_reply"
)

// CRMEventTypes are the events a journey may start on or wait for, with the
// words the journey editor shows.
var CRMEventTypes = []struct{ Key, Label string }{
	{CRMEventCustomerCreated, "A customer is added"},
	{CRMEventBillPaid, "A bill is paid"},
	{CRMEventReturn, "A return is completed"},
	{CRMEventOrderPlaced, "An online order is placed"},
	{CRMEventProductArrived, "An item the customer asked for arrives"},
	{CRMEventFeedbackLow, "A low feedback score"},
	{CRMEventCartAbandoned, "A web-shop cart is left (Shopify, Stage 99)"},
	{CRMEventCartPurchased, "A web-shop cart is bought (Shopify, Stage 99)"},
	{CRMEventMessageReply, "The customer replies to a message"},
}

func crmKnownEvent(t string) bool {
	for _, e := range CRMEventTypes {
		if e.Key == t {
			return true
		}
	}
	return false
}

// RecordCRMEvent records one event once. The same event type for the same
// reference is recorded only the first time.
func RecordCRMEvent(schema, customerID, eventType, refID string, payload map[string]interface{}) error {
	customerID = strings.TrimSpace(customerID)
	if customerID == "" || !crmKnownEvent(eventType) {
		return nil
	}
	if payload == nil {
		payload = map[string]interface{}{}
	}
	key := eventType + ":" + firstNonEmpty(strings.TrimSpace(refID), customerID)
	_, err := db.DB.Exec(fmt.Sprintf(`INSERT INTO %s.crm_events (customer_id, event_type, ref_id, payload, dedupe_key)
		VALUES ($1, $2, NULLIF($3, ''), $4, $5) ON CONFLICT (dedupe_key) DO NOTHING`, schema),
		customerID, eventType, refID, crmJSON(payload), key)
	return err
}

// RecordCRMEventForTenant is RecordCRMEvent for callers holding a tenant id.
// It never fails the caller: a CRM event is a consequence of a sale, not a
// condition of it.
func RecordCRMEventForTenant(tenantID, customerID, eventType, refID string, payload map[string]interface{}) {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return
	}
	if err := RecordCRMEvent(schema, customerID, eventType, refID, payload); err != nil && !strings.Contains(err.Error(), "does not exist") {
		LogSystemError(tenantID, "", "WARN", "crm_events", fmt.Sprintf("%s for %s not recorded: %v", eventType, customerID, err), "")
	}
}
