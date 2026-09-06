package engines

// Stage 49.1.5 - secure tenant provisioning and deprovisioning.
//
// Provisioning already minted a cryptographically random one-time admin
// password (ProvisionTenantSchema). Everything around it was ungoverned: the
// credential never expired, nothing recorded whether it had ever been
// rotated, there was no reversible "stop serving this tenant" state, and a
// deletion was a bare DROP SCHEMA with no backup, retention or legal-hold gate
// in front of it and no proof of a clean removal behind it.
//
// This file is the lifecycle state machine and its evidence trail. Storage is
// db/migrations_stage49_1_5_tenant_lifecycle.sql; the operator entry point is
// cmd/tenantctl. Deliberately not an HTTP surface: platform-level tenant
// creation and destruction is not something any request-bearing role should be
// able to reach, and keeping it offline adds one CLI command to the attack
// surface inventory (49.1.1) rather than a route.
//
// The state machine is small on purpose:
//
//	active ──suspend──▶ suspended ──resume──▶ active
//	   │                    │
//	   └────deprovision─────┴──▶ deprovision_requested ──purge──▶ (no row)
//
// suspended and deprovision_requested are identical in their effect on live
// traffic - every login and every existing session for the tenant is refused -
// and differ only in intent and in whether the retention clock is running.

import (
	"custom_erp/db"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"golang.org/x/crypto/bcrypt"
)

// Lifecycle states. A purged tenant has no registry row at all, which is what
// makes every lookup path fail closed with no extra check: db.GetTenantSchema
// cannot resolve it, so no route, worker, job or session can address it.
const (
	TenantStatusActive               = "active"
	TenantStatusSuspended            = "suspended"
	TenantStatusDeprovisionRequested = "deprovision_requested"
)

// Lifecycle event names written to public.tenant_lifecycle_events.
const (
	TenantEventProvisioned          = "provisioned"
	TenantEventBootstrapIssued      = "bootstrap_issued"
	TenantEventBootstrapConsumed    = "bootstrap_consumed"
	TenantEventBootstrapExpired     = "bootstrap_expired"
	TenantEventBootstrapRotated     = "bootstrap_rotated"
	TenantEventSuspended            = "suspended"
	TenantEventResumed              = "resumed"
	TenantEventDeprovisionRequested = "deprovision_requested"
	TenantEventLegalHoldSet         = "legal_hold_set"
	TenantEventLegalHoldCleared     = "legal_hold_cleared"
	TenantEventPurgeRefused         = "purge_refused"
	TenantEventPurged               = "purged"
	TenantEventResidueVerified      = "verified"
)

// ErrTenantNotRegistered is returned when a tenant id has no registry row -
// either it never existed or it has been purged. Callers treat it as "this
// tenant cannot be served", never as a transient failure.
var ErrTenantNotRegistered = errors.New("tenant is not registered")

const (
	defaultBootstrapTTLHours = 72
	defaultRetentionDays     = 30

	// UseDefaultRetention asks RequestTenantDeprovision for the policy
	// default rather than an explicit number of days. A caller-supplied 0 is
	// honoured as a genuine zero-retention agreement, so "unset" needs its
	// own value rather than overloading zero.
	UseDefaultRetention = -1
)

// bootstrapCredentialTTL bounds how long a hand-delivered one-time tenant
// admin password stays usable. After it, the credential is refused at login
// and an operator has to reissue one - which is recorded. 72h is long enough
// for a real handover across a weekend and short enough that a forgotten
// tenant is not a standing credential.
func bootstrapCredentialTTL() time.Duration {
	if v := os.Getenv("TENANT_BOOTSTRAP_TTL_HOURS"); v != "" {
		if hours, err := strconv.Atoi(v); err == nil && hours > 0 {
			return time.Duration(hours) * time.Hour
		}
	}
	return defaultBootstrapTTLHours * time.Hour
}

// tenantRetentionDays is how long a deprovisioned tenant's data is kept before
// a purge is permitted. It is a floor, not a schedule: nothing deletes
// automatically, an operator still has to run the purge and supply a backup
// reference.
func tenantRetentionDays() int {
	if v := os.Getenv("TENANT_RETENTION_DAYS"); v != "" {
		if days, err := strconv.Atoi(v); err == nil && days >= 0 {
			return days
		}
	}
	return defaultRetentionDays
}

