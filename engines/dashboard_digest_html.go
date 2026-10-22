package engines

import (
	"bytes"
	"encoding/csv"
	"fmt"
	"html"
	"log"
	"mime/multipart"
	"net/smtp"
	"net/textproto"
	"os"
	"strings"
	"time"

	"custom_erp/db"
)

// Stage 100.3.4: a custom dashboard's scheduled email (DashboardDigest)
// carries its charts, not only a CSV: each widget as a number or as bars
// drawn with table cells - every mail app shows those, where SVG is
// stripped - with the CSV attached. Run as the digest's owner and role, so
// it holds only what they may see (their stores, money if their role may).

// deliverCustomDashboardDigest renders and sends one digest; it returns the
// run status and how many rows the widgets held.
func deliverCustomDashboardDigest(schema, tenantID, digestID, role string, cd CustomDashboard, data map[string]interface{}) (string, int) {
	var owner string
	_ = db.DB.QueryRow(fmt.Sprintf(`SELECT COALESCE(created_by, '') FROM %s.documents WHERE id = $1`, schema), digestID).Scan(&owner)
	if owner == "" || owner == "system" {
		owner = cd.Owner
	}
	if role == "" {
		_ = db.DB.QueryRow(fmt.Sprintf(`SELECT COALESCE(role, '') FROM %s.users WHERE id = $1 OR username = $1 LIMIT 1`, schema), owner).Scan(&role)
	}
	body, csvText, rows := CustomDashboardDigestContent(tenantID, cd, DatasetViewer{Role: role, UserID: owner}, time.Now())

	recipient, _ := data["recipient_email"].(string)
	webhookURL, _ := data["webhook_url"].(string)
	tx, err := db.DB.Begin()
	if err != nil {
		return "Failed: " + err.Error(), rows
	}
	_ = db.SetSearchPath(tx, schema)
	if err := PublishEvent(tx, schema, "dashboard.digest_delivery", map[string]interface{}{
		"digest_id": digestID, "dashboard_layout_id": cd.ID, "tile_count": len(cd.Widgets), "row_count": rows,
		"recipient_email": recipient, "webhook_url": webhookURL, "csv": csvText, "html": body,
	}); err != nil {
		_ = tx.Rollback()
		return "Failed: " + err.Error(), rows
	}
	_ = tx.Commit()
	if strings.TrimSpace(recipient) == "" {
		return "Delivered", rows
	}
	switch sent, err := sendDashboardDigestMail(tenantID, recipient, cd.Name, body, csvText); {
	case err == ErrMailNotConfigured:
		return "Delivered (no mail account on this server - the webhook / event carries it)", rows
	case err != nil:
		return "Failed: " + err.Error(), rows
	case !sent:
		return "Delivered (simulated email)", rows
	}
	return "Delivered", rows
}

// CustomDashboardDigestContent is the email body and the CSV for one run.
func CustomDashboardDigestContent(tenantID string, cd CustomDashboard, v DatasetViewer, now time.Time) (string, string, int) {
	var page, csvAll strings.Builder
	total := 0
	from, to := ResolveDashboardRange(cd.Filters, now)
	period := "all dates"
	if from != "" {
		period = from + " to " + to
	}
	fmt.Fprintf(&page, `<div style="font-family:Arial,Helvetica,sans-serif;color:#1f2937;max-width:720px;">`+
		`<h2 style="margin:0 0 4px;">%s</h2><p style="margin:0 0 16px;color:#6b7280;">%s</p>`, html.EscapeString(cd.Name), html.EscapeString(period))
	for _, w := range cd.Widgets {
		fmt.Fprintf(&page, `<h3 style="margin:18px 0 6px;font-size:15px;">%s</h3>`, html.EscapeString(w.Title))
		csvAll.WriteString("## " + w.Title + "\n")
		if w.Source != "dataset" || w.Query == nil {
			page.WriteString(`<p style="color:#6b7280;">Open the dashboard to see this report.</p>`)
			continue
		}
		res, err := RunDatasetQuery(tenantID, v, ApplyDashboardFilters(*w.Query, cd.Filters, w.OwnFilters, now))
		if err != nil {
			fmt.Fprintf(&page, `<p style="color:#b91c1c;">Not shown: %s</p>`, html.EscapeString(err.Error()))
			csvAll.WriteString("(failed: " + err.Error() + ")\n\n")
			continue
		}
		total += len(res.Rows)
		csvAll.WriteString(datasetResultCSV(res))
		csvAll.WriteString("\n")
		page.WriteString(digestWidgetHTML(w, res))
	}
	page.WriteString(`<p style="margin-top:20px;color:#9ca3af;font-size:12px;">The figures as of this email, for the stores you may see. The CSV attached holds every widget's rows.</p></div>`)
	return page.String(), csvAll.String(), total
}

