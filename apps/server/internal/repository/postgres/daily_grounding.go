package postgres

import (
	"context"
	"encoding/json"

	"github.com/zhanshimian/server/internal/service/daily"
)

// 每日内容的 grounding：只取稳定特征（身高/体重），不取任何当日状态——
// 内容池里没有任何一条可以要求知道用户当天穿什么。
//
// 没有资料行不是错误：用中性基因继续，选品只会落到「不挑人」的事实上。

var _ daily.Reader = (*Store)(nil)

func (s *Store) ReadDailyGrounding(ctx context.Context, userID string) (daily.Grounding, error) {
	var grounding daily.Grounding
	var preferences []byte
	err := s.pool.QueryRow(ctx, `
		SELECT COALESCE(up.height_cm, 0), COALESCE(up.preferences, '{}'::jsonb)
		FROM user_profiles up WHERE up.user_id=$1::uuid`, userID).
		Scan(&grounding.HeightCM, &preferences)
	if err != nil {
		if isNotFound(err) {
			return daily.Grounding{}, nil
		}
		return daily.Grounding{}, err
	}
	if len(preferences) > 0 {
		parsed := map[string]any{}
		if err := json.Unmarshal(preferences, &parsed); err == nil {
			if weight, ok := parsed["weight_kg"].(float64); ok {
				value := weight
				grounding.WeightKG = &value
			}
		}
	}
	return grounding, nil
}
