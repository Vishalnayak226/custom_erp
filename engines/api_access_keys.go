package engines

import (
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net"
	"regexp"
	"sort"
	"strings"
	"time"

	"custom_erp/db"
)

// API access for every module (205.3-205.5). A key now carries an owner, the
// modules and document types it was given, and a few limits. What a call may do
// is always the intersection of what the key was given, what the plan holds and
// what the owner may do today, so a key never reaches further than its owner.

const apiKeyV2Marker = "erp_v2_"

var apiTenantInKey = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]{0,60}$`)

// Refusal codes, one per cause (internal/server/error_catalog_extensions.go).
const (
	APIKeyOwnerInactive    = "APIKEY-0501"
	APIKeyModuleNotInPlan  = "APIKEY-0502"
	APIKeyModuleNotOnKey   = "APIKEY-0503"
	APIKeyOwnerLostModule  = "APIKEY-0504"
	APIKeyDoctypeNotShown  = "APIKEY-0505"
	APIKeyDoctypeNotOnKey  = "APIKEY-0506"
	APIKeyNoApproval       = "APIKEY-0507"
	APIKeyIPNotAllowed     = "APIKEY-0508"
	APIKeyExpired          = "APIKEY-0509"
	APIKeyOldSecretEnded   = "APIKEY-0510"
	APIKeyStoreNotAllowed  = "APIKEY-0511"
	APIKeyPendingClaim     = "APIKEY-0512"
	APIKeyActionNotAllowed = "APIKEY-0513"
	APIKeyInvalidRequest   = "APIKEY-0514"
	APIKeyWebhookURL       = "APIKEY-0515"
	APIKeyActionUnknown    = "APIKEY-0516"
	APIKeyReportUnknown    = "APIKEY-0517"
	APIKeyServerFault      = "APIKEY-0518"
)

// APIRefusal is a call turned away by the access check, with its catalogue code.
type APIRefusal struct {
	Code   string
	Detail string
}

func (e *APIRefusal) Error() string { return e.Code + ": " + e.Detail }

// APIKeyAccess is what a key was given, beyond its scopes.
type APIKeyAccess struct {
	OwnerUserID    string
	Modules        map[string]string
	Doctypes       map[string][]string
	CanApprove     bool
	IPAllow        []string
	Stores         []string
	OldKeyEndsAt   *time.Time
	ClaimState     string
	ClaimExpiresAt *time.Time
	QuickStart     string
	RateLimit      int
	DailyQuota     int
}

type apiKeyRowJSON struct {
	ID             string              `json:"id"`
	Name           string              `json:"name"`
	KeyPrefix      string              `json:"key_prefix"`
	Scopes         []string            `json:"scopes"`
	Status         string              `json:"status"`
	ExpiresAt      *string             `json:"expires_at"`
	LastUsedAt     *string             `json:"last_used_at"`
	RevokedAt      *string             `json:"revoked_at"`
	AuthMode       string              `json:"auth_mode"`
	CreatedBy      string              `json:"created_by"`
	CreatedAt      string              `json:"created_at"`
	OwnerUserID    *string             `json:"owner_user_id"`
	Modules        map[string]string   `json:"modules"`
	Doctypes       map[string][]string `json:"doctypes"`
	CanApprove     bool                `json:"can_approve"`
	IPAllow        []string            `json:"ip_allow"`
	Stores         []string            `json:"stores"`
	OldKeyEndsAt   *string             `json:"old_key_ends_at"`
	ClaimState     *string             `json:"claim_state"`
	ClaimExpiresAt *string             `json:"claim_expires_at"`
	QuickStart     *string             `json:"quick_start"`
	RateLimit      *int                `json:"rate_limit_per_minute"`
	DailyQuota     *int                `json:"daily_quota"`
}

func parseAPITime(s *string) *time.Time {
	if s == nil || strings.TrimSpace(*s) == "" {
		return nil
	}
	for _, layout := range []string{time.RFC3339Nano, "2006-01-02T15:04:05.999999999", "2006-01-02T15:04:05.999999999Z07"} {
		if t, err := time.Parse(layout, *s); err == nil {
			return &t
		}
	}
	return nil
}

func derefStr(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

func (j apiKeyRowJSON) access() *APIKeyAccess {
	a := &APIKeyAccess{
		OwnerUserID: derefStr(j.OwnerUserID), Modules: j.Modules, Doctypes: j.Doctypes,
		CanApprove: j.CanApprove, IPAllow: j.IPAllow, Stores: j.Stores,
		OldKeyEndsAt: parseAPITime(j.OldKeyEndsAt), ClaimState: derefStr(j.ClaimState),
		ClaimExpiresAt: parseAPITime(j.ClaimExpiresAt), QuickStart: derefStr(j.QuickStart),
	}
	if j.RateLimit != nil {
		a.RateLimit = *j.RateLimit
	}
	if j.DailyQuota != nil {
		a.DailyQuota = *j.DailyQuota
	}
	return a
}

// loadAPIKeyRow reads one credential through to_jsonb, so a schema the 205.4
// migration has not reached yet still answers (every new column reads as empty).
func loadAPIKeyRow(schema, credentialID string) (*apiKeyRowJSON, error) {
	var raw []byte
	err := db.DB.QueryRow(fmt.Sprintf(`SELECT to_jsonb(c) FROM %s.api_credentials c WHERE id = $1`, schema), credentialID).Scan(&raw)
	if err == sql.ErrNoRows {
		return nil, &ValidationError{Code: "GLOBAL-0004", Message: "API key not found"}
	}
	if err != nil {
		return nil, err
	}
	var row apiKeyRowJSON
	if err := json.Unmarshal(raw, &row); err != nil {
		return nil, err
	}
	return &row, nil
}

// LoadAPIKeyAccess is what the middleware reads once per call.
func LoadAPIKeyAccess(tenantID, credentialID string) (*APIKeyAccess, error) {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return nil, err
	}
	row, err := loadAPIKeyRow(schema, credentialID)
	if err != nil {
		return nil, err
	}
	return row.access(), nil
}

// ---- keys that carry their company (D10) --------------------------------

func generatePublicAPIKeyForTenant(tenantID string) (rawKey, keyPrefix, secretHash string, err error) {
	if !apiTenantInKey.MatchString(tenantID) {
		return generatePublicAPIKey()
	}
	prefixBytes := make([]byte, 8)
	secretBytes := make([]byte, 32)
	if _, err = rand.Read(prefixBytes); err != nil {
		return "", "", "", err
	}
	if _, err = rand.Read(secretBytes); err != nil {
		return "", "", "", err
	}
	keyPrefix = hex.EncodeToString(prefixBytes)
	rawKey = apiKeyV2Marker + tenantID + "_" + keyPrefix + "_" + hex.EncodeToString(secretBytes)
	digest := sha256.Sum256([]byte(rawKey))
	return rawKey, keyPrefix, hex.EncodeToString(digest[:]), nil
}

// parseV2PublicAPIKey splits erp_v2_<tenant>_<prefix16>_<secret64> from the
// right, so a company code that holds underscores still parses.
func parseV2PublicAPIKey(rawKey string) (tenant, prefix string, err error) {
	rest := strings.TrimPrefix(rawKey, apiKeyV2Marker)
	if len(rest) < 1+1+16+1+64 {
		return "", "", ErrInvalidAPICredential
	}
	secret := rest[len(rest)-64:]
	if rest[len(rest)-65] != '_' {
		return "", "", ErrInvalidAPICredential
	}
	head := rest[:len(rest)-65]
	if len(head) < 18 || head[len(head)-17] != '_' {
		return "", "", ErrInvalidAPICredential
	}
	prefix = head[len(head)-16:]
	tenant = head[:len(head)-17]
	if !apiTenantInKey.MatchString(tenant) {
		return "", "", ErrInvalidAPICredential
	}
	if _, e := hex.DecodeString(prefix); e != nil {
		return "", "", ErrInvalidAPICredential
	}
	if _, e := hex.DecodeString(secret); e != nil {
		return "", "", ErrInvalidAPICredential
	}
	return tenant, prefix, nil
}

// PublicAPIKeyTenant returns the company code a new-format key carries, or ""
// for an old key (which needs the X-Tenant-ID header).
func PublicAPIKeyTenant(rawKey string) string {
	rawKey = strings.TrimSpace(rawKey)
	if !strings.HasPrefix(rawKey, apiKeyV2Marker) {
		return ""
	}
	tenant, _, err := parseV2PublicAPIKey(rawKey)
	if err != nil {
		return ""
	}
	return tenant
}

// DiagnoseRefusedAPIKey runs only after authentication failed. It re-reads the
// key and, only when the secret itself proves right, says why the key stopped:
// expired, or waiting to be claimed. A wrong secret learns nothing.
func DiagnoseRefusedAPIKey(tenantID, rawKey string) string {
	rawKey = strings.TrimSpace(rawKey)
	prefix, err := publicAPIKeyPrefix(rawKey)
	if err != nil {
		return ""
	}
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return ""
	}
	var hash, status string
	var expires sql.NullTime
	err = db.DB.QueryRow(fmt.Sprintf(`SELECT secret_hash, status, expires_at FROM %s.api_credentials WHERE key_prefix = $1`, schema), prefix).Scan(&hash, &status, &expires)
	if err != nil || !publicAPIKeyMatches(rawKey, hash) {
		return ""
	}
	if status == "Active" && expires.Valid && !expires.Time.After(time.Now()) {
		return APIKeyExpired
	}
	return ""
}

// ---- who may do what ------------------------------------------------------

// CheckAPIKeyCall applies every ceiling to one call: the IP list, the old
// secret, a link not yet claimed, the owner still active, the module in the
// plan and on the key, the document type shown and on the key, and the owner's
// own rights today. action is read, create, update or delete; an empty
// doctype checks only the key-level rules (/me).
func CheckAPIKeyCall(tenantID string, cred *APICredential, access *APIKeyAccess, doctype, action, clientIP string) (*APIRefusal, error) {
	if access == nil {
		return nil, nil
	}
	if len(access.IPAllow) > 0 && !ipAllowed(access.IPAllow, clientIP) {
		return &APIRefusal{APIKeyIPNotAllowed, "address " + clientIP + " is not on this key's list"}, nil
	}
	if access.OldKeyEndsAt != nil && !access.OldKeyEndsAt.After(time.Now()) {
		return &APIRefusal{APIKeyOldSecretEnded, "the old secret ended " + access.OldKeyEndsAt.UTC().Format(time.RFC3339)}, nil
	}
	if access.ClaimState == "pending" {
		return &APIRefusal{APIKeyPendingClaim, "the developer link has not been opened"}, nil
	}
	var state LiveUserState
	if access.OwnerUserID != "" {
		var err error
		state, err = ResolveLiveUserState(tenantID, access.OwnerUserID)
		if err != nil {
			if IsUserNotActiveError(err) {
				return &APIRefusal{APIKeyOwnerInactive, "the key's owner is not active"}, nil
			}
			return nil, err
		}
		for _, store := range access.Stores {
			if !state.Access.Allows(store) {
				return &APIRefusal{APIKeyStoreNotAllowed, "the owner no longer works at store " + store}, nil
			}
		}
	}
	if doctype == "" {
		return nil, nil
	}
	module, err := ModuleForDoctype(tenantID, doctype)
	if err != nil {
		return nil, err
	}
	if module == "" {
		return &APIRefusal{APIKeyDoctypeNotShown, doctype + " is not a known document type"}, nil
	}
	enabled, err := IsModuleEnabled(tenantID, module)
	if err != nil {
		return nil, err
	}
	if !enabled {
		return &APIRefusal{APIKeyModuleNotInPlan, "module " + module + " is not in this plan"}, nil
	}
	write := action != "read"
	if !credentialHoldsModule(cred, module, write) {
		return &APIRefusal{APIKeyModuleNotOnKey, "the key has no " + map[bool]string{true: "Change", false: "View"}[write] + " access to module " + module}, nil
	}
	class := APIDoctypeClass(doctype)
	if class == APIDoctypeNever || (write && class != APIDoctypeRW) {
		return &APIRefusal{APIKeyDoctypeNotShown, doctype + " cannot be " + map[bool]string{true: "changed", false: "read"}[write] + " through the API"}, nil
	}
	if list := access.Doctypes[module]; len(list) > 0 && !containsString(list, doctype) {
		return &APIRefusal{APIKeyDoctypeNotOnKey, "the key was not given " + doctype}, nil
	}
	if access.OwnerUserID != "" {
		ok, err := RoleMayOnDoctype(tenantID, state.Role, doctype, action)
		if err != nil {
			return nil, err
		}
		if !ok {
			return &APIRefusal{APIKeyOwnerLostModule, "the owner may not " + action + " " + doctype}, nil
		}
	}
	return nil, nil
}

func credentialHoldsModule(cred *APICredential, module string, write bool) bool {
	if cred == nil {
		return false
	}
	for _, scope := range cred.Scopes {
		if scope == module+":write" || (!write && scope == module+":read") {
			return true
		}
	}
	return false
}

func ipAllowed(list []string, clientIP string) bool {
	ip := net.ParseIP(strings.TrimSpace(clientIP))
	if ip == nil {
		return false
	}
	for _, entry := range list {
		entry = strings.TrimSpace(entry)
		if _, cidr, err := net.ParseCIDR(entry); err == nil {
			if cidr.Contains(ip) {
				return true
			}
		} else if other := net.ParseIP(entry); other != nil && other.Equal(ip) {
			return true
		}
	}
	return false
}

// OwnerStateForKey returns the owner's live role, location and store access.
func OwnerStateForKey(tenantID string, access *APIKeyAccess) (LiveUserState, error) {
	if access == nil || access.OwnerUserID == "" {
		return LiveUserState{}, errUserNotActive
	}
	return ResolveLiveUserState(tenantID, access.OwnerUserID)
}

// ---- module catalogue for the screen -------------------------------------

type APIAccessDoctype struct {
	Doctype string `json:"doctype"`
	Label   string `json:"label"`
	Class   string `json:"class"`
}

type APIAccessModule struct {
	Key       string             `json:"key"`
	Name      string             `json:"name"`
	Summary   string             `json:"summary"`
	InPlan    bool               `json:"in_plan"`
	CanView   bool               `json:"can_view"`
	CanChange bool               `json:"can_change"`
	Reason    string             `json:"reason"`
	Doctypes  []APIAccessDoctype `json:"doctypes"`
}

var camelBreak = regexp.MustCompile(`([a-z0-9])([A-Z])`)

func apiDoctypeLabel(name string) string { return camelBreak.ReplaceAllString(name, "$1 $2") }

type rolePerm struct{ read, create, update, del bool }

func rolePermissionFlags(tenantID, role string) (map[string]rolePerm, error) {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return nil, err
	}
	rows, err := db.DB.Query(fmt.Sprintf(`SELECT doctype_name, COALESCE(allow_read,false), COALESCE(allow_create,false),
		COALESCE(allow_update,false), COALESCE(allow_delete,false) FROM %s.role_permissions WHERE role = $1`, schema), role)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]rolePerm{}
	for rows.Next() {
		var name string
		var p rolePerm
		if err := rows.Scan(&name, &p.read, &p.create, &p.update, &p.del); err != nil {
			return nil, err
		}
		out[name] = p
	}
	return out, rows.Err()
}

// APIAccessModules lists every module with what the given role may hand out.
func APIAccessModules(tenantID, role string) ([]APIAccessModule, error) {
	modules, err := ListModules()
	if err != nil {
		return nil, err
	}
	ents, err := ListModuleEntitlements(tenantID)
	if err != nil {
		return nil, err
	}
	inPlan := map[string]bool{}
	for _, e := range ents {
		inPlan[e.ModuleKey] = e.Enabled
	}
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return nil, err
	}
	rows, err := db.DB.Query(fmt.Sprintf(`SELECT name, COALESCE(module_key,'') FROM %s.doctype_meta ORDER BY name`, schema))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	byModule := map[string][]string{}
	for rows.Next() {
		var name, module string
		if err := rows.Scan(&name, &module); err != nil {
			return nil, err
		}
		byModule[module] = append(byModule[module], name)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	full := HasFullAccess(role)
	var perms map[string]rolePerm
	if !full {
		if perms, err = rolePermissionFlags(tenantID, role); err != nil {
			return nil, err
		}
	}
	out := make([]APIAccessModule, 0, len(modules))
	for _, m := range modules {
		entry := APIAccessModule{Key: m.ModuleKey, Name: m.DisplayName, Summary: m.Description, InPlan: inPlan[m.ModuleKey], Doctypes: []APIAccessDoctype{}}
		for _, name := range byModule[m.ModuleKey] {
			class := APIDoctypeClass(name)
			if class == APIDoctypeNever {
				continue
			}
			entry.Doctypes = append(entry.Doctypes, APIAccessDoctype{Doctype: name, Label: apiDoctypeLabel(name), Class: class})
			p := perms[name]
			if full || p.read {
				entry.CanView = true
			}
			if class == APIDoctypeRW && (full || p.create || p.update) {
				entry.CanChange = true
			}
		}
		switch {
		case !entry.InPlan:
			entry.CanView, entry.CanChange = false, false
			entry.Reason = "Not in your plan"
		case len(entry.Doctypes) == 0:
			entry.CanView, entry.CanChange = false, false
			entry.Reason = "Nothing in this module is available to the API"
		case !entry.CanView:
			entry.Reason = "You do not have rights in this module"
		case !entry.CanChange:
			entry.Reason = "You can only view this module"
		}
		out = append(out, entry)
	}
	return out, nil
}

// ---- validation of what a key is given -----------------------------------

// APIKeyGrant is the module and document type choice of one key.
type APIKeyGrant struct {
	Modules    map[string]string   `json:"modules"`
	Doctypes   map[string][]string `json:"doctypes"`
	CanApprove bool                `json:"can_approve"`
}

var apiQuickStarts = map[string]map[string]string{
	"catalogue": {"pim": "read", "master_data": "read"},
	"orders":    {"oms": "write", "sales": "write"},
	"stock":     {"inventory": "read", "wms": "read"},
	"finance":   {"finance": "read"},
}

// APIKeyScopesFor turns module levels into scope names (Change implies View).
func APIKeyScopesFor(modules map[string]string) []string {
	set := map[string]bool{}
	for module, level := range modules {
		set[module+":read"] = true
		if level == "write" {
			set[module+":write"] = true
		}
	}
	out := make([]string, 0, len(set))
	for s := range set {
		out = append(out, s)
	}
	sort.Strings(out)
	return out
}

func apiBad(sub, msg string) error {
	return &ValidationError{Code: APIKeyInvalidRequest, SubFor: sub, Message: msg}
}

func apiNotAllowed(msg string) error {
	return &ValidationError{Code: APIKeyActionNotAllowed, Message: msg}
}

// validateAPIKeyGrant holds a grant to what the owner may do right now. A
// blank document type list for a module means every document type the module
// shows to keys.
func validateAPIKeyGrant(tenantID, ownerRole string, g *APIKeyGrant, quickStart string) error {
	if len(g.Modules) == 0 && quickStart != "" {
		preset, ok := apiQuickStarts[quickStart]
		if !ok {
			return apiBad("Quick start", "unknown quick start "+quickStart)
		}
		g.Modules = map[string]string{}
		for k, v := range preset {
			g.Modules[k] = v
		}
	}
	if len(g.Modules) == 0 {
		return apiBad("Modules", "tick at least one module")
	}
	catalogue, err := APIAccessModules(tenantID, ownerRole)
	if err != nil {
		return err
	}
	byKey := map[string]APIAccessModule{}
	for _, m := range catalogue {
		byKey[m.Key] = m
	}
	for module, level := range g.Modules {
		m, ok := byKey[module]
		if !ok {
			return apiBad("Modules", "unknown module "+module)
		}
		if level != "read" && level != "write" {
			return apiBad("Modules", module+" must be read or write")
		}
		if !m.InPlan {
			return &ValidationError{Code: APIKeyModuleNotInPlan, Message: module + " is not in your plan"}
		}
		if (level == "read" && !m.CanView) || (level == "write" && !m.CanChange) {
			return apiNotAllowed("you cannot give " + level + " access to " + module + ": " + m.Reason)
		}
	}
	for module, list := range g.Doctypes {
		if _, on := g.Modules[module]; !on {
			return apiBad("Document types", "document types given for "+module+", which is not ticked")
		}
		shown := map[string]bool{}
		for _, d := range byKey[module].Doctypes {
			shown[d.Doctype] = true
		}
		for _, d := range list {
			if !shown[d] {
				return apiBad("Document types", d+" is not available in "+module)
			}
		}
	}
	return nil
}

// ---- create, list, change, rotate, revoke --------------------------------

type CreateAPIKeyInput struct {
	Name       string
	QuickStart string
	Grant      APIKeyGrant
	AuthMode   string
	Expires    string
	Stores     []string
	IPAllow    []string
	Deliver    string
}

type APIKeyActor struct {
	UserID string
	Role   string
}

type APIKeyLimits struct {
	RateLimitPerMinute int `json:"rate_limit_per_minute"`
	DailyQuota         int `json:"daily_quota"`
	CallsToday         int `json:"calls_today"`
	LeftToday          int `json:"left_today"`
}

type APIKeyRow struct {
	ID           string              `json:"id"`
	Name         string              `json:"name"`
	KeyPrefix    string              `json:"key_prefix"`
	AuthMode     string              `json:"auth_mode"`
	Modules      map[string]string   `json:"modules"`
	Doctypes     map[string][]string `json:"doctypes"`
	CanApprove   bool                `json:"can_approve"`
	OwnerID      string              `json:"owner_id"`
	OwnerName    string              `json:"owner_name"`
	OwnerActive  bool                `json:"owner_active"`
	Status       string              `json:"status"`
	ExpiresAt    *time.Time          `json:"expires_at"`
	OldKeyEndsAt *time.Time          `json:"old_key_ends_at"`
	LastUsedAt   *time.Time          `json:"last_used_at"`
	CallsToday   int                 `json:"calls_today"`
	ErrorsToday  int                 `json:"errors_today"`
	Stores       []string            `json:"stores"`
	IPAllow      []string            `json:"ip_allow"`
	LegacyScopes []string            `json:"legacy_scopes"`
	Limits       *APIKeyLimits       `json:"limits,omitempty"`
}

// CreatedAPIKey is the answer to create and rotate. The key shows once.
type CreatedAPIKey struct {
	Credential    APICredential `json:"credential"`
	APIKey        string        `json:"api_key,omitempty"`
	ClientID      string        `json:"client_id,omitempty"`
	ClientSecret  string        `json:"client_secret,omitempty"`
	TenantCode    string        `json:"tenant_code"`
	BaseURL       string        `json:"base_url,omitempty"`
	DeveloperLink *struct {
		URL       string    `json:"url"`
		ExpiresAt time.Time `json:"expires_at"`
	} `json:"developer_link,omitempty"`
}

// Present gives the answer its final shape for the screen: the public base
// address, and for an OAuth client the client id and secret in place of api_key.
func (c *CreatedAPIKey) Present(publicOrigin string) *CreatedAPIKey {
	if c == nil {
		return c
	}
	if publicOrigin != "" {
		c.BaseURL = strings.TrimRight(publicOrigin, "/") + "/api/public/v1"
	}
	if c.Credential.AuthMode == "oauth_client" && c.APIKey != "" {
		c.ClientID = c.Credential.KeyPrefix
		c.ClientSecret = c.APIKey
		c.APIKey = ""
	}
	return c
}

var apiTenantCodeRE = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]{0,60}$`)

