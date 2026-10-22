package engines

import (
	"custom_erp/db"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"
)

// Stage 101.8 - consent ledger, preference page and unsubscribe, erasure that
// sticks (the blueprint's M07, M35).
//
// A reachable phone is not permission. Every grant, refusal or withdrawal is
// a new crm_consent_ledger row - who, when, how, which notice - and rows are
// never edited (nothing in this package updates the table; erasure is the one
// delete, D12). The Customer's consent_sms / consent_whatsapp / consent_email
// ticks are the summary of the latest marketing row per channel, kept so the
// till card, imports and the older campaign code read the same answer.
//
// Strength order, strongest first (crmReachability):
//
//	erased / suppressed contact -> stop everything (channel All)
//	-> channel opt-out (purpose all) -> topic (channel x purpose) -> consent
//
// No row means not reachable for marketing (D7); service messages are allowed
// unless the customer said no to them (D6).

// Ledger vocabulary.
const (
	CRMConsentGranted   = "granted"
	CRMConsentDenied    = "denied"
	CRMConsentWithdrawn = "withdrawn"

	crmChannelAll = "All"
	crmPurposeAll = "all"
)

var crmConsentSources = map[string]string{
	"form":            "Recorded by staff on the customer page",
	"till":            "Recorded at the till",
	"store_app":       "Recorded in the store app",
	"import":          "Imported",
	"preference_page": "The customer, on their preference page",
	"unsubscribe":     "The customer, from the unsubscribe link",
	"stop_reply":      "The customer replied STOP",
	"privacy_request": "A privacy request",
	"bounce":          "The address bounced or complained",
	"legacy":          "Carried in from the customer record",
	"shopify":         "The web shop",
	"merge":           "Moved from a merged duplicate",
}

// CRMConsentInput is one consent action to record.
type CRMConsentInput struct {
	CustomerID    string `json:"customer_id"`
	Channel       string `json:"channel"`
	Purpose       string `json:"purpose"`
	Action        string `json:"action"`
	Source        string `json:"source"`
	Evidence      string `json:"evidence"`
	NoticeVersion string `json:"notice_version"`
	RecordedBy    string `json:"-"`
}

// CRMConsentRow is one ledger row.
type CRMConsentRow struct {
	ID            int64     `json:"id"`
	Channel       string    `json:"channel"`
	Purpose       string    `json:"purpose"`
	Action        string    `json:"action"`
	Source        string    `json:"source"`
	SourceLabel   string    `json:"source_label"`
	RecordedBy    string    `json:"recorded_by,omitempty"`
	Evidence      string    `json:"evidence,omitempty"`
	NoticeVersion string    `json:"notice_version,omitempty"`
	CreatedAt     time.Time `json:"created_at"`
}

// CRMConsentState is the answer for one channel x purpose.
type CRMConsentState struct {
	Channel   string `json:"channel"`
	Purpose   string `json:"purpose"`
	State     string `json:"state"` // granted, denied, withdrawn, none
	Reachable bool   `json:"reachable"`
	Reason    string `json:"reason,omitempty"` // the suppression code when not reachable
	Source    string `json:"source,omitempty"`
	At        string `json:"at,omitempty"`
}

func crmConsentChannel(raw string) string {
	if strings.EqualFold(strings.TrimSpace(raw), crmChannelAll) {
		return crmChannelAll
	}
	return crmChannel(raw)
}

func crmConsentPurpose(raw string) string {
	if strings.EqualFold(strings.TrimSpace(raw), crmPurposeAll) {
		return crmPurposeAll
	}
	return crmPurpose(raw)
}

// RecordCRMConsent records one consent action. Repeating the latest action
// for the same channel and purpose records nothing more (a second "stop" is
// harmless). An import can never turn a refusal or withdrawal back into a
// grant - only the customer can (101.8.2). A withdrawal cancels every message
// to the customer on that channel that has not reached the provider yet.
func RecordCRMConsent(tenantID string, in CRMConsentInput) (*CRMConsentState, error) {
	schema, err := crmSchema(tenantID)
	if err != nil {
		return nil, err
	}
	return recordCRMConsentInSchema(schema, in)
}

