package engines

import (
	"crypto/sha256"
	"custom_erp/db"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"strings"
	"time"
)

// Stage 101.3.2 / 101.3.3 / 101.3.5 - campaigns that really send, their
// results, and hold-outs (the blueprint's M12, M15, M16, M22, M23).
//
// A campaign is segment version x template version x channel x schedule x
// budget x goal. It is a Campaign record (kind "Message") so it goes through
// the approval engine like any other document (D13): Submit pins the exact
// segment and template versions and a hash of everything that matters;
// approval schedules a run; any edit after that sends it back to Draft and
// needs approving again. The client's Stage 92 self-approval setting
// applies, unless crm.campaign_second_approver is on.
//
// A run, at its start time, freezes who is in the segment (a snapshot),
// records the hold-out group as HeldOut sends, gives each customer their own
// one-use voucher when the campaign offers one, and queues one send per
// customer for the gate. Older trigger campaigns (Birthday / Lapsed, 26.7.4)
// keep working as they did; their replacement is the ready-made journeys.

const crmCampaignKind = "Message"

// CRMCampaignInput is a save from the campaign screen.
type CRMCampaignInput struct {
	Name         string  `json:"name"`
	Channel      string  `json:"channel"`
	SegmentID    string  `json:"segment_id"`
	TemplateID   string  `json:"template_id"`
	ScheduleAt   string  `json:"schedule_at"` // YYYY-MM-DDTHH:MM, client time zone; blank = as soon as approved
	EndsAt       string  `json:"ends_at"`
	Budget       float64 `json:"budget"`
	GoalDays     int     `json:"goal_days"`
	HoldoutPct   int     `json:"holdout_pct"`
	SkipIfBought bool    `json:"skip_if_bought"`
	Voucher      *struct {
		DiscountType  string `json:"discount_type"`
		DiscountValue int    `json:"discount_value"`
		ValidDays     int    `json:"valid_days"`
		Channel       string `json:"channel"`
	} `json:"voucher,omitempty"`
}

// CRMCampaign is a campaign with its latest run.
type CRMCampaign struct {
	ID       string                 `json:"id"`
	Status   string                 `json:"status"`
	Data     map[string]interface{} `json:"data"`
	Runs     []CRMCampaignRun       `json:"runs"`
	Legacy   bool                   `json:"legacy"`
	Approval []ApprovalLogEntry     `json:"approval_log,omitempty"`
}

// CRMCampaignRun is one run's state and counts.
type CRMCampaignRun struct {
	ID              string         `json:"id"`
	Status          string         `json:"status"`
	Test            bool           `json:"test"`
	ScheduledAt     time.Time      `json:"scheduled_at"`
	StartedAt       *time.Time     `json:"started_at,omitempty"`
	FinishedAt      *time.Time     `json:"finished_at,omitempty"`
	Channel         string         `json:"channel"`
	TemplateID      string         `json:"template_id"`
	TemplateVersion int            `json:"template_version"`
	SnapshotID      string         `json:"snapshot_id,omitempty"`
	Budget          *float64       `json:"budget,omitempty"`
	HoldoutPct      int            `json:"holdout_pct"`
	Counts          map[string]int `json:"counts"`
}

func crmParseLocal(schema, s string) (string, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return "", nil
	}
	for _, layout := range []string{"2006-01-02T15:04", "2006-01-02 15:04", "2006-01-02T15:04:05", "2006-01-02"} {
		if t, err := time.ParseInLocation(layout, s, crmLocation(schema)); err == nil {
			return t.Format("2006-01-02 15:04"), nil
		}
	}
	return "", crmErr(CRMCodeCampaign, "%q is not a date and time", s)
}

// crmLocalToDBSQL converts a client-zone wall clock ('2026-10-25 10:00')
// into the session-local TIMESTAMP every documents/crm column uses.
func crmLocalToDBSQL(param, zone string) string {
	return fmt.Sprintf("((%s::timestamp AT TIME ZONE '%s') AT TIME ZONE current_setting('TimeZone'))", param, strings.ReplaceAll(zone, "'", ""))
}

