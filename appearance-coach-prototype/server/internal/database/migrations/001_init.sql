CREATE EXTENSION IF NOT EXISTS pgcrypto;

CREATE TABLE IF NOT EXISTS schema_migrations (
  version text PRIMARY KEY,
  applied_at timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE users (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  open_id text NOT NULL UNIQUE,
  nickname text NOT NULL DEFAULT '见我用户',
  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE user_sessions (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  user_id uuid NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  token_digest bytea NOT NULL UNIQUE,
  expires_at timestamptz NOT NULL,
  created_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX user_sessions_user_id_idx ON user_sessions(user_id);

CREATE TABLE media_assets (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  user_id uuid NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  kind text NOT NULL CHECK (kind IN ('face', 'side', 'body', 'feedback')),
  storage_key text NOT NULL,
  mime_type text NOT NULL,
  byte_size bigint NOT NULL CHECK (byte_size > 0),
  created_at timestamptz NOT NULL DEFAULT now(),
  deleted_at timestamptz
);
CREATE INDEX media_assets_user_id_idx ON media_assets(user_id) WHERE deleted_at IS NULL;

CREATE TABLE analyses (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  user_id uuid NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  scene text NOT NULL DEFAULT 'general',
  media_ids uuid[] NOT NULL,
  profile jsonb NOT NULL DEFAULT '{}'::jsonb,
  status text NOT NULL CHECK (status IN ('queued', 'processing', 'completed', 'failed')),
  progress int NOT NULL DEFAULT 0 CHECK (progress BETWEEN 0 AND 100),
  stage text NOT NULL DEFAULT '等待分析',
  error_message text NOT NULL DEFAULT '',
  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX analyses_user_created_idx ON analyses(user_id, created_at DESC);

CREATE TABLE analysis_jobs (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  analysis_id uuid NOT NULL UNIQUE REFERENCES analyses(id) ON DELETE CASCADE,
  user_id uuid NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  payload jsonb NOT NULL,
  status text NOT NULL DEFAULT 'queued' CHECK (status IN ('queued', 'running', 'done', 'failed')),
  attempts int NOT NULL DEFAULT 0,
  next_run_at timestamptz NOT NULL DEFAULT now(),
  locked_at timestamptz,
  last_error text NOT NULL DEFAULT '',
  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX analysis_jobs_claim_idx ON analysis_jobs(status, next_run_at, created_at);

CREATE TABLE reports (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  analysis_id uuid NOT NULL UNIQUE REFERENCES analyses(id) ON DELETE CASCADE,
  user_id uuid NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  current_image_url text NOT NULL,
  impression_tags text[] NOT NULL,
  priority_title text NOT NULL,
  priority_copy text NOT NULL,
  provider_version text NOT NULL,
  generated_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX reports_user_id_idx ON reports(user_id);

CREATE TABLE report_findings (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  report_id uuid NOT NULL REFERENCES reports(id) ON DELETE CASCADE,
  label text NOT NULL,
  category text NOT NULL,
  severity text NOT NULL,
  anchor_x double precision NOT NULL CHECK (anchor_x BETWEEN 0 AND 1),
  anchor_y double precision NOT NULL CHECK (anchor_y BETWEEN 0 AND 1),
  sort_order int NOT NULL DEFAULT 0
);

CREATE TABLE plans (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  report_id uuid NOT NULL REFERENCES reports(id) ON DELETE CASCADE,
  user_id uuid NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  name text NOT NULL,
  slug text NOT NULL,
  image_url text NOT NULL,
  recommended boolean NOT NULL DEFAULT false,
  descriptor text NOT NULL,
  why text NOT NULL,
  outcome_tags text[] NOT NULL,
  difference_tags text[] NOT NULL,
  sort_order int NOT NULL,
  selected_at timestamptz,
  UNIQUE(report_id, slug)
);
CREATE INDEX plans_report_sort_idx ON plans(report_id, sort_order);

CREATE TABLE plan_steps (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  plan_id uuid NOT NULL REFERENCES plans(id) ON DELETE CASCADE,
  category text NOT NULL CHECK (category IN ('hair', 'makeup', 'outfit')),
  title text NOT NULL,
  summary text NOT NULL,
  details jsonb NOT NULL DEFAULT '[]'::jsonb,
  sort_order int NOT NULL,
  UNIQUE(plan_id, category)
);

CREATE TABLE checklist_items (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  plan_id uuid NOT NULL REFERENCES plans(id) ON DELETE CASCADE,
  user_id uuid NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  category text NOT NULL,
  title text NOT NULL,
  description text NOT NULL,
  meta text NOT NULL DEFAULT '',
  completed boolean NOT NULL DEFAULT false,
  sort_order int NOT NULL,
  updated_at timestamptz NOT NULL DEFAULT now(),
  UNIQUE(plan_id, category)
);
CREATE INDEX checklist_user_plan_idx ON checklist_items(user_id, plan_id, sort_order);

CREATE TABLE feedback (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  user_id uuid NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  plan_id uuid NOT NULL REFERENCES plans(id) ON DELETE CASCADE,
  tags text[] NOT NULL,
  comment text NOT NULL DEFAULT '',
  created_at timestamptz NOT NULL DEFAULT now()
);
