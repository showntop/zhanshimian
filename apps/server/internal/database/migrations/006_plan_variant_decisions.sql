-- 方案卡堆的喜欢/跳过决策：方案 tab 每张卡一次右滑（喜欢）/左滑（跳过）。
-- 每 (user, variant) 至多一行，改主意即 UPSERT（updated_at 前移），撤销即删行。
--
-- 决策参与规划指纹（brief.go planningInputFingerprint）：新决策改变
-- PlanningInputHash，下一轮生成拿到新身份——既绕开语义键去重，又把
-- 「喜欢了什么/跳过了什么」作为 decision_memory 快照喂给生成器（worker.go）。
--
-- 归属由复合外键结构性保证：决策行只能指向本人名下的方案集与方案版本，
-- 服务层不需要再做一次租户校验；注销清除（DELETE /v1/me/data）经 users
-- 级联把决策一并带走，不需要单独的清除分支。

CREATE TABLE plan_variant_decisions (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  user_id uuid NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  plan_set_id uuid NOT NULL,
  plan_variant_id uuid NOT NULL,
  decision text NOT NULL CHECK (decision IN ('like','skip')),
  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now(),
  UNIQUE (user_id, id),
  UNIQUE (user_id, plan_variant_id),
  FOREIGN KEY (user_id, plan_set_id) REFERENCES plan_sets(user_id, id) ON DELETE CASCADE,
  FOREIGN KEY (user_id, plan_variant_id) REFERENCES plan_variants(user_id, id) ON DELETE CASCADE
);

-- 规划指纹只取最近一段决策（planningDecisionLimit），按新旧排读。
CREATE INDEX plan_variant_decisions_recent_idx
  ON plan_variant_decisions (user_id, created_at DESC, id DESC);
