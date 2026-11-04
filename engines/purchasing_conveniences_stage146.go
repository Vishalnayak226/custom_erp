package engines

import (
	"encoding/json"
	"fmt"
	"log"
	"strings"

	"custom_erp/db"
)

// Stage 146.4.6: small purchasing conveniences. Every one is off (setting 0 /
// false, or an empty field) until a client turns it on.
//
//	(a) a PO waiting for approval is reminded, then escalated;
//	(b) a vendor invoice dated before its PO warns and is listed;
//	(d) the buyer is told of rejected qty on a posted GRN;
//	(e) a return with no store takes its sales channel's default store;
//	(f) a long import / export tells the person who started it.
//
// (c) needs no engine code: a PO is read by the generic sticker source.
const (
	SettingPOApprovalReminderHours   = "procurement.po_approval_reminder_hours"
	SettingPOApprovalEscalationHours = "procurement.po_approval_escalation_hours"
	SettingNotifyBuyerOnRejection    = "procurement.notify_buyer_on_rejection"
	SettingJobDoneMinRows            = "notifications.job_done_min_rows"

	// PURCHA-4968: the warning logged when a vendor invoice pre-dates its PO.
	VendorInvoiceBeforePOCode = "PURCHA-4968"
)

func init() {
	hours := func(key, label, desc string) {
		RegisterSetting(SettingDefinition{Key: key, Module: "Procurement & GRN", Label: label, Type: SettingTypeInt, Default: "0", Unit: "hours",
			Min: settingBound(0), Max: settingBound(8760), Description: desc + " 0 = off."})
	}
	hours(SettingPOApprovalReminderHours, "Remind approvers of a waiting PO after",
		"Hours a purchase order waits for approval before the \"PO Approval Reminder\" notification goes out, once.")
	hours(SettingPOApprovalEscalationHours, "Escalate a waiting PO after",
		"Hours a purchase order waits for approval before the \"PO Approval Escalation\" notification goes out, once.")
	RegisterSetting(SettingDefinition{Key: SettingNotifyBuyerOnRejection, Module: "Procurement & GRN", Label: "Tell the buyer about rejected quantity",
		Type: SettingTypeBool, Default: "false",
		Description: "On: when a goods receipt is posted with rejected quantity, the \"GRN Rejected Qty\" notification goes out for the buyer who raised its PO."})
	RegisterSetting(SettingDefinition{Key: SettingJobDoneMinRows, Module: "Notifications", Label: "Email when a big import or export finishes, from rows",
		Type: SettingTypeInt, Default: "0", Min: settingBound(0), Max: settingBound(10000000),
		Description: "An import or export of at least this many rows tells the person who started it (a notification, and an email if they have an address). 0 = off."})

	RegisterReport(ReportDefinition{ID: "vendor-invoices-before-po", Label: "Vendor invoices dated before their PO", Category: "Procurement",
		Columns: []ReportColumn{{Key: "invoice", Label: "Invoice"}, {Key: "vendor", Label: "Vendor"}, {Key: "po", Label: "PO"}, {Key: "invoice_date", Label: "Invoice date"}, {Key: "po_date", Label: "PO date"}},
		Run:     vendorInvoicesBeforePOReport})
}