// SplitClaimToken reads a link token of the form <tenant>.<48 hex>. The tenant
// travels inside the token so the link needs nothing else. ok is false for any
// other shape, and callers answer every such token alike.
func SplitClaimToken(token string) (tenantID, secret string, ok bool) {
	token = strings.TrimSpace(token)
	i := strings.LastIndex(token, ".")
	if i <= 0 {
		return "", "", false
	}
	tenantID, secret = token[:i], token[i+1:]
	if !apiTenantCodeRE.MatchString(tenantID) || len(secret) != 48 {
		return "", "", false
	}
	return tenantID, secret, true
}

func parseAPIKeyExpiry(s string) (*time.Time, error) {
	s = strings.TrimSpace(s)
	now := time.Now()
	var t time.Time
	switch s {
	case "", "never":
		return nil, nil
	case "30d":
		t = now.AddDate(0, 0, 30)
	case "90d":
		t = now.AddDate(0, 0, 90)
	case "1y":
		t = now.AddDate(1, 0, 0)
	default:
		// The end of the picked day where the business is, not in UTC: a key
		// "until 21 Nov" must not still work on the morning of 22 Nov (ask 226).
		d, err := time.ParseInLocation("2006-01-02", s, BusinessLocation())
		if err != nil {
			return nil, apiBad("Expires", "use 30d, 90d, 1y, never or a date as YYYY-MM-DD")
		}
		t = d.AddDate(0, 0, 1).Add(-time.Second)
	}
	if !t.After(now) {
		return nil, apiBad("Expires", "the expiry must be in the future")
	}
	return &t, nil
}

