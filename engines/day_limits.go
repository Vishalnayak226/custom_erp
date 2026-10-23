package engines

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"strings"
	"time"

	"custom_erp/db"
)

// Stage 130.10 (78.3 a-h, user decision 2026-10-22: all eight, each 0 = off).
// The day limits established ERPs put around purchasing and transfers:
//
//	(a) a PO's Expected delivery date, and a reminder N days before it
//	    (the "PO Delivery Reminder" notification event, to whatever channel
//	    the client's Notification Templates send it);
//	(b) a receipt more than N days after - or M days before - that date is
//	    marked Late / Early on the goods receipt and listed in a report;
//	(c) a vendor invoice date (goods receipt, vendor bill) at most N days back
//	    and M ahead, and a PO's expected date at most N days back - an
//	    administrator may go past both;
//	(d) goods receipts not billed N days after posting (report);
//	(e) a purchase requisition not turned into a PO within N days expires;
//	(f) a vendor quote with no "valid until" is valid N days;
//	(g) a transfer dispatched N days ago and not received is flagged
//	    ("Transfer In Transit Overdue") and listed;
//	(h) a vendor invoice of a financial year entered after 30 November of
//	    the next is marked: its input tax credit is time-barred (CGST Act
//	    Section 16(4)) - a fixed date, switched on or off.
//
// Every limit ships at 0 / off, so nothing changes until a client sets one.

const (
	SettingVendorReminderDays    = "procurement.vendor_reminder_days"
	SettingGRNLateAfterDays      = "procurement.grn_late_after_days"
	SettingGRNEarlyBeforeDays    = "procurement.grn_early_before_days"
	SettingBackdateDays          = "procurement.backdate_days"
	SettingForwardDateDays       = "procurement.forward_date_days"
	SettingGRNUnbilledDays       = "procurement.grn_unbilled_days"
	SettingRequisitionExpiryDays = "procurement.requisition_expiry_days"
	SettingQuoteValidityDays     = "procurement.quote_validity_days"
	SettingTransitAlertDays      = "transfers.in_transit_alert_days"
	SettingITCDeadlineWarning    = "procurement.itc_deadline_warning"
)

func init() {
	days := func(key, module, label, desc string) {
		RegisterSetting(SettingDefinition{Key: key, Module: module, Label: label, Type: SettingTypeInt, Default: "0", Unit: "days",
			Min: settingBound(0), Max: settingBound(3650), Description: desc + " 0 = off."})
	}
	days(SettingVendorReminderDays, "Procurement & GRN", "Remind the vendor before the expected delivery",
		"Days before a PO's Expected delivery date that the \"PO Delivery Reminder\" notification goes out (to the channel its Notification Template names), once per PO.")
	days(SettingGRNLateAfterDays, "Procurement & GRN", "Receipt is late after",
		"Days after the PO's Expected delivery date beyond which a goods receipt is marked Late (and listed in Late and early receipts).")
	days(SettingGRNEarlyBeforeDays, "Procurement & GRN", "Receipt is early before",
		"Days before the PO's Expected delivery date beyond which a goods receipt is marked Early.")
	days(SettingBackdateDays, "Procurement & GRN", "Dates may go back at most",
		"How far back a vendor invoice date (goods receipt, vendor bill) or a PO's Expected delivery date may be. An administrator may go past it.")
	days(SettingForwardDateDays, "Procurement & GRN", "Dates may go ahead at most",
		"How far ahead a vendor invoice date may be. An administrator may go past it.")
	days(SettingGRNUnbilledDays, "Procurement & GRN", "Goods receipt not billed after",
		"Days after posting that a goods receipt with no vendor bill shows in Goods receipts not billed.")
	days(SettingRequisitionExpiryDays, "Procurement & GRN", "Purchase requisition expires after",
		"Days after which a purchase requisition not turned into a PO is closed as Expired.")
	days(SettingQuoteValidityDays, "Procurement & GRN", "A vendor quote is valid for",
		"Days a vendor quote stays valid when its Valid until is left blank.")
	days(SettingTransitAlertDays, "Stock Transfer", "Transfer in transit too long after",
		"Days after dispatch that a transfer not yet received is flagged (the \"Transfer In Transit Overdue\" notification) and listed in Transfers in transit too long.")
	RegisterSetting(SettingDefinition{Key: SettingITCDeadlineWarning, Module: "GST & Compliance", Label: "Mark vendor invoices past the input credit deadline",
		Type: SettingTypeBool, Default: "false",
		Description: "On: a vendor invoice of one financial year entered after 30 November of the next is marked - its input tax credit cannot be claimed (CGST Act Section 16(4)) - and listed in Input credit past its deadline."})

	RegisterReport(ReportDefinition{ID: "grn-receipt-timing", Label: "Late and early receipts", Category: "Procurement",
		Columns: []ReportColumn{{Key: "grn", Label: "Goods receipt"}, {Key: "po", Label: "PO"}, {Key: "vendor", Label: "Vendor"}, {Key: "expected", Label: "Expected"}, {Key: "received", Label: "Received"}, {Key: "timing", Label: "Timing"}},
		Run:     grnReceiptTimingReport})
	RegisterReport(ReportDefinition{ID: "grn-not-billed", Label: "Goods receipts not billed", Category: "Procurement",
		Columns: []ReportColumn{{Key: "grn", Label: "Goods receipt"}, {Key: "vendor", Label: "Vendor"}, {Key: "posted", Label: "Posted"}, {Key: "days", Label: "Days"}, {Key: "vendor_invoice_no", Label: "Vendor invoice no."}},
		Run:     grnNotBilledReport})
	RegisterReport(ReportDefinition{ID: "transfers-in-transit-long", Label: "Transfers in transit too long", Category: "Inventory",
		Columns: []ReportColumn{{Key: "transfer", Label: "Transfer"}, {Key: "from", Label: "From"}, {Key: "to", Label: "To"}, {Key: "dispatched", Label: "Dispatched"}, {Key: "days", Label: "Days in transit"}},
		Run:     transfersInTransitReport})
	RegisterReport(ReportDefinition{ID: "itc-past-deadline", Label: "Input credit past its deadline", Category: "Finance",
		Columns: []ReportColumn{{Key: "document", Label: "Document"}, {Key: "type", Label: "Type"}, {Key: "vendor", Label: "Vendor"}, {Key: "invoice_date", Label: "Invoice date"}, {Key: "deadline", Label: "Deadline"}},
		Run:     itcPastDeadlineReport})
}

