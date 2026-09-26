DROP INDEX IF EXISTS interventions_snooze_idx;
DROP INDEX IF EXISTS interventions_task_stage_idx;

ALTER TABLE interventions
    DROP COLUMN snooze_until,
    DROP COLUMN stage;
