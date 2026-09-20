package postgres

import (
	"context"
	"encoding/json"

	"github.com/zhanshimian/server/internal/domain"
	"github.com/zhanshimian/server/internal/service/daily"
)

// 知识事实检索（方案 §2.4）：纯 SQL 结构化过滤，不做向量——
// 事实量 < 1000 时足够，知识库大了再上 pgvector。
//
// 过滤顺序：reviewed=true（未确认不参与生成）→ 排除近期已推 → 季节。
// 基因匹配在 service 层做（JSONB 条件判断放在 Go 里可读且易测）。

var _ daily.KnowledgeStore = (*Store)(nil)

const knowledgeFactSelectSQL = `
	SELECT id::text, domain, fact, boundary, gene_fit, season, source, reviewed, created_at
	FROM knowledge_fact`

func (s *Store) ListReviewedFacts(ctx context.Context, excludeIDs []string, season string) ([]domain.KnowledgeFact, error) {
	rows, err := s.pool.Query(ctx, knowledgeFactSelectSQL+`
		WHERE reviewed
		  AND NOT (id = ANY($1::uuid[]))
		  AND (cardinality(season)=0 OR $2 = '' OR $2 = ANY(season) OR season IS NULL)
		ORDER BY domain, id`, normalizeIDs(excludeIDs), season)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	facts := []domain.KnowledgeFact{}
	for rows.Next() {
		fact, err := scanKnowledgeFact(rows)
		if err != nil {
			return nil, err
		}
		facts = append(facts, fact)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return facts, nil
}

type knowledgeFactScanner interface {
	Scan(dest ...any) error
}

func scanKnowledgeFact(row knowledgeFactScanner) (domain.KnowledgeFact, error) {
	var fact domain.KnowledgeFact
	var geneFit []byte
	err := row.Scan(&fact.ID, &fact.Domain, &fact.Fact, &fact.Boundary, &geneFit, &fact.Season, &fact.Source, &fact.Reviewed, &fact.CreatedAt)
	if err != nil {
		return fact, err
	}
	parsed := map[string][]string{}
	if len(geneFit) > 0 {
		if err := json.Unmarshal(geneFit, &parsed); err != nil {
			return fact, err
		}
	}
	fact.GeneFit = domain.GeneCondition(parsed)
	return fact, nil
}

// normalizeIDs 把空串与非法 uuid 剔掉：uuid[] 一旦含非法值整条查询会报 22P02。
func normalizeIDs(values []string) []string {
	out := make([]string, 0, len(values))
	for _, value := range values {
		if value == "" {
			continue
		}
		out = append(out, value)
	}
	return out
}
