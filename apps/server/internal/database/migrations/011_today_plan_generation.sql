-- Today plans get the same async look-generation lifecycle as plans: the
-- bundled stock photo stays as the immediate 风格参考, a worker renders the
-- user's own try-on in the background and fills generated_image_url.
ALTER TABLE today_plans
  ADD COLUMN generated_image_url text NOT NULL DEFAULT '',
  ADD COLUMN generated_storage_key text NOT NULL DEFAULT '',
  ADD COLUMN generation_status text NOT NULL DEFAULT 'idle'
    CHECK (generation_status IN ('idle', 'queued', 'processing', 'completed', 'failed')),
  ADD COLUMN generation_attempts int NOT NULL DEFAULT 0,
  ADD COLUMN generation_next_run_at timestamptz NOT NULL DEFAULT now(),
  ADD COLUMN generation_locked_at timestamptz,
  ADD COLUMN generation_error text NOT NULL DEFAULT '',
  ADD COLUMN look_provider text NOT NULL DEFAULT '';

CREATE INDEX today_plans_generation_claim_idx ON today_plans(generation_status, generation_next_run_at);
