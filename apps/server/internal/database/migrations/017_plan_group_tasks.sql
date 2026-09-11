-- 017: 报告与方案解耦——tasks 表新增 plan_group 任务类型
-- （general 方案组从报告内容生成，见 service.processPlanGroup）。
ALTER TABLE tasks DROP CONSTRAINT IF EXISTS tasks_type_check;
ALTER TABLE tasks ADD CONSTRAINT tasks_type_check
  CHECK (type IN ('analysis', 'hair_preview', 'plan_group', 'plan_look', 'today_look'));
