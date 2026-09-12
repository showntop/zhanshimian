CREATE EXTENSION IF NOT EXISTS pgcrypto;

CREATE FUNCTION reject_immutable_update() RETURNS trigger
LANGUAGE plpgsql AS $$
BEGIN
  RAISE EXCEPTION 'immutable relation % cannot be changed', TG_TABLE_NAME
    USING ERRCODE = '55000';
END;
$$;

CREATE TABLE users (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  nickname text NOT NULL DEFAULT '',
  created_at timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE user_identities (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  user_id uuid NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  provider text NOT NULL CHECK (provider IN ('wechat_miniapp','wechat_app','apple','phone')),
  identifier text NOT NULL,
  created_at timestamptz NOT NULL DEFAULT now(),
  UNIQUE (user_id, id),
  UNIQUE (provider, identifier)
);

CREATE TABLE user_sessions (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  user_id uuid NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  token_digest bytea NOT NULL UNIQUE,
  expires_at timestamptz NOT NULL,
  created_at timestamptz NOT NULL DEFAULT now(),
  UNIQUE (user_id, id)
);

CREATE TABLE user_profiles (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  user_id uuid NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  role text NOT NULL DEFAULT '',
  height_cm int CHECK (height_cm IS NULL OR height_cm BETWEEN 100 AND 250),
  budget text NOT NULL DEFAULT '',
  preferences jsonb NOT NULL DEFAULT '{}'::jsonb,
  avoidances jsonb NOT NULL DEFAULT '{}'::jsonb,
  current_report_id uuid,
  version int NOT NULL DEFAULT 1 CHECK (version > 0),
  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now(),
  UNIQUE (user_id, id),
  UNIQUE (user_id)
);

CREATE TABLE operations (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  user_id uuid NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  kind text NOT NULL CHECK (kind IN ('assessment','plan_set','render','execution_feedback')),
  subject_type text NOT NULL,
  subject_id uuid NOT NULL,
  status text NOT NULL CHECK (status IN
    ('accepted','running','retrying','succeeded','failed','cancelled','superseded')),
  progress_bps int NOT NULL DEFAULT 0 CHECK (progress_bps BETWEEN 0 AND 10000),
  stage_code text NOT NULL DEFAULT '',
  public_message text NOT NULL DEFAULT '',
  error_code text,
  trace_id text,
  retryable boolean NOT NULL DEFAULT false,
  result_type text,
  result_id uuid,
  version int NOT NULL DEFAULT 1 CHECK (version > 0),
  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now(),
  finished_at timestamptz,
  UNIQUE (user_id, id),
  CHECK (status <> 'failed' OR trace_id IS NOT NULL)
);

CREATE TABLE tasks (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  user_id uuid NOT NULL,
  operation_id uuid NOT NULL,
  type text NOT NULL,
  subject_type text NOT NULL,
  subject_id uuid NOT NULL,
  subject_generation bigint NOT NULL DEFAULT 0,
  payload_version int NOT NULL CHECK (payload_version > 0),
  payload jsonb NOT NULL,
  dedupe_key text NOT NULL,
  status text NOT NULL CHECK (status IN
    ('queued','leased','retry_wait','succeeded','failed','cancelled','superseded')),
  priority int NOT NULL DEFAULT 0,
  attempt int NOT NULL DEFAULT 0 CHECK (attempt >= 0),
  max_attempts int NOT NULL CHECK (max_attempts > 0),
  available_at timestamptz NOT NULL DEFAULT now(),
  lease_token uuid,
  lease_owner text,
  lease_expires_at timestamptz,
  heartbeat_at timestamptz,
  cancel_requested_at timestamptz,
  progress_bps int NOT NULL DEFAULT 0 CHECK (progress_bps BETWEEN 0 AND 10000),
  stage_code text NOT NULL,
  error_class text CHECK (error_class IN
    ('transient','throttled','permanent','quality_rejected','superseded')),
  error_code text,
  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now(),
  finished_at timestamptz,
  UNIQUE(user_id,id),
  UNIQUE(user_id,dedupe_key),
  FOREIGN KEY(user_id,operation_id) REFERENCES operations(user_id,id) ON DELETE CASCADE,
  CHECK (
    (status='leased' AND lease_token IS NOT NULL AND lease_owner IS NOT NULL AND lease_expires_at IS NOT NULL)
    OR status<>'leased'
  )
);

ALTER TABLE tasks
  ADD CONSTRAINT tasks_user_fk
  FOREIGN KEY (user_id) REFERENCES users(id) ON DELETE CASCADE;

CREATE TABLE provider_invocations (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  user_id uuid NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  operation_id uuid NOT NULL,
  task_id uuid NOT NULL,
  attempt_no int NOT NULL CHECK (attempt_no >= 0),
  capability text NOT NULL,
  routing_config_version text NOT NULL,
  provider_key text NOT NULL,
  model_key text NOT NULL,
  protocol text NOT NULL,
  request_hash text NOT NULL CHECK (request_hash ~ '^[0-9a-f]{64}$'),
  provider_request_id text,
  status text NOT NULL CHECK (status IN ('started','succeeded','failed')),
  input_tokens int,
  output_tokens int,
  input_images int,
  output_images int,
  estimated_cost_cny numeric,
  latency_ms int,
  error_class text CHECK (error_class IN
    ('transient','throttled','permanent','quality_rejected','superseded')),
  error_code text,
  started_at timestamptz NOT NULL DEFAULT now(),
  finished_at timestamptz,
  created_at timestamptz NOT NULL DEFAULT now(),
  UNIQUE (user_id, id),
  FOREIGN KEY (user_id, operation_id) REFERENCES operations(user_id, id) ON DELETE CASCADE,
  FOREIGN KEY (user_id, task_id) REFERENCES tasks(user_id, id) ON DELETE CASCADE
);

CREATE TABLE media_assets (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  user_id uuid NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  origin text NOT NULL CHECK (origin IN ('user_upload','provider_output','demo','bundled_reference')),
  purpose text NOT NULL CHECK (purpose IN ('face','side','body','render_candidate','feedback','wardrobe')),
  object_key text NOT NULL UNIQUE,
  sha256 text NOT NULL CHECK (sha256 ~ '^[0-9a-f]{64}$'),
  mime_type text NOT NULL,
  byte_size bigint NOT NULL CHECK (byte_size > 0),
  width int CHECK (width IS NULL OR width > 0),
  height int CHECK (height IS NULL OR height > 0),
  state text NOT NULL CHECK (state IN ('quarantined','ready','published','deleted')),
  display_kind text NOT NULL CHECK (display_kind IN
    ('original','generated_reference','effect_example','style_reference')),
  provider_invocation_id uuid,
  created_at timestamptz NOT NULL DEFAULT now(),
  deleted_at timestamptz,
  UNIQUE (user_id, id),
  FOREIGN KEY (user_id, provider_invocation_id)
    REFERENCES provider_invocations(user_id, id) ON DELETE CASCADE,
  CHECK (origin <> 'provider_output' OR provider_invocation_id IS NOT NULL),
  CHECK (origin <> 'demo' OR display_kind = 'effect_example'),
  CHECK (purpose <> 'render_candidate' OR state IN ('quarantined','published','deleted')),
  CHECK (origin <> 'provider_output' OR state <> 'published' OR mime_type = 'image/jpeg'),
  CHECK (origin <> 'user_upload' OR mime_type IN ('image/jpeg','image/png'))
);

CREATE TABLE upload_intents (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  user_id uuid NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  purpose text NOT NULL CHECK (purpose IN ('face','side','body','feedback','wardrobe')),
  mime_type text NOT NULL CHECK (mime_type IN ('image/jpeg','image/png')),
  byte_size bigint NOT NULL CHECK (byte_size > 0),
  sha256 text NOT NULL CHECK (sha256 ~ '^[0-9a-f]{64}$'),
  object_key text NOT NULL UNIQUE,
  status text NOT NULL CHECK (status IN ('pending','completed','expired')),
  expires_at timestamptz NOT NULL DEFAULT (now() + interval '15 minutes'),
  completed_media_asset_id uuid,
  version int NOT NULL DEFAULT 1 CHECK (version > 0),
  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now(),
  UNIQUE (user_id, id),
  FOREIGN KEY (user_id, completed_media_asset_id)
    REFERENCES media_assets(user_id, id) ON DELETE CASCADE,
  CHECK (status <> 'completed' OR completed_media_asset_id IS NOT NULL)
);

CREATE TABLE object_gc_jobs (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  user_id uuid NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  object_key text NOT NULL UNIQUE,
  reason text NOT NULL DEFAULT 'deleted',
  status text NOT NULL CHECK (status IN ('pending','succeeded','failed')),
  available_at timestamptz NOT NULL DEFAULT now(),
  attempt int NOT NULL DEFAULT 0 CHECK (attempt >= 0),
  last_error text,
  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now(),
  UNIQUE (user_id, id)
);

CREATE TABLE idempotency_keys (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  user_id uuid NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  key text NOT NULL CHECK (char_length(key) BETWEEN 1 AND 128),
  request_fingerprint text NOT NULL CHECK (request_fingerprint ~ '^[0-9a-f]{64}$'),
  status text NOT NULL CHECK (status IN ('in_progress','completed')),
  response_status int,
  response_body jsonb,
  scope text,
  resource_id uuid,
  expires_at timestamptz NOT NULL,
  created_at timestamptz NOT NULL DEFAULT now(),
  UNIQUE (user_id, id),
  UNIQUE (user_id, key),
  CHECK (
    (status = 'completed' AND response_status IS NOT NULL AND response_body IS NOT NULL)
    OR status = 'in_progress'
  )
);

CREATE TABLE billing_wallets (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  user_id uuid NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  credits int NOT NULL DEFAULT 0 CHECK (credits >= 0),
  welcome_analysis_used boolean NOT NULL DEFAULT false,
  welcome_plan_set_used boolean NOT NULL DEFAULT false,
  version int NOT NULL DEFAULT 1 CHECK (version > 0),
  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now(),
  UNIQUE (user_id, id),
  UNIQUE (user_id)
);

CREATE TABLE photo_sets (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  user_id uuid NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  profile_snapshot jsonb NOT NULL,
  content_hash text NOT NULL CHECK (content_hash ~ '^[0-9a-f]{64}$'),
  schema_version text NOT NULL,
  created_at timestamptz NOT NULL DEFAULT now(),
  UNIQUE (user_id, id),
  UNIQUE (user_id, content_hash)
);

CREATE TABLE photo_set_items (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  user_id uuid NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  photo_set_id uuid NOT NULL,
  role text NOT NULL CHECK (role IN ('face','side','body')),
  media_asset_id uuid NOT NULL,
  created_at timestamptz NOT NULL DEFAULT now(),
  UNIQUE (user_id, id),
  UNIQUE (photo_set_id, role),
  UNIQUE (photo_set_id, media_asset_id),
  UNIQUE (user_id, photo_set_id, role),
  UNIQUE (user_id, photo_set_id, media_asset_id),
  FOREIGN KEY (user_id, photo_set_id) REFERENCES photo_sets(user_id, id) ON DELETE CASCADE,
  FOREIGN KEY (user_id, media_asset_id) REFERENCES media_assets(user_id, id) ON DELETE CASCADE
);

CREATE TABLE analysis_runs (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  user_id uuid NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  photo_set_id uuid NOT NULL,
  operation_id uuid NOT NULL,
  input_hash text NOT NULL CHECK (input_hash ~ '^[0-9a-f]{64}$'),
  profile_snapshot jsonb NOT NULL,
  analyzer_schema_version text NOT NULL,
  quality_policy_version text NOT NULL,
  provider_invocation_id uuid,
  outcome text CHECK (outcome IN ('published','rejected','failed')),
  report_id uuid,
  created_at timestamptz NOT NULL DEFAULT now(),
  finished_at timestamptz,
  UNIQUE (user_id, id),
  UNIQUE (user_id, input_hash),
  FOREIGN KEY (user_id, photo_set_id) REFERENCES photo_sets(user_id, id) ON DELETE CASCADE,
  FOREIGN KEY (user_id, operation_id) REFERENCES operations(user_id, id) ON DELETE CASCADE,
  FOREIGN KEY (user_id, provider_invocation_id) REFERENCES provider_invocations(user_id, id) ON DELETE CASCADE
);

CREATE TABLE quality_evaluations (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  user_id uuid NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  subject_type text NOT NULL CHECK (subject_type IN ('report','plan_set','render_candidate')),
  subject_id uuid NOT NULL,
  render_candidate_subject_id uuid GENERATED ALWAYS AS (
    CASE WHEN subject_type = 'render_candidate' THEN subject_id END
  ) STORED,
  policy_version text NOT NULL,
  decision text NOT NULL CHECK (decision IN ('pass','retry','reject','error')),
  reason_codes text[] NOT NULL DEFAULT '{}',
  internal_scores jsonb NOT NULL DEFAULT '{}',
  evaluator_invocation_id uuid,
  created_at timestamptz NOT NULL DEFAULT now(),
  UNIQUE (user_id, id),
  FOREIGN KEY (user_id, evaluator_invocation_id)
    REFERENCES provider_invocations(user_id, id) ON DELETE CASCADE
);

CREATE TABLE reports (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  user_id uuid NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  photo_set_id uuid NOT NULL,
  profile_snapshot jsonb NOT NULL,
  impression_tags text[] NOT NULL,
  priority_title text NOT NULL,
  priority_copy text NOT NULL,
  hero_asset_id uuid NOT NULL,
  schema_version text NOT NULL,
  content_hash text NOT NULL CHECK (content_hash ~ '^[0-9a-f]{64}$'),
  provider_invocation_id uuid NOT NULL,
  quality_evaluation_id uuid NOT NULL,
  created_at timestamptz NOT NULL DEFAULT now(),
  UNIQUE (user_id, id),
  UNIQUE (user_id, content_hash),
  FOREIGN KEY (user_id, photo_set_id) REFERENCES photo_sets(user_id, id) ON DELETE CASCADE,
  FOREIGN KEY (user_id, hero_asset_id) REFERENCES media_assets(user_id, id) ON DELETE CASCADE,
  FOREIGN KEY (user_id, provider_invocation_id) REFERENCES provider_invocations(user_id, id) ON DELETE CASCADE,
  FOREIGN KEY (user_id, quality_evaluation_id) REFERENCES quality_evaluations(user_id, id) ON DELETE CASCADE
);

CREATE TABLE report_findings (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  user_id uuid NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  report_id uuid NOT NULL,
  category text NOT NULL CHECK (category IN ('hair','makeup','outfit','color')),
  priority smallint NOT NULL CHECK (priority BETWEEN 1 AND 3),
  label text NOT NULL,
  visible_observation text NOT NULL,
  recommendation text NOT NULL,
  source_photo_item_id uuid NOT NULL,
  anchor_x double precision NOT NULL CHECK (anchor_x BETWEEN 0 AND 1),
  anchor_y double precision NOT NULL CHECK (anchor_y BETWEEN 0 AND 1),
  anchor_w double precision NOT NULL CHECK (anchor_w > 0 AND anchor_w <= 1 AND anchor_x + anchor_w <= 1),
  anchor_h double precision NOT NULL CHECK (anchor_h > 0 AND anchor_h <= 1 AND anchor_y + anchor_h <= 1),
  confidence double precision NOT NULL CHECK (confidence BETWEEN 0 AND 1),
  position smallint NOT NULL CHECK (position > 0),
  created_at timestamptz NOT NULL DEFAULT now(),
  UNIQUE (user_id, id),
  UNIQUE (user_id, report_id, position),
  FOREIGN KEY (user_id, report_id) REFERENCES reports(user_id, id) ON DELETE CASCADE,
  FOREIGN KEY (user_id, source_photo_item_id) REFERENCES photo_set_items(user_id, id) ON DELETE CASCADE
);

ALTER TABLE analysis_runs
  ADD CONSTRAINT analysis_runs_report_owner_fk
  FOREIGN KEY (user_id, report_id) REFERENCES reports(user_id, id) ON DELETE CASCADE;

ALTER TABLE user_profiles
  ADD CONSTRAINT user_profiles_current_report_fk
  FOREIGN KEY (current_report_id) REFERENCES reports(id) ON DELETE SET NULL;
ALTER TABLE user_profiles
  ADD CONSTRAINT user_profiles_current_report_owner_fk
  FOREIGN KEY (user_id, current_report_id) REFERENCES reports(user_id, id)
  DEFERRABLE INITIALLY DEFERRED;

CREATE TABLE plan_sets (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  user_id uuid NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  report_id uuid NOT NULL,
  profile_snapshot jsonb NOT NULL,
  scene text NOT NULL CHECK (scene IN ('general','interview','wedding','date','daily','gathering')),
  scene_brief jsonb NOT NULL,
  brief_hash text NOT NULL CHECK (brief_hash ~ '^[0-9a-f]{64}$'),
  planner_schema_version text NOT NULL,
  style_rule_version text NOT NULL,
  provider_invocation_id uuid NOT NULL,
  quality_evaluation_id uuid NOT NULL,
  created_at timestamptz NOT NULL DEFAULT now(),
  UNIQUE (user_id, id),
  UNIQUE (user_id, report_id, scene, brief_hash, planner_schema_version),
  FOREIGN KEY (user_id, report_id) REFERENCES reports(user_id, id) ON DELETE CASCADE,
  FOREIGN KEY (user_id, provider_invocation_id) REFERENCES provider_invocations(user_id, id) ON DELETE CASCADE,
  FOREIGN KEY (user_id, quality_evaluation_id) REFERENCES quality_evaluations(user_id, id) ON DELETE CASCADE
);

CREATE TABLE plan_variants (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  user_id uuid NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  plan_set_id uuid NOT NULL,
  slot smallint NOT NULL CHECK (slot BETWEEN 1 AND 3),
  key text NOT NULL CHECK (key IN ('sharp','warm','natural')),
  name text NOT NULL CHECK (length(btrim(name)) BETWEEN 1 AND 80),
  descriptor text NOT NULL CHECK (length(btrim(descriptor)) BETWEEN 1 AND 160),
  rationale text NOT NULL CHECK (length(btrim(rationale)) BETWEEN 1 AND 240),
  recommended boolean NOT NULL,
  outcome_tags text[] NOT NULL,
  difference_tags text[] NOT NULL,
  created_at timestamptz NOT NULL DEFAULT now(),
  UNIQUE (user_id, id),
  UNIQUE (user_id, id, plan_set_id),
  UNIQUE (user_id, plan_set_id, slot),
  UNIQUE (user_id, plan_set_id, key),
  FOREIGN KEY (user_id, plan_set_id)
    REFERENCES plan_sets(user_id, id) ON DELETE CASCADE DEFERRABLE INITIALLY DEFERRED
);

CREATE TABLE plan_steps (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  user_id uuid NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  plan_variant_id uuid NOT NULL,
  category text NOT NULL CHECK (category IN ('hair','makeup','outfit')),
  action text NOT NULL CHECK (action IN ('keep','adjust')),
  title text NOT NULL CHECK (length(btrim(title)) BETWEEN 1 AND 120),
  summary text NOT NULL CHECK (length(btrim(summary)) BETWEEN 1 AND 240),
  details jsonb NOT NULL,
  position smallint NOT NULL CHECK (position BETWEEN 1 AND 3),
  created_at timestamptz NOT NULL DEFAULT now(),
  UNIQUE (user_id, id),
  UNIQUE (user_id, plan_variant_id, category),
  UNIQUE (user_id, plan_variant_id, position),
  FOREIGN KEY (user_id, plan_variant_id)
    REFERENCES plan_variants(user_id, id) ON DELETE CASCADE DEFERRABLE INITIALLY DEFERRED
);

CREATE TABLE plan_step_groundings (
  user_id uuid NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  plan_step_id uuid NOT NULL,
  source_type text NOT NULL CHECK (source_type IN (
    'report_finding','scene_answer','profile_preference','style_rule','feedback_memory'
  )),
  source_id text NOT NULL CHECK (length(btrim(source_id)) BETWEEN 1 AND 200),
  reason text NOT NULL CHECK (length(btrim(reason)) BETWEEN 1 AND 240),
  PRIMARY KEY (user_id, plan_step_id, source_type, source_id),
  FOREIGN KEY (user_id, plan_step_id)
    REFERENCES plan_steps(user_id, id) ON DELETE CASCADE DEFERRABLE INITIALLY DEFERRED
);

CREATE TABLE render_specs (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  user_id uuid NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  plan_variant_id uuid NOT NULL,
  source_photo_set_id uuid NOT NULL,
  schema_version text NOT NULL,
  spec jsonb NOT NULL,
  content_hash text NOT NULL CHECK (content_hash ~ '^[0-9a-f]{64}$'),
  created_at timestamptz NOT NULL DEFAULT now(),
  UNIQUE (user_id, id),
  UNIQUE (user_id, plan_variant_id),
  FOREIGN KEY (user_id, plan_variant_id)
    REFERENCES plan_variants(user_id, id) ON DELETE CASCADE DEFERRABLE INITIALLY DEFERRED,
  FOREIGN KEY (user_id, source_photo_set_id)
    REFERENCES photo_sets(user_id, id) ON DELETE CASCADE DEFERRABLE INITIALLY DEFERRED
);

CREATE TABLE render_heads (
  user_id uuid NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  plan_variant_id uuid NOT NULL,
  generation integer NOT NULL DEFAULT 0 CHECK (generation >= 0),
  current_publication_id uuid,
  version bigint NOT NULL DEFAULT 0 CHECK (version >= 0),
  PRIMARY KEY (user_id, plan_variant_id),
  FOREIGN KEY (user_id, plan_variant_id)
    REFERENCES plan_variants(user_id, id) ON DELETE CASCADE
);

CREATE TABLE render_runs (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  user_id uuid NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  plan_variant_id uuid NOT NULL,
  render_spec_id uuid NOT NULL,
  generation integer NOT NULL CHECK (generation > 0),
  operation_id uuid NOT NULL,
  candidate_limit smallint NOT NULL DEFAULT 1 CHECK (candidate_limit IN (1, 2)),
  routing_policy_version text NOT NULL,
  quality_policy_version text NOT NULL,
  outcome text CHECK (outcome IN ('published','unavailable','failed','superseded')),
  created_at timestamptz NOT NULL DEFAULT now(),
  finished_at timestamptz,
  UNIQUE (user_id, id),
  UNIQUE (user_id, plan_variant_id, generation),
  UNIQUE (user_id, plan_variant_id, generation, id),
  FOREIGN KEY (user_id, plan_variant_id)
    REFERENCES plan_variants(user_id, id) ON DELETE CASCADE,
  FOREIGN KEY (user_id, render_spec_id)
    REFERENCES render_specs(user_id, id) ON DELETE CASCADE,
  FOREIGN KEY (user_id, operation_id)
    REFERENCES operations(user_id, id) ON DELETE CASCADE
);

CREATE TABLE render_candidates (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  user_id uuid NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  render_run_id uuid NOT NULL,
  ordinal smallint NOT NULL CHECK (ordinal IN (1, 2)),
  asset_id uuid NOT NULL,
  provider_invocation_id uuid NOT NULL,
  created_at timestamptz NOT NULL DEFAULT now(),
  UNIQUE (user_id, id),
  UNIQUE (user_id, id, asset_id),
  UNIQUE (user_id, render_run_id, ordinal),
  UNIQUE (user_id, render_run_id, id),
  UNIQUE (user_id, asset_id),
  FOREIGN KEY (user_id, render_run_id)
    REFERENCES render_runs(user_id, id) ON DELETE CASCADE,
  FOREIGN KEY (user_id, asset_id)
    REFERENCES media_assets(user_id, id) ON DELETE CASCADE,
  FOREIGN KEY (user_id, provider_invocation_id)
    REFERENCES provider_invocations(user_id, id) ON DELETE CASCADE
);

ALTER TABLE quality_evaluations
  ADD CONSTRAINT quality_evaluations_render_candidate_fk
  FOREIGN KEY (user_id, render_candidate_subject_id)
    REFERENCES render_candidates(user_id, id) ON DELETE CASCADE;

CREATE TABLE render_publications (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  user_id uuid NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  plan_variant_id uuid NOT NULL,
  render_run_id uuid NOT NULL,
  candidate_id uuid NOT NULL,
  quality_evaluation_id uuid NOT NULL,
  generation integer NOT NULL CHECK (generation > 0),
  created_at timestamptz NOT NULL DEFAULT now(),
  UNIQUE (user_id, id),
  UNIQUE (user_id, id, plan_variant_id),
  UNIQUE (user_id, plan_variant_id, id),
  UNIQUE (user_id, render_run_id),
  UNIQUE (user_id, candidate_id),
  UNIQUE (user_id, id, render_run_id, candidate_id, generation),
  FOREIGN KEY (user_id, plan_variant_id, generation, render_run_id)
    REFERENCES render_runs(user_id, plan_variant_id, generation, id) ON DELETE CASCADE,
  FOREIGN KEY (user_id, render_run_id, candidate_id)
    REFERENCES render_candidates(user_id, render_run_id, id) ON DELETE CASCADE,
  FOREIGN KEY (user_id, quality_evaluation_id)
    REFERENCES quality_evaluations(user_id, id) ON DELETE CASCADE
);

ALTER TABLE render_heads
  ADD CONSTRAINT render_heads_current_publication_fk
  FOREIGN KEY (current_publication_id) REFERENCES render_publications(id) ON DELETE SET NULL;
ALTER TABLE render_heads
  ADD CONSTRAINT render_heads_current_publication_owner_fk
  FOREIGN KEY (user_id, plan_variant_id, current_publication_id)
    REFERENCES render_publications(user_id, plan_variant_id, id)
    DEFERRABLE INITIALLY DEFERRED;

CREATE TABLE plan_selections (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  user_id uuid NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  plan_set_id uuid NOT NULL,
  plan_variant_id uuid NOT NULL,
  render_publication_id uuid,
  idempotency_key text NOT NULL,
  request_hash text NOT NULL,
  created_at timestamptz NOT NULL DEFAULT now(),
  UNIQUE (user_id, id),
  UNIQUE (user_id, id, plan_set_id),
  UNIQUE (user_id, idempotency_key),
  FOREIGN KEY (user_id, plan_set_id) REFERENCES plan_sets(user_id, id) ON DELETE CASCADE,
  FOREIGN KEY (user_id, plan_variant_id, plan_set_id)
    REFERENCES plan_variants(user_id, id, plan_set_id) ON DELETE CASCADE,
  FOREIGN KEY (user_id, render_publication_id, plan_variant_id)
    REFERENCES render_publications(user_id, id, plan_variant_id) ON DELETE CASCADE
);

CREATE UNIQUE INDEX plan_selections_natural_uniq
ON plan_selections (
  user_id, plan_set_id, plan_variant_id,
  coalesce(render_publication_id, '00000000-0000-0000-0000-000000000000'::uuid)
);

CREATE TABLE executions (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  user_id uuid NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  selection_id uuid NOT NULL,
  state text NOT NULL CHECK (state IN ('planned','active','completed','abandoned')),
  version integer NOT NULL DEFAULT 1 CHECK (version > 0),
  idempotency_key text NOT NULL,
  request_hash text NOT NULL,
  started_at timestamptz,
  completed_at timestamptz,
  abandoned_at timestamptz,
  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now(),
  UNIQUE (user_id, id),
  UNIQUE (user_id, id, selection_id),
  UNIQUE (user_id, selection_id),
  UNIQUE (user_id, idempotency_key),
  FOREIGN KEY (user_id, selection_id) REFERENCES plan_selections(user_id, id) ON DELETE CASCADE
);

CREATE TABLE execution_steps (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  user_id uuid NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  execution_id uuid NOT NULL,
  source_plan_step_id uuid NOT NULL,
  category text NOT NULL CHECK (category IN ('hair','makeup','outfit')),
  action text NOT NULL CHECK (action IN ('keep','adjust')),
  title text NOT NULL,
  summary text NOT NULL,
  details jsonb NOT NULL,
  position integer NOT NULL CHECK (position > 0),
  created_at timestamptz NOT NULL DEFAULT now(),
  UNIQUE (user_id, id),
  UNIQUE (user_id, execution_id, source_plan_step_id),
  UNIQUE (user_id, execution_id, position),
  FOREIGN KEY (user_id, execution_id) REFERENCES executions(user_id, id) ON DELETE CASCADE,
  FOREIGN KEY (user_id, source_plan_step_id) REFERENCES plan_steps(user_id, id) ON DELETE CASCADE
);

CREATE TABLE execution_events (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  user_id uuid NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  execution_id uuid NOT NULL,
  client_event_id text NOT NULL,
  request_hash text NOT NULL,
  event_type text NOT NULL CHECK (event_type IN
    ('started','step_completed','step_reopened','completed','abandoned')),
  execution_step_id uuid,
  occurred_at timestamptz NOT NULL,
  created_at timestamptz NOT NULL DEFAULT now(),
  UNIQUE (user_id, id),
  UNIQUE (user_id, execution_id, client_event_id),
  FOREIGN KEY (user_id, execution_id) REFERENCES executions(user_id, id) ON DELETE CASCADE,
  FOREIGN KEY (user_id, execution_step_id) REFERENCES execution_steps(user_id, id) ON DELETE CASCADE,
  CHECK (
    (event_type IN ('step_completed','step_reopened') AND execution_step_id IS NOT NULL)
    OR
    (event_type NOT IN ('step_completed','step_reopened') AND execution_step_id IS NULL)
  )
);

CREATE TABLE generation_feedback (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  user_id uuid NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  publication_id uuid NOT NULL,
  render_run_id uuid NOT NULL,
  candidate_id uuid NOT NULL,
  asset_id uuid NOT NULL,
  generation integer NOT NULL CHECK (generation > 0),
  tags text[] NOT NULL CHECK (cardinality(tags) BETWEEN 1 AND 6),
  comment text NOT NULL DEFAULT '' CHECK (char_length(comment) <= 500),
  media_asset_id uuid,
  idempotency_key text NOT NULL,
  request_hash text NOT NULL,
  created_at timestamptz NOT NULL DEFAULT now(),
  UNIQUE (user_id, id),
  UNIQUE (user_id, idempotency_key),
  FOREIGN KEY (user_id, publication_id, render_run_id, candidate_id, generation)
    REFERENCES render_publications(user_id, id, render_run_id, candidate_id, generation) ON DELETE CASCADE,
  FOREIGN KEY (user_id, candidate_id, asset_id)
    REFERENCES render_candidates(user_id, id, asset_id) ON DELETE CASCADE,
  FOREIGN KEY (user_id, media_asset_id) REFERENCES media_assets(user_id, id) ON DELETE CASCADE
);

CREATE TABLE execution_feedback (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  user_id uuid NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  execution_id uuid NOT NULL,
  selection_id uuid NOT NULL,
  plan_set_id uuid NOT NULL,
  tags text[] NOT NULL CHECK (cardinality(tags) BETWEEN 1 AND 5),
  comment text NOT NULL DEFAULT '' CHECK (char_length(comment) <= 500),
  media_asset_id uuid,
  preference jsonb,
  idempotency_key text NOT NULL,
  request_hash text NOT NULL,
  created_at timestamptz NOT NULL DEFAULT now(),
  UNIQUE (user_id, id),
  UNIQUE (user_id, execution_id),
  UNIQUE (user_id, idempotency_key),
  FOREIGN KEY (user_id, execution_id, selection_id)
    REFERENCES executions(user_id, id, selection_id) ON DELETE CASCADE,
  FOREIGN KEY (user_id, selection_id, plan_set_id)
    REFERENCES plan_selections(user_id, id, plan_set_id) ON DELETE CASCADE,
  FOREIGN KEY (user_id, media_asset_id) REFERENCES media_assets(user_id, id) ON DELETE CASCADE
);

CREATE TABLE billing_reservations (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  user_id uuid NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  operation_id uuid NOT NULL,
  kind text NOT NULL,
  units int NOT NULL CHECK (units > 0),
  status text NOT NULL CHECK (status IN ('reserved','settled','refunded')),
  result_type text,
  result_id uuid,
  version int NOT NULL DEFAULT 1 CHECK (version > 0),
  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now(),
  UNIQUE (user_id, id),
  UNIQUE (user_id, operation_id),
  FOREIGN KEY (user_id, operation_id) REFERENCES operations(user_id, id) ON DELETE CASCADE,
  CHECK (
    (status = 'settled' AND result_type IS NOT NULL AND result_id IS NOT NULL)
    OR status <> 'settled'
  )
);

CREATE TABLE billing_ledger (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  user_id uuid NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  delta int NOT NULL,
  reason text NOT NULL CHECK (reason IN ('reserve','settle','refund','welcome','purchase')),
  reference_type text NOT NULL,
  reference_id uuid NOT NULL,
  operation_id uuid,
  created_at timestamptz NOT NULL DEFAULT now(),
  UNIQUE (user_id, id),
  UNIQUE (user_id, reason, reference_type, reference_id),
  FOREIGN KEY (user_id, operation_id) REFERENCES operations(user_id, id) ON DELETE CASCADE
);

CREATE FUNCTION assert_plan_set_complete() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
  IF (SELECT count(*) FROM plan_variants WHERE user_id=NEW.user_id AND plan_set_id=NEW.id) <> 3
     OR (SELECT count(*) FROM plan_variants WHERE user_id=NEW.user_id AND plan_set_id=NEW.id AND recommended) <> 1
     OR EXISTS (
       SELECT 1 FROM plan_variants v
       WHERE v.user_id=NEW.user_id AND v.plan_set_id=NEW.id
         AND (
           (SELECT count(*) FROM plan_steps s WHERE s.user_id=v.user_id AND s.plan_variant_id=v.id) <> 3
           OR (SELECT count(DISTINCT s.category) FROM plan_steps s WHERE s.user_id=v.user_id AND s.plan_variant_id=v.id) <> 3
           OR (SELECT count(*) FROM render_specs r WHERE r.user_id=v.user_id AND r.plan_variant_id=v.id) <> 1
         )
     )
     OR EXISTS (
       SELECT 1
       FROM plan_steps s
       JOIN plan_variants v ON v.user_id=s.user_id AND v.id=s.plan_variant_id
       WHERE v.user_id=NEW.user_id AND v.plan_set_id=NEW.id
         AND NOT EXISTS (
           SELECT 1 FROM plan_step_groundings g
           WHERE g.user_id=s.user_id AND g.plan_step_id=s.id
         )
     )
  THEN
    RAISE EXCEPTION 'incomplete plan set %', NEW.id USING ERRCODE='23514';
  END IF;
  RETURN NEW;
END $$;

CREATE CONSTRAINT TRIGGER plan_sets_complete
  AFTER INSERT ON plan_sets
  DEFERRABLE INITIALLY DEFERRED
  FOR EACH ROW EXECUTE FUNCTION assert_plan_set_complete();

CREATE TRIGGER photo_sets_immutable BEFORE UPDATE ON photo_sets
  FOR EACH ROW EXECUTE FUNCTION reject_immutable_update();
CREATE TRIGGER photo_set_items_immutable BEFORE UPDATE ON photo_set_items
  FOR EACH ROW EXECUTE FUNCTION reject_immutable_update();
CREATE TRIGGER reports_immutable BEFORE UPDATE ON reports
  FOR EACH ROW EXECUTE FUNCTION reject_immutable_update();
CREATE TRIGGER report_findings_immutable BEFORE UPDATE ON report_findings
  FOR EACH ROW EXECUTE FUNCTION reject_immutable_update();
CREATE TRIGGER quality_evaluations_immutable BEFORE UPDATE ON quality_evaluations
  FOR EACH ROW EXECUTE FUNCTION reject_immutable_update();
CREATE TRIGGER plan_sets_immutable BEFORE UPDATE ON plan_sets
  FOR EACH ROW EXECUTE FUNCTION reject_immutable_update();
CREATE TRIGGER plan_variants_immutable BEFORE UPDATE ON plan_variants
  FOR EACH ROW EXECUTE FUNCTION reject_immutable_update();
CREATE TRIGGER plan_steps_immutable BEFORE UPDATE ON plan_steps
  FOR EACH ROW EXECUTE FUNCTION reject_immutable_update();
CREATE TRIGGER plan_step_groundings_immutable BEFORE UPDATE ON plan_step_groundings
  FOR EACH ROW EXECUTE FUNCTION reject_immutable_update();
CREATE TRIGGER render_specs_immutable BEFORE UPDATE ON render_specs
  FOR EACH ROW EXECUTE FUNCTION reject_immutable_update();
CREATE TRIGGER render_candidates_immutable BEFORE UPDATE ON render_candidates
  FOR EACH ROW EXECUTE FUNCTION reject_immutable_update();
CREATE TRIGGER render_publications_immutable BEFORE UPDATE ON render_publications
  FOR EACH ROW EXECUTE FUNCTION reject_immutable_update();
CREATE TRIGGER plan_selections_immutable BEFORE UPDATE ON plan_selections
  FOR EACH ROW EXECUTE FUNCTION reject_immutable_update();
CREATE TRIGGER execution_steps_immutable BEFORE UPDATE ON execution_steps
  FOR EACH ROW EXECUTE FUNCTION reject_immutable_update();
CREATE TRIGGER execution_events_immutable BEFORE UPDATE ON execution_events
  FOR EACH ROW EXECUTE FUNCTION reject_immutable_update();
CREATE TRIGGER generation_feedback_immutable BEFORE UPDATE ON generation_feedback
  FOR EACH ROW EXECUTE FUNCTION reject_immutable_update();
CREATE TRIGGER execution_feedback_immutable BEFORE UPDATE ON execution_feedback
  FOR EACH ROW EXECUTE FUNCTION reject_immutable_update();
CREATE TRIGGER billing_ledger_immutable BEFORE UPDATE ON billing_ledger
  FOR EACH ROW EXECUTE FUNCTION reject_immutable_update();

CREATE INDEX tasks_claim_idx
  ON tasks (status, available_at, priority DESC, created_at)
  WHERE status IN ('queued','retry_wait');
CREATE INDEX tasks_lease_expiry_idx
  ON tasks (lease_expires_at)
  WHERE status = 'leased';
CREATE INDEX operations_user_status_idx
  ON operations (user_id, status);
CREATE INDEX provider_invocations_operation_idx
  ON provider_invocations (user_id, operation_id);
CREATE INDEX provider_invocations_task_idx
  ON provider_invocations (user_id, task_id);
CREATE INDEX upload_intents_cleanup_idx
  ON upload_intents (created_at)
  WHERE completed_media_asset_id IS NULL;
CREATE INDEX plan_sets_report_scene_created_idx
  ON plan_sets (user_id, report_id, scene, created_at DESC, id DESC);
CREATE INDEX plan_variants_set_slot_idx
  ON plan_variants (user_id, plan_set_id, slot);
CREATE INDEX plan_steps_variant_position_idx
  ON plan_steps (user_id, plan_variant_id, position);
CREATE INDEX render_runs_variant_created_idx
  ON render_runs (user_id, plan_variant_id, created_at DESC);
CREATE INDEX render_candidates_run_idx
  ON render_candidates (user_id, render_run_id, ordinal);
CREATE INDEX quality_evaluations_subject_idx
  ON quality_evaluations (user_id, subject_type, subject_id, created_at DESC);
CREATE INDEX idempotency_keys_expiry_idx
  ON idempotency_keys (expires_at);
CREATE INDEX user_identities_user_idx
  ON user_identities (user_id);
CREATE INDEX user_sessions_user_idx
  ON user_sessions (user_id);