// TenantLifecycle is the registry's current view of one tenant.
type TenantLifecycle struct {
	TenantID   string
	SchemaName string
	Status     string
	Reason     string
	IsSandbox  bool

	ProvisionedAt  *time.Time
	ChangedAt      *time.Time
	RetentionUntil *time.Time

	LegalHold       bool
	LegalHoldReason string

	BootstrapIssuedAt   *time.Time
	BootstrapExpiresAt  *time.Time
	BootstrapConsumedAt *time.Time
	// BootstrapOutstanding is true while a one-time credential has been issued
	// and never rotated. BootstrapExpired additionally means it is past its
	// expiry and is now refused at login.
	BootstrapOutstanding bool
	BootstrapExpired     bool
}

// TenantLifecycleEvent is one row of the append-only evidence trail.
type TenantLifecycleEvent struct {
	TenantID   string
	Event      string
	Actor      string
	Detail     string
	OccurredAt time.Time
}

const tenantLifecycleColumns = `tenant_id, schema_name, lifecycle_status, COALESCE(lifecycle_reason, ''),
	COALESCE(is_sandbox, FALSE), provisioned_at, lifecycle_changed_at, retention_until,
	COALESCE(legal_hold, FALSE), COALESCE(legal_hold_reason, ''),
	bootstrap_password_hash, bootstrap_issued_at, bootstrap_expires_at, bootstrap_consumed_at`

func scanTenantLifecycle(scan func(dest ...interface{}) error) (TenantLifecycle, error) {
	var t TenantLifecycle
	var hash sql.NullString
	var provisioned, changed, retention, issued, expires, consumed sql.NullTime
	if err := scan(&t.TenantID, &t.SchemaName, &t.Status, &t.Reason, &t.IsSandbox,
		&provisioned, &changed, &retention, &t.LegalHold, &t.LegalHoldReason,
		&hash, &issued, &expires, &consumed); err != nil {
		return TenantLifecycle{}, err
	}
	assign := func(n sql.NullTime) *time.Time {
		if !n.Valid {
			return nil
		}
		v := n.Time
		return &v
	}
	t.ProvisionedAt, t.ChangedAt, t.RetentionUntil = assign(provisioned), assign(changed), assign(retention)
	t.BootstrapIssuedAt, t.BootstrapExpiresAt, t.BootstrapConsumedAt = assign(issued), assign(expires), assign(consumed)
	t.BootstrapOutstanding = hash.Valid && hash.String != "" && !consumed.Valid
	t.BootstrapExpired = t.BootstrapOutstanding && expires.Valid && time.Now().After(expires.Time)
	return t, nil
}

// GetTenantLifecycle reads one tenant's registry row.
func GetTenantLifecycle(tenantID string) (TenantLifecycle, error) {
	row := db.DB.QueryRow(`SELECT `+tenantLifecycleColumns+` FROM public.tenants WHERE tenant_id = $1`, tenantID)
	t, err := scanTenantLifecycle(row.Scan)
	if err == sql.ErrNoRows {
		return TenantLifecycle{}, ErrTenantNotRegistered
	}
	return t, err
}