// SaveCRMCampaign creates (id "") or edits a campaign. Editing one that was
// submitted or approved puts it back to Draft: it must be approved again.
func SaveCRMCampaign(tenantID, id string, in CRMCampaignInput, actor string) (*CRMCampaign, error) {
	schema, err := crmSchema(tenantID)
	if err != nil {
		return nil, err
	}
	in.Name = strings.TrimSpace(in.Name)
	channel := crmChannel(in.Channel)
	if in.Name == "" || channel == "" {
		return nil, crmErr(CRMCodeCampaign, "a campaign needs a name and a channel")
	}
	if in.HoldoutPct < 0 || in.HoldoutPct > 50 {
		return nil, crmErr(CRMCodeCampaign, "a hold-out is 0 to 50 percent")
	}
	if in.Budget < 0 {
		return nil, crmErr(CRMCodeCampaign, "a budget cannot be negative")
	}
	if in.GoalDays <= 0 {
		in.GoalDays = GetSettingIntForSchema(schema, "crm.attribution_days")
	}
	schedule, err := crmParseLocal(schema, in.ScheduleAt)
	if err != nil {
		return nil, err
	}
	ends, err := crmParseLocal(schema, in.EndsAt)
	if err != nil {
		return nil, err
	}
	if schedule != "" && ends != "" && ends <= schedule {
		return nil, crmErr(CRMCodeCampaign, "the end must be after the start")
	}
	data := map[string]interface{}{"kind": crmCampaignKind, "name": in.Name, "channel": channel, "segment_id": strings.TrimSpace(in.SegmentID),
		"template_id": strings.TrimSpace(in.TemplateID), "schedule_at": schedule, "ends_at": ends, "budget": in.Budget, "goal_days": in.GoalDays,
		"holdout_pct": in.HoldoutPct, "skip_if_bought": in.SkipIfBought, "total_amount": in.Budget, "trigger_type": ""}
	if in.Voucher != nil && in.Voucher.DiscountValue > 0 {
		vt := in.Voucher.DiscountType
		if vt != "Flat" {
			vt = "Percentage"
		}
		if vt == "Percentage" && in.Voucher.DiscountValue > 90 {
			return nil, crmErr(CRMCodeCampaign, "a percentage voucher can be at most 90%%")
		}
		valid := in.Voucher.ValidDays
		if valid <= 0 {
			valid = 30
		}
		data["voucher"] = map[string]interface{}{"discount_type": vt, "discount_value": in.Voucher.DiscountValue, "valid_days": valid,
			"channel": firstNonEmpty(in.Voucher.Channel, "Any")}
	}
	if id == "" {
		id = NewDocID("CMP")
		data["code"] = id
		data["status"] = "Draft"
		if _, err := db.DB.Exec(fmt.Sprintf(`INSERT INTO %s.documents (id, doctype, data, status, created_by) VALUES ($1, 'Campaign', $2, 'Draft', $3)`, schema),
			id, crmJSON(data), actor); err != nil {
			return nil, err
		}
		return GetCRMCampaign(tenantID, id)
	}
	cur, status, err := crmLoadCampaignDoc(schema, id)
	if err != nil {
		return nil, err
	}
	if strField(cur, "kind") != crmCampaignKind {
		return nil, crmErr(CRMCodeCampaign, "this is an older trigger campaign - edit it from the Campaign records list")
	}
	if status == "Running" || status == "Paused" {
		return nil, crmErr(CRMCodeCampaign, "the campaign is sending - stop it first, or copy it into a new one")
	}
	if status == "Completed" || status == "Stopped" {
		return nil, crmErr(CRMCodeCampaign, "the campaign has finished - copy it into a new one")
	}
	data["code"] = firstNonEmpty(strField(cur, "code"), id)
	data["status"] = "Draft"
	if _, err := db.DB.Exec(fmt.Sprintf(`UPDATE %s.documents SET data = $2, status = 'Draft', updated_at = CURRENT_TIMESTAMP WHERE id = $1 AND doctype = 'Campaign'`, schema),
		id, crmJSON(data)); err != nil {
		return nil, err
	}
	if status == "Pending Approval" || status == "Approved" {
		_ = logApprovalAction(tenantID, "Campaign", id, "Edited after submission - needs approval again", actor, "", in.Budget, "")
		_, _ = db.DB.Exec(fmt.Sprintf(`UPDATE %s.crm_campaign_runs SET status = 'Cancelled', finished_at = LOCALTIMESTAMP WHERE campaign_id = $1 AND status = 'Scheduled'`, schema), id)
	}
	return GetCRMCampaign(tenantID, id)
}

func crmLoadCampaignDoc(schema, id string) (map[string]interface{}, string, error) {
	var raw []byte
	var status string
	err := db.DB.QueryRow(fmt.Sprintf(`SELECT data, status FROM %s.documents WHERE id = $1 AND doctype = 'Campaign' AND deleted_at IS NULL`, schema), id).Scan(&raw, &status)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, "", crmNotFound("campaign", id)
	}
	if err != nil {
		return nil, "", err
	}
	var d map[string]interface{}
	_ = json.Unmarshal(raw, &d)
	if d == nil {
		d = map[string]interface{}{}
	}
	return d, status, nil
}

// crmCampaignHash is what approval covers; a change to any of it after
// submission means the approval no longer applies.
func crmCampaignHash(d map[string]interface{}) string {
	keys := []string{"name", "channel", "segment_id", "segment_version", "template_id", "template_version", "schedule_at", "ends_at", "budget",
		"goal_days", "holdout_pct", "skip_if_bought", "voucher"}
	m := map[string]interface{}{}
	for _, k := range keys {
		m[k] = d[k]
	}
	sum := sha256.Sum256(crmJSON(m))
	return hex.EncodeToString(sum[:])
}

// crmCampaignProblems is what stops a campaign from being submitted.
func crmCampaignProblems(schema string, d map[string]interface{}) []string {
	var p []string
	channel := strField(d, "channel")
	if strField(d, "segment_id") == "" {
		p = append(p, "choose a segment")
	} else if _, err := crmGetSegment(schema, strField(d, "segment_id"), 0); err != nil {
		p = append(p, "the segment no longer exists")
	}
	if strField(d, "template_id") == "" {
		p = append(p, "choose a template")
	} else if t, err := crmGetTemplate(schema, strField(d, "template_id")); err != nil {
		p = append(p, "the template no longer exists")
	} else {
		if t.Channel != channel {
			p = append(p, fmt.Sprintf("the template is for %s, the campaign sends by %s", t.Channel, channel))
		}
		if t.Purpose != CRMPurposeMarketing {
			p = append(p, "a campaign sends offers - choose a marketing template")
		}
		if s, _ := crmActiveSender(schema, channel); s != nil && s.Provider != CRMProviderMock && s.Provider != CRMProviderWebhook {
			if ok, why := crmTemplateReady(t); !ok {
				p = append(p, "the template "+why)
			}
		} else if t.Status != "Active" {
			p = append(p, "the template is switched off")
		}
	}
	if s, err := crmActiveSender(schema, channel); err != nil || s == nil {
		p = append(p, "no active "+channel+" sender (CRM > Settings > Senders)")
	} else if len(s.Problems) > 0 {
		p = append(p, "the "+channel+" sender is not complete: "+strings.Join(s.Problems, "; "))
	}
	for i, s := range p { // each one is shown as a sentence of its own
		p[i] = strings.ToUpper(s[:1]) + s[1:]
	}
	return p
}

