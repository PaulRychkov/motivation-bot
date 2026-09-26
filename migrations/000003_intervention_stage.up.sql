ALTER TABLE interventions
    ADD COLUMN stage text,
    ADD COLUMN snooze_until timestamptz;

CREATE INDEX interventions_task_stage_idx
    ON interventions (chat_profile_id, task_external_id, target_date, stage)
    WHERE task_external_id IS NOT NULL;

CREATE INDEX interventions_snooze_idx
    ON interventions (task_external_id, snooze_until)
    WHERE snooze_until IS NOT NULL;