// RunPOApprovalReminders (a) sends the reminder, then the escalation, for each
// PO still Pending Approval. Each goes once per wait: the stamp holds the
// updated_at it was sent for, so a PO edited and resubmitted waits afresh.
func RunPOApprovalReminders(tenantID, schema string) error {
	if err := RunPORuleEscalations(tenantID, schema); err != nil { // Stage 148.39
		LogSystemError(tenantID, "", "WARN", "PORuleEscalation", err.Error(), "")
	}
	for _, step := range []struct{ setting, event, stamp string }{
		{SettingPOApprovalReminderHours, "PO Approval Reminder", "approval_reminded_at"},
		{SettingPOApprovalEscalationHours, "PO Approval Escalation", "approval_escalated_at"},
	} {
		n := GetSettingInt(tenantID, step.setting)
		if n <= 0 {
			continue
		}
		ids, err := dayLimitRows(tenantID, fmt.Sprintf(`SELECT id, COALESCE(data->>'vendor', ''), COALESCE(data->>'total_amount', '0'), EXTRACT(EPOCH FROM (NOW() - updated_at))::bigint / 3600
			FROM {s}.documents WHERE doctype = 'PurchaseOrder' AND deleted_at IS NULL AND status = 'Pending Approval'
			  AND updated_at <= NOW() - make_interval(hours => $1) AND COALESCE(data->>'%s', '') <> updated_at::text
			ORDER BY updated_at LIMIT 500`, step.stamp), []interface{}{n},
			func(scan func(...interface{}) error) (map[string]interface{}, error) {
				var id, vendor, amount string
				var waited int64
				if err := scan(&id, &vendor, &amount, &waited); err != nil {
					return nil, err
				}
				return map[string]interface{}{"id": id, "vendor": vendor, "amount": amount, "hours": waited}, nil
			})
		if err != nil {
			return err
		}
		for _, r := range ids {
			id := r["id"].(string)
			// Stamped without touching updated_at, so the wait keeps counting.
			if _, err := db.DB.Exec(fmt.Sprintf(`UPDATE %s.documents SET data = data || jsonb_build_object($2::text, updated_at::text) WHERE id = $1 AND doctype = 'PurchaseOrder'`, schema), id, step.stamp); err != nil {
				return err
			}
			DispatchNotification(tenantID, step.event, id, map[string]string{"po_id": id, "vendor": r["vendor"].(string), "amount": r["amount"].(string), "hours_waiting": fmt.Sprint(r["hours"])})
		}
	}
	return nil
}

// VendorInvoiceBeforePO (b) returns the warning for an invoice dated before
// the day its PO was raised, or "" when there is nothing to warn about.
func VendorInvoiceBeforePO(tenantID string, payload map[string]interface{}) string {
	poID, invDate := strings.TrimSpace(str(payload, "po_id")), strings.TrimSpace(str(payload, "invoice_date"))
	if poID == "" || len(invDate) < 10 {
		return ""
	}
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return ""
	}
	var poDate string
	if err := db.DB.QueryRow(fmt.Sprintf(`SELECT public.erp_local_date(created_at)::text FROM %s.documents
		WHERE doctype = 'PurchaseOrder' AND deleted_at IS NULL AND (id = $1 OR data->>'code' = $1) LIMIT 1`, schema), poID).Scan(&poDate); err != nil {
		return ""
	}
	if invDate[:10] < poDate {
		return fmt.Sprintf("this invoice is dated %s, before its PO %s was raised on %s", invDate[:10], poID, poDate)
	}
	return ""
}

func vendorInvoicesBeforePOReport(tenantID string, _ map[string]string) ([]map[string]interface{}, error) {
	return dayLimitRows(tenantID, `SELECT COALESCE(v.data->>'invoice_number', v.id), COALESCE(v.data->>'vendor_id', ''), p.id,
		left(v.data->>'invoice_date', 10), public.erp_local_date(p.created_at)::text
		FROM {s}.documents v JOIN {s}.documents p ON p.doctype = 'PurchaseOrder' AND p.deleted_at IS NULL AND (p.id = v.data->>'po_id' OR p.data->>'code' = v.data->>'po_id')
		WHERE v.doctype = 'VendorInvoice' AND v.deleted_at IS NULL AND v.status <> 'Cancelled'
		  AND length(COALESCE(v.data->>'invoice_date', '')) >= 10 AND left(v.data->>'invoice_date', 10) < public.erp_local_date(p.created_at)::text
		ORDER BY v.created_at DESC LIMIT 20001`, nil,
		func(scan func(...interface{}) error) (map[string]interface{}, error) {
			var inv, vendor, po, invDate, poDate string
			if err := scan(&inv, &vendor, &po, &invDate, &poDate); err != nil {
				return nil, err
			}
			return map[string]interface{}{"invoice": inv, "vendor": vendor, "po": po, "invoice_date": invDate, "po_date": poDate}, nil
		})
}

