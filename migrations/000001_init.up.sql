CREATE EXTENSION IF NOT EXISTS pgcrypto;

CREATE TYPE intervention_kind AS ENUM ('morning_plan', 'evening_review', 'deadline_reminder', 'free_ping', 'question', 'praise');
CREATE TYPE intervention_trigger AS ENUM ('schedule', 'agent');
CREATE TYPE intervention_outcome AS ENUM ('pending', 'activated', 'partial', 'ignored', 'refused', 'rescheduled');
CREATE TYPE message_role AS ENUM ('user', 'assistant', 'tool');
CREATE TYPE tone AS ENUM ('gentle', 'neutral', 'energetic', 'strict', 'humorous');

CREATE TABLE chat_profiles (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    chat_id bigint UNIQUE NOT NULL,
    timezone text NOT NULL,
    quiet_start_min integer,
    quiet_end_min integer,
    morning_plan_min integer NOT NULL DEFAULT 540,
    evening_review_min integer NOT NULL DEFAULT 1290,
    free_ping_daily_budget integer NOT NULL DEFAULT 3,
    default_tone tone NOT NULL DEFAULT 'neutral',
    persona text,
    paused_until date,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT chat_profiles_quiet_pair CHECK ((quiet_start_min IS NULL) = (quiet_end_min IS NULL)),
    CONSTRAINT chat_profiles_quiet_start_range CHECK (quiet_start_min IS NULL OR (quiet_start_min >= 0 AND quiet_start_min <= 1439)),
    CONSTRAINT chat_profiles_quiet_end_range CHECK (quiet_end_min IS NULL OR (quiet_end_min >= 0 AND quiet_end_min <= 1439)),
    CONSTRAINT chat_profiles_morning_range CHECK (morning_plan_min >= 0 AND morning_plan_min <= 1439),
    CONSTRAINT chat_profiles_evening_range CHECK (evening_review_min >= 0 AND evening_review_min <= 1439)
);

CREATE TABLE interventions (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    chat_profile_id uuid NOT NULL REFERENCES chat_profiles (id),
    kind intervention_kind NOT NULL,
    triggered_by intervention_trigger NOT NULL,
    task_source text,
    task_external_id text,
    target_date date,
    body text NOT NULL,
    tone tone NOT NULL,
    telegram_message_id bigint,
    sent_at timestamptz NOT NULL,
    window_ends_at timestamptz NOT NULL,
    outcome intervention_outcome NOT NULL DEFAULT 'pending',
    evidence jsonb NOT NULL DEFAULT '[]',
    outcome_at timestamptz,
    local_date date NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT interventions_task_pair CHECK ((task_source IS NULL) = (task_external_id IS NULL)),
    CONSTRAINT interventions_window_after_sent CHECK (window_ends_at > sent_at)
);

CREATE UNIQUE INDEX interventions_scheduled_daily_uniq ON interventions (chat_profile_id, kind, local_date)
    WHERE kind IN ('morning_plan', 'evening_review');
CREATE INDEX interventions_profile_sent_idx ON interventions (chat_profile_id, sent_at DESC);
CREATE INDEX interventions_pending_window_idx ON interventions (window_ends_at) WHERE outcome = 'pending';
CREATE INDEX interventions_task_idx ON interventions (task_source, task_external_id) WHERE task_source IS NOT NULL;

CREATE TABLE inbox_events (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    event_id text NOT NULL,
    source text NOT NULL,
    type text NOT NULL,
    subject text,
    occurred_at timestamptz NOT NULL,
    payload jsonb NOT NULL,
    processed_at timestamptz,
    created_at timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT inbox_events_source_event_uniq UNIQUE (source, event_id)
);

CREATE INDEX inbox_events_unprocessed_idx ON inbox_events (occurred_at) WHERE processed_at IS NULL;
CREATE INDEX inbox_events_subject_idx ON inbox_events (source, subject, occurred_at);

CREATE TABLE dialog_messages (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    chat_profile_id uuid NOT NULL REFERENCES chat_profiles (id),
    role message_role NOT NULL,
    content text,
    tool_calls jsonb,
    tool_call_id text,
    intervention_id uuid REFERENCES interventions (id) ON DELETE SET NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT dialog_messages_has_body CHECK (content IS NOT NULL OR tool_calls IS NOT NULL),
    CONSTRAINT dialog_messages_tool_calls_role CHECK (tool_calls IS NULL OR role = 'assistant'),
    CONSTRAINT dialog_messages_tool_call_id_role CHECK ((role = 'tool') = (tool_call_id IS NOT NULL))
);

CREATE INDEX dialog_messages_profile_created_idx ON dialog_messages (chat_profile_id, created_at DESC);

CREATE TABLE agent_notes (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    key text UNIQUE NOT NULL,
    value jsonb NOT NULL,
    expires_at timestamptz,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE events_outbox (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    event_type text NOT NULL,
    aggregate_type text NOT NULL,
    aggregate_id uuid NOT NULL,
    payload jsonb NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    published_at timestamptz,
    attempts integer NOT NULL DEFAULT 0,
    last_error text
);

CREATE INDEX events_outbox_unpublished_idx ON events_outbox (created_at) WHERE published_at IS NULL;