// dayLimitDate reads a YYYY-MM-DD (or longer ISO) value.
func dayLimitDate(v string) (time.Time, bool) {
	v = strings.TrimSpace(v)
	if len(v) >= 10 {
		v = v[:10]
	}
	t, err := time.ParseInLocation("2006-01-02", v, time.Local)
	return t, err == nil
}

func todayDate() time.Time {
	y, m, d := time.Now().Date()
	return time.Date(y, m, d, 0, 0, 0, 0, time.Local)
}

// dateLimitFields are the user-typed dates (c) holds to the window.
var dateLimitFields = map[string]string{"GRN": "vendor_invoice_date", "VendorInvoice": "invoice_date", "PurchaseOrder": "expected_delivery_date"}

// ValidateDateLimits (c) refuses a date outside the client's window, unless
// the person saving is an administrator.
func ValidateDateLimits(tenantID, role, doctype string, payload map[string]interface{}) error {
	field := dateLimitFields[doctype]
	if field == "" || HasFullAccess(role) {
		return nil
	}
	back, ahead := GetSettingInt(tenantID, SettingBackdateDays), GetSettingInt(tenantID, SettingForwardDateDays)
	if back <= 0 && ahead <= 0 {
		return nil
	}
	d, ok := dayLimitDate(strField(payload, field))
	if !ok {
		return nil
	}
	today := todayDate()
	if back > 0 && d.Before(today.AddDate(0, 0, -back)) {
		return &ValidationError{Code: "PURCHA-1308", SubFor: field, Message: fmt.Sprintf("%s is more than %d days back - an administrator can enter it", d.Format("2006-01-02"), back)}
	}
	if ahead > 0 && doctype != "PurchaseOrder" && d.After(today.AddDate(0, 0, ahead)) {
		return &ValidationError{Code: "PURCHA-1308", SubFor: field, Message: fmt.Sprintf("%s is more than %d days ahead - an administrator can enter it", d.Format("2006-01-02"), ahead)}
	}
	return nil
}

// itcDeadline is 30 November after the end of the financial year (April to
// March) a document dated d belongs to.
func itcDeadline(d time.Time) time.Time {
	endYear := d.Year()
	if d.Month() >= time.April {
		endYear++
	}
	return time.Date(endYear, time.November, 30, 0, 0, 0, 0, time.Local)
}

