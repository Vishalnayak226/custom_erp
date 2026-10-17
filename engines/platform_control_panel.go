package engines

import (
	"custom_erp/db"
	"database/sql"
	"fmt"
	"os"
	"strings"
	"time"
)

// Stage 65.5 (the user, 2026-10-16: "a page where I can see all clients and
// what access I gave, release and all other control, where hosted and
// everything"): one read-only summary for the platform operator. Everything
// here already exists in its own place - the tenants registry, each client's
// module entitlements, users, audit and error logs, the deployments and
// backup_runs ledgers - this only gathers it. Changes stay on the screens
// that own them (Tenant Entitlements, Data Console, sandboxes).

// ControlPanelClient is one client's row.
type ControlPanelClient struct {
	TenantID         string     `json:"tenant_id"`
	Name             string     `json:"name"`
	Schema           string     `json:"schema"`
	Status           string     `json:"status"`
	StatusReason     string     `json:"status_reason,omitempty"`
	IsSandbox        bool       `json:"is_sandbox"`
	SandboxExpiresAt *time.Time `json:"sandbox_expires_at,omitempty"`
	HostSlug         string     `json:"host_slug,omitempty"`
	URL              string     `json:"url,omitempty"`
	CreatedAt        *time.Time `json:"created_at,omitempty"`
	ProvisionedAt    *time.Time `json:"provisioned_at,omitempty"`
	AppVersion       string     `json:"app_version,omitempty"`
	PinnedVersion    string     `json:"pinned_version,omitempty"`
	LegalHold        bool       `json:"legal_hold"`
	AdminSignedIn    bool       `json:"admin_signed_in"`
	ModulesEnabled   []string   `json:"modules_enabled"`
	ModulesDisabled  []string   `json:"modules_disabled"`
	ActiveUsers      int        `json:"active_users"`
	Records          int        `json:"records"`
	LastActivityAt   *time.Time `json:"last_activity_at,omitempty"`
	Errors24h        int        `json:"errors_24h"`
	LastErrorAt      *time.Time `json:"last_error_at,omitempty"`
	Problem          string     `json:"problem,omitempty"`
}

// ControlPanelDeployment is one row of the deployments ledger.
type ControlPanelDeployment struct {
	Environment string     `json:"environment"`
	GitCommit   string     `json:"git_commit"`
	AppVersion  string     `json:"app_version"`
	PromotedBy  string     `json:"promoted_by"`
	PromotedAt  *time.Time `json:"promoted_at,omitempty"`
	Status      string     `json:"status"`
	Notes       string     `json:"notes,omitempty"`
}

// ControlPanelBackup is the most recent whole-database backup run.
type ControlPanelBackup struct {
	FinishedAt *time.Time `json:"finished_at,omitempty"`
	Outcome    string     `json:"outcome"`
	SizeBytes  int64      `json:"size_bytes"`
	FileName   string     `json:"file_name,omitempty"`
	Host       string     `json:"host,omitempty"`
}

// ControlPanel is the whole page.
type ControlPanel struct {
	BaseDomain       string                   `json:"base_domain,omitempty"`
	Environment      string                   `json:"environment"`
	Clients          []ControlPanelClient     `json:"clients"`
	Deployments      []ControlPanelDeployment `json:"deployments"`
	LastBackup       *ControlPanelBackup      `json:"last_backup,omitempty"`
	LastGoodBackupAt *time.Time               `json:"last_good_backup_at,omitempty"`
}

