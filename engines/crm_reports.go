package engines

import (
	"custom_erp/db"
	"fmt"
	"strings"
	"time"
)

// Stage 101.7.1 - CRM reports (the blueprint's report catalog) and the CRM
// dashboard. Every report is a ReportDefinition, so it is listed, filtered by
// store, masked by role, exported and scheduled like every other report.
// Each description says its time zone (crm.time_zone), its date basis, and
// that it is computed when it runs. The dashboard tiles are the CRM screen's
// own call until Stage 100's dashboards exist; Stage 100 can take these
// reports as datasets.

func crmReportNote(basis string) string {
	return "Dates in the client's CRM time zone (Settings > CRM). Date basis: " + basis + ". Computed when the report runs - not a stored snapshot."
}

func crmRangeArgs(params map[string]string, zone string, col string, args []interface{}) (string, []interface{}) {
	where := ""
	if f := strings.TrimSpace(params["from_date"]); f != "" {
		args = append(args, f)
		where += fmt.Sprintf(" AND %s::date >= $%d::date", crmDocLocalSQL(col, zone), len(args))
	}
	if t := strings.TrimSpace(params["to_date"]); t != "" {
		args = append(args, t)
		where += fmt.Sprintf(" AND %s::date <= $%d::date", crmDocLocalSQL(col, zone), len(args))
	}
	return where, args
}

var crmDateParams = []ReportParam{{Key: "from_date", Label: "From", Type: "date"}, {Key: "to_date", Label: "To", Type: "date"}}

