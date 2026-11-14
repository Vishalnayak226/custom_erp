package engines

import (
	"crypto/rand"
	"custom_erp/db"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"github.com/lib/pq"
	"sort"
	"strings"
)

// 49.8.5 (screen): Settings -> Integrations -> Credentials lists every
// connector credential on one page - courier, channel, CleverTap, Pine Labs,
// Unicommerce - with kind, name, status, owner, last use and expiry, and
// rotates or switches one off. It never reads a secret: the list selects no
// secret column, and Rotate hands back only a secret this ERP generated
// itself (a blank webhook_secret), once.

// ConnectorCredentialRow is one line of the credentials list.
type ConnectorCredentialRow struct {
	Kind       string `json:"kind"` // courier | channel | clevertap | pinelabs | unicommerce
	Code       string `json:"code"` // what Disconnect / Rotate / Switch take
	Name       string `json:"name"`
	Status     string `json:"status"` // Active | Off | Expired
	Active     bool   `json:"active"`
	Owner      string `json:"owner"`
	LastUsedAt string `json:"last_used_at"`
	ExpiresAt  string `json:"expires_at"`
	ChangedAt  string `json:"changed_at"`
	// SecretFields names (never values) what Rotate can replace.
	SecretFields []string `json:"secret_fields"`
}

// credentialKindTable maps a kind to its table, key column and the column
// holding the one secret a plain Rotate replaces.
func credentialKindTable(kind string) (table, keyColumn string, ok bool) {
	switch kind {
	case "channel", "courier":
		return "channel_credentials", "channel_code", true
	case "clevertap":
		return "clevertap_credentials", "account_id", true
	case "pinelabs":
		return "pinelabs_credentials", "terminal_id", true
	case "unicommerce":
		return "unicommerce_credentials", "store_code", true
	}
	return "", "", false
}

func credentialStatus(active bool, expires sql.NullTime) string {
	switch {
	case !active:
		return "Off"
	case credentialExpired(expires):
		return "Expired"
	}
	return "Active"
}

// ListConnectorCredentials is every credential of a tenant, newest change
// first. A table an older schema lacks is skipped, not an error.
func ListConnectorCredentials(tenantID string) ([]ConnectorCredentialRow, error) {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return nil, err
	}
	out := []ConnectorCredentialRow{}
	type src struct{ kind, table, key string }
	for _, s := range []src{
		{"channel", "channel_credentials", "channel_code"},
		{"clevertap", "clevertap_credentials", "account_id"},
		{"pinelabs", "pinelabs_credentials", "terminal_id"},
		{"unicommerce", "unicommerce_credentials", "store_code"},
	} {
		rows, err := db.DB.Query(fmt.Sprintf(`SELECT %s, active, COALESCE(saved_by, ''), last_used_at, expires_at, updated_at FROM %s.%s ORDER BY updated_at DESC NULLS LAST`, s.key, schema, s.table))
		if err != nil {
			if isUndefinedTable(err) {
				continue
			}
			return nil, fmt.Errorf("list %s credentials: %w", s.kind, err)
		}
		for rows.Next() {
			var code, owner string
			var active bool
			var lastUsed, expires, changed sql.NullTime
			if err := rows.Scan(&code, &active, &owner, &lastUsed, &expires, &changed); err != nil {
				rows.Close()
				return nil, fmt.Errorf("list %s credentials: %w", s.kind, err)
			}
			kind, name := s.kind, code
			if s.kind == "channel" && strings.HasPrefix(code, "courier:") {
				kind, code = "courier", strings.TrimPrefix(code, "courier:")
				name = code
			}
			out = append(out, ConnectorCredentialRow{Kind: kind, Code: code, Name: name, Status: credentialStatus(active, expires), Active: active,
				Owner: owner, LastUsedAt: credentialTimeString(lastUsed), ExpiresAt: credentialTimeString(expires), ChangedAt: credentialTimeString(changed),
				SecretFields: credentialSecretFields(tenantID, kind, code)})
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return nil, fmt.Errorf("list %s credentials: %w", s.kind, err)
		}
	}
	return out, nil
}

// isUndefinedTable is Postgres 42P01: the table is not in this tenant's schema.
func isUndefinedTable(err error) bool {
	var pe *pq.Error
	return errors.As(err, &pe) && pe.Code == "42P01"
}

// credentialKey is the stored key for a kind and the code the screen shows.
func credentialKey(kind, code string) string {
	if kind == "courier" {
		return courierCredentialCode(code)
	}
	return code
}

// SetConnectorCredentialActive switches a credential off or on without
// deleting it. A switched-off one is refused where it is read; saving new
// fields switches it on again.
func SetConnectorCredentialActive(tenantID, kind, code string, active bool, actor string) error {
	kind, code = strings.ToLower(strings.TrimSpace(kind)), strings.TrimSpace(code)
	table, keyColumn, ok := credentialKindTable(kind)
	if !ok || code == "" {
		return errors.New("kind must be channel, courier, clevertap, pinelabs or unicommerce, with a code")
	}
	if active {
		if err := checkConnectorRehearsal(tenantID, kind, code, actor); err != nil {
			return err
		}
	}
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return err
	}
	res, err := db.DB.Exec(fmt.Sprintf(`UPDATE %s.%s SET active = $1, updated_at = CURRENT_TIMESTAMP WHERE %s = $2`, schema, table, keyColumn), active, credentialKey(kind, code))
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return fmt.Errorf("no %s credential is saved for %q", kind, code)
	}
	state := "off"
	if active {
		state = "on"
	}
	LogAuditEvent(tenantID, actor, "CONNECTOR_SWITCHED", "SUCCESS", fmt.Sprintf("kind=%s code=%s state=%s", kind, code, state))
	return nil
}

