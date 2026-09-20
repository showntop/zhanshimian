package daily

import (
	"context"
	"errors"
	"sort"
	"time"

	"github.com/zhanshimian/server/internal/domain"
)

// 生成审计的 outcome（方案 §2.2 generation_run）。
const (
	outcomeAccepted = "accepted"
	outcomeRejected = "rejected"
	outcomeFallback = "fallback"
)

// 降级原因（写进 generation_run.validation.reason）。
const (
	reasonNoFacts     = "no_facts"
	reasonLLMFailed   = "llm_failed"
	reasonRejected    = "rejected"
	reasonRetryFailed = "retry_failed"
	reasonSaveFailed  = "save_failed"
	reasonPoolEmpty   = "pool_empty"
)

// fallback 降级链的最后一级：兜底池（user_id IS NULL 的 daily_content，
// 规则选品）→ 再失败走静态问候（永不空屏）。
// 对客户端而言与正常生成同构，只有 source 字段不同。
func (s *Service) fallback(ctx context.Context, userID string, genDate string, snapshot *pickSnapshot, reason string) (GenerateResult, error) {
	writeCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 4*time.Second)
	defer cancel()

	content, picked := s.pickFallback(writeCtx, userID, genDate, snapshot)
	if !picked {
		content = staticContent(userID, genDate)
	}
	saved, err := s.content.SaveContent(writeCtx, content)
	if err != nil {
		// 落库失败也要把内容给到客户端（不空屏），只是这条不能收下。
		s.recordFallback(writeCtx, snapshot, reason, content)
		return GenerateResult{Source: SourceFallback, Content: content}, nil
	}
	s.recordFallback(writeCtx, snapshot, reason, saved)
	return GenerateResult{Source: SourceFallback, Content: saved}, nil
}

// pickFallback 兜底池选品：排除用户已收/近期推过的 key，优先补最薄的格。
func (s *Service) pickFallback(ctx context.Context, userID string, genDate string, snapshot *pickSnapshot) (domain.DailyContent, bool) {
	pool, err := s.content.FallbackPool(ctx)
	if err != nil || len(pool) == 0 {
		return domain.DailyContent{}, false
	}
	since := s.clock.Now().AddDate(0, 0, -seenWindowDays)
	seenKeys, _ := s.content.RecentContentKeys(ctx, userID, since)
	seen := map[string]bool{}
	for _, key := range seenKeys {
		seen[key] = true
	}
	if snapshot != nil && snapshot.BucketCounts == nil {
		snapshot.BucketCounts = map[string]int{}
	}
	buckets, err := s.collections.CountCollections(ctx, userID)
	if err != nil {
		buckets = map[string]int{}
	}
	thinnest := thinnestCount(buckets)

	candidates := make([]domain.DailyContent, 0, len(pool))
	for _, item := range pool {
		if seen[item.DedupeKey] {
			continue
		}
		candidates = append(candidates, item)
	}
	if len(candidates) == 0 {
		// 池子里的都推过了：不去重，按补薄格再选一次（宁可重复也不空屏）。
		candidates = pool
	}
	// 排序：与 prepare 场景一致的分类优先（兜底海报与等待动画同一主题），
	// 再补最薄的格，最后按 key 稳定排序。
	sort.Slice(candidates, func(i, j int) bool {
		mi := snapshot != nil && candidates[i].Category == snapshot.Category
		mj := snapshot != nil && candidates[j].Category == snapshot.Category
		if mi != mj {
			return mi
		}
		gi := thinnest - (buckets[candidates[i].Category])
		gj := thinnest - (buckets[candidates[j].Category])
		if gi != gj {
			return gi > gj
		}
		return candidates[i].DedupeKey < candidates[j].DedupeKey
	})
	picked := candidates[0]
	picked.UserID = userID
	picked.GenDate = genDate
	picked.Source = SourceFallback
	picked.FactIDs = []string{}
	picked.ModelKey = ""
	return picked, true
}

// staticContent 兜底池也空时的静态问候：同结构、可收下、永不空屏。
func staticContent(userID string, genDate string) domain.DailyContent {
	return domain.DailyContent{
		UserID:   userID,
		GenDate:  genDate,
		Category: "fit",
		Topic:    "今天先改一处",
		Lead:     "今天的内容还在准备，先给你一条不会出错的。",
		FitText:  "把注意力放在一处就好：今天只调整一个细节，比一次改全身更容易坚持。",
		Why:      "每天一个具体动作，累积起来才是变化。",
		Visual: domain.ContentVisual{
			Modality: "compare",
			Spec: map[string]any{
				"left":   map[string]any{"label": "一次改全身", "tone": "#8A8F83"},
				"right":  map[string]any{"label": "先改一处", "tone": "#8E9A83", "layered": true},
				"marker": "能坚持",
			},
			Alt: "一次改全身与先改一处的对比，先改一处更容易坚持",
		},
		FactIDs:   []string{},
		Source:    SourceFallback,
		DedupeKey: "static:" + genDate,
	}
}

// record 生成审计（accepted/rejected）。写入用 WithoutCancel：
// 生成超时取消了 ctx 也要留下这条痕迹。
func (s *Service) record(ctx context.Context, snapshot pickSnapshot, output ContentOutput, problems []error, outcome string, note string) {
	if s.runs == nil {
		return
	}
	writeCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 2*time.Second)
	defer cancel()
	run := domain.GenerationRun{
		UserID:     snapshot.UserID,
		GenDate:    snapshot.GenDate,
		FactIDs:    append([]string{}, snapshot.FactIDs...),
		PromptHash: promptHash(snapshot),
		Outcome:    outcome,
		ModelKey:   output.ModelKey,
		LatencyMS:  output.LatencyMS,
	}
	if output.Topic != "" {
		run.Output = map[string]any{
			"topic": output.Topic, "lead": output.Lead,
			"fit": output.Fit, "why": output.Why, "refs": output.Refs,
		}
	}
	validation := map[string]any{
		"structure": len(problems) == 0,
		"factTrace": !hasError(problems, errTrace),
		"blacklist": !hasError(problems, errBlacklist),
	}
	if note != "" {
		validation["reason"] = note
	}
	if len(problems) > 0 {
		messages := make([]string, 0, len(problems))
		for _, problem := range problems {
			messages = append(messages, problem.Error())
		}
		validation["problems"] = messages
	}
	run.Validation = validation
	run.EstimatedCostCNY = output.EstimatedCost
	_ = s.runs.RecordRun(writeCtx, run)
}

func (s *Service) recordFallback(ctx context.Context, snapshot *pickSnapshot, reason string, content domain.DailyContent) {
	if s.runs == nil {
		return
	}
	run := domain.GenerationRun{
		UserID:   content.UserID,
		GenDate:  content.GenDate,
		FactIDs:  append([]string{}, content.FactIDs...),
		Outcome:  outcomeFallback,
		ModelKey: content.ModelKey,
		Validation: map[string]any{
			"reason":       reason,
			"fallback_key": content.DedupeKey,
			"fallback_hit": true,
		},
	}
	if snapshot != nil {
		run.PromptHash = promptHash(*snapshot)
		run.FactIDs = append([]string{}, snapshot.FactIDs...)
	}
	_ = s.runs.RecordRun(ctx, run)
}

func promptHash(snapshot pickSnapshot) string {
	return hex64(snapshot.UserID + "|" + snapshot.GenDate + "|" + snapshot.Category + "|" + snapshot.Angle)
}

func hasError(problems []error, target error) bool {
	for _, problem := range problems {
		if errors.Is(problem, target) {
			return true
		}
	}
	return false
}