// NotifyBuyerOfGRNRejections (d) sends "GRN Rejected Qty" when a posted
// receipt has rejected quantity; it returns that quantity (0 = nothing sent).
func NotifyBuyerOfGRNRejections(tenantID, grnID string) float64 {
	if !GetSettingBool(tenantID, SettingNotifyBuyerOnRejection) {
		return 0
	}
	data, _, err := fetchDocData(tenantID, "GRN", grnID)
	if err != nil {
		return 0
	}
	var lines []map[string]interface{}
	switch v := data["received_items"].(type) {
	case string:
		_ = json.Unmarshal([]byte(v), &lines)
	default:
		lines = genericLineList(v)
	}
	rejected := 0.0
	for _, l := range lines {
		rejected += numFromInterface(l["rejected_qty"])
	}
	if rejected <= 0 {
		return 0
	}
	vars := map[string]string{"grn_id": grnID, "rejected_qty": fmt.Sprint(rejected), "vendor": str(data, "vendor")}
	if poIDs := GRNPOIDs(data); len(poIDs) > 0 {
		vars["po_id"] = poIDs[0]
		if schema, err := db.GetTenantSchema(tenantID); err == nil {
			var buyer string
			_ = db.DB.QueryRow(fmt.Sprintf(`SELECT COALESCE(u.username, d.created_by, '') FROM %s.documents d LEFT JOIN %s.users u ON u.id = d.created_by
				WHERE d.doctype = 'PurchaseOrder' AND d.deleted_at IS NULL AND (d.id = $1 OR d.data->>'code' = $1) LIMIT 1`, schema, schema), poIDs[0]).Scan(&buyer)
			vars["buyer"] = buyer
		}
	}
	DispatchNotification(tenantID, "GRN Rejected Qty", grnID, vars)
	return rejected
}

// channelDefaultReturnStore (e) is the default return store of the sales
// channel an order came in on, or "" (no channel, no default: as today).
func channelDefaultReturnStore(schema, orderID string) string {
	if strings.TrimSpace(orderID) == "" {
		return ""
	}
	var store string
	_ = db.DB.QueryRow(fmt.Sprintf(`SELECT COALESCE(c.data->>'default_return_location', '') FROM %s.documents o
		JOIN %s.documents c ON c.doctype = 'Channel' AND c.deleted_at IS NULL
		 AND (LOWER(c.data->>'code') = LOWER(o.data->>'channel') OR LOWER(c.data->>'name') = LOWER(o.data->>'channel'))
		WHERE (o.id = $1 OR o.data->>'code' = $1) AND o.deleted_at IS NULL AND COALESCE(o.data->>'channel', '') <> ''
		ORDER BY CASE WHEN COALESCE(c.data->>'default_return_location', '') <> '' THEN 0 ELSE 1 END LIMIT 1`, schema, schema), orderID).Scan(&store)
	return strings.TrimSpace(store)
}

// NotifyLongJobDone (f) tells whoever started a big import / export that it
// has finished: a notification, and an email message queued in the outbox
// (the outbox sender delivers it).
func NotifyLongJobDone(tenantID, userID, kind, name string, rows int) {
	min := GetSettingInt(tenantID, SettingJobDoneMinRows)
	if min <= 0 || rows < min {
		return
	}
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return
	}
	var username, email string
	_ = db.DB.QueryRow(fmt.Sprintf(`SELECT username, COALESCE(email, '') FROM %s.users WHERE id = $1 OR username = $1 LIMIT 1`, schema), userID).Scan(&username, &email)
	DispatchNotification(tenantID, "Long Job Finished", name, map[string]string{"kind": kind, "rows": fmt.Sprint(rows), "user": username})
	email = strings.TrimSpace(email)
	if email == "" {
		return
	}
	tx, err := db.DB.Begin()
	if err != nil {
		return
	}
	defer tx.Rollback()
	// email.send is the outbox's real sender event (146.5.6, outbox_email.go).
	if err := PublishEvent(tx, schema, "email.send", map[string]interface{}{
		"to":      email,
		"subject": fmt.Sprintf("%s finished: %s", kind, name),
		"body":    fmt.Sprintf("Your %s %q has finished (%d rows).", strings.ToLower(kind), name, rows),
	}); err != nil {
		log.Printf("[JOBDONE] could not queue the email: %v", err)
		return
	}
	_ = tx.Commit()
}
