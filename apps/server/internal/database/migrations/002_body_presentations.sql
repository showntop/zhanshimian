-- 3D 形象 Lite：身体环绕展示。视频与抽帧只存 COS object key，
-- 签名 URL 一律在读取时投影（敏感信息红线：库不存签名 URL）。
CREATE TABLE body_presentations (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  user_id uuid NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  body_media_id uuid NOT NULL REFERENCES media_assets(id),
  face_media_id uuid NOT NULL REFERENCES media_assets(id),
  representation text NOT NULL DEFAULT 'orbit'
    CHECK (representation IN ('orbit', 'mesh')),
  video_storage_key text NOT NULL DEFAULT '',
  duration_ms int NOT NULL DEFAULT 0,
  frames jsonb NOT NULL DEFAULT '[]',
  mesh jsonb,
  provider_version text NOT NULL DEFAULT '',
  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX body_presentations_user_created_idx
  ON body_presentations(user_id, created_at DESC);
