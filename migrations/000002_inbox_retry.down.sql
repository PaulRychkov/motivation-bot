ALTER TABLE inbox_events
    DROP COLUMN attempts,
    DROP COLUMN last_error,
    DROP COLUMN next_attempt_at;