func cleanIPList(list []string) ([]string, error) {
	out := []string{}
	for _, raw := range list {
		raw = strings.TrimSpace(raw)
		if raw == "" {
			continue
		}
		if _, _, err := net.ParseCIDR(raw); err != nil && net.ParseIP(raw) == nil {
			return nil, apiBad("IP allow list", raw+" is not an IP address or range")
		}
		out = append(out, raw)
	}
	return out, nil
}

func cleanKeyStoreList(list []string) []string {
	out := []string{}
	for _, s := range list {
		if s = strings.TrimSpace(s); s != "" {
			out = append(out, s)
		}
	}
	return out
}

func newClaimToken() (token, hash string, err error) {
	b := make([]byte, 24)
	if _, err = rand.Read(b); err != nil {
		return "", "", err
	}
	token = hex.EncodeToString(b)
	d := sha256.Sum256([]byte(token))
	return token, hex.EncodeToString(d[:]), nil
}

// CreateAPIKey makes a key owned by the actor, held to the actor's rights.
func CreateAPIKey(tenantID string, actor APIKeyActor, in CreateAPIKeyInput) (*CreatedAPIKey, error) {
	name := strings.TrimSpace(in.Name)
	if name == "" || len(name) > 120 {
		return nil, apiBad("Name", "give the key a name of up to 120 characters")
	}
	if in.AuthMode == "" {
		in.AuthMode = "api_key"
	}
	if in.AuthMode != "api_key" && in.AuthMode != "oauth_client" {
		return nil, apiBad("Auth mode", "use api_key or oauth_client")
	}
	if in.Deliver == "" {
		in.Deliver = "show"
	}
	if in.Deliver != "show" && in.Deliver != "developer_link" {
		return nil, apiBad("Deliver", "use show or developer_link")
	}
	if in.Deliver == "developer_link" && in.AuthMode != "api_key" {
		return nil, apiBad("Deliver", "a developer link carries an API key, not an OAuth client")
	}
	grant := in.Grant
	if err := validateAPIKeyGrant(tenantID, actor.Role, &grant, in.QuickStart); err != nil {
		return nil, err
	}
	expiresAt, err := parseAPIKeyExpiry(in.Expires)
	if err != nil {
		return nil, err
	}
	ips, err := cleanIPList(in.IPAllow)
	if err != nil {
		return nil, err
	}
	stores := cleanKeyStoreList(in.Stores)
	if state, e := ResolveLiveUserState(tenantID, actor.UserID); e == nil {
		for _, s := range stores {
			if !state.Access.Allows(s) {
				return nil, apiNotAllowed("you do not work at store " + s)
			}
		}
	}
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return nil, err
	}
	scopes := APIKeyScopesFor(grant.Modules)
	tx, err := db.DB.Begin()
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	issued, err := insertAPICredentialV2(tx, schema, tenantID, name, scopes, expiresAt, actor.UserID, nil)
	if err != nil {
		return nil, err
	}
	if err := writeAPIKeyAccessTx(tx, schema, issued.Credential.ID, actor.UserID, grant, ips, stores, in.QuickStart); err != nil {
		return nil, err
	}
	out := &CreatedAPIKey{Credential: issued.Credential, TenantCode: tenantID}
	if in.AuthMode == "oauth_client" {
		if _, err := tx.Exec(fmt.Sprintf(`UPDATE %s.api_credentials SET auth_mode='oauth_client' WHERE id=$1`, schema), issued.Credential.ID); err != nil {
			return nil, err
		}
		out.Credential.AuthMode = "oauth_client"
	}
	if in.Deliver == "developer_link" {
		token, tokenHash, err := newClaimToken()
		if err != nil {
			return nil, err
		}
		expires := time.Now().Add(48 * time.Hour)
		// The row is made but its secret is not given out; a random digest that no
		// key can match keeps it unusable until the developer opens the link.
		junk := make([]byte, 32)
		_, _ = rand.Read(junk)
		unusable := sha256.Sum256(junk)
		if _, err := tx.Exec(fmt.Sprintf(`UPDATE %s.api_credentials SET secret_hash=$2, claim_state='pending',
			claim_token_hash=$3, claim_expires_at=$4 WHERE id=$1`, schema), issued.Credential.ID, hex.EncodeToString(unusable[:]), tokenHash, expires); err != nil {
			return nil, err
		}
		if err := tx.Commit(); err != nil {
			return nil, err
		}
		out.APIKey = ""
		out.DeveloperLink = &struct {
			URL       string    `json:"url"`
			ExpiresAt time.Time `json:"expires_at"`
		}{URL: "/api-claim.html?t=" + tenantID + "." + token, ExpiresAt: expires}
		return out, nil
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	out.APIKey = issued.APIKey
	return out, nil
}