// GetControlPanel builds the summary. One client's unreadable schema is
// reported on its row (Problem) rather than failing the page.
func GetControlPanel() (ControlPanel, error) {
	base := strings.ToLower(strings.TrimSpace(os.Getenv("TENANT_BASE_DOMAIN")))
	env := strings.TrimSpace(os.Getenv("ENV"))
	if env == "" {
		env = "development"
	}
	out := ControlPanel{BaseDomain: base, Environment: env, Clients: []ControlPanelClient{}, Deployments: []ControlPanelDeployment{}}

	rows, err := db.DB.Query(`SELECT tenant_id, COALESCE(name, tenant_id), schema_name, COALESCE(lifecycle_status, ''),
		COALESCE(lifecycle_reason, ''), COALESCE(is_sandbox, FALSE), sandbox_expires_at, COALESCE(host_slug, ''),
		created_at, provisioned_at, COALESCE(app_version, ''), COALESCE(pinned_version, ''), COALESCE(legal_hold, FALSE),
		bootstrap_consumed_at IS NOT NULL OR bootstrap_issued_at IS NULL
		FROM public.tenants ORDER BY is_sandbox, tenant_id`)
	if err != nil {
		return out, err
	}
	for rows.Next() {
		var c ControlPanelClient
		var sandboxExp, created, provisioned sql.NullTime
		if err := rows.Scan(&c.TenantID, &c.Name, &c.Schema, &c.Status, &c.StatusReason, &c.IsSandbox, &sandboxExp, &c.HostSlug,
			&created, &provisioned, &c.AppVersion, &c.PinnedVersion, &c.LegalHold, &c.AdminSignedIn); err != nil {
			rows.Close()
			return out, err
		}
		c.SandboxExpiresAt, c.CreatedAt, c.ProvisionedAt = nullTimePtr(sandboxExp), nullTimePtr(created), nullTimePtr(provisioned)
		if c.HostSlug != "" && base != "" {
			c.URL = "https://" + c.HostSlug + "." + base + "/"
		}
		out.Clients = append(out.Clients, c)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return out, err
	}

	for i := range out.Clients {
		fillControlPanelClient(&out.Clients[i])
	}

	// Releases and backups: both ledgers are optional on an older database.
	if dRows, err := db.DB.Query(`SELECT COALESCE(environment, ''), COALESCE(git_commit, ''), COALESCE(app_version, ''),
		COALESCE(promoted_by, ''), promoted_at, COALESCE(build_status, ''), COALESCE(notes, '')
		FROM public.deployments ORDER BY promoted_at DESC NULLS LAST LIMIT 10`); err == nil {
		for dRows.Next() {
			var d ControlPanelDeployment
			var at sql.NullTime
			if dRows.Scan(&d.Environment, &d.GitCommit, &d.AppVersion, &d.PromotedBy, &at, &d.Status, &d.Notes) == nil {
				d.PromotedAt = nullTimePtr(at)
				out.Deployments = append(out.Deployments, d)
			}
		}
		dRows.Close()
	}
	var b ControlPanelBackup
	var finished sql.NullTime
	var size sql.NullInt64
	if err := db.DB.QueryRow(`SELECT finished_at, COALESCE(outcome, ''), size_bytes, COALESCE(file_name, ''), COALESCE(host, '')
		FROM public.backup_runs WHERE scope = 'database' ORDER BY finished_at DESC NULLS LAST LIMIT 1`).Scan(&finished, &b.Outcome, &size, &b.FileName, &b.Host); err == nil {
		b.FinishedAt, b.SizeBytes = nullTimePtr(finished), size.Int64
		out.LastBackup = &b
	}
	var good sql.NullTime
	if db.DB.QueryRow(`SELECT MAX(finished_at) FROM public.backup_runs WHERE scope = 'database' AND outcome = 'Success'`).Scan(&good) == nil {
		out.LastGoodBackupAt = nullTimePtr(good)
	}
	return out, nil
}

func fillControlPanelClient(c *ControlPanelClient) {
	c.ModulesEnabled, c.ModulesDisabled = []string{}, []string{}
	if !validSQLIdentifier(c.Schema) {
		c.Problem = "schema name is not usable"
		return
	}
	var exists bool
	if db.DB.QueryRow(`SELECT EXISTS (SELECT 1 FROM pg_namespace WHERE nspname = $1)`, c.Schema).Scan(&exists); !exists {
		c.Problem = "schema " + c.Schema + " does not exist"
		return
	}
	if mRows, err := db.DB.Query(fmt.Sprintf(`SELECT module_key, enabled FROM %s.module_entitlements ORDER BY module_key`, c.Schema)); err == nil {
		for mRows.Next() {
			var key string
			var on bool
			if mRows.Scan(&key, &on) == nil {
				if on {
					c.ModulesEnabled = append(c.ModulesEnabled, key)
				} else {
					c.ModulesDisabled = append(c.ModulesDisabled, key)
				}
			}
		}
		mRows.Close()
	}
	_ = db.DB.QueryRow(fmt.Sprintf(`SELECT COUNT(*) FROM %s.users WHERE status = 'Active'`, c.Schema)).Scan(&c.ActiveUsers)
	_ = db.DB.QueryRow(fmt.Sprintf(`SELECT COUNT(*) FROM %s.documents WHERE deleted_at IS NULL`, c.Schema)).Scan(&c.Records)
	var last, lastErr sql.NullTime
	if db.DB.QueryRow(fmt.Sprintf(`SELECT MAX(created_at) FROM %s.audit_logs`, c.Schema)).Scan(&last) == nil {
		c.LastActivityAt = nullTimePtr(last)
	}
	_ = db.DB.QueryRow(fmt.Sprintf(`SELECT COUNT(*) FILTER (WHERE created_at > NOW() - INTERVAL '24 hours'), MAX(created_at) FROM %s.system_error_logs`, c.Schema)).Scan(&c.Errors24h, &lastErr)
	c.LastErrorAt = nullTimePtr(lastErr)
}

func nullTimePtr(t sql.NullTime) *time.Time {
	if !t.Valid {
		return nil
	}
	v := t.Time
	return &v
}
