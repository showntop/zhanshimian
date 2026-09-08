-- 补充资料持久化：身高/职业/预算必填 + 选填体重与三围。行随需创建（PUT 幂等 upsert），
-- 缺失行表示用户尚未填写，读路径返回 null。
CREATE TABLE user_profiles (
  user_id uuid PRIMARY KEY REFERENCES users(id) ON DELETE CASCADE,
  height_cm int NOT NULL CHECK (height_cm BETWEEN 100 AND 250),
  role text NOT NULL,
  budget text NOT NULL,
  weight_kg numeric CHECK (weight_kg IS NULL OR weight_kg BETWEEN 25 AND 300),
  bust_cm numeric CHECK (bust_cm IS NULL OR bust_cm BETWEEN 40 AND 200),
  waist_cm numeric CHECK (waist_cm IS NULL OR waist_cm BETWEEN 40 AND 200),
  hip_cm numeric CHECK (hip_cm IS NULL OR hip_cm BETWEEN 40 AND 200),
  updated_at timestamptz NOT NULL DEFAULT now()
);