// PrepareDayLimitStamps marks a document on save: (b) a goods receipt's
// timing against its PO, (h) a vendor invoice past its input credit
// deadline, (f) a vendor quote's default validity.
func PrepareDayLimitStamps(tenantID, doctype string, isCreate bool, payload map[string]interface{}) {
	today := todayDate()
	if isCreate && doctype == "GRN" {
		late, early := GetSettingInt(tenantID, SettingGRNLateAfterDays), GetSettingInt(tenantID, SettingGRNEarlyBeforeDays)
		if po := strings.TrimSpace(strField(payload, "po_id")); po != "" && (late > 0 || early > 0) {
			if poData, _, err := fetchDocData(tenantID, "PurchaseOrder", po); err == nil && poData != nil {
				if exp, ok := dayLimitDate(strField(poData, "expected_delivery_date")); ok {
					payload["expected_delivery_date"] = exp.Format("2006-01-02")
					diff := int(today.Sub(exp).Hours() / 24)
					switch {
					case late > 0 && diff > late:
						payload["receipt_timing"] = fmt.Sprintf("Late by %d days", diff)
					case early > 0 && -diff > early:
						payload["receipt_timing"] = fmt.Sprintf("Early by %d days", -diff)
					}
				}
			}
		}
	}
	if field := map[string]string{"GRN": "vendor_invoice_date", "VendorInvoice": "invoice_date"}[doctype]; field != "" && GetSettingBool(tenantID, SettingITCDeadlineWarning) {
		if d, ok := dayLimitDate(strField(payload, field)); ok {
			if dl := itcDeadline(d); today.After(dl) {
				payload["itc_time_barred"] = fmt.Sprintf("Input tax credit time-barred: invoice dated %s, deadline %s (Section 16(4))", d.Format("2006-01-02"), dl.Format("2006-01-02"))
			} else {
				delete(payload, "itc_time_barred")
			}
		}
	}
	if isCreate && doctype == "VendorQuote" && strings.TrimSpace(strField(payload, "valid_until")) == "" {
		if n := GetSettingInt(tenantID, SettingQuoteValidityDays); n > 0 {
			payload["valid_until"] = today.AddDate(0, 0, n).Format("2006-01-02")
		}
	}
}

