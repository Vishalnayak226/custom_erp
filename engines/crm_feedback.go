package engines

import (
	"custom_erp/db"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"
)

// Stage 101.5.1 - feedback after a bill (the blueprint's M17). A survey asks
// one question - NPS (0-10) or a rating (1-5) - by a link sent after a bill,
// at most once per bill and not again to the same customer within its
// cool-down. Changing the question makes a new survey version, so answers
// are always read against the question they answered. NPS is the share of
// promoters (9-10) minus the share of detractors (0-6), shown with the
// number of answers. A low score opens one Service Ticket for the store.

// CRMSurvey is a survey and its current question.
type CRMSurvey struct {
	ID             string    `json:"id"`
	Name           string    `json:"name"`
	CurrentVersion int       `json:"current_version"`
	Question       string    `json:"question"`
	Scale          string    `json:"scale"`
	Channel        string    `json:"channel"`
	TemplateID     string    `json:"template_id,omitempty"`
	Enabled        bool      `json:"enabled"`
	DelayHours     int       `json:"delay_hours"`
	CooldownDays   int       `json:"cooldown_days"`
	LowScore       int       `json:"low_score"`
	UpdatedAt      time.Time `json:"updated_at"`
}

// SaveCRMSurvey creates or edits a survey; a changed question or scale is a
// new version.
func SaveCRMSurvey(tenantID, id string, in CRMSurvey, actor string) (*CRMSurvey, error) {
	schema, err := crmSchema(tenantID)
	if err != nil {
		return nil, err
	}
	in.Name, in.Question = strings.TrimSpace(in.Name), strings.TrimSpace(in.Question)
	if in.Name == "" || in.Question == "" {
		return nil, crmErr(CRMCodeFeedback, "a survey needs a name and a question")
	}
	if in.Scale != "rating" {
		in.Scale = "nps"
	}
	if in.Channel = crmChannel(in.Channel); in.Channel == "" {
		in.Channel = CRMChannelWhatsApp
	}
	if in.DelayHours < 0 || in.DelayHours > 168 {
		return nil, crmErr(CRMCodeFeedback, "send it 0 to 168 hours after the bill")
	}
	if in.CooldownDays < 0 {
		in.CooldownDays = 0
	}
	maxScore := 10
	if in.Scale == "rating" {
		maxScore = 5
	}
	if in.LowScore <= 0 || in.LowScore > maxScore {
		in.LowScore = map[string]int{"nps": 6, "rating": 2}[in.Scale]
	}
	if in.TemplateID != "" {
		t, err := crmGetTemplate(schema, in.TemplateID)
		if err != nil {
			return nil, err
		}
		if t.Channel != in.Channel {
			return nil, crmErr(CRMCodeFeedback, "the template is for %s, the survey sends by %s", t.Channel, in.Channel)
		}
		if t.Current == nil || !strings.Contains(t.Current.Body, "link.feedback") {
			return nil, crmErr(CRMCodeFeedback, "the template must contain {{link.feedback}} so the customer can answer")
		}
	}
	tx, err := db.DB.Begin()
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	if id == "" {
		id = NewDocID("SRV")
		if _, err := tx.Exec(fmt.Sprintf(`INSERT INTO %s.crm_surveys (id, name, current_version, channel, template_id, enabled, delay_hours, cooldown_days, low_score, created_by)
			VALUES ($1, $2, 1, $3, NULLIF($4, ''), $5, $6, $7, $8, $9)`, schema), id, in.Name, in.Channel, in.TemplateID, in.Enabled, in.DelayHours, in.CooldownDays, in.LowScore, actor); err != nil {
			return nil, err
		}
		if _, err := tx.Exec(fmt.Sprintf(`INSERT INTO %s.crm_survey_versions (survey_id, version, question, scale, created_by) VALUES ($1, 1, $2, $3, $4)`, schema), id, in.Question, in.Scale, actor); err != nil {
			return nil, err
		}
	} else {
		var cur int
		var q, sc string
		err := tx.QueryRow(fmt.Sprintf(`SELECT s.current_version, v.question, v.scale FROM %s.crm_surveys s JOIN %s.crm_survey_versions v ON v.survey_id = s.id AND v.version = s.current_version
			WHERE s.id = $1 FOR UPDATE OF s`, schema, schema), id).Scan(&cur, &q, &sc)
		if errors.Is(err, sql.ErrNoRows) {
			return nil, crmNotFound("survey", id)
		}
		if err != nil {
			return nil, err
		}
		if q != in.Question || sc != in.Scale {
			cur++
			if _, err := tx.Exec(fmt.Sprintf(`INSERT INTO %s.crm_survey_versions (survey_id, version, question, scale, created_by) VALUES ($1, $2, $3, $4, $5)`, schema), id, cur, in.Question, in.Scale, actor); err != nil {
				return nil, err
			}
		}
		if _, err := tx.Exec(fmt.Sprintf(`UPDATE %s.crm_surveys SET name = $2, current_version = $3, channel = $4, template_id = NULLIF($5, ''), enabled = $6, delay_hours = $7,
			cooldown_days = $8, low_score = $9, updated_at = NOW() WHERE id = $1`, schema), id, in.Name, cur, in.Channel, in.TemplateID, in.Enabled, in.DelayHours, in.CooldownDays, in.LowScore); err != nil {
			return nil, err
		}
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return crmGetSurvey(schema, id)
}

func crmGetSurvey(schema, id string) (*CRMSurvey, error) {
	var s CRMSurvey
	var tid sql.NullString
	err := db.DB.QueryRow(fmt.Sprintf(`SELECT s.id, s.name, s.current_version, v.question, v.scale, s.channel, s.template_id, s.enabled, s.delay_hours, s.cooldown_days, s.low_score, s.updated_at
		FROM %s.crm_surveys s JOIN %s.crm_survey_versions v ON v.survey_id = s.id AND v.version = s.current_version WHERE s.id = $1`, schema, schema), id).
		Scan(&s.ID, &s.Name, &s.CurrentVersion, &s.Question, &s.Scale, &s.Channel, &tid, &s.Enabled, &s.DelayHours, &s.CooldownDays, &s.LowScore, &s.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, crmNotFound("survey", id)
	}
	s.TemplateID = tid.String
	return &s, err
}

// ListCRMSurveys lists surveys.
func ListCRMSurveys(tenantID string) ([]CRMSurvey, error) {
	schema, err := crmSchema(tenantID)
	if err != nil {
		return nil, err
	}
	rows, err := db.DB.Query(fmt.Sprintf(`SELECT id FROM %s.crm_surveys ORDER BY enabled DESC, updated_at DESC`, schema))
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
	out := []CRMSurvey{}
	for _, id := range ids {
		if s, err := crmGetSurvey(schema, id); err == nil {
			out = append(out, *s)
		}
	}
	return out, nil
}

func init() {
	registerCRMEventHandler(CRMEventBillPaid, func(schema string, ev crmEvent) {
		var id string
		if err := db.DB.QueryRow(fmt.Sprintf(`SELECT id FROM %s.crm_surveys WHERE enabled ORDER BY updated_at DESC LIMIT 1`, schema)).Scan(&id); err != nil {
			return
		}
		store, _ := ev.Payload["store"].(string)
		if _, err := crmRequestFeedback(schema, id, ev.CustomerID, ev.RefID, store); err != nil {
			LogSystemError("", "", "WARN", "crm_feedback", fmt.Sprintf("feedback for bill %s not requested: %v", ev.RefID, err), "")
		}
	})
}

// crmRequestFeedback asks for feedback on one bill, once, unless the
// customer was asked within the survey's cool-down.
func crmRequestFeedback(schema, surveyID, customerID, billID, store string) (string, error) {
	s, err := crmGetSurvey(schema, surveyID)
	if err != nil {
		return "", err
	}
	if s.CooldownDays > 0 {
		var recent bool
		_ = db.DB.QueryRow(fmt.Sprintf(`SELECT EXISTS(SELECT 1 FROM %s.crm_feedback WHERE customer_id = $1 AND created_at > LOCALTIMESTAMP - make_interval(days => $2))`, schema),
			customerID, s.CooldownDays).Scan(&recent)
		if recent {
			return "", nil
		}
	}
	token, err := crmToken(12)
	if err != nil {
		return "", err
	}
	tx, err := db.DB.Begin()
	if err != nil {
		return "", err
	}
	defer tx.Rollback()
	id := NewDocID("FBK")
	res, err := tx.Exec(fmt.Sprintf(`INSERT INTO %s.crm_feedback (id, token_hash, survey_id, survey_version, customer_id, bill_id, store) VALUES ($1, $2, $3, $4, $5, $6, NULLIF($7, ''))
		ON CONFLICT (bill_id) DO NOTHING`, schema), id, crmTokenHash(token), s.ID, s.CurrentVersion, customerID, billID, store)
	if err != nil {
		return "", err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return "", nil // this bill was already asked about
	}
	vars := map[string]string{"link.feedback": crmPublicLink(schema, "/f/", token), "bill.number": crmBillNumber(schema, billID)}
	body := ""
	if s.TemplateID == "" {
		body = fmt.Sprintf("Thank you for shopping with %s. How did we do? %s", firstNonEmpty(crmBusinessName(schema), "us"), vars["link.feedback"])
	}
	sendID, _, err := crmQueueSend(tx, schema, CRMSendRequest{RunKind: "feedback", RunID: id, CustomerID: customerID, Channel: s.Channel, Purpose: CRMPurposeService,
		TemplateID: s.TemplateID, Body: body, Vars: vars, DelaySeconds: s.DelayHours * 3600, ExpiresInHours: s.DelayHours + 72, CreatedBy: "feedback"})
	if err != nil {
		return "", err
	}
	if _, err := tx.Exec(fmt.Sprintf(`UPDATE %s.crm_feedback SET send_id = $2 WHERE id = $1`, schema), id, sendID); err != nil {
		return "", err
	}
	return id, tx.Commit()
}

func crmBillNumber(schema, billID string) string {
	var n string
	if err := db.DB.QueryRow(fmt.Sprintf(`SELECT COALESCE(NULLIF(data->>'bill_number', ''), id) FROM %s.documents WHERE id = $1`, schema), billID).Scan(&n); err != nil {
		return billID
	}
	return n
}

// CRMFeedbackForm is what the customer's feedback page shows.
type CRMFeedbackForm struct {
	Business string `json:"business"`
	Question string `json:"question"`
	Scale    string `json:"scale"`
	Answered bool   `json:"answered"`
	Score    *int   `json:"score,omitempty"`
}

func crmFeedbackByToken(schema, token string) (id, customerID, billID, store string, surveyID string, version int, score sql.NullInt64, err error) {
	token = strings.TrimSpace(token)
	if len(token) < 10 || len(token) > 64 {
		err = crmErr(CRMCodeLink, "this link is not valid")
		return
	}
	var st sql.NullString
	err = db.DB.QueryRow(fmt.Sprintf(`SELECT id, customer_id, bill_id, store, survey_id, survey_version, score FROM %s.crm_feedback WHERE token_hash = $1`, schema), crmTokenHash(token)).
		Scan(&id, &customerID, &billID, &st, &surveyID, &version, &score)
	if errors.Is(err, sql.ErrNoRows) {
		err = crmErr(CRMCodeLink, "this link is not valid or has expired")
	}
	store = st.String
	return
}

// GetCRMFeedbackForm reads the form behind a feedback link.
func GetCRMFeedbackForm(tenantID, token string) (*CRMFeedbackForm, error) {
	schema, err := crmSchema(tenantID)
	if err != nil {
		return nil, crmErr(CRMCodeLink, "this link is not valid")
	}
	_, _, _, _, surveyID, version, score, err := crmFeedbackByToken(schema, token)
	if err != nil {
		return nil, err
	}
	f := &CRMFeedbackForm{Business: crmBusinessName(schema), Answered: score.Valid}
	if score.Valid {
		v := int(score.Int64)
		f.Score = &v
	}
	if err := db.DB.QueryRow(fmt.Sprintf(`SELECT question, scale FROM %s.crm_survey_versions WHERE survey_id = $1 AND version = $2`, schema), surveyID, version).Scan(&f.Question, &f.Scale); err != nil {
		return nil, crmErr(CRMCodeLink, "this survey is no longer available")
	}
	return f, nil
}

// SubmitCRMFeedback records the answer once; a second answer is thanked and
// ignored. A low score opens one Service Ticket.
func SubmitCRMFeedback(tenantID, token string, score int, comment string) (*CRMFeedbackForm, error) {
	schema, err := crmSchema(tenantID)
	if err != nil {
		return nil, crmErr(CRMCodeLink, "this link is not valid")
	}
	id, customerID, billID, store, surveyID, version, prior, err := crmFeedbackByToken(schema, token)
	if err != nil {
		return nil, err
	}
	if prior.Valid {
		return GetCRMFeedbackForm(tenantID, token)
	}
	var scale string
	var low int
	if err := db.DB.QueryRow(fmt.Sprintf(`SELECT v.scale, s.low_score FROM %s.crm_survey_versions v JOIN %s.crm_surveys s ON s.id = v.survey_id WHERE v.survey_id = $1 AND v.version = $2`, schema, schema),
		surveyID, version).Scan(&scale, &low); err != nil {
		return nil, crmErr(CRMCodeLink, "this survey is no longer available")
	}
	lo, hi := 0, 10
	if scale == "rating" {
		lo, hi = 1, 5
	}
	if score < lo || score > hi {
		return nil, crmErr(CRMCodeFeedback, "choose a score from %d to %d", lo, hi)
	}
	comment = truncateRunes(strings.TrimSpace(comment), 1000)
	res, err := db.DB.Exec(fmt.Sprintf(`UPDATE %s.crm_feedback SET score = $2, comment = NULLIF($3, ''), responded_at = LOCALTIMESTAMP WHERE id = $1 AND score IS NULL`, schema), id, score, comment)
	if err != nil {
		return nil, err
	}
	if n, _ := res.RowsAffected(); n == 1 && score <= low {
		tid, _ := tenantIDForSchema(schema)
		desc := fmt.Sprintf("Feedback score %d of %d on bill %s.", score, hi, crmBillNumber(schema, billID))
		if comment != "" {
			desc += " The customer wrote: " + comment
		}
		ticket, err := CreateCRMTicket(tid, "feedback", CRMTicketInput{CustomerID: customerID, BillID: billID, Store: store, Channel: "Feedback", Description: desc, Priority: "High"})
		if err == nil {
			_, _ = db.DB.Exec(fmt.Sprintf(`UPDATE %s.crm_feedback SET ticket_id = $2 WHERE id = $1`, schema), id, ticket)
		}
		_ = RecordCRMEvent(schema, customerID, CRMEventFeedbackLow, id, map[string]interface{}{"score": score, "bill": billID})
	}
	return GetCRMFeedbackForm(tenantID, token)
}

// CRMFeedbackSummary is a survey's results for a period.
type CRMFeedbackSummary struct {
	SurveyID string                `json:"survey_id"`
	Versions []CRMFeedbackVersion  `json:"versions"`
	Recent   []CRMFeedbackResponse `json:"recent"`
	From     string                `json:"from,omitempty"`
	To       string                `json:"to,omitempty"`
}

type CRMFeedbackVersion struct {
	Version    int      `json:"version"`
	Question   string   `json:"question"`
	Scale      string   `json:"scale"`
	Asked      int      `json:"asked"`
	Responses  int      `json:"responses"`
	Promoters  int      `json:"promoters"`
	Passives   int      `json:"passives"`
	Detractors int      `json:"detractors"`
	NPS        *float64 `json:"nps,omitempty"`
	Average    *float64 `json:"average,omitempty"`
}

type CRMFeedbackResponse struct {
	At       time.Time `json:"at"`
	Customer string    `json:"customer"`
	Store    string    `json:"store,omitempty"`
	Score    int       `json:"score"`
	Comment  string    `json:"comment,omitempty"`
	TicketID string    `json:"ticket_id,omitempty"`
}

// GetCRMFeedbackSummary reports NPS / ratings per survey version.
func GetCRMFeedbackSummary(tenantID, surveyID, from, to string) (*CRMFeedbackSummary, error) {
	schema, err := crmSchema(tenantID)
	if err != nil {
		return nil, err
	}
	out := &CRMFeedbackSummary{SurveyID: surveyID, From: from, To: to, Versions: []CRMFeedbackVersion{}, Recent: []CRMFeedbackResponse{}}
	dates := ""
	args := []interface{}{surveyID}
	if from != "" {
		args = append(args, from)
		dates += fmt.Sprintf(" AND f.created_at >= public.erp_day_start($%d::date)", len(args))
	}
	if to != "" {
		args = append(args, to)
		dates += fmt.Sprintf(" AND f.created_at < public.erp_day_start($%d::date + 1)", len(args))
	}
	where := "f.survey_id = $1" + dates
	rows, err := db.DB.Query(fmt.Sprintf(`SELECT v.version, v.question, v.scale, COUNT(f.id), COUNT(f.score),
		COUNT(f.id) FILTER (WHERE f.score >= 9), COUNT(f.id) FILTER (WHERE f.score BETWEEN 7 AND 8), COUNT(f.id) FILTER (WHERE f.score <= 6), AVG(f.score)
		FROM %s.crm_survey_versions v LEFT JOIN %s.crm_feedback f ON f.survey_id = v.survey_id AND f.survey_version = v.version%s
		WHERE v.survey_id = $1 GROUP BY v.version, v.question, v.scale ORDER BY v.version DESC`, schema, schema, dates), args...)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var v CRMFeedbackVersion
		var avg sql.NullFloat64
		if err := rows.Scan(&v.Version, &v.Question, &v.Scale, &v.Asked, &v.Responses, &v.Promoters, &v.Passives, &v.Detractors, &avg); err != nil {
			rows.Close()
			return nil, err
		}
		if v.Responses > 0 {
			if v.Scale == "nps" {
				n := round2(100*float64(v.Promoters)/float64(v.Responses) - 100*float64(v.Detractors)/float64(v.Responses))
				v.NPS = &n
			} else {
				v.Promoters, v.Passives, v.Detractors = 0, 0, 0
			}
			a := round2(avg.Float64)
			v.Average = &a
		}
		out.Versions = append(out.Versions, v)
	}
	rows.Close()
	rows, err = db.DB.Query(fmt.Sprintf(`SELECT f.responded_at, COALESCE(c.data->>'name', ''), COALESCE(f.store, ''), f.score, COALESCE(f.comment, ''), COALESCE(f.ticket_id, '')
		FROM %s.crm_feedback f LEFT JOIN %s.documents c ON c.id = f.customer_id AND c.doctype = 'Customer'
		WHERE %s AND f.score IS NOT NULL ORDER BY f.responded_at DESC LIMIT 50`, schema, schema, where), args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var r CRMFeedbackResponse
		if err := rows.Scan(&r.At, &r.Customer, &r.Store, &r.Score, &r.Comment, &r.TicketID); err != nil {
			return nil, err
		}
		out.Recent = append(out.Recent, r)
	}
	return out, rows.Err()
}
