-- Sprint 13: Workspace file panel — track output files per task
-- Adds output_files column to zbot_tasks to record files created during task execution.

ALTER TABLE zbot_tasks ADD COLUMN IF NOT EXISTS output_files TEXT[] DEFAULT '{}';

-- Index for workflows that produced files (useful for /api/workflow/:id/files endpoint).
CREATE INDEX IF NOT EXISTS idx_zbot_tasks_output_files ON zbot_tasks USING GIN (output_files) WHERE output_files != '{}';
