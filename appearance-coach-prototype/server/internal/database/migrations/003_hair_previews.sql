CREATE TABLE hair_previews (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  user_id uuid NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  report_id uuid REFERENCES reports(id) ON DELETE SET NULL,
  media_id uuid NOT NULL REFERENCES media_assets(id) ON DELETE CASCADE,
  scene text NOT NULL DEFAULT 'daily',
  style_id text NOT NULL CHECK (style_id IN ('sharp', 'warm', 'natural')),
  style_name text NOT NULL,
  status text NOT NULL DEFAULT 'queued' CHECK (status IN ('queued', 'processing', 'completed', 'failed')),
  progress int NOT NULL DEFAULT 0 CHECK (progress BETWEEN 0 AND 100),
  stage text NOT NULL DEFAULT '等待生成',
  source_image_url text NOT NULL,
  result_image_url text NOT NULL DEFAULT '',
	result_storage_key text NOT NULL DEFAULT '',
  provider_version text NOT NULL DEFAULT '',
  saved boolean NOT NULL DEFAULT false,
  error_message text NOT NULL DEFAULT '',
  attempts int NOT NULL DEFAULT 0,
  next_run_at timestamptz NOT NULL DEFAULT now(),
  locked_at timestamptz,
  last_error text NOT NULL DEFAULT '',
  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX hair_previews_claim_idx ON hair_previews(status, next_run_at, created_at);
CREATE INDEX hair_previews_user_created_idx ON hair_previews(user_id, created_at DESC);
