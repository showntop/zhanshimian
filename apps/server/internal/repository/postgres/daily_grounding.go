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
	// 报告要点（可提升点的中性短句）：没有报告不是错误，空着继续。
	findings, err := s.readLatestFindingsText(ctx, userID)
	if err == nil {
		grounding.Findings = findings
	}
	return grounding, nil
}

// readLatestFindingsText 最近一次报告的可提升点，整理成给模型的中性短句
//（标签 + 建议）。只读不评判：报告本身已过形象顾问审，原文转述。
func (s *Store) readLatestFindingsText(ctx context.Context, userID string) ([]string, error) {
	var reportID string
	if err := s.pool.QueryRow(ctx, `
		SELECT id::text FROM reports
		WHERE user_id=$1::uuid ORDER BY created_at DESC LIMIT 1`, userID).Scan(&reportID); err != nil {
		return nil, err
	}
	rows, err := s.readReportFindings(ctx, userID, reportID)
	if err != nil {
		return nil, err
	}
	out := make([]string, 0, len(rows))
	for _, f := range rows {
		line := f.Label
		if f.Recommendation != "" {
			line += "，建议：" + f.Recommendation
		}
		if line != "" {
			out = append(out, line)
		}
	}
	return out, nil
}
