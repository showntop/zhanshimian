-- 次数钱包、流水、订单、用量窗口。注销用户时随 users CASCADE 清理。
CREATE TABLE billing_wallets (
  user_id uuid PRIMARY KEY REFERENCES users(id) ON DELETE CASCADE,
  credits int NOT NULL DEFAULT 0 CHECK (credits >= 0),
  welcome_analysis_used boolean NOT NULL DEFAULT false,
  welcome_plan_set_used boolean NOT NULL DEFAULT false,
  updated_at timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE billing_ledger (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  user_id uuid NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  delta int NOT NULL,
  reason text NOT NULL CHECK (reason IN ('reserve', 'refund', 'welcome', 'purchase')),
  action text NOT NULL DEFAULT '',
  ref_type text NOT NULL,
  ref_id text NOT NULL,
  created_at timestamptz NOT NULL DEFAULT now(),
  UNIQUE (ref_type, ref_id, reason)
);
CREATE INDEX billing_ledger_user_idx ON billing_ledger(user_id, created_at DESC);

CREATE TABLE billing_orders (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  user_id uuid NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  sku_id text NOT NULL,
  product_id text NOT NULL,
  credits int NOT NULL CHECK (credits > 0),
  amount_fen int NOT NULL CHECK (amount_fen > 0),
  out_trade_no text NOT NULL UNIQUE,
  wx_order_id text NOT NULL DEFAULT '',
  status text NOT NULL DEFAULT 'created' CHECK (status IN ('created', 'paid', 'fulfilled', 'refunded', 'closed')),
  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX billing_orders_user_idx ON billing_orders(user_id, created_at DESC);

CREATE TABLE billing_usage (
  user_id uuid NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  bucket timestamptz NOT NULL,
  action text NOT NULL,
  count int NOT NULL DEFAULT 0 CHECK (count >= 0),
  PRIMARY KEY (user_id, bucket, action)
);
