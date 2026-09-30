-- Retire group effort ceilings/mappings and allow future effort labels in logs.
UPDATE groups
SET max_reasoning_effort = '', reasoning_effort_mappings = '[]'::jsonb
WHERE max_reasoning_effort <> '' OR reasoning_effort_mappings <> '[]'::jsonb;

ALTER TABLE usage_logs ALTER COLUMN reasoning_effort TYPE TEXT;