func recordCRMConsentInSchema(schema string, in CRMConsentInput) (*CRMConsentState, error) {
	in.Channel, in.Purpose = crmConsentChannel(in.Channel), crmConsentPurpose(in.Purpose)
	in.Action, in.Source = strings.ToLower(strings.TrimSpace(in.Action)), strings.ToLower(strings.TrimSpace(in.Source))
	if in.Channel == "" || in.Purpose == "" {
		return nil, crmErr(CRMCodeConsent, "choose a channel (SMS, WhatsApp, Email or All) and a purpose (marketing, service or all)")
	}
	if in.Action != CRMConsentGranted && in.Action != CRMConsentDenied && in.Action != CRMConsentWithdrawn {
		return nil, crmErr(CRMCodeConsent, "a consent is granted, denied or withdrawn")
	}
	if _, ok := crmConsentSources[in.Source]; !ok {
		return nil, crmErr(CRMCodeConsent, "say where this consent came from")
	}
	if in.Action == CRMConsentGranted && (in.Channel == crmChannelAll || in.Purpose == crmPurposeAll) {
		return nil, crmErr(CRMCodeConsent, "consent is given one channel and purpose at a time")
	}
	c, err := crmLoadCustomer(schema, in.CustomerID)
	if errors.Is(err, ErrCRMNotFound) {
		return nil, crmNotFound("customer", in.CustomerID)
	}
	if err != nil {
		return nil, err
	}
	if crmCustomerErased(c) {
		return nil, crmErr(CRMCodeErased, "this customer was erased - nothing can be recorded for them")
	}

	tx, err := db.DB.Begin()
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	// One writer per customer at a time, so two screens recording at once
	// cannot interleave a grant and a withdrawal.
	if _, err := tx.Exec(`SELECT pg_advisory_xact_lock(hashtext($1))`, schema+":consent:"+c.ID); err != nil {
		return nil, err
	}
	var latest string
	err = tx.QueryRow(fmt.Sprintf(`SELECT action FROM %s.crm_consent_ledger WHERE customer_id = $1 AND channel = $2 AND purpose = $3 ORDER BY id DESC LIMIT 1`, schema),
		c.ID, in.Channel, in.Purpose).Scan(&latest)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return nil, err
	}
	if in.Action == CRMConsentGranted && in.Source == "import" && (latest == CRMConsentWithdrawn || latest == CRMConsentDenied) {
		return nil, crmErr(CRMCodeConsent, "the customer said no on %s - only they can opt back in, an import cannot", in.Channel)
	}
	if latest != in.Action {
		notice := strings.TrimSpace(in.NoticeVersion)
		if notice == "" && in.Action == CRMConsentGranted {
			notice = GetSettingStringForSchema(schema, "crm.notice_version")
		}
		if _, err := tx.Exec(fmt.Sprintf(`INSERT INTO %s.crm_consent_ledger (customer_id, channel, purpose, action, source, recorded_by, evidence, notice_version)
			VALUES ($1, $2, $3, $4, $5, NULLIF($6, ''), NULLIF($7, ''), NULLIF($8, ''))`, schema),
			c.ID, in.Channel, in.Purpose, in.Action, in.Source, in.RecordedBy, truncateRunes(strings.TrimSpace(in.Evidence), 1000), notice); err != nil {
			return nil, err
		}
		if err := crmSyncConsentTicksTx(tx, schema, c.ID, in.RecordedBy); err != nil {
			return nil, err
		}
		if in.Action != CRMConsentGranted {
			if err := crmCancelQueuedForConsentTx(tx, schema, c.ID, in.Channel, in.Purpose); err != nil {
				return nil, err
			}
		}
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	states, err := crmConsentStates(schema, c)
	if err != nil {
		return nil, err
	}
	ch := in.Channel
	if ch == crmChannelAll {
		ch = CRMChannelSMS
	}
	p := in.Purpose
	if p == crmPurposeAll {
		p = CRMPurposeMarketing
	}
	for i := range states {
		if states[i].Channel == ch && states[i].Purpose == p {
			return &states[i], nil
		}
	}
	return &CRMConsentState{Channel: ch, Purpose: p}, nil
}