func insertAPICredentialV2(q apiCredentialQueryRower, schema, tenantID, name string, scopes []string, expiresAt *time.Time, createdBy string, rotatedFrom *string) (*IssuedAPICredential, error) {
	scopeJSON, _ := json.Marshal(scopes)
	var rotated interface{}
	if rotatedFrom != nil && *rotatedFrom != "" {
		rotated = *rotatedFrom
	}
	for attempt := 0; attempt < 3; attempt++ {
		rawKey, prefix, hash, err := generatePublicAPIKeyForTenant(tenantID)
		if err != nil {
			return nil, err
		}
		cred := APICredential{Name: name, KeyPrefix: prefix, Scopes: scopes, Status: "Active", AuthMode: "api_key", ExpiresAt: expiresAt, CreatedBy: createdBy}
		err = q.QueryRow(fmt.Sprintf(`INSERT INTO %s.api_credentials
			(name, key_prefix, secret_hash, scopes, expires_at, rotated_from, created_by)
			VALUES ($1,$2,$3,$4::jsonb,$5,$6,$7) RETURNING id, created_at`, schema),
			name, prefix, hash, scopeJSON, expiresAt, rotated, createdBy).Scan(&cred.ID, &cred.CreatedAt)
		if err == nil {
			if rotatedFrom != nil {
				cred.RotatedFrom = *rotatedFrom
			}
			return &IssuedAPICredential{Credential: cred, APIKey: rawKey,
				SecretNote: "Shown once. Store it securely now; only a one-way digest is retained."}, nil
		}
		if !strings.Contains(strings.ToLower(err.Error()), "key_prefix") {
			return nil, err
		}
	}
	return nil, fmt.Errorf("could not allocate a unique API key prefix")
}

