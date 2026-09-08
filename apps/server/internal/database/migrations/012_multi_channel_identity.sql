-- 多端身份：同一用户可绑定 wechat_miniapp / wechat_app / apple / phone 多个身份。
-- 登录时按 (provider, identifier) 找回同一 user_id，跨端数据互通。
CREATE TABLE user_identities (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  user_id uuid NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  provider text NOT NULL CHECK (provider IN ('wechat_miniapp', 'wechat_app', 'apple', 'phone')),
  identifier text NOT NULL,
  created_at timestamptz NOT NULL DEFAULT now(),
  UNIQUE(provider, identifier)
);
CREATE INDEX user_identities_user_idx ON user_identities(user_id);

-- 启动回填：存量 users.open_id 全部登记为小程序身份。迁移随服务启动执行，
-- 升级后老用户第一次用小程序 code 登录即命中身份表，不会另建账号。
INSERT INTO user_identities(user_id, provider, identifier)
SELECT id, 'wechat_miniapp', open_id FROM users
ON CONFLICT DO NOTHING;
