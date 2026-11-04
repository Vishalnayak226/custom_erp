package engines

import (
	"custom_erp/db"
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

// Stage 146.5.6: a feed = export template + product group + "ready only" +
// format + destination. Running one stores a PIMExportRun with the rows, the
// skipped items and the reason for each, and what happened to the delivery.
// The file itself is not kept: running the feed again makes it again.

// maxFeedRunSkipped bounds the skipped list kept on one run record; the full
// count is always stored.
const maxFeedRunSkipped = 500

// ValidatePIMFeedDocument runs at ValidateDocument's shared exit.
func ValidatePIMFeedDocument(tenantID string, payload map[string]interface{}) error {
	template := strings.TrimSpace(pimString(payload["export_template"]))
	if template == "" {
		return templateErr("PIM-0956", "Template", "a feed needs an export template")
	}
	var channel string
	if db.DB != nil {
		_, ch, _, _, _, err := fetchPIMExportTemplate(tenantID, template)
		if err != nil {
			return templateErr("PIM-0956", "Template", err.Error())
		}
		channel = ch
		if g := strings.TrimSpace(pimString(payload["product_group"])); g != "" {
			if _, _, gErr := fetchPIMProductGroup(tenantID, g); gErr != nil {
				return templateErr("PIM-0956", "Product Group", gErr.Error())
			}
		}
	}
	ready := strings.TrimSpace(pimString(payload["ready_only"]))
	if ready != "" && ready != "Yes" && ready != "No" {
		return templateErr("PIM-0956", "Ready Only", "ready only must be Yes or No")
	}
	if ready == "Yes" && db.DB != nil && channel == "" {
		return templateErr("PIM-0957", "Ready Only", "ready only needs a template that belongs to a channel")
	}
	if f := strings.ToLower(strings.TrimSpace(pimString(payload["format"]))); f != "" && f != "csv" && f != "xlsx" {
		return templateErr("PIM-0956", "Format", "format must be csv or xlsx")
	}
	switch dest := strings.TrimSpace(pimString(payload["destination"])); dest {
	case "", "Download":
	case "Email":
		if _, err := MailRecipients(pimString(payload["recipient_email"])); err != nil {
			return templateErr("PIM-0956", "Recipient Email", err.Error())
		}
	default:
		return templateErr("PIM-0956", "Destination", "destination must be Download or Email")
	}
	return nil
}

func fetchPIMFeed(tenantID, idOrCode string) (string, map[string]interface{}, string, error) {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return "", nil, "", err
	}
	var id, raw, status string
	err = db.DB.QueryRow(fmt.Sprintf(`SELECT id, data, status FROM %s.documents
		WHERE doctype = 'PIMFeed' AND (id = $1 OR UPPER(data->>'code') = UPPER($1)) AND deleted_at IS NULL
		ORDER BY CASE WHEN id = $1 THEN 0 ELSE 1 END LIMIT 1`, schema), idOrCode).Scan(&id, &raw, &status)
	if err != nil {
		return "", nil, "", templateErr("PIM-0958", "Feed", fmt.Sprintf("feed %q not found", idOrCode))
	}
	var data map[string]interface{}
	if err := json.Unmarshal([]byte(raw), &data); err != nil {
		return "", nil, "", err
	}
	return id, data, status, nil
}

// PIMFeedRunResult is a finished run and its file.
type PIMFeedRunResult struct {
	RunID           string          `json:"run_id"`
	Status          string          `json:"status"`
	Rows            int             `json:"rows"`
	Skipped         []PIMExportSkip `json:"skipped"`
	Destination     string          `json:"destination"`
	Delivery        string          `json:"delivery_status"`
	DeliveryMessage string          `json:"delivery_message"`
	Data            []byte          `json:"-"`
	ContentType     string          `json:"-"`
	Extension       string          `json:"-"`
}

// RunPIMFeed runs a feed now: export through the template with the feed's
// group / ready-only / format, deliver (download = returned to the caller;
// email = through the real mail sender), and record the run.
func RunPIMFeed(tenantID, user, feedID string) (*PIMFeedRunResult, error) {
	id, data, status, err := fetchPIMFeed(tenantID, feedID)
	if err != nil {
		return nil, err
	}
	if status != "Active" {
		return nil, templateErr("PIM-0958", "Feed", fmt.Sprintf("feed %q is not active", feedID))
	}
	started := time.Now().UTC()
	template := strings.TrimSpace(pimString(data["export_template"]))
	format := strings.ToLower(strings.TrimSpace(pimString(data["format"])))
	dest := firstNonEmpty(strings.TrimSpace(pimString(data["destination"])), "Download")
	out, rerr := RunPIMExport(tenantID, template, PIMExportFilter{
		Group: strings.TrimSpace(pimString(data["product_group"])), ReadyOnly: pimString(data["ready_only"]) == "Yes", Format: format})
	res := &PIMFeedRunResult{Destination: dest, Skipped: []PIMExportSkip{}}
	if rerr != nil {
		res.Status, res.Delivery, res.DeliveryMessage = "Failed", "Not delivered", rerr.Error()
		res.RunID = recordPIMFeedRun(tenantID, user, id, template, format, started, res)
		return res, rerr
	}
	res.Rows, res.Skipped = out.Rows, out.Skipped
	res.Data, res.ContentType, res.Extension = out.Data, out.ContentType, out.Extension
	res.Status = "Exported"
	if len(out.Skipped) > 0 {
		res.Status = "Exported with skips"
	}
	if dest == "Email" {
		name := firstNonEmpty(pimString(data["code"]), id) + "." + out.Extension
		res.Delivery, res.DeliveryMessage = SendOutboxEmail(tenantID, OutboxEmail{
			To: pimString(data["recipient_email"]), Subject: "Feed " + firstNonEmpty(pimString(data["name"]), id),
			Body:       fmt.Sprintf("Rows: %d. Skipped: %d.", out.Rows, len(out.Skipped)),
			AttachName: name, AttachType: out.ContentType, Attach: out.Data})
	} else {
		res.Delivery = "Download"
	}
	res.RunID = recordPIMFeedRun(tenantID, user, id, template, format, started, res)
	return res, nil
}

func recordPIMFeedRun(tenantID, user, feedID, template, format string, started time.Time, r *PIMFeedRunResult) string {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return ""
	}
	skipped := r.Skipped
	if len(skipped) > maxFeedRunSkipped {
		skipped = skipped[:maxFeedRunSkipped]
	}
	id := NewDocID("PXR")
	body, _ := json.Marshal(map[string]interface{}{"code": id, "feed": feedID, "export_template": template,
		"started_at": started.Format(time.RFC3339), "finished_at": time.Now().UTC().Format(time.RFC3339),
		"status": r.Status, "rows": r.Rows, "skipped_count": len(r.Skipped), "skipped": skipped,
		"format": firstNonEmpty(format, "csv"), "destination": r.Destination,
		"delivery_status": r.Delivery, "delivery_message": r.DeliveryMessage, "run_by": user})
	if _, err := db.DB.Exec(fmt.Sprintf(`INSERT INTO %s.documents(id, doctype, data, status, created_by) VALUES($1,'PIMExportRun',$2,$3,$4)`, schema),
		id, body, r.Status, user); err != nil {
		return ""
	}
	return id
}
