package engines

import (
	"custom_erp/db"
	"encoding/json"
	"fmt"
)

// Content version snapshots (Stage 26.4.6). product_content_versions is a
// dedicated system-written table (not a doctype) - same reasoning as
// pim_publish_queue/log (Stage 15.2): never authored directly by a user. A
// snapshot is taken only on a real "Approved" decision (see the hook in
// DecideApproval, engines/approval.go), so the history is exactly "every
// version that was ever genuinely approved," not every draft edit.

type ContentVersionEntry struct {
	ID        int                    `json:"id"`
	VersionNo int                    `json:"version_no"`
	Status    string                 `json:"status"`
	SavedBy   string                 `json:"saved_by"`
	CreatedAt string                 `json:"created_at"`
	Data      map[string]interface{} `json:"data"`
}

// snapshotProductContentVersion is best-effort by design: called right
// after DecideApproval's own transaction has already committed the
// approval, so a snapshot failure here must never be surfaced as an
// approval failure - the approval already happened.
func snapshotProductContentVersion(tenantID, contentID string, data map[string]interface{}, savedBy string) {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return
	}
	marshaled, err := json.Marshal(data)
	if err != nil {
		return
	}
	var nextVersion int
	_ = db.DB.QueryRow(fmt.Sprintf(`SELECT COALESCE(MAX(version_no), 0) + 1 FROM %s.product_content_versions WHERE content_id = $1`, schema), contentID).Scan(&nextVersion)
	_, _ = db.DB.Exec(fmt.Sprintf(`
		INSERT INTO %s.product_content_versions (content_id, version_no, data, status, saved_by)
		VALUES ($1, $2, $3, 'Approved', $4)`, schema), contentID, nextVersion, marshaled, savedBy)
}

// ListProductContentVersions returns every approved snapshot for one
// ProductContent id, newest first.
func ListProductContentVersions(tenantID, contentID string) ([]ContentVersionEntry, error) {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return nil, err
	}
	rows, err := db.DB.Query(fmt.Sprintf(`
		SELECT id, version_no, status, COALESCE(saved_by, ''), created_at::text, data
		FROM %s.product_content_versions WHERE content_id = $1 ORDER BY version_no DESC`, schema), contentID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []ContentVersionEntry{}
	for rows.Next() {
		var e ContentVersionEntry
		var dataStr string
		if err := rows.Scan(&e.ID, &e.VersionNo, &e.Status, &e.SavedBy, &e.CreatedAt, &dataStr); err != nil {
			return nil, err
		}
		_ = json.Unmarshal([]byte(dataStr), &e.Data)
		out = append(out, e)
	}
	return out, rows.Err()
}

// RollbackProductContentVersion restores a prior approved snapshot as the
// current ProductContent, set back to Draft rather than silently
// re-becoming Approved without a fresh review - the same no-silent-state-
// change stance the rest of the approval flow already takes (e.g.
// re-approval-on-edit). Logged into the existing approval_log so the
// rollback shows up in ListApprovalLog's history alongside every other
// action on this document.
func RollbackProductContentVersion(tenantID, contentID string, versionID int, actorUserID, actorRole string) error {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return err
	}
	var dataStr string
	err = db.DB.QueryRow(fmt.Sprintf(`SELECT data FROM %s.product_content_versions WHERE id = $1 AND content_id = $2`, schema), versionID, contentID).Scan(&dataStr)
	if err != nil {
		return fmt.Errorf("version not found: %v", err)
	}
	var data map[string]interface{}
	if err := json.Unmarshal([]byte(dataStr), &data); err != nil {
		return err
	}
	data["status"] = "Draft"
	marshaled, err := json.Marshal(data)
	if err != nil {
		return err
	}
	if _, err := db.DB.Exec(fmt.Sprintf(`UPDATE %s.documents SET data = $1, status = 'Draft', updated_at = CURRENT_TIMESTAMP WHERE doctype = 'ProductContent' AND id = $2`, schema), marshaled, contentID); err != nil {
		return err
	}
	return logApprovalAction(tenantID, "ProductContent", contentID, "Modified", actorUserID, actorRole, 0, fmt.Sprintf("Rolled back to version %d", versionID))
}
