package engines

import (
	"context"
	"custom_erp/db"
	"encoding/json"
	"fmt"
	"log"
	"time"
)

// Stage 26.7.4: Campaign definition (birthday/lapsed-customer triggers) +
// communication log. Campaign is a flat-schema Master doctype
// (db/migrations_stage26_7_4_campaign.sql) - create/list/edit come free
// from the generic doctype machinery, zero bespoke code for those.
// StartCampaignWorker is the only new engine logic: a daily-granularity
// scan that calls the already-built-but-previously-uncalled
// LogCustomerEventToCleverTap for every customer newly matching an Active
// campaign's trigger.

func alreadySentCampaignToday(schema, campaignID, customerID string) bool {
	var exists bool
	_ = db.DB.QueryRow(fmt.Sprintf(
		`SELECT EXISTS(SELECT 1 FROM %s.clevertap_event_log WHERE campaign_id = $1 AND customer_id = $2 AND created_at::date = CURRENT_DATE)`, schema),
		campaignID, customerID).Scan(&exists)
	return exists
}

// sendBirthdayCampaign matches Active Customers whose date_of_birth's
// month-day equals today's, ignoring year (to_char comparison, so it
// naturally recurs every year without any year-tracking logic).
func sendBirthdayCampaign(schema, campaignID, campaignName, messageTemplate string) {
	rows, err := db.DB.Query(fmt.Sprintf(`
		SELECT id, COALESCE(data->>'name', '') FROM %s.documents
		WHERE doctype = 'Customer' AND status = 'Active' AND data->>'date_of_birth' IS NOT NULL
		  AND to_char((data->>'date_of_birth')::date, 'MM-DD') = to_char(CURRENT_DATE, 'MM-DD')`, schema))
	if err != nil {
		log.Printf("[CAMPAIGN] Birthday scan failed for campaign %s in schema %s: %v", campaignID, schema, err)
		return
	}
	type match struct{ id, name string }
	var matches []match
	for rows.Next() {
		var m match
		if err := rows.Scan(&m.id, &m.name); err == nil {
			matches = append(matches, m)
		}
	}
	rows.Close()

	for _, m := range matches {
		if alreadySentCampaignToday(schema, campaignID, m.id) {
			continue
		}
		message := renderTemplate(messageTemplate, map[string]string{"customer_name": m.name})
		if err := logCleverTapEventInSchema(schema, "Birthday Campaign", m.id, map[string]interface{}{
			"campaign_id": campaignID, "campaign_name": campaignName, "message": message,
		}); err != nil {
			log.Printf("[CAMPAIGN] Failed to log birthday event for customer %s (campaign %s): %v", m.id, campaignID, err)
		}
	}
}

// sendLapsedCustomerCampaign matches customers whose most recent Paid
// POSCart is older than lapsedDays - "lapsed" implies a previously active
// customer, so someone who has never purchased is not a match.
func sendLapsedCustomerCampaign(schema, campaignID, campaignName, messageTemplate string, lapsedDays int) {
	rows, err := db.DB.Query(fmt.Sprintf(`
		SELECT data->>'customer_id' FROM %s.documents
		WHERE doctype = 'POSCart' AND status = 'Paid' AND COALESCE(data->>'customer_id', '') <> ''
		GROUP BY data->>'customer_id'
		HAVING MAX(created_at) < CURRENT_DATE - ($1 || ' days')::interval`, schema), lapsedDays)
	if err != nil {
		log.Printf("[CAMPAIGN] Lapsed-customer scan failed for campaign %s in schema %s: %v", campaignID, schema, err)
		return
	}
	var customerIDs []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err == nil {
			customerIDs = append(customerIDs, id)
		}
	}
	rows.Close()

	message := renderTemplate(messageTemplate, map[string]string{})
	for _, customerID := range customerIDs {
		if alreadySentCampaignToday(schema, campaignID, customerID) {
			continue
		}
		if err := logCleverTapEventInSchema(schema, "Lapsed Customer Campaign", customerID, map[string]interface{}{
			"campaign_id": campaignID, "campaign_name": campaignName, "message": message,
		}); err != nil {
			log.Printf("[CAMPAIGN] Failed to log lapsed-customer event for customer %s (campaign %s): %v", customerID, campaignID, err)
		}
	}
}

func runCampaignsForSchema(schema string) {
	rows, err := db.DB.Query(fmt.Sprintf(
		`SELECT id, data FROM %s.documents WHERE doctype = 'Campaign' AND status = 'Active'`, schema))
	if err != nil {
		log.Printf("[CAMPAIGN] Failed to list campaigns in schema %s: %v", schema, err)
		return
	}
	type row struct{ id, data string }
	var campaigns []row
	for rows.Next() {
		var r row
		if err := rows.Scan(&r.id, &r.data); err == nil {
			campaigns = append(campaigns, r)
		}
	}
	rows.Close()

	for _, c := range campaigns {
		var data map[string]interface{}
		if err := json.Unmarshal([]byte(c.data), &data); err != nil {
			log.Printf("[CAMPAIGN] Skipping corrupt campaign %s in schema %s: %v", c.id, schema, err)
			continue
		}
		name, _ := data["name"].(string)
		triggerType, _ := data["trigger_type"].(string)
		messageTemplate, _ := data["message_template"].(string)
		switch triggerType {
		case "Birthday":
			sendBirthdayCampaign(schema, c.id, name, messageTemplate)
		case "Lapsed Customer":
			lapsedDays := int(numFromInterface(data["lapsed_days"]))
			if lapsedDays <= 0 {
				lapsedDays = 90
			}
			sendLapsedCustomerCampaign(schema, c.id, name, messageTemplate, lapsedDays)
		default:
			log.Printf("[CAMPAIGN] Campaign %s in schema %s has an unknown trigger_type %q", c.id, schema, triggerType)
		}
	}
}

