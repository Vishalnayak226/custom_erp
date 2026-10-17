package engines

import (
	"custom_erp/db"
	"fmt"
	"time"
)

// Stage 65.6 (the user, 2026-10-17: "How to pause? or 30 day trial ... I need
// in UI"): a free trial per client. A client on trial works normally until
// public.tenants.trial_ends_at; from then on TenantIsOperational refuses it the
// same way it refuses a paused client - new logins and live sessions alike,
// nothing deleted. Extending the trial, or ending it because the client has
// paid, gives access back at once.
//
// No background job: the gate every login and every session already passes
// through compares the date (cached alongside the status, so a trial that runs
// out mid-window still stops on time), the same way sandbox expiry works.

const (
	TenantEventTrialSet   = "trial_set"
	TenantEventTrialEnded = "trial_ended"

	// DefaultTrialDays is what the screens offer; MaxTrialDays bounds a typo.
	DefaultTrialDays = 30
	MaxTrialDays     = 365
)

// trialOver reports whether a trial end date has passed. No date, no trial.
func trialOver(endsAt *time.Time) bool {
	return endsAt != nil && !time.Now().Before(*endsAt)
}

// SetTenantTrial gives a client a trial ending `days` days from now - starting
// one, or extending or shortening the current one. days == 0 ends the trial
// because the client has paid: no end date, full access. The change goes to the
// client's evidence trail like every other lifecycle change; reason is optional
// here because the end date itself is the record.
func SetTenantTrial(tenantID string, days int, actor, reason string) (*time.Time, error) {
	if days < 0 || days > MaxTrialDays {
		return nil, fmt.Errorf("a trial is 1 to %d days, or 0 to end it", MaxTrialDays)
	}
	life, err := GetTenantLifecycle(tenantID)
	if err != nil {
		return nil, err
	}
	if life.IsSandbox {
		return nil, fmt.Errorf("%s is a sandbox, which has its own expiry - a trial is for a client", tenantID)
	}
	if life.Status != TenantStatusActive && life.Status != TenantStatusSuspended {
		return nil, fmt.Errorf("%s is %s - a trial can only be set on an active or paused client", tenantID, life.Status)
	}

	var endsAt *time.Time
	event, detail := TenantEventTrialEnded, "trial ended - client is paying"
	if days > 0 {
		t := time.Now().AddDate(0, 0, days)
		endsAt = &t
		event, detail = TenantEventTrialSet, fmt.Sprintf("trial ends %s (%d days)", t.Format("2006-01-02 15:04 MST"), days)
	}
	if reason != "" {
		detail += ": " + reason
	}

	tx, err := db.DB.Begin()
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	if _, err := tx.Exec(`UPDATE public.tenants SET trial_ends_at = $1 WHERE tenant_id = $2`, endsAt, tenantID); err != nil {
		return nil, err
	}
	if err := recordTenantLifecycleEventTx(tx, tenantID, event, actor, detail); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	// Same as a pause or resume: every live session feels it now, not after
	// the cache window.
	InvalidateTenantLifecycleCache(tenantID)
	invalidateLiveUserStatesForTenant(tenantID)
	return endsAt, nil
}
