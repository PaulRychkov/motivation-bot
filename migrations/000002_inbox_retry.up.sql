ALTER TABLE inbox_events
    ADD COLUMN attempts integer NOT NULL DEFAULT 0,
    ADD COLUMN last_error text,
    ADD COLUMN next_attempt_at timestamptz;
