package engines

import (
	"crypto/rand"
	"crypto/sha256"
	"custom_erp/db"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"
	_ "time/tzdata" // Stage 101: client time zones must load on any host
)

// Stage 101 - the CRM platform (docs/product/crm-platform-plan-2026-10-19.md).
// The CRM sits inside the ERP: it reads bills, returns, points and vouchers
// where they already live and adds only what the ERP did not have. Files:
//
//	crm_core.go        settings, time zone, hashing, tokens, masking helpers
//	crm_customer.go    101.1.1 the customer page, 101.1.4 contact masking
//	crm_timeline.go    101.1.2 one timeline
//	crm_merge.go       101.1.3 duplicates and approved merges
//	crm_consent.go     101.8 consent ledger, preference page, erasure
//	crm_segments.go    101.2.1 / 101.2.3 segment builder and static lists
//	crm_lifecycle.go   101.2.2 lifecycle states
//	crm_send_gate.go   101.9 the one place every CRM message passes
//	crm_providers.go   101.3.1 senders (email, SMS, WhatsApp, webhook, mock)
//	crm_templates.go   101.3.4 versioned templates
//	crm_campaigns.go   101.3.2 / .3 / .5 campaign runs, results, hold-outs
//	crm_journeys.go    101.4 journeys, crm_events
//	crm_feedback.go    101.5.1, crm_tickets.go 101.5.2, crm_inbox.go 101.5.3
//	crm_loyalty_depth.go 101.11, crm_clienteling.go 101.12, crm_pipeline.go 101.6
//	crm_reports.go     101.7.1
//
// The rule every file keeps (plan section 5): a message to a customer is
// queued as a crm_sends row and decided by the send gate, never sent from
// anywhere else.

// CRM error codes (internal/server/error_catalog_extensions.go).
const (
	CRMCodeCustomer  = "CRMPLT-1101"
	CRMCodeConsent   = "CRMPLT-1102"
	CRMCodeSegment   = "CRMPLT-1103"
	CRMCodeTemplate  = "CRMPLT-1104"
	CRMCodeSender    = "CRMPLT-1105"
	CRMCodeCampaign  = "CRMPLT-1106"
	CRMCodeJourney   = "CRMPLT-1107"
	CRMCodeLink      = "CRMPLT-1108"
	CRMCodeSlotFull  = "CRMPLT-1109"
	CRMCodeVoucher   = "CRMPLT-1110"
	CRMCodePipeline  = "CRMPLT-1111"
	CRMCodeMerge     = "CRMPLT-1112"
	CRMCodeMessage   = "CRMPLT-1113"
	CRMCodeTicket    = "CRMPLT-1114"
	CRMCodeList      = "CRMPLT-1115"
	CRMCodeHold      = "CRMPLT-1116"
	CRMCodeReferral  = "CRMPLT-1117"
	CRMCodeErased    = "CRMPLT-1118"
	CRMCodeNotFound  = "CRMPLT-1119"
	CRMCodeTask      = "CRMPLT-1120"
	CRMCodeFeedback  = "CRMPLT-1121"
	CRMCodeQuotation = "CRMPLT-1122"
)

// ErrCRMNotFound is a CRM record that does not exist (or not in this client).
var ErrCRMNotFound = errors.New("not found")

func crmErr(code, format string, args ...interface{}) error {
	return &ValidationError{Code: code, Message: fmt.Sprintf(format, args...)}
}

func crmNotFound(what, id string) error {
	return &ValidationError{Code: CRMCodeNotFound, Message: fmt.Sprintf("%s %q was not found", what, id)}
}

// Message channels the CRM sends by.
const (
	CRMChannelSMS      = "SMS"
	CRMChannelWhatsApp = "WhatsApp"
	CRMChannelEmail    = "Email"
)

// Purposes a message is sent for (D6): marketing needs a recorded grant;
// service (feedback, appointment reminders, "your order arrived") is allowed
// unless the customer has said no to it.
const (
	CRMPurposeMarketing = "marketing"
	CRMPurposeService   = "service"
)

var crmChannels = []string{CRMChannelSMS, CRMChannelWhatsApp, CRMChannelEmail}

