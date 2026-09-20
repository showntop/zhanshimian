package postgres

import (
	"context"
	"encoding/json"

	"github.com/zhanshimian/server/internal/domain"
	"github.com/zhanshimian/server/internal/service/daily"
)

// 生成审计：每次 LLM 调用一条（accepted / rejected / fallback），
// 供输出聚类监控与事后回溯。

var _ daily.RunStore = (*Store)(nil)

func (s *Store) RecordRun(ctx context.Context, run domain.GenerationRun) error {
	output, err := marshalJSON(run.Output)
	if err != nil {
		return err
	}
	validation, err := marshalJSON(run.Validation)
	if err != nil {
		return err
	}
	_, err = s.pool.Exec(ctx, `
		INSERT INTO generation_run(user_id, gen_date, fact_ids, prompt_hash, output, validation,
		                           outcome, model_key, latency_ms, estimated_cost_cny)
		VALUES (NULLIF($1,'')::uuid, NULLIF($2,'')::date, $3::uuid[], $4, $5, $6, $7, $8, $9, $10)`,
		run.UserID, run.GenDate, normalizeIDs(run.FactIDs), run.PromptHash, output, validation,
		run.Outcome, run.ModelKey, run.LatencyMS, run.EstimatedCostCNY)
	return err
}

func marshalJSON(value map[string]any) ([]byte, error) {
	if value == nil {
		return nil, nil
	}
	return json.Marshal(value)
}
