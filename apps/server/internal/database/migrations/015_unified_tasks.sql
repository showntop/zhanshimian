-- 统一任务系统：4 套队列（analysis_jobs / hair_previews / plans.generation_* /
-- today_plans.generation_*）收敛为一张 tasks 表 + 一个认领循环。
CREATE TABLE tasks (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  user_id uuid NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  type text NOT NULL CHECK (type IN ('analysis', 'hair_preview', 'plan_look', 'today_look')),
  payload jsonb NOT NULL DEFAULT '{}'::jsonb,
  status text NOT NULL DEFAULT 'queued' CHECK (status IN ('queued', 'processing', 'completed', 'failed')),
  progress int NOT NULL DEFAULT 0 CHECK (progress BETWEEN 0 AND 100),
  stage text NOT NULL DEFAULT '',
  attempts int NOT NULL DEFAULT 0,
  next_run_at timestamptz NOT NULL DEFAULT now(),
  locked_at timestamptz,
  last_error text NOT NULL DEFAULT '',
  result_ref text NOT NULL DEFAULT '',
  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX tasks_claim_idx ON tasks(status, next_run_at, created_at);
CREATE INDEX tasks_user_created_idx ON tasks(user_id, created_at DESC);

-- 存量数据向前兼容：把仍在排队/执行中的任务搬进 tasks（payload 只带领域行 ref，
-- 处理器按 ref 回读业务数据）。重新排队即可——旧 worker 已随发布退出。
INSERT INTO tasks(user_id, type, payload, status, progress, stage, attempts, next_run_at, created_at, updated_at)
SELECT j.user_id, 'analysis',
  jsonb_build_object('analysis_id', j.analysis_id::text),
  'queued', a.progress, a.stage, j.attempts, now(), j.created_at, now()
FROM analysis_jobs j
JOIN analyses a ON a.id = j.analysis_id
WHERE j.status IN ('queued', 'running');

INSERT INTO tasks(user_id, type, payload, status, progress, stage, attempts, next_run_at, created_at, updated_at)
SELECT h.user_id, 'hair_preview',
  jsonb_build_object('preview_id', h.id::text),
  'queued', h.progress, h.stage, h.attempts, now(), h.created_at, now()
FROM hair_previews h
WHERE h.status IN ('queued', 'processing');

INSERT INTO tasks(user_id, type, payload, status, attempts, next_run_at, created_at, updated_at)
SELECT p.user_id, 'plan_look',
  jsonb_build_object('plan_id', p.id::text),
  'queued', p.generation_attempts, now(), now(), now()
FROM plans p
WHERE p.generation_status IN ('queued', 'processing');

INSERT INTO tasks(user_id, type, payload, status, attempts, next_run_at, created_at, updated_at)
SELECT t.user_id, 'today_look',
  jsonb_build_object('plan_id', t.id::text),
  'queued', t.generation_attempts, now(), t.created_at, now()
FROM today_plans t
WHERE t.generation_status IN ('queued', 'processing');

-- analysis_jobs 废弃：队列状态由 tasks 承载，业务状态留在 analyses。
DROP TABLE IF EXISTS analysis_jobs;

-- 领域表瘦身：删除全部队列列。保留只读展示列 generated_image_url / look_provider，
-- 以及注销账号时清理对象存储所需的 *_storage_key。
ALTER TABLE plans
  DROP COLUMN IF EXISTS generation_status,
  DROP COLUMN IF EXISTS generation_attempts,
  DROP COLUMN IF EXISTS generation_next_run_at,
  DROP COLUMN IF EXISTS generation_locked_at,
  DROP COLUMN IF EXISTS generation_error;
DROP INDEX IF EXISTS plans_generation_claim_idx;

ALTER TABLE today_plans
  DROP COLUMN IF EXISTS generation_status,
  DROP COLUMN IF EXISTS generation_attempts,
  DROP COLUMN IF EXISTS generation_next_run_at,
  DROP COLUMN IF EXISTS generation_locked_at,
  DROP COLUMN IF EXISTS generation_error;
DROP INDEX IF EXISTS today_plans_generation_claim_idx;

ALTER TABLE hair_previews
  DROP COLUMN IF EXISTS status,
  DROP COLUMN IF EXISTS progress,
  DROP COLUMN IF EXISTS stage,
  DROP COLUMN IF EXISTS error_message,
  DROP COLUMN IF EXISTS attempts,
  DROP COLUMN IF EXISTS next_run_at,
  DROP COLUMN IF EXISTS locked_at,
  DROP COLUMN IF EXISTS last_error;
DROP INDEX IF EXISTS hair_previews_claim_idx;
