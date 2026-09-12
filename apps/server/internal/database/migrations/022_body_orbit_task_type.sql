-- 022: body_orbit 任务类型（3D 形象 Lite）
ALTER TABLE tasks DROP CONSTRAINT IF EXISTS tasks_type_check;
ALTER TABLE tasks ADD CONSTRAINT tasks_type_check
  CHECK (type IN ('analysis', 'hair_preview', 'plan_group', 'plan_look', 'today_look', 'body_orbit'));