func digestFormat(v interface{}, money bool) string {
	switch x := v.(type) {
	case nil:
		if money {
			return "hidden"
		}
		return "-"
	case float64:
		s := fmt.Sprintf("%.2f", x)
		s = strings.TrimSuffix(strings.TrimSuffix(s, "0"), "0")
		s = strings.TrimSuffix(s, ".")
		if money {
			return "Rs " + s
		}
		return s
	}
	return fmt.Sprint(v)
}

func digestWidgetHTML(w DashboardWidget, res DatasetResult) string {
	var b strings.Builder
	var groups, measures []DatasetColumn
	for _, c := range res.Columns {
		if c.Type == "number" {
			measures = append(measures, c)
		} else {
			groups = append(groups, c)
		}
	}
	if len(measures) == 0 {
		return ""
	}
	m := measures[0]
	if len(groups) == 0 || w.Kind == "number" {
		if len(res.Rows) == 0 {
			return `<p style="color:#6b7280;">No data.</p>`
		}
		val := res.Rows[0][m.Key]
		fmt.Fprintf(&b, `<div style="font-size:28px;font-weight:bold;">%s</div>`, html.EscapeString(digestFormat(val, m.Money)))
		if res.Previous != nil {
			cur, ok1 := val.(float64)
			prev, ok2 := res.Previous[m.Key].(float64)
			if ok1 && ok2 && prev != 0 {
				pct := (cur - prev) / prev * 100
				color, arrow := "#15803d", "&#9650;"
				if pct < 0 {
					color, arrow = "#b91c1c", "&#9660;"
				}
				fmt.Fprintf(&b, `<div style="color:%s;font-size:13px;">%s %.1f%% on the period before</div>`, color, arrow, pct)
			}
		}
		return b.String()
	}
	if len(groups) > 1 || w.Kind == "table" || w.Kind == "pivot" {
		b.WriteString(`<table style="border-collapse:collapse;font-size:13px;">`)
		b.WriteString("<tr>")
		for _, c := range res.Columns {
			fmt.Fprintf(&b, `<th style="text-align:left;padding:4px 10px;border-bottom:1px solid #e5e7eb;">%s</th>`, html.EscapeString(c.Label))
		}
		b.WriteString("</tr>")
		for i, r := range res.Rows {
			if i == 20 {
				break
			}
			b.WriteString("<tr>")
			for _, c := range res.Columns {
				fmt.Fprintf(&b, `<td style="padding:4px 10px;border-bottom:1px solid #f3f4f6;">%s</td>`, html.EscapeString(digestFormat(r[c.Key], c.Money)))
			}
			b.WriteString("</tr>")
		}
		b.WriteString("</table>")
		return b.String()
	}
	max := 0.0
	for _, r := range res.Rows {
		if v, ok := r[m.Key].(float64); ok && v > max {
			max = v
		}
	}
	b.WriteString(`<table style="border-collapse:collapse;font-size:13px;width:100%;">`)
	for i, r := range res.Rows {
		if i == 15 {
			break
		}
		v, _ := r[m.Key].(float64)
		pct := 0
		if max > 0 && v > 0 {
			pct = int(v / max * 100)
		}
		fmt.Fprintf(&b, `<tr><td style="padding:3px 8px;white-space:nowrap;width:30%%;">%s</td>`+
			`<td style="padding:3px 0;width:55%%;"><div style="background:#4f6bed;height:12px;width:%d%%;min-width:2px;border-radius:2px;"></div></td>`+
			`<td style="padding:3px 8px;text-align:right;white-space:nowrap;">%s</td></tr>`,
			html.EscapeString(fmt.Sprint(r[groups[0].Key])), pct, html.EscapeString(digestFormat(r[m.Key], m.Money)))
	}
	b.WriteString("</table>")
	return b.String()
}

