-- ---------------------------------------------------------------------------
-- Stage 101 - the CRM platform (user, 2026-10-19 "CRM Moule better/best one";
-- 2026-10-22 "Build all"). Plan: docs/product/crm-platform-plan-2026-10-19.md.
--
-- Every table below lives in each client schema and is new, so nothing a
-- client already has changes meaning:
--   101.1  crm_customer_notes                    notes on the customer page
--   101.8  crm_consent_ledger                    append-only consent evidence
--          crm_suppressions                      hashed phone / email blocks
--          crm_pref_tokens                       preference-page links
--   101.2  crm_segments, _versions, _snapshots,  segment builder, frozen
--          _snapshot_members                     versions and who was in one
--          crm_static_lists, _members            uploaded phone lists
--          crm_lifecycle_versions                lifecycle thresholds
--   101.3  crm_templates, crm_template_versions  versioned message templates
--          crm_senders                           email / SMS / WhatsApp senders
--          crm_campaign_runs                     one run of a campaign
--   101.9  crm_sends                             one row per message decision
--   101.4  crm_journeys, _versions, _runs        automatic journeys
--          crm_events                            what journeys react to
--   101.5  crm_surveys, _versions, crm_feedback  feedback after a bill
--          crm_ticket_notes                      private ticket notes
--          crm_conversations, _messages          the WhatsApp inbox
--   101.11 crm_voucher_reservations              reserve, then capture a code
--          crm_referral_codes, crm_referrals     refer a friend
--          loyalty_point_ledger.pending_until    points pending a return window
--   101.12 crm_tasks, crm_store_slots,           store worklist, appointments,
--          crm_appointments, crm_wishlist,       wish lists and hold requests
--          crm_hold_requests
--   101.6  crm_pipeline_stages, crm_leads,       the business-buyer pipeline
--          crm_opportunities, _history,
--          crm_forecast_snapshots, crm_lead_rules
--
-- Record types: Customer gains sizes, preferred store and salesman, tags and
-- an email consent tick; ServiceTicket gains bill, store, channel and the SLA
-- clock; Campaign's status and channel lists widen; two new record types,
-- CustomerMerge (a merge waiting for approval) and SalesQuotation (101.6.2:
-- a won deal needs an approved quotation). One approval rule is added only
-- where the client has none for Campaign, CustomerMerge and SalesQuotation
-- (Store Manager approves).
--
-- Additive and idempotent, replayed into every client schema.
-- ---------------------------------------------------------------------------
DO $mig$
DECLARE
  s RECORD;