// RotateConnectorCredential replaces the secret fields of one credential and
// stamps the change (owner = actor, updated_at, an audit row). fields names
// what to replace: a value is the new secret; a blank webhook_secret asks for
// one to be generated, and every generated value is returned once - nothing
// else is ever returned. Other fields on file are kept. A blank value for any
// other field is refused.
func RotateConnectorCredential(tenantID, kind, code string, fields map[string]string, actor string) (generated map[string]string, err error) {
	kind, code = strings.ToLower(strings.TrimSpace(kind)), strings.TrimSpace(code)
	if _, _, ok := credentialKindTable(kind); !ok || code == "" {
		return nil, errors.New("kind must be channel, courier, clevertap, pinelabs or unicommerce, with a code")
	}
	if len(fields) == 0 {
		return nil, errors.New("the new secret is required")
	}
	generated = map[string]string{}
	clean := map[string]string{}
	for k, v := range fields {
		k, v = strings.TrimSpace(k), strings.TrimSpace(v)
		if k == "" {
			continue
		}
		if v == "" {
			if k != "webhook_secret" {
				return nil, fmt.Errorf("%s cannot be blank", k)
			}
			buf := make([]byte, 24)
			if _, err := rand.Read(buf); err != nil {
				return nil, err
			}
			v = hex.EncodeToString(buf)
			generated[k] = v
		}
		clean[k] = v
	}
	if len(clean) == 0 {
		return nil, errors.New("the new secret is required")
	}
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return nil, err
	}
	switch kind {
	case "channel", "courier":
		key := credentialKey(kind, code)
		cur, err := getChannelCredentialRaw(tenantID, key)
		if err != nil {
			return nil, fmt.Errorf("no %s credential is saved for %q", kind, code)
		}
		for k, v := range clean {
			cur[k] = v
		}
		if err := SaveChannelCredentialBy(tenantID, key, cur, actor, nil); err != nil {
			return nil, err
		}
	default:
		column := map[string][]string{"clevertap": {"passcode"}, "pinelabs": {"api_key"}, "unicommerce": {"api_key", "api_secret"}}[kind]
		table, keyColumn, _ := credentialKindTable(kind)
		set, args := []string{}, []interface{}{}
		for _, c := range column {
			if v, ok := clean[c]; ok {
				args = append(args, v)
				set = append(set, fmt.Sprintf("%s = $%d", c, len(args)))
			}
		}
		if len(set) == 0 {
			return nil, fmt.Errorf("a %s credential rotates %s", kind, strings.Join(column, " or "))
		}
		args = append(args, strings.TrimSpace(actor), code)
		res, err := db.DB.Exec(fmt.Sprintf(`UPDATE %s.%s SET %s, active = TRUE, saved_by = COALESCE(NULLIF($%d, ''), saved_by), updated_at = CURRENT_TIMESTAMP WHERE %s = $%d`,
			schema, table, strings.Join(set, ", "), len(args)-1, keyColumn, len(args)), args...)
		if err != nil {
			return nil, err
		}
		if n, _ := res.RowsAffected(); n == 0 {
			return nil, fmt.Errorf("no %s credential is saved for %q", kind, code)
		}
	}
	names := make([]string, 0, len(clean))
	for k := range clean {
		names = append(names, k)
	}
	LogAuditEvent(tenantID, actor, "CONNECTOR_ROTATED", "SUCCESS", fmt.Sprintf("kind=%s code=%s fields=%s", kind, code, strings.Join(names, ",")))
	return generated, nil
}

// credentialSecretFields names the fields Rotate can replace: fixed for the
// table-backed kinds; for a channel / courier, the secret-looking field names
// on file (names only - the values stay encrypted).
func credentialSecretFields(tenantID, kind, code string) []string {
	switch kind {
	case "clevertap":
		return []string{"passcode"}
	case "pinelabs":
		return []string{"api_key"}
	case "unicommerce":
		return []string{"api_key", "api_secret"}
	}
	fields, err := getChannelCredentialRaw(tenantID, credentialKey(kind, code))
	if err != nil {
		return []string{}
	}
	names := []string{}
	for k := range fields {
		l := strings.ToLower(k)
		if strings.HasSuffix(l, "_previous") || strings.HasSuffix(l, "_rotated_at") {
			continue
		}
		for _, hint := range []string{"token", "secret", "key", "pass"} {
			if strings.Contains(l, hint) {
				names = append(names, k)
				break
			}
		}
	}
	sort.Strings(names)
	return names
}

// CourierProviderNames is the registered couriers, for the Add page.
func CourierProviderNames() []string {
	courierMu.RLock()
	defer courierMu.RUnlock()
	names := make([]string, 0, len(courierAdapters))
	for k := range courierAdapters {
		names = append(names, k)
	}
	sort.Strings(names)
	return names
}