// crmLoadLatestConsent reads the newest ledger row per channel x purpose.
func crmLoadLatestConsent(q interface {
	Query(string, ...interface{}) (*sql.Rows, error)
}, schema, customerID string) (map[string]crmLatestConsent, error) {
	rows, err := q.Query(fmt.Sprintf(`SELECT DISTINCT ON (channel, purpose) id, channel, purpose, action, source, created_at FROM %s.crm_consent_ledger
		WHERE customer_id = $1 ORDER BY channel, purpose, id DESC`, schema), customerID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	latest := map[string]crmLatestConsent{}
	for rows.Next() {
		var ch, p string
		var r crmLatestConsent
		if err := rows.Scan(&r.id, &ch, &p, &r.action, &r.source, &r.at); err != nil {
			return nil, err
		}
		latest[ch+"|"+p] = r
	}
	return latest, rows.Err()
}

// crmSyncConsentTicksTx rewrites the Customer's marketing ticks from the
// ledger, in the same transaction as the row that changed them.
func crmSyncConsentTicksTx(tx *sql.Tx, schema, customerID, actor string) error {
	latest, err := crmLoadLatestConsent(tx, schema, customerID)
	if err != nil {
		return err
	}
	ticks := map[string]bool{}
	for _, ch := range crmChannels {
		state, code := crmEffectiveConsent(latest, ch, CRMPurposeMarketing)
		ticks[ch] = code == "" && state == CRMConsentGranted
	}
	patch := crmJSON(map[string]interface{}{
		"consent_sms": ticks[CRMChannelSMS], "consent_whatsapp": ticks[CRMChannelWhatsApp], "consent_email": ticks[CRMChannelEmail],
		"consent_updated_at": time.Now().Format("2006-01-02 15:04"),
		"consent_source":     "Recorded by " + firstNonEmpty(strings.TrimSpace(actor), "system"),
	})
	_, err = tx.Exec(fmt.Sprintf(`UPDATE %s.documents SET data = data || $2::jsonb, updated_at = CURRENT_TIMESTAMP WHERE doctype = 'Customer' AND id = $1`, schema), customerID, patch)
	return err
}

// crmCancelQueuedForConsentTx cancels the customer's messages on channel that
// no provider has seen yet (101.8's done-when: a withdrawal while 1,000 are
// queued stops every one not yet handed over). The gate re-checks consent at
// send time too, so a message queued a moment later is still stopped.
func crmCancelQueuedForConsentTx(tx *sql.Tx, schema, customerID, channel, purpose string) error {
	where := "customer_id = $1 AND status IN ('Queued', 'Deferred')"
	args := []interface{}{customerID}
	if channel != crmChannelAll {
		args = append(args, channel)
		where += fmt.Sprintf(" AND channel = $%d", len(args))
	}
	if purpose != crmPurposeAll {
		args = append(args, purpose)
		where += fmt.Sprintf(" AND purpose = $%d", len(args))
	}
	_, err := tx.Exec(fmt.Sprintf(`UPDATE %s.crm_sends SET status = 'Cancelled', reason_code = 'CONSENT_WITHDRAWN',
		reason_detail = 'The customer withdrew consent before it was sent', decided_at = NOW(), final_at = NOW(),
		history = history || $%d::jsonb WHERE `+where, schema, len(args)+1), append(args, crmHistoryEntry("Cancelled", "consent withdrawn"))...)
	return err
}

// CRMConsentLedger is a customer's ledger, newest first.
func CRMConsentLedger(tenantID, customerID string) ([]CRMConsentRow, error) {
	schema, err := crmSchema(tenantID)
	if err != nil {
		return nil, err
	}
	rows, err := db.DB.Query(fmt.Sprintf(`SELECT id, channel, purpose, action, source, COALESCE(recorded_by, ''), COALESCE(evidence, ''), COALESCE(notice_version, ''), created_at
		FROM %s.crm_consent_ledger WHERE customer_id = $1 ORDER BY id DESC LIMIT 200`, schema), customerID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []CRMConsentRow{}
	for rows.Next() {
		var r CRMConsentRow
		if err := rows.Scan(&r.ID, &r.Channel, &r.Purpose, &r.Action, &r.Source, &r.RecordedBy, &r.Evidence, &r.NoticeVersion, &r.CreatedAt); err != nil {
			return nil, err
		}
		r.SourceLabel = crmConsentSources[r.Source]
		out = append(out, r)
	}
	return out, rows.Err()
}

// crmConsentStates answers every channel x purpose for one customer.
func crmConsentStates(schema string, c crmCustomer) ([]CRMConsentState, error) {
	latest, err := crmLoadLatestConsent(db.DB, schema, c.ID)
	if err != nil {
		return nil, err
	}
	suppressed, err := crmContactSuppressed(schema, c)
	if err != nil {
		return nil, err
	}
	out := []CRMConsentState{}
	for _, ch := range crmChannels {
		for _, p := range []string{CRMPurposeMarketing, CRMPurposeService} {
			st := CRMConsentState{Channel: ch, Purpose: p, State: "none"}
			if r, ok := latest[ch+"|"+p]; ok {
				st.Source, st.At = r.source, r.at.Format("2006-01-02 15:04")
			} else if p == CRMPurposeMarketing && truthy(c.Data[crmConsentTick(ch)]) {
				// A tick with no ledger row (an import since the carry-in):
				// a grant of unknown provenance, shown as such.
				latest[ch+"|"+p] = crmLatestConsent{action: CRMConsentGranted, source: "legacy"}
				st.Source = "legacy"
			}
			var code string
			st.State, code = crmEffectiveConsent(latest, ch, p)
			switch {
			case crmCustomerErased(c):
				st.Reason = "ERASED"
			case suppressed[ch] != "":
				st.Reason = suppressed[ch]
			default:
				st.Reason = code
			}
			st.Reachable = st.Reason == ""
			out = append(out, st)
		}
	}
	return out, nil
}

func crmConsentTick(channel string) string {
	switch channel {
	case CRMChannelSMS:
		return "consent_sms"
	case CRMChannelWhatsApp:
		return "consent_whatsapp"
	case CRMChannelEmail:
		return "consent_email"
	}
	return ""
}

// crmLatestConsent is the newest ledger row for one channel x purpose.
type crmLatestConsent struct {
	id             int64
	action, source string
	at             time.Time
}

func crmIsNo(action string) bool { return action == CRMConsentWithdrawn || action == CRMConsentDenied }

// crmEffectiveConsent applies the strength order to one channel x purpose:
// stop everything (All / all), then all marketing (All / purpose), then the
// whole channel (channel / all), then the topic itself, then consent. A
// broader "no" stops only what was agreed before it: a later, specific grant
// by the customer (the preference page, the till) is the newer decision and
// wins. Returns the topic's state and the suppression code ("" = reachable).
func crmEffectiveConsent(latest map[string]crmLatestConsent, channel, purpose string) (string, string) {
	topic, has := latest[channel+"|"+purpose]
	state := "none"
	if has {
		state = topic.action
	}
	for _, b := range []struct{ key, code string }{
		{crmChannelAll + "|" + crmPurposeAll, "GLOBAL_OPT_OUT"},
		{crmChannelAll + "|" + purpose, "GLOBAL_OPT_OUT"},
		{channel + "|" + crmPurposeAll, "CHANNEL_OPT_OUT"},
	} {
		if r, ok := latest[b.key]; ok && crmIsNo(r.action) && (!has || r.id > topic.id || topic.id == 0 && topic.source == "legacy") {
			return state, b.code
		}
	}
	if crmIsNo(state) {
		return state, "TOPIC_OPT_OUT"
	}
	if purpose == CRMPurposeMarketing && state != CRMConsentGranted {
		return state, "NO_CONSENT"
	}
	return state, ""
}

// crmCustomerErased: an erased (anonymized) or merged-away customer is never
// messaged.
func crmCustomerErased(c crmCustomer) bool {
	return strField(c.Data, "anonymized_at") != "" || c.Status == "Merged"
}

// crmContactSuppressed reports, per channel, a hashed block on the
// customer's address (bounce, complaint, erasure of another record with it).
func crmContactSuppressed(schema string, c crmCustomer) (map[string]string, error) {
	out := map[string]string{}
	for _, ch := range crmChannels {
		_, _, hash, ok := crmContact(schema, c.Data, ch)
		if !ok {
			continue
		}
		kind := "phone"
		if ch == CRMChannelEmail {
			kind = "email"
		}
		var reason string
		err := db.DB.QueryRow(fmt.Sprintf(`SELECT reason FROM %s.crm_suppressions WHERE kind = $1 AND value_hash = $2 ORDER BY id LIMIT 1`, schema), kind, hash).Scan(&reason)
		if errors.Is(err, sql.ErrNoRows) {
			continue
		}
		if err != nil {
			return nil, err
		}
		out[ch] = "SUPPRESSED_" + strings.ToUpper(reason)
	}
	return out, nil
}

// CRMConsentFor is the public form of the per-customer answer.
func CRMConsentFor(tenantID, customerID string) ([]CRMConsentState, error) {
	schema, err := crmSchema(tenantID)
	if err != nil {
		return nil, err
	}
	c, err := crmLoadCustomer(schema, customerID)
	if errors.Is(err, ErrCRMNotFound) {
		return nil, crmNotFound("customer", customerID)
	}
	if err != nil {
		return nil, err
	}
	return crmConsentStates(schema, c)
}

// crmSuppress adds a hashed block (idempotent).
func crmSuppress(q interface {
	Exec(string, ...interface{}) (sql.Result, error)
}, schema, kind, hash, reason string) error {
	if hash == "" {
		return nil
	}
	_, err := q.Exec(fmt.Sprintf(`INSERT INTO %s.crm_suppressions (kind, value_hash, reason) VALUES ($1, $2, $3) ON CONFLICT (kind, value_hash, reason) DO NOTHING`, schema), kind, hash, reason)
	return err
}

// --- preference page, unsubscribe, STOP (101.8.2) ----------------------------

// crmIssuePrefToken gives a message its own preference link token. The link
// carries no customer id; only its hash is stored.
func crmIssuePrefToken(schema, customerID, sendID string) (string, error) {
	token, err := crmToken(12)
	if err != nil {
		return "", err
	}
	_, err = db.DB.Exec(fmt.Sprintf(`INSERT INTO %s.crm_pref_tokens (token_hash, customer_id, send_id) VALUES ($1, $2, NULLIF($3, ''))`, schema),
		crmTokenHash(token), customerID, sendID)
	return token, err
}

// crmPublicLink is a customer-facing link: the configured base, else the
// client's own host when this server knows one. tenant_id rides along so the
// page's call reaches the right client on a shared host.
func crmPublicLink(schema, path, token string) string {
	base := crmPublicBase(schema)
	if base == "" {
		// No address customers can reach: no link, so a template that
		// needs one is held (TEMPLATE_DATA_MISSING) rather than sent broken.
		return ""
	}
	tid, _ := tenantIDForSchema(schema)
	link := base + path + "?t=" + token
	if tid != "" && tid != "default" {
		link += "&tenant_id=" + tid
	}
	return link
}

func crmCustomerForPrefToken(schema, token string) (crmCustomer, error) {
	token = strings.TrimSpace(token)
	if len(token) < 10 || len(token) > 64 {
		return crmCustomer{}, crmErr(CRMCodeLink, "this link is not valid")
	}
	var customerID string
	err := db.DB.QueryRow(fmt.Sprintf(`SELECT customer_id FROM %s.crm_pref_tokens WHERE token_hash = $1`, schema), crmTokenHash(token)).Scan(&customerID)
	if errors.Is(err, sql.ErrNoRows) {
		return crmCustomer{}, crmErr(CRMCodeLink, "this link is not valid or has expired")
	}
	if err != nil {
		return crmCustomer{}, err
	}
	c, err := crmLoadCustomer(schema, customerID)
	if err != nil || crmCustomerErased(c) {
		return crmCustomer{}, crmErr(CRMCodeLink, "this link is not valid or has expired")
	}
	return c, nil
}

// CRMPreferences is what the customer sees on their preference page: their
// first name and, per channel, whether offers and service messages may come.
type CRMPreferences struct {
	FirstName string                  `json:"first_name"`
	Business  string                  `json:"business,omitempty"`
	StopAll   bool                    `json:"stop_all"`
	Channels  []CRMPreferencesChannel `json:"channels"`
}

type CRMPreferencesChannel struct {
	Channel   string `json:"channel"`
	Contact   string `json:"contact"`
	Marketing bool   `json:"marketing"`
	Service   bool   `json:"service"`
}

func GetCRMPreferences(tenantID, token string) (*CRMPreferences, error) {
	schema, err := crmSchema(tenantID)
	if err != nil {
		return nil, crmErr(CRMCodeLink, "this link is not valid")
	}
	c, err := crmCustomerForPrefToken(schema, token)
	if err != nil {
		return nil, err
	}
	return crmPreferencesFor(schema, c)
}

func crmPreferencesFor(schema string, c crmCustomer) (*CRMPreferences, error) {
	states, err := crmConsentStates(schema, c)
	if err != nil {
		return nil, err
	}
	name := strings.Fields(c.Name())
	out := &CRMPreferences{Channels: []CRMPreferencesChannel{}}
	if len(name) > 0 {
		out.FirstName = name[0]
	}
	out.Business = crmBusinessName(schema)
	for _, ch := range crmChannels {
		_, masked, _, ok := crmContact(schema, c.Data, ch)
		if !ok {
			continue
		}
		pc := CRMPreferencesChannel{Channel: ch, Contact: masked}
		for _, st := range states {
			if st.Channel != ch {
				continue
			}
			if st.Reason == "GLOBAL_OPT_OUT" {
				out.StopAll = true
			}
			if st.Purpose == CRMPurposeMarketing {
				pc.Marketing = st.Reachable
			} else {
				pc.Service = st.Reachable
			}
		}
		out.Channels = append(out.Channels, pc)
	}
	return out, nil
}

// CRMPreferenceChange is the customer's own choice from the page.
type CRMPreferenceChange struct {
	StopAll  *bool `json:"stop_all,omitempty"`
	Channels []struct {
		Channel   string `json:"channel"`
		Marketing *bool  `json:"marketing,omitempty"`
		Service   *bool  `json:"service,omitempty"`
	} `json:"channels"`
}

// SetCRMPreferences records the customer's choices - the one place a
// customer opts back in by themselves.
func SetCRMPreferences(tenantID, token string, change CRMPreferenceChange) (*CRMPreferences, error) {
	schema, err := crmSchema(tenantID)
	if err != nil {
		return nil, crmErr(CRMCodeLink, "this link is not valid")
	}
	c, err := crmCustomerForPrefToken(schema, token)
	if err != nil {
		return nil, err
	}
	evidence := "Preference link " + crmTokenHash(token)[:12]
	rec := func(ch, p, action string) error {
		_, err := recordCRMConsentInSchema(schema, CRMConsentInput{CustomerID: c.ID, Channel: ch, Purpose: p, Action: action,
			Source: "preference_page", Evidence: evidence, RecordedBy: "customer"})
		return err
	}
	if change.StopAll != nil {
		action := CRMConsentWithdrawn
		if !*change.StopAll {
			action = CRMConsentGranted
		}
		if action == CRMConsentWithdrawn {
			if err := rec(crmChannelAll, crmPurposeAll, action); err != nil {
				return nil, err
			}
		} else if err := crmLiftStopAll(schema, c.ID, evidence); err != nil {
			return nil, err
		}
	}
	for _, ch := range change.Channels {
		channel := crmChannel(ch.Channel)
		if channel == "" {
			continue
		}
		if ch.Marketing != nil {
			a := CRMConsentWithdrawn
			if *ch.Marketing {
				a = CRMConsentGranted
				// Opting in to one channel lifts a channel-wide "no" the
				// customer set earlier on it.
				if err := crmLiftChannelOff(schema, c.ID, channel, evidence); err != nil {
					return nil, err
				}
			}
			if err := rec(channel, CRMPurposeMarketing, a); err != nil {
				return nil, err
			}
		}
		if ch.Service != nil {
			a := CRMConsentWithdrawn
			if *ch.Service {
				a = CRMConsentGranted
			}
			if err := rec(channel, CRMPurposeService, a); err != nil {
				return nil, err
			}
		}
	}
	_, _ = db.DB.Exec(fmt.Sprintf(`UPDATE %s.crm_pref_tokens SET used_at = NOW() WHERE token_hash = $1`, schema), crmTokenHash(token))
	c, err = crmLoadCustomer(schema, c.ID)
	if err != nil {
		return nil, err
	}
	return crmPreferencesFor(schema, c)
}

// crmLiftStopAll / crmLiftChannelOff undo a customer's own blanket "no" -
// recorded as a fresh grant row on the same key, so the history keeps both.
func crmLiftStopAll(schema, customerID, evidence string) error {
	_, err := db.DB.Exec(fmt.Sprintf(`INSERT INTO %s.crm_consent_ledger (customer_id, channel, purpose, action, source, recorded_by, evidence)
		SELECT $1::text, 'All', 'all', 'granted', 'preference_page', 'customer', $2::text
		 WHERE (SELECT action FROM %s.crm_consent_ledger WHERE customer_id = $1::text AND channel = 'All' AND purpose = 'all' ORDER BY id DESC LIMIT 1) IN ('withdrawn', 'denied')`,
		schema, schema), customerID, evidence)
	return err
}

func crmLiftChannelOff(schema, customerID, channel, evidence string) error {
	_, err := db.DB.Exec(fmt.Sprintf(`INSERT INTO %s.crm_consent_ledger (customer_id, channel, purpose, action, source, recorded_by, evidence)
		SELECT $1::text, $2::text, 'all', 'granted', 'preference_page', 'customer', $3::text
		 WHERE (SELECT action FROM %s.crm_consent_ledger WHERE customer_id = $1::text AND channel = $2::text AND purpose = 'all' ORDER BY id DESC LIMIT 1) IN ('withdrawn', 'denied')`,
		schema, schema), customerID, channel, evidence)
	return err
}

// CRMUnsubscribe is the email one-click unsubscribe (RFC 8058) and the
// "unsubscribe" link: marketing email off. Repeating it is harmless.
func CRMUnsubscribe(tenantID, token string) error {
	schema, err := crmSchema(tenantID)
	if err != nil {
		return crmErr(CRMCodeLink, "this link is not valid")
	}
	c, err := crmCustomerForPrefToken(schema, token)
	if err != nil {
		return err
	}
	channel := CRMChannelEmail
	var sendChannel string
	if err := db.DB.QueryRow(fmt.Sprintf(`SELECT s.channel FROM %s.crm_pref_tokens t JOIN %s.crm_sends s ON s.id = t.send_id WHERE t.token_hash = $1`, schema, schema),
		crmTokenHash(token)).Scan(&sendChannel); err == nil && sendChannel != "" {
		channel = sendChannel
	}
	_, err = recordCRMConsentInSchema(schema, CRMConsentInput{CustomerID: c.ID, Channel: channel, Purpose: CRMPurposeMarketing,
		Action: CRMConsentWithdrawn, Source: "unsubscribe", Evidence: "Unsubscribe link " + crmTokenHash(token)[:12], RecordedBy: "customer"})
	return err
}

// crmIsStopWord reports a reply that means "stop sending me offers".
func crmIsStopWord(body string) bool {
	w := strings.ToUpper(strings.Trim(strings.TrimSpace(body), ".!"))
	switch w {
	case "STOP", "STOP ALL", "STOPALL", "UNSUBSCRIBE", "CANCEL", "END", "QUIT", "OPT OUT", "OPTOUT":
		return true
	}
	return false
}

// crmHandleStop withdraws marketing on channel for every customer with this
// phone - from a provider callback, so it takes the phone, not a customer.
func crmHandleStop(schema, channel, e164, evidence string) (int, error) {
	ids, err := crmCustomersByPhone(schema, e164)
	if err != nil {
		return 0, err
	}
	for _, id := range ids {
		if _, err := recordCRMConsentInSchema(schema, CRMConsentInput{CustomerID: id, Channel: channel, Purpose: CRMPurposeMarketing,
			Action: CRMConsentWithdrawn, Source: "stop_reply", Evidence: evidence, RecordedBy: "customer"}); err != nil {
			return 0, err
		}
	}
	return len(ids), nil
}

// crmCustomersByPhone finds live customers whose phone is this number, in
// national or international form.
func crmCustomersByPhone(schema, e164 string) ([]string, error) {
	digits := strings.TrimPrefix(strings.TrimSpace(e164), "+")
	rows, err := db.DB.Query(fmt.Sprintf(`SELECT id, COALESCE(data->>'phone', ''), COALESCE(data->>'phone_country', '') FROM %s.documents
		WHERE doctype = 'Customer' AND deleted_at IS NULL AND status <> 'Merged' AND COALESCE(data->>'anonymized_at', '') = ''
		  AND $1 LIKE '%%' || regexp_replace(COALESCE(data->>'phone', ''), '[^0-9]', '', 'g') AND COALESCE(data->>'phone', '') <> ''`, schema), digits)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var id, phone, country string
		if err := rows.Scan(&id, &phone, &country); err != nil {
			return nil, err
		}
		if country == "" {
			if tid, err := tenantIDForSchema(schema); err == nil {
				country = TenantPhoneCountry(tid)
			}
		}
		if p := NormalizePhone(phone, country); p.Valid && p.E164 == "+"+digits {
			out = append(out, id)
		}
	}
	return out, rows.Err()
}

// --- erasure that sticks (101.8.3) -------------------------------------------

// crmEraseCustomer runs inside an approved erasure (AnonymizeCustomer),
// before the record is anonymised: every queued message and live journey is
// cancelled first, the CRM's own personal rows go, and the phone and email
// are kept only as hashes so an import or the web shop cannot bring the
// person back.
func crmEraseCustomer(schema, customerID string, data map[string]interface{}) error {
	tx, err := db.DB.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.Exec(fmt.Sprintf(`UPDATE %s.crm_sends SET status = 'Cancelled', reason_code = 'ERASED', reason_detail = 'The customer was erased',
		decided_at = NOW(), final_at = NOW(), rendered_body = NULL, rendered_subject = NULL, contact_masked = NULL
		WHERE customer_id = $1 AND status IN ('Queued', 'Deferred')`, schema), customerID); err != nil {
		return err
	}
	// Sent messages keep their decision record (D12) but not the text or
	// the address.
	if _, err := tx.Exec(fmt.Sprintf(`UPDATE %s.crm_sends SET rendered_body = NULL, rendered_subject = NULL, contact_masked = NULL, vars = '{}'::jsonb WHERE customer_id = $1`, schema), customerID); err != nil {
		return err
	}
	if _, err := tx.Exec(fmt.Sprintf(`UPDATE %s.crm_journey_runs SET state = 'Cancelled', exit_reason = 'erased', ended_at = NOW(), updated_at = NOW(), context = '{}'::jsonb
		WHERE customer_id = $1 AND state IN ('Active', 'Waiting')`, schema), customerID); err != nil {
		return err
	}
	for _, stmt := range []string{
		`DELETE FROM %s.crm_consent_ledger WHERE customer_id = $1`,
		`DELETE FROM %s.crm_customer_notes WHERE customer_id = $1`,
		`DELETE FROM %s.crm_pref_tokens WHERE customer_id = $1`,
		`DELETE FROM %s.crm_wishlist WHERE customer_id = $1`,
		`DELETE FROM %s.crm_static_list_members WHERE customer_id = $1`,
		`UPDATE %s.crm_conversations SET phone = NULL, status = 'Closed', updated_at = NOW() WHERE customer_id = $1`,
		`UPDATE %s.crm_feedback SET comment = NULL WHERE customer_id = $1`,
		`UPDATE %s.crm_appointments SET note = NULL WHERE customer_id = $1`,
		`UPDATE %s.crm_events SET payload = '{}'::jsonb WHERE customer_id = $1`,
	} {
		if _, err := tx.Exec(fmt.Sprintf(stmt, schema), customerID); err != nil {
			return err
		}
	}
	if _, err := tx.Exec(fmt.Sprintf(`DELETE FROM %s.crm_conversation_messages WHERE conversation_id IN (SELECT id FROM %s.crm_conversations WHERE customer_id = $1)`, schema, schema), customerID); err != nil {
		return err
	}
	tid, _ := tenantIDForSchema(schema)
	for _, h := range crmPhoneHashes(tid, strField(data, "phone")) {
		if err := crmSuppress(tx, schema, "phone", h, "erased"); err != nil {
			return err
		}
	}
	if _, _, h, ok := crmContact(schema, data, CRMChannelSMS); ok {
		if err := crmSuppress(tx, schema, "phone", h, "erased"); err != nil {
			return err
		}
	}
	if e := strings.TrimSpace(strField(data, "email")); e != "" {
		if err := crmSuppress(tx, schema, "email", crmHash("email", e), "erased"); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// crmErasedProfileFields are the CRM fields an erasure clears alongside
// AnonymizeCustomer's own name / phone / email / birthday.
var crmErasedProfileFields = []string{"sizes", "tags", "preferred_store", "preferred_salesman", "date_of_anniversary", "gender",
	"consent_sms", "consent_whatsapp", "consent_email", "consent_source", "clevertap_segments", "profile_sources"}

// CRMErasedContactBlocked reports whether a phone or email belongs to an
// erased customer - checked before any new Customer is created (the form,
// the till, an import, the web shop), so erasure sticks.
func CRMErasedContactBlocked(tenantID, phone, email string) (bool, error) {
	schema, err := crmSchema(tenantID)
	if err != nil {
		return false, err
	}
	var hashes []string
	kinds := []string{}
	for _, h := range crmPhoneHashes(tenantID, phone) {
		hashes, kinds = append(hashes, h), append(kinds, "phone")
	}
	if e := strings.TrimSpace(email); e != "" {
		hashes, kinds = append(hashes, crmHash("email", e)), append(kinds, "email")
	}
	for i := range hashes {
		var n int
		if err := db.DB.QueryRow(fmt.Sprintf(`SELECT COUNT(*) FROM %s.crm_suppressions WHERE kind = $1 AND value_hash = $2 AND reason = 'erased'`, schema), kinds[i], hashes[i]).Scan(&n); err != nil {
			if strings.Contains(err.Error(), "does not exist") {
				return false, nil
			}
			return false, err
		}
		if n > 0 {
			return true, nil
		}
	}
	return false, nil
}

// --- the Customer save (generic form, import) --------------------------------

// CRMCustomerSave is what the generic Customer save does for the CRM, before
// and after its one write: before, a new record whose phone or email was
// erased is refused and the profile fields that changed are stamped with
// their source; after, ticks that changed become consent ledger rows.
type CRMCustomerSave struct {
	tenantID, actor, source string
	changes                 []CRMConsentInput
}

// PrepareCRMCustomerSave runs before the write. source is "form" for the
// record screen, "till" for the POS quick-add.
func PrepareCRMCustomerSave(tenantID, doctype, role string, isCreate bool, prior, payload map[string]interface{}, actor, source string) (*CRMCustomerSave, error) {
	if doctype != "Customer" || payload == nil {
		return nil, nil
	}
	crmRestoreMaskedContacts(tenantID, role, prior, payload)
	if isCreate {
		blocked, err := CRMErasedContactBlocked(tenantID, strField(payload, "phone"), strField(payload, "email"))
		if err != nil {
			return nil, err
		}
		if blocked {
			return nil, crmErr(CRMCodeErased, "this phone number or email belonged to a customer who asked to be erased, so it cannot be added again")
		}
	}
	crmStampProfileSources(prior, payload, source, actor)
	save := &CRMCustomerSave{tenantID: tenantID, actor: actor, source: source}
	for _, ch := range crmChannels {
		f := crmConsentTick(ch)
		if _, present := payload[f]; !present {
			continue
		}
		now, was := truthy(payload[f]), truthy(prior[f])
		if now == was {
			continue
		}
		action := CRMConsentWithdrawn
		if now {
			action = CRMConsentGranted
		}
		save.changes = append(save.changes, CRMConsentInput{Channel: ch, Purpose: CRMPurposeMarketing, Action: action, Source: source, RecordedBy: actor})
	}
	return save, nil
}

// Commit runs after the write has committed.
func (s *CRMCustomerSave) Commit(customerID string, isCreate bool) {
	if s == nil {
		return
	}
	for _, ch := range s.changes {
		ch.CustomerID = customerID
		if _, err := RecordCRMConsent(s.tenantID, ch); err != nil {
			LogSystemError(s.tenantID, "", "ERROR", "crm_consent", "consent not recorded for "+customerID+": "+err.Error(), "")
		}
	}
	if isCreate {
		if schema, err := crmSchema(s.tenantID); err == nil {
			_ = RecordCRMEvent(schema, customerID, CRMEventCustomerCreated, customerID, nil)
		}
	}
}

// CRMCheckImportedCustomer is the import's per-row check: an erased contact
// is refused, and a consent tick an import sets cannot override a refusal or
// withdrawal the customer made (only the customer opts back in).
func CRMCheckImportedCustomer(tenantID string, isCreate bool, row map[string]interface{}) error {
	if isCreate {
		blocked, err := CRMErasedContactBlocked(tenantID, strField(row, "phone"), strField(row, "email"))
		if err != nil {
			return err
		}
		if blocked {
			return crmErr(CRMCodeErased, "phone or email belonged to a customer who asked to be erased - not imported")
		}
		return nil
	}
	id := firstNonEmpty(strField(row, "id"), strField(row, "code"))
	if id == "" {
		return nil
	}
	schema, err := crmSchema(tenantID)
	if err != nil {
		return err
	}
	for _, ch := range crmChannels {
		f := crmConsentTick(ch)
		if !truthy(row[f]) {
			continue
		}
		var latest string
		err := db.DB.QueryRow(fmt.Sprintf(`SELECT action FROM %s.crm_consent_ledger l JOIN %s.documents d ON d.id = l.customer_id
			WHERE (d.id = $1 OR d.data->>'code' = $1) AND d.doctype = 'Customer' AND l.channel IN ($2, 'All') AND l.purpose IN ('marketing', 'all')
			ORDER BY l.id DESC LIMIT 1`, schema, schema), id, ch).Scan(&latest)
		if err == nil && (latest == CRMConsentWithdrawn || latest == CRMConsentDenied) {
			row[f] = false
		}
	}
	return nil
}

// crmStampProfileSources records, per profile field changed on this save,
// where the value came from and when (101.1.1 "imported fields show source
// and last update").
func crmStampProfileSources(prior, payload map[string]interface{}, source, actor string) {
	sources := map[string]interface{}{}
	if m, ok := prior["profile_sources"].(map[string]interface{}); ok {
		for k, v := range m {
			sources[k] = v
		}
	}
	changed := false
	stamp := map[string]interface{}{"source": source, "by": actor, "at": time.Now().Format("2006-01-02 15:04")}
	for _, f := range crmProfileFields {
		nv, present := payload[f]
		if !present || crmString(nv) == crmString(prior[f]) {
			continue
		}
		sources[f] = stamp
		changed = true
	}
	if changed || len(sources) > 0 {
		payload["profile_sources"] = sources
	}
}

// crmProfileFields are the profile fields whose source is tracked.
var crmProfileFields = []string{"name", "phone", "email", "date_of_birth", "date_of_anniversary", "gender", "sizes",
	"preferred_store", "preferred_salesman", "tags", "city", "state", "pincode", "address"}

// crmImportStampSources stamps an imported row's profile sources.
func crmImportStampSources(prior, row map[string]interface{}, actor string) {
	crmStampProfileSources(prior, row, "import", actor)
}

// crmReady reports whether this client schema has the Stage 101 tables (a
// schema the migration has not reached yet keeps the older behaviour).
func crmReady(schema string) bool {
	var ok bool
	_ = db.DB.QueryRow(`SELECT to_regclass($1) IS NOT NULL`, schema+".crm_sends").Scan(&ok)
	return ok
}

func crmEraseCustomerIfReady(schema, customerID string, data map[string]interface{}) error {
	if !crmReady(schema) {
		return nil
	}
	return crmEraseCustomer(schema, customerID, data)
}

// crmPublicBase is the address customer links start with: the client's
// setting, else its own hostname (TenantURL), else "".
func crmPublicBase(schema string) string {
	if base := strings.TrimRight(strings.TrimSpace(GetSettingStringForSchema(schema, "crm.public_link_base")), "/"); base != "" {
		return base
	}
	if tid, err := tenantIDForSchema(schema); err == nil {
		if u, err := TenantURL(tid); err == nil && u != "" {
			return strings.TrimRight(u, "/")
		}
	}
	return ""
}
