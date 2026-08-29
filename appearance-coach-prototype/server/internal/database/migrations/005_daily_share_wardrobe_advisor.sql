ALTER TABLE media_assets DROP CONSTRAINT IF EXISTS media_assets_kind_check;
ALTER TABLE media_assets ADD CONSTRAINT media_assets_kind_check
  CHECK (kind IN ('face', 'side', 'body', 'feedback', 'outfit', 'product', 'wardrobe'));

CREATE TABLE today_plans (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  user_id uuid NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  report_id uuid REFERENCES reports(id) ON DELETE SET NULL,
  plan_date date NOT NULL DEFAULT current_date,
  context jsonb NOT NULL DEFAULT '{}'::jsonb,
  title text NOT NULL,
  summary text NOT NULL,
  image_url text NOT NULL,
  steps jsonb NOT NULL DEFAULT '[]'::jsonb,
  active boolean NOT NULL DEFAULT false,
  feedback text NOT NULL DEFAULT '',
  regenerate_count int NOT NULL DEFAULT 0,
  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now(),
  UNIQUE(user_id, plan_date)
);
CREATE INDEX today_plans_user_date_idx ON today_plans(user_id, plan_date DESC);

CREATE TABLE share_cards (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  user_id uuid NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  source_type text NOT NULL CHECK (source_type IN ('plan', 'today')),
  source_id uuid NOT NULL,
  token text NOT NULL UNIQUE DEFAULT encode(gen_random_bytes(18), 'hex'),
  snapshot jsonb NOT NULL,
  include_photo boolean NOT NULL DEFAULT false,
  revoked_at timestamptz,
  expires_at timestamptz NOT NULL DEFAULT now() + interval '30 days',
  created_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX share_cards_user_created_idx ON share_cards(user_id, created_at DESC);

CREATE TABLE wardrobe_items (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  user_id uuid NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  media_id uuid REFERENCES media_assets(id) ON DELETE SET NULL,
  name text NOT NULL,
  category text NOT NULL CHECK (category IN ('top', 'bottom', 'outer', 'shoes', 'bag')),
  color text NOT NULL,
  season text NOT NULL DEFAULT 'all',
  formality text NOT NULL DEFAULT 'proper',
  scenes text[] NOT NULL DEFAULT ARRAY['daily']::text[],
  image_url text NOT NULL,
  favorite boolean NOT NULL DEFAULT false,
  wear_count int NOT NULL DEFAULT 0,
  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX wardrobe_items_user_category_idx ON wardrobe_items(user_id, category, updated_at DESC);

CREATE TABLE wardrobe_outfits (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  user_id uuid NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  title text NOT NULL,
  note text NOT NULL,
  context jsonb NOT NULL DEFAULT '{}'::jsonb,
  item_ids uuid[] NOT NULL,
  worn_at timestamptz,
  created_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX wardrobe_outfits_user_created_idx ON wardrobe_outfits(user_id, created_at DESC);

CREATE TABLE advisor_conversations (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  user_id uuid NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  title text NOT NULL DEFAULT '私人顾问',
  context jsonb NOT NULL DEFAULT '{}'::jsonb,
  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX advisor_conversations_user_updated_idx ON advisor_conversations(user_id, updated_at DESC);

CREATE TABLE advisor_messages (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  conversation_id uuid NOT NULL REFERENCES advisor_conversations(id) ON DELETE CASCADE,
  user_id uuid NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  role text NOT NULL CHECK (role IN ('user', 'assistant')),
  content text NOT NULL,
  created_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX advisor_messages_conversation_idx ON advisor_messages(conversation_id, created_at);

CREATE TABLE advisor_actions (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  message_id uuid NOT NULL REFERENCES advisor_messages(id) ON DELETE CASCADE,
  user_id uuid NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  kind text NOT NULL,
  label text NOT NULL,
  payload jsonb NOT NULL DEFAULT '{}'::jsonb,
  applied boolean NOT NULL DEFAULT false,
  created_at timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE product_events (
  id bigserial PRIMARY KEY,
  user_id uuid REFERENCES users(id) ON DELETE SET NULL,
  name text NOT NULL,
  payload jsonb NOT NULL DEFAULT '{}'::jsonb,
  created_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX product_events_name_created_idx ON product_events(name, created_at DESC);
