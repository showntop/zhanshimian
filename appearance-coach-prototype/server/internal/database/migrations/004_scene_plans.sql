ALTER TABLE plans
  ADD COLUMN IF NOT EXISTS scene text NOT NULL DEFAULT 'general',
  ADD COLUMN IF NOT EXISTS scene_brief jsonb NOT NULL DEFAULT '{}'::jsonb;

ALTER TABLE plans DROP CONSTRAINT IF EXISTS plans_report_id_slug_key;
ALTER TABLE plans DROP CONSTRAINT IF EXISTS plans_report_scene_slug_key;
ALTER TABLE plans
  ADD CONSTRAINT plans_report_scene_slug_key UNIQUE(report_id, scene, slug);

CREATE INDEX IF NOT EXISTS plans_report_scene_sort_idx ON plans(report_id, scene, sort_order);
