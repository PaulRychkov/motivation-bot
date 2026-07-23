CREATE TABLE chat_profiles (
    id text PRIMARY KEY,
    chat_id integer UNIQUE NOT NULL,
    timezone text NOT NULL,
    quiet_start_min integer,
    quiet_end_min integer,
    morning_plan_min integer NOT NULL DEFAULT 540,
    evening_review_min integer NOT NULL DEFAULT 1290,
    free_ping_daily_budget integer NOT NULL DEFAULT 3,
    default_tone text NOT NULL DEFAULT 'neutral' CHECK (default_tone IN ('gentle','neutral','energetic','strict','humorous')),
    persona text,
    paused_until text,
    created_at datetime NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at datetime NOT NULL DEFAULT CURRENT_TIMESTAMP,
    CONSTRAINT chat_profiles_quiet_pair CHECK ((quiet_start_min IS NULL) = (quiet_end_min IS NULL)),
    CONSTRAINT chat_profiles_morning_range CHECK (morning_plan_min >= 0 AND morning_plan_min <= 1439),
    CONSTRAINT chat_profiles_evening_range CHECK (evening_review_min >= 0 AND evening_review_min <= 1439)
);

CREATE TABLE interventions (
    id text PRIMARY KEY,
    chat_profile_id text NOT NULL REFERENCES chat_profiles (id),
    kind text NOT NULL CHECK (kind IN ('morning_plan','evening_review','deadline_reminder','free_ping','question','praise')),
    triggered_by text NOT NULL CHECK (triggered_by IN ('schedule','agent')),
    task_source text,
    task_external_id text,
    target_date text,
    body text NOT NULL,
    tone text NOT NULL CHECK (tone IN ('gentle','neutral','energetic','strict','humorous')),
    telegram_message_id integer,
    sent_at datetime NOT NULL,
    window_ends_at datetime NOT NULL,
    outcome text NOT NULL DEFAULT 'pending' CHECK (outcome IN ('pending','activated','partial','ignored','refused','rescheduled')),
    evidence text NOT NULL DEFAULT '[]',
    outcome_at datetime,
    local_date text NOT NULL,
    created_at datetime NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at datetime NOT NULL DEFAULT CURRENT_TIMESTAMP,
    CONSTRAINT interventions_task_pair CHECK ((task_source IS NULL) = (task_external_id IS NULL)),
    CONSTRAINT interventions_window_after_sent CHECK (window_ends_at > sent_at)
);

CREATE UNIQUE INDEX interventions_scheduled_daily_uniq ON interventions (chat_profile_id, kind, local_date)
    WHERE kind IN ('morning_plan', 'evening_review');
CREATE INDEX interventions_profile_sent_idx ON interventions (chat_profile_id, sent_at DESC);
CREATE INDEX interventions_pending_window_idx ON interventions (window_ends_at) WHERE outcome = 'pending';
CREATE INDEX interventions_task_idx ON interventions (task_source, task_external_id) WHERE task_source IS NOT NULL;

CREATE TABLE inbox_events (
    id text PRIMARY KEY,
    event_id text NOT NULL,
    source text NOT NULL,
    type text NOT NULL,
    subject text,
    occurred_at datetime NOT NULL,
    payload text NOT NULL,
    processed_at datetime,
    attempts integer NOT NULL DEFAULT 0,
    last_error text,
    next_attempt_at datetime,
    created_at datetime NOT NULL DEFAULT CURRENT_TIMESTAMP,
    CONSTRAINT inbox_events_source_event_uniq UNIQUE (source, event_id)
);

CREATE INDEX inbox_events_unprocessed_idx ON inbox_events (occurred_at) WHERE processed_at IS NULL;
CREATE INDEX inbox_events_subject_idx ON inbox_events (source, subject, occurred_at);

CREATE TABLE dialog_messages (
    id text PRIMARY KEY,
    chat_profile_id text NOT NULL REFERENCES chat_profiles (id),
    role text NOT NULL CHECK (role IN ('user','assistant','tool')),
    content text,
    tool_calls text,
    tool_call_id text,
    intervention_id text REFERENCES interventions (id) ON DELETE SET NULL,
    created_at datetime NOT NULL DEFAULT CURRENT_TIMESTAMP,
    CONSTRAINT dialog_messages_has_body CHECK (content IS NOT NULL OR tool_calls IS NOT NULL)
);

CREATE INDEX dialog_messages_profile_created_idx ON dialog_messages (chat_profile_id, created_at DESC);

CREATE TABLE agent_notes (
    id text PRIMARY KEY,
    key text UNIQUE NOT NULL,
    value text NOT NULL,
    expires_at datetime,
    created_at datetime NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at datetime NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE events_outbox (
    id text PRIMARY KEY,
    event_type text NOT NULL,
    aggregate_type text NOT NULL,
    aggregate_id text NOT NULL,
    payload text NOT NULL,
    created_at datetime NOT NULL DEFAULT CURRENT_TIMESTAMP,
    published_at datetime,
    attempts integer NOT NULL DEFAULT 0,
    last_error text
);

CREATE INDEX events_outbox_unpublished_idx ON events_outbox (created_at) WHERE published_at IS NULL;