BEGIN
  FOR s IN
    SELECT schema_name FROM information_schema.schemata WHERE schema_name LIKE 'tenant\_%' ESCAPE '\'
  LOOP
    CONTINUE WHEN to_regclass(format('%I.documents', s.schema_name)) IS NULL;

    -- 101.1.2 notes ------------------------------------------------------
    EXECUTE format($f$
      CREATE TABLE IF NOT EXISTS %I.crm_customer_notes (
        id          BIGSERIAL PRIMARY KEY,
        customer_id VARCHAR(100) NOT NULL,
        note        TEXT         NOT NULL,
        created_by  VARCHAR(100),
        created_at  TIMESTAMP    NOT NULL DEFAULT NOW()
      )$f$, s.schema_name);
    EXECUTE format('CREATE INDEX IF NOT EXISTS crm_customer_notes_customer_idx ON %I.crm_customer_notes (customer_id, created_at DESC)', s.schema_name);

    -- 101.8 consent, suppression, preference links -----------------------
    EXECUTE format($f$
      CREATE TABLE IF NOT EXISTS %I.crm_consent_ledger (
        id             BIGSERIAL PRIMARY KEY,
        customer_id    VARCHAR(100) NOT NULL,
        channel        VARCHAR(20)  NOT NULL,
        purpose        VARCHAR(20)  NOT NULL,
        action         VARCHAR(20)  NOT NULL,
        source         VARCHAR(60)  NOT NULL,
        recorded_by    VARCHAR(100),
        evidence       TEXT,
        notice_version VARCHAR(40),
        created_at     TIMESTAMP    NOT NULL DEFAULT NOW()
      )$f$, s.schema_name);
    EXECUTE format('CREATE INDEX IF NOT EXISTS crm_consent_ledger_customer_idx ON %I.crm_consent_ledger (customer_id, channel, purpose, id DESC)', s.schema_name);
    -- Today's two ticks (55.17a) carried in once as legacy evidence, so the
    -- ledger is complete from the start. A customer who already has a row
    -- for that channel is left alone, so a replay adds nothing.
    EXECUTE format($f$
      INSERT INTO %I.crm_consent_ledger (customer_id, channel, purpose, action, source, recorded_by, evidence, created_at)
      SELECT d.id, c.channel, 'marketing', 'granted', 'legacy', NULLIF(d.data->>'consent_source', ''),
             'Carried in from the customer record''s tick (Stage 55.17a)',
             COALESCE(CASE WHEN d.data->>'consent_updated_at' ~ '^\d{4}-\d{2}-\d{2}( \d{2}:\d{2}(:\d{2})?)?$'
                           THEN (d.data->>'consent_updated_at')::timestamp END, d.updated_at, NOW())
        FROM %I.documents d
        CROSS JOIN (VALUES ('SMS', 'consent_sms'), ('WhatsApp', 'consent_whatsapp')) AS c(channel, field)
       WHERE d.doctype = 'Customer' AND d.deleted_at IS NULL
         AND LOWER(COALESCE(d.data->>c.field, '')) IN ('true', '1', 'yes', 'on')
         AND NOT EXISTS (SELECT 1 FROM %I.crm_consent_ledger l WHERE l.customer_id = d.id AND l.channel = c.channel)
    $f$, s.schema_name, s.schema_name, s.schema_name);

    EXECUTE format($f$
      CREATE TABLE IF NOT EXISTS %I.crm_suppressions (
        id         BIGSERIAL PRIMARY KEY,
        kind       VARCHAR(10) NOT NULL,
        value_hash VARCHAR(64) NOT NULL,
        reason     VARCHAR(30) NOT NULL,
        created_at TIMESTAMP   NOT NULL DEFAULT NOW(),
        UNIQUE (kind, value_hash, reason)
      )$f$, s.schema_name);

    EXECUTE format($f$
      CREATE TABLE IF NOT EXISTS %I.crm_pref_tokens (
        token_hash  VARCHAR(64)  PRIMARY KEY,
        customer_id VARCHAR(100) NOT NULL,
        send_id     VARCHAR(80),
        created_at  TIMESTAMP    NOT NULL DEFAULT NOW(),
        used_at     TIMESTAMP
      )$f$, s.schema_name);
    EXECUTE format('CREATE INDEX IF NOT EXISTS crm_pref_tokens_customer_idx ON %I.crm_pref_tokens (customer_id)', s.schema_name);

    -- 101.2 segments, lists, lifecycle -------------------------------------
    EXECUTE format($f$
      CREATE TABLE IF NOT EXISTS %I.crm_segments (
        id              VARCHAR(80)  PRIMARY KEY,
        name            VARCHAR(200) NOT NULL,
        description     TEXT,
        current_version INT          NOT NULL DEFAULT 1,
        status          VARCHAR(20)  NOT NULL DEFAULT 'Active',
        created_by      VARCHAR(100),
        created_at      TIMESTAMP    NOT NULL DEFAULT NOW(),
        updated_at      TIMESTAMP    NOT NULL DEFAULT NOW()
      )$f$, s.schema_name);
    EXECUTE format($f$
      CREATE TABLE IF NOT EXISTS %I.crm_segment_versions (
        segment_id VARCHAR(80) NOT NULL,
        version    INT         NOT NULL,
        definition JSONB       NOT NULL,
        created_by VARCHAR(100),
        created_at TIMESTAMP   NOT NULL DEFAULT NOW(),
        PRIMARY KEY (segment_id, version)
      )$f$, s.schema_name);
    EXECUTE format($f$
      CREATE TABLE IF NOT EXISTS %I.crm_segment_snapshots (
        id           VARCHAR(80) PRIMARY KEY,
        segment_id   VARCHAR(80) NOT NULL,
        version      INT         NOT NULL,
        purpose      VARCHAR(40) NOT NULL,
        ref_id       VARCHAR(100),
        member_count INT         NOT NULL DEFAULT 0,
        created_at   TIMESTAMP   NOT NULL DEFAULT NOW()
      )$f$, s.schema_name);
    EXECUTE format($f$
      CREATE TABLE IF NOT EXISTS %I.crm_segment_snapshot_members (
        snapshot_id VARCHAR(80)  NOT NULL,
        customer_id VARCHAR(100) NOT NULL,
        PRIMARY KEY (snapshot_id, customer_id)
      )$f$, s.schema_name);
    EXECUTE format($f$
      CREATE TABLE IF NOT EXISTS %I.crm_static_lists (
        id            VARCHAR(80)  PRIMARY KEY,
        name          VARCHAR(200) NOT NULL,
        source        TEXT         NOT NULL,
        expires_on    DATE,
        row_count     INT          NOT NULL DEFAULT 0,
        matched_count INT          NOT NULL DEFAULT 0,
        created_by    VARCHAR(100),
        created_at    TIMESTAMP    NOT NULL DEFAULT NOW()
      )$f$, s.schema_name);
    EXECUTE format($f$
      CREATE TABLE IF NOT EXISTS %I.crm_static_list_members (
        list_id     VARCHAR(80) NOT NULL,
        phone       VARCHAR(30) NOT NULL,
        customer_id VARCHAR(100),
        PRIMARY KEY (list_id, phone)
      )$f$, s.schema_name);
    EXECUTE format('CREATE INDEX IF NOT EXISTS crm_static_list_members_customer_idx ON %I.crm_static_list_members (customer_id)', s.schema_name);
    EXECUTE format($f$
      CREATE TABLE IF NOT EXISTS %I.crm_lifecycle_versions (
        version      SERIAL PRIMARY KEY,
        new_days     INT       NOT NULL,
        active_days  INT       NOT NULL,
        at_risk_days INT       NOT NULL,
        created_by   VARCHAR(100),
        created_at   TIMESTAMP NOT NULL DEFAULT NOW()
      )$f$, s.schema_name);

    -- 101.3 templates, senders, campaign runs ------------------------------
    EXECUTE format($f$
      CREATE TABLE IF NOT EXISTS %I.crm_templates (
        id              VARCHAR(80)  PRIMARY KEY,
        name            VARCHAR(200) NOT NULL,
        channel         VARCHAR(20)  NOT NULL,
        language        VARCHAR(10)  NOT NULL DEFAULT 'en',
        purpose         VARCHAR(20)  NOT NULL DEFAULT 'marketing',
        current_version INT          NOT NULL DEFAULT 0,
        status          VARCHAR(20)  NOT NULL DEFAULT 'Active',
        created_by      VARCHAR(100),
        created_at      TIMESTAMP    NOT NULL DEFAULT NOW(),
        updated_at      TIMESTAMP    NOT NULL DEFAULT NOW()
      )$f$, s.schema_name);
    EXECUTE format($f$
      CREATE TABLE IF NOT EXISTS %I.crm_template_versions (
        template_id      VARCHAR(80) NOT NULL,
        version          INT         NOT NULL,
        subject          TEXT,
        body             TEXT        NOT NULL,
        variables        JSONB       NOT NULL DEFAULT '[]'::jsonb,
        dlt_template_id  VARCHAR(40),
        wa_template_name VARCHAR(200),
        wa_language      VARCHAR(20),
        wa_status        VARCHAR(20),
        wa_status_at     TIMESTAMP,
        created_by       VARCHAR(100),
        created_at       TIMESTAMP   NOT NULL DEFAULT NOW(),
        PRIMARY KEY (template_id, version)
      )$f$, s.schema_name);
    EXECUTE format($f$
      CREATE TABLE IF NOT EXISTS %I.crm_senders (
        id           VARCHAR(80)  PRIMARY KEY,
        channel      VARCHAR(20)  NOT NULL,
        provider     VARCHAR(30)  NOT NULL,
        name         VARCHAR(200) NOT NULL,
        config       JSONB        NOT NULL DEFAULT '{}'::jsonb,
        secret       BYTEA,
        callback_key VARCHAR(64),
        status       VARCHAR(20)  NOT NULL DEFAULT 'Active',
        created_by   VARCHAR(100),
        created_at   TIMESTAMP    NOT NULL DEFAULT NOW(),
        updated_at   TIMESTAMP    NOT NULL DEFAULT NOW()
      )$f$, s.schema_name);
    EXECUTE format($f$
      CREATE TABLE IF NOT EXISTS %I.crm_campaign_runs (
        id               VARCHAR(80)  PRIMARY KEY,
        campaign_id      VARCHAR(100) NOT NULL,
        campaign_hash    VARCHAR(64),
        snapshot_id      VARCHAR(80),
        channel          VARCHAR(20)  NOT NULL,
        template_id      VARCHAR(80),
        template_version INT,
        status           VARCHAR(20)  NOT NULL DEFAULT 'Scheduled',
        test             BOOLEAN      NOT NULL DEFAULT FALSE,
        scheduled_at     TIMESTAMP    NOT NULL DEFAULT NOW(),
        ends_at          TIMESTAMP,
        budget           NUMERIC(14,2),
        holdout_pct      INT          NOT NULL DEFAULT 0,
        goal_days        INT          NOT NULL DEFAULT 7,
        voucher          JSONB,
        options          JSONB        NOT NULL DEFAULT '{}'::jsonb,
        counts           JSONB        NOT NULL DEFAULT '{}'::jsonb,
        created_by       VARCHAR(100),
        created_at       TIMESTAMP    NOT NULL DEFAULT NOW(),
        started_at       TIMESTAMP,
        finished_at      TIMESTAMP
      )$f$, s.schema_name);
    -- a schema that ran an earlier draft of this file has the table without these
    EXECUTE format('ALTER TABLE %I.crm_campaign_runs ADD COLUMN IF NOT EXISTS options JSONB NOT NULL DEFAULT ''{}''::jsonb', s.schema_name);
    EXECUTE format('ALTER TABLE %I.crm_campaign_runs ADD COLUMN IF NOT EXISTS counts JSONB NOT NULL DEFAULT ''{}''::jsonb', s.schema_name);
    EXECUTE format('CREATE INDEX IF NOT EXISTS crm_campaign_runs_campaign_idx ON %I.crm_campaign_runs (campaign_id, created_at DESC)', s.schema_name);

    -- 101.9 the send decisions ---------------------------------------------
    EXECUTE format($f$
      CREATE TABLE IF NOT EXISTS %I.crm_sends (
        id               VARCHAR(80)  PRIMARY KEY,
        run_kind         VARCHAR(20)  NOT NULL,
        run_id           VARCHAR(120) NOT NULL,
        customer_id      VARCHAR(100) NOT NULL,
        channel          VARCHAR(20)  NOT NULL,
        purpose          VARCHAR(20)  NOT NULL DEFAULT 'marketing',
        template_id      VARCHAR(80),
        template_version INT,
        body_override    TEXT,
        vars             JSONB        NOT NULL DEFAULT '{}'::jsonb,
        status           VARCHAR(20)  NOT NULL DEFAULT 'Queued',
        reason_code      VARCHAR(40),
        reason_detail    TEXT,
        due_at           TIMESTAMP    NOT NULL DEFAULT NOW(),
        expires_at       TIMESTAMP,
        contact_hash     VARCHAR(64),
        contact_masked   VARCHAR(80),
        sender_id        VARCHAR(80),
        provider         VARCHAR(30),
        provider_ref     VARCHAR(200),
        parts            INT,
        cost_estimate    NUMERIC(12,4),
        cost_actual      NUMERIC(12,4),
        rendered_subject TEXT,
        rendered_body    TEXT,
        attempts         INT          NOT NULL DEFAULT 0,
        history          JSONB        NOT NULL DEFAULT '[]'::jsonb,
        created_by       VARCHAR(100),
        created_at       TIMESTAMP    NOT NULL DEFAULT NOW(),
        decided_at       TIMESTAMP,
        sent_at          TIMESTAMP,
        final_at         TIMESTAMP,
        UNIQUE (run_kind, run_id, customer_id, channel)
      )$f$, s.schema_name);
    EXECUTE format($f$CREATE INDEX IF NOT EXISTS crm_sends_due_idx ON %I.crm_sends (due_at) WHERE status IN ('Queued', 'Deferred')$f$, s.schema_name);
    EXECUTE format('CREATE INDEX IF NOT EXISTS crm_sends_customer_idx ON %I.crm_sends (customer_id, sent_at DESC)', s.schema_name);
    EXECUTE format('CREATE INDEX IF NOT EXISTS crm_sends_run_idx ON %I.crm_sends (run_kind, run_id)', s.schema_name);
    EXECUTE format('CREATE INDEX IF NOT EXISTS crm_sends_provider_ref_idx ON %I.crm_sends (provider_ref)', s.schema_name);

    -- 101.4 journeys and the events they react to --------------------------
    EXECUTE format($f$
      CREATE TABLE IF NOT EXISTS %I.crm_journeys (
        id              VARCHAR(80)  PRIMARY KEY,
        name            VARCHAR(200) NOT NULL,
        description     TEXT,
        ready_made      VARCHAR(40),
        enabled         BOOLEAN      NOT NULL DEFAULT FALSE,
        current_version INT          NOT NULL DEFAULT 0,
        draft           JSONB,
        created_by      VARCHAR(100),
        created_at      TIMESTAMP    NOT NULL DEFAULT NOW(),
        updated_at      TIMESTAMP    NOT NULL DEFAULT NOW()
      )$f$, s.schema_name);
    EXECUTE format($f$
      CREATE TABLE IF NOT EXISTS %I.crm_journey_versions (
        journey_id   VARCHAR(80) NOT NULL,
        version      INT         NOT NULL,
        definition   JSONB       NOT NULL,
        published_by VARCHAR(100),
        published_at TIMESTAMP   NOT NULL DEFAULT NOW(),
        PRIMARY KEY (journey_id, version)
      )$f$, s.schema_name);
    EXECUTE format($f$
      CREATE TABLE IF NOT EXISTS %I.crm_journey_runs (
        id          VARCHAR(80)  PRIMARY KEY,
        journey_id  VARCHAR(80)  NOT NULL,
        version     INT          NOT NULL,
        customer_id VARCHAR(100) NOT NULL,
        cart_key    VARCHAR(120) NOT NULL DEFAULT '',
        state       VARCHAR(20)  NOT NULL DEFAULT 'Active',
        step        INT          NOT NULL DEFAULT 0,
        wait_until  TIMESTAMP,
        wait_event  VARCHAR(60),
        trigger_ref VARCHAR(120),
        context     JSONB        NOT NULL DEFAULT '{}'::jsonb,
        history     JSONB        NOT NULL DEFAULT '[]'::jsonb,
        exit_reason VARCHAR(60),
        started_at  TIMESTAMP    NOT NULL DEFAULT NOW(),
        updated_at  TIMESTAMP    NOT NULL DEFAULT NOW(),
        ended_at    TIMESTAMP
      )$f$, s.schema_name);
    EXECUTE format($f$CREATE UNIQUE INDEX IF NOT EXISTS crm_journey_runs_live_uq ON %I.crm_journey_runs (journey_id, customer_id, cart_key) WHERE state IN ('Active', 'Waiting')$f$, s.schema_name);
    EXECUTE format('CREATE INDEX IF NOT EXISTS crm_journey_runs_due_idx ON %I.crm_journey_runs (state, wait_until)', s.schema_name);
    EXECUTE format('CREATE INDEX IF NOT EXISTS crm_journey_runs_customer_idx ON %I.crm_journey_runs (customer_id, journey_id, ended_at DESC)', s.schema_name);
    EXECUTE format($f$
      CREATE TABLE IF NOT EXISTS %I.crm_events (
        id           BIGSERIAL PRIMARY KEY,
        customer_id  VARCHAR(100) NOT NULL,
        event_type   VARCHAR(60)  NOT NULL,
        ref_id       VARCHAR(120),
        payload      JSONB        NOT NULL DEFAULT '{}'::jsonb,
        dedupe_key   VARCHAR(200) NOT NULL UNIQUE,
        occurred_at  TIMESTAMP    NOT NULL DEFAULT NOW(),
        processed_at TIMESTAMP
      )$f$, s.schema_name);
    EXECUTE format('CREATE INDEX IF NOT EXISTS crm_events_pending_idx ON %I.crm_events (id) WHERE processed_at IS NULL', s.schema_name);
    EXECUTE format('CREATE INDEX IF NOT EXISTS crm_events_customer_idx ON %I.crm_events (customer_id, event_type, occurred_at DESC)', s.schema_name);

    -- 101.5 feedback, ticket notes, the WhatsApp inbox ---------------------
    EXECUTE format($f$
      CREATE TABLE IF NOT EXISTS %I.crm_surveys (
        id              VARCHAR(80)  PRIMARY KEY,
        name            VARCHAR(200) NOT NULL,
        current_version INT          NOT NULL DEFAULT 1,
        channel         VARCHAR(20)  NOT NULL DEFAULT 'WhatsApp',
        template_id     VARCHAR(80),
        enabled         BOOLEAN      NOT NULL DEFAULT FALSE,
        delay_hours     INT          NOT NULL DEFAULT 2,
        cooldown_days   INT          NOT NULL DEFAULT 30,
        low_score       INT          NOT NULL DEFAULT 6,
        created_by      VARCHAR(100),
        created_at      TIMESTAMP    NOT NULL DEFAULT NOW(),
        updated_at      TIMESTAMP    NOT NULL DEFAULT NOW()
      )$f$, s.schema_name);
    EXECUTE format($f$
      CREATE TABLE IF NOT EXISTS %I.crm_survey_versions (
        survey_id  VARCHAR(80) NOT NULL,
        version    INT         NOT NULL,
        question   TEXT        NOT NULL,
        scale      VARCHAR(10) NOT NULL DEFAULT 'nps',
        created_by VARCHAR(100),
        created_at TIMESTAMP   NOT NULL DEFAULT NOW(),
        PRIMARY KEY (survey_id, version)
      )$f$, s.schema_name);
    EXECUTE format($f$
      CREATE TABLE IF NOT EXISTS %I.crm_feedback (
        id             VARCHAR(80)  PRIMARY KEY,
        token_hash     VARCHAR(64)  NOT NULL UNIQUE,
        survey_id      VARCHAR(80)  NOT NULL,
        survey_version INT          NOT NULL,
        customer_id    VARCHAR(100) NOT NULL,
        bill_id        VARCHAR(100) NOT NULL UNIQUE,
        store          VARCHAR(100),
        send_id        VARCHAR(80),
        score          INT,
        comment        TEXT,
        ticket_id      VARCHAR(100),
        created_at     TIMESTAMP    NOT NULL DEFAULT NOW(),
        responded_at   TIMESTAMP
      )$f$, s.schema_name);
    EXECUTE format('CREATE INDEX IF NOT EXISTS crm_feedback_customer_idx ON %I.crm_feedback (customer_id, created_at DESC)', s.schema_name);
    EXECUTE format($f$
      CREATE TABLE IF NOT EXISTS %I.crm_ticket_notes (
        id         BIGSERIAL PRIMARY KEY,
        ticket_id  VARCHAR(100) NOT NULL,
        note       TEXT         NOT NULL,
        private    BOOLEAN      NOT NULL DEFAULT TRUE,
        created_by VARCHAR(100),
        created_at TIMESTAMP    NOT NULL DEFAULT NOW()
      )$f$, s.schema_name);
    EXECUTE format('CREATE INDEX IF NOT EXISTS crm_ticket_notes_ticket_idx ON %I.crm_ticket_notes (ticket_id, created_at)', s.schema_name);
    EXECUTE format($f$
      CREATE TABLE IF NOT EXISTS %I.crm_conversations (
        id               VARCHAR(80)  PRIMARY KEY,
        channel          VARCHAR(20)  NOT NULL DEFAULT 'WhatsApp',
        phone_hash       VARCHAR(64)  NOT NULL,
        phone            VARCHAR(30),
        customer_id      VARCHAR(100),
        mode             VARCHAR(10)  NOT NULL DEFAULT 'auto',
        assigned_to      VARCHAR(100),
        ticket_id        VARCHAR(100),
        status           VARCHAR(20)  NOT NULL DEFAULT 'Open',
        unread           INT          NOT NULL DEFAULT 0,
        last_inbound_at  TIMESTAMP,
        last_outbound_at TIMESTAMP,
        created_at       TIMESTAMP    NOT NULL DEFAULT NOW(),
        updated_at       TIMESTAMP    NOT NULL DEFAULT NOW(),
        UNIQUE (channel, phone_hash)
      )$f$, s.schema_name);
    EXECUTE format($f$
      CREATE TABLE IF NOT EXISTS %I.crm_conversation_messages (
        id              BIGSERIAL PRIMARY KEY,
        conversation_id VARCHAR(80)  NOT NULL,
        direction       VARCHAR(3)   NOT NULL,
        body            TEXT,
        provider_ref    VARCHAR(200),
        send_id         VARCHAR(80),
        sent_by         VARCHAR(100),
        created_at      TIMESTAMP    NOT NULL DEFAULT NOW()
      )$f$, s.schema_name);
    EXECUTE format('CREATE INDEX IF NOT EXISTS crm_conversation_messages_conv_idx ON %I.crm_conversation_messages (conversation_id, id)', s.schema_name);
    EXECUTE format($f$CREATE UNIQUE INDEX IF NOT EXISTS crm_conversation_messages_ref_uq ON %I.crm_conversation_messages (provider_ref) WHERE provider_ref IS NOT NULL AND direction = 'in'$f$, s.schema_name);

    -- 101.11 codes, referrals, pending points --------------------------------
    EXECUTE format($f$
      CREATE TABLE IF NOT EXISTS %I.crm_voucher_reservations (
        id          VARCHAR(80)  PRIMARY KEY,
        code        VARCHAR(100) NOT NULL,
        channel     VARCHAR(20)  NOT NULL,
        ref_id      VARCHAR(120) NOT NULL,
        customer_id VARCHAR(100),
        status      VARCHAR(20)  NOT NULL DEFAULT 'Reserved',
        expires_at  TIMESTAMP    NOT NULL,
        created_at  TIMESTAMP    NOT NULL DEFAULT NOW(),
        closed_at   TIMESTAMP,
        UNIQUE (code, ref_id)
      )$f$, s.schema_name);
    EXECUTE format('CREATE INDEX IF NOT EXISTS crm_voucher_reservations_code_idx ON %I.crm_voucher_reservations (code, status)', s.schema_name);
    EXECUTE format($f$
      CREATE TABLE IF NOT EXISTS %I.crm_referral_codes (
        code        VARCHAR(20)  PRIMARY KEY,
        customer_id VARCHAR(100) NOT NULL UNIQUE,
        created_at  TIMESTAMP    NOT NULL DEFAULT NOW()
      )$f$, s.schema_name);
    EXECUTE format($f$
      CREATE TABLE IF NOT EXISTS %I.crm_referrals (
        id            VARCHAR(80)  PRIMARY KEY,
        code          VARCHAR(20)  NOT NULL,
        referrer_id   VARCHAR(100) NOT NULL,
        friend_id     VARCHAR(100) NOT NULL UNIQUE,
        status        VARCHAR(20)  NOT NULL DEFAULT 'Waiting',
        first_bill_id VARCHAR(100),
        qualifies_on  DATE,
        reward_points INT          NOT NULL DEFAULT 0,
        flag_reason   TEXT,
        created_by    VARCHAR(100),
        created_at    TIMESTAMP    NOT NULL DEFAULT NOW(),
        released_at   TIMESTAMP
      )$f$, s.schema_name);
    IF to_regclass(format('%I.loyalty_point_ledger', s.schema_name)) IS NOT NULL THEN
      EXECUTE format('ALTER TABLE %I.loyalty_point_ledger ADD COLUMN IF NOT EXISTS pending_until TIMESTAMP', s.schema_name);
      EXECUTE format('ALTER TABLE %I.loyalty_point_ledger ADD COLUMN IF NOT EXISTS cancelled_at TIMESTAMP', s.schema_name);
    END IF;

    -- 101.12 worklist, appointments, wish lists, holds ----------------------
    EXECUTE format($f$
      CREATE TABLE IF NOT EXISTS %I.crm_tasks (
        id             VARCHAR(80)  PRIMARY KEY,
        kind           VARCHAR(30)  NOT NULL,
        title          VARCHAR(300) NOT NULL,
        customer_id    VARCHAR(100),
        lead_id        VARCHAR(80),
        opportunity_id VARCHAR(80),
        store          VARCHAR(100),
        owner          VARCHAR(100),
        due_at         TIMESTAMP,
        status         VARCHAR(20)  NOT NULL DEFAULT 'Open',
        outcome        TEXT,
        ref_doctype    VARCHAR(100),
        ref_id         VARCHAR(120),
        created_by     VARCHAR(100),
        created_at     TIMESTAMP    NOT NULL DEFAULT NOW(),
        done_at        TIMESTAMP,
        done_by        VARCHAR(100)
      )$f$, s.schema_name);
    EXECUTE format('CREATE INDEX IF NOT EXISTS crm_tasks_owner_idx ON %I.crm_tasks (owner, status, due_at)', s.schema_name);
    EXECUTE format('CREATE INDEX IF NOT EXISTS crm_tasks_store_idx ON %I.crm_tasks (store, status, due_at)', s.schema_name);
    EXECUTE format('CREATE INDEX IF NOT EXISTS crm_tasks_customer_idx ON %I.crm_tasks (customer_id)', s.schema_name);
    EXECUTE format($f$
      CREATE TABLE IF NOT EXISTS %I.crm_store_slots (
        store        VARCHAR(100) PRIMARY KEY,
        slot_minutes INT          NOT NULL DEFAULT 30,
        capacity     INT          NOT NULL DEFAULT 1,
        opens        VARCHAR(5)   NOT NULL DEFAULT '10:00',
        closes       VARCHAR(5)   NOT NULL DEFAULT '20:00',
        days         VARCHAR(20)  NOT NULL DEFAULT '1234567',
        updated_by   VARCHAR(100),
        updated_at   TIMESTAMP    NOT NULL DEFAULT NOW()
      )$f$, s.schema_name);
    EXECUTE format('ALTER TABLE %I.crm_store_slots ADD COLUMN IF NOT EXISTS reminder_channel VARCHAR(20) NOT NULL DEFAULT ''WhatsApp''', s.schema_name);
    EXECUTE format('ALTER TABLE %I.crm_store_slots ADD COLUMN IF NOT EXISTS reminder_template_id VARCHAR(80)', s.schema_name);
    EXECUTE format($f$
      CREATE TABLE IF NOT EXISTS %I.crm_appointments (
        id            VARCHAR(80)  PRIMARY KEY,
        customer_id   VARCHAR(100) NOT NULL,
        store         VARCHAR(100) NOT NULL,
        kind          VARCHAR(20)  NOT NULL DEFAULT 'Visit',
        slot_start    TIMESTAMP    NOT NULL,
        slot_minutes  INT          NOT NULL DEFAULT 30,
        status        VARCHAR(20)  NOT NULL DEFAULT 'Booked',
        note          TEXT,
        staff         VARCHAR(100),
        created_by    VARCHAR(100),
        created_at    TIMESTAMP    NOT NULL DEFAULT NOW(),
        cancelled_at  TIMESTAMP,
        cancel_reason TEXT
      )$f$, s.schema_name);
    EXECUTE format('CREATE INDEX IF NOT EXISTS crm_appointments_slot_idx ON %I.crm_appointments (store, slot_start, status)', s.schema_name);
    EXECUTE format('CREATE INDEX IF NOT EXISTS crm_appointments_customer_idx ON %I.crm_appointments (customer_id, slot_start DESC)', s.schema_name);
    EXECUTE format($f$
      CREATE TABLE IF NOT EXISTS %I.crm_wishlist (
        id          BIGSERIAL PRIMARY KEY,
        customer_id VARCHAR(100) NOT NULL,
        item_code   VARCHAR(200) NOT NULL,
        note        TEXT,
        store       VARCHAR(100),
        created_by  VARCHAR(100),
        created_at  TIMESTAMP    NOT NULL DEFAULT NOW(),
        removed_at  TIMESTAMP
      )$f$, s.schema_name);
    EXECUTE format($f$CREATE UNIQUE INDEX IF NOT EXISTS crm_wishlist_live_uq ON %I.crm_wishlist (customer_id, item_code) WHERE removed_at IS NULL$f$, s.schema_name);
    EXECUTE format($f$
      CREATE TABLE IF NOT EXISTS %I.crm_hold_requests (
        id           VARCHAR(80)  PRIMARY KEY,
        customer_id  VARCHAR(100) NOT NULL,
        item_code    VARCHAR(200) NOT NULL,
        store        VARCHAR(100) NOT NULL,
        qty          NUMERIC(12,3) NOT NULL DEFAULT 1,
        status       VARCHAR(20)  NOT NULL DEFAULT 'Requested',
        hold_until   DATE,
        note         TEXT,
        requested_by VARCHAR(100),
        decided_by   VARCHAR(100),
        decided_at   TIMESTAMP,
        decline_reason TEXT,
        ready_send_id  VARCHAR(80),
        created_at   TIMESTAMP    NOT NULL DEFAULT NOW()
      )$f$, s.schema_name);
    EXECUTE format('CREATE INDEX IF NOT EXISTS crm_hold_requests_store_idx ON %I.crm_hold_requests (store, status)', s.schema_name);

    -- 101.6 the business-buyer pipeline --------------------------------------
    EXECUTE format($f$
      CREATE TABLE IF NOT EXISTS %I.crm_pipeline_stages (
        id          VARCHAR(80)  PRIMARY KEY,
        name        VARCHAR(100) NOT NULL,
        position    INT          NOT NULL,
        probability INT          NOT NULL DEFAULT 0,
        kind        VARCHAR(10)  NOT NULL DEFAULT 'open',
        requires    JSONB        NOT NULL DEFAULT '[]'::jsonb,
        committed   BOOLEAN      NOT NULL DEFAULT FALSE,
        active      BOOLEAN      NOT NULL DEFAULT TRUE
      )$f$, s.schema_name);
    EXECUTE format($f$
      CREATE TABLE IF NOT EXISTS %I.crm_lead_rules (
        id       BIGSERIAL PRIMARY KEY,
        source   VARCHAR(60)  NOT NULL,
        owner    VARCHAR(100) NOT NULL,
        position INT          NOT NULL DEFAULT 0
      )$f$, s.schema_name);
    EXECUTE format($f$
      CREATE TABLE IF NOT EXISTS %I.crm_leads (
        id             VARCHAR(80)  PRIMARY KEY,
        name           VARCHAR(200) NOT NULL,
        company        VARCHAR(200),
        phone          VARCHAR(30),
        email          VARCHAR(200),
        source         VARCHAR(60)  NOT NULL DEFAULT 'Other',
        owner          VARCHAR(100),
        status         VARCHAR(20)  NOT NULL DEFAULT 'New',
        notes          TEXT,
        customer_id    VARCHAR(100),
        opportunity_id VARCHAR(80),
        disqualify_reason TEXT,
        created_by     VARCHAR(100),
        created_at     TIMESTAMP    NOT NULL DEFAULT NOW(),
        updated_at     TIMESTAMP    NOT NULL DEFAULT NOW(),
        converted_at   TIMESTAMP
      )$f$, s.schema_name);
    EXECUTE format('CREATE INDEX IF NOT EXISTS crm_leads_owner_idx ON %I.crm_leads (owner, status)', s.schema_name);
    EXECUTE format($f$
      CREATE TABLE IF NOT EXISTS %I.crm_opportunities (
        id             VARCHAR(80)  PRIMARY KEY,
        title          VARCHAR(300) NOT NULL,
        customer_id    VARCHAR(100) NOT NULL,
        lead_id        VARCHAR(80),
        owner          VARCHAR(100),
        stage_id       VARCHAR(80)  NOT NULL,
        value          NUMERIC(14,2) NOT NULL DEFAULT 0,
        probability    INT          NOT NULL DEFAULT 0,
        expected_close DATE,
        status         VARCHAR(10)  NOT NULL DEFAULT 'Open',
        lost_reason    TEXT,
        quotation_id   VARCHAR(100),
        sales_order_id VARCHAR(100),
        created_by     VARCHAR(100),
        created_at     TIMESTAMP    NOT NULL DEFAULT NOW(),
        updated_at     TIMESTAMP    NOT NULL DEFAULT NOW(),
        closed_at      TIMESTAMP
      )$f$, s.schema_name);
    EXECUTE format('CREATE INDEX IF NOT EXISTS crm_opportunities_stage_idx ON %I.crm_opportunities (status, stage_id)', s.schema_name);
    EXECUTE format($f$CREATE UNIQUE INDEX IF NOT EXISTS crm_opportunities_lead_uq ON %I.crm_opportunities (lead_id) WHERE lead_id IS NOT NULL$f$, s.schema_name);
    EXECUTE format($f$
      CREATE TABLE IF NOT EXISTS %I.crm_opportunity_history (
        id             BIGSERIAL PRIMARY KEY,
        opportunity_id VARCHAR(80)  NOT NULL,
        field          VARCHAR(40)  NOT NULL,
        from_value     TEXT,
        to_value       TEXT,
        changed_by     VARCHAR(100),
        changed_at     TIMESTAMP    NOT NULL DEFAULT NOW()
      )$f$, s.schema_name);
    EXECUTE format('CREATE INDEX IF NOT EXISTS crm_opportunity_history_opp_idx ON %I.crm_opportunity_history (opportunity_id, id)', s.schema_name);
    EXECUTE format($f$
      CREATE TABLE IF NOT EXISTS %I.crm_forecast_snapshots (
        id       VARCHAR(80)  PRIMARY KEY,
        label    VARCHAR(100) NOT NULL,
        taken_by VARCHAR(100),
        taken_at TIMESTAMP    NOT NULL DEFAULT NOW(),
        rows     JSONB        NOT NULL
      )$f$, s.schema_name);

    -- Record types --------------------------------------------------------
    CONTINUE WHEN to_regclass(format('%I.doctype_fields', s.schema_name)) IS NULL;

    EXECUTE format($sql$
      INSERT INTO %I.doctype_fields (doctype_name, fieldname, label, fieldtype, mandatory, options, display_order)
      VALUES
        ('Customer', 'sizes',              'Sizes',                    'Data',  FALSE, NULL, 15),
        ('Customer', 'preferred_store',    'Preferred store',          'Data',  FALSE, NULL, 16),
        ('Customer', 'preferred_salesman', 'Preferred salesman',       'Data',  FALSE, NULL, 17),
        ('Customer', 'tags',               'Tags',                     'Data',  FALSE, NULL, 18),
        ('Customer', 'consent_email',      'Agrees to offers by email', 'Check', FALSE, NULL, 65),
        ('ServiceTicket', 'bill_id',           'Bill',                 'Data',  FALSE, NULL, 20),
        ('ServiceTicket', 'store',             'Store',                'Data',  FALSE, NULL, 21),
        ('ServiceTicket', 'channel',           'Raised by',            'Select', FALSE, 'Store,Phone,WhatsApp,Email,Feedback', 22),
        ('ServiceTicket', 'waiting_on',        'Waiting on',           'Select', FALSE, 'Customer,Internal', 23),
        ('ServiceTicket', 'first_response_at', 'First response',       'Data',  FALSE, NULL, 24),
        ('ServiceTicket', 'resolved_at',       'Resolved on',          'Data',  FALSE, NULL, 25)
      ON CONFLICT (doctype_name, fieldname) DO NOTHING
    $sql$, s.schema_name);

    EXECUTE format($sql$
      UPDATE %I.doctype_fields SET options = 'Active,Inactive,Draft,Pending Approval,Approved,Rejected,Running,Paused,Stopped,Completed'
       WHERE doctype_name = 'Campaign' AND fieldname = 'status' AND options = 'Active,Inactive'
    $sql$, s.schema_name);
    EXECUTE format($sql$
      UPDATE %I.doctype_fields SET options = 'WhatsApp,SMS,Email'
       WHERE doctype_name = 'Campaign' AND fieldname = 'channel' AND options = 'WhatsApp,SMS'
    $sql$, s.schema_name);
    EXECUTE format($sql$
      UPDATE %I.doctype_fields SET mandatory = FALSE
       WHERE doctype_name = 'Campaign' AND fieldname = 'trigger_type'
    $sql$, s.schema_name);

    CONTINUE WHEN to_regclass(format('%I.doctype_meta', s.schema_name)) IS NULL;

    EXECUTE format($sql$
      INSERT INTO %I.doctype_meta (name, module, document_type, module_key)
      VALUES ('CustomerMerge', 'CRM', 'Transaction', 'crm_loyalty'),
             ('SalesQuotation', 'CRM', 'Transaction', 'crm_loyalty')
      ON CONFLICT (name) DO NOTHING
    $sql$, s.schema_name);
    EXECUTE format($sql$
      INSERT INTO %I.doctype_fields (doctype_name, fieldname, label, fieldtype, mandatory, options, display_order)
      VALUES
        ('CustomerMerge', 'keep_id',     'Keep',              'Data', TRUE,  NULL, 1),
        ('CustomerMerge', 'merge_id',    'Merge into it',     'Data', TRUE,  NULL, 2),
        ('CustomerMerge', 'reason',      'Why',               'Data', FALSE, NULL, 3),
        ('CustomerMerge', 'preview',     'What moves',        'Data', FALSE, NULL, 4),
        ('CustomerMerge', 'status',      'Status',            'Select', TRUE, 'Draft,Pending Approval,Approved,Merged,Rejected', 5),
        ('SalesQuotation', 'customer',       'Customer',       'Link', TRUE,  'Customer', 1),
        ('SalesQuotation', 'opportunity_id', 'Opportunity',    'Data', FALSE, NULL, 2),
        ('SalesQuotation', 'valid_until',    'Valid until',    'Date', FALSE, NULL, 3),
        ('SalesQuotation', 'items',          'Lines',          'Data', TRUE,  NULL, 4),
        ('SalesQuotation', 'total_amount',   'Total',          'Number', TRUE, NULL, 5),
        ('SalesQuotation', 'terms',          'Terms',          'Data', FALSE, NULL, 6),
        ('SalesQuotation', 'status',         'Status',         'Select', TRUE, 'Draft,Pending Approval,Approved,Rejected,Converted', 7)
      ON CONFLICT (doctype_name, fieldname) DO NOTHING
    $sql$, s.schema_name);
    EXECUTE format($sql$
      INSERT INTO %I.role_permissions (role, doctype_name, allow_read, allow_create, allow_update, allow_delete)
      VALUES ('HR/Admin', 'CustomerMerge', TRUE, FALSE, FALSE, FALSE),
             ('Store Manager', 'CustomerMerge', TRUE, FALSE, FALSE, FALSE),
             ('HR/Admin', 'SalesQuotation', TRUE, TRUE, TRUE, FALSE),
             ('Store Manager', 'SalesQuotation', TRUE, TRUE, TRUE, FALSE)
      ON CONFLICT (role, doctype_name) DO NOTHING
    $sql$, s.schema_name);

    IF to_regclass(format('%I.approval_rules', s.schema_name)) IS NOT NULL THEN
      EXECUTE format($sql$
        INSERT INTO %I.approval_rules (doctype, min_amount, max_amount, required_role)
        SELECT 'Campaign', 0, NULL, 'Store Manager'
         WHERE NOT EXISTS (SELECT 1 FROM %I.approval_rules WHERE doctype = 'Campaign')
      $sql$, s.schema_name, s.schema_name);
      EXECUTE format($sql$
        INSERT INTO %I.approval_rules (doctype, min_amount, max_amount, required_role)
        SELECT 'CustomerMerge', 0, NULL, 'Store Manager'
         WHERE NOT EXISTS (SELECT 1 FROM %I.approval_rules WHERE doctype = 'CustomerMerge')
      $sql$, s.schema_name, s.schema_name);
      EXECUTE format($sql$
        INSERT INTO %I.approval_rules (doctype, min_amount, max_amount, required_role)
        SELECT 'SalesQuotation', 0, NULL, 'Store Manager'
         WHERE NOT EXISTS (SELECT 1 FROM %I.approval_rules WHERE doctype = 'SalesQuotation')
      $sql$, s.schema_name, s.schema_name);
    END IF;
  END LOOP;
END
$mig$;