type apiKeyExecer interface {
	Exec(query string, args ...interface{}) (sql.Result, error)
}

func writeAPIKeyAccessTx(q apiKeyExecer, schema, credentialID, ownerID string, g APIKeyGrant, ips, stores []string, quickStart string) error {
	mods, _ := json.Marshal(nonNilMap(g.Modules))
	docs, _ := json.Marshal(nonNilDoctypes(g.Doctypes))
	ipJSON, _ := json.Marshal(nonNilList(ips))
	storeJSON, _ := json.Marshal(nonNilList(stores))
	_, err := q.Exec(fmt.Sprintf(`UPDATE %s.api_credentials SET owner_user_id=$2, modules=$3::jsonb, doctypes=$4::jsonb,
		can_approve=$5, ip_allow=$6::jsonb, stores=$7::jsonb, quick_start=NULLIF($8,'') WHERE id=$1`, schema),
		credentialID, ownerID, mods, docs, g.CanApprove, ipJSON, storeJSON, quickStart)
	return err
}

func nonNilMap(m map[string]string) map[string]string {
	if m == nil {
		return map[string]string{}
	}
	return m
}
func nonNilDoctypes(m map[string][]string) map[string][]string {
	if m == nil {
		return map[string][]string{}
	}
	return m
}
func nonNilList(l []string) []string {
	if l == nil {
		return []string{}
	}
	return l
}