// SubmitCRMCampaign pins the versions and sends the campaign for approval.
func SubmitCRMCampaign(tenantID, id, actor, role string) (*CRMCampaign, error) {
	schema, err := crmSchema(tenantID)
	if err != nil {
		return nil, err
	}
	d, status, err := crmLoadCampaignDoc(schema, id)
	if err != nil {
		return nil, err
	}
	if status != "Draft" && status != "Rejected" {
		return nil, crmErr(CRMCodeCampaign, "only a draft campaign can be submitted (this one is %s)", status)
	}
	if p := crmCampaignProblems(schema, d); len(p) > 0 {
		return nil, crmErr(CRMCodeCampaign, "%s", strings.Join(p, "; "))
	}
	seg, _ := crmGetSegment(schema, strField(d, "segment_id"), 0)
	tpl, _ := crmGetTemplate(schema, strField(d, "template_id"))
	d["segment_version"], d["template_version"] = seg.CurrentVersion, tpl.CurrentVersion
	d["approval_hash"] = crmCampaignHash(d)
	d["submitted_by"] = actor
	d["status"] = "Draft"
	if _, err := db.DB.Exec(fmt.Sprintf(`UPDATE %s.documents SET data = $2, status = 'Draft', updated_at = CURRENT_TIMESTAMP WHERE id = $1`, schema), id, crmJSON(d)); err != nil {
		return nil, err
	}
	if err := SubmitForApproval(tenantID, "Campaign", id, actor, role); err != nil {
		return nil, err
	}
	return GetCRMCampaign(tenantID, id)
}

func init() {
	RegisterApprovalDecisionHook("Campaign", false, func(tenantID, docID, decision, actor string) {
		if decision != "Approved" {
			return
		}
		if err := crmScheduleApprovedCampaign(tenantID, docID, actor); err != nil {
			LogSystemError(tenantID, "", "ERROR", "crm_campaign", fmt.Sprintf("approved campaign %s not scheduled: %v", docID, err), "")
		}
	})
	registerCRMTick("campaign runs", crmCampaignTick)
}

// crmScheduleApprovedCampaign creates the run an approval allows - once, and
// only if nothing approval covers changed since submission.
func crmScheduleApprovedCampaign(tenantID, id, actor string) error {
	schema, err := crmSchema(tenantID)
	if err != nil {
		return err
	}
	d, status, err := crmLoadCampaignDoc(schema, id)
	if err != nil || strField(d, "kind") != crmCampaignKind {
		return err
	}
	if status != "Approved" {
		return nil
	}
	if strField(d, "approval_hash") != crmCampaignHash(d) {
		_, _ = db.DB.Exec(fmt.Sprintf(`UPDATE %s.documents SET status = 'Draft', updated_at = CURRENT_TIMESTAMP WHERE id = $1`, schema), id)
		return fmt.Errorf("campaign %s changed after it was submitted - back to Draft", id)
	}
	var exists bool
	_ = db.DB.QueryRow(fmt.Sprintf(`SELECT EXISTS(SELECT 1 FROM %s.crm_campaign_runs WHERE campaign_id = $1 AND test = FALSE AND status <> 'Cancelled')`, schema), id).Scan(&exists)
	if exists {
		return nil
	}
	zone := crmZoneName(schema)
	sched := "LOCALTIMESTAMP"
	args := []interface{}{NewDocID("RUN"), id, crmCampaignHash(d), strField(d, "channel"), strField(d, "template_id"), int(numFromInterface(d["template_version"])),
		int(numFromInterface(d["holdout_pct"])), int(numFromInterface(d["goal_days"])), actor}
	var budget interface{}
	if b := numFromInterface(d["budget"]); b > 0 {
		budget = b
	}
	args = append(args, budget, crmJSON(d["voucher"]), crmJSON(map[string]interface{}{"skip_if_bought": truthy(d["skip_if_bought"])}))
	if s := strField(d, "schedule_at"); s != "" {
		args = append(args, s)
		sched = crmLocalToDBSQL(fmt.Sprintf("$%d", len(args)), zone)
	}
	ends := "NULL"
	if e := strField(d, "ends_at"); e != "" {
		args = append(args, e)
		ends = crmLocalToDBSQL(fmt.Sprintf("$%d", len(args)), zone)
	}
	_, err = db.DB.Exec(fmt.Sprintf(`INSERT INTO %s.crm_campaign_runs (id, campaign_id, campaign_hash, channel, template_id, template_version, holdout_pct, goal_days, created_by,
		budget, voucher, options, scheduled_at, ends_at, status)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, %s, %s, 'Scheduled')`, schema, sched, ends), args...)
	return err
}