// runDayLimitJobs does (a), (e) and (g) for one client.
func runDayLimitJobs(schema string) error {
	tenantID, err := tenantIDForSchema(schema)
	if err != nil || tenantID == "" {
		return err
	}
	today := todayDate().Format("2006-01-02")
	stamp := func(doctype, id string, patch map[string]interface{}) error {
		b, _ := json.Marshal(patch)
		_, err := db.DB.Exec(fmt.Sprintf(`UPDATE %s.documents SET data = data || $3::jsonb, updated_at = CURRENT_TIMESTAMP WHERE doctype = $1 AND id = $2`, schema), doctype, id, string(b))
		return err
	}
	ids := func(q string, args ...interface{}) ([]string, error) {
		rows, err := db.DB.Query(fmt.Sprintf(q, schema), args...)
		if err != nil {
			return nil, err
		}
		defer rows.Close()
		var out []string
		for rows.Next() {
			var id string
			if rows.Scan(&id) == nil {
				out = append(out, id)
			}
		}
		return out, rows.Err()
	}
	// (a) vendor reminders.
	if n := GetSettingInt(tenantID, SettingVendorReminderDays); n > 0 {
		due := todayDate().AddDate(0, 0, n).Format("2006-01-02")
		list, err := ids(`SELECT id FROM %s.documents WHERE doctype = 'PurchaseOrder' AND deleted_at IS NULL
			AND COALESCE(data->>'expected_delivery_date', '') <> '' AND left(data->>'expected_delivery_date', 10) <= $1
			AND left(data->>'expected_delivery_date', 10) >= $2 AND COALESCE(data->>'vendor_reminded_on', '') = ''
			AND status NOT IN ('Draft', 'Cancelled', 'Closed', 'Received', 'Short Closed', 'Rejected')`, due, today)
		if err != nil {
			return err
		}
		for _, id := range list {
			if err := stamp("PurchaseOrder", id, map[string]interface{}{"vendor_reminded_on": today}); err != nil {
				return err
			}
			DispatchNotification(tenantID, "PO Delivery Reminder", id, map[string]string{"po_id": id})
		}
	}
	// (e) requisitions that never became a PO.
	if n := GetSettingInt(tenantID, SettingRequisitionExpiryDays); n > 0 {
		list, err := ids(`SELECT id FROM %s.documents WHERE doctype = 'PurchaseRequisition' AND deleted_at IS NULL
			AND status IN ('Draft', 'Pending Approval', 'Approved', 'Open') AND COALESCE(data->>'po_id', '') = ''
			AND created_at < NOW() - make_interval(days => $1)`, n)
		if err != nil {
			return err
		}
		for _, id := range list {
			if _, err := db.DB.Exec(fmt.Sprintf(`UPDATE %s.documents SET status = 'Expired', data = data || '{"status":"Expired"}'::jsonb, updated_at = CURRENT_TIMESTAMP, updated_by = 'system', edit_reason = $2
				WHERE doctype = 'PurchaseRequisition' AND id = $1`, schema), id, fmt.Sprintf("not turned into a PO within %d days", n)); err != nil {
				return err
			}
			LogAuditEvent(tenantID, "system", "PR_EXPIRED", "SUCCESS", fmt.Sprintf("Purchase requisition %s expired: not turned into a PO within %d days", id, n))
		}
	}
	// (g) transfers in transit too long.
	if n := GetSettingInt(tenantID, SettingTransitAlertDays); n > 0 {
		list, err := ids(`SELECT id FROM %s.documents WHERE doctype = 'TransferOrder' AND deleted_at IS NULL AND status = 'Dispatched'
			AND COALESCE(data->>'in_transit_overdue_on', '') = ''
			AND COALESCE(NULLIF(data->>'dispatched_at', '')::timestamptz, updated_at) < NOW() - make_interval(days => $1)`, n)
		if err != nil {
			return err
		}
		for _, id := range list {
			if err := stamp("TransferOrder", id, map[string]interface{}{"in_transit_overdue_on": today}); err != nil {
				return err
			}
			DispatchNotification(tenantID, "Transfer In Transit Overdue", id, map[string]string{"transfer_id": id})
		}
	}
	// Stage 130.16 (74.3.3): job work overdue, or near its statutory return limit.
	if err := JobWorkOverdue(tenantID, func(event, id string, vars map[string]string) { DispatchNotification(tenantID, event, id, vars) }); err != nil {
		return err
	}
	// Stage 130.16 (74.4 / 74.5.2): e-way bills expire on time, and the daily
	// reconciliation run (once a day; a mismatch is an inbox exception).
	if err := RefreshEWBQueue(tenantID); err != nil {
		LogSystemError(tenantID, "system", "WARN", "EWBQueue", err.Error(), "")
	}
	if _, err := RunReconciliationChecks(tenantID, false); err != nil {
		LogSystemError(tenantID, "system", "WARN", "Reconciliation", err.Error(), "")
	}
	return nil
}

// StartDayLimitsWorker runs the day-limit jobs on every client each interval.
func StartDayLimitsWorker(ctx context.Context, interval time.Duration) {
	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				if !IsSchedulerLeader() { // 134.7: one server runs each sweep
					continue
				}
				if db.DB == nil {
					continue
				}
				schemas, err := listTenantSchemas()
				if err != nil {
					continue
				}
				for _, schema := range schemas {
					if err := runDayLimitJobs(schema); err != nil {
						log.Printf("[DAY-LIMITS] %s: %v", schema, err)
					}
				}
			}
		}
	}()
}

// --- reports --------------------------------------------------------------

func dayLimitRows(tenantID, query string, args []interface{}, row func(scan func(...interface{}) error) (map[string]interface{}, error)) ([]map[string]interface{}, error) {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return nil, err
	}
	rows, err := db.DB.Query(strings.ReplaceAll(query, "{s}", schema), args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []map[string]interface{}{}
	for rows.Next() {
		r, err := row(rows.Scan)
		if err != nil {
			return nil, err
		}
		if r != nil {
			out = append(out, r)
		}
	}
	return out, rows.Err()
}

func grnReceiptTimingReport(tenantID string, _ map[string]string) ([]map[string]interface{}, error) {
	return dayLimitRows(tenantID, `SELECT COALESCE(data->>'code', id), COALESCE(data->>'po_id', ''), COALESCE(data->>'vendor', ''),
		COALESCE(data->>'expected_delivery_date', ''), created_at::date::text, data->>'receipt_timing'
		FROM {s}.documents WHERE doctype = 'GRN' AND deleted_at IS NULL AND COALESCE(data->>'receipt_timing', '') <> '' ORDER BY created_at DESC LIMIT 5000`, nil,
		func(scan func(...interface{}) error) (map[string]interface{}, error) {
			var grn, po, vendor, exp, rec, timing string
			if err := scan(&grn, &po, &vendor, &exp, &rec, &timing); err != nil {
				return nil, err
			}
			return map[string]interface{}{"grn": grn, "po": po, "vendor": vendor, "expected": exp, "received": rec, "timing": timing}, nil
		})
}