// StartCampaignWorker polls every tenant schema (re-queried each tick, same
// convention as StartOutboxWorker) for Active campaigns whose trigger
// newly matches a customer.
func StartCampaignWorker(ctx context.Context, interval time.Duration) {
	ticker := time.NewTicker(interval)
	go func() {
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				if db.DB == nil {
					continue
				}
				schemas, err := listTenantSchemas()
				if err != nil {
					log.Printf("[CAMPAIGN] Failed to list tenant schemas: %v", err)
					continue
				}
				for _, schema := range schemas {
					runCampaignsForSchema(schema)
				}
			}
		}
	}()
}

// CampaignROIRow is one campaign's reach + attributed revenue - a rough
// proxy given this system has no real marketing-spend tracking beyond the
// campaign's own optional cost field; not a precise attribution model.
type CampaignROIRow struct {
	CampaignID         string   `json:"campaign_id"`
	CampaignName       string   `json:"campaign_name"`
	CustomersTargeted  int      `json:"customers_targeted"`
	AttributedRevenue  float64  `json:"attributed_revenue"`
	Cost               float64  `json:"cost"`
	ROI                *float64 `json:"roi,omitempty"`
}

// GetCampaignROIReport attributes revenue to a campaign as: every Paid
// POSCart from a customer that campaign actually sent to, dated on or
// after the campaign's own creation date. A simplification (no real
// pre/post-campaign control-group comparison), the same spirit as this
// codebase's other approximated reports (e.g. GetPayablesAgeingReport's
// own documented one).
func GetCampaignROIReport(tenantID string) ([]CampaignROIRow, error) {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return nil, err
	}
	rows, err := db.DB.Query(fmt.Sprintf(`
		SELECT id, COALESCE(data->>'name', ''), COALESCE((data->>'cost')::numeric, 0), created_at
		FROM %s.documents WHERE doctype = 'Campaign'`, schema))
	if err != nil {
		return nil, err
	}
	type campaignRow struct {
		id, name  string
		cost      float64
		createdAt time.Time
	}
	var campaigns []campaignRow
	for rows.Next() {
		var c campaignRow
		if err := rows.Scan(&c.id, &c.name, &c.cost, &c.createdAt); err == nil {
			campaigns = append(campaigns, c)
		}
	}
	rows.Close()

	out := []CampaignROIRow{}
	for _, c := range campaigns {
		var targeted int
		if err := db.DB.QueryRow(fmt.Sprintf(
			`SELECT COUNT(DISTINCT customer_id) FROM %s.clevertap_event_log WHERE campaign_id = $1`, schema), c.id).
			Scan(&targeted); err != nil {
			return nil, err
		}
		var revenue float64
		if err := db.DB.QueryRow(fmt.Sprintf(`
			SELECT COALESCE(SUM((data->>'amount_paid')::numeric), 0) FROM %s.documents
			WHERE doctype = 'POSCart' AND status = 'Paid' AND created_at >= $1
			  AND data->>'customer_id' IN (SELECT DISTINCT customer_id FROM %s.clevertap_event_log WHERE campaign_id = $2)`,
			schema, schema), c.createdAt, c.id).Scan(&revenue); err != nil {
			return nil, err
		}
		row := CampaignROIRow{CampaignID: c.id, CampaignName: c.name, CustomersTargeted: targeted, AttributedRevenue: revenue, Cost: c.cost}
		if c.cost > 0 {
			roi := (revenue - c.cost) / c.cost
			row.ROI = &roi
		}
		out = append(out, row)
	}
	return out, nil
}

func init() {
	RegisterReport(ReportDefinition{
		ID: "campaign-roi", Label: "Campaign ROI", Category: "CRM",
		Columns: []ReportColumn{
			{Key: "campaign_id", Label: "Campaign ID"}, {Key: "campaign_name", Label: "Campaign"},
			{Key: "customers_targeted", Label: "Customers Targeted"},
			{Key: "attributed_revenue", Label: "Attributed Revenue", Sensitive: true},
			{Key: "cost", Label: "Cost", Sensitive: true}, {Key: "roi", Label: "ROI", Sensitive: true},
		},
		Run: func(tenantID string, params map[string]string) ([]map[string]interface{}, error) {
			rows, err := GetCampaignROIReport(tenantID)
			if err != nil {
				return nil, err
			}
			return structsToRows(rows)
		},
	})
}
