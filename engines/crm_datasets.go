package engines

// Stage 101.7.1 on Stage 100: the CRM's data as dashboard datasets, so a
// custom dashboard (Reports > Custom dashboards) can chart messages, consent,
// feedback and the pipeline next to sales. The CRM's own Overview tab and its
// reports read the same tables.

func init() {
	cust := ` LEFT JOIN %[1]s.documents cu ON cu.doctype = 'Customer' AND cu.id = %s`
	RegisterDashboardDataset(DashboardDataset{
		Key: "crm_messages", Label: "CRM messages", About: "Every CRM message and what happened to it: sent, delivered, or why not.",
		Doctype: "Customer", DocField: "customer_id", List: "crm", DateField: "date",
		from:  `%[1]s.crm_sends s` + sprintfKeep(cust, "s.customer_id"),
		where: `TRUE`,
		Fields: []DatasetField{
			dsF("date", "Date", "date", `s.created_at`),
			dsF("channel", "Channel", "category", `s.channel`),
			dsF("purpose", "Purpose", "category", `s.purpose`),
			dsF("status", "Status", "category", `s.status`),
			dsF("reason", "Reason not sent", "category", `COALESCE(s.reason_code, '(sent)')`),
			dsF("source", "Sent by", "category", `s.run_kind`),
			dsF("customer_id", "Customer", "category", `s.customer_id`),
			dsF("parts", "SMS parts", "number", `COALESCE(s.parts, 0)`),
			dsMoney("cost", "Cost", `COALESCE(s.cost_actual, s.cost_estimate, 0)`),
		},
	})
	RegisterDashboardDataset(DashboardDataset{
		Key: "crm_consent", Label: "CRM consent", About: "Every consent given, refused or withdrawn, by channel, purpose and source.",
		Doctype: "Customer", DocField: "customer_id", List: "crm", DateField: "date",
		from:  `%[1]s.crm_consent_ledger l`,
		where: `TRUE`,
		Fields: []DatasetField{
			dsF("date", "Date", "date", `l.created_at`),
			dsF("channel", "Channel", "category", `l.channel`),
			dsF("purpose", "Purpose", "category", `l.purpose`),
			dsF("action", "Action", "category", `l.action`),
			dsF("source", "Source", "category", `l.source`),
			dsF("customer_id", "Customer", "category", `l.customer_id`),
		},
	})
	RegisterDashboardDataset(DashboardDataset{
		Key: "crm_feedback", Label: "CRM feedback", About: "Feedback after bills: score, promoter or detractor, by store.",
		Doctype: "Customer", DocField: "customer_id", List: "crm", DateField: "date",
		from:   `%[1]s.crm_feedback f` + dsLocationJoin("lo", `f.store`),
		where:  `f.score IS NOT NULL`,
		stores: []string{`f.store`},
		Fields: []DatasetField{
			dsF("date", "Date", "date", `f.responded_at`),
			dsF("store", "Store", "category", dsStoreName("lo", `f.store`)),
			dsF("band", "Promoter / detractor", "category", `CASE WHEN f.score >= 9 THEN 'Promoter' WHEN f.score >= 7 THEN 'Passive' ELSE 'Detractor' END`),
			dsF("customer_id", "Customer", "category", `f.customer_id`),
			dsF("score", "Score", "number", `f.score`),
		},
	})
	RegisterDashboardDataset(DashboardDataset{
		Key: "crm_pipeline", Label: "CRM pipeline", About: "Business deals: stage, owner, value and weighted value.",
		Doctype: "SalesQuotation", List: "crm", DateField: "date",
		from:  `%[1]s.crm_opportunities o LEFT JOIN %[1]s.crm_pipeline_stages st ON st.id = o.stage_id`,
		where: `TRUE`,
		Fields: []DatasetField{
			dsF("date", "Expected close", "date", `COALESCE(o.expected_close, o.created_at::date)`),
			dsF("stage", "Stage", "category", `COALESCE(st.name, o.stage_id)`),
			dsF("status", "Status", "category", `o.status`),
			dsF("owner", "Owner", "category", `COALESCE(NULLIF(o.owner, ''), '(nobody)')`),
			dsMoney("value", "Value", `o.value`),
			dsMoney("weighted", "Weighted value", `o.value * o.probability / 100.0`),
		},
	})
}

// sprintfKeep fills one %s in a dataset fragment while leaving its %[1]s
// (the schema, filled later by the dataset layer) in place.
func sprintfKeep(fragment, value string) string {
	out := ""
	for i := 0; i < len(fragment); i++ {
		if fragment[i] == '%' && i+1 < len(fragment) && fragment[i+1] == 's' {
			out += value
			i++
			continue
		}
		out += string(fragment[i])
	}
	return out
}
