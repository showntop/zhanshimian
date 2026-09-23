-- 每日内容（Daily Content）：知识事实 → 生成产物 → 生成审计 → 收藏。
--
-- 四张表对应方案 §2.2，主键/外键遵循本仓口径（uuid + 复合外键 + ON DELETE CASCADE）：
--   knowledge_fact   人工编辑生产、顾问确认的原子知识事实（整个系统的原料）
--   daily_content    每天每用户一条；user_id 为空的行是公共兜底池条目
--   generation_run   每次 LLM 调用一条（评估与回溯）
--   daily_collection 收藏：内容引用 + 完整副本 + 多态素材 + 生命周期
--
-- 归属由外键结构性保证：注销（DELETE /v1/me/data）经 users 级联带走，
-- 不需要单独的清除分支。

CREATE TABLE knowledge_fact (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  domain text NOT NULL CHECK (domain IN ('color','fit','proportion','fabric','occasion','howto','outfit')),
  fact text NOT NULL,
  boundary text NOT NULL DEFAULT '',
  gene_fit jsonb NOT NULL DEFAULT '{}'::jsonb,
  season text[] NOT NULL DEFAULT '{}',
  source text NOT NULL DEFAULT '编辑整理',
  reviewed boolean NOT NULL DEFAULT false,
  created_at timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX knowledge_fact_domain_idx ON knowledge_fact (domain, reviewed);

CREATE TABLE daily_content (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  user_id uuid REFERENCES users(id) ON DELETE CASCADE,
  gen_date date NOT NULL,
  category text NOT NULL,
  topic text NOT NULL,
  lead text NOT NULL,
  fit_text text NOT NULL,
  why text NOT NULL,
  visual jsonb NOT NULL DEFAULT '{}'::jsonb,
  fact_ids uuid[] NOT NULL DEFAULT '{}',
  source text NOT NULL DEFAULT 'generated' CHECK (source IN ('generated','fallback')),
  model_key text NOT NULL DEFAULT '',
  dedupe_key text NOT NULL,
  created_at timestamptz NOT NULL DEFAULT now()
);

-- 同一用户同一天只生成一次（方案铁律 2）。兜底池条目 user_id IS NULL，
-- 不受此约束——池里每个分类可以有多条。
CREATE UNIQUE INDEX daily_content_user_date_idx
  ON daily_content (user_id, gen_date) WHERE user_id IS NOT NULL;
CREATE INDEX daily_content_user_idx ON daily_content (user_id, gen_date DESC);
CREATE INDEX daily_content_pool_idx ON daily_content (category, dedupe_key) WHERE user_id IS NULL;

CREATE TABLE generation_run (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  user_id uuid REFERENCES users(id) ON DELETE CASCADE,
  gen_date date,
  fact_ids uuid[] NOT NULL DEFAULT '{}',
  prompt_hash text NOT NULL DEFAULT '',
  output jsonb,
  validation jsonb,
  outcome text NOT NULL CHECK (outcome IN ('accepted','rejected','fallback')),
  model_key text NOT NULL DEFAULT '',
  latency_ms integer NOT NULL DEFAULT 0,
  estimated_cost_cny double precision,
  created_at timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX generation_run_user_idx ON generation_run (user_id, created_at DESC);

CREATE TABLE daily_collection (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  user_id uuid NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  content_id text NOT NULL,
  content_key text NOT NULL,
  content_snapshot jsonb NOT NULL,
  title text NOT NULL,
  summary text NOT NULL DEFAULT '',
  category text NOT NULL,
  assets jsonb NOT NULL DEFAULT '[]'::jsonb,
  context jsonb NOT NULL DEFAULT '{}'::jsonb,
  gene_snapshot jsonb,
  status text NOT NULL DEFAULT 'saved' CHECK (status IN ('saved','tried','kept')),
  note text NOT NULL DEFAULT '',
  saved_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now(),
  UNIQUE (user_id, id),
  UNIQUE (user_id, content_key)
);

-- 打开手册某一格
CREATE INDEX daily_collection_user_category_idx ON daily_collection (user_id, category, saved_at DESC);
-- 手册时间线
CREATE INDEX daily_collection_user_saved_idx ON daily_collection (user_id, saved_at DESC);