// ListTenantLifecycles returns every registered tenant, oldest first.
func ListTenantLifecycles() ([]TenantLifecycle, error) {
	rows, err := db.DB.Query(`SELECT ` + tenantLifecycleColumns + ` FROM public.tenants ORDER BY created_at, tenant_id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []TenantLifecycle{}
	for rows.Next() {
		t, err := scanTenantLifecycle(rows.Scan)
		if err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

// RecordTenantLifecycleEvent appends one row of evidence. Never foreign-keyed
// to public.tenants: a purge deletes the registry row and this trail is what
// outlives it.
func RecordTenantLifecycleEvent(tenantID, event, actor, detail string) error {
	if actor == "" {
		actor = "system"
	}
	_, err := db.DB.Exec(
		`INSERT INTO public.tenant_lifecycle_events (tenant_id, event, actor, detail) VALUES ($1, $2, $3, $4)`,
		tenantID, event, actor, detail)
	return err
}

// recordTenantLifecycleEventTx is the same insert inside a caller's
// transaction, so provisioning evidence commits with the provisioning itself
// rather than describing a tenant that was rolled back.
func recordTenantLifecycleEventTx(tx *sql.Tx, tenantID, event, actor, detail string) error {
	if actor == "" {
		actor = "system"
	}
	_, err := tx.Exec(
		`INSERT INTO public.tenant_lifecycle_events (tenant_id, event, actor, detail) VALUES ($1, $2, $3, $4)`,
		tenantID, event, actor, detail)
	return err
}

// TenantLifecycleEvents returns the newest evidence rows for a tenant, purged
// ones included.
func TenantLifecycleEvents(tenantID string, limit int) ([]TenantLifecycleEvent, error) {
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	rows, err := db.DB.Query(`
		SELECT tenant_id, event, actor, detail, occurred_at
		  FROM public.tenant_lifecycle_events
		 WHERE tenant_id = $1
		 ORDER BY occurred_at DESC, id DESC
		 LIMIT $2`, tenantID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []TenantLifecycleEvent{}
	for rows.Next() {
		var e TenantLifecycleEvent
		if err := rows.Scan(&e.TenantID, &e.Event, &e.Actor, &e.Detail, &e.OccurredAt); err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

// ---------------------------------------------------------------------------
// Operational gate
// ---------------------------------------------------------------------------

// Cached because ResolveLiveUserState consults it on every authenticated
// request that misses the user cache. One small indexed read per tenant per
// window, sharing the TTL of the live user state it sits in front of, so a
// suspension is felt within the same already-documented SLO.
type tenantGateEntry struct {
	operational bool
	registered  bool
	fetchedAt   time.Time
}

var (
	tenantGateMu    sync.RWMutex
	tenantGateCache = map[string]tenantGateEntry{}
)

// TenantIsOperational reports whether a tenant may currently serve traffic.
// A suspended or deprovision-requested tenant is refused; an unregistered one
// is refused with ErrTenantNotRegistered rather than being treated as a
// database failure, so it fails closed as a rejected session rather than a
// retryable 503.
func TenantIsOperational(tenantID string) (bool, error) {
	// "" and "default" resolve to tenant_default without a registry lookup in
	// db.GetTenantSchema; a database that has never had a default row (fresh
	// test fixtures) must keep behaving exactly as it did before this gate.
	canonical := tenantID
	if canonical == "" {
		canonical = "default"
	}
	if db.DB == nil {
		// Nothing can be served without a database anyway; this only keeps the
		// gate from being the thing that panics first, matching
		// refreshTenantHostCache's own defensive check.
		return true, nil
	}

	ttl := authStateCacheTTL()
	if ttl > 0 {
		tenantGateMu.RLock()
		entry, ok := tenantGateCache[canonical]
		tenantGateMu.RUnlock()
		if ok && time.Since(entry.fetchedAt) < ttl {
			if !entry.registered {
				if canonical == "default" {
					return true, nil
				}
				return false, ErrTenantNotRegistered
			}
			return entry.operational, nil
		}
	}

	var status string
	err := db.DB.QueryRow(`SELECT lifecycle_status FROM public.tenants WHERE tenant_id = $1`, canonical).Scan(&status)
	switch {
	case err == sql.ErrNoRows:
		cacheTenantGate(canonical, tenantGateEntry{registered: false}, ttl)
		if canonical == "default" {
			return true, nil
		}
		return false, ErrTenantNotRegistered
	case err != nil:
		// A genuine database failure must not be cached and must not be
		// mistaken for a suspension - same rule as ResolveLiveUserState.
		return false, err
	}

	operational := status == TenantStatusActive
	cacheTenantGate(canonical, tenantGateEntry{operational: operational, registered: true}, ttl)
	return operational, nil
}

func cacheTenantGate(tenantID string, entry tenantGateEntry, ttl time.Duration) {
	if ttl <= 0 {
		return
	}
	entry.fetchedAt = time.Now()
	tenantGateMu.Lock()
	if len(tenantGateCache) >= authStateCacheMaxEntries {
		tenantGateCache = map[string]tenantGateEntry{}
	}
	tenantGateCache[tenantID] = entry
	tenantGateMu.Unlock()
}

// InvalidateTenantLifecycleCache drops a tenant's cached gate decision so a
// suspend, resume or purge takes effect on the very next request instead of
// waiting out the window. Called by every transition below.
func InvalidateTenantLifecycleCache(tenantID string) {
	if tenantID == "" {
		tenantID = "default"
	}
	tenantGateMu.Lock()
	delete(tenantGateCache, tenantID)
	tenantGateMu.Unlock()
}

// ResetTenantLifecycleCache clears the whole gate cache. Test-support only,
// matching ResetLiveUserStateCache.
func ResetTenantLifecycleCache() {
	tenantGateMu.Lock()
	tenantGateCache = map[string]tenantGateEntry{}
	tenantGateMu.Unlock()
}

// tenantHasCachedGate reports whether this process still holds a gate decision
// for a tenant. Used by the post-purge residue proof.
func tenantHasCachedGate(tenantID string) bool {
	if tenantID == "" {
		tenantID = "default"
	}
	tenantGateMu.RLock()
	_, ok := tenantGateCache[tenantID]
	tenantGateMu.RUnlock()
	return ok
}

// ---------------------------------------------------------------------------
// Bootstrap credential
// ---------------------------------------------------------------------------

// BootstrapState is what the login path needs to know about a tenant's
// one-time provisioning credential.
type BootstrapState int

const (
	// BootstrapNotApplicable - this account is not holding an unrotated
	// bootstrap credential. The overwhelmingly common case.
	BootstrapNotApplicable BootstrapState = iota
	// BootstrapPending - the one-time credential is still in force and within
	// its window. Login is allowed and the caller is told to rotate.
	BootstrapPending
	// BootstrapExpired - the one-time credential was never rotated and its
	// window has closed. Login is refused until an operator reissues one.
	BootstrapExpired
)

// recordBootstrapCredentialTx stamps a freshly issued one-time credential onto
// the registry row inside the caller's transaction.
func recordBootstrapCredentialTx(tx *sql.Tx, tenantID, passwordHash string, ttl time.Duration) error {
	_, err := tx.Exec(`
		UPDATE public.tenants
		   SET bootstrap_password_hash = $1,
		       bootstrap_issued_at = NOW(),
		       bootstrap_expires_at = NOW() + make_interval(secs => $2),
		       bootstrap_consumed_at = NULL
		 WHERE tenant_id = $3`, passwordHash, ttl.Seconds(), tenantID)
	return err
}

// EvaluateTenantBootstrapCredential decides what the login path should do with
// an account whose password has just verified.
//
// The test is deliberately a byte comparison of the account's stored bcrypt
// hash against the hash recorded at issue time: bcrypt salts every hash, so
// equality can only mean "this row has not been written since we issued the
// credential". Any rotation - through the app, through an operator reset,
// through raw SQL - breaks the equality, at which point the credential is
// marked consumed and never looked at again.
func EvaluateTenantBootstrapCredential(tenantID, currentPasswordHash string) (BootstrapState, *time.Time, error) {
	canonical := tenantID
	if canonical == "" {
		canonical = "default"
	}
	var hash sql.NullString
	var expires, consumed sql.NullTime
	err := db.DB.QueryRow(`
		SELECT bootstrap_password_hash, bootstrap_expires_at, bootstrap_consumed_at
		  FROM public.tenants WHERE tenant_id = $1`, canonical).Scan(&hash, &expires, &consumed)
	if err == sql.ErrNoRows {
		return BootstrapNotApplicable, nil, nil
	}
	if err != nil {
		return BootstrapNotApplicable, nil, err
	}
	if !hash.Valid || hash.String == "" || consumed.Valid {
		return BootstrapNotApplicable, nil, nil
	}

	if hash.String != currentPasswordHash {
		// Rotated. Record it once - clearing the hash makes this branch
		// unreachable afterwards - and stop treating the account as bootstrap.
		if _, err := db.DB.Exec(`
			UPDATE public.tenants
			   SET bootstrap_consumed_at = NOW(), bootstrap_password_hash = NULL
			 WHERE tenant_id = $1 AND bootstrap_consumed_at IS NULL`, canonical); err == nil {
			_ = RecordTenantLifecycleEvent(canonical, TenantEventBootstrapConsumed, "system",
				"one-time provisioning credential was rotated; tenant is now on an operator-chosen password")
		}
		return BootstrapNotApplicable, nil, nil
	}

	if expires.Valid && time.Now().After(expires.Time) {
		return BootstrapExpired, &expires.Time, nil
	}
	if expires.Valid {
		return BootstrapPending, &expires.Time, nil
	}
	return BootstrapPending, nil, nil
}

// RotateTenantBootstrapCredential issues a fresh one-time password for a
// tenant's admin account - the reset control behind an expired or lost
// handover credential. Returns the new password exactly once; it is never
// persisted in plaintext, the same guarantee provisioning gives.
func RotateTenantBootstrapCredential(tenantID, adminUsername, actor, reason string) (string, error) {
	if reason == "" {
		return "", fmt.Errorf("a reason is required and is recorded in the tenant's evidence trail")
	}
	life, err := GetTenantLifecycle(tenantID)
	if err != nil {
		return "", err
	}
	if life.Status != TenantStatusActive {
		return "", fmt.Errorf("tenant %s is %s - resume it before issuing a credential", tenantID, life.Status)
	}
	if !validSQLIdentifier(life.SchemaName) {
		return "", fmt.Errorf("registry holds an unusable schema name %q for %s", life.SchemaName, tenantID)
	}
	if adminUsername == "" {
		adminUsername = "admin"
	}

	password, err := generateRandomPassword()
	if err != nil {
		return "", err
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return "", err
	}

	tx, err := db.DB.Begin()
	if err != nil {
		return "", err
	}
	defer tx.Rollback()

	// The rotation must land on a real, active account or it is a credential
	// pointing at nothing - fail rather than silently succeed.
	res, err := tx.Exec(fmt.Sprintf(`
		UPDATE %s.users SET password_hash = $1, failed_login_count = 0, locked_until = NULL
		 WHERE username = $2 AND status = 'Active'`, life.SchemaName), string(hash), adminUsername)
	if err != nil {
		return "", fmt.Errorf("failed to set the new credential: %v", err)
	}
	if n, _ := res.RowsAffected(); n != 1 {
		return "", fmt.Errorf("no single active user %q in %s - nothing to rotate", adminUsername, life.SchemaName)
	}
	if err := recordBootstrapCredentialTx(tx, tenantID, string(hash), bootstrapCredentialTTL()); err != nil {
		return "", err
	}
	if err := recordTenantLifecycleEventTx(tx, tenantID, TenantEventBootstrapRotated, actor,
		fmt.Sprintf("one-time credential reissued for %s.%s: %s", life.SchemaName, adminUsername, reason)); err != nil {
		return "", err
	}
	if err := tx.Commit(); err != nil {
		return "", err
	}

	invalidateLiveUserStatesForTenant(tenantID)
	return password, nil
}

// ---------------------------------------------------------------------------
// Lifecycle transitions
// ---------------------------------------------------------------------------

func setTenantStatus(tenantID, from, to, actor, reason, event string) error {
	if reason == "" {
		return fmt.Errorf("a reason is required and is recorded in the tenant's evidence trail")
	}
	life, err := GetTenantLifecycle(tenantID)
	if err != nil {
		return err
	}
	if life.Status == to {
		return fmt.Errorf("tenant %s is already %s", tenantID, to)
	}
	if from != "" && life.Status != from {
		return fmt.Errorf("tenant %s is %s, not %s", tenantID, life.Status, from)
	}

	tx, err := db.DB.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.Exec(`
		UPDATE public.tenants
		   SET lifecycle_status = $1, lifecycle_reason = $2, lifecycle_changed_at = NOW()
		 WHERE tenant_id = $3`, to, reason, tenantID); err != nil {
		return err
	}
	if err := recordTenantLifecycleEventTx(tx, tenantID, event, actor, reason); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return err
	}

	// Every live session for this tenant has to feel the change, not just the
	// next login: drop both caches immediately rather than waiting out their
	// windows.
	InvalidateTenantLifecycleCache(tenantID)
	invalidateLiveUserStatesForTenant(tenantID)
	return nil
}

// SuspendTenant stops a tenant serving traffic without destroying anything.
// Reversible; the tenant's data is untouched.
func SuspendTenant(tenantID, actor, reason string) error {
	return setTenantStatus(tenantID, TenantStatusActive, TenantStatusSuspended, actor, reason, TenantEventSuspended)
}

// ResumeTenant returns a suspended tenant to service. Deliberately refuses a
// tenant already in deprovisioning: coming back from that is a decision with a
// retention clock attached to it, not an undo.
func ResumeTenant(tenantID, actor, reason string) error {
	return setTenantStatus(tenantID, TenantStatusSuspended, TenantStatusActive, actor, reason, TenantEventResumed)
}

// RequestTenantDeprovision starts offboarding: traffic stops immediately and
// the retention clock starts. Nothing is deleted here - PurgeTenant is a
// separate, separately-evidenced act, and it refuses until this clock has run
// out. Pass UseDefaultRetention for the policy default; an explicit 0 is
// honoured as an agreed zero-retention offboarding.
func RequestTenantDeprovision(tenantID, actor, reason string, retentionDays int) (time.Time, error) {
	if reason == "" {
		return time.Time{}, fmt.Errorf("a reason is required and is recorded in the tenant's evidence trail")
	}
	life, err := GetTenantLifecycle(tenantID)
	if err != nil {
		return time.Time{}, err
	}
	if life.Status == TenantStatusDeprovisionRequested {
		return time.Time{}, fmt.Errorf("tenant %s is already in deprovisioning", tenantID)
	}
	if retentionDays < 0 {
		retentionDays = tenantRetentionDays()
	}

	tx, err := db.DB.Begin()
	if err != nil {
		return time.Time{}, err
	}
	defer tx.Rollback()

	var retentionUntil time.Time
	if err := tx.QueryRow(`
		UPDATE public.tenants
		   SET lifecycle_status = $1, lifecycle_reason = $2, lifecycle_changed_at = NOW(),
		       retention_until = NOW() + make_interval(days => $3)
		 WHERE tenant_id = $4
		 RETURNING retention_until`,
		TenantStatusDeprovisionRequested, reason, retentionDays, tenantID).Scan(&retentionUntil); err != nil {
		return time.Time{}, err
	}
	if err := recordTenantLifecycleEventTx(tx, tenantID, TenantEventDeprovisionRequested, actor,
		fmt.Sprintf("%s (retention %d day(s); purge not permitted before %s)",
			reason, retentionDays, retentionUntil.Format(time.RFC3339))); err != nil {
		return time.Time{}, err
	}
	if err := tx.Commit(); err != nil {
		return time.Time{}, err
	}

	InvalidateTenantLifecycleCache(tenantID)
	invalidateLiveUserStatesForTenant(tenantID)
	return retentionUntil, nil
}

// SetTenantLegalHold blocks (or unblocks) any purge of a tenant's data
// regardless of retention. A hold outranks every other purge precondition.
func SetTenantLegalHold(tenantID string, on bool, actor, reason string) error {
	if reason == "" {
		return fmt.Errorf("a reason is required and is recorded in the tenant's evidence trail")
	}
	if _, err := GetTenantLifecycle(tenantID); err != nil {
		return err
	}
	tx, err := db.DB.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.Exec(
		`UPDATE public.tenants SET legal_hold = $1, legal_hold_reason = $2 WHERE tenant_id = $3`,
		on, reason, tenantID); err != nil {
		return err
	}
	event := TenantEventLegalHoldCleared
	if on {
		event = TenantEventLegalHoldSet
	}
	if err := recordTenantLifecycleEventTx(tx, tenantID, event, actor, reason); err != nil {
		return err
	}
	return tx.Commit()
}

// ---------------------------------------------------------------------------
// Purge and residue proof
// ---------------------------------------------------------------------------

// TenantResidueReport is the "nothing of this tenant remains" proof. Every
// field is something that could still address the tenant after a deletion.
type TenantResidueReport struct {
	TenantID     string
	SchemaName   string
	RegistryRow  bool           // a row in public.tenants still resolves the tenant
	SchemaExists bool           // the schema itself still exists
	SchemaTables int            // tables left inside it
	HostSlug     string         // hostname still mapped to the tenant
	PublicRows   map[string]int // public.<table> -> rows still carrying this tenant_id
	CachedState  bool           // an in-process auth/gate cache entry still exists
	Clean        bool
}

// VerifyTenantResidue proves - or disproves - that no route, worker, hostname,
// cached session or stray row can still address a tenant. Safe to run before a
// purge too, where it simply describes what is there.
func VerifyTenantResidue(tenantID, schemaName string) (TenantResidueReport, error) {
	rep := TenantResidueReport{TenantID: tenantID, SchemaName: schemaName, PublicRows: map[string]int{}}

	var slug sql.NullString
	err := db.DB.QueryRow(`SELECT schema_name, host_slug FROM public.tenants WHERE tenant_id = $1`, tenantID).Scan(&rep.SchemaName, &slug)
	switch {
	case err == nil:
		rep.RegistryRow = true
		if slug.Valid {
			rep.HostSlug = slug.String
		}
	case err == sql.ErrNoRows:
		rep.SchemaName = schemaName
	default:
		return rep, err
	}

	if rep.SchemaName != "" {
		if err := db.DB.QueryRow(
			`SELECT EXISTS (SELECT 1 FROM pg_namespace WHERE nspname = $1)`, rep.SchemaName).Scan(&rep.SchemaExists); err != nil {
			return rep, err
		}
		if rep.SchemaExists {
			if err := db.DB.QueryRow(
				`SELECT COUNT(*) FROM pg_tables WHERE schemaname = $1`, rep.SchemaName).Scan(&rep.SchemaTables); err != nil {
				return rep, err
			}
		}
	}

	// Any public table that carries a tenant_id, discovered rather than
	// hardcoded, so a table added by a later stage is covered without anyone
	// remembering to come back here. The evidence trail is the deliberate
	// exception: it has to survive the tenant it describes.
	rows, err := db.DB.Query(`
		SELECT table_name FROM information_schema.columns
		 WHERE table_schema = 'public' AND column_name = 'tenant_id'
		   AND table_name <> 'tenant_lifecycle_events'
		 ORDER BY table_name`)
	if err != nil {
		return rep, err
	}
	defer rows.Close()
	var tables []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return rep, err
		}
		tables = append(tables, name)
	}
	if err := rows.Err(); err != nil {
		return rep, err
	}
	for _, table := range tables {
		if !validSQLIdentifier(table) {
			continue
		}
		var n int
		if err := db.DB.QueryRow(
			fmt.Sprintf(`SELECT COUNT(*) FROM public.%s WHERE tenant_id = $1`, table), tenantID).Scan(&n); err != nil {
			return rep, err
		}
		if n > 0 {
			rep.PublicRows[table] = n
		}
	}

	rep.CachedState = tenantHasCachedGate(tenantID) || tenantHasCachedUserState(tenantID)
	rep.Clean = !rep.RegistryRow && !rep.SchemaExists && rep.HostSlug == "" && len(rep.PublicRows) == 0 && !rep.CachedState
	return rep, nil
}

// validSQLIdentifier guards the two places here that have to interpolate a
// name read back from the catalog or the registry rather than bind it.
func validSQLIdentifier(name string) bool {
	if name == "" || len(name) > 63 {
		return false
	}
	for i, c := range name {
		switch {
		case c >= 'a' && c <= 'z', c >= 'A' && c <= 'Z', c == '_':
		case i > 0 && c >= '0' && c <= '9':
		default:
			return false
		}
	}
	return true
}

// PurgeTenant permanently destroys a tenant. Every precondition below is a
// refusal, not a warning, and each refusal is itself recorded:
//
//   - the tenant must already be in deprovision_requested, so a purge can
//     never be the first act taken against a live tenant;
//   - no legal hold;
//   - the retention window must have elapsed;
//   - a backup reference must be supplied, and it goes into the evidence trail
//     as the answer to "where is this tenant's data if it is needed again".
//
// The drop and the registry delete share one transaction (PostgreSQL DDL is
// transactional), so a failure cannot leave a registry row pointing at a
// dropped schema.
func PurgeTenant(tenantID, actor, backupRef string) (TenantResidueReport, error) {
	refuse := func(why string) (TenantResidueReport, error) {
		_ = RecordTenantLifecycleEvent(tenantID, TenantEventPurgeRefused, actor, why)
		return TenantResidueReport{TenantID: tenantID}, errors.New(why)
	}

	life, err := GetTenantLifecycle(tenantID)
	if err != nil {
		return TenantResidueReport{TenantID: tenantID}, err
	}
	if life.Status != TenantStatusDeprovisionRequested {
		return refuse(fmt.Sprintf("refused: tenant is %s - deprovision it first so the retention clock runs", life.Status))
	}
	if life.LegalHold {
		return refuse(fmt.Sprintf("refused: legal hold in force (%s)", life.LegalHoldReason))
	}
	if life.RetentionUntil != nil && time.Now().Before(*life.RetentionUntil) {
		return refuse(fmt.Sprintf("refused: retention window runs until %s", life.RetentionUntil.Format(time.RFC3339)))
	}
	if strings.TrimSpace(backupRef) == "" {
		return refuse("refused: no backup reference supplied - a purge must record where the tenant's data was preserved")
	}
	if !validSQLIdentifier(life.SchemaName) {
		return refuse(fmt.Sprintf("refused: registry holds an unusable schema name %q", life.SchemaName))
	}
	if life.SchemaName == "tenant_default" {
		return refuse("refused: tenant_default is the template every tenant is cloned from")
	}

	tx, err := db.DB.Begin()
	if err != nil {
		return TenantResidueReport{TenantID: tenantID}, err
	}
	defer tx.Rollback()
	if _, err := tx.Exec(fmt.Sprintf(`DROP SCHEMA IF EXISTS %s CASCADE`, life.SchemaName)); err != nil {
		return TenantResidueReport{TenantID: tenantID}, err
	}
	if _, err := tx.Exec(`DELETE FROM public.tenants WHERE tenant_id = $1`, tenantID); err != nil {
		return TenantResidueReport{TenantID: tenantID}, err
	}
	if err := recordTenantLifecycleEventTx(tx, tenantID, TenantEventPurged, actor,
		fmt.Sprintf("schema %s dropped and registry row deleted; backup reference: %s", life.SchemaName, backupRef)); err != nil {
		return TenantResidueReport{TenantID: tenantID}, err
	}
	if err := tx.Commit(); err != nil {
		return TenantResidueReport{TenantID: tenantID}, err
	}

	InvalidateTenantLifecycleCache(tenantID)
	invalidateLiveUserStatesForTenant(tenantID)

	rep, err := VerifyTenantResidue(tenantID, life.SchemaName)
	if err != nil {
		return rep, err
	}
	detail := "verified clean: no registry row, schema, hostname, public-table row or cached session remains"
	if !rep.Clean {
		detail = fmt.Sprintf("RESIDUE REMAINS after purge: registry=%v schema=%v tables=%d host=%q public_rows=%v cached=%v",
			rep.RegistryRow, rep.SchemaExists, rep.SchemaTables, rep.HostSlug, rep.PublicRows, rep.CachedState)
	}
	_ = RecordTenantLifecycleEvent(tenantID, TenantEventResidueVerified, actor, detail)
	return rep, nil
}

// ---------------------------------------------------------------------------
// Least-privilege database ownership
// ---------------------------------------------------------------------------

// TenantDatabasePrivilege describes the database role the application actually
// connects as, and who owns a tenant's schema. 49.1.5 asks for least-privilege
// DB ownership; what this repo can assert from inside the process is the
// posture, not the fix - creating and granting roles is deployment state and
// belongs to 49.7.4. Reporting it is what makes the gap visible rather than
// assumed.
type TenantDatabasePrivilege struct {
	ConnectedRole string
	IsSuperuser   bool
	CanCreateRole bool
	CanCreateDB   bool
	SchemaOwner   string
	Findings      []string
}

// TenantDatabasePrivilegeReport inspects the connected role and, when a schema
// name is given, that schema's owner.
func TenantDatabasePrivilegeReport(schemaName string) (TenantDatabasePrivilege, error) {
	var p TenantDatabasePrivilege
	if err := db.DB.QueryRow(`
		SELECT current_user,
		       COALESCE((SELECT rolsuper FROM pg_roles WHERE rolname = current_user), FALSE),
		       COALESCE((SELECT rolcreaterole FROM pg_roles WHERE rolname = current_user), FALSE),
		       COALESCE((SELECT rolcreatedb FROM pg_roles WHERE rolname = current_user), FALSE)
	`).Scan(&p.ConnectedRole, &p.IsSuperuser, &p.CanCreateRole, &p.CanCreateDB); err != nil {
		return p, err
	}
	if schemaName != "" {
		var owner sql.NullString
		if err := db.DB.QueryRow(
			`SELECT pg_get_userbyid(nspowner) FROM pg_namespace WHERE nspname = $1`, schemaName).Scan(&owner); err != nil && err != sql.ErrNoRows {
			return p, err
		}
		p.SchemaOwner = owner.String
	}
	if p.IsSuperuser {
		p.Findings = append(p.Findings, "the application connects as a PostgreSQL SUPERUSER: a SQL-injection or code-execution bug reaches every database on the cluster and the filesystem. Run the app as an owner-only role (49.7.4).")
	}
	if p.CanCreateRole {
		p.Findings = append(p.Findings, "the application's database role has CREATEROLE: it can grant itself privileges it was not given.")
	}
	if p.CanCreateDB {
		p.Findings = append(p.Findings, "the application's database role has CREATEDB.")
	}
	return p, nil
}
