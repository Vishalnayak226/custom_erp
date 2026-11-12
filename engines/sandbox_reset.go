package engines

import (
	"custom_erp/db"
	"database/sql"
	"errors"
	"fmt"
	"io"
	"math/rand/v2"
	"strings"
	"time"
)

// Stage 47.12.6: the synthetic, resettable sandbox.
//
//   - Create: ProvisionSandboxTenant (a normal tenant, flagged a sandbox as
//     it is made) then SeedSandboxTenant - synthetic data only, from the
//     deterministic generator the performance laboratory uses
//     (engines/synthetic_tenant.go).
//   - Reset: ResetSandboxData empties the sandbox with the SAME table
//     classification ResetTenantData uses on a real client, then
//     ResetAndReseedSandbox fills it again from the same seed. It will not
//     run on anything that is not a sandbox: see requireSandbox.
//   - Nothing real goes in: CopyTenantDataIntoSandbox exists to refuse. There
//     is no other path that reads one client's records into another; the
//     sandbox's only data is what the generator invents.
//   - Nothing real goes out: engines/sandbox_effects.go.

// sandboxSeedShape is the one shape a sandbox may be seeded with outside a
// scratch database (CheckSyntheticTarget): the small one, so a sandbox on a
// real server stays small.
const sandboxSeedShape = "small"

var errNotSandbox = errors.New("is not a sandbox - refusing")

// requireSandbox is the guard in front of every destructive sandbox action.
// It reads the registry inside q (a transaction, so the row is locked while
// the action runs) and refuses unless every one of these holds: the tenant
// exists, it is flagged is_sandbox, it carries a sandbox expiry (every
// sandbox is made with one), it is not the default client or the template
// schema, and its schema is a tenant_ schema. The flag itself can only be
// switched on for a tenant created in the last hour (migration trigger).
func requireSandbox(q interface {
	QueryRow(query string, args ...any) *sql.Row
}, tenantID string, lock bool) (schema string, err error) {
	tenantID = strings.TrimSpace(tenantID)
	if tenantID == "" {
		return "", errors.New("a tenant id is required")
	}
	query := `SELECT schema_name, COALESCE(is_sandbox, FALSE), sandbox_expires_at IS NOT NULL FROM public.tenants WHERE tenant_id = $1`
	if lock {
		query += ` FOR UPDATE`
	}
	var flagged, hasExpiry bool
	if err := q.QueryRow(query, tenantID).Scan(&schema, &flagged, &hasExpiry); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return "", fmt.Errorf("no tenant %q", tenantID)
		}
		return "", err
	}
	switch {
	case !flagged || !hasExpiry:
		return "", fmt.Errorf("%s %w", tenantID, errNotSandbox)
	case tenantID == "default" || schema == "tenant_default" || !strings.HasPrefix(schema, "tenant_") || !validSQLIdentifier(schema):
		return "", fmt.Errorf("%s %w", tenantID, errNotSandbox)
	}
	return schema, nil
}

// SandboxSeed says how a sandbox is filled.
type SandboxSeed struct {
	Shape string // "" = small
	Seed  uint64 // one seed always builds the same data
	Log   io.Writer
}

// SeedSandboxTenant fills an empty sandbox with synthetic data. It refuses a
// tenant that is not a sandbox, a sandbox that already holds business
// records (reset it first), and any shape but the small one on a database
// that does not look like a scratch one.
func SeedSandboxTenant(tenantID string, in SandboxSeed) (*SynthResult, error) {
	schema, err := requireSandbox(db.DB, tenantID, false)
	if err != nil {
		return nil, err
	}
	if in.Shape == "" {
		in.Shape = sandboxSeedShape
	}
	shape, ok := SynthShapeNamed(in.Shape)
	if !ok {
		return nil, fmt.Errorf("unknown shape %q (have %s)", in.Shape, strings.Join(SynthShapeNames(), ", "))
	}
	if in.Shape != sandboxSeedShape {
		if err := CheckSyntheticTarget(); err != nil {
			return nil, fmt.Errorf("only the %q shape is seeded here: %w", sandboxSeedShape, err)
		}
	}
	if err := shape.check(); err != nil {
		return nil, err
	}
	plan, err := PlanTenantDataReset(tenantID)
	if err != nil {
		return nil, err
	}
	var held int64
	for _, n := range plan.DocumentsRemoved {
		held += n
	}
	if held > 0 {
		return nil, fmt.Errorf("%s already holds %d business record(s) - reset it first", tenantID, held)
	}
	opts := SynthOptions{Shape: in.Shape, Seed: in.Seed, TenantID: tenantID, Log: in.Log}
	g := &synthGen{opts: opts, shape: shape, tenantID: tenantID, schema: schema, log: in.Log,
		rng: rand.New(rand.NewPCG(in.Seed, 0x4709_0001))}
	return g.build(time.Now())
}

