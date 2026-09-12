-- 3D 形象 Lite：身体环绕展示。视频与抽帧只存 COS object key，
-- 签名 URL 一律在读取时投影（敏感信息红线：库不存签名 URL）。

-- 公开操作允许 body_orbit 类型（与 assessment/plan_set/render 同一轮询入口）。
ALTER TABLE operations DROP CONSTRAINT operations_kind_check;
ALTER TABLE operations ADD CONSTRAINT operations_kind_check
  CHECK (kind IN ('assessment','plan_set','render','execution_feedback','body_orbit'));

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

-- 计费产物白名单加入 body_orbit（创建 3D 形象按次预扣 1 次额度）。
ALTER TABLE billing_reservations DROP CONSTRAINT billing_reservations_product_check;
ALTER TABLE billing_reservations ADD CONSTRAINT billing_reservations_product_check
  CHECK (product IN ('assessment','plan_set','render_publication','body_orbit'));

ALTER TABLE billing_ledger DROP CONSTRAINT billing_ledger_product_check;
ALTER TABLE billing_ledger ADD CONSTRAINT billing_ledger_product_check
  CHECK (product IN ('assessment','plan_set','render_publication','body_orbit','credit_pack'));

-- settle 行的复合约束：body_orbit 与 assessment/plan_set 同组（无 publication_id）。
ALTER TABLE billing_ledger DROP CONSTRAINT billing_ledger_check1;
ALTER TABLE billing_ledger ADD CONSTRAINT billing_ledger_entry_shape_check
  CHECK (
    (entry_type='reserve' AND operation_id IS NOT NULL AND publication_id IS NULL
      AND product<>'credit_pack'
      AND ((charge_source='credits' AND delta<0)
        OR (charge_source IN ('welcome_analysis','welcome_plan_set') AND delta=0)))
    OR (entry_type='settle' AND operation_id IS NOT NULL AND delta=0
      AND charge_source IN ('credits','welcome_analysis','welcome_plan_set')
      AND ((product='render_publication' AND publication_id IS NOT NULL)
        OR (product IN ('assessment','plan_set','body_orbit') AND publication_id IS NULL)))
    OR (entry_type='refund' AND operation_id IS NOT NULL AND publication_id IS NULL
      AND ((charge_source='credits' AND delta>0)
        OR (charge_source IN ('welcome_analysis','welcome_plan_set') AND delta=0)))
    OR (entry_type='purchase' AND order_id IS NOT NULL AND product='credit_pack'
      AND charge_source='purchase' AND delta>0)
  );
