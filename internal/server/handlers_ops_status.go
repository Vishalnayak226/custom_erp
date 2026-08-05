package server

import (
	"encoding/json"
	"net/http"
	"strings"

	"custom_erp/db"
)

// Stage 25.8: DEPLOY-0204..0210 / DR-0211..0214 (Production Mandatory tier,
// outside the 187-code Mature-ERP recount) had nothing to attach to -
// manage.ps1/promote.ps1 already do real build/release/backup/restore/
// restore-drill work, but never reported outcomes back through the HTTP
// API. Both handlers are HR/Admin-only, matching every other ops-visibility
// endpoint (handleVerifyAuditLogChain, handleRolePermissions). Both read
// from control-plane tables (public.deployments, public.ops_run_log) that
// live in dev's own database regardless of which environment a row is
// about - same reasoning promote.ps1's own header comment gives for
// public.deployments/public.schema_migrations.

type deploymentStatusRow struct {
	Environment string  `json:"environment"`
	GitCommit   string  `json:"git_commit"`
	AppVersion  string  `json:"app_version"`
	PromotedBy  string  `json:"promoted_by"`
	PromotedAt  string  `json:"promoted_at"`
	BuildStatus string  `json:"build_status"`
	Notes       string  `json:"notes"`
	Code        *string `json:"code,omitempty"`
	Message     *string `json:"message,omitempty"`
}

// deployBuildStatusCode maps public.deployments.build_status/notes to the
// nearest DEPLOY-02xx catalog scenario. "passed" and "rolled_back" are
// resolved/informational states, not failures, so they get no code - same
// "don't force-fit a code onto a non-error state" discipline as Stage 25
// Batch 3/4's deferred-code notes. promote.ps1 writes "failed" for two
// distinct reasons distinguished only by notes text: "go test failed -
// promotion refused" (DEPLOY-0205, build/test gate) and "Health check
// failed: ..." (DEPLOY-0208, the target came up on the wrong binary/didn't
// come up at all after Start-Environment's port-poll).
func deployBuildStatusCode(buildStatus, notes string) string {
	if buildStatus != "failed" {
		return ""
	}
	if strings.HasPrefix(notes, "Health check failed") {
		return "DEPLOY-0208"
	}
	return "DEPLOY-0205"
}

func handleDeploymentStatus(w http.ResponseWriter, r *http.Request) {
	role := r.Header.Get("Resolved-Role")
	if !requireHRAdmin(w, r, role) {
		return
	}

	rows, err := db.DB.Query(`
		SELECT environment, git_commit, COALESCE(app_version, ''), COALESCE(promoted_by, ''),
			promoted_at::text, build_status, COALESCE(notes, '')
		FROM public.deployments ORDER BY promoted_at DESC LIMIT 50`)
	if err != nil {
		writeAPIErrorGeneric(w, r, http.StatusInternalServerError, err.Error())
		return
	}
	defer rows.Close()

	history := []deploymentStatusRow{}
	latestByEnv := map[string]deploymentStatusRow{}
	for rows.Next() {
		var d deploymentStatusRow
		if err := rows.Scan(&d.Environment, &d.GitCommit, &d.AppVersion, &d.PromotedBy, &d.PromotedAt, &d.BuildStatus, &d.Notes); err != nil {
			writeAPIErrorGeneric(w, r, http.StatusInternalServerError, err.Error())
			return
		}
		if code := deployBuildStatusCode(d.BuildStatus, d.Notes); code != "" {
			entry := errorCatalog[code]
			d.Code = &code
			d.Message = &entry.UserMessage
		}
		history = append(history, d)
		if _, seen := latestByEnv[d.Environment]; !seen {
			latestByEnv[d.Environment] = d
		}
	}

	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"latest_by_environment": latestByEnv,
		"history":               history,
	})
}