// crmChannel returns the canonical channel name, or "" for anything else.
func crmChannel(raw string) string {
	for _, c := range crmChannels {
		if strings.EqualFold(strings.TrimSpace(raw), c) {
			return c
		}
	}
	return ""
}

func crmPurpose(raw string) string {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case CRMPurposeService:
		return CRMPurposeService
	case CRMPurposeMarketing, "":
		return CRMPurposeMarketing
	}
	return ""
}

func init() {
	reg := func(d SettingDefinition) { d.Module = "CRM"; RegisterSetting(d) }
	reg(SettingDefinition{Key: "crm.time_zone", Label: "Customer time zone", Type: SettingTypeString, Default: "Asia/Kolkata",
		Description: "The time zone birthdays, quiet hours and segment windows (\"bought in the last 30 days\") are counted in. An IANA name such as Asia/Kolkata or Asia/Dubai."})
	reg(SettingDefinition{Key: "crm.limit_per_day", Label: "Marketing messages per customer per 24 hours", Type: SettingTypeInt, Default: "2", Min: settingBound(0),
		Description: "Across SMS, WhatsApp and email together. A message over the limit is not sent and is recorded as CAP_REACHED. Service messages (feedback, reminders) do not count."})
	reg(SettingDefinition{Key: "crm.limit_per_week", Label: "Marketing messages per customer per 7 days", Type: SettingTypeInt, Default: "5", Min: settingBound(0),
		Description: "Across all channels; the same rule as the 24-hour limit."})
	reg(SettingDefinition{Key: "crm.min_gap_hours", Label: "Hours between two marketing messages", Type: SettingTypeInt, Default: "4", Min: settingBound(0),
		Description: "A second marketing message to the same customer waits at least this long after the first."})
	reg(SettingDefinition{Key: "crm.quiet_start", Label: "Quiet hours start", Type: SettingTypeString, Default: "21:00",
		Description: "From this time (customer time zone) no marketing message is sent; it waits until quiet hours end, or expires if its campaign ends first."})
	reg(SettingDefinition{Key: "crm.quiet_end", Label: "Quiet hours end", Type: SettingTypeString, Default: "09:00",
		Description: "Marketing messages held overnight go out from this time."})
	reg(SettingDefinition{Key: "crm.attribution_days", Label: "Days a message gets credit for a bill", Type: SettingTypeInt, Default: "7", Min: settingBound(1),
		Description: "A bill is credited to the last marketing message the customer got this many days before it (once per bill; a return reduces the credit)."})
	reg(SettingDefinition{Key: "crm.test_allowlist", Label: "Test numbers and emails", Type: SettingTypeString, Default: "",
		Description: "Comma-separated phones and emails. A test send, and every send from a server that is not production, reaches only these; anyone else is recorded as NOT_ALLOWLISTED."})
	reg(SettingDefinition{Key: "crm.contact_access_roles", Label: "Roles that see full phone numbers and emails", Type: SettingTypeString, Default: "Store Manager,Cashier",
		Description: "Comma-separated role names. Every other role sees customers' phone numbers and emails masked (98******10) on screens, in exports and through the API. Administrators always see them."})
	reg(SettingDefinition{Key: "crm.campaign_second_approver", Label: "A campaign always needs a second person to approve it", Type: SettingTypeBool, Default: "false",
		Description: "On: the person who submitted a campaign can never approve it, whatever Approvals > who may approve their own documents says."})
	reg(SettingDefinition{Key: "crm.public_link_base", Label: "Web address for customer links", Type: SettingTypeString, Default: "",
		Description: "The address preference and feedback links in messages start with, for example https://minn.wholeops.in. Blank uses this server's own address when it is known."})
	reg(SettingDefinition{Key: "crm.notice_version", Label: "Privacy notice version", Type: SettingTypeString, Default: "v1",
		Description: "Stored with every consent the staff record, so you can tell which notice the customer agreed to. Change it when the notice text changes."})
	reg(SettingDefinition{Key: "crm.sms_cost_per_part", Label: "SMS cost per part", Type: SettingTypeFloat, Default: "0.25", Min: settingBound(0),
		Description: "Used to estimate and reserve a campaign's budget; a long SMS is billed per 160-character part (70 with non-English letters). The provider's own charge replaces it when reported."})
	reg(SettingDefinition{Key: "crm.whatsapp_cost", Label: "WhatsApp cost per message", Type: SettingTypeFloat, Default: "0.80", Min: settingBound(0),
		Description: "Estimate for budget reservation; replaced by the actual charge when the provider reports it."})
	reg(SettingDefinition{Key: "crm.email_cost", Label: "Email cost per message", Type: SettingTypeFloat, Default: "0", Min: settingBound(0),
		Description: "Estimate for budget reservation."})
	reg(SettingDefinition{Key: "crm.message_retention_months", Label: "Keep message records for", Type: SettingTypeInt, Default: "24", Unit: "months", Min: settingBound(1),
		Description: "Message bodies and send decisions older than this are deleted by the nightly clean-up. Consent evidence is kept for as long as the customer exists."})
	reg(SettingDefinition{Key: "crm.unknown_after_minutes", Label: "Minutes before an unanswered send is \"unknown\"", Type: SettingTypeInt, Default: "15", Min: settingBound(1),
		Description: "A message handed to the provider with no answer after this long is marked Unknown and is never resent by itself; the provider's status or a later callback settles it."})
	reg(SettingDefinition{Key: "crm.business_hours", Label: "Service desk hours", Type: SettingTypeString, Default: "10:00-20:00",
		Description: "Ticket response and resolution times count only these hours (HH:MM-HH:MM, customer time zone)."})
	reg(SettingDefinition{Key: "crm.business_days", Label: "Service desk days", Type: SettingTypeString, Default: "123456",
		Description: "Days the desk works: 1 = Monday ... 7 = Sunday."})
	reg(SettingDefinition{Key: "crm.ticket_first_response_hours", Label: "First response within", Type: SettingTypeInt, Default: "4", Unit: "working hours", Min: settingBound(1),
		Description: "A ticket not answered within this many working hours shows as late."})
	reg(SettingDefinition{Key: "crm.ticket_resolution_hours", Label: "Resolve within", Type: SettingTypeInt, Default: "48", Unit: "working hours", Min: settingBound(1),
		Description: "A ticket not resolved within this many working hours shows as late."})
	reg(SettingDefinition{Key: "crm.voucher_reservation_minutes", Label: "Hold a code for a checkout for", Type: SettingTypeInt, Default: "30", Unit: "minutes", Min: settingBound(1),
		Description: "While one till or the web shop holds a single-use code, no other checkout can take it. The hold lapses after this long if the sale is not finished."})
	reg(SettingDefinition{Key: "crm.voucher_on_cancel", Label: "A cancelled bill's single-use code", Type: SettingTypeSelect, Default: "reinstate",
		Options:     []SettingOption{{Value: "reinstate", Label: "Can be used again, if it has not expired"}, {Value: "void", Label: "Is used up"}},
		Description: "What happens to a single-use voucher code when the bill it was used on is cancelled or fully returned."})
	reg(SettingDefinition{Key: "crm.referral_reward_points", Label: "Points for a successful referral", Type: SettingTypeInt, Default: "100", Min: settingBound(0),
		Description: "Given to the customer who referred a friend, once the friend's first bill has passed the return window (Sales & Returns > return window)."})
	reg(SettingDefinition{Key: "loyalty.pending_points", Label: "Points wait for the return window", Type: SettingTypeBool, Default: "false",
		Description: "On: points earned on a bill show as pending and can be redeemed only once the bill's return window (Sales & Returns) has passed; a return inside the window cancels them. Off: points can be used at once and are taken back on a return."})
}