// crmCampaignTick starts runs that are due and closes those that finished.
func crmCampaignTick(schema string) {
	rows, err := db.DB.Query(fmt.Sprintf(`SELECT id FROM %s.crm_campaign_runs WHERE status = 'Scheduled' AND scheduled_at <= LOCALTIMESTAMP ORDER BY scheduled_at LIMIT 5`, schema))
	if err != nil {
		return
	}
	var due []string
	for rows.Next() {
		var id string
		if rows.Scan(&id) == nil {
			due = append(due, id)
		}
	}
	rows.Close()
	for _, id := range due {
		if err := crmStartRun(schema, id); err != nil {
			LogSystemError("", "", "ERROR", "crm_campaign", fmt.Sprintf("run %s in %s did not start: %v", id, schema, err), "")
			_, _ = db.DB.Exec(fmt.Sprintf(`UPDATE %s.crm_campaign_runs SET status = 'Failed', finished_at = LOCALTIMESTAMP, counts = counts || $2::jsonb WHERE id = $1`, schema),
				id, crmJSON(map[string]string{"error": err.Error()}))
		}
	}
	_, _ = db.DB.Exec(fmt.Sprintf(`UPDATE %[1]s.crm_campaign_runs r SET status = 'Completed', finished_at = LOCALTIMESTAMP
		WHERE r.status = 'Running' AND NOT EXISTS (SELECT 1 FROM %[1]s.crm_sends s WHERE s.run_kind = 'campaign' AND s.run_id = r.id AND s.status IN ('Queued', 'Deferred', 'Sending'))`, schema))
	_, _ = db.DB.Exec(fmt.Sprintf(`UPDATE %[1]s.documents c SET status = 'Completed', data = data || '{"status":"Completed"}'::jsonb, updated_at = CURRENT_TIMESTAMP
		WHERE c.doctype = 'Campaign' AND c.status = 'Running' AND NOT EXISTS (SELECT 1 FROM %[1]s.crm_campaign_runs r WHERE r.campaign_id = c.id AND r.test = FALSE AND r.status IN ('Scheduled', 'Running', 'Paused'))`, schema))
}