type opsRunRow struct {
	RunType     string  `json:"run_type"`
	Environment string  `json:"environment"`
	Status      string  `json:"status"`
	Detail      string  `json:"detail"`
	StartedAt   string  `json:"started_at"`
	FinishedAt  string  `json:"finished_at"`
	Code        *string `json:"code,omitempty"`
	Message     *string `json:"message,omitempty"`
}

func handleBackupStatus(w http.ResponseWriter, r *http.Request) {
	role := r.Header.Get("Resolved-Role")
	if !requireHRAdmin(w, r, role) {
		return
	}

	rows, err := db.DB.Query(`
		SELECT run_type, environment, status, detail, started_at::text, finished_at::text
		FROM public.ops_run_log
		WHERE run_type IN ('backup', 'restore', 'restore_drill', 'tenant_export')
		ORDER BY finished_at DESC LIMIT 100`)
	if err != nil {
		writeAPIErrorGeneric(w, r, http.StatusInternalServerError, err.Error())
		return
	}
	defer rows.Close()

	history := []opsRunRow{}
	for rows.Next() {
		var o opsRunRow
		if err := rows.Scan(&o.RunType, &o.Environment, &o.Status, &o.Detail, &o.StartedAt, &o.FinishedAt); err != nil {
			writeAPIErrorGeneric(w, r, http.StatusInternalServerError, err.Error())
			return
		}
		if o.Status == "failed" {
			var code string
			switch o.RunType {
			case "backup":
				code = "DR-0211" // "Backup failed"
			case "restore", "restore_drill":
				code = "DR-0212" // "Restore checksum mismatch" - the only DR restore-failure scenario in the catalog; Restore-Database/Invoke-RestoreDrill's other failure reasons (pg_restore error, missing backup) reuse it too since there's no separate generic-restore-failure row.
			}
			if code != "" {
				entry := errorCatalog[code]
				o.Code = &code
				o.Message = &entry.UserMessage
			}
		}
		history = append(history, o)
	}

	// DR-0213 (restore drill overdue, Warning/non-blocking) and DR-0214 (RPO
	// breach, Error/blocking) are computed status conditions, not failures
	// of any single logged run - they describe the *absence* of a recent
	// enough success. Computed in SQL (not by parsing the ::text timestamps
	// above in Go) so timezone/format handling stays Postgres's problem.
	// Thresholds match manage.ps1's own registered schedule
	// (Register-BackupSchedule: daily backup, monthly drill).
	var lastBackupAt, lastDrillAt *string
	var backupOverdue, drillOverdue bool
	err = db.DB.QueryRow(`
		SELECT MAX(finished_at) FILTER (WHERE run_type = 'backup' AND status = 'success')::text,
			MAX(finished_at) FILTER (WHERE run_type = 'backup' AND status = 'success') < now() - interval '2 days'
			OR MAX(finished_at) FILTER (WHERE run_type = 'backup' AND status = 'success') IS NULL,
			MAX(finished_at) FILTER (WHERE run_type = 'restore_drill' AND status = 'success')::text,
			MAX(finished_at) FILTER (WHERE run_type = 'restore_drill' AND status = 'success') < now() - interval '35 days'
			OR MAX(finished_at) FILTER (WHERE run_type = 'restore_drill' AND status = 'success') IS NULL
		FROM public.ops_run_log`).Scan(&lastBackupAt, &backupOverdue, &lastDrillAt, &drillOverdue)
	if err != nil {
		writeAPIErrorGeneric(w, r, http.StatusInternalServerError, err.Error())
		return
	}

	warnings := []map[string]string{}
	if drillOverdue {
		entry := errorCatalog["DR-0213"]
		warnings = append(warnings, map[string]string{"code": "DR-0213", "message": entry.UserMessage})
	}
	if backupOverdue {
		entry := errorCatalog["DR-0214"]
		warnings = append(warnings, map[string]string{"code": "DR-0214", "message": entry.UserMessage})
	}

	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"last_backup_at":        lastBackupAt,
		"last_restore_drill_at": lastDrillAt,
		"warnings":              warnings,
		"history":               history,
	})
}