// ResetSandboxData empties a sandbox back to a freshly provisioned client, by
// the table classification a real client's reset uses (TestTenantResetClassifiesEveryTable
// holds both to it). Unlike ResetTenantData it needs no suspension, backup or
// second person: a sandbox holds only invented data. It still refuses a
// client under a legal hold or a table nobody classified, and refuses anything
// that is not a sandbox - checked again inside the transaction, on the locked
// registry row, so the flag cannot change between the check and the delete.
// The sandbox's expiry is pushed out by a fresh TTL window and the
// suppressed-effect log of the rehearsal is cleared with the data.
func ResetSandboxData(tenantID, actor, reason string) (*TenantResetPlan, error) {
	if strings.TrimSpace(reason) == "" {
		return nil, errors.New("a reason is required and is recorded in the sandbox's evidence trail")
	}
	if _, err := requireSandbox(db.DB, tenantID, false); err != nil {
		return nil, err
	}
	plan, err := PlanTenantDataReset(tenantID)
	if err != nil {
		return nil, err
	}
	if len(plan.Unclassified) > 0 {
		return plan, fmt.Errorf("not reset: tables not classified as kept or cleared: %s", strings.Join(plan.Unclassified, ", "))
	}
	if life, err := GetTenantLifecycle(tenantID); err != nil {
		return nil, err
	} else if life.LegalHold {
		return plan, errors.New("not reset: the client is under a legal hold")
	}
	tx, err := db.DB.Begin()
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	schema, err := requireSandbox(tx, tenantID, true)
	if err != nil {
		return nil, err
	}
	if schema != plan.Schema {
		return nil, errors.New("the sandbox's schema changed during the reset - nothing was reset")
	}
	if err := applyTenantResetTablesTx(tx, plan); err != nil {
		return nil, err
	}
	if _, err := tx.Exec(`DELETE FROM public.sandbox_effect_log WHERE tenant_id = $1`, tenantID); err != nil {
		return nil, err
	}
	if _, err := tx.Exec(`UPDATE public.tenants SET sandbox_expires_at = CURRENT_TIMESTAMP + ($1 || ' days')::interval WHERE tenant_id = $2`, sandboxDefaultTTLDays, tenantID); err != nil {
		return nil, err
	}
	removed := int64(0)
	for _, n := range plan.DocumentsRemoved {
		removed += n
	}
	if err := recordTenantLifecycleEventTx(tx, tenantID, TenantEventDataReset, actor,
		fmt.Sprintf("sandbox reset: %d record(s) removed, %d login(s) kept; reason: %s", removed, plan.Users, reason)); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	InvalidateTenantLifecycleCache(tenantID)
	invalidateSandboxFlag()
	plan.Applied = true
	if life, err := GetTenantLifecycle(tenantID); err == nil && life.Status == TenantStatusSuspended && strings.HasPrefix(life.Reason, sandboxExpiredReasonPrefix) {
		if err := ResumeTenant(tenantID, "system:sandbox-reset", "sandbox reset: a new expiry was set"); err != nil {
			return plan, err
		}
	}
	return plan, nil
}

// ResetAndReseedSandbox is the rehearsal's one step: empty the sandbox, then
// fill it again from the seed.
func ResetAndReseedSandbox(tenantID, actor, reason string, in SandboxSeed) (*SynthResult, error) {
	if _, err := ResetSandboxData(tenantID, actor, reason); err != nil {
		return nil, err
	}
	return SeedSandboxTenant(tenantID, in)
}

// CreateSyntheticSandbox makes a new sandbox client and fills it with
// synthetic data. adminPassword is the one-time password of its admin login
// (ProvisionTenantSchema's own guarantee: shown once, stored as a hash).
func CreateSyntheticSandbox(appVersion string, ttlDays int, in SandboxSeed) (tenantID, adminPassword string, res *SynthResult, err error) {
	tenantID, _, adminPassword, err = ProvisionSandboxTenant(appVersion, ttlDays)
	if err != nil {
		return "", "", nil, err
	}
	if res, err = SeedSandboxTenant(tenantID, in); err != nil {
		return tenantID, adminPassword, nil, fmt.Errorf("the sandbox %s was created but not filled: %w", tenantID, err)
	}
	return tenantID, adminPassword, res, nil
}

// CopyTenantDataIntoSandbox always refuses. A sandbox is filled only by the
// synthetic generator: a client's records - customers, staff, prices, the
// ledger - never go into a place built to be shown, shared and thrown away.
func CopyTenantDataIntoSandbox(sourceTenantID, sandboxTenantID string) error {
	return fmt.Errorf("refused: no client's data is copied into a sandbox (%q into %q); seed it with synthetic data instead (tenantctl sandbox-create / sandbox-reset)", sourceTenantID, sandboxTenantID)
}
