ALTER TABLE media_assets DROP CONSTRAINT IF EXISTS media_assets_kind_check;
ALTER TABLE media_assets ADD CONSTRAINT media_assets_kind_check
  CHECK (kind IN ('face', 'side', 'body', 'feedback', 'outfit', 'product'));

CREATE TABLE tool_results (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  user_id uuid NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  report_id uuid REFERENCES reports(id) ON DELETE SET NULL,
  media_id uuid REFERENCES media_assets(id) ON DELETE SET NULL,
  kind text NOT NULL CHECK (kind IN ('hair', 'outfit', 'purchase')),
  scene text NOT NULL DEFAULT 'daily',
  payload jsonb NOT NULL,
  saved boolean NOT NULL DEFAULT false,
  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX tool_results_user_created_idx ON tool_results(user_id, created_at DESC);
