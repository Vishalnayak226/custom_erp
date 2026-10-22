package engines

import (
	"custom_erp/db"
	"fmt"
)

// Stage 101.4.1 - ready-made journeys (the blueprint's J01-J08): welcome,
// thank-you after the first bill, birthday, anniversary, points about to
// expire, win-back, product arrived, a left web-shop cart, and the feedback
// survey after a bill. "Add the ready-made journeys" creates each once, with
// its own template (WhatsApp, editable), published but switched OFF - the
// client turns on the ones it wants, after linking the WhatsApp template
// name WhatsApp approved (or changing the channel).

type crmReadyJourney struct {
	Key, Name, Description string
	Template               CRMTemplateInput
	Def                    func(templateID string) CRMJourneyDefinition
}

var crmReadyJourneys = []crmReadyJourney{
	{Key: "welcome", Name: "Welcome", Description: "A hello an hour after a customer is added.",
		Template: CRMTemplateInput{Name: "Welcome", Channel: CRMChannelWhatsApp, Purpose: CRMPurposeMarketing,
			Body: "Hi {{customer.first_name|there}}, welcome to {{business.name|our store}}! We'll share new arrivals and offers here. To stop: {{link.preferences}}"},
		Def: func(t string) CRMJourneyDefinition {
			return CRMJourneyDefinition{Trigger: CRMJourneyTrigger{Type: "event", Event: CRMEventCustomerCreated},
				Steps: []CRMJourneyStep{{Type: "wait", Hours: 1}, {Type: "send", Channel: CRMChannelWhatsApp, TemplateID: t, Purpose: CRMPurposeMarketing}}}
		}},
	{Key: "first_bill_thanks", Name: "Thank you for the first bill", Description: "Two hours after a customer's first bill.",
		Template: CRMTemplateInput{Name: "Thank you - first bill", Channel: CRMChannelWhatsApp, Purpose: CRMPurposeService,
			Body: "Thank you for shopping at {{store.name|our store}}, {{customer.first_name|there}}. Your bill {{bill.number}} is saved with us."},
		Def: func(t string) CRMJourneyDefinition {
			return CRMJourneyDefinition{Trigger: CRMJourneyTrigger{Type: "event", Event: CRMEventBillPaid, FirstBillOnly: true},
				Steps: []CRMJourneyStep{{Type: "wait", Hours: 2}, {Type: "send", Channel: CRMChannelWhatsApp, TemplateID: t, Purpose: CRMPurposeService}}}
		}},
	{Key: "birthday", Name: "Birthday", Description: "On the birthday, with a 10% voucher for 7 days.",
		Template: CRMTemplateInput{Name: "Birthday", Channel: CRMChannelWhatsApp, Purpose: CRMPurposeMarketing,
			Body: "Happy birthday, {{customer.first_name|there}}! Here is {{voucher.value}} for you at {{business.name|our store}}: code {{voucher.code}}, valid till {{voucher.expiry}}. To stop: {{link.preferences}}"},
		Def: func(t string) CRMJourneyDefinition {
			return CRMJourneyDefinition{Trigger: CRMJourneyTrigger{Type: "date", Date: "birthday"}, CooldownDays: 300,
				Steps: []CRMJourneyStep{{Type: "send", Channel: CRMChannelWhatsApp, TemplateID: t, Purpose: CRMPurposeMarketing,
					Voucher: map[string]interface{}{"discount_type": "Percentage", "discount_value": 10, "valid_days": 7, "channel": "Any"}}}}
		}},
	{Key: "anniversary", Name: "Anniversary", Description: "On the anniversary, with a 10% voucher for 7 days.",
		Template: CRMTemplateInput{Name: "Anniversary", Channel: CRMChannelWhatsApp, Purpose: CRMPurposeMarketing,
			Body: "Happy anniversary, {{customer.first_name|there}}! Celebrate with {{voucher.value}} at {{business.name|our store}}: code {{voucher.code}}, till {{voucher.expiry}}. To stop: {{link.preferences}}"},
		Def: func(t string) CRMJourneyDefinition {
			return CRMJourneyDefinition{Trigger: CRMJourneyTrigger{Type: "date", Date: "anniversary"}, CooldownDays: 300,
				Steps: []CRMJourneyStep{{Type: "send", Channel: CRMChannelWhatsApp, TemplateID: t, Purpose: CRMPurposeMarketing,
					Voucher: map[string]interface{}{"discount_type": "Percentage", "discount_value": 10, "valid_days": 7, "channel": "Any"}}}}
		}},
	{Key: "points_expiring", Name: "Points about to expire", Description: "Seven days before points lapse.",
		Template: CRMTemplateInput{Name: "Points expiring", Channel: CRMChannelWhatsApp, Purpose: CRMPurposeService,
			Body: "{{customer.first_name|Hello}}, {{points.expiring}} of your {{business.name|loyalty}} points expire on {{points.expiry_date}}. Use them on your next visit."},
		Def: func(t string) CRMJourneyDefinition {
			return CRMJourneyDefinition{Trigger: CRMJourneyTrigger{Type: "date", Date: "points_expiring", DaysBefore: 7}, CooldownDays: 25,
				Steps: []CRMJourneyStep{{Type: "send", Channel: CRMChannelWhatsApp, TemplateID: t, Purpose: CRMPurposeService}}}
		}},
	{Key: "win_back", Name: "Win back (not seen for 90 days)", Description: "A 15% voucher; if they still do not come in 14 days, a call task for their salesman.",
		Template: CRMTemplateInput{Name: "We miss you", Channel: CRMChannelWhatsApp, Purpose: CRMPurposeMarketing,
			Body: "We miss you, {{customer.first_name|there}}! Come back for {{voucher.value}} with code {{voucher.code}} (till {{voucher.expiry}}). To stop: {{link.preferences}}"},
		Def: func(t string) CRMJourneyDefinition {
			return CRMJourneyDefinition{Trigger: CRMJourneyTrigger{Type: "inactive", Days: 90}, Goal: &CRMJourneyGoal{Event: CRMEventBillPaid}, CooldownDays: 180,
				Steps: []CRMJourneyStep{
					{Type: "send", Channel: CRMChannelWhatsApp, TemplateID: t, Purpose: CRMPurposeMarketing,
						Voucher: map[string]interface{}{"discount_type": "Percentage", "discount_value": 15, "valid_days": 14, "channel": "Any"}},
					{Type: "wait_event", Event: CRMEventBillPaid, Hours: 14 * 24, Yes: -1},
					{Type: "task", Kind: "call", Title: "Call {{customer.first_name|the customer}} - not seen for 90 days", Owner: "preferred_salesman", DueHours: 48},
				}}
		}},
	{Key: "product_arrived", Name: "Product arrived", Description: "When an item the customer asked for arrives (a hold kept, an arrival task done).",
		Template: CRMTemplateInput{Name: "Your item has arrived", Channel: CRMChannelWhatsApp, Purpose: CRMPurposeService,
			Body: "Good news, {{customer.first_name|there}}: {{item.name|your item}} is here at {{store.name|the store}}."},
		Def: func(t string) CRMJourneyDefinition {
			return CRMJourneyDefinition{Trigger: CRMJourneyTrigger{Type: "event", Event: CRMEventProductArrived},
				Steps: []CRMJourneyStep{{Type: "send", Channel: CRMChannelWhatsApp, TemplateID: t, Purpose: CRMPurposeService}}}
		}},
	{Key: "abandoned_cart", Name: "Cart left on the web shop", Description: "An hour after a web-shop cart is left (needs Stage 99's Shopify events); ends when the same cart is bought.",
		Template: CRMTemplateInput{Name: "Your cart is waiting", Channel: CRMChannelWhatsApp, Purpose: CRMPurposeMarketing,
			Body: "{{customer.first_name|Hi}}, you left something in your cart at {{business.name|our shop}}. It's still waiting for you: {{item.link|}} To stop: {{link.preferences}}"},
		Def: func(t string) CRMJourneyDefinition {
			return CRMJourneyDefinition{Trigger: CRMJourneyTrigger{Type: "event", Event: CRMEventCartAbandoned, StaleHours: 24}, PerCart: true,
				Goal: &CRMJourneyGoal{Event: CRMEventCartPurchased}, CooldownDays: 1,
				Steps: []CRMJourneyStep{{Type: "wait", Hours: 1}, {Type: "send", Channel: CRMChannelWhatsApp, TemplateID: t, Purpose: CRMPurposeMarketing}}}
		}},
}