// crmStartRun freezes the segment and queues one send per customer.
func crmStartRun(schema, runID string) error {
	tx, err := db.DB.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var campaignID, channel, templateID string
	var templateVersion, holdout int
	var voucherJSON []byte
	var endsAt sql.NullTime
	err = tx.QueryRow(fmt.Sprintf(`SELECT campaign_id, channel, COALESCE(template_id, ''), COALESCE(template_version, 0), holdout_pct, voucher, ends_at
		FROM %s.crm_campaign_runs WHERE id = $1 AND status = 'Scheduled' FOR UPDATE`, schema), runID).
		Scan(&campaignID, &channel, &templateID, &templateVersion, &holdout, &voucherJSON, &endsAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	d, _, err := crmLoadCampaignDoc(schema, campaignID)
	if err != nil {
		return err
	}
	snap, matched, err := crmSnapshotSegment(tx, schema, strField(d, "segment_id"), int(numFromInterface(d["segment_version"])), "campaign", runID)
	if err != nil {
		return err
	}
	expires := "r.ends_at"
	var voucher map[string]interface{}
	_ = json.Unmarshal(voucherJSON, &voucher)
	held := crmHoldoutSQL("r.campaign_id", "m.customer_id", holdout)
	if len(voucher) == 0 {
		if _, err := tx.Exec(fmt.Sprintf(`INSERT INTO %[1]s.crm_sends (id, run_kind, run_id, customer_id, channel, purpose, template_id, template_version, status, reason_code,
			reason_detail, expires_at, final_at, created_by, history)
			SELECT 'SND-' || r.id || '-' || md5(m.customer_id), 'campaign', r.id, m.customer_id, r.channel, 'marketing', r.template_id, r.template_version,
			       CASE WHEN %[2]s THEN 'HeldOut' ELSE 'Queued' END, CASE WHEN %[2]s THEN 'HOLD_OUT' END,
			       CASE WHEN %[2]s THEN 'In the campaign''s hold-out group (not sent, on purpose)' END,
			       %[3]s, CASE WHEN %[2]s THEN LOCALTIMESTAMP END, 'campaign', '[]'::jsonb
			  FROM %[1]s.crm_segment_snapshot_members m JOIN %[1]s.crm_campaign_runs r ON r.id = $1
			 WHERE m.snapshot_id = $2
			ON CONFLICT (run_kind, run_id, customer_id, channel) DO NOTHING`, schema, held, expires), runID, snap); err != nil {
			return err
		}
	} else {
		rows, err := tx.Query(fmt.Sprintf(`SELECT customer_id FROM %s.crm_segment_snapshot_members WHERE snapshot_id = $1`, schema), snap)
		if err != nil {
			return err
		}
		var members []string
		for rows.Next() {
			var id string
			if rows.Scan(&id) == nil {
				members = append(members, id)
			}
		}
		rows.Close()
		for _, cust := range members {
			req := CRMSendRequest{RunKind: "campaign", RunID: runID, CustomerID: cust, Channel: channel, Purpose: CRMPurposeMarketing, TemplateID: templateID,
				TemplateVersion: templateVersion, CreatedBy: "campaign", ExpiresAtSQL: fmt.Sprintf("(SELECT ends_at FROM %s.crm_campaign_runs WHERE id = '%s')", schema, strings.ReplaceAll(runID, "'", ""))}
			if crmInHoldout(campaignID, cust, holdout) {
				req.Status, req.ReasonCode = crmSendHeldOut, "HOLD_OUT"
			} else {
				code, value, expiry, err := crmIssueCampaignVoucherTx(tx, schema, campaignID, runID, cust, voucher)
				if err != nil {
					return err
				}
				req.Vars = map[string]string{"voucher.code": code, "voucher.value": value, "voucher.expiry": expiry}
			}
			if _, _, err := crmQueueSend(tx, schema, req); err != nil {
				return err
			}
		}
	}
	var heldOut int
	_ = tx.QueryRow(fmt.Sprintf(`SELECT COUNT(*) FROM %s.crm_sends WHERE run_kind = 'campaign' AND run_id = $1 AND status = 'HeldOut'`, schema), runID).Scan(&heldOut)
	if _, err := tx.Exec(fmt.Sprintf(`UPDATE %s.crm_campaign_runs SET status = 'Running', snapshot_id = $2, started_at = LOCALTIMESTAMP, counts = $3 WHERE id = $1`, schema),
		runID, snap, crmJSON(map[string]int{"matched": matched, "held_out": heldOut})); err != nil {
		return err
	}
	if _, err := tx.Exec(fmt.Sprintf(`UPDATE %s.documents SET status = 'Running', data = data || '{"status":"Running"}'::jsonb, updated_at = CURRENT_TIMESTAMP WHERE id = $1 AND doctype = 'Campaign' AND status = 'Approved'`, schema), campaignID); err != nil {
		return err
	}
	return tx.Commit()
}

// crmIssueCampaignVoucherTx gives one customer their own one-use code.
func crmIssueCampaignVoucherTx(tx *sql.Tx, schema, campaignID, runID, customerID string, v map[string]interface{}) (code, value, expiry string, err error) {
	var existing string
	if err := tx.QueryRow(fmt.Sprintf(`SELECT data->>'code' FROM %s.documents WHERE doctype = 'Voucher' AND data->>'campaign_run' = $1 AND data->>'customer_id' = $2 LIMIT 1`, schema),
		runID, customerID).Scan(&existing); err == nil {
		code = existing
	} else {
		if code, err = crmCode("V", 10); err != nil {
			return "", "", "", err
		}
	}
	days := int(numFromInterface(v["valid_days"]))
	if days <= 0 {
		days = 30
	}
	exp := time.Now().In(crmLocation(schema)).AddDate(0, 0, days)
	dt := strField(v, "discount_type")
	dv := int(numFromInterface(v["discount_value"]))
	value = fmt.Sprintf("%d%% off", dv)
	if dt == "Flat" {
		value = fmt.Sprintf("%d off", dv)
	}
	expiry = exp.Format("2 Jan")
	if existing != "" {
		return code, value, expiry, nil
	}
	ch := strField(v, "channel")
	if ch == "Any" {
		ch = ""
	}
	data := crmJSON(map[string]interface{}{"code": code, "discount_type": dt, "discount_value": dv, "expiry_date": exp.Format("2006-01-02"),
		"max_uses": 1, "used_count": 0, "customer_id": customerID, "status": "Active", "channel": firstNonEmpty(ch, "Any"),
		"campaign_id": campaignID, "campaign_run": runID})
	_, err = tx.Exec(fmt.Sprintf(`INSERT INTO %s.documents (id, doctype, data, status, created_by) VALUES ($1, 'Voucher', $2, 'Active', 'campaign')`, schema),
		NewDocID("VCH"), data)
	return code, value, expiry, err
}

// SetCRMCampaignRunState pauses, resumes or stops a run. Stopping cancels
// every message not yet handed over.
func SetCRMCampaignRunState(tenantID, runID, action, actor string) (*CRMCampaignRun, error) {
	schema, err := crmSchema(tenantID)
	if err != nil {
		return nil, err
	}
	var cur, campaignID string
	if err := db.DB.QueryRow(fmt.Sprintf(`SELECT status, campaign_id FROM %s.crm_campaign_runs WHERE id = $1`, schema), runID).Scan(&cur, &campaignID); err != nil {
		return nil, crmNotFound("campaign run", runID)
	}
	next := map[string]map[string]string{
		"pause":  {"Running": "Paused", "Scheduled": "Paused"},
		"resume": {"Paused": "Running"},
		"stop":   {"Running": "Stopped", "Paused": "Stopped", "Scheduled": "Stopped"},
	}[action][cur]
	if next == "" {
		return nil, crmErr(CRMCodeCampaign, "a %s run cannot be %sd", strings.ToLower(cur), action)
	}
	if action == "resume" {
		var started sql.NullTime
		_ = db.DB.QueryRow(fmt.Sprintf(`SELECT started_at FROM %s.crm_campaign_runs WHERE id = $1`, schema), runID).Scan(&started)
		if !started.Valid {
			next = "Scheduled"
		}
	}
	tx, err := db.DB.Begin()
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	if _, err := tx.Exec(fmt.Sprintf(`UPDATE %s.crm_campaign_runs SET status = $2::text, finished_at = CASE WHEN $2::text = 'Stopped' THEN LOCALTIMESTAMP ELSE finished_at END WHERE id = $1`, schema), runID, next); err != nil {
		return nil, err
	}
	if next == "Stopped" {
		if _, err := tx.Exec(fmt.Sprintf(`UPDATE %s.crm_sends SET status = 'Cancelled', reason_code = 'CAMPAIGN_STOPPED', reason_detail = 'The campaign was stopped by '||$2,
			decided_at = LOCALTIMESTAMP, final_at = LOCALTIMESTAMP WHERE run_kind = 'campaign' AND run_id = $1 AND status IN ('Queued', 'Deferred')`, schema), runID, actor); err != nil {
			return nil, err
		}
	}
	docStatus := map[string]string{"Paused": "Paused", "Running": "Running", "Stopped": "Stopped", "Scheduled": "Approved"}[next]
	if _, err := tx.Exec(fmt.Sprintf(`UPDATE %s.documents SET status = $2::text, data = data || jsonb_build_object('status', $2::text), updated_at = CURRENT_TIMESTAMP WHERE id = $1 AND doctype = 'Campaign'`, schema),
		campaignID, docStatus); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	LogAuditEvent(tenantID, actor, "CRM_CAMPAIGN_"+strings.ToUpper(action), "SUCCESS", "run "+runID)
	runs, err := crmCampaignRuns(schema, campaignID)
	if err != nil {
		return nil, err
	}
	for i := range runs {
		if runs[i].ID == runID {
			return &runs[i], nil
		}
	}
	return nil, crmNotFound("campaign run", runID)
}

func crmCampaignRuns(schema, campaignID string) ([]CRMCampaignRun, error) {
	rows, err := db.DB.Query(fmt.Sprintf(`SELECT id, status, test, scheduled_at, started_at, finished_at, channel, COALESCE(template_id, ''), COALESCE(template_version, 0),
		COALESCE(snapshot_id, ''), budget, holdout_pct, counts FROM %s.crm_campaign_runs WHERE campaign_id = $1 ORDER BY created_at DESC LIMIT 20`, schema), campaignID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []CRMCampaignRun{}
	for rows.Next() {
		var r CRMCampaignRun
		var started, finished sql.NullTime
		var budget sql.NullFloat64
		var counts []byte
		if err := rows.Scan(&r.ID, &r.Status, &r.Test, &r.ScheduledAt, &started, &finished, &r.Channel, &r.TemplateID, &r.TemplateVersion, &r.SnapshotID, &budget, &r.HoldoutPct, &counts); err != nil {
			return nil, err
		}
		if started.Valid {
			r.StartedAt = &started.Time
		}
		if finished.Valid {
			r.FinishedAt = &finished.Time
		}
		if budget.Valid {
			r.Budget = &budget.Float64
		}
		r.Counts = map[string]int{}
		var raw map[string]interface{}
		_ = json.Unmarshal(counts, &raw)
		for k, v := range raw {
			if n, ok := v.(float64); ok {
				r.Counts[k] = int(n)
			}
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// GetCRMCampaign returns a campaign with its runs and approval history.
func GetCRMCampaign(tenantID, id string) (*CRMCampaign, error) {
	schema, err := crmSchema(tenantID)
	if err != nil {
		return nil, err
	}
	d, status, err := crmLoadCampaignDoc(schema, id)
	if err != nil {
		return nil, err
	}
	c := &CRMCampaign{ID: id, Status: status, Data: d, Legacy: strField(d, "kind") != crmCampaignKind}
	if c.Runs, err = crmCampaignRuns(schema, id); err != nil {
		return nil, err
	}
	if log, err := ListApprovalLog(tenantID, "Campaign", id); err == nil {
		c.Approval = log
	}
	return c, nil
}

// ListCRMCampaigns lists campaigns, newest first (older trigger campaigns
// included, marked legacy).
func ListCRMCampaigns(tenantID string) ([]CRMCampaign, error) {
	schema, err := crmSchema(tenantID)
	if err != nil {
		return nil, err
	}
	rows, err := db.DB.Query(fmt.Sprintf(`SELECT id FROM %s.documents WHERE doctype = 'Campaign' AND deleted_at IS NULL ORDER BY updated_at DESC LIMIT 200`, schema))
	if err != nil {
		return nil, err
	}
	var ids []string
	for rows.Next() {
		var id string
		if rows.Scan(&id) == nil {
			ids = append(ids, id)
		}
	}
	rows.Close()
	out := []CRMCampaign{}
	for _, id := range ids {
		c, err := GetCRMCampaign(tenantID, id)
		if err != nil {
			continue
		}
		c.Approval = nil
		out = append(out, *c)
	}
	return out, nil
}

// CRMCampaignPreview is the pre-send waterfall: who matches, who is held out,
// who cannot be reached and why, and what it should cost.
type CRMCampaignPreview struct {
	Segment       *CRMSegmentCount `json:"segment"`
	HoldoutPct    int              `json:"holdout_pct"`
	HeldOut       int              `json:"held_out_estimate"`
	Final         int              `json:"final"`
	EstimateEach  float64          `json:"estimate_each"`
	EstimateTotal float64          `json:"estimate_total"`
	Budget        float64          `json:"budget"`
	OverBudget    bool             `json:"over_budget"`
	Problems      []string         `json:"problems"`
	SMSParts      int              `json:"sms_parts,omitempty"`
}

// PreviewCRMCampaign counts a campaign before it is sent.
func PreviewCRMCampaign(tenantID, role, id string) (*CRMCampaignPreview, error) {
	schema, err := crmSchema(tenantID)
	if err != nil {
		return nil, err
	}
	d, _, err := crmLoadCampaignDoc(schema, id)
	if err != nil {
		return nil, err
	}
	out := &CRMCampaignPreview{Problems: crmCampaignProblems(schema, d), HoldoutPct: int(numFromInterface(d["holdout_pct"])), Budget: numFromInterface(d["budget"])}
	if out.Problems == nil {
		out.Problems = []string{}
	}
	channel := strField(d, "channel")
	if seg, err := crmGetSegment(schema, strField(d, "segment_id"), 0); err == nil {
		if out.Segment, err = CountCRMSegment(tenantID, role, seg.Definition, channel); err != nil {
			return nil, err
		}
		out.HeldOut = int(math.Round(float64(out.Segment.Final) * float64(out.HoldoutPct) / 100))
		out.Final = out.Segment.Final - out.HeldOut
	}
	parts := 1
	if t, err := crmGetTemplate(schema, strField(d, "template_id")); err == nil && t.Current != nil {
		parts = t.Current.SMSParts
		if channel == CRMChannelSMS {
			out.SMSParts = parts
		}
	}
	out.EstimateEach = round2(crmEstimateCost(schema, channel, parts))
	out.EstimateTotal = round2(out.EstimateEach * float64(out.Final))
	out.OverBudget = out.Budget > 0 && out.EstimateTotal > out.Budget
	return out, nil
}

// SendCRMCampaignTest sends the campaign's message to one customer whose
// number or email is on the test allowlist.
func SendCRMCampaignTest(tenantID, id, customerID, actor string) (*CRMSend, error) {
	schema, err := crmSchema(tenantID)
	if err != nil {
		return nil, err
	}
	d, _, err := crmLoadCampaignDoc(schema, id)
	if err != nil {
		return nil, err
	}
	return SendCRMStaffMessage(tenantID, actor, CRMStaffMessage{CustomerID: customerID, Channel: strField(d, "channel"), Purpose: CRMPurposeMarketing,
		TemplateID: strField(d, "template_id"), Test: true,
		Vars: map[string]string{"voucher.code": "TESTCODE", "voucher.value": "10% off", "voucher.expiry": time.Now().AddDate(0, 0, 30).Format("2 Jan")}})
}

// --- results (101.3.3) and lift (101.3.5) ---------------------------------------------

// CRMCampaignResults are a run's numbers. A number the channel does not
// report (email delivery, SMS reads) is nil - shown blank, never 0.
type CRMCampaignResults struct {
	RunID           string           `json:"run_id"`
	Channel         string           `json:"channel"`
	Matched         int              `json:"matched"`
	HeldOut         int              `json:"held_out"`
	Suppressed      map[string]int   `json:"suppressed"`
	Sent            int              `json:"sent"`
	Accepted        int              `json:"accepted"`
	Delivered       *int             `json:"delivered"`
	Read            *int             `json:"read"`
	Failed          int              `json:"failed"`
	Unknown         int              `json:"unknown"`
	Waiting         int              `json:"waiting"`
	VouchersIssued  int              `json:"vouchers_issued"`
	VouchersUsed    int              `json:"vouchers_used"`
	BillsCredited   int              `json:"bills_credited"`
	SalesCredited   float64          `json:"sales_credited"`
	CostEstimate    float64          `json:"cost_estimate"`
	CostActual      *float64         `json:"cost_actual"`
	AttributionDays int              `json:"attribution_days"`
	Lift            *CRMCampaignLift `json:"lift,omitempty"`
	TimeZone        string           `json:"time_zone"`
	DateBasis       string           `json:"date_basis"`
}

// CRMCampaignLift compares everyone assigned to the message with the hold-out.
type CRMCampaignLift struct {
	Treated       int      `json:"treated"`
	TreatedBuyers int      `json:"treated_buyers"`
	Control       int      `json:"control"`
	ControlBuyers int      `json:"control_buyers"`
	TreatedRate   float64  `json:"treated_rate"`
	ControlRate   float64  `json:"control_rate"`
	Lift          *float64 `json:"lift,omitempty"`
	Verdict       string   `json:"verdict"`
}

// GetCRMCampaignResults computes a run's results now.
func GetCRMCampaignResults(tenantID, runID string) (*CRMCampaignResults, error) {
	schema, err := crmSchema(tenantID)
	if err != nil {
		return nil, err
	}
	r := &CRMCampaignResults{RunID: runID, Suppressed: map[string]int{}, TimeZone: crmZoneName(schema),
		DateBasis: "bill date; a bill is credited to the last offer the customer got in the days before it"}
	var counts []byte
	var started sql.NullTime
	var goalDays int
	var campaignID string
	if err := db.DB.QueryRow(fmt.Sprintf(`SELECT channel, counts, started_at, goal_days, campaign_id FROM %s.crm_campaign_runs WHERE id = $1`, schema), runID).
		Scan(&r.Channel, &counts, &started, &goalDays, &campaignID); err != nil {
		return nil, crmNotFound("campaign run", runID)
	}
	var c map[string]interface{}
	_ = json.Unmarshal(counts, &c)
	r.Matched = int(numFromInterface(c["matched"]))
	r.AttributionDays = GetSettingIntForSchema(schema, "crm.attribution_days")
	rows, err := db.DB.Query(fmt.Sprintf(`SELECT status, COALESCE(reason_code, ''), COUNT(*), COALESCE(SUM(cost_estimate), 0), SUM(cost_actual), COUNT(cost_actual)
		FROM %s.crm_sends WHERE run_kind = 'campaign' AND run_id = $1 GROUP BY 1, 2`, schema), runID)
	if err != nil {
		return nil, err
	}
	var delivered, read, actualRows int
	var actual float64
	for rows.Next() {
		var st, reason string
		var n, withCost int
		var est float64
		var act sql.NullFloat64
		if err := rows.Scan(&st, &reason, &n, &est, &act, &withCost); err != nil {
			rows.Close()
			return nil, err
		}
		switch st {
		case crmSendHeldOut:
			r.HeldOut += n
		case crmSendSuppress, crmSendExpired, crmSendCancelled:
			r.Suppressed[firstNonEmpty(reason, st)] += n
		case crmSendQueued, crmSendDeferred:
			r.Waiting += n
		case crmSendFailed:
			r.Failed += n
			r.Sent += n
		case crmSendUnknown:
			r.Unknown += n
			r.Sent += n
		case crmSendSending:
			r.Sent += n
		case crmSendAccepted, crmSendSimulated:
			r.Sent += n
			r.Accepted += n
		case crmSendDelivered:
			r.Sent += n
			r.Accepted += n
			delivered += n
		case crmSendRead:
			r.Sent += n
			r.Accepted += n
			delivered += n
			read += n
		}
		if st != crmSendFailed || reason != "" {
			r.CostEstimate += est
		}
		if act.Valid {
			actual += act.Float64
			actualRows += withCost
		}
	}
	rows.Close()
	if r.Channel != CRMChannelEmail && (delivered > 0 || r.Accepted == 0) {
		r.Delivered = &delivered
	}
	if r.Channel == CRMChannelWhatsApp {
		r.Read = &read
	}
	r.CostEstimate = round2(r.CostEstimate)
	if actualRows > 0 {
		a := round2(actual)
		r.CostActual = &a
	}
	_ = db.DB.QueryRow(fmt.Sprintf(`SELECT COUNT(*), COUNT(*) FILTER (WHERE COALESCE((data->>'used_count')::int, 0) > 0) FROM %s.documents WHERE doctype = 'Voucher' AND data->>'campaign_run' = $1`, schema),
		runID).Scan(&r.VouchersIssued, &r.VouchersUsed)
	// D10: the last marketing message a customer got within the attribution
	// window before a bill gets the bill, once; returns reduce it.
	err = db.DB.QueryRow(fmt.Sprintf(`
		WITH mine AS (
		  SELECT customer_id, sent_at FROM %[1]s.crm_sends WHERE run_kind = 'campaign' AND run_id = $1 AND status IN ('Accepted', 'Delivered', 'Read', 'Unknown', 'Simulated')
		), bills AS (
		  SELECT b.id, b.data->>'customer_id' AS cid, b.created_at, %[2]s AS amount
		    FROM %[1]s.documents b JOIN mine m ON m.customer_id = b.data->>'customer_id'
		   WHERE b.doctype = 'POSCart' AND b.status = 'Paid' AND b.created_at > m.sent_at AND b.created_at <= m.sent_at + make_interval(days => $2)
		), credited AS (
		  SELECT DISTINCT bl.id, bl.amount FROM bills bl
		   WHERE (SELECT s.run_id FROM %[1]s.crm_sends s WHERE s.customer_id = bl.cid AND s.purpose = 'marketing' AND s.status IN ('Accepted', 'Delivered', 'Read', 'Unknown', 'Simulated')
		            AND s.sent_at < bl.created_at AND s.sent_at >= bl.created_at - make_interval(days => $2) ORDER BY s.sent_at DESC LIMIT 1) = $1
		)
		SELECT COUNT(*), COALESCE(SUM(c.amount - COALESCE((SELECT SUM(%[3]s) FROM %[1]s.documents r WHERE r.doctype = 'ReturnRequest'
		       AND r.data->>'original_order_id' = c.id AND r.status IN ('Approved', 'Received', 'QC Complete', 'Closed')), 0)), 0) FROM credited c`,
		schema, crmBillAmountSQL("b"), crmNumSQL("r.data->>'total_refund_eligible'")), runID, r.AttributionDays).Scan(&r.BillsCredited, &r.SalesCredited)
	if err != nil {
		return nil, err
	}
	r.SalesCredited = round2(r.SalesCredited)
	if r.HeldOut > 0 && started.Valid {
		if goalDays <= 0 {
			goalDays = r.AttributionDays
		}
		r.Lift, err = crmCampaignLift(schema, runID, started.Time, goalDays)
		if err != nil {
			return nil, err
		}
	}
	return r, nil
}

func crmCampaignLift(schema, runID string, started time.Time, goalDays int) (*CRMCampaignLift, error) {
	l := &CRMCampaignLift{}
	err := db.DB.QueryRow(fmt.Sprintf(`SELECT
		COUNT(*) FILTER (WHERE s.status <> 'HeldOut'), COUNT(*) FILTER (WHERE s.status <> 'HeldOut' AND b.bought),
		COUNT(*) FILTER (WHERE s.status = 'HeldOut'), COUNT(*) FILTER (WHERE s.status = 'HeldOut' AND b.bought)
		FROM %[1]s.crm_sends s, LATERAL (SELECT EXISTS(SELECT 1 FROM %[1]s.documents d WHERE d.doctype = 'POSCart' AND d.status = 'Paid' AND d.data->>'customer_id' = s.customer_id
		  AND d.created_at >= $2 AND d.created_at < $2 + make_interval(days => $3)) AS bought) b
		WHERE s.run_kind = 'campaign' AND s.run_id = $1`, schema), runID, started, goalDays).Scan(&l.Treated, &l.TreatedBuyers, &l.Control, &l.ControlBuyers)
	if err != nil {
		return nil, err
	}
	if l.Treated > 0 {
		l.TreatedRate = round2(100 * float64(l.TreatedBuyers) / float64(l.Treated))
	}
	if l.Control > 0 {
		l.ControlRate = round2(100 * float64(l.ControlBuyers) / float64(l.Control))
	}
	switch {
	case l.Treated < 30 || l.Control < 30 || l.TreatedBuyers+l.ControlBuyers < 10:
		l.Verdict = "Not enough data yet"
	case l.ControlRate == 0:
		l.Verdict = "The hold-out bought nothing - lift cannot be computed"
	default:
		v := round2((l.TreatedRate - l.ControlRate) / l.ControlRate * 100)
		l.Lift = &v
		l.Verdict = fmt.Sprintf("%.1f%% more buyers among those messaged than in the hold-out", v)
		if v < 0 {
			l.Verdict = fmt.Sprintf("%.1f%% fewer buyers among those messaged than in the hold-out", -v)
		}
	}
	return l, nil
}
