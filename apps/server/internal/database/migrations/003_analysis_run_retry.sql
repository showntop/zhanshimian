-- 终态(failed/rejected)的 analysis_runs 不再被 CreateOrReuseAssessment 复用:
-- 同 input_hash 重提按 StartWithTask 语义开新 run,历史终态 run 共存保留。
-- 在途(outcome IS NULL)run 仍保持每 (user_id, input_hash) 唯一,防同输入并发双跑。
ALTER TABLE analysis_runs DROP CONSTRAINT analysis_runs_user_id_input_hash_key;

CREATE UNIQUE INDEX analysis_runs_inflight_input_uidx
  ON analysis_runs(user_id, input_hash) WHERE outcome IS NULL;