// ClaimAPIKey is what the developer's link does: it makes the secret and shows
// it once. The row stays unusable until now, and the link works once.
func ClaimAPIKey(tenantID, token string) (*CreatedAPIKey, error) {
	token = strings.TrimSpace(token)
	if t, secret, ok := SplitClaimToken(token); ok {
		tenantID, token = t, secret
	}
	if len(token) != 48 || !apiTenantCodeRE.MatchString(tenantID) {
		return nil, ErrInvalidAPICredential
	}
	d := sha256.Sum256([]byte(token))
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return nil, ErrInvalidAPICredential
	}
	tx, err := db.DB.Begin()
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	var id, name string
	var expires sql.NullTime
	err = tx.QueryRow(fmt.Sprintf(`SELECT id::text, name, claim_expires_at FROM %s.api_credentials
		WHERE claim_state='pending' AND claim_token_hash=$1 AND status='Active' FOR UPDATE`, schema), hex.EncodeToString(d[:])).Scan(&id, &name, &expires)
	if err != nil || !expires.Valid || !expires.Time.After(time.Now()) {
		return nil, ErrInvalidAPICredential
	}
	rawKey, prefix, hash, err := generatePublicAPIKeyForTenant(tenantID)
	if err != nil {
		return nil, err
	}
	if _, err := tx.Exec(fmt.Sprintf(`UPDATE %s.api_credentials SET key_prefix=$2, secret_hash=$3, claim_state='claimed',
		claim_token_hash=NULL, claimed_at=CURRENT_TIMESTAMP WHERE id=$1`, schema), id, prefix, hash); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return &CreatedAPIKey{Credential: APICredential{ID: id, Name: name, KeyPrefix: prefix, Status: "Active", AuthMode: "api_key"}, APIKey: rawKey, TenantCode: tenantID}, nil
}

func apiKeyOwnerNames(schema string, ids []string) (map[string]string, map[string]bool, error) {
	names, active := map[string]string{}, map[string]bool{}
	if len(ids) == 0 {
		return names, active, nil
	}
	rows, err := db.DB.Query(fmt.Sprintf(`SELECT id, username, COALESCE(status,'') FROM %s.users WHERE id = ANY($1::text[])`, schema), "{"+strings.Join(quoteAll(ids), ",")+"}")
	if err != nil {
		return nil, nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var id, username, status string
		if err := rows.Scan(&id, &username, &status); err != nil {
			return nil, nil, err
		}
		names[id], active[id] = username, status == "Active"
	}
	return names, active, rows.Err()
}