func grnNotBilledReport(tenantID string, _ map[string]string) ([]map[string]interface{}, error) {
	n := GetSettingInt(tenantID, SettingGRNUnbilledDays)
	if n <= 0 {
		return []map[string]interface{}{}, nil
	}
	return dayLimitRows(tenantID, `SELECT COALESCE(g.data->>'code', g.id), COALESCE(g.data->>'vendor', ''), g.created_at::date::text,
		(CURRENT_DATE - g.created_at::date), COALESCE(g.data->>'vendor_invoice_no', '')
		FROM {s}.documents g WHERE g.doctype = 'GRN' AND g.deleted_at IS NULL AND g.status IN ('Posted', 'Completed', 'Received')
		  AND COALESCE(g.data->>'vendor_bill_id', '') = '' AND g.created_at < NOW() - make_interval(days => $1)
		  AND NOT EXISTS (SELECT 1 FROM {s}.documents v WHERE v.doctype = 'VendorInvoice' AND v.deleted_at IS NULL AND v.data->>'grn_id' = g.id)
		ORDER BY g.created_at LIMIT 5000`, []interface{}{n},
		func(scan func(...interface{}) error) (map[string]interface{}, error) {
			var grn, vendor, posted, inv string
			var days int
			if err := scan(&grn, &vendor, &posted, &days, &inv); err != nil {
				return nil, err
			}
			return map[string]interface{}{"grn": grn, "vendor": vendor, "posted": posted, "days": days, "vendor_invoice_no": inv}, nil
		})
}

func transfersInTransitReport(tenantID string, _ map[string]string) ([]map[string]interface{}, error) {
	n := GetSettingInt(tenantID, SettingTransitAlertDays)
	if n <= 0 {
		return []map[string]interface{}{}, nil
	}
	return dayLimitRows(tenantID, `SELECT COALESCE(data->>'code', id), COALESCE(data->>'from_location', data->>'source_location', ''), COALESCE(data->>'to_location', data->>'destination_location', ''),
		COALESCE(NULLIF(data->>'dispatched_at', '')::timestamptz, updated_at)::date::text,
		(CURRENT_DATE - COALESCE(NULLIF(data->>'dispatched_at', '')::timestamptz, updated_at)::date)
		FROM {s}.documents WHERE doctype = 'TransferOrder' AND deleted_at IS NULL AND status = 'Dispatched'
		  AND COALESCE(NULLIF(data->>'dispatched_at', '')::timestamptz, updated_at) < NOW() - make_interval(days => $1)
		ORDER BY 4 LIMIT 5000`, []interface{}{n},
		func(scan func(...interface{}) error) (map[string]interface{}, error) {
			var id, from, to, at string
			var days int
			if err := scan(&id, &from, &to, &at, &days); err != nil {
				return nil, err
			}
			return map[string]interface{}{"transfer": id, "from": from, "to": to, "dispatched": at, "days": days}, nil
		})
}

func itcPastDeadlineReport(tenantID string, _ map[string]string) ([]map[string]interface{}, error) {
	return dayLimitRows(tenantID, `SELECT COALESCE(data->>'code', id), doctype, COALESCE(data->>'vendor', data->>'vendor_id', ''),
		COALESCE(data->>'vendor_invoice_date', data->>'invoice_date', ''), data->>'itc_time_barred'
		FROM {s}.documents WHERE doctype IN ('GRN', 'VendorInvoice') AND deleted_at IS NULL AND COALESCE(data->>'itc_time_barred', '') <> ''
		ORDER BY created_at DESC LIMIT 5000`, nil,
		func(scan func(...interface{}) error) (map[string]interface{}, error) {
			var id, typ, vendor, date, note string
			if err := scan(&id, &typ, &vendor, &date, &note); err != nil {
				return nil, err
			}
			deadline := ""
			if d, ok := dayLimitDate(date); ok {
				deadline = itcDeadline(d).Format("2006-01-02")
			}
			return map[string]interface{}{"document": id, "type": typ, "vendor": vendor, "invoice_date": date, "deadline": deadline}, nil
		})
}
