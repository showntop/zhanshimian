-- 手机号验证码。只存摘要（sha256(phone:code)），明文验证码不落库；
-- 60s 冷却按最新一条未使用记录判断，5/hour/phone 按创建时间计数。
CREATE TABLE sms_codes (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  phone text NOT NULL,
  code_digest bytea NOT NULL,
  expires_at timestamptz NOT NULL,
  used_at timestamptz,
  created_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX sms_codes_phone_created_idx ON sms_codes(phone, created_at DESC);
CREATE INDEX sms_codes_created_idx ON sms_codes(created_at);