// --- time zone ----------------------------------------------------------

var crmZoneCache sync.Map // name -> *time.Location

// crmLocation is the client's CRM time zone; an unknown name falls back to
// India Standard Time, the default, rather than failing a send.
func crmLocation(schema string) *time.Location {
	name := strings.TrimSpace(GetSettingStringForSchema(schema, "crm.time_zone"))
	if name == "" {
		name = "Asia/Kolkata"
	}
	if loc, ok := crmZoneCache.Load(name); ok {
		return loc.(*time.Location)
	}
	loc, err := time.LoadLocation(name)
	if err != nil {
		loc = time.FixedZone("IST", 5*3600+1800)
	}
	crmZoneCache.Store(name, loc)
	return loc
}

// crmZoneName is the zone name for SQL (AT TIME ZONE); Postgres knows the
// same IANA names.
func crmZoneName(schema string) string {
	return crmLocation(schema).String()
}

// crmLocalTodaySQL is today's date in the client's zone, as SQL.
func crmLocalTodaySQL(zone string) string {
	return fmt.Sprintf("(NOW() AT TIME ZONE '%s')::date", strings.ReplaceAll(zone, "'", ""))
}

// crmDocLocalSQL converts a documents timestamp column (session-local
// TIMESTAMP, see the ERP timestamp note) to the client's wall clock.
func crmDocLocalSQL(col, zone string) string {
	return fmt.Sprintf("((%s AT TIME ZONE current_setting('TimeZone')) AT TIME ZONE '%s')", col, strings.ReplaceAll(zone, "'", ""))
}

