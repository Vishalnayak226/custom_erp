package engines

import (
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"

	"custom_erp/db"
)

// Stage 49.2.6: one identity per integration and purpose, and a rotation
// policy. Both settings are off; with them off a credential's purpose is only
// a label and nothing is refused or revoked.
const (
	SettingAPICredentialOnePerPurpose = "security.api_credential_one_per_purpose"
	SettingAPICredentialMaxAgeDays    = "security.api_credential_max_age_days"

	apiCredentialRotationWarnDays = 14
	apiCredentialPurposeMax       = 80
)

var apiCredentialPurposePattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9 ._/-]*$`)

// NormalizeAPICredentialPurpose trims and checks a purpose (blank = none).
func NormalizeAPICredentialPurpose(purpose string) (string, error) {
	purpose = strings.TrimSpace(purpose)
	if purpose == "" {
		return "", nil
	}
	if len(purpose) > apiCredentialPurposeMax || !apiCredentialPurposePattern.MatchString(purpose) {
		return "", &ValidationError{Code: "GLOBAL-0002", SubFor: "Purpose", Message: "purpose must be letters, digits, spaces and . _ / - (80 characters at most)"}
	}
	return purpose, nil
}

func apiPurposeTaken() error {
	return &ValidationError{Code: "SEC-0290", SubFor: "Purpose", Message: "another active API access already has this purpose"}
}

// CheckAPICredentialPurposeFree refuses, when the one-per-purpose setting is
// on, a purpose another Active credential already holds. Run before issuing
// so the secret is never minted for a refused request.
func CheckAPICredentialPurposeFree(tenantID, purpose string) error {
	if purpose == "" || !GetSettingBool(tenantID, SettingAPICredentialOnePerPurpose) {
		return nil
	}
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return err
	}
	var taken bool
	if err := db.DB.QueryRow(fmt.Sprintf(`SELECT EXISTS (SELECT 1 FROM %s.api_credentials
		WHERE status = 'Active' AND LOWER(purpose) = LOWER($1))`, schema), purpose).Scan(&taken); err != nil {
		return err
	}
	if taken {
		return apiPurposeTaken()
	}
	return nil
}

// SetAPICredentialPurpose records the purpose on a just-issued credential.
// With the setting on, the update itself refuses a purpose another Active
// credential holds, so two issues racing cannot both win; the caller revokes
// the new credential on that error.
func SetAPICredentialPurpose(tenantID, credentialID, purpose string) error {
	if purpose == "" {
		return nil
	}
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return err
	}
	guard := ""
	if GetSettingBool(tenantID, SettingAPICredentialOnePerPurpose) {
		guard = fmt.Sprintf(` AND NOT EXISTS (SELECT 1 FROM %s.api_credentials o
			WHERE o.status = 'Active' AND o.id <> c.id AND LOWER(o.purpose) = LOWER($1))`, schema)
	}
	res, err := db.DB.Exec(fmt.Sprintf(`UPDATE %s.api_credentials c SET purpose = $1 WHERE c.id = $2%s`, schema, guard), purpose, credentialID)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return apiPurposeTaken()
	}
	return nil
}

// apiCredentialPurposes returns purpose by credential id (blank ones omitted).
func apiCredentialPurposes(schema string) map[string]string {
	out := map[string]string{}
	rows, err := db.DB.Query(fmt.Sprintf(`SELECT id::text, purpose FROM %s.api_credentials WHERE COALESCE(purpose, '') <> ''`, schema))
	if err != nil {
		return out
	}
	defer rows.Close()
	for rows.Next() {
		var id, p string
		if rows.Scan(&id, &p) == nil {
			out[id] = p
		}
	}
	return out
}

// SweepAgedAPICredentials applies the rotation policy: an Active credential
// older than the maximum age is revoked through RevokeAPICredential; one
// within two weeks of it gets one warning row in the audit log. Age counts
// from the latest of issue (a rotation is a new issue) and this setting's last
// change, so turning the policy on gives every existing key a full period.
func SweepAgedAPICredentials(tenantID string) (int, error) {
	days := GetSettingInt(tenantID, SettingAPICredentialMaxAgeDays)
	if days <= 0 {
		return 0, nil
	}
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return 0, err
	}
	rows, err := db.DB.Query(fmt.Sprintf(`
		SELECT c.id::text, c.name,
		       GREATEST(c.created_at,
		         COALESCE((SELECT s.updated_at FROM %[1]s.system_settings s WHERE s.key = $1), c.created_at),
		         COALESCE((SELECT MAX(t.lifecycle_changed_at) FROM public.tenants t WHERE t.schema_name = $2), c.created_at))
		FROM %[1]s.api_credentials c
		WHERE c.status = 'Active' AND c.revoked_at IS NULL
		ORDER BY c.name LIMIT %[2]d`, schema, identityLifecycleRowCap), SettingAPICredentialMaxAgeDays, schema)
	if err != nil {
		return 0, err
	}
	type cred struct {
		id, name string
		since    time.Time
	}
	var all []cred
	for rows.Next() {
		var c cred
		if err := rows.Scan(&c.id, &c.name, &c.since); err != nil {
			rows.Close()
			return 0, err
		}
		all = append(all, c)
	}
	rows.Close()
	revoked := 0
	now := time.Now()
	for _, c := range all {
		due := c.since.Add(time.Duration(days) * 24 * time.Hour)
		switch {
		case !now.Before(due):
			if rerr := RevokeAPICredential(tenantID, c.id); rerr != nil {
				var ve *ValidationError
				if errors.As(rerr, &ve) {
					continue // revoked meanwhile
				}
				return revoked, rerr
			}
			LogAuditEvent(tenantID, "system", "API_CREDENTIAL_REVOKED", "SUCCESS",
				fmt.Sprintf("credential=%s name=%q - older than %d days (API access rotation policy)", c.id, c.name, days))
			revoked++
		case !now.Before(due.Add(-apiCredentialRotationWarnDays * 24 * time.Hour)):
			var warned bool
			_ = db.DB.QueryRow(fmt.Sprintf(`SELECT EXISTS (SELECT 1 FROM %s.audit_logs WHERE action = 'API_CREDENTIAL_ROTATION_DUE' AND details LIKE $1)`, schema),
				"credential="+c.id+" %").Scan(&warned)
			if !warned {
				LogAuditEvent(tenantID, "system", "API_CREDENTIAL_ROTATION_DUE", "WARNING",
					fmt.Sprintf("credential=%s name=%q - revoked on %s unless rotated (API access rotation policy)", c.id, c.name, due.UTC().Format("2006-01-02")))
			}
		}
	}
	return revoked, nil
}
