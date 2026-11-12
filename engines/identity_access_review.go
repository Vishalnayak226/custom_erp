package engines

import (
	"context"
	"custom_erp/db"
	"fmt"
	"strings"
	"time"

	"github.com/lib/pq"
)

// Stage 49.2.1 (access recertification): an admin confirms or revokes each
// user's role and stores, and every decision is kept - who, when and what the
// access was when it was looked at (access_reviews). security.access_review_days
// sets how often; a user not reviewed inside it is overdue. Nothing is disabled
// by the clock. Revoke is the same disable Deactivate does (session dropped,
// audit row), with the same guards: never yourself, the Super Admin, the support
// user or the last active admin.

const SettingAccessReviewDays = "security.access_review_days"

const (
	AccessReviewConfirmed = "Confirmed"
	AccessReviewRevoked   = "Revoked"
)

// AccessReviewRow is one Active user with the state of their last review.
type AccessReviewRow struct {
	ID           string     `json:"id"`
	Username     string     `json:"username"`
	Role         string     `json:"role"`
	LocationCode string     `json:"location_code"`
	StoreAccess  string     `json:"store_access"`
	LastDecision string     `json:"last_decision,omitempty"`
	LastBy       string     `json:"last_reviewed_by,omitempty"`
	LastAt       *time.Time `json:"last_reviewed_at,omitempty"`
	Overdue      bool       `json:"overdue"`
}

// AccessReviewList is the review screen's data.
type AccessReviewList struct {
	Days    int               `json:"days"` // 0 = no schedule
	Overdue int               `json:"overdue"`
	Rows    []AccessReviewRow `json:"rows"`
}

// ListAccessReviews lists Active users, overdue first, then longest unreviewed.
func ListAccessReviews(tenantID string) (*AccessReviewList, error) {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return nil, err
	}
	days := GetSettingInt(tenantID, SettingAccessReviewDays)
	rows, err := db.DB.Query(fmt.Sprintf(`
		SELECT u.id, u.username, COALESCE(u.role, ''), COALESCE(u.location_code, ''),
		       COALESCE(u.store_access, '') || CASE WHEN COALESCE(array_length(u.stores, 1), 0) > 0 THEN ': ' || array_to_string(u.stores, ', ') ELSE '' END,
		       COALESCE(r.decision, ''), COALESCE(r.decided_by, ''), r.decided_at
		FROM %[1]s.users u
		LEFT JOIN LATERAL (SELECT decision, decided_by, decided_at FROM %[1]s.access_reviews a
		                   WHERE a.user_id = u.id ORDER BY a.decided_at DESC LIMIT 1) r ON TRUE
		WHERE u.status = 'Active' AND %[2]s
		ORDER BY r.decided_at NULLS FIRST, u.username LIMIT %[3]d`, schema, NotSupportUserSQL("u"), identityLifecycleRowCap))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := &AccessReviewList{Days: days, Rows: []AccessReviewRow{}}
	cutoff := time.Now().AddDate(0, 0, -days)
	for rows.Next() {
		var r AccessReviewRow
		var at pq.NullTime
		if err := rows.Scan(&r.ID, &r.Username, &r.Role, &r.LocationCode, &r.StoreAccess, &r.LastDecision, &r.LastBy, &at); err != nil {
			return nil, err
		}
		if at.Valid {
			t := at.Time
			r.LastAt = &t
		}
		r.Overdue = days > 0 && (r.LastAt == nil || r.LastAt.Before(cutoff))
		if r.Overdue {
			out.Overdue++
		}
		out.Rows = append(out.Rows, r)
	}
	return out, rows.Err()
}

// RecordAccessReview stores one decision. note is required for a revoke.
func RecordAccessReview(ctx context.Context, tenantID, userID, decision, note, actorID, actorUsername string) error {
	if decision != AccessReviewConfirmed && decision != AccessReviewRevoked {
		return &ValidationError{Code: "GLOBAL-0002", Message: "decision is Confirmed or Revoked"}
	}
	note = strings.TrimSpace(note)
	if decision == AccessReviewRevoked && note == "" {
		return &ValidationError{Code: "GLOBAL-0002", SubFor: "note", Message: "a reason is required to revoke access"}
	}
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return err
	}
	var username, role, loc, access, status string
	err = db.DB.QueryRow(fmt.Sprintf(`SELECT username, COALESCE(role, ''), COALESCE(location_code, ''),
		COALESCE(store_access, '') || CASE WHEN COALESCE(array_length(stores, 1), 0) > 0 THEN ': ' || array_to_string(stores, ', ') ELSE '' END,
		COALESCE(status, '') FROM %s.users u WHERE id = $1 AND %s`, schema, NotSupportUserSQL("u")), userID).Scan(&username, &role, &loc, &access, &status)
	if err != nil {
		return &ValidationError{Code: "GLOBAL-0002", Message: "user not found"}
	}
	if decision == AccessReviewRevoked {
		if userID == actorID {
			return &ValidationError{Code: "GLOBAL-0002", Message: "you cannot revoke your own access"}
		}
		if IsSuperAdmin(role) {
			return &ValidationError{Code: "GLOBAL-0002", Message: "a Super Admin's access is not revoked here"}
		}
		if HasFullAccess(role) && status == "Active" {
			if n, cErr := countActiveAdmins(schema); cErr != nil || n <= 1 {
				return &ValidationError{Code: "GLOBAL-0002", Message: "this is the last active admin; add another admin first"}
			}
		}
	}
	if _, err := db.DB.Exec(fmt.Sprintf(`INSERT INTO %s.access_reviews (user_id, username, role, location_code, store_access, decision, note, decided_by)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8)`, schema), userID, username, role, loc, access, decision, note, actorUsername); err != nil {
		return err
	}
	if decision == AccessReviewRevoked {
		if _, err := db.DB.Exec(fmt.Sprintf(`UPDATE %s.users SET status = 'Inactive' WHERE id = $1`, schema), userID); err != nil {
			return err
		}
		InvalidateLiveUserState(tenantID, userID)
		LogAuditEventCtx(ctx, tenantID, actorUsername, "USER_MANAGEMENT", "USER_STATUS_CHANGED",
			fmt.Sprintf("Set user %s status to Inactive - access revoked in review (%s)", userID, note))
		recordLeaverForReview(ctx, tenantID, userID, actorUsername, "access revoked in review")
		return nil
	}
	LogAuditEventCtx(ctx, tenantID, actorUsername, "USER_MANAGEMENT", "ACCESS_REVIEW_CONFIRMED",
		fmt.Sprintf("Access of %s confirmed: role %s, store %s", username, role, loc))
	return nil
}
