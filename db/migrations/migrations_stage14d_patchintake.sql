-- Stage 14.13-14.16: Patch/Bug-Intake Automation
--
-- Design note (deviation from the original plan sketch, stated explicitly):
-- the worker NEVER mutates any tenant/business state, in any environment,
-- regardless of classification. "auto_safe" means "this error signature is
-- known noise (expected behavior, not a bug) - dismiss it without bothering
-- a human," not "automatically change configuration." This is a stricter,
-- safer reading of the user-confirmed policy ("never auto-deploy actual
-- code changes to live") - it makes the "worker can never mutate a live
-- environment" guarantee true by construction (the worker only ever writes
-- to these two audit tables), rather than needing a live-vs-dev/test branch
-- to enforce it. Actually applying any real fix (a module-entitlement
-- toggle, a code change + promotion) stays a deliberate, separate action a
-- human takes using the tools already built in Phases A/C - this system is
-- a triage queue and decision audit trail, not an auto-executor. It has no
-- automated fix-generation capability, and doesn't pretend to.

CREATE TABLE IF NOT EXISTS public.patch_policy_rules (
    id SERIAL PRIMARY KEY,
    error_pattern TEXT NOT NULL,   -- Go regexp, matched against system_error_logs.error_message
    classification VARCHAR(20) NOT NULL CHECK (classification IN ('auto_safe', 'requires_approval')),
    description TEXT,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
);

-- Deliberately minimal seed: exactly one auto_safe rule (expected,
-- self-resolving rate-limit noise), so the "auto" path is demonstrated with
-- something genuinely risk-free rather than left entirely unseeded.
-- Everything else falls through to the fail-closed 'requires_approval'
-- default (same posture as IsFeatureEnabled/IsModuleEnabled elsewhere in
-- this codebase - unmatched means "don't assume it's safe").
INSERT INTO public.patch_policy_rules (error_pattern, classification, description) VALUES
('^Rate limit exceeded', 'auto_safe', 'Expected behavior under load, not a bug - dismissed automatically to reduce noise.')
ON CONFLICT DO NOTHING;

CREATE TABLE IF NOT EXISTS public.patch_proposals (
    id SERIAL PRIMARY KEY,
    tenant_id VARCHAR(100) NOT NULL,
    module_source VARCHAR(100) NOT NULL,
    signature TEXT NOT NULL,          -- normalized error_message, the grouping key
    error_sample TEXT NOT NULL,       -- one representative full error_message
    occurrence_count INT NOT NULL DEFAULT 1,
    classification VARCHAR(20) NOT NULL,
    status VARCHAR(20) NOT NULL DEFAULT 'pending' CHECK (status IN ('pending', 'approved', 'rejected', 'dismissed')),
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    decided_by VARCHAR(100),
    decided_at TIMESTAMP,
    notes TEXT
);
CREATE INDEX IF NOT EXISTS idx_patch_proposals_status ON public.patch_proposals (status, created_at DESC);

-- Single-row table tracking the worker's last successful scan, so each tick
-- only looks at genuinely new system_error_logs rows rather than
-- reprocessing the same window every cycle.
CREATE TABLE IF NOT EXISTS public.patch_intake_state (
    id INT PRIMARY KEY DEFAULT 1,
    last_run_at TIMESTAMP,
    CONSTRAINT single_row CHECK (id = 1)
);
INSERT INTO public.patch_intake_state (id, last_run_at) VALUES (1, NULL) ON CONFLICT (id) DO NOTHING;