// --- hashing, tokens ------------------------------------------------------

// crmHash is the one-way form a phone or email is kept in once the person
// behind it is erased or unsubscribed: enough to recognise it again, never
// enough to read it back.
func crmHash(kind, value string) string {
	v := strings.ToLower(strings.TrimSpace(value))
	if kind == "phone" {
		v = strings.Map(func(r rune) rune {
			if (r >= '0' && r <= '9') || r == '+' {
				return r
			}
			return -1
		}, v)
	}
	sum := sha256.Sum256([]byte("crm:" + kind + ":" + v))
	return hex.EncodeToString(sum[:])
}

// crmToken is a random, URL-safe token of n random bytes.
func crmToken(n int) (string, error) {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

func crmTokenHash(token string) string {
	sum := sha256.Sum256([]byte(strings.TrimSpace(token)))
	return hex.EncodeToString(sum[:])
}

// crmCode is an unguessable code from an alphabet with no look-alike letters
// (no 0/O, 1/I/L), for vouchers and referral codes read out over a counter.
func crmCode(prefix string, n int) (string, error) {
	const alphabet = "ABCDEFGHJKMNPQRSTUVWXYZ23456789"
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	out := make([]byte, n)
	for i := range b {
		out[i] = alphabet[int(b[i])%len(alphabet)]
	}
	return prefix + string(out), nil
}

// --- masking ----------------------------------------------------------------

func crmMaskPhone(p string) string {
	digits := strings.Map(func(r rune) rune {
		if r >= '0' && r <= '9' {
			return r
		}
		return -1
	}, p)
	if len(digits) <= 4 {
		return strings.Repeat("*", len(digits))
	}
	return digits[:2] + strings.Repeat("*", len(digits)-4) + digits[len(digits)-2:]
}

func crmMaskEmail(e string) string {
	e = strings.TrimSpace(e)
	at := strings.LastIndex(e, "@")
	if at <= 0 {
		if e == "" {
			return ""
		}
		return "***"
	}
	return e[:1] + "***" + e[at:]
}

// RoleSeesContacts reports whether a role sees customers' phone numbers and
// emails in full (101.1.4, the blueprint's M06-R03). Full-access roles
// always do; others only when named in crm.contact_access_roles.
func RoleSeesContacts(tenantID, role string) bool {
	if HasFullAccess(role) {
		return true
	}
	for _, r := range strings.Split(GetSettingString(tenantID, "crm.contact_access_roles"), ",") {
		if r = strings.TrimSpace(r); r != "" && normalizeRoleKey(r) == normalizeRoleKey(role) {
			return true
		}
	}
	return false
}

// MaskCustomerContacts masks a Customer record's phone and email in place
// for a role without contact access. It is applied where every Customer read
// already passes (FilterFieldsForRole), so the record screen, lists, exports
// and the API agree.
func MaskCustomerContacts(tenantID, role string, data map[string]interface{}) {
	if data == nil || RoleSeesContacts(tenantID, role) {
		return
	}
	if p := strField(data, "phone"); p != "" {
		data["phone"] = crmMaskPhone(p)
	}
	if e := strField(data, "email"); e != "" {
		data["email"] = crmMaskEmail(e)
	}
}

// --- customers --------------------------------------------------------------

type crmCustomer struct {
	ID     string
	Status string
	Data   map[string]interface{}
}

func (c crmCustomer) Name() string { return strField(c.Data, "name") }

// crmLoadCustomer reads one Customer by id or code.
func crmLoadCustomer(schema, id string) (crmCustomer, error) {
	var raw []byte
	var c crmCustomer
	err := db.DB.QueryRow(fmt.Sprintf(`SELECT id, status, data FROM %s.documents
		WHERE doctype = 'Customer' AND deleted_at IS NULL AND (id = $1 OR data->>'code' = $1) ORDER BY (id = $1) DESC LIMIT 1`, schema), strings.TrimSpace(id)).
		Scan(&c.ID, &c.Status, &raw)
	if errors.Is(err, sql.ErrNoRows) {
		return c, ErrCRMNotFound
	}
	if err != nil {
		return c, err
	}
	_ = json.Unmarshal(raw, &c.Data)
	if c.Data == nil {
		c.Data = map[string]interface{}{}
	}
	return c, nil
}

// crmContact is where a message on channel would go for a customer: the
// address, its masked form and its hash. ok=false when there is none.
func crmContact(schema string, data map[string]interface{}, channel string) (address, masked, hash string, ok bool) {
	switch channel {
	case CRMChannelEmail:
		e := strings.TrimSpace(strField(data, "email"))
		if e == "" || !strings.Contains(e, "@") {
			return "", "", "", false
		}
		return e, crmMaskEmail(e), crmHash("email", e), true
	case CRMChannelSMS, CRMChannelWhatsApp:
		raw := strings.TrimSpace(strField(data, "phone"))
		if raw == "" {
			return "", "", "", false
		}
		country := strField(data, "phone_country")
		if country == "" {
			if tid, err := tenantIDForSchema(schema); err == nil {
				country = TenantPhoneCountry(tid)
			}
		}
		p := NormalizePhone(raw, country)
		if !p.Valid || p.E164 == "" {
			return "", "", "", false
		}
		return p.E164, crmMaskPhone(p.E164), crmHash("phone", p.E164), true
	}
	return "", "", "", false
}

// crmPhoneHashes is every hash a raw phone might have been suppressed under
// (as typed and as E.164), for the erasure check on a new record.
func crmPhoneHashes(tenantID, raw string) []string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil
	}
	out := []string{crmHash("phone", raw)}
	if p := NormalizePhone(raw, TenantPhoneCountry(tenantID)); p.Valid && p.E164 != "" {
		out = append(out, crmHash("phone", p.E164))
	}
	return out
}

// crmAppendHistory is a JSON array entry for a history column.
func crmHistoryEntry(status, detail string) []byte {
	b, _ := json.Marshal([]map[string]string{{"at": time.Now().UTC().Format(time.RFC3339), "status": status, "detail": detail}})
	return b
}

func crmJSON(v interface{}) []byte {
	b, err := json.Marshal(v)
	if err != nil {
		return []byte("null")
	}
	return b
}

func crmString(v interface{}) string {
	switch t := v.(type) {
	case nil:
		return ""
	case string:
		return strings.TrimSpace(t)
	case float64:
		if t == float64(int64(t)) {
			return fmt.Sprintf("%d", int64(t))
		}
		return fmt.Sprintf("%g", t)
	}
	return strings.TrimSpace(fmt.Sprint(v))
}

func crmSchema(tenantID string) (string, error) {
	return db.GetTenantSchema(tenantID)
}

// crmBusinessName is the client's name as customers know it (public.tenants).
func crmBusinessName(schema string) string {
	var name string
	if err := db.DB.QueryRow(`SELECT COALESCE(name, '') FROM public.tenants WHERE schema_name = $1`, schema).Scan(&name); err != nil {
		return ""
	}
	return strings.TrimSpace(name)
}