// InstallCRMReadyJourneys adds every ready-made journey (and the feedback
// survey) the client does not have yet. Each is published and switched off.
func InstallCRMReadyJourneys(tenantID, actor string) ([]CRMJourney, error) {
	schema, err := crmSchema(tenantID)
	if err != nil {
		return nil, err
	}
	for _, r := range crmReadyJourneys {
		var exists bool
		if err := db.DB.QueryRow(fmt.Sprintf(`SELECT EXISTS(SELECT 1 FROM %s.crm_journeys WHERE ready_made = $1)`, schema), r.Key).Scan(&exists); err != nil {
			return nil, err
		}
		if exists {
			continue
		}
		t, err := SaveCRMTemplate(tenantID, "", r.Template, actor)
		if err != nil {
			return nil, err
		}
		j, err := SaveCRMJourney(tenantID, "", r.Name, r.Description, r.Def(t.ID), actor)
		if err != nil {
			return nil, err
		}
		if _, err := db.DB.Exec(fmt.Sprintf(`UPDATE %s.crm_journeys SET ready_made = $2 WHERE id = $1`, schema), j.ID, r.Key); err != nil {
			return nil, err
		}
		if _, err := PublishCRMJourney(tenantID, j.ID, actor); err != nil {
			return nil, err
		}
	}
	var surveys int
	_ = db.DB.QueryRow(fmt.Sprintf(`SELECT COUNT(*) FROM %s.crm_surveys`, schema)).Scan(&surveys)
	if surveys == 0 {
		t, err := SaveCRMTemplate(tenantID, "", CRMTemplateInput{Name: "Feedback after a bill", Channel: CRMChannelWhatsApp, Purpose: CRMPurposeService,
			Body: "Thank you for shopping at {{store.name|our store}}, {{customer.first_name|there}}. How did we do? Tap to answer: {{link.feedback}}"}, actor)
		if err != nil {
			return nil, err
		}
		if _, err := SaveCRMSurvey(tenantID, "", CRMSurvey{Name: "After a bill", Question: "How likely are you to recommend us to a friend?", Scale: "nps",
			Channel: CRMChannelWhatsApp, TemplateID: t.ID, DelayHours: 2, CooldownDays: 30, LowScore: 6}, actor); err != nil {
			return nil, err
		}
	}
	return ListCRMJourneys(tenantID)
}