// datasetResultCSV is a result as CSV, header first.
func datasetResultCSV(res DatasetResult) string {
	var buf bytes.Buffer
	w := csv.NewWriter(&buf)
	head := make([]string, len(res.Columns))
	for i, c := range res.Columns {
		head[i] = c.Label
	}
	_ = w.Write(head)
	for _, r := range res.Rows {
		line := make([]string, len(res.Columns))
		for i, c := range res.Columns {
			if r[c.Key] != nil {
				line[i] = csvSafeCell(fmt.Sprint(r[c.Key]))
			}
		}
		_ = w.Write(line)
	}
	w.Flush()
	return buf.String()
}

// csvSafeCell keeps a cell from being read as a spreadsheet formula.
func csvSafeCell(s string) string {
	if s != "" && strings.ContainsRune("=+-@", rune(s[0])) {
		if _, err := fmt.Sscanf(s, "%g", new(float64)); err != nil {
			return "'" + s
		}
	}
	return s
}

// sendDashboardDigestMail sends the digest as an HTML email with the CSV
// attached, through the server's mail account (SMTP_*, as documents are
// sent). simulated when the client is a sandbox or side effects are off.
func sendDashboardDigestMail(tenantID, to, name, body, csvText string) (sent bool, err error) {
	recipients, err := MailRecipients(to)
	if err != nil {
		return false, err
	}
	host := os.Getenv("SMTP_HOST")
	if host == "" {
		return false, ErrMailNotConfigured
	}
	if sandbox, _ := IsSandboxTenant(tenantID); sandbox || !ExternalSideEffectsEnabled() {
		log.Printf("[DASHBOARD_DIGEST] email simulated (sandbox or side effects off): %q to %d recipient(s)", name, len(recipients))
		return false, nil
	}
	port := os.Getenv("SMTP_PORT")
	if port == "" {
		port = "587"
	}
	from := os.Getenv("SMTP_FROM")
	if from == "" {
		from = "no-reply@custom-erp.local"
	}
	var msg bytes.Buffer
	mw := multipart.NewWriter(&msg)
	subject := strings.NewReplacer("\r", " ", "\n", " ").Replace("Dashboard: " + name)
	fmt.Fprintf(&msg, "From: %s\r\nTo: %s\r\nSubject: %s\r\nMIME-Version: 1.0\r\nContent-Type: multipart/mixed; boundary=%s\r\n\r\n",
		from, strings.Join(recipients, ", "), subject, mw.Boundary())
	part, _ := mw.CreatePart(textproto.MIMEHeader{"Content-Type": {"text/html; charset=utf-8"}, "Content-Transfer-Encoding": {"base64"}})
	writeBase64Lines(part, []byte(body))
	att, _ := mw.CreatePart(textproto.MIMEHeader{"Content-Type": {"text/csv; charset=utf-8"}, "Content-Transfer-Encoding": {"base64"},
		"Content-Disposition": {`attachment; filename="dashboard.csv"`}})
	writeBase64Lines(att, []byte(csvText))
	_ = mw.Close()
	var auth smtp.Auth
	if user := os.Getenv("SMTP_USER"); user != "" {
		auth = smtp.PlainAuth("", user, os.Getenv("SMTP_PASSWORD"), host)
	}
	if err := smtp.SendMail(host+":"+port, auth, from, recipients, msg.Bytes()); err != nil {
		return false, fmt.Errorf("the mail server refused the email: %v", err)
	}
	return true, nil
}

// DatasetResultXLSX is a result as an Excel sheet, header first (100.3.4).
func DatasetResultXLSX(title string, res DatasetResult) ([]byte, error) {
	sheet := XLSXSheet{Name: title, FreezeRows: 1}
	head := make([]XLSXCell, len(res.Columns))
	widths := make([]float64, len(res.Columns))
	for i, c := range res.Columns {
		head[i] = XLSXCell{Text: c.Label, Header: true}
		widths[i] = 18
	}
	sheet.ColWidths = widths
	sheet.Rows = append(sheet.Rows, head)
	for _, r := range res.Rows {
		line := make([]XLSXCell, len(res.Columns))
		for i, c := range res.Columns {
			switch v := r[c.Key].(type) {
			case float64:
				line[i] = XLSXCell{Num: v, IsNum: true}
			case nil:
				if c.Money {
					line[i] = XLSXCell{Text: "hidden"}
				}
			default:
				line[i] = XLSXCell{Text: fmt.Sprint(v)}
			}
		}
		sheet.Rows = append(sheet.Rows, line)
	}
	return WriteXLSX(sheet)
}