func crmQueryRows(query string, args ...interface{}) ([]map[string]interface{}, error) {
	rows, err := db.DB.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	cols, _ := rows.Columns()
	out := []map[string]interface{}{}
	for rows.Next() {
		vals := make([]interface{}, len(cols))
		ptrs := make([]interface{}, len(cols))
		for i := range vals {
			ptrs[i] = &vals[i]
		}
		if err := rows.Scan(ptrs...); err != nil {
			return nil, err
		}
		m := map[string]interface{}{}
		for i, c := range cols {
			switch v := vals[i].(type) {
			case []byte:
				m[c] = string(v)
			case time.Time:
				m[c] = v.Format("2006-01-02 15:04")
			default:
				m[c] = v
			}
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

func init() {
	reg := func(def ReportDefinition) {
		def.Category = "CRM"
		def.ModuleKey = "crm_loyalty"
		RegisterReport(def)
	}
	reg(ReportDefinition{ID: "crm-customer-summary", Label: "CRM Customer Summary", StoreKey: "-",
		Description: "How many customers are in each lifecycle state, what they spend and how many can be reached. " + crmReportNote("each customer's bills to date"),
		Columns: []ReportColumn{{Key: "lifecycle", Label: "Lifecycle"}, {Key: "customers", Label: "Customers", Total: true}, {Key: "spend", Label: "Spend (net of returns)", Sensitive: true, Total: true},
			{Key: "avg_spend", Label: "Average spend", Sensitive: true}, {Key: "reachable_sms", Label: "Offers by SMS", Total: true}, {Key: "reachable_whatsapp", Label: "Offers by WhatsApp", Total: true},
			{Key: "reachable_email", Label: "Offers by email", Total: true}},
		Run: func(tenantID string, params map[string]string) ([]map[string]interface{}, error) {
			schema, err := crmSchema(tenantID)
			if err != nil {
				return nil, err
			}
			tick := func(f string) string {
				return fmt.Sprintf("COUNT(*) FILTER (WHERE LOWER(COALESCE(data->>'%s', '')) IN ('true', '1', 'yes', 'on'))", f)
			}
			return crmQueryRows(crmFactsCTE(schema, crmLifecycleThresholds(schema), false) + fmt.Sprintf(` SELECT lifecycle, COUNT(*) AS customers, ROUND(SUM(spend), 2) AS spend,
				ROUND(AVG(spend) FILTER (WHERE visits > 0), 2) AS avg_spend, %s AS reachable_sms, %s AS reachable_whatsapp, %s AS reachable_email FROM cf GROUP BY lifecycle ORDER BY customers DESC`,
				tick("consent_sms"), tick("consent_whatsapp"), tick("consent_email")))
		}})
	reg(ReportDefinition{ID: "crm-consent-audit", Label: "Consent Audit", StoreKey: "-",
		Description: "Every consent given, refused or withdrawn, with who recorded it, how and under which notice. " + crmReportNote("when the consent was recorded"),
		Params:      crmDateParams,
		Columns: []ReportColumn{{Key: "recorded_at", Label: "Recorded"}, {Key: "customer_id", Label: "Customer"}, {Key: "customer", Label: "Name"}, {Key: "channel", Label: "Channel"},
			{Key: "purpose", Label: "Purpose"}, {Key: "action", Label: "Action"}, {Key: "source", Label: "Source"}, {Key: "recorded_by", Label: "Recorded by"}, {Key: "notice_version", Label: "Notice"}, {Key: "evidence", Label: "Evidence"}},
		Run: func(tenantID string, params map[string]string) ([]map[string]interface{}, error) {
			schema, err := crmSchema(tenantID)
			if err != nil {
				return nil, err
			}
			where, args := crmRangeArgs(params, crmZoneName(schema), "l.created_at", nil)
			return crmQueryRows(fmt.Sprintf(`SELECT l.created_at AS recorded_at, l.customer_id, COALESCE(c.data->>'name', '') AS customer, l.channel, l.purpose, l.action, l.source,
				COALESCE(l.recorded_by, '') AS recorded_by, COALESCE(l.notice_version, '') AS notice_version, COALESCE(l.evidence, '') AS evidence
				FROM %s.crm_consent_ledger l LEFT JOIN %s.documents c ON c.id = l.customer_id AND c.doctype = 'Customer' WHERE TRUE%s ORDER BY l.id DESC LIMIT 5000`, schema, schema, where), args...)
		}})
	reg(ReportDefinition{ID: "crm-segment-members", Label: "Segment Members", StoreKey: "-",
		Description: "Who is in a segment now (its current version). " + crmReportNote("each customer's bills to date"),
		Params:      []ReportParam{{Key: "segment_id", Label: "Segment (id from CRM > Segments)", Type: "text", Required: true}},
		Columns: []ReportColumn{{Key: "customer_id", Label: "Customer"}, {Key: "name", Label: "Name"}, {Key: "phone", Label: "Phone"}, {Key: "email", Label: "Email"},
			{Key: "lifecycle", Label: "Lifecycle"}, {Key: "rfm", Label: "RFM"}, {Key: "visits", Label: "Bills"}, {Key: "spend", Label: "Spend", Sensitive: true}, {Key: "last_bill", Label: "Last bill"}},
		Run: func(tenantID string, params map[string]string) ([]map[string]interface{}, error) {
			schema, err := crmSchema(tenantID)
			if err != nil {
				return nil, err
			}
			seg, err := crmGetSegment(schema, strings.TrimSpace(params["segment_id"]), 0)
			if err != nil {
				return nil, err
			}
			pred, args, err := crmCompileSegment(schema, &seg.Definition, 1)
			if err != nil {
				return nil, err
			}
			return crmQueryRows(crmFactsCTE(schema, crmLifecycleThresholds(schema), false)+` SELECT id AS customer_id, name, phone, email, lifecycle, rfm, visits, ROUND(spend, 2) AS spend,
				to_char(last_at, 'YYYY-MM-DD') AS last_bill FROM cf WHERE `+pred+` ORDER BY spend DESC LIMIT 5000`, args...)
		}})
	reg(ReportDefinition{ID: "crm-campaign-summary", Label: "Campaign Summary", StoreKey: "-",
		Description: "Each campaign run: reached, held out, suppressed, delivered, credited sales and cost. Delivery is blank where the channel does not report it. " + crmReportNote("the run's start; a bill is credited to the last offer within the attribution days before it"),
		Params:      crmDateParams,
		Columns: []ReportColumn{{Key: "campaign", Label: "Campaign"}, {Key: "run_id", Label: "Run"}, {Key: "channel", Label: "Channel"}, {Key: "started", Label: "Started"}, {Key: "status", Label: "Status"},
			{Key: "matched", Label: "Matched", Total: true}, {Key: "held_out", Label: "Hold-out", Total: true}, {Key: "suppressed", Label: "Not sent", Total: true}, {Key: "sent", Label: "Sent", Total: true},
			{Key: "delivered", Label: "Delivered"}, {Key: "failed", Label: "Failed", Total: true}, {Key: "bills_credited", Label: "Bills credited", Total: true},
			{Key: "sales_credited", Label: "Sales credited", Sensitive: true, Total: true}, {Key: "cost", Label: "Cost", Sensitive: true, Total: true}},
		Run: func(tenantID string, params map[string]string) ([]map[string]interface{}, error) {
			schema, err := crmSchema(tenantID)
			if err != nil {
				return nil, err
			}
			where, args := crmRangeArgs(params, crmZoneName(schema), "COALESCE(r.started_at, r.created_at)", nil)
			runs, err := crmQueryRows(fmt.Sprintf(`SELECT r.id, COALESCE(c.data->>'name', r.campaign_id) AS campaign, r.status, COALESCE(r.started_at, r.scheduled_at) AS started
				FROM %s.crm_campaign_runs r LEFT JOIN %s.documents c ON c.id = r.campaign_id WHERE r.test = FALSE%s ORDER BY r.created_at DESC LIMIT 500`, schema, schema, where), args...)
			if err != nil {
				return nil, err
			}
			out := []map[string]interface{}{}
			for _, run := range runs {
				res, err := GetCRMCampaignResults(tenantID, crmString(run["id"]))
				if err != nil {
					continue
				}
				suppressed := 0
				for _, n := range res.Suppressed {
					suppressed += n
				}
				row := map[string]interface{}{"campaign": run["campaign"], "run_id": run["id"], "channel": res.Channel, "started": run["started"], "status": run["status"],
					"matched": res.Matched, "held_out": res.HeldOut, "suppressed": suppressed, "sent": res.Sent, "failed": res.Failed,
					"bills_credited": res.BillsCredited, "sales_credited": res.SalesCredited, "cost": res.CostEstimate}
				if res.Delivered != nil {
					row["delivered"] = *res.Delivered
				}
				if res.CostActual != nil {
					row["cost"] = *res.CostActual
				}
				out = append(out, row)
			}
			return out, nil
		}})
	reg(ReportDefinition{ID: "crm-message-detail", Label: "Message Detail", StoreKey: "-",
		Description: "Every CRM message and its decision: sent, delivered, or why not (the reason code). " + crmReportNote("when the message was queued"),
		Params:      append(append([]ReportParam{}, crmDateParams...), ReportParam{Key: "status", Label: "Status", Type: "select", Options: ",Queued,Sending,Accepted,Delivered,Read,Failed,Unknown,Suppressed,Expired,Cancelled,HeldOut,Simulated"}),
		Columns: []ReportColumn{{Key: "queued_at", Label: "Queued"}, {Key: "kind", Label: "From"}, {Key: "customer_id", Label: "Customer"}, {Key: "customer", Label: "Name"}, {Key: "channel", Label: "Channel"},
			{Key: "purpose", Label: "Purpose"}, {Key: "status", Label: "Status"}, {Key: "reason", Label: "Reason"}, {Key: "contact", Label: "To"}, {Key: "parts", Label: "SMS parts"}, {Key: "cost", Label: "Cost", Sensitive: true}},
		Run: func(tenantID string, params map[string]string) ([]map[string]interface{}, error) {
			schema, err := crmSchema(tenantID)
			if err != nil {
				return nil, err
			}
			where, args := crmRangeArgs(params, crmZoneName(schema), "s.created_at", nil)
			if st := strings.TrimSpace(params["status"]); st != "" {
				args = append(args, st)
				where += fmt.Sprintf(" AND s.status = $%d", len(args))
			}
			return crmQueryRows(fmt.Sprintf(`SELECT s.created_at AS queued_at, s.run_kind AS kind, s.customer_id, COALESCE(c.data->>'name', '') AS customer, s.channel, s.purpose, s.status,
				COALESCE(s.reason_code, '') AS reason, COALESCE(s.contact_masked, '') AS contact, s.parts, COALESCE(s.cost_actual, s.cost_estimate) AS cost
				FROM %s.crm_sends s LEFT JOIN %s.documents c ON c.id = s.customer_id AND c.doctype = 'Customer' WHERE TRUE%s ORDER BY s.created_at DESC LIMIT 5000`, schema, schema, where), args...)
		}})
	reg(ReportDefinition{ID: "crm-journey-performance", Label: "Journey Performance", StoreKey: "-",
		Description: "Per journey: customers who entered, are in it now, finished, left on reaching the goal, and messages sent. " + crmReportNote("when the customer entered"),
		Params:      crmDateParams,
		Columns: []ReportColumn{{Key: "journey", Label: "Journey"}, {Key: "enabled", Label: "On"}, {Key: "entered", Label: "Entered", Total: true}, {Key: "live", Label: "In it now", Total: true},
			{Key: "completed", Label: "Finished", Total: true}, {Key: "goal", Label: "Reached the goal", Total: true}, {Key: "messages", Label: "Messages sent", Total: true}},
		Run: func(tenantID string, params map[string]string) ([]map[string]interface{}, error) {
			schema, err := crmSchema(tenantID)
			if err != nil {
				return nil, err
			}
			where, args := crmRangeArgs(params, crmZoneName(schema), "r.started_at", nil)
			return crmQueryRows(fmt.Sprintf(`SELECT j.name AS journey, CASE WHEN j.enabled THEN 'Yes' ELSE 'No' END AS enabled, COUNT(r.id) AS entered,
				COUNT(r.id) FILTER (WHERE r.state IN ('Active', 'Waiting')) AS live, COUNT(r.id) FILTER (WHERE r.state = 'Completed') AS completed,
				COUNT(r.id) FILTER (WHERE r.exit_reason = 'goal') AS goal,
				(SELECT COUNT(*) FROM %[1]s.crm_sends s WHERE s.run_kind = 'journey' AND split_part(s.run_id, ':', 1) IN (SELECT id FROM %[1]s.crm_journey_runs WHERE journey_id = j.id)
				   AND s.status IN `+crmCountedStatusesSQL+`) AS messages
				FROM %[1]s.crm_journeys j LEFT JOIN %[1]s.crm_journey_runs r ON r.journey_id = j.id%[2]s GROUP BY j.id, j.name, j.enabled ORDER BY j.name`, schema, where), args...)
		}})
	reg(ReportDefinition{ID: "crm-credited-sales", Label: "Credited Sales", StoreKey: "store",
		Description: "Bills credited to a CRM message: the last marketing message the customer got within the attribution days before the bill, once per bill. " + crmReportNote("the bill date"),
		Params:      crmDateParams,
		Columns: []ReportColumn{{Key: "bill_date", Label: "Bill date"}, {Key: "bill", Label: "Bill"}, {Key: "store", Label: "Store"}, {Key: "customer", Label: "Customer"},
			{Key: "credited_to", Label: "Credited to"}, {Key: "message_at", Label: "Message sent"}, {Key: "amount", Label: "Amount", Sensitive: true, Total: true}},
		Run: func(tenantID string, params map[string]string) ([]map[string]interface{}, error) {
			schema, err := crmSchema(tenantID)
			if err != nil {
				return nil, err
			}
			where, args := crmRangeArgs(params, crmZoneName(schema), "b.created_at", []interface{}{GetSettingInt(tenantID, "crm.attribution_days")})
			return crmQueryRows(fmt.Sprintf(`SELECT b.created_at AS bill_date, COALESCE(NULLIF(b.data->>'bill_number', ''), b.id) AS bill, COALESCE(b.data->>'location', '') AS store,
				COALESCE(c.data->>'name', '') AS customer, m.run_kind || ' ' || m.run_id AS credited_to, m.sent_at AS message_at, %[2]s AS amount
				FROM %[1]s.documents b LEFT JOIN %[1]s.documents c ON c.id = b.data->>'customer_id' AND c.doctype = 'Customer'
				JOIN LATERAL (SELECT s.run_kind, s.run_id, s.sent_at FROM %[1]s.crm_sends s WHERE s.customer_id = b.data->>'customer_id' AND s.purpose = 'marketing'
				  AND s.status IN ('Accepted', 'Delivered', 'Read', 'Unknown', 'Simulated') AND s.sent_at < b.created_at AND s.sent_at >= b.created_at - make_interval(days => $1)
				  ORDER BY s.sent_at DESC LIMIT 1) m ON TRUE
				WHERE b.doctype = 'POSCart' AND b.status = 'Paid'%[3]s ORDER BY b.created_at DESC LIMIT 5000`, schema, crmBillAmountSQL("b"), where), args...)
		}})
	reg(ReportDefinition{ID: "crm-rfm-lifecycle", Label: "RFM and Lifecycle", StoreKey: "-",
		Description: "Every customer's RFM segment (scored against the whole customer base, as the RFM report does) and lifecycle state. " + crmReportNote("each customer's bills to date"),
		Params:      []ReportParam{{Key: "lifecycle", Label: "Lifecycle", Type: "select", Options: "," + strings.Join(CRMLifecycleStates, ",")}},
		Columns: []ReportColumn{{Key: "customer_id", Label: "Customer"}, {Key: "name", Label: "Name"}, {Key: "lifecycle", Label: "Lifecycle"}, {Key: "rfm", Label: "RFM"},
			{Key: "visits", Label: "Bills"}, {Key: "spend", Label: "Spend", Sensitive: true}, {Key: "days_since", Label: "Days since last bill"}},
		Run: func(tenantID string, params map[string]string) ([]map[string]interface{}, error) {
			schema, err := crmSchema(tenantID)
			if err != nil {
				return nil, err
			}
			args := []interface{}{strings.TrimSpace(params["lifecycle"])}
			return crmQueryRows(crmFactsCTE(schema, crmLifecycleThresholds(schema), false)+` SELECT id AS customer_id, name, lifecycle, rfm, visits, ROUND(spend, 2) AS spend, days_since
				FROM cf WHERE ($1 = '' OR lifecycle = $1) ORDER BY spend DESC LIMIT 5000`, args...)
		}})
	reg(ReportDefinition{ID: "crm-coupon-use", Label: "Coupon Use", StoreKey: "-",
		Description: "Vouchers the CRM issued (campaigns, journeys) and whether they were used. " + crmReportNote("when the voucher was issued"),
		Params:      crmDateParams,
		Columns: []ReportColumn{{Key: "issued", Label: "Issued"}, {Key: "code", Label: "Code"}, {Key: "customer", Label: "Customer"}, {Key: "offer", Label: "Offer"}, {Key: "from", Label: "From"},
			{Key: "expiry", Label: "Expires"}, {Key: "used", Label: "Used", Total: true}, {Key: "status", Label: "Status"}},
		Run: func(tenantID string, params map[string]string) ([]map[string]interface{}, error) {
			schema, err := crmSchema(tenantID)
			if err != nil {
				return nil, err
			}
			where, args := crmRangeArgs(params, crmZoneName(schema), "v.created_at", nil)
			return crmQueryRows(fmt.Sprintf(`SELECT v.created_at AS issued, v.data->>'code' AS code, COALESCE(c.data->>'name', '') AS customer,
				COALESCE(v.data->>'discount_value', '') || CASE WHEN v.data->>'discount_type' = 'Flat' THEN ' off' ELSE '%% off' END AS offer,
				COALESCE(v.data->>'campaign_id', '') AS from, COALESCE(v.data->>'expiry_date', '') AS expiry, COALESCE((v.data->>'used_count')::int, 0) AS used, v.status
				FROM %s.documents v LEFT JOIN %s.documents c ON c.id = v.data->>'customer_id' AND c.doctype = 'Customer'
				WHERE v.doctype = 'Voucher' AND COALESCE(v.data->>'campaign_run', '') <> ''%s ORDER BY v.created_at DESC LIMIT 5000`, schema, schema, where), args...)
		}})
	reg(ReportDefinition{ID: "crm-service-sla", Label: "Service SLA", StoreKey: "store",
		Description: "Customer tickets against their first-response and resolution times (working hours only). " + crmReportNote("when the ticket was opened"),
		Params:      crmDateParams,
		Columns: []ReportColumn{{Key: "ticket", Label: "Ticket"}, {Key: "opened", Label: "Opened"}, {Key: "customer", Label: "Customer"}, {Key: "store", Label: "Store"}, {Key: "state", Label: "State"},
			{Key: "first_response_due", Label: "Respond by"}, {Key: "first_response_late", Label: "Response late"}, {Key: "resolve_due", Label: "Resolve by"}, {Key: "resolve_late", Label: "Resolution late"}},
		Run: func(tenantID string, params map[string]string) ([]map[string]interface{}, error) {
			tickets, err := ListCRMTickets(tenantID, map[string]string{})
			if err != nil {
				return nil, err
			}
			from, to := strings.TrimSpace(params["from_date"]), strings.TrimSpace(params["to_date"])
			out := []map[string]interface{}{}
			for _, t := range tickets {
				d := t.CreatedAt.Format("2006-01-02")
				if (from != "" && d < from) || (to != "" && d > to) {
					continue
				}
				out = append(out, map[string]interface{}{"ticket": t.ID, "opened": t.CreatedAt.Format("2006-01-02 15:04"), "customer": t.CustomerName, "store": t.Store, "state": t.State,
					"first_response_due": t.FirstResponseDue.Format("2006-01-02 15:04"), "first_response_late": map[bool]string{true: "Late", false: ""}[t.FirstResponseLate],
					"resolve_due": t.ResolveDue.Format("2006-01-02 15:04"), "resolve_late": map[bool]string{true: "Late", false: ""}[t.ResolveLate]})
			}
			return out, nil
		}})
	reg(ReportDefinition{ID: "crm-clienteling", Label: "Clienteling Tasks", StoreKey: "store",
		Description: "The store worklist: tasks by store and owner, done and open, with how many were late. " + crmReportNote("when the task was created"),
		Params:      crmDateParams,
		Columns: []ReportColumn{{Key: "store", Label: "Store"}, {Key: "owner", Label: "Owner"}, {Key: "kind", Label: "Kind"}, {Key: "open", Label: "Open", Total: true},
			{Key: "overdue", Label: "Overdue", Total: true}, {Key: "done", Label: "Done", Total: true}, {Key: "appointments", Label: "Appointments", Total: true}},
		Run: func(tenantID string, params map[string]string) ([]map[string]interface{}, error) {
			schema, err := crmSchema(tenantID)
			if err != nil {
				return nil, err
			}
			where, args := crmRangeArgs(params, crmZoneName(schema), "t.created_at", nil)
			// Appointments are counted per store once and joined: a subquery on t.store was
			// refused by Postgres ("subquery uses ungrouped column t.store") because the rows
			// are grouped on COALESCE(t.store, '') - the report failed for every client.
			return crmQueryRows(fmt.Sprintf(`SELECT COALESCE(t.store, '') AS store, COALESCE(t.owner, '(anyone)') AS owner, t.kind,
				COUNT(*) FILTER (WHERE t.status = 'Open') AS open, COUNT(*) FILTER (WHERE t.status = 'Open' AND t.due_at < LOCALTIMESTAMP) AS overdue,
				COUNT(*) FILTER (WHERE t.status = 'Done') AS done,
				COALESCE(MAX(ap.n), 0) AS appointments
				FROM %[1]s.crm_tasks t
				LEFT JOIN (SELECT a.store, COUNT(*) AS n FROM %[1]s.crm_appointments a GROUP BY a.store) ap ON ap.store = t.store
				WHERE TRUE%[2]s GROUP BY 1, 2, 3 ORDER BY 1, 2, 3`, schema, where), args...)
		}})
	reg(ReportDefinition{ID: "crm-feedback", Label: "Feedback and NPS", StoreKey: "store",
		Description: "Feedback by store: answers, promoters, detractors and NPS (promoters minus detractors, % of answers). " + crmReportNote("when the feedback was asked"),
		Params:      crmDateParams,
		Columns: []ReportColumn{{Key: "store", Label: "Store"}, {Key: "asked", Label: "Asked", Total: true}, {Key: "answers", Label: "Answers", Total: true}, {Key: "promoters", Label: "Promoters", Total: true},
			{Key: "detractors", Label: "Detractors", Total: true}, {Key: "nps", Label: "NPS"}, {Key: "tickets", Label: "Tickets opened", Total: true}},
		Run: func(tenantID string, params map[string]string) ([]map[string]interface{}, error) {
			schema, err := crmSchema(tenantID)
			if err != nil {
				return nil, err
			}
			where, args := crmRangeArgs(params, crmZoneName(schema), "f.created_at", nil)
			return crmQueryRows(fmt.Sprintf(`SELECT COALESCE(f.store, '') AS store, COUNT(*) AS asked, COUNT(f.score) AS answers,
				COUNT(*) FILTER (WHERE f.score >= 9) AS promoters, COUNT(*) FILTER (WHERE f.score <= 6) AS detractors,
				CASE WHEN COUNT(f.score) > 0 THEN ROUND(100.0 * (COUNT(*) FILTER (WHERE f.score >= 9) - COUNT(*) FILTER (WHERE f.score <= 6)) / COUNT(f.score), 1) END AS nps,
				COUNT(f.ticket_id) AS tickets
				FROM %s.crm_feedback f JOIN %s.crm_survey_versions v ON v.survey_id = f.survey_id AND v.version = f.survey_version AND v.scale = 'nps'
				WHERE TRUE%s GROUP BY 1 ORDER BY 1`, schema, schema, where), args...)
		}})
	reg(ReportDefinition{ID: "crm-message-cost", Label: "Message Cost", StoreKey: "-",
		Description: "What CRM messages cost by month and channel: the estimate reserved and the provider's actual charge where it reported one. " + crmReportNote("when the message was sent"),
		Params:      crmDateParams,
		Columns: []ReportColumn{{Key: "month", Label: "Month"}, {Key: "channel", Label: "Channel"}, {Key: "messages", Label: "Messages", Total: true}, {Key: "sms_parts", Label: "SMS parts", Total: true},
			{Key: "estimate", Label: "Estimated", Sensitive: true, Total: true}, {Key: "actual", Label: "Charged (reported)", Sensitive: true, Total: true}},
		Run: func(tenantID string, params map[string]string) ([]map[string]interface{}, error) {
			schema, err := crmSchema(tenantID)
			if err != nil {
				return nil, err
			}
			zone := crmZoneName(schema)
			where, args := crmRangeArgs(params, zone, "s.sent_at", nil)
			return crmQueryRows(fmt.Sprintf(`SELECT to_char(%s, 'YYYY-MM') AS month, s.channel, COUNT(*) AS messages, COALESCE(SUM(s.parts) FILTER (WHERE s.channel = 'SMS'), 0) AS sms_parts,
				ROUND(COALESCE(SUM(s.cost_estimate), 0), 2) AS estimate, ROUND(SUM(s.cost_actual), 2) AS actual
				FROM %s.crm_sends s WHERE s.status IN `+crmCountedStatusesSQL+` AND s.sent_at IS NOT NULL%s GROUP BY 1, 2 ORDER BY 1 DESC, 2`, crmDocLocalSQL("s.sent_at", zone), schema, where), args...)
		}})
	reg(ReportDefinition{ID: "crm-pipeline", Label: "Sales Pipeline", StoreKey: "-",
		Description: "Open business deals by stage: value, chance and weighted value; won and lost in the period. " + crmReportNote("the expected close date"),
		Params:      []ReportParam{{Key: "status", Label: "Deals", Type: "select", Options: "Open,Won,Lost,All"}},
		Columns: []ReportColumn{{Key: "opportunity", Label: "Deal"}, {Key: "customer", Label: "Customer"}, {Key: "owner", Label: "Owner"}, {Key: "stage", Label: "Stage"},
			{Key: "value", Label: "Value", Sensitive: true, Total: true}, {Key: "probability", Label: "Chance %"}, {Key: "weighted", Label: "Weighted", Sensitive: true, Total: true},
			{Key: "expected_close", Label: "Expected close"}, {Key: "status", Label: "Status"}, {Key: "sales_order", Label: "Sales order"}},
		Run: func(tenantID string, params map[string]string) ([]map[string]interface{}, error) {
			st := strings.ToLower(strings.TrimSpace(params["status"]))
			if st == "" {
				st = "open"
			}
			if st != "all" && st != "open" {
				st = crmTitle(st)
			}
			opps, err := ListCRMOpportunities(tenantID, map[string]string{"status": st})
			if err != nil {
				return nil, err
			}
			out := []map[string]interface{}{}
			for _, o := range opps {
				out = append(out, map[string]interface{}{"opportunity": o.Title, "customer": o.CustomerName, "owner": o.Owner, "stage": o.Stage, "value": o.Value,
					"probability": o.Probability, "weighted": o.Weighted, "expected_close": o.ExpectedClose, "status": o.Status, "sales_order": o.SalesOrderID})
			}
			return out, nil
		}})
}

// CRMDashboard is the CRM screen's front page.
type CRMDashboard struct {
	Customers        int                 `json:"customers"`
	Lifecycle        []CRMLifecycleCount `json:"lifecycle"`
	Reachable        map[string]int      `json:"reachable"`
	MessagesMonth    map[string]int      `json:"messages_month"`
	CampaignsLive    int                 `json:"campaigns_live"`
	JourneysOn       int                 `json:"journeys_on"`
	JourneyRuns      int                 `json:"journey_runs_live"`
	NPS              *float64            `json:"nps"`
	NPSAnswers       int                 `json:"nps_answers"`
	OpenTickets      int                 `json:"open_tickets"`
	OpenTasks        int                 `json:"open_tasks"`
	UnreadInbox      int                 `json:"unread_inbox"`
	Unknown          int                 `json:"unknown_outcomes"`
	PipelineWeighted float64             `json:"pipeline_weighted"`
	SendersReady     map[string]bool     `json:"senders_ready"`
	TimeZone         string              `json:"time_zone"`
	At               time.Time           `json:"at"`
}

// GetCRMDashboard gathers the CRM's headline numbers.
func GetCRMDashboard(tenantID string) (*CRMDashboard, error) {
	schema, err := crmSchema(tenantID)
	if err != nil {
		return nil, err
	}
	d := &CRMDashboard{Reachable: map[string]int{}, MessagesMonth: map[string]int{}, SendersReady: map[string]bool{}, TimeZone: crmZoneName(schema), At: time.Now()}
	if d.Lifecycle, _, err = GetCRMLifecycleCounts(tenantID); err != nil {
		return nil, err
	}
	for _, l := range d.Lifecycle {
		d.Customers += l.Count
	}
	var sms, wa, em int
	_ = db.DB.QueryRow(crmFactsCTE(schema, crmLifecycleThresholds(schema), false)+` SELECT
		COUNT(*) FILTER (WHERE LOWER(COALESCE(data->>'consent_sms', '')) IN ('true', '1', 'yes', 'on')),
		COUNT(*) FILTER (WHERE LOWER(COALESCE(data->>'consent_whatsapp', '')) IN ('true', '1', 'yes', 'on')),
		COUNT(*) FILTER (WHERE LOWER(COALESCE(data->>'consent_email', '')) IN ('true', '1', 'yes', 'on')) FROM cf`).Scan(&sms, &wa, &em)
	d.Reachable[CRMChannelSMS], d.Reachable[CRMChannelWhatsApp], d.Reachable[CRMChannelEmail] = sms, wa, em
	rows, err := db.DB.Query(fmt.Sprintf(`SELECT status, COUNT(*) FROM %s.crm_sends WHERE created_at >= date_trunc('month', LOCALTIMESTAMP) GROUP BY status`, schema))
	if err == nil {
		for rows.Next() {
			var s string
			var n int
			if rows.Scan(&s, &n) == nil {
				d.MessagesMonth[s] = n
			}
		}
		rows.Close()
	}
	_ = db.DB.QueryRow(fmt.Sprintf(`SELECT COUNT(*) FROM %s.crm_campaign_runs WHERE status IN ('Running', 'Scheduled') AND test = FALSE`, schema)).Scan(&d.CampaignsLive)
	_ = db.DB.QueryRow(fmt.Sprintf(`SELECT COUNT(*) FROM %s.crm_journeys WHERE enabled`, schema)).Scan(&d.JourneysOn)
	_ = db.DB.QueryRow(fmt.Sprintf(`SELECT COUNT(*) FROM %s.crm_journey_runs WHERE state IN ('Active', 'Waiting')`, schema)).Scan(&d.JourneyRuns)
	var answers, prom, det int
	_ = db.DB.QueryRow(fmt.Sprintf(`SELECT COUNT(f.score), COUNT(*) FILTER (WHERE f.score >= 9), COUNT(*) FILTER (WHERE f.score <= 6) FROM %s.crm_feedback f
		JOIN %s.crm_survey_versions v ON v.survey_id = f.survey_id AND v.version = f.survey_version AND v.scale = 'nps' WHERE f.responded_at > LOCALTIMESTAMP - interval '90 days'`, schema, schema)).Scan(&answers, &prom, &det)
	if answers > 0 {
		n := round2(100*float64(prom)/float64(answers) - 100*float64(det)/float64(answers))
		d.NPS, d.NPSAnswers = &n, answers
	}
	_ = db.DB.QueryRow(fmt.Sprintf(`SELECT COUNT(*) FROM %s.documents WHERE doctype = 'ServiceTicket' AND status NOT IN ('Resolved', 'Closed', 'Cancelled') AND deleted_at IS NULL`, schema)).Scan(&d.OpenTickets)
	_ = db.DB.QueryRow(fmt.Sprintf(`SELECT COUNT(*) FROM %s.crm_tasks WHERE status = 'Open'`, schema)).Scan(&d.OpenTasks)
	_ = db.DB.QueryRow(fmt.Sprintf(`SELECT COALESCE(SUM(unread), 0) FROM %s.crm_conversations WHERE status = 'Open'`, schema)).Scan(&d.UnreadInbox)
	_ = db.DB.QueryRow(fmt.Sprintf(`SELECT COUNT(*) FROM %s.crm_sends WHERE status = 'Unknown'`, schema)).Scan(&d.Unknown)
	_ = db.DB.QueryRow(fmt.Sprintf(`SELECT COALESCE(SUM(value * probability / 100.0), 0) FROM %s.crm_opportunities WHERE status = 'Open'`, schema)).Scan(&d.PipelineWeighted)
	d.PipelineWeighted = round2(d.PipelineWeighted)
	for _, ch := range crmChannels {
		s, _ := crmActiveSender(schema, ch)
		d.SendersReady[ch] = s != nil && len(s.Problems) == 0
	}
	return d, nil
}
