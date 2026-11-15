-- 50.12 / BLD-024..027 (ask 193, ADR-009): the platform's own SaaS billing.
-- Control-plane tables in public (like public.tenant_lifecycle_events): they
-- describe the platform's relation with a client, not the client's books.
-- Additive and safe to run twice.

-- What each sellable plan is: the packages it grants (BLD-023 contract), its
-- monthly fee in paise (INR), the Razorpay plan it maps to, trial days, and
-- included quota plus overage rate per meter.
CREATE TABLE IF NOT EXISTS public.saas_plans (
    plan_key         VARCHAR(60) PRIMARY KEY,
    display_name     VARCHAR(200) NOT NULL,
    package_keys     JSONB NOT NULL DEFAULT '[]',
    amount_paise     BIGINT NOT NULL CHECK (amount_paise >= 0),
    period           VARCHAR(20) NOT NULL DEFAULT 'monthly',
    trial_days       INT NOT NULL DEFAULT 0,
    quotas           JSONB NOT NULL DEFAULT '{}',
    overage_paise    JSONB NOT NULL DEFAULT '{}',
    razorpay_plan_id VARCHAR(60),
    active           BOOLEAN NOT NULL DEFAULT TRUE,
    created_at       TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- One subscription per client. state: pending, active, past_due (grace),
-- suspended, cancelled (access until access_until), expired.
CREATE TABLE IF NOT EXISTS public.saas_subscriptions (
    tenant_id                VARCHAR(100) PRIMARY KEY,
    plan_key                 VARCHAR(60) NOT NULL,
    state                    VARCHAR(20) NOT NULL DEFAULT 'pending',
    razorpay_subscription_id VARCHAR(60) UNIQUE,
    short_url                TEXT NOT NULL DEFAULT '',
    period_start             TIMESTAMPTZ,
    period_end               TIMESTAMPTZ,
    grace_until              TIMESTAMPTZ,
    access_until             TIMESTAMPTZ,
    billing_suspended        BOOLEAN NOT NULL DEFAULT FALSE,
    last_event_at            BIGINT NOT NULL DEFAULT 0,
    explanation              TEXT NOT NULL DEFAULT '',
    created_at               TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at               TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- Every provider event seen, once. The unique provider event id is what stops
-- a replay doing anything twice.
CREATE TABLE IF NOT EXISTS public.saas_subscription_events (
    event_id   VARCHAR(120) PRIMARY KEY,
    tenant_id  VARCHAR(100) NOT NULL DEFAULT '',
    event      VARCHAR(60) NOT NULL,
    event_at   BIGINT NOT NULL DEFAULT 0,
    outcome    VARCHAR(20) NOT NULL,
    detail     TEXT NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- The platform's own GST invoice for each captured payment. One per payment.
CREATE TABLE IF NOT EXISTS public.saas_invoices (
    invoice_no           VARCHAR(40) PRIMARY KEY,
    tenant_id            VARCHAR(100) NOT NULL,
    razorpay_payment_id  VARCHAR(60) NOT NULL UNIQUE,
    period_start         TIMESTAMPTZ,
    period_end           TIMESTAMPTZ,
    gross_paise          BIGINT NOT NULL,
    taxable_paise        BIGINT NOT NULL,
    gst_paise            BIGINT NOT NULL,
    gst_rate             NUMERIC(5,2) NOT NULL,
    created_at           TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE TABLE IF NOT EXISTS public.saas_invoice_counters (
    fiscal_year VARCHAR(9) PRIMARY KEY,
    last_no     BIGINT NOT NULL DEFAULT 0
);

-- Billable usage. period_key is the Indian-time calendar month. A repeated
-- (tenant, meter, dedupe_key) is the same fact, never a second one.
CREATE TABLE IF NOT EXISTS public.saas_usage (
    id          BIGSERIAL PRIMARY KEY,
    tenant_id   VARCHAR(100) NOT NULL,
    meter       VARCHAR(40) NOT NULL,
    period_key  CHAR(7) NOT NULL,
    dedupe_key  VARCHAR(160) NOT NULL,
    qty         BIGINT NOT NULL,
    occurred_at TIMESTAMPTZ NOT NULL,
    note        TEXT NOT NULL DEFAULT '',
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (tenant_id, meter, dedupe_key)
);
CREATE INDEX IF NOT EXISTS idx_saas_usage_period ON public.saas_usage (tenant_id, period_key, meter);

-- Who changed a client's packages, from what to what, and how it ended.
CREATE TABLE IF NOT EXISTS public.saas_package_transitions (
    id            BIGSERIAL PRIMARY KEY,
    tenant_id     VARCHAR(100) NOT NULL,
    request_id    VARCHAR(100) NOT NULL,
    actor         VARCHAR(100) NOT NULL,
    from_modules  JSONB NOT NULL DEFAULT '[]',
    to_modules    JSONB NOT NULL DEFAULT '[]',
    packages      JSONB NOT NULL DEFAULT '[]',
    status        VARCHAR(20) NOT NULL,
    detail        TEXT NOT NULL DEFAULT '',
    created_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (tenant_id, request_id)
);