func quoteAll(ids []string) []string {
	out := make([]string, len(ids))
	for i, id := range ids {
		out[i] = `"` + strings.NewReplacer(`\`, `\\`, `"`, `\"`).Replace(id) + `"`
	}
	return out
}

func apiKeyStatus(j apiKeyRowJSON, access *APIKeyAccess, ownerActive bool, now time.Time) string {
	switch {
	case j.Status == "Revoked":
		return "revoked"
	case access.ClaimState == "pending":
		return "pending_claim"
	}
	if exp := parseAPITime(j.ExpiresAt); exp != nil && !exp.After(now) {
		return "expired"
	}
	if access.OldKeyEndsAt != nil {
		if access.OldKeyEndsAt.After(now) {
			return "rotating"
		}
		return "expired"
	}
	if access.OwnerUserID != "" && !ownerActive {
		return "owner_inactive"
	}
	if exp := parseAPITime(j.ExpiresAt); exp != nil && exp.Before(now.AddDate(0, 0, 14)) {
		return "expiring"
	}
	return "active"
}

// ListAPIKeys returns the keys the actor may see: their own, or every key for
// an administrator who asks for scope all.
func ListAPIKeys(tenantID, actorID string, all bool, statusFilter string) ([]APIKeyRow, error) {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return nil, err
	}
	rows, err := db.DB.Query(fmt.Sprintf(`SELECT to_jsonb(c) FROM %s.api_credentials c ORDER BY created_at DESC`, schema))
	if err != nil {
		return nil, err
	}
	var parsed []apiKeyRowJSON
	for rows.Next() {
		var raw []byte
		var j apiKeyRowJSON
		if err := rows.Scan(&raw); err != nil {
			rows.Close()
			return nil, err
		}
		if err := json.Unmarshal(raw, &j); err != nil {
			rows.Close()
			return nil, err
		}
		if all || derefStr(j.OwnerUserID) == actorID {
			parsed = append(parsed, j)
		}
	}
	rows.Close()
	ownerIDs := []string{}
	for _, j := range parsed {
		if id := derefStr(j.OwnerUserID); id != "" {
			ownerIDs = append(ownerIDs, id)
		}
	}
	names, active, err := apiKeyOwnerNames(schema, ownerIDs)
	if err != nil {
		return nil, err
	}
	calls, errs := map[string]int{}, map[string]int{}
	crow, err := db.DB.Query(fmt.Sprintf(`SELECT credential_id::text, COUNT(*), COUNT(*) FILTER (WHERE status_code >= 400)
		FROM %s.api_request_log WHERE created_at >= public.erp_day_start(public.erp_today()) AND credential_id IS NOT NULL GROUP BY credential_id`, schema))
	if err == nil {
		for crow.Next() {
			var id string
			var n, e int
			if crow.Scan(&id, &n, &e) == nil {
				calls[id], errs[id] = n, e
			}
		}
		crow.Close()
	}
	now := time.Now()
	out := make([]APIKeyRow, 0, len(parsed))
	for _, j := range parsed {
		access := j.access()
		owner := access.OwnerUserID
		row := APIKeyRow{
			ID: j.ID, Name: j.Name, KeyPrefix: j.KeyPrefix, AuthMode: j.AuthMode,
			Modules: nonNilMap(j.Modules), Doctypes: nonNilDoctypes(j.Doctypes), CanApprove: j.CanApprove,
			OwnerID: owner, OwnerName: names[owner], OwnerActive: active[owner],
			ExpiresAt: parseAPITime(j.ExpiresAt), OldKeyEndsAt: access.OldKeyEndsAt, LastUsedAt: parseAPITime(j.LastUsedAt),
			CallsToday: calls[j.ID], ErrorsToday: errs[j.ID], Stores: nonNilList(j.Stores), IPAllow: nonNilList(j.IPAllow),
			LegacyScopes: legacyScopesOf(j.Scopes),
		}
		row.Status = apiKeyStatus(j, access, active[owner], now)
		if statusFilter != "" && statusFilter != row.Status {
			continue
		}
		out = append(out, row)
	}
	return out, nil
}

func legacyScopesOf(scopes []string) []string {
	out := []string{}
	for _, s := range scopes {
		for _, legacy := range PublicAPIScopeCatalog {
			if s == legacy {
				out = append(out, s)
			}
		}
	}
	return out
}

// GetAPIKey is one row plus its limits.
func GetAPIKey(tenantID, actorID string, all bool, credentialID string) (*APIKeyRow, error) {
	rows, err := ListAPIKeys(tenantID, actorID, all, "")
	if err != nil {
		return nil, err
	}
	for i := range rows {
		if rows[i].ID == credentialID {
			perMin, perDay, err := ResolvePublicAPICredentialLimits(tenantID, credentialID)
			if err != nil {
				return nil, err
			}
			left := perDay - rows[i].CallsToday
			if left < 0 {
				left = 0
			}
			rows[i].Limits = &APIKeyLimits{RateLimitPerMinute: perMin, DailyQuota: perDay, CallsToday: rows[i].CallsToday, LeftToday: left}
			return &rows[i], nil
		}
	}
	return nil, &ValidationError{Code: "GLOBAL-0004", Message: "API key not found"}
}

// UpdateAPIKeyModules changes what a key was given. Widening is held to the
// owner's rights today, not the editor's, and is audited by the caller.
func UpdateAPIKeyModules(tenantID, credentialID string, g APIKeyGrant) error {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return err
	}
	row, err := loadAPIKeyRow(schema, credentialID)
	if err != nil {
		return err
	}
	if row.Status != "Active" {
		return &ValidationError{Code: "GLOBAL-0004", Message: "active API key not found"}
	}
	ownerRole := ""
	if owner := derefStr(row.OwnerUserID); owner != "" {
		state, err := ResolveLiveUserState(tenantID, owner)
		if err != nil {
			return &ValidationError{Code: APIKeyOwnerInactive, Message: "the key's owner is not active"}
		}
		ownerRole = state.Role
	}
	if err := validateAPIKeyGrant(tenantID, ownerRole, &g, ""); err != nil {
		return err
	}
	scopes := APIKeyScopesFor(g.Modules)
	for _, legacy := range legacyScopesOf(row.Scopes) {
		scopes = append(scopes, legacy)
	}
	scopes = dedupSorted(scopes)
	scopeJSON, _ := json.Marshal(scopes)
	mods, _ := json.Marshal(nonNilMap(g.Modules))
	docs, _ := json.Marshal(nonNilDoctypes(g.Doctypes))
	_, err = db.DB.Exec(fmt.Sprintf(`UPDATE %s.api_credentials SET scopes=$2::jsonb, modules=$3::jsonb, doctypes=$4::jsonb, can_approve=$5
		WHERE id=$1 AND status='Active'`, schema), credentialID, scopeJSON, mods, docs, g.CanApprove)
	return err
}

func dedupSorted(in []string) []string {
	set := map[string]bool{}
	for _, s := range in {
		set[s] = true
	}
	out := make([]string, 0, len(set))
	for s := range set {
		out = append(out, s)
	}
	sort.Strings(out)
	return out
}

// ParseAPIOverlap turns now|1h|24h|7d into a duration; 0 means cut at once.
func ParseAPIOverlap(s string) (time.Duration, error) {
	switch strings.TrimSpace(s) {
	case "now":
		return 0, nil
	case "1h":
		return time.Hour, nil
	case "", "24h":
		return 24 * time.Hour, nil
	case "7d":
		return 7 * 24 * time.Hour, nil
	}
	return 0, apiBad("Overlap", "use now, 1h, 24h or 7d")
}

// RotateAPIKey makes a new secret for the same key settings. The old secret
// keeps working for the overlap, then stops (APIKEY-0510).
func RotateAPIKey(tenantID, credentialID, rotatedBy string, overlap time.Duration) (*CreatedAPIKey, error) {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return nil, err
	}
	row, err := loadAPIKeyRow(schema, credentialID)
	if err != nil {
		return nil, err
	}
	// 227.2: a key still waiting for its developer link has no secret to rotate.
	if row.ClaimState != nil && *row.ClaimState == "pending" {
		return nil, &ValidationError{Code: APIKeyPendingClaim, Message: "the developer has not opened the link yet; wait for them, or revoke this key and make a new one"}
	}
	if row.Status != "Active" || row.AuthMode != "api_key" || overlap == 0 {
		issued, err := RotateAPICredential(tenantID, credentialID, rotatedBy)
		if err != nil {
			return nil, err
		}
		// RotateAPICredential makes an old-format key; give the replacement its
		// settings so it is not left without an owner.
		if row.OwnerUserID != nil || row.Modules != nil {
			_ = copyAPIKeyAccess(schema, credentialID, issued.Credential.ID)
		}
		return &CreatedAPIKey{Credential: issued.Credential, APIKey: issued.APIKey, TenantCode: tenantID}, nil
	}
	if exp := parseAPITime(row.ExpiresAt); exp != nil && !exp.After(time.Now()) {
		return nil, apiBad("Expires", "an expired key cannot be rotated; make a new key")
	}
	tx, err := db.DB.Begin()
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	issued, err := insertAPICredentialV2(tx, schema, tenantID, row.Name, row.Scopes, parseAPITime(row.ExpiresAt), rotatedBy, &credentialID)
	if err != nil {
		return nil, err
	}
	if _, err := tx.Exec(fmt.Sprintf(`UPDATE %s.api_credentials n SET owner_user_id=o.owner_user_id, modules=o.modules,
		doctypes=o.doctypes, can_approve=o.can_approve, ip_allow=o.ip_allow, stores=o.stores, quick_start=o.quick_start,
		rate_limit_per_minute=o.rate_limit_per_minute, daily_quota=o.daily_quota, purpose=o.purpose
		FROM %s.api_credentials o WHERE n.id=$1 AND o.id=$2`, schema, schema), issued.Credential.ID, credentialID); err != nil {
		return nil, err
	}
	if _, err := tx.Exec(fmt.Sprintf(`UPDATE %s.api_credentials SET old_key_ends_at = CURRENT_TIMESTAMP + ($2 * INTERVAL '1 second') WHERE id=$1`, schema),
		credentialID, int64(overlap.Seconds())); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return &CreatedAPIKey{Credential: issued.Credential, APIKey: issued.APIKey, TenantCode: tenantID}, nil
}

func copyAPIKeyAccess(schema, fromID, toID string) error {
	_, err := db.DB.Exec(fmt.Sprintf(`UPDATE %s.api_credentials n SET owner_user_id=o.owner_user_id, modules=o.modules,
		doctypes=o.doctypes, can_approve=o.can_approve, ip_allow=o.ip_allow, stores=o.stores, quick_start=o.quick_start
		FROM %s.api_credentials o WHERE n.id=$1 AND o.id=$2`, schema, schema), toID, fromID)
	return err
}

// TakeOverAPIKey hands a key to the administrator, who then answers for it.
func TakeOverAPIKey(tenantID, credentialID, newOwnerID string) error {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return err
	}
	res, err := db.DB.Exec(fmt.Sprintf(`UPDATE %s.api_credentials SET owner_user_id=$2 WHERE id=$1 AND status='Active'`, schema), credentialID, newOwnerID)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return &ValidationError{Code: "GLOBAL-0004", Message: "active API key not found"}
	}
	return nil
}

// APIKeyOwnerID is the owner of a key, or "" for a key made before owners.
func APIKeyOwnerID(tenantID, credentialID string) (string, error) {
	acc, err := LoadAPIKeyAccess(tenantID, credentialID)
	if err != nil {
		return "", err
	}
	return acc.OwnerUserID, nil
}

// APIKeyOwnerName is the owner's username, for the Resolved-Username header and the screen.
func APIKeyOwnerName(tenantID, ownerID string) string {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return ownerID
	}
	var name string
	if err := db.DB.QueryRow(fmt.Sprintf(`SELECT username FROM %s.users WHERE id = $1`, schema), ownerID).Scan(&name); err != nil || name == "" {
		return ownerID
	}
	return name
}
