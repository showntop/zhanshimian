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
	reasonNoPlanner   = "no_planner"
	reasonLLMFailed   = "llm_failed"
	reasonRejected    = "rejected"
	reasonRetryFailed = "retry_failed"
	reasonSaveFailed  = "save_failed"
	reasonPoolEmpty   = "pool_empty"
)

// fallback 降级链的最后一级：兜底池（user_id IS NULL 的 daily_content，
// 人工审过的内容）→ 再失败走静态问候（永不空屏）。
// 对客户端而言与正常生成同构，只有 source 字段不同。
// preferredCategory 是生成阶段的自报分类（可能为空）：兜底海报尽量同格。
func (s *Service) fallback(ctx context.Context, userID string, genDate string, preferredCategory string, reason string) (GenerateResult, error) {
	writeCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 4*time.Second)
	defer cancel()

	content, picked := s.pickFallback(writeCtx, userID, genDate, preferredCategory)
	if !picked {
		content = staticContent(userID, genDate)
	}
	saved, err := s.content.SaveContent(writeCtx, content)
	if err != nil {
		// 落库失败也要把内容给到客户端（不空屏），只是这条不能收下。
		s.recordFallback(writeCtx, reason, content)
		return GenerateResult{Source: SourceFallback, Content: content}, nil
	}
	s.recordFallback(writeCtx, reason, saved)
	return GenerateResult{Source: SourceFallback, Content: saved}, nil
}

// pickFallback 兜底池挑内容：排除近 30 天已推的 key；同格优先
// （自报分类与兜底海报同一主题），池子推完则不再去重（宁重复不空屏）。
func (s *Service) pickFallback(ctx context.Context, userID string, genDate string, preferredCategory string) (domain.DailyContent, bool) {
	pool, err := s.content.FallbackPool(ctx)
	if err != nil || len(pool) == 0 {
		return domain.DailyContent{}, false
	}
	since := s.clock.Now().AddDate(0, 0, -30)
	seenKeys, _ := s.content.RecentContentKeys(ctx, userID, since)
	seen := map[string]bool{}
	for _, key := range seenKeys {
		seen[key] = true
	}

	candidates := make([]domain.DailyContent, 0, len(pool))
	for _, item := range pool {
		if seen[item.DedupeKey] {
			continue
		}
		candidates = append(candidates, item)
	}
	if len(candidates) == 0 {
		// 池子里的都推过了：不去重，再选一次（宁可重复也不空屏）。
		candidates = pool
	}
	// 排序：同格优先，之后按 key 稳定排序。
	sort.Slice(candidates, func(i, j int) bool {
		mi := preferredCategory != "" && candidates[i].Category == preferredCategory
		mj := preferredCategory != "" && candidates[j].Category == preferredCategory
		if mi != mj {
			return mi
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
func (s *Service) record(ctx context.Context, gctx generateContext, output ContentOutput, problems []error, outcome string, note string) {
	if s.runs == nil {
		return
	}
	writeCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 2*time.Second)
	defer cancel()
	run := domain.GenerationRun{
		UserID:     gctx.UserID,
		GenDate:    gctx.GenDate,
		FactIDs:    append([]string{}, gctx.referenceIDs...),
		PromptHash: promptHash(gctx),
		Outcome:    outcome,
		ModelKey:   output.ModelKey,
		LatencyMS:  output.LatencyMS,
	}
	if output.Topic != "" {
		run.Output = map[string]any{
			"topic": output.Topic, "lead": output.Lead, "category": output.Category,
			"fit": output.Fit, "why": output.Why,
		}
	}
	validation := map[string]any{
		"structure": len(problems) == 0,
		"dedup":     !hasError(problems, errDuplicate),
		"blacklist": !hasError(problems, errBlacklist),
		"category":  !hasError(problems, errCategory),
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

func (s *Service) recordFallback(ctx context.Context, reason string, content domain.DailyContent) {
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
	_ = s.runs.RecordRun(ctx, run)
}

func promptHash(gctx generateContext) string {
	return hex64(gctx.UserID + "|" + gctx.GenDate + "|" + gctx.City + "|" + gctx.InterestText)
}

func hasError(problems []error, target error) bool {
	for _, problem := range problems {
		if errors.Is(problem, target) {
			return true
		}
	}
	return false
}
